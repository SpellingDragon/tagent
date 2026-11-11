// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
//
// Package-internal proof for the exec integrity domain: a held resource is
// probed before it is reused, a dead pane is released and the command rebuilt,
// a stale generation is never answered as this run's result, and the generation
// marker is stripped from the receipt beside the proof that the frame was this
// generation's. The fast fake-tmux faces and the real tmux integration anchors
// live together here.
package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// reuseSceneExecutor is a scripted executor that can present a dead pane beside
// the frame a previous generation left behind: Start answers one frame per
// generation, so a rebuild is visible as a new start and a new frame.
type reuseSceneExecutor struct {
	mu     sync.Mutex
	starts int
	probes int
	stops  int
	dead   bool
	frames []string
}

func (e *reuseSceneExecutor) Start(_ context.Context, decl ExecutionDecl) (ResourceKey, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.starts++
	return decl.Resource, nil
}

func (e *reuseSceneExecutor) Probe(_ context.Context, _ ResourceKey) (ProbeResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probes++
	if e.dead {
		return ProbeResult{State: ProbeDead, Detail: "the pane is dead"}, nil
	}
	return ProbeResult{State: ProbeAlive}, nil
}

func (e *reuseSceneExecutor) Capture(_ context.Context, _ ResourceKey) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.starts < 1 || e.starts > len(e.frames) {
		return []byte("frame-without-a-generation\n"), nil
	}
	return []byte(e.frames[e.starts-1]), nil
}

func (e *reuseSceneExecutor) Stop(_ context.Context, key ResourceKey) (StopEvidence, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stops++
	return StopEvidence{Resource: key, Confirmed: true, ExitCode: 0}, nil
}

func (e *reuseSceneExecutor) markDead(dead bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dead = dead
}

func (e *reuseSceneExecutor) startCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts
}

func (e *reuseSceneExecutor) probeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.probes
}

// staleFrameExecutor answers the captures of the generations it is told to keep
// stale with the named stale-generation error, and every other capture with a
// fresh frame: it is the scripted shape of an executor whose capture check saw
// the scrollback of an earlier generation.
type staleFrameExecutor struct {
	mu       sync.Mutex
	starts   int
	probes   int
	stale    map[int]bool
	stopping int
}

func (e *staleFrameExecutor) Start(_ context.Context, decl ExecutionDecl) (ResourceKey, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.starts++
	return decl.Resource, nil
}

func (e *staleFrameExecutor) Probe(_ context.Context, _ ResourceKey) (ProbeResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probes++
	return ProbeResult{State: ProbeDead, Detail: "the pane is dead"}, nil
}

func (e *staleFrameExecutor) Capture(_ context.Context, _ ResourceKey) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stale[e.starts] {
		return nil, errStaleGeneration
	}
	return []byte(fmt.Sprintf("fresh-output-%d\n", e.starts)), nil
}

func (e *staleFrameExecutor) Stop(_ context.Context, key ResourceKey) (StopEvidence, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopping++
	return StopEvidence{Resource: key, Confirmed: true, ExitCode: 0}, nil
}

func (e *staleFrameExecutor) startCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts
}

