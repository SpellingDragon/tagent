// 本文件负责属主回收与热参路由：一代只在自身引用全部落账后关闭、无关代独立回收、同代重发布
// 不算新代、关闭有界且持有未收敛者，以及 org 热参的路由与守卫。
// 契约: docs/wiki/agent/execution-generations.md#generation-not-fingerprint
// 契约: docs/wiki/platform/org-hot-reload.md#generations
package agent

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/stretchr/testify/require"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// countingRunner is a minimal runner.Runner that counts Close calls (the upstream
// interface documents Close as idempotent, so a second call is a bug).
type countingRunner struct {
	closed atomic.Int64
}

func (f *countingRunner) Run(context.Context, string, string, model.Message, ...upagent.RunOption) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	close(ch)
	return ch, nil
}
func (f *countingRunner) Close() error {
	f.closed.Add(1)
	return nil
}

var _ runner.Runner = (*countingRunner)(nil)

func newRecycleManager() *ContextManager {
	return &ContextManager{name: "recycle-test", runner: &countingRunner{}}
}

// TestRecycle_GenerationClosesOnlyAfterItsOwnReferencesDrop 钉住 每代各自排空：钉在某一代的租约只让该代保持打开，释放后回收的也只有它。
func TestRecycle_GenerationClosesOnlyAfterItsOwnReferencesDrop(t *testing.T) {
	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)

	lease := cm.AcquireLease(LeaseTurn)
	require.Same(t, g1, lease.Runner(), "the lease pins the generation in force at acquire")

	g2 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig())
	require.Zero(t, g1.closed.Load(), "a retired generation with a live reference must not be closed")

	lease.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "the last reference dropping reclaims exactly that generation")
	require.Zero(t, g2.closed.Load(), "the generation now in force is never reclaimed by a release")

	lease.Release()
	require.Equal(t, int64(1), g1.closed.Load())
	require.Zero(t, cm.ExecutorRefs().PendingRetirees, "a reclaimed generation leaves the unconverged list (bounded bookkeeping)")
}

// TestRecycle_UnrelatedGenerationReclaimsIndependently 钉住 is D6's「无关旧代独立回收」 at the mechanism level: holding G1 open must not keep an unreferenced G2 alive.
func TestRecycle_UnrelatedGenerationReclaimsIndependently(t *testing.T) {
	cm := newRecycleManager()
	held := cm.AcquireLease(LeaseTurn)
	defer held.Release()

	g2 := &countingRunner{}
	g3 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig())
	cm.PublishExecutor(g3, cm.ExecutorConfig())

	require.Equal(t, int64(1), g2.closed.Load(), "G2 has no references — it is reclaimed while G1 still runs")
	require.Zero(t, g3.closed.Load(), "the current generation stays open")
}

// TestContextManager_Close_IsBoundedAndHoldsUnconverged 钉住 公开关闭有界：已收敛的每一件资源都恰好关闭一次。
// - 生产者从未确认停止的那次执行必须显式保留并上报，绝不为凑过计数而强关；
// - 屏障之后被解除时，它也只被释放一次。
func TestContextManager_Close_IsBoundedAndHoldsUnconverged(t *testing.T) {
	grace := execCloseGrace
	execCloseGrace = 100 * time.Millisecond
	defer func() { execCloseGrace = grace }()

	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)
	held := cm.AcquireLease(LeaseTurn)
	g2 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig())

	unconverged := cm.UnconvergedRefs()
	require.Len(t, unconverged, 1, "the still-held retired generation is nameable before the drain")
	require.Equal(t, "recycle-test", unconverged[0].Owner, "the report says WHICH owner is stuck")
	require.Equal(t, 1, unconverged[0].Refs[LeaseTurn.String()], "and for WHAT it is still held")

	err := cm.Close()
	require.ErrorIs(t, err, ErrExecUnconverged, "a stuck execution is never reported as a clean close")
	require.Equal(t, int64(1), g2.closed.Load(), "the unreferenced current generation converged and closed once")
	require.Zero(t, g1.closed.Load(), "the generation with a live producer is HELD, not force-closed")
	require.Len(t, cm.UnconvergedRefs(), 1, "and it stays on the books afterwards — the report is readable, not consumed")

	held.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "the release after the barrier closes it exactly once")
	require.Empty(t, cm.UnconvergedRefs(), "and it leaves the unconverged list")
	held.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "a repeated release stays inert")
}

