package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestHandoffRedrivesStalledOwedRecords pins the send face of the hand-off as every obligation that still stands owed, not only the bodies one call registered.
//   - a body whose owed declaration landed and whose send never happened is redriven by the next hand-off that meets it, and its obligation closes as sent
//   - a body the account already discharged is not sent a second time
//   - the turn commits its body under the notification's own invocation, and a host beside no outbound sender leaves the obligation owed without a settlement fact
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestHandoffRedrivesStalledOwedRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	addr := event.Address{AgentID: "agent-a", SessionID: "session-handoff-redrive"}
	notice := task.Notice{TaskID: "task-handoff-redrive", Round: 1, Kind: task.NoticeSettled, Seq: 1}
	if _, err := run.Invoke(ctx, InvokeRequest{
		InvocationID: noticeInvocation(notice),
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "answer the notification"}},
	}); err != nil {
		t.Fatalf("drive the follow-up turn: %v", err)
	}
	bodies := generatedBodiesOf(run.SessionFacts(addr), noticeInvocation(notice))
	if len(bodies) != 1 {
		t.Fatalf("bodies the follow-up turn generated = %+v, want the one answer", bodies)
	}
	obligation, ok := freshObligationOf(bodies[0])
	if !ok {
		t.Fatalf("the generated body names no delivery obligation: %+v", bodies[0])
	}
	host := &Host{run: run, entry: "agent-a"}
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off beside no sender: %v", err)
	}
	if settled := hostTestSettlements(t, host, addr); len(settled) != 0 {
		t.Fatalf("settlement facts beside no sender = %+v, want the obligation still owed (no settlement fact)", settled)
	}
	channel := &hostTestChannel{}
	host.sender = channel
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off beside a sender: %v", err)
	}
	if got := channel.bodies(); len(got) != 1 || got[0] != bodies[0].content {
		t.Fatalf("bodies the hand-off sent = %q, want the one stalled obligation %q", got, bodies[0].content)
	}
	settled := hostTestSettlements(t, host, addr)
	if len(settled) != 1 || settled[0].state != deliveryStateSent || settled[0].identity != obligation {
		t.Fatalf("settlement facts beside the redrive = %+v, want the one obligation %s closed as sent", settled, obligation)
	}
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off a third time: %v", err)
	}
	if got := channel.bodies(); len(got) != 1 {
		t.Fatalf("bodies after the discharged hand-off = %q, want the one body already sent", got)
	}
	if settled := hostTestSettlements(t, host, addr); len(settled) != 1 {
		t.Fatalf("settlement facts after the discharged hand-off = %+v, want the one receipt", settled)
	}
}

// TestDeliveredBodyNamesAnUnconfirmedReceipt pins the second result of a landed send whose receipt did not reach the account.
//   - positive: a send the channel accepted answers delivered beside a cause naming the missing receipt
//   - negative: the same send over a live account answers no cause at all
//   - the account is closed right behind the send, which is the window a stop between the two leaves
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveredBodyNamesAnUnconfirmedReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	host := &Host{run: run, entry: "agent-a"}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-unconfirmed-receipt"}
	unconfirmed := &hostTestChannel{}
	unconfirmedCtx, unconfirmedCancel := context.WithCancel(ctx)
	unconfirmed.afterSend = unconfirmedCancel
	outcome, cause := host.deliverObligation(unconfirmedCtx, "delivery-fresh-unconfirmed",
		unconfirmed, plugin.Delivery{Content: "the body nobody confirmed", Lineage: "task", Target: addr})
	if outcome != deliverySent {
		t.Fatalf("outcome of the landed send = %v, want the delivered outcome", outcome)
	}
	if !strings.Contains(cause, "the delivery landed but its receipt did not") {
		t.Fatalf("cause of the landed send = %q, want the note naming the unconfirmed receipt", cause)
	}
	confirmed := &hostTestChannel{}
	outcome, cause = host.deliverObligation(ctx, "delivery-fresh-confirmed",
		confirmed, plugin.Delivery{Content: "the body the account confirmed", Lineage: "task", Target: addr})
	if outcome != deliverySent || cause != "" {
		t.Fatalf("outcome and cause of the confirmed send = %v, %q, want a delivered send beside no cause", outcome, cause)
	}
	if settled := hostTestSettlements(t, host, addr); len(settled) != 1 || settled[0].state != deliveryStateSent || settled[0].identity != "delivery-fresh-confirmed" {
		t.Fatalf("settlement facts beside the two sends = %+v, want the one receipt the live account took", settled)
	}
}

// hostTestSettlements reads every settlement fact of one address's chain,
// ordered by fact key, so a check judges the account the chain states however
// the window answers.
func hostTestSettlements(t *testing.T, host *Host, addr event.Address) []deliverySettlement {
	t.Helper()
	if host == nil || host.run == nil {
		t.Fatalf("the check handed no assembled run to read settlement facts from")
	}
	facts := host.run.SessionFacts(addr)
	settled := make([]deliverySettlement, 0, len(facts))
	for _, fact := range facts {
		settlement, ok := settlementOf(fact)
		if !ok {
			continue
		}
		settled = append(settled, settlement)
	}
	sort.Slice(settled, func(i, j int) bool { return settled[i].fact.Key < settled[j].fact.Key })
	return settled
}

// hostTestChannel is the outbound face one check hands the host: it keeps every
// body it accepted and accepts all of them, so a redrive is counted rather than
// refused by the channel.
type hostTestChannel struct {
	// afterSend runs after one accepted body is recorded, so a check can close the
	// account behind a send that already landed.
	afterSend func()
	mu        sync.Mutex
	sent      []string
}

// Send records one accepted body and reports the delivery as landed.
func (c *hostTestChannel) Send(_ context.Context, _ event.Address, content string) error {
	c.mu.Lock()
	c.sent = append(c.sent, content)
	c.mu.Unlock()
	if c.afterSend != nil {
		c.afterSend()
	}
	return nil
}

// bodies reports the bodies the channel accepted, in the order it took them.
func (c *hostTestChannel) bodies() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}

// closeResidentsSession is the session the delegated round of the close check
// runs in, so its resident loop is one the run mounted for itself.
const closeResidentsSession = "session-close-residents"

// closeResidentsAdmit answers the admission receipt of the call it is handed and
// no result: the segment that issued it parks awaiting a settled result this
// check never lands, so the loop of that segment stands between turns while the
// close reads its criterion.
type closeResidentsAdmit struct {
	taskID   event.TaskID
	admitted chan struct{}
}

// Declaration returns the declaration naming the admitting tool.
func (a *closeResidentsAdmit) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "admit-close"}
}

