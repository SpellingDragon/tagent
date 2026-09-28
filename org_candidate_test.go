package tagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// introduce-durable-workflow-engine §2.1/§2.2 契约测：候选快照私有性与
// 组织指纹覆盖面。断言对象是「换代所用配置真的是新配置」与「执行配置变更
// 必然换代」，不是日志编号。

// populatedAgentConfig sets every AgentConfig field to a distinct non-zero
// value so the fingerprint subset can be audited field by field.
func populatedAgentConfig() AgentConfig {
	enable := true
	effort := "high"
	tokens := 4096
	return AgentConfig{
		Model: "m1", Provider: "p1", PromptDir: "pd",
		SystemPrompt:         PromptConfig{Inline: "sp"},
		Memory:               MemoryConfig{Type: "memory"},
		Tools:                []ToolRef{{Kind: ToolKindAgent, AgentID: "sub", Description: "d", EventParams: []string{"event_key"}, ExtraParams: []ExtraParam{{Name: "action", Type: "string"}}, Async: &enable}},
		MaxToolIterations:    7,
		MaxTokens:            8000,
		Temperature:          0.5,
		CompressThreshold:    0.7,
		KeepRecentTasks:      3,
		TaskTerminalTTL:      "2m",
		TaskDefaultTTL:       "10m",
		ResumeContextRounds:  2,
		Compress:             CompressConfig{CompactKeysListed: 1, RecentFullCount: 2, CardMaxChars: 300},
		ThinkingEnabled:      &enable,
		ThinkingTokens:       &tokens,
		ReasoningEffort:      &effort,
		ReasoningContentMode: "raw",
		Meditation:           MeditationConfig{Enabled: true},
		WorkspaceRoot:        "/ws",
		Description:          "desc",
	}
}

// fingerprintExcludedFields are the AgentConfig fields deliberately outside the
// org fingerprint, keyed by their **YAML/JSON name** (what the subset carries).
// Each entry states WHY; a new field is only acceptable here if it is
// hot-applicable through its own contract or needs a restart.
var fingerprintExcludedFields = map[string]string{
	"memory":             "storage paths/backends cannot migrate at runtime — restart (pinned by the reloader's changedMemoryAgents/residentMemFP pre-check)",
	"max_tokens":         "hot-applicable via ApplyOrgHotParams (compressor budget)",
	"compress_threshold": "hot-applicable via ApplyOrgHotParams (compressor threshold)",
	"keep_recent_tasks":  "hot-applicable via ApplyOrgHotParams",
	"task_terminal_ttl":  "hot-applicable via the TaskManager reaper setter",
	"task_default_ttl":   "hot-applicable via the TaskManager reaper setter",
}

func jsonFieldName(f reflect.StructField) string {
	tag := strings.Split(f.Tag.Get("json"), ",")[0]
	if tag == "" {
		return f.Name
	}
	return tag
}

// TestOrgFingerprint_AuditsEveryAgentConfigField 钉住 穷举式审计：每个配置字段要么折进指纹子集，要么在排除表里点名并写明理由。
// - 漏掉一个执行相关字段（工具开关、模型选择），该字段的变化就不会触发生成换代，这正是"配置改了而执行没变"的缺陷类；
// - 新增字段无需改动本用例即被要求归类，审计面随类型定义自动闭合。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestOrgFingerprint_AuditsEveryAgentConfigField(t *testing.T) {
	ac := populatedAgentConfig()
	raw, err := canonicalAgentSubset(&ac)
	require.NoError(t, err)
	inSubset := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(raw, &inSubset))

	acType := reflect.TypeOf(AgentConfig{})
	for i := 0; i < acType.NumField(); i++ {
		f := acType.Field(i)
		if !f.IsExported() {
			continue
		}
		name := jsonFieldName(f)
		if reason, excluded := fingerprintExcludedFields[name]; excluded {
			require.Falsef(t, inSubset[name] != nil,
				"field %q is declared excluded (%s) yet appears in the fingerprint subset — the tables disagree", name, reason)
			continue
		}
		require.Truef(t, inSubset[name] != nil,
			"field %q is neither in the fingerprint subset nor in fingerprintExcludedFields — a change to it would not force a new generation", name)
	}

	// Reverse drift: the subset must not carry keys AgentConfig no longer has
	// (a stale key would keep fingerprinting a dead field).
	for key := range inSubset {
		found := false
		for i := 0; i < acType.NumField(); i++ {
			if f := acType.Field(i); f.IsExported() && jsonFieldName(f) == key {
				found = true
				break
			}
		}
		require.Truef(t, found, "fingerprint subset has key %q with no matching AgentConfig field", key)
	}
}

// TestOrgFingerprint_CoversFullToolRef 钉住 工具引用的每个维度都是执行绑定，每一项都必须移动指纹。
// - 维度含内建标识、目标 agent、描述来源、事件与额外参数、异步门、工厂属性、远端端点；
// - 任一维度被标成不参与序列化，都会让真实的路由变化在指纹上隐身。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestOrgFingerprint_CoversFullToolRef(t *testing.T) {
	reflType := reflect.TypeOf(ToolRef{})
	for i := 0; i < reflType.NumField(); i++ {
		f := reflType.Field(i)
		if !f.IsExported() {
			continue
		}
		require.NotEqualf(t, "-", strings.Split(f.Tag.Get("json"), ",")[0],
			"ToolRef field %q is json:\"-\": it would never reach the org fingerprint", f.Name)
	}

	base := Config{Entry: "main", Agents: map[string]AgentConfig{
		"main": {Model: "m", Tools: []ToolRef{{Kind: ToolKindTool, ID: "exec", Properties: map[string]interface{}{"workspace": "/a"}}}},
	}}
	baseFP, err := computeOrgFingerprint(&base)
	require.NoError(t, err)

	// editMain replaces the (non-addressable) map value after mutating a copy.
	editMain := func(c *Config, mutate func(*AgentConfig)) {
		ac := c.Agents["main"]
		mutate(&ac)
		c.Agents["main"] = ac
	}
	mutations := []struct {
		name   string
		mutate func(*AgentConfig)
	}{
		{"tool id", func(a *AgentConfig) { a.Tools[0].ID = "read" }},
		{"tool kind", func(a *AgentConfig) { a.Tools[0].Kind = ToolKindAgent }},
		{"agent target", func(a *AgentConfig) { a.Tools[0].AgentID = "other" }},
		{"description", func(a *AgentConfig) { a.Tools[0].Description = "changed" }},
		{"description file", func(a *AgentConfig) { a.Tools[0].DescriptionFile = "d.md" }},
		{"event params", func(a *AgentConfig) { a.Tools[0].EventParams = []string{"event_key"} }},
		{"extra params", func(a *AgentConfig) { a.Tools[0].ExtraParams = []ExtraParam{{Name: "action"}} }},
		{"async gate", func(a *AgentConfig) { off := false; a.Tools[0].Async = &off }},
		{"factory props", func(a *AgentConfig) { a.Tools[0].Properties["workspace"] = "/b" }},
		{"last tool removed", func(a *AgentConfig) { a.Tools = nil }},
	}
	for _, m := range mutations {
		// Per-case deep copy through the same Clone the runtime uses: a shallow
		// struct copy would share Tools' backing array / Properties' map and let
		// one case mutate the base fixture.
		c2, cerr := base.Clone()
		require.NoError(t, cerr)
		editMain(c2, m.mutate)
		fp2, err := computeOrgFingerprint(c2)
		require.NoErrorf(t, err, "mutation %q", m.name)
		require.NotEqualf(t, baseFP, fp2,
			"mutation %q did not change the org fingerprint — the change would not force a new generation", m.name)
	}
}

// TestConfigClone_IsPrivateAndFingerprintNeutral 钉住 快照私有性：已发布的世代拥有自己那份配置。
// - 改动副本绝不触及原件，反之亦然；
// - 副本保持指纹中性——一次克隆不得看起来像是新版本。
// 契约: docs/wiki/platform/org-hot-reload.md#config-clone
func TestConfigClone_IsPrivateAndFingerprintNeutral(t *testing.T) {
	orig := populatedAgentConfig()
	cfg := Config{
		Entry: "main", ConfigPath: "/etc/tagent.yaml", Model: "m1", Provider: "p1",
		PromptDir: "pd",
		Agents:    map[string]AgentConfig{"main": orig, "sub": {Model: "m2"}},
		Providers: map[string]ProviderConfig{"p1": {Provider: "openai", APIEndpoint: "https://a"}},
	}
	fp, err := computeOrgFingerprint(&cfg)
	require.NoError(t, err)
	mc := cfg.Agents["main"]
	mfp := agentMemoryFingerprint(&mc)

	clone, err := cfg.Clone()
	require.NoError(t, err)
	require.NotNil(t, clone)

	clonedFP, err := computeOrgFingerprint(clone)
	require.NoError(t, err)
	require.Equal(t, fp, clonedFP, "clone must be fingerprint-neutral")
	cmc := clone.Agents["main"]
	clonedMemFP := agentMemoryFingerprint(&cmc)
	require.Equal(t, mfp, clonedMemFP, "clone must be memory-fingerprint-neutral")
	require.Equal(t, cfg.ConfigPath, clone.ConfigPath, "json:\"-\" field must survive the clone")

	// Mutate every aliased container the clone could have shared.
	clone.Agents["main"] = AgentConfig{Model: "mutated"}
	clone.Agents["newagent"] = AgentConfig{Model: "x"}
	delete(clone.Providers, "p1")
	clone.Providers["added"] = ProviderConfig{Provider: "anthropic"}
	subAC := clone.Agents["sub"]
	subAC.Tools = []ToolRef{{Kind: ToolKindTool, ID: "t"}}
	clone.Agents["sub"] = subAC

	require.Equal(t, orig.Model, cfg.Agents["main"].Model, "Agents map must not be shared")
	_, stillThere := cfg.Agents["newagent"]
	require.False(t, stillThere, "Agents map keys must not be shared")
	_, ok := cfg.Providers["p1"]
	require.True(t, ok, "Providers map must not be shared")
	_, ok = cfg.Providers["added"]
	require.False(t, ok, "Providers map keys must not be shared")
	require.Nil(t, cfg.Agents["sub"].Tools, "per-agent slices must not be shared")
	require.Equal(t, fp, mustFP(t, &cfg), "the original must be untouched by clone mutations")
}

