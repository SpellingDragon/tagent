// meditation_autonomy_test.go 钉住冥想自主面的端到端行为：工具面（file/exec/recall/send）
// 经既有装配接线、状态机驱动的自主整理轮、负反馈进摘要、自察迁移到宿主自己的专属反思
// 会话，以及挂载形态的行为等价。模型回答由脚本端点给出，装配走真实 assembly.Boot，
// 因此这些用例证明的是"真实装配面上的行为"，不是替身对象之间的约定。
package main

import (
	"context"
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
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
)

const (
	autonomyAPIKeyEnv  = "TAGENT_TEST_API_KEY"
	autonomyReflection = "tagent-session:tagent:reflection"
	autonomyCuration   = "tagent-session:tagent:meditation"
	autonomyBusiness   = "tagent-session:tagent:autonomy-business"
	autonomyLesson     = "LESSON-CARD-MARKER"
	autonomyNegative   = "NEGATIVE-FEEDBACK-MARKER"
	autonomyReal       = "REAL-CONTEXT-MARKER"
	autonomyTidy       = "TIDY-OUTPUT-MARKER"
)

// autonomyRequest is one model request the scripted endpoint answered: the tools
// the turn was offered, the messages it read and the tool calls it had issued.
type autonomyRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

// reflection reports whether this request is the reflection turn's own.
func (r autonomyRequest) reflection() bool {
	return strings.Contains(r.text(), "[reflection]")
}

// text renders every message body the request read.
func (r autonomyRequest) text() string {
	parts := make([]string, 0, len(r.Messages))
	for _, msg := range r.Messages {
		parts = append(parts, msg.Content)
	}
	return strings.Join(parts, "\n")
}

// toolNames reports the model-facing tool names of this request.
func (r autonomyRequest) toolNames() map[string]bool {
	names := make(map[string]bool, len(r.Tools))
	for _, tool := range r.Tools {
		names[tool.Function.Name] = true
	}
	return names
}

// hasToolResult reports whether a call's observation reached the model.
func (r autonomyRequest) hasToolResult(callID string) bool {
	for _, msg := range r.Messages {
		if msg.Role == "tool" && msg.ToolCallID == callID {
			return true
		}
	}
	return false
}

// issued reports whether the model already asked for a named tool.
func (r autonomyRequest) issued(name string) bool {
	for _, msg := range r.Messages {
		for _, call := range msg.ToolCalls {
			if call.Function.Name == name {
				return true
			}
		}
	}
	return false
}

// toolResult returns the observation text of one call.
func (r autonomyRequest) toolResult(callID string) string {
	for _, msg := range r.Messages {
		if msg.Role == "tool" && msg.ToolCallID == callID {
			return msg.Content
		}
	}
	return ""
}

// autonomyProvider is the scripted OpenAI-compatible endpoint: it records every
// request and answers through the callback the case controls.
type autonomyProvider struct {
	mu       sync.Mutex
	requests []autonomyRequest
	ch       chan autonomyRequest
	answer   func(autonomyRequest) string
}

// newAutonomyProvider starts the endpoint beside one answer script.
func newAutonomyProvider(t *testing.T, answer func(autonomyRequest) string) (*autonomyProvider, *httptest.Server) {
	t.Helper()
	provider := &autonomyProvider{ch: make(chan autonomyRequest, 64), answer: answer}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req autonomyRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		provider.mu.Lock()
		provider.requests = append(provider.requests, req)
		provider.mu.Unlock()
		select {
		case provider.ch <- req:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, provider.answer(req))
	}))
	t.Cleanup(server.Close)
	return provider, server
}

// await reads requests until one matches, failing when the deadline passes.
func (p *autonomyProvider) await(t *testing.T, match func(autonomyRequest) bool) autonomyRequest {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case req := <-p.ch:
			if match(req) {
				return req
			}
		case <-deadline:
			p.mu.Lock()
			total := len(p.requests)
			p.mu.Unlock()
			t.Fatalf("no matching model request arrived within the deadline (%d requests answered)", total)
		}
	}
}

