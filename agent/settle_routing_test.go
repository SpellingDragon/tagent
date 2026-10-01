// 本文件负责结算后的路由与投递屏障：注册表路由、调用标识绑定、后台结算优先走绑定总线并在
// 缺失时回落，以及投递计数在屏障后必须可对账。
// 契约: docs/wiki/platform/reincarnation-notice.md#delivery
// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// drainIDs pulls everything currently on the bus and returns the event ids seen.
func drainIDs(bus *EventBus) []string {
	var out []string
	for _, e := range bus.TryPull() {
		if e != nil {
			out = append(out, e.ID)
		}
	}
	return out
}

// TestSettleSinkRegistry_Routing 钉住 路由裁决的分支形状：已绑定的调用靠把结算发布到自己的总线来接它。
// - 结算因此成为一条普通可拉取事件，不设旁路队列；
// - 未知或空标识返回未命中，由调用方回落到共享总线；解绑即停止路由。
func TestSettleSinkRegistry_Routing(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()

	evt := &AgentEvent{ID: "e1"}
	require.False(t, r.route("X", evt), "no binding → fall back to bus")
	require.False(t, r.route("", evt), "empty invocation id → fall back (non-delegation)")

	r.bind("X", invBus)
	require.True(t, r.route("X", evt), "bound bus takes the event via Publish")
	require.Contains(t, drainIDs(invBus), "e1", "the settle is on the bound bus, not a bypass queue")

	r.unbind("X")
	require.False(t, r.route("X", evt), "after unbind → fall back (loop exited)")
}

// TestTaskInvocationID locks reading the S2m provenance handle off a settled task.
func TestTaskInvocationID(t *testing.T) {
	require.Equal(t, "", taskInvocationID(nil), "nil task → empty (non-delegation)")
	require.Equal(t, "", taskInvocationID(&task.Task{}), "no Origin → empty")
	tk := &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: "deleg-x"}}}
	require.Equal(t, "deleg-x", taskInvocationID(tk))
}

// TestDeliverTaskSettledDecision 钉住 locks the routing decision the agent's OnSettle hook calls: a bound invocation's bus takes it; otherwise the shared bus does.
func TestDeliverTaskSettledDecision(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	tk := func(id string) *task.Task {
		return &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	}

	deliverTaskSettled(sinks, bus, tk("no-sink"), &AgentEvent{ID: "b1", Source: SourceTask})
	require.Contains(t, drainIDs(bus), "b1", "unsettled delegation id → routed to persistentBus (unchanged behavior)")

	invBus := NewEventBus()
	sinks.bind("X", invBus)
	deliverTaskSettled(sinks, bus, tk("X"), &AgentEvent{ID: "s1"})
	require.Contains(t, drainIDs(invBus), "s1", "bound bus owns the settle")
	require.NotContains(t, drainIDs(bus), "s1", "routed settle must NOT also hit the shared bus")
}

// TestDrainSettleBusTo 钉住 循环退出终态排空：unbind 之前窗口内 route() 已发布到调用总线、
// 却没有消费者剩下的 settle，必须被转发到共享总线而非静默丢弃——registry 注释承诺的
// "safe drop" 只有在回落总线上真的可见时才成立。nil 任一侧安全、空总线无副作用。
func TestDrainSettleBusTo(t *testing.T) {
	inv := NewEventBus()
	shared := NewEventBus()

	inv.Publish(&AgentEvent{ID: "window-1", Source: SourceTask})
	inv.Publish(&AgentEvent{ID: "window-2", Source: SourceTask})
	drainSettleBusTo(inv, shared)
	ids := drainIDs(shared)
	require.Contains(t, ids, "window-1", "windowed settle must surface on the persistent bus")
	require.Contains(t, ids, "window-2", "every residual event is forwarded, not just the first")
	require.Empty(t, inv.TryPull(), "the drain empties the invocation bus")

	drainSettleBusTo(inv, shared)
	require.Empty(t, drainIDs(shared), "draining an already-empty inv bus has no effect")

	drainSettleBusTo(nil, shared)
	drainSettleBusTo(inv, nil)
}

