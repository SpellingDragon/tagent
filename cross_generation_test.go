package tagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	tasktool "github.com/SpellingDragon/tagent/tool/task"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §3.4 production-entry cross-publication vertical acceptance. The hardest leg —
// a SYNCHRONOUS in-flight delegation holding its G1 target across a G2 publish —
// already lives in org_delegation_test.go (TestOrgDelegation_InFlightDelegation…).
// This file closes the remaining clauses 3.4 names but no entry test yet proves:
//   - a BACKGROUND (async-spawned) execution keeps its G1 target across the publish;
//   - the retired generation FINALLY stops and its reference is reclaimed after the
//     in-flight call lands (「旧执行最终停止／资源回收」);
//   - an INPUT THAT QUEUED behind the parked turn executes on the NEW generation
//     (「排队输入取执行时版本」), which two already-closed sequential turns cannot show;
//   - when the model calls no tool, NEITHER the old nor the new target runs
//     (「不调用工具时 B/C 都不执行」).
//
// Retry-across-publish and turn-boundary single-version acquisition are pinned at
// the agent layer (agent/turn_binding_test.go) on the same pinned-executor path the
// entry uses; they are not re-implemented here as redundant echo tests.

// entryGeneration returns the entry cm's ACTIVE (non-retired) generation id, or -1.
func entryGeneration(t *testing.T, entry *agent.TagentAgent) int64 {
	t.Helper()
	for _, g := range entry.ContextManager().ExecutorRefs().Generations {
		if !g.Retired {
			return g.Generation
		}
	}
	return -1
}

// hasGeneration reports whether a generation row with this id is still on the books
// (a retired generation disappears once its own reference count reaches zero — the
// reclaim gate reads exactly this accounting).
func hasGeneration(entry *agent.TagentAgent, id int64) bool {
	for _, g := range entry.ContextManager().ExecutorRefs().Generations {
		if g.Generation == id {
			return true
		}
	}
	return false
}

func crossWrite(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// TestOrgCrossPublish_BackgroundExecutionKeepsTargetAcrossPublish 钉住 后台生产者的委派跨发布仍跑它开始的那一代。
// - 被任务层收养的异步委派停在途中时发布新一代，新代目标不得在这次在途调用里被偷走；
// - 该委派的答案必须仍然送回发起回合，且恰好服务一次；
// - 新代可以在此之后跑（发布会抬起按当代面委派的提示回合），非法形状只有新代先于旧代答案跑。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestOrgCrossPublish_BackgroundExecutionKeepsTargetAcrossPublish(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	// async default (drop `async:false`) → the delegation is adopted by the task
	// layer and runs as a background producer, exactly the path §3.4 must cover.
	asyncB := strings.Replace(delegYAML("b"), "        async: false\n", "", 1)
	require.NotContains(t, asyncB, "async:", "precondition: async left at default")
	crossWrite(t, yamlPath, asyncB, &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	bGate := make(chan struct{})
	m := &delegModel{gates: map[string]chan struct{}{"SUB-B-PROMPT": bGate}}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "crossbg-session")
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
	waitFor(t, "the async background run entered and parked mid-call", func() bool {
		return countServed(m.snapshot(), "SUB-B-PROMPT") == 1
	})

	// Publish the C-generation while the background producer holds the G1 target.
	crossWrite(t, yamlPath, delegYAML("c"), &tick)
	entry.CheckOrgReload()
	require.Nil(t, entry.ContextManager().SubagentWrapper("b"), "precondition: b is unrouted in the new generation")

	mid := m.snapshot()
	require.Zero(t, countServed(mid, "SUB-C-PROMPT"), "a mid-call publication must not steal the in-flight background run")

	close(bGate)
	// B's answer must still surface to the requesting turn, from the target it began with.
	waitFor(t, "the background run delivered B's answer back inline", func() bool {
		return firstResultIndex(m.snapshot(), "served:SUB-B-PROMPT") >= 0
	})
	after := m.snapshot()
	require.Equal(t, 1, countServed(after, "SUB-B-PROMPT"), "the background execution is served exactly once, by G1's target")
	// C MAY run later (the publish itself raises a `[system-alert]` notice turn that
	// delegates on the now-current face), but it must not have run WHILE the G1 call
	// was in flight — so the only legal ordering is: B's answer first, then any C.
	bReturned := firstResultIndex(after, "served:SUB-B-PROMPT")
	cRan := firstServeIndex(after, "SUB-C-PROMPT")
	require.True(t, cRan < 0 || cRan > bReturned,
		"the new target must not run while the G1 background call is in flight (B returned at %d, first C at %d)", bReturned, cRan)
}

// TestOrgCrossPublish_RetiredGenerationReclaimedAfterInFlight 钉住 退役代由引用持有、引用落地后独立回收。
// - 在途调用持有它那一代时，该代必须标为退役但不得关闭——提前关闭是禁止形状；
// - 它的引用计数必须为正，且这份引用就是在途调用自己取的那一枚；
// - 调用落地、回合释放之后，这一代必须按自身账面被独立回收，不得永久留存。
// 契约: docs/wiki/agent/execution-generations.md#lease-holds-reference
func TestOrgCrossPublish_RetiredGenerationReclaimedAfterInFlight(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	crossWrite(t, yamlPath, delegYAML("b"), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	bGate := make(chan struct{})
	m := &delegModel{gates: map[string]chan struct{}{"SUB-B-PROMPT": bGate}}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "crossreclaim-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	g1 := entryGeneration(t, entry)
	require.NotEqual(t, int64(-1), g1, "the active generation is observable before the publish")

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "B entered and parked mid-call", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") == 1 })

	// Publish G2: the generation the in-flight turn rides becomes retired BUT held.
	crossWrite(t, yamlPath, delegYAML("c"), &tick)
	entry.CheckOrgReload()
	g1row := func() *agent.GenerationRefs {
		for i, g := range entry.ContextManager().ExecutorRefs().Generations {
			if g.Generation == g1 {
				return &entry.ContextManager().ExecutorRefs().Generations[i]
			}
		}
		return nil
	}
	row := g1row()
	require.NotNil(t, row, "the in-flight generation is still on the books")
	require.True(t, row.Retired, "a publish retired it")
	require.False(t, row.Closed, "but a live reference must keep it from being closed (§4.1: held, not force-closed)")
	require.Positive(t, row.Total, "held by the reference the in-flight call took")

	close(bGate)
	waitFor(t, "the in-flight turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })

	// After the call lands the reference releases; the retired generation is then
	// reclaimed on its own accounting (row disappears from the reference view).
	waitFor(t, "the retired G1 generation is reclaimed once nothing references it", func() bool {
		return !hasGeneration(entry, g1)
	})
}

// TestOrgCrossPublish_QueuedInputTakesNewGenerationAtExecution 钉住 排队输入取它开始执行时的那一代。
// - 第二条输入必须是在第一条停在途中时到达的，否则它没有真的排队，两次顺序回合证不了这条；
// - 第一条落地仍用它开始的那一代，第二条随后执行必须取发布后的新代；
// - 顺序即判据：旧代答案必须先于新代出现，反过来说明冻结发生在入队那一刻。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestOrgCrossPublish_QueuedInputTakesNewGenerationAtExecution(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	crossWrite(t, yamlPath, delegYAML("b"), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	bGate := make(chan struct{})
	m := &delegModel{gates: map[string]chan struct{}{"SUB-B-PROMPT": bGate}}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "crossqueue-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	// Turn 1 is now parked inside the B call (holding G1).
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "turn 1 parked in B", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") == 1 })

	// Queue a SECOND input while turn 1 is still in flight — it cannot execute yet.
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second request"))
	require.NoError(t, err)

	// Publish C while both the in-flight turn and the queued input are pending.
	crossWrite(t, yamlPath, delegYAML("c"), &tick)
	entry.CheckOrgReload()

	// Release turn 1: it lands on B. The queued input then executes and must take C.
	close(bGate)
	waitFor(t, "the in-flight first turn was served by B", func() bool {
		return firstResultIndex(m.snapshot(), "served:SUB-B-PROMPT") >= 0
	})
	waitFor(t, "the queued second input executed on the new generation C", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT") >= 1
	})

	after := m.snapshot()
	require.Equal(t, 1, countServed(after, "SUB-B-PROMPT"),
		"the in-flight turn stayed on its original target; the queue did not leak a B call")
	bReturned := firstResultIndex(after, "served:SUB-B-PROMPT")
	cRan := firstServeIndex(after, "SUB-C-PROMPT")
	require.True(t, cRan > bReturned,
		"the queued input must run AFTER the in-flight turn landed, on C (B returned at %d, first C at %d)", bReturned, cRan)
}

