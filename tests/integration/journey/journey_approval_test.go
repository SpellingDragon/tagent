// J6 is the approval journey: the admission ruling grades one tool critical, the model
// calls it, and the call parks in the approval wait instead of running. An operator
// approves the held arguments through the approval manager, the model retries the same
// call in a second user turn, and the approved call runs exactly once into the
// destination it named.
//
// The journey takes the production seat: the approver is the admission ruling the host
// attaches to every wrapped call, the approval manager is the same waiting book an
// operator entry answers, and the retry is a second Host.Invoke.
// - the approval decision is never injected into a wrapper
// - Boot forwards the configured policy and enforcement grade onto this same assembly beside the approver
// - the call names a session destination, so the body lands in the peer session the document's deliver_to white list declares, not in the outbound channel
// - the only body that leaves through the channel is the turn body itself, so the journey pins that the approved call ran exactly once where it landed
// - the document declares the one delivery white list the send destination is judged against
// - the entry agent dispatches the one communication tool, and the peer agent is its one declared target
// - the declaration reflects on no host-facing material here, so the reflection face stays quiet and the journey observes the delivery, not a reflection pass
// - a body is delivered only into a session this run already bound, so the journey opens the peer session first, under the lineage a sent body carries
// - the approved call runs exactly once: the one body the retry carried lands in the session it named, and the receipts of that session read no second one
// - the delivery owes the peer session exactly one round, and that round reads the approved body
// - the user still receives the turn's own answer and nothing the send carried: the outbound channel serves the turn body alone
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package journey

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// session, peerAgent and peerSession name the journey's own identities: the user session the
// two user turns run in, the peer session the approved call delivers to, and the bodies each
// seat carries.
const (
	session      = "session-journey-approval"
	peerAgent    = "worker"
	peerSession  = "session-journey-approval-worker"
	toolBody     = "the approved tool ran and reached the user"
	heldBody     = "the critical call is waiting for approval"
	finalBody    = "the approved call ran to completion"
	peerSeedBody = "the peer session stands ready"
	peerBody     = "the peer session read the approved body"
	// callArgs is the one argument surface the held call and its retry share
	// byte for byte, which is what makes the approval digest answer for both: a
	// session destination naming the peer the white list below already declares.
	callArgs = `{"target":{"agent":"` + peerAgent + `","session":"` + peerSession + `"},"content":"` + toolBody + `"}`
)

