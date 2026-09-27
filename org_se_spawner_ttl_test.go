package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/require"
)

// seSpawnerTTLYAML: entry main → sub1, and sub1 owns the plain `exec` tool (the
// ActionTool). task_default_ttl is the only axis moved between renders, and it
// lives in hotSignature (not the structural fingerprint), so a change takes the
// numeric-only commit branch — exactly the branch where the retired push model
// left the spawner holding a construction-frozen number.
func seSpawnerTTLYAML(t testing.TB, ttl string) string {
	t.Helper()
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
  sub1:
    system_prompt:
      inline: "sub1"
    tools:
      - kind: tool
        id: exec
        description: "shell"
    task_default_ttl: %q
    memory:
      type: localfile
      path: %q
`, ttl, testStore(t, "hottest-se-spawner"))
}

// seActionToolOf returns the ActionTool on an owner's live tool face — the very
// instance a spawn goes through, not a freshly built one. Tools are wrapped by
// `OutputLimitTool` at agent construction (agent.go:433) while the composition
// root binds the TTL source to the INNER instance (build_agent.go), so the
// unwrap here is what proves the two are the same object rather than a copy.
func seActionToolOf(t *testing.T, a *agent.TagentAgent) *action.ActionTool {
	t.Helper()
	require.NotNil(t, a, "owner 须在常驻表")
	for _, tl := range a.Tools() {
		if olt, ok := tl.(*agent.OutputLimitTool); ok {
			tl = olt.Unwrap()
		}
		if at, ok := tl.(*action.ActionTool); ok {
			return at
		}
	}
	t.Fatal("sub1 的工具面里须有 ActionTool（kind: tool / id: exec）")
	return nil
}

// TestSE_SpawnerTTLReachesRealSpawnSpec is 6.4's spawner-axis end-to-end anchor
// (introduce-durable-workflow-engine, 轮八十 — added when 轮七十九 reopened the
// task after discovering ActionTool was a second TTL authority).
//
// It asserts the HOST RESULT, not a getter echo: the `task.TaskSpec` handed to
// the task layer for a spawn that omits `ttl` must carry the number currently in
// the committed application record, observed through the SAME ActionTool instance
// the owner has been serving since startup. Under the retired push model the
// record rotated and the TaskManager (which pulls) followed, while this tool kept
// the number pushed at its construction — so the second assertion below is the
// one that would have failed before the fix.
func TestSE_SpawnerTTLReachesRealSpawnSpec(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(seSpawnerTTLYAML(t, "30m"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	sub1 := residentCacheForTest(entry)["sub1"]
	at := seActionToolOf(t, sub1)
	genBefore := entry.OrgDiagnostics()["generation"].(int64)

	decl := task.Declarative{Kind: "command", Command: "sleep 1", Desc: "board row", TaskID: "sess-se"}
	require.Equal(t, 30*time.Minute, at.SpecFromDeclarative(nil, decl).TTL,
		"初始 spawn 规格须取配置的 task_default_ttl=30m（经源解析），不是构造地板 10m")

	// numeric-only 旋转：只改 task_default_ttl，结构指纹不变。
	write(seSpawnerTTLYAML(t, "45m"))
	entry.CheckOrgReload()

	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"].(int64),
		"task_default_ttl 变更须落在 numeric-only 分支（不加结构代）")
	require.Equal(t, 45*time.Minute, scHot(t, sub1).TaskDefaultTTL, "记录轴须已轮转到 45m")
	require.Equal(t, 45*time.Minute, at.SpecFromDeclarative(nil, decl).TTL,
		"6.4 spawner 轴锚：同一个 ActionTool 实例（无 push、无重装配）的下一次 spawn 规格必须现读记录新值")

	// 记录不外溢：模型显式声明的 ttl 仍优先。
	explicit := decl
	explicit.Params = map[string]string{"ttl": "7"}
	require.Equal(t, 7*time.Second, at.SpecFromDeclarative(nil, explicit).TTL,
		"显式 ttl 参数须优先于记录默认")
}