// TestBackgroundSettleRoutesToBoundBus 钉住 真实后台结算经绑定了投递回调的任务管理器时，必须到达该调用所绑定的总线。
// - 只落到共享总线，等待中的那次调用就拿不到结果。
func TestBackgroundSettleRoutesToBoundBus(t *testing.T) {
	sinks := newSettleSinkRegistry()
	fallback := NewEventBus()
	invBus := NewEventBus()
	sinks.bind("deleg-x", invBus)

	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			deliverTaskSettled(sinks, fallback, tk, newTaskSettledEvent(tk, sig, 0, ""))
		},
	})
	d := task.NewManualDetectorDetach(40 * time.Millisecond)
	res := tm.Spawn(
		task.TaskSpec{Kind: "command", Desc: "late job", Origin: map[string]string{metaKeyInvocationID: "deleg-x"}},
		d,
	)
	require.False(t, res.Settled, "expected background (ack), not inline settle")
	d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "done later"})
	d.Done()

	deadline := time.Now().Add(3 * time.Second)
	var settled []*AgentEvent
	for time.Now().Before(deadline) {
		if got := invBus.TryPull(); len(got) > 0 {
			settled = got
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NotEmpty(t, settled, "background settle not published to the bound invocation bus")
	require.Contains(t, settled[0].Message.Content, "done later", "the routed event is the task_settled result")
}

// TestBackgroundSettleFallsBackToBus 钉住 未绑定总线时（入口属主或未绑定调用），真实后台结算仍发到共享总线——行为保持不变。
func TestBackgroundSettleFallsBackToBus(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			deliverTaskSettled(sinks, bus, tk, newTaskSettledEvent(tk, sig, 0, ""))
		},
	})
	d := task.NewManualDetectorDetach(40 * time.Millisecond)
	res := tm.Spawn(
		task.TaskSpec{Kind: "command", Desc: "late job", Origin: map[string]string{metaKeyInvocationID: "nobody-listening"}},
		d,
	)
	require.False(t, res.Settled)
	d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "later"})
	d.Done()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range bus.TryPull() {
			if e != nil && e.Source == SourceTask {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("unbound background settle did not fall back to the bus")
}

// TestSettleAccounting_DeliveryBarrier 钉住 静默判据：委派的每个派生任务都被投递到绑定总线之后才算静默。
// - 任务仅达到终态状态不构成静默——判定看的是送达。
func TestSettleAccounting_DeliveryBarrier(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()
	r.bind("X", invBus)

	require.True(t, r.quiescent("X"), "fresh binding with nothing spawned is quiescent")

	r.noteSpawn("X")
	require.False(t, r.quiescent("X"), "spawned task with settle not yet delivered must keep the tail alive")

	r.noteSpawn("X")
	require.False(t, r.quiescent("X"))

	require.True(t, r.route("X", &AgentEvent{ID: "e1"}))
	require.False(t, r.quiescent("X"), "one delivered, one still expected")

	require.True(t, r.route("X", &AgentEvent{ID: "e2"}))
	require.True(t, r.quiescent("X"), "spawned == delivered → quiescent")

	require.ElementsMatch(t, []string{"e1", "e2"}, drainIDs(invBus))
}

// TestSettleAccounting_NoBindingIsUnaccounted 钉住 未绑定总线时行为保持中性：登记派生是空操作、静默判定为假、路由回落到未命中。
// - 三条路径都不得触碰任何计数器。
func TestSettleAccounting_NoBindingIsUnaccounted(t *testing.T) {
	r := newSettleSinkRegistry()

	r.noteSpawn("ghost")
	require.False(t, r.quiescent("ghost"), "no binding → never quiescent (caller falls back to bus)")
	require.False(t, r.route("ghost", &AgentEvent{ID: "e"}), "no binding → route false → bus fallback")
}

// TestSettleAccounting_UnaccountedDeliveryDoesNotGoNegative 钉住 总线已绑但从未记过账的标识仍要完成投递，且计数绝不落到负数。
// - "迟到结算先于记账"是允许的顺序；计数未知不得伪装成零，也不得反向扣减。
func TestSettleAccounting_UnaccountedDeliveryDoesNotGoNegative(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()
	r.bind("Y", invBus)

	require.True(t, r.route("Y", &AgentEvent{ID: "s1"}), "delivery to a bound bus succeeds regardless of accounting")
	require.True(t, r.quiescent("Y"), "pending stayed 0 (guarded decrement never negative)")
	require.Contains(t, drainIDs(invBus), "s1")
}

// TestSettleAccounting_UnbindClosesAccounting 钉住 解绑委派的总线要同时清掉路由目标与记账，使迟到结算回落到总线并安全丢弃。
// - 解绑延后到这一步是有意排序：不能让记账先于路由消失。
func TestSettleAccounting_UnbindClosesAccounting(t *testing.T) {
	r := newSettleSinkRegistry()
	invBus := NewEventBus()
	r.bind("Z", invBus)
	r.noteSpawn("Z")
	require.False(t, r.quiescent("Z"))

	r.unbind("Z")
	require.False(t, r.quiescent("Z"), "after unbind → no binding → not quiescent")
	require.False(t, r.route("Z", &AgentEvent{ID: "late"}), "after unbind → route false → bus fallback")
}

// stubSpawner returns a fixed SpawnResult so the accounting decorator can be tested
// without a real task manager or timing.
type stubSpawner struct{ settled bool }

func (s *stubSpawner) Spawn(task.TaskSpec, task.SettleDetector) task.SpawnResult {
	return task.SpawnResult{Settled: s.settled}
}

// TestCountingSpawner_BooksOnlyBackground 钉住 记账装饰器只为后台派生记一笔预期结算，其路由之后再清除。
// - 内联派生先记账随即作废，绝不留幽灵待决把屏障困住；
// - 未绑定总线时整个装饰器是空操作，对非委派回合保持行为中性。
func TestCountingSpawner_BooksOnlyBackground(t *testing.T) {
	r := newSettleSinkRegistry()
	bx := NewEventBus()
	r.bind("X", bx)

	(&countingSpawner{inner: &stubSpawner{settled: false}, sinks: r, id: "X"}).Spawn(task.TaskSpec{}, nil)
	require.False(t, r.quiescent("X"), "a background spawn books an expected settle")
	require.True(t, r.route("X", &AgentEvent{ID: "s"}), "its settle delivers")
	require.True(t, r.quiescent("X"), "spawned == delivered → quiescent")

	by := NewEventBus()
	r.bind("Y", by)
	(&countingSpawner{inner: &stubSpawner{settled: true}, sinks: r, id: "Y"}).Spawn(task.TaskSpec{}, nil)
	require.True(t, r.quiescent("Y"), "an inline settle is voided immediately — no phantom pending")

	res := (&countingSpawner{inner: &stubSpawner{settled: false}, sinks: r, id: "ghost"}).Spawn(task.TaskSpec{}, nil)
	require.False(t, res.Settled, "the inner spawn result is passed through unchanged")
	require.False(t, r.quiescent("ghost"), "unbound id is never accounted")
}

// quiescent lives in a _test.go file since the  dead-code audit: it is
// the tests' termination oracle only (awaiting, in settle_routing.go, is what
// production consults); kept as a method so the oracle reads exactly the state
// the production predicate sees.
// quiescent reports whether the invocation's bus is bound AND every task spawned
// under it has had its settle delivered (spawned == delivered). This is the D-b
// termination predicate, immune to the terminal-before-delivery race. It is the
// shared shell's越窗 exit condition: bound ∧ pending==0 ∧ the bus drains empty.
func (r *settleSinkRegistry) quiescent(id string) bool {
	if r == nil || id == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byInv[id]; !ok {
		return false
	}
	return r.pending[id] == 0
}

// TestOnSettle_WritesDeterministicFeedback 钉住 持久化落下的结算必须自动绑定一条确定性裁决的反馈事件。
// - 完成⇒正例、失败⇒负例；可疑与已脱离什么都不写——护栏只接受确定裁决，不被含糊结算污染；
// - 锚点就是该结算事件本身：结算记录即任务产出，其打包标识使它可归因。
func TestOnSettle_WritesDeterministicFeedback(t *testing.T) {
	cases := []struct {
		status      string
		wantVerdict string
	}{
		{"completed", "positive"},
		{"failed", "negative"},
		{"suspect", ""},
		{"alive-detached", ""},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			store := memory.NewInMemoryStore()
			cm := &ContextManager{
				name:        "tagent",
				memStore:    store,
				projection:  compress.NewSessionProjection(),
				partitionID: 7,
			}
			evt := &AgentEvent{
				Type:      "external_input",
				Source:    SourceTask,
				Timestamp: time.Now(),
				Message:   &model.Message{Role: model.RoleUser, Content: "[task settled] done"},
				Metadata:  map[string]any{"settle_status": tc.status},
			}
			cm.persistBusEvent(evt)

			refs, err := store.QueryEvents(memory.QueryOptions{
				PartitionIDs: []int{7}, EventTypes: []string{"feedback"}, Limit: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantVerdict == "" {
				if len(refs) != 0 {
					t.Fatalf("status %q must not write feedback, got %d", tc.status, len(refs))
				}
				return
			}
			if len(refs) != 1 {
				t.Fatalf("status %q: expected 1 feedback, got %d", tc.status, len(refs))
			}
			fb, err := store.GetEvent(refs[0].EventKey)
			if err != nil || fb == nil {
				t.Fatal(err)
			}
			if !contains(fb.Content, `"verdict":"`+tc.wantVerdict+`"`) {
				t.Fatalf("verdict missing in content: %s", fb.Content)
			}
			if fb.Metadata["subtype"] != "task_settle" {
				t.Fatalf("subtype = %q", fb.Metadata["subtype"])
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// ctxCaptureModel records the ctx handed to GenerateContent so tests can
// assert on context-injected values (e.g. the task spawner lineage).
type ctxCaptureModel struct {
	mu   sync.Mutex
	ctx  context.Context
	info model.Info
}

func (m *ctxCaptureModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{ID: "t", Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}
	close(ch)
	return ch, nil
}

func (m *ctxCaptureModel) Info() model.Info { return m.info }

func (m *ctxCaptureModel) captured() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ctx
}

// TestRunFlow_SpawnCarriesTriggerSourceLineage 钉住 写侧谱系：回合入口必须把本次触发源注入派生包装器。
// - 冥想回合内派生的任务，其结算事件因此带着冥想谱系；
// - 若结算只剩光秃秃的任务来源，投递门就认不出它，内部产出会流向用户最后的闲聊会话。
func TestRunFlow_SpawnCarriesTriggerSourceLineage(t *testing.T) {
	m := &ctxCaptureModel{info: model.Info{Name: "mock"}}
	cm := newTestContextManager("lineage-cm", m, nil, make(chan *trpcEvent.Event, 16), NewEventBus())
	cm.taskController = task.NewTaskManager(task.TaskManagerConfig{})
	cm.triggerSource = "meditation"

	if err := cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"}); err != nil {
		t.Fatalf("RunFlow: %v", err)
	}

	sp, ok := task.TaskSpawnerFromContext(m.captured())
	if !ok {
		t.Fatal("no TaskSpawner in RunFlow ctx")
	}
	os, ok := sp.(*task.OriginSpawner)
	if !ok {
		t.Fatalf("spawner = %T, want *task.OriginSpawner", sp)
	}
	if got := os.Origin[tagentevent.MetaKeyTriggerSource]; got != "meditation" {
		t.Fatalf("Origin[trigger_source] = %q, want %q", got, "meditation")
	}
}

// TestRunFlow_SpawnBareControllerWithoutTriggerSource 钉住 钉住 触发源为空时，裸 TaskController 仍作为 spawner 注入上下文，且不得被误判成 OriginSpawner。
func TestRunFlow_SpawnBareControllerWithoutTriggerSource(t *testing.T) {
	m := &ctxCaptureModel{info: model.Info{Name: "mock"}}
	cm := newTestContextManager("bare-cm", m, nil, make(chan *trpcEvent.Event, 16), NewEventBus())
	cm.taskController = task.NewTaskManager(task.TaskManagerConfig{})
	cm.triggerSource = ""

	if err := cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"}); err != nil {
		t.Fatalf("RunFlow: %v", err)
	}

	sp, ok := task.TaskSpawnerFromContext(m.captured())
	if !ok {
		t.Fatal("no TaskSpawner in RunFlow ctx")
	}
	if _, isOrigin := sp.(*task.OriginSpawner); isOrigin {
		t.Fatal("empty trigger source must keep the bare TaskController spawner")
	}
}

// completionOnlyEnvelope drives the real protocol to the "Phase A durable,
// Phase B never landed" state: input committed, completion frozen on the
// envelope, NO receipt on the chain, claim held (state=claimed).
func completionOnlyEnvelope(t *testing.T, dir string) (*TagentAgent, *memory.InMemoryStore, string) {
	t.Helper()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("recon-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))
	healthy := ta.contextManager.memStore.(*memory.InMemoryStore)

	ta.contextManager.memStore = &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), bus.DurablePending(), "precondition: not acked")
	ta.contextManager.memStore = healthy
	return ta, healthy, batch[0].claim.Path
}

func envelopeAt(t *testing.T, path string) *reliability.Envelope {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var env reliability.Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	return &env
}

func tamperEnvelope(t *testing.T, path string, edit func(*reliability.Envelope)) {
	t.Helper()
	env := envelopeAt(t, path)
	edit(env)
	raw, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))
}

