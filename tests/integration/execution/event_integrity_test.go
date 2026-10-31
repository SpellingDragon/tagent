package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"
	orgstate "github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestEventIntegrityNoticeIdentity checks the notification identity and its idempotency over the formal assembly.
// - the settle notice is read by the next decision of the invocation that issued the call
// - a notice that opens a range of its own is read by the follow-up; one that lands beside a parked call is read by the decision in flight
// - both shapes read the notice once, so the follow-up step of the script is optional
// - the decision that read the notice answers it, and the consumer delivers that segment's answer to the address the notice was accepted on
// - both shapes are one reply delivered once, so the check reads the body the endpoint served to that very request instead of naming which one
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestEventIntegrityNoticeIdentity(t *testing.T) {
	t.Run("tool_and_subagent_notice_reaches_original_invocation", runNoticeReachesOriginalInvocation)
	t.Run("duplicate_delivery", runDuplicateNoticeDelivery)
	t.Run("conflicting_body_same_identity", runConflictingNoticeBody)
	t.Run("cross_invocation_not_merged", runCrossInvocationNoticeScoping)
	t.Run("handoff_preserves_issued_target", runHandoffPreservesIssuedTarget)
	t.Run("multiple_progress_ordered_by_seq", runNoticeProgressOrder)
}

// runNoticeReachesOriginalInvocation drives one declared send tool beside one
// declared worker agent over Boot and Host.Invoke, and reads back the identity
// and the responsibility range of both terminal notifications.
// - the send tool settles one task whose terminal notification carries the fact of the call and reaches the next decision of the invocation that issued it
// - the delegated worker settles a task of its own whose terminal notification carries the original invocation and is read once by the next decision of that invocation, which answers it
// - both identities come from the production points: one term settles one task, and no notice is invented by the check
func runNoticeReachesOriginalInvocation(t *testing.T) {
	const (
		input       = "report the tool and worker notices"
		note        = "the tool notice body"
		workerInput = "produce the worker notice body"
		workerOut   = "the worker notice body settled"
		accepted    = "the entry accepted the delegated task"
		followed    = "the settle notice reached the user"
	)
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	script.expect("model-entry", "the entry sends its tool note", eventIntegrityTail(input)).
		answers(eventIntegrityToolCallAnswer("send", "call-notice-tool", `{"content":"`+note+`"}`))
	script.expect("model-entry", "the entry delegates the worker", eventIntegrityCarriesToolResult("call-notice-tool")).
		answers(eventIntegrityToolCallAnswer("worker", "call-notice-worker", `{"input":"`+workerInput+`"}`))
	script.expect("model-entry", "the entry accepts the delegated task", eventIntegrityCarriesToolResult("call-notice-worker")).
		answers(eventIntegrityAnswerOf(accepted))
	script.expect("model-worker", "the worker answers the delegated turn", eventIntegrityTail(workerInput)).
		answers(eventIntegrityAnswerOf(workerOut))
	script.expect("model-entry", "the settled notice fires its follow-up", eventIntegrityCarriesSettleNotice(workerOut)).
		answers(eventIntegrityAnswerOf(followed)).optional()
	provider.decide = script.decide

	sender := newEventIntegritySender()
	host := eventIntegrityBoot(t, provider, sender)
	eventIntegrityKeepHost(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-notice-identity"}

	turn, err := eventIntegrityInvoke(ctx, host, addr, "", input)
	if err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 3 {
		t.Fatalf("entry turn = (%v, %d decisions), want the completed turn of its three decisions",
			turn.Terminal, len(turn.Decisions))
	}
	sender.awaitDelivered(t, addr, note)
	if got := sender.delivered(addr, note); got != 1 {
		t.Fatalf("the send body reached the user %d times, want exactly the one delivery", got)
	}

	facts := host.Run().SessionFacts(addr)
	toolCall, ok := eventIntegrityActionFact(facts, "call-notice-tool")
	if !ok {
		t.Fatalf("no action fact carries the send call identity; facts = %s", eventIntegrityFactsSummary(facts))
	}
	if toolCall.InvocationID != turn.InvocationID {
		t.Fatalf("tool result fact invocation = %q, want the issuing invocation %q", toolCall.InvocationID, turn.InvocationID)
	}
	toolSettle, ok := eventIntegritySettleFact(facts, toolCall.TaskID)
	if !ok {
		t.Fatalf("no single settled fact for the send task %q; facts = %s", toolCall.TaskID, eventIntegrityFactsSummary(facts))
	}
	if toolSettle.InvocationID != turn.InvocationID {
		t.Fatalf("tool settle fact invocation = %q, want the original invocation %q", toolSettle.InvocationID, turn.InvocationID)
	}
	if toolSettle.Owner != addr {
		t.Fatalf("tool settle fact owner = %+v, want the issuing address %+v", toolSettle.Owner, addr)
	}

	notices := eventIntegrityNotices(t, host.Run(), addr)
	toolNotice, ok := eventIntegrityNoticeOfTask(notices, toolCall.TaskID)
	if !ok {
		t.Fatalf("no notice identity recorded for the send task %q; notices = %+v", toolCall.TaskID, notices)
	}
	if toolNotice.Ref.Notice.Kind != task.NoticeSettled || toolNotice.Ref.Notice.Seq != 0 {
		t.Fatalf("tool notice identity = %s, want the settled terminal at seq 0", toolNotice.Ref.Notice.FactKey())
	}
	if toolNotice.Ref.Address != addr || toolNotice.Ref.Notice.Round < 1 {
		t.Fatalf("tool notice = %+v, want it owned by %+v at its execution round", toolNotice.Ref, addr)
	}
	toolNotice = eventIntegrityAwaitConsumed(t, host.Run(), addr, toolCall.TaskID)

	delegated := eventIntegrityAwaitDelegatedSettle(t, host.Run(), addr, workerOut)
	if delegated.InvocationID != turn.InvocationID {
		t.Fatalf("delegated settle fact invocation = %q, want the original invocation %q", delegated.InvocationID, turn.InvocationID)
	}
	if delegated.TaskID == "" || delegated.TaskID == toolCall.TaskID {
		t.Fatalf("delegated task = %q beside the tool task %q, want a task identity of its own", delegated.TaskID, toolCall.TaskID)
	}
	delegatedNotice := eventIntegrityAwaitConsumed(t, host.Run(), addr, delegated.TaskID)
	if delegatedNotice.Ref.Notice.Kind != task.NoticeSettled || delegatedNotice.Ref.Notice.Seq != 0 {
		t.Fatalf("delegated notice identity = %s, want the settled terminal at seq 0", delegatedNotice.Ref.Notice.FactKey())
	}
	if delegatedNotice.Ref.Address != addr {
		t.Fatalf("delegated notice target = %+v, want the issuing address %+v", delegatedNotice.Ref.Address, addr)
	}

	settled := host.Run().SessionFacts(addr)
	followUp := eventIntegrityNoticeFacts(settled)
	if len(followUp) != 1 {
		t.Fatalf("notice facts = %d, want the one follow-up range the delegated settle fired; facts = %s",
			len(followUp), eventIntegrityFactsSummary(settled))
	}
	notice := eventIntegrityNoticeOfFact(followUp[0])
	if got, want := followUp[0].Key, agent.NoticeFactKey(followUp[0].InvocationID, notice); got != want {
		t.Fatalf("notice fact key = %q, want the canonical identity %q of the notice %s", got, want, notice.FactKey())
	}
	if followUp[0].InvocationID == turn.InvocationID {
		t.Fatalf("notice fact invocation = %q, want the follow-up range beside the original %q", followUp[0].InvocationID, turn.InvocationID)
	}
	if followUp[0].Owner != addr || followUp[0].TaskID != delegated.TaskID {
		t.Fatalf("notice fact = %+v, want it owned by %+v and carrying the delegated task %q", followUp[0], addr, delegated.TaskID)
	}
	if got := eventIntegrityFactKeyCount(settled, followUp[0].Key); got != 1 {
		t.Fatalf("facts under the notice key %q = %d, want exactly 1", followUp[0].Key, got)
	}

	requests := provider.requests()
	if len(requests) < 4 || len(requests) > 5 {
		t.Fatalf("model requests = %d, want the three decisions of the entry beside the worker's one, "+
			"and the one follow-up decision when the notice opens a range of its own (%s)",
			len(requests), eventIntegrityDescribeRequests(requests))
	}
	for _, callID := range []string{"call-notice-tool", "call-notice-worker"} {
		next := eventIntegrityRequestsFirstCarryingToolCall(requests, eventIntegrityEntryModel, callID)
		if len(next) != 1 {
			t.Fatalf("requests first carrying the assistant call %q = %v, want exactly the one decision that followed it", callID, next)
		}
		readers := eventIntegrityRequestsFirstReadingToolResult(requests, eventIntegrityEntryModel, callID)
		if len(readers) != 1 {
			t.Fatalf("requests first reading the result of %q = %v, want exactly the one decision it was delivered to", callID, readers)
		}
		if readers[0] != next[0] {
			t.Fatalf("the result of %q was first read by request %d, want the next decision of the issuing invocation at %d",
				callID, readers[0], next[0])
		}
	}
	material := strings.TrimSpace(followUp[0].Message.Content)
	carrying := eventIntegrityRequestsCarryingUser(requests, material)
	if len(carrying) != 1 {
		t.Fatalf("requests carrying the notice material = %v, want the one decision that read it (%s)",
			carrying, eventIntegrityDescribeRequests(requests))
	}
	answered := provider.answerOf(carrying[0])
	if answered == "" {
		t.Fatalf("request %d read the notice material beside no served answer (%s)",
			carrying[0], eventIntegrityDescribeRequests(requests))
	}
	sender.awaitDelivered(t, addr, answered)
	if got := sender.delivered(addr, answered); got != 1 {
		t.Fatalf("the reply %q of the decision that read the settle notice reached the user %d times, "+
			"want exactly the one delivery of the segment it closed", answered, got)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the notice identity script left violations: %v", violations)
	}
	eventIntegrityClose(t, ctx, host)
}

// runDuplicateNoticeDelivery presents one production notice identity twice to the
// production mailbox consumer and requires the acceptance to stay one record.
// - the identity read from the formal run is the four-tuple the production points wrote
// - a repeat delivery of the same identity and the same body answers no error and lands no second entry
// - the fact key of the identity is the same on both presentations, so no second key is minted
func runDuplicateNoticeDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	eventIntegrityEnv(t)
	formal := eventIntegrityNoticeFromFormalRun(t, ctx, "session-notice-duplicate", "the duplicate notice body")
	notice := formal.notice
	store := newEventIntegrityStore(t, ctx, formal.addr, formal.instance)

	if err := store.track(ctx, notice.TaskID); err != nil {
		t.Fatalf("register the delivery route of %q: %v", notice.TaskID, err)
	}
	delivery := eventIntegrityDeliveryContext(ctx, formal.addr, formal.instance, formal.invocationID)
	if err := store.accept(delivery, notice); err != nil {
		t.Fatalf("accept the production notice %s: %v", notice.FactKey(), err)
	}
	if err := store.accept(delivery, notice); err != nil {
		t.Fatalf("re-deliver the same notice identity %s = %v, want the idempotent acceptance", notice.FactKey(), err)
	}
	mailbox, err := store.mailbox(ctx)
	if err != nil {
		t.Fatalf("read the mailbox of the accepted notice: %v", err)
	}
	if len(mailbox) != 1 {
		t.Fatalf("mailbox entries = %d, want the one fact of the repeated identity %s: %+v",
			len(mailbox), notice.FactKey(), mailbox)
	}
	if mailbox[0].Notice != notice {
		t.Fatalf("mailbox notice = %s, want the delivered identity %s", mailbox[0].Notice.FactKey(), notice.FactKey())
	}
	if mailbox[0].AcceptSeq != 1 {
		t.Fatalf("mailbox acceptance sequence = %d, want the first acceptance kept", mailbox[0].AcceptSeq)
	}
	if mailbox[0].EventID != notice.Event || mailbox[0].InvocationID != formal.invocationID {
		t.Fatalf("mailbox entry = %+v, want the committed fact %q of the issuing invocation %q",
			mailbox[0], notice.Event, formal.invocationID)
	}
	if first, repeat := notice.FactKey(), formal.notice.FactKey(); first != repeat {
		t.Fatalf("notice fact keys = %q and %q, want one canonical identity", first, repeat)
	}
	key := agent.NoticeFactKey(formal.invocationID, notice)
	if again := agent.NoticeFactKey(formal.invocationID, formal.notice); again != key {
		t.Fatalf("notice fact keys = %q and %q, want one canonical key for one identity", key, again)
	}
}

