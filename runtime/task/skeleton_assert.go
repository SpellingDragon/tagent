package task

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
//
// Compile-time assertions for the task ports and the records they carry: every
// declared method shape, every declared field name and type, the named refusals
// and the vocabulary constants that must stay compile-time constants. A renamed
// field or a widened signature stops this file from compiling.
//
// The declared task, commit and notice types and their fields.
var (
	_ = Spec{
		CallID:       "",
		DedupKey:     "",
		InvocationID: "",
		InstanceID:   "",
		Description:  "",
		Source:       event.Address{},
		Target:       event.Address{},
		Execution:    "",
	}

	_ DedupKey      = ""
	_ Serialization = ""

	_ = AdmissionReceipt{
		CallID:   "",
		TaskID:   "",
		Accepted: false,
		Kind:     AdmissionFresh,
	}

	_ = CommitRecord{
		MutationID:       "",
		TaskID:           "",
		ExpectedRevision: 0,
		Status:           StatusUnknown,
		Outbox:           []Envelope{},
	}

	_ = Envelope{
		Notice:         Notice{},
		Target:         event.Address{},
		Source:         event.Address{},
		TargetInstance: "",
	}

	_ = Notice{
		TaskID: "",
		Round:  0,
		Kind:   NoticeAccepted,
		Seq:    0,
		Event:  "",
	}

	_ = Hold{
		TaskID:         "",
		Reason:         "",
		ApprovalID:     "",
		ExpectedDigest: "",
	}

	_ = ApprovalAnswer{
		ApprovalID:     "",
		ExpectedDigest: "",
		Granted:        false,
		Reason:         "",
	}

	_ = Obligations{
		Count:      0,
		Owner:      "",
		Blocked:    []BlockedObligation{},
		ExitReason: "",
		Logical:    LogicalActive,
	}

	_ = BlockedObligation{
		Notice: Notice{},
		Reason: "",
	}

	_ = HandoffTarget{
		Target:         event.Address{},
		TargetInstance: "",
	}

	_ = LogicalChange{
		TaskID: "",
		Status: StatusUnknown,
		From:   LogicalActive,
		To:     LogicalActive,
		Since:  time.Time{},
	}

	_ = Policy{
		WatchAfter:       0,
		StableAfter:      0,
		SuspectAfter:     0,
		ExecutionTimeout: 0,
		TTL:              0,
	}
)

// The status and commit-result vocabularies stay compile-time constants.
const (
	_ = StatusUnknown
	_ = StatusAccepted
	_ = StatusRunning
	_ = StatusSettled
	_ = StatusFailed
	_ = StatusCancelled
	_ = StatusAwaitingApproval

	_ = CommitUncertain
	_ = CommitCommitted
	_ = CommitNotCommitted

	_ = NoticeAccepted
	_ = NoticeProgress
	_ = NoticeSettled
	_ = NoticeFailed
	_ = NoticeCancelled

	_ = AdmissionFresh
	_ = AdmissionDedup
	_ = AdmissionRejected

	_ = LogicalActive
	_ = LogicalWatch
	_ = LogicalStable
	_ = LogicalSuspect
	_ = LogicalTimedOut
	_ = LogicalExpired

	_ = PolicyDefault
	_ = PolicyResponsive
	_ = PolicyPatient
)