func countReceipts(t *testing.T, store interface {
	QueryEvents(memory.QueryOptions) ([]memory.EventReference, error)
}) int {
	t.Helper()
	n := 0
	refs, _ := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			n++
		}
	}
	return n
}

// TestReconcile_PreparedOnlyContinuesInput 钉住 只有预处理事实、尚无完成记录的信封不由回收处置。
// - 它留给正常重放路径补齐后执行：不落回执、不确认。
func TestReconcile_PreparedOnlyContinuesInput(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("only-prep"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Continued)
	require.Zero(t, s.ReceiptsAdded+s.Cleaned+s.Quarantined+s.Blocked, "a prepared-only envelope is untouched")
	require.FileExists(t, batch[0].claim.Path)
	require.Equal(t, int64(1), bus.DurablePending(), "continue input: nothing consumed")
	require.Equal(t, 0, countReceipts(t, ta.contextManager.memStore))
}

// TestReconcile_CompletionOnlyResubmitsReceiptOnly 钉住 "完成已持久、回执未落地"这一崩溃窗口靠重放收敛：只重投信封自身完成里冻结的回执并清理。
// - 不重跑模型（输入事实数不增长），也不重新冻结；
// - 收敛不依赖投影扫描曾列出该键——直接盘点的性质正是收割路径不必要的原因。
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestReconcile_CompletionOnlyResubmitsReceiptOnly(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)
	reserved := envelopeAt(t, path).ReceiptKey
	require.NotEmpty(t, reserved)
	require.Equal(t, 0, countReceipts(t, healthy), "precondition: no receipt on the chain")

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.ReceiptsAdded, "completion-only → re-submit the receipt, then clean up")
	require.Zero(t, s.Quarantined+s.Blocked)

	require.NoFileExists(t, path, "envelope cleaned up")
	require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	require.Equal(t, 1, countReceipts(t, healthy), "exactly the ONE frozen receipt landed")
	key, kerr := tagentevent.ParseEventKey(reserved)
	require.NoError(t, kerr)
	stored, gerr := healthy.GetEvent(key)
	require.NoError(t, gerr, "receipt verified under the envelope's OWN reserved key")
	require.Equal(t, tagentevent.TypeInboxReceipt, stored.EventType)
	require.Equal(t, 2, healthy.GetStats().TotalEvents, "input fact + receipt only — no new input, no re-run")
}

// TestReconcile_MatchingReceiptCleansUpOnly 钉住 回执已持久而确认时崩溃的窗口：链上已有预留键下的回执，回收只能做清理。
// - 不得二次提交回执；保留额的释放走同一条确认持久的路径。
func TestReconcile_MatchingReceiptCleansUpOnly(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)

	env := envelopeAt(t, path)
	cred, verr := ta.contextManager.verifyReceiptCredential(env.Completion)
	require.NoError(t, verr)
	require.Equal(t, 1, countReceipts(t, healthy), "receipt on the chain")

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Cleaned)
	require.Zero(t, s.ReceiptsAdded, "a matching receipt is NEVER re-submitted")
	require.Equal(t, cred.ReceiptKey, env.ReceiptKey)
	require.NoFileExists(t, path)
	require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	require.Equal(t, 1, countReceipts(t, healthy), "cleanup added no second receipt")
	require.Equal(t, 2, healthy.GetStats().TotalEvents, "input + the single receipt, nothing else")
}

