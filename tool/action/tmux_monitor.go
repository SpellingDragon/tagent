package action

import (
	"crypto/md5"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// TmuxMonitor monitors tmux sessions and detects state changes.
type TmuxMonitor struct {
	executor                  sessionInspector
	interval                  time.Duration
	stableDuration            time.Duration
	interactiveStableDuration time.Duration
	fakeDeadDuration          time.Duration
	heartbeatCommand          string
	heartbeatTimeout          time.Duration

	sessions map[string]*TmuxSession
	mu       sync.RWMutex
	stopCh   chan struct{}
	running  atomic.Bool

	// lastNotifiedStatus tracks the last state for which we triggered a callback
	// for each session. This prevents duplicate callbacks for the same stable state
	// (e.g., Stable -> Stable should not trigger twice).
	lastNotifiedStatus map[string]SessionStatus

	// sessionCallbacks holds optional per-session state-change callbacks,
	// registered via AddSessionWithCallback. They fire alongside (in addition
	// to) StateChangeCallback, under the same meaningful-state + dedup gate.
	// This lets a caller route a specific session's transitions to a dedicated
	// consumer (e.g. a per-call settle detector) without global-callback demux.
	sessionCallbacks map[string]func(sessionID string, oldStatus, newStatus SessionStatus, output string)

	// schedule is the adaptive poll schedule (dense→backoff). Its DenseInterval
	// is the loop tick; nextPoll tracks each session's next due time so the loop
	// polls only due sessions and reschedules each by its age (D1/D4). A zero
	// schedule (e.g. tests that build TmuxMonitor directly) disables adaptivity
	// (every session is always due).
	schedule PollSchedule
	nextPoll map[string]time.Time

	// StateChangeCallback is called when session state changes.
	// The callback receives session ID, old status, new status, and output snapshot.
	// It's the caller's responsibility to store events to MemoryStore.
	StateChangeCallback func(sessionID string, oldStatus, newStatus SessionStatus, output string)
}

// sessionInspector abstracts tmux operations for testability.
type sessionInspector interface {
	ProcessExists(sessionID string) bool
	IsPaneDead(sessionID string) bool
	GetSessionOutput(sessionID string) (string, error)
	SendHeartbeat(sessionID string) string
	KillSession(sessionID string) error
	RestartSession(sessionID string, opts TmuxCreateOptions) error
}

// compile-time check: TmuxExecutor implements sessionInspector.
var _ sessionInspector = (*TmuxExecutor)(nil)

// MonitorConfig holds configuration for TmuxMonitor
type MonitorConfig struct {
	Interval                  time.Duration
	StableDuration            time.Duration
	InteractiveStableDuration time.Duration
	FakeDeadDuration          time.Duration
	HeartbeatCommand          string
	HeartbeatTimeout          time.Duration

	// Adaptive poll schedule (optional; unset fields fall back to defaults, with
	// DenseInterval derived from Interval). See PollSchedule.
	DenseInterval time.Duration
	DenseDuration time.Duration
	BackoffFactor float64
	MaxInterval   time.Duration
}

// DefaultMonitorConfig returns default monitor configuration
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		// Interval is the poll cadence. Kept low so a finished command's
		// completion is detected within a few seconds — this lets short
		// commands settle INLINE within the task layer's sync-wait window,
		// while genuinely long-running work exceeds the window and goes async.
		Interval:                  3 * time.Second,
		StableDuration:            60 * time.Second,
		InteractiveStableDuration: 90 * time.Second,
		FakeDeadDuration:          150 * time.Second,
		HeartbeatCommand:          "echo ping",
		HeartbeatTimeout:          5 * time.Second,
	}
}

// TmuxMonitorOption configures TmuxMonitor
type TmuxMonitorOption func(*TmuxMonitor)

