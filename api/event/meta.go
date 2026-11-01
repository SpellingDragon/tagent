package event

import "strings"

// 契约: docs/wiki/event/event-architecture.md#const-list
//
// TypeExternalInput, TypeAgentOutput, TypeActionCommand, TypeThinkingPlan,
// TypeContextCompress, TypeTaskSettled, TypeFeedback, TypeGovernance and the
// three TypeAttention roles are the frozen skeleton event type strings.
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

// MetaKeyEventKey, MetaKeyPartitionID, MetaKeyEventType, MetaKeyEventSummary,
// MetaKeyTriggerSource, MetaKeySettleTriggerSource, MetaKeyBundleID,
// MetaKeyNoticeKind, MetaKeyTaskID, MetaKeyNoticeSource, the four
// MetaKeyDelivery entries and the five MetaKeyAttention entries name the frozen
// metadata keys: the event identity, its partition, its type and summary, the
// trigger source, the settle trigger lineage, the bundle attribution, the kind of
// the notification a notice fact reports, the task it reports on, the source one
// notice message came from, the four entries that account one delivery, and the
// kind, target, lineage, validity and clearance word an attention fact carries.
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
)

// MetaPrefix marks a pass-through business metadata key: any key carrying this
// prefix is forwarded verbatim, and a pass-through key is never read as a
// business field.
const MetaPrefix = "meta_"

// NoticeMeta is the pair a notice fact states: the kind of the notification it
// reports and the task it reports on. The two entries are read and written as
// one block, so a fact either names the notification it carries or names none.
type NoticeMeta struct {
	// Kind is the kind of the notification a notice fact reports.
	Kind string
	// TaskID is the task a notice fact reports on.
	TaskID string
}

// DeliveryMeta is the four entries that account one delivery: the address the
// body reached, the identity the delivery is deduplicated under, the receipt it
// stands at and the source event it is filed under. Both delivery families stamp
// these same four entries, so one delivery is read as one block whichever route
// carried the body.
type DeliveryMeta struct {
	// Target is the address a delivery reached.
	Target string
	// Identity is the identity one delivery is deduplicated under.
	Identity string
	// Receipt is the receipt a delivery stands at.
	Receipt string
	// Source is the source event one delivery is filed under.
	Source string
}

// ParsedMeta is the structured form of one event metadata map: the frozen keys
// declared above, the business entries carried by the pass-through prefix, and
// every other entry kept verbatim. The notice pair and the four delivery entries
// are each read as one block, so the metadata of one notice fact and the account
// of one delivery are named by the concept they belong to rather than by a run
// of sibling fields.
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
	// Delivery is the four entries that account one delivery the fact carried.
	Delivery DeliveryMeta
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
		case MetaKeyNoticeKind:
			parsed.Notice.Kind = value
		case MetaKeyTaskID:
			parsed.Notice.TaskID = value
		case MetaKeyDeliveryTarget:
			parsed.Delivery.Target = value
		case MetaKeyDeliveryIdentity:
			parsed.Delivery.Identity = value
		case MetaKeyDeliveryReceipt:
			parsed.Delivery.Receipt = value
		case MetaKeyDeliverySource:
			parsed.Delivery.Source = value
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
