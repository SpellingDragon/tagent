package meditation // import "github.com/SpellingDragon/tagent/features/evolution/meditation"

The meditation domain's own judgments over the facts it consumes: the freshness
judgment that reads one observed fact and the delivery judgment that reads one
fired reflection's input. A fact carries its trigger-source stamp as recording
metadata; these judgments state, in the domain's own vocabulary, which stamps
open the freshness gate and which may leave through the delivery port.

Skeleton compile-time assertions for the meditation shapes: the reflection loop
and the pass it reads keep their own declared surface here: the loop's assembly
and tick operations, the observation face it reads, the digest it renders and
the execution watermark it advances only on a consumed turn. One file holds the
one shape pin of the package: a renamed field or a widened signature stops it
from compiling.

CONSTANTS

const (
	// DefaultObservationPageLimit bounds one pass over the observed face; a face
	// with a longer backlog reports the bound instead of growing the pass.
	DefaultObservationPageLimit = 1000
	// DefaultDigestFactLimit bounds how many hydrated facts one digest lists.
	DefaultDigestFactLimit = 8
)
const DeliverSummaryContentMax = 8 * 1024
    DeliverSummaryContentMax is the byte ceiling of one summary body.

const DeliverSummarySummaryMax = 120
    DeliverSummarySummaryMax is the rune ceiling of the stored event summary.

const DeliverSummaryToolName = "deliver_summary"

    DeliverSummaryToolName is the model-visible name of the cross-session write
    face a reflection turn may call: one summary, delivered by the model's own
    decision, never by the reflection mechanism in silence.

const LineageReflection = "meditation"

    LineageReflection is the trigger-source stamp a reflection input carries:
    the reflection turn's own lineage, which is self-managed traffic, so an
    agent's own reflection never makes it reflect on itself again.

VARIABLES

var ErrAnchorStoreUnusable = errors.New("evolution: the anchor store requires a path")
    ErrAnchorStoreUnusable reports an anchor store built without a path:
    a watermark that has nowhere to be recorded cannot carry across a restart.

var ErrAnchorUnreadable = errors.New("evolution: the anchor record cannot be read")
    ErrAnchorUnreadable reports an anchor record that exists but cannot be read
    or parsed. The gate reads it as an unestablished watermark and stays closed;
    no default and no fallback criterion is substituted for it.

var ErrDigestFaceUnavailable = errors.New("evolution: the digest has no observed pass to render")
    ErrDigestFaceUnavailable reports a digest asked to render without the pass
    it summarizes. An absent pass is named as unavailable, never presented as a
    quiet face.

var ErrDigestScanMismatch = errors.New("evolution: the observed pass opened at another watermark")
    ErrDigestScanMismatch reports a digest asked to render a pass that opened
    at a watermark other than the one it was asked about. The pass a digest
    summarizes is the one its caller already read, so the rendering never
    triggers a second pass over the fact chain.

var ErrObservationScopeOutOfBounds = errors.New("evolution: the observed namespaces reach outside the authorized read scope")
    ErrObservationScopeOutOfBounds reports an observation face that names a
    namespace outside the authorized read scope. The face is refused by name at
    assembly instead of being trimmed: reading less than declared would silently
    change what the reflection observes.

var ErrObservedFaceUnwired = errors.New("evolution: the observed fact-chain read face is not wired")
    ErrObservedFaceUnwired reports a pass attempted while the read face of
    the observed fact chain is not wired. The reflection gate stays closed:
    the fact chain is the only data face of the freshness judgment.

var ErrReflectionAgentUnnamed = errors.New("evolution: reflection requires the observing agent's namespace")

    ErrReflectionAgentUnnamed reports a reflection assembled without the
    observing agent's own identity: an observation face cannot be resolved and
    no loop session can be addressed without it.

var ErrReflectionInputUnkeyed = errors.New("evolution: the reflection input committed no identity")
    ErrReflectionInputUnkeyed reports a reflection input that committed no
    identity: without one, a consumption report cannot name the turn it settles.

