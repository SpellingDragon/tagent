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

	crossWrite(t, yamlPath, delegYAML("c"), &tick)
	entry.CheckOrgReload()
	require.Nil(t, entry.ContextManager().SubagentWrapper("b"), "precondition: b is unrouted in the new generation")

	mid := m.snapshot()
	require.Zero(t, countServed(mid, "SUB-C-PROMPT"), "a mid-call publication must not steal the in-flight background run")

	close(bGate)
	waitFor(t, "the background run delivered B's answer back inline", func() bool {
		return firstResultIndex(m.snapshot(), "served:SUB-B-PROMPT") >= 0
	})
	after := m.snapshot()
	require.Equal(t, 1, countServed(after, "SUB-B-PROMPT"), "the background execution is served exactly once, by G1's target")
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

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "turn 1 parked in B", func() bool { return countServed(m.snapshot(), "SUB-B-PROMPT") == 1 })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second request"))
	require.NoError(t, err)

	crossWrite(t, yamlPath, delegYAML("c"), &tick)
	entry.CheckOrgReload()

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
	waitFor(t, "the entry turn ran", func() bool { return m.count("ENTRY-A-PROMPT") >= 1 })
	require.Contains(t, m.offeredTools("ENTRY-A-PROMPT"), "b", "precondition: b is genuinely offered on the face")

	require.Zero(t, m.count("SUB-B-PROMPT"), "an offered-but-not-called target must not run")
	require.Zero(t, m.count("SUB-C-PROMPT"), "a target not even offered must not run")
}

// ackSettleModel is the witness that the hardest cross-generation row needs and
// the shared mock cannot provide: a detached run's result comes back as a
// USER-role `[task settled] … 结果: …` notification (measured — not as a tool
// result, so `delegServed.ToolResults` is blind to it). This model records, per
// ENTRY call, which tools that call was offered and whether its request carried
// the settle notification, so the row can be attributed by causality: the turn
// that holds the background answer is the settle-driven turn, and the tool set
// offered to IT is the generation that turn runs on.
type ackSettleModel struct {
	mu      sync.Mutex
	calls   []ackEntryCall
	subRuns int
	gate    chan struct{}
}

type ackEntryCall struct {
	tools   []string
	settled bool
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

	if label == "SUB-B-PROMPT" && g != nil {
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

	waitFor(t, "the parent turn closed on an ack", func() bool {
		calls, _ := m.snapshot()
		return len(calls) >= 2
	})
	prePublish, _ := m.snapshot()
	for _, c := range prePublish {
		require.False(t, c.settled,
			"precondition: nothing may have settled yet — otherwise this is the inline shape the earlier anchors already pin")
	}

	crossWrite(t, yamlPath, strings.ReplaceAll(delegYAMLSeq("c"), "        async: false\n", ""), &tick)
	entry.CheckOrgReload()
	require.Nil(t, entry.ContextManager().SubagentWrapper("b"),
		"precondition: the published generation really stopped routing b")

	close(bGate)

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

// remoteSpellingYAML is a remote-only reference — legal with no local definition
// at all — written with or without the explicit kind.
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

	writeAliasConfig(t, yamlPath, aliasSpellingYAML("explicit", 2, true, 4, 6000), &tick)
	entry.CheckOrgReload()
	sub2 := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, sub2, "precondition: sub2 became a resident owner through the hot path")
	require.Equal(t, 3000, sub2.OrgBudgetLine(), "6000×0.5 from the record it was built with")

	recAtPublish := diagnosticsReceipts(t, entry.OrgDiagnostics())
	require.Equalf(t, "applied", recAtPublish["sub2"].Outcome,
		"§5.2/§5.59：结构发布当轮就必须描述新装上的消费源（实得 %+v）", recAtPublish["sub2"])
	require.Equal(t, 6000, recAtPublish["sub2"].MaxTokens)

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

	held.Release()
	require.Equal(t, 4000, sub2.OrgBudgetLine())
}

