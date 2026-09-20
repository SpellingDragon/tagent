package agent

// §8.4 — FINISH-SIDE crash windows, real subprocesses: die after the model
// returned (before any completion), after the completion froze durable
// (before any receipt), and after the receipted rewrite (before the Ack
// barrier). Each parent reopens EVERY surface fresh (inbox, localfile store,
// retention guard) and drives the real §5.7 ReconcileOutstanding. Pinned
// semantics: a durable completion is never re-executed (the recovery process
// has no model at all); a cancelled turn writes NO terminal state; without a
// durable completion, a re-run is the honest boundary and converges to
// exactly one receipt per envelope.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
)

const (
	finishCrashEnv = "TAGENT_FINISH_CRASH_AT" // "pre-finish" | "4:renamed" | "5:renamed"
	finishRootEnv  = "TAGENT_FINISH_DIR"
)

func childFinishCrash(root, spec string) {
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	mustX(err)
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	mustX(err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus.SetRetentionGuard(store)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-finish", persistentBus: bus, contextManager: cm}

	if _, err := bus.PublishContext(context.Background(), durableMsg("finish-slot-X")); err != nil {
		childFatal("publish: " + err.Error())
	}
	batch, err := bus.Pull(context.Background())
	mustX(err)
	if len(batch) != 1 {
		childFatal(fmt.Sprintf("expected 1-event batch, got %d", len(batch)))
	}
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		childFatal("prepare failed")
	}
	if !cm.persistBusEvent(batch[0]) {
		childFatal("persist failed")
	}
	if spec == "pre-finish" {
		childExit("child died after model returned, before any completion")
	}

	// Walk the REAL finish phases step by step, dying between them (the same
	// partial-progress disk states a crash can leave, without any seam):
	paths, byPath := groupClaimsByPath(batch)
	path := paths[0]
	completion, raw, err := buildEnvelopeCompletion(byPath[path], selectedKeySet(batch),
		completedOutcome(), ta.name, cm.partitionID, time.Now().UnixMilli(), cm.buildTurnAttribution(context.Background()))
	if err != nil {
		childFatal("completion build: " + err.Error())
	}
	if err := bus.RecordCompletion(path, raw); err != nil {
		childFatal("record completion: " + err.Error())
	}
	if spec == "post-completion" {
		childExit("child died after the completion froze durable, before any receipt")
	}
	cred, err := cm.verifyReceiptCredential(raw) // commits the receipt FACT (Phase B)
	if err != nil {
		childFatal("verify credential: " + err.Error())
	}
	// Phase C split across a second leaf instance over the SAME dir: write the
	// receipted state, die BEFORE the ack barrier.
	in2, err := reliability.NewInbox(filepath.Join(root, "inbox"), 0)
	if err != nil {
		childFatal("second leaf: " + err.Error())
	}
	_ = completion
	if err := in2.RecordReceipt(path, cred); err != nil {
		childFatal("record receipt: " + err.Error())
	}
	childExit("child died after the receipted rewrite, before the ack barrier")
}

// reopenFinishParent gives the parent the FULL fresh stack (independent
// read-back: nothing shared with the child process).
func reopenFinishParent(t *testing.T, root string) (*memory.FileSegmentStore, *EventBus, *TagentAgent) {
	t.Helper()
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	require.NoError(t, err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox())
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-finish", persistentBus: bus, contextManager: cm}
	return store, bus, ta
}

func finishEnvelopes(t *testing.T, root string) []OutstandingEnvLike {
	t.Helper()
	// Independent leaf read-back of the raw envelope JSON facts on disk.
	dir := filepath.Join(root, "inbox", "inbox-v2")
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	require.NoError(t, err)
	var out []OutstandingEnvLike
	for _, e := range entries {
		raw, err := os.ReadFile(e)
		require.NoError(t, err)
		out = append(out, OutstandingEnvLike{Name: filepath.Base(e), Raw: string(raw)})
	}
	return out
}

// OutstandingEnvLike is a raw-disk envelope snapshot for cross-checks (the
// parent must verify what the CHILD left, before any recovery rewrite).
type OutstandingEnvLike struct{ Name, Raw string }

