// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestSubagentToolDeclaration holds the tool contract the model reads and the refusals a call answers with:
// - the description names the agents file source and every explicit parameter, as two mutually exclusive sources
// - the description states the bare ok receipt and promises no task identity or progress delivery
// - the input schema declares exactly the argument keys of both sources beside the request input and the structured handoff
// - both sources at once, a failed assembly, an unreadable file, an undeclared argument and a refused delegation answer a named denial beside no task
// - a declared handoff is admitted beside its request, while a handoff naming no intent or an undeclared key answers a named denial beside no task
// - a call carrying no admission identity and a second admission of the same call are answered without a second turn
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestSubagentToolDeclaration(t *testing.T) {
	tool := newToolForDeclarationTest(t, &stubDelegation{})

	t.Run("the description names both assembly sources", func(t *testing.T) {
		declaration := tool.Declaration()
		if declaration.Name != ToolName {
			t.Fatalf("declaration name = %q, want %q", declaration.Name, ToolName)
		}
		for _, source := range []string{"agents_file", "prompt", "model", "skills", "tools", "mcp"} {
			if !strings.Contains(declaration.Description, source) {
				t.Fatalf("description names no %q source: %q", source, declaration.Description)
			}
		}
		if !strings.Contains(declaration.Description, "exactly one assembly source") {
			t.Fatalf("description does not state the sources are exclusive: %q", declaration.Description)
		}
		for _, segment := range []string{"structured handoff", "intent", "acceptance constraints"} {
			if !strings.Contains(declaration.Description, segment) {
				t.Fatalf("description names no %q of the handoff: %q", segment, declaration.Description)
			}
		}
	})

	t.Run("the description promises no receipt payload", func(t *testing.T) {
		description := tool.Declaration().Description
		if !strings.Contains(description, "bare ok") {
			t.Fatalf("description = %q, want it to state the bare ok receipt", description)
		}
		for _, promise := range []string{"task identity", "carries the task", "progress"} {
			if strings.Contains(description, promise) {
				t.Fatalf("description = %q, want no %q promise the bare ok does not keep", description, promise)
			}
		}
	})

	t.Run("the input schema declares both sources", func(t *testing.T) {
		schema := tool.Declaration().InputSchema
		want := []string{"agents_file", "name", "description", "model", "prompt", "skills", "tools", "mcp",
			"temperature", "thinking_enabled", "reasoning_effort", "input", "handoff"}
		if len(schema.Properties) != len(want) {
			t.Fatalf("schema properties = %d, want the %d declared arguments", len(schema.Properties), len(want))
		}
		for _, key := range want {
			if schema.Properties[key] == nil {
				t.Fatalf("schema declares no %q argument", key)
			}
		}
		for _, key := range []string{"skills", "tools", "mcp"} {
			if schema.Properties[key].Type != "array" || schema.Properties[key].Items == nil {
				t.Fatalf("schema argument %q = %+v, want a string array", key, schema.Properties[key])
			}
		}
		for key, typ := range map[string]string{"temperature": "number", "thinking_enabled": "boolean", "reasoning_effort": "string"} {
			if got := schema.Properties[key].Type; got != typ {
				t.Fatalf("schema argument %q = %q, want the declared %q sampling", key, got, typ)
			}
		}
		if schema.Properties["handoff"].Type != "object" {
			t.Fatalf("schema argument %q = %+v, want the structured handoff object", "handoff", schema.Properties["handoff"])
		}
	})

	t.Run("a call naming both sources is refused", func(t *testing.T) {
		delegation := &stubDelegation{}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{"agents_file": "reviewer.md", "name": "reviewer"})
		requireDenial(t, out, ErrSourceConflict)
		if got := delegation.calls(); got != 0 {
			t.Fatalf("delegations = %d, want no task from a declaration two sources name", got)
		}
	})

	t.Run("an assembly that fails is refused", func(t *testing.T) {
		delegation := &stubDelegation{}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{"prompt": "review the change"})
		requireDenial(t, out, ErrNameMissing)
		if got := delegation.calls(); got != 0 {
			t.Fatalf("delegations = %d, want no task from an assembly that failed", got)
		}
	})

	t.Run("an unreadable agents file is refused", func(t *testing.T) {
		delegation := &stubDelegation{}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{"agents_file": "no-such-agents-file.md"})
		requireDenial(t, out, ErrFileUnreadable)
		if got := delegation.calls(); got != 0 {
			t.Fatalf("delegations = %d, want no task from an unreadable file", got)
		}
	})

	t.Run("an argument the declaration does not name is refused", func(t *testing.T) {
		delegation := &stubDelegation{}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{"name": "reviewer", "max_tokens": 256})
		requireDenial(t, out, ErrArguments)
		if got := delegation.calls(); got != 0 {
			t.Fatalf("delegations = %d, want no task from an undeclared argument", got)
		}
	})

	t.Run("a declared handoff is admitted beside its request", func(t *testing.T) {
		delegation := &stubDelegation{}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{
			"name":   "reviewer",
			"prompt": "review the change",
			"handoff": map[string]any{
				"intent":      "review the change",
				"constraints": []string{"every finding names a file"},
			},
		})
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call result = %#v, want the admission receipt of the declared handoff", out)
		}
		if got := delegation.calls(); got != 1 {
			t.Fatalf("delegations = %d, want the one task the declared handoff admitted", got)
		}
	})

	t.Run("a handoff naming no intent is refused", func(t *testing.T) {
		delegation := &stubDelegation{}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{
			"name":    "reviewer",
			"prompt":  "review the change",
			"handoff": map[string]any{"constraints": []string{"every finding names a file"}},
		})
		requireDenial(t, out, agent.ErrHandoffIntentMissing)
		if got := delegation.calls(); got != 0 {
			t.Fatalf("delegations = %d, want no task from a handoff naming no intent", got)
		}
	})

	t.Run("a handoff key the declaration does not name is refused", func(t *testing.T) {
		delegation := &stubDelegation{}
		tool := newToolForDeclarationTest(t, delegation)
		out, err := tool.Call(task.WithCallFrame(context.Background(), task.CallFrame{
			Identity: task.CallIdentity{InvocationID: "inv-caller", DecisionID: "decision-caller", CallID: "call-review"},
			Address:  event.Address{AgentID: "agent-caller", SessionID: "session-caller"},
		}), []byte(`{"name":"reviewer","handoff":{"intent":"review","budget":"2"}}`))
		if err != nil {
			t.Fatalf("call the tool = %v, want a refusal result", err)
		}
		requireDenial(t, out, ErrHandoffArguments)
		if got := delegation.calls(); got != 0 {
			t.Fatalf("delegations = %d, want no task from an undeclared handoff key", got)
		}
	})

	t.Run("a refused delegation is refused by name", func(t *testing.T) {
		delegation := &stubDelegation{err: agent.ErrDelegationAdmissionRefused}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"})
		requireDenial(t, out, agent.ErrDelegationAdmissionRefused)
	})

	t.Run("a call carrying no admission identity is a protocol fault", func(t *testing.T) {
		tool := newToolForDeclarationTest(t, &stubDelegation{})
		_, err := tool.Call(context.Background(), []byte(`{"name":"reviewer"}`))
		if err == nil || !strings.Contains(err.Error(), task.ErrCallFrameMissing.Error()) {
			t.Fatalf("call without a frame = %v, want the named protocol fault", err)
		}
	})

	t.Run("a second admission of the same call starts no second turn", func(t *testing.T) {
		delegation := &stubDelegation{receipt: task.AdmissionReceipt{
			CallID:   "call-review",
			TaskID:   "task-review",
			Accepted: true,
			Kind:     task.AdmissionDedup,
		}}
		tool := newToolForDeclarationTest(t, delegation)
		out := callTool(t, tool, map[string]any{"agents_file": writeReviewerAgentsFile(t)})
		receipt, ok := out.(task.AdmissionReceipt)
		if !ok {
			t.Fatalf("call result = %#v, want the admission receipt", out)
		}
		if receipt.TaskID != "task-review" || receipt.Kind != task.AdmissionDedup {
			t.Fatalf("receipt = %+v, want the dedup admission of the first call", receipt)
		}
		if got := delegation.completions(); got != 0 {
			t.Fatalf("terminals reported = %d, want none from a dedup admission", got)
		}
	})
}