func mustFP(t *testing.T, c *Config) string {
	t.Helper()
	fp, err := computeOrgFingerprint(c)
	require.NoError(t, err)
	return fp
}

// TestOrgCoordinator_SameContentAndPublishIdentity 钉住 分工：内容指纹决定重载是否改变东西，单调序号才是发布身份。
// - 回滚以新序号重新发布；
// - 被取代世代的内容绝不重新被视为当前。
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestOrgCoordinator_SameContentAndPublishIdentity(t *testing.T) {
	startup := &Config{Entry: "main", Agents: map[string]AgentConfig{"main": populatedAgentConfig()}}
	fpA := mustFP(t, startup)

	c := newOrgCoordinator()
	c.init(fpA, startup)
	require.Equal(t, fpA, c.current.fingerprint)
	require.True(t, c.sameAsCurrent(fpA), "startup content is current until a publish")
	require.False(t, c.sameAsCurrent("ffffffff"))
	require.Nil(t, c.rollbackSource(), "nothing has been published yet — no rollback source")

	// Structural change: swap reports the superseded fp and advances the sequence.
	fpB := "bbbb1111"
	oldFP, gen := c.swap(fpB, startup, nil)
	require.Equal(t, fpA, oldFP, "the log/alert line names the superseded fingerprint")
	require.Equal(t, 1, gen.seq, "first publish is generation 1")
	require.Equal(t, fpB, c.current.fingerprint)
	require.False(t, c.sameAsCurrent(fpA), "superseded content must not count as current")
	require.True(t, c.sameAsCurrent(fpB), "a same-content reload short-circuits here — no swap, no rebuild")

	// Rollback republishes the old CONTENT under a NEW sequence (D4: content hash
	// is not a publish identity) and leaves the ring pointing at the same source.
	rg := c.recordRollback(fpA, startup, nil)
	require.Equal(t, 2, rg.seq)
	require.Equal(t, fpA, c.current.fingerprint)
	require.NotNil(t, c.rollbackSource())
	require.Equal(t, fpA, c.rollbackSource().fingerprint)
	require.NotNil(t, rg.cfg, "a rollback stores the restored full config, not a nil alias")

	// Diagnostics: a rejection is visible until a publish succeeds.
	c.recordFailure(errSentinel{})
	require.EqualError(t, c.lastFailure(), "sentinel")
	_, gen3 := c.swap("cccc2222", startup, nil)
	require.Equal(t, 3, gen3.seq)
	require.Nil(t, c.lastFailure(), "a successful publish clears the rejection record")
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }

// R01／§2.3（design D3「事务式候选」）契约测。
//
// 缺陷：热增删里 `added`/`addedNames` 只在 `buildAgent` **成功返回后**由 seed 差集
// 填充，且 `rc.resident.Add` 在循环内每轮就发布。于是：
//   1. 父 agent 在 DFS 里递归建好依赖 Z（Z 已 registerStoreOwner + 持独有 store），
//      随后父自身步骤失败时 `return` 早于 seed 差集 → Z 不进回退清单 → 幽灵 owner；
//      失败父自身的 owner 登记也不被撤销（它不进 cache，外层无从枚举）。
//   2. 第一个 top 的 Add 已换入在线面，若后续 top 失败才 Unpublish → 期间并发读
//      可见未提交候选的新增者（半提交）。
//
// 本合同：一次被拒候选结束后，owner 归属必须与候选起点**逐名相等**（无净泄漏），
// 序号不前进，在线面不变。用「父成功依赖子 + 父自身在递归之后才失败」这一确定
// 时序触发：aaa_parent 引用 zzz_dep（合法、先建），随后 aaa_parent 因缺失的冥想
// 提示词文件在其依赖建成之后才失败。

const candTxnStartupYAML = `entry: main
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
        agent: keep
        description: "keep"
  keep:
    system_prompt:
      inline: "keep"
`

// main delegates to keep AND aaa_parent; aaa_parent delegates to the valid
// zzz_dep (built first via recursion) and THEN fails on a missing meditation
// prompt file — after its dependency is already resident-registered.
const candTxnRefusedYAML = `entry: main
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
        agent: keep
        description: "keep"
      - kind: agent
        agent: aaa_parent
        description: "p"
  keep:
    system_prompt:
      inline: "keep"
  aaa_parent:
    system_prompt:
      inline: "aaa_parent"
    tools:
      - kind: agent
        agent: zzz_dep
        description: "z"
    meditation:
      enabled: true
      prompt_file: "does-not-exist-r01-probe.md"
  zzz_dep:
    system_prompt:
      inline: "zzz_dep"
`

// TestOrgHotAdd_RefusedCandidateLeaksNoOwner 钉住 被拒候选不得遗留任何属主登记，包括失败的父与已成功的依赖。
// - 发布序号不前进，在线面不变；
// - 依赖项的登记同样必须在回退时撤销，否则它以孤儿身份继续占用属主。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestOrgHotAdd_RefusedCandidateLeaksNoOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(candTxnStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)

	owners0 := ta.StoreOwnerSnapshot()
	require.NotNil(t, owners0, "owner probe must be wired")
	require.Contains(t, owners0, "main")
	require.Contains(t, owners0, "keep")
	require.NotContains(t, owners0, "zzz_dep", "precondition: dep not resident at startup")
	require.NotContains(t, owners0, "aaa_parent")
	gen0 := ta.OrgDiagnostics()["generation"]

	// Trigger the refused candidate (aaa_parent fails after zzz_dep is built).
	write(candTxnRefusedYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "a refused candidate never publishes")
	require.NotNil(t, st["lastFailure"], "and the refusal is diagnosable")

	after := ta.StoreOwnerSnapshot()
	// Nothing the candidate registered may survive it — the recursively-built
	// dependency Z and the late-failed parent must both be revoked, leaving owner
	// attribution exactly as at the candidate start.
	require.Equal(t, owners0, after,
		"a refused candidate must revoke EVERY owner it registered (recursive deps + failed parent): no orphan, no leak")
	require.NotContains(t, after, "zzz_dep", "the recursively-built dependency's owner must be revoked")
	require.NotContains(t, after, "aaa_parent", "the late-failed parent's owner must be revoked")

	// The live topology is untouched (no half commit).
	require.NotContains(t, residentCacheForTest(ta), "zzz_dep")
	require.NotContains(t, residentCacheForTest(ta), "aaa_parent")
	require.Equal(t, []string{"keep"}, entryToolNames(ta), "the still-effective generation keeps routing")
}

// TestOrgHotAdd_LegalSharedDependencyBuildsOnce 钉住 跨多个新增顶层项的公共依赖只构建一次。
// - 候选缓存共享，全部成功随单次提交一起发布；
// - 不得出现第二个 writer。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestOrgHotAdd_LegalSharedDependencyBuildsOnce(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(candTxnStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)
	gen0 := ta.OrgDiagnostics()["generation"]

	// Two new parents sharing one dep, all valid → must hot-add all three once.
	legalYAML := `entry: main
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
        agent: p1
        description: "1"
      - kind: agent
        agent: p2
        description: "2"
  p1:
    system_prompt:
      inline: "p1"
    tools:
      - kind: agent
        agent: shared_dep
        description: "s"
  p2:
    system_prompt:
      inline: "p2"
    tools:
      - kind: agent
        agent: shared_dep
        description: "s"
  shared_dep:
    system_prompt:
      inline: "shared_dep"
`
	write(legalYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Greater(t, st["generation"], gen0, "the legal add publishes")
	require.Nil(t, st["lastFailure"], "no refusal for a legitimate add")
	table := residentCacheForTest(ta)
	require.NotNil(t, table["p1"])
	require.NotNil(t, table["p2"])
	require.NotNil(t, table["shared_dep"], "the shared dep is resident exactly once")
	owners := ta.StoreOwnerSnapshot()
	require.Contains(t, owners, "shared_dep")
	require.Contains(t, owners, "p1")
	require.Contains(t, owners, "p2")
	// The dep behind both parents is one and the same instance — a second writer
	// would have built a distinct agent for shared_dep under the second parent.
	require.Equal(t, []string{"p1", "p2"}, entryToolNames(ta))
}