// autonomyAnswer renders one plain completion body.
func autonomyAnswer(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic("autonomy: encode the scripted answer: " + err.Error())
	}
	return `{"id":"autonomy","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":` +
		string(encoded) + `},"finish_reason":"stop"}]}`
}

// autonomyCall renders one tool-call completion body.
func autonomyCall(id, name, args string) string {
	encode := func(s string) string {
		raw, err := json.Marshal(s)
		if err != nil {
			panic("autonomy: encode the scripted tool call: " + err.Error())
		}
		return string(raw)
	}
	return `{"id":"autonomy","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"",` +
		`"tool_calls":[{"id":` + encode(id) + `,"type":"function","function":{"name":` + encode(name) +
		`,"arguments":` + encode(args) + `}}]},"finish_reason":"tool_calls"}]}`
}

// autonomyCalls renders one completion carrying several tool calls.
func autonomyCalls(calls ...[3]string) string {
	encoded := make([]string, 0, len(calls))
	for _, call := range calls {
		encode := func(s string) string {
			raw, err := json.Marshal(s)
			if err != nil {
				panic("autonomy: encode the scripted tool call: " + err.Error())
			}
			return string(raw)
		}
		encoded = append(encoded, `{"id":`+encode(call[0])+`,"type":"function","function":{"name":`+encode(call[1])+`,"arguments":`+encode(call[2])+`}}`)
	}
	return `{"id":"autonomy","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[` +
		strings.Join(encoded, ",") + `]},"finish_reason":"tool_calls"}]}`
}

// autonomySender records every body the host's outbound face accepted.
type autonomySender struct {
	mu     sync.Mutex
	bodies []string
}

func (s *autonomySender) Send(_ context.Context, _ event.Address, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, body)
	return nil
}

func (s *autonomySender) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// autonomyDocument fixes the host document one case boots.
type autonomyDocument struct {
	root       string
	endpoint   string
	tools      []string
	meditation bool
	deliverTo  []string
	curator    bool
}

// writeAutonomyDocument writes the host document and the prompt it names.
func writeAutonomyDocument(t *testing.T, cfg autonomyDocument) string {
	t.Helper()
	var b strings.Builder
	line := func(text string) { fmt.Fprintln(&b, text) }
	line("entry: tagent")
	line("provider: local")
	line("model: scripted")
	line("prompt_dir: " + cfg.root)
	line("providers:")
	line("  local: { provider: openai, api_endpoint: " + strconvQuote(cfg.endpoint+"/v1") + ", api_key_env: " + autonomyAPIKeyEnv + " }")
	line("agents:")
	line("  tagent:")
	line("    tools:")
	for _, name := range cfg.tools {
		line("      - { kind: tool, id: " + name + " }")
	}
	line("    file: { base_dir: " + strconvQuote(cfg.root) + " }")
	line("    exec: { monitor: { interval: 1s, stable_duration: 2s, fake_dead_duration: 5s } }")
	line("    memory: { type: localfile, path: " + strconvQuote(filepath.Join(cfg.root, "data")) + ", read_namespaces: [tagent] }")
	if cfg.meditation {
		line("    meditation:")
		line("      enabled: true")
		line("      interval: 1ms")
		line("      min_gap: 0s")
		line("      prompt_file: meditation.md")
		line("      deliver_to: [" + strings.Join(cfg.deliverTo, ",") + "]")
	}
	if cfg.curator {
		line("  meditator:")
		line("    tools:")
		line("      - { kind: tool, id: send }")
		line("    memory: { type: localfile, path: " + strconvQuote(filepath.Join(cfg.root, "data")) + ", read_namespaces: [tagent] }")
		line("    meditation:")
		line("      enabled: true")
		line("      interval: 1ms")
		line("      min_gap: 0s")
		line("      prompt_file: meditation.md")
		line("      observed_namespaces: [tagent]")
		line("      deliver_to: [tagent]")
	}
	line("storage: { type: localfile, root: " + strconvQuote(filepath.Join(cfg.root, "data")) + " }")
	path := filepath.Join(cfg.root, "tagent.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.root, "meditation.md"), []byte("reflect on the observed material"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	return path
}

// strconvQuote quotes one YAML scalar with JSON's string rules, which is a YAML
// subset.
func strconvQuote(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic("autonomy: quote the document scalar: " + err.Error())
	}
	return string(raw)
}