// TestCallSubagentSamplingOverride drives the sampling a call names onto the delegated turn:
// - the temperature, thinking switch and reasoning effort an explicit source names reach the request of the sub-agent's turn
// - a call naming no sampling injects none of the three, so the model default stands
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestCallSubagentSamplingOverride(t *testing.T) {
	t.Run("the sampling an explicit source names reaches the delegated request", func(t *testing.T) {
		provider := &serveModel{name: "model-caller", answer: "review recorded"}
		tool := newSamplingTool(t, &stubDelegation{}, provider)
		out := callTool(t, tool, map[string]any{
			"name":             "reviewer",
			"prompt":           "review the change",
			"temperature":      0.1,
			"thinking_enabled": true,
			"reasoning_effort": "low",
		})
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call result = %#v, want the admission receipt of the sampled call", out)
		}
		request := awaitSampledRequest(t, provider)
		if request.Temperature == nil || *request.Temperature != 0.1 {
			t.Fatalf("delegated request temperature = %v, want the 0.1 the call named", request.Temperature)
		}
		if request.ThinkingEnabled == nil || !*request.ThinkingEnabled {
			t.Fatalf("delegated request thinking switch = %v, want the true the call named", request.ThinkingEnabled)
		}
		if request.ReasoningEffort == nil || *request.ReasoningEffort != "low" {
			t.Fatalf("delegated request reasoning effort = %v, want the low the call named", request.ReasoningEffort)
		}
	})

	t.Run("a call naming no sampling injects none", func(t *testing.T) {
		provider := &serveModel{name: "model-caller", answer: "review recorded"}
		tool := newSamplingTool(t, &stubDelegation{}, provider)
		out := callTool(t, tool, map[string]any{"name": "reviewer", "prompt": "review the change"})
		if _, ok := out.(task.AdmissionReceipt); !ok {
			t.Fatalf("call result = %#v, want the admission receipt of the bare call", out)
		}
		request := awaitSampledRequest(t, provider)
		if request.Temperature != nil {
			t.Fatalf("delegated request temperature = %v, want no key beside a call naming none", *request.Temperature)
		}
		if request.ThinkingEnabled != nil {
			t.Fatalf("delegated request thinking switch = %v, want no key beside a call naming none", *request.ThinkingEnabled)
		}
		if request.ReasoningEffort != nil {
			t.Fatalf("delegated request reasoning effort = %v, want no key beside a call naming none", *request.ReasoningEffort)
		}
	})
}

