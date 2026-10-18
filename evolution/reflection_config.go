package evolution

import "time"

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
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
// its cadence and the static prompt its input carries.
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
}
