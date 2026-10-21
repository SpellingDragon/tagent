package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// TestFinalRoundClosingSemantics drives one loop under a watchdog budget and pins what the spent budget leaves for the model:
// - a spent round budget turns the next decision into the closing round, which carries the closing instruction and ends the drive
// - a spent round budget of two lets exactly two tool rounds run and never a third beside the one closing round
// - a spent time budget turns the next decision into the closing round for the same reason of its own
// - the closing round runs under its own short timeout and never holds the drive past it
// - a drive under no budget runs no closing round and keeps ending on its own terminal
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestFinalRoundClosingSemantics(t *testing.T) {
	t.Run("a spent round budget turns the next decision into the closing round that ends the drive", func(t *testing.T) {
		script := newClosingRoundScript(
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-1", "echo-a")}},
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-2", "echo-a")}},
		)
		loop, history, marks := closingRoundLoop(t, script)
		loop.SetFinalRound(FinalRoundPolicy{MaxRounds: 1, Timeout: 5 * time.Second, Prompt: "close it out"})

		turn, err := loop.Drive(context.Background(), closingRoundInvocation())
		if err != nil {
			t.Fatalf("drive under a spent round budget = %v, want the closing round delivered", err)
		}
		if len(turn.Decisions) != 2 {
			t.Fatalf("decisions of the drive = %d, want the one tool round beside the closing round", len(turn.Decisions))
		}
		if turn.FinalRound != FinalRoundRounds {
			t.Fatalf("final round reason = %q, want %q beside the spent round budget", turn.FinalRound, FinalRoundRounds)
		}
		if got := marks.recorded(); len(got) != 2 || got[0] || !got[1] {
			t.Fatalf("closing-round marks of the request boundaries = %v, want the closing round marked and the tool round not", got)
		}
		closing := turn.Decisions[1].Request.Messages
		if last := closing[len(closing)-1]; last.Role != model.RoleUser || last.Content != "close it out" {
			t.Fatalf("last message of the closing request = %+v, want the closing instruction carried after the transcript", last)
		}
		ordinary := turn.Decisions[0].Request.Messages
		if invariantCarries(ordinary, "close it out") {
			t.Fatalf("tool round request carries the closing instruction before the budget was spent: %+v", ordinary)
		}
		committed := history.SessionHistory(closingRoundAddress)
		if answer := closingRoundAnswer(committed, "call-2"); answer != finalRoundRefusal {
			t.Fatalf("answer of the closing round's call = %q, want the refusal that admits nothing", answer)
		}
		if answer := closingRoundAnswer(committed, "call-1"); answer == finalRoundRefusal {
			t.Fatalf("answer of the tool round's call = %q, want the ordinary admission path of that round", answer)
		}
	})

	t.Run("a round budget of two lets two tool rounds run and never a third beside the one closing round", func(t *testing.T) {
		script := newClosingRoundScript(
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-1", "echo-a")}},
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-2", "echo-a")}},
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-3", "echo-a")}},
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-4", "echo-a")}},
		)
		loop, _, _ := closingRoundLoop(t, script)
		loop.SetFinalRound(FinalRoundPolicy{MaxRounds: 2, Timeout: 5 * time.Second})

		turn, err := loop.Drive(context.Background(), closingRoundInvocation())
		if err != nil {
			t.Fatalf("drive under a round budget of two = %v, want the closing round delivered", err)
		}
		if len(turn.Decisions) != 3 {
			t.Fatalf("decisions of the drive = %d, want two tool rounds beside exactly one closing round", len(turn.Decisions))
		}
		if turn.FinalRound != FinalRoundRounds {
			t.Fatalf("final round reason = %q, want %q beside the spent round budget", turn.FinalRound, FinalRoundRounds)
		}
		if served := len(script.handed()); served != 3 {
			t.Fatalf("requests handed to the model = %d, want the drive to end on the closing round's own request", served)
		}
	})

	t.Run("a spent time budget turns the next decision into the closing round", func(t *testing.T) {
		script := newClosingRoundScript(
			closingRoundStep{hold: 80 * time.Millisecond, calls: []model.ToolCall{closingRoundCall("call-1", "echo-a")}},
			closingRoundStep{answer: "delivered"},
		)
		loop, history, marks := closingRoundLoop(t, script)
		loop.SetFinalRound(FinalRoundPolicy{Budget: 20 * time.Millisecond, Timeout: 5 * time.Second})

		turn, err := loop.Drive(context.Background(), closingRoundInvocation())
		if err != nil {
			t.Fatalf("drive under a spent time budget = %v, want the closing round delivered", err)
		}
		if turn.FinalRound != FinalRoundBudget {
			t.Fatalf("final round reason = %q, want %q beside the spent time budget", turn.FinalRound, FinalRoundBudget)
		}
		if len(turn.Decisions) != 2 || turn.Terminal != model.KindCompleted {
			t.Fatalf("turn = (%v, %d decisions), want the closing answer beside the spent budget", turn.Terminal, len(turn.Decisions))
		}
		if got := marks.recorded(); len(got) != 2 || got[0] || !got[1] {
			t.Fatalf("closing-round marks of the request boundaries = %v, want only the closing round marked", got)
		}
		if committed := history.SessionHistory(closingRoundAddress); !invariantCarries(committed, "delivered") {
			t.Fatalf("committed transcript = %+v, want the closing round's answer kept as the drive's last word", committed)
		}
	})

	t.Run("the closing round runs under its own short timeout and never holds the drive past it", func(t *testing.T) {
		script := newClosingRoundScript(
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-1", "echo-a")}},
			closingRoundStep{hang: true},
		)
		loop, _, _ := closingRoundLoop(t, script)
		loop.SetFinalRound(FinalRoundPolicy{MaxRounds: 1, Timeout: 150 * time.Millisecond})

		started := time.Now()
		turn, err := loop.Drive(context.Background(), closingRoundInvocation())
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("drive beside a closing round that never answers = %v, want the timeout reduced as a terminal", err)
		}
		if elapsed >= 2*time.Second {
			t.Fatalf("drive beside a closing round that never answers held for %v, want the closing timeout respected", elapsed)
		}
		if turn.Terminal != model.KindCancelled {
			t.Fatalf("terminal of the timed-out closing round = %v, want %v", turn.Terminal, model.KindCancelled)
		}
		if turn.FinalRound != FinalRoundRounds || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%q, %d decisions), want the closing round marked beside the tool round", turn.FinalRound, len(turn.Decisions))
		}
	})

	t.Run("a drive under no budget runs no closing round and ends on its own terminal", func(t *testing.T) {
		script := newClosingRoundScript(
			closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-1", "echo-a")}},
			closingRoundStep{answer: "done"},
		)
		loop, _, marks := closingRoundLoop(t, script)

		turn, err := loop.Drive(context.Background(), closingRoundInvocation())
		if err != nil {
			t.Fatalf("drive under no budget = %v, want the drive to end on its own terminal", err)
		}
		if turn.FinalRound != FinalRoundNone {
			t.Fatalf("final round reason = %q, want no closing round under no budget", turn.FinalRound)
		}
		if len(turn.Decisions) != 2 || turn.Terminal != model.KindCompleted {
			t.Fatalf("turn = (%v, %d decisions), want the drive's own terminal", turn.Terminal, len(turn.Decisions))
		}
		for _, marked := range marks.recorded() {
			if marked {
				t.Fatal("a request boundary of a drive under no budget was marked as the closing round")
			}
		}
	})
}

