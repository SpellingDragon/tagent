package event // import "github.com/SpellingDragon/tagent/api/event"

The framework checkpoint family: one fact carrying every recoverable state
slice of the runtime, stated at one partition-sequence cut. A checkpoint is
the framework's own fact and never material of a session chain, so it names no
message the model reads and carries the state of every consumer that delegated
its state to the framework beside the offset that state corresponds to. One
family, one offset semantics: the recovery of any consumer is "read the newest
checkpoint, restore the slices, replay the facts past the offset".

The compress product family: the product a consumer minted for one compress
request, stated as the fact it is — which request it answers, which producer
executed it, the product key the projection carries, the entries the product
covers and the product text. A product is filed once per request: the identity
is content-addressed over the request and the key, so a retried execution
answers the same identity instead of filing a second product.

The compress request family: the judgment that material is due for compression,
stated as the fact it is — who judged, why, what was judged and the identity
the judgment is quoted by — and never as the instruction that performs the
compression. A producer files the request it judged, a consumer reads it,
compresses the material the request names and files the product it minted. A
repeated judgment keeps the identity and the first filing moment of the request
already on record, so one body of material carries one request however often it
is judged.

The judgment is vocabulary rather than a domain mechanism: the mechanisms
that judge it (workspace on a tool result, features/evolution/meditation on
a card sequence) and the log that files it (memory) live in three domains,
and the event axis is the one package all three already speak. The producer and
the reason a mechanism states stay with that mechanism; this file states the
fact they are quoted by.

The configuration change family: the judgment that one dimension of one agent's
configuration moved onto a published generation, stated as the fact it is —
which agent, which dimension, the generation it landed in and what changed — and
never as the instruction that applies the change. A reload states the judgment
once per agent dimension that moved: the prompt and binding dimensions read
their move from the description, the model dimension carries its version move in
the payload beside the exported fields, rendered under the replacement metadata
keys, the file dimension names the file and the content version it observed,
and the organization dimensions state the structure the declaration moved — one
five-field fact per dimension, never a second family.

A configuration change is control-plane material: it names the change,
not a transcript line, so the frozen request role table does not map its type
and a drive never reads it as input. The fact is read by its identity and its
metadata, exactly as the publication ledger it reports beside is read.

The final round request family: the judgment that one range's budget is spent
and its next decision is the closing round, stated as the fact it is — which
range, why, and the inputs the judgment read. A producer files the judgment,
the loop reads it at a decision boundary and runs the closing round; a range
carries one request however often the judgment is re-evaluated.

The model dimension vocabulary of a configuration change: the metadata keys a
config_change fact of the model dimension carries — the version the move starts
from, the version it asks for and the result the producer states — and the
result stamp a producer writes when it asks a lease to apply the move.

The independent model replacement family is retired: a model move is one
configuration change of the model dimension, never a second fact family.
No type, constructor or event shape stands beside the five-field change; a lease
observer reads the same fact under these keys, exactly as it reads the dimension
and the publication generation beside them.

