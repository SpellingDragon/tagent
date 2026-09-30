package action

import (
	"crypto/md5"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

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

	// processExistsCalls Call tracking
	processExistsCalls  int
	isPaneDeadCalls     int
	getOutputCalls      int
	sendHeartbeatCalls  int
	killSessionCalls    int
	restartSessionCalls int

	// alive3 R3 三态探测 mock：aliveKnown=false 时 SessionAlive3 返回 unknown。
	alive3      bool
	alive3Knwn  bool
	alive3Err   bool
	alive3Calls int
}

func (m *mockInspector) SessionAlive3(sessionID string) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive3Calls++
	if m.alive3Err {
		return false, false
	}
	if !m.alive3Knwn && !m.alive3 {
		return m.processExists, true
	}
	return m.alive3, m.alive3Knwn
}

// setAlive3 programs the tri-state probe : alive=false+known=true → dead;
// alive=true+known=true → alive; known=false → unknown.
func (m *mockInspector) setAlive3(alive, known bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive3, m.alive3Knwn = alive, known
}

func (m *mockInspector) setAlive3Err(err bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive3Err = err
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

// newTestMonitor creates a TmuxMonitor with a mock inspector for testing.
// Uses very short durations so tests can verify state transitions with minimal time manipulation.
func newTestMonitor(inspector *mockInspector) *TmuxMonitor {
	return &TmuxMonitor{
		executor:                  inspector,
		interval:                  30 * time.Second,
		stableDuration:            10 * time.Millisecond,
		interactiveStableDuration: 10 * time.Millisecond,
		fakeDeadDuration:          1 * time.Hour,
		heartbeatCommand:          "echo ping",
		sessions:                  make(map[string]*TmuxSession),
		lastNotifiedStatus:        make(map[string]SessionStatus),
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
	if session.LastOutput != "old_output" {
		t.Errorf("expected LastOutput='old_output' (preserved), got %q", session.LastOutput)
	}
}

func TestDetectSessionState_OutputStable_TransitionToStable(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "A")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))

	inspector.setOutput("A", nil)
	status := tm.detectSessionState(session)

	if status != SessionRunning {
		t.Errorf("check 1: expected SessionRunning, got %s", status)
	}
	if session.StableSince.IsZero() {
		t.Error("check 1: StableSince should be set when output first becomes unchanged")
	}

	time.Sleep(15 * time.Millisecond)

	status = tm.detectSessionState(session)

	if status != SessionStable {
		t.Errorf("check 2: expected SessionStable (alive+quiet, no auto-complete), got %s", status)
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

	tm.detectSessionState(session)
	if session.StableSince.IsZero() {
		t.Error("StableSince should be set on first unchanged output")
	}

	time.Sleep(15 * time.Millisecond)

	status := tm.detectSessionState(session)
	if status != SessionStable {
		t.Errorf("check 2: expected SessionStable (interactive), got %s", status)
	}
}

func TestDetectSessionState_HeartbeatNoResponse_PaneDead_ReturnsCompleted(t *testing.T) {
	inspector := &mockInspector{
		processExists: true,
		isPaneDead:    true,
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("test", SessionRunning, "final")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("final")))
	session.StableSince = time.Now().Add(-1 * time.Hour)

	inspector.setOutput("final", nil)

	status := tm.detectSessionState(session)

	if status != SessionCompleted {
		t.Errorf("BUG: expected SessionCompleted (pane is dead), got %s", status)
	}
	if session.LastOutput != "final" {
		t.Errorf("BUG: LastOutput not captured! got %q", session.LastOutput)
	}
}

func TestCheckSession_NoStateChange_NoCallback(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "A")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))

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

func TestOutputConsistency_MultipleUpdatesBeforeStable(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "")

	steps := []string{
		"line1\n",
		"line1\nline2\n",
		"line1\nline2\nline3\n",
		"line1\nline2\nline3\n",
		"line1\nline2\nline3\n",
	}

	for i, output := range steps {
		inspector.setOutput(output, nil)
		status := tm.detectSessionState(session)

		if i < 3 && status != SessionRunning {
			t.Errorf("step %d: expected running, got %s", i, status)
		}

		if i == 3 {
			time.Sleep(15 * time.Millisecond)
			status = tm.detectSessionState(session)
		}

		if i == 4 && status != SessionStable {
			t.Errorf("step %d: expected stable (alive+quiet, no auto-complete), got %s", i, status)
		}
	}

	if session.LastOutput != "line1\nline2\nline3\n" && session.LastOutput != steps[2] {
		t.Errorf("LastOutput not consistent: got %q", session.LastOutput)
	}
}

