// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
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

// taskRecoveryStorageRoot is the durable root the host document of the vertical
// check declares, so a process that stopped and one that boots read one account.
func taskRecoveryStorageRoot(root string) string { return filepath.Join(root, "storage") }

// taskRecoveryDocument writes one host document over the scripted endpoint and
// answers its path.
func taskRecoveryDocument(t *testing.T, root, endpoint string) string {
	t.Helper()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(generationDocument(root, endpoint, "")), 0o644); err != nil {
		t.Fatalf("write the configuration source: %v", err)
	}
	return configPath
}

// countingGenerationProvider answers one scripted completion per request and
// counts the requests it served, so a check reads whether a boot drove a model.
func countingGenerationProvider(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	served := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		served.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"generation","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	return server, served
}

// taskRecoverySpec builds the declaration one task of the vertical check is
// admitted under, routed to the address its notifications reach.
func taskRecoverySpec(call string, addr event.Address, instance event.InstanceID) task.Spec {
	return task.Spec{
		CallID:       call,
		DedupKey:     task.DedupKey("dedup-" + call),
		InvocationID: event.InvocationID("invocation-task-recovery"),
		InstanceID:   instance,
		Description:  "one admitted call of the vertical check",
		Source:       addr,
		Target:       addr,
		Execution:    task.Serialization("session-task-recovery"),
	}
}

// taskRecoveryEnvelope builds the notification one settled task leaves owed,
// carrying the routing it froze at that transition.
func taskRecoveryEnvelope(notice task.Notice, addr event.Address, instance event.InstanceID) task.Envelope {
	return task.Envelope{Notice: notice, Target: addr, Source: addr, TargetInstance: instance}
}

// settleTaskBesideNotice admits one task and settles it beside the notification
// its outbox leaves owed, and answers the identity the registry minted.
func settleTaskBesideNotice(t *testing.T, ctx context.Context, registry *task.MemoryRegistry, call string, envelope task.Envelope) event.TaskID {
	t.Helper()
	receipt, err := registry.Register(ctx, taskRecoverySpec(call, envelope.Target, envelope.TargetInstance))
	if err != nil || receipt.Kind != task.AdmissionFresh {
		t.Fatalf("admit the task of %s = (%+v, %v), want a fresh admission", call, receipt, err)
	}
	envelope.Notice.TaskID = receipt.TaskID
	if result, err := registry.Advance(ctx, receipt.TaskID, task.StatusSettled, task.MutationID("settle-"+call), []task.Envelope{envelope}); err != nil || result != task.CommitCommitted {
		t.Fatalf("settle the task of %s = (%v, %v), want a committed change", call, result, err)
	}
	return receipt.TaskID
}

// closeTaskRecoveryHost ends one host of the vertical check and fails the test
// when the close reports an error it cannot judge.
func closeTaskRecoveryHost(t *testing.T, host *Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host of the vertical check: %v", err)
	}
}