// txnYAML renders the S-B fixture: entry main → sub1; hot-add cases inject
// extra referenced agents. failZzz points zzz_probe's memory at /dev/null so
// its store creation fails DETERMINISTICALLY mid-candidate (after aaa_probe,
// which sorts first, has been built successfully) — the refused-candidate
// cleanup path then has TWO acquired responsibilities to unwind.
func txnYAML(t testing.TB, mainModel string, addProbes, failZzz bool) string {
	t.Helper()
	probes := ""
	if addProbes {
		zzzPath := fmt.Sprintf("%q", testStore(t, "hottest-zzz"))
		if failZzz {
			zzzPath = `"/dev/null/zzz-probe-cannot-create"`
		}
		probes = fmt.Sprintf(`      - kind: agent
        agent: aaa_probe
        description: "aaa"
      - kind: agent
        agent: zzz_probe
        description: "zzz"
  aaa_probe:
    system_prompt:
      inline: "aaa"
    memory:
      type: localfile
      path: %q
  zzz_probe:
    system_prompt:
      inline: "zzz"
    memory:
      type: localfile
      path: %s
`, testStore(t, "hottest-aaa"), zzzPath)
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
%s  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
`, mainModel, probes, testStore(t, "hottest-sub1"))
}

// TestTxn_RefusedCandidateDiscardsInReverseAcquisitionOrder 钉住 撤销必须按获取的逆序展开。
// - 候选按确定次序取得责任（名称升序），其后某一步失败时，最后取得的先退、先前建立的后进；
// - 按映射遍历的差异回滚保证不了这一序，只有有序责任表把它变成契约；
// - 观察点是一个仅供测试使用的撤销次序探针。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestTxn_RefusedCandidateDiscardsInReverseAcquisitionOrder(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	write(txnYAML(t, "model-a", false, false))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err, "LoadConfig baseline")

	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err, "New")
	t.Cleanup(func() { _ = entry.Close() })

	write(txnYAML(t, "model-a", true, true)) // hot-add aaa_probe + fail zzz_probe
	entry.CheckOrgReload()

	order := orgLastDiscardOrder()
	require.NotEmpty(t, order,
		"refused candidate must record its discard order (probe) — cleanup ran without the responsibility table")
	// aaa_probe 是唯一完整建成的 owner（zzz 在 store 创建处失败，仅登记）；逆序
	// 要求 zzz 的部分登记先于 aaa 的完整 Close 撤销。
	require.Equal(t, []string{"zzz_probe", "aaa_probe"}, order,
		"S-B: discard must unwind in REVERSE acquisition order (zzz partial first, then aaa)")
}

// TestTxn_RefusedCandidateLeavesNoOwnerOrTableResidue 钉住 同一条泄漏契约也必须经责任表这条路成立。
// - 撤销必须建立在表上而不是差异上，改路不改语义；
// - 属主与表项都不得留下残迹。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestTxn_RefusedCandidateLeavesNoOwnerOrTableResidue(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	write(txnYAML(t, "model-a", false, false))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	before := entry.StoreOwnerSnapshot()
	write(txnYAML(t, "model-a", true, true))
	entry.CheckOrgReload()

	after := entry.StoreOwnerSnapshot()
	require.Equal(t, before, after,
		"refused candidate must revoke every owner registration it made")
}

// orgLastDiscardOrder moved to its only consumer (2026-09-27 dead-code audit):
// TEST-only introspection of the candidate transaction order.
// orgLastDiscardOrder returns the most recent candidate-discard order (TEST
// introspection only; nil before the first discard).
func orgLastDiscardOrder() []string {
	if v, ok := lastDiscardOrder.Load().([]string); ok {
		return v
	}
	return nil
}

// 回滚与正向热更共用同一条候选通路：私有候选 overlay → 唯一提交点 commit → 其后任何
// 失败都由 defer 的 abandon 按获取逆序整体回退，所以后段失败不会半改在线拓扑。三行验收各钉一测：
//
//	(d) 回滚后段失败不半改：未发布的 owner 不得留在在线清册，其存储租约不得被持有
//	(b) 热增 → 数值更新 → 真实在途调用 → 回滚：宿主结果与寿命都取该 agent 自己的消费者
//	(c) 移除父项保留共享子项后回滚：共享子仍恰好一个 owner（二次获取会失败关闭）

const (
	g24Tool   = "g24_flaky_tool" // unique: RegisterPlainTool panics on a duplicate id
	g24Leaf   = "g24_leaf"
	g24Mid    = "g24_mid"
	g24B      = "g24_b"
	g24P1     = "g24_p1"
	g24P2     = "g24_p2"
	g24Shared = "g24_shared"
)

// stageGate is the injected REAL failure: a plain-tool factory that cannot serve
// past a call budget. Nothing in the product path knows about it.
//
// The registration is process-global and duplicate ids panic (§0 口径②), so the
// gate is a package-level singleton armed once and RESET per run — which keeps
// this file usable under a `-count>1` repetition gate instead of needing an
// exemption.
type stageGate struct {
	limit atomic.Int64
	calls atomic.Int64
}

var (
	g24GateOnce sync.Once
	g24Flaky    = &stageGate{}
)

func armStageGate(t *testing.T) *stageGate {
	t.Helper()
	g24GateOnce.Do(func() {
		agent.RegisterPlainTool(g24Tool, g24Flaky.tool)
	})
	g24Flaky.calls.Store(0)
	g24Flaky.limit.Store(1 << 40)
	return g24Flaky
}

func (g *stageGate) tool(_ agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
	if g.calls.Add(1) > g.limit.Load() {
		return nil, errors.New("injected: tool factory cannot serve")
	}
	return &mockCallableTool{name: g24Tool}, nil
}

func writeGateConfig(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// leafYAML: main → mid → leaf. leaf declares the flaky tool and owns its own
// localfile store, so a leaked owner is observable twice over: in the resident
// table, and as a still-held writer lock. withLeaf=false drops leaf from mid's
// routing (structural: the routing shape is fingerprinted).
func leafYAML(withLeaf bool, leafStore string) string {
	midTail := "    tools: []\n"
	leafDef := ""
	if withLeaf {
		midTail = fmt.Sprintf("    tools:\n      - kind: agent\n        agent: %s\n        description: delegate-leaf\n", g24Leaf)
		leafDef = fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"LEAF\"\n    memory:\n      type: localfile\n      path: %q\n    tools:\n      - kind: tool\n        id: %s\n", g24Leaf, leafStore, g24Tool)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-mid
  %s:
    system_prompt:
      inline: "MID"
    memory:
      type: memory
%s%s`, g24Mid, g24Mid, midTail, leafDef)
}

// TestLateStageFailureLeavesNoOwnerPublished 钉住 最末一环（装配新面）失败时，被拒的回滚不得留下属主发布或租约。
// - 场景次序：常驻叶子 → 结构移除并发布（空闲退役）→ 回滚恢复其路由（须重新取回）→ 同一工具的末环装配失败；
// - 拒绝之后在线拓扑必须与原先完全一致：当前世代不变、叶子不常驻、其存储 writer 槽归还；
// - 预算取自实测的冷启动计数，因此度量的是回滚自身的次序，而不是猜出的常量。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestLateStageFailureLeavesNoOwnerPublished(t *testing.T) {
	gate := armStageGate(t) // cold start succeeds unconditionally; the budget is set below

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-g24-leaf")
	tick := writeGateConfig(t, yamlPath, leafYAML(true, store), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	leafBefore := residentCacheForTest(entry)[g24Leaf]
	require.NotNil(t, leafBefore, "precondition: leaf is resident on the startup generation")

	// Let exactly ONE more tool construction succeed — the rollback's re-acquisition —
	// and fail the face build that follows it.
	coldCalls := gate.calls.Load()
	gate.limit.Store(coldCalls + 1)

	// Structural removal: leaf becomes unrouted and retires.
	tick = writeGateConfig(t, yamlPath, leafYAML(false, store), tick)
	entry.CheckOrgReload()
	genAfterRemoval := diagInt64(t, entry.OrgDiagnostics(), "generation")
	waitFor(t, "the unrouted leaf retired", func() bool {
		return residentCacheForTest(entry)[g24Leaf] == nil
	})
	// The removal itself rebuilt no leaf tools; note the call count so the
	// rollback's first tool construction is the re-acquisition.
	gate.limit.Store(gate.calls.Load() + 1)

	// Roll back to the generation routing leaf: re-acquisition succeeds, the face
	// build fails — the last stage before publishing.
	entry.Rollback()

	require.Equal(t, genAfterRemoval, diagInt64(t, entry.OrgDiagnostics(), "generation"),
		"a refused rollback must not publish")
	require.Nil(t, residentCacheForTest(entry)[g24Leaf],
		"后段失败的回滚不得把未发布的 owner 留在在线清册里")
	midNow := residentCacheForTest(entry)[g24Mid]
	require.NotNil(t, midNow)
	require.Nil(t, midNow.ContextManager().SubagentWrapper(g24Leaf),
		"and the serving face must still not route it")
	require.NoError(t, takeOverStore(t, store),
		"§2.4(d)：被拒候选为 leaf 取的 store 租约必须随回退归还，否则该路径永久占住写者名额")
}

