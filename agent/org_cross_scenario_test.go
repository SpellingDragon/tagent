package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	"github.com/stretchr/testify/require"
)

// §5.2 交叉场景（执行器层）：并发发布 × 请求获取、回滚期间在途调用、多次连续发布后
// 旧代回收恰一次。配置形状的场景（删除最后一个工具、参数删除回落默认、候选后半段失败
// 的新增回退）在根包 org_cross_config_scenario_test.go。
//
// 本文件的并发刻意保持「一次只有一个 turn 在飞」：两个同时运行的 flow 会命中上游
// v1.10.0 的 session 并发读写竞态（台账 U-1），与本变更要验的发布/获取原子性无关，
// 混进来只会让这条门永久红。发布与获取的竞争依然成立——发布在独立 goroutine 中持续进行。

// countingModel answers deterministically and counts how often it served.
type countingModel struct {
	label string
	calls atomic.Int64
	resp  *model.Response
	gate  chan struct{} // non-nil → GenerateContent blocks until the gate opens
}

func (m *countingModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.calls.Add(1)
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

func (m *countingModel) Info() model.Info { return model.Info{Name: "counting-" + m.label} }

func crossResp(content string) *model.Response {
	return &model.Response{Done: true, Choices: []model.Choice{{
		Message: model.Message{Role: model.RoleAssistant, Content: content}}}}
}

// closeCounter observes how many times OUR sweep closes a superseded executor.
type closeCounter struct {
	runner.Runner
	closes atomic.Int64
}

func (c *closeCounter) Close() error {
	c.closes.Add(1)
	return c.Runner.Close()
}

// execModel maps a published executor to the model instance it was built with,
// recorded BEFORE publish so lookups never race with the publisher.
type execModel struct {
	mu   sync.Mutex
	byEx map[runner.Runner]*countingModel
}

func (e *execModel) put(r runner.Runner, m *countingModel) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.byEx[r] = m
}

func (e *execModel) get(r runner.Runner) (*countingModel, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m, ok := e.byEx[r]
	return m, ok
}

// TestCrossScenario_AcquiredExecutorIsWhatServesTheTurn 是「并发发布 × 请求获取」：
// 一边持续发布，一边逐 turn 获取版本并跑完。断言的不是「服务了某个合法代」，而是
// 更强的那条——**每个 turn 实际服务的模型，恰好是它自己 BeginTurn 拿到的那台执行器
// 所持有的模型**；同时引用计数必须回到零（获取/释放成对）。
func TestCrossScenario_AcquiredExecutorIsWhatServesTheTurn(t *testing.T) {
	base := &countingModel{label: "base", resp: crossResp("served:base")}
	cm := newTestContextManager("xcross", base, nil, make(chan *event.Event, 8192), nil)

	known := &execModel{byEx: map[runner.Runner]*countingModel{}}
	// Seed the identity of the boot executor, then release its hand-off reference
	// immediately — holding it would gate the sweep and defeat the convergence
	// assertion at the end of this test.
	baseExec, releaseBase := cm.BeginTurn()
	known.put(baseExec, base)
	releaseBase()

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			m := &countingModel{label: "gen", resp: crossResp("served:gen")}
			face := cm.ExecutorConfig()
			face.Model = m
			cand := cm.NewExecutorCandidate(face)
			if cand == nil {
				return
			}
			known.put(cand, m)
			cm.PublishExecutor(cand, face)
		}
	}()

	const turns = 40
	for i := 0; i < turns; i++ {
		exec, release := cm.BeginTurn()
		require.NotNil(t, exec, "turn %d must acquire an executor", i)
		served, ok := known.get(exec)
		require.True(t, ok, "turn %d acquired an executor nobody built — torn hand-out", i)

		before := served.calls.Load()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := cm.RunFlowWithExecutor(ctx, model.NewUserMessage("x"), exec)
		cancel()
		release() // 与生产同形：turn 结束才释放（endTurn 里的那一步）
		require.NoError(t, err, "turn %d", i)
		require.Greater(t, served.calls.Load(), before,
			"turn %d was served by a different model than the executor it acquired", i)
	}
	close(stop)
	<-done

	require.Eventually(t, func() bool { return cm.ExecutorRefs().InFlightTurns == 0 }, 10*time.Second, 20*time.Millisecond,
		"every acquired reference must be released")
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 20*time.Millisecond,
		"superseded executors must all be reclaimed once the turns end")
}

