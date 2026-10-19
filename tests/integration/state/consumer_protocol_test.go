package state

import (
	"context"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"

	orgstate "github.com/SpellingDragon/tagent/runtime/state"
)

// TestSecondWaveConsumersShareTheProtocol pins the delivery consumer and the apply consumer to the shared consumer protocol.
// - the deliverer is offered the envelope frozen for the notice it reads, and a delivery advances the obligation without recording a second fact
// - a repeated flush of an accepted notice delivers no second time, and the committed fact chain keeps the identity it had
// - the apply consumer governs the very candidate plan it is handed and records the same fingerprint the plan carries
// - a candidate carrying the fingerprint already in effect publishes no new generation and commits no second record
// 契约: docs/EXTENSIONS.md#consumer-protocol
func TestSecondWaveConsumersShareTheProtocol(t *testing.T) {
	t.Run("the deliverer consumes the frozen envelope and records no second fact", func(t *testing.T) {
		ctx := context.Background()
		commit := task.NewMemoryCommit(nil)
		registry := task.NewMemoryRegistry(commit, task.NewMemoryAdmission())
		target := event.Address{AgentID: "agent-d", SessionID: "session-d"}
		source := event.Address{AgentID: "agent-d", SessionID: "session-source"}
		var delivered []task.Envelope
		manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(
			func(_ context.Context, envelope task.Envelope) (task.DeliveryAcceptance, error) {
				delivered = append(delivered, envelope)
				return task.DeliveryAccepted, nil
			}))

		receipt, err := manager.Admit(ctx, task.Spec{
			CallID:       "call-d",
			InvocationID: event.InvocationID("invocation-d"),
			InstanceID:   event.InstanceID("instance-d"),
			Source:       source,
			Target:       target,
		})
		if err != nil || !receipt.Accepted {
			t.Fatalf("admit the task = (%+v, %v), want one accepted task", receipt, err)
		}
		notice := task.Notice{
			TaskID: receipt.TaskID,
			Round:  1,
			Kind:   task.NoticeSettled,
			Seq:    0,
			Event:  event.EventID("fact-delivery"),
		}
		envelope := task.Envelope{
			Notice:         notice,
			Target:         target,
			Source:         source,
			TargetInstance: event.InstanceID("instance-d"),
		}
		result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled,
			task.MutationID("settle:"+string(receipt.TaskID)), []task.Envelope{envelope})
		if err != nil || result != task.CommitCommitted {
			t.Fatalf("commit the settled transition = (%v, %v), want the one recorded fact", result, err)
		}
		before := commit.Facts(receipt.TaskID)
		if len(before) != 2 || len(before[1].Outbox) != 1 || before[1].Outbox[0] != envelope {
			t.Fatalf("committed chain = %+v, want the settled fact carrying the frozen envelope", before)
		}

		if err := manager.Flush(ctx, receipt.TaskID); err != nil {
			t.Fatalf("flush the notice it owes: %v", err)
		}
		if len(delivered) != 1 || delivered[0] != envelope {
			t.Fatalf("delivered = %+v, want the envelope the notice was committed under (%+v)", delivered, envelope)
		}
		if delivered[0].Notice.FactKey() != notice.FactKey() || delivered[0].Notice.Event != notice.Event {
			t.Fatalf("delivered identity = %+v, want the notice key %q and the fact it points at unchanged", delivered[0].Notice, notice.FactKey())
		}
		if open, err := manager.Obligations(ctx, receipt.TaskID); err != nil || open.Count != 0 {
			t.Fatalf("obligations after the acceptance = (%+v, %v), want the delivery settled", open, err)
		}
		if err := manager.Flush(ctx, receipt.TaskID); err != nil {
			t.Fatalf("flush beside the settled obligation: %v", err)
		}
		if len(delivered) != 1 {
			t.Fatalf("deliveries = %d, want a retry to deliver no second time", len(delivered))
		}
		if after := commit.Facts(receipt.TaskID); !reflect.DeepEqual(after, before) {
			t.Fatalf("committed chain after delivery = %+v, want %+v unchanged", after, before)
		}
	})

	t.Run("the apply consumer governs the candidate it applies and publishes no second generation", func(t *testing.T) {
		ctx := context.Background()
		policy := &applyPolicy{answers: map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval": {Verdict: orgstate.CheckPass, Reason: "evidence collected", Scope: "org"},
		}}
		sink := &recordSink{}
		pub := orgstate.NewPublisher(orgstate.NewBindings(),
			orgstate.PublishContract{Required: []orgstate.CheckName{"eval"}}, policy, sink)

		leases := orgstate.NewLeases()
		cand := publicationCandidate(t, pub.Baseline(), "fp-apply", &stubModel{name: "gen-apply"}, leases)
		outcome, err := pub.Publish(ctx, cand)
		if err != nil || outcome.Result != orgstate.PublishApplied {
			t.Fatalf("publish the candidate = (%+v, %v), want one applied publication", outcome, err)
		}
		if policy.read.Plan.Fingerprint != cand.Plan.Plan.Fingerprint {
			t.Fatalf("governed plan fingerprint = %q, want the candidate identity %q it read",
				policy.read.Plan.Fingerprint, cand.Plan.Plan.Fingerprint)
		}
		if len(sink.records) != 1 || len(outcome.Publication.Fingerprint) == 0 {
			t.Fatalf("records = %+v, want the one record of the publication", sink.records)
		}
		record := sink.records[0]
		if record.Fingerprint != cand.Plan.Plan.Fingerprint || uint64(record.Generation) != outcome.Publication.Generation {
			t.Fatalf("record = (%q, %d), want the fingerprint it read and the generation applied (%q, %d)",
				record.Fingerprint, record.Generation, cand.Plan.Plan.Fingerprint, outcome.Publication.Generation)
		}
		if cand.Plan.Plan.Fingerprint != "fp-apply" {
			t.Fatalf("candidate fingerprint after the publication = %q, want the identity it carried", cand.Plan.Plan.Fingerprint)
		}

		repeat := publicationCandidate(t, pub.Baseline(), "fp-apply", &stubModel{name: "gen-apply"}, orgstate.NewLeases())
		again, err := pub.Publish(ctx, repeat)
		if err != nil || again.Result != orgstate.PublishUnchanged {
			t.Fatalf("publish the identical candidate = (%+v, %v), want no new generation", again, err)
		}
		if len(sink.records) != 1 || again.Publication.Generation != outcome.Publication.Generation {
			t.Fatalf("records/generation = (%d, %d), want the single record and generation (%d)",
				len(sink.records), again.Publication.Generation, outcome.Publication.Generation)
		}
	})
}

// applyPolicy is the governance stand-in of the apply case: it records the plan
// it was asked to govern, so the identity the consumer read is checkable.
type applyPolicy struct {
	read    orgstate.StatePlan
	answers map[orgstate.CheckName]orgstate.CheckAnswer
}

// Check reports the fixed dispositions beside the plan it read.
func (p *applyPolicy) Check(_ context.Context, plan orgstate.StatePlan) (map[orgstate.CheckName]orgstate.CheckAnswer, error) {
	p.read = plan
	return p.answers, nil
}
