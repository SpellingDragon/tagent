package action

import (
	"crypto/md5"
	"fmt"
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

	// Completion: process doesn't exist or pane dead
	// Save output before returning so callback receives it
	// Only update LastOutput if we successfully got current output
	if !processExists || isPaneDead {
		if err == nil {
			session.LastOutput = currentOutput
			session.LastOutputMD5 = currentMD5
		}
		// If GetSessionOutput failed, preserve the old LastOutput
		return SessionCompleted
	}

	// At this point: processExists && !isPaneDead (session is alive)
	// Check fakeDead and Stable using elapsed time since output first became unchanged
	if !session.StableSince.IsZero() {
		stableDuration := time.Since(session.StableSince)

		// Fake dead: stable for too long without output change
		// Use strict greater-than so Stable fires BEFORE fakeDead on the same check
		if stableDuration > tm.fakeDeadDuration {
			// Log diagnostic info for fake_alive/fake_dead detection
			outputPreview := currentOutput
			if len(outputPreview) > 200 {
				outputPreview = outputPreview[:200] + "..."
			}
			log.Infof("[TmuxMonitor] session %s entering fake check: stableDuration=%s fakeDeadDuration=%s command=%q isInteractive=%v isTUI=%v output(len=%d): %q",
				session.ID, stableDuration, tm.fakeDeadDuration, session.Command, session.IsInteractive, session.IsTUI, len(currentOutput), outputPreview)

			// TUI sessions: skip heartbeat to avoid injecting text via send-keys.
			// Return TimedOut so the session is removed from monitoring —
			// the agent has already received the Stable event and can decide
			// whether to restart or abandon the TUI session.
			if session.IsTUI {
				session.LastOutput = currentOutput
				session.LastOutputMD5 = currentMD5
				return SessionTimedOut
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
			// Non-interactive commands: stable means completed.
			// The command has finished executing and produced its final output.
			// Don't enter fake_dead detection — heartbeat only proves shell is alive,
			// not that the command is still running.
			if !session.IsInteractive && !session.IsTUI {
				session.LastOutput = currentOutput
				session.LastOutputMD5 = currentMD5
				log.Infof("[TmuxMonitor] session %s non-interactive command completed after stable duration %s",
					session.ID, stableDuration)
				return SessionCompleted
			}
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
