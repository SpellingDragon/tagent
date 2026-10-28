package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestNewToolAdapterNeedsNoHostCompensation drives the contract a new tool is registered under.
// - a tool placed in NativeAgent.Tools runs admission, execution, settlement and delivery through the formal entry
// - a restart replays the settled tool task from the fact its result was recorded as, and asks the adapter for nothing
// - a failing adapter ends its task failed through the same faces, beside no second notice
// - progress and stop evidence travel between the executor and the task face, and a late settlement compensates nothing
// - the lifecycle vocabulary stays the closed set the core declares, so a new tool introduces no status
// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
func TestNewToolAdapterNeedsNoHostCompensation(t *testing.T) {
	t.Run("a tool placed in NativeAgent.Tools runs admission, execution, settlement and delivery", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assertStatusVocabularyUnchanged(t)
		adapter := &minimalAdapter{name: "probe", output: "adapter-out"}
		provider := adapterProvider()
		addr := event.Address{AgentID: "agent-a", SessionID: "session-adapter"}
		run, err := tagent.Assemble(ctx, adapterDeclaration(), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{adapter}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime beside the new adapter: %v", err)
		}

		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-adapter",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "use the new tool"}},
		})
		if err != nil {
			t.Fatalf("invoke the adapter range: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the tool call turn and the final answer", turn.Terminal, len(turn.Decisions))
		}

		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("requests = %d, want one per decision of the range", len(requests))
		}
		second := requests[1].Messages
		receipt := messageUnder(second, "call-1", model.RoleTool)
		if receipt.Content != task.AdmissionReceiptText {
			t.Fatalf("receipt of call-1 = %q, want the minimal admission confirmation %q", receipt.Content, task.AdmissionReceiptText)
		}
		if got := adapter.calls(); got != 1 {
			t.Fatalf("adapter executions = %d, want the admitted call executed exactly once", got)
		}

		observation := messageUnder(second, "call-1", model.RoleUser)
		if observation.Content != "adapter-out" {
			t.Fatalf("observation of call-1 = %q, want the settled body the adapter produced", observation.Content)
		}
		if observation.ToolName != "probe" {
			t.Fatalf("observation of call-1 = %+v, want the call identity preserved on the settled body", observation)
		}

		tasks := listObjects(t, ctx, run, addr, query.ObjectTask)
		if len(tasks) != 1 {
			t.Fatalf("task objects = %+v, want the one task the call was admitted as", tasks)
		}
		assertClosedStatus(t, "the settled task", tasks[0].State.TaskStatus)
		if tasks[0].State.TaskStatus != task.StatusSettled {
			t.Fatalf("task status = %s, want the terminal settlement of the adapter's result", statusName(tasks[0].State.TaskStatus))
		}
		notices := listObjects(t, ctx, run, addr, query.ObjectNotice)
		if len(notices) != 1 {
			t.Fatalf("notice objects = %+v, want the one settled notification of the call", notices)
		}
		if notices[0].State != query.DetailRetired {
			t.Fatalf("notice %v state = %+v, want the consumed phase: nothing is owed beside a delivered result", notices[0].Ref.Notice, notices[0].State)
		}
	})

	t.Run("a restart replays the settled tool task from the fact the adapter's result was recorded as", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-adapter-restart"}
		adapter := &minimalAdapter{name: "probe", output: "adapter-out"}
		run, err := tagent.Assemble(ctx, adapterDeclaration(), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: adapterProvider(), Tools: []trpctool.CallableTool{adapter}},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithStorage(cfg))
		if err != nil {
			t.Fatalf("assemble the durable runtime beside the new adapter: %v", err)
		}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-adapter",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "use the new tool"}},
		}); err != nil {
			t.Fatalf("invoke the adapter range: %v", err)
		}
		if got := adapter.calls(); got != 1 {
			t.Fatalf("adapter executions = %d, want the admitted call executed exactly once", got)
		}

		restarted := &minimalAdapter{name: "probe", output: "adapter-out"}
		second, err := tagent.Assemble(ctx, adapterDeclaration(), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: adapterProvider(), Tools: []trpctool.CallableTool{restarted}},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithStorage(cfg))
		if err != nil {
			t.Fatalf("restart the durable runtime: %v", err)
		}
		report := second.Recovery()
		if len(report.Tasks) != 1 || !report.Tasks[0].Recovered || report.Tasks[0].Refused != nil {
			t.Fatalf("recovery report = %+v, want the one admitted task rebuilt from its chain", report.Tasks)
		}
		assertClosedStatus(t, "the restored task", report.Tasks[0].Status)
		if report.Tasks[0].Status != task.StatusSettled {
			t.Fatalf("restored status = %s, want the settlement the chain already carried", statusName(report.Tasks[0].Status))
		}
		if got := restarted.calls(); got != 0 {
			t.Fatalf("adapter executions after the restart = %d, want none asked beside a landed settlement", got)
		}
		tasks := listObjects(t, ctx, second, addr, query.ObjectTask)
		if len(tasks) != 1 || tasks[0].State.TaskStatus != task.StatusSettled {
			t.Fatalf("task objects after the restart = %+v, want the one settled task and no task opened beside it", tasks)
		}
		for _, notice := range listObjects(t, ctx, second, addr, query.ObjectNotice) {
			if notice.State != query.DetailRetired {
				t.Fatalf("notice %v state after the restart = %+v, want the consumed phase: a delivered result is never owed again", notice.Ref.Notice, notice.State)
			}
		}
	})

	t.Run("a failing adapter ends its task failed through the same faces", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		adapter := &minimalAdapter{name: "probe", err: errors.New("the adapter could not reach its target")}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-adapter-failure"}
		provider := adapterProvider()
		run, err := tagent.Assemble(ctx, adapterDeclaration(), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{adapter}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime beside the failing adapter: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-adapter",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "use the new tool"}},
		})
		if err != nil {
			t.Fatalf("invoke the adapter range: %v", err)
		}
		if len(turn.Decisions) != 2 {
			t.Fatalf("turn = %d decisions, want the tool call turn and the decision beside its settled failure", len(turn.Decisions))
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("requests = %d, want one per decision of the range", len(requests))
		}
		failure := messageUnder(requests[1].Messages, "call-1", model.RoleUser)
		if failure.Content != adapter.err.Error() {
			t.Fatalf("observation of call-1 = %q, want the adapter's own error as the settled body", failure.Content)
		}
		tasks := listObjects(t, ctx, run, addr, query.ObjectTask)
		if len(tasks) != 1 {
			t.Fatalf("task objects = %+v, want the one task the failed call was admitted as", tasks)
		}
		assertClosedStatus(t, "the failed task", tasks[0].State.TaskStatus)
		if tasks[0].State.TaskStatus != task.StatusFailed {
			t.Fatalf("task status = %s, want the failure the adapter's error states", statusName(tasks[0].State.TaskStatus))
		}
		notices := listObjects(t, ctx, run, addr, query.ObjectNotice)
		if len(notices) != 1 || notices[0].State != query.DetailRetired {
			t.Fatalf("notice objects = %+v, want the one consumed failure notification beside no second pass", notices)
		}
	})

	t.Run("progress and stop evidence travel between the executor and the task face", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newAdapterRig(t, ctx)
		adapter := &minimalAdapter{name: "probe", output: "adapter-out", gate: make(chan struct{})}
		wrapper := rig.wrap(adapter)

		returned, err := wrapper.Call(adapterCallContext("call-cancel"), []byte(`{}`))
		if err != nil {
			t.Fatalf("Call = (%v, %v), want one admission receipt", returned, err)
		}
		receipt, ok := returned.(task.AdmissionReceipt)
		if !ok || !receipt.Accepted || receipt.Kind != task.AdmissionFresh || receipt.TaskID == "" {
			t.Fatalf("Call = (%+v), want one accepted fresh admission receipt", returned)
		}
		taskID := receipt.TaskID

		envelope, err := rig.registry.IssueNotice(ctx, taskID, task.NoticeProgress, "evt-progress")
		if err != nil {
			t.Fatalf("issue the progress notification: %v", err)
		}
		if result, err := rig.registry.Advance(ctx, taskID, task.StatusRunning,
			task.MutationID("progress:"+string(taskID)+":1"), []task.Envelope{envelope}); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance progress = (%v, %v), want the report committed beside its notification", result, err)
		}
		if err := rig.manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush the progress notification: %v", err)
		}
		if got := rig.deliveredNotices(); len(got) != 1 || got[0].Kind != task.NoticeProgress {
			t.Fatalf("delivered = %+v, want the one progress notification the task face owed", got)
		}
		if reported, err := rig.registry.ProgressReported(ctx, taskID); err != nil || !reported {
			t.Fatalf("progress report = (%v, %v), want the running phase read back from the chain", reported, err)
		}
		_, running, err := rig.registry.Lookup(ctx, taskID)
		if err != nil {
			t.Fatalf("look up the running task: %v", err)
		}
		assertClosedStatus(t, "the running task", running)
		if running != task.StatusRunning {
			t.Fatalf("status = %s, want the running phase as the only state a progress report leaves", statusName(running))
		}

		cancelReceipt, err := rig.registry.RequestCancel(ctx, taskID)
		if err != nil || !cancelReceipt.Requested {
			t.Fatalf("request the cancellation = (%+v, %v), want the control receipt recorded", cancelReceipt, err)
		}
		adapter.stop()
		if err := rig.manager.RecordStop(ctx, task.StopEvidence{
			TaskID:  taskID,
			Stopped: adapter.stopped(),
			Reason:  "the adapter stopped its work",
		}); err != nil {
			t.Fatalf("record the adapter's stop evidence: %v", err)
		}
		if err := rig.manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush the cancellation notification: %v", err)
		}
		delivered := rig.deliveredNotices()
		if len(delivered) != 2 || delivered[1].Kind != task.NoticeCancelled {
			t.Fatalf("delivered = %+v, want the progress report beside the cancellation the stop evidence closed", delivered)
		}
		_, cancelled, err := rig.registry.Lookup(ctx, taskID)
		if err != nil {
			t.Fatalf("look up the cancelled task: %v", err)
		}
		assertClosedStatus(t, "the cancelled task", cancelled)
		if cancelled != task.StatusCancelled {
			t.Fatalf("status = %s, want the cancellation the stop evidence reported", statusName(cancelled))
		}

		close(adapter.gate)
		select {
		case <-rig.recorded:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the adapter's result to be recorded")
		}
		if got := rig.awaitStableDeliveries(t, 2); got != 2 {
			t.Fatalf("deliveries beside the late settlement = %d, want the 2 the chain already carried", got)
		}
		open, err := rig.manager.Obligations(ctx, taskID)
		if err != nil || open.Count != 0 {
			t.Fatalf("obligations beside the late settlement = (%+v, %v), want none owed", open, err)
		}

		rebuild := rig.rebuild(t, ctx, taskID)
		if rebuild.Status != task.StatusCancelled {
			t.Fatalf("restored status = %s, want the cancellation the chain carried", statusName(rebuild.Status))
		}
		if len(rebuild.Owed) != 2 {
			t.Fatalf("obligations after the restart = %+v, want the two notifications the chain carries", rebuild.Owed)
		}
		for _, owed := range rebuild.Owed {
			if owed.Kind != task.ObligationAwaitingDelivery {
				t.Fatalf("obligation after the restart = %+v, want no execution obligation: the stop evidence closed it before the restart", owed)
			}
		}
		wanted := make([]task.Notice, 0, len(rebuild.Owed))
		for _, owed := range rebuild.Owed {
			wanted = append(wanted, owed.Notice)
		}
		if !reflect.DeepEqual(rebuild.Delivered, wanted) {
			t.Fatalf("delivered after the restart = %+v, want the very notifications the chain owed %+v", rebuild.Delivered, wanted)
		}
		if rebuild.Remaining != 0 {
			t.Fatalf("obligations beside the restart's own settlement = %d, want none", rebuild.Remaining)
		}
	})
}

