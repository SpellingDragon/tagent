package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// hotYAML renders the three-agent topology used by the hot-reload tests:
// entry references sub1+sub2 (kind: agent); each agent has its OWN isolated
// memory store so per-agent store identity is observable.
func hotYAML(keep1, keep2 int, entryExtraTool bool) string {
	extra := ""
	if entryExtraTool {
		extra = fmt.Sprintf("      - kind: tool\n        id: recall\n        description: %q\n", "extra-"+fmt.Sprint(time.Now().UnixNano()))
	}
	keepLine := func(n int) string {
		if n <= 0 {
			return "" // omitted → field deleted → falls back to parsed default (4.7)
		}
		return fmt.Sprintf("    keep_recent_tasks: %d\n", n)
	}
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
      - kind: agent
        agent: sub2
        description: "sub2"
%s
  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
%s  sub2:
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
%s`, extra, filepath.Join("hottest-sub1"), keepLine(keep1), filepath.Join("hottest-sub2"), keepLine(keep2))
}

// LoadConfigForTest loads the YAML config (real strict parsing path).
func LoadConfigForTest(t *testing.T, path string) Config {
	t.Helper()
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	return *cfg
}

// residentCacheForTest returns the resident binding table (4.5 introspection).
func residentCacheForTest(ta *agent.TagentAgent) map[string]*agent.TagentAgent {
	return ta.ResidentTable()
}

func keepRecentOf(ta *agent.TagentAgent) int { return ta.OrgKeepRecent() }

// TestHotReload_MixedChange_IdentityAndParams（4.5/4.6/4.7）：
// entry+sub1+sub2 拓扑上同时变更工具（结构）与子 agent 预算（数值）：
// ① 各 agent 常驻 store 身份不漂移（指针不变，sub1/sub2 不被换成 entry 的）；
// ② 数值参数作用于新代真实对象（keepRecent 热生效）；
// ③ 字段删除回落解析默认（4.7 全量 desired）；
// ④ 新增 agent 拒绝热更（4.5 fail-closed）。
func TestHotReload_MixedChange_IdentityAndParams(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(content string) {
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
	}
	write(hotYAML(2, 2, false))

	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()

	// 常驻身份快照（3 agents：main + sub1 + sub2）。
	cache := residentCacheForTest(ta)
	require.Len(t, cache, 3)
	idMain := cache["main"].MemStore()
	idSub1 := cache["sub1"].MemStore()
	idSub2 := cache["sub2"].MemStore()
	require.NotSame(t, idSub1, idMain, "sub1 must own its own store")
	require.NotSame(t, idSub2, idMain, "sub2 must own its own store")

	// 混合变更：entry 加一个工具（结构，org fingerprint 变）+ sub1 keep_recent_tasks 2→5。
	write(hotYAML(5, 2, true))
	ta.CheckOrgReload()

	// ① 身份不漂移。
	require.Same(t, idMain, cache["main"].MemStore(), "entry store identity unchanged")
	require.Same(t, idSub1, cache["sub1"].MemStore(), "sub1 store identity unchanged (no drift to entry store)")
	require.Same(t, idSub2, cache["sub2"].MemStore(), "sub2 store identity unchanged")

	// ② 数值热生效于 sub1 的真实压缩器（新代对象）。
	require.Equal(t, 5, keepRecentOf(cache["sub1"]), "sub1 keepRecent hot-applied")
	// ③ sub2 字段未变 → 不受影响（仍为启动值 2 的语义）。
	require.Equal(t, 2, keepRecentOf(cache["sub2"]), "untouched agent keeps its configured value")

	// ④ 新增 agent 拒绝：fresh 拓扑多出 sub3。
	yaml := hotYAML(5, 2, false) + `
  sub3:
    system_prompt:
      inline: "sub3"
  main2:
    system_prompt:
      inline: "x"
`
	_ = yaml
	// 新增方式：把 sub3 挂进 main 的 tools（写入带 sub3 引用的完整配置）。
	write(fmt.Sprintf(`entry: main
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
      - kind: agent
        agent: sub2
        description: "sub2"
      - kind: agent
        agent: sub3
        description: "sub3-new"
  sub1:
    system_prompt:
      inline: "sub1"
  sub2:
    system_prompt:
      inline: "sub2"
  sub3:
    system_prompt:
      inline: "sub3"
`))
	ta.CheckOrgReload()
	// sub3 不得进入常驻绑定（拓扑增减 fail-closed）。
	require.Nil(t, residentCacheForTest(ta)["sub3"], "hot-added agent must be refused, never merged")
	// 原身份依旧不漂移。
	require.Same(t, idMain, cache["main"].MemStore())
}

func TestHotReload_MemoryRejectionNotEffective(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(content string) {
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
	}
	base := hotYAML(2, 2, false)
	write(base)
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()
	cache0 := residentCacheForTest(ta)
	idMainKeep := cache0["main"].MemStore()
	idSub1Keep := cache0["sub1"].MemStore()

	// memory 段变更（sub1 fsync off）→ 拒绝。
	modified := fmt.Sprintf(`entry: main
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
      - kind: agent
        agent: sub2
        description: "sub2"
  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
      fsync: false
    keep_recent_tasks: 2
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
    keep_recent_tasks: 2
`, filepath.Join("hottest-sub1"), filepath.Join("hottest-sub2"))
	write(modified)
	ta.CheckOrgReload()

	// 再次编辑（其他字段变化）→ 仍以 effective 为基准拒绝（不因第二次检查推进）。
	write(base)
	ta.CheckOrgReload()
	// 验证：拒绝路径不改变任何常驻身份/资源（effective 不推进）。
	cache := residentCacheForTest(ta)
	require.Same(t, idSub1Keep, cache["sub1"].MemStore(), "rejected reload must not touch resident stores")
	_ = idMainKeep
}

var _ model.Model = (*stubModel)(nil)

// TestHotReload_FieldDeletionFallsBackToDefault（4.7）：删除 keep_recent_tasks
// 字段 → 回落解析默认（2），而非保留上次热更的 5。
func TestHotReload_FieldDeletionFallsBackToDefault(t *testing.T) {
	// 4.7：断言对象是 sub1（keep_recent_tasks 5 写在 sub1；entry 无此字段）。
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(c string) { require.NoError(t, os.WriteFile(yamlPath, []byte(c), 0o644)) }
	write(hotYAML(5, 2, false))
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()
	sub1 := residentCacheForTest(ta)["sub1"]
	require.Equal(t, 5, sub1.OrgKeepRecent(), "startup honors explicit keep_recent_tasks on sub1")

	write(hotYAML(0, 2, false)) // field DELETED
	ta.CheckOrgReload()
	sub1 = residentCacheForTest(ta)["sub1"]
	require.Equal(t, 2, sub1.OrgKeepRecent(),
		"deleted field falls back to the parsed default (2) — full-desired semantics")
}