// TestDeadPaneNotReusedOnHeldPath pins the held run face over a dead pane.
// - command A leaves its frame on the dead pane
// - command B over the same key must not answer that frame
// - the dead generation is released and the command runs in a new one
func TestDeadPaneNotReusedOnHeldPath(t *testing.T) {
	ctx := context.Background()
	backend := &reuseSceneExecutor{
		dead:   true,
		frames: []string{"stale-output\n", "fresh-output\n"},
	}
	manager := NewManager(backend)
	run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
	key := ResourceKey{Kind: "probe", Key: "dead-pane-held"}
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })

	answerA, err := run.Call(ctx, execIntegrityRunArgs(t, key))
	if err != nil {
		t.Fatalf("command A through exec_run = %v", err)
	}
	if !strings.Contains(fmt.Sprint(answerA), "stale-output") {
		t.Fatalf("the scene never formed: the receipt of command A = %q, want the first generation's frame", answerA)
	}

	backend.markDead(true)
	answerB, errB := run.Call(ctx, execIntegrityRunArgs(t, key))
	if errB != nil {
		t.Fatalf("command B over the dead pane = %v, want the rebuilt execution", errB)
	}
	receiptB := fmt.Sprint(answerB)
	if strings.Contains(receiptB, "stale-output") {
		t.Fatalf("the receipt of command B replayed the dead generation's frame: %q", receiptB)
	}
	if !strings.Contains(receiptB, "fresh-output") {
		t.Fatalf("the receipt of command B = %q, want the rebuilt generation's frame", receiptB)
	}
	if !strings.Contains(receiptB, "rebuild:") {
		t.Fatalf("the receipt of command B = %q, want the rebuild fact in its metadata", receiptB)
	}
	if got := backend.startCount(); got != 2 {
		t.Fatalf("the executor started %d generations, want the dead one released and rebuilt", got)
	}
	if got := backend.probeCount(); got < 2 {
		t.Fatalf("the executor observed %d probes, want the held resource probed before reuse", got)
	}
	if got := manager.Reused(); got != 0 {
		t.Fatalf("reused = %d, want no reuse of the dead pane", got)
	}
	if got := manager.rebuiltCount(); got != 1 {
		t.Fatalf("rebuilt = %d, want one rebuild of the dead generation", got)
	}
	explanation, err := manager.Explain(key)
	if err != nil {
		t.Fatalf("Explain after the rebuild = %v", err)
	}
	if explanation.rebuilds != 1 {
		t.Fatalf("explain.rebuilds = %d, want the rebuild fact beside the new generation", explanation.rebuilds)
	}
}

// TestReusePathProbesPaneState pins the probe a held acquisition takes.
// - a live held resource is reused after exactly one probe
// - a dead held resource is released and rebuilt
// - an undecidable probe refuses the reuse by name
func TestReusePathProbesPaneState(t *testing.T) {
	ctx := context.Background()
	manager, backend, _ := newLeaseTestManager(t)
	decl := testExecutionDecl("reuse-probe")

	if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
		t.Fatalf("first acquisition = %v", err)
	}
	if got := backend.probeCount(); got != 0 {
		t.Fatalf("a fresh start probed %d times, want no probe before the resource exists", got)
	}
	if _, err := manager.Acquire(ctx, decl, WithTTL(time.Minute)); err != nil {
		t.Fatalf("held acquisition = %v", err)
	}
	if got := backend.probeCount(); got != 1 {
		t.Fatalf("the held reuse observed %d probes, want exactly one probe of the held pane", got)
	}
	if got := manager.Reused(); got != 1 {
		t.Fatalf("reused = %d, want the live held resource reused", got)
	}

	backend.probeFn = func() (ProbeResult, error) {
		return ProbeResult{State: ProbeDead, Detail: "the pane is dead"}, nil
	}
	if _, err := manager.Acquire(ctx, decl); err != nil {
		t.Fatalf("acquisition over the dead held pane = %v, want a rebuilt generation", err)
	}
	if got := backend.probeCount(); got != 2 {
		t.Fatalf("the dead held pane observed %d probes, want its state probed before the rebuild", got)
	}
	if got := backend.startCount(); got != 2 {
		t.Fatalf("the executor started %d generations, want the dead one rebuilt", got)
	}
	if got := manager.rebuiltCount(); got != 1 {
		t.Fatalf("rebuilt = %d, want one rebuild", got)
	}

	backend.probeFn = func() (ProbeResult, error) {
		return ProbeResult{State: ProbeUnknown, Detail: "the pane did not answer"}, nil
	}
	if _, err := manager.Acquire(ctx, decl); !errors.Is(err, errReuseUnconfirmed) {
		t.Fatalf("acquisition beside an undecidable held pane = %v, want the named reuse refusal", err)
	}
	if got := backend.startCount(); got != 2 {
		t.Fatalf("the undecidable probe started %d generations, want no rebuild on a verdict it does not have", got)
	}
}