var ErrReflectionKeyMismatch = errors.New("evolution: the outcome names a reflection input other than the one awaiting consumption")
    ErrReflectionKeyMismatch reports an outcome naming a reflection input other
    than the one awaiting consumption. The report is refused, so a report about
    an older turn can never settle a newer one.

var ErrReflectionNotPending = errors.New("evolution: no reflection input awaits an outcome")
    ErrReflectionNotPending reports an outcome for a reflection input that is
    not awaiting one: a repeated or stale report never moves the watermark.

var ErrWatermarkSourceUnwired = errors.New("evolution: the execution watermark source is not wired")
    ErrWatermarkSourceUnwired reports a tick attempted while the execution
    watermark has no source. The gate stays closed instead of reading a fresh
    default.

FUNCTIONS

func DeliverableLineage(stamp string) bool
    DeliverableLineage reports whether a trigger-source stamp names traffic
    the host may see delivered, from the vocabulary the delivery policy reads.
    The stamp stays on the fact as recording metadata.

func SelfManagedLineage(stamp string) bool
    SelfManagedLineage reports whether a trigger-source stamp names traffic
    the agent manages for itself rather than an interaction someone awaits. The
    derivation is the recording reading of the stamp: an unknown or empty stamp
    is self-managed, and no freshness decision is read from this derivation.

TYPES

type AnchorStore struct {
	// Has unexported fields.
}
    AnchorStore is the anchor record of one observing agent: the pair of
    loop anchors kept in one file and rewritten atomically, so a reader never
    observes a half-written record.

    The store is the gate's only watermark source. A record that is absent reads
    as a start with no prior execution, while a record that exists but cannot be
    read or parsed is refused by name: an unreadable watermark is never reported
    as a fresh one.

func NewAnchorStore(path string) (*AnchorStore, error)
    NewAnchorStore returns the anchor store of the record at path, creating
    its directory when the directory is absent. A store built without a path is
    refused with ErrAnchorStoreUnusable.

func (s *AnchorStore) Advance(ctx context.Context, moment time.Time) error
    Advance moves the execution watermark to the moment a consumed reflection
    turn was injected at and keeps the session anchor as it stands. A moment
    that is not after the recorded watermark leaves the record as it is,
    so the watermark never moves backwards.

func (s *AnchorStore) Current(ctx context.Context) (Watermark, error)
    Current reports the execution watermark the anchor record holds. An absent
    record reports the zero watermark of a start with no prior execution,
    and an unreadable record is refused with ErrAnchorUnreadable so the gate
    stays closed.

func (s *AnchorStore) Load() (Anchors, error)
    Load reads the anchor record. An absent record is a start with no prior
    execution and carries no error; a record that cannot be read or parsed is
    refused with ErrAnchorUnreadable.

func (s *AnchorStore) Path() string
    Path reports the record path this store reads and writes.

func (s *AnchorStore) Save(record Anchors) error
    Save rewrites the anchor record atomically: the new record is written beside
    it and renamed over it, so no reader observes a partial record.

type Anchors struct {
	// LastTurnEnd is the moment the last turn of the session ended.
	LastTurnEnd time.Time `json:"last_turn_end"`
	// LastMeditation is the moment of the last consumed reflection turn.
	LastMeditation time.Time `json:"last_meditation"`
}
    Anchors is the persisted pair of loop anchors: the moment the last
    reflection turn was consumed and the moment the last turn of the session
    ended.

    The reflection watermark heads the freshness window and the rhythm floor,
    so it is the anchor the gate reads and the anchor a consumed reflection
    advances. The session anchor is carried beside it in the same record,
    and an entry this structure does not name is ignored when the record is read
    rather than refused.

type DeliverSummaryTool struct {
	// Has unexported fields.
}
    DeliverSummaryTool is the meditation session's cross-session write tool:
    it delivers one summary to every active session of the declared deliver_to
    agents.

    Every refusal is answered as result text and the turn continues; an error is
    reserved for an argument shape that is not JSON at all.

func NewDeliverSummaryTool(targets []string, sink EventSink, sessions SessionQuery) *DeliverSummaryTool
    NewDeliverSummaryTool assembles the cross-session write tool over the
    declared target agents, the write face and the live-binding face.