// ownerBYAML routes main→b when withB, with b's OWN hot numerics; changing those
// numerics is numeric-only (fingerprint-excluded), changing the routing is not.
func ownerBYAML(withB bool, keep int, terminal string) string {
	mainTail := "    tools: []\n"
	bDef := ""
	if withB {
		mainTail = fmt.Sprintf("    tools:\n      - kind: agent\n        agent: %s\n        description: delegate-b\n        async: false\n", g24B)
		bDef = fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"SUB-B\"\n    memory:\n      type: memory\n    keep_recent_tasks: %d\n    task_terminal_ttl: %q\n", g24B, keep, terminal)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
%s%s`, mainTail, bDef)
}

// TestRollbackOfHotAddNumericWithInFlightTurn 钉住 热加之后接纯数值更新再回滚时，在途调用必须跑完。
// - 断言只读该 agent 自己的真实消费者与宿主可见答复，不读指纹；
// - 回滚环里的来源值必须回来，且它自始至终只有一个属主；
// - 已在途的那次调用不得被回滚重发布拆掉。
// 契约: docs/wiki/platform/org-hot-reload.md#rollback
func TestRollbackOfHotAddNumericWithInFlightTurn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeGateConfig(t, yamlPath, ownerBYAML(false, 2, "1m"), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "g24-b-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	// (1) hot-add B → structural publish.
	tick = writeGateConfig(t, yamlPath, ownerBYAML(true, 2, "1m"), tick)
	entry.CheckOrgReload()
	ownerB := residentCacheForTest(entry)[g24B]
	require.NotNil(t, ownerB, "B became a resident owner")

	// (2) numeric-only update on B (structure identical).
	tick = writeGateConfig(t, yamlPath, ownerBYAML(true, 7, "5m"), tick)
	entry.CheckOrgReload()
	require.Equal(t, 7, ownerB.OrgKeepRecent(), "B's own compressor consumer took the update")
	require.Equal(t, 5*time.Minute, ownerB.TaskManager().TerminalTTL(), "B's own manager took the update")

	// (3) a real delegation across the rollback: park B mid-call, roll back,
	// release; the pinned call must still be served.
	bGate := make(chan struct{})
	m.armGate("SUB-B", bGate)
	t.Cleanup(func() { disarmGate(bGate) })
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("in-flight")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "B parked mid-call", func() bool {
		for _, s := range m.snapshot() {
			if s.System == "SUB-B" {
				return true
			}
		}
		return false
	})

	entry.Rollback() // publishes a new generation while B's call is in flight
	servedBefore := countServed(m.snapshot(), "SUB-B")
	disarmGate(bGate)
	waitFor(t, "the in-flight B call completed", func() bool {
		return countServed(m.snapshot(), "SUB-B") > servedBefore
	})

	// (4) the rollback restored the ring source through the same record: B's OWN
	// consumers moved back, and B is still exactly one owner instance.
	require.Equal(t, 2, ownerB.OrgKeepRecent(), "§2.4(b)：回滚把 B 自身的 keepRecent 恢复到环源值")
	require.Equal(t, time.Minute, ownerB.TaskManager().TerminalTTL(), "§2.4(b)：回滚把 B 自身的 terminal TTL 恢复到环源值")
	require.Same(t, ownerB, residentCacheForTest(entry)[g24B],
		"§2.4(b)：回滚推进执行面，不另造第二个 B owner（单一 owner＝实例与 store 身份不动）")

	// (5) and a FRESH call after the rollback is served too (the new generation is
	// live, not a half-published face).
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("after")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a post-rollback call is served", func() bool {
		return countServed(m.snapshot(), "SUB-B") > servedBefore+1
	})
}

// diamondYAML routes main→{p1,p2} (or only p2), both to the SAME shared child
// which owns its own localfile store.
func diamondYAML(withP1 bool, sharedStore string) string {
	mainTools := fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: d-p2\n", g24P2)
	if withP1 {
		mainTools = fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: d-p1\n%s", g24P1, mainTools)
	}
	routes := func(parent string) string {
		return fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"P-%s\"\n    memory:\n      type: memory\n    tools:\n      - kind: agent\n        agent: %s\n        description: shared-child\n", parent, parent, g24Shared)
	}
	p1 := ""
	if withP1 {
		p1 = routes(g24P1)
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
%s%s%s  %s:
    system_prompt:
      inline: "SHARED"
    memory:
      type: localfile
      path: %q
`, mainTools, p1, routes(g24P2), g24Shared, sharedStore)
}

// TestRemovedParentRollbackKeepsSharedChildSingleOwner 钉住 移除一个父项而共享子项仍被另一方路由时，回滚只重新取得那个父项。
// - 子项沿用唯一既存属主；再取一次其存储会因单写者失败关闭，所以回滚确实生效本身就是见证；
// - 子项的属主身份必须保持不变。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestRemovedParentRollbackKeepsSharedChildSingleOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-g24-shared")
	tick := writeGateConfig(t, yamlPath, diamondYAML(true, store), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	shared := residentCacheForTest(entry)[g24Shared]
	require.NotNil(t, shared, "precondition: the shared child is resident")

	tick = writeGateConfig(t, yamlPath, diamondYAML(false, store), tick)
	entry.CheckOrgReload()
	genRemoved := diagInt64(t, entry.OrgDiagnostics(), "generation")
	waitFor(t, "the removed parent retired", func() bool {
		return residentCacheForTest(entry)[g24P1] == nil
	})
	require.Same(t, shared, residentCacheForTest(entry)[g24Shared],
		"the surviving route keeps the SAME child owner (still declared by p2's face)")

	entry.Rollback()

	require.Greater(t, diagInt64(t, entry.OrgDiagnostics(), "generation"), genRemoved,
		"§2.4(c)：回滚须真正发布新代（若它二次获取了共享子的 store，单写者门会把它 fail-closed 在此）")
	require.NotNil(t, residentCacheForTest(entry)[g24P1], "the parent came back")
	require.Same(t, shared, residentCacheForTest(entry)[g24Shared],
		"§2.4(c)：共享子仍是同一个 owner，不因回滚被再造/重取")
}

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

func diagInt64(t *testing.T, d map[string]any, k string) int64 {
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

// TestFullConfigAndRollback 钉住 针对真实构造的 agent 驱动完整的配置与回滚契约。
// - 纯数值应用必须轮转回滚环、推进修订号与应用时间，却不推进结构世代；
// - 语义完全相同的应用不轮转；
// - 回滚发布新世代，同时恢复结构与五项热参；被拒候选两轴都不动；
// - 每个取值断言都读真实消费者（压缩预算与保留数、任务管理器寿命），绝不读常驻配置的复读。
// 契约: docs/wiki/platform/org-hot-reload.md#rollback
func TestFullConfigAndRollback(t *testing.T) {
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
	require.EqualValues(t, 0, diagInt64(t, d, "generation"))
	require.EqualValues(t, 0, diagInt64(t, d, "revision"))
	_, hasPub := diagTime(t, d, "lastPublishedAt")
	require.False(t, hasPub, "startup is not a structural publish")

	// (1) First STRUCTURAL update (prompt A→B): generation 1, revision 1, a
	// structural publish time appears, hot consumers unchanged (only prompt moved).
	write(l3YAML("B", 2, 4000, 0.5, "1m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "structural change advances the generation")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"), "a structural publish is also a full apply")
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
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "numeric-only must NOT bump the structural generation")
	require.EqualValues(t, 2, diagInt64(t, d, "revision"), "numeric-only is a full apply → bumps revision")
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
	require.EqualValues(t, 2, diagInt64(t, d, "revision"), "semantically identical apply must not rotate/advance (D9)")
	require.EqualValues(t, 1, diagInt64(t, d, "generation"))

	// (4) ROLLBACK restores the PRE-NUMERIC full config: keep/max/terminal revert
	// (this is what numeric-only ring rotation bought), and it publishes a NEW
	// generation + advances lastPublishedAt.
	revBeforeRollback := diagInt64(t, d, "revision")
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, diagInt64(t, d, "generation"), "rollback republishes as a new generation")
	require.EqualValues(t, revBeforeRollback+1, diagInt64(t, d, "revision"))
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
	require.EqualValues(t, 2, diagInt64(t, d, "generation"), "a rollback to identical content must not bump the generation")
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
	require.EqualValues(t, 3, diagInt64(t, d, "generation"), "rollback after a real change publishes a new generation")
	require.Equal(t, 2, entry.OrgKeepRecent(), "the fresh numeric edit is rolled back to the ring source")
}

// TestRollbackHookSurvivesNumericOnlyFirstUpdate 钉住 回滚钩子必须在装载器装配时装好一次，与哪个分支先触发无关。
// - 否则首个更新是纯数值的组织会有轮转过的回滚环却没有钩子，回滚静默成空操作；
// - 场景为启动 → 纯数值 → 回滚恢复启动期的热值并发布新世代，断言取真实消费者而非取值器。
// 契约: docs/wiki/platform/org-hot-reload.md#rollback
func TestRollbackHookSurvivesNumericOnlyFirstUpdate(t *testing.T) {
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
	require.EqualValues(t, 0, diagInt64(t, d, "generation"), "numeric-only must not bump the generation")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"), "numeric-only is a full apply")
	require.Equal(t, 7, entry.OrgKeepRecent())
	require.Equal(t, 4500, entry.OrgBudgetLine())

	// Rollback with NO structural publish ever must still restore the startup
	// values through the real consumers and publish a new generation. Before
	// the fix this was a silent no-op (hook never installed): keep stays 7.
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "rollback publishes a new generation")
	require.EqualValues(t, 2, diagInt64(t, d, "revision"))
	require.Equal(t, 2, entry.OrgKeepRecent(), "rollback restores the startup keepRecent")
	require.Equal(t, 2000, entry.OrgBudgetLine(), "rollback restores the startup budget")
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL(), "rollback restores the startup terminal TTL")

	// The ring walked forward: a second rollback faces identical content and is
	// a safe no-op (values stay at the restored startup config).
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "a rollback to identical content must not spin a new generation")
	require.Equal(t, 2, entry.OrgKeepRecent())
}

// TestRejectedCandidateKeepsBothAxes 钉住 纯数值应用之后遇到坏配置时，世代、修订号、应用时间与真实消费者都停在原值。
// - 不得半替换：一轴动了而另一轴没动。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestRejectedCandidateKeepsBothAxes(t *testing.T) {
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
	genB, revB := diagInt64(t, before, "generation"), diagInt64(t, before, "revision")
	appliedB, _ := diagTime(t, before, "lastAppliedAt")

	write("entry: [this is not valid yaml")
	entry.CheckOrgReload()
	after := entry.OrgDiagnostics()
	require.EqualValues(t, genB, diagInt64(t, after, "generation"), "a rejection must not advance the generation")
	require.EqualValues(t, revB, diagInt64(t, after, "revision"), "a rejection must not advance the revision")
	appliedA, _ := diagTime(t, after, "lastAppliedAt")
	require.True(t, appliedA.Equal(appliedB), "a rejection must not move lastAppliedAt")
	require.NotNil(t, after["lastFailure"], "the rejection reason must be observable")
	// Real consumers unchanged (no half-swap).
	require.Equal(t, 6, entry.OrgKeepRecent())
	require.Equal(t, 4800, entry.OrgBudgetLine())
}

// TestCoordinatorHotApplyRevision 钉住 直接钉住协调器状态机，不经 agent。
// - 语义相同的热应用不轮转也不推进，真实的热应用两者都推进；
// - 任何一次热应用都不得触碰结构世代与发布时间。
// 契约: docs/wiki/platform/org-hot-reload.md#identical-apply
func TestCoordinatorHotApplyRevision(t *testing.T) {
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

// sdCloseYAML routes `routed` from main, giving every routed agent its OWN
// localfile store so the final lock state is observable from outside the org.
func sdCloseYAML(t testing.TB, routed []string, storeOf func(name string) string) string {
	t.Helper()
	var tools string
	for _, r := range routed {
		tools += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r)
	}
	defs := ""
	for _, r := range routed {
		defs += fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: localfile\n      path: %q\n", r, r, storeOf(r))
	}
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n    tools:\n" + tools + defs
}

