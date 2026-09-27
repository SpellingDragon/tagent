package tagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	tasktool "github.com/SpellingDragon/tagent/tool/task"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// 轮九十四（evidence §5.50）：§4.2 的**重估**——「A→B→C 中 B 重入 C」这个多级面
// 究竟成不成立，用行为回答，不用读码回答（tasks 4.2 明写：禁止在重估前写新机制）。
//
// 重估问题的两面：
//   ① 环存活期内 B 重入 C —— 有发起者分支：ctx 带 B 的在途租约，解析必须走
//      **B 的该代面**（继承发起租约），既不能扫祖先工具表，也不能改用新代目标。
//   ② B 环静默后 relaunch —— 无发起者分支：解析必须回退到 **B 常驻 owner 面**
//      （实例常驻、未关），而不是 entry 面。
// 另两态按 4.2 的验收子句一并钉：G2 删 C → 具名拒绝且不改链；G2 改 C → 无发起者
// 重入必须打到**新代**的 C（这一态在 3.2 主干落地前是红的：未变父不推进 face，
// 重入永远打在旧叶子上——见 §5.43/§5.46）。

// chainDelegModel delegates to whichever offered tool names one of the agents in
// `prefer` (priority order). The framework does not promise tool ordering, so a
// mock that grabs tools[0] would assert an accident of slice order rather than the
// orchestration (the lesson recorded in §6.20); naming the target makes each
// level's delegation deterministic at every depth.
type chainDelegModel struct {
	mu      sync.Mutex
	served  []delegServed
	perCall map[string]int
	prefer  []string
}

func (m *chainDelegModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n%2 == 1 {
		for _, want := range m.prefer {
			for _, t := range tools {
				if t == want {
					offer = t
					break
				}
			}
			if offer != "" {
				break
			}
		}
	}
	m.served = append(m.served, delegServed{System: label, Tools: tools, Answered: offer})
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
		close(ch)
		return ch, nil
	}
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "served:" + label}}}}
	close(ch)
	return ch, nil
}

func (m *chainDelegModel) Info() model.Info { return model.Info{Name: "chain-deleg-model"} }

func (m *chainDelegModel) snapshot() []delegServed {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]delegServed, len(m.served))
	copy(out, m.served)
	return out
}

// reentryChainYAML builds a→b→c. b carries the PRODUCTION task tools, so the
// re-entry under test is the same object the LLM would call. c is delegated
// asynchronously (default), which is what puts a subagent task on **b's own**
// board rather than on the entry's.
func reentryChainYAML(cPrompt string, dropC bool) string {
	bTools := "      - kind: agent\n        agent: c\n        description: delegate-c\n      - kind: tool\n        id: relaunch_task\n      - kind: tool\n        id: resume_task\n"
	if dropC {
		bTools = "      - kind: tool\n        id: relaunch_task\n      - kind: tool\n        id: resume_task\n"
	}
	cDef := ""
	if !dropC {
		cDef = fmt.Sprintf("  c:\n    system_prompt:\n      inline: %q\n    memory:\n      type: memory\n", cPrompt)
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    memory:\n      type: memory\n    tools:\n      - kind: agent\n        agent: b\n        description: delegate-b\n" +
		"  b:\n    system_prompt:\n      inline: \"SUB-B\"\n    memory:\n      type: memory\n    tools:\n" + bTools + cDef
}

func writeReentryChain(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// startReentryChain runs one full a→b→c delegation and returns the org plus b's
// resident owner with the subagent task it left on b's OWN board.
func startReentryChain(t *testing.T, yamlPath string) (entry *agent.TagentAgent, b *agent.TagentAgent, taskID string, m *chainDelegModel) {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m = &chainDelegModel{prefer: []string{"b", "c"}}
	entry, err = New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)

	out, err := entry.StartLoop("u", "reentry-chain-session")
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
	waitFor(t, "the nested delegation settled on c", func() bool { return countServed(m.snapshot(), "SUB-C") >= 1 })

	b = residentCacheForTest(entry)["b"]
	require.NotNil(t, b, "b is a resident owner")
	// b's OWN board holds the subagent task — the spawner-ownership contract (§7.2/D3).
	var found *task.Task
	for _, tk := range b.TaskManager().List() {
		if tk.Spec.Kind == "subagent" {
			found = tk
			break
		}
	}
	require.NotNil(t, found, "the subagent task belongs to b's own manager, not the entry's")
	return entry, b, found.ID, m
}

// TestReentry42_WithinLoopInitiatorResolvesOnBsOwnFace pins ①: while a call on b
// is alive, a re-entry it starts must resolve against b's DECLARED generation.
//
// The publish that removes c mid-flight is what makes this non-vacuous: without a
// generation change both resolution branches answer the same way, so the test
// would prove nothing about which face was read. With it, falling back to the
// effective face (or to the entry's tool table, the pre-§4.2 failure) would
// refuse, while the spec requires the legal G1 binding to survive
// (resident-continuity「仍持 G1 租约的发起者不因 G2 删除目标而丢失其合法 G1 绑定」) —
// and it survives only because §3.2's declaration holds keep the pinned
// generation's child reachable.
func TestReentry42_WithinLoopInitiatorResolvesOnBsOwnFace(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", false), time.Now())

	entry, b, taskID, m := startReentryChain(t, yamlPath)
	defer func() { _ = entry.Close() }()

	before := countServed(m.snapshot(), "SUB-C")
	lease := b.ContextManager().AcquireLease(agent.LeaseSubCall) // b's live call is the initiator
	defer lease.Release()

	// A newer generation stops routing c WHILE the initiating call is alive.
	writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", true), tick)
	entry.CheckOrgReload()
	require.Nil(t, b.ContextManager().SubagentWrapper("c"),
		"precondition: the EFFECTIVE face no longer routes c, so only a declared-generation read can work")
	require.Equal(t, before, countServed(m.snapshot(), "SUB-C"), "no new serve happened on its own")

	ctx := lease.WithContext(task.WithTaskSpawner(context.Background(), b.TaskManager()))
	res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, taskID))
	require.NoError(t, err)
	require.NotContains(t, res.(string), "失败",
		"①有发起者的重入须按发起代解析，不因新代删除目标而丢失合法绑定：%v", res)
	waitFor(t, "the re-entered nested delegation ran on b's pinned face", func() bool {
		return countServed(m.snapshot(), "SUB-C") > before
	})
}

