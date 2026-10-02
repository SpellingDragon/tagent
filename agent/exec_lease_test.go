// 本文件负责执行器代际与租约引用：新代按序号而非内容指纹认定、已收敛代必须拒绝新引用且不
// 静默改投、已退役但仍被持有的代仍可承接子调用、纯构造不得触碰在线执行器。
// 契约: docs/wiki/agent/execution-generations.md#lease-holds-reference
// 契约: docs/wiki/agent/execution-generations.md#generation-not-fingerprint
package agent

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestExecLease_RetiredGenerationRefusesNewReference(t *testing.T) {
	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)

	first := cm.AcquireLease(LeaseTurn)
	require.Same(t, g1, first.Runner(), "precondition: the turn pinned the generation in force")
	idle := cm.activeBinding()
	idleGen := idle.id
	first.Release()

	g2 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig())
	require.True(t, idle.retired, "precondition: the superseded generation is retired")
	require.Equal(t, int64(1), g1.closed.Load(), "precondition: being idle, it was reclaimed at once")

	if _, ok := idle.tryAcquireActive(LeaseTurn); ok {
		t.Fatal("a retired generation accepted a NEW reference — D6 is unenforced, and the caller would run on the closed runner above")
	}
	require.Zero(t, idle.total, "the refusal must not leave a reference behind")

	fresh := cm.AcquireLease(LeaseTurn)
	require.Same(t, g2, fresh.Runner(), "the acquire path re-pins the generation actually in force")
	require.NotEqual(t, idleGen, fresh.Generation(), "and never the superseded one")
	fresh.Release()
	require.Zero(t, g2.closed.Load(), "releasing a held generation must not close the one in force")
}

// TestExecLease_DeriveStillLandsOnRetiredButHeldGeneration 钉住 钉住 从"已被取代但仍被引用"的执行派生子调用仍然合法，且在最后一个持有者离开前不得回收该代。
func TestExecLease_DeriveStillLandsOnRetiredButHeldGeneration(t *testing.T) {
	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)

	parent := cm.AcquireLease(LeaseTurn)
	held := cm.activeBinding()

	cm.PublishExecutor(&countingRunner{}, cm.ExecutorConfig())
	require.True(t, held.retired, "precondition: superseded while still referenced")
	require.False(t, held.closed, "a generation with outstanding references is never force-closed")
	require.Equal(t, int64(0), g1.closed.Load())

	child := parent.Derive(LeaseSubCall)
	require.Equal(t, held.id, child.Generation(), "the derived reference rides the SAME generation")
	require.Equal(t, 2, held.total)

	child.Release()
	require.Equal(t, 1, held.total)
	require.Equal(t, int64(0), g1.closed.Load(), "not before the last holder is gone")

	parent.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "the last release reclaims it exactly once")
}

// lastRequestToolNames returns the tool names carried by the most recent
// request the model actually received.
func lastRequestToolNames(m *requestCapturingModel) []string {
	reqs := m.snapshotRequests()
	if len(reqs) == 0 {
		return nil
	}
	var names []string
	for _, tl := range reqs[len(reqs)-1].Tools {
		if tl == nil {
			continue
		}
		if d := tl.Declaration(); d != nil {
			names = append(names, d.Name)
		}
	}
	return names
}

// TestExecutorPublish_ClearedToolBindingDoesNotSurvive 钉住 去掉最后一个 agent 工具后，发布的那一代不得继续声明它——候选不得回落到已发布的那份 Tools 切片。
func TestExecutorPublish_ClearedToolBindingDoesNotSurvive(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("pub-clear-tool", capture,
		[]trpctool.Tool{&pinEchoTool{name: "gen1_only_tool"}},
		make(chan *event.Event, 16), nil)

	live := cm.currentRunner()
	out, err := live.Run(context.Background(), "u1", "s1", model.NewUserMessage("first"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)
	require.Equal(t, []string{"gen1_only_tool"}, lastRequestToolNames(capture),
		"gen1 must advertise the tool the config declared")

	face := cm.ExecutorConfig()
	require.NotNil(t, face.Model, "the published face carries the model in force")
	require.Len(t, face.Tools, 1, "precondition: gen1 face has exactly one tool")
	face.Tools = nil

	cand := cm.NewExecutorCandidate(face)
	require.NotNil(t, cand)
	require.Same(t, live, cm.currentRunner(),
		"the effective runner must be untouched until publish")

	published := cm.PublishExecutor(cand, face)
	require.NotNil(t, published)

	out2, err := published.Run(context.Background(), "u1", "s1", model.NewUserMessage("second"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	require.Empty(t, lastRequestToolNames(capture),
		"gen2 must declare NO tool: the deleted tool must not survive into the new generation")

	require.Empty(t, cm.ExecutorConfig().Tools, "published snapshot must not keep the stale tool")
}

// TestExecutorCandidate_ConstructionDoesNotTouchLiveExecutor 钉住 拆分体的另一半：候选构造可被丢弃（发布前失败即关闭），只有发布动作才切换接缝并更新生效面。
// - 构造期间绝不触碰在线执行器。
func TestExecutorCandidate_ConstructionDoesNotTouchLiveExecutor(t *testing.T) {
	live := &fakeRunner{id: "gen-live"}
	cm := &ContextManager{runner: live, execCfg: ContextManagerConfig{Name: "cand-only"}}

	cand := cm.NewExecutorCandidate(ContextManagerConfig{Name: "cand-only", Model: &requestCapturingModel{}})
	require.NotNil(t, cand)
	require.Same(t, live, cm.currentRunner(),
		"candidate construction must leave the effective runner untouched")
	require.Equal(t, "cand-only", cm.ExecutorConfig().Name,
		"the effective face must not advance before publish")
	require.Zero(t, cm.ExecutorRefs().PendingRetirees,
		"constructing a candidate must not retire — let alone close — the live runner")

	published := cm.PublishExecutor(cand, ContextManagerConfig{Name: "gen-published"})
	require.NotNil(t, published)
	require.NotSame(t, live, cm.currentRunner(), "publish moves the seam")
	require.Equal(t, "gen-published", cm.ExecutorConfig().Name, "publish records the face")

	again := cm.PublishExecutor(nil, ContextManagerConfig{Name: "must-not-land"})
	require.NotNil(t, again)
	require.Equal(t, "gen-published", cm.ExecutorConfig().Name)
}

// TestExecutorPublish_RepublishedSameFaceKeepsBehavior 钉住 重新发布是显式交出一张要生效的面，而不是"传零配置让旧绑定自己回来"。
// - 常驻状态必须继续流经新执行器：既不重新挂载，也不重复。
func TestExecutorPublish_RepublishedSameFaceKeepsBehavior(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("pub-same-face", capture, nil, make(chan *event.Event, 16), nil)

	face := cm.ExecutorConfig()
	r2 := cm.PublishExecutor(cm.NewExecutorCandidate(face), face)
	require.NotNil(t, r2)
	setNotice(cm, "[recovery] survives an explicit republish")

	out, err := r2.Run(context.Background(), "test-user", "s-2", model.NewUserMessage("after publish"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)

	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs)
	require.Equal(t, 1, countCarried(reqs), "the republished executor consumes the resident notice exactly once")
	require.Empty(t, peekNotice(cm))
}

// TestExecutorRefs_RevealsInFlightTurnAndUnconvergedRetiree 钉住 钉 （D9）的最后一腿 ——「退役引用与未收敛 owner」必须可从既有诊断面读到，而不是只活在日志里。
func TestExecutorRefs_RevealsInFlightTurnAndUnconvergedRetiree(t *testing.T) {
	blocking := &requestCapturingModel{}
	cm := newTestContextManager("refs", blocking, nil, make(chan *event.Event, 64), nil)

	idle := cm.ExecutorRefs()
	require.Zero(t, idle.InFlightTurns, "an idle context manager holds no turn references")
	require.Zero(t, idle.PendingRetirees, "and no unreclaimed retirees")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- cm.RunFlowWithExecutor(ctx, model.NewUserMessage("hold the reference"), nil)
	}()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().InFlightTurns >= 1 }, 10*time.Second, 20*time.Millisecond,
		"an in-flight turn must hold a runner reference")

	face := cm.ExecutorConfig()
	face.Model = &requestCapturingModel{resp: gateOKResp()}
	cm.PublishExecutor(cm.NewExecutorCandidate(face), face)

	refs := cm.ExecutorRefs()
	require.Equal(t, 1, refs.PendingRetirees, "the superseded runner is an unconverged retiree")
	require.Greater(t, refs.InFlightTurns, int64(0), "it is held back by the turn still running on it")
	require.Greater(t, refs.OldestPending, time.Duration(0), "its wait age is reported")
	require.Equal(t, retiredLeakAfter, refs.LeakThreshold)

	cancel()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 20*time.Millisecond,
		"once the last turn holding it ends, the sweep reclaims the retiree")
	_ = <-done
}

