package execution

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// removedOwners is an owner binding whose current orchestration keeps none of
// the targets it is asked about, so a restore refuses the re-delivery by name.
type removedOwners struct{}

func (removedOwners) OwnerOf(ctx context.Context, owner, target event.AgentID) (event.AgentID, error) {
	return "", task.ErrTargetRemoved
}

// TestHandoffFutureNotifications checks the handoff of one task's notifications across target and instance, beside retries:
// - a notification issued before the handoff keeps the old target its envelope froze
// - a notification issued after the handoff freezes the new target and instance
// - the source and the target instance claim their own notifications apart
// - an instance that closes keeps what it still owes, recoverable and undropped
// - a retry repeats the frozen routing and never re-resolves a current target
// - progress notifications carry stable ordered seqs and repeat transport records no second fact
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestHandoffFutureNotifications(t *testing.T) {
	ctx := context.Background()
	carrier := task.NewMemoryCommit(nil)
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(carrier, admission)

	oldTarget := event.Address{AgentID: "agent-recv", SessionID: "session-old"}
	newTarget := event.Address{AgentID: "agent-recv", SessionID: "session-new"}
	const sourceInstance event.InstanceID = "instance-source"
	const targetInstance event.InstanceID = "instance-target"

	var attempts []task.Envelope
	accept := func(envelope task.Envelope) (task.DeliveryAcceptance, error) {
		return task.DeliveryPending, nil
	}
	manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(
		func(ctx context.Context, envelope task.Envelope) (task.DeliveryAcceptance, error) {
			attempts = append(attempts, envelope)
			return accept(envelope)
		}))

	register := func(t *testing.T, name string, instance event.InstanceID, target event.Address) event.TaskID {
		t.Helper()
		spec := task.Spec{
			CallID:       "call-" + name,
			DedupKey:     task.DedupKey("dedup-" + name),
			InvocationID: event.InvocationID("inv-" + name),
			InstanceID:   instance,
			Description:  "hand the report of " + name + " over",
			Source:       event.Address{AgentID: "agent-src", SessionID: "session-src"},
			Target:       target,
			Execution:    "serial",
		}
		receipt, err := manager.Admit(ctx, spec)
		if err != nil || !receipt.Accepted || receipt.Kind != task.AdmissionFresh {
			t.Fatalf("admit %s = (%+v, %v), want one accepted fresh admission", name, receipt, err)
		}
		if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusRunning, task.MutationID("run-"+name), nil); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance %s = (%v, %v), want the task running", name, result, err)
		}
		return receipt.TaskID
	}

	issue := func(t *testing.T, taskID event.TaskID, fact event.EventID, mutation task.MutationID) task.Envelope {
		t.Helper()
		envelope, err := registry.IssueNotice(ctx, taskID, task.NoticeProgress, fact)
		if err != nil {
			t.Fatalf("issue notice: %v", err)
		}
		if result, err := registry.Advance(ctx, taskID, task.StatusRunning, mutation, []task.Envelope{envelope}); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want the notification committed beside its frozen routing", result, err)
		}
		return envelope
	}

	t.Run("a notification started before the handoff keeps its old target", func(t *testing.T) {
		attempts = nil
		taskID := register(t, "started", sourceInstance, oldTarget)
		if _, err := manager.HandoffOf(ctx, taskID); !errors.Is(err, task.ErrNoHandoff) {
			t.Fatalf("handoff record err = %v, want %v for a task no handoff moved", err, task.ErrNoHandoff)
		}
		first := issue(t, taskID, "evt-started-1", "progress-started-1")
		if first.Target != oldTarget || first.TargetInstance != sourceInstance {
			t.Fatalf("envelope = %+v, want the pre-handoff notification frozen beside %v / %s", first, oldTarget, sourceInstance)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if len(attempts) != 1 || attempts[0] != first {
			t.Fatalf("attempts = %+v, want the one owed notification offered beside its frozen routing", attempts)
		}
		if err := manager.Handoff(ctx, taskID, task.HandoffTarget{Target: newTarget, TargetInstance: targetInstance}); err != nil {
			t.Fatalf("handoff: %v", err)
		}
		attempts = nil
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush after the handoff: %v", err)
		}
		if len(attempts) != 1 {
			t.Fatalf("retried %d notifications, want the one still owed offered again", len(attempts))
		}
		if attempts[0] != first {
			t.Fatalf("retry envelope = %+v, want the started notification kept beside the old target %+v and never re-routed", attempts[0], first)
		}
	})

	t.Run("a future notification moves to the new target and instance", func(t *testing.T) {
		attempts = nil
		taskID := register(t, "future", sourceInstance, oldTarget)
		before := issue(t, taskID, "evt-future-1", "progress-future-1")
		if err := manager.Handoff(ctx, taskID, task.HandoffTarget{Target: newTarget, TargetInstance: targetInstance}); err != nil {
			t.Fatalf("handoff: %v", err)
		}
		after := issue(t, taskID, "evt-future-2", "progress-future-2")
		if after.Target != newTarget || after.TargetInstance != targetInstance {
			t.Fatalf("envelope = %+v, want the future notification frozen beside %v / %s", after, newTarget, targetInstance)
		}
		if after.Source != before.Source {
			t.Fatalf("source = %+v, want the frozen source %+v kept beside the new target", after.Source, before.Source)
		}
		record, err := manager.HandoffOf(ctx, taskID)
		if err != nil {
			t.Fatalf("handoff record: %v", err)
		}
		want := task.HandoffRecord{
			TaskID:         taskID,
			PreviousTarget: oldTarget,
			SourceInstance: sourceInstance,
			Target:         newTarget,
			TargetInstance: targetInstance,
		}
		if record != want {
			t.Fatalf("handoff record = %+v, want the recorded link %+v from the previous target to the new one", record, want)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if len(attempts) != 2 || attempts[0] != before || attempts[1] != after {
			t.Fatalf("attempts = %+v, want the old notification beside its old routing and the future one beside the new routing", attempts)
		}
		for _, attempt := range attempts {
			if attempt.Notice == before.Notice && attempt.TargetInstance != sourceInstance {
				t.Fatalf("attempt = %+v, want an old notification never delivered to the new instance", attempt)
			}
		}
	})

	t.Run("the source and the target instance claim their own notifications apart", func(t *testing.T) {
		attempts = nil
		taskID := register(t, "claims", sourceInstance, oldTarget)
		before := issue(t, taskID, "evt-claims-1", "progress-claims-1")
		if err := manager.Handoff(ctx, taskID, task.HandoffTarget{Target: newTarget, TargetInstance: targetInstance}); err != nil {
			t.Fatalf("handoff: %v", err)
		}
		after := issue(t, taskID, "evt-claims-2", "progress-claims-2")

		type identity struct {
			task  event.TaskID
			round int64
			kind  task.NoticeKind
			seq   int64
		}
		claims := map[identity]event.InstanceID{}
		facts := 0
		for _, record := range carrier.Facts(taskID) {
			facts++
			for _, envelope := range record.Outbox {
				key := identity{envelope.Notice.TaskID, envelope.Notice.Round, envelope.Notice.Kind, envelope.Notice.Seq}
				if prior, ok := claims[key]; ok {
					t.Fatalf("notification %+v claimed by both %s and %s, want one instance and one fact per notification", key, prior, envelope.TargetInstance)
				}
				claims[key] = envelope.TargetInstance
			}
		}
		if got := claims[identity{taskID, before.Notice.Round, task.NoticeProgress, before.Notice.Seq}]; got != sourceInstance {
			t.Fatalf("claim = %s, want the pre-handoff notification owned by the source instance %s", got, sourceInstance)
		}
		if got := claims[identity{taskID, after.Notice.Round, task.NoticeProgress, after.Notice.Seq}]; got != targetInstance {
			t.Fatalf("claim = %s, want the future notification owned by the target instance %s", got, targetInstance)
		}

		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("repeated flush: %v", err)
		}
		for _, attempt := range attempts {
			key := identity{attempt.Notice.TaskID, attempt.Notice.Round, attempt.Notice.Kind, attempt.Notice.Seq}
			if attempt.TargetInstance != claims[key] {
				t.Fatalf("attempt = %+v, want the notification delivered beside the frozen instance %s that owns it", attempt, claims[key])
			}
		}

		if result, err := registry.Advance(ctx, taskID, task.StatusRunning, "progress-claims-1", []task.Envelope{before}); result != task.CommitCommitted || err != nil {
			t.Fatalf("repeated mutation = (%v, %v), want the fact already stored", result, err)
		}
		repeated := 0
		for _, record := range carrier.Facts(taskID) {
			repeated += len(record.Outbox)
		}
		if repeated != len(claims) {
			t.Fatalf("stored %d notifications beside %d identities, want no second fact for a repeated transport or mutation", repeated, len(claims))
		}
	})

	t.Run("a closed instance keeps the notifications it still owes", func(t *testing.T) {
		attempts = nil
		const closedInstance event.InstanceID = "instance-closed"
		taskID := register(t, "closed", closedInstance, oldTarget)
		kept1 := issue(t, taskID, "evt-closed-1", "progress-closed-1")
		kept2 := issue(t, taskID, "evt-closed-2", "progress-closed-2")
		if err := manager.CloseInstance(ctx, closedInstance); err != nil {
			t.Fatalf("close instance: %v", err)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush after the close: %v", err)
		}
		if len(attempts) != 0 {
			t.Fatalf("attempts = %+v, want no retained notification delivered anywhere after the close", attempts)
		}
		open, err := manager.Obligations(ctx, taskID)
		if err != nil {
			t.Fatalf("obligations: %v", err)
		}
		if open.Count != 3 {
			t.Fatalf("obligations = %+v, want the execution obligation beside the two notifications retained", open)
		}
		if len(open.Blocked) != 2 || open.Blocked[0].Reason != task.ErrInstanceClosed.Error() {
			t.Fatalf("blocked = %+v, want both retained notifications refused by name as %q", open.Blocked, task.ErrInstanceClosed)
		}
		retained, err := manager.Retained(ctx, closedInstance)
		if err != nil {
			t.Fatalf("retained: %v", err)
		}
		if len(retained) != 2 || retained[0] != kept1 || retained[1] != kept2 {
			t.Fatalf("retained = %+v, want the two undelivered notifications kept beside their frozen routing", retained)
		}

		material, err := manager.RecoveryMaterial(ctx, closedInstance)
		if err != nil {
			t.Fatalf("recovery material: %v", err)
		}
		if len(material) != 4 {
			t.Fatalf("recovery material = %d links, want the admission link beside the three facts it follows", len(material))
		}
		if material[0].Spec.CallID != "call-closed" || material[1].Spec != (task.Spec{}) {
			t.Fatalf("material = %+v, want the admission material on the first link only", material[:2])
		}
		registry2 := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
		manager2 := task.NewMemoryManager(registry2, nil)
		report, err := registry2.Restore(ctx, material, removedOwners{})
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		if len(report.Tasks) != 1 || report.Tasks[0].TaskID != taskID || !report.Tasks[0].Recovered || report.Tasks[0].Status != task.StatusRunning {
			t.Fatalf("restore report = %+v, want the task rebuilt from the chain", report)
		}
		if !errors.Is(report.Tasks[0].Refused, task.ErrTargetRemoved) {
			t.Fatalf("refused = %v, want the re-delivery refused by name while the chain is kept either way", report.Tasks[0].Refused)
		}
		open2, err := manager2.Obligations(ctx, taskID)
		if err != nil {
			t.Fatalf("obligations after the restore: %v", err)
		}
		if open2.Count != 3 {
			t.Fatalf("obligations = %+v, want the recovered task keeping its undelivered notifications either way", open2)
		}
		recovered := map[event.TaskID][]task.Notice{}
		for _, obligation := range open2.Open {
			if obligation.Notice != (task.Notice{}) {
				recovered[obligation.TaskID] = append(recovered[obligation.TaskID], obligation.Notice)
			}
		}
		if len(recovered[taskID]) != 2 || recovered[taskID][0] != kept1.Notice || recovered[taskID][1] != kept2.Notice {
			t.Fatalf("recovered = %+v, want the retained notifications restored under their stable identities", recovered)
		}

		rebornID := register(t, "reborn", "instance-reborn", oldTarget)
		fresh := issue(t, rebornID, "evt-reborn-1", "progress-reborn-1")
		if err := manager.Flush(ctx, rebornID); err != nil {
			t.Fatalf("flush of the same-name instance: %v", err)
		}
		if len(attempts) != 1 || attempts[0] != fresh || fresh.TargetInstance != "instance-reborn" {
			t.Fatalf("attempts = %+v, want the new instance served only by its own notifications", attempts)
		}
		for _, attempt := range attempts {
			if attempt.TargetInstance == closedInstance {
				t.Fatalf("attempt = %+v, want no retained notification flowing into the same-name instance", attempt)
			}
		}
		if still, err := manager.Retained(ctx, closedInstance); err != nil || len(still) != 2 {
			t.Fatalf("retained = (%+v, %v), want the two notifications still kept after the same-name instance ran", still, err)
		}
	})

	t.Run("a retry never re-routes a committed notification", func(t *testing.T) {
		attempts = nil
		taskID := register(t, "retry", sourceInstance, oldTarget)
		owed := issue(t, taskID, "evt-retry-1", "progress-retry-1")
		for i := 0; i < 3; i++ {
			if err := manager.Flush(ctx, taskID); err != nil {
				t.Fatalf("flush %d: %v", i, err)
			}
		}
		if len(attempts) != 3 {
			t.Fatalf("attempts = %d, want the owed notification offered on every retry", len(attempts))
		}
		for i, attempt := range attempts {
			if attempt != owed {
				t.Fatalf("retry %d = %+v, want the frozen routing %+v repeated and no target re-resolved", i, attempt, owed)
			}
		}
		if err := manager.Handoff(ctx, taskID, task.HandoffTarget{Target: newTarget, TargetInstance: targetInstance}); err != nil {
			t.Fatalf("handoff: %v", err)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush after the handoff: %v", err)
		}
		if attempts[3] != owed {
			t.Fatalf("retry after the handoff = %+v, want the started notification still beside the target fixed at %v", attempts[3], oldTarget)
		}
		future := issue(t, taskID, "evt-retry-2", "progress-retry-2")
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush of the future notification: %v", err)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("repeated flush of the future notification: %v", err)
		}
		if len(attempts) != 8 {
			t.Fatalf("attempts = %d, want the owed notification beside the future one offered on every retry", len(attempts))
		}
		for i, attempt := range attempts {
			switch attempt.Notice {
			case owed.Notice:
				if attempt != owed {
					t.Fatalf("retry %d = %+v, want the started notification still beside the target fixed at %v", i, attempt, oldTarget)
				}
			case future.Notice:
				if attempt != future {
					t.Fatalf("retry %d = %+v, want the future notification retried beside its own frozen routing %+v", i, attempt, future)
				}
			default:
				t.Fatalf("retry %d = %+v, want no notification beyond the two this task owes", i, attempt)
			}
		}
	})

	t.Run("progress notifications keep stable ordered seqs", func(t *testing.T) {
		attempts = nil
		taskID := register(t, "seq", sourceInstance, oldTarget)
		notices := make([]task.Envelope, 0, 3)
		for i := 0; i < 3; i++ {
			fact := event.EventID(fmt.Sprintf("evt-seq-%d", i))
			envelope, err := registry.IssueNotice(ctx, taskID, task.NoticeProgress, fact)
			if err != nil {
				t.Fatalf("issue notice %d: %v", i, err)
			}
			if envelope.Notice.TaskID != taskID || envelope.Notice.Round != 1 || envelope.Notice.Kind != task.NoticeProgress || envelope.Notice.Seq != int64(i) {
				t.Fatalf("notice %d = %+v, want the identity %s/1/progress/%d carried field by field", i, envelope.Notice, taskID, i)
			}
			notices = append(notices, envelope)
		}
		if result, err := registry.Advance(ctx, taskID, task.StatusRunning, "progress-seq", notices); result != task.CommitCommitted || err != nil {
			t.Fatalf("advance = (%v, %v), want the three progress notifications committed", result, err)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if err := manager.Flush(ctx, taskID); err != nil {
			t.Fatalf("repeated flush: %v", err)
		}
		if len(attempts) != 6 {
			t.Fatalf("attempts = %d, want the three notifications offered once per transport", len(attempts))
		}
		for round := 0; round < 2; round++ {
			for i, want := range notices {
				if got := attempts[round*3+i]; got != want {
					t.Fatalf("transport %d notification %d = %+v, want the stable identity %+v in seq order", round, i, got, want)
				}
			}
		}
		open, err := manager.Obligations(ctx, taskID)
		if err != nil {
			t.Fatalf("obligations: %v", err)
		}
		if open.Count != 4 {
			t.Fatalf("obligations = %+v, want the execution obligation beside the three still owed after the repeated transport", open)
		}
		owed := 0
		for _, record := range carrier.Facts(taskID) {
			owed += len(record.Outbox)
		}
		if owed != 3 {
			t.Fatalf("stored %d notifications, want one fact per identity and none for the repeated transport", owed)
		}
		if result, err := registry.Advance(ctx, taskID, task.StatusRunning, "progress-seq", notices); result != task.CommitCommitted || err != nil {
			t.Fatalf("repeated mutation = (%v, %v), want the fact already stored", result, err)
		}
		if facts := len(carrier.Facts(taskID)); facts != 3 {
			t.Fatalf("stored %d facts, want the admission, the run and the one progress fact", facts)
		}
		fourth, err := registry.IssueNotice(ctx, taskID, task.NoticeProgress, "evt-seq-3")
		if err != nil || fourth.Notice.Seq != 3 {
			t.Fatalf("next notice = (%+v, %v), want the sequence continuing at 3 beside the identities already issued", fourth, err)
		}
		recovered := task.NewNoticeSequencer()
		for _, envelope := range notices {
			recovered.Observe(envelope.Notice)
		}
		next := recovered.Issue(taskID, 1, task.NoticeProgress, "evt-seq-4")
		if next.Seq != 3 {
			t.Fatalf("recovered sequence = %d, want the sequencer continuing past the identities the fact chain carries", next.Seq)
		}
	})
}
