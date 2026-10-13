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
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
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
	// States narrows the projection to the named detail states.
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
// vocabulary is introduced beside the event, task and state identifiers.
type ObjectRef struct {
	// Kind names the kind of object the reference describes.
	Kind ObjectKind
	// Address is the explicit agent session the object belongs to.
	Address event.Address
	// InvocationID names an invocation object; it is empty for another kind.
	InvocationID event.InvocationID
	// TaskID names a task object; it is empty for another kind.
	TaskID event.TaskID
	// Notice names a notice object; it is empty for another kind.
	Notice task.Notice
	// Lease names a resource lease object; it is empty for another kind.
	Lease state.LeaseRef
}
