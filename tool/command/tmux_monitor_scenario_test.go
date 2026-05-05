package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ==================== Scenario Regression Tests ====================

// testSessionRecorder captures all state transitions and system messages.
type testSessionRecorder struct {
	t          *testing.T
	events     []string
	messages   []string
	monitor    *TmuxMonitor
	sessionIDs map[string]bool
}

func newRecorder(t *testing.T, monitor *TmuxMonitor) *testSessionRecorder {
	r := &testSessionRecorder{
		t:          t,
		monitor:    monitor,
		sessionIDs: make(map[string]bool),
	}
	monitor.StateChangeCallback = func(sessionID string, oldS, newS SessionStatus, output string) {
		ts := time.Now().Format("15:04:05.000")
		session, ok := monitor.GetSession(sessionID)

		var extra string
		if ok && !session.StableSince.IsZero() {
			stableDur := time.Since(session.StableSince).Round(time.Second)
			extra = fmt.Sprintf(" [stableSince=%v ago]", stableDur)
		}
		event := fmt.Sprintf("[%s] %s %s→%s stableDuration=%v%s",
			ts, sessionID[:12], oldS, newS,
			func() time.Duration {
				if ok && !session.StableSince.IsZero() {
					return time.Since(session.StableSince).Round(time.Second)
				}
				return -1
			}(), extra)

		outputPreview := ""
		if len(output) > 100 {
			outputPreview = output[:100] + "..."
		} else {
			outputPreview = output
		}
		msg := fmt.Sprintf("%s\n  output(%d): %s", event, len(output), outputPreview)

		r.events = append(r.events, event)
		r.messages = append(r.messages, msg)
		r.t.Log(msg)
	}
	return r
}

func (r *testSessionRecorder) addSession(id string) {
	r.sessionIDs[id] = true
}

func (r *testSessionRecorder) dumpSummary() {
	r.t.Logf("========== SCENARIO SUMMARY: %d events ==========", len(r.events))
	for i, evt := range r.events {
		r.t.Logf("  %d. %s", i+1, evt)
	}
}

// ==================== Scenario A: Normal command lifecycle ====================

