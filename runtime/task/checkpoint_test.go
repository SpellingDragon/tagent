package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// TestRegistryCheckpointStatesTheOpenAccountNotTheSettledHistory pins what the slice is the size of.
//   - a task the run settled states no entry, so the snapshot names the open work alone
//   - the sweep that drops the settled records changes the body by nothing, so what the checkpoint carries is never the chains it settled
//   - negative: the task still holding open work is stated, so the body is the open account rather than a slice that states nothing
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestRegistryCheckpointStatesTheOpenAccountNotTheSettledHistory(t *testing.T) {
	ctx := context.Background()
	clock := &invariantClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	commit := NewMemoryCommit(nil)
	registry := NewMemoryRegistry(commit, NewMemoryAdmission(), WithClock(clock.now), WithTerminalTTL(time.Hour))
	var settled []event.TaskID
	for round := 0; round < 6; round++ {
		receipt, err := registry.Register(ctx, invariantTaskSpec("call-"+string(rune('a'+round)), "dedup-"+string(rune('a'+round))))
		if err != nil {
			t.Fatalf("admit the task of round %d: %v", round, err)
		}
		settled = append(settled, receipt.TaskID)
		if result, err := registry.Advance(ctx, receipt.TaskID, StatusSettled, "settled", nil); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task of round %d = (%v, %v), want a committed change", round, result, err)
		}
	}
	open, err := registry.Register(ctx, invariantTaskSpec("call-open", "dedup-open"))
	if err != nil {
		t.Fatalf("admit the open task: %v", err)
	}
	statedTasks := func(payload []byte, reading string) []StatedTask {
		var wire RegistryCheckpoint
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatalf("the %s snapshot states no readable account", reading)
		}
		return wire.Tasks
	}
	kept, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	if stated := statedTasks(kept, "kept"); len(stated) != 1 || stated[0].TaskID != open.TaskID {
		t.Fatalf("the snapshot states %+v, want the open task %s alone beside %d settled one(s)", stated, open.TaskID, len(settled))
	}
	clock.at = clock.at.Add(2 * time.Hour)
	if dropped := registry.PruneTerminal(clock.at); dropped != len(settled) {
		t.Fatalf("the sweep dropped %d task(s), want the %d that stood past the bound", dropped, len(settled))
	}
	swept, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the swept account: %v", err)
	}
	if !bytes.Equal(swept, kept) {
		t.Fatalf("the swept snapshot states %s, want the %s the account stated beside the settled records: the body is the open work alone",
			swept, kept)
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

// TestCoveredTaskMustNotReissueASettledNoticeIdentity pins the notice sequencer across the two recovery paths.
//   - positive: the covered path issues the next identity past every envelope the account settled, as the chain fold does
//   - negative: the fold answers a higher sequence for the same account, so the two paths must agree
//   - consumer: RestoreCheckpoint on the covered path and Registry.Restore on the chain-fold path
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCoveredTaskMustNotReissueASettledNoticeIdentity(t *testing.T) {
	ctx := context.Background()
	commit := NewMemoryCommit(nil)
	registry := NewMemoryRegistry(commit, NewMemoryAdmission())
	receipt, err := registry.Register(ctx, invariantTaskSpec("call-seq", "dedup-seq"))
	if err != nil {
		t.Fatalf("admit the task: %v", err)
	}
	taskID := receipt.TaskID
	envelope, err := registry.IssueNotice(ctx, taskID, NoticeProgress, "fact-seq-1")
	if err != nil {
		t.Fatalf("issue the first notice: %v", err)
	}
	if result, err := registry.Advance(ctx, taskID, StatusRunning, "seq-mutation-0", []Envelope{envelope}); err != nil || result != CommitCommitted {
		t.Fatalf("commit the notice = (%v, %v)", result, err)
	}
	manager := NewMemoryManager(registry, nil)
	manager.acceptDelivery(envelope.Notice)
	registry.mu.Lock()
	open := 0
	for _, held := range registry.tasks[taskID].obligations {
		if held.obligation.Kind == ObligationAwaitingDelivery {
			open++
		}
	}
	round := registry.tasks[taskID].round
	registry.mu.Unlock()
	if open != 0 {
		t.Fatalf("the task still holds %d obligation(s) after acceptance, want the delivery obligation closed", open)
	}
	payload, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}

	covered := NewMemoryRegistry(NewMemoryCommit(nil), NewMemoryAdmission())
	if err := covered.RestoreCheckpoint(ctx, payload); err != nil {
		t.Fatalf("restore the checkpoint: %v", err)
	}
	reissued, err := covered.IssueNotice(ctx, taskID, NoticeProgress, "fact-seq-2")
	if err != nil {
		t.Fatalf("issue the next notice on the covered path: %v", err)
	}

	folded := NewMemoryRegistry(commit, NewMemoryAdmission())
	facts := commit.Facts(taskID)
	chain := make([]TaskRecord, 0, len(facts))
	for at, fact := range facts {
		link := TaskRecord{Fact: fact}
		if at == 0 {
			link.Spec = invariantTaskSpec("call-seq", "dedup-seq")
		}
		chain = append(chain, link)
	}
	if _, err := folded.Restore(ctx, chain, invariantOwners{}); err != nil {
		t.Fatalf("fold the chain: %v", err)
	}
	foldedIssue, err := folded.IssueNotice(ctx, taskID, NoticeProgress, "fact-seq-2")
	if err != nil {
		t.Fatalf("issue the next notice on the folded path: %v", err)
	}
	sameIdentity := reissued.Notice.TaskID == envelope.Notice.TaskID &&
		reissued.Notice.Round == envelope.Notice.Round &&
		reissued.Notice.Kind == envelope.Notice.Kind &&
		reissued.Notice.Seq == envelope.Notice.Seq
	if sameIdentity {
		t.Fatalf("the covered path re-issued the four-part identity %s of a notice the account already settled (only the event reference differs, and the identity a repeat is judged by is task/round/kind/seq): RestoreCheckpoint observes only the open obligations (runtime/task/checkpoint.go:267-269) while the fold observes every envelope (runtime/task/restore.go:293), so the two recovery paths hand out different identities for one account",
			envelope.Notice.FactKey())
	}
	if reissued.Notice.Seq != foldedIssue.Notice.Seq {
		t.Fatalf("the covered path issues sequence %d while the folded path issues %d for the same account, want the two recovery paths to agree",
			reissued.Notice.Seq, foldedIssue.Notice.Seq)
	}
	t.Logf("covered path issues seq=%d, folded path issues seq=%d, settled identity was seq=%d, task round=%d",
		reissued.Notice.Seq, foldedIssue.Notice.Seq, envelope.Notice.Seq, round)
}

