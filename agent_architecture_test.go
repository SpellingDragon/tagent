// 本文件负责生产入口委派的垂直验收（A→B / A→C）：断言对象全部是宿主可见行为——
// 真实模型请求里声明的工具集合、实际被调起的子 agent，而非常量、指针或 hash。
package tagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"
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
	// parked counts, per label, calls currently blocked on a gate — the direct
	// "in flight right now" edge; records land before the gate check, so record
	// counts cannot prove a call is parked.
	parked map[string]int
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

// parkEnter records that a call of `label` is now blocked on its gate.
func (m *delegModel) parkEnter(label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.parked == nil {
		m.parked = map[string]int{}
	}
	m.parked[label]++
}

// parkExit records that a parked call of `label` left its gate (released or
// context-cancelled).
func (m *delegModel) parkExit(label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.parked[label]--
}

// parkedNow reports how many calls of `label` are blocked on gates at this
// instant — the direct witness that an in-flight window is open.
func (m *delegModel) parkedNow(label string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.parked[label]
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
		m.parkEnter(label)
		select {
		case <-g:
			m.parkExit(label)
		case <-ctx.Done():
			m.parkExit(label)
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

const mod = "github.com/SpellingDragon/tagent"

// internalDeps returns the set of internal packages (module-scoped) that pkg
// transitively depends on. Uses `go list -deps` — slow-ish (a few seconds),
// acceptable for one CI job.
func internalDeps(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	deps := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == mod:
			deps["."] = true
		case strings.HasPrefix(line, mod+"/"):
			deps[strings.TrimPrefix(line, mod+"/")] = true
		}
	}
	return deps
}

func assertNoDeps(t *testing.T, pkg string, deps map[string]bool, forbidden ...string) {
	t.Helper()
	if len(forbidden) == 0 {
		t.Fatalf("assertNoDeps(%s): empty forbidden set — the assertion is a no-op (self-check against silent guard rot)", pkg)
	}
	for _, f := range forbidden {
		if deps[f] {
			t.Errorf("layer violation: %s imports %s (revised direction, introduce-durable-workflow-engine: root composes {agent → plugin → memory → event}; there is NO workflow/graph layer — see TestArch_NoSecondOrchestrationRepresentation)", pkg, f)
		}
	}
}

// assertNoDepsPrefix is the family-level guard: it flags any internal dep at or
// under each prefix (agent, agent/task, ...; memory, memory/embedder, ...), so
// the assertion does not depend on enumerating every current subpackage — new
// subpackages under a forbidden family are caught automatically. The package's
// own subtree is exempt (so asserting "workflow" on a workflow/* package never
// self-hits).
func assertNoDepsPrefix(t *testing.T, pkg string, deps map[string]bool, prefixes ...string) {
	t.Helper()
	if len(prefixes) == 0 {
		t.Fatalf("assertNoDepsPrefix(%s): empty prefix set — the assertion is a no-op", pkg)
	}
	for dep := range deps {
		if dep == pkg || strings.HasPrefix(dep, pkg+"/") {
			continue
		}
		for _, p := range prefixes {
			if dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("layer violation: %s imports %s (forbidden family %q; agent must NOT depend on any workflow/*, workflow/* must NOT depend on agent/memory/plugin/tool/root)", pkg, dep, p)
				break
			}
		}
	}
}

// TestArch_LayeredDependencyDirection asserts the layering mechanically: each package must not import the families its assertion lists.
// - The agent family is the inner runtime: it must not reach the root package nor import any workflow/* package.
// - Reception, completion, task and recovery stay inside the original protocols.
// - The org version reaches them only through the minimal injection contract owned by the root, never through an imported orchestration layer.
//
// 契约: docs/wiki/agent/agent-architecture.md#package-layout
func TestArch_LayeredDependencyDirection(t *testing.T) {
	if testing.Short() {
		t.Skip("go list -deps is not short-mode friendly")
	}

	assertNoDeps(t, mod+"/event", internalDeps(t, mod+"/event"),
		"agent", "plugin", "memory", "tool", "rl", "evolution")

	for _, pkg := range []string{mod + "/memory", mod + "/memory/kv", mod + "/memory/engine", mod + "/memory/embedder"} {
		deps := internalDeps(t, pkg)
		delete(deps, "event")
		assertNoDeps(t, pkg, deps, "agent", "plugin", "tool", "rl", "evolution")
	}

	deps := internalDeps(t, mod+"/plugin")
	delete(deps, "event")
	delete(deps, "memory")
	assertNoDeps(t, mod+"/plugin", deps, "agent", "tool", "rl", "evolution")

	assertNoDeps(t, mod+"/config", internalDeps(t, mod+"/config"), ".")
	assertNoDeps(t, mod+"/agent", internalDeps(t, mod+"/agent"), "config")

	for _, pkg := range []string{mod + "/agent", mod + "/agent/task", mod + "/agent/compress", mod + "/agent/governance", mod + "/agent/reliability"} {
		deps := internalDeps(t, pkg)
		delete(deps, "event")
		delete(deps, "memory")
		delete(deps, "plugin")
		assertNoDeps(t, pkg, deps, ".")
		assertNoDepsPrefix(t, pkg, deps, "workflow")
	}
}

// TestArch_NoSecondOrchestrationRepresentation guards that no second orchestration representation, graph-compiler package or gray dispatch key ever reappears.
func TestArch_NoSecondOrchestrationRepresentation(t *testing.T) {
	if _, err := os.Stat("workflow"); err == nil {
		t.Errorf("a top-level workflow/ package exists again: the standalone graph DSL was withdrawn (second-round scope) — orchestration must be composed by the root over the existing YAML hierarchy")
	}
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./...: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == mod+"/workflow" || strings.HasPrefix(line, mod+"/workflow/") {
			t.Errorf("module still depends on the withdrawn orchestration package %q", line)
		}
	}
}