func TestOutputConsistency_OutputMD5_EmptyOutput(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("test", SessionRunning, "")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("")))

	inspector.setOutput("", nil)

	tm.detectSessionState(session)
	if session.StableSince.IsZero() {
		t.Error("StableSince should be set for unchanged empty output")
	}

	time.Sleep(15 * time.Millisecond)

	status := tm.detectSessionState(session)
	if status != SessionStable {
		t.Errorf("expected SessionStable for consistent empty output (alive+quiet), got %s", status)
	}
}

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

	sessions := tm.ListSessions()
	if len(sessions) != 10 {
		t.Errorf("expected 10 sessions, got %d", len(sessions))
	}

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

func TestStateMachine_FullLifecycle_NormalCompletion(t *testing.T) {
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
		{output: "step2", processExists: true, isPaneDead: false, expectedState: SessionRunning},
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionRunning},
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionRunning},
		{output: "step3", processExists: true, isPaneDead: false, expectedState: SessionCompleted},
	}

	var lastTransition string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		lastTransition = fmt.Sprintf("%s→%s output=%q", oldS, newS, output)
	}

	for i, step := range lifecycle {
		inspector.setProcess(step.processExists, step.isPaneDead)
		inspector.setOutput(step.output, nil)

		if i == 2 {
			tm.checkSession(session)
			time.Sleep(15 * time.Millisecond)
			continue
		}

		oldStatus := session.Status
		tm.checkSession(session)

		if step.expectedState == SessionCompleted {
			if oldStatus != SessionRunning || lastTransition == "" {
				t.Errorf("step %d: expected running→completed transition, got %s→? transition=%q",
					i, oldStatus, lastTransition)
			}
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
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("fake", SessionRunning, "X")
	session.IsInteractive = true
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("X")))
	tm.sessions["fake"] = session

	inspector.setOutput("X", nil)

	tm.checkSession(session)
	if _, exists := tm.GetSession("fake"); !exists {
		t.Fatal("session should not be removed after first check")
	}

	session.StableSince = time.Now().Add(-1 * time.Hour)

	tm.checkSession(session)

	_, exists := tm.GetSession("fake")
	if exists {
		t.Error("BUG: session should be removed after fake_dead")
	}
}

// newTUISession creates a TmuxSession with IsTUI=true for TUI testing.
func newTUISession(id string, status SessionStatus, lastOutput string) *TmuxSession {
	s := newTestSession(id, status, lastOutput)
	s.IsTUI = true
	return s
}

func TestTUI_OutputAlwaysChanging_StaysRunning(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTUISession("tui", SessionRunning, "frame1")

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

	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times — should be 0!", inspector.sendHeartbeatCalls)
	}
}

func TestTUI_FakeDeadTimeout_ReturnsTimedOut_NoHeartbeat(t *testing.T) {
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "no_response",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTUISession("tui", SessionRunning, "A")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("A")))
	session.StableSince = time.Now().Add(-1 * time.Hour)

	inspector.setOutput("A", nil)

	status := tm.detectSessionState(session)

	if status != SessionTimedOut {
		t.Errorf("TUI at fakeDead should return TimedOut (skip heartbeat), got %s", status)
	}
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times — should be 0!", inspector.sendHeartbeatCalls)
	}
}

func TestTUI_FullLifecycle_RunningToCompleted(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTUISession("tui_lifecycle", SessionRunning, "frame1")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("frame1")))
	tm.sessions["tui_lifecycle"] = session

	var transitions []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

	inspector.setOutput("frame1", nil)
	tm.checkSession(session)
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session)

	if session.Status != SessionStable {
		t.Errorf("phase 1: TUI should reach Stable when idle, got %s", session.Status)
	}

	inspector.setProcess(false, true)
	inspector.setOutput("frame1\ntui_final", nil)
	tm.checkSession(session)

	if len(transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d: %v", len(transitions), transitions)
	}
	if len(transitions) >= 2 && transitions[1] != "stable→completed" {
		t.Errorf("expected stable→completed, got %v", transitions)
	}

	_, exists := tm.GetSession("tui_lifecycle")
	if exists {
		t.Error("TUI session should be removed after completion")
	}

	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI triggered heartbeat %d times", inspector.sendHeartbeatCalls)
	}
}