// TestRebuildRetryMetadataRecorded pins the rebuild and retry facts.
// - a dead-pane rebuild is carried in the receipt and in Explain
// - a stale-generation retry is carried beside the rebuilt frame
func TestRebuildRetryMetadataRecorded(t *testing.T) {
	ctx := context.Background()

	t.Run("dead_pane_rebuild_is_recorded", func(t *testing.T) {
		backend := &reuseSceneExecutor{
			dead:   true,
			frames: []string{"old-frame\n", "new-frame\n"},
		}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "rebuild-metadata"}
		t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
		if _, err := run.Call(ctx, execIntegrityRunArgs(t, key)); err != nil {
			t.Fatalf("command A = %v", err)
		}
		backend.markDead(true)
		answer, err := run.Call(ctx, execIntegrityRunArgs(t, key))
		if err != nil {
			t.Fatalf("command B = %v", err)
		}
		receipt := fmt.Sprint(answer)
		if !strings.Contains(receipt, "rebuild:") {
			t.Fatalf("receipt = %q, want the rebuild fact", receipt)
		}
		explanation, err := manager.Explain(key)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if explanation.rebuilds != 1 || manager.rebuiltCount() != 1 {
			t.Fatalf("rebuild fact = explanation %d manager %d, want one recorded rebuild", explanation.rebuilds, manager.rebuiltCount())
		}
	})

	t.Run("stale_generation_retry_is_recorded", func(t *testing.T) {
		backend := &staleFrameExecutor{stale: map[int]bool{1: true}}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "retry-metadata"}
		t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
		answer, err := run.Call(ctx, execIntegrityRunArgs(t, key))
		if err != nil {
			t.Fatalf("the retried execution = %v", err)
		}
		receipt := fmt.Sprint(answer)
		if !strings.Contains(receipt, "fresh-output-2") {
			t.Fatalf("receipt = %q, want the rebuilt generation's frame", receipt)
		}
		if !strings.Contains(receipt, "retry:") {
			t.Fatalf("receipt = %q, want the retry fact", receipt)
		}
		explanation, err := manager.Explain(key)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if explanation.staleRetries != 1 || manager.retryCount() != 1 {
			t.Fatalf("retry fact = explanation %d manager %d, want one recorded retry", explanation.staleRetries, manager.retryCount())
		}
		if got := backend.startCount(); got != 2 {
			t.Fatalf("the retry started %d generations, want exactly one rebuild", got)
		}
	})

	t.Run("rebuild_fact_survives_the_retry", func(t *testing.T) {
		backend := &staleFrameExecutor{stale: map[int]bool{2: true}}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "rebuild-retry-metadata"}
		t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
		if _, err := run.Call(ctx, execIntegrityRunArgs(t, key)); err != nil {
			t.Fatalf("command A = %v", err)
		}
		answer, err := run.Call(ctx, execIntegrityRunArgs(t, key))
		if err != nil {
			t.Fatalf("the rebuilt and retried execution = %v", err)
		}
		receipt := fmt.Sprint(answer)
		if !strings.Contains(receipt, "fresh-output-3") {
			t.Fatalf("receipt = %q, want the retried generation's frame", receipt)
		}
		if !strings.Contains(receipt, "rebuild:") || !strings.Contains(receipt, "retry:") {
			t.Fatalf("receipt = %q, want both the rebuild and the retry facts", receipt)
		}
		explanation, err := manager.Explain(key)
		if err != nil {
			t.Fatalf("Explain = %v", err)
		}
		if explanation.rebuilds != 1 || explanation.staleRetries != 1 {
			t.Fatalf("explanation rebuilds=%d staleRetries=%d, want the rebuilt generation's facts kept through the retry", explanation.rebuilds, explanation.staleRetries)
		}
		if got := backend.startCount(); got != 3 {
			t.Fatalf("the call started %d generations, want command A, the rebuild, and the retry", got)
		}
	})
}

