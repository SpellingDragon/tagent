package action

import (
	"crypto/md5"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ==================== Mock Inspector ====================

// mockInspector is a programmable sessionInspector for unit testing.
type mockInspector struct {
	mu sync.Mutex

	processExists bool
	isPaneDead    bool
	// paneDeadOnRecheck: if >= 0, on the Nth call to IsPaneDead, returns true instead of isPaneDead.
	// Used to simulate "pane died between initial check and post-heartbeat re-check".
	paneDeadOnRecheck int
	output            string
	outputErr         error
	heartbeatResp     string
	killErr           error
	restartErr        error

	// Call tracking
	processExistsCalls  int
	isPaneDeadCalls     int
	getOutputCalls      int
	sendHeartbeatCalls  int
	killSessionCalls    int
	restartSessionCalls int
}

func (m *mockInspector) ProcessExists(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processExistsCalls++
	return m.processExists
}

func (m *mockInspector) IsPaneDead(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isPaneDeadCalls++
	if m.paneDeadOnRecheck > 0 && m.isPaneDeadCalls >= m.paneDeadOnRecheck {
		return true
	}
	return m.isPaneDead
}

func (m *mockInspector) GetSessionOutput(sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getOutputCalls++
	return m.output, m.outputErr
}

func (m *mockInspector) SendHeartbeat(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendHeartbeatCalls++
	return m.heartbeatResp
}

func (m *mockInspector) KillSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killSessionCalls++
	return m.killErr
}

func (m *mockInspector) RestartSession(sessionID string, opts TmuxCreateOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restartSessionCalls++
	return m.restartErr
}

// setProcess sets the process state and clears call counters.
func (m *mockInspector) setProcess(exists, paneDead bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processExists = exists
	m.isPaneDead = paneDead
}

// setOutput sets the output state and clears call counters.
func (m *mockInspector) setOutput(output string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.output = output
	m.outputErr = err
}

// setHeartbeat sets the heartbeat response.
func (m *mockInspector) setHeartbeat(resp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeatResp = resp
}

// resetCallCounters resets all call counters.
func (m *mockInspector) resetCallCounters() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processExistsCalls = 0
	m.isPaneDeadCalls = 0
	m.getOutputCalls = 0
	m.sendHeartbeatCalls = 0
	m.killSessionCalls = 0
	m.restartSessionCalls = 0
}

// ==================== Test Helpers ====================

// newTestMonitor creates a TmuxMonitor with a mock inspector for testing.
// Uses very short durations so tests can verify state transitions with minimal time manipulation.
func newTestMonitor(inspector *mockInspector) *TmuxMonitor {
	return &TmuxMonitor{
		executor:                  inspector,
		interval:                  30 * time.Second,
		stableDuration:            10 * time.Millisecond,
		interactiveStableDuration: 10 * time.Millisecond,
		fakeDeadDuration:          1 * time.Hour, // effectively disabled for most tests
		heartbeatCommand:          "echo ping",
		sessions:                  make(map[string]*TmuxSession),
	}
}

// newTestSession creates a TmuxSession in a known state for testing.
func newTestSession(id string, status SessionStatus, lastOutput string) *TmuxSession {
	lastMD5 := ""
	if lastOutput != "" {
		lastMD5 = fmt.Sprintf("%x", md5.Sum([]byte(lastOutput)))
	}
	return &TmuxSession{
		ID:            id,
		Status:        status,
		LastOutput:    lastOutput,
		LastOutputMD5: lastMD5,
	}
}

// ==================== detectSessionState Tests ====================

func TestDetectSessionState_NilExecutor(t *testing.T) {
	tm := &TmuxMonitor{executor: nil}
	session := newTestSession("test", SessionRunning, "")

	status := tm.detectSessionState(session)
	if status != SessionError {
		t.Errorf("expected SessionError, got %s", status)
	}
}

func TestDetectSessionState_OutputError_ProcessExists(t *testing.T) {
	inspector := &mockInspector{
		processExists: true,
		outputErr:     fmt.Errorf("tmux not found"),
	}
	tm := newTestMonitor(inspector)
	session := newTestSession("test", SessionRunning, "")

	// When GetSessionOutput fails, the code uses empty output and continues
	// (pane may be dead but output still in buffer)
	status := tm.detectSessionState(session)
	if status != SessionRunning {
		t.Errorf("expected SessionRunning (output error is non-fatal), got %s", status)
	}
}

