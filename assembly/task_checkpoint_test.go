// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package assembly

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
)

// taskCheckpointSpec builds one task declaration the fixture admits.
func taskCheckpointSpec(callID, dedup string) task.Spec {
	return task.Spec{
		CallID:       callID,
		DedupKey:     task.DedupKey(dedup),
		InvocationID: "invocation-task-checkpoint",
		InstanceID:   "instance-task-checkpoint",
		Description:  "one admitted call",
		Source:       deliveryTestAddress("session-task-checkpoint"),
		Target:       deliveryTestAddress("session-task-checkpoint"),
		Execution:    task.Serialization("session-serialized"),
	}
}

// settleTasks admits and settles one task per call and answers the identities the
// registry minted, in admission order.
func settleTasks(t *testing.T, ctx context.Context, run *Run, calls []string) []event.TaskID {
	t.Helper()
	ids := make([]event.TaskID, 0, len(calls))
	for _, call := range calls {
		receipt, err := run.registry.Register(ctx, taskCheckpointSpec(call, "dedup-"+call))
		if err != nil {
			t.Fatalf("admit the task of %s: %v", call, err)
		}
		if receipt.Kind != task.AdmissionFresh {
			t.Fatalf("admission of %s = %+v, want a fresh admission", call, receipt)
		}
		if result, err := run.registry.Advance(ctx, receipt.TaskID, task.StatusSettled, task.MutationID("settle-"+call), nil); err != nil || result != task.CommitCommitted {
			t.Fatalf("settle the task of %s = (%v, %v), want a committed change", call, result, err)
		}
		ids = append(ids, receipt.TaskID)
	}
	return ids
}

// taskCheckpointCalls names the calls of one fixture round.
func taskCheckpointCalls(prefix string, count int) []string {
	calls := make([]string, 0, count)
	for at := 0; at < count; at++ {
		calls = append(calls, prefix+"-"+string(rune('a'+at)))
	}
	return calls
}

// recoveredTasksByID reports the outcomes of one boot by the task they state, so a
// check compares the two recovery paths task by task.
func recoveredTasksByID(report task.RestoreReport) map[event.TaskID]task.RestoreOutcome {
	out := make(map[event.TaskID]task.RestoreOutcome, len(report.Tasks))
	for _, outcome := range report.Tasks {
		out[outcome.TaskID] = outcome
	}
	return out
}

// statedTaskOf reports the account one run states for a task, with the movement
// stamps read in UTC so the comparison names the instant and not the reading.
func statedTaskOf(t *testing.T, ctx context.Context, run *Run, taskID event.TaskID) task.StatedTask {
	t.Helper()
	payload, err := run.registry.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account of the run: %v", err)
	}
	var account task.RegistryCheckpoint
	if err := json.Unmarshal(payload, &account); err != nil {
		t.Fatalf("the account states no readable checkpoint: %v", err)
	}
	for _, stated := range account.Tasks {
		if stated.TaskID != taskID {
			continue
		}
		stated.AdmittedAt = stated.AdmittedAt.UTC().Round(0)
		stated.ChangedAt = stated.ChangedAt.UTC().Round(0)
		stated.RunSince = stated.RunSince.UTC().Round(0)
		stated.TTLAnchor = stated.TTLAnchor.UTC().Round(0)
		return stated
	}
	t.Fatalf("the account states no task %s", taskID)
	return task.StatedTask{}
}