// TestStaleScrollbackNotTrusted pins the explicit failure of two stale generations.
// - both the first and the rebuilt capture answer a stale frame
// - the call returns the named failure and no success receipt
func TestStaleScrollbackNotTrusted(t *testing.T) {
	ctx := context.Background()
	backend := &staleFrameExecutor{stale: map[int]bool{1: true, 2: true}}
	manager := NewManager(backend)
	run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
	key := ResourceKey{Kind: "probe", Key: "stale-scrollback"}
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })

	answer, err := run.Call(ctx, execIntegrityRunArgs(t, key))
	if err == nil {
		t.Fatalf("the two stale generations answered %q, want the explicit failure", answer)
	}
	if answer != nil {
		t.Fatalf("the stale failure carried a receipt %q, want no success claim", answer)
	}
	if !errors.Is(err, errStaleGeneration) {
		t.Fatalf("the stale failure = %v, want the named stale-generation error", err)
	}
	if got := backend.startCount(); got != 2 {
		t.Fatalf("the stale retry started %d generations, want the bounded single retry", got)
	}
	if got := manager.retryCount(); got != 1 {
		t.Fatalf("retry count = %d, want one recorded retry", got)
	}
}

// execIntegrityRunArgs renders one formal exec_run call over the given resource.
func execIntegrityRunArgs(t *testing.T, key ResourceKey) []byte {
	t.Helper()
	return execIntegrityRunArgsFor(t, key, "sh", []string{"-c", "echo run"})
}

// execIntegrityRunArgsFor renders one formal exec_run call for the given command.
func execIntegrityRunArgsFor(t *testing.T, key ResourceKey, command string, args []string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"command":  command,
		"args":     args,
		"kind":     key.Kind,
		"resource": key.Key,
		"ttl":      "1m",
	})
	if err != nil {
		t.Fatalf("run arguments = %v", err)
	}
	return raw
}

// fakeTmuxScript emulates the tmux subcommands the executor calls over one
// session recorded in FAKE_TMUX_STATE. respawn-pane writes the frame the
// generation's command would have produced: in normal mode it carries the
// generation marker the executor injected into the command line, and in
// no-marker mode it writes the scrollback of an earlier generation instead.
const fakeTmuxScript = `#!/bin/sh
set -u
state="${FAKE_TMUX_STATE:?}"
mode="${FAKE_TMUX_MODE:-normal}"
cmd="${1:-}"
if [ $# -gt 0 ]; then shift; fi
case "$cmd" in
  list-sessions)
    if [ -f "$state/session" ]; then cat "$state/session"; fi
    ;;
  display-message)
    fmt=""
    for arg in "$@"; do fmt="$arg"; done
    if [ -f "$state/dead" ]; then
      case "$fmt" in
        *pane_dead_status*) echo "1 0" ;;
        *) echo 1 ;;
      esac
    else
      case "$fmt" in
        *pane_dead_status*) echo "0 " ;;
        *) echo 0 ;;
      esac
    fi
    ;;
  new-session)
    name=""
    while [ $# -gt 0 ]; do
      case "$1" in
        -s) shift; name="${1:-}" ;;
      esac
      shift
    done
    printf '%s' "$name" > "$state/session"
    : > "$state/frame"
    rm -f "$state/dead"
    n=0
    if [ -f "$state/starts" ]; then n=$(cat "$state/starts"); fi
    n=$((n+1))
    printf '%s' "$n" > "$state/starts"
    ;;
  set-option)
    ;;
  show-options)
    echo on
    ;;
  respawn-pane)
    line=""
    for arg in "$@"; do line="$arg"; done
    if [ "$mode" = "no-marker" ]; then
      printf 'stale-scrollback\n' > "$state/frame"
    else
      n=$(cat "$state/starts")
      token=$(printf '%s\n' "$line" | sed -n 's/.*<<<exec-generation:\([A-Za-z0-9]*\)>>>.*/\1/p' | head -1)
      printf '<<<exec-generation:%s>>>\nfresh-output-%s\n' "$token" "$n" > "$state/frame"
    fi
    : > "$state/dead"
    ;;
  capture-pane)
    if [ -f "$state/frame" ]; then cat "$state/frame"; fi
    ;;
  kill-session)
    rm -f "$state/session" "$state/dead"
    ;;
esac
exit 0
`

// fakeTmuxExecutor returns a real TmuxExecutor over the fake tmux script in the
// given mode: the fast regression face exercises the production verification
// and stripping paths without a real tmux server.
func fakeTmuxExecutor(t *testing.T, mode string) (*TmuxExecutor, string) {
	t.Helper()
	binDir := t.TempDir()
	stateDir := t.TempDir()
	script := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(script, []byte(fakeTmuxScript), 0o700); err != nil {
		t.Fatalf("write the fake tmux script = %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TMUX_STATE", stateDir)
	t.Setenv("FAKE_TMUX_MODE", mode)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction over the fake tmux = %v", err)
	}
	return executor, stateDir
}