func TestDetectSessionState_OutputError_ProcessNotExists(t *testing.T) {
	inspector := &mockInspector{
		processExists: false,
		outputErr:     fmt.Errorf("tmux not found"),
	}
	tm := newTestMonitor(inspector)
	session := newTestSession("test", SessionRunning, "old_output")

	status := tm.detectSessionState(session)
	if status != SessionCompleted {
		t.Errorf("expected SessionCompleted, got %s", status)
	}
	// Bug fix verification: lastOutput should NOT be overwritten when GetSessionOutput fails
	// because we don't have a currentOutput to capture.
	if session.LastOutput != "old_output" {
		t.Errorf("expected LastOutput='old_output' (preserved), got %q", session.LastOutput)
	}
}

func TestDetectSessionState_OutputStable_TransitionToStable(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	// Simulate: output is "A" for two consecutive checks
	session := newTestSession("test", SessionRunning, "A")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))

	// Check 1: output unchanged "A" (same MD5) — StableSince set, but duration < threshold
	inspector.setOutput("A", nil)
	status := tm.detectSessionState(session)

	if status != SessionRunning {
		t.Errorf("check 1: expected SessionRunning, got %s", status)
	}
	if session.StableSince.IsZero() {
		t.Error("check 1: StableSince should be set when output first becomes unchanged")
	}

	// Advance time past stableDuration (10ms)
	time.Sleep(15 * time.Millisecond)

	// Check 2: output still "A" → non-interactive command should transition to Completed
	status = tm.detectSessionState(session)

	if status != SessionCompleted {
		t.Errorf("check 2: expected SessionCompleted for non-interactive command, got %s", status)
	}
	if session.LastOutput != "A" {
		t.Errorf("expected LastOutput='A', got %q", session.LastOutput)
	}
}

func TestDetectSessionState_OutputStable_InteractiveThreshold(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	// interactive sessions use same stableDuration in test setup (10ms)

	session := newTestSession("test", SessionRunning, "A")
	session.IsInteractive = true
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))

	inspector.setOutput("A", nil)

	// Check 1: StableSince set, but duration < threshold → Running
	tm.detectSessionState(session)
	if session.StableSince.IsZero() {
		t.Error("StableSince should be set on first unchanged output")
	}

	// Advance time past stableDuration
	time.Sleep(15 * time.Millisecond)

	// Check 2: duration ≥ threshold → Stable (interactive uses interactiveStableDuration)
	status := tm.detectSessionState(session)
	if status != SessionStable {
		t.Errorf("check 2: expected SessionStable (interactive), got %s", status)
	}
}

func TestDetectSessionState_HeartbeatNoResponse_PaneDead_ReturnsCompleted(t *testing.T) {
	// Bug3 fix verification: when heartbeat gets no_response but the pane
	// is actually dead, the session should be classified as Completed, not FakeDead.
	inspector := &mockInspector{
		processExists: true,
		isPaneDead:    true, // pane died while we were monitoring
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond // short duration to trigger fakeDead check

	session := newTestSession("test", SessionRunning, "final")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("final")))
	session.StableSince = time.Now().Add(-1 * time.Hour) // simulate being stable for a long time

	inspector.setOutput("final", nil)

	status := tm.detectSessionState(session)

	if status != SessionCompleted {
		t.Errorf("BUG: expected SessionCompleted (pane is dead), got %s", status)
	}
	// Final output MUST be captured
	if session.LastOutput != "final" {
		t.Errorf("BUG: LastOutput not captured! got %q", session.LastOutput)
	}
}

// ==================== checkSession Tests ====================

func TestCheckSession_NoStateChange_NoCallback(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "A")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))

	// Output unchanged, stableCount=0 → stays running
	inspector.setOutput("A", nil)

	var callbackCalled bool
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		callbackCalled = true
	}

	tm.checkSession(session)

	if callbackCalled {
		t.Error("expected no callback when state doesn't change")
	}
}

// ==================== Output Consistency Tests ====================

