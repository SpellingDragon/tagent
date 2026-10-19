package execution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestSubagentCallAdmitsTask drives one call_subagent call from its assembly to its task:
// - the call answers with the admission receipt while the delegated turn is still held
// - the terminal notification lands on the address that issued the call, under the task identity of the receipt
// - the invocation that issued the call reads its receipt, then the settlement, and only then decides again
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestSubagentCallAdmitsTask(t *testing.T) {
	t.Run("the receipt returns while the delegated turn is still held", func(t *testing.T) {
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer", gate: make(chan struct{}), started: make(chan struct{})}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		rig.start(t, &convModel{name: "model-caller", answer: func(*model.Request) *model.Response {
			return convFinalTurn("the caller decides alone")
		}}, nil, issuer)

		args := []byte(`{"name":"reviewer","description":"reviews one change","prompt":"review the change",` +
			`"model":"model-reviewer","input":"the change under review"}`)
		type answer struct {
			out any
			err error
		}
		answered := make(chan answer, 1)
		go func() {
			out, err := tool.Call(task.WithCallFrame(context.Background(), subagentCallFrame(issuer, "call-review")), args)
			answered <- answer{out: out, err: err}
		}()

		var got answer
		select {
		case got = <-answered:
		case <-time.After(2 * time.Second):
			t.Fatal("the call did not answer with its receipt while the delegated turn was still held")
		}
		if got.err != nil {
			t.Fatalf("call the tool = %v, want the admission receipt", got.err)
		}
		receipt, ok := got.out.(task.AdmissionReceipt)
		if !ok || !receipt.Accepted || receipt.Kind != task.AdmissionFresh || receipt.TaskID == "" {
			t.Fatalf("call result = %+v, want one accepted fresh admission beside its task identity", got.out)
		}
		select {
		case <-delegated.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the delegated turn never started beside the receipt")
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations = %d, want the admitted call still booked", rig.ledger.Pending())
		}
		close(delegated.gate)

		envelope := awaitEnvelope(t, rig.arrived)
		if envelope.Target != issuer {
			t.Fatalf("terminal notification target = %v, want the address that issued the call %v", envelope.Target, issuer)
		}
		if envelope.TargetInstance != rig.instance {
			t.Fatalf("terminal notification instance = %q, want %q", envelope.TargetInstance, rig.instance)
		}
		notice := envelope.Notice
		if notice.TaskID != receipt.TaskID || notice.Kind != task.NoticeSettled || notice.Round != 1 || notice.Seq != 0 {
			t.Fatalf("terminal notification = %+v, want the settled identity of %s", notice, receipt.TaskID)
		}
		if got := rig.ledger.View(rig.invocationID); len(got) != 1 || got[0].Status != task.StatusSettled {
			t.Fatalf("delegation view = %+v, want the call landed at its terminal", got)
		}
		if got := delegated.count(); got != 1 {
			t.Fatalf("delegated turns = %d, want exactly the one assembled turn", got)
		}
	})

	t.Run("the invocation that issued the call reads its receipt and then the settlement", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer"}
		file := writeSubagentAgentsFile(t)
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		call := fmt.Sprintf(`{"agents_file":%q,"input":"the change under review"}`, file)
		caller := &convModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-review", subagent.ToolName, call))
			}
			return convFinalTurn("the delegation was admitted")
		}}
		rig.start(t, caller, []trpctool.Tool{tool}, issuer)

		turn := rig.drive(t, ctx, issuer, "review the change")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		requests := caller.recorded()
		if len(requests) != 2 {
			t.Fatalf("caller requests = %d, want one request per decision", len(requests))
		}
		receipt := findMessage(requests[1].Messages, "call-review", model.RoleTool)
		if receipt == nil || !strings.Contains(receipt.Content, "accepted task-") {
			t.Fatalf("second request carries no admission receipt: %+v", requests[1].Messages)
		}
		settled := findMessage(requests[1].Messages, "call-review", model.RoleUser)
		if settled == nil {
			t.Fatalf("second request carries no settled observation: %+v", requests[1].Messages)
		}
		envelope := awaitEnvelope(t, rig.arrived)
		if envelope.Target != issuer {
			t.Fatalf("terminal notification target = %v, want the address that issued the call %v", envelope.Target, issuer)
		}
		if envelope.Notice.TaskID == "" || envelope.Notice.Kind != task.NoticeSettled {
			t.Fatalf("terminal notification = %+v, want the settled identity of the admitted call", envelope.Notice)
		}
		if got := rig.ledger.View(rig.invocationID); len(got) != 1 || got[0].Status != task.StatusSettled {
			t.Fatalf("delegation view = %+v, want the call landed at its terminal", got)
		}
		if got := delegated.count(); got != 1 {
			t.Fatalf("delegated turns = %d, want exactly the one assembled turn", got)
		}
	})
}

