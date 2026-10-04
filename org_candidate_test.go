// org_candidate_test 覆盖候选事务与回滚：拒绝退场、指纹排除字段、关闭排空与同义应用不轮转。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
package tagent

import (
	"github.com/SpellingDragon/tagent/agent/resources"

	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/config"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

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
	mfp := config.AgentMemoryFingerprint(&mc)

	clone, err := cfg.Clone()
	require.NoError(t, err)
	require.NotNil(t, clone)

	clonedFP, err := computeOrgFingerprint(clone)
	require.NoError(t, err)
	require.Equal(t, fp, clonedFP, "clone must be fingerprint-neutral")
	cmc := clone.Agents["main"]
	clonedMemFP := config.AgentMemoryFingerprint(&cmc)
	require.Equal(t, mfp, clonedMemFP, "clone must be memory-fingerprint-neutral")
	require.Equal(t, cfg.ConfigPath, clone.ConfigPath, "json:\"-\" field must survive the clone")

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

	fpB := "bbbb1111"
	oldFP, gen := c.swap(fpB, startup, nil)
	require.Equal(t, fpA, oldFP, "the log/alert line names the superseded fingerprint")
	require.Equal(t, 1, gen.seq, "first publish is generation 1")
	require.Equal(t, fpB, c.current.fingerprint)
	require.False(t, c.sameAsCurrent(fpA), "superseded content must not count as current")
	require.True(t, c.sameAsCurrent(fpB), "a same-content reload short-circuits here — no swap, no rebuild")

	rg := c.recordRollback(fpA, startup, nil)
	require.Equal(t, 2, rg.seq)
	require.Equal(t, fpA, c.current.fingerprint)
	require.NotNil(t, c.rollbackSource())
	require.Equal(t, fpA, c.rollbackSource().fingerprint)
	require.NotNil(t, rg.cfg, "a rollback stores the restored full config, not a nil alias")

	c.recordFailure(errSentinel{})
	require.EqualError(t, c.lastFailure(), "sentinel")
	_, gen3 := c.swap("cccc2222", startup, nil)
	require.Equal(t, 3, gen3.seq)
	require.Nil(t, c.lastFailure(), "a successful publish clears the rejection record")
}

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

	write(candTxnRefusedYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "a refused candidate never publishes")
	require.NotNil(t, st["lastFailure"], "and the refusal is diagnosable")

	after := ta.StoreOwnerSnapshot()
	require.Equal(t, owners0, after,
		"a refused candidate must revoke EVERY owner it registered (recursive deps + failed parent): no orphan, no leak")
	require.NotContains(t, after, "zzz_dep", "the recursively-built dependency's owner must be revoked")
	require.NotContains(t, after, "aaa_parent", "the late-failed parent's owner must be revoked")

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
	require.Equal(t, []string{"p1", "p2"}, entryToolNames(ta))
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

	write(txnYAML(t, "model-a", true, true))
	entry.CheckOrgReload()

	order := orgLastDiscardOrder()
	require.NotEmpty(t, order,
		"refused candidate must record its discard order (probe) — cleanup ran without the responsibility table")
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

