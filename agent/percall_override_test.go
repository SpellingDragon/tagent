// 本文件负责 per-call 子 agent 覆盖层的调用作用域语义：覆盖只在装配期进入本次调用的视图、
// 随调用消亡且不写回常驻定义，越域请求在参数校验点具名拒绝且不产生任何执行。
// 契约: docs/wiki/platform/org-hot-reload.md#percall-overrides
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// percallLeafTool is a named non-delegation leaf tool. A blank agent's declared
// leaf set IS its maximum tool domain, so a test needs tools whose names it can
// enumerate.
type percallLeafTool struct{ name string }

func (t percallLeafTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name, Description: "percall leaf", InputSchema: &trpctool.Schema{Type: "object"}}
}

func (t percallLeafTool) Call(context.Context, []byte) (any, error) { return "ok", nil }

// viewRecorderModel is a fake model that keeps every model.Request it served, so
// the ASSEMBLED execution view of one call is read off the real request: the
// system prompt it was shown, the tool surface it was offered, and WHICH model
// instance actually answered.
//
// - `gate`, when non-nil, parks every call before answering (in-flight observation).
// - Instances are per-agent, so two racing calls never interleave a shared counter.
type viewRecorderModel struct {
	label string
	mu    sync.Mutex
	reqs  []*model.Request
	resp  *model.Response
	gate  chan struct{}
}

func (m *viewRecorderModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	if m.gate != nil {
		select {
		case <-m.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan *model.Response, 1)
	ch <- m.resp
	close(ch)
	return ch, nil
}

func (m *viewRecorderModel) Info() model.Info { return model.Info{Name: "view-" + m.label} }

func (m *viewRecorderModel) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reqs)
}

func (m *viewRecorderModel) at(i int) *model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i >= len(m.reqs) {
		return nil
	}
	return m.reqs[i]
}

// systemText returns the concatenated content of every system-role message of
// request i — the prompt this call's view actually carried.
func (m *viewRecorderModel) systemText(i int) string {
	req := m.at(i)
	if req == nil {
		return ""
	}
	var out string
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem {
			out += msg.Content
		}
	}
	return out
}

// toolNames returns the tool surface advertised to request i.
func (m *viewRecorderModel) toolNames(i int) []string {
	req := m.at(i)
	if req == nil {
		return nil
	}
	var names []string
	for name := range req.Tools {
		names = append(names, name)
	}
	return names
}

// armGate parks every later call of this model until the returned channel closes.
func (m *viewRecorderModel) armGate() chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gate = make(chan struct{})
	return m.gate
}

func percallFinalResp(content string) *model.Response {
	return &model.Response{ID: "final", Done: true, Choices: []model.Choice{{
		Message: model.Message{Role: model.RoleAssistant, Content: content}}}}
}

// percallToolCallResp builds one host turn that delegates to `toolName` with the
// given arguments.
func percallToolCallResp(id, toolName string, args map[string]any) *model.Response {
	data, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return &model.Response{
		ID:   id,
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{
					Type:     "function",
					ID:       id,
					Function: model.FunctionDefinitionParam{Name: toolName, Arguments: data},
				}},
			},
		}},
	}
}

// percallScriptModel answers its N-th request from a fixed script and repeats the
// last entry afterwards, so an extra assembly round cannot derail a test.
type percallScriptModel struct {
	mu    sync.Mutex
	reqs  []*model.Response
	calls int
}

func (m *percallScriptModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	n := m.calls
	m.calls++
	var resp *model.Response
	if n < len(m.reqs) {
		resp = m.reqs[n]
	} else {
		resp = m.reqs[len(m.reqs)-1]
	}
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *percallScriptModel) Info() model.Info { return model.Info{Name: "percall-script"} }

func (m *percallScriptModel) served() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// newPercallBlank builds the real delegation target: an agent whose file truth is
// an empty shell prompt plus an explicit tool list (that list is its maximum tool
// domain), served by `m`.
func newPercallBlank(tb testing.TB, name string, m model.Model, tools []trpctool.Tool) *TagentAgent {
	tb.Helper()
	ta, err := NewTagentAgent(&TagentConfig{
		Model:             m,
		Name:              name,
		SystemPrompt:      "BASE-SHELL-PROMPT",
		Description:       "blank delegate",
		MaxToolIterations: 5,
		MaxTokens:         4000,
		CompressThreshold: 0.5,
		KeepRecentTasks:   2,
		Tools:             tools,
	})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = ta.Close() })
	return ta
}

// startPercallHost runs a resident host loop over `tools` and drains its output.
// The delegation under test therefore travels the real path: host turn →
// framework tool dispatch → AgentToolWrapper.Call → the delegate's own Run
// assembly.
func startPercallHost(tb testing.TB, name string, script []*model.Response, tools []trpctool.Tool) (*TagentAgent, *percallScriptModel) {
	tb.Helper()
	m := &percallScriptModel{reqs: script}
	host, err := NewTagentAgent(&TagentConfig{
		Model:             m,
		Name:              name,
		SystemPrompt:      "You are the delegating host.",
		MaxToolIterations: 8,
		MaxTokens:         4000,
		CompressThreshold: 0.5,
		KeepRecentTasks:   2,
		MemoryStore:       memory.NewInMemoryStore(),
		Tools:             tools,
	})
	require.NoError(tb, err)
	out, err := host.StartLoop("percall-user", name)
	require.NoError(tb, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	tb.Cleanup(func() {
		_ = host.Close()
		<-done
	})
	return host, m
}

// injectPercallHost feeds one user message into a resident host loop.
func injectPercallHost(tb testing.TB, host *TagentAgent) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := host.InjectMessageContext(ctx, "user", model.NewUserMessage("delegate it"))
	require.NoError(tb, err)
}

// overrideRejectError is the shape a fail-closed per-call override rejection must
// carry: a named field, so the caller learns WHICH override was refused instead
// of meeting a truncated or silently accepted call.
type overrideRejectError interface {
	error
	OverrideField() string
}

