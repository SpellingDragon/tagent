// 本文件负责生产入口委派的垂直验收（A→B / A→C）：断言对象全部是宿主可见行为——
// 真实模型请求里声明的工具集合、实际被调起的子 agent，而非常量、指针或 hash。
package tagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// delegYAML renders entry "a" delegating to exactly one sub-agent.
func delegYAML(target string) string { return delegYAMLSeq(target) }

// delegYAMLSeq renders entry "a" with the entry-reachable agent set held
// constant (b and c are always both declared as tools) so only the ORDER of the
// delegation tools differs between generations — the one shape of an A→B / A→C
// switch that can publish without hot add/remove support. async:false keeps
// the delegation inside the driving turn (no task-layer detour). No
// model/providers section: the host-injected mock model (WithModel) serves every
// agent, which is what makes the delegation observable at the model boundary.
func delegYAMLSeq(targets ...string) string {
	var toolLines string
	for _, t := range targets {
		toolLines += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %s\n        async: false\n", t, "delegate-"+t)
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A-PROMPT\"\n    max_tool_iterations: 2\n    memory:\n      type: memory\n    tools:\n" + toolLines +
		`  b:
    system_prompt:
      inline: "SUB-B-PROMPT"
    memory:
      type: memory
  c:
    system_prompt:
      inline: "SUB-C-PROMPT"
    memory:
      type: memory
`
}

// delegModel records, per served request, WHO was calling (system prompt),
// WHICH tools that caller was offered, and what it answered. The first call of
// the entry agent issues a tool call to whatever tool the entry was offered —
// so the delegation target is chosen by the published declaration, exactly as a
// real model would choose it.
type delegModel struct {
	mu      sync.Mutex
	served  []delegServed
	perCall map[string]int
	// gates parks a labeled agent's call until the test closes the channel — the
	// handle on 「这个委派还在途」 that the cross-publication assertions need.
	gates map[string]chan struct{}
}

type delegServed struct {
	System   string
	Tools    []string
	Answered string
	// ToolResults are the tool-role message contents the caller received, i.e.
	// whose answer this call is being handed — the in-flight target witness.
	ToolResults []string
}

// gateFor returns the park channel for a caller label (nil = answer immediately).
func (m *delegModel) gateFor(label string) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gates[label]
}

func (m *delegModel) recordLocked(s delegServed) {
	m.served = append(m.served, s)
}

func (m *delegModel) snapshot() []delegServed {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]delegServed, len(m.served))
	copy(out, m.served)
	return out
}

func delegSystemOf(req *model.Request) string {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem {
			return msg.Content
		}
	}
	return ""
}

func delegToolNames(req *model.Request) []string {
	var names []string
	for _, t := range req.Tools {
		if d := t.Declaration(); d != nil {
			names = append(names, d.Name)
		}
	}
	return names
}

// delegLabel normalizes the caller identity: the framework decorates a system
// prompt after a tool round, so an agent is recognized by the LEADING token of its
// prompt (the decoration is appended, never prefixed) and reported under one
// stable label — which is what lets per-call parity work at every nesting level.
func delegLabel(sys string) string {
	if f := strings.Fields(sys); len(f) > 0 {
		return f[0]
	}
	return sys
}

