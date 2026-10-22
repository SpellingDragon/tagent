// Package journey drives one whole process through the production path —
// Boot assembles the host, Invoke drives a turn, the run commits and the
// sender receives — with only the model HTTP endpoint scripted. Nothing below
// the endpoint is replaced: the host, the loop, the task wrapper, the mailbox,
// the delivery face and the sender are the ones a deployment runs.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

const (
	// journeyEntryAgent is the entry agent every journey hosts: the address the
	// user writes to and the one the host delivers to.
	journeyEntryAgent = event.AgentID("user")
	// journeyEntryModel is the model name the entry agent resolves beside no
	// per-agent override.
	journeyEntryModel = "model-chat"
	// journeyWorkerModel is the model name a delegated worker resolves.
	journeyWorkerModel = "model-worker"
	// journeyScriptWait bounds how long a scripted reply may be held open.
	journeyScriptWait = 30 * time.Second
)

// journeyReply is one scripted completion the endpoint serves: the body it
// answers with, and optionally a gate it waits on before answering, so a
// journey holds a delegated turn open while it observes the state before that
// answer exists.
type journeyReply struct {
	body  string
	gate  <-chan struct{}
	delay time.Duration
}

// journeyRequest is one request the endpoint answered, recorded as it arrived.
type journeyRequest struct {
	Model string
	Body  map[string]json.RawMessage
}

// journeyMessage is one message of a recorded request, decoded enough to read
// the text and the tool calls a journey asserts on.
type journeyMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// journeyProvider is the OpenAI-compatible endpoint the journeys run against:
// each model name carries its own queue of scripted completions, every request
// is recorded as it arrived, and a reply the test gated is answered only once
// the gate opens. Per-model queues keep the delegated turn's requests apart
// from the caller's when both run at once.
type journeyProvider struct {
	server  *httptest.Server
	mu      sync.Mutex
	scripts map[string][]journeyReply
	models  []string
	bodies  []journeyRequest
}

// newJourneyProvider starts the scripted endpoint and closes it with the test.
func newJourneyProvider(t *testing.T) *journeyProvider {
	t.Helper()
	provider := &journeyProvider{scripts: map[string][]journeyReply{}}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// script queues replies of one model name, served in order.
func (p *journeyProvider) script(modelName string, replies ...journeyReply) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scripts[modelName] = append(p.scripts[modelName], replies...)
}

// scriptAnswers queues terminal answers of one model name.
func (p *journeyProvider) scriptAnswers(modelName string, contents ...string) {
	replies := make([]journeyReply, 0, len(contents))
	for _, content := range contents {
		replies = append(replies, journeyReply{body: journeyAnswer(content)})
	}
	p.script(modelName, replies...)
}

// scriptGated queues one terminal answer the endpoint serves only after the
// gate opens, so a journey controls when a delegated turn settles.
func (p *journeyProvider) scriptGated(modelName string, gate <-chan struct{}, content string) {
	p.script(modelName, journeyReply{body: journeyAnswer(content), gate: gate})
}

// serve answers one chat completion: it records the request, pops the next
// scripted reply of that model name, waits for the gate the reply names and
// answers with the reply body.
func (p *journeyProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	modelName := ""
	if raw, ok := body["model"]; ok {
		_ = json.Unmarshal(raw, &modelName)
	}
	p.mu.Lock()
	p.models = append(p.models, modelName)
	p.bodies = append(p.bodies, journeyRequest{Model: modelName, Body: body})
	var reply journeyReply
	if queued := p.scripts[modelName]; len(queued) > 0 {
		reply = queued[0]
		p.scripts[modelName] = queued[1:]
	}
	p.mu.Unlock()
	if reply.gate != nil {
		timer := time.NewTimer(journeyScriptWait)
		defer timer.Stop()
		select {
		case <-reply.gate:
		case <-timer.C:
			http.Error(w, "the scripted reply was never released", http.StatusGatewayTimeout)
			return
		case <-r.Context().Done():
			return
		}
	}
	if reply.delay > 0 {
		time.Sleep(reply.delay)
	}
	payload := reply.body
	if payload == "" {
		payload = journeyAnswer("ok")
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, payload)
}

