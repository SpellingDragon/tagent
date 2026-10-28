package task

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestProgressReadsFromTheCommittedChain computes progress back from the chain alone, with no status stored for it.
// - a running task that reported no progress reads as no progress
// - a chain carrying a progress notification reads as progress after a restore, and its status is running
// - a terminal task reads as no progress beside the progress its chain carries
// - a chain written while progress was a status replays as running and still reads as progress
// - the stored ordinal of every other status stayed where it was
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestProgressReadsFromTheCommittedChain(t *testing.T) {
	ctx := context.Background()

	t.Run("a running task that reported no progress reads as no progress", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); result != CommitCommitted || err != nil {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		reported, err := registry.ProgressReported(ctx, taskID)
		if err != nil || reported {
			t.Fatalf("progress of a task that reported none = (%v, %v), want no progress", reported, err)
		}
		_, status, err := registry.Lookup(ctx, taskID)
		if err != nil || status != StatusRunning {
			t.Fatalf("status = (%v, %v), want the running phase with no progress status beside it", status, err)
		}
	})

	t.Run("a chain carrying a progress notification reads as progress after a restore", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-2", "dedup-2")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); result != CommitCommitted || err != nil {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		envelope, err := registry.IssueNotice(ctx, taskID, NoticeProgress, "fact-progress")
		if err != nil {
			t.Fatalf("issue the progress notification: %v", err)
		}
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "progress", []Envelope{envelope}); result != CommitCommitted || err != nil {
			t.Fatalf("commit the progress notification = (%v, %v), want the fact stored beside its running phase", result, err)
		}
		if reported, err := registry.ProgressReported(ctx, taskID); err != nil || !reported {
			t.Fatalf("progress of the live task = (%v, %v), want the progress its chain carries", reported, err)
		}

		chain, err := manager.RecoveryMaterial(ctx, "instance-a")
		if err != nil {
			t.Fatalf("read the recovery material of the instance: %v", err)
		}
		if len(chain) == 0 {
			t.Fatal("the recovery material carries no chain for a task the instance still owes")
		}
		restored, _ := invariantRegistry(t)
		report, err := restored.Restore(ctx, chain, invariantOwners{})
		if err != nil {
			t.Fatalf("fold the chain back: %v", err)
		}
		if len(report.Tasks) != 1 || !report.Tasks[0].Recovered || report.Tasks[0].Status != StatusRunning {
			t.Fatalf("restore report = %+v, want the one task rebuilt at its running phase", report)
		}
		reported, err := restored.ProgressReported(ctx, taskID)
		if err != nil || !reported {
			t.Fatalf("progress of the restored task = (%v, %v), want the same answer the live task gave", reported, err)
		}
	})

	t.Run("a terminal task reads as no progress beside the progress its chain carries", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-3", "dedup-3")
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", nil); result != CommitCommitted || err != nil {
			t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
		}
		progress, err := registry.IssueNotice(ctx, taskID, NoticeProgress, "fact-progress")
		if err != nil {
			t.Fatalf("issue the progress notification: %v", err)
		}
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "progress", []Envelope{progress}); result != CommitCommitted || err != nil {
			t.Fatalf("commit the progress notification = (%v, %v), want the fact stored", result, err)
		}
		settled, err := registry.IssueNotice(ctx, taskID, NoticeSettled, "fact-settled")
		if err != nil {
			t.Fatalf("issue the settled notification: %v", err)
		}
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settle", []Envelope{settled}); result != CommitCommitted || err != nil {
			t.Fatalf("settle the task = (%v, %v), want the terminal fact stored", result, err)
		}
		reported, err := registry.ProgressReported(ctx, taskID)
		if err != nil || reported {
			t.Fatalf("progress of a settled task = (%v, %v), want no progress beside a closed round", reported, err)
		}
	})

	t.Run("a chain written while progress was a status replays as running and still reads as progress", func(t *testing.T) {
		spec := invariantTaskSpec("call-legacy", "dedup-legacy")
		taskID := event.TaskID("task-legacy")
		notice := Notice{TaskID: taskID, Round: 1, Kind: NoticeProgress, Seq: 0, Event: "fact-legacy"}
		chain := []TaskRecord{
			{
				Spec: spec,
				Fact: CommitRecord{MutationID: MutationID("admit:" + string(taskID)), TaskID: taskID, Status: StatusAccepted},
			},
			{
				Fact: CommitRecord{
					MutationID:       MutationID("progress:" + string(taskID)),
					TaskID:           taskID,
					ExpectedRevision: 1,
					Status:           statusProgressRetired,
					Outbox: []Envelope{{
						Notice:         notice,
						Target:         spec.Target,
						Source:         spec.Source,
						TargetInstance: spec.InstanceID,
					}},
				},
			},
		}
		registry, _ := invariantRegistry(t)
		report, err := registry.Restore(ctx, chain, invariantOwners{})
		if err != nil {
			t.Fatalf("fold a chain written while progress was a status: %v", err)
		}
		if len(report.Tasks) != 1 || !report.Tasks[0].Recovered {
			t.Fatalf("restore report = %+v, want the task rebuilt from the chain", report)
		}
		if _, status, err := registry.Lookup(ctx, taskID); err != nil || status != StatusRunning {
			t.Fatalf("replayed status = (%v, %v), want the running phase the retired ordinal described", status, err)
		}
		if reported, err := registry.ProgressReported(ctx, taskID); err != nil || !reported {
			t.Fatalf("progress of the replayed task = (%v, %v), want the progress notification the chain still carries", reported, err)
		}
	})

	t.Run("the stored ordinal of every other status stayed where it was", func(t *testing.T) {
		want := []struct {
			name   string
			status Status
			stored int
		}{
			{"unknown", StatusUnknown, 0},
			{"accepted", StatusAccepted, 1},
			{"running", StatusRunning, 2},
			{"settled", StatusSettled, 4},
			{"failed", StatusFailed, 5},
			{"cancelled", StatusCancelled, 6},
			{"awaiting approval", StatusAwaitingApproval, 7},
		}
		for _, status := range want {
			if int(status.status) != status.stored {
				t.Fatalf("%s stores as %d, want %d: a status is stored as its ordinal, so closing the retired gap would replay every later status one place off",
					status.name, int(status.status), status.stored)
			}
		}
		if got := replayedStatus(statusProgressRetired); got != StatusRunning {
			t.Fatalf("the retired ordinal replays as %v, want the running phase", got)
		}
		for _, status := range want {
			if got := replayedStatus(status.status); got != status.status {
				t.Fatalf("%s replays as %v, want it unchanged", status.name, got)
			}
		}
	})
}
