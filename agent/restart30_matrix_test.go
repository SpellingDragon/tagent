package agent

// §8.5 — 30 DETERMINISTIC independent-process restarts over ONE durable root.
// Every round is a fresh child process that recovers (real §5.7 reconcile),
// accepts its pre-scheduled batch (AB = two envelopes merged into one
// business turn; C = a single envelope next turn), walks the real protocol
// and then hard-exits at its pre-scheduled window. No sleeps, no graceful
// Close as a crash stand-in, no cached read-backs: the parent asserts on raw
// disk through brand-new instances between rounds. The final gate drains the
// residue, closes the shared resources, and re-verifies everything through
// yet another independent reopen (shared close ∩ recovery interleaving).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	r30Env    = "TAGENT_R30_ROUND" // round number (child mode/target derived deterministically)
	r30Dir    = "TAGENT_R30_DIR"
	r30Rounds = 30
)

// r30Plan is the deterministic schedule shared by parent and child.
func r30Plan(round int) (mode, target string) {
	if round%2 == 0 {
		mode = "ab"
	} else {
		mode = "c"
	}
	switch round % 4 {
	case 1:
		target = "post-persist"
	case 2:
		target = "post-completion"
	case 3:
		target = "post-receipted"
	default:
		target = "post-ack"
	}
	if round == r30Rounds-1 {
		target = "post-receipted" // leave a receipted-unacked residue for the close∩recovery gate
	}
	return
}

func r30Contents(round int, mode string) []string {
	if mode == "ab" {
		return []string{fmt.Sprintf("r30-%02d-A", round), fmt.Sprintf("r30-%02d-B", round)}
	}
	return []string{fmt.Sprintf("r30-%02d-C", round)}
}

func r30Stack(root string) (*memory.FileSegmentStore, *EventBus, *TagentAgent) {
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 500)
	mustX(err)
	// Mirror the production wiring cold start (wiring.go:420), which — among
	// other things — raises the snowflake floor from the durable chain so a
	// new process generation can never re-issue a committed key (§8.5).
	mustX(store.RebuildLiveCounts())
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	mustX(err)
	bus.SetRetentionGuard(store)
	mustX(bus.ArmRetentionFromInbox())
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "r30", persistentBus: bus, contextManager: cm}
	return store, bus, ta
}

// r30ChildRound runs one scheduled round IN THIS PROCESS (spawned as a child):
// recover → accept → walk the protocol → exit at the scheduled window.
func r30ChildRound(round int, root string) {
	mode, target := r30Plan(round)
	store, bus, ta := r30Stack(root)
	defer store.Close()

	s, err := ta.ReconcileOutstanding()
	mustX(err)
	fmt.Printf("RECON r=%d add=%d clean=%d cont=%d quar=%d block=%d\n", round, s.ReceiptsAdded, s.Cleaned, s.Continued, s.Quarantined, s.Blocked)

	for _, body := range r30Contents(round, mode) {
		if _, err := bus.PublishContext(context.Background(), durableMsg(body)); err != nil {
			childFatal("publish: " + err.Error())
		}
	}
	batch, err := bus.Pull(context.Background())
	mustX(err)
	if len(batch) == 0 {
		childFatal(fmt.Sprintf("round %d: empty batch after publishing %v", round, r30Contents(round, mode)))
	}
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		childFatal("prepare failed")
	}
	for _, e := range batch {
		if !ta.contextManager.persistBusEvent(e) {
			// Real finding of the 30-round cadence: two INDEPENDENT processes
			// may mint the same snowflake key within one millisecond (the
			// in-process seq guard cannot span process generations). The store
			// CONFLICTs it, the claim is HELD, the envelope replays later — the
			// protocol's honest retry path, not data loss. End the round as any
			// submit-gate failure would.
			childExit(fmt.Sprintf("round %d died on a commit conflict (claim held, replays later)", round))
		}
	}
	if target == "post-persist" {
		childExit(fmt.Sprintf("round %d died after input commits, before completion", round))
	}

	// Per-envelope phases exactly as the real finishDurableBatch drives them —
	// backlog replays CAN mix with fresh envelopes in one Pull (§4.2), and each
	// envelope gets its OWN frozen completion + receipt.
	paths, byPath := groupClaimsByPath(batch)
	committed := selectedKeySet(batch)
	completions := map[string]json.RawMessage{}
	creds := map[string]reliability.ReceiptCredential{}
	for _, path := range paths {
		_, raw, err := buildEnvelopeCompletion(byPath[path], committed,
			completedOutcome(), ta.name, ta.contextManager.partitionID, time.Now().UnixMilli(),
			ta.contextManager.buildTurnAttribution(context.Background()))
		if err != nil {
			childFatal("completion build: " + err.Error())
		}
		if err := bus.RecordCompletion(path, raw); err != nil {
			childFatal("record completion: " + err.Error())
		}
		completions[path] = raw
	}
	if target == "post-completion" {
		childExit(fmt.Sprintf("round %d died after durable completions, before receipts", round))
	}
	for _, path := range paths {
		cred, err := ta.contextManager.verifyReceiptCredential(completions[path]) // commits the receipt FACT
		if err != nil {
			childFatal("verify: " + err.Error())
		}
		creds[path] = cred
	}
	if target == "post-receipted" {
		// Write the receipted state through an independent leaf and die BEFORE
		// the ack barrier (ConfirmDurable would have acked).
		in2, err := reliability.NewInbox(filepath.Join(root, "inbox"), 0)
		if err != nil {
			childFatal("leaf: " + err.Error())
		}
		for _, path := range paths {
			if err := in2.RecordReceipt(path, creds[path]); err != nil {
				childFatal("receipted write: " + err.Error())
			}
		}
		childExit(fmt.Sprintf("round %d died receipted, before the ack barrier", round))
	}
	for _, path := range paths {
		if err := bus.ConfirmDurable(path, creds[path]); err != nil {
			childFatal("confirm: " + err.Error())
		}
	}
	childExit(fmt.Sprintf("round %d completed the full protocol (process exit, no Close of shared roots)", round))
}