func TestMultiTurn_RepeatedStableRunningCycles(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 100 * time.Hour

	session := newTestSession("multiturn", SessionRunning, "init")
	session.IsInteractive = true
	tm.sessions["multiturn"] = session

	var transitions []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

	inspector.setOutput("Turn1: line1", nil)
	tm.checkSession(session)
	if session.Status != SessionRunning {
		t.Errorf("turn1 check1: expected Running, got %s", session.Status)
	}

	inspector.setOutput("Turn1: line1\nTurn1: line2", nil)
	tm.checkSession(session)

	inspector.setOutput("Turn1: line1\nTurn1: line2", nil)
	tm.checkSession(session)
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session)
	if session.Status != SessionStable {
		t.Errorf("turn1: expected Stable, got %s", session.Status)
	}

	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...", nil)
	tm.checkSession(session)
	if session.Status != SessionRunning {
		t.Errorf("turn2: expected Running (output changed), got %s", session.Status)
	}
	if !session.StableSince.IsZero() {
		t.Errorf("turn2: StableSince should reset to zero on output change, got %v", session.StableSince)
	}

	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...\nTurn2: done", nil)
	tm.checkSession(session)
	tm.checkSession(session)
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session)
	if session.Status != SessionStable {
		t.Errorf("turn2: expected Stable, got %s", session.Status)
	}

	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...\nTurn2: done\nTurn3: final", nil)
	tm.checkSession(session)
	if session.Status != SessionRunning {
		t.Errorf("turn3: expected Running, got %s", session.Status)
	}

	inspector.setProcess(false, true)
	inspector.setOutput("Turn1: line1\nTurn1: line2\nTurn2: processing...\nTurn2: done\nTurn3: final\nEXIT", nil)
	tm.checkSession(session)

	expectedCount := 5
	if len(transitions) != expectedCount {
		t.Errorf("expected %d transitions, got %d: %v", expectedCount, len(transitions), transitions)
	}

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

	_, exists := tm.GetSession("multiturn")
	if exists {
		t.Error("session should be removed after completion")
	}
}

func TestMultiTurn_LongSession_FiftyChecks(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 100 * time.Hour

	session := newTestSession("longrun", SessionRunning, "")
	tm.sessions["longrun"] = session

	var callbacks int
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		callbacks++
	}

	baseOutput := "build output line "
	phase := 0

	for i := 0; i < 50; i++ {
		if i%5 == 0 {
			phase = 0
		}

		var output string
		switch phase {
		case 0:
			output = fmt.Sprintf("%s%d\n%s%d", baseOutput, i, baseOutput, i+1)
			phase = 1
		case 1:
			output = fmt.Sprintf("%s%d\n%s%d", baseOutput, i-1, baseOutput, i)
			phase = 2
		default:
			output = fmt.Sprintf("%s%d\n%s%d", baseOutput, i-2, baseOutput, i-1)
		}

		inspector.setOutput(output, nil)
		oldStatus := session.Status
		tm.checkSession(session)

		if session.Status == SessionCompleted || session.Status == SessionError {
			t.Errorf("check %d: session terminated unexpectedly (%s)", i, session.Status)
			break
		}

		if session.Status == oldStatus && callbacks > 0 {
		}
	}

	t.Logf("Long session: 50 checks, %d state change callbacks", callbacks)

	if callbacks > 100 {
		t.Errorf("too many callbacks (%d) — possible instability", callbacks)
	}
	if !session.StableSince.IsZero() && time.Since(session.StableSince) > 30*time.Second {
		t.Errorf("StableSince too old (%v) — should reset on each output change", time.Since(session.StableSince))
	}
}

