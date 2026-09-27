package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"

	"github.com/SpellingDragon/tagent/rl"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// RegisterCloser registers a closer to be called on agent shutdown.
func (ta *TagentAgent) RegisterCloser(c Closer) {
	ta.closers = append(ta.closers, c)
}

// CheckOrgReload runs the armed org-config hot-reload check once (ops/test
// entry point; production arms it per-LLM-call via SetOrgReloader). No-op when
// no reloader is armed (config path unknown).
func (ta *TagentAgent) CheckOrgReload() {
	if ta.contextManager != nil {
		ta.contextManager.CheckOrgReload()
	}
}

// OrgThreshold returns the live compression threshold (introspection for
// tests/ops). It reads the compressor's resolved boundary value — no
// ContextManager mirror (D4/M-3 single source; the mirror was also a
// background-write vs read data race after hot-apply moved to the rebuild goroutine).
func (ta *TagentAgent) OrgThreshold() float64 {
	if ta == nil || ta.contextManager == nil || ta.contextManager.contextCompressor == nil {
		return 0
	}
	return ta.contextManager.contextCompressor.Threshold()
}

// SetOrgReloader arms a lazy org-config check invoked before each LLM call
// (agent-config-hot-reload, incremental A). The tagent layer wires this
// when a config path is known (WithConfigPath). fn must be cheap when
// nothing changed and must never fail the calling path.
//
// D3 (§2.3): production arms a NON-BLOCKING trigger here (schedule a merged
// background rebuild); the synchronous ops path is SetOrgReloadSyncCheck.
func (ta *TagentAgent) SetOrgReloader(fn func()) {
	if ta.contextManager != nil {
		ta.contextManager.SetOrgReloader(fn)
	}
}

// SetOrgReloadSyncCheck arms the synchronous org-reload entry used by ops
// (CheckOrgReload / Rollback semantics): it blocks until the request's build or
// rejection is settled, while business turns keep the non-blocking lazy trigger
// (D3 §2.3, swappable-executor「手动检查同步等待但不封住业务获取」).
func (ta *TagentAgent) SetOrgReloadSyncCheck(fn func()) {
	if ta.contextManager != nil {
		ta.contextManager.SetOrgReloadSyncCheck(fn)
	}
}

// SetTrajectoryRecorder sets the trajectory recorder for this agent.
// When set, StartLoop will automatically call SetSessionInfo on it.
// §6.2: do NOT also RegisterCloser it — closeOnce is its sole close owner,
// flushing AFTER the runner stopped; a closer registration would re-introduce
// double ownership and an early (pre-runner) close.
func (ta *TagentAgent) SetTrajectoryRecorder(tr *rl.TrajectoryRecorder) {
	ta.trajectoryRecorder = tr
}

// TrajectoryRecorder returns the trajectory recorder if one is set, or nil.
func (ta *TagentAgent) TrajectoryRecorder() *rl.TrajectoryRecorder {
	return ta.trajectoryRecorder
}

// §6.1 lifecycle state machine — Start/Stop/Close share ONE coordination so a
// concurrent stop/close never skips the wait for the in-flight loop and never
// runs the close sequence twice. (The close ORDER is documented at closeOnce.)
const (
	loopIdle     int32 = iota // never started on this instance
	loopRunning               // loop goroutine published (Wg registered BEFORE the publish)
	loopStopping              // a caller won the stop CAS and is cancelling+draining
	loopClosed                // terminal: StartLoop/accept refuse; output settled by the close tail
)

// loopTerminatedNow reports the terminal-for-acceptance states: an initiated
// stop is as final as a finished one (inject refuses, V15 semantics kept).
func (ta *TagentAgent) loopTerminatedNow() bool {
	s := ta.loopState.Load()
	return s == loopStopping || s == loopClosed
}

// turnDrainTimeout bounds the wait for in-flight runner turns during close.
// var (not const) so tests can shorten it.
var turnDrainTimeout = 30 * time.Second

// cleanerStopGrace bounds the wait for the workspace cleaner goroutine to return
// after its context was cancelled. It waits on a channel close, so the grace is
// a hang guard, not a polling interval. var so tests can shorten it.
var cleanerStopGrace = 2 * time.Second

// CloseStarted reports whether this instance's close sequence has begun. The
// first Close wins the CAS and later callers wait on its result, so "started"
// means a terminal close owns this instance. Introspection for §4.3/R02's
// witnesses: an org Close must be observable in every resident owner, not just
// the entry.
func (ta *TagentAgent) CloseStarted() bool {
	ta.closeMu.Lock()
	defer ta.closeMu.Unlock()
	return ta.closeStarted
}

