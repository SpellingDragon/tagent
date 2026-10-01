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
//
// 契约: docs/wiki/tool/tmux-action.md#notify-gates
type TmuxMonitor struct {
	executor                  sessionInspector
	interval                  time.Duration
	stableDuration            time.Duration
	interactiveStableDuration time.Duration
	fakeDeadDuration          time.Duration
	heartbeatCommand          string
	heartbeatTimeout          time.Duration
	// probeUnknownLimitN（R3）：fail-dead 加闸阈值（拍平配置，0→default 3）。
	probeUnknownLimitN int

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
	// PaneDeadStatus（failure-polarity passthrough D1）：读死 pane 退出码；
	// known=false 表示状态不可辨（pane 活/会话消失/命令失败），调用方按 unknown 处理。
	PaneDeadStatus(sessionID string) (code int, known bool)
	// SessionAlive3（R3）：三态探测——list-sessions 单源；known=false 表示命令
	// 不可辨（monitor 计数加闸，不立即判死）。
	SessionAlive3(sessionID string) (alive, known bool)
	GetSessionOutput(sessionID string) (string, error)
	SendHeartbeat(sessionID string) string
	KillSession(sessionID string) error
	RestartSession(sessionID string, opts TmuxCreateOptions) error
}

// compile-time check: TmuxExecutor implements sessionInspector.
var _ sessionInspector = (*TmuxExecutor)(nil)

// MonitorConfig holds configuration for TmuxMonitor
type MonitorConfig struct {
	// Interval 基础轮询节奏（自适应调度下的上限见 MaxInterval）。
	Interval time.Duration
	// StableDuration 输出稳定判定阈值。
	StableDuration time.Duration
	// InteractiveStableDuration TUI 会话的稳定判定阈值。
	InteractiveStableDuration time.Duration
	// FakeDeadDuration 假死判定阈值。
	FakeDeadDuration time.Duration
	// HeartbeatCommand 探活所用命令。
	HeartbeatCommand string
	// HeartbeatTimeout 探活命令的超时。
	HeartbeatTimeout time.Duration

	// DenseInterval Adaptive poll schedule (optional; unset fields fall back to defaults, with
	// DenseInterval derived from Interval). See PollSchedule.
	DenseInterval time.Duration
	DenseDuration time.Duration
	BackoffFactor float64
	MaxInterval   time.Duration

	// ProbeUnknownLimit（R3）：连续 unknown 探测加闸阈值——达到才按 dead 处理。
	// 0 → defaultProbeUnknownLimit（3）。
	ProbeUnknownLimit int
}

// defaultProbeUnknownLimit is the fail-dead gate threshold : how many
// consecutive UNKNOWN probes before a session is treated as dead.
const defaultProbeUnknownLimit = 3

func (tm *TmuxMonitor) probeUnknownLimit() int {
	if tm.probeUnknownLimitN > 0 {
		return tm.probeUnknownLimitN
	}
	return defaultProbeUnknownLimit
}

// DefaultMonitorConfig returns default monitor configuration
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
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
		tm.probeUnknownLimitN = cfg.ProbeUnknownLimit
		tm.heartbeatCommand = cfg.HeartbeatCommand
		tm.heartbeatTimeout = cfg.HeartbeatTimeout
		if cfg.Interval > 0 {
			tm.schedule.DenseInterval = cfg.Interval
		}
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
	sched.DenseInterval = defaultCfg.Interval

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