// TestGenerationTokenFakeTmuxFast pins the generation paths over a fake tmux.
// - every start mints a new token
// - the marker is stripped from the receipt
// - a dead held generation is rebuilt instead of replayed
func TestGenerationTokenFakeTmuxFast(t *testing.T) {
	executor, stateDir := fakeTmuxExecutor(t, "normal")
	manager := NewManager(executor)
	run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
	key := ResourceKey{Kind: KindTmux, Key: "fake-fast"}
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })

	answerA, err := run.Call(context.Background(), execIntegrityRunArgsFor(t, key, "sh", []string{"-c", "echo first"}))
	if err != nil {
		t.Fatalf("command A through the fake tmux = %v", err)
	}
	receiptA := fmt.Sprint(answerA)
	if !strings.Contains(receiptA, "fresh-output-1") {
		t.Fatalf("the receipt of command A = %q, want the fake generation's frame", receiptA)
	}
	if strings.Contains(receiptA, generationMarkerPrefix) {
		t.Fatalf("the receipt of command A = %q carries the generation marker", receiptA)
	}
	if !strings.Contains(receiptA, "execution proof: generation verified") {
		t.Fatalf("the receipt of command A = %q, want the verified generation proof", receiptA)
	}
	first, ok := executor.generationOf(key)
	if !ok {
		t.Fatalf("the executor keeps no generation for the first run")
	}

	answerB, err := run.Call(context.Background(), execIntegrityRunArgsFor(t, key, "sh", []string{"-c", "echo second"}))
	if err != nil {
		t.Fatalf("command B over the fake dead pane = %v", err)
	}
	receiptB := fmt.Sprint(answerB)
	if !strings.Contains(receiptB, "fresh-output-2") {
		t.Fatalf("the receipt of command B = %q, want the rebuilt generation's frame", receiptB)
	}
	if strings.Contains(receiptB, "fresh-output-1") {
		t.Fatalf("the receipt of command B = %q replayed the first generation's frame", receiptB)
	}
	if strings.Contains(receiptB, generationMarkerPrefix) {
		t.Fatalf("the receipt of command B = %q carries the generation marker", receiptB)
	}
	if !strings.Contains(receiptB, "rebuild:") {
		t.Fatalf("the receipt of command B = %q, want the rebuild fact", receiptB)
	}
	second, ok := executor.generationOf(key)
	if !ok {
		t.Fatalf("the executor keeps no generation for the rebuilt run")
	}
	if first.token == second.token {
		t.Fatalf("the rebuilt generation carries the old token %q, want a new one", first.token)
	}
	if got := manager.Acquired(); got != 2 {
		t.Fatalf("acquired = %d, want two generations", got)
	}
	if got := manager.rebuiltCount(); got != 1 {
		t.Fatalf("rebuilt = %d, want one rebuild", got)
	}
	starts, err := os.ReadFile(filepath.Join(stateDir, "starts"))
	if err != nil {
		t.Fatalf("read the fake tmux start count = %v", err)
	}
	if strings.TrimSpace(string(starts)) != "2" {
		t.Fatalf("the fake tmux started %q session(s), want two", strings.TrimSpace(string(starts)))
	}
}

// TestGenerationCheckIsUnconditional pins that any TAGENT_EXPERIMENT_* value is inert: a missing marker is still refused as a stale generation.
func TestGenerationCheckIsUnconditional(t *testing.T) {
	for _, name := range []string{
		"TAGENT_EXPERIMENT_EXEC_GENERATION",
		"TAGENT_EXPERIMENT_DELIVERY_EVENTIFIED",
		"TAGENT_EXPERIMENT_REQUEST_BUDGET",
	} {
		t.Setenv(name, "off")
	}
	executor, _ := fakeTmuxExecutor(t, "no-marker")
	manager := NewManager(executor)
	run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
	key := ResourceKey{Kind: KindTmux, Key: "unconditional"}
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
	answer, err := run.Call(context.Background(), execIntegrityRunArgsFor(t, key, "sh", []string{"-c", "echo first"}))
	if err == nil {
		t.Fatalf("the missing marker answered %q, want the explicit stale-generation failure", answer)
	}
	if !errors.Is(err, errStaleGeneration) {
		t.Fatalf("the missing marker = %v, want the named stale-generation error", err)
	}
	if got := manager.retryCount(); got != 1 {
		t.Fatalf("retry count = %d, want the single bounded retry", got)
	}
}

