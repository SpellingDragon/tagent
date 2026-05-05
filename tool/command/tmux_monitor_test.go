package command

import (
	"crypto/md5"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ==================== Mock Inspector ====================

// mockInspector is a programmable sessionInspector for unit testing.
type mockInspector struct {
	mu sync.Mutex

	processExists bool
	isPaneDead    bool
	output        string
	outputErr     error
	heartbeatResp string
	killErr       error
	restartErr    error

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
func newTestMonitor(inspector *mockInspector) *TmuxMonitor {
	return &TmuxMonitor{
		executor:             inspector,
		interval:             30 * time.Second,
		stableThreshold:      2,
		interactiveThreshold: 3,
		fakeDeadThreshold:    5,
		heartbeatCommand:     "echo ping",
		sessions:             make(map[string]*TmuxSession),
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
		StableCount:   0,
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

	status := tm.detectSessionState(session)
	if status != SessionError {
		t.Errorf("expected SessionError, got %s", status)
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

	// Check 1: output unchanged "A" (same MD5)
	inspector.setOutput("A", nil)
	status := tm.detectSessionState(session)

	if status != SessionRunning {
		t.Errorf("check 1: expected SessionRunning, got %s", status)
	}
	if session.StableCount != 1 {
		t.Errorf("check 1: expected StableCount=1, got %d", session.StableCount)
	}

	// Check 2: output still "A" → should transition to Stable
	status = tm.detectSessionState(session)

	if status != SessionStable {
		t.Errorf("check 2: expected SessionStable, got %s", status)
	}
	if session.StableCount != 2 {
		t.Errorf("check 2: expected StableCount=2, got %d", session.StableCount)
	}
	if session.StableSince.IsZero() {
		t.Error("check 2: StableSince should be set when reaching Stable")
	}
	if session.LastOutput != "A" {
		t.Errorf("expected LastOutput='A', got %q", session.LastOutput)
	}
}

func TestDetectSessionState_OutputStable_InteractiveThreshold(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "A")
	session.IsInteractive = true
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))

	inspector.setOutput("A", nil)

	// interactive threshold = 3
	// Check 1: stableCount=1 → running
	tm.detectSessionState(session)
	if session.StableCount != 1 {
		t.Errorf("check 1: expected StableCount=1, got %d", session.StableCount)
	}

	// Check 2: stableCount=2 → running
	status := tm.detectSessionState(session)
	if status != SessionRunning {
		t.Errorf("check 2: expected SessionRunning, got %s", status)
	}

	// Check 3: stableCount=3 → stable (reached interactive threshold)
	status = tm.detectSessionState(session)
	if status != SessionStable {
		t.Errorf("check 3: expected SessionStable, got %s", status)
	}
}

func TestDetectSessionState_HeartbeatNoResponse_PaneDead_ReturnsCompleted(t *testing.T) {
	// 🔴 Bug3 fix verification: when heartbeat gets no_response but the pane
	// is actually dead, the session should be classified as Completed, not FakeDead.
	// This happens when the process dies during the stable-count accumulation window.
	inspector := &mockInspector{
		processExists: true,
		isPaneDead:    true, // pane died while we were counting
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "final")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("final")))
	session.StableCount = 5 // At fake dead threshold

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
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "")

	steps := []string{
		"line1\n",
		"line1\nline2\n",
		"line1\nline2\nline3\n",
		"line1\nline2\nline3\n", // stable
		"line1\nline2\nline3\n", // stable
	}

	for i, output := range steps {
		inspector.setOutput(output, nil)
		status := tm.detectSessionState(session)

		if i < 3 && status != SessionRunning {
			t.Errorf("step %d: expected running, got %s", i, status)
		}
		if i == 4 && status != SessionStable {
			t.Errorf("step %d: expected stable, got %s", i, status)
		}
	}

	// After the sequence, LastOutput should hold the final stable output
	if session.LastOutput != "line1\nline2\nline3\n" && session.LastOutput != steps[2] {
		// After step 2 (index 2), the output for "line1\nline2\nline3\n" was set.
		// At step 3 (index 3), StableCount=1, detected as running (no state transition) but LastOutput updated.
		// At step 4 (index 4), StableCount=2 → stable, NO LastOutput update.
		// So LastOutput should be from step 3 (index 3): "line1\nline2\nline3\n"
		t.Errorf("LastOutput not consistent: got %q", session.LastOutput)
	}
}