// GenerateContent is the delegation mock's whole script: every agent's first call of a
// turn delegates to the tool it was offered, and its second call (after the tool
// result) closes that turn — so a nested A→B→C is served by the same script at each
// level, against the declaration THAT level was given.
//
// A call parks first when the test armed a gate for that agent: the parked window is
// the in-flight span across which a generation may be published.
func (m *delegModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)
	var toolResults []string
	for _, msg := range req.Messages {
		if msg.Role == model.RoleTool && msg.Content != "" {
			toolResults = append(toolResults, msg.Content)
		}
	}

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n%2 == 1 && len(tools) > 0 {
		offer = tools[0]
	}
	m.recordLocked(delegServed{System: label, Tools: tools, Answered: offer, ToolResults: toolResults})
	m.mu.Unlock()

	if g := m.gateFor(label); g != nil {
		select {
		case <-g:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

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

func (m *delegModel) Info() model.Info { return model.Info{Name: "deleg-model"} }

// waitFor polls until cond holds or the deadline passes (turn completion is
// observed through the model boundary, which is the host-visible edge).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 20*time.Second, 20*time.Millisecond, "timeout waiting for: %s", what)
}

func countServed(snaps []delegServed, system string) int {
	n := 0
	for _, s := range snaps {
		if s.System == system {
			n++
		}
	}
	return n
}

func entryDeclarations(snaps []delegServed) [][]string {
	var out [][]string
	for _, s := range snaps {
		if s.System == "ENTRY-A-PROMPT" {
			out = append(out, s.Tools)
		}
	}
	return out
}

// TestOrgDelegation_TargetFollowsPublishedGenerationNotMutableGlobals 钉住 委派目标解析自已发布代的声明面，编辑既有配置即改变下一个请求真正可调的子 agent。
// - 入口运行时——存储、会话服务、常驻绑定表——在一次真实发布前后按指针保持身份，不是"内容相同的新实例"；
// - 第一代只被提供 b，未引用的 c 一次都不跑；结构发布让 A→B 变 A→C，新代既声明也确实服务 C；
// - 被移除的 B 得不到任何新调用，却保留其常驻属主，使同名再入复用原存储属主。
// - turn 边界必须显式等到 entry 的第二次调用：不等齐，第二次调用可能落进第二代前缀，使声明与停止增长两条断言因时序运气假失败。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestOrgDelegation_TargetFollowsPublishedGenerationNotMutableGlobals(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(delegYAML("b"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "deleg-session")
	require.NoError(t, err)
	drop := make(chan struct{})
	go func() {
		defer close(drop)
		for range out {
		}
	}()

	storeBefore := entry.MemStore()
	tableBefore := residentCacheForTest(entry)

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "sub-agent b served the first delegation", func() bool {
		return countServed(m.snapshot(), "SUB-B-PROMPT") > 0
	})
	waitFor(t, "turn 1 closed (entry made its second call)", func() bool {
		return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2
	})

	snaps := m.snapshot()
	decls := entryDeclarations(snaps)
	require.NotEmpty(t, decls, "the entry agent must have been called")
	for i, d := range decls {
		require.Equal(t, []string{"b"}, d, "entry call %d must be offered exactly the configured target b", i)
	}
	require.Zero(t, countServed(snaps, "SUB-C-PROMPT"), "an agent the config does not reference must never run")

	write(delegYAML("c"))
	entry.CheckOrgReload()

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second request"))
	require.NoError(t, err)
	waitFor(t, "the newly added agent c serves the second delegation", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT") > 0
	})
	waitFor(t, "turn 2 closed (entry made its fourth call)", func() bool {
		return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 4
	})

	after := m.snapshot()
	firstTurnEntryCalls := len(entryDeclarations(snaps))
	secondTurn := entryDeclarations(after)[firstTurnEntryCalls:]
	require.NotEmpty(t, secondTurn, "the second turn must have reached the entry model")
	for i, d := range secondTurn {
		require.Equal(t, []string{"c"}, d,
			"second-turn entry call %d must be offered the published generation's target c", i)
	}
	require.Equal(t, countServed(snaps, "SUB-B-PROMPT"), countServed(after, "SUB-B-PROMPT"),
		"the removed agent must stop receiving calls once the new generation is published")

	require.Same(t, storeBefore, entry.MemStore(), "the entry store identity must not drift on a reload")
	table := residentCacheForTest(entry)
	require.Nil(t, tableBefore["c"], "c was not resident before the add — the add is real, not a pre-existing identity")
	require.NotNil(t, table["c"], "the hot-added agent is merged into the resident binding table")
	require.NotSame(t, entry.MemStore(), table["c"].MemStore(),
		"the hot-added agent owns its own store (no drift onto the entry store)")
	bAfter, bStillHere := table["b"]
	if bStillHere {
		require.Same(t, tableBefore["b"], bAfter,
			"an unrouted owner still referenced must stay the SAME instance across the publish")
	} else {
		require.True(t, tableBefore["b"].CloseStarted(),
			"a name that left the resident table did so by being retired, not silently dropped")
	}
}

