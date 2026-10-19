package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	orgconfig "github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"

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
	declarationPath := filepath.Join(root, "declaration.yaml")
	scenarioWritePrompt(t, declarationPath, scenarioConfigDeclaration(parentPrompt, childPrompt))
	decl, err := orgconfig.Load(ctx, declarationPath)
	if err != nil {
		t.Fatalf("load the declaration: %v", err)
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
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-parent": {Model: parent, Tools: []trpctool.CallableTool{echo}, Plugins: recorder.declaredElements(t, decl.Agents[0].Plugins)},
		"agent-child":  {Model: child, Tools: []trpctool.CallableTool{echo}},
	}, tagent.WithGovernance(&scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}))
	if err != nil {
		t.Fatalf("assemble the declaration: %v", err)
	}

	parentTurn, err := run.Invoke(ctx, tagent.InvokeRequest{
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
	observations["parent_pipeline_before_order"] = recorder.order("before")
	observations["parent_pipeline_after_order"] = recorder.order("after")
	observations["parent_pipeline_matches_declaration"] =
		scenarioSameOrder(recorder.order("before"), scenarioDeclaredOrder(declared, recorder.plugins(), plugin.PhaseCallTransform)) &&
			scenarioSameOrder(recorder.order("after"), scenarioDeclaredOrder(declared, recorder.plugins(),
				plugin.PhasePostCommitObserve, plugin.PhaseFinalPresentation))
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
	after  []string
}

// record files one pass visit under the element name.
func (r *scenarioPhaseRecorder) record(pass, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pass == "before" {
		r.before = append(r.before, name)
		return
	}
	r.after = append(r.after, name)
}

// order reports the element names one pass visited, in visit order.
func (r *scenarioPhaseRecorder) order(pass string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pass == "before" {
		return append([]string{}, r.before...)
	}
	return append([]string{}, r.after...)
}

// declaredElements reports the pipeline elements the declaration names, in the
// order it names them, so the host contributes what the declaration spells
// instead of one fixed list beside it.
func (r *scenarioPhaseRecorder) declaredElements(t *testing.T, names []string) []plugin.Plugin {
	t.Helper()
	registry := make(map[string]plugin.Plugin, len(names))
	for _, element := range r.plugins() {
		registry[element.Name()] = element
	}
	out := make([]plugin.Plugin, 0, len(names))
	for _, name := range names {
		element, ok := registry[name]
		if !ok {
			t.Fatalf("the declaration names plugin %q the drive carries no element for", name)
		}
		out = append(out, element)
	}
	return out
}

// plugins reports the four pipeline elements the drive carries, one per phase
// face the declaration names.
func (r *scenarioPhaseRecorder) plugins() []plugin.Plugin {
	return []plugin.Plugin{
		&scenarioPhasePlugin{name: "transform-a", phase: plugin.PhaseCallTransform, recorder: r},
		&scenarioPhasePlugin{name: "transform-b", phase: plugin.PhaseCallTransform, recorder: r},
		&scenarioPhasePlugin{name: "observe-a", phase: plugin.PhasePostCommitObserve, recorder: r},
		&scenarioPhasePlugin{name: "presenter-b", phase: plugin.PhaseFinalPresentation, recorder: r},
	}
}

// scenarioPhasePlugin is one pipeline element of the drive: it serves one phase
// and files every visit of that phase under its name.
type scenarioPhasePlugin struct {
	name     string
	phase    plugin.Phase
	recorder *scenarioPhaseRecorder
}

// Name reports the stable name of the element.
func (p *scenarioPhasePlugin) Name() string { return p.name }

// Phase reports the one phase the element serves.
func (p *scenarioPhasePlugin) Phase() plugin.Phase { return p.phase }

// TransformCall files the Before pass visit of a call-transform element.
func (p *scenarioPhasePlugin) TransformCall(context.Context, *trpcmodel.ToolCall) error {
	p.recorder.record("before", p.name)
	return nil
}

// ObserveCommit files the After pass visit of a post-commit observer.
func (p *scenarioPhasePlugin) ObserveCommit(context.Context, event.EventID) error {
	p.recorder.record("after", p.name)
	return nil
}

// Present files the After pass visit of a final presenter.
func (p *scenarioPhasePlugin) Present(_ context.Context, id event.EventID) (string, bool) {
	p.recorder.record("after", p.name)
	return "presentation:" + string(id), false
}

// scenarioConfigDeclaration spells the parent and child agents the drive
// declares: the child names its own model, prompt file and tool, and the parent
// names the plugin pipeline in execution order.
func scenarioConfigDeclaration(parentPrompt, childPrompt string) string {
	return fmt.Sprintf(`entry: agent-parent
agents:
  - id: agent-parent
    model: model-parent
    files:
      - path: %q
        kind: prompt
    plugins:
      - transform-a
      - transform-b
      - observe-a
      - presenter-b
    tools:
      - name: call_subagent
  - id: agent-child
    parent_id: agent-parent
    model: model-child
    files:
      - path: %q
        kind: prompt
    tools:
      - name: echo-child
`, parentPrompt, childPrompt)
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
// one of the given phases, in declaration order.
func scenarioDeclaredOrder(names []string, elements []plugin.Plugin, phases ...plugin.Phase) []string {
	phaseOf := make(map[string]plugin.Phase, len(elements))
	for _, element := range elements {
		phaseOf[element.Name()] = element.Phase()
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		for _, phase := range phases {
			if phaseOf[name] == phase {
				out = append(out, name)
				break
			}
		}
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
