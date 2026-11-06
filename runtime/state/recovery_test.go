package state

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// seedCarrier drives one real write set: declare, two commits, one consumption
// receipt — the prefixes that carry records beside the facts.
func seedCarrier(t *testing.T, ctx context.Context, log *invariantFactLog, taskID event.TaskID, tag string) {
	t.Helper()
	carrier, err := NewDurableCommit(log, nil)
	if err != nil {
		t.Fatalf("open the seeding carrier: %v", err)
	}
	if err := carrier.Declare(ctx, taskID, task.Spec{}); err != nil {
		t.Fatalf("declare %s: %v", tag, err)
	}
	first := invariantRecord(taskID, 0, task.StatusRunning, invariantNotice(taskID, 1))
	if result, err := carrier.Commit(ctx, first); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the first fact of %s = (%v, %v)", tag, result, err)
	}
	second := invariantRecord(taskID, 1, task.StatusSettled, invariantNotice(taskID, 2))
	if result, err := carrier.Commit(ctx, second); err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the second fact of %s = (%v, %v)", tag, result, err)
	}
	if err := carrier.recordConsumption(ctx, first.Outbox[0].Notice); err != nil {
		t.Fatalf("record the consumption of %s: %v", tag, err)
	}
}

// assertCarrierEqual compares two rebuilt carriers on every observable face.
func assertCarrierEqual(t *testing.T, want, got *DurableCommit, taskIDs ...event.TaskID) {
	t.Helper()
	for _, taskID := range taskIDs {
		if w, g := len(want.Facts(taskID)), len(got.Facts(taskID)); w != g {
			t.Fatalf("facts of %s = %d, want %d", taskID, g, w)
		}
		wantFacts, gotFacts := want.Facts(taskID), got.Facts(taskID)
		for i := range wantFacts {
			if wantFacts[i].MutationID != gotFacts[i].MutationID || wantFacts[i].Status != gotFacts[i].Status {
				t.Fatalf("fact %d of %s = %+v, want %+v", i, taskID, gotFacts[i], wantFacts[i])
			}
		}
		if w, g := want.Revision(taskID), got.Revision(taskID); w != g {
			t.Fatalf("revision of %s = %d, want %d", taskID, g, w)
		}
		wantOwed, gotOwed := want.Outbox(taskID), got.Outbox(taskID)
		if len(wantOwed) != len(gotOwed) {
			t.Fatalf("outbox of %s = %d entries, want %d", taskID, len(gotOwed), len(wantOwed))
		}
		for i := range wantOwed {
			if wantOwed[i].Delivery != gotOwed[i].Delivery {
				t.Fatalf("outbox %d of %s = %v, want %v", i, taskID, gotOwed[i].Delivery, wantOwed[i].Delivery)
			}
		}
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