// TestSubAgentFinalRoundBounds drives three agents of one loop under their own watchdog budgets:
// - a drive of an agent declaring a round budget of one runs one tool round beside its closing round
// - a drive of an agent declaring a round budget of two runs two tool rounds beside its closing round
// - a drive of an agent declaring no budget reads the loop's default
// - every drive carries the closing instruction of the policy it ran under
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestSubAgentFinalRoundBounds(t *testing.T) {
	script := newClosingRoundScript(
		closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-a1", "echo-a")}},
		closingRoundStep{answer: "a delivered"},
		closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-b1", "echo-a")}},
		closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-b2", "echo-a")}},
		closingRoundStep{answer: "b delivered"},
		closingRoundStep{calls: []model.ToolCall{closingRoundCall("call-c1", "echo-a")}},
		closingRoundStep{answer: "c delivered"},
	)
	loop, _ := finalRoundBoundsLoop(t, script)
	loop.SetFinalRound(FinalRoundPolicy{MaxRounds: 1, Timeout: 5 * time.Second, Prompt: "close the default"})
	loop.SetAgentFinalRound("agent-a", FinalRoundPolicy{MaxRounds: 1, Timeout: 5 * time.Second, Prompt: "close agent a"})
	loop.SetAgentFinalRound("agent-b", FinalRoundPolicy{MaxRounds: 2, Timeout: 5 * time.Second, Prompt: "close agent b"})

	cases := []struct {
		agent     event.AgentID
		decision  string
		decisions int
		closing   string
	}{
		{agent: "agent-a", decision: "a", decisions: 2, closing: "close agent a"},
		{agent: "agent-b", decision: "b", decisions: 3, closing: "close agent b"},
		{agent: "agent-c", decision: "c", decisions: 2, closing: "close the default"},
	}
	for _, tc := range cases {
		turn, err := loop.Drive(context.Background(), finalRoundBoundsInvocation(tc.agent, tc.decision))
		if err != nil {
			t.Fatalf("drive of %q = %v, want the closing round delivered", tc.agent, err)
		}
		if turn.FinalRound != FinalRoundRounds {
			t.Fatalf("final round reason of %q = %q, want %q beside its own spent round budget", tc.agent, turn.FinalRound, FinalRoundRounds)
		}
		if len(turn.Decisions) != tc.decisions {
			t.Fatalf("decisions of %q = %d, want %d beside its own round budget", tc.agent, len(turn.Decisions), tc.decisions)
		}
		closing := turn.Decisions[len(turn.Decisions)-1].Request.Messages
		if last := closing[len(closing)-1]; last.Role != model.RoleUser || last.Content != tc.closing {
			t.Fatalf("closing instruction of %q = %+v, want %q", tc.agent, last, tc.closing)
		}
	}
}

