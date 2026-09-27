package tagent

// 轮一百零四（evidence §5.60）：§5.2 的差集。逐行核对「必测」九行后，只有两类在本
// 变更此前零覆盖（映射表见 evidence）：
//
//	① **运行对象别名**——5.2 明令「不得只解释为 YAML legacy 键而漏运行对象别名」。
//	   `org_config_alias_folding_test.go` 钉的是 `compress.summary_model` 这类**配置键**
//	   别名；而 `kind:` 省略 ≡ `kind: agent` 这类**运行对象**别名只靠 ApplyDefaults
//	   归一，此前无测。风险具体：可达性／退役／`remoteDeclarationOnly` 等判定同时接受
//	   两种拼写，若日后有人「简化」成只认显式值，同一份配置的别名拼写就会走出不同拓扑
//	   （§5.53 那个 remote-only 永拒缺陷正是这条判定错一次的产物）。
//	② 热增 owner 的**记录源是否真接上了**——§5.59 修掉的次序缺陷需要一条持久守卫：
//	   缺它时新 owner 在下一轮 numeric-only 之前完全读不到唯一记录。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// d52PullModel answers every request with plain text: the tests below hold a call
// open with a real LEASE, so nothing depends on model latency.
type d52PullModel struct{}

func (m *d52PullModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("ok")}}}
	close(ch)
	return ch, nil
}

func (m *d52PullModel) Info() model.Info { return model.Info{Name: "d52-pull"} }

// d52YAML renders entry "main" routing sub1 (and optionally sub2 with its own
// numbers) using the requested tool-ref spelling: "explicit" writes `kind: agent`,
// "omitted" leaves the kind out — the same orchestration, spelled differently.
func d52YAML(spelling string, keepMain int, withSub2 bool, keepSub2, maxSub2 int) string {
	ref := "      - kind: agent\n        agent: sub1\n        description: \"delegate-sub1\"\n"
	if spelling == "omitted" {
		ref = "      - agent: sub1\n        description: \"delegate-sub1\"\n"
	}
	sub2Ref := ""
	if withSub2 {
		sub2Ref = "      - kind: agent\n        agent: sub2\n        description: \"delegate-sub2\"\n"
	}
	sub2Def := ""
	if withSub2 {
		sub2Def = fmt.Sprintf("  sub2:\n    system_prompt:\n      inline: \"SUB2-D52\"\n    keep_recent_tasks: %d\n    max_tokens: %d\n    compress_threshold: 0.5\n    memory:\n      type: memory\n", keepSub2, maxSub2)
	}
	return "entry: main\nagents:\n  main:\n" +
		"    system_prompt:\n      inline: \"MAIN-D52\"\n" +
		fmt.Sprintf("    keep_recent_tasks: %d\n", keepMain) +
		"    memory:\n      type: memory\n" +
		"    tools:\n" + ref + sub2Ref +
		"  sub1:\n    system_prompt:\n      inline: \"SUB1-D52\"\n    memory:\n      type: memory\n" +
		sub2Def
}

