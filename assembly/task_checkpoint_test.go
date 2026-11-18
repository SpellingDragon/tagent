// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package assembly

import (
	"context"
	"encoding/json"
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

// settledState is what one finished task's chain states about it: the declaration
// and status a reader names it by, the revision its last link leaves and the
// mutations its links commit. A task the account seats nowhere answers this and no
// stored entry, because the fields a checkpoint states beyond these are the open
// work alone.
type settledState struct {
	spec      task.Spec
	status    task.Status
	revision  int64
	mutations []task.MutationID
}

// chainStateOf answers the state one task's chain states, read by its identity
// through the run's own registry and commit: the registry names the declaration
// and the status by hydrating the chain, and the links name the revisions they
// applied.
func chainStateOf(t *testing.T, ctx context.Context, run *Run, taskID event.TaskID) settledState {
	t.Helper()
	durable, held := run.commit.inner.(*state.DurableCommit)
	if !held {
		t.Fatalf("the run holds the commit %T, which keeps no chain to read the task %s from", run.commit.inner, taskID)
	}
	chain, err := durable.Records(ctx, taskID)
	if err != nil {
		t.Fatalf("read the chain of %s by identity: %v", taskID, err)
	}
	if len(chain) == 0 {
		t.Fatalf("the chain of %s carries no link, want the facts it committed", taskID)
	}
	spec, status, err := run.registry.Lookup(ctx, taskID)
	if err != nil {
		t.Fatalf("name the task %s by identity: %v", taskID, err)
	}
	mutations := make([]task.MutationID, 0, len(chain))
	for _, link := range chain {
		mutations = append(mutations, link.Fact.MutationID)
	}
	return settledState{spec: spec, status: status, revision: chain[len(chain)-1].Fact.ExpectedRevision + 1, mutations: mutations}
}

// sameSettledState reports whether two reads of one finished task agree on every
// field its chain states.
func sameSettledState(got, want settledState) bool {
	return settledStateOf(got) == settledStateOf(want)
}

// settledStateOf renders one finished task's state as the bytes its chain states.
func settledStateOf(state settledState) string {
	payload, err := json.Marshal(struct {
		Spec      task.Spec
		Status    task.Status
		Revision  int64
		Mutations []task.MutationID
	}{state.spec, state.status, state.revision, state.mutations})
	if err != nil {
		return ""
	}
	return string(payload)
}

// accountStates reports whether the account one run states carries one task: the
// read that answers true for the open work and false for a task on its chain alone.
func accountStates(t *testing.T, ctx context.Context, run *Run, taskID event.TaskID) bool {
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
		if stated.TaskID == taskID {
			return true
		}
	}
	return false
}