// noToolModel answers every request with a final text message and NEVER issues a tool
// call, regardless of what tools it is offered. It records HOW MANY times each caller
// (by system label) reached the model — which is how the negative control observes that
// a delegation target never actually ran (a sub-agent only reaches the model when its
// delegation is invoked).
type noToolModel struct {
	mu      sync.Mutex
	calls   map[string]int
	offered map[string][]string
}

func (m *noToolModel) record(label string, tools []string) {
	m.mu.Lock()
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	if m.offered == nil {
		m.offered = map[string][]string{}
	}
	m.calls[label]++
	if _, ok := m.offered[label]; !ok {
		m.offered[label] = tools
	}
	m.mu.Unlock()
}

func (m *noToolModel) count(label string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[label]
}

func (m *noToolModel) offeredTools(label string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offered[label]
}

func (m *noToolModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	m.record(label, delegToolNames(req))
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "final answer, no tool"}}}}
	close(ch)
	return ch, nil
}

func (m *noToolModel) Info() model.Info { return model.Info{Name: "no-tool-model"} }

// TestOrgCrossPublish_NoToolCallRunsNeitherTarget 钉住 未调用工具时被 Offer 与未被 Offer 的目标都不执行。
// - 前置必须证明确实 Offer 过、且请求确实到达模型，否则"从未运行"是空洞通过；
// - 热路径不得因某个执行器已被路由就抢先执行它——路由声明的是可用性，不是执行。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestOrgCrossPublish_NoToolCallRunsNeitherTarget(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	crossWrite(t, yamlPath, delegYAML("b"), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &noToolModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "crossnotool-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("just talk"))
	require.NoError(t, err)
	// Precondition: the entry really was offered its delegation tool (else "never ran"
	// would be vacuous) and really reached the model, but chose not to call it.
	waitFor(t, "the entry turn ran", func() bool { return m.count("ENTRY-A-PROMPT") >= 1 })
	require.Contains(t, m.offeredTools("ENTRY-A-PROMPT"), "b", "precondition: b is genuinely offered on the face")

	// Neither the offered target (b) nor an unoffered one (c) may execute.
	require.Zero(t, m.count("SUB-B-PROMPT"), "an offered-but-not-called target must not run")
	require.Zero(t, m.count("SUB-C-PROMPT"), "a target not even offered must not run")
}

// ackSettleModel is the witness §3.4's hardest row needs and the shared mock
// cannot provide: a detached run's result comes back as a USER-role
// `[task settled] … 结果: …` notification (measured — not as a tool result, so
// `delegServed.ToolResults` is blind to it). This model records, per ENTRY call,
// which tools that call was offered and whether its request carried the settle
// notification, so the row can be attributed by causality: the turn that holds
// the background answer is the settle-driven turn, and the tool set offered to
// IT is the generation that turn runs on.
type ackSettleModel struct {
	mu      sync.Mutex
	calls   []ackEntryCall
	subRuns int
	gate    chan struct{}
}

type ackEntryCall struct {
	tools   []string
	settled bool // its request contained the [task settled] notice carrying B's answer
}

func (m *ackSettleModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)
	joined := strings.Builder{}
	for _, mm := range req.Messages {
		joined.WriteString(string(mm.Role))
		joined.WriteByte(' ')
		joined.WriteString(mm.Content)
		joined.WriteByte('\n')
	}
	text := joined.String()

	m.mu.Lock()
	if label == "SUB-B-PROMPT" {
		m.subRuns++
	}
	settled := strings.Contains(text, "[task settled]") && strings.Contains(text, "served:SUB-B-PROMPT")
	offer := ""
	if label == "ENTRY-A-PROMPT" {
		if len(m.calls)%2 == 0 && len(tools) > 0 {
			offer = tools[0]
		}
		m.calls = append(m.calls, ackEntryCall{tools: tools, settled: settled})
	}
	g := m.gate
	m.mu.Unlock()

	if label == "SUB-B-PROMPT" && g != nil { // the producer parks: the parent must take the ack
		select {
		case <-g:
		case <-time.After(30 * time.Second):
		}
	}

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: "served:" + label}}}}
	}
	close(ch)
	return ch, nil
}

func (m *ackSettleModel) Info() model.Info { return model.Info{Name: "ack-settle-model"} }

func (m *ackSettleModel) snapshot() ([]ackEntryCall, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ackEntryCall(nil), m.calls...), m.subRuns
}

// TestOrgCrossPublish_SettleTurnRunsOnTheNewGeneration 钉住 回执已交、父回合已结束之后再发布：后台仍用旧代，结算回合用新代。
// - 回执必须是逼出来的而非假设：缩短包装器的密集窗口，让任务层在旧代仍停着时就分离并回执；
// - 父回合必须在手里没有旧代答案的情况下结束，这条才真的落在"已回执且父回合已结束"那一格；
// - 结算抬起的回合，工具表必须含新代目标、不含被摘掉的旧代目标，那次后台运行仍恰好跑一次；
// - 不断言新代总调用数为零——发布抬起的提示回合按当代面委派合法，判据是归属不是计数。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestOrgCrossPublish_SettleTurnRunsOnTheNewGeneration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	g1 := strings.ReplaceAll(delegYAMLSeq("b"), "        async: false\n", "")
	require.NotContains(t, g1, "async:", "precondition: async at its default, so the run is adopted by the task layer")
	crossWrite(t, yamlPath, g1, &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	bGate := make(chan struct{})
	m := &ackSettleModel{gate: bGate}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.NotNil(t, entry.ContextManager().SubagentWrapper("b"), "precondition: b is routed on G1")
	entry.ContextManager().SubagentWrapper("b").SetAsyncDenseDuration(30 * time.Millisecond)

	out, err := entry.StartLoop("u", "crosssettleturn-session")
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
	waitFor(t, "the background run entered and parked", func() bool {
		_, runs := m.snapshot()
		return runs == 1
	})

	// THE ACK HALF: the parent turn ended, and it ended holding only the ack.
	waitFor(t, "the parent turn closed on an ack", func() bool {
		calls, _ := m.snapshot()
		return len(calls) >= 2
	})
	prePublish, _ := m.snapshot()
	for _, c := range prePublish {
		require.False(t, c.settled,
			"precondition: nothing may have settled yet — otherwise this is the inline shape the earlier anchors already pin")
	}

	// Publish the generation that un-routes b while the producer is still parked.
	crossWrite(t, yamlPath, strings.ReplaceAll(delegYAMLSeq("c"), "        async: false\n", ""), &tick)
	entry.CheckOrgReload()
	require.Nil(t, entry.ContextManager().SubagentWrapper("b"),
		"precondition: the published generation really stopped routing b")

	close(bGate) // the producer stops only now: its settle raises the next entry turn

	var settleTurn *ackEntryCall
	waitFor(t, "the settle re-entered as a fresh entry turn", func() bool {
		calls, _ := m.snapshot()
		for i := range calls {
			if calls[i].settled {
				settleTurn = &calls[i]
				return true
			}
		}
		return false
	})

	require.Contains(t, settleTurn.tools, "c",
		"§3.4：ACK 之后由 task_settled 抬起的新 turn 必须按新代声明执行（其工具集未含 c ⇒ 它仍跑在旧代面上）")
	require.NotContains(t, settleTurn.tools, "b",
		"and must not still carry the superseded target as if nothing had been published")

	_, runs := m.snapshot()
	require.Equal(t, 1, runs,
		"the in-flight G1 run was neither re-run nor replaced by the publication")
}

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

// plainTextPullModel answers every request with plain text: the tests below hold a call
// open with a real LEASE, so nothing depends on model latency.
type plainTextPullModel struct{}

func (m *plainTextPullModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("ok")}}}
	close(ch)
	return ch, nil
}

func (m *plainTextPullModel) Info() model.Info { return model.Info{Name: "d52-pull"} }