// Call answers the admission receipt of the call it is handed.
func (a *closeResidentsAdmit) Call(ctx context.Context, _ []byte) (any, error) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return nil, task.ErrCallFrameMissing
	}
	select {
	case a.admitted <- struct{}{}:
	default:
	}
	return task.AdmissionReceipt{
		CallID:   frame.Identity.CallID,
		TaskID:   a.taskID,
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}, nil
}

// TestCloseStopsTheResidentsTheRunMounted pins the stop the close of a run owes the resident loops it mounted:
// - negative: the close never returns beside a segment a resident loop still flies, because a run at rest over a segment that lands afterwards is refused
// - positive: the stop reaches the segment the loop left flying, so the delegated round waiting on it answers the cancelled terminal instead of hanging
// - the run is cut, not drained: the round the stop left without an answer stays owed beside the chain it was committed on
// - the criterion reads that round off the chain, so the next process drives it again after the close answered
// - a restart reading that chain reads the input it still owes and no turn the close wrote behind its own answer
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestCloseStopsTheResidentsTheRunMounted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admit := &closeResidentsAdmit{taskID: "task-close-residents", admitted: make(chan struct{}, 4)}
	round := 0
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		round++
		if round == 1 {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{{
						ID:   "call-close-residents",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name:      "admit-close",
							Arguments: []byte(`{}`),
						},
					}},
				}}},
				Done: true,
			}
		}
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the worker answered"}}},
			Done:    true,
		}
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	natives := map[event.AgentID]NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{admit}},
	}
	run, err := Assemble(ctx, decl, natives, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	site := event.Address{AgentID: "agent-a", SessionID: closeResidentsSession}
	if err := run.establish(ctx, site); err != nil {
		t.Fatalf("establish the site of the delegated round: %v", err)
	}

	submitted := make(chan agent.DelegatedAnswer, 1)
	go func() {
		answer, submitErr := run.delegations.Submit(ctx, agent.DelegatedInput{
			Site:        site,
			Binding:     state.AgentBinding{Model: provider, Tools: []trpctool.Tool{admit}, PromptSource: state.PromptSource{Version: "v1"}},
			Instruction: "run the delegated round",
		})
		if submitErr != nil {
			submitted <- agent.DelegatedAnswer{Failure: submitErr.Error()}
			return
		}
		submitted <- answer
	}()
	select {
	case <-admit.admitted:
	case <-time.After(30 * time.Second):
		t.Fatal("the delegated round never reached its admission, want the resident loop to drive it to the call it parks on")
	}

	report, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("close the run beside the resident loop it mounted: %v", err)
	}
	if report.Drained {
		t.Fatal("the close of the run reported it drained, want the input round the stopped segment consumed and never answered still owed")
	}
	if report.Outcome != agent.CloseCut {
		t.Fatalf("the close of the run ended %v, want the cut the stopped segment left beside the round it consumed", report.Outcome)
	}
	if len(report.Unsettled) == 0 || report.Unsettled[0].FactKey == "" {
		t.Fatalf("the close of the run reported unsettled %+v, want the driving fact of the round it cut", report.Unsettled)
	}
	select {
	case answer := <-submitted:
		if answer.Status != task.StatusCancelled {
			t.Fatalf("the delegated round beside the close = (%+v, %q), want the cancelled terminal: its segment is closed by the stop, not left flying",
				answer, answer.Failure)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the close returned beside a resident loop it never stopped: the delegated round still waits on a segment nobody closes")
	}

	settled := run.SessionFacts(site)
	time.Sleep(200 * time.Millisecond)
	if after := run.SessionFacts(site); len(after) != len(settled) {
		t.Fatalf("facts of %s beside the close = %d, want the %d that stood when the close answered: a segment wrote behind its own answer",
			site.SessionID, len(after), len(settled))
	}
	for _, fe := range settled {
		if fe.Type == event.TypeAgentOutput {
			t.Fatalf("agent_output facts of %s beside the close = %+v, want none: the abandoned segment committed no turn",
				site.SessionID, fe)
		}
	}
}

// longHostSummaryAnswer is the gist the scripted endpoint answers a band summary
// with: longer than the declared budget, so a summary that carries it whole
// answers a bound other than the declared one.
var longHostSummaryAnswer = strings.Repeat("[10-20 08:00|gen:?] the settled line of the band\n", 60)

// hostSummaryEndpoint is the model face one booted host asks its band summaries
// over: it answers the long gist and records every request body it served, so a
// check reads what the fold asked rather than only what it filed.
type hostSummaryEndpoint struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

// newHostSummaryEndpoint starts the scripted endpoint over the declared answer.
func newHostSummaryEndpoint(t *testing.T) *hostSummaryEndpoint {
	t.Helper()
	endpoint := &hostSummaryEndpoint{}
	answer, err := json.Marshal(longHostSummaryAnswer)
	if err != nil {
		t.Fatalf("render the scripted answer: %v", err)
	}
	endpoint.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		endpoint.mu.Lock()
		endpoint.bodies = append(endpoint.bodies, string(body))
		endpoint.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"host-summary","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`, answer)
	}))
	t.Cleanup(endpoint.Close)
	return endpoint
}

// servedBodies reports the request bodies the endpoint served, in arrival order.
func (e *hostSummaryEndpoint) servedBodies() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.bodies...)
}

// commitHostSummaryTurns commits closed task turns under one booted run, letting
// the fold fire between commits as it does online.
func commitHostSummaryTurns(t *testing.T, ctx context.Context, run *Run, addr event.Address, turns int) {
	t.Helper()
	base := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	for turn := 0; turn < turns; turn++ {
		for position, leg := range []struct {
			position string
			typ      string
			summary  string
		}{
			{"0-in", event.TypeExternalInput, fmt.Sprintf("ask %02d", turn)},
			{"1-plan", event.TypeThinkingPlan, fmt.Sprintf("plan %02d", turn)},
			{"2-call", event.TypeActionCommand, fmt.Sprintf("call %02d", turn)},
			{"3-out", event.TypeAgentOutput, fmt.Sprintf("answer %02d", turn)},
		} {
			evt := event.Event{
				Key:       event.EventID(fmt.Sprintf("%02d-%s", turn, leg.position)),
				Type:      leg.typ,
				Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
				Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
				Message:   model.Message{Role: model.RoleUser, Content: hostSummaryBody(turn, leg.position)},
			}
			if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
				t.Fatalf("commit turn %d leg %s: %v", turn, leg.position, err)
			}
		}
	}
}

