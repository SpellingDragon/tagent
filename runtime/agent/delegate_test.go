package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegationObservation pins the lines the delegation ledger writes for every admission and every settle.
// - a fresh admission writes one DEBUG line carrying the task, the agent and the call
// - a settle that lands writes one INFO line carrying the task, the agent, the terminal and the elapsed time
// - a settle the open obligations withhold, and every named refusal, write their own DEBUG line
// - a logger that fails blocks neither the admission nor the settle: the bypass drops the line and keeps the ledger
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegationObservation(t *testing.T) {
	ctx := context.Background()

	t.Run("a fresh admission and its settle write their lines", func(t *testing.T) {
		observer := &delegationObserver{}
		defer watchDelegationLog(t, observer)()
		tasks := newDelegationTasksFace()
		tasks.deliverFresh(task.AdmissionReceipt{CallID: "call-1", TaskID: "task-1", Accepted: true, Kind: task.AdmissionFresh})
		ledger := NewDelegator(tasks, delegationBorrow{}, "inst-1")
		if _, err := ledger.Delegate(ctx, observationDelegation("call-1")); err != nil {
			t.Fatalf("admit the delegation: %v", err)
		}
		if !observer.mentions("debug", "delegation admitted", "result=fresh", "task=task-1", "agent=helper", "call=call-1") {
			t.Fatalf("debug lines = %v, want the fresh admission beside its task, agent and call", observer.all())
		}
		if _, err := ledger.Complete(ctx, "task-1", task.StatusSettled, "fact-1"); err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		if !observer.mentions("info", "delegation settled", "task=task-1", "agent=helper", "status=settled", "elapsed=") {
			t.Fatalf("info lines = %v, want the landed settle beside its terminal and elapsed time", observer.all())
		}
	})

	t.Run("a withheld settle and a refused admission write their lines", func(t *testing.T) {
		observer := &delegationObserver{}
		defer watchDelegationLog(t, observer)()
		tasks := newDelegationTasksFace()
		tasks.deliverFresh(task.AdmissionReceipt{CallID: "call-1", TaskID: "task-1", Accepted: true, Kind: task.AdmissionFresh})
		ledger := NewDelegator(tasks, delegationBorrow{}, "inst-1")
		if _, err := ledger.Delegate(ctx, Delegation{}); err == nil {
			t.Fatal("admission of an unnamed delegation = nil, want the named refusal")
		}
		if !observer.mentions("debug", "delegation refused", "result=unnamed") {
			t.Fatalf("debug lines = %v, want the refusal that names the missing call identity", observer.all())
		}
		if _, err := ledger.Delegate(ctx, observationDelegation("call-1")); err != nil {
			t.Fatalf("admit the delegation: %v", err)
		}
		tasks.hold("task-1", 1)
		if _, err := ledger.Complete(ctx, "task-1", task.StatusSettled, "fact-1"); err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		if !observer.mentions("debug", "delegation settle withheld", "task=task-1", "status=settled") {
			t.Fatalf("debug lines = %v, want the settle the open obligation withheld", observer.all())
		}
		if observer.mentions("info", "delegation settled") {
			t.Fatalf("info lines = %v, want no landed settle while the obligation is open", observer.all())
		}
		if _, err := ledger.Complete(ctx, "task-2", task.StatusSettled, "fact-2"); err == nil {
			t.Fatal("settle of an unknown task = nil, want the named refusal")
		}
		if !observer.mentions("debug", "delegation refused", "result=unknown", "task=task-2") {
			t.Fatalf("debug lines = %v, want the refusal that names the unknown task", observer.all())
		}
	})

	t.Run("a failing logger blocks neither the admission nor the settle", func(t *testing.T) {
		observer := &delegationObserver{panicsLeft: 2}
		defer watchDelegationLog(t, observer)()
		tasks := newDelegationTasksFace()
		tasks.deliverFresh(task.AdmissionReceipt{CallID: "call-1", TaskID: "task-1", Accepted: true, Kind: task.AdmissionFresh})
		ledger := NewDelegator(tasks, delegationBorrow{}, "inst-1")
		if _, err := ledger.Delegate(ctx, observationDelegation("call-1")); err != nil {
			t.Fatalf("admit the delegation beside a failing logger: %v", err)
		}
		if notice, err := ledger.Complete(ctx, "task-1", task.StatusSettled, "fact-1"); err != nil || notice.TaskID != "task-1" {
			t.Fatalf("settle beside a failing logger = (%v, %v), want the settle of the admitted task", notice, err)
		}
		if pending := ledger.Pending(); pending != 0 {
			t.Fatalf("pending delegations = %d, want none after the settle landed", pending)
		}
	})
}

