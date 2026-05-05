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
	executor             sessionInspector
	interval             time.Duration
	stableThreshold      int
	interactiveThreshold int
	fakeDeadThreshold    int
	heartbeatCommand     string
	heartbeatTimeout     time.Duration

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
	Interval             time.Duration
	StableThreshold      int
	InteractiveThreshold int
	FakeDeadThreshold    int
	HeartbeatCommand     string
	HeartbeatTimeout     time.Duration
}

// DefaultMonitorConfig returns default monitor configuration
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		Interval:             30 * time.Second,
		StableThreshold:      2,
		InteractiveThreshold: 3,
		FakeDeadThreshold:    5,
		HeartbeatCommand:     "echo ping",
		HeartbeatTimeout:     5 * time.Second,
	}
}

// TmuxMonitorOption configures TmuxMonitor
type TmuxMonitorOption func(*TmuxMonitor)

// WithMonitorConfig sets the monitor configuration
func WithMonitorConfig(cfg MonitorConfig) TmuxMonitorOption {
	return func(tm *TmuxMonitor) {
		tm.interval = cfg.Interval
		tm.stableThreshold = cfg.StableThreshold
		tm.interactiveThreshold = cfg.InteractiveThreshold
		tm.fakeDeadThreshold = cfg.FakeDeadThreshold
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
		interval:             defaultCfg.Interval,
		stableThreshold:      defaultCfg.StableThreshold,
		interactiveThreshold: defaultCfg.InteractiveThreshold,
		fakeDeadThreshold:    defaultCfg.FakeDeadThreshold,
		heartbeatCommand:     defaultCfg.HeartbeatCommand,
		heartbeatTimeout:     defaultCfg.HeartbeatTimeout,
		sessions:             make(map[string]*TmuxSession),
		stopCh:               make(chan struct{}),
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

// detectSessionState detects the current state of a session
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

	// Check for fake alive (process exists but no output change)
	if processExists && !isPaneDead {
		if currentMD5 == session.LastOutputMD5 {
			session.StableCount++

			// Fake dead detection: exceeded threshold but process still running
			if session.StableCount > tm.fakeDeadThreshold {
				// TUI sessions: skip heartbeat to avoid injecting text via send-keys.
				// The agent has already received the Stable event (with StableSince),
				// and can make its own timeout/cleanup decision based on the output.
				if session.IsTUI {
					session.LastOutput = currentOutput
					session.LastOutputMD5 = currentMD5
					session.StableCount = tm.fakeDeadThreshold // cap to avoid unbounded growth
					return SessionRunning
				}

				heartbeatResult := tm.executor.SendHeartbeat(session.ID)
				if heartbeatResult == "ok" {
					// Process responds to heartbeat, it's fake alive (process is stuck)
					return SessionFakeAlive
				}
				// Heartbeat got no response.
				// Re-check pane dead status: the pane may have died during our
				// stable-count accumulation, in which case this is a normal
				// completion, not a fake dead.
				if tm.executor.IsPaneDead(session.ID) {
					session.LastOutput = currentOutput
					session.LastOutputMD5 = currentMD5
					return SessionCompleted
				}
				return SessionFakeDead
			}
		} else {
			// Output changed, reset stable count
			session.StableCount = 0
			session.StableSince = time.Time{} // new output cycle
		}
	}

	// Check for fake dead (process doesn't exist but pane not marked dead)
	if !processExists && !isPaneDead {
		session.LastOutput = currentOutput
		session.LastOutputMD5 = currentMD5
		return SessionCompleted
	}

	// Normal state detection
	if isPaneDead || !processExists {
		session.LastOutput = currentOutput
		session.LastOutputMD5 = currentMD5
		return SessionCompleted
	}

	// Output stability detection
	threshold := tm.getThreshold(session.IsInteractive)
	if session.StableCount >= threshold {
		if session.StableSince.IsZero() {
			session.StableSince = time.Now()
		}
		return SessionStable
	}

	// Update session state
	session.LastOutput = currentOutput
	session.LastOutputMD5 = currentMD5

	return SessionRunning
}

// handleFakeAlive handles fake alive state (process stuck but responsive)
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
		session.Status = SessionError
	}
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

// getThreshold returns the stability threshold based on session type
func (tm *TmuxMonitor) getThreshold(isInteractive bool) int {
	if isInteractive {
		return tm.interactiveThreshold
	}
	return tm.stableThreshold
}
