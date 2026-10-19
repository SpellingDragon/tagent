package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// invariantAddress is the explicit address every record and delivery of these invariants names.
var invariantAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// TestDurableFactChainInvariants pins the fact chain's idempotency and the single advance of a delivery.
// - a repeated mutation identity stores one fact and leaves every delivery where it was
// - one mutation identity beside other content, and a record expecting another revision, are refused by name
// - a commit the write barrier left unconfirmed reports uncertain and keeps no fact
// - a second carrier over the same log revives the facts and every delivery as pending again
// - the admission material is declared once and never replaced by other content
// - an acceptance of a notification the carrier owes nothing for is refused by name
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDurableFactChainInvariants(t *testing.T) {
	ctx := context.Background()
	taskID := event.TaskID("task-1")

	t.Run("a repeated mutation identity stores one fact and leaves every delivery where it was", func(t *testing.T) {
		carrier, err := NewDurableCommit(newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable carrier: %v", err)
		}
		record := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
		if result, err := carrier.Commit(ctx, record); err != nil || result != task.CommitCommitted {
			t.Fatalf("commit the first fact = (%v, %v), want a committed record", result, err)
		}
		if result, err := carrier.Commit(ctx, record); err != nil || result != task.CommitCommitted {
			t.Fatalf("repeat the mutation = (%v, %v), want the stored fact reported again", result, err)
		}
		if got := len(carrier.Facts(taskID)); got != 1 {
			t.Fatalf("facts after a repeated mutation = %d, want one", got)
		}
		if got := carrier.Revision(taskID); got != 1 {
			t.Fatalf("revision after a repeated mutation = %d, want one", got)
		}
		owed := carrier.Outbox(taskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryPending {
			t.Fatalf("outbox after a repeated mutation = %+v, want one pending notification", owed)
		}
		accepted, err := carrier.Accept(ctx, record.Outbox[0].Notice)
		if err != nil || accepted != task.DeliveryAccepted {
			t.Fatalf("accept the delivered notification = (%v, %v), want it accepted", accepted, err)
		}
		if _, err := carrier.Commit(ctx, record); err != nil {
			t.Fatalf("repeat the mutation beside an accepted delivery: %v", err)
		}
		owed = carrier.Outbox(taskID)
		if len(owed) != 1 || owed[0].Delivery != task.DeliveryAccepted {
			t.Fatalf("outbox after a repeated mutation = %+v, want the acceptance kept", owed)
		}
	})

	t.Run("one mutation identity beside other content is refused by name", func(t *testing.T) {
		carrier, err := NewDurableCommit(newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable carrier: %v", err)
		}
		record := invariantRecord(taskID, 0, task.StatusRunning)
		if _, err := carrier.Commit(ctx, record); err != nil {
			t.Fatalf("commit the first fact: %v", err)
		}
		conflict := record
		conflict.Status = task.StatusSettled
		if _, err := carrier.Commit(ctx, conflict); !errors.Is(err, task.ErrMutationConflict) {
			t.Fatalf("one identity beside other content = %v, want %v", err, task.ErrMutationConflict)
		}
		if got := len(carrier.Facts(taskID)); got != 1 {
			t.Fatalf("facts after the refused mutation = %d, want the stored one alone", got)
		}
		if got := carrier.Revision(taskID); got != 1 {
			t.Fatalf("revision after the refused mutation = %d, want it unmoved", got)
		}
	})

	t.Run("a record expecting another revision is refused by name", func(t *testing.T) {
		carrier, err := NewDurableCommit(newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable carrier: %v", err)
		}
		if _, err := carrier.Commit(ctx, invariantRecord(taskID, 5, task.StatusRunning)); !errors.Is(err, task.ErrRevisionConflict) {
			t.Fatalf("a record expecting revision 5 = %v, want %v", err, task.ErrRevisionConflict)
		}
		if got := len(carrier.Facts(taskID)); got != 0 {
			t.Fatalf("facts after the refused revision = %d, want none", got)
		}
	})

	t.Run("a commit the write barrier left unconfirmed reports uncertain and keeps no fact", func(t *testing.T) {
		log := newInvariantFactLog()
		carrier, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the durable carrier: %v", err)
		}
		log.refuseSync(true)
		record := invariantRecord(taskID, 0, task.StatusRunning)
		result, err := carrier.Commit(ctx, record)
		if err == nil || result != task.CommitUncertain {
			t.Fatalf("commit behind a failed barrier = (%v, %v), want an uncertain outcome", result, err)
		}
		if got := len(carrier.Facts(taskID)); got != 0 {
			t.Fatalf("facts behind a failed barrier = %d, want none counted as stored", got)
		}
		if got := carrier.Revision(taskID); got != 0 {
			t.Fatalf("revision behind a failed barrier = %d, want none", got)
		}
		log.refuseSync(false)
		if result, err := carrier.Commit(ctx, record); err != nil || result != task.CommitCommitted {
			t.Fatalf("reconciled retry = (%v, %v), want the record stored once", result, err)
		}
		if got := len(carrier.Facts(taskID)); got != 1 {
			t.Fatalf("facts after the reconciled retry = %d, want one", got)
		}
	})

	t.Run("a probed attempt is uncertain and is never counted as stored", func(t *testing.T) {
		probe := func(task.CommitRecord) bool { return true }
		carrier, err := NewDurableCommit(newInvariantFactLog(), probe)
		if err != nil {
			t.Fatalf("open the durable carrier: %v", err)
		}
		if result, err := carrier.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning)); err != nil || result != task.CommitUncertain {
			t.Fatalf("probed commit = (%v, %v), want an uncertain outcome", result, err)
		}
		if got := len(carrier.Facts(taskID)); got != 0 {
			t.Fatalf("facts after a probed commit = %d, want none", got)
		}
	})

	t.Run("a second carrier over the same log revives the facts and every delivery as pending again", func(t *testing.T) {
		log := newInvariantFactLog()
		first, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the first durable carrier: %v", err)
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
			t.Fatalf("reopen the durable carrier over the same log: %v", err)
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
		carrier, err := NewDurableCommit(newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable carrier: %v", err)
		}
		spec := invariantSpec()
		if err := carrier.Declare(ctx, taskID, spec); err != nil {
			t.Fatalf("declare the admission material: %v", err)
		}
		if err := carrier.Declare(ctx, taskID, spec); err != nil {
			t.Fatalf("repeat the declaration: %v", err)
		}
		other := spec
		other.Description = "another admission material"
		if err := carrier.Declare(ctx, taskID, other); !errors.Is(err, ErrSpecConflict) {
			t.Fatalf("declaration beside other content = %v, want %v", err, ErrSpecConflict)
		}
	})

	t.Run("an acceptance of a notification the carrier owes nothing for is refused by name", func(t *testing.T) {
		carrier, err := NewDurableCommit(newInvariantFactLog(), nil)
		if err != nil {
			t.Fatalf("open the durable carrier: %v", err)
		}
		if _, err := carrier.Accept(ctx, invariantNotice("task-unknown", 1)); !errors.Is(err, ErrNoticeNotOwed) {
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
		if err := store.Consume(ctx, invariantAddress, "instance-a", 1); err != nil {
			t.Fatalf("repeat the consumption: %v", err)
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
}

// newInvariantFactLog returns an empty log beside a barrier that completes.
func newInvariantFactLog() *invariantFactLog {
	return &invariantFactLog{staged: map[string][]byte{}, barrier: map[string][]byte{}}
}

// Put stages one pair without claiming it durable.
func (l *invariantFactLog) Put(_ context.Context, key string, value []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
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