// CleanerStopped reports whether this instance's workspace cleaner goroutine has
// returned. Only meaningful together with CloseStarted (the close cancels it);
// the spec asks whether the maintenance producer really converged.
func (ta *TagentAgent) CleanerStopped() bool {
	if ta.cleanupDone == nil {
		return true // no cleaner was ever started on this instance
	}
	select {
	case <-ta.cleanupDone:
		return true
	default:
		return false
	}
}

// settleOutput closes the output channel and the loop terminal signal EXACTLY
// ONCE per instance (review C-1: structurally, not by state-machine
// inference) — safe from the loop goroutine tail and the Close idle branch
// alike.
func (ta *TagentAgent) settleOutput() {
	ta.outputSettle.Do(func() {
		if ta.outputCh != nil {
			close(ta.outputCh)
		}
		if d := ta.loopDone; d != nil {
			close(d)
		}
	})
}

// waitForTurns blocks until every in-flight runner turn — resident AND
// one-shot/sub-call (review M-1: the loop's WaitGroup alone never covered
// those) — drained, warning instead of hanging on a caller-owned context.
func (ta *TagentAgent) waitForTurns() {
	if ta.contextManager == nil {
		return
	}
	if !ta.contextManager.WaitForInFlight(turnDrainTimeout) {
		log.Warnf("[Close] in-flight turns still active after %v — settling anyway (late hook events drop)", turnDrainTimeout)
	}
}

// finishLoop is the loop goroutine's tail: wait for remaining in-flight turns,
// then settle output + terminal signal exactly once. Abnormal (recovered)
// exits take the same path (V15 contract: after StopLoop returns, the channel
// is closed).
func (ta *TagentAgent) finishLoop() {
	ta.waitForTurns()
	ta.settleOutput()
}

// Close shuts the instance down. §6.1 (spec runtime-resource-ownership): the
// FIRST call executes the close sequence; every other caller — concurrent or
// later — waits for and returns the SAME completion result. Closers, leases
// and the store release therefore run exactly once per instance.
func (ta *TagentAgent) Close() (err error) {
	ta.closeMu.Lock()
	if ta.closeStarted {
		done := ta.closeDone
		ta.closeMu.Unlock()
		<-done // the first Close publishes its result before closing done
		return ta.closeErr
	}
	ta.closeStarted = true
	ta.closeDone = make(chan struct{})
	ta.closeMu.Unlock()

	// Review M-3: publish through defer so even a panicking closeOnce cannot
	// strand the waiters forever — closeDone is ALWAYS closed, and the panic
	// keeps propagating to the executing caller.
	defer func() {
		ta.closeMu.Lock()
		ta.closeErr = err
		ta.closeMu.Unlock()
		close(ta.closeDone)
	}()
	err = ta.closeOnce()
	return err
}

