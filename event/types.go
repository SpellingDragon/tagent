// 契约: docs/wiki/event/event-architecture.md#overview
//
// Package event is the runtime's event identity and fact carrier: it defines
// the typed identifiers an event carries, the address of the agent session an
// event belongs to, and the event payload itself.
//
// The payload keeps the native locked tRPC model message, so no second message
// representation is built beside it. Every identifier is a distinct string type,
// so a session identifier cannot be passed where an agent identifier is
// expected.
package event

import (
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// AgentID identifies an agent inside an application.
type AgentID string

// SessionID identifies one conversation session of an agent.
type SessionID string

// EventID is the canonical identity of a stored event.
type EventID string

// InvocationID identifies one responsibility scope: a trusted admission or a
// delegation entry. One session may hold several invocations at the same time.
type InvocationID string

// TaskID identifies one task across every execution round it runs.
type TaskID string

// InstanceID identifies one runtime instance. Rebuilding an agent under the same
// name yields a different instance.
type InstanceID string

// EpisodeID identifies an application level episode and its causal lineage; a
// session or a trace is never substituted for it.
type EpisodeID string

// DecisionID identifies one model decision boundary.
type DecisionID string

// AttemptID identifies one observable model attempt; a retry hidden below the
// decision boundary does not fake a new attempt.
type AttemptID string

// Address names the agent session a payload belongs to. It is an explicit
// target: neither a default nor a most-recent session is substituted for it.
type Address struct {
	// AgentID identifies the agent.
	AgentID AgentID
	// SessionID identifies the session of that agent.
	SessionID SessionID
}

// IsZero reports whether the address names no agent and no session.
func (a Address) IsZero() bool {
	return a.AgentID == "" && a.SessionID == ""
}

// IsComplete reports whether the address names both an agent and a session.
func (a Address) IsComplete() bool {
	return a.AgentID != "" && a.SessionID != ""
}

// Event is one immutable runtime fact: the canonical key it is stored under, its
// frozen type, the time it was recorded, its metadata projection, the native
// model payload it carries, and the identities that scope it.
//
// G0 (payload carrying) is not settled yet: if the design rules for a local
// shadow message, the Message field below changes to that local type instead of
// the native model message.
type Event struct {
	// Key is the canonical identity of the event, in its hex form.
	Key EventID
	// Type is the frozen event type, one of the Type constants of this package.
	Type string
	// Timestamp is the time the event was recorded.
	Timestamp time.Time
	// Meta carries the event metadata entries keyed by the MetaKey constants of
	// this package.
	Meta map[string]string
	// Message is the native model payload the event carries.
	Message model.Message

	// Owner is the address that owns the event.
	Owner Address
	// Source is the address the event came from; the source instance and the
	// notification target instance are kept apart.
	Source Address
	// Target is the address the event is delivered to, required and never
	// defaulted.
	Target Address
	// InvocationID scopes the event to one responsibility range.
	InvocationID InvocationID
	// TaskID is the task the event belongs to; it is empty for an event outside
	// a task.
	TaskID TaskID
	// InstanceID is the runtime instance the event belongs to. The deduplication
	// scope is (InstanceID, InvocationID, DecisionID, CallID).
	InstanceID InstanceID
	// EpisodeID is the application level episode the event belongs to; it is
	// never derived from a session or a trace.
	EpisodeID EpisodeID
	// DecisionID scopes the event to one model decision.
	DecisionID DecisionID
	// AttemptID scopes the event to one observable model attempt.
	AttemptID AttemptID
	// CallID is the native provider call identifier, kept byte-identical and
	// never regenerated. It is not globally unique, so the deduplication scope
	// always includes InstanceID, InvocationID and DecisionID with it.
	CallID string
}
