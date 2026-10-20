package telemetry // import "github.com/SpellingDragon/tagent/telemetry"

The log face of the observation bypass: the observer that renders each committed
fact into one framework log line after the fact is durable. The level follows
the fact type — a turn boundary is INFO and every per-fact detail is DEBUG —
so the INFO stream stays bounded by the number of turns while a tool call,
a delivery and a payload shape stay one line away.

A line carries the fact identity, the address it belongs to, the tool name and
a bounded payload shape; content is never rendered. A logger that fails is a
missing line, never a blocked fact: the failure is recovered, the gap is retried
at DEBUG, and the committed fact stays readable exactly as before.

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

VARIABLES

var ErrCommitObserverUnwired = errors.New("telemetry: the commit observer is not wired")
    ErrCommitObserverUnwired reports a post-commit observation attempt on an
    observer whose lookup or sink was never wired: a gap by name, not a silent
    no-op that would read as a delivered observation.

var ErrLogObserverUnwired = errors.New("telemetry: the log observer is not wired")
    ErrLogObserverUnwired reports a post-commit observation attempt on a log
    observer that carries no logger: a gap by name, not a silent no-op that
    would read as a delivered line.

FUNCTIONS

func NewSDKObserver(audit plugin.AuditSink, opts ...Option) (*SDKObserver, error)
    NewSDKObserver assembles one post-commit observer: the audit sink the
    layered records land in, beside the options that redirect or close the OTel
    layers. The default handles are the framework's own, so an unconfigured
    runtime observes through the framework's noop SDK at zero cost and a
    configured one exports through the framework's OTel pipeline without this
    package re-hosting an SDK.

TYPES

type CommitObserver struct {
	// Has unexported fields.
}

    CommitObserver is the post-commit observation wiring of the After pass:
    it resolves the committed fact behind one identity and hands it unchanged to
    the observation sink, as a read-only duty over a fact whose canonical commit
    already happened.

    A failed read or a failed sink is returned so the After pass records it as
    a named gap beside this plugin's name: the committed fact is never rolled
    back, never withheld from any reader, and the observed work is never re-run
    to satisfy an observer.

func NewCommitObserver(facts plugin.FactLookup, sink plugin.ObservationSink) *CommitObserver
    NewCommitObserver wires one fact lookup and one observation sink behind the
    post-commit face. Both are required: an unwired observer refuses by name
    instead of reporting an observation it never made.

func (o *CommitObserver) ObserveCommit(ctx context.Context, id event.EventID) error
    ObserveCommit resolves one committed fact and hands it unchanged to the
    observation sink. It runs only after the canonical commit of that fact;
    an identity with no committed fact behind it is refused as a gap and never
    observed as if it had committed.

type LogObserver struct {
	// Has unexported fields.
}
    LogObserver is the post-commit observer over the framework log.Logger:
    one line per committed fact at the level that fact belongs to, so a turn
    boundary is visible at INFO while every per-fact detail stays at DEBUG.
    It reuses the framework logger instead of declaring a second logging face
    beside it.

    The redline list (4 rules): MUST run after the fact is durable, and never
    before it. MUST NOT render fact content: identity, address, tool name and
    a bounded payload shape only. MUST NOT hold back or reorder a committed
    fact for a slow or failing logger. MUST NOT roll back the committed fact or
    re-run the observed work to satisfy the log.

func NewLogObserver(logger trpclog.Logger) *LogObserver
    NewLogObserver wires one framework logger behind the post-commit log face.
    A nil logger lands on the framework default, so an assembly that names no
    logger observes through the same zap sink the framework logs to.

func (o *LogObserver) Observe(_ context.Context, evt event.Event) (err error)
    Observe renders one committed fact into its line. The level is chosen by
    the fact's frozen type: a turn boundary is INFO and every per-fact detail is
    DEBUG, so the INFO stream stays bounded by the number of turns. A fact that
    carries no identity is a WARNING and is still not a refusal: it is a gap in
    the observation record, and the fact behind it stays as committed.

    A logger that fails is a missing line: the failure is recovered, the gap is
    retried at DEBUG, and no error reaches the caller. The observation bypass
    has no second channel for a log that cannot be written, and a formatting
    failure is never a reason to touch the committed fact or re-run the observed
    work.

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

    - The record-writing face of the training surface shares the word and
    nothing else: that face writes the remembered episode, decision and attempt
    records, while this sink admits committed events for asynchronous delivery.

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
