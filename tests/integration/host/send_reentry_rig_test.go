// The material the send re-entry checks of this package drive: one entry agent holding
// the communication tool beside the delegation tool, one reviewing agent the delegation
// names, and the bodies the two rounds and the re-entry carry.
//
// The rig exists because a send naming a task is answered by a round this host really
// serves: a document that delegates nothing carries no task to re-enter, so a successful
// send can only be observed beside a delegation the run itself opened.
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
package host

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
)

// sendRigEntryAgent names the identities, calls and bodies of the re-entry rig.
const (
	sendRigEntryAgent    = "user"
	sendRigEntryModel    = "model-entry"
	sendRigReviewerAgent = "reviewer"
	sendRigReviewerModel = "model-reviewer"
	sendRigSession       = "session-send-reentry-rig"
	sendRigToolSend      = "send"
	sendRigToolSubagent  = "call_subagent"

	// sendRigDelegateCall is the call identity of the delegation the entry turn
	// opens, and sendRigSendCall the one of the send that re-enters it.
	sendRigDelegateCall = "call-send-rig-delegate"
	sendRigSendCall     = "call-send-rig-send"

	sendRigRequest     = "send rig: review the note under way"
	sendRigFirstRound  = "send rig: the first round settled"
	sendRigSecondRound = "send rig: the second round read the re-entry"
	sendRigBody        = "send rig: the note the re-entry carries"
	sendRigTurnDone    = "send rig: the turn is done"

	// sendRigReceiptOK is the whole receipt of a re-entered task, and
	// sendRigDeniedPrefix opens the receipt of a call the host refuses by name.
	sendRigReceiptOK    = "ok"
	sendRigDeniedPrefix = "[send_denied] "
)

// sendRigWindow bounds how long a check waits for a decision of the entry turn and for
// the round a re-entry is expected to serve.
const sendRigWindow = 20 * time.Second

// sendRigAddress is the address the entry turn is driven at.
var sendRigAddress = event.Address{AgentID: sendRigEntryAgent, SessionID: sendRigSession}

// sendRig is the decision script of the re-entry rig: the entry turn delegates the
// review once, then re-enters the task it read the terminal of once, and every reviewing
// round answers the provider on its own. Each decision is claimed once, so a drive that
// repeats a step is served the terminal answer instead of silently doubling the flow.
type sendRig struct {
	provider *scriptedProvider
	mu       sync.Mutex
	issued   map[string]bool
}

// newSendRig starts the endpoint the rig boots against.
func newSendRig(t *testing.T) *sendRig {
	t.Helper()
	rig := &sendRig{provider: newScriptedProvider(t), issued: map[string]bool{}}
	rig.provider.decide = rig.decide
	return rig
}

// decide answers one request off the material it carried: a reviewing request answers
// its own round, and the entry turn delegates once and then hands the body back to the
// task whose terminal it read.
func (r *sendRig) decide(request scriptedRequest) (string, error) {
	if request.Model == sendRigReviewerModel {
		if sendRigCarries(request, sendRigBody) {
			return sendRigAnswered(sendRigSecondRound), nil
		}
		return sendRigAnswered(sendRigFirstRound), nil
	}
	if r.claim("delegate") {
		return settleToolCallAnswer(sendRigToolSubagent, sendRigDelegateCall, `{"name":"`+sendRigReviewerAgent+
			`","model":"`+sendRigReviewerModel+`","prompt":"`+sendRigRequest+`"}`), nil
	}
	if task := sendRigSettledTask(request); task != "" && r.claim("send") {
		return settleToolCallAnswer(sendRigToolSend, sendRigSendCall, `{"target":{"task":"`+task+
			`"},"content":"`+sendRigBody+`"}`), nil
	}
	return sendRigAnswered(sendRigTurnDone), nil
}

// claim reports whether the named decision is this request's to make, exactly once.
func (r *sendRig) claim(step string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.issued[step] {
		return false
	}
	r.issued[step] = true
	return true
}

// sendRigBoot boots the entry document the re-entry rig drives: the entry agent beside
// both the communication tool and the delegation tool, beside the reviewing agent a
// delegation names.
func sendRigBoot(t *testing.T, rig *sendRig, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, rig.provider.endpoint(), sendRigEntryModel)
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
		t.Fatalf("boot the send re-entry rig: %v", err)
	}
	return host
}