func (t *DeliverSummaryTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call delivers one summary to every active session of the declared targets.
    Refusals — malformed fields, an empty or oversized body, an unwired write
    face and a target with no bound session — are answered as result text,
    so the reflection turn continues and the model sees why nothing was written.

func (t *DeliverSummaryTool) Declaration() *trpctool.Declaration
    Declaration reports the tool declaration: one content argument, delivered to
    the sessions the declaration's deliver_to targets bind.

type DeliveryPolicy struct{}
    DeliveryPolicy is the meditation domain's delivery policy: one fired
    reflection's own input in, whether its output may leave through the delivery
    port out. A stamp the policy does not name is withheld.

func (DeliveryPolicy) Deliverable(input event.Event) bool
    Deliverable reports whether the output behind one fired reflection input may
    leave through the delivery port.

type Digest interface {
	// Render returns the bounded self-state summary of the observed face since
	// the given watermark. When the face holds more than the bound renders, the
	// count left out is reported in the result instead of being dropped in
	// silence.
	Render(ctx context.Context, since Watermark) (DigestSummary, error)
}

    Digest is the reflection loop's self-state summary: a deterministic,
    bounded rendering of the observed face since a watermark that a reflection
    turn reads without spending a model call.

    The rendering is bounded, so a face with a long backlog reports how many
    of its facts the bound left out instead of growing without limit, and the
    same face at the same watermark renders the same summary. A render that
    cannot reach the face reports that it is unavailable by name rather than
    presenting an empty summary as a quiet face. The redline list (3 rules):
    MUST render the same summary for the same face at the same watermark.
    MUST report the count a bound left out beside the rendering it bounded.
    MUST NOT present an unreachable face as a quiet one.

type DigestSummary struct {
	// Body is the rendered summary of the observed face.
	Body string
	// Omitted is the number of facts the render bound left out of Body.
	Omitted int
}
    DigestSummary is the rendered self-state summary together with the count of
    facts the render bound left out.

type Driver struct {
	// Has unexported fields.
}

    Driver is the event-driven face of one reflection loop: it observes the
    facts the agent's own pipeline commits and runs a reflection pass only when
    one of them names an interaction someone awaits and the rhythm has elapsed.

    There is no ticker: a reflection is the answer to a new event, so a process
    with nothing new never fires one. The interval is a floor between two fires,
    never a clock that fires on its own. A pass the rhythm floor holds back
    keeps its episode open through the standing re-arm and evaluates again once
    the window opens, so a floor above zero still fires instead of waiting for a
    commit the held session may never send.

func NewDriver(reflection *Reflection, interval, minGap time.Duration,
	slot func(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool),
	instance event.InstanceID) *Driver
    NewDriver assembles the event-driven reflection face over one reflection
    loop, the floor between two fires, the floor between two consumed
    reflections, the read of the observed session's execution seat and the
    instance that seat is held in.

    The interval is measured from the last fire; the first fire is never
    held back, so the first event of a process is answered at once,
    and a non-positive interval disables that floor. The min gap stays the
    reflection's own rhythm floor, read from the execution watermark, so a fired
    but not yet consumed reflection is never re-fired early. A nil seat read or
    an empty instance leaves the pass its rhythm alone.

func (d *Driver) Name() string
    Name reports the stable name of this plugin instance.

func (d *Driver) ObserveCommit(ctx context.Context, id event.EventID) error
    ObserveCommit reads the fact one commit wrote and advances the driver's
    timer state when that fact names an interaction someone awaits, recording
    the session that material belongs to beside it: the pass reflects on that
    session's seat.

    The pass itself runs detached from the committing turn: a reflection spends
    a model call, and a user turn never waits for it. A failed read is reported
    and never rolls the commit back, and a stamp outside the host-facing
    vocabulary leaves the timer untouched, so an agent's own reflection never
    reflects again.

func (d *Driver) Port() plugin.Port
    Port reports the consumption the driver serves: one committed fact in, one
    observation out. The observation is the reflection pass the fact may fire.

func (d *Driver) SetObserver(observer func(TickOutcome))
    SetObserver attaches the reading of each pass this driver runs. It is the
    host's log face; a nil observer drops the readings.

func (d *Driver) Stop()
    Stop ends the driver: no further commit fires a pass, a pass in flight is
    cancelled, and Stop waits for it to return. A repeated stop is a no-op.

type EventSink interface {
	// CommitToSession commits evt into the fact chain of addr.
	CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error)
}
    EventSink is the cross-session write face of the runtime: one event
    committed into the fact chain of the address it names, under a fresh
    identity of its own.

    It is the only way a reflection summary reaches another session: the tool
    decides to write, the sink files the fact, and the target session's own next
    assembly reads it like any other external input. 红线清单（2 条）： MUST commit
    the event into the chain of the address it names. MUST NOT write into the
    caller's chain or answer an identity the commit did not record.