// WithMonitorConfig sets the monitor configuration
func WithMonitorConfig(cfg MonitorConfig) TmuxMonitorOption {
	return func(tm *TmuxMonitor) {
		tm.interval = cfg.Interval
		tm.stableDuration = cfg.StableDuration
		tm.interactiveStableDuration = cfg.InteractiveStableDuration
		tm.fakeDeadDuration = cfg.FakeDeadDuration
		tm.heartbeatCommand = cfg.HeartbeatCommand
		tm.heartbeatTimeout = cfg.HeartbeatTimeout
		if cfg.Interval > 0 {
			tm.schedule.DenseInterval = cfg.Interval // dense cadence = configured interval
		}
		// Explicit schedule overrides (optional).
		if cfg.DenseInterval > 0 {
			tm.schedule.DenseInterval = cfg.DenseInterval
		}
		if cfg.DenseDuration > 0 {
			tm.schedule.DenseDuration = cfg.DenseDuration
		}
		if cfg.BackoffFactor >= 1 {
			tm.schedule.BackoffFactor = cfg.BackoffFactor
		}
		if cfg.MaxInterval > 0 {
			tm.schedule.MaxInterval = cfg.MaxInterval
		}
	}
}

// WithMonitorExecutor sets the tmux executor
func WithMonitorExecutor(exec sessionInspector) TmuxMonitorOption {
	return func(tm *TmuxMonitor) {
		tm.executor = exec
	}
}

// WithMonitorStateChangeCallback sets the state change callback
func WithMonitorStateChangeCallback(cb func(sessionID string, oldStatus, newStatus SessionStatus, output string)) TmuxMonitorOption {
	return func(tm *TmuxMonitor) {
		tm.StateChangeCallback = cb
	}
}

// NewTmuxMonitor creates a new tmux monitor
func NewTmuxMonitor(opts ...TmuxMonitorOption) *TmuxMonitor {
	defaultCfg := DefaultMonitorConfig()

	sched := DefaultPollSchedule()
	sched.DenseInterval = defaultCfg.Interval // preserve the tuned base cadence

	tm := &TmuxMonitor{
		interval:                  defaultCfg.Interval,
		stableDuration:            defaultCfg.StableDuration,
		interactiveStableDuration: defaultCfg.InteractiveStableDuration,
		fakeDeadDuration:          defaultCfg.FakeDeadDuration,
		heartbeatCommand:          defaultCfg.HeartbeatCommand,
		heartbeatTimeout:          defaultCfg.HeartbeatTimeout,
		sessions:                  make(map[string]*TmuxSession),
		stopCh:                    make(chan struct{}),
		lastNotifiedStatus:        make(map[string]SessionStatus),
		sessionCallbacks:          make(map[string]func(sessionID string, oldStatus, newStatus SessionStatus, output string)),
		schedule:                  sched,
		nextPoll:                  make(map[string]time.Time),
	}

	for _, opt := range opts {
		opt(tm)
	}

	return tm
}

// IsRunning returns whether the monitor is running (thread-safe).
func (tm *TmuxMonitor) IsRunning() bool {
	return tm.running.Load()
}

// Start starts the monitor
func (tm *TmuxMonitor) Start() {
	if tm.running.Swap(true) {
		return
	}

	tm.stopCh = make(chan struct{})
	go tm.monitorLoop()
	log.Infof("[TmuxMonitor] started with interval %v", tm.interval)
}

// Stop stops the monitor
func (tm *TmuxMonitor) Stop() {
	if !tm.running.Swap(false) {
		return
	}

	close(tm.stopCh)
	log.Infof("[TmuxMonitor] stopped")
}

// AddSession adds a session to monitor
func (tm *TmuxMonitor) AddSession(session *TmuxSession) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.sessions[session.ID] = session
	tm.markDueLocked(session)
	log.Infof("[TmuxMonitor] added session %s", session.ID)
}

// markDueLocked marks a freshly-added session as due on the next tick and sets
// its CreatedAt (for age-based scheduling) if unset. Caller holds tm.mu.
// No-op when the schedule map is absent (non-adaptive test monitors).
func (tm *TmuxMonitor) markDueLocked(session *TmuxSession) {
	if tm.nextPoll == nil {
		return
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	tm.nextPoll[session.ID] = time.Now()
}

// AddSessionWithCallback adds a session to monitoring and registers a callback
// that fires for THIS session's meaningful state changes, in addition to any
// global StateChangeCallback and under the same meaningful-state + dedup gate.
// Used by callers that want per-session routing (e.g. a per-call settle
// detector) without demultiplexing the global callback.
func (tm *TmuxMonitor) AddSessionWithCallback(session *TmuxSession, cb func(sessionID string, oldStatus, newStatus SessionStatus, output string)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.sessions[session.ID] = session
	if cb != nil {
		if tm.sessionCallbacks == nil {
			tm.sessionCallbacks = make(map[string]func(sessionID string, oldStatus, newStatus SessionStatus, output string))
		}
		tm.sessionCallbacks[session.ID] = cb
	}
	tm.markDueLocked(session)
	log.Infof("[TmuxMonitor] added session %s (per-session callback)", session.ID)
}

// SessionIDs returns a snapshot of the currently monitored session IDs
// (used by ActionTool.Close to reap live sessions on graceful shutdown).
func (tm *TmuxMonitor) SessionIDs() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	ids := make([]string, 0, len(tm.sessions))
	for id := range tm.sessions {
		ids = append(ids, id)
	}
	return ids
}

