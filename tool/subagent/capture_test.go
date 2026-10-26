// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestWorkerCallsEnterTrajectory pins the delegated worker's model calls in the run's trajectory:
// - a builder carrying the run's capture fixes one request view per model call the delegated turn served
// - every response the worker model served is admitted as one attempt beside the call that produced it
// - the turn seals the capture, so the attempts land in the record file and the writer-confirmed manifest reads back complete
// - the attempt and snapshot accounts reconcile with the calls the worker model actually served
// - both provider shapes enter the same account: an iterating model and a channel-only model
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestWorkerCallsEnterTrajectory(t *testing.T) {
	t.Run("an iterating worker model", func(t *testing.T) {
		requireWorkerCallsEnterTrajectory(t, &scriptedWorker{name: "model-worker"})
	})
	t.Run("a channel-only worker model", func(t *testing.T) {
		requireWorkerCallsEnterTrajectory(t, &plainWorker{script: &scriptedWorker{name: "model-worker"}})
	})
}

// requireWorkerCallsEnterTrajectory runs one delegated turn over the given worker model and
// reconciles the captured account with the calls that model actually served.
func requireWorkerCallsEnterTrajectory(t *testing.T, provider workerProvider) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	capture := newWorkerCapture(t, dir, "cap-worker")
	delegation := &stubDelegation{receipt: task.AdmissionReceipt{
		CallID:   "call-worker",
		TaskID:   "task-worker",
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}}
	tool := newWorkerTool(t, delegation, provider, capture)

	out := callTool(t, tool, map[string]any{"agents_file": writeWorkerAgentsFile(t)})
	if _, ok := out.(task.AdmissionReceipt); !ok {
		t.Fatalf("call result = %#v, want the receipt of the delegated call", out)
	}
	if status := awaitWorkerTerminal(ctx, t, delegation); status != task.StatusSettled {
		t.Fatalf("terminal = %v, want the settled terminal of the captured turn", status)
	}
	if got := provider.served(); got != 2 {
		t.Fatalf("worker model calls = %d, want the two calls of one delegated turn", got)
	}

	attempts := readAttemptRecords(t, dir)
	if len(attempts) != 2 {
		t.Fatalf("attempt records = %d, want one per model call the worker served", len(attempts))
	}
	decisions := map[event.DecisionID]bool{}
	for _, attempt := range attempts {
		if attempt.ID == "" || attempt.DecisionID == "" {
			t.Fatalf("attempt = %+v, want the identities the capture records under", attempt)
		}
		if decisions[attempt.DecisionID] {
			t.Fatalf("decision %q holds two attempts, want one decision per model call", attempt.DecisionID)
		}
		decisions[attempt.DecisionID] = true
		if attempt.Terminal != model.KindCompleted {
			t.Fatalf("attempt terminal = %v, want the terminal the served response produced", attempt.Terminal)
		}
		if len(attempt.Response.Choices) == 0 {
			t.Fatalf("attempt response = %+v, want the served response kept verbatim", attempt.Response)
		}
	}

	manifest, err := rl.ReadCaptureSeal(dir)
	if err != nil {
		t.Fatalf("read the capture seal: %v", err)
	}
	if !manifest.Sealed || manifest.Partial {
		t.Fatalf("manifest = %+v, want the writer-confirmed seal of a complete capture", manifest)
	}
	if manifest.CaptureID != "cap-worker" || manifest.Records != 2 || manifest.Dropped != 0 {
		t.Fatalf("manifest = %+v, want the capture id, the two records and no loss", manifest)
	}

	refs := capture.SnapshotRefs()
	if len(refs) != 2 {
		t.Fatalf("fixed snapshots = %d, want one request view per model call", len(refs))
	}
	for _, ref := range refs {
		snapshot, err := capture.SnapshotOf(ref)
		if err != nil {
			t.Fatalf("read the fixed snapshot back: %v", err)
		}
		if snapshot.Model != "model-worker" {
			t.Fatalf("snapshot model = %q, want the worker model the call was declared against", snapshot.Model)
		}
		if !carriesTool(snapshot.VisibleTools, workerExecToolName) {
			t.Fatalf("snapshot visible tools = %+v, want the tool surface the worker saw", snapshot.VisibleTools)
		}
	}
	t.Logf("reconciled worker calls: served=%d attempts=%d snapshots=%d manifest.records=%d manifest.dropped=%d",
		provider.served(), len(attempts), len(refs), manifest.Records, manifest.Dropped)
}