// The port method signatures are pinned exactly on their implementations.
var (
	_ func(context.Context, CommitRecord) (CommitResult, error)                  = (*MemoryCommit)(nil).Commit
	_ func(context.Context, Spec) (AdmissionReceipt, error)                      = (*MemoryRegistry)(nil).Register
	_ func(context.Context, event.TaskID) (Spec, Status, error)                  = (*MemoryRegistry)(nil).Lookup
	_ func(context.Context, event.TaskID, time.Time) (LogicalStatus, error)      = (*MemoryRegistry)(nil).Logical
	_ func(context.Context, time.Time) []LogicalChange                           = (*MemoryRegistry)(nil).Watch
	_ func(Policy) RegistryOption                                                = WithPolicy
	_ func(string) (Policy, bool)                                                = PolicyNamed
	_ func(context.Context, event.TaskID) error                                  = (*MemoryRegistry)(nil).Cancel
	_ func(context.Context, event.TaskID, event.Event) (AdmissionReceipt, error) = (*MemoryRegistry)(nil).Resume
	_ func(context.Context, Spec) (AdmissionReceipt, error)                      = (*MemoryManager)(nil).Admit
	_ func(context.Context, event.TaskID) (Obligations, error)                   = (*MemoryManager)(nil).Obligations
	_ func(context.Context, event.TaskID, HandoffTarget) error                   = (*MemoryManager)(nil).Handoff
	_ func(context.Context, event.TaskID) error                                  = (*MemoryManager)(nil).Flush
)

// The progress read keeps its documented shape: it is a derived read of the
// committed chain, pinned beside the status machine rather than as one of its
// values, so a reader asks the chain instead of a status the machine keeps.
var _ func(*MemoryRegistry, context.Context, event.TaskID) (bool, error) = (*MemoryRegistry).ProgressReported

// The durable restore face keeps its documented shapes: the self-contained
// restore link beside its codec, the owner-binding port and the named
// refusals of a restore.
var (
	_ = TaskRecord{
		Spec: Spec{},
		Fact: CommitRecord{},
	}

	_ = RestoreOutcome{
		TaskID:    "",
		Status:    StatusUnknown,
		Owner:     "",
		Recovered: false,
		Refused:   nil,
	}

	_ = RestoreReport{
		Tasks: []RestoreOutcome{},
	}

	_ error = ErrTargetRemoved
	_ error = ErrNoOwnerBinding
	_ error = ErrChainBroken

	_ OwnerBinding = (OwnerBinding)(nil)

	_ func(OwnerBinding, context.Context, event.AgentID, event.AgentID) (event.AgentID, error) = OwnerBinding.OwnerOf

	_ func(TaskRecord) ([]byte, error) = EncodeTaskRecord
	_ func([]byte) (TaskRecord, error) = DecodeTaskRecord

	_ func(*MemoryRegistry, context.Context, []TaskRecord, OwnerBinding) (RestoreReport, error) = (*MemoryRegistry).Restore
)

// The handoff, retry and instance-close faces keep their documented shapes: the
// recorded handoff link, the stable notice sequencer, the frozen-envelope
// deliverer beside its manager option, and the named refusals and retained
// material of a closed instance.
var (
	_ = HandoffRecord{
		TaskID:         "",
		PreviousTarget: event.Address{},
		SourceInstance: "",
		Target:         event.Address{},
		TargetInstance: "",
	}

	_ EnvelopeDeliverer = (EnvelopeDeliverer)(nil)
	_ ManagerOption     = (ManagerOption)(nil)

	_ error = ErrNoHandoff
	_ error = ErrInstanceClosed

	_ func(*NoticeSequencer, event.TaskID, int64, NoticeKind, event.EventID) Notice = (*NoticeSequencer).Issue
	_ func(*NoticeSequencer, Notice)                                                = (*NoticeSequencer).Observe

	_ func(*MemoryRegistry, context.Context, event.TaskID, NoticeKind, event.EventID) (Envelope, error) = (*MemoryRegistry).IssueNotice

	_ func(*MemoryManager, context.Context, event.TaskID) (HandoffRecord, error)    = (*MemoryManager).HandoffOf
	_ func(*MemoryManager, context.Context, event.InstanceID) error                 = (*MemoryManager).CloseInstance
	_ func(*MemoryManager, context.Context, event.InstanceID) ([]Envelope, error)   = (*MemoryManager).Retained
	_ func(*MemoryManager, context.Context, event.InstanceID) ([]TaskRecord, error) = (*MemoryManager).RecoveryMaterial

	_ func(EnvelopeDeliverer) ManagerOption = WithEnvelopeDeliverer
)