// TestRebuiltSessionReallyRuns pins the rebuild on a real tmux.
// - command A ends in its dead pane
// - command B starts in a new session, writes its disk anchor, and exits 7
// - the receipt carries the rebuild fact and no frame of command A
func TestRebuiltSessionReallyRuns(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v", err)
	}
	manager := NewManager(executor)
	run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
	key := tmuxTestKey()
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })

	const staleMarker = "dead-generation-marker"
	answerA, err := run.Call(context.Background(), execIntegrityRunArgsFor(t, key, "sh", []string{"-c", "echo " + staleMarker}))
	if err != nil {
		t.Fatalf("command A = %v", err)
	}
	if !strings.Contains(fmt.Sprint(answerA), staleMarker) {
		t.Fatalf("the scene never formed: the receipt of command A = %q, want its marker", answerA)
	}
	waitForPaneDead(t, executor, key)

	anchor := filepath.Join(t.TempDir(), "rebuilt-really-ran")
	answerB, err := run.Call(context.Background(), execIntegrityRunArgsFor(t, key, "sh", []string{"-c", fmt.Sprintf("touch %s; exit 7", shellQuote(anchor))}))
	if err != nil {
		t.Fatalf("command B over the dead pane = %v, want the rebuilt execution", err)
	}
	receiptB := fmt.Sprint(answerB)
	if strings.Contains(receiptB, staleMarker) {
		t.Fatalf("the receipt of command B replayed the dead generation's scrollback: %q", receiptB)
	}
	if !strings.Contains(receiptB, "rebuild:") {
		t.Fatalf("the receipt of command B = %q, want the rebuild fact", receiptB)
	}
	if !strings.Contains(receiptB, "execution proof: generation verified") {
		t.Fatalf("the receipt of command B = %q, want the verified generation proof", receiptB)
	}
	if !waitForPath(anchor, 2*time.Second) {
		t.Fatalf("command B claimed a rebuilt execution but its disk anchor %s never appeared", anchor)
	}

	waitForPaneDead(t, executor, key)
	evidence, err := manager.Release(context.Background(), key)
	if err != nil {
		t.Fatalf("release after command B = %v", err)
	}
	if !evidence.Released || !evidence.Stop.Confirmed {
		t.Fatalf("release evidence = %+v, want the rebuilt session confirmed stopped", evidence)
	}
	if evidence.Stop.ExitCode != 7 {
		t.Fatalf("the rebuilt session exit = %d, want the 7 command B ended with", evidence.Stop.ExitCode)
	}
	if got := manager.Acquired(); got != 2 {
		t.Fatalf("acquired = %d, want command A and the rebuilt command B", got)
	}
	if got := manager.rebuiltCount(); got != 1 {
		t.Fatalf("rebuilt = %d, want one rebuild", got)
	}
}

// TestTokenStrippedFromReceipt pins the generation marker out of the output.
// - the receipt and a direct capture carry the command's own line
// - neither carries the marker or the token
func TestTokenStrippedFromReceipt(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v", err)
	}
	manager := NewManager(executor)
	run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
	key := tmuxTestKey()
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })

	answer, err := run.Call(context.Background(), execIntegrityRunArgsFor(t, key, "sh", []string{"-c", "echo clean-output"}))
	if err != nil {
		t.Fatalf("the execution = %v", err)
	}
	receipt := fmt.Sprint(answer)
	if !strings.Contains(receipt, "clean-output") {
		t.Fatalf("the receipt = %q, want the command's own line", receipt)
	}
	if strings.Contains(receipt, generationMarkerPrefix) {
		t.Fatalf("the receipt = %q carries the generation marker", receipt)
	}
	generation, ok := executor.generationOf(key)
	if !ok {
		t.Fatalf("the executor keeps no generation for the run")
	}
	if strings.Contains(receipt, generation.token) {
		t.Fatalf("the receipt = %q carries the generation token %q", receipt, generation.token)
	}
	frame, err := executor.Capture(context.Background(), key)
	if err != nil {
		t.Fatalf("the direct capture = %v", err)
	}
	if strings.Contains(string(frame), generationMarkerPrefix) {
		t.Fatalf("the direct capture = %q carries the generation marker", frame)
	}
	if !strings.Contains(string(frame), "clean-output") {
		t.Fatalf("the direct capture = %q, want the command's own line", frame)
	}
}

