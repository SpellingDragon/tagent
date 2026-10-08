// org_candidate_test 覆盖候选事务与回滚：拒绝退场、指纹排除字段、关闭排空与同义应用不轮转。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
package tagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/org"
	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/resources"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/config"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestOrgFingerprint_AuditsEveryAgentConfigField 钉住 穷举式审计：每个配置字段要么折进指纹子集，要么在排除表里点名并写明理由。
// - 漏掉一个执行相关字段（工具开关、模型选择），该字段的变化就不会触发生成换代，这正是"配置改了而执行没变"的缺陷类；
// - 新增字段无需改动本用例即被要求归类，审计面随类型定义自动闭合。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestOrgFingerprint_AuditsEveryAgentConfigField(t *testing.T) {
	ac := populatedAgentConfig()
	raw, err := org.CanonicalAgentSubset(&ac)
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
	baseFP, err := org.ComputeOrgFingerprint(&base)
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
		fp2, err := org.ComputeOrgFingerprint(c2)
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
	fp, err := org.ComputeOrgFingerprint(&cfg)
	require.NoError(t, err)
	mc := cfg.Agents["main"]
	mfp := config.AgentMemoryFingerprint(&mc)

	clone, err := cfg.Clone()
	require.NoError(t, err)
	require.NotNil(t, clone)

	clonedFP, err := org.ComputeOrgFingerprint(clone)
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
	"memory":             "storage paths/backends cannot migrate at runtime — restart (pinned by the reloader's config.ChangedMemoryAgents/residentMemFP pre-check)",
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

func mustFP(t *testing.T, c *Config) string {
	t.Helper()
	fp, err := org.ComputeOrgFingerprint(c)
	require.NoError(t, err)
	return fp
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }

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

// candTxnRefusedYAML 让 main 同时委派 keep 与 aaa_parent，aaa_parent 再委派合法的 zzz_dep：
// 依赖经递归先建成并登记属主，aaa_parent 随后因缺失冥想提示词文件、在它的依赖建成之后才失败，
// 以此确定时序触发「被拒候选必须完全退场」。
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

// orgLastDiscardOrder returns the most recent candidate-discard order (TEST
// introspection only; nil before the first discard).
func orgLastDiscardOrder() []string {
	if v, ok := org.LastDiscardOrder(); ok {
		return v
	}
	return nil
}

const (
	g24Tool   = "g24_flaky_tool"
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
// The registration is process-global and a duplicate id panics, so the gate is a
// package-level singleton armed once and RESET per run — which keeps this file
// usable under a -count>1 repetition gate instead of needing an exemption.
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

// countMainCompletedByB counts MAIN records whose ToolResults already carry
// B's answer — the host-visible completion edge of a MAIN→B delegation turn.
func countMainCompletedByB(snaps []delegServed) int {
	n := 0
	for _, s := range snaps {
		if s.System != "MAIN" {
			continue
		}
		for _, r := range s.ToolResults {
			if strings.Contains(r, "served:SUB-B") {
				n++
				break
			}
		}
	}
	return n
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

// l3YAML renders a single-entry org named main whose system_prompt participates in the
// org fingerprint, while keep/max/threshold/terminal are hot-applicable numerics outside it.
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

// assertStoreWriterFree proves that a store lease really was handed back: the
// single-writer lock on that path must be acquirable by this test alone. A lock that
// is still held means either a leak or a live writer, so the exit is checked here.
func assertStoreWriterFree(t *testing.T, path string) {
	t.Helper()
	probe, err := os.OpenFile(filepath.Join(resources.Canonicalize(path), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer probe.Close()
	require.NoError(t, resources.FlockExclusive(probe),
		"关闭后 %s 的写锁必须已归还（恰一次释放，不待下一个用户请求）", path)
}

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

// sub2MemDefault renders sub2 with its own store path: each agent needs a distinct store
// to have observable identity. testStore moves the path out of the working tree and
// isolates it per case, because acquire makes the directory and takes its flock before
// dispatching on the store kind — a memory store still touches the filesystem.
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

// noopStoreForTakeover embeds a nil MemoryStore: Acquire only needs a non-nil store
// value, which this zero-cost stub provides.
type noopStoreForTakeover struct{ memory.MemoryStore }

// takeOverStore asks a FRESH registry to open the path: success proves the
// previous holder handed the writer slot back exactly once.
func takeOverStore(t *testing.T, path string) error {
	t.Helper()
	fresh := resources.NewRuntimeResources()
	fp := resources.FingerprintMemory(config.MemoryConfig{Type: "localfile", Path: path})
	_, _, rel, err := fresh.Acquire("localfile", path, fp, func() (resources.OpenedResource, error) {
		return resources.OpenedResource{Store: noopStoreForTakeover{}}, nil
	})
	if err == nil {
		require.NoError(t, rel())
	}
	return err
}

// seedUnackedEnvelope writes one claimed+prepared but un-acked durable envelope
// into the inbox rooted at spillDir (spillDir/inbox-v2), whose prepared fact key
// is factKey and reserved receipt key is receiptKey. A later
// NewReliableEventBus(spillDir)+ArmRetentionFromInbox enumerates it and protects
// exactly those keys, the same way a restart-recovery owner does.
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

	tick = writeGateConfig(t, yamlPath, ownerBYAML(true, 2, "1m"), tick)
	entry.CheckOrgReload()
	ownerB := residentCacheForTest(entry)[g24B]
	require.NotNil(t, ownerB, "B became a resident owner")

	tick = writeGateConfig(t, yamlPath, ownerBYAML(true, 7, "5m"), tick)
	entry.CheckOrgReload()
	require.Equal(t, 7, ownerB.OrgKeepRecent(), "B's own compressor consumer took the update")
	require.Equal(t, 5*time.Minute, ownerB.TaskManager().TerminalTTL(), "B's own manager took the update")

	bGate := make(chan struct{})
	m.armGate("SUB-B", bGate)
	t.Cleanup(func() { disarmGate(bGate) })
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("in-flight")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "B parked mid-call", func() bool {
		return m.parkedNow("SUB-B") >= 1
	})

	entry.Rollback()
	completedBase := countMainCompletedByB(m.snapshot())
	disarmGate(bGate)
	waitFor(t, "the in-flight B call completed", func() bool {
		return countMainCompletedByB(m.snapshot()) > completedBase
	})

	require.Equal(t, 2, ownerB.OrgKeepRecent(), "§2.4(b)：回滚把 B 自身的 keepRecent 恢复到环源值")
	require.Equal(t, time.Minute, ownerB.TaskManager().TerminalTTL(), "§2.4(b)：回滚把 B 自身的 terminal TTL 恢复到环源值")
	require.Same(t, ownerB, residentCacheForTest(entry)[g24B],
		"§2.4(b)：回滚推进执行面，不另造第二个 B owner（单一 owner＝实例与 store 身份不动）")

	servedNow := countServed(m.snapshot(), "SUB-B")
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("after")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a post-rollback call is served", func() bool {
		return countServed(m.snapshot(), "SUB-B") > servedNow
	})
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

	require.Equal(t, 2, entry.OrgKeepRecent())
	require.Equal(t, 2000, entry.OrgBudgetLine())
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"))
	require.EqualValues(t, 0, diagInt64(t, d, "revision"))
	_, hasPub := diagTime(t, d, "lastPublishedAt")
	require.False(t, hasPub, "startup is not a structural publish")

	write(l3YAML("B", 2, 4000, 0.5, "1m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "structural change advances the generation")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"), "a structural publish is also a full apply")
	pub1, hasPub := diagTime(t, d, "lastPublishedAt")
	require.True(t, hasPub)
	require.Equal(t, 2000, entry.OrgBudgetLine(), "prompt-only change leaves the budget")

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

	write(l3YAML("B", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, diagInt64(t, d, "revision"), "semantically identical apply must not rotate/advance (D9)")
	require.EqualValues(t, 1, diagInt64(t, d, "generation"))

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

	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, diagInt64(t, d, "generation"), "a rollback to identical content must not bump the generation")
	require.Equal(t, 2, entry.OrgKeepRecent(), "values stay at the restored source, unchanged")
	require.Equal(t, 2000, entry.OrgBudgetLine())
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL())

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

	write(l3YAML("A", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"), "numeric-only must not bump the generation")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"), "numeric-only is a full apply")
	require.Equal(t, 7, entry.OrgKeepRecent())
	require.Equal(t, 4500, entry.OrgBudgetLine())

	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "rollback publishes a new generation")
	require.EqualValues(t, 2, diagInt64(t, d, "revision"))
	require.Equal(t, 2, entry.OrgKeepRecent(), "rollback restores the startup keepRecent")
	require.Equal(t, 2000, entry.OrgBudgetLine(), "rollback restores the startup budget")
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL(), "rollback restores the startup terminal TTL")

	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "a rollback to identical content must not spin a new generation")
	require.Equal(t, 2, entry.OrgKeepRecent())
}

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
	park := newBuildPark()
	defer park.disarm()
	t.Cleanup(func() { _ = entry.Close() })

	write(sdCloseYAML(t, []string{"sub1", "sub2"}, storeOf))
	_ = acquireWithin(t, entry, 2*time.Second)
	park.waitEntered(t)

	closed := make(chan error, 1)
	go func() { closed <- entry.Close() }()
	time.Sleep(50 * time.Millisecond)
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

// TestFactoryBuiltReleasesItsStoreLease 钉住 同一契约的工厂侧：为该名字获取的租约经与配置路径同一个出口归还。
// - 出口只在最后一步交出，绝不在未收敛时交；
// - 可观察的见证是叶子自身路径上的 writer 锁：工厂分支不得从旁门再次取得属主责任。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestFactoryBuiltReleasesItsStoreLease(t *testing.T) {
	const leafName = "g33_fact_leaf"
	agent.RegisterToolAgent(leafName, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
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

func diagYAML(model string, subs ...string) string {
	return diagYAMLMem(model, "      type: memory\n", subs...)
}

// diagYAMLMem additionally overrides the ENTRY agent's memory block: an
// owner-held agent changing its storage must stay refused.
func diagYAMLMem(model, mainMem string, subs ...string) string {
	var b strings.Builder
	b.WriteString("entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n    model: " + model + "\n" +
		"    system_prompt:\n      inline: \"diag entry\"\n    tools:\n")
	for _, s := range subs {
		b.WriteString("      - kind: agent\n        agent: " + s + "\n        description: \"" + s + "\"\n")
	}
	b.WriteString("    memory:\n" + mainMem)
	for _, s := range subs {
		b.WriteString("  " + s + ":\n    model: " + model +
			"\n    system_prompt:\n      inline: \"" + s + "\"\n    memory:\n      type: memory\n")
	}
	return b.String()
}

// TestOrgCoordinator_StatusIsConsistentCopy 钉住 代际状态是一次一致快照，交出的是拷贝。
// - 失败记录带所在代与其 desired；被拒不进序号、不清 effective，成功发布才清除失败记录并记时间；
// - desired 与 fingerprint 分歧是运维可见信号，无新候选时失败记录报当前指纹，不造幻影缺口；
// - 逐 agent 回执整块替换（只保留最近一轮），改返回拷贝改不动内部状态。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestOrgCoordinator_StatusIsConsistentCopy(t *testing.T) {
	c := newOrgCoordinator()
	c.init("fp0", &Config{})

	st := c.status()
	require.Equal(t, int64(0), st.Generation, "init records generation 0")
	require.Equal(t, "fp0", st.Fingerprint)
	require.Nil(t, st.LastFailure, "no rejection recorded yet")
	require.True(t, st.LastApplied.IsZero(), "nothing applied yet")

	c.recordFailure(errSentinel{})
	st = c.status()
	require.NotNil(t, st.LastFailure)
	require.Equal(t, 0, st.LastFailure.Generation, "a rejection locates the generation it was attempted against")
	require.Equal(t, "sentinel", st.LastFailure.Error)
	require.Equal(t, st.Fingerprint, st.LastFailure.Desired,
		"without a fresh candidate in this cycle the failure reports the current fingerprint — no phantom gap")
	require.Equal(t, int64(0), st.Generation, "a rejection does not advance the generation")

	c.noteDesired("fp9")
	c.recordFailure(errSentinel{})
	st = c.status()
	require.Equal(t, "fp9", st.LastFailure.Desired)
	require.NotEqual(t, st.Fingerprint, st.LastFailure.Desired, "desired ≠ effective is the ops-facing signal")

	st.LastFailure.Error = "tampered"
	require.Equal(t, "sentinel", c.status().LastFailure.Error, "status hands out a copy, not the live record")

	_, gen := c.swap("fp1", &Config{}, nil)
	st = c.status()
	require.Equal(t, int64(1), st.Generation)
	require.Equal(t, "fp1", st.Fingerprint)
	require.Nil(t, st.LastFailure, "a successful publish clears the rejection record")
	require.Equal(t, st.Fingerprint, st.Desired, "success converges desired onto effective — no phantom pending change")
	require.False(t, st.LastApplied.IsZero())
	require.Equal(t, 1, gen.seq)

	c.recordApply([]OrgAgentApply{{Name: "a", Outcome: "applied"}, {Name: "b", Outcome: "draining"}})
	require.Len(t, c.status().Agents, 2)
	c.recordApply([]OrgAgentApply{{Name: "a", Outcome: "applied"}})
	st = c.status()
	require.Len(t, st.Agents, 1, "the receipt set is replaced wholesale — no unbounded history")
	st.Agents[0].Outcome = "tampered"
	require.Equal(t, "applied", c.status().Agents[0].Outcome, "status hands out a copy of the receipts too")
}

// TestOrgDiagnostics_FingerprintLabelIsBounded 钉住 指纹在诊断里只作截断标签，够对齐哪一次候选、不够当业务键。
// - 执行路径不得据截断指纹选版：它不是应用可见的身份（resident-continuity）。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestOrgDiagnostics_FingerprintLabelIsBounded(t *testing.T) {
	c := newOrgCoordinator()
	long := strings.Repeat("a", 64)
	c.init(long, &Config{})
	require.Len(t, c.status().Fingerprint, 8, "diagnostic label is the truncated fingerprint")
}

// TestOrgDiagnostics_EndToEnd 钉住 被拒候选与成功发布都经同一诊断访问器可观测，序号只随成功前进。
// - 载荷有界：字段是约定的固定集合，实时欠账组与关闭态各自成组、不冒充提交记录的原子快照；
// - 拒绝可具名到被拒的那次候选（报其 desired、不前进序号），成功发布前进序号并清除失败记录；
// - 逐 agent 回执与引用面就在同一载荷上，无需第二套抓取协议。
// - 每次写入都把 mtime 显式推进 2s：文件系统时间戳粒度会吞掉快速连续写入，不严格变新则换代不会发生。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestOrgDiagnostics_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(diagYAML("gpt-x", "helper"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	payload := func() map[string]any {
		t.Helper()
		p := entry.OrgDiagnostics()
		require.NotNil(t, p, "the assembly layer must register a diagnostics provider")
		_, err := json.Marshal(p)
		require.NoError(t, err, "the payload must be diagnostic-serializable as-is")
		for k := range p {
			switch k {
			case "generation", "revision", "fingerprint", "desired", "agents", "configPath", "lastAppliedAt", "lastPublishedAt", "lastFailure", "liveDebt", "close":
			default:
				t.Fatalf("unexpected diagnostics key %q — the payload is bounded by contract", k)
			}
		}
		return p
	}

	require.Equal(t, int64(0), payload()["generation"])

	entry.CheckOrgReload()
	st := payload()
	require.Equal(t, int64(0), st["generation"], "the startup generation stays 0")
	require.NotEmpty(t, st["fingerprint"], "the first check records the effective fingerprint")
	require.Nil(t, st["lastFailure"])

	write("entry: [broken")
	entry.CheckOrgReload()
	st = payload()
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "a rejection must surface a structured failure record, got %T", st["lastFailure"])
	require.Contains(t, fail.Error, "config parse")
	require.Equal(t, 0, fail.Generation)
	require.Empty(t, fail.Desired, "a config that cannot be parsed has no desired fingerprint — no phantom claim")
	require.Equal(t, int64(0), st["generation"], "a rejected candidate never advances the generation")

	write(diagYAML("gpt-w", "helper"))
	entry.CheckOrgReload()
	st = payload()
	require.Equal(t, int64(1), st["generation"], "the successful reload is generation 1")
	require.Nil(t, st["lastFailure"], "success clears the stale rejection record")
	require.NotNil(t, st["lastAppliedAt"])
	require.Equal(t, st["fingerprint"], st["desired"],
		"after a successful publish there must be no leftover desired-vs-effective gap")

	rec, ok := st["agents"].([]OrgAgentApply)
	require.True(t, ok, "per-agent receipts must be observable")
	outcomes := map[string]string{}
	for _, r := range rec {
		outcomes[r.Name] = r.Outcome
	}
	require.Equal(t, "applied", outcomes["main"], "the entry reports its own application")
	require.Equal(t, "applied", outcomes["helper"], "every routed agent reports its own application")
	debt, ok := st["liveDebt"].(OrgLiveDebt)
	require.True(t, ok, "the live reference debt must be its own declared group")
	require.False(t, debt.CapturedAt.IsZero(), "a live read must say when it was taken (§5.1: 不冒充记录快照)")
	refs := debt.Executors
	require.Zero(t, refs.PendingRetirees, "nothing is in flight here, so the superseded runner is already reclaimed")

	write(diagYAMLMem("gpt-w", "      type: localfile\n      path: diag-moved\n", "helper", "late"))
	entry.CheckOrgReload()
	st = payload()
	fail, ok = st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "a refused candidate must be diagnosable")
	require.Contains(t, fail.Error, "memory section changed")
	require.NotEmpty(t, fail.Desired, "the refusal must say WHICH candidate it refused")
	require.NotEqual(t, st["fingerprint"], fail.Desired,
		"desired ≠ effective is the direct evidence behind 「我改了为什么没生效」")
	require.Equal(t, int64(1), st["generation"], "the refusal keeps serving generation 1")
}

