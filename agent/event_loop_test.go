// 本文件负责生命周期收敛：关闭序列按序执行且单点报错不中断其余、并发关闭返回首次结果，
// 以及写入者未确认停止时 `Close` 必须报错并保持持有底层存储。
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// closeTrackingCloser tracks when Close() is called for testing close ordering.
type closeTrackingCloser struct {
	mu        sync.Mutex
	closed    bool
	closeTime time.Time
	closeErr  error
}

func (m *closeTrackingCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.closeTime = time.Now()
	return m.closeErr
}

func (m *closeTrackingCloser) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func TestTagentAgent_CloseWithClosers(t *testing.T) {
	closer1 := &closeTrackingCloser{}
	closer2 := &closeTrackingCloser{}

	ta := &TagentAgent{
		closers: []Closer{closer1, closer2},
	}

	if closer1.isClosed() || closer2.isClosed() {
		t.Fatal("nothing should be closed before Close()")
	}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if !closer1.isClosed() {
		t.Error("closer1 should be closed")
	}
	if !closer2.isClosed() {
		t.Error("closer2 should be closed")
	}
}

func TestTagentAgent_CloseWithNoClosers(t *testing.T) {
	ta := &TagentAgent{closers: nil}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

func TestTagentAgent_CloseCloserError(t *testing.T) {
	failingCloser := &closeTrackingCloser{closeErr: fmt.Errorf("closer failed")}

	ta := &TagentAgent{
		closers: []Closer{failingCloser},
	}

	err := ta.Close()
	if err == nil {
		t.Fatal("expected error from failing closer")
	}
}

func TestTagentAgent_RegisterCloser(t *testing.T) {
	ta := &TagentAgent{}

	if len(ta.closers) != 0 {
		t.Fatalf("expected 0 closers, got %d", len(ta.closers))
	}

	closer := &closeTrackingCloser{}
	ta.RegisterCloser(closer)

	if len(ta.closers) != 1 {
		t.Fatalf("expected 1 closer, got %d", len(ta.closers))
	}

	if err := ta.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if !closer.isClosed() {
		t.Error("registered closer should be closed")
	}
}

type slowErrCloser struct {
	mu    sync.Mutex
	calls int
	err   error
	delay time.Duration
}

func (c *slowErrCloser) Close() error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	time.Sleep(c.delay)
	return c.err
}

func (c *slowErrCloser) callsNow() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestLifecycle_ConcurrentCloseSameResult 钉住 三个调用者并发进入 Close 时，关闭序列恰好执行一次，且每个调用者都拿回首次调用的同一错误。
func TestLifecycle_ConcurrentCloseSameResult(t *testing.T) {
	boom := errors.New("closer boom")
	c := &slowErrCloser{err: boom, delay: 80 * time.Millisecond}
	ta := &TagentAgent{outputCh: make(chan *event.Event)}
	ta.RegisterCloser(c)

	const n = 3
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs[i] = ta.Close() }(i)
	}
	wg.Wait()

	assert.Equal(t, 1, c.callsNow(), "the close sequence must execute exactly once")
	for i, e := range errs {
		require.Error(t, e, "caller %d must see the close error", i)
		assert.Contains(t, e.Error(), boom.Error(), "caller %d gets the SAME completion result", i)
	}
}

// TestLifecycle_LaterCloseReturnsFirstResult 钉住 a sequential second Close also replays the first result instead of re-running anything.
func TestLifecycle_LaterCloseReturnsFirstResult(t *testing.T) {
	c := &slowErrCloser{delay: 0}
	ta := &TagentAgent{outputCh: make(chan *event.Event)}
	ta.RegisterCloser(c)

	require.NoError(t, ta.Close())
	require.NoError(t, ta.Close())
	require.NoError(t, ta.Close())
	assert.Equal(t, 1, c.callsNow(), "duplicate release must not re-execute")
}