// firstServeIndex returns the index of the first recorded call made BY `system`,
// or -1 when that agent never ran.
func firstServeIndex(snaps []delegServed, system string) int {
	for i, s := range snaps {
		if s.System == system {
			return i
		}
	}
	return -1
}

// firstResultIndexBy returns the index of the first call made BY `system` that was
// handed a tool result containing `want`, or -1. Together with firstServeIndex it
// turns the recording into an ORDERING witness — which ran before what — instead of
// relying on how many turns happened to be pulled (a published generation also
// raises its own `[system-alert]` notice turn, which may itself delegate).
func firstResultIndexBy(snaps []delegServed, system, want string) int {
	for i, s := range snaps {
		if s.System != system {
			continue
		}
		for _, tr := range s.ToolResults {
			if strings.Contains(tr, want) {
				return i
			}
		}
	}
	return -1
}

// firstResultIndex is the entry-scoped form of firstResultIndexBy.
func firstResultIndex(snaps []delegServed, want string) int {
	return firstResultIndexBy(snaps, "ENTRY-A-PROMPT", want)
}

// TestOrgDelegation_InFlightDelegationKeepsItsOwnGenerationTarget 钉住 在途子调用跨一次发布仍由它开始那一代的目标服务。
// - B 停在途中时发布移除 B、改路由 C 的新一代，该回合必须由 B 服务并把 B 的答案恰好回给发起回合一次；
// - 新目标 C 在 B 返回之前不得抢跑——被证伪的形状是委派在活调用底下重读已发布面、或其执行代被回收；
// - 换代影响止于本 turn：此后请求由 C 持续服务，被移除的 B 得不到新调用。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestOrgDelegation_InFlightDelegationKeepsItsOwnGenerationTarget(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(delegYAML("b"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	bGate := make(chan struct{})
	m := &delegModel{gates: map[string]chan struct{}{"SUB-B-PROMPT": bGate}}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "inflight-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "B entered and parked mid-call", func() bool {
		return countServed(m.snapshot(), "SUB-B-PROMPT") == 1
	})

	write(delegYAML("c"))
	entry.CheckOrgReload()
	require.Nil(t, entry.ContextManager().SubagentWrapper("b"),
		"precondition: the published generation really stopped routing b (the reload is not a no-op)")
	require.NotNil(t, entry.ContextManager().SubagentWrapper("c"), "…and routes c instead")

	mid := m.snapshot()
	require.Zero(t, countServed(mid, "SUB-C-PROMPT"),
		"a mid-call publication must not steal the in-flight delegation")

	close(bGate)
	waitFor(t, "the parent turn received B's answer", func() bool {
		return firstResultIndex(m.snapshot(), "served:SUB-B-PROMPT") >= 0
	})

	after := m.snapshot()
	require.Equal(t, 1, countServed(after, "SUB-B-PROMPT"),
		"the in-flight delegation is served exactly once, by the target it started with")
	bReturned := firstResultIndex(after, "served:SUB-B-PROMPT")
	cRan := firstServeIndex(after, "SUB-C-PROMPT")
	require.True(t, cRan < 0 || cRan > bReturned,
		"the new target must not run while the old generation's call is in flight (B returned at %d, first C call at %d)",
		bReturned, cRan)

	cBefore := countServed(after, "SUB-C-PROMPT")
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second request"))
	require.NoError(t, err)
	waitFor(t, "the next request is served by the new target", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT") > cBefore
	})
	require.Equal(t, 1, countServed(m.snapshot(), "SUB-B-PROMPT"),
		"a removed target receives no new call once the new generation is in force")
}

// nestedYAML wires A→B→C: the entry delegates to b, and b — under its OWN
// generation binding — delegates to c.
func nestedYAML() string {
	return `entry: a
agents:
  a:
    system_prompt:
      inline: "ENTRY-A-PROMPT"
    max_tool_iterations: 2
    memory:
      type: memory
    tools:
      - kind: agent
        agent: b
        description: delegate-b
        async: false
  b:
    system_prompt:
      inline: "SUB-B-PROMPT"
    max_tool_iterations: 2
    memory:
      type: memory
    tools:
      - kind: agent
        agent: c
        description: delegate-c
        async: false
  c:
    system_prompt:
      inline: "SUB-C-PROMPT"
    memory:
      type: memory
`
}