// TestPerCallOverride_SecondCallWithoutOverrideUsesGenerationDefault 钉住覆盖的作用域：一次委派携带覆盖、紧随的第二次不携带。
// - 第一次的执行视图必须是覆盖值（提示词与工具面都换掉）；
// - 第二次必须回到本代定义的视图，且常驻定义逐字不变。
func TestPerCallOverride_SecondCallWithoutOverrideUsesGenerationDefault(t *testing.T) {
	childView := &viewRecorderModel{label: "child", resp: percallFinalResp("CHILD-ANSWER")}
	blank := newPercallBlank(t, "percall-blank-default", childView,
		[]trpctool.Tool{percallLeafTool{"read_file"}, percallLeafTool{"save_file"}})

	w := NewAgentToolWrapper(blank, "delegate to the blank agent", nil, nil)
	host, hostModel := startPercallHost(t, "percall-host-default", []*model.Response{
		percallToolCallResp("ov-1", "percall-blank-default", map[string]any{
			"request":                "first delegation",
			"system_prompt_override": "OVERRIDE-PROMPT-1",
			"tools_subset":           []string{"read_file"},
		}),
		percallToolCallResp("ov-2", "percall-blank-default", map[string]any{
			"request": "second delegation, no override",
		}),
		percallFinalResp("HOST-DONE"),
	}, []trpctool.Tool{w})

	injectPercallHost(t, host)
	require.Eventually(t, func() bool { return childView.calls() >= 2 }, 20*time.Second, 20*time.Millisecond,
		"the host turn must have delegated twice (host served %d rounds)", hostModel.served())

	require.Contains(t, childView.systemText(0), "OVERRIDE-PROMPT-1",
		"call A carries an override → its assembled view must show it")
	require.Equal(t, []string{"read_file"}, childView.toolNames(0),
		"and its tool surface must be the narrowed subset")

	require.NotContains(t, childView.systemText(1), "OVERRIDE-PROMPT-1",
		"call B carries NO override → the override must not have leaked into it")
	require.Contains(t, childView.systemText(1), "BASE-SHELL-PROMPT",
		"call B must be served the generation-defined prompt")
	require.Len(t, childView.toolNames(1), 2, "call B must see the full declared tool domain")

	require.Equal(t, "BASE-SHELL-PROMPT", blank.config.SystemPrompt,
		"an override may never be written back onto the resident definition")
	require.Len(t, blank.Tools(), 2, "an override may never narrow the resident tool declaration")
}

// TestPerCallOverride_ToolsSubsetBeyondDomainIsRefused 钉住最大工具域是硬上界：`tools_subset` 含域外条目即 fail-closed。
// - 拒绝必须具名（点名被拒字段与越域条目），不得静默截断或放行；
// - 拒绝发生在任何执行之前——被调方一次都不运行。
func TestPerCallOverride_ToolsSubsetBeyondDomainIsRefused(t *testing.T) {
	childView := &viewRecorderModel{label: "refuse", resp: percallFinalResp("SHOULD-NOT-RUN")}
	blank := newPercallBlank(t, "percall-blank-refuse", childView,
		[]trpctool.Tool{percallLeafTool{"read_file"}})

	w := NewAgentToolWrapper(blank, "delegate", nil, nil)
	args, err := json.Marshal(map[string]any{
		"request":      "out of domain work",
		"tools_subset": []string{"read_file", "mcp_call"},
	})
	require.NoError(t, err)

	out, callErr := w.Call(context.Background(), args)
	require.Error(t, callErr, "a tools_subset beyond the declared maximum domain must be refused")
	var shaped overrideRejectError
	require.True(t, errors.As(callErr, &shaped),
		"and the refusal must be structured (naming the refused field), got %v", callErr)
	require.Equal(t, "tools_subset", shaped.OverrideField())
	require.Contains(t, callErr.Error(), "mcp_call", "the refusal must name the offending entry")
	require.Nil(t, out, "a refused call produces no result")
	require.Zero(t, childView.calls(), "and the delegate never runs")
}

// percallArgs marshals tool arguments, failing the test on an impossible encode.
func percallArgs(tb testing.TB, args map[string]any) []byte {
	tb.Helper()
	data, err := json.Marshal(args)
	require.NoError(tb, err)
	return data
}

// TestPerCallOverride_ModelOverrideServesRegisteredReference 钉住 model_override 只认已注册引用。
// - 引用已注册：本次调用由那台实例服务，被调方自有模型一次都不出场；
// - 引用未注册：具名结构化拒绝，两个模型都不出场；
// - 被接受的用例走真实委派链路：宿主回合 → 框架工具派发 → AgentToolWrapper.Call → 被调方自己的装配。
func TestPerCallOverride_ModelOverrideServesRegisteredReference(t *testing.T) {
	registered := &viewRecorderModel{label: "registered", resp: percallFinalResp("REGISTERED-MODEL-ANSWER")}
	RegisterModelReference("percall-ref-registered", registered)

	own := &viewRecorderModel{label: "own", resp: percallFinalResp("OWN-MODEL-ANSWER")}
	blank := newPercallBlank(t, "percall-blank-model", own,
		[]trpctool.Tool{percallLeafTool{"read_file"}})
	w := NewAgentToolWrapper(blank, "delegate to the blank agent", nil, nil)

	host, _ := startPercallHost(t, "percall-host-model", []*model.Response{
		percallToolCallResp("m-1", "percall-blank-model", map[string]any{
			"request":        "work under an overridden model",
			"model_override": "percall-ref-registered",
		}),
		percallFinalResp("HOST-DONE"),
	}, []trpctool.Tool{w})
	injectPercallHost(t, host)

	require.Eventually(t, func() bool { return registered.calls() >= 1 }, 20*time.Second, 20*time.Millisecond,
		"the registered reference must serve the overridden call")
	require.Contains(t, registered.systemText(0), "BASE-SHELL-PROMPT",
		"a model override replaces the model, leaving the other view faces at the generation's values")
	require.Zero(t, own.calls(),
		"the delegate's own model must not serve a call whose model was overridden")

	out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
		"request":        "work under a name nobody registered",
		"model_override": "percall-ref-unknown",
	}))
	require.Error(t, err, "an unregistered model reference must be refused")
	var shaped overrideRejectError
	require.True(t, errors.As(err, &shaped), "and the refusal must be structured, got %v", err)
	require.Equal(t, "model_override", shaped.OverrideField())
	require.Contains(t, err.Error(), "percall-ref-unknown", "the refusal must name the offending reference")
	require.Nil(t, out, "a refused call produces no result")
	require.Zero(t, own.calls(), "and the delegate still never runs")
}