// subagentCallRig assembles the real task face, binding holder and delegation ledger one
// call_subagent call is admitted through, beside the deliverer that routes every terminal
// notification back into the invocation that issued the call.
type subagentCallRig struct {
	instance     event.InstanceID
	invocationID event.InvocationID
	bindings     *state.Bindings
	tasks        *delegationFace
	ledger       *agent.Delegator
	loop         *agent.Loop
	arrived      chan task.NoticeEnvelope
}

// newSubagentCallRig returns the real faces of one sub-agent call, before any generation
// is held.
func newSubagentCallRig(t *testing.T) *subagentCallRig {
	t.Helper()
	rig := &subagentCallRig{
		instance:     event.InstanceID("instance-subagent-call"),
		invocationID: event.InvocationID("inv-subagent-call"),
		bindings:     state.NewBindings(),
		arrived:      make(chan task.NoticeEnvelope, 4),
	}
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(rig.accept))
	rig.tasks = &delegationFace{MemoryManager: manager, MemoryRegistry: registry}
	rig.ledger = agent.NewDelegator(rig.tasks, rig.bindings, rig.instance)
	return rig
}

// newTool returns the call_subagent tool over the rig's real delegation and borrowing
// faces and the builder the subtest fixes.
func (r *subagentCallRig) newTool(t *testing.T, builder subagent.Builder) *subagent.Tool {
	t.Helper()
	tool, err := subagent.NewTool(subagent.ToolOptions{
		Parent:     &subagent.Assembly{Definition: subagent.Definition{Name: "caller", Model: "model-caller"}},
		Builder:    builder,
		Delegation: r.ledger,
		Bindings:   r.bindings,
	})
	if err != nil {
		t.Fatalf("construct the call_subagent tool: %v", err)
	}
	return tool
}

// start holds the generation the call borrows and builds the loop of the invoking
// session over it.
func (r *subagentCallRig) start(t *testing.T, provider model.Model, tools []trpctool.Tool, issuer event.Address) {
	t.Helper()
	binding := state.AgentBinding{
		Model:        provider,
		Tools:        tools,
		PromptSource: state.PromptSource{Version: "prompt-subagent-call-v1"},
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: &config.Declaration{
		Entry:  string(issuer.AgentID),
		Agents: []config.AgentSpec{{ID: string(issuer.AgentID), Model: provider.Info().Name}},
	}}, map[event.AgentID]state.AgentBinding{issuer.AgentID: binding}, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	if err := r.bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
	r.loop = agent.NewLoop(r.bindings, state.NewOccupancy(), r.instance, nil)
}

// accept reports the delivery-layer outcome of one notification beside the routing its
// envelope froze and hands the settled notification back into the invocation that issued
// the call.
func (r *subagentCallRig) accept(ctx context.Context, envelope task.NoticeEnvelope) (task.DeliveryAcceptance, error) {
	if err := r.ledger.Observe(ctx, envelope.Notice); err != nil {
		return task.DeliveryRejected, err
	}
	r.loop.Backflow(r.invocationID, envelope.Notice)
	r.arrived <- envelope
	return task.DeliveryAccepted, nil
}

