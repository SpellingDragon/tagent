package state

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// seededSpec builds the admission material each seeded task declares: every
// exported field of task.Spec holds a non-zero value, so the equivalence
// comparison sees a difference on any field the restore path could drop.
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

// seedCarrier drives one real write set with non-zero content on every table:
// specs with identities, two commits carrying outboxes, one consumption, one
// attempt and one refusal with its routing and moment frozen.
func seedCarrier(t *testing.T, ctx context.Context, log FactLog, taskID event.TaskID, tag string) {
	t.Helper()
	carrier, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the seeding carrier: %v", err)
	}
	spec := seededSpec(tag)
	if err := carrier.Declare(ctx, taskID, spec); err != nil {
		t.Fatalf("declare %s: %v", tag, err)
	}
	first := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
	first.Outbox[0].Target = event.Address{AgentID: "agent-b", SessionID: event.SessionID("session-" + tag)}
	first.Outbox[0].Source = event.Address{AgentID: "agent-a", SessionID: event.SessionID("session-" + tag)}
	if result, err := carrier.Commit(ctx, first); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the first fact of %s = (%v, %v)", tag, result, err)
	}
	second := invariantRecord(taskID, 1, task.StatusSettled, invariantNotice(taskID, 2))
	second.Outbox[0].Target = event.Address{AgentID: "agent-b", SessionID: event.SessionID("session-" + tag)}
	if result, err := carrier.Commit(ctx, second); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the second fact of %s = (%v, %v)", tag, result, err)
	}
	if err := carrier.recordConsumption(ctx, first.Outbox[0].Notice); err != nil {
		t.Fatalf("record the consumption of %s: %v", tag, err)
	}
	if err := carrier.recordAttempt(ctx, second.Outbox[0].Notice, SettleAttempt{Terminal: "completed"}); err != nil {
		t.Fatalf("record the attempt of %s: %v", tag, err)
	}
	refused := invariantNotice(event.TaskID("task-other-"+tag), 9)
	if err := carrier.recordRejection(ctx, Rejection{
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
			t.Fatalf("the seed leaves task.Spec.%s at its zero value, so the equivalence cannot see a difference there", field.Name)
		}
	}
}

// assertCarrierEqual compares two rebuilt carriers on every observable face
// with reflect.DeepEqual, so a lost or changed field on any of the eight
// tables fails the equivalence instead of passing on equal lengths.
func assertCarrierEqual(t *testing.T, want, got *DurableCommit, taskIDs ...event.TaskID) {
	t.Helper()
	for _, taskID := range taskIDs {
		if !reflect.DeepEqual(want.Facts(taskID), got.Facts(taskID)) {
			t.Fatalf("facts of %s differ: got %+v, want %+v", taskID, got.Facts(taskID), want.Facts(taskID))
		}
		if w, g := want.Revision(taskID), got.Revision(taskID); w != g {
			t.Fatalf("revision of %s = %d, want %d", taskID, g, w)
		}
		if !reflect.DeepEqual(want.Outbox(taskID), got.Outbox(taskID)) {
			t.Fatalf("outbox of %s differs: got %+v, want %+v", taskID, got.Outbox(taskID), want.Outbox(taskID))
		}
	}
	if !reflect.DeepEqual(want.mutations, got.mutations) {
		t.Fatalf("mutations differ: got %+v, want %+v", got.mutations, want.mutations)
	}
	if !reflect.DeepEqual(want.consumed, got.consumed) {
		t.Fatalf("consumed differ: got %+v, want %+v", got.consumed, want.consumed)
	}
	if !reflect.DeepEqual(want.specs, got.specs) {
		t.Fatalf("specs differ: got %+v, want %+v", got.specs, want.specs)
	}
	if !reflect.DeepEqual(want.attempts, got.attempts) {
		t.Fatalf("attempts differ: got %+v, want %+v", got.attempts, want.attempts)
	}
	if !reflect.DeepEqual(want.rejected, got.rejected) {
		t.Fatalf("rejected differ: got %+v, want %+v", got.rejected, want.rejected)
	}
}