// finalRoundBoundsInvocation returns one range of the named agent, in a session
// of its own so the three drives never share a transcript.
func finalRoundBoundsInvocation(agent event.AgentID, decision string) Invocation {
	return Invocation{
		InvocationID: event.InvocationID("invocation-" + decision),
		Address:      event.Address{AgentID: agent, SessionID: event.SessionID("session-" + decision)},
		Instance:     "instance-a",
		Ready: []event.Event{{
			Key:     event.EventID("event-" + decision),
			Message: model.Message{Role: model.RoleUser, Content: "start"},
		}},
	}
}

// finalRoundBoundsLoop returns one loop over the three declared agents that
// share the model given, beside the transcript face the drives commit into.
func finalRoundBoundsLoop(t *testing.T, m model.Model) (*Loop, *invariantLoopHistory) {
	t.Helper()
	ids := []event.AgentID{"agent-a", "agent-b", "agent-c"}
	declaration := &config.Declaration{Entry: "agent-a", Agents: make([]config.AgentSpec, 0, len(ids))}
	held := make(map[event.AgentID]state.AgentBinding, len(ids))
	for _, id := range ids {
		declaration.Agents = append(declaration.Agents, config.AgentSpec{ID: string(id)})
		held[id] = state.AgentBinding{Model: m, PromptSource: state.PromptSource{Version: "v1"}}
	}
	plan, err := state.NewStatePlan(
		state.OrgPlan{Declaration: declaration, Fingerprint: "fp-bounds"},
		held,
		nil,
	)
	if err != nil {
		t.Fatalf("build the binding plan of the per-agent closing-round loop: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the binding generation of the per-agent closing-round loop: %v", err)
	}
	loop := NewLoop(bindings, state.NewOccupancy(), "instance-a", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)
	return loop, history
}

// closingRoundAddress is the session the closing-round drives run in.
var closingRoundAddress = event.Address{AgentID: "agent-a", SessionID: "session-a"}

// closingRoundStep is one scripted answer of the closing-round model: the time
// the call stands, the tool calls or the answer it streams, or the never-ending
// stream a timed-out closing round sees.
type closingRoundStep struct {
	hold   time.Duration
	calls  []model.ToolCall
	answer string
	hang   bool
}