func TestScenarioA_NormalLifecycle(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available")
	}

	executor := NewTmuxExecutor()
	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval:                  300 * time.Millisecond,
			StableDuration:           0,
			InteractiveStableDuration: 900 * time.Millisecond,
			FakeDeadDuration:          1500 * time.Millisecond,
		}),
	)
	rec := newRecorder(t, monitor)

	// Keeper to keep tmux server alive
	keeper, _ := executor.CreateSession(context.Background(), TmuxCreateOptions{Command: "sleep 120"})
	defer executor.KillSession(keeper.ID)

	// Create a normal session: short script that produces output then exits
	scriptPath := filepath.Join(t.TempDir(), "scenario_a.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/bash\necho 'step1: init'\nsleep 0.3\necho 'step2: processing'\nsleep 0.3\necho 'step3: done'\n"), 0755)

	t.Log("=== SCENARIO A: Normal command lifecycle ===")
	t.Log("Expect: running → (output changes, stableCount resets) → stable → completed")

	normalSess, err := executor.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "bash " + scriptPath,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer executor.KillSession(normalSess.ID)
	rec.addSession(normalSess.ID)

	session := &TmuxSession{
		ID:     normalSess.ID,
		Status: SessionRunning,
		IsTUI:  false,
	}
	monitor.AddSession(session)

	// Poll until completed (max 10s)
	deadline := time.Now().Add(10 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		oldStatus := session.Status
		newStatus := monitor.detectSessionState(session)
		if newStatus != oldStatus {
			oldOutput := session.LastOutput
			session.Status = newStatus
			if monitor.StateChangeCallback != nil {
				monitor.StateChangeCallback(session.ID, oldStatus, newStatus, oldOutput)
			}
			if newStatus == SessionCompleted {
				completed = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	rec.dumpSummary()

	if !completed {
		t.Error("Session did not complete!")
	}

	// Verify: output should be captured
	if session.LastOutput == "" {
		t.Error("Final output not captured")
	} else {
		t.Logf("Final output (%d bytes): %q", len(session.LastOutput), session.LastOutput)
	}

	// Verify key transitions happened
	hasRunningToStable := false
	hasToCompleted := false
	for _, evt := range rec.events {
		if strings.Contains(evt, "running→stable") {
			hasRunningToStable = true
		}
		if strings.Contains(evt, "→completed") {
			hasToCompleted = true
		}
	}
	if !hasRunningToStable {
		t.Error("Missing running→stable transition")
	}
	if !hasToCompleted {
		t.Error("Missing →completed transition")
	}
	t.Logf("Scenario A: %d total events, running→stable=%v, →completed=%v",
		len(rec.events), hasRunningToStable, hasToCompleted)
}

// ==================== Scenario B: TUI idle → Stable → timeout → Running ====================

func TestScenarioB_TUI_IdleTimeout(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available")
	}

	executor := NewTmuxExecutor()
	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval:           300 * time.Millisecond,
			StableDuration:     0,
			FakeDeadDuration:   1200 * time.Millisecond, // Low to trigger quickly
		}),
	)
	rec := newRecorder(t, monitor)

	keeper, _ := executor.CreateSession(context.Background(), TmuxCreateOptions{Command: "sleep 120"})
	defer executor.KillSession(keeper.ID)

	// TUI simulation: prints a static screen (output never changes)
	scriptPath := filepath.Join(t.TempDir(), "tui_idle.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/bash\necho '╔══════════════════╗'\necho '║  TUI IDLE SCREEN  ║'\necho '║  Waiting...       ║'\necho '╚══════════════════╝'\nwhile true; do sleep 10; done\n"), 0755)

	tuiSess, err := executor.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "bash " + scriptPath,
	})
	if err != nil {
		t.Fatalf("create TUI session: %v", err)
	}
	defer executor.KillSession(tuiSess.ID)
	rec.addSession(tuiSess.ID)

	tuiMon := &TmuxSession{ID: tuiSess.ID, Status: SessionRunning, IsTUI: true}
	monitor.AddSession(tuiMon)

	t.Log("=== SCENARIO B: TUI idle → Stable → fakeDead timeout → Running (no heartbeat) ===")
	t.Log("Expect: running→stable [stableSince set] → stable→running [stableSince preserved]")

	// Run checks for enough cycles to pass fakeDeadThreshold
	for i := 0; i < 12; i++ {
		// Artificially set status to trigger callbacks properly
		oldStatus := tuiMon.Status
		newStatus := monitor.detectSessionState(tuiMon)
		if newStatus != oldStatus {
			// Simulate checkSession behavior: fire callback, update status
			oldOutput := tuiMon.LastOutput
			tuiMon.Status = newStatus
			if monitor.StateChangeCallback != nil {
				monitor.StateChangeCallback(tuiMon.ID, oldStatus, newStatus, oldOutput)
			}
			// Manually handle special states (TUI just gets Running)
			switch newStatus {
			case SessionCompleted, SessionError:
				monitor.RemoveSession(tuiMon.ID)
			}
		}
		time.Sleep(150 * time.Millisecond)
	}

	rec.dumpSummary()

	// Check StableSince was preserved through timeout
	if tuiMon.StableSince.IsZero() {
		t.Error("StableSince should be set (session reached Stable)")
	} else {
		t.Logf("StableSince: %v ago", time.Since(tuiMon.StableSince).Round(time.Second))
	}

	// Count transitions
	stableCount := 0
	runningCount := 0
	for _, evt := range rec.events {
		if strings.Contains(evt, "→stable") {
			stableCount++
		}
		if strings.Contains(evt, "→running") {
			runningCount++
		}
	}
	t.Logf("Scenario B: %d events, →stable=%d, →running=%d",
		len(rec.events), stableCount, runningCount)

	// TUI should NOT reach completed/fake_dead — should stay alive
	if _, exists := monitor.GetSession(tuiMon.ID); !exists {
		t.Error("TUI session should still be alive (not removed)!")
	}
}

// ==================== Scenario C: Multi-turn interactions ====================