// observationDelegation is one delegation of the observation checks: a complete
// call identity, a complete target address and the initiating reference the pin
// takes.
func observationDelegation(callID string) Delegation {
	return Delegation{
		InvocationID: "inv-1",
		DecisionID:   "dec-1",
		CallID:       callID,
		Sites:        DelegationSites{Target: event.Address{AgentID: "helper", SessionID: "session-helper"}},
		Ref:          state.BindingRef{AgentID: "helper", InstanceID: "inst-1", Generation: 1},
	}
}

// TestDelegationRoundReopen pins the reopening one re-entered round takes and the withdrawal of a round nothing served.
// - a settled delegation reopens onto the round the task machine opened, and the round is booked again so its terminal is awaited
// - an unknown delegation and a delegation that has not settled are each refused by name, and the precheck answers the same two cases without moving the entry
// - a reopened round nothing served is withdrawn back to the terminal it reopened from, so the ledger reads the outcome it stood at
// - a withdrawal of a round the ledger is not holding moves nothing
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegationRoundReopen(t *testing.T) {
	t.Run("a_settled_delegation_reopens_onto_the_next_round", func(t *testing.T) {
		ledger := delegationSettled(t)
		if err := ledger.CanReopenRound("task-1"); err != nil {
			t.Fatalf("precheck of the settled delegation = %v, want the round it may open", err)
		}
		if err := ledger.ReopenRound("task-1", 7); err != nil {
			t.Fatalf("reopen the settled delegation = %v, want the round it opened", err)
		}
		status, round, pending, held := delegationRoundOf(ledger, "task-1")
		if !held || status != task.StatusRunning || round != 7 || pending != task.StatusUnknown {
			t.Fatalf("entry after the reopen = (status=%v round=%d pending=%v held=%v), want the round 7 in flight awaiting its terminal",
				status, round, pending, held)
		}
		if got := ledger.Pending(); got != 1 {
			t.Fatalf("pending delegations after the reopen = %d, want the one round it awaited", got)
		}
	})

	t.Run("refuses_an_unknown_and_an_unsettled_delegation", func(t *testing.T) {
		ledger := delegationSettled(t)
		if err := ledger.CanReopenRound("task-nowhere"); !errors.Is(err, ErrDelegationUnknown) {
			t.Fatalf("precheck of an unknown delegation = %v, want %v", err, ErrDelegationUnknown)
		}
		if err := ledger.ReopenRound("task-nowhere", 7); !errors.Is(err, ErrDelegationUnknown) {
			t.Fatalf("reopen of an unknown delegation = %v, want %v", err, ErrDelegationUnknown)
		}
		if err := ledger.ReopenRound("task-1", 7); err != nil {
			t.Fatalf("reopen the settled delegation = %v", err)
		}
		if err := ledger.CanReopenRound("task-1"); !errors.Is(err, ErrDelegationNotSettled) {
			t.Fatalf("precheck of the reopened delegation = %v, want %v", err, ErrDelegationNotSettled)
		}
		if err := ledger.ReopenRound("task-1", 8); !errors.Is(err, ErrDelegationNotSettled) {
			t.Fatalf("second reopen of one delegation = %v, want %v", err, ErrDelegationNotSettled)
		}
		if _, round, _, _ := delegationRoundOf(ledger, "task-1"); round != 7 {
			t.Fatalf("round of the refused second reopen = %d, want the one round it holds 7", round)
		}
	})

	t.Run("a_round_nothing_served_is_withdrawn", func(t *testing.T) {
		ledger := delegationSettled(t)
		if err := ledger.ReopenRound("task-1", 7); err != nil {
			t.Fatalf("reopen the settled delegation = %v", err)
		}
		ledger.AbandonRound("task-1", 7)
		status, _, pending, _ := delegationRoundOf(ledger, "task-1")
		if status != task.StatusSettled || pending != task.StatusUnknown {
			t.Fatalf("entry after the withdrawal = (status=%v pending=%v), want the terminal it reopened from and no pending settle", status, pending)
		}
		if got := ledger.Pending(); got != 0 {
			t.Fatalf("pending delegations after the withdrawal = %d, want none: the booking of the withdrawn round is given back", got)
		}
		if err := ledger.CanReopenRound("task-1"); err != nil {
			t.Fatalf("precheck after the withdrawal = %v, want the delegation that may be reopened again", err)
		}
	})

	t.Run("withdrawing_another_round_moves_nothing", func(t *testing.T) {
		ledger := delegationSettled(t)
		if err := ledger.ReopenRound("task-1", 7); err != nil {
			t.Fatalf("reopen the settled delegation = %v", err)
		}
		ledger.AbandonRound("task-1", 6)
		ledger.AbandonRound("task-nowhere", 7)
		status, round, _, _ := delegationRoundOf(ledger, "task-1")
		if status != task.StatusRunning || round != 7 {
			t.Fatalf("entry after a withdrawal of another round = (status=%v round=%d), want the round 7 it still holds", status, round)
		}
	})
}