// aliasSpellingYAML renders entry "main" routing sub1 (and optionally sub2 with its own
// numbers) using the requested tool-ref spelling: "explicit" writes `kind: agent`,
// "omitted" leaves the kind out — the same orchestration, spelled differently.
func aliasSpellingYAML(spelling string, keepMain int, withSub2 bool, keepSub2, maxSub2 int) string {
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

// remoteSpellingYAML is a remote-only reference (legal with no local definition at all,
// §5.44/§5.53) written with or without the explicit kind.
func remoteSpellingYAML(spelling, endpoint string) string {
	ref := "      - kind: agent\n        agent: knowledge\n        description: delegate-knowledge\n        async: false\n"
	if spelling == "omitted" {
		ref = "      - agent: knowledge\n        description: delegate-knowledge\n        async: false\n"
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    max_tool_iterations: 2\n    memory:\n      type: memory\n    tools:\n" + ref +
		fmt.Sprintf("        remote:\n          url: %q\n", endpoint)
}

func writeAliasConfig(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// TestRuntimeObjectAliasIsNotAStructuralChange 钉住 同一编排换书写形不算结构变更。
// - 省略工具引用 kind 的写法必须与显式写法落在同一代：序号不前进、不记失败、声明照旧路由；
// - 已在跑的属主身份必须原样保留，换写法不得把它换成新构造的那一个。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestRuntimeObjectAliasIsNotAStructuralChange(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	writeAliasConfig(t, yamlPath, aliasSpellingYAML("explicit", 2, false, 0, 0), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&plainTextPullModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Contains(t, entryToolNames(entry), "sub1", "precondition: the explicit spelling routes sub1")
	before := diagInt64(t, entry.OrgDiagnostics(), "generation")
	sub1Before := residentCacheForTest(entry)["sub1"]
	require.NotNil(t, sub1Before, "precondition: sub1 is a resident owner")

	// Same orchestration, other spelling.
	writeAliasConfig(t, yamlPath, aliasSpellingYAML("omitted", 2, false, 0, 0), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.Equalf(t, before, diagInt64(t, d, "generation"),
		"§5.2：别名拼写不是结构变更——换写法不得推进代际（failure=%v）", d["lastFailure"])
	require.NotContains(t, d, "lastFailure", "and the alias-only edit must not be recorded as a failure")
	require.Contains(t, entryToolNames(entry), "sub1",
		"the two spellings must resolve to the same reachable runtime object")
	require.Same(t, sub1Before, residentCacheForTest(entry)["sub1"],
		"and must not rebuild or replace the owner that is already serving")
}

// TestRemoteOnlyAliasSpellingStillPublishes 钉住 只声明远端的引用在两种书写形下都必须能热更。
// - 远端声明属运行期对象事实，两种书写都必须被接受；把它简化回只认显式，这种部署形状就会重新被拒绝；
// - 同名改指到另一个端点是结构变更，必须发布新代，且两种写法下都不得被拒绝。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestRemoteOnlyAliasSpellingStillPublishes(t *testing.T) {
	svcA := newRemoteService(t)
	svcB := newRemoteService(t)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeAliasConfig(t, yamlPath, remoteSpellingYAML("omitted", svcA.srv.URL), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&wireModel{args: `{"request":"alias spelling"}`}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.NotNil(t, entry.ContextManager().SubagentWrapper("knowledge"),
		"precondition: a remote-only ref with the kind omitted is still a legal declaration")

	// Re-point the same name at another endpoint: structural, and it must publish.
	writeAliasConfig(t, yamlPath, remoteSpellingYAML("explicit", svcB.srv.URL), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.NotZerof(t, diagInt64(t, d, "generation"),
		"§5.53 的 remote-only 放行必须对两种拼写一致，否则别名拼写的部署又被「引用未定义」永拒（failure=%v）", d["lastFailure"])
	require.NotContains(t, d, "lastFailure", "and the publish must not be refused under either spelling")
}

// TestHotAddedOwnerPullsTheRecordAfterNumericOnly 钉住 结构发布当场就把新属主接上记录源并写进回执。
// - 判别点在结构发布那一刻：新装属主此刻既要出现在回执集里，也要已绑定已提交记录；
// - 该属主被真实租约持在途中时落地一次纯数值应用，它自己的消费边界必须解析到新值，且在途边界上保持稳定；
// - 数值应用不得增加结构代，但必须算作一次完整应用。
// 契约: docs/wiki/agent/execution-generations.md#hot-source-pull-authority
func TestHotAddedOwnerPullsTheRecordAfterNumericOnly(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	writeAliasConfig(t, yamlPath, aliasSpellingYAML("explicit", 2, false, 0, 0), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&plainTextPullModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// Structural publish: sub2 joins through the hot path with its own numbers.
	writeAliasConfig(t, yamlPath, aliasSpellingYAML("explicit", 2, true, 4, 6000), &tick)
	entry.CheckOrgReload()
	sub2 := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, sub2, "precondition: sub2 became a resident owner through the hot path")
	require.Equal(t, 3000, sub2.OrgBudgetLine(), "6000×0.5 from the record it was built with")

	// The §5.59 discriminator, asserted AT the structural publish: the owner this
	// very publish installed must already be described by the receipt set and
	// already wired to the committed record. (Before the ordering fix, applyHotAll
	// ran before the candidate merge, so sub2 appeared in neither.)
	recAtPublish := diagnosticsReceipts(t, entry.OrgDiagnostics())
	require.Equalf(t, "applied", recAtPublish["sub2"].Outcome,
		"§5.2/§5.59：结构发布当轮就必须描述新装上的消费源（实得 %+v）", recAtPublish["sub2"])
	require.Equal(t, 6000, recAtPublish["sub2"].MaxTokens)

	// Hold the new owner in flight across a purely numeric edit to IT.
	held := sub2.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer held.Release()

	writeAliasConfig(t, yamlPath, aliasSpellingYAML("explicit", 2, true, 9, 8000), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.NotZero(t, diagInt64(t, d, "revision"), "the numeric-only edit is a full apply")
	require.Equalf(t, 4000, sub2.OrgBudgetLine(),
		"§5.2/§5.59：热增 owner 必须经它自己的记录源读到本轮提交值（8000×0.5）；缺记录源时它读不到")
	require.Equal(t, 9, sub2.OrgKeepRecent(), "and the same for keepRecent")

	rec := diagnosticsReceipts(t, d)
	require.Equal(t, "applied", rec["sub2"].Outcome, "the receipt describes the installed consumer")
	require.Equal(t, 8000, rec["sub2"].MaxTokens)

	// Resolution is stable across the in-flight boundary, not only after release.
	held.Release()
	require.Equal(t, 4000, sub2.OrgBudgetLine())
}

// §5.2 交叉场景（配置形状）：删除最后一个工具、参数删除与结构变更同候选、以及
// 候选后半段失败时已并入新增的回退。执行器层的并发/回收场景见
// agent/org_cross_scenario_test.go。

// TestCrossConfig_RemovingTheLastToolClearsTheDeclaration 删除最后一个工具：被清掉的
// 委派绑定不得存活到下一代的模型请求里（这曾是旧 `RebuildExecutor` 零值合并的泄漏形态）。
func TestCrossConfig_RemovingTheLastToolClearsTheDeclaration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, []string{"sub1"}, entryToolNames(entry), "baseline: exactly one delegation is offered")
	// §4.3 migrated the tail assertion below: an unrouted owner is kept only while it
	// is still needed, so the reference that makes it needed is taken here.
	keepDraining := residentCacheForTest(entry)["sub1"].ContextManager().AcquireLease(agent.LeaseSubCall)
	defer keepDraining.Release()
	genBefore := entry.OrgDiagnostics()["generation"]

	// 移除唯一工具（entry 变成零工具组织）。
	write(ownerYAML(t, nil, sub2MemDefault(t)))
	entry.CheckOrgReload()

	require.Empty(t, entryToolNames(entry),
		"removing the LAST tool must clear the declaration — a stale binding is a routing leak")
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64)-genBefore.(int64),
		"the topology delta still publishes exactly one new generation")

	// 唯一被路由的 agent 是 entry 本身；两位子 agent 应报 draining（不碰其参数）。
	rec := entry.OrgDiagnostics()["agents"].([]OrgAgentApply)
	outcomes := map[string]string{}
	for _, r := range rec {
		outcomes[r.Name] = r.Outcome
	}
	require.Equal(t, "applied", outcomes["main"])
	require.Equal(t, "draining", outcomes["sub1"], "the removed owner is reported, not silently re-parameterized")

	// 常驻 owner 保留（旧代收尾用），但已不在可路由集合。
	require.NotNil(t, residentCacheForTest(entry)["sub1"], "the removed owner stays resident")
}

// TestCrossConfig_DeletedNumericFieldFallsBackWithinStructuralCandidate 把「参数删除
// 回落解析默认」放在**同一个结构变更候选**里验：重建与回落必须一起生效——只验数值分
// 支会漏掉「新壳按删除后的配置重建」这一半。
func TestCrossConfig_DeletedNumericFieldFallsBackWithinStructuralCandidate(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(hotYAML(t, 7, 7, false))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, 7, keepRecentOf(residentCacheForTest(entry)["sub1"]), "baseline configured value")

	// 同一候选：sub1 的 keep_recent_tasks **删除** + entry 多一个工具（结构变更）。
	write(hotYAML(t, 0, 7, true))
	entry.CheckOrgReload()

	table := residentCacheForTest(entry)
	require.Equal(t, 2, keepRecentOf(table["sub1"]),
		"a deleted numeric field must fall back to the parsed default inside a structural candidate")
	require.Equal(t, 7, keepRecentOf(table["sub2"]),
		"the sibling whose field is untouched keeps its value (full-desired applies per agent, not blanket)")
}

