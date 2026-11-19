package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
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
	waitScopeGate(t, "the refused notification to stand owed on the mailbox", func() bool {
		seats := run.store.Unconsumed()
		return len(seats) == 1 && seats[0].Claim == state.ClaimPending
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

// deliveryFactObligationAssemble assembles one process over the storage root the
// check shares, beside the stand-in governance that answers every check pass.
func deliveryFactObligationAssemble(t *testing.T, ctx context.Context, root string, provider model.Model, tool *deliveryFactObligationTool) *Run {
	t.Helper()
	run, err := assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}},
	}, map[event.AgentID]NativeAgent{
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

// factTypesOf reports the types of one chain, so a check names what it read.
func factTypesOf(facts []event.Event) []string {
	out := make([]string, 0, len(facts))
	for _, fact := range facts {
		out = append(out, fact.Type)
	}
	return out
}
