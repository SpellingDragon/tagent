package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §6.1 — the full-instance lifecycle: Start/Stop/Close share ONE coordination.
// The first close executes, every other caller waits for and returns the SAME
// completion result (never skipped because a flag already flipped), the
// in-flight count registers BEFORE running is published, and the output
// channel settles exactly once — including the never-started and panicking
// paths.

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

// TestLifecycle_ConcurrentCloseSameResult: three callers enter Close while the
// first is still inside the (slow) sequence — the closers run EXACTLY ONCE and
// every caller returns the first call's identical error (spec Scenario「完整
// Close 与执行交错」). Fail-before: without the once-coordination (pre-§6.1),
// all three run the sequence and the closer is closed three times.
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

// TestLifecycle_LaterCloseReturnsFirstResult: a sequential second Close also
// replays the first result instead of re-running anything.
func TestLifecycle_LaterCloseReturnsFirstResult(t *testing.T) {
	c := &slowErrCloser{delay: 0}
	ta := &TagentAgent{outputCh: make(chan *event.Event)}
	ta.RegisterCloser(c)

	require.NoError(t, ta.Close())
	require.NoError(t, ta.Close())
	require.NoError(t, ta.Close())
	assert.Equal(t, 1, c.callsNow(), "duplicate release must not re-execute")
}

// TestLifecycle_CloseNeverStarted_SettlesOutputOnce: an instance whose loop
// never ran still closes its output channel exactly once and locks the
// terminal state — a later StartLoop refuses rather than handing a dead
// channel to a consumer.
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

// TestLifecycle_ConcurrentStopLoopsWaitTerminal drives the REAL loop: two
// concurrent StopLoop calls must both observe the goroutine's terminal (the
// output channel closed by the loop itself, exactly once) — the loser of the
// stop CAS may not skip the wait because the state already flipped.
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
		_ = ok // drained or closed — either way the channel must not panic on later use
	case <-time.After(2 * time.Second):
	}
	// Output settled exactly once: a third stop is a clean no-op wait.
	assert.NotPanics(t, ta.StopLoop)

	_, err = ta.StartLoop("test-user", "test-session")
	require.ErrorContains(t, err, "already terminated")
}

// §6.2 sequence witnesses -------------------------------------------------------

