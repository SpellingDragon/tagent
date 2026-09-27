package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// introduce-durable-workflow-engine §3.1/§3.2 契约测：一次业务 turn 只取一次
// 版本，且取到的执行器在本 turn 内（含重试 attempt）不因中途发布而改变。

// TestBeginTurn_ChecksOncePerTurnNotPerIteration pins where the organization
// check lives now. Before §3.1 the check ran in the BeforeModel callback, i.e.
// before EVERY LLM iteration — a turn with several iterations could therefore
// cross generations mid-flight. It must now fire exactly once per business turn
// (BeginTurn), and zero times from the model callbacks.
func TestBeginTurn_ChecksOncePerTurnNotPerIteration(t *testing.T) {
	var checks atomic.Int64
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("bind-check", capture, nil, make(chan *event.Event, 16), nil)
	cm.SetOrgReloader(func() { checks.Add(1) })

	// The turn boundary: check + pin. §2.3 — acquiring registers the in-flight
	// reference, so every acquire has a matching release.
	r, release := cm.BeginTurn()
	defer release()
	require.NotNil(t, r)
	require.Equal(t, int64(1), checks.Load(), "the turn boundary checks configuration exactly once")

	// Two further model iterations (two turns driven WITHOUT a boundary call):
	// the callbacks must not re-check — pre-§3.1 this grew the counter by one per
	// LLM call.
	for i := 0; i < 2; i++ {
		out, err := r.Run(context.Background(), "u", "s-check", model.NewUserMessage("iterate"))
		require.NoError(t, err)
		drainRunner(out, 5*time.Second)
	}
	require.Positive(t, capture.requestCount(), "precondition: the model was actually called")
	require.Equal(t, int64(1), checks.Load(),
		"BeforeModel must no longer run the organization check per LLM iteration")

	// Every turn gets its own check — the boundary is not a one-shot.
	_, release2 := cm.BeginTurn()
	release2()
	require.Equal(t, int64(2), checks.Load())
}

// TestRunFlowWithExecutor_PinnedExecutorSurvivesMidTurnPublish is §3.2's core:
// the executor taken at the turn boundary runs the whole turn, even when a new
// generation is published before the attempt starts (the retry-loop case). A
// turn that takes no pin resolves the current executor — that is the next
// turn's behavior, not this one's.
func TestRunFlowWithExecutor_PinnedExecutorSurvivesMidTurnPublish(t *testing.T) {
	gen1 := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("bind-pin", gen1, nil, make(chan *event.Event, 16), nil)
	cm.SetOrgReloader(func() {})

	pinned, releasePinned := cm.BeginTurn()
	defer releasePinned()
	require.NotNil(t, pinned)

	// Publish gen2 (new model instance = new generation's executor face).
	gen2 := &requestCapturingModel{resp: gateOKResp()}
	face := cm.ExecutorConfig()
	face.Model = gen2
	require.NotNil(t, cm.PublishExecutor(cm.NewExecutorCandidate(face), face))

	// The in-flight turn still executes on gen1.
	require.NoError(t, cm.RunFlowWithExecutor(context.Background(), model.NewUserMessage("in-flight"), pinned))
	require.Equal(t, 1, gen1.requestCount(), "the pinned executor must serve the whole turn")
	require.Equal(t, 0, gen2.requestCount(), "a mid-turn publication must not steal an in-flight turn")

	// A NEW turn (no pin) resolves the executor afresh and lands on gen2.
	next, releaseNext := cm.BeginTurn()
	defer releaseNext()
	require.NoError(t, cm.RunFlowWithExecutor(context.Background(), model.NewUserMessage("next"), next))
	require.Equal(t, 1, gen1.requestCount(), "the superseded generation must stop receiving turns")
	require.Equal(t, 1, gen2.requestCount(), "the next turn must use the newly published generation")
}

// NOTE（§5.2 交叉测试的前置发现）：曾尝试再加一例「在模型回调内部发布新代」以
// 强化在途断言，实测触发上游 v1.10.0 的 session 竞态（inmemory.SessionService
// .AppendEvent 写 vs flow 处理协程 Session.Clone 读），与本变更语义无关：生产
// 发布点只在 turn 边界（BeginTurn 之前/之后），不会在一次 run 内部换缝。该交叉
// 场景留给 §5.2 在解决会话共享边界后重建，记录于此以免重复踩。