// hostSummaryBody reports the text one committed leg carries: long enough that a
// few turns fill a small declared window.
func hostSummaryBody(turn int, position string) string {
	return strings.Repeat(fmt.Sprintf("host 正文 %s %02d。", position, turn), 12)
}

// hostHistorySummaries reports the text of the history summaries the chain of one
// session holds, in commit order.
func hostHistorySummaries(run *Run, addr event.Address) []string {
	out := make([]string, 0, 4)
	for _, evt := range run.SessionFacts(addr) {
		if evt.Type != event.CompressProductType {
			continue
		}
		if !strings.HasPrefix(evt.Meta[metaKeyCompressKey], "-hist-") {
			continue
		}
		out = append(out, evt.Meta[metaKeyCompressText])
	}
	return out
}

// TestHostBootCompressionObeysTheDeclaredSummaryBudget anchors the declared budget over the host entry a deployment writes.
//   - declaration: the boot document names summary_budget_bytes, and the check patches no field of its own
//   - instruction: the band summary the host asks over that document states the declared bound
//   - product: the history summary the fold files on the chain carries at most that many bytes
//   - answer kept: the product carries the model answer rather than the mechanical form
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestHostBootCompressionObeysTheDeclaredSummaryBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if len(longHostSummaryAnswer) <= declaredHostSummaryBudget {
		t.Fatalf("the scripted answer carries %d bytes, want it longer than the bound the fold applies",
			len(longHostSummaryAnswer))
	}
	endpoint := newHostSummaryEndpoint(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	block := fmt.Sprintf("    replay_window: 60\n    summary_budget_bytes: %d\n", declaredHostSummaryBudget)
	host := bootGenerationHost(t, configPath, generationDocument(root, endpoint.URL+"/v1", block))
	run := host.Run()
	if run == nil {
		t.Fatal("the boot answered no run")
	}
	addr := event.Address{AgentID: "user", SessionID: "session-host-summary-budget"}
	commitHostSummaryTurns(t, ctx, run, addr, 8)
	summaries := hostHistorySummaries(run, addr)
	if len(summaries) == 0 {
		t.Fatal("the booted host filed no history summary, so the declared budget was never read")
	}
	stated := false
	for _, body := range endpoint.servedBodies() {
		if strings.Contains(body, fmt.Sprintf("within %d bytes", declaredHostSummaryBudget)) {
			stated = true
		}
	}
	for _, summary := range summaries {
		if len(summary) > declaredHostSummaryBudget {
			t.Fatalf("the history summary carries %d bytes, want at most the declared %d", len(summary), declaredHostSummaryBudget)
		}
		if !strings.Contains(summary, "the settled line of the band") {
			t.Fatalf("the history summary = %q, want the model answer cut to the declared bound rather than the mechanical form", summary)
		}
	}
	if !stated {
		t.Fatalf("no band summary request stated the declared %d-byte bound", declaredHostSummaryBudget)
	}
}

// deliveryFactObligationToolName names the one native call the settled
// notification of the check belongs to.
const deliveryFactObligationToolName = "echo-obligation"

// deliveryFactObligationAnswer is the body the scripted model speaks from the
// result the backflow handed on, so a turn that carries it drove the hand-over.
const deliveryFactObligationAnswer = "the held result arrived"

// TestDeliveryFactFailurePreservesTheNoticeObligation pins the obligation a refused delivery fact leaves standing.
//   - the settled call of one native tool commits the delivery fact of its range, so the refusal injected below is the refusal the sink reads
//   - the range drives the handed-on result in this process beside the refusal: the completed turn of two decisions is the hand-over that stands
//   - the refusal leaves the notification owed and writes no consumption receipt, because the mark the restart pairs the result with never landed
//   - the process after the restart restores the owed notification from the storage root the first left and hands it over once: one delivery fact and one receipt
//   - a second hand-over of that notification meets the settled seat, so the redelivery writes no second receipt
//   - the entry keeps the claim the hand-over took; it stood pending while the refusal handed the claim back, which let a pump here drive the notice twice
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveryFactFailurePreservesTheNoticeObligation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	root := t.TempDir()
	addr := deliveryTestAddress("session-delivery-fact-obligation")
	refusal := errors.New("the commit face refused the delivery fact of the settled call")
	tool := &deliveryFactObligationTool{}
	run := deliveryFactObligationRun(t, ctx, root, tool)

	var mu sync.Mutex
	refused := 0
	run.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		if evt.Type == event.TypeTaskSettled {
			mu.Lock()
			refused++
			mu.Unlock()
			return "", refusal
		}
		return run.commitFactChecked(ctx, evt)
	})

	turn, err := run.Invoke(ctx, InvokeRequest{
		InvocationID:  "inv-delivery-fact-obligation",
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "call the tool once"}},
	})
	if err != nil {
		t.Fatalf("invoke the run beside the refused delivery fact: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("the turn beside the refused delivery fact = (%v, %d decisions), want the completed turn of two decisions: the handed-on result drives the range beside the refusal",
			turn.Terminal, turn.DecisionCount)
	}
	mu.Lock()
	struck := refused
	mu.Unlock()
	if struck != 1 {
		t.Fatalf("the commit face refused %d delivery fact(s), want the one the settled call commits", struck)
	}
	if facts := deliveryFactsOf(run.SessionFacts(addr)); len(facts) != 0 {
		t.Fatalf("the chain holds %d delivery fact(s) beside the refusal, want none: %+v", len(facts), facts)
	}
	waitScopeGate(t, "the refused notification to stand owed under the claim this process took", func() bool {
		seats := run.store.Unconsumed()
		return len(seats) == 1 && seats[0].Claim == state.ClaimClaimed
	})
	owed := run.store.Unconsumed()[0]
	if keys := consumeReceiptKeys(t, ctx, run.tier); len(keys) != 0 {
		t.Fatalf("the root holds %d consumption receipt(s) beside the refused delivery fact, want none: the notification stays owed", len(keys))
	}
	if _, _, err := run.Checkpoint(ctx); err != nil {
		t.Fatalf("file the checkpoint of the process beside the owed notification: %v", err)
	}
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the process beside the owed notification: %v", err)
	}

	booted := deliveryFactObligationBoot(t, ctx, root, tool)
	restored := booted.store.Unconsumed()
	if len(restored) != 1 || restored[0].Notice != owed.Notice {
		t.Fatalf("the booted mailbox owes %+v, want the notification the refused delivery fact left owed %+v", restored, owed.Notice)
	}
	if keys := consumeReceiptKeys(t, ctx, booted.tier); len(keys) != 0 {
		t.Fatalf("the booted root holds %d consumption receipt(s), want none: the obligation crossed the restart", len(keys))
	}
	handover := task.WithCallFrame(context.WithoutCancel(ctx), tool.frameOf(t))
	envelope := task.Envelope{
		Notice:         restored[0].Notice,
		Target:         restored[0].Address,
		Source:         addr,
		TargetInstance: restored[0].Instance,
	}
	if acceptance, err := booted.deliver(handover, envelope); err != nil || acceptance != task.DeliveryAccepted {
		t.Fatalf("the process after the restart offered the owed notification and answered (%v, %v), want the accepted delivery that commits its settle fact", acceptance, err)
	}
	if facts := deliveryFactsOf(booted.SessionFacts(addr)); len(facts) != 1 {
		t.Fatalf("the booted chain holds %d delivery fact(s) after the hand-over, want the one the refusal left missing: %+v (chain=%v)",
			len(facts), facts, factTypesOf(booted.SessionFacts(addr)))
	}
	if turns := driveSettleTurns(t, booted, restored[0].Address); turns != 1 {
		t.Fatalf("the boot drove %d follow-up turn(s) for the notification the refusal left owed, want the one turn", turns)
	}
	if keys := consumeReceiptKeys(t, ctx, booted.tier); len(keys) != 1 {
		t.Fatalf("the booted root holds %d consumption receipt(s) after the handed-over notification settled, want one", len(keys))
	}
	if acceptance, err := booted.deliver(handover, envelope); err != nil || acceptance != task.DeliveryAccepted {
		t.Fatalf("the second offer of the same notification answered (%v, %v), want the accepted delivery of the fact the chain already holds", acceptance, err)
	}
	if turns := driveSettleTurns(t, booted, restored[0].Address); turns != 0 {
		t.Fatalf("the second offer drove %d follow-up turn(s) for a notification the root already settled, want none", turns)
	}
	if facts := deliveryFactsOf(booted.SessionFacts(addr)); len(facts) != 1 {
		t.Fatalf("the chain holds %d delivery fact(s) after the second offer, want the one the first wrote: %+v", len(facts), facts)
	}
	if keys := consumeReceiptKeys(t, ctx, booted.tier); len(keys) != 1 {
		t.Fatalf("the root holds %d consumption receipt(s) after the second offer, want the one the first wrote", len(keys))
	}
	if _, err := booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the process after the restart: %v", err)
	}
}

