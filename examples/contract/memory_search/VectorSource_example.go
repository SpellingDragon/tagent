package memorysearchexample

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/search"
)

// readyVectors is the canonical embedding seam a retrieval borrows its vectors from: the
// vector of one event key and whether its generation completed.
type readyVectors struct {
	vectors map[event.EventID][]float32
}

// Vector returns the embedding vector of the event key and whether it is ready.
func (v *readyVectors) Vector(key event.EventID) ([]float32, bool) {
	vector, ok := v.vectors[key]
	if !ok {
		return nil, false
	}
	return vector, true
}

var _ search.VectorSource = (*readyVectors)(nil)

// ExampleVectorSource reads the vector of one completed generation and reports a key whose
// generation has not completed as not ready instead of inferring a vector for it.
//
// 契约: docs/wiki/memory/memory-architecture.md#inmemory-retrieval
func ExampleVectorSource() {
	source := &readyVectors{vectors: map[event.EventID][]float32{"event-1": {0.5, 0.25}}}

	vector, ready := source.Vector("event-1")
	if !ready || len(vector) != 2 {
		return
	}
	if _, ready := source.Vector("event-2"); ready {
		return
	}
}