// TestReentry42_PostSilenceRelaunchUsesResidentOwnerFace pins ②: with b's loop
// silent and NO initiating call, the re-entry must fall back to b's resident
// owner face (the instance is resident and unclosed) — never to the entry's.
func TestReentry42_PostSilenceRelaunchUsesResidentOwnerFace(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", false), time.Now())

	entry, b, taskID, m := startReentryChain(t, yamlPath)
	defer func() { _ = entry.Close() }()

	before := countServed(m.snapshot(), "SUB-C")
	ctx := task.WithTaskSpawner(context.Background(), b.TaskManager()) // no lease: no initiator

	res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, taskID))
	require.NoError(t, err)
	require.NotContains(t, res.(string), "失败", "②b 环静默后重入必须从其常驻 owner 面解析：%v", res)
	waitFor(t, "the re-entry served through b's owner face", func() bool {
		return countServed(m.snapshot(), "SUB-C") > before
	})
}

// TestReentry42_GenerationThatRemovedTargetRefusesWithoutRerouting is the third
// state: after a published generation stops routing c, a re-entry of the stored
// task is refused by NAME with the version reason, and neither the removed target
// nor a substitute runs.
func TestReentry42_GenerationThatRemovedTargetRefusesWithoutRerouting(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", false), time.Now())

	entry, b, taskID, m := startReentryChain(t, yamlPath)
	defer func() { _ = entry.Close() }()

	tick = writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", true), tick)
	entry.CheckOrgReload()
	require.Nil(t, b.ContextManager().SubagentWrapper("c"),
		"precondition: the published generation really stopped routing c on b's face")

	runsBefore := countServed(m.snapshot(), "SUB-C")
	ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
	res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, taskID))
	require.NoError(t, err, "a refusal is reported as an answer, not a transport error")
	text, ok := res.(string)
	require.True(t, ok)
	require.Contains(t, text, "重跑任务", "宿主须看见是哪个动作失败：%s", text)
	require.Contains(t, text, "EFFECTIVE orchestration generation",
		"并看见拒绝理由是版本选择而非缺记录：%s", text)
	require.Equal(t, runsBefore, countServed(m.snapshot(), "SUB-C"),
		"§4.2：被拒重入不得把已移除目标再跑一次，也不得静默改道新目标")
}

// TestReentry42_ChangedTargetResolvesOnTheNewGeneration is the fourth state, and
// the one that could not pass before §3.2's trunk: a generation that changed c
// must be what a no-initiator re-entry reaches. b's own declaration is unchanged
// here, so this is precisely the「未变父也随发布推进执行视图」leg at depth 2.
func TestReentry42_ChangedTargetResolvesOnTheNewGeneration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", false), time.Now())

	entry, b, taskID, m := startReentryChain(t, yamlPath)
	defer func() { _ = entry.Close() }()

	tick = writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C-G2", false), tick)
	entry.CheckOrgReload()
	require.NotNil(t, b.ContextManager().SubagentWrapper("c"), "c is still routed after the publish")

	ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
	res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, taskID))
	require.NoError(t, err)
	require.NotContains(t, res.(string), "失败", "仍被路由的目标须经新面重入：%v", res)

	newBefore := countServed(m.snapshot(), "SUB-C-G2")
	waitFor(t, "the re-entry served the NEW c (b's face advanced with the publish)", func() bool {
		return countServed(m.snapshot(), "SUB-C-G2") > newBefore
	})
}
