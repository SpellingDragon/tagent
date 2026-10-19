package capabilities

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// TestNativeToolsAndSkillState drives the public assembly with the native file, skill and function tools registered and checks the state their execution carries.
// - the registered native file, skill and function tools enter Request.Tools and execute for real
// - the strict protocol stand-in reads the real request of the round instead of answering a fixed final
// - the skill load state flows back into the state the effective binding carries: the fixed body and content version of a loaded skill
// - a refused skill load reports the named reason it refused by
// - a file change is read at the next request boundary while the request under way keeps its fixed view and the structural generation stays where it is
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestNativeToolsAndSkillState(t *testing.T) {
	t.Run("the registered native file, skill and function tools enter Request.Tools and execute for real", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, filepath.Join("prompts", "system.md"), "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")

		var fnMu sync.Mutex
		fnCalls := 0
		fnTool := function.NewFunctionTool(
			func(_ context.Context, in ntFnInput) (string, error) {
				fnMu.Lock()
				fnCalls++
				fnMu.Unlock()
				return "fn-out:" + in.Text, nil
			},
			function.WithName("fn_tool"),
			function.WithDescription("echo the text back through a native function tool"),
		)

		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			if round == 1 {
				return ntToolTurn(
					ntCall("call-fn", "fn_tool", `{"text":"hi"}`),
					ntCall("call-save", "save_file", `{"file_name":"probe.txt","contents":"probe\n","overwrite":true}`),
					ntCall("call-load", "skill_load", `{"skill":"greet"}`),
				)
			}
			for _, name := range []string{"fn_tool", "read_file", "save_file", "skill_load"} {
				if req.Tools[name] == nil {
					t.Errorf("second request tools carry no %q, want every registered native tool visible to the model", name)
				}
			}
			if len(req.Messages) != 10 {
				t.Errorf("second request messages = %d (%+v), want the fixed file bodies, user, assistant, one admission receipt and one settled observation per call", len(req.Messages), req.Messages)
			} else {
				if msg := req.Messages[0]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "system prompt") {
					t.Errorf("second request prompt body = %+v, want the prompt file fixed at the request boundary", msg)
				}
				if msg := req.Messages[1]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "body v1") {
					t.Errorf("second request skill body = %+v, want the skill file fixed at the request boundary", msg)
				}
				if msg := req.Messages[3]; msg.Role != model.RoleAssistant || len(msg.ToolCalls) != 3 {
					t.Errorf("second request assistant = %+v, want the real assistant message carrying all three calls", msg)
				}
				for i, want := range []struct{ callID, tool, settled string }{
					{callID: "call-fn", tool: "fn_tool", settled: "fn-out:hi"},
					{callID: "call-save", tool: "save_file", settled: "Successfully saved: probe.txt"},
					{callID: "call-load", tool: "skill_load", settled: "loaded: greet"},
				} {
					checkReceipt(t, req.Messages[4+i], want.callID, want.tool, want.settled)
					checkObservation(t, req.Messages[7+i], want.callID, want.tool, want.settled)
				}
			}
			return ntFinalTurn("settled")
		}}

		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{fnTool}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-native-tools",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-native-tools"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the native tools: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}

		fnMu.Lock()
		gotFn := fnCalls
		fnMu.Unlock()
		if gotFn != 1 {
			t.Fatalf("function tool executions = %d, want the contributed function tool executed exactly once", gotFn)
		}
		probe, err := os.ReadFile(filepath.Join(root, "probe.txt"))
		if err != nil || string(probe) != "probe\n" {
			t.Fatalf("probe file = (%q, %v), want the native file tool wrote the real file under the registered root", probe, err)
		}
		for _, name := range []string{"fn_tool", "read_file", "save_file", "skill_load"} {
			if turn.Decisions[0].Request.Tools[name] == nil {
				t.Fatalf("first request tools = %v, want the registered %q carried into Request.Tools", turn.Decisions[0].Request.Tools, name)
			}
		}
		res := resourcesOf(t, turn.Decisions[0])
		kinds := map[state.ToolKind]bool{}
		for _, rec := range res.Resources() {
			kinds[rec.Kind] = true
		}
		if !kinds[state.ToolKindFile] || !kinds[state.ToolKindSkill] || !kinds[state.ToolKindFunction] {
			t.Fatalf("registration records = %+v, want the file, skill and function families all registered", res.Resources())
		}
		if load, ok := res.SkillLoad("greet"); !ok || !load.Loaded || !strings.Contains(load.Body, "body v1") {
			t.Fatalf("skill load state = (%+v, %v), want the executed skill load left its state on the binding", load, ok)
		}
	})

	t.Run("the skill load state flows back into the state the effective binding carries", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, filepath.Join("prompts", "system.md"), "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")

		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			if round == 1 {
				return ntToolTurn(
					ntCall("call-load", "skill_load", `{"skill":"greet"}`),
					ntCall("call-missing", "skill_load", `{"skill":"missing"}`),
				)
			}
			if len(req.Messages) != 8 {
				t.Errorf("second request messages = %d (%+v), want the fixed file bodies, user, assistant, one admission receipt and one settled observation per call", len(req.Messages), req.Messages)
			} else {
				for i, want := range []struct{ callID, tool, settled string }{
					{callID: "call-load", tool: "skill_load", settled: "loaded: greet"},
					{callID: "call-missing", tool: "skill_load", settled: "unknown skill"},
				} {
					checkReceipt(t, req.Messages[4+i], want.callID, want.tool, want.settled)
					checkObservation(t, req.Messages[6+i], want.callID, want.tool, want.settled)
				}
			}
			return ntFinalTurn("settled")
		}}

		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-skill-state",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-skill-state"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the skill loads: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}

		res := resourcesOf(t, turn.Decisions[0])
		load, ok := res.SkillLoad("greet")
		if !ok || !load.Loaded || load.Version == "" || !strings.Contains(load.Body, "body v1") {
			t.Fatalf("loaded skill state = (%+v, %v), want the body and content version fixed on the binding", load, ok)
		}
		if load.Agent != "agent-a" {
			t.Fatalf("loaded skill agent = %q, want the agent whose request carried the load", load.Agent)
		}
		refused, ok := res.SkillLoad("missing")
		if !ok || refused.Loaded || !strings.Contains(refused.Reason, "unknown skill") {
			t.Fatalf("refused skill state = (%+v, %v), want the named reason of the load that did not succeed", refused, ok)
		}
		if len(res.SkillLoads()) != 2 {
			t.Fatalf("skill load states = %+v, want one recorded state per skill the request loaded", res.SkillLoads())
		}
	})

	t.Run("a file change is read at the next request boundary while one request keeps its fixed view", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, filepath.Join("prompts", "system.md"), "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		rewritten := "---\nname: greet\ndescription: \"greet the user\"\n---\n\nbody v2\n"

		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			switch round {
			case 1:
				return ntToolTurn(
					ntCall("call-load-1", "skill_load", `{"skill":"greet"}`),
					ntCall("call-load-2", "skill_load", `{"skill":"greet"}`),
				)
			case 2:
				return ntToolTurn(
					ntCall("call-save", "save_file", fmt.Sprintf(`{"file_name":"skills/greet/SKILL.md","contents":%q,"overwrite":true}`, rewritten)),
				)
			case 4:
				return ntToolTurn(ntCall("call-load-3", "skill_load", `{"skill":"greet"}`))
			default:
				return ntFinalTurn(fmt.Sprintf("round-%d done", round))
			}
		}}

		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		address := event.Address{AgentID: "agent-a", SessionID: "session-file-change"}
		first, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-file-change-1",
			Address:      address,
			Input:        []model.Message{{Role: model.RoleUser, Content: "invoke one"}},
		})
		if err != nil {
			t.Fatalf("invoke the first request: %v", err)
		}
		if first.Terminal != model.KindCompleted || len(first.Decisions) != 3 {
			t.Fatalf("first turn = (%v, %d decisions), want the completed terminal beside the load, rewrite and closing decisions", first.Terminal, len(first.Decisions))
		}

		res := resourcesOf(t, first.Decisions[0])
		fixed, ok := res.SkillLoad("greet")
		if !ok || !fixed.Loaded || !strings.Contains(fixed.Body, "body v1") {
			t.Fatalf("fixed skill view = (%+v, %v), want the body the request fixed kept as its own view", fixed, ok)
		}
		onDisk, err := os.ReadFile(filepath.Join(root, "skills", "greet", skill.SkillFile))
		if err != nil || !strings.Contains(string(onDisk), "body v2") {
			t.Fatalf("skill file = (%q, %v), want the rewrite landed on disk inside the request", onDisk, err)
		}

		second, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-file-change-2",
			Address:      address,
			Input:        []model.Message{{Role: model.RoleUser, Content: "invoke two"}},
		})
		if err != nil {
			t.Fatalf("invoke the second request: %v", err)
		}
		if second.Terminal != model.KindCompleted || len(second.Decisions) != 2 {
			t.Fatalf("second turn = (%v, %d decisions), want the completed terminal beside two decisions", second.Terminal, len(second.Decisions))
		}
		reread, ok := res.SkillLoad("greet")
		if !ok || !reread.Loaded || !strings.Contains(reread.Body, "body v2") {
			t.Fatalf("reread skill view = (%+v, %v), want the rewritten content read at the next request boundary", reread, ok)
		}
		if reread.Version == fixed.Version {
			t.Fatalf("content versions = (%s, %s), want the rewritten body fixed under a new content version", fixed.Version, reread.Version)
		}
		for _, decision := range append(append([]agent.Decision{}, first.Decisions...), second.Decisions...) {
			if decision.Binding.Model != provider {
				t.Fatalf("decision binding model = %v, want the same structural generation across both requests", decision.Binding.Model)
			}
			if decision.Binding.Set != res {
				t.Fatalf("decision binding set = %v, want the tool-set connection of the same structural generation", decision.Binding.Set)
			}
		}
	})
}

