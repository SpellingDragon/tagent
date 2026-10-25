// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
//
// Package-internal proof for leaf 01.10a: the query faces of one execution
// resource tell a returned call, a running process, a real exit with its code,
// a confirmed release and a named unknown apart. Every observation runs through
// the formal RunTool/ReleaseTool entry and the Manager query faces; the test
// reads no internal ledger and rewrites none.
package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	osexec "os/exec"
	"strings"
	"testing"
	"time"
)

// TestEventIntegrityExecLifecycle pins the five lifecycle states one execution resource exposes to its query faces:
// - a returned call answers the frame it captured and proves no ending by itself
// - a partial frame read while the process runs is not a completion
// - the same key queried again sees the output produced after the call returned
// - a process that really exited reports its exit code through the release
// - a release answers a confirmed stop or a named unknown / retained resource
// - a stable frame stands beside a live process and is never reported as success
func TestEventIntegrityExecLifecycle(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)

	t.Run("returned_call_does_not_prove_exit", func(t *testing.T) {
		manager, _ := eventIntegrityManager(t)
		key := eventIntegrityKey("returned")
		eventIntegrityCleanup(t, manager, key)
		run := NewRunTool(manager)

		answer := eventIntegrityRun(t, run, KindTmux, key, "echo alpha; sleep 30; exit 5", "2m")
		if !strings.HasPrefix(answer, "executed "+key.String()+": ") {
			t.Fatalf("exec_run answer = %q, want the executed frame under %s", answer, key)
		}
		if !strings.Contains(answer, "alpha") {
			t.Fatalf("exec_run answer = %q, want the frame carrying alpha", answer)
		}
		if strings.Contains(answer, "exit=") || strings.Contains(answer, "released") {
			t.Fatalf("exec_run answer = %q claims an ending the call never observed", answer)
		}
		eventIntegrityAssertNoCompletionVerdict(t, "exec_run answer after the call returned", answer)

		if held := manager.Held(); held != 1 {
			t.Fatalf("held after the call returned = %d, want the resource still held", held)
		}
		observed, err := manager.Observe(context.Background(), key)
		if err != nil {
			t.Fatalf("observe after the call returned = %v", err)
		}
		if observed.State != ProbeAlive {
			t.Fatalf("observed state after the call returned = %v, want %v", observed.State, ProbeAlive)
		}
		if err := osexec.Command("tmux", "has-session", "-t", key.Key).Run(); err != nil {
			t.Fatalf("the session %s is gone right after the call returned: %v", key.Key, err)
		}
		if err := manager.Renew(context.Background(), key, time.Minute); err != nil {
			t.Fatalf("renew after the call returned = %v, want the lease face reachable", err)
		}
		explanation := eventIntegrityAssertUnsettled(t, manager, key)
		if explanation.Renewed != 1 {
			t.Fatalf("explain.renewed = %d, want the one renewal this test drove", explanation.Renewed)
		}
		t.Logf("STATE returned-call: answer=%q alive=%v held=%d renewed=%d",
			answer, observed.State, manager.Held(), explanation.Renewed)
	})

	t.Run("partial_output_is_not_completion", func(t *testing.T) {
		manager, executor := eventIntegrityManager(t)
		key := eventIntegrityKey("partial")
		eventIntegrityCleanup(t, manager, key)
		run := NewRunTool(manager)

		answer := eventIntegrityRun(t, run, KindTmux, key, "echo alpha; sleep 2; echo omega; sleep 30; exit 5", "2m")
		if !strings.Contains(answer, "alpha") {
			t.Fatalf("exec_run answer = %q, want the partial frame carrying alpha", answer)
		}
		if strings.Contains(answer, "omega") {
			t.Fatalf("exec_run answer = %q carries the later marker, want the partial frame only", answer)
		}
		eventIntegrityAssertNoCompletionVerdict(t, "exec_run answer over a partial frame", answer)

		if held := manager.Held(); held != 1 {
			t.Fatalf("held after the partial frame = %d, want the process still held", held)
		}
		observed, err := manager.Observe(context.Background(), key)
		if err != nil {
			t.Fatalf("observe after the partial frame = %v", err)
		}
		if observed.State != ProbeAlive {
			t.Fatalf("observed state after the partial frame = %v, want %v", observed.State, ProbeAlive)
		}
		eventIntegrityAssertUnsettled(t, manager, key)
		eventIntegrityAssertMonitorVocabulary(t)

		monitor := NewMonitor(executor, MonitorConfig{
			Interval:         50 * time.Millisecond,
			StableDuration:   200 * time.Millisecond,
			FakeDeadDuration: 10 * time.Second,
		})
		status, err := monitor.Poll(context.Background(), key)
		if err != nil {
			t.Fatalf("monitor poll over the partial frame = %v", err)
		}
		if status != MonitorWatch {
			t.Fatalf("first monitor status over the partial frame = %q, want %q", status, MonitorWatch)
		}
		t.Logf("STATE partial-output: answer=%q status=%q alive=%v held=%d; no query face reports completion",
			answer, status, observed.State, manager.Held())
	})

	t.Run("same_key_capture_sees_more_output", func(t *testing.T) {
		manager, _ := eventIntegrityManager(t)
		key := eventIntegrityKey("reuse")
		eventIntegrityCleanup(t, manager, key)
		run := NewRunTool(manager)

		first := eventIntegrityRun(t, run, KindTmux, key, "echo alpha; sleep 2; echo omega; sleep 30; exit 5", "2m")
		if !strings.Contains(first, "alpha") {
			t.Fatalf("first exec_run answer = %q, want the frame carrying alpha", first)
		}

		ctx := context.Background()
		eventIntegrityAwait(t, 20*time.Second, "the later frame carrying omega", func() (bool, error) {
			frame, err := manager.Capture(ctx, key)
			if err != nil {
				return false, err
			}
			return strings.Contains(string(frame), "omega"), nil
		})
		later, err := manager.Capture(ctx, key)
		if err != nil {
			t.Fatalf("capture the later frame = %v", err)
		}
		if !strings.Contains(string(later), "alpha") || !strings.Contains(string(later), "omega") {
			t.Fatalf("later capture = %q, want both alpha and omega", later)
		}

		second := eventIntegrityRun(t, run, KindTmux, key, "unused", "2m")
		if !strings.Contains(second, "alpha") || !strings.Contains(second, "omega") {
			t.Fatalf("second exec_run answer = %q, want the later frame carrying both markers", second)
		}
		eventIntegrityAssertNoCompletionVerdict(t, "second exec_run answer over the same key", second)
		if strings.Contains(second, "exit=") || strings.Contains(second, "released") {
			t.Fatalf("second exec_run answer = %q claims an ending the query never observed", second)
		}
		if acquired := manager.Acquired(); acquired != 1 {
			t.Fatalf("acquired = %d, want one execution started", acquired)
		}
		if reused := manager.Reused(); reused != 1 {
			t.Fatalf("reused = %d, want the second acquisition answered from the held resource", reused)
		}
		if held := manager.Held(); held != 1 {
			t.Fatalf("held = %d, want the resource still held", held)
		}
		t.Logf("STATE same-key: first=%q second=%q acquired=%d reused=%d",
			first, second, manager.Acquired(), manager.Reused())
	})

	t.Run("release_reports_exit_code", func(t *testing.T) {
		manager, _ := eventIntegrityManager(t)
		key := eventIntegrityKey("exit")
		eventIntegrityCleanup(t, manager, key)
		run := NewRunTool(manager)
		release := NewReleaseTool(manager)

		answer := eventIntegrityRun(t, run, KindTmux, key, "echo beta; sleep 1; exit 7", "2m")
		if !strings.Contains(answer, "beta") {
			t.Fatalf("exec_run answer = %q, want the frame carrying beta", answer)
		}
		if strings.Contains(answer, "exit=") {
			t.Fatalf("exec_run answer = %q carries an exit code before any stop", answer)
		}

		ctx := context.Background()
		eventIntegrityAwait(t, 20*time.Second, "the process behind the key to exit", func() (bool, error) {
			observed, err := manager.Observe(ctx, key)
			if err != nil {
				return false, err
			}
			return observed.State == ProbeDead, nil
		})

		released, err := eventIntegrityRelease(t, release, key)
		if err != nil {
			t.Fatalf("exec_release after the real exit = %v", err)
		}
		if !strings.Contains(released, "released "+key.String()) || !strings.Contains(released, "after confirmed stop") {
			t.Fatalf("exec_release answer = %q, want the confirmed release under %s", released, key)
		}
		if !strings.Contains(released, "exit=7") {
			t.Fatalf("exec_release answer = %q, want the observed exit code 7", released)
		}
		if held := manager.Held(); held != 0 {
			t.Fatalf("held after the confirmed release = %d, want nothing held", held)
		}
		if freed := manager.Released(); freed != 1 {
			t.Fatalf("released = %d, want the one confirmed release", freed)
		}
		if _, err := manager.Explain(key); !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("explain after the confirmed release = %v, want ErrResourceUnknown", err)
		}
		t.Logf("STATE exit-code: run=%q release=%q held=%d", answer, released, manager.Held())
	})

	t.Run("released_or_named_unknown", func(t *testing.T) {
		manager, _ := eventIntegrityManager(t)
		key := eventIntegrityKey("released")
		eventIntegrityCleanup(t, manager, key)
		run := NewRunTool(manager)
		release := NewReleaseTool(manager)

		if answer := eventIntegrityRun(t, run, KindTmux, key, "echo delta; sleep 30", "2m"); !strings.Contains(answer, "delta") {
			t.Fatalf("exec_run answer = %q, want the frame carrying delta", answer)
		}
		released, err := eventIntegrityRelease(t, release, key)
		if err != nil {
			t.Fatalf("exec_release over the live resource = %v", err)
		}
		if !strings.Contains(released, "released "+key.String()) || !strings.Contains(released, "after confirmed stop") {
			t.Fatalf("exec_release answer = %q, want the confirmed release under %s", released, key)
		}
		if !strings.Contains(released, "exit=-1") {
			t.Fatalf("exec_release answer = %q, want the unobserved live exit reported as -1", released)
		}
		if held := manager.Held(); held != 0 {
			t.Fatalf("held after the confirmed release = %d, want nothing held", held)
		}
		unknownAnswer, err := eventIntegrityRelease(t, release, key)
		if !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("release of the freed key = %v, want ErrResourceUnknown", err)
		}
		if unknownAnswer != "" {
			t.Fatalf("release of the freed key answered %q, want the named refusal only", unknownAnswer)
		}
		if _, err := manager.Explain(key); !errors.Is(err, ErrResourceUnknown) {
			t.Fatalf("explain of the freed key = %v, want ErrResourceUnknown", err)
		}

		backend := &scriptedExecutor{
			captureFn: func() ([]byte, error) { return []byte("partial frame"), nil },
			stopFn: func(context.Context) (StopEvidence, error) {
				return StopEvidence{Confirmed: false, Reason: "the executor did not observe an ending"}, nil
			},
		}
		retainedManager := NewManager(backend)
		retainedKey := ResourceKey{Kind: "probe", Key: "retained-unconfirmed"}
		retainedRun := NewRunTool(retainedManager)
		retainedRelease := NewReleaseTool(retainedManager)
		if answer := eventIntegrityRun(t, retainedRun, "probe", retainedKey, "unused", "2m"); !strings.Contains(answer, "partial frame") {
			t.Fatalf("exec_run answer over the scripted executor = %q, want its recorded frame", answer)
		}
		retained, err := eventIntegrityRelease(t, retainedRelease, retainedKey)
		if err != nil {
			t.Fatalf("exec_release over the unconfirmed stop = %v", err)
		}
		if !strings.Contains(retained, "retained "+retainedKey.String()) || !strings.Contains(retained, "stop unconfirmed") {
			t.Fatalf("exec_release answer = %q, want the retained resource beside its unconfirmed stop", retained)
		}
		if !strings.Contains(retained, "the executor did not observe an ending") {
			t.Fatalf("exec_release answer = %q, want the reason the stop stayed unconfirmed", retained)
		}
		if held := retainedManager.Held(); held != 1 {
			t.Fatalf("held after the unconfirmed stop = %d, want the resource retained", held)
		}
		if freed := retainedManager.Released(); freed != 0 {
			t.Fatalf("released = %d, want no release beside an unconfirmed stop", freed)
		}
		retainedExplanation, err := retainedManager.Explain(retainedKey)
		if err != nil {
			t.Fatalf("explain after the unconfirmed stop = %v", err)
		}
		if retainedExplanation.LastStop == nil || retainedExplanation.LastStop.Confirmed {
			t.Fatalf("explain.last_stop = %+v, want the unconfirmed stop recorded", retainedExplanation.LastStop)
		}
		if backend.stopCount() != 1 {
			t.Fatalf("scripted stops = %d, want the one stop the release drove", backend.stopCount())
		}
		t.Logf("STATE released: release=%q unknown=%v retained=%q held=%d",
			released, ErrResourceUnknown, retained, retainedManager.Held())
	})

	t.Run("stable_is_not_success", func(t *testing.T) {
		manager, executor := eventIntegrityManager(t)
		key := eventIntegrityKey("stable")
		eventIntegrityCleanup(t, manager, key)
		run := NewRunTool(manager)

		answer := eventIntegrityRun(t, run, KindTmux, key, "echo gamma; sleep 30", "2m")
		if !strings.Contains(answer, "gamma") {
			t.Fatalf("exec_run answer = %q, want the frame carrying gamma", answer)
		}
		eventIntegrityAssertNoCompletionVerdict(t, "exec_run answer before the stable observation", answer)

		ctx := context.Background()
		monitor := NewMonitor(executor, MonitorConfig{
			Interval:         50 * time.Millisecond,
			StableDuration:   200 * time.Millisecond,
			FakeDeadDuration: 10 * time.Second,
		})
		first, err := monitor.Poll(ctx, key)
		if err != nil {
			t.Fatalf("first monitor poll = %v", err)
		}
		if first != MonitorWatch {
			t.Fatalf("first monitor status = %q, want %q", first, MonitorWatch)
		}
		status := eventIntegrityAwaitMonitor(t, monitor, key, MonitorStable, 5*time.Second)
		stored, observedAtAll := monitor.Status(key)
		if !observedAtAll || stored != MonitorStable {
			t.Fatalf("monitor status = (%q, %v), want (%q, true)", stored, observedAtAll, MonitorStable)
		}

		observed, err := manager.Observe(ctx, key)
		if err != nil {
			t.Fatalf("observe beside the stable frame = %v", err)
		}
		if observed.State != ProbeAlive {
			t.Fatalf("observed state beside the stable frame = %v, want %v", observed.State, ProbeAlive)
		}
		if held := manager.Held(); held != 1 {
			t.Fatalf("held beside the stable frame = %d, want the resource still held", held)
		}
		eventIntegrityAssertUnsettled(t, manager, key)
		eventIntegrityAssertMonitorVocabulary(t)

		sameKey := eventIntegrityRun(t, run, KindTmux, key, "unused", "2m")
		if !strings.Contains(sameKey, "gamma") {
			t.Fatalf("same-key exec_run answer = %q, want the stable frame carrying gamma", sameKey)
		}
		eventIntegrityAssertNoCompletionVerdict(t, "same-key exec_run answer beside a stable frame", sameKey)

		t.Logf("STATE stable: status=%q alive=%v held=%d same-key=%q; "+
			"the package exposes no success/completion report face",
			status, observed.State, manager.Held(), sameKey)
	})
}

