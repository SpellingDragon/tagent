package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestPluginPhaseOrdering drives the loop and task wrapper with a plugin pipeline on the borrowed binding and checks the phase ordering of the execution pass.
// - the Before pass runs exactly once per tool call and executes the transformed final call: the core re-checks that call and the same final call is recorded
// - a denied call executes nothing and produces only its named paired result, with no task registered
// - the After pass never runs before the canonical commit: not on a refused call and not on a call whose commit failed
// - a failed observation is a visible named gap and never rolls the committed fact back
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestPluginPhaseOrdering(t *testing.T) {
	t.Run("the Before pass runs once per call and executes the transformed final call", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-1", "echo-raw", `{"x":1}`))
			}
			return convFinalTurn("settled")
		}}
		log := &phaseLog{}
		var sawTransformName, sawTransformArgs, sawRiskName, sawRiskArgs string
		renamer := &phaseFace{name: "renamer", port: plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductCall}, log: log,
			transform: func(call *model.ToolCall) error {
				sawTransformName = call.Function.Name
				sawTransformArgs = string(call.Function.Arguments)
				call.Function.Name = "echo-final"
				call.Function.Arguments = []byte(`{"x":2}`)
				return nil
			}}
		grader := &phaseFace{name: "grader", port: plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductRuling}, log: log,
			risk: func(call *model.ToolCall) plugin.RiskLevel {
				sawRiskName = call.Function.Name
				sawRiskArgs = string(call.Function.Arguments)
				return plugin.RiskMedium
			}}
		raw := &phaseTool{name: "echo-raw", output: "raw-out", log: log}
		cooked := &phaseTool{name: "echo-final", output: "final-out", log: log}
		rig := newPhaseRig(t, provider, []*phaseTool{raw, cooked}, []plugin.Instance{renamer, grader}, nil, log)

		turn := rig.drive(t, ctx, "run")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := log.count("transform:renamer"); got != 1 {
			t.Fatalf("transform entries = %d, want the Before transform run exactly once per call", got)
		}
		if got := log.count("risk:grader"); got != 1 {
			t.Fatalf("risk entries = %d, want the Before risk face run exactly once per call", got)
		}
		if !(log.indexOf("transform:renamer") < log.indexOf("risk:grader") &&
			log.indexOf("risk:grader") < log.indexOf("exec:echo-final") &&
			log.indexOf("exec:echo-final") < log.indexOf("commit:call-1")) {
			t.Fatalf("phase order = %v, want transform before risk before execution before commit", log.snapshot())
		}
		if sawTransformName != "echo-raw" || sawTransformArgs != `{"x":1}` {
			t.Fatalf("transform saw (%q, %q), want the native call the model recorded", sawTransformName, sawTransformArgs)
		}
		if sawRiskName != "echo-final" || sawRiskArgs != `{"x":2}` {
			t.Fatalf("risk saw (%q, %q), want the transformed final call the pass produced", sawRiskName, sawRiskArgs)
		}
		if got := log.count("exec:echo-raw"); got != 0 || raw.calls() != 0 {
			t.Fatalf("raw tool executions = (%d, %d), want the untransformed call never executed", got, raw.calls())
		}
		if cooked.calls() != 1 || cooked.args() != `{"x":2}` {
			t.Fatalf("final tool executions = (%d, %q), want the transformed arguments executed exactly once", cooked.calls(), cooked.args())
		}
		approvals := rig.approvals()
		if len(approvals) != 1 || approvals[0].tool != "echo-final" || approvals[0].args != `{"x":2}` {
			t.Fatalf("approvals = %+v, want the core re-check of the transformed final call", approvals)
		}
		results := rig.results()
		if len(results) != 1 || results[0].Tool != "echo-final" || results[0].CallID != "call-1" {
			t.Fatalf("recorded results = %+v, want the transformed final call recorded under its native CallID", results)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want one request per decision", len(requests))
		}
		replay := requests[1].Messages
		if len(replay) != 4 {
			t.Fatalf("replay messages = %d (%+v), want user, assistant, one admission receipt and one settled observation", len(replay), replay)
		}
		if assistant := replay[1]; assistant.Role != model.RoleAssistant || len(assistant.ToolCalls) != 1 ||
			assistant.ToolCalls[0].ID != "call-1" || assistant.ToolCalls[0].Function.Name != "echo-raw" ||
			string(assistant.ToolCalls[0].Function.Arguments) != `{"x":1}` {
			t.Fatalf("replay assistant = %+v, want the native call the model recorded kept byte-identical", assistant)
		}
		if receipt := replay[2]; receipt.Role != model.RoleTool || receipt.ToolID != "call-1" || receipt.ToolName != "echo-final" ||
			receipt.Content != "ok" ||
			strings.Contains(receipt.Content, "final-out") || strings.Contains(receipt.Content, "the call was answered with") {
			t.Fatalf("paired tool message = %+v, want the admission receipt of call-1 paired under its native CallID", receipt)
		}
		if obs := replay[3]; obs.Content != "final-out" || obs.ToolID != "call-1" || obs.ToolName != "echo-final" || obs.Role == model.RoleTool {
			t.Fatalf("settled observation = %+v, want the recorded final call settled under its native CallID as its own observation", obs)
		}
	})

	t.Run("a denied call executes nothing and produces only its named paired result", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-2", "echo-final", `{"x":1}`))
			}
			return convFinalTurn("settled")
		}}
		log := &phaseLog{}
		observer := &phaseFace{name: "observer", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}, log: log}
		presenter := &phaseFace{name: "presenter", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductView}, log: log}
		cooked := &phaseTool{name: "echo-final", output: "never", log: log}
		deny := task.Approver(func(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
			return task.Ruling{Kind: task.RulingDeny, Reason: "risk rule"}, nil
		})
		rig := newPhaseRig(t, provider, []*phaseTool{cooked}, []plugin.Instance{observer, presenter}, deny, log)

		turn := rig.drive(t, ctx, "run")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the refusal paired and the turn closed completed", turn.Terminal, len(turn.Decisions))
		}
		if cooked.calls() != 0 || log.count("exec:echo-final") != 0 {
			t.Fatalf("tool executions = (%d log entries), want a denied call executing nothing", log.count("exec:echo-final"))
		}
		results := rig.results()
		if len(results) != 1 {
			t.Fatalf("recorded results = %+v, want exactly the one named paired result of the refusal", results)
		}
		paired := results[0]
		if paired.Denied == nil || paired.Denied.Tool != "echo-final" || paired.Denied.Reason != "risk rule" ||
			paired.CallID != "call-2" || paired.Tool != "echo-final" {
			t.Fatalf("paired result = %+v, want the named refusal of echo-final paired with call-2", paired)
		}
		if paired.TaskID != "" {
			t.Fatalf("paired result task = %q, want a refusal that registers no task", paired.TaskID)
		}
		identity := task.CallIdentity{
			InstanceID:   rig.instance,
			InvocationID: rig.invocationID,
			DecisionID:   turn.Decisions[0].DecisionID,
			CallID:       "call-2",
		}
		if _, known := rig.admission.Lookup(identity.DedupKey()); known {
			t.Fatal("the denied call must register no task and withdraw its pre-registration")
		}
		if log.count("observe:observer") != 0 || log.count("present:presenter") != 0 {
			t.Fatalf("after pass entries = %v, want no observation or presentation beside a refusal", log.snapshot())
		}
		replay := provider.recorded()[1].Messages
		pair := replay[len(replay)-1]
		if pair.ToolID != "call-2" || pair.ToolName != "echo-final" || !strings.Contains(pair.Content, "risk rule") {
			t.Fatalf("paired tool message = %+v, want the named refusal answering call-2", pair)
		}
	})

	t.Run("the After pass runs only after the canonical commit", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		t.Run("the committed fact is observed and presented after its commit", func(t *testing.T) {
			provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
				if len(req.Messages) == 1 {
					return convToolTurn(convCall("call-3", "echo-final", `{"x":1}`))
				}
				return convFinalTurn("settled")
			}}
			log := &phaseLog{}
			var rig *phaseRig
			var observed event.EventID
			var observedCommitted bool
			observer := &phaseFace{name: "observer", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}, log: log,
				observe: func(fact event.EventID) error {
					observed = fact
					_, observedCommitted = rig.facts.resolve(fact)
					return nil
				}}
			presenter := &phaseFace{name: "presenter", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductView}, log: log}
			cooked := &phaseTool{name: "echo-final", output: "final-out", log: log}
			rig = newPhaseRig(t, provider, []*phaseTool{cooked}, []plugin.Instance{observer, presenter}, nil, log)

			turn := rig.drive(t, ctx, "run")
			if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
				t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
			}
			if !(log.indexOf("commit:call-3") < log.indexOf("observe:observer")) ||
				!(log.indexOf("commit:call-3") < log.indexOf("present:presenter")) {
				t.Fatalf("phase order = %v, want the observation and the presentation after the commit", log.snapshot())
			}
			if log.count("observe:observer") != 1 || log.count("present:presenter") != 1 {
				t.Fatalf("after pass entries = %v, want one observation and one presentation of the committed fact", log.snapshot())
			}
			if observed != "fact-call-3" || !observedCommitted {
				t.Fatalf("observation saw (%q, committed=%v), want the committed fact already in the table when the After pass ran", observed, observedCommitted)
			}
		})

		t.Run("a refused call presents nothing", func(t *testing.T) {
			provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
				if len(req.Messages) == 1 {
					return convToolTurn(convCall("call-3b", "echo-final", `{"x":1}`))
				}
				return convFinalTurn("settled")
			}}
			log := &phaseLog{}
			observer := &phaseFace{name: "observer", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}, log: log}
			presenter := &phaseFace{name: "presenter", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductView}, log: log}
			cooked := &phaseTool{name: "echo-final", output: "never", log: log}
			deny := task.Approver(func(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
				return task.Ruling{Kind: task.RulingDeny, Reason: "risk rule"}, nil
			})
			rig := newPhaseRig(t, provider, []*phaseTool{cooked}, []plugin.Instance{observer, presenter}, deny, log)

			turn := rig.drive(t, ctx, "run")
			if turn.Terminal != model.KindCompleted {
				t.Fatalf("turn terminal = %v, want the refusal closing the turn completed", turn.Terminal)
			}
			if log.count("observe:observer") != 0 || log.count("present:presenter") != 0 {
				t.Fatalf("after pass entries = %v, want no After pass beside a refused call", log.snapshot())
			}
		})

		t.Run("a failed commit presents nothing", func(t *testing.T) {
			provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
				if len(req.Messages) == 1 {
					return convToolTurn(convCall("call-3c", "echo-final", `{"x":1}`))
				}
				return convFinalTurn("settled")
			}}
			log := &phaseLog{}
			observer := &phaseFace{name: "observer", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}, log: log}
			presenter := &phaseFace{name: "presenter", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductView}, log: log}
			cooked := &phaseTool{name: "echo-final", output: "final-out", log: log}
			rig := newPhaseRig(t, provider, []*phaseTool{cooked}, []plugin.Instance{observer, presenter}, nil, log)
			rig.failRecord(errors.New("the recorder is down"))

			turn := rig.drive(t, ctx, "run")
			if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
				t.Fatalf("turn = (%v, %d decisions), want the unsettled fact still closing the turn completed", turn.Terminal, len(turn.Decisions))
			}
			if log.count("exec:echo-final") != 1 {
				t.Fatalf("execution entries = %d, want the tool run once beside the failed commit", log.count("exec:echo-final"))
			}
			if log.count("commit:call-3c") != 0 || log.count("observe:observer") != 0 || log.count("present:presenter") != 0 {
				t.Fatalf("phase entries = %v, want no commit and no After pass when the commit failed", log.snapshot())
			}
			if got := rig.results(); len(got) != 0 {
				t.Fatalf("recorded results = %+v, want no fact recorded beside the failed commit", got)
			}
		})
	})

	t.Run("a failed observation is a visible gap and never rolls the fact back", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-4", "echo-final", `{"x":1}`))
			}
			return convFinalTurn("settled")
		}}
		log := &phaseLog{}
		bad := &phaseFace{name: "bad-observer", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}, log: log,
			observe: func(event.EventID) error {
				return errors.New("the observer is down")
			}}
		presenter := &phaseFace{name: "presenter", port: plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductView}, log: log,
			present: func(fact event.EventID) (string, bool) {
				return "view:" + string(fact), false
			}}
		cooked := &phaseTool{name: "echo-final", output: "kept", log: log}
		rig := newPhaseRig(t, provider, []*phaseTool{cooked}, []plugin.Instance{bad, presenter}, nil, log)

		turn := rig.drive(t, ctx, "run")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the failed observation never breaking the turn", turn.Terminal, len(turn.Decisions))
		}
		if log.count("observe:bad-observer") != 1 || log.count("present:presenter") != 1 {
			t.Fatalf("after pass entries = %v, want the failed observation recorded beside the presentation that followed", log.snapshot())
		}
		msg, ok := rig.facts.resolve("fact-call-4")
		if !ok || msg.Content != "kept" {
			t.Fatalf("committed fact = (%+v, %v), want the committed fact kept as it stands beside the failed observation", msg, ok)
		}
		replay := provider.recorded()[1].Messages
		if obs := replay[len(replay)-1]; obs.Content != "kept" || obs.ToolID != "call-4" || obs.ToolName != "echo-final" || obs.Role == model.RoleTool {
			t.Fatalf("settled observation = %+v, want the committed fact kept as its own observation in the transcript", obs)
		}

		shown := rig.presentations()
		if len(shown) != 1 {
			t.Fatalf("presentations = %+v, want the one After outcome the drive handed to the sink", shown)
		}
		presentation := shown[0]
		if presentation.Fact != "fact-call-4" || len(presentation.Gaps) != 1 || len(presentation.Views) != 1 {
			t.Fatalf("presentation = %+v, want the committed fact beside one visible observation gap and one rendered view", presentation)
		}
		gap := presentation.Gaps[0]
		if gap.Plugin != "bad-observer" || gap.Fact != "fact-call-4" || gap.Err == nil {
			t.Fatalf("gap = %+v, want the failed observation named beside its fact and error", gap)
		}
		view := presentation.Views[0]
		if view.Plugin != "presenter" || view.Text != "view:fact-call-4" || view.Dropped {
			t.Fatalf("view = %+v, want the rendered view kept beside the gap and not dropped", view)
		}
		if _, ok := rig.facts.resolve("fact-call-4"); !ok {
			t.Fatal("the visible gap must never roll the committed fact back")
		}
	})
}