func TestCrashMatrix_FinishSideWindows(t *testing.T) {
	if spec := os.Getenv(finishCrashEnv); spec != "" {
		childFinishCrash(os.Getenv(finishRootEnv), spec)
		return
	}
	t.Run("model_returned_before_completion", func(t *testing.T) {
		root := t.TempDir()
		runFinishChild(t, root, "pre-finish")

		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		// No terminal state exists: the requeued envelope is pending with NO
		// completion — a dead turn never fakes one.
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Empty(t, countType(refs, tagentevent.TypeInboxReceipt), "no receipt before completion")
		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1)
		require.NotContains(t, envs[0].Raw, `"completion"`, "the crashed turn wrote NO completion")

		// Without a durable completion the honest boundary RE-RUNS: replay the
		// whole turn on this process (frozen keys → idempotent commits).
		batch, err := bus.Pull(context.Background())
		require.NoError(t, err)
		require.Len(t, batch, 1, "the pending item is claimable again — never silently lost")
		require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
		for _, e := range batch {
			require.True(t, ta.contextManager.persistBusEvent(e))
		}
		ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
		require.Equal(t, int64(0), bus.DurablePending())
		require.Empty(t, finishEnvelopes(t, root), "acked and unlinked — processed-cleaned")
	})

	t.Run("completion_durable_no_reexecution", func(t *testing.T) {
		root := t.TempDir()
		runFinishChild(t, root, "post-completion")

		// BEFORE any recovery: the completion itself survived on raw disk.
		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1)
		require.Contains(t, envs[0].Raw, `"completion"`, "the frozen completion outlived the crash")

		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		// §5.7 recovery: a durable completion re-submits ONLY its receipt.
		// This process has NO model wired — "no re-execution" is STRUCTURAL.
		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.ReceiptsAdded, "exactly one re-submitted receipt")
		require.Equal(t, int64(0), bus.DurablePending())

		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 1, countType(refs, tagentevent.TypeExternalInput), "input fact NOT double-written")
		require.Equal(t, 1, countType(refs, tagentevent.TypeInboxReceipt), "exactly one receipt on the chain")
		require.Empty(t, finishEnvelopes(t, root), "envelope cleaned")
	})

	t.Run("receipted_before_ack_barrier", func(t *testing.T) {
		root := t.TempDir()
		runFinishChild(t, root, "post-receipted")

		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1)
		require.Contains(t, envs[0].Raw, `"state":"receipted"`, "the receipted rewrite landed")

		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 0, s.ReceiptsAdded, "receipt already durable — recovery adds nothing")
		require.Equal(t, int64(0), bus.DurablePending(), "the owed ack finished — barrier + exactly-once release")
		require.Empty(t, finishEnvelopes(t, root))
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 1, countType(refs, tagentevent.TypeInboxReceipt), "still exactly one receipt — no duplicate")
	})

	t.Run("cancel_never_fakes_terminal", func(t *testing.T) {
		// Non-crash sibling in the same matrix: a CANCELLED outcome passed to
		// the real finish path must write NOTHING terminal — claim kept, no
		// completion, no receipt, replay still owns the original.
		root := t.TempDir()
		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		_, err := bus.PublishContext(context.Background(), durableMsg("cancel-slot-Y"))
		require.NoError(t, err)
		batch, err := bus.Pull(context.Background())
		require.NoError(t, err)
		require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
		for _, e := range batch {
			require.True(t, ta.contextManager.persistBusEvent(e))
		}
		ta.finishDurableBatch(context.Background(), batch, batch, turnOutcome{status: turnCancelled})

		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1, "the cancelled turn keeps its claim (never cleaned silently)")
		require.NotContains(t, envs[0].Raw, `"completion"`, "a cancelled turn NEVER freezes a completion")
		require.NotContains(t, envs[0].Raw, `"state":"receipted"`, "nor a receipt state")
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 0, countType(refs, tagentevent.TypeInboxReceipt), "no receipt fact for a cancelled turn")
		require.Positive(t, bus.DurablePending(), "the item stays outstanding for honest replay")
	})
}
func childFatal(msg string) {
	fmt.Fprintln(os.Stderr, "CHILD FATAL:", msg)
	os.Exit(1)
}

func childExit(msg string) {
	fmt.Println(msg)
	os.Stdout.Sync()
	os.Exit(0) // the crash: no Close, no cleanup, no deferred barriers
}

func runFinishChild(t *testing.T, root, spec string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashMatrix_FinishSideWindows$")
	cmd.Env = append(os.Environ(), finishCrashEnv+"="+spec, finishRootEnv+"="+root)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child must hit crash point %s:\n%s", spec, out)
	require.Contains(t, string(out), "child died")
}

func countType(refs []memory.EventReference, typ string) int {
	n := 0
	for _, r := range refs {
		if r.EventType == typ {
			n++
		}
	}
	return n
}