// deliveryFactObligationRun assembles one process over the storage root the
// restart of the check reads back: the scripted model calls the one native tool
// and speaks from the result the backflow hands on.
func deliveryFactObligationRun(t *testing.T, ctx context.Context, root string, tool *deliveryFactObligationTool) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(req *model.Request) *model.Response {
		if settledToolResultOf(req, deliveryFactObligationToolName) {
			return &model.Response{Choices: []model.Choice{{Message: model.Message{
				Role: model.RoleAssistant, Content: deliveryFactObligationAnswer}}}, Done: true}
		}
		return &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{
				ID:   "call-obligation",
				Type: "function",
				Function: model.FunctionDefinitionParam{
					Name:      deliveryFactObligationToolName,
					Arguments: []byte(`{}`),
				},
			}},
		}}}, Done: true}
	}}
	return deliveryFactObligationAssemble(t, ctx, root, provider, tool)
}

// deliveryFactObligationBoot assembles one more process over the same storage
// root, so every record it reads comes from the root the first process left.
func deliveryFactObligationBoot(t *testing.T, ctx context.Context, root string, tool *deliveryFactObligationTool) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: deliveryFactObligationAnswer}}}, Done: true}
	}}
	return deliveryFactObligationAssemble(t, ctx, root, provider, tool)
}

// deliveryFactObligationDeclaration names the one agent every process of the
// check assembles, so the run and the plugin seam mounted over it read the same
// declaration rather than two copies of one target set.
func deliveryFactObligationDeclaration() *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}},
	}
}

// deliveryFactObligationAssemble assembles one process over the storage root the
// check shares, beside the stand-in governance that answers every check pass.
func deliveryFactObligationAssemble(t *testing.T, ctx context.Context, root string, provider model.Model, tool *deliveryFactObligationTool) *Run {
	t.Helper()
	run, err := assemble(ctx, deliveryFactObligationDeclaration(), map[event.AgentID]NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, false,
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if err != nil {
		t.Fatalf("assemble the runtime over %s: %v", root, err)
	}
	return run
}

// settledToolResultOf reports whether one model request already carries the tool
// result of the named call, so the scripted model speaks from a result it read
// instead of from the call it issued.
func settledToolResultOf(req *model.Request, tool string) bool {
	for _, message := range req.Messages {
		if message.Role == model.RoleTool && message.ToolName == tool {
			return true
		}
	}
	return false
}

// deliveryFactsOf reports the delivery facts one chain carries: the record a
// restart pairs the handed-on result with.
func deliveryFactsOf(facts []event.Event) []event.Event {
	var out []event.Event
	for _, fact := range facts {
		if fact.Type == event.TypeTaskSettled {
			out = append(out, fact)
		}
	}
	return out
}

// deliveryFactObligationTool is the one native tool the settled call of the check
// calls: it keeps the call frame of its own execution, which is the frame the
// mailbox face reads, so the process after the restart hands the same
// notification over through the frame the first process was driven with.
type deliveryFactObligationTool struct {
	mu     sync.Mutex
	frames []task.CallFrame
}

// Declaration names the tool the scripted round calls.
func (t *deliveryFactObligationTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: deliveryFactObligationToolName}
}

// Call executes the call once, keeping its frame, and answers the output the
// result under test is handed on from.
func (t *deliveryFactObligationTool) Call(ctx context.Context, _ []byte) (any, error) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return nil, errors.New("the call of the check carries no frame")
	}
	t.mu.Lock()
	t.frames = append(t.frames, frame)
	t.mu.Unlock()
	return deliveryFactObligationAnswer, nil
}

// frameOf reports the call frame of the one execution the check drove.
func (t *deliveryFactObligationTool) frameOf(tb testing.TB) task.CallFrame {
	tb.Helper()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.frames) != 1 {
		tb.Fatalf("the tool executed %d time(s), want the one call the settled notification belongs to", len(t.frames))
	}
	return t.frames[0]
}

// noticeTurnsOf reports the follow-up turns one chain carries: the distinct
// notification invocations the settle pump drives, named by the notification's
// own identity rather than by a count this check keeps beside it.
func noticeTurnsOf(facts []event.Event) int {
	seen := map[event.InvocationID]bool{}
	for _, fact := range facts {
		if strings.HasPrefix(string(fact.InvocationID), "notice:") {
			seen[fact.InvocationID] = true
		}
	}
	return len(seen)
}