// bootAutonomyHost assembles the host and registers its close.
func bootAutonomyHost(t *testing.T, configPath string) (*assembly.Host, *autonomySender) {
	t.Helper()
	t.Setenv(autonomyAPIKeyEnv, "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	t.Setenv("TAGENT_SESSION_ID", "")
	sender := &autonomySender{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Logf("close the host: %v", err)
		}
	})
	return host, sender
}

// invokeAutonomyTurn drives one user turn at a fixed business session.
func invokeAutonomyTurn(t *testing.T, host *assembly.Host, session, content string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address:       event.Address{AgentID: "tagent", SessionID: event.SessionID(session)},
		TriggerSource: "user",
		Input:         []tagentmodel.Message{{Role: tagentmodel.RoleUser, Content: content}},
	})
	if err != nil {
		t.Fatalf("invoke the business turn: %v", err)
	}
}

// requireTmux skips the assembly cases that declare the exec face beside a host
// without tmux, the way the operation face itself refuses.
func requireTmux(t *testing.T) {
	t.Helper()
	if !tagentexec.IsTmuxAvailable() {
		t.Skip("tmux is unavailable, so the declared exec face cannot be assembled")
	}
}

// awaitSessionFacts waits for one address's chain to satisfy the predicate.
func awaitSessionFacts(t *testing.T, host *assembly.Host, session string, match func([]event.Event) bool) []event.Event {
	t.Helper()
	return awaitSessionFactsOf(t, host, "tagent", session, match)
}

// awaitSessionFactsOf waits for one agent's named session to satisfy the
// predicate: the curation declaration writes under its own agent, not the host.
func awaitSessionFactsOf(t *testing.T, host *assembly.Host, agent, session string, match func([]event.Event) bool) []event.Event {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	addr := event.Address{AgentID: event.AgentID(agent), SessionID: event.SessionID(session)}
	for time.Now().Before(deadline) {
		facts := host.Run().SessionFacts(addr)
		if match(facts) {
			return facts
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, active := range host.Run().ActiveSessions("") {
		t.Logf("session %s/%s facts=%d", active.AgentID, active.SessionID, len(host.Run().SessionFacts(active)))
	}
	t.Fatalf("session %q never reached the awaited state", session)
	return nil
}

// TestMeditatorToolFaceWired pins the migrated tool face: the agent that declares meditation gets the same file, exec, recall and send tools.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditatorToolFaceWired(t *testing.T) {
	requireTmux(t)
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if req.reflection() {
			return autonomyAnswer("reflection examined the material")
		}
		return autonomyAnswer("business answer")
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:     root,
		endpoint: server.URL,
		tools: []string{"send", "read_file", "list_file", "search_file", "search_content",
			"read_multiple_files", "save_file", "replace_content", "exec",
			"recall_query", "recall_get", "recall_recent", "recall_trace"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "business turn before reflection")
	req := provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	names := req.toolNames()
	for _, want := range []string{"send", "read_file", "save_file", "exec_run", "exec_release", "recall_query", "recall_recent"} {
		if !names[want] {
			t.Errorf("reflection tool face carries no %q, want the declared file/exec/recall/send tools", want)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(req.text()), "[reflection]") {
		t.Errorf("reflection input = %q, want the unified [reflection] header", req.text())
	}
}

// TestMeditatorToolFaceStillGated pins the negative anchor: a send outside the declared deliver_to white list is refused by name.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditatorToolFaceStillGated(t *testing.T) {
	requireTmux(t)
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if !req.reflection() {
			return autonomyAnswer("business answer")
		}
		if req.hasToolResult("call-gate") {
			return autonomyAnswer("the gate refused the outside target")
		}
		return autonomyCall("call-gate", "send", `{"target":{"agent":"intruder","session":"tagent-session:intruder:main"},"content":"lesson outside the white list"}`)
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "exec", "recall_recent", "read_file"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "business turn before the gated send")
	req := provider.await(t, func(req autonomyRequest) bool {
		return req.reflection() && req.hasToolResult("call-gate")
	})
	if !strings.Contains(req.text(), "deliver_to white list") || !strings.Contains(req.text(), "intruder") {
		t.Fatalf("refused send observation = %q, want the named deliver_to refusal naming the outside target", req.text())
	}
}

