// The continuation of one delegated task survives the process that admitted it: the
// declaration travels on the delegated input fact, the delegation ledger rebuilds its
// entry from the task chain, and the re-entry after the restart serves the next round
// on the same assembly.
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package host

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// sendRigRestartWindow bounds the restart anchor's own waits: the boot of a second
// process is heavier than one decision, so the window the harness uses for a decision
// is widened here.
const sendRigRestartWindow = 60 * time.Second

// sendRigRestartedSendCall is the native call identity the restarted process's send
// carries: a fresh identity, so the receipt the anchor waits on is the call the second
// process made and not the replayed result of the call the first process made.
const sendRigRestartedSendCall = "call-send-rig-send-after-restart"

// TestReenteredTaskServesAfterRestart pins the continuation of a delegated task across
// a process restart:
// - positive: the terminal the first process leaves behind is re-entered by the second process, and the body reaches the reviewing round
// - high-water: a task the first process already re-entered once continues at the round after the one it reached, so the restarted round is the third and never a reused identity
// - declaration: the re-entered round runs under the instruction and model the delegated input fact recorded, not under a declaration of the second process
// - round identity: the re-entered round settles under its own notification identity, so the rounds are read apart
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestReenteredTaskServesAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	root := t.TempDir()

	// The first process delegates the review, re-enters it once when its terminal lands,
	// and then leaves a task that already reached the second round behind: the restart
	// must continue at the third.
	first := newSendRig(t)
	firstHost := bootSendRigOverRoot(t, first, newDeliverySender(nil), root)
	if _, err := driveIntegrityTurn(ctx, firstHost, sendRigAddress, sendRigRequest); err != nil {
		closeSendRigHost(t, firstHost)
		t.Fatalf("the first process delegated the review beside: %v", err)
	}
	var task event.TaskID
	waitForSettle(t, "the delegated review to land its terminal in the first process", func() bool {
		task = sendRigSettledTaskID(firstHost, sendRigAddress)
		return task != ""
	})
	if notice := sendRigAwaitNotice(t, firstHost, task, 2); !strings.Contains(notice, sendRigSecondRound) {
		closeSendRigHost(t, firstHost)
		t.Fatalf("the second round's notice = %q, want the verdict the in-process re-entry produced %q", notice, sendRigSecondRound)
	}

	// The declaration travels on the task fact of each delegated input batch and
	// nowhere else: the instruction and material facts are material of the round, so a
	// re-entry reads the assembly from the one fact that closes the batch.
	site := event.Address{
		AgentID:   event.AgentID(sendRigReviewerAgent),
		SessionID: subagent.DelegatedSiteSession(sendRigAddress.SessionID, sendRigDelegateCall),
	}
	declaredTasks := 0
	for _, fact := range firstHost.Run().SessionFacts(site) {
		switch fact.Meta["delegated_part"] {
		case "task":
			declaration := fact.Meta[event.MetaKeyDelegation]
			if declaration == "" {
				closeSendRigHost(t, firstHost)
				t.Fatalf("the task fact of the delegated batch carries no declaration: %+v", fact.Meta)
			}
			if !strings.Contains(declaration, `"name":"`+sendRigReviewerAgent+`"`) {
				closeSendRigHost(t, firstHost)
				t.Fatalf("the declaration on the task fact = %q, want the assembled name %q", declaration, sendRigReviewerAgent)
			}
			declaredTasks++
		case "instruction", "material":
			if fact.Meta[event.MetaKeyDelegation] != "" {
				closeSendRigHost(t, firstHost)
				t.Fatalf("the %q fact carries the declaration, want the task fact alone", fact.Meta["delegated_part"])
			}
		}
	}
	if declaredTasks != 2 {
		closeSendRigHost(t, firstHost)
		t.Fatalf("task facts carrying a declaration = %d, want the two rounds the first process served", declaredTasks)
	}
	closeSendRigHost(t, firstHost)

	// The restart: a fresh process over the same storage, a fresh endpoint and a fresh
	// decision script that hands the body back to the task the first process left.
	second := newSendRig(t)
	second.reentryTask = task
	second.sendCall = sendRigRestartedSendCall
	secondHost := bootSendRigOverRoot(t, second, newDeliverySender(nil), root)
	t.Cleanup(func() { closeSendRigHost(t, secondHost) })
	if _, err := driveIntegrityTurn(ctx, secondHost, sendRigAddress, "after the restart, hand the note back"); err != nil {
		t.Fatalf("the restarted process drove the entry turn beside: %v", err)
	}
	if receipt := sendRigAwaitSendReceipt(t, second, sendRigRestartedSendCall); receipt != sendRigReceiptOK {
		t.Fatalf("the re-entry receipt after the restart = %q, want the bare %q of a re-entered task", receipt, sendRigReceiptOK)
	}
	sendRigAwaitRestartedReviewerBody(t, second)

	// The declaration the delegated input fact carried is the one the re-entered round
	// runs under: the second process never declared the reviewing instruction itself.
	carried := false
	for _, request := range second.provider.servedRequests() {
		if request.Model != sendRigReviewerModel || !sendRigCarries(request, sendRigBody) {
			continue
		}
		carried = true
		if !sendRigCarries(request, sendRigRequest) {
			t.Fatalf("the re-entered reviewing request lost the recorded instruction %q: %+v", sendRigRequest, request.Messages)
		}
	}
	if !carried {
		t.Fatal("the restarted process served no reviewing request carrying the re-entry body")
	}

	// The restarted round is the third round of the same task: the two rounds the first
	// process reached are consumed, not unopened, and the notice keeps its own identity.
	notice := sendRigAwaitNotice(t, secondHost, task, 3)
	if !strings.Contains(notice, sendRigSecondRound) {
		t.Fatalf("the third round's notice = %q, want the verdict the restarted reviewing round produced %q", notice, sendRigSecondRound)
	}
}