// factTypesOf reports the types of one chain, so a check names what it read.
func factTypesOf(facts []event.Event) []string {
	out := make([]string, 0, len(facts))
	for _, fact := range facts {
		out = append(out, fact.Type)
	}
	return out
}

// corruptTheLandedAttempt rewrites every attempt record one root holds with bytes
// no reader decodes, so a boot that reads the attempt of a notification whose turn
// already landed meets a record it cannot read.
func corruptTheLandedAttempt(t *testing.T, ctx context.Context, root string) int {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier beside the landed attempt: %v", err)
	}
	log := ledger.TierFactLog(tier)
	stored, err := log.Scan(ctx, "tagent/task/v1/attempt:")
	if err != nil {
		t.Fatalf("read the attempt records of the root: %v", err)
	}
	if len(stored) == 0 {
		t.Fatalf("the root states no attempt record, want the landed attempt this check makes unreadable")
	}
	for key := range stored {
		if err := log.Put(ctx, key, []byte("{the attempt record this reader cannot decode")); err != nil {
			t.Fatalf("write the unreadable attempt at %s: %v", key, err)
		}
	}
	if err := log.Sync(ctx); err != nil {
		t.Fatalf("complete the barrier over the unreadable attempt: %v", err)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("release the storage tier beside the unreadable attempt: %v", err)
	}
	return len(stored)
}

// TestUnreadableReceiptDoesNotDriveAgain pins the fence a restart reads over a notification whose turn landed beside an unreadable record.
//   - the fixture leaves a settled task past the cut whose attempt landed a completed terminal, then makes that attempt record undecodable
//   - a boot either refuses the unreadable account by name or closes the entry beside the terminal it already states; it never drives the turn twice
//   - the settle fact is readable beside the boot, so a turn that fired here would be the re-drive under test and not a missing fact
//   - a boot that refuses the account by name drives no turn, and the refusal still stands owed: the root holds no consumption receipt
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestUnreadableReceiptDoesNotDriveAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	root := t.TempDir()
	notice := redeliveredNoticeOf("unreadable-receipt")
	addr := stopBesideTheCut(t, ctx, root, notice, true)
	stateTheOwedNotification(t, ctx, root, notice)
	if corrupted := corruptTheLandedAttempt(t, ctx, root); corrupted == 0 {
		t.Fatal("the fixture corrupted no attempt record, so the restart reads no unreadable receipt")
	}

	booted, err := assembleTheRoot(ctx, root)
	if err != nil {
		if !strings.Contains(fmt.Sprint(err), "attempt") && !strings.Contains(fmt.Sprint(err), "task") {
			t.Fatalf("the boot beside an unreadable attempt record answered %v, want the refusal by the identity it could not read", err)
		}
		tier, openErr := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
		if openErr != nil {
			t.Fatalf("open the root the refused boot left: %v", openErr)
		}
		defer func() {
			if closeErr := tier.Close(); closeErr != nil {
				t.Fatalf("release the root the refused boot left: %v", closeErr)
			}
		}()
		receipts, scanErr := ledger.TierFactLog(tier).Scan(ctx, "tagent/task/v1/consume:")
		if scanErr != nil {
			t.Fatalf("read the consumption receipts of the refused root: %v", scanErr)
		}
		if len(receipts) != 0 {
			t.Fatalf("the root holds %d consumption receipt(s) beside the refused boot, want none: the notification stays owed", len(receipts))
		}
		t.Logf("the boot refused the unreadable attempt record and kept the notification owed: %v", err)
		return
	}
	defer func() {
		if _, closeErr := booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); closeErr != nil {
			t.Fatalf("close the booted process: %v", closeErr)
		}
	}()
	if _, held := seatOfTask(booted.store.Unconsumed(), notice.TaskID); !held {
		t.Fatalf("the booted mailbox owes %+v, want the notification whose attempt record a stop left behind", booted.store.Unconsumed())
	}
	if turns := driveSettleTurns(t, booted, addr); turns != 0 {
		t.Fatalf("the boot drove %d follow-up turn(s) for a notification whose attempt already landed a terminal, want none: the unreadable record states a turn that ran, not a blank seat",
			turns)
	}
}

// assembleTheRoot assembles one process over a storage root and hands the error
// back, so a check reads the refusal a boot answers instead of stopping there.
func assembleTheRoot(ctx context.Context, root string) (*Run, error) {
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	return assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}, {ID: "agent-b", Model: "model-a", ReplayWindow: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, false,
		WithGovernance(snapshotTestGovernance{}), redeliveredNoticeStorage(root))
}

// TestBackflowFailureRecoversWithoutManualSettle pins the obligation a refused delivery fact leaves for the next process to recover on its own.
//   - the settled call of one native tool commits the delivery fact of its range, and the commit face refuses it
//   - the process that follows reads the owed notification back from the root the refused one left
//   - the boot hands that notification over and closes it without the check building a driver, naming a commit face or writing a settle fact
//   - the pump is the one the builtin catalog mounts for the host, so the obligation reaches a turn by the recovery the product runs
//   - the refused hand-over keeps its claim with the process that took it, so the owed entry is no pending work that process reads again
//   - the product's own recovery runs here beside the commit face and the fire face the catalog hands the pump, and no driver of this check
//   - the declared scope is the document's own, and the gate waits for the loop of the owed address to rest, so the driver stops between drives
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestBackflowFailureRecoversWithoutManualSettle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	addr := deliveryTestAddress("session-backflow-recovery")
	refusal := errors.New("the commit face refused the delivery fact of the settled call")
	tool := &deliveryFactObligationTool{}
	run := deliveryFactObligationRun(t, ctx, root, tool)
	run.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		if evt.Type == event.TypeTaskSettled {
			return "", refusal
		}
		return run.commitFactChecked(ctx, evt)
	})
	if _, err := run.Invoke(ctx, InvokeRequest{
		InvocationID:  "inv-backflow-recovery",
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "call the tool once"}},
	}); err != nil {
		t.Fatalf("invoke the run beside the refused delivery fact: %v", err)
	}
	waitScopeGate(t, "the refused notification to stand owed under the claim this process took", func() bool {
		seats := run.store.Unconsumed()
		return len(seats) == 1 && seats[0].Claim == state.ClaimClaimed
	})
	if _, _, err := run.Checkpoint(ctx); err != nil {
		t.Fatalf("file the checkpoint of the process beside the owed notification: %v", err)
	}
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the process beside the owed notification: %v", err)
	}

	booted := deliveryFactObligationBoot(t, ctx, root, tool)
	defer func() {
		if _, err := booted.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
			t.Fatalf("close the process after the restart: %v", err)
		}
	}()
	restored := booted.store.Unconsumed()
	if len(restored) != 1 {
		t.Fatalf("the booted mailbox owes %+v, want the one notification the refused delivery fact left", restored)
	}
	host := deliveryTestHost(ctx, booted, nil)
	seam := &hostRunPluginSeam{
		run:  booted,
		host: func() *Host { return host },
		port: &hostMeditationPort{},
		decl: deliveryFactObligationDeclaration(),
	}
	if _, err := hostPlugins(seam); err != nil {
		t.Fatalf("mount the host plugins of the booted run: %v", err)
	}
	if seam.settle == nil {
		t.Fatal("the catalog mounted no settle driver: the assembly the host builds carries no recovery of the obligation a refused delivery fact leaves")
	}
	consumer := host.consumersOf().Ensure(restored[0].Address)
	seam.settle.Wake()
	waitScopeGate(t, "the catalog-assembled pump to drive the owed notification to rest", func() bool {
		return len(booted.store.Unconsumed()) == 0 && consumer.Resting()
	})
	seam.settle.Stop()
	facts := deliveryFactsOf(booted.SessionFacts(addr))
	receipts := consumeReceiptKeys(t, ctx, booted.tier)
	turns := noticeTurnsOf(booted.SessionFacts(addr))
	if len(facts) != 1 || len(receipts) != 1 || turns != 1 {
		t.Fatalf("the process after the restart answered %d delivery fact(s), %d receipt(s) and %d follow-up turn(s) with no driver of this check, want the one of each the recovery owes: the notification the refused write left stands owed over every restart",
			len(facts), len(receipts), turns)
	}
}