// TestLifecycle_CloseNeverStarted_SettlesOutputOnce 钉住 从未跑过事件循环的实例仍恰好关闭一次输出通道，并锁定终态。
// - 之后再启动必须被拒绝，而不是把一条死通道交给消费者。
func TestLifecycle_CloseNeverStarted_SettlesOutputOnce(t *testing.T) {
	ta := &TagentAgent{outputCh: make(chan *event.Event)}
	require.NoError(t, ta.Close())

	select {
	case _, ok := <-ta.outputCh:
		assert.False(t, ok, "Close on a never-started instance must still settle the output")
	default:
		t.Fatal("output channel left open")
	}
	_, err := ta.StartLoop("u", "s")
	require.ErrorContains(t, err, "already terminated", "the terminal is locked in")
	assert.False(t, ta.IsLoopActive())
}

// TestLifecycle_ConcurrentStopLoopsWaitTerminal 钉住 两个并发停止都必须观察到该协程真正终结：输出通道由循环自身恰好关闭一次。
// - 在比较交换中落败的一方不得因为状态已翻就跳过等待。
func TestLifecycle_ConcurrentStopLoopsWaitTerminal(t *testing.T) {
	ta := newLoopTestAgent(t)
	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)
	require.True(t, ta.IsLoopActive())

	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "lifecycle race"})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); ta.StopLoop() }()
	}
	wg.Wait()

	assert.False(t, ta.IsLoopActive())
	select {
	case _, ok := <-outputCh:
		_ = ok
	case <-time.After(2 * time.Second):
	}
	assert.NotPanics(t, ta.StopLoop)

	_, err = ta.StartLoop("test-user", "test-session")
	require.ErrorContains(t, err, "already terminated")
}

type traceLog struct {
	mu    sync.Mutex
	evs   []string
	state func() int32
}

func (t *traceLog) add(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != nil {
		name = fmt.Sprintf("%s@state%d", name, t.state())
	}
	t.evs = append(t.evs, name)
}

type traceCloser struct {
	name string
	tr   *traceLog
}

func (c *traceCloser) Close() error { c.tr.add(c.name); return nil }

// runnerSpy witnesses the runner-close step of the sequence.
type runnerSpy struct {
	tr *traceLog
}

func (r *runnerSpy) Run(context.Context, string, string, model.Message, ...upagent.RunOption) (<-chan *event.Event, error) {
	return nil, errors.New("runnerSpy: not runnable")
}
func (r *runnerSpy) Close() error { r.tr.add("runner"); return nil }

// storeSpy is an isolated (unleased) store: its Close must be the LAST step,
// performed by the release tail ALONE.
type storeSpy struct {
	*memory.InMemoryStore
	mu    sync.Mutex
	tr    *traceLog
	calls int
}

func (s *storeSpy) Close() error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	s.tr.add("store")
	return nil
}
func (s *storeSpy) closeCalls() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }

// TestLifecycle_CloseSequence_RefuseFirstRunnerThenLeaseLast 钉住 关闭次序：入口拒绝先于一切资源关闭，closers 先于 runner，runner 先于 store 释放；store 只由释放尾链恰好关闭一次。
func TestLifecycle_CloseSequence_RefuseFirstRunnerThenLeaseLast(t *testing.T) {
	tr := &traceLog{}
	sp := &storeSpy{InMemoryStore: memory.NewInMemoryStore(), tr: tr}
	ta := &TagentAgent{
		outputCh:      make(chan *event.Event),
		memStore:      sp,
		memStoreOwned: true,
	}
	tr.state = func() int32 { return ta.loopState.Load() }
	ta.contextManager = &ContextManager{runner: &runnerSpy{tr: tr}}
	ta.RegisterCloser(&traceCloser{name: "tool", tr: tr})

	require.NoError(t, ta.Close())

	tr.mu.Lock()
	evs := append([]string(nil), tr.evs...)
	tr.mu.Unlock()
	require.Equal(t, []string{
		fmt.Sprintf("tool@state%d", loopClosed),
		fmt.Sprintf("runner@state%d", loopClosed),
		fmt.Sprintf("store@state%d", loopClosed),
	}, evs, "refusal first, closers → runner → lease release LAST")

	assert.Equal(t, 1, sp.closeCalls(), "the store has exactly one close owner — it is not in the closers list")
}

