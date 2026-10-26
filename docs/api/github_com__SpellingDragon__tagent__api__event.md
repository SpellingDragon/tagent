package event // import "github.com/SpellingDragon/tagent/api/event"

The compress request family: the judgment that material is due for compression,
stated as the fact it is — who judged, why, what was judged and the identity
the judgment is quoted by — and never as the instruction that performs the
compression. A producer files the request it judged, a consumer reads it,
compresses the material the request names and files the product it minted. A
repeated judgment keeps the identity and the first filing moment of the request
already on record, so one body of material carries one request however often it
is judged.

The judgment is vocabulary rather than a domain mechanism: the mechanisms that
judge it (workspace on a tool result, evolution/meditation on a card sequence)
and the log that files it (memory) live in three domains, and the event axis is
the one package all three already speak. The producer and the reason a mechanism
states stay with that mechanism; this file states the fact they are quoted by.

The model replacement family: the judgment that one agent's model reference
moves from the version in effect to another version, stated as the fact it is —
which agent, which versions, what source judged it and the result the producer
states — and never as the instruction that rebuilds and installs a model. A
producer files the replacement it judged, the lease that serves the agent reads
it and runs its own rebuild, publication and installation steps. A repeated
judgment keeps the identity and the first filing moment of the replacement
already on record, so one move of one agent carries one fact however often it is
judged.

The configuration change is one source of the fact, never the fact itself:
the source stamp names what judged the move ("config" for a declaration change)
and another source states its own stamp without a second type.

Package event is the runtime's event identity and fact carrier: it defines the
typed identifiers an event carries, the address of the agent session an event
belongs to, and the event payload itself.

The payload keeps the native locked tRPC model message, so no second message
representation is built beside it. Every identifier is a distinct string type,
so a session identifier cannot be passed where an agent identifier is expected.
The package also declares Fact, the fact category contract each fact family
satisfies in its own package; the families are registered in the fact vocabulary
page.

CONSTANTS

const (
	TypeExternalInput   = "external_input"
	TypeAgentOutput     = "agent_output"
	TypeActionCommand   = "action_command"
	TypeThinkingPlan    = "thinking_plan"
	TypeContextCompress = "context_compress"
	TypeTaskSettled     = "task_settled"
	TypeFeedback        = "feedback"
	TypeGovernance      = "governance"

	// LineageDelegation is the trigger-source stamp of a delegated call: the
	// delegated turn is traffic the agent manages for itself, so the stamp
	// opens no host-facing gate and is never a freshness fact.
	LineageDelegation = "delegation"
)

    TypeExternalInput, TypeAgentOutput, TypeActionCommand, TypeThinkingPlan,
    TypeContextCompress, TypeTaskSettled, TypeFeedback and TypeGovernance are
    the frozen skeleton event type strings.

    LineageDelegation is the trigger-source stamp a delegated call carries on
    its admission record: the delegated turn is traffic the agent manages for
    itself. Every value except TypeTaskSettled is kept from the stored event
    vocabulary, so events already stored stay readable; TypeTaskSettled raises
    the settled task notification to a first-class event type and carries its
    own name.

const (
	MetaKeyEventKey            = "event_key"
	MetaKeyPartitionID         = "partition_id"
	MetaKeyEventType           = "event_type"
	MetaKeyEventSummary        = "event_summary"
	MetaKeyTriggerSource       = "trigger_source"
	MetaKeySettleTriggerSource = "settle_trigger_source"
	MetaKeyBundleID            = "bundle_id"
	MetaKeyNoticeKind          = "kind"
	MetaKeyTaskID              = "task_id"
)
    MetaKeyEventKey, MetaKeyPartitionID, MetaKeyEventType, MetaKeyEventSummary,
    MetaKeyTriggerSource, MetaKeySettleTriggerSource, MetaKeyBundleID,
    MetaKeyNoticeKind and MetaKeyTaskID name the frozen metadata keys: the event
    identity, its partition, its type and summary, the trigger source, the
    settle trigger lineage, the bundle attribution, the kind of the notification
    a notice fact reports and the task it reports on.