// minimalAdapter is one new tool adapter, providing exactly the three faces the
// contract asks of it and nothing else: execution through the native callable
// port, its own stop evidence, and the result body recovery reads back from the
// fact it was recorded as. It keeps no queue, no second record of its calls and
// no state beside the stop it reports.
type minimalAdapter struct {
	name   string
	output string
	err    error
	// gate holds the execution until the sub-case opens it, so a sub-case stands
	// beside a call that is still in flight.
	gate chan struct{}

	mu         sync.Mutex
	executions int
	halted     bool
	haltReport bool
}

// Declaration returns the declaration naming the adapter, so the model is
// offered the name and no schema of its own.
func (a *minimalAdapter) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: a.name}
}

// Call is the execution face: it runs the adapter's work once and reports the
// body the host records as the fact recovery reads.
func (a *minimalAdapter) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	a.mu.Lock()
	a.executions++
	halted, gate := a.halted, a.gate
	a.mu.Unlock()
	if halted {
		return nil, errAdapterStopped
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return a.output, a.err
}

// stop is the cancellation face: the adapter abandons its work and holds the
// stop it reports to the task face.
func (a *minimalAdapter) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.halted = true
	a.haltReport = true
}

// stopped reports whether the adapter actually stopped its work.
func (a *minimalAdapter) stopped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.haltReport
}

