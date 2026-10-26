// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Package query is the runtime's read-only inspection projection: it carries the
// bounded, immutable view through which an already committed runtime state is
// listed, described and followed across agents, sessions, invocations, tasks,
// notices and resource leases.
//
// The projection is never a second source of truth and never grants
// authorization of its own: it reads the facts the state, task, memory and plugin
// owners already hold, filters them by the scope the caller already has, and
// answers with immutable snapshots whose counts are derived from the detail
// entries rather than pre-aggregated. This is the phase 1 skeleton: it declares
// shapes and holds no behaviour.
package query

import (
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// AuthorizationScope is the set of objects a caller is already authorized to
// read. It is carried into a filter and is never widened by one: an empty scope
// reads nothing, and a dimension outside the scope is excluded rather than
// rewritten.
type AuthorizationScope struct {
	// Agents lists the agent identities the scope covers.
	Agents []event.AgentID
	// Sessions lists the session addresses the scope covers. A session is always
	// scoped to one agent, so it is named by its full address.
	Sessions []event.Address
}

// ListFilter is the one filter a list, detail or subscription request carries:
// the authorization scope the caller already holds together with the dimension
// filters that narrow the projection.
//
// The authorization scope is applied before every dimension filter, so an object
// the caller may not read is excluded rather than rewritten, and no dimensions
// are summed together. The agent dimensions in particular stay separate: a
// configured agent, a resident agent and a busy agent are three distinct readings
// taken from the detail entries, never one "running" number obtained by adding
// the three counts together.
type ListFilter struct {
	// Scope is the authorization scope the caller already holds.
	Scope AuthorizationScope
	// Agents narrows the projection to the named agent identities.
	Agents []event.AgentID
	// Sessions narrows the projection to the named session addresses.
	Sessions []event.Address
	// Kinds narrows the projection to the named object kinds.
	Kinds []ObjectKind
	// States narrows the projection to the named detail states. One named
	// state is a complete reading of one kind and keeps exactly the entries
	// that read the same.
	States []DetailState
}

// ObjectKind names the kind of object a reference or a detail entry describes.
type ObjectKind string

const (
	// ObjectAgent is an agent object.
	ObjectAgent ObjectKind = "agent"
	// ObjectSession is a session object.
	ObjectSession ObjectKind = "session"
	// ObjectInvocation is an invocation object.
	ObjectInvocation ObjectKind = "invocation"
	// ObjectTask is a task object.
	ObjectTask ObjectKind = "task"
	// ObjectNotice is a notice object.
	ObjectNotice ObjectKind = "notice"
	// ObjectLease is a resource lease object.
	ObjectLease ObjectKind = "lease"
)

// ObjectRef names one object of the projection without carrying its detail: the
// kind of object it is together with the identity that names it.
//
// Only the identity field matching Kind is populated, and that identity stays in
// the typed form its kind already defines, so no second, flattened identity
// vocabulary is introduced beside the event, task and state identifiers. Two
// references name one object by that identity, never by struct equality:
//   - two references name the same object when Kind and the identity fields of that kind match
//   - an agent is named by Address.AgentID, a session by Address and InstanceID
//   - an invocation is named by InvocationID, a task by TaskID, a lease by Lease.Kind and Lease.Key
//   - a notice is named by the TaskID, Round, Kind and Seq of Notice
//   - every other field of a reference is content beside that identity and takes no part in equality
//   - two agent references that differ only in SessionID name one agent, not two
//   - compare references by that identity, never by struct equality or field by field: a key of the whole struct splits one object into several
//   - the stable identity order of a reading sorts by this identity, kinds first and then the identity fields of the kind (see DetailSource.Order)
type ObjectRef struct {
	// Kind names the kind of object the reference describes.
	Kind ObjectKind
	// Address is the explicit agent session the object belongs to.
	Address event.Address
	// InstanceID names the runtime instance the object belongs to: a reference
	// never names an object without saying which runtime instance holds it, and
	// it uses the same instance identity the event vocabulary already defines
	// rather than a second one invented here.
	InstanceID event.InstanceID
	// InvocationID names an invocation object; it is empty for another kind.
	InvocationID event.InvocationID
	// TaskID names a task object; it is empty for another kind.
	TaskID event.TaskID
	// Notice names a notice object; it is empty for another kind.
	Notice task.Notice
	// Lease names a resource lease object; it is empty for another kind.
	Lease state.LeaseRef
}

// Cursor is the opaque continuation token of one bounded, paged read.
//
// A cursor is minted together with the read that produced it and stays bound to
// that read's revision, authorization scope and filter. Presenting it under
// another authorization scope or under another filter is refused by name
// (ErrCursorScopeChanged, ErrCursorFilterChanged), replaying it at another
// revision is refused by name (ErrCursorRevisionMismatch), and presenting one
// whose bounded read window has expired is refused as ErrCursorWindowExpired.
// No path silently serves a page from another view: every refusal is named so
// the caller can tell a changed authorization from a changed filter, a replayed
// revision and an expired window. The token is opaque and cannot be
// constructed or inspected outside this package: it exposes no field, the zero
// value is not a usable cursor, and the revision, scope and filter it was
// minted under stay inside it. The scope a cursor is bound to is the scope its
// caller already held when the read ran; carrying a cursor never widens that
// scope and a cursor never authorizes a read of its own. A page of a list read
// is a bounded run of detail entries of one snapshot revision continued by a
// cursor, so paging stays on one stable sequence while the underlying sources
// advance underneath.
type Cursor struct {
	bound cursorBinding
}

// cursorBinding is the identity one cursor answers for: the revision, the
// authorization scope and the filter of the read that minted it. It is kept
// unexported so no caller can forge a cursor or read its binding; the read
// service checks it against the request a cursor is presented with and refuses
// a mismatch by name.
type cursorBinding struct {
	// Revision is the snapshot revision the minting read was anchored to.
	Revision Revision
	// Scope is the authorization scope the minting read ran under.
	Scope AuthorizationScope
	// Filter is the dimension filter the minting read ran under. It carries the
	// same scope, so a change of authorization and a change of filter are
	// refused separately rather than collapsing into one mismatch.
	Filter ListFilter
}

// ErrCursorRevisionMismatch, ErrCursorScopeChanged, ErrCursorFilterChanged and
// ErrCursorWindowExpired are the named cursor refusals: a cursor that does not
// match the read it is presented with is refused by name and never answered
// with a wrong page.
var (
	// ErrCursorRevisionMismatch refuses a cursor replayed at another revision
	// than the one it was minted at: the page it would continue belongs to
	// another read.
	ErrCursorRevisionMismatch = errors.New("query: the cursor was minted at another revision")
	// ErrCursorScopeChanged refuses a cursor presented under another
	// authorization scope than the one it was minted under: a cursor never
	// survives a change of authorization.
	ErrCursorScopeChanged = errors.New("query: the cursor was minted under another authorization scope")
	// ErrCursorFilterChanged refuses a cursor presented under another filter
	// than the one it was minted under: a cursor never survives a change of
	// filter.
	ErrCursorFilterChanged = errors.New("query: the cursor was minted under another filter")
	// ErrCursorWindowExpired refuses a cursor whose bounded read window has
	// expired: the expiry is named instead of continuing from a position outside
	// the servable window.
	ErrCursorWindowExpired = errors.New("query: the cursor's read window has expired")
	// ErrSnapshotBudgetExceeded refuses a read whose demand is larger than the
	// single-snapshot object budget can hold faithfully. The refusal is named so
	// an oversized request is told apart from an empty view: a page larger than
	// the whole budget is never answered with a silently shortened one.
	ErrSnapshotBudgetExceeded = errors.New("query: the request exceeds the single-snapshot budget")
)

// SourceName names one backend source the projection reads from. It names an
// owner the runtime already has and introduces no new source registry; sources
// are read side by side and never added together into one global position.
type SourceName string

// SourcePosition is one source's own position in its own stream. The value is
// opaque: it is source-private, it is never parsed, and two positions of
// different sources are never compared or subtracted to fabricate a global lag
// across backends. A position says only where one source stands; how far that
// source lags the snapshot revision is carried by the lag state beside it and
// never folded into the position itself. The zero value is no position at all.
// A position is carried in the text form its source already names, written as
// SourcePosition("...") and never read back field by field.
type SourcePosition string

// WatermarkLagState names which of the three readings a source's lag carries:
// the source has caught up with the snapshot revision, it lags by a known
// amount, or it lags by an amount nobody has measured. The three readings stay
// apart: an unknown lag is its own state, it is never folded into the position,
// never reduced to a boolean and never reported as a zero lag.
type WatermarkLagState string

const (
	// WatermarkCaughtUp names a source that has reached the snapshot revision,
	// so there is no lag to report.
	WatermarkCaughtUp WatermarkLagState = "caught-up"
	// WatermarkLagKnown names a source that lags the snapshot revision by a
	// measured amount, carried in Lag together with the unit and the origin of
	// the measurement.
	WatermarkLagKnown WatermarkLagState = "lag-known"
	// WatermarkLagUnknown names a source that lags the snapshot revision by an
	// amount nobody has measured. The unknown lag is expressed as this state and
	// as nothing else: not as a position, not as a boolean and not as a zero
	// amount in Lag.
	WatermarkLagUnknown WatermarkLagState = "lag-unknown"
)

// SourceLag is a measured lag of one source behind the snapshot revision. A lag
// is carried only when its amount is known, and the amount always says its unit
// and where the measurement came from; a lag nobody measured is carried as
// WatermarkLagUnknown instead of being reported as a zero amount.
type SourceLag struct {
	// Amount is the measured distance of the source behind the snapshot
	// revision.
	Amount uint64
	// Unit names the unit Amount is measured in (for example revisions or
	// events); an amount is never reported without its unit.
	Unit string
	// Origin names the party that produced the measurement (for example the
	// source owner or the read service); an amount is never reported without
	// saying where it came from.
	Origin string
}

// SourceWatermark is one source's position in the projection beside what is
// known about how far that source lags the snapshot revision. The projection
// carries one watermark per source and never merges them into one global
// position: the sources advance independently and the view never claims one
// atomic read across every backend.
type SourceWatermark struct {
	// Source names the backend source this watermark is read from.
	Source SourceName
	// Position is the opaque position the source stands at.
	Position SourcePosition
	// LagState names which of the three readings the lag of this source
	// carries: caught up, lag known or lag unknown.
	LagState WatermarkLagState
	// Lag reports the measured lag and is meaningful only when LagState is
	// WatermarkLagKnown; it carries no amount under either of the other two
	// readings.
	Lag SourceLag
}

// Budget bounds what one snapshot read materializes: the largest number of
// snapshot objects and the largest number of estimated bytes of snapshot
// objects one read holds. A read materializes one snapshot object at a time and
// checks the object bound before it builds the next one, so the objects one
// read materializes never exceed MaxObjects, and an object whose size would
// carry the held bytes beyond MaxBytes is the named boundary of the view
// instead of being held silently.
//
// A zero bound is not a bound at all: a Budget with both fields zero reads the
// complete view, the same way a window below one reading still keeps one
// reading. The budget bounds snapshot objects and never the frozen source
// reading behind them: it answers "how large may one snapshot grow" and never
// re-opens or re-reads the owners underneath.
type Budget struct {
	// MaxObjects is the largest number of snapshot objects one read may
	// materialize; zero materializes without an object bound.
	MaxObjects int
	// MaxBytes is the largest number of estimated bytes the snapshot objects of
	// one read may hold; zero holds without a byte bound.
	MaxBytes int
}

// DefaultBudget is the budget a projection is built under when its caller names
// none: one snapshot holds at most this many objects and this many estimated
// bytes, so an unbounded inventory still answers with a bounded, named view
// instead of an unbounded one.
var DefaultBudget = Budget{MaxObjects: 4096, MaxBytes: 4 << 20}

// SnapshotTruncation names the part of one filtered view the snapshot budget
// left out beside the view it did answer with, so a bounded view is never
// mistaken for the complete one and no object is dropped in silence. The zero
// value reports a complete view: every entry the filter keeps is held.
type SnapshotTruncation struct {
	// OmittedObjects counts the identities the reference dimensions of the
	// filter keep that the budget left out of the view, in the stable identity
	// order of the reading. With a state dimension active the count names the
	// candidates the reference dimensions keep: the state of an unmaterialized
	// entry is unknown by construction, so the count is the honest extent of the
	// omission rather than a guess at how many of those candidates the state
	// dimension would have kept.
	OmittedObjects int
}
