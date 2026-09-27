package tagent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// l3YAML renders a single-"main" org. `prompt` is FINGERPRINTED (system_prompt);
// keep/max/threshold/terminal are hot-applicable (excluded from the org
// fingerprint), so changing only those takes the numeric-only path and changing
// prompt forces a structural swap — the two axes §2.4/L-3 keeps distinct.
func l3YAML(prompt string, keep, max int, threshold float64, terminal string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: " + strconv.Quote(prompt) + "\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keep) + "\n" +
		"    max_tokens: " + strconv.Itoa(max) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(threshold, 'f', -1, 64) + "\n" +
		"    task_terminal_ttl: " + strconv.Quote(terminal) + "\n" +
		"    memory:\n      type: memory\n"
}

func di64(t *testing.T, d map[string]any, k string) int64 {
	t.Helper()
	v, ok := d[k]
	require.Truef(t, ok, "diagnostics missing %q", k)
	n, ok := v.(int64)
	require.Truef(t, ok, "diagnostics %q is %T, want int64", k, v)
	return n
}

func diagTime(t *testing.T, d map[string]any, k string) (time.Time, bool) {
	t.Helper()
	v, ok := d[k]
	if !ok {
		return time.Time{}, false
	}
	tt, ok := v.(time.Time)
	require.Truef(t, ok, "diagnostics %q is %T, want time.Time", k, v)
	return tt, true
}

// TestL3_FullConfigAndRollback drives the whole D9/L-3 contract against a real
// agent built by tagent.New: numeric-only applications must rotate the rollback
// ring (so a later Rollback restores the pre-edit hot values), bump revision and
// lastAppliedAt WITHOUT a structural generation bump, an identical re-save must
// not rotate, a rollback must publish a new generation restoring structure AND
// the five hot params, and a rejected candidate must leave both axes untouched.
// Every value assertion reads the REAL consumer (compressor budget/keepRecent,
// TaskManager TTL), never a resident config echo.
func TestL3_FullConfigAndRollback(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(l3YAML("A", 2, 4000, 0.5, "1m"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// Baseline: consumers seeded; startup is generation 0, revision 0, no publish.
	require.Equal(t, 2, entry.OrgKeepRecent())
	require.Equal(t, 2000, entry.OrgBudgetLine()) // 4000 × 0.5
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, di64(t, d, "generation"))
	require.EqualValues(t, 0, di64(t, d, "revision"))
	_, hasPub := diagTime(t, d, "lastPublishedAt")
	require.False(t, hasPub, "startup is not a structural publish")

	// (1) First STRUCTURAL update (prompt A→B): generation 1, revision 1, a
	// structural publish time appears, hot consumers unchanged (only prompt moved).
	write(l3YAML("B", 2, 4000, 0.5, "1m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, di64(t, d, "generation"), "structural change advances the generation")
	require.EqualValues(t, 1, di64(t, d, "revision"), "a structural publish is also a full apply")
	pub1, hasPub := diagTime(t, d, "lastPublishedAt")
	require.True(t, hasPub)
	require.Equal(t, 2000, entry.OrgBudgetLine(), "prompt-only change leaves the budget")

	// (2) NUMERIC-ONLY update (keep 2→7, max 4000→9000, terminal 1m→5m): the
	// real consumers move, revision bumps, generation does NOT, lastPublishedAt
	// stays frozen (no structure moved), lastAppliedAt advances.
	lastApplied1, _ := diagTime(t, d, "lastAppliedAt")
	write(l3YAML("B", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, di64(t, d, "generation"), "numeric-only must NOT bump the structural generation")
	require.EqualValues(t, 2, di64(t, d, "revision"), "numeric-only is a full apply → bumps revision")
	require.Equal(t, 7, entry.OrgKeepRecent(), "compressor keepRecent is the real consumer")
	require.Equal(t, 4500, entry.OrgBudgetLine(), "9000 × 0.5 must reach the effective budget line")
	require.Equal(t, 5*time.Minute, entry.TaskManager().TerminalTTL(), "TaskManager TTL is the real consumer")
	pub2, _ := diagTime(t, d, "lastPublishedAt")
	require.True(t, pub2.Equal(pub1), "lastPublishedAt must NOT move on a numeric-only apply")
	lastApplied2, _ := diagTime(t, d, "lastAppliedAt")
	require.True(t, lastApplied2.After(lastApplied1), "lastAppliedAt advances on a numeric-only apply")

	// (3) IDENTICAL re-save (same numerics, mtime bumped): must not rotate.
	write(l3YAML("B", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, di64(t, d, "revision"), "semantically identical apply must not rotate/advance (D9)")
	require.EqualValues(t, 1, di64(t, d, "generation"))

	// (4) ROLLBACK restores the PRE-NUMERIC full config: keep/max/terminal revert
	// (this is what numeric-only ring rotation bought), and it publishes a NEW
	// generation + advances lastPublishedAt.
	revBeforeRollback := di64(t, d, "revision")
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, di64(t, d, "generation"), "rollback republishes as a new generation")
	require.EqualValues(t, revBeforeRollback+1, di64(t, d, "revision"))
	require.Equal(t, 2, entry.OrgKeepRecent(), "rollback restores the pre-numeric keepRecent")
	require.Equal(t, 2000, entry.OrgBudgetLine(), "rollback restores the pre-numeric budget (structure + hot params together)")
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL(), "rollback restores the pre-numeric terminal TTL")
	pub3, _ := diagTime(t, d, "lastPublishedAt")
	require.True(t, pub3.After(pub1), "rollback is a structural publish → lastPublishedAt advances")

	// (5) CONSECUTIVE rollback: the ring faces the same source (documented
	// ping-pong), so once the current generation already equals that source a
	// further Rollback is a SAFE NO-OP — it must not half-swap, panic, or spin a
	// new generation for identical content (sameFullAsCurrent short-circuit).
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, di64(t, d, "generation"), "a rollback to identical content must not bump the generation")
	require.Equal(t, 2, entry.OrgKeepRecent(), "values stay at the restored source, unchanged")
	require.Equal(t, 2000, entry.OrgBudgetLine())
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL())

	// (6) A NEW numeric-only change followed by rollback still walks forward:
	// generation advances to 3 and the fresh value is reverted, proving the ring
	// is not permanently stuck after the (5) short-circuit.
	write(l3YAML("B", 9, 4000, 0.5, "1m"))
	entry.CheckOrgReload()
	require.Equal(t, 9, entry.OrgKeepRecent())
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 3, di64(t, d, "generation"), "rollback after a real change publishes a new generation")
	require.Equal(t, 2, entry.OrgKeepRecent(), "the fresh numeric edit is rolled back to the ring source")
}

