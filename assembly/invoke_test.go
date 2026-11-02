package assembly

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// criterionAddress is the address the rest criterion cases read and deliver at.
var criterionAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// TestRunFenceReadsTheRunSequence pins that the fence a close judges under is the sequence every account of the run advances.
// - one write of any account moves it
// - a fence read from one account's own commit sequence stands still while the chain and the mailbox move
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestRunFenceReadsTheRunSequence(t *testing.T) {
	ctx := context.Background()
	run := &Run{movement: task.NewMovement()}
	at := run.fence(ctx)
	run.movement.Advance()
	if got := run.fence(ctx); got == at {
		t.Fatalf("fence after one write of an account = %d, want the run's own sequence to have moved past %d", got, at)
	}

	// A run assembled beside no sequence answers the zero every read of one instant
	// equals, so its criterion is judged by the account's own snapshot alone.
	var unsequenced Run
	if got := unsequenced.fence(ctx); got != 0 {
		t.Fatalf("fence of a run beside no sequence = %d, want 0", got)
	}
}

// TestCutApprovalWaitsLandsTheCancel pins that an approval wait no restart can rejoin is ended by name instead of being left as a wait.
// - the task lands cancelled with a fact, so the criterion stops listing it
// - a hold is memory of this process alone, so a wait left standing is work no later process can finish
// - a wait one of this process's holds still carries is left standing, because a ruling here resumes it
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestCutApprovalWaitsLandsTheCancel(t *testing.T) {
	ctx := context.Background()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
	receipt := parkedApprovalTask(t, ctx, registry, "call-approval", "inv-approval")
	held := parkedApprovalTask(t, ctx, registry, "call-approval-held", "inv-approval-held")

	run := &Run{registry: registry}
	book := task.NewHoldBook()
	book.Record(task.Hold{TaskID: held, Reason: "a call waiting for a ruling", ApprovalID: "approval-held"})
	run.hold(book)

	cut := run.cutApprovalWaits(ctx, task.Unsettled{Work: []task.UnsettledWork{
		{TaskID: receipt, Status: task.StatusAwaitingApproval, Address: criterionAddress},
		{TaskID: held, Status: task.StatusAwaitingApproval, Address: criterionAddress},
	}})
	if len(cut) != 1 || cut[0].TaskID != receipt || cut[0].Reason != reasonApprovalCut {
		t.Fatalf("cancelled approval waits = %+v, want the waiting task no hold carries named beside the reason it was cut", cut)
	}
	if _, status, err := registry.Lookup(ctx, receipt); err != nil || status != task.StatusCancelled {
		t.Fatalf("the task after the close = (%v, %v), want it cancelled as the fact the chain holds", status, err)
	}
	if _, status, err := registry.Lookup(ctx, held); err != nil || status != task.StatusAwaitingApproval {
		t.Fatalf("the task a live hold waits on = (%v, %v), want the wait a ruling here can still resume", status, err)
	}
	if unfinished, err := registry.Unfinished(ctx); err != nil || len(unfinished) != 1 || unfinished[0].TaskID != held {
		t.Fatalf("unfinished work after the close = (%+v, %v), want the held wait alone", unfinished, err)
	}
}

// parkedApprovalTask admits one task into the registry given and moves it to the
// approval wait every case of the cut reads, so the cases read one account.
func parkedApprovalTask(t *testing.T, ctx context.Context, registry *task.MemoryRegistry, callID, invocation string) event.TaskID {
	t.Helper()
	receipt, err := registry.Register(ctx, task.Spec{
		CallID:       callID,
		DedupKey:     task.DedupKey("dedup-" + callID),
		InvocationID: event.InvocationID(invocation),
		InstanceID:   "instance-a",
		Description:  "one call that waits for approval",
		Source:       criterionAddress,
		Target:       criterionAddress,
	})
	if err != nil {
		t.Fatalf("admit the waiting task: %v", err)
	}
	for _, step := range []struct {
		to       task.Status
		mutation task.MutationID
	}{
		{task.StatusRunning, "start"},
		{task.StatusAwaitingApproval, "park"},
	} {
		if result, err := registry.Advance(ctx, receipt.TaskID, step.to, step.mutation, nil); err != nil || result != task.CommitCommitted {
			t.Fatalf("move the task to %v = (%v, %v), want a committed change", step.to, result, err)
		}
	}
	return receipt.TaskID
}

// TestDeliverAnswersRejectionAndRoutingApart pins that a target which refused a notification is a rejection and a route the mailbox cannot open is not.
// - a refusal is the consumption result of its notification and answers rejected
// - an unestablished address is work still owed and answers pending with its reason recorded
// - a routing failure reported as a rejection withdraws a notification nobody answered
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliverAnswersRejectionAndRoutingApart(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemoryStore(state.NewAuthority())
	if _, err := store.Establish(ctx, criterionAddress, "instance-a"); err != nil {
		t.Fatalf("establish the target site: %v", err)
	}
	if err := store.TrackDelivery(ctx, "task-1", task.HandoffTarget{
		Target:         criterionAddress,
		TargetInstance: "instance-a",
	}); err != nil {
		t.Fatalf("register the delivery route: %v", err)
	}
	run := &Run{store: store}

	if outcome, err := run.deliver(ctx, progressEnvelope(criterionAddress, "task-1", 1)); err != nil || outcome != task.DeliveryAccepted {
		t.Fatalf("delivery at an open site = (%v, %v), want it accepted", outcome, err)
	}
	if err := store.CloseSession(ctx, criterionAddress); err != nil {
		t.Fatalf("close the target session: %v", err)
	}
	outcome, err := run.deliver(ctx, progressEnvelope(criterionAddress, "task-1", 2))
	if outcome != task.DeliveryRejected || !errors.Is(err, state.ErrDeliveryRefused) {
		t.Fatalf("delivery at a closed target = (%v, %v), want a rejection beside the named refusal %v", outcome, err, state.ErrDeliveryRefused)
	}

	unrouted := event.Address{AgentID: "agent-never", SessionID: "session-never"}
	outcome, err = run.deliver(ctx, progressEnvelope(unrouted, "task-2", 1))
	if outcome != task.DeliveryPending || err == nil {
		t.Fatalf("delivery at an unestablished address = (%v, %v), want pending beside the named routing failure", outcome, err)
	}
	if errors.Is(err, state.ErrDeliveryRefused) {
		t.Fatalf("delivery at an unestablished address = %v, want a routing failure that is not a refusal: nobody refused it", err)
	}
}

// progressEnvelope is one notification of the named task at the round and sequence
// given, routed at the target address beside which the delivery cases run.
func progressEnvelope(target event.Address, taskID event.TaskID, seq int64) task.Envelope {
	return task.Envelope{
		Notice:         task.Notice{TaskID: taskID, Round: 1, Kind: task.NoticeProgress, Seq: seq, Event: "fact-progress"},
		Target:         target,
		Source:         criterionAddress,
		TargetInstance: "instance-a",
	}
}