const (
	// MetaKeyModelFromVersion names the version the move starts from.
	MetaKeyModelFromVersion = "model_from_version"
	// MetaKeyModelToVersion names the version the move asks for.
	MetaKeyModelToVersion = "model_to_version"
	// MetaKeyModelReplacementResult names the result the producer states.
	MetaKeyModelReplacementResult = "model_replacement_result"
)
    MetaKeyModelFromVersion, MetaKeyModelToVersion and
    MetaKeyModelReplacementResult name the metadata keys a model replacement
    fact carries beside the frozen trigger source stamp: the version the move
    starts from, the version it asks for and the result the producer states.

const CompressRequestType = "compress.request"
    CompressRequestType is the frozen type of a compress request fact.

const MetaPrefix = "meta_"
    MetaPrefix marks a pass-through business metadata key: any key carrying
    this prefix is forwarded verbatim, and a pass-through key is never read as a
    business field.

const ModelReplacementResultRequested = "requested"
    ModelReplacementResultRequested is the result a producer states when
    it files a move for the lease to apply: the fact records the judgment,
    and the lease records whether the replacement actually took effect in its
    own history.

const ModelReplacementSourceConfig = "config"
    ModelReplacementSourceConfig is the source stamp of a replacement judged
    by a declaration change: the agent's declared model reference moved and the
    configuration reload filed the fact.

const ModelReplacementType = "model.replacement"
    ModelReplacementType is the frozen type of a model replacement fact.

TYPES

type Address struct {
	// AgentID identifies the agent.
	AgentID AgentID
	// SessionID identifies the session of that agent.
	SessionID SessionID
}
    Address names the agent session a payload belongs to. It is an explicit
    target: neither a default nor a most-recent session is substituted for it.

func (a Address) IsComplete() bool
    IsComplete reports whether the address names both an agent and a session.

func (a Address) IsZero() bool
    IsZero reports whether the address names no agent and no session.

type AgentID string
    AgentID identifies an agent inside an application.

type AttemptID string
    AttemptID identifies one observable model attempt; a retry hidden below the
    decision boundary does not fake a new attempt.

type CompressRequest struct {
	// ID is the identity of the judgment: the content-addressed key the request
	// is quoted by, derived from the producer, the subject and the reason.
	ID EventID
	// Producer names the mechanism that judged the material due for compression.
	Producer string
	// Reason states why that mechanism judged it due.
	Reason string
	// Subject is the identity of the material judged: the content-addressed key
	// of the sequence or result the request names.
	Subject string
	// At is the moment the judgment was filed.
	At time.Time
}
    CompressRequest is one compress request fact: the judgment that material is
    over its bound. It names who judged, why, what was judged and the identity
    the request is quoted by, and it is immutable once filed — the same judgment
    files the same identity, and a retry never refiles it under a second one.

func CompressRequestFor(producer, subject, reason string) CompressRequest
    CompressRequestFor returns the request fact one judgment files: the identity
    is content-addressed over the producer, the subject and the reason, so the
    same judgment always answers the same identity and a re-judgment adds no
    second request. The filing moment stays zero for the log to stamp on first
    filing.

func (r CompressRequest) FactKey() string
    FactKey returns the compress request's identity as a fact key:
    the content-addressed key one judgment is quoted by, unchanged by every
    re-filing of that judgment.

func (r CompressRequest) FactType() string
    FactType returns the frozen type a compress request is read as.

type DecisionID string
    DecisionID identifies one model decision boundary.

type EpisodeID string
    EpisodeID identifies an application level episode and its causal lineage;
    a session or a trace is never substituted for it.