// assertStoreWriterFree proves the owner's store lease was ACTUALLY handed back:
// the single-writer lock on that path can be taken exclusively by this test.
// Releasing the lease is the resource-exit event §4.3/D8 demands on Close —
// "之后恰一次释放组件、store lease 和登记，无需下个用户请求" — and a lock still
// held would mean either a leak or a still-live writer.
func assertStoreWriterFree(t *testing.T, path string) {
	t.Helper()
	probe, err := os.OpenFile(filepath.Join(canonicalize(path), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer probe.Close()
	require.NoError(t, flockExclusive(probe),
		"关闭后 %s 的写锁必须已归还（恰一次释放，不待下一个用户请求）", path)
}

// TestOrgClose_CoversCandidatePublishedDuringDrain 钉住 最终关闭必须覆盖所有属主、候选与共享资源。
// - 属主关闭器登记在停止重载之后：仍在进行中的构建可能在快照之后又新增一个属主，从而永远躲过清扫；
// - 场景是把一次构建停在重载互斥之内，启动关闭，再放它发布；
// - 每个属主，含排空期间落位的这一个，都必须被恰好关闭一次，其存储租约真正归还。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_CoversCandidatePublishedDuringDrain(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	storeOf := func(name string) string { return filepath.Join(dir, "store-"+name) }
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdCloseYAML(t, []string{"sub1"}, storeOf))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	// Belt for the failure path: a parked build must never survive a bail-out,
	// or the deferred Close would hang the package (the round-76 lesson).
	park := newBuildPark()
	defer park.disarm()
	t.Cleanup(func() { _ = entry.Close() })

	// Structural hot-add; the next business acquire schedules the build, which
	// parks while holding `mu`.
	write(sdCloseYAML(t, []string{"sub1", "sub2"}, storeOf))
	_ = acquireWithin(t, entry, 2*time.Second)
	park.waitEntered(t)

	closed := make(chan error, 1)
	go func() { closed <- entry.Close() }()
	time.Sleep(50 * time.Millisecond) // let Close reach its bounded drain
	park.letGo()

	select {
	case err := <-closed:
		require.NoError(t, err, "org Close must converge on its own")
	case <-time.After(25 * time.Second):
		t.Fatal("org Close never finished with a candidate in flight — the drain or the sweep is unbounded")
	}

	owners := residentCacheForTest(entry)
	require.Contains(t, owners, "sub2",
		"precondition: the drained candidate really published during the Close sequence")
	for _, name := range []string{"sub1", "sub2"} {
		o := owners[name]
		require.NotNilf(t, o, "owner %q must be listed for the sweep", name)
		require.Truef(t, o.CloseStarted(), "owner %q escaped the org sweep — it was never closed", name)
	}
	assertStoreWriterFree(t, storeOf("sub1"))
	assertStoreWriterFree(t, storeOf("sub2"))
}

// §4.3（D7）子树热增删的常驻所有权契约。委派行为面（新代的声明与实际调用目标随
// 发布改变）由 TestOrgDelegation_TargetFollowsPublishedGenerationNotMutableGlobals
// 覆盖；本文件只管**所有权**：谁持有 store、移除后谁保留、同名重入复用谁、被拒
// 候选的新增是否回退。
//
// 断言用实例/store 身份（require.Same / NotSame），因为「owner 有没有被复制或替换」
// 就是这些指针本身；行为侧的真实路由证据已由委派测给出。

// ownerYAML renders entry "main" delegating to `targets`（sub1/sub2 始终被定义，
// 所以移除只改变可达性，不改变配置里存在什么）。sub2 的 memory 段作为参数，便于
// 渲染“同名重入且存储不变”与“重入但存储变了”两种候选。
func ownerYAML(t testing.TB, targets []string, sub2Mem string) string {
	t.Helper()
	var toolLines string
	for _, t := range targets {
		toolLines += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", t, t)
	}
	head := "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    tools:\n"
	defs := fmt.Sprintf(`  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
      path: %q
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
`, testStore(t, "own-sub1"))
	return head + toolLines + defs + sub2Mem + "\n"
}

// 每个 agent 一个独立 store 实例，才有可观察的身份。路径经 testStore 挪出工作树并
// 按用例隔离——早先此处注释声称“type: memory 就不落盘”，是错的：resources.acquire 在
// 按 type 分派之前无条件 MkdirAll + 取目录写锁，相对路径会在仓库根造出目录。
func sub2MemDefault(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-sub2"))
}

func sub2MemMoved(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-sub2-moved"))
}

// sub2MemInMemory carries no path, so it needs no per-case store root.
const sub2MemInMemory = "      type: memory\n"

// ownerWriter bumps mtime deterministically: FS granularity can otherwise
// swallow a rapid rewrite and silently skip the reload.
func ownerWriter(t *testing.T, yamlPath string) func(string) {
	t.Helper()
	tick := time.Now()
	return func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
}

func buildOwnerAgent(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ta.Close() })
	return ta
}

func entryToolNames(ta *agent.TagentAgent) []string {
	face := ta.ContextManager().ExecutorConfig()
	var out []string
	for _, tl := range face.Tools {
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// TestOrgHotRemove_KeepsOwnerButStopsRouting 钉住 移除只摘除新代的可路由集合与工具声明，原属主保留、绝不提前退役。
// - 这是旧代执行、后台任务与已接受输入仍可访问其存储的前提；
// - 也是同名重入能复用原属主的前提。
func TestOrgHotRemove_KeepsOwnerButStopsRouting(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	ta := buildOwnerAgent(t, yamlPath)

	table := residentCacheForTest(ta)
	require.NotNil(t, table["sub2"], "sub2 is resident at startup")
	sub2Instance, sub2Store := table["sub2"], table["sub2"].MemStore()
	require.NotSame(t, table["main"].MemStore(), sub2Store, "sub2 owns its own store")
	// §4.3 migrated this case: an unrouted owner is kept while it is STILL NEEDED and
	// retired once it is not. The claim under test ("unrouted, not retired early") is
	// about the drain window, so the window now has a real reference in it — taken on
	// the same accounting the reclaim gate reads.
	drainRef := sub2Instance.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer drainRef.Release()

	// 移除 sub2 的可路由性（其定义仍在配置里——可达集合才是拓扑真源）。
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta.CheckOrgReload()

	after := residentCacheForTest(ta)
	require.Same(t, sub2Instance, after["sub2"],
		"a removed agent keeps its resident owner — it is unrouted, not retired")
	require.Same(t, sub2Store, after["sub2"].MemStore(), "and keeps the same store, not a replacement")
	require.NotContains(t, entryToolNames(ta), "sub2",
		"the published generation must not offer a removed agent as a tool")
	require.Contains(t, entryToolNames(ta), "sub1", "the surviving target still routes")
}

// TestOrgHotAdd_ReentryReusesOriginalOwnerUnlessStorageChanged 钉住 同名重入复用原存储属主，不产生第二个 writer。
// - 重入时若存储段发生变化，必须拒绝该候选。
func TestOrgHotAdd_ReentryReusesOriginalOwnerUnlessStorageChanged(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	ta := buildOwnerAgent(t, yamlPath)
	orig := residentCacheForTest(ta)["sub2"]
	genAtStart := ta.OrgDiagnostics()["generation"]
	// §4.3 migrated both cases below: they assert "same-name re-entry reuses the
	// ORIGINAL owner" / "the refusal keeps the original owner in place", which is the
	// semantics WHILE the owner is still live. An idle removed owner is now retired
	// (TestRetire_*), so this test holds one reference to stay in its own subject.
	heldOut := orig.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer heldOut.Release()

	// 1) remove, then re-add with the SAME storage section → the original owner is
	// reused: the same instance, the same store, and NOT a freshly built second one.
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta.CheckOrgReload()
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.Same(t, orig, table["sub2"], "a same-name re-entry reuses the original resident owner")
	require.Same(t, orig.MemStore(), table["sub2"].MemStore(), "…and its original store (no second writer)")
	require.Contains(t, entryToolNames(ta), "sub2", "…and is routable again")
	require.Greater(t, ta.OrgDiagnostics()["generation"], genAtStart, "the re-add is a real publish")

	// 2) re-entry with a CHANGED storage section → refused: the candidate would put
	// a different store behind a name whose original owner still holds the old one.
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta.CheckOrgReload()
	before := ta.OrgDiagnostics()["generation"]
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemMoved(t)))
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, before, st["generation"], "a storage-changing re-entry never publishes")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, fail.Error, "memory section changed")
	require.Contains(t, fail.Error, "sub2")
	require.Same(t, orig, residentCacheForTest(ta)["sub2"],
		"the refusal keeps the ORIGINAL owner in place — the moved store was never adopted")
	require.NotContains(t, entryToolNames(ta), "sub2", "and the refused candidate routes nowhere")

	// 3) the refusal is sticky: re-checking the same un-effective storage change while
	// editing an unrelated field still refuses (no bypass through a second check).
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemMoved(t)))
	ta.CheckOrgReload()
	require.Equal(t, before, ta.OrgDiagnostics()["generation"], "the refusal does not lapse into acceptance")
}

// TestOrgHotAdd_NewAgentMayCarryItsOwnMemorySection 钉住 新增 agent 自带存储段属声明内容，不是运行时存储迁移。
// - 它不得被存储先序检查误拒，否则热新增永不可达。
func TestOrgHotAdd_NewAgentMayCarryItsOwnMemorySection(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta := buildOwnerAgent(t, yamlPath)
	require.NotContains(t, residentCacheForTest(ta), "sub2")

	// sub2 comes back only as a NEW name relative to the reachable topology, with a
	// memory section no existing owner holds.
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemInMemory))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.NotNil(t, table["sub2"], "a new agent with its own memory section must hot-add")
	require.NotSame(t, table["main"].MemStore(), table["sub2"].MemStore(), "on its own store")
	require.Nil(t, ta.OrgDiagnostics()["lastFailure"], "no refusal for a legitimate add")
	require.Contains(t, entryToolNames(ta), "sub2")
}