// phaseRig is one runtime assembly of the plugin phase ordering: the persistent
// event loop over a held binding generation carrying the named plugins, the
// registration wrapper of each native tool over the memory manager, and the
// shared faces the assertions read afterwards.
type phaseRig struct {
	loop         *agent.Loop
	admission    *task.MemoryAdmission
	log          *phaseLog
	facts        *phaseFacts
	instance     event.InstanceID
	session      event.SessionID
	invocationID event.InvocationID

	mu        sync.Mutex
	recorded  []task.Result
	shown     []approvalSeen
	after     []view.Presentation
	recordErr error
	approve   task.Approver
}

// approvalSeen is one core re-check the wrapper's approval face was shown: the
// tool name and the argument bytes the call was admitted under.
type approvalSeen struct {
	tool string
	args string
}

// newPhaseRig assembles the real loop and task layer around one scripted model
// and its tools, with the named plugins published on the borrowed binding and
// every phase entry written into the shared log.
func newPhaseRig(t *testing.T, provider *convModel, tools []*phaseTool, faces []plugin.Instance, approve task.Approver, log *phaseLog) *phaseRig {
	t.Helper()
	rig := &phaseRig{
		admission:    task.NewMemoryAdmission(),
		log:          log,
		facts:        &phaseFacts{facts: map[event.EventID]model.Message{}},
		instance:     event.InstanceID("instance-phase"),
		session:      event.SessionID("session-phase"),
		invocationID: event.InvocationID("inv-phase"),
		approve:      approve,
	}
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), rig.admission)
	manager := task.NewMemoryManager(registry, nil)
	wrapped := make([]trpctool.Tool, 0, len(tools))
	for _, tool := range tools {
		wrapped = append(wrapped, task.Wrap(manager, tool,
			task.WithResultRecorder(rig.record),
			task.WithMailbox(rig.sink),
			task.WithApprover(rig.ruling)))
	}
	binding := state.AgentBinding{
		Model:        provider,
		Tools:        wrapped,
		Plugins:      faces,
		PromptSource: state.PromptSource{Version: "prompt-phase-v1"},
	}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: provider.name}},
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: decl}, map[event.AgentID]state.AgentBinding{"agent-a": binding}, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
	rig.loop = agent.NewLoop(bindings, state.NewOccupancy(), rig.instance, rig.facts.resolve)
	rig.loop.SetPresentations(rig.capture)
	return rig
}