type Event struct {
	// Key is the canonical identity of the event, in its hex form. It heads the
	// identity block, with Type and Timestamp: what the stored fact is on its
	// own. An event is quoted by Key alone; Type and Timestamp describe that
	// fact and never re-identify it.
	Key EventID
	// Type is the frozen event type, one of the Type constants of this package.
	Type string
	// Timestamp is the time the event was recorded.
	Timestamp time.Time

	// Meta carries the event metadata entries keyed by the MetaKey constants of
	// this package. It heads the content block, with Message: what the event
	// carries. Meta and Message are two faces of one fact, and neither is a
	// second representation of the other.
	Meta map[string]string
	// Message is the native model payload the event carries.
	Message model.Message

	// Owner is the address that owns the event. It heads the ownership block,
	// with Source, Target, InvocationID, TaskID, InstanceID, EpisodeID,
	// DecisionID and AttemptID: the addresses and identifiers that place the
	// event.
	//
	// Source and Target are the addresses the event came from and is delivered
	// to; the source instance and the notification target instance are kept
	// apart, and Target is never defaulted. The identifiers scope the event to
	// one responsibility range, task, runtime instance, episode, model decision
	// and model attempt, and stay empty where the event falls outside of them.
	Owner Address
	// Source is the address the event came from; the source instance and the
	// notification target instance are kept apart.
	Source Address
	// Target is the address the event is delivered to, required and never
	// defaulted.
	Target Address
	// InvocationID scopes the event to one responsibility range.
	InvocationID InvocationID
	// TaskID is the task the event belongs to; it is empty for an event outside
	// a task.
	TaskID TaskID
	// InstanceID is the runtime instance the event belongs to. The deduplication
	// scope is (InstanceID, InvocationID, DecisionID, CallID).
	InstanceID InstanceID
	// EpisodeID is the application level episode the event belongs to; it is
	// never derived from a session or a trace.
	EpisodeID EpisodeID
	// DecisionID scopes the event to one model decision.
	DecisionID DecisionID
	// AttemptID scopes the event to one observable model attempt.
	AttemptID AttemptID

	// CallID is the native provider call identifier, kept byte-identical and
	// never regenerated. It heads the ledger block, with ApprovalID; those two
	// keys are what the deduplication scope and the approval record book the
	// call by. It is not globally unique, so the deduplication scope always
	// includes InstanceID, InvocationID and DecisionID with it.
	CallID string
	// ApprovalID is the approval ticket identity of the call this event records:
	// tagent issues it when the host parks the call in a hold, and it is globally
	// unique. It is empty when the call needs no approval.
	ApprovalID string
}
    Event is one immutable runtime fact: the canonical key it is stored under,
    its frozen type, the time it was recorded, its metadata projection,
    the native model payload it carries, and the identities that scope it. Its
    fields form four blocks — identity, content, ownership and ledger — and the
    doc of the first field of each names the block and what its fields share.

    The content block carries the native locked tRPC model message: no local
    message type stands beside it, so a fact stored by one process reads back as
    the same payload in another.

func (e Event) FactKey() string
    FactKey returns the event's canonical key as its fact identity, spelt as the
    hex string the store holds the event under; Type and Timestamp describe that
    fact and never re-identify it.

func (e Event) FactType() string
    FactType returns the event's frozen type, one of the Type constants of this
    package.

type EventID string
    EventID is the canonical identity of a stored event.

type Fact interface {
	// FactKey is the identity the fact is quoted by: one fact answers the same
	// key every time it is asked, and a retry keeps the original identity.
	FactKey() string
	// FactType is the frozen type the fact is read as.
	FactType() string
}
    Fact is the fact category contract every fact family satisfies: the identity
    it is quoted by and the frozen type it is read as. Both questions are
    asked of the fact as it stands, because a fact is immutable once produced
    — neither answer is ever re-derived from the store or from the consumer
    that reads it. The identity is a plain string rather than EventID: it is a
    spelling the vocabulary registers, so a family living in a package that must
    not import this one still states it. The families, the package each of them
    lives in and the mechanism that locks each identity are registered in the
    fact vocabulary page; the event family below is the canonical one.

    红线清单（2 条）： MUST answer the same identity and the same frozen type every time
    one fact is asked MUST NOT re-derive an identity from a non-identity field
    or rewrite a quoted identity