// closeOnce is the single executed close sequence in the §6.2 order (spec
// runtime-resource-ownership L7): refuse intake/new calls → stop message
// producers → cancel and wait for ALL in-flight calls → close the runner →
// release the root lease LAST; the trajectory flushes after the runner stopped.
// No resource is closed twice: the store and the recorder have exactly ONE
// owner each (the release path / this sequence), never also plain closers.
func (ta *TagentAgent) closeOnce() error {
	var errs []error

	// (1) Refuse intake / settle the loop terminal. The idle settle runs under
	// sessionMu — the SAME lock StartLoop publishes under — so "Close settled
	// the terminal, then a racing StartLoop resurrected running and its
	// goroutine double-closed the output" is impossible (review C-1). The
	// output itself is NOT settled here: it settles only AFTER every in-flight
	// turn drained (review M-1), via finishLoop / the idle branch below.
	ta.sessionMu.Lock()
	settledIdle := ta.loopState.CompareAndSwap(loopIdle, loopClosed)
	if settledIdle && ta.loopDone == nil {
		ta.loopDone = make(chan struct{}) // written before the state publish below unlocks readers
	}
	ta.sessionMu.Unlock()

	if settledIdle {
		// (2) No producers are live; close the durable receive boundary.
		if ta.persistentBus != nil {
			if err := ta.persistentBus.CloseDurable(); err != nil {
				errs = append(errs, fmt.Errorf("close durable inbox: %w", err))
			}
		}
	} else {
		// (2)+(3) Winner of the stop CAS stops message producers (meditation),
		// cancels and drains the loop; the loop goroutine's finish tail waits for
		// remaining in-flight turns and settles the output exactly once.
		ta.StopLoop()
		// (1 cont.) Close the durable receive boundary only after intake has
		// been refused for long enough that in-flight acks landed — an unacked
		// envelope stays on disk for the next process (shutdown is NOT loss).
		if ta.persistentBus != nil {
			if err := ta.persistentBus.CloseDurable(); err != nil {
				errs = append(errs, fmt.Errorf("close durable inbox: %w", err))
			}
		}
	}

	// Stop the workspace cleaner goroutine (background maintenance producer) and
	// WAIT for it to return: cancelling is a request, and §4.3/R02 asks whether the
	// owner's maintenance goroutine actually converged. A goroutine that ignores
	// its cancellation is reported, never silently counted as stopped.
	if ta.cleanupCancel != nil {
		ta.cleanupCancel()
	}
	if ta.cleanupDone != nil {
		select {
		case <-ta.cleanupDone:
		case <-time.After(cleanerStopGrace):
			errs = append(errs, fmt.Errorf("workspace cleaner still running after %v", cleanerStopGrace))
		}
	}

	if settledIdle {
		// The never-started form must still wait its in-flight one-shot/sub
		// turns BEFORE the output settles (review M-1), then close exactly once.
		ta.waitForTurns()
		ta.settleOutput()
	}

	// §4.1: a resource a still-live execution could touch is never torn down
	// BEFORE the convergence verdict exists. So the verdict is asked first: with
	// work outstanding, the tool closers (ActionTool's monitor, MCP connections)
	// and everything after them move to the tail. With nothing outstanding this
	// is exactly the §6.2 order the converged path always used (closers → runner
	// → lease last), so no converged instance changes behaviour.
	liveWork := ta.contextManager != nil && ta.contextManager.OutstandingRefs() > 0
	if !liveWork {
		// Close registered closers (e.g., ActionTool stops the TmuxMonitor, MCP
		// connections) — AFTER in-flight calls drained, BEFORE the runner: these
		// are tools/connections the runner still uses until it closes.
		for _, c := range ta.closers {
			if err := c.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close resource: %w", err))
			}
		}
	}

	// Close the runner (unified Runner under ContextManager) — no new turns
	// can start after this point. The close is BOUNDED: an execution whose
	// producer never confirmed a stop is reported, not force-closed (§4.1), and
	// the flag below is what keeps its shared store out of reach of that zombie.
	var unconverged bool
	if ta.contextManager != nil {
		if err := ta.contextManager.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close context manager: %w", err))
			unconverged = errors.Is(err, ErrExecUnconverged)
		}
	}

	if liveWork || unconverged {
		// §4.1's terminal half: the bounded return reported what it could not
		// finish, but the responsibility does not end there. The remaining exit
		// (tool closers that a live execution still uses, the recorder, the store
		// lease and its owner registration) is carried by ONE continuation, armed
		// on the manager's own reclaim event, and runs when the producer actually
		// stops — no new business request, no second Close, no timer, no retry
		// queue. The first report stays visible; the final outcome is recorded
		// separately (DeferredCloseOutcome).
		ta.deferFinalExit(liveWork)
		if len(errs) > 0 {
			return fmt.Errorf("close errors: %w", errors.Join(errs...))
		}
		return nil
	}

	// Close TrajectoryRecorder AFTER the runner stopped (§6.2): the writeLoop
	// flushes what the final turns recorded, no further LLM calls exist.
	if ta.trajectoryRecorder != nil {
		if err := ta.trajectoryRecorder.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close trajectory recorder: %w", err))
		}
	}

	// LAST — release the root lease (§6.2/§6.3): an agent holding a registry
	// lease releases it (the last lease closes store + engine); borrowed shells
	// hold NO close right over shared state (§6.3/review M-2: only an agent
	// that OWNS its isolated store may direct-close); leased holders exit only
	// through their release. The release's close error reaches this first Close
	// (D5: never claim a safe close silently).
	if err := ta.exitMemoryStore(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// Join, never %v on the slice: §4.1's caller must be able to ASK whether
		// the close left an unconverged execution (errors.Is) so it can act on it.
		// A flattened string message makes the classification — and therefore the
		// honest「未确认停止」handling — unavailable to the host.
		return fmt.Errorf("close errors: %w", errors.Join(errs...))
	}
	return nil
}

// exitMemoryStore takes the store's single exit: through the lease release when
// one exists (the registry's last-owner semantics decide the real close), or a
// direct close only for an instance that OWNS an isolated store with no closer
// elsewhere. Taking the exit also revokes this owner's store-owner registration,
// so a later reuse of the same storage identity is not refused by a stale entry.
func (ta *TagentAgent) exitMemoryStore() error {
	if ta.memStoreRelease != nil {
		if err := ta.memStoreRelease(); err != nil {
			return fmt.Errorf("release memory store: %w", err)
		}
		ta.revokeStoreOwner() // §4.3: the exit was really taken, so the assembly may forget this owner
		return nil
	}
	if ta.memStoreOwned {
		if c, ok := ta.memStore.(interface{ Close() error }); ok {
			if err := c.Close(); err != nil {
				return fmt.Errorf("close memory store: %w", err)
			}
			ta.revokeStoreOwner()
			return nil
		}
		ta.revokeStoreOwner() // no closer to run: nothing is left open under this owner
	}
	return nil
}