// newBareCarrier rebuilds over the same log ignoring any ticket, the
// independent full-replay source the equivalence is checked against.
func newBareCarrier(log FactLog) (*DurableCommit, error) {
	d := newCarrier(log)
	if err := d.load(context.Background()); err != nil {
		return nil, err
	}
	return d, nil
}

// TestFullReplayEqualsDumpPlusDelta pins ticket-restore equals full replay.
//   - the snapshot is filed after the first task; the second lands after it
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestFullReplayEqualsDumpPlusDelta(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	early := event.TaskID("task-early")
	seedCarrier(t, ctx, log, early, "early")
	writer, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the ticket writer: %v", err)
	}
	if err := writer.WriteRecoveryTicket(ctx); err != nil {
		t.Fatalf("file the recovery ticket: %v", err)
	}
	late := event.TaskID("task-late")
	seedCarrier(t, ctx, log, late, "late")

	full, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("rebuild by the ticket path: %v", err)
	}
	if !full.UsedTicket() {
		t.Fatal("the rebuild beside a valid ticket did not use it")
	}
	bare, err := newBareCarrier(log)
	if err != nil {
		t.Fatalf("rebuild by the bare replay: %v", err)
	}
	assertCarrierEqual(t, bare, full, early, late)
}

// TestRecoveryTicketFallbacks pins every refusal falls back to full replay.
//   - no ticket / deleted blob / digest failure / future schema / early-empty ticket
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestRecoveryTicketFallbacks(t *testing.T) {
	ctx := context.Background()
	taskID := event.TaskID("task-fallback")

	t.Run("no ticket answers the bare replay", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "bare")
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild without a ticket: %v", err)
		}
		if restored.UsedTicket() {
			t.Fatal("a rebuild without a ticket claimed to use one")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})

	t.Run("a corrupted blob falls back and loses nothing", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "corrupt")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket: %v", err)
		}
		for key := range log.barrier {
			if strings.HasPrefix(key, snapshotPrefix) {
				log.barrier[key] = append([]byte("tampered"), log.barrier[key]...)
			}
		}
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside a corrupted blob: %v", err)
		}
		if restored.UsedTicket() {
			t.Fatal("a corrupted blob was trusted")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})

	t.Run("a deleted blob falls back and loses nothing", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "gone")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket: %v", err)
		}
		for key := range log.barrier {
			if strings.HasPrefix(key, snapshotPrefix) {
				delete(log.barrier, key)
			}
		}
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside a ticket whose blob is gone: %v", err)
		}
		if restored.UsedTicket() {
			t.Fatal("a ticket whose blob is gone was trusted")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})

	t.Run("a future schema falls back and loses nothing", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "future")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket: %v", err)
		}
		for key, body := range log.barrier {
			if !strings.HasPrefix(key, ticketPrefix) {
				continue
			}
			var filed map[string]any
			if err := json.Unmarshal(body, &filed); err != nil {
				t.Fatalf("parse the filed ticket: %v", err)
			}
			filed["schema_version"] = 99
			edited, err := json.Marshal(filed)
			if err != nil {
				t.Fatalf("re-file the ticket: %v", err)
			}
			log.barrier[key] = edited
		}
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside a future schema: %v", err)
		}
		if restored.UsedTicket() {
			t.Fatal("a future schema was trusted")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})

	t.Run("a same-length blob with a wrong digest falls back and loses nothing", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "digest")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket: %v", err)
		}
		for key, body := range log.barrier {
			if !strings.HasPrefix(key, snapshotPrefix) || len(body) < 2 {
				continue
			}
			flipped := append([]byte(nil), body...)
			if flipped[0] == 123 {
				flipped[0] = 32
			} else {
				flipped[0] = 123
			}
			log.barrier[key] = flipped
		}
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside the wrong digest: %v", err)
		}
		if restored.UsedTicket() {
			t.Fatal("a blob that failed its digest was trusted")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})

	t.Run("a legacy schema-one ticket is refused by version", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "legacy")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket: %v", err)
		}
		for key, body := range log.barrier {
			if !strings.HasPrefix(key, ticketPrefix) {
				continue
			}
			log.barrier[key] = []byte(strings.Replace(string(body), "\"schema_version\":2", "\"schema_version\":1", 1))
		}
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside the legacy ticket: %v", err)
		}
		if restored.UsedTicket() {
			t.Fatal("a schema-one ticket was trusted by a schema-two build")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})

	t.Run("a blob carrying a foreign schema falls back and loses nothing", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "blob-schema")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket: %v", err)
		}
		var edited []byte
		for key, body := range log.barrier {
			if !strings.HasPrefix(key, snapshotPrefix) {
				continue
			}
			var image map[string]any
			if err := json.Unmarshal(body, &image); err != nil {
				t.Fatalf("parse the snapshot image: %v", err)
			}
			image["schema_version"] = 99
			edited, err = json.Marshal(image)
			if err != nil {
				t.Fatalf("re-render the image: %v", err)
			}
			log.barrier[key] = edited
		}
		if edited == nil {
			t.Fatal("the seeded log held no snapshot blob")
		}
		sum := sha256.Sum256(edited)
		digest := "sha256:" + hex.EncodeToString(sum[:])
		for key, body := range log.barrier {
			if !strings.HasPrefix(key, ticketPrefix) {
				continue
			}
			var fields map[string]any
			if err := json.Unmarshal(body, &fields); err != nil {
				t.Fatalf("parse the filed ticket: %v", err)
			}
			fields["digest"] = digest
			fields["length"] = len(edited)
			refile, err := json.Marshal(fields)
			if err != nil {
				t.Fatalf("re-render the ticket: %v", err)
			}
			log.barrier[key] = refile
		}
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside the foreign blob schema: %v", err)
		}
		if restored.UsedTicket() {
			t.Fatal("a blob whose schema this build does not restore was trusted")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})

	t.Run("a ticket beside an empty log restores by pure delta", func(t *testing.T) {
		log := newInvariantFactLog()
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket: %v", err)
		}
		seedCarrier(t, ctx, log, taskID, "after-empty")
		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside the early-empty ticket: %v", err)
		}
		if got := len(restored.Facts(taskID)); got != 2 {
			t.Fatalf("facts after the delta merge = %d, want the two committed records", got)
		}
	})
}