Package event is the runtime's event identity and fact contract: it defines the
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
	// ConfigChangeDimensionPrompt names the prompt sources the agent reads.
	ConfigChangeDimensionPrompt = "prompt"
	// ConfigChangeDimensionBinding names the declaration binding of the agent
	// outside its prompt sources and model reference.
	ConfigChangeDimensionBinding = "binding"
	// ConfigChangeDimensionModel names the model reference the agent's lease
	// serves: the move carries the versions between it in the fact payload.
	ConfigChangeDimensionModel = "model"
	// ConfigChangeDimensionFile names the declared file content the request
	// boundary read: the fact carries which file and which content version it
	// observed, and the boundary projects the content back from it.
	ConfigChangeDimensionFile = "file"
	// ConfigChangeDimensionOrgAgents names the declared agent set: a member
	// added to or removed from the declaration.
	ConfigChangeDimensionOrgAgents = "org.agents"
	// ConfigChangeDimensionOrgEntry names the entry agent the organization
	// speaks to.
	ConfigChangeDimensionOrgEntry = "org.entry"
	// ConfigChangeDimensionOrgAssembly names the config-declared delegation
	// assembly: the agent references one declaration wires — the declared target,
	// the tool name it is reached by, its description file and its event params.
	ConfigChangeDimensionOrgAssembly = "org.assembly"
)
    ConfigChangeDimensionPrompt, ConfigChangeDimensionBinding,
    ConfigChangeDimensionModel and ConfigChangeDimensionFile name the per-agent
    configuration dimensions this family states: the prompt sources one agent is
    assembled from, the remaining declaration binding of that agent, the model
    reference a lease serves the agent through and the declared file content a
    request boundary reads.

    ConfigChangeDimensionOrgAgents, ConfigChangeDimensionOrgEntry and
    ConfigChangeDimensionOrgAssembly name the organization dimensions the
    declaration diff states: the declared agent set, the entry agent the
    organization speaks to and the delegation assembly the declaration wires.
    The organization is the control plane, so a move of its structure is a
    change of this same family under its own dimension instead of a second fact
    type.

const (
	// MetaKeyConfigChangeDimension names the configuration dimension that moved.
	MetaKeyConfigChangeDimension = "config_change_dimension"
	// MetaKeyConfigChangeGeneration names the generation the change landed in.
	MetaKeyConfigChangeGeneration = "config_change_generation"
)
    MetaKeyConfigChangeDimension and MetaKeyConfigChangeGeneration name the
    metadata keys a configuration change fact carries beside the frozen trigger
    source stamp: the dimension that moved and the generation the publication
    assigned. The generation is the publication identity, never a content
    fingerprint, so a reader compares it with the publication ledger directly.