// TestOrgHotAdd_UnroutedDefinitionChangeMustNotFreezeReload 钉住 第三态：定义仍在 agents 里、却已从工具链摘除的 agent 改掉了存储路径。
// - 它不进本代构造，没有第二 writer 要防，因此不得冻结整条热更路；
// - 它日后重入时仍须被拒绝，粘性未被削薄。
func TestOrgHotAdd_UnroutedDefinitionChangeMustNotFreezeReload(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	sub2Store := residentCacheForTest(entry)["sub2"].MemStore()
	require.NotSame(t, entry.MemStore(), sub2Store, "precondition: sub2 owns its own store")
	gen0 := entry.OrgDiagnostics()["generation"].(int64)

	// ① 摘 sub2 路由 + 同时改它的路径：必须发布（不冻结）。
	write(ownerYAML(t, []string{"sub1"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	require.Equal(t, gen0+1, entry.OrgDiagnostics()["generation"].(int64),
		"a storage change on an UNROUTABLE-but-still-defined agent must not freeze orchestration hot-reload")
	require.Nil(t, entry.OrgDiagnostics()["lastFailure"], "and nothing was refused")

	// ② 一次与此无关的编排变更仍要生效（这才是 H-1 修前被永久冻住的形态）。
	write(ownerYAMLWithModel(t, "test-model-x", []string{"sub1"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	require.Equal(t, gen0+2, entry.OrgDiagnostics()["generation"].(int64),
		"later orchestration edits must still apply")

	// ③ sub2 重入且存储与 owner 基准不符 → 仍拒（粘性），且原 owner/store 不被换掉。
	write(ownerYAMLWithModel(t, "test-model-x", []string{"sub1", "sub2"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	st := entry.OrgDiagnostics()
	require.Equal(t, gen0+2, st["generation"].(int64),
		"a re-entering name whose storage differs from its owner's baseline must still be refused")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "and the refusal must be diagnosable")
	require.Contains(t, fail.Error, "sub2")
	// §4.3 migrated (and strengthened): by this point sub2 has been unrouted long
	// enough to be retired, so "the original owner is still installed" is no longer
	// the claim. What must survive is D7's real guarantee — the storage baseline is
	// data (residentMemFP), so the re-entry is STILL refused, and the refused
	// candidate must not have built any owner behind the name.
	require.Nil(t, residentCacheForTest(entry)["sub2"],
		"the original owner was retired while unrouted — no instance is held for a name nothing needs")
	require.NotContains(t, entryToolNames(entry), "sub2",
		"and the refused candidate neither adopted the moved store nor routed the name")
}

// TestRelaunch_TargetResolvesAgainstPublishedGeneration 钉住 显式重投的解析源必须跟着已发布代走。
// - 启动代认得它；热移除其路由之后便认不出它，否则重投会静默复活已退役的绑定；
// - 同名重入之后重新认得。
func TestRelaunch_TargetResolvesAgainstPublishedGeneration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	cm := entry.ContextManager()
	require.NotNil(t, cm.SubagentWrapper("sub1"), "the startup generation routes to sub1")

	// 摘掉 sub1 的路由（定义保留——正是 §4.2 与 H-1 交界的形状）。
	write(ownerYAML(t, []string{"sub2"}, sub2MemDefault(t)))
	entry.CheckOrgReload()
	require.Nil(t, cm.SubagentWrapper("sub1"),
		"after a hot removal an explicit relaunch must NOT resolve sub1 — the retired binding may not be revived")
	require.NotNil(t, cm.SubagentWrapper("sub2"), "the retained target keeps resolving (the face is not emptied by the removal)")

	// 同名重入（存储未变）→ 必须重新可解析，且解析到的仍是**原 owner** 的绑定。
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	entry.CheckOrgReload()
	require.NotNil(t, cm.SubagentWrapper("sub1"), "re-entry makes it routable again")
	require.Equal(t, int64(0), cm.ExecutorRefs().InFlightTurns, "sanity: a reload publishes a generation but pins no in-flight turn")
}

// §3.4「热增后真实数据归属——各给宿主返回与资源尾部证据」。
//
// 此前的热增证点全部停在**身份**上（`require.NotSame` 的两台 store），从未证明
// 「热增 agent 的真实读写确实落在它自己的存储里」——身份不同不等于数据落对地方：
// 一次把子 agent 回合写进宿主 store 的实现，在身份断言下照样全绿。本测把这条
// 数据流走到底：真实委派 → 宿主拿到真实返回 → 子回合记录只出现在**子自己的**
// store（宿主 store 里没有它）→ 真落盘字节可寻 → Close 后该数据的主人释放其
// store 注册（资源尾部）。
//
// 判据刻意不用「宿主 store 完全找不到该文本」：委派返回本就以 action_command
// 记录进宿主回合，那是正确行为而非泄漏。区分归属的是**记录种类**——子 agent
// 自己回合的 agent_output 只能落在子自己的分区里。

const (
	hotAddReq  = "HOTADD-REQ-98"
	hotAddAns  = "HOTADD-ANS-98"
	hotAddMark = "main-final-98"
)

// hotAddDataYAML renders entry "main" routing `targets`, with sub1/sub2 always
// DEFINED (so publishing sub2 is a hot ADD of a routed owner, not a new
// definition) and sub2 on its own localfile store. Deliberately NO model/providers
// section: resolveAgentModel's documented order 2 hands an agent that declares no
// model to the host-injected instance — that is what lets one mock serve main and
// sub2 and makes the delegation observable at the model boundary.
func hotAddDataYAML(t *testing.T, targets []string) string {
	t.Helper()
	var toolLines string
	for _, name := range targets {
		toolLines += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n        async: false\n", name, name)
	}
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    max_tool_iterations: 2\n    memory:\n      type: memory\n      path: " + fmt.Sprintf("%q\n", testStore(t, "hotadd-data-main")) +
		"    tools:\n" + toolLines +
		fmt.Sprintf(`  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
      path: %q
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
`, testStore(t, "hotadd-data-sub1"), testStore(t, "hotadd-data-sub2"))
}

// hotAddDataModel drives a REAL delegation: the entry's first call asks for the
// hot-added sub-agent with a payload only that sub-agent can echo, and the
// sub-agent answers with a distinctive marker. Every request is captured so the
// host's view of the returned value is read off the wire, not off an internal
// field.
type hotAddDataModel struct {
	mu       sync.Mutex
	requests []*model.Request
}

func (m *hotAddDataModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func (m *hotAddDataModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	m.mu.Lock()
	m.requests = append(m.requests, req)
	mainCalls := 0
	for _, r := range m.requests {
		if len(r.Messages) > 0 && strings.HasPrefix(r.Messages[0].Content, "main") {
			mainCalls++
		}
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	switch {
	case strings.HasPrefix(system, "sub2"):
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage(hotAddAns)}}}
	case strings.HasPrefix(system, "main") && mainCalls == 1:
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-sub2", Function: model.FunctionDefinitionParam{
				Name: "sub2", Arguments: []byte(`{"request":"` + hotAddReq + `"}`)}}},
		}}}}
	default:
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage(hotAddMark)}}}
	}
	close(ch)
	return ch, nil
}

func (m *hotAddDataModel) Info() model.Info { return model.Info{Name: "hotadd-data-model"} }

// storeFacts lists the events in `store` whose stored content matches `keyword`,
// as "type|content" read back off the record itself — attribution is judged on
// what the store actually holds, not on a summary the framework handed to the
// caller. Limit is explicit: QueryEvents returns nothing without one.
// The partition axis is the ownership axis: context_manager derives it from the
// agent NAME (`PartitionIDFromName(cfg.Name)`), and an unpartitioned query
// deliberately scans NOTHING (the isolation contract both store implementations
// share) — so reading back attribution must name the partition it belongs to.
func storeFacts(t *testing.T, store memory.MemoryStore, partitionName, keyword string) []string {
	t.Helper()
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName(partitionName)},
		Keyword:      keyword,
		Limit:        50,
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	var out []string
	for _, ref := range refs {
		full, err := store.GetEvent(ref.EventKey)
		if err != nil {
			continue
		}
		out = append(out, ref.EventType+"|"+full.Content)
	}
	return out
}

func countFacts(facts []string, eventType, substr string) int {
	n := 0
	for _, f := range facts {
		parts := strings.SplitN(f, "|", 2)
		if parts[0] == eventType && strings.Contains(parts[1], substr) {
			n++
		}
	}
	return n
}

