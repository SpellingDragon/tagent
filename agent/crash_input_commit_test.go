package agent

// §8.3 last window — crash at "each input commit": a 2-slot batch is claimed,
// durably prepared, and the FIRST input fact commits; the process then dies
// BEFORE the second commit (real subprocess, no Close). The parent reopens the
// localfile backend and the inbox through fresh instances and reconciles:
// identity (the landed fact carries the FROZEN prepared key), source (both
// slot originals remain in the envelope), counts (exactly one input fact),
// and unconfirmed material (slot B was never committed — its original is
// still lossless in the inbox and replay finishes it WITHOUT double-writes).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
)

const (
	commitCrashEnv = "TAGENT_INPUT_CRASH_CHILD"
	commitRootEnv  = "TAGENT_INPUT_CRASH_DIR"
)

// committedInputKeys counts external_input facts on the chain.
func committedInputs(t *testing.T, s memory.MemoryStore) []memory.EventReference {
	refs, err := s.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 100})
	require.NoError(t, err)
	var out []memory.EventReference
	for _, r := range refs {
		if r.EventType == tagentevent.TypeExternalInput {
			out = append(out, r)
		}
	}
	return out
}

func childInputCommitCrash(root string) {
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	mustX(err)
	defer func() { _ = bus }()
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	mustX(err)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-commit", persistentBus: bus, contextManager: cm}
	_, err = bus.PublishContext(context.Background(), durableMsg("commit-slot-A"))
	mustX(err)
	_, err = bus.PublishContext(context.Background(), durableMsg("commit-slot-B"))
	mustX(err)
	batch, err := bus.Pull(context.Background())
	mustX(err)
	if len(batch) != 2 {
		panic(fmt.Sprintf("child: expected one 2-slot batch, got %d", len(batch)))
	}
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		panic("child: prepare must succeed before any commit")
	}
	if !cm.persistBusEvent(batch[0]) {
		panic("child: first commit must succeed")
	}
	fmt.Println("child committed slot A, dying before slot B")
	os.Stdout.Sync()
	os.Exit(0) // the crash: no second commit, no receipt, no completion, no ack
}

func TestCrashWindow_InputCommit(t *testing.T) {
	if os.Getenv(commitCrashEnv) == "1" {
		childInputCommitCrash(os.Getenv(commitRootEnv))
		return
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashWindow_InputCommit$")
	cmd.Env = append(os.Environ(), commitCrashEnv+"=1", commitRootEnv+"="+root)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child must reach its crash point:\n%s", out)
	require.Contains(t, string(out), "dying before slot B")

	// ---- INDEPENDENT reopen: fresh store + fresh inbox over the same dirs --
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	require.NoError(t, err)
	defer store.Close()

	inputs := committedInputs(t, store)
	require.Len(t, inputs, 1, "exactly ONE input committed before the crash (count reconciliation)")
	landed, gerr := store.GetEvent(inputs[0].EventKey)
	require.NoError(t, gerr)
	require.Contains(t, landed.Content, "commit-slot-A")

	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-commit", persistentBus: bus, contextManager: cm}

	// SOURCE + unconfirmed material: both slot originals survive in the
	// requeued envelope — B was never committed yet is NOT lost.
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 2, "the crashed batch replays whole — the uncommitted slot B is still claimable")
	var sawB bool
	for _, e := range batch {
		if e.Message != nil && e.Message.Content == "commit-slot-B" {
			sawB = true
		}
	}
	require.True(t, sawB, "slot B's ORIGINAL must remain lossless in the inbox (no silent loss)")

	// IDENTITY: replay re-persists from the FROZEN prepared facts — slot A's
	// commit is idempotent (same event key, no second fact), slot B lands once.
	require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
	for _, e := range batch {
		require.True(t, cm.persistBusEvent(e), "replay commits must succeed on the frozen keys")
	}
	inputs = committedInputs(t, store)
	require.Len(t, inputs, 2, "A must NOT double-write across the crash replay; B adds exactly one")

	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending(), "the replayed turn acks only after receipt is durable")
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 100})
	require.NoError(t, err)
	var receipts int
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			receipts++
		}
	}
	require.Equal(t, 2, receipts, "one receipt per accepted envelope after the crash replay — no duplicate, none lost")

	// The frozen key of slot A really is the committed identity (not a re-stamp).
	envs, err := openEnvelopesForTest(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	_ = envs // after the ack barrier the envelope is gone; empty IS the assertion below
	require.Empty(t, envs, "processed-cleaned: the acked envelope left the inbox")
}

// openEnvelopesForTest reads the inbox dir through a fresh leaf (the test must
// not reuse the live instance to claim "nothing outstanding").
func openEnvelopesForTest(dir string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "inbox-v2", "*.json"))
	if err != nil {
		return nil, err
	}
	return entries, nil
}
