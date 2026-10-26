package acceptance

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
)

// TestScenario12SubagentSourcesAndNotice drives both assembly sources of call_subagent.
// - the agents file source and the explicit source each answer with a receipt before the delegated turn ends
// - each terminal notification lands on the address that issued the call under the receipt's task identity
// - a call naming both sources is refused by name beside no task at all
// 契约: docs/EXTENSIONS.md#event-consumption
func TestScenario12SubagentSourcesAndNotice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*scenarioTimeout)
	defer cancel()
	scenario, err := ScenarioOf("S12")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	issuer := event.Address{AgentID: "agent-issuer", SessionID: "session-issuer"}
	agentsFile := scenarioWriteAgentsFile(t)
	file := scenarioDriveSubagentSource(t, ctx, issuer, "call-agents-file",
		fmt.Sprintf(`{"agents_file":%q,"input":"the change under review"}`, agentsFile))
	explicit := scenarioDriveSubagentSource(t, ctx, issuer, "call-explicit",
		fmt.Sprintf(`{"name":"reviewer","description":"reviews one change","prompt":%q,"model":"model-reviewer","input":"the change under review"}`,
			scenarioExplicitInstruction))
	refused := scenarioRefuseBothSources(t, ctx, issuer, agentsFile)

	observations["agents_file_receipt_before_terminal"] = file.early
	observations["explicit_receipt_before_terminal"] = explicit.early
	observations["terminal_notice_targets_issuer"] =
		file.targetsIssuer(issuer) && explicit.targetsIssuer(issuer)
	observations["notice_identity_fields"] = map[string]any{
		"task":     file.identityFields()["task"] == true && explicit.identityFields()["task"] == true,
		"round":    file.identityFields()["round"] == true && explicit.identityFields()["round"] == true,
		"category": file.identityFields()["category"] == true && explicit.identityFields()["category"] == true,
		"sequence": file.identityFields()["sequence"] == true && explicit.identityFields()["sequence"] == true,
	}
	observations["agents_file_source_assembled"] = file.assembledOn(scenarioAgentsFileInstruction)
	observations["explicit_source_assembled"] = explicit.assembledOn(scenarioExplicitInstruction)
	observations["both_sources_refused_by_name"] = refused.named
	observations["both_sources_admitted_tasks"] = refused.admitted

	refs = append(refs, string(file.receipt.TaskID), file.callID, string(explicit.receipt.TaskID), explicit.callID)
	EmitScenarioRecord(t, scenario, refs, observations)
}

const (
	// scenarioAgentsFileInstruction is the prompt body the agents file carries.
	scenarioAgentsFileInstruction = "scenario-agents-file-instruction"
	// scenarioExplicitInstruction is the prompt text the explicit source carries.
	scenarioExplicitInstruction = "scenario-explicit-instruction"
	// scenarioDelegatedModel is the model reference both sources name.
	scenarioDelegatedModel = "model-reviewer"
)

// scenarioSourceOutcome is one driven source: the call identity it was issued
// under, the receipt it answered with, whether that receipt returned before the
// delegated turn ended, the terminal envelope it produced and the request the
// delegated model was driven with.
type scenarioSourceOutcome struct {
	instance event.InstanceID
	callID   string
	receipt  task.AdmissionReceipt
	early    bool
	envelope task.Envelope
	requests []model.Request
}

// targetsIssuer reports whether the terminal notification landed on the
// address that issued the call, under the instance that froze the route.
func (o scenarioSourceOutcome) targetsIssuer(issuer event.Address) bool {
	return o.envelope.Target == issuer && o.envelope.TargetInstance == o.instance
}

// identityFields reports whether the terminal notification kept the identity
// fields of the receipt it belongs to.
func (o scenarioSourceOutcome) identityFields() map[string]bool {
	notice := o.envelope.Notice
	return map[string]bool{
		"task":     notice.TaskID == o.receipt.TaskID,
		"round":    notice.Round == 1,
		"category": notice.Kind == task.NoticeSettled,
		"sequence": notice.Seq == 0,
	}
}

// assembledOn reports whether the delegated turn ran on the declared model with
// the named instruction.
func (o scenarioSourceOutcome) assembledOn(instruction string) bool {
	if len(o.requests) != 1 {
		return false
	}
	return scenarioAnyMessage(o.requests[0].Messages, instruction)
}