// TestLateStageFailureLeavesNoOwnerPublished 钉住 最末一环（装配新面）失败时，被拒的回滚不得留下属主发布或租约。
// - 场景次序：常驻叶子 → 结构移除并发布（空闲退役）→ 回滚恢复其路由（须重新取回）→ 同一工具的末环装配失败；
// - 拒绝之后在线拓扑必须与原先完全一致：当前世代不变、叶子不常驻、其存储 writer 槽归还；
// - 预算取自实测的冷启动计数，因此度量的是回滚自身的次序，而不是猜出的常量。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestLateStageFailureLeavesNoOwnerPublished(t *testing.T) {
	gate := armStageGate(t)

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

	coldCalls := gate.calls.Load()
	gate.limit.Store(coldCalls + 1)

	tick = writeGateConfig(t, yamlPath, leafYAML(false, store), tick)
	entry.CheckOrgReload()
	genAfterRemoval := diagInt64(t, entry.OrgDiagnostics(), "generation")
	waitFor(t, "the unrouted leaf retired", func() bool {
		return residentCacheForTest(entry)[g24Leaf] == nil
	})
	gate.limit.Store(gate.calls.Load() + 1)

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

	write(l3YAML("A", 6, 8000, 0.6, "2m"))
	entry.CheckOrgReload()
	before := entry.OrgDiagnostics()
	require.Equal(t, 6, entry.OrgKeepRecent())
	require.Equal(t, 4800, entry.OrgBudgetLine())
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

	c.swap(fp, base, nil)
	require.EqualValues(t, 1, c.status().Generation)
	require.EqualValues(t, 1, c.status().Revision)
	require.False(t, c.status().LastPublished.IsZero())
	pubAt := c.status().LastPublished

	require.False(t, c.recordHotApply(base, nil))
	require.EqualValues(t, 1, c.status().Revision)
	require.EqualValues(t, 1, c.status().Generation)

	changed := &Config{Entry: "main", Agents: map[string]AgentConfig{
		"main": {KeepRecentTasks: 7, MaxTokens: 9000, CompressThreshold: 0.5},
	}}
	require.True(t, c.recordHotApply(changed, nil))
	st := c.status()
	require.EqualValues(t, 2, st.Revision)
	require.EqualValues(t, 1, st.Generation)
	require.True(t, st.LastPublished.Equal(pubAt), "hot apply must not move lastPublishedAt")
	require.False(t, st.LastApplied.Equal(st.LastPublished))

	src := c.rollbackSource()
	require.NotNil(t, src)
	require.NotNil(t, src.cfg)
	require.Equal(t, 2, src.cfg.Agents["main"].KeepRecentTasks)
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
	drainRef := sub2Instance.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer drainRef.Release()

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
	heldOut := orig.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer heldOut.Release()

	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta.CheckOrgReload()
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.Same(t, orig, table["sub2"], "a same-name re-entry reuses the original resident owner")
	require.Same(t, orig.MemStore(), table["sub2"].MemStore(), "…and its original store (no second writer)")
	require.Contains(t, entryToolNames(ta), "sub2", "…and is routable again")
	require.Greater(t, ta.OrgDiagnostics()["generation"], genAtStart, "the re-add is a real publish")

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

	write(ownerYAML(t, []string{"sub1"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	require.Equal(t, gen0+1, entry.OrgDiagnostics()["generation"].(int64),
		"a storage change on an UNROUTABLE-but-still-defined agent must not freeze orchestration hot-reload")
	require.Nil(t, entry.OrgDiagnostics()["lastFailure"], "and nothing was refused")

	write(ownerYAMLWithModel(t, "test-model-x", []string{"sub1"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	require.Equal(t, gen0+2, entry.OrgDiagnostics()["generation"].(int64),
		"later orchestration edits must still apply")

	write(ownerYAMLWithModel(t, "test-model-x", []string{"sub1", "sub2"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	st := entry.OrgDiagnostics()
	require.Equal(t, gen0+2, st["generation"].(int64),
		"a re-entering name whose storage differs from its owner's baseline must still be refused")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "and the refusal must be diagnosable")
	require.Contains(t, fail.Error, "sub2")
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

	write(ownerYAML(t, []string{"sub2"}, sub2MemDefault(t)))
	entry.CheckOrgReload()
	require.Nil(t, cm.SubagentWrapper("sub1"),
		"after a hot removal an explicit relaunch must NOT resolve sub1 — the retired binding may not be revived")
	require.NotNil(t, cm.SubagentWrapper("sub2"), "the retained target keeps resolving (the face is not emptied by the removal)")

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	entry.CheckOrgReload()
	require.NotNil(t, cm.SubagentWrapper("sub1"), "re-entry makes it routable again")
	require.Equal(t, int64(0), cm.ExecutorRefs().InFlightTurns, "sanity: a reload publishes a generation but pins no in-flight turn")
}

const (
	hotAddReq  = "HOTADD-REQ-98"
	hotAddAns  = "HOTADD-ANS-98"
	hotAddMark = "main-final-98"
)

// TestHotAddDataLandsInItsOwnStoreWithHostReturn 钉住 经热路径加入的属主，其数据落在自己的存储段，并回到正确的宿主。
func TestHotAddDataLandsInItsOwnStoreWithHostReturn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(hotAddDataYAML(t, []string{"sub1"}))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &hotAddDataModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Nil(t, residentCacheForTest(entry)["sub2"], "precondition: sub2 has no owner before it is routed")

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

	require.Zero(t, countFacts(entryFacts, "agent_output", hotAddAns),
		"the hot-added owner's turn record must not be written into the host's store")

	require.Empty(t, storeFacts(t, entry.MemStore(), "sub2", hotAddAns),
		"the host store must carry no record under the hot-added owner's partition")

	storeDir := testStore(t, "hotadd-data-sub2")
	waitFor(t, "the owner's data really persisted to its own store directory", func() bool {
		return dirContains(t, storeDir, hotAddAns)
	})

	require.Contains(t, entry.StoreOwnerSnapshot(), "sub2", "precondition: the hot-added owner registered its store")
	require.NoError(t, entry.Close())
	require.NotContains(t, entry.StoreOwnerSnapshot(), "sub2",
		"the owner holding this data must release its store registration when the org closes")
	require.True(t, sub2.CloseStarted(), "and its own close sequence really ran")
}

// TestFactoryReloadConstructsNoOrphanAgents 钉住 触到工厂所属 agent 的结构发布必须构造零个 agent。
// - 装配若走整件产品式的工厂，就会为取一份执行配置而建出无人关闭的整个 agent；
// - 工厂交付声明时，同一发布走所有属主共用的那条面路径推进，且不构造任何东西。
func TestFactoryReloadConstructsNoOrphanAgents(t *testing.T) {
	const leaf = "g33_orphan_leaf"
	agent.RegisterToolAgent(leaf, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
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
	entry.CheckOrgReload()

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second"))
	require.NoError(t, err)
	waitFor(t, "a fresh delegation serves the new factory declaration", func() bool {
		return countServed(m.snapshot(), "FACTORY-3") >= 1
	})
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
				Tools:        []ToolRef{{Kind: ToolKindTool, ID: "no-such-tool-id-g33"}},
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

func TestResidentShellBuild_DoesNotDoubleArmSharedLease(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	busRoot := filepath.Join(root, "bus")
	spillRoot := filepath.Join(root, "spill")

	mc := MemoryConfig{Type: "localfile", Path: storeDir}
	rawStore, _, preRelease, err := resources.DefaultResources.Acquire("localfile", storeDir, resources.FingerprintMemory(mc), func() (resources.OpenedResource, error) {
		return openLocalFileStore(mc)
	})
	require.NoError(t, err)
	defer func() { _ = preRelease() }()
	fss, ok := rawStore.(*memory.FileSegmentStore)
	require.True(t, ok, "localfile shared store must be a *memory.FileSegmentStore")
	lease := fss.RetentionLease()
	require.NotNil(t, lease, "buildSharedResource must wire the retention lease")

	pid := memory.PartitionIDFromName("tagent")
	now := time.Now().UnixMilli()
	factKey := memory.NewSnowflakeEventKey(pid, now-10*24*3600*1000)
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

	resident, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeResident)
	require.NoError(t, err)
	rc.entryMemStore = resident.MemStore()
	rc.entrySessionSvc = resident.SessionSvc()
	require.Equal(t, 1, lease.Holders(factKey), "resident arm protects the fact original")
	require.Equal(t, 1, lease.Holders(receiptKey), "resident arm protects the receipt original")

	gen2 := cfg.Agents["tagent"]
	gen2.SystemPrompt = PromptConfig{Inline: "gen2"}
	_, err = buildAgent("tagent", gen2, cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeExecutorShell)
	require.NoError(t, err)

	require.Equal(t, 1, lease.Holders(factKey), "executor shell must add NO holder to the shared fact lease (C1)")
	require.Equal(t, 1, lease.Holders(receiptKey), "executor shell must add NO holder to the shared receipt lease (C1)")
}
