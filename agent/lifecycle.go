package agent

import (
	"context"
	"fmt"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"

	"github.com/SpellingDragon/tagent/rl"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// RegisterCloser registers a closer to be called on agent shutdown.
func (ta *TagentAgent) RegisterCloser(c Closer) {
	ta.closers = append(ta.closers, c)
}

// ApplyOrgParams hot-swaps org-layer numeric parameters onto the live agent
// without rebuilding the topology (agent-config-hot-reload, incremental A).
// Currently migratable: compress_threshold. Structural changes (tools,
// sub-agents, prompts wiring) require the snapshot-level rebuild path and
// are NOT handled here.
// CheckOrgReload runs the armed org-config hot-reload check once (ops/test
// entry point; production arms it per-LLM-call via SetOrgReloader). No-op when
// no reloader is armed (config path unknown).
func (ta *TagentAgent) CheckOrgReload() {
	if ta.contextManager != nil {
		ta.contextManager.CheckOrgReload()
	}
}

// OrgThreshold returns the live compression threshold (introspection for
// tests/ops; the authoritative consumer is the compressor's atomic threshold).
func (ta *TagentAgent) OrgThreshold() float64 {
	if ta.contextManager == nil {
		return 0
	}
	return ta.contextManager.thresholdPct
}

func (ta *TagentAgent) ApplyOrgParams(thresholdPct float64) {
	if ta.contextManager != nil {
		ta.contextManager.ApplyOrgParams(thresholdPct)
	}
}

// SetOrgReloader arms a lazy org-config check invoked before each LLM call
// (agent-config-hot-reload, incremental A). The tagent layer wires this
// when a config path is known (WithConfigPath). fn must be cheap when
// nothing changed and must never fail the calling path.
func (ta *TagentAgent) SetOrgReloader(fn func()) {
	if ta.contextManager != nil {
		ta.contextManager.SetOrgReloader(fn)
	}
}

// SetTrajectoryRecorder sets the trajectory recorder for this agent.
// When set, StartLoop will automatically call SetSessionInfo on it.
// The recorder should also be registered via RegisterCloser for graceful shutdown.
func (ta *TagentAgent) SetTrajectoryRecorder(tr *rl.TrajectoryRecorder) {
	ta.trajectoryRecorder = tr
}

// TrajectoryRecorder returns the trajectory recorder if one is set, or nil.
func (ta *TagentAgent) TrajectoryRecorder() *rl.TrajectoryRecorder {
	return ta.trajectoryRecorder
}

// Close closes all registered resources and the runner.
// Closers (e.g., ActionTool) are stopped first, then the MemoryStore
// (if it supports closing), and finally the runner.
func (ta *TagentAgent) Close() error {
	var errs []error

	// Stop Persistent Event Loop first if active
	if ta.loopActive.Load() {
		ta.StopLoop()
	}

	// Stop the workspace cleaner goroutine.
	if ta.cleanupCancel != nil {
		ta.cleanupCancel()
	}

	// Close registered closers first (e.g., ActionTool stops TmuxMonitor)
	for _, c := range ta.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close resource: %w", err))
		}
	}

	// Close the durable inbox (unconfirmed items stay on disk for the next
	// process — shutdown is NOT data loss; resident-readiness-plan 3.2).
	if ta.persistentBus != nil {
		if err := ta.persistentBus.CloseDurable(); err != nil {
			errs = append(errs, fmt.Errorf("close durable inbox: %w", err))
		}
	}

	// Memory-store shutdown (resident-readiness-plan 4.2): an agent holding a
	// registry lease RELEASES it (the last lease closes the store); agents
	// without a lease (borrowed shells) never touch the shared store, and
	// fully-owned isolated stores fall back to direct Close.
	if ta.memStoreRelease != nil {
		ta.memStoreRelease()
	} else if c, ok := ta.memStore.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close memory store: %w", err))
		}
	}

	// Close ContextManager (closes unified Runner)
	if ta.contextManager != nil {
		if err := ta.contextManager.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close context manager: %w", err))
		}
	}

	// Close TrajectoryRecorder (flush writeLoop + close files)
	// Must be after contextManager.Close() so no new LLM calls are made.
	if ta.trajectoryRecorder != nil {
		if err := ta.trajectoryRecorder.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close trajectory recorder: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
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
	// on the loopActive check + initialization sequence.
	ta.sessionMu.Lock()
	// Terminal lifecycle (V15): after StopLoop the outputCh is closed — a
	// silent restart would hand consumers a dead channel and double-close it
	// on the next Stop. Fail explicitly instead.
	if ta.loopTerminated.Load() {
		ta.sessionMu.Unlock()
		return nil, fmt.Errorf("persistent loop already terminated: StopLoop is terminal on a TagentAgent instance; create a new agent for a fresh loop")
	}
	if ta.loopActive.Load() {
		ta.sessionMu.Unlock()
		return ta.outputCh, nil
	}

	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())
	ta.loopActive.Store(true)
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

	// Launch runEventLoop in a dedicated goroutine.
	ta.loopWg.Add(1)
	go func() {
		defer ta.loopWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[StartLoop] runEventLoop panic recovered: %v", r)
			}
			close(ta.outputCh)
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