// TestPublishBothEntriesShareOneLinearization 钉住 两个发布入口——单属主发布与暂存代激活——必须共用同一切换主体。
// - 漂移点只会落在一处：重新发布当前已在跑的那一个时，一份推进记录中的生效面、另一份静默跳过；
// - 因此两条入口对同一情形的观察结果必须一致。
// 契约: docs/wiki/agent/execution-generations.md#single-linearization-body
func TestPublishBothEntriesShareOneLinearization(t *testing.T) {
	entries := []struct {
		name    string
		publish func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner
	}{
		{"PublishExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.PublishExecutor(r, face)
		}},
		{"ActivateExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.ActivateExecutor(cm.StageExecutor(r, face, nil))
		}},
	}
	for _, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			cm := newTestContextManager("one-linearization", &requestCapturingModel{resp: gateOKResp()},
				nil, make(chan *event.Event, 16), nil)
			defer func() { _ = cm.Close() }()

			face := cm.ExecutorConfig()
			face.Name = "one-linearization"
			first := cm.NewExecutorCandidate(face)
			require.NotNil(t, first)
			e.publish(cm, first, face)
			live := cm.currentRunner()
			require.Same(t, first, live, "precondition: the entry installed a new generation")

			adv := cm.ExecutorConfig()
			adv.SystemPrompt = "recorded-face-advanced"
			got := e.publish(cm, live, adv)

			require.Same(t, live, got, "a same-runner re-publish must keep the one identity (no second generation)")
			require.Same(t, live, cm.currentRunner(), "and must not swap what the next turn runs")
			require.Equal(t, "recorded-face-advanced", cm.ExecutorConfig().SystemPrompt,
				"§5.3 merge: both entries must agree that the RECORDED face advances")
			require.Zero(t, cm.ExecutorRefs().PendingRetirees,
				"nothing may be retired behind a re-publish of the runner still in force")
		})
	}
}

// closeCountRunner records how often its Close ran, so a retired-while-in-use
// executor becomes an observable fact instead of an assumption.
type closeCountRunner struct {
	fakeRunner
	closes *int32
}

func (c *closeCountRunner) Close() error {
	atomic.AddInt32(c.closes, 1)
	return nil
}

// TestPublishFirstGenerationOfInitialRunnerNeverClosesIt 钉住 发布冷启动时构造所用的那个 runner，是它的第一代：收编即装配。
// - 为同一对象造一个前驱绑定，会把下一回合正要运行的执行器退役并关闭；
// - 因此两条入口都必须让这个在跑的 runner 保持未被退役、仍在生效。
func TestPublishFirstGenerationOfInitialRunnerNeverClosesIt(t *testing.T) {
	type entry struct {
		name    string
		publish func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner
	}
	entries := []entry{
		{"PublishExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.PublishExecutor(r, face)
		}},
		{"ActivateExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.ActivateExecutor(cm.StageExecutor(r, face, nil))
		}},
	}
	for _, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			var closes int32
			initial := &closeCountRunner{fakeRunner: fakeRunner{id: "initial"}, closes: &closes}
			cm := &ContextManager{runner: initial, name: "first-gen"}
			require.Nil(t, cm.active, "precondition: a runner is in force with no generation installed")

			face := cm.ExecutorConfig()
			got := e.publish(cm, initial, face)

			require.Same(t, initial, got, "the runner in force must still be the one published")
			require.Same(t, initial, cm.currentRunner(), "and the next turn must run it")
			require.Zero(t, atomic.LoadInt32(&closes),
				"§5.4: adopting the construction runner must never retire/close it — it is that runner's first generation")
			require.Zero(t, cm.ExecutorRefs().PendingRetirees, "nothing may sit retired behind a first publish")
			require.NotNil(t, cm.active, "the publish must still install a generation to hold references")
		})
	}
}

// fakeRunner 记录自身身份的哑 runner（仅测缝语义）。
type fakeRunner struct {
	id string
}

func (f *fakeRunner) Run(ctx context.Context, userID, sessionID string, message model.Message, runOpts ...trpcagent.RunOption) (<-chan *event.Event, error) {
	return make(chan *event.Event, 1), nil
}

func (f *fakeRunner) Close() error { return nil }

var _ runner.Runner = (*fakeRunner)(nil)

// publish installs a candidate while keeping the face the CM already records.
func publish(cm *ContextManager, id string) {
	cm.PublishExecutor(&fakeRunner{id: id}, cm.ExecutorConfig())
}

// TestExecutorPublish_DrainFreeTurnLevel ⑤in-flight turn 用旧 runner 跑完 + 下一 turn 起新（drain-free turn 级）。
func TestExecutorPublish_DrainFreeTurnLevel(t *testing.T) {
	old := &fakeRunner{id: "gen1"}
	cm := &ContextManager{runner: old}

	inFlight := cm.currentRunner()
	publish(cm, "gen2")

	if inFlight == nil || inFlight.(*fakeRunner).id != "gen1" {
		t.Fatalf("in-flight turn must keep the old runner reference, got %v", inFlight)
	}
	if got := cm.currentRunner(); got.(*fakeRunner).id != "gen2" {
		t.Fatalf("next turn must see the new runner, got %v", got)
	}
}

// TestExecutorPublish_ConcurrentPublishVsRead 钉住 钉住 并发取引用与发布换入不得撕裂：每次 RunFlow 取到的 runner 引用都非 nil（-race 重点）。
func TestExecutorPublish_ConcurrentPublishVsRead(t *testing.T) {
	cm := &ContextManager{runner: &fakeRunner{id: "gen0"}}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if r := cm.currentRunner(); r == nil {
						t.Error("runner reference must never be nil mid-publish")
						return
					}
				}
			}
		}()
	}
	for gen := 1; gen <= 50; gen++ {
		publish(cm, "gen")
	}
	close(stop)
	wg.Wait()
}

// TestExecutorPublish_ResidentInvariants 钉住 钉住 发布换代后常驻不变量成立：cm 本体、bus、projection、TaskManager 的引用原封不动。
func TestExecutorPublish_ResidentInvariants(t *testing.T) {
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	proj := compress.NewSessionProjection()
	bus, err := NewReliableEventBus("")
	if err != nil {
		t.Fatal(err)
	}
	cm := &ContextManager{runner: &fakeRunner{id: "g1"}, taskController: tm, projection: proj, bus: bus}

	publish(cm, "g2")

	if cm.taskController != tm {
		t.Error("TaskManager reference must survive the publish (org-level resident)")
	}
	if cm.projection != proj {
		t.Error("projection must survive the publish (R1 continuity)")
	}
	if cm.bus != bus {
		t.Error("bus must survive the publish (resident loop)")
	}
}

func benchPublishCM(b *testing.B, name string) *ContextManager {
	b.Helper()
	cm := newTestContextManager(name, &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 4096), nil)
	b.Cleanup(func() { _ = cm.Close() })
	return cm
}

