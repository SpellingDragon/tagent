package execution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task/approval"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// longContextChars is the ready input the over-threshold cases open on:
// 240,000 bytes of message content, which the fuse reads as 60,000 prompt
// tokens — past the documented threshold and past the size the victim degraded
// at, in the shape that costs the assembly nothing but memory.
const longContextChars = 240_000

// TestLongContextFuse 钉住长上下文防线：组装后的请求超过阈值时，该次响应的调用整批挂审批，按裁决整批执行或整批不执行。
// - approval_admits_the_batch_over_the_threshold：60K 近似 token 的会话上模型发 3 个并行调用 → 恰 1 项审批（rule 为 long-context）；批准后 3 条受理回执 + 1 条批次回执。
// - refusal_runs_nothing_over_the_threshold：拒绝时 3 条拒绝回执、零派发。
// - expiry_runs_nothing_over_the_threshold：短有效期无裁决 → 3 条未执行回执、零派发。
// - threshold_off_admits_every_call：阈值 0（关闭态）时 3 个调用全部受理且不登记审批。
// - below_threshold_admits_every_call：阈值高于会话大小时 3 个调用全部受理且不登记审批。
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestLongContextFuse(t *testing.T) {
	t.Run("approval_admits_the_batch_over_the_threshold", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{LongContextTokens: agent.DefaultLongContextTokens})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := longContextRig(t, "session-long")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, longContextRequest(addr, longContextChars))
		req := awaitBatchRequest(t, book, dir)
		if req.RuleID != "long-context" {
			t.Fatalf("approval rule = %q, want the long-context threshold that held the response", req.RuleID)
		}
		for _, want := range []string{`"calls":3`, fmt.Sprintf(`"threshold":%d`, agent.DefaultLongContextTokens), `"prompt_tokens"`} {
			if !strings.Contains(req.ArgsPreview, want) {
				t.Fatalf("approval preview = %q, want the summary head %s an approver reads", req.ArgsPreview, want)
			}
		}
		decideBatch(t, book, req.ID, approval.ApprovalApproved)
		awaitAsync(t, errs)
		if got := tool.calls(); got != 3 {
			t.Fatalf("dispatched calls of the approved batch = %d, want every call of the response", got)
		}
		if tokens := longContextRequestTokens(t, provider, 0); tokens <= agent.DefaultLongContextTokens {
			t.Fatalf("approx prompt tokens of the opening request = %d, want the session above the %d threshold the case names", tokens, agent.DefaultLongContextTokens)
		}
		receipts, admissions, others := toolReceipts(provider.recorded()[1].Messages)
		if receipts != 3 || admissions != 3 {
			t.Fatalf("tool receipts = %d (admissions %d, others %v), want one admission per call of the approved batch", receipts, admissions, others)
		}
		notice := batchNoticeIn(provider.recorded()[1].Messages)
		for _, want := range []string{"3", "approved", req.ID} {
			if !strings.Contains(notice, want) {
				t.Fatalf("batch receipt = %q, want the batch size, the approval and the request identity named", notice)
			}
		}
		if registered := registeredBatches(t, dir); len(registered) != 1 {
			t.Fatalf("registered approval requests = %d (%v), want exactly one for the whole response", len(registered), registered)
		}
	})

	t.Run("refusal_runs_nothing_over_the_threshold", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{LongContextTokens: agent.DefaultLongContextTokens})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := longContextRig(t, "session-long-refused")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, longContextRequest(addr, longContextChars))
		req := awaitBatchRequest(t, book, dir)
		decideBatch(t, book, req.ID, approval.ApprovalDenied)
		awaitAsync(t, errs)
		if got := tool.calls(); got != 0 {
			t.Fatalf("dispatched calls of a refused batch = %d, want none", got)
		}
		receipts, admissions, others := toolReceipts(provider.recorded()[1].Messages)
		if receipts != 3 || admissions != 0 {
			t.Fatalf("tool receipts = %d (admissions %d), want one refusal per call and no admission", receipts, admissions)
		}
		for _, text := range others {
			if !strings.Contains(text, "refused this batch") {
				t.Fatalf("refusal receipt = %q, want every call of the refused batch answered with its ruling", text)
			}
		}
	})

	t.Run("expiry_runs_nothing_over_the_threshold", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{LongContextTokens: agent.DefaultLongContextTokens})
		book, dir := approvalBook(t, 100*time.Millisecond)
		run, provider, tool, addr := longContextRig(t, "session-long-expired")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		errs := invokeAsync(ctx, run, longContextRequest(addr, longContextChars))
		awaitBatchRequest(t, book, dir)
		awaitAsync(t, errs)
		if got := tool.calls(); got != 0 {
			t.Fatalf("dispatched calls of an expired batch = %d, want none", got)
		}
		receipts, admissions, others := toolReceipts(provider.recorded()[1].Messages)
		if receipts != 3 || admissions != 0 {
			t.Fatalf("tool receipts = %d (admissions %d), want one not-executed receipt per call", receipts, admissions)
		}
		for _, text := range others {
			if !strings.Contains(text, "expired without a ruling") {
				t.Fatalf("expiry receipt = %q, want every call of the expired batch told its request outlived its validity", text)
			}
		}
	})

	t.Run("threshold_off_admits_every_call", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{LongContextTokens: 0})
		book, dir := approvalBook(t, 0)
		run, provider, tool, addr := longContextRig(t, "session-long-off")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if _, err := run.Invoke(ctx, longContextRequest(addr, longContextChars)); err != nil {
			t.Fatalf("invoke the long-context range beside the fuse off: %v", err)
		}
		if got := tool.calls(); got != 3 {
			t.Fatalf("dispatched calls beside the fuse off = %d, want every call of the response", got)
		}
		if tokens := longContextRequestTokens(t, provider, 0); tokens <= agent.DefaultLongContextTokens {
			t.Fatalf("approx prompt tokens of the opening request = %d, want the same over-threshold session the held case runs", tokens)
		}
		receipts, admissions, others := toolReceipts(provider.recorded()[1].Messages)
		if receipts != 3 || admissions != 3 {
			t.Fatalf("receipts = %d (admissions %d, others %v), want every call admitted beside the fuse off", receipts, admissions, others)
		}
		if pending := book.Pending(); len(pending) != 0 {
			t.Fatalf("pending approvals beside the fuse off = %d, want the fuse off to park nothing", len(pending))
		}
		if registered := registeredBatches(t, dir); len(registered) != 0 {
			t.Fatalf("registered approval requests beside the fuse off = %d (%v), want none", len(registered), registered)
		}
	})

	t.Run("below_threshold_admits_every_call", func(t *testing.T) {
		fuseLimits(t, agent.FuseLimits{LongContextTokens: 4 * longContextChars})
		_, dir := approvalBook(t, 0)
		run, provider, tool, addr := longContextRig(t, "session-long-below")
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if _, err := run.Invoke(ctx, longContextRequest(addr, 2_000)); err != nil {
			t.Fatalf("invoke the short range under the threshold: %v", err)
		}
		if got := tool.calls(); got != 3 {
			t.Fatalf("dispatched calls below the threshold = %d, want every call of the response", got)
		}
		receipts, admissions, others := toolReceipts(provider.recorded()[1].Messages)
		if receipts != 3 || admissions != 3 {
			t.Fatalf("receipts = %d (admissions %d, others %v), want every call admitted below the threshold", receipts, admissions, others)
		}
		if registered := registeredBatches(t, dir); len(registered) != 0 {
			t.Fatalf("registered approval requests below the threshold = %d (%v), want none", len(registered), registered)
		}
	})
}