// TestLifecycle_BorrowedShellNeverClosesSharedStore 钉住 借用壳不持有共享释放权：实例持有注册表租约时，共享 store 只能经该释放闭包退出，直接的 Close 兜底不得触发。
func TestLifecycle_BorrowedShellNeverClosesSharedStore(t *testing.T) {
	tr := &traceLog{}
	sp := &storeSpy{InMemoryStore: memory.NewInMemoryStore(), tr: tr}
	released := 0
	ta := &TagentAgent{
		outputCh: make(chan *event.Event),
		memStore: sp,
		memStoreRelease: func() error {
			released++
			tr.add("release")
			return nil
		},
	}
	require.NoError(t, ta.Close())
	assert.Equal(t, 1, released, "the lease release is the store's exit route")
	assert.Equal(t, 0, sp.closeCalls(), "a leased holder never direct-closes the shared store")
}

// TestLifecycle_UnconvergedExecutionHoldsStoreLease 钉住 生产者未确认停止时，Close 必须返回未收敛报告，且在该写入者可能仍活着的期间绝不退出 store——租约要显式持有，诚实结果是"报错＋保持持有"（released 为原子量：收尾尾巴运行在回收协程上）。
func TestLifecycle_UnconvergedExecutionHoldsStoreLease(t *testing.T) {
	drain, grace := turnDrainTimeout, execCloseGrace
	turnDrainTimeout, execCloseGrace = 50*time.Millisecond, 50*time.Millisecond
	defer func() { turnDrainTimeout, execCloseGrace = drain, grace }()

	zombie := &countingRunner{}
	cm := &ContextManager{name: "zombie-owner", runner: zombie}
	var released atomic.Int32
	ta := &TagentAgent{
		outputCh:       make(chan *event.Event, 1),
		contextManager: cm,
		memStore:       memory.NewInMemoryStore(),
		memStoreRelease: func() error {
			released.Add(1)
			return nil
		},
	}
	stuck := cm.AcquireLease(LeaseTurn)

	err := ta.Close()
	require.ErrorIs(t, err, ErrExecUnconverged, "a stuck execution must reach the host, not be normalised")
	assert.Zero(t, released.Load(), "the store lease stays HELD while its writer may still be live")
	assert.Zero(t, zombie.closed.Load(), "and the executor it runs on is not force-closed")

	stuck.Release()
	assert.Equal(t, int64(1), zombie.closed.Load(), "the generation is reclaimed exactly once, after the stop")
}