// TouchSession re-enters dense polling for a LIVE session (resume path: a
// resumed round wants quick settle detection, exactly like a fresh spawn) by
// resetting the session's age and marking it due. Returns false if the
// session is no longer monitored (reaped) so the caller can surface the
// error. The per-session callback is NOT touched — the detector is bound to
// the session for its whole lifetime (see TmuxSettleDetector.Rearm).
func (tm *TmuxMonitor) TouchSession(sessionID string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	session, ok := tm.sessions[sessionID]
	if !ok {
		return false
	}
	session.CreatedAt = time.Now() // re-enter dense polling for the resumed round
	session.Status = SessionRunning
	tm.markDueLocked(session)
	log.Infof("[TmuxMonitor] touched session %s (resume round)", sessionID)
	return true
}

// RemoveSession removes a session from monitoring
func (tm *TmuxMonitor) RemoveSession(sessionID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.sessions, sessionID)
	delete(tm.sessionCallbacks, sessionID)
	delete(tm.nextPoll, sessionID)
	log.Infof("[TmuxMonitor] removed session %s", sessionID)
}

// GetSession gets a session by ID
func (tm *TmuxMonitor) GetSession(sessionID string) (*TmuxSession, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	session, exists := tm.sessions[sessionID]
	return session, exists
}

// GetSessionStatus returns the current status of a session (thread-safe).
// Use this instead of GetSession(...).Status to avoid data races with checkSession.
func (tm *TmuxMonitor) GetSessionStatus(sessionID string) (SessionStatus, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	session, exists := tm.sessions[sessionID]
	if !exists {
		return "", false
	}
	return session.Status, true
}

// ListSessions returns all monitored sessions
func (tm *TmuxMonitor) ListSessions() []*TmuxSession {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	sessions := make([]*TmuxSession, 0, len(tm.sessions))
	for _, s := range tm.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// monitorLoop is the main monitoring loop
func (tm *TmuxMonitor) monitorLoop() {
	tick := tm.schedule.DenseInterval
	if tick <= 0 {
		tick = tm.interval
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-tm.stopCh:
			return
		case <-ticker.C:
			tm.checkAllSessions()
		}
	}
}

// checkAllSessions checks all monitored sessions by snapshotting the session
// list and calling checkSession for each. This avoids holding the lock across
// callbacks (which may call GetSession and deadlock).
func (tm *TmuxMonitor) checkAllSessions() {
	now := time.Now()
	tm.mu.RLock()
	adaptive := tm.nextPoll != nil
	sessions := make([]*TmuxSession, 0, len(tm.sessions))
	for id, s := range tm.sessions {
		if adaptive {
			if np, ok := tm.nextPoll[id]; ok && np.After(now) {
				continue // not due yet (backoff)
			}
		}
		sessions = append(sessions, s)
	}
	tm.mu.RUnlock()

	for _, session := range sessions {
		tm.checkSession(session)
		if adaptive {
			tm.rescheduleSession(session, now)
		}
	}
}

// rescheduleSession sets a session's next poll time by its age-derived interval
// (dense→backoff). A stable (service-ready) session polls at the sparsest
// cadence (D6). A removed (terminal) session drops its schedule.
func (tm *TmuxMonitor) rescheduleSession(s *TmuxSession, now time.Time) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, ok := tm.sessions[s.ID]; !ok {
		delete(tm.nextPoll, s.ID)
		return
	}
	age := now.Sub(s.CreatedAt)
	if s.CreatedAt.IsZero() {
		age = 0
	}
	interval := tm.schedule.intervalForAge(age)
	if s.Status == SessionStable && tm.schedule.MaxInterval > 0 {
		interval = tm.schedule.MaxInterval
	}
	tm.nextPoll[s.ID] = now.Add(interval)
}

