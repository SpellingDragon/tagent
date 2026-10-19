package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestTaskLifecycle checks the in-memory task registry and task manager over one lifecycle.
// - a fresh admission keeps its obligation open and a repeated admission adds none
// - an admission past the concurrency limit is refused by name and keeps what is admitted
// - a running task reports progress and settles once, and a repeated mutation stores one fact
// - a cancellation receipt is not termination evidence and a late result never revives a terminal task
// - the obligations a manager holds stay classified and queryable by kind
// - a committed notification freezes its delivery routing and a handoff moves only the notifications created after it
// - resume feeds one declared input under the same task identity and refuses a round already in flight
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	carrier := task.NewMemoryCommit(nil)
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(carrier, admission, task.WithConcurrencyLimit(1))
	refusalText := "the target refused the notification"
	deliveries := 0
	manager := task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		deliveries++
		if notice.Seq == 0 {
			return task.DeliveryRejected, errors.New(refusalText)
		}
		return task.DeliveryPending, nil
	})

	specA := task.Spec{
		CallID:       "call-a",
		DedupKey:     "dedup-a",
		InvocationID: "inv-1",
		InstanceID:   "instance-a",
		Description:  "compile the report",
		Source:       event.Address{AgentID: "agent-a", SessionID: "session-a"},
		Target:       event.Address{AgentID: "agent-a", SessionID: "session-a"},
		Execution:    "serial",
	}
	specB := specA
	specB.CallID = "call-b"
	specB.DedupKey = "dedup-b"
	specC := specA
	specC.CallID = "call-c"
	specC.DedupKey = "dedup-c"

	var taskA, taskB, taskC event.TaskID

	t.Run("a fresh admission keeps its obligation open", func(t *testing.T) {
		receipt, err := registry.Register(ctx, specA)
		if err != nil || !receipt.Accepted || receipt.Kind != task.AdmissionFresh || receipt.CallID != "call-a" || receipt.TaskID == "" {
			t.Fatalf("register = (%+v, %v), want one accepted fresh admission of call-a", receipt, err)
		}
		taskA = receipt.TaskID
		if _, status, err := registry.Lookup(ctx, taskA); err != nil || status != task.StatusAccepted {
			t.Fatalf("lookup = (%v, %v), want the admitted task accepted", status, err)
		}
		open, err := manager.Obligations(ctx, taskA)
		if err != nil {
			t.Fatalf("obligations: %v", err)
		}
		if open.Count != 1 || open.Owner != specA.InvocationID {
			t.Fatalf("obligations = %+v, want one obligation owned by %s", open, specA.InvocationID)
		}
		if open.Open[0].Kind != task.ObligationAwaitingTool || open.Open[0].TaskID != taskA || open.Open[0].Notice != (task.Notice{}) {
			t.Fatalf("obligation = %+v, want the execution obligation of %s", open.Open[0], taskA)
		}
		if open.ExitReason != "" || len(open.Blocked) != 0 {
			t.Fatalf("obligations = %+v, want no exit reason and nothing blocked", open)
		}

		repeat, err := registry.Register(ctx, specA)
		if err != nil || !repeat.Accepted || repeat.Kind != task.AdmissionDedup || repeat.TaskID != taskA {
			t.Fatalf("repeat register = (%+v, %v), want a dedup reporting %s", repeat, err, taskA)
		}
		if open, err = manager.Obligations(ctx, taskA); err != nil || open.Count != 1 {
			t.Fatalf("obligations = (%+v, %v), want the obligation of the kept admission only", open, err)
		}
	})

	t.Run("an admission past the limit is refused by name", func(t *testing.T) {
		receipt, err := registry.Register(ctx, specB)
		if !errors.Is(err, task.ErrConcurrencyLimit) {
			t.Fatalf("register err = %v, want %v", err, task.ErrConcurrencyLimit)
		}
		if receipt.Accepted || receipt.Kind != task.AdmissionRejected || receipt.TaskID != "" {
			t.Fatalf("receipt = %+v, want one refused admission", receipt)
		}
		if _, status, err := registry.Lookup(ctx, taskA); err != nil || status != task.StatusAccepted {
			t.Fatalf("lookup = (%v, %v), want the admitted task kept", status, err)
		}
		if open, err := manager.Obligations(ctx, taskA); err != nil || open.Count != 1 {
			t.Fatalf("obligations = (%+v, %v), want the admitted obligation kept", open, err)
		}
	})

	t.Run("progress reports and settlement stores one fact", func(t *testing.T) {
		if result, err := registry.Advance(ctx, taskA, task.StatusRunning, "run-a", nil); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want task-a running", result, err)
		}
		if result, err := registry.Advance(ctx, taskA, task.StatusUnknown, "unknown-a", nil); result != task.CommitNotCommitted || !errors.Is(err, task.ErrStatusUnknown) {
			t.Fatalf("advance = (%v, %v), want the unknown status refused", result, err)
		}
		progress := task.Notice{TaskID: taskA, Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "evt-progress-a"}
		progressEnvelope := task.Envelope{Notice: progress, Target: specA.Target, Source: specA.Source, TargetInstance: specA.InstanceID}
		if result, err := registry.Advance(ctx, taskA, task.StatusProgress, "progress-a", []task.Envelope{progressEnvelope}); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want task-a reporting progress", result, err)
		}
		if open, err := manager.Obligations(ctx, taskA); err != nil || open.Count != 2 {
			t.Fatalf("obligations = (%+v, %v), want the execution obligation beside the notification owed", open, err)
		} else if open.Open[0].Kind != task.ObligationAwaitingTool || open.Open[1].Notice != progress {
			t.Fatalf("obligations = %+v, want the execution obligation beside the progress notification owed", open.Open)
		}

		settled := task.Notice{TaskID: taskA, Round: 1, Kind: task.NoticeSettled, Seq: 0, Event: "evt-settled-a"}
		outbox := []task.Envelope{{Notice: settled, Target: specA.Target, Source: specA.Source, TargetInstance: specA.InstanceID}}
		if result, err := registry.Advance(ctx, taskA, task.StatusSettled, "settle-a", outbox); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want task-a settled", result, err)
		}
		if _, status, err := registry.Lookup(ctx, taskA); err != nil || status != task.StatusSettled {
			t.Fatalf("lookup = (%v, %v), want task-a settled", status, err)
		}
		if open, err := manager.Obligations(ctx, taskA); err != nil || open.Count != 2 {
			t.Fatalf("obligations = (%+v, %v), want the execution obligation closed beside two notifications owed", open, err)
		} else if open.Open[0].Notice != progress || open.Open[1].Notice != settled {
			t.Fatalf("obligations = %+v, want the two notifications still owed", open.Open)
		}

		if result, err := registry.Advance(ctx, taskA, task.StatusSettled, "settle-a", outbox); result != task.CommitCommitted || err != nil {
			t.Fatalf("repeated settle = (%v, %v), want the fact already stored", result, err)
		}
		facts := 0
		for _, record := range carrier.Facts(taskA) {
			if record.MutationID == "settle-a" {
				facts++
			}
		}
		if facts != 1 {
			t.Fatalf("stored %d settle facts, want the one stored fact", facts)
		}
		if open, err := manager.Obligations(ctx, taskA); err != nil || open.Count != 2 {
			t.Fatalf("obligations = (%+v, %v), want no obligation added by the repeated settlement", open, err)
		}

		if result, err := registry.Advance(ctx, taskA, task.StatusFailed, "late-a", nil); result != task.CommitNotCommitted || !errors.Is(err, task.ErrTerminalState) {
			t.Fatalf("late change = (%v, %v), want the terminal fact kept", result, err)
		}
		if _, status, _ := registry.Lookup(ctx, taskA); status != task.StatusSettled {
			t.Fatalf("status = %v, want settled kept", status)
		}
	})

	t.Run("a cancellation receipt is not termination evidence", func(t *testing.T) {
		receipt, err := registry.Register(ctx, specB)
		if err != nil || !receipt.Accepted || receipt.Kind != task.AdmissionFresh {
			t.Fatalf("register = (%+v, %v), want one fresh admission with the slot free", receipt, err)
		}
		taskB = receipt.TaskID
		if result, err := registry.Advance(ctx, taskB, task.StatusRunning, "run-b", nil); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want task-b running", result, err)
		}
		control, err := registry.RequestCancel(ctx, taskB)
		if err != nil || !control.Requested || control.TaskID != taskB {
			t.Fatalf("cancel receipt = (%+v, %v), want the request recorded", control, err)
		}
		if _, status, err := registry.Lookup(ctx, taskB); err != nil || status != task.StatusRunning {
			t.Fatalf("lookup = (%v, %v), want task-b running until an executor reports a stop", status, err)
		}
		again, err := registry.RequestCancel(ctx, taskB)
		if err != nil || !again.Requested || again.TaskID != taskB {
			t.Fatalf("repeated cancel receipt = (%+v, %v), want the same request recorded once", again, err)
		}
		if _, status, err := registry.Lookup(ctx, taskA); err != nil || status != task.StatusSettled {
			t.Fatalf("lookup = (%v, %v), want the cancel confined to task-b", status, err)
		}
		if err := manager.RecordStop(ctx, task.StopEvidence{TaskID: taskB, Stopped: false, Reason: "the producer would not stop"}); err != nil {
			t.Fatalf("record stop: %v", err)
		}
		if _, status, err := registry.Lookup(ctx, taskB); err != nil || status != task.StatusRunning {
			t.Fatalf("lookup = (%v, %v), want an unconfirmed stop to move nothing", status, err)
		}
	})

	t.Run("a terminal task is not revived", func(t *testing.T) {
		if err := manager.RecordStop(ctx, task.StopEvidence{TaskID: taskB, Stopped: true, Reason: "killed by the caller"}); err != nil {
			t.Fatalf("record stop: %v", err)
		}
		if _, status, err := registry.Lookup(ctx, taskB); err != nil || status != task.StatusCancelled {
			t.Fatalf("lookup = (%v, %v), want task-b cancelled after the confirmed stop", status, err)
		}
		open, err := manager.Obligations(ctx, taskB)
		if err != nil {
			t.Fatalf("obligations: %v", err)
		}
		if open.ExitReason != "killed by the caller" {
			t.Fatalf("exit reason = %q, want the reason the execution obligation ended", open.ExitReason)
		}
		if open.Count != 1 || open.Open[0].Kind != task.ObligationAwaitingDelivery || open.Open[0].Notice.Kind != task.NoticeCancelled {
			t.Fatalf("obligations = %+v, want the cancelled notification still owed", open.Open)
		}

		// The cancelled notification was committed beside the delivery routing
		// frozen at the stop: target, source and target instance are filled from
		// the declaration the task was admitted under.
		var cancelled []task.Envelope
		for _, record := range carrier.Facts(taskB) {
			for _, envelope := range record.Outbox {
				if envelope.Notice.Kind == task.NoticeCancelled {
					cancelled = append(cancelled, envelope)
				}
			}
		}
		if len(cancelled) != 1 {
			t.Fatalf("cancelled notifications = %+v, want exactly one beside the confirmed stop", cancelled)
		}
		if got := cancelled[0]; got.Target != specB.Target || got.Source != specB.Source || got.TargetInstance != specB.InstanceID {
			t.Fatalf("envelope = %+v, want the cancelled notification frozen beside the declaration of task-b", got)
		}
		if result, err := registry.Advance(ctx, taskB, task.StatusSettled, "late-b", nil); result != task.CommitNotCommitted || !errors.Is(err, task.ErrTerminalState) {
			t.Fatalf("late result = (%v, %v), want the terminal fact kept", result, err)
		}
		if _, status, _ := registry.Lookup(ctx, taskB); status != task.StatusCancelled {
			t.Fatalf("status = %v, want cancelled kept", status)
		}
		if _, err := registry.Resume(ctx, taskB, event.Event{Key: "evt-resume-b"}); !errors.Is(err, task.ErrResumeCancelled) {
			t.Fatalf("resume err = %v, want %v", err, task.ErrResumeCancelled)
		}
	})

	t.Run("obligations stay classified and queryable", func(t *testing.T) {
		receipt, err := manager.Admit(ctx, specC)
		if err != nil || !receipt.Accepted || receipt.Kind != task.AdmissionFresh {
			t.Fatalf("admit = (%+v, %v), want one accepted admission", receipt, err)
		}
		taskC = receipt.TaskID
		if result, err := registry.Advance(ctx, taskC, task.StatusRunning, "run-c", nil); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want task-c running", result, err)
		}
		first := task.Notice{TaskID: taskC, Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "evt-progress-c-0"}
		second := task.Notice{TaskID: taskC, Round: 1, Kind: task.NoticeProgress, Seq: 1, Event: "evt-progress-c-1"}
		firstEnvelope := task.Envelope{Notice: first, Target: specC.Target, Source: specC.Source, TargetInstance: specC.InstanceID}
		secondEnvelope := task.Envelope{Notice: second, Target: specC.Target, Source: specC.Source, TargetInstance: specC.InstanceID}
		if result, err := registry.Advance(ctx, taskC, task.StatusProgress, "progress-c", []task.Envelope{firstEnvelope, secondEnvelope}); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want task-c reporting progress", result, err)
		}
		if err := manager.Flush(ctx, taskC); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if deliveries != 2 {
			t.Fatalf("deliveries = %d, want the two notifications offered to the target", deliveries)
		}
		if err := manager.Handoff(ctx, taskC, task.HandoffTarget{Target: specC.Target, TargetInstance: "instance-b"}); err != nil {
			t.Fatalf("handoff: %v", err)
		}
		open, err := manager.Obligations(ctx, taskC)
		if err != nil {
			t.Fatalf("obligations: %v", err)
		}
		if open.Count != 3 {
			t.Fatalf("obligations = %+v, want three open obligations across the three classes", open)
		}
		byKind := map[task.ObligationKind]task.Obligation{}
		for _, obligation := range open.Open {
			if obligation.TaskID != taskC {
				t.Fatalf("obligation = %+v, want every entry to carry task-c", obligation)
			}
			byKind[obligation.Kind] = obligation
		}
		if len(byKind) != 3 {
			t.Fatalf("classes = %v, want the three obligation classes queryable", byKind)
		}
		if byKind[task.ObligationAwaitingTool].Notice != (task.Notice{}) {
			t.Fatalf("obligation = %+v, want the execution obligation to point at the task itself", byKind[task.ObligationAwaitingTool])
		}
		if byKind[task.ObligationAwaitingDelivery].Notice != first {
			t.Fatalf("obligation = %+v, want the refused notification kept beside its old target", byKind[task.ObligationAwaitingDelivery])
		}
		if byKind[task.ObligationHandedOff].Notice != second {
			t.Fatalf("obligation = %+v, want the unanswered notification handed to the new target", byKind[task.ObligationHandedOff])
		}
		if len(open.Blocked) != 1 || open.Blocked[0].Notice != first || open.Blocked[0].Reason != refusalText {
			t.Fatalf("blocked = %+v, want the refused notification beside its reason", open.Blocked)
		}

		for _, record := range carrier.Facts(taskC) {
			for _, envelope := range record.Outbox {
				if envelope.TargetInstance != specC.InstanceID {
					t.Fatalf("envelope = %+v, want the pre-handoff notification kept beside the frozen instance %q", envelope, specC.InstanceID)
				}
			}
		}
	})

	t.Run("resume feeds one declared input under the same identity", func(t *testing.T) {
		before := len(carrier.Facts(taskA))
		input := event.Event{Key: "evt-resume-a", TaskID: taskA, Type: event.TypeFeedback, InvocationID: specA.InvocationID, InstanceID: specA.InstanceID}
		receipt, err := registry.Resume(ctx, taskA, input)
		if err != nil || !receipt.Accepted || receipt.Kind != task.AdmissionFresh || receipt.TaskID != taskA {
			t.Fatalf("resume = (%+v, %v), want the same task identity re-entering its lifecycle", receipt, err)
		}
		if _, status, err := registry.Lookup(ctx, taskA); err != nil || status != task.StatusRunning {
			t.Fatalf("lookup = (%v, %v), want the standard lifecycle re-entered", status, err)
		}
		if open, err := manager.Obligations(ctx, taskA); err != nil || open.Count != 3 {
			t.Fatalf("obligations = (%+v, %v), want the reopened execution obligation beside the two owed", open, err)
		}

		again, err := registry.Resume(ctx, taskA, input)
		if err != nil || !again.Accepted || again.TaskID != taskA {
			t.Fatalf("repeated resume = (%+v, %v), want the same declaration accepted once", again, err)
		}
		if facts := len(carrier.Facts(taskA)); facts != before+1 {
			t.Fatalf("stored %d facts, want the one resume declaration stored", facts)
		}
		if open, err := manager.Obligations(ctx, taskA); err != nil || open.Count != 3 {
			t.Fatalf("obligations = (%+v, %v), want no obligation added by the repeated declaration", open, err)
		}

		if _, err := registry.Resume(ctx, taskC, event.Event{Key: "evt-resume-c"}); !errors.Is(err, task.ErrRoundInFlight) {
			t.Fatalf("resume err = %v, want %v", err, task.ErrRoundInFlight)
		}
	})
}
