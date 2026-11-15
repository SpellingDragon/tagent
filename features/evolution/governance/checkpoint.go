// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The governance state slice: the ledger's and the goal registry's derived state,
// the two event streams' tails and the approval book as one framework checkpoint
// slice. The governance events stay the truth of the governance history and the
// replay source; this slice is the state they already folded, so a restarted
// process reads a cut the size of the state it holds instead of scanning the
// stream it committed.
//
// The slice carries the cut beside the state it cut: the tail each stream ended
// at, so a restarted writer continues its numbering past the keys already written
// instead of re-keying over commitments, and a boot that finds a cut reads the
// events past it alone.
//
// The redline list (3 rules):
// MUST keep the governance events as the truth: the slice is derived, never the source, and no statement here rewrites or drops a committed event.
// MUST keep the stream keys at their 19-digit form, so the lexicographic order of the keys stays the order the operations happened in.
// MUST refuse a payload it does not recognize by name instead of restoring an empty state over a cut the process committed.
package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// stateSliceName is the name the governance state is keyed by in a checkpoint. It
// is the name the framework state seam aggregates the slice under and the name a
// restore has to answer once the wiring is complete.
const stateSliceName = "governance"

// stateSliceVersion is the layout version of the governance slice payload. A
// payload of another version is refused by name: a cut this code cannot read is
// not a cut it states as an empty one.
const stateSliceVersion = 1

// errStateUnreadable and errStateVersion report the two ways a stated governance
// cut refuses a restore: a body that is no governance state at all, and a state of
// a layout this code does not read. Neither falls back to an empty state, because
// an empty state beside a committed cut hands the streams' keys to a writer that
// would then re-key over events already stored.
var (
	errStateUnreadable = errors.New("governance: the state slice states no readable governance state")
	errStateVersion    = errors.New("governance: the state slice states a governance layout this code does not read")
)

// stateSliceWire is the payload of the governance state slice: the layout version,
// the tail each event stream ended at, the derived state those streams folded, and
// the approval requests the book holds.
type stateSliceWire struct {
	// Version is the payload layout this cut states.
	Version int `json:"version"`
	// DenialSeq is the sequence the denial stream ended at when the cut was taken.
	DenialSeq int64 `json:"denial_seq"`
	// GoalSeq is the sequence the goal stream ended at when the cut was taken.
	GoalSeq int64 `json:"goal_seq"`
	// GoalOrder is the declaration order the registry had reached: the sequence the
	// goal identities are minted in, kept apart from the event stream numbering.
	GoalOrder int `json:"goal_order"`
	// Records is the ledger index the denial stream folded, oldest first.
	Records []DenialRecord `json:"records,omitempty"`
	// Goals is every goal the registry held, ordered by identity.
	Goals []*Goal `json:"goals,omitempty"`
	// Approvals is every request the approval book held.
	Approvals []approval.ApprovalRequest `json:"approvals,omitempty"`
}

// StateSeat is the governance recoverable state as the framework sees it: the
// ledger and the goal registry that fold the two governance streams, and the
// approval book those rulings park calls on. The seat is what the composition
// mounts on the runtime's state seam, so one checkpoint cut states the governance
// state and one restore puts it back.
//
// 契约: docs/wiki/agent/governance-enforcement.md#denial-ledger
type StateSeat struct {
	ledger    *DenialLedger
	goals     *GoalRegistry
	approvals *approval.ApprovalManager
}

// NewStateSeat returns the governance state of one process as a state seam slice:
// the ledger, the goal registry and the approval book that stand behind it.
func NewStateSeat(ledger *DenialLedger, goals *GoalRegistry, approvals *approval.ApprovalManager) *StateSeat {
	return &StateSeat{ledger: ledger, goals: goals, approvals: approvals}
}

// StateName reports the name the governance state is keyed by in a checkpoint.
func (s *StateSeat) StateName() string { return stateSliceName }

// SnapshotState states the governance state as JSON: the tail each stream ended at,
// the derived index those streams folded and the approval book. The read takes the
// holders' own read locks and nothing else — it never flushes, commits or waits for
// a commit of its own — so the cut never stands between a governance write and its
// landing.
func (s *StateSeat) SnapshotState(_ context.Context) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	wire := stateSliceWire{Version: stateSliceVersion}
	if s.ledger != nil {
		wire.DenialSeq, wire.Records = s.ledger.stated()
	}
	if s.goals != nil {
		wire.GoalSeq, wire.GoalOrder, wire.Goals = s.goals.stated()
	}
	if s.approvals != nil {
		wire.Approvals = s.approvals.SnapshotEvidence()
	}
	return json.Marshal(wire)
}

// RestoreState puts one governance state back: the cut each stream continues from,
// the index the streams already folded and the approval requests the book held. The
// events past the cut are folded beside it when the durable face is bound
// (StateSeat.Bind), so a boot states the same governance as the process that cut it.
//
// A cut that states nothing is the zero cut: the streams continue from their first
// key, the events past it are the whole stream, and the approval book reads the
// request files, because a process that took no cut left them as its only record.
func (s *StateSeat) RestoreState(ctx context.Context, payload []byte) error {
	if s == nil {
		return nil
	}
	if statesNothing(payload) {
		if s.ledger != nil {
			s.ledger.seat(nil, 0)
		}
		if s.goals != nil {
			s.goals.seat(nil, 0, 0)
		}
		if s.approvals != nil {
			return s.approvals.RestoreBoot(ctx)
		}
		return nil
	}
	var wire stateSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return fmt.Errorf("%w: %v", errStateUnreadable, err)
	}
	if wire.Version != stateSliceVersion {
		return fmt.Errorf("%w: version %d, want %d", errStateVersion, wire.Version, stateSliceVersion)
	}
	if wire.DenialSeq < 0 || wire.GoalSeq < 0 || wire.GoalOrder < 0 {
		return fmt.Errorf("%w: negative watermark denial=%d goal=%d order=%d",
			errStateUnreadable, wire.DenialSeq, wire.GoalSeq, wire.GoalOrder)
	}
	if s.ledger != nil {
		s.ledger.seat(wire.Records, wire.DenialSeq)
	}
	if s.goals != nil {
		s.goals.seat(wire.Goals, wire.GoalOrder, wire.GoalSeq)
	}
	if s.approvals != nil {
		return s.approvals.RestoreEvidence(ctx, wire.Approvals)
	}
	return nil
}

// Bind hands the governance state the durable face the slice's cut belongs to: each
// stream folds the events that landed past the cut it restored, so the state this
// process serves is the state the cut plus its tail states. A nil log is a process
// with no durable tier: the state stays the memory state the restore seated.
func (s *StateSeat) Bind(ctx context.Context, log GovernanceEventLog) error {
	if s == nil || log == nil {
		return nil
	}
	if s.ledger != nil {
		if err := s.ledger.Bind(ctx, log); err != nil {
			return fmt.Errorf("governance: read the denial stream past the state cut: %w", err)
		}
	}
	if s.goals != nil {
		if err := s.goals.Bind(ctx, log); err != nil {
			return fmt.Errorf("governance: read the goal stream past the state cut: %w", err)
		}
	}
	return nil
}

// statesNothing reports whether one payload states no governance state at all: the
// empty cut the seam states for a holder it has nothing to say about.
func statesNothing(payload []byte) bool {
	trimmed := bytes.TrimSpace(payload)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