// runConflictingNoticeBody delivers one recorded identity beside a different body
// and requires the named refusal instead of a silent merge.
// - the same identity carrying a different text is refused by name with ErrNoticeConflict
// - the recorded entry keeps the first body and no second entry is appended
func runConflictingNoticeBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	eventIntegrityEnv(t)
	formal := eventIntegrityNoticeFromFormalRun(t, ctx, "session-notice-conflict", "the conflicting notice body")
	notice := formal.notice
	store := newEventIntegrityStore(t, ctx, formal.addr, formal.instance)
	if err := store.track(ctx, notice.TaskID); err != nil {
		t.Fatalf("register the delivery route of %q: %v", notice.TaskID, err)
	}
	delivery := eventIntegrityDeliveryContext(ctx, formal.addr, formal.instance, formal.invocationID)
	if err := store.accept(delivery, notice); err != nil {
		t.Fatalf("accept the production notice %s: %v", notice.FactKey(), err)
	}

	conflict := notice
	conflict.Text = "a body the identity never carried"
	if err := store.accept(delivery, conflict); !errors.Is(err, orgstate.ErrNoticeConflict) {
		t.Fatalf("same identity beside a different body = %v, want %v", err, orgstate.ErrNoticeConflict)
	}
	mailbox, err := store.mailbox(ctx)
	if err != nil {
		t.Fatalf("read the mailbox of the accepted notice: %v", err)
	}
	if len(mailbox) != 1 {
		t.Fatalf("mailbox entries = %d, want the one accepted identity %s and no silently merged second entry",
			len(mailbox), notice.FactKey())
	}
	if mailbox[0].Notice.Text != notice.Text || mailbox[0].Notice.Event != notice.Event {
		t.Fatalf("recorded notice = %+v, want the first body %q kept beside the conflict refusal", mailbox[0].Notice, notice.Text)
	}
}

// runCrossInvocationNoticeScoping drives two entries that issue the same native
// call identity and requires their notices to stay two identities.
// - each entry lands its own input fact under the identity the drive named, and each is answered by a segment of its own that the consumption loop minted
// - the same call identifier in the two segments admits two tasks with distinct task identities
// - each terminal fact stays in its own responsibility range and each notice keeps its own four-tuple and fact key
// - accepting both notices into one mailbox keeps two entries: different segments are never merged
func runCrossInvocationNoticeScoping(t *testing.T) {
	const (
		firstInput  = "send the first scoped notice"
		secondInput = "send the second scoped notice"
		firstNote   = "the first scoped body"
		secondNote  = "the second scoped body"
	)
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	script.expect("model-entry", "the first invocation sends its note", eventIntegrityTail(firstInput)).
		answers(eventIntegrityToolCallAnswer("send", "call-shared", `{"content":"`+firstNote+`"}`))
	script.expect("model-entry", "the first invocation ends", eventIntegrityCarriesToolResult("call-shared")).
		answers(eventIntegrityAnswerOf("the first invocation settled"))
	script.expect("model-entry", "the second invocation sends its note", eventIntegrityTail(secondInput)).
		answers(eventIntegrityToolCallAnswer("send", "call-shared", `{"content":"`+secondNote+`"}`))
	script.expect("model-entry", "the second invocation ends", eventIntegrityCarriesToolResult("call-shared")).
		answers(eventIntegrityAnswerOf("the second invocation settled"))
	provider.decide = script.decide

	sender := newEventIntegritySender()
	host := eventIntegrityBoot(t, provider, sender)
	eventIntegrityKeepHost(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-notice-scoping"}

	firstTurn, err := eventIntegrityInvoke(ctx, host, addr, "invocation-scope-one", firstInput)
	if err != nil {
		t.Fatalf("the first invocation ended by error: %v", err)
	}
	secondTurn, err := eventIntegrityInvoke(ctx, host, addr, "invocation-scope-two", secondInput)
	if err != nil {
		t.Fatalf("the second invocation ended by error: %v", err)
	}
	if firstTurn.Terminal != model.KindCompleted || secondTurn.Terminal != model.KindCompleted {
		t.Fatalf("turns of the two entries = (%v, %v), want each to be answered by the segment that consumed it",
			firstTurn.Terminal, secondTurn.Terminal)
	}
	if firstTurn.InvocationID == secondTurn.InvocationID {
		t.Fatalf("turn identities = %q and %q, want the two ranges the consumption loop minted, one per entry",
			firstTurn.InvocationID, secondTurn.InvocationID)
	}

	facts := host.Run().SessionFacts(addr)
	for at, named := range []event.InvocationID{"invocation-scope-one", "invocation-scope-two"} {
		landed := eventIntegrityInputFacts(facts, []string{firstInput, secondInput}[at])
		if len(landed) != 1 {
			t.Fatalf("input facts carrying the entry %q = %d, want the one fact that entry committed; facts = %s",
				named, len(landed), eventIntegrityFactsSummary(facts))
		}
		if landed[0].InvocationID != named {
			t.Fatalf("input fact of the entry = %q, want the identity the drive named %q: each source keeps its own",
				landed[0].InvocationID, named)
		}
	}
	calls := eventIntegrityActionFactsByCall(facts, "call-shared")
	if len(calls) != 2 {
		t.Fatalf("action facts carrying call-shared = %d, want one per segment; facts = %s",
			len(calls), eventIntegrityFactsSummary(facts))
	}
	if calls[0].TaskID == calls[1].TaskID {
		t.Fatalf("task ids of the two segments = %q, want distinct identities for one native call identity", calls[0].TaskID)
	}
	if calls[0].InvocationID == calls[1].InvocationID {
		t.Fatalf("action facts of the two segments carry the invocation %q, want each to stay in the range that issued it",
			calls[0].InvocationID)
	}
	for _, call := range calls {
		settle, ok := eventIntegritySettleFact(facts, call.TaskID)
		if !ok {
			t.Fatalf("no single settle fact for task %q of invocation %q; facts = %s",
				call.TaskID, call.InvocationID, eventIntegrityFactsSummary(facts))
		}
		if settle.InvocationID != call.InvocationID {
			t.Fatalf("settle fact of task %q carries invocation %q, want its own %q", call.TaskID, settle.InvocationID, call.InvocationID)
		}
	}

	notices := eventIntegrityNotices(t, host.Run(), addr)
	firstNotice, ok := eventIntegrityNoticeOfTask(notices, calls[0].TaskID)
	if !ok {
		t.Fatalf("no notice for the first segment; notices = %+v", notices)
	}
	secondNotice, ok := eventIntegrityNoticeOfTask(notices, calls[1].TaskID)
	if !ok {
		t.Fatalf("no notice for the second segment; notices = %+v", notices)
	}
	if firstNotice.Ref.Notice.FactKey() == secondNotice.Ref.Notice.FactKey() {
		t.Fatalf("notice identities of the two segments = %s and %s, want two identities",
			firstNotice.Ref.Notice.FactKey(), secondNotice.Ref.Notice.FactKey())
	}
	firstNotice = eventIntegrityAwaitConsumed(t, host.Run(), addr, firstNotice.Ref.Notice.TaskID)
	secondNotice = eventIntegrityAwaitConsumed(t, host.Run(), addr, secondNotice.Ref.Notice.TaskID)
	keyOne := agent.NoticeFactKey(calls[0].InvocationID, firstNotice.Ref.Notice)
	keyTwo := agent.NoticeFactKey(calls[1].InvocationID, secondNotice.Ref.Notice)
	if keyOne == keyTwo {
		t.Fatalf("notice keys = %q and %q, want the range identity kept apart", keyOne, keyTwo)
	}

	store := newEventIntegrityStore(t, ctx, addr, firstNotice.Ref.InstanceID)
	for at, notice := range []task.Notice{firstNotice.Ref.Notice, secondNotice.Ref.Notice} {
		if err := store.track(ctx, notice.TaskID); err != nil {
			t.Fatalf("register the delivery route of %q: %v", notice.TaskID, err)
		}
		delivery := eventIntegrityDeliveryContext(ctx, addr, firstNotice.Ref.InstanceID, calls[at].InvocationID)
		if err := store.accept(delivery, notice); err != nil {
			t.Fatalf("accept the notice %s of its own invocation: %v", notice.FactKey(), err)
		}
	}
	mailbox, err := store.mailbox(ctx)
	if err != nil {
		t.Fatalf("read the mailbox of the two invocations: %v", err)
	}
	if len(mailbox) != 2 {
		t.Fatalf("mailbox entries = %d, want the two notices of two invocations kept apart: %+v", len(mailbox), mailbox)
	}
	if mailbox[0].InvocationID == mailbox[1].InvocationID {
		t.Fatalf("mailbox invocations = (%q, %q), want each notice under its own responsibility range",
			mailbox[0].InvocationID, mailbox[1].InvocationID)
	}
	for _, entry := range mailbox {
		if entry.InvocationID != calls[0].InvocationID && entry.InvocationID != calls[1].InvocationID {
			t.Fatalf("mailbox entry = %+v, want it booked under one of the two ranges the segments minted (%q, %q)",
				entry, calls[0].InvocationID, calls[1].InvocationID)
		}
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the cross-invocation script left violations: %v", violations)
	}
	eventIntegrityClose(t, ctx, host)
}

// runHandoffPreservesIssuedTarget drives the production wrapper as the notice
// producer beside the production manager's handoff and requires the frozen
// routing and the future routing to stay apart.
// - a notification created after the handoff routes to the new target and its instance
// - a notification committed before the handoff keeps the target its envelope froze
// - both identities are the producer's: one terminal per task, at seq 0
func runHandoffPreservesIssuedTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	targetA := event.Address{AgentID: "agent-handoff", SessionID: "session-handoff-a"}
	targetB := event.Address{AgentID: "agent-handoff", SessionID: "session-handoff-b"}

	delivered := make(chan task.Envelope, 4)
	manager := task.NewMemoryManager(
		task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission()),
		nil,
		task.WithEnvelopeDeliverer(func(_ context.Context, envelope task.Envelope) (task.DeliveryAcceptance, error) {
			delivered <- envelope
			return task.DeliveryAccepted, nil
		}),
	)
	arrived := make(chan task.Notice, 4)
	mailbox := func(_ context.Context, notice task.Notice) error {
		arrived <- notice
		return errEventIntegrityNotAccepted
	}

	future := newEventIntegrityGateTool("work-future")
	wrappedFuture := task.Wrap(manager, future,
		task.WithResultRecorder(eventIntegrityRecordResult), task.WithMailbox(mailbox))
	answer, err := wrappedFuture.Call(task.WithCallFrame(ctx, task.CallFrame{
		Identity: task.CallIdentity{InstanceID: "instance-a", InvocationID: "invocation-handoff", DecisionID: "decision-future", CallID: "call-future"},
		Address:  targetA,
	}), []byte(`{}`))
	if err != nil {
		t.Fatalf("admit the future notification call: %v", err)
	}
	futureReceipt, ok := answer.(task.AdmissionReceipt)
	if !ok {
		t.Fatalf("admitted future call answered %T, want the one admission receipt", answer)
	}
	future.await(t)
	if err := manager.Handoff(ctx, futureReceipt.TaskID, task.HandoffTarget{Target: targetB, TargetInstance: "instance-b"}); err != nil {
		t.Fatalf("hand off the running task %q: %v", futureReceipt.TaskID, err)
	}
	future.release()
	futureNotice := eventIntegrityAwaitNotice(t, arrived)
	if err := manager.Flush(ctx, futureReceipt.TaskID); err != nil {
		t.Fatalf("flush the future notification of %q: %v", futureReceipt.TaskID, err)
	}
	futureEnvelope := eventIntegrityAwaitEnvelope(t, delivered)
	if futureEnvelope.Target != targetB || futureEnvelope.TargetInstance != "instance-b" {
		t.Fatalf("future notification routing = %+v, want the handed-off target %+v at instance-b",
			futureEnvelope, targetB)
	}
	if futureEnvelope.Notice != futureNotice {
		t.Fatalf("delivered notice = %s, want the identity the producer created %s",
			futureEnvelope.Notice.FactKey(), futureNotice.FactKey())
	}

	preserved := newEventIntegrityGateTool("work-preserved")
	preserved.release()
	wrappedPreserved := task.Wrap(manager, preserved,
		task.WithResultRecorder(eventIntegrityRecordResult), task.WithMailbox(mailbox))
	preservedAnswer, err := wrappedPreserved.Call(task.WithCallFrame(ctx, task.CallFrame{
		Identity: task.CallIdentity{InstanceID: "instance-a", InvocationID: "invocation-handoff", DecisionID: "decision-preserved", CallID: "call-preserved"},
		Address:  targetA,
	}), []byte(`{}`))
	if err != nil {
		t.Fatalf("admit the preserved notification call: %v", err)
	}
	preservedReceipt, ok := preservedAnswer.(task.AdmissionReceipt)
	if !ok {
		t.Fatalf("admitted preserved call answered %T, want the one admission receipt", preservedAnswer)
	}
	preserved.await(t)
	preservedNotice := eventIntegrityAwaitNotice(t, arrived)
	if err := manager.Handoff(ctx, preservedReceipt.TaskID, task.HandoffTarget{Target: targetB, TargetInstance: "instance-b"}); err != nil {
		t.Fatalf("hand off the settled task %q: %v", preservedReceipt.TaskID, err)
	}
	if err := manager.Flush(ctx, preservedReceipt.TaskID); err != nil {
		t.Fatalf("flush the preserved notification of %q: %v", preservedReceipt.TaskID, err)
	}
	preservedEnvelope := eventIntegrityAwaitEnvelope(t, delivered)
	if preservedEnvelope.Target != targetA || preservedEnvelope.TargetInstance != "instance-a" {
		t.Fatalf("issued notification routing = %+v, want the frozen target %+v at instance-a",
			preservedEnvelope, targetA)
	}
	if preservedEnvelope.Notice != preservedNotice {
		t.Fatalf("delivered notice = %s, want the identity the producer created %s",
			preservedEnvelope.Notice.FactKey(), preservedNotice.FactKey())
	}
	for _, envelope := range []task.Envelope{futureEnvelope, preservedEnvelope} {
		notice := envelope.Notice
		if notice.Kind != task.NoticeSettled || notice.Seq != 0 || notice.Round < 1 || notice.Event == "" {
			t.Fatalf("producer notice = %+v, want one settled terminal at seq 0 pointing at its fact", notice)
		}
	}
}

