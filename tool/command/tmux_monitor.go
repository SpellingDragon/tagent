package command

import (
	"crypto/md5"
	"fmt"
	"log"
	"sync"
	"time"
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
	running  bool

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

// Start starts the monitor
func (tm *TmuxMonitor) Start() {
	if tm.running {
		return
	}

	tm.running = true
	tm.stopCh = make(chan struct{})

	go tm.monitorLoop()
	log.Printf("[TmuxMonitor] started with interval %v", tm.interval)
}

// Stop stops the monitor
func (tm *TmuxMonitor) Stop() {
	if !tm.running {
		return
	}

	close(tm.stopCh)
	tm.running = false
	log.Printf("[TmuxMonitor] stopped")
}

// AddSession adds a session to monitor
func (tm *TmuxMonitor) AddSession(session *TmuxSession) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.sessions[session.ID] = session
	log.Printf("[TmuxMonitor] added session %s", session.ID)
}

// RemoveSession removes a session from monitoring
func (tm *TmuxMonitor) RemoveSession(sessionID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.sessions, sessionID)
	log.Printf("[TmuxMonitor] removed session %s", sessionID)
}

// GetSession gets a session by ID
func (tm *TmuxMonitor) GetSession(sessionID string) (*TmuxSession, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	session, exists := tm.sessions[sessionID]
	return session, exists
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

// checkAllSessions checks all monitored sessions
func (tm *TmuxMonitor) checkAllSessions() {
	tm.mu.Lock()
	sessions := make([]*TmuxSession, 0, len(tm.sessions))
	for _, s := range tm.sessions {
		sessions = append(sessions, s)
	}
	tm.mu.Unlock()

	for _, session := range sessions {
		tm.checkSession(session)
	}
}

// checkSession checks a single session's state
func (tm *TmuxMonitor) checkSession(session *TmuxSession) {
	oldStatus := session.Status
	newStatus := tm.detectSessionState(session)

	if newStatus != oldStatus {
		// State changed
		oldOutput := session.LastOutput
		session.Status = newStatus

		log.Printf("[TmuxMonitor] session %s: %s -> %s",
			session.ID, oldStatus, newStatus)

		// Trigger callback (caller handles event storage)
		if tm.StateChangeCallback != nil {
			tm.StateChangeCallback(session.ID, oldStatus, newStatus, oldOutput)
		}

		// Handle special states
		switch newStatus {
		case SessionFakeAlive:
			tm.handleFakeAlive(session)
		case SessionFakeDead:
			tm.handleFakeDead(session)
			tm.RemoveSession(session.ID)
		case SessionCompleted, SessionError:
			tm.RemoveSession(session.ID)
		}
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
		if !processExists {
			return SessionCompleted
		}
		return SessionError
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

	// Completion: process doesn't exist but pane not marked dead
	if !processExists && !isPaneDead {
		session.LastOutput = currentOutput
		session.LastOutputMD5 = currentMD5
		return SessionCompleted
	}

	// Completion: pane dead or process gone
	if isPaneDead || !processExists {
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
			// Return Running with StableSince preserved — the agent has already
			// received the Stable event and can make its own timeout/cleanup decision.
			if session.IsTUI {
				session.LastOutput = currentOutput
				session.LastOutputMD5 = currentMD5
				return SessionRunning
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
	log.Printf("[TmuxMonitor] session %s is fake alive, attempting restart", session.ID)

	// Try to restart
	opts := TmuxCreateOptions{
		Command:       session.Command,
		WorkDir:       session.WorkDir,
		IsInteractive: session.IsInteractive,
	}

	err := tm.executor.RestartSession(session.ID, opts)
	if err != nil {
		log.Printf("[TmuxMonitor] failed to restart session %s: %v", session.ID, err)
		return
	}

	// Reset stability tracking so the next detectSessionState sees fresh state.
	session.StableSince = time.Time{}
	session.LastOutput = ""
	session.LastOutputMD5 = ""
	session.CreatedAt = time.Now()
	log.Printf("[TmuxMonitor] session %s restarted successfully", session.ID)
}


// handleFakeDead handles fake dead state (process exists but unresponsive)
func (tm *TmuxMonitor) handleFakeDead(session *TmuxSession) {
	log.Printf("[TmuxMonitor] session %s is fake dead, forcing cleanup", session.ID)

	// Force kill session
	if err := tm.executor.KillSession(session.ID); err != nil {
		log.Printf("[TmuxMonitor] failed to kill session %s: %v", session.ID, err)
	}

	// Mark as completed
	session.Status = SessionCompleted
}

// getStableDuration returns the time-based stability threshold based on session type.
func (tm *TmuxMonitor) getStableDuration(isInteractive bool) time.Duration {
	if isInteractive {
		return tm.interactiveStableDuration
	}
	return tm.stableDuration
}