// deferFinalExit arms the one continuation that carries what this bounded Close
// could not honestly finish. `carryClosers` marks the case where tool closers
// were deliberately NOT run because an execution was still live; the recorder
// and the store exit always move here. The arm runs the tail immediately if the
// manager is already quiet, so a race between this decision and the last reclaim
// can never strand the responsibility.
func (ta *TagentAgent) deferFinalExit(carryClosers bool) {
	log.Warnf("[Close] bounded return with execution(s) still unconfirmed — tool/recorder/store exit deferred to the real stop (final exit will be taken exactly once by this owner's tail)")
	ta.closeMu.Lock()
	if ta.closeTail != nil {
		ta.closeMu.Unlock()
		return // a continuation is already armed; at most one per owner
	}
	ta.closeTail = func() {
		ta.closeTailOnce.Do(func() {
			var errs []error
			if carryClosers {
				for _, c := range ta.closers {
					if err := c.Close(); err != nil {
						errs = append(errs, fmt.Errorf("close resource: %w", err))
					}
				}
			}
			if ta.trajectoryRecorder != nil {
				if err := ta.trajectoryRecorder.Close(); err != nil {
					errs = append(errs, fmt.Errorf("close trajectory recorder: %w", err))
				}
			}
			if err := ta.exitMemoryStore(); err != nil {
				errs = append(errs, err)
			}
			ta.closeMu.Lock()
			ta.closeTailDone = true
			ta.closeTailErr = errors.Join(errs...)
			ta.closeMu.Unlock()
			if ta.closeTailErr != nil {
				log.Errorf("[Close] deferred final exit reported: %v — the first report stays visible too", ta.closeTailErr)
				return
			}
			log.Infof("[Close] deferred final exit completed exactly once after the executions actually stopped")
		})
	}
	ta.closeMu.Unlock()

	if ta.contextManager != nil {
		ta.contextManager.armFullyDrained(func() {
			ta.closeMu.Lock()
			tail := ta.closeTail
			ta.closeMu.Unlock()
			if tail != nil {
				go tail() // off the reclaim path: a tool Close may block on its own I/O
			}
		})
	}
}

// DeferredCloseOutcome reports the §4.1 terminal tail's result separately from
// the first bounded report: done=false until the deferred exit has run (or the
// close completed inline and never deferred anything). The first Close's error
// is deliberately NOT rewritten by it — 「初次错误保持可见」.
func (ta *TagentAgent) DeferredCloseOutcome() (done bool, err error) {
	// Read the manager's accounting BEFORE taking closeMu: nothing here holds a
	// ContextManager lock while acquiring closeMu, so the reclaim path (which
	// takes closeMu to record the outcome) can never invert the order.
	quiet := ta.contextManager == nil || ta.contextManager.OutstandingRefs() == 0
	ta.closeMu.Lock()
	defer ta.closeMu.Unlock()
	if quiet && ta.closeTail == nil {
		// Nothing was ever deferred: an inline close that took every exit has no
		// tail to wait for, and reporting it as "not done" would be a lie.
		return true, nil
	}
	return ta.closeTailDone, ta.closeTailErr
}

// SetBundleIDProvider wires the active-bundle lookup used by both event
// persistence paths to stamp bundle_id into FullEvent.Metadata
// (D1-B, design-report-closeout). Entry-only; nil/no-active -> no stamp.
func (ta *TagentAgent) SetBundleIDProvider(fn func() string) {
	if ta == nil || ta.contextManager == nil {
		return
	}
	ta.contextManager.bundleIDFn = fn
}

// AppendProjectionRef appends an EventReference to this agent's session
// projection (5.5, design-report-closeout): used by the mem_spill replay
// double-write so replayed events restore the store⇔projection invariant.
// nil-safe.
func (ta *TagentAgent) AppendProjectionRef(ref memory.EventReference) {
	if ta == nil || ta.contextManager == nil || ta.contextManager.projection == nil {
		return
	}
	ta.contextManager.projection.Append(ref)
}