// TestPerCallOverride_ContextRefsTravelTheExistingChannel 钉住 context_refs 不另建传递面：引用经父存储解析后走既有外部上下文通道。
// - 被调方的请求里必须出现该事件的摘要与该通道的固定分节标记。
func TestPerCallOverride_ContextRefsTravelTheExistingChannel(t *testing.T) {
	parentStore := memory.NewInMemoryStore()
	partitionID := memory.PartitionIDFromName("percall-refs")
	key := memory.NewSnowflakeEventKey(partitionID, 0)
	require.NoError(t, parentStore.StoreEvent(key, memory.FullEvent{
		EventKey:     key,
		PartitionID:  partitionID,
		EventType:    "user_input",
		EventSummary: "PERCALL-CONTEXT-REF-SUMMARY",
		Content:      "referenced conversation content",
	}))

	childView := &viewRecorderModel{label: "refs", resp: percallFinalResp("CHILD-ANSWER")}
	blank := newPercallBlank(t, "percall-blank-refs", childView,
		[]trpctool.Tool{percallLeafTool{"read_file"}})
	w := NewAgentToolWrapper(blank, "delegate", []string{"event_keys"}, parentStore)

	_, err := w.Call(context.Background(), percallArgs(t, map[string]any{
		"request":      "work with referenced context",
		"context_refs": []string{strconv.FormatInt(key, 10)},
	}))
	require.NoError(t, err)
	require.GreaterOrEqual(t, childView.calls(), 1, "the delegate must run")

	var served string
	for _, msg := range childView.at(0).Messages {
		served += msg.Content
	}
	require.Contains(t, served, "PERCALL-CONTEXT-REF-SUMMARY",
		"the referenced event must reach the delegate's request")
	require.Contains(t, served, "[External Context from Parent Agent]",
		"and it must arrive through the existing external-context channel, not a new one")
}

// TestPerCallOverride_DeclarationAdmitsTheFourArguments 钉住参数面：四个覆盖参数对委派工具可见，模型才能提出它们。
// - 缺任一声明，覆盖就是模型够不着的死参数。
func TestPerCallOverride_DeclarationAdmitsTheFourArguments(t *testing.T) {
	own := &viewRecorderModel{label: "decl", resp: percallFinalResp("ANSWER")}
	blank := newPercallBlank(t, "percall-blank-decl", own, []trpctool.Tool{percallLeafTool{"read_file"}})
	w := NewAgentToolWrapper(blank, "delegate", nil, nil)

	props := w.Declaration().InputSchema.Properties
	for _, name := range []string{"system_prompt_override", "model_override", "tools_subset", "context_refs"} {
		require.Contains(t, props, name, "%s must be declared on the delegation tool", name)
	}
}

// TestPerCallOverride_RebuiltRegistryReplaysTheFrozenOverrides 钉住跨重启重放：登记重建折叠出的记录必须带着原覆盖。
// - task_spawned 事实里的覆盖逐字段还原，标量 knob 仍留在 Params；
// - 重启后由该记录发起的重放，读到的视图覆盖与原调用相同，来源是记录而非任何活对象。
func TestPerCallOverride_RebuiltRegistryReplaysTheFrozenOverrides(t *testing.T) {
	store := memory.NewInMemoryStore()
	now := time.Now().UnixMilli()
	taskID := "percall-restart-task"
	frozen := task.Declarative{
		Kind:        "subagent",
		Desc:        "percall-blank: work",
		AgentName:   "percall-blank",
		MessageBody: "work",
		Params:      map[string]string{"ttl": "600"},
		Overrides: &task.Overrides{
			SystemPrompt: "OVERRIDE-PROMPT",
			ModelRef:     "percall-ref-registered",
			ToolsSubset:  []string{"read_file"},
		},
	}
	storeSpawned(t, store, taskID, frozen, now-30_000)

	tm := task.NewTaskManager(task.TaskManagerConfig{})
	var replayed []*task.Overrides
	restored := RebuildTaskRegistry(store, sinkPartition, tm, func(decl task.Declarative) task.TaskSpec {
		folded := decl
		return task.TaskSpec{
			Kind:        folded.Kind,
			Desc:        folded.Desc,
			Declarative: &folded,
			Relaunch: func(context.Context) (task.SpawnResult, error) {
				replayed = append(replayed, folded.Overrides)
				return task.SpawnResult{}, nil
			},
		}
	})
	require.Equal(t, 1, restored, "the fact chain must restore the delegation's task")

	got, ok := tm.Get(taskID)
	require.True(t, ok, "a task with no terminal settle comes back onto the board")
	require.Equal(t, frozen.Overrides, got.Spec.Declarative.Overrides,
		"the rebuilt record carries the frozen view, field for field")
	require.Equal(t, frozen.Params, got.Spec.Declarative.Params,
		"and the scalar knobs stay where they were recorded")

	_, err := tm.Relaunch(context.Background(), taskID)
	require.NoError(t, err)
	require.Len(t, replayed, 1, "the re-entry closure ran")
	require.Equal(t, frozen.Overrides, replayed[0],
		"a re-entry after a restart replays the overrides off the record")
	require.Equal(t, "OVERRIDE-PROMPT", replayed[0].SystemPrompt)
	require.Equal(t, []string{"read_file"}, replayed[0].ToolsSubset)
}