// TestBoundedReturnThenExactlyOneFinalExit 钉住 有界关闭报出未收敛执行时，仍要对未完成的部分负责。
// - 生产者真正停止后，由同一属主恰好执行一次最终退出：不需要新请求、第二次关闭或轮询定时器；
// - 收敛未知之前，活动执行仍可能触及的资源（工具 closer、记录器、存储租约）不得被拆掉。
func TestBoundedReturnThenExactlyOneFinalExit(t *testing.T) {
	drain, grace := turnDrainTimeout, execCloseGrace
	turnDrainTimeout, execCloseGrace = 50*time.Millisecond, 50*time.Millisecond
	defer func() { turnDrainTimeout, execCloseGrace = drain, grace }()

	zombie := &countingRunner{}
	cm := &ContextManager{name: "tail-owner", runner: zombie}
	var released atomic.Int32
	var revoked atomic.Int32
	closer := &slowErrCloser{}
	ta := &TagentAgent{
		outputCh:       make(chan *event.Event, 1),
		contextManager: cm,
		memStore:       memory.NewInMemoryStore(),
		memStoreRelease: func() error {
			released.Add(1)
			return nil
		},
		storeOwnerRevoke: func() { revoked.Add(1) },
	}
	ta.RegisterCloser(closer)

	stuck := cm.AcquireLease(LeaseTurn)

	err := ta.Close()
	require.ErrorIs(t, err, ErrExecUnconverged, "the first Close reports the unconverged list honestly")
	assert.Equal(t, 0, closer.callsNow(),
		"§4.1: while an execution may still be live, its still-used tool closers are not run before convergence is known")
	assert.Zero(t, zombie.closed.Load(), "and its executor is held, never force-closed")
	assert.Zero(t, released.Load(), "the store writer slot stays with the possibly-live writer")
	assert.Zero(t, revoked.Load(), "and the owner registration is not dropped while the exit is unfinished")

	stuck.Release()

	require.Eventually(t, func() bool { return released.Load() == 1 }, 2*time.Second, 10*time.Millisecond,
		"§4.1: the same tail must take the store exit EXACTLY ONCE after the real stop")
	assert.Equal(t, int64(1), zombie.closed.Load(), "the generation was reclaimed exactly once")
	assert.Equal(t, 1, closer.callsNow(), "the deferred tool closer ran in the tail, exactly once")
	require.Eventually(t, func() bool { return revoked.Load() == 1 }, 2*time.Second, 10*time.Millisecond,
		"§4.1: and this owner's registration was revoked by that same tail, once")

	done, tailErr := ta.DeferredCloseOutcome()
	require.True(t, done, "the final completion is recorded separately from the first report")
	require.NoError(t, tailErr)

	require.ErrorIs(t, ta.Close(), ErrExecUnconverged, "the first bounded report is not rewritten")
	assert.Equal(t, 1, closer.callsNow(), "the replay must not redo any cleanup")
	assert.Equal(t, int32(1), released.Load(), "the store exit is exactly-once, not per-Close")
}

// TestLifecycle_ExecutorShellNeverClosesSharedStore 钉住 壳指向共享常驻存储、不持租约也不拥有任何东西时，关闭必须完全不触碰共享存储。
func TestLifecycle_ExecutorShellNeverClosesSharedStore(t *testing.T) {
	tr := &traceLog{}
	shared := &storeSpy{InMemoryStore: memory.NewInMemoryStore(), tr: tr}
	shell := &TagentAgent{
		outputCh: make(chan *event.Event),
		memStore: shared,
	}
	require.NoError(t, shell.Close())
	assert.Equal(t, 0, shared.closeCalls(), "the shell holds no shared-release right")
}

// TestLifecycle_IdleCloseSettlesOutputAfterInFlightTurns 钉住 从未启动过的实例仍可能有子调用回合在向共享输出通道投递：收尾必须等它们结束。
// - 绝不在仍有发送者时关闭通道——那只会把丢数据换成一次崩溃。
func TestLifecycle_IdleCloseSettlesOutputAfterInFlightTurns(t *testing.T) {
	orig := turnDrainTimeout
	turnDrainTimeout = 2 * time.Second
	defer func() { turnDrainTimeout = orig }()

	outputCh := make(chan *event.Event, 1)
	cm := &ContextManager{}
	ta := &TagentAgent{outputCh: outputCh, contextManager: cm}

	closedWhileInFlight := make(chan bool, 1)
	turnLease := cm.AcquireLease(LeaseTurn)
	go func() {
		time.Sleep(50 * time.Millisecond)
		select {
		case _, ok := <-outputCh:
			closedWhileInFlight <- !ok
		default:
			closedWhileInFlight <- false
		}
		turnLease.Release()
	}()

	require.NoError(t, ta.Close())
	assert.False(t, <-closedWhileInFlight, "output must NOT settle before every in-flight turn drained (review M-1)")
	_, ok := <-outputCh
	assert.False(t, ok, "output settled after the drain")
}

// TestLifecycle_StartCloseRacesNeverDoubleSettle 钉住 空闲收尾与启动发布在同一协调下竞争时，每种结果只能是启动后干净关闭或被拒绝。
// - 绝不出现二次关闭 panic，也不允许循环在已终结之后复活；本用例在竞态检测下运行。
func TestLifecycle_StartCloseRacesNeverDoubleSettle(t *testing.T) {
	for i := 0; i < 100; i++ {
		ta := newLoopTestAgent(t)
		var wg sync.WaitGroup
		var startErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, startErr = ta.StartLoop("u", "s") }()
		go func() { defer wg.Done(); _ = ta.Close() }()
		wg.Wait()
		if startErr == nil {
			assert.False(t, ta.IsLoopActive())
		}
		_, err := ta.StartLoop("u", "s")
		assert.ErrorContains(t, err, "already terminated")
	}
}