// TestTargetDeclarationPromisesTheBareReceipt pins the model face of one declared agent tool to the receipt an admission answers with:
// - the description states the bare ok receipt
// - it promises no task identity and no progress delivery
// - it names the terminal notification as the follow-up material
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestTargetDeclarationPromisesTheBareReceipt(t *testing.T) {
	target, err := NewTarget(TargetOptions{
		Name:        "recall",
		Description: "reads the memory this run holds",
		Assembly:    &Assembly{Definition: Definition{Name: "recall", Model: "model-recall"}},
		Delegation:  &stubDelegation{},
		Bindings:    state.NewBindings(),
	})
	if err != nil {
		t.Fatalf("construct the target tool: %v", err)
	}
	description := target.Declaration().Description
	if !strings.Contains(description, "bare ok") {
		t.Fatalf("description = %q, want it to state the bare ok receipt", description)
	}
	for _, promise := range []string{"task identity", "carries the task", "progress"} {
		if strings.Contains(description, promise) {
			t.Fatalf("description = %q, want no %q promise the bare ok does not keep", description, promise)
		}
	}
	if !strings.Contains(description, "terminal") || !strings.Contains(description, "notification") {
		t.Fatalf("description = %q, want it to name the terminal notification", description)
	}
}

// newSamplingTool returns the tool over one delegation stand-in, the model
// stand-in its builder resolves and the real borrowing face of a held generation.
func newSamplingTool(t *testing.T, delegation *stubDelegation, provider *serveModel) *Tool {
	t.Helper()
	bindings := state.NewBindings()
	holdBinding(t, bindings, event.Address{AgentID: "agent-caller", SessionID: "session-caller"})
	tool, err := NewTool(ToolOptions{
		Parent: &Assembly{Definition: Definition{Description: "the caller", Model: "model-caller"}},
		Builder: Builder{Model: func(context.Context, string) (trpcmodel.Model, error) {
			return provider, nil
		}},
		Delegation: delegation,
		Bindings:   bindings,
	})
	if err != nil {
		t.Fatalf("construct the call_subagent tool: %v", err)
	}
	return tool
}