// TestTaskRecoveryNormalizesCheckpointAndTail drives the task-to-mailbox vertical through one real boot on each side of a stop.
//   - fixture: a booted host settles one task whose outbox leaves a notification owed and files the cut that states it, then stops
//   - fixture: past that cut the account lands the receipt of the stated notification beside a task admitted with the notification it still owes
//   - positive: the account is seated from the cut and the tail, so the tasks the two sources state read back at the state they committed
//   - positive: the task that finished behind the cut states no entry and reads back from its chain alone
//   - positive: the notification the receipt answers opens no delivery, and the one the tail states is seated with the routing its task froze
//   - negative: a boot that restored the slices before it seated the account loses the notification the tail states
//   - negative: a boot that read the cut's mailbox payload as the whole owed set drives a notification the receipt already answered
//   - the recovery drives nothing of its own: the boot serves no model request and lands no second receipt
//   - consumer: the public Boot entry, the mailbox restore and the tail fold behind it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTaskRecoveryNormalizesCheckpointAndTail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storageRoot := taskRecoveryStorageRoot(root)
	coveredAddr := event.Address{AgentID: "user", SessionID: "session-task-recovery-covered"}
	coveredInstance := event.InstanceID("instance-task-recovery-covered")
	tailAddr := event.Address{AgentID: "user", SessionID: "session-task-recovery-tail"}
	tailInstance := event.InstanceID("instance-task-recovery-tail")
	provider, served := countingGenerationProvider(t)
	configPath := taskRecoveryDocument(t, root, provider.URL+"/v1")
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	source, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the host that files the cut: %v", err)
	}
	stopped := source.Run()
	stopped.stopCheckpointFiler()
	commitWindowFact(t, ctx, stopped, coveredAddr,
		windowEvent(coveredAddr, "tr-plan", "inv-task-recovery", "the fact the cut covers", event.TypeThinkingPlan))
	finished := settleTasks(t, ctx, stopped, []string{"finished"})
	coveredNotice := task.Notice{
		Round: 1, Kind: task.NoticeSettled, Seq: 1,
		Event: event.EventID("fact-task-recovery-covered"),
		Text:  "the result the stopped process never consumed",
	}
	answeredTask := settleTaskBesideNotice(t, ctx, stopped.registry, "answered",
		taskRecoveryEnvelope(coveredNotice, coveredAddr, coveredInstance))
	coveredNotice.TaskID = answeredTask
	if owed := stopped.durableAccount().OwedNotices(); len(owed) != 1 || owed[0].Notice.Notice.TaskID != answeredTask {
		t.Fatalf("the account of the stopped process owes %+v, want the notification the settled task left", owed)
	}
	cutKey, filed, err := stopped.Checkpoint(ctx)
	if err != nil || !filed || cutKey == "" {
		t.Fatalf("file the cut of the stopped process = (%q, %t, %v), want the checkpoint that states the owed notification", cutKey, filed, err)
	}
	if payload := slicePayloadOf(t, checkpointOfIdentity(t, ctx, stopped, cutKey), state.StateSliceMailbox); !strings.Contains(payload, string(answeredTask)) {
		t.Fatalf("the cut states %q for the mailbox, want the notification %s the tail receipt is read past", payload, answeredTask)
	}
	closeTaskRecoveryHost(t, source)

	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: storageRoot, WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier beside the cut: %v", err)
	}
	account := state.NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
	if err := account.Seat(ctx, nil, nil); err != nil {
		t.Fatalf("seat the account of the storage: %v", err)
	}
	mailbox := state.NewMemoryStore(state.NewAuthority(), account)
	if _, err := mailbox.Establish(ctx, coveredAddr, coveredInstance); err != nil {
		t.Fatalf("establish the site the stated notification is delivered to: %v", err)
	}
	if err := mailbox.TrackDelivery(ctx, answeredTask, task.HandoffTarget{Target: coveredAddr, TargetInstance: coveredInstance}); err != nil {
		t.Fatalf("register the delivery route of %s: %v", answeredTask, err)
	}
	if err := mailbox.AcceptNotice(ctx, coveredNotice); err != nil {
		t.Fatalf("accept the notification %s past the cut: %v", coveredNotice.FactKey(), err)
	}
	covered, held := seatOfTask(mailbox.Unconsumed(), answeredTask)
	if !held {
		t.Fatalf("the mailbox past the cut owes %+v, want the notification it is about to consume", mailbox.Unconsumed())
	}
	if err := mailbox.Claim(ctx, covered.Address, covered.Instance, covered.AcceptSeq); err != nil {
		t.Fatalf("claim the notification %s past the cut: %v", coveredNotice.FactKey(), err)
	}
	if err := mailbox.Consume(ctx, covered.Address, covered.Instance, covered.AcceptSeq); err != nil {
		t.Fatalf("consume the notification %s past the cut: %v", coveredNotice.FactKey(), err)
	}

	owedNotice := task.Notice{
		Round: 1, Kind: task.NoticeProgress, Seq: 1,
		Event: event.EventID("fact-task-recovery-tail"),
		Text:  "the progress the tail states",
	}
	owedTask := event.TaskID("task-recovery-tail")
	owedNotice.TaskID = owedTask
	if err := account.Declare(ctx, owedTask, taskRecoverySpec("tail", tailAddr, tailInstance)); err != nil {
		t.Fatalf("declare the task the tail states: %v", err)
	}
	if result, err := account.Commit(ctx, task.CommitRecord{
		MutationID: "admit-tail",
		TaskID:     owedTask,
		Status:     task.StatusRunning,
		Outbox:     []task.Envelope{taskRecoveryEnvelope(owedNotice, tailAddr, tailInstance)},
	}); err != nil || result != task.CommitCommitted {
		t.Fatalf("admit the task the tail states = (%v, %v), want a committed record", result, err)
	}
	if err := tier.Close(); err != nil {
		t.Fatalf("release the storage tier beside the tail: %v", err)
	}

	observer := &foldObserver{}
	previous := trpclog.Default
	trpclog.Default = observer
	t.Cleanup(func() { trpclog.Default = previous })
	booted, err := Boot(configPath)
	if err != nil {
		t.Fatalf("boot the process that restores the cut beside its tail: %v", err)
	}
	t.Cleanup(func() { closeTaskRecoveryHost(t, booted) })
	restored := booted.Run()
	if got := served.Load(); got != 0 {
		t.Fatalf("the boot served %d model request(s) while it recovered, want none: a recovery drives no work of its own", got)
	}
	if got, _ := restored.CheckpointFiled(); got != cutKey && got == "" {
		t.Fatalf("the boot read no checkpoint, want the cut %q the stopped process filed", cutKey)
	}
	if _, status, err := restored.registry.Lookup(ctx, finished[0]); err != nil || status != task.StatusSettled {
		t.Fatalf("the finished task %s reads as (%v, %v), want the settled state it committed", finished[0], status, err)
	}
	if _, status, err := restored.registry.Lookup(ctx, answeredTask); err != nil || status != task.StatusSettled {
		t.Fatalf("the answered task %s reads as (%v, %v), want the settled state the cut states", answeredTask, status, err)
	}
	if _, status, err := restored.registry.Lookup(ctx, owedTask); err != nil || status != task.StatusRunning {
		t.Fatalf("the owed task %s reads as (%v, %v), want the running state the tail states", owedTask, status, err)
	}
	report := recoveredTasksByID(restored.Recovery())
	if _, named := report[finished[0]]; named {
		t.Fatalf("the recovery report names the finished task %s, want the work the account holds no entry for", finished[0])
	}
	if _, named := report[owedTask]; !named {
		t.Fatalf("the recovery report names none of the task %s the tail states, want the chain the tail fold rebuilt", owedTask)
	}
	seats := restored.store.Unconsumed()
	if _, carried := seatOfTask(seats, answeredTask); carried {
		t.Fatalf("the booted mailbox owes %+v, want the notification %s left out: the receipt past the cut answers it", seats, answeredTask)
	}
	owedSeat, carried := seatOfTask(seats, owedTask)
	if !carried {
		t.Fatalf("the booted mailbox owes %+v, want the notification %s the account states past the cut", seats, owedTask)
	}
	if owedSeat.Address != tailAddr || owedSeat.Instance != tailInstance {
		t.Fatalf("the restored seat routes to %v at %s, want the routing the admitted task froze %v at %s",
			owedSeat.Address, owedSeat.Instance, tailAddr, tailInstance)
	}
	if owedSeat.Claim != state.ClaimPending {
		t.Fatalf("the restored seat stands at claim %v, want the pending delivery the recovery leaves to its owner", owedSeat.Claim)
	}
	if receipts := consumeReceiptKeys(t, ctx, restored.tier); len(receipts) != 1 {
		t.Fatalf("the root holds %d consumption receipt(s), want the one the stopped process left past the cut: a recovery consumes nothing", len(receipts))
	}
	if !observedLine(observer, "info", "boot seated the task account", "tasks=2") ||
		observedLine(observer, "info", "boot seated the task account", "tail=none") {
		t.Fatalf("the boot seated %v, want the two tasks the cut and its tail state past a bounded tail", observer.lines)
	}
	if !observedLine(observer, "info", "restore folded", "chains=1", "covered=1") {
		t.Fatalf("the boot stated no fold of the one chain past the cut beside the one it covers, want the tailed task folded and the stated one repaired: %v", observer.lines)
	}
}

