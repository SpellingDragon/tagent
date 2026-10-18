package query

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Skeleton compile-time assertions for the query carriers. The assertions pin
// what the package proves on its own: every declared field keeps its name and
// type and every declared vocabulary stays a compile-time constant. The
// inventory carriers beside them are served by the read ports they declare, and
// those ports are asserted below against the owner faces that satisfy them.
var (
	_ = AuthorizationScope{
		Agents:   []event.AgentID{},
		Sessions: []event.Address{},
	}

	_ = ListFilter{
		Scope:    AuthorizationScope{},
		Agents:   []event.AgentID{},
		Sessions: []event.Address{},
		Kinds:    []ObjectKind{},
		States:   []DetailState{},
	}

	_ = ObjectRef{
		Kind:         ObjectAgent,
		Address:      event.Address{},
		InstanceID:   "",
		InvocationID: "",
		TaskID:       "",
		Notice:       task.Notice{},
		Lease:        state.LeaseRef{},
	}

	_ = Snapshot{
		Revision:   0,
		Watermarks: []SourceWatermark{},
		Details:    []Detail{},
		Truncated:  SnapshotTruncation{},
	}

	_ = Budget{
		MaxObjects: 0,
		MaxBytes:   0,
	}

	_ = SnapshotTruncation{
		OmittedObjects: 0,
	}

	_ = Detail{
		Ref:     ObjectRef{},
		State:   DetailState{},
		Blocked: []ObligationProgress{},
	}

	// The detail-state readings keep their declared shape: a renamed or
	// retyped reading field stops these blocks from compiling. Only the fields
	// belonging to the kind of the object carry a reading, so the structured
	// state replaces one aggregate label rather than adding fields beside it.
	_ = DetailState{
		Agent:       AgentState{},
		Session:     SessionState{},
		Decision:    "",
		TaskStatus:  0,
		NoticePhase: "",
		Holder:      state.LeaseRef{},
	}
	_ = AgentState{Configured: false, Resident: false, Busy: false}
	_ = SessionState{Accepted: 0, Claimed: 0, Completed: 0, Consumed: 0}

	_ = ObligationProgress{
		Obligation: task.BlockedObligation{},
		Advancer:   "",
		State:      "",
	}

	_ = Cursor{
		bound: cursorBinding{
			Revision: 0,
			Scope:    AuthorizationScope{},
			Filter:   ListFilter{},
		},
	}

	_ = SourceWatermark{
		Source:   "",
		Position: "",
		LagState: "",
		Lag:      SourceLag{},
	}

	_ = SourceLag{
		Amount: 0,
		Unit:   "",
		Origin: "",
	}

	_ = Update{
		Op:       "",
		Revision: 0,
		Ref:      ObjectRef{},
		Detail:   Detail{},
		Gap:      Gap{},
		Reset:    Reset{},
	}

	_ = Gap{
		From:   0,
		To:     0,
		Reason: "",
	}

	_ = Reset{
		Watermarks: []SourceWatermark{},
	}

	_ = Subscription{
		Revision: 0,
		Filter:   ListFilter{},
	}
)

// The named cursor refusals must stay errors a caller can match with errors.Is:
// replacing one with an untyped string stops these blocks from compiling.
var (
	_ error = ErrCursorRevisionMismatch
	_ error = ErrCursorScopeChanged
	_ error = ErrCursorFilterChanged
	_ error = ErrCursorWindowExpired
	_ error = ErrSnapshotBudgetExceeded
)

// The bounded read service keeps its declared construction surface: a renamed
// constructor, a retyped parameter or a drifted method signature stops these
// blocks from compiling. The production reading source and its frozen detail
// source keep satisfying the lazy materialization ports the budget walk is
// written against.
var (
	_ func(*Inventory, int) *Projection                                        = NewProjection
	_ func(ReadingSource, int, Budget) *Projection                             = NewBoundedProjection
	_ func(Reading) *Inventory                                                 = NewInventory
	_ func(*Inventory) ReadingSource                                           = NewInventorySource
	_ func(Detail) int                                                         = DetailBytes
	_ ReadingSource                                                            = inventorySource{}
	_ DetailSource                                                             = (*viewDetailSource)(nil)
	_ func(*Projection, context.Context, ListFilter) (Snapshot, Cursor, error) = (*Projection).Snapshot
)