func TestScenarioC_MultiTurn(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available")
	}

	executor := NewTmuxExecutor()
	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval:         300 * time.Millisecond,
			StableDuration:   0,
			FakeDeadDuration: 30 * time.Second, // Disable fakeDead for this test
		}),
	)
	rec := newRecorder(t, monitor)

	keeper, _ := executor.CreateSession(context.Background(), TmuxCreateOptions{Command: "sleep 120"})
	defer executor.KillSession(keeper.ID)

	// Long-running session that accumulates output
	scriptPath := filepath.Join(t.TempDir(), "multiturn.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/bash\necho 'turn1: init'\nsleep 0.5\necho 'turn1: processing'\nsleep 1\necho 'turn1: done'\nsleep 2\necho 'turn2: starting'\nsleep 0.5\necho 'turn2: complete'\nsleep 3\necho 'FINAL'\n"), 0755)

	sess, err := executor.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "bash " + scriptPath,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer executor.KillSession(sess.ID)
	rec.addSession(sess.ID)

	monSess := &TmuxSession{ID: sess.ID, Status: SessionRunning, IsTUI: false}
	monitor.AddSession(monSess)

	t.Log("=== SCENARIO C: Multi-turn (output changes → stable → changes → stable → completed) ===")
	t.Log("Expect: running→stable [turn1], stable→running [turn2], running→stable [turn2], stable→completed")

	// Poll until completed
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		oldStatus := monSess.Status
		newStatus := monitor.detectSessionState(monSess)
		if newStatus != oldStatus {
			oldOutput := monSess.LastOutput
			monSess.Status = newStatus
			if monitor.StateChangeCallback != nil {
				monitor.StateChangeCallback(monSess.ID, oldStatus, newStatus, oldOutput)
			}
			if newStatus == SessionCompleted {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	rec.dumpSummary()

	// Verify at least one stable→running (output changes after stable)
	hasStableToRunning := false
	hasRunningToStable := false
	for _, evt := range rec.events {
		if strings.Contains(evt, "stable→running") {
			hasStableToRunning = true
		}
		if strings.Contains(evt, "running→stable") {
			hasRunningToStable = true
		}
	}
	t.Logf("Scenario C: %d events, running→stable=%v, stable→running=%v",
		len(rec.events), hasRunningToStable, hasStableToRunning)

	if !hasStableToRunning {
		t.Error("Missing stable→running (output should have changed during turn2)")
	}
}

// ==================== Scenario D: TUI + Normal side-by-side ====================

func TestScenarioD_TUIPlusNormal(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available")
	}

	executor := NewTmuxExecutor()
	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval:         300 * time.Millisecond,
			StableDuration:   0,
			FakeDeadDuration: 1200 * time.Millisecond,
		}),
	)
	rec := newRecorder(t, monitor)

	keeper, _ := executor.CreateSession(context.Background(), TmuxCreateOptions{Command: "sleep 120"})
	defer executor.KillSession(keeper.ID)

	// TUI: static screen (idle)
	tuiScript := filepath.Join(t.TempDir(), "tui_scenario_d.sh")
	os.WriteFile(tuiScript, []byte("#!/bin/bash\necho '╔══════╗'\necho '║ TUI  ║'\necho '╚══════╝'\nwhile true; do sleep 10; done\n"), 0755)

	tuiSess, _ := executor.CreateSession(context.Background(), TmuxCreateOptions{Command: "bash " + tuiScript})
	defer executor.KillSession(tuiSess.ID)

	// Normal: short command
	normalScript := filepath.Join(t.TempDir(), "normal_scenario_d.sh")
	os.WriteFile(normalScript, []byte("#!/bin/bash\necho 'normal_start'\nsleep 0.5\necho 'normal_done'\n"), 0755)

	normalSess, _ := executor.CreateSession(context.Background(), TmuxCreateOptions{Command: "bash " + normalScript})
	defer executor.KillSession(normalSess.ID)

	tuiMon := &TmuxSession{ID: tuiSess.ID, Status: SessionRunning, IsTUI: true}
	normalMon := &TmuxSession{ID: normalSess.ID, Status: SessionRunning, IsTUI: false}
	monitor.AddSession(tuiMon)
	monitor.AddSession(normalMon)
	rec.addSession(tuiSess.ID)
	rec.addSession(normalSess.ID)

	t.Log("=== SCENARIO D: TUI + Normal side-by-side ===")
	t.Log("Expect: TUI reaches Stable; Normal completes; TUI survives fakeDead timeout")

	normalDone := false
	tuiAlive := true
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		// Check TUI
		tuiOld := tuiMon.Status
		tuiNew := monitor.detectSessionState(tuiMon)
		if tuiNew != tuiOld {
			oldOut := tuiMon.LastOutput
			tuiMon.Status = tuiNew
			if monitor.StateChangeCallback != nil {
				monitor.StateChangeCallback(tuiMon.ID, tuiOld, tuiNew, oldOut)
			}
		}

		// Check Normal
		normOld := normalMon.Status
		normNew := monitor.detectSessionState(normalMon)
		if normNew != normOld {
			oldOut := normalMon.LastOutput
			normalMon.Status = normNew
			if monitor.StateChangeCallback != nil {
				monitor.StateChangeCallback(normalMon.ID, normOld, normNew, oldOut)
			}
			if normNew == SessionCompleted {
				normalDone = true
			}
		}

		// Check TUI still alive
		if _, exists := monitor.GetSession(tuiMon.ID); !exists {
			t.Logf("TUI was removed unexpectedly at status=%s", tuiMon.Status)
			tuiAlive = false
			break
		}

		if normalDone {
			// Continue a few more cycles to observe TUI timeout
			time.Sleep(500 * time.Millisecond)
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	rec.dumpSummary()

	t.Logf("TUI final status: %s, stableSince=%v ago",
		tuiMon.Status,
		func() string {
			if tuiMon.StableSince.IsZero() {
				return "never"
			}
			return time.Since(tuiMon.StableSince).Round(time.Second).String()
		}())

	t.Logf("Normal: completed=%v, final output=%q", normalDone, normalMon.LastOutput)

	if !normalDone {
		t.Error("Normal session should have completed")
	}
	if !tuiAlive {
		t.Error("TUI session should still be alive")
	}
}