func TestOutputConsistency_MultipleUpdatesBeforeStable(t *testing.T) {
	// Simulate a realistic scenario: command produces output in chunks,
	// then stabilizes. Verify LastOutput tracks correctly through each step.
	// With new behavior: non-interactive commands go directly to completed after stable.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "")

	steps := []string{
		"line1\n",
		"line1\nline2\n",
		"line1\nline2\nline3\n",
		"line1\nline2\nline3\n", // stable
		"line1\nline2\nline3\n", // stable → completed (non-interactive)
	}

	for i, output := range steps {
		inspector.setOutput(output, nil)
		status := tm.detectSessionState(session)

		if i < 3 && status != SessionRunning {
			t.Errorf("step %d: expected running, got %s", i, status)
		}

		// Stable detection needs time to pass (time-based, not count-based)
		if i == 3 {
			time.Sleep(15 * time.Millisecond)
			status = tm.detectSessionState(session)
		}

		// Non-interactive command: stable → completed
		if i == 4 && status != SessionCompleted {
			t.Errorf("step %d: expected completed (non-interactive after stable), got %s", i, status)
		}
	}

	// After the sequence, LastOutput should hold the final stable output
	if session.LastOutput != "line1\nline2\nline3\n" && session.LastOutput != steps[2] {
		// After step 2 (index 2), the output for "line1\nline2\nline3\n" was set.
		// At step 3 (index 3), StableCount=1, detected as running (no state transition) but LastOutput updated.
		// At step 4 (index 4), StableCount=2 → stable → completed, LastOutput updated.
		// So LastOutput should be from step 4 (index 4): "line1\nline2\nline3\n"
		t.Errorf("LastOutput not consistent: got %q", session.LastOutput)
	}
}

func TestOutputConsistency_OutputMD5_EmptyOutput(t *testing.T) {
	// Edge case: empty output should also work for MD5 comparison
	// With new behavior: non-interactive commands go directly to completed after stable.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("")))

	inspector.setOutput("", nil)

	// Check 1: empty output unchanged → StableSince set
	tm.detectSessionState(session)
	if session.StableSince.IsZero() {
		t.Error("StableSince should be set for unchanged empty output")
	}

	// Advance time past stableDuration
	time.Sleep(15 * time.Millisecond)

	// Check 2: empty → completed (non-interactive after stable)
	status := tm.detectSessionState(session)
	if status != SessionCompleted {
		t.Errorf("expected SessionCompleted for consistent empty output (non-interactive), got %s", status)
	}
}

// ==================== Concurrency Tests ====================

func TestMonitor_ConcurrentSessionAccess(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	// Add sessions concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			session := newTestSession(
				fmt.Sprintf("session-%d", id),
				SessionRunning,
				fmt.Sprintf("output-%d", id),
			)
			tm.AddSession(session)
		}(i)
	}
	wg.Wait()

	// Verify all sessions were added
	sessions := tm.ListSessions()
	if len(sessions) != 10 {
		t.Errorf("expected 10 sessions, got %d", len(sessions))
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("session-%d", id)
			_, ok := tm.GetSession(name)
			if !ok {
				t.Errorf("session %s not found", name)
			}
		}(i)
	}
	wg.Wait()
}

// ==================== State Machine Tests ====================

func TestStateMachine_FullLifecycle_NormalCompletion(t *testing.T) {
	// Full lifecycle: running → stable → completed
	// With new behavior: non-interactive commands go directly from running → completed after stable
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("lifecycle", SessionRunning, "step1")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("step1")))

	tm.sessions["lifecycle"] = session

	type transition struct {
		output        string
		processExists bool
		isPaneDead    bool
		expectedState SessionStatus
	}

	lifecycle := []transition{
		// Phase 1: running with changing output
		{output: "step2", processExists: true, isPaneDead: false, expectedState: SessionRunning},
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionRunning},
		// Phase 2: output stabilizes (needs time to pass for Stable detection)
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionRunning},
		// Phase 3: non-interactive command completes after stable (running → completed)
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionCompleted},
	}

	var lastTransition string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		lastTransition = fmt.Sprintf("%s→%s output=%q", oldS, newS, output)
	}

	for i, step := range lifecycle {
		inspector.setProcess(step.processExists, step.isPaneDead)
		inspector.setOutput(step.output, nil)

		// For time-based stability detection, allow time to pass between stabilizing checks
		if i == 2 {
			// Step 2 sets StableSince; wait for duration to pass before step 3
			tm.checkSession(session)
			time.Sleep(15 * time.Millisecond)
			continue
		}

		oldStatus := session.Status
		tm.checkSession(session)

		if step.expectedState == SessionCompleted {
			// After completion, session is removed → get the transition result
			// With new behavior: running → completed (not stable → completed)
			if oldStatus != SessionRunning || lastTransition == "" {
				t.Errorf("step %d: expected running→completed transition, got %s→? transition=%q",
					i, oldStatus, lastTransition)
			}
			// Verify output captured
			if session.LastOutput != "step3" {
				t.Errorf("step %d: final output not captured! got %q", i, session.LastOutput)
			}
			break
		}

		if session.Status != step.expectedState {
			t.Errorf("step %d: expected %s, got %s (StableSince=%v)",
				i, step.expectedState, session.Status, session.StableSince)
		}
	}
}