// TestConsumptionAfterSnapshotNotReOwed pins post-snapshot consumption stays discharged.
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestConsumptionAfterSnapshotNotReOwed(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-consumed-after")
	seedCarrier(t, ctx, log, taskID, "seed")
	writer, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the writer: %v", err)
	}
	if err := writer.WriteRecoveryTicket(ctx); err != nil {
		t.Fatalf("file the ticket: %v", err)
	}
	second := invariantNotice(taskID, 2)
	if err := writer.recordConsumption(ctx, second); err != nil {
		t.Fatalf("consume after the snapshot: %v", err)
	}
	restored, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := restored.Outbox(taskID); len(got) != 0 {
		t.Fatalf("outbox after the restore = %v, want the discharged notice not re-owed", got)
	}
	bare, err := newBareCarrier(log)
	if err != nil {
		t.Fatalf("bare replay: %v", err)
	}
	if w := bare.Outbox(taskID); len(w) != 0 {
		t.Fatalf("bare outbox = %v, want empty beside the same receipts", w)
	}
}

// TestRecommitAfterTicketRestoreIsIdempotent pins recommit stays idempotent.
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestRecommitAfterTicketRestoreIsIdempotent(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-recommit")
	seedCarrier(t, ctx, log, taskID, "seed")
	writer, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the writer: %v", err)
	}
	if err := writer.WriteRecoveryTicket(ctx); err != nil {
		t.Fatalf("file the ticket: %v", err)
	}
	restored, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	stored := restored.Facts(taskID)
	if len(stored) != 2 {
		t.Fatalf("facts after the restore = %d, want the two seeded records", len(stored))
	}
	mutationsBefore := len(restored.mutations)
	if result, err := restored.Commit(ctx, stored[1]); err != nil || result != task.CommitCommitted {
		t.Fatalf("re-commit beside the restored mutations = (%v, %v), want the stored fact reported again", result, err)
	}
	if got := len(restored.Facts(taskID)); got != 2 {
		t.Fatalf("facts after the re-commit = %d, want the two stored records without a duplicate append", got)
	}
	if got := len(restored.mutations); got != mutationsBefore {
		t.Fatalf("mutations after the re-commit = %d, want %d unchanged", got, mutationsBefore)
	}
}