// TestNoCaptureZeroRecords pins the uncaptured worker path:
// - a builder beside no capture runs the delegated turn as it stands and reports its settled terminal
// - the capture present beside the run fixes no snapshot, admits no record and leaves no seal
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestNoCaptureZeroRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	capture := newWorkerCapture(t, dir, "cap-idle")
	delegation := &stubDelegation{receipt: task.AdmissionReceipt{
		CallID:   "call-worker",
		TaskID:   "task-worker",
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}}
	provider := &scriptedWorker{name: "model-worker"}
	tool := newWorkerTool(t, delegation, provider, nil)

	if out := callTool(t, tool, map[string]any{"agents_file": writeWorkerAgentsFile(t)}); out == nil {
		t.Fatal("call returned no receipt beside no capture")
	}
	if status := awaitWorkerTerminal(ctx, t, delegation); status != task.StatusSettled {
		t.Fatalf("terminal = %v, want the settled terminal beside no capture", status)
	}
	if got := provider.served(); got != 2 {
		t.Fatalf("worker model calls = %d, want the two calls of one delegated turn", got)
	}
	if _, err := os.Stat(filepath.Join(dir, rl.SinkRecordsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record file error = %v, want no record file beside no capture", err)
	}
	if refs := capture.SnapshotRefs(); len(refs) != 0 {
		t.Fatalf("fixed snapshots = %d, want none beside no capture", len(refs))
	}
	manifest, err := rl.ReadCaptureSeal(dir)
	if err != nil {
		t.Fatalf("read the capture seal: %v", err)
	}
	if manifest.Sealed {
		t.Fatalf("manifest = %+v, want no seal beside no capture", manifest)
	}
}

// newWorkerCapture returns the trajectory capture of one test directory.
func newWorkerCapture(t *testing.T, dir, id string) *rl.TrajectoryCapture {
	t.Helper()
	writer, err := rl.NewFileCaptureWriter(dir, 0)
	if err != nil {
		t.Fatalf("build the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink(id, rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("build the capture sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCapture(id, sink, rl.SnapshotPolicy{})
	if err != nil {
		t.Fatalf("build the trajectory capture: %v", err)
	}
	return capture
}

// newWorkerTool returns the call_subagent tool over one delegation stand-in, one scripted
// worker model and the capture the delegated turn records through.
func newWorkerTool(t *testing.T, delegation *stubDelegation, provider workerProvider, capture rl.CaptureScope) *Tool {
	t.Helper()
	bindings := state.NewBindings()
	holdBinding(t, bindings, event.Address{AgentID: "agent-caller", SessionID: "session-caller"})
	tool, err := NewTool(ToolOptions{
		Parent: &Assembly{Definition: Definition{Description: "the caller", Model: "model-caller"}},
		Builder: Builder{
			Model: func(context.Context, string) (trpcmodel.Model, error) { return provider, nil },
			Tools: func(context.Context, []string) ([]trpctool.Tool, error) {
				return []trpctool.Tool{workerExecTool{}}, nil
			},
			Capture: capture,
		},
		Delegation: delegation,
		Bindings:   bindings,
	})
	if err != nil {
		t.Fatalf("construct the call_subagent tool: %v", err)
	}
	return tool
}

// awaitWorkerTerminal waits until the delegation stand-in reported the terminal of the
// delegated turn and reports that terminal.
func awaitWorkerTerminal(ctx context.Context, t *testing.T, delegation *stubDelegation) task.Status {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if delegation.completions() > 0 {
			return delegation.lastTerminal()
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the delegated turn reported no terminal: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

// lastTerminal reports the last terminal the stand-in was asked to report.
func (s *stubDelegation) lastTerminal() task.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.terminal) == 0 {
		return task.StatusUnknown
	}
	return s.terminal[len(s.terminal)-1]
}

// readAttemptRecords reads the sealed attempt records of one capture directory back.
func readAttemptRecords(t *testing.T, dir string) []rl.AttemptRecord {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, rl.SinkRecordsFile))
	if err != nil {
		t.Fatalf("read the record file: %v", err)
	}
	var attempts []rl.AttemptRecord
	for _, raw := range bytes.Split(content, []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line, err := rl.ParseLine(raw)
		if err != nil {
			t.Fatalf("parse stored record %q: %v", raw, err)
		}
		if line.Kind == rl.RecordKindAttempt {
			attempts = append(attempts, *line.Attempt)
		}
	}
	return attempts
}

