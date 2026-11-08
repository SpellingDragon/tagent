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

// reentryEntryAgent names the material the re-entry anchor is written in: one entry agent
// holding both the delegating tool and the communication tool, one reviewing agent whose
// delegated round settles, and the one body the communication tool carries back to it.
// - reentryReviewerFirst is the verdict the reviewing turn answers its first round with, so the terminal notification that carries it is what the entry turn reads
// - reentryBody is the body the send call delivers, and it must reach the reviewing agent as the input of its second round, never the outbound channel
// - reentryAbsentModel is the model reference a second delegation names without any agent behind it, so that delegated round fails and leaves a failed task behind
const (
	reentryEntryAgent    = "sender"
	reentryEntryModel    = "model-sender"
	reentryReviewerAgent = "reviewer"
	reentryReviewerModel = "model-reviewer"
	reentryAbsentModel   = "model-absent"
	reentrySession       = "session-send-reentry"
	reentryInvocation    = "inv-send-reentry"
	reentryToolSend      = "send"
	reentryToolSubagent  = "call_subagent"

	reentryCallReviewer    = "call-reentry-reviewer"
	reentryCallAbsent      = "call-reentry-absent"
	reentryCallSendFailed  = "call-reentry-send-failed"
	reentryCallSendNowhere = "call-reentry-send-nowhere"
	reentryCallSendSettle  = "call-reentry-send-settled"

	reentryRequest      = "reentry: review the change under way"
	reentryFirstRound   = "reentry: the first round settled"
	reentrySecondRound  = "reentry: the second round read the re-entry input"
	reentryBody         = "reentry: the change moved on, review it again"
	reentryTurnDone     = "reentry: the turn is done"
	reentryNowhereTask  = "task-no-host-holds"
	reentryReceiptOK    = "ok"
	reentryDeniedPrefix = "[send_denied] "
)

// reentryFailedDenial and reentryUnknownDenial are the refusal texts the entry turn reads
// back: the failed task answers the relaunch it is re-entered by, and an identity the task
// table holds no record of answers that identity, so each negative anchor asserts the case
// it named rather than any refusal.
const (
	reentryFailedDenial  = "the task is failed"
	reentryUnknownDenial = "holds no record of"
)

// reentryWindow bounds how long the anchor waits for a decision of the entry turn and
// for the second round the re-entry is expected to serve.
const reentryWindow = 20 * time.Second

// TestSendReentersAgentTask drives the re-entry face of the send tool through the formal host entry.
// - positive: a turn reads the terminal of the task it delegated and re-enters that very task with fresh input
// - positive: the send call naming the settled task is answered with the bare ok receipt
// - positive: the body it carried reaches the reviewing agent as the input of its second round
// - negative: the body never leaves through the outbound channel, because the call names a task
// - negative (denies_failed_task): a failed task is refused by name and keeps the one terminal it holds
// - negative (denies_unknown_task): an identity the task table holds no record of is refused by name
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestSendReentersAgentTask(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	provider := newReentryProvider()
	sender := &symptomSender{}
	host := reentryBoot(t, provider, sender)
	addr := event.Address{AgentID: reentryEntryAgent, SessionID: reentrySession}
	provider.watchTerminal(func(kind string) event.TaskID {
		return reentryTerminalTask(host.Run().SessionFacts(addr), kind)
	})

	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: reentryInvocation,
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: reentryRequest}},
	})
	if err != nil {
		t.Fatalf("invoke the turn that delegates the review: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the delegating invocation", turn.Terminal)
	}

	receipt, found := reentryAwaitReceipt(provider, reentryCallSendSettle, reentryWindow)
	if !found {
		t.Fatalf("the turn that reads the settled review never issued the send call:\n%s", symptomDescribe(provider.requests()))
	}
	if receipt != reentryReceiptOK {
		t.Fatalf("send{target:{task}} result = %q, want the bare %q of a re-entered task:\n%s",
			receipt, reentryReceiptOK, symptomDescribe(provider.requests()))
	}
	if round := reentryAwaitReviewerRound(provider, reentryWindow); round == "" {
		t.Fatalf("the re-entered reviewing task served no second round carrying %q:\n%s",
			reentryBody, symptomDescribe(provider.requests()))
	}
	for _, body := range sender.accepted() {
		if strings.Contains(body, reentryBody) {
			t.Fatalf("the re-entry body left through the outbound channel: %q", body)
		}
	}

	t.Run("denies_failed_task", func(t *testing.T) {
		refused, found := reentryAwaitReceipt(provider, reentryCallSendFailed, reentryWindow)
		if !found {
			t.Fatalf("the entry turn never sent to the failed task:\n%s", symptomDescribe(provider.requests()))
		}
		if !strings.HasPrefix(refused, reentryDeniedPrefix) || !strings.Contains(refused, reentryFailedDenial) {
			t.Fatalf("send{target:{task}} of a failed task result = %q, want the named refusal beside the relaunch that answers it", refused)
		}
		if !strings.Contains(refused, "relaunch") {
			t.Fatalf("the refusal of a failed task = %q, want it to name the relaunch that answers it", refused)
		}
		failed := reentryTerminalTask(host.Run().SessionFacts(addr), "failed")
		if failed == "" {
			t.Fatalf("the delegated round that cannot resolve its model left no failed task in the chain")
		}
		if terminals := reentryTerminalCount(host.Run().SessionFacts(addr), failed); terminals != 1 {
			t.Fatalf("terminal notifications of the failed task = %d, want the one it settled under: a refused re-entry opens no round", terminals)
		}
		reentryAssertRefusalFiled(t, host, failed, addr)
	})

	t.Run("denies_unknown_task", func(t *testing.T) {
		refused, found := reentryAwaitReceipt(provider, reentryCallSendNowhere, reentryWindow)
		if !found {
			t.Fatalf("the entry turn never sent to an unknown task:\n%s", symptomDescribe(provider.requests()))
		}
		if !strings.HasPrefix(refused, reentryDeniedPrefix) || !strings.Contains(refused, reentryUnknownDenial) {
			t.Fatalf("send{target:{task}} of an unknown task result = %q, want the named refusal of the identity", refused)
		}
		if !strings.Contains(refused, reentryNowhereTask) {
			t.Fatalf("the refusal of an unknown task = %q, want it to name the identity it refused", refused)
		}
		reentryAssertRefusalFiled(t, host, event.TaskID(reentryNowhereTask), addr)
	})
}