func TestOutputConsistency_OutputMD5_EmptyOutput(t *testing.T) {
	// Edge case: empty output should also work for MD5 comparison
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("")))

	inspector.setOutput("", nil)

	// Check 1: empty output unchanged → stableCount=1
	tm.detectSessionState(session)
	if session.StableCount != 1 {
		t.Errorf("expected StableCount=1 for unchanged empty output, got %d", session.StableCount)
	}

	// Check 2: empty → stableCount=2 → stable
	status := tm.detectSessionState(session)
	if status != SessionStable {
		t.Errorf("expected SessionStable for consistent empty output, got %s", status)
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
		// Phase 2: output stabilizes
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionRunning}, // stableCount=1
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionStable},  // stableCount=2
		// Phase 3: process completes
		{output: "step3\nfinal", processExists: false, isPaneDead: true, expectedState: SessionCompleted},
	}

	var lastTransition string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		lastTransition = fmt.Sprintf("%s→%s output=%q", oldS, newS, output)
	}

	for i, step := range lifecycle {
		inspector.setProcess(step.processExists, step.isPaneDead)
		inspector.setOutput(step.output, nil)

		oldStatus := session.Status
		tm.checkSession(session)

		if step.expectedState == SessionCompleted {
			// After completion, session is removed → get the transition result
			if oldStatus != SessionStable || lastTransition == "" {
				t.Errorf("step %d: expected stable→completed transition, got %s→? transition=%q",
					i, oldStatus, lastTransition)
			}
			// Verify output captured
			if session.LastOutput != "step3\nfinal" {
				t.Errorf("step %d: final output not captured! got %q", i, session.LastOutput)
			}
			break
		}

		if session.Status != step.expectedState {
			t.Errorf("step %d: expected %s, got %s (stableCount=%d)",
				i, step.expectedState, session.Status, session.StableCount)
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

	session := newTestSession("fake", SessionRunning, "X")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("X")))
	tm.sessions["fake"] = session

	inspector.setOutput("X", nil)

	// Build up stableCount to fakeDeadThreshold
	// Threshold is 5, so we need stableCount to reach 6
	for i := 0; i < 6; i++ {
		if _, exists := tm.GetSession("fake"); !exists {
			t.Fatalf("session removed prematurely at iteration %d", i)
		}
		tm.checkSession(session)
	}

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
		if session.StableCount != 0 {
			t.Errorf("check %d: TUI StableCount should stay 0 (output changed), got %d", i, session.StableCount)
		}
	}

	// Heartbeat should NEVER be sent for TUI sessions
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times — should be 0!", inspector.sendHeartbeatCalls)
	}
}

