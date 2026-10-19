package execution

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"
)

// TestSecondWaveConsumersShareTheProtocol pins the close consumer to the shared consumer protocol.
// - the close reads the obligations of the task it admitted and offers the notice beside the envelope that was committed for it
// - the delivered envelope keeps the notice key and the fact identity the committed outbox froze, and the acceptance settles the obligation
// - the close records no second task fact: the committed chain and the task status stay exactly as the close read them
// 契约: docs/EXTENSIONS.md#consumer-protocol
func TestSecondWaveConsumersShareTheProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	instance := event.InstanceID("instance-protocol")
	target := event.Address{AgentID: "agent-c", SessionID: "session-c"}
	source := event.Address{AgentID: "agent-c", SessionID: "session-source"}
	commit := task.NewMemoryCommit(nil)
	registry := task.NewMemoryRegistry(commit, task.NewMemoryAdmission())
	var delivered []task.NoticeEnvelope
	manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(
		func(_ context.Context, envelope task.NoticeEnvelope) (task.DeliveryAcceptance, error) {
			delivered = append(delivered, envelope)
			return task.DeliveryAccepted, nil
		}))
	shutdown := agent.NewShutdown(&delegationFace{MemoryManager: manager, MemoryRegistry: registry})

	receipt, err := shutdown.Admit(ctx, task.Spec{
		CallID:       "call-c",
		InvocationID: event.InvocationID("invocation-c"),
		InstanceID:   instance,
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
		Event:  event.EventID("fact-close"),
	}
	envelope := task.NoticeEnvelope{
		Notice:         notice,
		Target:         target,
		Source:         source,
		TargetInstance: instance,
	}
	result, err := shutdown.Advance(ctx, receipt.TaskID, task.StatusSettled,
		task.MutationID("settle:"+string(receipt.TaskID)), []task.NoticeEnvelope{envelope})
	if err != nil || result != task.CommitCommitted {
		t.Fatalf("commit the settled transition = (%v, %v), want the one recorded fact", result, err)
	}
	open, err := shutdown.Obligations(ctx, receipt.TaskID)
	if err != nil || open.Count != 1 || open.Open[0].Kind != task.ObligationAwaitingDelivery {
		t.Fatalf("obligations read by the close = (%+v, %v), want the delivery it still owes", open, err)
	}

	declared, status, err := registry.Lookup(ctx, receipt.TaskID)
	if err != nil {
		t.Fatalf("read the task the close settles: %v", err)
	}
	before := commit.Facts(receipt.TaskID)
	if len(before) != 2 || len(before[1].Outbox) != 1 || before[1].Outbox[0] != envelope {
		t.Fatalf("committed chain = %+v, want the settled fact carrying the frozen envelope", before)
	}

	report, err := shutdown.Close(ctx, agent.CloseRequest{
		Address:  target,
		Deadline: time.Now().Add(10 * time.Second),
	})
	if err != nil || !report.Drained || len(report.Unsettled) != 0 {
		t.Fatalf("close = (%+v, %v), want the drain beside the accepted delivery", report, err)
	}
	if len(delivered) != 1 || delivered[0] != envelope {
		t.Fatalf("delivered = %+v, want the envelope the outbox committed (%+v)", delivered, envelope)
	}
	if delivered[0].Notice.FactKey() != notice.FactKey() || delivered[0].Notice.Event != notice.Event {
		t.Fatalf("delivered identity = %+v, want the notice key %q and the fact it points at unchanged",
			delivered[0].Notice, notice.FactKey())
	}
	if after := commit.Facts(receipt.TaskID); !reflect.DeepEqual(after, before) {
		t.Fatalf("committed chain after the close = %+v, want %+v unchanged", after, before)
	}
	declaredAfter, statusAfter, err := registry.Lookup(ctx, receipt.TaskID)
	if err != nil || declaredAfter != declared || statusAfter != status {
		t.Fatalf("task after the close = (%+v, %v, %v), want the declared identity and status it read",
			declaredAfter, statusAfter, err)
	}
	if settled, err := shutdown.Obligations(ctx, receipt.TaskID); err != nil || settled.Count != 0 {
		t.Fatalf("obligations after the close = (%+v, %v), want the delivery settled", settled, err)
	}
}
