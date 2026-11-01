// The governance gate read from the attention side: the critical call one
// document declares is held as an attention question of the approval manager, and
// the ruling the retry reads is the answer that question carries — through the
// public boot entry, the declared risk table and the wired approvals manager.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// gateHeldCallAnswer is the first held send call of the gate scenes: it names the
// one session destination the approved document's white list declares, so its digest
// is the one the retry repeats and the ruling binds.
// gateRetryCallAnswer is the retry of one held send call: the same arguments under
// a fresh call identity, the shape a model issues when it retries a call it was
// told to wait for.
const gateHeldCallAnswer = `{"id":"scripted-tool-first","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"sending","tool_calls":[{"id":"call-1","type":"function","function":{"name":"send","arguments":"{\"target\":{\"agent\":\"user\",\"session\":\"session-attention-gate-approved\"},\"content\":\"hi\"}"}}]},"finish_reason":"tool_calls"}]}`
const gateRetryCallAnswer = `{"id":"scripted-tool-retry","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"sending","tool_calls":[{"id":"call-1-retry","type":"function","function":{"name":"send","arguments":"{\"target\":{\"agent\":\"user\",\"session\":\"session-attention-gate-approved\"},\"content\":\"hi\"}"}}]},"finish_reason":"tool_calls"}]}`

// gateCriticalAgentBlock declares the one send tool every gate scene holds.
const gateCriticalAgentBlock = "    model: model-a\n" +
	"    tools:\n" +
	"      - { kind: tool, id: send }\n"

// gateApprovedAgentBlock declares the one send tool the approved scene holds beside
// the one delivery white list its destination is judged against: the reflection
// section names the agent's own partition, which is the face the observed gate
// reads, so the destination the call names is a session the door lets through
// instead of one it refuses beside no declared table.
func gateApprovedAgentBlock(promptPath string) string {
	return gateCriticalAgentBlock +
		"    meditation:\n" +
		"      enabled: true\n" +
		"      interval: 1h\n" +
		"      min_gap: 0s\n" +
		"      prompt_file: " + promptPath + "\n" +
		"      observed_namespaces: [user]\n" +
		"      deliver_to: [user]\n"
}

// gateMeditationPrompt is the static reflection prompt the approved document
// declares, so the boot reads one beside the white list above.
const gateMeditationPrompt = "Reflect over the gate scene and deliver one body.\n"

// gateCriticalTopBlock declares the critical grade the gate renders its hold at.
const gateCriticalTopBlock = "governance:\n" +
	"  risk_rules:\n" +
	"    - { tool: send, level: critical }\n"