// sendRigAnswered renders one plain assistant answer of the rig.
func sendRigAnswered(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic("host: encode the rig answer: " + err.Error())
	}
	return `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":` +
		string(encoded) + `},"finish_reason":"stop"}]}`
}

// sendRigCarries reports whether the request carried the text in any message.
func sendRigCarries(request scriptedRequest, text string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

// sendRigSettledTask reports the task the settle notice the request carried belongs to,
// and the empty identity beside a request carrying no such notice. The material a settle
// turn reads opens with "tagent: task <id> <kind>", so the identity is read off the text
// the runtime committed rather than minted by the check.
func sendRigSettledTask(request scriptedRequest) string {
	for _, message := range request.Messages {
		if message.Role != "user" {
			continue
		}
		if task := sendRigTaskOfNotice(message.Content); task != "" {
			return task
		}
	}
	return ""
}

// sendRigTaskOfNotice reports the task one settle notice material names.
func sendRigTaskOfNotice(material string) string {
	const opening = "tagent: task "
	at := strings.Index(material, opening)
	if at < 0 {
		return ""
	}
	rest := material[at+len(opening):]
	if line := strings.IndexAny(rest, " \n"); line > 0 {
		return rest[:line]
	}
	return ""
}

// sendRigReceipt reads the receipt the model was served for one call: the user material
// the runtime commits beside the call result, never the call acceptance the protocol
// renders as the tool message, so a check reading a receipt can never read the
// acceptance in its place.
func sendRigReceipt(request scriptedRequest, callID string) (string, bool) {
	for index, message := range request.Messages {
		if message.ToolCallID != callID || message.Role != "tool" {
			continue
		}
		for _, later := range request.Messages[index+1:] {
			if later.Role == "user" {
				return later.Content, true
			}
		}
	}
	return "", false
}

// sendRigAwaitReceipt waits for the result the model read beside one call, failing the
// check when the window closes first.
func sendRigAwaitReceipt(t *testing.T, rig *sendRig, callID string) string {
	t.Helper()
	deadline := time.Now().Add(sendRigWindow)
	for {
		for _, request := range rig.provider.servedRequests() {
			if receipt, ok := sendRigReceipt(request, callID); ok {
				return receipt
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no decision of the rig read the result of %q within %s", callID, sendRigWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sendRigAwaitReviewerBody waits for a reviewing request that carried the re-entry body,
// the second round the send opened.
func sendRigAwaitReviewerBody(t *testing.T, rig *sendRig) {
	t.Helper()
	deadline := time.Now().Add(sendRigWindow)
	for {
		for _, request := range rig.provider.servedRequests() {
			if request.Model == sendRigReviewerModel && sendRigCarries(request, sendRigBody) {
				return
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("no reviewing round carried %q within %s", sendRigBody, sendRigWindow)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sendRigSettledTaskID reports the task of the last settled notification the chain holds.
func sendRigSettledTaskID(host *assembly.Host, addr event.Address) event.TaskID {
	facts := host.Run().SessionFacts(addr)
	for at := len(facts) - 1; at >= 0; at-- {
		if facts[at].Type != event.TypeTaskSettled || facts[at].TaskID == "" {
			continue
		}
		if facts[at].Meta[event.MetaKeyNoticeKind] != "settled" {
			continue
		}
		return facts[at].TaskID
	}
	return ""
}

// sendRigNoticeMaterials reports the notice materials the chain holds for one task,
// keyed by the four-part invocation identity of the notification they drive.
func sendRigNoticeMaterials(host *assembly.Host, addr event.Address, taskID event.TaskID) map[event.InvocationID]string {
	out := map[event.InvocationID]string{}
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type != event.TypeFeedback || fact.TaskID != taskID {
			continue
		}
		if !strings.HasPrefix(string(fact.InvocationID), "notice:") {
			continue
		}
		out[fact.InvocationID] = fact.Message.Content
	}
	return out
}

// sendRigNoticeInvocation names the invocation one settled notification of a round
// drives under: task, round, kind and sequence, the four-part identity the runtime mints.
func sendRigNoticeInvocation(taskID event.TaskID, round int) event.InvocationID {
	return event.InvocationID("notice:" + string(taskID) + ":" + strconv.Itoa(round) + ":settled:0")
}