// scenarioDriveSubagentSource drives one source through the real tool, task face
// and delegation ledger.
func scenarioDriveSubagentSource(t *testing.T, ctx context.Context, issuer event.Address, callID, args string) scenarioSourceOutcome {
	t.Helper()
	provider := &scenarioGatedModel{
		name:    scenarioDelegatedModel,
		gate:    make(chan struct{}),
		started: make(chan struct{}),
	}
	rig := newScenarioSubagentRig(t)
	tool := rig.newTool(t, scenarioSubagentBuilder(provider))
	rig.hold(t, &scenarioModel{name: "model-issuer", answer: func(*model.Request) *model.Response {
		return scenarioFinalTurn("the issuing session decides alone")
	}}, issuer)

	answered := make(chan scenarioAnswer, 1)
	go func() {
		out, err := tool.Call(task.WithCallFrame(context.Background(), rig.frame(issuer, callID)), []byte(args))
		answered <- scenarioAnswer{out: out, err: err}
	}()
	var got scenarioAnswer
	select {
	case got = <-answered:
	case <-time.After(scenarioTimeout):
		t.Fatalf("call %s did not answer with its receipt", callID)
	}
	if got.err != nil {
		t.Fatalf("call %s = %v, want the admission receipt", callID, got.err)
	}
	receipt, ok := got.out.(task.AdmissionReceipt)
	if !ok || !receipt.Accepted || receipt.TaskID == "" {
		t.Fatalf("call %s answered %+v, want one accepted admission beside its task identity", callID, got.out)
	}
	awaitSignal(t, provider.started, "the delegated turn to reach its model")
	early := len(rig.arrived) == 0
	close(provider.gate)
	envelope := scenarioAwaitEnvelope(t, rig.arrived)
	return scenarioSourceOutcome{
		instance: rig.instance,
		callID:   callID,
		receipt:  receipt,
		early:    early,
		envelope: envelope,
		requests: provider.recorded(),
	}
}

// scenarioRefusalOutcome reports the refusal of one call naming both sources.
type scenarioRefusalOutcome struct {
	named    bool
	admitted int
}

// scenarioRefuseBothSources drives one call that names both assembly sources
// and reports the named refusal beside the tasks it admitted.
func scenarioRefuseBothSources(t *testing.T, ctx context.Context, issuer event.Address, agentsFile string) scenarioRefusalOutcome {
	t.Helper()
	provider := &scenarioGatedModel{name: scenarioDelegatedModel, started: make(chan struct{})}
	rig := newScenarioSubagentRig(t)
	tool := rig.newTool(t, scenarioSubagentBuilder(provider))
	rig.hold(t, &scenarioModel{name: "model-issuer", answer: func(*model.Request) *model.Response {
		return scenarioFinalTurn("the issuing session decides alone")
	}}, issuer)
	args := fmt.Sprintf(`{"agents_file":%q,"name":"reviewer","prompt":%q}`, agentsFile, scenarioExplicitInstruction)
	out, err := tool.Call(task.WithCallFrame(ctx, rig.frame(issuer, "call-both-sources")), []byte(args))
	if err != nil {
		t.Fatalf("call both sources = %v, want the named refusal as result text", err)
	}
	text := fmt.Sprint(out)
	named := strings.Contains(text, "[call_subagent_denied]") &&
		strings.Contains(text, "both an agents file and explicit parameters were given") &&
		strings.Contains(text, "no task was admitted")
	select {
	case <-provider.started:
		t.Fatalf("a refused call drove the delegated model: %s", text)
	default:
	}
	return scenarioRefusalOutcome{named: named, admitted: rig.ledger.Pending()}
}

// scenarioAnswer is one tool answer of a driven call.
type scenarioAnswer struct {
	out any
	err error
}

// scenarioGatedModel is the delegated turn's model: it records the request it is
// driven with, signals its entry and holds its answer until the gate opens.
type scenarioGatedModel struct {
	mu       sync.Mutex
	name     string
	gate     chan struct{}
	started  chan struct{}
	once     sync.Once
	requests []model.Request
}

