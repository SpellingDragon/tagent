package meditation

import (
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// SessionQuery is the live-binding face of the runtime: the sessions currently bound
// to one agent.
//
// 红线清单（2 条）：
// MUST answer the complete addresses of the sessions actually bound to the agent.
// MUST NOT invent a session or report an address the run has not established.
type SessionQuery interface {
	// ActiveSessions returns the complete addresses of the sessions bound to
	// agentID, in a deterministic order.
	ActiveSessions(agentID event.AgentID) []event.Address
}

// ReflectionDelivery is the delivery face one reflection turn reads its send calls
// off: the declared deliver_to targets and the live-binding face each of them
// resolves against.
//
// The reflection renders the sessions a target currently binds into the input it
// injects, so the turn reads the exact agent and session pair a send call names
// instead of guessing one. The delivery itself stays the turn's own decision: this
// face only states where a body could go.
type ReflectionDelivery struct {
	// Targets are the declared deliver_to agent names, in declaration order.
	Targets []string
	// Sessions resolves the live sessions one target binds.
	Sessions SessionQuery
}

// ReflectionLoop is the loop session one reflection reflects into: the observing
// agent's own identity and namespace, and the session its input is injected into.
type ReflectionLoop struct {
	// Agent is the observing agent's own identity and namespace.
	Agent string
	// Session is the loop session the reflection input is injected into, which is
	// always this agent's own session.
	Session string
}

// ReflectionFace is the face one reflection observes and the bounds it reads it
// under: the declared namespaces, the authorized read scope they must sit inside
// and the per-pass and per-digest bounds.
type ReflectionFace struct {
	// ObservedNamespaces declares the face; empty means the agent's own partition.
	ObservedNamespaces []string
	// ReadNamespaces is the authorized read scope of the agent.
	ReadNamespaces []string
	// PageLimit bounds one pass over the observed face; zero runs the default.
	PageLimit int
	// FactLimit bounds the facts one digest lists; zero runs the default.
	FactLimit int
}

// ReflectionCadence is how often one reflection may fire: the tick cadence and the
// floor between two consumed reflections.
type ReflectionCadence struct {
	// Interval is the tick cadence, reported beside a pending input that has
	// outlived it by three intervals.
	Interval time.Duration
	// MinGap is the floor between two consumed reflections.
	MinGap time.Duration
}

// ReflectionConfig is the assembly input of one reflecting agent's loop: the loop
// session it reflects into, the face it observes and the bounds it reads it under,
// its cadence, the static prompt its input carries and the delivery face its turn
// reads send calls off.
//
// The prompt is text the reflection renders without spending a model call.
type ReflectionConfig struct {
	// Loop is the loop session the reflection reflects into.
	Loop ReflectionLoop
	// Face is the observation face and the bounds it is read under.
	Face ReflectionFace
	// Cadence is how often one reflection may fire.
	Cadence ReflectionCadence
	// Prompt is the static reflection prompt carried beside the digest.
	Prompt string
	// Delivery is the declared delivery face, whose live sessions are rendered
	// into the input of every fired reflection.
	Delivery ReflectionDelivery
}