// stageGeneration publishes one orchestration generation of a resident child: its
// own execution face plus the assembly config a call pinned to it resolves. The
// wrapper handed back is wired to THAT generation's binding, which is how an
// ordinary delegation picks up a reloaded definition.
func stageGeneration(tb testing.TB, blank *TagentAgent, label, promptText string, m model.Model, tools []trpctool.Tool) *AgentToolWrapper {
	tb.Helper()
	cm := blank.contextManager
	face := cm.ExecutorConfig()
	face.Model = m
	face.Tools = tools
	face.SystemPrompt = promptText
	runCfg := *blank.config
	runCfg.Model = m
	runCfg.Tools = tools
	runCfg.SystemPrompt = promptText
	runCfg.SystemPromptSource = nil
	staged := cm.StageExecutor(cm.NewExecutorCandidate(face), face, &runCfg)
	require.NotNil(tb, staged, "generation %s must stage a candidate", label)
	require.NotNil(tb, cm.ActivateExecutor(staged), "generation %s must activate", label)

	w := NewAgentToolWrapper(blank, "delegate to the blank agent", nil, nil)
	w.setDeclared(staged.binding)
	require.True(tb, w.declaredSet(), "the wrapper must route through generation %s", label)
	return w
}

// TestPerCallOverride_InFlightCallSurvivesGenerationReload 钉住覆盖与代际热更的交叠面：覆盖调用在途时发布新代。
// - 在途调用一路用它的装配视图（本代定义加覆盖）跑完，既不漂到新代模型，也不被新代冲掉覆盖；
// - 紧随其后的无覆盖调用落在新代上，视图是新代自己的定义；
// - 常驻定义始终逐字不变。
func TestPerCallOverride_InFlightCallSurvivesGenerationReload(t *testing.T) {
	g1 := &viewRecorderModel{label: "g1", resp: percallFinalResp("G1-ANSWER")}
	gate := g1.armGate()
	release := func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}
	t.Cleanup(release)
	blank := newPercallBlank(t, "percall-blank-gen", g1,
		[]trpctool.Tool{percallLeafTool{"read_file"}, percallLeafTool{"save_file"}})
	require.Equal(t, "BASE-SHELL-PROMPT", blank.config.SystemPrompt, "the resident definition before any publish")

	w1 := stageGeneration(t, blank, "gen-1", "GEN-1-PROMPT", g1,
		[]trpctool.Tool{percallLeafTool{"read_file"}})

	type callOutcome struct {
		out any
		err error
	}
	argsA := percallArgs(t, map[string]any{
		"request":                "in flight across a reload",
		"system_prompt_override": "OVERRIDE-PROMPT",
	})
	done := make(chan callOutcome, 1)
	go func() {
		out, err := w1.Call(context.Background(), argsA)
		done <- callOutcome{out, err}
	}()

	require.Eventually(t, func() bool { return g1.calls() >= 1 }, 10*time.Second, 20*time.Millisecond,
		"the overridden call must reach its generation's model before the reload lands")
	require.Equal(t, []string{"read_file"}, g1.toolNames(0),
		"the call's base view is the generation it resolved, whose declared surface is that one tool")
	require.Contains(t, g1.systemText(0), "OVERRIDE-PROMPT",
		"and the override replaces this call's prompt")
	require.NotContains(t, g1.systemText(0), "BASE-SHELL-PROMPT",
		"the overridden face is what the call was served, not the resident definition's")

	g2 := &viewRecorderModel{label: "g2", resp: percallFinalResp("G2-ANSWER")}
	w2 := stageGeneration(t, blank, "gen-2", "GEN-2-PROMPT", g2,
		[]trpctool.Tool{percallLeafTool{"read_file"}, percallLeafTool{"save_file"}})

	release()
	res := <-done
	require.NoError(t, res.err, "the in-flight call must finish, not be re-routed mid-run")
	require.Contains(t, res.out, "G1-ANSWER", "and it finished on the generation it started on")
	require.Equal(t, 1, g1.calls(), "a reload must not add a round to the in-flight generation")
	require.Zero(t, g2.calls(),
		"the in-flight call never touched the new generation — its pinned view, override included, did not drift")

	out2, err := w2.Call(context.Background(), percallArgs(t, map[string]any{
		"request": "a later call without any override",
	}))
	require.NoError(t, err)
	require.Contains(t, out2, "G2-ANSWER", "a call after the reload runs on the new generation")
	require.Contains(t, g2.systemText(0), "GEN-2-PROMPT", "and gets the new generation's own prompt")
	require.NotContains(t, g2.systemText(0), "OVERRIDE-PROMPT",
		"the earlier call's override ended with that call")
	require.Len(t, g2.toolNames(0), 2, "with the new generation's full tool surface")

	require.Equal(t, "BASE-SHELL-PROMPT", blank.config.SystemPrompt,
		"neither an override nor a publish writes back onto the resident definition")
	require.Len(t, blank.Tools(), 2, "and the resident tool declaration stays as declared")
}

// TestPerCallOverride_RelaunchCarriesTheFrozenOverrides 钉住 跨重启重建后的 relaunch 把冻结覆盖交给重投递器。
// - Declarative.Overrides 必须随 relaunch 传入 redispatch——折叠还原而不透传＝覆盖在重投递处蒸发；
// - 重投递后的任务再冻结同一覆盖（二级 relaunch 不降级为代际视图）。
// 契约: docs/wiki/platform/org-hot-reload.md#percall-overrides
func TestPerCallOverride_RelaunchCarriesTheFrozenOverrides(t *testing.T) {
	frozen := &task.Overrides{SystemPrompt: "OVERRIDE-PROMPT", ToolsSubset: []string{"read_file"}}
	var got *task.Overrides
	var gotName, gotBody string
	redispatch := func(_ context.Context, agentName, body string, ov *task.Overrides) (task.SpawnResult, error) {
		gotName, gotBody, got = agentName, body, ov
		return task.SpawnResult{}, nil
	}
	decl := task.Declarative{
		Kind: "subagent", AgentName: "percall-blank", MessageBody: "work",
		Overrides: frozen,
	}

	spec := action.SubagentSpecFromDeclarative(redispatch, decl)
	require.NotNil(t, spec.Relaunch, "subagent relaunch must exist (promise table)")
	_, err := spec.Relaunch(context.Background())
	require.NoError(t, err)
	require.Equal(t, "percall-blank", gotName)
	require.Equal(t, "work", gotBody)
	require.Equal(t, frozen, got, "the relaunch must hand the frozen overrides to the re-dispatcher")
}