// TestTaskRestoreBoundedByCheckpoint pins the recovery cost of the task account.
//   - a boot over a checkpointed store reads no chain of the tasks the checkpoint states
//   - a chain younger than the cut is folded whole, so the fallback still restores what the cut did not cover
//   - the account the boot restores states every task the source process held, field for field
//   - the task the checkpoint restored answers the state a fold of its own chain answers, the two paths stating one account
//   - the same store beside no checkpoint folds every chain (negative: the bounded read is the checkpoint's, not a dropped chain)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTaskRestoreBoundedByCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	const coveredCount, youngCount = 8, 3
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	addr := deliveryTestAddress("session-task-checkpoint")
	source := snapshotTestRun(t, ctx, storage())
	commitWindowFact(t, ctx, source, addr, windowEvent(addr, "tc-in", "inv-task-checkpoint", "the one fact of the chain", event.TypeExternalInput))
	covered := settleTasks(t, ctx, source, taskCheckpointCalls("covered", coveredCount))
	if _, stated, err := source.Checkpoint(ctx); err != nil || !stated {
		t.Fatalf("file the checkpoint of the account = (%t, %v), want a filed fact", stated, err)
	}
	if id, _ := source.CheckpointFiled(); id == "" {
		t.Fatal("the source run filed no checkpoint to cut the account at")
	}
	young := settleTasks(t, ctx, source, taskCheckpointCalls("young", youngCount))
	want := map[event.TaskID]task.StatedTask{}
	for _, taskID := range append(append([]event.TaskID{}, covered...), young...) {
		want[taskID] = statedTaskOf(t, ctx, source, taskID)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	observer := &foldObserver{}
	original := trpclog.Default
	trpclog.Default = observer
	t.Cleanup(func() { trpclog.Default = original })
	booted := snapshotTestRun(t, ctx, storage())
	if !observer.mentions("restore folded", "chains=3", "links=6", "covered=8") {
		t.Fatalf("info lines = %v, want the fold beside the chains it read and the tasks the checkpoint covered", observer.lines)
	}
	report := recoveredTasksByID(booted.Recovery())
	for _, taskID := range covered {
		outcome, held := report[taskID]
		if !held {
			t.Fatalf("the recovery report states no task %s, want every task of the account named", taskID)
		}
		if outcome.Status != task.StatusSettled {
			t.Fatalf("the recovered task %s stands at %v, want the settled state the source held", taskID, outcome.Status)
		}
		if got := statedTaskOf(t, ctx, booted, taskID); !sameStatedTask(got, want[taskID]) {
			t.Fatalf("the booted task %s = %+v, want the state the source process held %+v", taskID, got, want[taskID])
		}
	}
	for _, taskID := range young {
		outcome, held := report[taskID]
		if !held || !outcome.Recovered {
			t.Fatalf("the recovery report states %+v for %s, want the young chain folded", outcome, taskID)
		}
		if got := statedTaskOf(t, ctx, booted, taskID); !sameChainState(got, want[taskID]) {
			t.Fatalf("the folded task %s = %+v, want the state its chain states %+v", taskID, got, want[taskID])
		}
	}
	names := make([]event.TaskID, 0, len(report))
	for taskID := range report {
		names = append(names, taskID)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	if len(names) != coveredCount+youngCount {
		t.Fatalf("the report names %d task(s), want the %d the source held", len(names), coveredCount+youngCount)
	}
	durable, held := booted.commit.inner.(*state.DurableCommit)
	if !held {
		t.Fatal("the booted run holds no durable commit to fold the chain through")
	}
	chain, err := durable.Records(ctx, covered[0])
	if err != nil {
		t.Fatalf("read the chain of the covered task: %v", err)
	}
	refolded := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
	if _, err := refolded.Restore(ctx, chain, runOwners{bindings: booted.bindings}); err != nil {
		t.Fatalf("fold the chain of the covered task: %v", err)
	}
	foldedAccount, err := refolded.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the folded account: %v", err)
	}
	var refoldedCheckpoint task.RegistryCheckpoint
	if err := json.Unmarshal(foldedAccount, &refoldedCheckpoint); err != nil {
		t.Fatalf("the folded account states no readable checkpoint: %v", err)
	}
	if len(refoldedCheckpoint.Tasks) != 1 {
		t.Fatalf("the fold states %d task(s), want the one chain it read", len(refoldedCheckpoint.Tasks))
	}
	if got := statedTaskOf(t, ctx, booted, covered[0]); !sameChainState(got, refoldedCheckpoint.Tasks[0]) {
		t.Fatalf("the checkpoint restored %+v, want the state the fold of the same chain answers %+v",
			got, refoldedCheckpoint.Tasks[0])
	}
}

