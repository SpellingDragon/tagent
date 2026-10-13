package query

import (
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Skeleton compile-time assertions for the phase 1 query carriers. The phase 2
// implementation that will serve these carriers does not exist yet and no port
// interface is declared for it, so the assertions pin what the skeleton proves on
// its own: every declared field keeps its name and type and every declared
// vocabulary stays a compile-time constant.
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
		InvocationID: "",
		TaskID:       "",
		Notice:       task.Notice{},
		Lease:        state.LeaseRef{},
	}

	_ = Snapshot{
		Revision: 0,
		Details:  []Detail{},
	}

	_ = Detail{
		Ref:          ObjectRef{},
		State:        "",
		Blocked:      []task.BlockedObligation{},
		NextAdvancer: "",
	}

	_ = Subscription{
		Revision: 0,
		Filter:   ListFilter{},
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

	_ DetailState = DetailActive
	_ DetailState = DetailBlocked
	_ DetailState = DetailRetired
)