// TestModelOverride_ReferenceResolution 钉住四种引用形态都从「已选执行视图」解析，而不是从可变的全局表反复查证：
//   - 已注册引用：由那台实例服务，被调方自有模型一次都不出场；
//   - 配置模型（保留名 agent:<agent>）：解析为该执行视图里的实际 agent 模型，无需任何注册动作；
//   - 自定义注册占用保留名：与本代实际模型不是同一实例即冲突，具名拒绝、不产生任何执行，且这次失败的候选不改写进程全局注册表（既不覆盖也不删除）；
//   - 未注册引用：具名拒绝，两个模型都不出场。
//
// 契约: docs/wiki/platform/org-hot-reload.md#percall-overrides
func TestModelOverride_ReferenceResolution(t *testing.T) {
	t.Run("registered reference serves that instance", func(t *testing.T) {
		registered := &viewRecorderModel{label: "o2a-registered", resp: percallFinalResp("REGISTERED-ANSWER")}
		RegisterModelReference("o2a-ref-registered", registered)
		own := &viewRecorderModel{label: "o2a-own-registered", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-ref-agent-registered", own,
			[]trpctool.Tool{percallLeafTool{"read_file"}})
		w := NewAgentToolWrapper(blank, "delegate", nil, nil)

		out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "work under a registered reference",
			"model_override": "o2a-ref-registered",
		}))
		require.NoError(t, err, "a registered reference must resolve for this call")
		require.Contains(t, out, "REGISTERED-ANSWER")
		require.GreaterOrEqual(t, registered.calls(), 1,
			"the instance the reference names must serve the call")
		require.Zero(t, own.calls(),
			"an overridden model replaces the delegate's own instance for this call")
	})

	t.Run("reserved name resolves the view's own model", func(t *testing.T) {
		own := &viewRecorderModel{label: "o2a-config-model", resp: percallFinalResp("CONFIG-MODEL-ANSWER")}
		blank := newPercallBlank(t, "o2a-ref-agent-config", own,
			[]trpctool.Tool{percallLeafTool{"read_file"}})
		w := NewAgentToolWrapper(blank, "delegate", nil, nil)

		out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "work addressed at the config-driven model itself",
			"model_override": "agent:o2a-ref-agent-config",
		}))
		require.NoError(t, err,
			"a config-driven instance's own model is addressable under the reserved name, "+
				"without anybody having registered it: %v", err)
		require.Contains(t, out, "CONFIG-MODEL-ANSWER")
		require.GreaterOrEqual(t, own.calls(), 1,
			"the reserved name must be served by the actual model of the selected execution view")
	})

	t.Run("custom registration occupying the reserved name is refused by name", func(t *testing.T) {
		foreign := &viewRecorderModel{label: "o2a-foreign", resp: percallFinalResp("FOREIGN-ANSWER")}
		RegisterModelReference("agent:o2a-ref-agent-conflict", foreign)
		own := &viewRecorderModel{label: "o2a-own-conflict", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-ref-agent-conflict", own,
			[]trpctool.Tool{percallLeafTool{"read_file"}})
		w := NewAgentToolWrapper(blank, "delegate", nil, nil)

		out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "work under a name the view also owns",
			"model_override": "agent:o2a-ref-agent-conflict",
		}))
		require.Error(t, err,
			"a custom registration that conflicts with the reserved name of a config-driven "+
				"model is refused, never silently served by either side")
		var shaped overrideRejectError
		require.True(t, errors.As(err, &shaped), "and the refusal must be structured, got %v", err)
		require.Equal(t, "model_override", shaped.OverrideField())
		require.Contains(t, err.Error(), "agent:o2a-ref-agent-conflict",
			"the refusal must name the conflicting reference")
		require.Nil(t, out, "a refused call produces no result")
		require.Zero(t, foreign.calls(), "the registered instance must not serve a refused call")
		require.Zero(t, own.calls(), "nor may the call fall back to the delegate's own model")

		still, ok := LookupModelReference("agent:o2a-ref-agent-conflict")
		require.True(t, ok, "a refused reference must still be readable from the registry")
		require.Same(t, foreign, still,
			"a failed candidate must not rewrite or drop the process-wide registry entry")
	})

	t.Run("unknown reference is refused without running", func(t *testing.T) {
		own := &viewRecorderModel{label: "o2a-own-unknown", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-ref-agent-unknown", own,
			[]trpctool.Tool{percallLeafTool{"read_file"}})
		w := NewAgentToolWrapper(blank, "delegate", nil, nil)

		out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "work under nobody's reference",
			"model_override": "o2a-ref-never-registered",
		}))
		require.Error(t, err, "an unregistered reference must be refused")
		var shaped overrideRejectError
		require.True(t, errors.As(err, &shaped), "and the refusal must be structured, got %v", err)
		require.Equal(t, "model_override", shaped.OverrideField())
		require.Contains(t, err.Error(), "o2a-ref-never-registered",
			"the refusal must name the offending reference")
		require.Nil(t, out)
		require.Zero(t, own.calls(),
			"an unresolvable reference never falls back to the delegate's own model")
	})
}