// TestRecycle_SameExecutorRepublishedIsNotANewGeneration 钉住 唯一身份规则：重新发布当前已在跑的 runner 不得另起一代。
// - 回滚落回当前生效面、冗余候选都属此类；另起一代意味着同一 runner 被关闭两次。
func TestRecycle_SameExecutorRepublishedIsNotANewGeneration(t *testing.T) {
	cm := newRecycleManager()
	_, release := cm.BeginTurn()
	defer release()

	current := cm.currentRunner()
	face := cm.ExecutorConfig()
	require.Same(t, current, cm.PublishExecutor(current, face),
		"republishing the in-force executor returns the executor that stays in force")

	before := cm.ExecutorRefs().PendingRetirees
	require.Same(t, current, cm.PublishExecutor(current, face))
	require.Equal(t, before, cm.ExecutorRefs().PendingRetirees,
		"a redundant publish of the same executor creates no extra generation to reclaim")
	require.Zero(t, current.(*countingRunner).closed.Load(),
		"and never closes the executor that is still in force")
}

// TestRecycle_RaceStress 钉住 concurrent acquire/release, publication and terminal Close — the lock discipline must hold and no runner may ever be closed twice.
func TestRecycle_RaceStress(t *testing.T) {
	cm := newRecycleManager()
	const gens = 50
	runners := make([]*countingRunner, gens)
	for i := range runners {
		runners[i] = &countingRunner{}
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			lease := cm.AcquireLease(LeaseTurn)
			child := lease.Derive(LeaseSubCall)
			lease.Release()
			child.Release()
		}
	}()

	for i := 0; i < gens; i++ {
		cm.PublishExecutor(runners[i], cm.ExecutorConfig())
	}

	close(stop)
	wg.Wait()
	require.NoError(t, cm.Close())

	for i, r := range runners {
		require.LessOrEqual(t, r.closed.Load(), int64(1), "generation %d closed more than once", i)
	}
}

// rotateHot installs a mutable  pull source on cm's compressor — the test
// stand-in for what production does by rotating the committed record
// (cm.ApplyOrgHotParams / the compressor push face are deleted). Nil-safe, so a
// compressor-less manager stays a guarded no-op.
func rotateHot(cm *ContextManager, cur *OrgHotParams) {
	if cm == nil || cm.contextCompressor == nil {
		return
	}
	cm.contextCompressor.SetHotSource(func() compress.HotNumbers {
		return compress.HotNumbers{
			ThresholdPct: cur.ThresholdPct,
			MaxTokens:    cur.MaxTokens,
			KeepRecent:   cur.KeepRecentTasks,
		}
	})
}

func TestApplyOrgHotParams_RoutesAndGuards(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, cc := rbFoldCM(store, proj, 2)

	cur := &OrgHotParams{ThresholdPct: 0.9}
	rotateHot(cm, cur)
	require.Equal(t, 0.9, cc.Threshold(), "threshold must resolve from the hot source")

	*cur = OrgHotParams{}
	require.Equal(t, 0.8, cc.Threshold(), "a zero reading falls back to construction, it must not clobber to zero")
}

func TestApplyOrgHotParams_BudgetLineMoves(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cmA := driveRealFold(t, store, proj, 2)
	ccA := cmA.contextCompressor
	cm := &ContextManager{partitionID: rbPid, memStore: store, projection: proj, contextCompressor: ccA}

	line := func() int { return ccA.BudgetLine() }
	require.Equal(t, 48, line(), "rbFoldCM construction line")

	rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: 16000, KeepRecentTasks: 5})

	require.Equal(t, 12800, line(), "source rotation must move the budget line to 16000×0.8")
}

