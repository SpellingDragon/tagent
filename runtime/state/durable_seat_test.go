// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
package state

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// statedAccountOf renders what a checkpoint would state for one task of a commit:
// the admission material, every fact the task committed and the settle account the
// durable record holds. It is the entry a boot reads instead of the task's prefixes.
func statedAccountOf(t *testing.T, commit *DurableCommit, taskID event.TaskID) task.StatedTask {
	t.Helper()
	receipts := commit.ReceiptsOf(taskID)
	commit.mu.Lock()
	spec, revision := commit.specs[taskID], commit.revisions[taskID]
	facts := append([]task.CommitRecord(nil), commit.facts[taskID]...)
	commit.mu.Unlock()
	return task.StatedTask{
		TaskID:    taskID,
		Spec:      spec,
		Status:    task.StatusRunning,
		Revision:  revision,
		Mutations: facts,
		Consumed:  receipts.Consumed,
		Attempts:  receipts.Attempts,
		Rejected:  receipts.Rejected,
	}
}

// statedPayloadOf renders one task account as the tasks slice payload states it: the
// three receipt families go through the producer the checkpoint uses, so an account
// that completed nothing is stated as the empty list and never as a missing key.
func statedPayloadOf(t *testing.T, ctx context.Context, account task.SettleAccount, entries ...task.StatedTask) []byte {
	t.Helper()
	body, err := json.Marshal(task.RegistryCheckpoint{Tasks: entries})
	if err != nil {
		t.Fatalf("state the task account: %v", err)
	}
	body, err = task.StateSettleAccount(ctx, body, account)
	if err != nil {
		t.Fatalf("state the settle account beside the tasks: %v", err)
	}
	return body
}

// TestDurableLoadReadsTheTaskSlice pins the read a boot pays for a task its checkpoint covers.
//   - positive: the seated account equals the loaded one in the tables a recovery reads — revisions, facts, owed notices, receipts — with no prefix read
//   - negative of the read: the same commit seated from no entry reads the five prefixes whole, so a zero read is the slice and never an empty log
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestDurableLoadReadsTheTaskSlice(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-seat-covered")
	seedCommit(t, ctx, log, taskID, "seat")
	loaded, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("load the commit from the whole log: %v", err)
	}
	entry := statedAccountOf(t, loaded, taskID)
	if len(entry.Mutations) == 0 || len(entry.Consumed)+len(entry.Attempts)+len(entry.Rejected) == 0 {
		t.Fatalf("the fixture states mutations=%d receipts=%d, want a task whose account carries both",
			len(entry.Mutations), len(entry.Consumed)+len(entry.Attempts)+len(entry.Rejected))
	}

	seated := NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(ctx, statedPayloadOf(t, ctx, loaded, entry), map[string]struct{}{}); err != nil {
		t.Fatalf("seat the task account from the checkpoint: %v", err)
	}
	for _, scan := range seated.LoadStats().Scans {
		t.Fatalf("the seat read the prefix %q of a task the checkpoint covers, want no durable prefix read", scan.Prefix)
	}
	if got, want := seated.Revision(taskID), loaded.Revision(taskID); got != want {
		t.Fatalf("the seated revision = %d, want the %d the whole log states", got, want)
	}
	if got, want := len(seated.Facts(taskID)), len(loaded.Facts(taskID)); got != want {
		t.Fatalf("the seated fact chain holds %d record(s), want the %d the whole log states", got, want)
	}
	if got, want := len(seated.Outbox(taskID)), len(loaded.Outbox(taskID)); got != want {
		t.Fatalf("the seated task owes %d notice(s), want the %d the whole log states", got, want)
	}
	seatedReceipts, loadedReceipts := seated.ReceiptsOf(taskID), loaded.ReceiptsOf(taskID)
	if len(seatedReceipts.Consumed) != len(loadedReceipts.Consumed) || len(seatedReceipts.Attempts) != len(loadedReceipts.Attempts) ||
		len(seatedReceipts.Rejected) != len(loadedReceipts.Rejected) {
		t.Fatalf("the seated task states %d receipt(s), %d attempt(s) and %d refusal(s), want the %d, %d and %d the whole log states",
			len(seatedReceipts.Consumed), len(seatedReceipts.Attempts), len(seatedReceipts.Rejected),
			len(loadedReceipts.Consumed), len(loadedReceipts.Attempts), len(loadedReceipts.Rejected))
	}

	whole := NewEmptyDurableCommit(log, nil)
	if err := whole.Seat(ctx, nil, nil); err != nil {
		t.Fatalf("seat the commit from the whole log: %v", err)
	}
	if scans := whole.LoadStats().Scans; len(scans) != 5 {
		t.Fatalf("the whole-log seat read %d prefix(es), want the five an account seated from no entry pays", len(scans))
	}
}

// TestDurableLoadRefusesAMismatchedReceiptIdentity pins the checkpoint a boot refuses to seat: a receipt stated under another task.
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestDurableLoadRefusesAMismatchedReceiptIdentity(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-seat-mismatched")
	other := event.TaskID("task-seat-other")
	if err := NewEmptyDurableCommit(log, nil).Declare(ctx, taskID, task.Spec{CallID: "call-seat"}); err != nil {
		t.Fatalf("declare the admission material: %v", err)
	}
	notice := invariantNotice(taskID, 1)
	entry := task.StatedTask{
		TaskID:   taskID,
		Status:   task.StatusRunning,
		Consumed: []task.StatedConsumption{{Notice: notice, Event: "fact-seat"}},
		Attempts: []task.StatedAttempt{},
		Rejected: []task.StatedRefusal{{Notice: inviteNoticeOf(other)}},
	}
	body, err := json.Marshal(task.RegistryCheckpoint{Tasks: []task.StatedTask{entry}})
	if err != nil {
		t.Fatalf("state the task account: %v", err)
	}
	err = NewEmptyDurableCommit(log, nil).Seat(ctx, body, map[string]struct{}{})
	if !errors.Is(err, ErrSeatRefused) {
		t.Fatalf("the seat of a refusal stated under another task = %v, want the mismatch named: a seat that dropped such a receipt would owe the notification it closed a second delivery", err)
	}
}

// inviteNoticeOf names one notification of one task, so a refusal the fixture states
// under another task than the entry it sits in carries a real four-part identity.
func inviteNoticeOf(taskID event.TaskID) task.Notice {
	return task.Notice{TaskID: taskID, Round: 1, Kind: task.NoticeProgress, Seq: 1}
}