// The single read port and its three semantic reading units keep their declared
// shape: a renamed method, a renamed field or a retyped reading stops these
// blocks from compiling.
var (
	_ = Generation{
		Agents: []event.AgentID{},
		Leases: []state.LeaseRef{},
	}

	_ = SiteReading{
		Session:    state.Session{},
		Invocation: "",
		Decision:   "",
		Held:       false,
	}

	_ = TaskReading{
		TaskID:      "",
		Spec:        task.Spec{},
		Status:      0,
		Obligations: task.Obligations{},
		Outbox:      []task.OutboxNotice{},
	}
)

// The declared vocabularies must stay compile-time constants: a variable or a
// computed value stops these blocks from compiling.
const (
	_ ObjectKind = ObjectAgent
	_ ObjectKind = ObjectSession
	_ ObjectKind = ObjectInvocation
	_ ObjectKind = ObjectTask
	_ ObjectKind = ObjectNotice
	_ ObjectKind = ObjectLease

	_ GapKind = GapWindowExpired
	_ GapKind = GapOverflow
	_ GapKind = GapDeleted

	_ AdvanceState = AdvanceWaiting
	_ AdvanceState = AdvanceMoving
	_ AdvanceState = AdvanceSettled

	_ WatermarkLagState = WatermarkCaughtUp
	_ WatermarkLagState = WatermarkLagKnown
	_ WatermarkLagState = WatermarkLagUnknown

	_ UpdateOp = UpdateUpsert
	_ UpdateOp = UpdateDelete
	_ UpdateOp = UpdateGap
	_ UpdateOp = UpdateReset

	_ NoticePhase = NoticePhaseOwed
	_ NoticePhase = NoticePhaseAccepted
	_ NoticePhase = NoticePhaseClaimed
	_ NoticePhase = NoticePhaseConsumed
)

// The named detail-state anchor must stay a declared value a caller can
// compare one detail reading against: a detail state is a structured reading
// and never a constant, but the anchor of the consumed notification is a
// declared value of the vocabulary and not a computed one.
var _ DetailState = DetailRetired

// The inventory carriers keep their declared shape: a renamed or retyped field
// stops these blocks from compiling.
var (
	_ = InventoryView{
		agents:      []AgentEntry{},
		sessions:    []SessionEntry{},
		invocations: []InvocationEntry{},
		tasks:       []TaskEntry{},
		notices:     []NoticeEntry{},
		leases:      []LeaseEntry{},
	}

	_ = AgentEntry{AgentID: "", Configured: false, Resident: false, Busy: false}
	_ = SessionEntry{Address: event.Address{}, Instance: "", Accepted: 0, Claimed: 0, Completed: 0, Consumed: 0}
	_ = InvocationEntry{InvocationID: "", Address: event.Address{}, Instance: "", Decision: "", Pending: []task.Notice{}}
	_ = TaskEntry{TaskID: "", Spec: task.Spec{}, Status: 0, Obligations: task.Obligations{}}
	_ = NoticeEntry{Notice: task.Notice{}, Target: event.Address{}, TargetInstance: "", Phase: "", Delivery: 0, AcceptSeq: 0, Claim: 0, Completed: false, Blocked: []task.BlockedObligation{}}
	_ = LeaseEntry{Lease: state.LeaseRef{}}

	_ = InventoryCounts{Agents: AgentCounts{}, Sessions: 0, Invocations: 0, Tasks: 0, Notices: 0, Leases: 0}
	_ = AgentCounts{Configured: 0, Resident: 0, Busy: 0}

	_       = SourceError{Dimension: "", Cause: nil}
	_ error = ErrSourceUnreadable
)

// The subscription read port keeps its declared shape: a renamed method or a
// retyped parameter of Follow stops this block from compiling.
var _ func(*Projection, context.Context, Subscription, int) (Subscription, []Update, error) = (*Projection).Follow

// The default budget must stay a declared value a caller can read: it is the
// budget every projection is built under when its caller names none.
var _ Budget = DefaultBudget