// calls reports how often the adapter executed its work.
func (a *minimalAdapter) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.executions
}

// errAdapterStopped reports an execution the adapter abandoned beside its stop.
var errAdapterStopped = errors.New("adapter: the work was stopped")

// adapterRig assembles the task face one new adapter is registered against: the
// registry over a durable commit, the manager holding the obligations beside the
// delivery a sub-case reads, and the recorder the adapter's body is recorded
// through.
type adapterRig struct {
	ctx      context.Context
	cfg      ledger.StorageConfig
	registry *task.MemoryRegistry
	manager  *task.MemoryManager
	recorded chan task.Result

	mu        sync.Mutex
	delivered []task.Notice
}

// newAdapterRig opens one durable tier and assembles the task face over it, so
// the chain a sub-case builds is the chain a restart reads back.
func newAdapterRig(t *testing.T, ctx context.Context) *adapterRig {
	t.Helper()
	rig := &adapterRig{
		ctx:      ctx,
		cfg:      ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()},
		recorded: make(chan task.Result, 4),
	}
	registry := task.NewMemoryRegistry(rig.durable(t), task.NewMemoryAdmission())
	rig.registry = registry
	rig.manager = task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		rig.mu.Lock()
		rig.delivered = append(rig.delivered, notice)
		rig.mu.Unlock()
		return task.DeliveryAccepted, nil
	})
	return rig
}

