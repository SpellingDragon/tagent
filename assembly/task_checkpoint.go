// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The task state slice: the account of the task registry as the framework
// checkpoint carries it. Every other slice states work the process holds in
// flight; this one states the tasks the registry is still driving, so a restarted
// run knows its open work before it reads a single chain. A task the run finished
// states nothing here, and the chain stays the truth of it: the slice spares the
// covered part of every open chain and nothing else, which is what keeps its body
// equal however many tasks the storage already settled.
//
// The slice is also the owner of the restore prerequisite that account carries:
// it seats the durable account from the payload it states before any holder
// restores the state that reads it (see SeatRestore), so the boot that restores
// the mailbox reads a seated account without the coordinator reading what this
// slice states.
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/state"
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
	// account is the durable account this slice seats, and tier is the storage the
	// seat bounds its tail at. Both are nil beside a run whose commit keeps no
	// durable record: the slice then states an empty account and declares no seat.
	account *state.DurableCommit
	tier    *ledger.Tier
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

// SeatRestore seats the durable account from the payload this slice states before
// any holder is handed a slice: the mailbox reads the notifications that account
// still owes, so an account seated after that holder restores leaves every notice
// past the cut unseated and undelivered, which is a work item a restart silently
// loses.
//
//   - The payload is this slice's own, read by the name the slice states, so the coordinator that seats the account names no slice of any consumer.
//   - A cut that states no tasks slice, a walk that finds no framework checkpoint and a storage that states no commit order seat the account from the whole log instead.
//   - The account is seated once per boot, so a second cut restored beside the first leaves the account the first seat read as it stands.
func (s tasksStateSlice) SeatRestore(ctx context.Context, slices []event.CheckpointSlice) error {
	if s.account == nil || s.account.Seated() {
		return nil
	}
	var stated json.RawMessage
	for _, slice := range slices {
		if slice.Name == s.StateName() {
			stated = slice.Payload
			break
		}
	}
	tail, err := tailFactKeys(ctx, s.tier)
	if err != nil {
		return fmt.Errorf("tagent: walk the commit order of the task account: %w", err)
	}
	if err := s.account.Seat(ctx, stated, tail); err != nil {
		return fmt.Errorf("tagent: seat the durable task account of the checkpoint: %w", err)
	}
	stateSeatedAccount(s.account, tail)
	return nil
}

// stateSeatedAccount states what a boot read back by identity when it seated the
// task account: the tasks the account holds, the size of the tail the storage
// stated for those reads, and the reads and the keys they answered. A tail of
// none is the boot that read the whole log, so its reads are the family scans
// beside a storage that states no cut; a seated account that read no key states
// zero, which is the checkpoint covering every task the account holds.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func stateSeatedAccount(durable *state.DurableCommit, tail map[string]struct{}) {
	scans := durable.LoadStats().Scans
	keys := 0
	for _, scan := range scans {
		keys += scan.Keys
	}
	cut := "none"
	if tail != nil {
		cut = strconv.Itoa(len(tail))
	}
	assemblyInfo("tagent: boot seated the task account tasks=%d tail=%s reads=%d keys=%d",
		len(durable.TaskIDs()), cut, len(scans), keys)
}