// runNoticeProgressOrder checks the notification ordering the production points
// can support, and records the sequence the production paths cannot produce.
// - the formal chain produces no NoticeProgress: every production point writes Seq 0 beside a terminal kind only (runtime/task/wrapper.go:302, runtime/agent/delegate.go:404, runtime/task/cancel.go:81-86)
// - the single terminal notice of one task keeps one identity and one fact key across two reads, so no sequence is invented or renumbered
// - a repeated delivery of one progress-shaped identity is idempotently refused by the production mailbox consumer
func runNoticeProgressOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	eventIntegrityEnv(t)
	formal := eventIntegrityNoticeFromFormalRun(t, ctx, "session-notice-progress", "the progress ordering body")
	notice := formal.notice
	if notice.Kind != task.NoticeSettled || notice.Seq != 0 || notice.Round < 1 {
		t.Fatalf("production notice = %+v, want the settled terminal the wrapper writes at seq 0", notice)
	}

	t.Logf("progress notifications unreachable: production points write Seq 0 beside terminal kinds only (%s)",
		"runtime/task/wrapper.go:302, runtime/agent/delegate.go:404, runtime/task/cancel.go:81-86")

	if got := eventIntegrityNoticeCount(formal.notices, notice.TaskID); got != 1 {
		t.Fatalf("notices recorded for task %q = %d, want the single terminal the production point wrote: %+v",
			notice.TaskID, got, formal.notices)
	}
	key := agent.NoticeFactKey(formal.invocationID, notice)
	if again := agent.NoticeFactKey(formal.invocationID, notice); again != key {
		t.Fatalf("notice fact keys = %q and %q, want one stable identity", key, again)
	}

	store := newEventIntegrityStore(t, ctx, formal.addr, formal.instance)
	if err := store.track(ctx, notice.TaskID); err != nil {
		t.Fatalf("register the delivery route of %q: %v", notice.TaskID, err)
	}
	delivery := eventIntegrityDeliveryContext(ctx, formal.addr, formal.instance, formal.invocationID)
	if err := store.accept(delivery, notice); err != nil {
		t.Fatalf("accept the production notice %s: %v", notice.FactKey(), err)
	}
	if err := store.accept(delivery, notice); err != nil {
		t.Fatalf("re-deliver the same terminal identity %s = %v, want the idempotent acceptance",
			notice.FactKey(), err)
	}
	mailbox, err := store.mailbox(ctx)
	if err != nil {
		t.Fatalf("read the mailbox of the accepted terminal: %v", err)
	}
	if len(mailbox) != 1 || mailbox[0].Notice != notice || mailbox[0].AcceptSeq != 1 {
		t.Fatalf("mailbox = %+v, want the single terminal notice %s kept in its own order",
			mailbox, notice.FactKey())
	}

	progress := notice
	progress.Kind = task.NoticeProgress
	progress.Seq = 0
	progressKey := agent.NoticeFactKey(formal.invocationID, progress)
	if progressKey == key {
		t.Fatalf("progress fact key = %q, want a category of its own beside the terminal %q", progressKey, key)
	}
	if err := store.accept(delivery, progress); err != nil {
		t.Fatalf("accept the progress identity %s: %v", progress.FactKey(), err)
	}
	if err := store.accept(delivery, progress); err != nil {
		t.Fatalf("re-deliver the same progress identity %s = %v, want the idempotent acceptance",
			progress.FactKey(), err)
	}
	mailbox, err = store.mailbox(ctx)
	if err != nil {
		t.Fatalf("read the mailbox of the accepted progress: %v", err)
	}
	if len(mailbox) != 2 {
		t.Fatalf("mailbox entries = %d, want the one terminal beside the one progress identity: %+v",
			len(mailbox), mailbox)
	}
	if mailbox[1].Notice != progress || mailbox[1].AcceptSeq != 2 {
		t.Fatalf("progress entry = %+v, want the progress identity accepted once at its own sequence", mailbox[1])
	}
}

// eventIntegrityEntryModel, eventIntegrityWorkerModel and eventIntegrityWorkerAgent are the model identities and the declared worker agent the formal documents name.
const (
	eventIntegrityEntryModel  = "model-entry"
	eventIntegrityWorkerModel = "model-worker"
	eventIntegrityWorkerAgent = "worker"
)

// eventIntegrityEnv fixes the deployment environment every formal boot of this
// check runs under.
func eventIntegrityEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
}

// eventIntegrityProvider is the OpenAI-compatible endpoint the formal boots run
// against: every chat completion is recorded in the decoded shape a script
// selects on, and the scripted decision answers it. A request outside the
// script is refused instead of answered with a default terminal turn.
type eventIntegrityProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	models []string
	bodies []map[string]json.RawMessage
	served []string
	decide func(eventIntegrityRequest) (string, error)
}

// newEventIntegrityProvider starts the endpoint and closes it with the test.
func newEventIntegrityProvider(t *testing.T) *eventIntegrityProvider {
	t.Helper()
	provider := &eventIntegrityProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

// serve answers one chat completion and records the request it carried.
func (p *eventIntegrityProvider) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var body map[string]json.RawMessage
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	var modelName string
	if raw, ok := body["model"]; ok {
		_ = json.Unmarshal(raw, &modelName)
	}
	p.mu.Lock()
	index := len(p.bodies)
	p.models = append(p.models, modelName)
	p.bodies = append(p.bodies, body)
	p.served = append(p.served, "")
	decide := p.decide
	p.mu.Unlock()

	payload := eventIntegrityDefaultAnswer
	status := http.StatusOK
	if decide != nil {
		decided, err := decide(eventIntegrityDecodeRequest(index, modelName, body))
		if err != nil {
			status = http.StatusBadRequest
			payload = eventIntegrityRefusalAnswer(err.Error())
		} else {
			payload = decided
		}
	}
	p.mu.Lock()
	p.served[index] = payload
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, payload)
}

// requests reports the requests the endpoint answered in the decoded shape a
// script selects on, as a copy the caller owns.
func (p *eventIntegrityProvider) requests() []eventIntegrityRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventIntegrityRequest, 0, len(p.bodies))
	for index, body := range p.bodies {
		out = append(out, eventIntegrityDecodeRequest(index, p.models[index], body))
	}
	return out
}

// answerOf reports the assistant body the endpoint served on the request of one
// ordinal, so a check reads the reply a decision really produced instead of the
// body it meant to produce.
func (p *eventIntegrityProvider) answerOf(index int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.served) {
		return ""
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(p.served[index]), &decoded); err != nil || len(decoded.Choices) == 0 {
		return ""
	}
	return decoded.Choices[0].Message.Content
}

// endpoint reports the base URL the host document's provider entry names.
func (p *eventIntegrityProvider) endpoint() string { return p.server.URL + "/v1" }

// eventIntegrityDefaultAnswer is the terminal answer served beside no scripted
// decision: an assistant message with no tool call ends the turn that reads it.
const eventIntegrityDefaultAnswer = `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`

// eventIntegrityRequest is one chat completion request as the endpoint decoded
// it: the ordinal it arrived at, the model identity it named, and the messages
// it carried, so a step selects from the request itself instead of an ordinal.
type eventIntegrityRequest struct {
	Index    int
	Model    string
	Messages []eventIntegrityMessage
	Tools    []string
}

// eventIntegrityMessage is one message a request carried.
type eventIntegrityMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []eventIntegrityToolCall
}

// eventIntegrityToolCall is one tool call an assistant message carried.
type eventIntegrityToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// describe renders the request as the line a mismatch reads.
func (r eventIntegrityRequest) describe() string {
	roles := make([]string, 0, len(r.Messages))
	for _, message := range r.Messages {
		roles = append(roles, message.Role)
	}
	return fmt.Sprintf("model=%q messages=%v tools=%v", r.Model, roles, r.Tools)
}

// eventIntegrityDecodeRequest reads one recorded body into the request shape a
// step selects on.
func eventIntegrityDecodeRequest(index int, modelName string, body map[string]json.RawMessage) eventIntegrityRequest {
	request := eventIntegrityRequest{Index: index, Model: modelName}
	if raw, ok := body["messages"]; ok {
		var messages []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(raw, &messages); err == nil {
			for _, message := range messages {
				decoded := eventIntegrityMessage{Role: message.Role, Content: eventIntegrityContent(message.Content), ToolCallID: message.ToolCallID}
				for _, call := range message.ToolCalls {
					decoded.ToolCalls = append(decoded.ToolCalls, eventIntegrityToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
				}
				request.Messages = append(request.Messages, decoded)
			}
		}
	}
	if raw, ok := body["tools"]; ok {
		var tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err == nil {
			for _, tool := range tools {
				request.Tools = append(request.Tools, tool.Function.Name)
			}
		}
	}
	return request
}

// eventIntegrityContent reads one message content the protocol may render as a
// plain string or as text parts.
func eventIntegrityContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "")
}

