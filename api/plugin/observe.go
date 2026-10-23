package plugin

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
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

// Observer is the post-commit observation entry: the single callback a runtime
// invokes for each fact that has committed successfully. It is one outbound
// port of the observation bypass: it consumes committed facts, never blocks the
// caller that produced one, and never withholds a fact from a reader because an
// observer is slow or broken; with observation switched off, the committed
// facts stay available to every reader.
//
// The redline list (4 rules):
// MUST run after the fact is durable.
// MUST record a failure as a gap in the observation record and report it to the caller.
// MUST NOT hold back or reorder a committed fact for a slow or failing observer.
// MUST NOT roll back the committed fact or re-run the observed work to satisfy an observer.
type Observer interface {
	// Observe reports one committed fact to the observation side.
	//
	// The fact is passed unchanged and is never rewritten here. A returned
	// error records a missing observation, not an undone fact.
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

// Layer names one layer of the layered SDK observation attempted for one
// committed fact. The layers are attempted in the fixed order the constants
// declare, so one audit record always reads trace then metric regardless of
// how many facts one run observes.
type Layer string

const (
	// LayerTrace is the trace layer: the span the SDK records for one
	// committed fact.
	LayerTrace Layer = "trace"

	// LayerMetric is the metric layer: the counter the SDK adds for one
	// committed fact.
	LayerMetric Layer = "metric"
)

// Attempt is one SDK observation attempt at one layer: the layer it was made
// at and whether it was skipped because the observation is switched off. A
// skipped attempt is a legitimate recorded state, not a silent omission: an
// audit reader sees which layers the SDK attempted and which it deliberately
// left alone.
//
// An attempt here is an observation attempt at one SDK layer, never a model
// attempt: the remembered form of a model attempt is a record of the training
// surface, and the identity of a model attempt is the event package's
// AttemptID. The three readings stay apart and none is decoded as another.
type Attempt struct {
	// Layer names the layer the attempt was made at.
	Layer Layer

	// Skipped reports the attempt was not made because the observation is
	// switched off. A layer that ran records the attempt as made, whether or
	// not the framework behind it holds a real exporter yet.
	Skipped bool
}

// AuditRecord is the layered audit record of one committed fact's observation:
// one entry per SDK layer, in the fixed layer order, describing the attempt
// that layer made for that fact. The record is evidence about the observation,
// never about the fact: it carries the fact's identity and the layered
// attempts and rewrites nothing the fact already holds.
type AuditRecord struct {
	// Fact is the identity of the committed fact the attempts were made for.
	Fact event.EventID

	// Attempts holds one entry per SDK layer, in the fixed layer order
	// trace then metric.
	Attempts []Attempt
}

// AuditSink is the audit side of the observation bypass: it receives the
// layered audit record of one observed fact and reports whether the record
// landed. An implementation must be safe for concurrent use, because the
// post-commit pass may observe facts from several writers at once.
//
// The redline list (3 rules):
// MUST be safe for concurrent use.
// MUST report a record that did not land as a missing observation.
// MUST NOT touch the committed fact the record describes.
type AuditSink interface {
	// Audit hands one layered audit record to the audit side. A returned
	// error means the record did not land and is reported to the caller as a
	// missing observation; it never touches the committed fact the record
	// describes.
	Audit(ctx context.Context, record AuditRecord) error
}
