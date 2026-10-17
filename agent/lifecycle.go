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
// D3: production arms a NON-BLOCKING trigger here (schedule a merged
// background rebuild); the synchronous ops path is SetOrgReloadSyncCheck.
func (ta *TagentAgent) SetOrgReloader(fn func()) {
	if ta.contextManager != nil {
		ta.contextManager.SetOrgReloader(fn)
	}
}

// SetOrgReloadSyncCheck arms the synchronous org-reload entry used by ops
// (CheckOrgReload / Rollback semantics): it blocks until the request's build or
// rejection is settled, while business turns keep the non-blocking lazy trigger
// .
func (ta *TagentAgent) SetOrgReloadSyncCheck(fn func()) {
	if ta.contextManager != nil {
		ta.contextManager.SetOrgReloadSyncCheck(fn)
	}
}

// SetTrajectoryRecorder sets the trajectory recorder for this agent.
// When set, StartLoop will automatically call SetSessionInfo on it.
// : do NOT also RegisterCloser it — closeOnce is its sole close owner,
// flushing AFTER the runner stopped; a closer registration would re-introduce
// double ownership and an early (pre-runner) close.
func (ta *TagentAgent) SetTrajectoryRecorder(tr *rl.TrajectoryRecorder) {
	ta.trajectoryRecorder = tr
}

// TrajectoryRecorder returns the trajectory recorder if one is set, or nil.
func (ta *TagentAgent) TrajectoryRecorder() *rl.TrajectoryRecorder {
	return ta.trajectoryRecorder
}

// loopIdle lifecycle state machine — Start/Stop/Close share ONE coordination so a
//
// concurrent stop/close never skips the wait for the in-flight loop and never
// runs the close sequence twice. (The close ORDER is documented at closeOnce.)
const (
	loopIdle int32 = iota
	loopRunning
	loopStopping
	loopClosed
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
// means a terminal close owns this instance. Introspection for /R02's
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
		return true
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

// Close shuts the instance down. : the
// FIRST call executes the close sequence; every other caller — concurrent or
// later — waits for and returns the SAME completion result. Closers, leases
// and the store release therefore run exactly once per instance.
func (ta *TagentAgent) Close() (err error) {
	ta.closeMu.Lock()
	if ta.closeStarted {
		done := ta.closeDone
		ta.closeMu.Unlock()
		<-done
		return ta.closeErr
	}
	ta.closeStarted = true
	ta.closeDone = make(chan struct{})
	ta.closeMu.Unlock()

	defer func() {
		ta.closeMu.Lock()
		ta.closeErr = err
		ta.closeMu.Unlock()
		close(ta.closeDone)
	}()
	err = ta.closeOnce()
	return err
}

// closeOnce is the single executed close sequence in the  order (spec
// runtime-resource-ownership L7): refuse intake/new calls → stop message
// producers → cancel and wait for ALL in-flight calls → close the runner →
// release the root lease LAST; the trajectory flushes after the runner stopped.
// No resource is closed twice: the store and the recorder have exactly ONE
// owner each (the release path / this sequence), never also plain closers.
func (ta *TagentAgent) closeOnce() error {
	var errs []error
	ta.beginOwnedShutdown()
	ctx, cancel := context.WithTimeout(context.Background(), turnDrainTimeout)
	defer cancel()
	if err := ta.waitOwnedShutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	carryClosers := !ta.producersStopped() || !ta.executionWorkQuiet()
	if !carryClosers {
		for _, c := range ta.closers {
			if err := c.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close resource: %w", err))
			}
		}
	}
	if ta.contextManager != nil {
		ta.contextManager.retireForOwnerClose()
	}
	grace, cancelGrace := context.WithTimeout(context.Background(), execCloseGrace)
	defer cancelGrace()
	if !ta.waitFinalExitReady(grace) {
		errs = append(errs, fmt.Errorf("%w: private drivers, producers or execution faces remain held", ErrExecUnconverged))
		ta.deferFinalExit(carryClosers)
		return errors.Join(errs...)
	}
	errs = append(errs, ta.finishOwnedExit(carryClosers))
	return errors.Join(errs...)
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
		ta.revokeStoreOwner()
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
		ta.revokeStoreOwner()
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
	log.Warnf("[Close] execution shutdown remains unconfirmed; final exit stays with this owner")
	ta.closeMu.Lock()
	if ta.closeTail != nil {
		ta.closeMu.Unlock()
		return
	}
	ta.closeTail = func() {
		if !ta.finalExitReady() {
			return
		}
		ta.closeTailOnce.Do(func() {
			err := ta.finishOwnedExit(carryClosers)
			ta.closeMu.Lock()
			ta.closeTailDone, ta.closeTailErr = true, err
			ta.closeMu.Unlock()
			if err != nil {
				log.Errorf("[Close] deferred final exit reported: %v", err)
			}
		})
	}
	ta.closeMu.Unlock()
	if ta.contextManager != nil {
		ta.contextManager.armFullyDrained(ta.tryDeferredFinalExit)
	}
	ta.tryDeferredFinalExit()
}