// d52RemoteYAML is a remote-only reference (legal with no local definition at all,
// §5.44/§5.53) written with or without the explicit kind.
func d52RemoteYAML(spelling, endpoint string) string {
	ref := "      - kind: agent\n        agent: knowledge\n        description: delegate-knowledge\n        async: false\n"
	if spelling == "omitted" {
		ref = "      - agent: knowledge\n        description: delegate-knowledge\n        async: false\n"
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    max_tool_iterations: 2\n    memory:\n      type: memory\n    tools:\n" + ref +
		fmt.Sprintf("        remote:\n          url: %q\n", endpoint)
}

func d52Write(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// TestD52_RuntimeObjectAliasIsNotAStructuralChange pins ①: re-spelling the SAME
// orchestration (tool-ref kind omitted) must not read as a topology change, must
// not fail, and must not replace the owner it already runs.
func TestD52_RuntimeObjectAliasIsNotAStructuralChange(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	d52Write(t, yamlPath, d52YAML("explicit", 2, false, 0, 0), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&d52PullModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Contains(t, entryToolNames(entry), "sub1", "precondition: the explicit spelling routes sub1")
	before := di64(t, entry.OrgDiagnostics(), "generation")
	sub1Before := residentCacheForTest(entry)["sub1"]
	require.NotNil(t, sub1Before, "precondition: sub1 is a resident owner")

	// Same orchestration, other spelling.
	d52Write(t, yamlPath, d52YAML("omitted", 2, false, 0, 0), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.Equalf(t, before, di64(t, d, "generation"),
		"§5.2：别名拼写不是结构变更——换写法不得推进代际（failure=%v）", d["lastFailure"])
	require.NotContains(t, d, "lastFailure", "and the alias-only edit must not be recorded as a failure")
	require.Contains(t, entryToolNames(entry), "sub1",
		"the two spellings must resolve to the same reachable runtime object")
	require.Same(t, sub1Before, residentCacheForTest(entry)["sub1"],
		"and must not rebuild or replace the owner that is already serving")
}

// TestD52_RemoteOnlyAliasSpellingStillPublishes is ① × §5.53: the remote-only
// reference that hot-reloads thanks to `remoteDeclarationOnly` must behave
// identically when written with the kind omitted. That helper accepts both
// spellings because the alias is a runtime-object fact; this is the guard that
// keeps it from being "simplified" back to explicit-only, which would re-break
// exactly the deployment shape §5.53 found permanently refused.
func TestD52_RemoteOnlyAliasSpellingStillPublishes(t *testing.T) {
	svcA := newRemoteService(t)
	svcB := newRemoteService(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	d52Write(t, yamlPath, d52RemoteYAML("omitted", svcA.srv.URL), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&wireModel{args: `{"request":"alias spelling"}`}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.NotNil(t, entry.ContextManager().SubagentWrapper("knowledge"),
		"precondition: a remote-only ref with the kind omitted is still a legal declaration")

	// Re-point the same name at another endpoint: structural, and it must publish.
	d52Write(t, yamlPath, d52RemoteYAML("explicit", svcB.srv.URL), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.NotZerof(t, di64(t, d, "generation"),
		"§5.53 的 remote-only 放行必须对两种拼写一致，否则别名拼写的部署又被「引用未定义」永拒（failure=%v）", d["lastFailure"])
	require.NotContains(t, d, "lastFailure", "and the publish must not be refused under either spelling")
}

// TestD52_HotAddedOwnerPullsTheRecordAfterNumericOnly is ②: the §5.59 ordering fix
// must stay fixed. A hot-added owner is held in flight by a real lease while a
// numeric-only apply lands; its own consumers must then resolve the NEW committed
// values — impossible unless the structural publish wired its record source.
func TestD52_HotAddedOwnerPullsTheRecordAfterNumericOnly(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	d52Write(t, yamlPath, d52YAML("explicit", 2, false, 0, 0), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&d52PullModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// Structural publish: sub2 joins through the hot path with its own numbers.
	d52Write(t, yamlPath, d52YAML("explicit", 2, true, 4, 6000), &tick)
	entry.CheckOrgReload()
	sub2 := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, sub2, "precondition: sub2 became a resident owner through the hot path")
	require.Equal(t, 3000, sub2.OrgBudgetLine(), "6000×0.5 from the record it was built with")

	// The §5.59 discriminator, asserted AT the structural publish: the owner this
	// very publish installed must already be described by the receipt set and
	// already wired to the committed record. (Before the ordering fix, applyHotAll
	// ran before the candidate merge, so sub2 appeared in neither.)
	recAtPublish := d51Receipts(t, entry.OrgDiagnostics())
	require.Equalf(t, "applied", recAtPublish["sub2"].Outcome,
		"§5.2/§5.59：结构发布当轮就必须描述新装上的消费源（实得 %+v）", recAtPublish["sub2"])
	require.Equal(t, 6000, recAtPublish["sub2"].MaxTokens)

	// Hold the new owner in flight across a purely numeric edit to IT.
	held := sub2.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer held.Release()

	d52Write(t, yamlPath, d52YAML("explicit", 2, true, 9, 8000), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.NotZero(t, di64(t, d, "revision"), "the numeric-only edit is a full apply")
	require.Equalf(t, 4000, sub2.OrgBudgetLine(),
		"§5.2/§5.59：热增 owner 必须经它自己的记录源读到本轮提交值（8000×0.5）；缺记录源时它读不到")
	require.Equal(t, 9, sub2.OrgKeepRecent(), "and the same for keepRecent")

	rec := d51Receipts(t, d)
	require.Equal(t, "applied", rec["sub2"].Outcome, "the receipt describes the installed consumer")
	require.Equal(t, 8000, rec["sub2"].MaxTokens)

	// Resolution is stable across the in-flight boundary, not only after release.
	held.Release()
	require.Equal(t, 4000, sub2.OrgBudgetLine())
}
