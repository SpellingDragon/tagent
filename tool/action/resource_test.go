// 契约: docs/wiki/tool/tmux-action.md#named-session-singleton
package action

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

func assertQuietObservation(t *testing.T, mode SessionMode, tui bool) {
	t.Helper()
	inspector := &mockInspector{processExists: true, output: "unchanged"}
	monitor := newTestMonitor(inspector)
	session := newTestSession("quiet", SessionRunning, inspector.output)
	session.Mode, session.IsTUI = mode, tui
	session.QuietTimeout = time.Second
	session.StableSince = time.Now().Add(-time.Hour)
	detector := NewTmuxSettleDetector(session.ID, nil)
	t.Cleanup(detector.Cancel)
	monitor.AddSessionWithCallback(session, func(_ string, _, status SessionStatus, output string) { detector.OnStateChange(status, output) })
	monitor.checkSession(session)
	require.Equal(t, SessionTimedOut, session.Status)
	_, tracked := monitor.GetSession(session.ID)
	require.True(t, tracked)
	require.Zero(t, inspector.killSessionCalls)
	require.Zero(t, inspector.restartSessionCalls)
	require.Zero(t, inspector.sendHeartbeatCalls)
	sig := <-detector.Settled()
	require.Equal(t, task.SettleSuspect, sig.Kind)
	require.False(t, sig.Final)
	require.False(t, sig.Ready)
	require.False(t, sig.WaitSatisfied)
	select {
	case <-detector.Stopped():
		t.Fatal("a quiet process has not stopped")
	default:
	}
}

type fakeTmuxResource struct {
	mu                                     sync.Mutex
	name, resource, command, output        string
	present, dead, killFails, sendFails    bool
	exit, creates, activates, kills, sends int
}

func (f *fakeTmuxResource) run(_ context.Context, args []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch args[0] {
	case "new-session":
		for i, arg := range args {
			if arg == "-s" {
				f.name = args[i+1]
			}
			if arg == ";" {
				f.command = args[i-1]
				break
			}
		}
		f.present, f.dead = true, false
		if f.resource == "" {
			f.resource = "100:$7"
		}
		f.creates++
		return "", nil
	case "has-session":
		if !f.present {
			return "", fmt.Errorf("absent")
		}
		return "", nil
	case "display-message":
		if !f.present {
			return "", fmt.Errorf("absent")
		}
		switch args[len(args)-1] {
		case "#{pid}:#{session_id}":
			return f.resource, nil
		case "#{pane_start_command}":
			return f.command, nil
		case "#{pane_dead}":
			if f.dead {
				return "1", nil
			}
			return "0", nil
		case "#{pane_dead_status}":
			if f.dead {
				return strconv.Itoa(f.exit), nil
			}
			return "", nil
		case "#{pane_pid}":
			return "0", nil
		}
	case "list-sessions":
		if f.present {
			return f.name, nil
		}
		return "", nil
	case "capture-pane":
		return f.output, nil
	case "pipe-pane":
		return "", nil
	case "if-shell":
		if !f.present || !strings.Contains(args[4], "#{==:#{pid}:#{session_id},"+f.resource+"}") {
			return "tagent-resource-changed", nil
		}
		if strings.Contains(args[4], "#{pane_start_command}") && !strings.Contains(args[4], "#{==:#{pane_start_command},"+f.command+"}") {
			return "tagent-resource-changed", nil
		}
		branch := args[5]
		switch {
		case strings.Contains(branch, "respawn-pane"):
			f.activates++
			f.command = "executed business"
			f.dead = false
		case strings.Contains(branch, "kill-session"):
			if f.killFails {
				return "", fmt.Errorf("kill rejected")
			}
			f.kills++
			f.present = false
		case strings.Contains(branch, "send-keys"):
			if f.sendFails {
				return "", fmt.Errorf("send rejected")
			}
			f.sends++
		}
		return "", nil
	}
	return "", nil
}