type FactAttribution struct {
	// Lineage is the trigger-source stamp the fact carries; empty means the fact
	// carries none.
	Lineage string
	// SelfManaged reports the lineage derivation of this fact.
	SelfManaged bool
}
    FactAttribution is whose traffic a fact is: the trigger-source stamp it
    carries and the derivation of that stamp.

type FactContent struct {
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact.
	Summary string
	// Timestamp is the moment the fact was recorded.
	Timestamp time.Time
}
    FactContent is what a fact recorded: its frozen type, its stored summary and
    the moment it was recorded.

type FactIdentity struct {
	// Key is the canonical identity of the fact.
	Key event.EventID
	// Partition is the namespace name the fact was filed under.
	Partition string
}
    FactIdentity is which fact a pass read and where it was filed: the canonical
    identity the fact committed under and the namespace name it was filed as.

type NoveltyPolicy struct{}
    NoveltyPolicy is the meditation domain's freshness policy: one observed fact
    in, whether it is new material for the reflection out. The criteria live in
    this domain, so a stamp the policy does not name is never new material.

func (NoveltyPolicy) Novel(fact event.Event) bool
    Novel reports whether one observed fact opens the freshness gate.

type ObservationScan struct {
	// Watermark is the execution watermark the window opened after.
	Watermark time.Time
	// Partitions holds one tally line per observed partition, in declared order.
	Partitions []PartitionActivity
	// Facts is the hydrated sample in read order, bounded by the pass's fact limit.
	Facts []ScannedFact
	// Counts is the accounting of the read.
	Counts ScanCounts
	// Verdict is what the freshness judgment read from this pass.
	Verdict ScanVerdict
}
    ObservationScan is the bounded evidence of one pass over the observed face:
    the watermark the window opened at, the per-partition tallies, the hydrated
    sample, the accounting of that read and the verdict the freshness judgment
    read from it.

    The evidence is kept so the digest renders from this pass and never reads
    the fact chain a second time.

type ObservationScanRequest struct {
	// Since is the execution watermark the window opens strictly after; the zero
	// value opens the window at the epoch.
	Since time.Time
	// PageLimit bounds the reference page; zero runs the default bound.
	PageLimit int
	// FactLimit bounds the hydrated sample; zero runs the default bound.
	FactLimit int
}
    ObservationScanRequest is the window and bounds of one pass over the
    observed face.

type ObservedFacts interface {
	// QueryEvents returns the references matching opts.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
	// CommittedFact returns the committed fact the identity names; an identity
	// with no committed fact behind it is an error, never an empty fact.
	CommittedFact(ctx context.Context, id event.EventID) (event.Event, error)
}
    ObservedFacts is the read-only fact-chain face the reflection judgment
    runs on: the conditional retrieval the authorization-scoped query answers
    through, beside the committed-fact read that hydrates one reference into the
    fact it names.

    A query answers references only, which carry no trigger source,
    so the judgment hydrates the newest references one by one and stops at
    its first hit. Both calls are reads: a pass never writes the chain it
    observes. The redline list (3 rules): MUST answer references through the
    authorization-scoped query and hydrate each one through its committed fact.
    MUST NOT write the chain it observes. MUST NOT answer an identity that never
    committed as an empty fact.

