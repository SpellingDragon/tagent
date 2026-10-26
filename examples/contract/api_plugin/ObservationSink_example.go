package pluginexample

import (
	"context"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// countingSink is the canonical observation sink: it reports one committed fact to the
// observation side unchanged and counts the reports it made.
type countingSink struct {
	mu      sync.Mutex
	reports int
}

// Observe reports one committed fact to the observation side; the fact is passed
// unchanged, and a returned error records a missing observation rather than an undone
// fact.
func (s *countingSink) Observe(_ context.Context, evt event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports++
	_ = evt.Key
	return nil
}

// Reports reports how many observations this sink made.
func (s *countingSink) Reports() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reports
}

var _ plugin.ObservationSink = (*countingSink)(nil)

// ExampleObservationSink hands one committed fact to the observation side: the sink never
// rewrites the fact and never withholds it, so a failed observation stays a gap beside the
// committed fact.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func ExampleObservationSink() {
	sink := &countingSink{}
	err := sink.Observe(context.Background(), event.Event{Key: "fact-1"})
	if err != nil {
		return
	}
	_ = sink.Reports()
}