// TestDigestIncludesNegativeFeedback pins the negative-feedback priority: a feedback fact renders before ordinary facts and carries its event key.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestDigestIncludesNegativeFeedback(t *testing.T) {
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if req.reflection() {
			return autonomyAnswer("negative feedback read")
		}
		return autonomyAnswer("business answer")
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "recall_recent"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	addr := event.Address{AgentID: "tagent", SessionID: autonomyBusiness}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key, err := host.Run().CommitToSession(ctx, addr, event.Event{
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource: "failure",
			event.MetaKeyEventSummary:  autonomyNegative,
		},
		Message: tagentmodel.Message{Role: tagentmodel.RoleUser, Content: autonomyNegative + " body"},
	})
	if err != nil {
		t.Fatalf("commit the negative feedback fact: %v", err)
	}
	invokeAutonomyTurn(t, host, autonomyBusiness, "business turn after the failure")
	req := provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	if !strings.Contains(req.text(), "negative feedback") {
		t.Fatalf("reflection input = %q, want the negative feedback priority section", req.text())
	}
	if !strings.Contains(req.text(), string(key)) {
		t.Fatalf("reflection input = %q, want the negative event key %q", req.text(), key)
	}
	if !strings.Contains(req.text(), autonomyNegative) {
		t.Fatalf("reflection input = %q, want the negative feedback summary", req.text())
	}
}

// TestSelfMeditationFullContext pins the full-context rule: the reflection reads the real committed bodies of its own partition, not only digest summaries.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestSelfMeditationFullContext(t *testing.T) {
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if req.reflection() {
			return autonomyAnswer("full context read")
		}
		return autonomyAnswer("business answer")
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "recall_recent"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "please inspect "+autonomyReal+" in this conversation")
	req := provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	if !strings.Contains(req.text(), autonomyReal) {
		t.Fatalf("reflection input = %q, want the real body carrying %q", req.text(), autonomyReal)
	}
	if !strings.Contains(req.text(), "observed material") {
		t.Fatalf("reflection input = %q, want the bounded material section", req.text())
	}
}

// TestReflectionSessionIsolated pins the dedicated reflection session and the business chain that carries no reflection fact.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestReflectionSessionIsolated(t *testing.T) {
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if req.reflection() {
			return autonomyAnswer("reflection kept its own session")
		}
		return autonomyAnswer("business answer")
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "recall_recent"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "business turn for isolation")
	provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	reflectionFacts := awaitSessionFacts(t, host, autonomyReflection, func(facts []event.Event) bool {
		return len(facts) >= 2
	})
	reflectionInput := false
	for _, fact := range reflectionFacts {
		if fact.Owner.AgentID != "tagent" {
			t.Errorf("reflection fact owner = %q, want the host itself", fact.Owner.AgentID)
		}
		if strings.Contains(fact.Message.Content, "[reflection]") {
			reflectionInput = true
		}
	}
	if !reflectionInput {
		t.Fatalf("reflection chain carries no [reflection] input: %+v", reflectionFacts)
	}
	businessFacts := host.Run().SessionFacts(event.Address{AgentID: "tagent", SessionID: autonomyBusiness})
	for _, fact := range businessFacts {
		if strings.Contains(fact.Message.Content, "[reflection]") {
			t.Fatalf("business chain carries a reflection fact: %+v", fact)
		}
	}
}