// endpoint reports the base URL the host document's provider entry names.
func (p *journeyProvider) endpoint() string { return p.server.URL + "/v1" }

// servedModels reports the model names the endpoint was asked for, in request
// order.
func (p *journeyProvider) servedModels() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.models...)
}

// recorded reports the requests the endpoint answered, in request order.
func (p *journeyProvider) recorded() []journeyRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]journeyRequest(nil), p.bodies...)
}

// requestsFor reports the requests one model name carried, in request order.
func (p *journeyProvider) requestsFor(modelName string) []journeyRequest {
	out := make([]journeyRequest, 0, 4)
	for _, req := range p.recorded() {
		if req.Model == modelName {
			out = append(out, req)
		}
	}
	return out
}

// text renders the text one recorded request carried: every message's content
// in order, so a journey asserts on what the model actually read.
func (r journeyRequest) text() string {
	messages := r.messages()
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) != "" {
			parts = append(parts, msg.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// messages decodes the messages one recorded request carried.
func (r journeyRequest) messages() []journeyMessage {
	raw, ok := r.Body["messages"]
	if !ok {
		return nil
	}
	var messages []journeyMessage
	_ = json.Unmarshal(raw, &messages)
	return messages
}

// toolCalls reports the tool calls the model returned in one recorded
// request's messages, in message order.
func (r journeyRequest) toolCalls() []string {
	var names []string
	for _, msg := range r.messages() {
		for _, call := range msg.ToolCalls {
			names = append(names, call.Function.Name)
		}
	}
	return names
}

// journeyAnswer renders the terminal completion body whose assistant text is
// the given content.
func journeyAnswer(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic("journey: encode the scripted answer: " + err.Error())
	}
	return `{"id":"journey","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":` +
		string(encoded) + `},"finish_reason":"stop"}]}`
}

// journeyToolCall renders the completion body of one decision that calls one
// tool with the given arguments, so the loop admits the call through the real
// task wrapper.
func journeyToolCall(id, name, args string) string {
	return `{"id":"journey","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"",` +
		`"tool_calls":[{"id":` + journeyQuote(id) + `,"type":"function","function":{"name":` + journeyQuote(name) +
		`,"arguments":` + journeyQuote(args) + `}}]},"finish_reason":"tool_calls"}]}`
}

// journeyQuote renders one string as the JSON literal the completion body
// carries.
func journeyQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic("journey: encode the scripted literal: " + err.Error())
	}
	return string(encoded)
}

// journeyDelivery is one body the sender accepted beside the address it was
// addressed to.
type journeyDelivery struct {
	Target  event.Address
	Content string
}

// journeySender is the outbound message face of the journeys: every accepted
// body is recorded beside the address it was delivered to, in delivery order,
// so a journey asserts on what the user actually received.
type journeySender struct {
	mu        sync.Mutex
	delivered []journeyDelivery
}

// Send records one body and reports the accepted delivery.
func (s *journeySender) Send(_ context.Context, target event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delivered = append(s.delivered, journeyDelivery{Target: target, Content: content})
	return nil
}

// deliveries reports the accepted bodies beside their addresses, in delivery
// order.
func (s *journeySender) deliveries() []journeyDelivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]journeyDelivery(nil), s.delivered...)
}

// bodies reports the accepted bodies in delivery order.
func (s *journeySender) bodies() []string {
	out := make([]string, 0, len(s.deliveries()))
	for _, delivery := range s.deliveries() {
		out = append(out, delivery.Content)
	}
	return out
}