// TestRunReceiptCarriesExecutionProof pins the execution proof line.
// - a verified real-tmux execution says so
// - an executor without the generation face makes no proof claim
// - a stale failure is never a success receipt
func TestRunReceiptCarriesExecutionProof(t *testing.T) {
	t.Run("real_tmux_receipt_states_the_verified_generation", func(t *testing.T) {
		if !IsTmuxAvailable() {
			t.Skip("tmux is not available on this system")
		}
		isolateTmuxServer(t)
		executor, err := NewTmuxExecutor()
		if err != nil {
			t.Fatalf("executor construction = %v", err)
		}
		manager := NewManager(executor)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := tmuxTestKey()
		t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
		answer, err := run.Call(context.Background(), execIntegrityRunArgsFor(t, key, "sh", []string{"-c", "echo proof-anchor"}))
		if err != nil {
			t.Fatalf("the execution = %v", err)
		}
		receipt := fmt.Sprint(answer)
		if !strings.Contains(receipt, "execution proof: generation verified") {
			t.Fatalf("the receipt = %q, want the verified generation proof", receipt)
		}
		if strings.Contains(receipt, generationMarkerPrefix) {
			t.Fatalf("the receipt = %q carries the generation marker", receipt)
		}
	})

	t.Run("executor_without_a_generation_face_makes_no_proof_claim", func(t *testing.T) {
		backend := &scriptedExecutor{
			probeFn: func() (ProbeResult, error) {
				return ProbeResult{State: ProbeDead, Detail: "no session"}, nil
			},
		}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "proof-absent"}
		t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
		answer, err := run.Call(context.Background(), execIntegrityRunArgs(t, key))
		if err != nil {
			t.Fatalf("the execution = %v", err)
		}
		receipt := fmt.Sprint(answer)
		if !strings.Contains(receipt, "recorded output") {
			t.Fatalf("the receipt = %q, want the executor's frame", receipt)
		}
		if strings.Contains(receipt, "execution proof") {
			t.Fatalf("the receipt = %q claims a generation proof the executor cannot offer", receipt)
		}
	})

	t.Run("stale_failure_is_not_a_success_receipt", func(t *testing.T) {
		backend := &staleFrameExecutor{stale: map[int]bool{1: true, 2: true}}
		manager := NewManager(backend)
		run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
		key := ResourceKey{Kind: "probe", Key: "proof-stale"}
		t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
		answer, err := run.Call(context.Background(), execIntegrityRunArgs(t, key))
		if err == nil {
			t.Fatalf("the two stale generations answered %q, want the explicit failure", answer)
		}
		if symptomAnchorReceiptClaimsSuccess(fmt.Sprint(answer), err) {
			t.Fatalf("the stale failure presented a success receipt: %q", answer)
		}
	})
}

// waitForPath reports whether path appeared before the window closed; the
// window only keeps a slow host from reading a real write as an absence.
func waitForPath(path string, window time.Duration) bool {
	settle := time.Now().Add(window)
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false
		}
		if !time.Now().Before(settle) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSymptomAnchorExecDeadPaneIntegrity pins the user-visible symptom of an exec_run over a dead pane.
