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
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
type scriptedProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
	answer func(index int) string
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
	p.mu.Unlock()
	payload := scriptedTerminalAnswer
	if answer != nil {
		payload = answer(index)
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, payload)
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
