package tagent

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// memoryPublications keeps the publication records of one assembled run in
// memory. It is the recorder port the sole publication entry commits through;
// the durable ledger of the history domain is not wired into this minimal
// entry yet.
type memoryPublications struct {
	mu      sync.Mutex
	records []state.PublicationRecord
}

// CommitPublication stores one publication record.
func (m *memoryPublications) CommitPublication(_ context.Context, record state.PublicationRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, record)
	return nil
}

// commitCarrier is the fact carrier behind one run's commit port: the commit
// port itself beside the notification ledger the close and the diagnostic read
// settle and list through. Both the in-memory commit and the durable commit
// satisfy it, so the run keeps one shape over either carrier.
type commitCarrier interface {
	task.Commit
	Outbox(taskID event.TaskID) []task.OutboxNotice
}

// runCommit is the commit carrier of one assembled run: every task fact is
// stored in the commit ledger behind it — the in-memory ledger of the task
// layer or the durable one the configured storage tier opens — and the
// identity of each task that committed a fact is remembered beside it, so the
// diagnostic read face lists the tasks the run holds without keeping a second
// copy of their content.
type runCommit struct {
	inner commitCarrier
	mu    sync.Mutex
	held  map[event.TaskID]bool
}

// The carrier is the very commit port the registry submits through.
var _ task.Commit = (*runCommit)(nil)

// Commit stores one task fact and remembers the identity of its task.
func (c *runCommit) Commit(ctx context.Context, record task.CommitRecord) (task.CommitResult, error) {
	result, err := c.inner.Commit(ctx, record)
	if result == task.CommitCommitted && record.TaskID != "" {
		c.mu.Lock()
		c.held[record.TaskID] = true
		c.mu.Unlock()
	}
	return result, err
}

// Declare stores the admission material of one task beside its first commit
// when the carrier behind this run holds a durable chain, so every fact chain
// the storage keeps is self-contained and a cold start rebuilds the task from
// material the chain already carries. A carrier beside no durable chain stores
// nothing and keeps the admission material in the registry of the run alone.
func (c *runCommit) Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error {
	type declarer interface {
		Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error
	}
	if d, ok := c.inner.(declarer); ok {
		return d.Declare(ctx, taskID, spec)
	}
	return nil
}

// TaskIDs reports the identity of every task this run holds, in identity order.
func (c *runCommit) TaskIDs() []event.TaskID {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]event.TaskID, 0, len(c.held))
	for id := range c.held {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// runOwners resolves the owner of a restored target under the current
// orchestration binding of this run. The formal root's task vocabulary — the
// wrapped call and the delegation alike — records a task's source and target as
// the address of its own owner, so a target that names its owner resolves to
// that owner while the binding still holds it; a target naming a member of the
// owner's tool set resolves through the binding owners, and both are refused by
// name when the current binding holds neither.
type runOwners struct{ bindings *state.Bindings }

// The formal root resolves restored targets through this very face.
var _ task.OwnerBinding = runOwners{}

// OwnerOf resolves one restored target against the current binding.
func (o runOwners) OwnerOf(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error) {
	if target == owner {
		handle, err := o.bindings.Borrow(ctx, owner)
		if err != nil {
			return "", fmt.Errorf("%w: owner %s target %s: %v", task.ErrTargetRemoved, owner, target, err)
		}
		defer handle.Release(ctx)
		return owner, nil
	}
	return state.NewBindingOwners(o.bindings).OwnerOf(ctx, owner, target)
}

// remember records one task identity the run holds, so a task the assembly
// restored from a durable chain is listed beside the tasks it admitted itself.
func (c *runCommit) remember(taskID event.TaskID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held[taskID] = true
}

// Outbox reports the notifications one stored commit record still owes, each
// beside the delivery-layer state it stands in.
func (c *runCommit) Outbox(taskID event.TaskID) []task.OutboxNotice {
	return c.inner.Outbox(taskID)
}

// closeFace is the task face the close of this run settles through: the manager
// admits and settles the obligations and the registry lands the transitions,
// exactly the faces the wrapped tools of this run are driven over.
type closeFace struct {
	manager  *task.MemoryManager
	registry *task.MemoryRegistry
}

// The close face settles its delegations through the very task face this run
// wraps its tools over.
var _ agent.DelegationTasks = closeFace{}

// Admit admits one delegated call as a task through the manager.
func (f closeFace) Admit(ctx context.Context, spec task.Spec) (task.AdmissionReceipt, error) {
	return f.manager.Admit(ctx, spec)
}

// Advance moves one task to the next status through the registry.
func (f closeFace) Advance(ctx context.Context, taskID event.TaskID, to task.Status, mutation task.MutationID, outbox []task.Envelope) (task.CommitResult, error) {
	return f.registry.Advance(ctx, taskID, to, mutation, outbox)
}

// Obligations reports the obligations the manager still holds for one task.
func (f closeFace) Obligations(ctx context.Context, taskID event.TaskID) (task.Obligations, error) {
	return f.manager.Obligations(ctx, taskID)
}

// Flush delivers the notifications of one task through the manager.
func (f closeFace) Flush(ctx context.Context, taskID event.TaskID) error {
	return f.manager.Flush(ctx, taskID)
}
