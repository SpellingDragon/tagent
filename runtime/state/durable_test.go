package state

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// invariantAddress is the explicit address every record and delivery of these invariants names.
var invariantAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

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
		commit, err := NewDurableCommit(newInvariantFactLog(), nil)
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
		commit, err := NewDurableCommit(newInvariantFactLog(), nil)
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
		commit, err := NewDurableCommit(newInvariantFactLog(), nil)
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
		commit, err := NewDurableCommit(log, nil)
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
		commit, err := NewDurableCommit(newInvariantFactLog(), probe)
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
		first, err := NewDurableCommit(log, nil)
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
		second, err := NewDurableCommit(log, nil)
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
		commit, err := NewDurableCommit(newInvariantFactLog(), nil)
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
		commit, err := NewDurableCommit(newInvariantFactLog(), nil)
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

// Get returns one pair the barrier completed.
func (l *invariantFactLog) Get(_ context.Context, key string) ([]byte, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
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
		commit, err := NewDurableCommit(log, nil)
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
		commit, err := NewDurableCommit(log, nil)
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
		commit, err := NewDurableCommit(log, nil)
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
		commit, err := NewDurableCommit(log, nil)
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
		commit, err := NewDurableCommit(log, nil)
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
	FactLog
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
		commit, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		if err := commit.recordAttempt(ctx, notice, SettleAttempt{}); err != nil {
			t.Fatalf("record the in-flight attempt: %v", err)
		}
		read, held := commit.attemptOf(notice)
		if !held || read.Terminal != "" {
			t.Fatalf("attempt = (%+v, %v), want the in-flight marker the drive lands before its turn", read, held)
		}
		if err := commit.recordAttempt(ctx, notice, SettleAttempt{Terminal: "completed"}); err != nil {
			t.Fatalf("record the terminal: %v", err)
		}
		read, held = commit.attemptOf(notice)
		if !held || read.Terminal != "completed" {
			t.Fatalf("attempt = (%+v, %v), want the terminal the turn ended on", read, held)
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
		first, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		if err := first.recordAttempt(ctx, notice, SettleAttempt{Terminal: "completed"}); err != nil {
			t.Fatalf("record the attempt: %v", err)
		}
		second, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("reopen the commit: %v", err)
		}
		read, held := second.attemptOf(notice)
		if !held || read.Terminal != "completed" {
			t.Fatalf("attempt read by the second commit = (%+v, %v), want the terminal the first one landed", read, held)
		}
	})

	t.Run("a repeated record of the same content stages no second pair", func(t *testing.T) {
		log := &countingFactLog{FactLog: newInvariantFactLog()}
		commit, err := NewDurableCommit(log, nil)
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
		commit, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the commit: %v", err)
		}
		log.refusePut(attemptWriteErr)
		if err := commit.recordAttempt(ctx, notice, SettleAttempt{}); !errors.Is(err, attemptWriteErr) {
			t.Fatalf("record beside a refused write = %v, want the failure of the log", err)
		}
		if _, held := commit.attemptOf(notice); held {
			t.Fatalf("attempt held beside a refused write = true, want nothing confirmed")
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
	commit, err := NewDurableCommit(log, nil)
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
	reopened, err := NewDurableCommit(log, nil)
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
	first, err := NewDurableCommit(log, nil)
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
	second, err := NewDurableCommit(log, nil)
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
	inner     FactLog
	failEvery map[string]bool
}

// Put stages one pair on the inner log.
func (l *selectiveScanLog) Put(ctx context.Context, key string, value []byte) error {
	return l.inner.Put(ctx, key, value)
}

// Get reads one pair from the inner log.
func (l *selectiveScanLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return l.inner.Get(ctx, key)
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
func seedCommit(t *testing.T, ctx context.Context, log FactLog, taskID event.TaskID, tag string) {
	t.Helper()
	commit, err := NewDurableCommit(log, nil)
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
	FactLog
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
		cfg := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("open the journal tier: %v", err)
		}
		taskID := event.TaskID("task-barrier")
		seedCommit(t, ctx, ledger.TierFactLog(tier), taskID, "barrier")
		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen the journal tier: %v", err)
		}
		restored, err := NewDurableCommit(ledger.TierFactLog(reopened), nil)
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
		tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
		if err != nil {
			t.Fatalf("open the journal tier: %v", err)
		}
		log := barrierFailureLog{FactLog: ledger.TierFactLog(tier)}
		commit, err := NewDurableCommit(log, nil)
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
