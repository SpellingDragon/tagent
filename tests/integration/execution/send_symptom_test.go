package execution

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
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// symptomEntryAgent names the material the symptom anchor is written in: one entry agent
// holding both the delegating tool and the communication tool, one reviewing agent the
// delegated turn runs on, and the one body the communication tool carries back to it.
// - symptomFirstRound is the verdict the reviewing turn answers its first round with, so the terminal notification it carries is what the entry turn reads back
// - symptomReentryBody is the body the send call delivers, and it must reach the reviewing agent as the input of its second round, never the outbound channel
const (
	symptomEntryAgent    = "user"
	symptomEntryModel    = "model-entry"
	symptomReviewerAgent = "reviewer"
	symptomReviewerModel = "model-reviewer"
	symptomSession       = "session-send-symptom"
	symptomInvocation    = "inv-send-symptom"
	symptomCallSubagent  = "call-symptom-subagent"
	symptomCallSend      = "call-symptom-send"
	symptomRequest       = "symptom: review the change under way"
	symptomFirstRound    = "symptom: the first round settled"
	symptomSecondRound   = "symptom: the second round read the re-entry input"
	symptomReentryBody   = "symptom: the change moved on, review it again"
	symptomSettledAbsent = "symptom: the settle the send names carries no task identity"
	symptomTurnDone      = "symptom: the turn is done"
	symptomReceiptOK     = "ok"
)

// symptomBootWindow and symptomReentryWindow bound how long the anchor waits: the boot
// for the turn that reads the settled notification, and the re-entry for the second round
// it is expected to serve.
const (
	symptomBootWindow    = 30 * time.Second
	symptomReentryWindow = 15 * time.Second
)

// TestSendSymptomReentersAgentTask drives the one symptom of the send face: a turn re-enters the reviewing task it delegated with fresh input.
// - positive: the turn reads the terminal of the reviewing task it delegated, and calls the communication tool to re-enter that very task
// - positive: the send call naming the settled task is answered with the bare ok receipt
// - positive: the body it carried reaches the reviewing agent as the input of its second round
// - negative: the body never leaves through the outbound channel, because the call names a task
// - negative: the turn that issues the call keeps deciding after the receipt, so the re-entry never ends it
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestSendSymptomReentersAgentTask(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := newSymptomProvider()
	sender := &symptomSender{}
	host := symptomBoot(t, provider, sender)
	addr := event.Address{AgentID: symptomEntryAgent, SessionID: symptomSession}
	provider.watchSettledTask(func() event.TaskID { return symptomSettledTask(host.Run().SessionFacts(addr)) })

	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: symptomInvocation,
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: symptomRequest}},
	})
	if err != nil {
		t.Fatalf("invoke the turn that delegates the review: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the delegating invocation", turn.Terminal)
	}

	receipt, found := symptomAwaitSendReceipt(t, provider, symptomReentryWindow)
	if !found {
		t.Fatalf("the turn that reads the settled review never issued the send call:\n%s", symptomDescribe(provider.requests()))
	}
	if receipt != symptomReceiptOK {
		t.Fatalf("send{target:{task}} result = %q, want the bare %q of a re-entered task:\n%s",
			receipt, symptomReceiptOK, symptomDescribe(provider.requests()))
	}
	if round := symptomAwaitReviewerRound(t, provider, symptomReentryWindow); round == "" {
		t.Fatalf("the re-entered reviewing task served no second round carrying %q:\n%s",
			symptomReentryBody, symptomDescribe(provider.requests()))
	}
	for _, body := range sender.accepted() {
		if strings.Contains(body, symptomReentryBody) {
			t.Fatalf("the re-entry body left through the outbound channel: %q", body)
		}
	}
}

// symptomProvider is the OpenAI-compatible endpoint the anchor boots against: it scripts
// the delegating decision, the re-entry decision and the reviewing turn's own answers off
// the request it serves, so the flow is sequenced by the material the run really carries
// rather than by an arrival order the anchor cannot pin.
type symptomProvider struct {
	server *httptest.Server

	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
	issued map[string]bool
	// settledTask reports the task identity the terminal notification in the session
	// chain belongs to. The receipt the model reads is a bare ok, so the anchor reads
	// the identity off the fact chain the run really holds.
	settledTask func() event.TaskID
}

