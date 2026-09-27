package tagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// §3.4 首个生产入口垂直验收（A→B / A→C）。断言对象全部是宿主可见行为：
// 真实模型请求里声明的工具集合、实际被调起的子 agent，而非常量/指针/hash。

// delegYAML renders entry "a" delegating to exactly one sub-agent.
func delegYAML(target string) string { return delegYAMLSeq(target) }

// delegYAMLSeq renders entry "a" with the entry-reachable agent set held
// constant (b and c are always both declared as tools) so only the ORDER of the
// delegation tools differs between generations — the one shape of an A→B / A→C
// switch that can publish before §4.3 (hot add/remove) lands. async:false keeps
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
	// Every agent's first call of a turn delegates to the tool it was offered;
	// its second call (after the tool result) closes that turn — so a nested
	// delegation (A→B→C) is served by the same script, at each level, against the
	// declaration THAT level was given.
	offer := ""
	if n%2 == 1 && len(tools) > 0 {
		offer = tools[0]
	}
	m.recordLocked(delegServed{System: label, Tools: tools, Answered: offer, ToolResults: toolResults})
	m.mu.Unlock()

	// Park this agent's call if the test armed a gate for it: this is the in-flight
	// window across which a generation may be published.
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

// TestOrgDelegation_TargetFollowsPublishedGenerationNotMutableGlobals is the
// change's headline acceptance: editing the EXISTING YAML (no new syntax) changes
// which sub-agent the next request can actually call, while the entry runtime —
// its store, session service and resident binding table — keeps its identity.
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

	// resident identity BEFORE the generation switch (spec: 热更不破坏常驻身份).
	storeBefore := entry.MemStore()
	tableBefore := residentCacheForTest(entry)

	// ---- turn 1: the published generation offers B ----
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "sub-agent b served the first delegation", func() bool {
		return countServed(m.snapshot(), "SUB-B-PROMPT") > 0
	})
	// Turn 边界必须显式同步（审阅 M-4）：本 mock 的第 2 次 entry 调用才收尾本 turn，
	// 而 “b 被服务过” 在第 1 次调用就成立。若不等齐就取快照，第 2 次调用可能落进
	// 第二代前缀，使“第二代声明严格等于 [c]”与“B 不再增长”两条断言因时序运气而假失败。
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

	// ---- structural reload: A→B becomes A→C ----
	//
	// §4.3 makes this shape publishable: C enters the entry-reachable topology
	// (hot add, built under the original resource/recovery protocol) and B leaves
	// it (hot remove). The published generation must BOTH OFFER and ACTUALLY SERVE
	// C; B is unrouted but keeps its resident owner (never retired early), which is
	// what makes a later same-name re-entry reuse the original storage owner.
	write(delegYAML("c"))
	entry.CheckOrgReload() // ops entry; production reaches the same path via BeginTurn

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

	// ---- resident identity across a REAL publish ----
	// 身份不漂用 Same（指针），不用 DeepEqual：换成“内容相同的新实例”正是本变更要拦的形态。
	require.Same(t, storeBefore, entry.MemStore(), "the entry store identity must not drift on a reload")
	table := residentCacheForTest(entry)
	require.Nil(t, tableBefore["c"], "c was not resident before the add — the add is real, not a pre-existing identity")
	require.NotNil(t, table["c"], "the hot-added agent is merged into the resident binding table")
	require.NotSame(t, entry.MemStore(), table["c"].MemStore(),
		"the hot-added agent owns its own store (no drift onto the entry store)")
	// §4.3 migrated the claim from "unrouted, never retired" to what actually must
	// hold across a real publish: the removed owner's identity never DRIFTS. It is
	// either still resident as the very same instance (something still needs it), or
	// it left by retirement — which must be an actual close, never a silent drop or a
	// look-alike replacement.
	bAfter, bStillHere := table["b"]
	if bStillHere {
		require.Same(t, tableBefore["b"], bAfter,
			"an unrouted owner still referenced must stay the SAME instance across the publish")
	} else {
		require.True(t, tableBefore["b"].CloseStarted(),
			"a name that left the resident table did so by being retired, not silently dropped")
	}
}

