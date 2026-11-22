package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// invariantAddress is the explicit address every record and delivery of these invariants names.
var invariantAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// loadedDurableCommit answers the account of one fact log read whole, through the
// entry a boot that holds no checkpoint seats with: a seat beside no payload falls
// back to the whole log, so this is the read a caller without a cut pays.
func loadedDurableCommit(ctx context.Context, log durable.FactLog, uncertain task.UncertainProbe) (*DurableCommit, error) {
	d := NewEmptyDurableCommit(log, uncertain)
	if err := d.Seat(ctx, nil, nil); err != nil {
		return nil, err
	}
	return d, nil
}

// TestDurableFactChainInvariants pins the fact chain's idempotency and the single advance of a delivery.
// - a repeated mutation identity stores one fact and leaves every delivery where it was
// - one mutation identity beside other content, and a record expecting another revision, are refused by name
// - a commit the write barrier left unconfirmed reports uncertain and keeps no fact
// - a second commit over the same log revives the facts and every delivery as pending again
// - the admission material is declared once and never replaced by other content
// - an acceptance of a notification the commit owes nothing for is refused by name
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDurableFactChainInvariants(t *testing.T) {
	ctx := context.Background()
	taskID := event.TaskID("task-1")

	t.Run("a repeated mutation identity stores one fact and leaves every delivery where it was", func(t *testing.T) {
		commit, err := loadedDurableCommit(ctx, newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable commit: %v", err)
		}
		record := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
		if result, err := commit.Commit(ctx, record); err != nil || result != task.CommitCommitted {
			t.Fatalf("commit the first fact = (%v, %v), want a committed record", result, err)
		}
		if result, err := commit.Commit(ctx, record); err != nil || result != task.CommitCommitted {
			t.Fatalf("repeat the mutation = (%v, %v), want the stored fact reported again", result, err)
		}
		if got := len(commit.Facts(taskID)); got != 1 {
			t.Fatalf("facts after a repeated mutation = %d, want one", got)
		}
		if got := commit.Revision(taskID); got != 1 {
			t.Fatalf("revision after a repeated mutation = %d, want one", got)
		}
		owed := commit.Outbox(taskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("outbox after a repeated mutation = %+v, want one pending notification", owed)
		}
		accepted, err := commit.Accept(ctx, record.Outbox[0].Notice)
		if err != nil || accepted != task.DeliveryAccepted {
			t.Fatalf("accept the delivered notification = (%v, %v), want it accepted", accepted, err)
		}
		if _, err := commit.Commit(ctx, record); err != nil {
			t.Fatalf("repeat the mutation beside an accepted delivery: %v", err)
		}
		owed = commit.Outbox(taskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryAccepted {
			t.Fatalf("outbox after a repeated mutation = %+v, want the acceptance kept", owed)
		}
	})

	t.Run("one mutation identity beside other content is refused by name", func(t *testing.T) {
		commit, err := loadedDurableCommit(ctx, newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable commit: %v", err)
		}
		record := invariantRecord(taskID, 0, task.StatusRunning)
		if _, err := commit.Commit(ctx, record); err != nil {
			t.Fatalf("commit the first fact: %v", err)
		}
		conflict := record
		conflict.Status = task.StatusSettled
		if _, err := commit.Commit(ctx, conflict); !errors.Is(err, task.ErrMutationConflict) {
			t.Fatalf("one identity beside other content = %v, want %v", err, task.ErrMutationConflict)
		}
		if got := len(commit.Facts(taskID)); got != 1 {
			t.Fatalf("facts after the refused mutation = %d, want the stored one alone", got)
		}
		if got := commit.Revision(taskID); got != 1 {
			t.Fatalf("revision after the refused mutation = %d, want it unmoved", got)
		}
	})

	t.Run("a record expecting another revision is refused by name", func(t *testing.T) {
		commit, err := loadedDurableCommit(ctx, newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable commit: %v", err)
		}
		if _, err := commit.Commit(ctx, invariantRecord(taskID, 5, task.StatusRunning)); !errors.Is(err, task.ErrRevisionConflict) {
			t.Fatalf("a record expecting revision 5 = %v, want %v", err, task.ErrRevisionConflict)
		}
		if got := len(commit.Facts(taskID)); got != 0 {
			t.Fatalf("facts after the refused revision = %d, want none", got)
		}
	})

	t.Run("a commit the write barrier left unconfirmed reports uncertain and keeps no fact", func(t *testing.T) {
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the durable commit: %v", err)
		}
		log.refuseSync(true)
		record := invariantRecord(taskID, 0, task.StatusRunning)
		result, err := commit.Commit(ctx, record)
		if err == nil || result != task.CommitUncertain {
			t.Fatalf("commit behind a failed barrier = (%v, %v), want an uncertain outcome", result, err)
		}
		if got := len(commit.Facts(taskID)); got != 0 {
			t.Fatalf("facts behind a failed barrier = %d, want none counted as stored", got)
		}
		if got := commit.Revision(taskID); got != 0 {
			t.Fatalf("revision behind a failed barrier = %d, want none", got)
		}
		log.refuseSync(false)
		if result, err := commit.Commit(ctx, record); err != nil || result != task.CommitCommitted {
			t.Fatalf("reconciled retry = (%v, %v), want the record stored once", result, err)
		}
		if got := len(commit.Facts(taskID)); got != 1 {
			t.Fatalf("facts after the reconciled retry = %d, want one", got)
		}
	})

	t.Run("a probed attempt is uncertain and is never counted as stored", func(t *testing.T) {
		probe := func(task.CommitRecord) bool { return true }
		commit, err := loadedDurableCommit(ctx, newInvariantFactLog(), probe)
		if err != nil {
			t.Fatalf("open the durable commit: %v", err)
		}
		if result, err := commit.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning)); err != nil || result != task.CommitUncertain {
			t.Fatalf("probed commit = (%v, %v), want an uncertain outcome", result, err)
		}
		if got := len(commit.Facts(taskID)); got != 0 {
			t.Fatalf("facts after a probed commit = %d, want none", got)
		}
	})

	t.Run("a second commit over the same log revives the facts and every delivery as pending again", func(t *testing.T) {
		log := newInvariantFactLog()
		first, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the first durable commit: %v", err)
		}
		opened := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
		if _, err := first.Commit(ctx, opened); err != nil {
			t.Fatalf("commit the opening fact: %v", err)
		}
		settled := invariantRecord(taskID, 1, task.StatusSettled, invariantNotice(taskID, 2))
		if _, err := first.Commit(ctx, settled); err != nil {
			t.Fatalf("commit the settling fact: %v", err)
		}
		if _, err := first.Accept(ctx, opened.Outbox[0].Notice); err != nil {
			t.Fatalf("accept the delivered notification: %v", err)
		}
		spec := invariantSpec()
		if err := first.Declare(ctx, taskID, spec); err != nil {
			t.Fatalf("declare the admission material: %v", err)
		}
		second, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("reopen the durable commit over the same log: %v", err)
		}
		if got := len(second.Facts(taskID)); got != 2 {
			t.Fatalf("facts revived from the log = %d, want both", got)
		}
		if got := second.Revision(taskID); got != 2 {
			t.Fatalf("revision revived from the log = %d, want both records behind it", got)
		}
		owed := second.Outbox(taskID)
		if len(owed) != 2 {
			t.Fatalf("obligations revived from the log = %d, want both notifications owed", len(owed))
		}
		for _, notice := range owed {
			if notice.Delivery != task.DeliveryPending {
				t.Fatalf("revived delivery = %v, want every notification pending again", notice.Delivery)
			}
		}
		chain, err := second.Records(ctx, taskID)
		if err != nil {
			t.Fatalf("read the revived chain: %v", err)
		}
		if len(chain) != 2 {
			t.Fatalf("links revived from the log = %d, want both facts", len(chain))
		}
		if chain[0].Spec != spec {
			t.Fatalf("admission link material = %+v, want the declared one", chain[0].Spec)
		}
		if chain[1].Spec != (task.Spec{}) {
			t.Fatalf("later link material = %+v, want the admission link alone to carry it", chain[1].Spec)
		}
	})

	t.Run("the admission material is declared once and never replaced by other content", func(t *testing.T) {
		commit, err := loadedDurableCommit(ctx, newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable commit: %v", err)
		}
		spec := invariantSpec()
		if err := commit.Declare(ctx, taskID, spec); err != nil {
			t.Fatalf("declare the admission material: %v", err)
		}
		if err := commit.Declare(ctx, taskID, spec); err != nil {
			t.Fatalf("repeat the declaration: %v", err)
		}
		other := spec
		other.Description = "another admission material"
		if err := commit.Declare(ctx, taskID, other); !errors.Is(err, ErrSpecConflict) {
			t.Fatalf("declaration beside other content = %v, want %v", err, ErrSpecConflict)
		}
	})

	t.Run("an acceptance of a notification the commit owes nothing for is refused by name", func(t *testing.T) {
		commit, err := loadedDurableCommit(ctx, newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable commit: %v", err)
		}
		if _, err := commit.Accept(ctx, invariantNotice("task-unknown", 1)); !errors.Is(err, ErrNoticeNotOwed) {
			t.Fatalf("acceptance beside no obligation = %v, want %v", err, ErrNoticeNotOwed)
		}
	})
}

// TestMailboxSnapshotInvariants pins the mailbox's copy-out read beside the three time points it keeps apart.
// - a read hands out a copy, so a reader's edit never reaches the site it read
// - an instance another instance replaced keeps refusing input while its accepted entries stay readable
// - the consumption position stops before the first unfinished entry and absorbs nothing behind it
// - claim, settlement and acceptance each move only from their own predecessor state
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestMailboxSnapshotInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("a read hands out a copy instead of a reference into the site", func(t *testing.T) {
		store := invariantStore(t)
		if err := store.AcceptNotice(ctx, invariantNotice("task-1", 1)); err != nil {
			t.Fatalf("accept the delivered notification: %v", err)
		}
		read, err := store.Site(ctx, invariantReader, invariantAddress, "instance-a")
		if err != nil {
			t.Fatalf("read the site: %v", err)
		}
		if len(read.Mailbox) != 1 || read.Mailbox[0].AcceptSeq != 1 {
			t.Fatalf("mailbox read = %+v, want the accepted entry at sequence 1", read.Mailbox)
		}
		read.Mailbox[0].Claim = ClaimConsumed
		read.Mailbox = append(read.Mailbox, MailboxEntry{AcceptSeq: 99})
		read.Consumed = 42
		again, err := store.Site(ctx, invariantReader, invariantAddress, "instance-a")
		if err != nil {
			t.Fatalf("read the site again: %v", err)
		}
		if len(again.Mailbox) != 1 {
			t.Fatalf("mailbox after a reader's edit = %d entries, want the site's own log", len(again.Mailbox))
		}
		if got := again.Mailbox[0].Claim; got != ClaimPending {
			t.Fatalf("claim after a reader's edit = %v, want the recorded one", got)
		}
		if got := again.Consumed; got != 0 {
			t.Fatalf("consumption position after a reader's edit = %d, want the site's own", got)
		}
	})

	t.Run("an instance another instance replaced keeps refusing input while its accepted entries stay readable", func(t *testing.T) {
		store := invariantStore(t)
		if err := store.AcceptNotice(ctx, invariantNotice("task-1", 1)); err != nil {
			t.Fatalf("accept the delivered notification: %v", err)
		}
		if _, err := store.Establish(ctx, invariantAddress, "instance-b"); err != nil {
			t.Fatalf("establish the address on the replacing instance: %v", err)
		}
		if _, err := store.Establish(ctx, invariantAddress, "instance-a"); !errors.Is(err, ErrInstanceRetired) {
			t.Fatalf("re-establish a replaced instance = %v, want %v", err, ErrInstanceRetired)
		}
		if err := store.AcceptNotice(ctx, invariantNotice("task-1", 2)); !errors.Is(err, ErrInstanceRetired) {
			t.Fatalf("input aimed at a replaced instance = %v, want %v", err, ErrInstanceRetired)
		}
		retired, err := store.Site(ctx, invariantReader, invariantAddress, "instance-a")
		if err != nil {
			t.Fatalf("read the replaced instance: %v", err)
		}
		if len(retired.Mailbox) != 1 {
			t.Fatalf("accepted entries of the replaced instance = %d, want them readable", len(retired.Mailbox))
		}
	})

	t.Run("the consumption position stops before the first unfinished entry", func(t *testing.T) {
		store := invariantStore(t)
		for seq := 1; seq <= 3; seq++ {
			if err := store.AcceptNotice(ctx, invariantNotice("task-1", int64(seq))); err != nil {
				t.Fatalf("accept notification %d: %v", seq, err)
			}
			if err := store.Claim(ctx, invariantAddress, "instance-a", AcceptSeq(seq)); err != nil {
				t.Fatalf("claim entry %d: %v", seq, err)
			}
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", 2); err != nil {
			t.Fatalf("consume entry 2: %v", err)
		}
		read := invariantRead(t, store)
		if got := read.Consumed; got != 0 {
			t.Fatalf("consumption position beside an unfinished entry = %d, want it before the unfinished one", got)
		}
		if got := read.Mailbox[1].Claim; got != ClaimConsumed {
			t.Fatalf("the finished entry's claim = %v, want its own completion recorded", got)
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", 1); err != nil {
			t.Fatalf("consume entry 1: %v", err)
		}
		read = invariantRead(t, store)
		if got := read.Consumed; got != 2 {
			t.Fatalf("consumption position after the hole closed = %d, want the contiguous prefix", got)
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", 3); err != nil {
			t.Fatalf("consume entry 3: %v", err)
		}
		if got := invariantRead(t, store).Consumed; got != 3 {
			t.Fatalf("consumption position after the last entry = %d, want the whole prefix", got)
		}
	})

	t.Run("claim, settlement and acceptance each move only from their own predecessor state", func(t *testing.T) {
		store := invariantStore(t)
		notice := invariantNotice("task-1", 1)
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("accept the delivered notification: %v", err)
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", 1); !errors.Is(err, ErrNotClaimed) {
			t.Fatalf("consume beside no claim = %v, want %v", err, ErrNotClaimed)
		}
		if err := store.Reclaim(ctx, invariantAddress, "instance-a", 1); !errors.Is(err, ErrNotClaimed) {
			t.Fatalf("reclaim beside no claim = %v, want %v", err, ErrNotClaimed)
		}
		if err := store.Claim(ctx, invariantAddress, "instance-a", 1); err != nil {
			t.Fatalf("claim the entry: %v", err)
		}
		if err := store.Claim(ctx, invariantAddress, "instance-a", 1); !errors.Is(err, ErrClaimConflict) {
			t.Fatalf("second claim of an entry = %v, want %v", err, ErrClaimConflict)
		}
		if err := store.Reclaim(ctx, invariantAddress, "instance-a", 1); err != nil {
			t.Fatalf("reclaim an unfinished claim: %v", err)
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", 1); err != nil {
			t.Fatalf("consume the claimed entry: %v", err)
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", 1); !errors.Is(err, ErrClaimSettled) {
			t.Fatalf("repeat the consumption = %v, want %v beside a settled entry refusing the repeat by name", err, ErrClaimSettled)
		}
		if err := store.Reclaim(ctx, invariantAddress, "instance-a", 1); !errors.Is(err, ErrClaimSettled) {
			t.Fatalf("reclaim a settled entry = %v, want %v", err, ErrClaimSettled)
		}
		if err := store.Claim(ctx, invariantAddress, "instance-a", 9); !errors.Is(err, ErrEntryUnknown) {
			t.Fatalf("claim a sequence the mailbox never assigned = %v, want %v", err, ErrEntryUnknown)
		}
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("replay the accepted notification: %v", err)
		}
		if got := len(invariantRead(t, store).Mailbox); got != 1 {
			t.Fatalf("entries after a replayed acceptance = %d, want the first entry alone", got)
		}
		other := notice
		other.Event = "another-committed-fact"
		if err := store.AcceptNotice(ctx, other); !errors.Is(err, ErrNoticeConflict) {
			t.Fatalf("one notification identity beside other content = %v, want %v", err, ErrNoticeConflict)
		}
	})
}