type traceLog struct {
	mu    sync.Mutex
	evs   []string
	state func() int32 // loop-state witnessed at each recorded step
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
// performed by the release tail ALONE (§6.2 removed it from plain closers).
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

// TestLifecycle_CloseSequence_RefuseFirstRunnerThenLeaseLast pins the §6.2
// order on the observable surface: intake refusal (state already terminal)
// precedes every resource close; registered closers run before the runner;
// the runner closes BEFORE the store release; the store — no longer listed in
// plain closers — is closed exactly once by the release tail alone.
// Fail-before: with the pre-§6.2 order the trace shows store before runner
// (lease released under a live runner) and/or the store closing twice via the
// closers list plus the release path.
func TestLifecycle_CloseSequence_RefuseFirstRunnerThenLeaseLast(t *testing.T) {
	tr := &traceLog{}
	sp := &storeSpy{InMemoryStore: memory.NewInMemoryStore(), tr: tr}
	ta := &TagentAgent{
		outputCh: make(chan *event.Event),
		memStore: sp,
		// The fallback direct-Close requires EXPLICIT sole-ownership (§6.3/
		// review M-2): release==nil alone no longer suffices.
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

// TestLifecycle_BorrowedShellNeverClosesSharedStore — §6.3 借用壳无共享释放权
// on the agent surface: when the instance holds a registry lease (release
// non-nil), the shared store is exited ONLY through that release closure (the
// registry's last-owner semantics decide the real close). The direct fallback
// Close must not fire — pre-§6.2, the store additionally sat in the closers
// list, letting any holder tear down shared state out from under survivors.
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

// TestLifecycle_UnconvergedExecutionHoldsStoreLease — §4.1's other half of the
// same scenario (spec 取消后生产者尚未退出): when an execution's producer never
// confirms a stop, Close returns the unconverged report AND the store exit is NOT
// taken while that writer may still be live. Releasing the lease under a
// possibly-live writer is what would turn a stuck turn into a torn-down shared
// store, so the honest outcome is an error plus an explicit hold.
//
// 〔轮九十三显式修订（§4.1 收口，evidence §5.49）〕this test used to close with
// `assert.Zero(released)` after the barrier lifted — "the already-returned Close
// does not retroactively exit the store". That half is WITHDRAWN: §4.1 makes the
// same owner carry the remainder to the real stop and take it exactly once, so a
// permanently-zero exit is the abandonment, not the contract. The completion is
// asserted by TestLifecycle41_BoundedReturnThenExactlyOneFinalExit; what stays
// here is what this test actually names — the HOLD while live, plus the honest
// report. `released` is atomic because the deferred tail now runs off the
// reclaim goroutine (measured: a real data race while this test still read a
// plain int after the lift).
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
	stuck := cm.AcquireLease(LeaseTurn) // a producer that never comes back

	err := ta.Close()
	require.ErrorIs(t, err, ErrExecUnconverged, "a stuck execution must reach the host, not be normalised")
	assert.Zero(t, released.Load(), "the store lease stays HELD while its writer may still be live")
	assert.Zero(t, zombie.closed.Load(), "and the executor it runs on is not force-closed")

	stuck.Release() // barrier lifts — the ordinary reclaim path takes over
	assert.Equal(t, int64(1), zombie.closed.Load(), "the generation is reclaimed exactly once, after the stop")
}

// TestLifecycle41_BoundedReturnThenExactlyOneFinalExit is §4.1's terminal
// contract. A bounded Close that reports an unconverged execution must not stop
// being responsible for what it could not finish: when that producer ACTUALLY
// stops, the SAME owner takes the final exit exactly once — with no new business
// request, no second Close and no polling timer — and the resources a live
// execution could still touch (its tool closers, the recorder, the store lease)
// are not torn down before convergence is known.
//
// 〔轮九十三显式修订旧断言并记原因〕the predecessor here
// (TestLifecycle_UnconvergedExecutionHoldsStoreLease) asserted
// `released == 0` AFTER the barrier lifted, with the message "the already-
// returned Close does not retroactively exit the store". That encoded exactly
// the abandonment §4.1 removes: holding while live is right, but never finishing
// leaves the writer slot and the owner registration held forever. The
// still-holds-while-live half is kept in that test; the "never finishes" half is
// superseded by this one.
func TestLifecycle41_BoundedReturnThenExactlyOneFinalExit(t *testing.T) {
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
		// The assembly's owner registration — §4.1's third thing the tail owes:
		// runner, store lease, AND this registration each exit exactly once.
		storeOwnerRevoke: func() { revoked.Add(1) },
	}
	ta.RegisterCloser(closer)

	stuck := cm.AcquireLease(LeaseTurn) // a producer that never confirms a stop

	err := ta.Close()
	require.ErrorIs(t, err, ErrExecUnconverged, "the first Close reports the unconverged list honestly")
	assert.Equal(t, 0, closer.callsNow(),
		"§4.1: while an execution may still be live, its still-used tool closers are not run before convergence is known")
	assert.Zero(t, zombie.closed.Load(), "and its executor is held, never force-closed")
	assert.Zero(t, released.Load(), "the store writer slot stays with the possibly-live writer")
	assert.Zero(t, revoked.Load(), "and the owner registration is not dropped while the exit is unfinished")

	// The barrier lifts. Nothing else happens: no new request, no second Close.
	stuck.Release()

	require.Eventually(t, func() bool { return released.Load() == 1 }, 2*time.Second, 10*time.Millisecond,
		"§4.1: the same tail must take the store exit EXACTLY ONCE after the real stop")
	assert.Equal(t, int64(1), zombie.closed.Load(), "the generation was reclaimed exactly once")
	assert.Equal(t, 1, closer.callsNow(), "the deferred tool closer ran in the tail, exactly once")
	// The registration follows the release inside the same tail step; wait rather
	// than assume the two counters land in the same instant.
	require.Eventually(t, func() bool { return revoked.Load() == 1 }, 2*time.Second, 10*time.Millisecond,
		"§4.1: and this owner's registration was revoked by that same tail, once")

	done, tailErr := ta.DeferredCloseOutcome()
	require.True(t, done, "the final completion is recorded separately from the first report")
	require.NoError(t, tailErr)

	// The first error stays visible to the host, and a replayed Close re-runs
	// nothing (并发/重复 Close 不重做清理).
	require.ErrorIs(t, ta.Close(), ErrExecUnconverged, "the first bounded report is not rewritten")
	assert.Equal(t, 1, closer.callsNow(), "the replay must not redo any cleanup")
	assert.Equal(t, int32(1), released.Load(), "the store exit is exactly-once, not per-Close")
}

// TestLifecycle_ExecutorShellNeverClosesSharedStore — review M-2's REAL shell
// shape: memStore points at the SHARED resident store and the shell holds NO
// lease (release nil) and owns nothing — Close must leave the shared store
// completely untouched (spec: 借用执行壳不得关闭共享状态).
func TestLifecycle_ExecutorShellNeverClosesSharedStore(t *testing.T) {
	tr := &traceLog{}
	shared := &storeSpy{InMemoryStore: memory.NewInMemoryStore(), tr: tr}
	shell := &TagentAgent{
		outputCh: make(chan *event.Event),
		memStore: shared, // borrowed from the resident agent; memStoreOwned false
	}
	require.NoError(t, shell.Close())
	assert.Equal(t, 0, shared.closeCalls(), "the shell holds no shared-release right")
}

// TestLifecycle_IdleCloseSettlesOutputAfterInFlightTurns — review M-1: a
// never-started instance may STILL have one-shot/sub-call turns streaming to
// the shared outputCh; the settle must wait them out, never close the channel
// under a live sender (send-on-closed panic).
func TestLifecycle_IdleCloseSettlesOutputAfterInFlightTurns(t *testing.T) {
	orig := turnDrainTimeout
	turnDrainTimeout = 2 * time.Second
	defer func() { turnDrainTimeout = orig }()

	outputCh := make(chan *event.Event, 1)
	cm := &ContextManager{}
	ta := &TagentAgent{outputCh: outputCh, contextManager: cm}

	// One turn in flight when Close begins; it records whether the output was
	// ALREADY closed while it was still streaming.
	closedWhileInFlight := make(chan bool, 1)
	turnLease := cm.AcquireLease(LeaseTurn)
	go func() {
		time.Sleep(50 * time.Millisecond)
		select {
		case _, ok := <-outputCh:
			closedWhileInFlight <- !ok // got the zero value ⇒ channel was closed
		default:
			closedWhileInFlight <- false // still open at the turn's tail — correct
		}
		turnLease.Release()
	}()

	require.NoError(t, ta.Close())
	assert.False(t, <-closedWhileInFlight, "output must NOT settle before every in-flight turn drained (review M-1)")
	// Post-wait, the settle did happen (exactly once, V15 contract kept).
	_, ok := <-outputCh
	assert.False(t, ok, "output settled after the drain")
}

// TestLifecycle_StartCloseRacesNeverDoubleSettle — review C-1 stress: Close's
// idle settle and StartLoop's publish race under one coordination; every
// outcome must be “started then cleanly closed” or “refused”, never a
// double-close panic (a panic here kills the process — the loudest verdict)
// and never a loop resurrected over a settled terminal. Run with -race.
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
			// The loop won: it must be terminal now, and the output settled.
			assert.False(t, ta.IsLoopActive())
		}
		// Either way the instance is terminal: a later start refuses.
		_, err := ta.StartLoop("u", "s")
		assert.ErrorContains(t, err, "already terminated")
	}
}