// TestCoveredTaskKeepsTheIdentityOfANoticeItStillOwes pins the sequencer over the obligations a covered task holds open.
//   - positive: the covered path continues past the identity of an undelivered notice it still owes, as the chain fold does
//   - negative: a covered restore that observed no envelope re-issues that identity, so the accounts settled and still owed share one source
//   - consumer: RestoreCheckpoint on the covered path and Registry.Restore on the chain-fold path
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCoveredTaskKeepsTheIdentityOfANoticeItStillOwes(t *testing.T) {
	ctx := context.Background()
	commit := NewMemoryCommit(nil)
	registry := NewMemoryRegistry(commit, NewMemoryAdmission())
	receipt, err := registry.Register(ctx, invariantTaskSpec("call-owed-seq", "dedup-owed-seq"))
	if err != nil {
		t.Fatalf("admit the task: %v", err)
	}
	taskID := receipt.TaskID
	envelope, err := registry.IssueNotice(ctx, taskID, NoticeProgress, "fact-owed-1")
	if err != nil {
		t.Fatalf("issue the notice: %v", err)
	}
	if result, err := registry.Advance(ctx, taskID, StatusRunning, "owed-mutation-0", []Envelope{envelope}); err != nil || result != CommitCommitted {
		t.Fatalf("commit the notice = (%v, %v)", result, err)
	}
	payload, err := registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	covered := NewMemoryRegistry(NewMemoryCommit(nil), NewMemoryAdmission())
	if err := covered.RestoreCheckpoint(ctx, payload); err != nil {
		t.Fatalf("restore the checkpoint: %v", err)
	}
	reissued, err := covered.IssueNotice(ctx, taskID, NoticeProgress, "fact-owed-2")
	if err != nil {
		t.Fatalf("issue the next notice on the covered path: %v", err)
	}
	if reissued.Notice.FactKey() == envelope.Notice.FactKey() {
		t.Fatalf("the covered path re-issued the four-part identity %s of a notice the task still owes", envelope.Notice.FactKey())
	}
	folded := NewMemoryRegistry(commit, NewMemoryAdmission())
	facts := commit.Facts(taskID)
	chain := make([]TaskRecord, 0, len(facts))
	for at, fact := range facts {
		link := TaskRecord{Fact: fact}
		if at == 0 {
			link.Spec = invariantTaskSpec("call-owed-seq", "dedup-owed-seq")
		}
		chain = append(chain, link)
	}
	if _, err := folded.Restore(ctx, chain, invariantOwners{}); err != nil {
		t.Fatalf("fold the chain: %v", err)
	}
	foldedIssue, err := folded.IssueNotice(ctx, taskID, NoticeProgress, "fact-owed-2")
	if err != nil {
		t.Fatalf("issue the next notice on the folded path: %v", err)
	}
	if reissued.Notice.FactKey() != foldedIssue.Notice.FactKey() {
		t.Fatalf("the covered path issues %s while the folded path issues %s for the same account, want the two recovery paths to agree",
			reissued.Notice.FactKey(), foldedIssue.Notice.FactKey())
	}
}

