// The governance anchor of the cross-session delivery: the critical grade one
// document declares for the send tool holds the call before the delivery face runs,
// the model reads the wait instead of a delivery receipt, and the ruling the approver
// records is what lets the same body land exactly once.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package host

import (
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// sendApproveCall and sendApproveRetryCall are the call identities of the approval
// anchor: the held call and its retry, the shape a model issues after it reads the wait.
const (
	sendApproveCall      = "call-approve-send"
	sendApproveRetryCall = "call-approve-retry-send"
)

// sendApproveRiskBlock declares the critical grade the anchor holds send at. It is the
// top-level block a deployment declares, appended to the delivery document as it
// stands.
const sendApproveRiskBlock = "governance:\n" +
	"  risk_rules:\n" +
	"    - { tool: " + hostDriveToolSend + ", level: critical }\n"

// sendApproveDocument renders the delivery document beside the risk grade its send
// calls are ruled under.
func sendApproveDocument(root, endpoint string) string {
	return hostDriveDocument(root, endpoint) + sendApproveRiskBlock
}

// TestSendCrossSessionApproval drives the governance gate of the cross-session face through the boot entry a deployment uses.
// - positive: an approved ruling lets the retried body land once, and the target is driven through the one round the delivery owes
// - negative held_body_never_reported_delivered: while the call waits, the body is in no target chain
// - negative held_body_never_reported_delivered: no target round read it, and the receipt the model read claims no delivery
// - the retry names the same arguments under a fresh call identity, the shape a model issues when it retries a call it was told to wait for
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestSendCrossSessionApproval(t *testing.T) {
	t.Run("held_body_never_reported_delivered", func(t *testing.T) {
		scene, approvals := newSendApproveScene(t)
		scene.bindPeer(t)
		scene.invokeMeditation(t, sendApproveCall, hostDrivePlainSend(sendApproveCall))
		held := scene.awaitHeldSend(t, sendApproveCall, approvals)

		if facts := scene.bodyFacts(t); len(facts) != 0 {
			t.Fatalf("facts of a held body = %d, want none: a hold executes nothing:\n%s", len(facts), scene.describe())
		}
		if rounds := scene.peerRoundsCarrying(t); len(rounds) != 0 {
			t.Fatalf("target rounds carrying a held body = %d, want none:\n%s", len(rounds), scene.describe())
		}
		if bodies := scene.sender.bodies(); len(bodies) != 0 {
			t.Fatalf("outbound bodies beside a held send = %q, want none: a session destination is not the channel", bodies)
		}
		if held.Status != approval.ApprovalPending {
			t.Fatalf("the waiting question stands at %q, want the pending state the model read", held.Status)
		}
	})

	t.Run("approved_send_delivers_exactly_once", func(t *testing.T) {
		scene, approvals := newSendApproveScene(t)
		scene.bindPeer(t)
		scene.invokeMeditation(t, sendApproveCall, hostDrivePlainSend(sendApproveCall))
		held := scene.awaitHeldSend(t, sendApproveCall, approvals)

		if _, err := approvals.Decide(held.ID, approval.ApprovalApproved, "the-send-approver"); err != nil {
			t.Fatalf("approve the held send: %v", err)
		}
		if pending := approvals.Pending(); len(pending) != 0 {
			t.Fatalf("pending approvals after the ruling = %+v, want the answered question gone", pending)
		}

		scene.invokeMeditation(t, sendApproveRetryCall, hostDrivePlainSend(sendApproveRetryCall))
		if receipt := scene.awaitReceipt(t, sendApproveRetryCall); receipt != hostDriveReceiptOK {
			t.Fatalf("send{target:{agent,session}} result after the approval = %q, want the bare %q:\n%s",
				receipt, hostDriveReceiptOK, scene.describe())
		}
		scene.awaitPeerBody(t)
		if facts := scene.bodyFacts(t); len(facts) != 1 {
			t.Fatalf("delivered facts after the approval = %d, want the one body the approved call carried:\n%s",
				len(facts), scene.describe())
		}
		if rounds := scene.peerRoundsCarrying(t); len(rounds) != 1 {
			t.Fatalf("target rounds carrying the body = %d, want the one round the approved delivery owed:\n%s",
				len(rounds), scene.describe())
		}
		scene.refuteOutbound(t)
	})
}

// newSendApproveScene boots the delivery scene over the approval channel, so the
// governing risk table of the document really holds the send call.
func newSendApproveScene(t *testing.T) (*hostDriveScene, *approval.ApprovalManager) {
	t.Helper()
	approvals := approval.NewApprovalManager(t.TempDir(), time.Hour)
	scene := newHostDriveSceneWith(t, sendApproveDocument, assembly.WithApprovals(approvals))
	return scene, approvals
}

// awaitHeldSend waits for the receipt the reflecting turn read beside a held call and
// answers the question the hold registered.
//
// The hold is rendered by the admission wrapper before the tool runs, so the text the
// model reads is the runtime's own wait, paired with its call under the tool role. The
// check states that the receipt claims no delivery in either dialect: neither the bare
// ok of a delivered body nor the [send_held] result text the send face answers a body
// it parks itself.
func (s *hostDriveScene) awaitHeldSend(t *testing.T, callID string, approvals *approval.ApprovalManager) *approval.ApprovalRequest {
	t.Helper()
	receipt := s.awaitToolResult(t, callID)
	if receipt == hostDriveReceiptOK || strings.HasPrefix(receipt, hostSendHeldPrefixProbe) {
		t.Fatalf("the held send read back %q, want a wait that claims no delivery:\n%s", receipt, s.describe())
	}
	if !strings.Contains(receipt, "parked awaiting approval") {
		t.Fatalf("the held send read back %q, want the wait the admission wrapper renders:\n%s", receipt, s.describe())
	}
	pending := approvals.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending approvals = %+v, want the one question the critical send parked", pending)
	}
	if pending[0].ToolName != hostDriveToolSend {
		t.Fatalf("the parked question names tool %q, want %q", pending[0].ToolName, hostDriveToolSend)
	}
	return pending[0]
}

// awaitToolResult waits for the text the model read under one call identity, paired
// with its call under the tool role: the shape a call that never executed answers in,
// beside the input material a settled result is entered as.
func (s *hostDriveScene) awaitToolResult(t *testing.T, callID string) string {
	t.Helper()
	deadline := time.Now().Add(hostDriveWindow)
	for {
		for _, request := range s.provider.servedRequests() {
			for _, message := range request.Messages {
				if message.Role == "tool" && message.ToolCallID == callID {
					return message.Content
				}
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no decision read a tool result under %q within %s:\n%s", callID, hostDriveWindow, s.describe())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// hostSendHeldPrefixProbe is the hold prefix of the send receipt dialect as the anchor
// reads it back from the model-visible text. It is written here because the prefix is a
// private constant of the assembly package, and a check of the receipt dialect has to
// name the text it refuses.
const hostSendHeldPrefixProbe = "[send_held] "