func TestRestart30_DeterministicIndependentRestarts(t *testing.T) {
	if r := os.Getenv(r30Env); r != "" {
		n := 0
		if _, err := fmt.Sscanf(r, "%d", &n); err != nil || n < 1 || n > r30Rounds {
			childFatal("bad round " + r)
		}
		r30ChildRound(n, os.Getenv(r30Dir))
		return
	}
	root := t.TempDir()
	totalInputs, prevInputs, totalReceipts := 0, 0, 0

	for round := 1; round <= r30Rounds; round++ {
		mode, target := r30Plan(round)
		cmd := exec.Command(os.Args[0], "-test.run", "^TestRestart30_DeterministicIndependentRestarts$")
		cmd.Env = append(os.Environ(), r30Env+"="+fmt.Sprint(round), r30Dir+"="+root)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "round %d (%s/%s) child:\n%s", round, mode, target, out)
		require.Contains(t, string(out), fmt.Sprintf("RECON r=%d", round), "every round must start with a real reconcile pass")

		// ---- bounded raw-disk audit through BRAND-NEW instances ------------
		store, _, _ := r30Stack(root)
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 1000})
		require.NoError(t, err)
		seen := map[int64]bool{}
		var inputs, receipts int
		for _, r := range refs {
			require.False(t, seen[r.EventKey], "event key %d listed twice — no duplicate identities", r.EventKey)
			seen[r.EventKey] = true
			if r.EventType == tagentevent.TypeExternalInput {
				inputs++
			}
			if r.EventType == tagentevent.TypeInboxReceipt {
				receipts++
			}
		}
		totalInputs += len(r30Contents(round, mode))
		// Real monotone identity vs the cumulative scheduled count — NOT a vacuous
		// ">= 1": committed facts never vanish across a restart (non-decreasing) and
		// can never exceed what was scheduled (a conflict round holds its claim and
		// replays later, so on-disk may trail the schedule but never outrun it).
		require.LessOrEqual(t, inputs, totalInputs, "round %d: committed inputs must never exceed the cumulative scheduled count (%d)", round, totalInputs)
		require.GreaterOrEqual(t, inputs, prevInputs, "round %d: committed-input count is monotone across restarts (facts once laid are never lost)", round)
		prevInputs = inputs
		require.LessOrEqual(t, receipts, inputs, "receipts can never outrun committed inputs")
		// Content provenance, not mere non-emptiness: any outstanding envelope must
		// carry a scheduled round marker in its raw bytes. A round that dies BEFORE
		// its ack barrier keeps its OWN envelope(s) on disk, so that round's exact
		// marker must appear. A post-ack round has drained its own envelopes — the
		// identity check is vacuous there, so the ledger makes NO identity claim for
		// it (fail-before: the old code asserted only NotEmpty(Raw) every round).
		envs := finishEnvelopes(t, root)
		for _, e := range envs {
			require.Regexp(t, `r30-\d\d-`, e.Raw, "an outstanding envelope must carry a scheduled round marker (provenance)")
		}
		if target != "post-ack" {
			marker := fmt.Sprintf("r30-%02d-", round)
			found := false
			for _, e := range envs {
				if strings.Contains(e.Raw, marker) {
					found = true
				}
			}
			require.True(t, found, "round %d (%s) left its own envelope(s) carrying marker %q on disk", round, target, marker)
		} else if len(envs) == 0 {
			t.Logf("round %d post-ack: inbox drained — no envelope identity claim made this round", round)
		}
		require.NoError(t, store.Close())
	}

	// ================= final gate: drain, shared close, independent reopen ==
	store, bus, ta := r30Stack(root)
	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Zero(t, s.Quarantined+s.Blocked, "30 scheduled rounds must leave NO unexplainable material: %+v", s)
	// Continued residue (post-persist rounds) replays to convergence — the
	// unacked-envelope ledger bounds the loop (Pull would otherwise wait for
	// producers that never come).
	for bus.DurablePending() > 0 {
		batch, err := bus.Pull(context.Background())
		require.NoError(t, err)
		if len(batch) == 0 {
			break
		}
		require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
		for _, e := range batch {
			require.True(t, ta.contextManager.persistBusEvent(e))
		}
		ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	}

	// Per-envelope receipts: 15 AB rounds ×2 + 15 C rounds ×1 = 45 acceptances.
	wantAccepted := 0
	for round := 1; round <= r30Rounds; round++ {
		mode, _ := r30Plan(round)
		wantAccepted += len(r30Contents(round, mode))
	}
	require.Equal(t, 45, wantAccepted)
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 1000})
	require.NoError(t, err)
	var inputs, receipts int
	keys := map[int64]bool{}
	for _, r := range refs {
		require.False(t, keys[r.EventKey], "final chain has a duplicate key %d", r.EventKey)
		keys[r.EventKey] = true
		switch r.EventType {
		case tagentevent.TypeExternalInput:
			inputs++
		case tagentevent.TypeInboxReceipt:
			receipts++
		}
	}
	require.Equal(t, wantAccepted, inputs, "every accepted input landed EXACTLY once across 30 restarts (totalInputs=%d)", totalInputs)
	require.Equal(t, wantAccepted, receipts, "one durable receipt per accepted envelope — none lost, none doubled (totalReceipts=%d)", totalReceipts)
	for _, body := range append(r30Contents(1, "c"), r30Contents(30, "ab")...) {
		found := false
		for _, r := range refs {
			full, gerr := store.GetEvent(r.EventKey)
			require.NoError(t, gerr)
			if full.EventType == tagentevent.TypeExternalInput && full.Content == body {
				found = true
			}
		}
		require.True(t, found, "content %q must be recallable from the fact chain", body)
	}

	// Shared close ∩ recovery interleaving: the round-29 receipted residue was
	// converged by the reconcile above; closing the shared durable roots now
	// must leave NOTHING recoverable behind…
	require.Equal(t, int64(0), bus.DurablePending())
	require.Empty(t, finishEnvelopes(t, root), "inbox fully drained before the close interleave")
	require.NoError(t, bus.CloseDurable())
	require.NoError(t, store.Close())

	// …and an INDEPENDENT reopen verifies the post-close state is stable and
	// idempotent (no re-execution, no extra receipts, still clean).
	store2, bus2, ta2 := r30Stack(root)
	s2, err := ta2.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, ReconcileSummary{}, s2, "nothing left to recover after the close interleave")
	require.Equal(t, int64(0), bus2.DurablePending())
	refs2, err := store2.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 1000})
	require.NoError(t, err)
	require.Len(t, refs2, len(refs), "the post-close chain is byte-stable (no phantom writes)")
	tmps, err := filepath.Glob(filepath.Join(root, "inbox", "inbox-v2", "*.tmp"))
	require.NoError(t, err)
	require.Empty(t, tmps, "no unconfirmed material residue after 30 deterministic rounds")
	require.NoError(t, bus2.CloseDurable())
	require.NoError(t, store2.Close())
}