// TestReconcile_ContradictionsQuarantine 钉住 each contradiction is isolated with its bytes kept — never re-stamped, never cleaned up, never input-consumed.
func TestReconcile_ContradictionsQuarantine(t *testing.T) {
	t.Run("undecodable completion", func(t *testing.T) {
		dir := t.TempDir()
		ta, healthy, path := completionOnlyEnvelope(t, dir)
		tamperEnvelope(t, path, func(e *reliability.Envelope) { e.Completion = json.RawMessage(`"a string"`) })

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined)
		require.NoFileExists(t, path, "removed from the live inbox")
		require.FileExists(t, filepath.Join(dir, "inbox-v2", "quarantine", filepath.Base(path)), "bytes KEPT for inspection")
		require.Equal(t, int64(0), ta.persistentBus.DurablePending(), "capacity released through quarantine accounting")
		require.Equal(t, 0, countReceipts(t, healthy), "a contradiction never produces a receipt")
	})

	t.Run("reserved key owned by different content", func(t *testing.T) {
		dir := t.TempDir()
		ta, healthy, path := completionOnlyEnvelope(t, dir)
		env := envelopeAt(t, path)
		key, kerr := tagentevent.ParseEventKey(env.ReceiptKey)
		require.NoError(t, kerr)
		foreign := memory.FullEvent{EventKey: key, PartitionID: 1, EventType: tagentevent.TypeInboxReceipt,
			EventSummary: "forged", Content: "forged", Timestamp: 1, Metadata: map[string]string{"request_id": "forged"}}
		require.NoError(t, healthy.StoreEvent(key, foreign), "a DIFFERENT record owns the reserved key")

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined, "same-key-different-content is a deterministic contradiction")
		require.Equal(t, int64(0), ta.persistentBus.DurablePending(), "quarantine releases capacity, bytes kept")
		require.FileExists(t, filepath.Join(dir, "inbox-v2", "quarantine", filepath.Base(path)))
	})

	t.Run("completion identity drifts from the envelope", func(t *testing.T) {
		dir := t.TempDir()
		ta, _, path := completionOnlyEnvelope(t, dir)
		tamperEnvelope(t, path, func(e *reliability.Envelope) { e.RequestID = "someone-else" })

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined)
		require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	})

	t.Run("receipted state without completion", func(t *testing.T) {
		dir := t.TempDir()
		ta, _, path := completionOnlyEnvelope(t, dir)
		tamperEnvelope(t, path, func(e *reliability.Envelope) {
			e.State = reliability.InboxStateReceipted
			e.Completion = nil
		})

		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.Quarantined, "「任何缺 completion 的 receipt」quarantines — never falls through to continue-input")
		require.Equal(t, int64(0), ta.persistentBus.DurablePending())
	})
}

// ioFaultStore fails EVERY GetEvent with a transient I/O error (not not-found).
type ioFaultStore struct {
	*memory.InMemoryStore
}

func (s *ioFaultStore) GetEvent(key int64) (*memory.FullEvent, error) {
	return nil, errors.New("simulated disk I/O failure")
}

// TestReconcile_ReadIOBlocksConservatively 钉住 读不到的链不得被当作"回执缺失"来处置。
// - 信封保留：不提交、不隔离、不确认；一次健康的重试即可收敛。
func TestReconcile_ReadIOBlocksConservatively(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)
	ta.contextManager.memStore = &ioFaultStore{InMemoryStore: healthy}

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Blocked, "I/O trouble blocks the disposition, never guesses it")
	require.Zero(t, s.ReceiptsAdded+s.Cleaned+s.Quarantined)
	require.FileExists(t, path, "the envelope is retained")
	require.Equal(t, int64(1), ta.persistentBus.DurablePending())
	require.Equal(t, 0, countReceipts(t, healthy), "nothing was submitted on uncertain ground")

	ta.contextManager.memStore = healthy
	s2, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s2.ReceiptsAdded, "the same envelope converges once I/O recovers")
}

// TestReconcile_UnreadableEnvelopeBlocks 钉住 an envelope present but undecodable at inventory time blocks conservatively.
func TestReconcile_UnreadableEnvelopeBlocks(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("io-env"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(batch[0].claim.Path, []byte("not-json-anymore"), 0o644))

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.Blocked)
	require.Zero(t, s.Quarantined+s.ReceiptsAdded+s.Cleaned)
	require.FileExists(t, batch[0].claim.Path, "bytes retained, nothing guessed")
}

func credReceiptKeyHex(atMs int64) (int64, string) {
	key := memory.NewSnowflakeEventKey(1, atMs)
	return key, tagentevent.FormatEventKey(key)
}

func frozenCompletion(t *testing.T, receiptKeyHex string, fact memory.FullEvent) json.RawMessage {
	t.Helper()
	b, err := freezeCompletion(completion{
		CompletionVersion: completionVersion,
		RequestID:         "req-cred",
		ReceiptKey:        receiptKeyHex,
		CompletedAtMs:     1234567,
		BatchResult:       batchCompleted,
		Slots:             []completionSlot{{Slot: 0, SourceID: "s0", Disposition: slotSkipped, Reason: skipReasonNotSelected}},
		ReceiptFact:       fact,
	})
	require.NoError(t, err)
	return b
}

// TestVerifyReceiptCredential_IssuedOnlyAfterChainVerification 钉住 凭据就是预留键，且只在回执事实确认上链之后签发。
// - 重复验证收敛到已提交，绝不追加第二条回执事件。
func TestVerifyReceiptCredential_IssuedOnlyAfterChainVerification(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore()}
	_, keyHex := credReceiptKeyHex(1_700_000_000_000)
	fact, err := buildReceiptFact(keyHex, 1, "req-cred", "t", 1234567, nil)
	require.NoError(t, err)
	raw := frozenCompletion(t, keyHex, fact)

	cred, err := cm.verifyReceiptCredential(raw)
	require.NoError(t, err)
	require.Equal(t, keyHex, cred.ReceiptKey)
	require.Equal(t, 1, cm.memStore.GetStats().TotalEvents, "the verified receipt fact is on the chain")

	cred2, err := cm.verifyReceiptCredential(raw)
	require.NoError(t, err)
	require.Equal(t, cred, cred2, "verification is idempotent")
	require.Equal(t, 1, cm.memStore.GetStats().TotalEvents, "never a second receipt event")
}

// TestVerifyReceiptCredential_RejectsIllegalCompletion 钉住 garbage bytes or a foreign schema version are NOT a legal completion — no credential, no chain write.
func TestVerifyReceiptCredential_RejectsIllegalCompletion(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore()}

	cred, err := cm.verifyReceiptCredential(json.RawMessage(`not-json`))
	require.Error(t, err)
	require.Empty(t, cred.ReceiptKey)

	cred, err = cm.verifyReceiptCredential(json.RawMessage(`{"completion_version":99}`))
	require.Error(t, err)
	require.Empty(t, cred.ReceiptKey)
	require.Equal(t, 0, cm.memStore.GetStats().TotalEvents, "an illegal completion submits nothing")
}