// TestMissingTaskSliceReadsTheWholeLogInsteadOfEmpty pins the seat of a cut that states no task account.
//   - fixture: a process settles two tasks and files the framework cut, then files a session cut that states no tasks slice and stops
//   - positive: the boot seats the account from the whole log, so the tasks the newest cut states nowhere read back settled
//   - negative: a boot that read the missing tasks slice as an empty account answers no task and folds no chain
//   - consumer: the assemble restore, tasksStateSlice.SeatRestore and the registry lookup behind it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestMissingTaskSliceReadsTheWholeLogInsteadOfEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	addr := deliveryTestAddress("session-task-missing-slice")
	source := snapshotTestRun(t, ctx, storage())
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "tm", 1)
	settled := settleTasks(t, ctx, source, []string{"missing-slice-a", "missing-slice-b"})
	want := map[event.TaskID]settledState{}
	for _, taskID := range settled {
		want[taskID] = chainStateOf(t, ctx, source, taskID)
	}
	if _, filed, err := source.Checkpoint(ctx); err != nil || !filed {
		t.Fatalf("file the framework cut = (%t, %v), want a filed fact", filed, err)
	}
	commitSessionTurns(t, ctx, source, addr, "tm-past", 1)
	sessionKey, filed, err := source.CheckpointSession(ctx, addr)
	if err != nil || !filed {
		t.Fatalf("file the session cut = (%t, %v), want the fact that supersedes the framework cut", filed, err)
	}
	names := checkpointOfIdentity(t, ctx, source, sessionKey).CheckpointSliceNames()
	if slices.Contains(names, checkpointSliceTasks) {
		t.Fatalf("the newest cut states %v, want a cut that states no tasks slice: a cut stating it proves no missing slice", names)
	}
	endRun(t, source)

	booted := snapshotTestRun(t, ctx, storage())
	defer endRun(t, booted)
	account := booted.durableAccount()
	if account == nil || !account.Seated() {
		t.Fatalf("the boot seated the account %v, want the account the whole log states", account)
	}
	held := account.TaskIDs()
	if len(held) != len(settled) {
		t.Fatalf("the boot seated %d task(s) %v, want the %d past a cut that states none: a missing tasks slice is the whole log, never an empty account",
			len(held), held, len(settled))
	}
	for _, taskID := range settled {
		if !slices.Contains(held, taskID) {
			t.Fatalf("the seated account holds %v, want the task %s the cut states nowhere", held, taskID)
		}
		if got := chainStateOf(t, ctx, booted, taskID); !sameSettledState(got, want[taskID]) {
			t.Fatalf("the booted task %s = %+v, want the state its chain states %+v", taskID, got, want[taskID])
		}
	}
}