// BenchmarkNewExecutorCandidate 度量纯构建路径：造一个候选，不安装、不退役、不记日志。
// 每轮先放弃前一轮遗留的候选再构造下一个（有界、每轮一次），放弃的代价单独在
// BenchmarkCandidateAbandon 计量；不把 b.N 个活对象留给进程结束。
func BenchmarkNewExecutorCandidate(b *testing.B) {
	cm := benchPublishCM(b, "bench-cand")
	face := cm.ExecutorConfig()

	var prev *StagedGeneration
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if prev != nil {
			prev.Discard()
		}
		prev = cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil)
	}
	b.StopTimer()
	if prev != nil {
		prev.Discard()
	}
	if got := cm.ExecutorRefs(); got.PendingRetirees != 0 || got.InFlightTurns != 0 {
		b.Fatalf("construction+abandon alone must leave no retirement debt or refs: %+v", got)
	}
}

// BenchmarkCandidateAbandon 计量「放弃一个已构造候选」本身：这是热更失败路径的
// 成本，过去从未被单独看过，于是它被埋在构建或回收的数字里。
func BenchmarkCandidateAbandon(b *testing.B) {
	cm := benchPublishCM(b, "bench-abandon")
	face := cm.ExecutorConfig()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil)
		b.StartTimer()
		st.Discard()
	}
	b.StopTimer()
}

// BenchmarkCommitPrepared 度量「提交」：把已准备好的候选换入，并让其上一代退役。
// 候选在计时区外构造（StopTimer/StartTimer），故构建成本不计入本数字；
// 回收观测也在计时区外做，不与本相位混计。
func BenchmarkCommitPrepared(b *testing.B) {
	cm := benchPublishCM(b, "bench-commit")
	face := cm.ExecutorConfig()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil)
		b.StartTimer()
		cm.ActivateExecutor(st)
	}
	b.StopTimer()
	if got := cm.ExecutorRefs(); got.PendingRetirees != 0 {
		b.Fatalf("an idle commit must reclaim its predecessor immediately: %+v", got)
	}
}

// BenchmarkLeaseAcquire 只计量「获取」：登记在途引用并取回执行器。释放在计时区外
// 完成——把 acquire/release 合成一个数字，就看不出热更期间真正被挡住的是哪一半。
func BenchmarkLeaseAcquire(b *testing.B) {
	cm := benchPublishCM(b, "bench-acquire")

	var held []*ExecLease
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		held = append(held, cm.AcquireLease(LeaseTurn))
	}
	b.StopTimer()
	for _, l := range held {
		l.Release()
	}
	if got := cm.ExecutorRefs(); got.InFlightTurns != 0 {
		b.Fatalf("every acquired lease must be released: %+v", got)
	}
}

// BenchmarkLeaseRelease 计量「释放」单独的成本：它是退役代真正被回收的触发点
// （回收本身另测），也是空闲态能否立刻收敛的关键。
func BenchmarkLeaseRelease(b *testing.B) {
	cm := benchPublishCM(b, "bench-release")

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l := cm.AcquireLease(LeaseTurn)
		b.StartTimer()
		l.Release()
		b.StopTimer()
	}
	if got := cm.ExecutorRefs(); got.InFlightTurns != 0 {
		b.Fatalf("release loop must not leak references: %+v", got)
	}
}

// BenchmarkRetirementReclaim 度量「回收」这一具体 disposer：先在计时外造出一个
// 被引用保住的退役代，然后只计它被真正关闭的那一步。轮询式观测一律放在计时区外。
func BenchmarkRetirementReclaim(b *testing.B) {
	cm := benchPublishCM(b, "bench-reclaim")
	face := cm.ExecutorConfig()
	benchRun := func() *ExecLease { return cm.AcquireLease(LeaseTurn) }

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		keeper := benchRun()
		cm.ActivateExecutor(cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil))
		b.StartTimer()
		keeper.Release()
		b.StopTimer()
	}
	if got := cm.ExecutorRefs(); got.PendingRetirees != 0 || got.InFlightTurns != 0 {
		b.Fatalf("reclaim must finish without residue: %+v", got)
	}
}

// BenchmarkBeginTurn keeps the PAIRED cost (the actual per-turn hot path): acquire
// and release as production does them, with the reloader that returns early.
func BenchmarkBeginTurn(b *testing.B) {
	cm := benchPublishCM(b, "bench-begin-turn")
	cm.SetOrgReloader(func() { _ = time.Since(time.Now()) })

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec, release := cm.BeginTurn()
		if exec == nil {
			b.Fatal("BeginTurn must return the effective executor")
		}
		release()
	}
	b.StopTimer()
	if refs := cm.ExecutorRefs(); refs.InFlightTurns != 0 {
		b.Fatalf("acquire/release must be paired; %d references leaked", refs.InFlightTurns)
	}
}

// BenchmarkRunFlowPinnedExecutor 度量带钉定执行器的 turn 入口开销（pinned 非 nil
// 时省掉一次 active 读，其余同形）。
func BenchmarkRunFlowPinnedExecutor(b *testing.B) {
	m := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("bench-runflow", m, nil, make(chan *event.Event, 4096), nil)
	b.Cleanup(func() { _ = cm.Close() })
	exec, release := cm.BeginTurn()
	defer release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		if err := cm.RunFlowWithExecutor(ctx, model.NewUserMessage("bench"), exec); err != nil {
			b.Fatal(err)
		}
		cancel()
	}
}