// TestCrossConfig_RemovingTheLastToolClearsTheDeclaration 钉住 删除最后一个工具必须清空委派声明，被清的绑定不得存活到下一代的模型请求里。
// - 拓扑 delta 恰好发布一代；执行面只来自被发布的那一代，不回落进任何别的已发布快照（零值合并泄漏正是这一形状）；
// - 被移除属主报 draining、参数不被静默重下发，却保留其常驻 owner 供旧代收尾。
// 契约: docs/wiki/agent/execution-generations.md#published-wrapper-immutable
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
	keepDraining := residentCacheForTest(entry)["sub1"].ContextManager().AcquireLease(agent.LeaseSubCall)
	defer keepDraining.Release()
	genBefore := entry.OrgDiagnostics()["generation"]

	write(ownerYAML(t, nil, sub2MemDefault(t)))
	entry.CheckOrgReload()

	require.Empty(t, entryToolNames(entry),
		"removing the LAST tool must clear the declaration — a stale binding is a routing leak")
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64)-genBefore.(int64),
		"the topology delta still publishes exactly one new generation")

	rec := entry.OrgDiagnostics()["agents"].([]OrgAgentApply)
	outcomes := map[string]string{}
	for _, r := range rec {
		outcomes[r.Name] = r.Outcome
	}
	require.Equal(t, "applied", outcomes["main"])
	require.Equal(t, "draining", outcomes["sub1"], "the removed owner is reported, not silently re-parameterized")

	require.NotNil(t, residentCacheForTest(entry)["sub1"], "the removed owner stays resident")
}

// TestCrossConfig_DeletedNumericFieldFallsBackWithinStructuralCandidate 钉住 删除的数值字段在同一结构变更候选里回落解析默认。
// - 重建与回落必须在一次候选里一起生效——只验数值分支会漏掉"新壳按删除后的配置重建"这一半；
// - 未动的同属主保持其值：全期望应用逐 agent 生效，不是一刀切覆盖。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
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

	write(hotYAML(t, 0, 7, true))
	entry.CheckOrgReload()

	table := residentCacheForTest(entry)
	require.Equal(t, 2, keepRecentOf(table["sub1"]),
		"a deleted numeric field must fall back to the parsed default inside a structural candidate")
	require.Equal(t, 7, keepRecentOf(table["sub2"]),
		"the sibling whose field is untouched keeps its value (full-desired applies per agent, not blanket)")
}

// TestCrossConfig_RefusedLaterAddRollsBackEarlierAdd 钉住 候选后半段失败时，先前已并入的新增身份必须整体回退、旧代照常服务。
// - 同一轮里一个新增已并入、另一个新增失败时，前者身份解绑、不留半截 owner；
// - 下一次合法新增仍须能用。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
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

	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()
	genAtStart := entry.OrgDiagnostics()["generation"]

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

	require.Equal(t, []string{"sub1"}, entryToolNames(entry), "the retained generation keeps serving unchanged")

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

// chainDelegModel delegates to whichever offered tool names one of the agents in
// `prefer` (priority order). The framework does not promise tool ordering, so a
// mock that grabs tools[0] would assert an accident of slice order rather than the
// orchestration; naming the target makes each level's delegation deterministic at
// every depth.
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