// TestVerifyReceiptCredential_RejectsIdentityDrift 钉住 事件键不等于完成所预留键的回执事实是矛盾，不是凭据。
// - 必须在任何提交之前拒绝：外来的键不得借合法信封的预留混上链。
func TestVerifyReceiptCredential_RejectsIdentityDrift(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: memory.NewInMemoryStore()}
	_, keyHex := credReceiptKeyHex(1_700_000_000_000)
	_, otherHex := credReceiptKeyHex(1_700_000_001_000)
	require.NotEqual(t, keyHex, otherHex)
	fact, err := buildReceiptFact(otherHex, 1, "req-cred", "t", 1234567, nil)
	require.NoError(t, err)
	raw := frozenCompletion(t, keyHex, fact)

	cred, err := cm.verifyReceiptCredential(raw)
	require.ErrorContains(t, err, "identity contradiction")
	require.Empty(t, cred.ReceiptKey)
	require.Equal(t, 0, cm.memStore.GetStats().TotalEvents, "a drifting fact is never committed")
}

// TestVerifyReceiptCredential_CommitFailureYieldsNoCredential 钉住 回执在链上验证不过时根本走不到记录回执那一步——调用方继续持有认领。
func TestVerifyReceiptCredential_CommitFailureYieldsNoCredential(t *testing.T) {
	cm := &ContextManager{partitionID: 1, memStore: &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}}
	_, keyHex := credReceiptKeyHex(1_700_000_000_000)
	fact, err := buildReceiptFact(keyHex, 1, "req-cred", "t", 1234567, nil)
	require.NoError(t, err)
	raw := frozenCompletion(t, keyHex, fact)

	cred, err := cm.verifyReceiptCredential(raw)
	require.Error(t, err)
	require.Empty(t, cred.ReceiptKey)
}

// TestFinishDurableBatch_ReceiptFailureDoesNotMaskInput 钉住 输入事实已提交时，回执提交失败必须扣住领取，且不写任何回执事件。
// - 信封的准备预留（事实键与预留键）保持完整，供启动对账仅从持久完成重投回执；
// - 回执失败既不消费也不损坏输入证据——这正是不掩盖的含义。
func TestFinishDurableBatch_ReceiptFailureDoesNotMaskInput(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-mask"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))
	healthy := ta.contextManager.memStore
	require.Equal(t, 1, healthy.GetStats().TotalEvents)

	ta.contextManager.memStore = &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), bus.DurablePending(), "a receipt failure must not ack")
	m, ok, merr := bus.inbox.MaterialOfPath(batch[0].claim.Path)
	require.NoError(t, merr)
	require.True(t, ok)
	require.Equal(t, batch[0].claim.ReceiptKey, m.ReceiptKey, "the reserved key survives the failed attempt")
	require.NotEmpty(t, m.FactKeys, "the input's prepared fact key is untouched")

	require.Equal(t, 1, healthy.GetStats().TotalEvents, "input fact stands alone; no receipt was written")
	refs, _ := healthy.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		require.NotEqual(t, tagentevent.TypeInboxReceipt, r.EventType)
	}
}

// TestFinishDurableBatch_InputConflictIsNeverReceipted 钉住 与本轮冻结矛盾的已持久完成属输入侧确定性错误：不铸造凭据、不落回执。
// - 认领与权威的冻结字节都保留；回执错误不得覆盖输入证据，输入冲突也不得产出回执。
func TestFinishDurableBatch_InputConflictIsNeverReceipted(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-conflict"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))

	path := batch[0].claim.Path
	require.NoError(t, bus.RecordCompletion(path, json.RawMessage(`{"completion_version":1,"request_id":"other"}`)))

	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), bus.DurablePending(), "a completion conflict never receipts the envelope")
	refs, _ := ta.contextManager.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		require.NotEqual(t, tagentevent.TypeInboxReceipt, r.EventType,
			"an input-side conflict must never leave a receipt behind")
	}
}

func newSubmitGateAgent(t *testing.T, dir string) (*TagentAgent, *EventBus, *ContextManager) {
	t.Helper()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	ta := &TagentAgent{name: "submit-gate", persistentBus: bus, contextManager: cm}
	return ta, bus, cm
}

// TestSubmitDurableBatch_AllOrNothingOnConflict 钉住 钉住 持久批次全有或全无：某个信封出现确定性预备冲突时，同批合法的输入也不得提交。
func TestSubmitDurableBatch_AllOrNothingOnConflict(t *testing.T) {
	ta, bus, cm := newSubmitGateAgent(t, t.TempDir())
	ctx := context.Background()
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "A"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "B"}))
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 2)

	// Isolate B deterministically: freeze its on-disk envelope with a DIFFERENT
	// receipt_key than the fresh one submitDurableBatch will build → ErrReceiptKeyConflict.
	var pathB string
	for _, ev := range batch {
		if ev.Message != nil && ev.Message.Content == "B" {
			pathB = ev.claim.Path
		}
	}
	require.NotEmpty(t, pathB)
	require.NoError(t, bus.inbox.PrepareFacts(pathB, "conflicting-receipt-key",
		[]json.RawMessage{json.RawMessage(`{"event_key":1,"content":"B"}`)}))

	outcome := ta.submitDurableBatch(ctx, batch, batch)
	require.Equal(t, submitConflict, outcome.status, "a deterministic prepare conflict must be classified as such")
	require.Equal(t, pathB, outcome.conflict)
	require.Empty(t, cm.projection.GetAll(), "§3.6-①: a conflicting batch must NOT commit the good input A")
}

// TestSubmitDurableBatch_TransientNotConflict 钉住 钉住 预备阶段的瞬时 I/O 失败属可重试，不得判成冲突。
func TestSubmitDurableBatch_TransientNotConflict(t *testing.T) {
	ta, _, cm := newSubmitGateAgent(t, t.TempDir())
	ctx := context.Background()
	ev := &AgentEvent{
		ID: "x", Type: "external_input", Source: "user", Timestamp: time.Now(),
		Message:  &model.Message{Role: model.RoleUser, Content: "Z"},
		Metadata: map[string]any{},
		claim:    &durableClaim{Path: "/no/such/envelope.json", RequestID: "Z", Slot: 0},
	}
	outcome := ta.submitDurableBatch(ctx, []*AgentEvent{ev}, []*AgentEvent{ev})
	require.Equal(t, submitTransient, outcome.status, "an I/O prepare failure is transient, not a conflict")
	require.Empty(t, outcome.conflict, "a transient failure isolates nothing")
	require.Empty(t, cm.projection.GetAll(), "a transient failure commits no fact")
}