// contributedStateSlice is the consumer state a check contributes through the
// public option: it states a seal no committed fact carries beside the facts it
// folds, so a check tells a holder the checkpoint restored from one the chain
// replay alone rebuilt.
type contributedStateSlice struct {
	name string
	mu   sync.Mutex
	// seal is the state only the snapshot carries: no fact derives it, so a
	// holder that answers it states the cut that restored it.
	seal string
	// facts states one line per fact the holder folded, keyed by identity.
	facts map[event.EventID]string
	// restored counts the payloads the state seam handed back, and early records
	// whether a fact reached the holder before any payload did.
	restored int
	early    bool
	// folded names the facts the holder derived itself, so a check tells a bounded
	// boot that folds the tail from one that rebuilt the whole chain: a payload the
	// seam restored states facts the holder never derived, and counting them as
	// derived would read every boot as a whole-chain one.
	folded map[event.EventID]struct{}
}

// StateName reports the name the contributed slice is keyed by.
func (s *contributedStateSlice) StateName() string { return s.name }

// contributedSliceState is the payload one contributed slice states.
type contributedSliceState struct {
	Seal  string   `json:"seal"`
	Facts []string `json:"facts"`
}

// SnapshotState states the seal beside the folded facts in identity order, so the
// same account renders the same bytes.
func (s *contributedStateSlice) SnapshotState(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.facts))
	for key := range s.facts {
		keys = append(keys, string(key))
	}
	slices.Sort(keys)
	stated := make([]string, 0, len(keys))
	for _, key := range keys {
		stated = append(stated, key+"="+s.facts[event.EventID(key)])
	}
	return json.Marshal(contributedSliceState{Seal: s.seal, Facts: stated})
}

// RestoreState puts one stated payload back; the empty cut is the null state the
// seam states for a holder that states nothing.
func (s *contributedStateSlice) RestoreState(_ context.Context, payload []byte) error {
	if string(payload) == "null" {
		return nil
	}
	var stated contributedSliceState
	if err := json.Unmarshal(payload, &stated); err != nil {
		return fmt.Errorf("the contributed slice states no readable payload: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restored++
	s.seal = stated.Seal
	if s.facts == nil {
		s.facts = map[event.EventID]string{}
	}
	for _, line := range stated.Facts {
		at := strings.IndexByte(line, '=')
		if at <= 0 {
			return fmt.Errorf("the contributed slice states no entry in %q", line)
		}
		s.facts[event.EventID(line[:at])] = line[at+1:]
	}
	return nil
}

// Reduce folds one committed fact into the stated account.
func (s *contributedStateSlice) Reduce(_ context.Context, evt event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restored == 0 && len(s.facts) == 0 {
		s.early = true
	}
	if s.facts == nil {
		s.facts = map[event.EventID]string{}
	}
	if s.folded == nil {
		s.folded = map[event.EventID]struct{}{}
	}
	s.folded[evt.Key] = struct{}{}
	s.facts[evt.Key] = evt.Type + ":" + evt.Message.Content
	return nil
}

// derived reports the identities the holder folded itself, apart from the facts
// the seam restored into it.
func (s *contributedStateSlice) derived() map[event.EventID]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	folded := make(map[event.EventID]struct{}, len(s.folded))
	for key := range s.folded {
		folded[key] = struct{}{}
	}
	return folded
}

// stated reports the seal, the folded facts and what the seam handed back.
func (s *contributedStateSlice) stated() (string, map[event.EventID]string, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	facts := make(map[event.EventID]string, len(s.facts))
	for key, state := range s.facts {
		facts[key] = state
	}
	return s.seal, facts, s.restored, s.early
}

// TestBootMountsAContributedStateSliceAndRestoresIt pins the public state entry: a contributed slice mounts ahead of the restore and states its own cut.
//   - positive: the booted run names the contributed slice and its checkpoint states the seal only the snapshot carries
//   - positive: the restart hands that payload back before any fact of the tail folds, and the holder states every fact the live process derived
//   - negative: a boot that dropped the contribution answers none of the above, and refuses the cut by name instead
//   - consumer: the public Boot entry with Run.Checkpoint, never Assemble direct
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestBootMountsAContributedStateSliceAndRestoresIt(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(generationDocument(root, provider.URL+"/v1", "")), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	const sliceName = "assembly.contributed-state-slice"
	live := &contributedStateSlice{name: sliceName, seal: "seal-the-live-process-states"}
	first, err := Boot(configPath, WithStateSlice(live))
	if err != nil {
		t.Fatalf("boot the host beside the contributed state slice: %v", err)
	}
	first.Run().stopCheckpointFiler()
	if !slices.Contains(first.Run().StateRegistry().Names(), sliceName) {
		t.Fatalf("the booted run mounts the state slices %v, want the contributed %s among them", first.Run().StateRegistry().Names(), sliceName)
	}
	addr := event.Address{AgentID: "user", SessionID: "session-contributed-state-slice"}
	commitSessionTurns(t, ctx, first.Run(), addr, "contributed-state-slice", 3)
	key, filed, err := first.Run().Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the cut of the booted run = (%t, %v), want a filed checkpoint", filed, err)
	}
	if payload := slicePayloadOf(t, checkpointOfIdentity(t, ctx, first.Run(), key), sliceName); !strings.Contains(payload, live.seal) {
		t.Fatalf("the cut states %q for the contributed slice, want the seal %q only the snapshot carries", payload, live.seal)
	}
	commitSessionTurns(t, ctx, first.Run(), addr, "contributed-state-slice-tail", 1)
	liveSeal, liveFacts, _, _ := live.stated()
	tailed := 0
	for key := range liveFacts {
		if strings.HasPrefix(string(key), "contributed-state-slice-") {
			tailed++
		}
	}
	if tailed != 16 {
		t.Fatalf("the live holder folded %d session fact(s), want the twelve of the cut and the four of the tail", tailed)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatalf("close the process that contributed the slice: %v", err)
	}

	booted := &contributedStateSlice{name: sliceName}
	second, err := Boot(configPath, WithStateSlice(booted))
	if err != nil {
		t.Fatalf("boot the host beside the contribution and the cut: %v", err)
	}
	t.Cleanup(func() { closeLiveness(t, second) })
	bootedSeal, bootedFacts, restores, early := booted.stated()
	if bootedSeal != liveSeal {
		t.Fatalf("the restarted holder states the seal %q, want the %q the checkpoint carries: the cut never landed in the contributed slice", bootedSeal, liveSeal)
	}
	if restores == 0 {
		t.Fatal("the state seam handed the restarted holder no payload, want the cut restored into the contributed slice")
	}
	if early {
		t.Fatal("a fact reached the restarted holder before the checkpoint payload did, want the restore ahead of the tail fold")
	}
	if len(bootedFacts) != len(liveFacts) {
		t.Fatalf("the restarted holder states %d fact(s), want the %d the cut and the tail state", len(bootedFacts), len(liveFacts))
	}
	restoredSession := 0
	for key, want := range liveFacts {
		got, held := bootedFacts[key]
		if !held {
			t.Fatalf("the restarted holder states no fact %s the live process derived", key)
		}
		if got != want {
			t.Fatalf("the restarted holder derives %q for the fact %s, want the %q the live process derived", got, key, want)
		}
		if strings.HasPrefix(string(key), "contributed-state-slice-") {
			restoredSession++
		}
	}
	if restoredSession != tailed {
		t.Fatalf("the restarted holder states %d session fact(s), want the %d the cut and the tail state", restoredSession, tailed)
	}
}