func fakeActionTool(t *testing.T, fake *fakeTmuxResource) *ActionTool {
	t.Helper()
	executor := &TmuxExecutor{prefix: "fake-action", pipeRoot: t.TempDir(), run: fake.run}
	monitor := NewTmuxMonitor(WithMonitorExecutor(executor), WithMonitorConfig(MonitorConfig{Interval: time.Hour, DenseInterval: time.Hour, StableDuration: time.Second, InteractiveStableDuration: time.Second, FakeDeadDuration: 2 * time.Second}))
	ct := &ActionTool{tmuxExecutor: executor, tmuxMonitor: monitor, residentMetaDirOverride: t.TempDir(), defaultTTL: time.Hour}
	t.Cleanup(func() { _ = ct.Close() })
	return ct
}

// TestActionAdmissionPrecedesResourcePreparation keeps rejected intents from starting commands.
func TestActionAdmissionPrecedesResourcePreparation(t *testing.T) {
	for _, intentRejected := range []bool{false, true} {
		t.Run(strconv.FormatBool(intentRejected), func(t *testing.T) {
			fake := &fakeTmuxResource{}
			ct := fakeActionTool(t, fake)
			cfg := task.TaskManagerConfig{}
			if intentRejected {
				cfg.OnSpawnAccepted = func(*task.Task) bool { return false }
			} else {
				cfg.SpawnGate = func() string { return "blocked" }
			}
			manager := task.NewTaskManager(cfg)
			result, err := ct.Call(task.WithTaskSpawner(t.Context(), manager), []byte(`{"command":"side-effect"}`))
			require.NoError(t, err)
			require.Equal(t, "blocked", result.(*ActionToolResult).Status)
			require.Zero(t, fake.creates)
			require.Zero(t, fake.activates)
		})
	}
}

// TestActionBindingPrecedesActivation executes a deduplicated command once after durable binding.
func TestActionBindingPrecedesActivation(t *testing.T) {
	fake := &fakeTmuxResource{}
	ct := fakeActionTool(t, fake)
	intent, binding := false, false
	manager := task.NewTaskManager(task.TaskManagerConfig{
		OnSpawnAccepted: func(tk *task.Task) bool {
			require.Zero(t, fake.creates)
			require.Equal(t, "intent", tk.SpecSnapshot().Declarative.Params[pPhase])
			intent = true
			return true
		},
		OnStartAccepted: func(tk *task.Task) bool {
			require.True(t, intent)
			require.Equal(t, 1, fake.creates)
			require.Zero(t, fake.activates)
			decl := tk.SpecSnapshot().Declarative
			require.Equal(t, "100:$7", decl.Params[pResourceID])
			require.Equal(t, "prepared", decl.Params[pPhase])
			require.NotEmpty(t, decl.Params[pPlaceholder])
			binding = true
			return true
		},
	})
	args := []byte(`{"command":"service-command","mode":"resident","probe":"true"}`)
	ctx := task.WithTaskSpawner(t.Context(), manager)
	first, err := ct.Call(ctx, args)
	require.NoError(t, err)
	require.True(t, binding)
	require.Equal(t, "ready", first.(*ActionToolResult).Status)
	_, err = ct.Call(ctx, args)
	require.NoError(t, err)
	require.Equal(t, 1, fake.creates)
	require.Equal(t, 1, fake.activates)
}

// TestIntentEvidenceSeparatesWatchReadyAndInput requires evidence for each foreground outcome.
func TestIntentEvidenceSeparatesWatchReadyAndInput(t *testing.T) {
	d := NewTmuxSettleDetector("evidence", nil)
	defer d.Cancel()
	require.NoError(t, d.ConfigureIntent(ModeResident, "READY", ""))
	require.NoError(t, d.SetWatch("ERROR", time.Nanosecond))
	d.OnWatchOutput("ERROR")
	watch := <-d.Settled()
	require.Equal(t, task.SettleWatch, watch.Kind)
	require.False(t, watch.Ready)
	require.False(t, watch.WaitSatisfied)
	d.OnStateChange(SessionStable, "quiet")
	stable := <-d.Settled()
	require.Equal(t, "stable", stable.Observation)
	require.False(t, stable.WaitSatisfied)
	d.OnStateChange(SessionRunning, "READY")
	require.Equal(t, task.SettleReady, (<-d.Settled()).Kind)
	d.EmitProbeResult(false, "down")
	require.Equal(t, "probe_failure", (<-d.Settled()).Observation)
	d.EmitProbeResult(true, "healthy")
	require.Equal(t, "probe_recovered", (<-d.Settled()).Observation)

	input := NewTmuxSettleDetector("input", nil)
	defer input.Cancel()
	require.NoError(t, input.ConfigureIntent(ModeInteractive, "", `PROMPT>`))
	input.OnStateChange(SessionRunning, "busy")
	select {
	case sig := <-input.Settled():
		t.Fatalf("busy output requested input: %+v", sig)
	default:
	}
	input.OnStateChange(SessionRunning, "PROMPT>")
	require.Equal(t, task.SettleInput, (<-input.Settled()).Kind)
	input.OnStateChange(SessionRunning, "PROMPT>")
	select {
	case sig := <-input.Settled():
		t.Fatalf("unchanged prompt repeated: %+v", sig)
	default:
	}
}