// eventIntegrityManager returns the manager over a real tmux executor.
func eventIntegrityManager(t *testing.T) (*Manager, *TmuxExecutor) {
	t.Helper()
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v, want a tmux executor", err)
	}
	return NewManager(executor), executor
}

// eventIntegrityKey returns a tmux resource key whose session name belongs to
// this test run.
func eventIntegrityKey(label string) ResourceKey {
	return ResourceKey{Kind: KindTmux, Key: fmt.Sprintf("tagent-ei-%s-%d", label, time.Now().UnixNano())}
}

// eventIntegrityCleanup releases the resource and removes its session after the
// test, whatever the test observed.
func eventIntegrityCleanup(t *testing.T, manager *Manager, key ResourceKey) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = manager.Release(ctx, key)
		_ = osexec.Command("tmux", "kill-session", "-t", key.Key).Run()
	})
}

// eventIntegrityRun drives one formal exec_run call and returns its answer.
func eventIntegrityRun(t *testing.T, tool *RunTool, kind string, key ResourceKey, script, ttl string) string {
	t.Helper()
	args, err := json.Marshal(map[string]any{
		"command":  "sh",
		"args":     []string{"-c", script},
		"kind":     kind,
		"resource": key.Key,
		"ttl":      ttl,
	})
	if err != nil {
		t.Fatalf("run arguments = %v", err)
	}
	answer, err := tool.Call(context.Background(), args)
	if err != nil {
		t.Fatalf("exec_run over %s = %v", key, err)
	}
	return fmt.Sprint(answer)
}