// TestEliminationList_ZeroLegacySymbols statically checks that every mechanism on the elimination list stays absent from non-test Go files.
// - The list governs live code: a comment may record that a symbol is gone, so only non-comment lines are scanned.
// - The elimination list closes with two jointly-sufficient proofs: this static check and the three-boot-state dynamic test.
// - The static half catches deleted mechanisms left uncalled but present; the dynamic half proves prior-format data is read-not, consumed-not, wiped-not.
func TestEliminationList_ZeroLegacySymbols(t *testing.T) {
	banned := []struct{ pattern, why string }{
		{"task_stale_after", "10.5: stale observation wall deleted; TTL is the only age path"},
		{"task_job_deadline", "10.5: second age wall merged into task_default_ttl"},
		{"TaskMaxDetachedAge", "10.5: compat remapping removed, no aliases"},
		{"type SpillStore", "決策10: legacy spill overflow format removed (files are inert transitional data)"},
		{"func NewSpillStore", "決策10: same"},
		{"loopTerminated ", "6.1: replaced by the loopState machine (loopTerminatedNow helper)"},
		{"loopActive ", "6.1: same"},
		{"collectUnconfirmedReceipts", "5.7: old collect-chain replaced by direct envelope reconcile"},
		{"persistInboxReceipt(", "5.3: fresh-key receipt minting deleted (reserved-key commit only)"},
		{"ConfirmDurableByRequestID", "5.4: bare request-ID confirm removed (verified receipt credential only)"},
		{"ReconcileDurableReceipts", "5.7: harvest-style receipt reconcile removed (per-envelope fixed-key reconcile)"},
		{"PathForReceiptKey", "5.7: receipt-key→path reverse index removed (envelopes carry their own fixed key)"},
		{"ReceiptNote", "5.4: free-text receipt note replaced by reliability.ReceiptCredential"},
		{"ErrLegacySpillNotDrained", "3.7: legacy drain-as-boot-precondition removed (inert transitional data)"},
		{"checkUpgradeGates", "3.7: whole-tree upgrade gate removed (only current-format quarantine blocks reopen)"},
		{"ReadyCh", "3.2: zero-consumer cold-start readiness signal removed"},
	}
	// RECURSIVE over the whole repo (review 7677c07 #1: top-level-only scans
	// left agent/reliability etc. unguarded — the exact places old mechanisms
	// would resurrect). Dot-dirs, openspec docs and tests are excluded.
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "openspec") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, b := range banned {
		for _, f := range files {
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			for i, line := range strings.Split(string(src), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				require.NotContains(t, line, b.pattern,
					"§8.7 elimination list: %s must not appear in live code (%s) — found in %s:%d",
					b.pattern, b.why, f, i+1)
			}
		}
	}
}

const triPhaseEnv = "TAGENT_TRI_PHASE"

// TestLatestPathOnly_ThreeBootStates runs the dynamic half: three independent boots, each driving one real durable turn.
// - The three states (fresh, restart over the current state, post-reset) run as independent processes: a boot is only evidenced by a real process start.
// - Same-process multi-generation runner boot/Close churn is avoided: it triggers a framework-internal state-lifecycle race.
// - No production path performs that churn, so running each state in its own process costs no coverage.
// - Prior-format markers are planted only after the first child exited, so no live writer can consume them.
// - The parent orchestrates dirs, marker planting and the managed reset between the children.
func TestLatestPathOnly_ThreeBootStates(t *testing.T) {
	if phase := os.Getenv(triPhaseEnv); phase != "" {
		triChildPhase(t, phase)
		return
	}
	root := t.TempDir()
	env := append(os.Environ(),
		"TAGENT_TRI_STORE="+filepath.Join(root, "store"),
		"TAGENT_TRI_SPILL="+filepath.Join(root, "spill"),
		"TAGENT_TRI_ANCHOR="+filepath.Join(root, "anchor"))
	runChild := func(phase string) {
		runBootChild(t, env, triPhaseEnv+"="+phase, "TestLatestPathOnly_ThreeBootStates$")
	}

	runChild("1")

	spill := filepath.Join(root, "spill")
	legacyV1 := filepath.Join(spill, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyV1), 0o755))
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(spill, "tagent", "job.spill"), []byte("spill"), 0o644))

	runChild("2")
	require.Equal(t, `{"version":1}`, readFile(t, legacyV1), "legacy v1 item stays inert across a real restart")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "anchor"), 0o755))
	_, err := drillResetManagedUnits(filepath.Join(root, "store"), spill, filepath.Join(root, "anchor"), "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "the authorized reset cleared the legacy set")

	runChild("3")
}

// triRaceOnlyFramework CLASSIFIES a child failure whose EVERY DATA RACE block involves
// zero tagent frames - a trpc-agent-go lifecycle race family known from upstream.
//
// - Diagnostic label only: it never suppresses a child failure; runBootChild fails acceptance on ANY DATA RACE regardless of this verdict.
// - Deliberately conservative: one tagent frame anywhere, or any assertion or panic, yields false.
// - Every race block stack must be framework-internal; the two registered families are exempt as a family because product code holds zero references to the raced queues.
// - Only the accessor sections are scanned: the trailing created-at origin stacks inevitably name ancestor test frames and must not veto.
func triRaceOnlyFramework(out []byte) bool {
	text := string(out)
	if !strings.Contains(text, "DATA RACE") || !strings.Contains(text, "--- FAIL") {
		return false
	}
	steerFamily := []string{"internal/state/steer.(*Queue).Close", "cloneState"}
	sessionFamily := []string{"session.(*Session).Clone", "UpdateUserSession"}
	famOK := familyExemptionEnabled()
	matchesAll := func(block string, sig []string) bool {
		for _, fr := range sig {
			if !strings.Contains(block, fr) {
				return false
			}
		}
		return true
	}
	for _, block := range strings.Split(text, "WARNING: DATA RACE")[1:] {
		block, _, _ = strings.Cut(block, "==================")
		if famOK && (matchesAll(block, steerFamily) || matchesAll(block, sessionFamily)) {
			continue
		}
		if i := strings.Index(block, "created at:"); i >= 0 {
			block = block[:i]
		}
		if strings.Contains(block, "github.com/SpellingDragon/tagent") {
			return false
		}
	}
	inFail := false
	inOrigin := false
	for _, ln := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(ln, "--- FAIL"):
			inFail, inOrigin = true, false
			continue
		case ln == "FAIL", ln == "PASS", strings.HasPrefix(ln, "ok "), strings.HasPrefix(ln, "--- PASS"), strings.HasPrefix(ln, "=== "):
			inFail, inOrigin = false, false
			continue
		}
		if !inFail {
			continue
		}
		if strings.HasPrefix(ln, "panic:") || strings.HasPrefix(ln, "fatal error:") {
			return false
		}
		if t := strings.TrimSpace(ln); t == "" {
			continue
		}
		if strings.Contains(ln, "created at:") {
			inOrigin = true
			continue
		}
		if inOrigin && strings.HasPrefix(ln, "  ") {
			continue
		}
		if t := strings.TrimSpace(ln); !strings.Contains(t, "race detected during execution of test") {
			return false
		}
	}
	return true
}