// TestMonitorRunningOutputTriggersWatch samples watch matches during a running state.
func TestMonitorRunningOutputTriggersWatch(t *testing.T) {
	inspector := &mockInspector{processExists: true, output: "boot"}
	monitor := newTestMonitor(inspector)
	session := newTestSession("watch-running", SessionRunning, inspector.output)
	detector := NewTmuxSettleDetector(session.ID, nil)
	defer detector.Cancel()
	require.NoError(t, detector.SetWatch("ERROR", time.Second))
	monitor.AddSessionWithRoundCallback(session, func(round uint64, status SessionStatus, output string) {
		detector.OnWatchOutputRound(round, output)
		detector.OnObservation(round, status, output)
	})
	inspector.setOutput("boot\nERROR new", nil)
	monitor.checkSession(session)
	require.Equal(t, SessionRunning, session.Status)
	require.Equal(t, task.SettleWatch, (<-detector.Settled()).Kind)
}

// TestTerminalReceiptSurvivesMailboxPressure keeps callbacks bounded and evidence until exact acceptance.
func TestTerminalReceiptSurvivesMailboxPressure(t *testing.T) {
	d := NewTmuxSettleDetector("mailbox", nil)
	defer d.Cancel()
	for i := 0; i < 1000; i++ {
		d.OnStateChange(SessionStable, strconv.Itoa(i))
	}
	d.OnStateChange(SessionCompleted, "complete output")
	var terminal task.SettleSignal
	for sig := range d.Settled() {
		if sig.Kind == task.SettleCompleted {
			terminal = sig
		}
	}
	require.Equal(t, "complete output", terminal.Output)
	require.NotZero(t, terminal.Seq)
	acknowledged := 0
	d.ackFn = func() error { acknowledged++; return nil }
	stale := terminal
	stale.Seq--
	d.AcknowledgeSignal(stale)
	require.Zero(t, acknowledged)
	d.AcknowledgeSignal(terminal)
	d.AcknowledgeSignal(terminal)
	require.Equal(t, 1, acknowledged)
}

// TestRoundFencingRejectsOldSnapshots keeps old observations out of a resumed round.
func TestRoundFencingRejectsOldSnapshots(t *testing.T) {
	d := NewTmuxSettleDetector("rounds", nil)
	defer d.Cancel()
	require.NoError(t, d.ConfigureIntent(ModeResident, "READY", ""))
	d.BeginRound(7)
	d.OnObservation(6, SessionCompleted, "old terminal")
	d.EmitProbeResultRound(6, true, "old readiness")
	select {
	case sig := <-d.Settled():
		t.Fatalf("old round emitted: %+v", sig)
	default:
	}
	d.OnObservation(7, SessionRunning, "READY")
	sig := <-d.Settled()
	require.Equal(t, uint64(7), sig.ProducerRound)
	require.Equal(t, task.SettleReady, sig.Kind)
}