// TestMovementCoversTheMailbox pins the mailbox account's own sequence beside the store it writes.
// - an accepted entry, a completed consumption and the refusal a closed target answers each advance it
// - a judgment reads that account's own sequence instead of a mechanism kept beside it
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestMovementCoversTheMailbox(t *testing.T) {
	ctx := context.Background()

	t.Run("an accepted entry advances the sequence", func(t *testing.T) {
		store := invariantStore(t)
		at := store.Load()
		if err := store.AcceptNotice(ctx, invariantNotice("task-1", 1)); err != nil {
			t.Fatalf("accept the delivered notification: %v", err)
		}
		if got := store.Load(); got == at {
			t.Fatalf("the sequence stood at %d after an accepted entry, want the write of the mailbox to advance it", got)
		}
	})

	t.Run("a completed consumption advances the sequence", func(t *testing.T) {
		store := invariantStore(t)
		if err := store.AcceptNotice(ctx, invariantNotice("task-1", 1)); err != nil {
			t.Fatalf("accept the delivered notification: %v", err)
		}
		if err := store.Claim(ctx, invariantAddress, "instance-a", 1); err != nil {
			t.Fatalf("claim the accepted entry: %v", err)
		}
		at := store.Load()
		if err := store.Consume(ctx, invariantAddress, "instance-a", 1); err != nil {
			t.Fatalf("consume the claimed entry: %v", err)
		}
		if got := store.Load(); got == at {
			t.Fatalf("the sequence stood at %d after a completed consumption, want the write of the mailbox to advance it", got)
		}
	})

	t.Run("a refused delivery advances the sequence", func(t *testing.T) {
		store := invariantStore(t)
		if err := store.CloseSession(ctx, invariantAddress); err != nil {
			t.Fatalf("close the session of the delivery target: %v", err)
		}
		at := store.Load()
		err := store.AcceptNotice(ctx, invariantNotice("task-1", 1))
		if !errors.Is(err, ErrDeliveryRefused) {
			t.Fatalf("acceptance at a closed target = %v, want the named refusal %v", err, ErrDeliveryRefused)
		}
		if got := store.Load(); got == at {
			t.Fatalf("the sequence stood at %d after a refusal, want the write of the mailbox to advance it", got)
		}
	})
}

// TestRoutingIsNotARefusal pins that a route the mailbox cannot open names routing and never an answer its target gave.
// - an unestablished address and a retired instance answer as themselves, and no rejection is recorded for a notification nobody refused
// - a routing failure wrapped as a refusal writes a feedback fact the target never authored
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRoutingIsNotARefusal(t *testing.T) {
	ctx := context.Background()
	store := invariantStore(t)

	unrouted := invariantNotice("task-1", 1)
	if err := store.TrackDelivery(ctx, "task-1", task.HandoffTarget{
		Target:         invariantAddress,
		TargetInstance: "instance-retired",
	}); err != nil {
		t.Fatalf("register the delivery route: %v", err)
	}
	if err := store.AcceptNotice(ctx, unrouted); err == nil {
		t.Fatal("acceptance at an instance this store never established = nil, want the named routing failure")
	}
	if got := store.Rejections(); len(got) != 0 {
		t.Fatalf("rejections after a routing failure = %+v, want none: nobody refused the notification", got)
	}
	if store.Refused(unrouted) {
		t.Fatal("the notification stands recorded as refused, want it still owed: a routing failure is not an answer")
	}
	if err := store.TrackDelivery(ctx, "task-1", task.HandoffTarget{
		Target:         event.Address{AgentID: "agent-never", SessionID: "session-never"},
		TargetInstance: "instance-a",
	}); err != nil {
		t.Fatalf("reroute the delivery: %v", err)
	}
	err := store.AcceptNotice(ctx, invariantNotice("task-1", 2))
	if err == nil || errors.Is(err, ErrDeliveryRefused) {
		t.Fatalf("acceptance at an address this run never established = %v, want a routing failure that is not a refusal", err)
	}
	if !errors.Is(err, ErrUnknownTarget) {
		t.Fatalf("acceptance at an unestablished address = %v, want %v", err, ErrUnknownTarget)
	}
}

// invariantReader is the subject reads are granted for in these invariants.
const invariantReader Subject = "reader-a"

// invariantStore returns one store whose reader is granted the session address.
func invariantStore(t *testing.T) *MemoryStore {
	t.Helper()
	ctx := context.Background()
	authority := NewAuthority()
	if err := authority.GrantRead(ctx, invariantReader, invariantAddress); err != nil {
		t.Fatalf("grant the read of the session address: %v", err)
	}
	store := NewMemoryStore(authority)
	if _, err := store.Establish(ctx, invariantAddress, "instance-a"); err != nil {
		t.Fatalf("establish the session site: %v", err)
	}
	if err := store.TrackDelivery(ctx, "task-1", task.HandoffTarget{Target: invariantAddress, TargetInstance: "instance-a"}); err != nil {
		t.Fatalf("register the delivery route: %v", err)
	}
	return store
}

// invariantRead returns the site read back through the granted reader.
func invariantRead(t *testing.T, store *MemoryStore) *Session {
	t.Helper()
	read, err := store.Site(context.Background(), invariantReader, invariantAddress, "instance-a")
	if err != nil {
		t.Fatalf("read the session site: %v", err)
	}
	return read
}

// invariantNotice builds one notification of the invariants' task at the sequence named.
func invariantNotice(taskID event.TaskID, seq int64) task.Notice {
	return task.Notice{
		TaskID: taskID,
		Round:  1,
		Kind:   task.NoticeProgress,
		Seq:    seq,
		Event:  event.EventID(string(taskID) + "-fact"),
	}
}

// invariantRecord builds one commit record of the invariants' task at the revision named.
func invariantRecord(taskID event.TaskID, revision int64, status task.Status, notices ...task.Notice) task.CommitRecord {
	outbox := make([]task.Envelope, 0, len(notices))
	for _, notice := range notices {
		outbox = append(outbox, task.Envelope{
			Notice:         notice,
			Target:         invariantAddress,
			Source:         invariantAddress,
			TargetInstance: "instance-a",
		})
	}
	return task.CommitRecord{
		MutationID:       task.MutationID(fmt.Sprintf("%s-mutation-%d", taskID, revision)),
		TaskID:           taskID,
		ExpectedRevision: revision,
		Status:           status,
		Outbox:           outbox,
	}
}

// invariantSpec builds the admission material of the invariants' task.
func invariantSpec() task.Spec {
	return task.Spec{
		CallID:       "call-1",
		DedupKey:     task.DedupKey("instance-a\x00invocation-1\x00decision-1\x00call-1"),
		InvocationID: "invocation-1",
		InstanceID:   "instance-a",
		Description:  "one admitted call",
		Source:       invariantAddress,
		Target:       invariantAddress,
		Execution:    task.Serialization("session-serialized"),
	}
}

// invariantFactLog keeps staged pairs apart from the pairs the barrier completed.
type invariantFactLog struct {
	mu       sync.Mutex
	staged   map[string][]byte
	barrier  map[string][]byte
	failSync bool
	failPut  error
	failScan error
	failGet  error
}

// newInvariantFactLog returns an empty log beside a barrier that completes.
func newInvariantFactLog() *invariantFactLog {
	return &invariantFactLog{staged: map[string][]byte{}, barrier: map[string][]byte{}}
}

// Put stages one pair without claiming it durable.
func (l *invariantFactLog) Put(_ context.Context, key string, value []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failPut != nil {
		return l.failPut
	}
	l.staged[key] = append([]byte(nil), value...)
	return nil
}

// Get returns one pair the barrier completed, unless a probe made the read fail.
func (l *invariantFactLog) Get(_ context.Context, key string) ([]byte, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failGet != nil {
		return nil, false, l.failGet
	}
	value, ok := l.barrier[key]
	return value, ok, nil
}

// Scan returns every completed pair under one prefix.
func (l *invariantFactLog) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failScan != nil {
		return nil, l.failScan
	}
	out := map[string][]byte{}
	for key, value := range l.barrier {
		if strings.HasPrefix(key, prefix) {
			out[key] = value
		}
	}
	return out, nil
}

// Sync completes the barrier over every staged pair unless it was made to fail.
func (l *invariantFactLog) Sync(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failSync {
		return errors.New("state: the write barrier did not complete")
	}
	for key, value := range l.staged {
		l.barrier[key] = value
	}
	l.staged = map[string][]byte{}
	return nil
}

// refuseSync makes the barrier fail or complete the next sync.
func (l *invariantFactLog) refuseSync(refuse bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failSync = refuse
}

// refusePut makes every stage answer the failure handed over, or stage again
// when it is nil.
func (l *invariantFactLog) refusePut(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failPut = err
}

// refuseScan makes every scan answer the failure handed over, or read again
// when it is nil.
func (l *invariantFactLog) refuseScan(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failScan = err
}

// refuseGet makes every point read answer the failure handed over, or read
// again when it is nil.
func (l *invariantFactLog) refuseGet(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failGet = err
}