// newSymptomProvider starts the endpoint and closes it with the test.
func newSymptomProvider() *symptomProvider {
	provider := &symptomProvider{issued: map[string]bool{}}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	provider.server.Config.RegisterOnShutdown(func() {})
	return provider
}

// watchSettledTask attaches the read face the re-entry decision takes the task identity
// from, and closes the endpoint with the test.
func (p *symptomProvider) watchSettledTask(read func() event.TaskID) {
	p.mu.Lock()
	p.settledTask = read
	p.mu.Unlock()
}

// close ends the endpoint the host was booted against.
func (p *symptomProvider) close() { p.server.Close() }

// endpoint reports the base URL the host document's provider entry names.
func (p *symptomProvider) endpoint() string { return p.server.URL + "/v1" }

// serve answers one chat completion and records the request it carried.
func (p *symptomProvider) serve(w http.ResponseWriter, r *http.Request) {
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
	settledTask := p.settledTask
	p.mu.Unlock()

	payload := p.answer(intakeDecodeRequest(index, modelName, body), settledTask)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, payload)
}

// answer scripts one decision off the request: the entry turn delegates once, then reads
// the settled review and re-enters that task once, and the reviewing turn answers its
// first round beside the second round the re-entry opens.
func (p *symptomProvider) answer(request intakeRequest, settledTask func() event.TaskID) string {
	if request.Model == symptomReviewerModel {
		if symptomCarries(request, symptomReentryBody) {
			return intakeFinalAnswer(symptomSecondRound)
		}
		return intakeFinalAnswer(symptomFirstRound)
	}
	if p.claim(symptomCallSubagent) {
		return intakeToolCallAnswer(symptomToolSubagent, symptomCallSubagent, `{"name":`+intakeQuoted(symptomReviewerAgent)+
			`,"model":`+intakeQuoted(symptomReviewerModel)+`,"prompt":`+intakeQuoted(symptomRequest)+`}`)
	}
	if !symptomCarries(request, symptomFirstRound) {
		return intakeFinalAnswer(symptomTurnDone)
	}
	if p.claim(symptomCallSend) {
		taskID := symptomAwaitTask(settledTask, 5*time.Second)
		if taskID == "" {
			return intakeFinalAnswer(symptomSettledAbsent)
		}
		return intakeToolCallAnswer(symptomToolSend, symptomCallSend, `{"target":{"task":`+intakeQuoted(string(taskID))+
			`},"content":`+intakeQuoted(symptomReentryBody)+`}`)
	}
	return intakeFinalAnswer(symptomTurnDone)
}

// claim reports whether the named call is this request's to issue, exactly once.
func (p *symptomProvider) claim(call string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.issued[call] {
		return false
	}
	p.issued[call] = true
	return true
}

// requests reports the requests the endpoint served, in request order.
func (p *symptomProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// symptomToolSubagent and symptomToolSend are the declaration-side names of the two tools
// the entry agent holds.
const (
	symptomToolSubagent = "call_subagent"
	symptomToolSend     = "send"
)

// symptomCarries reports whether one request carries the named material in any message.
func symptomCarries(request intakeRequest, material string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, material) {
			return true
		}
	}
	return false
}