// TestUncoveredContributedSliceReplaysTheWholeChain pins the boot of a contribution no restored cut states a slice of.
//   - fixture: one process commits six turns, files the cut, commits two turns past it and stops, so the chain holds 32 facts with 24 below the cut and 8 past it
//   - negative: a bounded replay alone would leave the holder the 8 facts past the cut and nothing of the 24 below it
//   - positive: the restarted holder derives all 32 facts of the chain and every line it states is the one that fact derives
//   - consumer: Assemble beside the public WithStateSlice option, the entry a caller contributes through, never a hand-driven rebuild
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestUncoveredContributedSliceReplaysTheWholeChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-uncovered-contribution"}
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: t.TempDir()})
	fileContributionChainWithCut(t, ctx, storage, addr)

	const sliceName = "assembly.uncovered-contribution"
	fresh := &contributedStateSlice{name: sliceName}
	booted := windowChainRun(t, ctx, storage, WithStateSlice(fresh))
	defer endRun(t, booted)
	if !slices.Contains(booted.StateRegistry().Names(), sliceName) {
		t.Fatalf("the booted run mounts the state slices %v, want the contributed %s among them", booted.StateRegistry().Names(), sliceName)
	}
	_, stated, restores, _ := fresh.stated()
	if restores != 0 {
		t.Fatalf("the returned cut handed the contributed slice %d payload(s), want none: the cut states no slice of it", restores)
	}
	chain := contributionChainKeys(addr)
	derived := fresh.derived()
	if len(derived) < len(chain) {
		t.Fatalf("the restarted holder derived %d fact(s), want the one it derives for every fact of the %d the chain holds: the boot folded a subset of the chain", len(derived), len(chain))
	}
	for _, key := range chain {
		if _, folded := derived[key]; !folded {
			t.Fatalf("the restarted holder derived no fact %s: the account the chain holds below the cut is missing from it", key)
		}
		want := derivationOf(t, ctx, booted, key)
		if got := stated[key]; got != want {
			t.Fatalf("the restarted holder states %q for the fact %s, want the %q that fact derives", got, key, want)
		}
	}
}

// TestCoveredContributedSliceKeepsTheBoundedBoot pins the boot of a contribution the restored cut states.
//   - fixture: the same chain, with the contributing holder mounted in the process that files the cut, so the cut carries its slice
//   - positive: the seam hands the payload back and the holder states every chain fact, the 24 below the cut from the payload and the 8 past it from the fold
//   - negative: the derivation count is the holder's own, so a boot that replayed the whole chain instead would derive all 32 and fail here
//   - consumer: Assemble beside the public WithStateSlice option, never a hand-driven rebuild
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCoveredContributedSliceKeepsTheBoundedBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-covered-contribution"}
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: t.TempDir()})
	const sliceName = "assembly.covered-contribution"
	fileContributionChainWithCut(t, ctx, storage, addr, WithStateSlice(&contributedStateSlice{name: sliceName}))

	fresh := &contributedStateSlice{name: sliceName}
	booted := windowChainRun(t, ctx, storage, WithStateSlice(fresh))
	defer endRun(t, booted)
	_, stated, restores, early := fresh.stated()
	if restores == 0 {
		t.Fatal("the state seam handed the restarted holder no payload, want the cut the source process filed into it")
	}
	if early {
		t.Fatal("a fact reached the restarted holder before the cut it states did")
	}
	chain := contributionChainKeys(addr)
	derived := fresh.derived()
	for _, key := range chain[len(chain)-8:] {
		if _, folded := derived[key]; !folded {
			t.Fatalf("the restarted holder derived no fact %s, want the 8 that landed past the cut", key)
		}
	}
	folded := 0
	for _, key := range chain {
		if _, held := derived[key]; held {
			folded++
		}
		if _, held := stated[key]; !held {
			t.Fatalf("the restarted holder states no fact %s, want the one the payload of its own cut carried", key)
		}
	}
	if folded == len(chain) {
		t.Fatalf("the restarted holder derived all %d facts of the chain, want the tail past the cut alone: a cut that states the slice keeps the bounded replay", len(chain))
	}
}

