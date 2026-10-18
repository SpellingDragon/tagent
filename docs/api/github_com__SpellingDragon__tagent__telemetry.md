package telemetry // import "github.com/SpellingDragon/tagent/telemetry"

The OTel/audit face of the observation bypass: the layered SDK observation
attempted for one committed fact, beside the audit record that layering leaves
behind. The observer is a thin wrapper over the tracer and meter handles the
locked framework exposes, so the framework's own OTel SDK is reused rather
than re-hosted, and every layer is attempted after the fact committed and never
before it.

Nothing here owns runtime state. The observation is switched off by
configuration and never by data: with OTel closed the trace and metric layers
are recorded as skipped attempts, the audit record still lands, and every
committed fact stays readable exactly as before, so closing OTel changes neither
a committed fact nor any query read.

Package telemetry is the runtime's observation bypass: it carries the outbound
ports through which an already committed fact is observed for tracing and audit,
and the bounded recorder that hands those observations to a writer.

Nothing here owns runtime state. The ports consume committed facts, never block
the caller that produced one, and never withhold a fact from a caller because an
observer is slow or broken; with observation switched off, the committed facts
stay available to every reader.

TYPES

type Attempt struct {
	// Layer names the layer the attempt was made at.
	Layer Layer

	// Skipped reports the attempt was not made because the observation is
	// switched off. A layer that ran records the attempt as made, whether or
	// not the framework behind it holds a real exporter yet.
	Skipped bool
}
    Attempt is one SDK observation attempt at one layer: the layer it was made
    at and whether it was skipped because the observation is switched off.
    A skipped attempt is a legitimate recorded state, not a silent omission: an
    audit reader sees which layers the SDK attempted and which it deliberately
    left alone.

type AuditRecord struct {
	// Fact is the identity of the committed fact the attempts were made for.
	Fact event.EventID

	// Attempts holds one entry per SDK layer, in the fixed layer order
	// trace then metric.
	Attempts []Attempt
}
    AuditRecord is the layered audit record of one committed fact's observation:
    one entry per SDK layer, in the fixed layer order, describing the attempt
    that layer made for that fact. The record is evidence about the observation,
    never about the fact: it carries the fact's identity and the layered
    attempts and rewrites nothing the fact already holds.

type AuditSink interface {
	// Audit hands one layered audit record to the audit side. A returned
	// error means the record did not land and is reported to the caller as a
	// missing observation; it never touches the committed fact the record
	// describes.
	Audit(ctx context.Context, record AuditRecord) error
}
    AuditSink is the audit side of the observation bypass: it receives the
    layered audit record of one observed fact and reports whether the record
    landed. An implementation must be safe for concurrent use, because the
    post-commit pass may observe facts from several writers at once.

    The redline list (3 rules): MUST be safe for concurrent use. MUST report
    a record that did not land as a missing observation. MUST NOT touch the
    committed fact the record describes.

type Layer string
    Layer names one layer of the layered SDK observation attempted for one
    committed fact. The layers are attempted in the fixed order the constants
    declare, so one audit record always reads trace then metric regardless of
    how many facts one run observes.

const (
	// LayerTrace is the trace layer: the span the SDK records for one
	// committed fact.
	LayerTrace Layer = "trace"

	// LayerMetric is the metric layer: the counter the SDK adds for one
	// committed fact.
	LayerMetric Layer = "metric"
)
type Observer interface {
	// Observe reports one committed fact to the observation side.
	//
	// The fact is passed unchanged and is never rewritten here. A returned
	// error records a missing observation, not an undone fact.
	Observe(ctx context.Context, evt event.Event) error
}
    Observer is the post-commit observation entry: the single callback a runtime
    invokes for each fact that has committed successfully.

    The redline list (4 rules): MUST run after the fact is durable. MUST record
    a failure as a gap in the observation record and report it to the caller.
    MUST NOT hold back or reorder a committed fact for a slow or failing
    observer. MUST NOT roll back the committed fact or re-run the observed work
    to satisfy an observer.

type Option func(*SDKObserver)
    Option configures one SDKObserver at assembly time.

func WithMeter(meter metric.Meter) Option
    WithMeter replaces the default framework meter handle, so a caller that
    hosts its own meter provider can point the metric layer at it.

