package execution

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	// finalRoundToolName is the tool the closing-round cases call.
	finalRoundToolName = "deliver_tool"
	// finalRoundToolOutput is what that tool answers.
	finalRoundToolOutput = "delivered"
	// finalRoundPrompt is the closing instruction the case attaches, so the
	// request of the closing round is read by the instruction it carries.
	finalRoundPrompt = "close out: deliver what this invocation already produced"
)

// TestFinalRoundClosesBesideRealDrain drives one range to its spent round budget over the real task face:
// - the decision the spent budget leaves is the closing round, marked on its request boundary beside the closing instruction it carries
// - the tool call that round issues is admitted nowhere and never executes, while the call of the round before it still runs to real settlement
// - the closing round ends the drive, and the close that follows drains to real termination beside no obligation it opened
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestFinalRoundClosesBesideRealDrain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := event.Address{AgentID: "agent-final", SessionID: "session-final"}
	tool := &convTool{name: finalRoundToolName, output: finalRoundToolOutput}

	roundMu := sync.Mutex{}
	round := 0
	provider := &customEventModel{name: "model-final", answer: func(req *model.Request) *model.Response {
		roundMu.Lock()
		round++
		current := round
		roundMu.Unlock()
		if current == 1 {
			return convToolTurn(convCall("call-open", finalRoundToolName, `{"step":1}`))
		}
		if last := req.Messages[len(req.Messages)-1]; last.Role != model.RoleUser || last.Content != finalRoundPrompt {
			t.Errorf("closing request ends on %+v, want the closing instruction carried after the transcript", last)
		}
		return convToolTurn(convCall("call-late", finalRoundToolName, `{"step":2}`))
	}}
	rig := newFinalRoundRig(t, provider, tool, owner)
	rig.loop.SetFinalRound(agent.FinalRoundPolicy{MaxRounds: 1, Timeout: 5 * time.Second, Prompt: finalRoundPrompt})

	turn, err := rig.loop.Drive(ctx, agent.Invocation{
		InvocationID: rig.invocationID,
		Address:      owner,
		Instance:     rig.instance,
		Ready: []event.Event{{
			Key:     "event-final",
			Message: model.Message{Role: model.RoleUser, Content: "start"},
		}},
	})
	if err != nil {
		t.Fatalf("drive to the spent round budget: %v", err)
	}
	if turn.FinalRound != agent.FinalRoundRounds {
		t.Fatalf("final round reason = %q, want %q", turn.FinalRound, agent.FinalRoundRounds)
	}
	if len(turn.Decisions) != 2 || turn.Terminal != model.KindCompleted {
		t.Fatalf("turn = (%v, %d decisions), want the tool round beside the closing round", turn.Terminal, len(turn.Decisions))
	}
	if got := rig.marks.recorded(); len(got) != 2 || got[0] || !got[1] {
		t.Fatalf("closing-round marks of the request boundaries = %v, want only the closing round marked", got)
	}
	if got := rig.gate.admissions(); got != 1 {
		t.Fatalf("admissions of the tool face = %d, want the closing round's call admitted nowhere", got)
	}
	if got := tool.calls(); got != 1 {
		t.Fatalf("tool executions = %d, want the closing round's call never executed", got)
	}
	committed := rig.history.SessionHistory(owner)
	answer := finalRoundAnswer(committed, "call-late")
	if !strings.Contains(answer, "budget of this invocation is spent") {
		t.Fatalf("answer of the closing round's call = %q, want the refusal that admits nothing", answer)
	}
	if settled := finalRoundAnswer(committed, "call-open"); strings.Contains(settled, "budget of this invocation is spent") {
		t.Fatalf("answer of the tool round's call = %q, want that round's ordinary admission", settled)
	}
	if got := len(provider.recorded()); got != 2 {
		t.Fatalf("model requests = %d, want the tool round beside the closing round", got)
	}

	report, err := rig.shutdown.Close(ctx, agent.CloseRequest{
		Address:  owner,
		Deadline: time.Now().Add(10 * time.Second),
	})
	if err != nil || !report.Drained || len(report.Unsettled) != 0 {
		t.Fatalf("close after the closing round = (%+v, %v), want the drain beside real termination", report, err)
	}
	admitted := rig.gate.taskIDs()
	if len(admitted) != 1 {
		t.Fatalf("tasks tracked by the close face = %v, want the one task the tool round opened", admitted)
	}
	open, err := rig.shutdown.Obligations(ctx, admitted[0])
	if err != nil || open.Count != 0 {
		t.Fatalf("obligations of the admitted task after the drain = (%+v, %v), want every obligation settled for real", open, err)
	}
}

// finalRoundRig is the host loop of the closing-round case: the task manager
// whose mailbox is the delivery face, the close face that fronts those
// obligations, and the transcript face the drive commits into.
type finalRoundRig struct {
	instance     event.InstanceID
	invocationID event.InvocationID
	bindings     *state.Bindings
	loop         *agent.Loop
	shutdown     *agent.Shutdown
	gate         *finalRoundGate
	history      *finalRoundHistory
	marks        *finalRoundMarks

	mu    sync.Mutex
	facts map[event.EventID]model.Message
	seq   uint64
}