// TestCrossConfig_RefusedLaterAddRollsBackEarlierAdd 是「候选后半段失败」：同一轮里
// 先成功并入一个新增 agent，随后另一个新增失败 → 已并入的身份必须解绑，且旧代原样
// 服务；下一次合法新增仍要能用（不留半截 owner）。
func TestCrossConfig_RefusedLaterAddRollsBackEarlierAdd(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	// 启动代：entry → sub1。
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()
	genAtStart := entry.OrgDiagnostics()["generation"]

	// 候选：新增 okagent（可构造）+ badagent（构造必失败：system_prompt 指向不存在的文件）。
	// 名字排序决定构建顺序：badagent 先失败时 okagent 尚未并入，测不到回退；因此用
	// a-ok / z-bad 保证「先成功后失败」。
	write(addTwoYAML(t, []string{"sub1"}, "a2okagent", "zzbadagent"))
	entry.CheckOrgReload()

	st := entry.OrgDiagnostics()
	require.Equal(t, genAtStart, st["generation"],
		"a candidate whose later half failed must never publish")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the failure must be diagnosable")
	require.Contains(t, fail.Error, "zzbadagent", "the failure must name the agent that could not be built")

	table := residentCacheForTest(entry)
	require.NotContains(t, table, "a2okagent",
		"the earlier successful add must be UNPUBLISHED when the candidate is refused (reverse-order rollback)")
	require.NotContains(t, entryToolNames(entry), "a2okagent",
		"a rolled-back identity must not be routable")
	require.NotContains(t, table, "zzbadagent", "the failed add is obviously not resident")

	// 旧代原样服务：sub1 仍是唯一的可路由目标。
	require.Equal(t, []string{"sub1"}, entryToolNames(entry), "the retained generation keeps serving unchanged")

	// 回退必须干净：随后单独新增 a2okagent 要能正常发布（若残留半截 owner 会撞车）。
	write(addOneYAML(t, []string{"sub1"}, "a2okagent"))
	entry.CheckOrgReload()
	st = entry.OrgDiagnostics()
	require.Equal(t, int64(1), st["generation"].(int64)-genAtStart.(int64),
		"a later legitimate add must publish cleanly after the rollback")
	require.NotNil(t, residentCacheForTest(entry)["a2okagent"], "the retried add becomes resident")
	require.Contains(t, entryToolNames(entry), "a2okagent", "and is routable")
}

// addTwoYAML renders entry delegating to `base` plus two new agents. The second one
// ("zzbadagent") has an un-creatable localfile store path, so its RESIDENT build fails
// mid-candidate — after the first add has been built and merged.
func addTwoYAML(t testing.TB, base []string, okName, badName string) string {
	t.Helper()
	y := addOneYAML(t, base, okName)
	return strings.Replace(y,
		"description: \""+okName+"\"",
		"description: \""+okName+"\"\n      - kind: agent\n        agent: "+badName+"\n        description: \""+badName+"\"", 1) +
		badAgentDef(badName)
}

// addOneYAML renders the same org with just one added agent (memory path per agent:
// two agents sharing an empty path would collide on one store owner and mask the
// failure this scenario is about).
func addOneYAML(t testing.TB, base []string, name string) string {
	t.Helper()
	var toolLines, defs strings.Builder
	for _, n := range append(append([]string{}, base...), name) {
		toolLines.WriteString("      - kind: agent\n        agent: " + n + "\n        description: \"" + n + "\"\n")
	}
	defs.WriteString(orgHead())
	defs.WriteString(toolLines.String())
	defs.WriteString(subDef(t, base[0]))
	defs.WriteString(agentDef(t, name, "inline: \"ok agent\""))
	return defs.String()
}

func orgHead() string {
	return "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    tools:\n"
}

// subDef renders the startup sub-agent exactly as ownerYAML defines it, so its memory
// section stays byte-identical across candidates (otherwise the memory rule refuses
// the candidate for the wrong reason).
func subDef(t testing.TB, name string) string {
	t.Helper()
	return agentDef(t, name, "inline: \""+name+"\"")
}

func agentDef(t testing.TB, name, prompt string) string {
	t.Helper()
	return "  " + name + ":\n    system_prompt:\n      " + prompt +
		fmt.Sprintf("\n    memory:\n      type: memory\n      path: %q\n", testStore(t, "own-"+name))
}

// badAgentDef is a schema-valid agent whose store cannot be created: config parse and
// Validate pass, so the failure lands in buildAgent ("agent %q: create memory store")
// — late enough that an earlier add in the same candidate is already merged.
//
// 曾试过用“提示词文件缺失”作为构造失败点，但 loader 是 load-if-present（缺失仅
// INFO 跳过），不会失败；一个不存在的存储路径才是确定性的构造期失败。
func badAgentDef(name string) string {
	return "  " + name + ":\n    system_prompt:\n      inline: \"bad\"\n    memory:\n      type: localfile\n      path: \"/dev/null/" + name + "-store\"\n"
}

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

// TestWithinLoopInitiatorResolvesOnBsOwnFace pins ①: while a call on b
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
func TestWithinLoopInitiatorResolvesOnBsOwnFace(t *testing.T) {
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

// TestPostSilenceRelaunchUsesResidentOwnerFace pins ②: with b's loop
// silent and NO initiating call, the re-entry must fall back to b's resident
// owner face (the instance is resident and unclosed) — never to the entry's.
func TestPostSilenceRelaunchUsesResidentOwnerFace(t *testing.T) {
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

// TestGenerationThatRemovedTargetRefusesWithoutRerouting is the third
// state: after a published generation stops routing c, a re-entry of the stored
// task is refused by NAME with the version reason, and neither the removed target
// nor a substitute runs.
func TestGenerationThatRemovedTargetRefusesWithoutRerouting(t *testing.T) {
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

// TestChangedTargetResolvesOnTheNewGeneration is the fourth state, and
// the one that could not pass before §3.2's trunk: a generation that changed c
// must be what a no-initiator re-entry reaches. b's own declaration is unchanged
// here, so this is precisely the「未变父也随发布推进执行视图」leg at depth 2.
func TestChangedTargetResolvesOnTheNewGeneration(t *testing.T) {
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

// 轮九十九（evidence §5.55）：4.2 的最后一条腿——**WAL 重建入口的多级重入**。
//
// 轮九十四已证「生产 Spawn 入口」四态（§5.50），当时如实记：重启入口缺 org 级
// harness。本测补的就是那一句：任务落在 **b 自己的 board** 上且**未终结**时进程
// 结束（崩溃形状的 WAL：只有 task_spawned，没有终态 settle），随后由**独立进程**
// 在同一批持久 store 上重启，再从 b 的重建 board 上重放 `relaunch_task`。
//
// 为什么必须是跨进程：一次 boot 只有真实进程启动才算证据（§8 xproc 纪律），
// 同进程内多轮 New/Close 翻动并不是生产路径。崩溃形状靠**不调用 Close 直接退出**
// 得到——这正是「上次运行留下未终结任务」的物理形态，而不是我手搓一条记录。
//
// 判定的锋利处在负面子进程：重启后把 c 从拓扑里摘掉，重建出的存量任务必须**按名
// 拒绝**而不是被旧代绑定静默复活——wireAgent 的 redispatch 注释点名的就是这件事
// （「若传快照，后续代移除的目标仍会被旧代绑定静默复活」）。

const (
	walReentryPhaseEnv = "TAGENT_WAL42_PHASE"
	walReentryYamlEnv  = "TAGENT_WAL42_YAML"
	walReentryFilter   = "TestRelaunchAfterRestartResolvesOnTheCurrentFace$"
)

// walReentryYAML renders a→b→c with EVERY agent on its own localfile store, so the
// fact chain physically survives the process that wrote it. b carries the
// PRODUCTION task tools (relaunch/resume) and delegates to c asynchronously
// (default), which is what puts the subagent task on b's own board.
func walReentryYAML(root string, dropC bool) string {
	mem := func(name string) string {
		return fmt.Sprintf("    memory:\n      type: localfile\n      path: %q\n", filepath.Join(root, "store-"+name))
	}
	cRef := "      - kind: agent\n        agent: c\n        description: delegate-c\n"
	cDef := "  c:\n    system_prompt:\n      inline: \"SUB-C\"\n" + mem("c")
	if dropC {
		cRef, cDef = "", ""
	}
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n" + mem("a") +
		"    tools:\n      - kind: agent\n        agent: b\n        description: delegate-b\n" +
		"  b:\n    system_prompt:\n      inline: \"SUB-B\"\n" + mem("b") +
		"    tools:\n" + cRef +
		"      - kind: tool\n        id: relaunch_task\n      - kind: tool\n        id: resume_task\n" + cDef
}

// walReentryModel delegates to a named target at every level (never tools[0], §6.20)
// and can park c's FIRST call so the task is still unfinished when the process dies.
type walReentryModel struct {
	mu      sync.Mutex
	served  []delegServed
	perCall map[string]int
	prefer  []string
	gate    chan struct{}
	gated   bool
}

func (m *walReentryModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
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
	park := m.gate
	if label == "SUB-C" && park != nil && !m.gated {
		m.gated = true
	} else {
		park = nil
	}
	m.served = append(m.served, delegServed{System: label, Tools: tools, Answered: offer})
	m.mu.Unlock()

	if park != nil { // hold the producer open: the task must NOT reach a terminal settle
		select {
		case <-park:
		case <-time.After(60 * time.Second):
		}
	}

	ch := make(chan *model.Response, 1)
	if offer != "" {
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-" + offer,
				Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant, Content: "served:" + label}}}}
	}
	close(ch)
	return ch, nil
}