// ==================== Send-Keys Interference Test ====================

// tuiSimScript returns the path to the simulated TUI script.
func tuiSimScript(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "tui_sim.sh")
	content := `#!/bin/bash
i=0
while true; do
  echo "╔══════════════════╗"
  echo "║  TUI SIM v1.0    ║"
  echo "║  Frame: $i       ║"
  echo "╚══════════════════╝"
  i=$((i + 1))
  sleep 1
done
`
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write tui script: %v", err)
	}
	return scriptPath
}

// TestTUI_SendKeysInterference demonstrates that send-keys injects text into TUI.
// This is why heartbeat must NOT use send-keys for TUI sessions.
func TestTUI_SendKeysInterference(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available")
	}

	executor := NewTmuxExecutor()

	keeper, err := executor.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "sleep 120",
	})
	if err != nil {
		t.Fatalf("failed to create keeper: %v", err)
	}
	defer executor.KillSession(keeper.ID)

	tuiScript := tuiSimScript(t)
	tuiSess, err := executor.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "bash " + tuiScript,
	})
	if err != nil {
		t.Fatalf("failed to create TUI session: %v", err)
	}
	defer executor.KillSession(tuiSess.ID)

	time.Sleep(1 * time.Second)

	baseline, err := executor.GetSessionOutput(tuiSess.ID)
	if err != nil {
		t.Fatalf("failed to get baseline output: %v", err)
	}
	t.Logf("Baseline output size: %d bytes", len(baseline))

	heartbeatMarker := "HEARTBEAT_MARKER_" + fmt.Sprintf("%d", time.Now().UnixNano())
	err = exec.Command("tmux", "send-keys", "-t", tuiSess.ID, heartbeatMarker).Run()
	if err != nil {
		t.Fatalf("failed to send keys: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	afterOutput, err := executor.GetSessionOutput(tuiSess.ID)
	if err != nil {
		t.Fatalf("failed to get after output: %v", err)
	}

	if !contains(afterOutput, heartbeatMarker) {
		t.Logf("Marker not visible (TUI may have redrawn): size=%d", len(afterOutput))
	} else {
		t.Logf("⚠️  send-keys marker FOUND in TUI output — this is the interference problem!")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