// StartLoop starts the persistent event loop for this agent.
// The loop runs in a dedicated goroutine and processes events until StopLoop is called.
// Returns the output channel that emits events as they are processed.
// The channel is closed exactly once, when the loop goroutine exits.
// StopLoop is TERMINAL: a second StartLoop on the same instance returns an
// error — the closed outputCh makes silent restart a production panic (V15).
// Creates a new TagentAgent for a fresh loop.
func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error) {
	// Use sessionMu to prevent concurrent StartLoop calls from racing
	// on the state check + initialization sequence.
	ta.sessionMu.Lock()
	switch ta.loopState.Load() {
	case loopRunning:
		ch := ta.outputCh
		ta.sessionMu.Unlock()
		return ch, nil
	case loopStopping, loopClosed:
		// Terminal lifecycle (V15): after StopLoop the outputCh is closed — a
		// silent restart would hand consumers a dead channel and double-close it
		// on the next Stop. Fail explicitly instead.
		ta.sessionMu.Unlock()
		return nil, fmt.Errorf("persistent loop already terminated: StopLoop is terminal on a TagentAgent instance; create a new agent for a fresh loop")
	}

	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())
	if ta.loopDone == nil {
		ta.loopDone = make(chan struct{})
	}
	// §6.1 (design L151): the in-flight registration (loopWg.Add) and the
	// terminal channel are recorded BEFORE publishing loopRunning — a
	// concurrent StopLoop can never observe running without the count, so its
	// Wait can never start before the Add (the race detector treats
	// Add-after-Wait as a hard violation, and a reviewer stress caught exactly
	// this when Add sat next to the go statement). The publish itself is a CAS
	// under the SAME sessionMu the Close idle-settle uses, so a racing Close can
	// never be resurrected over a settled terminal (review C-1).
	ta.loopWg.Add(1)
	if !ta.loopState.CompareAndSwap(loopIdle, loopRunning) {
		ta.loopWg.Add(-1) // defensive: unreachable while both flips hold sessionMu
		ta.sessionMu.Unlock()
		return nil, fmt.Errorf("persistent loop already terminated: StopLoop is terminal on a TagentAgent instance; create a new agent for a fresh loop")
	}
	ta.sessionMu.Unlock()

	// Cache session context for event injection.
	ta.setSessionContext(userID, sessionID)

	// Create or attach session for the persistent loop.
	sess := ta.getOrCreateSession(sessionID)
	_ = sess // session managed by ContextManager's Runner

	// Update ContextManager with session context.
	ta.contextManager.SetUserIDSessionID(userID, sessionID)

	// Set TrajectoryRecorder session info (if enabled).
	if ta.trajectoryRecorder != nil {
		ta.trajectoryRecorder.SetSessionInfo(userID, sessionID)
	}

	// Launch runEventLoop in a dedicated goroutine. The in-flight count was
	// registered under sessionMu, BEFORE the running publish above.
	go func() {
		defer ta.loopWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[StartLoop] runEventLoop panic recovered: %v", r)
			}
			// Abnormal exit still terminates correctly (§6.1/V15): drain the
			// remaining turns, then settle output + terminal exactly once.
			ta.finishLoop()
		}()
		ta.runEventLoop(ta.loopCtx, ta.persistentBus, ta.contextManager)
	}()

	// Start meditation manager (if configured).
	if ta.meditationMgr != nil {
		ta.meditationMgr.Start()
	}

	log.Infof("[StartLoop] persistent event loop started user=%s session=%s", userID, sessionID)
	return ta.outputCh, nil
}

// StopLoop stops the persistent event loop. §6.1: the CAS winner cancels and
// drains; every other caller — a second StopLoop, a Close arriving mid-stop —
// waits for the SAME terminal instead of skipping because the flag already
// reads false (spec: 不因 active 已变 false 跳过等待).
func (ta *TagentAgent) StopLoop() {
	if !ta.loopState.CompareAndSwap(loopRunning, loopStopping) {
		switch ta.loopState.Load() {
		case loopIdle:
			return // never started: nothing to stop (Close settles the terminal)
		case loopStopping, loopClosed:
			if done := ta.loopDone; done != nil {
				<-done // the in-flight stopper publishes the terminal
			}
			return
		}
	}
	// Winner: stop injecting new meditation events first, then cancel + drain.
	if ta.meditationMgr != nil {
		ta.meditationMgr.Stop()
	}
	ta.loopCancel()
	ta.loopWg.Wait()
	ta.loopState.Store(loopClosed)
	log.Infof("[StopLoop] persistent event loop stopped")
}

// IsLoopActive returns true if the persistent event loop is currently running.
func (ta *TagentAgent) IsLoopActive() bool {
	return ta.loopState.Load() == loopRunning
}

// submitStatus classifies the outcome of the §4.2 durable submit gate so the event
// loop acts per design 决策4 / spec persistent-event-loop L90: commit-and-model on
// OK, isolate-and-stop on a deterministic conflict, ordered bounded backoff on a
// transient I/O failure, retain-and-exit on cancellation.
type submitStatus int

const (
	submitOK        submitStatus = iota // every selected fact prepared AND stored → model may run
	submitTransient                     // retryable I/O — re-attempt the SAME batch, no model, no next batch
	submitConflict                      // deterministic format/identity conflict — conflicting input isolated
	submitCancelled                     // context cancelled mid-submit — claims retained, no model
)

type submitOutcome struct {
	status   submitStatus
	conflict string // path of the isolated conflicting envelope (when status==submitConflict)
}