// TestHotAddDataLandsInItsOwnStoreWithHostReturn 钉住 经热路径加入的属主，其数据落在自己的存储段，并回到正确的宿主。
func TestHotAddDataLandsInItsOwnStoreWithHostReturn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	// Cold: sub2 is DEFINED but unrouted, so it has no owner yet.
	write(hotAddDataYAML(t, []string{"sub1"}))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &hotAddDataModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }() // idempotent; the explicit Close below is the one under test

	require.Nil(t, residentCacheForTest(entry)["sub2"], "precondition: sub2 has no owner before it is routed")

	// Hot-add: route sub2 — its owner is built on the hot path with its own store.
	write(hotAddDataYAML(t, []string{"sub1", "sub2"}))
	entry.CheckOrgReload()
	sub2 := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, sub2, "precondition: sub2 became a resident owner through the hot path")
	require.NotSame(t, entry.MemStore(), sub2.MemStore(), "and it holds its own store")

	out, err := entry.StartLoop("u", "hotadd-data-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start the hot-added work"))
	require.NoError(t, err)

	// (1) 宿主返回：the host turn really received the sub-agent's payload.
	waitFor(t, "the hot-added owner served the delegation and the host got its answer", func() bool {
		for _, req := range m.snapshot() {
			for _, got := range toolResultsOf(req) {
				if strings.Contains(got, hotAddAns) {
					return true
				}
			}
		}
		return false
	})

	// (2) 真实落位：the sub-agent's OWN turn records live in the sub-agent's store.
	// Event write-back is asynchronous, so the read is bounded-wait, not immediate.
	var sub2Facts []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sub2Facts = storeFacts(t, sub2.MemStore(), "sub2", hotAddAns)
		if countFacts(sub2Facts, "agent_output", hotAddAns) >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	entryFacts := storeFacts(t, entry.MemStore(), "main", hotAddAns)
	require.Empty(t, storeFacts(t, sub2.MemStore(), "main", hotAddAns),
		"非空洞性自检：同一台 store 换一个主人的分区就什么都看不见——上面的命中确由归属轴给出，不是「查什么都有」")
	require.GreaterOrEqual(t, countFacts(sub2Facts, "agent_output", hotAddAns), 1,
		"§3.4：热增 owner 自己回合的产出必须落在它自己的 store 里（实测其记录集：%v）", sub2Facts)

	// (3) 不串宿主：the host's store must not carry the sub-agent's own turn record.
	// It legitimately carries the returned value as an action_command — that is the
	// delegation result, not leakage; what must NOT appear there is a second owner's
	// agent_output.
	require.Zero(t, countFacts(entryFacts, "agent_output", hotAddAns),
		"the hot-added owner's turn record must not be written into the host's store")

	// (3b) 跨 owner 不可见：the host's store must not even hold a record under the
	// sub-agent's partition — data does not migrate between owners' namespaces.
	require.Empty(t, storeFacts(t, entry.MemStore(), "sub2", hotAddAns),
		"the host store must carry no record under the hot-added owner's partition")

	// (4) 真落盘：localfile means bytes on disk, so the attribution is physical.
	storeDir := testStore(t, "hotadd-data-sub2")
	waitFor(t, "the owner's data really persisted to its own store directory", func() bool {
		return dirContains(t, storeDir, hotAddAns)
	})

	// (5) 资源尾部：the owner that held this data releases its store registration on
	// the organization's Close.
	require.Contains(t, entry.StoreOwnerSnapshot(), "sub2", "precondition: the hot-added owner registered its store")
	require.NoError(t, entry.Close())
	require.NotContains(t, entry.StoreOwnerSnapshot(), "sub2",
		"the owner holding this data must release its store registration when the org closes")
	require.True(t, sub2.CloseStarted(), "and its own close sequence really ran")
}

// dirContains reports whether any file under root carries the marker bytes.
func dirContains(t *testing.T, root, marker string) bool {
	t.Helper()
	var found bool
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || found {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr == nil && strings.Contains(string(b), marker) {
			found = true
		}
		return nil
	})
	return found
}

// 轮九十一（evidence §5.47）：② 工厂公开合同一次迁移的失败契约。
//
// 轮九十把发布改为「每个可达 owner 都 stage→wire→activate」，而旧合同的工厂产物是
// **整只 agent**（装配中段不产配置，cfg 为 nil），于是暴露出两类工厂 owner 特有缺陷
// ——这两条测在合同迁移前实测为红：
//
//	D-f1 每次结构发布，为工厂 owner 装配 face 时都会**再构造一个整只
//	     TagentAgent**（旧合同要求工厂自己 NewTagentAgent），它只被用来抄
//	     ExecutorConfig，随即成为无人 Close 的孤儿（bus/TaskManager/runner 全
//	     新建）。违背 D1「修改 B 不复制它的 bus/TaskManager」与 2.3 准出。
//	D-f2 staged 代的 runCfg 对工厂 owner 恒为 nil ⇒ 被钉的委派调用回退
//	     `*ta.config`（**构造期**配置），工厂声明变了也永不到达——正是
//	     「不按陈旧 ta.config 先造后补」在工厂分支的未兑现半。
//
// 合同迁移（工厂返回 *TagentConfig，构造归唯一 wireAgent 路径）后，两条都必须绿。

// factoryTrunkYAML routes `leaf` from main with async:false (a sync delegation:
// the witness is the call itself, not a settle-driven extra turn) and makes the
// leaf's OWN max_tool_iterations the varied field — it is fingerprinted, so
// changing it publishes a new generation, and it also reaches the factory via
// ToolAgentFactoryConfig, which is how the declaration changes.
func factoryTrunkYAML(leaf string, iters int) string {
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-leaf
        async: false
  %s:
    system_prompt:
      inline: "DECLARED-IGNORED"
    memory:
      type: memory
    max_tool_iterations: %d
`, leaf, leaf, iters)
}

func writeFactoryTrunk(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// TestFactoryReloadConstructsNoOrphanAgents 钉住 触到工厂所属 agent 的结构发布必须构造零个 agent。
// - 装配若走整件产品式的工厂，就会为取一份执行配置而建出无人关闭的整个 agent；
// - 工厂交付声明时，同一发布走所有属主共用的那条面路径推进，且不构造任何东西。
func TestFactoryReloadConstructsNoOrphanAgents(t *testing.T) {
	const leaf = "g33_orphan_leaf" // unique: RegisterToolAgent panics on a duplicate id
	agent.RegisterToolAgent(leaf, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		// The migrated contract: a declaration, never a constructed agent.
		return &agent.TagentConfig{
			Name:        leaf,
			Model:       fc.Model,
			MemoryStore: fc.MemoryStore,
		}, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 2), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	require.NotNil(t, residentCacheForTest(entry)[leaf], "precondition: the factory owner is resident")

	before := agent.TagentAgentsConstructed()
	tick = writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 3), tick)
	entry.CheckOrgReload()

	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"D-f1：工厂 owner 的结构发布不得整只再造一个 agent——它只被用来抄 face，随后成为无人 Close 的孤儿（D1「修改 B 不复制它的 bus/TaskManager」）")
}

// TestFactoryConfigChangeReachesDelegations 钉住 工厂声明发生变化的发布之后，新的委派调用必须运行新声明。
// - 包装器经已声明代自己的执行视图解析目标；工厂属主的这份视图必须带着新的装配配置；
// - 视图缺配置时，声明式调用会退回构造期配置，旧提示词被永久服务。
func TestFactoryConfigChangeReachesDelegations(t *testing.T) {
	const leaf = "g33_stale_leaf"
	agent.RegisterToolAgent(leaf, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		// The factory's declaration is keyed to the iteration budget it is handed,
		// so a structural publish that changes it changes the factory OUTPUT too.
		return &agent.TagentConfig{
			Name:         leaf,
			Model:        fc.Model,
			MemoryStore:  fc.MemoryStore,
			SystemPrompt: fmt.Sprintf("FACTORY-%d", fc.MaxToolIterations),
		}, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 2), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	// Close via DEFER (runs before t.Cleanup on a FailNow): if an assertion below
	// fails mid-loop, StopLoop still happens first and the drain below cannot hang.
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "factory-trunk-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first"))
	require.NoError(t, err)
	waitFor(t, "the factory leaf served on G1", func() bool {
		return countServed(m.snapshot(), "FACTORY-2") >= 1
	})

	tick = writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 3), tick)
	entry.CheckOrgReload() // publish G2: the factory declaration changed with it

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second"))
	require.NoError(t, err)
	waitFor(t, "a fresh delegation serves the new factory declaration", func() bool {
		return countServed(m.snapshot(), "FACTORY-3") >= 1
	})
}

// 3.3 工厂前置能力门（evidence §5.45，合同迁移 §5.47）：ToolAgentFactory 在组织
// 装配里只有一处生产消费点（build_agent.go 的 `registry.GetToolAgentFactory(name)`）。
// 旧合同下其产物**整只返回**、跳过 wireAgent 尾段——轮八十九据此测出 store 租约永久
// 泄漏并当场修复；轮九十一经用户裁决把合同一次迁移为「工厂交配置、组织负责构造」，
// 该泄漏类从此**结构上不可能**（租约走 TagentConfig.MemStoreRelease 同一退出槽）。
// 本文件因此双职：特征测继续钉住**迁移后也不得改变**的承诺（内置名保护、菱形单调用、
// 失败包装形状、工厂自持声明不被 Tools 构建覆盖），并把租约规则留成回归臂——
// 规则＝「store 只由 RuntimeResources 退出」，任何构造分支都不得留下一张没人接手、
// 也无法归还的租约。

// factoryLeafYAML routes `leafName` from main and gives it its OWN localfile store, so the
// writer lock on that path is an outside-observable witness of whether the lease came back.
func factoryLeafYAML(leafName, storePath string) string {
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-leaf
        async: false
  %s:
    system_prompt:
      inline: "LEAF"
    memory:
      type: localfile
      path: %q
`, leafName, leafName, storePath)
}

// takeOverStore asks a FRESH registry to open the path: success proves the previous holder
// handed the writer slot back exactly once; ErrStoreLocked means a lease leaked, and
// ErrResourcePoisoned means it was released uncleanly.
func takeOverStore(t *testing.T, path string) error {
	t.Helper()
	fresh := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, rel, err := fresh.acquire("localfile", path, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	if err == nil {
		require.NoError(t, rel())
	}
	return err
}

// TestFactoryConfigBuiltReleasesItsStoreLease 钉住 对照组：按配置建出的属主在组织关闭时交还自己的租约。
// - 缺这条对照，工厂那一侧即便从不归还，也可能因测试装置看不见租约而通过。
func TestFactoryConfigBuiltReleasesItsStoreLease(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-config-built")
	require.NoError(t, os.WriteFile(yamlPath, []byte(factoryLeafYAML("g33_ctrl_leaf", store)), 0o644))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)

	require.NotNil(t, residentCacheForTest(entry)["g33_ctrl_leaf"], "precondition: the leaf is a resident owner")
	require.NoError(t, entry.Close())
	require.NoError(t, takeOverStore(t, store), "config-built owner: 关闭后租约必须恰一次归还")
}

