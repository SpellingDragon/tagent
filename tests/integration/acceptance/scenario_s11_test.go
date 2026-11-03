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

	orgconfig "github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestScenario11ConfigDrivenSubagentAndPipeline drives one declaration of a parent beside its child.
//   - the child is assembled on the model, prompt file and tool reference its own declaration names
//   - the parent pipeline runs its Before and After passes in the declared plugin order
//   - the parent's sub-agent call is admitted as a task: the model reads the minimal admission confirmation and the task is read from the chain
//   - the child result settles back to the parent address as a fact of its own
//
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
		return scenarioFinalTurn(scenarioChildAnswer)
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
	if parentTurn.Terminal != model.KindCompleted || parentTurn.DecisionCount != 2 {
		t.Fatalf("parent turn = (%v, %d decisions), want the completed terminal beside two decisions",
			parentTurn.Terminal, parentTurn.DecisionCount)
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

	addr := event.Address{AgentID: "agent-parent", SessionID: "session-config-parent"}
	taskID, childResultDelivered := scenarioSubagentFacts(t, run, addr, "call-subagent", scenarioChildAnswer)

	for at, req := range parentRequests {
		parts := []string{}
		for _, m := range req.Messages {
			parts = append(parts, fmt.Sprintf("%s/%s/%q", m.Role, m.ToolID, m.Content))
		}
		t.Logf("S11 parent view %d: %s", at, strings.Join(parts, "; "))
	}
	{
		deadline := time.Now().Add(3 * time.Second)
		for len(parent.recorded()) <= 2 && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		for at, req := range parent.recorded() {
			parts := []string{}
			for _, m := range req.Messages {
				parts = append(parts, fmt.Sprintf("%s/%s/%q", m.Role, m.ToolID, m.Content))
			}
			t.Logf("S11 final view %d: %s", at, strings.Join(parts, "; "))
		}
	}
	declared := decl.Agents[0].Plugins
	declaredAfter := scenarioDeclaredPortOrder(declared, recorder.plugins(),
		plugin.PortFactObservation,
		plugin.PortFactView)
	settleDeadline := time.Now().Add(5 * time.Second)
	var afterGroups []scenarioAfterGroup
	var afterFacts []event.Event
	for {
		afterGroups = recorder.afterGroups()
		afterFacts = run.SessionFacts(addr)
		if len(afterGroups) >= len(parentRequests)+1 &&
			scenarioAfterCoversFacts(afterGroups, afterFacts) {
			break
		}
		if time.Now().After(settleDeadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	observations["parent_pipeline_before_order"] = recorder.beforeOrder()
	observations["parent_pipeline_after_order"] = scenarioDistinctGroups(afterGroups)
	observations["parent_pipeline_after_covers_facts"] =
		scenarioAfterCoversFacts(afterGroups, afterFacts)
	observations["parent_pipeline_matches_declaration"] =
		scenarioSameOrder(recorder.beforeOrder(), scenarioDeclaredOrder(declared, recorder.plugins(),
			plugin.PortCallTransform)) &&
			scenarioEveryGroupIs(afterGroups, declaredAfter)
	observations["parent_instruction_from_declared_file"] =
		scenarioAnyMessage(parentRequests[0].Messages, scenarioParentInstruction)

	spec := scenarioDeclaredAgent(t, decl)
	observations["subagent_call_admits_task"] =
		scenarioAdmissionConfirmed(parentRequests[1].Messages, "call-subagent") &&
			strings.HasPrefix(taskID, "task-")
	observations["subagent_result_delivered"] = childResultDelivered
	observations["child_agent_model_requests"] = len(childRequests)
	observations["child_agent_runs_declared_model"] = len(childRequests) == 1 &&
		child.Info().Name == spec.Model
	observations["child_agent_prompt_from_own_file"] = len(childRequests) == 1 &&
		scenarioAnyMessage(childRequests[0].Messages, scenarioChildInstruction)
	observations["child_agent_tool_names"] = scenarioToolNames(childRequests[0].Tools)
	observations["child_agent_declares_own_tool"] = scenarioDeclaresTool(spec, scenarioToolNames(childRequests[0].Tools))

	parentDecisionIDs := []event.DecisionID{}
	for _, fact := range run.SessionFacts(event.Address{AgentID: "agent-parent", SessionID: "session-config-parent"}) {
		if fact.InvocationID == parentTurn.InvocationID && fact.DecisionID != "" &&
			(fact.Type == event.TypeThinkingPlan || fact.Type == event.TypeAgentOutput) {
			parentDecisionIDs = append(parentDecisionIDs, fact.DecisionID)
		}
	}
	if len(parentDecisionIDs) != parentTurn.DecisionCount {
		t.Fatalf("parent decision facts = %d, want the %d decisions of the parent turn", len(parentDecisionIDs), parentTurn.DecisionCount)
	}
	for _, decisionID := range parentDecisionIDs {
		refs = append(refs, string(decisionID))
	}
	refs = append(refs, taskID)
	EmitScenarioRecord(t, scenario, refs, observations)
}

const (
	// scenarioParentInstruction marks the prompt file the parent agent declares.
	scenarioParentInstruction = "scenario-parent-instruction"
	// scenarioChildInstruction marks the prompt file the child agent declares.
	scenarioChildInstruction = "scenario-child-instruction"
	// scenarioChildAnswer is the scripted child body the parent address reads back
	// from the delegated task's settle fact.
	scenarioChildAnswer = "the child recorded the review"
	// scenarioAdmissionReceiptText is the minimal confirmation an admitted call
	// answers; the task identity never travels in it.
	scenarioAdmissionReceiptText = "ok"
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

// scenarioAfterGroup is one committed fact's After pass: the fact it was
// reached for beside the elements it visited, in visit order.
type scenarioAfterGroup struct {
	fact  string
	names []string
}

// afterGroups reports one group per committed fact, in the order the facts first
// reached the pass. A fact is grouped by its own identity rather than by
// consecutive visits: two facts' passes may interleave, and each fact still
// carries its own order. The After pass runs once per committed fact through the
// pipeline of the fact's own agent — the turn-start input, the turn-end output
// and the settled delivery of a range each commit a fact of their own — so a
// fact is observed in the declaration's order while two distinct facts carry no
// order between them.
func (r *scenarioPhaseRecorder) afterGroups() []scenarioAfterGroup {
	r.mu.Lock()
	defer r.mu.Unlock()
	at := make(map[string]int, len(r.after))
	out := make([]scenarioAfterGroup, 0, len(r.after))
	for _, visit := range r.after {
		index, held := at[visit.fact]
		if !held {
			index = len(out)
			at[visit.fact] = index
			out = append(out, scenarioAfterGroup{fact: visit.fact})
		}
		out[index].names = append(out[index].names, visit.name)
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
func scenarioEveryGroupIs(groups []scenarioAfterGroup, want []string) bool {
	if len(groups) == 0 {
		return false
	}
	for _, group := range groups {
		if !scenarioSameOrder(group.names, want) {
			return false
		}
	}
	return true
}

// scenarioDistinctGroups reports the distinct element orders the pass visited, in
// first-seen order. The number of committed facts is delivery-order dependent, so
// only the distinct order is pinned; scenarioAfterCoversFacts carries the
// coverage quantity instead.
func scenarioDistinctGroups(groups []scenarioAfterGroup) [][]string {
	out := make([][]string, 0, len(groups))
	seen := make(map[string]bool, len(groups))
	for _, group := range groups {
		key := strings.Join(group.names, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, append([]string{}, group.names...))
	}
	return out
}

// scenarioAfterCoversFacts reports whether every fact on the chain got exactly
// one After group and every group names a fact of that chain. This is the process
// quantity the old group count carried: the count itself varies with delivery
// order, but each committed fact must still be observed once.
func scenarioAfterCoversFacts(groups []scenarioAfterGroup, facts []event.Event) bool {
	if len(facts) == 0 {
		return false
	}
	want := make(map[string]bool, len(facts))
	for _, fact := range facts {
		want[string(fact.Key)] = true
	}
	seen := make(map[string]int, len(groups))
	for _, group := range groups {
		if !want[group.fact] {
			return false
		}
		seen[group.fact]++
	}
	for key := range want {
		if seen[key] != 1 {
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

// scenarioAdmissionConfirmed reports whether the next model view reads back the
// minimal admission confirmation of one call. The task identity and the state
// word never travel to the model, so this read confirms admission only; the
// call→task link is read from the chain by scenarioSubagentFacts.
func scenarioAdmissionConfirmed(messages []model.Message, callID string) bool {
	receipt := scenarioMessageOf(messages, callID, model.RoleTool)
	return receipt.Content == scenarioAdmissionReceiptText
}

// scenarioSubagentFacts waits inside a bound for the chain facts of one
// sub-agent call and reports the task the call was admitted as and whether the
// child result was delivered back to the parent address. The receipt never
// names the task, so both are read from the chain: the action command under the
// native call identity names the admitted task, and a settled fact carrying the
// child result names the delivery.
func scenarioSubagentFacts(t *testing.T, run *assembly.Run, addr event.Address, callID, resultMarker string) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		taskID, delivered := scenarioSubagentCallFacts(run.SessionFacts(addr), callID, resultMarker)
		if taskID != "" && delivered {
			return taskID, true
		}
		if time.Now().After(deadline) {
			return taskID, delivered
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// scenarioSubagentCallFacts reads the call→task link and the child-result
// delivery from one chain snapshot.
func scenarioSubagentCallFacts(facts []event.Event, callID, resultMarker string) (string, bool) {
	taskID := ""
	delivered := false
	for _, fact := range facts {
		switch fact.Type {
		case event.TypeActionCommand:
			identity := fact.Message.ToolID
			if identity == "" {
				identity = fact.CallID
			}
			if identity == callID && fact.TaskID != "" {
				taskID = string(fact.TaskID)
			}
		case event.TypeTaskSettled:
			if fact.TaskID != "" && strings.Contains(fact.Message.Content, resultMarker) {
				delivered = true
			}
		}
	}
	return taskID, delivered
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