func TestMultiTurn_OutputAccumulation(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 100 * time.Hour

	session := newTestSession("accum", SessionRunning, "")

	type turn struct {
		output   string
		expected string
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

	if session.LastOutput != "step1: init\nstep2: build\nstep3: test\n" {
		t.Errorf("final LastOutput not accumulated correctly: %q", session.LastOutput)
	}
}

func TestMultiTurn_TUI_AgentGetsStableEvent(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTUISession("tui_mt", SessionRunning, "screen1")
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("screen1")))
	tm.sessions["tui_mt"] = session

	var events []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		events = append(events, fmt.Sprintf("%s→%s output_len=%d", oldS, newS, len(output)))
	}

	inspector.setOutput("screen1", nil)
	tm.checkSession(session)
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session)

	if session.Status != SessionStable {
		t.Fatalf("round 1: expected Stable, got %s", session.Status)
	}

	inspector.setOutput("screen1\nscreen2: processing your request...", nil)
	tm.checkSession(session)
	if session.Status != SessionRunning {
		t.Fatalf("round 2: expected Running after change, got %s", session.Status)
	}

	tm.checkSession(session)
	time.Sleep(15 * time.Millisecond)
	tm.checkSession(session)
	if session.Status != SessionStable {
		t.Errorf("round 2: expected Stable, got %s", session.Status)
	}

	session.StableSince = time.Now().Add(-2 * time.Hour)
	inspector.setOutput("screen1\nscreen2: processing your request...", nil)
	tm.checkSession(session)

	if session.Status != SessionTimedOut {
		t.Errorf("round 3: expected TimedOut after fakeDead timeout, got %s", session.Status)
	}

	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI heartbeat calls: %d (should be 0)", inspector.sendHeartbeatCalls)
	}

	if len(events) < 3 {
		t.Errorf("expected at least 3 events, got %d: %v", len(events), events)
	}

	t.Logf("TUI multi-turn events: %v", events)
}

func TestCallback_ReceiveExactOutput(t *testing.T) {
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

func TestDetectSessionState_FakeAlive_HeartbeatOk(t *testing.T) {
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "ok",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("fakealive", SessionRunning, "unchanged")
	session.IsInteractive = true
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("unchanged")))
	session.StableSince = time.Now().Add(-1 * time.Hour)

	inspector.setOutput("unchanged", nil)

	status := tm.detectSessionState(session)

	if status != SessionFakeAlive {
		t.Errorf("expected SessionFakeAlive, got %s", status)
	}
	if inspector.sendHeartbeatCalls != 1 {
		t.Errorf("expected 1 heartbeat, got %d", inspector.sendHeartbeatCalls)
	}
}

func TestDetectSessionState_FakeDead_RecheckPaneDead(t *testing.T) {
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "no_response",
	}
	inspector.isPaneDead = false
	inspector.paneDeadOnRecheck = 2

	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("recheck", SessionRunning, "stable_output")
	session.IsInteractive = true
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

func TestHandleFakeAlive_RestartSuccess_ContinuesTracking(t *testing.T) {
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "ok",
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("restart_ok", SessionRunning, "stuck")
	session.IsInteractive = true
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("stuck")))
	session.StableSince = time.Now().Add(-1 * time.Hour)
	tm.sessions["restart_ok"] = session

	inspector.setOutput("stuck", nil)

	var transitions []string
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

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
	if session.Status != SessionFakeAlive {
		t.Errorf("expected Status=FakeAlive (set by checkSession), got %s", session.Status)
	}
	if _, exists := tm.GetSession("restart_ok"); !exists {
		t.Fatal("session should still be tracked after FakeAlive restart")
	}

	inspector.setOutput("fresh_output", nil)
	oldStatus := session.Status
	tm.checkSession(session)

	if session.Status != SessionRunning {
		t.Errorf("after restart, expected Running, got %s (was %s)", session.Status, oldStatus)
	}

	if session.Status != SessionRunning {
		t.Errorf("expected state to transition to Running after restart, got %s", session.Status)
	}

	if _, exists := tm.GetSession("restart_ok"); !exists {
		t.Fatal("session should still be tracked after FakeAlive restart")
	}
}

func TestHandleFakeAlive_RestartFailure_StaysFakeAlive(t *testing.T) {
	inspector := &mockInspector{
		processExists: true,
		heartbeatResp: "ok",
		restartErr:    fmt.Errorf("tmux unavailable"),
	}
	tm := newTestMonitor(inspector)
	tm.fakeDeadDuration = 10 * time.Millisecond

	session := newTestSession("restart_fail", SessionRunning, "stuck")
	session.IsInteractive = true
	session.LastOutputMD5 = fmt.Sprintf("%x", md5.Sum([]byte("stuck")))
	session.StableSince = time.Now().Add(-1 * time.Hour)
	tm.sessions["restart_fail"] = session

	inspector.setOutput("stuck", nil)

	var callbackFired bool
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		callbackFired = true
	}

	tm.checkSession(session)

	if callbackFired {
		t.Error("callback should NOT fire on running→fake_alive transition (FakeAlive is intermediate state)")
	}
	if inspector.restartSessionCalls != 1 {
		t.Errorf("expected 1 restart attempt, got %d", inspector.restartSessionCalls)
	}
	if session.Status != SessionFakeAlive {
		t.Errorf("expected Status=FakeAlive (untouched on failure), got %s", session.Status)
	}
	if _, exists := tm.GetSession("restart_fail"); !exists {
		t.Error("session should remain tracked after failed restart")
	}
}