// ntFnInput is the input of the contributed native function tool.
type ntFnInput struct {
	Text string `json:"text"`
}

// ntModel is the strict protocol stand-in of these sub-cases: it records every
// native request it is driven with and answers each with the scripted response
// of its round. It never reaches a real endpoint.
type ntModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the native request and streams the scripted response.
func (m *ntModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the fake provider's descriptor.
func (m *ntModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the fake provider was driven with.
func (m *ntModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// declarationOf declares one agent assembled from the prompt and skill files
// under the given root.
func declarationOf(root string) *config.Declaration {
	return &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:    "agent-a",
			Model: "model-a",
			Files: []config.FileRef{
				{Path: filepath.Join(root, "prompts", "system.md"), Kind: config.FileKindPrompt},
				skillFileRef(root, "greet"),
			},
			Params: map[string]float64{},
		}},
	}
}

// skillFileRef references one skill folder's SKILL.md under the given root.
func skillFileRef(root, name string) config.FileRef {
	return config.FileRef{
		Path: filepath.Join(root, "skills", name, skill.SkillFile),
		Kind: config.FileKindSkill,
	}
}

// writePromptFile writes one prompt file under the given root.
func writePromptFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the prompt directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write the prompt file: %v", err)
	}
}

// writeSkillFolder writes one skill folder with its SKILL.md under the given
// root, in the shape the framework skill repository scans and parses.
func writeSkillFolder(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the skill directory: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: \"greet the user\"\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write the skill file: %v", err)
	}
}