// TestLifecycle_PanickingCloseDoesNotStrandWaiters 钉住 即便关闭序列发生 panic，结果的发布也挂在延迟调用上。
// - 并发等待者因此拿到终局答复，而不是永久阻塞在关闭完成信号上。
func TestLifecycle_PanickingCloseDoesNotStrandWaiters(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	ta := &TagentAgent{outputCh: make(chan *event.Event)}
	ta.RegisterCloser(blockCloser{entered, release})
	ta.RegisterCloser(panicCloser{})

	waiter := make(chan error, 1)
	go func() {
		defer func() { _ = recover() }()
		_ = ta.Close()
	}()
	<-entered
	go func() { waiter <- ta.Close() }()
	time.Sleep(20 * time.Millisecond)
	close(release)
	select {
	case <-waiter:
	case <-time.After(3 * time.Second):
		t.Fatal("waiter stranded: a panicking close never published closeDone (review M-3)")
	}
}

type panicCloser struct{}

func (panicCloser) Close() error { panic("closer exploded") }

// blockCloser parks inside closeOnce until released, giving the joiner a
// deterministic window to enter as a WAITER (first-caller-runs semantics).
type blockCloser struct {
	entered chan struct{}
	release chan struct{}
}

func (b blockCloser) Close() error {
	close(b.entered)
	<-b.release
	return nil
}

func setNotice(cm *ContextManager, s string) {
	cm.recoveryMu.Lock()
	cm.recoveryNotice = s
	cm.recoveryMu.Unlock()
}

func peekNotice(cm *ContextManager) string {
	cm.recoveryMu.Lock()
	defer cm.recoveryMu.Unlock()
	return cm.recoveryNotice
}

func countCarried(reqs []*model.Request) int {
	n := 0
	for _, r := range reqs {
		for _, m := range r.Messages {
			if strings.HasPrefix(m.Content, "[recovery]") {
				n++
				break
			}
		}
	}
	return n
}

// failingCaptureModel records the request then reports a provider failure —
// the 「模型收到调用后失败」 shape (the call was ENTERED; the error follows).
type failingCaptureModel struct {
	mu       sync.Mutex
	requests []*model.Request
}

func (m *failingCaptureModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return nil, errors.New("provider down")
}
func (m *failingCaptureModel) Info() model.Info { return model.Info{Name: "failing-capture"} }
func (m *failingCaptureModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func drainRunner(out <-chan *event.Event, limit time.Duration) {
	deadline := time.After(limit)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-deadline:
			return
		}
	}
}

// TestNotice_ShortCircuitAndCancel_StaysPending 钉住 装配完成却从未进入被包装模型的请求，必须把通告留在待取状态。
// - 调用方取消、或后续回调在进入模型之前短路时，没有任何东西被消费；下一次真实调用才恰好带出一次。
func TestNotice_ShortCircuitAndCancel_StaysPending(t *testing.T) {
	g, inner, cm := newGate(t)
	setNotice(cm, "[recovery] pending after short circuit")

	_, cancel := context.WithCancel(context.Background())
	cancel()

	require.Equal(t, "[recovery] pending after short circuit", peekNotice(cm),
		"no underlying call ⇒ no consumption")
	require.Equal(t, 0, inner.requestCount())

	_, _ = g.GenerateContent(context.Background(), plainReq())
	require.Equal(t, 1, countCarried(inner.snapshotRequests()), "next real call carries the preserved notice")
	require.Empty(t, peekNotice(cm), "consumed on that real call")
}