// TestLessonsLandInOwnMemory pins where a reflection's product lands: an ordinary agent_output fact of the reflection session.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestLessonsLandInOwnMemory(t *testing.T) {
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if req.reflection() {
			return autonomyAnswer(autonomyLesson + " recorded as a lesson")
		}
		return autonomyAnswer("business answer")
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "recall_recent"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "business turn before the lesson")
	provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	facts := awaitSessionFacts(t, host, autonomyReflection, func(facts []event.Event) bool {
		for _, fact := range facts {
			if strings.Contains(fact.Message.Content, autonomyLesson) {
				return true
			}
		}
		return false
	})
	landed := false
	for _, fact := range facts {
		if !strings.Contains(fact.Message.Content, autonomyLesson) {
			continue
		}
		landed = true
		if fact.Type != event.TypeAgentOutput {
			t.Errorf("lesson fact type = %q, want the ordinary agent_output type", fact.Type)
		}
	}
	if !landed {
		t.Fatalf("reflection chain carries no lesson: %+v", facts)
	}
}

// TestLessonsReachableByBusinessTurns pins the delivery side: a reflection sends its lesson through the existing send gate.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestLessonsReachableByBusinessTurns(t *testing.T) {
	var lessonKey string
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if req.reflection() {
			if req.hasToolResult("call-send-lesson") {
				return autonomyAnswer("lesson delivered")
			}
			return autonomyCall("call-send-lesson", "send",
				`{"target":{"agent":"tagent","session":"`+autonomyBusiness+`"},"content":"`+autonomyLesson+` from the reflection"}`)
		}
		if strings.Contains(req.text(), "BUSINESS-RECALL-REQUEST") {
			if req.hasToolResult("call-get-lesson") {
				return autonomyAnswer("lesson read")
			}
			return autonomyCall("call-get-lesson", "recall_get", `{"key":"`+lessonKey+`"}`)
		}
		return autonomyAnswer("business answer")
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "recall_get", "recall_recent"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "business turn before the lesson")
	provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	facts := awaitSessionFacts(t, host, autonomyBusiness, func(facts []event.Event) bool {
		for _, fact := range facts {
			if strings.Contains(fact.Message.Content, autonomyLesson) {
				return true
			}
		}
		return false
	})
	for _, fact := range facts {
		if strings.Contains(fact.Message.Content, autonomyLesson) {
			lessonKey = string(fact.Key)
		}
	}
	if lessonKey == "" {
		t.Fatalf("business chain carries no delivered lesson: %+v", facts)
	}
	invokeAutonomyTurn(t, host, autonomyBusiness, "BUSINESS-RECALL-REQUEST read the lesson card")
	req := provider.await(t, func(req autonomyRequest) bool {
		return strings.Contains(req.text(), "BUSINESS-RECALL-REQUEST") && req.hasToolResult("call-get-lesson")
	})
	if !strings.Contains(req.text(), autonomyLesson) {
		t.Fatalf("business recall observation = %q, want the lesson card by key", req.text())
	}
}

