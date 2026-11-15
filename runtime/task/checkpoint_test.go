package task

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestRegistryCheckpointRoundTrip pins the state a task checkpoint states.
//   - a snapshot names every task the registry holds beside the revision it stands at
//   - two snapshots of one account state the same bytes, the snapshot being keyed by task identity
//   - the revision an entry states is the one the next record expects, so a boot folds only the links past it
//   - a restore rebuilds the same status, declaration, round, stamps, handoff and obligations
//   - a repeated mutation of a restored task is recognized as the one already stored
//   - a restore never overwrites a task the registry already rebuilt from its chain
//   - a payload naming no identity, an undeterminable status or a mutation of another task is refused by name
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRegistryCheckpointRoundTrip(t *testing.T) {
	ctx := context.Background()

	t.Run("a snapshot restores the same account", func(t *testing.T) {
		source, _ := invariantRegistry(t)
		manager := NewMemoryManager(source, nil)
		settled := invariantRegister(t, source, "call-settled", "dedup-settled")
		delivering := invariantRegister(t, source, "call-delivering", "dedup-delivering")
		if result, err := source.Advance(ctx, settled, StatusRunning, "running-1", nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the first task under way = (%v, %v), want a committed change", result, err)
		}
		notice := invariantEnvelope(settled, NoticeSettled, 0)
		if result, err := source.Advance(ctx, settled, StatusSettled, "settled-1", []Envelope{notice}); err != nil || result != CommitCommitted {
			t.Fatalf("settle the first task = (%v, %v), want a committed change", result, err)
		}
		if result, err := source.Advance(ctx, delivering, StatusRunning, "running-2", []Envelope{invariantEnvelope(delivering, NoticeAccepted, 0)}); err != nil || result != CommitCommitted {
			t.Fatalf("move the second task under way = (%v, %v), want a committed change", result, err)
		}
		if err := manager.Handoff(ctx, delivering, invariantHandoffTarget); err != nil {
			t.Fatalf("hand the notifications of the second task over: %v", err)
		}
		payload, err := source.SnapshotCheckpoint(ctx)
		if err != nil {
			t.Fatalf("state the account: %v", err)
		}
		var stated RegistryCheckpoint
		if err := json.Unmarshal(payload, &stated); err != nil {
			t.Fatalf("the snapshot states no readable account: %v", err)
		}
		if len(stated.Tasks) != 2 {
			t.Fatalf("the snapshot states %d task(s), want the 2 the registry holds", len(stated.Tasks))
		}
		again, err := source.SnapshotCheckpoint(ctx)
		if err != nil {
			t.Fatalf("state the account a second time: %v", err)
		}
		if string(again) != string(payload) {
			t.Fatalf("the second snapshot states %s, want the bytes of the first", again)
		}

		restored, _ := invariantRegistry(t)
		if err := restored.RestoreCheckpoint(ctx, payload); err != nil {
			t.Fatalf("put the account back: %v", err)
		}
		for _, taskID := range []event.TaskID{settled, delivering} {
			wantSpec, wantStatus, err := source.Lookup(ctx, taskID)
			if err != nil {
				t.Fatalf("read the task %s of the source registry: %v", taskID, err)
			}
			gotSpec, gotStatus, err := restored.Lookup(ctx, taskID)
			if err != nil {
				t.Fatalf("the restored registry holds no task %s: %v", taskID, err)
			}
			if !reflect.DeepEqual(gotSpec, wantSpec) || gotStatus != wantStatus {
				t.Fatalf("the restored task %s = (%+v, %v), want (%+v, %v)", taskID, gotSpec, gotStatus, wantSpec, wantStatus)
			}
			if got, want := restored.checkpointOf(t, taskID), source.checkpointOf(t, taskID); !reflect.DeepEqual(got, want) {
				t.Fatalf("the restored record %s = %+v, want the record the source held %+v", taskID, got, want)
			}
		}
		coverage := restored.CheckpointCoverage()
		for _, taskID := range []event.TaskID{settled, delivering} {
			if got, want := coverage[taskID], source.tasks[taskID].revision; got != want {
				t.Fatalf("the coverage of %s = %d, want the revision %d the entry states", taskID, got, want)
			}
		}

		if result, err := restored.Advance(ctx, delivering, StatusRunning, "running-2", []Envelope{invariantEnvelope(delivering, NoticeAccepted, 0)}); err != nil || result != CommitCommitted {
			t.Fatalf("repeat the mutation of a restored task = (%v, %v), want the stored fact", result, err)
		}
		if got, want := restored.tasks[delivering].revision, source.tasks[delivering].revision; got != want {
			t.Fatalf("the repeat moved the restored task to revision %d, want it to stand at %d", got, want)
		}
	})

	t.Run("a restore never overwrites a task rebuilt from its chain", func(t *testing.T) {
		source, commit := invariantRegistry(t)
		taskID := invariantRegister(t, source, "call-chain", "dedup-chain")
		payload, err := source.SnapshotCheckpoint(ctx)
		if err != nil {
			t.Fatalf("state the account: %v", err)
		}
		if result, err := source.Advance(ctx, taskID, StatusFailed, "failed-chain", nil); err != nil || result != CommitCommitted {
			t.Fatalf("end the task on its chain = (%v, %v), want a committed change", result, err)
		}
		facts := commit.Facts(taskID)
		chain := make([]TaskRecord, 0, len(facts))
		for at, fact := range facts {
			link := TaskRecord{Fact: fact}
			if at == 0 {
				link.Spec = invariantTaskSpec("call-chain", "dedup-chain")
			}
			chain = append(chain, link)
		}
		folded, _ := invariantRegistry(t)
		if _, err := folded.Restore(ctx, chain, invariantOwners{}); err != nil {
			t.Fatalf("fold the chain into a second registry: %v", err)
		}
		if err := folded.RestoreCheckpoint(ctx, payload); err != nil {
			t.Fatalf("put the account back beside a folded task: %v", err)
		}
		if _, status, err := folded.Lookup(ctx, taskID); err != nil || status != StatusFailed {
			t.Fatalf("the restored registry states (%v, %v) beside a chain that failed the task, want the folded status", status, err)
		}
	})

	t.Run("a payload the registry cannot read is refused by name", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		for name, payload := range map[string]string{
			"no identity":               `{"tasks":[{"status":1,"revision":1}]}`,
			"undeterminable":            `{"tasks":[{"task_id":"t1","status":0,"revision":1}]}`,
			"foreign mutation":          `{"tasks":[{"task_id":"t1","status":1,"revision":1,"mutations":[{"MutationID":"m1","TaskID":"t2"}]}]}`,
			"no identity of a mutation": `{"tasks":[{"task_id":"t1","status":1,"revision":1,"mutations":[{"TaskID":"t1"}]}]}`,
			"negative revision": func() string {
				return `{"tasks":[{"task_id":"t1","status":1,"revision":-1}]}`
			}(),
			"not json": `{not-a-payload`,
		} {
			if err := registry.RestoreCheckpoint(ctx, []byte(payload)); err == nil {
				t.Fatalf("the %s payload was accepted, want it refused", name)
			}
		}
		if len(registry.tasks) != 0 {
			t.Fatalf("the refusals restored %d task(s), want none", len(registry.tasks))
		}
		if coverage := registry.CheckpointCoverage(); coverage != nil {
			t.Fatalf("coverage = %v, want none beside a registry no checkpoint restored into", coverage)
		}
	})
}