// TestSnapshotBesideConcurrentCommits pins snapshot under concurrent commits.
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestSnapshotBesideConcurrentCommits(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	writer, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the writer: %v", err)
	}
	const rounds = 50
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < rounds; i++ {
			taskID := event.TaskID(fmt.Sprintf("task-race-%d", i))
			if err := writer.Declare(ctx, taskID, task.Spec{}); err != nil {
				t.Errorf("declare %s: %v", taskID, err)
				return
			}
			record := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
			if _, err := writer.Commit(ctx, record); err != nil {
				t.Errorf("commit %s: %v", taskID, err)
				return
			}
		}
	}()
	for i := 0; i < 5; i++ {
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the ticket beside concurrent commits: %v", err)
		}
	}
	<-done
	restored, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("restore beside the concurrent writes: %v", err)
	}
	for i := 0; i < rounds; i++ {
		taskID := event.TaskID(fmt.Sprintf("task-race-%d", i))
		if got := len(restored.Facts(taskID)); got != 1 {
			t.Fatalf("facts of %s = %d, want the one committed record", taskID, got)
		}
	}
}

// namespaceFaultStore is the port decorator these checks inject failures
// through: it wraps an opened atomic store and refuses the reads a key
// namespace, so the injection sits at the store seam and runs unchanged over any
// engine.
type namespaceFaultStore struct {
	inner     ledger.AtomicStore
	namespace string
	err       error
}

// Read refuses a read that names the faulting namespace and passes the rest
// through to the wrapped engine.
func (s *namespaceFaultStore) Read(ctx context.Context, fetch ledger.Fetch) ([]ledger.Pair, error) {
	if strings.HasPrefix(fetch.Key, s.namespace+":") || strings.HasPrefix(fetch.Prefix, s.namespace+":") {
		return nil, s.err
	}
	return s.inner.Read(ctx, fetch)
}

// Keys enumerates the wrapped engine's keys.
func (s *namespaceFaultStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	return s.inner.Keys(ctx, prefix)
}

// Namespaces enumerates the wrapped engine's namespaces.
func (s *namespaceFaultStore) Namespaces(ctx context.Context) ([]string, error) {
	return s.inner.Namespaces(ctx)
}

// Commit passes the batch through to the wrapped engine.
func (s *namespaceFaultStore) Commit(ctx context.Context, batch ledger.Batch) error {
	return s.inner.Commit(ctx, batch)
}

// TestRecoveryFallsBackOnCorruptShard pins physical lazy-shard recovery.
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestRecoveryFallsBackOnCorruptShard(t *testing.T) {
	ctx := context.Background()
	for _, label := range []string{"tagent/task/v1/snapshot", "tagent/task/v1/ticket"} {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			store, err := ledger.OpenAtomicStore(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
			if err != nil {
				t.Fatalf("open the seed engine: %v", err)
			}
			faulty := &namespaceFaultStore{inner: store}
			cfg := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, Engine: faulty}
			tier, err := ledger.OpenTier(ctx, cfg)
			if err != nil {
				t.Fatalf("open the seed tier: %v", err)
			}
			log := ledger.TierFactLog(tier)
			taskID := event.TaskID("task-corrupt-shard")
			seedCarrier(t, ctx, log, taskID, "shard")
			writer, err := NewDurableCommit(log, nil)
			if err != nil {
				t.Fatalf("open the recovery writer: %v", err)
			}
			if err := writer.WriteRecoveryTicket(ctx); err != nil {
				t.Fatalf("file the recovery ticket: %v", err)
			}
			faulty.namespace = label
			faulty.err = fmt.Errorf("%w: injected on the %s read", ledger.ErrSnapshotCorrupt, label)
			reopened, err := ledger.OpenTier(ctx, cfg)
			if err != nil {
				t.Fatalf("tier opening still scans the corrupt recovery namespace: %v", err)
			}
			restored, err := NewDurableCommit(ledger.TierFactLog(reopened), nil)
			if err != nil {
				t.Fatalf("the corrupt %s namespace did not fall back to full replay: %v", label, err)
			}
			if restored.UsedTicket() {
				t.Fatalf("the corrupt %s namespace claimed a ticket restore", label)
			}
			bare, err := newBareCarrier(ledger.TierFactLog(reopened))
			if err != nil {
				t.Fatalf("full replay beside the corrupt shard: %v", err)
			}
			assertCarrierEqual(t, bare, restored, taskID)
		})
	}
}