func TestStateMachine_FakeDeadLifecycle(t *testing.T) {
	// Lifecycle: running → stable → (unchanged for too long) → fake_dead
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond // short to trigger fakeDead

	session := newTestSession("fake", SessionRunning, "X")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("X")))
	tm.sessions["fake"] = session

	inspector.setOutput("X", nil)

	// First check: sets StableSince
	tm.checkSession(session)
	if _, exists := tm.GetSession("fake"); !exists {
		t.Fatal("session should not be removed after first check")
	}

	// Set StableSince far in the past to simulate long stability
	session.StableSince = time.Now().Add(-1 * time.Hour)

	// Second check: fakeDead triggers because stableDuration > fakeDeadDuration
	tm.checkSession(session)

	// After fake_dead, session should be removed
	_, exists := tm.GetSession("fake")
	if exists {
		t.Error("BUG: session should be removed after fake_dead")
	}
}

// ==================== TUI Session Tests ====================

// newTUISession creates a TmuxSession with IsTUI=true for TUI testing.
func newTUISession(id string, status SessionStatus, lastOutput string) *TmuxSession {
	s := newTestSession(id, status, lastOutput)
	s.IsTUI = true
	return s
}

func TestTUI_OutputAlwaysChanging_StaysRunning(t *testing.T) {
	// TUI apps constantly redraw — output MD5 changes every check.
	// The session should stay Running forever, never reaching stable or triggering heartbeat.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTUISession("tui", SessionRunning, "frame1")

	// Simulate 10 checks with constantly changing TUI output
	for i := 0; i < 10; i++ {
		inspector.setOutput(fmt.Sprintf("frame_%d", i), nil)
		status := tm.detectSessionState(session)

		if status != SessionRunning {
			t.Errorf("check %d: TUI should stay Running, got %s", i, status)
		}
		if !session.StableSince.IsZero() {
			t.Errorf("check %d: TUI StableSince should remain zero (output changed), got %v", i, session.StableSince)
		}
	}

	// Heartbeat should NEVER be sent for TUI sessions
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times — should be 0!", inspector.sendHeartbeatCalls)
	}
}

func TestTUI_FakeDeadTimeout_ReturnsTimedOut_NoHeartbeat(t *testing.T) {
	// When a TUI session stays stable beyond fakeDeadDuration,
	// return TimedOut instead of triggering heartbeat/send-keys.
	// The session is removed from monitoring — the agent has already
	// received the Stable event and can decide whether to restart.
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond // short to trigger fakeDead

	session := newTUISession("tui", SessionRunning, "A")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))
	session.StableSince = time.Now().Add(-1 * time.Hour) // simulate being stable for a long time

	inspector.setOutput("A", nil)

	// stableDuration > fakeDeadDuration → TUI skip heartbeat → TimedOut
	status := tm.detectSessionState(session)

	if status != SessionTimedOut {
		t.Errorf("TUI at fakeDead should return TimedOut (skip heartbeat), got %s", status)
	}
	// Heartbeat must NOT be sent for TUI
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times — should be 0!", inspector.sendHeartbeatCalls)
	}
}

func TestTUI_FullLifecycle_RunningToCompleted(t *testing.T) {
	// TUI lifecycle: running → stable → completed.
	// TUI can naturally reach Stable when output stabilizes. The agent receives
	// the Stable event and can decide whether to send input or wait.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTUISession("tui_lifecycle", SessionRunning, "frame1")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("frame1")))
	tm.sessions["tui_lifecycle"] = session

	var transitions []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

	// Phase 1: TUI output stabilizes (same frame, then time passes)
	inspector.setOutput("frame1", nil)
	tm.checkSession(session)          // StableSince set → running (duration < threshold)
	time.Sleep(15 * time.Millisecond) // advance past stableDuration
	tm.checkSession(session)          // duration ≥ threshold → Stable

	if session.Status != SessionStable {
		t.Errorf("phase 1: TUI should reach Stable when idle, got %s", session.Status)
	}

	// Phase 2: Process exits
	inspector.setProcess(false, true)
	inspector.setOutput("frame1\ntui_final", nil)
	tm.checkSession(session)

	// Expected transitions: running→stable, stable→completed
	if len(transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d: %v", len(transitions), transitions)
	}
	if len(transitions) >= 2 && transitions[1] != "stable→completed" {
		t.Errorf("expected stable→completed, got %v", transitions)
	}

	// Session should be removed after completion
	_, exists := tm.GetSession("tui_lifecycle")
	if exists {
		t.Error("TUI session should be removed after completion")
	}

	// Heartbeat should never have been sent
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times", inspector.sendHeartbeatCalls)
	}
}

// ==================== Multi-Turn Conversation Tests ====================