// awaitSampledRequest waits for the request of the delegated turn and reports it:
// the turn runs beside the admission, so the check waits on the model it drives.
func awaitSampledRequest(t *testing.T, provider *serveModel) trpcmodel.Request {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if requests := provider.recorded(); len(requests) > 0 {
			return requests[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the delegated turn issued no request before the deadline")
	return trpcmodel.Request{}
}

// newToolForDeclarationTest returns the tool over one delegation stand-in and the real
// borrowing face of a held generation.
func newToolForDeclarationTest(t *testing.T, delegation *stubDelegation) *Tool {
	t.Helper()
	bindings := state.NewBindings()
	holdBinding(t, bindings, event.Address{AgentID: "agent-caller", SessionID: "session-caller"})
	tool, err := NewTool(ToolOptions{
		Parent:     &Assembly{Definition: Definition{Description: "the caller", Model: "model-caller"}},
		Delegation: delegation,
		Bindings:   bindings,
	})
	if err != nil {
		t.Fatalf("construct the call_subagent tool: %v", err)
	}
	return tool
}

// callTool drives one call of the tool under the frame of a decision that issued it.
func callTool(t *testing.T, tool *Tool, args map[string]any) any {
	t.Helper()
	ctx := task.WithCallFrame(context.Background(), task.CallFrame{
		Identity: task.CallIdentity{
			InstanceID:   "instance-subagent",
			InvocationID: "inv-caller",
			DecisionID:   "decision-caller",
			CallID:       "call-review",
		},
		Address:   event.Address{AgentID: "agent-caller", SessionID: "session-caller"},
		Execution: "session-serialized",
	})
	encoded, err := jsonArgs(args)
	if err != nil {
		t.Fatalf("encode the call arguments: %v", err)
	}
	out, err := tool.Call(ctx, encoded)
	if err != nil {
		t.Fatalf("call the tool = %v, want a result", err)
	}
	return out
}

// requireDenial reports whether one call answered the named refusal beside no task.
func requireDenial(t *testing.T, out any, want error) {
	t.Helper()
	text, ok := out.(string)
	if !ok {
		t.Fatalf("call result = %#v, want the refusal text", out)
	}
	if !strings.HasPrefix(text, deniedMarker) {
		t.Fatalf("refusal text = %q, want the %q marker", text, deniedMarker)
	}
	if !strings.Contains(text, want.Error()) {
		t.Fatalf("refusal text = %q, want the named reason %q", text, want.Error())
	}
	if !strings.Contains(text, "no task was admitted") {
		t.Fatalf("refusal text = %q, want the statement that no task was admitted", text)
	}
}

// holdBinding holds one candidate generation for the given addresses on the holder.
func holdBinding(t *testing.T, bindings *state.Bindings, addresses ...event.Address) {
	t.Helper()
	specs := make([]config.AgentSpec, 0, len(addresses))
	held := make(map[event.AgentID]state.AgentBinding, len(addresses))
	for _, addr := range addresses {
		specs = append(specs, config.AgentSpec{ID: string(addr.AgentID), Model: "model-" + string(addr.AgentID)})
		held[addr.AgentID] = state.AgentBinding{
			Model:        &serveModel{name: "model-" + string(addr.AgentID)},
			PromptSource: state.PromptSource{Version: "prompt-subagent-v1"},
		}
	}
	plan, err := state.NewStatePlan(state.OrgPlan{
		Declaration: &config.Declaration{Entry: string(addresses[0].AgentID), Agents: specs},
	}, held, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
}

// stubDelegation is the delegation stand-in of the declaration test: it records every
// admission and every terminal it was asked to report.
type stubDelegation struct {
	mu          sync.Mutex
	requests    []agent.Delegation
	receipt     task.AdmissionReceipt
	err         error
	terminal    []task.Status
	terminalErr error
}

// Delegate records the admission request and reports the scripted receipt.
func (s *stubDelegation) Delegate(_ context.Context, req agent.Delegation) (task.AdmissionReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	return s.receipt, s.err
}

// Complete records the terminal the tool asked to report.
func (s *stubDelegation) Complete(_ context.Context, _ event.TaskID, status task.Status, _ event.EventID) (task.Notice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminal = append(s.terminal, status)
	return task.Notice{}, s.terminalErr
}

// calls reports how often the tool asked for an admission.
func (s *stubDelegation) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// completions reports how often the tool asked to report a terminal.
func (s *stubDelegation) completions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.terminal)
}

// jsonArgs encodes one call's arguments.
func jsonArgs(args map[string]any) ([]byte, error) {
	return json.Marshal(args)
}

// writeReviewerAgentsFile writes one agents file declaring a reviewer sub-agent.
func writeReviewerAgentsFile(t *testing.T) string {
	t.Helper()
	return writeAgentsFile(t, t.TempDir(), "reviewer.md", `---
name: reviewer
description: reviews one change
model: model-reviewer
tools: [exec_run]
---
Review the change and report findings.
`)
}

// The tool answers the native callable tool port.
var _ trpctool.CallableTool = (*Tool)(nil)