// TestGateReadsAttentionRuling pins the governance gate reading its ruling off the attention facts.
// - a critical call parks as one waiting question and delivers nothing while it waits
// - the approval closes that question, and the retry of the same arguments runs
// - a refusal closes it too and no retry of the refused arguments runs
// - a manager rebuilt over the same directory reads the same ruling back
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestGateReadsAttentionRuling(t *testing.T) {
	t.Run("an approved ruling lets the held call run", func(t *testing.T) {
		t.Setenv("TAGENT_TEST_API_KEY", "test-key")
		t.Setenv("TAGENT_HTTP_PORT", "0")
		t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
		provider := newScriptedProvider(t)
		provider.answer = func(index int) string {
			switch index {
			case 0:
				return gateHeldCallAnswer
			case 2:
				return gateRetryCallAnswer
			default:
				return scriptedTerminalAnswer
			}
		}
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		promptPath := filepath.Join(root, "meditation.md")
		writeHostDocumentFile(t, promptPath, gateMeditationPrompt)
		writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), gateApprovedAgentBlock(promptPath), gateCriticalTopBlock))
		destination := event.Address{AgentID: "user", SessionID: "session-attention-gate-approved"}
		approvals := approval.NewApprovalManager(root, time.Hour)
		sender := &deliveryRecorder{}
		host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithApprovals(approvals))
		if err != nil {
			t.Fatalf("boot a document declaring a critical risk rule: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if turn := invokeScriptedTurn(t, ctx, host, "session-attention-gate-approved"); turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the turn to close after the held call", turn.Terminal)
		}
		pending := approvals.Pending()
		if len(pending) != 1 || pending[0].Status != approval.ApprovalPending {
			t.Fatalf("pending approvals = %+v, want the one question the critical call parked", pending)
		}
		held := pending[0]
		if got := gateDeliveredBodies(host, destination, "hi"); got != 0 {
			t.Fatalf("delivered bodies at %+v while the call waits = %d, want the held call to execute nothing", destination, got)
		}
		for _, body := range sender.bodies() {
			if body == "hi" {
				t.Fatalf("delivered channel bodies while the call waits = %q, want the held call to execute nothing", sender.bodies())
			}
		}
		if seen := approvals.Lookup(held.ToolName, held.ArgsDigest); seen == nil || seen.Status != approval.ApprovalPending {
			t.Fatalf("lookup of the held arguments = %+v, want the wait the gate registered", seen)
		}
		if _, err := approvals.Decide(held.ID, approval.ApprovalApproved, "the-gate-approver"); err != nil {
			t.Fatalf("approve the held call: %v", err)
		}
		if pending := approvals.Pending(); len(pending) != 0 {
			t.Fatalf("pending approvals after the ruling = %+v, want the answered question gone", pending)
		}
		if seen := approvals.Lookup(held.ToolName, held.ArgsDigest); seen == nil || seen.Status != approval.ApprovalApproved {
			t.Fatalf("lookup after the ruling = %+v, want the answer the question carries", seen)
		}

		if turn := invokeScriptedTurn(t, ctx, host, "session-attention-gate-approved"); turn.Terminal != model.KindCompleted {
			t.Fatalf("retry turn terminal = %v, want the approved retry to close", turn.Terminal)
		}
		if got := gateDeliveredBodies(host, destination, "hi"); got != 1 {
			t.Fatalf("delivered bodies at %+v after the approval = %d, want the approved retry to run exactly once", destination, got)
		}
		rebuilt := approval.NewApprovalManager(root, time.Hour)
		restored := rebuilt.Lookup(held.ToolName, held.ArgsDigest)
		if restored == nil || restored.ID != held.ID || restored.Status != approval.ApprovalApproved {
			t.Fatalf("restarted lookup = %+v, want the ruling read back over the same directory", restored)
		}
		closeAttentionGate(t, host)
	})

	t.Run("a denied ruling never runs the held call", func(t *testing.T) {
		t.Setenv("TAGENT_TEST_API_KEY", "test-key")
		t.Setenv("TAGENT_HTTP_PORT", "0")
		t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
		provider := newScriptedProvider(t)
		provider.answer = func(index int) string {
			switch index {
			case 0:
				return scriptedToolCallAnswer
			case 2:
				return gateRetryCallAnswer
			default:
				return scriptedTerminalAnswer
			}
		}
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), gateCriticalAgentBlock, gateCriticalTopBlock))
		approvals := approval.NewApprovalManager(root, time.Hour)
		sender := &deliveryRecorder{}
		host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithApprovals(approvals))
		if err != nil {
			t.Fatalf("boot a document declaring a critical risk rule: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if turn := invokeScriptedTurn(t, ctx, host, "session-attention-gate-denied"); turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the turn to close after the held call", turn.Terminal)
		}
		pending := approvals.Pending()
		if len(pending) != 1 {
			t.Fatalf("pending approvals = %+v, want the one question the critical call parked", pending)
		}
		held := pending[0]
		if _, err := approvals.Decide(held.ID, approval.ApprovalDenied, "the-gate-approver"); err != nil {
			t.Fatalf("deny the held call: %v", err)
		}
		if turn := invokeScriptedTurn(t, ctx, host, "session-attention-gate-denied"); turn.Terminal != model.KindCompleted {
			t.Fatalf("retry turn terminal = %v, want the refused retry to close", turn.Terminal)
		}
		for _, body := range sender.bodies() {
			if body == "hi" {
				t.Fatalf("delivered bodies after the refusal = %q, want the refused call to run nothing", sender.bodies())
			}
		}
		if seen := approvals.Lookup(held.ToolName, held.ArgsDigest); seen == nil || seen.Status != approval.ApprovalDenied {
			t.Fatalf("lookup after the refusal = %+v, want the answer the question carries", seen)
		}
		closeAttentionGate(t, host)
	})
}