// TestMeditationAutonomousTidyRun pins the autonomous tidy turn: the state machine fires and the reflection uses the ordinary file, exec and recall faces.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditationAutonomousTidyRun(t *testing.T) {
	requireTmux(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tidy-note.md"), []byte("TIDY-INPUT"), 0o644); err != nil {
		t.Fatalf("write the tidy input: %v", err)
	}
	round := 0
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if !req.reflection() {
			return autonomyAnswer("business answer")
		}
		if req.issued("exec_release") {
			return autonomyAnswer("TIDY-DONE")
		}
		if req.hasToolResult("call-exec") {
			if _, err := os.Stat(filepath.Join(root, "tidy-exec.txt")); err == nil {
				return autonomyCall("call-release", "exec_release", `{"kind":"`+tagentexec.KindTmux+`","resource":"tidy-1"}`)
			}
		}
		round++
		if round == 1 {
			return autonomyCalls(
				[3]string{"call-read", "read_file", `{"file_name":"tidy-note.md"}`},
				[3]string{"call-recall", "recall_recent", `{"limit":5}`},
				[3]string{"call-save", "save_file", `{"file_name":"tidy-output.md","contents":"` + autonomyTidy + `","overwrite":true}`},
				[3]string{"call-exec", "exec_run", `{"command":"sh","args":["-c","echo TIDY-EXEC > ` + root + `/tidy-exec.txt"],"kind":"` + tagentexec.KindTmux + `","resource":"tidy-1"}`},
			)
		}
		if round > 40 {
			return autonomyAnswer("TIDY-WAIT")
		}
		return autonomyCall(fmt.Sprintf("call-wait-%d", round), "recall_recent", `{"limit":1}`)
	})
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "read_file", "save_file", "exec", "recall_recent"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "an idle window for the autonomous tidy run")
	req := provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	for _, want := range []string{"read_file", "save_file", "exec_run", "recall_recent"} {
		if !req.toolNames()[want] {
			t.Errorf("tidy tool face carries no %q", want)
		}
	}
	facts := awaitSessionFacts(t, host, autonomyReflection, func(facts []event.Event) bool {
		for _, fact := range facts {
			if strings.Contains(fact.Message.Content, "TIDY-DONE") {
				return true
			}
		}
		return false
	})
	landed := false
	for _, fact := range facts {
		if strings.Contains(fact.Message.Content, "TIDY-DONE") {
			landed = true
		}
	}
	if !landed {
		t.Fatalf("reflection chain carries no tidy settlement: %+v", facts)
	}
	written, err := os.ReadFile(filepath.Join(root, "tidy-output.md"))
	if err != nil {
		t.Fatalf("read the tidy product: %v", err)
	}
	if strings.TrimSpace(string(written)) != autonomyTidy {
		t.Fatalf("tidy product = %q, want the file tool's ordinary product", written)
	}
	executed, err := os.ReadFile(filepath.Join(root, "tidy-exec.txt"))
	if err != nil {
		t.Fatalf("read the exec product: %v", err)
	}
	if strings.TrimSpace(string(executed)) != "TIDY-EXEC" {
		t.Fatalf("exec product = %q, want the ordinary exec face's output", executed)
	}
}