// fileContributionChainWithCut commits the chain both contribution checks restart
// over and ends the process that filed its cut: eight turns of four legs, the cut
// filed between the sixth and the seventh, so 24 facts stand below the cut and 8
// past it.
func fileContributionChainWithCut(t *testing.T, ctx context.Context, storage Option, addr event.Address, opts ...Option) {
	t.Helper()
	source := windowChainRun(t, ctx, append([]Option{storage}, opts...)...)
	source.stopCheckpointFiler()
	commitWindowTurns(t, ctx, source, addr, 6)
	if _, filed, err := source.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the cut of the source chain = (%t, %v), want a filed checkpoint", filed, err)
	}
	commitWindowTurnsFrom(t, ctx, source, addr, 6, 2)
	endRun(t, source)
}

// contributionChainKeys answers the identities the contribution fixture commits, in
// chain order: the denominators of both checks are the chain the tier holds, never
// the replay that read it.
func contributionChainKeys(addr event.Address) []event.EventID {
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	keys := make([]event.EventID, 0, 32)
	for turn := 0; turn < 8; turn++ {
		for position := 0; position < 4; position++ {
			keys = append(keys, event.EventID(timeStampKey(base, addr, turn, position)))
		}
	}
	return keys
}

// derivationOf reads one committed fact back from the tier and answers the line the
// contributed holder states for it, so a check compares the restarted state against
// the chain itself instead of against the replay that read it.
func derivationOf(t *testing.T, ctx context.Context, run *Run, key event.EventID) string {
	t.Helper()
	evt, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the committed fact %s back: %v", key, err)
	}
	return evt.Type + ":" + evt.Message.Content
}

// TestBootRefusesTwoStateSlicesOfOneName pins the named refusal of a repeated contribution: the assembly stops instead of overwriting the owned slice.
//   - negative: the boot answers an error naming the slice and the already-mounted state
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestBootRefusesTwoStateSlicesOfOneName(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(generationDocument(root, provider.URL+"/v1", "")), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	const sliceName = "assembly.contributed-state-slice-repeat"
	host, err := Boot(configPath,
		WithStateSlice(&contributedStateSlice{name: sliceName}),
		WithStateSlice(&contributedStateSlice{name: sliceName}))
	if err == nil {
		closeErr := host.Close(ctx)
		t.Fatalf("the boot over two contributions of %s answered success (close %v), want it refused by name", sliceName, closeErr)
	}
	if !strings.Contains(err.Error(), sliceName) || !strings.Contains(err.Error(), "already mounted") {
		t.Fatalf("the boot refusal states %q, want it to name the slice %s and the already-mounted state", err, sliceName)
	}
}

// TestBootRefusesAnOptionItCannotHonor pins the seam between the two entries: a face Boot builds itself is never dropped in silence.
//   - negative: WithCapture, WithEmbedder, WithMemoryPlan, WithReflectionAnchors, withBootConfigReconcile and withTaskTerminalTTL are refused by name
//   - negative: the refusal names the option and the Assemble entry that seats it, answers no host and leaves the storage the boot would have opened unopened
//   - positive: the same document boots beside a supported contribution, so the refusal is the option's and never the document's
//   - consumer: the public Boot entry beside the public options a caller holds, never Assemble direct
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestBootRefusesAnOptionItCannotHonor(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newGenerationTestProvider(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(generationDocument(root, provider.URL+"/v1", "")), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	for _, refused := range []struct {
		name   string
		option Option
	}{
		{name: "WithCapture", option: WithCapture(snapshotTestCapture(t))},
		{name: "WithEmbedder", option: WithEmbedder(embedder.NewHashEmbedder(0))},
		{name: "WithMemoryPlan", option: WithMemoryPlan(&MemoryPlan{})},
		{name: "WithReflectionAnchors", option: WithReflectionAnchors(filepath.Join(root, "anchors.json"))},
		{name: "withBootConfigReconcile", option: withBootConfigReconcile(func(context.Context, *Run, *config.Declaration, map[event.AgentID]NativeAgent) error {
			return nil
		})},
		{name: "withTaskTerminalTTL", option: withTaskTerminalTTL(time.Second)},
	} {
		host, err := Boot(configPath, refused.option)
		if err == nil {
			closeErr := host.Close(ctx)
			t.Fatalf("the boot beside %s answered success (close %v), want the option refused by name", refused.name, closeErr)
		}
		if host != nil {
			closeErr := host.Close(ctx)
			t.Fatalf("the boot beside %s answered the host %+v (close %v), want no host started", refused.name, host, closeErr)
		}
		if !strings.Contains(err.Error(), refused.name) || !strings.Contains(err.Error(), "Assemble") {
			t.Fatalf("the boot refusal states %q, want it to name %s and the Assemble entry that seats the face", err, refused.name)
		}
		if _, statErr := os.Stat(filepath.Join(root, "storage", hostApprovalsDir)); !os.IsNotExist(statErr) {
			t.Fatalf("the boot refused for %s opened %s (stat %v), want nothing of the boot to have run",
				refused.name, filepath.Join(root, "storage", hostApprovalsDir), statErr)
		}
	}

	const sliceName = "assembly.refused-option-control"
	control, err := Boot(configPath, WithStateSlice(&contributedStateSlice{name: sliceName}))
	if err != nil {
		t.Fatalf("the same document booted beside the supported contribution %s = %v, want success: the refusals above must be the options'", sliceName, err)
	}
	closeLiveness(t, control)
}

// TestContributedSliceNamedLikeAFrameworkFaceIsRefused pins the framework's reserved slice names.
//   - a contributed holder naming a framework slice is refused by name before any holder mounts
//   - the refusal covers a name only the framework reserves beside one the host mounts itself
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestContributedSliceNamedLikeAFrameworkFaceIsRefused(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"delivery", "governance"} {
		_, err := contributedSliceRun(ctx, name)
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("the contributed slice named %q = %v, want a refusal naming the reserved framework slice", name, err)
		}
	}
}

// contributedSliceRun assembles a run beside one contributed state slice of the given name.
func contributedSliceRun(ctx context.Context, name string) (*Run, error) {
	return Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}},
	}, map[event.AgentID]NativeAgent{}, WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
		WithStateSlice(&foldRecorder{name: name}))
}