// TestNotice_UnderlyingFailure_ConsumedNoResend 钉住 进入被包装的模型即算消费：随后供应方失败也不重发。
// - 诊断仍可解读，后续调用绝不重复该通告——消费边界就是进入模型这一步。
func TestNotice_UnderlyingFailure_ConsumedNoResend(t *testing.T) {
	fail := &failingCaptureModel{}
	cm := newTestContextManager("nt-fail", &loopMockModel{}, nil, nil, nil)
	g := newExecutionGateModel(fail, cm)
	setNotice(cm, "[recovery] failed-provider case")
	cm.recoveryMu.Lock()
	cm.recovery = &RecoveryResult{Mode: "snapshot", Status: "partial", Truncated: 3}
	cm.recoveryMu.Unlock()

	_, err := g.GenerateContent(context.Background(), plainReq())
	require.Error(t, err, "the underlying failure surfaces to the caller")
	require.Empty(t, peekNotice(cm), "entering the model consumed the notice despite provider failure")
	require.Equal(t, 1, countCarried(fail.snapshot()))

	_, _ = g.GenerateContent(context.Background(), plainReq())
	require.Equal(t, 1, countCarried(fail.snapshot()), "no resend after the underlying already saw it")
	require.NotNil(t, cm.RecoveryResult(), "diagnostics remain queryable after consumption")
}

// TestNotice_ConcurrentCalls_ExactlyOneCarries 钉住 — spec L35: concurrent calls against ONE resident recovery state consume at most once.
func TestNotice_ConcurrentCalls_ExactlyOneCarries(t *testing.T) {
	g, inner, cm := newGate(t)
	setNotice(cm, "[recovery] raced once")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = g.GenerateContent(context.Background(), plainReq())
		}()
	}
	wg.Wait()

	require.Equal(t, 8, inner.requestCount(), "all callers reach the model")
	require.Equal(t, 1, countCarried(inner.snapshotRequests()), "at most ONE concurrent call may carry the notice")
}