// TestRecoveryCursorReadOrRemoved pins the one home of the recovery cursor.
//   - the snapshot image spells no cursor: the ticket beside it is the one spelling
//   - a blob written before the removal still restores, its cursor read past
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestRecoveryCursorReadOrRemoved(t *testing.T) {
	ctx := context.Background()
	taskID := event.TaskID("task-cursor")

	t.Run("the image carries no second cursor", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "cursor")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the recovery writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the recovery ticket: %v", err)
		}
		body, held, err := log.Get(ctx, snapshotCurrentKey)
		if err != nil || !held {
			t.Fatalf("read the snapshot image: held=%v err=%v", held, err)
		}
		var image map[string]json.RawMessage
		if err := json.Unmarshal(body, &image); err != nil {
			t.Fatalf("decode the snapshot image: %v", err)
		}
		if _, carried := image["cursor"]; carried {
			t.Fatalf("the snapshot image spells a cursor beside the ticket: %s", body)
		}
		cursor, _, _, _, ok, err := writer.latestTicket(ctx)
		if err != nil || !ok {
			t.Fatalf("read the recovery ticket: held=%v err=%v", ok, err)
		}
		if cursor == "" {
			t.Fatal("the recovery ticket names no coverage cursor")
		}
	})

	t.Run("a blob carrying a cursor still restores", func(t *testing.T) {
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, taskID, "legacy-cursor")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the recovery writer: %v", err)
		}
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the recovery ticket: %v", err)
		}
		body, held, err := log.Get(ctx, snapshotCurrentKey)
		if err != nil || !held {
			t.Fatalf("read the snapshot image: held=%v err=%v", held, err)
		}
		coverage, _, _, _, ok, err := writer.latestTicket(ctx)
		if err != nil || !ok {
			t.Fatalf("read the recovery ticket: held=%v err=%v", ok, err)
		}
		var image map[string]json.RawMessage
		if err := json.Unmarshal(body, &image); err != nil {
			t.Fatalf("decode the snapshot image: %v", err)
		}
		legacyCursor, err := json.Marshal(coverage)
		if err != nil {
			t.Fatalf("render the legacy cursor: %v", err)
		}
		image["cursor"] = legacyCursor
		legacy, err := json.Marshal(image)
		if err != nil {
			t.Fatalf("render the legacy snapshot image: %v", err)
		}
		if err := log.Put(ctx, snapshotCurrentKey, legacy); err != nil {
			t.Fatalf("store the legacy snapshot image: %v", err)
		}
		sum := sha256.Sum256(legacy)
		ticket := event.RecoveryTicketFor(recoverySchema, coverage, "sha256:"+hex.EncodeToString(sum[:]), int64(len(legacy)))
		if err := log.Put(ctx, ticketPrefix+ticket.FactKey(), ticket.Payload()); err != nil {
			t.Fatalf("file the legacy recovery ticket: %v", err)
		}
		filed, err := log.Scan(ctx, ticketPrefix)
		if err != nil {
			t.Fatalf("scan the filed recovery tickets: %v", err)
		}
		log.mu.Lock()
		for key := range filed {
			if key != ticketPrefix+ticket.FactKey() {
				delete(log.barrier, key)
			}
		}
		log.mu.Unlock()
		if err := log.Sync(ctx); err != nil {
			t.Fatalf("make the legacy ticket durable: %v", err)
		}

		restored, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("rebuild beside the legacy blob: %v", err)
		}
		if !restored.UsedTicket() {
			t.Fatal("the legacy blob carrying a cursor was not restored from its ticket")
		}
		bare, err := newBareCarrier(log)
		if err != nil {
			t.Fatalf("bare replay: %v", err)
		}
		assertCarrierEqual(t, bare, restored, taskID)
	})
}