// TestOutputCapIsConstructionDerivedBoundary 钉住 工具输出上限由构造期的最大令牌数派生，且刻意不属于那五条热轴。
// - 把所有派生数字折进热面与指纹，正是判据所排除的做法；
// - 派生结果既封顶又设底，而一个限流工具一旦建成，其上限即告冻结。
func TestOutputCapIsConstructionDerivedBoundary(t *testing.T) {
	require.Equal(t, 8000, outputCapForMaxTokens(4000))
	require.Equal(t, 20000, outputCapForMaxTokens(10000))
	require.Equal(t, toolOutputCapChars, outputCapForMaxTokens(128000))
	require.Equal(t, toolOutputCapChars, outputCapForMaxTokens(0))
	require.Equal(t, toolOutputCapChars, outputCapForMaxTokens(-5))

	olt := NewOutputLimitTool(leafTool{name: "x"}, outputCapForMaxTokens(4000))
	cm := &ContextManager{}
	rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: 100000})
	require.Equal(t, 8000, olt.maxChars, "a hot max_tokens change must NOT resize an already-built tool's derived output cap")
}

// TestResidentBudgetHotAppliesToRealConsumer 钉住 热更确实作用于真实消费者：max_tokens 落到压缩器侧的有效预算线并随热更移动，与上方冻结的输出上限互补；CM 侧不留第二处预算来源。
func TestResidentBudgetHotAppliesToRealConsumer(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, cc := rbFoldCM(store, proj, 2)
	require.Equal(t, 48, cc.BudgetLine(), "constructed from the config source at build (new-call-effective-at-init)")

	rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: 16000, KeepRecentTasks: 5})
	require.Equal(t, 12800, cc.BudgetLine(), "source rotation reaches the compressor (authoritative source), no separate cm field")
}

// noopLeafTool is a non-delegation tool so the gated org needs no sub-agents.
type noopLeafTool struct{}

func (noopLeafTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "noop", Description: "noop"}
}
func (noopLeafTool) Call(_ context.Context, _ []byte) (any, error) { return "ok", nil }

// gatedSequenceModel emits a canned assistant turn whose SECOND model call
// parks on a test gate. The park point is inside the invocation's RunFlow —
// i.e. inside the window where the invocation-private CM is registered live.
type gatedSequenceModel struct {
	mu      sync.Mutex
	idx     int
	gate    chan struct{}
	entered chan struct{}
	once    sync.Once
}

func scriptedToolCallResp() *model.Response {
	return &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant,
		ToolCalls: []model.ToolCall{{ID: "tc-1", Function: model.FunctionDefinitionParam{
			Name: "noop", Arguments: []byte(`{}`),
		}}},
	}}}}
}

func scriptedFinalResp() *model.Response {
	return &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("done")}}}
}

func (m *gatedSequenceModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.idx
	m.idx++
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	switch idx {
	case 0:
		ch <- scriptedToolCallResp()
		close(ch)
		return ch, nil
	case 1:
		m.once.Do(func() { close(m.entered) })
		select {
		case <-m.gate:
		case <-ctx.Done():
			close(ch)
			return ch, ctx.Err()
		}
		ch <- scriptedFinalResp()
		close(ch)
		return ch, nil
	default:
		ch <- scriptedFinalResp()
		close(ch)
		return ch, nil
	}
}

func (m *gatedSequenceModel) Info() model.Info { return model.Info{Name: "gated"} }