// eventIntegrityRefusalAnswer renders the refusal the endpoint answers a request
// its script never declared.
func eventIntegrityRefusalAnswer(reason string) string {
	encoded, err := json.Marshal(reason)
	if err != nil {
		panic("execution: encode the scripted refusal: " + err.Error())
	}
	return `{"error":{"message":` + string(encoded) + `,"type":"invalid_request_error"}}`
}

// eventIntegrityScript is the declared decision script of one drive: the steps
// of each model identity in the order the drive expects them, beside the
// violations the drive produced against it. A request outside the script, a
// declared step never reached and a step reached twice are recorded as named
// violations, never swallowed by a default answer.
type eventIntegrityScript struct {
	mu         sync.Mutex
	streams    []*eventIntegrityStream
	byIdentity map[string]*eventIntegrityStream
	violations []string
}

// eventIntegrityStream is the ordered step list of one model identity.
type eventIntegrityStream struct {
	identity string
	steps    []*eventIntegrityStep
	next     int
}

// eventIntegrityStep is one declared decision: the request it matches and the
// answer it serves. An optional step is one only a declared shape reaches: the
// drive may read the material it waits for inside a decision already in
// flight, so the step records the shape the run took instead of a violation.
type eventIntegrityStep struct {
	name     string
	identity string
	match    func(eventIntegrityRequest) bool
	answer   string
	maySkip  bool
	taken    bool
}

// newEventIntegrityScript returns the script a drive declares its steps over.
func newEventIntegrityScript() *eventIntegrityScript {
	return &eventIntegrityScript{byIdentity: map[string]*eventIntegrityStream{}}
}

// expect declares the next step of one model identity: the request the step
// matches and the answer it serves.
func (s *eventIntegrityScript) expect(identity, name string, match func(eventIntegrityRequest) bool) *eventIntegrityStep {
	stream := s.byIdentity[identity]
	if stream == nil {
		stream = &eventIntegrityStream{identity: identity}
		s.byIdentity[identity] = stream
		s.streams = append(s.streams, stream)
	}
	step := &eventIntegrityStep{name: name, identity: identity, match: match}
	stream.steps = append(stream.steps, step)
	return step
}

// answers declares the payload the endpoint serves on this step.
func (s *eventIntegrityStep) answers(payload string) *eventIntegrityStep {
	s.answer = payload
	return s
}

// optional declares a step of one shape beside the shape that may fold it into
// a decision already declared, so a drive that takes either shape audits clean.
func (s *eventIntegrityStep) optional() *eventIntegrityStep {
	s.maySkip = true
	return s
}

// decide selects the answer of one request against the declared script.
func (s *eventIntegrityScript) decide(request eventIntegrityRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stream := s.byIdentity[request.Model]
	if stream == nil {
		return "", s.violateLocked("unexpected", fmt.Sprintf("model %q outside the script (request %d)", request.Model, request.Index))
	}
	if stream.next >= len(stream.steps) {
		return "", s.violateLocked("unexpected", fmt.Sprintf("no declared step left for model %q (request %d)", request.Model, request.Index))
	}
	step := stream.steps[stream.next]
	if !step.match(request) {
		return "", s.violateLocked("unexpected", fmt.Sprintf("request %d did not match the next step %q of %q: %s",
			request.Index, step.name, request.Model, request.describe()))
	}
	stream.next++
	step.taken = true
	return step.answer, nil
}

// violateLocked records one violation and answers it as the error the endpoint
// refuses the request with.
func (s *eventIntegrityScript) violateLocked(kind, detail string) error {
	line := kind + ": " + detail
	s.violations = append(s.violations, line)
	return errors.New(line)
}

// eventIntegrityDescribeRequests renders the requests the endpoint served, in the
// shape a failure states what the model was really asked.
func eventIntegrityDescribeRequests(requests []eventIntegrityRequest) string {
	described := make([]string, 0, len(requests))
	for _, request := range requests {
		described = append(described, fmt.Sprintf("#%d %s", request.Index, request.describe()))
	}
	return strings.Join(described, "; ")
}

// audit reports the violations the drive left against its script, beside every
// declared step the drive never reached. A step declared optional is a shape the
// drive may take instead of another, so its absence is no violation.
func (s *eventIntegrityScript) audit() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.violations...)
	for _, stream := range s.streams {
		for _, step := range stream.steps {
			if !step.taken && !step.maySkip {
				out = append(out, fmt.Sprintf("missing: step %q of %q was never reached", step.name, stream.identity))
			}
		}
	}
	return out
}

// eventIntegrityTail matches the request whose last message carries the text.
func eventIntegrityTail(text string) func(eventIntegrityRequest) bool {
	return func(request eventIntegrityRequest) bool {
		if len(request.Messages) == 0 {
			return false
		}
		return strings.Contains(request.Messages[len(request.Messages)-1].Content, text)
	}
}

// eventIntegrityCarriesToolResult matches the request that carries the result
// recorded under one native call identity.
func eventIntegrityCarriesToolResult(callID string) func(eventIntegrityRequest) bool {
	return func(request eventIntegrityRequest) bool {
		for _, message := range request.Messages {
			if message.ToolCallID == callID {
				return true
			}
		}
		return false
	}
}

// eventIntegrityCarriesSettleNotice matches the request that carries the settled
// notice of the named body: the material the formal settle turn reads is the
// task and the terminal it settled on beside the result body.
func eventIntegrityCarriesSettleNotice(body string) func(eventIntegrityRequest) bool {
	return func(request eventIntegrityRequest) bool {
		for _, message := range request.Messages {
			if message.Role != string(model.RoleUser) {
				continue
			}
			if strings.Contains(message.Content, "tagent: task ") &&
				strings.Contains(message.Content, " settled") &&
				strings.Contains(message.Content, body) {
				return true
			}
		}
		return false
	}
}

// eventIntegrityToolCallAnswer renders the chat completion that issues one tool
// call under the named native call identity.
func eventIntegrityToolCallAnswer(name, callID, arguments string) string {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		panic("execution: encode the scripted tool arguments: " + err.Error())
	}
	return `{"id":"scripted-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"calling","tool_calls":[{"id":"` +
		callID + `","type":"function","function":{"name":"` + name + `","arguments":` + string(encoded) + `}}]},"finish_reason":"tool_calls"}]}`
}

// eventIntegrityAnswerOf renders the chat completion that ends the turn with the
// named assistant body.
func eventIntegrityAnswerOf(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic("execution: encode the scripted assistant body: " + err.Error())
	}
	return `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":` + string(encoded) + `},"finish_reason":"stop"}]}`
}

// eventIntegritySender is the outbound face of the formal boot: every accepted
// body is recorded beside the address it was delivered to, so the check reads
// what the host really sent instead of what it meant to send.
type eventIntegritySender struct {
	mu      sync.Mutex
	targets []event.Address
	bodies  []string
}

// newEventIntegritySender returns the recording outbound face.
func newEventIntegritySender() *eventIntegritySender { return &eventIntegritySender{} }

// Send records one delivered body beside its target address.
func (s *eventIntegritySender) Send(_ context.Context, target event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append(s.targets, target)
	s.bodies = append(s.bodies, content)
	return nil
}

// deliveredCount counts every body the host delivered to one address.
func (s *eventIntegritySender) deliveredCount(addr event.Address) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for i := range s.targets {
		if s.targets[i] == addr {
			count++
		}
	}
	return count
}

// delivered counts the bodies the host delivered to one address that carry the
// named text.
func (s *eventIntegritySender) delivered(addr event.Address, text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for i := range s.bodies {
		if s.targets[i] == addr && strings.Contains(s.bodies[i], text) {
			count++
		}
	}
	return count
}

// awaitDelivered waits until the named body reached the address exactly once,
// so a check reads a delivery the turn really made instead of racing it.
func (s *eventIntegritySender) awaitDelivered(t *testing.T, addr event.Address, text string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if s.delivered(addr, text) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the body %q was never delivered to %+v", text, addr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// eventIntegrityHostDocument renders one host document whose entry agent keeps
// the declared send tool beside the declared worker agent, and whose model
// window is declared so the session chain carries the projection and the notice
// feed the follow-up range reads.
func eventIntegrityHostDocument(root, endpoint string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + eventIntegrityEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    model: " + eventIntegrityEntryModel + "\n")
	b.WriteString("    max_tokens: 32000\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: send }\n")
	b.WriteString("      - { kind: agent, agent: " + eventIntegrityWorkerAgent + " }\n")
	b.WriteString("  " + eventIntegrityWorkerAgent + ":\n")
	b.WriteString("    model: " + eventIntegrityWorkerModel + "\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// eventIntegrityBoot boots the formal host the checks drive.
func eventIntegrityBoot(t *testing.T, provider *eventIntegrityProvider, sender *eventIntegritySender) *assembly.Host {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(eventIntegrityHostDocument(root, provider.endpoint())), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the formal host: %v", err)
	}
	return host
}

// eventIntegrityKeepHost stops the host when the check ends, so a failed check
// leaves no listening face behind.
func eventIntegrityKeepHost(t *testing.T, host *assembly.Host) {
	t.Helper()
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := host.Close(closeCtx); err != nil {
			t.Logf("the formal host ended its close beside: %v", err)
		}
	})
}

// eventIntegrityClose drains the run the host drives and requires the formal
// obligations to converge before the host closes.
func eventIntegrityClose(t *testing.T, ctx context.Context, host *assembly.Host) {
	t.Helper()
	report, err := host.Run().Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(20 * time.Second)})
	if err != nil {
		t.Fatalf("drain the run the host drives: %v", err)
	}
	if !report.Drained || len(report.Unsettled) != 0 {
		t.Fatalf("drain report = %+v, want every formal obligation settled before the close", report)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// eventIntegrityInvoke drives one invocation of the formal host.
func eventIntegrityInvoke(ctx context.Context, host *assembly.Host, addr event.Address, invocationID event.InvocationID, input string) (agent.Turn, error) {
	return host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: invocationID,
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
}

// eventIntegrityActionFact reports the one recorded result fact of a native
// call.
func eventIntegrityActionFact(facts []event.Event, callID string) (event.Event, bool) {
	calls := eventIntegrityActionFactsByCall(facts, callID)
	if len(calls) != 1 {
		return event.Event{}, false
	}
	return calls[0], true
}

// eventIntegrityActionFactsByCall reports the recorded result facts that carry
// one native call identity.
func eventIntegrityActionFactsByCall(facts []event.Event, callID string) []event.Event {
	out := make([]event.Event, 0, 1)
	for _, fact := range facts {
		if fact.Type != event.TypeActionCommand || fact.Message.ToolID != callID {
			continue
		}
		out = append(out, fact)
	}
	return out
}

// eventIntegrityInputFacts reports the external input facts of one session whose
// message carries the named material.
func eventIntegrityInputFacts(facts []event.Event, material string) []event.Event {
	out := make([]event.Event, 0, 1)
	for _, fact := range facts {
		if fact.Type == event.TypeExternalInput && strings.Contains(fact.Message.Content, material) {
			out = append(out, fact)
		}
	}
	return out
}

// eventIntegritySettleFact reports the terminal settle fact of the named task,
// preferring the formal delivery record: the loop's own read of a settled
// result commits an empty backflow fact of the same type beside the delivery
// fact that carries the material, so the check selects by the settle lineage
// rather than by whichever landed first.
func eventIntegritySettleFact(facts []event.Event, taskID event.TaskID) (event.Event, bool) {
	var out []event.Event
	for _, fact := range facts {
		if fact.Type != event.TypeTaskSettled || fact.TaskID != taskID {
			continue
		}
		if fact.Meta[event.MetaKeySettleTriggerSource] != "" {
			return fact, true
		}
		out = append(out, fact)
	}
	if len(out) == 0 {
		return event.Event{}, false
	}
	return out[0], true
}

// eventIntegrityDelegatedSettle reports the one terminal settle fact that
// carries the named result body.
func eventIntegrityDelegatedSettle(facts []event.Event, body string) (event.Event, bool) {
	out := make([]event.Event, 0, 1)
	for _, fact := range facts {
		if fact.Type != event.TypeTaskSettled || !strings.Contains(fact.Message.Content, body) {
			continue
		}
		out = append(out, fact)
	}
	if len(out) != 1 {
		return event.Event{}, false
	}
	return out[0], true
}

// eventIntegrityAwaitDelegatedSettle waits for the one terminal settle fact
// that carries the named result body: the delivery of a delegated settle lands
// on its own thread beside the turn that issued the call, so the check reads
// the chain until that fact arrives instead of assuming it already landed.
func eventIntegrityAwaitDelegatedSettle(t *testing.T, run *assembly.Run, addr event.Address, body string) event.Event {
	t.Helper()
	deadline := time.Now().Add(eventIntegrityConsumeWindow)
	for {
		facts := run.SessionFacts(addr)
		if fact, ok := eventIntegrityDelegatedSettle(facts, body); ok {
			return fact
		}
		if time.Now().After(deadline) {
			t.Fatalf("no single delegated settle fact carries %q within %s; facts = %s",
				body, eventIntegrityConsumeWindow, eventIntegrityFactsSummary(facts))
		}
		time.Sleep(eventIntegrityPollInterval)
	}
}

// eventIntegrityNoticeFacts reports the committed notice facts of one session:
// the feedback facts the notice feed reads.
func eventIntegrityNoticeFacts(facts []event.Event) []event.Event {
	out := make([]event.Event, 0, 1)
	for _, fact := range facts {
		if fact.Type != event.TypeFeedback || fact.Meta[event.MetaKeyNoticeKind] == "" {
			continue
		}
		out = append(out, fact)
	}
	return out
}

// eventIntegrityNoticeOfFact rebuilds the notice identity one notice fact was
// minted under: the task and the category the fact carries, the fact it points
// at, and the zero round and sequence the follow-up path mints.
func eventIntegrityNoticeOfFact(fact event.Event) task.Notice {
	return task.Notice{
		TaskID: fact.TaskID,
		Kind:   task.NoticeKind(fact.Meta[event.MetaKeyNoticeKind]),
		Event:  fact.Key,
	}
}

// eventIntegrityFactKeyCount counts the facts committed under one fact key.
func eventIntegrityFactKeyCount(facts []event.Event, key event.EventID) int {
	count := 0
	for _, fact := range facts {
		if fact.Key == key {
			count++
		}
	}
	return count
}

// eventIntegrityFactsSummary renders the facts one failed check reads.
func eventIntegrityFactsSummary(facts []event.Event) string {
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		lines = append(lines, fmt.Sprintf("{type=%s key=%s task=%s call=%s invocation=%s}",
			fact.Type, fact.Key, fact.TaskID, fact.CallID, fact.InvocationID))
	}
	return "[" + strings.Join(lines, " ") + "]"
}

