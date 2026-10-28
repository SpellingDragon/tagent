// J6 is the approval journey: the admission ruling grades one tool critical,
// the model calls it, and the call parks in the approval wait instead of
// running. An operator approves the held arguments through the approval
// manager, the model retries the same call in a second user turn, and the tool
// runs beside the answer the user receives.
//
// The journey takes the production seat: the approver is the admission ruling
// the host attaches to every wrapped call, the approval manager is the same
// waiting book an operator entry answers, and the retry is a second
// Host.Invoke — the approval decision is never injected into a wrapper. Boot
// forwards the configured policy and enforcement grade onto this same
// assembly beside the approver, so the ruling seat is always the one the
// document named.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package journey

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// TestJourneyApprovalHoldThenRun drives one held call and its approved retry:
// - the first turn's critical call parks unrun, and the model reads the park receipt
// - the waiting answer reaches the user, and the held arguments wait under the critical rule
// - approving and retrying runs the same call, so the tool body and final answer arrive
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestJourneyApprovalHoldThenRun(t *testing.T) {
	const (
		session   = "session-journey-approval"
		toolBody  = "the approved tool ran and reached the user"
		heldBody  = "the critical call is waiting for approval"
		finalBody = "the approved call ran to completion"
		callArgs  = `{"content":"the approved tool ran and reached the user"}`
	)
	manager := approval.NewApprovalManager("", 0, approval.WithApprovalRescanInterval(0))
	approve := journeyCriticalRuling(manager)
	provider := newJourneyProvider(t)
	provider.script(journeyEntryModel, journeyReply{body: journeyToolCall("call-send-1", "send", callArgs)})
	provider.scriptAnswers(journeyEntryModel, heldBody)
	provider.script(journeyEntryModel, journeyReply{body: journeyToolCall("call-send-2", "send", callArgs)})
	provider.scriptAnswers(journeyEntryModel, finalBody)
	host := bootJourney(t, provider,
		journeySpec{EntryTools: []string{"send"}, MaxTokens: 4096},
		assembly.WithApprover(approve),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	host.invokeOK(t, ctx, session, "please run the critical tool")

	pending := waitForPendingApproval(t, manager)
	if pending.ToolName != "send" {
		t.Fatalf("pending approval tool = %q, want send", pending.ToolName)
	}
	if pending.RuleID != "journey-critical-send" {
		t.Fatalf("pending approval rule = %q, want the critical send rule", pending.RuleID)
	}
	held := host.sender.bodies()
	if len(held) != 1 || !strings.Contains(held[0], heldBody) {
		t.Fatalf("delivered bodies after the hold = %q, want the waiting answer alone", held)
	}
	for _, body := range held {
		if strings.Contains(body, toolBody) {
			t.Fatalf("delivered bodies after the hold = %q, want the held tool not to have run", held)
		}
	}
	requests := provider.waitForRequests(t, journeyEntryModel, 2, 30*time.Second)
	if text := requests[1].text(); !strings.Contains(text, "parked awaiting approval") {
		t.Fatalf("held turn request text = %q, want the park receipt the model read", text)
	}

	if _, err := manager.Decide(pending.ID, approval.ApprovalApproved, "journey-approver"); err != nil {
		t.Fatalf("approve the held call: %v", err)
	}

	host.invokeOK(t, ctx, session, "retry the critical tool")
	host.sender.waitFor(t, toolBody, 30*time.Second)
	host.sender.waitFor(t, finalBody, 30*time.Second)
	delivered := host.sender.bodies()
	if len(delivered) < 3 {
		t.Fatalf("delivered bodies = %q, want the waiting answer, the tool body and the final answer", delivered)
	}
	if !strings.Contains(delivered[0], heldBody) {
		t.Fatalf("first delivered body = %q, want the waiting answer the hold produced", delivered[0])
	}
}

// journeyCriticalRuling is the admission ruling of this journey: every send call
// is graded critical, so a call whose exact arguments are not approved yet parks
// in the approval wait under a request the manager registers, and a call whose
// arguments carry an approved request runs. Every other tool is admitted for
// execution.
func journeyCriticalRuling(manager *approval.ApprovalManager) task.Approver {
	return func(ctx context.Context, tool string, jsonArgs []byte) (task.Ruling, error) {
		if tool != "send" {
			return task.Ruling{Kind: task.RulingRun}, nil
		}
		digest := approval.ArgsDigest(jsonArgs)
		if req := manager.Lookup(tool, digest); req != nil && req.Status == approval.ApprovalApproved {
			return task.Ruling{Kind: task.RulingRun, Reason: "the operator approved the held arguments"}, nil
		}
		id := task.NewApprovalID()
		if _, err := manager.Request(ctx, id, tool, jsonArgs, plugin.RiskCritical,
			"journey-critical-send", "the send tool is a critical operation of this journey"); err != nil {
			return task.Ruling{}, err
		}
		return task.Ruling{
			Kind:   task.RulingHold,
			Reason: "the send tool is a critical operation of this journey; critical risk awaits approval",
		}, nil
	}
}

// waitForPendingApproval waits until the approval manager holds a request and
// reports it, so the journey reads the request the gate really registered
// instead of assuming when the admission reached it.
func waitForPendingApproval(t *testing.T, manager *approval.ApprovalManager) *approval.ApprovalRequest {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if pending := manager.Pending(); len(pending) > 0 {
			return pending[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending approvals = %d after 30s, want the held call", len(manager.Pending()))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
