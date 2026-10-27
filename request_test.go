package tagent

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestRequestSnapshotsSingleSource pins the one home of the safe request snapshots: the capture fixes each request once and every read answers from it.
//   - the read face answers exactly the snapshots the capture holds, in fix order, each beside its decision
//   - the content read back is the captured snapshot's own content, byte for byte
//   - call arguments come from the same captured transcript; a call it never carried answers the empty object
//   - a second read answers the same snapshots, and a run beside no capture answers none
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestRequestSnapshotsSingleSource(t *testing.T) {
	t.Run("the read face answers the capture's own snapshots", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		capture := snapshotTestCapture(t)
		run := snapshotTestRun(t, ctx, WithCapture(capture))
		turn := snapshotTestInvoke(t, ctx, run)
		if len(turn.Decisions) != 2 {
			t.Fatalf("decisions = %d, want the tool call and the closing decision of one turn", len(turn.Decisions))
		}
		records := run.RequestSnapshots()
		refs := capture.SnapshotRefs()
		if len(records) != len(refs) {
			t.Fatalf("read face = %d snapshots, want the %d the capture holds", len(records), len(refs))
		}
		if len(records) != len(turn.Decisions) {
			t.Fatalf("read face = %d snapshots, want one per decision beside the %d decisions", len(records), len(turn.Decisions))
		}
		fixed := capture.Snapshots()
		if len(fixed) != len(records) {
			t.Fatalf("capture account = %d snapshots, want the %d the read face answers", len(fixed), len(records))
		}
		for i := range records {
			if records[i].Decision != turn.Decisions[i].DecisionID {
				t.Fatalf("snapshot %d decision = %s, want the decision whose request it was fixed for %s",
					i, records[i].Decision, turn.Decisions[i].DecisionID)
			}
			if fixed[i].Decision != records[i].Decision {
				t.Fatalf("capture snapshot %d decision = %s, want the %s the read face answers",
					i, fixed[i].Decision, records[i].Decision)
			}
			held, err := capture.SnapshotOf(refs[i])
			if err != nil {
				t.Fatalf("read capture snapshot %d back: %v", i, err)
			}
			want, err := rl.MarshalSnapshot(held)
			if err != nil {
				t.Fatalf("marshal capture snapshot %d: %v", i, err)
			}
			got, err := rl.MarshalSnapshot(records[i].Snapshot)
			if err != nil {
				t.Fatalf("marshal read-back snapshot %d: %v", i, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("read-back snapshot %d = %s, want the capture's own content %s", i, got, want)
			}
		}
		if again := run.RequestSnapshots(); len(again) != len(records) {
			t.Fatalf("second read = %d snapshots, want the %d the first read answered", len(again), len(records))
		}
	})

	t.Run("call arguments are answered from the captured transcript", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		capture := snapshotTestCapture(t)
		run := snapshotTestRun(t, ctx, WithCapture(capture))
		snapshotTestInvoke(t, ctx, run)
		const issued = `{"path":"greet"}`
		if got := run.callArgumentsOf("call-1"); got != issued {
			t.Fatalf("call arguments = %q, want the %q the request was issued with", got, issued)
		}
		if got := run.callArgumentsOf("call-absent"); got != "{}" {
			t.Fatalf("arguments of a call no snapshot carried = %q, want the empty argument object", got)
		}
		carried := ""
		for _, one := range capture.Snapshots() {
			for _, message := range one.Snapshot.Messages {
				for _, call := range message.ToolCalls {
					if call.ID == "call-1" {
						carried = string(call.Function.Arguments)
					}
				}
			}
		}
		if carried != issued {
			t.Fatalf("captured transcript arguments = %q, want the %q the read answered", carried, issued)
		}
	})

	t.Run("a run beside no capture answers an empty read face", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		run := snapshotTestRun(t, ctx)
		snapshotTestInvoke(t, ctx, run)
		if records := run.RequestSnapshots(); len(records) != 0 {
			t.Fatalf("read face = %d snapshots beside no capture, want none fixed", len(records))
		}
		if got := run.callArgumentsOf("call-1"); got != "{}" {
			t.Fatalf("call arguments beside no capture = %q, want the empty argument object", got)
		}
	})
}

// snapshotTestCapture builds the trajectory capture one assembled run fixes its
// request views through: a real file writer under a temporary directory and one
// bounded sink over it, so the read holds what a production capture would.
func snapshotTestCapture(t *testing.T) *rl.TrajectoryCapture {
	t.Helper()
	writer, err := rl.NewFileCaptureWriter(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("run-request-views", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCapture("run-request-views", sink, rl.SnapshotPolicy{})
	if err != nil {
		t.Fatalf("open the trajectory capture: %v", err)
	}
	return capture
}

// snapshotTestRun assembles the runtime over one scripted provider whose first
// decision calls a tool and whose second decision settles the turn, with the
// options the caller attaches: the extra option is how a test attaches the
// capture whose account the read face answers from, or leaves it beside none.
func snapshotTestRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	t.Helper()
	round := 0
	var mu sync.Mutex
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		mu.Lock()
		defer mu.Unlock()
		round++
		if round == 1 {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{{
						ID:   "call-1",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name:      "echo-a",
							Arguments: []byte(`{"path":"greet"}`),
						},
					}},
				}}},
				Done: true,
			}
		}
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
			Done:    true,
		}
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	natives := map[event.AgentID]NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&snapshotTestTool{name: "echo-a", output: "out-a"}}},
	}
	options := append([]Option{WithGovernance(snapshotTestGovernance{})}, opts...)
	run, err := Assemble(ctx, decl, natives, options...)
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// snapshotTestInvoke drives one invocation through the formal entry of the
// assembled run and returns the turn it served.
func snapshotTestInvoke(t *testing.T, ctx context.Context, run *Run) agent.Turn {
	t.Helper()
	turn, err := run.Invoke(ctx, InvokeRequest{
		InvocationID: "inv-request-views",
		Episode:      "ep-request-views",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-request-views"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "drive the tool"}},
	})
	if err != nil {
		t.Fatalf("invoke the assembled run: %v", err)
	}
	return turn
}

// snapshotTestModel is the scripted provider stand-in: it records the native
// request it was driven with and streams the scripted answer.
type snapshotTestModel struct {
	mu       sync.Mutex
	answer   func(*model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the native request and streams the scripted answer.
func (m *snapshotTestModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted provider's descriptor.
func (m *snapshotTestModel) Info() model.Info {
	return model.Info{Name: "model-a"}
}

// snapshotTestTool is the native tool stand-in: it executes once per call and
// answers with its declared output.
type snapshotTestTool struct {
	name   string
	output string
}

// Declaration returns the declaration naming the stand-in tool.
func (t *snapshotTestTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the stand-in tool once and reports its declared output.
func (t *snapshotTestTool) Call(context.Context, []byte) (any, error) {
	return t.output, nil
}

// snapshotTestGovernance is the passing governance stand-in: the publication
// checks of this assembly answer pass at every required name, so the test reads
// the snapshot account rather than a governance ruling.
type snapshotTestGovernance struct{}

// Check answers every required check of one candidate plan with a pass.
func (snapshotTestGovernance) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       {Name: "eval", Verdict: state.CheckPass},
		"benchmark":  {Name: "benchmark", Verdict: state.CheckPass},
		"apply-gate": {Name: "apply-gate", Verdict: state.CheckPass},
	}, nil
}
