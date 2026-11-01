// The projection acceptance tests of the attention lifecycle change: the
// ObjectAttention dimension stands beside the two symptom anchors, so a notice's
// validity, an approval's answer, the failed drain and a delivered summary are
// each read off the projection instead of off a face of their own.
package host

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// attentionDetails reads the ObjectAttention dimension of the booted run for one
// address, so a case reads the presence exactly as the projection answers it.
func attentionDetails(t *testing.T, ctx context.Context, host *assembly.Host, addr event.Address) []query.Detail {
	t.Helper()
	snapshot, err := host.Run().Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectAttention},
	})
	if err != nil {
		t.Fatalf("read the attention dimension: %v", err)
	}
	return snapshot.Details
}

// attentionOfKind keeps the attention details of one kind: the presence of one
// kind is read off the very entries the dimension answers.
func attentionOfKind(details []query.Detail, kind string) []query.Detail {
	kept := make([]query.Detail, 0, len(details))
	for _, detail := range details {
		if detail.Ref.AttentionQuestion().Kind == kind {
			kept = append(kept, detail)
		}
	}
	return kept
}

// noticePhases reads the ObjectNotice phases of one address, so a case tells
// whether the attention reading changed what the notice dimension reports.
func noticePhases(t *testing.T, ctx context.Context, host *assembly.Host, addr event.Address) []query.NoticePhase {
	t.Helper()
	snapshot, err := host.Run().Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectNotice},
	})
	if err != nil {
		t.Fatalf("read the notice dimension: %v", err)
	}
	phases := make([]query.NoticePhase, 0, len(snapshot.Details))
	for _, detail := range snapshot.Details {
		phases = append(phases, detail.State.NoticePhase)
	}
	return phases
}

// bootNoticeHost boots a host whose entry agent delegates to a worker, so the
// delegated task settles a notification into the entry mailbox. The scripted
// endpoint is the caller's, so a case scripts the delegation beside the
// follow-up turn its own way.
func bootNoticeHost(t *testing.T, provider *scriptedProvider) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

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
	host, err := assembly.Boot(configPath, assembly.WithSender(&deliveryRecorder{}))
	if err != nil {
		t.Fatalf("boot the delegating host: %v", err)
	}
	return host
}

// bootApprovalHost boots a host whose send call is held at the critical grade,
// so one approval question stands in the attention presence until it is
// answered. The scripted endpoint is the caller's, so a case scripts the held
// turn beside whatever it parks it on.
func bootApprovalHost(t *testing.T, provider *scriptedProvider) (*assembly.Host, *approval.ApprovalManager) {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

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
	host, err := assembly.Boot(configPath, assembly.WithSender(&deliveryRecorder{}), assembly.WithApprovals(approvals))
	if err != nil {
		t.Fatalf("boot a document declaring a critical risk rule: %v", err)
	}
	return host, approvals
}