// carriesTool reports whether one fixed view lists the named tool.
func carriesTool(names []rl.VersionedName, want string) bool {
	for _, name := range names {
		if name.Name == want {
			return true
		}
	}
	return false
}

// writeWorkerAgentsFile writes one agents file declaring the worker the test delegates to.
func writeWorkerAgentsFile(t *testing.T) string {
	t.Helper()
	return writeAgentsFile(t, t.TempDir(), "worker.md", `---
name: worker
description: runs one delegated turn
model: model-worker
tools: [exec_run]
---
Run the delegated turn and report its result.
`)
}

// workerExecToolName is the tool the scripted worker calls before it answers.
const workerExecToolName = "exec_run"

// workerExecTool is the tool the scripted worker calls: it answers one recorded result, so
// the framework runs a second model call of the same delegated turn.
type workerExecTool struct{}

var _ trpctool.CallableTool = workerExecTool{}

// Declaration reports the tool the delegated turn exposes.
func (workerExecTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        workerExecToolName,
		Description: "runs one command",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{"command": {Type: "string"}},
		},
	}
}

// Call answers the scripted tool result of one call.
func (workerExecTool) Call(context.Context, []byte) (any, error) {
	return "executed", nil
}

// workerProvider is one worker model of the capture test: the served count reconciles the
// captured attempts with the calls the model actually answered.
type workerProvider interface {
	trpcmodel.Model
	served() int
}

// scriptedWorker is the worker model of the capture test: its first call answers one tool
// call and every later call answers the final text, so one delegated turn serves two model
// calls.
type scriptedWorker struct {
	name string

	mu    sync.Mutex
	calls int
}

var (
	_ workerProvider      = (*scriptedWorker)(nil)
	_ trpcmodel.IterModel = (*scriptedWorker)(nil)
	_ workerProvider      = (*plainWorker)(nil)
)

// plainWorker is the worker model that answers through a response channel alone, so the
// capture wrapper bridges the stream the provider served.
type plainWorker struct {
	script *scriptedWorker
}

// Info reports the model descriptor.
func (m *plainWorker) Info() trpcmodel.Info { return m.script.Info() }

// GenerateContent answers one worker request with the single response of its scripted step.
func (m *plainWorker) GenerateContent(ctx context.Context, request *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	return m.script.GenerateContent(ctx, request)
}

// served reports the calls the worker model answered.
func (m *plainWorker) served() int { return m.script.served() }

// Info reports the model descriptor.
func (m *scriptedWorker) Info() trpcmodel.Info { return trpcmodel.Info{Name: m.name} }

// served reports the calls the worker model answered.
func (m *scriptedWorker) served() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// GenerateContent answers one worker request with the single response of its scripted step.
func (m *scriptedWorker) GenerateContent(context.Context, *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	ch := make(chan *trpcmodel.Response, 1)
	ch <- m.next()
	close(ch)
	return ch, nil
}

// GenerateContentIter answers one worker request in the caller goroutine, so the capture
// wrapper serves the iterator path the framework drives.
func (m *scriptedWorker) GenerateContentIter(context.Context, *trpcmodel.Request) (trpcmodel.Seq[*trpcmodel.Response], error) {
	return func(yield func(*trpcmodel.Response) bool) {
		yield(m.next())
	}, nil
}

// next reports the response of one scripted step: the first call asks for the declared
// tool and every later call answers the final text.
func (m *scriptedWorker) next() *trpcmodel.Response {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.calls == 1 {
		return &trpcmodel.Response{
			Done: true,
			Choices: []trpcmodel.Choice{{Message: trpcmodel.Message{
				Role: trpcmodel.RoleAssistant,
				ToolCalls: []trpcmodel.ToolCall{{
					ID:   "call-exec-1",
					Type: "function",
					Function: trpcmodel.FunctionDefinitionParam{
						Name:      workerExecToolName,
						Arguments: []byte(`{"command":"true"}`),
					},
				}},
			}}},
		}
	}
	return &trpcmodel.Response{
		Done:    true,
		Choices: []trpcmodel.Choice{{Message: trpcmodel.NewAssistantMessage("worker-answer")}},
	}
}
