package pluginexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// errObserverGap is what the canonical observer reports for a fact it could not record.
var errObserverGap = errors.New("observer: observation gap")

// factTally is the canonical post-commit observer: it counts the committed facts it was
// handed, passes each one through unchanged, and reports the ones it failed to record as
// a gap rather than as an undone fact.
type factTally struct {
	seen map[event.EventID]string
	fail bool
}

// Observe records one committed fact for the observation side.
func (o *factTally) Observe(_ context.Context, evt event.Event) error {
	if o.fail {
		return errObserverGap
	}
	if o.seen == nil {
		o.seen = map[event.EventID]string{}
	}
	o.seen[evt.Key] = evt.Type
	return nil
}

var _ plugin.Observer = (*factTally)(nil)

// ExampleObserver reports one committed fact after it is durable: the fact arrives unchanged
// and is tallied, and a failure to record it is returned as an observation gap while the
// fact itself stays committed.
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
func ExampleObserver() {
	observer := &factTally{}
	evt := event.Event{Key: event.EventID("fact-1"), Type: event.TypeExternalInput}
	if err := observer.Observe(context.Background(), evt); err != nil {
		return
	}
	observer.fail = true
	if err := observer.Observe(context.Background(), evt); err == nil {
		return
	}
	_ = observer.seen[evt.Key]
}