// TestDurableCommitFailureObservation pins the lines the persistence face writes for every write it could not store.
// - a refused write reports the uncommitted outcome at ERROR and stores no fact
// - a refused barrier reports the uncertain outcome at ERROR and reports no durable success
// - a refused declaration reports the undeclared outcome at ERROR
// - an unreadable log reports the failed load at ERROR and answers no commit
// - a failing logger changes no outcome: the bypass drops the line and keeps the refusal
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDurableCommitFailureObservation(t *testing.T) {
	ctx := context.Background()
	taskID := event.TaskID("task-1")
	writeErr := errors.New("state: the log refused the write")

	t.Run("a refused write reports the uncommitted outcome", func(t *testing.T) {
		observer := &durableObserver{}
		defer watchDurableLog(t, observer)()
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		log.refusePut(writeErr)
		result, err := commit.Commit(ctx, invariantRecord(taskID, 0, task.StatusAccepted))
		if !errors.Is(err, writeErr) || result != task.CommitNotCommitted {
			t.Fatalf("commit beside a refused write = (%v, %v), want the uncommitted outcome beside its failure", result, err)
		}
		if !observer.mentions("error", "durable commit write failed", "task=task-1", "result=uncommitted") {
			t.Fatalf("error lines = %v, want the refused write beside its task and outcome", observer.all())
		}
	})

	t.Run("a refused barrier reports the uncertain outcome", func(t *testing.T) {
		observer := &durableObserver{}
		defer watchDurableLog(t, observer)()
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		log.refuseSync(true)
		result, err := commit.Commit(ctx, invariantRecord(taskID, 0, task.StatusAccepted))
		if err == nil || result != task.CommitUncertain {
			t.Fatalf("commit beside a refused barrier = (%v, %v), want the uncertain outcome", result, err)
		}
		if !observer.mentions("error", "durable commit barrier failed", "task=task-1", "result=uncertain") {
			t.Fatalf("error lines = %v, want the refused barrier beside its task and outcome", observer.all())
		}
	})

	t.Run("a refused declaration reports the undeclared outcome", func(t *testing.T) {
		observer := &durableObserver{}
		defer watchDurableLog(t, observer)()
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		log.refusePut(writeErr)
		if err := commit.Declare(ctx, taskID, invariantSpec()); !errors.Is(err, writeErr) {
			t.Fatalf("declaration beside a refused write = %v, want the failure of the log", err)
		}
		if !observer.mentions("error", "durable declare write failed", "task=task-1", "result=undeclared") {
			t.Fatalf("error lines = %v, want the refused declaration beside its task and outcome", observer.all())
		}
	})

	t.Run("an unreadable log reports the failed load", func(t *testing.T) {
		observer := &durableObserver{}
		defer watchDurableLog(t, observer)()
		log := newInvariantFactLog()
		log.refuseScan(writeErr)
		commit, err := loadedDurableCommit(ctx, log, nil)
		if !errors.Is(err, writeErr) || commit != nil {
			t.Fatalf("open beside an unreadable log = (%v, %v), want no commit beside the failure", commit, err)
		}
		if !observer.mentions("error", "durable commit load failed") {
			t.Fatalf("error lines = %v, want the failed load", observer.all())
		}
	})

	t.Run("a failing logger changes no outcome", func(t *testing.T) {
		observer := &durableObserver{panicsLeft: 1}
		defer watchDurableLog(t, observer)()
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit beside a failing logger: %v", err)
		}
		log.refusePut(writeErr)
		result, err := commit.Commit(ctx, invariantRecord(taskID, 0, task.StatusAccepted))
		if !errors.Is(err, writeErr) || result != task.CommitNotCommitted {
			t.Fatalf("commit beside a failing logger = (%v, %v), want the uncommitted outcome kept", result, err)
		}
	})
}

// durableObserver is the framework logger the observation checks of this package
// read the persistence face through: it records every line beside its level and
// refuses the writes its counter still holds by panicking.
type durableObserver struct {
	mu         sync.Mutex
	lines      []string
	panicsLeft int
}

// record takes one line at the named level, or refuses it by panicking.
func (o *durableObserver) record(level, text string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.panicsLeft > 0 {
		o.panicsLeft--
		panic("the logger refused the write")
	}
	o.lines = append(o.lines, level+": "+text)
}