const (
	TypeExternalInput   = "external_input"
	TypeAgentOutput     = "agent_output"
	TypeActionCommand   = "action_command"
	TypeThinkingPlan    = "thinking_plan"
	TypeContextCompress = "context_compress"
	TypeTaskSettled     = "task_settled"
	TypeFeedback        = "feedback"
	TypeGovernance      = "governance"

	// TypeAttentionQuestion is the frozen type of a question put to a target
	// thread or person: the opening fact of one attention lifecycle.
	TypeAttentionQuestion = "attention.question"
	// TypeAttentionAnswer is the frozen type of the ruling a question is
	// answered with.
	TypeAttentionAnswer = "attention.answer"
	// TypeAttentionClearance is the frozen type of the fact that removes a
	// question from the presence read.
	TypeAttentionClearance = "attention.clearance"

	// LineageDelegation is the trigger-source stamp of a delegated call: the
	// delegated turn is traffic the agent manages for itself, so the stamp
	// opens no host-facing gate and is never a freshness fact.
	LineageDelegation = "delegation"
)

    TypeExternalInput, TypeAgentOutput, TypeActionCommand, TypeThinkingPlan,
    TypeContextCompress, TypeTaskSettled, TypeFeedback, TypeGovernance and the
    three TypeAttention roles are the frozen skeleton event type strings.

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

	// MetaKeyNoticeSource names the source one notice message came from: the
	// reincarnation notice, the system alert, a delivery receipt and the like.
	// It is the per-message key the round-level trigger source is read beside —
	// the input of one turn may mix both, so "where this message came from" and
	// "who triggered this turn" are two entries and neither overwrites the
	// other. A notice fact is queryable by source without reading its body.
	MetaKeyNoticeSource = "notice_source"

	// MetaKeyInputEvents names the ordered input fact identities an agent_output
	// actually consumed. It is a causal relation on the existing output fact, not
	// a second answer ledger; the value is a JSON array of event.EventID strings.
	MetaKeyInputEvents = "input_events"

	// MetaKeyDelegation names the resolved declaration one delegated input fact
	// carries: the assembly the delegated round runs on — its name, model,
	// instruction, skill, tool and MCP references and the sampling its requests
	// carry — rendered as JSON. The declaration travels on the fact, so a re-entry
	// after a restart rebuilds the assembly from the site's own chain instead of
	// holding a second durable copy of it in process memory. The key is read on the
	// task fact that closes one delegated input batch; the instruction and material
	// facts of that batch carry none.
	MetaKeyDelegation = "delegation"

	// MetaKeyDeliveryTarget names the one address a delivery reached: the site whose
	// chain took the body. Both delivery families stamp it, so the account of a
	// delivery is read from the same entry whichever path carried it.
	MetaKeyDeliveryTarget = "delivery.target"
	// MetaKeyDeliveryIdentity names the identity one delivery is deduplicated
	// under: the call frame of the sender for an agent's own body, the notification
	// identity for a framework state change. A repeated delivery of one identity
	// lands no second record.
	MetaKeyDeliveryIdentity = "delivery.identity"
	// MetaKeyDeliveryReceipt names the receipt a delivery stands at: the body
	// reached the chain of the target.
	MetaKeyDeliveryReceipt = "delivery.receipt"
	// MetaKeyDeliverySource names the source event one delivery is filed under, so
	// the deliveries of one source event are read as one account beside the
	// identity of the delivery itself.
	MetaKeyDeliverySource = "delivery.source"
	// MetaKeyDeliveryDenial names the category one refused delivery was turned away
	// under: the gate that refused it, or the stage of the round that never landed
	// it. A receipt that stands at delivery or at a sent body carries no category,
	// so the entry is read as the reason word of a refusal and of nothing else.
	MetaKeyDeliveryDenial = "delivery.denial"

	// MetaKeyAttentionKind names the kind a question is asked under: approval,
	// notice or summary, an absent entry reading as approval.
	MetaKeyAttentionKind = "attention.kind"
	// MetaKeyAttentionTarget names the thread or person a question is put to.
	MetaKeyAttentionTarget = "attention.target"
	// MetaKeyAttentionLineage names the context a question came from: a call
	// identity, a task identity or a reflection source.
	MetaKeyAttentionLineage = "attention.lineage"
	// MetaKeyAttentionExpiresMs names the validity of a question as an absolute
	// millisecond stamp; zero reads as cleared when the work ends.
	MetaKeyAttentionExpiresMs = "attention.expires_ms"
	// MetaKeyAttentionClearance names the word a clearance carries: answer,
	// expiry or work-end.
	MetaKeyAttentionClearance = "attention.clearance"

	// MetaKeyCheckpointOffset names the partition-sequence watermark one
	// framework checkpoint covers, rendered as the sorted partition:seq pairs.
	// It is read to decide coverage without decoding the state slices, and it
	// states the same cut the payload states.
	MetaKeyCheckpointOffset = "checkpoint.offset"
	// MetaKeyCheckpointSlices names the state slices one framework checkpoint
	// carries, rendered as the sorted consumer names, so what a checkpoint
	// covers is queryable without decoding the state of every consumer.
	MetaKeyCheckpointSlices = "checkpoint.slices"
)
    MetaKeyEventKey, MetaKeyPartitionID, MetaKeyEventType, MetaKeyEventSummary,
    MetaKeyTriggerSource, MetaKeySettleTriggerSource, MetaKeyBundleID,
    MetaKeyNoticeKind, MetaKeyTaskID, MetaKeyNoticeSource, the five
    MetaKeyDelivery entries and the five MetaKeyAttention entries
    name the frozen metadata keys: the event identity, its partition,
    its type and summary, the trigger source, the settle trigger lineage,
    the bundle attribution, the kind of the notification a notice fact reports,
    the task it reports on, the source one notice message came from,
    the five entries that account one delivery, and the kind, target, lineage,
    validity and clearance word an attention fact carries. MetaKeyInputEvents
    and MetaKeyDelegation name the two facts an event carries beyond its own
    account: the input relation its output consumed and the resolved declaration
    its delegated round runs on.