// TestBuildResultFromSignal_OutputTruncation 钉住 小输出原样返回；大输出转储到文件并保留截断尾部在内联，使上下文有界。
func TestBuildResultFromSignal_OutputTruncation(t *testing.T) {
	ct := &ActionTool{
		tmuxMonitor: NewTmuxMonitor(WithMonitorExecutor(&mockInspector{processExists: true})),
		workspace:   t.TempDir(),
	}

	session := &TmuxSession{
		ID:          "trunc_test",
		Status:      SessionStable,
		StableSince: time.Now().Add(-30 * time.Second),
	}
	ct.tmuxMonitor.AddSession(session)

	short := ct.buildResultFromSignal("trunc_test", "echo short", false,
		task.SettleSignal{Kind: task.SettleStable, Output: "short output line\n"})
	if !strings.Contains(short.Output, "short output line") {
		t.Errorf("short output should be delivered intact, got %q", short.Output)
	}
	if short.OutputFile != "" {
		t.Errorf("short output should not spill to a file, got %q", short.OutputFile)
	}

	largeLine := "line of output data that will eventually be truncated because too long\n"
	largeOutput := strings.Repeat(largeLine, 100)
	large := ct.buildResultFromSignal("trunc_test", "echo large", false,
		task.SettleSignal{Kind: task.SettleStable, Output: largeOutput})
	if large.OutputFile == "" {
		t.Error("large output should be spilled to OutputFile")
	}
	if strings.Contains(large.Output, largeOutput) {
		t.Error("full large output should NOT appear inline")
	}
	if !strings.Contains(large.Output, largeLine) {
		t.Error("truncated tail of output should appear inline")
	}
}

// TestWithMonitorConfig_ScheduleParams 钉住 MonitorConfig schedule params flow into the monitor's PollSchedule (4.2).
func TestWithMonitorConfig_ScheduleParams(t *testing.T) {
	tm := NewTmuxMonitor(WithMonitorConfig(MonitorConfig{
		Interval:      time.Second,
		DenseDuration: 5 * time.Second,
		BackoffFactor: 3,
		MaxInterval:   30 * time.Second,
	}))
	if tm.schedule.DenseInterval != time.Second {
		t.Errorf("DenseInterval = %v, want 1s (from Interval)", tm.schedule.DenseInterval)
	}
	if tm.schedule.DenseDuration != 5*time.Second {
		t.Errorf("DenseDuration = %v, want 5s", tm.schedule.DenseDuration)
	}
	if tm.schedule.BackoffFactor != 3 {
		t.Errorf("BackoffFactor = %v, want 3", tm.schedule.BackoffFactor)
	}
	if tm.schedule.MaxInterval != 30*time.Second {
		t.Errorf("MaxInterval = %v, want 30s", tm.schedule.MaxInterval)
	}
}

func adaptiveTestMonitor(inspector *mockInspector) *TmuxMonitor {
	tm := newTestMonitor(inspector)
	tm.schedule = DefaultPollSchedule()
	tm.nextPoll = make(map[string]time.Time)
	return tm
}

// TestReschedule_DenseToBackoff 钉住 a young session reschedules at the dense interval; an old one backs off to a larger interval.
//
// 契约: docs/wiki/tool/tool-architecture.md#tmux-monitor
func TestReschedule_DenseToBackoff(t *testing.T) {
	tm := adaptiveTestMonitor(&mockInspector{processExists: true})
	now := time.Now()

	young := newTestSession("y", SessionRunning, "A")
	young.CreatedAt = now
	tm.sessions["y"] = young
	tm.rescheduleSession(young, now)
	if got := tm.nextPoll["y"].Sub(now); got != tm.schedule.DenseInterval {
		t.Errorf("young interval = %v, want dense %v", got, tm.schedule.DenseInterval)
	}

	old := newTestSession("o", SessionRunning, "A")
	old.CreatedAt = now.Add(-100 * time.Second)
	tm.sessions["o"] = old
	tm.rescheduleSession(old, now)
	if got := tm.nextPoll["o"].Sub(now); got < 30*time.Second {
		t.Errorf("old interval = %v, want backed off (≥30s)", got)
	}
}