// TestL3_RollbackHookSurvivesNumericOnlyFirstUpdate closes the §2.4 reopened
// gap: the rollback hook used to be installed only inside the structural-publish
// branch, so an org whose FIRST update was numeric-only had a rotated rollback
// ring (recordHotApply) but no hook — Rollback() was a silent no-op, violating
// D9 "rollback uses the previous full effective config". The hook must therefore
// be installed once at reloader assembly, independent of which branch fires
// first. Scenario: startup → numeric-only → Rollback restores the startup hot
// values and publishes a new generation (real consumers, not getters).
func TestL3_RollbackHookSurvivesNumericOnlyFirstUpdate(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(l3YAML("A", 2, 4000, 0.5, "1m"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()
	require.Equal(t, 2, entry.OrgKeepRecent())

	// FIRST update is numeric-only: consumers move, ring rotates, no structure.
	write(l3YAML("A", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, di64(t, d, "generation"), "numeric-only must not bump the generation")
	require.EqualValues(t, 1, di64(t, d, "revision"), "numeric-only is a full apply")
	require.Equal(t, 7, entry.OrgKeepRecent())
	require.Equal(t, 4500, entry.OrgBudgetLine())

	// Rollback with NO structural publish ever must still restore the startup
	// values through the real consumers and publish a new generation. Before
	// the fix this was a silent no-op (hook never installed): keep stays 7.
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, di64(t, d, "generation"), "rollback publishes a new generation")
	require.EqualValues(t, 2, di64(t, d, "revision"))
	require.Equal(t, 2, entry.OrgKeepRecent(), "rollback restores the startup keepRecent")
	require.Equal(t, 2000, entry.OrgBudgetLine(), "rollback restores the startup budget")
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL(), "rollback restores the startup terminal TTL")

	// The ring walked forward: a second rollback faces identical content and is
	// a safe no-op (values stay at the restored startup config).
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, di64(t, d, "generation"), "a rollback to identical content must not spin a new generation")
	require.Equal(t, 2, entry.OrgKeepRecent())
}