// TestResourceABACannotReleaseReplacement fences old ACK and cancellation against a reused name.
func TestResourceABACannotReleaseReplacement(t *testing.T) {
	fake := &fakeTmuxResource{name: "n-shared", resource: "100:$7", present: true, dead: true}
	ct := fakeActionTool(t, fake)
	session := &TmuxSession{ID: fake.name, ResourceID: fake.resource, Mode: ModeResident}
	d, err := ct.detectorForSession(session, ActionArgs{Mode: "resident"})
	require.NoError(t, err)
	ct.saveBoundMeta(session, ActionArgs{Mode: "resident"})
	d.OnStateChange(SessionCompleted, "old output")
	sig := <-d.Settled()
	fake.mu.Lock()
	fake.resource, fake.dead = "100:$8", false
	fake.mu.Unlock()
	ct.saveBoundMeta(&TmuxSession{ID: fake.name, ResourceID: "100:$8"}, ActionArgs{Mode: "resident"})
	d.AcknowledgeSignal(sig)
	d.Cancel()
	require.True(t, fake.present)
	require.Zero(t, fake.kills)
	var meta ResidentMeta
	b, err := os.ReadFile(ct.metaPath(fake.name))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &meta))
	require.Equal(t, "100:$8", meta.ResourceID)
}

// TestCancelCannotCertifyFailedKill distinguishes observer closure from external process termination.
func TestCancelCannotCertifyFailedKill(t *testing.T) {
	fake := &fakeTmuxResource{name: "n-kill-failure", resource: "100:$7", present: true, killFails: true}
	ct := fakeActionTool(t, fake)
	d, err := ct.detectorForSession(&TmuxSession{ID: fake.name, ResourceID: fake.resource}, ActionArgs{})
	require.NoError(t, err)
	d.Cancel()
	select {
	case <-d.Done():
	default:
		t.Fatal("cancel should close observations")
	}
	select {
	case <-d.Stopped():
		t.Fatal("failed kill cannot certify producer termination")
	default:
	}
	require.Error(t, ct.Close())
}

// TestRecoverDeclaredDeadCommandPreservesRoundAndBaseline recovers an unnamed completed process without rerunning it.
func TestRecoverDeclaredDeadCommandPreservesRoundAndBaseline(t *testing.T) {
	fake := &fakeTmuxResource{name: "generated-job", resource: "100:$7", command: "executed business", output: "old\nnew result", present: true, dead: true}
	ct := fakeActionTool(t, fake)
	decl := DeclarativeFromArgs(ActionArgs{Command: "business", Mode: "oneshot"}, fake.name)
	decl.RoundID, decl.ProducerRound, decl.SignalSeq = 4, 4, 7
	decl.Params[pResourceID], decl.Params[pBaseline], decl.Params[pPhase], decl.Params[pPlaceholder] = fake.resource, "1", "prepared", "sleep-placeholder"
	manager := task.NewTaskManager(task.TaskManagerConfig{})
	tk := manager.RestoreTask("recovered-task", ct.SpecFromDeclarative(manager, *decl), time.Now(), task.TaskRunning)
	d := ct.RecoverTaskDetector(tk)
	require.NotNil(t, d)
	d.(task.ActivatableDetector).Start()
	ct.tmuxMonitor.checkAllSessions()
	sig := <-d.Settled()
	require.Equal(t, uint64(4), sig.ProducerRound)
	require.Equal(t, uint64(8), sig.Seq)
	require.Equal(t, "new result", sig.Output)
	require.Equal(t, task.SignalSourceID(tk.ID, 4, 8), sig.SourceID)
	require.Zero(t, fake.activates)
	require.Zero(t, fake.kills)
	require.NoError(t, ct.Close())
	require.True(t, fake.present)
	d.(task.SignalAcknowledger).AcknowledgeSignal(sig)
	require.False(t, fake.present)
}

// TestRecoverPinsLegacyResourceBeforeActivation persists identity for records predating resource fencing.
func TestRecoverPinsLegacyResourceBeforeActivation(t *testing.T) {
	fake := &fakeTmuxResource{name: "legacy-job", resource: "100:$7", command: "already executed", present: true}
	ct := fakeActionTool(t, fake)
	decl := DeclarativeFromArgs(ActionArgs{Command: "business", Mode: "oneshot"}, fake.name)
	manager := task.NewTaskManager(task.TaskManagerConfig{})
	tk := manager.RestoreTask("legacy-task", ct.SpecFromDeclarative(manager, *decl), time.Now(), task.TaskRunning)
	d := ct.RecoverTaskDetector(tk)
	require.NotNil(t, d)
	pinned := d.(interface{ RecoveredSpec() task.TaskSpec }).RecoveredSpec()
	require.Equal(t, fake.resource, pinned.Declarative.Params[pResourceID])
	require.Zero(t, fake.activates)
}