// registeredFamilyVersion is the trpc-agent-go release the exempted framework
// race families (steer.Queue.Close × invocation-state clone; session.Clone) were
// verified against. The exemption is bound to it so a framework upgrade can
// never silently widen or mislabel the exempt set: bump go.mod and the family
// exemption lapses until the races are re-registered here. Both families are
// FIXED upstream in v1.11.2 (steer: #1926 queue-cancel signal and #2165/#2462
// clone elimination; session: UpdateUserSession's EventMu widened over
// UpdatedAt) — the registration stays at v1.10.0 deliberately, so on the
// upgraded link the exemption stays LAPSED and any reappearance fails as an
// unknown (new) race.
const registeredFamilyVersion = "v1.10.0"

// familyExemptionEnabled gates the registered-race-family exemption on the linked
// framework version. A var so the classifier self-test deterministically
// exercises the upgrade (mismatch) path.
var familyExemptionEnabled = func() bool {
	return trpcAgentVersion() == registeredFamilyVersion
}

// trpcAgentVersion reports the linked trpc-agent-go module version, preferring
// build info and falling back to the go.mod require (the deterministic pin) when
// the test binary carries no dependency list. "" only when neither is readable.
func trpcAgentVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "trpc.group/trpc-go/trpc-agent-go" {
				return d.Version
			}
		}
	}
	return trpcAgentVersionFromGoMod()
}

// trpcAgentVersionFromGoMod reads the exact base-module require line
// ("trpc.group/trpc-go/trpc-agent-go vX") from go.mod, ignoring submodule
// requires whose path extends the base (…/model/provider etc.).
func trpcAgentVersionFromGoMod() string {
	src, err := os.ReadFile("go.mod")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(src), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) == 2 && f[0] == "trpc.group/trpc-go/trpc-agent-go" {
			return f[1]
		}
	}
	return ""
}

// TestTriRaceOnlyFrameworkClassifier exercises the diagnostic label only; acceptance is decided by TestBootChildVerdictNoExemption.
// - The family exemption is version-bound: it lapses once the linked trpc-agent-go differs from the registered release.
// - The exemption predicate is stubbed here and restored via t.Cleanup, so a panicking assertion cannot leak the stub.
func TestTriRaceOnlyFrameworkClassifier(t *testing.T) {
	race := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX (0.1s)\n    testing.go:1: race detected during execution of test\nFAIL\n"
	require.True(t, triRaceOnlyFramework([]byte(race)), "pure framework race is classified as the upstream family")
	tagentFrame := strings.Replace(race, "trpc.group/x/runner.Close()", "github.com/SpellingDragon/tagent/agent.go:1 x()", 1)
	origin := strings.Replace(race, "FAIL\n", "Goroutine 1 (running) created at:\n  github.com/SpellingDragon/tagent/test.go:1 t()\nFAIL\n", 1)
	require.True(t, triRaceOnlyFramework([]byte(origin)), "created-at ancestor test frames do not change the framework-family classification")
	require.False(t, triRaceOnlyFramework([]byte(tagentFrame)), "an unknown race with a tagent frame is never classified as framework-only")
	fam := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/trpc-go/trpc-agent-go/internal/state/steer.(*Queue).Close()\n==================\nRead at 0x1:\n  trpc.group/x/agent.cloneStateReflectValue()\n  github.com/SpellingDragon/tagent/agent.executionGateModel.GenerateContentIter.func1()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n"
	require.True(t, triRaceOnlyFramework([]byte(fam)), "family signature + wrapper frame is classified as framework (when the family is registered)")
	assertion := strings.Replace(race, "testing.go:1: race detected during execution of test", "Error: Should be true", 1)
	require.False(t, triRaceOnlyFramework([]byte(assertion)), "an assertion failure is never exempt")
	require.False(t, triRaceOnlyFramework([]byte("--- FAIL: TestX\n    Error: boom\n")), "non-race failure not exempt")

	blankHide := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n\n    main_test.go:99: Error: want 1 got 2\nFAIL\n"
	require.False(t, triRaceOnlyFramework([]byte(blankHide)), "a failure after a blank line is never exempt")

	panicHide := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\npanic: runtime error: index out of range\n\tgx/y.go:1 +0x1\nexit status 2\n"
	require.False(t, triRaceOnlyFramework([]byte(panicHide)), "a panic is never exempt behind a race")

	familyWithTagent := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/trpc-go/trpc-agent-go/internal/state/steer.(*Queue).Close()\n  trpc.group/x/agent.cloneStateReflectValue()\n  github.com/SpellingDragon/tagent/agent.executionGateModel.GenerateContentIter.func1()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n"
	require.False(t, familyExemptionEnabled(), "linked trpc-agent-go must NOT equal the v1.10.0 registration after the upgrade: both families are fixed upstream, so the classifier's family branch is lapsed and a reappearance is labeled unknown")
	orig := familyExemptionEnabled
	t.Cleanup(func() { familyExemptionEnabled = orig })
	familyExemptionEnabled = func() bool { return true }
	require.True(t, triRaceOnlyFramework([]byte(familyWithTagent)), "at the registered version the classifier recognizes the family + wrapper frame")
	familyExemptionEnabled = func() bool { return false }
	require.False(t, triRaceOnlyFramework([]byte(familyWithTagent)), "when the version no longer matches, the classifier stops recognizing the family + wrapper frame")
}