// waitForCount waits until at least count bodies were delivered and reports
// them, so a journey that waits on a detached turn asserts on what the user
// received instead of on an internal state.
func (s *journeySender) waitForCount(t *testing.T, count int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		bodies := s.bodies()
		if len(bodies) >= count {
			return bodies
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivered bodies = %q after %s, want at least %d body(ies)", bodies, timeout, count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitFor waits until a delivered body carries the given text and reports that
// body, so a journey waits on what the user received.
func (s *journeySender) waitFor(t *testing.T, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, body := range s.bodies() {
			if strings.Contains(body, substr) {
				return body
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivered bodies = %q after %s, want one carrying %q", s.bodies(), timeout, substr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// journeySpec is the host document one journey boots: the entry agent, the
// delegated worker it may call, and whatever extra agent sections the journey
// declares (the meditation session included).
type journeySpec struct {
	// Model is the entry agent's model name.
	Model string
	// WorkerModel is the delegated worker's model name.
	WorkerModel string
	// MaxTokens is the entry agent's declared model window, zero for none.
	MaxTokens int
	// EntryTools lists the host tool ids the entry agent declares.
	EntryTools []string
	// Delegate names the worker agent the entry agent may call; empty declares
	// no worker.
	Delegate string
	// WorkerTools lists the host tool ids the worker declares.
	WorkerTools []string
	// EntryExtra lists extra host-document lines of the entry agent section,
	// already indented.
	EntryExtra []string
	// WorkerExtra lists extra host-document lines of the worker section.
	WorkerExtra []string
	// ExtraAgents carries full agent sections by id, each line already indented.
	ExtraAgents map[string][]string
	// PromptFiles carries files written beside the document, by name.
	PromptFiles map[string]string
	// DocumentExtra lists extra top-level document lines.
	DocumentExtra []string
}

// journeyHost is one booted host beside the faces a journey asserts on.
type journeyHost struct {
	host       *tagent.Host
	sender     *journeySender
	provider   *journeyProvider
	root       string
	configPath string
	spec       journeySpec
}

// bootJourney writes the host document over a temp root, boots the real host
// over it and returns the host beside its recorded sender. The host closes with
// the test under a bounded context: a journey that parks a call in the approval
// wait leaves one task obligation beside no settlement face, so the close
// reports it as unsettled instead of waiting the whole drain window here.
func bootJourney(t *testing.T, provider *journeyProvider, spec journeySpec, options ...tagent.HostOption) *journeyHost {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	if spec.Model == "" {
		spec.Model = journeyEntryModel
	}
	if spec.WorkerModel == "" {
		spec.WorkerModel = journeyWorkerModel
	}
	root := t.TempDir()
	for name, content := range spec.PromptFiles {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the prompt directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write the prompt file %s: %v", name, err)
		}
	}
	configPath := filepath.Join(root, "tagent.yaml")
	writeJourneyDocument(t, configPath, journeyDocument(root, provider.endpoint(), spec))
	sender := &journeySender{}
	options = append([]tagent.HostOption{tagent.WithSender(sender)}, options...)
	host, err := tagent.Boot(configPath, options...)
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	journey := &journeyHost{host: host, sender: sender, provider: provider, root: root, configPath: configPath, spec: spec}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Errorf("close the host: %v", err)
		}
	})
	return journey
}

// journeyDocument renders the host document one journey boots: the entry agent
// beside the worker it may delegate to and every extra agent section the
// journey declares.
func journeyDocument(root, endpoint string, spec journeySpec) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + spec.Model + "\n")
	if len(spec.PromptFiles) > 0 {
		b.WriteString("prompt_dir: " + root + "\n")
	}
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	writeJourneyTools(&b, spec.EntryTools, spec.Delegate)
	if spec.MaxTokens > 0 {
		b.WriteString(fmt.Sprintf("    max_tokens: %d\n", spec.MaxTokens))
	}
	for _, line := range spec.EntryExtra {
		b.WriteString(line + "\n")
	}
	if spec.Delegate != "" {
		b.WriteString("  " + spec.Delegate + ":\n")
		b.WriteString("    model: " + spec.WorkerModel + "\n")
		writeJourneyTools(&b, spec.WorkerTools, "")
		for _, line := range spec.WorkerExtra {
			b.WriteString(line + "\n")
		}
	}
	names := make([]string, 0, len(spec.ExtraAgents))
	for name := range spec.ExtraAgents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteString("  " + name + ":\n")
		for _, line := range spec.ExtraAgents[name] {
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	for _, line := range spec.DocumentExtra {
		b.WriteString(line + "\n")
	}
	return strings.ReplaceAll(b.String(), "{{root}}", root)
}

