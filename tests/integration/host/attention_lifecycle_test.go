// The symptom anchors of the attention lifecycle change: two user-voice
// scenes that stand red until the unified attention primitive lands — a
// settled approval is gone for every reader once its answer lands, and a
// notification nobody consumed is gone once the work ends.
package host

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// TestAttentionClearsApprovalOnAnswer pins the approval symptom anchor: an answered approval is gone for every reader.
//
// - the critical send call is held and answered as approved
// - the pending face lists the answered request nowhere after the answer
// - the request file stays a channel record that no presence face reads
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
	if present := approvals.AttentionPresence(ctx); len(present) != 1 {
		t.Fatalf("the held approval stands in the attention presence as %+v, want exactly the one question it raised", present)
	}
	if _, err := approvals.Decide(held.ID, approval.ApprovalApproved, "attention-anchor"); err != nil {
		t.Fatalf("answer the held request: %v", err)
	}

	if pending := approvals.Pending(); len(pending) != 0 {
		t.Fatalf("the answered approval %s still stands on the pending face: %+v", held.ID, pending)
	}
	if present := approvals.AttentionPresence(ctx); len(present) != 0 {
		t.Fatalf("the answered approval %s still stands in the attention presence: %+v", held.ID, present)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestAttentionClearsNoticeOnWorkEnd pins the work-end symptom anchor: an unconsumed notification is gone once the host's work ends.
//
// - the delegated task settles and its notification stands on the chain
// - the close drains the work to its end without the notification pending
// - the notice dimension lists the notification nowhere after the drain
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionClearsNoticeOnWorkEnd(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	const (
		workerBody = "the settled worker reports the anchor body"
		entryBody  = "the entry closed over its delegation"
	)
	provider := newScriptedProvider(t)
	provider.decide = func(request scriptedRequest) (string, error) {
		switch {
		case !scriptedCarriesAgentTool(request, "worker"):
			return scriptedAnswerOf(workerBody), nil
		case settleDriveReadsMaterial(request, workerBody):
			return scriptedAnswerOf("the follow-up carried the settlement to the user"), nil
		case scriptedDelegatedTo(request, "worker"):
			return scriptedAnswerOf(entryBody), nil
		default:
			return wakeDelegate("call-attention-worker", "compute the anchor total"), nil
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
	waitForSettle(t, "the notice material to land on the chain", func() bool {
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyTriggerSource] == "task" {
				return true
			}
		}
		return false
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

// TestAttentionNoticeQuestionClearsOnWorkEnd pins the notice question leaving the attention presence once the work ends.
//
// - the delegated task settles and its notification stands on the chain unconsumed while its follow-up is held
// - the attention dimension reports that notification as one notice question before the close
// - the close drains the held follow-up, and the same read reports no notice question afterwards
// - the assertion reads the ObjectAttention dimension, not the ObjectNotice phase the merge line drains
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionNoticeQuestionClearsOnWorkEnd(t *testing.T) {
	host, addr, followUp, releaseFollowUp := bootUnconsumedNoticeScene(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	select {
	case <-followUp:
	case <-ctx.Done():
		t.Fatalf("the held follow-up of the unconsumed notification never reached its provider: %v", ctx.Err())
	}

	before := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindNotice)
	if len(before) != 1 {
		t.Fatalf("notice questions before the close = %d, want the one unconsumed notification the chain holds", len(before))
	}
	if phase := before[0].State.AttentionPhase(); phase != query.AttentionPhaseQuestion {
		t.Fatalf("notice question phase before the close = %q, want the question phase", phase)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	closed := make(chan error, 1)
	begun := make(chan struct{})
	go func() {
		close(begun)
		closed <- host.Close(closeCtx)
	}()
	<-begun
	time.Sleep(300 * time.Millisecond)
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "one more request"}},
	}); !errors.Is(err, assembly.ErrHostClosed) {
		t.Fatalf("invoke after the close began = %v, want the intake refusal ErrHostClosed", err)
	}
	releaseFollowUp()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("drain the work to its end: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("the close never returned beside the released follow-up: %v", ctx.Err())
	}
	if report := host.CloseReport(); report.Outcome != agent.CloseQuiesced || !report.Drained {
		t.Fatalf("close report = (%+v), want the held follow-up drained to rest before the work end is read", report)
	}

	after := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindNotice)
	if len(after) != 0 {
		t.Fatalf("notice questions after the drained close = %d, want none: a question the work ended on is gone from the attention presence", len(after))
	}
}

// bootUnconsumedNoticeScene boots the one delegated scene the notice anchors read: an
// entry agent delegating to a worker through the public boot, one turn that settles
// the delegated task, and the follow-up turn held at the model boundary so the
// notification stands on the chain with nobody consuming it. It answers the host,
// the entry address and the release of the held turn the anchors read, so a scene
// is opened the way a deployment opens it instead of by hand.
//
// The script answers by invocation rather than by call ordinal. The notification
// really lands and its follow-up really arrives, and the turn is held instead of
// refused: a refusal is a consumption result of its own since the A+ close (a
// failed answer settles the notice), so it cannot leave the notification
// unconsumed. The held turn is what the close's drain owes, so the work end is the
// gate that settles the notice the case read as a question.
func bootUnconsumedNoticeScene(t *testing.T) (host *assembly.Host, addr event.Address, followUp <-chan struct{}, releaseFollowUp func()) {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	const (
		workerBody     = "the settled worker reports the anchor body"
		entryBody      = "the entry closed over its delegation"
		followUpAnswer = "the held follow-up carried the settlement before the work ended"
	)
	provider := newScriptedProvider(t)
	arrived := make(chan struct{})
	held := make(chan struct{})
	var arrivedOnce, releaseOnce sync.Once
	provider.decide = func(request scriptedRequest) (string, error) {
		switch {
		case !scriptedCarriesAgentTool(request, "worker"):
			return scriptedAnswerOf(workerBody), nil
		case settleDriveReadsMaterial(request, workerBody):
			arrivedOnce.Do(func() { close(arrived) })
			<-held
			return scriptedAnswerOf(followUpAnswer), nil
		case scriptedDelegatedTo(request, "worker"):
			return scriptedAnswerOf(entryBody), nil
		default:
			return wakeDelegate("call-attention-worker", "compute the anchor total"), nil
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
	invokeCtx, invokeCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer invokeCancel()
	addr = event.Address{AgentID: "user", SessionID: "session-attention-work-end"}
	if turn := invokeScriptedTurn(t, invokeCtx, host, string(addr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("the entry turn terminal = %v, want the turn to close after its delegation", turn.Terminal)
	}
	waitForSettle(t, "the notice material to land on the chain", func() bool {
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyTriggerSource] == "task" {
				return true
			}
		}
		return false
	})
	return host, addr, arrived, func() { releaseOnce.Do(func() { close(held) }) }
}
