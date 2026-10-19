// 契约: docs/wiki/event/event-architecture.md#overview
// 契约: docs/FACT-VOCABULARY.md#fact-category
//
// Package event is the runtime's event identity and fact carrier: it defines
// the typed identifiers an event carries, the address of the agent session an
// event belongs to, and the event payload itself.
//
// The payload keeps the native locked tRPC model message, so no second message
// representation is built beside it. Every identifier is a distinct string type,
// so a session identifier cannot be passed where an agent identifier is
// expected. The package also declares Fact, the fact category contract each fact
// family satisfies in its own package; the families are registered in the fact
// vocabulary page.
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
// model payload it carries, and the identities that scope it. Its fields form
// four blocks — identity, content, ownership and ledger — and the doc of the
// first field of each names the block and what its fields share.
//
// G0 (payload carrying) is not settled yet: if the design rules for a local
// shadow message, the Message field below changes to that local type instead of
// the native model message.
type Event struct {
	// Key is the canonical identity of the event, in its hex form. It heads the
	// identity block, with Type and Timestamp: what the stored fact is on its
	// own. An event is quoted by Key alone; Type and Timestamp describe that
	// fact and never re-identify it.
	Key EventID
	// Type is the frozen event type, one of the Type constants of this package.
	Type string
	// Timestamp is the time the event was recorded.
	Timestamp time.Time

	// Meta carries the event metadata entries keyed by the MetaKey constants of
	// this package. It heads the content block, with Message: what the event
	// carries. Meta and Message are two faces of one fact, and neither is a
	// second representation of the other.
	Meta map[string]string
	// Message is the native model payload the event carries.
	Message model.Message

	// Owner is the address that owns the event. It heads the ownership block,
	// with Source, Target, InvocationID, TaskID, InstanceID, EpisodeID,
	// DecisionID and AttemptID: the addresses and identifiers that place the
	// event.
	//
	// Source and Target are the addresses the event came from and is delivered
	// to; the source instance and the notification target instance are kept
	// apart, and Target is never defaulted. The identifiers scope the event to
	// one responsibility range, task, runtime instance, episode, model decision
	// and model attempt, and stay empty where the event falls outside of them.
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
	// never regenerated. It heads the ledger block, with ApprovalID; those two
	// keys are what the deduplication scope and the approval record book the
	// call by. It is not globally unique, so the deduplication scope always
	// includes InstanceID, InvocationID and DecisionID with it.
	CallID string
	// ApprovalID is the approval ticket identity of the call this event records:
	// tagent issues it when the host parks the call in a hold, and it is globally
	// unique. It is empty when the call needs no approval.
	ApprovalID string
}