// Debug records one debug line.
func (o *durableObserver) Debug(args ...any) { o.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (o *durableObserver) Debugf(format string, args ...any) {
	o.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (o *durableObserver) Info(args ...any) { o.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (o *durableObserver) Infof(format string, args ...any) {
	o.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (o *durableObserver) Warn(args ...any) { o.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (o *durableObserver) Warnf(format string, args ...any) {
	o.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (o *durableObserver) Error(args ...any) { o.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (o *durableObserver) Errorf(format string, args ...any) {
	o.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (o *durableObserver) Fatal(args ...any) { o.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (o *durableObserver) Fatalf(format string, args ...any) {
	o.record("fatal", fmt.Sprintf(format, args...))
}

// all reports every line taken, whatever the level.
func (o *durableObserver) all() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.lines...)
}

// mentions reports whether one line taken at the level carries every fragment.
func (o *durableObserver) mentions(level string, fragments ...string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, line := range o.lines {
		if !strings.HasPrefix(line, level+": ") {
			continue
		}
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
				break
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// watchDurableLog points the framework logger at the observer for the duration of
// one check and answers the restore that ends it, so a check reads the lines of
// its own face and leaves the package logger as it found it.
func watchDurableLog(t *testing.T, observer trpclog.Logger) func() {
	t.Helper()
	original := trpclog.Default
	trpclog.Default = observer
	restore := func() { trpclog.Default = original }
	t.Cleanup(restore)
	return restore
}

// attemptWriteErr is the failure a refused attempt write reports.
var attemptWriteErr = errors.New("state: the log refused the attempt")

// countingFactLog counts the pairs staged through one fact log, so a check reads
// whether a repeated record staged a second pair.
type countingFactLog struct {
	durable.FactLog
	mu   sync.Mutex
	puts int
}

// Put stages one pair and counts it.
func (l *countingFactLog) Put(ctx context.Context, key string, value []byte) error {
	l.mu.Lock()
	l.puts++
	l.mu.Unlock()
	return l.FactLog.Put(ctx, key, value)
}

// writes reports how many pairs were staged.
func (l *countingFactLog) writes() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.puts
}

// TestSettleAttemptRecordInvariants pins the attempt record the settle pump reads before a second drive.
// - the in-flight marker lands first and the terminal replaces it, so one notification holds one attempt record
// - a commit opened over the same log reads the attempt the stopped process left
// - a repeated record of the same content stages no second pair
// - a refused write confirms no attempt and reports the failure of the log
// - a mailbox beside no durable account answers from its own bookkeeping alone, and refuses a notice without identity
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSettleAttemptRecordInvariants(t *testing.T) {
	ctx := context.Background()
	notice := task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 0,
		Event: event.EventID("task-1-settled")}

	t.Run("the in-flight marker and the terminal are one record per notification", func(t *testing.T) {
		log := &countingFactLog{FactLog: newInvariantFactLog()}
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		if err := commit.recordAttempt(ctx, notice, SettleAttempt{}); err != nil {
			t.Fatalf("record the in-flight attempt: %v", err)
		}
		read, held, err := commit.attemptOf(notice)
		if err != nil || !held || read.Terminal != "" {
			t.Fatalf("attempt = (%+v, %v, %v), want the in-flight marker the drive lands before its turn", read, held, err)
		}
		if err := commit.recordAttempt(ctx, notice, SettleAttempt{Terminal: "completed"}); err != nil {
			t.Fatalf("record the terminal: %v", err)
		}
		read, held, err = commit.attemptOf(notice)
		if err != nil || !held || read.Terminal != "completed" {
			t.Fatalf("attempt = (%+v, %v, %v), want the terminal the turn ended on", read, held, err)
		}
		staged, err := log.Scan(ctx, attemptPrefix)
		if err != nil {
			t.Fatalf("scan the attempt records: %v", err)
		}
		if len(staged) != 1 {
			t.Fatalf("attempt records = %d, want the one record the notification holds", len(staged))
		}
		if writes := log.writes(); writes != 2 {
			t.Fatalf("staged writes = %d, want the marker and the terminal", writes)
		}
	})

	t.Run("a commit opened over the same log reads the attempt back", func(t *testing.T) {
		log := newInvariantFactLog()
		first, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		if err := first.recordAttempt(ctx, notice, SettleAttempt{Terminal: "completed"}); err != nil {
			t.Fatalf("record the attempt: %v", err)
		}
		second, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("reopen the commit: %v", err)
		}
		read, held, err := second.attemptOf(notice)
		if err != nil || !held || read.Terminal != "completed" {
			t.Fatalf("attempt read by the second commit = (%+v, %v, %v), want the terminal the first one landed", read, held, err)
		}
	})

	t.Run("a repeated record of the same content stages no second pair", func(t *testing.T) {
		log := &countingFactLog{FactLog: newInvariantFactLog()}
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		for i := 0; i < 3; i++ {
			if err := commit.recordAttempt(ctx, notice, SettleAttempt{}); err != nil {
				t.Fatalf("record the in-flight attempt %d: %v", i+1, err)
			}
		}
		if writes := log.writes(); writes != 1 {
			t.Fatalf("staged writes = %d, want the one in-flight marker the repeated records share", writes)
		}
	})

	t.Run("a refused write confirms no attempt", func(t *testing.T) {
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		log.refusePut(attemptWriteErr)
		if err := commit.recordAttempt(ctx, notice, SettleAttempt{}); !errors.Is(err, attemptWriteErr) {
			t.Fatalf("record beside a refused write = %v, want the failure of the log", err)
		}
		if _, held, err := commit.attemptOf(notice); held || err != nil {
			t.Fatalf("attempt held beside a refused write = (%v, %v), want nothing confirmed", held, err)
		}
	})

	t.Run("a mailbox beside no durable account answers from its own bookkeeping", func(t *testing.T) {
		store := NewMemoryStore(nil)
		if _, held, err := store.Attempt(ctx, notice); held || err != nil {
			t.Fatalf("attempt of a notification nothing drove = (%v, %v), want no record and no failure", held, err)
		}
		if err := store.RecordAttempt(ctx, notice, SettleAttempt{Terminal: "completed"}); err != nil {
			t.Fatalf("record the attempt in the mailbox: %v", err)
		}
		read, held, err := store.Attempt(ctx, notice)
		if err != nil || !held || read.Terminal != "completed" {
			t.Fatalf("attempt read back = (%+v, %v, %v), want the terminal the mailbox recorded", read, held, err)
		}
		if err := store.RecordAttempt(ctx, task.Notice{}, SettleAttempt{}); !errors.Is(err, ErrNoticeIdentityMissing) {
			t.Fatalf("record beside no notice identity = %v, want the missing identity by name", err)
		}
	})
}

// TestUnreadableAttemptRefusesTheDriveByIdentity pins the attempt read that decides whether a follow-up turn is driven a second time.
//   - positive: an attempt record the log holds decodes back as the terminal the turn landed, read by the notification identity a seated task never touched
//   - negative: bytes the reader cannot decode are reported by name beside no answer, so the pump never reads "no attempt" where the record exists
//   - negative: a read the log refused is reported by name beside no answer
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestUnreadableAttemptRefusesTheDriveByIdentity(t *testing.T) {
	ctx := context.Background()
	notice := task.Notice{TaskID: "task-unreadable", Round: 1, Kind: task.NoticeSettled, Seq: 0,
		Event: event.EventID("task-unreadable-settled")}
	body, err := json.Marshal(attemptRecord{
		TaskID: notice.TaskID, Round: notice.Round, Kind: notice.Kind, Seq: notice.Seq,
		Attempt: SettleAttempt{Terminal: "completed"},
	})
	if err != nil {
		t.Fatalf("render the attempt record: %v", err)
	}

	t.Run("a record the log holds answers the terminal it states", func(t *testing.T) {
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		if err := log.Put(ctx, attemptKey(notice), body); err != nil {
			t.Fatalf("stage the attempt record: %v", err)
		}
		if err := log.Sync(ctx); err != nil {
			t.Fatalf("complete the barrier: %v", err)
		}
		read, held, err := NewMemoryStore(nil, commit).Attempt(ctx, notice)
		if err != nil || !held || read.Terminal != "completed" {
			t.Fatalf("the attempt by identity = (%+v, held=%v, err=%v), want the completed terminal the log holds",
				read, held, err)
		}
	})

	t.Run("bytes no reader decodes are named beside no answer", func(t *testing.T) {
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		if err := log.Put(ctx, attemptKey(notice), []byte("this is not an attempt record")); err != nil {
			t.Fatalf("stage the unreadable record: %v", err)
		}
		if err := log.Sync(ctx); err != nil {
			t.Fatalf("complete the barrier: %v", err)
		}
		read, held, err := NewMemoryStore(nil, commit).Attempt(ctx, notice)
		if err == nil {
			t.Fatalf("the attempt by identity = (%+v, held=%v, err=nil), want the unreadable record named: a read folded into \"no attempt\" drives the turn a second time",
				read, held)
		}
		if read.Terminal != "" || held {
			t.Fatalf("the unreadable attempt answered (%+v, held=%v) beside its failure, want no attempt reported", read, held)
		}
	})

	t.Run("a read the log refused is named beside no answer", func(t *testing.T) {
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		if err := log.Put(ctx, attemptKey(notice), body); err != nil {
			t.Fatalf("stage the attempt record: %v", err)
		}
		if err := log.Sync(ctx); err != nil {
			t.Fatalf("complete the barrier: %v", err)
		}
		readErr := errors.New("the log refused the read of the attempt family")
		log.refuseGet(readErr)
		read, held, err := NewMemoryStore(nil, commit).Attempt(ctx, notice)
		if !errors.Is(err, readErr) {
			t.Fatalf("the attempt read beside a refused read = (%+v, held=%v, err=%v), want %v",
				read, held, err, readErr)
		}
		if read.Terminal != "" || held {
			t.Fatalf("the refused attempt read answered (%+v, held=%v) beside its failure, want no attempt reported", read, held)
		}
	})
}

// TestUnreadableReceiptFailsTheRestoreByName pins the receipt read a mailbox restore makes over a notification its account never seated.
//   - positive: a notification the account holds no receipt for is seated by the restore beside its frozen routing
//   - negative: a scan the log refused fails the restore by name and seats nothing
//   - so a receipt the reader could not reach is never folded into "not settled" and handed to the follow-up turn the account already closed
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestUnreadableReceiptFailsTheRestoreByName(t *testing.T) {
	ctx := context.Background()
	notice := task.Notice{TaskID: "task-restored-receipt", Round: 1, Kind: task.NoticeSettled, Seq: 0,
		Event: event.EventID("task-restored-receipt-settled")}
	envelope := task.Envelope{Notice: notice, Target: invariantAddress, Source: invariantAddress,
		TargetInstance: event.InstanceID("instance-a")}
	payload := staleMailboxPayload(t, envelope)

	t.Run("a notification the account holds no receipt for is seated", func(t *testing.T) {
		commit, err := loadedDurableCommit(ctx, newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		store := NewMemoryStore(NewAuthority(), commit)
		if err := NewMailboxState(store).RestoreState(ctx, payload); err != nil {
			t.Fatalf("restore the mailbox slice: %v", err)
		}
		if seats := store.Unconsumed(); len(seats) != 1 || seats[0].Notice != notice {
			t.Fatalf("the restored mailbox owes %+v, want the one notification the account states no receipt for", seats)
		}
	})

	t.Run("a read the log refused fails the restore by name", func(t *testing.T) {
		log := newInvariantFactLog()
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		store := NewMemoryStore(NewAuthority(), commit)
		readErr := errors.New("the log refused the read of the receipt family")
		log.refuseGet(readErr)
		err = NewMailboxState(store).RestoreState(ctx, payload)
		if !errors.Is(err, readErr) {
			t.Fatalf("the restore beside a refused receipt read = %v, want %v: a read folded into \"not settled\" hands a settled notification to a second turn",
				err, readErr)
		}
		if seats := store.Unconsumed(); len(seats) != 0 {
			t.Fatalf("the refused restore owes %+v, want none: the account states no receipt this reader reached", seats)
		}
	})
}

// receiptReadLog records the reads one commit makes: every key a point read names
// and every prefix a scan names, in the order they happen.
type receiptReadLog struct {
	durable.FactLog
	mu    sync.Mutex
	gets  []string
	scans []string
}

// Get records the key and forwards the point read.
func (l *receiptReadLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	reader, statesPointRead := l.FactLog.(pointReadLog)
	if !statesPointRead {
		return nil, false, fmt.Errorf("the wrapped log states no point read")
	}
	l.mu.Lock()
	l.gets = append(l.gets, key)
	l.mu.Unlock()
	return reader.Get(ctx, key)
}

// Scan records the prefix and forwards the scan.
func (l *receiptReadLog) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	l.mu.Lock()
	l.scans = append(l.scans, prefix)
	l.mu.Unlock()
	return l.FactLog.Scan(ctx, prefix)
}

// readMark is the point one observation window began at.
type readMark struct {
	gets  int
	scans int
}

// mark starts a new observation window and reports where it began.
func (l *receiptReadLog) mark() readMark {
	l.mu.Lock()
	defer l.mu.Unlock()
	return readMark{gets: len(l.gets), scans: len(l.scans)}
}

// readsSince reports the point reads and the scans made since one mark.
func (l *receiptReadLog) readsSince(mark readMark) ([]string, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.gets[mark.gets:]...), append([]string(nil), l.scans[mark.scans:]...)
}

// TestReceiptReadIsTheOneKeyItNames pins the read a restore pays for one notification.
//   - positive: the receipt read is the two keys of that notification's identity, each a point read
//   - negative: no read of the receipt families is a scan, so the read never costs the pairs beside the key
//   - positive: the receipt read stays outside the load's prefix measurement, which states what a whole-family read costs
//   - consumer: the mailbox restore folds a payload through the commit the boot opened (MailboxState.RestoreState)
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestReceiptReadIsTheOneKeyItNames(t *testing.T) {
	ctx := context.Background()
	notice := task.Notice{TaskID: "task-one-key", Round: 1, Kind: task.NoticeSettled, Seq: 0,
		Event: event.EventID("task-one-key-settled")}
	envelope := task.Envelope{Notice: notice, Target: invariantAddress, Source: invariantAddress,
		TargetInstance: event.InstanceID("instance-a")}
	log := &receiptReadLog{FactLog: newInvariantFactLog()}
	commit, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("open the commit: %v", err)
	}
	store := NewMemoryStore(NewAuthority(), commit)
	measured := len(commit.LoadStats().Scans)
	mark := log.mark()

	if err := NewMailboxState(store).RestoreState(ctx, staleMailboxPayload(t, envelope)); err != nil {
		t.Fatalf("restore the mailbox slice: %v", err)
	}
	if seats := store.Unconsumed(); len(seats) != 1 || seats[0].Notice != notice {
		t.Fatalf("the restored mailbox owes %+v, want the one notification the account states no receipt for", seats)
	}

	gets, scans := log.readsSince(mark)
	want := []string{consumeKey(notice), rejectKey(notice)}
	if strings.Join(gets, " ") != strings.Join(want, " ") {
		t.Fatalf("the receipt read paid for the keys %v, want the two keys of the notification's own identity %v", gets, want)
	}
	if len(scans) != 0 {
		t.Fatalf("the receipt read scanned %v, want no scan at all: a known key pays for its own pair, never for the family beside it", scans)
	}
	if got := len(commit.LoadStats().Scans); got != measured {
		t.Fatalf("the load measurement states %d prefix read(s) after the receipt read, want the %d it states before it: the receipt read is not a whole-family read", got, measured)
	}
}

// TestRestoredTaskRoundHighWater pins the round a rebuilt task re-enters at.
// - a terminal the log already consumed re-enters at the round after it reached
// - a terminal notice still owed re-enters at the same round, never rewound
// - a reactivation whose round never settled consumes the round it opened
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestRestoredTaskRoundHighWater(t *testing.T) {
	ctx := context.Background()
	const taskID = event.TaskID("task-round-water")
	settled := func(round int64) *task.Notice {
		return &task.Notice{
			TaskID: taskID,
			Round:  round,
			Kind:   task.NoticeSettled,
			Seq:    0,
			Event:  event.EventID(fmt.Sprintf("%s-settled-%d", taskID, round)),
		}
	}
	links := func(settleSecond bool) []roundLink {
		chain := []roundLink{
			{mutation: task.MutationID("admit:" + string(taskID)), status: task.StatusAccepted},
			{mutation: task.MutationID("run:" + string(taskID)), status: task.StatusRunning},
			{mutation: task.MutationID("settle:" + string(taskID) + ":1"), status: task.StatusSettled, notice: settled(1)},
			{mutation: task.MutationID("reactivate:" + string(taskID) + ":1"), status: task.StatusRunning},
		}
		if settleSecond {
			chain = append(chain, roundLink{
				mutation: task.MutationID("settle:" + string(taskID) + ":2"),
				status:   task.StatusSettled,
				notice:   settled(2),
			})
		}
		return chain
	}
	restore := func(t *testing.T, links []roundLink, consumed []int64) *task.MemoryRegistry {
		t.Helper()
		commit, chain := durableRoundChain(t, taskID, links, consumed)
		registry := task.NewMemoryRegistry(commit, task.NewMemoryAdmission())
		if _, err := registry.Restore(ctx, chain, roundOwners{}); err != nil {
			t.Fatalf("restore the task chain: %v", err)
		}
		return registry
	}

	t.Run("consumed notices continue at the next round", func(t *testing.T) {
		registry := restore(t, links(true), []int64{1, 2})
		round, err := registry.Reactivate(ctx, taskID)
		if err != nil || round != 3 {
			t.Fatalf("re-entry after the restart = (%d, %v), want the third round of the task", round, err)
		}
	})

	t.Run("unconsumed notices do not advance the next round", func(t *testing.T) {
		registry := restore(t, links(true), nil)
		round, err := registry.Reactivate(ctx, taskID)
		if err != nil || round != 3 {
			t.Fatalf("re-entry beside the owed notices = (%d, %v), want the third round of the task", round, err)
		}
	})

	t.Run("a reactivation that settled nothing still consumes its round", func(t *testing.T) {
		registry := restore(t, links(false), []int64{1})
		if err := registry.RollbackReactivation(ctx, taskID, 2); err != nil {
			t.Fatalf("put back the round the reactivation opened = %v, want the round 2 of the chain", err)
		}
		round, err := registry.Reactivate(ctx, taskID)
		if err != nil || round != 3 {
			t.Fatalf("re-entry after the rolled-back round = (%d, %v), want the third round of the task", round, err)
		}
	})
}

// roundLink is one fact of the round-chain fixture: the mutation identity the fact
// commits under, the status it moves the task to and the terminal notice it carries.
type roundLink struct {
	mutation task.MutationID
	status   task.Status
	notice   *task.Notice
}

// durableRoundChain stores one task's chain on a fresh durable log, consumes the
// terminal notices of the rounds named, and reopens the commit over the log, so a
// restored registry reads the chain the way a restarted process does.
func durableRoundChain(t *testing.T, taskID event.TaskID, links []roundLink, consumed []int64) (*DurableCommit, []task.TaskRecord) {
	t.Helper()
	ctx := context.Background()
	log := newInvariantFactLog()
	commit, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("open the durable commit: %v", err)
	}
	if err := commit.Declare(ctx, taskID, invariantSpec()); err != nil {
		t.Fatalf("declare the admission material: %v", err)
	}
	for at, link := range links {
		record := task.CommitRecord{
			MutationID:       link.mutation,
			TaskID:           taskID,
			ExpectedRevision: int64(at),
			Status:           link.status,
		}
		if link.notice != nil {
			record.Outbox = []task.Envelope{{
				Notice:         *link.notice,
				Target:         invariantAddress,
				Source:         invariantAddress,
				TargetInstance: "instance-a",
			}}
		}
		if result, err := commit.Commit(ctx, record); err != nil || result != task.CommitCommitted {
			t.Fatalf("commit %s = (%v, %v), want a committed fact", link.mutation, result, err)
		}
	}
	for _, round := range consumed {
		var notice task.Notice
		for _, link := range links {
			if link.notice != nil && link.notice.Round == round {
				notice = *link.notice
			}
		}
		if notice.TaskID == "" {
			t.Fatalf("the chain carries no notice of round %d to consume", round)
		}
		if err := commit.recordConsumption(ctx, notice); err != nil {
			t.Fatalf("record the consumption of round %d: %v", round, err)
		}
	}
	reopened, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("reopen the durable commit: %v", err)
	}
	chain, err := reopened.Records(ctx, taskID)
	if err != nil {
		t.Fatalf("read the restored chain: %v", err)
	}
	return reopened, chain
}

// roundOwners resolves every restored target to itself, so the round checks read
// the chain alone.
type roundOwners struct{}

// OwnerOf reports the target as its own owner.
func (roundOwners) OwnerOf(_ context.Context, _ event.AgentID, target event.AgentID) (event.AgentID, error) {
	return target, nil
}

// TestLoadScanCounts pins the five per-prefix read measurements of one load.
// - a load that reaches the end reports exactly five entries in scan order
// - each entry carries its own key count and byte total from the real writes
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestLoadScanCounts(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	first, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("open the first commit: %v", err)
	}
	taskID := event.TaskID("task-load-scan")
	if err := first.Declare(ctx, taskID, task.Spec{}); err != nil {
		t.Fatalf("declare the admission material: %v", err)
	}
	record := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
	if result, err := first.Commit(ctx, record); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit one fact = (%v, %v), want committed", result, err)
	}
	if err := first.recordConsumption(ctx, record.Outbox[0].Notice); err != nil {
		t.Fatalf("record the consumption receipt: %v", err)
	}
	second, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("rebuild the commit from the same log: %v", err)
	}
	stats := second.LoadStats()
	if len(stats.Scans) != 5 {
		t.Fatalf("scan entries = %d, want the five prefix reads", len(stats.Scans))
	}
	order := []string{specPrefix, consumePrefix, attemptPrefix, rejectPrefix, factPrefix}
	for i, s := range stats.Scans {
		if s.Prefix != order[i] {
			t.Errorf("scan %d prefix = %q, want %q", i, s.Prefix, order[i])
		}
		wantKeys, wantBytes := barrierTotals(log, s.Prefix)
		if s.Keys != wantKeys {
			t.Errorf("scan %q keys = %d, want %d from the completed barrier", s.Prefix, s.Keys, wantKeys)
		}
		if s.Bytes != wantBytes {
			t.Errorf("scan %q bytes = %d, want %d from the completed barrier", s.Prefix, s.Bytes, wantBytes)
		}
		if s.Elapsed < 0 {
			t.Errorf("scan %q elapsed is negative", s.Prefix)
		}
	}
	if got := len(second.Facts(taskID)); got != 1 {
		t.Fatalf("rebuilt facts = %d, want the measurement to leave the recovery intact", got)
	}
}