// TestWithinLoopInitiatorResolvesOnBsOwnFace 钉住 有在途发起调用时，它发起的重入按发起者声明的那一代解析。
// - 途中发布停止路由 c 才使本测非空洞：无换代时两条解析分支同答，证不了读的是哪张面；
// - 仍持 G1 租约的发起者不因 G2 删除目标而丢失其合法 G1 绑定，合法绑定能存活正因声明保持让被钉代的子仍可达；
// - 退回 effective 面或入口工具表都会拒绝，规格要求 G1 绑定存活并在 b 的被钉面上服务重入。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
func TestWithinLoopInitiatorResolvesOnBsOwnFace(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", false), time.Now())

	entry, b, taskID, m := startReentryChain(t, yamlPath)
	defer func() { _ = entry.Close() }()

	before := countServed(m.snapshot(), "SUB-C")
	lease := b.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer lease.Release()

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

// TestPostSilenceRelaunchUsesResidentOwnerFace 钉住 属主环路静默且无发起调用时，重入从该属主的常驻 owner 面解析。
// - 实例常驻且未关闭，解析面是 b 自己的 owner 面，绝不从入口的面。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
func TestPostSilenceRelaunchUsesResidentOwnerFace(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	writeReentryChain(t, yamlPath, reentryChainYAML("SUB-C", false), time.Now())

	entry, b, taskID, m := startReentryChain(t, yamlPath)
	defer func() { _ = entry.Close() }()

	before := countServed(m.snapshot(), "SUB-C")
	ctx := task.WithTaskSpawner(context.Background(), b.TaskManager())

	res, err := tasktool.NewRelaunchTaskTool().Call(ctx, relaunchArgs(t, taskID))
	require.NoError(t, err)
	require.NotContains(t, res.(string), "失败", "②b 环静默后重入必须从其常驻 owner 面解析：%v", res)
	waitFor(t, "the re-entry served through b's owner face", func() bool {
		return countServed(m.snapshot(), "SUB-C") > before
	})
}

// TestGenerationThatRemovedTargetRefusesWithoutRerouting 钉住 新一代停止路由目标后，存量任务的重入被具名拒绝并附版本理由。
// - 既不重跑被移除目标，也不静默改道替身；
// - 拒绝作为答案返回（宿主看见哪个动作失败、理由是版本选择而非缺记录），不是传输错误。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
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

// TestChangedTargetResolvesOnTheNewGeneration 钉住 改了目标的新一代是无发起者重入所达的那张面。
// - b 自身声明未变，故这正是"未变父也随发布推进执行视图"在深度二上的落地；
// - 仍被路由的目标须经新面重入，到达的是新 c。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
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

// walReentryModel delegates to a named target at every level (never tools[0], whose order
// the framework does not promise) and can park c's FIRST call so the task stays un-settled.
type walReentryModel struct {
	mu      sync.Mutex
	served  []delegServed
	perCall map[string]int
	prefer  []string
	gate    chan struct{}
	gated   bool
}

// GenerateContent is the WAL-reentry mock. It holds the producer open while a gate
// is armed: the delegated task must NOT reach a terminal settle during that window, so
// the board the next boot folds really still carries it.
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

	if park != nil {
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

// TestRelaunchAfterRestartResolvesOnTheCurrentFace 钉住 跨进程重启后，重入在当前面解析。
// - 每段 boot 交给它自己的进程、只在其间编排持久状态——一次 boot 只有真实进程启动才算证据；
// - 正负两腿各用独立持久根：负腿从崩溃状态本身重启，而非已被正腿结算过的 board。
// - 崩溃形状＝任务落在 b 自己的 board 上、只有 task_spawned 而无终态 settle 时进程结束（靠不调 Close 直接退出得到，而非手搓一条记录）；
// - 重启后从 b 的重建 board 重放 relaunch_task：摘掉目标 c 的负腿必须按名拒绝，不得被旧代绑定静默复活。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
func TestRelaunchAfterRestartResolvesOnTheCurrentFace(t *testing.T) {
	if phase := os.Getenv(walReentryPhaseEnv); phase != "" {
		walReentryChild(t, phase)
		return
	}

	runScenario := func(dropOnRestart bool) {
		dir := t.TempDir()
		yamlPath := filepath.Join(dir, "tagent.yaml")
		env := append(os.Environ(), walReentryYamlEnv+"="+yamlPath)

		runBootChild(t, env, walReentryPhaseEnv+"=spawn", walReentryFilter)

		if dropOnRestart {
			require.NoError(t, os.WriteFile(yamlPath, []byte(walReentryYAML(dir, true)), 0o644))
			runBootChild(t, env, walReentryPhaseEnv+"=restart_drop_c", walReentryFilter)
			return
		}
		runBootChild(t, env, walReentryPhaseEnv+"=restart", walReentryFilter)
	}
	runScenario(false)
	runScenario(true)
}

// walReentryChild runs one boot of the WAL-reentry scenario as its own process.
//
// The spawn phase gives the record time to reach the durable path, then exits WITHOUT
// Close. The task must be left un-settled — that is the whole state under test, and it
// is the physical shape of a crash rather than a hand-written record.
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

// TestLiveSessionStaysWatchedAcrossToolGeneration 钉住 工具换代后仍存活的真实会话必须继续被当前代跟踪。
// - 三条 boot 各用独立持久根：热更窗口那条全程发生在单进程内（spawn → publish → re-attempt），复用前一根会拿到会话早已死亡的遗留任务（实测）。
// - 以重启为锚而非热更窗口：热更后要进入裁决区需一次真实静默，其下限被钉在稳定窗（非 TUI 60s／TUI 90s），钉成常驻锚只会变成负载敏感的偶发红灯；
// - 重启入口有确定裁决：RestoreTask 把 running 降级 suspect，TaskID 桥先裁孤儿、再把被当前 monitor 跟踪的任务提升回 running；
// - 一条存活会话被判「未跟踪」，等价于监视信号在某一代工具手里丢失——该链与热更窗口检验的是同一处 seam。
func TestLiveSessionStaysWatchedAcrossToolGeneration(t *testing.T) {
	if phase := os.Getenv(sessionWatchPhaseEnv); phase != "" {
		sessionWatchChild(t, phase)
		return
	}
	root := t.TempDir()
	yamlPath := filepath.Join(root, "tagent.yaml")
	env := append(os.Environ(), sessionWatchYamlEnv+"="+yamlPath)

	runBootChild(t, env, sessionWatchPhaseEnv+"=spawn", sessionWatchFilter)

	runBootChild(t, env, sessionWatchPhaseEnv+"=restart", sessionWatchFilter)

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

// sessionWatchChild runs one boot of the live-session anchor as its own process.
//
// The spawn phase owns one fixed session name so phase logs stay attributable, and
// kills any session of that name first: a named session survives a CRASHED earlier run
// of this test the same way it survives a restart, while the tool refuses a duplicate
// name — without that preflight the anchor stops being re-runnable after a failed run.
// It then lets the record reach the durable path and leaves WITHOUT Close, so nothing
// reaps the session and the task never settles.
//
// The publish phase writes a NEW generation, which re-assembles the owner's ActionTool.
// The current generation must still own the live session: asking for the same logical
// name has to be refused as a duplicate, not answered by a second session nobody was
// tracking.
//
// The final wait is bounded because a full test run hosts every package binary at once
// and the boot's first tmux verification can miss the window. List() re-runs
// reconcileDetached, so re-fetching exercises the designed re-adjudication path rather
// than mere patience: alone or whole-package the promotion lands before the first
// fetch, and a session that never promotes still fails with the same message.
func sessionWatchChild(t *testing.T, phase string) {
	yamlPath := os.Getenv(sessionWatchYamlEnv)
	require.NotEmpty(t, yamlPath)
	root := filepath.Dir(yamlPath)

	switch phase {
	case "spawn":
		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAML(root)), 0o644))
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
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "hotreload":
		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAML(root)), 0o644))
		killOwnSession(t, "n-"+sessionWatchSvcName)
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

		require.NoError(t, os.WriteFile(yamlPath, []byte(sessionWatchYAMLAfterPublish(root)), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
		before := m.count()
		entry.CheckOrgReload()
		require.Greater(t, diagInt64(t, entry.OrgDiagnostics(), "generation"), int64(0),
			"precondition: the publish really advanced the generation")

		_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("start it again"))
		require.NoError(t, err)
		waitFor(t, "the second attempt was answered", func() bool { return m.count() > before+1 })

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

// TestOrgDelegation_AllLevelsRepublishedReachTheNewLeaf 钉住 每一层都改变时，结构发布必须在每个深度确定性地交付新声明。
// - 非法形状是按 map 序走变更集，父壳早于子壳装配、DFS 命中旧常驻子而把旧目标烙进新面，使结果逐次翻覆；
// - 一代生效后的下一回合必须在每个深度都跑它自己的新声明，面只来自被发布的那一代、不回落进旧快照。
// 契约: docs/wiki/agent/execution-generations.md#published-wrapper-immutable
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

	write(allLevelsChangedYAML())
	entry.CheckOrgReload()

	before := countServed(m.snapshot(), "SUB-C-PROMPT-G2")
	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("after"))
	require.NoError(t, err)
	waitFor(t, "the next turn reaches the new leaf at depth 3", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT-G2") > before
	})
}

// TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget 钉住 跨发布时，停在途的 B 向下委派到 C 仍跑它被钉那一代声明的 C。
// - 补齐别的锚到不了的这一面：稳定态各层见自身声明、单跳发起者持自身代都已证，唯独 B 停在途中、G2 换掉 C 后 B 向下到 C 没证；
// - 第三条回合必须跑新 C，使本锚自判别——发布从未落地时第一条断言会因错误理由通过。
// - 见证按序号取而非取最新：只有本次委派自身的 settle 能产出下一条记录，而它不可能早于该跳生产者返回——两跳落在两次轮询之间时见证才不会漂。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestOrgDelegation_NestedHopKeepsTheInitiatingGenerationTarget(t *testing.T) {
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

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first request"))
	require.NoError(t, err)
	waitFor(t, "the leaf served on G1", func() bool { return countServed(m.snapshot(), "SUB-C-PROMPT") >= 1 })

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
	entry.CheckOrgReload()

	entriesBefore := countServed(m.snapshot(), "SUB-C-PROMPT")
	g2Before := countServed(m.snapshot(), "SUB-C-PROMPT-G2")
	disarmGate(bGate)

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
	pinned := answers[hopsBefore]
	require.Contains(t, pinned, `"served:SUB-C-PROMPT"`,
		"被钉跳的回执必须是 G1 之 C 的回答（派生前继承发起调用租约）")
	require.NotContains(t, pinned, `"served:SUB-C-PROMPT-G2"`,
		"§3.2：B 的 G1 代执行不得因为 G2 换了 C 就被改道到新目标——被钉跳的回执不能来自新代目标")
	require.Greater(t, countServed(m.snapshot(), "SUB-C-PROMPT"), entriesBefore,
		"the pinned hop really executed on the old C (its answer came from somewhere)")

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("third request"))
	require.NoError(t, err)
	waitFor(t, "a fresh turn serves the new C", func() bool {
		return countServed(m.snapshot(), "SUB-C-PROMPT-G2") > g2Before
	})
}

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
	pick string
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

