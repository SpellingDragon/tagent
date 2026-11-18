// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The governance state of a host as the framework checkpoint carries it: the
// ledger and the goal registry that fold the two governance streams and the
// approval book the rulings park calls with. The host seats that state on the
// run's state seam, so one cut states it and one restore puts it back — a
// restarted process reads a state the size of the governance state it holds
// instead of the stream and the request files it committed.
//
// The seat and the streams are one recovery: the cut states where each stream
// ended and the index it folded, and the events past the cut are the tail the
// bind folds beside it. A process whose checkpoint states no governance cut
// restores the zero cut, so the tail past it is the whole stream and the
// approval book reads the request files — the same state a young run starts from.
package assembly

import (
	"context"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// hostApprovalSeat reports the options one boot-local approval book is built
// beside. A host that seats the governance state marks the book seated, so the
// book reads no request file at construction and the restore of that state puts
// it back; a host beside a caller policy seats nothing and keeps the book's own
// restore from its files.
func hostApprovalSeat(seated bool) []approval.ApprovalOption {
	if !seated {
		return nil
	}
	return []approval.ApprovalOption{approval.WithApprovalSeat()}
}

// seatGovernance mounts the governance state on the run's state seam and hands it
// the durable face its cut belongs to: the cut the checkpoint states is restored
// on the mount, the events that landed past it are folded beside it, and a boot
// whose checkpoint states no governance state restores the zero cut first so the
// tail past it rebuilds the state from the whole stream.
//
// The mount is what makes the governance state the framework's: a slice no cut
// names waits nowhere, and a cut this host cannot seat is refused by the pending
// restore the boot reads after it.
func (h *Host) seatGovernance(ctx context.Context, seat *governance.StateSeat) error {
	if h == nil || h.run == nil || seat == nil {
		return nil
	}
	run := h.run
	stated := pendingStateSlice(run, seat.StateName())
	if _, err := run.RegisterStateSlice(seat); err != nil {
		return err
	}
	if !stated {
		if err := seat.RestoreState(ctx, nil); err != nil {
			return err
		}
	}
	var log durable.FactLog
	if run.tier != nil {
		log = ledger.TierFactLog(run.tier)
	}
	return seat.Bind(ctx, log)
}

// pendingStateSlice reports whether the checkpoint this boot restored named one
// state slice no mounted holder answered yet: the payload waits in the seam and
// lands the moment its holder mounts.
func pendingStateSlice(run *Run, name string) bool {
	for _, pending := range run.StateRegistry().PendingRestore() {
		if pending == name {
			return true
		}
	}
	return false
}