// resourcesOf reports the tool resources the decision's binding carries.
func resourcesOf(t *testing.T, decision agent.Decision) *state.ToolResources {
	t.Helper()
	res, ok := decision.Binding.Set.(*state.ToolResources)
	if !ok {
		t.Fatalf("binding tool set = %T, want the tool resources the composition root registered", decision.Binding.Set)
	}
	return res
}

// checkPair checks one paired tool message against the call it answers.
func checkPair(t *testing.T, msg model.Message, callID, tool, content string) {
	t.Helper()
	if msg.Role != model.RoleTool || msg.ToolID != callID || msg.ToolName != tool || !strings.Contains(msg.Content, content) {
		t.Errorf("paired tool message = %+v, want call %q answered under tool %q carrying %q", msg, callID, tool, content)
	}
}

// checkReceipt checks one paired tool message against the admission receipt of
// its call: the receipt pairs the call with its admitted task before that task
// settles, beside no settled body and beside no wrapper prefix.
func checkReceipt(t *testing.T, msg model.Message, callID, tool, settled string) {
	t.Helper()
	if msg.Role != model.RoleTool || msg.ToolID != callID || msg.ToolName != tool ||
		!strings.Contains(msg.Content, "accepted task-") || !strings.Contains(msg.Content, "(running)") ||
		strings.Contains(msg.Content, settled) || strings.Contains(msg.Content, "the call was answered with") {
		t.Errorf("paired tool message = %+v, want the admission receipt of %q paired under tool %q beside no settled body", msg, callID, tool)
	}
}

// checkObservation checks one settled observation against the call it settles:
// the settled body enters the request as its own observation under the call
// identity it belongs to, beside no wrapper prefix and never as a second
// synchronous tool response under that identity.
func checkObservation(t *testing.T, msg model.Message, callID, tool, settled string) {
	t.Helper()
	if !strings.Contains(msg.Content, settled) || msg.ToolID != callID || msg.ToolName != tool ||
		msg.Role == model.RoleTool || strings.Contains(msg.Content, "the call was answered with") {
		t.Errorf("settled observation = %+v, want the settled body of %q carrying %q as its own observation", msg, callID, settled)
	}
}

// ntCall builds one native tool call of a scripted assistant message.
func ntCall(id, name, args string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      name,
			Arguments: []byte(args),
		},
	}
}

// ntToolTurn builds the scripted terminal response carrying native tool calls.
func ntToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Done:    true,
	}
}

// ntFinalTurn builds the scripted terminal response carrying the final answer.
func ntFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}
