package tagent

// §8.7 — the elimination list closes HERE with two jointly-sufficient proofs
// (design 決策10 L189): (a) a STATIC dependency check that deleted legacy
// mechanisms stay deleted (no "uncalled but present" residue), and (b) the
// THREE BOOT STATES — brand-new dir, restart-over-current-state, boot after
// the managed reset — each running one real turn on the CURRENT path, with
// legacy markers planted beside the data proving they are read-NOT,
// consumed-not, and wiped-not.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestEliminationList_ZeroLegacySymbols — static check over every NON-TEST
// Go file: each pattern was a live mechanism before this change and its
// removal was ordered by 決策10's list (old parsing/aliases/confirm APIs,
// compatibility-only wrappers, weak fallbacks, duplicate owners, WAL/fsync
// claims of the minimized localfile backend).
func TestEliminationList_ZeroLegacySymbols(t *testing.T) {
	banned := []struct{ pattern, why string }{
		{"task_stale_after", "10.5: stale observation wall deleted; TTL is the only age path"},
		{"task_job_deadline", "10.5: second age wall merged into task_default_ttl"},
		{"TaskMaxDetachedAge", "10.5: compat remapping removed, no aliases"},
		{"type SpillStore", "決策10: legacy spill overflow format removed (files are inert transitional data)"},
		{"func NewSpillStore", "決策10: same"},
		{"loopTerminated ", "6.1: replaced by the loopState machine (loopTerminatedNow helper)"},
		{"loopActive ", "6.1: same"},
		{"collectUnconfirmedReceipts", "5.7: old collect-chain replaced by direct envelope reconcile"},
		{"persistInboxReceipt(", "5.3: fresh-key receipt minting deleted (reserved-key commit only)"},
	}
	var files []string
	for _, dir := range []string{".", "agent", "memory", "event", "tool"} {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	require.NotEmpty(t, files)
	for _, b := range banned {
		for _, f := range files {
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			// Comments legitimately RECORD the deletion ("the old
			// task_stale_after is gone"); the elimination list governs LIVE
			// code, so scan only non-comment lines.
			for i, line := range strings.Split(string(src), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				require.NotContains(t, line, b.pattern,
					"§8.7 elimination list: %s must not appear in live code (%s) — found in %s:%d",
					b.pattern, b.why, f, i+1)
			}
		}
	}
}

// TestLatestPathOnly_ThreeBootStates runs the dynamic half: fresh boot,
// current-state restart, and post-reset boot — one real durable turn each,
// with legacy markers that must remain untouched inert.
func TestLatestPathOnly_ThreeBootStates(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	spillDir := filepath.Join(root, "spill")
	anchorDir := filepath.Join(root, "anchor")
	boot := func(m *drillModel) *agent.TagentAgent {
		ta, err := New(Config{
			Entry: "tagent",
			Agents: map[string]AgentConfig{"tagent": {
				SystemPrompt: PromptConfig{Inline: "three-state"},
				MaxTokens:    4000,
				Memory:       MemoryConfig{Type: "localfile", Path: storeDir},
			}},
			Reliability: ReliabilityConfig{BusSpillDir: spillDir, MeditationAnchorDir: anchorDir},
		}, WithModel(m))
		require.NoError(t, err)
		return ta
	}
	// oneTurn drives a real input through the CURRENT path to the model.
	oneTurn := func(ta *agent.TagentAgent, m *drillModel, tag string) {
		out, err := ta.StartLoop("u", "tri-session")
		require.NoError(t, err)
		drop := make(chan struct{})
		go func() {
			defer close(drop)
			for range out {
			}
		}()
		_, err = ta.InjectMessageContext(context.Background(), "user", model.NewUserMessage(tag))
		require.NoError(t, err)
		require.Eventually(t, func() bool { return drillSaw(m, tag) }, 15*time.Second, 20*time.Millisecond,
			"boot must run the CURRENT path to the model (tag %s)", tag)
		require.NoError(t, ta.Close())
		<-drop
	}

	// STATE 1 — brand-new empty dirs: boot + one real durable turn.
	m1 := &drillModel{}
	oneTurn(boot(m1), m1, "tri-fresh-turn")

	// Plant LEGACY markers while down: v1 item + stray spill file.
	legacyV1 := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyV1), 0o755))
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	legacySpill := filepath.Join(spillDir, "tagent", "job.spill")
	require.NoError(t, os.WriteFile(legacySpill, []byte("spill"), 0o644))

	// STATE 2 — restart over the CURRENT state with legacy markers alongside:
	// only the latest protocol runs; markers stay byte-identical (read-not,
	// migrated-not, wiped-not) and prior facts ARE in the projection.
	m2 := &drillModel{}
	before := readFile(t, legacyV1)
	ta2 := boot(m2)
	out2, err := ta2.StartLoop("u", "tri-session")
	require.NoError(t, err)
	drop2 := make(chan struct{})
	go func() {
		defer close(drop2)
		for range out2 {
		}
	}()
	_, err = ta2.InjectMessageContext(context.Background(), "user", model.NewUserMessage("tri-restart-turn"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return drillSaw(m2, "tri-restart-turn") }, 15*time.Second, 20*time.Millisecond)
	require.True(t, drillSaw(m2, "tri-fresh-turn"), "current-state facts ARE recovered into the projection")
	require.NoError(t, ta2.Close())
	<-drop2
	require.Equal(t, before, readFile(t, legacyV1), "legacy v1 item is inert: never read, rewritten or removed")
	require.FileExists(t, legacySpill, "legacy spill is inert")

	// STATE 3 — after the managed unit reset (§8.6 orchestration) boot again:
	// current-format only, nothing recoverable, no pre-reset input resurfaces.
	require.NoError(t, os.MkdirAll(anchorDir, 0o755))
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "the authorized reset cleared the legacy set")
	m3 := &drillModel{}
	ta3 := boot(m3)
	s3, err := ta3.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, agent.ReconcileSummary{}, s3, "reset-then-boot recovers nothing (authorized discard, no half-state)")
	oneTurn(ta3, m3, "tri-postreset-turn")
	require.False(t, drillSaw(m3, "tri-fresh-turn") || drillSaw(m3, "tri-restart-turn"),
		"post-reset projection carries NO pre-reset input — clearing old data is never passed off as processed")
}

func drillSaw(m *drillModel, sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.reqs {
		for _, msg := range req {
			if strings.Contains(msg.Content, sub) {
				return true
			}
		}
	}
	return false
}

func readFile(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}