// TestReleaseBatchClaims_RequeuesForOrderedReclaim 钉住 钉住 releaseBatchClaims 把已领取的信封退回待取，使下一次按序领取不被打乱。
func TestReleaseBatchClaims_RequeuesForOrderedReclaim(t *testing.T) {
	ta, bus, _ := newSubmitGateAgent(t, t.TempDir())
	ctx := context.Background()
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "A"}))
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 1)

	ta.releaseBatchClaims(batch)

	again, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, again, 1, "a released claim must be re-claimable, never dropped")
	require.Equal(t, "A", again[0].Message.Content)
}

// TestSubmitDurableBatch_DeterministicStoreConflictIsolates 钉住 提交层的四态闭合：与不同内容相撞的冻结事实键在重放中永不可能成功。
// - 因此协议必须走与准备冲突相同的隔离出口处置信封，而不是把它当作瞬时 I/O 无限重试。
func TestSubmitDurableBatch_DeterministicStoreConflictIsolates(t *testing.T) {
	root := t.TempDir()
	store, bus, ta := r30Stack(root)
	defer store.Close()

	_, err := bus.PublishContext(context.Background(), durableMsg("good-input"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)
	require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())

	_, err = bus.PublishContext(context.Background(), durableMsg("colliding-input"))
	require.NoError(t, err)
	batch2, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch2, 1)
	require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch2); return st }())
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
	require.NoError(t, err)
	var goodKey int64
	for _, r := range refs {
		if r.EventType == "external_input" {
			goodKey = r.EventKey
		}
	}
	require.NotZero(t, goodKey)
	ev := batch2[0]
	var frozen map[string]any
	require.NoError(t, json.Unmarshal(ev.claim.PreparedFact, &frozen))
	frozen["event_key"] = goodKey
	frozen["content"] = "DIFFERENT-MATERIAL"
	evil, err := json.Marshal(frozen)
	require.NoError(t, err)
	ev.claim.PreparedFact = evil

	out := ta.submitDurableBatch(context.Background(), batch2, batch2)
	require.Equal(t, submitConflict, out.status, "a deterministic conflict is NEVER transient")
	require.Equal(t, ev.claim.Path, out.conflict)

	q, err := filepath.Glob(filepath.Join(root, "inbox", "inbox-v2", "quarantine", "*"))
	require.NoError(t, err)
	require.Len(t, q, 1, "the conflicting envelope is isolated, not retried forever")
	refs, err = store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
	require.NoError(t, err)
	for _, r := range refs {
		full, gerr := store.GetEvent(r.EventKey)
		require.NoError(t, gerr)
		require.NotContains(t, full.Content, "DIFFERENT-MATERIAL", "the colliding write never entered the chain")
	}
}

func newPreparedGateCM() *ContextManager {
	return &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
}

func evtWithPreparedClaim(fact json.RawMessage) *AgentEvent {
	return &AgentEvent{
		ID: "x", Type: "external_input", Source: "user", Timestamp: time.Now(),
		Message:  &model.Message{Role: model.RoleUser, Content: "hello"},
		Metadata: map[string]any{},
		claim:    &durableClaim{Path: "/tmp/e.json", RequestID: "r", Slot: 0, PreparedFact: fact},
	}
}

func TestPersistBusEvent_GatesUndecodablePreparedFact(t *testing.T) {
	cm := newPreparedGateCM()
	require.False(t, cm.persistBusEvent(evtWithPreparedClaim(json.RawMessage("{ not valid json"))),
		"a corrupt durable prepared_fact must gate the store, not restamp a fresh key")
	require.Empty(t, cm.projection.GetAll(), "gating must not append a phantom projection ref")
}

func TestPersistBusEvent_GatesIncompletePreparedFact(t *testing.T) {
	cm := newPreparedGateCM()
	incomplete := json.RawMessage(`{"content":"hello"}`)
	require.False(t, cm.persistBusEvent(evtWithPreparedClaim(incomplete)),
		"an incomplete prepared_fact must gate the store, not restamp a fresh key")
	require.Empty(t, cm.projection.GetAll())
}

func TestPersistBusEvent_ReusesCompletePreparedFact(t *testing.T) {
	cm := newPreparedGateCM()
	complete, err := json.Marshal(memory.FullEvent{
		EventKey:     987654321,
		PartitionID:  1,
		EventType:    "external_input",
		EventSummary: "hello",
		Content:      "hello",
		Metadata:     map[string]string{"agent_name": "a"},
	})
	require.NoError(t, err)
	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(complete)),
		"a complete prepared_fact is reused verbatim and stored")
	require.Len(t, cm.projection.GetAll(), 1)
}

// durableAgent builds a minimal agent wired to a durable bus over dir, with a
// real InMemoryStore fact chain — enough to exercise the full
// publish → claim → dedup-replay → receipt → ack lifecycle.
func durableAgent(t *testing.T, dir string) *TagentAgent {
	t.Helper()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
		bus:         bus,
	}
	return &TagentAgent{
		name:           "durable-test",
		persistentBus:  bus,
		contextManager: cm,
	}
}

// durableAgentReopen reuses cm's store+projection — the durable fact chain that
// SURVIVES the restart — while opening a fresh bus over dir (the replay path).
// Sharing the store is what makes the frozen-key idempotency observable: a
// replay that re-derived a fresh key would double-write and drift the
// projection; reusing the envelope's frozen prepared_fact must not.
func durableAgentReopen(t *testing.T, dir string, cm *ContextManager) *TagentAgent {
	t.Helper()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	cm.bus = bus
	return &TagentAgent{
		name:           "durable-test",
		persistentBus:  bus,
		contextManager: cm,
	}
}

func durableMsg(content string) *AgentEvent {
	return NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: content})
}

// TestDurableReceipt_ReplayNoDoubleWrite 钉住 在入库之后、回执之前崩溃的重放：同输入不重复入库，投影也不重复追加。
// - 信封携带冻结的预处理事实与回执键重放；复用冻结键时绝不重写时间、归因或摘要。
func TestDurableReceipt_ReplayNoDoubleWrite(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus

	_, err := bus.PublishContext(context.Background(), durableMsg("fact-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		t.Fatalf("first-claim prepare must freeze the fact durably, got status %d", st)
	}
	for _, evt := range batch {
		require.True(t, ta.contextManager.persistBusEvent(evt))
	}
	require.Equal(t, 1, ta.contextManager.memStore.GetStats().TotalEvents)
	projKeys := keysOf(ta.contextManager.projection.GetAll())
	require.Len(t, projKeys, 1)

	require.Equal(t, int64(1), bus.DurablePending())

	ta2 := durableAgentReopen(t, dir, ta.contextManager)
	replayed, err := ta2.persistentBus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, replayed, 1)
	require.NotNil(t, replayed[0].claim, "replayed input must carry a typed durable claim")
	require.NotEmpty(t, replayed[0].claim.PreparedFact, "replay must carry the frozen prepared fact (verbatim reuse)")
	require.NotEmpty(t, replayed[0].claim.ReceiptKey, "replay must carry the reserved receipt key")

	if st, _ := ta2.prepareBatchFacts(replayed); st != submitOK {
		t.Fatalf("replayed prepare must reuse the frozen fact, got status %d", st)
	}
	for _, evt := range replayed {
		require.True(t, ta2.contextManager.persistBusEvent(evt))
	}
	require.Equal(t, 1, ta2.contextManager.memStore.GetStats().TotalEvents,
		"replayed input must NOT double-write the fact")
	require.Equal(t, projKeys, keysOf(ta2.contextManager.projection.GetAll()),
		"projection stays idempotent by key")

	ta2.finishDurableBatch(context.Background(), replayed, replayed, completedOutcome())
	require.Equal(t, int64(0), ta2.persistentBus.DurablePending())
	require.Equal(t, 2, ta2.contextManager.memStore.GetStats().TotalEvents,
		"one original fact + one inbox_receipt event")

	// receipt 是事实链事件且不入投影。
	var receiptFound bool
	refs, _ := ta2.contextManager.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			receiptFound = true
		}
	}
	require.True(t, receiptFound, "receipt must live in the fact chain (dedup window truth source)")
	for _, ref := range ta2.contextManager.projection.GetAll() {
		require.NotEqual(t, tagentevent.TypeInboxReceipt, ref.EventType,
			"receipt must never enter the projection (§5.5 event.IsNonProjectionRecord)")
	}
}