const (
	// MetaKeyModelFromVersion names the version the move starts from.
	MetaKeyModelFromVersion = "model_from_version"
	// MetaKeyModelToVersion names the version the move asks for.
	MetaKeyModelToVersion = "model_to_version"
	// MetaKeyModelReplacementResult names the result the producer states.
	MetaKeyModelReplacementResult = "model_replacement_result"
)
    MetaKeyModelFromVersion, MetaKeyModelToVersion and
    MetaKeyModelReplacementResult name the metadata keys a configuration change
    of the model dimension carries beside the frozen trigger source stamp:
    the version the move starts from, the version it asks for and the result
    the producer states. The model move adds no exported field to the change:
    these keys are its payload.

const CheckpointVersion = 1
    CheckpointVersion is the payload layout version of a checkpoint fact: a
    payload of another version is refused by name, because restoring state under
    a layout the reader does not know would invent the slices it cannot read.

const CompressProductType = "compress.product"
    CompressProductType is the frozen type of a compress product fact.

const CompressRequestType = "compress.request"
    CompressRequestType is the frozen type of a compress request fact.

const ConfigChangeSourceConfig = "config"
    ConfigChangeSourceConfig is the source stamp of a change judged by a
    declaration reload: the configuration source observed the move and the host
    filed the fact when the new generation settled. The stamp is a vocabulary
    constant, never a field of the fact: every configuration change of this
    family is judged by the same source, so a reader compares it with the
    trigger source stamp the event commit carries.

const FinalRoundRequestType = "final_round.request"
    FinalRoundRequestType is the frozen type of a final round request fact.

const MetaPrefix = "meta_"
    MetaPrefix marks a pass-through business metadata key: any key carrying
    this prefix is forwarded verbatim, and a pass-through key is never read as a
    business field.

const ModelReplacementResultRequested = "requested"
    ModelReplacementResultRequested is the result a producer states when
    it files a move for the lease to apply: the fact records the judgment,
    and the lease records whether the replacement actually took effect in its
    own history.

const TypeCheckpoint = "checkpoint"
    TypeCheckpoint is the frozen type of a framework checkpoint fact.

const TypeConfigChange = "config_change"
    TypeConfigChange is the frozen type of a configuration change fact.

FUNCTIONS

func CheckpointKey(seq int64) string
    CheckpointKey is the fact key one checkpoint is filed under: the frozen
    `checkpoint:<partitionSeq>` spelling, whose sequence slot is the partition
    sequence the cut advanced to.

func CheckpointPayloadOf(c Checkpoint) ([]byte, error)
    CheckpointPayloadOf renders the canonical payload bytes of one checkpoint:
    the cut, the identity and the slices exactly as the identity was
    content-addressed over them.

func CompressionMechanism(typ string) bool
    CompressionMechanism reports whether one frozen event type names a fact of
    the compression mechanism itself: a judgment or the product that answers it.
    Such a fact carries an internal summary and recovery metadata, never
    material the query face, candidate embedding or the model's context may
    surface.

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

type Checkpoint struct {
	// Key is the fact key the checkpoint commits under: the partition-sequence
	// slot it was taken at, so the key of every checkpoint of a partition is
	// monotone past the key of the one before it and no two cuts share one.
	Key string
	// ID is the content-addressed identity of the checkpoint: two cuts stating
	// the same state answer the same identity.
	ID EventID
	// Offset is the partition-sequence watermark the state corresponds to.
	Offset CheckpointOffset
	// Slices is the state of every registered consumer, in registration order.
	Slices []CheckpointSlice
}
    Checkpoint is one framework checkpoint fact: the content-addressed identity,
    the cut it states and the state slices restored from it. The writing moment
    is the commit stamp of the fact, so it is not stated twice.

