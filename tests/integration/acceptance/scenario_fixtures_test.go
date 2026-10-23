package acceptance

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// scenarioModel is the native provider of the scenario drives: it records every
// request it is driven with and answers each through the scripted answer, so the
// observations read the requests the runtime actually assembled.
// 契约: docs/wiki/agent/agent-architecture.md#test-support
type scenarioModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the native request beside the scripted answer.
func (m *scenarioModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the provider descriptor.
func (m *scenarioModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the provider was driven with.
func (m *scenarioModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// scenarioTool is the native tool of the scenario drives: it counts its runs and
// reports the configured output under the arguments it received.
type scenarioTool struct {
	mu     sync.Mutex
	name   string
	output string
	count  int
	args   string
}

// Declaration reports the native declaration of the tool.
func (t *scenarioTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the tool once beside the arguments it received.
func (t *scenarioTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count++
	t.args = string(jsonArgs)
	return t.output, nil
}

// calls reports how often the tool executed.
func (t *scenarioTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// lastArgs reports the arguments the tool last received.
func (t *scenarioTool) lastArgs() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.args
}

// scenarioGatedTool is the native tool whose call enters and then waits for the
// gate, so a scenario can observe the runtime while the call is still in flight.
type scenarioGatedTool struct {
	mu      sync.Mutex
	name    string
	output  string
	entered chan struct{}
	gate    chan struct{}
	count   int
	once    sync.Once
}

// Declaration reports the native declaration of the gated tool.
func (t *scenarioGatedTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call signals its entry and waits for the gate before reporting its output.
func (t *scenarioGatedTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	t.count++
	t.mu.Unlock()
	if t.entered != nil {
		t.once.Do(func() { close(t.entered) })
	}
	if t.gate != nil {
		select {
		case <-t.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return t.output, nil
}

// calls reports how often the gated tool executed.
func (t *scenarioGatedTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// scenarioToolTurn builds the scripted native response carrying one assistant
// message of tool calls.
func scenarioToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Done:    true,
	}
}

// scenarioFinalTurn builds the scripted terminal response carrying an answer.
func scenarioFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

// scenarioFailTurn builds the scripted terminal response carrying a provider
// failure.
func scenarioFailTurn(message string) *model.Response {
	return &model.Response{Done: true, Error: &model.ResponseError{Message: message, Type: model.ErrorTypeAPIError}}
}

// scenarioCall builds one native tool call of a scripted assistant message.
func scenarioCall(id, name, args string) model.ToolCall {
	return model.ToolCall{ID: id, Type: "function", Function: model.FunctionDefinitionParam{Name: name, Arguments: []byte(args)}}
}

// scenarioGovernance is the deterministic governance of the publication driven
// scenarios: each named check answers one fixed disposition.
type scenarioGovernance struct {
	answers map[state.CheckName]state.CheckAnswer
}

// Check answers the named checks through the fixed dispositions.
func (g *scenarioGovernance) Check(ctx context.Context, plan state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return g.answers, nil
}

// scenarioPassingChecks reports one passing disposition per named check.
func scenarioPassingChecks(names ...state.CheckName) map[state.CheckName]state.CheckAnswer {
	answers := map[state.CheckName]state.CheckAnswer{}
	for _, name := range names {
		answers[name] = state.CheckAnswer{
			Name:     name,
			Verdict:  state.CheckPass,
			Reason:   "the scenario stand-in reports the mechanism check as passed",
			Scope:    "scenario",
			Evidence: []string{"deterministic scenario input"},
		}
	}
	return answers
}

// scenarioPublishContract reports the publish contract the scenario publications
// carry a disposition for.
func scenarioPublishContract() state.PublishContract {
	return state.PublishContract{Required: []state.CheckName{"eval", "benchmark", "apply-gate"}}
}

// scenarioPublicationSink records the publication records committed through it.
type scenarioPublicationSink struct {
	mu      sync.Mutex
	records []state.PublicationRecord
}

// CommitPublication records one publication record.
func (s *scenarioPublicationSink) CommitPublication(ctx context.Context, record state.PublicationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

// published reports how many publication records were committed.
func (s *scenarioPublicationSink) published() []state.PublicationRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]state.PublicationRecord(nil), s.records...)
}

// scenarioReleases records the release of each candidate resource by name.
type scenarioReleases struct {
	mu    sync.Mutex
	names []string
}

// track reports the release of one named resource.
func (r *scenarioReleases) track(name string) func(context.Context) error {
	return func(context.Context) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.names = append(r.names, name)
		return nil
	}
}

// released reports the released resource names in release order.
func (r *scenarioReleases) released() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.names...)
}

// scenarioCandidate prepares one publication candidate of the named declaration
// beside the candidate-exclusive leases.
func scenarioCandidate(t *testing.T, pub *state.Publisher, decl *config.Declaration, agents map[event.AgentID]state.AgentBinding, fingerprint string, leases *state.Leases) state.Candidate {
	t.Helper()
	plan := state.OrgPlan{Declaration: decl, Baseline: pub.Baseline(), Fingerprint: fingerprint}
	built, err := state.NewStatePlan(plan, agents, leases.Refs())
	if err != nil {
		t.Fatalf("prepare the candidate plan %s: %v", fingerprint, err)
	}
	return state.Candidate{Plan: built, Resources: leases}
}

// scenarioDeclaration reports the two-agent declaration the organization
// publication scenarios drive.
func scenarioDeclaration() *config.Declaration {
	return &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{
			{ID: "agent-a", Model: "model-a"},
			{ID: "agent-b", Model: "model-b"},
		},
	}
}

// scenarioBindings reports one binding per agent of the two-agent declaration.
func scenarioBindings(a, b model.Model) map[event.AgentID]state.AgentBinding {
	return map[event.AgentID]state.AgentBinding{
		"agent-a": {Model: a},
		"agent-b": {Model: b},
	}
}

// awaitSignal waits for one signal channel to close inside the scenario bound.
func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(scenarioTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// scenarioTimeout bounds every scenario wait, so a stalled drive fails instead of
// hanging the run.
const scenarioTimeout = 30 * time.Second

// scenarioDecisionIDs reports the decision identities of one turn.
func scenarioDecisionIDs(decisions []agent.Decision) []string {
	ids := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		ids = append(ids, string(decision.DecisionID))
	}
	return ids
}

// scenarioRoles reports the role sequence of one recorded request.
func scenarioRoles(messages []model.Message) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, string(message.Role))
	}
	return roles
}

// scenarioContent reports the content of the messages carrying one call
// identity, in the order they appear.
func scenarioContent(messages []model.Message, callID string) []string {
	out := []string{}
	for _, message := range messages {
		if message.ToolID == callID {
			out = append(out, message.Content)
		}
	}
	return out
}

// scenarioDescribe reports a readable value for one observation of an unexpected
// type, so a failed drive names what it received.
func scenarioDescribe(value any) string {
	return fmt.Sprintf("%v", value)
}
