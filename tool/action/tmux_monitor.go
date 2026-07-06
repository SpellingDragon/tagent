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
}

// DefaultMonitorConfig returns default monitor configuration
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		Interval:                  30 * time.Second,
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

	tm := &TmuxMonitor{
		interval:                  defaultCfg.Interval,
		stableDuration:            defaultCfg.StableDuration,
		interactiveStableDuration: defaultCfg.InteractiveStableDuration,
		fakeDeadDuration:          defaultCfg.FakeDeadDuration,
		heartbeatCommand:          defaultCfg.HeartbeatCommand,
		heartbeatTimeout:          defaultCfg.HeartbeatTimeout,
		sessions:                  make(map[string]*TmuxSession),
		stopCh:                    make(chan struct{}),
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
	log.Infof("[TmuxMonitor] added session %s", session.ID)
}

// RemoveSession removes a session from monitoring
func (tm *TmuxMonitor) RemoveSession(sessionID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.sessions, sessionID)
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
	ticker := time.NewTicker(tm.interval)
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
	tm.mu.RLock()
	sessions := make([]*TmuxSession, 0, len(tm.sessions))
	for _, s := range tm.sessions {
		sessions = append(sessions, s)
	}
	tm.mu.RUnlock()

	for _, session := range sessions {
		tm.checkSession(session)
	}
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
		log.Infof("[TmuxMonitor] removed session %s", session.ID)
	}

	sessionID := session.ID
	tm.mu.Unlock()

	if tm.StateChangeCallback != nil {
		tm.StateChangeCallback(sessionID, oldStatus, newStatus, newOutput)
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
	if !processExists || isPaneDead {
		session.LastOutput = currentOutput
		session.LastOutputMD5 = currentMD5
		return SessionCompleted
	}

	// At this point: processExists && !isPaneDead (session is alive)
	// Check fakeDead and Stable using elapsed time since output first became unchanged
	if !session.StableSince.IsZero() {
		stableDuration := time.Since(session.StableSince)

		// Fake dead: stable for too long without output change
		// Use strict greater-than so Stable fires BEFORE fakeDead on the same check
		if stableDuration > tm.fakeDeadDuration {
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
	log.Infof("[TmuxMonitor] session %s is fake alive, attempting restart", session.ID)

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
