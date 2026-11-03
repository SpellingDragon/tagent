// The send carries a body to the task it names and never to the outbound channel: the
// turn body is the one body the host delivers, so the seam this check pins is the one
// design DEL-1 left in place.
package host

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSendDeliversNoChannelBody drives the send face of the formal host and checks the three anchors of the delivery seam.
// - positive: a send naming the settled task of a delegation is answered with the bare ok, and the body it carried reaches the reviewing round the re-entry opened
// - negative: no body the send carried reaches the outbound sender, so the channel is never used in its place
// - consumer: the turn body of the same run still leaves through that very sender, so the outbound chain of a turn carries the turn body alone
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestSendDeliversNoChannelBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rig := newSendRig(t)
	sender := newDeliverySender(nil)
	host := sendRigBoot(t, rig, sender)
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer closeCancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the no-channel-body host ended its close beside: %v", err)
		}
	})

	if _, err := driveIntegrityTurn(ctx, host, sendRigAddress, sendRigRequest); err != nil {
		t.Fatalf("the turn that re-enters the task it delegated ended by error: %v", err)
	}
	if receipt := sendRigAwaitReceipt(t, rig, sendRigSendCall); receipt != sendRigReceiptOK {
		t.Fatalf("the send receipt = %q, want the bare %q of a body handed to the task it named", receipt, sendRigReceiptOK)
	}
	sendRigAwaitReviewerBody(t, rig)

	bodies := sender.bodies()
	for _, body := range bodies {
		if strings.Contains(body, sendRigBody) {
			t.Fatalf("the send body left through the outbound channel: %q; recorded bodies = %q", body, bodies)
		}
	}
	delivered := false
	for _, body := range bodies {
		if strings.TrimSpace(body) == sendRigTurnDone {
			delivered = true
		}
	}
	if !delivered {
		t.Fatalf("delivered bodies = %q, want the turn body %q beside the send that delivered nothing", bodies, sendRigTurnDone)
	}
}