// delegationSettled returns a ledger holding one delegation that settled, so the rehearsal starts
// from the terminal a re-entry reopens a task out of.
func delegationSettled(t *testing.T) *Delegator {
	t.Helper()
	ctx := context.Background()
	tasks := newDelegationTasksFace()
	tasks.deliverFresh(task.AdmissionReceipt{CallID: "call-1", TaskID: "task-1", Accepted: true, Kind: task.AdmissionFresh})
	ledger := NewDelegator(tasks, delegationBorrow{}, "inst-1")
	if _, err := ledger.Delegate(ctx, observationDelegation("call-1")); err != nil {
		t.Fatalf("admit the delegation: %v", err)
	}
	if _, err := ledger.Complete(ctx, "task-1", task.StatusSettled, "fact-1"); err != nil {
		t.Fatalf("settle the delegation: %v", err)
	}
	if got := ledger.Pending(); got != 0 {
		t.Fatalf("pending delegations of the settled one = %d, want none", got)
	}
	return ledger
}

// delegationRoundOf reports the status, the round and the pending terminal one entry holds, so a
// check reads the bookkeeping the reopen and the withdrawal move.
func delegationRoundOf(ledger *Delegator, taskID event.TaskID) (task.Status, int64, task.Status, bool) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	entry, ok := ledger.entries[taskID]
	if !ok {
		return task.StatusUnknown, 0, task.StatusUnknown, false
	}
	return entry.record.Status, entry.round, entry.pending, true
}

// delegationBorrow is the borrowing face of the observation checks: it serves one
// handle that holds no releasing work, so the ledger's own bookkeeping is what the
// checks read.
type delegationBorrow struct{}

// Borrow serves the handle of the generation a request begins under.
func (delegationBorrow) Borrow(context.Context, event.AgentID) (*state.BindingHandle, error) {
	return &state.BindingHandle{}, nil
}

// BorrowDerived serves the handle a delegated request inherits.
func (delegationBorrow) BorrowDerived(context.Context, state.BindingRef) (*state.BindingHandle, error) {
	return &state.BindingHandle{}, nil
}

// delegationTasksFace is the task face the observation checks hand the ledger: it
// answers the admission receipt it was made to report and the obligations one task
// still holds, so the ledger's booking and its withheld settle are what the checks
// read.
type delegationTasksFace struct {
	mu      sync.Mutex
	open    map[event.TaskID]int
	receipt task.AdmissionReceipt
}

// newDelegationTasksFace returns a task face owing nothing.
func newDelegationTasksFace() *delegationTasksFace {
	return &delegationTasksFace{open: map[event.TaskID]int{}}
}

// Admit answers the admission receipt the face was made to report.
func (f *delegationTasksFace) Admit(context.Context, task.Spec) (task.AdmissionReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.receipt, nil
}

// Advance reports the transition committed.
func (f *delegationTasksFace) Advance(context.Context, event.TaskID, task.Status, task.MutationID, []task.Envelope) (task.CommitResult, error) {
	return task.CommitCommitted, nil
}

// Obligations reports the obligations the face still holds for one task.
func (f *delegationTasksFace) Obligations(_ context.Context, taskID event.TaskID) (task.Obligations, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := f.open[taskID]
	out := task.Obligations{Count: count}
	for i := 0; i < count; i++ {
		out.Open = append(out.Open, task.Obligation{Kind: task.ObligationAwaitingDelivery, TaskID: taskID})
	}
	return out, nil
}

// Flush offers nothing: the checks answer the settle through the obligation index.
func (f *delegationTasksFace) Flush(context.Context, event.TaskID) error { return nil }

// hold records the obligations one task still holds.
func (f *delegationTasksFace) hold(taskID event.TaskID, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open[taskID] = count
}

// deliverFresh makes the face answer the receipt named from now on.
func (f *delegationTasksFace) deliverFresh(receipt task.AdmissionReceipt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.receipt = receipt
}

// delegationObserver is the framework logger the observation checks of this
// package read the delegation face through: it records every line beside its
// level and refuses the writes its counter still holds by panicking.
type delegationObserver struct {
	mu         sync.Mutex
	lines      []string
	panicsLeft int
}