// eventIntegrityRelease drives one formal exec_release call and returns its
// answer, with an empty answer standing beside a named refusal.
func eventIntegrityRelease(t *testing.T, tool *ReleaseTool, key ResourceKey) (string, error) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"kind": key.Kind, "resource": key.Key})
	if err != nil {
		t.Fatalf("release arguments = %v", err)
	}
	answer, err := tool.Call(context.Background(), args)
	if answer == nil {
		return "", err
	}
	return fmt.Sprint(answer), err
}

// eventIntegrityAwait polls one observation until it holds or the deadline.
func eventIntegrityAwait(t *testing.T, timeout time.Duration, description string, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		holds, err := check()
		switch {
		case err != nil:
			lastErr = err
		case holds:
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("await %s = %v, want it within %s", description, lastErr, timeout)
	}
	t.Fatalf("await %s did not hold within %s", description, timeout)
}

// eventIntegrityAwaitMonitor polls one monitor until it reports the wanted
// classification or the deadline.
func eventIntegrityAwaitMonitor(t *testing.T, monitor *Monitor, key ResourceKey, want MonitorStatus, timeout time.Duration) MonitorStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last MonitorStatus
	for time.Now().Before(deadline) {
		status, err := monitor.Poll(context.Background(), key)
		if err != nil {
			t.Fatalf("monitor poll = %v", err)
		}
		last = status
		if status == want {
			return status
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("monitor status = %q after %s, want %q", last, timeout, want)
	return last
}

// eventIntegrityAssertUnsettled pins that the held resource keeps its execution
// outcome open: no stop was observed, and the explain face still carries the
// unsettled obligation.
func eventIntegrityAssertUnsettled(t *testing.T, manager *Manager, key ResourceKey) Explanation {
	t.Helper()
	explanation, err := manager.Explain(key)
	if err != nil {
		t.Fatalf("explain = %v, want the held resource", err)
	}
	if !explanation.Held {
		t.Fatalf("explain.held = false, want the resource still held")
	}
	if explanation.LastStop != nil {
		t.Fatalf("explain.last_stop = %+v, want no stop observed", explanation.LastStop)
	}
	if !strings.Contains(strings.ToLower(strings.Join(explanation.Obligations, " ")), "unsettled") {
		t.Fatalf("explain.obligations = %v, want the unsettled execution outcome kept open", explanation.Obligations)
	}
	return explanation
}

// eventIntegrityCompletionTokens are the words a success or completion verdict
// would carry. No query face of the package may answer with any of them.
var eventIntegrityCompletionTokens = []string{"success", "succeed", "complete", "completed", "done", "finished"}

// eventIntegrityAssertNoCompletionVerdict pins that one query answer reports no
// success or completion verdict.
func eventIntegrityAssertNoCompletionVerdict(t *testing.T, surface, answer string) {
	t.Helper()
	lowered := strings.ToLower(answer)
	for _, token := range eventIntegrityCompletionTokens {
		if strings.Contains(lowered, token) {
			t.Fatalf("%s = %q reports %q; no query face may report success or completion", surface, answer, token)
		}
	}
}

// eventIntegrityAssertMonitorVocabulary pins the whole monitor verdict
// vocabulary: silence classifications only, none of them a success verdict.
func eventIntegrityAssertMonitorVocabulary(t *testing.T) {
	t.Helper()
	for _, status := range []MonitorStatus{MonitorWatch, MonitorStable, MonitorSuspect} {
		eventIntegrityAssertNoCompletionVerdict(t, "monitor status", string(status))
	}
}
