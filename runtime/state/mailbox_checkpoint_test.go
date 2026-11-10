package state

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState pins the whole owed set one boot restores from the mailbox slice alone.
// - positive: an obligation the cut stated comes back from the payload
// - positive: an obligation committed past the cut comes back from the account behind the mailbox
// - parity: the restored seats are the account's owed set, neither short nor doubled
// - negative: dropping the account from the restore leaves the past-cut obligation unseated
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState(t *testing.T) {
	ctx := context.Background()
	taskID := eventTaskID()
	account, err := NewDurableCommit(newInvariantFactLog(), nil)
	if err != nil {
		t.Fatalf("open the durable account: %v", err)
	}
	if _, err := account.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))); err != nil {
		t.Fatalf("commit the obligation the cut covers: %v", err)
	}
	cut := NewMemoryStore(NewAuthority(), account)
	payload, err := NewMailboxState(cut).SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the mailbox slice at the cut: %v", err)
	}
	if _, err := account.Commit(ctx, invariantRecord(taskID, 1, task.StatusRunning, invariantNotice(taskID, 2))); err != nil {
		t.Fatalf("commit the obligation past the cut: %v", err)
	}

	booted := NewMemoryStore(NewAuthority(), account)
	if err := NewMailboxState(booted).RestoreState(ctx, payload); err != nil {
		t.Fatalf("restore the mailbox slice: %v", err)
	}

	seats := booted.Unconsumed()
	if len(seats) != 2 {
		t.Fatalf("restored seats = %+v, want the obligation the cut stated beside the one committed past it: dropping the account from the restore leaves the past-cut obligation unseated", seats)
	}
	stated, past := noticeSeqOf(seats, 1), noticeSeqOf(seats, 2)
	if !stated {
		t.Fatalf("restored seats = %+v, want the obligation the cut stated back from the payload", seats)
	}
	if !past {
		t.Fatalf("restored seats = %+v, want the obligation committed past the cut back from the account", seats)
	}
	owed := account.OwedNotices()
	if len(owed) != len(seats) {
		t.Fatalf("restored seats = %d beside the %d the account owes, want the same set", len(seats), len(owed))
	}
	for i := range owed {
		if seats[i].Notice != owed[i].Notice.Notice {
			t.Fatalf("restored seat %d = %+v, want the accounted obligation %+v", i, seats[i].Notice, owed[i].Notice.Notice)
		}
	}
}

// TestMailboxSliceRestoreSeatsThePayloadBesideNoAccount pins the stated payload as a restore source of its own.
// - positive: a mailbox opened beside no durable account restores the obligations the cut stated from the payload alone
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestMailboxSliceRestoreSeatsThePayloadBesideNoAccount(t *testing.T) {
	ctx := context.Background()
	taskID := eventTaskID()
	account, err := NewDurableCommit(newInvariantFactLog(), nil)
	if err != nil {
		t.Fatalf("open the durable account: %v", err)
	}
	if _, err := account.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))); err != nil {
		t.Fatalf("commit the stated obligation: %v", err)
	}
	payload, err := NewMailboxState(NewMemoryStore(NewAuthority(), account)).SnapshotState(ctx)
	if err != nil {
		t.Fatalf("state the mailbox slice: %v", err)
	}

	beside := NewMemoryStore(NewAuthority())
	if err := NewMailboxState(beside).RestoreState(ctx, payload); err != nil {
		t.Fatalf("restore the mailbox slice beside no account: %v", err)
	}
	seats := beside.Unconsumed()
	if len(seats) != 1 || seats[0].Notice.Seq != 1 {
		t.Fatalf("restored seats beside no account = %+v, want the one obligation the payload stated", seats)
	}
}

// TestMailboxSliceRestoresTheOwedSetBesideACutThatStatesNothing pins the restore of a boot whose tier holds no carrier.
//   - positive: the framework restore of the empty cut seats every obligation the account owes
//   - parity: the seated set is the account's owed set, neither short nor doubled
//   - negative: the mailbox a boot mounts owes nothing until the framework restores it, so skipping the restore leaves the work unseated
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestMailboxSliceRestoresTheOwedSetBesideACutThatStatesNothing(t *testing.T) {
	ctx := context.Background()
	taskID := eventTaskID()
	account, err := NewDurableCommit(newInvariantFactLog(), nil)
	if err != nil {
		t.Fatalf("open the durable account: %v", err)
	}
	if _, err := account.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))); err != nil {
		t.Fatalf("commit the owed obligation: %v", err)
	}

	booted := NewMemoryStore(NewAuthority(), account)
	states := NewStateRegistry()
	if _, err := states.Register(NewMailboxState(booted)); err != nil {
		t.Fatalf("mount the mailbox state slice: %v", err)
	}
	if seats := booted.Unconsumed(); len(seats) != 0 {
		t.Fatalf("the mailbox holds %+v before the framework restored it, want the owed set to arrive through the restore", seats)
	}
	if err := states.RestoreEmptyState(ctx); err != nil {
		t.Fatalf("restore the state slices beside a cut that states nothing: %v", err)
	}

	seats := booted.Unconsumed()
	owed := account.OwedNotices()
	if len(seats) != len(owed) || len(seats) != 1 {
		t.Fatalf("restored seats = %+v beside the %d the account owes, want the framework restore to seat the owed set: skipping it leaves the work unseated", seats, len(owed))
	}
	if seats[0].Notice != owed[0].Notice.Notice {
		t.Fatalf("restored seat = %+v, want the accounted obligation %+v", seats[0].Notice, owed[0].Notice.Notice)
	}
}

// noticeSeqOf reports whether the restored seats carry the notification of one
// sequence number.
func noticeSeqOf(seats []WorkSeat, seq int64) bool {
	for _, seat := range seats {
		if seat.Notice.Seq == seq {
			return true
		}
	}
	return false
}

// eventTaskID is the task identity the mailbox slice checks state their
// obligations under.
func eventTaskID() event.TaskID { return event.TaskID("task-mailbox-slice") }