// checkSession checks a single session: detects state, updates fields under
// lock, fires callback outside lock. Completed/errored sessions are removed.
func (tm *TmuxMonitor) checkSession(session *TmuxSession) {
	tm.mu.Lock()

	oldStatus := session.Status
	newStatus := tm.detectSessionState(session)

	if newStatus == oldStatus {
		tm.mu.Unlock()
		return
	}

	// detectSessionState updates session.LastOutput with current output on
	// state transitions (e.g. SessionCompleted). Use the updated value for
	// the callback so consumers receive the final output.
	newOutput := session.LastOutput
	session.Status = newStatus

	log.Infof("[TmuxMonitor] session %s: %s -> %s",
		session.ID, oldStatus, newStatus)

	shouldRemove := false
	switch newStatus {
	case SessionFakeAlive:
		tm.handleFakeAlive(session)
	case SessionFakeDead:
		shouldRemove = tm.handleFakeDead(session)
	case SessionTimedOut, SessionCompleted, SessionError:
		shouldRemove = true
	}

	if shouldRemove {
		delete(tm.sessions, session.ID)
		delete(tm.nextPoll, session.ID)
		log.Infof("[TmuxMonitor] removed session %s", session.ID)
	}

	sessionID := session.ID

	globalCb := tm.StateChangeCallback
	sessCb := tm.sessionCallbacks[sessionID]

	// Check if we should trigger a callback:
	// 1. A callback (global or per-session) must be registered
	// 2. Both old and new states must be meaningful (not FakeDead/FakeAlive)
	// 3. New state must differ from the last notified state (prevent duplicates)
	shouldCallback := false
	if (globalCb != nil || sessCb != nil) &&
		isMeaningfulState(oldStatus) && isMeaningfulState(newStatus) {
		lastNotified := tm.lastNotifiedStatus[sessionID]
		if lastNotified != newStatus {
			tm.lastNotifiedStatus[sessionID] = newStatus
			shouldCallback = true
		}
	}

	// If the session was removed above (terminal state), also drop its
	// per-session callback and dedup state. sessCb is already captured for the
	// final callback below, so the terminal transition still notifies.
	if shouldRemove {
		delete(tm.sessionCallbacks, sessionID)
		delete(tm.lastNotifiedStatus, sessionID)
	}

	tm.mu.Unlock()

	// Trigger callbacks outside the lock to avoid holding the lock during
	// potentially slow callback execution. The global and per-session callbacks
	// (if present) fire under the same meaningful-state + dedup gate.
	if shouldCallback {
		if globalCb != nil {
			globalCb(sessionID, oldStatus, newStatus, newOutput)
		}
		if sessCb != nil {
			sessCb(sessionID, oldStatus, newStatus, newOutput)
		}
	}
}

// isMeaningfulState returns true for states that warrant event injection.
// Meaningful states are: Running, Stable, Completed, Error, TimedOut.
// Intermediate states (FakeDead, FakeAlive) are suppressed to avoid event noise.
func isMeaningfulState(s SessionStatus) bool {
	switch s {
	case SessionRunning, SessionStable, SessionCompleted, SessionError, SessionTimedOut:
		return true
	default:
		return false
	}
}

// detectSessionState detects the current state of a session.
// All state detection is time-based, using StableSince as the sole indicator.
// No stableCount — the elapsed duration since output first became unchanged
// determines whether the session is Stable or fakeDead.
// fakeDeadThreshold returns the fake-dead detection threshold for a session:
// the session-level QuietTimeout when set (>0), otherwise the monitor's global
// fakeDeadDuration. Both fake-dead judgment paths (non-TUI heartbeat branch and
// TUI TimedOut branch, which share the same threshold check) must use this
// helper so a per-session quiet_timeout applies uniformly.
// stableWindow returns the stability window for a session type (non-TUI 60s,
// TUI 90s). Exported to Call validation via a method so the quiet_timeout
// lower bound always matches the monitor's actual configuration.
func (tm *TmuxMonitor) stableWindow(isTUI bool) time.Duration {
	if isTUI {
		return tm.interactiveStableDuration
	}
	return tm.stableDuration
}