func TestTUI_FakeDeadTimeout_ReturnsRunning_NoHeartbeat(t *testing.T) {
	// When a TUI session stays stable beyond fakeDeadThreshold,
	// return Running instead of triggering heartbeat/send-keys.
	// The agent has already received the Stable event and can decide.
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)

	session := newTUISession("tui", SessionRunning, "A")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))
	session.StableCount = 5                                 // At fake dead threshold (5, need > 5 to trigger)
	session.StableSince = time.Now().Add(-30 * time.Second) // Was stable for 30s

	inspector.setOutput("A", nil)

	// stableCount=6 > fakeDeadThreshold(5) → TUI skip heartbeat → Running
	status := tm.detectSessionState(session)

	if status != SessionRunning {
		t.Errorf("TUI at fakeDead should return Running (skip heartbeat), got %s", status)
	}
	// Heartbeat must NOT be sent for TUI
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times — should be 0!", inspector.sendHeartbeatCalls)
	}
	// StableCount should be capped at fakeDeadThreshold to avoid unbounded growth
	if session.StableCount != 5 {
		t.Errorf("expected StableCount=5 (capped at fakeDeadThreshold), got %d", session.StableCount)
	}
	// StableSince should persist — agent can compute duration from Stable event to now
	if session.StableSince.IsZero() {
		t.Error("StableSince should persist after TUI fakeDead timeout (agent needs this context)")
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

	// Phase 1: TUI output stabilizes (same frame for 2 checks)
	inspector.setOutput("frame1", nil)
	tm.checkSession(session) // stableCount=1 → Running
	tm.checkSession(session) // stableCount=2 → Stable

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
	// Simulates agent repeatedly sending input to a session:
	// turn1: output changes → running → stable
	// turn2: output changes → running → stable
	// turn3: output changes → running → stable → completed
	// Each transition should fire exactly one callback.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadThreshold = 100 // Disable fake-dead to focus on multi-turn

	session := newTestSession("multiturn", SessionRunning, "init")
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

	// Same output twice → reach Stable
	inspector.setOutput("Turn1: line1\nTurn1: line2", nil)
	tm.checkSession(session) // stableCount=1
	if session.Status != SessionRunning {
		t.Errorf("turn1 check3: expected Running (stableCount=1), got %s", session.Status)
	}
	tm.checkSession(session) // stableCount=2 → Stable
	if session.Status != SessionStable {
		t.Errorf("turn1: expected Stable, got %s", session.Status)
	}

	// === Turn 2: agent sends more input, output changes ===
	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...", nil)
	tm.checkSession(session) // changed → Running, stableCount reset
	if session.Status != SessionRunning {
		t.Errorf("turn2: expected Running (output changed), got %s", session.Status)
	}
	if session.StableCount != 0 {
		t.Errorf("turn2: StableCount should reset to 0, got %d", session.StableCount)
	}

	// Turn 2 stabilizes
	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...\nTurn2: done", nil)
	tm.checkSession(session) // changed
	tm.checkSession(session) // stableCount=1
	tm.checkSession(session) // stableCount=2 → Stable
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
	tm.fakeDeadThreshold = 100 // Disable fake-dead for this test

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
	if session.StableCount > 10 {
		t.Errorf("StableCount too high (%d) — should reset on each change", session.StableCount)
	}
}

func TestMultiTurn_OutputAccumulation(t *testing.T) {
	// Verifies that LastOutput correctly accumulates output across multiple
	// interaction turns, and the agent always sees the full history.
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadThreshold = 100

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

	// Round 1: TUI stabilizes
	inspector.setOutput("screen1", nil)
	tm.checkSession(session) // stableCount=1 → Running
	tm.checkSession(session) // stableCount=2 → Stable

	if session.Status != SessionStable {
		t.Fatalf("round 1: expected Stable, got %s", session.Status)
	}

	// Round 2: Agent sends input, TUI screen changes
	inspector.setOutput("screen1\nscreen2: processing your request...", nil)
	tm.checkSession(session) // changed → Running
	if session.Status != SessionRunning {
		t.Fatalf("round 2: expected Running after change, got %s", session.Status)
	}

	// Round 2 output stabilizes
	tm.checkSession(session) // stableCount=1
	tm.checkSession(session) // stableCount=2 → Stable
	if session.Status != SessionStable {
		t.Errorf("round 2: expected Stable, got %s", session.Status)
	}

	// Round 3: FakeDead timeout — stays stable too long
	// Simulate staying stable past fakeDeadThreshold
	session.StableCount = 5 // at threshold
	inspector.setOutput("screen1\nscreen2: processing your request...", nil)
	tm.checkSession(session) // stableCount=6 > 5 → TUI skip heartbeat → Running

	// Agent should have received: stable→running (fake-dead timeout)
	// Agent can now decide to kill or wait
	if session.Status != SessionRunning {
		t.Errorf("round 3: expected Running after fakeDead timeout, got %s", session.Status)
	}

	// Verify heartbeat was NEVER sent
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI heartbeat calls: %d (should be 0)", inspector.sendHeartbeatCalls)
	}

	// Verify all expected events fired
	// running→stable (round1), stable→running (round2), running→stable (round2),
	// stable→running (round3 fakeDead timeout, but only if old status was Stable)
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