// TestCrossScenario_InFlightTurnSurvivesRollbackPublish 是「回滚期间在途调用」：
// turn 在 gen1 上起飞，途中先被 gen2 超代、又被回滚式重发布，该 turn 必须一路用
// 起飞时那台执行器跑完。
//
// §3.2/§4.1（D6）把「被超代的都不许回收」改成逐代判定：
//   - 这个 turn 真正引用的 gen1 在它结束前不得回收（安全半）；
//   - 与它无关、无人引用的 gen2 必须立刻独立回收，不为一个不相干的在途 turn 陪绑
//     （独立半）——旧断言 `PendingRetirees == 2` 钉的是全局聚合计数的陪绑语义，
//     spec runtime-resource-ownership「无关旧代独立回收」已否掉它（迁移记录见
//     evidence 轮三十一）。
func TestCrossScenario_InFlightTurnSurvivesRollbackPublish(t *testing.T) {
	gate := make(chan struct{})
	gen1 := &countingModel{label: "gen1", resp: crossResp("served:gen1"), gate: gate}
	cm := newTestContextManager("xrollback", gen1, nil, make(chan *event.Event, 256), nil)

	firstLease := cm.BeginTurnLease()
	first := firstLease.Runner()
	require.NotNil(t, first)
	inFlight := firstLease.Generation()
	turnErr := make(chan error, 1)
	go func() {
		turnErr <- cm.RunFlowWithExecutor(firstLease.WithContext(context.Background()), model.NewUserMessage("in flight"), first)
	}()
	require.Eventually(t, func() bool { return gen1.calls.Load() >= 1 }, 10*time.Second, 20*time.Millisecond,
		"the turn must reach the model before the publishes land")

	face2 := cm.ExecutorConfig()
	face2.Model = &countingModel{label: "gen2", resp: crossResp("served:gen2")}
	mid := &closeCounter{Runner: cm.NewExecutorCandidate(face2)}
	cm.PublishExecutor(mid, face2) // 超代
	face3 := cm.ExecutorConfig()
	face3.Model = &countingModel{label: "rollback", resp: crossResp("served:rollback")}
	cm.PublishExecutor(cm.NewExecutorCandidate(face3), face3) // 回滚式重发布（新序号）

	// 独立半：无人引用的中间代在本次发布的调用栈里就被回收，不等任何 turn。
	require.Equal(t, int64(1), mid.closes.Load(),
		"an unreferenced generation is reclaimed independently, not held hostage by an unrelated in-flight turn")
	// 安全半：被这个 turn 引用的那代仍然开着。
	row := genRow(cm, inFlight)
	require.NotNil(t, row, "the in-flight generation stays on the books until its own reference drops")
	require.False(t, row.Closed, "the executor serving the in-flight turn must not be closed mid-flight")

	refs := cm.ExecutorRefs()
	require.Equal(t, 1, refs.PendingRetirees, "only the executor the in-flight turn actually uses waits")
	require.GreaterOrEqual(t, refs.InFlightTurns, int64(1))

	close(gate)
	require.NoError(t, <-turnErr)
	firstLease.Release() // 与生产同形：turn 结束才释放本 turn 的 in-flight 引用
	require.Equal(t, int64(1), gen1.calls.Load(),
		"the in-flight turn ran on the executor it acquired at turn start — no mid-turn switch")
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 20*time.Millisecond,
		"once the turn ends, its generation is reclaimed too")
	require.Equal(t, int64(1), mid.closes.Load(), "the intermediate generation was closed exactly once in total")
}

// TestCrossScenario_EachSupersededExecutorClosesExactlyOnce 是「多次连续发布后旧代
// 回收恰一次」：连发 5 代（空闲态）后再发一台收尾，使 5 台全部被超代——每台必须被
// 我们的 sweep 关闭恰好一次：重复关是多余调用，漏关是泄漏。
func TestCrossScenario_EachSupersededExecutorClosesExactlyOnce(t *testing.T) {
	cm := newTestContextManager("xclose", &countingModel{label: "start", resp: crossResp("served:start")},
		nil, make(chan *event.Event, 1024), nil)

	wrapped := make([]*closeCounter, 0, 6)
	for i := 0; i < 5; i++ {
		face := cm.ExecutorConfig()
		face.Model = &countingModel{label: "gen", resp: crossResp("served:gen")}
		c := &closeCounter{Runner: cm.NewExecutorCandidate(face)}
		wrapped = append(wrapped, c)
		cm.PublishExecutor(c, face)
	}
	face := cm.ExecutorConfig()
	face.Model = &countingModel{label: "last", resp: crossResp("served:last")}
	cm.PublishExecutor(cm.NewExecutorCandidate(face), face) // 把第 5 台也超代

	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 20*time.Millisecond)
	for i, c := range wrapped {
		require.Equal(t, int64(1), c.closes.Load(), "superseded executor %d must be closed exactly once", i)
	}
	require.Zero(t, cm.ExecutorRefs().InFlightTurns)
}