// declarationNames lists what a tool slice advertises (the model-visible surface).
func declarationNames(tools []trpctool.Tool) []string {
	var out []string
	for _, tl := range tools {
		if tl == nil {
			continue
		}
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// TestExecutorConfig_ToolsContainerIsPrivate A：getter 返回的 Tools 容器私有——覆盖元素不得写穿在线面，在线执行仍见原工具。
func TestExecutorConfig_ToolsContainerIsPrivate(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	keep := &pinEchoTool{name: "keep"}
	cm := newTestContextManager("iso-tools", capture, []trpctool.Tool{keep}, make(chan *event.Event, 16), nil)

	out, err := cm.currentRunner().Run(context.Background(), "u1", "s1", model.NewUserMessage("first"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)
	require.Equal(t, []string{"keep"}, lastRequestToolNames(capture), "precondition: gen1 advertises keep")

	face := cm.ExecutorConfig()
	require.Len(t, face.Tools, 1)
	face.Tools[0] = &pinEchoTool{name: "evil"}

	require.Same(t, keep, cm.ExecutorConfig().Tools[0],
		"the published face must keep its original tool element (getter must not alias the live Tools array)")
	require.Equal(t, []string{"keep"}, declarationNames(cm.ExecutorConfig().Tools),
		"and still advertise the original declaration")

	out2, err := cm.currentRunner().Run(context.Background(), "u1", "s1", model.NewUserMessage("second"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	require.Equal(t, []string{"keep"}, lastRequestToolNames(capture),
		"online execution must be unaffected by mutating a returned snapshot")
}

// TestExecutorConfig_ValuePointersArePrivate B：getter 返回的三个值指针独立——解引用写不得触及在线配置。
func TestExecutorConfig_ValuePointersArePrivate(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("iso-vals", capture, nil, make(chan *event.Event, 16), nil)
	on, tok, eff := true, 128, "high"
	f0 := cm.ExecutorConfig()
	f0.ThinkingEnabled, f0.ThinkingTokens, f0.ReasoningEffort = &on, &tok, &eff
	cm.PublishExecutor(cm.NewExecutorCandidate(f0), f0)

	face := cm.ExecutorConfig()
	require.NotNil(t, face.ThinkingEnabled)
	require.NotNil(t, face.ThinkingTokens)
	require.NotNil(t, face.ReasoningEffort)
	*face.ThinkingEnabled = false
	*face.ThinkingTokens = 999
	*face.ReasoningEffort = "low"

	live := cm.ExecutorConfig()
	require.True(t, *live.ThinkingEnabled, "getter must not leak the live ThinkingEnabled pointee")
	require.Equal(t, 128, *live.ThinkingTokens, "nor ThinkingTokens")
	require.Equal(t, "high", *live.ReasoningEffort, "nor ReasoningEffort")
}

// TestPublishExecutor_DoesNotAliasCallerInput C：发布对调用方输入取私有副本——发布后调用方继续改自己的输入不得影响在线面。
func TestPublishExecutor_DoesNotAliasCallerInput(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("pub-alias", capture, nil, make(chan *event.Event, 16), nil)
	keep := &pinEchoTool{name: "keep"}
	input := cm.ExecutorConfig()
	input.Tools = []trpctool.Tool{keep}
	cm.PublishExecutor(cm.NewExecutorCandidate(input), input)

	input.Tools[0] = &pinEchoTool{name: "evil"}
	require.Same(t, keep, cm.ExecutorConfig().Tools[0],
		"publish must take a private copy of the caller's input face, not alias it")
}

// publishFresh 是"取代并退役"辅助器：发布一个新候选（其 runner 包着 closeCounter 供调用方观察
// 关闭），并连同其代际 id 一起返回。新候选成为当前代，原当前代被取代并退役。
//
// PublishExecutor 返回当下生效的 runner，而不是代际 id。刚发布那一代的 id 经一次作用在
// 「当前绑定」上的临时租约读得；释放这次探测不会回收它——回收只发生在"既已退役又无引用"的
// 代际上，而它仍是当前代。
func publishFresh(cm *ContextManager) (int64, *closeCounter) {
	face := cm.ExecutorConfig()
	observed := &closeCounter{Runner: cm.NewExecutorCandidate(face)}
	cm.PublishExecutor(observed, face)
	probe := cm.AcquireLease(LeaseTurn)
	gid := probe.Generation()
	probe.Release()
	return gid, observed
}

// TestCommitSeparateFromReclaim 钉住 提交与回收是两个阶段：钉住活动代再被取代（即提交）时，被引用的 runner 仍处于打开并计为一个待退役者——关闭不夹带在提交里，只有释放引用才触发回收。耗时只记录，绝不充当正确性阈值。
func TestCommitSeparateFromReclaim(t *testing.T) {
	cm := newTestContextManager("d53-split", &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 64), nil)

	gid, held := publishFresh(cm)
	lease := cm.AcquireLease(LeaseTurn)
	require.Equal(t, gid, lease.Generation(), "the lease pins the generation we are watching")

	commitStart := time.Now()
	_, _ = publishFresh(cm)
	commitDur := time.Since(commitStart)

	row := genRow(cm, gid)
	require.NotNil(t, row, "the referenced generation stays on the books")
	require.True(t, row.Retired, "a newer publish superseded it")
	require.False(t, row.Closed, "the commit MUST NOT close a still-referenced runner")
	require.Zero(t, held.closes.Load(), "no close happened inside the commit section")
	require.Equal(t, 1, cm.ExecutorRefs().PendingRetirees, "honestly counted as one live-referenced retiree")

	reclaimStart := time.Now()
	lease.Release()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 },
		10*time.Second, 10*time.Millisecond, "reclaimed once nothing references it")
	reclaimDur := time.Since(reclaimStart)
	require.Equal(t, int64(1), held.closes.Load(), "the close happened exactly once, triggered by release")

	t.Logf("[§5.3 phase sample] 提交(commit, close deferred)=%v  回收(reclaim-on-release)=%v — observational; structural facts asserted above",
		commitDur.Round(time.Microsecond), reclaimDur.Round(time.Microsecond))
}

// TestHeldResourcesCountedNotForceClosedUnderChurn 钉住 换代过程中固定持有若干引用时，待退役集合必须是活引用的诚实计数。
// - 绝不为凑过计数门而强关仍在使用的 runner；引用落账后应收敛到全部回收。
func TestHeldResourcesCountedNotForceClosedUnderChurn(t *testing.T) {
	cm := newTestContextManager("d53-churn", &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 512), nil)

	const k = 6
	leases := make([]*ExecLease, 0, k)
	gids := make([]int64, 0, k)
	counters := make([]*closeCounter, 0, k)

	for i := 0; i < k; i++ {
		gid, c := publishFresh(cm)
		lease := cm.AcquireLease(LeaseTurn)
		require.Equal(t, gid, lease.Generation(), "iteration %d pins the generation it is watching", i)
		leases = append(leases, lease)
		gids = append(gids, gid)
		counters = append(counters, c)
	}
	publishFresh(cm)

	refs := cm.ExecutorRefs()
	require.Equal(t, k, refs.PendingRetirees,
		"the retired set equals the number of live references (bounded by refs, not by history)")
	for i, gid := range gids {
		row := genRow(cm, gid)
		require.NotNil(t, row, "held generation %d stays on the books", gid)
		require.True(t, row.Retired, "held generation %d was superseded", gid)
		require.False(t, row.Closed, "held generation %d is open until its own reference drops", gid)
		require.Equal(t, 1, row.Total, "held generation %d is pinned by exactly its one lease", gid)
		require.Zero(t, counters[i].closes.Load(), "held generation %d was never force-closed", gid)
	}

	for i := len(leases) - 1; i >= 0; i-- {
		leases[i].Release()
		require.Eventually(t, func() bool {
			return cm.ExecutorRefs().PendingRetirees == i && counters[i].closes.Load() == 1
		}, 10*time.Second, 10*time.Millisecond,
			"releasing reference %d reclaims exactly its own generation, leaving %d held", i, i)
	}

	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 10*time.Millisecond)
	require.Zero(t, cm.ExecutorRefs().InFlightTurns, "all references released")
	for i, c := range counters {
		require.Equal(t, int64(1), c.closes.Load(), "generation %d closed exactly once in total", i)
	}
}

// TestBeginTurn_ChecksOncePerTurnNotPerIteration 钉住 pins where the organization check lives now. Before  the check ran in the BeforeModel callback, i.e.
func TestBeginTurn_ChecksOncePerTurnNotPerIteration(t *testing.T) {
	var checks atomic.Int64
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("bind-check", capture, nil, make(chan *event.Event, 16), nil)
	cm.SetOrgReloader(func() { checks.Add(1) })

	r, release := cm.BeginTurn()
	defer release()
	require.NotNil(t, r)
	require.Equal(t, int64(1), checks.Load(), "the turn boundary checks configuration exactly once")

	for i := 0; i < 2; i++ {
		out, err := r.Run(context.Background(), "u", "s-check", model.NewUserMessage("iterate"))
		require.NoError(t, err)
		drainRunner(out, 5*time.Second)
	}
	require.Positive(t, capture.requestCount(), "precondition: the model was actually called")
	require.Equal(t, int64(1), checks.Load(),
		"BeforeModel must no longer run the organization check per LLM iteration")

	_, release2 := cm.BeginTurn()
	release2()
	require.Equal(t, int64(2), checks.Load())
}

// TestRunFlowWithExecutor_PinnedExecutorSurvivesMidTurnPublish 钉住 回合边界取到的执行器跑完整个回合，即便回合开始前发布了新一代（重试循环的情形）；未取钉的回合才去解析当前执行器，那是下一回合的行为，与本回合无关。
func TestRunFlowWithExecutor_PinnedExecutorSurvivesMidTurnPublish(t *testing.T) {
	gen1 := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("bind-pin", gen1, nil, make(chan *event.Event, 16), nil)
	cm.SetOrgReloader(func() {})

	pinned, releasePinned := cm.BeginTurn()
	defer releasePinned()
	require.NotNil(t, pinned)

	gen2 := &requestCapturingModel{resp: gateOKResp()}
	face := cm.ExecutorConfig()
	face.Model = gen2
	require.NotNil(t, cm.PublishExecutor(cm.NewExecutorCandidate(face), face))

	require.NoError(t, cm.RunFlowWithExecutor(context.Background(), model.NewUserMessage("in-flight"), pinned))
	require.Equal(t, 1, gen1.requestCount(), "the pinned executor must serve the whole turn")
	require.Equal(t, 0, gen2.requestCount(), "a mid-turn publication must not steal an in-flight turn")

	next, releaseNext := cm.BeginTurn()
	defer releaseNext()
	require.NoError(t, cm.RunFlowWithExecutor(context.Background(), model.NewUserMessage("next"), next))
	require.Equal(t, 1, gen1.requestCount(), "the superseded generation must stop receiving turns")
	require.Equal(t, 1, gen2.requestCount(), "the next turn must use the newly published generation")
}

