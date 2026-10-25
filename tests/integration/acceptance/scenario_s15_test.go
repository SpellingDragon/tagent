package acceptance

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
)

// scenarioS15Policy is the threshold set the ladder scenario reads one task
// under: the three silences are a minute apart, and no execution timeout or time
// to live cuts the ladder short.
var scenarioS15Policy = task.Policy{
	WatchAfter:   time.Minute,
	StableAfter:  2 * time.Minute,
	SuspectAfter: 3 * time.Minute,
}

// scenarioS15Margin is the offset a probe instant is placed past its threshold,
// so the silence read is on the far side of the threshold either way.
const scenarioS15Margin = 10 * time.Second

// scenarioS15ProgressFragment is the input material a progress notification is
// handed to the next decision as.
const scenarioS15ProgressFragment = "reports progress in round"

// scenarioS15Input is the input that opens the range the progress lane drives.
const scenarioS15Input = "逻辑状态场景的输入。"

// scenarioS15Spec is the task the ladder scenarid admits into the registry.
func scenarioS15Spec() task.Spec {
	return task.Spec{
		CallID:       "call-s15",
		DedupKey:     task.DedupKey("inst-s15\x00inv-s15\x00dec-s15\x00call-s15"),
		InvocationID: "inv-s15",
		InstanceID:   "inst-s15",
		Description:  "tool-s15",
		Source:       event.Address{AgentID: "agent-a", SessionID: "session-s15"},
		Target:       event.Address{AgentID: "tool-s15", SessionID: "session-s15"},
		Execution:    "serialized",
	}
}

// TestScenario15TaskLogicalStatus drives the logical status ladder of one task beside the progress lane of one range.
// - a task whose state stands unchanged crosses watch, stable and suspect in that order, and each step is reported once
// - the range is opened by one decision before the report arrives, so the fact has an addressed range to land in
// - the decision after the notification is the one that must stay clear of it
// - a progress notification stays a fact of the range and reaches no decision of the invocation that issued the call, and answers no call
// - the lane is a witness, not a recorder: the observation reads false only while the assertions on the request and the committed fact hold
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestScenario15TaskLogicalStatus(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S15")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}

	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission(), task.WithPolicy(scenarioS15Policy))
	receipt, err := registry.Register(ctx, scenarioS15Spec())
	if err != nil {
		t.Fatalf("register the ladder task: %v", err)
	}
	registered := time.Now()
	ladder := []string{}
	for _, silence := range []time.Duration{
		time.Minute + scenarioS15Margin,
		2*time.Minute + scenarioS15Margin,
		3*time.Minute + scenarioS15Margin,
	} {
		for _, change := range registry.Watch(ctx, registered.Add(silence)) {
			if change.TaskID != receipt.TaskID {
				continue
			}
			ladder = append(ladder, string(change.To))
		}
	}

	loop, provider := scenarioProgressLoop(t)
	const invocationID = event.InvocationID("inv-progress-15")
	facts := &scenarioProgressFacts{}
	loop.SetFactCommit(facts.commit)
	rangeAddress := event.Address{AgentID: "agent-a", SessionID: "session-s15"}
	instance := event.InstanceID("inst-s15")
	if _, err := loop.Drive(ctx, agent.Invocation{
		InvocationID: invocationID,
		Address:      rangeAddress,
		Instance:     instance,
		Ready:        []event.Event{{Key: "event-s15", Message: model.Message{Role: model.RoleUser, Content: scenarioS15Input}}},
	}); err != nil {
		t.Fatalf("open the range the progress lane drives: %v", err)
	}
	progress := task.Notice{TaskID: "task-progress-15", Round: 1, Kind: task.NoticeProgress, Seq: 0, Event: "fact-progress-15"}
	loop.Backflow(invocationID, progress)
	if _, err := loop.Drive(ctx, agent.Invocation{
		InvocationID: invocationID,
		Address:      rangeAddress,
		Instance:     instance,
		Ready:        []event.Event{{Key: "event-s15-next", Message: model.Message{Role: model.RoleUser, Content: scenarioS15Input}}},
	}); err != nil {
		t.Fatalf("drive the range beside a progress notification: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want the one decision per drive", len(requests))
	}
	carried, answered := false, false
	for _, message := range requests[1].Messages {
		if strings.Contains(message.Content, scenarioS15ProgressFragment) {
			carried = carried || (message.Role == model.RoleUser && message.ToolID == "" && message.ToolName == "")
			answered = answered || message.ToolID == "task-progress-15"
		}
	}

	observations := map[string]any{}
	if carried {
		t.Errorf("request messages = %+v, want the progress of the call carried into no decision", requests[1].Messages)
	}
	if landed := facts.under(agent.NoticeFactKey(invocationID, progress)); len(landed) != 1 {
		t.Errorf("facts committed under the notice identity = %d, want the one fact the progress lands as", len(landed))
	}
	observations["logical_status_ladder"] = ladder
	observations["progress_reaches_decision"] = carried && !answered

	EmitScenarioRecord(t, scenario, []string{string(receipt.TaskID), string(invocationID)}, observations)
}

// scenarioProgressLoop returns one loop over the held binding of the progress
// scenario beside the provider it drives, so a notification handed back to an
// invocation reaches that invocation's next decision.
func scenarioProgressLoop(t *testing.T) (*agent.Loop, *scenarioModel) {
	t.Helper()
	provider := &scenarioModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return scenarioFinalTurn("answered")
	}}
	address := event.Address{AgentID: "agent-a", SessionID: "session-s15"}
	binding := state.AgentBinding{
		Model:        provider,
		PromptSource: state.PromptSource{Version: "prompt-scenario-15-v1"},
	}
	bindings := state.NewBindings()
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: &config.Declaration{
		Entry:  string(address.AgentID),
		Agents: []config.AgentSpec{{ID: string(address.AgentID), Model: provider.Info().Name}},
	}}, map[event.AgentID]state.AgentBinding{address.AgentID: binding}, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
	return agent.NewLoop(bindings, state.NewOccupancy(), event.InstanceID("inst-s15"), nil), provider
}

// scenarioProgressFacts is the commit face of the progress scenario: it keeps
// every fact the loop commits, so the report can be read back under the identity
// production commits it with rather than under one the scenario invents.
type scenarioProgressFacts struct {
	mu   sync.Mutex
	seen []event.Event
}

// commit keeps one fact as it stands and reports the identity it carries.
func (f *scenarioProgressFacts) commit(evt event.Event) event.EventID {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, evt)
	return evt.Key
}

// under reports the facts committed under one identity.
func (f *scenarioProgressFacts) under(key event.EventID) []event.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var hits []event.Event
	for _, evt := range f.seen {
		if evt.Key == key {
			hits = append(hits, evt)
		}
	}
	return hits
}
