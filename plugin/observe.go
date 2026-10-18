package plugin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// ObservationSource declares the observation face a reflection may read and
// judges whether that face holds activity fresh enough to reflect on. The face is an
// application authorization rather than a capability the source grants itself: the
// declared namespaces must stay within the namespaces the application already authorized
// for reading plus the agent's own partition, an undeclared face is the agent's own
// partition, and a declared face that reaches past that boundary is refused by name when
// the source is assembled instead of being trimmed in silence.
//
// The redline list (3 rules):
// MUST keep the observed face within the authorized namespaces plus the agent's own partition.
// MUST NOT treat a fact of the observing agent's own lineage as fresh.
// MUST NOT substitute a fallback criterion when the chain cannot be read.
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

// ObservationSink is the outbound port the post-commit observation wiring hands
// one committed fact to. It keeps the exact shape of the runtime's observation
// port, so the OTel/audit observer plugs in at the composition root without this
// package depending on it: the pipeline stays on its single leaf edge to event,
// and the sink is a choice the composition root makes rather than a fact the
// plugin layer records.
//
// The redline list (3 rules):
// MUST pass the fact it was handed unchanged.
// MUST NOT rewrite the committed fact or hold it back from any reader.
// MUST NOT report a failed observation as a delivered one.
type ObservationSink interface {
	// Observe reports one committed fact to the observation side. The fact is
	// passed unchanged and is never rewritten here. A returned error records a
	// missing observation, not an undone fact.
	Observe(ctx context.Context, evt event.Event) error
}

// FactLookup reads one committed fact by identity: the read-only face the
// post-commit wiring resolves an already committed fact through. It answers
// with the fact as committed and never with a reconstruction, so an identity
// that did not commit is refused rather than fabricated.
//
// The redline list (2 rules):
// MUST answer the fact as it committed, and report an identity with no committed fact as an error.
// MUST NOT fabricate a fact or answer a reconstruction.
type FactLookup interface {
	// CommittedFact returns the committed fact the identity names. An identity
	// with no committed fact behind it is an error, never an empty fact.
	CommittedFact(ctx context.Context, id event.EventID) (event.Event, error)
}

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
	facts FactLookup
	sink  ObservationSink
}

// NewCommitObserver wires one fact lookup and one observation sink behind the
// post-commit face. Both are required: an unwired observer refuses by name
// instead of reporting an observation it never made.
func NewCommitObserver(facts FactLookup, sink ObservationSink) *CommitObserver {
	return &CommitObserver{facts: facts, sink: sink}
}

// ErrCommitObserverUnwired reports a post-commit observation attempt on an
// observer whose lookup or sink was never wired: a gap by name, not a silent
// no-op that would read as a delivered observation.
var ErrCommitObserverUnwired = errors.New("plugin: the commit observer is not wired")

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
		return fmt.Errorf("plugin: observe %s: read the committed fact: %w", id, err)
	}
	if err := o.sink.Observe(ctx, evt); err != nil {
		return fmt.Errorf("plugin: observe %s: %w", id, err)
	}
	return nil
}