// barrierTotals counts the completed pairs one prefix holds and their total
// value length, straight from the log's own barrier — the independent source
// the scan measurement is checked against.
func barrierTotals(log *invariantFactLog, prefix string) (keys int, bytes int64) {
	log.mu.Lock()
	defer log.mu.Unlock()
	for key, value := range log.barrier {
		if strings.HasPrefix(key, prefix) {
			keys++
			bytes += int64(len(value))
		}
	}
	return keys, bytes
}

// selectiveScanLog forwards to an inner log but refuses the scans of one
// named prefix, so a check can observe the commit beside a read that fails
// partway instead of refusing every prefix at once.
type selectiveScanLog struct {
	inner     durable.FactLog
	failEvery map[string]bool
}

// Put stages one pair on the inner log.
func (l *selectiveScanLog) Put(ctx context.Context, key string, value []byte) error {
	return l.inner.Put(ctx, key, value)
}

// Get forwards one key's point read to the inner log.
func (l *selectiveScanLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	reader, statesPointRead := l.inner.(pointReadLog)
	if !statesPointRead {
		return nil, false, fmt.Errorf("the wrapped log states no point read")
	}
	return reader.Get(ctx, key)
}

// Scan refuses the named prefixes and forwards the rest.
func (l *selectiveScanLog) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	if l.failEvery[prefix] {
		return nil, errors.New("state: the read was refused")
	}
	return l.inner.Scan(ctx, prefix)
}

// Sync completes the barrier on the inner log.
func (l *selectiveScanLog) Sync(ctx context.Context) error {
	return l.inner.Sync(ctx)
}

// TestLoadScanCountsRefusedScanRecordsNothing pins the refused-scan negative.
//   - a failed prefix read records no measurement, so only completed reads appear
func TestLoadScanCountsRefusedScanRecordsNothing(t *testing.T) {
	d := &DurableCommit{log: &selectiveScanLog{inner: newInvariantFactLog(),
		failEvery: map[string]bool{consumePrefix: true}}}
	if _, err := d.scan(context.Background(), specPrefix); err != nil {
		t.Fatalf("the first prefix read: %v", err)
	}
	if _, err := d.scan(context.Background(), consumePrefix); err == nil {
		t.Fatal("the refused prefix read was reported as a success")
	}
	if got := d.LoadStats(); len(got.Scans) != 1 || got.Scans[0].Prefix != specPrefix {
		t.Fatalf("stats after a refused read = %+v, want only the read that reached its end", got.Scans)
	}
}

// seededSpec builds the admission material each seeded task declares: every
// exported field of task.Spec holds a non-zero value, so a comparison of two
// commits sees a difference on any field the load path could drop.
func seededSpec(tag string) task.Spec {
	return task.Spec{
		CallID:        "call-" + tag,
		DedupKey:      task.DedupKey("dedup-" + tag),
		InvocationID:  event.InvocationID("inv-" + tag),
		InstanceID:    event.InstanceID("inst-" + tag),
		Description:   "the seeded task " + tag,
		Source:        event.Address{AgentID: "agent-a", SessionID: event.SessionID("session-" + tag)},
		Target:        event.Address{AgentID: "agent-b", SessionID: event.SessionID("session-" + tag)},
		TriggerSource: "user",
		Execution:     task.Serialization("session-" + tag + "-serialized"),
	}
}

// seedCommit drives one real write set with non-zero content on every table:
// specs with identities, two commits carrying outboxes, one consumption, one
// attempt and one refusal with its routing and moment frozen.
func seedCommit(t *testing.T, ctx context.Context, log durable.FactLog, taskID event.TaskID, tag string) {
	t.Helper()
	commit, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("open the seeding commit: %v", err)
	}
	spec := seededSpec(tag)
	if err := commit.Declare(ctx, taskID, spec); err != nil {
		t.Fatalf("declare %s: %v", tag, err)
	}
	first := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
	first.Outbox[0].Target = event.Address{AgentID: "agent-b", SessionID: event.SessionID("session-" + tag)}
	first.Outbox[0].Source = event.Address{AgentID: "agent-a", SessionID: event.SessionID("session-" + tag)}
	if result, err := commit.Commit(ctx, first); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the first fact of %s = (%v, %v)", tag, result, err)
	}
	second := invariantRecord(taskID, 1, task.StatusSettled, invariantNotice(taskID, 2))
	second.Outbox[0].Target = event.Address{AgentID: "agent-b", SessionID: event.SessionID("session-" + tag)}
	if result, err := commit.Commit(ctx, second); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the second fact of %s = (%v, %v)", tag, result, err)
	}
	if err := commit.recordConsumption(ctx, first.Outbox[0].Notice); err != nil {
		t.Fatalf("record the consumption of %s: %v", tag, err)
	}
	if err := commit.recordAttempt(ctx, second.Outbox[0].Notice, SettleAttempt{Terminal: "completed"}); err != nil {
		t.Fatalf("record the attempt of %s: %v", tag, err)
	}
	refused := invariantNotice(event.TaskID("task-other-"+tag), 9)
	if err := commit.recordRejection(ctx, Rejection{
		Notice:   refused,
		Target:   event.Address{AgentID: "agent-c", SessionID: event.SessionID("session-refused-" + tag)},
		Instance: event.InstanceID("inst-refused-" + tag),
		Reason:   "refused in the seed " + tag,
		At:       time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("record the rejection of %s: %v", tag, err)
	}
}

// TestSeedCarriesEverySpecField pins the seed against task.Spec itself.
// - every exported field holds a non-zero value, so a 0 == 0 comparison cannot hide one
// - a field added to task.Spec later fails here until it is seeded
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestSeedCarriesEverySpecField(t *testing.T) {
	spec := seededSpec("every")
	value := reflect.ValueOf(spec)
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		if value.Field(i).IsZero() {
			t.Fatalf("the seed leaves task.Spec.%s at its zero value, so the comparison cannot see a difference there", field.Name)
		}
	}
}

// barrierFailureLog is one fact log whose completed barrier is replaced by a
// failure, so a check can pin what a commit reports when the barrier it reports
// success behind did not complete. The store itself is the real journal tier:
// only the barrier is injected.
type barrierFailureLog struct {
	durable.FactLog
}

// Sync reports a barrier that did not complete without touching the store.
func (barrierFailureLog) Sync(context.Context) error { return ErrBarrierRefused }

// ErrBarrierRefused is the injected barrier failure of the checks below.
var ErrBarrierRefused = errors.New("state: the injected barrier did not complete")

// TestDurableCommitBarrier pins the commit barrier on the journal tier.
// - a commit behind a completed barrier survives a reopen of the same tier: the task fact, its spec and its owed notification all read back
// - a barrier that did not complete reports CommitUncertain and stores nothing the next commit reads
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestDurableCommitBarrier(t *testing.T) {
	ctx := context.Background()

	t.Run("committed_record_survives_reopen", func(t *testing.T) {
		root := t.TempDir()
		cfg := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("open the journal tier: %v", err)
		}
		taskID := event.TaskID("task-barrier")
		seedCommit(t, ctx, ledger.TierFactLog(tier), taskID, "barrier")
		if err := tier.Close(); err != nil {
			t.Fatalf("close the journal tier before the reopen: %v", err)
		}
		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen the journal tier: %v", err)
		}
		restored, err := loadedDurableCommit(ctx, ledger.TierFactLog(reopened), nil)
		if err != nil {
			t.Fatalf("restore the commit beside the committed facts: %v", err)
		}
		if len(restored.Facts(taskID)) != 2 {
			t.Fatalf("restored facts of %s = %d, want the 2 committed", taskID, len(restored.Facts(taskID)))
		}
		if restored.specs[taskID] != seededSpec("barrier") {
			t.Fatalf("restored spec of %s = %+v, want the declared one", taskID, restored.specs[taskID])
		}
	})

	t.Run("barrier_failure_returns_uncertain", func(t *testing.T) {
		root := t.TempDir()
		tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the journal tier: %v", err)
		}
		log := barrierFailureLog{FactLog: ledger.TierFactLog(tier)}
		commit, err := loadedDurableCommit(ctx, log, nil)
		if err != nil {
			t.Fatalf("open the commit behind the failing barrier: %v", err)
		}
		taskID := event.TaskID("task-uncertain")
		if err := commit.Declare(ctx, taskID, seededSpec("uncertain")); err == nil || !errors.Is(err, ErrBarrierRefused) {
			t.Fatalf("declare behind the failing barrier = %v, want the named barrier failure", err)
		}
		result, err := commit.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning))
		if result != task.CommitUncertain || err == nil || !errors.Is(err, ErrBarrierRefused) {
			t.Fatalf("commit behind the failing barrier = (%v, %v), want the uncertain outcome beside the named failure", result, err)
		}
	})
}

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
	loaded, err := loadedDurableCommit(ctx, log, nil)
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

// accountFaces is the four recovery faces one account answers, read the same whichever path seated it.
type accountFaces struct {
	revision int64
	facts    int
	owed     []task.Notice
	consumed int
	attempts int
	rejected int
}

// facesOf reads the four faces of one task beside its chain position.
func facesOf(commit *DurableCommit, taskID event.TaskID) accountFaces {
	receipts := commit.ReceiptsOf(taskID)
	owed := make([]task.Notice, 0)
	for _, entry := range commit.Outbox(taskID) {
		owed = append(owed, entry.Notice.Notice)
	}
	return accountFaces{
		revision: commit.Revision(taskID), facts: len(commit.Facts(taskID)), owed: owed,
		consumed: len(receipts.Consumed), attempts: len(receipts.Attempts), rejected: len(receipts.Rejected),
	}
}

// sameFaces reports whether two reads of one account answer the same four faces.
func sameFaces(left, right accountFaces) bool {
	if left.revision != right.revision || left.facts != right.facts ||
		left.consumed != right.consumed || left.attempts != right.attempts || left.rejected != right.rejected ||
		len(left.owed) != len(right.owed) {
		return false
	}
	for at := range left.owed {
		if left.owed[at] != right.owed[at] {
			return false
		}
	}
	return true
}

// tailOf names the five durable family keys of one task, the set a storage states past its cut.
func tailOf(taskID event.TaskID) map[string]struct{} {
	return map[string]struct{}{
		specPrefix + string(taskID):    {},
		factPrefix + string(taskID):    {},
		consumePrefix + string(taskID): {},
		attemptPrefix + string(taskID): {},
		rejectPrefix + string(taskID):  {},
	}
}

// seedThreeNoticeTask commits one task carrying three notifications and returns them in sequence order.
func seedThreeNoticeTask(t *testing.T, ctx context.Context, log durable.FactLog, taskID event.TaskID) []task.Notice {
	t.Helper()
	commit := NewEmptyDurableCommit(log, nil)
	if err := commit.Declare(ctx, taskID, seededSpec("three-notice")); err != nil {
		t.Fatalf("declare the admission material: %v", err)
	}
	notices := []task.Notice{invariantNotice(taskID, 1), invariantNotice(taskID, 2), invariantNotice(taskID, 3)}
	if result, err := commit.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning, notices...)); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the three-notice record = (%v, %v)", result, err)
	}
	return notices
}