// TestOrgDelegation_NestedLevelsServeFromTheirOwnBindings 钉住 多级委派每一层带它自己那代的声明、最深结果逐层回流。
// - entry 的请求带 [b]、嵌套层 b 的请求带它自己的 [c] 而非 entry 的、叶 c 无委派可给；
// - C 的答案先回到直接父 B、B 携 C 的工作回到 entry，叶在本 turn 恰好跑一次；
// - 读全局 agent 表会在此暴露为缺失或错误的工具声明、或结果永不抵达。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestOrgDelegation_NestedLevelsServeFromTheirOwnBindings(t *testing.T) {
	yamlPath := writeYAML(t, nestedYAML())
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "nested-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("root request"))
	require.NoError(t, err)
	waitFor(t, "the deepest level ran", func() bool { return countServed(m.snapshot(), "SUB-C-PROMPT") >= 1 })
	waitFor(t, "the entry turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })

	snaps := m.snapshot()
	require.Equal(t, []string{"b"}, snaps[firstServeIndex(snaps, "ENTRY-A-PROMPT")].Tools,
		"the entry delegates on its own binding")
	require.Equal(t, []string{"c"}, snaps[firstServeIndex(snaps, "SUB-B-PROMPT")].Tools,
		"the nested level delegates on ITS own binding, not the entry's")
	require.Empty(t, snaps[firstServeIndex(snaps, "SUB-C-PROMPT")].Tools,
		"the leaf has no delegation to offer")
	require.Greater(t, firstResultIndexBy(snaps, "SUB-B-PROMPT", "served:SUB-C-PROMPT"), firstServeIndex(snaps, "SUB-C-PROMPT"),
		"C's answer must reach its direct parent B")
	require.Greater(t, firstResultIndex(snaps, "served:SUB-B-PROMPT"), firstServeIndex(snaps, "SUB-B-PROMPT"),
		"B's answer (carrying C's work) must reach the entry")
	require.Equal(t, 1, countServed(snaps, "SUB-C-PROMPT"), "the leaf ran exactly once for this turn")
}

// TestOrgDelegation_ManagedAsyncDelegationIsAdoptedByTheTaskLayer 钉住 async 取默认时委派确实被任务层收养并仍把结果 inline 交回请求回合。
// - spawn 记录存在且键为 agent:request 形状，证明确实过了任务层而非同步旁路；
// - 结算结果回到发起 turn，位于子 agent 被服务之后。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestOrgDelegation_ManagedAsyncDelegationIsAdoptedByTheTaskLayer(t *testing.T) {
	asyncYAML := strings.Replace(delegYAML("b"), "        async: false\n", "", 1)
	require.NotContains(t, asyncYAML, "async:", "precondition: async is left at its default")
	yamlPath := writeYAML(t, asyncYAML)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "async-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("root request"))
	require.NoError(t, err)
	waitFor(t, "the sub-agent ran", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") >= 1 })
	waitFor(t, "the entry turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })

	// 真的过了任务层（不是同步旁路）：spawn 记录存在且键为 agent:request 形状。
	var keys []string
	for _, tk := range entry.TaskManager().List() {
		keys = append(keys, tk.Spec.Key)
	}
	require.Contains(t, keys, "b:work", "an async-enabled delegation must be adopted by the task layer")

	snaps := m.snapshot()
	require.Greater(t, firstResultIndex(snaps, "served:SUB-B-PROMPT"), firstServeIndex(snaps, "SUB-B-PROMPT"),
		"the settled result must be delivered back into the requesting turn")
}

// probeFactoryTool is a kind:tool (builtin/factory) shape instance: it answers
// with a payload only IT can produce, so the parent's tool result identifies which
// built object served the call.
type probeFactoryTool struct{ marker string }

func (p *probeFactoryTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "r33_probe", Description: "probe", InputSchema: &trpctool.Schema{Type: "object"}}
}
func (p *probeFactoryTool) Call(_ context.Context, _ []byte) (any, error) { return p.marker, nil }

// registerProbeFactory guards the one-time tool registration: RegisterPlainTool
// panics on a duplicate id, so the probe factory is installed once per process
// (the test is then re-runnable with -count=N).
var registerProbeFactory sync.Once