// receiptFaultStore serves the fact chain normally but refuses EXPLICIT replay
// commits for the internal inbox-receipt event.  submits the receipt through the
// replay interface (not the old fresh-key StoreEvent), so receipt-commit durability
// failure must retain the claim — the ack is never granted without its receipt.
type receiptFaultStore struct {
	*memory.InMemoryStore
}

func (s *receiptFaultStore) ReplayEvent(key int64, e memory.FullEvent) (memory.ReplayResult, memory.FullEvent, error) {
	if e.EventType == tagentevent.TypeInboxReceipt {
		return 0, memory.FullEvent{}, errors.New("receipt replay disk full")
	}
	return s.InMemoryStore.ReplayEvent(key, e)
}

// TestDurableReceipt_StoreFailureKeepsClaim 钉住 回执事件提交失败时信封绝不被 ack：prepare 冻结事实并预留键使完成可成形，随后让回执的显式重放失败，claim 必须保留（绝不无凭据确认）。
func TestDurableReceipt_StoreFailureKeepsClaim(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	_, err := ta.persistentBus.PublishContext(context.Background(), durableMsg("x"))
	require.NoError(t, err)
	batch, err := ta.persistentBus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		t.Fatalf("prepare must freeze fact + reserve receipt key, got %d", st)
	}

	ta.contextManager.memStore = &receiptFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(1), ta.persistentBus.DurablePending(),
		"unbacked ack is forbidden — the claim must stay for replay")
}

// TestPersistBusEvent_UnpreparedClaimGates 钉住 （D2「准备失败不调用 StoreEvent」）
func TestPersistBusEvent_UnpreparedClaimGates(t *testing.T) {
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	gated := &AgentEvent{
		Message:   &model.Message{Role: model.RoleUser, Content: "half"},
		Timestamp: time.Now(),
		claim:     &durableClaim{Path: "/x/1.json", RequestID: "req-half", Slot: 0},
	}
	require.False(t, cm.persistBusEvent(gated), "unprepared claim must be gated (no half-written fact)")
	require.Equal(t, 0, cm.memStore.GetStats().TotalEvents, "gated claim writes nothing")
	require.Equal(t, 0, cm.projection.Len(), "gated claim appends nothing")

	evt := &AgentEvent{
		Message:   &model.Message{Role: model.RoleUser, Content: "orphan"},
		Timestamp: time.Now(),
	}
	require.True(t, cm.persistBusEvent(evt), "claim-less volatile event stores through the default branch")
	require.Equal(t, 1, cm.memStore.GetStats().TotalEvents)
	require.Equal(t, 1, cm.projection.Len())
}

func keysOf(refs []memory.EventReference) []int64 {
	out := make([]int64, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.EventKey)
	}
	return out
}

var _ = errors.New

// TestDurableReceipt_MultiEnvelopeBatchReplay 钉住 跨崩溃的多信封批次：带证据的重放与无证据的首次同批到达时，两者的输入事实都必须落库。
// - 每个信封都拿到自己的写回证据；回执加确认之后任何一方都不许丢。
func TestDurableReceipt_MultiEnvelopeBatchReplay(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus

	recA, err := bus.PublishEnvelopeContext(context.Background(), "user",
		[]model.Message{{Role: model.RoleUser, Content: "msg-A"}})
	require.NoError(t, err)
	require.True(t, recA.Durable)
	claimedA, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, claimedA, 1)
	if st, _ := ta.prepareBatchFacts(claimedA); st != submitOK {
		t.Fatalf("claimedA prepare must freeze the fact durably, got status %d", st)
	}
	for _, evt := range claimedA {
		require.True(t, ta.contextManager.persistBusEvent(evt))
	}
	keysA := keysOf(ta.contextManager.projection.GetAll())
	require.Len(t, keysA, 1)
	require.Equal(t, int64(1), bus.DurablePending())

	ta2 := durableAgent(t, dir)
	bus2 := ta2.persistentBus
	recB, err := bus2.PublishEnvelopeContext(context.Background(), "user",
		[]model.Message{{Role: model.RoleUser, Content: "msg-B"}})
	require.NoError(t, err)
	require.True(t, recB.Durable)

	batch, err := bus2.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 2, "A replay + B first delivery in one batch")

	if st, _ := ta2.prepareBatchFacts(batch); st != submitOK {
		t.Fatalf("replayed prepare must reuse the frozen fact, got status %d", st)
	}
	for _, evt := range batch {
		require.True(t, ta2.contextManager.persistBusEvent(evt))
	}
	require.Len(t, keysOf(ta2.contextManager.projection.GetAll()), 2,
		"both A and B input facts must exist after replay")
	require.Equal(t, int64(2), bus2.DurablePending(), "both envelopes still unconfirmed")

	provenance := bus2.DurableProvenance(batch)
	require.Len(t, provenance, 2, "both envelopes must be covered by provenance")

	ta2.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus2.DurablePending(),
		"both envelopes must be receipted+acked with their facts safely on chain")
}

func TestReplayProjectionHandler_ExcludesCurrentInternalRecords(t *testing.T) {
	ta := durableAgent(t, t.TempDir())
	handler := ReplayProjectionHandler(ta)

	internal := []memory.FullEvent{
		{EventKey: 101, EventType: tagentevent.TypeInboxReceipt},
		{EventKey: 102, EventType: tagentevent.TypeTaskSpawned},
		{EventKey: 103, EventType: tagentevent.TypeResidentSession},
		{EventKey: 104, EventType: tagentevent.TypeContextCompressSummary},
		{EventKey: 105, EventType: tagentevent.TypeAgentOutput,
			Metadata: map[string]string{tagentevent.MetaKeyTaskInlineRecord: "true"}},
	}
	for _, ev := range internal {
		handler(ev)
	}
	require.Equal(t, 0, ta.contextManager.projection.Len(),
		"§5.5: spill backfill excludes EVERY current internal record class, receipt included")

	handler(memory.FullEvent{EventKey: 106, EventType: tagentevent.TypeExternalInput,
		EventSummary: "real history"})
	require.Equal(t, 1, ta.contextManager.projection.Len(),
		"business events still flow back through the same handler")
}