// TestCorruptTaskSliceRefusesBoot pins the refusal of an account the newest cut states unreadably.
//   - fixture: a process settles one task and files the framework cut; a second files a readable cut at that offset whose tasks slice states no account, and stops
//   - positive: the boot refuses by name, and the refusal names the task account it could not hold
//   - negative: a boot that skipped the unreadable slice starts beside an account the cut does not state
//   - consumer: the assemble restore, tasksStateSlice.SeatRestore and DurableCommit.Seat behind it
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestCorruptTaskSliceRefusesBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	addr := deliveryTestAddress("session-task-corrupt-slice")
	source := snapshotTestRun(t, ctx, storage())
	source.stopCheckpointFiler()
	commitSessionTurns(t, ctx, source, addr, "tc", 1)
	settleTasks(t, ctx, source, []string{"corrupt-slice"})
	cutKey, filed, err := source.Checkpoint(ctx)
	if err != nil || !filed {
		t.Fatalf("file the framework cut = (%t, %v), want a filed fact", filed, err)
	}
	cut := checkpointOfIdentity(t, ctx, source, cutKey)
	if len(cut.Slices) == 0 {
		t.Fatal("the framework cut states no slice, so the corruption below proves nothing")
	}
	endRun(t, source)

	corrupt := snapshotTestRun(t, ctx, storage())
	corrupt.stopCheckpointFiler()
	corrupt.checkpointEvery = 1 << 30
	corrupt.checkpointPeriod = 1000 * time.Hour
	bad, err := event.CheckpointFor(cut.Offset, []event.CheckpointSlice{{
		Name:    checkpointSliceTasks,
		Payload: json.RawMessage(`{"tasks":"not-an-account"}`),
	}})
	if err != nil {
		t.Fatalf("state the cut of the fixture: %v", err)
	}
	bad.Key = string(event.EventID("checkpoint:9999"))
	evt := checkpointEventOf(bad, event.Address{AgentID: addr.AgentID})
	if _, stated := event.CheckpointOf(evt); !stated {
		t.Fatal("the fixture states a cut no reader can read, want the corrupt account carried inside a readable one")
	}
	if _, err := corrupt.commitFactChecked(ctx, evt); err != nil {
		t.Fatalf("file the cut whose tasks slice states no account: %v", err)
	}
	endRun(t, corrupt)

	booted, err := bootWindowChain(ctx, storage())
	if err == nil {
		endRun(t, booted)
		t.Fatal("the boot beside a tasks slice that states no account answered success, want the cut refused by name")
	}
	if !errors.Is(err, state.ErrSeatRefused) {
		t.Fatalf("the boot beside the corrupt tasks slice = %v, want the refusal %v", err, state.ErrSeatRefused)
	}
	t.Logf("the boot refused the corrupt tasks slice: %v", err)
	for _, named := range []string{"task account", "checkpoint:9999"} {
		if !strings.Contains(err.Error(), named) {
			t.Fatalf("the refusal %q names no %q", err, named)
		}
	}
}
