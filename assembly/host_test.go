package assembly

import (
	"context"
	"encoding/json"
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
//   - a body recorded owed whose send never happened is redriven by the next hand-off that meets it, and its record closes as sent
//   - a body the account already discharged is not sent a second time
//   - the turn commits its body under the notification's own invocation, and a host beside no outbound sender records the obligation without sending
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
	host := &Host{run: run, entry: "agent-a", pendingDeliveries: map[string]*heldDelivery{}}
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off beside no sender: %v", err)
	}
	stalled := hostTestDeliveryRecords(t, ctx, host)
	if len(stalled) != 1 || stalled[0].Status != hostDeliveryOwed {
		t.Fatalf("records beside no sender = %+v, want the one obligation left owed", stalled)
	}
	channel := &hostTestChannel{}
	host.sender = channel
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off beside a sender: %v", err)
	}
	if got := channel.bodies(); len(got) != 1 || got[0] != stalled[0].Content {
		t.Fatalf("bodies the hand-off sent = %q, want the one stalled obligation %q", got, stalled[0].Content)
	}
	if records := hostTestDeliveryRecords(t, ctx, host); len(records) != 1 || records[0].Status != hostDeliverySent {
		t.Fatalf("records beside the redrive = %+v, want the one obligation closed as sent", records)
	}
	if err := host.handoffSettledBodies(ctx, addr, notice); err != nil {
		t.Fatalf("hand the settled bodies off a third time: %v", err)
	}
	if got := channel.bodies(); len(got) != 1 {
		t.Fatalf("bodies after the discharged hand-off = %q, want the one body already sent", got)
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
	host := &Host{run: run, entry: "agent-a", pendingDeliveries: map[string]*heldDelivery{}}
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
	if records := hostTestDeliveryRecords(t, ctx, host); len(records) != 1 || records[0].Status != hostDeliverySent || records[0].ID != "delivery-fresh-confirmed" {
		t.Fatalf("records beside the two sends = %+v, want the one receipt the live account took", records)
	}
}

// hostTestDeliveryRecords reads every delivery record the host's storage tier
// holds, ordered by identity so one check judges the same account however the
// tier answers a scan.
func hostTestDeliveryRecords(t *testing.T, ctx context.Context, host *Host) []hostDeliveryRecord {
	t.Helper()
	log := host.deliveryDurable()
	if log == nil {
		t.Fatalf("the assembled run opened no storage tier to read delivery records from")
	}
	held, err := log.Scan(ctx, hostDeliveryPrefix)
	if err != nil {
		t.Fatalf("scan the delivery records: %v", err)
	}
	keys := make([]string, 0, len(held))
	for key := range held {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]hostDeliveryRecord, 0, len(keys))
	for _, key := range keys {
		var record hostDeliveryRecord
		if err := json.Unmarshal(held[key], &record); err != nil {
			t.Fatalf("read the delivery record %s back: %v", key, err)
		}
		records = append(records, record)
	}
	return records
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