// TestJourneyApprovalHoldThenRun drives one held call and its approved retry:
// - the first turn's critical call parks unrun, and the model reads the park receipt
// - the waiting answer reaches the user, and the held arguments wait under the critical rule
// - approving and retrying runs the same call exactly once into the session it names
// - the approved body drives that peer session through one round, beside the user's own answer
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestJourneyApprovalHoldThenRun(t *testing.T) {
	userAddr := event.Address{AgentID: journeyEntryAgent, SessionID: event.SessionID(session)}
	peerAddr := event.Address{AgentID: peerAgent, SessionID: event.SessionID(peerSession)}
	manager := approval.NewApprovalManager("", 0, approval.WithApprovalRescanInterval(0))
	approve := journeyCriticalRuling(manager)
	provider := newJourneyProvider(t)
	provider.script(journeyEntryModel, journeyReply{body: journeyToolCall("call-send-1", "send", callArgs)})
	provider.scriptAnswers(journeyEntryModel, heldBody)
	provider.script(journeyEntryModel, journeyReply{body: journeyToolCall("call-send-2", "send", callArgs)})
	provider.scriptAnswers(journeyEntryModel, finalBody)
	provider.scriptAnswers(journeyWorkerModel, peerSeedBody, peerBody)
	host := bootJourney(t, provider, journeySpec{
		EntryTools: []string{"send"},
		MaxTokens:  4096,
		EntryExtra: []string{
			"    memory:",
			"      read_namespaces: [" + peerAgent + "]",
			"    meditation:",
			"      enabled: true",
			"      interval: 1h",
			"      min_gap: 0s",
			"      prompt_file: meditation.md",
			"      observed_namespaces: [" + peerAgent + "]",
			"      deliver_to: [" + peerAgent + "]",
		},
		ExtraAgents: map[string][]string{
			peerAgent: {"    model: " + journeyWorkerModel},
		},
		PromptFiles: map[string]string{
			"meditation.md": "Reflect over the peer session and deliver one summary.\n",
		},
	},
		assembly.WithApprover(approve),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	host.invokePeer(t, ctx, peerAddr, peerSeedBody)

	host.invokeOK(t, ctx, session, "please run the critical tool")

	pending := waitForPendingApproval(t, manager)
	if pending.ToolName != "send" {
		t.Fatalf("pending approval tool = %q, want send", pending.ToolName)
	}
	if pending.RuleID != "journey-critical-send" {
		t.Fatalf("pending approval rule = %q, want the critical send rule", pending.RuleID)
	}
	held := bodiesTo(host.sender.deliveries(), userAddr)
	if len(held) != 1 || !strings.Contains(held[0], heldBody) {
		t.Fatalf("delivered bodies after the hold = %q, want the waiting answer alone", held)
	}
	for _, body := range host.sender.bodies() {
		if strings.Contains(body, toolBody) {
			t.Fatalf("delivered bodies after the hold = %q, want the held tool not to have run", host.sender.bodies())
		}
	}
	if ran := sentBodies(host.peerFacts(peerAddr), toolBody); len(ran) != 0 {
		t.Fatalf("peer session bodies after the hold = %q, want none: a hold executes nothing", ran)
	}
	requests := provider.waitForRequests(t, journeyEntryModel, 2, 30*time.Second)
	if text := requests[1].text(); !strings.Contains(text, "parked awaiting approval") {
		t.Fatalf("held turn request text = %q, want the park receipt the model read", text)
	}

	if _, err := manager.Decide(pending.ID, approval.ApprovalApproved, "journey-approver"); err != nil {
		t.Fatalf("approve the held call: %v", err)
	}
	if left := manager.Pending(); len(left) != 0 {
		t.Fatalf("pending approvals after the ruling = %+v, want the answered question gone", left)
	}

	host.invokeOK(t, ctx, session, "retry the critical tool")

	delivered := host.waitForSessionFact(t, peerAddr, sentBody(toolBody), 30*time.Second)
	if ran := sentBodies(host.peerFacts(peerAddr), toolBody); len(ran) != 1 {
		t.Fatalf("peer session bodies after the approval = %q, want the one body %q the approved call carried",
			ran, toolBody)
	}
	if delivered.Owner != peerAddr {
		t.Fatalf("delivered body owner = %+v, want the peer session %+v the call named", delivered.Owner, peerAddr)
	}
	if leaked := sentBodies(host.sessionFacts(session), toolBody); len(leaked) != 0 {
		t.Fatalf("caller session bodies = %q, want none: the body landed in the destination the call named", leaked)
	}
	if left := manager.Pending(); len(left) != 0 {
		t.Fatalf("pending approvals after the approved retry = %+v, want no second question: the approved arguments run", left)
	}

	driverRequests := provider.waitForRequests(t, journeyWorkerModel, 2, 30*time.Second)
	if len(driverRequests) != 2 {
		t.Fatalf("peer session requests = %d, want the seed turn and the one round the delivery owed", len(driverRequests))
	}
	if text := driverRequests[1].text(); !strings.Contains(text, toolBody) {
		t.Fatalf("driven peer request text = %q, want the approved body presented to the peer", text)
	}
	if got := bodiesTo(host.sender.deliveries(), peerAddr); len(got) != 0 {
		t.Fatalf("peer session outbound bodies = %q, want none: a peer turn is no user seat", got)
	}
	if answer := host.waitForSessionFact(t, peerAddr, answerCarrying(peerBody), 30*time.Second); answer.Key == "" {
		t.Fatalf("driven peer answer = %+v, want the committed answer of the round the delivery owed", answer)
	}

	deliveredBodies := bodiesTo(host.sender.deliveries(), userAddr)
	if len(deliveredBodies) != 2 || !strings.Contains(deliveredBodies[0], heldBody) ||
		!strings.Contains(deliveredBodies[1], finalBody) {
		t.Fatalf("delivered bodies = %q, want the waiting answer and then the final answer", deliveredBodies)
	}
	for _, body := range host.sender.bodies() {
		if strings.Contains(body, toolBody) {
			t.Fatalf("delivered bodies after the approval = %q, want no channel body carrying a send body", host.sender.bodies())
		}
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

// invokePeer drives one turn of one peer session through the same host entry a
// peer delivery lands through: the address the call names, one message, and the
// trigger source a sent body stamps, because the session a peer writes into is
// opened by that body and never by a user turn of the peer's own.
func (h *journeyHost) invokePeer(t *testing.T, ctx context.Context, addr event.Address, input string) {
	t.Helper()
	turn, err := h.host.Invoke(ctx, assembly.InvokeRequest{
		Address:       addr,
		Input:         []model.Message{{Role: model.RoleUser, Content: input}},
		TriggerSource: sentLineage,
	})
	if err != nil {
		t.Fatalf("invoke the peer turn of %+v: %v", addr, err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("peer turn terminal of %+v = %v, want the completed turn", addr, turn.Terminal)
	}
}

// sentLineage is the trigger source a cross-session delivery stamps on the fact
// it commits (design D8), so the peer turn the journey opens carries the same
// lineage a delivered body does.
const sentLineage = "send"

// peerFacts reports the committed facts of one peer session, so a journey reads
// what the destination session received instead of what the caller answered.
func (h *journeyHost) peerFacts(addr event.Address) []event.Event {
	return h.host.Run().SessionFacts(addr)
}

// sentBodies reports the bodies one session chain carries that a send landed,
// in commit order: the facts the cross-session delivery stamped beside the body
// the call named.
func sentBodies(facts []event.Event, want string) []string {
	out := make([]string, 0, len(facts))
	for _, fact := range facts {
		if fact.Meta[event.MetaKeyTriggerSource] != sentLineage {
			continue
		}
		if strings.Contains(fact.Message.Content, want) {
			out = append(out, fact.Message.Content)
		}
	}
	return out
}

// waitForSessionFact waits until one session chain carries a fact the match
// accepts and reports it, so a journey waits on what the destination session
// committed instead of on an internal state.
func (h *journeyHost) waitForSessionFact(t *testing.T, addr event.Address, match func(event.Event) bool, timeout time.Duration) event.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, fact := range h.peerFacts(addr) {
			if match(fact) {
				return fact
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %+v carries no matching fact after %s", addr, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sentBody matches the fact one cross-session delivery commits beside the body
// the call named.
func sentBody(want string) func(event.Event) bool {
	return func(fact event.Event) bool {
		return fact.Meta[event.MetaKeyTriggerSource] == sentLineage && strings.Contains(fact.Message.Content, want)
	}
}

// answerCarrying matches the model answer one turn commits into its own chain.
func answerCarrying(want string) func(event.Event) bool {
	return func(fact event.Event) bool {
		return fact.Type == event.TypeAgentOutput && strings.Contains(fact.Message.Content, want)
	}
}

// bodiesTo reports the bodies delivered to one address, in delivery order, so a
// journey reads the user's own deliveries apart from the peer's.
func bodiesTo(deliveries []journeyDelivery, addr event.Address) []string {
	out := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.Target != addr {
			continue
		}
		out = append(out, delivery.Content)
	}
	return out
}