// DeferredCloseOutcome reports the  terminal tail's result separately from
// the first bounded report: done=false until the deferred exit has run (or the
// close completed inline and never deferred anything). The first Close's error
// is deliberately NOT rewritten by it — 「初次错误保持可见」.
func (ta *TagentAgent) DeferredCloseOutcome() (done bool, err error) {
	ta.closeMu.Lock()
	defer ta.closeMu.Unlock()
	if ta.closeTail == nil && ta.closeFinalDone != nil {
		select {
		case <-ta.closeFinalDone:
			return true, nil
		default:
		}
	}
	return ta.closeTailDone, ta.closeTailErr
}

// SetBundleIDProvider wires the active-bundle lookup used by both event
// persistence paths to stamp bundle_id into FullEvent.Metadata
// . Entry-only; nil/no-active -> no stamp.
func (ta *TagentAgent) SetBundleIDProvider(fn func() string) {
	if ta == nil || ta.contextManager == nil {
		return
	}
	ta.contextManager.bundleIDFn = fn
}

// AppendProjectionRef appends an EventReference to this agent's session
// projection: used by the mem_spill replay
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
	ta.sessionMu.Lock()
	switch ta.loopState.Load() {
	case loopRunning:
		ch := ta.outputCh
		ta.sessionMu.Unlock()
		return ch, nil
	case loopStopping, loopClosed:
		ta.sessionMu.Unlock()
		return nil, fmt.Errorf("persistent loop already terminated: StopLoop is terminal on a TagentAgent instance; create a new agent for a fresh loop")
	}

	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())
	if ta.loopDone == nil {
		ta.loopDone = make(chan struct{})
	}
	ta.loopWg.Add(1)
	if !ta.loopState.CompareAndSwap(loopIdle, loopRunning) {
		ta.loopWg.Add(-1)
		ta.sessionMu.Unlock()
		return nil, fmt.Errorf("persistent loop already terminated: StopLoop is terminal on a TagentAgent instance; create a new agent for a fresh loop")
	}
	ta.sessionMu.Unlock()

	ta.setSessionContext(userID, sessionID)

	sess := ta.getOrCreateSession(sessionID)
	_ = sess

	ta.contextManager.SetUserIDSessionID(userID, sessionID)

	if ta.trajectoryRecorder != nil {
		ta.trajectoryRecorder.SetSessionInfo(userID, sessionID)
	}

	go func() {
		defer ta.loopWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[StartLoop] runEventLoop panic recovered: %v", r)
			}
			ta.finishLoop()
		}()
		ta.runEventLoop(ta.loopCtx, ta.persistentBus, ta.contextManager)
	}()

	if ta.meditationMgr != nil {
		ta.meditationMgr.Start()
	}

	log.Infof("[StartLoop] persistent event loop started user=%s session=%s", userID, sessionID)
	return ta.outputCh, nil
}