func TestMultiTurn_RepeatedStableRunningCycles(t *testing.T) {
	// Simulates agent repeatedly sending input to an interactive session:
	// turn1: output changes → running → stable
	// turn2: output changes → running → stable
	// turn3: output changes → running → stable → completed
	// Each transition should fire exactly one callback.
	// Note: This test uses an interactive session (IsInteractive=true) to allow
	// multiple stable→running cycles. Non-interactive commands complete after stable.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 100 * time.Hour // Disable fake-dead to focus on multi-turn

	session := newTestSession("multiturn", SessionRunning, "init")
	session.IsInteractive = true // Allow multiple stable→running cycles
	tm.sessions["multiturn"] = session

	var transitions []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

	// === Turn 1: output arrives and stabilizes ===
	inspector.setOutput("Turn1: line1", nil)
	tm.checkSession(session) // changed → Running (stableCount=0)
	if session.Status != SessionRunning {
		t.Errorf("turn1 check1: expected Running, got %s", session.Status)
	}

	inspector.setOutput("Turn1: line1\nTurn1: line2", nil)
	tm.checkSession(session) // changed → Running

	// Same output twice → reach Stable (time-based: need sleep between checks)
	inspector.setOutput("Turn1: line1\nTurn1: line2", nil)
	tm.checkSession(session) // StableSince set
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session) // duration ≥ threshold → Stable
	if session.Status != SessionStable {
		t.Errorf("turn1: expected Stable, got %s", session.Status)
	}

	// === Turn 2: agent sends more input, output changes ===
	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...", nil)
	tm.checkSession(session) // changed → Running, stableCount reset
	if session.Status != SessionRunning {
		t.Errorf("turn2: expected Running (output changed), got %s", session.Status)
	}
	if !session.StableSince.IsZero() {
		t.Errorf("turn2: StableSince should reset to zero on output change, got %v", session.StableSince)
	}

	// Turn 2 stabilizes (time-based: 1st registers new output, 2nd confirms same, sleep, 3rd→Stable)
	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...\nTurn2: done", nil)
	tm.checkSession(session) // new output → StableSince reset
	tm.checkSession(session) // same → StableSince set, duration < threshold
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session) // same → duration ≥ threshold → Stable
	if session.Status != SessionStable {
		t.Errorf("turn2: expected Stable, got %s", session.Status)
	}

	// === Turn 3: final output then process exits ===
	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...\nTurn2: done\nTurn3: final", nil)
	tm.checkSession(session) // changed → Running
	if session.Status != SessionRunning {
		t.Errorf("turn3: expected Running, got %s", session.Status)
	}

	inspector.setProcess(false, true)
	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...\nTurn2: done\nTurn3: final\nEXIT", nil)
	tm.checkSession(session) // completed

	// Expected transitions:
	// running→stable (turn1), stable→running (turn2), running→stable (turn2),
	// stable→running (turn3), running→completed (final)
	expectedCount := 5
	if len(transitions) != expectedCount {
		t.Errorf("expected %d transitions, got %d: %v", expectedCount, len(transitions), transitions)
	}

	// Verify specific key transitions
	hasStableToRunning := false
	hasRunningToCompleted := false
	for _, tr := range transitions {
		if tr == "stable→running" {
			hasStableToRunning = true
		}
		if tr == "running→completed" {
			hasRunningToCompleted = true
		}
	}
	if !hasStableToRunning {
		t.Error("missing stable→running transition (agent re-engaging)")
	}
	if !hasRunningToCompleted {
		t.Error("missing running→completed transition")
	}

	// Session should be removed
	_, exists := tm.GetSession("multiturn")
	if exists {
		t.Error("session should be removed after completion")
	}
}