// TestRejectedBindingNeverExecutesBusinessCommand stops only the admitted placeholder when binding fails.
func TestRejectedBindingNeverExecutesBusinessCommand(t *testing.T) {
	fake := &fakeTmuxResource{}
	ct := fakeActionTool(t, fake)
	manager := task.NewTaskManager(task.TaskManagerConfig{OnStartAccepted: func(*task.Task) bool { return false }})
	result, err := ct.Call(task.WithTaskSpawner(t.Context(), manager), []byte(`{"command":"side-effect"}`))
	require.NoError(t, err)
	require.Equal(t, "blocked", result.(*ActionToolResult).Status)
	require.Equal(t, 1, fake.creates)
	require.Zero(t, fake.activates)
	require.Equal(t, 1, fake.kills)
	require.False(t, fake.present)
}

// TestRecoverUnacknowledgedTerminalReleasesExactReceipt retains a recovered terminal pane until its saved receipt is accepted.
func TestRecoverUnacknowledgedTerminalReleasesExactReceipt(t *testing.T) {
	fake := &fakeTmuxResource{name: "terminal-job", resource: "100:$7", present: true, dead: true, output: "complete output"}
	ct := fakeActionTool(t, fake)
	decl := DeclarativeFromArgs(ActionArgs{Command: "business", Mode: "oneshot"}, fake.name)
	decl.RoundID, decl.ProducerRound, decl.SignalSeq = 5, 5, 9
	decl.Params[pResourceID] = fake.resource
	manager := task.NewTaskManager(task.TaskManagerConfig{OnSettleAccepted: func(*task.Task, task.SettleSignal) bool { return false }})
	tk := manager.RestoreTask("terminal-task", ct.SpecFromDeclarative(manager, *decl), time.Now(), task.TaskCompleted)
	receipt := task.SettleSignal{RoundID: 5, ProducerRound: 5, Seq: 9, SourceID: task.SignalSourceID(tk.ID, 5, 9), Kind: task.SettleCompleted, Final: true, Output: fake.output}
	require.True(t, manager.RestorePendingDelivery(tk.ID, receipt))
	d := ct.RecoverTaskDetector(tk)
	require.NotNil(t, d)
	d.(task.ActivatableDetector).Start()
	require.Zero(t, fake.activates)
	require.Zero(t, fake.kills)
	wrong := receipt
	wrong.SourceID = "wrong"
	d.(task.SignalAcknowledger).AcknowledgeSignal(wrong)
	require.True(t, fake.present)
	d.(task.SignalAcknowledger).AcknowledgeSignal(receipt)
	require.False(t, fake.present)
}

// TestResumeSendFailurePreservesProducerRound continues observing the old producer after the new input intent fails.
func TestResumeSendFailurePreservesProducerRound(t *testing.T) {
	fake := &fakeTmuxResource{name: "n-repl", resource: "100:$7", present: true, sendFails: true}
	ct := fakeActionTool(t, fake)
	session := &TmuxSession{ID: fake.name, ResourceID: fake.resource, Status: SessionRunning, Mode: ModeInteractive}
	detector, err := ct.detectorForSession(session, ActionArgs{Mode: "interactive"})
	require.NoError(t, err)
	detector.BeginRound(4)
	ct.tmuxMonitor.AddSession(session)
	prepared, err := ct.resumeResourceClosure(fake.name, fake.resource, false, detector)(t.Context(), "new input")
	require.NoError(t, err)
	prepared.(interface{ BeginRound(uint64) }).BeginRound(5)
	require.Equal(t, uint64(4), prepared.(interface{ ProducerRound() uint64 }).ProducerRound())
	prepared.(task.ActivatableDetector).Start()
	aborted := <-prepared.Settled()
	require.Equal(t, uint64(5), aborted.RoundID)
	require.Equal(t, uint64(4), aborted.ProducerRound)
	require.Equal(t, "round_aborted", aborted.Observation)
	require.False(t, aborted.Final)
	require.True(t, fake.present)
	fake.mu.Lock()
	fake.dead = true
	fake.mu.Unlock()
	detector.OnObservation(4, SessionCompleted, "old producer finished")
	terminal := <-prepared.Settled()
	require.Equal(t, uint64(4), terminal.ProducerRound)
	require.Equal(t, "old producer finished", terminal.Output)
}