func (m *walReentryModel) Info() model.Info { return model.Info{Name: "walReentry-model"} }

func (m *walReentryModel) snapshot() []delegServed {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]delegServed(nil), m.served...)
}

// firstSubagentTask reads the subagent task off the owner's OWN board.
func firstSubagentTask(b *agent.TagentAgent) *task.Task {
	for _, tk := range b.TaskManager().List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	return nil
}

func walReentryBoot(t *testing.T, yamlPath string, m *walReentryModel) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

// TestRelaunchAfterRestartResolvesOnTheCurrentFace is the parent: it hands
// each boot to its own process and only orchestrates the durable state between them.
func TestRelaunchAfterRestartResolvesOnTheCurrentFace(t *testing.T) {
	if phase := os.Getenv(walReentryPhaseEnv); phase != "" {
		walReentryChild(t, phase)
		return
	}

	// Each scenario gets its OWN durable root: the negative leg must restart over
	// the crash state itself, not over a board the positive leg already settled
	// (a settled task correctly folds to nothing — sharing a root would have tested
	// that instead of the intended refusal).
	runScenario := func(dropOnRestart bool) {
		dir := t.TempDir()
		yamlPath := filepath.Join(dir, "tagent.yaml")
		env := append(os.Environ(), walReentryYamlEnv+"="+yamlPath)

		// BOOT 1 — leave an UNFINISHED subagent task on b's board, then die abruptly
		// (no Close): the crash-shaped fact chain the next boot has to fold.
		runBootChild(t, env, walReentryPhaseEnv+"=spawn", walReentryFilter)

		if dropOnRestart {
			// BOOT 2' — the operator removed c before the restart: the rebuilt task
			// must be refused BY NAME, not resurrected through a snapshot of the old face.
			require.NoError(t, os.WriteFile(yamlPath, []byte(walReentryYAML(dir, true)), 0o644))
			runBootChild(t, env, walReentryPhaseEnv+"=restart_drop_c", walReentryFilter)
			return
		}
		// BOOT 2 — restart over that WAL with c still routed: the rebuilt task must
		// really re-run, resolving through b's rebuilt owner face at depth 2.
		runBootChild(t, env, walReentryPhaseEnv+"=restart", walReentryFilter)
	}
	runScenario(false)
	runScenario(true)
}

func walReentryChild(t *testing.T, phase string) {
	yamlPath := os.Getenv(walReentryYamlEnv)
	require.NotEmpty(t, yamlPath, "the parent must hand the shared config path through the env")
	dir := filepath.Dir(yamlPath)

	switch phase {
	case "spawn":
		require.NoError(t, os.WriteFile(yamlPath, []byte(walReentryYAML(dir, false)), 0o644))
		gate := make(chan struct{})
		m := &walReentryModel{prefer: []string{"b", "c"}, gate: gate}
		entry := walReentryBoot(t, yamlPath, m)
		out, err := entry.StartLoop("u", "walReentry-spawn")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first"))
		require.NoError(t, err)
		waitFor(t, "the nested delegation entered and is still running", func() bool {
			return countServed(m.snapshot(), "SUB-C") >= 1
		})
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b, "b is a resident owner")
		require.NotNil(t, firstSubagentTask(b),
			"precondition: the unfinished subagent task sits on b's OWN board")
		// Give the spawn record time to reach the durable path, then exit WITHOUT
		// Close. The task must be left un-settled — that is the whole state under test.
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "restart":
		m := &walReentryModel{prefer: []string{"b", "c"}}
		entry := walReentryBoot(t, yamlPath, m)
		defer func() { _ = entry.Close() }()
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b)

		tk := firstSubagentTask(b)
		require.NotNilf(t, tk,
			"§4.2 WAL 重建入口：重启后 b 自己的 board 必须从事实链折回那个未终结的子 agent 任务（实得 board=%v）", len(b.TaskManager().List()))
		before := countServed(m.snapshot(), "SUB-C")

		// No initiating call exists after a restart: this is the 无发起者 branch,
		// driven through the PRODUCTION relaunch tool against b's own controller.
		ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
		res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, tk.ID))
		require.NoError(t, err)
		require.NotContains(t, res.(string), "失败",
			"重建出的合法任务在重启入口必须能重放：%v", res)
		waitFor(t, "the relaunched depth-2 delegation really ran on the rebuilt face", func() bool {
			return countServed(m.snapshot(), "SUB-C") > before
		})

	case "restart_drop_c":
		m := &walReentryModel{prefer: []string{"b", "c"}}
		entry := walReentryBoot(t, yamlPath, m)
		defer func() { _ = entry.Close() }()
		b := residentCacheForTest(entry)["b"]
		require.NotNil(t, b)
		tk := firstSubagentTask(b)
		require.NotNil(t, tk, "precondition: the rebuilt board still carries the durable task")
		require.Nil(t, b.ContextManager().SubagentWrapper("c"),
			"precondition: the restarted face no longer routes c")

		before := countServed(m.snapshot(), "SUB-C")
		ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())
		res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, tk.ID))
		if err == nil {
			require.Contains(t, res.(string), "失败",
				"§4.2：重启后目标已被摘路由，存量任务必须**按名拒绝**而不是静默复活旧代绑定：%v", res)
		}
		require.Zero(t, countServed(m.snapshot(), "SUB-C")-before,
			"a refused re-entry must not execute the removed target")

	default:
		t.Fatalf("unknown phase %q", phase)
	}
	os.Exit(0)
}

// 轮一百（evidence §5.56）：3.3「有状态工具」的最后一腿——**org 级端到端锚**，
// 用真实 tmux 会话证明「已纳管任务不因工具换代失监视」。
//
// 为什么走重启而不是热更窗口：`quiet_timeout` 的下限被钉在稳定窗（60s，TUI 90s），
// 也就是说热更之后再靠「静默→suspect」把任务推进裁决区，需要 60s 以上真实静默——
// 那属既有负载敏感 tmux 族（§5.36/§5.40），钉成常驻锚只会变成偶发红灯。而重启入口
// 有一个**确定**的同款裁决：`RestoreTask` 把 running 语义降级为 suspect，随后
// `build_agent.go:761-787` 的 TaskID 桥先裁孤儿、再把「被当前 monitor 跟踪」的任务
// 提升回 running——一个存活会话要被判「未跟踪」，等价于监视信号在某一代工具手里丢了。
// 这条链与热更窗口检验的是**同一处 seam**（换代装配新工具后，管理器读的必须是当前代
// 的 monitor），且它顺带把本子句另一句「声明构造与恢复重挂/monitor 激活分离」钉住：
// 重挂必须先于裁决与提升，否则合法活会话会被自己裁成 reincarnation orphan。
//
// 崩溃形状与轮九十九同法：spawn 子进程起一个真实长命令会话，等记录过 durable 路径后
// **不调 Close 直接退出**——tmux 会话属于 server 不属于本进程，因此它活到下一次 boot。