// durable opens the rig's tier as the commit carrier the registry writes to.
func (r *adapterRig) durable(t *testing.T) *state.DurableCommit {
	t.Helper()
	tier, err := ledger.OpenTier(r.ctx, r.cfg)
	if err != nil {
		t.Fatalf("open the durable tier: %v", err)
	}
	durable, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("open the durable commit: %v", err)
	}
	return durable
}

// wrap registers one adapter the way the host registers a native tool: over the
// manager, beside the result recorder and the mailbox the settled body travels
// through.
func (r *adapterRig) wrap(adapter *minimalAdapter) *task.Wrapper {
	return task.Wrap(r.manager, adapter,
		task.WithResultRecorder(func(ctx context.Context, result task.Result) (event.EventID, error) {
			r.recorded <- result
			return event.EventID("fact-" + result.CallID), nil
		}),
		task.WithMailbox(func(ctx context.Context, notice task.Notice) error {
			r.mu.Lock()
			r.delivered = append(r.delivered, notice)
			r.mu.Unlock()
			return nil
		}))
}

// deliveredNotices reports the notifications the task face handed to the target.
func (r *adapterRig) deliveredNotices() []task.Notice {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]task.Notice(nil), r.delivered...)
}

// awaitStableDeliveries waits for the named count of deliveries and then holds
// the count over a bounded moment, so a notice a compensation pass would push
// beside a refused settlement has the time to arrive before it is read as
// absent.
func (r *adapterRig) awaitStableDeliveries(t *testing.T, want int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(r.deliveredNotices()) < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stable := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(stable) {
		if got := len(r.deliveredNotices()); got > want {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	return len(r.deliveredNotices())
}

// rebuildReport is what a restart reads back for one task: the status it replays,
// the obligations the chain still carries, the notifications those obligations
// name, what the restart's own delivery face settled and what stayed open.
type rebuildReport struct {
	Status    task.Status
	Owed      []task.Obligation
	Delivered []task.Notice
	Remaining int
}

// rebuild reopens the rig's tier and restores the named task from its chain, so
// a sub-case reads what recovery answers without the live registry: the
// obligations the chain carries are then settled through the same task face the
// live run uses, and never through a pass of the restart's own.
func (r *adapterRig) rebuild(t *testing.T, ctx context.Context, taskID event.TaskID) rebuildReport {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, r.cfg)
	if err != nil {
		t.Fatalf("reopen the durable tier: %v", err)
	}
	restored, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("reopen the durable commit: %v", err)
	}
	chain, err := restored.Records(ctx, taskID)
	if err != nil {
		t.Fatalf("read the chain of %s: %v", taskID, err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(state.StatePlan{
		Agents: map[event.AgentID]state.AgentBinding{
			"agent-a": {Tools: []trpctool.Tool{restartOwnerTool{name: "agent-a"}}},
		},
	}); err != nil {
		t.Fatalf("hold the binding: %v", err)
	}
	registry := task.NewMemoryRegistry(restored, task.NewMemoryAdmission())
	report, err := registry.Restore(ctx, chain, state.NewBindingOwners(bindings))
	if err != nil {
		t.Fatalf("restore the chain of %s: %v", taskID, err)
	}
	if len(report.Tasks) != 1 || report.Tasks[0].TaskID != taskID || !report.Tasks[0].Recovered || report.Tasks[0].Refused != nil {
		t.Fatalf("restore report = %+v, want the one task rebuilt from its chain", report.Tasks)
	}
	run := rebuildReport{Status: report.Tasks[0].Status}
	var delivered []task.Notice
	manager := task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		delivered = append(delivered, notice)
		return task.DeliveryAccepted, nil
	})
	open, err := manager.Obligations(ctx, taskID)
	if err != nil {
		t.Fatalf("report the obligations of the restored task: %v", err)
	}
	run.Owed = append(run.Owed, open.Open...)
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush the obligations the chain carried: %v", err)
	}
	after, err := manager.Obligations(ctx, taskID)
	if err != nil {
		t.Fatalf("report the obligations beside the restart's settlement: %v", err)
	}
	run.Delivered = append(run.Delivered, delivered...)
	run.Remaining = after.Count
	return run
}