func CheckpointFor(offset CheckpointOffset, slices []CheckpointSlice) (Checkpoint, error)
    CheckpointFor returns the checkpoint fact for one cut: the identity is
    content-addressed over the layout version, the offset and the slices,
    so a cut that states the very state a filed checkpoint already states keeps
    one identity instead of stating a second copy of it.

func CheckpointOf(evt Event) (Checkpoint, bool)
    CheckpointOf reads one committed fact back as the checkpoint it carries,
    and reports whether the fact states one. A fact of another frozen type,
    a payload of another layout version, and a payload whose stated offset
    and slices do not content-address to the identity it states all answer
    no checkpoint: state is restored from the one payload that states it,
    and a payload that states other state under that identity is refused rather
    than restored as if it were its own.

func (c Checkpoint) CheckpointSliceNames() []string
    CheckpointSliceNames reports the names of the slices one checkpoint carries,
    sorted, so a reader that only needs to know what a checkpoint covers answers
    without decoding the state of every consumer.

func (c Checkpoint) FactKey() string
    FactKey returns the checkpoint's identity as a fact key: the partition
    sequence slot the checkpoint commits under.

func (c Checkpoint) FactType() string
    FactType returns the frozen type a checkpoint is read as.

func (c Checkpoint) MetaEntries() map[string]string
    MetaEntries states the two metadata entries one checkpoint fact carries
    from the cut it was taken at: the partition-sequence watermark and the slice
    names, both sorted so the fact renders the same text for the same coverage.

type CheckpointOffset map[int]int64
    CheckpointOffset is the partition-sequence watermark one checkpoint covers:
    for every partition, the sequence slot the partition had committed when the
    cut was taken. It is the partition sequence the landing path numbers commits
    with, never a process-local counter: a process-local number is neither
    monotonic across restarts nor comparable between partitions, so it can never
    stand for "everything up to here is covered".

func (o CheckpointOffset) String() string
    String renders one watermark as the sorted partition:seq pairs.

type CheckpointPayload struct {
	Version int               `json:"version"`
	ID      EventID           `json:"id"`
	Offset  CheckpointOffset  `json:"offset"`
	Slices  []CheckpointSlice `json:"slices"`
}
    CheckpointPayload is the payload layout of one checkpoint fact: the layout
    version, the identity the payload content-addresses to, the cut and the
    state slices — Projection, Delivery, Mailbox and Tasks — in registration
    order.

type CheckpointSlice struct {
	// Name is the consumer name the slice is keyed by.
	Name string `json:"name"`
	// Payload is the consumer's own recoverable state, as JSON.
	Payload json.RawMessage `json:"payload"`
}
    CheckpointSlice is the recoverable state one registered consumer delegated
    to the framework: the consumer name it is keyed by beside the consumer's
    own payload. The payload is the consumer's business — the fact family
    carries the name and the bytes, never a structure a reader of the fact could
    interpret as state of its own.

type CompressProduct struct {
	// ID is the identity of the product: content-addressed over the answers and
	// the product key, so one request answers one product identity.
	ID EventID
	// Answers names the judgment this product answers and the mechanism that
	// executed the compression.
	Answers ProductAnswers
	// Key is the product key the projection carries the entry under; its prefix
	// states the kind.
	Key EventID
	// Covered lists the entry keys the product covers, in their original order.
	Covered []EventID
	// Text is the product text the projection presents.
	Text string

	// Has unexported fields.
}
    CompressProduct is one compress product fact: the material a consumer folded
    for one judgment. It is immutable once filed, and it is the only source the
    projection restores a synthetic product entry from. What the product is — a
    folded segment or a history summary — is read off the key's prefix, and the
    filing moment is the commit stamp of the fact, so neither is stated twice.