// TestSnapshotImageIsClockPure pins the time purity of the recovery payload.
// - two carriers holding the same tables render the same image at different wall-clock moments
// - the ticket beside each one names the same coverage cursor
// - a boot-relative value frozen into the image would render two bodies for one carrier state, and the second boot would restore the first one's decision
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestSnapshotImageIsClockPure(t *testing.T) {
	ctx := context.Background()
	render := func(t *testing.T, wait time.Duration) ([]byte, string) {
		t.Helper()
		log := newInvariantFactLog()
		seedCarrier(t, ctx, log, event.TaskID("task-pure"), "pure")
		writer, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the recovery writer: %v", err)
		}
		time.Sleep(wait)
		if err := writer.WriteRecoveryTicket(ctx); err != nil {
			t.Fatalf("file the recovery ticket: %v", err)
		}
		body, held, err := log.Get(ctx, snapshotCurrentKey)
		if err != nil || !held {
			t.Fatalf("read the snapshot image: held=%v err=%v", held, err)
		}
		cursor, _, _, _, ok, err := writer.latestTicket(ctx)
		if err != nil || !ok {
			t.Fatalf("read the recovery ticket: held=%v err=%v", ok, err)
		}
		return body, cursor
	}
	first, firstCursor := render(t, 0)
	second, secondCursor := render(t, 20*time.Millisecond)
	if !bytes.Equal(first, second) {
		t.Fatalf("the same carrier state rendered two images across the clock:\n%s\n%s", first, second)
	}
	if firstCursor != secondCursor {
		t.Fatalf("cursor = %q then %q, want the coverage of the same tables", secondCursor, firstCursor)
	}
	if firstCursor == "" {
		t.Fatal("the ticket names no coverage cursor, so the comparison proves nothing")
	}
}

// TestAttemptAccountingSurvivesRestart pins the settle attempt a restart reads.
// - an attempt in flight and one that ended on a terminal both read back by notice identity
// - a notice nobody attempted still answers no attempt
// - re-recording an attempt the account already holds stages no second record and resets no terminal
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestAttemptAccountingSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	log := newInvariantFactLog()
	taskID := event.TaskID("task-attempt")
	seedCarrier(t, ctx, log, taskID, "attempt")
	writer, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the recovery writer: %v", err)
	}
	running := invariantNotice(taskID, 4)
	done := invariantNotice(taskID, 5)
	if err := writer.recordAttempt(ctx, running, SettleAttempt{}); err != nil {
		t.Fatalf("record the attempt in flight: %v", err)
	}
	if err := writer.recordAttempt(ctx, done, SettleAttempt{Terminal: "completed"}); err != nil {
		t.Fatalf("record the settled attempt: %v", err)
	}
	if err := writer.WriteRecoveryTicket(ctx); err != nil {
		t.Fatalf("file the recovery ticket: %v", err)
	}

	restored, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("restore the carrier: %v", err)
	}
	if !restored.UsedTicket() {
		t.Fatal("the rebuild beside a valid ticket did not use it")
	}
	mailbox := NewMemoryStore(nil, restored)
	held, found, err := mailbox.Attempt(ctx, running)
	if err != nil || !found {
		t.Fatalf("the attempt in flight = (%+v, %v, %v), want the record the ticket carried", held, found, err)
	}
	if held.Terminal != "" {
		t.Fatalf("the attempt in flight carries the terminal %q, want the turn still in flight", held.Terminal)
	}
	settled, found, err := mailbox.Attempt(ctx, done)
	if err != nil || !found {
		t.Fatalf("the settled attempt = (%+v, %v, %v), want the record the ticket carried", settled, found, err)
	}
	if settled.Terminal != "completed" {
		t.Fatalf("the settled attempt ended on %q, want completed", settled.Terminal)
	}
	if _, found, err := mailbox.Attempt(ctx, invariantNotice(taskID, 6)); err != nil || found {
		t.Fatalf("an un-attempted notice answered (%v, %v), want no attempt", found, err)
	}
	before := len(restored.attempts)
	if err := mailbox.RecordAttempt(ctx, done, SettleAttempt{Terminal: "completed"}); err != nil {
		t.Fatalf("re-record the settled attempt: %v", err)
	}
	if got := len(restored.attempts); got != before {
		t.Fatalf("the attempts table grew from %d to %d on a repeated attempt", before, got)
	}
	again, _, err := mailbox.Attempt(ctx, done)
	if err != nil || again.Terminal != "completed" {
		t.Fatalf("the re-recorded attempt = (%+v, %v), want the terminal it already carried", again, err)
	}
}