// drive runs one invocation of the issuing session to its terminal.
func (r *subagentCallRig) drive(t *testing.T, ctx context.Context, issuer event.Address, input string) agent.Turn {
	t.Helper()
	turn, err := r.loop.Drive(ctx, agent.Invocation{
		InvocationID: r.invocationID,
		Address:      issuer,
		Instance:     r.instance,
		Ready: []event.Event{{
			Key:       event.EventID("ev-" + string(r.invocationID)),
			Type:      event.TypeExternalInput,
			Timestamp: time.Now(),
			Message:   model.Message{Role: model.RoleUser, Content: input},
		}},
	})
	if err != nil {
		t.Fatalf("drive the invocation that issues the call: %v", err)
	}
	return turn
}

// subagentCallFrame returns the frame the loop of the issuing session gives one call.
func subagentCallFrame(issuer event.Address, callID string) task.CallFrame {
	return task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-subagent-call",
			InvocationID: "inv-subagent-call",
			DecisionID:   "decision-subagent-call",
			CallID:       callID,
		},
		Address:   issuer,
		Execution: "session-serialized",
	}
}

// delegatedBuilder resolves the model reference of the assembled sub-agent into the
// scripted model of the delegated turn; every other reference stays undeclared.
func delegatedBuilder(provider trpcmodel.Model) subagent.Builder {
	return subagent.Builder{Model: func(_ context.Context, name string) (trpcmodel.Model, error) {
		if name != provider.Info().Name {
			return nil, fmt.Errorf("model reference %q names no scripted model", name)
		}
		return provider, nil
	}}
}

// heldServeModel is the delegated turn's model: it records every request and answers the
// assembled instruction, and may hold its answer until the subtest opens its gate.
type heldServeModel struct {
	name    string
	gate    chan struct{}
	started chan struct{}
	once    sync.Once

	mu       sync.Mutex
	requests []trpcmodel.Request
}

// GenerateContent records the request, signals that the turn started, waits for the gate
// when one is attached and streams the final answer.
func (m *heldServeModel) GenerateContent(_ context.Context, req *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	m.mu.Unlock()
	if m.started != nil {
		m.once.Do(func() { close(m.started) })
	}
	if m.gate != nil {
		<-m.gate
	}
	ch := make(chan *trpcmodel.Response, 1)
	ch <- &trpcmodel.Response{
		Done:    true,
		Choices: []trpcmodel.Choice{{Message: trpcmodel.NewAssistantMessage("review recorded")}},
	}
	close(ch)
	return ch, nil
}

// Info reports the delegated model descriptor.
func (m *heldServeModel) Info() trpcmodel.Info { return trpcmodel.Info{Name: m.name} }

// count reports how many turns the delegated model was driven with.
func (m *heldServeModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// findMessage reports the transcript message carrying the named call identity under the
// named role, or nil when the transcript carries none.
func findMessage(messages []model.Message, callID string, role model.Role) *model.Message {
	for i := range messages {
		if messages[i].ToolID == callID && messages[i].Role == role {
			return &messages[i]
		}
	}
	return nil
}

// awaitEnvelope waits for one notification envelope to reach the deliverer.
func awaitEnvelope(t *testing.T, ch <-chan task.NoticeEnvelope) task.NoticeEnvelope {
	t.Helper()
	select {
	case envelope := <-ch:
		return envelope
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the terminal notification")
		return task.NoticeEnvelope{}
	}
}

// writeSubagentAgentsFile writes one agents file declaring a reviewer sub-agent.
func writeSubagentAgentsFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "reviewer.md")
	content := `---
name: reviewer
description: reviews one change
model: model-reviewer
---
Review the change and report findings.
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write the agents file: %v", err)
	}
	return path
}