// TestGateFailsClosedWithoutAttention pins the gate refusing or holding rather than running.
// - a document beside no wired approval channel refuses the critical call and delivers nothing
// - a request directory that cannot be written leaves the wait standing beside the named failure, and delivers nothing
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestGateFailsClosedWithoutAttention(t *testing.T) {
	t.Run("no approval channel refuses the critical call", func(t *testing.T) {
		t.Setenv("TAGENT_TEST_API_KEY", "test-key")
		t.Setenv("TAGENT_HTTP_PORT", "0")
		t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
		provider := newScriptedProvider(t)
		provider.answer = func(index int) string {
			if index == 0 {
				return scriptedToolCallAnswer
			}
			return scriptedTerminalAnswer
		}
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		document := wiringHostDocument(root, provider.endpoint(), gateCriticalAgentBlock, gateCriticalTopBlock)
		document = strings.Replace(document, "storage: { type: localfile, root: "+root+" }\n", "", 1)
		writeHostDocumentFile(t, configPath, document)
		sender := &deliveryRecorder{}
		host, err := assembly.Boot(configPath, assembly.WithSender(sender))
		if err != nil {
			t.Fatalf("boot a document declaring a critical risk rule beside no approval channel: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if turn := invokeScriptedTurn(t, ctx, host, "session-attention-gate-unwired"); turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the refused call to close the turn", turn.Terminal)
		}
		for _, body := range sender.bodies() {
			if body == "hi" {
				t.Fatalf("delivered bodies beside no approval channel = %q, want the critical call to run nothing", sender.bodies())
			}
		}
		waitSeen, refusalSeen := false, false
		for _, body := range provider.recorded() {
			for _, message := range settleRequestTexts(t, body) {
				if strings.Contains(message.content, "no approval channel is wired") {
					refusalSeen = true
				}
				if strings.Contains(message.content, "parked awaiting approval") {
					waitSeen = true
				}
			}
		}
		if !refusalSeen || waitSeen {
			t.Fatalf("model requests carried the refusal %v and the wait %v, want the refusal alone: a call beside no channel never parks", refusalSeen, waitSeen)
		}
		closeAttentionGate(t, host)
	})

	t.Run("an unwritable request directory keeps the wait standing", func(t *testing.T) {
		t.Setenv("TAGENT_TEST_API_KEY", "test-key")
		t.Setenv("TAGENT_HTTP_PORT", "0")
		t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
		provider := newScriptedProvider(t)
		provider.answer = func(index int) string {
			if index == 0 {
				return scriptedToolCallAnswer
			}
			return scriptedTerminalAnswer
		}
		root := t.TempDir()
		configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
		writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), gateCriticalAgentBlock, gateCriticalTopBlock))
		blocked := filepath.Join(root, "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("place a file where the approvals directory would be: %v", err)
		}
		approvals := approval.NewApprovalManager(blocked, time.Hour)
		sender := &deliveryRecorder{}
		host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithApprovals(approvals))
		if err != nil {
			t.Fatalf("boot a document declaring a critical risk rule: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if turn := invokeScriptedTurn(t, ctx, host, "session-attention-gate-unwritable"); turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the turn to close after the hold", turn.Terminal)
		}
		for _, body := range sender.bodies() {
			if body == "hi" {
				t.Fatalf("delivered bodies beside an unwritable request directory = %q, want the critical call to run nothing", sender.bodies())
			}
		}
		pending := approvals.Pending()
		if len(pending) != 1 || pending[0].Status != approval.ApprovalPending {
			t.Fatalf("pending approvals = %+v, want the wait to stand beside the persistence failure", pending)
		}
		if !strings.Contains(pending[0].Reason, "critical risk awaits approval") {
			t.Fatalf("waiting reason = %q, want the wait the critical grade renders", pending[0].Reason)
		}
		closeAttentionGate(t, host)
	})
}

// gateDeliveredBodies counts the bodies one delivery landed in the destination the
// gate scene names, read off the destination chain the way a delivery stamps it:
// the trigger source of the body, the destination it was handed to and the content
// the call carried. A held call leaves the count at zero and an approved retry
// leaves exactly one, so the scene reads the run of the call instead of a receipt.
func gateDeliveredBodies(host *assembly.Host, addr event.Address, content string) int {
	count := 0
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type != event.TypeExternalInput || fact.Meta[event.MetaKeyTriggerSource] != "send" {
			continue
		}
		if fact.Meta[event.MetaKeyDeliveryTarget] != string(addr.AgentID)+"/"+string(addr.SessionID) {
			continue
		}
		if fact.Message.Content == content {
			count++
		}
	}
	return count
}

// closeAttentionGate drains one gate scene under a bounded grace, so a wait the
// scene left open is reported beside the drain instead of holding the anchor until
// its own deadline.
func closeAttentionGate(t *testing.T, host *assembly.Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}