func registerProbeToolFactory() {
	registerProbeFactory.Do(func() {
		agent.RegisterPlainTool("r33_probe", func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
			return &probeFactoryTool{marker: "FACTORY-OK:" + cfg.ID}, nil
		})
	})
}

// TestOrgDelegation_FactoryToolIsDeclaredCalledAndReturnedFromItsGeneration 钉住 kind:tool 工厂产物在其所建之代被声明、被真实执行、返回值回到父回合。
// - 工厂结果出现在真实模型请求的工具面里、被真正调用、其返回（只有那个被构建对象能产出的载荷）回到父 turn；
// - 服务调用的正是已发布面构建时持有的那个实例。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestOrgDelegation_FactoryToolIsDeclaredCalledAndReturnedFromItsGeneration(t *testing.T) {
	registerProbeToolFactory()

	yamlPath := writeYAML(t, `entry: a
agents:
  a:
    system_prompt:
      inline: "ENTRY-A-PROMPT"
    max_tool_iterations: 2
    memory:
      type: memory
    tools:
      - kind: tool
        id: r33_probe
`)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "factory-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("root request"))
	require.NoError(t, err)
	waitFor(t, "the entry turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })

	snaps := m.snapshot()
	require.Equal(t, []string{"r33_probe"}, snaps[0].Tools, "the factory tool is offered as declared")
	require.Equal(t, "r33_probe", snaps[0].Answered, "the model really called it")
	require.Greater(t, firstResultIndex(snaps, "FACTORY-OK:r33_probe"), 0,
		"the factory instance's own return must reach the parent turn")
}

// remoteAnswer is the distinctive payload the stand-in service returns. Finding it
// back in the parent's tool-result message proves the delegation really crossed
// the wire and came back — nothing local could have produced this string.
const remoteAnswer = "REMOTE-A2A-ANSWER::42"

// remoteService is a protocol-faithful A2A endpoint stand-in: agent card at
// /.well-known/agent*.json, JSON-RPC on every other path. Each RPC body is
// recorded verbatim, so assertions run against what ACTUALLY went over the wire
// (endpoint usage, message text, transferred state) rather than an internal field.
type remoteService struct {
	mu       sync.Mutex
	rpcs     []map[string]any
	cardHits int
	// failFirstRPC answers this many RPC calls with a transport error before it
	// starts succeeding — the shape the A2A retry branch exists for.
	failFirstRPC int
	// onFailure runs inside the handler of a TRANSIENT (503) attempt, before the
	// error is answered. A test uses it to publish exactly DURING the retry
	// window, so the window is synchronous to the failure rather than a sleep.
	onFailure func()
	srv       *httptest.Server
}

// newRemoteService stands in for a remote A2A peer: it serves the agent card, counts
// RPCs, and fails the first failFirstRPC requests at transport level.
//
// A transport failure must surface from the client Run instead of being answered
// silently — that error is the trigger the remote retry branch waits for. The failure
// hook fires while the in-flight attempt is being retried, so a publish can be timed
// to exactly that window.
func newRemoteService(t *testing.T) *remoteService {
	t.Helper()
	r := &remoteService{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "well-known") {
			r.mu.Lock()
			r.cardHits++
			r.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":                 "knowledge",
				"description":          "remote knowledge service",
				"url":                  r.srv.URL,
				"version":              "1.0.0",
				"capabilities":         map[string]any{"streaming": false},
				"defaultInputModes":    []string{"text/plain"},
				"defaultOutputModes":   []string{"text/plain"},
				"skills":               []any{},
				"preferredTransport":   "JSONRPC",
				"protocolVersion":      "1.0",
				"additionalInterfaces": []any{},
			})
			return
		}
		var rpc map[string]any
		if err := json.NewDecoder(req.Body).Decode(&rpc); err != nil {
			http.Error(w, "bad rpc", http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.rpcs = append(r.rpcs, rpc)
		n := len(r.rpcs)
		transient := n <= r.failFirstRPC
		r.mu.Unlock()
		if transient {
			if hook := r.failureHook(); hook != nil {
				hook()
			}
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      rpc["id"],
			"result": map[string]any{
				"kind":      "message",
				"messageId": "remote-msg-1",
				"role":      "agent",
				"parts":     []any{map[string]any{"kind": "text", "text": remoteAnswer}},
			},
		})
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// failureHook reads the armed transient-failure hook under the same lock the
// handler uses for its bookkeeping (the mutex is already released by the time the
// caller is about to answer 503, so the hook must not be invoked while holding it).
func (r *remoteService) failureHook() func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.onFailure
}