// writeJourneyTools renders the tools section of one agent: the host tools it
// names beside the delegation to the worker when one is declared.
func writeJourneyTools(b *strings.Builder, tools []string, delegate string) {
	if len(tools) == 0 && delegate == "" {
		return
	}
	b.WriteString("    tools:\n")
	for _, tool := range tools {
		b.WriteString("      - { kind: tool, id: " + tool + " }\n")
	}
	if delegate != "" {
		b.WriteString("      - { kind: agent, agent: " + delegate + " }\n")
	}
}

// writeJourneyDocument writes one host document body to the configuration
// source.
func writeJourneyDocument(t *testing.T, configPath, document string) {
	t.Helper()
	if err := os.WriteFile(configPath, []byte(document), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
}

// rewrite rewrites the configuration source in place, the step a supervisor
// runs before it posts the reload, and keeps the same document shape.
func (h *journeyHost) rewrite(t *testing.T, spec journeySpec) {
	t.Helper()
	if spec.Model == "" {
		spec.Model = journeyEntryModel
	}
	if spec.WorkerModel == "" {
		spec.WorkerModel = journeyWorkerModel
	}
	h.spec = spec
	writeJourneyDocument(t, h.configPath, journeyDocument(h.root, h.provider.endpoint(), spec))
}

// invoke drives one user turn through the host: the entry address the host
// delivers to, one user message, and the user trigger the meditation driver
// and the delivery face both read.
func (h *journeyHost) invoke(ctx context.Context, session, input string) (agent.Turn, error) {
	return h.host.Invoke(ctx, tagent.InvokeRequest{
		Address:       event.Address{AgentID: journeyEntryAgent, SessionID: event.SessionID(session)},
		Input:         []model.Message{{Role: model.RoleUser, Content: input}},
		TriggerSource: "user",
	})
}

// invokeOK drives one user turn and requires the completed terminal.
func (h *journeyHost) invokeOK(t *testing.T, ctx context.Context, session, input string) agent.Turn {
	t.Helper()
	turn, err := h.invoke(ctx, session, input)
	if err != nil {
		t.Fatalf("invoke the user turn: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn", turn.Terminal)
	}
	return turn
}

// sessionFacts reports the committed facts of the user session the journeys
// read the chain through.
func (h *journeyHost) sessionFacts(session string) []event.Event {
	return h.host.Run().SessionFacts(event.Address{AgentID: journeyEntryAgent, SessionID: event.SessionID(session)})
}

// waitForRequests waits until the endpoint answered at least count requests of
// one model name and reports them, so a journey waits on the request the model
// actually received instead of on an internal state.
func (p *journeyProvider) waitForRequests(t *testing.T, modelName string, count int, timeout time.Duration) []journeyRequest {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		requests := p.requestsFor(modelName)
		if len(requests) >= count {
			return requests
		}
		if time.Now().After(deadline) {
			t.Fatalf("requests for model %q = %d after %s, want at least %d", modelName, len(requests), timeout, count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForFact waits until the user session chain carries a fact the match
// accepts and reports it, so a journey waits on what the session recorded.
func (h *journeyHost) waitForFact(t *testing.T, session string, match func(event.Event) bool, timeout time.Duration) event.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, fact := range h.sessionFacts(session) {
			if match(fact) {
				return fact
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %q carries no matching fact after %s", session, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