//
// - 场景：命令 A 在自己的会话里跑到结束，pane 死亡而 remain-on-exit 保留输出，stale marker 仍可被捕获。
// - 命令 B（touch 一个磁盘锚点）经正式 exec_run 入口在同一资源上执行。
// - 回执声明成功 ⇒ 命令 B 的磁盘锚点真实存在（磁盘事实一致）。
// - 命令 B 的回执不得携带/回放上一世代的 stale-output 作为本次结果（世代一致）。
// - 场景在真实 tmux 上重放；tmux 缺席=具名 skip 且视为未交付，域门禁判红。
// - 磁盘事实在有界 settle 后读取：exec_run 已返回，真正跑过的命令立刻留下锚点，窗口只防慢宿主把真实写入读成缺失。
//
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestSymptomAnchorExecDeadPaneIntegrity(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v", err)
	}
	manager := NewManager(executor)
	run := NewRunTool(manager, WithRunMonitor(MonitorConfig{Interval: 20 * time.Millisecond}))
	key := tmuxTestKey()
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })

	const staleMarker = "stale-output"
	answerA, err := run.Call(context.Background(), symptomAnchorRunArgs(t, "sh", []string{"-c", "echo " + staleMarker}, key))
	if err != nil {
		t.Fatalf("command A through exec_run = %v", err)
	}
	receiptA := fmt.Sprint(answerA)
	if !strings.Contains(receiptA, staleMarker) {
		t.Fatalf("the scene never formed: the receipt of command A = %q, want its marker", receiptA)
	}
	waitForPaneDead(t, executor, key)
	probe, err := executor.Probe(context.Background(), key)
	if err != nil || probe.State != ProbeDead {
		t.Fatalf("the scene never formed: the pane after command A = %+v err=%v, want the dead pane remain-on-exit keeps", probe, err)
	}
	if frame, err := executor.Capture(context.Background(), key); err != nil || !strings.Contains(string(frame), staleMarker) {
		t.Fatalf("the scene never formed: the frame of the dead pane = %q err=%v, want the retained %q", frame, err, staleMarker)
	}

	anchor := filepath.Join("/tmp", fmt.Sprintf("tagent-dead-pane-anchor-%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(anchor) })
	if _, err := os.Stat(anchor); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the anchor before command B = %v, want an absent path", err)
	}
	answerB, errB := run.Call(context.Background(), symptomAnchorRunArgs(t, "touch", []string{anchor}, key))
	receiptB := ""
	if answerB != nil {
		receiptB = fmt.Sprint(answerB)
	}
	anchorOnDisk := false
	settle := time.Now().Add(2 * time.Second)
	for {
		_, statErr := os.Stat(anchor)
		if statErr == nil {
			anchorOnDisk = true
			break
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("the anchor after command B cannot be judged: %v", statErr)
		}
		if !time.Now().Before(settle) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	claimsSuccess := symptomAnchorReceiptClaimsSuccess(receiptB, errB)
	staleReplayed := strings.Contains(receiptB, staleMarker)

	if claimsSuccess && !anchorOnDisk {
		t.Errorf("exec_run 回执声明成功，但磁盘锚点不存在：success is not backed by the disk fact\nanchor=%s\nanchor exists=%v\nreceipt=%q\nerror=%v\nstale replayed=%v\ncommand A receipt=%q",
			anchor, anchorOnDisk, receiptB, errB, staleReplayed, receiptA)
	}

	if staleReplayed {
		t.Errorf("exec_run 回执回放了上一世代的 scrollback %q：generation mismatch, the old dead pane's frame was answered as command B's result\nreceipt=%q\nerror=%v\nanchor exists=%v\ncommand A receipt=%q",
			staleMarker, receiptB, errB, anchorOnDisk, receiptA)
	}
}

// symptomAnchorRunArgs renders one formal exec_run call over the given resource.
func symptomAnchorRunArgs(t *testing.T, command string, args []string, key ResourceKey) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"command":  command,
		"args":     args,
		"kind":     key.Kind,
		"resource": key.Key,
		"ttl":      "1m",
	})
	if err != nil {
		t.Fatalf("run arguments = %v", err)
	}
	return raw
}

// symptomAnchorReceiptClaimsSuccess reports whether one exec_run receipt presents
// the call as having run its command: an error-free answer that says it executed
// the command, or reads out the zero exit status a successful execution ends
// with. A refusal, a retention or a reattachment makes no such claim.
func symptomAnchorReceiptClaimsSuccess(receipt string, callErr error) bool {
	if callErr != nil {
		return false
	}
	return strings.HasPrefix(receipt, "executed ") || strings.Contains(receipt, "exit=0")
}