func (r *remoteService) rpcCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rpcs)
}

func (r *remoteService) snapshotRPCs() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.rpcs...)
}

// wireModel scripts ONE real turn: the first call delegates to whatever tool the
// entry was actually offered (so the target is chosen by the published
// declaration, exactly as a real model chooses), later calls close the turn. Every
// request is captured — that is where the tool result comes back into view.
type wireModel struct {
	mu       sync.Mutex
	requests []*model.Request
	args     string
}

func (m *wireModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func (m *wireModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	n := len(m.requests)
	args := m.args
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if n == 1 {
		var names []string
		for _, tl := range req.Tools {
			if d := tl.Declaration(); d != nil {
				names = append(names, d.Name)
			}
		}
		if len(names) > 0 {
			ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
				Role: model.RoleAssistant,
				ToolCalls: []model.ToolCall{{Type: "function", ID: "call-1", Function: model.FunctionDefinitionParam{
					Name: names[0], Arguments: []byte(args)}}},
			}}}}
			close(ch)
			return ch, nil
		}
	}
	ch <- &model.Response{Done: true, Choices: []model.Choice{{
		Message: model.NewAssistantMessage("entry-final")}}}
	close(ch)
	return ch, nil
}

func (m *wireModel) Info() model.Info { return model.Info{Name: "wire-model"} }

func offeredToolNames(req *model.Request) []string {
	var out []string
	for _, tl := range req.Tools {
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// toolResultsOf returns the tool-role message contents of a request — the parent's
// view of what the delegation actually returned.
func toolResultsOf(req *model.Request) []string {
	var out []string
	for _, msg := range req.Messages {
		if msg.Role == model.RoleTool && msg.Content != "" {
			out = append(out, msg.Content)
		}
	}
	return out
}

// remoteYAML renders an entry that delegates to exactly one agent, declared ONLY
// as a remote endpoint. `extra` is appended to the tool block (event_params etc.).
func remoteYAML(endpoint, extra string) string {
	return fmt.Sprintf(`entry: a
agents:
  a:
    system_prompt:
      inline: "ENTRY-A-PROMPT"
    max_tool_iterations: 2
    memory:
      type: memory
    tools:
      - kind: agent
        agent: knowledge
        description: delegate-knowledge
        async: false
%s
        remote:
          url: %q
`, extra, endpoint)
}

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tagent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// driveOneTurn starts the loop, injects one request and waits for the entry turn
// to close (two entry model calls: the delegation and the final).
func driveOneTurn(t *testing.T, entry *agent.TagentAgent, m *wireModel) {
	t.Helper()
	out, err := entry.StartLoop("u", "a2a-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("go"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(m.snapshot()) >= 2 }, 20*time.Second, 20*time.Millisecond,
		"the entry turn must close after the delegation returned")
}

// TestRemoteRefDelegatesWithRealDeclarationEndpointAndReturn 钉住 仅远程、无本地定义的子 agent 配置走通到部署级：真实声明、真实端点、真实返回。
// - 配置加载且不因缺本地定义而失败，发布的模型面在真实请求里暴露该远端委派工具；
// - 调用确实落到声明的 URL，远端答案作为 tool result 回到父 turn，本地没有任何东西能产出该串；
// - 父请求原文随委派送出到端点。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestRemoteRefDelegatesWithRealDeclarationEndpointAndReturn(t *testing.T) {
	svc := newRemoteService(t)
	yamlPath := writeYAML(t, remoteYAML(svc.srv.URL, ""))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err, "a remote-only reference must load without a local definition")
	require.NotContains(t, cfg.Agents, "knowledge", "the config really has NO local definition")

	m := &wireModel{args: `{"request":"what does the remote know?"}`}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	driveOneTurn(t, entry, m)

	require.Contains(t, offeredToolNames(m.snapshot()[0]), "knowledge",
		"the published face must offer the remote delegation to the model")

	require.Eventually(t, func() bool { return svc.rpcCount() >= 1 }, 20*time.Second, 20*time.Millisecond,
		"the delegation must actually reach the declared endpoint")

	// 真实工具返回：远端答案以 tool 结果回到父 turn（本地没有任何东西能产出这个串）。
	var sawAnswer bool
	for _, req := range m.snapshot() {
		for _, got := range toolResultsOf(req) {
			if strings.Contains(got, remoteAnswer) {
				sawAnswer = true
			}
		}
	}
	require.True(t, sawAnswer, "the remote answer must come back as the parent's tool result")

	body, _ := json.Marshal(svc.snapshotRPCs()[0])
	require.Contains(t, string(body), "what does the remote know?",
		"the delegated request text must ride to the endpoint")
}

