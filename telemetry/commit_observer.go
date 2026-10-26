package telemetry

import (
	"context"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
//
// CommitObserver is the post-commit observation wiring of the After pass: it
// resolves the committed fact behind one identity and hands it unchanged to
// the observation sink, as a read-only duty over a fact whose canonical commit
// already happened.
//
// A failed read or a failed sink is returned so the After pass records it as a
// named gap beside this plugin's name: the committed fact is never rolled
// back, never withheld from any reader, and the observed work is never re-run
// to satisfy an observer.
type CommitObserver struct {
	facts plugin.FactLookup
	sink  plugin.ObservationSink
}

// NewCommitObserver wires one fact lookup and one observation sink behind the
// post-commit face. Both are required: an unwired observer refuses by name
// instead of reporting an observation it never made.
func NewCommitObserver(facts plugin.FactLookup, sink plugin.ObservationSink) *CommitObserver {
	return &CommitObserver{facts: facts, sink: sink}
}

// ErrCommitObserverUnwired reports a post-commit observation attempt on an
// observer whose lookup or sink was never wired: a gap by name, not a silent
// no-op that would read as a delivered observation.
var ErrCommitObserverUnwired = errors.New("telemetry: the commit observer is not wired")

// ObserveCommit resolves one committed fact and hands it unchanged to the
// observation sink. It runs only after the canonical commit of that fact; an
// identity with no committed fact behind it is refused as a gap and never
// observed as if it had committed.
func (o *CommitObserver) ObserveCommit(ctx context.Context, id event.EventID) error {
	if o == nil || o.facts == nil || o.sink == nil {
		return ErrCommitObserverUnwired
	}
	evt, err := o.facts.CommittedFact(ctx, id)
	if err != nil {
		return fmt.Errorf("telemetry: observe %s: read the committed fact: %w", id, err)
	}
	if err := o.sink.Observe(ctx, evt); err != nil {
		return fmt.Errorf("telemetry: observe %s: %w", id, err)
	}
	return nil
}