func TestMultiTurn_LongSession_FiftyChecks(t *testing.T) {
	// Simulates a long-running session (e.g., build process) that survives
	// 50+ monitor checks. Output changes every ~5th check, no spurious events.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 100 * time.Hour // Disable fake-dead for this test

	session := newTestSession("longrun", SessionRunning, "")
	tm.sessions["longrun"] = session

	var callbacks int
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		callbacks++
	}

	baseOutput := "build output line "
	phase := 0 // 0=changing, 1=stabilizing, 2=stable, repeat

	for i := 0; i < 50; i++ {
		// Every ~5 checks, change output (simulating new build output)
		if i%5 == 0 {
			phase = 0
		}

		var output string
		switch phase {
		case 0: // just changed
			output = fmt.Sprintf("%s%d\n%s%d", baseOutput, i, baseOutput, i+1)
			phase = 1
		case 1: // same as previous
			output = fmt.Sprintf("%s%d\n%s%d", baseOutput, i-1, baseOutput, i)
			phase = 2
		default: // stable
			output = fmt.Sprintf("%s%d\n%s%d", baseOutput, i-2, baseOutput, i-1)
		}

		inspector.setOutput(output, nil)
		oldStatus := session.Status
		tm.checkSession(session)

		// Verify state consistency
		if session.Status == SessionCompleted || session.Status == SessionError {
			t.Errorf("check %d: session terminated unexpectedly (%s)", i, session.Status)
			break
		}

		// No duplicate callbacks for same state
		if session.Status == oldStatus && callbacks > 0 {
			// OK — callbacks only fire on transitions
		}
	}

	t.Logf("Long session: 50 checks, %d state change callbacks", callbacks)

	// With threshold=2 and output changing every ~5 checks, we expect roughly:
	// 10 changes × 2 transitions each = ~20 callbacks
	// But exact count depends on timing; just verify no explosion
	if callbacks > 100 {
		t.Errorf("too many callbacks (%d) — possible instability", callbacks)
	}
	// Verify StableSince resets correctly on output changes (not stuck accumulating)
	// After final stable period, StableSince should reflect recent stability, not ancient history
	if !session.StableSince.IsZero() && time.Since(session.StableSince) > 30*time.Second {
		t.Errorf("StableSince too old (%v) — should reset on each output change", time.Since(session.StableSince))
	}
}

func TestMultiTurn_OutputAccumulation(t *testing.T) {
	// Verifies that LastOutput correctly accumulates output across multiple
	// interaction turns, and the agent always sees the full history.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 100 * time.Hour

	session := newTestSession("accum", SessionRunning, "")

	type turn struct {
		output   string
		expected string // expected session.LastOutput after this turn
	}

	turns := []turn{
		{output: "step1: init\n", expected: "step1: init\n"},
		{output: "step1: init\nstep2: build\n", expected: "step1: init\nstep2: build\n"},
		{output: "step1: init\nstep2: build\nstep3: test\n", expected: "step1: init\nstep2: build\nstep3: test\n"},
	}

	for i, turn := range turns {
		inspector.setOutput(turn.output, nil)
		tm.detectSessionState(session)

		if session.LastOutput != turn.expected {
			t.Errorf("turn %d: LastOutput mismatch\n  got:  %q\n  want: %q",
				i, session.LastOutput, turn.expected)
		}
	}

	// After stabilization, LastOutput should hold the full accumulated output
	if session.LastOutput != "step1: init\nstep2: build\nstep3: test\n" {
		t.Errorf("final LastOutput not accumulated correctly: %q", session.LastOutput)
	}
}

func TestMultiTurn_TUI_AgentGetsStableEvent(t *testing.T) {
	// Simulates a TUI multi-turn conversation. The agent should:
	// 1. Get Running→Stable when TUI output stabilizes
	// 2. Get Stable→Running when TUI output changes (agent sent input)
	// 3. Get Stable→Running when fakeDead timeout expires (no heartbeat)
	// This gives the agent enough information to make decisions.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTUISession("tui_mt", SessionRunning, "screen1")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("screen1")))
	tm.sessions["tui_mt"] = session

	var events []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		events = append(events, fmt.Sprintf("%s→%s output_len=%d", oldS, newS, len(output)))
	}

	// Round 1: TUI stabilizes (time-based: need sleep between checks)
	inspector.setOutput("screen1", nil)
	tm.checkSession(session) // StableSince set → Running
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session) // duration ≥ threshold → Stable

	if session.Status != SessionStable {
		t.Fatalf("round 1: expected Stable, got %s", session.Status)
	}

	// Round 2: Agent sends input, TUI screen changes
	inspector.setOutput("screen1\nscreen2: processing your request...", nil)
	tm.checkSession(session) // changed → Running
	if session.Status != SessionRunning {
		t.Fatalf("round 2: expected Running after change, got %s", session.Status)
	}

	// Round 2 output stabilizes (need time to pass)
	tm.checkSession(session) // StableSince set
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session) // duration ≥ threshold → Stable
	if session.Status != SessionStable {
		t.Errorf("round 2: expected Stable, got %s", session.Status)
	}

	// Round 3: FakeDead timeout — stays stable too long.
	// Simulate staying stable past fakeDeadDuration by setting StableSince far in the past.
	// Phase 5: TUI sessions now return SessionTimedOut (removed) instead of SessionRunning.
	session.StableSince = time.Now().Add(-2 * time.Hour)
	inspector.setOutput("screen1\nscreen2: processing your request...", nil)
	tm.checkSession(session) // duration > fakeDeadDuration → TUI → TimedOut (removed)

	// Agent should have received: stable→timed_out (fake-dead timeout)
	// The session is now marked for removal.
	if session.Status != SessionTimedOut {
		t.Errorf("round 3: expected TimedOut after fakeDead timeout, got %s", session.Status)
	}

	// Verify heartbeat was NEVER sent
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI heartbeat calls: %d (should be 0)", inspector.sendHeartbeatCalls)
	}

	// Verify all expected events fired
	// running→stable (round1), stable→running (round2), running→stable (round2),
	// stable→timed_out (round3 fakeDead timeout)
	if len(events) < 3 {
		t.Errorf("expected at least 3 events, got %d: %v", len(events), events)
	}

	t.Logf("TUI multi-turn events: %v", events)
}

