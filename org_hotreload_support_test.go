// org_hotreload_support 是本族共享 fixture 与基准：只放非 Test 声明，不参与职责同位计数。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// cfgFor builds a minimal valid Config for fingerprint tests.
func cfgFor() *Config {
	return &Config{
		Entry:     "main",
		PromptDir: "resources/prompts",
		Providers: map[string]ProviderConfig{
			"p1": {Provider: "openai", APIEndpoint: "https://api.example.com"},
		},
		Agents: map[string]AgentConfig{
			"main": {
				Model:             "gpt-x",
				CompressThreshold: 0.8,
				Tools:             []ToolRef{{Kind: "tool", ID: "recall"}},
			},
			"sub": {
				Model: "gpt-y",
			},
		},
	}
}

// e2eYAML renders the minimal org config used by the hot-shift e2e test.
// Structural fields stay byte-identical across renders so only
// compress_threshold (hot-applicable, fingerprint-excluded) or an explicit
// org field can move the fingerprint.
func e2eYAML(threshold float64, model string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n    model: " + model + "\n" +
		"    system_prompt:\n      inline: \"e2e hot shift\"\n" +
		"    compress_threshold: " + strconv.FormatFloat(threshold, 'f', -1, 64) + "\n" +
		"    memory:\n      type: memory\n"
}

func ownerYAMLWithModel(t testing.TB, model string, targets []string, sub2Mem string) string {
	t.Helper()
	return strings.Replace(ownerYAML(t, targets, sub2Mem), "model: test-model\n", "model: "+model+"\n", 1)
}