type PartitionActivity struct {
	// Name is the namespace name the partition was resolved from.
	Name string
	// Partition is the storage partition the name resolves to.
	Partition int
	// References counts this partition's entries on the reference page, which
	// the read face answers without any hydration.
	References int
	// External counts hydrated facts the freshness policy reads as new
	// material: the freshness evidence.
	External int
	// SelfManaged counts hydrated facts the freshness policy reads as
	// self-managed, unknown attribution included.
	SelfManaged int
}
    PartitionActivity is one observed partition's tally in a pass: how much
    of the reference page belongs to it and how the hydrated sample splits by
    lineage.

type Reflection struct {
	// Has unexported fields.
}
    Reflection is the reflection loop of one agent: it reads the authorized
    observation face, decides whether a reflection is due, injects the input
    event into the agent's own loop session and advances the execution watermark
    when that turn is consumed.

    One mechanism covers every observation face: observing the agent's own
    partition is self-maintenance and observing others' partitions is the
    side-car meditation, and the action is the same explicit input in either
    case. A fire only writes the input; the watermark moves when the turn is
    consumed, to the injection moment, so every fact a yield window covered
    stays inside the next window. A read the pass cannot complete keeps the gate
    closed by name, and no fallback criterion is substituted for an unreachable
    chain.

func NewReflection(cfg ReflectionConfig, gate WatermarkGate, facts ObservedFacts, input ReflectionInput) (*Reflection, error)
    NewReflection assembles the reflection loop of one agent over its watermark
    gate, its observed fact-chain face and the loop-session injection face. A
    declared observation face outside the authorized scope refuses the assembly
    by name.

func (r *Reflection) CommittedFact(ctx context.Context, id event.EventID) (event.Event, error)
    CommittedFact reads the fact one commit identity names through the observed
    chain. It is the read the event-driven face starts from: the commit notice
    carries an identity, and the fact itself is read back here.

func (r *Reflection) DeferredCount() int
    DeferredCount reports how many reflections yielded since the last consumed
    one.

func (r *Reflection) FreshSince(ctx context.Context, since time.Time) (bool, error)
    FreshSince reports whether the observed face holds a fact recorded after
    since that the freshness policy reads as new material.

    A face that cannot be read is reported as an error and never as "nothing
    fresh": the fact chain is the judgment's only data face.

func (r *Reflection) LastDeferredAt() time.Time
    LastDeferredAt reports the moment of the most recent deferral.

func (r *Reflection) MinGap() time.Duration
    MinGap reports the floor between two consumed reflections this loop holds:
    the window a pass is judged against, and the one an event-driven face holds
    a suppressed pass open for.

func (r *Reflection) NoteOutcome(ctx context.Context, receipt ReflectionReceipt) error
    NoteOutcome settles what became of the reflection input awaiting one.

    A consumed turn advances the execution watermark to that input's injection
    moment and clears the deferral tally; a deferred turn clears the pending
    input and leaves the watermark in place; an in-flight turn keeps it pending.
    A report naming an input other than the pending one, or arriving while
    none is pending, is refused by name so a repeated report can never move the
    watermark again.

func (r *Reflection) ObservedNamespaces() []string
    ObservedNamespaces reports the resolved observation face of this reflection.

func (r *Reflection) Pending() (event.EventID, bool)
    Pending reports the identity of the reflection input awaiting its outcome,
    if one is pending.

func (r *Reflection) Scope() ReflectionScope
    Scope reports the resolved observation face, names and partitions together.

func (r *Reflection) Tick(ctx context.Context, now time.Time) TickOutcome
    Tick runs one reflection tick at now: it establishes the execution
    watermark, reads the observed face once, renders the digest from that
    same pass, applies the pending guard, the freshness judgment and the
    rhythm floor, and injects one explicit input event when all three open.
    The rhythm floor is read on both sides of one execution — the interval
    since the last consumed reflection, and the age of the newest host-facing
    fact — and a floor that holds the tick back reports the fact holding it,
    leaving the watermark and the pending input untouched.

    A tick whose gate cannot be established reports TickClosed with the fact it
    could not establish and injects nothing; a tick that injected reports the
    input and settles the outcome the loop side handed back.

