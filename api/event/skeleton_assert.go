package event

import (
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// 契约: docs/wiki/event/event-architecture.md#overview
//
// Skeleton compile-time assertions for the phase 1 event identity and payload
// types.
//
// The zero-value composite literals below name every documented field of Event
// and Address, so a renamed field or a changed field type stops the package from
// compiling, and the signature assertions pin the exported predicates.
var (
	_ = Event{
		Key:          "",
		Type:         "",
		Timestamp:    time.Time{},
		Meta:         map[string]string{},
		Message:      model.Message{},
		Owner:        Address{},
		Source:       Address{},
		Target:       Address{},
		InvocationID: "",
		TaskID:       "",
		InstanceID:   "",
		EpisodeID:    "",
		DecisionID:   "",
		AttemptID:    "",
		CallID:       "",
		ApprovalID:   "",
	}

	_ = Address{
		AgentID:   "",
		SessionID: "",
	}
)

// The exported address predicates must keep exactly these signatures.
var (
	_ func(Address) bool = Address.IsZero
	_ func(Address) bool = Address.IsComplete
)

// The compress request family keeps its declared shapes: the judgment a producer
// files, its identity derivation and the frozen type it is read as.
var (
	_ = CompressRequest{ID: "", Producer: "", Reason: "", Subject: "", At: time.Time{}}

	_ func(string, string, string) CompressRequest = CompressRequestFor
)

// CompressRequestType is a frozen type word, not a computed value.
const _ = CompressRequestType

// CompressProductType is a frozen type word, not a computed value.
const _ = CompressProductType

// FinalRoundRequestType is a frozen type word, not a computed value.
const _ = FinalRoundRequestType

// The model dimension vocabulary of a configuration change keeps its frozen
// names: the result stamp a producer states and the metadata keys the move is
// rendered under. The independent replacement family is retired, so no fact type
// stands here beside the change that carries the move.
const (
	_ = ModelReplacementResultRequested
	_ = MetaKeyModelFromVersion
	_ = MetaKeyModelToVersion
	_ = MetaKeyModelReplacementResult
)

// The configuration change family keeps its declared shapes: the judgment a
// reload files, its identity derivation, the event it commits as and the frozen
// words it is read by.
var (
	_ = ConfigChange{ID: "", AgentID: "", Dimension: "", Generation: 0, Description: ""}

	_ func(AgentID, uint64, string, string, string) ConfigChange = ConfigChangeModelFor

	_ Fact = ConfigChange{}

	_ func(AgentID, string, uint64, string) ConfigChange = ConfigChangeFor

	_ func(ConfigChange, time.Time) Event = ConfigChange.Event

	_ func(ConfigChange) string = ConfigChange.FactKey
)

// The configuration change type, its source and dimension stamps and the
// metadata keys it carries are frozen names, not computed values.
const (
	_ = TypeConfigChange
	_ = ConfigChangeSourceConfig
	_ = ConfigChangeDimensionPrompt
	_ = ConfigChangeDimensionBinding
	_ = ConfigChangeDimensionModel
	_ = ConfigChangeDimensionFile
	_ = ConfigChangeDimensionOrgAgents
	_ = ConfigChangeDimensionOrgEntry
	_ = ConfigChangeDimensionOrgAssembly
	_ = MetaKeyConfigChangeDimension
	_ = MetaKeyConfigChangeGeneration
)

// The frozen type and metadata names must stay compile-time constants: a
// variable or a computed value stops this block from compiling.
const (
	_ = TypeExternalInput
	_ = TypeAgentOutput
	_ = TypeActionCommand
	_ = TypeThinkingPlan
	_ = TypeContextCompress
	_ = TypeTaskSettled
	_ = TypeFeedback
	_ = TypeGovernance

	_ = TypeAttentionQuestion
	_ = TypeAttentionAnswer
	_ = TypeAttentionClearance

	_ = MetaKeyEventKey
	_ = MetaKeyPartitionID
	_ = MetaKeyEventType
	_ = MetaKeyEventSummary
	_ = MetaKeyTriggerSource
	_ = MetaKeySettleTriggerSource
	_ = MetaKeyBundleID
	_ = MetaKeyNoticeKind
	_ = MetaKeyTaskID

	_ = MetaKeyAttentionKind
	_ = MetaKeyAttentionTarget
	_ = MetaKeyAttentionLineage
	_ = MetaKeyAttentionExpiresMs
	_ = MetaKeyAttentionClearance

	_ = MetaPrefix
)