// checkpointOf reports the state one registry states for a task, with the
// movement stamps read in UTC: a stamp carries a monotonic reading and a zone a
// JSON round trip does not carry, and the state a checkpoint states is the
// instant, never the reading it was taken with.
func (r *MemoryRegistry) checkpointOf(t *testing.T, taskID event.TaskID) StatedTask {
	t.Helper()
	entry, held := r.tasks[taskID]
	if !held {
		t.Fatalf("the registry holds no task %s", taskID)
	}
	stated := entry.checkpointOf(taskID)
	stated.AdmittedAt = stated.AdmittedAt.UTC().Round(0)
	stated.ChangedAt = stated.ChangedAt.UTC().Round(0)
	stated.RunSince = stated.RunSince.UTC().Round(0)
	stated.TTLAnchor = stated.TTLAnchor.UTC().Round(0)
	return stated
}

// TestRegistryCheckpointStatesTheAccountNotTheHistory pins what the slice is the size of.
//   - a settled task past the terminal bound leaves the account, so the snapshot stops stating it
//   - the state the checkpoint carries is the account the registry keeps, never the chains it settled
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRegistryCheckpointStatesTheAccountNotTheHistory(t *testing.T) {
	ctx := context.Background()
	clock := &invariantClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	commit := NewMemoryCommit(nil)
	registry := NewMemoryRegistry(commit, NewMemoryAdmission(), WithClock(clock.now), WithTerminalTTL(time.Hour))
	var taskIDs []event.TaskID
	for round := 0; round < 6; round++ {
		receipt, err := registry.Register(ctx, invariantTaskSpec("call-"+string(rune('a'+round)), "dedup-"+string(rune('a'+round))))
		if err != nil {
			t.Fatalf("admit the task of round %d: %v", round, err)
		}
		taskIDs = append(taskIDs, receipt.TaskID)
		if result, err := registry.Advance(ctx, receipt.TaskID, StatusSettled, "settled", nil); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task of round %d = (%v, %v), want a committed change", round, result, err)
		}
	}
	kept, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	var stated RegistryCheckpoint
	if err := json.Unmarshal(kept, &stated); err != nil {
		t.Fatalf("the snapshot states no readable account: %v", err)
	}
	if len(stated.Tasks) != len(taskIDs) {
		t.Fatalf("the snapshot states %d task(s), want the %d the registry holds", len(stated.Tasks), len(taskIDs))
	}
	clock.at = clock.at.Add(2 * time.Hour)
	if dropped := registry.PruneTerminal(clock.at); dropped != len(taskIDs) {
		t.Fatalf("the sweep dropped %d task(s), want the %d that stood past the bound", dropped, len(taskIDs))
	}
	swept, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the swept account: %v", err)
	}
	var after RegistryCheckpoint
	if err := json.Unmarshal(swept, &after); err != nil {
		t.Fatalf("the swept snapshot states no readable account: %v", err)
	}
	if len(after.Tasks) != 0 {
		t.Fatalf("the swept snapshot still states %d task(s), want the account alone", len(after.Tasks))
	}
}