const (
	sessionWatchPhaseEnv = "TAGENT_MON33_PHASE"
	sessionWatchYamlEnv  = "TAGENT_MON33_YAML"
	sessionWatchFilter   = "TestLiveSessionStaysWatchedAcrossToolGeneration$"
	sessionWatchCommand  = "sleep 150"
	sessionWatchSvcName  = "sessionWatchsvc"
)

// sessionWatchYAML renders an entry that owns the REAL exec tool (the ActionTool), on a
// localfile store so the task record outlives the process. No model/providers
// section: the host-injected mock then serves the agent (resolveAgentModel order 2).
func sessionWatchYAML(root string) string { return sessionWatchYAMLWithExtra(root, "") }

// sessionWatchYAMLAfterPublish adds a routed sub-agent: a declaration the current face
// does not have, so CheckOrgReload really publishes a NEW generation (and with it
// a freshly assembled ActionTool for the owner).
func sessionWatchYAMLAfterPublish(root string) string {
	return sessionWatchYAMLWithExtra(root, "      - kind: agent\n        agent: helper\n        description: delegate-helper\n"+"  helper:\n    system_prompt:\n      inline: \"SUB-HELPER\"\n    memory:\n      type: memory\n")
}

func sessionWatchYAMLWithExtra(root, extra string) string {
	return "entry: a\nagents:\n  a:\n    system_prompt:\n      inline: \"ENTRY-A\"\n    max_tool_iterations: 2\n    memory:\n      type: localfile\n      path: " +
		fmt.Sprintf("%q", filepath.Join(root, "store-a")) +
		"\n    tools:\n      - kind: tool\n        id: exec\n" + extra
}

// sessionWatchModel asks for the action tool once, with a command that keeps its tmux
// session alive well past the restart, then closes the turn on the ack. The
// registry id is "exec" while the tool's own DECLARATION name is "action"
// (action_tool.go:287) — the model only ever sees the declaration name, so
// targeting the id from YAML would silently offer nothing.
type sessionWatchModel struct {
	mu    sync.Mutex
	calls int
	// offered records which call numbers actually issued a tool call, so the
	// post-publish assertion cannot be satisfied by a turn that never delegated.
	offered  []int
	requests []*model.Request
}

// count reads the call total under the same mutex the model writes under — the
// polling predicates below must never touch the field directly (a plain read
// against the model's locked write is a data race, caught by -race here).
func (m *sessionWatchModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *sessionWatchModel) offeredCalls() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.offered...)
}

func (m *sessionWatchModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func (m *sessionWatchModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := delegLabel(delegSystemOf(req))
	tools := delegToolNames(req)

	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.calls++
	n := m.calls
	offer := ""
	// calls 1 and 3 each attempt a spawn under the SAME logical name
	if n == 1 || n == 3 {
		for _, t := range tools {
			if t == "action" {
				offer = t
			}
		}
	}
	if offer != "" {
		m.offered = append(m.offered, n)
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if offer != "" {
		// `mode: resident` is what makes the session survive the restart at all:
		// startup reaps prefix-matched LEFTOVER sessions (they would hold ptys
		// forever), and only sessions recorded in the persisted resident metadata
		// are re-attached afterwards (action_tool.go:201-219, D1). Using the
		// default oneshot mode would have the second boot correctly kill it.
		// `name` is what makes it addressable ACROSS the restart: a named session
		// becomes `n-<logical>` and is spared by the startup orphan reap (only
		// generated `prefix-<ts>` names are reaped), and the tool's own schema
		// recommends exactly this pairing for mode=resident services.
		args, err := json.Marshal(map[string]any{
			"command": sessionWatchCommand, "ttl": 600, "mode": "resident", "name": sessionWatchSvcName,
		})
		if err != nil {
			return nil, err
		}
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{Type: "function", ID: "call-action", Function: model.FunctionDefinitionParam{
				Name: offer, Arguments: args}}},
		}}}}
	} else {
		ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("served:" + label)}}}
	}
	close(ch)
	return ch, nil
}

func (m *sessionWatchModel) Info() model.Info { return model.Info{Name: "sessionWatch-model"} }

// commandTask finds the exec task and its backing tmux session name.
func commandTask(ta *agent.TagentAgent) (*task.Task, string) {
	for _, tk := range ta.TaskManager().List() {
		if tk.Spec.Kind == "command" && tk.Spec.Declarative != nil && tk.Spec.Declarative.TaskID != "" {
			return tk, tk.Spec.Declarative.TaskID
		}
	}
	return nil, ""
}

// tmuxSessionAlive asks the tmux server itself — the ground truth the monitor's
// answer has to agree with, so a promoted task cannot be an accident of a dead session.
func tmuxSessionAlive(session string) bool {
	if session == "" {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
}

// killOwnSession releases ONLY a session this test created (name guard), leaving
// anything else on the operator's tmux server untouched.
func killOwnSession(t *testing.T, session string) {
	t.Helper()
	if session != "n-"+sessionWatchSvcName {
		t.Logf("refusing to kill a session this test did not create: %q", session)
		return
	}
	if out, err := exec.Command("tmux", "kill-session", "-t", session).CombinedOutput(); err != nil {
		t.Logf("kill-session %s: %v %s", session, err, out)
	}
}

func TestLiveSessionStaysWatchedAcrossToolGeneration(t *testing.T) {
	if phase := os.Getenv(sessionWatchPhaseEnv); phase != "" {
		sessionWatchChild(t, phase)
		return
	}
	root := t.TempDir()
	yamlPath := filepath.Join(root, "tagent.yaml")
	env := append(os.Environ(), sessionWatchYamlEnv+"="+yamlPath)

	// BOOT 1 — start a real session and die WITHOUT Close: the task stays un-settled
	// in the durable chain while its tmux session lives on in the server.
	// the session name this scenario owns is fixed, so the phase log is traceable
	runBootChild(t, env, sessionWatchPhaseEnv+"=spawn", sessionWatchFilter)

	// BOOT 2 — a fresh process with a freshly assembled ActionTool must still watch
	// that live session: adjudication first, then promotion of the tracked task.
	runBootChild(t, env, sessionWatchPhaseEnv+"=restart", sessionWatchFilter)

	// BOOT 3 — the HOT-RELOAD window, in its OWN durable root: everything happens
	// inside one process (spawn → publish → re-attempt), and reusing the root above
	// would hand it a leftover task whose session is long dead (measured).
	hotRoot := t.TempDir()
	hotYaml := filepath.Join(hotRoot, "tagent.yaml")
	runBootChild(t, append(os.Environ(), sessionWatchYamlEnv+"="+hotYaml), sessionWatchPhaseEnv+"=hotreload", sessionWatchFilter)
}

func sessionWatchBoot(t *testing.T, yamlPath string) (*agent.TagentAgent, *sessionWatchModel) {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &sessionWatchModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry, m
}

func sessionWatchChild(t *testing.T, phase string) {
	yamlPath := os.Getenv(sessionWatchYamlEnv)
	require.NotEmpty(t, yamlPath)
	root := filepath.Dir(yamlPath)

	switch phase {
	case "spawn":
		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAML(root)), 0o644))
		// Idempotent preflight: a named session survives a CRASHED earlier run of
		// this test the same way it survives a restart, and the tool refuses a
		// duplicate name — without this the anchor would stop being re-runnable
		// after any failed run (observed while probing). Only ever our own name.
		killOwnSession(t, "n-"+sessionWatchSvcName)
		entry, _ := sessionWatchBoot(t, yamlPath)
		out, err := entry.StartLoop("u", "sessionWatch-spawn")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start a long job"))
		require.NoError(t, err)

		var session string
		waitFor(t, "the exec task exists with a backing session", func() bool {
			_, session = commandTask(entry)
			return session != ""
		})
		require.Truef(t, tmuxSessionAlive(session),
			"precondition: the tmux server must really be running session %s", session)
		// Let the spawn record reach the durable path, then leave WITHOUT Close so
		// nothing reaps the session and the task never settles.
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "hotreload":
		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAML(root)), 0o644))
		killOwnSession(t, "n-"+sessionWatchSvcName) // idempotent preflight, own name only
		entry, m := sessionWatchBoot(t, yamlPath)
		tick := time.Now().Add(2 * time.Second)
		out, err := entry.StartLoop("u", "sessionWatch-hot")
		require.NoError(t, err)
		go func() {
			for range out {
			}
		}()
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start the resident service"))
		require.NoError(t, err)

		var session string
		waitFor(t, "the resident session is up under the first generation", func() bool {
			_, session = commandTask(entry)
			return session != ""
		})
		require.Truef(t, tmuxSessionAlive(session), "precondition: %s must be live", session)

		// Publish a NEW generation: the owner's ActionTool is re-assembled.
		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAMLAfterPublish(root)), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
		before := m.count()
		entry.CheckOrgReload()
		require.Greater(t, diagInt64(t, entry.OrgDiagnostics(), "generation"), int64(0),
			"precondition: the publish really advanced the generation")

		// The CURRENT generation must still own the live session: asking for the
		// same logical name has to be refused as a duplicate, not answered by a
		// second session nobody was tracking.
		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start it again"))
		require.NoError(t, err)
		waitFor(t, "the second attempt was answered", func() bool { return m.count() > before+1 })

		// Non-vacuity: the post-publish attempt must really have REACHED the tool
		// (two spawns attempted), or the count check below would be satisfiable by
		// a turn that never delegated at all.
		require.Contains(t, m.offeredCalls(), 3,
			"precondition: the current generation must really have been asked to spawn again")

		tasks := 0
		for _, tk := range entry.TaskManager().List() {
			if tk.Spec.Kind == "command" {
				tasks++
			}
		}
		require.Equalf(t, 1, tasks,
			"§3.3「已纳管任务不因工具换代失监视」：换代后的当前代仍须认得那个存活会话（同名第二次派生被当成新会话＝monitor 空、监视已失），实得 command 任务数=%d", tasks)
		require.Truef(t, tmuxSessionAlive(session), "the original session %s must be the one still alive", session)

		killOwnSession(t, session)
		require.NoError(t, entry.Close())
		os.Exit(0)

	case "restart":
		entry, _ := sessionWatchBoot(t, yamlPath)
		tk, session := commandTask(entry)
		require.NotNilf(t, tk,
			"§3.3 端到端：重启必须从事实链折回那个 exec 任务（board=%v）", len(entry.TaskManager().List()))
		require.Truef(t, tmuxSessionAlive(session),
			"precondition: session %s must still be alive for the monitoring claim to mean anything", session)

		// The generation that just booted assembled a NEW ActionTool and reattached
		// the live session BEFORE adjudication; the TaskID bridge therefore promotes
		// the restored suspect back to running. A monitoring signal left on the
		// superseded tool would instead report "untracked" and retire it as a
		// reincarnation orphan (or at best leave it suspect forever).
		//
		// Bounded wait (2026-09-27 audit): under a full `go test ./...` run the
		// machine hosts every package binary at once and the boot's first tmux
		// verification can miss the window; List() re-runs reconcileDetached, so
		// re-fetching exercises the designed re-adjudication path rather than mere
		// patience. Alone / whole-package the promotion lands before the first
		// fetch; a session that never promotes still fails with the same message.
		deadline := time.Now().Add(30 * time.Second)
		for tk.Status() != task.TaskRunning && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			tk, _ = commandTask(entry)
		}
		require.Equalf(t, task.TaskRunning, tk.Status(),
			"§3.3「已纳管任务不因工具换代失监视」：存活会话 %s 在换代后的新代里必须仍被监视并提升回 running（实得 status=%s）",
			session, string(tk.Status()))

		killOwnSession(t, session)
		require.NoError(t, entry.Close())
		os.Exit(0)

	default:
		t.Fatalf("unknown phase %q", phase)
	}
}

