// The settle notice of a re-entered round is a notification of its own: the round the
// re-entry opened is notified under its own identity, never merged into the notice of
// the round before, and the follow-up turn it drives runs exactly once.
package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestReactivatedSettleFiresOnce drives one delegated task through a re-entry and checks the three anchors of the new round's notification identity.
// - positive: the new round settles into a notice of its own, carried under the four-part identity of that round beside the material it produced
// - negative (old_round_notice_not_merged): the notice of the new round is a fact of its own, so the notice of the round before keeps its own identity and body
// - consumer: the follow-up turn of the new round is driven once, so the initiator's later turns read the notification one time
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestReactivatedSettleFiresOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rig := newSendRig(t)
	sender := newDeliverySender(nil)
	host := sendRigBoot(t, rig, sender)
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer closeCancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the settle-once host ended its close beside: %v", err)
		}
	})

	if _, err := driveIntegrityTurn(ctx, host, sendRigAddress, sendRigRequest); err != nil {
		t.Fatalf("the turn that re-enters the task it delegated ended by error: %v", err)
	}
	if receipt := sendRigAwaitReceipt(t, rig, sendRigSendCall); receipt != sendRigReceiptOK {
		t.Fatalf("the send receipt = %q, want the bare %q of a re-entered task", receipt, sendRigReceiptOK)
	}
	sendRigAwaitReviewerBody(t, rig)

	task := sendRigSettledTaskID(host, sendRigAddress)
	if task == "" {
		t.Fatalf("the session chain holds no settled notification of the delegated round")
	}
	first := sendRigAwaitNotice(t, host, task, 1)
	second := sendRigAwaitNotice(t, host, task, 2)

	t.Run("the new round is notified under its own identity", func(t *testing.T) {
		if !strings.Contains(first, sendRigFirstRound) {
			t.Fatalf("the notice of the first round = %q, want the verdict of that round %q", first, sendRigFirstRound)
		}
		if !strings.Contains(second, sendRigSecondRound) {
			t.Fatalf("the notice of the second round = %q, want the verdict the re-entered round produced %q", second, sendRigSecondRound)
		}
	})

	t.Run("old_round_notice_not_merged", func(t *testing.T) {
		if strings.Contains(second, sendRigFirstRound) {
			t.Fatalf("the notice of the second round = %q, want no material of the round before: a merge would carry both verdicts", second)
		}
		if first == second {
			t.Fatalf("both rounds were notified under the same material %q, want one notice per round", first)
		}
		notices := sendRigNoticeMaterials(host, sendRigAddress, task)
		if len(notices) != 2 {
			t.Fatalf("notices of the task = %+v, want the two rounds reported apart", notices)
		}
	})

	t.Run("the follow-up turn fires once", func(t *testing.T) {
		if driven := sendRigAwaitDrivenOnce(t, rig, second); driven != 1 {
			t.Fatalf("model requests opening with the notice of the second round = %d, want exactly 1 follow-up turn; matched requests = %+v",
				driven, sendRigRequestsClosingWith(rig, second))
		}
	})
}

// sendRigDriveGrace is how long a check waits beside a driven turn before it reads the
// count again: a second drive of one notification would open inside that window, so the
// count this check asserts is the count of the notification and not the count of the
// drives that happened to have started yet.
const sendRigDriveGrace = 300 * time.Millisecond

// sendRigAwaitDrivenOnce waits until the notice drove a follow-up turn and answers the
// number of drives the material opened.
func sendRigAwaitDrivenOnce(t *testing.T, rig *sendRig, material string) int {
	t.Helper()
	deadline := time.Now().Add(sendRigWindow)
	for {
		if len(sendRigRequestsClosingWith(rig, material)) > 0 {
			time.Sleep(sendRigDriveGrace)
			return len(sendRigRequestsClosingWith(rig, material))
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the notice of the new round drove no follow-up turn within %s; material = %q", sendRigWindow, material)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sendRigAwaitNotice waits for the settle notice one round of one task is reported under
// and answers the material it carries, failing the check when the window closes first.
func sendRigAwaitNotice(t *testing.T, host *assembly.Host, task event.TaskID, round int) string {
	t.Helper()
	invocation := sendRigNoticeInvocation(task, round)
	deadline := time.Now().Add(sendRigWindow)
	for {
		if material, ok := sendRigNoticeMaterials(host, sendRigAddress, task)[invocation]; ok {
			return material
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the round %d of task %s was reported under no notice of its own within %s; notices = %+v",
				round, task, sendRigWindow, sendRigNoticeMaterials(host, sendRigAddress, task))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sendRigRequestsClosingWith reports the requests whose last message is the material.
func sendRigRequestsClosingWith(rig *sendRig, material string) []scriptedRequest {
	var matched []scriptedRequest
	for _, request := range rig.provider.servedRequests() {
		if len(request.Messages) == 0 {
			continue
		}
		last := request.Messages[len(request.Messages)-1]
		if last.Role == "user" && last.Content == material {
			matched = append(matched, request)
		}
	}
	return matched
}