// TestResourceActivationAndResumeRefuseABA prevents stale activation and input from reaching a replacement session.
func TestResourceActivationAndResumeRefuseABA(t *testing.T) {
	fake := &fakeTmuxResource{name: "n-reused", resource: "100:$8", present: true, command: "placeholder"}
	ct := fakeActionTool(t, fake)
	require.Error(t, ct.tmuxExecutor.activateResource(fake.name, "100:$7", "placeholder", "business"))
	require.Error(t, ct.tmuxExecutor.sendResource(fake.name, "100:$7", "input"))
	require.Zero(t, fake.activates)
	require.Zero(t, fake.sends)
}

// TestRecoveredPlaceholderActivatesOnlyOnce retries activation only while the original placeholder remains.
func TestRecoveredPlaceholderActivatesOnlyOnce(t *testing.T) {
	fake := &fakeTmuxResource{name: "prepared-job", resource: "100:$7", present: true, command: "original-placeholder"}
	ct := fakeActionTool(t, fake)
	decl := DeclarativeFromArgs(ActionArgs{Command: "business", Mode: "oneshot"}, fake.name)
	decl.RoundID, decl.ProducerRound = 3, 3
	decl.Params[pResourceID], decl.Params[pPhase], decl.Params[pPlaceholder] = fake.resource, "prepared", fake.command
	manager := task.NewTaskManager(task.TaskManagerConfig{})
	tk := manager.RestoreTask("prepared-task", ct.SpecFromDeclarative(manager, *decl), time.Now(), task.TaskRunning)
	first := ct.RecoverTaskDetector(tk)
	require.NotNil(t, first)
	first.(task.ActivatableDetector).Start()
	require.Equal(t, 1, fake.activates)
	secondTool := fakeActionTool(t, fake)
	second := secondTool.RecoverTaskDetector(tk)
	require.NotNil(t, second)
	second.(task.ActivatableDetector).Start()
	require.Equal(t, 1, fake.activates)
}

// TestOneshotSilenceNeverSatisfiesExitWait keeps stable and suspect observations from ending the foreground exit wait.
func TestOneshotSilenceNeverSatisfiesExitWait(t *testing.T) {
	d := NewTmuxSettleDetector("quiet-job", nil, 20*time.Millisecond)
	defer d.Cancel()
	manager := task.NewTaskManager(task.TaskManagerConfig{})
	go func() { d.OnStateChange(SessionStable, "quiet"); d.OnStateChange(SessionTimedOut, "still alive") }()
	result := manager.Spawn(task.TaskSpec{Kind: "command", Desc: "quiet job", Mode: "oneshot", WaitFor: task.WaitExit}, d)
	require.False(t, result.Settled)
	require.Equal(t, task.TaskRunning, result.Task.Status())
	select {
	case <-d.Stopped():
		t.Fatal("silence cannot confirm process death")
	default:
	}
}

// TestSessionOperationsRespectResourceCredential refuses stale direct sends and stops.
func TestSessionOperationsRespectResourceCredential(t *testing.T) {
	fake := &fakeTmuxResource{name: "n-reused-ops", resource: "100:$8", present: true}
	ct := fakeActionTool(t, fake)
	_, err := ct.opSend(&ActionArgs{Keys: "input", ResourceID: "100:$7"}, fake.name)
	require.Error(t, err)
	_, err = ct.opStop(&ActionArgs{ResourceID: "100:$7"}, fake.name)
	require.Error(t, err)
	require.Zero(t, fake.sends)
	require.Zero(t, fake.kills)
}

