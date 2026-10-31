// The symptom anchors of the attention lifecycle change: two user-voice
// scenes that stand red until the unified attention primitive lands — a
// settled approval is gone for every reader once its answer lands, and a
// notification nobody consumed is gone once the work ends.
package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// TestAttentionClearsApprovalOnAnswer is the approval symptom anchor: once a
// held call is answered, the question it raised is gone for every reader —
// the request directory the external approver watches carries no file for it,
// and the lookup face the gate reads reports no standing approval.
//
// - the critical send call is held and answered as approved
// - the request file under the approval directory is gone after the answer
// - the lookup face reports the answered request as nothing at all
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestAttentionClearsApprovalOnAnswer(t *testing.T) {
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
	agentBlock := "    model: model-a\n" +
		"    tools:\n" +
		"      - { kind: tool, id: send }\n"
	topBlock := "governance:\n" +
		"  risk_rules:\n" +
		"    - { tool: send, level: critical }\n"
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), agentBlock, topBlock))
	approvals := approval.NewApprovalManager(root, time.Hour)
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithApprovals(approvals))
	if err != nil {
		t.Fatalf("boot a document declaring a critical risk rule: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if turn := invokeScriptedTurn(t, ctx, host, "session-attention-approval"); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the turn to close after the held call", turn.Terminal)
	}
	pending := approvals.Pending()
	if len(pending) != 1 || pending[0].Status != approval.ApprovalPending {
		t.Fatalf("pending approvals = %+v, want the one request the critical call registered", pending)
	}
	held := pending[0]
	if held.RiskLevel != plugin.RiskCritical {
		t.Fatalf("pending request = %+v, want the send call held at the critical grade", held)
	}
	if _, err := approvals.Decide(held.ID, approval.ApprovalApproved, "attention-anchor"); err != nil {
		t.Fatalf("answer the held request: %v", err)
	}

	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			t.Fatalf("the answered approval %s still stands in the request directory as %q: an answered question is gone for every reader", held.ID, entry.Name())
		}
	}
	if decided := approvals.Lookup(held.ToolName, held.ArgsDigest); decided != nil && decided.Status == approval.ApprovalApproved {
		t.Fatalf("the lookup face still reports the answered approval %s as approved: an answered question is gone for every reader", held.ID)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestAttentionClearsNoticeOnWorkEnd is the work-end symptom anchor: a
// notification nobody consumed is gone once the host's work ends — after the
// close drains, the notice dimension of the read face lists it nowhere.
// The delegation settles a real notification, and the follow-up turn it fires
// is answered by a refusal, so the notice stands unconsumed exactly as the
// anchor's scene requires and the close measures the work-end clearance alone.
//
// - the delegated task settles and the notification fires its follow-up turn
// - the refusal leaves the notification unconsumed when the close begins
// - the notice dimension lists the notification nowhere after the drain
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionClearsNoticeOnWorkEnd(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	provider := newScriptedProvider(t)
	calls := 0
	provider.answer = func(index int) string {
		calls++
		switch calls {
		case 1:
			return wakeDelegate("call-attention-worker", "compute the anchor total")
		case 2:
			return scriptedAnswerOf("the settled worker reports the anchor body")
		case 3:
			return scriptedAnswerOf("the entry closed over its delegation")
		default:
			return scriptedRefusalAnswer("the follow-up model call refused")
		}
	}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry",
		"  user:\n"+
			"    model: model-entry\n"+
			"    max_tokens: 32000\n"+
			"    tools:\n"+
			"      - { kind: agent, agent: worker }\n"+
			"  worker:\n"+
			"    model: model-entry\n"))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-attention-work-end"}
	if turn := invokeScriptedTurn(t, ctx, host, string(addr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("the entry turn terminal = %v, want the turn to close after its delegation", turn.Terminal)
	}
	waitForSettle(t, "the refused follow-up turn to run", func() bool {
		return len(provider.servedModels()) >= 3
	})

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("drain the work to its end: %v", err)
	}

	snapshot, err := host.Run().Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectNotice},
	})
	if err != nil {
		t.Fatalf("read the notice dimension after the drain: %v", err)
	}
	for _, detail := range snapshot.Details {
		if detail.Ref.Kind == query.ObjectNotice && detail.State.NoticePhase != query.NoticePhaseConsumed {
			t.Fatalf("an unconsumed notification of task %q still stands after the work ended (phase %q): a question the work ended on is gone for every reader",
				detail.Ref.Notice.TaskID, detail.State.NoticePhase)
		}
	}
}