// TestReclaimTurnTakesCurrentGeneration 钉住  的机制钉：后台任务 settle 回流 形成的新顶层 turn 与其来源执行无关——它在**自己开始执行时**取当时的有效代。
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

	gen2 := &requestCapturingModel{resp: gateOKResp()}
	face := cm.ExecutorConfig()
	face.Model = gen2
	require.NotNil(t, cm.PublishExecutor(cm.NewExecutorCandidate(face), face))

	bus.Publish(NewExternalInputEvent(SourceTask, model.Message{
		Role: model.RoleUser, Content: "[task settled] background work (id=t-1) completed → 结果: done",
	}))
	require.Eventually(t, func() bool { return gen2.requestCount() >= 1 }, 10*time.Second, 20*time.Millisecond,
		"the reclaim turn must run on the generation current at ITS OWN start")

	require.Equal(t, int64(2), checks.Load(), "the reclaim turn acquires once, like any other turn")
	require.Equal(t, 1, gen1.requestCount(),
		"the superseded generation must serve no later turn — the settle source does not pin it")
}

// TestBeginTurn_RegistersHandoffReferenceUntilReleased 钉住"取到即登记"这条独立契约：获取版本本身就在交出执行器之前登记在途引用。
// - 否则一次发布加回收清扫若落在"取到执行器"与"进入运行主体"之间，就会关掉本回合正要用的那台 runner；
// - 登记必须早于交付，该窗口才不存在。
func TestBeginTurn_RegistersHandoffReferenceUntilReleased(t *testing.T) {
	cm := newTestContextManager("handoff", &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 64), nil)

	lease := cm.BeginTurnLease()
	require.Equal(t, int64(1), cm.ExecutorRefs().InFlightTurns,
		"acquiring must register the in-flight reference immediately, not on run entry")
	handed := lease.Generation()

	mid := publishObservedGeneration(cm)
	publishObservedGeneration(cm)

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

// leaseCountingRunner records Close calls so a test can observe exactly when a
// generation was reclaimed.
type leaseCountingRunner struct {
	id     string
	closed atomic.Int64
}

func (f *leaseCountingRunner) Run(context.Context, string, string, model.Message, ...trpcagent.RunOption) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	close(ch)
	return ch, nil
}

func (f *leaseCountingRunner) Close() error {
	f.closed.Add(1)
	return nil
}

var _ runner.Runner = (*leaseCountingRunner)(nil)

func leaseCloseCount(r runner.Runner) int64 {
	if c, ok := r.(*leaseCountingRunner); ok {
		return c.closed.Load()
	}
	return -1
}

// TestLease_UnrelatedGenerationReclaimedIndependently 钉住 无关旧代必须能被独立回收。
// - 场景里一回合全程持有某一代的引用，另一代随即被取代且无人引用；
// - 聚合式的在途门不得因此挡住被取代那代的回收。
func TestLease_UnrelatedGenerationReclaimedIndependently(t *testing.T) {
	blocking := &requestCapturingModel{}
	cm := newTestContextManager("lease-indep", blocking, nil, make(chan *event.Event, 64), nil)
	g1 := cm.currentRunner()
	require.NotNil(t, g1)

	pinned, releaseTurn := cm.BeginTurn()
	require.Same(t, g1, pinned, "the turn pins the generation in force at acquire")
	t.Cleanup(releaseTurn)

	face := cm.ExecutorConfig()
	g2 := &leaseCountingRunner{id: "g2"}
	cm.PublishExecutor(g2, face)
	g3 := &leaseCountingRunner{id: "g3"}
	cm.PublishExecutor(g3, face)

	require.Equal(t, int64(1), leaseCloseCount(g2),
		"an unreferenced retired generation must be reclaimed while an older generation is still in flight (D6「一个 G1 未停止调用只阻挡其实际使用资源」)")

	releaseTurn()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 },
		5*time.Second, 20*time.Millisecond,
		"once the last reference drops, every retiree of this cm is reclaimed")
}

// gateAgent is a sub-agent whose run is held on a test gate. `running` fires when
// its Run begins, `finished` closes when it returns — the two edges a lease test
// must observe (acquire before work starts, release only after the stop).
type gateAgent struct {
	name     string
	running  chan struct{}
	finish   chan struct{}
	finished chan struct{}
	once     sync.Once
	once2    sync.Once
}

func newGateAgent(name string) *gateAgent {
	return &gateAgent{
		name:     name,
		running:  make(chan struct{}),
		finish:   make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (a *gateAgent) Run(ctx context.Context, _ *trpcagent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 4)
	a.once.Do(func() { close(a.running) })
	go func() {
		defer a.once2.Do(func() { close(a.finished) })
		select {
		case <-a.finish:
		case <-ctx.Done():
		}
		ch <- &event.Event{Response: &model.Response{Done: true, Choices: []model.Choice{
			{Message: model.NewAssistantMessage("done")},
		}}}
		close(ch)
	}()
	return ch, nil
}

func (a *gateAgent) Tools() []trpctool.Tool              { return nil }
func (a *gateAgent) Info() trpcagent.Info                { return trpcagent.Info{Name: a.name, Description: "gated"} }
func (a *gateAgent) SubAgents() []trpcagent.Agent        { return nil }
func (a *gateAgent) FindSubAgent(string) trpcagent.Agent { return nil }

// leaseHarness wires a generation-bearing context manager, a turn lease pinned on
// it (what a business turn holds) and a task manager for the async paths.
type leaseHarness struct {
	cm      *ContextManager
	turn    *ExecLease
	gen1    *leaseCountingRunner
	spawner *task.TaskManager
	ctx     context.Context
}

func newLeaseHarness(t *testing.T, gate func() string) *leaseHarness {
	t.Helper()
	cm := &ContextManager{name: "d6", runner: &leaseCountingRunner{id: "boot"}}
	g1 := &leaseCountingRunner{id: "g1"}
	cm.PublishExecutor(g1, ContextManagerConfig{Name: "d6"})
	turn := cm.AcquireLease(LeaseTurn)
	t.Cleanup(turn.Release)
	h := &leaseHarness{cm: cm, turn: turn, gen1: g1}
	cfg := task.TaskManagerConfig{}
	if gate != nil {
		cfg.SpawnGate = gate
	}
	h.spawner = task.NewTaskManager(cfg)
	h.ctx = turn.WithContext(task.WithTaskSpawner(context.Background(), h.spawner))
	return h
}

func (h *leaseHarness) refs() ExecutorRefs { return h.cm.ExecutorRefs() }

// TestLease_DerivedSubCallHoldsCallersGenerationUntilItStops 钉住 嵌套委派继承发起方那一代：真实子调用的引用挂在调用方所钉的那一代上。
// - 因此调用中途发布新一代，也不能在该子调用停止之前回收调用方那一代。
func TestLease_DerivedSubCallHoldsCallersGenerationUntilItStops(t *testing.T) {
	child, g := newGatedOrg(t)
	h := newLeaseHarness(t, nil)

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("go")))
	out, err := child.Run(h.ctx, inv)
	require.NoError(t, err)
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the gated child never reached its model call — harness broken")
	}
	require.Equal(t, 1, h.refs().SubCalls,
		"a running sub-call holds its own reference kind on the caller's generation")
	require.Equal(t, int64(1), h.refs().InFlightTurns, "the caller's turn is counted once, not per descendant")

	g2 := &leaseCountingRunner{id: "g2"}
	h.cm.PublishExecutor(g2, ContextManagerConfig{Name: "d6-g2"})
	un := h.cm.UnconvergedRefs()
	require.Len(t, un, 1, "the retired caller generation is the unconverged one")
	require.Equal(t, 1, un[0].Refs[LeaseTurn.String()])
	require.Equal(t, 1, un[0].Refs[LeaseSubCall.String()], "the CHILD is why it cannot be reclaimed yet")
	require.Zero(t, h.gen1.closed.Load(), "a live derived call keeps its generation open")

	close(g.gate)
	for range out {
	}
	require.Zero(t, h.refs().SubCalls, "the reference drops exactly when the child stops")
	require.Zero(t, h.gen1.closed.Load(), "the caller's turn still references it")

	h.turn.Release()
	require.Eventually(t, func() bool { return h.gen1.closed.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"the generation closes once its LAST reference is gone")
	require.Equal(t, int64(1), h.gen1.closed.Load(), "exactly once")
}