// writeBumped 写入 yaml 后把 mtime 严格递增，绕开文件系统时间戳粒度让相邻两次写入落在同一刻、mtime 比较因此漏检变更的问题。
func writeBumped(tb testing.TB, yamlPath, content string, tick *time.Time) {
	tb.Helper()
	require.NoError(tb, os.WriteFile(yamlPath, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(tb, os.Chtimes(yamlPath, *tick, *tick))
}

// BenchmarkOrgReloadHotPath 度量 turn 起点检查在生产热路径上的真实开销：未变更时一次 os.Stat 与 mtime 比较即返回，这项代价必须与代际数和拓扑大小无关。
func BenchmarkOrgReloadHotPath(b *testing.B) {
	dir := b.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(b, yamlPath, ownerYAMLWithModel(b, "bench-model", []string{"sub1", "sub2"}, sub2MemDefault(b)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(b, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(b, err)
	defer func() { _ = entry.Close() }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.CheckOrgReload()
	}
}

// BenchmarkCandidateConstruction_RealOrg 度量真实组织形状（entry + 两个子 agent，
// 逐身份借用常驻 store）下「构造一个候选」的开销——热更中最重的一步，且它在发布
// 之前完成，故不阻塞在线 turn。
func BenchmarkCandidateConstruction_RealOrg(b *testing.B) {
	dir := b.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(b, yamlPath, ownerYAMLWithModel(b, "bench-model", []string{"sub1", "sub2"}, sub2MemDefault(b)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(b, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(b, err)
	defer func() { _ = entry.Close() }()

	cm := entry.ContextManager()
	face := cm.ExecutorConfig()

	var prev *agent.StagedGeneration
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if prev != nil {
			prev.Discard()
		}
		cand := cm.NewExecutorCandidate(face)
		if cand == nil {
			b.Fatal("candidate construction returned nil")
		}
		prev = cm.StageExecutor(cand, face, nil)
	}
	b.StopTimer()
	if prev != nil {
		prev.Discard()
	}
	if refs := cm.ExecutorRefs(); refs.PendingRetirees != 0 || refs.InFlightTurns != 0 {
		b.Fatalf("construction plus abandon must retire nothing and hold no refs: %+v", refs)
	}
}

// BenchmarkConfigRead_ProductionShape 只测读取阶段：解析生产形状的配置文件。读、编辑与构造三项成本必须分开测——合成一个数字无法指出该优化哪一项，因此循环内既不写文件也不触碰 mtime。
func BenchmarkConfigRead_ProductionShape(b *testing.B) {
	dir := b.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(b, yamlPath, prodShapeYAML(b, "bench-read"), &tick)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadConfig(yamlPath); err != nil {
			b.Fatal(err)
		}
	}
}

// prodShapeYAML renders that shape; `model` flips the org fingerprint so every
// reload really publishes.
func prodShapeYAML(t testing.TB, model string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("entry: orchestrator\nprompt_dir: resources/prompts\nmodel: " + model +
		"\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  orchestrator:\n    system_prompt:\n      inline: \"orchestrator\"\n    max_tool_iterations: 24\n    tools:\n")
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("worker%d", i)
		fmt.Fprintf(&b, "      - kind: agent\n        agent: %s\n        description: %q\n", name, name)
	}
	b.WriteString("      - kind: tool\n        id: recall\n        description: \"recall\"\n")
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("worker%d", i)
		fmt.Fprintf(&b, "  %s:\n    system_prompt:\n      inline: %q\n    keep_recent_tasks: 4\n    memory:\n      type: memory\n      path: %q\n",
			name, name, testStore(t, "prod-"+name))
	}
	return b.String()
}

// hotYAML renders the three-agent topology used by the hot-reload tests:
// entry references sub1+sub2 (kind: agent); each agent has its OWN isolated
// memory store so per-agent store identity is observable.
func hotYAML(t testing.TB, keep1, keep2 int, entryExtraTool bool) string {
	t.Helper()
	extra := ""
	if entryExtraTool {
		extra = fmt.Sprintf("      - kind: tool\n        id: recall\n        description: %q\n", "extra-"+fmt.Sprint(time.Now().UnixNano()))
	}
	keepLine := func(n int) string {
		if n <= 0 {
			return ""
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
%s`, extra, testStore(t, "hottest-sub1"), keepLine(keep1), testStore(t, "hottest-sub2"), keepLine(keep2))
}

// hotYAMLAddSub3 renders hotYAML plus a THIRD sub-agent, leaving every
// pre-existing agent's memory section byte-identical so the only delta is the
// topology add. The added agent gets its own localfile store, which is what makes
// “did it really acquire its own resource?” observable.
func hotYAMLAddSub3(t testing.TB, keep1, keep2 int) string {
	t.Helper()
	s := hotYAML(t, keep1, keep2, false)
	s = strings.Replace(s, `description: "sub2"`,
		"description: \"sub2\"\n      - kind: agent\n        agent: sub3\n        description: \"sub3\"", 1)
	return s + fmt.Sprintf("  sub3:\n    system_prompt:\n      inline: \"sub3\"\n    memory:\n      type: localfile\n      path: %q\n",
		testStore(t, "hottest-sub3"))
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

var _ model.Model = (*stubModel)(nil)

// dropAgentYAML renders the main+sub1+sub2 org with an explicit keep_recent_tasks
// per agent and the names in `drop` removed from BOTH the entry's tools and the
// agents map — a real removal, which is the draining-owner case.
func dropAgentYAML(t testing.TB, keeps map[string]int, drop ...string) string {
	t.Helper()
	dropped := map[string]bool{}
	for _, d := range drop {
		dropped[d] = true
	}
	var tools, agents strings.Builder
	for _, n := range []string{"sub1", "sub2"} {
		if dropped[n] {
			continue
		}
		fmt.Fprintf(&tools, "      - kind: agent\n        agent: %s\n        description: %q\n", n, n)
		fmt.Fprintf(&agents, "  %s:\n    system_prompt:\n      inline: %q\n    keep_recent_tasks: %d\n    memory:\n      type: localfile\n      path: %q\n",
			n, n, keeps[n], testStore(t, "hottest-drop-"+n))
	}
	return "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    keep_recent_tasks: " +
		fmt.Sprint(keeps["main"]) +
		fmt.Sprintf("\n    memory:\n      type: localfile\n      path: %q\n    tools:\n", testStore(t, "hottest-drop-main")) +
		tools.String() + agents.String()
}

const entryRenameStartupYAML = `entry: main
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
    memory:
      type: memory
`

// entryRenameChangedYAML 钉住入口改名场景：入口改为 entry2、旧 main 定义删除，entry2 自身有效并委派 sub1。
const entryRenameChangedYAML = `entry: entry2
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  entry2:
    system_prompt:
      inline: "entry2"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
`

// runBounded runs fn and fails the test if it does not return within d, so a gate
// that hangs instead of refusing is reported as a hang — never as an indefinite suite.
func runBounded(t *testing.T, what string, d time.Duration, fn func() error) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- fn() }()
	select {
	case err := <-errCh:
		return err
	case <-time.After(d):
		t.Fatalf("%s 在已收敛关闭后未返回——闸门必须拒绝，不能挂住", what)
		return nil
	}
}

// aliasYAML 生成带 compress 段的组织配置文本，用于对比旧式扁平键与规范 ModelRef 两种写法。
func aliasYAML(compressBlock string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n    system_prompt:\n      inline: \"P\"\n" +
		compressBlock +
		"    memory:\n      type: memory\n"
}

// writeCfg writes content and returns the config loaded through the REAL parse path
// (LoadConfig runs ApplyDefaults → FoldModelRefAliases), which is exactly what the
// reloader feeds to org.ComputeOrgFingerprint on a fresh check (tagent.go:693).
func writeCfg(t *testing.T, dir, name, content string) *Config {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	cfg, err := LoadConfig(p)
	require.NoError(t, err, "config %s must load", name)
	return cfg
}

// buildPark arms orgBuildBarrier so a background reload parks while holding the
// reload mutex, and hands the test a deterministic enter/release handshake.
type buildPark struct {
	entered  chan struct{}
	release  chan struct{}
	relOnce  sync.Once
	enterSig sync.Once
}

func newBuildPark() *buildPark {
	p := &buildPark{entered: make(chan struct{}), release: make(chan struct{})}
	h := func() {
		p.enterSig.Do(func() { close(p.entered) })
		<-p.release
	}
	orgBuildBarrier.Store(&h)
	return p
}

// waitEntered fails the test if no build actually reached the barrier (i.e. the
// build is genuinely parked under `mu`), so the bounded-acquire assertions below
// would otherwise measure nothing.
func (p *buildPark) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(10 * time.Second):
		p.letGo()
		t.Fatal("the candidate build never parked at the barrier — this run would measure nothing")
	}
}

func (p *buildPark) letGo() { p.relOnce.Do(func() { close(p.release) }) }

// disarm clears the global seam and, if the test bailed out early, frees any
// parked build so Close-style drains cannot hang the package. Register it after
// the Close cleanup: cleanups run LIFO, so the release must happen before the
// drain starts waiting on the mutex the parked build still holds.
func (p *buildPark) disarm() {
	orgBuildBarrier.Store(nil)
	p.letGo()
}

// acquireWithin runs a business-turn acquire on its own goroutine and fails if
// it does not return within `within`. A correct acquire touches only os.Stat and
// executorMu, never the reload mutex, so it must never be held by a parked build
// or a Close drain.
func acquireWithin(t *testing.T, ta *agent.TagentAgent, within time.Duration) (got any) {
	t.Helper()
	ch := make(chan any, 1)
	go func() {
		r, release := ta.ContextManager().BeginTurn()
		ch <- r
		release()
	}()
	select {
	case v := <-ch:
		return v
	case <-time.After(within):
		t.Fatalf("business acquire blocked >%s while a build/Close held the reload mutex — "+
			"the acquire path is NOT the short lock D3 requires (it must never touch `mu`)", within)
		return nil
	}
}

func genOf(ta *agent.TagentAgent) int64 {
	g, _ := ta.OrgDiagnostics()["generation"].(int64)
	return g
}

// scHotAddNumericYAML: entry main → sub1; sub3 block controlled by addSub3.
// B's max_tokens/keep_recent move between renders (numeric-only for sub3).
func scHotAddNumericYAML(t testing.TB, sub3 bool, sub3MaxTokens int, sub1Keep int) string {
	t.Helper()
	sub3Tool, sub3Block := "", ""
	if sub3 {
		sub3Tool = `      - kind: agent
        agent: sub3
        description: "sub3"
`
		sub3Block = fmt.Sprintf(`  sub3:
    system_prompt:
      inline: "sub3"
    max_tokens: %d
    memory:
      type: localfile
      path: %q
`, sub3MaxTokens, testStore(t, "hottest-sc3"))
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
%s  sub1:
    system_prompt:
      inline: "sub1"
    keep_recent_tasks: %d
    memory:
      type: localfile
      path: %q
%s`, sub3Tool, sub1Keep, testStore(t, "hottest-sc1"), sub3Block)
}

// scHot reads an owner's record-backed hot bundle (HotSnapshot resolves through
// the injected currentHotFor, i.e. the single committed application record; it
// falls back to the construction snapshot only with no record entry).
func scHot(t *testing.T, a *agent.TagentAgent) agent.OrgHotParams {
	t.Helper()
	require.NotNil(t, a, "owner 须在常驻表")
	p, ok := a.HotSnapshot()
	require.True(t, ok, "HotSnapshot 须可解析（记录源或构造快照）")
	return p
}

func snapshotRotationYAML(prompt string, subMax int, subThreshold float64) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"model: test-model\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: " + strconv.Quote(prompt) + "\n" +
		"    memory:\n      type: memory\n" +
		"    tools:\n      - kind: agent\n        agent: sub1\n        description: \"sub1\"\n" +
		"  sub1:\n" +
		"    system_prompt:\n      inline: \"sub1\"\n" +
		"    max_tokens: " + strconv.Itoa(subMax) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(subThreshold, 'f', -1, 64) + "\n" +
		"    keep_recent_tasks: 2\n" +
		"    memory:\n      type: memory\n"
}

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
