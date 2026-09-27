package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// deshellYAML renders the de-shell fixture (S-A/2.3): entry main delegating to
// sub1+sub2, each with an OWN model so a per-agent model change is a structural
// fingerprint delta (models move the fingerprint; prompts/memory here stay
// byte-identical unless the case says otherwise).
func deshellYAML(t testing.TB, mainModel, sub1Model, sub2Model string, sub3 bool) string {
	t.Helper()
	sub3Tool := ""
	sub3Block := ""
	if sub3 {
		// sub3 must be REFERENCED from main's tools or reachableAgents excludes
		// it — an unreferenced declaration is not a hot-add (and never builds).
		sub3Tool = `      - kind: agent
        agent: sub3
        description: "sub3"
`
		sub3Block = fmt.Sprintf(`  sub3:
    system_prompt:
      inline: "sub3"
    model: %s
    memory:
      type: localfile
      path: %q
`, sub1Model+"-x", testStore(t, "hottest-sub3"))
	}
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    model: %s
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
      - kind: agent
        agent: sub2
        description: "sub2"
%s  sub1:
    model: %s
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
  sub2:
    model: %s
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
%s`, mainModel, sub3Tool, sub1Model, testStore(t, "hottest-sub1"), sub2Model, testStore(t, "hottest-sub2"), sub3Block)
}

// deshellHarness boots the org from deshellYAML(m0, s1, s2, sub3), returns the
// entry, the config path and an mtime-forcing writer (same tick discipline as
// the e2e family — FS granularity would otherwise swallow rapid rewrites).
func deshellHarness(t *testing.T, m0, s1, s2 string, sub3 bool) (*agent.TagentAgent, string) {
	t.Helper()
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(yamlPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		tick = tick.Add(2 * time.Second)
		if err := os.Chtimes(yamlPath, tick, tick); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	write(deshellYAML(t, m0, s1, s2, sub3))
	cfg, err := LoadConfig(yamlPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = entry.Close() })
	return entry, yamlPath
}

// deshellReload mutates the config to (m1, s1b, s2b, sub3) and drives the
// synchronous check; it FAILS the test unless the structural publish actually
// happened (generation advanced) — a skipped reload would trivially satisfy any
// construction-count assertion.
func deshellReload(t *testing.T, entry *agent.TagentAgent, yamlPath, m1, s1b, s2b string, sub3 bool) {
	t.Helper()
	before := entry.OrgDiagnostics()["generation"].(int64)
	if err := os.WriteFile(yamlPath, []byte(deshellYAML(t, m1, s1b, s2b, sub3)), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(yamlPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	entry.CheckOrgReload()
	after := entry.OrgDiagnostics()["generation"].(int64)
	require.Greater(t, after, before, "structural reload did not publish — construction-count assertions below would be vacuous")
}

// TestDeshell_EntryRegenerationConstructsZeroAgents is the S-A red anchor: a
// hot reload that only MODIFIES the entry (model swap; sub1/sub2 byte-identical)
// must construct ZERO TagentAgents. The discarded entry shell was the entire
// point of D1 去壳 — one full agent (bus/TaskManager/cleaner) per publish,
// thrown away.
func TestDeshell_EntryRegenerationConstructsZeroAgents(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A: modifying an existing agent must regenerate through the face, not construct shells")
}

// TestDeshell_ChangedSubAgentConstructsOneTransitional pinned the transitional
// cost of S-A: modifying sub1 (entry untouched) constructed EXACTLY ONE agent —
// sub1's transitional executor carrier.〔轮九十迁移（显式）〕S-D/3.2 has now
// landed (user-approved holding expansion): every owner's execution view advances
// through staged faces wired to stable resident instances, and the transitional
// shell is DEAD — the terminal ZERO this anchor always named as its own end state.
// The name keeps the history; the pinned number is the terminal one.
func TestDeshell_ChangedSubAgentConstructsOneTransitional(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1x", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-D terminal: a changed sub-agent advances through its staged face on the stable resident instance — the transitional carrier is gone")
}

// TestDeshell_HotAddConstructsExactlyTheNewAgent pins J2 (去壳≠去能力): a
// hot-add builds the new agent fully (ONE construction) and — de-shelled — no
// longer re-shells entry/unchanged siblings around it.
func TestDeshell_HotAddConstructsExactlyTheNewAgent(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1", "sub-m2", true)
	require.Equal(t, int64(1), agent.TagentAgentsConstructed()-before,
		"J2: hot-add constructs exactly the new full agent; entry/unchanged siblings must not re-construct")
}

// TestDeshell_RollbackConstructsZeroForEntryOnlyChange extends the anchor to
// the rollback path (same de-shelled treatment): rolling back an entry-only
// structural change constructs ZERO agents.
func TestDeshell_RollbackConstructsZeroForEntryOnlyChange(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	entry.Rollback()
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A rollback: entry-only rollback must regenerate through the face, not shells")
}
