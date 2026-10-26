package task

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// invariantHandoffAddress is the address a handoff moves the invariants' future notifications to.
var invariantHandoffAddress = event.Address{AgentID: "agent-b", SessionID: "session-b"}

// invariantHandoffTarget is the target a handoff moves the invariants' future notifications to.
var invariantHandoffTarget = HandoffTarget{Target: invariantHandoffAddress, TargetInstance: "instance-b"}

// TestHandoffRestoreInvariants pins the frozen routing of a handoff and the chain it travels with.
// - a handoff moves only the notifications issued after it, and the ones already created keep their frozen routing
// - a handoff is one durable mutation of the task's chain, and its repeat records no second link
// - a chain folded back into a registry rebuilds the same routing move, the same status and every obligation
// - a link carrying an undeterminable status is refused by name and rebuilds nothing
// - a chain carrying no admission material is refused by name and rebuilds nothing
// - a link whose revision does not follow the one before it is refused by name
// - a settled link closes the execution obligation and keeps the delivery obligation open
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestHandoffRestoreInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("a handoff moves only the notifications issued after it", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		before, err := registry.IssueNotice(ctx, taskID, NoticeAccepted, "fact-1")
		if err != nil {
			t.Fatalf("issue the notification before the handoff: %v", err)
		}
		if result, err := registry.Advance(ctx, taskID, StatusRunning, "running", []Envelope{before}); err != nil || result != CommitCommitted {
			t.Fatalf("commit the notification before the handoff = (%v, %v), want a committed change", result, err)
		}
		if err := manager.Handoff(ctx, taskID, invariantHandoffTarget); err != nil {
			t.Fatalf("hand the future notifications over: %v", err)
		}
		record, err := manager.HandoffOf(ctx, taskID)
		if err != nil {
			t.Fatalf("read the handoff link: %v", err)
		}
		if record.PreviousTarget != invariantTaskAddress || record.Target != invariantHandoffAddress {
			t.Fatalf("handoff link = %+v, want the declaration's target before it and the new target after it", record)
		}
		if record.SourceInstance != "instance-a" || record.TargetInstance != "instance-b" {
			t.Fatalf("handoff instances = (%q, %q), want the source and the target kept apart", record.SourceInstance, record.TargetInstance)
		}
		retained, err := manager.Retained(ctx, "instance-a")
		if err != nil {
			t.Fatalf("read what the source instance still owes: %v", err)
		}
		if len(retained) != 1 || retained[0] != before {
			t.Fatalf("notifications retained on the source instance = %+v, want the frozen routing of the one already created", retained)
		}
		moved, err := manager.Retained(ctx, "instance-b")
		if err != nil {
			t.Fatalf("read what the target instance owes: %v", err)
		}
		if len(moved) != 0 {
			t.Fatalf("notifications retained on the target instance = %+v, want the already created one untouched", moved)
		}
		held, err := manager.Obligations(ctx, taskID)
		if err != nil {
			t.Fatalf("read the obligations after the handoff: %v", err)
		}
		if held.Count != 2 || len(held.Open) != 2 {
			t.Fatalf("obligations after the handoff = %+v, want the admission's own one beside the moved delivery", held)
		}
		for _, open := range held.Open {
			if open.Kind == ObligationAwaitingDelivery {
				t.Fatalf("obligation after the handoff = %+v, want every undelivered notification reclassified as handed-off", open)
			}
		}
		if held.Open[1].Kind != ObligationHandedOff || held.Open[1].Notice != before.Notice {
			t.Fatalf("moved obligation = %+v, want the already created notification's identity kept", held.Open[1])
		}
		after, err := registry.IssueNotice(ctx, taskID, NoticeProgress, "fact-2")
		if err != nil {
			t.Fatalf("issue the notification after the handoff: %v", err)
		}
		if after.Target != invariantHandoffAddress || after.TargetInstance != "instance-b" {
			t.Fatalf("routing of the notification issued after the handoff = (%+v, %q), want the new target", after.Target, after.TargetInstance)
		}
	})

	t.Run("a handoff is one durable mutation and its repeat records no second link", func(t *testing.T) {
		registry, carrier := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if err := manager.Handoff(ctx, taskID, invariantHandoffTarget); err != nil {
			t.Fatalf("hand the future notifications over: %v", err)
		}
		facts := carrier.Facts(taskID)
		links := 0
		for _, fact := range facts {
			if strings.HasPrefix(string(fact.MutationID), handoffPrefix) {
				links++
			}
		}
		if links != 1 {
			t.Fatalf("handoff links on the chain = %d, want the move carried beside the task's facts", links)
		}
		if got := carrier.Revision(taskID); got != int64(len(facts)) {
			t.Fatalf("revision after the handoff = %d, want one per stored fact", got)
		}
		if err := manager.Handoff(ctx, taskID, invariantHandoffTarget); err != nil {
			t.Fatalf("repeat the handoff: %v", err)
		}
		if got := len(carrier.Facts(taskID)); got != len(facts) {
			t.Fatalf("facts after a repeated handoff = %d, want no second link", got)
		}
	})

	t.Run("a chain folded back rebuilds the routing move, the status and every obligation", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		before, err := registry.IssueNotice(ctx, taskID, NoticeAccepted, "fact-1")
		if err != nil {
			t.Fatalf("issue the notification before the handoff: %v", err)
		}
		if _, err := registry.Advance(ctx, taskID, StatusRunning, "running", []Envelope{before}); err != nil {
			t.Fatalf("commit the notification before the handoff: %v", err)
		}
		if err := manager.Handoff(ctx, taskID, invariantHandoffTarget); err != nil {
			t.Fatalf("hand the future notifications over: %v", err)
		}
		after, err := registry.IssueNotice(ctx, taskID, NoticeProgress, "fact-2")
		if err != nil {
			t.Fatalf("issue the notification after the handoff: %v", err)
		}
		if _, err := registry.Advance(ctx, taskID, StatusProgress, "progress", []Envelope{after}); err != nil {
			t.Fatalf("commit the notification after the handoff: %v", err)
		}
		chain, err := manager.RecoveryMaterial(ctx, "instance-b")
		if err != nil {
			t.Fatalf("read the recovery material of the target instance: %v", err)
		}
		if len(chain) == 0 {
			t.Fatal("the recovery material carries no chain for a task the instance still owes")
		}
		restored, _ := invariantRegistry(t)
		report, err := restored.Restore(ctx, chain, invariantOwners{})
		if err != nil {
			t.Fatalf("fold the chain back: %v", err)
		}
		if len(report.Tasks) != 1 || !report.Tasks[0].Recovered {
			t.Fatalf("restore report = %+v, want the one task rebuilt from the chain", report)
		}
		if got := report.Tasks[0].Status; got != StatusProgress {
			t.Fatalf("restored status = %v, want the status the chain ends at", got)
		}
		restoredManager := NewMemoryManager(restored, nil)
		record, err := restoredManager.HandoffOf(ctx, taskID)
		if err != nil {
			t.Fatalf("read the restored handoff link: %v", err)
		}
		original, err := manager.HandoffOf(ctx, taskID)
		if err != nil {
			t.Fatalf("read the recorded handoff link: %v", err)
		}
		if record != original {
			t.Fatalf("restored handoff link = %+v, want the recorded one %+v", record, original)
		}
		held, err := restoredManager.Obligations(ctx, taskID)
		if err != nil {
			t.Fatalf("read the restored obligations: %v", err)
		}
		if held.Count != 3 {
			t.Fatalf("restored obligations = %+v, want the admission's own one beside both delivery obligations", held)
		}
		kinds := map[ObligationKind]int{}
		for _, open := range held.Open {
			kinds[open.Kind]++
		}
		if kinds[ObligationHandedOff] != 1 || kinds[ObligationAwaitingDelivery] != 1 {
			t.Fatalf("restored obligations = %+v, want the handed-off one beside the one still awaiting delivery", held)
		}
		future, err := restored.IssueNotice(ctx, taskID, NoticeProgress, "fact-3")
		if err != nil {
			t.Fatalf("issue the notification of the rebuilt task: %v", err)
		}
		if future.Target != invariantHandoffAddress || future.TargetInstance != "instance-b" {
			t.Fatalf("routing of the rebuilt task = (%+v, %q), want the recorded handoff", future.Target, future.TargetInstance)
		}
	})

	t.Run("a link carrying an undeterminable status is refused by name", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		chain := []TaskRecord{{
			Spec: invariantTaskSpec("call-1", "dedup-1"),
			Fact: CommitRecord{MutationID: "admit:task-1", TaskID: "task-1", Status: StatusUnknown},
		}}
		if _, err := registry.Restore(ctx, chain, invariantOwners{}); !errors.Is(err, ErrStatusUnknown) {
			t.Fatalf("folding a link of an undeterminable status = %v, want %v", err, ErrStatusUnknown)
		}
		if _, _, err := registry.Lookup(ctx, "task-1"); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("a task built from the refused link = %v, want %v", err, ErrTaskNotFound)
		}
	})

	t.Run("a chain carrying no admission material is refused by name", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		chain := []TaskRecord{{Fact: CommitRecord{MutationID: "admit:task-1", TaskID: "task-1", Status: StatusAccepted}}}
		if _, err := registry.Restore(ctx, chain, nil); !errors.Is(err, ErrNoOwnerBinding) {
			t.Fatalf("a restore beside no owner binding = %v, want %v", err, ErrNoOwnerBinding)
		}
		report, err := registry.Restore(ctx, chain, invariantOwners{})
		if err != nil {
			t.Fatalf("fold a chain carrying no admission material: %v", err)
		}
		if len(report.Tasks) != 1 {
			t.Fatalf("restore report = %+v, want one outcome for the refused task", report)
		}
		if !errors.Is(report.Tasks[0].Refused, ErrSpecMissing) {
			t.Fatalf("refusal of the chain carrying no admission material = %v, want %v", report.Tasks[0].Refused, ErrSpecMissing)
		}
		if report.Tasks[0].Recovered {
			t.Fatalf("restore outcome = %+v, want no recovery from a link carrying no admission material", report.Tasks[0])
		}
		if _, _, err := registry.Lookup(ctx, "task-1"); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("a task built from the refused chain = %v, want %v", err, ErrTaskNotFound)
		}
	})

	t.Run("a link whose revision does not follow the one before it is refused by name", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		chain := []TaskRecord{
			{
				Spec: invariantTaskSpec("call-1", "dedup-1"),
				Fact: CommitRecord{MutationID: "admit:task-1", TaskID: "task-1", ExpectedRevision: 0, Status: StatusAccepted},
			},
			{
				Fact: CommitRecord{MutationID: "running", TaskID: "task-1", ExpectedRevision: 5, Status: StatusRunning},
			},
		}
		if _, err := registry.Restore(ctx, chain, invariantOwners{}); !errors.Is(err, ErrChainBroken) {
			t.Fatalf("folding a link that does not follow = %v, want %v", err, ErrChainBroken)
		}
	})

	t.Run("a repeated restore adds no second fact and no second obligation", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		envelope, err := registry.IssueNotice(ctx, taskID, NoticeAccepted, "fact-1")
		if err != nil {
			t.Fatalf("issue the notification: %v", err)
		}
		if _, err := registry.Advance(ctx, taskID, StatusRunning, "running", []Envelope{envelope}); err != nil {
			t.Fatalf("commit the notification: %v", err)
		}
		chain, err := manager.RecoveryMaterial(ctx, "instance-a")
		if err != nil {
			t.Fatalf("read the recovery material: %v", err)
		}
		restored, _ := invariantRegistry(t)
		if _, err := restored.Restore(ctx, chain, invariantOwners{}); err != nil {
			t.Fatalf("fold the chain: %v", err)
		}
		restoredManager := NewMemoryManager(restored, nil)
		opened := invariantObligationCount(t, restoredManager, taskID)
		report, err := restored.Restore(ctx, chain, invariantOwners{})
		if err != nil {
			t.Fatalf("fold the same chain again: %v", err)
		}
		if len(report.Tasks) != 1 || report.Tasks[0].Recovered {
			t.Fatalf("repeated restore report = %+v, want an idempotent hit", report)
		}
		if got := invariantObligationCount(t, restoredManager, taskID); got != opened {
			t.Fatalf("obligations after a repeated restore = %d, want the first fold's %d", got, opened)
		}
		reissued := restored.seq.Issue(taskID, envelope.Notice.Round, envelope.Notice.Kind, "fact-2")
		if reissued.Seq != envelope.Notice.Seq+1 {
			t.Fatalf("sequence reissued in the restored category = %d, want the restored identity's next one", reissued.Seq)
		}
	})

	t.Run("a settled link closes the execution obligation and keeps the delivery obligation", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		envelope := Envelope{
			Notice:         Notice{TaskID: "task-1", Round: 1, Kind: NoticeSettled, Event: "fact-1"},
			Target:         invariantTaskAddress,
			Source:         invariantTaskAddress,
			TargetInstance: "instance-a",
		}
		chain := []TaskRecord{
			{
				Spec: invariantTaskSpec("call-1", "dedup-1"),
				Fact: CommitRecord{MutationID: "admit:task-1", TaskID: "task-1", ExpectedRevision: 0, Status: StatusAccepted},
			},
			{
				Fact: CommitRecord{MutationID: "settled", TaskID: "task-1", ExpectedRevision: 1, Status: StatusSettled, Outbox: []Envelope{envelope}},
			},
		}
		report, err := registry.Restore(ctx, chain, invariantOwners{})
		if err != nil {
			t.Fatalf("fold the settled chain: %v", err)
		}
		if len(report.Tasks) != 1 || report.Tasks[0].Status != StatusSettled {
			t.Fatalf("restore report = %+v, want the task rebuilt at the settled fact", report)
		}
		manager := NewMemoryManager(registry, nil)
		held, err := manager.Obligations(ctx, "task-1")
		if err != nil {
			t.Fatalf("read the restored obligations: %v", err)
		}
		if held.Count != 1 || len(held.Open) != 1 {
			t.Fatalf("obligations of the restored settled task = %+v, want the delivery obligation alone", held)
		}
		if got := held.Open[0]; got.Kind != ObligationAwaitingDelivery || got.Notice != envelope.Notice {
			t.Fatalf("obligation of the restored settled task = %+v, want the committed notification's delivery", got)
		}
	})
}

// invariantOwners resolves every target to one owner, or refuses it by name when made to.
type invariantOwners struct {
	removed bool
}

// OwnerOf reports the owner of one target under the current binding.
func (o invariantOwners) OwnerOf(context.Context, event.AgentID, event.AgentID) (event.AgentID, error) {
	if o.removed {
		return "", ErrTargetRemoved
	}
	return event.AgentID("agent-a"), nil
}
