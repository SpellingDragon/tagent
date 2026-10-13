package event

// 契约: docs/wiki/event/event-architecture.md#const-list
//
// TypeExternalInput, TypeAgentOutput, TypeActionCommand, TypeThinkingPlan,
// TypeContextCompress, TypeTaskSettled, TypeFeedback and TypeGovernance are the
// frozen skeleton event type strings.
//
// Every value except TypeTaskSettled is kept from the stored event vocabulary, so
// events already stored stay readable; TypeTaskSettled raises the settled task
// notification to a first-class event type and carries its own name.
const (
	TypeExternalInput   = "external_input"
	TypeAgentOutput     = "agent_output"
	TypeActionCommand   = "action_command"
	TypeThinkingPlan    = "thinking_plan"
	TypeContextCompress = "context_compress"
	TypeTaskSettled     = "task_settled"
	TypeFeedback        = "feedback"
	TypeGovernance      = "governance"
)

// MetaKeyEventKey, MetaKeyPartitionID, MetaKeyEventType, MetaKeyEventSummary,
// MetaKeyTriggerSource, MetaKeySettleTriggerSource and MetaKeyBundleID name the
// frozen metadata keys: the event identity, its partition, its type and summary,
// the trigger source, the settle trigger lineage and the bundle attribution.
const (
	MetaKeyEventKey            = "event_key"
	MetaKeyPartitionID         = "partition_id"
	MetaKeyEventType           = "event_type"
	MetaKeyEventSummary        = "event_summary"
	MetaKeyTriggerSource       = "trigger_source"
	MetaKeySettleTriggerSource = "settle_trigger_source"
	MetaKeyBundleID            = "bundle_id"
)

// MetaPrefix marks a pass-through business metadata key: any key carrying this
// prefix is forwarded verbatim, and a pass-through key is never read as a
// business field.
const MetaPrefix = "meta_"
