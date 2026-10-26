package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
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
		tasks := newInvariantShutdownTasks()
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
		tasks := newInvariantShutdownTasks()
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
		tasks := newInvariantShutdownTasks()
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
		Target:       event.Address{AgentID: "helper", SessionID: "session-helper"},
		Ref:          state.BindingRef{AgentID: "helper", InstanceID: "inst-1", Generation: 1},
	}
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