// record takes one line at the named level, or refuses it by panicking.
func (o *delegationObserver) record(level, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.panicsLeft > 0 {
		o.panicsLeft--
		panic("the logger refused the write")
	}
	o.lines = append(o.lines, level+": "+text)
}

// Debug records one debug line.
func (o *delegationObserver) Debug(args ...any) { o.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (o *delegationObserver) Debugf(format string, args ...any) {
	o.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (o *delegationObserver) Info(args ...any) { o.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (o *delegationObserver) Infof(format string, args ...any) {
	o.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (o *delegationObserver) Warn(args ...any) { o.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (o *delegationObserver) Warnf(format string, args ...any) {
	o.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (o *delegationObserver) Error(args ...any) { o.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (o *delegationObserver) Errorf(format string, args ...any) {
	o.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (o *delegationObserver) Fatal(args ...any) { o.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (o *delegationObserver) Fatalf(format string, args ...any) {
	o.record("fatal", fmt.Sprintf(format, args...))
}

// all reports every line taken, whatever the level.
func (o *delegationObserver) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// mentions reports whether one line taken at the level carries every fragment.
func (o *delegationObserver) mentions(level string, fragments ...string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, line := range o.lines {
		if !strings.HasPrefix(line, level+": ") {
			continue
		}
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
				break
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// watchDelegationLog points the framework logger at the observer for the duration
// of one check and answers the restore that ends it, so a check reads the lines of
// its own face and leaves the package logger as it found it.
func watchDelegationLog(t *testing.T, observer trpclog.Logger) func() {
	t.Helper()
	original := trpclog.Default
	trpclog.Default = observer
	restore := func() { trpclog.Default = original }
	t.Cleanup(restore)
	return restore
}

// TestDelegationEntryRestored pins the ledger entry a restarted process rebuilds from the task the record carries.
// - a settled delegated task restores into a settled entry that reopens onto the round a reactivation opens, and books that round
// - the rebuilt entry carries the invocation, call, decision and return site the task recorded
// - a running task restores into an entry the reopen still refuses by name
// - a task the ledger already holds is left exactly as it is
// - a task of another family, and an unnamed one, are refused by name
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegationEntryRestored(t *testing.T) {
	restoredSpec := func(taskID event.TaskID) task.Spec {
		return task.Spec{
			CallID:        "call-restored",
			DedupKey:      task.DedupKey("inst-1\x00inv-restored\x00dec-restored\x00call-restored:delegate"),
			InvocationID:  "inv-restored",
			InstanceID:    "inst-1",
			Description:   "delegate call-restored to reviewer",
			Source:        event.Address{AgentID: "caller", SessionID: "session-caller"},
			Target:        event.Address{AgentID: "caller", SessionID: "session-caller"},
			TriggerSource: event.LineageDelegation,
		}
	}

	t.Run("a settled task restores onto the round it reached", func(t *testing.T) {
		ledger := NewDelegator(newDelegationTasksFace(), delegationBorrow{}, "inst-1")
		if err := ledger.RestoreEntry("task-restored", restoredSpec("task-restored"), task.StatusSettled); err != nil {
			t.Fatalf("restore the settled delegation = %v", err)
		}
		if err := ledger.CanReopenRound("task-restored"); err != nil {
			t.Fatalf("precheck of the restored delegation = %v, want the terminal it was admitted with", err)
		}
		if err := ledger.ReopenRound("task-restored", 3); err != nil {
			t.Fatalf("reopen the restored delegation = %v", err)
		}
		status, round, pending, held := delegationRoundOf(ledger, "task-restored")
		if !held || status != task.StatusRunning || round != 3 || pending != task.StatusUnknown {
			t.Fatalf("entry after the reopen = (status=%v round=%d pending=%v held=%v), want the round 3 in flight",
				status, round, pending, held)
		}
		if got := ledger.Pending(); got != 1 {
			t.Fatalf("pending delegations after the reopen = %d, want the one round it awaited", got)
		}
		view := ledger.View("inv-restored")
		if len(view) != 1 {
			t.Fatalf("view of the restored invocation = %+v, want the one delegation it was admitted as", view)
		}
		record := view[0]
		if record.TaskID != "task-restored" || record.CallID != "call-restored" || record.DecisionID != "dec-restored" {
			t.Fatalf("restored record = %+v, want the invocation, call and decision the task recorded", record)
		}
		if record.Sites.Issuer != restoredSpec("task-restored").Source {
			t.Fatalf("restored return site = %+v, want the issuer the task recorded %+v", record.Sites.Issuer, restoredSpec("task-restored").Source)
		}
	})

	t.Run("a running task is still refused by name", func(t *testing.T) {
		ledger := NewDelegator(newDelegationTasksFace(), delegationBorrow{}, "inst-1")
		if err := ledger.RestoreEntry("task-running", restoredSpec("task-running"), task.StatusRunning); err != nil {
			t.Fatalf("restore the running delegation = %v", err)
		}
		if err := ledger.CanReopenRound("task-running"); !errors.Is(err, ErrDelegationNotSettled) {
			t.Fatalf("precheck of the running delegation = %v, want %v", err, ErrDelegationNotSettled)
		}
	})

	t.Run("a task the ledger already holds is left as it is", func(t *testing.T) {
		ledger := delegationSettled(t)
		if err := ledger.RestoreEntry("task-1", restoredSpec("task-1"), task.StatusSettled); err != nil {
			t.Fatalf("restore beside the live entry = %v", err)
		}
		status, round, _, held := delegationRoundOf(ledger, "task-1")
		if !held || status != task.StatusSettled || round != 1 {
			t.Fatalf("entry after the restore = (status=%v round=%d held=%v), want the live settled entry untouched",
				status, round, held)
		}
		view := ledger.View("inv-1")
		if len(view) != 1 || view[0].InvocationID != "inv-1" {
			t.Fatalf("view after the restore = %+v, want the live admission the ledger already held", view)
		}
	})

	t.Run("a task of another family is refused by name", func(t *testing.T) {
		ledger := NewDelegator(newDelegationTasksFace(), delegationBorrow{}, "inst-1")
		foreign := restoredSpec("task-foreign")
		foreign.TriggerSource = ""
		if err := ledger.RestoreEntry("task-foreign", foreign, task.StatusSettled); !errors.Is(err, ErrDelegationUnknown) {
			t.Fatalf("restore of a task of another family = %v, want %v", err, ErrDelegationUnknown)
		}
		if err := ledger.RestoreEntry("", restoredSpec("task-unnamed"), task.StatusSettled); !errors.Is(err, ErrDelegationUnknown) {
			t.Fatalf("restore of an unnamed task = %v, want %v", err, ErrDelegationUnknown)
		}
		if err := ledger.CanReopenRound("task-foreign"); !errors.Is(err, ErrDelegationUnknown) {
			t.Fatalf("precheck after the refused restore = %v, want %v", err, ErrDelegationUnknown)
		}
	})
}

// TestDelegatedTaskCommitFailureLeavesNoSegment pins that a refused task commit leaves the delegated round unstarted.
// - the caller reads the named cause of the refusal beside the failed terminal
// - the chain holds the instruction and the material facts and no task fact
// - no segment of the site was driven: no model request and the resident instance rests
// - a restarted loop over the same chain drives nothing either: the batch is read only through its task fact
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedTaskCommitFailureLeavesNoSegment(t *testing.T) {
	refused := errors.New("agent: the ledger refused the task fact")
	site := event.Address{AgentID: "agent-reviewer", SessionID: "session-caller#sub:call-review"}
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-batch", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)
	ledger := &batchCommitFace{history: history, refuse: delegatedPartTask, cause: refused}
	loop.SetFactCommit(ledger.commit)

	script := newInvariantLoopModel("the round must not run", nil)
	binding := state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}}
	consumers := NewConsumers(loop, context.Background(), nil)
	loop.SetConsumers(consumers)
	t.Cleanup(consumers.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer cancel()
	answer, err := loop.SubmitDelegated(ctx, DelegatedInput{
		Site:        site,
		Binding:     binding,
		Instruction: "record every finding beside the file it names",
		Request:     "the task whose commit the ledger refuses",
		Materials: []event.Event{{
			Key:     "ev-source",
			Type:    event.TypeExternalInput,
			Message: model.Message{Role: model.RoleUser, Content: "the material the batch projects"},
		}},
	})
	if err == nil {
		t.Fatalf("the refused task commit answered no error: the round ran beside a batch that never closed (answer=%+v)", answer)
	}
	if !errors.Is(err, refused) {
		t.Fatalf("the refused task commit answered %v, want the named cause %v", err, refused)
	}
	if answer.Status != task.StatusFailed || !strings.Contains(answer.Failure, refused.Error()) {
		t.Fatalf("the refused round = (%q, %q), want the failed terminal naming %q", answer.Failure, answer.Status, refused.Error())
	}

	facts := history.SessionFacts(site)
	parts := map[string]int{}
	for _, fact := range facts {
		parts[fact.Meta[delegatedPartMeta]]++
	}
	if parts[delegatedPartInstruction] != 1 || parts[delegatedPartMaterial] != 1 || parts[delegatedPartTask] != 0 {
		t.Fatalf("facts of the refused batch = %v, want the instruction and the material landed and no task: %+v", parts, facts)
	}

	if requests := script.requestMessages(); len(requests) != 0 {
		t.Fatalf("model requests = %d beside a batch whose task never landed, want no segment driven: %+v", len(requests), requests)
	}
	resident := consumers.Ensure(site)
	if resident == nil {
		t.Fatalf("the resident instance of %s = nil, want the mounted instance the round would run on", site.SessionID)
	}
	invariantAwait(t, resident.Resting)

	restarted := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-batch-restart", nil)
	restarted.SetHistory(history)
	restartedScript := newInvariantLoopModel("the restarted round must not run", nil)
	restartedBinding := state.AgentBinding{Model: restartedScript, PromptSource: state.PromptSource{Version: "v1"}}
	restartedScripts := NewConsumers(restarted, context.Background(), nil)
	restarted.SetConsumers(restartedScripts)
	t.Cleanup(restartedScripts.Stop)
	mounted := make(chan struct{}, 4)
	restartedScripts.SetProgress(func() {
		select {
		case mounted <- struct{}{}:
		default:
		}
	})
	instance := restartedScripts.mount(site, &restartedBinding)
	if instance == nil {
		t.Fatalf("the restarted loop mounted no instance for %s", site.SessionID)
	}
	instance.Wake()
	select {
	case <-mounted:
	case <-time.After(5 * time.Second):
		t.Fatal("the restarted loop never finished its first read of the chain")
	}
	if requests := restartedScript.requestMessages(); len(requests) != 0 {
		t.Fatalf("model requests = %d after the restart, want the orphan facts to drive nothing: %+v", len(requests), requests)
	}
}

// batchCommitFace is the commit face of one delegated batch that refuses the
// named part: every other fact lands into the chain face the way the run's
// ledger lands it, and the part it was fixed to refuse answers the named cause
// beside no record.
type batchCommitFace struct {
	mu      sync.Mutex
	history *invariantLoopHistory
	refuse  string
	cause   error
	seq     int
}

// commit keeps one fact of the batch, or answers the refusal the face was fixed
// with when the fact states the refused part.
func (f *batchCommitFace) commit(evt event.Event) (event.EventID, error) {
	if evt.Meta[delegatedPartMeta] == f.refuse {
		return "", f.cause
	}
	f.mu.Lock()
	if evt.Key == "" {
		f.seq++
		evt.Key = event.EventID(fmt.Sprintf("minted-batch-%d", f.seq))
	}
	f.mu.Unlock()
	f.history.land(evt)
	return evt.Key, nil
}

// TestDelegatedInstructionLandsOncePerSite pins that a delegated instruction commits once per execution site.
// - the site's chain holds exactly one system-message fact under the stable site key, carrying the instruction, its part and the delegation lineage
// - the fact carries no stamp of the round it was committed beside
// - a second round of the same site re-commits that identity and lands no second fact, while both rounds read it as a system message
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedInstructionLandsOncePerSite(t *testing.T) {
	const instruction = "record every finding beside the file it names"
	site := event.Address{AgentID: "agent-reviewer", SessionID: "session-caller#sub:call-review"}
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-instruction", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)
	ledger := newDelegatedBatchLedger(history)
	loop.SetFactCommit(ledger.commit)

	script := newInvariantLoopModel("review recorded", nil)
	binding := state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}}
	consumers := NewConsumers(loop, context.Background(), nil)
	loop.SetConsumers(consumers)
	t.Cleanup(consumers.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer cancel()
	for _, body := range []string{"review the first change", "review the second change"} {
		answer, err := loop.SubmitDelegated(ctx, DelegatedInput{
			Site:        site,
			Binding:     binding,
			Instruction: instruction,
			Request:     body,
		})
		if err != nil {
			t.Fatalf("the delegated round of %q = %v, want the settled answer", body, err)
		}
		if answer.Status != task.StatusSettled || answer.Body != "review recorded" {
			t.Fatalf("the delegated round of %q = (%q, %q), want the settled body the site produced", body, answer.Body, answer.Status)
		}
	}

	facts := history.SessionFacts(site)
	instructions := make([]event.Event, 0, 1)
	for _, fact := range facts {
		if fact.Meta[delegatedPartMeta] == delegatedPartInstruction {
			instructions = append(instructions, fact)
		}
	}
	if len(instructions) != 1 {
		t.Fatalf("instruction facts on the site chain = %d, want the one instruction of the site: %+v", len(instructions), facts)
	}
	got := instructions[0]
	if want := "delegated-agent-reviewer@session-caller#sub:call-review-instruction"; string(got.Key) != want {
		t.Fatalf("instruction fact key = %q, want the stable site key %q", got.Key, want)
	}
	if got.Type != event.TypeExternalInput {
		t.Fatalf("instruction fact type = %q, want the external input %q", got.Type, event.TypeExternalInput)
	}
	if got.Message.Role != model.RoleSystem {
		t.Fatalf("instruction fact role = %q, want the system message the instruction lands as", got.Message.Role)
	}
	if got.Message.Content != instruction {
		t.Fatalf("instruction fact body = %q, want the instruction the round runs under", got.Message.Content)
	}
	if got.Meta[event.MetaKeyTriggerSource] != event.LineageDelegation {
		t.Fatalf("instruction fact lineage = %q, want the delegation lineage %q", got.Meta[event.MetaKeyTriggerSource], event.LineageDelegation)
	}
	if got.DecisionID != "" || got.AttemptID != "" || got.CallID != "" || got.EpisodeID != "" || got.TaskID != "" {
		t.Fatalf("instruction fact carries round stamps (%q, %q, %q, %q, %q): the fact is one fact of the site",
			got.DecisionID, got.AttemptID, got.CallID, got.EpisodeID, got.TaskID)
	}

	if attempted := ledger.attempted(delegatedPartInstruction); attempted != 2 {
		t.Fatalf("instruction commits attempted = %d, want one per round so a re-entry keeps the fact of the site", attempted)
	}
	if landed := ledger.landed(delegatedPartInstruction); landed != 1 {
		t.Fatalf("instruction facts landed = %d, want the one identity the site already names", landed)
	}

	tasks := 0
	for _, fact := range facts {
		if fact.Meta[delegatedPartMeta] == delegatedPartTask {
			tasks++
		}
	}
	if tasks != 2 {
		t.Fatalf("task facts on the site chain = %d, want one per round", tasks)
	}

	requests := script.requestMessages()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want the one request of each round", len(requests))
	}
	for at, messages := range requests {
		if !carriesSystemMessage(messages, instruction) {
			t.Fatalf("round %d request carries no system message with the instruction: %+v", at+1, messages)
		}
	}
}

// carriesSystemMessage reports whether one request carries the text as a system
// message.
func carriesSystemMessage(messages []model.Message, text string) bool {
	for _, message := range messages {
		if message.Role == model.RoleSystem && message.Content == text {
			return true
		}
	}
	return false
}

// delegatedBatchLedger is the commit face of one delegated batch. It lands each
// fact into the chain face the way the run's ledger does — one record per
// identity, and a second commit under an identity the ledger already names is
// the commitment it already is — and counts what it saw per delegated part, so a
// check tells a commit that landed from one that was answered as an idempotent
// replay.
type delegatedBatchLedger struct {
	mu      sync.Mutex
	history *invariantLoopHistory
	facts   map[event.EventID]event.Event
	seq     int
	seen    map[string]int
	land    map[string]int
}

// newDelegatedBatchLedger returns a ledger landing into the named chain face.
func newDelegatedBatchLedger(history *invariantLoopHistory) *delegatedBatchLedger {
	return &delegatedBatchLedger{
		history: history,
		facts:   map[event.EventID]event.Event{},
		seen:    map[string]int{},
		land:    map[string]int{},
	}
}

// commit keeps one committed fact under its identity: a key the ledger already
// holds is answered as the fact it already names and lands nothing.
func (l *delegatedBatchLedger) commit(evt event.Event) (event.EventID, error) {
	l.mu.Lock()
	if evt.Key == "" {
		l.seq++
		evt.Key = event.EventID(fmt.Sprintf("minted-instruction-%d", l.seq))
	}
	part := evt.Meta[delegatedPartMeta]
	l.seen[part]++
	_, held := l.facts[evt.Key]
	if !held {
		l.facts[evt.Key] = evt
		l.land[part]++
	}
	l.mu.Unlock()
	if !held {
		l.history.land(evt)
	}
	return evt.Key, nil
}

// attempted reports how many commits of one delegated part the ledger saw.
func (l *delegatedBatchLedger) attempted(part string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[part]
}

// landed reports how many facts of one delegated part the ledger kept.
func (l *delegatedBatchLedger) landed(part string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.land[part]
}

// TestDelegatedInputRefusalLandsNothing pins that a refused delegated input commit leaves the round unstarted and the chain empty.
// - the caller reads the named cause of the refusal, not a silently empty round
// - the chain holds no input fact of the site: the refused fact was never landed
// - exactly one commit was attempted, and it is the instruction as a system message under the stable site key, stamped with its part and lineage
// - no segment of the site was driven: no model request and the resident instance rests
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedInputRefusalLandsNothing(t *testing.T) {
	refused := errors.New("agent: the ledger refused the delegated input")
	site := event.Address{AgentID: "agent-refused", SessionID: "session-refused"}
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-refused", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)

	var mu sync.Mutex
	var attempts []event.Event
	loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		mu.Lock()
		attempts = append(attempts, evt)
		mu.Unlock()
		return "", refused
	})

	consumers := NewConsumers(loop, context.Background(), nil)
	loop.SetConsumers(consumers)
	t.Cleanup(consumers.Stop)
	script := newInvariantLoopModel("the delegated turn must not run", nil)
	binding := state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}}

	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer cancel()
	answer, err := loop.SubmitDelegated(ctx, DelegatedInput{
		Site:        site,
		Binding:     binding,
		Instruction: "run only after the input commits",
		Request:     "the task whose commit the ledger refuses",
		Materials:   []event.Event{{Key: "ev-refused-source", Type: event.TypeExternalInput}},
	})
	if err == nil {
		t.Fatalf("the refused delegated input answered no error: the round ran beside a fact that never landed (answer=%+v)", answer)
	}
	if !errors.Is(err, refused) {
		t.Fatalf("the refused delegated input answered %v, want the named cause %v", err, refused)
	}
	if !strings.Contains(err.Error(), refused.Error()) {
		t.Fatalf("the refusal text %q does not name the cause %q", err.Error(), refused.Error())
	}

	if facts := history.SessionFacts(site); len(facts) != 0 {
		t.Fatalf("the chain of %s holds %d fact(s) beside a refused commit: %+v", site.SessionID, len(facts), facts)
	}

	mu.Lock()
	got := append([]event.Event(nil), attempts...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("commit attempts = %d, want exactly the first fact of the batch the round commits", len(got))
	}
	if got[0].Type != event.TypeExternalInput {
		t.Fatalf("the attempted fact type = %q, want the external input %q", got[0].Type, event.TypeExternalInput)
	}
	if got[0].Message.Role != model.RoleSystem {
		t.Fatalf("the attempted fact role = %q, want the system message the instruction lands as", got[0].Message.Role)
	}
	if got[0].Message.Content != "run only after the input commits" {
		t.Fatalf("the attempted fact body = %q, want the instruction the round runs under", got[0].Message.Content)
	}
	if want := "delegated-agent-refused@session-refused-instruction"; string(got[0].Key) != want {
		t.Fatalf("the attempted fact key = %q, want the stable site key %q of the instruction", got[0].Key, want)
	}
	if got[0].Meta[delegatedPartMeta] != delegatedPartInstruction {
		t.Fatalf("the attempted fact part = %q, want the instruction part %q", got[0].Meta[delegatedPartMeta], delegatedPartInstruction)
	}
	if got[0].Meta[event.MetaKeyTriggerSource] != event.LineageDelegation {
		t.Fatalf("the attempted fact lineage = %q, want the delegation lineage %q", got[0].Meta[event.MetaKeyTriggerSource], event.LineageDelegation)
	}
	if got[0].DecisionID != "" || got[0].AttemptID != "" || got[0].CallID != "" || got[0].EpisodeID != "" {
		t.Fatalf("the attempted fact carries round stamps (%q, %q, %q, %q): the instruction is one fact of the site, never a fact of the round",
			got[0].DecisionID, got[0].AttemptID, got[0].CallID, got[0].EpisodeID)
	}

	if requests := script.requestMessages(); len(requests) != 0 {
		t.Fatalf("model requests = %d beside a refused input commit, want no segment driven: %+v", len(requests), requests)
	}
	resident := consumers.Ensure(site)
	if resident == nil {
		t.Fatalf("the resident instance of %s = nil, want the mounted instance the round would run on", site.SessionID)
	}
	invariantAwait(t, resident.Resting)
	if answer.Status != task.StatusFailed {
		t.Fatalf("the refused round answered status %d, want the failed terminal %d", answer.Status, task.StatusFailed)
	}
	if !strings.Contains(answer.Failure, refused.Error()) {
		t.Fatalf("the refused round answers failure %q, want it to name the cause %q", answer.Failure, refused.Error())
	}
}