// TestOwedSetKeepsTheHoleWhenLaterNoticesComplete pins the owed set against out-of-order completion.
//   - positive: the account owes the unfinished notice by identity while the mailbox position stops before the hole
//   - negative: a maximum settled sequence number drops the hole, so a prefix watermark is not an owed set
//   - consumer: AcceptNotice/Claim/Consume on the mailbox beside the checkpoint seat of the same account
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestOwedSetKeepsTheHoleWhenLaterNoticesComplete(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-hole")
	notices := seedThreeNoticeTask(t, ctx, log, taskID)
	durable, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("load the account: %v", err)
	}
	store := NewMemoryStore(NewAuthority(), durable)
	if err := store.TrackDelivery(ctx, taskID, task.HandoffTarget{
		Target: invariantAddress, TargetInstance: "instance-a",
	}); err != nil {
		t.Fatalf("register the delivery: %v", err)
	}
	if err := store.restoreSeat(invariantAddress, "instance-a"); err != nil {
		t.Fatalf("open the site: %v", err)
	}
	for _, notice := range notices {
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("accept %s: %v", notice.FactKey(), err)
		}
	}
	for _, seq := range []AcceptSeq{3, 2} {
		if err := store.Claim(ctx, invariantAddress, "instance-a", seq); err != nil {
			t.Fatalf("claim %d: %v", seq, err)
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", seq); err != nil {
			t.Fatalf("consume %d: %v", seq, err)
		}
	}
	store.mu.Lock()
	position := store.sites[siteKey{addr: invariantAddress, instance: "instance-a"}].session.Consumed
	store.mu.Unlock()
	if position != 0 {
		t.Fatalf("the mailbox position advanced to %d past the hole at acceptance sequence 1, want it to stop before the hole", position)
	}
	owed := durable.OwedNotices()
	if len(owed) != 1 || owed[0].Notice.Notice != notices[0] {
		t.Fatalf("the account owes %d notice(s) after the later two completed, want exactly the hole %s", len(owed), notices[0].FactKey())
	}
	var maxSettled int64
	for _, receipt := range durable.ReceiptsOf(taskID).Consumed {
		if receipt.Notice.Seq > maxSettled {
			maxSettled = receipt.Notice.Seq
		}
	}
	skipped := 0
	for _, entry := range owed {
		if entry.Notice.Notice.Seq <= maxSettled {
			skipped++
		}
	}
	if skipped != len(owed) {
		t.Fatalf("the maximum-settled-sequence watermark %d skipped %d of %d owed notice(s), want it to skip all of them so the counterexample is the real hazard",
			maxSettled, skipped, len(owed))
	}
	t.Logf("out-of-order: mailbox position=%d, maxSettledSeq=%d, owed=%d (the hole), a max-seq watermark would drop %d/%d",
		position, maxSettled, len(owed), skipped, len(owed))

	entry := statedAccountOf(t, durable, taskID)
	seated := NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(ctx, statedPayloadOf(t, ctx, durable, entry), map[string]struct{}{}); err != nil {
		t.Fatalf("seat the account: %v", err)
	}
	if got, want := facesOf(seated, taskID), facesOf(durable, taskID); !sameFaces(got, want) {
		t.Fatalf("the seated account %+v differs from the runtime account %+v after out-of-order completion", got, want)
	}
}

// TestSeatedAccountAgreesWithIdentityRead pins the three read paths against one another on all four recovery faces.
//   - positive: the whole-log load, the checkpoint seat and the read of one task by its identity answer the same four faces
//   - negative: the covered seat reads no prefix while the identity read reads the five, so a zero read is the slice
//   - consumer: DurableCommit.ReceiptsOf/Outbox/Facts/Revision as a boot and the mailbox read them
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestSeatedAccountAgreesWithIdentityRead(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-agree")
	seedCommit(t, ctx, log, taskID, "agree")
	refusing := NewEmptyDurableCommit(log, nil)
	if err := refusing.recordRejection(ctx, Rejection{
		Notice: invariantNotice(taskID, 7), Target: invariantAddress, Instance: "instance-a",
		Reason: "refused beside the agreed account", At: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("record the refusal of the same task: %v", err)
	}
	loaded, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("load the whole log: %v", err)
	}
	want := facesOf(loaded, taskID)
	if want.consumed == 0 || want.attempts == 0 || want.rejected == 0 || len(want.owed) == 0 {
		t.Fatalf("the fixture answers %+v, want all three receipt families beside an owed notice", want)
	}

	seated := NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(ctx, statedPayloadOf(t, ctx, loaded, statedAccountOf(t, loaded, taskID)), map[string]struct{}{}); err != nil {
		t.Fatalf("seat from the slice: %v", err)
	}
	if got := facesOf(seated, taskID); !sameFaces(got, want) {
		t.Fatalf("the seated account %+v differs from the whole-log account %+v", got, want)
	}
	if scans := seated.LoadStats().Scans; len(scans) != 0 {
		t.Fatalf("the seat of a covered task read %d prefix(es), want none", len(scans))
	}

	byIdentity := NewEmptyDurableCommit(log, nil)
	empty, err := json.Marshal(task.RegistryCheckpoint{})
	if err != nil {
		t.Fatalf("state the empty account: %v", err)
	}
	if err := byIdentity.Seat(ctx, empty, tailOf(taskID)); err != nil {
		t.Fatalf("read the task by identity: %v", err)
	}
	if got := facesOf(byIdentity, taskID); !sameFaces(got, want) {
		t.Fatalf("the identity-read account %+v differs from the whole-log account %+v", got, want)
	}
	t.Logf("three paths agree: revision=%d facts=%d owed=%d consumed=%d attempts=%d rejected=%d; identity read paid %d prefix scans",
		want.revision, want.facts, len(want.owed), want.consumed, want.attempts, want.rejected, len(byIdentity.LoadStats().Scans))
}

// TestEffectiveTaskSetIsTheStatedAccountPlusTheTail pins which tasks a seated account holds.
//   - positive: the slice names a covered task and the tail names a young one, and the account holds both
//   - negative: a task neither source names is absent from TaskIDs while its receipts stay readable by identity
//   - consumer: DurableCommit.TaskIDs as the boot and the chain restore read the effective set
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestEffectiveTaskSetIsTheStatedAccountPlusTheTail(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	covered := event.TaskID("task-covered")
	young := event.TaskID("task-young")
	dropped := event.TaskID("task-dropped")
	seedCommit(t, ctx, log, covered, "covered")
	seedCommit(t, ctx, log, young, "young")
	seedCommit(t, ctx, log, dropped, "dropped")
	loaded, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("load the whole log: %v", err)
	}
	if got := len(loaded.TaskIDs()); got != 3 {
		t.Fatalf("the whole log holds %d task(s), want the 3 the fixture seeded", got)
	}

	entry := statedAccountOf(t, loaded, covered)
	seated := NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(ctx, statedPayloadOf(t, ctx, loaded, entry), tailOf(young)); err != nil {
		t.Fatalf("seat the covered task beside the young tail: %v", err)
	}
	ids := seated.TaskIDs()
	if len(ids) != 2 || ids[0] != covered || ids[1] != young {
		t.Fatalf("the seated account holds %v, want [%s %s]", ids, covered, young)
	}
	if got := facesOf(seated, young); !sameFaces(got, facesOf(loaded, young)) {
		t.Fatalf("the tail-read task %+v differs from its whole-log account %+v", got, facesOf(loaded, young))
	}
	if got := len(seated.ReceiptsOf(dropped).Consumed); got != 0 {
		t.Fatalf("the seated account states %d consumption receipt(s) of a task it never seated, want none", got)
	}
	byIdentity := NewEmptyDurableCommit(log, nil)
	empty, err := json.Marshal(task.RegistryCheckpoint{})
	if err != nil {
		t.Fatalf("state the empty account: %v", err)
	}
	if err := byIdentity.Seat(ctx, empty, tailOf(dropped)); err != nil {
		t.Fatalf("read the dropped task by identity: %v", err)
	}
	if got := len(byIdentity.ReceiptsOf(dropped).Consumed); got == 0 {
		t.Fatalf("the identity read of the dropped task answered no consumption receipt, want the one the log holds: the receipt outlives the account that stated it")
	}
	t.Logf("effective set: seated=%v, dropped task receipts seated=0 identity-read=%d",
		ids, len(byIdentity.ReceiptsOf(dropped).Consumed))
}

// unloadedFinishedTask is one stopped process's localfile root beside the task it
// finished: the checkpoint payload a boot seats (which states no entry for that
// task), the notification whose consumption receipt the log holds, the
// notification whose follow-up turn landed, and the notification nobody accepted.
type unloadedFinishedTask struct {
	cfg     ledger.StorageConfig
	payload []byte
	taskID  event.TaskID
	settled task.Envelope
	landed  task.Envelope
	owed    task.Envelope
}

// finishOneUnloadedTask runs one process over a real localfile root: it admits a
// task, settles it beside three notifications, consumes the first through the
// mailbox consumer, lands the attempt of the second, leaves the third accepted by
// nobody and files the checkpoint that states no entry for the finished task, then
// closes the tier the way a stop leaves it. The root it answers is the storage a
// boot reopens, and the payload is the account that boot seats.
func finishOneUnloadedTask(t *testing.T, ctx context.Context) unloadedFinishedTask {
	t.Helper()
	cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir(), WriteRights: true}
	tier, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the localfile root: %v", err)
	}
	commit, err := loadedDurableCommit(ctx, ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("open the durable account of the root: %v", err)
	}
	registry := task.NewMemoryRegistry(commit, task.NewMemoryAdmission())
	spec := seededSpec("unloaded")
	receipt, err := registry.Register(ctx, spec)
	if err != nil || receipt.Kind != task.AdmissionFresh {
		t.Fatalf("admit the task = (%+v, %v), want a fresh admission", receipt, err)
	}
	taskID := receipt.TaskID
	notices := []task.Notice{invariantNotice(taskID, 1), invariantNotice(taskID, 2), invariantNotice(taskID, 3)}
	record := invariantRecord(taskID, 0, task.StatusSettled, notices...)
	for at := range record.Outbox {
		record.Outbox[at].Target = spec.Target
		record.Outbox[at].TargetInstance = spec.InstanceID
	}
	if result, err := registry.Advance(ctx, taskID, task.StatusSettled, record.MutationID, record.Outbox); err != nil || result != task.CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}

	store := NewMemoryStore(NewAuthority(), commit)
	if err := store.TrackDelivery(ctx, taskID, task.HandoffTarget{Target: spec.Target, TargetInstance: spec.InstanceID}); err != nil {
		t.Fatalf("register the delivery route of the task: %v", err)
	}
	if err := store.restoreSeat(spec.Target, spec.InstanceID); err != nil {
		t.Fatalf("open the site of the target: %v", err)
	}
	for _, notice := range notices {
		if err := store.AcceptNotice(ctx, notice); err != nil {
			t.Fatalf("accept %s: %v", notice.FactKey(), err)
		}
	}
	seats := store.Unconsumed()
	if len(seats) != len(notices) {
		t.Fatalf("the mailbox holds %d accepted entrie(s), want the %d notifications the task committed", len(seats), len(notices))
	}
	if err := store.Claim(ctx, spec.Target, spec.InstanceID, seats[0].AcceptSeq); err != nil {
		t.Fatalf("claim the first accepted entry: %v", err)
	}
	if err := store.Consume(ctx, spec.Target, spec.InstanceID, seats[0].AcceptSeq); err != nil {
		t.Fatalf("consume the first accepted entry: %v", err)
	}
	if err := store.RecordAttempt(ctx, notices[1], SettleAttempt{Terminal: "completed"}); err != nil {
		t.Fatalf("land the follow-up turn of the second notification: %v", err)
	}
	manager := task.NewMemoryManager(registry, func(context.Context, task.Notice) (task.DeliveryAcceptance, error) {
		return task.DeliveryAccepted, nil
	})
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush the deliveries the task still owes: %v", err)
	}
	payload, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("file the task account: %v", err)
	}
	var wire task.RegistryCheckpoint
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("read the filed task account: %v", err)
	}
	if len(wire.Tasks) != 0 {
		t.Fatalf("the checkpoint states %d task entry(ies) of a task that finished with every obligation cleared, want none: its record stays on the chain",
			len(wire.Tasks))
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("close the localfile root before the boot: %v", err)
	}
	return unloadedFinishedTask{cfg: cfg, payload: payload, taskID: taskID,
		settled: record.Outbox[0], landed: record.Outbox[1], owed: record.Outbox[2]}
}

// bootUnloadedTask reopens the root of the stopped process and seats the account
// its checkpoint states beside the empty tail: nil is a storage that states no
// cut, and the empty set is the tail of a storage whose last commit is the
// checkpoint, so nothing the checkpoint never stated is read into the account and
// every receipt below is one the boot reads by identity or not at all.
func bootUnloadedTask(t *testing.T, ctx context.Context, fixture unloadedFinishedTask) (*DurableCommit, *MemoryStore) {
	t.Helper()
	tier, err := ledger.OpenTier(ctx, fixture.cfg)
	if err != nil {
		t.Fatalf("reopen the localfile root: %v", err)
	}
	t.Cleanup(func() {
		if err := tier.Close(); err != nil {
			t.Fatalf("close the reopened localfile root: %v", err)
		}
	})
	account := NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
	if err := account.Seat(ctx, fixture.payload, map[string]struct{}{}); err != nil {
		t.Fatalf("seat the account the checkpoint states: %v", err)
	}
	return account, NewMemoryStore(NewAuthority(), account)
}