// TestBootChildVerdictNoExemption asserts the boot-child verdict rejects every data race shape, including the pure-upstream family.
func TestBootChildVerdictNoExemption(t *testing.T) {
	exit := errors.New("exit status 1")
	frameworkRace := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX (0.1s)\n    testing.go:1: race detected during execution of test\nFAIL\n"
	require.True(t, triRaceOnlyFramework([]byte(frameworkRace)), "classifier labels the pure-upstream family")
	ok, _ := childOutcome([]byte(frameworkRace), exit)
	require.False(t, ok, "§6.6: a pure-upstream race must FAIL acceptance (the old exempt path returned true)")

	originRace := strings.Replace(frameworkRace, "FAIL\n", "Goroutine 1 (running) created at:\n  github.com/SpellingDragon/tagent/test.go:1 t()\nFAIL\n", 1)
	ok, _ = childOutcome([]byte(originRace), exit)
	require.False(t, ok, "created-at ancestor tagent frames do not rescue a race from failing")

	tagentRace := strings.Replace(frameworkRace, "trpc.group/x/runner.Close()", "github.com/SpellingDragon/tagent/agent.go:1 x()", 1)
	ok, _ = childOutcome([]byte(tagentRace), exit)
	require.False(t, ok, "a race with a tagent accessor frame fails")

	assertionOnly := "--- FAIL: TestX\n    main_test.go:9: Error: want 1 got 2\nFAIL\n"
	ok, _ = childOutcome([]byte(assertionOnly), exit)
	require.False(t, ok, "a non-race child failure still fails")

	clean := "=== RUN   TestX\n--- PASS: TestX (0.00s)\nPASS\nok  \tgithub.com/SpellingDragon/tagent\t0.01s\n"
	ok, note := childOutcome([]byte(clean), nil)
	require.True(t, ok, "a clean child passes: "+note)
}

// childOutcome decides a boot child's acceptance: NO data race — pure-upstream
// family included — may pass; every DATA RACE fails. triRaceOnlyFramework is
// consulted ONLY to label a sighting for triage; it does not change the verdict.
// Non-race non-zero exits also fail.
func childOutcome(out []byte, err error) (ok bool, note string) {
	if bytes.Contains(out, []byte("DATA RACE")) {
		if triRaceOnlyFramework(out) {
			return false, "known upstream framework race family (diagnostic label only; §6.6: any race still fails acceptance)"
		}
		return false, "unclassified data race"
	}
	if err != nil {
		return false, "child exited non-zero without a race"
	}
	return true, ""
}

// runBootChild executes a boot child process and requires a CLEAN run: zero data
// races and a zero exit. The acceptance path keeps NO race exemption — a race of
// any stack shape fails with the full log. v1.11.2 fixed the steer/session
// families upstream, and since producer-done is not a stream-close, tagent must
// not paper over lifecycle races either.
func runBootChild(t *testing.T, baseEnv []string, kv string, testFilter string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", testFilter, "-test.timeout", "120s")
	cmd.Env = append(baseEnv, kv)
	out, err := cmd.CombinedOutput()
	require.NotContainsf(t, string(out), "no tests to run",
		"boot child filter %q matched no test — the gate would pass vacuously:\n%s", testFilter, out)
	ok, note := childOutcome(out, err)
	require.Truef(t, ok, "boot child must pass with ZERO races (§6.6: no exemption) — %s:\n%s", note, out)
}

// triChildPhase runs one boot state inside its own process.
//
// driveTurn is local: it starts the loop, injects and waits for the model sighting, and
// returns a wait that drains the output reader — the caller must Close the agent before
// calling it. The child finishes by returning, so the test binary exits normally.
func triChildPhase(t *testing.T, phase string) {
	storeDir := os.Getenv("TAGENT_TRI_STORE")
	spillDir := os.Getenv("TAGENT_TRI_SPILL")
	anchorDir := os.Getenv("TAGENT_TRI_ANCHOR")
	boot := func(m *drillModel) *agent.TagentAgent {
		ta, err := New(Config{
			Entry: "tagent",
			Agents: map[string]AgentConfig{"tagent": {
				SystemPrompt: PromptConfig{Inline: "three-state"},
				MaxTokens:    4000,
				Memory:       MemoryConfig{Type: "localfile", Path: storeDir},
			}},
			Reliability: ReliabilityConfig{BusSpillDir: spillDir, MeditationAnchorDir: anchorDir},
		}, WithModel(m))
		require.NoError(t, err)
		return ta
	}
	driveTurn := func(ta *agent.TagentAgent, m *drillModel, tag string) func() {
		out, err := ta.StartLoop("u", "tri-session")
		require.NoError(t, err)
		drop := make(chan struct{})
		go func() {
			defer close(drop)
			for range out {
			}
		}()
		_, err = ta.InjectMessageContext(context.Background(), "user", model.NewUserMessage(tag))
		require.NoError(t, err)
		require.Eventually(t, func() bool { return drillSaw(m, tag) }, 30*time.Second, 20*time.Millisecond,
			"boot must run the CURRENT path to the model (tag %s)", tag)
		return func() { <-drop }
	}
	switch phase {
	case "1":
		m := &drillModel{}
		ta := boot(m)
		wait := driveTurn(ta, m, "tri-fresh-turn")
		require.NoError(t, ta.Close())
		wait()
	case "2":
		m := &drillModel{}
		ta := boot(m)
		wait := driveTurn(ta, m, "tri-restart-turn")
		require.True(t, drillSaw(m, "tri-fresh-turn"), "current-state facts ARE recovered into the projection")
		require.NoError(t, ta.Close())
		wait()
		legacy := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
		require.Equal(t, `{"version":1}`, readFile(t, legacy), "legacy v1 item is inert: never read, rewritten or removed")
	case "3":
		m := &drillModel{}
		ta := boot(m)
		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, agent.ReconcileSummary{}, s, "reset-then-boot recovers nothing (authorized discard, no half-state)")
		wait := driveTurn(ta, m, "tri-postreset-turn")
		require.False(t, drillSaw(m, "tri-fresh-turn") || drillSaw(m, "tri-restart-turn"),
			"post-reset projection carries NO pre-reset input — clearing old data is never passed off as processed")
		require.NoError(t, ta.Close())
		wait()
	default:
		t.Fatalf("unknown phase %q", phase)
	}
	os.Exit(0)
}