// submitDurableBatch is the §4.2 durable submit gate. It first runs the write-before
// prepare barrier for the WHOLE batch (freezing every claimed envelope's canonical
// prepared facts + a reserved receipt_key) and only then stores each selected fact.
// It returns a CLASSIFIED outcome instead of a bool, so a caller never treats "the
// first input" as standing for the batch:
//   - submitOK: all selected prepared AND stored — the model may run.
//   - submitConflict: a deterministic identity/format conflict — the offending
//     envelope is isolated (quarantined); the caller stops auto-consumption.
//   - submitTransient: a retryable I/O failure — NOTHING was committed; the caller
//     re-attempts the same batch with bounded backoff.
//   - submitCancelled: context cancelled mid-submit; claims are retained.
//
// Batch all-or-nothing (§3.6-①): no fact is stored unless EVERY envelope prepared,
// so a partially-prepared input never enters the fact chain. Volatile (claim-less)
// events are a no-op OK (their handling is unchanged).
func (ta *TagentAgent) submitDurableBatch(ctx context.Context, received, selected []*AgentEvent) submitOutcome {
	if ta.persistentBus == nil || ta.contextManager == nil {
		return submitOutcome{status: submitOK}
	}
	cm := ta.contextManager

	// Phase 1 — write-before prepare barrier across the WHOLE frozen received set,
	// before ANY store. §5.3 closes a latent §4.1 gap: prepare reserves each
	// envelope's receipt key + freezes every slot's canonical fact (design 决策2 L74
	// 「未执行的槽同样可有准备事实，但不提交」), INCLUDING slots that will be
	// filtered out (a yielding meditation). A yielding envelope therefore has a
	// reserved key so §5.3 can freeze a per-slot completion for it; only the SELECTED
	// facts are committed in Phase 2. Prepare stays all-or-nothing over received (spec
	// L121-122: any envelope's prepare/dir barrier failing starts no fact writes).
	status, conflictPath := ta.prepareBatchFacts(received)
	if status == submitConflict {
		ta.persistentBus.QuarantineEnvelope(conflictPath, "deterministic prepare conflict (§4.2) — isolated, not retried")
		return submitOutcome{status: submitConflict, conflict: conflictPath}
	}
	if status == submitTransient {
		return submitOutcome{status: submitTransient}
	}
	if err := ctx.Err(); err != nil {
		return submitOutcome{status: submitCancelled}
	}

	// Phase 2 — commit only the SELECTED facts. A store failure is transient I/O (the
	// fact is idempotent-by-key, so a re-attempt is safe); it gates the model. Filtered
	// (yielding-meditation) slots keep their prepared fact on the envelope but are never
	// committed to the chain here — they get a skipped disposition in the completion.
	for _, ev := range selected {
		if ev == nil || ev.claim == nil || ev.claim.Path == "" {
			continue // volatile — nothing durable to submit
		}
		if ok, deterministic := cm.persistBusEventCommitted(ev); !ok {
			if deterministic {
				// §8.5 four-state closure: retrying a deterministic store
				// conflict is a livelock (the frozen key NEVER changes) — the
				// envelope is ISOLATED through the same quarantine exit the
				// prepare-conflict path uses, and auto-consumption stops.
				ta.persistentBus.QuarantineEnvelope(ev.claim.Path,
					"deterministic store conflict (§8.5): the frozen fact key collides with different content or is forgotten — isolated, never retried")
				log.Errorf("[submitDurableBatch] deterministic store conflict rid=%s slot=%d — envelope isolated, batch stopped",
					ev.claim.RequestID, ev.claim.Slot)
				return submitOutcome{status: submitConflict, conflict: ev.claim.Path}
			}
			log.Warnf("[submitDurableBatch] durable store failed rid=%s slot=%d — transient, batch not committed, model gated",
				ev.claim.RequestID, ev.claim.Slot)
			return submitOutcome{status: submitTransient}
		}
		if err := ctx.Err(); err != nil {
			return submitOutcome{status: submitCancelled}
		}
	}
	return submitOutcome{status: submitOK}
}