func (r *Reflection) Watermark(ctx context.Context) (Watermark, error)
    Watermark reports the execution watermark the gate holds, refusing by name
    when no source is wired.

type ReflectionCadence struct {
	// Interval is the tick cadence, reported beside a pending input that has
	// outlived it by three intervals.
	Interval time.Duration
	// MinGap is the floor between two consumed reflections.
	MinGap time.Duration
}
    ReflectionCadence is how often one reflection may fire: the tick cadence and
    the floor between two consumed reflections.

type ReflectionConfig struct {
	// Loop is the loop session the reflection reflects into.
	Loop ReflectionLoop
	// Face is the observation face and the bounds it is read under.
	Face ReflectionFace
	// Cadence is how often one reflection may fire.
	Cadence ReflectionCadence
	// Prompt is the static reflection prompt carried beside the digest.
	Prompt string
}
    ReflectionConfig is the assembly input of one reflecting agent's loop:
    the loop session it reflects into, the face it observes and the bounds it
    reads it under, its cadence and the static prompt its input carries.

    The prompt is text the reflection renders without spending a model call.

type ReflectionFace struct {
	// ObservedNamespaces declares the face; empty means the agent's own partition.
	ObservedNamespaces []string
	// ReadNamespaces is the authorized read scope of the agent.
	ReadNamespaces []string
	// PageLimit bounds one pass over the observed face; zero runs the default.
	PageLimit int
	// FactLimit bounds the facts one digest lists; zero runs the default.
	FactLimit int
}
    ReflectionFace is the face one reflection observes and the bounds it reads
    it under: the declared namespaces, the authorized read scope they must sit
    inside and the per-pass and per-digest bounds.

type ReflectionInjection struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// At is the injection moment, which is the watermark candidate a consumed
	// turn advances the gate to.
	At time.Time
	// Event is the explicit input event as it was handed to the loop session.
	Event event.Event
	// Digest is the self-state summary the input carried.
	Digest DigestSummary
}
    ReflectionInjection is one injected reflection input as the firing tick
    reports it: the identity it committed under, the moment it was injected at,
    the event itself and the digest it carried.

type ReflectionInput interface {
	// InjectReflection commits one reflection input into the agent's own loop
	// session and reports the outcome of its turn.
	InjectReflection(ctx context.Context, evt event.Event) (ReflectionReceipt, error)
}
    ReflectionInput is the loop-session face one fired reflection injects its
    explicit input event through: the assembly commits the event as a fact of
    the agent's own partition, drives the observing agent's own loop session
    with it and reports what became of that turn.

    The receipt is the only evidence that a reflection turn ran: a fire that
    was only written never advances the watermark. The redline list (3 rules):
    MUST commit the reflection input as a fact of the agent's own partition.
    MUST report the outcome of the turn the injected input drove. MUST NOT
    report a reflection turn as consumed from a fire that was only written.

type ReflectionLoop struct {
	// Agent is the observing agent's own identity and namespace.
	Agent string
	// Session is the loop session the reflection input is injected into, which is
	// always this agent's own session.
	Session string
}

    ReflectionLoop is the loop session one reflection reflects into: the
    observing agent's own identity and namespace, and the session its input is
    injected into.

type ReflectionOutcome string
    ReflectionOutcome is what became of one injected reflection input.

const (
	// ReflectionConsumed reports a reflection turn that ran: the watermark
	// advances to the moment the turn was injected at.
	ReflectionConsumed ReflectionOutcome = "consumed"
	// ReflectionDeferred reports a reflection turn that yielded to real input at
	// either yield point: the watermark stays where it stands and the very same
	// facts are re-evaluated on the next tick.
	ReflectionDeferred ReflectionOutcome = "deferred"
	// ReflectionInFlight reports an injected input whose turn has not reached an
	// outcome yet; the input stays pending and no second one is injected.
	ReflectionInFlight ReflectionOutcome = "in-flight"
)
type ReflectionReceipt struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// Outcome is what became of the injected turn.
	Outcome ReflectionOutcome
	// At is the moment the outcome was reached; it dates the deferral tally and
	// never moves the watermark.
	At time.Time
	// Reason is the named ground of a deferral, when the loop side reports one.
	Reason string
}
    ReflectionReceipt is the loop side's report of one injected reflection
    input: the identity the fact committed under, what became of its turn and
    the moment that outcome was reached.