// armGate parks every FUTURE call of the agent whose prompt starts with `label`.
// Safe for concurrent use because GenerateContent reads the map under m.mu.
func (m *delegModel) armGate(label string, ch chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gates == nil {
		m.gates = map[string]chan struct{}{}
	}
	m.gates[label] = ch
}

// disarmGate frees a parked call and is idempotent, so a bail-out can never hang the
// package on a gate nobody will close.
func disarmGate(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// chainYAML is A→B→C with C's declaration text parameterized: changing C's prompt is a
// structural change, so applying it republishes C into a new generation.
func chainYAML(cPrompt string) string {
	return strings.Replace(nestedYAML(), `inline: "SUB-C-PROMPT"`, fmt.Sprintf("inline: %q", cPrompt), 1)
}

// allLevelsChangedYAML is the same chain with a new declaration at EVERY level.
func allLevelsChangedYAML() string {
	return strings.NewReplacer(
		`inline: "ENTRY-A-PROMPT"`, `inline: "ENTRY-A-PROMPT-G2"`,
		`inline: "SUB-B-PROMPT"`, `inline: "SUB-B-PROMPT-G2"`,
		`inline: "SUB-C-PROMPT"`, `inline: "SUB-C-PROMPT-G2"`,
	).Replace(nestedYAML())
}

// TestOrgDelegation_AllLevelsRepublishedReachTheNewLeaf pins defect D-b of evidence
// §5.44: when EVERY level changes, the transitional-carrier loop walked the changed set
// in MAP order, so a parent's shell could be assembled before its child's — and the DFS
// cache-hit the stale resident child, baking the old target into the new face. The outcome
// therefore flipped run to run (measured: the new leaf was reached in 1 of 6 identical
// publishes). Deterministic delivery is what a structural publish owes the caller: once a
// generation is in force, the next turn must run that generation's declarations at EVERY
// depth, not only where the build order happened to cooperate.
//
// Unlike the nested-hop contract above (which also needs D-a, the trunk), this case is
// reachable today: the entry's own face is rebuilt on every publish.
func TestOrgDelegation_AllLevelsRepublishedReachTheNewLeaf(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(chainYAML("SUB-C-PROMPT"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "all-levels-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("warm"))
	require.NoError(t, err)
	waitFor(t, "the chain ran on G1", func() bool { return countServed(m.snapshot(), "SUB-C-PROMPT") >= 1 })

	// Every level changes: entry, the middle agent, and the leaf.
	write(allLevelsChangedYAML())
	entry.CheckOrgReload()

	before := countServed(m.snapshot(), "SUB-C-PROMPT-G2")
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("after"))
	require.NoError(t, err)
	waitFor(t, "the next turn reaches the new leaf at depth 3", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT-G2") > before
	})
}

// TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget is 3.2's acceptance row
// 「A→B→C 各见本代自身声明」for the facet no existing anchor reaches: the NESTED hop
// across a publication.
//
// Coverage so far: NestedLevelsServeFromTheirOwnBindings proves each LEVEL sees its own
// declarations in steady state; InFlightDelegationKeepsItsOwnGenerationTarget proves a
// pinned initiator keeps its generation's target at ONE hop. Neither exercises B — pinned
// to G1 while parked — delegating DOWN to C after G2 replaced C.
//
// The pinned-face rule (resident-continuity「重试也不改路由」, subagent-turn-execution
// 「仍持 G1 租约的发起者不因 G2 删除目标而丢失其合法 G1 绑定」, D5) says that hop must run
// on the C that B's G1 face declared. The third turn must then run the new C — which is
// what makes this anchor self-discriminating: if the publish never landed, the first
// assertion would pass for the wrong reason.
func TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget(t *testing.T) {
	// Round 90: UN-SKIPPED as the trunk DoD (user-approved holding expansion). Red was
	// re-measured before implementation; it must be GREEN when the trunk lands.
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(chainYAML("SUB-C-PROMPT"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "nested-hop-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	// Turn 1: the full chain runs on G1, C included.
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "the leaf served on G1", func() bool { return countServed(m.snapshot(), "SUB-C-PROMPT") >= 1 })

	// Turn 2: park B mid-call, so its execution is pinned to G1 while G2 lands.
	bGate := make(chan struct{})
	m.armGate("SUB-B-PROMPT", bGate)
	t.Cleanup(func() { disarmGate(bGate) })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second request"))
	require.NoError(t, err)
	waitFor(t, "B parked mid-call", func() bool {
		for _, s := range m.snapshot() {
			if strings.HasPrefix(s.System, "SUB-B-PROMPT") && len(s.Tools) > 0 {
				return true
			}
		}
		return false
	})

	write(chainYAML("SUB-C-PROMPT-G2"))
	entry.CheckOrgReload() // publish G2 while B is parked

	entriesBefore := countServed(m.snapshot(), "SUB-C-PROMPT")
	g2Before := countServed(m.snapshot(), "SUB-C-PROMPT-G2")
	disarmGate(bGate)

	//〔轮九十断言迁移（显式，非静默改测）〕The previous formulation — "no
	// SUB-C-PROMPT-G2 serve before the injected third turn" — conflated "the
	// pinned hop is not re-routed" with "no fresh turn runs at all". The second
	// half held only under the D-a defect, where NO fresh turn could reach the
	// new C; with the trunk landed, the settle of this very delegation legitimately
	// drives a fresh turn that takes the NEW generation (correct per D6: a
	// task_settled re-entry uses the current effective face). So the assertion now
	// pins the hop's own answer: wait for a NEW B post-tool record (turn 1 already
	// produced one) and require ITS result to be the OLD C's answer, quoted-exact
	// so the -G2 variant cannot match as a prefix.
	bHopAnswers := func() []string {
		var out []string
		for _, s := range m.snapshot() {
			if s.System == "SUB-B-PROMPT" && len(s.ToolResults) > 0 {
				out = append(out, strings.Join(s.ToolResults, "\n"))
			}
		}
		return out
	}
	hopsBefore := len(bHopAnswers())
	waitFor(t, "the pinned G1 hop returned its answer to B", func() bool {
		return len(bHopAnswers()) > hopsBefore
	})
	answers := bHopAnswers()
	// The (hopsBefore+1)-th B-with-results record IS the pinned hop's: the only
	// later source of another one is the settle of this very delegation, which
	// cannot fire before the hop's producer returned — so indexing (not "latest")
	// keeps the witness on the hop even when both land between two polls.
	pinned := answers[hopsBefore]
	require.Contains(t, pinned, `"served:SUB-C-PROMPT"`,
		"§3.2：被钉跳的回执必须是 G1 之 C 的回答（D5「派生前继承发起调用租约」）")
	require.NotContains(t, pinned, `"served:SUB-C-PROMPT-G2"`,
		"§3.2：B 的 G1 代执行不得因为 G2 换了 C 就被改道到新目标——被钉跳的回执不能来自新代目标")
	require.Greater(t, countServed(m.snapshot(), "SUB-C-PROMPT"), entriesBefore,
		"the pinned hop really executed on the old C (its answer came from somewhere)")

	// …and the NEW generation must really be reachable — otherwise the assertion
	// above would only prove the publish never took effect.
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("third request"))
	require.NoError(t, err)
	waitFor(t, "a fresh turn serves the new C", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT-G2") > g2Before
	})
}