// staleMailboxPayload renders the mailbox slice a cut filed before the deliveries
// below landed, so a boot restores obligations its account has since answered.
func staleMailboxPayload(t *testing.T, owed ...task.Envelope) []byte {
	t.Helper()
	stated := make([]task.OutboxNotice, 0, len(owed))
	for _, envelope := range owed {
		stated = append(stated, task.OutboxNotice{Notice: envelope, Delivery: task.DeliveryPending})
	}
	payload, err := json.Marshal(mailboxSliceWire{Owed: stated})
	if err != nil {
		t.Fatalf("state the mailbox slice: %v", err)
	}
	return payload
}

// TestUnloadedSettledReceiptStaysSettled pins the receipt a boot reads for a task its checkpoint never stated.
//   - fixture: one real localfile root holding a task the run finished, so the checkpoint states no entry for it and the tail states no key
//   - positive: the consumption receipt the log holds reads back by the notification's own key, so the settled notification opens no delivery
//   - positive: the terminal a follow-up turn landed reads back the same way, so a restart closes the entry beside it instead of driving a second turn
//   - negative: a notification the log holds neither a receipt nor an attempt for stays owed
//   - consumer: the mailbox restore folds a stale payload through settled, and MemoryStore.Attempt reads the turn through attemptOf
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestUnloadedSettledReceiptStaysSettled(t *testing.T) {
	ctx := context.Background()
	fixture := finishOneUnloadedTask(t, ctx)
	account, store := bootUnloadedTask(t, ctx, fixture)
	if ids := account.TaskIDs(); len(ids) != 0 {
		t.Fatalf("the seated account holds the task(s) %v, want none: the finished task states no checkpoint entry and the tail states no key of it", ids)
	}

	answered, err := account.settled(fixture.settled.Notice)
	if err != nil || !answered {
		t.Fatalf("the settled notification %s reads as (settled=%v, err=%v), want the consumption receipt the log holds: the boot seats no identity of a finished task, and a receipt it cannot read is a delivery the restore owes a second time",
			fixture.settled.Notice.FactKey(), answered, err)
	}
	attempt, held, err := store.Attempt(ctx, fixture.landed.Notice)
	if err != nil || !held || attempt.Terminal != "completed" {
		t.Fatalf("the attempt of %s reads as (%+v, held=%v, err=%v), want the completed terminal the log holds: a restart that cannot read it drives a second turn for the same notification",
			fixture.landed.Notice.FactKey(), attempt, held, err)
	}
	answered, err = account.settled(fixture.owed.Notice)
	if err != nil || answered {
		t.Fatalf("the notification %s reads as (settled=%v, err=%v), want it owed", fixture.owed.Notice.FactKey(), answered, err)
	}

	if err := NewMailboxState(store).RestoreState(ctx, staleMailboxPayload(t, fixture.settled, fixture.landed, fixture.owed)); err != nil {
		t.Fatalf("restore the mailbox the cut stated: %v", err)
	}
	seats := store.Unconsumed()
	if len(seats) != 2 {
		t.Fatalf("the restored mailbox owes %d entrie(s) %+v, want the two notifications nobody completed: the settled one is closed by the receipt the log holds",
			len(seats), seats)
	}
	for _, seat := range seats {
		if seat.Notice == fixture.settled.Notice {
			t.Fatalf("the restored mailbox owes the settled notification %s, want it closed: the receipt the log holds is that notification's consumption result",
				seat.Notice.FactKey())
		}
	}
	answered, _ = account.settled(fixture.settled.Notice)
	t.Logf("unloaded account: settled=%v attempt=%+v owed seats=%d after a stale payload named all three",
		answered, attempt, len(seats))
}

// TestUnloadedOwedReceiptRemainsOwed pins the negative of the identity read: a notification nobody consumed stays owed.
//   - positive: the notification no receipt and no attempt answers reads as owed, and the restored mailbox seats it beside its frozen routing
//   - negative: the identity read is not a second account, so the task it answers for stays out of the seated receipts
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestUnloadedOwedReceiptRemainsOwed(t *testing.T) {
	ctx := context.Background()
	fixture := finishOneUnloadedTask(t, ctx)
	account, store := bootUnloadedTask(t, ctx, fixture)
	answered, err := account.settled(fixture.owed.Notice)
	if err != nil || answered {
		t.Fatalf("the notification %s reads as (settled=%v, err=%v), want it owed: no receipt and no attempt answers it, and a read that answered settled here would drop a delivery",
			fixture.owed.Notice.FactKey(), answered, err)
	}
	if attempt, held, err := account.attemptOf(fixture.owed.Notice); err != nil || held || attempt.Terminal != "" {
		t.Fatalf("the notification %s answers the attempt (%+v, held=%v, err=%v), want no attempt at all", fixture.owed.Notice.FactKey(), attempt, held, err)
	}
	receipts := account.ReceiptsOf(fixture.taskID)
	if total := len(receipts.Consumed) + len(receipts.Attempts) + len(receipts.Rejected); total != 0 {
		t.Fatalf("the seated account states %d receipt(s) of a task it never seated, want none: the identity read answers the reader and files no second account",
			total)
	}

	if err := NewMailboxState(store).RestoreState(ctx, staleMailboxPayload(t, fixture.owed)); err != nil {
		t.Fatalf("restore the mailbox the cut stated: %v", err)
	}
	seats := store.Unconsumed()
	if len(seats) != 1 || seats[0].Notice != fixture.owed.Notice {
		t.Fatalf("the restored mailbox owes %+v, want the one notification nobody completed", seats)
	}
	if seats[0].Address != fixture.owed.Target || seats[0].Instance != fixture.owed.TargetInstance {
		t.Fatalf("the restored seat routes to (%v, %v), want the frozen routing (%v, %v)",
			seats[0].Address, seats[0].Instance, fixture.owed.Target, fixture.owed.TargetInstance)
	}
}

// TestRepeatCommitOfAStoredMutationReowsNothing pins the repeat of one stored mutation identity.
//   - positive: the stored fact is answered, no second fact lands and no delivery state advances
//   - negative: other content under the same identity is refused by name
//   - consumer: DurableCommit.Commit as the stable instruction key of a delegated round reaches it
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestRepeatCommitOfAStoredMutationReowsNothing(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-repeat")
	notices := seedThreeNoticeTask(t, ctx, log, taskID)
	durable, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("load the account: %v", err)
	}
	if err := durable.recordConsumption(ctx, notices[0]); err != nil {
		t.Fatalf("record the consumption: %v", err)
	}
	if owed := len(durable.OwedNotices()); owed != 2 {
		t.Fatalf("the account owes %d notice(s) after one consumption, want 2", owed)
	}
	result, err := durable.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning, notices...))
	if err != nil || result != task.CommitCommitted {
		t.Fatalf("the repeat of a stored mutation = (%v, %v), want the stored fact answered", result, err)
	}
	if owed := len(durable.OwedNotices()); owed != 2 {
		t.Fatalf("the repeat of a stored mutation left %d owed notice(s), want the 2 it already owed: an idempotent hit advances no delivery", owed)
	}
	if facts := len(durable.Facts(taskID)); facts != 1 {
		t.Fatalf("the repeat landed %d fact(s), want the 1 already stored", facts)
	}
	if _, err := durable.Commit(ctx, invariantRecord(taskID, 0, task.StatusSettled, notices...)); err == nil {
		t.Fatalf("a repeat carrying other content was accepted, want it refused by name")
	}
}

// TestRuntimeCommitMustNotReoweAConsumedNotice pins the runtime commit against the recovery path.
//   - positive: a mutation naming a notice the account already consumed opens no second obligation
//   - negative: the recovery seat of the same records owes the smaller set, so the two paths must agree
//   - consumer: DurableCommit.Commit on the live path and DurableCommit.Seat on the recovery path
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestRuntimeCommitMustNotReoweAConsumedNotice(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-reowe")
	notices := seedThreeNoticeTask(t, ctx, log, taskID)
	durable, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("load the account: %v", err)
	}
	if err := durable.recordConsumption(ctx, notices[0]); err != nil {
		t.Fatalf("record the consumption: %v", err)
	}
	next := task.CommitRecord{
		MutationID: "task-reowe-mutation-later", TaskID: taskID, ExpectedRevision: 1,
		Status: task.StatusRunning,
		Outbox: []task.Envelope{{Notice: notices[0], Target: invariantAddress, Source: invariantAddress, TargetInstance: "instance-a"}},
	}
	if result, err := durable.Commit(ctx, next); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the later mutation = (%v, %v)", result, err)
	}
	runtimeOwed := len(durable.OwedNotices())

	entry := statedAccountOf(t, durable, taskID)
	seated := NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(ctx, statedPayloadOf(t, ctx, durable, entry), map[string]struct{}{}); err != nil {
		t.Fatalf("seat the account: %v", err)
	}
	recoveryOwed := len(seated.OwedNotices())
	if runtimeOwed != recoveryOwed {
		t.Fatalf("the runtime commit owes %d notice(s) while the recovery path owes %d for the same records and the same consumption receipt: Commit (runtime/state/durable.go:881-887) opens an obligation for every outbox envelope, while applyRecordLocked (:375-388) skips the ones a receipt or refusal already closed",
			runtimeOwed, recoveryOwed)
	}
	if runtimeOwed != 2 {
		t.Fatalf("the account owes %d notice(s), want the 2 the consumption receipt did not close", runtimeOwed)
	}
}

// naiveReceiptsOf answers one task's receipts straight from the three tables the
// account holds: the whole-table read the per-task index replaced, kept as the
// reference an index answer is compared against.
func naiveReceiptsOf(d *DurableCommit, taskID event.TaskID) task.Receipts {
	d.mu.Lock()
	defer d.mu.Unlock()
	receipts := task.Receipts{}
	for key, committed := range d.consumed {
		if key.task != taskID {
			continue
		}
		receipts.Consumed = append(receipts.Consumed, task.StatedConsumption{
			Notice: task.Notice{TaskID: key.task, Round: key.round, Kind: key.kind, Seq: key.seq},
			Event:  committed,
		})
	}
	for key, attempt := range d.attempts {
		if key.task != taskID {
			continue
		}
		receipts.Attempts = append(receipts.Attempts, task.StatedAttempt{
			Notice:   task.Notice{TaskID: key.task, Round: key.round, Kind: key.kind, Seq: key.seq},
			Terminal: attempt.Terminal,
		})
	}
	for key, rejection := range d.rejected {
		if key.task != taskID {
			continue
		}
		receipts.Rejected = append(receipts.Rejected, task.StatedRefusal{
			Notice: rejection.Notice, Target: rejection.Target, Instance: rejection.Instance,
			Reason: rejection.Reason, At: rejection.At,
		})
	}
	sort.Slice(receipts.Consumed, func(i, j int) bool {
		return noticeBefore(receipts.Consumed[i].Notice, receipts.Consumed[j].Notice)
	})
	sort.Slice(receipts.Attempts, func(i, j int) bool {
		return noticeBefore(receipts.Attempts[i].Notice, receipts.Attempts[j].Notice)
	})
	sort.Slice(receipts.Rejected, func(i, j int) bool {
		return noticeBefore(receipts.Rejected[i].Notice, receipts.Rejected[j].Notice)
	})
	return receipts
}

// settledAccountPair answers two tasks over one log, each with its own consumed
// notice, attempt and refusal, and returns the account the runtime write paths
// filled: the account every read path is compared on.
func settledAccountPair(t *testing.T, ctx context.Context, log durable.FactLog) (*DurableCommit, []event.TaskID) {
	t.Helper()
	written := NewEmptyDurableCommit(log, nil)
	taskIDs := []event.TaskID{"task-index-a", "task-index-b"}
	for _, taskID := range taskIDs {
		if err := written.Declare(ctx, taskID, seededSpec(string(taskID))); err != nil {
			t.Fatalf("declare the admission material of %s: %v", taskID, err)
		}
		if result, err := written.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning,
			invariantNotice(taskID, 1), invariantNotice(taskID, 2),
			invariantNotice(taskID, 3), invariantNotice(taskID, 4))); err != nil ||
			result != task.CommitCommitted {
			t.Fatalf("commit the three-notice record of %s = (%v, %v)", taskID, result, err)
		}
	}
	store := NewMemoryStore(NewAuthority(), written)
	for _, taskID := range taskIDs {
		if err := store.TrackDelivery(ctx, taskID, task.HandoffTarget{
			Target: invariantAddress, TargetInstance: "instance-a",
		}); err != nil {
			t.Fatalf("register the delivery of %s: %v", taskID, err)
		}
	}
	if err := store.restoreSeat(invariantAddress, "instance-a"); err != nil {
		t.Fatalf("open the site: %v", err)
	}
	for _, taskID := range taskIDs {
		for _, seq := range []int64{1, 2} {
			if err := store.AcceptNotice(ctx, invariantNotice(taskID, seq)); err != nil {
				t.Fatalf("accept %s: %v", invariantNotice(taskID, seq).FactKey(), err)
			}
		}
	}
	for _, seq := range []AcceptSeq{1, 2, 3, 4} {
		if err := store.Claim(ctx, invariantAddress, "instance-a", seq); err != nil {
			t.Fatalf("claim %d: %v", seq, err)
		}
		if err := store.Consume(ctx, invariantAddress, "instance-a", seq); err != nil {
			t.Fatalf("consume %d: %v", seq, err)
		}
	}
	for _, taskID := range taskIDs {
		if err := written.recordAttempt(ctx, invariantNotice(taskID, 3), SettleAttempt{Terminal: "completed"}); err != nil {
			t.Fatalf("record the attempt of %s: %v", taskID, err)
		}
		if err := written.recordRejection(ctx, Rejection{
			Notice: invariantNotice(taskID, 4), Target: invariantAddress, Instance: "instance-a",
			Reason: "the target refused it", At: time.Unix(0, 0).UTC(),
		}); err != nil {
			t.Fatalf("record the refusal of %s: %v", taskID, err)
		}
	}
	return written, taskIDs
}