// reentryRefusalStage is the stage of the frozen denial vocabulary a re-entry of a task is filed
// under: the task table refusing the identity the call named.
const reentryRefusalStage = "task"

// reentryAssertRefusalFiled reads the refusal of one re-entry back from the delivery account of the
// run: the sentence the model was answered with is not the account, so a body that reached no round
// is a record the run keeps and a reader queries afterwards.
func reentryAssertRefusalFiled(t *testing.T, host *assembly.Host, taskID event.TaskID, addr event.Address) {
	t.Helper()
	refusals := deliveryRefusals(t, host.Run().DeliveryLedger())
	for _, refusal := range refusals {
		if refusal.Receipt != assembly.DeliveryReceiptDenied {
			continue
		}
		if !strings.HasSuffix(refusal.Identity, "\x00"+string(taskID)) {
			continue
		}
		if refusal.Target != addr {
			t.Fatalf("target of the refusal of %s = %+v, want the session that asked for it %+v", taskID, refusal.Target, addr)
		}
		if refusal.Category != reentryRefusalStage {
			t.Fatalf("stage of the refusal of %s = %q, want %q", taskID, refusal.Category, reentryRefusalStage)
		}
		return
	}
	t.Fatalf("the account files no refusal of %s, so the refused body lives only in the text the model read: %s",
		taskID, reentryDescribeRefusals(refusals))
}

// reentryDescribeRefusals renders the refusals one account answers as one line each, so a failed read
// names what the account holds instead of the record that was missing.
func reentryDescribeRefusals(refusals []assembly.DeliveryRefusal) string {
	if len(refusals) == 0 {
		return "[]"
	}
	lines := make([]string, 0, len(refusals))
	for _, refusal := range refusals {
		lines = append(lines, fmt.Sprintf("%s -> %s/%s identity=%q receipt=%s category=%s",
			refusal.Source, refusal.Target.AgentID, refusal.Target.SessionID,
			refusal.Identity, refusal.Receipt, refusal.Category))
	}
	return strings.Join(lines, "; ")
}

// reentryProvider is the OpenAI-compatible endpoint the anchor boots against: it scripts
// the delegating decisions, the three send decisions and the reviewing turn's own
// answers off the request it serves, so the flow is sequenced by the material the run
// really carries rather than by an arrival order the anchor cannot pin.
type reentryProvider struct {
	server *httptest.Server

	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
	issued map[string]bool
	// terminal reports the task identity the last terminal notification of one kind in
	// the session chain belongs to. The receipts the model reads are the three result
	// texts, so the anchor reads the identities off the fact chain the run holds.
	terminal func(kind string) event.TaskID
}

// newReentryProvider starts the endpoint and closes it with the test.
func newReentryProvider() *reentryProvider {
	provider := &reentryProvider{issued: map[string]bool{}}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	return provider
}

// watchTerminal attaches the read face the send decisions take the task identities from.
func (p *reentryProvider) watchTerminal(read func(kind string) event.TaskID) {
	p.mu.Lock()
	p.terminal = read
	p.mu.Unlock()
}

// close ends the endpoint the host was booted against.
func (p *reentryProvider) close() { p.server.Close() }

// endpoint reports the base URL the host document's provider entry names.
func (p *reentryProvider) endpoint() string { return p.server.URL + "/v1" }

// serve answers one chat completion and records the request it carried.
func (p *reentryProvider) serve(w http.ResponseWriter, r *http.Request) {
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
	terminal := p.terminal
	p.mu.Unlock()

	payload := p.answer(intakeDecodeRequest(index, modelName, body), terminal)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, payload)
}