// §4.2 at the PRODUCTION task-action entry: the same `relaunch_task` /
// `resume_task` objects the LLM calls, driven against a generation published by
// the real reload path. Two things only this level can prove: the refusal reaches
// the host-visible tool answer (not just an internal error), and a wrapper built by
// a CANDIDATE carries a usable resident owner — without that wire every re-entry
// after the first hot update would fail closed.

// reentryYAML renders entry "a" delegating to `target` with relaunch_task and
// resume_task on the same face. `maxIters` is a fingerprint field, so changing it
// is what makes an edit publish a NEW generation while the routing shape holds.
func reentryYAML(target string, maxIters int) string {
	return fmt.Sprintf(`entry: a
agents:
  a:
    system_prompt:
      inline: "ENTRY-A-PROMPT"
    max_tool_iterations: %d
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-%s
      - kind: tool
        id: relaunch_task
      - kind: tool
        id: resume_task
  b:
    system_prompt:
      inline: "SUB-B-PROMPT"
    memory:
      type: memory
  c:
    system_prompt:
      inline: "SUB-C-PROMPT"
    memory:
      type: memory
`, maxIters, target, target)
}

func writeReentryYAML(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
}

// namedDelegModel is delegModel with the choice made BY NAME: the framework does
// not promise any ordering of the tools it hands the model, so a mock that calls
// "tools[0]" would be asserting an accident of slice order rather than the
// orchestration. This mock plays the honest LLM role: it delegates to the tool it
// was told to prefer, when that tool is really on the face it was offered.
type namedDelegModel struct {
	delegModel
	pick string // the delegation tool this caller asks for
}

func (m *namedDelegModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
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
		for _, t := range tools {
			if t == m.pick {
				offer = t
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

// subagentTask finds the task the delegation left behind (kind/key are the
// production spawn's own identity, not a test fixture).
func subagentTask(t *testing.T, tm *task.TaskManager) *task.Task {
	t.Helper()
	for _, tk := range tm.List() {
		if tk.Spec.Kind == "subagent" {
			return tk
		}
	}
	t.Fatalf("no subagent task on the board: %+v", tm.List())
	return nil
}

func relaunchArgs(t *testing.T, id string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"task_id": id})
	require.NoError(t, err)
	return b
}

// TestOrgReentry_RelaunchActionRefusesTargetRemovedByPublishedGeneration is 4.2's
// 「G2 删除 B 后的存量任务」at the entry: the delegation ran under G1, the operator
// then published a generation that routes c instead, and the stored task's relaunch
// — asked for through the production tool — must be REFUSED with the version reason,
// must create no new execution, and must not run the removed target again.
func TestOrgReentry_RelaunchActionRefusesTargetRemovedByPublishedGeneration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	writeReentryYAML(t, yamlPath, reentryYAML("b", 2))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &namedDelegModel{pick: "b"}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "reentry-session")
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
	waitFor(t, "b served the delegation", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") > 0 })
	waitFor(t, "the entry turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })

	// The declaration the model saw is the published face's own: the delegation and
	// both production task tools, all three offered on the real request.
	decls := entryDeclarations(m.snapshot())
	require.NotEmpty(t, decls)
	require.ElementsMatch(t, []string{"b", "relaunch_task", "resume_task"}, decls[0],
		"the entry is offered its delegation AND the production task-action tools")

	tk := subagentTask(t, entry.TaskManager())
	require.Equal(t, "b:work", tk.Spec.Key, "the board entry is the production spawn identity")

	// 换代：a 改路由 c，B 从有效面上消失（b 的常驻 owner 仍在，正是 R03 的复活风险面）。
	writeReentryYAML(t, yamlPath, reentryYAML("c", 2))
	entry.CheckOrgReload()
	require.Nil(t, entry.ContextManager().SubagentWrapper("b"),
		"precondition: the published generation really stopped routing b")

	runsBefore := countServed(m.snapshot(), "SUB-B-PROMPT")
	ctxWithTM := task.WithTaskSpawner(context.Background(), entry.TaskManager())
	res, err := tasktool.NewRelaunchTaskTool().Call(ctxWithTM, relaunchArgs(t, tk.ID))
	require.NoError(t, err, "the tool reports a refusal as an answer, not a transport error")
	text, ok := res.(string)
	require.True(t, ok)
	require.Contains(t, text, "重跑任务", "the host sees WHICH action failed: %s", text)
	require.Contains(t, text, "EFFECTIVE orchestration generation",
		"and WHY: version selection refused it, not a missing file: %s", text)

	require.Equal(t, runsBefore, countServed(m.snapshot(), "SUB-B-PROMPT"),
		"a refused relaunch must not run the removed target — not once")
	require.Equal(t, 0, countServed(m.snapshot(), "SUB-C-PROMPT"),
		"and it must not silently substitute the new target either")
}

// TestOrgReentry_RelaunchActionAfterPublishRunsCurrentGenerationTarget is the
// other side of the same entry: the target is STILL routed after a structural
// publish, so a re-entry that finds no initiating call must run it through the
// NEWLY PUBLISHED face. A wrapper built by a candidate without a resident-owner
// wire would fail closed here ("no resident owner") — which is exactly the
// wiring this test exists to falsify.
func TestOrgReentry_RelaunchActionAfterPublishRunsCurrentGenerationTarget(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	writeReentryYAML(t, yamlPath, reentryYAML("b", 2))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &namedDelegModel{pick: "b"}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "reentry-live-session")
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
	waitFor(t, "the first delegation settled", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") > 0 })
	waitFor(t, "the entry turn closed", func() bool { return countServed(m.snapshot(), "ENTRY-A-PROMPT") >= 2 })
	tk := subagentTask(t, entry.TaskManager())

	before := countServed(m.snapshot(), "SUB-B-PROMPT")
	// A structural edit that KEEPS a→b: max_tool_iterations is fingerprinted, so
	// this really publishes a new generation and rebuilds the wrapper.
	writeReentryYAML(t, yamlPath, reentryYAML("b", 3))
	entry.CheckOrgReload()
	require.NotNil(t, entry.ContextManager().SubagentWrapper("b"), "the new generation still routes b")

	ctxWithTM := task.WithTaskSpawner(context.Background(), entry.TaskManager())
	res, err := tasktool.NewRelaunchTaskTool().Call(ctxWithTM, relaunchArgs(t, tk.ID))
	require.NoError(t, err)
	text, ok := res.(string)
	require.True(t, ok)
	require.NotContains(t, text, "失败", "a still-routed target must relaunch through the published face: %s", text)

	waitFor(t, "the re-entered delegation ran on the current generation", func() bool {
		return countServed(m.snapshot(), "SUB-B-PROMPT") > before
	})
}