// TestModelOverride_GenerationBinding 钉住「一次覆盖只解析一次，且解析结果属于它那一代」：
//   - 保留名 agent:<agent> 解析的是**已选执行视图里的实际模型**：在途调用钉在发起代，随后的发布既不改写它这一代的模型，也不把它引到新代模型上；新的独立调用才读新面。
//   - 校验与装配共用同一份冻结引用集：同名引用在两阶段之间被重新注册时，装配仍跑在校验答出的那台实例上，被改指的实例一次都不出场。
//
// 契约: docs/wiki/agent/execution-generations.md#generation-wiring-window
func TestModelOverride_GenerationBinding(t *testing.T) {
	t.Run("in-flight call keeps the generation it resolved", func(t *testing.T) {
		birth := &viewRecorderModel{label: "o2a-birth", resp: percallFinalResp("BIRTH-ANSWER")}
		g1 := &viewRecorderModel{label: "o2a-g1", resp: percallFinalResp("G1-ANSWER")}
		gate := g1.armGate()
		release := func() {
			select {
			case <-gate:
			default:
				close(gate)
			}
		}
		t.Cleanup(release)

		all := []trpctool.Tool{percallLeafTool{"read_file"}, percallLeafTool{"save_file"}}
		blank := newPercallBlank(t, "o2a-bind-blank", birth, all)
		w1 := stageGeneration(t, blank, "o2a-bind-gen1", "GEN1-PROMPT", g1,
			[]trpctool.Tool{percallLeafTool{"read_file"}})

		type callOutcome struct {
			out any
			err error
		}
		done := make(chan callOutcome, 1)
		go func() {
			out, err := w1.Call(context.Background(), percallArgs(t, map[string]any{
				"request":        "work addressed at this generation's own model",
				"model_override": "agent:o2a-bind-blank",
			}))
			done <- callOutcome{out, err}
		}()

		require.Eventually(t, func() bool { return g1.calls() >= 1 }, 10*time.Second, 20*time.Millisecond,
			"the reserved name must resolve to the model of the generation this call was selected on")
		require.Zero(t, birth.calls(),
			"the reserved name is the view's model, not the construction-time definition's")
		require.Contains(t, g1.systemText(0), "GEN1-PROMPT",
			"and the call really is running on generation 1's own view")

		g2 := &viewRecorderModel{label: "o2a-g2", resp: percallFinalResp("G2-ANSWER")}
		w2 := stageGeneration(t, blank, "o2a-bind-gen2", "GEN2-PROMPT", g2, all)

		release()
		res := <-done
		require.NoError(t, res.err, "the in-flight call must finish on the view it started on")
		require.Contains(t, res.out, "G1-ANSWER")
		require.Equal(t, 1, g1.calls(), "a publish adds no round to the pinned generation")
		require.Zero(t, g2.calls(),
			"the later publish cannot reach into a call that already resolved its reference")

		out2, err := w2.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "a later independent call",
			"model_override": "agent:o2a-bind-blank",
		}))
		require.NoError(t, err)
		require.Contains(t, out2, "G2-ANSWER",
			"a new independent call reads the new face's own model under the same name")
		require.Equal(t, 1, g1.calls(), "and it never re-enters the retired generation")
	})

	t.Run("validation and assembly share one resolution", func(t *testing.T) {
		first := &viewRecorderModel{label: "o2a-two-stage-first", resp: percallFinalResp("FIRST-ANSWER")}
		later := &viewRecorderModel{label: "o2a-two-stage-later", resp: percallFinalResp("LATER-ANSWER")}
		RegisterModelReference("o2a-ref-two-stage", first)

		own := &viewRecorderModel{label: "o2a-two-stage-own", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-blank-two-stage", own,
			[]trpctool.Tool{percallLeafTool{"read_file"}})
		w := NewAgentToolWrapper(blank, "delegate", nil, nil)

		ov, err := w.parsePerCallOverrides(context.Background(), map[string]any{
			"request":        "work under a registered reference",
			"model_override": "o2a-ref-two-stage",
		})
		require.NoError(t, err, "the reference the view carries must validate")
		require.NotNil(t, ov)

		view, err := NewModelRefSnapshot("o2a-blank-two-stage", own)
		require.NoError(t, err)
		validated, err := resolveModelReference(ov.ModelRef, view)
		require.NoError(t, err)
		require.Same(t, first, validated)

		RegisterModelReference("o2a-ref-two-stage", later)

		cfg := *blank.config
		require.NoError(t, applyPerCallOverrides(&cfg, ov, view))
		require.Same(t, first, cfg.Model,
			"assembly must run on the instance the single resolution produced, not on whatever "+
				"the mutable table holds by then")
		require.NotSame(t, later, cfg.Model, "a re-pointed name cannot slip into a running call")
		require.Zero(t, later.calls(), "and the re-pointed instance never enters this call")
		require.Same(t, own, blank.config.Model,
			"the resolution is written into this call's copy of the view only")
	})
}

// stageGenerationPinned publishes one generation the way the org wiring does for a
// config-driven instance: its read-only model-reference snapshot is frozen in the
// staging window (before activation) and pinned to that generation. The wrapper
// returned is declared on THAT generation, so every call it takes resolves
// references off the snapshot that was published with it.
func stageGenerationPinned(tb testing.TB, blank *TagentAgent, label, promptText string,
	m model.Model, tools []trpctool.Tool) (*AgentToolWrapper, *ModelRefSnapshot) {
	tb.Helper()
	cm := blank.contextManager
	face := cm.ExecutorConfig()
	face.Model = m
	face.Tools = tools
	face.SystemPrompt = promptText
	runCfg := *blank.config
	runCfg.Model = m
	runCfg.Tools = tools
	runCfg.SystemPrompt = promptText
	runCfg.SystemPromptSource = nil
	staged := cm.StageExecutor(cm.NewExecutorCandidate(face), face, &runCfg)
	require.NotNil(tb, staged, "generation %s must stage a candidate", label)
	refs, err := NewModelRefSnapshot(runCfg.Name, runCfg.Model)
	require.NoError(tb, err, "generation %s must freeze the references of its own view", label)
	require.NoError(tb, PinModelReferences(staged, refs), "and pin them before activation")
	require.NotNil(tb, cm.ActivateExecutor(staged), "generation %s must activate", label)

	w := NewAgentToolWrapper(blank, "delegate to the blank agent", nil, nil)
	w.setDeclared(staged.binding)
	require.True(tb, w.declaredSet(), "the wrapper must route through generation %s", label)
	return w, refs
}

// percallRefSeq makes model-reference names unique across -count reruns: the
// registry is package-global and outlives one round, so a fixed name would
// leak the previous round's registration into this round's frozen-snapshot
// assertions.
var percallRefSeq atomic.Uint64

func percallRefName(base string) string {
	return fmt.Sprintf("%s-%d", base, percallRefSeq.Add(1))
}