// drive runs one invocation of the rig to its terminal.
func (r *phaseRig) drive(t *testing.T, ctx context.Context, input string) agent.Turn {
	t.Helper()
	turn, err := r.loop.Drive(ctx, agent.Invocation{
		InvocationID: r.invocationID,
		Address:      event.Address{AgentID: "agent-a", SessionID: r.session},
		Instance:     r.instance,
		Ready: []event.Event{
			{
				Key:       "ev-phase-input",
				Type:      "user_input",
				Timestamp: time.Now(),
				Message:   model.Message{Role: model.RoleUser, Content: input},
			},
		},
	})
	if err != nil {
		t.Fatalf("drive the invocation: %v", err)
	}
	return turn
}

// failRecord makes the next fact commit fail, so the call settles without a
// committed fact.
func (r *phaseRig) failRecord(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordErr = err
}

// record stores one result beside the fact identity it reports back and writes
// the commit into the shared phase log.
func (r *phaseRig) record(_ context.Context, result task.Result) (event.EventID, error) {
	r.mu.Lock()
	fail := r.recordErr
	r.mu.Unlock()
	if fail != nil {
		return "", fail
	}
	id := event.EventID(fmt.Sprintf("fact-%s", result.CallID))
	content := ""
	switch {
	case result.Denied != nil:
		content = "denied: " + result.Denied.Reason
	case result.Err != nil:
		content = result.Err.Error()
	default:
		content = fmt.Sprint(result.Output)
	}
	r.facts.put(id, model.Message{Role: model.RoleTool, ToolID: result.CallID, ToolName: result.Tool, Content: content})
	r.mu.Lock()
	r.recorded = append(r.recorded, result)
	r.mu.Unlock()
	r.log.add("commit:" + result.CallID)
	return id, nil
}