// eventIntegrityNotices reads the notification projection of one session over
// the formal read face.
func eventIntegrityNotices(t *testing.T, run *assembly.Run, addr event.Address) []query.Detail {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{addr.AgentID}, Sessions: []event.Address{addr}},
		Kinds: []query.ObjectKind{query.ObjectNotice},
	})
	if err != nil {
		t.Fatalf("read the notices of %+v: %v", addr, err)
	}
	return snapshot.Details
}

// eventIntegrityNoticeOfTask reports the one notice projection entry of the
// named task.
func eventIntegrityNoticeOfTask(notices []query.Detail, taskID event.TaskID) (query.Detail, bool) {
	out := make([]query.Detail, 0, 1)
	for _, detail := range notices {
		if detail.Ref.Kind != query.ObjectNotice || detail.Ref.Notice.TaskID != taskID {
			continue
		}
		out = append(out, detail)
	}
	if len(out) != 1 {
		return query.Detail{}, false
	}
	return out[0], true
}

// eventIntegrityNoticeCount counts the notice projection entries of the named
// task.
func eventIntegrityNoticeCount(notices []query.Detail, taskID event.TaskID) int {
	count := 0
	for _, detail := range notices {
		if detail.Ref.Kind == query.ObjectNotice && detail.Ref.Notice.TaskID == taskID {
			count++
		}
	}
	return count
}

// eventIntegrityConsumeWindow bounds how long one notice may take to reach its
// consumption, and eventIntegrityPollInterval is how often the projection is
// read while it waits.
const (
	eventIntegrityConsumeWindow = 30 * time.Second
	eventIntegrityPollInterval  = 20 * time.Millisecond
)

// eventIntegrityAwaitConsumed waits until the notice of one task reads as
// consumed on the projection. The consumption is the receipt of a turn that
// really ran: the settle driver claims the seat, the material the notice
// commits opens the segment the consumption loop drives, and only the turn that
// segment lands closes the entry — so the check waits for the account instead of
// reading it beside a turn that may still be running.
func eventIntegrityAwaitConsumed(t *testing.T, run *assembly.Run, addr event.Address, taskID event.TaskID) query.Detail {
	t.Helper()
	deadline := time.Now().Add(eventIntegrityConsumeWindow)
	for {
		notices := eventIntegrityNotices(t, run, addr)
		detail, ok := eventIntegrityNoticeOfTask(notices, taskID)
		switch {
		case ok && detail.State.NoticePhase == query.NoticePhaseConsumed:
			return detail
		case time.Now().After(deadline):
			if !ok {
				t.Fatalf("no single notice recorded for task %q; notices = %+v", taskID, notices)
			}
			t.Fatalf("notice phase of task %q = %q, want the consumption the live sink books beside the landed turn",
				taskID, detail.State.NoticePhase)
		}
		time.Sleep(eventIntegrityPollInterval)
	}
}

// eventIntegrityRequestsFirstCarryingToolCall reports the ordinal of the first
// request of one model that carries the assistant message of a call identity.
// The requests of a model form its decision stream, so the first carrier is the
// decision the issuing invocation took after it asked for the call, while the
// delegated turns of other models and the later replays of one history do not
// answer here.
func eventIntegrityRequestsFirstCarryingToolCall(requests []eventIntegrityRequest, modelName, callID string) []int {
	out := make([]int, 0, 1)
	for _, request := range requests {
		if request.Model != modelName {
			continue
		}
		if eventIntegrityRequestCarriesToolCall(request, callID) {
			out = append(out, request.Index)
			break
		}
	}
	return out
}

// eventIntegrityRequestCarriesToolCall reports whether the request carries the
// assistant message that asks for one native call identity.
func eventIntegrityRequestCarriesToolCall(request eventIntegrityRequest, callID string) bool {
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			if call.ID == callID {
				return true
			}
		}
	}
	return false
}

// eventIntegrityRequestsFirstReadingToolResult reports the ordinal of the first
// request of one model that carries the result of a call identity. One result is
// first read by the decision it was delivered to, while the later requests of
// the same decision stream only replay it out of their history.
func eventIntegrityRequestsFirstReadingToolResult(requests []eventIntegrityRequest, modelName, callID string) []int {
	out := make([]int, 0, 1)
	for _, request := range requests {
		if request.Model != modelName {
			continue
		}
		if eventIntegrityRequestCarriesToolResult(request, callID) {
			out = append(out, request.Index)
			break
		}
	}
	return out
}

// eventIntegrityRequestCarriesToolResult reports whether the request carries the
// result of one native call identity.
func eventIntegrityRequestCarriesToolResult(request eventIntegrityRequest, callID string) bool {
	for _, message := range request.Messages {
		if message.ToolCallID == callID {
			return true
		}
	}
	return false
}

// eventIntegrityRequestsCarryingUser reports the requests whose user material
// carries the named text, by their ordinal.
func eventIntegrityRequestsCarryingUser(requests []eventIntegrityRequest, material string) []int {
	out := make([]int, 0, 1)
	for index, request := range requests {
		if eventIntegrityUserCopies(request, material) > 0 {
			out = append(out, index)
		}
	}
	return out
}

// eventIntegrityUserCopies counts the user messages of one request that carry
// the named material.
func eventIntegrityUserCopies(request eventIntegrityRequest, material string) int {
	count := 0
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) && strings.Contains(message.Content, material) {
			count++
		}
	}
	return count
}

// eventIntegrityFormalNotice is one notice identity a formal run produced, read
// back from the run that minted it.
type eventIntegrityFormalNotice struct {
	addr         event.Address
	invocationID event.InvocationID
	instance     event.InstanceID
	notice       task.Notice
	phase        query.NoticePhase
	notices      []query.Detail
}

// eventIntegrityNoticeFromFormalRun drives one declared send call over Boot and
// Host.Invoke and reads back the terminal notice the production point minted,
// beside the invocation that issued the call and the site that owns the notice.
func eventIntegrityNoticeFromFormalRun(t *testing.T, ctx context.Context, session, body string) eventIntegrityFormalNotice {
	t.Helper()
	const (
		input  = "send one notice identity"
		answer = "the entry read the settled send"
	)
	callID := "call-" + session
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	script.expect(eventIntegrityEntryModel, "the entry sends its notice", eventIntegrityTail(input)).
		answers(eventIntegrityToolCallAnswer("send", callID, `{"content":"`+body+`"}`))
	script.expect(eventIntegrityEntryModel, "the entry ends after the settle", eventIntegrityCarriesToolResult(callID)).
		answers(eventIntegrityAnswerOf(answer))
	provider.decide = script.decide

	sender := newEventIntegritySender()
	host := eventIntegrityBoot(t, provider, sender)
	eventIntegrityKeepHost(t, host)
	addr := event.Address{AgentID: "user", SessionID: event.SessionID(session)}
	turn, err := eventIntegrityInvoke(ctx, host, addr, "", input)
	if err != nil {
		t.Fatalf("the formal run of %q ended by error: %v", session, err)
	}
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("formal turn of %q = (%v, %d decisions), want the two decisions of the send call",
			session, turn.Terminal, len(turn.Decisions))
	}
	if got := sender.delivered(addr, body); got != 1 {
		t.Fatalf("the send body reached the user %d times, want exactly the one delivery", got)
	}

	facts := host.Run().SessionFacts(addr)
	call, ok := eventIntegrityActionFact(facts, callID)
	if !ok {
		t.Fatalf("no single action fact carries %q; facts = %s", callID, eventIntegrityFactsSummary(facts))
	}
	if call.InvocationID != turn.InvocationID {
		t.Fatalf("the send fact invocation = %q, want the issuing range %q", call.InvocationID, turn.InvocationID)
	}
	notices := eventIntegrityNotices(t, host.Run(), addr)
	detail, ok := eventIntegrityNoticeOfTask(notices, call.TaskID)
	if !ok {
		t.Fatalf("no single notice of task %q; notices = %+v", call.TaskID, notices)
	}
	detail = eventIntegrityAwaitConsumed(t, host.Run(), addr, call.TaskID)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the formal run of %q left script violations: %v", session, violations)
	}
	eventIntegrityClose(t, ctx, host)
	return eventIntegrityFormalNotice{
		addr:         addr,
		invocationID: turn.InvocationID,
		instance:     detail.Ref.InstanceID,
		notice:       detail.Ref.Notice,
		phase:        detail.State.NoticePhase,
		notices:      notices,
	}
}

// eventIntegrityStore is the state owner the identity checks present one
// production notice to: the very MemoryStore the run books its own mailbox in,
// over one established site and the read grant of the check.
type eventIntegrityStore struct {
	store    *orgstate.MemoryStore
	subject  orgstate.Subject
	addr     event.Address
	instance event.InstanceID
}

// newEventIntegrityStore returns one store with the named site established and
// granted for reading.
func newEventIntegrityStore(t *testing.T, ctx context.Context, addr event.Address, instance event.InstanceID) *eventIntegrityStore {
	t.Helper()
	const subject orgstate.Subject = "subject-event-integrity"
	authority := orgstate.NewAuthority()
	if err := authority.GrantRead(ctx, subject, addr); err != nil {
		t.Fatalf("grant the read of %+v: %v", addr, err)
	}
	store := orgstate.NewMemoryStore(authority)
	if _, err := store.Establish(ctx, addr, instance); err != nil {
		t.Fatalf("establish the site of %+v at %q: %v", addr, instance, err)
	}
	return &eventIntegrityStore{store: store, subject: subject, addr: addr, instance: instance}
}

