// The runtime visibility tests of the host: the diagnostic switches the host is
// started with, the faces those switches open over the wired subsystems, the
// unified runtime map that stays readable beside them, and the lifecycle reading
// of a started, closed and restarted host.
package host

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/cli"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// visibilityRecorder is the training record face of these checks: it accepts the
// episode and decision writes and refuses the attempt writes by name, so the
// recording loss account of the run carries an entry the diagnostic face reads.
type visibilityRecorder struct{}

// RecordEpisode accepts the episode write.
func (visibilityRecorder) RecordEpisode(context.Context, rl.EpisodeRecord) error { return nil }

// RecordDecision accepts the decision write.
func (visibilityRecorder) RecordDecision(context.Context, rl.DecisionRecord) error { return nil }

// RecordAttempt refuses the attempt write by name.
func (visibilityRecorder) RecordAttempt(context.Context, rl.AttemptRecord) error {
	return errors.New("the record pipeline refuses the attempt write")
}

// visibilityObserver is the post-commit observation face of these checks: it
// counts the committed facts handed to it, so the diagnostic reading can be held
// against what the sink really took.
type visibilityObserver struct {
	mu   sync.Mutex
	seen int
}

// The observation face plugs into the assembly through the runtime's own port.
var _ plugin.Observer = (*visibilityObserver)(nil)

// Observe counts one committed fact handed over unchanged.
func (o *visibilityObserver) Observe(context.Context, event.Event) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen++
	return nil
}

// observed reports how many committed facts the sink took.
func (o *visibilityObserver) observed() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.seen
}

// visibilityRun assembles one runtime through the sole assembly entry beside the
// options the caller names, over the scripted model and echo tool of the public
// entry harness.
func visibilityRun(t *testing.T, ctx context.Context, opts ...tagent.Option) (*tagent.Run, *entryTool) {
	t.Helper()
	provider := &entryModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		if last := req.Messages[len(req.Messages)-1]; last.Content == "hello" {
			return entryToolTurn(entryCall("call-1", "echo", `{"x":1}`))
		}
		return entryFinalTurn("settled")
	}}
	tool := &entryTool{name: "echo", output: "out-a"}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, append([]tagent.Option{tagent.WithGovernance(passPolicy{})}, opts...)...)
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run, tool
}

// visibilityInvoke drives one invocation through the formal runtime entry at the
// session the caller names.
func visibilityInvoke(t *testing.T, ctx context.Context, run *tagent.Run, invocation event.InvocationID, session event.SessionID) {
	t.Helper()
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: invocation,
		Address:      event.Address{AgentID: "agent-a", SessionID: session},
		Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("invoke the assembled run: %v", err)
	}
}

// visibilityScope is the scope one session of the assembled agent is read under.
func visibilityScope(session event.SessionID) query.AuthorizationScope {
	return query.AuthorizationScope{
		Agents:   []event.AgentID{"agent-a"},
		Sessions: []event.Address{{AgentID: "agent-a", SessionID: session}},
	}
}

// visibilityTaskFilter narrows one reading to the task dimension.
func visibilityTaskFilter(scope query.AuthorizationScope) query.ListFilter {
	return query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}}
}