// invariantClock is a clock one test moves by hand.
type invariantClock struct {
	at time.Time
}

// now reports the moment the clock stands at.
func (c *invariantClock) now() time.Time { return c.at }

// TestRestoredTaskKeepsTheRevisionTheTailExpects pins the fold boundary.
// - the restored revision is the one the next link expects, so the tail a boot folds follows the account
// - a link the account already covers is not part of the tail
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRestoredTaskKeepsTheRevisionTheTailExpects(t *testing.T) {
	ctx := context.Background()
	source, _ := invariantRegistry(t)
	taskID := invariantRegister(t, source, "call-tail", "dedup-tail")
	if result, err := source.Advance(ctx, taskID, StatusRunning, "running-tail", nil); err != nil || result != CommitCommitted {
		t.Fatalf("move the task under way = (%v, %v), want a committed change", result, err)
	}
	payload, err := source.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	restored, _ := invariantRegistry(t)
	if err := restored.RestoreCheckpoint(ctx, payload); err != nil {
		t.Fatalf("put the account back: %v", err)
	}
	chain := []TaskRecord{
		{Spec: invariantTaskSpec("call-tail", "dedup-tail"), Fact: CommitRecord{TaskID: taskID, MutationID: "settle-tail", ExpectedRevision: 2, Status: StatusSettled}},
	}
	report, err := restored.Restore(ctx, chain, invariantOwners{})
	if err != nil {
		t.Fatalf("fold the tail onto the restored account: %v", err)
	}
	if len(report.Tasks) != 1 || report.Tasks[0].Refused != nil {
		t.Fatalf("the tail fold reported %+v, want the one task rebuilt", report.Tasks)
	}
	if _, status, err := restored.Lookup(ctx, taskID); err != nil || status != StatusSettled {
		t.Fatalf("the folded task states (%v, %v), want the settled tail", status, err)
	}
}

// statedSettleAccount is a receipt account one test states by hand: the three
// families one task holds, keyed by the task they answer.
type statedSettleAccount map[event.TaskID]Receipts

// ReceiptsOf answers the receipts of one task.
func (a statedSettleAccount) ReceiptsOf(taskID event.TaskID) Receipts { return a[taskID] }

