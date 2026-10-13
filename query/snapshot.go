package query

import "github.com/SpellingDragon/tagent/task"

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Revision is the bounded snapshot revision one read is anchored to. Every list,
// detail and cursor of that read shares it, so paging stays on one stable
// sequence instead of drifting as the underlying sources advance.
type Revision uint64

// Snapshot is one bounded, immutable view of the projection: the revision the
// view was read at and the detail entries it contains.
//
// The view is bounded and immutable once read, and every count the projection
// reports is derived from these detail entries rather than pre-aggregated beside
// them, so a host page, a model and a training reader all read the same derived
// numbers from the same entries. The view reports the source revision it read
// and never claims one atomic snapshot across every backend.
type Snapshot struct {
	// Revision is the snapshot revision every detail entry of this view shares.
	Revision Revision
	// Details lists one entry per object in the filtered view.
	Details []Detail
}

// Detail is the projected description of one object: its state, the obligations
// of it that cannot proceed, and the party that can move it forward next.
type Detail struct {
	// Ref names the object this detail describes.
	Ref ObjectRef
	// State is the state of the object as the projection read it.
	State DetailState
	// Blocked lists the obligations of the object that cannot proceed, each with
	// the reason it is blocked.
	Blocked []task.BlockedObligation
	// NextAdvancer names the party that can move a blocked object forward.
	NextAdvancer Advancer
}

// DetailState is the state of one object as the projection reads it. The
// vocabulary is per kind: an agent reads as configured, resident or busy, a task
// reads as its task status, a notice reads as its delivery phase, and a lease
// reads as its holder. The states are kept apart and never collapse into one
// aggregate "running" reading.
type DetailState string

const (
	// DetailActive is an object the projection read as live and in use.
	DetailActive DetailState = "active"
	// DetailBlocked is an object whose obligation cannot proceed, so the party
	// named by NextAdvancer must move it forward.
	DetailBlocked DetailState = "blocked"
	// DetailRetired is an object that was retired or closed, so it receives no
	// new input.
	DetailRetired DetailState = "retired"
)

// Advancer names the next party that can move a blocked object forward: the
// executor, notification transport or storage action the projection identifies
// as the one that will advance it.
type Advancer string
