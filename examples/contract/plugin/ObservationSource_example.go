package pluginexample

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/plugin"
)

// partitionSource is the canonical observation source: it names the partitions the
// application authorized for reading and judges whether that face holds fresh activity.
type partitionSource struct{}

// ObservedNamespaces returns the partitions this source is authorized to observe; an
// empty result means the agent's own partition is the face.
func (partitionSource) ObservedNamespaces() []string { return []string{"team"} }

// FreshSince reports whether the observed face holds a fact recorded after since whose
// lineage the observing agent does not manage itself.
func (partitionSource) FreshSince(_ context.Context, since time.Time) (bool, error) {
	if since.After(time.Now()) {
		return false, nil
	}
	return true, nil
}

var _ plugin.ObservationSource = partitionSource{}

// ExampleObservationSource reads the authorized face of one reflection: an undeclared
// face is the agent's own partition, and a chain that cannot be read keeps the gate
// closed instead of falling back to another criterion.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func ExampleObservationSource() {
	face := partitionSource{}
	fresh, err := face.FreshSince(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		return
	}
	_ = fresh
	_ = face.ObservedNamespaces()
}