func (tm *TmuxMonitor) fakeDeadThreshold(session *TmuxSession) time.Duration {
	if session != nil && session.QuietTimeout > 0 {
		return session.QuietTimeout
	}
	return tm.fakeDeadDuration
}

func (tm *TmuxMonitor) detectSessionState(session *TmuxSession) SessionStatus {
	if tm.executor == nil {
		return SessionError
	}

	// Check if session exists
	processExists := tm.executor.ProcessExists(session.ID)
	isPaneDead := tm.executor.IsPaneDead(session.ID)

	// Get current output
	currentOutput, err := tm.executor.GetSessionOutput(session.ID)
	if err != nil {
		// GetSessionOutput failed — try to capture output anyway
		// (pane may be dead but output still in buffer)
		currentOutput = ""
	}

	// Calculate MD5 of output
	currentMD5 := fmt.Sprintf("%x", md5.Sum([]byte(currentOutput)))

	// Track output stability using StableSince as the sole time-based indicator.
	// StableSince records when output FIRST became unchanged (not when Stable was declared).
	// This makes the time window accurately reflect "how long since last output change".
	if processExists && !isPaneDead {
		if currentMD5 == session.LastOutputMD5 {
			// Output unchanged: record stable-since time if this is the first unchanged check
			if session.StableSince.IsZero() {
				session.StableSince = time.Now()
			}
		} else {
			// Output changed: reset stable timer (new output cycle)
			session.StableSince = time.Time{}
		}
	}

	// Completion: process doesn't exist or pane dead.
	// Race guard: with remain-on-exit, capture-pane can race the pane
	// teardown and return empty/partial content. Retry briefly; if the
	// capture still yields nothing usable, PRESERVE the previous
	// LastOutput (the last Running-poll snapshot) instead of clobbering
	// it — consumers must get the true final tail (e.g. END_MARKER).
	if !processExists || isPaneDead {
		capture := currentOutput
		lastErr := err
		for attempt := 0; attempt < 10 && (lastErr != nil || strings.TrimSpace(capture) == ""); attempt++ {
			time.Sleep(250 * time.Millisecond)
			if retryOut, retryErr := tm.executor.GetSessionOutput(session.ID); retryErr == nil && strings.TrimSpace(retryOut) != "" {
				capture = retryOut
				lastErr = nil
				break
			} else if retryErr != nil {
				lastErr = retryErr
			}
		}
		// Convergence guard: a capture taken while tmux is still flushing
		// the pane's final lines into history can be valid-looking yet
		// TRUNCATED (missing the tail, e.g. END_MARKER). Re-capture once
		// after a short settle; if it grew, the first was mid-write.
		// NOTE: on tmux 3.4, capture-pane on a fully-dead pane can return EMPTY
		// (no history) for a short window after process exit; the retry loop
		// above (10x250ms) bridges that window. If still empty, keep last
		// Running-poll snapshot (best effort).
		if lastErr == nil && strings.TrimSpace(capture) != "" {
			// Settle window: tmux may still be draining the pty into
			// history after process exit; an early capture can be
			// stable-but-STALE (byte-identical truncation observed
			// across runs). Poll until content is identical twice in a
			// row or the window expires; the LONGEST capture wins.
			// Completed-path ONLY: interactive sessions (python REPL /
			// coding agents) never reach this branch -- their live-poll
			// reads and fake-dead MD5 detection are untouched.
			best := capture
			identical := 0
			for i := 0; i < 12; i++ {
				time.Sleep(250 * time.Millisecond)
				next, nextErr := tm.executor.GetSessionOutput(session.ID)
				if nextErr != nil {
					break
				}
				if next == best {
					identical++
					if identical >= 2 {
						break
					}
					continue
				}
				identical = 0
				if len(next) > len(best) {
					best = next
				}
			}
			capture = best
		}
		if lastErr == nil && strings.TrimSpace(capture) != "" {
			session.LastOutput = capture
			session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte(capture)))
		}
		return SessionCompleted
	}

	// At this point: processExists && !isPaneDead (session is alive)
	// Check fakeDead and Stable using elapsed time since output first became unchanged
	if !session.StableSince.IsZero() {
		stableDuration := time.Since(session.StableSince)

		// (2026-09-11 async-action overhaul, B1) Resident sessions: silence
		// is HEALTHY. A dev server / tunnel / trainer that prints nothing for
		// an hour is doing its job — no stable settle, no fake-dead kill, no
		// auto-reap. Only unexpected death settles (handled by the pane-dead
		// branch above). Liveness refinement (C2 probe) attaches separately.
		if session.Mode == ModeResident {
			session.LastOutput = currentOutput
			session.LastOutputMD5 = currentMD5
			return SessionRunning
		}

		// Fake dead: stable for too long without output change
		// Use strict greater-than so Stable fires BEFORE fakeDead on the same check
		if stableDuration > tm.fakeDeadThreshold(session) {
			// Log diagnostic info for fake_alive/fake_dead detection
			outputPreview := currentOutput
			if len(outputPreview) > 200 {
				outputPreview = outputPreview[:200] + "..."
			}
			log.Infof("[TmuxMonitor] session %s entering fake check: stableDuration=%s fakeDeadThreshold=%s command=%q isInteractive=%v isTUI=%v output(len=%d): %q",
				session.ID, stableDuration, tm.fakeDeadThreshold(session), session.Command, session.IsInteractive, session.IsTUI, len(currentOutput), outputPreview)

			// TUI sessions: skip heartbeat to avoid injecting text via send-keys.
			// Return TimedOut so the session is removed from monitoring —
			// the agent has already received the Stable event and can decide
			// whether to restart or abandon the TUI session.
			if session.IsTUI {
				session.LastOutput = currentOutput
				session.LastOutputMD5 = currentMD5
				return SessionTimedOut
			}

			// (2026-09-11 async-action overhaul, A1-fakedead) Non-interactive
			// sessions: silence is the NORMAL state of long workloads (a 54-min
			// compile prints nothing for minutes at a time). The old path sent
			// an `echo tmux_heartbeat` keystroke — which (a) pollutes the
			// foreground process's stdin and (b) is never executed anyway (the
			// foreground process is cargo/gcc, not a shell), so it always read
			// no_response → FakeDead → handleFakeDead killed LEGAL long tasks
			// at the default 150s. Liveness truth for non-interactive sessions
			// is the process/pane state, not keystroke echo:
			//   - pane dead   → real exit → SessionCompleted;
			//   - alive + caller set an explicit quiet_timeout → honor the hard
			//     timeout (FakeDead → kill) — the escape hatch, unchanged;
			//   - alive + default → keep SessionStable, never auto-kill.
			// (Kept the heartbeat path for interactive shells, where it works:
			// the shell DOES consume and echo the injected input.)
			if !session.IsInteractive {
				if tm.executor.IsPaneDead(session.ID) {
					session.LastOutput = currentOutput
					session.LastOutputMD5 = currentMD5
					return SessionCompleted
				}
				if session.QuietTimeout > 0 {
					// Caller-declared hard timeout: kill as before (skip the
					// meaningless heartbeat — same outcome, no stdin pollution).
					log.Infof("[TmuxMonitor] session %s quiet beyond declared quiet_timeout=%s, engaging fake-dead kill",
						session.ID, session.QuietTimeout)
					return SessionFakeDead
				}
				log.Infof("[TmuxMonitor] session %s quiet beyond %s but process alive and no explicit quiet_timeout — keeping stable (no auto-kill)",
					session.ID, tm.fakeDeadThreshold(session))
				return SessionStable
			}

			heartbeatResult := tm.executor.SendHeartbeat(session.ID)
			// Re-read output after heartbeat to see if heartbeat response is present
			afterOutput, _ := tm.executor.GetSessionOutput(session.ID)
			afterPreview := afterOutput
			if len(afterPreview) > 200 {
				afterPreview = afterPreview[:200] + "..."
			}
			log.Infof("[TmuxMonitor] session %s heartbeat result=%q (output before len=%d, after len=%d, after preview): %q",
				session.ID, heartbeatResult, len(currentOutput), len(afterOutput), afterPreview)
			if heartbeatResult == "ok" {
				// Process responds to heartbeat, it's fake alive (process is stuck)
				return SessionFakeAlive
			}
			// Heartbeat got no response.
			// Re-check pane dead status: the pane may have died during our
			// stable-period accumulation, in which case this is a normal
			// completion, not a fake dead.
			if tm.executor.IsPaneDead(session.ID) {
				session.LastOutput = currentOutput
				session.LastOutputMD5 = currentMD5
				return SessionCompleted
			}
			return SessionFakeDead
		}

		// Stable: output has been unchanged for sufficient time
		threshold := tm.getStableDuration(session.IsInteractive)
		if stableDuration >= threshold {
			// (2026-09-11 async-action overhaul, A1) Stable NO LONGER maps to
			// Completed for non-interactive sessions. The old branch returned
			// SessionCompleted while processExists && !isPaneDead — killing
			// long-silent-but-ALIVE workloads (compiles, downloads, resident
			// servers) via the terminal-status reap, and starving the task
			// layer's alive-detached semantics (D4), which never fired for
			// tmux tasks. Now every alive+quiet session reports SessionStable;
			// the task layer emits the one-time ∞ alive-detached notice and
			// keeps tracking. Real completion is detected by the pane-death
			// branch above (process actually exited → SessionCompleted).
			// Long-silent-alive sessions later crossing the fake-dead
			// threshold are handled by the heartbeat branch — which for
			// non-interactive sessions now checks process liveness via
			// pane/exit status FIRST instead of injecting keystrokes (a
			// heartbeat 'echo' into a compiling process's stdin corrupts it).
			session.LastOutput = currentOutput
			session.LastOutputMD5 = currentMD5
			log.Infof("[TmuxMonitor] session %s output stable for %s (process alive) — reporting stable, NOT completed",
				session.ID, stableDuration)
			return SessionStable
		}
	}

	// Default: still running
	session.LastOutput = currentOutput
	session.LastOutputMD5 = currentMD5

	return SessionRunning
}