func drillSaw(m *drillModel, sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.reqs {
		for _, msg := range req {
			if strings.Contains(msg.Content, sub) {
				return true
			}
		}
	}
	return false
}

func readFile(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}

type stubModel struct{ name string }

func (m *stubModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	return nil, nil
}
func (m *stubModel) Info() model.Info { return model.Info{Name: m.name} }

func TestResolveAgentModel_OverridesTakePrecedence(t *testing.T) {
	overrideModel := &stubModel{name: "override"}
	rc := &runtimeConfig{
		model: &stubModel{name: "default"},
		modelOverrides: map[string]model.Model{
			"tagent": overrideModel,
		},
	}

	cfg := Config{
		Provider: "openai",
		Agents: map[string]AgentConfig{
			"tagent": {Model: "glm-5"},
		},
	}

	got := rc.resolveAgentModel("tagent", cfg.Agents["tagent"], cfg)
	assert.Equal(t, "override", got.Info().Name)
}

func TestResolveAgentModel_NoModelField_UsesParent(t *testing.T) {
	parentModel := &stubModel{name: "parent"}
	rc := &runtimeConfig{
		model: parentModel,
	}

	cfg := Config{
		Provider: "openai",
		Agents: map[string]AgentConfig{
			"recall": {},
		},
	}

	got := rc.resolveAgentModel("recall", cfg.Agents["recall"], cfg)
	assert.Equal(t, "parent", got.Info().Name)
}

// TestResolveAgentModel_ResolvesFromProvider 验证 agent 只给出 provider 名时，按注册表解析出模型实例。
// - 模型构造要求 API key 非空（即便本次不发请求），故用假 key 占位。
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestResolveAgentModel_ResolvesFromProvider(t *testing.T) {
	os.Setenv("TEST_API_KEY", "test-key-123")
	defer os.Unsetenv("TEST_API_KEY")

	parentModel := &stubModel{name: "parent"}
	rc := &runtimeConfig{
		model: parentModel,
	}

	cfg := Config{
		Provider: "openai",
		Providers: map[string]ProviderConfig{
			"openai": {
				APIEndpoint: "https://api.example.com/v1",
				APIKeyEnv:   "TEST_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"knowledge": {Model: "gpt-4"},
		},
	}

	got := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	require.NotNil(t, got)
	assert.NotEqual(t, "parent", got.Info().Name)
}

// TestResolveAgentModel_CachesResolvedModels 验证解析结果按模型复用，同名模型不会构造出多个实例。
// - 两个 agent 故意共用同一 model 名；去掉这份重复会让本测失去意义。
func TestResolveAgentModel_CachesResolvedModels(t *testing.T) {
	os.Setenv("TEST_API_KEY", "test-key-456")
	defer os.Unsetenv("TEST_API_KEY")

	rc := &runtimeConfig{
		model: &stubModel{name: "parent"},
	}

	cfg := Config{
		Provider: "openai",
		Providers: map[string]ProviderConfig{
			"openai": {
				APIEndpoint: "https://api.example.com/v1",
				APIKeyEnv:   "TEST_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"knowledge": {Model: "gpt-4"},
			"recall":    {Model: "gpt-4"},
		},
	}

	got1 := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	got2 := rc.resolveAgentModel("recall", cfg.Agents["recall"], cfg)

	assert.Same(t, got1, got2)
}

func TestResolveAgentModel_AgentProviderOverridesGlobal(t *testing.T) {
	os.Setenv("TEST_KEY_A", "key-a")
	os.Setenv("TEST_KEY_B", "key-b")
	defer os.Unsetenv("TEST_KEY_A")
	defer os.Unsetenv("TEST_KEY_B")

	rc := &runtimeConfig{
		model: &stubModel{name: "parent"},
	}

	cfg := Config{
		Provider: "openai",
		Providers: map[string]ProviderConfig{
			"openai": {
				APIEndpoint: "https://api-a.example.com/v1",
				APIKeyEnv:   "TEST_KEY_A",
			},
			"anthropic": {
				APIEndpoint: "https://api-b.example.com",
				APIKeyEnv:   "TEST_KEY_B",
			},
		},
		Agents: map[string]AgentConfig{
			"knowledge": {
				Model:    "claude-3",
				Provider: "anthropic",
			},
		},
	}

	got := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	require.NotNil(t, got)
	assert.NotEqual(t, "parent", got.Info().Name)
}

func TestResolveAgentModel_FallsBackOnProviderError(t *testing.T) {
	rc := &runtimeConfig{
		model: &stubModel{name: "parent"},
	}

	cfg := Config{
		Provider: "nonexistent_provider",
		Agents: map[string]AgentConfig{
			"knowledge": {Model: "some-model"},
		},
	}

	got := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	assert.Equal(t, "parent", got.Info().Name)
}

func TestConfig_ApplyDefaults_SetsProvider(t *testing.T) {
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {},
		},
	}
	cfg.ApplyDefaults()
	assert.Equal(t, "openai", cfg.Provider)
}