// TestLease_BackgroundRunKeepsGenerationPastTheAck 钉住 返回确认不得释放执行引用——只有生产者真正返回才算释放。
// 契约: docs/wiki/agent/execution-generations.md#lease-holds-reference
func TestLease_BackgroundRunKeepsGenerationPastTheAck(t *testing.T) {
	h := newLeaseHarness(t, nil)
	child := newGateAgent("worker")
	w := NewAgentToolWrapper(child, "do work", nil, nil)
	w.SetAsyncDenseDuration(30 * time.Millisecond)

	out, err := w.Call(h.ctx, subagentCallArgs(t))
	require.NoError(t, err)
	require.Contains(t, out.(string), "后台运行", "the sync-wait window ended in an ack")

	require.Equal(t, 1, h.refs().BackgroundRuns,
		"an ACK is not a stop credential: the background run still references the generation")
	g2 := &leaseCountingRunner{id: "g2"}
	h.cm.PublishExecutor(g2, ContextManagerConfig{Name: "d6-g2"})
	require.Zero(t, h.gen1.closed.Load(), "the generation with a live background run stays open")

	h.turn.Release()
	require.Zero(t, h.gen1.closed.Load(), "releasing the turn alone must not reclaim the background's generation")

	close(child.finish)
	<-child.finished
	require.Eventually(t, func() bool { return h.gen1.closed.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"the generation is reclaimed exactly when the background producer stops")
	require.Zero(t, h.refs().BackgroundRuns, "and its reference is gone")
}

// TestLease_RejectedAndDedupedSpawnReleaseExactlyOnce 钉住 任务层未收养、而探测器生产者已在跑的两条路径：引用必须活到该生产者停止，且恰好释放一次。
// - 提前释放会让仍在写入的一方失去依赖；重复释放则把计数打成负数。
func TestLease_RejectedAndDedupedSpawnReleaseExactlyOnce(t *testing.T) {
	t.Run("spawn rejected by gate", func(t *testing.T) {
		h := newLeaseHarness(t, func() string { return "disk degraded: new background tasks paused" })
		child := newGateAgent("worker")
		w := NewAgentToolWrapper(child, "do work", nil, nil)
		w.SetAsyncDenseDuration(20 * time.Millisecond)

		go func() {
			time.Sleep(150 * time.Millisecond)
			close(child.finish)
		}()
		done := make(chan any, 1)
		go func() {
			out, _ := w.Call(h.ctx, subagentCallArgs(t))
			done <- out
		}()
		out := <-done
		require.Contains(t, out.(string), "未被任务层纳管")
		select {
		case <-child.finished:
		default:
			t.Fatal("§4.1「拒绝分支也先等停止」: the rejection branch returned while its un-adopted producer was still running")
		}
		require.Zero(t, h.refs().BackgroundRuns, "the un-adopted run's reference is released once, at its stop")
	})

	t.Run("deduped by same key", func(t *testing.T) {
		h := newLeaseHarness(t, nil)
		first := newGateAgent("worker")
		w := NewAgentToolWrapper(first, "do work", nil, nil)
		w.SetAsyncDenseDuration(20 * time.Millisecond)

		firstRunning := make(chan struct{})
		go func() {
			close(firstRunning)
			_, _ = w.Call(h.ctx, subagentCallArgs(t))
		}()
		<-first.running

		second := newGateAgent("worker")
		w2 := NewAgentToolWrapper(second, "do work", nil, nil)
		w2.SetAsyncDenseDuration(20 * time.Millisecond)
		out, err := w2.Call(h.ctx, subagentCallArgs(t))
		require.NoError(t, err)
		require.Contains(t, out.(string), "同名计划任务已在运行")
		second.once2.Do(func() { close(second.finished) })
		require.Eventually(t, func() bool { return h.refs().BackgroundRuns <= 1 }, 5*time.Second, 10*time.Millisecond,
			"only the adopted task keeps a background reference")

		close(first.finish)
		require.Eventually(t, func() bool { return h.refs().BackgroundRuns == 0 }, 5*time.Second, 10*time.Millisecond,
			"every background reference is released exactly once, including the deduped attempt")
	})
}

// TestLease_NoReferenceForBusinessTurnMultiplication 钉住 pins 's counting half: one business turn = ONE reference, whether or not its flow re-enters RunFlow.。
func TestLease_NoReferenceForBusinessTurnMultiplication(t *testing.T) {
	blocking := &requestCapturingModel{}
	cm := newTestContextManager("no-double-count", blocking, nil, make(chan *event.Event, 64), nil)

	lease := cm.AcquireLease(LeaseTurn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cm.RunFlowWithExecutor(lease.WithContext(ctx), model.NewUserMessage("one turn"), lease.Runner())
	}()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().InFlightTurns == 1 }, 10*time.Second, 20*time.Millisecond,
		"a turn carrying its lease must be counted ONCE (BeginTurn + RunFlow used to double-register)")
	require.Zero(t, cm.ExecutorRefs().SubCalls+cm.ExecutorRefs().BackgroundRuns)

	cancel()
	<-done
	lease.Release()
	require.Zero(t, cm.totalRefs(), "and the same single reference is what drops")
}

// pathModel answers the first call with a degenerate final (empty content, no
// tool call → the loop retries it) and parks every later call on `gate`, so a
// test can observe the reference state INSIDE the retry gap — the window where a
// per-attempt release would show up as zero.
type pathModel struct {
	calls atomic.Int64
	gate  chan struct{}
	resp  *model.Response
}

func (m *pathModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	n := m.calls.Add(1)
	ch := make(chan *model.Response, 1)
	go func() {
		defer close(ch)
		if n == 1 {
			ch <- &model.Response{ID: "r", Done: true, Choices: []model.Choice{{
				Message: model.Message{Role: model.RoleAssistant}}}}
			return
		}
		select {
		case <-m.gate:
		case <-ctx.Done():
			return
		}
		if m.resp != nil {
			ch <- m.resp
		}
	}()
	return ch, nil
}

func (m *pathModel) Info() model.Info { return model.Info{Name: "path-model"} }

// startLeaseLoop builds a resident loop over a readable context manager and
// drains its output, returning everything a path test needs to watch references.
func startLeaseLoop(t *testing.T, m model.Model) (*TagentAgent, *ContextManager) {
	t.Helper()
	outputCh := make(chan *event.Event, 256)
	bus := NewEventBus()
	cm := newTestContextManager("turn-paths", m, nil, outputCh, bus)
	ta := &TagentAgent{
		persistentBus:  bus,
		activeBus:      bus,
		contextManager: cm,
		config:         &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000},
		outputCh:       outputCh,
		name:           "turn-paths",
	}
	out, err := ta.StartLoop("test-user", "turn-paths")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() {
		_ = ta.Close()
		<-done
	})
	return ta, cm
}