// TestReschedule_StableSparse 钉住 a stable (service-ready) session polls at the sparsest cadence (MaxInterval), regardless of age.
func TestReschedule_StableSparse(t *testing.T) {
	tm := adaptiveTestMonitor(&mockInspector{processExists: true})
	now := time.Now()
	s := newTestSession("s", SessionStable, "A")
	s.CreatedAt = now
	tm.sessions["s"] = s
	tm.rescheduleSession(s, now)
	if got := tm.nextPoll["s"].Sub(now); got != tm.schedule.MaxInterval {
		t.Errorf("stable interval = %v, want MaxInterval %v", got, tm.schedule.MaxInterval)
	}
}

// TestCheckAllSessions_SkipsNotDue 钉住 a session whose nextPoll is in the future is not polled (the inspector is not touched).
func TestCheckAllSessions_SkipsNotDue(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := adaptiveTestMonitor(inspector)
	s := newTestSession("s", SessionRunning, "A")
	s.CreatedAt = time.Now()
	tm.sessions["s"] = s
	tm.nextPoll["s"] = time.Now().Add(time.Hour)

	inspector.resetCallCounters()
	tm.checkAllSessions()
	if inspector.processExistsCalls != 0 {
		t.Errorf("not-due session should be skipped; processExistsCalls = %d", inspector.processExistsCalls)
	}
}

// TestCheckAllSessions_PollsDue: a due session is polled and rescheduled.
func TestCheckAllSessions_PollsDue(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := adaptiveTestMonitor(inspector)
	s := newTestSession("s", SessionRunning, "A")
	s.CreatedAt = time.Now()
	tm.sessions["s"] = s
	tm.nextPoll["s"] = time.Now().Add(-time.Second)

	inspector.resetCallCounters()
	tm.checkAllSessions()
	if inspector.processExistsCalls == 0 {
		t.Errorf("due session should be polled")
	}
	if !tm.nextPoll["s"].After(time.Now()) {
		t.Errorf("due session should be rescheduled into the future")
	}
}

// TestAddSessionWithCallback_FiresPerSession 钉住 按会话注册的回调在该会话发生有意义迁移时触发，并与全局回调并存、互不排斥。
func TestAddSessionWithCallback_FiresPerSession(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("sess-1", SessionRunning, "A")

	var globalOld, globalNew SessionStatus
	var globalFired bool
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		globalFired = true
		globalOld, globalNew = oldS, newS
	}

	var perOld, perNew SessionStatus
	var perFired bool
	var perSID string
	tm.AddSessionWithCallback(session, func(sid string, oldS, newS SessionStatus, output string) {
		perFired = true
		perSID = sid
		perOld, perNew = oldS, newS
	})

	inspector.setProcess(false, false)
	tm.checkSession(session)

	if !perFired {
		t.Fatalf("per-session callback did not fire")
	}
	if perSID != "sess-1" || perOld != SessionRunning || perNew != SessionCompleted {
		t.Errorf("per-session cb args = (%s,%s→%s), want (sess-1,running→completed)", perSID, perOld, perNew)
	}
	if !globalFired || globalOld != SessionRunning || globalNew != SessionCompleted {
		t.Errorf("global cb should also fire with running→completed; got fired=%v %s→%s", globalFired, globalOld, globalNew)
	}
}

// TestAddSessionWithCallback_FiresWithoutGlobal 钉住 未注册全局状态变更回调时，按会话注册的回调照样触发——两者互不依赖。
func TestAddSessionWithCallback_FiresWithoutGlobal(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("sess-2", SessionRunning, "A")
	var fired bool
	tm.AddSessionWithCallback(session, func(sid string, oldS, newS SessionStatus, output string) {
		fired = true
	})

	inspector.setProcess(false, false)
	tm.checkSession(session)

	if !fired {
		t.Fatalf("per-session callback should fire even without a global callback")
	}
}

// TestAddSessionWithCallback_RemovedOnRemoveSession 钉住 RemoveSession drops the per-session callback so it does not fire afterwards.
func TestAddSessionWithCallback_RemovedOnRemoveSession(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("sess-3", SessionRunning, "A")
	var fired bool
	tm.AddSessionWithCallback(session, func(sid string, oldS, newS SessionStatus, output string) {
		fired = true
	})
	tm.RemoveSession("sess-3")

	tm.AddSession(session)
	inspector.setProcess(false, false)
	tm.checkSession(session)

	if fired {
		t.Errorf("per-session callback fired after RemoveSession")
	}
}