// closingRoundScript is the model of the closing-round cases: it hands the
// scripted answers in order and keeps every request it was handed.
type closingRoundScript struct {
	mu    sync.Mutex
	steps []closingRoundStep
	sent  []model.Request
}

// newClosingRoundScript returns a model answering with the scripted steps in order.
func newClosingRoundScript(steps ...closingRoundStep) *closingRoundScript {
	return &closingRoundScript{steps: steps}
}

// GenerateContent stands the call for the time its step names and streams that step's answer.
func (s *closingRoundScript) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	s.mu.Lock()
	s.sent = append(s.sent, *req)
	step := closingRoundStep{}
	if len(s.steps) > 0 {
		step = s.steps[0]
		s.steps = s.steps[1:]
	}
	s.mu.Unlock()
	if step.hold > 0 {
		select {
		case <-time.After(step.hold):
		case <-ctx.Done():
		}
	}
	if step.hang {
		return make(chan *model.Response), nil
	}
	ch := make(chan *model.Response, 1)
	message := model.Message{Role: model.RoleAssistant, Content: step.answer}
	if len(step.calls) > 0 {
		message = model.Message{Role: model.RoleAssistant, ToolCalls: step.calls}
	}
	ch <- &model.Response{Choices: []model.Choice{{Message: message}}, Done: true}
	close(ch)
	return ch, nil
}

// Info reports the model descriptor.
func (s *closingRoundScript) Info() model.Info { return model.Info{Name: "closing-round-model"} }

// handed reports the requests the model was handed, in order.
func (s *closingRoundScript) handed() []model.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Request(nil), s.sent...)
}

// closingRoundMarks records the closing-round mark of every request boundary
// the loop assembled.
type closingRoundMarks struct {
	mu    sync.Mutex
	marks []bool
}

// face records the mark of one request boundary.
func (m *closingRoundMarks) face(_ context.Context, boundary RequestBoundary) (BoundaryView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.marks = append(m.marks, boundary.FinalRound)
	return BoundaryView{}, nil
}

// recorded reports the closing-round marks in assembly order.
func (m *closingRoundMarks) recorded() []bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]bool(nil), m.marks...)
}

// closingRoundCall builds one native tool call of a scripted assistant message.
func closingRoundCall(id, name string) model.ToolCall {
	return model.ToolCall{
		ID:       id,
		Type:     "function",
		Function: model.FunctionDefinitionParam{Name: name, Arguments: []byte(`{"x":1}`)},
	}
}

// closingRoundAnswer reports the content of the message one transcript carries in answer of the named call.
func closingRoundAnswer(msgs []model.Message, callID string) string {
	for _, msg := range msgs {
		if msg.Role == model.RoleTool && msg.ToolID == callID {
			return msg.Content
		}
	}
	return ""
}

// closingRoundInvocation returns the one range the closing-round drives run.
func closingRoundInvocation() Invocation {
	return Invocation{
		InvocationID: "invocation-1",
		Address:      closingRoundAddress,
		Instance:     "instance-a",
		Ready: []event.Event{{
			Key:     "event-1",
			Message: model.Message{Role: model.RoleUser, Content: "start"},
		}},
	}
}

// closingRoundLoop returns one loop over the model named, beside the session
// transcript face the drives commit into and the boundary face that records
// the closing-round marks.
func closingRoundLoop(t *testing.T, m model.Model) (*Loop, *invariantLoopHistory, *closingRoundMarks) {
	t.Helper()
	declaration := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a"}}}
	plan, err := state.NewStatePlan(
		state.OrgPlan{Declaration: declaration, Fingerprint: "fp-a"},
		map[event.AgentID]state.AgentBinding{
			"agent-a": {Model: m, PromptSource: state.PromptSource{Version: "v1"}},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("build the binding plan of the closing-round loop: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the binding generation of the closing-round loop: %v", err)
	}
	loop := NewLoop(bindings, state.NewOccupancy(), "instance-a", nil)
	history := newInvariantLoopHistory()
	marks := &closingRoundMarks{}
	loop.SetHistory(history)
	loop.SetRequestFace(marks.face)
	return loop, history, marks
}