// TestNotice_RealRequest_VolatileRunner 钉住 走真实框架路径的那条易变支路：装配指令与历史、跑完回调之后，门在请求最尾部追加。
// - 通告恰可见一次、系统头部不动，且绝不残留在会话历史里；
// - 判据是行为性的：同一会话再跑一次会重放历史，其后的请求不得再带上它。
func TestNotice_RealRequest_VolatileRunner(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("nt-real", capture, nil, make(chan *event.Event, 16), nil)
	setNotice(cm, "[recovery] real-request tail")

	r := cm.currentRunner()
	require.NotNil(t, r)
	out, err := r.Run(context.Background(), "test-user", "s-1", model.NewUserMessage("hello world"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)

	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs, "the real runner must reach the model")
	first := reqs[len(reqs)-1]
	require.Contains(t, first.Messages[0].Content, "[context-guard]",
		"precondition: assembly-callback material present before the gate")
	require.True(t, strings.HasPrefix(first.Messages[len(first.Messages)-1].Content, "[recovery]"),
		"notice appended AFTER all assembled material on the real request")
	require.Equal(t, 1, countCarried(reqs), "visible exactly once")

	out2, err := r.Run(context.Background(), "test-user", "s-1", model.NewUserMessage("second turn"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	require.Equal(t, 1, countCarried(capture.snapshotRequests()),
		"the notice never becomes session history — later turns carry no copy")
}

// TestNotice_HotRebuild_StateContinues 钉住 换代执行器只换执行面（新的受控模型与 runner），恢复状态留在常驻上下文管理器上。
// - 待投递通告由新执行面恰好带出一次：既不重新挂起，也不重复；
// - 一次运行等于一次底层调用，不存在双重包装。
func TestNotice_HotRebuild_StateContinues(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("nt-hot", capture, nil, make(chan *event.Event, 16), nil)

	r2 := cm.PublishExecutor(cm.NewExecutorCandidate(cm.ExecutorConfig()), cm.ExecutorConfig())
	require.NotNil(t, r2)
	setNotice(cm, "[recovery] survives hot rebuild")

	out, err := r2.Run(context.Background(), "test-user", "s-2", model.NewUserMessage("after rebuild"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)

	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs)
	require.Equal(t, 1, countCarried(reqs), "the rebuilt executor consumes the resident notice exactly once")
	require.Empty(t, peekNotice(cm))

	out2, err := r2.Run(context.Background(), "test-user", "s-2", model.NewUserMessage("second"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	reqs2 := capture.snapshotRequests()
	require.Equal(t, 1, countCarried(reqs2), "never re-carried after consumption")
	require.Len(t, reqs2, 2, "one Run = exactly one underlying model call (no double gate wrapping)")
}

// TestNotice_DurableTurn_RealRequestCarriesOnce 钉住 持久一回合走完整提交面：回合流经属主主循环、框架与门。
// - 被包装模型实际看到的请求恰带通告一次，下一回合绝不重复；
// - 装配路径必须与生产一致（构造完整上下文管理器后建 agent 再套门），否则这条只测到桩件。
func TestNotice_DurableTurn_RealRequestCarriesOnce(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	bus := NewEventBus()
	ta := newDurableAgentWithStore("nt-durable", capture, memory.NewInMemoryStore(),
		make(chan *event.Event, 16), bus)
	cm := ta.contextManager
	cred := &plugin.EchoCredential{MergedMessage: "hello durable"}
	cred.Bind("inv-root")
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	setNotice(cm, "[recovery] durable turn notice")
	require.NoError(t, cm.RunFlow(ctx, model.NewUserMessage("hello durable")))
	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs, "the durable submission surface must reach the model")
	require.Equal(t, 1, countCarried(reqs), "the durable real request carries the notice exactly once")

	require.NoError(t, cm.RunFlow(ctx, model.NewUserMessage("second durable turn")))
	require.Equal(t, 1, countCarried(capture.snapshotRequests()), "never re-carried on later durable turns")
}

// TestNotice_DurableChain_FactsReceiptAndRestartClean 钉住 恢复通告绝不进入输入事实、收件箱回执或完成记录。
// - 断言对着独立重开做（同一目录上的新存储实例），不读缓存回取；
// - 全部事实通过却从未到达模型时，通告保持未消费：没有调用就不算消费。
// 契约: docs/wiki/platform/reincarnation-notice.md#consumption
func TestNotice_DurableChain_FactsReceiptAndRestartClean(t *testing.T) {
	root := t.TempDir()
	dirStore, dirInbox := root+"/store", root+"/inbox"
	kvStore, err := kv.NewLocalFileKV(dirStore)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, dirStore, 100)
	require.NoError(t, err)
	bus, err := NewReliableEventBus(dirInbox)
	require.NoError(t, err)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "nt-restart", persistentBus: bus, contextManager: cm}
	setNotice(cm, "[recovery] must never become a fact")

	_, err = bus.PublishContext(context.Background(), durableMsg("durable-input"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	for _, e := range batch {
		require.True(t, cm.persistBusEvent(e))
	}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending())

	require.Equal(t, "[recovery] must never become a fact", peekNotice(cm))

	assertNoNoticeOnChain := func(t *testing.T, s memory.MemoryStore, phase string) {
		refs, qerr := s.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 100})
		require.NoError(t, qerr)
		require.NotEmpty(t, refs, phase)
		for _, r := range refs {
			full, gerr := s.GetEvent(r.EventKey)
			require.NoError(t, gerr, phase)
			require.NotContains(t, full.Content, "[recovery]",
				"§7.4: fact/receipt content must never carry the transient notice (%s)", phase)
			require.NotContains(t, fmt.Sprintf("%v", full.Metadata), "[recovery]",
				"§7.4: no notice text in event metadata (%s)", phase)
		}
	}
	assertNoNoticeOnChain(t, store, "live instance")

	kv2, err := kv.NewLocalFileKV(dirStore)
	require.NoError(t, err)
	store2, err := memory.NewFileSegmentStore(kv2, nil, dirStore, 100)
	require.NoError(t, err)
	assertNoNoticeOnChain(t, store2, "independent reopen")
	require.NoError(t, store2.Close())
}