type ReflectionScope struct {
	// Own is the observing agent's own namespace, which is always observably
	// legal and is the face of an undeclared observation surface.
	Own string
	// Names lists the observed namespaces in declared order, deduplicated.
	Names []string
	// Partitions lists the storage partition each name resolves to, in the same
	// order as Names; the query scans exactly these partitions.
	Partitions []int
}

    ReflectionScope is the resolved observation face of one reflecting agent:
    the namespaces it observes and the storage partitions those namespaces
    resolve to.

    The face is an application authorization rather than a capability the
    reflection grants itself. A declared face stays within the namespaces
    the application authorized for reading plus the agent's own partition,
    an undeclared face is the agent's own partition, and a declared face that
    reaches past that boundary is refused at assembly.

func ResolveObservationScope(own string, declared, read []string) (ReflectionScope, error)
    ResolveObservationScope resolves the observation face of one agent from its
    declared namespaces, the namespaces it is authorized to read and its own
    namespace.

    An undeclared face is the agent's own partition and never falls back to the
    authorized read scope: what an agent may read is not what it reflects on.
    A declared name that is neither the agent's own nor inside the read scope is
    refused with ErrObservationScopeOutOfBounds naming every such namespace.

type ScanCounts struct {
	// ReferenceTotal is the size of the reference page the pass read.
	ReferenceTotal int
	// HydratedTotal is the number of facts the pass hydrated.
	HydratedTotal int
	// Unreadable counts references the pass could not hydrate; an unreadable fact
	// is never counted as freshness.
	Unreadable int
	// UnknownLineage counts hydrated facts that carry no trigger-source stamp.
	UnknownLineage int
	// Foreign counts hydrated facts filed under a partition outside the observed
	// face; such a fact is never counted as freshness either.
	Foreign int
}
    ScanCounts is the accounting of one pass: the reference page it read,
    how many facts it hydrated, and the references that yielded no freshness
    evidence.

type ScanDigest struct {
	// Has unexported fields.
}
    ScanDigest renders the self-state summary out of one completed observation
    pass: the watermark the window opened at, one tally line per observed
    partition and the hydrated facts the pass kept, all in the deterministic
    order the pass read them.

    The rendering spends no model call, blocks on no external surface, reads the
    fact chain no further than the pass already did and stays bounded whatever
    the backlog: partitions and facts beyond the bounds, and references the pass
    did not hydrate, are counted in Omitted instead of being dropped in silence.

func NewScanDigest(scan *ObservationScan, factLimit int) *ScanDigest
    NewScanDigest builds a digest over one completed pass, limiting the listed
    facts to factLimit entries; a non-positive limit runs the default bound.

func (d *ScanDigest) Render(ctx context.Context, since Watermark) (DigestSummary, error)
    Render returns the bounded self-state summary of the pass this digest was
    built over, refusing by name when there is no pass or when the pass opened
    at another watermark than the one since names.

type ScanVerdict struct {
	// Novelty reports whether the face holds a fact newer than the watermark that
	// the freshness policy reads as new material.
	Novelty bool
	// Recent is the newest fact the freshness policy reads as new material, when
	// the pass found one.
	Recent *ScannedFact
}
    ScanVerdict is what the freshness judgment read off one pass: whether the
    face holds a fact newer than the watermark that the freshness policy reads
    as new material, and that fact.

type ScannedFact struct {
	// Identity is the fact and the partition it was filed under.
	Identity FactIdentity
	// Content is what the fact recorded.
	Content FactContent
	// Attribution is whose traffic the fact is.
	Attribution FactAttribution
}
    ScannedFact is one hydrated fact of a pass, kept in the deterministic order
    the pass read it.