// adapterProvider scripts the model of both decisions: the first issues the call
// of the new tool, the second reads the settled body beside the receipt.
func adapterProvider() *convModel {
	return &convModel{name: "model-adapter", answer: func(req *model.Request) *model.Response {
		if len(req.Messages) == 1 {
			return convToolTurn(convCall("call-1", "probe", `{}`))
		}
		return convFinalTurn("done")
	}}
}

// adapterDeclaration is the declaration every adapter sub-case assembles.
func adapterDeclaration() *config.Declaration {
	return &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-adapter"}},
	}
}

// adapterCallContext is the call frame one wrapped adapter call is admitted
// under: the identity of the decision that issued it beside the address it
// belongs to.
func adapterCallContext(callID string) context.Context {
	return task.WithCallFrame(context.Background(), task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-adapter",
			InvocationID: "inv-adapter",
			DecisionID:   "decision-adapter",
			CallID:       callID,
		},
		Address:   event.Address{AgentID: "agent-a", SessionID: "session-adapter"},
		Execution: "serialized",
	})
}

// listObjects reads one dimension of the live scene through the formal read face.
func listObjects(t *testing.T, ctx context.Context, run *tagent.Run, addr event.Address, kind query.ObjectKind) []query.Detail {
	t.Helper()
	scope := query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}}
	snapshot, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{kind}})
	if err != nil {
		t.Fatalf("query the %s dimension: %v", kind, err)
	}
	return snapshot.Details
}

