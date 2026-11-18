package task

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestRestartHandoffOfReadsASettledTaskFromItsChain pins the recorded handoff link against the account a restart keeps.
// - positive: HandoffOf answers the link a finished task committed before the cut, equal to the live registry's answer
// - a finished task no handoff moved answers ErrNoHandoff rather than an unknown task
// - negative: an identity no chain holds answers ErrTaskNotFound
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRestartHandoffOfReadsASettledTaskFromItsChain(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	manager := NewMemoryManager(source, nil)
	moved := invariantRegister(t, source, "call-handoff-restart", "dedup-handoff-restart")
	if result, err := source.Advance(ctx, moved, StatusRunning, "run-handoff-restart", nil); err != nil || result != CommitCommitted {
		t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
	}
	target := HandoffTarget{Target: event.Address{AgentID: "agent-b", SessionID: "session-b"}, TargetInstance: "instance-b"}
	if err := manager.Handoff(ctx, moved, target); err != nil {
		t.Fatalf("record the handoff before the cut: %v", err)
	}
	if result, err := source.Advance(ctx, moved, StatusSettled, "end-handoff-restart", nil); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}
	plain := restartSettled(t, source, "call-handoff-plain-restart", "dedup-handoff-plain-restart")
	want, err := manager.HandoffOf(ctx, moved)
	if err != nil {
		t.Fatalf("read the handoff of the live settled task: %v", err)
	}

	restored := restartAccount(t, source, commit, moved, plain)
	restartUnheld(t, restored, moved)
	restartUnheld(t, restored, plain)
	restoredManager := NewMemoryManager(restored, nil)
	got, err := restoredManager.HandoffOf(ctx, moved)
	if err != nil {
		t.Fatalf("the restart read the settled task as %v, want the handoff its chain holds", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the restart reported %+v, want the %+v the live registry gave", got, want)
	}
	if _, err := restoredManager.HandoffOf(ctx, plain); !errors.Is(err, ErrNoHandoff) {
		t.Fatalf("HandoffOf of a finished task no handoff moved = %v, want %v", err, ErrNoHandoff)
	}

	stranger := event.TaskID("task-no-chain-holds")
	if _, err := restoredManager.HandoffOf(ctx, stranger); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("HandoffOf of an identity no chain holds = %v, want %v", err, ErrTaskNotFound)
	}
}

// TestRestartIssueNoticeServesASettledTaskFromItsChain pins the notification identity against the account a restart keeps.
// - positive: IssueNotice answers a finished task the checkpoint never stated, continuing the sequence its chain carries
// - the envelope freezes the routing the declaration named, resolved from the record read back
// - negative: an identity no chain holds answers ErrTaskNotFound
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRestartIssueNoticeServesASettledTaskFromItsChain(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	taskID := invariantRegister(t, source, "call-notice-restart", "dedup-notice-restart")
	if result, err := source.Advance(ctx, taskID, StatusRunning, "run-notice-restart", nil); err != nil || result != CommitCommitted {
		t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
	}
	progress, err := source.IssueNotice(ctx, taskID, NoticeProgress, "fact-progress")
	if err != nil {
		t.Fatalf("issue the progress notification: %v", err)
	}
	if result, err := source.Advance(ctx, taskID, StatusRunning, "progress-notice-restart", []Envelope{progress}); err != nil || result != CommitCommitted {
		t.Fatalf("commit the progress notification = (%v, %v), want a committed change", result, err)
	}
	accepting := NewMemoryManager(source, func(ctx context.Context, notice Notice) (DeliveryAcceptance, error) {
		return DeliveryAccepted, nil
	})
	if err := accepting.Flush(ctx, taskID); err != nil {
		t.Fatalf("deliver the progress notification before the cut: %v", err)
	}
	if result, err := source.Advance(ctx, taskID, StatusSettled, "end-notice-restart", nil); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}

	restored := restartAccount(t, source, commit, taskID)
	restartUnheld(t, restored, taskID)
	envelope, err := restored.IssueNotice(ctx, taskID, NoticeProgress, "fact-after-restart")
	if err != nil {
		t.Fatalf("the restart read the settled task as %v, want the next identity its chain admits", err)
	}
	if envelope.Notice.Seq != progress.Notice.Seq+1 {
		t.Fatalf("the issued sequence = %d, want %d: the identity continues past the one the chain carries", envelope.Notice.Seq, progress.Notice.Seq+1)
	}
	if envelope.Notice.TaskID != taskID || envelope.Notice.Event != "fact-after-restart" {
		t.Fatalf("the issued notice = %+v, want the named task beside the fact it points at", envelope.Notice)
	}
	if envelope.Target != invariantTaskAddress || envelope.TargetInstance != "instance-a" {
		t.Fatalf("the envelope froze %s / %s, want the routing the declaration named", envelope.Target, envelope.TargetInstance)
	}

	stranger := event.TaskID("task-no-chain-holds")
	if _, err := restored.IssueNotice(ctx, stranger, NoticeProgress, "fact-stranger"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("IssueNotice of an identity no chain holds = %v, want %v", err, ErrTaskNotFound)
	}
}
