// Package evolution holds the integration tests of the evolution cluster over
// the publisher and the structural gate surfaces.
package evolution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"

	"github.com/SpellingDragon/tagent/features/evolution/meditation"
)

// TestConsumersShareTheProtocol drives both in-process consumers over one fact each and checks the protocol they share: read, consume, produce.
// - the turn-driven consumer reads the one ready fact and commits the attempt record its evidence face admitted
// - the time-driven consumer reads the observed fact and fires one reflection input fact beside it
// - both cite the identity they read on what they produce, and neither rewrites the fact it read
// 契约: docs/EXTENSIONS.md#consumer-protocol
func TestConsumersShareTheProtocol(t *testing.T) {
	t.Run("the turn-driven consumer reads one fact and produces new facts beside it", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		rig := newReflectionRig(t, reflectionConfig(), t.TempDir())
		source := externalFact("turn-source", "the fact the turn consumes", time.Unix(0, 0))
		sourceKey := rig.ledger.record(source, rig.partition)
		before := rig.ledger.snapshot()
		records := &consumerRecords{ledger: rig.ledger, partition: rig.partition}
		rig.input.loop.SetCapture(rl.NewRecorderAdapter(records))

		turn, err := rig.input.loop.Drive(ctx, agent.Invocation{
			InvocationID: event.InvocationID("invocation-turn"),
			Address:      rig.address,
			Instance:     event.InstanceID("instance-reflect"),
			Ready:        []event.Event{source},
		})
		if err != nil {
			t.Fatalf("drive the turn over the fact it read: %v", err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %q, want the completed terminal of its one decision", turn.Terminal)
		}

		requests := rig.model.recorded()
		if len(requests) != 1 || !carriesContent(requests[0], source.Message.Content) {
			t.Fatalf("requests = %+v, want the one decision carrying the fact it read", requests)
		}

		decisions, attempts := records.produced()
		if len(decisions) != 0 || len(attempts) != 1 {
			t.Fatalf("records = (%d decisions, %d attempts), want the one attempt admission the loop keeps", len(decisions), len(attempts))
		}
		if attempts[0].DecisionID != turn.Decisions[0].DecisionID || attempts[0].Terminal != model.KindCompleted {
			t.Fatalf("attempt = (%s, %v), want it to belong to decision %s with the reduced terminal", attempts[0].DecisionID, attempts[0].Terminal, turn.Decisions[0].DecisionID)
		}
		assertFactProduced(t, rig.ledger, before, sourceKey, event.EventID(attempts[0].ID))
		attemptFact, err := rig.ledger.CommittedFact(ctx, event.EventID(attempts[0].ID))
		if err != nil {
			t.Fatalf("read the attempt fact: %v", err)
		}
		if !strings.Contains(attemptFact.Message.Content, string(attempts[0].DecisionID)) {
			t.Fatalf("attempt fact = %q, want it citing the decision it was admitted under (%s)", attemptFact.Message.Content, attempts[0].DecisionID)
		}
	})

	t.Run("the time-driven consumer reads one fact and fires one judgement fact beside it", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		rig := newReflectionRig(t, reflectionConfig(), t.TempDir())
		observed := time.Unix(0, 0).Add(2 * time.Minute)
		source := externalFact("tick-source", "the fact the tick reflects on", observed)
		sourceKey := rig.ledger.record(source, rig.partition)
		before := rig.ledger.snapshot()

		outcome := rig.reflection.Tick(ctx, observed.Add(time.Minute))
		if outcome.Decision.Disposition != meditation.TickFired || outcome.Injection == nil {
			t.Fatalf("tick = (%q, %s), want the read fact firing one judgement fact", outcome.Decision.Disposition, outcome.Decision.Reason)
		}
		if outcome.Scan == nil || outcome.Scan.Verdict.Recent == nil || outcome.Scan.Verdict.Recent.Identity.Key != sourceKey {
			t.Fatalf("scan = %+v, want the judged pass citing the fact it read (%s)", outcome.Scan, sourceKey)
		}
		assertFactProduced(t, rig.ledger, before, sourceKey, outcome.Injection.Key)

		produced, err := rig.ledger.CommittedFact(ctx, outcome.Injection.Key)
		if err != nil {
			t.Fatalf("read the judgement fact: %v", err)
		}
		if produced.Type != event.TypeExternalInput || produced.Meta[event.MetaKeyTriggerSource] != meditation.LineageReflection {
			t.Fatalf("judgement fact = (%q, %q), want a reflection input on the chain", produced.Type, produced.Meta[event.MetaKeyTriggerSource])
		}
	})
}