// messageUnder returns the message carrying the named call under the named role.
func messageUnder(messages []model.Message, callID string, role model.Role) model.Message {
	for _, message := range messages {
		if message.ToolID == callID && message.Role == role {
			return message
		}
	}
	return model.Message{}
}

// statusVocabulary is the closed lifecycle vocabulary of the task core: every
// value is a state a transition commits, and a new tool introduces none.
var statusVocabulary = []struct {
	name  string
	value task.Status
}{
	{"StatusUnknown", task.StatusUnknown},
	{"StatusAccepted", task.StatusAccepted},
	{"StatusRunning", task.StatusRunning},
	{"statusProgressRetired", task.Status(3)},
	{"StatusSettled", task.StatusSettled},
	{"StatusFailed", task.StatusFailed},
	{"StatusCancelled", task.StatusCancelled},
	{"StatusAwaitingApproval", task.StatusAwaitingApproval},
}

// assertClosedStatus holds one observed status inside the closed vocabulary, so a
// chain standing in a state the core does not declare fails where it is read.
func assertClosedStatus(t *testing.T, where string, status task.Status) {
	t.Helper()
	for _, known := range statusVocabulary {
		if known.value == status {
			return
		}
	}
	t.Fatalf("%s stands in status %d, want one of the closed vocabulary the core declares", where, int(status))
}

// statusName reports the declared name of one status.
func statusName(status task.Status) string {
	for _, known := range statusVocabulary {
		if known.value == status {
			return known.name
		}
	}
	return "an undeclared status"
}

// assertStatusVocabularyUnchanged pins the declaration itself: the closed set is
// the whole vocabulary the core may hold, so a status added beside it stops the
// new-tool contract from passing.
func assertStatusVocabularyUnchanged(t *testing.T) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "runtime", "task", "spec.go"))
	if err != nil {
		t.Fatalf("read the status declaration: %v", err)
	}
	declared, err := statusConstNames(string(body))
	if err != nil {
		t.Fatalf("parse the status declaration: %v", err)
	}
	want := make([]string, 0, len(statusVocabulary))
	for _, known := range statusVocabulary {
		want = append(want, known.name)
	}
	if !reflect.DeepEqual(declared, want) {
		t.Fatalf("declared statuses = %v, want the closed vocabulary %v", declared, want)
	}
}

// statusConstNames reads the identifiers of the Status const block in their
// declared order, so an inserted, appended or renamed value is read as such.
func statusConstNames(body string) ([]string, error) {
	at := strings.Index(body, "type Status int")
	if at < 0 {
		return nil, errors.New("the package declares no Status type")
	}
	rest := body[at:]
	open := strings.Index(rest, "const (")
	if open < 0 {
		return nil, errors.New("the Status type carries no const block")
	}
	block := rest[open+len("const ("):]
	end := strings.Index(block, "\n)")
	if end < 0 {
		return nil, errors.New("the Status const block is not closed")
	}
	var names []string
	for _, line := range strings.Split(block[:end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		names = append(names, strings.Fields(line)[0])
	}
	return names, nil
}