// answer scripts one decision off the request: the entry turn delegates the review and
// a round whose model no agent answers, then reads the terminal of both and issues the
// three send decisions the anchors read; the reviewing turn answers its first round
// beside the second round the re-entry opens.
func (p *reentryProvider) answer(request intakeRequest, terminal func(kind string) event.TaskID) string {
	if request.Model == reentryReviewerModel {
		if symptomCarries(request, reentryBody) {
			return intakeFinalAnswer(reentrySecondRound)
		}
		return intakeFinalAnswer(reentryFirstRound)
	}
	delegation := func(call, name, modelName string) string {
		return intakeToolCallAnswer(reentryToolSubagent, call, `{"name":`+intakeQuoted(name)+
			`,"model":`+intakeQuoted(modelName)+`,"prompt":`+intakeQuoted(reentryRequest)+`}`)
	}
	send := func(call, taskID string) string {
		return intakeToolCallAnswer(reentryToolSend, call, `{"target":{"task":`+intakeQuoted(taskID)+
			`},"content":`+intakeQuoted(reentryBody)+`}`)
	}
	settled, failed := "", ""
	if terminal != nil {
		settled, failed = string(terminal("settled")), string(terminal("failed"))
	}
	switch {
	case p.claim(reentryCallReviewer):
		return delegation(reentryCallReviewer, reentryReviewerAgent, reentryReviewerModel)
	case p.claim(reentryCallAbsent):
		return delegation(reentryCallAbsent, "absent", reentryAbsentModel)
	case failed != "" && p.claim(reentryCallSendFailed):
		return send(reentryCallSendFailed, failed)
	case symptomCarries(request, reentryFailedDenial) && p.claim(reentryCallSendNowhere):
		return send(reentryCallSendNowhere, reentryNowhereTask)
	case symptomCarries(request, reentryUnknownDenial) && settled != "" && p.claim(reentryCallSendSettle):
		return send(reentryCallSendSettle, settled)
	}
	return intakeFinalAnswer(reentryTurnDone)
}

// claim reports whether the named call is this request's to issue, exactly once.
func (p *reentryProvider) claim(call string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.issued[call] {
		return false
	}
	p.issued[call] = true
	return true
}

// requests reports the requests the endpoint served, in request order.
func (p *reentryProvider) requests() []intakeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]intakeRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, intakeDecodeRequest(index, p.models[index], body))
	}
	return out
}

// reentryTerminalTask reports the task the last terminal notification of one kind in a
// session chain belongs to: the settled or the failed notification the entry turn reads.
func reentryTerminalTask(facts []event.Event, kind string) event.TaskID {
	for at := len(facts) - 1; at >= 0; at-- {
		if facts[at].Type != event.TypeTaskSettled || facts[at].TaskID == "" {
			continue
		}
		if facts[at].Meta[event.MetaKeyNoticeKind] != kind {
			continue
		}
		return facts[at].TaskID
	}
	return ""
}

// reentryTerminalCount counts the terminal notifications one task holds in a session
// chain, so a task that was re-entered again is told apart from one that kept the
// single terminal it settled under.
func reentryTerminalCount(facts []event.Event, taskID event.TaskID) int {
	count := 0
	for _, fact := range facts {
		if fact.Type == event.TypeTaskSettled && fact.TaskID == taskID {
			count++
		}
	}
	return count
}

// reentryAwaitReceipt waits for the result the model read beside one call and reports
// it, or reports that no decision issued the call at all.
func reentryAwaitReceipt(provider *reentryProvider, callID string, window time.Duration) (string, bool) {
	deadline := time.Now().Add(window)
	for {
		if receipt, ok := symptomReceipt(provider.requests(), callID); ok {
			return receipt, true
		}
		if !time.Now().Before(deadline) {
			return "", false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// reentryAwaitReviewerRound waits for a reviewing request that carries the re-entry body
// and reports the material it was served, or reports that no second round ran.
func reentryAwaitReviewerRound(provider *reentryProvider, window time.Duration) string {
	deadline := time.Now().Add(window)
	for {
		for _, request := range provider.requests() {
			if request.Model == reentryReviewerModel && symptomCarries(request, reentryBody) {
				return reentryBody
			}
		}
		if !time.Now().Before(deadline) {
			return ""
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// reentryHostDocument renders the document the anchor boots: one entry agent holding both
// the delegating tool and the communication tool, beside the reviewing agent a
// delegation names as its model.
func reentryHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: " + reentryEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + reentryEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + reentryEntryAgent + ":\n")
	b.WriteString("    model: " + reentryEntryModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + reentryToolSend + " }\n")
	b.WriteString("      - { kind: tool, id: " + reentryToolSubagent + " }\n")
	b.WriteString("  " + reentryReviewerAgent + ":\n")
	b.WriteString("    model: " + reentryReviewerModel + "\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// reentryBoot boots the formal host the anchor drives, so every decision below reaches
// the runtime through the public entry the deployment uses.
func reentryBoot(t *testing.T, provider *reentryProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	t.Cleanup(provider.close)
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(reentryHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the send re-entry host: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the send re-entry host ended its close beside: %v", err)
		}
	})
	return host
}