// prepareBatchFacts runs the D2 write-before barrier (task 3.4) for the whole batch:
// for every claimed envelope it freezes the canonical prepared_fact for each slot AND
// reserves a receipt_key in ONE durable rewrite BEFORE the first fact is stored. An
// already-prepared slot (crash replay) is reused verbatim — never re-derived — so
// attribution/time/summary never restamp. Events without a durable claim are ignored.
// It returns submitOK when every envelope is fully prepared; submitConflict + the path
// on a deterministic format/identity conflict (caller isolates it); submitTransient on
// a retryable I/O failure. A fact that cannot be marshalled is deterministic
// corruption, not transient, so it is classified as a conflict.
func (ta *TagentAgent) prepareBatchFacts(events []*AgentEvent) (submitStatus, string) {
	if ta.persistentBus == nil || ta.contextManager == nil {
		return submitOK, ""
	}
	cm := ta.contextManager
	// Group claims by envelope, preserving first-seen order (deterministic).
	byPath := map[string][]*AgentEvent{}
	var order []string
	for _, ev := range events {
		if ev == nil || ev.claim == nil || ev.claim.Path == "" {
			continue
		}
		if _, ok := byPath[ev.claim.Path]; !ok {
			order = append(order, ev.claim.Path)
		}
		byPath[ev.claim.Path] = append(byPath[ev.claim.Path], ev)
	}
	for _, path := range order {
		group := byPath[path]
		// Slot count spans the max fixed slot index (slots are never compacted,
		// F4). A gap left by a dropped slot yields a nil fact, which PrepareFacts
		// treats as "keep existing" — safe, and the envelope only converges once
		// every slot is frozen.
		nslots := 0
		receiptKey := ""
		needBuild := false
		for _, ev := range group {
			if ev.claim.Slot+1 > nslots {
				nslots = ev.claim.Slot + 1
			}
			if ev.claim.ReceiptKey != "" {
				receiptKey = ev.claim.ReceiptKey
			}
			if len(ev.claim.PreparedFact) == 0 {
				needBuild = true
			}
		}
		if !needBuild {
			continue // fully-prepared replay: nothing to freeze, reuse verbatim
		}
		if receiptKey == "" {
			receiptKey = tagentevent.FormatEventKey(memory.NewSnowflakeEventKey(cm.partitionID, 0))
		}
		facts := make([]json.RawMessage, nslots)
		for _, ev := range group {
			if len(ev.claim.PreparedFact) > 0 {
				continue // keep the frozen slot (nil = leave as-is)
			}
			b, err := json.Marshal(cm.buildBusFact(ev))
			if err != nil {
				log.Errorf("[prepareBatchFacts] marshal failed rid=%s slot=%d — deterministic, isolating: %v",
					ev.claim.RequestID, ev.claim.Slot, err)
				return submitConflict, path
			}
			facts[ev.claim.Slot] = b
		}
		if err := ta.persistentBus.PrepareEnvelope(path, receiptKey, facts); err != nil {
			if errors.Is(err, reliability.ErrPrepareConflict) || errors.Is(err, reliability.ErrReceiptKeyConflict) {
				log.Warnf("[prepareBatchFacts] deterministic conflict for %s — isolating, stopping: %v", path, err)
				return submitConflict, path
			}
			log.Warnf("[prepareBatchFacts] transient prepare failure for %s (claim stays, no fact written): %v", path, err)
			return submitTransient, ""
		}
		// The freeze succeeded — stamp the durable bytes back onto the live
		// claims so persistBusEvent commits the EXACT frozen fact this turn.
		for _, ev := range group {
			if len(ev.claim.PreparedFact) == 0 {
				ev.claim.PreparedFact = facts[ev.claim.Slot]
			}
			ev.claim.ReceiptKey = receiptKey
		}
	}
	return submitOK, ""
}

// submitDurableBatchWithBackoff runs the §4.2 submit gate with the spec's bounded
// in-turn backoff (100/200/400ms) for a transient I/O failure, re-attempting the SAME
// batch under the SAME identity — never the next batch, never the model — until it
// commits, hits a deterministic conflict, is cancelled, or the backoff budget is spent
// (spec persistent-event-loop L90 「I/O 失败以 100/200/400ms 退避，不从下批取输入」).
func (ta *TagentAgent) submitDurableBatchWithBackoff(ctx context.Context, received, selected []*AgentEvent) submitOutcome {
	delays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	outcome := ta.submitDurableBatch(ctx, received, selected)
	for attempt := 0; outcome.status == submitTransient && attempt < len(delays); attempt++ {
		log.Warnf("[runEventLoop:%s] transient submit failure — backing off %v (attempt %d/%d), same batch, not the next",
			ta.name, delays[attempt], attempt+1, len(delays))
		select {
		case <-time.After(delays[attempt]):
		case <-ctx.Done():
			return submitOutcome{status: submitCancelled}
		}
		outcome = ta.submitDurableBatch(ctx, received, selected)
	}
	return outcome
}

// releaseBatchClaims returns every durable claim in the batch to pending so the
// oldest stuck envelope is re-claimed in strict order on the next Pull. Used on an
// exhausted transient backoff to preserve ordering + bounded backpressure without
// ever acking/dropping an uncommitted input (§4.2).
func (ta *TagentAgent) releaseBatchClaims(events []*AgentEvent) {
	if ta.persistentBus == nil {
		return
	}
	seen := map[string]bool{}
	for _, ev := range events {
		if ev == nil || ev.claim == nil || ev.claim.Path == "" || seen[ev.claim.Path] {
			continue
		}
		seen[ev.claim.Path] = true
		if err := ta.persistentBus.ReleaseClaim(ev.claim.Path); err != nil {
			log.Warnf("[runEventLoop:%s] release claim %s failed: %v", ta.name, ev.claim.RequestID, err)
		}
	}
}