// longContextRig assembles one fuse runtime whose agent answers the first
// request with three parallel calls of its one tool.
func longContextRig(t *testing.T, session string) (*assembly.Run, *convModel, *convTool, event.Address) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	tool := &convTool{name: "send", output: "sent"}
	provider := &convModel{name: "model-long", answer: func(req *model.Request) *model.Response {
		if len(req.Messages) == 1 {
			return convToolTurn(
				convCall("call-long-1", "send", `{"n":1}`),
				convCall("call-long-2", "send", `{"n":2}`),
				convCall("call-long-3", "send", `{"n":3}`),
			)
		}
		return convFinalTurn("done")
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: provider.name}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the long-context runtime: %v", err)
	}
	return run, provider, tool, event.Address{AgentID: "agent-a", SessionID: event.SessionID(session)}
}

// longContextRequest builds the invocation of one long-context range: the
// ready input carries the named size of transcript material.
func longContextRequest(addr event.Address, chars int) assembly.InvokeRequest {
	return assembly.InvokeRequest{
		InvocationID: event.InvocationID(fmt.Sprintf("inv-long-%d", chars)),
		Address:      addr,
		Input:        longContextInput(chars),
	}
}

// longContextInput builds the ready input of the long-context range: one user
// message carrying the named size of transcript material.
func longContextInput(chars int) []model.Message {
	return []model.Message{{Role: model.RoleUser, Content: strings.Repeat("context ", chars/8)}}
}

// longContextRequestTokens approximates the prompt size of the named recorded
// request the way the fuse reads it: the bytes of its messages over four.
func longContextRequestTokens(t *testing.T, provider *convModel, at int) int {
	t.Helper()
	requests := provider.recorded()
	if len(requests) <= at {
		t.Fatalf("recorded requests = %d, want the round at index %d", len(requests), at)
	}
	bytes := 0
	for _, msg := range requests[at].Messages {
		bytes += len(msg.Content) + len(msg.ToolID) + len(msg.ToolName)
		for _, call := range msg.ToolCalls {
			bytes += len(call.ID) + len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	return bytes / 4
}