// TestMeditationMountingBehaviorEquivalent pins the mounting shape: both the self-observation and the curation declaration keep their meaning.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditationMountingBehaviorEquivalent(t *testing.T) {
	t.Run("the host self-observation keeps the section and reflects in its own session", func(t *testing.T) {
		provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
			if req.reflection() {
				return autonomyAnswer("self observation done")
			}
			return autonomyAnswer("business answer")
		})
		root := t.TempDir()
		configPath := writeAutonomyDocument(t, autonomyDocument{
			root:       root,
			endpoint:   server.URL,
			tools:      []string{"send", "recall_recent"},
			meditation: true,
			deliverTo:  []string{"tagent"},
		})
		host, _ := bootAutonomyHost(t, configPath)
		invokeAutonomyTurn(t, host, autonomyBusiness, "business turn for the mount")
		req := provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
		if !strings.Contains(req.text(), "[tagent]") {
			t.Fatalf("self observation input = %q, want the observing agent's own partition", req.text())
		}
		awaitSessionFacts(t, host, autonomyReflection, func(facts []event.Event) bool { return len(facts) >= 2 })
		if facts := host.Run().SessionFacts(event.Address{AgentID: "tagent", SessionID: autonomyCuration}); len(facts) != 0 {
			t.Fatalf("self observation wrote the curation session: %+v", facts)
		}
	})

	t.Run("the optional curation declaration keeps its separate session", func(t *testing.T) {
		provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
			if req.reflection() {
				return autonomyAnswer("curation observation done")
			}
			return autonomyAnswer("business answer")
		})
		root := t.TempDir()
		configPath := writeAutonomyDocument(t, autonomyDocument{
			root:     root,
			endpoint: server.URL,
			tools:    []string{"send", "recall_recent"},
			curator:  true,
		})
		host, _ := bootAutonomyHost(t, configPath)
		invokeAutonomyTurn(t, host, autonomyBusiness, "business turn for the curation mount")
		req := provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
		if !strings.Contains(req.text(), "[tagent]") {
			t.Fatalf("curation input = %q, want the observed partition", req.text())
		}
		curation := awaitSessionFactsOf(t, host, "meditator", autonomyCuration, func(facts []event.Event) bool { return len(facts) >= 2 })
		for _, fact := range curation {
			if fact.Owner.AgentID != "meditator" {
				t.Errorf("curation fact owner = %q, want the declaring agent", fact.Owner.AgentID)
			}
		}
		if facts := host.Run().SessionFacts(event.Address{AgentID: "tagent", SessionID: autonomyReflection}); len(facts) != 0 {
			t.Fatalf("curation declaration wrote the host reflection session: %+v", facts)
		}
	})
}

// TestMeditationProductsLandAsNormalActivity pins the product path: the reflection writes through the ordinary file face and ordinary frozen fact types.
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestMeditationProductsLandAsNormalActivity(t *testing.T) {
	provider, server := newAutonomyProvider(t, func(req autonomyRequest) string {
		if !req.reflection() {
			return autonomyAnswer("business answer")
		}
		if req.hasToolResult("call-product") {
			return autonomyAnswer("product recorded")
		}
		return autonomyCall("call-product", "save_file", `{"file_name":"reflection-product.md","contents":"PRODUCT-BODY","overwrite":true}`)
	})
	root := t.TempDir()
	configPath := writeAutonomyDocument(t, autonomyDocument{
		root:       root,
		endpoint:   server.URL,
		tools:      []string{"send", "save_file", "recall_recent"},
		meditation: true,
		deliverTo:  []string{"tagent"},
	})
	host, _ := bootAutonomyHost(t, configPath)
	invokeAutonomyTurn(t, host, autonomyBusiness, "business turn for the product")
	provider.await(t, func(req autonomyRequest) bool { return req.reflection() })
	awaitSessionFacts(t, host, autonomyReflection, func(facts []event.Event) bool {
		for _, fact := range facts {
			if strings.Contains(fact.Message.Content, "product recorded") {
				return true
			}
		}
		return false
	})
	product, err := os.ReadFile(filepath.Join(root, "reflection-product.md"))
	if err != nil {
		t.Fatalf("read the reflection product: %v", err)
	}
	if string(product) != "PRODUCT-BODY" {
		t.Fatalf("reflection product = %q, want the file tool's ordinary write", product)
	}
	facts := awaitSessionFacts(t, host, autonomyReflection, func(facts []event.Event) bool {
		return len(facts) >= 2
	})
	allowed := map[string]bool{
		event.TypeExternalInput: true,
		event.TypeAgentOutput:   true,
		event.TypeActionCommand: true,
		event.TypeThinkingPlan:  true,
		event.TypeTaskSettled:   true,
		event.TypeFeedback:      true,
		"delivery.settlement":   true,
	}
	for _, fact := range facts {
		if !allowed[fact.Type] {
			t.Errorf("reflection fact type = %q, want an ordinary frozen type", fact.Type)
		}
	}
}