// TestModelOverride_ReentryIsolation 钉住默认/覆盖/重入矩阵：覆盖层不得改变任何既有上界，也不得跨调用存活。
//   - 无覆盖：调用落在本代视图上，常驻定义逐字不变（模型、提示词、工具面都是本代自己的）；
//   - 引用只给模型选择、不给调用路由：换掉模型的调用，工具面仍是属主声明的那份上界，越域条目在参数校验点具名拒绝，两台实例一次都不出场；
//   - 覆盖随调用消亡：下一次无覆盖调用回到本代自有实例与本代视图，且解析结果从不写回进程全局注册表；
//   - 重入读自己那代的冻结快照：本代快照不含的引用被具名拒绝，既不回落本代模型也不借新代放行，新独立代才看得见新注册的名字；两次调用之后执行引用归零，没有悬挂。
//
// 契约: docs/wiki/platform/org-hot-reload.md#percall-overrides
func TestModelOverride_ReentryIsolation(t *testing.T) {
	read := percallLeafTool{"read_file"}
	save := percallLeafTool{"save_file"}

	t.Run("a call carrying no override keeps the generation view", func(t *testing.T) {
		own := &viewRecorderModel{label: "o2a-m-plain-own", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-m-plain", own, []trpctool.Tool{read, save})
		gen := &viewRecorderModel{label: "o2a-m-plain-gen", resp: percallFinalResp("PLAIN-GEN-ANSWER")}
		w, _ := stageGenerationPinned(t, blank, "o2a-m-plain-gen1", "PLAIN-GEN-PROMPT", gen,
			[]trpctool.Tool{read})

		out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request": "ordinary work, no override",
		}))
		require.NoError(t, err)
		require.Contains(t, out, "PLAIN-GEN-ANSWER")
		require.Contains(t, gen.systemText(0), "PLAIN-GEN-PROMPT")
		require.Equal(t, []string{"read_file"}, gen.toolNames(0),
			"the undeclared call sees the generation's own tool surface")
		require.Zero(t, own.calls(),
			"a generation without an override never reaches the construction-time model")
		require.Same(t, own, blank.config.Model, "and the resident definition is untouched")
		require.Len(t, blank.Tools(), 2, "nor is the resident tool declaration narrowed")
	})

	t.Run("a model reference grants no tool routing", func(t *testing.T) {
		routingRef := percallRefName("o2a-ref-routing")
		registered := &viewRecorderModel{label: "o2a-m-rt", resp: percallFinalResp("ROUTING-ANSWER")}
		RegisterModelReference(routingRef, registered)

		own := &viewRecorderModel{label: "o2a-m-rt-own", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-m-routing", own, []trpctool.Tool{read, save})
		gen := &viewRecorderModel{label: "o2a-m-rt-gen", resp: percallFinalResp("GEN-ANSWER")}
		w, _ := stageGenerationPinned(t, blank, "o2a-m-rt-gen1", "ROUTING-GEN-PROMPT", gen,
			[]trpctool.Tool{read, save})

		out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request":                "work under another model",
			"model_override":         routingRef,
			"system_prompt_override": "ROUTING-PROMPT",
			"tools_subset":           []string{"read_file"},
		}))
		require.NoError(t, err)
		require.Contains(t, out, "ROUTING-ANSWER")
		require.Equal(t, []string{"read_file"}, registered.toolNames(0),
			"the tool surface stays the owner's declared upper bound — a model reference buys no route")
		require.NotContains(t, registered.toolNames(0), "save_file")
		require.Contains(t, registered.systemText(0), "ROUTING-PROMPT")

		before := registered.calls()
		out2, err2 := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "work under another model, tools nobody declared",
			"model_override": routingRef,
			"tools_subset":   []string{"read_file", "mcp_call"},
		}))
		require.Error(t, err2, "the maximum tool domain stays a hard bound beside a model override")
		var shaped overrideRejectError
		require.True(t, errors.As(err2, &shaped), "and the refusal names the field, got %v", err2)
		require.Equal(t, "tools_subset", shaped.OverrideField())
		require.Contains(t, err2.Error(), "mcp_call")
		require.Nil(t, out2)
		require.Equal(t, before, registered.calls(), "a refused call runs nothing")
		require.Zero(t, gen.calls(), "not even on the generation's own model")
		require.Zero(t, own.calls())
	})

	t.Run("an override dies with its call", func(t *testing.T) {
		registered := &viewRecorderModel{label: "o2a-m-leak-ref", resp: percallFinalResp("OVERRIDDEN-ANSWER")}
		RegisterModelReference("o2a-ref-leak", registered)

		own := &viewRecorderModel{label: "o2a-m-leak-own", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-m-leak", own, []trpctool.Tool{read, save})
		gen := &viewRecorderModel{label: "o2a-m-leak-gen", resp: percallFinalResp("GEN-ANSWER")}
		w, _ := stageGenerationPinned(t, blank, "o2a-m-leak-gen1", "LEAK-GEN-PROMPT", gen,
			[]trpctool.Tool{read, save})

		out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request":                "overridden work",
			"model_override":         "o2a-ref-leak",
			"system_prompt_override": "LEAK-PROMPT",
			"tools_subset":           []string{"read_file"},
		}))
		require.NoError(t, err)
		require.Contains(t, out, "OVERRIDDEN-ANSWER")
		require.Equal(t, 1, registered.calls())

		out2, err2 := w.Call(context.Background(), percallArgs(t, map[string]any{
			"request": "the next call carries nothing",
		}))
		require.NoError(t, err2)
		require.Contains(t, out2, "GEN-ANSWER",
			"the following call is served the generation's own model again")
		require.Contains(t, gen.systemText(0), "LEAK-GEN-PROMPT")
		require.NotContains(t, gen.systemText(0), "LEAK-PROMPT", "and the previous prompt override is gone")
		require.Len(t, gen.toolNames(0), 2, "and the previous tool narrowing is gone")
		require.Equal(t, 1, registered.calls(), "the overridden instance is not reached again")

		_, claimed := LookupModelReference(ReservedModelRef("o2a-m-leak"))
		require.False(t, claimed,
			"resolving a view's own model is a read of that view, never a write into the process registry")
		require.Same(t, own, blank.config.Model, "nor is the resident definition rewritten")
		require.Equal(t, "BASE-SHELL-PROMPT", blank.config.SystemPrompt)
	})

	t.Run("a reference the pinned view does not carry is refused, not served by a fallback", func(t *testing.T) {
		lateRef := percallRefName("o2a-ref-late")
		late := &viewRecorderModel{label: "o2a-m-late-ref", resp: percallFinalResp("LATE-ANSWER")}

		own := &viewRecorderModel{label: "o2a-m-late-own", resp: percallFinalResp("OWN-ANSWER")}
		blank := newPercallBlank(t, "o2a-m-late", own, []trpctool.Tool{read})
		gen1 := &viewRecorderModel{label: "o2a-m-late-gen1", resp: percallFinalResp("GEN1-ANSWER")}
		w1, snap1 := stageGenerationPinned(t, blank, "o2a-m-late-a", "LATE-GEN1-PROMPT", gen1,
			[]trpctool.Tool{read})

		RegisterModelReference(lateRef, late)

		_, frozen := snap1.Resolve(lateRef)
		require.False(t, frozen, "a published generation's snapshot stays frozen at what it was built with")

		out, err := w1.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "work addressed at a name this view never carried",
			"model_override": lateRef,
		}))
		require.Error(t, err, "a call resolves against ITS generation, not against the live registry")
		var shaped overrideRejectError
		require.True(t, errors.As(err, &shaped), "and the refusal is structured, got %v", err)
		require.Equal(t, "model_override", shaped.OverrideField())
		require.Contains(t, err.Error(), lateRef)
		require.Nil(t, out)
		require.Zero(t, late.calls(), "a refused reference is never served")
		require.Zero(t, gen1.calls(), "and it never falls back to the generation's own model")

		gen2 := &viewRecorderModel{label: "o2a-m-late-gen2", resp: percallFinalResp("GEN2-ANSWER")}
		w2, snap2 := stageGenerationPinned(t, blank, "o2a-m-late-b", "LATE-GEN2-PROMPT", gen2,
			[]trpctool.Tool{read})
		_, visible := snap2.Resolve(lateRef)
		require.True(t, visible, "a NEW independent generation reads the registry as it is now")

		out2, err2 := w2.Call(context.Background(), percallArgs(t, map[string]any{
			"request":        "the same work on the new face",
			"model_override": lateRef,
		}))
		require.NoError(t, err2)
		require.Contains(t, out2, "LATE-ANSWER")
		require.Zero(t, gen2.calls(),
			"the new generation's own model stays out too: the reference it resolved is what served the call")
		require.Zero(t, gen1.calls(), "and the refused generation never ran at all")

		require.Zero(t, blank.contextManager.OutstandingRefs(),
			"both calls released their execution references: a frozen view is held by nobody afterwards")
	})
}