// TestFactoryBuiltReleasesItsStoreLease 钉住 同一契约的工厂侧：为该名字获取的租约经与配置路径同一个出口归还。
// - 出口只在最后一步交出，绝不在未收敛时交；
// - 可观察的见证是叶子自身路径上的 writer 锁：工厂分支不得从旁门再次取得属主责任。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestFactoryBuiltReleasesItsStoreLease(t *testing.T) {
	const leafName = "g33_fact_leaf" // unique: RegisterToolAgent panics on a duplicate id
	agent.RegisterToolAgent(leafName, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		// What real factories do now: describe the agent from what the config gives them.
		return &agent.TagentConfig{
			Name:         leafName,
			Model:        fc.Model,
			MemoryStore:  fc.MemoryStore,
			SystemPrompt: "factory-built",
		}, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-factory-built")
	require.NoError(t, os.WriteFile(yamlPath, []byte(factoryLeafYAML(leafName, store)), 0o644))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)

	leaf := residentCacheForTest(entry)[leafName]
	require.NotNil(t, leaf, "precondition: the factory owner is a resident owner too")
	require.Equal(t, leafName, leaf.Info().Name, "precondition: the factory's declared identity is adopted verbatim")
	require.NoError(t, entry.Close())
	require.NoError(t, takeOverStore(t, store),
		"3.3 工厂门：工厂分支取了 store 租约就必须负责归还，否则该路径永久泄漏写者名额")
}

// TestFactoryCommittedBehavior 钉住 这道门不得静默改变的既定行为。
// - 内建名保护、对产品原样使用、这一分支上声明的工具表被忽略、菱形记忆化（两个父项只调一次工厂）；
// - 以及热更失败关闭路径所匹配的错误形状。
func TestFactoryCommittedBehavior(t *testing.T) {
	rc := &runtimeConfig{model: &stubModel{name: "m"}}
	loader := prompt.NewLoader("")

	t.Run("product is used verbatim and its declared tools are not consulted", func(t *testing.T) {
		const name = "g33_verbatim"
		calls := 0
		agent.RegisterToolAgent(name, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
			calls++
			return &agent.TagentConfig{Name: name, Model: fc.Model}, nil
		})
		cfg := Config{Agents: map[string]AgentConfig{
			name: {
				SystemPrompt: PromptConfig{Inline: "declared prompt"},
				Memory:       MemoryConfig{Type: "memory"},
				// A tool id that does not exist: the config-driven branch would fail here,
				// the factory branch never looks at it.
				Tools: []ToolRef{{Kind: ToolKindTool, ID: "no-such-tool-id-g33"}},
			},
		}}
		cache := map[string]*agent.TagentAgent{}
		ta, err := buildAgent(name, cfg.Agents[name], cfg, rc, loader, cache, buildModeResident)
		require.NoError(t, err, "工厂分支不构建声明的 Tools（既有承诺）")
		require.Same(t, cache[name], ta, "产物即缓存值")
		require.Equal(t, 1, calls)
	})

	t.Run("two parents share one factory call", func(t *testing.T) {
		const name = "g33_shared"
		calls := 0
		agent.RegisterToolAgent(name, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
			calls++
			return &agent.TagentConfig{Name: name, Model: fc.Model}, nil
		})
		parent := func(kids ...ToolRef) AgentConfig {
			return AgentConfig{SystemPrompt: PromptConfig{Inline: "p"}, Memory: MemoryConfig{Type: "memory"}, Tools: kids}
		}
		kid := ToolRef{Kind: ToolKindAgent, AgentID: name, Description: "d"}
		cfg := Config{Agents: map[string]AgentConfig{
			name: {SystemPrompt: PromptConfig{Inline: "leaf"}, Memory: MemoryConfig{Type: "memory"}},
			"p1": parent(kid),
			"p2": parent(kid),
		}}
		cache := map[string]*agent.TagentAgent{}
		for _, pn := range []string{"p1", "p2"} {
			_, err := buildAgent(pn, cfg.Agents[pn], cfg, rc, loader, cache, buildModeResident)
			require.NoError(t, err)
		}
		require.Equal(t, 1, calls, "同一缓存内菱形只允许构建一次")
	})

	t.Run("factory failure keeps the wrapped error shape", func(t *testing.T) {
		const name = "g33_failing"
		agent.RegisterToolAgent(name, func(agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
			return nil, fmt.Errorf("boom")
		})
		cfg := Config{Agents: map[string]AgentConfig{
			name: {SystemPrompt: PromptConfig{Inline: "x"}, Memory: MemoryConfig{Type: "memory"}},
		}}
		_, err := buildAgent(name, cfg.Agents[name], cfg, rc, loader, map[string]*agent.TagentAgent{}, buildModeResident)
		require.ErrorContains(t, err, fmt.Sprintf("agent %q: factory failed:", name),
			"错误形状是热更 fail-closed 的匹配面，改它等于改拒绝语义")
		require.ErrorContains(t, err, "boom", "工厂原始原因必须保留")
	})
}

// C1: the R4 executor-shell rebuild must never
// re-register recovery retention on the SHARED durable store. A durable bus
// build arms (ArmRetentionFromInbox) and a mem_spill attach arms
// (ProtectAllPending) the un-acked originals held under the resident owner's
// lease. The shell's own artifacts are discarded (its bus is never acked) and
// releaseRetention only runs on the resident bus's Ack path — so any holder the
// shell adds is a permanent lease leak, and an Arm-failure leg keeps a BeginHold
// with no matching EndHold (forgetting barrier hangs forever). The gate
// `!mode.isExecutorShell()` on agentCfg.BusSpillDir / ets.SetMemSpill restores
// recovery registration to resident-owner-only.

// seedUnackedEnvelope writes one claimed+prepared but un-acked durable envelope
// into the inbox rooted at spillDir (spillDir/inbox-v2), whose prepared fact key
// is factKey and reserved receipt key is receiptKey. A later
// NewReliableEventBus(spillDir)+ArmRetentionFromInbox enumerates it and protects
// exactly those keys — mirroring the §2.8 restart-recovery owner setup.
func seedUnackedEnvelope(t *testing.T, spillDir string, factKey, receiptKey int64) {
	t.Helper()
	in, err := reliability.NewInbox(spillDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "leak-probe", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e-leak","type":"external_input","message":{"role":"user","content":"x"}}`)}},
	})
	require.NoError(t, err)
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + strconv.FormatInt(factKey, 10) + `}`)}))
	require.NoError(t, in.Close())
}

func TestResidentShellBuild_DoesNotDoubleArmSharedLease(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	busRoot := filepath.Join(root, "bus")
	spillRoot := filepath.Join(root, "spill")

	mc := MemoryConfig{Type: "localfile", Path: storeDir}
	// Pre-acquire the shared durable store so the test holds the SAME
	// *FileSegmentStore (and its §2.8 retention lease) that the resident build
	// will later borrow via the path registry — the lease is created in-process
	// by buildSharedResource and is not reachable after the fact except through
	// the shared instance.
	rawStore, _, preRelease, err := defaultResources.acquire("localfile", storeDir, fingerprintMemory(mc), func() (openedResource, error) {
		return openLocalFileStore(mc)
	})
	require.NoError(t, err)
	defer func() { _ = preRelease() }()
	fss, ok := rawStore.(*memory.FileSegmentStore)
	require.True(t, ok, "localfile shared store must be a *memory.FileSegmentStore")
	lease := fss.RetentionLease()
	require.NotNil(t, lease, "buildSharedResource must wire the retention lease")

	// One overdue un-acked envelope in the entry's durable inbox.
	pid := memory.PartitionIDFromName("tagent")
	now := time.Now().UnixMilli()
	factKey := memory.NewSnowflakeEventKey(pid, now-10*24*3600*1000) // overdue fact original
	receiptKey := memory.NewSnowflakeEventKey(pid, now)
	seedUnackedEnvelope(t, filepath.Join(busRoot, "tagent"), factKey, receiptKey)

	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "resident"},
				Memory:       mc,
			},
		},
		Reliability: ReliabilityConfig{
			BusSpillDir:        busRoot,
			MemSpillDir:        spillRoot,
			DegradationEnabled: true,
		},
	}
	rc := &runtimeConfig{model: &factoryMockModel{}}
	loader := prompt.NewLoader("")

	// 1) Resident cold-start build: the durable bus arms recovery retention on
	//    the shared store's lease — exactly one holder per material key.
	resident, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeResident)
	require.NoError(t, err)
	rc.entryMemStore = resident.MemStore()
	rc.entrySessionSvc = resident.SessionSvc()
	require.Equal(t, 1, lease.Holders(factKey), "resident arm protects the fact original")
	require.Equal(t, 1, lease.Holders(receiptKey), "resident arm protects the receipt original")

	// 2) Hot-reload executor-shell rebuild (structural change → new generation).
	//    The shell borrows the resident store; with the C1 gate it builds a
	//    volatile bus (no BusSpillDir) and no spill, so it adds NO holder.
	gen2 := cfg.Agents["tagent"]
	gen2.SystemPrompt = PromptConfig{Inline: "gen2"}
	_, err = buildAgent("tagent", gen2, cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeExecutorShell)
	require.NoError(t, err)

	// fail-before: removing the `!mode.isExecutorShell()` gate on BusSpillDir /
	// SetMemSpill makes the shell re-open the SAME durable inbox and re-arm the
	// SAME keys → these become 2 (permanent leak, forgetting never resumes).
	// With the gate: unchanged.
	require.Equal(t, 1, lease.Holders(factKey), "executor shell must add NO holder to the shared fact lease (C1)")
	require.Equal(t, 1, lease.Holders(receiptKey), "executor shell must add NO holder to the shared receipt lease (C1)")
}