// hotParamsYAML 渲染 main→sub1 的两 agent 拓扑。四行数值（keep/max/threshold/terminal）
// 全部热可应用（不进 org 指纹），system_prompt 固定 → 只改数值即走 numeric-only 路径。
func hotParamsYAML(keepMain, maxMain int, thrMain float64, termMain string, keepSub, maxSub int, thrSub float64) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: \"diag entry\"\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keepMain) + "\n" +
		"    max_tokens: " + strconv.Itoa(maxMain) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(thrMain, 'f', -1, 64) + "\n" +
		"    task_terminal_ttl: " + strconv.Quote(termMain) + "\n" +
		"    tools:\n      - kind: agent\n        agent: sub1\n        description: \"sub1\"\n" +
		"    memory:\n      type: memory\n" +
		"  sub1:\n" +
		"    system_prompt:\n      inline: \"sub1\"\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keepSub) + "\n" +
		"    max_tokens: " + strconv.Itoa(maxSub) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(thrSub, 'f', -1, 64) + "\n" +
		"    memory:\n      type: memory\n"
}

// hotParamsYAMLNoSub renders main-only (sub1 dropped from BOTH the tool list and the
// agents table). Removing a routed agent is a structural change; held by an
// in-flight reference, sub1's owner stays resident → draining receipt.
func hotParamsYAMLNoSub(keepMain, maxMain int, thrMain float64, termMain string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: \"diag entry\"\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keepMain) + "\n" +
		"    max_tokens: " + strconv.Itoa(maxMain) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(thrMain, 'f', -1, 64) + "\n" +
		"    task_terminal_ttl: " + strconv.Quote(termMain) + "\n" +
		"    memory:\n      type: memory\n"
}

// budgetOf mirrors ContextCompressor.BudgetLine() exactly — int(float64(max)*thr)
// — so the receipt↔consumer cross-validation compares like-for-like and is immune
// to the float truncation of a non-exact threshold on either side.
func budgetOf(max int, thr float64) int { return int(float64(max) * thr) }

// diagnosticsReceipts reads the per-agent receipt set off the diagnostics payload.
func diagnosticsReceipts(t *testing.T, d map[string]any) map[string]OrgAgentApply {
	t.Helper()
	rec, ok := d["agents"].([]OrgAgentApply)
	require.True(t, ok, "per-agent receipts must be observable on the payload")
	byName := map[string]OrgAgentApply{}
	for _, r := range rec {
		byName[r.Name] = r
	}
	return byName
}

// TestReceiptIsBackedByRealConsumers 钉住 数值热更后每个 applied 回执所报数字等于该 agent 真实消费者现用的值，而非请求下发值的回声。
// - 预算线、keepRecent 逐一对齐其 live compressor 的实际消费；阈值非精确时用镜像公式比，不写死数字；
// - 路由子用其自己的压缩器，两个 agent 取到不同值——共享一个消费者就会露馅；
// - 无回执槽的轴（terminal TTL）在同一轮数值热更后于其消费者处证明确实移动。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestReceiptIsBackedByRealConsumers(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(hotParamsYAML(2, 4000, 0.5, "1m", 3, 8000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, 2000, entry.OrgBudgetLine())
	require.Equal(t, 4000, residentCacheForTest(entry)["sub1"].OrgBudgetLine())

	write(hotParamsYAML(7, 9000, 0.5, "5m", 5, 10000, 0.6))
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"), "numeric-only must not bump the structural generation")
	require.NotZero(t, diagInt64(t, d, "revision"), "numeric-only is a full apply")

	byName := diagnosticsReceipts(t, d)
	require.Equal(t, "applied", byName["main"].Outcome)
	require.Equal(t, "applied", byName["sub1"].Outcome)

	require.Equal(t, 9000, byName["main"].MaxTokens)
	require.InDelta(t, 0.5, byName["main"].ThresholdPct, 1e-9)
	require.Equal(t, 7, byName["main"].KeepRecentTasks)
	require.Equal(t, 7, entry.OrgKeepRecent(), "receipt keep == live compressor keepRecent (real consumer)")
	require.Equal(t, 4500, entry.OrgBudgetLine(), "live compressor moved to 9000×0.5")
	require.Equal(t, budgetOf(byName["main"].MaxTokens, byName["main"].ThresholdPct), entry.OrgBudgetLine(),
		"the receipt's own budget figures must reproduce what the compressor actually uses — 不只证明 setter 被调用")

	sub := residentCacheForTest(entry)["sub1"]
	require.Equal(t, 10000, byName["sub1"].MaxTokens)
	require.InDelta(t, 0.6, byName["sub1"].ThresholdPct, 1e-9)
	require.Equal(t, 5, byName["sub1"].KeepRecentTasks)
	require.Equal(t, 5, sub.OrgKeepRecent(), "sub receipt keep == sub live compressor keepRecent")
	require.Equal(t, budgetOf(byName["sub1"].MaxTokens, byName["sub1"].ThresholdPct), sub.OrgBudgetLine(),
		"a routed sub-agent's receipt budget must match its own compressor, not the entry's")
	require.NotEqual(t, entry.OrgBudgetLine(), sub.OrgBudgetLine(),
		"the two agents really did move to distinct values (guards against a shared/global consumer)")

	require.Equal(t, 5*time.Minute, entry.TaskManager().TerminalTTL(),
		"task_terminal_ttl reached its real consumer (TaskManager) on this numeric-only apply")
}

// TestDrainingReceiptTracksHeldConsumer 钉住 本代不路由的属主：回执为 draining 且数值字段为零，其真实消费者保持末次生效值。
// - 用真实在途引用（租约）造成排水窗口，使其属主留驻；
// - draining 回执不携带 applied 值，含义是本轮未评估，零配置是另一种事实；
// - 该属主的 live keepRecent 与预算仍是末次生效值，不被重新默认。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestDrainingReceiptTracksHeldConsumer(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(hotParamsYAML(2, 4000, 0.5, "1m", 3, 8000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	sub := residentCacheForTest(entry)["sub1"]
	heldKeep, heldBudget := sub.OrgKeepRecent(), sub.OrgBudgetLine()
	require.Equal(t, 3, heldKeep)
	require.Equal(t, 4000, heldBudget)

	draining := sub.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer draining.Release()

	write(hotParamsYAMLNoSub(7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	byName := diagnosticsReceipts(t, d)
	require.Equal(t, "applied", byName["main"].Outcome)
	require.Equal(t, 7, byName["main"].KeepRecentTasks)
	require.Equal(t, 4500, entry.OrgBudgetLine(), "the still-routed entry really moved")

	require.Equal(t, "draining", byName["sub1"].Outcome, "the unrouted owner reports a deliberate no-op")
	require.Zero(t, byName["sub1"].MaxTokens, "a draining receipt carries no applied value")
	require.Zero(t, byName["sub1"].KeepRecentTasks)
	require.Equal(t, heldKeep, sub.OrgKeepRecent(), "draining owner keeps its live keepRecent (not defaulted)")
	require.Equal(t, heldBudget, sub.OrgBudgetLine(), "draining owner keeps its live budget, not defaulted")
}

// routedSub2YAML renders main→(sub1,sub2) with sub2→leaf. sub2's own numeric knobs are
// parameters so a structural publish can introduce it with values that differ from
// the host's on every axis under test (budget inputs AND task TTL).
func routedSub2YAML(routeSub2 bool, keepSub2, maxSub2 int, thrSub2 float64, ttlSub2 string, ttlMain string) string {
	sub2Ref := ""
	if routeSub2 {
		sub2Ref = "      - kind: agent\n        agent: sub2\n        description: \"delegate-sub2\"\n"
	}
	sub2Def := ""
	if routeSub2 {
		sub2Def = "  sub2:\n" +
			"    system_prompt:\n      inline: \"SUB2-DIAG\"\n" +
			"    keep_recent_tasks: " + strconv.Itoa(keepSub2) + "\n" +
			"    max_tokens: " + strconv.Itoa(maxSub2) + "\n" +
			"    compress_threshold: " + strconv.FormatFloat(thrSub2, 'f', -1, 64) + "\n" +
			"    task_default_ttl: " + strconv.Quote(ttlSub2) + "\n" +
			"    memory:\n      type: memory\n" +
			"    tools:\n      - kind: agent\n        agent: leaf\n        description: \"delegate-leaf\"\n"
	}
	return "entry: main\nagents:\n  main:\n" +
		"    system_prompt:\n      inline: \"MAIN-DIAG\"\n" +
		"    keep_recent_tasks: 2\n    max_tokens: 4000\n    compress_threshold: 0.5\n" +
		"    task_default_ttl: " + strconv.Quote(ttlMain) + "\n" +
		"    memory:\n      type: memory\n" +
		"    tools:\n      - kind: agent\n        agent: sub1\n        description: \"delegate-sub1\"\n" + sub2Ref +
		"  sub1:\n    system_prompt:\n      inline: \"SUB1-DIAG\"\n    memory:\n      type: memory\n" +
		sub2Def +
		"  leaf:\n    system_prompt:\n      inline: \"LEAF-DIAG\"\n    memory:\n      type: memory\n"
}

func writeRoutedConfig(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// liveDebtOf / closeOf read the two cross-validation groups off the payload: each
// group carries its own collection instant, so a reading is never a snapshot stitched from several unlocked getters.
func liveDebtOf(t *testing.T, d map[string]any) OrgLiveDebt {
	t.Helper()
	debt, ok := d["liveDebt"].(OrgLiveDebt)
	require.True(t, ok, "the live reference debt must be reported as its own group")
	return debt
}

func closeOf(t *testing.T, d map[string]any) OrgCloseState {
	t.Helper()
	st, ok := d["close"].(OrgCloseState)
	require.True(t, ok, "the close phase must be reported as its own group")
	return st
}

// TestHotAddedOwnerReceiptMatchesRealConsumption 钉住 结构发布新增的 owner：回执等于其自身真实消费者的值，TTL 取其自己记录里的解析。
// - 回执预算对齐其 live compressor，且两个 agent 取到不同值（共享消费者会露馅）；
// - 它真实派生的子任务收到的是它自己的 manager 默认 TTL——非宿主的、非内置默认、非 setter 回声；
// - 同轮另一新增 owner 无 TTL 配置，取到第三个不同值，证明数值按属主分离而非广播。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestHotAddedOwnerReceiptMatchesRealConsumption(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	writeRoutedConfig(t, yamlPath, routedSub2YAML(false, 0, 0, 0, "3m", "9m"), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &chainDelegModel{prefer: []string{"sub2", "sub1", "leaf"}}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Nil(t, residentCacheForTest(entry)["sub2"], "precondition: sub2 has no owner before it is routed")

	writeRoutedConfig(t, yamlPath, routedSub2YAML(true, 7, 7000, 0.75, "3m", "9m"), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.NotZero(t, diagInt64(t, d, "generation"), "routing a new owner is structural: the generation advanced")

	rec := diagnosticsReceipts(t, d)
	sub2Rec, ok := rec["sub2"]
	require.Truef(t, ok, "the newly added owner must carry its own receipt (got %v)", rec["sub2"])
	require.Equal(t, "applied", sub2Rec.Outcome)
	require.Equal(t, 7000, sub2Rec.MaxTokens)
	require.Equal(t, 7, sub2Rec.KeepRecentTasks)

	sub2 := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, sub2)
	require.Equal(t, budgetOf(sub2Rec.MaxTokens, sub2Rec.ThresholdPct), sub2.OrgBudgetLine(),
		"§5.1：回执报的预算必须等于新 owner **自己真实 compressor** 的消费值，而非请求下发值的回声")
	require.Equal(t, 7, sub2.OrgKeepRecent(), "and the same for keepRecent at the live compressor")
	require.NotEqual(t, entry.OrgBudgetLine(), sub2.OrgBudgetLine(),
		"两个 agent 确实取到了不同的值（否则共享一个消费者也能通过本断言）")

	out, err := entry.StartLoop("u", "d53-receipt-consumer")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("do the work"))
	require.NoError(t, err)
	waitFor(t, "sub2 really delegated and adopted the run on its OWN board", func() bool {
		for _, tk := range sub2.TaskManager().List() {
			if tk.Spec.Kind == "subagent" {
				return true
			}
		}
		return false
	})

	// The spawned task carries no per-task override (TTL 0 = "inherit MY owner's
	// manager default" by design), so its lifetime is governed by sub2's own
	// resolved value — read it at that consumer's boundary.
	var specTTL time.Duration
	for _, tk := range sub2.TaskManager().List() {
		if tk.Spec.Kind == "subagent" {
			specTTL = tk.Spec.TTL
			break
		}
	}
	require.Zero(t, specTTL, "the subagent spawn inherits its owner's manager default by design (0 = no override)")
	require.Equal(t, 3*time.Minute, sub2.TaskManager().DefaultTTL(),
		"§5.1：热新增 owner 的 TTL 消费者必须解析到它自己的已提交记录（9m 是宿主的，10m 是内置默认）")

	leaf := residentCacheForTest(entry)["leaf"]
	require.NotNil(t, leaf)
	require.Equal(t, 10*time.Minute, leaf.TaskManager().DefaultTTL(),
		"the other new owner keeps the configured default — proving the values are per-owner, not a broadcast")
	require.Equal(t, 9*time.Minute, entry.TaskManager().DefaultTTL(), "the host's own value is a third distinct figure")
	require.Equal(t, budgetOf(7000, 0.75), sub2.OrgBudgetLine())

	debt := liveDebtOf(t, entry.OrgDiagnostics())
	require.False(t, debt.CapturedAt.IsZero(), "实时债务组必须自带采集时刻")
	require.GreaterOrEqual(t, debt.Executors.InFlightTurns, int64(0))
}

// TestCloseInitiatedIsDistinguishableFromResourcesExited 钉住 「关闭已发起」与「资源已退出」两个事实必须可区分。
// - 仍有引用持住时 Close 有界返回，载荷随即报 initiated、未 exited；
// - 收尾的延后尾巴跑完才翻到 exited；未发起过时该对读作（未发起、已退出）；
// - 造该状态用的是真实在途引用（租约），走的是回收门同一套账面。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestCloseInitiatedIsDistinguishableFromResourcesExited(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeRoutedConfig(t, yamlPath, routedSub2YAML(true, 5, 5000, 0.5, "4m", "9m"), &tick)

	entry := bootForDiagnostics(t, yamlPath)

	pristine := closeOf(t, entry.OrgDiagnostics())
	require.False(t, pristine.Initiated, "precondition: nothing has been closed yet")
	require.Truef(t, pristine.ResourcesExited,
		"precondition: nothing was ever deferred, so an unclosed owner is trivially not-stuck (got %+v)", pristine)

	held := entry.ContextManager().AcquireLease(agent.LeaseBackground)
	during := closeOf(t, entry.OrgDiagnostics())
	require.False(t, during.Initiated, "a held reference is not a close")
	require.Falsef(t, during.ResourcesExited,
		"work is outstanding, so the teardown cannot be reported as done (got %+v)", during)

	closed := make(chan error, 1)
	go func() { closed <- entry.Close() }()

	var mid OrgCloseState
	waitFor(t, "Close has been initiated while the reference is still held", func() bool {
		mid = closeOf(t, entry.OrgDiagnostics())
		return mid.Initiated && !mid.ResourcesExited
	})
	require.Falsef(t, mid.ResourcesExited,
		"§5.1：有界返回不得被读成收尾完成（已发起=%v 已退出=%v）", mid.Initiated, mid.ResourcesExited)

	held.Release()
	var fin OrgCloseState
	waitFor(t, "the deferred tail took every remaining exit", func() bool {
		fin = closeOf(t, entry.OrgDiagnostics())
		return fin.Initiated && fin.ResourcesExited
	})
	require.NoError(t, <-closed)
}

