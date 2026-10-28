package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	orgconfig "github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestScenario11ConfigDrivenSubagentAndPipeline drives one declaration of a parent beside its child.
// - the child is assembled on the model, prompt file and tool reference its own declaration names
// - the parent pipeline runs its Before and After passes in the declared plugin order
// - the parent's sub-agent call is admitted as a task and the parent reads its admission receipt
// 契约: docs/EXTENSIONS.md#pipeline-phases
func TestScenario11ConfigDrivenSubagentAndPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*scenarioTimeout)
	defer cancel()
	scenario, err := ScenarioOf("S11")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	root := t.TempDir()
	parentPrompt := filepath.Join(root, "parent.md")
	childPrompt := filepath.Join(root, "child.md")
	scenarioWritePrompt(t, parentPrompt, scenarioParentInstruction+"\n")
	scenarioWritePrompt(t, childPrompt, scenarioChildInstruction+"\n")
	decl := &orgconfig.Declaration{
		Entry: "agent-parent",
		Agents: []orgconfig.AgentSpec{
			{
				ID:      "agent-parent",
				Model:   "model-parent",
				Files:   []orgconfig.FileRef{{Path: parentPrompt, Kind: orgconfig.FileKindPrompt}},
				Plugins: []string{"transform-a", "transform-b", "observe-a", "presenter-b"},
				Tools:   []orgconfig.ToolRef{{Name: "call_subagent"}},
			},
			{
				ID:       "agent-child",
				ParentID: "agent-parent",
				Model:    "model-child",
				Files:    []orgconfig.FileRef{{Path: childPrompt, Kind: orgconfig.FileKindPrompt}},
				Tools:    []orgconfig.ToolRef{{Name: "echo-child"}},
			},
		},
	}

	recorder := &scenarioPhaseRecorder{}
	child := &scenarioModel{name: "model-child", answer: func(*model.Request) *model.Response {
		return scenarioFinalTurn("the child recorded the review")
	}}
	turns := 0
	parent := &scenarioModel{name: "model-parent"}
	parent.answer = func(*model.Request) *model.Response {
		turns++
		if turns == 1 {
			return scenarioToolTurn(scenarioCall("call-subagent", "call_subagent", scenarioSubagentArgs(t, decl)))
		}
		return scenarioFinalTurn("the parent settled")
	}
	echo := &scenarioTool{name: "echo-child", output: "echoed"}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-parent": {Model: parent, Tools: []trpctool.CallableTool{echo}, Plugins: recorder.declaredElements(t, decl.Agents[0].Plugins)},
		"agent-child":  {Model: child, Tools: []trpctool.CallableTool{echo}},
	}, assembly.WithGovernance(&scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}))
	if err != nil {
		t.Fatalf("assemble the declaration: %v", err)
	}

	parentTurn, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-config-parent",
		Address:      event.Address{AgentID: "agent-parent", SessionID: "session-config-parent"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "delegate the review"}},
	})
	if err != nil {
		t.Fatalf("invoke the parent: %v", err)
	}
	if parentTurn.Terminal != model.KindCompleted || len(parentTurn.Decisions) != 2 {
		t.Fatalf("parent turn = (%v, %d decisions), want the completed terminal beside two decisions",
			parentTurn.Terminal, len(parentTurn.Decisions))
	}
	parentRequests := parent.recorded()
	if len(parentRequests) != 2 {
		t.Fatalf("parent requests = %d, want one request per decision", len(parentRequests))
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(child.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	childRequests := child.recorded()

	declared := decl.Agents[0].Plugins
	declaredAfter := scenarioDeclaredPortOrder(declared, recorder.plugins(),
		plugin.PortFactObservation,
		plugin.PortFactView)
	settleDeadline := time.Now().Add(5 * time.Second)
	for len(recorder.afterGroups()) < len(parentRequests)+1 && time.Now().Before(settleDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	afterGroups := recorder.afterGroups()
	observations["parent_pipeline_before_order"] = recorder.beforeOrder()
	observations["parent_pipeline_after_order"] = afterGroups
	observations["parent_pipeline_matches_declaration"] =
		scenarioSameOrder(recorder.beforeOrder(), scenarioDeclaredOrder(declared, recorder.plugins(),
			plugin.PortCallTransform)) &&
			scenarioEveryGroupIs(afterGroups, declaredAfter)
	observations["parent_instruction_from_declared_file"] =
		scenarioAnyMessage(parentRequests[0].Messages, scenarioParentInstruction)

	spec := scenarioDeclaredAgent(t, decl)
	taskID, accepted := scenarioAdmissionReceipt(parentRequests[1].Messages, "call-subagent")
	observations["subagent_call_admits_task"] = accepted && strings.HasPrefix(taskID, "task-")
	observations["child_agent_model_requests"] = len(childRequests)
	observations["child_agent_runs_declared_model"] = len(childRequests) == 1 &&
		child.Info().Name == spec.Model
	observations["child_agent_prompt_from_own_file"] = len(childRequests) == 1 &&
		scenarioAnyMessage(childRequests[0].Messages, scenarioChildInstruction)
	observations["child_agent_tool_names"] = scenarioToolNames(childRequests[0].Tools)
	observations["child_agent_declares_own_tool"] = scenarioDeclaresTool(spec, scenarioToolNames(childRequests[0].Tools))

	refs = append(refs, string(parentTurn.Decisions[0].DecisionID), string(parentTurn.Decisions[1].DecisionID))
	refs = append(refs, taskID)
	EmitScenarioRecord(t, scenario, refs, observations)
}

const (
	// scenarioParentInstruction marks the prompt file the parent agent declares.
	scenarioParentInstruction = "scenario-parent-instruction"
	// scenarioChildInstruction marks the prompt file the child agent declares.
	scenarioChildInstruction = "scenario-child-instruction"
)

// scenarioPhaseRecorder records the order in which the pipeline passes reach
// their elements.
type scenarioPhaseRecorder struct {
	mu     sync.Mutex
	before []string
	after  []scenarioAfterVisit
}

// scenarioAfterVisit is one After pass visit: the element the pass reached
// beside the committed fact it was reached for.
type scenarioAfterVisit struct {
	fact string
	name string
}

// recordBefore files one Before pass visit under the element name.
func (r *scenarioPhaseRecorder) recordBefore(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.before = append(r.before, name)
}

// recordFact files one After pass visit under the element name and the committed
// fact the pass observed.
func (r *scenarioPhaseRecorder) recordFact(fact, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.after = append(r.after, scenarioAfterVisit{fact: string(fact), name: name})
}

// beforeOrder reports the element names the Before pass visited, in visit order.
func (r *scenarioPhaseRecorder) beforeOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.before...)
}