type InstanceID string
    InstanceID identifies one runtime instance. Rebuilding an agent under the
    same name yields a different instance.

type InvocationID string
    InvocationID identifies one responsibility scope: a trusted admission or a
    delegation entry. One session may hold several invocations at the same time.

type ModelReplacement struct {
	// ID is the identity of the judgment: the content-addressed key the
	// replacement is quoted by, derived from every field that states the move.
	ID EventID
	// AgentID names the agent whose model reference moves; the fact is owned by
	// this agent.
	AgentID AgentID
	// FromVersion is the version the move starts from, as the rendered identity
	// name it was declared under.
	FromVersion string
	// ToVersion is the version the move asks for: the version a rebuilt model
	// is installed under, never a rendered name.
	ToVersion string
	// Source names what judged the move ("config" for a declaration change).
	Source string
	// Result states the result the producer records for the move.
	Result string
	// At is the moment the judgment was filed.
	At time.Time
}
    ModelReplacement is one model replacement fact: the judgment that one
    agent's model reference moves from one version to another. It names the
    agent the fact is owned by, the version it starts from, the version it asks
    for, the source that judged the move and the result the producer states,
    and it is immutable once filed — the same judgment files the same identity,
    and a retry never refiles it under a second one.

func ModelReplacementFor(agentID AgentID, from, to, source, result string) ModelReplacement
    ModelReplacementFor returns the replacement fact one judgment files:
    the identity is content-addressed over every field that states the move,
    so the same judgment always answers the same identity and a re-judgment adds
    no second fact. The filing moment stays zero for the log to stamp on first
    filing.

func (r ModelReplacement) Event(at time.Time) Event
    Event renders the replacement fact as the event it commits as: the frozen
    type, the fact identity as the event key, the agent as the owner, the
    trigger source stamp the observation gate reads and the versions and result
    in the event metadata. The message carries the one-line read of the move,
    so a committed replacement is readable in the mapped sequence exactly as it
    was filed.

func (r ModelReplacement) FactKey() string
    FactKey returns the replacement's identity as a fact key: the
    content-addressed key one judgment is quoted by, unchanged by every
    re-filing of that judgment.

func (r ModelReplacement) FactType() string
    FactType returns the frozen type a model replacement is read as.

type ParsedMeta struct {
	// EventKey is the canonical event identity the entry names.
	EventKey string
	// PartitionID is the partition the event is filed under.
	PartitionID string
	// EventType is the frozen type of the event.
	EventType string
	// EventSummary is the short human-readable summary of the event.
	EventSummary string
	// TriggerSource names the trigger the event answers.
	TriggerSource string
	// SettleTriggerSource names the settle lineage the event reports.
	SettleTriggerSource string
	// BundleID is the bundle attribution of the event.
	BundleID string
	// NoticeKind is the kind of the notification a notice fact reports.
	NoticeKind string
	// TaskID is the task a notice fact reports on.
	TaskID string
	// PassThrough holds every MetaPrefix entry verbatim: such an entry is
	// forwarded as it stands and is never read as a frozen business field.
	PassThrough map[string]string
	// Unknown holds every entry that is neither a frozen key nor a prefixed key,
	// kept verbatim so an entry this parser does not model survives unchanged.
	Unknown map[string]string
}
    ParsedMeta is the structured form of one event metadata map: the frozen keys
    declared above, the business entries carried by the pass-through prefix,
    and every other entry kept verbatim.

func ParseEventMeta(meta map[string]string) ParsedMeta
    ParseEventMeta projects one metadata map into its structured form. A frozen
    key lands in its named field, a MetaPrefix entry lands in PassThrough,
    and any other entry lands in Unknown; no entry is dropped and the input map
    is left exactly as it was.

type SessionID string
    SessionID identifies one conversation session of an agent.

type TaskID string
    TaskID identifies one task across every execution round it runs.
