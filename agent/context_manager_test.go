// 本文件负责属主回收与热参路由：一代只在自身引用全部落账后关闭、无关代独立回收、同代重发布
// 不算新代、关闭有界且持有未收敛者，以及 org 热参的路由与守卫。
// 契约: docs/wiki/agent/execution-generations.md#generation-not-fingerprint
// 契约: docs/wiki/platform/org-hot-reload.md#generations
package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
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