// symptomAwaitTask waits for the task identity the settle read reports, bounded so a run
// that committed no settle answers the named gap instead of holding the request.
func symptomAwaitTask(read func() event.TaskID, window time.Duration) event.TaskID {
	if read == nil {
		return ""
	}
	deadline := time.Now().Add(window)
	for {
		if taskID := read(); taskID != "" {
			return taskID
		}
		if !time.Now().Before(deadline) {
			return ""
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// symptomSettledTask reports the task the settled notification that drives the turn
// belongs to: the delegated review, whose terminal carries the round result the entry
// turn reads. The task the registration wrapper admits for the same native call settles
// beside it and carries no result, so the body tells the two admissions apart.
func symptomSettledTask(facts []event.Event) event.TaskID {
	for at := len(facts) - 1; at >= 0; at-- {
		if facts[at].Type != event.TypeTaskSettled || facts[at].TaskID == "" {
			continue
		}
		if !strings.Contains(facts[at].Message.Content, symptomFirstRound) {
			continue
		}
		return facts[at].TaskID
	}
	return ""
}

// symptomAwaitSendReceipt waits for the result the model read beside the send call and
// reports it, or reports that no decision issued the call at all.
func symptomAwaitSendReceipt(t *testing.T, provider *symptomProvider, window time.Duration) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(window)
	for {
		for _, request := range provider.requests() {
			if receipt, ok := symptomReceipt(request, symptomCallSend); ok {
				return receipt, true
			}
		}
		if !time.Now().Before(deadline) {
			return "", false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// symptomReceipt reports the result text the model saw beside one call: the message the
// call is answered in carries the call identity, and the result text itself follows it.
func symptomReceipt(request intakeRequest, callID string) (string, bool) {
	for index, message := range request.Messages {
		if message.ToolCallID != callID {
			continue
		}
		for _, later := range request.Messages[index+1:] {
			if later.Role == string(model.RoleUser) {
				return later.Content, true
			}
		}
		return message.Content, true
	}
	return "", false
}

// symptomAwaitReviewerRound waits for a reviewing request that carries the re-entry body
// and reports the answer it was served, or reports that no second round ran.
func symptomAwaitReviewerRound(t *testing.T, provider *symptomProvider, window time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(window)
	for {
		for _, request := range provider.requests() {
			if request.Model == symptomReviewerModel && symptomCarries(request, symptomReentryBody) {
				return symptomReentryBody
			}
		}
		if !time.Now().Before(deadline) {
			return ""
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// symptomSender is the outbound face of the booted host: it records every body it
// accepted, so the anchor reads whether a re-entry body ever left as a message.
type symptomSender struct {
	mu     sync.Mutex
	bodies []string
}

// Send records one accepted body.
func (s *symptomSender) Send(_ context.Context, _ event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, content)
	return nil
}

// accepted reports the bodies the outbound face accepted so far, in send order.
func (s *symptomSender) accepted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

// symptomHostDocument renders the document the anchor boots: one entry agent holding both
// the delegating tool and the communication tool, beside the reviewing agent that tool
// declares as its model.
func symptomHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + symptomEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + symptomEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + symptomEntryAgent + ":\n")
	b.WriteString("    model: " + symptomEntryModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + symptomToolSend + " }\n")
	b.WriteString("      - { kind: tool, id: " + symptomToolSubagent + " }\n")
	b.WriteString("  " + symptomReviewerAgent + ":\n")
	b.WriteString("    model: " + symptomReviewerModel + "\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// symptomBoot boots the formal host the anchor drives, so every decision below reaches
// the runtime through the public entry the deployment uses.
func symptomBoot(t *testing.T, provider *symptomProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	t.Cleanup(provider.close)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(symptomHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the send symptom host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the send symptom host ended its close beside: %v", err)
		}
	})
	return host
}

// symptomDescribe renders the requests the anchor reads its evidence from, so a failure
// states what the model really saw instead of the shape the anchor meant to drive.
func symptomDescribe(requests []intakeRequest) string {
	described := make([]string, 0, len(requests))
	for _, request := range requests {
		messages := make([]string, 0, len(request.Messages))
		for _, message := range request.Messages {
			label := message.Role
			if message.ToolCallID != "" {
				label += "(" + message.ToolCallID + ")"
			}
			messages = append(messages, label+"="+intakeQuoted(message.Content))
		}
		described = append(described, fmt.Sprintf("#%d model=%q messages=[%s]", request.Index, request.Model, strings.Join(messages, ", ")))
	}
	return "[" + strings.Join(described, "\n") + "]"
}
