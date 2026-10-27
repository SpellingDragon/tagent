package state

import (
	"context"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/runtime/agent"
	orgstate "github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegationLineageLandsOnTheAdmissionRecord pins the delegation lineage onto the admission record.
// - a delegated admission records event.LineageDelegation on its task_spawned material, beside the fact chain
// - a reopened carrier reads the stamp back from the stored material instead of deriving one again
// - a user-path admission keeps an empty stamp, so the InvokeRequest lineage is unchanged
// - the meditation domain reads the delegation stamp as self-managed and never delivers it
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegationLineageLandsOnTheAdmissionRecord(t *testing.T) {
	ctx := context.Background()
	log := newLineageFactLog()
	durable, err := orgstate.NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the durable carrier: %v", err)
	}
	registry := task.NewMemoryRegistry(durable, task.NewMemoryAdmission())
	tasks := lineageTasks{MemoryManager: task.NewMemoryManager(registry, nil), MemoryRegistry: registry}
	ledger := agent.NewDelegator(tasks, lineageBorrow{}, event.InstanceID("instance-lineage"))
	target := event.Address{AgentID: "helper", SessionID: "session-helper"}
	receipt, err := ledger.Delegate(ctx, agent.Delegation{
		InvocationID:  event.InvocationID("inv-lineage"),
		DecisionID:    event.DecisionID("dec-lineage"),
		CallID:        "call-lineage",
		Target:        target,
		TriggerSource: event.LineageDelegation,
		Ref:           orgstate.BindingRef{AgentID: target.AgentID, InstanceID: "instance-lineage", Generation: 1},
	})
	if err != nil || !receipt.Accepted || receipt.Kind != task.AdmissionFresh {
		t.Fatalf("admit the delegated call = (%+v, %v), want one fresh accepted admission", receipt, err)
	}
	view := ledger.View(event.InvocationID("inv-lineage"))
	if len(view) != 1 || view[0].TriggerSource != event.LineageDelegation {
		t.Fatalf("delegation view = %+v, want the delegation lineage on the admission record", view)
	}
	spec, _, err := registry.Lookup(ctx, receipt.TaskID)
	if err != nil {
		t.Fatalf("look up the admitted task: %v", err)
	}
	if spec.TriggerSource != event.LineageDelegation {
		t.Fatalf("task material trigger source = %q, want %q", spec.TriggerSource, event.LineageDelegation)
	}
	chain, err := durable.Records(ctx, receipt.TaskID)
	if err != nil {
		t.Fatalf("read the durable task record: %v", err)
	}
	if len(chain) == 0 || chain[0].Spec.TriggerSource != event.LineageDelegation {
		t.Fatalf("stored task_spawned material = %+v, want the delegation lineage on the admission link", chain)
	}
	reopened, err := orgstate.NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("reopen the durable carrier: %v", err)
	}
	replayed, err := reopened.Records(ctx, receipt.TaskID)
	if err != nil {
		t.Fatalf("read the revived task record: %v", err)
	}
	if len(replayed) == 0 || replayed[0].Spec.TriggerSource != event.LineageDelegation {
		t.Fatalf("revived task_spawned material = %+v, want the recorded delegation lineage, not a derived one", replayed)
	}

	userReceipt, err := tasks.MemoryManager.Admit(ctx, task.Spec{
		CallID:       "call-user",
		InvocationID: event.InvocationID("inv-user"),
		InstanceID:   event.InstanceID("instance-lineage"),
		Description:  "a user-path admission",
		Source:       target,
		Target:       target,
	})
	if err != nil || !userReceipt.Accepted {
		t.Fatalf("admit the user-path task = (%+v, %v), want one accepted admission", userReceipt, err)
	}
	userSpec, _, err := registry.Lookup(ctx, userReceipt.TaskID)
	if err != nil {
		t.Fatalf("look up the user-path task: %v", err)
	}
	if userSpec.TriggerSource != "" {
		t.Fatalf("user-path trigger source = %q, want none: the invocation lineage is unchanged", userSpec.TriggerSource)
	}
	userChain, err := durable.Records(ctx, userReceipt.TaskID)
	if err != nil {
		t.Fatalf("read the user-path durable record: %v", err)
	}
	if len(userChain) == 0 || userChain[0].Spec.TriggerSource != "" {
		t.Fatalf("user-path task_spawned material = %+v, want no delegation stamp", userChain)
	}

	if !meditation.SelfManagedLineage(event.LineageDelegation) {
		t.Fatalf("meditation reads %q as host-facing, want self-managed", event.LineageDelegation)
	}
	if meditation.DeliverableLineage(event.LineageDelegation) {
		t.Fatalf("meditation delivers %q, want a self-managed stamp withheld from delivery", event.LineageDelegation)
	}
}

// lineageTasks is the task face of the lineage checks: the admission, obligation
// and flush faces of the manager beside the advance and lookup faces of the
// registry, so a delegated admission runs the real registration path.
type lineageTasks struct {
	*task.MemoryManager
	*task.MemoryRegistry
}

// lineageBorrow is the borrowing face of the lineage checks: it serves handles
// that hold no releasing work, so the checks read the ledger's own bookkeeping.
type lineageBorrow struct{}

// Borrow serves the handle of the generation a request begins under.
func (lineageBorrow) Borrow(context.Context, event.AgentID) (*orgstate.BindingHandle, error) {
	return &orgstate.BindingHandle{}, nil
}

// BorrowDerived serves the handle a delegated request inherits.
func (lineageBorrow) BorrowDerived(context.Context, orgstate.BindingRef) (*orgstate.BindingHandle, error) {
	return &orgstate.BindingHandle{}, nil
}

// lineageFactLog is the durable fact surface of the lineage checks: one in-memory
// key-value log whose sync barrier returns the context outcome.
type lineageFactLog struct {
	mu     sync.Mutex
	values map[string][]byte
}

// newLineageFactLog returns an empty fact log.
func newLineageFactLog() *lineageFactLog {
	return &lineageFactLog{values: map[string][]byte{}}
}

// Put stages one key and its value.
func (l *lineageFactLog) Put(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.values[key] = append([]byte(nil), value...)
	return nil
}

// Get returns the value stored under key and whether one is held.
func (l *lineageFactLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	value, ok := l.values[key]
	return append([]byte(nil), value...), ok, nil
}

// Scan returns every stored pair under the key prefix as key and value.
func (l *lineageFactLog) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string][]byte{}
	for key, value := range l.values {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			out[key] = append([]byte(nil), value...)
		}
	}
	return out, nil
}

// Sync completes the write barrier over everything staged so far.
func (l *lineageFactLog) Sync(ctx context.Context) error {
	return ctx.Err()
}