// finishDurableBatch (§5.3/§5.4) runs the two-phase completion protocol over the
// frozen received set. For each consumed envelope: ① freeze its completion DURABLY
// (RecordCompletion, design 决策5 L62/D3 step 7) BEFORE any receipt; ② verify the
// frozen receipt fact is on the chain under its RESERVED key and take the verified
// receipt credential (§5.4 — an illegal completion or uncommitted receipt yields NO
// credential); ③ only then RecordReceipt(cred) + Ack (ConfirmDurable). A failure at
// any phase retains the claim (never an unbacked ack); the RESULT WRITE is retried
// in-process without re-running the model, because the completion already carries the
// turn's result (spec L132-134: 「模型已结束但首次 completion 写入失败 → 同进程只
// 重试结果提交」). A cancelled turn forms no completion at all (§5.1) and its claims
// stay. Each slot's canonical fact was frozen at prepare over the WHOLE received set
// (决策2 L74), so even a filtered (yielding-meditation) envelope has a reserved key +
// prepared fact to freeze a per-slot skipped completion for — closing the §4.1
// ack-scope-vs-prepare-scope gap.
func (ta *TagentAgent) finishDurableBatch(ctx context.Context, received, selected []*AgentEvent, outcome turnOutcome) {
	if ta == nil || ta.persistentBus == nil || ta.contextManager == nil {
		return
	}
	if _, _, ok := batchResultFromOutcome(outcome); !ok {
		return // cancelled turn: no completion, claims retained (§5.1 returns first; defensive)
	}
	cm := ta.contextManager
	completedAt := time.Now().UnixMilli() // captured ONCE → every envelope's freeze is deterministic this turn
	attribution := cm.buildTurnAttribution(ctx)
	paths, byPath := groupClaimsByPath(received)
	committed := selectedKeySet(selected)
	for _, path := range paths {
		group := byPath[path]
		c, completion, err := buildEnvelopeCompletion(group, committed, outcome, ta.name, cm.partitionID, completedAt, attribution)
		if err != nil {
			log.Errorf("[finishDurableBatch] completion freeze failed rid=%s path=%s — claim held, NOT acked: %v",
				group[0].claim.RequestID, path, err)
			continue
		}
		// Phase A — durable completion BEFORE receipt, with bounded in-process retry
		// of the WRITE only (the model already ran; its result is in the completion).
		if err := ta.recordCompletionWithRetry(path, completion); err != nil {
			log.Warnf("[finishDurableBatch] completion not durable rid=%s path=%s after retry — claim held, receipt+ack deferred: %v",
				c.RequestID, path, err)
			continue
		}
		// Phase B — verify the receipt fact on the chain under its RESERVED key
		// (idempotent-by-key, never a fresh key/time) and take the §5.4 credential.
		// Failure keeps the claim; the durable completion lets §5.7 re-submit ONLY
		// the receipt on restart (no re-execution). A receipt failure never touches
		// the input's claim/prepared facts — receipt and input failure don't mask
		// each other.
		cred, verr := cm.verifyReceiptCredential(completion)
		if verr != nil {
			log.Warnf("[finishDurableBatch] receipt unverified rid=%s key=%s — completion durable, claim held (§5.7 re-submits receipt, no re-run): %v",
				c.RequestID, c.ReceiptKey, verr)
			continue
		}
		// Phase C — RecordReceipt (on the verified credential) + Ack.
		if err := ta.persistentBus.ConfirmDurable(path, cred); err != nil {
			log.Warnf("[finishDurableBatch] confirm for %s deferred (replay will Ack-skip): %v", c.RequestID, err)
		}
	}
}

// recordCompletionWithRetry durably freezes a completion, retrying the WRITE on a
// transient I/O failure with the same bounded, cancellable backoff as the submit gate
// (§4.2). The frozen bytes never change (deterministic freeze), so an identical retry
// converges idempotently at the inbox; a completion-conflict is a DEFINITE error (the
// already-durable completion is authoritative) and is NOT retried. Returns nil once the
// freeze is durable, or an error after the budget is spent (caller holds the claim).
func (ta *TagentAgent) recordCompletionWithRetry(path string, completion json.RawMessage) error {
	delays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	err := ta.persistentBus.RecordCompletion(path, completion)
	for attempt := 0; err != nil && !errors.Is(err, reliability.ErrCompletionConflict) && attempt < len(delays); attempt++ {
		log.Warnf("[finishDurableBatch] completion write retry %d/%d for %s: %v", attempt+1, len(delays), path, err)
		time.Sleep(delays[attempt])
		err = ta.persistentBus.RecordCompletion(path, completion)
	}
	return err
}