// TestRecoveryPreservesReplacementMetadata keeps an ended resource from overwriting its reused name.
func TestRecoveryPreservesReplacementMetadata(t *testing.T) {
	fake := &fakeTmuxResource{name: "n-recovery-aba", resource: "100:$8", present: true}
	ct := fakeActionTool(t, fake)
	ct.saveBoundMeta(&TmuxSession{ID: fake.name, ResourceID: fake.resource}, ActionArgs{Mode: "resident"})
	decl := DeclarativeFromArgs(ActionArgs{Command: "business", Mode: "oneshot"}, fake.name)
	decl.Params[pResourceID] = "100:$7"
	manager := task.NewTaskManager(task.TaskManagerConfig{})
	tk := manager.RestoreTask("aba-recovered-task", ct.SpecFromDeclarative(manager, *decl), time.Now(), task.TaskRunning)
	d := ct.RecoverTaskDetector(tk)
	require.NotNil(t, d)
	d.(task.ActivatableDetector).Start()
	sig := <-d.Settled()
	require.Equal(t, task.SettleFailed, sig.Kind)
	d.(task.SignalAcknowledger).AcknowledgeSignal(sig)
	require.True(t, fake.present)
	require.Zero(t, fake.kills)
	b, err := os.ReadFile(ct.metaPath(fake.name))
	require.NoError(t, err)
	var meta ResidentMeta
	require.NoError(t, json.Unmarshal(b, &meta))
	require.Equal(t, "100:$8", meta.ResourceID)
}

// TestResumeByteBoundaryExcludesUnchangedPrompt waits for input evidence produced after the resumed write.
func TestResumeByteBoundaryExcludesUnchangedPrompt(t *testing.T) {
	d := NewTmuxSettleDetector("byte-boundary", nil)
	defer d.Cancel()
	require.NoError(t, d.ConfigureIntent(ModeInteractive, "", `PROMPT>`))
	d.RearmOutput("old result\nPROMPT>")
	d.BeginRound(2)
	d.OnObservation(2, SessionRunning, "old result\nPROMPT>")
	select {
	case sig := <-d.Settled():
		t.Fatalf("unchanged prompt requested input: %+v", sig)
	default:
	}
	d.OnObservation(2, SessionRunning, "old result\nPROMPT>answer\nPROMPT>")
	require.Equal(t, task.SettleInput, (<-d.Settled()).Kind)
	d.OnObservation(2, SessionRunning, "old result\nPROMPT>answer\nPROMPT>noise")
	select {
	case sig := <-d.Settled():
		t.Fatalf("old pattern matched new noise: %+v", sig)
	default:
	}
	require.Equal(t, strconv.Itoa(len("old result\nPROMPT>")), d.DeclarativeParams()[pBaselineBytes])
}

// TestCancelledPreparedProducersCannotActivate keeps failed termination from permitting a later start.
func TestCancelledPreparedProducersCannotActivate(t *testing.T) {
	fake := &fakeTmuxResource{killFails: true}
	ct := fakeActionTool(t, fake)
	session, detector, err := ct.prepareSession(t.Context(), ActionArgs{Command: "business", Mode: "oneshot"})
	require.NoError(t, err)
	detector.Cancel()
	detector.Start()
	require.Zero(t, fake.activates)
	require.True(t, fake.present)
	resume := &preparedResumeDetector{TmuxSettleDetector: detector, monitor: ct.tmuxMonitor, activate: func() error { return ct.tmuxExecutor.sendResource(session.ID, session.ResourceID, "input") }, requested: 2}
	resume.Start()
	require.Zero(t, fake.sends)
	select {
	case <-detector.Stopped():
		t.Fatal("failed cancellation must stay unconfirmed")
	default:
	}
}