// GenerateContent records the request, signals the turn's entry, waits for the
// gate and streams the settled answer.
func (m *scenarioGatedModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	m.mu.Unlock()
	if m.started != nil {
		m.once.Do(func() { close(m.started) })
	}
	if m.gate != nil {
		select {
		case <-m.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan *model.Response, 1)
	ch <- scenarioFinalTurn("the delegated turn settled")
	close(ch)
	return ch, nil
}

// Info reports the delegated model descriptor.
func (m *scenarioGatedModel) Info() model.Info { return model.Info{Name: m.name} }

// recorded reports the requests the delegated model was driven with.
func (m *scenarioGatedModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// scenarioSubagentRig is the real task face, binding holder and delegation
// ledger one call_subagent call is admitted through.
type scenarioSubagentRig struct {
	instance     event.InstanceID
	invocationID event.InvocationID
	bindings     *state.Bindings
	ledger       *agent.Delegator
	loop         *agent.Loop
	arrived      chan task.Envelope
}

// scenarioDelegationFace is the task face the ledger settles its delegations
// through: the manager's admission, obligations and flush beside the registry's
// status transition, as one face.
type scenarioDelegationFace struct {
	*task.MemoryManager
	*task.MemoryRegistry
}

// newScenarioSubagentRig returns the real faces of one sub-agent call, before
// any generation is held.
func newScenarioSubagentRig(t *testing.T) *scenarioSubagentRig {
	t.Helper()
	rig := &scenarioSubagentRig{
		instance:     event.InstanceID("instance-scenario-subagent"),
		invocationID: event.InvocationID("inv-scenario-subagent"),
		bindings:     state.NewBindings(),
		arrived:      make(chan task.Envelope, 4),
	}
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(rig.accept))
	rig.ledger = agent.NewDelegator(&scenarioDelegationFace{MemoryManager: manager, MemoryRegistry: registry}, rig.bindings, rig.instance)
	return rig
}

// newTool returns the call_subagent tool over the rig's real delegation and
// borrowing faces and the builder the drive fixes.
func (r *scenarioSubagentRig) newTool(t *testing.T, builder subagent.Builder) *subagent.Tool {
	t.Helper()
	tool, err := subagent.NewTool(subagent.ToolOptions{
		Parent:     &subagent.Assembly{Definition: subagent.Definition{Name: "issuer", Model: "model-issuer"}},
		Builder:    builder,
		Delegation: r.ledger,
		Bindings:   r.bindings,
	})
	if err != nil {
		t.Fatalf("construct the call_subagent tool: %v", err)
	}
	return tool
}

// hold publishes the generation the call borrows and builds the loop of the
// issuing session over it.
func (r *scenarioSubagentRig) hold(t *testing.T, provider model.Model, issuer event.Address) {
	t.Helper()
	binding := state.AgentBinding{
		Model:        provider,
		PromptSource: state.PromptSource{Version: "prompt-scenario-subagent-v1"},
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

// accept reports the delivery-layer outcome of one notification beside the
// routing its envelope froze and hands the settled notification back into the
// invocation that issued the call.
func (r *scenarioSubagentRig) accept(ctx context.Context, envelope task.Envelope) (task.DeliveryAcceptance, error) {
	if err := r.ledger.Observe(ctx, envelope.Notice); err != nil {
		return task.DeliveryRejected, err
	}
	r.loop.Backflow(r.invocationID, envelope.Notice)
	r.arrived <- envelope
	return task.DeliveryAccepted, nil
}

// frame returns the frame the loop of the issuing session gives one call.
func (r *scenarioSubagentRig) frame(issuer event.Address, callID string) task.CallFrame {
	return task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   r.instance,
			InvocationID: r.invocationID,
			DecisionID:   event.DecisionID("decision-" + callID),
			CallID:       callID,
		},
		Address:   issuer,
		Execution: "session-serialized",
	}
}

// scenarioSubagentBuilder resolves the model reference of the assembled
// sub-agent into the scripted model of the delegated turn.
func scenarioSubagentBuilder(provider trpcmodel.Model) subagent.Builder {
	return subagent.Builder{Model: func(_ context.Context, name string) (trpcmodel.Model, error) {
		if name != provider.Info().Name {
			return nil, fmt.Errorf("model reference %q names no scripted model", name)
		}
		return provider, nil
	}}
}

// scenarioAwaitEnvelope waits for one notification envelope to reach the
// deliverer.
func scenarioAwaitEnvelope(t *testing.T, ch <-chan task.Envelope) task.Envelope {
	t.Helper()
	select {
	case envelope := <-ch:
		return envelope
	case <-time.After(scenarioTimeout):
		t.Fatal("timed out waiting for the terminal notification")
		return task.Envelope{}
	}
}

// scenarioWriteAgentsFile writes one agents file declaring a reviewer sub-agent.
func scenarioWriteAgentsFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reviewer.md")
	content := `---
name: reviewer
description: reviews one change
model: ` + scenarioDelegatedModel + `
---
` + scenarioAgentsFileInstruction + `
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write the agents file: %v", err)
	}
	return path
}