// TestTaskRestoreFoldsTheTailOfACoveredChain pins the coverage judgement at its boundary.
//   - the chain of a task the checkpoint states is folded from the link at the stated revision alone, so the links the entry covers are never read again
//   - the booted task stands at the state the tail states, not the one the checkpoint states (negative: a boot that dropped the tail answers the admitted state)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTaskRestoreFoldsTheTailOfACoveredChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	addr := deliveryTestAddress("session-task-tail")
	source := snapshotTestRun(t, ctx, storage())
	commitWindowFact(t, ctx, source, addr, windowEvent(addr, "tt-in", "inv-task-tail", "the one fact of the chain", event.TypeExternalInput))
	receipt, err := source.registry.Register(ctx, taskCheckpointSpec("tail-a", "dedup-tail-a"))
	if err != nil || receipt.Kind != task.AdmissionFresh {
		t.Fatalf("admit the task = (%+v, %v), want a fresh admission", receipt, err)
	}
	if _, stated, err := source.Checkpoint(ctx); err != nil || !stated {
		t.Fatalf("file the checkpoint of the account = (%t, %v), want a filed fact", stated, err)
	}
	cut := statedTaskOf(t, ctx, source, receipt.TaskID)
	if cut.Status != task.StatusAccepted || cut.Revision != 1 {
		t.Fatalf("the checkpoint states the task %+v, want the admitted state at revision 1", cut)
	}
	if result, err := source.registry.Advance(ctx, receipt.TaskID, task.StatusSettled, task.MutationID("settle-tail-a"), nil); err != nil || result != task.CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}
	want := statedTaskOf(t, ctx, source, receipt.TaskID)
	if want.Revision != cut.Revision+1 {
		t.Fatalf("the settled task stands at revision %d, want the one link past the revision the checkpoint states %d",
			want.Revision, cut.Revision)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	observer := &foldObserver{}
	original := trpclog.Default
	trpclog.Default = observer
	t.Cleanup(func() { trpclog.Default = original })
	booted := snapshotTestRun(t, ctx, storage())
	if !observer.mentions("restore folded", "chains=1", "links=1", "covered=0") {
		t.Fatalf("info lines = %v, want the fold of the one link past the cut beside no link the checkpoint covers", observer.lines)
	}
	report := recoveredTasksByID(booted.Recovery())
	outcome, held := report[receipt.TaskID]
	if !held || outcome.Status != task.StatusSettled {
		t.Fatalf("the recovery report states %+v for %s, want the settled state the tail carries", outcome, receipt.TaskID)
	}
	if got := statedTaskOf(t, ctx, booted, receipt.TaskID); !sameChainState(got, want) {
		t.Fatalf("the booted task = %+v, want the state the tail states %+v", got, want)
	}
}

// TestTaskRestoreWithoutCheckpointFoldsEveryChain pins the fallback the bootstrap keeps.
//   - a store whose process filed no checkpoint folds every chain it holds, so the two paths answer the same account
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTaskRestoreWithoutCheckpointFoldsEveryChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	const taskCount = 4
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	source := snapshotTestRun(t, ctx, storage())
	tasks := settleTasks(t, ctx, source, taskCheckpointCalls("young", taskCount))
	want := map[event.TaskID]task.StatedTask{}
	for _, taskID := range tasks {
		want[taskID] = statedTaskOf(t, ctx, source, taskID)
	}
	if id, _ := source.CheckpointFiled(); id != "" {
		t.Fatalf("the source filed the checkpoint %s, want a store no checkpoint covers", id)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	observer := &foldObserver{}
	original := trpclog.Default
	trpclog.Default = observer
	t.Cleanup(func() { trpclog.Default = original })
	booted := snapshotTestRun(t, ctx, storage())
	if !observer.mentions("restore folded", "chains=4", "links=8", "covered=0") {
		t.Fatalf("info lines = %v, want the fold of every chain beside no checkpoint", observer.lines)
	}
	report := recoveredTasksByID(booted.Recovery())
	for _, taskID := range tasks {
		outcome, held := report[taskID]
		if !held || !outcome.Recovered {
			t.Fatalf("the fallback report states %+v for %s, want the task rebuilt from its chain", outcome, taskID)
		}
		if got := statedTaskOf(t, ctx, booted, taskID); !sameChainState(got, want[taskID]) {
			t.Fatalf("the fallback restored %s = %+v, want the state its chain states %+v", taskID, got, want[taskID])
		}
	}
}