// TestReceiptsOfAnswersTheReceiptsOfOneTask pins the per-task answer against the whole-table read.
//   - positive: every task of the runtime account, the whole-log load, the checkpoint seat and the identity read answers its own three receipt families
//   - negative: a task the account never seated answers no receipt, so an indexed answer is the task's receipts and never a neighbour's
//   - consumer: ReceiptsOf as StateSettleAccount states the account of every task a checkpoint covers
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestReceiptsOfAnswersTheReceiptsOfOneTask(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	runtimeWritten, taskIDs := settledAccountPair(t, ctx, log)
	written, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("load the account: %v", err)
	}
	entries := make([]task.StatedTask, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		entries = append(entries, statedAccountOf(t, written, taskID))
	}
	seated := NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(ctx, statedPayloadOf(t, ctx, written, entries...), map[string]struct{}{}); err != nil {
		t.Fatalf("seat the stated account: %v", err)
	}
	empty, err := json.Marshal(task.RegistryCheckpoint{})
	if err != nil {
		t.Fatalf("state the empty account: %v", err)
	}
	byIdentity := NewEmptyDurableCommit(log, nil)
	tail := map[string]struct{}{}
	for _, taskID := range taskIDs {
		for key := range tailOf(taskID) {
			tail[key] = struct{}{}
		}
	}
	if err := byIdentity.Seat(ctx, empty, tail); err != nil {
		t.Fatalf("read both tasks by identity: %v", err)
	}
	paths := []struct {
		name   string
		commit *DurableCommit
	}{{"the runtime account", runtimeWritten}, {"the whole-log load", written}, {"the checkpoint seat", seated}, {"the identity read", byIdentity}}
	for _, path := range paths {
		for _, taskID := range taskIDs {
			answered := path.commit.ReceiptsOf(taskID)
			if len(answered.Consumed) == 0 || len(answered.Attempts) == 0 || len(answered.Rejected) == 0 {
				t.Fatalf("%s answers %+v for %s, want the three receipt families the write paths recorded", path.name, answered, taskID)
			}
			if reference := naiveReceiptsOf(path.commit, taskID); !reflect.DeepEqual(answered, reference) {
				t.Fatalf("%s answers %+v for %s while the whole-table read answers %+v: a per-task answer MUST be that task's receipts",
					path.name, answered, taskID, reference)
			}
		}
		if answered := path.commit.ReceiptsOf("task-never-seated"); len(answered.Consumed)+len(answered.Attempts)+len(answered.Rejected) != 0 {
			t.Fatalf("%s answers %+v for a task it never seated, want no receipt", path.name, answered)
		}
	}
	for _, taskID := range taskIDs {
		answered := seated.ReceiptsOf(taskID)
		if len(answered.Consumed) != 2 || len(answered.Attempts) != 1 || len(answered.Rejected) != 1 {
			t.Fatalf("the seated account of %s states %d consumed, %d attempt(s) and %d refusal(s), want the 2, 1 and 1 the write paths recorded",
				taskID, len(answered.Consumed), len(answered.Attempts), len(answered.Rejected))
		}
	}
}

// seatedManyTaskAccount answers an account of tasks tasks, each stating receipts
// consumed notices, seated from one payload so no log read is paid.
func seatedManyTaskAccount(t *testing.T, tasks, receipts int) *DurableCommit {
	t.Helper()
	wire := task.RegistryCheckpoint{Tasks: make([]task.StatedTask, 0, tasks)}
	for at := 0; at < tasks; at++ {
		taskID := costTaskID(tasks, at)
		consumed := make([]task.StatedConsumption, 0, receipts)
		for seq := 0; seq < receipts; seq++ {
			consumed = append(consumed, task.StatedConsumption{
				Notice: task.Notice{TaskID: taskID, Round: 1, Kind: task.NoticeProgress, Seq: int64(seq)},
				Event:  event.EventID("cost-fact"),
			})
		}
		wire.Tasks = append(wire.Tasks, task.StatedTask{
			TaskID: taskID, Revision: 0,
			Consumed: consumed, Attempts: []task.StatedAttempt{}, Rejected: []task.StatedRefusal{},
		})
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("state the cost payload: %v", err)
	}
	commit := NewEmptyDurableCommit(nil, nil)
	if err := commit.Seat(context.Background(), payload, map[string]struct{}{}); err != nil {
		t.Fatalf("seat the cost payload: %v", err)
	}
	return commit
}

// costTaskID answers the identity of the index-th task of a cost account.
func costTaskID(tasks, index int) event.TaskID {
	return event.TaskID(fmt.Sprintf("task-cost-%05d-of-%d", index, tasks))
}

// bestReceiptsOfCost answers the cheapest per-call cost of one task's receipts over
// several rounds: the statistic that carries the growth axis with the lowest noise.
func bestReceiptsOfCost(commit *DurableCommit, taskID event.TaskID) time.Duration {
	const rounds, calls = 5, 20
	best := time.Duration(1<<62 - 1)
	for round := 0; round < rounds; round++ {
		start := time.Now()
		for call := 0; call < calls; call++ {
			commit.ReceiptsOf(taskID)
		}
		if cost := time.Since(start) / calls; cost < best {
			best = cost
		}
	}
	return best
}

// TestReceiptsOfCostsOnlyItsOwnTask pins the growth axis of one task's answer.
//   - positive: one task's answer costs the same whether the account holds two tasks or 128 times as many
//   - negative: the whole-table read grows with the account, so the bound fails on the answer it replaced
//   - consumer: ReceiptsOf inside StateSettleAccount, once per task a checkpoint covers
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestReceiptsOfCostsOnlyItsOwnTask(t *testing.T) {
	const perTask = 64
	small := seatedManyTaskAccount(t, 2, perTask)
	big := seatedManyTaskAccount(t, 256, perTask)
	target := costTaskID(2, 0)
	if got := len(small.ReceiptsOf(target).Consumed); got != perTask {
		t.Fatalf("the small account answers %d receipt(s), want the %d it states", got, perTask)
	}
	if got := len(big.ReceiptsOf(costTaskID(256, 0)).Consumed); got != perTask {
		t.Fatalf("the big account answers %d receipt(s), want the %d it states", got, perTask)
	}
	bestReceiptsOfCost(small, target)
	bestReceiptsOfCost(big, costTaskID(256, 0))
	smallCost := bestReceiptsOfCost(small, target)
	bigCost := bestReceiptsOfCost(big, costTaskID(256, 0))
	const bound = 4
	if bigCost > bound*smallCost {
		t.Fatalf("one task's answer costs %v on an account of 256 tasks beside %v on an account of 2 (bound %d×): the answer walks the receipts of the tasks it is not about",
			bigCost, smallCost, bound)
	}
	naive := bestReceiptsOfNaiveCost(big, costTaskID(256, 0))
	t.Logf("per-task answer: 2 tasks=%v 256 tasks=%v (bound %d×); the whole-table read of the same task costs %v",
		smallCost, bigCost, bound, naive)
}

// bestReceiptsOfNaiveCost answers the cheapest per-call cost of the whole-table
// read, the answer the per-task index replaced.
func bestReceiptsOfNaiveCost(commit *DurableCommit, taskID event.TaskID) time.Duration {
	const rounds, calls = 5, 20
	best := time.Duration(1<<62 - 1)
	for round := 0; round < rounds; round++ {
		start := time.Now()
		for call := 0; call < calls; call++ {
			naiveReceiptsOf(commit, taskID)
		}
		if cost := time.Since(start) / calls; cost < best {
			best = cost
		}
	}
	return best
}

// TestSeatStatesTheOpenWorkAndReadsAFinishedTaskBackByIdentity pins what a boot pays for a task that finished before its cut.
//   - the checkpoint states no entry of the finished task, so the seat reads no durable key of it at all
//   - the account holds no identity of it, so what a boot reports is the open work and no finished identity
//   - the chain is read back by identity the first time a reader names the task, and answers the record the whole log states, byte for byte
//   - the read it pays is that one task's families, so the record stays on the chain instead of being carried by every checkpoint
//   - the history enumeration names the task the account does not hold, which is the read a caller asking for the whole history pays
//   - negative: an entry stating a settle account the task never held is refused by name, so the seat states no receipt it cannot answer
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestSeatStatesTheOpenWorkAndReadsAFinishedTaskBackByIdentity(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-seat-finished")
	seedCommit(t, ctx, log, taskID, "finished")
	whole, err := loadedDurableCommit(ctx, log, nil)
	if err != nil {
		t.Fatalf("load the commit from the whole log: %v", err)
	}
	want, err := whole.Records(ctx, taskID)
	if err != nil {
		t.Fatalf("read the chain of the whole log: %v", err)
	}
	if len(want) == 0 {
		t.Fatal("the fixture seeds no chain, so the read back below proves nothing")
	}

	seated := NewEmptyDurableCommit(log, nil)
	if err := seated.Seat(ctx, statedPayloadOf(t, ctx, whole), map[string]struct{}{}); err != nil {
		t.Fatalf("seat the account of a checkpoint stating no finished task: %v", err)
	}
	for _, scan := range seated.LoadStats().Scans {
		t.Fatalf("the seat read %q of a task the checkpoint states nothing of, want no durable key: the record stays on the chain", scan.Prefix)
	}
	if got := seated.TaskIDs(); len(got) != 0 {
		t.Fatalf("the seated account names %v, want no identity: a task that finished before the cut is no open work of the account", got)
	}
	if got := seated.Revision(taskID); got != 0 {
		t.Fatalf("the seated revision = %d, want no revision: the account holds no record of the finished task", got)
	}

	got, err := seated.Records(ctx, taskID)
	if err != nil {
		t.Fatalf("read the finished task back by identity: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("the task read back as %d link(s), want the %d the whole log states", len(got), len(want))
	}
	for at := range want {
		gotBody, err := task.EncodeTaskRecord(got[at])
		if err != nil {
			t.Fatalf("render the link %d read back: %v", at, err)
		}
		wantBody, err := task.EncodeTaskRecord(want[at])
		if err != nil {
			t.Fatalf("render the link %d of the whole log: %v", at, err)
		}
		if string(gotBody) != string(wantBody) {
			t.Fatalf("the link %d read back = %s, want the bytes the whole log states %s", at, gotBody, wantBody)
		}
	}
	reads := seated.LoadStats().Scans
	if len(reads) == 0 {
		t.Fatal("the read back paid no durable key, want the families of the one task it named")
	}
	for _, scan := range reads {
		if !strings.Contains(scan.Prefix, string(taskID)) {
			t.Fatalf("the read back scanned %q, want the families of the task %s alone: the record is read by identity, never by history",
				scan.Prefix, taskID)
		}
	}

	history, err := seated.HistoryTaskIDs(ctx)
	if err != nil {
		t.Fatalf("enumerate the task history: %v", err)
	}
	if !reflect.DeepEqual(history, []event.TaskID{taskID}) {
		t.Fatalf("the history enumeration names %v, want the %v the log holds a fact for: the account state is not the history", history, []event.TaskID{taskID})
	}

	refused := NewEmptyDurableCommit(log, nil)
	stranger := task.StatedTask{
		TaskID: taskID, Status: task.StatusRunning, Revision: whole.Revision(taskID),
		Consumed: []task.StatedConsumption{{Notice: task.Notice{TaskID: "task-seat-other"}, Event: "fact-1"}},
		Attempts: []task.StatedAttempt{}, Rejected: []task.StatedRefusal{},
	}
	strangerPayload, err := json.Marshal(task.RegistryCheckpoint{Tasks: []task.StatedTask{stranger}})
	if err != nil {
		t.Fatalf("state the entry beside a receipt: %v", err)
	}
	if err := refused.Seat(ctx, strangerPayload, map[string]struct{}{}); err == nil {
		t.Fatal("the seat accepted an entry stating a receipt of another task, want it refused by name")
	}
}