func bootForDiagnostics(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&chainDelegModel{prefer: []string{"sub2", "leaf"}}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

func TestExecGate_WorkAfterConvergedCloseIsRefused(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s3"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s3 := residentCacheForTest(entry)["s3"]
	require.NotNil(t, s3, "precondition: s3 resident at startup")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.NotContains(t, residentCacheForTest(entry), "s3", "precondition: s3 converged and left the table")
	require.True(t, s3.CloseStarted(), "precondition: its close completed, not just started")
	require.True(t, s3.Obligations().Idle(), "precondition: it holds no obligations")

	cm := s3.ContextManager()

	lease := cm.BeginTurnLease()
	require.NotNil(t, lease)
	if lease != nil {
		t.Cleanup(lease.Release)
		assert.Nil(t, lease.Runner(),
			"§3.2：已收敛关闭后 Acquire 必须被拒——把已关闭的执行器交给新 turn 正是 tryAcquireActive 注释里点名不可接受的失效")
	}
	assert.True(t, s3.Obligations().Idle(),
		"and a refused entry must not leave a new reference registered after the drain reported clean")

	_, _, injErr := s3.InjectEnvelope(context.Background(), "stale-holder",
		[]model.Message{{Role: model.RoleUser, Content: "into a converged owner"}})
	assert.ErrorIs(t, injErr, agent.ErrLoopTerminated,
		"§3.2：关闭后的 Inject 必须被具名拒绝（输入接受面＝环路已终止）")

	runErr := runBounded(t, "RunFlow", 10*time.Second, func() error {
		return cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "stale holder"})
	})
	assert.ErrorIs(t, runErr, agent.ErrExecClosed,
		"§3.2：关闭后的 Run 必须真正再进一次世代闸门并被具名拒绝，不得静默在已关闭执行器上跑完")
}

func TestAppliedRecordCommitsAtomicallyWithVersion(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	write(scHotAddNumericYAML(t, true, 4096, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	sub3 := func() *agent.TagentAgent { return residentCacheForTest(entry)["sub3"] }

	write(scHotAddNumericYAML(t, true, 5000, 2))
	entry.CheckOrgReload()
	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens, "前置：第一次 numeric 提交后记录须轮转到 5000")

	parked := make(chan struct{})
	release := make(chan struct{})
	park := func() {
		close(parked)
		<-release
	}
	orgCommitBarrier.Store(&park)
	t.Cleanup(func() { orgCommitBarrier.Store(nil) })
	releaseAll := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	t.Cleanup(releaseAll)

	done := make(chan struct{})
	go func() {
		defer close(done)
		write(scHotAddNumericYAML(t, true, 8100, 2))
		entry.CheckOrgReload()
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		releaseAll()
		<-done
		t.Fatal("S-C 红：数值-only 提交点未触发 orgCommitBarrier——记录轮转未落在同一提交闸门（半提交不可见契约缺实现）")
	}

	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens,
		"S-C 锚2：提交点内、记录轮转前，记录读者仍见上一次的 5000（半提交不可见）")
	require.Equal(t, 4000, sub3().OrgBudgetLine(),
		"S-E 锚：压缩器边界读经记录解析（源遮蔽 push 写入的原子字段），非 push 真值")

	releaseAll()
	<-done

	require.Equal(t, 8100, scHot(t, sub3()).MaxTokens, "提交完成后记录轮转到新值 8100")
	require.Equal(t, 6480, sub3().OrgBudgetLine(), "记录轮转后压缩器经源解析到新代预算线（8100×0.8），两轴同代")
}

// TestAppliedRecordReadIsLockFree 钉住 记录读面绝不触碰协调器锁。
// - 压缩器在每个存活上下文管理器的每个消费边界解析数值组，持锁读会把提交临界区放到压缩读路径上；
// - 装置在测试持有该锁时从活协程读记录：持锁实现只能有界失败，无锁实现立刻返回。
// 契约: docs/wiki/platform/org-hot-reload.md#lockfree-read
func TestAppliedRecordReadIsLockFree(t *testing.T) {
	coord := newOrgCoordinator()
	coord.init("", nil)
	_, gen := coord.swap("fp1", nil, []appliedAgent{{Name: "x", Hot: agent.OrgHotParams{MaxTokens: 7}}})
	require.NotNil(t, gen)

	var got agent.OrgHotParams
	done := make(chan struct{})
	coord.mu.Lock()
	go func() {
		got, _ = coord.currentHotFor("x")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		coord.mu.Unlock()
		t.Fatal("S-E 红：currentHotFor 仍依赖 coord.mu——记录读面未与提交临界区解耦（design §2 要求 S-E 无锁化，与 compressor 侧同源）")
	}
	coord.mu.Unlock()

	require.Equal(t, 7, got.MaxTokens, "无锁读仍须读到已提交记录")
}

func TestOrgFingerprint_StableAcrossEmptyChanges(t *testing.T) {
	a, err := org.ComputeOrgFingerprint(cfgFor())
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	b := cfgFor()
	b.Governance.Dir = "data/gov2"
	b.Reliability.BusSpillDir = "data/bus2"
	am := cfgFor()
	am.Agents["main"] = AgentConfig{Model: "gpt-x", CompressThreshold: 0.8, Tools: []ToolRef{{Kind: "tool", ID: "recall"}}, Memory: MemoryConfig{Path: "data/mem2"}}
	c := cfgFor()
	c.APIEndpoint = "https://other.example.com"
	act := cfgFor()
	act.Agents["main"] = AgentConfig{Model: "gpt-x", CompressThreshold: 0.5, Tools: []ToolRef{{Kind: "tool", ID: "recall"}}}

	for name, mod := range map[string]*Config{"gov": b, "mem": am, "api": c, "ct": act} {
		fp, err := org.ComputeOrgFingerprint(mod)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fp != a {
			t.Errorf("%s: excluded field changed fingerprint %s.. -> %s..", name, a[:8], fp[:8])
		}
	}
}

func TestOrgFingerprint_ChangesOnOrgFields(t *testing.T) {
	base, err := org.ComputeOrgFingerprint(cfgFor())
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	mut := []struct {
		name string
		mod  func(*Config)
	}{
		{"tools", func(c *Config) {
			ac := c.Agents["main"]
			ac.Tools = []ToolRef{{Kind: "tool", ID: "recall"}, {Kind: "tool", ID: "knowledge"}}
			c.Agents["main"] = ac
		}},
		{"agent_added", func(c *Config) { c.Agents["extra"] = AgentConfig{Model: "gpt-z"} }},
		{"agent_removed", func(c *Config) { delete(c.Agents, "sub") }},
		{"provider_endpoint", func(c *Config) {
			p := c.Providers["p1"]
			p.APIEndpoint = "https://v2.example.com"
			c.Providers["p1"] = p
		}},
		{"entry", func(c *Config) { c.Entry = "sub" }},
		{"model", func(c *Config) { ac := c.Agents["main"]; ac.Model = "gpt-w"; c.Agents["main"] = ac }},
	}
	for _, m := range mut {
		c := cfgFor()
		m.mod(c)
		fp, err := org.ComputeOrgFingerprint(c)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if fp == base {
			t.Errorf("%s: org field changed but fingerprint did not", m.name)
		}
	}
}

func TestOrgFingerprint_CanonicalStable(t *testing.T) {
	c1, c2 := cfgFor(), cfgFor()
	f1, err := org.ComputeOrgFingerprint(c1)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	f2, err := org.ComputeOrgFingerprint(c2)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if f1 != f2 {
		t.Errorf("canonical form unstable: %s.. vs %s..", f1[:8], f2[:8])
	}
}

// TestOrgFingerprint_ChangesOnGlobalModelDefaults 钉住 全局 provider 与 model 必须参与组织指纹。
// - 二者驱动子 agent 实例的解析；只改 yaml 时若不进指纹，这次翻转对热更完全隐身。
func TestOrgFingerprint_ChangesOnGlobalModelDefaults(t *testing.T) {
	base := &Config{Entry: "main", Provider: "zhipu", Model: "glm-5.3-flash"}
	fp0, err := org.ComputeOrgFingerprint(base)
	if err != nil {
		t.Fatalf("baseline fingerprint: %v", err)
	}
	for _, mut := range []struct {
		name string
		mut  func(c *Config)
	}{
		{"model", func(c *Config) { c.Model = "glm-5.3" }},
		{"provider", func(c *Config) { c.Provider = "deepseek" }},
	} {
		c := *base
		mut.mut(&c)
		fp, err := org.ComputeOrgFingerprint(&c)
		if err != nil {
			t.Fatalf("%s: fingerprint: %v", mut.name, err)
		}
		if fp == fp0 {
			t.Errorf("%s change did NOT alter org fingerprint (fp %s)", mut.name, fp[:8])
		}
	}
}

// TestMemoryFingerprint_DetectsMemoryOnlyChanges 钉住 白名单语义：只改存储段时组织指纹必须保持不变。
// - 因此存储段的变更必须由重载路径上的真实比较独立感知；
// - 否则懒检查会静默走数值分支——变更不生效，也不告警。
func TestMemoryFingerprint_DetectsMemoryOnlyChanges(t *testing.T) {
	base := cfgFor()
	orgFP, err := org.ComputeOrgFingerprint(base)
	if err != nil {
		t.Fatalf("org fp: %v", err)
	}
	mod := cfgFor()
	mod.Agents["main"] = AgentConfig{
		Model: "gpt-x", Tools: []ToolRef{{Kind: "tool", ID: "recall"}},
		Memory: MemoryConfig{Type: "file", Path: "data/mem2"},
	}
	orgFP2, err := org.ComputeOrgFingerprint(mod)
	if err != nil {
		t.Fatalf("org fp2: %v", err)
	}
	if orgFP2 != orgFP {
		t.Errorf("memory-only change must NOT alter the org fingerprint (D3 whitelist)")
	}
	orgMod := cfgFor()
	a := orgMod.Agents["main"]
	a.Model = "gpt-z"
	orgMod.Agents["main"] = a
	if ofp, err := org.ComputeOrgFingerprint(orgMod); err != nil || ofp == orgFP {
		t.Errorf("non-memory change must alter the org fingerprint (err=%v)", err)
	}
}