// StopLoop stops the persistent event loop.
// Cancels the loop context, waits for the AgentLoop goroutine to exit.
func (ta *TagentAgent) StopLoop() {
	if !ta.loopActive.Load() {
		return
	}
	ta.loopActive.Store(false)
	ta.loopTerminated.Store(true) // terminal: see StartLoop doc (V15 lifecycle fix)

	// Stop meditation manager first (stop injecting new meditation events).
	if ta.meditationMgr != nil {
		ta.meditationMgr.Stop()
	}

	ta.loopCancel()
	ta.loopWg.Wait()
	log.Infof("[StopLoop] persistent event loop stopped")
}

// IsLoopActive returns true if the persistent event loop is currently running.
func (ta *TagentAgent) IsLoopActive() bool {
	return ta.loopActive.Load()
}

// finishDurableBatch (resident-readiness-plan 3.4/3.5): the consuming turn
// finished — per consumed envelope: ① persist a fact-chain inbox_receipt
// event (the dedup-window truth source; stored-gate: failure ⇒ NOT receipted,
// the claim replays), ② receipt+ack the envelope. Facts committed during the
// turn were already written back onto the envelope by persistBusEvent
// (AppendDurableEventKeys), so a replay after a mid-turn crash carries
// inbox_dedup_keys and does NOT duplicate facts/projection.
func (ta *TagentAgent) finishDurableBatch(events []*AgentEvent) {
	if ta == nil || ta.persistentBus == nil {
		return
	}
	for _, pr := range ta.persistentBus.DurableProvenance(events) {
		path, rid := pr[0], pr[1]
		if !ta.persistInboxReceipt(rid) {
			log.Warnf("[finishDurableBatch] receipt event for %s NOT stored — claim stays for replay", rid)
			continue
		}
		if err := ta.persistentBus.ConfirmDurable(path); err != nil {
			log.Warnf("[finishDurableBatch] confirm for %s deferred (replay will Ack-skip): %v", rid, err)
		}
	}
}

// persistInboxReceipt stores the fact-chain receipt for one request id.
// Returns false when the store is unavailable or the write fails (fail-safe:
// the envelope stays claimed and replays — never an unbacked ack).
// reconcileDurableReceipts (cold-eyes Major 2): startup bridge between the
// fact-chain receipt events (collected during the rebuild scan) and the live
// inbox — envelopes that were receipted but never acked are converged without
// re-execution. Must run AFTER RebuildProjectionFromWAL (which populates the
// receipt list) and after the persistentBus is wired.
// SetTurnDurableInbound installs the current turn's claimed envelope
// provenance (cold-eyes Major 1); clearTurnDurableInbound resets it at turn
// end. Both are event-loop-only (single consumer).
func (ta *TagentAgent) SetTurnDurableInbound(d plugin.DurableInbound) {
	if ta == nil || ta.contextManager == nil {
		return
	}
	ta.contextManager.turnDurableInbound = d
}

func (ta *TagentAgent) clearTurnDurableInbound() {
	if ta == nil || ta.contextManager == nil {
		return
	}
	ta.contextManager.turnDurableInbound = plugin.DurableInbound{}
}

// ReconcileDurableReceipts converges durable envelopes whose fact-chain
// receipt survived a crash but whose ack did not (cold-eyes Major 2) —
// startup-only, after RebuildProjectionFromWAL populated the receipt list.
func (ta *TagentAgent) ReconcileDurableReceipts() {
	if ta == nil || ta.persistentBus == nil || ta.contextManager == nil {
		return
	}
	res := ta.contextManager.RecoveryResult()
	if res == nil || len(res.ReceiptedRequestIDs) == 0 {
		return
	}
	if n := ta.persistentBus.ReconcileReceipted(res.ReceiptedRequestIDs); n > 0 {
		log.Infof("[recovery] %d durable envelope(s) reconciled from fact-chain receipts (no re-execution)", n)
	}
}

func (ta *TagentAgent) persistInboxReceipt(requestID string) bool {
	cm := ta.contextManager
	if cm == nil || cm.memStore == nil {
		return false
	}
	key := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	now := time.Now().UnixMilli()
	ev := memory.FullEvent{
		EventKey:     key,
		PartitionID:  cm.partitionID,
		EventType:    tagentevent.TypeInboxReceipt,
		EventSummary: "[inbox receipt] " + requestID,
		Content:      "[inbox receipt] " + requestID,
		Timestamp:    now,
		Metadata: map[string]string{
			"inbox_request_id":           requestID,
			tagentevent.MetaKeyAgentName: ta.name,
		},
	}
	if cm.sessionID != "" {
		ev.Metadata[tagentevent.MetaKeyRolloutID] = cm.sessionID
	}
	return cm.memStore.StoreEvent(key, ev) == nil
}