// BenchmarkRecoveryBootMeasure records the restart profile the carrier's
// coverage cursor is decided on: the cost of one boot beside a ticket as the
// log grows, split into the restore and the delta merge. A boot whose cost
// tracks the history is a boot the cursor has not narrowed yet, so the numbers
// here are what a resume-from-cursor or a single-WAL carrier would have to
// beat; no carrier is added before one is measured.
func BenchmarkRecoveryBootMeasure(b *testing.B) {
	ctx := context.Background()
	for _, facts := range []int{100, 1000, 4000} {
		b.Run(fmt.Sprintf("facts=%d", facts), func(b *testing.B) {
			log := newInvariantFactLog()
			carrier, err := NewDurableCommit(log, nil)
			if err != nil {
				b.Fatalf("open the carrier: %v", err)
			}
			taskID := event.TaskID("task-boot")
			for at := 0; at < facts; at++ {
				if result, err := carrier.Commit(ctx, invariantRecord(taskID, int64(at), task.StatusRunning)); err != nil || result != task.CommitCommitted {
					b.Fatalf("commit fact %d = (%v, %v)", at, result, err)
				}
			}
			if err := carrier.WriteRecoveryTicket(ctx); err != nil {
				b.Fatalf("file the recovery ticket: %v", err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				restored, err := NewDurableCommit(log, nil)
				if err != nil {
					b.Fatalf("boot beside the ticket: %v", err)
				}
				if !restored.UsedTicket() {
					b.Fatal("the boot beside a valid ticket did not use it")
				}
			}
		})
	}
}

// barrierFailureLog is the durable fact surface of one opened tier with its
// barrier replaced by a failure, so a check can pin what a commit reports when
// the barrier it reports success behind did not complete. The store itself is
// the real journal tier: only the barrier is injected.
type barrierFailureLog struct {
	FactLog
}

// Sync reports a barrier that did not complete without touching the store.
func (barrierFailureLog) Sync(context.Context) error { return ErrBarrierRefused }

// ErrBarrierRefused is the injected barrier failure of the checks below.
var ErrBarrierRefused = errors.New("state: the injected barrier did not complete")

// TestDurableCommitBarrier pins the commit barrier on the journal tier.
// - a commit behind a completed barrier survives a reopen of the same tier: the task fact, its spec and its owed notification all read back
// - a barrier that did not complete reports CommitUncertain and stores nothing the next carrier reads
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
		seedCarrier(t, ctx, ledger.TierFactLog(tier), taskID, "barrier")
		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen the journal tier: %v", err)
		}
		restored, err := NewDurableCommit(ledger.TierFactLog(reopened), nil)
		if err != nil {
			t.Fatalf("restore the carrier beside the committed facts: %v", err)
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
		carrier, err := NewDurableCommit(log, nil)
		if err != nil {
			t.Fatalf("open the carrier behind the failing barrier: %v", err)
		}
		taskID := event.TaskID("task-uncertain")
		if err := carrier.Declare(ctx, taskID, seededSpec("uncertain")); err == nil || !errors.Is(err, ErrBarrierRefused) {
			t.Fatalf("declare behind the failing barrier = %v, want the named barrier failure", err)
		}
		result, err := carrier.Commit(ctx, invariantRecord(taskID, 0, task.StatusRunning))
		if result != task.CommitUncertain || err == nil || !errors.Is(err, ErrBarrierRefused) {
			t.Fatalf("commit behind the failing barrier = (%v, %v), want the uncertain outcome beside the named failure", result, err)
		}
	})
}
