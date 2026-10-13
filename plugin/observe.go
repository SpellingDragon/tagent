package plugin

import (
	"context"
	"time"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// ObservationSource declares the observation face a reflection may read and
// judges whether that face holds activity fresh enough to reflect on.
//
// The face is an application authorization rather than a capability the source
// grants itself: the declared namespaces must stay within the namespaces the
// application already authorized for reading plus the agent's own partition,
// an undeclared face is the agent's own partition, and a declared face that
// reaches past that boundary is refused by name when the source is assembled
// instead of being trimmed in silence.
type ObservationSource interface {
	// ObservedNamespaces returns the partitions this source is authorized to
	// observe. An empty result means the face was not declared, and the agent's
	// own partition is the face.
	ObservedNamespaces() []string

	// FreshSince reports whether the observed face holds a fact recorded after
	// since whose lineage is not managed by the observing agent itself, so a
	// notification the agent produces about its own work never makes it reflect
	// on itself again.
	//
	// The judgment reads the fact chain by identity, so a chain that cannot be
	// read keeps the gate closed: a face with nothing fresh and a failed read
	// both leave reflection off, and no fallback criterion is substituted for an
	// unreachable chain.
	FreshSince(ctx context.Context, since time.Time) (bool, error)
}

// The declared methods must keep exactly these shapes, and the declared name
// must stay an interface type.
var (
	_ func(ObservationSource) []string = ObservationSource.ObservedNamespaces

	_ func(ObservationSource, context.Context, time.Time) (bool, error) = ObservationSource.FreshSince

	_ ObservationSource = (ObservationSource)(nil)
)