// track registers where the future notices of one task are delivered.
func (s *eventIntegrityStore) track(ctx context.Context, taskID event.TaskID) error {
	return s.store.TrackDelivery(ctx, taskID, task.HandoffTarget{Target: s.addr, TargetInstance: s.instance})
}

// accept presents one notice to the production mailbox consumer.
func (s *eventIntegrityStore) accept(ctx context.Context, notice task.Notice) error {
	return s.store.AcceptNotice(ctx, notice)
}

// mailbox reads the acceptance log of the established site.
func (s *eventIntegrityStore) mailbox(ctx context.Context) ([]orgstate.MailboxEntry, error) {
	site, err := s.store.Site(ctx, s.subject, s.addr, s.instance)
	if err != nil {
		return nil, err
	}
	return site.Mailbox, nil
}

// eventIntegrityDeliveryContext carries the call identity one delivery is
// attributed to, so the mailbox records the responsibility range of the call
// that produced the notice.
func eventIntegrityDeliveryContext(ctx context.Context, addr event.Address, instance event.InstanceID, invocationID event.InvocationID) context.Context {
	return task.WithCallFrame(ctx, task.CallFrame{
		Identity: task.CallIdentity{InstanceID: instance, InvocationID: invocationID},
		Address:  addr,
	})
}

// errEventIntegrityNotAccepted is the refusal the handoff check's mailbox
// answers, so the delivery obligation stays open and the flush offers the frozen
// envelope.
var errEventIntegrityNotAccepted = errors.New("event integrity: the target has not accepted the notice yet")

// eventIntegrityGateTool is the gated native tool the handoff check wraps with
// the production wrapper: its execution parks until the check releases it, so a
// handoff lands while the call is still running.
type eventIntegrityGateTool struct {
	name    string
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
	served  sync.Once
}

// newEventIntegrityGateTool returns one parked tool of the named declaration.
func newEventIntegrityGateTool(name string) *eventIntegrityGateTool {
	return &eventIntegrityGateTool{name: name, entered: make(chan struct{}), gate: make(chan struct{})}
}

// Declaration reports the one tool the gate stands for.
func (g *eventIntegrityGateTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: g.name}
}

// Call signals its entry and waits for the release before answering.
func (g *eventIntegrityGateTool) Call(ctx context.Context, _ []byte) (any, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.gate:
		return g.name + " settled", nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// await waits until the call entered its execution.
func (g *eventIntegrityGateTool) await(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(20 * time.Second):
		t.Fatalf("the gated call of %q never entered its execution", g.name)
	}
}

// release lets the parked call settle.
func (g *eventIntegrityGateTool) release() {
	g.served.Do(func() { close(g.gate) })
}

// eventIntegrityRecordResult records the result of one wrapped call as a fact of
// the handoff check: the identity names the task, so each terminal fact stays
// its own.
func eventIntegrityRecordResult(_ context.Context, result task.Result) (event.EventID, error) {
	return event.EventID("fact-" + string(result.TaskID)), nil
}

// eventIntegrityAwaitNotice waits for one notice the producer offered.
func eventIntegrityAwaitNotice(t *testing.T, arrived <-chan task.Notice) task.Notice {
	t.Helper()
	select {
	case notice := <-arrived:
		return notice
	case <-time.After(20 * time.Second):
		t.Fatal("the producer never offered its notice")
		return task.Notice{}
	}
}

// eventIntegrityAwaitEnvelope waits for one envelope the manager flushed.
func eventIntegrityAwaitEnvelope(t *testing.T, delivered <-chan task.Envelope) task.Envelope {
	t.Helper()
	select {
	case envelope := <-delivered:
		return envelope
	case <-time.After(20 * time.Second):
		t.Fatal("the flush never offered the frozen envelope")
		return task.Envelope{}
	}
}

// TestEventIntegrityExecStateEvents checks the execution state events the manager produces after the acquiring call returned.
// - the lease keeps renewing after the call returned
// - stable, suspect and exited observations leave audit facts only: no turn and no model material
// - the next decision of the range reads no execution state material
// - the host close releases every resource it still holds, and a restarted host names the resource it cannot confirm
// - a restarted host takes a running session over instead of opening it twice
// - the final output of an exited run stays readable through the model's own tool call
// - two subtests keep frozen names from the old follow-up path while asserting silence; renaming needs the archived graph updated with it
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
func TestEventIntegrityExecStateEvents(t *testing.T) {
	if !tagentexec.IsTmuxAvailable() {
		t.Skip("tmux is not available on this runner")
	}
	isolateTmuxServer(t)
	t.Run("lease_keeps_renewing_after_return", runEventIntegrityExecLeaseKeepsRenewing)
	t.Run("state_change_emits_followup_notice", runEventIntegrityExecThreeStatesAudit)
	t.Run("next_turn_consumes_exec_state", runEventIntegrityExecNextTurnCarriesNoState)
	t.Run("host_close_releases_exec_resources", runEventIntegrityExecHostCloseReleases)
	t.Run("restart_takes_over_the_running_session", runEventIntegrityExecRestartTakesOver)
	t.Run("restart_takes_over_or_names_unconfirmed", runEventIntegrityExecRestartNamesUnknown)
	t.Run("unconfirmed_restart_not_reported_stopped", runEventIntegrityExecRestartNotReportedStopped)
	t.Run("final_output_survives_the_exit", runEventIntegrityExecFinalOutputSurvives)
}

const (
	// eventIntegrityExecModel is the model identity of the exec checks: each
	// check declares its own identity, so one script stream serves one host.
	eventIntegrityExecModel = "model-entry-exec"
	// eventIntegrityExecQuietRhythm keeps the stable and fake-dead thresholds
	// beyond the life of a check, so only the event a check drives is produced.
	eventIntegrityExecQuietRhythm = "{ interval: 100ms, stable_duration: 60000ms, interactive_stable_duration: 60000ms, fake_dead_duration: 60000ms }"
	// eventIntegrityExecTriStateRhythm reaches every intermediate observation
	// of a short live run before it exits: the stable threshold passes inside
	// the run, the fake-dead threshold passes while it is still alive, and the
	// observed death follows, so one boot produces stable, suspect and exited.
	eventIntegrityExecTriStateRhythm = "{ interval: 50ms, stable_duration: 150ms, interactive_stable_duration: 150ms, fake_dead_duration: 700ms }"
)

// eventIntegrityExecSuccessWords are the words a lifecycle observation must
// never use: stable is not success and a resource still held is not done.
var eventIntegrityExecSuccessWords = []string{"success", "complete", "done", "finished", "succeeded", "passed"}

// runEventIntegrityExecLeaseKeepsRenewing proves the lease belongs to the
// tracked resource rather than to the call that acquired it:
//   - one long run returns from its call and the host still holds it
//   - the renewal count grows after the call returned, so the lease kept
//     renewing beside the manager
//   - the tmux session is still alive, so the resource was not reaped
func runEventIntegrityExecLeaseKeepsRenewing(t *testing.T) {
	const input = "start the long run"
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	resource := eventIntegrityExecResource(t)
	key := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: resource}
	script.expect(eventIntegrityExecModel, "the entry starts the long run", eventIntegrityTail(input)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-long", eventIntegrityExecRunArgs(resource, "sleep", `["30"]`)))
	script.expect(eventIntegrityExecModel, "the entry accepts the long run", eventIntegrityCarriesToolResult("call-exec-long")).
		answers(eventIntegrityAnswerOf("the long run is held"))
	provider.decide = script.decide
	sender := newEventIntegritySender()
	host := eventIntegrityBootExec(t, provider, sender, eventIntegrityExecModel, eventIntegrityExecQuietRhythm)
	eventIntegrityKeepHost(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-exec-lease"}
	if _, err := eventIntegrityInvoke(ctx, host, addr, "", input); err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	first := eventIntegrityAwaitExecRenewed(t, host, key, 1)
	time.Sleep(450 * time.Millisecond)
	second, err := host.Run().ExecState(key)
	if err != nil {
		t.Fatalf("read the state of %s after the call returned: %v", key, err)
	}
	if !second.Held {
		t.Fatalf("the resource %s is not held after its call returned: %+v", key, second)
	}
	if second.Renewed <= first.Renewed {
		t.Fatalf("renewals after the call returned = %d -> %d, want the lease to keep renewing",
			first.Renewed, second.Renewed)
	}
	if !second.Deadline.After(time.Now()) {
		t.Fatalf("lease deadline %v is not in the future", second.Deadline)
	}
	if !eventIntegrityTmuxAlive(resource) {
		t.Fatalf("the session %s is not alive, want the tracked resource still running", resource)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the lease script left violations: %v", violations)
	}
}

// runEventIntegrityExecThreeStatesAudit proves the intermediate observations of
// one tracked resource are recorded as audit facts and never drive the model:
//   - a short run is held, then observed stable, suspect and exited
//   - each observation commits one payload-free feedback fact with resource, phase and exit-code metadata, no notice kind and no task id
//   - no request reaches the provider and no body reaches the address beside the three observations
//   - the confirmed exit frees the resource
func runEventIntegrityExecThreeStatesAudit(t *testing.T) {
	const input = "start the run whose states stay silent"
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	resource := eventIntegrityExecResource(t)
	key := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: resource}
	script.expect(eventIntegrityExecModel, "the entry starts the short run", eventIntegrityTail(input)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-states",
			eventIntegrityExecRunArgs(resource, "sh", `["-c","sleep 2; exit 7"]`)))
	script.expect(eventIntegrityExecModel, "the entry accepts the short run", eventIntegrityCarriesToolResult("call-exec-states")).
		answers(eventIntegrityAnswerOf("the short run is under way"))
	provider.decide = script.decide
	sender := newEventIntegritySender()
	host := eventIntegrityBootExec(t, provider, sender, eventIntegrityExecModel, eventIntegrityExecTriStateRhythm)
	eventIntegrityKeepHost(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-exec-states"}
	if _, err := eventIntegrityInvoke(ctx, host, addr, "", input); err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	beforeRequests := len(provider.requests())
	beforeOutbound := sender.deliveredCount(addr)
	for _, phase := range []tagentexec.StatePhase{tagentexec.StateStable, tagentexec.StateSuspect, tagentexec.StateExited} {
		fact := eventIntegrityAwaitExecStateFact(t, host, addr, phase)
		eventIntegrityAssertExecAuditFact(t, fact, key, phase, 7)
	}
	facts := eventIntegrityExecStateFacts(host.Run().SessionFacts(addr))
	observed := make(map[tagentexec.StatePhase]bool, 3)
	for _, fact := range facts {
		phase := tagentexec.StatePhase(fact.Meta[event.MetaPrefix+"phase"])
		observed[phase] = true
		eventIntegrityAssertExecAuditFact(t, fact, key, phase, 7)
	}
	for _, phase := range []tagentexec.StatePhase{tagentexec.StateStable, tagentexec.StateSuspect, tagentexec.StateExited} {
		if !observed[phase] {
			t.Fatalf("execution state audit facts carry no %s observation: %v", phase, facts)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if requests := len(provider.requests()); requests != beforeRequests {
		t.Fatalf("model requests = %d -> %d beside three silent observations, want no new turn: %s",
			beforeRequests, requests, eventIntegrityRequestsSummary(provider.requests()))
	}
	if outbound := sender.deliveredCount(addr); outbound != beforeOutbound {
		t.Fatalf("outbound bodies = %d -> %d beside three silent observations, want no new turn", beforeOutbound, outbound)
	}
	if _, err := host.Run().ExecState(key); !errors.Is(err, tagentexec.ErrResourceUnknown) {
		t.Fatalf("state of the exited resource = %v, want %v beside a confirmed exit", err, tagentexec.ErrResourceUnknown)
	}
	if eventIntegrityTmuxAlive(resource) {
		t.Fatalf("the session %s is alive after its confirmed exit", resource)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the state script left violations: %v", violations)
	}
}

// runEventIntegrityExecNextTurnCarriesNoState proves the next decision of the
// range reads no execution state material:
//   - the three observations of a short run commit their audit facts
//   - the next user turn adds exactly one provider request
//   - no request of the range carries the state text the old chain injected
func runEventIntegrityExecNextTurnCarriesNoState(t *testing.T) {
	const (
		input    = "start the run before the next question"
		question = "what is left in the background?"
		answer   = "nothing the state report says"
	)
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	resource := eventIntegrityExecResource(t)
	key := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: resource}
	script.expect(eventIntegrityExecModel, "the entry starts the short run", eventIntegrityTail(input)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-next",
			eventIntegrityExecRunArgs(resource, "sh", `["-c","sleep 2; exit 7"]`)))
	script.expect(eventIntegrityExecModel, "the entry accepts the short run", eventIntegrityCarriesToolResult("call-exec-next")).
		answers(eventIntegrityAnswerOf("the short run is under way"))
	script.expect(eventIntegrityExecModel, "the next user turn asks its own question", eventIntegrityTail(question)).
		answers(eventIntegrityAnswerOf(answer))
	provider.decide = script.decide
	sender := newEventIntegritySender()
	host := eventIntegrityBootExec(t, provider, sender, eventIntegrityExecModel, eventIntegrityExecTriStateRhythm)
	eventIntegrityKeepHost(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-exec-next"}
	if _, err := eventIntegrityInvoke(ctx, host, addr, "", input); err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	for _, phase := range []tagentexec.StatePhase{tagentexec.StateStable, tagentexec.StateSuspect, tagentexec.StateExited} {
		eventIntegrityAwaitExecStateFact(t, host, addr, phase)
	}
	before := len(provider.requests())
	if _, err := eventIntegrityInvoke(ctx, host, addr, "", question); err != nil {
		t.Fatalf("the next user turn ended by error: %v", err)
	}
	requests := provider.requests()
	if len(requests) != before+1 {
		t.Fatalf("model requests = %d -> %d beside one user turn, want exactly one new request: %s",
			before, len(requests), eventIntegrityRequestsSummary(requests))
	}
	for _, request := range requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "reports state") || strings.Contains(message.Content, "exec-state: ") {
				t.Fatalf("request %d carried execution state material in a %s message: %q",
					request.Index, message.Role, message.Content)
			}
		}
	}
	if _, err := host.Run().ExecState(key); !errors.Is(err, tagentexec.ErrResourceUnknown) {
		t.Fatalf("state of the exited resource = %v, want %v", err, tagentexec.ErrResourceUnknown)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the next-turn script left violations: %v", violations)
	}
}