// requireSettled asserts the loop's turn references all came back — exactly once
// per turn, never below zero (a doubled release shows up as a negative total, a
// lost defer as a permanent positive one).
func requireSettled(t *testing.T, cm *ContextManager) {
	t.Helper()
	require.Eventually(t, func() bool { return cm.totalRefs() == 0 }, 10*time.Second, 20*time.Millisecond,
		"every reference this turn took must be back at zero")
	require.Zero(t, cm.ExecutorRefs().PendingRetirees, "and nothing may be left unreclaimed")
}

func TestLease_EveryTurnPathReleasesExactlyOnce(t *testing.T) {
	t.Run("normal completion", func(t *testing.T) {
		gate := make(chan struct{})
		m := &countingModel{label: "ok", resp: crossResp("served:ok"), gate: gate}
		ta, cm := startLeaseLoop(t, m)
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return m.calls.Load() >= 1 }, 10*time.Second, 20*time.Millisecond,
			"precondition: the turn reached the model")
		require.Equal(t, int64(1), cm.ExecutorRefs().InFlightTurns,
			"a running turn holds exactly one reference")
		close(gate)
		requireSettled(t, cm)
	})

	t.Run("response-internal model error", func(t *testing.T) {
		m := &requestCapturingModel{resp: &model.Response{
			ID: "err", Done: true, Error: &model.ResponseError{Type: "server_error", Message: "upstream exploded"}}}
		ta, cm := startLeaseLoop(t, m)
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return m.requestCount() >= 1 }, 10*time.Second, 20*time.Millisecond,
			"precondition: the failing call must have been made")
		requireSettled(t, cm)
	})

	t.Run("degenerate retry keeps ONE reference across attempts", func(t *testing.T) {
		m := &pathModel{gate: make(chan struct{}), resp: gateOKResp()}
		ta, cm := startLeaseLoop(t, m)
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return m.calls.Load() >= 1 }, 10*time.Second, 20*time.Millisecond)
		require.Eventually(t, func() bool { return m.calls.Load() >= 2 }, 10*time.Second, 20*time.Millisecond,
			"the retry attempt must have started")
		require.Equal(t, int64(1), cm.ExecutorRefs().InFlightTurns,
			"one business turn holds one reference for ALL its attempts (§3.2 pin, §4.1 exactly-once)")
		close(m.gate)
		requireSettled(t, cm)
	})

	t.Run("cancellation mid-stream", func(t *testing.T) {
		ta, cm := startLeaseLoop(t, &requestCapturingModel{})
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return cm.ExecutorRefs().InFlightTurns == 1 },
			10*time.Second, 20*time.Millisecond, "the turn is parked mid-flight")
		ta.StopLoop()
		requireSettled(t, cm)
	})

	t.Run("intake refusal after close takes no reference", func(t *testing.T) {
		ta, cm := startLeaseLoop(t, &requestCapturingModel{resp: gateOKResp()})
		require.NoError(t, ta.Close())
		requireSettled(t, cm)
		require.NoError(t, ta.Close(), "a terminal close leaves nothing unconverged, so repeating it stays clean")
	})
}

// TestLease_LeaseNeverEntersPersistedTaskMaterial 钉住 边界：租约指针不得进入持久化的任务材料。
// - 派生任务持久的是身份加声明性字符串，租约在其中没有落点；
// - 所以被重放的任务不可能复活一个租约。
func TestLease_LeaseNeverEntersPersistedTaskMaterial(t *testing.T) {
	h := newLeaseHarness(t, nil)
	child := newGateAgent("worker")
	w := NewAgentToolWrapper(child, "do work", nil, nil)
	w.SetAsyncDenseDuration(30 * time.Millisecond)

	out, err := w.Call(h.ctx, subagentCallArgs(t))
	require.NoError(t, err)
	require.Contains(t, out.(string), "后台运行")
	defer close(child.finish)

	tasks := h.spawner.List()
	require.NotEmpty(t, tasks, "the run was adopted, so there IS persisted material to inspect")
	for _, tk := range tasks {
		require.NotNil(t, tk.Spec.Declarative, "a subagent task projects declarative identity")
		scanNoLease(t, reflect.ValueOf(tk.Spec.Declarative).Elem(), 0)
	}
	require.Empty(t, h.cm.UnconvergedRefs(), "the record carries identity only — it is not a reference holder")
}

// scanNoLease walks a persisted value and fails on any *ExecLease (or any pointer
// to a context-manager-side runtime object, which could only be a resurrection
// vector). Depth- and cycle-bounded for the small declarative surface.
func scanNoLease(t *testing.T, v reflect.Value, depth int) {
	t.Helper()
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			require.NotEqual(t, reflect.TypeOf(&ExecLease{}), v.Type(),
				"an execution lease must never appear inside persisted task material (found at depth %d)", depth)
			if v.Kind() == reflect.Interface && !v.IsNil() {
				scanNoLease(t, v.Elem(), depth+1)
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			scanNoLease(t, v.Field(i), depth+1)
		}
	case reflect.Map:
		for _, mk := range v.MapKeys() {
			scanNoLease(t, v.MapIndex(mk), depth+1)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			scanNoLease(t, v.Index(i), depth+1)
		}
	}
}

// TestBuildExecutionFaceMatchesCMFace 钉住 单一写法：去壳后的执行面派生结果必须与上下文管理器构造时的初始面逐字段相等。
// - 运行态句柄（记忆插件、会话服务、输出通道、总线、投影、事件回调）在派生面上故意为零，由装配处按常驻对象覆盖；
// - 这条等式只在执行面上成立，不要求整面全等。
func TestBuildExecutionFaceMatchesCMFace(t *testing.T) {
	enabled := true
	tokens := 4096
	effort := "high"
	cfg := &TagentConfig{
		Name:                 "face-eq",
		Model:                &mockModel{},
		SystemPrompt:         "prompt body",
		Temperature:          0.7,
		MaxToolIterations:    12,
		MaxTokens:            9000,
		CompressThreshold:    0.75,
		KeepRecentTasks:      9,
		ThinkingEnabled:      &enabled,
		ThinkingTokens:       &tokens,
		ReasoningEffort:      &effort,
		ReasoningContentMode: "parsed",
		Compress: CompressConfig{
			CompactKeysListed: 11,
			RecentFullCount:   22,
			CardMaxChars:      333,
			SummaryMaxTokens:  777,
		},
	}

	bus := NewEventBus()
	outCh := make(chan *event.Event, 4)
	proj := compress.NewSessionProjection()
	cm := newContextManagerFromConfig(cfg, nil, nil, nil, bus, outCh, proj, nil)
	cmFace := cm.ExecutorConfig()

	got := BuildExecutionFace(cfg)
	require.Equal(t, cmFace.Name, got.Name)
	require.Equal(t, cmFace.SystemPrompt, got.SystemPrompt)
	require.Equal(t, cmFace.Temperature, got.Temperature)
	require.Equal(t, cmFace.MaxToolIters, got.MaxToolIters)
	require.Equal(t, cmFace.MaxTokens, got.MaxTokens)
	require.Equal(t, cmFace.ThresholdPct, got.ThresholdPct)
	require.Equal(t, cmFace.ThinkingEnabled, got.ThinkingEnabled)
	require.Equal(t, cmFace.ThinkingTokens, got.ThinkingTokens)
	require.Equal(t, cmFace.ReasoningEffort, got.ReasoningEffort)
	require.Equal(t, cmFace.ReasoningContentMode, got.ReasoningContentMode)
	require.Equal(t, cmFace.CompactKeysListed, got.CompactKeysListed)
	require.Equal(t, cmFace.RecentFullCount, got.RecentFullCount)
	require.Equal(t, cmFace.CardMaxChars, got.CardMaxChars)
	require.Same(t, cmFace.Model, got.Model)
	require.Equal(t, cmFace.Tools, got.Tools)
	require.Equal(t, cmFace.MemStore, got.MemStore)
	require.NotNil(t, got.Compressor)
	require.NotNil(t, got.TokenCounter)
	require.Nil(t, got.MemPlugin)
	require.Nil(t, got.SessionSvc)
	require.Nil(t, got.Bus)
	require.Nil(t, got.Projection)
	require.Nil(t, got.OnEvent)
}