// afterGroups reports the elements each committed fact's After pass visited, one
// group per fact in the order the facts committed. The After pass runs once per
// committed fact through the pipeline of the fact's own agent — the turn-start
// input, the turn-end output and the settled delivery of a range each commit a
// fact of their own — so a fact is observed in the declaration's order while two
// distinct facts carry no order between them.
func (r *scenarioPhaseRecorder) afterGroups() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, 0, len(r.after))
	last := ""
	for _, visit := range r.after {
		if len(out) == 0 || visit.fact != last {
			out = append(out, []string{})
		}
		out[len(out)-1] = append(out[len(out)-1], visit.name)
		last = visit.fact
	}
	return out
}

// declaredElements reports the pipeline elements the declaration names, in the
// order it names them, so the host contributes what the declaration spells
// instead of one fixed list beside it.
func (r *scenarioPhaseRecorder) declaredElements(t *testing.T, names []string) []plugin.Instance {
	t.Helper()
	registry := make(map[string]plugin.Instance, len(names))
	for _, element := range r.plugins() {
		registry[element.Name()] = element
	}
	out := make([]plugin.Instance, 0, len(names))
	for _, name := range names {
		element, ok := registry[name]
		if !ok {
			t.Fatalf("the declaration names plugin %q the drive carries no element for", name)
		}
		out = append(out, element)
	}
	return out
}

// plugins reports the four pipeline elements the drive carries, one per port
// face the declaration names.
func (r *scenarioPhaseRecorder) plugins() []plugin.Instance {
	return []plugin.Instance{
		&scenarioPhasePlugin{name: "transform-a", port: plugin.PortCallTransform, recorder: r},
		&scenarioPhasePlugin{name: "transform-b", port: plugin.PortCallTransform, recorder: r},
		&scenarioPhasePlugin{name: "observe-a", port: plugin.PortFactObservation, recorder: r},
		&scenarioPhasePlugin{name: "presenter-b", port: plugin.PortFactView, recorder: r},
	}
}

// scenarioPhasePlugin is one pipeline element of the drive: it serves one port
// and files every visit of that port under its name.
type scenarioPhasePlugin struct {
	name     string
	port     plugin.Port
	recorder *scenarioPhaseRecorder
}

// Name reports the stable name of the element.
func (p *scenarioPhasePlugin) Name() string { return p.name }

// Port reports the one port the element serves.
func (p *scenarioPhasePlugin) Port() plugin.Port { return p.port }

// TransformCall files the Before pass visit of a call-transform element.
func (p *scenarioPhasePlugin) TransformCall(context.Context, *trpcmodel.ToolCall) error {
	p.recorder.recordBefore(p.name)
	return nil
}

// ObserveCommit files the After pass visit of a post-commit observer beside the
// fact it observed.
func (p *scenarioPhasePlugin) ObserveCommit(_ context.Context, id event.EventID) error {
	p.recorder.recordFact(string(id), p.name)
	return nil
}

