package tagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
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

// TestOrgCrossPublish_BackgroundExecutionKeepsTargetAcrossPublish is the「后台执行」
// clause at the entry: an async-spawned delegation is parked mid-run while a new
// generation (routing C) is published; the background producer still runs on the G1
// target B, C must not be stolen, and B's answer still reaches the requesting turn.
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

// TestOrgCrossPublish_RetiredGenerationReclaimedAfterInFlight completes 3.4's「旧执行
// 最终停止／资源回收」leg at the entry: while a delegation is in flight the retired G1
// generation stays on the books (held by its own reference); once the call lands and
// the turn releases, that generation must be independently reclaimed — not kept alive
// forever, and not force-closed while still referenced.
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

// TestOrgCrossPublish_QueuedInputTakesNewGenerationAtExecution is 3.4's「排队输入取
// 执行时版本」leg — and the specific thing that two already-closed sequential turns
// CANNOT show: a second input arrives WHILE the first turn is parked in flight, so it
// is genuinely queued across the publication. It must then execute against the
// generation in force at ITS execution time (C), while the in-flight first turn stays
// on B.
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

// TestOrgCrossPublish_NoToolCallRunsNeitherTarget is the「不调用工具时 B/C 都不执行」
// negative control at the entry: a published face may OFFER a delegation, but if the
// model does not call it, neither the offered target nor any other runs. Guards against
// a hot path that eagerly executes a routed executor independent of the model's choice.
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

// TestOrgCrossPublish_SettleTurnRunsOnTheNewGeneration is §3.4's
// 「真实 ACK 已返回且父 turn 已结束、后台 producer 停屏障期间发布 G2——后台用 G1，
// 停止后 task_settled 新 turn 用 G2」— the row its own clause says the earlier
// anchors stop short of (「现有测试止于 inline 返回」).
//
// The ack is forced rather than assumed: the wrapper's dense window is shortened
// so the task layer detaches and answers with an ack while B stays parked, and
// the test asserts the parent turn closed WITHOUT B's answer in hand before
// publishing. Per 3.4's caution it never demands「C 总调用数必须零」— the
// publish also raises a notice turn that may delegate on the current face, which
// is legal; the claim is attribution: the G1 run ran once on its own target, and
// the turn the settle raises afterwards runs on G2's declarations.
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