// TestPreparedTmuxExecPreservesFullOutput retains real process output and the dead pane until receipt acceptance.
func TestPreparedTmuxExecPreservesFullOutput(t *testing.T) {
	if testing.Short() {
		if os.Getenv("TAGENT_REQUIRE_TMUX") == "1" {
			t.Fatal("required tmux execution cannot run with -short")
		}
		t.Skip("real tmux execution is exercised by the full and race suites")
	}
	if !IsTmuxAvailable() {
		if os.Getenv("TAGENT_REQUIRE_TMUX") == "1" {
			t.Fatal("tmux is required by this test run")
		}
		t.Skip("tmux is not installed")
	}
	probeExecutor, probeID := mustTmuxSession(t, "sleep 30")
	require.NoError(t, probeExecutor.KillSession(probeID))
	ct := NewActionTool(WithOrphanCleanupDisabled(), WithResidentMetaDir(t.TempDir()), WithActionMonitorConfig(MonitorConfig{Interval: 10 * time.Millisecond, DenseInterval: 10 * time.Millisecond, DenseDuration: 200 * time.Millisecond, StableDuration: time.Second, InteractiveStableDuration: time.Second, FakeDeadDuration: 2 * time.Second}))
	defer ct.Close()
	marker := filepath.Join(t.TempDir(), "executed")
	command := "printf activated > " + tmuxQuote(marker) + "; printf 'BEGIN\\n'; i=0; while [ \"$i\" -lt 2500 ]; do printf 'line-%s\\n' \"$i\"; i=$((i+1)); done; printf 'END\\n'"
	session, detector, err := ct.prepareSession(t.Context(), ActionArgs{Command: command, Mode: "oneshot"})
	require.NoError(t, err)
	require.Contains(t, session.Placeholder, "tagent-prepare-")
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "preparation must not execute business work")
	detector.BeginRound(3)
	detector.Start()
	var terminal task.SettleSignal
	deadline := time.After(8 * time.Second)
	for terminal.Kind == "" {
		select {
		case sig, open := <-detector.Settled():
			if !open {
				t.Fatal("detector closed before terminal output")
			}
			if sig.Kind == task.SettleCompleted || sig.Kind == task.SettleFailed {
				terminal = sig
			}
		case <-deadline:
			t.Fatal("prepared command never produced a terminal receipt")
		}
	}
	require.NoError(t, terminal.Err)
	require.Equal(t, task.SettleCompleted, terminal.Kind)
	require.Equal(t, uint64(3), terminal.ProducerRound)
	output := strings.ReplaceAll(terminal.Output, "\r\n", "\n")
	require.Contains(t, output, "BEGIN\nline-0\n")
	require.Contains(t, output, "line-2499\nEND")
	require.True(t, ct.tmuxExecutor.SessionExists(session.ID))
	require.FileExists(t, ct.terminalOutputPath(session.ID, session.ResourceID))
	require.NoError(t, ct.Close())
	require.True(t, ct.tmuxExecutor.SessionExists(session.ID), "generation close must preserve unaccepted evidence")
	detector.AcknowledgeSignal(terminal)
	require.False(t, ct.tmuxExecutor.SessionExists(session.ID))
}

// TestLegacyRecoveryHonorsPinnedMetadata preserves the first durable identity pin across a partial recovery crash.
func TestLegacyRecoveryHonorsPinnedMetadata(t *testing.T) {
	fake := &fakeTmuxResource{name: "n-partial-pin", resource: "100:$8", present: true}
	ct := fakeActionTool(t, fake)
	ct.saveBoundMeta(&TmuxSession{ID: fake.name, ResourceID: "100:$7"}, ActionArgs{Mode: "resident"})
	decl := DeclarativeFromArgs(ActionArgs{Command: "old business", Mode: "resident"}, fake.name)
	manager := task.NewTaskManager(task.TaskManagerConfig{})
	tk := manager.RestoreTask("partial-pin-task", ct.SpecFromDeclarative(manager, *decl), time.Now(), task.TaskRunning)
	d := ct.RecoverTaskDetector(tk)
	require.NotNil(t, d)
	pinned := d.(interface{ RecoveredSpec() task.TaskSpec }).RecoveredSpec()
	require.Equal(t, "100:$7", pinned.Declarative.Params[pResourceID])
	d.(task.ActivatableDetector).Start()
	receipt := <-d.Settled()
	d.(task.SignalAcknowledger).AcknowledgeSignal(receipt)
	require.True(t, fake.present)
	require.Zero(t, fake.kills)
	require.Zero(t, fake.activates)
}