// TestOrgHotShift_EndToEnd 钉住 不重启进程驱动完整的热更懒检查闭环，对象是真实构造的 agent。
// - 次序为修改时间变动 → 重新解析 → 指纹比较 → 指纹不变则走数值热更；
// - 指纹变化而配置损坏时必须继续服务旧代，不得半改。
func TestOrgHotShift_EndToEnd(t *testing.T) {
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

	write(e2eYAML(0.8, "gpt-x"))
	cfg, err := LoadConfig(yamlPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = entry.Close() }()

	if got := entry.OrgThreshold(); got != 0.8 {
		t.Fatalf("initial threshold = %v, want 0.8 (constructor must seed cm.thresholdPct)", got)
	}

	write(e2eYAML(0.5, "gpt-x"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after hot shift threshold = %v, want 0.5", got)
	}

	write(e2eYAML(0.5, "gpt-w"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after structural change threshold = %v, want 0.5 (kept)", got)
	}

	write("entry: [broken")
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after broken config threshold = %v, want 0.5 (fail-closed)", got)
	}
}

// TestOrgHotReload_ExecutorSwapEndToEnd 钉住 结构变更后经只含执行器的面装配换入新执行器。
// - 换代不需重启，且常驻不变量原封不动；
// - 与懒检查那条互补：这一条只钉换缝的缝合点；
// - 不换入时运行器引用永不变化，正是必须重启这一旧方案的失败形状。
func TestOrgHotReload_ExecutorSwapEndToEnd(t *testing.T) {
	rc := &runtimeConfig{model: &factoryMockModel{}}
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "gen1 prompt"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	resident, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	rc.entryMemStore = resident.MemStore()
	rc.entrySessionSvc = resident.SessionSvc()
	require.NotNil(t, rc.entryMemStore)
	require.NotNil(t, rc.entrySessionSvc)

	oldRunner := resident.Runner()
	require.NotNil(t, oldRunner)
	oldStore := resident.MemStore()
	oldSvc := resident.SessionSvc()
	oldTM := resident.TaskManager()

	gen2 := cfg.Agents["tagent"]
	gen2.SystemPrompt = PromptConfig{Inline: "gen2 prompt"}
	rebuilt, err := buildAgent("tagent", gen2, cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeExecutorShell)
	require.NoError(t, err)
	newRunner := rebuilt.Runner()
	require.NotNil(t, newRunner)
	require.NotSame(t, oldRunner, newRunner, "rebuilt shell must carry a NEW runner")

	require.Same(t, oldStore, rebuilt.MemStore(), "executorOnly shell must reuse the resident memStore")
	require.Same(t, oldSvc, rebuilt.SessionSvc(), "executorOnly shell must reuse the resident sessionSvc")

	require.Same(t, oldRunner, resident.Runner(), "pre-swap: runner unchanged (structural change invisible)")

	resident.ContextManager().PublishExecutor(newRunner, resident.ContextManager().ExecutorConfig())
	require.Same(t, newRunner, resident.Runner(), "post-swap: next turn sees the new runner")

	require.Same(t, oldStore, resident.MemStore(), "fact chain must survive the swap")
	require.Same(t, oldSvc, resident.SessionSvc(), "session service must survive the swap")
	require.Same(t, oldTM, resident.TaskManager(), "task registry must survive the swap")

	rolled := false
	resident.SetRollbackFn(func() { rolled = true })
	resident.Rollback()
	require.True(t, rolled, "Rollback() must invoke the wired hook")
	resident.SetRollbackFn(nil)
	resident.Rollback()
}

// TestOrgGenerationsStructuresStayBounded 钉住 连续换代只推进序号，任何簿记结构的规模都恒等于拓扑大小。
// - 空闲态不得留下未回收的执行器；
// - 容量为二的回滚环在任意多代之后仍然可用。
func TestOrgGenerationsStructuresStayBounded(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	const generations = 25

	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "gen-0", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	for i := 1; i <= generations; i++ {
		writeBumped(t, yamlPath, ownerYAMLWithModel(t, "gen-"+strings.Repeat("x", i), []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
		entry.CheckOrgReload()
		require.Equal(t, int64(i), entry.OrgDiagnostics()["generation"],
			"generation %d must publish", i)
	}

	st := entry.OrgDiagnostics()
	require.Equal(t, int64(generations), st["generation"])

	require.Len(t, residentCacheForTest(entry), 3, "the resident binding table stays topology-sized across generations")
	rec, ok := st["agents"].([]OrgAgentApply)
	require.True(t, ok)
	require.Len(t, rec, 3, "the receipt set stays topology-sized — no per-generation accumulation")
	refs := entry.ContextManager().ExecutorRefs()
	require.Zero(t, refs.PendingRetirees, "an idle process holds no unreclaimed retired executors")

	entry.Rollback()
	require.Equal(t, int64(generations+1), entry.OrgDiagnostics()["generation"],
		"rollback after many generations republishes as a new sequence")
	require.Len(t, residentCacheForTest(entry), 3, "rollback neither grows nor drops the resident table")
}

// TestReloadCostAtProductionShape_Sample 钉住 报告生产形状（一个入口加四个子项）下一次完整重载与发布的开销。
// - 该数字只作观察量，不设墙钟上界：短锁是结构性性质，由调度那条用例钉住；
// - 必须保留每一轮都真实发布这一反空转前提，样本才有意义。
func TestReloadCostAtProductionShape_Sample(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeAt := func(model string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(prodShapeYAML(t, model)), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	writeAt("prod-a")

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	const rounds = 20
	start := time.Now()
	for i := 0; i < rounds; i++ {
		writeAt([]string{"prod-b", "prod-c"}[i%2])
		entry.CheckOrgReload()
	}
	elapsed := time.Since(start)

	st := entry.OrgDiagnostics()
	require.Equal(t, int64(rounds), st["generation"],
		"every round must really publish (otherwise this sample measures nothing)")

	per := elapsed / time.Duration(rounds)
	t.Logf("[perf sample] production shape (entry+4 subagents): one full reload+publish = %v (avg over %d rounds) — observational, not a correctness gate",
		per.Round(time.Microsecond), rounds)

	parseStart := time.Now()
	for i := 0; i < rounds; i++ {
		if _, lerr := LoadConfig(yamlPath); lerr != nil {
			t.Fatal(lerr)
		}
	}
	parsePer := time.Since(parseStart) / time.Duration(rounds)
	t.Logf("[perf sample] of which config re-parse alone = %v", parsePer.Round(time.Microsecond))
}

// TestPerPublishObjectLifespan 钉住 对固定拓扑，按对象身份记录冷启动、重载与回滚各自构造了什么。
// - 组织级计数不得当证据：活的 TaskManager 计数在复用与每轮丢弃副本两种形状下都不变；
// - 一次发布应为每个可达属主恰好构造一个新的执行面，此外什么都不构造；
// - 不得出现第二个 agent、第二个 TaskManager、重新绑定的存储、新的会话服务或第二个维护生产者；
// - 回滚必须与正向发布同价——它们是同一条代码路径。
func TestPerPublishObjectLifespan(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	writeBumped(t, yamlPath, prodShapeYAML(t, "life-a"), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	cold := map[string]*struct {
		agent, cm, tm, store, sess, runner interface{}
	}{}
	for name, owner := range residentCacheForTest(entry) {
		cold[name] = &struct {
			agent, cm, tm, store, sess, runner interface{}
		}{owner, owner.ContextManager(), owner.TaskManager(), owner.MemStore(), owner.SessionSvc(), owner.Runner()}
	}
	require.Len(t, cold, 5, "production shape: entry + 4 workers")

	compare := func(stage string, before map[string]any, owners map[string]interface{}) {
		t.Helper()
	}
	_ = compare

	current := func() map[string][6]interface{} {
		out := map[string][6]interface{}{}
		for name, owner := range residentCacheForTest(entry) {
			out[name] = [6]interface{}{owner, owner.ContextManager(), owner.TaskManager(), owner.MemStore(), owner.SessionSvc(), owner.Runner()}
		}
		return out
	}

	before := current()
	writeBumped(t, yamlPath, prodShapeYAML(t, "life-b"), &tick)
	entry.CheckOrgReload()
	require.Equal(t, int64(1), diagInt64(t, entry.OrgDiagnostics(), "generation"), "precondition: reload #1 published")

	after := current()
	reused, rebuilt := 0, 0
	for name, b := range before {
		a := after[name]
		labels := []string{"owner", "contextManager", "taskManager", "memStore", "sessionSvc", "runner"}
		for i := range labels {
			if b[i] == a[i] {
				reused++
			} else {
				rebuilt++
				require.Equalf(t, "runner", labels[i],
					"§5.3 reload: only the execution face may be reconstructed (owner %q had %s replaced)", name, labels[i])
			}
		}
	}
	require.Equal(t, 5, rebuilt, "one new runner per reachable owner, no more")
	require.Equal(t, 25, reused, "everything else (5 owners × 5 objects) must be the same instances")

	writeBumped(t, yamlPath, prodShapeYAML(t, "life-c"), &tick)
	entry.CheckOrgReload()
	after2 := current()
	rebuilt2 := 0
	for name, b := range after {
		for i := range b {
			if b[i] != after2[name][i] {
				rebuilt2++
			}
		}
	}
	require.Equal(t, 5, rebuilt2, "reload #2 constructs exactly the 5 faces again")
	require.Len(t, residentCacheForTest(entry), 5, "and no owner set growth across generations")

	rolledBefore := current()
	entry.Rollback()
	rolledAfter := current()
	rolledRebuilt := 0
	for name, b := range rolledBefore {
		for i := range b {
			if b[i] != rolledAfter[name][i] {
				rolledRebuilt++
			}
		}
	}
	require.Equal(t, 5, rolledRebuilt,
		"rollback reconstructs exactly the execution faces — it must not copy the agent set either")

	t.Logf("[§5.3 lifespan] topology=5 owners | per publish (cold→reload→reload→rollback): new TagentAgent=0, new ContextManager=0, new TaskManager=0, re-bound store=0, new session service=0, new runners=5 — the old shell path built one extra whole agent per CHANGED owner in addition to these")
}

// TestHotReload_MixedChange_IdentityAndParams 钉住 在入口加两个子项的拓扑上同时变更工具（结构）与子项预算（数值）。
// - 各 agent 的常驻存储身份不漂移：指针不变，子项不被换成入口那一份；
// - 数值参数作用于新代的真实对象；
// - 字段删除回落到解析默认，按全量期望值处理；
// - 新增 agent 拒绝热更，走失败关闭。
func TestHotReload_MixedChange_IdentityAndParams(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(content string) {
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
	}
	write(hotYAML(t, 2, 2, false))

	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()

	cache := residentCacheForTest(ta)
	require.Len(t, cache, 3)
	idMain := cache["main"].MemStore()
	idSub1 := cache["sub1"].MemStore()
	idSub2 := cache["sub2"].MemStore()
	require.NotSame(t, idSub1, idMain, "sub1 must own its own store")
	require.NotSame(t, idSub2, idMain, "sub2 must own its own store")

	write(hotYAML(t, 5, 2, true))
	ta.CheckOrgReload()

	require.Same(t, idMain, cache["main"].MemStore(), "entry store identity unchanged")
	require.Same(t, idSub1, cache["sub1"].MemStore(), "sub1 store identity unchanged (no drift to entry store)")
	require.Same(t, idSub2, cache["sub2"].MemStore(), "sub2 store identity unchanged")

	require.Equal(t, 5, keepRecentOf(cache["sub1"]), "sub1 keepRecent hot-applied")
	require.Equal(t, 2, keepRecentOf(cache["sub2"]), "untouched agent keeps its configured value")

	write(hotYAMLAddSub3(t, 5, 2))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.Len(t, table, 4, "the hot-added agent joins the resident binding table")
	require.NotNil(t, table["sub3"], "a hot add must be merged, not refused")
	require.NotSame(t, idMain, table["sub3"].MemStore(), "the added agent owns its own store (no drift onto the entry store)")
	require.NotSame(t, idSub1, table["sub3"].MemStore(), "…and does not borrow a sibling's store")
	require.Same(t, idMain, table["main"].MemStore(), "entry store identity survives a real publish")
	require.Same(t, idSub1, table["sub1"].MemStore(), "sub1 store identity survives the add")
	require.Same(t, idSub2, table["sub2"].MemStore(), "sub2 store identity survives the add")
	require.Equal(t, 2, keepRecentOf(table["sub3"]), "the added agent starts at its parsed default")
}

// TestHotReload_MemoryRejectionNotEffective 钉住 存储段变更被拒绝时不推进 effective 基准。
// - 第二次检查仍以上一次生效的记录为基准；拒绝路径不得改动任何常驻身份或资源。
// 契约: docs/wiki/platform/org-hot-reload.md#memory-preflight
func TestHotReload_MemoryRejectionNotEffective(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(content string) {
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
	}
	base := hotYAML(t, 2, 2, false)
	write(base)
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()
	cache0 := residentCacheForTest(ta)
	idMainKeep := cache0["main"].MemStore()
	idSub1Keep := cache0["sub1"].MemStore()

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
    keep_recent_tasks: 2
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
    keep_recent_tasks: 2
`, testStore(t, "hottest-sub1"), testStore(t, "hottest-sub2"))
	write(modified)
	ta.CheckOrgReload()

	write(base)
	ta.CheckOrgReload()
	cache := residentCacheForTest(ta)
	require.Same(t, idSub1Keep, cache["sub1"].MemStore(), "rejected reload must not touch resident stores")
	_ = idMainKeep
}

// TestHotReload_FieldDeletionFallsBackToDefault 钉住 数值热应用取本代解析结果，不沿用上代生效值。
// - 定义里删除 keep_recent_tasks 后该 agent 回落解析默认（2），不会停在曾被热更成的 5；
// - 断言对象必须自己携带该字段：入口无此字段时按它断言只会看到默认值，测不到回落方向。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotReload_FieldDeletionFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(c string) { require.NoError(t, os.WriteFile(yamlPath, []byte(c), 0o644)) }
	write(hotYAML(t, 5, 2, false))
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()
	sub1 := residentCacheForTest(ta)["sub1"]
	require.Equal(t, 5, sub1.OrgKeepRecent(), "startup honors explicit keep_recent_tasks on sub1")

	write(hotYAML(t, 0, 2, false))
	ta.CheckOrgReload()
	sub1 = residentCacheForTest(ta)["sub1"]
	require.Equal(t, 2, sub1.OrgKeepRecent(),
		"deleted field falls back to the parsed default (2) — full-desired semantics")
}

// TestHotReload_RemovedOwnerKeepsItsParams 钉住 排空中的属主不参与数值热应用，本代仍路由的照常收新值。
// - 定义被同时删除时把解析默认下发给排水属主，会静默改掉仍在服务的旧代工作；
// - 豁免必须有向：只放过排水者，不得连带冻住本代仍路由的 agent（含入口）；
// - 回执要把两种结果分开：排水者 outcome 为 draining 且不携带已应用值。
// - 排空窗口必须自己制造：先给待移除的属主占一份租约，未被路由且空闲的属主会在同一刻退役，场景就不真实了。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotReload_RemovedOwnerKeepsItsParams(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(dropAgentYAML(t, map[string]int{"main": 4, "sub1": 7, "sub2": 3}))
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	table := residentCacheForTest(ta)
	require.Equal(t, 7, keepRecentOf(table["sub1"]), "baseline: sub1 serves its configured value")
	draining := table["sub1"].ContextManager().AcquireLease(agent.LeaseSubCall)
	defer draining.Release()

	write(dropAgentYAML(t, map[string]int{"main": 4, "sub2": 5}, "sub1"))
	ta.CheckOrgReload()

	after := residentCacheForTest(ta)
	require.NotNil(t, after["sub1"], "the removed agent keeps its resident owner (drain, not retire)")
	require.Equal(t, 7, keepRecentOf(after["sub1"]),
		"a draining owner must NOT be re-parameterized to parsed defaults")
	require.Equal(t, 5, keepRecentOf(after["sub2"]),
		"an agent this generation still routes to keeps receiving its new value")

	rec, ok := ta.OrgDiagnostics()["agents"].([]OrgAgentApply)
	require.True(t, ok, "the per-agent receipts must be observable on the diagnostics payload")
	byName := map[string]OrgAgentApply{}
	for _, r := range rec {
		byName[r.Name] = r
	}
	require.Equal(t, "draining", byName["sub1"].Outcome, "the receipt names the deliberate no-op")
	require.Zero(t, byName["sub1"].KeepRecentTasks, "draining carries no applied value")
	require.Equal(t, "applied", byName["sub2"].Outcome)
	require.Equal(t, 5, byName["sub2"].KeepRecentTasks)
	require.Equal(t, "applied", byName["main"].Outcome, "the entry is covered too")
}

// TestOrgHotReload_EntryIdentityChangeRefusedBeforeBuild 钉住 入口身份变化在任何候选资源构建之前被拒。
// - 拒绝必须点名入口，诊断面要能把它与普通执行器重建失败分开；
// - 结构代不前进，新入口名下零属主被构建，旧入口实例与其存储身份都不变；
// - 仍生效的那一代按已发布声明继续路由。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestOrgHotReload_EntryIdentityChangeRefusedBeforeBuild(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(entryRenameStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)

	before := residentCacheForTest(ta)
	require.NotNil(t, before["main"], "startup entry 'main' is resident")
	mainInstance, mainStore := before["main"], before["main"].MemStore()
	gen0 := ta.OrgDiagnostics()["generation"]
	require.Contains(t, entryToolNames(ta), "sub1", "the startup generation routes to sub1")

	write(entryRenameChangedYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "an entry-identity change never publishes")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, strings.ToLower(fail.Error), "entry",
		"the refusal must name the entry-identity change, not surface an opaque rebuild error")

	after := residentCacheForTest(ta)
	require.Nil(t, after["entry2"], "no owner may be built for the refused entry identity")
	require.Same(t, mainInstance, after["main"], "the original entry owner is untouched")
	require.Same(t, mainStore, after["main"].MemStore(), "…and keeps its store identity")

	require.Contains(t, entryToolNames(ta), "sub1", "the old entry keeps routing to sub1")
}

// TestOrgHotReload_EntryRenameWithOldDefKeptAlsoRefused 钉住 入口身份规则不看旧定义去留。
// - 旧入口定义仍在 agents 里时，改名同样在构建之前被拒、新入口名下仍零属主；
// - 判定只取入口身份是否变化，携带两份合法配置的改名不得静默发布一代。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestOrgHotReload_EntryRenameWithOldDefKeptAlsoRefused(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(entryRenameStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)
	mainStore := residentCacheForTest(ta)["main"].MemStore()
	gen0 := ta.OrgDiagnostics()["generation"]

	bothYAML := entryRenameChangedYAML + `  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
`
	write(bothYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "keeping the old definition must not let a rename publish")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, strings.ToLower(fail.Error), "entry")
	require.Nil(t, residentCacheForTest(ta)["entry2"], "still zero build for the new entry")
	require.Same(t, mainStore, residentCacheForTest(ta)["main"].MemStore(), "old entry owner untouched")
}

// TestModelRefAliasesFoldToStableFingerprint 钉住 同一设置的旧式扁平写法与规范 ModelRef 写法必须折出同一份指纹。
func TestModelRefAliasesFoldToStableFingerprint(t *testing.T) {
	dir := t.TempDir()

	legacy := writeCfg(t, dir, "legacy.yaml", aliasYAML("    compress:\n      summary_model: summ\n"))
	canonical := writeCfg(t, dir, "canonical.yaml", aliasYAML("    compress:\n      summary:\n        model: summ\n"))

	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model,
		"summary_model folded into compress.summary.model at load")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel,
		"the legacy field is cleared after folding (not left as a second source)")

	require.Equal(t, mustFP(t, legacy), mustFP(t, canonical),
		"alias and canonical spellings of the same setting fold to an identical fingerprint")
}

// TestFingerprintFoldedFieldIsLive 钉住 被折叠字段必须真实参与指纹，稳定不得来自字段缺席。
// - 它是别名稳定性用例的对照组：折叠字段整体不进指纹时两种写法同样相等，真变更也一并隐身；
// - 同一条书写路径下的两个不同值必须让指纹随之变化。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintFoldedFieldIsLive(t *testing.T) {
	dir := t.TempDir()
	base := writeCfg(t, dir, "base.yaml", aliasYAML("    compress:\n      summary_model: summ\n"))
	changed := writeCfg(t, dir, "changed.yaml", aliasYAML("    compress:\n      summary_model: different\n"))
	require.NotEqual(t, mustFP(t, base), mustFP(t, changed),
		"the folded summary model is genuinely part of the fingerprint — stability is not from exclusion")
}

// TestFingerprintFoldIsIdempotentUnderRepeat 钉住 折叠必须是不动点：重跑既不搬值也不复活扁平键。
// - 第二次检查会拿到同一份已折叠的生效配置，重跑是常态而非例外；
// - 指纹必须逐字节不变，否则别名语义随检查次数漂移，代际比较失去意义。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintFoldIsIdempotentUnderRepeat(t *testing.T) {
	dir := t.TempDir()
	legacy := writeCfg(t, dir, "legacy.yaml", aliasYAML("    compress:\n      summary_model: summ\n      summary_provider: p1\n"))
	before := mustFP(t, legacy)

	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model)
	require.Equal(t, "p1", legacy.Agents["main"].Compress.Summary.Provider)

	legacy.FoldModelRefAliases()
	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model, "value is not lost on re-fold")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel, "legacy field does not reappear")
	require.Equal(t, before, mustFP(t, legacy), "re-folding does not drift the fingerprint")
}

// TestHotAddedAgentNumericOnlySeedsNextCall 钉住 数值热应用覆盖热增属主，其新调用种子取自提交点轮转后的记录。
// - 结构热增之后的纯数值编辑必须到达该属主的新子调用，冷启动与热增两条路同价；
// - 断言取宿主结果（预算线＝额度×阈值）而非字段回声，才能证明消费方真的现读；
// - 纯数值编辑不得增加结构代。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotAddedAgentNumericOnlySeedsNextCall(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	write(scHotAddNumericYAML(t, false, 0, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	write(scHotAddNumericYAML(t, true, 4096, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "热增须发布（否则后续断言空洞）")

	write(scHotAddNumericYAML(t, true, 8100, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "numeric-only 不加代")

	table := residentCacheForTest(entry)
	sub3 := table["sub3"]
	require.NotNil(t, sub3, "sub3 须在常驻表")
	require.Equal(t, 6480, sub3.OrgBudgetLine(),
		"S-C 锚1：热增→numeric-only 后，热增 owner 的新调用种子须读新值（8100×0.8；提交点单写者轮转）")
}

// TestHotParamSnapshotRotatesAtCommitPoint 钉住 每个 owner 的热参快照必须与常驻消费者在同一个提交点轮转。
// - 新建即有快照且等于解析结果：新发起子调用的私有管理器从它播种，播种源不能等到首次热更之后才出现；
// - 纯数值编辑不加代际，快照与真消费者同代取新值；
// - 回滚经同一条单点把快照一并恢复，下一次子调用播种到编辑前那一代。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotParamSnapshotRotatesAtCommitPoint(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(snapshotRotationYAML("A", 4000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	table := residentCacheForTest(entry)
	require.Contains(t, table, "sub1")
	sub1 := table["sub1"]

	p, ok := sub1.HotSnapshot()
	require.True(t, ok, "New must seed every owner's hot snapshot")
	require.Equal(t, 4000, p.MaxTokens)
	require.InDelta(t, 0.5, p.ThresholdPct, 1e-9)
	require.Equal(t, 2000, sub1.OrgBudgetLine(), "snapshot and resident compressor agree")

	write(snapshotRotationYAML("A", 9000, 0.9))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"), "numeric-only: no structural bump")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"))
	require.Equal(t, 8100, sub1.OrgBudgetLine(), "resident consumer took the new values")
	p, ok = sub1.HotSnapshot()
	require.True(t, ok)
	require.Equal(t, 9000, p.MaxTokens, "snapshot rotated at the SAME commit point (D4)")
	require.InDelta(t, 0.9, p.ThresholdPct, 1e-9)

	entry.Rollback()
	p, _ = sub1.HotSnapshot()
	require.Equal(t, 4000, p.MaxTokens, "rollback restores the seeding source")
	require.Equal(t, 2000, sub1.OrgBudgetLine())
}

// TestSpawnerTTLReachesRealSpawnSpec 钉住 spawn 规格的默认 TTL 现读已提交记录，不取构造期数值。
// - 断言取宿主结果（交给任务层的规格），且必须经由那个一直在服务的工具实例；
// - 只改默认 TTL 落在数值分支：结构代不变，记录轮转后同一实例的下一次 spawn 即取新值；
// - 记录不外溢：模型显式声明的 ttl 优先于记录默认。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestSpawnerTTLReachesRealSpawnSpec(t *testing.T) {
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

	write(seSpawnerTTLYAML(t, "45m"))
	entry.CheckOrgReload()

	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"].(int64),
		"task_default_ttl 变更须落在 numeric-only 分支（不加结构代）")
	require.Equal(t, 45*time.Minute, scHot(t, sub1).TaskDefaultTTL, "记录轴须已轮转到 45m")
	require.Equal(t, 45*time.Minute, at.SpecFromDeclarative(nil, decl).TTL,
		"6.4 spawner 轴锚：同一个 ActionTool 实例（无 push、无重装配）的下一次 spawn 规格必须现读记录新值")

	explicit := decl
	explicit.Params = map[string]string{"ttl": "7"}
	require.Equal(t, 7*time.Second, at.SpecFromDeclarative(nil, explicit).TTL,
		"显式 ttl 参数须优先于记录默认")
}

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

func TestOrgLazyCheck_BuildParkedDoesNotBlockTurnAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "lazy-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload()
	require.EqualValues(t, 0, genOf(ta), "priming is a numeric no-op — no generation yet")

	oldRunner := acquireWithin(t, ta, 2*time.Second)

	park := newBuildPark()
	defer park.disarm()

	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "lazy-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
	during := acquireWithin(t, ta, 2*time.Second)
	park.waitEntered(t)

	require.Same(t, oldRunner, during, "the witnessing turn keeps serving on the old effective")
	require.EqualValues(t, 0, genOf(ta), "nothing publishes while the build is parked")
	later := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, later, "requests started before the publish also keep the old effective")

	park.letGo()
	require.Eventually(t, func() bool { return genOf(ta) == 1 }, 10*time.Second, 20*time.Millisecond,
		"the parked build must publish after release")

	after := acquireWithin(t, ta, 2*time.Second)
	require.NotSame(t, oldRunner, after, "a turn starting after the publish uses the new generation")
}

// TestOrgSyncCheck_WaitsButDoesNotBlockBusinessAcquire 钉住 手动同步检查等构建与发布，但不封住业务获取。
// - 运维驱动的同步检查停在屏障上并持有重载互斥量，返回时机必须是发布落地；
// - 它等待期间业务获取仍必须立刻拿回当前生效的那一代，且代际尚未轮转；
// - 屏障放行后同步检查必须自己返回（有界）、代际加一，此后启动的回合拿新执行器。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgSyncCheck_WaitsButDoesNotBlockBusinessAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "sync-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload()
	require.EqualValues(t, 0, genOf(ta))
	oldRunner := acquireWithin(t, ta, 2*time.Second)

	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "sync-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	done := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(done) }()
	park.waitEntered(t)

	acq := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, acq, "a business request still gets the old effective while ops waits")
	require.EqualValues(t, 0, genOf(ta), "the sync call has not published yet")

	park.letGo()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the synchronous management call never returned after the barrier released")
	}
	require.EqualValues(t, 1, genOf(ta), "the management call published on return")

	after := acquireWithin(t, ta, 2*time.Second)
	require.NotSame(t, oldRunner, after, "turns started after the management call use the new generation")
}

// TestOrgBuildBarrierControl_MuTakersAreGated 钉住 对照组：停在屏障上的构建必须真的持有重载互斥量。
// - 持锁操作（第二次同步检查）要被挡住而超时，无锁的业务获取要照常返回——两条同时成立才说明挡的是锁而不是构建没跑；
// - 屏障不持锁、或业务获取偷偷取锁时，这条与它的对照用例一起变红，套件无法靠"什么都没测到"通过；
// - 放行后被挡的持锁操作必须最终完成，不得永久悬空。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgBuildBarrierControl_MuTakersAreGated(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "ctl-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload()
	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "ctl-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second)
	park.waitEntered(t)

	muTaken := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(muTaken) }()
	select {
	case <-muTaken:
		park.letGo()
		t.Fatal("control: a synchronous (mu-taking) reload returned while a build was parked — " +
			"the barrier is not holding `mu`, so the non-blocking tests would prove nothing")
	case <-time.After(500 * time.Millisecond):
	}

	acquireWithin(t, ta, 2*time.Second)

	park.letGo()
	select {
	case <-muTaken:
	case <-time.After(10 * time.Second):
		t.Fatal("the gated mu-taker never returned after the barrier released")
	}
}

// TestOrgCloseDrainDoesNotBlockBusinessAcquire 钉住 请求获取既不等构建也不等关闭排空。
// - 关闭已发起、其排空还堵在被停住的构建后面等锁时，业务获取仍必须立刻返回；
// - 放行后排空必须拿到锁并走完拆除，关闭有界返回——排空无上界即为缺陷。
// - 关闭由本用例自己发起并观察，因此不注册延迟关闭：延迟的第二次关闭会被 closeOnce 吞成 no-op，排空与返回的时序就观察不到了。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgCloseDrainDoesNotBlockBusinessAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)

	ta.CheckOrgReload()
	park := newBuildPark()
	defer func() { park.disarm(); _ = ta.Close() }()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second)
	park.waitEntered(t)

	closed := make(chan struct{})
	go func() { _ = ta.Close(); close(closed) }()

	acquireWithin(t, ta, 2*time.Second)

	park.letGo()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the parked build released — the drain is unbounded")
	}
}

func retYAML(t testing.TB, basename string, routed ...string) string {
	t.Helper()
	var tools strings.Builder
	for _, r := range routed {
		tools.WriteString(fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r))
	}
	defs := strings.Builder{}
	for _, d := range retAllDefs {
		defs.WriteString(fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: memory\n      path: %q\n", d, d, testStore(t, "retire-"+d)))
	}
	return "entry: main\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    tools:\n" +
		tools.String() + defs.String()
}

// retAllDefs are the names any generation may route (all always defined).
var retAllDefs = []string{"s1", "s2", "s3"}

// twoAgentsOneStore routes `routed` with s1/s2 sharing ONE store path — the shape
// that makes "did retiring one sibling close state the other still uses" observable.
func twoAgentsOneStore(t testing.TB, shared string, routed ...string) string {
	t.Helper()
	var tools strings.Builder
	for _, r := range routed {
		tools.WriteString(fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r))
	}
	defs := fmt.Sprintf("  s1:\n    system_prompt:\n      inline: \"PROMPT-s1\"\n    memory:\n      type: memory\n      path: %q\n"+
		"  s2:\n    system_prompt:\n      inline: \"PROMPT-s2\"\n    memory:\n      type: memory\n      path: %q\n"+
		"  s3:\n    system_prompt:\n      inline: \"PROMPT-s3\"\n    memory:\n      type: memory\n      path: %q\n",
		shared, shared, testStore(t, "retire-s3"))
	return "entry: main\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    tools:\n" +
		tools.String() + defs
}

func residentNamesOf(entry *agent.TagentAgent) []string {
	var out []string
	for n := range residentCacheForTest(entry) {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func buildRetireOrg(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

// TestRetire_RemovedIdleOwnerIsRetired 钉住 移除即停止接受新路由，义务全部收敛后关闭独占组件、释放租约并撤销登记。
// - 无人依赖这个属主时，摘除它的那次发布必须同时把它移出常驻表、关闭并撤销其存储属主登记；
// - 仍被路由的那个实例保持原样，不因别人的移除而改变身份。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_RemovedIdleOwnerIsRetired(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s3"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	owners := residentCacheForTest(entry)
	s1, s3 := owners["s1"], owners["s3"]
	require.NotNil(t, s3, "precondition: s3 is resident at startup")
	require.Contains(t, entry.StoreOwnerSnapshot(), "s3")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()

	require.NotContains(t, entryToolNames(entry), "s3", "the published generation must not route it")
	require.NotContains(t, residentCacheForTest(entry), "s3", "and a drained owner must leave the resident table")
	require.True(t, s3.CloseStarted(), "its exclusive components must be closed")
	require.True(t, s3.CleanerStopped(), "including its maintenance producer")
	require.NotContains(t, entry.StoreOwnerSnapshot(), "s3", "its store-owner registration must be revoked")
	require.Same(t, s1, residentCacheForTest(entry)["s1"], "the surviving owner is untouched")
}

// TestRetire_ObligationHoldsOwnerThenRetiresAtNextBoundary 钉住 安全的一半：仍有在途执行的属主，不被移除它的那次发布退役。
// - 义务消失之后才在下一个边界退役；
// - 提前关闭属于禁止情形，因此这份持有必须可观察，不能静默。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ObligationHoldsOwnerThenRetiresAtNextBoundary(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	require.Equal(t, 1, s2.Obligations().Executions, "precondition: the obligation probe sees the live execution")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()

	require.NotNil(t, residentCacheForTest(entry)["s2"],
		"an owner with a live obligation must stay resident — not retired early")
	require.False(t, s2.CloseStarted(), "and must NOT be closed")
	require.Contains(t, entry.StoreOwnerSnapshot(), "s2", "and keeps its store registration")

	lease.Release()
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()

	require.NotContains(t, residentCacheForTest(entry), "s2", "once the obligation converged it is retired")
	require.True(t, s2.CloseStarted())
	require.NotContains(t, entry.StoreOwnerSnapshot(), "s2")
}

// TestRetire_ReenteredNameReusesOriginalOwner 钉住 同名在其属主仍在排空时回来，必须由那一个实例服务。
// - 同一存储出现第二个属主，就是第二个 writer。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ReenteredNameReusesOriginalOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.False(t, s2.CloseStarted(), "precondition: still draining, not closed")

	write(retYAML(t, "c", "s1", "s2"))
	entry.CheckOrgReload()

	require.Same(t, s2, residentCacheForTest(entry)["s2"],
		"a re-entered name reuses the original owner while it is still live")
	require.False(t, s2.CloseStarted(), "and is never closed under the reuse")
	require.Same(t, s2.MemStore(), residentCacheForTest(entry)["s2"].MemStore(), "same store, not a replacement")
	lease.Release()
	_ = context.Background()
}

// TestRetire_ReentryIntoClosingOwnerIsRefused 钉住 属主已开始关闭时，想要这个名字的候选必须在任何资源建立之前就被拒绝。
// - 复用它已不可能；放入新的则构成同一存储身份的第二个 writer。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ReentryIntoClosingOwnerIsRefused(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	s2.Close()
	t.Cleanup(func() { lease.Release() })

	write(retYAML(t, "c", "s1", "s2"))
	before := entry.OrgDiagnostics()["generation"]
	entry.CheckOrgReload()

	require.NotContains(t, entryToolNames(entry), "s2",
		"a publish that wants a closing name must be refused, not silently served by a new owner")
	require.Same(t, s2, residentCacheForTest(entry)["s2"],
		"the refused candidate must not have replaced the owner with a second writer")
	require.Equal(t, before, entry.OrgDiagnostics()["generation"], "a refusal never advances the published sequence")

	lease.Release()
}

// TestRetire_ReentryAfterFinalExitRebuildsFreshOwner 钉住 被移除的属主最终退出之后，重新加回这个名字必须按恢复协议建新属主，而不是继续拒绝。
// - 释放本身带着排空推进，因此无需额外业务回合即可到达该退出点；
// - 对前属主已彻底消失的名字持续拒绝，等于把拒绝门漏进准入路径。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ReentryAfterFinalExitRebuildsFreshOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.NotNil(t, residentCacheForTest(entry)["s2"],
		"precondition: the in-flight reference keeps the unrouted owner resident")

	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["s2"] == nil
	}, 5*time.Second, 20*time.Millisecond, "final exit reached without any further traffic")

	write(retYAML(t, "c", "s1", "s2"))
	entry.CheckOrgReload()

	require.Contains(t, entryToolNames(entry), "s2",
		"a name whose owner finally exited is admitted again, not refused forever")
	rebuilt := residentCacheForTest(entry)["s2"]
	require.NotNil(t, rebuilt, "the re-added name must be served by a real owner")
	require.NotSame(t, s2, rebuilt, "a finally-exited owner is never resurrected — the exit was real")
}

// TestRetire_NameChurnStaysBounded 钉住 资源必须收敛到当前路由加真实待决义务所需的量，绝不按出现过的名字数收敛。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_NameChurnStaysBounded(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	for round := 0; round < 2; round++ {
		for _, n := range retAllDefs {
			write(retYAML(t, fmt.Sprintf("churn-%d-%s", round, n), "s1", n))
			entry.CheckOrgReload()
		}
	}
	require.Len(t, residentCacheForTest(entry), 3,
		"resident set must equal main + the currently routed names, not every name ever routed (got %v)",
		residentNamesOf(entry))
	require.Len(t, entry.StoreOwnerSnapshot(), 3, "and so must the store-owner registrations")
	debt, ok := entry.OrgDiagnostics()["liveDebt"].(OrgLiveDebt)
	require.True(t, ok, "the live debt group must always be present")
	require.Empty(t, debt.PendingRetirements,
		"nothing is left draining once every retired owner converged")
}

// TestRetire_SharedStoreSurvivesSiblingRetirement 钉住 两个属主共用一个存储时，退役其一不得在另一属主之下关掉共享状态。
// - 存储由租约记账决定何时消失，不由某个借用者的退出决定；
// - 幸存属主仍从同一存储对象服务：没被关掉、维护生产者仍在跑、仍被路由。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_SharedStoreSurvivesSiblingRetirement(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	shared := testStore(t, "retire-shared")
	write(twoAgentsOneStore(t, shared, "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	owners := residentCacheForTest(entry)
	s1, s2 := owners["s1"], owners["s2"]
	require.Same(t, s1.MemStore(), s2.MemStore(), "precondition: the two owners really share one store")

	write(twoAgentsOneStore(t, shared, "s1"))
	entry.CheckOrgReload()
	require.True(t, s2.CloseStarted(), "the unrouted sibling retired")
	require.NotContains(t, residentCacheForTest(entry), "s2")

	require.Same(t, s1.MemStore(), residentCacheForTest(entry)["s1"].MemStore())
	require.False(t, s1.CloseStarted(), "the surviving owner must not be closed by its sibling's retirement")
	require.False(t, s1.CleanerStopped(), "and its producers stay up")
	require.Contains(t, entryToolNames(entry), "s1", "and it is still routed")
}

// TestRetire_RollbackRetiresDroppedOwner 钉住 回滚也是一次发布，它丢掉的属主走同一条排水。
// - 回滚环里保存的配置是数据，从来不是留住一个在跑实例的理由；
// - 完全关闭的属主不被复活——名字获得一个全新属主，且不是那个已关实例。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_RollbackRetiresDroppedOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.NotContains(t, residentCacheForTest(entry), "s2", "precondition: G2 already retired it")

	entry.Rollback()
	require.Contains(t, entryToolNames(entry), "s2", "the rollback really re-routed s2")
	fresh := residentCacheForTest(entry)["s2"]
	require.NotNil(t, fresh, "a fully-closed owner is not resurrected; the name gets a fresh owner")
	require.NotSame(t, s2, fresh, "and it is NOT the closed instance")
	require.False(t, fresh.CloseStarted(), "the fresh owner serves")
}

// TestOrgHotAdd_VisibleOnlyAtCommit 钉住 暂存的新增属主在唯一提交点之前对读者不可见，提交时一次性可见。
// - 观察点必须落在提交临界区内：属主已建成、合并未发生。只在提交完成后断言可见，对"构建在途不得出现半提交拓扑"这一半是空洞的；
// - 此刻常驻表取不到它、现效面不 offer 它、发布序号不前进；释放后三者同时成立且序号恰好推进一次。
// - 刻意的例外是存储属主登记：它在构建期取得，被拒候选的回退要能撤销本候选登记过的每一个属主，把登记也说成不可见就描述了另一种设计。
// 契约: docs/wiki/platform/org-hot-reload.md#staged-add-visibility
func TestOrgHotAdd_VisibleOnlyAtCommit(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	require.NotContains(t, residentCacheForTest(entry), "s2",
		"precondition: s2 is defined in the file but unrouted, so it is not resident")
	genBefore := entry.OrgDiagnostics()["generation"]

	entered := make(chan struct{})
	release := make(chan struct{})
	var enter sync.Once
	park := func() {
		enter.Do(func() { close(entered) })
		<-release
	}
	orgCommitBarrier.Store(&park)
	t.Cleanup(func() { orgCommitBarrier.Store(nil) })

	write(retYAML(t, "b", "s1", "s2"))
	done := make(chan struct{})
	go func() {
		defer close(done)
		entry.CheckOrgReload()
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("the reload never reached the commit point — the witness below would be vacuous")
	}

	table := residentCacheForTest(entry)
	require.NotContains(t, table, "s2",
		"a staged add must not be resident while the candidate is still uncommitted")
	require.NotContains(t, entryToolNames(entry), "s2",
		"and must not be offered on the face that is still in force")
	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"the published sequence advances only at the commit")

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the released reload never completed")
	}

	require.NotNil(t, residentCacheForTest(entry)["s2"], "after the commit the owner is merged")
	require.Contains(t, entryToolNames(entry), "s2", "and the new generation offers it")
	require.Greater(t, entry.OrgDiagnostics()["generation"], genBefore, "with exactly one publish")
}

// retDiamondYAML renders the diamond: main → {s1, s2}, and BOTH of them route the
// shared leaf s3. `routeS1/routeS2` switch the entry's routes, which is how one
// generation can drop a branch while the other branch stays in force.
func retDiamondYAML(t testing.TB, routeS1, routeS2 bool) string {
	t.Helper()
	var mainTools string
	if routeS1 {
		mainTools += `      - kind: agent
        agent: s1
        description: "s1"
`
	}
	if routeS2 {
		mainTools += `      - kind: agent
        agent: s2
        description: "s2"
`
	}
	branch := func(name string) string {
		return fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: memory\n      path: %q\n    tools:\n      - kind: agent\n        agent: s3\n        description: \"s3\"\n",
			name, name, testStore(t, "retire-diamond-"+name))
	}
	return "entry: main\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n      path: " +
		fmt.Sprintf("%q\n    tools:\n%s", testStore(t, "retire-diamond-main"), mainTools) +
		branch("s1") + branch("s2") +
		fmt.Sprintf("  s3:\n    system_prompt:\n      inline: \"PROMPT-s3\"\n    memory:\n      type: memory\n      path: %q\n", testStore(t, "retire-diamond-s3"))
}

// TestRetire_DiamondSharedDependencyWaitsForAllBorrowers 钉住 菱形共享依赖在退役时等所有借用者，形状为 main→{s1,s2}→s3。
// - 无待决义务的分支 s1 收敛，共享叶子 s3 不因自己无活就退役——它仍在 s2 存活代的可调用闭包内、合法可委派；
// - 最后借用者排空后 s3 自行退出、不需额外业务回合，级联 s2 再 s3 在同一次排空内收敛。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_DiamondSharedDependencyWaitsForAllBorrowers(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retDiamondYAML(t, true, true))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	owners := residentCacheForTest(entry)
	s1, s2, s3 := owners["s1"], owners["s2"], owners["s3"]
	require.NotNil(t, s3, "precondition: the shared leaf is resident")
	require.NotNil(t, s1.ContextManager().SubagentWrapper("s3"), "both branches route s3…")
	require.NotNil(t, s2.ContextManager().SubagentWrapper("s3"), "…from their own faces")

	inFlight := s2.ContextManager().AcquireLease(agent.LeaseSubCall)

	write(retDiamondYAML(t, false, false))
	entry.CheckOrgReload()

	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["s1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"没有引用也没有保有的分支（s1）应收敛")

	require.NotNil(t, residentCacheForTest(entry)["s2"], "在途执行所在的分支不得被提前关掉")
	require.NotNil(t, residentCacheForTest(entry)["s3"],
		"§4.3 菱形锚：共享叶子被别的存活代保有时不得退役——它自己没有义务不代表可关")
	require.False(t, s3.CloseStarted(), "and its close must not even have begun")

	inFlight.Release()
	require.Eventually(t, func() bool {
		o := residentCacheForTest(entry)
		return o["s2"] == nil && o["s3"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"最后借用者退出后，s2 与共享的 s3 须在同一个排空里依次收敛（不等待新 turn）")
}

// sealThePath produces a REAL sealed store rather than a mocked error: it drives the
// unconfirmed-reclaim rule, so a live single-writer flock genuinely sits on the path and
// any later opener of it collides with a possibly-half-live backend instead of succeeding.
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
func sealThePath(t *testing.T, rr *resources.RuntimeResources, path string) {
	t.Helper()
	fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, _, err := rr.Acquire("localfile", path, fp, func() (resources.OpenedResource, error) {
		return resources.OpenedResource{}, fmt.Errorf("%w: kv close hung", resources.ErrReclaimUnconfirmed)
	})
	require.ErrorIs(t, err, resources.ErrReclaimUnconfirmed, "precondition: the seal must come from the real reclaim rule")
}

// assertPathStillSealed checks the seal WITHOUT touching the writer lock: an
// flock probe in the same process would take/convert the lock and release it on
// close (macOS flock semantics — measured: a second `-count` iteration then found
// the path free), so the seal is verified through the registry's own rule instead:
// a re-acquire on a sealed path must be refused with resources.ErrResourcePoisoned without
// ever running open(). A second writer therefore cannot exist, because the single
// writer slot was never handed out.
func assertPathStillSealed(t *testing.T, rr *resources.RuntimeResources, path string) {
	t.Helper()
	fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, _, err := rr.Acquire("localfile", path, fp, func() (resources.OpenedResource, error) {
		t.Error("open must NOT run again on a sealed path")
		return resources.OpenedResource{}, nil
	})
	require.ErrorIs(t, err, resources.ErrResourcePoisoned, "seal must persist — poisoned paths are never auto-unsealed")
}

func sdPoisonYAML(t testing.TB, routed []string, sealed string) string {
	t.Helper()
	var tools string
	for _, r := range routed {
		tools += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r)
	}
	sub1 := fmt.Sprintf("  sub1:\n    system_prompt:\n      inline: \"PROMPT-sub1\"\n    memory:\n      type: localfile\n      path: %q\n", testStore(t, "sd-poison-sub1"))
	sub2 := fmt.Sprintf("  sub2:\n    system_prompt:\n      inline: \"PROMPT-sub2\"\n    memory:\n      type: localfile\n      path: %q\n", sealed)
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n    tools:\n" +
		tools + sub1 + sub2
}

// TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing 钉住 热新增的存储路径被活写者封住时，必须在任何候选发布之前被拒。
// - 当前代必须完整照常服务、序号不前进；
// - 拒绝不得静默过期：poisoned 路径后续热更也不自动解封。
// - 封住动作放在自己的登记表里：它是该路径单写锁的另一个活持有者，用同进程构造出跨进程争用的形状。
// - 正向证据（lastFailure 具名到被拒的那次热新增）不可省：若换代根本没走到热新增分支，上面每条断言都会空洞地通过。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	sealed := filepath.Join(dir, "sealed-store")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	sealer := resources.NewRuntimeResources()
	sealThePath(t, sealer, sealed)
	t.Cleanup(func() { assertPathStillSealed(t, sealer, sealed) })

	write(sdPoisonYAML(t, []string{"sub1"}, sealed))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	residentBefore := residentCacheForTest(entry)
	sub1Before := residentBefore["sub1"]
	require.NotNil(t, sub1Before, "baseline: sub1 serves")
	genBefore := entry.OrgDiagnostics()["generation"]

	write(sdPoisonYAML(t, []string{"sub1", "sub2"}, sealed))
	entry.CheckOrgReload()

	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"§4.3：poisoned 路径上的热增必须被拒绝，绝不发布半相候选")
	require.Nil(t, residentCacheForTest(entry)["sub2"],
		"被封路径上不得出现第二个写者 owner")
	require.NotContains(t, entryToolNames(entry), "sub2", "旧代的工具面不得被部分改写")
	require.Same(t, sub1Before, residentCacheForTest(entry)["sub1"],
		"沿用旧配置＝同一存活 owner，不是重建的替身")
	require.Same(t, sub1Before.MemStore(), residentCacheForTest(entry)["sub1"].MemStore(),
		"存活 owner 的存储身份不漂移")
	require.False(t, sub1Before.CloseStarted(), "旧代 owner 不能被失败的回退牵连关闭")
	assertPathStillSealed(t, sealer, sealed)

	lf, ok := entry.OrgDiagnostics()["lastFailure"]
	require.True(t, ok && lf != nil, "被拒的热增必须留下可见失败记录（不是静默无操作）")
	require.Contains(t, fmt.Sprint(lf), "sub2", "记录须点出被封的那个名字")
	require.Contains(t, fmt.Sprint(lf), sealed, "and name the path it collided with")

	write(sdPoisonYAML(t, []string{"sub1", "sub2"}, sealed))
	entry.CheckOrgReload()
	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"poisoned 规则不因后续热更自动解封")
	require.Nil(t, residentCacheForTest(entry)["sub2"])
}

// closeOwnerYAML renders entry "main" delegating to `targets`. sub1/sub2 are
// always DEFINED — routability is the topology's truth source, not the file —
// and each keeps its own store so owner identity is observable per agent.
func closeOwnerYAML(t testing.TB, targets ...string) string {
	t.Helper()
	return ownerYAML(t, targets, fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-close-sub2")))
}

// buildCloseOrg builds the org WITHOUT the usual t.Cleanup Close: these tests
// drive Close themselves and assert on what it left behind.
func buildCloseOrg(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

// TestOrgClose_CoversEveryResidentOwner 钉住 组织最终关闭必须抵达它建出的每一个属主，而不只是交回的入口。
// - 入口 Close 返回后，每个冷建属主都要报自己的关闭、维护协程已返回、装配不留任何存储属主登记；
// - 见证取属主自身状态与装配登记表——只关了入口的清扫满足不了它们。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_CoversEveryResidentOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1", "sub2"))
	entry := buildCloseOrg(t, yamlPath)

	owners := residentCacheForTest(entry)
	require.NotNil(t, owners["sub1"], "precondition: sub1 is a resident owner")
	require.NotNil(t, owners["sub2"], "precondition: sub2 is a resident owner")
	before := entry.StoreOwnerSnapshot()
	require.Contains(t, before, "sub2", "precondition: every owner registered its store before close")

	require.NoError(t, entry.Close())

	for name, owner := range owners {
		if name == "main" {
			continue
		}
		require.True(t, owner.CloseStarted(),
			"resident owner %q was never closed by the organization Close", name)
		require.True(t, owner.CleanerStopped(),
			"owner %q left its workspace cleaner goroutine running", name)
	}
	require.Empty(t, entry.StoreOwnerSnapshot(),
		"a closed organization must leave no store-owner registration behind")
}

// TestOrgClose_CoversHotAddedOwner 钉住 经热路径加入的属主与冷建的一样被组织拥有，同一次 Close 必须抵达它。
// - 待关清单在关闭时读取、不在启动时定格，这正是全部差别；
// - 热新增属主的关闭须已发起、维护生产者须已停、其存储属主登记须已撤销。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_CoversHotAddedOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1"))
	entry := buildCloseOrg(t, yamlPath)

	write(closeOwnerYAML(t, "sub1", "sub2"))
	entry.CheckOrgReload()
	added := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, added, "precondition: sub2 became resident through the hot path")
	require.Contains(t, entry.StoreOwnerSnapshot(), "sub2",
		"precondition: the hot-added owner registered its own store")

	require.NoError(t, entry.Close())
	require.True(t, added.CloseStarted(),
		"an owner the org adopted after startup must still be closed by its Close")
	require.True(t, added.CleanerStopped(), "and its maintenance producer must have stopped")
	require.NotContains(t, entry.StoreOwnerSnapshot(), "sub2",
		"and its registration revoked")
}

// TestOrgClose_DoesNotReplaceOwners 钉住 关闭按名字取出的常驻属主，必须就是测试先前捕获的那些实例本身。
// - 二次 Close（普通 t.Cleanup 跟进）必须幂等——属主自身序列恰好跑一次、不得复活；
// - 清扫若是重建 agent 而非关闭它们，会在此暴露为假通过。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_DoesNotReplaceOwners(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1", "sub2"))
	entry := buildCloseOrg(t, yamlPath)

	sub1 := residentCacheForTest(entry)["sub1"]
	require.NoError(t, entry.Close())
	require.True(t, sub1.CloseStarted())
	require.NoError(t, entry.Close())
	require.Same(t, sub1, residentCacheForTest(entry)["sub1"])
}

// TestOrgClose_SharedStoreWaitsForEveryBorrower 钉住 关闭序列对共享一个存储的两个存活属主，恰好交还一次写者槽。
// - 后端下沉期间任何属主都不得报错，路径最终干净释放——泄漏的租约持有 flock，过早或重复释放会封住路径，二者都让下面的重开失败；
// - 恰一次释放经登记表自身规则观察，非破坏性，不用会接管所测锁的 flock 探针。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_SharedStoreWaitsForEveryBorrower(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	shared := testStore(t, "orgclose-shared-store")
	write(twoAgentsOneStore(t, shared, "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)

	owners := residentCacheForTest(entry)
	s1, s2 := owners["s1"], owners["s2"]
	require.NotNil(t, s1)
	require.NotNil(t, s2)
	require.Same(t, s1.MemStore(), s2.MemStore(), "precondition: both owners borrow the same store")

	require.NoError(t, entry.Close(),
		"§4.3/D8：共享 store 必须等所有借用者退出——关闭序列中任一 owner 报错都说明后端被提前拆走")
	require.True(t, s1.CloseStarted() && s2.CloseStarted(), "both borrowers must have gone down")

	fresh := resources.NewRuntimeResources()
	fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: shared})
	_, _, rel, err := fresh.Acquire("localfile", shared, fp, func() (resources.OpenedResource, error) {
		return resources.OpenedResource{Store: noopStoreForTakeover{}}, nil
	})
	require.NoError(t, err,
		"关闭后共享路径必须能被新世代干净接手（被持有＝租约泄漏；resources.ErrResourcePoisoned＝提前或重复释放被封路）")
	require.NoError(t, rel())
}

// TestRetire_SharedComponentWaitsForEveryBorrower 钉住 借用者仍活着时共享后端不得被拆，这一半在别处不可观察、在这里才可证。
// - 新代注册表试图接手该路径时必须撞上幸存者的活写者锁，失败的 LOCK_EX 不打扰持有者故非破坏；
// - 只有最后一个借用者退出后，新代才可接手。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_SharedComponentWaitsForEveryBorrower(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	shared := testStore(t, "retire-wait-borrowers")
	write(twoAgentsOneStore(t, shared, "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	t.Cleanup(func() { _ = entry.Close() })

	owners := residentCacheForTest(entry)
	s1, s2 := owners["s1"], owners["s2"]
	require.Same(t, s1.MemStore(), s2.MemStore(), "precondition: both owners borrow one store")

	write(twoAgentsOneStore(t, shared, "s1"))
	entry.CheckOrgReload()
	require.True(t, s2.CloseStarted(), "precondition: s2 retired")
	require.False(t, s1.CloseStarted(), "and s1 still borrows it")

	fresh := resources.NewRuntimeResources()
	fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: shared})
	_, _, _, err := fresh.Acquire("localfile", shared, fp, func() (resources.OpenedResource, error) {
		t.Error("a second writer must not be opened while a borrower is alive")
		return resources.OpenedResource{}, nil
	})
	require.ErrorIs(t, err, resources.ErrStoreLocked,
		"§4.3/D8：仍有借用者时共享后端不得拆除（写锁必须还被存活者持有）")
	require.NotErrorIs(t, err, resources.ErrResourcePoisoned, "「仍被持有」不同于「回收未确认被封路」")

	require.NoError(t, entry.Close())
	_, _, rel, err2 := fresh.Acquire("localfile", shared, fp, func() (resources.OpenedResource, error) {
		return resources.OpenedResource{Store: noopStoreForTakeover{}}, nil
	})
	require.NoError(t, err2, "最后借用者退出后路径必须干净交接")
	require.NoError(t, rel())
}

// sdUsageYAML renders the G1/G2 pair for the deferred-delegation anchor: G1
// routes main → sub1, G2 drops that route (making sub1 unrouted).
//
// The rendered config carries no model/providers section on purpose: the host-injected
// mock serves every agent, which is what lets a real delegation turn run without a
// live endpoint.
func sdUsageYAML(t testing.TB, routeSub1 bool) string {
	t.Helper()
	ref := ""
	if routeSub1 {
		ref = `      - kind: agent
        agent: sub1
        description: "sub1"
`
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
%s  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
`, ref, testStore(t, "hottest-sd-usage"))
}

// TestSD_DeferredDelegationIsProtectedByUsageRight 钉住 没被调用过的子代理不得因本体度量空闲，就在别一代仍路由它时被关闭。
// - 使用权由存活绑定各自已发布的面派生，不是第二套任务域、也不是平行路由表——那面已是唯一路由真源；
// - 一代仍是合法调用方直到其自身引用排空，被推迟的委派那时必须还能落到被保有的子代理。
// - 注入的 mock 对无工具的 agent 直接给最终答复，因此 sub1 内的真实委派回合会自行收尾，而不是伸手要活的 endpoint。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestSD_DeferredDelegationIsProtectedByUsageRight(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdUsageYAML(t, true))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&delegModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	main := residentCacheForTest(entry)["main"]
	require.NotNil(t, main, "G1：main 常驻")
	require.NotNil(t, residentCacheForTest(entry)["sub1"], "G1：sub1 常驻")

	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	g1Wrapper := lease.SubagentWrapper("sub1")
	require.NotNil(t, g1Wrapper, "G1 自己的面上必须解析得出 sub1（同一版本真源）")

	write(sdUsageYAML(t, false))
	entry.CheckOrgReload()

	require.Nil(t, entry.ContextManager().SubagentWrapper("sub1"),
		"前提：现效代确实不再路由 sub1")
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"§3.2 红锚：G1 仍保有 sub1 使用权（尚未调用也受保护），sweep 不得提前退役")

	_, err = g1Wrapper.Call(context.Background(), []byte(`{"request":"deferred call from G1"}`))
	require.NoError(t, err, "G1 在 G2 删除路由之后真调 sub1 仍须成功")

	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["sub1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"使用权释放后 sub1 须有界退役（不永久保有）")
}

// sdOneRouteYAML renders entry main with exactly one routed sub-agent name.
func sdOneRouteYAML(t testing.TB, routed string, store string) string {
	t.Helper()
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: %s
        description: %q
  %s:
    system_prompt:
      inline: %q
    memory:
      type: localfile
      path: %q
`, routed, routed, routed, routed, store)
}

// TestSD_ReleaseContinuesRetirementWithoutAnotherTurn 钉住 仅由使用权保有的待退役被释放本身解除阻塞，无新业务回合即退出。
// - 最后一个引用排空后属主须自行有界退出——若装配等下一个业务回合才察觉，空闲组织会把本已可关的属主无限期常驻；
// - 这正是"不可见的持有"要消除的对象。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestSD_ReleaseContinuesRetirementWithoutAnotherTurn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := testStore(t, "hottest-sd-poke")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdOneRouteYAML(t, "sub1", store))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&delegModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	main := residentCacheForTest(entry)["main"]
	require.NotNil(t, main)

	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	write(sdOneRouteYAML(t, "sub2", store))
	entry.CheckOrgReload()
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"前提：G1 持有使用权时 sub1 不得退役")

	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["sub1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"§4.3：使用权释放本身须续排退役，不得依赖再来的业务 turn")
}

// deshellYAML renders the de-shell fixture (S-A/2.3): entry main delegating to
// sub1+sub2, each with an OWN model so a per-agent model change is a structural
// fingerprint delta (models move the fingerprint; prompts/memory here stay
// byte-identical unless the case says otherwise).
func deshellYAML(t testing.TB, mainModel, sub1Model, sub2Model string, sub3 bool) string {
	t.Helper()
	sub3Tool := ""
	sub3Block := ""
	if sub3 {
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

// TestDeshell_EntryRegenerationConstructsZeroAgents 钉住 只改入口（换模型、兄弟逐字节相同）的热重载构造零个 agent。
// - 每次发布造一整只壳（总线/TaskManager/cleaner）再丢弃，正是去壳要消灭的代价；
// - 判别按对象寿命而非组织级计数：改既有 agent 须经面再生，不构造壳。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_EntryRegenerationConstructsZeroAgents(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A: modifying an existing agent must regenerate through the face, not construct shells")
}

// TestDeshell_ChangedSubAgentConstructsOneTransitional 钉住 改一个子代理（入口未动）经暂存面在稳定常驻实例上推进，构造零个 agent。
// - 暂存执行载体已并入常驻实例，构造数恒为零。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_ChangedSubAgentConstructsOneTransitional(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1x", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-D terminal: a changed sub-agent advances through its staged face on the stable resident instance — the transitional carrier is gone")
}

// TestDeshell_HotAddConstructsExactlyTheNewAgent 钉住 热新增完整构造那一个新 agent（恰好一个），去壳后不得围绕它重造入口与未变兄弟。
// - 去壳不等于去能力：新 agent 走完整常驻构造，入口与逐字节相同的兄弟不得再构造。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_HotAddConstructsExactlyTheNewAgent(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1", "sub-m2", true)
	require.Equal(t, int64(1), agent.TagentAgentsConstructed()-before,
		"J2: hot-add constructs exactly the new full agent; entry/unchanged siblings must not re-construct")
}

// TestDeshell_RollbackConstructsZeroForEntryOnlyChange 钉住 回滚一次仅入口的结构变更构造零个 agent，与正向热更同价。
// - 入口级回滚须经面再生，不构造壳。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_RollbackConstructsZeroForEntryOnlyChange(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	entry.Rollback()
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A rollback: entry-only rollback must regenerate through the face, not shells")
}

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

// drillResetManagedUnits is the operator-side orchestration: probe every gate FIRST
// (all-or-nothing), then remove only managed-layout files.
//
// - Gate one is live writers: the cross-process single-writer flock must be acquirable, so an in-flight owner is refused with zero changes.
// - Gate two is the leaf guarded ledger: probe.CloseDurable is verification only, and the sweep runs on a fresh instance so its unacked ledger matches the post-removal disk.
// - Removal covers the exact managed layout only: per-partition kv-*.json snapshots plus the single-file shape and their tmp residue; envelope-style tmps live under the inbox unit.
func drillResetManagedUnits(storeDir, spillParent, anchorDir, agentName string, confirm bool) ([]string, error) {
	if !confirm {
		return nil, fmt.Errorf("drill reset: requires explicit confirmation (destructive operator act)")
	}
	lockF, err := resources.AcquireDirLock(storeDir)
	if err != nil {
		return nil, fmt.Errorf("drill reset: live writer on %s: %w", storeDir, err)
	}
	defer func() { _ = resources.UnlockDirLock(lockF) }()

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
	_ = probe.CloseDurable()

	var removals []string
	for _, pat := range []string{"kv.json", "kv.json.tmp", "kv-*.json", "kv-*.json.tmp"} {
		m, _ := filepath.Glob(filepath.Join(storeDir, pat))
		removals = append(removals, m...)
	}
	live, _ := filepath.Glob(filepath.Join(spillParent, agentName, "inbox-v2", "*.json"))
	removals = append(removals, live...)
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

// TestDrill_ManagedRootReset_ConsistentUnitAndAllRefusals 钉住 托管根单元复位：任一 gate 不过就零改动拒绝，只清托管布局。
// - 拒绝即零改动：quarantine 未处置与活写者持锁（resources.ErrStoreLocked）都不得留下部分清理；非托管内容与软链的外部目标永不被删。
// - 复位后的启动相由独立进程完成（一次 boot 只有真实进程启动才算证据），该子进程不设任何竞态豁免：出现竞态或非零退出即硬失败并附全日志。
func TestDrill_ManagedRootReset_ConsistentUnitAndAllRefusals(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	spillDir := filepath.Join(root, "spill")
	anchorDir := filepath.Join(root, "anchor")
	require.NoError(t, os.MkdirAll(storeDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine"), 0o755))
	require.NoError(t, os.MkdirAll(anchorDir, 0o755))

	legacyV1 := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	legacySpill := filepath.Join(spillDir, "tagent", "job.spill")
	require.NoError(t, os.WriteFile(legacySpill, []byte("spill"), 0o644))
	victim := filepath.Join(root, "outside-victim.json")
	require.NoError(t, os.WriteFile(victim, []byte("DO-NOT-DELETE"), 0o644))
	require.NoError(t, os.Symlink(victim, filepath.Join(spillDir, "tagent", "inbox-v1", "escape.json")))

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

	unmanaged := filepath.Join(storeDir, "user-notes.txt")
	require.NoError(t, os.WriteFile(unmanaged, []byte("mine"), 0o644))
	evidence := filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine", "evidence-1.json")
	require.NoError(t, os.WriteFile(evidence, []byte(`{"corrupt":true}`), 0o644))

	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", false)
	require.Error(t, err)
	require.FileExists(t, legacyV1)

	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.Error(t, err, "undispositioned quarantine must refuse the unit reset")
	require.FileExists(t, evidence, "quarantine evidence files are NEVER deleted by the reset")
	require.FileExists(t, legacyV1, "refusal means ZERO changes")
	require.NotEmpty(t, kvSnapshotsIn(storeDir), "refusal means ZERO changes (a partition snapshot is on disk)")

	require.NoError(t, os.Rename(evidence, filepath.Join(root, "dispositioned-1.json")))

	held, err := resources.AcquireDirLock(storeDir)
	require.NoError(t, err)
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.ErrorIs(t, err, resources.ErrStoreLocked, "a live writer must be refused")
	require.FileExists(t, legacyV1)
	require.NoError(t, resources.UnlockDirLock(held))

	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "transitional v1 cleared")
	require.NoFileExists(t, legacySpill, "legacy spill cleared")
	require.Empty(t, kvSnapshotsIn(storeDir), "store unit cleared — consistency (every kv-*.json gone)")
	require.NoFileExists(t, filepath.Join(anchorDir, "tagent.json"), "anchor unit cleared")
	liveLeft, _ := filepath.Glob(filepath.Join(spillDir, "tagent", "inbox-v2", "*.json"))
	require.Empty(t, liveLeft, "live envelopes cleared with the store (no unit half-reset)")
	require.FileExists(t, unmanaged, "unmanaged content is never removed")
	require.FileExists(t, victim, "path escape removed at most the link, never the outside target")
	require.DirExists(t, filepath.Dir(evidence))

	runBootChild(t, append(os.Environ(),
		"TAGENT_DRILL_STORE="+storeDir,
		"TAGENT_DRILL_SPILL="+spillDir,
		"TAGENT_DRILL_ANCHOR="+anchorDir),
		"TAGENT_DRILL_PHASE=boot-turn", "TestDrill_ManagedRootResetBootChild$")
}

// TestDrill_ManagedRootResetBootChild 钉住 单元复位之后的启动相只认当前格式的路径，并能完成一个真实回合。
// - 判据按包含而非相等：框架守卫可能在用户输入上添加装饰。
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
					return true
				}
			}
		}
		return false
	}, 15*time.Second, 20*time.Millisecond, "post-reset boot must reach the model on the CURRENT path")
	require.NoError(t, ta.Close())
	<-drop
}

// kvSnapshotsIn lists the KV store unit's snapshot files (partition layout:
// kv-<label>.json; the single-file kv.json is counted for the ZERO-change proof).
func kvSnapshotsIn(storeDir string) []string {
	partitions, _ := filepath.Glob(filepath.Join(storeDir, "kv-*.json"))
	legacy, _ := filepath.Glob(filepath.Join(storeDir, "kv.json"))
	return append(partitions, legacy...)
}

func findCollisionPair(t *testing.T) (string, string) {
	t.Helper()
	seen := make(map[int]string)
	for i := 0; i < 1_000_000; i++ {
		name := fmt.Sprintf("agent-%d", i)
		pid := memory.PartitionIDFromName(name)
		if prev, dup := seen[pid]; dup {
			return prev, name
		}
		seen[pid] = name
	}
	t.Fatal("no collision pair found in 1M names — impossible for 10-bit hash")
	return "", ""
}

// TestPartitionCollision_FailsClosed：共享同一持久 store 的两个 agent 名哈希到同一 pid → 构造期 fail-closed，绝不静默合并记忆命名空间，绝不自动迁移历史。
// - nameB 必须经引用被递归构建，它的 store owner 才会登记，"同一 store 上的 pid 冲突"这一前提方成立。
//
// 契约: docs/wiki/platform/org-hot-reload.md#memory-preflight
func TestPartitionCollision_FailsClosed(t *testing.T) {
	nameA, nameB := findCollisionPair(t)
	dir := t.TempDir()

	cfg := Config{
		Entry: nameA,
		Providers: map[string]ProviderConfig{
			"openai": {APIEndpoint: "http://localhost:1"},
		},
		Agents: map[string]AgentConfig{
			nameA: {
				SystemPrompt: PromptConfig{Inline: "a"},
				Memory:       MemoryConfig{Type: "localfile", Path: dir},
				Tools: []ToolRef{
					{Kind: "agent", AgentID: nameB, Description: "b"},
				},
			},
			nameB: {
				SystemPrompt: PromptConfig{Inline: "b"},
				Memory:       MemoryConfig{Type: "localfile", Path: dir},
			},
		},
	}
	_, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.Error(t, err, "same-store pid collision must fail closed")
	require.Contains(t, err.Error(), "partition id collision")
	require.Contains(t, err.Error(), nameA)
	require.Contains(t, err.Error(), nameB)
}

// TestPartitionCollision_IsolatedStoresNoFalsePositive：同 pid 但各自隔离 store（空 path）互不影响 → 构造成功，守卫不得误报。
func TestPartitionCollision_IsolatedStoresNoFalsePositive(t *testing.T) {
	nameA, nameB := findCollisionPair(t)

	cfg := Config{
		Entry: nameA,
		Providers: map[string]ProviderConfig{
			"openai": {APIEndpoint: "http://localhost:1"},
		},
		Agents: map[string]AgentConfig{
			nameA: {SystemPrompt: PromptConfig{Inline: "a"}},
			nameB: {SystemPrompt: PromptConfig{Inline: "b"}},
		},
	}
	ta, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	require.NoError(t, ta.Close())
}

var _ model.Model = (*stubModel)(nil)

// TestRemoteDeclarationOnlyKeepsTheGate pins that the remote-declaration-only skip applies only where no owner can ever exist.
// - The gate exists to refuse a genuinely missing definition: a name reached non-remotely as well must still be refused.
// - A locally defined name is never declaration-only, and a remote block without a URL must not be skipped here.
func TestRemoteDeclarationOnlyKeepsTheGate(t *testing.T) {
	remoteRef := ToolRef{Kind: ToolKindAgent, AgentID: "ghost", Remote: &RemoteConfig{URL: "http://127.0.0.1:1"}}
	localRef := ToolRef{Kind: ToolKindAgent, AgentID: "ghost"}

	onlyRemote := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a": {Tools: []ToolRef{remoteRef}},
	}}
	require.True(t, config.RemoteDeclarationOnly(onlyRemote, "ghost"),
		"a name reached solely as a remote reference has no owner to build")

	mixed := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a": {Tools: []ToolRef{remoteRef}},
		"b": {Tools: []ToolRef{localRef}},
	}}
	require.False(t, config.RemoteDeclarationOnly(mixed, "ghost"),
		"a non-remote reference to the same name needs a real owner: the gate must still fail closed")

	defined := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a":     {Tools: []ToolRef{remoteRef}},
		"ghost": {},
	}}
	require.False(t, config.RemoteDeclarationOnly(defined, "ghost"),
		"a locally defined agent is never treated as declaration-only")

	blankURL := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a": {Tools: []ToolRef{{Kind: ToolKindAgent, AgentID: "ghost", Remote: &RemoteConfig{}}}},
	}}
	require.False(t, config.RemoteDeclarationOnly(blankURL, "ghost"),
		"a remote block without a URL is the mismatch §5.44 refuses at validation — it must not be skipped here either")
}

// TestBuildAgent_ReadPartitionsIncludeOwnNamespace pins that a built agent read scope always carries its OWN namespace partition first.
func TestBuildAgent_ReadPartitionsIncludeOwnNamespace(t *testing.T) {
	var captured agent.PlainToolFactoryConfig
	agent.RegisterPlainTool("test_read_partitions", func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		captured = cfg
		return &mockCallableTool{name: cfg.ID}, nil
	})

	own := memory.PartitionIDFromName("tagent")
	crossA := memory.PartitionIDFromName("recall")
	crossB := memory.PartitionIDFromName("knowledge")

	cases := []struct {
		name           string
		agentName      string
		readNamespaces []string
		wantPartitions []int
	}{
		{
			name:           "no read_namespaces still reads own timeline",
			agentName:      "tagent",
			readNamespaces: nil,
			wantPartitions: []int{own},
		},
		{
			name:           "read_namespaces appended after own",
			agentName:      "tagent",
			readNamespaces: []string{"recall", "knowledge"},
			wantPartitions: []int{own, crossA, crossB},
		},
		{
			name:           "own namespace listed in read_namespaces is deduped",
			agentName:      "tagent",
			readNamespaces: []string{"tagent", "recall"},
			wantPartitions: []int{own, crossA},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Agents: map[string]AgentConfig{
					tc.agentName: {
						SystemPrompt: PromptConfig{Inline: "prompt"},
						Memory:       MemoryConfig{Type: "memory", ReadNamespaces: tc.readNamespaces},
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "test_read_partitions"},
						},
					},
				},
			}
			rc := &runtimeConfig{model: &factoryMockModel{}}
			loader := prompt.NewLoader("")
			cache := make(map[string]*agent.TagentAgent)

			_, err := buildAgent(tc.agentName, cfg.Agents[tc.agentName], cfg, rc, loader, cache, buildModeResident)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPartitions, captured.ReadPartitionIDs,
				"read partitions must always include the agent's own namespace first")
		})
	}
}

// restartYAML renders the l3 shape plus the restart-only declaration blocks, so a
// mixed edit (hot-applicable numeric + a field with no runtime consumer) can be
// written without touching any fingerprinted field.
func restartYAML(keep int, govEnabled bool, extra string) string {
	body := l3YAML("A", keep, 5000, 0.6, "2m")
	if govEnabled {
		body += "governance:\n  enabled: true\n"
	}
	return body + extra
}

// TestOrgReload_UnsupportedConfig 钉住 热更维度分类里没有第三种读数「静默 applied」。
// - 既不进结构指纹也不进热参数摘要的字段（governance.*／reliability.*／trajectory_capture.*）只能重启生效。
// - 与可热字段混在一起时整批拒绝：数值也不得悄悄应用，成功的 revision 不推进。
// - 拒绝理由点名本轮 diff 的字段路径，下一轮重新清点。
// - 结构改动与不可热字段混合同样整批拒：代不前进，旧面继续服务。
// - 只改可热维度时照常应用：拒绝针对的是字段而不是热更本身。
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestOrgReload_UnsupportedConfig(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(restartYAML(3, false, ""))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	base := entry.OrgDiagnostics()
	gen0, rev0 := diagInt64(t, base, "generation"), diagInt64(t, base, "revision")
	_, appliedAtStartup := diagTime(t, base, "lastAppliedAt")
	require.Equal(t, 3, entry.OrgKeepRecent(), "precondition: the startup value is what is in force")

	write(restartYAML(6, true, ""))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.Equal(t, 3, entry.OrgKeepRecent(), "a rejected batch must leave even the hot-applicable part untouched (no silent apply)")
	require.EqualValues(t, gen0, diagInt64(t, d, "generation"), "a restart-required edit must not advance the generation")
	require.EqualValues(t, rev0, diagInt64(t, d, "revision"), "a restart-required edit must not advance the successful-apply revision")
	_, appliedNow := diagTime(t, d, "lastAppliedAt")
	require.Equal(t, appliedAtStartup, appliedNow, "a rejection must neither create nor move the apply record")

	fail, ok := d["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, fail.Error, "restart_required", "the reason is the restart dimension, not a build failure")
	require.Contains(t, fail.Error, "governance.enabled", "the refusal names the offending field path")
	rr, ok := d["restartRequired"].([]string)
	require.True(t, ok, "the desired/effective receipt carries this round's restart-only field paths")
	require.Contains(t, rr, "governance.enabled")

	write(restartYAML(3, true, "reliability:\n  bus_spill_dir: "+filepath.Join(dir, "bus")+"\ntrajectory_capture:\n  max_record_bytes: 4096\n"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, rev0, diagInt64(t, d, "revision"), "a restart-only edit is not an apply")
	fail, ok = d["lastFailure"].(*OrgFailure)
	require.True(t, ok)
	rr, ok = d["restartRequired"].([]string)
	require.True(t, ok, "the receipt still reports the refusal of this round")
	require.Contains(t, rr, "governance.enabled", "a field still differing from the effective config stays named")
	require.Contains(t, rr, "reliability.bus_spill_dir")
	require.Contains(t, rr, "trajectory_capture.max_record_bytes")

	write(restartYAML(9, false, ""))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.Equal(t, 9, entry.OrgKeepRecent(), "the hot parameters apply once no restart-only field differs")
	require.EqualValues(t, rev0+1, diagInt64(t, d, "revision"), "a real apply advances the revision again")
	require.NotContains(t, d, "restartRequired", "a successful round clears the restart receipt — it reports THIS round's diff")

	write(restartYAML(9, true, ""))
	withPrompt := strings.Replace(readFile(t, yamlPath), `inline: "A"`, `inline: "B"`, 1)
	require.NotContains(t, withPrompt, `inline: "A"`, "precondition: the prompt edit changes the structural fingerprint, so the refusal must precede any candidate build")
	write(withPrompt)
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, gen0, diagInt64(t, d, "generation"), "a structural edit bundled with a restart-only field is refused as a whole batch")
	require.EqualValues(t, rev0+1, diagInt64(t, d, "revision"), "and the revision does not advance on the refused batch")
	fail, ok = d["lastFailure"].(*OrgFailure)
	require.True(t, ok)
	require.Contains(t, fail.Error, "governance.enabled")
}

// TestOrgReload_ModelReferenceContinuity 钉住 每个发布的代带着自己冻结的模型引用快照。
// - 发布后新选中的代解析引用得到注册表当前的实例，配置模型以自己的保留名在同一视图内可达。
// - 在途的视图钉在发起代：注册表此后换了实例也不跟着走。
// - 候选的保留名被别的实例占住时具名拒绝：整批不发布，进程全局注册表原样不动（不重指也不删）。
// - 保留名由 agent 名导出，所以用例用一个唯一的 agent 名，不踩包里其他用例的引用表。
// - 把保留名交还给该代实际解析出的实例后，发布重新可用。
// 契约: docs/wiki/agent/execution-generations.md#model-reference-pinning
func TestOrgReload_ModelReferenceContinuity(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	contYAML := func(prompt string) string {
		return strings.ReplaceAll(l3YAML(prompt, 3, 5000, 0.6, "2m"), "main", "contmain")
	}
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(contYAML("A"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	probeA := &stubModel{name: "cont-a"}
	agent.RegisterModelReference("cont-ref", probeA)

	write(contYAML("B"))
	entry.CheckOrgReload()
	require.EqualValues(t, 1, diagInt64(t, entry.OrgDiagnostics(), "generation"), "precondition: the prompt edit published a new generation")

	lease1 := entry.ContextManager().AcquireLease(agent.LeaseSubCall)
	require.NotNil(t, lease1)
	defer lease1.Release()
	refs1 := lease1.ModelReferences()
	require.NotNil(t, refs1, "a published generation carries the frozen reference snapshot of its own view")
	reserved := agent.ReservedModelRef("contmain")
	own, ok := refs1.Resolve(reserved)
	require.True(t, ok, "the config-driven instance is addressable under its reserved name inside its own view")
	gotA, ok := refs1.Resolve("cont-ref")
	require.True(t, ok, "the snapshot carries the references the registry published at that instant")
	require.True(t, gotA == model.Model(probeA), "and froze them: resolution inside one view never goes back to the mutable registry")

	probeB := &stubModel{name: "cont-b"}
	agent.RegisterModelReference("cont-ref", probeB)
	write(contYAML("C"))
	entry.CheckOrgReload()
	require.EqualValues(t, 2, diagInt64(t, entry.OrgDiagnostics(), "generation"), "precondition: the second generation published")

	lease2 := entry.ContextManager().AcquireLease(agent.LeaseSubCall)
	require.NotNil(t, lease2)
	defer lease2.Release()
	gotB, ok := lease2.ModelReferences().Resolve("cont-ref")
	require.True(t, ok)
	require.True(t, gotB == model.Model(probeB), "a newly selected generation reads the reference as the registry now holds it")
	stillA, ok := lease1.ModelReferences().Resolve("cont-ref")
	require.True(t, ok)
	require.True(t, stillA == model.Model(probeA), "an in-flight view keeps the instance its generation froze")

	squatter := &stubModel{name: "squatter"}
	agent.RegisterModelReference(reserved, squatter)
	write(contYAML("D"))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 2, diagInt64(t, d, "generation"), "a candidate whose reserved name conflicts must not publish")
	fail, ok := d["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the conflict is refused by name and the name is diagnosable")
	require.Contains(t, fail.Error, reserved)
	held, ok := agent.LookupModelReference(reserved)
	require.True(t, ok)
	require.True(t, held == model.Model(squatter), "a refusal leaves the process registry exactly as it was — no re-point, no drop")

	agent.RegisterModelReference(reserved, own)
	write(contYAML("E"))
	entry.CheckOrgReload()
	require.EqualValues(t, 3, diagInt64(t, entry.OrgDiagnostics(), "generation"), "publishing resumes once the reserved name matches the instance the view runs")
	refs3 := entry.ContextManager().AcquireLease(agent.LeaseSubCall)
	require.NotNil(t, refs3)
	defer refs3.Release()
	own3, ok := refs3.ModelReferences().Resolve(reserved)
	require.True(t, ok)
	require.True(t, own3 == model.Model(own), "the reserved name resolves to the very instance the generation runs")
}

// TestOrgReload_RestartOnlyTopLevelSwitch 钉住 顶层布尔开关也落在须重启那一类里：
// - trajectory_dump 与 trajectory_dir 同批可热数值一起出现时整批拒绝，代与 revision 都不推进；
// - 拒绝回执逐条点名这两条字段路径，理由带 restart_required 而不是构建失败；
// - 交回原声明后热参数照常应用，证明拒绝针对的是字段而不是热更本身。
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestOrgReload_RestartOnlyTopLevelSwitch(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(restartYAML(3, false, "trajectory_dump: false\n"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	base := entry.OrgDiagnostics()
	gen0, rev0 := diagInt64(t, base, "generation"), diagInt64(t, base, "revision")
	require.Equal(t, 3, entry.OrgKeepRecent(), "precondition: the startup value is what is in force")
	appliedAtStartup, hasStartupApply := diagTime(t, base, "lastAppliedAt")

	write(restartYAML(6, false, "trajectory_dump: true\ntrajectory_dir: "+strconv.Quote(filepath.Join(dir, "traj"))+"\n"))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.Equal(t, 3, entry.OrgKeepRecent(), "a refused batch leaves even the hot-applicable number untouched")
	require.EqualValues(t, gen0, diagInt64(t, d, "generation"), "a top-level restart-only switch must not spin a generation")
	require.EqualValues(t, rev0, diagInt64(t, d, "revision"), "a restart-only edit is not an apply")
	appliedNow, hasApplied := diagTime(t, d, "lastAppliedAt")
	require.Equal(t, hasStartupApply, hasApplied, "a refused batch must not create the apply record")
	if hasApplied {
		require.Truef(t, appliedNow.Equal(appliedAtStartup), "a refused batch must not move the apply record: %v vs %v", appliedNow, appliedAtStartup)
	}

	fail, ok := d["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, fail.Error, "restart_required", "the reason is the restart dimension, not a build failure")
	rr, ok := d["restartRequired"].([]string)
	require.True(t, ok, "the receipt names this round's restart-only field paths")
	require.Contains(t, rr, "trajectory_dump", "a top-level boolean switch is named by its YAML spelling")
	require.Contains(t, rr, "trajectory_dir")

	write(restartYAML(9, false, "trajectory_dump: false\n"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.Equal(t, 9, entry.OrgKeepRecent(), "the hot parameter applies once no restart-only field differs")
	require.EqualValues(t, rev0+1, diagInt64(t, d, "revision"), "a real apply advances the revision again")
	require.NotContains(t, d, "restartRequired", "the receipt reports THIS round's diff, not a stale refusal")
}

// TestOrgFingerprint_MeditationExtFieldsMoveFingerprint 钉住 冥想观察面扩字段参与结构指纹换代。
// - observed_namespaces 与 deliver_to 的增改都移动组织指纹，热更因此走换代重建而非静默生效；
// - 相同声明的两次装配指纹中性，换代只由真实差异触发。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestOrgFingerprint_MeditationExtFieldsMoveFingerprint(t *testing.T) {
	withMed := func(med MeditationConfig) *Config {
		c := cfgFor()
		ac := c.Agents["main"]
		ac.Meditation = med
		c.Agents["main"] = ac
		return c
	}
	base, err := org.ComputeOrgFingerprint(withMed(MeditationConfig{Enabled: true}))
	require.NoError(t, err)

	muts := []struct {
		name string
		med  MeditationConfig
	}{
		{"observed_added", MeditationConfig{Enabled: true, ObservedNamespaces: []string{"recall"}}},
		{"observed_changed", MeditationConfig{Enabled: true, ObservedNamespaces: []string{"session"}}},
		{"deliver_to_added", MeditationConfig{Enabled: true, DeliverTo: []string{"entry"}}},
		{"deliver_to_changed", MeditationConfig{Enabled: true, DeliverTo: []string{"recall"}}},
	}
	for _, m := range muts {
		fp, err := org.ComputeOrgFingerprint(withMed(m.med))
		require.NoErrorf(t, err, "mutation %q", m.name)
		require.NotEqualf(t, base, fp,
			"mutation %q did not move the org fingerprint — a meditation ext-field change would not force a new generation", m.name)
	}

	decl := MeditationConfig{Enabled: true, ObservedNamespaces: []string{"recall"}, DeliverTo: []string{"entry"}}
	fpA, err := org.ComputeOrgFingerprint(withMed(decl))
	require.NoError(t, err)
	fpB, err := org.ComputeOrgFingerprint(withMed(decl))
	require.NoError(t, err)
	require.Equal(t, fpA, fpB, "an identical meditation declaration must be fingerprint-neutral")
}