type SessionQuery interface {
	// ActiveSessions returns the complete addresses of the sessions bound to
	// agentID, in a deterministic order.
	ActiveSessions(agentID event.AgentID) []event.Address
}
    SessionQuery is the live-binding face of the runtime: the sessions currently
    bound to one agent. A summary is delivered to the sessions that exist,
    and an agent with no bound session is a named refusal, never a silent skip.

    红线清单（2 条）： MUST answer the complete addresses of the sessions actually bound
    to the agent. MUST NOT invent a session or report an address the run has not
    established.

type TickDecision struct {
	// Disposition is what this tick decided.
	Disposition TickDisposition
	// Reason is the named ground of the disposition; a closed gate names the fact
	// it could not establish.
	Reason string
	// Cause is the named failure behind a closed gate, when one was returned by a
	// read or by the watermark source.
	Cause error
}
    TickDecision is what one reflection tick decided and the named ground behind
    that decision.

type TickDisposition string
    TickDisposition is what one reflection tick decided.

const (
	// TickFired reports that this tick injected a reflection input.
	TickFired TickDisposition = "fired"
	// TickPending reports that an injected reflection input still awaits its
	// outcome, so this tick injected nothing.
	TickPending TickDisposition = "pending"
	// TickClosed reports that the gate stayed closed because a fact it needs
	// could not be established.
	TickClosed TickDisposition = "closed"
	// TickNoNovelty reports a readable face holding nothing newer than the
	// watermark that the freshness policy reads as new material.
	TickNoNovelty TickDisposition = "no-novelty"
	// TickRhythm reports that the rhythm floor has not elapsed since the last
	// consumed reflection.
	TickRhythm TickDisposition = "rhythm"
)
type TickOutcome struct {
	// Decision is what this tick decided.
	Decision TickDecision
	// Scan is the evidence of the pass this tick read, when a pass ran.
	Scan *ObservationScan
	// Digest is the self-state summary rendered from that same pass.
	Digest DigestSummary
	// Injection is the reflection input this tick injected, when it fired.
	Injection *ReflectionInjection
}
    TickOutcome is the decision one reflection tick reached, the evidence of the
    pass it read and the injection it made.

type Watermark struct {
	// Moment is the moment the last consumed reflection turn advanced the
	// watermark to. Its zero value marks a start with no prior execution.
	Moment time.Time
}

    Watermark is the execution watermark of the reflection loop: the moment the
    last reflection turn advanced it to, which heads the freshness window and
    bounds how soon the next reflection may run.

    Injection alone never moves the watermark; only a reflection turn that is
    actually consumed advances it, to the moment the turn was injected at.
    A reflection that yields leaves the watermark in place, so the facts that
    reflection covered stay no older than the watermark instead of being burned
    out of the window, and a start with no watermark passes once and then locks
    itself. The watermark is advanced by consumption only, and the freshness
    policy excludes the agent's own reflection traffic, so a notification the
    agent sends about its own work never makes the agent reflect on itself
    again.

type WatermarkGate interface {
	// Current returns the current execution watermark. A watermark that cannot
	// be established is reported as an error, and the gate stays closed.
	Current(ctx context.Context) (Watermark, error)

	// Advance moves the watermark to the moment a consumed reflection turn was
	// injected at, and leaves it unmoved when that turn was deferred.
	Advance(ctx context.Context, moment time.Time) error
}
    WatermarkGate is the reflection loop's freshness gate over the execution
    watermark.

    The gate reads the watermark and advances it after a reflection turn is
    consumed. A watermark that is missing, or that cannot be read or parsed,
    keeps the gate closed and is reported by name; the gate is never opened
    on a fresh default or on a fallback criterion when the watermark cannot be
    established. The redline list (3 rules): MUST report a watermark it cannot
    establish as an error and keep the gate closed. MUST NOT open the gate on
    a fresh default or on a fallback criterion. MUST NOT advance the watermark
    past a reflection turn that was deferred.