func newGatedOrg(t *testing.T) (*TagentAgent, *gatedSequenceModel) {
	t.Helper()
	g := &gatedSequenceModel{gate: make(chan struct{}), entered: make(chan struct{})}
	ta, err := NewTagentAgent(&TagentConfig{
		Model:             g,
		Name:              "hotthread",
		SystemPrompt:      "sp",
		MaxToolIterations: 5,
		MaxTokens:         4000,
		CompressThreshold: 0.5,
		KeepRecentTasks:   2,
		Tools:             []trpctool.Tool{noopLeafTool{}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ta.Close() })
	return ta, g
}

// TestHotParamSnapshotSeededAtConstruction 钉住 构造期的种子快照：每个装配好的 agent 携带的快照等于其解析出的值。
// - 这样种子与常驻压缩器从第一刻起就共享同一代。
func TestHotParamSnapshotSeededAtConstruction(t *testing.T) {
	ta, _ := newGatedOrg(t)
	p, ok := ta.HotSnapshot()
	require.True(t, ok, "construction must seed the snapshot")
	require.Equal(t, 4000, p.MaxTokens)
	require.InDelta(t, 0.5, p.ThresholdPct, 1e-9)
	require.Equal(t, 2, p.KeepRecentTasks)
	require.Equal(t, 2000, ta.OrgBudgetLine(), "resident and snapshot agree at start")
	require.Zero(t, ta.LiveCMCount(), "no calls yet")
}

// TestFreshSubCallSeededFromSnapshot 钉住 只做数值项应用之后，新建的调用私有上下文管理器必须从生效值起步。
// - 缺陷形态是它停在构造期冻结的数值上，而不是当代生效值。
func TestFreshSubCallSeededFromSnapshot(t *testing.T) {
	ta, g := newGatedOrg(t)
	ta.SetHotSource(staticHotSource(OrgHotParams{ThresholdPct: 0.9, MaxTokens: 9000, KeepRecentTasks: 7}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inv := upagent.NewInvocation(upagent.WithInvocationMessage(model.NewUserMessage("go")))
	out, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the gated model call never began — harness broken")
	}
	require.Equal(t, 1, ta.LiveCMCount(), "exactly one live private CM while the call is in flight")

	live := ta.snapshotLiveCMs()[0]
	require.Equal(t, 8100, live.OrgBudgetLine(),
		"a fresh sub-call must initialize from the effective snapshot, not construction config")
	require.Equal(t, 7, live.OrgKeepRecent())

	close(g.gate)
	for range out {
	}
	require.Zero(t, ta.LiveCMCount(), "registration must be reclaimed when the call ends")
}

// TestInFlightSubCallAppliesAtBoundary 钉住 热应用发生时已在跑的调用，要在它的下一个消费边界取到新值（经活动注册表扇出）。
// - 注册表规模由并发调用数封顶，不随调用次数累计增长。
func TestInFlightSubCallAppliesAtBoundary(t *testing.T) {
	ta, g := newGatedOrg(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inv := upagent.NewInvocation(upagent.WithInvocationMessage(model.NewUserMessage("go")))
	out, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the gated model call never began — harness broken")
	}
	live := ta.snapshotLiveCMs()[0]
	require.Equal(t, 2000, live.OrgBudgetLine(), "in-flight call starts at its seeded generation")

	ta.SetHotSource(staticHotSource(OrgHotParams{ThresholdPct: 0.9, MaxTokens: 9000, KeepRecentTasks: 7}))
	require.Equal(t, 8100, live.OrgBudgetLine(),
		"in-flight private CM must take the hot update at its next budget/compress read")
	require.Equal(t, 7, live.OrgKeepRecent())

	close(g.gate)
	for range out {
	}
	require.Zero(t, ta.LiveCMCount())
}

// TestBackgroundHotApplyConcurrentWithCompressNoRace 钉住 与压缩并发轮换热参源时，每一趟读取都取到同一代的一致数值。
// - 读侧在并发应用下必须无竞态；数值组按整组一次读全，不逐字段取。
// 契约: docs/wiki/agent/compression-and-telemetry.md#hot-bundle-atomicity
func TestBackgroundHotApplyConcurrentWithCompressNoRace(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, cc := rbFoldCM(store, proj, 2)
	for _, ref := range rbAgedRefs(t, store, rbNowMs()+60_000_000) {
		proj.Append(ref)
	}
	snapshot := proj.GetAll()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			keep, max := 2, 60
			if i%2 == 0 {
				keep, max = 4, 16000
			}
			rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: max, KeepRecentTasks: keep})
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = cc.Compress(context.Background(), snapshot)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestAssembleRequest_PeeksDynamicOverhead 钉住装配期只读不消费本回合将注入的动态开销。
//   - pending recovery notice 只 peek，真正发起调用时才由门禁消费
//   - 看板每请求只渲染一次：预算与注入共用同一份文本，不会出现两次渲染漂移
func TestAssembleRequest_PeeksDynamicOverhead(t *testing.T) {
	t.Run("the pending notice can be what overflows the limit", func(t *testing.T) {
		cm := finalBudgetCM(t)
		long := "[recovery] " + repeatBudgetRunes('n', 300)
		setPendingNotice(cm, long)
		inner := &requestCapturingModel{resp: gateOKResp()}
		g := newExecutionGateModel(inner, cm)

		args := finalBudgetUnderArgs()
		cm.assembleRequest(context.Background(), args)

		_, err := g.GenerateContent(context.Background(), args.Request)
		require.Error(t, err, "本回合要注入的恢复通告是真实成本，不得当零")
		require.Contains(t, err.Error(), "budget_exceeded", "超限即具名拒发，got %v", err)

		require.Equal(t, long, pendingNotice(cm), "peek 不消费：拒发前通告仍在原位")
		require.Zero(t, inner.requestCount())
		require.Equal(t, long, cm.TakeRecoveryNotice(), "同一次调用真正发起时，通告仍可用")
	})

	t.Run("the live board is real overhead and renders exactly once", func(t *testing.T) {
		cm := finalBudgetCM(t)
		board := &countingBoardController{fakeTaskController: &fakeTaskController{
			tasks: []*task.Task{mkDigestTask("boardaaa11", repeatBudgetRunes('务', 200), task.TaskRunning, time.Minute)},
		}}
		cm.taskController = board
		inner := &requestCapturingModel{resp: gateOKResp()}
		g := newExecutionGateModel(inner, cm)

		args := finalBudgetUnderArgs()
		cm.assembleRequest(context.Background(), args)
		cm.injectLiveTaskBoard(args)

		_, err := g.GenerateContent(context.Background(), args.Request)
		require.Error(t, err, "看板随任务年龄每回合变化，是本回合的真实固定开销，不得当零")
		require.Contains(t, err.Error(), "budget_exceeded", "超限即具名拒发，got %v", err)
		require.Zero(t, inner.requestCount(), "拒发不落 provider")

		require.EqualValues(t, 1, board.listCalls.Load(),
			"预算读一次、尾部注入一次 = 同一份渲染；看板字节随任务年龄变化，禁止二次渲染")
		var last string
		for _, m := range args.Request.Messages {
			last = m.Content
		}
		require.Contains(t, last, "[后台任务看板]", "注入位置与内容不变（尾部）")
	})

	t.Run("no board, no notice: nothing is invented for the budget", func(t *testing.T) {
		cm := finalBudgetCM(t)
		inner := &requestCapturingModel{resp: gateOKResp()}
		g := newExecutionGateModel(inner, cm)
		args := finalBudgetUnderArgs()
		cm.assembleRequest(context.Background(), args)
		_, err := g.GenerateContent(context.Background(), args.Request)
		require.NoError(t, err, "缺项不造：没有动态开销时不该凭空拒发")
		require.Equal(t, 1, inner.requestCount())
	})
}

// countingBoardController 记录 List() 次数，从而能观察「每请求渲染几次」这一事实。
type countingBoardController struct {
	*fakeTaskController
	listCalls atomic.Int64
}

func (c *countingBoardController) List() []*task.Task {
	c.listCalls.Add(1)
	return c.fakeTaskController.List()
}

// capTestCM 直接经 NewContextManager 构造，使 ContextManagerConfig 的新字段（摘要时限秒数、
// 采集开关）走的就是生产那条构造路径。
func capTestCM(t *testing.T, name string, summarySeconds int, capture bool) *ContextManager {
	t.Helper()
	sc := compress.NewSmartCompressor(compress.WithMaxTokens(8000), compress.WithTokenCounter(&mockTokenCounter{tokens: 100}))
	store := memory.NewInMemoryStore()
	return NewContextManager(ContextManagerConfig{
		Name:                  name,
		UserID:                "test-user",
		SessionID:             "test-session",
		Model:                 &loopMockModel{},
		MaxToolIters:          10,
		Compressor:            sc,
		TokenCounter:          &mockTokenCounter{tokens: 100},
		MaxTokens:             8000,
		ThresholdPct:          0.8,
		MemStore:              store,
		SessionSvc:            sessioninmemory.NewSessionService(),
		Projection:            compress.NewSessionProjection(),
		SummaryTimeoutSeconds: summarySeconds,
		CaptureEnabled:        capture,
	})
}

// TestNewContextManager_SummaryTimeout 钉住配置以「秒」进入装配、以 Duration 落到压缩器。
//   - ≤0 一律走 compress 包默认：0 不等于「无时限」
func TestNewContextManager_SummaryTimeout(t *testing.T) {
	t.Run("positive seconds become a duration", func(t *testing.T) {
		cm := capTestCM(t, "sum-7s", 7, false)
		require.Equal(t, 7*time.Second, cm.contextCompressor.SummaryTimeout())
	})
	t.Run("zero keeps the package default", func(t *testing.T) {
		cm := capTestCM(t, "sum-0", 0, false)
		require.Equal(t, compress.DefaultSummaryTimeout, cm.contextCompressor.SummaryTimeout())
	})
	t.Run("negative keeps the package default (validation is the config layer's job)", func(t *testing.T) {
		cm := capTestCM(t, "sum-neg", -3, false)
		require.Equal(t, compress.DefaultSummaryTimeout, cm.contextCompressor.SummaryTimeout())
	})
}

// ctxCapturingRunner 记下它被调用时收到的 ctx，用于观察一次尝试实际携带的采集 scope。
type ctxCapturingRunner struct {
	mu   sync.Mutex
	seen context.Context
}

func (r *ctxCapturingRunner) Run(ctx context.Context, _, _ string, _ model.Message, _ ...upagent.RunOption) (<-chan *event.Event, error) {
	r.mu.Lock()
	r.seen = ctx
	r.mu.Unlock()
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (r *ctxCapturingRunner) Close() error { return nil }

func (r *ctxCapturingRunner) ctx() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen
}

// TestRunFlow_CaptureScope 钉住采集 scope 是「每尝试一个有界关联对象」的运行时装配。
//   - 关闭时 ctx 上什么都不装（零开销）；开启时装上本次 invocation 的 scope
//   - 归属来自 cm 现值与本回合归因，缺项不造；尝试结束即释放——它不是第二套持久化系统
func TestRunFlow_CaptureScope(t *testing.T) {
	t.Run("disabled installs nothing", func(t *testing.T) {
		cm := capTestCM(t, "cap-off", 0, false)
		r := &ctxCapturingRunner{}
		ctx := withInvocationID(context.Background(), "inv-off")
		require.NoError(t, cm.RunFlowWithExecutor(ctx, model.NewUserMessage("hi"), r))
		_, ok := rl.CaptureScopeFrom(r.ctx())
		require.False(t, ok, "trajectory_capture 关闭时不得在 ctx 上装任何东西")
	})

	t.Run("enabled installs the attempt scope and releases it", func(t *testing.T) {
		cm := capTestCM(t, "cap-on", 0, true)
		cm.SetTriggerSource("user")
		cm.turnEcho = &echoSpec{
			agent: "cap-on", session: "test-session", mergedMessage: "hi",
			committedKeys: []int64{111, 222},
		}
		r := &ctxCapturingRunner{}
		ctx := withInvocationID(context.Background(), "inv-42")
		require.NoError(t, cm.RunFlowWithExecutor(ctx, model.NewUserMessage("hi"), r))

		scope, ok := rl.CaptureScopeFrom(r.ctx())
		require.True(t, ok, "the model call chain must carry the scope")
		require.Equal(t, "inv-42", scope.InvocationID)

		wantNS := strconv.Itoa(memory.PartitionIDFromName("cap-on"))
		require.Equal(t, wantNS, scope.Owner["capture_namespace"], "分区数字串即采集命名空间")
		require.Equal(t, "cap-on", scope.Owner["agent_name"])
		require.Equal(t, "test-session", scope.Owner["root_session_id"])
		require.Equal(t, "test-session", scope.Owner["session_id"])
		require.Equal(t, "test-user", scope.Owner["user_id"])
		require.Equal(t, "inv-42", scope.Owner["invocation_id"])
		require.Equal(t, "user", scope.Owner["trigger_source"])
		require.Equal(t, "111,222", scope.Owner["input_event_keys"], "本批 committed keys 原样带上，不猜不补")
		require.NotContains(t, scope.Owner, "bundle_id", "缺项不造：没有身份就不写这个键")

		require.True(t, scope.Stats().Released, "attempt 结束即释放 scope（不是回合级、不是全局）")
	})

	t.Run("a turn without a delegation id still gets its own scope", func(t *testing.T) {
		cm := capTestCM(t, "cap-noid", 0, true)
		r := &ctxCapturingRunner{}
		require.NoError(t, cm.RunFlowWithExecutor(context.Background(), model.NewUserMessage("hi"), r))
		scope, ok := rl.CaptureScopeFrom(r.ctx())
		require.True(t, ok)
		require.Empty(t, scope.InvocationID, "没有委派身份就不造一个，关联侧自行具名 unbound")
		require.NotContains(t, scope.Owner, "invocation_id")
		require.NotContains(t, scope.Owner, "input_event_keys", "无 durable 输入即无 keys，不写空串键")
	})
}

// TestCallIDResolverBridge 钉住装配根的默认解析桥只答本次调用 ctx 上所装 scope 的精确键。
//   - 采集关闭（ctx 上没有 scope）或该响应身份没有条目时一律 false
//   - 写入方因此不盖 call_id，绝不拿「最近一次调用」凑一个；显式注入的解析器优先
func TestCallIDResolverBridge(t *testing.T) {
	ctx := context.Background()

	called := 0
	own := func(context.Context, string) (string, bool) {
		called++
		return "explicit", true
	}
	id, ok := callIDResolverFor(&TagentConfig{CallIDResolver: own})(ctx, "resp-1")
	require.True(t, ok, "配置显式给出的解析器优先于默认桥")
	require.Equal(t, "explicit", id)
	require.Equal(t, 1, called)

	id, ok = callIDResolverFor(nil)(ctx, "resp-1")
	require.False(t, ok, "trajectory_capture 关闭时 ctx 上没有 scope，解析器必须答否")
	require.Empty(t, id)
	id, ok = callIDResolverFor(&TagentConfig{})(ctx, "")
	require.False(t, ok, "空身份连查询都不该有结果")
	require.Empty(t, id)

	scope := rl.NewCaptureScope("inv-bridge")
	_, res := scope.Link(rl.ResponseKey("resp-1"), "call-9")
	require.Equal(t, rl.LinkStored, res, "首次落表不回显 call_id（回显属重放/冲突路径），命中判定看精确查找")

	scoped := rl.WithCaptureScope(ctx, scope)
	id, ok = callIDResolverFor(nil)(scoped, "resp-1")
	require.True(t, ok, "本次调用的响应身份在 scope 里有精确条目")
	require.Equal(t, "call-9", id)

	id, ok = callIDResolverFor(nil)(scoped, "resp-unbound")
	require.False(t, ok, "S2: 未命中必须具名 unbound，绝不取最近的 call_id")
	require.Empty(t, id)
}