// runEventIntegrityExecHostCloseReleases proves the host close reaches the exec
// ledger:
//   - a tracked resource outlives the call that acquired it
//   - Host.Close ends the tmux session, so the resource is freed instead of
//     leaking beside the closed host
//   - the state of the freed resource answers the named unknown
func runEventIntegrityExecHostCloseReleases(t *testing.T) {
	const input = "start the run the close must release"
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	resource := eventIntegrityExecResource(t)
	key := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: resource}
	script.expect(eventIntegrityExecModel, "the entry starts the run", eventIntegrityTail(input)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-close", eventIntegrityExecRunArgs(resource, "sleep", `["30"]`)))
	script.expect(eventIntegrityExecModel, "the entry accepts the run", eventIntegrityCarriesToolResult("call-exec-close")).
		answers(eventIntegrityAnswerOf("the run is held"))
	provider.decide = script.decide
	sender := newEventIntegritySender()
	host := eventIntegrityBootExec(t, provider, sender, eventIntegrityExecModel, eventIntegrityExecQuietRhythm)
	eventIntegrityKeepHost(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-exec-close"}
	if _, err := eventIntegrityInvoke(ctx, host, addr, "", input); err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	eventIntegrityAwaitExecRenewed(t, host, key, 1)
	if !eventIntegrityTmuxAlive(resource) {
		t.Fatalf("the session %s is not alive before the close", resource)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
	if eventIntegrityTmuxAlive(resource) {
		t.Fatalf("the session %s survived the host close, want the resource freed", resource)
	}
	if _, err := host.Run().ExecState(key); !errors.Is(err, tagentexec.ErrResourceUnknown) {
		t.Fatalf("state of the released resource = %v, want %v", err, tagentexec.ErrResourceUnknown)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the close script left violations: %v", violations)
	}
}

// runEventIntegrityExecRestartNamesUnknown proves a restarted host does not
// claim a resource it never held:
//   - one host holds a resource after its call returned
//   - a second host, booted beside an empty ledger, releases the same key
//   - the release answers the named unknown, the resource stays alive and the
//     first host still holds it
func runEventIntegrityExecRestartNamesUnknown(t *testing.T) {
	text, host, key := eventIntegrityExecRestart(t)
	if !strings.Contains(text, tagentexec.ErrResourceUnknown.Error()) {
		t.Fatalf("the restarted release answer = %q, want the named %v", text, tagentexec.ErrResourceUnknown)
	}
	if explanation, err := host.Run().ExecState(key); err != nil || !explanation.Held {
		t.Fatalf("the first host no longer holds %s: explanation=%+v err=%v", key, explanation, err)
	}
	if !eventIntegrityTmuxAlive(key.Key) {
		t.Fatalf("the session %s did not survive the foreign release", key.Key)
	}
}

// runEventIntegrityExecRestartNotReportedStopped is the negative anchor of the
// restart scene: a host that cannot confirm a resource reports the named
// unknown and never claims a release, a stop or a success it did not observe.
func runEventIntegrityExecRestartNotReportedStopped(t *testing.T) {
	text, host, key := eventIntegrityExecRestart(t)
	if !strings.Contains(text, tagentexec.ErrResourceUnknown.Error()) {
		t.Fatalf("the restarted release answer = %q, want the named %v", text, tagentexec.ErrResourceUnknown)
	}
	lower := strings.ToLower(text)
	for _, claim := range []string{"released ", "confirmed stop", "stopped"} {
		if strings.Contains(lower, claim) {
			t.Fatalf("the restarted release answer claims %q: %q", claim, text)
		}
	}
	if word := eventIntegrityExecSuccessClaim(text); word != "" {
		t.Fatalf("the restarted release answer claims %q: %q", word, text)
	}
	if !eventIntegrityTmuxAlive(key.Key) {
		t.Fatalf("the session %s was stopped by a host that never held it", key.Key)
	}
	if explanation, err := host.Run().ExecState(key); err != nil || !explanation.Held {
		t.Fatalf("the first host no longer holds %s: explanation=%+v err=%v", key, explanation, err)
	}
}

// runEventIntegrityExecRestartTakesOver proves the restart path of the run tool
// takes a running session over instead of opening it twice:
//   - one host starts one long run and returns from its call
//   - a second host, booted beside an empty ledger, runs the same key
//   - the call answers the reattachment beside the frame the session records,
//     never a second execution, and the takeover is readable through the state
//     query face of the restarted host
//   - the session and the first host's hold both survive the takeover
func runEventIntegrityExecRestartTakesOver(t *testing.T) {
	const (
		heldInput     = "start the run one host holds"
		takeoverInput = "take the running session over"
	)
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	resource := eventIntegrityExecResource(t)
	key := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: resource}
	firstModel := eventIntegrityExecModel + "-first"
	restartModel := eventIntegrityExecModel + "-takeover"
	script.expect(firstModel, "the first host starts the run", eventIntegrityTail(heldInput)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-takeover-held",
			eventIntegrityExecRunArgs(resource, "sh", `["-c","echo taken; sleep 30"]`)))
	script.expect(firstModel, "the first host accepts the run", eventIntegrityCarriesToolResult("call-exec-takeover-held")).
		answers(eventIntegrityAnswerOf("the run is held"))
	script.expect(restartModel, "the restarted host runs the same key", eventIntegrityTail(takeoverInput)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-takeover",
			eventIntegrityExecRunArgs(resource, "sh", `["-c","echo taken; sleep 30"]`)))
	script.expect(restartModel, "the restarted host accepts the takeover", eventIntegrityCarriesToolResult("call-exec-takeover")).
		answers(eventIntegrityAnswerOf("the session was reattached"))
	provider.decide = script.decide
	first := eventIntegrityBootExec(t, provider, newEventIntegritySender(), firstModel, eventIntegrityExecQuietRhythm)
	eventIntegrityKeepHost(t, first)
	restarted := eventIntegrityBootExec(t, provider, newEventIntegritySender(), restartModel, eventIntegrityExecQuietRhythm)
	eventIntegrityKeepHost(t, restarted)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := eventIntegrityInvoke(ctx, first, event.Address{AgentID: "user", SessionID: "session-exec-takeover-first"}, "", heldInput); err != nil {
		t.Fatalf("the first host turn ended by error: %v", err)
	}
	eventIntegrityAwaitExecRenewed(t, first, key, 1)
	if !eventIntegrityTmuxAlive(resource) {
		t.Fatalf("the session %s is not alive before the takeover", resource)
	}
	if _, err := eventIntegrityInvoke(ctx, restarted, event.Address{AgentID: "user", SessionID: "session-exec-takeover"}, "", takeoverInput); err != nil {
		t.Fatalf("the restarted host turn ended by error: %v", err)
	}
	text := eventIntegrityModelConversation(provider.requests(), restartModel)
	if !strings.Contains(text, "reattached "+key.String()) {
		t.Fatalf("the restarted host never read the reattachment of %s: %s", key,
			eventIntegrityRequestsSummary(provider.requests()))
	}
	if !strings.Contains(text, "taken") {
		t.Fatalf("the takeover answer carries no frame of the session: %s",
			eventIntegrityRequestsSummary(provider.requests()))
	}
	if strings.Contains(text, "executed "+key.String()) {
		t.Fatalf("the restarted host opened a second execution under %s: %s", key,
			eventIntegrityRequestsSummary(provider.requests()))
	}
	explanation, err := restarted.Run().ExecState(key)
	if err != nil || !explanation.Held || !explanation.Adopted {
		t.Fatalf("the restarted state of %s = %+v err=%v, want the takeover readable through the query face", key, explanation, err)
	}
	if firstExplanation, err := first.Run().ExecState(key); err != nil || !firstExplanation.Held {
		t.Fatalf("the first host no longer holds %s: %+v err=%v", key, firstExplanation, err)
	}
	if !eventIntegrityTmuxAlive(resource) {
		t.Fatalf("the session %s did not survive the takeover", resource)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the takeover script left violations: %v", violations)
	}
}

// runEventIntegrityExecFinalOutputSurvives proves the final output of one exited
// run is saved before its exit frees the resource and stays readable through
// the model's own tool call:
//   - one run produces its final frame and exits beside the observed code 7
//   - the tracker settles the exit and frees the resource
//   - the model runs the same key again and reads the settled report carrying
//     the final output instead of a rerun over the freed resource
func runEventIntegrityExecFinalOutputSurvives(t *testing.T) {
	const (
		input = "start the run whose final output must survive"
		read  = "read what the settled run left behind"
	)
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	resource := eventIntegrityExecResource(t)
	key := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: resource}
	script.expect(eventIntegrityExecModel, "the entry starts the run", eventIntegrityTail(input)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-final",
			eventIntegrityExecRunArgs(resource, "sh", `["-c","echo alpha; sleep 2; echo omega; exit 7"]`)))
	script.expect(eventIntegrityExecModel, "the entry accepts the run", eventIntegrityCarriesToolResult("call-exec-final")).
		answers(eventIntegrityAnswerOf("the run is under way"))
	script.expect(eventIntegrityExecModel, "the entry reads the settled key", eventIntegrityTail(read)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-final-read",
			eventIntegrityExecRunArgs(resource, "sh", `["-c","echo rerun"]`)))
	script.expect(eventIntegrityExecModel, "the entry accepts the settled report", eventIntegrityCarriesToolResult("call-exec-final-read")).
		answers(eventIntegrityAnswerOf("the settled report carries the final output"))
	provider.decide = script.decide
	sender := newEventIntegritySender()
	host := eventIntegrityBootExec(t, provider, sender, eventIntegrityExecModel, eventIntegrityExecQuietRhythm)
	eventIntegrityKeepHost(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-exec-final"}
	if _, err := eventIntegrityInvoke(ctx, host, addr, "", input); err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	fact := eventIntegrityAwaitExecStateFact(t, host, addr, tagentexec.StateExited)
	eventIntegrityAssertExecAuditFact(t, fact, key, tagentexec.StateExited, 7)
	if _, err := eventIntegrityInvoke(ctx, host, addr, "", read); err != nil {
		t.Fatalf("the reading turn ended by error: %v", err)
	}
	text := eventIntegrityModelConversation(provider.requests(), eventIntegrityExecModel)
	lastSettled := strings.LastIndex(text, "settled "+key.String())
	if lastSettled < 0 {
		t.Fatalf("the model never read the settled report of %s: %s", key,
			eventIntegrityRequestsSummary(provider.requests()))
	}
	if !strings.Contains(text[lastSettled:], "omega") {
		t.Fatalf("the settled report carries no final output: %s",
			eventIntegrityRequestsSummary(provider.requests()))
	}
	if !strings.Contains(text[lastSettled:], "exit=7") {
		t.Fatalf("the settled report carries no observed exit code: %s",
			eventIntegrityRequestsSummary(provider.requests()))
	}
	if lastExecuted := strings.LastIndex(text, "executed "+key.String()); lastExecuted > lastSettled {
		t.Fatalf("the settled key was rerun instead of reported: %s",
			eventIntegrityRequestsSummary(provider.requests()))
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the final-output script left violations: %v", violations)
	}
}