// handleFakeAlive handles fake alive state (process stuck but responsive).
// Attempts to restart the session under its ORIGINAL session ID so the monitor
// continues tracking it. On success, resets stability metadata; the next
// detectSessionState will see the fresh session and naturally transition
// FakeAlive → Running. On failure, leaves Status untouched so the next cycle
// re-evaluates — the session may complete naturally or reach fakeDead.
func (tm *TmuxMonitor) handleFakeAlive(session *TmuxSession) {
	log.Infof("[TmuxMonitor] session %s is fake alive (command=%q, isInteractive=%v, isTUI=%v), attempting restart",
		session.ID, session.Command, session.IsInteractive, session.IsTUI)

	// Try to restart
	opts := TmuxCreateOptions{
		Command:       session.Command,
		WorkDir:       session.WorkDir,
		IsInteractive: session.IsInteractive,
	}

	err := tm.executor.RestartSession(session.ID, opts)
	if err != nil {
		log.Errorf("[TmuxMonitor] failed to restart session %s: %v", session.ID, err)
		return
	}

	// Reset stability tracking so the next detectSessionState sees fresh state.
	session.StableSince = time.Time{}
	session.LastOutput = ""
	session.LastOutputMD5 = ""
	session.CreatedAt = time.Now()
	log.Infof("[TmuxMonitor] session %s restarted successfully", session.ID)
}