// 尚未纳入本例的第二形态（拓扑不变的 tools 顺序交换 → 实际委派目标随之改变）在
// `-race` 下首轮委派不稳定（turn 1 未走到子 agent），已删除以免留闪测；其
// 「声明与真实调用同代」断言已由上方 A→C 发布形态**部分覆盖**（拓扑变了的那一形态；「拓扑不变、仅 tools 顺序变化 → 实际目标改变」并未覆盖），顺序交换
// 变体与 U-1（上游 session 竞态）同批重建。

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

// TestOrgDelegation_InFlightDelegationKeepsItsOwnGenerationTarget is the
// subagent-turn-execution scenario「进行中热更不改变子调用目标」at the production
// entry: B's run is parked mid-call while the operator publishes the generation
// that removes B and routes C instead. The in-flight turn must still be SERVED BY
// B and hand B's answer back; C must not run until that call returned; and the
// removed target must receive no new call afterwards. What this falsifies is a
// delegation that re-reads the published face (or whose executor generation is
// reclaimed) underneath a live call.
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

	// 换代发生在 B 仍持租约的这一瞬间（ops 同步入口与业务 turn 共用同一条发布通路）。
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

	// 换代的影响边界是 turn：此后不再有 B 的调用，新目标持续服务后续请求。
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

// TestOrgDelegation_NestedLevelsServeFromTheirOwnBindings is §3.3's 多级委派 row at
// the production entry: every level's real model request must carry ITS OWN
// generation declaration, and the deepest answer must surface, level by level,
// back to the entry. A nested level that read a global agent table would show up
// here as a missing/wrong tool declaration or a result that never arrives.
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
	// 每一层看到的是它自己的声明：entry→[b]，b→[c]，c 无工具。
	require.Equal(t, []string{"b"}, snaps[firstServeIndex(snaps, "ENTRY-A-PROMPT")].Tools,
		"the entry delegates on its own binding")
	require.Equal(t, []string{"c"}, snaps[firstServeIndex(snaps, "SUB-B-PROMPT")].Tools,
		"the nested level delegates on ITS own binding, not the entry's")
	require.Empty(t, snaps[firstServeIndex(snaps, "SUB-C-PROMPT")].Tools,
		"the leaf has no delegation to offer")
	// 结果逐层回流。
	require.Greater(t, firstResultIndexBy(snaps, "SUB-B-PROMPT", "served:SUB-C-PROMPT"), firstServeIndex(snaps, "SUB-C-PROMPT"),
		"C's answer must reach its direct parent B")
	require.Greater(t, firstResultIndex(snaps, "served:SUB-B-PROMPT"), firstServeIndex(snaps, "SUB-B-PROMPT"),
		"B's answer (carrying C's work) must reach the entry")
	require.Equal(t, 1, countServed(snaps, "SUB-C-PROMPT"), "the leaf ran exactly once for this turn")
}

// TestOrgDelegation_ManagedAsyncDelegationIsAdoptedByTheTaskLayer is §3.3's 受管异步
// row at the production entry: with async left at its default, the delegation must
// really be adopted by the task layer (a spawn record exists) and still deliver its
// result inline to the requesting turn.
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

// RegisterPlainTool panics on a duplicate id, so the probe factory is installed
// once per process (the test is then re-runnable with -count=N).
var registerProbeFactory sync.Once

func registerProbeToolFactory() {
	registerProbeFactory.Do(func() {
		agent.RegisterPlainTool("r33_probe", func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
			return &probeFactoryTool{marker: "FACTORY-OK:" + cfg.ID}, nil
		})
	})
}

// TestOrgDelegation_FactoryToolIsDeclaredCalledAndReturnedFromItsGeneration is
// §3.3's 内置／工厂 row at the production entry: a kind:tool factory result must be
// offered in the REAL model request, really executed, and its return must reach the
// parent turn — from the instance the published face was built with.
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