// assertFactProduced checks the read → produce closure both consumers share: the
// produced fact is a new identity committed to the same chain, and the fact they
// read keeps the identity and content it had.
func assertFactProduced(t *testing.T, ledger *reflectionLedger, before map[event.EventID]event.Event, read, produced event.EventID) {
	t.Helper()
	if produced == "" || produced == read {
		t.Fatalf("produced fact = %q, want a new identity beside the fact it read (%s)", produced, read)
	}
	if _, err := ledger.CommittedFact(context.Background(), produced); err != nil {
		t.Fatalf("produced fact %q is not committed to the chain: %v", produced, err)
	}
	if !ledger.unchanged(before, read) {
		t.Fatalf("the consumer rewrote the fact it read: %q", read)
	}
}

// carriesContent reports whether one assembled request carries the content of the
// fact the consumer read.
func carriesContent(req model.Request, content string) bool {
	for _, msg := range req.Messages {
		if msg.Content == content {
			return true
		}
	}
	return false
}

// consumerRecords is the training record production face of the turn-driven case:
// it commits every record it is handed as a fact of the same chain the consumer
// reads, keyed by the identity the record was produced under.
type consumerRecords struct {
	ledger    *reflectionLedger
	partition int

	mu        sync.Mutex
	decisions []rl.DecisionRecord
	attempts  []rl.AttemptRecord
}

// RecordEpisode commits one episode record as the fact it produced.
func (c *consumerRecords) RecordEpisode(_ context.Context, record rl.EpisodeRecord) error {
	c.ledger.record(recordFact(event.EventID(record.ID), "training_episode", record.Timestamp, "episode "+string(record.ID)), c.partition)
	return nil
}

// RecordDecision keeps one decision record and commits it as the fact it produced,
// carrying the input identities the decision read.
func (c *consumerRecords) RecordDecision(_ context.Context, record rl.DecisionRecord) error {
	c.mu.Lock()
	c.decisions = append(c.decisions, record)
	c.mu.Unlock()

	inputs := make([]string, 0, len(record.InputEvents))
	for _, id := range record.InputEvents {
		inputs = append(inputs, string(id))
	}
	body := "decision " + string(record.ID) + " over " + strings.Join(inputs, ",")
	c.ledger.record(recordFact(event.EventID(record.ID), "training_decision", record.Timestamp, body), c.partition)
	return nil
}

// RecordAttempt keeps one attempt record and commits it as the fact it produced.
func (c *consumerRecords) RecordAttempt(_ context.Context, record rl.AttemptRecord) error {
	c.mu.Lock()
	c.attempts = append(c.attempts, record)
	c.mu.Unlock()

	body := fmt.Sprintf("attempt %s of decision %s terminal %d", record.ID, record.DecisionID, record.Terminal)
	c.ledger.record(recordFact(event.EventID(record.ID), "training_attempt", record.Timestamp, body), c.partition)
	return nil
}

// produced reports the decision and attempt records collected so far.
func (c *consumerRecords) produced() ([]rl.DecisionRecord, []rl.AttemptRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]rl.DecisionRecord(nil), c.decisions...), append([]rl.AttemptRecord(nil), c.attempts...)
}

// recordFact builds the fact one production record commits under: the record's own
// identity, the frozen type of its family and the body the record produced.
func recordFact(id event.EventID, kind string, at time.Time, body string) event.Event {
	return event.Event{
		Key:       id,
		Type:      kind,
		Timestamp: at,
		Meta:      map[string]string{event.MetaKeyEventSummary: body},
		Message:   model.Message{Role: model.RoleAssistant, Content: body},
	}
}

// The production face of the turn-driven consumer is the training record face the
// real loop is handed.
var _ rl.Recorder = (*consumerRecords)(nil)