// TestReentryOfNoTaskServesNoRound pins the negative half of the continuation's
// rebuild: a body naming a task the table holds no record of is refused by name,
// so no round — and therefore no continuation slot — is built for it.
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestReentryOfNoTaskServesNoRound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rig := newSendRig(t)
	rig.reentryTask = event.TaskID("task-never-delegated")
	host := bootSendRigOverRoot(t, rig, newDeliverySender(nil), t.TempDir())
	t.Cleanup(func() { closeSendRigHost(t, host) })
	if _, err := driveIntegrityTurn(ctx, host, sendRigAddress, "hand the note to a task the table never admitted"); err != nil {
		t.Fatalf("the entry turn naming no task ended beside: %v", err)
	}
	receipt := sendRigAwaitReceipt(t, rig, sendRigSendCall)
	if !strings.HasPrefix(receipt, sendRigDeniedPrefix) {
		t.Fatalf("receipt of a body naming no task = %q, want the denied prefix", receipt)
	}
	if !strings.Contains(receipt, "holds no record") {
		t.Fatalf("receipt of a body naming no task = %q, want the task table's no-record refusal", receipt)
	}
	for _, request := range rig.provider.servedRequests() {
		if request.Model == sendRigReviewerModel {
			t.Fatalf("a body naming no task served a reviewing round: %+v", request.Messages)
		}
	}
}

// sendRigAwaitSendReceipt waits for the exact receipt of one send call. The tool
// message of the call is the admission receipt (the bare ok) rather than the result the
// call dispatched: a refused call commits its denial as the user material after that
// admission, so the denial is read first and the admission is answered ok only after a
// grace in which no denial landed. The restart anchor reads a task with notices in
// flight, so notice material is never taken for the receipt.
func sendRigAwaitSendReceipt(t *testing.T, rig *sendRig, callID string) string {
	t.Helper()
	deadline := time.Now().Add(sendRigRestartWindow)
	var admitted time.Time
	for {
		for _, request := range rig.provider.servedRequests() {
			for index, message := range request.Messages {
				if message.ToolCallID != callID || message.Role != "tool" {
					continue
				}
				if message.Content == sendRigReceiptOK && admitted.IsZero() {
					admitted = time.Now()
				}
				for _, later := range request.Messages[index+1:] {
					if later.Role != "user" {
						continue
					}
					if strings.HasPrefix(later.Content, sendRigDeniedPrefix) {
						return later.Content
					}
					if later.Content == sendRigReceiptOK {
						return later.Content
					}
				}
			}
		}
		if !admitted.IsZero() && time.Since(admitted) >= sendRigReceiptGrace {
			return sendRigReceiptOK
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no decision of the restarted rig read the receipt of %q within %s", callID, sendRigRestartWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sendRigReceiptGrace is how long the receipt reader waits after a call's admission for
// the denial a refused dispatch commits after it, so the bare admission is answered as
// the delivery only when no denial followed.
const sendRigReceiptGrace = 500 * time.Millisecond

// bootSendRigOverRoot boots the re-entry rig over one storage root the caller owns, so a
// second boot of the same anchor reads the chain the first process committed.
func bootSendRigOverRoot(t *testing.T, rig *sendRig, sender assembly.MessageSender, root string) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	configPath := filepath.Join(root, "tagent.yaml")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, rig.provider.endpoint(), sendRigEntryModel,
		"  "+sendRigEntryAgent+":\n"+
			"    model: "+sendRigEntryModel+"\n"+
			"    tools:\n"+
			"      - { kind: tool, id: "+sendRigToolSend+" }\n"+
			"      - { kind: tool, id: "+sendRigToolSubagent+" }\n"+
			"  "+sendRigReviewerAgent+":\n"+
			"    model: "+sendRigReviewerModel+"\n"))
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the re-entry rig over %s: %v", root, err)
	}
	return host
}

// closeSendRigHost closes one boot of the rig under the anchor's own budget, reporting a
// close that ended beside work rather than failing the check on the shutdown path.
func closeSendRigHost(t *testing.T, host *assembly.Host) {
	t.Helper()
	if host == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Logf("the re-entry rig ended its close beside: %v", err)
	}
}

// sendRigAwaitRestartedReviewerBody waits for the re-entered round's request: a reviewing
// request that carries the re-entry body, which only the round the restart opened can.
func sendRigAwaitRestartedReviewerBody(t *testing.T, rig *sendRig) {
	t.Helper()
	deadline := time.Now().Add(sendRigRestartWindow)
	for {
		for _, request := range rig.provider.servedRequests() {
			if request.Model == sendRigReviewerModel && sendRigCarries(request, sendRigBody) {
				return
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the restarted process served no reviewing round carrying %q within %s", sendRigBody, sendRigRestartWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
