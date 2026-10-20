package event

import "strings"

// 契约: docs/wiki/event/event-architecture.md#const-list
//
// TypeExternalInput, TypeAgentOutput, TypeActionCommand, TypeThinkingPlan,
// TypeContextCompress, TypeTaskSettled, TypeFeedback and TypeGovernance are the
// frozen skeleton event type strings.
//
// LineageDelegation is the trigger-source stamp a delegated call carries on its
// admission record: the delegated turn is traffic the agent manages for itself.
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

	// LineageDelegation is the trigger-source stamp of a delegated call: the
	// delegated turn is traffic the agent manages for itself, so the stamp
	// opens no host-facing gate and is never a freshness fact.
	LineageDelegation = "delegation"
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

// ParsedMeta is the structured form of one event metadata map: the frozen keys
// declared above, the business entries carried by the pass-through prefix, and
// every other entry kept verbatim.
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
	// PassThrough holds every MetaPrefix entry verbatim: such an entry is
	// forwarded as it stands and is never read as a frozen business field.
	PassThrough map[string]string
	// Unknown holds every entry that is neither a frozen key nor a prefixed key,
	// kept verbatim so an entry this parser does not model survives unchanged.
	Unknown map[string]string
}

// ParseEventMeta projects one metadata map into its structured form. A frozen
// key lands in its named field, a MetaPrefix entry lands in PassThrough, and any
// other entry lands in Unknown; no entry is dropped and the input map is left
// exactly as it was.
func ParseEventMeta(meta map[string]string) ParsedMeta {
	parsed := ParsedMeta{
		PassThrough: map[string]string{},
		Unknown:     map[string]string{},
	}
	for key, value := range meta {
		switch key {
		case MetaKeyEventKey:
			parsed.EventKey = value
		case MetaKeyPartitionID:
			parsed.PartitionID = value
		case MetaKeyEventType:
			parsed.EventType = value
		case MetaKeyEventSummary:
			parsed.EventSummary = value
		case MetaKeyTriggerSource:
			parsed.TriggerSource = value
		case MetaKeySettleTriggerSource:
			parsed.SettleTriggerSource = value
		case MetaKeyBundleID:
			parsed.BundleID = value
		default:
			if strings.HasPrefix(key, MetaPrefix) {
				parsed.PassThrough[key] = value
				continue
			}
			parsed.Unknown[key] = value
		}
	}
	return parsed
}