// TestRuntimeVisibility drives the diagnostic switches of the host over the formal assembly, CLI and HTTP entries and checks what each face reports.
//   - the switch of a wired subsystem opens its face and the reading reflects the real state behind it
//   - a switch that is off closes its face by name while the unified runtime map stays readable
//   - a face the assembly carries no wiring for names that absence instead of answering a zero reading
//   - the reading is taken from the owners of the state, so a later admission shows up and no copy is kept
//   - the host serves its reading behind the request gate and closes through the drain on a termination signal
//   - a restarted host restores the persisted task and the persisted watermark, and invents no record it never stored
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestRuntimeVisibility(t *testing.T) {
	t.Run("开关驱动可见面: a switch opens its face over the wired subsystem", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		anchors := filepath.Join(t.TempDir(), "anchors.json")
		moment := time.Date(2024, time.March, 1, 9, 30, 0, 0, time.UTC)
		store, err := meditation.NewAnchorStore(anchors)
		if err != nil {
			t.Fatalf("open the anchor store: %v", err)
		}
		if err := store.Advance(ctx, moment); err != nil {
			t.Fatalf("record the reflection watermark: %v", err)
		}
		observer := &visibilityObserver{}
		run, tool := visibilityRun(t, ctx,
			tagent.WithReflectionAnchors(anchors),
			tagent.WithRecorder(visibilityRecorder{}),
			tagent.WithTelemetry(observer))
		visibilityInvoke(t, ctx, run, "inv-visible", "session-visible")
		if tool.calls() != 1 {
			t.Fatalf("tool executions = %d, want the admitted call executed exactly once", tool.calls())
		}
		filter := query.ListFilter{Scope: visibilityScope("session-visible")}
		opened, err := run.Diagnostics(ctx, filter, tagent.DiagnosticSwitches{Reflection: true, Training: true, Telemetry: true})
		if err != nil {
			t.Fatalf("read the open diagnostic faces: %v", err)
		}
		if len(opened.Query.Details) == 0 {
			t.Fatalf("unified reading = %d details, want the live scene of the run", len(opened.Query.Details))
		}
		direct, err := run.Query(ctx, filter)
		if err != nil {
			t.Fatalf("query the live scene: %v", err)
		}
		if got, want := kindCounts(opened.Query), kindCounts(direct); !reflect.DeepEqual(got, want) {
			t.Fatalf("diagnostic reading = %v, want the same derived counts as the query reading %v", got, want)
		}
		if !opened.Reflection.Face.Open || !opened.Reflection.Face.Available {
			t.Fatalf("reflection face = %+v, want the switch to open the wired anchor store", opened.Reflection.Face)
		}
		if generation := opened.Reflection.Publication.Generation; generation != 1 {
			t.Fatalf("published generation = %d, want the first generation the assembly published", generation)
		}
		watermark := opened.Reflection.Watermark
		if !watermark.Wired || !watermark.Established || !watermark.Moment.Equal(moment) {
			t.Fatalf("reflection watermark = %+v, want the persisted moment %s read from the anchor store", watermark, moment)
		}
		if !opened.Training.Face.Open || !opened.Training.Recorder {
			t.Fatalf("training face = %+v, want the switch to open the wired recorder", opened.Training.Face)
		}
		if opened.Training.Snapshots != 2 {
			t.Fatalf("request snapshots = %d, want one per model decision of the recorded turn", opened.Training.Snapshots)
		}
		if len(opened.Training.Losses) == 0 {
			t.Fatal("recording losses = none, want the attempt writes the recorder refused counted in the account")
		}
		for _, loss := range opened.Training.Losses {
			if loss.Kind != "attempt" || loss.Reason == "" {
				t.Fatalf("recording loss = %+v, want the refused attempt write beside its named reason", loss)
			}
		}
		deadline := time.Now().Add(10 * time.Second)
		for len(run.Presentations()) == 0 && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		opened, err = run.Diagnostics(ctx, filter, tagent.DiagnosticSwitches{Reflection: true, Training: true, Telemetry: true})
		if err != nil {
			t.Fatalf("re-read the open diagnostic faces: %v", err)
		}
		if !opened.Telemetry.Face.Open || !opened.Telemetry.Observer || opened.Telemetry.Presentations == 0 {
			t.Fatalf("telemetry face = %+v, want the switch to open the wired observation face", opened.Telemetry)
		}
		if observer.observed() == 0 {
			t.Fatal("observed facts = 0, want the post-commit face to have taken the committed facts it reports")
		}
		if opened.Telemetry.Gaps != 0 {
			t.Fatalf("observation gaps = %d, want none beside an observer that accepted every fact", opened.Telemetry.Gaps)
		}

		closed, err := run.Diagnostics(ctx, filter, tagent.DiagnosticSwitches{})
		if err != nil {
			t.Fatalf("read the closed diagnostic faces: %v", err)
		}
		for _, face := range []tagent.DiagnosticFace{closed.Reflection.Face, closed.Training.Face, closed.Telemetry.Face} {
			if face.Open || !face.Available || !strings.Contains(face.Reason, "switch") {
				t.Fatalf("closed face = %+v, want the wired subsystem reported closed by its switch", face)
			}
		}
		if len(closed.Query.Details) != len(opened.Query.Details) {
			t.Fatalf("unified reading = %d details beside the closed faces, want the %d the open reading carries",
				len(closed.Query.Details), len(opened.Query.Details))
		}
	})

	t.Run("OTel 关闭不影响 Query: the unified map reads on beside no observation face", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, _ := visibilityRun(t, ctx)
		visibilityInvoke(t, ctx, run, "inv-no-otel", "session-no-otel")
		report, err := run.Diagnostics(ctx, query.ListFilter{Scope: visibilityScope("session-no-otel")},
			tagent.DiagnosticSwitches{Reflection: true, Training: true, Telemetry: true})
		if err != nil {
			t.Fatalf("read the diagnostic faces: %v", err)
		}
		if report.Telemetry.Face.Open || report.Telemetry.Face.Available || !strings.Contains(report.Telemetry.Face.Reason, "observation") {
			t.Fatalf("telemetry face = %+v, want the absent observation face named instead of opened", report.Telemetry.Face)
		}
		if len(report.Query.Details) == 0 {
			t.Fatal("unified reading = no details, want the runtime map readable beside the closed observation face")
		}
		byKind := kindCounts(report.Query)
		for _, kind := range []query.ObjectKind{query.ObjectAgent, query.ObjectSession, query.ObjectTask, query.ObjectNotice} {
			if byKind[kind] == 0 {
				t.Fatalf("detail counts = %v, want the %s dimension read from its owner", byKind, kind)
			}
		}
		tasks, err := run.Query(ctx, visibilityTaskFilter(visibilityScope("session-no-otel")))
		if err != nil {
			t.Fatalf("query the task dimension: %v", err)
		}
		if len(tasks.Details) == 0 {
			t.Fatal("task details = none, want the filtered reading served beside the closed observation face")
		}
	})

	t.Run("诊断不复制状态: the reading follows the owners and keeps no copy", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, _ := visibilityRun(t, ctx)
		visibilityInvoke(t, ctx, run, "inv-copy-1", "session-copy")
		filter := query.ListFilter{Scope: visibilityScope("session-copy")}
		switches := tagent.DiagnosticSwitches{Training: true}
		first, err := run.Diagnostics(ctx, filter, switches)
		if err != nil {
			t.Fatalf("read the scene once: %v", err)
		}
		second, err := run.Diagnostics(ctx, filter, switches)
		if err != nil {
			t.Fatalf("read the unchanged scene twice: %v", err)
		}
		if !reflect.DeepEqual(first.Query.Details, second.Query.Details) {
			t.Fatalf("second reading = %d details, want the %d the unchanged scene carries",
				len(second.Query.Details), len(first.Query.Details))
		}
		if second.Query.Revision <= first.Query.Revision {
			t.Fatalf("revisions = %d then %d, want every read taken from the projection afresh",
				first.Query.Revision, second.Query.Revision)
		}
		if !reflect.DeepEqual(first.Training, second.Training) {
			t.Fatalf("training readings = %+v then %+v, want the same account beside no further write", first.Training, second.Training)
		}
		visibilityInvoke(t, ctx, run, "inv-copy-2", "session-copy")
		third, err := run.Diagnostics(ctx, filter, switches)
		if err != nil {
			t.Fatalf("read the grown scene: %v", err)
		}
		if len(third.Query.Details) <= len(first.Query.Details) {
			t.Fatalf("details = %d after the second turn, want more than the %d read before it",
				len(third.Query.Details), len(first.Query.Details))
		}
		if third.Training.Snapshots <= first.Training.Snapshots {
			t.Fatalf("request snapshots = %d after the second turn, want more than the %d read before it",
				third.Training.Snapshots, first.Training.Snapshots)
		}
	})

	t.Run("启动关闭与信号: the host serves its reading and closes through the drain", func(t *testing.T) {
		t.Setenv(tagent.DiagnosticSwitchEnv, "reflection,training")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, _ := visibilityRun(t, ctx, tagent.WithReflectionAnchors(filepath.Join(t.TempDir(), "anchors.json")), tagent.WithRecorder(visibilityRecorder{}))
		visibilityInvoke(t, ctx, run, "inv-host", "session-host")
		scope := visibilityScope("session-host")
		shell := cli.NewCLI(run, scope, cli.WithRequestCredential(entryCredential))
		base, stop := startVisibilityHost(t, ctx, shell)
		served := diagGet(t, base+"/diagnostics", "Bearer "+entryCredential)
		if served.status != http.StatusOK {
			t.Fatalf("diagnostic request status = %d (%s), want the reading behind the request gate", served.status, served.body)
		}
		var report tagent.DiagnosticReport
		if err := json.Unmarshal([]byte(served.body), &report); err != nil {
			t.Fatalf("decode the diagnostic reading: %v", err)
		}
		if !report.Switches.Reflection || !report.Switches.Training || report.Switches.Telemetry {
			t.Fatalf("served switches = %+v, want the two the process environment names", report.Switches)
		}
		if !report.Reflection.Face.Open || !report.Training.Face.Open || report.Telemetry.Face.Open {
			t.Fatalf("served faces = %+v / %+v / %+v, want the two the environment opened",
				report.Reflection.Face, report.Training.Face, report.Telemetry.Face)
		}
		if len(report.Query.Details) == 0 {
			t.Fatal("served details = none, want the live scene behind the request gate")
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatalf("send the termination signal: %v", err)
		}
		stop()
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-after-close",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-host"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		}); !errors.Is(err, agent.ErrShutdownRefused) {
			t.Fatalf("invoke after the close = %v, want the named refusal of the closed gate", err)
		}
		closed, err := run.Diagnostics(context.Background(), query.ListFilter{Scope: scope}, tagent.DiagnosticSwitches{})
		if err != nil {
			t.Fatalf("read the closed host: %v", err)
		}
		if !closed.Lifecycle.Closing {
			t.Fatalf("lifecycle = %+v, want the close of the run reported", closed.Lifecycle)
		}
		if !closed.Lifecycle.Reconciliation.Wired {
			t.Fatalf("reconciliation = %+v, want the delegation ledger the drain lands terminals through", closed.Lifecycle.Reconciliation)
		}
	})

	t.Run("未知开关具名拒: an unknown switch refuses the start by name", func(t *testing.T) {
		t.Setenv(tagent.DiagnosticSwitchEnv, "reflection,sparkles")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run, _ := visibilityRun(t, ctx)
		shell := cli.NewCLI(run, visibilityScope("session-host"))
		var stdout, stderr bytes.Buffer
		if code := shell.Main(ctx, []string{"start", "-addr", "127.0.0.1:0"}, &stdout, &stderr); code != 2 {
			t.Fatalf("start exit code = %d, want the usage refusal", code)
		}
		if !strings.Contains(stderr.String(), "the diagnostic switch names no subsystem") {
			t.Fatalf("stderr = %q, want the unknown switch named", stderr.String())
		}
		if strings.Contains(stdout.String(), "serving") {
			t.Fatalf("stdout = %q, want no service started beside the refused switch", stdout.String())
		}
	})

	t.Run("重启连续: a restarted host restores the persisted scene and adds nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}
		anchors := filepath.Join(root, "anchors.json")
		first, _ := visibilityRun(t, ctx, tagent.WithStorage(cfg), tagent.WithReflectionAnchors(anchors))
		visibilityInvoke(t, ctx, first, "inv-durable", "session-durable")
		scope := visibilityScope("session-durable")
		admitted, err := first.Query(ctx, visibilityTaskFilter(scope))
		if err != nil {
			t.Fatalf("query the admitted task: %v", err)
		}
		if len(admitted.Details) != 1 {
			t.Fatalf("admitted tasks = %d, want the one call the turn admitted", len(admitted.Details))
		}
		taskID := admitted.Details[0].Ref.TaskID
		instance := admitted.Details[0].Ref.InstanceID
		if _, err := first.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(10 * time.Second)}); err != nil {
			t.Fatalf("close the first host: %v", err)
		}
		moment := time.Date(2024, time.April, 2, 8, 15, 0, 0, time.UTC)
		store, err := meditation.NewAnchorStore(anchors)
		if err != nil {
			t.Fatalf("reopen the anchor store: %v", err)
		}
		if err := store.Advance(ctx, moment); err != nil {
			t.Fatalf("record the consumed reflection turn: %v", err)
		}
		second, _ := visibilityRun(t, ctx, tagent.WithStorage(cfg), tagent.WithReflectionAnchors(anchors))
		report, err := second.Diagnostics(ctx, query.ListFilter{Scope: scope}, tagent.DiagnosticSwitches{Reflection: true})
		if err != nil {
			t.Fatalf("read the restarted host: %v", err)
		}
		if len(report.Lifecycle.Recovered) != 1 {
			t.Fatalf("recovery outcomes = %+v, want the one task the persisted chain covers", report.Lifecycle.Recovered)
		}
		outcome := report.Lifecycle.Recovered[0]
		if outcome.TaskID != taskID || !outcome.Recovered || outcome.Refusal != "" || outcome.Status != task.StatusSettled {
			t.Fatalf("recovery outcome = %+v, want %s rebuilt beside the settled status it stored", outcome, taskID)
		}
		if report.Instance == instance {
			t.Fatalf("instance identity = %q, want a fresh identity beside the one the persisted task keeps", report.Instance)
		}
		restored, err := second.Query(ctx, visibilityTaskFilter(scope))
		if err != nil {
			t.Fatalf("query the restored scene: %v", err)
		}
		if len(restored.Details) != 1 {
			t.Fatalf("restored tasks = %d, want the one persisted task and no invented record", len(restored.Details))
		}
		if got := restored.Details[0].Ref; got.TaskID != taskID || got.InstanceID != instance {
			t.Fatalf("restored task ref = %+v, want the admission material of %s / %s kept", got, taskID, instance)
		}
		watermark := report.Reflection.Watermark
		if !watermark.Wired || !watermark.Established || !watermark.Moment.Equal(moment) {
			t.Fatalf("reflection watermark = %+v, want the moment %s the anchor store kept across the restart", watermark, moment)
		}
	})
}

// startVisibilityHost runs the start command of one shell over its own listener
// and reports the base URL it serves on beside the stop that waits for the close.
func startVisibilityHost(t *testing.T, ctx context.Context, shell *cli.CLI) (string, func()) {
	t.Helper()
	stdout, stdoutWriter := io.Pipe()
	var stderr bytes.Buffer
	served := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			served <- scanner.Text()
		}
		close(served)
		io.Copy(io.Discard, stdout)
	}()
	done := make(chan int, 1)
	go func() { done <- shell.Main(ctx, []string{"start", "-addr", "127.0.0.1:0"}, stdoutWriter, &stderr) }()
	line, ok := <-served
	if !ok {
		t.Fatalf("the start command served no address: %s", stderr.String())
	}
	base := "http://" + strings.TrimPrefix(line, "tagent: serving on ")
	return base, func() {
		select {
		case code := <-done:
			if code != 0 {
				t.Fatalf("start exit code = %d (%s), want the closed host to report its drain", code, stderr.String())
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("the start command did not return after its context ended: %s", stderr.String())
		}
	}
}
