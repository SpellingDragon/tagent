package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
		registry, commit := invariantRegistry(t)
		manager := NewMemoryManager(registry, nil)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if err := manager.Handoff(ctx, taskID, invariantHandoffTarget); err != nil {
			t.Fatalf("hand the future notifications over: %v", err)
		}
		facts := commit.Facts(taskID)
		links := 0
		for _, fact := range facts {
			if strings.HasPrefix(string(fact.MutationID), handoffPrefix) {
				links++
			}
		}
		if links != 1 {
			t.Fatalf("handoff links on the chain = %d, want the move carried beside the task's facts", links)
		}
		if got := commit.Revision(taskID); got != int64(len(facts)) {
			t.Fatalf("revision after the handoff = %d, want one per stored fact", got)
		}
		if err := manager.Handoff(ctx, taskID, invariantHandoffTarget); err != nil {
			t.Fatalf("repeat the handoff: %v", err)
		}
		if got := len(commit.Facts(taskID)); got != len(facts) {
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
		if _, err := registry.Advance(ctx, taskID, StatusRunning, "progress", []Envelope{after}); err != nil {
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
		if got := report.Tasks[0].Status; got != StatusRunning {
			t.Fatalf("restored status = %v, want the running phase the chain ends at", got)
		}
		if reported, err := restored.ProgressReported(ctx, taskID); err != nil || !reported {
			t.Fatalf("progress reported by the restored task = (%v, %v), want the progress the chain carries", reported, err)
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

// taskDerivation is the part of one task a committed fact decides: the answers a
// live move and a chain fold must state alike.
type taskDerivation struct {
	Status      Status
	Revision    int64
	Round       int64
	Mutations   []CommitRecord
	Obligations []checkpointObligation
}

// derivationOf reads one task's derivation out of a checkpoint payload.
func derivationOf(t *testing.T, payload []byte, taskID event.TaskID) taskDerivation {
	t.Helper()
	var wire RegistryCheckpoint
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("read the stated account: %v", err)
	}
	for _, stated := range wire.Tasks {
		if stated.TaskID == taskID {
			return taskDerivation{
				Status:      stated.Status,
				Revision:    stated.Revision,
				Round:       stated.Round,
				Mutations:   stated.Mutations,
				Obligations: stated.Obligations,
			}
		}
	}
	t.Fatalf("the stated account names no task %s", taskID)
	return taskDerivation{}
}

// TestTaskOnlineAndRestoreApplyOneRule pins the one rule a committed fact is applied by.
//   - positive: a task the live path moved and the same chain folded into a fresh registry derive one status, revision and round
//   - positive: they derive one mutation set and one obligation set, reactivation included
//   - positive: the round a re-entry opened states one status, revision, round, mutation set and obligation set on both sides
//   - consumer: MemoryRegistry.Advance, MemoryRegistry.Reactivate and MemoryRegistry.Resume on the live side, MemoryRegistry.Restore on the recovery side
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestTaskOnlineAndRestoreApplyOneRule(t *testing.T) {
	t.Run("the two settled rounds of one task", twoSettledRoundsState)
	t.Run("the round a re-entry opened", reentryParity)
}

// twoSettledRoundsState states one chain of two settled rounds on both paths and pins
// the account they derive from it.
func twoSettledRoundsState(t *testing.T) {
	ctx := context.Background()
	live, commit := invariantRegistry(t)
	taskID := invariantRegister(t, live, "call-one-rule", "dedup-one-rule")
	if result, err := live.Advance(ctx, taskID, StatusRunning, MutationID("run:"+string(taskID)),
		[]Envelope{invariantEnvelope(taskID, NoticeAccepted, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
	}
	if result, err := live.Advance(ctx, taskID, StatusSettled, MutationID("settle:"+string(taskID)+":1"),
		[]Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the first round = (%v, %v), want a committed change", result, err)
	}
	round, err := live.Reactivate(ctx, taskID)
	if err != nil {
		t.Fatalf("re-enter the settled task: %v", err)
	}
	second := Envelope{
		Notice:         Notice{TaskID: taskID, Round: round, Kind: NoticeSettled, Seq: 0, Event: "fact-2"},
		Target:         invariantTaskAddress,
		Source:         invariantTaskAddress,
		TargetInstance: "instance-a",
	}
	if result, err := live.Advance(ctx, taskID, StatusSettled, MutationID(fmt.Sprintf("settle:%s:%d", taskID, round)),
		[]Envelope{second}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the second round = (%v, %v), want a committed change", result, err)
	}
	livePayload, err := live.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the live account: %v", err)
	}

	records := commit.Facts(taskID)
	chain := make([]TaskRecord, 0, len(records))
	for i, fact := range records {
		link := TaskRecord{Fact: fact}
		if i == 0 {
			link.Spec = invariantTaskSpec("call-one-rule", "dedup-one-rule")
		}
		chain = append(chain, link)
	}
	recovered, _ := invariantRegistry(t)
	report, err := recovered.Restore(ctx, chain, invariantOwners{})
	if err != nil {
		t.Fatalf("fold the chain the live path committed: %v", err)
	}
	if len(report.Tasks) != 1 || !report.Tasks[0].Recovered || report.Tasks[0].Refused != nil {
		t.Fatalf("the fold reported %+v, want the one task rebuilt from its chain", report.Tasks)
	}
	restoredPayload, err := recovered.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the recovered account: %v", err)
	}

	liveState := derivationOf(t, livePayload, taskID)
	restoredState := derivationOf(t, restoredPayload, taskID)
	if !reflect.DeepEqual(liveState, restoredState) {
		t.Fatalf("the live path derives %+v, the fold of the same chain derives %+v: one committed fact is applied by one rule", liveState, restoredState)
	}
	if restoredState.Status != StatusSettled || restoredState.Revision != int64(len(records)) || restoredState.Round != round {
		t.Fatalf("the recovered task stands at %+v, want the settled round %d at revision %d", restoredState, round, len(records))
	}
	if len(restoredState.Mutations) != len(records) {
		t.Fatalf("the recovered task holds %d mutation(s), want the %d the chain committed", len(restoredState.Mutations), len(records))
	}
	if len(restoredState.Obligations) != 3 {
		t.Fatalf("the recovered task owes %+v, want the three notifications the chain committed and no execution obligation: the two settles closed it", restoredState.Obligations)
	}
	for _, held := range restoredState.Obligations {
		if held.Kind != ObligationAwaitingDelivery {
			t.Fatalf("the recovered task owes %+v, want delivery obligations alone", held)
		}
	}
}

// reentryLeg is one way a task re-enters a round: the name the leg is reported under
// and the call it re-enters by.
type reentryLeg struct {
	name    string
	reenter func(t *testing.T, registry *MemoryRegistry, taskID event.TaskID)
}

// reentryLegs answers the two re-entries a task opens a round by: a reactivation of a
// settled task and a resume of one.
func reentryLegs() []reentryLeg {
	return []reentryLeg{
		{
			name: "reactivate",
			reenter: func(t *testing.T, registry *MemoryRegistry, taskID event.TaskID) {
				t.Helper()
				if _, err := registry.Reactivate(context.Background(), taskID); err != nil {
					t.Fatalf("re-enter the settled task: %v", err)
				}
			},
		},
		{
			name: "resume",
			reenter: func(t *testing.T, registry *MemoryRegistry, taskID event.TaskID) {
				t.Helper()
				if _, err := registry.Resume(context.Background(), taskID, event.Event{Key: "input-reentry"}); err != nil {
					t.Fatalf("resume the settled task: %v", err)
				}
			},
		},
	}
}

// reentryParity pins that the round a re-entry opened states the same account on the
// live path and on the fold of its own chain.
func reentryParity(t *testing.T) {
	for _, leg := range reentryLegs() {
		t.Run(leg.name, func(t *testing.T) {
			liveState, foldedState := reentryDerivation(t, leg)
			if !reflect.DeepEqual(liveState, foldedState) {
				t.Fatalf("the live path derives %+v, the fold of the same chain derives %+v: one committed re-entry is applied by one rule", liveState, foldedState)
			}
			if liveState.Status != StatusRunning || liveState.Round != 2 {
				t.Fatalf("the re-entered task stands at %+v, want the running second round", liveState)
			}
		})
	}
}

// reentryDerivation drives one re-entry on the live path and answers what both sides
// state for the task it re-entered: the live registry and a fresh registry folding the
// chain the live path committed.
//
// The leg starts from a settled task that owes no execution obligation, so the one read
// afterwards is the turn the re-entry opened rather than one a settlement left behind.
func reentryDerivation(t *testing.T, leg reentryLeg) (taskDerivation, taskDerivation) {
	t.Helper()
	ctx := context.Background()
	live, commit := invariantRegistry(t)
	taskID := invariantRegister(t, live, "call-reentry", "dedup-reentry")
	if result, err := live.Advance(ctx, taskID, StatusRunning, MutationID("run:"+string(taskID)),
		[]Envelope{invariantEnvelope(taskID, NoticeAccepted, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
	}
	if result, err := live.Advance(ctx, taskID, StatusSettled, MutationID("settle:"+string(taskID)+":1"),
		[]Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the first round = (%v, %v), want a committed change", result, err)
	}
	if settled := statedAccountOf(t, live, taskID); executionObligations(settled.Obligations) != 0 {
		t.Fatalf("the settled task owes %+v with %d execution obligation(s): the leg must start from none",
			settled.Obligations, executionObligations(settled.Obligations))
	}
	leg.reenter(t, live, taskID)
	return statedAccountOf(t, live, taskID),
		derivationOf(t, foldedAccountOf(t, commit, taskID, invariantTaskSpec("call-reentry", "dedup-reentry")), taskID)
}

// statedAccountOf states one registry's account and answers the derivation of the named task.
func statedAccountOf(t *testing.T, registry *MemoryRegistry, taskID event.TaskID) taskDerivation {
	t.Helper()
	payload, err := registry.SnapshotCheckpoint(context.Background())
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	return derivationOf(t, payload, taskID)
}

// foldedAccountOf folds the chain one live registry committed into a fresh registry and
// answers the account the fold states.
func foldedAccountOf(t *testing.T, commit *MemoryCommit, taskID event.TaskID, spec Spec) []byte {
	t.Helper()
	records := commit.Facts(taskID)
	chain := make([]TaskRecord, 0, len(records))
	for i, fact := range records {
		link := TaskRecord{Fact: fact}
		if i == 0 {
			link.Spec = spec
		}
		chain = append(chain, link)
	}
	folded, _ := invariantRegistry(t)
	report, err := folded.Restore(context.Background(), chain, invariantOwners{})
	if err != nil {
		t.Fatalf("fold the chain the live path committed: %v", err)
	}
	if len(report.Tasks) != 1 || !report.Tasks[0].Recovered || report.Tasks[0].Refused != nil {
		t.Fatalf("the fold reported %+v, want the one task rebuilt from its chain", report.Tasks)
	}
	payload, err := folded.SnapshotCheckpoint(context.Background())
	if err != nil {
		t.Fatalf("state the recovered account: %v", err)
	}
	return payload
}

// executionObligations counts the execution obligations one stated account holds: the
// turns the task still owes, beside the delivery obligations it holds as well.
func executionObligations(held []checkpointObligation) int {
	count := 0
	for _, obligation := range held {
		if obligation.Kind == ObligationAwaitingTool {
			count++
		}
	}
	return count
}

// TestReactivatedTaskKeepsItsExecutionObligationAcrossRestore pins the execution obligation a re-entry opens to the fact that opened it.
//   - positive: a settled task re-entered on the live path owes the turn of the round it opened
//   - negative: the chain folded into a fresh registry states that obligation too; a fold that dropped it rebuilds the task in flight owing nothing
//   - positive: the execution obligation points at the task itself and carries no delivery of its own, on both sides alike
//   - consumer: MemoryRegistry.Reactivate and MemoryRegistry.Resume on the live side, MemoryRegistry.Restore on the recovery side
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestReactivatedTaskKeepsItsExecutionObligationAcrossRestore(t *testing.T) {
	for _, leg := range reentryLegs() {
		t.Run(leg.name, func(t *testing.T) {
			liveState, foldedState := reentryDerivation(t, leg)
			if held := executionObligations(liveState.Obligations); held != 1 {
				t.Fatalf("the live task owes %d execution obligation(s) beside %+v, want the one turn the round it opened waits for",
					held, liveState.Obligations)
			}
			if held := executionObligations(foldedState.Obligations); held != 1 {
				t.Fatalf("the task folded from its chain owes %d execution obligation(s) beside %+v, want the one turn the round it opened waits for: the chain carries the re-entry alone",
					held, foldedState.Obligations)
			}
			if !reflect.DeepEqual(liveState.Obligations, foldedState.Obligations) {
				t.Fatalf("the live task owes %+v, the fold of the same chain owes %+v", liveState.Obligations, foldedState.Obligations)
			}
			for _, held := range foldedState.Obligations {
				if held.Kind != ObligationAwaitingTool {
					continue
				}
				if held.Notice != (Notice{}) || held.Envelope != (Envelope{}) {
					t.Fatalf("the execution obligation = %+v, want the task itself and no delivery of its own", held)
				}
			}
		})
	}
}

// TestTaskApplyRefusesABrokenRevision pins the revision one chain link must follow.
//   - negative: a link whose expected revision does not follow the entry is refused by name
//   - positive: the refused link applies nothing, so the task keeps the status, revision and round the links before it landed
//   - consumer: MemoryRegistry.Restore folds the chain
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestTaskApplyRefusesABrokenRevision(t *testing.T) {
	ctx := context.Background()
	const taskID = event.TaskID("task-broken-revision")
	registry, _ := invariantRegistry(t)
	chain := []TaskRecord{
		{Spec: invariantTaskSpec("call-broken", "dedup-broken"), Fact: CommitRecord{
			MutationID: MutationID("admit:" + string(taskID)), TaskID: taskID, ExpectedRevision: 0, Status: StatusAccepted}},
		{Fact: CommitRecord{
			MutationID: MutationID("run:" + string(taskID)), TaskID: taskID, ExpectedRevision: 5, Status: StatusRunning}},
	}
	if _, err := registry.Restore(ctx, chain, invariantOwners{}); !errors.Is(err, ErrChainBroken) {
		t.Fatalf("a link that does not follow its chain = %v, want %v", err, ErrChainBroken)
	}
	payload, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account beside a refused link: %v", err)
	}
	state := derivationOf(t, payload, taskID)
	if state.Status != StatusAccepted || state.Revision != 1 || state.Round != 1 {
		t.Fatalf("the task beside a refused link stands at %+v, want the accepted link at revision 1 and round 1: the refused link applied nothing", state)
	}
}

// TestTaskApplyRefusesAConflictingMutation pins the one mutation identity rule of the apply.
//   - negative: a second link under a mutation identity the task already stores with other content is refused by name
//   - positive: the refused link applies nothing, so the task keeps the fact that identity already names and no second mutation
//   - consumer: MemoryRegistry.Restore folds the chain
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestTaskApplyRefusesAConflictingMutation(t *testing.T) {
	ctx := context.Background()
	const taskID = event.TaskID("task-conflicting-mutation")
	registry, _ := invariantRegistry(t)
	chain := []TaskRecord{
		{Spec: invariantTaskSpec("call-conflict", "dedup-conflict"), Fact: CommitRecord{
			MutationID: MutationID("admit:" + string(taskID)), TaskID: taskID, ExpectedRevision: 0, Status: StatusAccepted}},
		{Fact: CommitRecord{
			MutationID: MutationID("run:" + string(taskID)), TaskID: taskID, ExpectedRevision: 1, Status: StatusRunning}},
		{Fact: CommitRecord{
			MutationID: MutationID("run:" + string(taskID)), TaskID: taskID, ExpectedRevision: 2, Status: StatusSettled}},
	}
	if _, err := registry.Restore(ctx, chain, invariantOwners{}); !errors.Is(err, ErrMutationConflict) {
		t.Fatalf("a mutation identity carrying other content = %v, want %v", err, ErrMutationConflict)
	}
	payload, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account beside a refused link: %v", err)
	}
	state := derivationOf(t, payload, taskID)
	if state.Status != StatusRunning || state.Revision != 2 || len(state.Mutations) != 2 {
		t.Fatalf("the task beside a refused link stands at %+v, want the running fact at revision 2 and no second mutation", state)
	}
}