// TestTaskSliceStatesTheSettleAccount pins what a task checkpoint states beside the registry's own account.
//   - the three receipt families of a task the checkpoint names are stated entry for entry, and a payload round trip restores the same account
//   - a task beside no receipt states the three families as empty lists, never as missing keys, so a quiet account cannot be told from an older checkpoint
//   - the tasks the payload states are unchanged in every other field, so the receipts cost the checkpoint no part of the account it already carried
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTaskSliceStatesTheSettleAccount(t *testing.T) {
	ctx := context.Background()
	source, _ := invariantRegistry(t)
	withReceipts := invariantRegister(t, source, "call-account", "dedup-account")
	without := invariantRegister(t, source, "call-quiet", "dedup-quiet")
	payload, err := source.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	refused := time.Date(2026, 11, 6, 12, 30, 0, 0, time.UTC)
	account := statedSettleAccount{
		withReceipts: Receipts{
			Consumed: []StatedConsumption{{
				Notice: Notice{TaskID: withReceipts, Round: 1, Kind: NoticeSettled, Seq: 2},
				Event:  "event-consumed",
			}},
			Attempts: []StatedAttempt{{
				Notice:   Notice{TaskID: withReceipts, Round: 1, Kind: NoticeSettled, Seq: 2},
				Terminal: "completed",
			}},
			Rejected: []StatedRefusal{{
				Notice:   Notice{TaskID: withReceipts, Round: 2, Kind: NoticeAccepted, Seq: 0},
				Target:   event.Address{AgentID: "agent-a", SessionID: "session-account"},
				Instance: "instance-account",
				Reason:   "the target answered no",
				At:       refused,
			}},
		},
	}
	stated, err := StateSettleAccount(ctx, payload, account)
	if err != nil {
		t.Fatalf("state the settle account: %v", err)
	}
	var wire RegistryCheckpoint
	if err := json.Unmarshal(stated, &wire); err != nil {
		t.Fatalf("the stated payload carries no readable account: %v", err)
	}
	byTask := map[event.TaskID]StatedTask{}
	for _, entry := range wire.Tasks {
		byTask[entry.TaskID] = entry
	}
	held, carried := byTask[withReceipts]
	if !carried {
		t.Fatalf("the stated payload names no task %s", withReceipts)
	}
	if !reflect.DeepEqual(held.Consumed, account[withReceipts].Consumed) {
		t.Fatalf("the stated consumption receipts = %+v, want the account's %+v", held.Consumed, account[withReceipts].Consumed)
	}
	if !reflect.DeepEqual(held.Attempts, account[withReceipts].Attempts) {
		t.Fatalf("the stated attempts = %+v, want the account's %+v", held.Attempts, account[withReceipts].Attempts)
	}
	if !reflect.DeepEqual(held.Rejected, account[withReceipts].Rejected) {
		t.Fatalf("the stated refusals = %+v, want the account's %+v", held.Rejected, account[withReceipts].Rejected)
	}
	quiet, carried := byTask[without]
	if !carried {
		t.Fatalf("the stated payload names no task %s", without)
	}
	if quiet.Consumed == nil || quiet.Attempts == nil || quiet.Rejected == nil {
		t.Fatalf("the quiet task states %+v, want the three families as empty lists rather than missing keys", quiet)
	}
	var rendered map[string]json.RawMessage
	if err := json.Unmarshal(stated, &rendered); err != nil {
		t.Fatalf("read the stated payload as a document: %v", err)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(rendered["tasks"], &entries); err != nil {
		t.Fatalf("read the stated tasks: %v", err)
	}
	for _, entry := range entries {
		if string(entry["task_id"]) != `"`+string(without)+`"` {
			continue
		}
		for _, family := range []string{"consumed", "attempts", "rejected"} {
			stated, carried := entry[family]
			if !carried || string(stated) != "[]" {
				t.Fatalf("the quiet task states the %s family as %s, want an empty list", family, string(stated))
			}
		}
	}
	restored, _ := invariantRegistry(t)
	if err := restored.RestoreCheckpoint(ctx, stated); err != nil {
		t.Fatalf("put the account back: %v", err)
	}
	if got, want := restored.checkpointOf(t, withReceipts), source.checkpointOf(t, withReceipts); !reflect.DeepEqual(got, want) {
		t.Fatalf("the restored record = %+v, want the record the source held %+v", got, want)
	}
}

// TestTaskSliceRefusesAReceiptOfAnotherTask pins the identity check of a stated receipt.
//   - a receipt filed under a task it does not answer refuses the restore by name, naming both identities
//   - the same receipt filed under the task it answers is accepted, so the refusal is the mismatch and not the family
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTaskSliceRefusesAReceiptOfAnotherTask(t *testing.T) {
	ctx := context.Background()
	source, _ := invariantRegistry(t)
	answered := invariantRegister(t, source, "call-answered", "dedup-answered")
	other := invariantRegister(t, source, "call-other", "dedup-other")
	payload, err := source.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	receipt := StatedConsumption{Notice: Notice{TaskID: other, Round: 0, Kind: NoticeSettled, Seq: 0}}
	under := func(taskID event.TaskID) []byte {
		var wire RegistryCheckpoint
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatalf("the snapshot states no readable account: %v", err)
		}
		for at := range wire.Tasks {
			if wire.Tasks[at].TaskID == taskID {
				wire.Tasks[at].Consumed = []StatedConsumption{receipt}
			}
		}
		body, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("state the receipt under %s: %v", taskID, err)
		}
		return body
	}
	restored, _ := invariantRegistry(t)
	err = restored.RestoreCheckpoint(ctx, under(answered))
	if err == nil {
		t.Fatalf("the restore accepted the receipt of %s filed under %s, want it refused by name", other, answered)
	}
	if !strings.Contains(err.Error(), string(other)) || !strings.Contains(err.Error(), string(answered)) {
		t.Fatalf("the refusal = %v, want both identities named", err)
	}
	if err := restored.RestoreCheckpoint(ctx, under(other)); err != nil {
		t.Fatalf("the restore refused the receipt of %s filed under %s: %v", other, other, err)
	}
}