// newFinalRoundRig builds the host loop over the given provider and tool, with
// the tool registered through the task wrapper and its admissions tracked
// beside the close face.
func newFinalRoundRig(t *testing.T, provider model.Model, tool *convTool, owner event.Address) *finalRoundRig {
	t.Helper()
	rig := &finalRoundRig{
		instance:     event.InstanceID("instance-final"),
		invocationID: event.InvocationID("inv-final"),
		bindings:     state.NewBindings(),
		history:      newFinalRoundHistory(),
		marks:        &finalRoundMarks{},
		facts:        map[event.EventID]model.Message{},
	}
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(rig.envelope))
	rig.shutdown = agent.NewShutdown(&delegationFace{MemoryManager: manager, MemoryRegistry: registry})
	ledger := agent.NewDelegator(rig.shutdown, rig.bindings, rig.instance)
	rig.shutdown.SetReconciler(ledger)
	rig.gate = &finalRoundGate{
		inner: task.Wrap(manager, tool,
			task.WithResultRecorder(rig.record), task.WithMailbox(rig.mail)),
		shutdown: rig.shutdown,
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: &config.Declaration{
		Entry:  string(owner.AgentID),
		Agents: []config.AgentSpec{{ID: string(owner.AgentID), Model: provider.Info().Name}},
	}}, map[event.AgentID]state.AgentBinding{owner.AgentID: {
		Model:        provider,
		Tools:        []trpctool.Tool{rig.gate},
		PromptSource: state.PromptSource{Version: "prompt-final-round-v1"},
	}}, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	if err := rig.bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
	rig.loop = agent.NewLoop(rig.bindings, state.NewOccupancy(), rig.instance, rig.resolve)
	rig.loop.SetHistory(rig.history)
	rig.loop.SetRequestFace(rig.marks.face)
	rig.loop.SetWatch(rig.shutdown.Watch)
	return rig
}

// envelope reports the acceptance of every notification the manager offers for
// delivery.
func (r *finalRoundRig) envelope(context.Context, task.Envelope) (task.DeliveryAcceptance, error) {
	return task.DeliveryAccepted, nil
}

// mail admits one settled notification into the mailbox of this rig and hands
// it back into the invocation that issued the call.
func (r *finalRoundRig) mail(_ context.Context, notice task.Notice) error {
	r.loop.Backflow(r.invocationID, notice)
	return nil
}

// record stores the result of one wrapped call as the fact its notification
// names, so the backflow carries the settled body into the next decision.
func (r *finalRoundRig) record(_ context.Context, result task.Result) (event.EventID, error) {
	content := ""
	if text, ok := result.Output.(string); ok {
		content = text
	}
	if result.Err != nil {
		content = result.Err.Error()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	id := event.EventID("fact-final-" + string(result.CallID))
	r.facts[id] = model.Message{Role: model.RoleTool, ToolID: result.CallID, ToolName: result.Tool, Content: content}
	return id, nil
}

// resolve returns the message one recorded fact carries.
func (r *finalRoundRig) resolve(id event.EventID) (model.Message, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	msg, ok := r.facts[id]
	return msg, ok
}

// finalRoundGate is the tool face of the closing-round case: it hands every
// call to the task wrapper and records the task of each fresh admission beside
// the close face, so the drain settles the obligations the drive really opened.
type finalRoundGate struct {
	inner    trpctool.CallableTool
	shutdown *agent.Shutdown

	mu    sync.Mutex
	count int
	tasks []event.TaskID
}

// Declaration returns the declaration of the wrapped tool unchanged.
func (g *finalRoundGate) Declaration() *trpctool.Declaration { return g.inner.Declaration() }

// Call admits one call through the task wrapper and tracks the task it opened.
func (g *finalRoundGate) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	answer, err := g.inner.Call(ctx, jsonArgs)
	if receipt, ok := answer.(task.AdmissionReceipt); ok && receipt.Accepted {
		g.mu.Lock()
		g.count++
		g.tasks = append(g.tasks, receipt.TaskID)
		g.mu.Unlock()
		g.shutdown.Track(receipt.TaskID)
	}
	return answer, err
}

// admissions reports how many calls of this tool were admitted.
func (g *finalRoundGate) admissions() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.count
}

// taskIDs reports the tasks the admissions of this tool opened, in order.
func (g *finalRoundGate) taskIDs() []event.TaskID {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]event.TaskID(nil), g.tasks...)
}

// finalRoundMarks records the closing-round mark of every request boundary the
// loop assembled.
type finalRoundMarks struct {
	mu    sync.Mutex
	marks []bool
}

// face records the mark of one request boundary.
func (m *finalRoundMarks) face(_ context.Context, boundary agent.RequestBoundary) (agent.BoundaryView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.marks = append(m.marks, boundary.FinalRound)
	return agent.BoundaryView{}, nil
}

// recorded reports the closing-round marks in assembly order.
func (m *finalRoundMarks) recorded() []bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]bool(nil), m.marks...)
}

// finalRoundHistory keeps one transcript per session address.
type finalRoundHistory struct {
	mu        sync.Mutex
	bySession map[event.SessionID][]model.Message
}

// newFinalRoundHistory returns an empty session transcript face.
func newFinalRoundHistory() *finalRoundHistory {
	return &finalRoundHistory{bySession: map[event.SessionID][]model.Message{}}
}

// SessionHistory reports the transcript accumulated for one session.
func (h *finalRoundHistory) SessionHistory(addr event.Address) []model.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]model.Message(nil), h.bySession[addr.SessionID]...)
}

// CommitHistory appends one ended drive's transcript to its own session.
func (h *finalRoundHistory) CommitHistory(addr event.Address, msgs []model.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bySession[addr.SessionID] = append(h.bySession[addr.SessionID], msgs...)
}

// SessionFacts reports no fact of the session: this rig keeps the transcript alone and
// commits no chain beside it, so a decision boundary of a drive reads no input again.
func (h *finalRoundHistory) SessionFacts(event.Address) []event.Event { return nil }

// finalRoundAnswer reports the content of the message one transcript carries in
// answer of the named call.
func finalRoundAnswer(msgs []model.Message, callID string) string {
	for _, msg := range msgs {
		if msg.Role == model.RoleTool && msg.ToolID == callID {
			return msg.Content
		}
	}
	return ""
}