func CompressProductFor(request EventID, producer string, key EventID, covered []EventID, text string) CompressProduct
    CompressProductFor returns the product fact one execution files:
    the identity is content-addressed over the request it answers and the key
    it carries, so a repeated execution of the same judgment files nothing new.
    The filing moment stays zero for the log to stamp on first filing.

func (p CompressProduct) Checkpoint() []byte
    Checkpoint reports the opaque recovery payload this product fact carries.

func (p CompressProduct) FactKey() string
    FactKey returns the product's identity as a fact key: the identity one
    execution is quoted by, unchanged by every retry of that execution.

func (p CompressProduct) FactType() string
    FactType returns the frozen type a compress product is read as.

func (p CompressProduct) WithCheckpoint(payload []byte) CompressProduct
    WithCheckpoint states the opaque recovery payload this product fact carries.

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

type ConfigChange struct {
	// ID is the identity of the judgment: the content-addressed key the change
	// is quoted by, derived from every field that states the move.
	ID EventID
	// AgentID names the agent whose declared dimension moved; the fact is owned
	// by this agent.
	AgentID AgentID
	// Dimension names the configuration dimension that moved.
	Dimension string
	// Generation is the publication identity the change landed in.
	Generation uint64
	// Description states the change the fact reports.
	Description string

	// Has unexported fields.
}
    ConfigChange is one configuration change fact: the judgment that one
    dimension of one agent's configuration moved onto a published generation.
    It names the agent the fact is owned by, the dimension that moved,
    the generation the publication assigned and the change it describes,
    and it is immutable once filed — the same judgment files the same identity,
    and a retry never refiles it under a second one.

    The five exported fields state every configuration change. The model
    dimension's version move is carried in the unexported payload beside them
    and rendered by Event under the replacement metadata keys, so the model move
    adds no exported field and no second fact family: a reader of a model change
    reads the same five-field fact under the model dimension.

func ConfigChangeFor(agentID AgentID, dimension string, generation uint64, description string) ConfigChange
    ConfigChangeFor returns the change fact one reload files for a dimension
    stated by its description: the identity is content-addressed over the agent,
    the dimension, the generation, the description and the frozen source,
    so the same judgment always answers the same identity and a re-judgment adds
    no second fact. The filing moment stays zero for the log to stamp on first
    filing.

func ConfigChangeModelFor(agentID AgentID, generation uint64, from, to, result string) ConfigChange
    ConfigChangeModelFor returns the change fact of one model reference move:
    the identity is content-addressed over the agent, the model dimension, the
    generation and the versions and result the move states, so the same judgment
    always answers the same identity and a re-judgment adds no second fact.
    The description names the move; the versions and result ride in the payload
    and are rendered under the replacement metadata keys by Event.

func (c ConfigChange) Event(at time.Time) Event
    Event renders the change fact as the event it commits as: the frozen type,
    the fact identity as the event key, the agent as the owner, the trigger
    source stamp the observation gate reads, the dimension and the publication
    generation in the event metadata — and, for the model dimension, the version
    move under the model replacement metadata keys. The message carries the
    change description, so a committed configuration change is readable on the
    chain exactly as it was filed; the projection boundary maps no entry for the
    type, so a model request never reads the judgment as transcript.

func (c ConfigChange) FactKey() string
    FactKey returns the change's identity as a fact key: the content-addressed
    key one judgment is quoted by, unchanged by every re-filing of that
    judgment.

func (c ConfigChange) FactType() string
    FactType returns the frozen type a configuration change is read as.

type DecisionID string
    DecisionID identifies one model decision boundary.