// TestNoticeExpiryLeavesAttention pins the notice validity: a notification nobody consumed leaves the presence once its validity runs out.
//
// - the delegated task settles and its notification stands in the presence beside a validity
// - a clock past that validity drops the notification from the presence
// - the ObjectNotice dimension reports the same phases as before the clock moved
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestNoticeExpiryLeavesAttention(t *testing.T) {
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	var released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	defer unblock()

	const workerBody = "the settled worker reports the expiry body"
	provider := newScriptedProvider(t)
	provider.decide = func(request scriptedRequest) (string, error) {
		switch {
		case !scriptedCarriesAgentTool(request, "worker"):
			return scriptedAnswerOf(workerBody), nil
		case !scriptedDelegatedTo(request, "worker"):
			return wakeDelegate("call-attention-notice-expiry", "compute the expiry total"), nil
		}
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		return scriptedTerminalAnswer, nil
	}
	host := bootNoticeHost(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-attention-notice-expiry"}

	driving := make(chan agent.Turn, 1)
	failed := make(chan error, 1)
	go func() {
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			failed <- err
			return
		}
		driving <- turn
	}()
	select {
	case <-arrived:
	case err := <-failed:
		t.Fatalf("invoke the booted host: %v", err)
	case <-ctx.Done():
		t.Fatalf("the settled notification reached no follow-up turn: %v", ctx.Err())
	}

	var presence []query.Detail
	waitForSettle(t, "the unconsumed notification to stand in the attention presence as the question the held follow-up turn leaves", func() bool {
		presence = attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindNotice)
		return len(presence) > 0
	})
	for _, detail := range presence {
		question := detail.Ref.AttentionQuestion()
		if question.ExpiresMs == 0 {
			t.Fatalf("the notification question %q carries no validity, want the read face to name expires_ms", question.Lineage)
		}
		if phase := detail.State.AttentionPhase(); phase != query.AttentionPhaseQuestion {
			t.Fatalf("the notification question %q stands in phase %q, want the question phase beside a validity still running", question.Lineage, phase)
		}
	}
	before := noticePhases(t, ctx, host, addr)

	restore := assembly.SetAttentionClock(func() time.Time { return time.Now().Add(time.Hour) })
	defer restore()

	if kept := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindNotice); len(kept) != 0 {
		t.Fatalf("the expired notifications stand in the presence as %+v, want every one of them gone", kept)
	}
	after := noticePhases(t, ctx, host, addr)
	if len(after) != len(before) {
		t.Fatalf("notice phases = %v, want the %d phases the notice dimension reported before the validity ran out", after, len(before))
	}
	for at := range after {
		if after[at] != before[at] {
			t.Fatalf("notice phases = %v, want the phases %v the notice dimension reported before the validity ran out", after, before)
		}
	}

	unblock()
	select {
	case turn := <-driving:
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("the entry turn terminal = %v, want the turn to close beside its released follow-up", turn.Terminal)
		}
	case err := <-failed:
		t.Fatalf("invoke beside the released follow-up: %v", err)
	case <-ctx.Done():
		t.Fatalf("the entry turn never closed beside the released follow-up: %v", ctx.Err())
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestApprovalAttentionLeavesProjectionOnAnswer pins the answer clearing: an approval question leaves the ObjectAttention dimension once its answer lands.
//
// - the critical send call is held and stands in the presence as one approval question
// - the answer lands and the presence lists no question of that kind
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestApprovalAttentionLeavesProjectionOnAnswer(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.answer = func(index int) string {
		if index == 0 {
			return scriptedToolCallAnswer
		}
		return scriptedTerminalAnswer
	}
	host, approvals := bootApprovalHost(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-attention-projection-answer"}
	if turn := invokeScriptedTurn(t, ctx, host, string(addr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("the turn terminal = %v, want the turn to close after the held call", turn.Terminal)
	}

	held := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindApproval)
	if len(held) != 1 {
		t.Fatalf("approval questions = %+v, want exactly the one the held call raised", held)
	}
	if phase := held[0].State.AttentionPhase(); phase != query.AttentionPhaseQuestion {
		t.Fatalf("the held approval stands in phase %q, want the question phase", phase)
	}
	pending := approvals.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending approvals = %+v, want the one request the critical call registered", pending)
	}
	if _, err := approvals.Decide(pending[0].ID, approval.ApprovalApproved, "projection-anchor"); err != nil {
		t.Fatalf("answer the held request: %v", err)
	}

	if kept := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindApproval); len(kept) != 0 {
		t.Fatalf("the answered approval stands in the presence as %+v, want the question gone once its answer landed", kept)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestWorkEndClearsNothingOnFailedDrain pins the clearing gate of the work end: a close that drains no obligation clears nothing.
//
// - the critical send call is held and stands in the presence beside the model production that parks the drain
// - the close reports the unsettled drain it could not finish
// - the approval question still stands in the presence after that failed drain
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func TestWorkEndClearsNothingOnFailedDrain(t *testing.T) {
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	var released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	defer unblock()
	provider := newScriptedProvider(t)
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Index == 0 {
			return scriptedToolCallAnswer, nil
		}
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		return scriptedTerminalAnswer, nil
	}
	host, approvals := bootApprovalHost(t, provider)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-attention-failed-drain"}

	driving := make(chan agent.Turn, 1)
	failed := make(chan error, 1)
	go func() {
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			failed <- err
			return
		}
		driving <- turn
	}()
	select {
	case <-arrived:
	case err := <-failed:
		t.Fatalf("invoke the booted host: %v", err)
	case <-ctx.Done():
		t.Fatalf("the held turn reached no parked decision: %v", ctx.Err())
	}

	if held := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindApproval); len(held) != 1 {
		t.Fatalf("approval questions = %+v, want exactly the one the held call raised", held)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	report, err := host.Run().Close(closeCtx, agent.CloseRequest{Deadline: time.Now().Add(3 * time.Second)})
	if err != nil || report.Outcome != agent.CloseCut || report.Drained {
		t.Fatalf("close beside the parked model = (%+v, %v), want the close cut with the work still on the chain", report, err)
	}

	kept := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindApproval)
	if len(kept) != 1 {
		t.Fatalf("approval questions after the failed drain = %+v, want the unsettled question still in the presence", kept)
	}

	unblock()
	select {
	case <-driving:
	case err := <-failed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("invoke beside the released decision: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("the held turn never closed beside its released decision: %v", ctx.Err())
	}
	if pending := approvals.Pending(); len(pending) == 1 {
		if _, err := approvals.Decide(pending[0].ID, approval.ApprovalApproved, "failed-drain-anchor"); err != nil {
			t.Fatalf("answer the held request after the failed drain: %v", err)
		}
	}
}