// relaunchStubAgent is a named delegation target. Only Info().Name and Run()
// matter here: the accept path asserts on the SpawnResult key (spawn happens
// before the detector's run), and Run() returns an already-closed stream.
type relaunchStubAgent struct {
	trpcagent.Agent
	name string
}

func (a relaunchStubAgent) Info() trpcagent.Info { return trpcagent.Info{Name: a.name} }

func (a relaunchStubAgent) Run(_ context.Context, _ *trpcagent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func relaunchWrapper(name string) *AgentToolWrapper {
	return NewAgentToolWrapper(relaunchStubAgent{name: name}, name, nil, nil)
}

// TestSubagentWrapper_FollowsEffectiveFace 钉住 解析源本身：`SubagentWrapper` 认的是 已发布的面孔，因此换代、回滚式重发布都会立刻改变它认得谁——这正是「不另立第二 路由真源」的形状。
func TestSubagentWrapper_FollowsEffectiveFace(t *testing.T) {
	keep, drop := relaunchWrapper("keep"), relaunchWrapper("drop")
	cm := newTestContextManager("relaunch-face", &requestCapturingModel{resp: gateOKResp()},
		[]trpctool.Tool{keep, drop}, make(chan *event.Event, 32), nil)

	require.Same(t, drop, cm.SubagentWrapper("drop"), "baseline: both targets resolve")

	face := cm.ExecutorConfig()
	face.Tools = []trpctool.Tool{keep}
	cm.PublishExecutor(cm.NewExecutorCandidate(face), face)

	require.Same(t, keep, cm.SubagentWrapper("keep"))
	require.Nil(t, cm.SubagentWrapper("drop"),
		"a target removed by the effective generation must stop resolving — that is what makes a relaunch refuse instead of reviving it")

	back := cm.ExecutorConfig()
	back.Tools = []trpctool.Tool{keep, drop}
	cm.PublishExecutor(cm.NewExecutorCandidate(back), back)
	require.Same(t, drop, cm.SubagentWrapper("drop"), "republishing the old topology restores resolvability")
}

// TestSubagentRedispatcher_RefusesTargetNotInEffectiveGeneration 钉住  的拒绝面： 重投解析不到当前代目标时，返回明确错误、不产出 SpawnResult，且**不写任务板**； 当前代仍认得的名字照常投递。
func TestSubagentRedispatcher_RefusesTargetNotInEffectiveGeneration(t *testing.T) {
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	live := relaunchWrapper("live")
	routed := map[string]*AgentToolWrapper{"live": live}
	redispatch := SubagentRedispatcher(func(_ context.Context, name string) (*AgentToolWrapper, *ExecLease, error) {
		return routed[name], nil, nil
	}, tm)

	res, err := redispatch(context.Background(), "retired", "resume work")
	require.Error(t, err, "a target absent from the effective generation must be refused")
	require.Contains(t, err.Error(), "EFFECTIVE orchestration generation",
		"and the message must say WHY (version selection), not just 'not found'")
	require.Contains(t, err.Error(), "retired", "naming the target so the operator can act")
	require.Nil(t, res.Task, "no execution may be created for a refused relaunch (task board untouched)")

	res2, err2 := redispatch(context.Background(), "live", "resume work")
	require.NoError(t, err2, "a still-routed target must relaunch normally")
	require.NotNil(t, res2.Task, "a still-routed target really gets a task")
	require.Equal(t, "live:resume work", res2.Task.Spec.Key, "spawned under the current binding")

	delete(routed, "live")
	_, err3 := redispatch(context.Background(), "live", "resume work again")
	require.Error(t, err3, "removal from the effective face, not the record, decides the outcome")
}

// countingModel answers deterministically and counts how often it served.
type countingModel struct {
	label string
	calls atomic.Int64
	resp  *model.Response
	gate  chan struct{}
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

// TestCrossScenario_AcquiredExecutorIsWhatServesTheTurn 是「并发发布 × 逐回合获取」：断言的不是"服务了某个合法代"，而是更强的每条——每个回合实际服务的模型，恰好是它 BeginTurn 拿到的那台执行器所持有的模型；同时引用计数必须回到零（获取与释放成对）。
func TestCrossScenario_AcquiredExecutorIsWhatServesTheTurn(t *testing.T) {
	base := &countingModel{label: "base", resp: crossResp("served:base")}
	cm := newTestContextManager("xcross", base, nil, make(chan *event.Event, 8192), nil)

	known := &execModel{byEx: map[runner.Runner]*countingModel{}}
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
		release()
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

// TestCrossScenario_InFlightTurnSurvivesRollbackPublish 钉住 「回滚期间在途调用」： turn 在 gen1 上起飞，途中先被 gen2 超代、又被回滚式重发布，该 turn 必须一路用 起飞时那台执行器跑完。
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
	cm.PublishExecutor(mid, face2)
	face3 := cm.ExecutorConfig()
	face3.Model = &countingModel{label: "rollback", resp: crossResp("served:rollback")}
	cm.PublishExecutor(cm.NewExecutorCandidate(face3), face3)

	require.Equal(t, int64(1), mid.closes.Load(),
		"an unreferenced generation is reclaimed independently, not held hostage by an unrelated in-flight turn")
	row := genRow(cm, inFlight)
	require.NotNil(t, row, "the in-flight generation stays on the books until its own reference drops")
	require.False(t, row.Closed, "the executor serving the in-flight turn must not be closed mid-flight")

	refs := cm.ExecutorRefs()
	require.Equal(t, 1, refs.PendingRetirees, "only the executor the in-flight turn actually uses waits")
	require.GreaterOrEqual(t, refs.InFlightTurns, int64(1))

	close(gate)
	require.NoError(t, <-turnErr)
	firstLease.Release()
	require.Equal(t, int64(1), gen1.calls.Load(),
		"the in-flight turn ran on the executor it acquired at turn start — no mid-turn switch")
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 20*time.Millisecond,
		"once the turn ends, its generation is reclaimed too")
	require.Equal(t, int64(1), mid.closes.Load(), "the intermediate generation was closed exactly once in total")
}

// TestCrossScenario_EachSupersededExecutorClosesExactlyOnce 钉住 「多次连续发布后旧代 回收恰一次」：连发 5 代（空闲态）后再发一台收尾，使 5 台全部被超代——每台必须被 我们的 sweep 关闭恰好一次：重复关是多余调用，漏关是泄漏。
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
	cm.PublishExecutor(cm.NewExecutorCandidate(face), face)

	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 20*time.Millisecond)
	for i, c := range wrapped {
		require.Equal(t, int64(1), c.closes.Load(), "superseded executor %d must be closed exactly once", i)
	}
	require.Zero(t, cm.ExecutorRefs().InFlightTurns)
}

// TestSubagentRun_RefusalLeaksNoLiveCM 钉住委托被拒时私有 CM 既不注册也不悬挂。
// - 拒绝分支直接 Close，不经 registerLiveCM：liveCMs 保持零，owner 义务能归零，退役排水不被卡死。
func TestSubagentRun_RefusalLeaksNoLiveCM(t *testing.T) {
	ta, err := NewTagentAgent(&TagentConfig{
		Model:        newRecordableMockModel(gateOKResp()),
		SystemPrompt: "refuser",
		Name:         "refuser",
	})
	require.NoError(t, err)

	require.NoError(t, ta.contextManager.Close())

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationID("refused-inv-1"),
		trpcagent.WithInvocationMessage(model.NewUserMessage("hello")),
	)
	_, err = ta.Run(context.Background(), inv)
	require.ErrorContains(t, err, "invocation refused")
	require.Zero(t, ta.LiveCMCount(), "a refused delegation must not leave a live CM registered")

	rep := ta.Obligations()
	require.Zero(t, rep.Invocations, "refusal must not inflate owner obligations")
}