// sink routes one settled notification back into the intake of the invocation
// that issued the call.
func (r *phaseRig) sink(_ context.Context, notice task.Notice) error {
	r.loop.Backflow(r.invocationID, notice)
	return nil
}

// ruling records the core re-check the wrapper showed and answers with the
// ruling the sub-case armed, or with a run.
func (r *phaseRig) ruling(_ context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
	r.mu.Lock()
	r.shown = append(r.shown, approvalSeen{tool: tool, args: string(jsonArgs)})
	next := r.approve
	r.mu.Unlock()
	if next != nil {
		return next(context.Background(), tool, jsonArgs)
	}
	return task.Ruling{Kind: task.RulingRun}, nil
}

// results reports the results recorded so far.
func (r *phaseRig) results() []task.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]task.Result(nil), r.recorded...)
}

// approvals reports the core re-checks shown to the approval face so far.
func (r *phaseRig) approvals() []approvalSeen {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]approvalSeen(nil), r.shown...)
}

// capture keeps one After pass presentation the loop handed to the sink.
func (r *phaseRig) capture(presentation view.Presentation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.after = append(r.after, presentation)
}

// presentations reports the After pass outcomes the loop handed to the sink,
// as a copy the caller owns.
func (r *phaseRig) presentations() []view.Presentation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]view.Presentation, 0, len(r.after))
	for _, presentation := range r.after {
		got := presentation
		got.Views = append([]view.RenderedView(nil), presentation.Views...)
		got.Gaps = append([]view.ObservationGap(nil), presentation.Gaps...)
		out = append(out, got)
	}
	return out
}