func TestCallback_ReceiveExactOutput(t *testing.T) {
	// Verify that callback receives the exact output string, not truncated or modified,
	// for the "running→completed" transition.
	inspector := &mockInspector{
		processExists: false,
		isPaneDead:    true,
	}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "")
	largeOutput := ""
	for i := 0; i < 100; i++ {
		largeOutput += fmt.Sprintf("line %d: some output data here for testing\n", i)
	}
	inspector.setOutput(largeOutput, nil)

	var receivedOutput string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		receivedOutput = output
	}

	tm.sessions["test"] = session
	tm.checkSession(session)

	if receivedOutput != largeOutput {
		t.Errorf("callback output mismatch: lengths: got=%d want=%d",
			len(receivedOutput), len(largeOutput))
	}
}

// ==================== FakeAlive Path Tests ====================

func TestDetectSessionState_FakeAlive_HeartbeatOk(t *testing.T) {
	// When heartbeat returns "ok", the session is FakeAlive (process stuck but responsive).
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "ok",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("fakealive", SessionRunning, "unchanged")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("unchanged")))
	session.StableSince = time.Now().Add(-1 * time.Hour) // simulate long stability

	inspector.setOutput("unchanged", nil)

	status := tm.detectSessionState(session)

	if status != SessionFakeAlive {
		t.Errorf("expected SessionFakeAlive, got %s", status)
	}
	if inspector.sendHeartbeatCalls != 1 {
		t.Errorf("expected 1 heartbeat, got %d", inspector.sendHeartbeatCalls)
	}
}

// ==================== FakeDead Re-check Test ====================

func TestDetectSessionState_FakeDead_RecheckPaneDead(t *testing.T) {
	// When heartbeat returns "no_response", a SECOND IsPaneDead check fires (line 321).
	// If the pane died during the stable-accumulation period, it'''s Completed, not FakeDead.
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "no_response",
	}
	// First IsPaneDead call (line 252) → false (session appears alive)
	// Second IsPaneDead call (line 321) → true (pane died during the check)
	inspector.isPaneDead = false
	inspector.paneDeadOnRecheck = 2

	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("recheck", SessionRunning, "stable_output")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("stable_output")))
	session.StableSince = time.Now().Add(-1 * time.Hour)

	inspector.setOutput("stable_output", nil)

	status := tm.detectSessionState(session)

	if status != SessionCompleted {
		t.Errorf("expected SessionCompleted (pane died on re-check), got %s", status)
	}
	if session.LastOutput != "stable_output" {
		t.Errorf("LastOutput not captured! got %q", session.LastOutput)
	}
	if inspector.sendHeartbeatCalls != 1 {
		t.Errorf("expected 1 heartbeat, got %d", inspector.sendHeartbeatCalls)
	}
}

// ==================== handleFakeAlive Full Flow Tests ====================