// TestOrgReentry_RelaunchActionRefusesTargetRemovedByPublishedGeneration 钉住 生产入口上被发布移除目标的重放被版本拒绝且不新建执行。
// - 入口被 Offer 的是它自己的委派加两个生产任务工具（relaunch/resume），board 项是生产 spawn 身份；
// - 换代把 a 改路由 c、b 从有效面消失（其常驻 owner 仍在，正是复活风险面）；
// - 被拒重入不得重跑那个目标，也不得静默改用替身跑新目标。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
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

	decls := entryDeclarations(m.snapshot())
	require.NotEmpty(t, decls)
	require.ElementsMatch(t, []string{"b", "relaunch_task", "resume_task"}, decls[0],
		"the entry is offered its delegation AND the production task-action tools")

	tk := subagentTask(t, entry.TaskManager())
	require.Equal(t, "b:work", tk.Spec.Key, "the board entry is the production spawn identity")

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

// TestOrgReentry_RelaunchActionAfterPublishRunsCurrentGenerationTarget 钉住 目标经结构发布仍被路由时，无发起调用重入经新发布面运行它。
// - 一次保持 a→b 的结构编辑（max_tool_iterations 进指纹）确实发布新一代并重建 wrapper；
// - 缺常驻 owner 接线的候选会在此 fail-closed（"no resident owner"），正是本测要证伪的接线。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
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