// TestTaskRestoreBoundedByCheckpoint pins the recovery cost of the task account.
//   - a boot over a checkpointed store reads no chain of a task that finished behind the cut, so its fold names the work past the cut alone
//   - a chain younger than the cut is folded whole, so the fallback still restores what the cut did not cover
//   - the account the boot seats carries the open work past the cut and no task the source finished before it
//   - the query face names every task the source process held, and each one reads its settled state back from its own chain
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
	want := map[event.TaskID]settledState{}
	for _, taskID := range append(append([]event.TaskID{}, covered...), young...) {
		want[taskID] = chainStateOf(t, ctx, source, taskID)
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	observer := &foldObserver{}
	original := trpclog.Default
	trpclog.Default = observer
	t.Cleanup(func() { trpclog.Default = original })
	booted := snapshotTestRun(t, ctx, storage())
	if !observer.mentions("restore folded", "chains=3", "links=6", "covered=0") {
		t.Fatalf("info lines = %v, want the fold of the three chains past the cut beside no chain the checkpoint covers: every task it would state finished before the cut", observer.lines)
	}
	report := recoveredTasksByID(booted.Recovery())
	if len(report) != youngCount {
		t.Fatalf("the recovery report names %d task(s), want the %d the account seats past the cut: a task that finished before the newest checkpoint is enumerated nowhere", len(report), youngCount)
	}
	for _, taskID := range covered {
		if _, held := report[taskID]; held {
			t.Fatalf("the recovery report names the finished task %s, want no trace of it: it finished before the newest checkpoint and costs no enumeration", taskID)
		}
		if accountStates(t, ctx, booted, taskID) {
			t.Fatalf("the account states the finished task %s, want the open work alone: its record stays on its chain and is read back by identity", taskID)
		}
		if got := chainStateOf(t, ctx, booted, taskID); !sameSettledState(got, want[taskID]) {
			t.Fatalf("the booted task %s = %+v, want the state its chain states %+v", taskID, got, want[taskID])
		}
	}
	for _, taskID := range young {
		outcome, held := report[taskID]
		if !held || !outcome.Recovered {
			t.Fatalf("the recovery report states %+v for %s, want the young chain folded", outcome, taskID)
		}
		if got := chainStateOf(t, ctx, booted, taskID); !sameSettledState(got, want[taskID]) {
			t.Fatalf("the folded task %s = %+v, want the state its chain states %+v", taskID, got, want[taskID])
		}
	}
	readings, err := runReading{run: booted}.Tasks(ctx)
	if err != nil {
		t.Fatalf("read the tasks the storage behind the boot holds: %v", err)
	}
	if len(readings) != coveredCount+youngCount {
		t.Fatalf("the task reading names %d task(s), want the %d the source held", len(readings), coveredCount+youngCount)
	}
	for _, reading := range readings {
		if reading.Status != task.StatusSettled {
			t.Fatalf("the task reading states %s at %v, want the settled state the source held", reading.TaskID, reading.Status)
		}
	}
	durable, held := booted.commit.inner.(*state.DurableCommit)
	if !held {
		t.Fatal("the booted run holds no durable commit to fold the chain through")
	}
	chain, err := durable.Records(ctx, covered[0])
	if err != nil {
		t.Fatalf("read the chain of the covered task: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("the chain of the finished task carries %d link(s), want the two it committed: the admission and the settle", len(chain))
	}
	if last := chain[len(chain)-1].Fact; last.Status != task.StatusSettled || last.ExpectedRevision != 1 {
		t.Fatalf("the last link of the chain states %+v, want the settle applied to the revision the admission left", last)
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
	want := chainStateOf(t, ctx, source, receipt.TaskID)
	if want.revision != cut.Revision+1 {
		t.Fatalf("the settled task stands at revision %d, want the one link past the revision the checkpoint states %d",
			want.revision, cut.Revision)
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
	if got := chainStateOf(t, ctx, booted, receipt.TaskID); !sameSettledState(got, want) {
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
	want := map[event.TaskID]settledState{}
	for _, taskID := range tasks {
		want[taskID] = chainStateOf(t, ctx, source, taskID)
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
		if got := chainStateOf(t, ctx, booted, taskID); !sameSettledState(got, want[taskID]) {
			t.Fatalf("the fallback restored %s = %+v, want the state its chain states %+v", taskID, got, want[taskID])
		}
	}
}

// TestTaskTailFoldLeavesNoSweepStamp pins the movement stamp of a record a tail fold moved.
//   - a task the checkpoint states beside a settling link past the cut stands at the state the tail states, read back from its chain
//   - the record the tail moved carries no movement stamp, so the terminal sweep keeps it, and a record carrying the stamp it settled at is dropped
//   - the sweep is the face that observes the stamp: the account states no finished task to read one from
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
	restored := chainStateOf(t, ctx, booted, receipt.TaskID)
	if restored.status != task.StatusSettled || restored.revision != cut.Revision+1 {
		t.Fatalf("the booted task = %+v, want the settled state of the one link the tail carries past revision %d", restored, cut.Revision)
	}
	if _, err := booted.registry.Register(ctx, taskCheckpointSpec("after-boot", "dedup-after-boot")); err != nil {
		t.Fatalf("admit the task beside the settled record: %v", err)
	}
	if _, status, err := booted.registry.Lookup(ctx, receipt.TaskID); err != nil || status != task.StatusSettled {
		t.Fatalf("the settled record reads as (%v, %v) beside the admission that swept, want it kept", status, err)
	}
	if dropped := booted.registry.PruneTerminal(time.Now()); dropped != 0 {
		t.Fatalf("the sweep dropped %d task(s) by a stamp the tail fold never stated, want the record kept", dropped)
	}
	if dropped := source.registry.PruneTerminal(time.Now().Add(2 * ttl)); dropped != 1 {
		t.Fatalf("the sweep dropped %d task(s) of the source, whose record carries the stamp it settled at, want the one record named: the keep above is a claim about the stamp only beside a stamped record that drops", dropped)
	}
}