func TestConfig_ProviderConfigParsing(t *testing.T) {
	yamlData := `
provider: anthropic
providers:
  anthropic:
    api_endpoint: "https://api.anthropic.com"
    api_key_env: "ANTHROPIC_API_KEY"
  openai:
    api_endpoint: "https://api.openai.com/v1"
    api_key_env: "OPENAI_API_KEY"
agents:
  tagent:
    provider: anthropic
    model: claude-3
  knowledge:
    model: gpt-4
entry: tagent
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	cfg.ApplyDefaults()
	assert.Equal(t, "anthropic", cfg.Provider)
	assert.Len(t, cfg.Providers, 2)
	assert.Equal(t, "https://api.anthropic.com", cfg.Providers["anthropic"].APIEndpoint)
	assert.Equal(t, "ANTHROPIC_API_KEY", cfg.Providers["anthropic"].APIKeyEnv)
	assert.Equal(t, "anthropic", cfg.Agents["tagent"].Provider)
	assert.Equal(t, "", cfg.Agents["knowledge"].Provider)
}

// TestTencentProvider_Hy3Model verifies that the tencent provider (OpenAI-compatible) can call the hy3 model.
func TestTencentProvider_Hy3Model(t *testing.T) {
	apiKey := os.Getenv("TENCENT_API_KEY")
	if apiKey == "" {
		t.Skip("TENCENT_API_KEY not set, skipping integration test")
	}

	cfg := Config{
		Provider: "tencent",
		Providers: map[string]ProviderConfig{
			"tencent": {
				Provider:    "openai",
				APIEndpoint: "https://tokenhub.tencentmaas.com/v1",
				APIKeyEnv:   "TENCENT_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"test": {
				Provider: "tencent",
				Model:    "hy3",
			},
		},
	}
	cfg.ApplyDefaults()

	rc := &runtimeConfig{}
	resolvedModel := rc.resolveAgentModel("test", cfg.Agents["test"], cfg)
	require.NotNil(t, resolvedModel, "model should be resolved")

	ctx := context.Background()
	req := &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "你好，请用一句话介绍自己"},
		},
	}

	respCh, err := resolvedModel.GenerateContent(ctx, req)
	require.NoError(t, err, "GenerateContent should not error")

	var fullContent string
	for resp := range respCh {
		if resp != nil && len(resp.Choices) > 0 {
			fullContent += resp.Choices[0].Message.Content
		}
	}

	assert.NotEmpty(t, fullContent, "response content should not be empty")
	t.Logf("hy3 model response: %s", fullContent)
}

func TestConfig_ProviderConfigWithProtocolField(t *testing.T) {
	yamlData := `
provider: zhipu
providers:
  zhipu:
    provider: openai           # 智谱 GLM 使用 OpenAI 兼容协议
    api_endpoint: "https://open.bigmodel.cn/api/paas/v4"
    api_key_env: "ZAI_API_KEY"
  deepseek:
    provider: openai           # DeepSeek 也使用 OpenAI 兼容协议
    api_endpoint: "https://api.deepseek.com/v1"
    api_key_env: "DEEPSEEK_API_KEY"
  anthropic:
    provider: anthropic        # Anthropic 使用原生协议
    api_endpoint: "https://api.anthropic.com"
    api_key_env: "ANTHROPIC_API_KEY"
agents:
  tagent:
    provider: zhipu
    model: glm-5
  knowledge:
    provider: deepseek
    model: deepseek-chat
  action:
    provider: anthropic
    model: claude-3
entry: tagent
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	cfg.ApplyDefaults()

	assert.Equal(t, "zhipu", cfg.Provider)
	assert.Len(t, cfg.Providers, 3)

	assert.Equal(t, "openai", cfg.Providers["zhipu"].Provider)
	assert.Equal(t, "https://open.bigmodel.cn/api/paas/v4", cfg.Providers["zhipu"].APIEndpoint)
	assert.Equal(t, "ZAI_API_KEY", cfg.Providers["zhipu"].APIKeyEnv)

	assert.Equal(t, "openai", cfg.Providers["deepseek"].Provider)
	assert.Equal(t, "https://api.deepseek.com/v1", cfg.Providers["deepseek"].APIEndpoint)
	assert.Equal(t, "DEEPSEEK_API_KEY", cfg.Providers["deepseek"].APIKeyEnv)

	assert.Equal(t, "anthropic", cfg.Providers["anthropic"].Provider)
	assert.Equal(t, "https://api.anthropic.com", cfg.Providers["anthropic"].APIEndpoint)
	assert.Equal(t, "ANTHROPIC_API_KEY", cfg.Providers["anthropic"].APIKeyEnv)

	assert.Equal(t, "zhipu", cfg.Agents["tagent"].Provider)
	assert.Equal(t, "deepseek", cfg.Agents["knowledge"].Provider)
	assert.Equal(t, "anthropic", cfg.Agents["action"].Provider)
}
func TestConfig_ResolveAgentProvider(t *testing.T) {
	yamlData := `
provider: zhipu
api_endpoint: "https://open.bigmodel.cn/api/paas/v4"
api_key_env: "ZAI_API_KEY"
providers:
  zhipu:
    provider: openai
    api_endpoint: "https://open.bigmodel.cn/api/paas/v4"
    api_key_env: "ZAI_API_KEY"
  tencent:
    provider: openai
    api_endpoint: "https://tokenhub.tencentmaas.com/v1"
    api_key_env: "TENCENT_API_KEY"
agents:
  tagent:
    provider: tencent
    model: hy3
  knowledge:
    model: glm-5
entry: tagent
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	cfg.ApplyDefaults()

	endpoint, apiKeyEnv, err := cfg.ResolveAgentProvider("tagent")
	require.NoError(t, err)
	assert.Equal(t, "https://tokenhub.tencentmaas.com/v1", endpoint)
	assert.Equal(t, "TENCENT_API_KEY", apiKeyEnv)

	endpoint, apiKeyEnv, err = cfg.ResolveAgentProvider("knowledge")
	require.NoError(t, err)
	assert.Equal(t, "https://open.bigmodel.cn/api/paas/v4", endpoint)
	assert.Equal(t, "ZAI_API_KEY", apiKeyEnv)

	endpoint, apiKeyEnv, err = cfg.ResolveAgentProvider("")
	require.NoError(t, err)
	assert.Equal(t, "https://open.bigmodel.cn/api/paas/v4", endpoint)
	assert.Equal(t, "ZAI_API_KEY", apiKeyEnv)

	_, _, err = cfg.ResolveAgentProvider("nonexistent")
	require.Error(t, err)
}

// declTool 是仅用于契约矩阵声明的最小 tool.Tool（只满足 Declaration()）。
type declTool struct{ d trpctool.Declaration }

func (t declTool) Declaration() *trpctool.Declaration { return &t.d }

// contractObservation 汇聚一次 GenerateContent 流式调用的全部可观测事实。
type contractObservation struct {
	content      string
	reasoning    string
	chunks       int
	usageSeen    bool
	promptTokens int
	complTokens  int
	toolCalls    []model.ToolCall
	finishReason string
	// apiErr 承载 resp.Error：API 级错误（如 tool_choice 被拒），区别于 GenerateContent 返回的函数级错误（连接/鉴权）。
	apiErr string
}

// chatOnce 用真实模型跑一次请求并收集观测。调用方负责预算控制（MaxTokens/prompt 长度）。
//
// 内容增量在流式下位于 Delta，非流式或终块位于 Message；部分适配器只在终块 Message 给全
// 文本，故两处都取且以较长者为准。tool_calls 与 reasoning 同样双取。
func chatOnce(t *testing.T, m model.Model, req *model.Request) (*contractObservation, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return nil, err
	}
	obs := &contractObservation{}
	for resp := range ch {
		if resp == nil {
			continue
		}
		obs.chunks++
		if resp.Error != nil {
			obs.apiErr = resp.Error.Message
			if obs.apiErr == "" {
				obs.apiErr = "<non-nil resp.Error>"
			}
		}
		if resp.Usage != nil && (resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0) {
			obs.usageSeen = true
			if resp.Usage.PromptTokens > obs.promptTokens {
				obs.promptTokens = resp.Usage.PromptTokens
			}
			if resp.Usage.CompletionTokens > obs.complTokens {
				obs.complTokens = resp.Usage.CompletionTokens
			}
		}
		if len(resp.Choices) == 0 {
			continue
		}
		c := resp.Choices[0]
		if c.FinishReason != nil && *c.FinishReason != "" {
			obs.finishReason = *c.FinishReason
		}
		obs.content += c.Delta.Content
		if c.Message.Content != "" && !resp.IsPartial {
			if c.Message.Content != obs.content && len(c.Message.Content) > len(obs.content) {
				obs.content = c.Message.Content
			}
		}
		if r := c.Delta.ReasoningContent; r != "" {
			obs.reasoning += r
		}
		if c.Message.ReasoningContent != "" {
			obs.reasoning += c.Message.ReasoningContent
		}
		for _, tc := range append(append([]model.ToolCall{}, c.Delta.ToolCalls...), c.Message.ToolCalls...) {
			if tc.Function.Name != "" || len(tc.Function.Arguments) > 0 {
				obs.toolCalls = append(obs.toolCalls, tc)
			}
		}
	}
	return obs, nil
}

// newDeepSeekModel 按已授权配置构造真实模型；未授权返回 nil + skip。
func newDeepSeekModel(t *testing.T) model.Model {
	t.Helper()
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY 未设置：3.8 真实模型契约矩阵未授权 → SKIP（不记 PASS）")
	}
	modelID := os.Getenv("DEEPSEEK_MODEL")
	if modelID == "" {
		modelID = "deepseek-flash"
	}
	cfg := Config{
		Provider: "deepseek",
		Providers: map[string]ProviderConfig{
			"deepseek": {
				Provider:    "openai",
				APIEndpoint: "https://api.deepseek.com/v1",
				APIKeyEnv:   "DEEPSEEK_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"matrix": {Provider: "deepseek", Model: modelID},
		},
	}
	cfg.ApplyDefaults()
	rc := &runtimeConfig{}
	m := rc.resolveAgentModel("matrix", cfg.Agents["matrix"], cfg)
	require.NotNil(t, m, "model 应被解析（配置正确但未授权时会先 skip）")
	t.Logf("契约矩阵目标模型: %s @ %s", modelID, "https://api.deepseek.com/v1")
	return m
}

// TestModelContractMatrix_DeepSeek 用真实远端 endpoint 验证 ReAct 与常驻管线所依赖的模型协议契约，六个观测面各对应一个同名子测。
// - 授权门：DEEPSEEK_API_KEY 未设则整组 t.Skip，明确记 SKIP，绝不记 PASS。
// - 预算门：三次真实调用（文本、强制 tool_call、工具结果回环），每次 MaxTokens≤128、prompt 极短；usage、流式、reasoning 复用第一次调用的观测。
// - 判定纪律：非空文本、usage>0、tool_calls 参数为合法 JSON 属核心契约，真实模型不满足即 FAIL 并作为发现上报。
// - 模型特定能力（分块流式、reasoning_content）按观测记录：不具备只记 SKIP/note，既不冒充 PASS 也不误判 FAIL。
// - 端点参数差异由 apiErr 承接：deepseek-flash 的 thinking 模式不接受强制 tool_choice（实测 400），故请求侧显式关思考并指名工具。
func TestModelContractMatrix_DeepSeek(t *testing.T) {
	m := newDeepSeekModel(t)
	mt := 128

	obs1, err := chatOnce(t, m, &model.Request{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "你是简洁的助手，用一句话回答。"},
			{Role: model.RoleUser, Content: "1+1 等于几？只回答数字。"},
		},
		GenerationConfig: model.GenerationConfig{MaxTokens: &mt, Temperature: ptrF(0), Stream: true},
	})
	require.NoError(t, err, "GenerateContent 函数级错误（连接/鉴权）")

	t.Run("text_generation", func(t *testing.T) {
		require.NotEmpty(t, obs1.content, "非流式/终块也未取到文本内容")
		t.Logf("文本响应=%q finish=%q chunks=%d", obs1.content, obs1.finishReason, obs1.chunks)
	})

	t.Run("usage_accounting", func(t *testing.T) {
		require.True(t, obs1.usageSeen, "响应未携带非零 usage（无法驱动压缩阈值）")
		assert.Greater(t, obs1.promptTokens, 0, "prompt_tokens 应为正")
		assert.Greater(t, obs1.complTokens, 0, "completion_tokens 应为正")
		t.Logf("usage: prompt=%d completion=%d", obs1.promptTokens, obs1.complTokens)
	})

	t.Run("streaming", func(t *testing.T) {
		require.GreaterOrEqual(t, obs1.chunks, 1, "GenerateContent 应至少产出一个响应块且正常关闭通道")
		if obs1.chunks <= 1 {
			t.Skipf("endpoint 本次仅返回单块（未分块流式），流式增量契约不据此判 FAIL：chunks=%d", obs1.chunks)
		}
		t.Logf("流式增量确认：chunks=%d", obs1.chunks)
	})

	t.Run("reasoning_content_passthrough", func(t *testing.T) {
		if obs1.reasoning == "" {
			t.Skip("该模型/端点本次未返回 reasoning_content：非推理能力缺失即 SKIP，不记 PASS/FAIL")
		}
		t.Logf("reasoning 透传确认：len=%d（且未破坏 content 解析）", len(obs1.reasoning))
	})

	weatherTool := declTool{d: trpctool.Declaration{
		Name:        "get_weather",
		Description: "查询指定城市当前天气",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"city": {Type: "string", Description: "城市名"},
			},
			Required: []string{"city"},
		},
	}}
	obs2, err := chatOnce(t, m, &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "北京现在天气怎么样？"},
		},
		GenerationConfig: model.GenerationConfig{MaxTokens: &mt, Temperature: ptrF(0)},
		Tools:            map[string]trpctool.Tool{"get_weather": weatherTool},
		ExtraFields: map[string]any{
			"thinking":    map[string]any{"type": "disabled"},
			"tool_choice": "required",
		},
	})
	if err != nil {
		t.Fatalf("tool_calls 请求函数级错误（真实契约发现）: %v", err)
	}

	var called model.ToolCall
	for _, tc := range obs2.toolCalls {
		if tc.Function.Name != "" {
			called = tc
			break
		}
	}
	t.Run("native_tool_calls", func(t *testing.T) {
		t.Logf("诊断: chunks=%d finish=%q apiErr=%q content=%q raw_toolcalls=%d",
			obs2.chunks, obs2.finishReason, obs2.apiErr, obs2.content, len(obs2.toolCalls))
		if obs2.apiErr != "" {
			t.Skipf("端点对指名 tool_choice 返回 API 级错误（能力/参数差异，非本测试可判定为缺陷）: %s", obs2.apiErr)
		}
		require.NotEmpty(t, obs2.toolCalls, "强制指名工具仍无 tool_calls：ReAct 无法驱动，属真实契约缺陷")
		require.NotEmpty(t, called.Function.Name, "tool_call 缺少 function.name")
		assert.Equal(t, "get_weather", called.Function.Name)
		require.True(t, json.Valid(called.Function.Arguments),
			"tool_call 参数必须是合法 JSON（否则框架无法反序列化执行），实得=%q", string(called.Function.Arguments))
		t.Logf("tool_call: name=%s args=%s", called.Function.Name, string(called.Function.Arguments))
	})

	if called.Function.Name == "" {
		t.Skip("无可用 tool_call，跳过工具结果回环（依赖调用 2 的产物，不凭空构造）")
	}
	roundTrip := []model.Message{
		{Role: model.RoleUser, Content: "北京现在天气怎么样？"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{called}},
		{Role: model.RoleTool, ToolID: called.ID, ToolName: called.Function.Name, Content: `{"city":"北京","temp":"21C","sky":"晴"}`},
	}
	obs3, err := chatOnce(t, m, &model.Request{
		Messages:         roundTrip,
		GenerationConfig: model.GenerationConfig{MaxTokens: &mt, Temperature: ptrF(0)},
		Tools:            map[string]trpctool.Tool{"get_weather": weatherTool},
	})
	require.NoError(t, err, "工具结果回环请求函数级错误")
	t.Run("tool_result_roundtrip", func(t *testing.T) {
		require.NotEmpty(t, obs3.content, "送回工具结果后未得续答内容：多轮 tool 契约不成立")
		t.Logf("回环续答=%q", obs3.content)
	})
}

func ptrF(v float64) *float64 { return &v }

var testStoreRoots sync.Map

// testStore 把单元测试的 memory store 路径挪出仓库工作树，并按测试用例隔离。
//
// - 必须显式挪出：resources.acquire 在按 type 分派之前就 MkdirAll 并取写锁，type localfile 还另建 relations.journal。
// - 同一用例内同名 store 返回同一绝对路径；不同用例落在不同根，互不串存储。
// 契约: docs/wiki/agent/agent-architecture.md#test-support
func testStore(t testing.TB, name string) string {
	t.Helper()
	if root, ok := testStoreRoots.Load(t); ok {
		return filepath.Join(root.(string), name)
	}
	root := t.TempDir()
	testStoreRoots.Store(t, root)
	t.Cleanup(func() { testStoreRoots.Delete(t) })
	return filepath.Join(root, name)
}

// TestTestStore_IsolatesPerCase pins the isolation contract of the test-store helper.
// - Different cases never share a store root, while one case keeps a stable path for the same store name.
// - That stability is the identity premise for restart simulation and multi-generation rendering.
// - A root shared by PID plus a fixed name would land two cases in one directory where they see each other's bytes.
func TestTestStore_IsolatesPerCase(t *testing.T) {
	var aPath string
	t.Run("caseA_writes", func(t *testing.T) {
		aPath = testStore(t, "probe")
		require.NoError(t, os.MkdirAll(aPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(aPath, "marker"), []byte("A"), 0o644))
		require.Equal(t, aPath, testStore(t, "probe"),
			"the same case+name must resolve to the same store (restart / multi-generation identity)")
	})
	t.Run("caseB_mustNotSeeA", func(t *testing.T) {
		bPath := testStore(t, "probe")
		require.NotEqual(t, aPath, bPath, "sibling cases must not share a store root")
		_, err := os.Stat(filepath.Join(bPath, "marker"))
		require.Error(t, err, "case B must not observe case A's bytes")
	})
}