// Present files the After pass visit of a final presenter beside the fact it
// presented.
func (p *scenarioPhasePlugin) Present(_ context.Context, id event.EventID) (string, bool) {
	p.recorder.recordFact(string(id), p.name)
	return "presentation:" + string(id), false
}

// scenarioSubagentArgs reports the call arguments derived from the child
// agent's own declaration, so the call carries what the declaration names.
func scenarioSubagentArgs(t *testing.T, decl *orgconfig.Declaration) string {
	t.Helper()
	child := scenarioDeclaredAgent(t, decl)
	prompt := ""
	for _, file := range child.Files {
		if file.Kind == orgconfig.FileKindPrompt {
			prompt = file.Path
			break
		}
	}
	tools := make([]string, 0, len(child.Tools))
	for _, ref := range child.Tools {
		tools = append(tools, ref.Name)
	}
	args, err := json.Marshal(map[string]any{
		"name":   child.ID,
		"model":  child.Model,
		"prompt": prompt,
		"tools":  tools,
		"input":  "review the change",
	})
	if err != nil {
		t.Fatalf("encode the sub-agent call: %v", err)
	}
	return string(args)
}

// scenarioDeclaredAgent reports the non-entry agent of the declaration.
func scenarioDeclaredAgent(t *testing.T, decl *orgconfig.Declaration) orgconfig.AgentSpec {
	t.Helper()
	for _, spec := range decl.Agents {
		if spec.ID != decl.Entry {
			return spec
		}
	}
	t.Fatalf("declaration %q names no child agent", decl.Entry)
	return orgconfig.AgentSpec{}
}

// scenarioDeclaredOrder reports the names of the declaration whose elements serve
// one of the given ports, in declaration order.
func scenarioDeclaredOrder(names []string, elements []plugin.Instance, ports ...plugin.Port) []string {
	portOf := make(map[string]plugin.Port, len(elements))
	for _, element := range elements {
		portOf[element.Name()] = element.Port()
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		for _, port := range ports {
			if portOf[name] == port {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// scenarioEveryGroupIs reports whether every group of a per-fact grouping of one
// pass visited exactly the declared order, and at least one fact was observed: a
// pass that reached no fact at all matches no declaration.
func scenarioEveryGroupIs(groups [][]string, want []string) bool {
	if len(groups) == 0 {
		return false
	}
	for _, group := range groups {
		if !scenarioSameOrder(group, want) {
			return false
		}
	}
	return true
}

// scenarioDeclaredPortOrder reports the declared elements serving the given
// ports in the order the After pass visits them: the ports in the order the
// pass runs them, and each port in the declaration's own order. The port is
// what places an element in the pipeline, so this is the declaration read the way
// the pipeline reads it.
func scenarioDeclaredPortOrder(names []string, elements []plugin.Instance, ports ...plugin.Port) []string {
	out := make([]string, 0, len(names))
	for _, port := range ports {
		out = append(out, scenarioDeclaredOrder(names, elements, port)...)
	}
	return out
}

// scenarioSameOrder reports whether two name sequences carry the same order.
func scenarioSameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// scenarioAnyMessage reports whether any message content carries the marker.
func scenarioAnyMessage(messages []model.Message, marker string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, marker) {
			return true
		}
	}
	return false
}

// scenarioToolNames reports the tool names one request carries, in stable order.
func scenarioToolNames(tools map[string]trpctool.Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// scenarioDeclaresTool reports whether the agent declaration names one of the
// tool names the request carried.
func scenarioDeclaresTool(spec orgconfig.AgentSpec, names []string) bool {
	for _, ref := range spec.Tools {
		for _, name := range names {
			if ref.Name == name {
				return true
			}
		}
	}
	return false
}

// scenarioMessageOf reports the message filed under one call identity and role.
func scenarioMessageOf(messages []model.Message, callID string, role model.Role) model.Message {
	for _, message := range messages {
		if message.ToolID == callID && message.Role == role {
			return message
		}
	}
	return model.Message{}
}

// scenarioAdmissionReceipt reports the task identity and acceptance the next
// model view reads back for one call.
func scenarioAdmissionReceipt(messages []model.Message, callID string) (string, bool) {
	receipt := scenarioMessageOf(messages, callID, model.RoleTool)
	observation := scenarioMessageOf(messages, callID, model.RoleUser)
	if !strings.HasPrefix(receipt.Content, "accepted ") {
		return "", false
	}
	fields := strings.Fields(strings.Trim(observation.Content, "{}"))
	if len(fields) != 4 || fields[0] != callID {
		return "", false
	}
	return fields[1], fields[2] == "true"
}

// scenarioWritePrompt writes one prompt or declaration file of the drive.
func scenarioWritePrompt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