// TestTaskTailFoldLeavesNoSweepStamp pins the movement stamp of a record a tail fold moved.
//   - a task the checkpoint states beside a settling link past the cut stands at the state the tail states
//   - the movement stamp of that record is unknown, not the one the checkpoint stated before the cut
//   - the terminal sweep keeps the record as it keeps one rebuilt from its chain alone (negative: the stale stamp would stand past the bound and drop it)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTaskTailFoldLeavesNoSweepStamp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	const ttl = time.Hour
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	addr := deliveryTestAddress("session-task-tail-stamp")
	source := snapshotTestRun(t, ctx, storage, withTaskTerminalTTL(ttl))
	commitWindowFact(t, ctx, source, addr, windowEvent(addr, "ts-in", "inv-task-tail-stamp", "the one fact of the chain", event.TypeExternalInput))
	receipt, err := source.registry.Register(ctx, taskCheckpointSpec("tail-stamp-a", "dedup-tail-stamp-a"))
	if err != nil || receipt.Kind != task.AdmissionFresh {
		t.Fatalf("admit the task = (%+v, %v), want a fresh admission", receipt, err)
	}
	if _, stated, err := source.Checkpoint(ctx); err != nil || !stated {
		t.Fatalf("file the checkpoint of the account = (%t, %v), want a filed fact", stated, err)
	}
	cut := statedTaskOf(t, ctx, source, receipt.TaskID)
	if cut.ChangedAt.IsZero() {
		t.Fatalf("the checkpoint states the task %+v, want the admission stamp it was moved at", cut)
	}
	if result, err := source.registry.Advance(ctx, receipt.TaskID, task.StatusSettled, task.MutationID("settle-tail-stamp-a"), nil); err != nil || result != task.CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	booted := snapshotTestRun(t, ctx, storage, withTaskTerminalTTL(time.Nanosecond))
	restored := statedTaskOf(t, ctx, booted, receipt.TaskID)
	if restored.Status != task.StatusSettled || restored.Revision != cut.Revision+1 {
		t.Fatalf("the booted task = %+v, want the settled state of the one link the tail carries past revision %d", restored, cut.Revision)
	}
	if !restored.ChangedAt.IsZero() {
		t.Fatalf("the booted task carries the movement stamp %v, want none: the link the tail folded states no instant", restored.ChangedAt)
	}
	if _, err := booted.registry.Register(ctx, taskCheckpointSpec("after-boot", "dedup-after-boot")); err != nil {
		t.Fatalf("admit the task beside the settled record: %v", err)
	}
	if _, status, err := booted.registry.Lookup(ctx, receipt.TaskID); err != nil || status != task.StatusSettled {
		t.Fatalf("the settled record reads as (%v, %v) beside the admission that swept, want it kept", status, err)
	}
	if dropped := booted.registry.PruneTerminal(time.Now()); dropped != 0 {
		t.Fatalf("the sweep dropped %d task(s) by the stamp the checkpoint stated before the cut, want the record kept", dropped)
	}
}

// sameStatedTask reports whether two states of one task agree on every field the
// checkpoint carries.
func sameStatedTask(got, want task.StatedTask) bool {
	return statedStateOf(got) == statedStateOf(want)
}

// sameChainState reports whether two states of one task agree on every field a
// chain fold can answer: the movement stamps a link carries no record of stand
// outside the comparison, because no fold computes them back.
func sameChainState(got, want task.StatedTask) bool {
	strip := func(stated task.StatedTask) string {
		stated.AdmittedAt = time.Time{}
		stated.ChangedAt = time.Time{}
		stated.RunSince = time.Time{}
		stated.TTLAnchor = time.Time{}
		return statedStateOf(stated)
	}
	return strip(got) == strip(want)
}

// statedStateOf renders one state of a task as the bytes a checkpoint states.
func statedStateOf(stated task.StatedTask) string {
	payload, err := json.Marshal(stated)
	if err != nil {
		return ""
	}
	return string(payload)
}