// handleFakeDead handles fake dead state (process exists but unresponsive).
// Returns true if the session should be removed from monitoring, false if
// the session should be retained for a retry on the next check cycle.
// KillSession is retried up to 3 times before force-removing the session.
func (tm *TmuxMonitor) handleFakeDead(session *TmuxSession) bool {
	log.Infof("[TmuxMonitor] session %s is fake dead, attempting kill (retry=%d)",
		session.ID, session.KillRetryCount)

	// Attempt to kill the session
	if err := tm.executor.KillSession(session.ID); err != nil {
		session.KillRetryCount++
		log.Errorf("[TmuxMonitor] failed to kill session %s (retry=%d): %v",
			session.ID, session.KillRetryCount, err)

		if session.KillRetryCount < 3 {
			// Revert status to Stable so the next detectSessionState cycle
			// will re-detect FakeDead and trigger another kill attempt.
			session.Status = SessionStable
			return false // Keep session in monitoring map
		}
		// Max retries reached: force remove even if kill failed
		log.Warnf("[TmuxMonitor] session %s reached max kill retries, force-removing", session.ID)
	}

	// Kill succeeded or max retries reached
	session.Status = SessionCompleted
	return true
}

// getStableDuration returns the time-based stability threshold based on session type.
func (tm *TmuxMonitor) getStableDuration(isInteractive bool) time.Duration {
	if isInteractive {
		return tm.interactiveStableDuration
	}
	return tm.stableDuration
}
