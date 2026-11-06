package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// seedCarrier drives one real write set with non-zero content on every table:
// specs with identities, two commits carrying outboxes, one consumption, one
// attempt and one refusal with its routing and moment frozen.
func seedCarrier(t *testing.T, ctx context.Context, log *invariantFactLog, taskID event.TaskID, tag string) {
	t.Helper()
	carrier, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the seeding carrier: %v", err)
	}
	spec := task.Spec{
		CallID:        "call-" + tag,
		DedupKey:      task.DedupKey("dedup-" + tag),
		InvocationID:  event.InvocationID("inv-" + tag),
		InstanceID:    event.InstanceID("inst-" + tag),
		Description:   "the seeded task " + tag,
		Source:        event.Address{AgentID: "agent-a", SessionID: event.SessionID("session-" + tag)},
		Target:        event.Address{AgentID: "agent-b", SessionID: event.SessionID("session-" + tag)},
		TriggerSource: "user",
	}
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
//   - no ticket / digest failure / future schema / early-empty ticket
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