// TestModelOverride_AssemblyReadsThePinnedGeneration 钉住变参接缝的真实消费者：
//   - 装配点解析 model_override 读的是本次调用所在代际发布时冻结的那份引用快照，与参数校验点同一次解析；
//   - 两参调用会把解析退回可变的进程表，于是校验答复 first、装配却服务 later。
func TestModelOverride_AssemblyReadsThePinnedGeneration(t *testing.T) {
	read := percallLeafTool{"read_file"}

	first := &viewRecorderModel{label: "o2a-pin-first", resp: percallFinalResp("FIRST-ANSWER")}
	RegisterModelReference("o2a-ref-pin", first)

	own := &viewRecorderModel{label: "o2a-pin-own", resp: percallFinalResp("OWN-ANSWER")}
	blank := newPercallBlank(t, "o2a-pin", own, []trpctool.Tool{read})
	gen := &viewRecorderModel{label: "o2a-pin-gen", resp: percallFinalResp("GEN-ANSWER")}
	w, snap := stageGenerationPinned(t, blank, "o2a-pin-gen1", "PIN-GEN-PROMPT", gen, []trpctool.Tool{read})

	_, held := snap.Resolve("o2a-ref-pin")
	require.True(t, held, "这一代发布时该名指向 FIRST")

	later := &viewRecorderModel{label: "o2a-pin-later", resp: percallFinalResp("LATER-ANSWER")}
	RegisterModelReference("o2a-ref-pin", later)

	out, err := w.Call(context.Background(), percallArgs(t, map[string]any{
		"request":        "work addressed at the pinned name",
		"model_override": "o2a-ref-pin",
	}))
	require.NoError(t, err, "调用在校验点命中的引用，装配不得改口拒绝")
	require.Contains(t, out, "FIRST-ANSWER",
		"装配必须落在校验答复的那一实例：调用作用域属于代际快照，不属于可变的进程表")
	require.Zero(t, later.calls(), "重新指向的实例不进入已选定在旧代上的调用")
	require.Zero(t, gen.calls(), "覆盖生效时连本代自身模型也不出场")
	require.Same(t, own, blank.config.Model, "常驻定义逐字不变")

	gen2 := &viewRecorderModel{label: "o2a-pin-gen2", resp: percallFinalResp("GEN2-ANSWER")}
	w2, snap2 := stageGenerationPinned(t, blank, "o2a-pin-gen2", "PIN-GEN2-PROMPT", gen2, []trpctool.Tool{read})
	_, nowVisible := snap2.Resolve("o2a-ref-pin")
	require.True(t, nowVisible, "新独立代读到此刻的注册表")

	out2, err2 := w2.Call(context.Background(), percallArgs(t, map[string]any{
		"request":        "the same work on the new face",
		"model_override": "o2a-ref-pin",
	}))
	require.NoError(t, err2)
	require.Contains(t, out2, "LATER-ANSWER", "新代际的服务实例随发布更新")
	require.Equal(t, 1, first.calls(), "旧实例只服务它自己那一次调用，不多不少")
	require.Zero(t, gen2.calls(), "新代自身模型同样不出场：引用解析到什么就是什么")
	require.Zero(t, blank.contextManager.OutstandingRefs(), "两次调用的执行引用都已释放")
}