// RebindCallback replaces the per-session callback of an ALREADY-MONITORED
// session (cross-restart resume builds a fresh detector that must take over
// the state-change supply; the old binding belongs to a detector from a
// previous generation). Returns false when the session is not monitored —
// rebinding never (re-)adds a session, that stays AddSessionWithCallback's
// contract.
func (tm *TmuxMonitor) RebindCallback(sessionID string, cb func(sessionID string, oldStatus, newStatus SessionStatus, output string)) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, ok := tm.sessions[sessionID]; !ok {
		return false
	}
	if tm.sessionCallbacks == nil {
		tm.sessionCallbacks = make(map[string]func(sessionID string, oldStatus, newStatus SessionStatus, output string))
	}
	tm.sessionCallbacks[sessionID] = cb
	return true
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
// session has already been reaped (not monitored) so the caller can surface the
// error. The per-session callback is NOT touched — the detector is bound to
// the session for its whole lifetime (see TmuxSettleDetector.Rearm).
func (tm *TmuxMonitor) TouchSession(sessionID string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	session, ok := tm.sessions[sessionID]
	if !ok {
		return false
	}
	session.CreatedAt = time.Now()
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
				continue
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
// cadence . A removed (terminal) session drops its schedule.
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

	shouldCallback := false
	if (globalCb != nil || sessCb != nil) &&
		isMeaningfulState(oldStatus) && isMeaningfulState(newStatus) {
		lastNotified := tm.lastNotifiedStatus[sessionID]
		if lastNotified != newStatus {
			tm.lastNotifiedStatus[sessionID] = newStatus
			shouldCallback = true
		}
	}

	if shouldRemove {
		delete(tm.sessionCallbacks, sessionID)
		delete(tm.lastNotifiedStatus, sessionID)
	}

	tm.mu.Unlock()

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

// stableWindow detectSessionState detects the current state of a session.
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

// deathPolarity decides the settle status for a detected process death
// (failure-polarity passthrough D1): a KNOWN non-zero exit code (including a
// negative signal death) is reported as SessionError so the failure reaches the
// notification; a clean exit (0) or an unresolvable status keeps the current
// completed polarity. Reading the code is the single source of failure truth —
// the monitor does not guess success.
func (tm *TmuxMonitor) deathPolarity(sessionID string) SessionStatus {
	if code, known := tm.executor.PaneDeadStatus(sessionID); known && code != 0 {
		return SessionError
	}
	return SessionCompleted
}

// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
func (tm *TmuxMonitor) detectSessionState(session *TmuxSession) SessionStatus {
	if tm.executor == nil {
		return SessionError
	}

	alive, known := tm.executor.SessionAlive3(session.ID)
	if !known {
		session.ProbeUnknownCount++
		if session.ProbeUnknownCount < tm.probeUnknownLimit() {
			log.Warnf("[TmuxMonitor] probe UNKNOWN for %s (%d/%d) — keeping session alive",
				session.ID, session.ProbeUnknownCount, tm.probeUnknownLimit())
			return SessionRunning
		}
		log.Errorf("[TmuxMonitor] probe UNKNOWN %d consecutive times for %s — treating as dead",
			session.ProbeUnknownCount, session.ID)
		processExists, isPaneDead := false, true
		currentOutput, err := tm.executor.GetSessionOutput(session.ID)
		if err != nil {
			currentOutput = ""
		}
		if strings.TrimSpace(currentOutput) != "" {
			session.LastOutput = currentOutput
			session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte(currentOutput)))
		}
		_ = processExists
		_ = isPaneDead
		return SessionError
	}
	session.ProbeUnknownCount = 0

	processExists := tm.executor.ProcessExists(session.ID)
	if !alive {
		processExists = false
	}
	isPaneDead := tm.executor.IsPaneDead(session.ID)

	currentOutput, err := tm.executor.GetSessionOutput(session.ID)
	if err != nil {
		currentOutput = ""
	}

	currentMD5 := fmt.Sprintf("%x", md5.Sum([]byte(currentOutput)))

	if processExists && !isPaneDead {
		if currentMD5 == session.LastOutputMD5 {
			if session.StableSince.IsZero() {
				session.StableSince = time.Now()
			}
		} else {
			session.StableSince = time.Time{}
		}
	}

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
		if lastErr == nil && strings.TrimSpace(capture) != "" {
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
		return tm.deathPolarity(session.ID)
	}

	if !session.StableSince.IsZero() {
		stableDuration := time.Since(session.StableSince)

		if session.Mode == ModeResident {
			session.LastOutput = currentOutput
			session.LastOutputMD5 = currentMD5
			return SessionRunning
		}

		if stableDuration > tm.fakeDeadThreshold(session) {
			outputPreview := currentOutput
			if len(outputPreview) > 200 {
				outputPreview = outputPreview[:200] + "..."
			}
			log.Infof("[TmuxMonitor] session %s entering fake check: stableDuration=%s fakeDeadThreshold=%s command=%q isInteractive=%v isTUI=%v output(len=%d): %q",
				session.ID, stableDuration, tm.fakeDeadThreshold(session), session.Command, session.IsInteractive, session.IsTUI, len(currentOutput), outputPreview)

			if session.IsTUI {
				session.LastOutput = currentOutput
				session.LastOutputMD5 = currentMD5
				return SessionTimedOut
			}

			if !session.IsInteractive {
				if tm.executor.IsPaneDead(session.ID) {
					session.LastOutput = currentOutput
					session.LastOutputMD5 = currentMD5
					return tm.deathPolarity(session.ID)
				}
				if session.QuietTimeout > 0 {
					log.Infof("[TmuxMonitor] session %s quiet beyond declared quiet_timeout=%s, engaging fake-dead kill",
						session.ID, session.QuietTimeout)
					return SessionFakeDead
				}
				log.Infof("[TmuxMonitor] session %s quiet beyond %s but process alive and no explicit quiet_timeout — keeping stable (no auto-kill)",
					session.ID, tm.fakeDeadThreshold(session))
				return SessionStable
			}

			heartbeatResult := tm.executor.SendHeartbeat(session.ID)
			afterOutput, _ := tm.executor.GetSessionOutput(session.ID)
			afterPreview := afterOutput
			if len(afterPreview) > 200 {
				afterPreview = afterPreview[:200] + "..."
			}
			log.Infof("[TmuxMonitor] session %s heartbeat result=%q (output before len=%d, after len=%d, after preview): %q",
				session.ID, heartbeatResult, len(currentOutput), len(afterOutput), afterPreview)
			if heartbeatResult == "ok" {
				return SessionFakeAlive
			}
			if tm.executor.IsPaneDead(session.ID) {
				session.LastOutput = currentOutput
				session.LastOutputMD5 = currentMD5
				return tm.deathPolarity(session.ID)
			}
			return SessionFakeDead
		}

		threshold := tm.getStableDuration(session.IsInteractive)
		if stableDuration >= threshold {
			session.LastOutput = currentOutput
			session.LastOutputMD5 = currentMD5
			log.Infof("[TmuxMonitor] session %s output stable for %s (process alive) — reporting stable, NOT completed",
				session.ID, stableDuration)
			return SessionStable
		}
	}

	session.LastOutput = currentOutput
	session.LastOutputMD5 = currentMD5

	return SessionRunning
}

// handleFakeAlive attempts to recover a session judged fake-alive; on failure it
// leaves Status untouched. The recovery strategy — which identity is preserved, how
// stability metadata is reset, and where the state machine is expected to move
// afterwards — is specified in the document below.
//
// 契约: docs/wiki/tool/tmux-action.md#fake-alive-restart
func (tm *TmuxMonitor) handleFakeAlive(session *TmuxSession) {
	log.Infof("[TmuxMonitor] session %s is fake alive (command=%q, isInteractive=%v, isTUI=%v), attempting restart",
		session.ID, session.Command, session.IsInteractive, session.IsTUI)

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

	if err := tm.executor.KillSession(session.ID); err != nil {
		session.KillRetryCount++
		log.Errorf("[TmuxMonitor] failed to kill session %s (retry=%d): %v",
			session.ID, session.KillRetryCount, err)

		if session.KillRetryCount < 3 {
			session.Status = SessionStable
			return false
		}
		log.Warnf("[TmuxMonitor] session %s reached max kill retries, force-removing", session.ID)
		session.Status = SessionError
		return true
	}

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
