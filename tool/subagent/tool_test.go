// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestSubagentToolDeclaration holds the tool contract the model reads and the refusals a call answers with:
// - the description names the agents file source and every explicit parameter, as two mutually exclusive sources
// - the input schema declares exactly the argument keys of both sources beside the request input
// - both sources at once, a failed assembly, an unreadable file, an undeclared argument and a refused delegation answer a named denial beside no task
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
	})

	t.Run("the input schema declares both sources", func(t *testing.T) {
		schema := tool.Declaration().InputSchema
		want := []string{"agents_file", "name", "description", "model", "prompt", "skills", "tools", "mcp", "input"}
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
		out := callTool(t, tool, map[string]any{"name": "reviewer", "temperature": 0.2})
		requireDenial(t, out, ErrArguments)
		if got := delegation.calls(); got != 0 {
			t.Fatalf("delegations = %d, want no task from an undeclared argument", got)
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