// StopLoop stops the persistent event loop. : the CAS winner cancels and
// drains; every other caller — a second StopLoop, a Close arriving mid-stop —
// waits for the SAME terminal instead of skipping because the flag already
// reads false.
func (ta *TagentAgent) StopLoop() {
	if !ta.loopState.CompareAndSwap(loopRunning, loopStopping) {
		switch ta.loopState.Load() {
		case loopIdle:
			return
		case loopStopping, loopClosed:
			if done := ta.loopDone; done != nil {
				<-done
			}
			return
		}
	}
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

// submitStatus classifies the outcome of the durable submit gate so the event
// loop acts per one of four verdicts: commit-and-model on OK, isolate-and-stop
// on a deterministic conflict, ordered bounded backoff on a transient I/O
// failure, retain-and-exit on cancellation.
// 契约: docs/wiki/reliability/durable-delivery.md#release-claim-backoff
type submitStatus int

const (
	submitOK submitStatus = iota
	submitTransient
	submitConflict
	submitCancelled
)

type submitOutcome struct {
	status   submitStatus
	conflict string
}

// submitDurableBatch is the durable submit gate: it runs the write-before prepare barrier
// for the WHOLE batch and only then stores each selected fact, returning a classified
// outcome so a caller never treats one input as standing for the batch.
//
// - submitOK lets the model run; submitConflict isolates the offending envelope and stops auto-consumption; submitTransient committed nothing and is retried with bounded backoff; submitCancelled returns the claims.
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func (ta *TagentAgent) submitDurableBatch(ctx context.Context, received, selected []*AgentEvent) submitOutcome {
	return ta.submitDurableBatchForCM(ctx, ta.contextManager, received, selected)
}

func (ta *TagentAgent) submitDurableBatchForCM(ctx context.Context, cm *ContextManager, received, selected []*AgentEvent) submitOutcome {
	bus := ta.batchBusForCM(cm)
	if bus == nil || cm == nil {
		return submitOutcome{status: submitOK}
	}

	status, conflictPath := ta.prepareBatchFactsForCM(cm, received)
	if status == submitConflict {
		bus.QuarantineEnvelope(conflictPath, "deterministic prepare conflict (§4.2) — isolated, not retried")
		return submitOutcome{status: submitConflict, conflict: conflictPath}
	}
	if status == submitTransient {
		return submitOutcome{status: submitTransient}
	}
	if err := ctx.Err(); err != nil {
		return submitOutcome{status: submitCancelled}
	}

	for _, ev := range selected {
		if ev == nil || ev.claim == nil || ev.claim.Path == "" {
			continue
		}
		if ok, deterministic := cm.persistBusEventCommitted(ev); !ok {
			if deterministic {
				bus.QuarantineEnvelope(ev.claim.Path,
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
	return ta.prepareBatchFactsForCM(ta.contextManager, events)
}

func (ta *TagentAgent) prepareBatchFactsForCM(cm *ContextManager, events []*AgentEvent) (submitStatus, string) {
	bus := ta.batchBusForCM(cm)
	if bus == nil || cm == nil {
		return submitOK, ""
	}
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
			continue
		}
		if receiptKey == "" {
			receiptKey = tagentevent.FormatEventKey(memory.NewSnowflakeEventKey(cm.partitionID, 0))
		}
		facts := make([]json.RawMessage, nslots)
		for _, ev := range group {
			if len(ev.claim.PreparedFact) > 0 {
				continue
			}
			if ev.volatileFact == nil {
				fact := cm.buildBusFact(ev)
				ev.volatileFact = &fact
			}
			b, err := json.Marshal(*ev.volatileFact)
			if err != nil {
				log.Errorf("[prepareBatchFacts] marshal failed rid=%s slot=%d — deterministic, isolating: %v",
					ev.claim.RequestID, ev.claim.Slot, err)
				return submitConflict, path
			}
			facts[ev.claim.Slot] = b
		}
		if err := bus.PrepareEnvelope(path, receiptKey, facts); err != nil {
			if errors.Is(err, reliability.ErrPrepareConflict) || errors.Is(err, reliability.ErrReceiptKeyConflict) {
				log.Warnf("[prepareBatchFacts] deterministic conflict for %s — isolating, stopping: %v", path, err)
				return submitConflict, path
			}
			log.Warnf("[prepareBatchFacts] transient prepare failure for %s (claim stays, no fact written): %v", path, err)
			return submitTransient, ""
		}
		for _, ev := range group {
			if len(ev.claim.PreparedFact) == 0 {
				ev.claim.PreparedFact = facts[ev.claim.Slot]
			}
			ev.claim.ReceiptKey = receiptKey
		}
	}
	return submitOK, ""
}

// submitDurableBatchWithBackoff runs the  submit gate with the spec's bounded
// in-turn backoff (100/200/400ms) for a transient I/O failure, re-attempting the SAME
// batch under the SAME identity — never the next batch, never the model — until it
// commits, hits a deterministic conflict, is cancelled, or the backoff budget is spent
// .
func (ta *TagentAgent) submitDurableBatchWithBackoff(ctx context.Context, received, selected []*AgentEvent) submitOutcome {
	return ta.submitDurableBatchWithBackoffForCM(ctx, ta.contextManager, received, selected)
}

func (ta *TagentAgent) submitDurableBatchWithBackoffForCM(ctx context.Context, cm *ContextManager, received, selected []*AgentEvent) submitOutcome {
	delays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	outcome := ta.submitDurableBatchForCM(ctx, cm, received, selected)
	for attempt := 0; outcome.status == submitTransient && attempt < len(delays); attempt++ {
		log.Warnf("[runEventLoop:%s] transient submit failure — backing off %v (attempt %d/%d), same batch, not the next",
			ta.name, delays[attempt], attempt+1, len(delays))
		select {
		case <-time.After(delays[attempt]):
		case <-ctx.Done():
			return submitOutcome{status: submitCancelled}
		}
		outcome = ta.submitDurableBatchForCM(ctx, cm, received, selected)
	}
	return outcome
}

// releaseBatchClaims returns every durable claim in the batch to pending so the
// oldest stuck envelope is re-claimed in strict order on the next Pull. Used on an
// exhausted transient backoff to preserve ordering + bounded backpressure without
// ever acking/dropping an uncommitted input.
func (ta *TagentAgent) releaseBatchClaims(events []*AgentEvent) {
	ta.releaseBatchClaimsForCM(ta.contextManager, events)
}

func (ta *TagentAgent) releaseBatchClaimsForCM(cm *ContextManager, events []*AgentEvent) {
	bus := ta.batchBusForCM(cm)
	if bus == nil {
		return
	}
	seen := map[string]bool{}
	for _, ev := range events {
		if ev == nil || ev.claim == nil || ev.claim.Path == "" || seen[ev.claim.Path] {
			continue
		}
		seen[ev.claim.Path] = true
		if err := bus.ReleaseClaim(ev.claim.Path); err != nil {
			log.Warnf("[runEventLoop:%s] release claim %s failed: %v", ta.name, ev.claim.RequestID, err)
		}
	}
}

// finishDurableBatch runs the two-phase completion protocol over the
// frozen received set. For each consumed envelope: ① freeze its completion DURABLY
// BEFORE any receipt; ② verify the
// frozen receipt fact is on the chain under its RESERVED key and take the verified
// receipt credential ( — an illegal completion or uncommitted receipt yields NO
// credential); ③ only then RecordReceipt(cred) + Ack (ConfirmDurable). A failure at
// any phase retains the claim (never an unbacked ack); the RESULT WRITE is retried
// in-process without re-running the model, because the completion already carries the
// turn's result (spec L132-134: 「模型已结束但首次 completion 写入失败 → 同进程只
// 重试结果提交」). A cancelled turn forms no completion at all and its claims
// stay. Each slot's canonical fact was frozen at prepare over the WHOLE received set
// (决策2 L74), so even a filtered (yielding-meditation) envelope has a reserved key +
// prepared fact to freeze a per-slot skipped completion for — closing the
// ack-scope-vs-prepare-scope gap.
func (ta *TagentAgent) finishDurableBatch(ctx context.Context, received, selected []*AgentEvent, outcome turnOutcome) {
	ta.finishDurableBatchForCM(ctx, ta.contextManager, received, selected, outcome)
}

func (ta *TagentAgent) finishDurableBatchForCM(ctx context.Context, cm *ContextManager, received, selected []*AgentEvent, outcome turnOutcome) bool {
	bus := ta.batchBusForCM(cm)
	if ta == nil || bus == nil || cm == nil {
		return true
	}
	if _, _, ok := batchResultFromOutcome(outcome); !ok {
		return true
	}
	allCommitted := true
	completedAt := time.Now().UnixMilli()
	attribution := cm.buildTurnAttribution(ctx)
	paths, byPath := groupClaimsByPath(received)
	committed := selectedKeySet(selected)
	for _, path := range paths {
		group := byPath[path]
		c, completion, err := buildEnvelopeCompletion(group, committed, outcome, ta.name, cm.partitionID, completedAt, attribution)
		if err != nil {
			log.Errorf("[finishDurableBatch] completion freeze failed rid=%s path=%s — claim held, NOT acked: %v",
				group[0].claim.RequestID, path, err)
			allCommitted = false
			continue
		}
		if err := ta.recordCompletionWithRetryForBus(ctx, bus, path, completion); err != nil {
			log.Warnf("[finishDurableBatch] completion not durable rid=%s path=%s after retry — claim held, receipt+ack deferred: %v",
				c.RequestID, path, err)
			allCommitted = false
			continue
		}
		cred, verr := cm.verifyReceiptCredential(completion)
		if verr != nil {
			log.Warnf("[finishDurableBatch] receipt unverified rid=%s key=%s — completion durable, claim held (§5.7 re-submits receipt, no re-run): %v",
				c.RequestID, c.ReceiptKey, verr)
			allCommitted = false
			continue
		}
		if err := bus.ConfirmDurable(path, cred); err != nil {
			allCommitted = false
			log.Warnf("[finishDurableBatch] confirm for %s deferred (replay will Ack-skip): %v", c.RequestID, err)
		}
	}
	return allCommitted
}

// recordCompletionWithRetry durably freezes a completion, retrying the WRITE on a
// transient I/O failure with the same bounded, cancellable backoff as the submit gate
// . The frozen bytes never change (deterministic freeze), so an identical retry
// converges idempotently at the inbox; a completion-conflict is a DEFINITE error (the
// already-durable completion is authoritative) and is NOT retried. Returns nil once the
// freeze is durable, or an error after the budget is spent (caller holds the claim).
func (ta *TagentAgent) recordCompletionWithRetry(path string, completion json.RawMessage) error {
	return ta.recordCompletionWithRetryForBus(context.Background(), ta.persistentBus, path, completion)
}

func (ta *TagentAgent) recordCompletionWithRetryForBus(ctx context.Context, bus *EventBus, path string, completion json.RawMessage) error {
	delays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	err := bus.RecordCompletion(path, completion)
	for attempt := 0; err != nil && !errors.Is(err, reliability.ErrCompletionConflict) && attempt < len(delays); attempt++ {
		log.Warnf("[finishDurableBatch] completion write retry %d/%d for %s: %v", attempt+1, len(delays), path, err)
		select {
		case <-time.After(delays[attempt]):
		case <-ctx.Done():
			return ctx.Err()
		}
		err = bus.RecordCompletion(path, completion)
	}
	return err
}

// batchBusForCM resolves this private mailbox or the owner's original assembly.
func (ta *TagentAgent) batchBusForCM(cm *ContextManager) *EventBus {
	if ta == nil || cm == nil {
		return nil
	}
	if cm.bus != nil {
		return cm.bus
	}
	if cm == ta.contextManager {
		return ta.persistentBus
	}
	return nil
}