func WithOTelDisabled() Option
    WithOTelDisabled switches the OTel layers off by configuration: the trace
    and metric layers are recorded as skipped attempts, no SDK call is made,
    and the audit record still lands. Closing OTel is a legitimate state and
    never a reason to withhold any committed fact from any reader.

func WithTracer(tracer trace.Tracer) Option
    WithTracer replaces the default framework tracer handle, so a caller that
    hosts its own tracer provider can point the trace layer at it.

type Recorder interface {
	// Record admits one committed fact for asynchronous delivery.
	//
	// A refusal at the queue bound or the byte budget is an error and
	// increments the loss ledger; it never blocks the caller.
	Record(ctx context.Context, evt event.Event) error

	// FlushAndWait drains the admitted records through the same admission
	// protocol Close uses, then returns the writer-confirmed seal.
	FlushAndWait(ctx context.Context) (SealResult, error)

	// Close stops admission and drains the admitted records, and returns once
	// the writer has finished.
	Close(ctx context.Context) error
}

    Recorder is the bounded, sealable sink behind the observation bypass:
    it admits observation records without ever blocking the caller that produced
    the fact, and it reports what a writer confirmed once the run is sealed.

    The redline list (4 rules): MUST refuse a record that would cross the queue
    length or the byte budget, and count it in the loss ledger. MUST read the
    loss ledger from counters that stay readable while the queue is full.
    MUST NOT block the caller that produced the fact. MUST NOT report a run
    whose writer never confirmed a seal as complete.

type SDKObserver struct {
	// Has unexported fields.
}
    SDKObserver is the post-commit observer over the framework OTel SDK.
    It attempts the trace and metric layers in the fixed layer order, records
    each attempt into the layered audit record, and hands that record to the
    audit sink: SDK attempts at observation are observed layer by layer instead
    of collapsing into one opaque success.

    With OTel closed the layers are recorded as skipped and no SDK call is made;
    with OTel open the layers run against the tracer and meter handles,
    which stay the framework's noop handles until the framework configures an
    exporter. Every failure here is a missing observation: it is returned to the
    caller, recorded as a gap beside the plugin that failed, and never rolls the
    committed fact back or re-runs the observed work.

func NewSDKObserver(audit AuditSink, opts ...Option) (*SDKObserver, error)
    NewSDKObserver assembles one post-commit observer: the audit sink the
    layered records land in, beside the options that redirect or close the OTel
    layers. The default handles are the framework's own, so an unconfigured
    runtime observes through the framework's noop SDK at zero cost and a
    configured one exports through the framework's OTel pipeline without this
    package re-hosting an SDK.

func (o *SDKObserver) Observe(ctx context.Context, evt event.Event) error
    Observe observes one committed fact through the layered SDK attempt and
    leaves the layered audit record behind. It runs after the fact is durable,
    so a slow or failing observation can never hold that fact back, reorder it,
    or withhold it from any reader.

    A returned error records a missing observation: a failed layer attempt
    or an audit record that did not land. It never records an undone fact,
    and the observed work is never re-run to satisfy an observer.

type SealResult struct {
	// Status is the recorded seal status of the run.
	Status SealStatus
	// Reason names why the run is partial or unknown; it is empty for a sealed
	// run.
	Reason string
	// Dropped counts records refused by the bound on the queue or the byte
	// budget.
	Dropped uint64
	// Errors counts records that failed to serialize, write or sync.
	Errors uint64
}
    SealResult reports the outcome of sealing one recorder run: the status the
    writer confirmed, the reason a run is not sealed, and the loss ledger the
    run accumulated.

type SealStatus string
    SealStatus names the recorded seal status of one recorder run.

const (
	// SealStatusSealed marks a run the writer confirmed as sealed.
	SealStatusSealed SealStatus = "sealed"

	// SealStatusPartial marks a run that ended without a writer seal.
	SealStatusPartial SealStatus = "partial"

	// SealStatusUnknown marks a run whose seal evidence is missing.
	SealStatusUnknown SealStatus = "unknown"
)
    SealStatusSealed, SealStatusPartial and SealStatusUnknown name the recorded
    seal statuses: a run is sealed only when a real writer confirmed the seal;
    a run whose writer never confirmed one is partial, and a run whose seal
    evidence is missing is unknown rather than complete.