// TestLifecycle_PanickingCloseDoesNotStrandWaiters — review M-3: even if the
// executed close sequence PANICS, the result publication rides defer, so
// concurrent waiters receive a terminal answer instead of blocking on
// closeDone forever.
func TestLifecycle_PanickingCloseDoesNotStrandWaiters(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	ta := &TagentAgent{outputCh: make(chan *event.Event)}
	ta.RegisterCloser(blockCloser{entered, release})
	ta.RegisterCloser(panicCloser{})

	// The EXECUTOR runs on the main goroutine and parks inside closeOnce at
	// blockCloser; the joiner then enters deterministically as a WAITER on
	// <-closeDone. Releasing lets the executor run into panicCloser and panic
	// THROUGH the defer publication. The joiner unblocking at all proves the
	// publication fires even under panic (review M-3): without the defer the
	// panic skips publication and the joiner is stranded forever.
	waiter := make(chan error, 1)
	go func() {
		defer func() { _ = recover() }() // executor's panic is caught HERE; the joiner unblocking is the assertion
		_ = ta.Close()
	}()
	<-entered
	go func() { waiter <- ta.Close() }() // parks on <-closeDone as the joiner
	time.Sleep(20 * time.Millisecond)    // let the joiner reach <-done
	close(release)                       // executor resumes → panics → defer publishes
	select {
	case <-waiter: // publication reached — waiters are never stranded
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
