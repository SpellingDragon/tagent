// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The task state slice: the account of the task registry as the framework
// checkpoint carries it. Every other slice states work the process holds in
// flight; this one states the tasks the registry is still driving, so a restarted
// run knows its open work before it reads a single chain. A task the run finished
// states nothing here, and the chain stays the truth of it: the slice spares the
// covered part of every open chain and nothing else, which is what keeps its body
// equal however many tasks the storage already settled.
package assembly

import (
	"context"

	"github.com/SpellingDragon/tagent/runtime/task"
)

// tasksStateSlice is the task registry's recoverable state as the framework
// sees it: the account of the registry beside the settle account the durable
// record of those tasks holds.
type tasksStateSlice struct {
	registry *task.MemoryRegistry
	// receipts is the durable settle account stated beside the registry's own, so
	// a boot reads the consumption receipts, attempts and refusals of the tasks the
	// checkpoint covers out of the checkpoint. It is nil beside a run whose commit
	// keeps no durable record, and the slice then states an empty account.
	receipts task.SettleAccount
}

// StateName reports the name the task slice is keyed by.
func (s tasksStateSlice) StateName() string { return checkpointSliceTasks }

// SnapshotState states the account of the registry: every task it still drives
// beside the chain revision each one stands at and the settle account the durable
// record of that task holds. A task the run finished is stated by nothing, so the
// account costs the work still open and no identity of the tasks behind it.
func (s tasksStateSlice) SnapshotState(ctx context.Context) ([]byte, error) {
	if s.registry == nil {
		return []byte("null"), nil
	}
	payload, err := s.registry.SnapshotCheckpoint(ctx)
	if err != nil {
		return nil, err
	}
	return task.StateSettleAccount(ctx, payload, s.receipts)
}

// RestoreState puts the open account back, so the boot folds only the chain links
// that landed past the cut instead of every task chain the storage holds. The
// durable record is attached beside it as the source the registry reads a task
// from when the checkpoint stated no entry for it and a reader names it.
func (s tasksStateSlice) RestoreState(ctx context.Context, payload []byte) error {
	if s.registry == nil {
		return nil
	}
	if chains, held := s.receipts.(task.ChainSource); held {
		s.registry.RestoreChainSource(chains)
	}
	return s.registry.RestoreCheckpoint(ctx, payload)
}
