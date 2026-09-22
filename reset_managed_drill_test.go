package tagent

// §8.6 — MANAGED-ROOT RESET DRILL, run entirely inside temporary managed dirs
// (never a real unspecified directory). Consistent recovery-unit reset: the
// store, the inbox (v2 live tree + transitional legacy) and the meditation
// anchor are ONE unit — a reset clears them all or nothing. Guards drilled
// here: live-writer refusal (flock), explicit-confirm refusal, path-escape
// safety (symlink victims survive), unmanaged content preserved, quarantine
// evidence never auto-wiped (current corruption must be dispositioned by the
// operator first), and a post-reset boot that only knows the current format.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// drillModel records every real request it is shown and answers one turn.
type drillModel struct {
	mu   sync.Mutex
	reqs [][]model.Message
}

func (m *drillModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	snap := make([]model.Message, len(req.Messages))
	copy(snap, req.Messages)
	m.reqs = append(m.reqs, snap)
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "drill-ack"}}}}
	close(ch)
	return ch, nil
}

func (m *drillModel) Info() model.Info { return model.Info{Name: "drill-model"} }

// drillResetManagedUnits is the operator-side orchestration: probe every gate
// FIRST (all-or-nothing), then remove only managed-layout files.
func drillResetManagedUnits(storeDir, spillParent, anchorDir, agentName string, confirm bool) ([]string, error) {
	if !confirm {
		return nil, fmt.Errorf("drill reset: requires explicit confirmation (destructive operator act)")
	}
	// Gate 1 — live writers: the store's cross-process single-writer flock
	// must be acquirable (in-flight owner ⇒ refuse with zero changes).
	lockF, err := acquireDirLock(storeDir)
	if err != nil {
		return nil, fmt.Errorf("drill reset: live writer on %s: %w", storeDir, err)
	}
	defer func() { _ = unlockDirLock(lockF) }() // unlock closes

	// Gate 2 — verify-and-enumerate before touching anything: an unreadable
	// CURRENT backend (corruption / I/O trouble) is never treated as legacy
	// data to sweep.
	kvStore, err := kv.NewLocalFileKV(storeDir)
	if err != nil {
		return nil, fmt.Errorf("drill reset: store backend unreadable (current trouble, NOT transitional): %w", err)
	}
	store, err := memory.NewFileSegmentStore(kvStore, nil, storeDir, 100)
	if err != nil {
		return nil, fmt.Errorf("drill reset: store open failed: %w", err)
	}
	if err := store.RebuildLiveCounts(); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("drill reset: current-format scan failed — refusing to wipe: %w", err)
	}
	_ = store.Close()
	probe, err := agent.NewReliableEventBus(filepath.Join(spillParent, agentName))
	if err != nil {
		return nil, fmt.Errorf("drill reset: inbox undisposable/quarantine undispositioned: %w", err)
	}
	_ = probe.CloseDurable() // verification only — the leaf reopens post-removal

	var removals []string
	// Store unit: only the managed layout (kv snapshot + its tmp).
	// Exact managed layout names (review 7677c07 #2): LocalFileKV writes
	// "kv.json" + its single "kv.json.tmp"; envelope-style tmps live under
	// the inbox unit and are matched there by pattern.
	for _, pat := range []string{"kv.json", "kv.json.tmp"} {
		m, _ := filepath.Glob(filepath.Join(storeDir, pat))
		removals = append(removals, m...)
	}
	// Inbox unit: live-tree envelopes of the unit (quarantine/ NOT matched)
	// plus leaf-classified transitional legacy via the leaf's own guarded API.
	live, _ := filepath.Glob(filepath.Join(spillParent, agentName, "inbox-v2", "*.json"))
	removals = append(removals, live...)
	// Anchor unit: only <anchorDir>/<agent>.json.
	removals = append(removals, filepath.Join(anchorDir, agentName+".json"))

	// All gates passed — commit the unit reset.
	var removed []string
	for _, p := range removals {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("drill reset remove %s: %w", p, err)
		}
		if _, statErr := os.Lstat(p); statErr != nil {
			removed = append(removed, p)
		}
	}
	// The leaf's guarded transitional sweep runs on a FRESH instance so its
	// unacked ledger matches the post-removal disk (the point of a unit reset).
	leaf, err := agent.NewReliableEventBus(filepath.Join(spillParent, agentName))
	if err != nil {
		return removed, fmt.Errorf("drill reset reopen: %w", err)
	}
	n, err := leaf.ResetTransitional(true)
	if err != nil {
		_ = leaf.CloseDurable()
		return removed, fmt.Errorf("drill reset transitional leaf: %w", err)
	}
	_ = leaf.CloseDurable()
	return append(removed, fmt.Sprintf("%d transitional file(s)", n)), nil
}