func TestPersistBusEvent_NormalCommitExcludesInternalRecords(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	cm := ta.contextManager

	weird, err := json.Marshal(memory.FullEvent{
		EventKey: 9001, PartitionID: 1, EventType: tagentevent.TypeInboxReceipt,
		EventSummary: "s", Timestamp: time.Now().UnixMilli(),
	})
	require.NoError(t, err)
	gated := &AgentEvent{
		ID: "e1", Type: tagentevent.TypeExternalInput,
		Message:   &model.Message{Role: model.RoleUser, Content: "x"},
		Timestamp: time.Now(),
		claim:     &durableClaim{Path: "/x/1.json", RequestID: "r1", Slot: 0, PreparedFact: weird},
	}
	require.True(t, cm.persistBusEvent(gated), "the fact itself still commits")
	require.Equal(t, 0, cm.projection.Len(),
		"§5.5: the normal-commit append shares the event-package predicate — an internal record never occupies the projection")

	biz, err := json.Marshal(memory.FullEvent{
		EventKey: 9002, PartitionID: 1, EventType: tagentevent.TypeExternalInput,
		EventSummary: "input", Content: "hello", Timestamp: time.Now().UnixMilli(),
	})
	require.NoError(t, err)
	ok := &AgentEvent{
		ID: "e2", Type: tagentevent.TypeExternalInput,
		Message:   &model.Message{Role: model.RoleUser, Content: "hello"},
		Timestamp: time.Now(),
		claim:     &durableClaim{Path: "/x/2.json", RequestID: "r2", Slot: 0, PreparedFact: biz},
	}
	require.True(t, cm.persistBusEvent(ok))
	require.Equal(t, 1, cm.projection.Len())
}

// TestEventKeys_EndToEndContract 钉住 事件键的四段接缝必须按模型真实经历的方式串起来。
// - 时间线渲染 → 模型照抄十六进制串 → 参数解析 → 父存储查找，必须解析到同一批事件；
// - 各段单独全绿不代表接缝一致，接缝断裂只会表现为"看起来能用但取回的是空"。
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestEventKeys_EndToEndContract(t *testing.T) {
	store := memory.NewInMemoryStore()
	k1, k2 := int64(0x1201a3f4b5c01), int64(0x1201a3f4b5c02)
	if err := store.StoreEvent(k1, memory.FullEvent{EventKey: k1, EventType: "external_input", EventSummary: "部署请求", Content: "请部署 v2 到测试环境"}); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreEvent(k2, memory.FullEvent{EventKey: k2, EventType: "agent_output", EventSummary: "部署完成", Content: "v2 已部署,健康检查通过"}); err != nil {
		t.Fatal(err)
	}

	line1 := tagentevent.FormatEventPrefix(k1, "external_input") + " 部署请求"
	line2 := tagentevent.FormatEventPrefix(k2, "agent_output") + " 部署完成"

	extract := func(line string) string {
		inner := line[strings.Index(line, "[evt_")+len("[evt_") : strings.Index(line, "]")]
		return inner[:strings.Index(inner, "|")]
	}
	modelArgs := map[string]any{
		"request":    "分析这两次部署",
		"event_keys": []any{extract(line1), extract(line2)},
	}
	raw, _ := json.Marshal(modelArgs)

	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)

	if _, err := w.Call(context.Background(), raw); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if mock.lastInv == nil {
		t.Fatal("sub-agent was not invoked")
	}
	rs := mock.lastInv.RunOptions.RuntimeState
	rawCtx, ok := rs[ExternalContextKey].(json.RawMessage)
	if !ok {
		t.Fatalf("external_context missing: model-copied hex keys were dropped (the silent-failure mode this test guards against); RuntimeState=%v", rs)
	}
	var entries []ExternalContextEntry
	if err := json.Unmarshal(rawCtx, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].EventKey != k1 || entries[1].EventKey != k2 {
		t.Fatalf("resolved events must be exactly the ones the model pointed at, got %+v", entries)
	}
	if entries[0].EventSummary != "部署请求" || entries[1].EventSummary != "部署完成" {
		t.Fatalf("summaries must round-trip, got %+v", entries)
	}
}

// TestEventKeys_AutoInjectFallback 写明一层掩蔽行为：模型未传任何事件键（或全部解析失败）时，包装器静默注入近期投影事件而不是显式失败。
// - 线上因此看不见十六进制回归：调用日志记着 event_keys=0，而子 agent"看起来能用"；
// - 该回退是有意的人机工程，但必须作为"会掩蔽契约断裂"的一层被写明。
func TestEventKeys_AutoInjectFallback(t *testing.T) {
	store := memory.NewInMemoryStore()
	k := int64(0x77aa01)
	_ = store.StoreEvent(k, memory.FullEvent{EventKey: k, EventType: "external_input", EventSummary: "近期事件"})

	proj := compress.NewSessionProjection()
	proj.Append(memory.EventReference{EventKey: k, EventType: "external_input", EventSummary: "近期事件"})

	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	w.SetParentProjection(proj)

	raw, _ := json.Marshal(map[string]any{"request": "x", "event_keys": []any{"totally-not-a-key"}})
	if _, err := w.Call(context.Background(), raw); err != nil {
		t.Fatalf("Call: %v", err)
	}
	rs := mock.lastInv.RunOptions.RuntimeState
	if _, ok := rs[ExternalContextKey]; !ok {
		t.Fatalf("auto-inject fallback should have supplied recent events")
	}
}

var _ = agent.Info{}

// TestToInt64Key_HexContract 钉住 事件键参数按十六进制字符串（时间线形态）规范化解析；只认十进制的解析会把模型从自己上下文里抄来的键静默丢弃。
func TestToInt64Key_HexContract(t *testing.T) {
	k := int64(0x1201a3f4b5c6d)
	hex := tagentevent.FormatEventKey(k)

	if got := toInt64Key(hex); got != k {
		t.Errorf("canonical hex %q must parse to %d, got %d", hex, k, got)
	}
	if got := toInt64Key("evt_" + hex); got != k {
		t.Errorf("evt_-prefixed hex must parse, got %d", got)
	}
	if got := toInt64Key("12345"); got != 0x12345 {
		t.Errorf("digit-only string resolves as hex first, got %d", got)
	}
	if got := toInt64Key(float64(99)); got != 99 {
		t.Errorf("numeric fallback must survive, got %d", got)
	}
	if got := toInt64Key("not-a-key"); got != 0 {
		t.Errorf("garbage must yield 0, got %d", got)
	}
}
