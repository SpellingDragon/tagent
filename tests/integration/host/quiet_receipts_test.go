package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
)

// TestQuietSendReceipt 钉住投递成功只回最小凭据，具名拒绝仍带自己的原因。
// - success_receipt_is_minimal_ok：正式 Host.Invoke 下重入一轮真实任务的 send 回执恰为 "ok"，目标会话 id 与送达字样都不进模型材料（重入体只到被重入的那一轮，永不出网）。
// - denied_receipt_keeps_named_reason：空正文的拒绝仍是 "[send_denied] content is empty"，拒因绝不降为 ok。
// 契约: docs/wiki/platform/platform-subsystems.md#governance-gate
func TestQuietSendReceipt(t *testing.T) {
	t.Run("success_receipt_is_minimal_ok", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		rig := newSendRig(t)
		sender := newDeliverySender(nil)
		host := sendRigBoot(t, rig, sender)
		t.Cleanup(func() {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer closeCancel()
			if err := host.Close(closeCtx); err != nil {
				t.Logf("the quiet receipt host ended its close beside: %v", err)
			}
		})

		if _, err := driveIntegrityTurn(ctx, host, sendRigAddress, sendRigRequest); err != nil {
			t.Fatalf("the turn that re-enters the task it delegated ended by error: %v", err)
		}
		receipt := sendRigAwaitReceipt(t, rig, sendRigSendCall)
		if receipt != sendRigReceiptOK {
			t.Fatalf("the success receipt = %q, want the bare %q the re-entry is answered with", receipt, sendRigReceiptOK)
		}
		if strings.Contains(receipt, string(sendRigAddress.SessionID)) || strings.Contains(receipt, "delivered") {
			t.Fatalf("the success receipt = %q, want no target session and no delivery wording beside the credential", receipt)
		}
		sendRigAwaitReviewerBody(t, rig)
		for _, body := range sender.bodies() {
			if strings.Contains(body, sendRigBody) {
				t.Fatalf("the re-entry body left through the outbound channel: %q", body)
			}
		}
	})

	t.Run("denied_receipt_keeps_named_reason", func(t *testing.T) {
		const (
			input  = "send an empty note"
			answer = "the answer beside the refusal"
			callID = "call-quiet-denied"
		)
		provider := newScriptedProvider(t)
		t.Setenv("TAGENT_TEST_API_KEY", "test-key")
		t.Setenv("TAGENT_HTTP_PORT", "0")
		script := newIntegrityScript()
		script.expect("model-entry", "the entry sends an empty body", requestTail(input)).
			answers(settleToolCallAnswer("send", callID, `{"target":{"agent":"user","session":"session-quiet-denied"},"content":""}`))
		script.expect("model-entry", "the entry answers beside the refusal", requestCarriesToolResult(callID)).
			answers(scriptedAnswerOf(answer)).
			delivers(answer)
		provider.decide = script.decide

		sender := newDeliverySender(nil)
		host := bootDeliveryHost(t, provider, sender)
		keepIntegrityHost(t, script, host)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, input)
		if err != nil {
			t.Fatalf("the refused send turn ended by error: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("the refused send turn = (%v, %d decisions), want the completed turn of the refusal and the answer",
				turn.Terminal, len(turn.Decisions))
		}

		requests := provider.servedRequests()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want the refusal decision beside the answer that read it", len(requests))
		}
		receipt, ok := sendRigReceipt(requests[1], callID)
		if !ok {
			t.Fatalf("the answer decision carried no tool result for %q: %+v", callID, requests[1].Messages)
		}
		if receipt != "[send_denied] content is empty" {
			t.Fatalf("the refusal receipt = %q, want the named reason the caller corrects against", receipt)
		}

		if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != answer {
			t.Fatalf("delivered bodies = %q, want the answer alone beside a refusal that delivered nothing", bodies)
		}
	})
}
