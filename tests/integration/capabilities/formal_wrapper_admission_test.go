// Package capabilities exercises the native capability surfaces of the formal
// runtime — the file, skill, MCP and function tool families and the governance
// that wraps them — through the public entries of the module root.
package capabilities

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// passPolicy is the deterministic stand-in of the real governance policy the
// formal assembly is configured with in these checks: the fixed passing
// dispositions are mechanism inputs carrying no quality conclusion, and the
// formal quality assessment lives in the structural evaluation gate.
type passPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (passPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       passingCheck("eval"),
		"benchmark":  passingCheck("benchmark"),
		"apply-gate": passingCheck("apply-gate"),
	}, nil
}

// passingCheck reports the passing disposition of one named mechanism check.
func passingCheck(name state.CheckName) state.CheckAnswer {
	return state.CheckAnswer{Name: name, Verdict: state.CheckPass, Reason: "the stand-in passes the mechanism check", Scope: "assembly", Evidence: []string{"deterministic stand-in"}}
}

// TestFormalWrapperAdmission drives the unified admission wrap of the formal assembly and checks the pre-execution contract of one call.
//   - beside no approver the strict grade refuses a call before any execution and registers no task (the deny path is shared across every family)
//   - beside an approver every family registers a real task, answers its call with the real admission receipt and settles its result as its own observation
//
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
func TestFormalWrapperAdmission(t *testing.T) {
	t.Run("the strict grade beside no approver refuses a call before any execution", func(t *testing.T) {
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
				return ntToolTurn(ntCall("call-save", "save_file", `{"file_name":"probe.txt","contents":"probe\n","overwrite":true}`))
			}
			if msg := req.Messages[0]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "system prompt") {
				t.Errorf("second request prompt body = %+v, want the prompt file fixed at the request boundary", msg)
			}
			if msg := req.Messages[1]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "body v1") {
				t.Errorf("second request skill body = %+v, want the skill file fixed at the request boundary", msg)
			}
			checkPair(t, req.Messages[4], "call-save", "save_file", task.ErrDenied.Error())
			checkPair(t, req.Messages[4], "call-save", "save_file", "strict enforcement admits no call without an approver")
			if strings.Contains(req.Messages[4].Content, "Successfully saved") {
				t.Errorf("paired answer = %q, want no settled result beside a call refused before execution", req.Messages[4].Content)
			}
			return ntFinalTurn("settled")
		}}
		run, err := assembly.Assemble(ctx, declarationOf(root), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithEnforcement(governance.EnforcementStrict))
		if err != nil {
			t.Fatalf("assemble the strict runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-strict"}
		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-strict",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the strict runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if _, err := os.Stat(filepath.Join(root, "probe.txt")); !os.IsNotExist(err) {
			t.Fatalf("probe file stat = %v, want the call refused before execution to write nothing", err)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}
		reading, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the strict scene: %v", err)
		}
		for _, detail := range reading.Details {
			if detail.Ref.Kind == query.ObjectTask {
				t.Fatalf("task detail = %+v, want no task registered for a call refused before execution", detail.Ref)
			}
		}
	})

	t.Run("beside an approver every family answers its call with the real admission receipt", func(t *testing.T) {
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
		ruled := 0
		approve := func(_ context.Context, _ string, _ []byte) (task.Ruling, error) {
			mu.Lock()
			ruled++
			mu.Unlock()
			return task.Ruling{Kind: task.RulingRun}, nil
		}
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
			if len(req.Messages) != 10 {
				t.Errorf("ruled request messages = %d (%+v), want the fixed file bodies, user, assistant, one admission receipt and one settled observation per call", len(req.Messages), req.Messages)
			} else {
				if msg := req.Messages[0]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "system prompt") {
					t.Errorf("ruled request prompt body = %+v, want the prompt file fixed at the request boundary", msg)
				}
				if msg := req.Messages[1]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "body v1") {
					t.Errorf("ruled request skill body = %+v, want the skill file fixed at the request boundary", msg)
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
		run, err := assembly.Assemble(ctx, declarationOf(root), map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{fnTool}},
		}, assembly.WithGovernance(passPolicy{}), assembly.WithEnforcement(governance.EnforcementStrict), assembly.WithApprover(approve))
		if err != nil {
			t.Fatalf("assemble the ruled runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-ruled"}
		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-ruled",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the ruled runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		mu.Lock()
		gotRuled := ruled
		mu.Unlock()
		if gotRuled != 3 {
			t.Fatalf("approver consultations = %d, want one ruling per admitted call", gotRuled)
		}
		fnMu.Lock()
		gotFn := fnCalls
		fnMu.Unlock()
		if gotFn != 1 {
			t.Fatalf("function tool executions = %d, want the admitted call executed exactly once", gotFn)
		}
		probe, err := os.ReadFile(filepath.Join(root, "probe.txt"))
		if err != nil || string(probe) != "probe\n" {
			t.Fatalf("probe file = (%q, %v), want the admitted file call wrote the real file", probe, err)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}
		reading, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
		if err != nil {
			t.Fatalf("query the admitted tasks: %v", err)
		}
		taskIDs := map[event.TaskID]bool{}
		for _, detail := range reading.Details {
			taskIDs[detail.Ref.TaskID] = true
		}
		if len(taskIDs) != 3 {
			t.Fatalf("registered tasks = %v, want one task per admitted call", taskIDs)
		}
	})
}