// TestReclaimTurnTakesCurrentGeneration 是 §4.4 的机制钉：后台任务 settle 回流
// 形成的新顶层 turn 与其来源执行无关——它在**自己开始执行时**取当时的有效代。
// 回流事件与 newTaskSettledEvent 的产物同构（SourceTask 的 external_input，见
// agent/event_bus.go:239），故本例不需要真起任务即可复现同一 turn 边界。
func TestReclaimTurnTakesCurrentGeneration(t *testing.T) {
	gen1 := &requestCapturingModel{resp: gateOKResp()}
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 200)
	cm := newTestContextManager("bind-reclaim", gen1, nil, outputCh, bus)
	ta := &TagentAgent{
		persistentBus:  bus,
		activeBus:      bus,
		contextManager: cm,
		config:         &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000},
		outputCh:       outputCh,
		name:           "bind-reclaim",
	}
	var checks atomic.Int64
	cm.SetOrgReloader(func() { checks.Add(1) })

	out, err := ta.StartLoop("test-user", "reclaim-session")
	require.NoError(t, err)
	defer ta.StopLoop()
	go func() {
		for range out {
		}
	}()

	ta.InjectMessage(model.NewUserMessage("first input"))
	require.Eventually(t, func() bool { return gen1.requestCount() >= 1 }, 10*time.Second, 20*time.Millisecond,
		"the first turn must reach the model")
	require.Equal(t, int64(1), checks.Load(), "turn 1 acquires the version exactly once")

	// Publish gen2 while the loop is idle.
	gen2 := &requestCapturingModel{resp: gateOKResp()}
	face := cm.ExecutorConfig()
	face.Model = gen2
	require.NotNil(t, cm.PublishExecutor(cm.NewExecutorCandidate(face), face))

	// The reclaim turn: same event shape a settled background task publishes.
	bus.Publish(NewExternalInputEvent(SourceTask, model.Message{
		Role: model.RoleUser, Content: "[task settled] background work (id=t-1) completed → 结果: done",
	}))
	require.Eventually(t, func() bool { return gen2.requestCount() >= 1 }, 10*time.Second, 20*time.Millisecond,
		"the reclaim turn must run on the generation current at ITS OWN start")

	require.Equal(t, int64(2), checks.Load(), "the reclaim turn acquires once, like any other turn")
	require.Equal(t, 1, gen1.requestCount(),
		"the superseded generation must serve no later turn — the settle source does not pin it")
}

// TestBeginTurn_RegistersHandoffReferenceUntilReleased 是 §2.3「acquire 后立即登记」
// 的独立契约：获取版本这件事本身就必须在**交出执行器之前**登记在途引用，否则一次
// 发布 + 回收 sweep 落在「取到执行器」与「进入 run 主体」之间，就会关掉本 turn 正要
// 用的那台 runner（旧形态只在 RunFlow 内部计数，登记晚于交付，窗口真实存在）。
//
// §3.2/§4.1（D6）之后，回收是**逐代**的，因此这条契约被拆成两半各自钉住：
//   - 安全半：被这次 turn 实际引用的那代，在释放前绝不被 sweep 关掉；
//   - 独立半：与它无关、无人引用的中间代，必须立刻独立回收。
//
// 迁移记录：本例原先断言 `PendingRetirees == 2`（「一次 sweep 不得关掉任何已交出但
// 尚未运行的执行器」），那是全局聚合计数的语义——一个在途 turn 把无关代一起扣住，
// 正是 D6 与 spec「无关旧代独立回收」明确否掉的形态。原安全半仍以逐代形式保留，
// 未放宽（见 evidence 轮三十一）。
func TestBeginTurn_RegistersHandoffReferenceUntilReleased(t *testing.T) {
	cm := newTestContextManager("handoff", &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 64), nil)

	lease := cm.BeginTurnLease() // 只获取，尚未进入任何 run 主体
	require.Equal(t, int64(1), cm.ExecutorRefs().InFlightTurns,
		"acquiring must register the in-flight reference immediately, not on run entry")
	handed := lease.Generation()

	// 在途引用持有期间连发两代。第一代（mid）随后即被第二代超代且无人引用，
	// 必须被独立回收；第二代成为当前代。
	mid := publishObservedGeneration(cm)
	publishObservedGeneration(cm) // 第三代成为当前代，mid 随之退役且无人引用

	require.Equal(t, int64(1), mid.closes.Load(),
		"an unreferenced generation must be reclaimed independently of an unrelated in-flight turn")

	refs := cm.ExecutorRefs()
	require.Equal(t, 1, refs.PendingRetirees, "only the generation actually referenced stays unconverged")
	row := genRow(cm, handed)
	require.NotNil(t, row, "the handed-out generation is still on the books")
	require.True(t, row.Retired, "it has been superseded")
	require.False(t, row.Closed, "but a sweep must not close the executor that was handed out but not yet run")
	require.Equal(t, 1, row.Total, "held by exactly the reference the turn took")

	lease.Release()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 },
		10*time.Second, 20*time.Millisecond, "releasing opens this generation's own reclaim gate")
	require.Zero(t, cm.ExecutorRefs().InFlightTurns)
	require.Equal(t, int64(1), mid.closes.Load(),
		"reclaiming the referenced generation never re-closes the intermediate one")

	// 释放幂等：生产的 endTurn 可能被多条退路触达，重复释放不得把计数打成负数。
	lease.Release()
	require.Zero(t, cm.ExecutorRefs().InFlightTurns)
}

// publishObservedGeneration builds a generation with a brand-new model instance
// (identity change ⇒ a NEW generation, not a re-publish of the same face) and
// publishes it wrapped so the test can count how often OUR reclaim closes it.
func publishObservedGeneration(cm *ContextManager) *closeCounter {
	face := cm.ExecutorConfig()
	face.Model = &requestCapturingModel{resp: gateOKResp()}
	observed := &closeCounter{Runner: cm.NewExecutorCandidate(face)}
	cm.PublishExecutor(observed, face)
	return observed
}

// genRow reads one generation's diagnostic row, or nil once it has been
// reclaimed (the row disappears from both the active slot and the unconverged
// list the moment its own reference count drops).
func genRow(cm *ContextManager, id int64) *GenerationRefs {
	refs := cm.ExecutorRefs()
	for i, g := range refs.Generations {
		if g.Generation == id {
			return &refs.Generations[i]
		}
	}
	return nil
}
