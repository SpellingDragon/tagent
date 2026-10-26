package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestQuietReceipts 钉住一次受理只回最小确认，业务需注意的等待仍带自己的措辞。
// - admission_receipt_is_minimal_ok：正式派发路径的下一轮请求里，与本调用配对的 tool 消息恰为 "ok"，结算正文只作自身观察、不充当第二份 tool 答案。
// - hold_receipt_keeps_business_wording：扣留的调用仍回 "tagent: the call is parked awaiting approval"，静默不波及审批等待。
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestQuietReceipts(t *testing.T) {
	t.Run("admission_receipt_is_minimal_ok", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tool := &convTool{name: "quiet-tool", output: "quiet-body"}
		provider := &convModel{name: "model-quiet", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-quiet-1", "quiet-tool", `{}`))
			}
			return convFinalTurn("done")
		}}
		decl := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-quiet"}},
		}
		run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the quiet runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-quiet"}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-quiet-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "please delegate"}},
		}); err != nil {
			t.Fatalf("invoke the quiet range: %v", err)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("recorded requests = %d, want the call round beside the round that reads its answer", len(requests))
		}
		answers, settled := 0, 0
		var receipt model.Message
		for _, msg := range requests[1].Messages {
			if msg.ToolID != "call-quiet-1" {
				continue
			}
			if msg.Role == model.RoleTool {
				answers++
				receipt = msg
				continue
			}
			settled++
		}
		if answers != 1 {
			t.Fatalf("tool answers under call-quiet-1 = %d, want the single admission receipt", answers)
		}
		if receipt.ToolName != "quiet-tool" || receipt.Content != "ok" {
			t.Fatalf("admission receipt = %+v, want the minimal ok paired under the tool that was called", receipt)
		}
		for _, word := range []string{"task-", "(running)", "background task", "notification", "accepted "} {
			if strings.Contains(receipt.Content, word) {
				t.Errorf("admission receipt = %q, want nothing of the admission beside the confirmation: %q is speaking material the model repeats", receipt.Content, word)
			}
		}
		if settled != 1 {
			t.Fatalf("settled observations under call-quiet-1 = %d, want the settled body carried beside the receipt rather than as a second answer", settled)
		}
	})

	t.Run("hold_receipt_keeps_business_wording", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tool := &convTool{name: "held-tool", output: "held-body"}
		provider := &convModel{name: "model-held", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-quiet-2", "held-tool", `{}`))
			}
			return convFinalTurn("done")
		}}
		decl := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-held"}},
		}
		approve := func(context.Context, string, []byte) (task.Ruling, error) {
			return task.Ruling{Kind: task.RulingHold, Reason: "waiting for approval"}, nil
		}
		run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithApprover(approve))
		if err != nil {
			t.Fatalf("assemble the held runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-held"}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-quiet-2",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "please delegate"}},
		}); err != nil {
			t.Fatalf("invoke the held range: %v", err)
		}
		if got := tool.calls(); got != 0 {
			t.Fatalf("held executions = %d, want the parked call to execute nothing", got)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("recorded requests = %d, want the call round beside the round that reads its answer", len(requests))
		}
		answers := 0
		var receipt model.Message
		for _, msg := range requests[1].Messages {
			if msg.ToolID == "call-quiet-2" && msg.Role == model.RoleTool {
				answers++
				receipt = msg
			}
		}
		if answers != 1 {
			t.Fatalf("tool answers under call-quiet-2 = %d, want the single waiting receipt", answers)
		}
		if receipt.ToolName != "held-tool" || receipt.Content != "tagent: the call is parked awaiting approval" {
			t.Fatalf("waiting receipt = %+v, want the approval wait keeping its business wording beside the minimal confirmations", receipt)
		}
	})
}