// chainOf answers the restore links one commit holds for a task, so a test can
// stand in for the durable account a watermark is read back from.
type chainOf struct {
	commit *MemoryCommit
	specs  map[event.TaskID]Spec
}

// Records answers the chain of one task with the admission material on its first link.
func (c chainOf) Records(ctx context.Context, taskID event.TaskID) ([]TaskRecord, error) {
	facts := c.commit.Facts(taskID)
	chain := make([]TaskRecord, 0, len(facts))
	for at, fact := range facts {
		link := TaskRecord{Fact: fact}
		if at == 0 {
			link.Spec = c.specs[taskID]
		}
		chain = append(chain, link)
	}
	return chain, nil
}

// TestCheckpointStatesNoSettledTaskAtAll pins the size of the task slice.
//   - a task the run finished is stated by no entry, so the account carries no identity of a settled task at any depth
//   - the body costs the open work alone whatever the settled task committed, so it never carries the settled text
//   - negative one: a task still under way is stated whole, so the open work is what the body is made of
//   - negative two: a task that reached its terminal still owing a delivery is stated whole, because that obligation is open work
//   - a restored registry answers the declaration and status of a finished task from its chain the first time a reader names it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCheckpointStatesNoSettledTaskAtAll(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	const settledCount = 20
	var settled []event.TaskID
	for at := 0; at < settledCount; at++ {
		call := fmt.Sprintf("call-watermark-%02d", at)
		receipt, err := source.Register(ctx, Spec{
			CallID: call, DedupKey: DedupKey("dedup-" + call), InvocationID: "invocation-1",
			InstanceID: "instance-a", Description: strings.Repeat("settled", 400),
			Source: invariantTaskAddress, Target: invariantTaskAddress,
		})
		if err != nil {
			t.Fatalf("admit the task of %s: %v", call, err)
		}
		if result, err := source.Advance(ctx, receipt.TaskID, StatusRunning, MutationID("run-"+call), nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the task of %s under way = (%v, %v), want a committed change", call, result, err)
		}
		if result, err := source.Advance(ctx, receipt.TaskID, StatusSettled, MutationID("settle-"+call), nil); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task of %s = (%v, %v), want a committed change", call, result, err)
		}
		settled = append(settled, receipt.TaskID)
	}
	underway := invariantRegister(t, source, "call-watermark-open", "dedup-watermark-open")
	owing := invariantRegister(t, source, "call-watermark-owing", "dedup-watermark-owing")
	if result, err := source.Advance(ctx, owing, StatusSettled, "settle-owing", []Envelope{invariantEnvelope(owing, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task still owing a delivery = (%v, %v), want a committed change", result, err)
	}
	payload, err := source.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	var stated RegistryCheckpoint
	if err := json.Unmarshal(payload, &stated); err != nil {
		t.Fatalf("the snapshot states no readable account: %v", err)
	}
	byTask := make(map[event.TaskID]StatedTask, len(stated.Tasks))
	for _, entry := range stated.Tasks {
		byTask[entry.TaskID] = entry
	}
	for _, taskID := range settled {
		if entry, held := byTask[taskID]; held {
			t.Fatalf("the account states the finished task %s = %+v, want no entry: its record stays on the chain", taskID, entry)
		}
	}
	if got, want := len(stated.Tasks), 2; got != want {
		t.Fatalf("the account states %d entr(ies) beside %d finished task(s), want the %d tasks holding open work", got, settledCount, want)
	}
	if len(payload) >= settledCount*len(strings.Repeat("settled", 400))/2 {
		t.Fatalf("the account states %d byte(s) for %d finished tasks whose declarations carry %d byte(s) each, want the declarations left on the chain",
			len(payload), settledCount, len(strings.Repeat("settled", 400)))
	}
	if entry := byTask[underway]; entry.Spec.CallID != "call-watermark-open" || len(entry.Mutations) == 0 {
		t.Fatalf("the account states the task still under way as %+v, want its record whole", entry)
	}
	if entry := byTask[owing]; len(entry.Mutations) == 0 || len(entry.Obligations) == 0 {
		t.Fatalf("the account states the task that still owes a delivery as %+v, want its record whole beside that obligation", entry)
	}

	specs := make(map[event.TaskID]Spec, len(settled))
	for _, taskID := range settled {
		specs[taskID] = source.tasks[taskID].spec
	}
	restored := NewMemoryRegistry(commit, NewMemoryAdmission())
	restored.RestoreChainSource(chainOf{commit: commit, specs: specs})
	if err := restored.RestoreCheckpoint(ctx, payload); err != nil {
		t.Fatalf("put the account back: %v", err)
	}
	for _, taskID := range settled {
		gotSpec, gotStatus, err := restored.Lookup(ctx, taskID)
		if err != nil {
			t.Fatalf("read back the finished task %s by identity: %v", taskID, err)
		}
		if wantSpec, wantStatus, err := source.Lookup(ctx, taskID); err != nil || gotStatus != wantStatus {
			t.Fatalf("the task %s read back at %v beside the %v it settled at (%v)", taskID, gotStatus, wantStatus, err)
		} else if !reflect.DeepEqual(gotSpec, wantSpec) {
			t.Fatalf("the task %s read back its declaration %+v, want the %+v it was admitted under", taskID, gotSpec, wantSpec)
		}
	}
}

// TestRestartEntryPointsReadASettledTaskFromItsChain pins the account a restart keeps against the faces that move a task.
//   - positive: after a restart the finished task answers Advance and Cancel with the terminal refusal its own record holds
//   - positive: the cancelled task answers Resume with the cancellation refusal, and the settled task re-enters the lifecycle
//   - negative: a task no checkpoint stated answers ErrTaskNotFound from all three faces
//   - consumer: Registry.Advance, Registry.Cancel and Registry.Resume over the account a checkpoint restored
//
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestRestartEntryPointsReadASettledTaskFromItsChain(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	settled := invariantRegister(t, source, "call-restart-settled", "dedup-restart-settled")
	cancelled := invariantRegister(t, source, "call-restart-cancelled", "dedup-restart-cancelled")
	for name, ending := range map[string]struct {
		taskID event.TaskID
		status Status
	}{"settled": {settled, StatusSettled}, "cancelled": {cancelled, StatusCancelled}} {
		if result, err := source.Advance(ctx, ending.taskID, StatusRunning, MutationID("run-restart-"+name), nil); err != nil || result != CommitCommitted {
			t.Fatalf("move the %s task under way = (%v, %v), want a committed change", name, result, err)
		}
		if result, err := source.Advance(ctx, ending.taskID, ending.status, MutationID("end-restart-"+name), nil); err != nil || result != CommitCommitted {
			t.Fatalf("end the %s task = (%v, %v), want a committed change", name, result, err)
		}
	}
	payload, err := source.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	var stated RegistryCheckpoint
	if err := json.Unmarshal(payload, &stated); err != nil {
		t.Fatalf("the snapshot states no readable account: %v", err)
	}
	if len(stated.Tasks) != 0 {
		t.Fatalf("the account states %+v, want no entry: both tasks finished before the cut and stay on their chains", stated.Tasks)
	}

	specs := map[event.TaskID]Spec{settled: source.tasks[settled].spec, cancelled: source.tasks[cancelled].spec}
	restored := NewMemoryRegistry(commit, NewMemoryAdmission())
	restored.RestoreChainSource(chainOf{commit: commit, specs: specs})
	if err := restored.RestoreCheckpoint(ctx, payload); err != nil {
		t.Fatalf("put the account back: %v", err)
	}

	if _, err := restored.Advance(ctx, settled, StatusFailed, "after-restart-failed", nil); !errors.Is(err, ErrTerminalState) {
		t.Fatalf("the restart moved the settled task = %v, want %v", err, ErrTerminalState)
	}
	if _, err := restored.Advance(ctx, cancelled, StatusRunning, "after-restart-run", nil); !errors.Is(err, ErrTerminalState) {
		t.Fatalf("the restart moved the cancelled task = %v, want %v", err, ErrTerminalState)
	}
	if err := restored.Cancel(ctx, settled); !errors.Is(err, ErrTerminalState) {
		t.Fatalf("the restart cancelled the settled task = %v, want %v", err, ErrTerminalState)
	}
	if _, err := restored.Resume(ctx, cancelled, event.Event{Key: "after-restart-input"}); !errors.Is(err, ErrResumeCancelled) {
		t.Fatalf("the restart resumed the cancelled task = %v, want %v", err, ErrResumeCancelled)
	}
	receipt, err := restored.Resume(ctx, settled, event.Event{Key: "after-restart-input"})
	if err != nil || !receipt.Accepted || receipt.Kind != AdmissionFresh {
		t.Fatalf("the restart re-entered the settled task = (%+v, %v), want a fresh admission", receipt, err)
	}
	if _, status, err := restored.Lookup(ctx, settled); err != nil || status != StatusRunning {
		t.Fatalf("the re-entered task stands at (%v, %v), want the round the resume opened", status, err)
	}

	stranger := event.TaskID("task-no-checkpoint-named")
	if _, err := restored.Advance(ctx, stranger, StatusRunning, "stranger-run", nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Advance of a task no checkpoint stated = %v, want %v", err, ErrTaskNotFound)
	}
	if err := restored.Cancel(ctx, stranger); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Cancel of a task no checkpoint stated = %v, want %v", err, ErrTaskNotFound)
	}
	if _, err := restored.Resume(ctx, stranger, event.Event{Key: "stranger-input"}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Resume of a task no checkpoint stated = %v, want %v", err, ErrTaskNotFound)
	}
}