// TestL3_RejectedCandidateKeepsBothAxes: a broken config after a numeric-only
// apply must leave generation, revision, lastAppliedAt and the real consumers
// exactly as they were — 「失败两轴均保持当前值」 (no half-swap).
func TestL3_RejectedCandidateKeepsBothAxes(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(l3YAML("A", 3, 5000, 0.6, "2m"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	write(l3YAML("A", 6, 8000, 0.6, "2m")) // numeric-only, applies
	entry.CheckOrgReload()
	before := entry.OrgDiagnostics()
	require.Equal(t, 6, entry.OrgKeepRecent())
	require.Equal(t, 4800, entry.OrgBudgetLine()) // 8000 × 0.6
	genB, revB := di64(t, before, "generation"), di64(t, before, "revision")
	appliedB, _ := diagTime(t, before, "lastAppliedAt")

	write("entry: [this is not valid yaml")
	entry.CheckOrgReload()
	after := entry.OrgDiagnostics()
	require.EqualValues(t, genB, di64(t, after, "generation"), "a rejection must not advance the generation")
	require.EqualValues(t, revB, di64(t, after, "revision"), "a rejection must not advance the revision")
	appliedA, _ := diagTime(t, after, "lastAppliedAt")
	require.True(t, appliedA.Equal(appliedB), "a rejection must not move lastAppliedAt")
	require.NotNil(t, after["lastFailure"], "the rejection reason must be observable")
	// Real consumers unchanged (no half-swap).
	require.Equal(t, 6, entry.OrgKeepRecent())
	require.Equal(t, 4800, entry.OrgBudgetLine())
}

// TestL3_CoordinatorHotApplyRevision pins the coordinator state machine directly
// (no agent): identical hot applies do not rotate or advance, real ones do, and
// none of them touch the structural generation or lastPublishedAt.
func TestL3_CoordinatorHotApplyRevision(t *testing.T) {
	base := &Config{Entry: "main", Agents: map[string]AgentConfig{
		"main": {KeepRecentTasks: 2, MaxTokens: 4000, CompressThreshold: 0.5},
	}}
	fp := mustFP(t, base)
	c := newOrgCoordinator()
	c.init(fp, base)
	require.EqualValues(t, 0, c.status().Revision)
	require.Zero(t, c.status().LastPublished)

	// Structural publish: gen 1, revision 1, publish time set.
	c.swap(fp, base, nil)
	require.EqualValues(t, 1, c.status().Generation)
	require.EqualValues(t, 1, c.status().Revision)
	require.False(t, c.status().LastPublished.IsZero())
	pubAt := c.status().LastPublished

	// Identical hot apply: no rotation, no advance.
	require.False(t, c.recordHotApply(base, nil))
	require.EqualValues(t, 1, c.status().Revision)
	require.EqualValues(t, 1, c.status().Generation)

	// Changed hot apply: rotates the ring + bumps revision + advances lastApplied,
	// but leaves generation and lastPublishedAt frozen.
	changed := &Config{Entry: "main", Agents: map[string]AgentConfig{
		"main": {KeepRecentTasks: 7, MaxTokens: 9000, CompressThreshold: 0.5},
	}}
	require.True(t, c.recordHotApply(changed, nil))
	st := c.status()
	require.EqualValues(t, 2, st.Revision)
	require.EqualValues(t, 1, st.Generation)
	require.True(t, st.LastPublished.Equal(pubAt), "hot apply must not move lastPublishedAt")
	require.False(t, st.LastApplied.Equal(st.LastPublished))

	// The rollback source now holds the PRE-change full config.
	src := c.rollbackSource()
	require.NotNil(t, src)
	require.NotNil(t, src.cfg)
	require.Equal(t, 2, src.cfg.Agents["main"].KeepRecentTasks)
}