func TestDrill_ManagedRootReset_ConsistentUnitAndAllRefusals(t *testing.T) {
	root := t.TempDir() // THE managed dir — the drill never touches real dirs
	storeDir := filepath.Join(root, "store")
	spillDir := filepath.Join(root, "spill")
	anchorDir := filepath.Join(root, "anchor")
	require.NoError(t, os.MkdirAll(storeDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine"), 0o755))
	require.NoError(t, os.MkdirAll(anchorDir, 0o755))

	// --- seed: legacy transitional material -------------------------------
	legacyV1 := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	legacySpill := filepath.Join(spillDir, "tagent", "job.spill")
	require.NoError(t, os.WriteFile(legacySpill, []byte("spill"), 0o644))
	// symlink-escape bait inside the legacy set: removing the LINK must never
	// touch its outside victim.
	victim := filepath.Join(root, "outside-victim.json")
	require.NoError(t, os.WriteFile(victim, []byte("DO-NOT-DELETE"), 0o644))
	require.NoError(t, os.Symlink(victim, filepath.Join(spillDir, "tagent", "inbox-v1", "escape.json")))

	// --- seed: CURRENT-format unit data (store facts + live envelope + anchor)
	kvStore, err := kv.NewLocalFileKV(storeDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, storeDir, 100)
	require.NoError(t, err)
	k := memory.NewSnowflakeEventKey(1, 0)
	require.NoError(t, store.StoreEvent(k, memory.FullEvent{EventKey: k, PartitionID: 1, EventType: "external_input", Content: "current-fact", Timestamp: 1}))
	require.NoError(t, store.Close())
	bus, err := agent.NewReliableEventBus(filepath.Join(spillDir, "tagent"))
	require.NoError(t, err)
	_, err = bus.PublishContext(context.Background(), agent.NewExternalInputEvent("user", model.NewUserMessage("pre-reset-envelope")))
	require.NoError(t, err)
	require.NoError(t, bus.CloseDurable())
	require.NoError(t, os.WriteFile(filepath.Join(anchorDir, "tagent.json"), []byte("anchor"), 0o644))

	// --- seed: UNMANAGED content + quarantine evidence (must survive) -----
	unmanaged := filepath.Join(storeDir, "user-notes.txt")
	require.NoError(t, os.WriteFile(unmanaged, []byte("mine"), 0o644))
	evidence := filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine", "evidence-1.json")
	require.NoError(t, os.WriteFile(evidence, []byte(`{"corrupt":true}`), 0o644))

	// LEG b1 — confirm gate: refusal, zero changes.
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", false)
	require.Error(t, err)
	require.FileExists(t, legacyV1)

	// LEG b2 — quarantine evidence blocks the reset until the operator
	// dispositions it: the CURRENT corruption is never swept as "legacy".
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.Error(t, err, "undispositioned quarantine must refuse the unit reset")
	require.FileExists(t, evidence, "quarantine evidence files are NEVER deleted by the reset")
	require.FileExists(t, legacyV1, "refusal means ZERO changes")
	require.FileExists(t, filepath.Join(storeDir, "kv.json"), "refusal means ZERO changes")

	// Operator dispositions the evidence (moves it out — the human act).
	require.NoError(t, os.Rename(evidence, filepath.Join(root, "dispositioned-1.json")))

	// LEG c — live writer refuses: hold the store's flock, reset must refuse
	// with zero changes, then succeed once the writer leaves.
	held, err := acquireDirLock(storeDir)
	require.NoError(t, err)
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.ErrorIs(t, err, ErrStoreLocked, "a live writer must be refused")
	require.FileExists(t, legacyV1)
	require.NoError(t, unlockDirLock(held)) // unlockDirLock closes the file

	// LEG d — the consistent unit reset itself.
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "transitional v1 cleared")
	require.NoFileExists(t, legacySpill, "legacy spill cleared")
	require.NoFileExists(t, filepath.Join(storeDir, "kv.json"), "store unit cleared — consistency")
	require.NoFileExists(t, filepath.Join(anchorDir, "tagent.json"), "anchor unit cleared")
	liveLeft, _ := filepath.Glob(filepath.Join(spillDir, "tagent", "inbox-v2", "*.json"))
	require.Empty(t, liveLeft, "live envelopes cleared with the store (no unit half-reset)")
	// Survivors: unmanaged content, the symlink's victim, quarantine DIR.
	require.FileExists(t, unmanaged, "unmanaged content is never removed")
	require.FileExists(t, victim, "path escape removed at most the link, never the outside target")
	require.DirExists(t, filepath.Dir(evidence))

	// LEG e — post-reset boot ONLY knows the current path. Runs as an
	// independent process (boot evidence layer + the registered framework
	// race family can hit any main-process runner boot/Close; the child
	// asserts everything below and the parent classifier fails hard on any
	// non-family failure).
	runRaceExemptChild(t, append(os.Environ(),
		"TAGENT_DRILL_STORE="+storeDir,
		"TAGENT_DRILL_SPILL="+spillDir,
		"TAGENT_DRILL_ANCHOR="+anchorDir),
		"TAGENT_DRILL_PHASE=boot-turn", "TestDrill_ManagedRootResetBootChild$")
}

// TestDrill_ManagedRootReset boot-phase child: drives one post-reset turn.
func TestDrill_ManagedRootResetBootChild(t *testing.T) {
	if os.Getenv("TAGENT_DRILL_PHASE") != "boot-turn" {
		t.Skip("drill boot child")
	}
	storeDir := os.Getenv("TAGENT_DRILL_STORE")
	spillDir := os.Getenv("TAGENT_DRILL_SPILL")
	anchorDir := os.Getenv("TAGENT_DRILL_ANCHOR")
	m := &drillModel{}
	ta, err := New(Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{"tagent": {
			SystemPrompt: PromptConfig{Inline: "post-reset"},
			MaxTokens:    4000,
			Memory:       MemoryConfig{Type: "localfile", Path: storeDir},
		}},
		Reliability: ReliabilityConfig{BusSpillDir: spillDir, MeditationAnchorDir: anchorDir},
	}, WithModel(m))
	require.NoError(t, err, "reset-then-boot under the CURRENT format")
	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, agent.ReconcileSummary{}, s, "nothing recoverable survived the unit reset (as authorized)")
	out, err := ta.StartLoop("u", "post-reset-session")
	require.NoError(t, err)
	drop := make(chan struct{})
	go func() {
		defer close(drop)
		for range out {
		}
	}()
	_, err = ta.InjectMessageContext(context.Background(), "user", model.NewUserMessage("post-reset-first-input"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, req := range m.reqs {
			for _, msg := range req {
				if strings.Contains(msg.Content, "post-reset-first-input") {
					return true // Contains: the framework guard may decorate the input (§7.4 precedent)
				}
			}
		}
		return false
	}, 15*time.Second, 20*time.Millisecond, "post-reset boot must reach the model on the CURRENT path")
	require.NoError(t, ta.Close())
	<-drop
}
