// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The task state slice: the account of the task registry as the framework
// checkpoint carries it. Every other slice states work the process holds in
// flight; this one states the tasks it settled beside the ones it is still
// driving, so a restarted run knows its tasks before it reads a single chain.
// The chain stays the truth of a task and the source of the links that landed
// past the cut: the slice only spares the covered part of it, which is what
// stops the recovery cost from growing with the tasks the storage already
// settled.
package assembly

import (
	"context"

	"github.com/SpellingDragon/tagent/runtime/task"
)

// tasksStateSlice is the task registry's recoverable state as the framework
// sees it.
type tasksStateSlice struct {
	registry *task.MemoryRegistry
}

// StateName reports the name the task slice is keyed by.
func (s tasksStateSlice) StateName() string { return checkpointSliceTasks }

// SnapshotState states the account of the registry: every task it holds beside
// the chain revision each one stands at.
func (s tasksStateSlice) SnapshotState(ctx context.Context) ([]byte, error) {
	if s.registry == nil {
		return []byte("null"), nil
	}
	return s.registry.SnapshotCheckpoint(ctx)
}

// RestoreState puts the account back, so the boot folds only the chain links
// that landed past the cut instead of every task chain the storage holds.
func (s tasksStateSlice) RestoreState(ctx context.Context, payload []byte) error {
	if s.registry == nil {
		return nil
	}
	return s.registry.RestoreCheckpoint(ctx, payload)
}
