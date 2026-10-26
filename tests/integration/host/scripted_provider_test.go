package host

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// scriptedTerminalAnswer is the one terminal answer the scripted provider
// serves beside no scripted reply: an assistant message with no tool call ends
// the turn of the drive that reads it.
const scriptedTerminalAnswer = `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`

// scriptedProvider is the OpenAI-compatible endpoint the boot checks run
// against: every chat completion answers the scripted terminal answer, and the
// body of every request is recorded, so the model a request named and the
// sampling it carried are read from what the provider actually saw instead of
// from the declaration alone.
//
// Two decision hooks extend the call ordinal: answer reads the ordinal alone,
// and decide reads the whole request - the identity it named beside the
// messages and tools it carried - so a multi-decision script selects the next
// step from what the drive actually asked for. A decision that reports an
// error refuses the request instead of answering a default terminal.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
type scriptedProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
	answer func(index int) string
	decide func(request scriptedRequest) (string, error)
}

// scriptedRequest is one chat completion request as the endpoint decoded it:
// the ordinal it arrived at, the model it named, and the messages and tools it
// carried, so a decision selects its next step from the request itself.
type scriptedRequest struct {
	Index    int
	Model    string
	Body     map[string]json.RawMessage
	Messages []scriptedMessage
	Tools    []string
}

// scriptedMessage is one message a scripted request carried.
type scriptedMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []scriptedToolCall
}

// scriptedToolCall is one tool call an assistant message of a request carried.
type scriptedToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// newScriptedProvider starts the endpoint and closes it with the test.
func newScriptedProvider(t *testing.T) *scriptedProvider {
	t.Helper()
	provider := &scriptedProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve answers one chat completion and records the body the request carried.
func (p *scriptedProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	var modelName string
	if raw, ok := body["model"]; ok {
		_ = json.Unmarshal(raw, &modelName)
	}
	p.mu.Lock()
	index := len(p.bodies)
	p.models = append(p.models, modelName)
	p.bodies = append(p.bodies, body)
	answer := p.answer
	decide := p.decide
	p.mu.Unlock()
	payload := scriptedTerminalAnswer
	status := http.StatusOK
	switch {
	case decide != nil:
		decided, err := decide(decodeScriptedRequest(index, modelName, body))
		if err != nil {
			status = http.StatusBadRequest
			payload = scriptedRefusalAnswer(err.Error())
			break
		}
		payload = decided
	case answer != nil:
		payload = answer(index)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, payload)
}

// decodeScriptedRequest reads one recorded body into the request shape a
// decision selects on.
func decodeScriptedRequest(index int, modelName string, body map[string]json.RawMessage) scriptedRequest {
	request := scriptedRequest{Index: index, Model: modelName, Body: body}
	if raw, ok := body["messages"]; ok {
		var messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(raw, &messages); err == nil {
			for _, message := range messages {
				decoded := scriptedMessage{Role: message.Role, Content: scriptedContent(message.Content), ToolCallID: message.ToolCallID}
				for _, call := range message.ToolCalls {
					decoded.ToolCalls = append(decoded.ToolCalls, scriptedToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
				}
				request.Messages = append(request.Messages, decoded)
			}
		}
	}
	if raw, ok := body["tools"]; ok {
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err == nil {
			for _, tool := range tools {
				request.Tools = append(request.Tools, tool.Function.Name)
			}
		}
	}
	return request
}

// scriptedContent reads one message content the protocol may render as a plain
// string or as parts.
func scriptedContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "")
}

// scriptedRefusalAnswer renders the refusal the endpoint answers a request its
// script never declared, so the drive fails instead of reading a default
// terminal answer.
func scriptedRefusalAnswer(reason string) string {
	encoded, err := json.Marshal(reason)
	if err != nil {
		panic("host: encode the scripted refusal: " + err.Error())
	}
	return `{"error":{"message":` + string(encoded) + `,"type":"invalid_request_error"}}`
}

// servedModels reports the model names the endpoint was asked for, in request
// order, as a copy the caller owns.
func (p *scriptedProvider) servedModels() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.models...)
}

// recorded reports the requests the endpoint answered, as a copy the caller
// owns.
func (p *scriptedProvider) recorded() []map[string]json.RawMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]json.RawMessage(nil), p.bodies...)
}

// servedRequests reports the requests the endpoint answered in the decoded
// shape a decision selects on, as a copy the caller owns.
func (p *scriptedProvider) servedRequests() []scriptedRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]scriptedRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, decodeScriptedRequest(index, p.models[index], body))
	}
	return out
}

// endpoint reports the base URL the document's provider entry names.
func (p *scriptedProvider) endpoint() string { return p.server.URL + "/v1" }

// scriptedHostDocument renders one host document whose entry agent resolves the
// named model at the scripted provider over the storage root.
func scriptedHostDocument(root, endpoint, modelName string) string {
	return scriptedHostDocumentWithAgent(root, endpoint, modelName, "")
}

// scriptedHostDocumentWithAgent renders one host document whose entry agent
// section carries the given block of agent fields; an empty block declares the
// entry agent bare.
func scriptedHostDocumentWithAgent(root, endpoint, modelName, agentBlock string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + modelName + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	if strings.TrimSpace(agentBlock) == "" {
		b.WriteString("  user: {}\n")
	} else {
		b.WriteString("  user:\n")
		b.WriteString(agentBlock)
	}
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// structuralScriptedHostDocument renders one host document that declares a
// second agent beside the entry agent, the structural change a reload refuses.
func structuralScriptedHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: model-a\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user: {}\n")
	b.WriteString("  other: {}\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// writeScriptedHostDocument writes one host document over the temp root and
// answers its path.
func writeScriptedHostDocument(t *testing.T, root, endpoint, modelName string) string {
	t.Helper()
	configPath := filepath.Join(root, "tagent.yaml")
	rewriteScriptedHostDocument(t, configPath, root, endpoint, modelName)
	return configPath
}

// rewriteScriptedHostDocument rewrites the configuration source in place, the
// step a supervisor runs before it posts the reload.
func rewriteScriptedHostDocument(t *testing.T, configPath, root, endpoint, modelName string) {
	t.Helper()
	writeHostDocumentFile(t, configPath, scriptedHostDocument(root, endpoint, modelName))
}

// writeHostDocumentFile writes one document body to the configuration source.
func writeHostDocumentFile(t *testing.T, configPath, document string) {
	t.Helper()
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
}

// TestEventIntegrityHarness drives the scripted multi-decision fixture over the formal assembly and checks the three anchors of the integrity harness:
// - positive: a declared multi-decision script runs the whole chain and every model step maps to the action it caused
// - negative: a request outside the script, a declared step never reached and a step reached twice each fail by name
// - consumer: the boot assembly consumes the real requests Host.Invoke drives, identity by identity, beside a tool parked on the outbound face
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestEventIntegrityHarness(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Run("positive", runIntegrityPositive)
	t.Run("negative", runIntegrityNegative)
	t.Run("consumer", runIntegrityConsumer)
}