// TestRemoteRefCarriesEventContextOverTheWire 钉住 远端委派把父存储解析出的 event_key 上下文作为 transferred state 跨线送达。
// - 远程目标看到与本地子 agent 同等的上下文，供给来自父绑定而非新绑定；
// - 上下文随跨线载荷到达端点，并按远程映射回读的那个 transferred-state 键携带。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestRemoteRefCarriesEventContextOverTheWire(t *testing.T) {
	svc := newRemoteService(t)
	yamlPath := writeYAML(t, remoteYAML(svc.srv.URL, "        event_params: [event_keys]\n"))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &wireModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// A real event in the parent's own store, referenced by key from the model's
	// tool call (the same plumbing a local sub-agent consumes).
	const key = int64(0x0a2a000000001)
	require.NoError(t, entry.MemStore().StoreEvent(key, memory.FullEvent{
		EventKey: key, EventType: "external_input",
		EventSummary: "远端上下文注入事件", Content: "远端上下文注入事件",
	}))
	m.args = fmt.Sprintf(`{"request":"analyze","event_keys":[%q]}`, tagentevent.FormatEventKey(key))

	driveOneTurn(t, entry, m)
	require.Eventually(t, func() bool { return svc.rpcCount() >= 1 }, 20*time.Second, 20*time.Millisecond,
		"the delegation must reach the endpoint before the parent can be answered")
	body, _ := json.Marshal(svc.snapshotRPCs())
	require.Contains(t, string(body), "远端上下文注入事件",
		"the parent-resolved event context must cross the wire to the remote target")
	require.Contains(t, string(body), agent.ExternalContextKey,
		"and it must travel under the transferred-state key the remote maps back")
}

// TestRemoteRefRejectsURLLessDeclaration 钉住 声明为远程却无 endpoint 的引用必须被拒绝，不得静默按本地构建。
// - 按本地构建会运行一个与配置所述不同的运行时，加载期即报错并要求 url。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestRemoteRefRejectsURLLessDeclaration(t *testing.T) {
	yamlPath := writeYAML(t, remoteYAML("", "")+"\n  knowledge:\n    system_prompt:\n      inline: \"SUB-K-PROMPT\"\n    memory:\n      type: memory\n")
	_, err := LoadConfig(yamlPath)
	require.Error(t, err, "a remote declaration without an endpoint must be refused, not built local")
	require.Contains(t, err.Error(), "requires a url")
}

// TestRemoteDelegationRetriesAgainstTheSameDeclaredTarget 钉住 远端传输失败必须对同一声明端点、同一委派载荷重试，父收到真实答案而非错误事件。
// - 传输失败驱动远端重试分支（至少两次尝试），每次尝试携带同一委派载荷；
// - 在重试路径重解析目标是被禁止的形状——全局表查询会静默做这件事。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestRemoteDelegationRetriesAgainstTheSameDeclaredTarget(t *testing.T) {
	svc := newRemoteService(t)
	svc.mu.Lock()
	svc.failFirstRPC = 1
	svc.mu.Unlock()

	yamlPath := writeYAML(t, remoteYAML(svc.srv.URL, ""))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &wireModel{args: `{"request":"retry this delegation"}`}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	driveOneTurn(t, entry, m)

	require.Eventually(t, func() bool { return svc.rpcCount() >= 2 }, 20*time.Second, 20*time.Millisecond,
		"the transport failure must drive the remote retry branch, not a silent failure")
	rpcs := svc.snapshotRPCs()
	for i, rpc := range rpcs[:2] {
		body, _ := json.Marshal(rpc)
		require.Contains(t, string(body), "retry this delegation",
			"attempt %d must carry the same delegation payload", i)
	}
	var sawAnswer bool
	for _, req := range m.snapshot() {
		for _, got := range toolResultsOf(req) {
			if strings.Contains(got, remoteAnswer) {
				sawAnswer = true
			}
		}
	}
	require.True(t, sawAnswer, "the retried attempt's answer must reach the parent turn")
}

