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

	_ = MetaKeyEventKey
	_ = MetaKeyPartitionID
	_ = MetaKeyEventType
	_ = MetaKeyEventSummary
	_ = MetaKeyTriggerSource
	_ = MetaKeySettleTriggerSource
	_ = MetaKeyBundleID

	_ = MetaPrefix
)