type DeliveryMeta struct {
	// Target is the address a delivery reached.
	Target string
	// Identity is the identity one delivery is deduplicated under.
	Identity string
	// Receipt is the receipt a delivery stands at.
	Receipt string
	// Source is the source event one delivery is filed under.
	Source string
	// Denial is the category one refused delivery was turned away under, empty for
	// a delivery that landed.
	Denial string
}
    DeliveryMeta is the entries that account one delivery: the address the
    body reached, the identity the delivery is deduplicated under, the receipt
    it stands at, the source event it is filed under and, for an attempt that
    reached nothing, the stage that turned it away. Both delivery families stamp
    the same landed entries, so one delivery is read as one block whichever
    route carried the body, and a refusal reads through the same block beside
    the one extra entry.

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

type FinalRoundRequest struct {
	// ID is the identity of the judgment: content-addressed over the range and
	// the reason, so the same judgment never files a second request.
	ID EventID
	// Range is the invocation whose budget the judgment read.
	Range InvocationID
	// Reason states why the budget is spent: the round count or the elapsed time.
	Reason string
	// Rounds is the number of tool rounds the range had run when judged.
	Rounds int
	// Elapsed is the time the range had spent when judged.
	Elapsed time.Duration
}
    FinalRoundRequest is one final round request fact: the judgment that a
    range's budget is spent. It is immutable once filed, and its identity is
    independent of the moment and the measured inputs, so one range answers one
    request.

func FinalRoundRequestFor(rangeID InvocationID, reason string, rounds int, elapsed time.Duration) FinalRoundRequest
    FinalRoundRequestFor returns the request fact one judgment files:
    the identity is content-addressed over the range and the reason, so a
    re-evaluation of the same spent budget answers the same identity. The filing
    moment stays zero for the log to stamp on first filing.

func (r FinalRoundRequest) FactKey() string
    FactKey returns the request's identity as a fact key: the identity one range
    is quoted by, unchanged by every re-evaluation of the same budget.

func (r FinalRoundRequest) FactType() string
    FactType returns the frozen type a final round request is read as.

type InstanceID string
    InstanceID identifies one runtime instance. Rebuilding an agent under the
    same name yields a different instance.

type InvocationID string
    InvocationID identifies one responsibility scope: a trusted admission or a
    delegation entry. One session may hold several invocations at the same time.

type NoticeMeta struct {
	// Kind is the kind of the notification a notice fact reports.
	Kind string
	// TaskID is the task a notice fact reports on.
	TaskID string
}
    NoticeMeta is the pair a notice fact states: the kind of the notification it
    reports and the task it reports on. The two entries are read and written as
    one block, so a fact either names the notification it carries or names none.

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
	// Notice is the notice-fact pair the fact states: the kind of the
	// notification it reports and the task it reports on.
	Notice NoticeMeta
	// Delivery is the five entries that account one delivery the fact carried.
	Delivery DeliveryMeta
	// PassThrough holds every MetaPrefix entry verbatim: such an entry is
	// forwarded as it stands and is never read as a frozen business field.
	PassThrough map[string]string
	// Unknown holds every entry that is neither a frozen key nor a prefixed key,
	// kept verbatim so an entry this parser does not model survives unchanged.
	Unknown map[string]string
}
    ParsedMeta is the structured form of one event metadata map: the frozen keys
    declared above, the business entries carried by the pass-through prefix,
    and every other entry kept verbatim. The notice pair and the delivery
    entries are each read as one block, so the metadata of one notice fact and
    the account of one delivery are named by the concept they belong to rather
    than by a run of sibling fields.

func ParseEventMeta(meta map[string]string) ParsedMeta
    ParseEventMeta projects one metadata map into its structured form. A frozen
    key lands in its named field, a MetaPrefix entry lands in PassThrough,
    and any other entry lands in Unknown; no entry is dropped and the input map
    is left exactly as it was.

type ProductAnswers struct {
	// Request is the judgment this product answers.
	Request EventID
	// Producer names the mechanism that executed the compression.
	Producer string
}
    ProductAnswers names the judgment one product answers beside the mechanism
    that executed it.

type SessionID string
    SessionID identifies one conversation session of an agent.

type TaskID string
    TaskID identifies one task across every execution round it runs.