func TestHandleFakeAlive_RestartSuccess_ContinuesTracking(t *testing.T) {
	// Full flow: Running→FakeAlive→restart succeeds→state resets→next check→FakeAlive→Running.
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "ok",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("restart_ok", SessionRunning, "stuck")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("stuck")))
	session.StableSince = time.Now().Add(-1 * time.Hour)
	tm.sessions["restart_ok"] = session

	inspector.setOutput("stuck", nil)

	var transitions []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

	// Step 1: detectSessionState → FakeAlive → handleFakeAlive (restart succeeds)
	tm.checkSession(session)

	if inspector.restartSessionCalls != 1 {
		t.Errorf("expected 1 restart call, got %d", inspector.restartSessionCalls)
	}
	if !session.StableSince.IsZero() {
		t.Error("StableSince should be reset after successful restart")
	}
	if session.LastOutput != "" {
		t.Errorf("LastOutput should be empty after restart, got %q", session.LastOutput)
	}
	// Status should still be FakeAlive (set by checkSession, NOT changed by handleFakeAlive)
	if session.Status != SessionFakeAlive {
		t.Errorf("expected Status=FakeAlive (set by checkSession), got %s", session.Status)
	}
	if _, exists := tm.GetSession("restart_ok"); !exists {
		t.Fatal("session should still be tracked after FakeAlive restart")
	}

	// Step 2: Next check — restarted session produces new output.
	inspector.setOutput("fresh_output", nil)
	oldStatus := session.Status // FakeAlive
	tm.checkSession(session)

	if session.Status != SessionRunning {
		t.Errorf("after restart, expected Running, got %s (was %s)", session.Status, oldStatus)
	}

	// Verify FakeAlive→Running transition fired
	hasFakeAliveToRunning := false
	for _, tr := range transitions {
		if tr == "fake_alive→running" {
			hasFakeAliveToRunning = true
		}
	}
	if !hasFakeAliveToRunning {
		t.Errorf("missing fake_alive→running transition, got: %v", transitions)
	}

	// Verify expected transition count: running→fake_alive + fake_alive→running = 2
	if len(transitions) < 2 {
		t.Errorf("expected at least 2 transitions, got %d: %v", len(transitions), transitions)
	}
}

func TestHandleFakeAlive_RestartFailure_StaysFakeAlive(t *testing.T) {
	// When restart fails, handleFakeAlive leaves Status untouched.
	// The session stays in the map; the next cycle re-evaluates naturally.
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "ok",
		restartErr:    fmt.Errorf("tmux unavailable"),
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("restart_fail", SessionRunning, "stuck")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("stuck")))
	session.StableSince = time.Now().Add(-1 * time.Hour)
	tm.sessions["restart_fail"] = session

	inspector.setOutput("stuck", nil)

	var callbackFired bool
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		callbackFired = true
	}

	tm.checkSession(session)

	if !callbackFired {
		t.Error("callback should fire on running→fake_alive transition")
	}
	if inspector.restartSessionCalls != 1 {
		t.Errorf("expected 1 restart attempt, got %d", inspector.restartSessionCalls)
	}
	// Status stays FakeAlive (NOT changed to Error — let next cycle re-evaluate)
	if session.Status != SessionFakeAlive {
		t.Errorf("expected Status=FakeAlive (untouched on failure), got %s", session.Status)
	}
	// Session remains tracked
	if _, exists := tm.GetSession("restart_fail"); !exists {
		t.Error("session should remain tracked after failed restart")
	}
}

// ==================== handleStateChange Output Truncation Test ====================

// mockInjector records injected messages for testing handleStateChange.
type mockInjector struct {
	mu       sync.Mutex
	messages []model.Message
}

func (m *mockInjector) InjectMessageWithSource(source string, msg model.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

func (m *mockInjector) lastContent() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1].Content
}

func TestHandleStateChange_OutputTruncation(t *testing.T) {
	injector := &mockInjector{}
	ct := &ActionTool{
		injector:    injector,
		tmuxMonitor: NewTmuxMonitor(WithMonitorExecutor(&mockInspector{processExists: true})),
	}

	// Add a session with known state
	session := &TmuxSession{
		ID:          "trunc_test",
		Status:      SessionRunning,
		StableSince: time.Now().Add(-30 * time.Second),
	}
	ct.tmuxMonitor.AddSession(session)

	// Sub-test 1: output <= 2000 chars — no truncation
	shortOutput := "short output line\n"
	ct.handleStateChange("trunc_test", string(SessionRunning), string(SessionStable), shortOutput)

	content := injector.lastContent()
	if content == "" {
		t.Fatal("expected injected message")
	}
	if strings.Contains(content, "...(truncated)") {
		t.Error("short output should NOT have truncation marker")
	}

	// Sub-test 2: output > 2000 chars — saved to file with tail shown
	largeLine := "line of output data that will eventually be truncated because too long\n"
	largeOutput := strings.Repeat(largeLine, 100) // >2000 chars

	ct.handleStateChange("trunc_test", string(SessionStable), string(SessionRunning), largeOutput)

	content = injector.lastContent()
	// Should indicate full output was saved to file
	if !strings.Contains(content, "完整内容已保存到") {
		t.Error("large output should indicate full content saved to file")
	}
	// Should show last 2000 characters
	if !strings.Contains(content, "显示最后 2000 字符") {
		t.Error("large output should indicate showing last 2000 characters")
	}
	// After truncation, the message should NOT contain the full output
	if strings.Contains(content, largeOutput) {
		t.Error("full large output should NOT appear in message")
	}
	// But the tail should be present
	if !strings.Contains(content, largeLine) {
		t.Error("truncated tail of output should appear in message")
	}
}