// TestRemoteRetryAcrossPublishKeepsTheDeclaredEndpoint 钉住 重试过程中发布把同名 agent 指向不同端点时，重试仍锁在它开始声明的端点与载荷上。
// - 判别是因果的、不数全局总量：父拿到答案之前，后继端点一次都不得被联系；
// - 原端点上记录的每次尝试都携带同一委派载荷，传输重试固定端点与载荷、本地回合不重试。
// - 换代依赖 mtime 严格变新：不晚于上次成功载入则懒检测命中缓存、发布成为 no-op，故显式把 mtime 设到 +90s 而非依赖系统时钟。
// - 在途代必须在发布之前捕获（此处在重试钩子能发布之前取值），否则被断言回收的代就不是被打断的那一代。
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestRemoteRetryAcrossPublishKeepsTheDeclaredEndpoint(t *testing.T) {
	original := newRemoteService(t)
	original.mu.Lock()
	original.failFirstRPC = 1
	original.mu.Unlock()
	successor := newRemoteService(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	crossWrite(t, yamlPath, remoteYAML(original.srv.URL, ""), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &wireModel{args: `{"request":"retry across a publish"}`}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// Publish exactly during the retry window: re-point `knowledge` elsewhere.
	var published sync.Once
	original.mu.Lock()
	original.onFailure = func() {
		published.Do(func() {
			hookTick := time.Now().Add(90 * time.Second)
			require.NoError(t, os.WriteFile(yamlPath, []byte(remoteYAML(successor.srv.URL, "")), 0o644))
			require.NoError(t, os.Chtimes(yamlPath, hookTick, hookTick))
			entry.CheckOrgReload()
		})
	}
	original.mu.Unlock()

	inflightGen := entryGeneration(t, entry)
	require.GreaterOrEqual(t, inflightGen, int64(0), "precondition: the entry has an active generation")

	out, err := entry.StartLoop("u", "a2a-retry-publish-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("go"))
	require.NoError(t, err)

	// Wait for the parent to receive the answer, failing the moment the successor
	// endpoint is touched: that would mean the retry re-resolved onto G2.
	var answered bool
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !answered {
		require.Zero(t, successor.rpcCount(),
			"§3.4：重试期间发布后，重投仍须打在发起时声明的端点——后继端点被联系即说明重试改道了")
		for _, req := range m.snapshot() {
			for _, got := range toolResultsOf(req) {
				if strings.Contains(got, remoteAnswer) {
					answered = true
				}
			}
		}
		if !answered {
			time.Sleep(20 * time.Millisecond)
		}
	}
	require.True(t, answered, "the retried attempt's real answer must reach the parent turn")

	require.Eventuallyf(t, func() bool { return !hasGeneration(entry, inflightGen) },
		20*time.Second, 20*time.Millisecond,
		"the superseded generation %d that held the in-flight remote call must be reclaimed after the call lands: %+v",
		inflightGen, entry.ContextManager().ExecutorRefs().Generations)

	require.Greater(t, diagInt64(t, entry.OrgDiagnostics(), "generation"), int64(0),
		"precondition: a new generation was published during the retry")
	require.GreaterOrEqual(t, original.rpcCount(), 2,
		"the transient failure must drive a retry against the declared endpoint")
	for i, rpc := range original.snapshotRPCs() {
		body, _ := json.Marshal(rpc)
		require.Contains(t, string(body), "retry across a publish",
			"attempt %d must carry the same delegation payload across the publish", i)
	}
}