// phaseLog is the ordered entry log of one rig: every pass face, tool execution
// and fact commit writes one named entry, so the assertions read the real order
// the phases ran in.
type phaseLog struct {
	mu      sync.Mutex
	entries []string
}

// add appends one named entry.
func (l *phaseLog) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
}

// snapshot reports the entries logged so far.
func (l *phaseLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// count reports how often the named entry was logged.
func (l *phaseLog) count(entry string) int {
	got := 0
	for _, one := range l.snapshot() {
		if one == entry {
			got++
		}
	}
	return got
}

// indexOf reports the position of the named entry, or -1 when it was not logged.
func (l *phaseLog) indexOf(entry string) int {
	for i, one := range l.snapshot() {
		if one == entry {
			return i
		}
	}
	return -1
}

// phaseFace is one test instance of the pipeline: a named element serving one
// port, whose transform, risk, observe and present closures the sub-case arms
// and whose every entry is written into the shared phase log.
type phaseFace struct {
	name string
	port plugin.Port
	log  *phaseLog

	transform func(call *model.ToolCall) error
	risk      func(call *model.ToolCall) plugin.RiskLevel
	observe   func(fact event.EventID) error
	present   func(fact event.EventID) (string, bool)
}

// Name returns the stable name of the test instance.
func (f *phaseFace) Name() string { return f.name }

// Port returns the single port the test instance serves.
func (f *phaseFace) Port() plugin.Port { return f.port }

// TransformCall logs the entry and runs the armed transform closure.
func (f *phaseFace) TransformCall(_ context.Context, call *model.ToolCall) error {
	f.log.add("transform:" + f.name)
	if f.transform != nil {
		return f.transform(call)
	}
	return nil
}

// AssessRisk logs the entry and grades through the armed risk closure.
func (f *phaseFace) AssessRisk(_ context.Context, call *model.ToolCall) plugin.RiskLevel {
	f.log.add("risk:" + f.name)
	if f.risk != nil {
		return f.risk(call)
	}
	return plugin.RiskLow
}

// ObserveCommit logs the entry and runs the armed observe closure.
func (f *phaseFace) ObserveCommit(_ context.Context, fact event.EventID) error {
	f.log.add("observe:" + f.name)
	if f.observe != nil {
		return f.observe(fact)
	}
	return nil
}

// Present logs the entry and renders through the armed present closure.
func (f *phaseFace) Present(_ context.Context, fact event.EventID) (string, bool) {
	f.log.add("present:" + f.name)
	if f.present != nil {
		return f.present(fact)
	}
	return "", false
}

// phaseTool is the native tool stand-in of the rig: it logs its execution into
// the shared phase log, counts its calls and reports the arguments it ran with.
type phaseTool struct {
	name   string
	output string
	log    *phaseLog

	mu       sync.Mutex
	count    int
	lastArgs string
}

// Declaration returns the declaration naming the fake tool.
func (t *phaseTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call logs its execution, counts the call and reports the output.
func (t *phaseTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	t.count++
	t.lastArgs = string(jsonArgs)
	t.mu.Unlock()
	t.log.add("exec:" + t.name)
	return t.output, nil
}

// calls reports how often the fake tool executed.
func (t *phaseTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// args reports the native arguments the fake tool last ran with.
func (t *phaseTool) args() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastArgs
}

// phaseFacts is the committed fact table of one rig: the native tool message
// each recorded fact carries.
type phaseFacts struct {
	mu    sync.Mutex
	facts map[event.EventID]model.Message
}

// put records one fact beside its native tool message.
func (f *phaseFacts) put(id event.EventID, msg model.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.facts[id] = msg
}

// resolve reports the native tool message one committed fact carries.
func (f *phaseFacts) resolve(id event.EventID) (model.Message, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	msg, ok := f.facts[id]
	return msg, ok
}