// TestSettledTasksAddNoCheckpointByte pins the size of the task slice at four depths of a settled history.
//   - the account states the open work alone at 8, 32, 128 and 512 finished tasks, so no finished identity enters the body
//   - the body of the four depths is byte-identical, so a finished task costs the slice no byte at any depth
//   - the sweep that drops every finished record changes the body by nothing, so the size of the account a registry holds is not the size of the body
//   - negative: the open work is stated whole beside that history, so the equality stands beside an account stating entries rather than beside an empty one
//   - consumer: SnapshotCheckpoint, the task slice the framework checkpoint files
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestSettledTasksAddNoCheckpointByte(t *testing.T) {
	ctx := context.Background()
	registry := NewMemoryRegistry(NewMemoryCommit(nil), NewMemoryAdmission(), WithTerminalTTL(time.Hour))
	open := make([]event.TaskID, 0, 2)
	for _, call := range []string{"open-a", "open-b"} {
		receipt, err := registry.Register(ctx, invariantTaskSpec("call-"+call, "dedup-"+call))
		if err != nil {
			t.Fatalf("admit the open task %s: %v", call, err)
		}
		open = append(open, receipt.TaskID)
	}
	statedBody := func(registry *MemoryRegistry, depth int) []byte {
		payload, err := registry.SnapshotCheckpoint(ctx)
		if err != nil {
			t.Fatalf("state the account beside %d finished task(s): %v", depth, err)
		}
		var wire RegistryCheckpoint
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatalf("the account beside %d finished task(s) states no readable payload", depth)
		}
		if len(wire.Tasks) != len(open) {
			t.Fatalf("the account beside %d finished task(s) states %d entr(ies), want the %d open task(s) alone",
				depth, len(wire.Tasks), len(open))
		}
		return payload
	}
	settle := func(registry *MemoryRegistry, at int) {
		taskID := invariantRegister(t, registry, fmt.Sprintf("call-depth-%03d", at), fmt.Sprintf("dedup-depth-%03d", at))
		if result, err := registry.Advance(ctx, taskID, StatusSettled, MutationID(fmt.Sprintf("settle-depth-%03d", at)), nil); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task at %d = (%v, %v), want a committed change", at, result, err)
		}
	}
	depths := []int{8, 32, 128, 512}
	bodies := make([][]byte, len(depths))
	finished := 0
	for at, depth := range depths {
		for ; finished < depth; finished++ {
			settle(registry, finished)
		}
		bodies[at] = statedBody(registry, depth)
	}
	for at := 1; at < len(depths); at++ {
		if !bytes.Equal(bodies[at], bodies[0]) {
			t.Fatalf("the checkpoint states %d byte(s) beside %d finished task(s) beside the %d byte(s) it states beside %d, want the same body at every depth: a finished task costs the slice no byte",
				len(bodies[at]), depths[at], len(bodies[0]), depths[0])
		}
	}
	if dropped := registry.PruneTerminal(time.Now().Add(2 * time.Hour)); dropped != depths[len(depths)-1] {
		t.Fatalf("the sweep dropped %d record(s), want the %d the registry held past the bound", dropped, depths[len(depths)-1])
	}
	if swept := statedBody(registry, depths[len(depths)-1]); !bytes.Equal(swept, bodies[len(bodies)-1]) {
		t.Fatalf("the account whose sweep dropped every finished record states %s, want the %s it stated while holding them: the body is the open work alone",
			swept, bodies[len(bodies)-1])
	}
}