// TestSummaryAttentionClearsWhenRead pins the summary presence: it stands until the target session answers, and leaves when the work ends unread.
//
// - the delivery fact lands in the target chain and stands in the presence as one summary
// - the target session's next output drops it from the presence while the delivery fact stays on the chain
// - a second summary nobody read leaves the presence once the host closes over a drained work
//
// 契约: docs/wiki/mechanism-tour.md#冥想
func TestSummaryAttentionClearsWhenRead(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")

	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedTerminalAnswer }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, wiringHostDocument(root, provider.endpoint(), "", ""))
	host, err := assembly.Boot(configPath, assembly.WithSender(&deliveryRecorder{}))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-attention-summary-read"}

	delivered, err := host.Run().CommitToSession(ctx, addr, reflectionDelivery(addr, "the reflection body the session reads"))
	if err != nil {
		t.Fatalf("deliver one reflection summary into the target chain: %v", err)
	}
	presence := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindSummary)
	if len(presence) != 1 {
		t.Fatalf("summary questions = %+v, want exactly the one delivery that landed", presence)
	}
	if got := presence[0].Ref.AttentionQuestion().Target; got != string(addr.AgentID)+"/"+string(addr.SessionID) {
		t.Fatalf("summary target = %q, want the delivered session named as one target", got)
	}

	if turn := invokeScriptedTurn(t, ctx, host, string(addr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("the target turn terminal = %v, want the session to answer the delivered summary", turn.Terminal)
	}
	if kept := attentionOfKind(attentionDetails(t, ctx, host, addr), task.AttentionKindSummary); len(kept) != 0 {
		t.Fatalf("the read summary stands in the presence as %+v, want it gone once the target session answered", kept)
	}
	found := false
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Key == delivered {
			found = true
		}
	}
	if !found {
		t.Fatalf("the delivery fact %q left the chain, want the delivery itself kept beside the cleared presence", delivered)
	}

	unread := event.Address{AgentID: "user", SessionID: "session-attention-summary-unread"}
	if _, err := host.Run().CommitToSession(ctx, unread, reflectionDelivery(unread, "the reflection body nobody reads")); err != nil {
		t.Fatalf("deliver one reflection summary nobody reads: %v", err)
	}
	if presence := attentionOfKind(attentionDetails(t, ctx, host, unread), task.AttentionKindSummary); len(presence) != 1 {
		t.Fatalf("summary questions beside the unread delivery = %+v, want the one summary still standing", presence)
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	if err := host.Close(closeCtx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
	if kept := attentionOfKind(attentionDetails(t, context.Background(), host, unread), task.AttentionKindSummary); len(kept) != 0 {
		t.Fatalf("the unread summary stands in the presence after the work ended as %+v, want it gone with the work", kept)
	}
}

// reflectionDelivery renders one summary delivery the way the meditation tool
// commits it: an external input carrying the reflection lineage into the target
// address, so the presence of the summary kind is read off that very fact.
func reflectionDelivery(addr event.Address, content string) event.Event {
	return event.Event{
		Type:      event.TypeExternalInput,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource: meditation.LineageReflection,
			event.MetaKeyEventSummary:  "the reflection summary",
		},
		Message: model.Message{Role: model.RoleUser, Content: content},
		Owner:   addr,
		Source:  addr,
		Target:  addr,
	}
}