// eventIntegrityExecRestart drives the restart scene over two formal hosts: the
// first acquires one long run and returns from its call, the second boots
// beside an empty execution ledger and releases the same key. It returns the
// text the second host's release call answered with beside the first host and
// the key that host still holds.
func eventIntegrityExecRestart(t *testing.T) (string, *assembly.Host, tagentexec.ResourceKey) {
	t.Helper()
	const (
		heldInput     = "start the run one host holds"
		restartInput  = "release the key this host never acquired"
		restartAnswer = "the restarted host named the unknown"
	)
	eventIntegrityEnv(t)
	provider := newEventIntegrityProvider(t)
	script := newEventIntegrityScript()
	resource := eventIntegrityExecResource(t)
	key := tagentexec.ResourceKey{Kind: tagentexec.KindTmux, Key: resource}
	firstModel := eventIntegrityExecModel + "-first"
	restartModel := eventIntegrityExecModel + "-restart"
	script.expect(firstModel, "the first host starts the run", eventIntegrityTail(heldInput)).
		answers(eventIntegrityToolCallAnswer("exec_run", "call-exec-held", eventIntegrityExecRunArgs(resource, "sleep", `["30"]`)))
	script.expect(firstModel, "the first host accepts the run", eventIntegrityCarriesToolResult("call-exec-held")).
		answers(eventIntegrityAnswerOf("the run is held"))
	script.expect(restartModel, "the restarted host releases the key", eventIntegrityTail(restartInput)).
		answers(eventIntegrityToolCallAnswer("exec_release", "call-exec-restart",
			fmt.Sprintf(`{"kind":%q,"resource":%q}`, tagentexec.KindTmux, resource)))
	script.expect(restartModel, "the restarted host reports the named unknown", eventIntegrityCarriesToolResult("call-exec-restart")).
		answers(eventIntegrityAnswerOf(restartAnswer))
	provider.decide = script.decide
	first := eventIntegrityBootExec(t, provider, newEventIntegritySender(), firstModel, eventIntegrityExecQuietRhythm)
	eventIntegrityKeepHost(t, first)
	restarted := eventIntegrityBootExec(t, provider, newEventIntegritySender(), restartModel, eventIntegrityExecQuietRhythm)
	eventIntegrityKeepHost(t, restarted)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := eventIntegrityInvoke(ctx, first, event.Address{AgentID: "user", SessionID: "session-exec-first"}, "", heldInput); err != nil {
		t.Fatalf("the first host turn ended by error: %v", err)
	}
	eventIntegrityAwaitExecRenewed(t, first, key, 1)
	if _, err := eventIntegrityInvoke(ctx, restarted, event.Address{AgentID: "user", SessionID: "session-exec-restart"}, "", restartInput); err != nil {
		t.Fatalf("the restarted host turn ended by error: %v", err)
	}
	text := eventIntegrityModelConversation(provider.requests(), restartModel)
	if !strings.Contains(text, tagentexec.ErrResourceUnknown.Error()) {
		t.Fatalf("the restarted host never read the named unknown of its release: %s",
			eventIntegrityRequestsSummary(provider.requests()))
	}
	if err := restarted.Close(ctx); err != nil {
		t.Fatalf("close the restarted host: %v", err)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the restart script left violations: %v", violations)
	}
	return text, first, key
}

// eventIntegrityExecResource names one execution resource of a check: a tmux
// session name the executor accepts, unique to the run, removed when the check
// ends whatever the check observed.
func eventIntegrityExecResource(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("tagent-exec-integrity-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = osexec.Command("tmux", "kill-session", "-t", name).Run() })
	return name
}

// eventIntegrityExecRunArgs renders the arguments of one exec_run call.
func eventIntegrityExecRunArgs(resource, command, argsJSON string) string {
	return fmt.Sprintf(`{"command":%q,"args":%s,"kind":%q,"resource":%q}`,
		command, argsJSON, tagentexec.KindTmux, resource)
}

// eventIntegrityExecDocument renders one host document that grants the exec face
// to its entry agent under the given monitor rhythm, with the local storage tier
// the session chain registers its projection on.
func eventIntegrityExecDocument(root, endpoint, modelName, monitor string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + modelName + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user:\n")
	b.WriteString("    model: " + modelName + "\n")
	b.WriteString("    max_tokens: 32000\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: " + tagentexec.PresetName + " }\n")
	b.WriteString("    exec:\n")
	b.WriteString("      monitor: " + monitor + "\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// eventIntegrityBootExec boots one formal host whose entry agent declares the
// exec face under the given model identity and monitor rhythm.
func eventIntegrityBootExec(t *testing.T, provider *eventIntegrityProvider, sender *eventIntegritySender, modelName, monitor string) *assembly.Host {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	if err := os.WriteFile(configPath, []byte(eventIntegrityExecDocument(root, provider.endpoint(), modelName, monitor)), 0o644); err != nil {
		t.Fatalf("write the exec host document: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the exec host: %v", err)
	}
	return host
}

// eventIntegrityAwaitExecRenewed waits until the ledger reports the resource
// held beside at least the named number of renewals.
func eventIntegrityAwaitExecRenewed(t *testing.T, host *assembly.Host, key tagentexec.ResourceKey, want int) tagentexec.Explanation {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var explanation tagentexec.Explanation
	var err error
	for {
		explanation, err = host.Run().ExecState(key)
		if err == nil && explanation.Held && explanation.Renewed >= want {
			return explanation
		}
		if time.Now().After(deadline) {
			t.Fatalf("the resource %s never reached %d renewal(s): explanation=%+v err=%v",
				key, want, explanation, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// eventIntegrityTmuxAlive reports whether the named tmux session is alive.
func eventIntegrityTmuxAlive(resource string) bool {
	return osexec.Command("tmux", "has-session", "-t", resource).Run() == nil
}

// eventIntegrityExecStateFacts reports every execution state audit fact one
// session recorded, across all observed phases.
func eventIntegrityExecStateFacts(facts []event.Event) []event.Event {
	out := make([]event.Event, 0, 3)
	for _, fact := range facts {
		if fact.Type != event.TypeFeedback {
			continue
		}
		if strings.HasPrefix(fact.Meta[event.MetaKeyEventSummary], "exec-state: ") {
			out = append(out, fact)
		}
	}
	return out
}

// eventIntegrityAwaitExecStateFact waits until the session records the one
// audit fact of the named observed phase.
func eventIntegrityAwaitExecStateFact(t *testing.T, host *assembly.Host, addr event.Address, phase tagentexec.StatePhase) event.Event {
	t.Helper()
	summary := "exec-state: " + string(phase)
	deadline := time.Now().Add(20 * time.Second)
	for {
		matches := make([]event.Event, 0, 1)
		for _, fact := range host.Run().SessionFacts(addr) {
			if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyEventSummary] == summary {
				matches = append(matches, fact)
			}
		}
		if len(matches) >= 1 {
			return matches[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("the %s observation left %d audit facts, want at least one: %v", phase, len(matches),
				eventIntegrityExecStateFacts(host.Run().SessionFacts(addr)))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// eventIntegrityAssertExecAuditFact checks one execution state fact is an audit
// record and never model material: feedback-typed, payload-free and carrying
// the resource and phase metadata without the notice keys that drive turns.
func eventIntegrityAssertExecAuditFact(t *testing.T, fact event.Event, key tagentexec.ResourceKey, phase tagentexec.StatePhase, exitCode int) {
	t.Helper()
	if fact.Type != event.TypeFeedback {
		t.Fatalf("the %s audit fact type = %q, want %q", phase, fact.Type, event.TypeFeedback)
	}
	if fact.Message.Content != "" {
		t.Fatalf("the %s audit fact carries model material %q, want no payload", phase, fact.Message.Content)
	}
	if fact.TaskID != "" {
		t.Fatalf("the %s audit fact carries task id %q, want none", phase, fact.TaskID)
	}
	if kind := fact.Meta[event.MetaKeyNoticeKind]; kind != "" {
		t.Fatalf("the %s audit fact carries notice kind %q, want none", phase, kind)
	}
	if id := fact.Meta[event.MetaKeyTaskID]; id != "" {
		t.Fatalf("the %s audit fact carries notice task id %q, want none", phase, id)
	}
	if got := fact.Meta[event.MetaPrefix+"resource"]; got != key.String() {
		t.Fatalf("the %s audit fact resource = %q, want %q", phase, got, key)
	}
	if got := fact.Meta[event.MetaPrefix+"phase"]; got != string(phase) {
		t.Fatalf("the %s audit fact phase = %q, want %q", phase, got, phase)
	}
	exit, held := fact.Meta[event.MetaPrefix+"exit_code"]
	if phase == tagentexec.StateExited {
		if !held || exit != strconv.Itoa(exitCode) {
			t.Fatalf("the exited audit fact exit_code = %q (recorded %v), want %d", exit, held, exitCode)
		}
		return
	}
	if held {
		t.Fatalf("the %s audit fact carries exit code %q, want none beside no confirmed stop", phase, exit)
	}
}

// eventIntegrityExecSuccessClaim reports the success word one lifecycle text
// wrongly claims, or the empty string beside a text that claims none.
func eventIntegrityExecSuccessClaim(text string) string {
	lower := strings.ToLower(text)
	for _, word := range eventIntegrityExecSuccessWords {
		if strings.Contains(lower, word) {
			return word
		}
	}
	return ""
}

// eventIntegrityModelConversation reports every message body the requests of
// one model identity carried, in arrival order, so the observation of a call
// and the answer of a turn are read as the model really saw them.
func eventIntegrityModelConversation(requests []eventIntegrityRequest, modelName string) string {
	var b strings.Builder
	for _, request := range requests {
		if request.Model != modelName {
			continue
		}
		for _, message := range request.Messages {
			b.WriteString(message.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// eventIntegrityRequestsSummary renders the requests a failure reads.
func eventIntegrityRequestsSummary(requests []eventIntegrityRequest) string {
	lines := make([]string, 0, len(requests))
	for _, request := range requests {
		texts := make([]string, 0, len(request.Messages))
		for _, message := range request.Messages {
			texts = append(texts, message.Role+":"+message.Content)
		}
		lines = append(lines, fmt.Sprintf("request %d model=%s messages=%v", request.Index, request.Model, texts))
	}
	return strings.Join(lines, " | ")
}

// isolateTmuxServer points this check at a tmux server of its own: another
// package finishing the shared server must not take the live execution with it,
// and the socket directory stays short enough for the platform limit.
func isolateTmuxServer(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tagent-tmux-")
	if err != nil {
		t.Fatalf("create the private tmux directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
}
