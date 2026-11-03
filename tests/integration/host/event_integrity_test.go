// TestEventIntegrityReproduction reproduces the two candidate consumption
// defects of a settled task notice over the formal Boot assembly, beside the
// normal control and the identity correlation of the same path:
// - control_normal_path: a normal user turn answers once, with one input fact per decision
// - duplicate_notice_material: one notice identity must generate exactly one new user-input fact
// - busy_notice_loss: a notice arriving while a follow-up is in flight must be queued, not dropped
// - request_and_send_identity_correlation: every Boot request and sender action maps back to the notice identity
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestEventIntegrityReproduction drives the four fixed reproduction anchors of the contract-evidence leaf over the integrity harness's Boot assembly.
// - The parent selector names all four subtests, so a red diagnostic run is read from the named child terminal, never from a package failure alone.
func TestEventIntegrityReproduction(t *testing.T) {
	reproductionEnv(t)
	t.Run("control_normal_path", runControlNormalPath)
	t.Run("duplicate_notice_material", runDuplicateNoticeMaterial)
	t.Run("busy_notice_loss", runBusyNoticeLoss)
	t.Run("request_and_send_identity_correlation", runRequestAndSendIdentityCorrelation)
}

// reproductionEnv fixes the deployment environment every reproduction subtest
// boots under.
func reproductionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
}

// bootReproductionHost boots the entry document the reproduction anchors drive:
// the entry agent keeps the send tool beside the declared worker agent, and the
// model window is declared so the session chain carries the projection and the
// notice feed the follow-up range reads.
func bootReproductionHost(t *testing.T, provider *scriptedProvider, sender recordingSender) *assembly.Host {
	t.Helper()
	return bootIntegrityHostWithAgents(t, provider, sender,
		"  user:\n"+
			"    model: model-entry\n"+
			"    max_tokens: 32000\n"+
			"    tools:\n"+
			"      - { kind: tool, id: send }\n"+
			"      - { kind: agent, agent: worker }\n"+
			"  worker:\n"+
			"    model: model-worker\n")
}

// requestCarriesSettleNotice matches the request that carries the settled
// notice of the named worker body: the material the formal settle turn reads is
// the task and the terminal it settled on beside the result body.
func requestCarriesSettleNotice(body string) func(scriptedRequest) bool {
	return func(request scriptedRequest) bool {
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

// reproductionSettleFacts reports the settled delivery facts whose material
// carries the named worker body.
func reproductionSettleFacts(facts []event.Event, body string) []event.Event {
	body = strings.TrimSpace(body)
	var out []event.Event
	for _, fact := range facts {
		if fact.Type != event.TypeTaskSettled {
			continue
		}
		if fact.Meta[event.MetaKeySettleTriggerSource] == "" {
			continue
		}
		if !strings.Contains(fact.Message.Content, body) {
			continue
		}
		out = append(out, fact)
	}
	return out
}

// reproductionSettleMaterial reports the settled material of one delegated task
// and the notice identity it carries: the transport record when the chain holds
// one, and the notice feedback fact the consumed settlement commits otherwise —
// an attended settlement may be consumed inside the flying segment and never
// leave a transport record of its own.
//
// The material is awaited rather than read off one snapshot: the delegated round
// runs on the instance of its own execution site beside the receipt the admission
// already returned, so its settle lands after the turn that issued the call ended —
// the ordering the settle face is built for, and the same wait the busy-notice
// saturation reads its delivery fact with.
func reproductionSettleMaterial(t *testing.T, host *assembly.Host, addr event.Address, body string, timeout time.Duration) (string, task.Notice) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		facts := host.Run().SessionFacts(addr)
		settles := reproductionSettleFacts(facts, body)
		if len(settles) > 1 {
			t.Fatalf("settled delivery facts carrying %q = %d, want at most 1; the chain held %s",
				body, len(settles), reproductionFactsSummary(facts))
		}
		if len(settles) == 1 {
			return strings.TrimSpace(settles[0].Message.Content), reproductionNoticeOf(settles[0])
		}
		for _, fact := range facts {
			if fact.Type != event.TypeFeedback || fact.Meta[event.MetaKeyNoticeKind] == "" {
				continue
			}
			if strings.Contains(fact.Message.Content, body) {
				return strings.TrimSpace(fact.Message.Content), reproductionNoticeOf(fact)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no fact carries the settled material %q within %s; the chain held %s",
				body, timeout, reproductionFactsSummary(facts))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// reproductionNoticeOf reconstructs the notification identity one settled
// delivery fact reports: the stable four-tuple the follow-up turn commits under.
func reproductionNoticeOf(fact event.Event) task.Notice {
	taskID := fact.TaskID
	if taskID == "" {
		taskID = event.TaskID(fact.Meta[event.MetaKeyTaskID])
	}
	kind := task.NoticeKind(fact.Meta[event.MetaKeyNoticeKind])
	if kind == "" {
		kind = task.NoticeKind(fact.Meta[event.MetaKeySettleTriggerSource])
	}
	return task.Notice{TaskID: taskID, Kind: kind}
}

// reproductionAwaitNoticeFact waits for the one notice feedback fact the notice
// turn of a settlement commits. That turn lands on its own thread beside the
// answer that consumed its material, so the check reads the chain until the
// fact arrives instead of assuming the turn already ran.
func reproductionAwaitNoticeFact(t *testing.T, host *assembly.Host, addr event.Address, taskID event.TaskID, material string) event.Event {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		facts := host.Run().SessionFacts(addr)
		var found []event.Event
		for _, fact := range facts {
			if fact.Type != event.TypeFeedback || fact.Meta[event.MetaKeyNoticeKind] == "" {
				continue
			}
			if fact.TaskID != taskID || strings.TrimSpace(fact.Message.Content) != strings.TrimSpace(material) {
				continue
			}
			found = append(found, fact)
		}
		if len(found) > 1 {
			t.Fatalf("the chain carried %d notice facts of task %s, want exactly 1; facts %s",
				len(found), taskID, reproductionFactsSummary(found))
		}
		if len(found) == 1 {
			return found[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no notice fact of task %s carrying the settled material arrived within 30s; facts %s",
				taskID, reproductionFactsSummary(facts))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// reproductionInputFacts reports the new user-input facts that carry the notice
// material: the feedback fact a notice commits beside the external input fact a
// follow-up range commits. A fact that is the transport record of the notice
// itself is not an input fact, and a body repeated in another invocation's
// history is not a second input of this notice.
func reproductionInputFacts(facts []event.Event, material string) []event.Event {
	material = strings.TrimSpace(material)
	var out []event.Event
	for _, fact := range facts {
		if fact.Message.Role != model.RoleUser {
			continue
		}
		if strings.TrimSpace(fact.Message.Content) != material {
			continue
		}
		switch fact.Type {
		case event.TypeFeedback, event.TypeExternalInput, event.TypeTaskSettled:
			out = append(out, fact)
		}
	}
	return out
}

// reproductionInputConsumers reports the input facts that name the range
// consuming the material: the transport record of a settled result (key prefix
// settle-) names the call's own range, so it is the delivery the range reads,
// never a second input of the same material.
func reproductionInputConsumers(inputFacts []event.Event) []event.Event {
	consumers := make([]event.Event, 0, len(inputFacts))
	for _, fact := range inputFacts {
		if strings.HasPrefix(string(fact.Key), "settle-") {
			continue
		}
		consumers = append(consumers, fact)
	}
	return consumers
}

// reproductionUserCopies reports how many user messages of one request carry
// the given material. The count is read as the consumption surface beside the
// fact count, never as the duplicate assertion itself: a body may legitimately
// reappear in history.
func reproductionUserCopies(request scriptedRequest, material string) int {
	material = strings.TrimSpace(material)
	copies := 0
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) && strings.TrimSpace(message.Content) == material {
			copies++
		}
	}
	return copies
}

// reproductionToolResults reports how many messages of one request carry the
// result of the named tool call.
func reproductionToolResults(request scriptedRequest, callID string) int {
	count := 0
	for _, message := range request.Messages {
		if message.ToolCallID == callID {
			count++
		}
	}
	return count
}

// reproductionCarriesToolCall reports whether one request carries the named
// tool call beside the argument fragment it issued.
func reproductionCarriesToolCall(request scriptedRequest, name, callID, argument string) bool {
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			if call.ID == callID && call.Name == name && strings.Contains(call.Arguments, argument) {
				return true
			}
		}
	}
	return false
}

// reproductionRequestsCarrying reports the indices of the requests that carry
// the given notice material as one of their user messages.
func reproductionRequestsCarrying(requests []scriptedRequest, material string) []int {
	material = strings.TrimSpace(material)
	var out []int
	for index, request := range requests {
		for _, message := range request.Messages {
			if message.Role == string(model.RoleUser) && strings.TrimSpace(message.Content) == material {
				out = append(out, index)
				break
			}
		}
	}
	return out
}

// reproductionNoticeKeyCount reports how many facts of one chain carry the
// given notice fact key.
func reproductionNoticeKeyCount(facts []event.Event, key event.EventID) int {
	count := 0
	for _, fact := range facts {
		if fact.Key == key {
			count++
		}
	}
	return count
}

// reproductionFactSummary renders one fact as the line a failed check reads.
func reproductionFactSummary(fact event.Event) string {
	return fmt.Sprintf("key=%s type=%s role=%s invocation=%s task=%s content=%q",
		fact.Key, fact.Type, fact.Message.Role, fact.InvocationID, fact.TaskID, fact.Message.Content)
}

// reproductionFactsSummary renders the facts of one check as one line.
func reproductionFactsSummary(facts []event.Event) string {
	parts := make([]string, 0, len(facts))
	for _, fact := range facts {
		parts = append(parts, reproductionFactSummary(fact))
	}
	return "[" + strings.Join(parts, "; ") + "]"
}

// reproductionWaitForSettleFact waits until the chain carries exactly one
// settled delivery fact for the named worker body and answers it.
func reproductionWaitForSettleFact(t *testing.T, host *assembly.Host, addr event.Address, body string, timeout time.Duration) event.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last []event.Event
	for {
		last = host.Run().SessionFacts(addr)
		if settles := reproductionSettleFacts(last, body); len(settles) == 1 {
			return settles[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no single settled delivery fact carried %q within %s; the chain held %s",
				body, timeout, reproductionFactsSummary(last))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// reproductionWaitForBody waits until the sender records the named body.
func reproductionWaitForBody(t *testing.T, sender recordingSender, body string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, recorded := range sender.bodies() {
			if recorded == body {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sender never recorded %q within %s; recorded bodies = %q",
				body, timeout, sender.bodies())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// runControlNormalPath drives the normal single-path control: one user input,
// one send tool call, one final answer. Every decision material enters exactly
// once, and every recorded action maps back to the request that caused it. The
// send call is answered by name and delivers nothing outbound, so the one action
// this path causes is the final answer.
func runControlNormalPath(t *testing.T) {
	const (
		input  = "report the control note"
		note   = "the control note"
		answer = "the control path settled"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	sendStep := script.expect("model-entry", "the entry sends its control note", requestTail(input)).
		answers(settleToolCallAnswer("send", "call-control-1", `{"content":"`+note+`"}`)).
		park()
	answerStep := script.expect("model-entry", "the entry ends on its control answer", requestCarriesToolResult("call-control-1")).
		answers(scriptedAnswerOf(answer)).
		delivers(answer).
		park()
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-reproduction-control"}

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, input)
		driven <- integrityOutcome{turn: turn, err: err}
	}()

	script.await(t, sendStep)
	if actions := sender.actions(); len(actions) != 0 {
		t.Fatalf("outbound actions before the first decision answered = %+v, want none", actions)
	}
	script.release(sendStep)

	script.await(t, answerStep)
	if actions := sender.actions(); len(actions) != 0 {
		t.Fatalf("outbound actions before the control answer = %+v, want none: the send face delivers nothing outbound", actions)
	}
	if !scriptedSawContent(provider, "[send_denied] ") {
		t.Fatalf("the control send call read back no named denial, want the refusal of a call that names no destination")
	}
	script.release(answerStep)

	got := awaitIntegrityDrive(t, driven, "the control turn")
	if got.err != nil {
		t.Fatalf("the control turn ended by error: %v", got.err)
	}
	if got.turn.Terminal != model.KindCompleted || got.turn.DecisionCount != 2 {
		t.Fatalf("control turn = (%v, %d decisions), want the completed turn of its two decisions",
			got.turn.Terminal, got.turn.DecisionCount)
	}
	reproductionWaitForBody(t, sender, answer, 30*time.Second)

	requests := provider.servedRequests()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want one per control decision", len(requests))
	}
	if !requestTail(input)(requests[0]) {
		t.Fatalf("request 1 = %+v, want the user input as its tail", requests[0])
	}
	if got := reproductionUserCopies(requests[0], input); got != 1 {
		t.Fatalf("request 1 carried %d copies of the user input, want exactly 1", got)
	}
	if got := reproductionToolResults(requests[1], "call-control-1"); got != 1 {
		t.Fatalf("request 2 carried %d results of call-control-1, want exactly 1", got)
	}
	if !reproductionCarriesToolCall(requests[1], "send", "call-control-1", note) {
		t.Fatalf("request 2 did not carry the send call of the control note in its history: %+v", requests[1])
	}
	if got := reproductionUserCopies(requests[1], input); got != 1 {
		t.Fatalf("request 2 carried %d copies of the user input in history, want exactly 1", got)
	}

	actions := sender.actions()
	want := []string{answer}
	if len(actions) != len(want) {
		t.Fatalf("sender actions = %+v, want the %d actions the control path causes", actions, len(want))
	}
	for index, action := range actions {
		if action.body != want[index] || action.target != addr || !action.confirmed() {
			t.Fatalf("action %d = %+v, want %q addressed to %+v and confirmed", index+1, action, want[index], addr)
		}
	}
	if declared := script.actionBodies(); !reflect.DeepEqual(declared, sender.bodies()) {
		t.Fatalf("declared action bodies = %q, want the recorded %q", declared, sender.bodies())
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the control script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runDuplicateNoticeMaterial drives one settled notice through the formal
// Boot -> delivery -> SettleDriver -> follow-up chain and judges the notice by
// its identity, fact keys and causal attribution: the identity must generate
// exactly one new user-input fact for the follow-up range. The body may already
// appear in history; only a second newly generated input fact for the same
// notice is the defect.
//
// - A notice the settle lands beside the segment still flying is read by that segment's own next decision, and the spec allows the settled result and the notice to share one request.
// - The shape is therefore read off the requests that were served, never off a timing guess: the prefix a notice-opened segment reads carries the settled result of the earlier segment, so carrying the result names no shape while sharing one request does.
func runDuplicateNoticeMaterial(t *testing.T) {
	const (
		input       = "delegate the duplicate notice task"
		workerInput = "compute the duplicate notice total"
		workerOut   = "the duplicate notice worker reports 42"
		accepted    = "the duplicate notice task was accepted"
		follow      = "the duplicate notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry delegates the duplicate notice task", requestTail(input)).
		answers(settleToolCallAnswer("worker", "call-dup-delegate", `{"input":"`+workerInput+`"}`))
	script.expect("model-entry", "the entry accepts the delegated task", requestCarriesToolResult("call-dup-delegate")).
		answers(scriptedAnswerOf(accepted)).
		delivers(accepted)
	script.expect("model-worker", "the worker answers the delegated task", requestTail(workerInput)).
		answers(scriptedAnswerOf(workerOut))
	followStep := script.expect("model-entry", "the settled notice fires its follow-up", requestCarriesSettleNotice(workerOut)).
		answers(scriptedAnswerOf(follow)).
		delivers(follow).
		park().
		optional()
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-reproduction-duplicate"}

	turn, err := driveIntegrityTurn(ctx, host, addr, input)
	if err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("entry turn = (%v, %d decisions), want the completed turn of its two decisions",
			turn.Terminal, turn.DecisionCount)
	}
	material, notice := reproductionSettleMaterial(t, host, addr, workerOut, 30*time.Second)
	facts := host.Run().SessionFacts(addr)
	_, followIndex := continuationWaitForRequest(t, provider, material, 30*time.Second)
	baselineRequests := provider.servedRequests()
	acceptIndex := -1
	for index, request := range baselineRequests {
		if acceptIndex < 0 && requestCarriesToolResult("call-dup-delegate")(request) {
			acceptIndex = index
		}
	}
	batchedShape := acceptIndex >= 0 && followIndex == acceptIndex
	if !batchedShape {
		script.await(t, followStep)
		script.release(followStep)
	}
	reproductionWaitForBody(t, sender, continuationNoticeAnswerBody(follow, accepted, batchedShape), 30*time.Second)
	facts = host.Run().SessionFacts(addr)
	inputFacts := reproductionInputFacts(facts, material)
	if len(inputFacts) == 0 {
		t.Fatalf("no new user-input fact carries the notice material %q of identity %s; the chain held %s",
			material, notice.FactKey(), reproductionFactsSummary(facts))
	}
	consumers := reproductionInputConsumers(inputFacts)
	if len(consumers) == 0 {
		consumers = inputFacts
	}
	followUp := consumers[0].InvocationID
	for _, fact := range consumers[1:] {
		if fact.InvocationID != followUp {
			t.Fatalf("the notice material entered invocations %s and %s, want one follow-up range for identity %s; facts %s",
				followUp, fact.InvocationID, notice.FactKey(), reproductionFactsSummary(inputFacts))
		}
	}
	noticeCopies := 0
	for _, fact := range facts {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyNoticeKind] != "" &&
			strings.TrimSpace(fact.Message.Content) == strings.TrimSpace(material) {
			noticeCopies++
		}
	}
	if noticeCopies > 1 {
		t.Fatalf("the chain carried %d notice facts for identity %s, want at most 1; facts %s",
			noticeCopies, notice.FactKey(), reproductionFactsSummary(facts))
	}

	requests := provider.servedRequests()
	carrying := reproductionRequestsCarrying(requests, material)
	if len(carrying) != 1 {
		t.Fatalf("requests carrying the notice material = %v, want the one follow-up decision; requests = %+v", carrying, requests)
	}
	requestCopies := reproductionUserCopies(requests[carrying[0]], material)
	if len(consumers) != 1 {
		t.Fatalf("the follow-up notice identity %s (task %s kind %s) consumed the settled material through %d new user-input facts in invocation %s, want exactly 1: %s; the follow-up decision carried %d user copies of the material, so the same settled notice entered the model twice",
			notice.FactKey(), notice.TaskID, notice.Kind, len(consumers), followUp,
			reproductionFactsSummary(consumers), requestCopies)
	}

	followActions := 0
	for _, action := range sender.actions() {
		if action.body != follow {
			continue
		}
		followActions++
		if action.target != addr || !action.confirmed() {
			t.Fatalf("the notice-driven action = %+v, want %q addressed to %+v and confirmed", action, follow, addr)
		}
	}
	wantFollowActions := 1
	if batchedShape {
		wantFollowActions = 0
	}
	if followActions != wantFollowActions {
		t.Fatalf("send actions carrying the follow-up answer = %d in the %s shape, want %d",
			followActions, continuationShapeName(batchedShape), wantFollowActions)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the duplicate script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runBusyNoticeLoss parks the first notice-driven follow-up at the model
// barrier, then settles a second task while that follow-up is still in flight.
// The second notice must be retained and consumed by a later decision; a chain
// that only records the delivery fact and never turns again has dropped it.
func runBusyNoticeLoss(t *testing.T) {
	const (
		inputA     = "start the first background task"
		inputB     = "start the second background task"
		workerInA  = "compute the first background total"
		workerInB  = "compute the second background total"
		workerOutA = "the first background worker settled on 41"
		workerOutB = "the second background worker settled on 42"
		acceptedA  = "the first background task was accepted"
		acceptedB  = "the second background task was accepted"
		followA    = "the first background notice reached the user"
		followB    = "the second background notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry delegates the first task", requestTail(inputA)).
		answers(settleToolCallAnswer("worker", "call-busy-1", `{"input":"`+workerInA+`"}`))
	script.expect("model-entry", "the entry accepts the first task", requestCarriesToolResult("call-busy-1")).
		answers(scriptedAnswerOf(acceptedA)).
		delivers(acceptedA)
	script.expect("model-entry", "the entry delegates the second task", requestTail(inputB)).
		answers(settleToolCallAnswer("worker", "call-busy-2", `{"input":"`+workerInB+`"}`))
	script.expect("model-entry", "the entry accepts the second task", requestCarriesToolResult("call-busy-2")).
		answers(scriptedAnswerOf(acceptedB)).
		delivers(acceptedB)
	workerA := script.expect("model-worker", "the first worker answers", requestTail(workerInA)).
		answers(scriptedAnswerOf(workerOutA)).
		park()
	workerB := script.expect("model-worker", "the second worker answers", requestTail(workerInB)).
		answers(scriptedAnswerOf(workerOutB)).
		park()
	followStepA := script.expect("model-entry", "the first settled notice fires its follow-up", requestCarriesSettleNotice(workerOutA)).
		answers(scriptedAnswerOf(followA)).
		delivers(followA).
		park()
	followStepB := script.expect("model-entry", "the second settled notice fires its follow-up", requestCarriesSettleNotice(workerOutB)).
		answers(scriptedAnswerOf(followB)).
		delivers(followB).
		park()
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-reproduction-busy"}

	turnA, err := driveIntegrityTurn(ctx, host, addr, inputA)
	if err != nil {
		t.Fatalf("the first entry turn ended by error: %v", err)
	}
	if turnA.Terminal != model.KindCompleted || turnA.DecisionCount != 2 {
		t.Fatalf("first entry turn = (%v, %d decisions), want the completed turn of its two decisions",
			turnA.Terminal, turnA.DecisionCount)
	}
	script.await(t, workerA)

	turnB, err := driveIntegrityTurn(ctx, host, addr, inputB)
	if err != nil {
		t.Fatalf("the second entry turn ended by error: %v", err)
	}
	if turnB.Terminal != model.KindCompleted || turnB.DecisionCount != 2 {
		t.Fatalf("second entry turn = (%v, %d decisions), want the completed turn of its two decisions",
			turnB.Terminal, turnB.DecisionCount)
	}
	script.await(t, workerB)

	script.release(workerA)
	script.await(t, followStepA)
	script.release(workerB)
	settleB := reproductionWaitForSettleFact(t, host, addr, workerOutB, 30*time.Second)
	noticeB := reproductionNoticeOf(settleB)
	materialB := strings.TrimSpace(settleB.Message.Content)

	script.release(followStepA)
	reproductionWaitForBody(t, sender, followA, 30*time.Second)

	deadline := time.Now().Add(30 * time.Second)
	consumed := false
	for {
		if carries := reproductionRequestsCarrying(provider.servedRequests(), materialB); len(carries) > 0 {
			consumed = true
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !consumed {
		facts := host.Run().SessionFacts(addr)
		taskFacts, noticeFacts := 0, 0
		for _, fact := range facts {
			if fact.TaskID != settleB.TaskID {
				continue
			}
			taskFacts++
			if fact.Type == event.TypeFeedback {
				noticeFacts++
			}
		}
		t.Fatalf("the settled delivery fact %s (task %s kind %s) was recorded while the first follow-up was parked at the model, but no later decision consumed it within 30s: model requests = %d, sender bodies = %q, facts of task %s = %d with %d feedback notice fact(s), so the second notice was dropped instead of retained for the next turn",
			settleB.Key, settleB.TaskID, noticeB.Kind, len(provider.servedRequests()), sender.bodies(), settleB.TaskID, taskFacts, noticeFacts)
	}
	script.release(followStepB)
	reproductionWaitForBody(t, sender, followB, 30*time.Second)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the busy script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runRequestAndSendIdentityCorrelation drives the same formal notice path and
// maps the Boot requests and sender records back to the notice identity, one by
// one: the settled delivery fact, the follow-up fact key, the unique request
// that carries the material and the unique action the notice answers. The check
// commits nothing by hand; every fact it reads was committed by Boot. The
// delegation result probe pins its first carrier: the replayed chain repeats
// the pairing in every later request, so the first carrier is the accept
// decision the probe maps back.
//
// - A notice the settle lands beside the segment still flying is read by that segment's own next decision, and the spec allows the settled result and the notice to share one request.
// - The shape is therefore read off the requests that were served, never off a timing guess: the prefix a notice-opened segment reads carries the settled result of the earlier segment, so carrying the result names no shape while sharing one request does.
// - The batched shape pins that the notice request also carries the delegation tool result: the fact judgement above already pins followIndex == acceptIndex.
func runRequestAndSendIdentityCorrelation(t *testing.T) {
	const (
		input       = "delegate the correlated task"
		workerInput = "compute the correlated total"
		workerOut   = "the correlated worker reports 42"
		accepted    = "the correlated task was accepted"
		follow      = "the correlated notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry delegates the correlated task", requestTail(input)).
		answers(settleToolCallAnswer("worker", "call-correlation", `{"input":"`+workerInput+`"}`))
	script.expect("model-entry", "the entry accepts the correlated task", requestCarriesToolResult("call-correlation")).
		answers(scriptedAnswerOf(accepted)).
		delivers(accepted)
	script.expect("model-worker", "the worker answers the correlated task", requestTail(workerInput)).
		answers(scriptedAnswerOf(workerOut))
	followStep := script.expect("model-entry", "the settled notice fires its correlated follow-up", requestCarriesSettleNotice(workerOut)).
		answers(scriptedAnswerOf(follow)).
		delivers(follow).
		park().
		optional()
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-reproduction-correlation"}

	turn, err := driveIntegrityTurn(ctx, host, addr, input)
	if err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("entry turn = (%v, %d decisions), want the completed turn of its two decisions",
			turn.Terminal, turn.DecisionCount)
	}
	material, notice := reproductionSettleMaterial(t, host, addr, workerOut, 30*time.Second)
	facts := host.Run().SessionFacts(addr)
	for _, settle := range reproductionSettleFacts(facts, workerOut) {
		if got := settle.Meta[event.MetaKeySettleTriggerSource]; got != string(task.NoticeSettled) {
			t.Fatalf("the settled fact carries settle_trigger_source = %q, want %q from the formal delivery path",
				got, task.NoticeSettled)
		}
	}
	if !strings.Contains(material, string(notice.TaskID)) || !strings.Contains(material, string(notice.Kind)) {
		t.Fatalf("the settled material %q names neither the task %s nor the kind %s of its identity",
			material, notice.TaskID, notice.Kind)
	}
	_, followIndex := continuationWaitForRequest(t, provider, material, 30*time.Second)
	baselineRequests := provider.servedRequests()
	acceptIndex := -1
	for index, request := range baselineRequests {
		if acceptIndex < 0 && requestCarriesToolResult("call-correlation")(request) {
			acceptIndex = index
		}
	}
	batchedShape := acceptIndex >= 0 && followIndex == acceptIndex
	if !batchedShape {
		script.await(t, followStep)
		script.release(followStep)
	}
	reproductionWaitForBody(t, sender, continuationNoticeAnswerBody(follow, accepted, batchedShape), 30*time.Second)
	facts = host.Run().SessionFacts(addr)

	inputFacts := reproductionInputFacts(facts, material)
	if len(inputFacts) == 0 {
		t.Fatalf("no user-input fact carries the notice material %q of identity %s; the chain held %s",
			material, notice.FactKey(), reproductionFactsSummary(facts))
	}
	_ = inputFacts[0]
	noticeFact := reproductionAwaitNoticeFact(t, host, addr, notice.TaskID, material)
	if want := agent.NoticeFactKey(noticeFact.InvocationID, notice); noticeFact.Key != want {
		t.Fatalf("the notice fact key = %s, want the notice identity key %s", noticeFact.Key, want)
	}

	requests := provider.servedRequests()
	if requests[followIndex].Model != "model-entry" {
		t.Fatalf("the follow-up request named model %q, want the entry identity of the remembered user address", requests[followIndex].Model)
	}
	if acceptIndex < 0 {
		t.Fatalf("no request carried the delegation tool result of call-correlation; requests = %+v", baselineRequests)
	}
	workerIndex := -1
	for index, request := range requests {
		if request.Model == "model-worker" && requestTail(workerInput)(request) {
			workerIndex = index
		}
	}
	if workerIndex < 0 {
		t.Fatalf("no request reached the declared worker identity %q; requests = %+v", workerInput, requests)
	}
	if batchedShape {
		if !requestCarriesToolResult("call-correlation")(requests[followIndex]) {
			t.Fatalf("the notice request %d carried no delegation tool result beside the batched shape", followIndex)
		}
	} else {
		if followIndex <= acceptIndex || followIndex <= workerIndex {
			t.Fatalf("the follow-up request index = %d, want it after the delegation tool result %d and the worker decision %d",
				followIndex, acceptIndex, workerIndex)
		}
		if followIndex != len(requests)-1 {
			t.Fatalf("the follow-up request index = %d of %d, want the last decision of the chain", followIndex, len(requests))
		}
	}

	followActions := 0
	for _, action := range sender.actions() {
		if action.body != follow {
			continue
		}
		followActions++
		if action.target != addr || !action.confirmed() {
			t.Fatalf("the notice-driven action = %+v, want %q addressed to %+v and confirmed", action, follow, addr)
		}
	}
	wantFollowActions := 1
	if batchedShape {
		wantFollowActions = 0
	}
	if followActions != wantFollowActions {
		t.Fatalf("send actions carrying the follow-up answer = %d in the %s shape, want %d",
			followActions, continuationShapeName(batchedShape), wantFollowActions)
	}
	if declared := script.actionBodies(); !reflect.DeepEqual(declared, sender.bodies()) {
		t.Fatalf("declared action bodies = %q, want the recorded %q", declared, sender.bodies())
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the correlation script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// TestEventIntegrityTaskContinuation drives the task-continuation anchors: a settled notice drives one next decision, and a busy notice is retained.
// - The follow-up stays in the original session's responsibility scope and carries the original task causality.
// - One notice identity is one new input of one logical consumption.
// - A busy notice is retained and consumed by a later decision.
// - Minting a new invocation for the follow-up range is the admitted shape, so the checks read causality and non-merging, not literal reuse.
// - The telemetry presentation of an unconsumed notice is not asserted: the baseline wires no such degradation face.
// - The leaf owns consumption semantics, not presentation.
// - The windowless anchor joins the list: under every window declaration the notice reaches the follow-up from the committed fact.
// - The notice fact must still reach the follow-up from the committed fact itself.
func TestEventIntegrityTaskContinuation(t *testing.T) {
	reproductionEnv(t)
	t.Run("idle_notice_drives_next_decision", runIdleNoticeContinuation)
	t.Run("busy_notice_retained_and_consumed", runBusyNoticeRetained)
	t.Run("two_tasks_same_host_each_continuation", runTwoTaskContinuations)
	t.Run("multi_session_same_host_isolated", runMultiSessionContinuations)
	t.Run("late_notice_not_dropped", runLateNoticeContinuation)
	t.Run("no_duplicate_input_for_same_notice", runNoDuplicateNoticeInput)
	t.Run("busy_notice_not_dropped", runBusyNoticeNotDropped)
	t.Run("no_new_invocation_for_notice", runNoNewInvocationForNotice)
	t.Run("notice_visible_under_small_window", runNoticeVisibleUnderSmallWindow)
	t.Run("notice_input_without_projection", runNoticeInputWithoutProjection)
}

// expectContinuationDelegation declares the two entry decisions of one
// delegated task: the delegation call and the answer that accepts it.
func expectContinuationDelegation(script *integrityScript, name, input, callID, workerInput, accepted string) {
	script.expect("model-entry", "the entry delegates "+name, requestTail(input)).
		answers(settleToolCallAnswer("worker", callID, `{"input":"`+workerInput+`"}`))
	script.expect("model-entry", "the entry accepts "+name, requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(accepted)).
		delivers(accepted)
}

// expectContinuationWorker declares the worker decision of one delegated task.
func expectContinuationWorker(script *integrityScript, name, workerInput, workerOut string) *integrityStep {
	return script.expect("model-worker", "the worker answers "+name, requestTail(workerInput)).
		answers(scriptedAnswerOf(workerOut))
}

// expectContinuationFollowUp declares the follow-up decision one settled notice
// drives at the entry agent. The step is optional: a notice that lands while the
// entry segment still flies is read by that segment's own next decision instead
// of opening a range of its own, and both shapes read the notice material once
// (the spec allows the two sources to share one request).
func expectContinuationFollowUp(script *integrityScript, name, workerOut, follow string) *integrityStep {
	return script.expect("model-entry", "the "+name+" notice fires its follow-up", requestCarriesSettleNotice(workerOut)).
		answers(scriptedAnswerOf(follow)).
		delivers(follow).
		optional()
}

// driveContinuationTurn drives one user turn and requires the completed turn of
// its declared decisions.
func driveContinuationTurn(t *testing.T, ctx context.Context, host *assembly.Host, addr event.Address, input string, decisions int) agent.Turn {
	t.Helper()
	turn, err := driveIntegrityTurn(ctx, host, addr, input)
	if err != nil {
		t.Fatalf("the user turn %q ended by error: %v", input, err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != decisions {
		t.Fatalf("user turn %q = (%v, %d decisions), want the completed turn of %d decisions",
			input, turn.Terminal, turn.DecisionCount, decisions)
	}
	return turn
}

// continuationWaitForRequest waits until exactly one request carries the notice
// material and answers that request beside its index. A second request carrying
// the same material fails at once: one notice identity enters one decision.
func continuationWaitForRequest(t *testing.T, provider *scriptedProvider, material string, timeout time.Duration) (scriptedRequest, int) {
	t.Helper()
	material = strings.TrimSpace(material)
	deadline := time.Now().Add(timeout)
	for {
		requests := provider.servedRequests()
		carrying := reproductionRequestsCarrying(requests, material)
		switch {
		case len(carrying) == 1:
			return requests[carrying[0]], carrying[0]
		case len(carrying) > 1:
			t.Fatalf("requests carrying the notice material %q = %v, want exactly the one follow-up decision",
				material, carrying)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no request carried the notice material %q within %s; model requests = %d",
				material, timeout, len(requests))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// continuationNoticeFact reports the one feedback fact a follow-up range commits
// for the named task, and fails beside the chain when the count is not one.
func continuationNoticeFact(t *testing.T, facts []event.Event, taskID event.TaskID) event.Event {
	t.Helper()
	var out []event.Event
	for _, fact := range facts {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyTriggerSource] == "task" && fact.TaskID == taskID {
			out = append(out, fact)
		}
	}
	if len(out) != 1 {
		t.Fatalf("the chain carried %d feedback notice facts of task %s, want exactly 1: %s",
			len(out), taskID, reproductionFactsSummary(facts))
	}
	return out[0]
}

// continuationNoticeFactCount reports how many feedback notice facts of the
// named task the chain carries, without requiring the count.
func continuationNoticeFactCount(facts []event.Event, taskID event.TaskID) int {
	count := 0
	for _, fact := range facts {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyTriggerSource] == "task" && fact.TaskID == taskID {
			count++
		}
	}
	return count
}

// continuationUserInvocation reports the invocation the ready input of the
// named user turn committed under: the range identity a follow-up must stay
// beside without merging into it.
func continuationUserInvocation(t *testing.T, facts []event.Event, input string) event.InvocationID {
	t.Helper()
	input = strings.TrimSpace(input)
	for _, fact := range facts {
		if fact.Type == event.TypeExternalInput && strings.TrimSpace(fact.Message.Content) == input {
			return fact.InvocationID
		}
	}
	t.Fatalf("no external input fact carried the user input %q: %s", input, reproductionFactsSummary(facts))
	return ""
}

// continuationWaitForNotice waits until exactly one notice feedback fact of the
// named task stands on the chain and answers it.
func continuationWaitForNotice(t *testing.T, host *assembly.Host, addr event.Address, taskID event.TaskID, timeout time.Duration) event.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		facts := host.Run().SessionFacts(addr)
		if continuationNoticeFactCount(facts, taskID) == 1 {
			return continuationNoticeFact(t, facts, taskID)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the chain carried no single notice fact of task %s within %s: %s",
				taskID, timeout, reproductionFactsSummary(facts))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// continuationFollowAnswer requires the answer of the decision that read the
// notice to leave through the sender exactly once, in whichever shape the
// consumption loop records it: a notice that lands while the entry segment
// still flies is read by that segment's own next decision — the spec allows the
// settled result and the notice to share one request — so the single body that
// segment closes on answers the notice, and a notice the closed segment never
// saw opens a segment of its own whose body is the follow-up answer. The shape
// is read off the request that carried the material, never off a timing guess:
// the batched request is the one that carries the settled result as well.
func continuationFollowAnswer(t *testing.T, provider *scriptedProvider, sender recordingSender, addr event.Address, material, callID, follow, batched string) {
	t.Helper()
	_, batchedShape := continuationNoticeAnswer(t, provider, sender, material, callID, follow, batched)
	continuationRequireAction(t, sender, continuationNoticeAnswerBody(follow, batched, batchedShape), addr)
}

// continuationNoticeAnswer waits until the decision that read the notice material
// answers through the sender, and reports the request that carried the material
// beside whether the notice shared the entry segment's request. The consumption
// loop reads a notice that lands while the entry segment still flies in that
// segment's own next decision — one request carries the settled result and the
// notice — so the segment closes on the one body its decision answered; a notice
// the closed segment never saw opens a range of its own and answers with the
// follow-up body. The shape is read off the request that carried the material,
// never off a timing guess.
func continuationNoticeAnswer(t *testing.T, provider *scriptedProvider, sender recordingSender, material, callID, follow, batched string) (scriptedRequest, bool) {
	t.Helper()
	request, _ := continuationWaitForRequest(t, provider, material, 30*time.Second)
	batchedShape := requestCarriesToolResult(callID)(request)
	reproductionWaitForBody(t, sender, continuationNoticeAnswerBody(follow, batched, batchedShape), 30*time.Second)
	return request, batchedShape
}

// continuationNoticeAnswerBody reports the body the decision that read the notice
// answers with in the shape the chain records.
func continuationNoticeAnswerBody(follow, batched string, batchedShape bool) string {
	if batchedShape {
		return batched
	}
	return follow
}

// continuationShapeName names the shape one notice answer landed in, so a check
// reports the shape it read rather than a guess it made.
func continuationShapeName(batchedShape bool) string {
	if batchedShape {
		return "batched"
	}
	return "separate"
}

// continuationAnswerDelivered requires every body the declared answers of one
// notice decision leave through the sender once: the batched shape leaves the
// entry answer the notice was read beside, because the segment answers once, and
// the separate shape leaves the entry answer and then the follow-up body.
func continuationAnswerDelivered(t *testing.T, sender recordingSender, addr event.Address, accepted, follow string, batchedShape bool) {
	t.Helper()
	continuationRequireAction(t, sender, accepted, addr)
	if batchedShape {
		return
	}
	continuationRequireAction(t, sender, follow, addr)
}

// continuationSingleCopy requires the request to carry the given user material
// exactly once, the consumption surface beside the fact count.
func continuationSingleCopy(t *testing.T, request scriptedRequest, material, what string) {
	t.Helper()
	if got := reproductionUserCopies(request, material); got != 1 {
		t.Fatalf("%s carried %d user copies of %q, want exactly 1: %+v", what, got, material, request.Messages)
	}
}

// continuationAbsent requires the request to carry no user copy of the material.
func continuationAbsent(t *testing.T, request scriptedRequest, material, what string) {
	t.Helper()
	if got := reproductionUserCopies(request, material); got != 0 {
		t.Fatalf("%s carried %d user copies of %q, want none: %+v", what, got, material, request.Messages)
	}
}

// continuationHistory requires the follow-up request to carry the prior range of
// its own session: every named user input exactly once.
func continuationHistory(t *testing.T, request scriptedRequest, inputs ...string) {
	t.Helper()
	for _, input := range inputs {
		if got := reproductionUserCopies(request, input); got != 1 {
			t.Fatalf("the follow-up request carried %d copies of the earlier input %q, want the one its session committed: %+v",
				got, input, request.Messages)
		}
	}
}

// continuationRequireAction requires exactly one confirmed action carrying the
// body at the address.
func continuationRequireAction(t *testing.T, sender recordingSender, body string, addr event.Address) {
	t.Helper()
	count := 0
	for _, action := range sender.actions() {
		if action.body != body {
			continue
		}
		count++
		if action.target != addr || !action.confirmed() {
			t.Fatalf("the action %q = %+v, want it addressed to %+v and confirmed", body, action, addr)
		}
	}
	if count != 1 {
		t.Fatalf("actions carrying %q = %d, want exactly 1; recorded bodies = %q", body, count, sender.bodies())
	}
}

// continuationSettle waits for the one settled delivery fact of the body and
// answers it beside its notice identity and material.
func continuationSettle(t *testing.T, host *assembly.Host, addr event.Address, body string) (event.Event, task.Notice, string) {
	t.Helper()
	settle := reproductionWaitForSettleFact(t, host, addr, body, 30*time.Second)
	return settle, reproductionNoticeOf(settle), strings.TrimSpace(settle.Message.Content)
}

// continuationQuiet requires the provider to serve no request during one quiet
// window: the driver rests after a completed firing instead of firing again.
func continuationQuiet(t *testing.T, provider *scriptedProvider, window time.Duration) {
	t.Helper()
	before := len(provider.servedRequests())
	time.Sleep(window)
	if after := len(provider.servedRequests()); after != before {
		t.Fatalf("model requests during the %s quiet window = %d, want the %d already served", window, after, before)
	}
}

// runIdleNoticeContinuation drives one task to its terminal while the host is
// idle: the delivered notice must drive the next decision of the same session,
// carrying the notice material once beside the history the earlier range
// committed.
func runIdleNoticeContinuation(t *testing.T) {
	const (
		input       = "delegate the idle continuation task"
		workerInput = "compute the idle continuation total"
		workerOut   = "the idle continuation worker reports 42"
		accepted    = "the idle continuation task was accepted"
		follow      = "the idle continuation notice reached the user"
		callID      = "call-continuation-idle"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the idle task", input, callID, workerInput, accepted)
	worker := expectContinuationWorker(script, "the idle task", workerInput, workerOut)
	expectContinuationFollowUp(script, "the idle continuation", workerOut, follow)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-continuation-idle"}

	driveContinuationTurn(t, ctx, host, addr, input, 2)
	script.await(t, worker)
	script.release(worker)

	settle, notice, material := continuationSettle(t, host, addr, workerOut)
	request, requestIndex := continuationWaitForRequest(t, provider, material, 30*time.Second)
	userInvocation := continuationUserInvocation(t, host.Run().SessionFacts(addr), input)
	continuationFollowAnswer(t, provider, sender, addr, material, callID, follow, accepted)

	continuationSingleCopy(t, request, material, "the follow-up request")
	continuationHistory(t, request, input)
	if requestIndex != len(provider.servedRequests())-1 {
		t.Fatalf("the follow-up request index = %d, want the last decision of the chain", requestIndex)
	}

	facts := host.Run().SessionFacts(addr)
	followFact := continuationNoticeFact(t, facts, settle.TaskID)
	if followFact.InvocationID == "" || followFact.InvocationID == userInvocation {
		t.Fatalf("the follow-up fact invocation = %q, want a range of its own beside the user range %q",
			followFact.InvocationID, userInvocation)
	}
	if want := agent.NoticeFactKey(followFact.InvocationID, notice); followFact.Key != want {
		t.Fatalf("the follow-up fact key = %s, want the notice identity key %s", followFact.Key, want)
	}
	if got := followFact.Meta[event.MetaKeyTaskID]; got != string(settle.TaskID) {
		t.Fatalf("the follow-up fact carries task key %q, want %q", got, settle.TaskID)
	}
	if got := followFact.Meta[event.MetaKeyNoticeKind]; got != string(notice.Kind) {
		t.Fatalf("the follow-up fact carries notice kind %q, want %q", got, notice.Kind)
	}
	continuationRequireAction(t, sender, accepted, addr)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the idle continuation script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runBusyNoticeRetained parks the first notice-driven follow-up at the model,
// delivers a second terminal notice while that turn is in flight, and requires
// the second notice to be retained and consumed after the first turn ends: the
// driver defers the busy notice instead of dropping it.
func runBusyNoticeRetained(t *testing.T) {
	const (
		inputA     = "start the first retained task"
		inputB     = "start the second retained task"
		workerInA  = "compute the first retained total"
		workerInB  = "compute the second retained total"
		workerOutA = "the first retained worker settled on 41"
		workerOutB = "the second retained worker settled on 42"
		acceptedA  = "the first retained task was accepted"
		acceptedB  = "the second retained task was accepted"
		followA    = "the first retained notice reached the user"
		followB    = "the second retained notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the first retained task", inputA, "call-continuation-busy-1", workerInA, acceptedA)
	expectContinuationDelegation(script, "the second retained task", inputB, "call-continuation-busy-2", workerInB, acceptedB)
	workerA := expectContinuationWorker(script, "the first retained task", workerInA, workerOutA).park()
	workerB := expectContinuationWorker(script, "the second retained task", workerInB, workerOutB).park()
	followStepA := expectContinuationFollowUp(script, "first retained", workerOutA, followA).park()
	followStepB := expectContinuationFollowUp(script, "second retained", workerOutB, followB).park()
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-continuation-busy"}

	driveContinuationTurn(t, ctx, host, addr, inputA, 2)
	script.await(t, workerA)
	driveContinuationTurn(t, ctx, host, addr, inputB, 2)
	script.await(t, workerB)

	script.release(workerA)
	script.await(t, followStepA)
	settleA, _, materialA := continuationSettle(t, host, addr, workerOutA)
	requestA, indexA := continuationWaitForRequest(t, provider, materialA, 30*time.Second)

	script.release(workerB)
	settleB, noticeB, materialB := continuationSettle(t, host, addr, workerOutB)
	if carrying := reproductionRequestsCarrying(provider.servedRequests(), materialB); len(carrying) != 0 {
		t.Fatalf("the busy notice opened a decision beside the turn in flight: requests %v", carrying)
	}
	continuationQuiet(t, provider, 200*time.Millisecond)

	script.release(followStepA)
	reproductionWaitForBody(t, sender, followA, 30*time.Second)
	requestB, indexB := continuationWaitForRequest(t, provider, materialB, 30*time.Second)
	script.release(followStepB)
	reproductionWaitForBody(t, sender, followB, 30*time.Second)
	if indexB <= indexA {
		t.Fatalf("the retained follow-up request index = %d, want it after the in-flight follow-up %d", indexB, indexA)
	}
	continuationSingleCopy(t, requestA, materialA, "the in-flight follow-up request")
	continuationSingleCopy(t, requestB, materialB, "the retained follow-up request")
	continuationSingleCopy(t, requestB, materialA, "the retained follow-up request history")
	continuationHistory(t, requestB, inputA, inputB)

	facts := host.Run().SessionFacts(addr)
	followFactA := continuationNoticeFact(t, facts, settleA.TaskID)
	followFactB := continuationNoticeFact(t, facts, settleB.TaskID)
	if followFactA.Key == followFactB.Key || followFactA.InvocationID == followFactB.InvocationID {
		t.Fatalf("the two notices share identity %s/%s or range %s/%s, want one range each",
			followFactA.Key, followFactB.Key, followFactA.InvocationID, followFactB.InvocationID)
	}
	if want := agent.NoticeFactKey(followFactB.InvocationID, noticeB); followFactB.Key != want {
		t.Fatalf("the retained follow-up fact key = %s, want the notice identity key %s", followFactB.Key, want)
	}
	if got := followFactB.Meta[event.MetaKeyTaskID]; got != string(settleB.TaskID) {
		t.Fatalf("the retained follow-up fact carries task key %q, want %q", got, settleB.TaskID)
	}
	continuationRequireAction(t, sender, acceptedA, addr)
	continuationRequireAction(t, sender, acceptedB, addr)
	continuationRequireAction(t, sender, followA, addr)
	continuationRequireAction(t, sender, followB, addr)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the busy continuation script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runTwoTaskContinuations drives two delegated tasks of one session to their
// terminals, one after the other: each notice must drive a continuation of its
// own, in its own range, and neither range may consume the other's notice as a
// second new input.
func runTwoTaskContinuations(t *testing.T) {
	const (
		inputA     = "delegate the first continuation task"
		inputB     = "delegate the second continuation task"
		workerInA  = "compute the first continuation total"
		workerInB  = "compute the second continuation total"
		workerOutA = "the first continuation worker settled on 41"
		workerOutB = "the second continuation worker settled on 42"
		acceptedA  = "the first continuation task was accepted"
		acceptedB  = "the second continuation task was accepted"
		followA    = "the first continuation notice reached the user"
		followB    = "the second continuation notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the first task", inputA, "call-continuation-two-1", workerInA, acceptedA)
	expectContinuationDelegation(script, "the second task", inputB, "call-continuation-two-2", workerInB, acceptedB)
	workerA := expectContinuationWorker(script, "the first task", workerInA, workerOutA).park()
	workerB := expectContinuationWorker(script, "the second task", workerInB, workerOutB).park()
	expectContinuationFollowUp(script, "first continuation", workerOutA, followA)
	expectContinuationFollowUp(script, "second continuation", workerOutB, followB)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-continuation-two-tasks"}

	driveContinuationTurn(t, ctx, host, addr, inputA, 2)
	script.await(t, workerA)
	driveContinuationTurn(t, ctx, host, addr, inputB, 2)
	script.await(t, workerB)

	script.release(workerA)
	settleA, noticeA, materialA := continuationSettle(t, host, addr, workerOutA)
	requestA, indexA := continuationWaitForRequest(t, provider, materialA, 30*time.Second)
	reproductionWaitForBody(t, sender, followA, 30*time.Second)

	script.release(workerB)
	settleB, noticeB, materialB := continuationSettle(t, host, addr, workerOutB)
	requestB, indexB := continuationWaitForRequest(t, provider, materialB, 30*time.Second)
	reproductionWaitForBody(t, sender, followB, 30*time.Second)

	if indexB <= indexA {
		t.Fatalf("the second continuation request index = %d, want it after the first continuation %d", indexB, indexA)
	}
	continuationSingleCopy(t, requestA, materialA, "the first continuation request")
	continuationSingleCopy(t, requestB, materialB, "the second continuation request")
	continuationSingleCopy(t, requestB, materialA, "the second continuation request history")
	continuationHistory(t, requestA, inputA, inputB)
	continuationHistory(t, requestB, inputA, inputB)

	facts := host.Run().SessionFacts(addr)
	followFactA := continuationNoticeFact(t, facts, settleA.TaskID)
	followFactB := continuationNoticeFact(t, facts, settleB.TaskID)
	if followFactA.TaskID == followFactB.TaskID {
		t.Fatalf("the two continuations report task %s, want one task each", followFactA.TaskID)
	}
	if followFactA.InvocationID == followFactB.InvocationID || followFactA.Key == followFactB.Key {
		t.Fatalf("the two notices were merged into range %s or fact %s, want one range and one fact each",
			followFactA.InvocationID, followFactA.Key)
	}
	if want := agent.NoticeFactKey(followFactA.InvocationID, noticeA); followFactA.Key != want {
		t.Fatalf("the first continuation fact key = %s, want the notice identity key %s", followFactA.Key, want)
	}
	if want := agent.NoticeFactKey(followFactB.InvocationID, noticeB); followFactB.Key != want {
		t.Fatalf("the second continuation fact key = %s, want the notice identity key %s", followFactB.Key, want)
	}
	continuationRequireAction(t, sender, acceptedA, addr)
	continuationRequireAction(t, sender, acceptedB, addr)
	continuationRequireAction(t, sender, followA, addr)
	continuationRequireAction(t, sender, followB, addr)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the two-task continuation script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runMultiSessionContinuations drives one delegated task per session on the
// same host: every notice must continue in the session that owns it, and no
// material or fact may cross into the other session's chain.
func runMultiSessionContinuations(t *testing.T) {
	const (
		inputA     = "delegate the first isolated task"
		inputB     = "delegate the second isolated task"
		workerInA  = "compute the first isolated total"
		workerInB  = "compute the second isolated total"
		workerOutA = "the first isolated worker settled on 41"
		workerOutB = "the second isolated worker settled on 42"
		acceptedA  = "the first isolated task was accepted"
		acceptedB  = "the second isolated task was accepted"
		followA    = "the first isolated notice reached its user"
		followB    = "the second isolated notice reached its user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the first isolated task", inputA, "call-continuation-session-1", workerInA, acceptedA)
	expectContinuationDelegation(script, "the second isolated task", inputB, "call-continuation-session-2", workerInB, acceptedB)
	workerA := expectContinuationWorker(script, "the first isolated task", workerInA, workerOutA).park()
	workerB := expectContinuationWorker(script, "the second isolated task", workerInB, workerOutB).park()
	expectContinuationFollowUp(script, "first isolated", workerOutA, followA)
	expectContinuationFollowUp(script, "second isolated", workerOutB, followB)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addrA := event.Address{AgentID: "user", SessionID: "session-continuation-first"}
	addrB := event.Address{AgentID: "user", SessionID: "session-continuation-second"}

	driveContinuationTurn(t, ctx, host, addrA, inputA, 2)
	script.await(t, workerA)
	driveContinuationTurn(t, ctx, host, addrB, inputB, 2)
	script.await(t, workerB)

	script.release(workerA)
	settleA, noticeA, materialA := continuationSettle(t, host, addrA, workerOutA)
	requestA, _ := continuationWaitForRequest(t, provider, materialA, 30*time.Second)
	reproductionWaitForBody(t, sender, followA, 30*time.Second)

	script.release(workerB)
	settleB, noticeB, materialB := continuationSettle(t, host, addrB, workerOutB)
	requestB, _ := continuationWaitForRequest(t, provider, materialB, 30*time.Second)
	reproductionWaitForBody(t, sender, followB, 30*time.Second)

	continuationSingleCopy(t, requestA, materialA, "the first session continuation request")
	continuationSingleCopy(t, requestB, materialB, "the second session continuation request")
	continuationHistory(t, requestA, inputA)
	continuationHistory(t, requestB, inputB)
	continuationAbsent(t, requestA, inputB, "the first session continuation request")
	continuationAbsent(t, requestB, inputA, "the second session continuation request")
	continuationAbsent(t, requestA, materialB, "the first session continuation request")
	continuationAbsent(t, requestB, materialA, "the second session continuation request")

	factsA := host.Run().SessionFacts(addrA)
	factsB := host.Run().SessionFacts(addrB)
	if got := reproductionSettleFacts(factsA, workerOutB); len(got) != 0 {
		t.Fatalf("the first session chain carried the second session's settle fact: %s",
			reproductionFactsSummary(got))
	}
	if got := reproductionSettleFacts(factsB, workerOutA); len(got) != 0 {
		t.Fatalf("the second session chain carried the first session's settle fact: %s",
			reproductionFactsSummary(got))
	}
	followFactA := continuationNoticeFact(t, factsA, settleA.TaskID)
	followFactB := continuationNoticeFact(t, factsB, settleB.TaskID)
	if followFactA.Owner != addrA || followFactB.Owner != addrB {
		t.Fatalf("the follow-up facts are owned by %+v and %+v, want %+v and %+v",
			followFactA.Owner, followFactB.Owner, addrA, addrB)
	}
	if followFactA.InvocationID == followFactB.InvocationID {
		t.Fatalf("the two sessions share the follow-up range %s, want one range each", followFactA.InvocationID)
	}
	if want := agent.NoticeFactKey(followFactA.InvocationID, noticeA); followFactA.Key != want {
		t.Fatalf("the first session follow-up fact key = %s, want the notice identity key %s", followFactA.Key, want)
	}
	if want := agent.NoticeFactKey(followFactB.InvocationID, noticeB); followFactB.Key != want {
		t.Fatalf("the second session follow-up fact key = %s, want the notice identity key %s", followFactB.Key, want)
	}
	continuationRequireAction(t, sender, acceptedA, addrA)
	continuationRequireAction(t, sender, acceptedB, addrB)
	continuationRequireAction(t, sender, followA, addrA)
	continuationRequireAction(t, sender, followB, addrB)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the multi-session continuation script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runLateNoticeContinuation settles a second task only after the first
// notice-driven follow-up already ended and the driver rested: the late notice
// must still drive its own next decision instead of being lost with the window
// that observed it.
func runLateNoticeContinuation(t *testing.T) {
	const (
		inputA     = "delegate the early continuation task"
		inputB     = "delegate the late continuation task"
		workerInA  = "compute the early continuation total"
		workerInB  = "compute the late continuation total"
		workerOutA = "the early continuation worker settled on 41"
		workerOutB = "the late continuation worker settled on 42"
		acceptedA  = "the early continuation task was accepted"
		acceptedB  = "the late continuation task was accepted"
		followA    = "the early continuation notice reached the user"
		followB    = "the late continuation notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the early task", inputA, "call-continuation-late-1", workerInA, acceptedA)
	expectContinuationDelegation(script, "the late task", inputB, "call-continuation-late-2", workerInB, acceptedB)
	workerA := expectContinuationWorker(script, "the early task", workerInA, workerOutA).park()
	workerB := expectContinuationWorker(script, "the late task", workerInB, workerOutB).park()
	expectContinuationFollowUp(script, "early continuation", workerOutA, followA)
	expectContinuationFollowUp(script, "late continuation", workerOutB, followB)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-continuation-late"}

	driveContinuationTurn(t, ctx, host, addr, inputA, 2)
	script.await(t, workerA)
	driveContinuationTurn(t, ctx, host, addr, inputB, 2)
	script.await(t, workerB)

	script.release(workerA)
	settleA, _, materialA := continuationSettle(t, host, addr, workerOutA)
	requestA, indexA := continuationWaitForRequest(t, provider, materialA, 30*time.Second)
	reproductionWaitForBody(t, sender, followA, 30*time.Second)

	if got := reproductionSettleFacts(host.Run().SessionFacts(addr), workerOutB); len(got) != 0 {
		t.Fatalf("the late task settled before its model turn was released: %s", reproductionFactsSummary(got))
	}
	continuationQuiet(t, provider, 250*time.Millisecond)

	script.release(workerB)
	settleB, noticeB, materialB := continuationSettle(t, host, addr, workerOutB)
	requestB, indexB := continuationWaitForRequest(t, provider, materialB, 30*time.Second)
	reproductionWaitForBody(t, sender, followB, 30*time.Second)

	if indexB <= indexA {
		t.Fatalf("the late continuation request index = %d, want it after the early continuation", indexB)
	}
	continuationSingleCopy(t, requestA, materialA, "the early continuation request")
	continuationSingleCopy(t, requestB, materialB, "the late continuation request")
	continuationSingleCopy(t, requestB, materialA, "the late continuation request history")
	continuationHistory(t, requestB, inputA, inputB)

	facts := host.Run().SessionFacts(addr)
	followFactA := continuationNoticeFact(t, facts, settleA.TaskID)
	followFactB := continuationNoticeFact(t, facts, settleB.TaskID)
	if followFactA.InvocationID == followFactB.InvocationID {
		t.Fatalf("the late notice reused the early continuation range %s, want a range of its own", followFactA.InvocationID)
	}
	if want := agent.NoticeFactKey(followFactB.InvocationID, noticeB); followFactB.Key != want {
		t.Fatalf("the late continuation fact key = %s, want the notice identity key %s", followFactB.Key, want)
	}
	continuationRequireAction(t, sender, acceptedA, addr)
	continuationRequireAction(t, sender, acceptedB, addr)
	continuationRequireAction(t, sender, followA, addr)
	continuationRequireAction(t, sender, followB, addr)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the late continuation script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runNoDuplicateNoticeInput judges the duplicate anchor by fact identity instead
// of by body: the material of one notice identity must enter the chain as
// exactly one new input fact - the feedback fact the follow-up range reads at
// its head - and no external input fact of the same material may be committed
// beside it for the same range.
func runNoDuplicateNoticeInput(t *testing.T) {
	const (
		input       = "delegate the single-material task"
		workerInput = "compute the single-material total"
		workerOut   = "the single-material worker reports 42"
		accepted    = "the single-material task was accepted"
		follow      = "the single-material notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the single-material task", input, "call-continuation-single", workerInput, accepted)
	worker := expectContinuationWorker(script, "the single-material task", workerInput, workerOut)
	expectContinuationFollowUp(script, "single-material", workerOut, follow)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-continuation-single-material"}

	driveContinuationTurn(t, ctx, host, addr, input, 2)
	script.await(t, worker)
	script.release(worker)

	settle, notice, material := continuationSettle(t, host, addr, workerOut)
	request, batched := continuationNoticeAnswer(t, provider, sender, material, "call-continuation-single", follow, accepted)
	facts := host.Run().SessionFacts(addr)

	inputs := reproductionInputConsumers(reproductionInputFacts(facts, material))
	if len(inputs) != 1 {
		t.Fatalf("the notice identity %s generated %d new user-input facts carrying %q, want exactly 1: %s",
			notice.FactKey(), len(inputs), material, reproductionFactsSummary(inputs))
	}
	entry := inputs[0]
	if entry.Type != event.TypeFeedback {
		t.Fatalf("the single notice input entered as a %s fact, want the feedback fact the range reads at its head: %s",
			entry.Type, reproductionFactSummary(entry))
	}
	if got := entry.Meta[event.MetaKeyTriggerSource]; got != "task" {
		t.Fatalf("the notice input carries trigger source %q, want %q", got, "task")
	}
	if entry.TaskID != settle.TaskID || entry.Meta[event.MetaKeyTaskID] != string(settle.TaskID) {
		t.Fatalf("the notice input carries task %q/%q, want %q",
			entry.TaskID, entry.Meta[event.MetaKeyTaskID], settle.TaskID)
	}
	if got := entry.Meta[event.MetaKeyNoticeKind]; got != string(notice.Kind) {
		t.Fatalf("the notice input carries notice kind %q, want %q", got, notice.Kind)
	}
	if want := agent.NoticeFactKey(entry.InvocationID, notice); entry.Key != want {
		t.Fatalf("the notice input key = %s, want the notice identity key %s", entry.Key, want)
	}
	external := 0
	for _, fact := range facts {
		if fact.Type != event.TypeExternalInput || fact.InvocationID != entry.InvocationID {
			continue
		}
		if strings.TrimSpace(fact.Message.Content) == material {
			external++
		}
	}
	if external != 0 {
		t.Fatalf("the follow-up range %s carried %d external input facts of the notice material beside its notice fact, want none: %s",
			entry.InvocationID, external, reproductionFactsSummary(facts))
	}
	continuationSingleCopy(t, request, material, "the follow-up request")
	if carrying := reproductionRequestsCarrying(provider.servedRequests(), material); len(carrying) != 1 {
		t.Fatalf("requests carrying the notice material = %v, want the one follow-up decision", carrying)
	}
	continuationAnswerDelivered(t, sender, addr, accepted, follow, batched)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the single-material script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runBusyNoticeNotDropped settles a second task while the first follow-up is
// parked in flight, and requires the busy notice to be consumed by a later
// decision: before the parked turn ends the notice exists only as its delivery
// fact, and after that turn ends its own range commits and reads it.
func runBusyNoticeNotDropped(t *testing.T) {
	const (
		inputA     = "start the first retained-notice task"
		inputB     = "start the second retained-notice task"
		workerInA  = "compute the first retained-notice total"
		workerInB  = "compute the second retained-notice total"
		workerOutA = "the first retained-notice worker settled on 41"
		workerOutB = "the second retained-notice worker settled on 42"
		acceptedA  = "the first retained-notice task was accepted"
		acceptedB  = "the second retained-notice task was accepted"
		followA    = "the first retained-notice notice reached the user"
		followB    = "the second retained-notice notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the first retained-notice task", inputA, "call-continuation-drop-1", workerInA, acceptedA)
	expectContinuationDelegation(script, "the second retained-notice task", inputB, "call-continuation-drop-2", workerInB, acceptedB)
	workerA := expectContinuationWorker(script, "the first retained-notice task", workerInA, workerOutA).park()
	workerB := expectContinuationWorker(script, "the second retained-notice task", workerInB, workerOutB).park()
	followStepA := expectContinuationFollowUp(script, "first retained-notice", workerOutA, followA).park()
	expectContinuationFollowUp(script, "second retained-notice", workerOutB, followB)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-continuation-drop"}

	driveContinuationTurn(t, ctx, host, addr, inputA, 2)
	script.await(t, workerA)
	driveContinuationTurn(t, ctx, host, addr, inputB, 2)
	script.await(t, workerB)

	script.release(workerA)
	script.await(t, followStepA)
	_, _, materialA := continuationSettle(t, host, addr, workerOutA)
	requestA, indexA := continuationWaitForRequest(t, provider, materialA, 30*time.Second)

	script.release(workerB)
	settleB, noticeB, materialB := continuationSettle(t, host, addr, workerOutB)
	facts := host.Run().SessionFacts(addr)
	if got := continuationNoticeFactCount(facts, settleB.TaskID); got != 0 {
		t.Fatalf("the busy notice committed %d feedback facts before its turn fired, want the delivery retained in the driver: %s",
			got, reproductionFactsSummary(facts))
	}
	if carrying := reproductionRequestsCarrying(provider.servedRequests(), materialB); len(carrying) != 0 {
		t.Fatalf("the busy notice opened a decision beside the turn in flight: requests %v", carrying)
	}

	script.release(followStepA)
	reproductionWaitForBody(t, sender, followA, 30*time.Second)
	requestB, indexB := continuationWaitForRequest(t, provider, materialB, 30*time.Second)
	reproductionWaitForBody(t, sender, followB, 30*time.Second)
	if indexB <= indexA {
		t.Fatalf("the retained notice request index = %d, want it after the in-flight follow-up %d", indexB, indexA)
	}
	continuationSingleCopy(t, requestA, materialA, "the in-flight follow-up request")
	continuationSingleCopy(t, requestB, materialB, "the retained notice request")

	facts = host.Run().SessionFacts(addr)
	followFactB := continuationNoticeFact(t, facts, settleB.TaskID)
	if want := agent.NoticeFactKey(followFactB.InvocationID, noticeB); followFactB.Key != want {
		t.Fatalf("the retained notice fact key = %s, want the notice identity key %s", followFactB.Key, want)
	}
	if got := followFactB.Meta[event.MetaKeyTaskID]; got != string(settleB.TaskID) {
		t.Fatalf("the retained notice fact carries task key %q, want %q", got, settleB.TaskID)
	}
	continuationRequireAction(t, sender, acceptedA, addr)
	continuationRequireAction(t, sender, acceptedB, addr)
	continuationRequireAction(t, sender, followA, addr)
	continuationRequireAction(t, sender, followB, addr)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the retained-notice script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runNoNewInvocationForNotice judges the identity anchor in the narrowed sense of
// the frozen contract: the follow-up stays in the responsibility scope of the
// original session, carries the original task causality, and neither merges the
// user range into its own nor the notice into the user range. The literal reuse
// of the original invocation identity is not asserted.
func runNoNewInvocationForNotice(t *testing.T) {
	const (
		input       = "delegate the responsibility-scope task"
		workerInput = "compute the responsibility-scope total"
		workerOut   = "the responsibility-scope worker reports 42"
		accepted    = "the responsibility-scope task was accepted"
		follow      = "the responsibility-scope notice reached the user"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the responsibility-scope task", input, "call-continuation-scope", workerInput, accepted)
	worker := expectContinuationWorker(script, "the responsibility-scope task", workerInput, workerOut)
	expectContinuationFollowUp(script, "responsibility-scope", workerOut, follow)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-continuation-scope"}

	driveContinuationTurn(t, ctx, host, addr, input, 2)
	script.await(t, worker)
	script.release(worker)

	settle, notice, material := continuationSettle(t, host, addr, workerOut)
	request, batched := continuationNoticeAnswer(t, provider, sender, material, "call-continuation-scope", follow, accepted)
	facts := host.Run().SessionFacts(addr)
	followFact := continuationNoticeFact(t, facts, settle.TaskID)
	userInvocation := continuationUserInvocation(t, facts, input)

	if followFact.Owner.AgentID != addr.AgentID || followFact.Owner.SessionID != addr.SessionID {
		t.Fatalf("the follow-up fact owner = %+v, want the user address %+v: the continuation left the responsibility scope of its session",
			followFact.Owner, addr)
	}
	if followFact.InvocationID == "" || followFact.InvocationID == userInvocation {
		t.Fatalf("the follow-up fact invocation = %q, want a range of its own beside the user range %q",
			followFact.InvocationID, userInvocation)
	}
	if want := agent.NoticeFactKey(followFact.InvocationID, notice); followFact.Key != want {
		t.Fatalf("the follow-up fact key = %s, want the notice identity key %s", followFact.Key, want)
	}
	if followFact.TaskID != settle.TaskID || followFact.Meta[event.MetaKeyTaskID] != string(settle.TaskID) {
		t.Fatalf("the follow-up fact carries task %q/%q, want the original task %q",
			followFact.TaskID, followFact.Meta[event.MetaKeyTaskID], settle.TaskID)
	}
	if got := followFact.Meta[event.MetaKeyEventSummary]; got != string(notice.Kind) {
		t.Fatalf("the follow-up fact carries event summary %q, want the notice kind %q", got, notice.Kind)
	}
	if got := reproductionNoticeKeyCount(facts, agent.NoticeFactKey(userInvocation, notice)); got != 0 {
		t.Fatalf("the chain carried %d notice facts under the user range %s, want the notice in a range of its own",
			got, userInvocation)
	}
	for _, fact := range facts {
		if fact.Type != event.TypeExternalInput || fact.InvocationID != followFact.InvocationID {
			continue
		}
		if strings.TrimSpace(fact.Message.Content) == input {
			t.Fatalf("the user input was folded into the follow-up range %s, want the ranges kept apart: %s",
				followFact.InvocationID, reproductionFactSummary(fact))
		}
	}
	continuationSingleCopy(t, request, material, "the follow-up request")
	continuationAnswerDelivered(t, sender, addr, accepted, follow, batched)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the responsibility-scope script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// TestEventIntegrityDelivery drives the delivery anchors: the send tool delivers through the host sender, and answered actions keep their call identity.
// - Every legal action is delivered on its own identity; a confirmed action is never sent a second time on reentry or on a later turn.
// - An unknown result keeps its action, target and error unconfirmed and is neither retried automatically nor reported as a success.
// - An explicit retry stays linked to the action it repeats; distinct actions are never merged.
// - The send tool is admitted through the formal task wrapper: its outcome is recorded under the decision's call identity.
// - That outcome backflows as the observation the next decision reads.
// - The confirmation state is read at the host's outbound port: the sender is the delivery face every body is handed to.
// - The checks claim no channel-side idempotency the deployment has no evidence for.
func TestEventIntegrityDelivery(t *testing.T) {
	t.Run("all_legal_actions_delivered", runAllLegalActionsDelivered)
	t.Run("confirmed_action_reentry_no_resend", runConfirmedActionReentryNoResend)
	t.Run("unknown_result_retained", runUnknownResultRetained)
	t.Run("explicit_retry_keeps_link", runExplicitRetryKeepsLink)
	t.Run("confirmed_action_not_resent", runConfirmedActionNotResent)
	t.Run("unknown_result_not_auto_resent", runUnknownResultNotAutoResent)
	t.Run("distinct_actions_not_merged", runDistinctActionsNotMerged)
}

// deliveryAddr is the explicit user address every delivery check drives.
var deliveryAddr = event.Address{AgentID: "user", SessionID: "session-event-integrity-delivery"}

// errDeliveryOutcomeUnknown is the ambiguous failure the controlled sender
// answers: the body may or may not have reached the channel, so the action is
// neither confirmed nor safely retried by the host.
var errDeliveryOutcomeUnknown = errors.New("tagent test: the channel lost the delivery acknowledgement")

// deliverySender is the controlled outbound face of the delivery checks: it
// records every action beside the call frame the call carried and answers it
// with the result the check scripts.
type deliverySender struct {
	mu       sync.Mutex
	recorded []deliveryAction
	answer   func(index int, action deliveryAction) error
}

// deliveryAction is one outbound action as the controlled sender read it: the
// address, the body, the call frame a declared tool call carried (absent for a
// turn answer) and the result the sender answered.
type deliveryAction struct {
	target event.Address
	body   string
	frame  task.CallFrame
	framed bool
	result error
}

// confirmed reports whether the sender answered the action without a refusal;
// a non-nil result is the unconfirmed state the action stayed in.
func (a deliveryAction) confirmed() bool { return a.result == nil }

// newDeliverySender returns the controlled sender beside the answer a check
// scripts; a nil answer accepts every action.
func newDeliverySender(answer func(index int, action deliveryAction) error) *deliverySender {
	return &deliverySender{answer: answer}
}

// Send records one action and answers it with the scripted result.
func (s *deliverySender) Send(ctx context.Context, target event.Address, body string) error {
	action := deliveryAction{target: target, body: body}
	action.frame, action.framed = task.CallFrameFrom(ctx)
	s.mu.Lock()
	index := len(s.recorded)
	s.recorded = append(s.recorded, action)
	answer := s.answer
	s.mu.Unlock()
	var result error
	if answer != nil {
		result = answer(index, action)
	}
	s.mu.Lock()
	s.recorded[index].result = result
	s.mu.Unlock()
	return result
}

// actions reports the actions the sender answered in delivery order, as a copy
// the caller owns.
func (s *deliverySender) actions() []deliveryAction {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]deliveryAction(nil), s.recorded...)
}

// bodies reports the recorded bodies in delivery order.
func (s *deliverySender) bodies() []string {
	actions := s.actions()
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		out = append(out, action.body)
	}
	return out
}

// bootDeliveryHost boots the entry document the delivery checks drive: the
// entry agent beside the declared send tool, resolved at the scripted provider
// and attached to the controlled sender through the formal option.
func bootDeliveryHost(t *testing.T, provider *scriptedProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-entry",
		"    tools:\n"+
			"      - { kind: tool, id: send }\n"))
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the delivery host: %v", err)
	}
	return host
}

// deliveryActionOf reports the one action carrying the body, failing beside the
// count when the body was delivered zero times or more than once.
func deliveryActionOf(t *testing.T, sender *deliverySender, body string) deliveryAction {
	t.Helper()
	var matched []deliveryAction
	for _, action := range sender.actions() {
		if action.body == body {
			matched = append(matched, action)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("delivered actions carrying %q = %d, want exactly 1; recorded bodies = %q",
			body, len(matched), sender.bodies())
	}
	return matched[0]
}

// deliveryUnframedAction reports the one action a turn answer produced: an
// action delivered beside no call frame.
func deliveryUnframedAction(t *testing.T, sender *deliverySender) deliveryAction {
	t.Helper()
	var matched []deliveryAction
	for _, action := range sender.actions() {
		if !action.framed {
			matched = append(matched, action)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("turn-answer actions beside no call frame = %d, want exactly 1; recorded actions = %+v",
			len(matched), sender.actions())
	}
	return matched[0]
}

// deliveryAssistantFacts reports the committed assistant-output facts of the
// session that carry the body: the source identities a turn answer was
// delivered from.
func deliveryAssistantFacts(host *assembly.Host, addr event.Address, body string) []event.Event {
	body = strings.TrimSpace(body)
	var matched []event.Event
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type != event.TypeAgentOutput || len(fact.Message.ToolCalls) > 0 {
			continue
		}
		if strings.TrimSpace(fact.Message.Content) == body {
			matched = append(matched, fact)
		}
	}
	return matched
}

// deliveryAssistantFact reports the one committed assistant-output fact of the
// session carrying the body.
func deliveryAssistantFact(t *testing.T, host *assembly.Host, addr event.Address, body string) event.Event {
	t.Helper()
	matched := deliveryAssistantFacts(host, addr, body)
	if len(matched) != 1 {
		t.Fatalf("committed assistant-output facts carrying %q = %d, want exactly 1", body, len(matched))
	}
	return matched[0]
}

// deliveryToolResult reports the result text one model request carried for the
// call, the surface the caller of a send action reads. A wrapped call reaches
// the model as its admission receipt under the call id beside the observation
// the settled task backflows as the user material that follows it; the
// observation carries no call id on the wire, so the check reads the user
// message after the receipt instead of the receipt itself.
func deliveryToolResult(request scriptedRequest, callID string) (string, bool) {
	for index, message := range request.Messages {
		if message.ToolCallID != callID {
			continue
		}
		for _, later := range request.Messages[index+1:] {
			if later.Role == string(model.RoleUser) {
				return later.Content, true
			}
		}
		return message.Content, true
	}
	return "", false
}

// deliveryRequireAddress requires the action to be addressed to the user
// address.
func deliveryRequireAddress(t *testing.T, action deliveryAction, addr event.Address) {
	t.Helper()
	if action.target != addr {
		t.Fatalf("the action %q was addressed to %+v, want %+v", action.body, action.target, addr)
	}
}

// deliveryRequireConfirmed requires the sender to have answered the action
// without a refusal.
func deliveryRequireConfirmed(t *testing.T, action deliveryAction) {
	t.Helper()
	if !action.confirmed() {
		t.Fatalf("the action %q = %+v, want it confirmed by the sender", action.body, action)
	}
}

// deliveryBodyCount reports how many recorded actions carried the body.
func deliveryBodyCount(actions []deliveryAction, body string) int {
	count := 0
	for _, action := range actions {
		if action.body == body {
			count++
		}
	}
	return count
}

// runAllLegalActionsDelivered requires the one outbound action a turn has to
// reach the sender exactly once on the identity its own source carries: the
// declared send tool answers its caller with a receipt and reaches no channel,
// so the turn answer stays the only delivered body, delivered from the
// committed output fact of the turn's range beside no call frame.
func runAllLegalActionsDelivered(t *testing.T) {
	reproductionEnv(t)
	const (
		input  = "deliver the explicit action beside the answer"
		note   = "the explicit delivery action"
		answer = "the final delivery answer"
		callID = "call-delivery-legal"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry sends its explicit action", requestTail(input)).
		answers(settleToolCallAnswer("send", callID, `{"content":"`+note+`"}`))
	script.expect("model-entry", "the entry ends on its final answer", requestCarriesToolResult(callID)).
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
		t.Fatalf("the delivery turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("the delivery turn = (%v, %d decisions), want the completed turn of its two decisions",
			turn.Terminal, turn.DecisionCount)
	}

	requests := provider.servedRequests()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want the send decision beside the answer that read it", len(requests))
	}
	receipt, ok := sendRigReceipt(requests[1], callID)
	if !ok {
		t.Fatalf("the answer decision carried no tool result for %q: %+v", callID, requests[1].Messages)
	}
	if !strings.HasPrefix(receipt, sendRigDeniedPrefix) {
		t.Fatalf("the send receipt = %q, want the named denial a send naming no target reads, never a delivery", receipt)
	}

	finished := deliveryUnframedAction(t, sender)
	if finished.body != answer {
		t.Fatalf("the one delivered action carried %q, want the turn answer %q: the send reached no channel", finished.body, answer)
	}
	deliveryRequireAddress(t, finished, deliveryAddr)
	deliveryRequireConfirmed(t, finished)
	fact := deliveryAssistantFact(t, host, deliveryAddr, answer)
	if fact.InvocationID != turn.InvocationID {
		t.Fatalf("the turn answer fact invocation = %q, want the turn range %q", fact.InvocationID, turn.InvocationID)
	}
	if fact.Key == "" || string(fact.Key) == callID {
		t.Fatalf("the turn answer identity %q collides with the send call %q: the receipt must never take the answer's identity",
			fact.Key, callID)
	}

	if got := sender.bodies(); !reflect.DeepEqual(got, []string{answer}) {
		t.Fatalf("delivered bodies = %q, want the final answer alone: the declared send tool delivers nothing", got)
	}
	if got := len(sender.actions()); got != 1 {
		t.Fatalf("recorded actions = %d, want the turn answer alone", got)
	}
	if got := deliveryBodyCount(sender.actions(), note); got != 0 {
		t.Fatalf("the send body %q reached the channel %d times, want 0", note, got)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the delivery script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runConfirmedActionReentryNoResend requires a later turn on the same session
// to deliver only the new body of its own range: the confirmed bodies of the
// first turn stay delivered once and are not executed again when the delivery
// path reads the session facts a second time.
func runConfirmedActionReentryNoResend(t *testing.T) {
	reproductionEnv(t)
	const (
		firstInput   = "deliver the confirmed action of the first turn"
		firstNote    = "the confirmed explicit action"
		firstAnswer  = "the confirmed final answer"
		secondInput  = "answer the second turn"
		secondAnswer = "the second turn answer"
		callID       = "call-delivery-reentry"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the first turn sends its explicit action", requestTail(firstInput)).
		answers(settleToolCallAnswer("send", callID, `{"content":"`+firstNote+`"}`))
	script.expect("model-entry", "the first turn ends on its final answer", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(firstAnswer)).
		delivers(firstAnswer)
	script.expect("model-entry", "the second turn answers", requestTail(secondInput)).
		answers(scriptedAnswerOf(secondAnswer)).
		delivers(secondAnswer)
	provider.decide = script.decide

	sender := newDeliverySender(nil)
	host := bootDeliveryHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	first, err := driveIntegrityTurn(ctx, host, deliveryAddr, firstInput)
	if err != nil {
		t.Fatalf("the first delivery turn ended by error: %v", err)
	}
	if first.Terminal != model.KindCompleted || first.DecisionCount != 2 {
		t.Fatalf("the first turn = (%v, %d decisions), want the completed turn of its two decisions",
			first.Terminal, first.DecisionCount)
	}
	if got := sender.bodies(); !reflect.DeepEqual(got, []string{firstAnswer}) {
		t.Fatalf("delivered bodies of the first turn = %q, want the final answer alone: the send delivers nothing", got)
	}
	firstFact := deliveryAssistantFact(t, host, deliveryAddr, firstAnswer)

	second, err := driveIntegrityTurn(ctx, host, deliveryAddr, secondInput)
	if err != nil {
		t.Fatalf("the second delivery turn ended by error: %v", err)
	}
	if second.Terminal != model.KindCompleted || second.DecisionCount != 1 {
		t.Fatalf("the second turn = (%v, %d decisions), want the completed turn of its own decision",
			second.Terminal, second.DecisionCount)
	}
	if second.InvocationID == first.InvocationID {
		t.Fatalf("the second turn reused the invocation %q of the first turn", first.InvocationID)
	}
	secondAction := deliveryActionOf(t, sender, secondAnswer)
	deliveryRequireAddress(t, secondAction, deliveryAddr)
	deliveryRequireConfirmed(t, secondAction)
	if secondAction.framed {
		t.Fatalf("the second turn answer was delivered beside the call frame %+v; a turn answer carries no tool call identity", secondAction.frame)
	}
	secondFact := deliveryAssistantFact(t, host, deliveryAddr, secondAnswer)
	if secondFact.InvocationID != second.InvocationID {
		t.Fatalf("the second turn answer fact invocation = %q, want the turn range %q", secondFact.InvocationID, second.InvocationID)
	}

	if got := sender.bodies(); !reflect.DeepEqual(got, []string{firstAnswer, secondAnswer}) {
		t.Fatalf("delivered bodies after the reentry = %q, want only the new answer beside the confirmed answer of the first turn", got)
	}
	actions := sender.actions()
	if got := deliveryBodyCount(actions, firstAnswer); got != 1 {
		t.Fatalf("the confirmed turn answer %q was delivered %d times across the reentry, want exactly 1", firstAnswer, got)
	}
	if got := deliveryBodyCount(actions, firstNote); got != 0 {
		t.Fatalf("the send body %q reached the channel %d times across the reentry, want 0: a send delivers nothing", firstNote, got)
	}
	if firstFact.Key == secondFact.Key {
		t.Fatalf("the two turn answers share the fact identity %q", firstFact.Key)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the reentry script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runUnknownResultRetained requires an ambiguous delivery result to leave the
// body unconfirmed with its target and error, to stay owed in the delivery
// table instead of reading as a success, and to leave the turn completed
// beside the send receipt the caller corrects against.
func runUnknownResultRetained(t *testing.T) {
	reproductionEnv(t)
	const (
		input  = "send the action whose result is unknown"
		note   = "the action with the unknown result"
		answer = "the answer after the unknown send"
		callID = "call-delivery-unknown"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry sends the unknown-result action", requestTail(input)).
		answers(settleToolCallAnswer("send", callID, `{"content":"`+note+`"}`))
	script.expect("model-entry", "the entry answers beside the denial", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(answer)).
		delivers(answer)
	provider.decide = script.decide

	sender := newDeliverySender(func(index int, _ deliveryAction) error {
		if index == 0 {
			return errDeliveryOutcomeUnknown
		}
		return nil
	})
	host := bootDeliveryHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, input)
	if err != nil {
		t.Fatalf("the delivery turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("the delivery turn = (%v, %d decisions), want the completed turn that continued past the denial",
			turn.Terminal, turn.DecisionCount)
	}

	requests := provider.servedRequests()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want the send decision beside the answer that followed it", len(requests))
	}
	receipt, ok := sendRigReceipt(requests[1], callID)
	if !ok {
		t.Fatalf("the answer decision carried no tool result for %q: %+v", callID, requests[1].Messages)
	}
	if receipt == sendRigReceiptOK || !strings.HasPrefix(receipt, sendRigDeniedPrefix) {
		t.Fatalf("the caller read %q, want the named denial of a send naming no target, never the delivered ok", receipt)
	}

	actions := sender.actions()
	if len(actions) != 1 {
		t.Fatalf("recorded actions = %d, want the ambiguous turn answer alone: a send reaches no channel", len(actions))
	}
	if got := deliveryBodyCount(actions, note); got != 0 {
		t.Fatalf("the send body %q reached the channel %d times, want 0", note, got)
	}
	finished := actions[0]
	if finished.body != answer {
		t.Fatalf("the ambiguous action carried %q, want the turn answer %q", finished.body, answer)
	}
	deliveryRequireAddress(t, finished, deliveryAddr)
	if finished.confirmed() {
		t.Fatalf("the answer %q was recorded confirmed although the sender answered %v", answer, errDeliveryOutcomeUnknown)
	}
	if !errors.Is(finished.result, errDeliveryOutcomeUnknown) {
		t.Fatalf("the action error = %v, want the ambiguity %v retained beside the action", finished.result, errDeliveryOutcomeUnknown)
	}
	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("held deliveries = %+v, want the ambiguous answer owed as the one outstanding body", held)
	}
	if held[0].Delivery.Content != answer || held[0].Delivery.Target != deliveryAddr {
		t.Fatalf("the held delivery = %+v, want the ambiguous answer %q owed to %+v", held[0].Delivery, answer, deliveryAddr)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the unknown-result script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runExplicitRetryKeepsLink requires an explicit retry of a body whose delivery
// stayed unknown to deliver a new copy while the original unconfirmed record
// stays beside it: the approval re-delivers the same body to the same target,
// never overwrites the ambiguity that caused it, and consumes the hold so a
// repeated approval delivers nothing.
func runExplicitRetryKeepsLink(t *testing.T) {
	reproductionEnv(t)
	const (
		input  = "send the action that needs an explicit retry"
		note   = "the explicitly retried action"
		answer = "the answer after the explicit retry"
		callID = "call-delivery-retry"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry sends the action that fails", requestTail(input)).
		answers(settleToolCallAnswer("send", callID, `{"content":"`+note+`"}`))
	script.expect("model-entry", "the entry answers beside the denial", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(answer)).
		delivers(answer)
	provider.decide = script.decide

	sender := newDeliverySender(func(index int, _ deliveryAction) error {
		if index == 0 {
			return errDeliveryOutcomeUnknown
		}
		return nil
	})
	host := bootDeliveryHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, input)
	if err != nil {
		t.Fatalf("the delivery turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("the delivery turn = (%v, %d decisions), want the completed turn of the send and the answer",
			turn.Terminal, turn.DecisionCount)
	}

	requests := provider.servedRequests()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want the send decision beside the answer that read it", len(requests))
	}
	receipt, ok := sendRigReceipt(requests[1], callID)
	if !ok {
		t.Fatalf("the answer decision carried no tool result for %q: %+v", callID, requests[1].Messages)
	}
	if !strings.HasPrefix(receipt, sendRigDeniedPrefix) {
		t.Fatalf("the caller read %q for the send naming no target, want the named denial", receipt)
	}

	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("held deliveries before the retry = %+v, want the unconfirmed answer owed as the one body", held)
	}
	if held[0].Delivery.Content != answer || held[0].Delivery.Target != deliveryAddr {
		t.Fatalf("the held delivery before the retry = %+v, want the ambiguous answer %q owed to %+v",
			held[0].Delivery, answer, deliveryAddr)
	}
	before := sender.actions()
	if len(before) != 1 || before[0].confirmed() || !errors.Is(before[0].result, errDeliveryOutcomeUnknown) {
		t.Fatalf("recorded actions before the retry = %+v, want the ambiguous answer alone, unconfirmed", before)
	}

	if err := host.ApproveDelivery(ctx, held[0].ID); err != nil {
		t.Fatalf("the explicit retry of the held answer was refused: %v", err)
	}

	actions := sender.actions()
	if len(actions) != 2 {
		t.Fatalf("recorded actions after the retry = %d, want the ambiguous answer beside the retried copy", len(actions))
	}
	if got := deliveryBodyCount(actions, answer); got != 2 {
		t.Fatalf("actions carrying the retried body %q = %d, want the original beside the retry, not merged", answer, got)
	}
	original := actions[0]
	deliveryRequireAddress(t, original, deliveryAddr)
	if original.confirmed() || !errors.Is(original.result, errDeliveryOutcomeUnknown) {
		t.Fatalf("the original action = %+v, want it unconfirmed with the ambiguity %v retained", original, errDeliveryOutcomeUnknown)
	}
	repeated := actions[1]
	deliveryRequireAddress(t, repeated, deliveryAddr)
	deliveryRequireConfirmed(t, repeated)
	if repeated.body != answer {
		t.Fatalf("the retry carried %q, want the original body %q", repeated.body, answer)
	}
	if got := host.HeldDeliveries(); len(got) != 0 {
		t.Fatalf("held deliveries after the retry = %+v, want the one approval to consume the hold", got)
	}
	if err := host.ApproveDelivery(ctx, held[0].ID); !errors.Is(err, assembly.ErrDeliveryNotFound) {
		t.Fatalf("the repeated approval of %q = %v, want the named absence of the consumed hold", held[0].ID, err)
	}
	if got := deliveryBodyCount(sender.actions(), answer); got != 2 {
		t.Fatalf("the answer %q was delivered %d times after the repeated approval, want the original beside the one retry", answer, got)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the explicit-retry script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runConfirmedActionNotResent requires a confirmed turn answer to stay
// delivered once when a later turn of the same session answers the same body: a
// new turn is a new action identity beside a new committed fact, and it must
// not deliver the confirmed action again.
func runConfirmedActionNotResent(t *testing.T) {
	reproductionEnv(t)
	const (
		firstInput  = "deliver the confirmed answer of the first turn"
		answer      = "the answer delivered twice as two actions"
		secondInput = "answer the later turn with the same body"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the first turn answers", requestTail(firstInput)).
		answers(scriptedAnswerOf(answer)).
		delivers(answer)
	script.expect("model-entry", "the later turn answers the same body", requestTail(secondInput)).
		answers(scriptedAnswerOf(answer)).
		delivers(answer)
	provider.decide = script.decide

	sender := newDeliverySender(nil)
	host := bootDeliveryHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	first, err := driveIntegrityTurn(ctx, host, deliveryAddr, firstInput)
	if err != nil {
		t.Fatalf("the first delivery turn ended by error: %v", err)
	}
	if first.Terminal != model.KindCompleted || first.DecisionCount != 1 {
		t.Fatalf("the first turn = (%v, %d decisions), want the completed turn of its own decision",
			first.Terminal, first.DecisionCount)
	}
	before := sender.actions()
	if got := deliveryBodyCount(before, answer); got != 1 {
		t.Fatalf("the confirmed answer %q was delivered %d times in the first turn, want exactly 1", answer, got)
	}

	second, err := driveIntegrityTurn(ctx, host, deliveryAddr, secondInput)
	if err != nil {
		t.Fatalf("the later delivery turn ended by error: %v", err)
	}
	if second.Terminal != model.KindCompleted || second.DecisionCount != 1 {
		t.Fatalf("the later turn = (%v, %d decisions), want the completed turn of its own decision",
			second.Terminal, second.DecisionCount)
	}

	after := sender.actions()
	if got := deliveryBodyCount(after, answer); got != 2 {
		t.Fatalf("the repeated answer %q was delivered %d times across the two turns, want the confirmed action beside the one new action of the later turn, not a resend",
			answer, got)
	}
	if got := len(after); got != 2 {
		t.Fatalf("recorded actions = %d, want the first turn's answer beside the later turn's", got)
	}
	if !reflect.DeepEqual(after[:len(before)], before) {
		t.Fatalf("the later turn rewrote the confirmed actions %+v of the first turn", before)
	}
	facts := deliveryAssistantFacts(host, deliveryAddr, answer)
	if len(facts) != 2 {
		t.Fatalf("committed assistant-output facts carrying %q = %d, want the first turn's fact beside the later turn's", answer, len(facts))
	}
	identities := map[event.InvocationID]event.EventID{}
	for _, fact := range facts {
		if fact.InvocationID != first.InvocationID && fact.InvocationID != second.InvocationID {
			t.Fatalf("the answer fact belongs to invocation %q, want the first range %q or the later range %q",
				fact.InvocationID, first.InvocationID, second.InvocationID)
		}
		identities[fact.InvocationID] = fact.Key
	}
	if len(identities) != 2 || identities[first.InvocationID] == identities[second.InvocationID] {
		t.Fatalf("the two answer actions share the source identity %q; a later turn must be its own action", identities)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the confirmed-action script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runUnknownResultNotAutoResent requires an ambiguous delivery result to stay
// the one action the sender saw: the host neither retries it on its own nor
// reports a success, during the turn, across a quiet window, or while it drains.
func runUnknownResultNotAutoResent(t *testing.T) {
	reproductionEnv(t)
	const (
		input  = "send the action that must not be retried automatically"
		note   = "the action that must not be auto-retried"
		answer = "the answer beside the ambiguous send"
		callID = "call-delivery-no-auto-retry"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry sends the ambiguous action", requestTail(input)).
		answers(settleToolCallAnswer("send", callID, `{"content":"`+note+`"}`))
	script.expect("model-entry", "the entry answers beside the denial", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(answer)).
		delivers(answer)
	provider.decide = script.decide

	sender := newDeliverySender(func(index int, _ deliveryAction) error {
		if index == 0 {
			return errDeliveryOutcomeUnknown
		}
		return nil
	})
	host := bootDeliveryHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	turn, err := driveIntegrityTurn(ctx, host, deliveryAddr, input)
	if err != nil {
		t.Fatalf("the delivery turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("the delivery turn = (%v, %d decisions), want the completed turn of the send and the answer",
			turn.Terminal, turn.DecisionCount)
	}
	if got := deliveryBodyCount(sender.actions(), answer); got != 1 {
		t.Fatalf("the ambiguous answer %q was delivered %d times during the turn, want exactly 1", answer, got)
	}
	if got := deliveryBodyCount(sender.actions(), note); got != 0 {
		t.Fatalf("the send body %q reached the channel %d times during the turn, want 0", note, got)
	}
	receipt, ok := sendRigReceipt(provider.servedRequests()[1], callID)
	if !ok || !strings.HasPrefix(receipt, sendRigDeniedPrefix) || receipt == sendRigReceiptOK {
		t.Fatalf("the caller read %q for the send naming no target, want the named denial and never the delivered ok", receipt)
	}
	held := host.HeldDeliveries()
	if len(held) != 1 || held[0].Delivery.Content != answer {
		t.Fatalf("held deliveries = %+v, want the ambiguous answer owed as the one body", held)
	}

	time.Sleep(300 * time.Millisecond)
	if got := deliveryBodyCount(sender.actions(), answer); got != 1 {
		t.Fatalf("the ambiguous answer %q was delivered %d times after the quiet window, want exactly 1", answer, got)
	}

	closeIntegrityHost(t, ctx, host)
	actions := sender.actions()
	if got := deliveryBodyCount(actions, answer); got != 1 {
		t.Fatalf("the ambiguous answer %q was delivered %d times after the drain, want exactly 1", answer, got)
	}
	sent := actions[0]
	if sent.confirmed() {
		t.Fatalf("the drained action %q was recorded confirmed although its result stayed unknown", answer)
	}
	if !errors.Is(sent.result, errDeliveryOutcomeUnknown) {
		t.Fatalf("the drained action error = %v, want the ambiguity %v retained", sent.result, errDeliveryOutcomeUnknown)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the no-auto-retry script left violations: %v", violations)
	}
}

// runDistinctActionsNotMerged requires two bodies carrying the same text to stay
// two actions when the earlier delivery stayed unknown and the later one landed:
// the success of the later copy must not cover the unconfirmed state of the
// earlier one, the hold stays owed beside it, and the two source identities stay
// apart.
func runDistinctActionsNotMerged(t *testing.T) {
	reproductionEnv(t)
	const (
		input       = "send and answer the same body"
		body        = "the body delivered by two distinct actions"
		callID      = "call-delivery-distinct"
		secondInput = "answer the same body on a later turn"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry sends the shared body", requestTail(input)).
		answers(settleToolCallAnswer("send", callID, `{"content":"`+body+`"}`))
	script.expect("model-entry", "the entry answers the shared body", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(body)).
		delivers(body)
	script.expect("model-entry", "the later turn answers the shared body", requestTail(secondInput)).
		answers(scriptedAnswerOf(body)).
		delivers(body)
	provider.decide = script.decide

	sender := newDeliverySender(func(index int, _ deliveryAction) error {
		if index == 0 {
			return errDeliveryOutcomeUnknown
		}
		return nil
	})
	host := bootDeliveryHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	first, err := driveIntegrityTurn(ctx, host, deliveryAddr, input)
	if err != nil {
		t.Fatalf("the delivery turn ended by error: %v", err)
	}
	if first.Terminal != model.KindCompleted || first.DecisionCount != 2 {
		t.Fatalf("the delivery turn = (%v, %d decisions), want the completed turn of its two decisions",
			first.Terminal, first.DecisionCount)
	}
	if got := deliveryBodyCount(sender.actions(), body); got != 1 {
		t.Fatalf("the shared body %q was delivered %d times in the first turn, want the answer alone: a send reaches no channel", body, got)
	}
	receipt, ok := sendRigReceipt(provider.servedRequests()[1], callID)
	if !ok || !strings.HasPrefix(receipt, sendRigDeniedPrefix) {
		t.Fatalf("the caller read %q for the send naming no target, want the named denial", receipt)
	}

	second, err := driveIntegrityTurn(ctx, host, deliveryAddr, secondInput)
	if err != nil {
		t.Fatalf("the later delivery turn ended by error: %v", err)
	}
	if second.Terminal != model.KindCompleted || second.DecisionCount != 1 {
		t.Fatalf("the later turn = (%v, %d decisions), want the completed turn of its own decision",
			second.Terminal, second.DecisionCount)
	}

	actions := sender.actions()
	if len(actions) != 2 {
		t.Fatalf("recorded actions = %d, want the earlier answer beside the later one even though both carry the same body", len(actions))
	}
	if got := deliveryBodyCount(actions, body); got != 2 {
		t.Fatalf("actions carrying the shared body %q = %d, want the two distinct actions not merged by body", body, got)
	}
	earlier := actions[0]
	deliveryRequireAddress(t, earlier, deliveryAddr)
	if earlier.confirmed() || !errors.Is(earlier.result, errDeliveryOutcomeUnknown) {
		t.Fatalf("the earlier action = %+v, want it unconfirmed with the ambiguity %v", earlier, errDeliveryOutcomeUnknown)
	}
	later := actions[1]
	deliveryRequireAddress(t, later, deliveryAddr)
	deliveryRequireConfirmed(t, later)
	if earlier.body != body || later.body != body {
		t.Fatalf("the two actions carried %q and %q, want the shared body %q on both", earlier.body, later.body, body)
	}
	held := host.HeldDeliveries()
	if len(held) != 1 || held[0].Delivery.Content != body {
		t.Fatalf("held deliveries = %+v, want the earlier unknown copy still owed beside the later one that landed", held)
	}
	facts := deliveryAssistantFacts(host, deliveryAddr, body)
	if len(facts) != 2 {
		t.Fatalf("committed assistant-output facts carrying %q = %d, want the earlier turn's fact beside the later turn's", body, len(facts))
	}
	if facts[0].Key == "" || facts[0].Key == facts[1].Key {
		t.Fatalf("the two answers share the source identity %q: the later success must not merge into the earlier action", facts[0].Key)
	}
	if facts[0].InvocationID == facts[1].InvocationID {
		t.Fatalf("the two answers belong to the same invocation %q, want one range per turn", facts[0].InvocationID)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the distinct-action script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// deliveryRequestCarries reports the request that carries the content as one of
// its messages.
func deliveryRequestCarries(req model.Request, content string) bool {
	for _, message := range req.Messages {
		if message.Content == content {
			return true
		}
	}
	return false
}

// scriptedRequestCarries reports the request that carries the content as one of
// its messages.
func scriptedRequestCarries(request scriptedRequest, content string) bool {
	for _, message := range request.Messages {
		if message.Content == content {
			return true
		}
	}
	return false
}

// reflectionDeliveryHostDocument renders one host document whose meditation
// section observes the entry agent and delivers to the given white list.
func reflectionDeliveryHostDocument(root, endpoint, deliverTo string) string {
	var b strings.Builder
	b.WriteString("entry: user\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: model-entry\n")
	b.WriteString("prompt_dir: " + root + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  user: { max_tokens: 32000 }\n")
	b.WriteString("  meditator:\n")
	b.WriteString("    model: model-meditator\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: send }\n")
	b.WriteString("    memory: { type: localfile, path: " + root + ", read_namespaces: [user] }\n")
	b.WriteString("    meditation:\n")
	b.WriteString("      enabled: true\n")
	b.WriteString("      interval: 1h\n")
	b.WriteString("      min_gap: 50ms\n")
	b.WriteString("      prompt_file: meditation.md\n")
	b.WriteString("      observed_namespaces: [user]\n")
	b.WriteString("      deliver_to: " + deliverTo + "\n")
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// bootReflectionDeliveryHost writes and boots the reflection delivery document
// beside the given white list, answering the boot result so a refusal check
// reads the named refusal.
func bootReflectionDeliveryHost(t *testing.T, provider *scriptedProvider, deliverTo string) (*assembly.Host, error) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "tagent.yaml")
	writeHostDocumentFile(t, configPath, reflectionDeliveryHostDocument(root, provider.endpoint(), deliverTo))
	if err := os.WriteFile(filepath.Join(root, "meditation.md"), []byte("reflect on the user session"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
	if err != nil {
		return nil, err
	}
	return host, nil
}

// TestEventIntegrityReflectionWhiteList drives the boot-time white list and the one grant it opens.
// - empty_whitelist_fail_closed: a document enabling meditation beside no deliver_to target refuses the boot
// - unknown_target_named_refusal: a deliver_to naming an undeclared agent refuses the boot by name
// - whitelist_grants_send_and_target_reads_it: a live target reaches the reflection request as the send grant it opens
// - whitelist_grants_send_and_target_reads_it: one call lands exactly one delivery fact, which the target reads next turn
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestEventIntegrityReflectionWhiteList(t *testing.T) {
	reproductionEnv(t)
	t.Run("empty_whitelist_fail_closed", runEmptyWhitelistFailClosed)
	t.Run("unknown_target_named_refusal", runUnknownTargetNamedRefusal)
	t.Run("whitelist_grants_send_and_target_reads_it", runWhitelistGrantsSendAndTargetReadsIt)
}

// runEmptyWhitelistFailClosed requires a document that enables meditation
// beside an empty deliver_to to be refused at the boot: the delivery face is
// fail-closed, so an empty white list never serves a summary that reaches
// nobody.
func runEmptyWhitelistFailClosed(t *testing.T) {
	provider := newScriptedProvider(t)
	host, err := bootReflectionDeliveryHost(t, provider, "[]")
	if host != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = host.Close(closeCtx)
	}
	if err == nil {
		t.Fatal("meditation beside an empty deliver_to booted, want the fail-closed refusal")
	}
	if !strings.Contains(err.Error(), "names no deliver_to target") {
		t.Fatalf("the empty-whitelist refusal = %v, want the missing target named", err)
	}
}

// runUnknownTargetNamedRefusal requires a deliver_to naming an undeclared agent
// to be refused by name at the boot: the white list resolves against the
// declared agents, never against a name invented at delivery time.
func runUnknownTargetNamedRefusal(t *testing.T) {
	provider := newScriptedProvider(t)
	host, err := bootReflectionDeliveryHost(t, provider, "[nobody]")
	if host != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = host.Close(closeCtx)
	}
	if err == nil {
		t.Fatal("meditation delivering to an undeclared agent booted, want the named refusal")
	}
	if !strings.Contains(err.Error(), `undeclared agent "nobody"`) {
		t.Fatalf("the unknown-target refusal = %v, want the undeclared agent named", err)
	}
}

// runWhitelistGrantsSendAndTargetReadsIt requires a white list naming a live
// target to reach the reflection turn as the one cross-session grant it opens:
// the request declares the send tool that white list grants, one call lands
// exactly one delivery fact in the target chain under the send lineage, and the
// target's next request reads that body off the chain rather than beside a
// second delivery.
func runWhitelistGrantsSendAndTargetReadsIt(t *testing.T) {
	scene := newHostDriveScene(t)
	scene.bindPeer(t)
	scene.invokeMeditation(t, hostDriveSendCall, hostDrivePlainSend(hostDriveSendCall))

	if receipt := scene.awaitReceipt(t, hostDriveSendCall); receipt != hostDriveReceiptOK {
		t.Fatalf("send{target:{agent,session}} result beside the declared white list = %q, want the bare %q of a delivered body:\n%s",
			receipt, hostDriveReceiptOK, scene.describe())
	}
	declared := false
	for _, request := range scene.provider.servedRequests() {
		if request.Model != hostDriveMeditatorModel {
			continue
		}
		for _, tool := range request.Tools {
			if tool == hostDriveToolSend {
				declared = true
			}
		}
	}
	if !declared {
		t.Fatalf("no reflection request declared the %q tool the white list grants:\n%s",
			hostDriveToolSend, scene.describe())
	}

	scene.awaitPeerBody(t)
	facts := scene.bodyFacts(t)
	if len(facts) != 1 {
		t.Fatalf("delivered facts = %d, want the one body the call carried:\n%s", len(facts), scene.describe())
	}
	if facts[0].Type != event.TypeExternalInput {
		t.Fatalf("delivered fact type = %q, want the external input a delivery lands", facts[0].Type)
	}
	if got := facts[0].Meta[event.MetaKeyTriggerSource]; got != hostDriveLineage {
		t.Fatalf("delivered fact trigger source = %q, want the %q lineage of a body a peer sent", got, hostDriveLineage)
	}
	if facts[0].Target != scene.peer {
		t.Fatalf("delivered fact target = %+v, want the peer session %+v", facts[0].Target, scene.peer)
	}
	if rounds := scene.peerRoundsCarrying(t); len(rounds) != 1 {
		t.Fatalf("target rounds carrying the body = %d, want the one round the delivery drove:\n%s",
			len(rounds), scene.describe())
	}

	scene.drivePeerTurn(t)
	if rounds := scene.peerRoundsCarrying(t); len(rounds) != 2 {
		t.Fatalf("target rounds carrying the body after the next turn = %d, want the delivery round and the turn that read it:\n%s",
			len(rounds), scene.describe())
	}
	if again := scene.bodyFacts(t); len(again) != 1 {
		t.Fatalf("delivered facts beside the target's next turn = %d, want the single fact the one call landed:\n%s",
			len(again), scene.describe())
	}
	scene.refuteOutbound(t)
}

// TestEventIntegrityReplacement drives the event-driven model replacement of one agent's lease over the formal runtime and Boot entries:
// - new_request_gets_new_version: the model configuration change installs the new version and the next request is served by it
// - inflight_stream_keeps_start_version: a stream that began before the replacement keeps its start version to the end
// - duplicate_same_version_not_reinstalled: a same-version reload and a same-version model configuration change publish and install nothing, with a named result
// - failed_replacement_not_installed: a refused rebuild installs no version and leaves the version in effect serving
// - formal_reload_entry: POST /reload publishes the rewritten document and files one model configuration change
// - the next real request is served by the new version
// - failed_install_retried_by_next_reload: a model install the reload could not complete is judged again by the next carried reload
// - the install lands then, under the same fact identity
//
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestEventIntegrityReplacement(t *testing.T) {
	reproductionEnv(t)
	t.Run("new_request_gets_new_version", runNewRequestGetsNewVersion)
	t.Run("inflight_stream_keeps_start_version", runInflightStreamKeepsStartVersion)
	t.Run("duplicate_same_version_not_reinstalled", runDuplicateSameVersionNotReinstalled)
	t.Run("failed_replacement_not_installed", runFailedReplacementNotInstalled)
	t.Run("formal_reload_entry", runFormalReloadEntry)
	t.Run("failed_install_retried_by_next_reload", runFailedInstallRetriedByNextReload)
}

// replacementSession is the one session the replacement checks drive.
const replacementSession = event.SessionID("session-replacement-integrity")

// replacementVersionModel is one native model version of the replacement
// checks: every request it serves is recorded, and a version whose stream the
// check parks hands that stream over instead of answering at once.
type replacementVersionModel struct {
	mu       sync.Mutex
	name     string
	answer   func(*model.Request) *model.Response
	stream   chan *model.Response
	started  chan struct{}
	startOne sync.Once
	requests []model.Request
}

// newReplacementVersionModel returns one scripted version answering with the
// content it is named with.
func newReplacementVersionModel(name, content string) *replacementVersionModel {
	return &replacementVersionModel{
		name:   name,
		answer: func(*model.Request) *model.Response { return entryFinalTurn(content) },
	}
}

// GenerateContent records the request it serves and hands over the parked
// stream when the check named one, or the scripted answer otherwise.
func (m *replacementVersionModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	stream, answer, started := m.stream, m.answer, m.started
	m.mu.Unlock()
	if started != nil {
		m.startOne.Do(func() { close(started) })
	}
	if stream != nil {
		return stream, nil
	}
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted model's descriptor.
func (m *replacementVersionModel) Info() model.Info { return model.Info{Name: m.name} }

// recorded reports the requests this version was driven with.
func (m *replacementVersionModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// calls reports how many requests this version served.
func (m *replacementVersionModel) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// replacementPartial is one partial response a parked stream hands over before
// the replacement runs, so the stream is open while the lease moves.
func replacementPartial(content string) *model.Response {
	return &model.Response{
		Choices:   []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		IsPartial: true,
	}
}

// replacementRig is one assembled runtime carrying the event-driven model lease
// the replacement checks drive.
type replacementRig struct {
	run   *assembly.Run
	lease *rl.ModelLease
}

// newReplacementRig assembles one runtime over the initial model of the lease,
// the version the lease starts on and the rebuild a model configuration change runs.
func newReplacementRig(t *testing.T, initial model.Model, version string, rebuild func(string) (model.Model, error)) *replacementRig {
	t.Helper()
	lease, err := rl.NewModelLease(initial, version)
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	decl := &config.Declaration{Entry: "user", Agents: []config.AgentSpec{{
		ID:        "user",
		Model:     initial.Info().Name,
		MaxTokens: 32000,
	}}}
	run, err := assembly.Assemble(context.Background(), decl, map[event.AgentID]assembly.NativeAgent{
		"user": {Model: initial},
	}, assembly.WithGovernance(passPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}),
		assembly.WithModelLease("user", lease, assembly.WithLeaseRebuild(rebuild)),
	)
	if err != nil {
		t.Fatalf("assemble the replacement runtime: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if report, err := run.Close(ctx, agent.CloseRequest{Deadline: time.Now().Add(15 * time.Second)}); err != nil || !report.Drained {
			t.Logf("the replacement runtime ended its close beside: report=%+v err=%v", report, err)
		}
		_ = lease.Close()
	})
	return &replacementRig{run: run, lease: lease}
}

// replacementInvokeRequest builds one user turn of the replacement checks.
func replacementInvokeRequest(id, input string) assembly.InvokeRequest {
	return assembly.InvokeRequest{
		InvocationID:  event.InvocationID(id),
		Address:       event.Address{AgentID: "user", SessionID: replacementSession},
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: input}},
	}
}

// invokeReplacementTurn drives one user turn and requires it to return.
func invokeReplacementTurn(t *testing.T, run *assembly.Run, id, input string) agent.Turn {
	t.Helper()
	turn, err := run.Invoke(context.Background(), replacementInvokeRequest(id, input))
	if err != nil {
		t.Fatalf("invoke %s: %v", id, err)
	}
	return turn
}

// fileReplacementChange commits one model dimension configuration change on the
// rig's session chain and runs its After pass, the same fact a reload files.
// The change starts from the version the lease holds.
func fileReplacementChange(t *testing.T, rig *replacementRig, to string) event.EventID {
	t.Helper()
	fact := event.ConfigChangeModelFor("user", 0, rig.lease.CurrentVersion(), to, event.ModelReplacementResultRequested)
	key, err := rig.run.CommitToSession(context.Background(), event.Address{AgentID: "user", SessionID: replacementSession}, fact.Event(time.Now().UTC()))
	if err != nil {
		t.Fatalf("file the model configuration change: %v", err)
	}
	return key
}

// replacementGapOf reports the observation gaps the named plugin left on the
// run's After pass presentations, joined so a named refusal is read by errors.Is.
func replacementGapOf(run *assembly.Run, plugin string) error {
	var found error
	for _, presentation := range run.Presentations() {
		for _, gap := range presentation.Gaps {
			if gap.Plugin == plugin {
				found = errors.Join(found, gap.Err)
			}
		}
	}
	return found
}

// runNewRequestGetsNewVersion requires the model configuration change to
// install the new version and the next request to be served by it: the
// requests the two model versions recorded are the evidence, never the reload
// receipt alone.
func runNewRequestGetsNewVersion(t *testing.T) {
	first := newReplacementVersionModel("model-a", "the first version answered")
	second := newReplacementVersionModel("model-b", "the second version answered")
	var requested []string
	rig := newReplacementRig(t, first, "local/model-a", func(version string) (model.Model, error) {
		requested = append(requested, version)
		if version != "local/model-b" {
			return nil, fmt.Errorf("the replacement asked for %q", version)
		}
		return second, nil
	})

	if turn := invokeReplacementTurn(t, rig.run, "inv-replacement-first", "the first user round"); turn.Terminal != model.KindCompleted {
		t.Fatalf("the first turn terminal = %v, want the completed turn of the version in effect", turn.Terminal)
	}
	if first.calls() != 1 || second.calls() != 0 {
		t.Fatalf("model calls = (%d, %d), want the first request on the version in effect and none on the new one", first.calls(), second.calls())
	}

	fileReplacementChange(t, rig, "local/model-b")
	if len(requested) != 1 || requested[0] != "local/model-b" {
		t.Fatalf("rebuilt versions = %v, want exactly the one move the fact asked for", requested)
	}
	if got := rig.lease.CurrentVersion(); got != "model-b@local/model-b" {
		t.Fatalf("the lease holds %q, want the version the replacement installed", got)
	}

	if turn := invokeReplacementTurn(t, rig.run, "inv-replacement-second", "the second user round"); turn.Terminal != model.KindCompleted {
		t.Fatalf("the second turn terminal = %v, want the completed turn of the installed version", turn.Terminal)
	}
	if first.calls() != 1 || second.calls() != 1 {
		t.Fatalf("model calls = (%d, %d), want the next request served by the installed version alone", first.calls(), second.calls())
	}
	requests := second.recorded()
	if len(requests) != 1 || !deliveryRequestCarries(requests[0], "the second user round") {
		t.Fatalf("the new version requests = %+v, want the request that began after the replacement", requests)
	}
}

// runInflightStreamKeepsStartVersion requires a stream that began before the
// replacement to keep its start version to the end: the new version records no
// request while the stream is open and the next request alone reaches it.
func runInflightStreamKeepsStartVersion(t *testing.T) {
	first := newReplacementVersionModel("model-a", "unused")
	first.stream = make(chan *model.Response, 4)
	first.started = make(chan struct{})
	second := newReplacementVersionModel("model-b", "the new version answered")
	rig := newReplacementRig(t, first, "local/model-a", func(version string) (model.Model, error) {
		if version != "local/model-b" {
			return nil, fmt.Errorf("the replacement asked for %q", version)
		}
		return second, nil
	})

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := rig.run.Invoke(context.Background(), replacementInvokeRequest("inv-replacement-inflight", "the in-flight user round"))
		driven <- integrityOutcome{turn: turn, err: err}
	}()
	select {
	case <-first.started:
	case <-time.After(20 * time.Second):
		t.Fatal("the in-flight request never reached the version in effect")
	}

	fileReplacementChange(t, rig, "local/model-b")
	if got := rig.lease.CurrentVersion(); got != "model-b@local/model-b" {
		t.Fatalf("the lease holds %q, want the replacement installed beside the open stream", got)
	}
	if got := second.calls(); got != 0 {
		t.Fatalf("the new version served %d requests, want none while the stream that began before it is open", got)
	}

	first.stream <- replacementPartial("the in-flight stream began on the start version")
	first.stream <- entryFinalTurn("the in-flight stream kept its start version")
	close(first.stream)

	got := awaitIntegrityDrive(t, driven, "the in-flight replacement turn")
	if got.err != nil {
		t.Fatalf("the in-flight turn ended by error: %v", got.err)
	}
	if got.turn.Terminal != model.KindCompleted {
		t.Fatalf("the in-flight turn terminal = %v, want it completed on its start version", got.turn.Terminal)
	}
	if first.calls() != 1 || second.calls() != 0 {
		t.Fatalf("model calls = (%d, %d), want the whole stream served by the version it began on", first.calls(), second.calls())
	}

	if turn := invokeReplacementTurn(t, rig.run, "inv-replacement-after-inflight", "the after-replacement round"); turn.Terminal != model.KindCompleted {
		t.Fatalf("the after-replacement turn terminal = %v, want it completed", turn.Terminal)
	}
	if first.calls() != 1 || second.calls() != 1 {
		t.Fatalf("model calls = (%d, %d), want the next request served by the installed version", first.calls(), second.calls())
	}
	requests := second.recorded()
	if len(requests) != 1 || !deliveryRequestCarries(requests[0], "the after-replacement round") {
		t.Fatalf("the new version requests = %+v, want only the request that began after the stream ended", requests)
	}
}

// runDuplicateSameVersionNotReinstalled requires a same-version reload and a
// same-version model configuration change to publish and install nothing, with
// a determinable result: the host names no change, and the lease answers the
// named version-held refusal without a second rebuild or publication.
func runDuplicateSameVersionNotReinstalled(t *testing.T) {
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
	if err != nil {
		t.Fatalf("boot the replacement host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-reload-duplicate"}
	if turn, err := driveIntegrityTurn(ctx, host, addr, "the first reload round"); err != nil || turn.Terminal != model.KindCompleted {
		t.Fatalf("the first reload round = (%v, %v), want it completed on the boot model", turn.Terminal, err)
	}

	first, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the unchanged document: %v", err)
	}
	if !first.Reloaded || len(first.ModelChanges) != 0 {
		t.Fatalf("the unchanged reload = %+v, want the publication beside no judged model change", first)
	}
	second, err := host.Reload(ctx)
	if err != nil {
		t.Fatalf("reload the unchanged document again: %v", err)
	}
	if !second.Reloaded || len(second.ModelChanges) != 0 {
		t.Fatalf("the second unchanged reload = %+v, want no judged model change again", second)
	}
	if publications := host.Run().LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none beside a same-version reload", publications)
	}
	if turn, err := driveIntegrityTurn(ctx, host, addr, "the second reload round"); err != nil || turn.Terminal != model.KindCompleted {
		t.Fatalf("the second reload round = (%v, %v), want it completed on the same version", turn.Terminal, err)
	}
	models := provider.servedModels()
	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-a" {
		t.Fatalf("served models = %v, want both rounds served by the version the unchanged reload kept", models)
	}
	closeIntegrityHost(t, ctx, host)

	initial := newReplacementVersionModel("model-a", "the version in effect answered")
	versioned := newReplacementVersionModel("model-a", "the versioned replacement answered")
	rebuilds := 0
	rig := newReplacementRig(t, initial, "", func(version string) (model.Model, error) {
		rebuilds++
		if version != "local/model-a" {
			return nil, fmt.Errorf("the replacement asked for %q", version)
		}
		return versioned, nil
	})
	invokeReplacementTurn(t, rig.run, "inv-duplicate-before", "the round before the duplicate")
	fileReplacementChange(t, rig, "local/model-a")
	if rebuilds != 1 || rig.lease.CurrentVersion() != "model-a@local/model-a" {
		t.Fatalf("the first move rebuilt %d times and left %q, want the one versioning install", rebuilds, rig.lease.CurrentVersion())
	}
	fileReplacementChange(t, rig, "local/model-a")
	if rebuilds != 1 {
		t.Fatalf("the duplicate replacement rebuilt %d times, want no second rebuild", rebuilds)
	}
	if publications := rig.run.LeasePublications(); len(publications) != 1 {
		t.Fatalf("lease publications = %+v, want the one install and no second", publications)
	}
	if gapErr := replacementGapOf(rig.run, "model-lease"); !errors.Is(gapErr, rl.ErrLeaseVersionHeld) {
		t.Fatalf("the duplicate replacement left %v, want the named version-held refusal", gapErr)
	}
	invokeReplacementTurn(t, rig.run, "inv-duplicate-after", "the round after the duplicate")
	if initial.calls() != 1 || versioned.calls() != 1 {
		t.Fatalf("model calls = (%d, %d), want the duplicate to install nothing beside the versioned replacement", initial.calls(), versioned.calls())
	}
}

// runFailedReplacementNotInstalled requires a refused rebuild to install no
// version: the attempt is recorded as refused, nothing is published, the named
// gap carries the refusal, and the version in effect keeps serving requests.
func runFailedReplacementNotInstalled(t *testing.T) {
	first := newReplacementVersionModel("model-a", "the version in effect answered")
	cause := errors.New("the requested version could not be built")
	rebuilds := 0
	rig := newReplacementRig(t, first, "local/model-a", func(string) (model.Model, error) {
		rebuilds++
		return nil, cause
	})

	if turn := invokeReplacementTurn(t, rig.run, "inv-refused-first", "the first user round"); turn.Terminal != model.KindCompleted {
		t.Fatalf("the first turn terminal = %v, want the completed turn of the version in effect", turn.Terminal)
	}
	fileReplacementChange(t, rig, "local/model-b")
	if rebuilds != 1 {
		t.Fatalf("the refused replacement rebuilt %d times, want the one attempt the fact asked for", rebuilds)
	}
	if got := rig.lease.CurrentVersion(); got != "model-a@local/model-a" {
		t.Fatalf("the lease holds %q, want the version in effect untouched by the refused rebuild", got)
	}
	records := rig.lease.Replacements()
	if len(records) != 1 || records[0].State != rl.ReplacementRefused || !strings.Contains(records[0].Reason, cause.Error()) {
		t.Fatalf("replacement records = %+v, want the one refused attempt beside its cause", records)
	}
	if publications := rig.run.LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none beside a refused rebuild", publications)
	}
	if gapErr := replacementGapOf(rig.run, "model-lease"); !errors.Is(gapErr, rl.ErrLeaseRebuildFailed) {
		t.Fatalf("the refused replacement left %v, want the named rebuild refusal", gapErr)
	}

	if turn := invokeReplacementTurn(t, rig.run, "inv-refused-second", "the second user round"); turn.Terminal != model.KindCompleted {
		t.Fatalf("the second turn terminal = %v, want the version in effect to keep serving", turn.Terminal)
	}
	if first.calls() != 2 {
		t.Fatalf("the version in effect served %d requests, want it to serve after the refused replacement", first.calls())
	}
}

// runFormalReloadEntry drives POST /reload, the formal configuration entry, and
// requires the next real request to be served by the version the rewritten
// document names: the provider's recorded model is the evidence.
func runFormalReloadEntry(t *testing.T) {
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
	if err != nil {
		t.Fatalf("boot the reload host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-reload-entry"}
	if turn, err := driveIntegrityTurn(ctx, host, addr, "the first round"); err != nil || turn.Terminal != model.KindCompleted {
		t.Fatalf("the first round = (%v, %v), want it completed on the boot model", turn.Terminal, err)
	}

	before := host.Run().CommittedFacts()
	rewriteScriptedHostDocument(t, configPath, root, provider.endpoint(), "model-b")
	answer := postReload(t, host.FaceAddr())
	if !answer.Reloaded || len(answer.ModelChanges) != 1 {
		t.Fatalf("the reload answer = %+v, want the one judged move of the rewritten document", answer)
	}
	change := answer.ModelChanges[0]
	if change.Agent != "user" || change.From != "local/model-a" || change.To != "local/model-b" || change.Fact == "" {
		t.Fatalf("judged change = %+v, want the entry agent's move between the two registered versions beside its fact", change)
	}
	if after := host.Run().CommittedFacts(); after != before+1 {
		t.Fatalf("committed facts = %d after the reload, want exactly the one configuration change beside the %d before", after, before)
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed configuration change: %v", err)
	}
	if fact.Type != event.TypeConfigChange {
		t.Fatalf("committed fact type = %q, want the configuration change the reload files", fact.Type)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionModel {
		t.Fatalf("committed fact dimension = %q, want the model dimension", got)
	}
	if got := fact.Meta[event.MetaKeyConfigChangeGeneration]; got != strconv.FormatUint(answer.Publication.Generation, 10) {
		t.Fatalf("committed fact generation = %q, want the publication generation %d", got, answer.Publication.Generation)
	}
	if got := fact.Meta[event.MetaKeyTriggerSource]; got != event.ConfigChangeSourceConfig {
		t.Fatalf("committed fact trigger source = %q, want the configuration source", got)
	}
	if got := fact.Meta[event.MetaKeyModelFromVersion]; got != "model-a@local/model-a" {
		t.Fatalf("committed fact from version = %q, want the version the move started from", got)
	}
	if got := fact.Meta[event.MetaKeyModelToVersion]; got != "local/model-b" {
		t.Fatalf("committed fact to version = %q, want the version the move asked for", got)
	}
	if got := fact.Meta[event.MetaKeyModelReplacementResult]; got != event.ModelReplacementResultRequested {
		t.Fatalf("committed fact result = %q, want the requested result", got)
	}

	if turn, err := driveIntegrityTurn(ctx, host, addr, "the second round"); err != nil || turn.Terminal != model.KindCompleted {
		t.Fatalf("the second round = (%v, %v), want it completed on the installed version", turn.Terminal, err)
	}
	requests := provider.servedRequests()
	if len(requests) != 2 || requests[0].Model != "model-a" || requests[1].Model != "model-b" {
		t.Fatalf("provider requests = %+v, want the reload to move the next request onto the new version", requests)
	}
	if !scriptedRequestCarries(requests[1], "the second round") {
		t.Fatalf("the new version request = %+v, want the round that began after the reload", requests[1].Messages)
	}
	closeIntegrityHost(t, ctx, host)
}

// runFailedInstallRetriedByNextReload requires a model install the first reload could not complete to be judged again by the next carried reload.
//   - the first reload publishes the rewritten document and files the one model configuration change
//   - the rebuild refuses the version because the provider key is absent
//   - the next reload carries the same document, so the publication is unchanged and the change is re-judged under the same content-addressed fact identity
//   - the duplicate identity still runs the After pass, so the install lands then, without a second fact and without a second generation
func runFailedInstallRetriedByNextReload(t *testing.T) {
	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	host, err := assembly.Boot(configPath, assembly.WithSender(newRecordingSender()))
	if err != nil {
		t.Fatalf("boot the retry host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-reload-retry"}
	if turn, err := driveIntegrityTurn(ctx, host, addr, "the round before the retry"); err != nil || turn.Terminal != model.KindCompleted {
		t.Fatalf("the round before the retry = (%v, %v), want it completed on the boot model", turn.Terminal, err)
	}

	rewriteScriptedHostDocument(t, configPath, root, provider.endpoint(), "model-b")
	t.Setenv("TAGENT_TEST_API_KEY", "")
	first := postReload(t, host.FaceAddr())
	if !first.Reloaded || !first.Publication.Published || len(first.ModelChanges) != 1 {
		t.Fatalf("the first reload = %+v, want the published document beside the one judged model change", first)
	}
	change := first.ModelChanges[0]
	if change.Fact == "" {
		t.Fatal("the first reload judged the model change beside no committed fact")
	}
	if publications := host.Run().LeasePublications(); len(publications) != 0 {
		t.Fatalf("lease publications = %+v, want none beside a rebuild the absent key refused", publications)
	}
	if gapErr := replacementGapOf(host.Run(), "model-lease"); !errors.Is(gapErr, rl.ErrLeaseRebuildFailed) {
		t.Fatalf("the refused install left %v, want the named rebuild refusal", gapErr)
	}

	before := host.Run().CommittedFacts()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	second := postReload(t, host.FaceAddr())
	if !second.Reloaded || second.Publication.Published {
		t.Fatalf("the carried reload = %+v, want the unchanged publication of the document already in effect", second)
	}
	if second.Publication.Generation != first.Publication.Generation {
		t.Fatalf("carried generation = %d, want the generation the first reload published %d", second.Publication.Generation, first.Publication.Generation)
	}
	if len(second.ModelChanges) != 1 || second.ModelChanges[0].Fact != change.Fact {
		t.Fatalf("carried model changes = %+v, want the same content-addressed fact %q the first reload filed", second.ModelChanges, change.Fact)
	}
	if after := host.Run().CommittedFacts(); after != before {
		t.Fatalf("committed facts = %d after the carried reload, want the %d before: a repeated judgment refiles no fact", after, before)
	}
	publications := host.Run().LeasePublications()
	if len(publications) != 1 || publications[0].Name() != "model-b@local/model-b" {
		t.Fatalf("lease publications = %+v, want the one version the carried reload installed", publications)
	}
	fact, err := host.Run().CommittedEvent(ctx, event.EventID(change.Fact))
	if err != nil {
		t.Fatalf("read the committed model change: %v", err)
	}
	if fact.Type != event.TypeConfigChange || fact.Meta[event.MetaKeyConfigChangeDimension] != event.ConfigChangeDimensionModel {
		t.Fatalf("committed fact = (%q, %q), want the model dimension of the configuration change family", fact.Type, fact.Meta[event.MetaKeyConfigChangeDimension])
	}

	if turn, err := driveIntegrityTurn(ctx, host, addr, "the round after the retry"); err != nil || turn.Terminal != model.KindCompleted {
		t.Fatalf("the round after the retry = (%v, %v), want it completed", turn.Terminal, err)
	}
	requests := provider.servedRequests()
	if len(requests) != 2 || requests[0].Model != "model-a" || requests[1].Model != "model-b" {
		t.Fatalf("provider requests = %+v, want the retried install to move the next round onto the new version", requests)
	}
	closeIntegrityHost(t, ctx, host)
}

// TestEventIntegrityIdentityMatrix drives the input-identity and session matrix: repeated bodies, one re-sent identity, and each session's read request.
// - The parent drives the load once and the anchors read it back.
// - positive: both identities of a repeated body are consumed, the re-sent identity is consumed once, and each session's notices and actions keep their identity
// - negative: deduplicating by body, consuming a re-sent identity twice and crossing identities between sessions each fail by name
// - negative: addressing an action to the wrong session also fails by name
// - consumer: Boot, Host.Invoke and Run.CommitToSession drive the load; every request the checks read is the request the provider actually served
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestEventIntegrityIdentityMatrix(t *testing.T) {
	reproductionEnv(t)
	scene := driveIdentityMatrix(t)
	t.Run("positive", func(t *testing.T) { runIdentityMatrixPositive(t, scene) })
	t.Run("negative", func(t *testing.T) { runIdentityMatrixNegative(t, scene) })
	t.Run("consumer", func(t *testing.T) { runIdentityMatrixConsumer(t, scene) })
}

// identityRowBody is the one input body both sessions commit under two
// identities each: the material a body-keyed dedup would collapse.
const identityRowBody = "replay the shared identity row"

// identityBodyMerged, identityResentTwice, identitySessionBleed and identityTargetWrong are the forbidden shapes the identity matrix rejects by name.
const (
	identityBodyMerged   = "same_body_identity_merged"
	identityResentTwice  = "same_identity_resent_twice"
	identitySessionBleed = "cross_session_identity_bleed"
	identityTargetWrong  = "action_target_mismatch"
)

// identityMatrixScene is one observed load of the identity matrix: the two
// sessions' consumption surfaces beside the actions the sender recorded, the
// requests the provider served and the addresses the boot assembly established.
type identityMatrixScene struct {
	sessions []identityMatrixSession
	requests []scriptedRequest
	actions  []senderAction
	active   []event.Address
}

// identityMatrixSession is one session's consumption surface: the read request
// its own entry drove, the follow-up request its own notice drove, the ended
// projection its chain registered, and the identities it committed.
type identityMatrixSession struct {
	addr          event.Address
	readInput     string
	readIndex     int
	read          scriptedRequest
	followIndex   int
	followRequest scriptedRequest
	entries       []replay.ProjectionEntry
	keys          []event.EventID
	answered      []event.EventID
	accepted      string
	followBody    string
	readAnswer    string
	material      string
	settle        event.Event
	notice        task.Notice
	noticeFact    event.Event
}

// identityMatrixViolations reports every way one observed load leaves the
// matrix, each under the named forbidden shape it shows. An empty answer is the
// load the positive anchor accepts.
func identityMatrixViolations(scene identityMatrixScene) []string {
	if len(scene.sessions) != 2 {
		return []string{fmt.Sprintf("identity_matrix: %d sessions observed, want the two the matrix drives", len(scene.sessions))}
	}
	var violations []string
	for index, session := range scene.sessions {
		other := scene.sessions[1-index]
		violations = append(violations, identityPreservationViolations(session)...)
		violations = append(violations, identityIsolationViolations(session, other)...)
		violations = append(violations, identityActionViolations(scene.actions, session, other)...)
	}
	return violations
}

// identityPreservationViolations reports the two failures of the body/identity
// relation: a repeated body collapsed to one input, and a re-sent identity
// consumed twice.
func identityPreservationViolations(session identityMatrixSession) []string {
	var violations []string
	switch copies := reproductionUserCopies(session.read, identityRowBody); {
	case copies < 2:
		violations = append(violations, fmt.Sprintf("%s: the read request of %s carried %d user copies of %q, want the two identities it committed",
			identityBodyMerged, session.addr.SessionID, copies, identityRowBody))
	case copies > 2:
		violations = append(violations, fmt.Sprintf("%s: the read request of %s carried %d user copies of %q, want the two identities with the re-sent one consumed once",
			identityResentTwice, session.addr.SessionID, copies, identityRowBody))
	}
	for _, key := range session.keys {
		switch held := identityMatrixEntryCount(session.entries, key); {
		case held == 0:
			violations = append(violations, fmt.Sprintf("%s: the chain of %s holds no entry of the identity %s, want one entry for each of the two identities",
				identityBodyMerged, session.addr.SessionID, key))
		case held > 1:
			violations = append(violations, fmt.Sprintf("%s: the chain of %s holds %d entries of the identity %s, want the re-sent identity consumed once",
				identityResentTwice, session.addr.SessionID, held, key))
		}
	}
	return violations
}

// identityIsolationViolations reports every way one session's consumption
// surface carries the other session's identity: its committed identities, its
// input body or its own bodies.
func identityIsolationViolations(session, other identityMatrixSession) []string {
	var violations []string
	for _, key := range other.keys {
		if held := identityMatrixEntryCount(session.entries, key); held != 0 {
			violations = append(violations, fmt.Sprintf("%s: the chain of %s holds %d entries of the other session's identity %s",
				identitySessionBleed, session.addr.SessionID, held, key))
		}
	}
	if copies := reproductionUserCopies(session.read, other.readInput); copies != 0 {
		violations = append(violations, fmt.Sprintf("%s: the read request of %s carried %d user copies of the other session's input %q",
			identitySessionBleed, session.addr.SessionID, copies, other.readInput))
	}
	for _, body := range []string{other.accepted, other.followBody} {
		if identityMatrixRequestCarries(session.read, body) {
			violations = append(violations, fmt.Sprintf("%s: the read request of %s carried the other session's body %q",
				identitySessionBleed, session.addr.SessionID, body))
		}
	}
	return violations
}

// identityActionViolations reports every action of the session's own identities
// that did not land exactly once at its own address, beside every action of the
// other session's identities that landed at this one.
func identityActionViolations(actions []senderAction, session, other identityMatrixSession) []string {
	var violations []string
	for _, body := range []string{session.accepted, session.followBody} {
		landed := 0
		for _, action := range actions {
			if action.body != body {
				continue
			}
			landed++
			if action.target != session.addr || !action.confirmed() {
				violations = append(violations, fmt.Sprintf("%s: the action %q landed at %+v (confirmed %v), want %+v",
					identityTargetWrong, body, action.target, action.confirmed(), session.addr))
			}
		}
		if landed != 1 {
			violations = append(violations, fmt.Sprintf("%s: the action %q of %s was recorded %d times, want exactly one confirmed action at %+v",
				identityTargetWrong, body, session.addr.SessionID, landed, session.addr))
		}
	}
	for _, body := range []string{other.accepted, other.followBody} {
		for _, action := range actions {
			if action.body == body && action.target == session.addr {
				violations = append(violations, fmt.Sprintf("%s: the other session's action %q landed at %+v",
					identityTargetWrong, body, action.target))
			}
		}
	}
	return violations
}

// identityMatrixViolationKinds reports the distinct forbidden shapes one answer
// of identityMatrixViolations shows, sorted, so a mutated load is required to
// leave exactly the shape it introduced.
func identityMatrixViolationKinds(violations []string) []string {
	seen := map[string]bool{}
	kinds := make([]string, 0, len(violations))
	for _, violation := range violations {
		kind := violation
		if at := strings.Index(kind, ": "); at >= 0 {
			kind = kind[:at]
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// identityMatrixEntryCount reports how many entries of one projection carry the
// given identity.
func identityMatrixEntryCount(entries []replay.ProjectionEntry, key event.EventID) int {
	count := 0
	for _, entry := range entries {
		if entry.Key == key {
			count++
		}
	}
	return count
}

// identityMatrixRequestCarries reports whether one request carries the exact
// body in any role.
func identityMatrixRequestCarries(request scriptedRequest, body string) bool {
	body = strings.TrimSpace(body)
	for _, message := range request.Messages {
		if strings.TrimSpace(message.Content) == body {
			return true
		}
	}
	return false
}

// identityMatrixDedupedByBody returns the load a body-keyed dedup would leave:
// the second identity of the repeated body never enters the chain and only one
// copy of the body reaches the read request.
func identityMatrixDedupedByBody(scene identityMatrixScene) identityMatrixScene {
	mutated := cloneIdentityMatrixScene(scene)
	session := &mutated.sessions[0]
	session.entries = identityMatrixEntriesWithout(session.entries, session.keys[1])
	session.read.Messages = identityMatrixWithoutOneUserCopy(session.read.Messages, identityRowBody)
	return mutated
}

// identityMatrixResentTwice returns the load a re-sent identity consumed twice
// would leave: its second entry stays in the chain and its second copy reaches
// the read request.
func identityMatrixResentTwice(scene identityMatrixScene) identityMatrixScene {
	mutated := cloneIdentityMatrixScene(scene)
	session := &mutated.sessions[0]
	session.entries = append(session.entries, replay.ProjectionEntry{
		Key:     session.keys[0],
		Type:    event.TypeExternalInput,
		Content: identityRowBody,
	})
	session.read.Messages = append(session.read.Messages, scriptedMessage{Role: string(model.RoleUser), Content: identityRowBody})
	return mutated
}

// identityMatrixBleedingSessions returns the load a shared chain would leave:
// the other session's identity and its own input enter this session's chain and
// its read request.
func identityMatrixBleedingSessions(scene identityMatrixScene) identityMatrixScene {
	mutated := cloneIdentityMatrixScene(scene)
	session, other := &mutated.sessions[0], mutated.sessions[1]
	session.entries = append(session.entries, replay.ProjectionEntry{
		Key:     other.keys[0],
		Type:    event.TypeExternalInput,
		Content: other.readInput,
	})
	session.read.Messages = append(session.read.Messages, scriptedMessage{Role: string(model.RoleUser), Content: other.readInput})
	return mutated
}

// identityMatrixMisaddressedAction returns the load an action addressed to the
// wrong session would leave: the following session answers with the other
// session's notice body.
func identityMatrixMisaddressedAction(scene identityMatrixScene) identityMatrixScene {
	mutated := cloneIdentityMatrixScene(scene)
	from, to := mutated.sessions[0], mutated.sessions[1]
	for index := range mutated.actions {
		if mutated.actions[index].body == from.followBody {
			mutated.actions[index].target = to.addr
			break
		}
	}
	return mutated
}

// cloneIdentityMatrixScene copies one scene so a mutation never rewrites the
// load the positive anchor reads.
func cloneIdentityMatrixScene(scene identityMatrixScene) identityMatrixScene {
	cloned := identityMatrixScene{
		requests: append([]scriptedRequest(nil), scene.requests...),
		actions:  append([]senderAction(nil), scene.actions...),
		active:   append([]event.Address(nil), scene.active...),
	}
	for _, session := range scene.sessions {
		copied := session
		copied.entries = append([]replay.ProjectionEntry(nil), session.entries...)
		copied.keys = append([]event.EventID(nil), session.keys...)
		copied.answered = append([]event.EventID(nil), session.answered...)
		copied.read.Messages = append([]scriptedMessage(nil), session.read.Messages...)
		copied.followRequest.Messages = append([]scriptedMessage(nil), session.followRequest.Messages...)
		cloned.sessions = append(cloned.sessions, copied)
	}
	return cloned
}

// identityMatrixEntriesWithout reports the entries of one projection without
// the given identity.
func identityMatrixEntriesWithout(entries []replay.ProjectionEntry, key event.EventID) []replay.ProjectionEntry {
	kept := make([]replay.ProjectionEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Key == key {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// identityMatrixWithoutOneUserCopy reports the messages of one request with one
// user copy of the body withdrawn.
func identityMatrixWithoutOneUserCopy(messages []scriptedMessage, body string) []scriptedMessage {
	kept := make([]scriptedMessage, 0, len(messages))
	withdrawn := false
	for _, message := range messages {
		if !withdrawn && message.Role == string(model.RoleUser) && strings.TrimSpace(message.Content) == body {
			withdrawn = true
			continue
		}
		kept = append(kept, message)
	}
	return kept
}

// driveIdentityMatrix drives the load the matrix reads once over the boot
// assembly: two sessions delegate the same input body, each worker settles the
// same result body under its own task identity, each notice drives the
// follow-up of its own session, and each session commits one input body twice
// under two identities beside one re-send of the first identity. The final read
// turn of each session is the request its own entry assembles from its own
// chain.
func driveIdentityMatrix(t *testing.T) identityMatrixScene {
	t.Helper()
	const (
		sharedInput = "start the shared identity task"
		workerInA   = "compute the first identity total"
		workerInB   = "compute the second identity total"
		workerOut   = "the shared identity worker reports 42"
		acceptedA   = "the first identity task was accepted"
		acceptedB   = "the second identity task was accepted"
		followA     = "the first identity notice reached the user"
		followB     = "the second identity notice reached the user"
		readA       = "read the first identity rows"
		readB       = "read the second identity rows"
		answerA     = "the first identity rows were read"
		answerB     = "the second identity rows were read"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the first session delegates the shared input", requestTail(sharedInput)).
		answers(settleToolCallAnswer("worker", "call-identity-a", `{"input":"`+workerInA+`"}`))
	script.expect("model-entry", "the first session accepts the shared task", requestCarriesToolResult("call-identity-a")).
		answers(scriptedAnswerOf(acceptedA)).delivers(acceptedA)
	script.expect("model-entry", "the second session delegates the shared input", requestTail(sharedInput)).
		answers(settleToolCallAnswer("worker", "call-identity-b", `{"input":"`+workerInB+`"}`))
	script.expect("model-entry", "the second session accepts the shared task", requestCarriesToolResult("call-identity-b")).
		answers(scriptedAnswerOf(acceptedB)).delivers(acceptedB)
	workerA := script.expect("model-worker", "the first worker answers the shared task", requestTail(workerInA)).
		answers(scriptedAnswerOf(workerOut)).park()
	workerB := script.expect("model-worker", "the second worker answers the shared task", requestTail(workerInB)).
		answers(scriptedAnswerOf(workerOut)).park()
	script.expect("model-entry", "the first identity notice fires its follow-up", requestCarriesSettleNotice(workerOut)).
		answers(scriptedAnswerOf(followA)).delivers(followA)
	script.expect("model-entry", "the second identity notice fires its follow-up", requestCarriesSettleNotice(workerOut)).
		answers(scriptedAnswerOf(followB)).delivers(followB)
	script.expect("model-entry", "the first session reads its identity rows", requestTail(readA)).
		answers(scriptedAnswerOf(answerA)).delivers(answerA)
	script.expect("model-entry", "the second session reads its identity rows", requestTail(readB)).
		answers(scriptedAnswerOf(answerB)).delivers(answerB)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addrA := event.Address{AgentID: "user", SessionID: "session-identity-matrix-first"}
	addrB := event.Address{AgentID: "user", SessionID: "session-identity-matrix-second"}

	driveContinuationTurn(t, ctx, host, addrA, sharedInput, 2)
	script.await(t, workerA)
	driveContinuationTurn(t, ctx, host, addrB, sharedInput, 2)
	script.await(t, workerB)

	script.release(workerA)
	settleA, noticeA, materialA := continuationSettle(t, host, addrA, workerOut)
	reproductionWaitForBody(t, sender, followA, 30*time.Second)
	script.release(workerB)
	settleB, noticeB, materialB := continuationSettle(t, host, addrB, workerOut)
	reproductionWaitForBody(t, sender, followB, 30*time.Second)

	factsA := host.Run().SessionFacts(addrA)
	factsB := host.Run().SessionFacts(addrB)
	noticeFactA := continuationNoticeFact(t, factsA, settleA.TaskID)
	noticeFactB := continuationNoticeFact(t, factsB, settleB.TaskID)

	keysA := []event.EventID{"identity-row-first-session-first", "identity-row-first-session-second"}
	keysB := []event.EventID{"identity-row-second-session-first", "identity-row-second-session-second"}
	answeredA := commitIdentityRows(t, ctx, host, addrA, keysA)
	answeredB := commitIdentityRows(t, ctx, host, addrB, keysB)

	driveContinuationTurn(t, ctx, host, addrA, readA, 1)
	driveContinuationTurn(t, ctx, host, addrB, readB, 1)

	requests := provider.servedRequests()
	readARequest, readAIndex := identityReadRequest(t, requests, readA)
	readBRequest, readBIndex := identityReadRequest(t, requests, readB)
	followARequest, followAIndex := identityFollowRequest(t, requests, materialA, acceptedA, acceptedB, readAIndex, readBIndex)
	followBRequest, followBIndex := identityFollowRequest(t, requests, materialB, acceptedB, acceptedA, readAIndex, readBIndex)
	entriesA, mappedA := host.Run().ReplayEntries(addrA.SessionID)
	if !mappedA {
		t.Fatalf("the chain of %s registered no replay projection, want the declared window to map it", addrA.SessionID)
	}
	entriesB, mappedB := host.Run().ReplayEntries(addrB.SessionID)
	if !mappedB {
		t.Fatalf("the chain of %s registered no replay projection, want the declared window to map it", addrB.SessionID)
	}

	scene := identityMatrixScene{
		requests: requests,
		actions:  sender.actions(),
		active:   host.Run().ActiveSessions("user"),
		sessions: []identityMatrixSession{
			{
				addr: addrA, readInput: readA, readIndex: readAIndex, read: readARequest,
				followIndex: followAIndex, followRequest: followARequest,
				entries: entriesA, keys: keysA, answered: answeredA,
				accepted: acceptedA, followBody: followA, readAnswer: answerA, material: materialA,
				settle: settleA, notice: noticeA, noticeFact: noticeFactA,
			},
			{
				addr: addrB, readInput: readB, readIndex: readBIndex, read: readBRequest,
				followIndex: followBIndex, followRequest: followBRequest,
				entries: entriesB, keys: keysB, answered: answeredB,
				accepted: acceptedB, followBody: followB, readAnswer: answerB, material: materialB,
				settle: settleB, notice: noticeB, noticeFact: noticeFactB,
			},
		},
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the identity-matrix script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
	return scene
}

// commitIdentityRows commits the same input body into one session under the
// given identities, then re-sends the first identity, and requires each commit
// to answer the identity the caller stated.
func commitIdentityRows(t *testing.T, ctx context.Context, host *assembly.Host, addr event.Address, keys []event.EventID) []event.EventID {
	t.Helper()
	answered := make([]event.EventID, 0, len(keys)+1)
	for _, pass := range [][]event.EventID{keys, keys[:1]} {
		for _, key := range pass {
			evt := event.Event{
				Key:     key,
				Type:    event.TypeExternalInput,
				Message: model.Message{Role: model.RoleUser, Content: identityRowBody},
			}
			got, err := host.Run().CommitToSession(ctx, addr, evt)
			if err != nil {
				t.Fatalf("commit the identity %s into %s: %v", key, addr.SessionID, err)
			}
			if got != key {
				t.Fatalf("the commit of the identity %s answered %s, want the identity the caller stated", key, got)
			}
			answered = append(answered, got)
		}
	}
	return answered
}

// identityReadRequest reports the one request the read turn of a session drove:
// the request whose last message is that session's own read input.
func identityReadRequest(t *testing.T, requests []scriptedRequest, input string) (scriptedRequest, int) {
	t.Helper()
	match := requestTail(input)
	found := -1
	for index, request := range requests {
		if !match(request) {
			continue
		}
		if found >= 0 {
			t.Fatalf("requests whose last message is %q = %d and %d, want the one read turn", input, found, index)
		}
		found = index
	}
	if found < 0 {
		t.Fatalf("no request whose last message is %q; served models = %v", input, identityMatrixRequestModels(requests))
	}
	return requests[found], found
}

// identityFollowRequest reports the one follow-up request one session's notice
// drove: the request that carries the settled material beside that session's
// own accepted body, excluding the read turns already identified.
func identityFollowRequest(t *testing.T, requests []scriptedRequest, material, ownMark, foreignMark string, excluded ...int) (scriptedRequest, int) {
	t.Helper()
	skip := map[int]bool{}
	for _, index := range excluded {
		skip[index] = true
	}
	carries := requestCarriesSettleNotice(material)
	found := -1
	for index, request := range requests {
		if skip[index] || !carries(request) {
			continue
		}
		if !identityMatrixRequestCarries(request, ownMark) || identityMatrixRequestCarries(request, foreignMark) {
			continue
		}
		if found >= 0 {
			t.Fatalf("follow-up requests carrying %q beside %q = %d and %d, want the one the session's notice drove",
				material, ownMark, found, index)
		}
		found = index
	}
	if found < 0 {
		t.Fatalf("no follow-up request carried %q beside %q; served models = %v", material, ownMark, identityMatrixRequestModels(requests))
	}
	return requests[found], found
}

// identityMatrixRequestModels renders the models of the served requests as the
// line a failed check reads.
func identityMatrixRequestModels(requests []scriptedRequest) []string {
	models := make([]string, 0, len(requests))
	for _, request := range requests {
		models = append(models, request.Model)
	}
	return models
}

// runIdentityMatrixPositive reads the shared load back: both identities of the
// repeated body survive, the re-sent identity is consumed once, and the two
// sessions' notices and actions each belong to their own identity.
func runIdentityMatrixPositive(t *testing.T, scene identityMatrixScene) {
	t.Helper()
	if violations := identityMatrixViolations(scene); len(violations) != 0 {
		t.Fatalf("the observed identity matrix left violations: %v", violations)
	}
	first, second := scene.sessions[0], scene.sessions[1]
	if first.settle.TaskID == second.settle.TaskID {
		t.Fatalf("the two sessions settled under the same task identity %s, want one identity each", first.settle.TaskID)
	}
	if first.notice == second.notice {
		t.Fatalf("the two sessions carry the same notice identity %+v, want one identity each", first.notice)
	}
	for _, session := range scene.sessions {
		if want := agent.NoticeFactKey(session.noticeFact.InvocationID, session.notice); session.noticeFact.Key != want {
			t.Fatalf("the notice fact of %s = %s, want the notice identity key %s", session.addr.SessionID, session.noticeFact.Key, want)
		}
		if !strings.Contains(session.settle.Message.Content, session.material) {
			t.Fatalf("the settle fact of %s carries %q, want the material %q", session.addr.SessionID, session.settle.Message.Content, session.material)
		}
		if got := reproductionUserCopies(session.followRequest, session.material); got != 1 {
			t.Fatalf("the follow-up request of %s carried %d user copies of the settled body %q, want exactly the one its own notice identity committed",
				session.addr.SessionID, got, session.material)
		}
		if !identityMatrixRequestCarries(session.followRequest, session.accepted) {
			t.Fatalf("the follow-up request of %s does not carry its own accepted body %q", session.addr.SessionID, session.accepted)
		}
		if got := reproductionUserCopies(session.read, identityRowBody); got != 2 {
			t.Fatalf("the read request of %s carried %d user copies of %q, want the two identities it committed",
				session.addr.SessionID, got, identityRowBody)
		}
		identityMatrixRequireAction(t, scene.actions, session.accepted, session.addr)
		identityMatrixRequireAction(t, scene.actions, session.followBody, session.addr)
	}
}

// identityMatrixRequireAction requires exactly one confirmed action carrying the
// body at the address.
func identityMatrixRequireAction(t *testing.T, actions []senderAction, body string, addr event.Address) {
	t.Helper()
	landed := 0
	for _, action := range actions {
		if action.body != body {
			continue
		}
		landed++
		if action.target != addr || !action.confirmed() {
			t.Fatalf("the action %q = %+v, want it addressed to %+v and confirmed", body, action, addr)
		}
	}
	if landed != 1 {
		t.Fatalf("actions carrying %q = %d, want exactly 1 at %+v; recorded bodies = %v", body, landed, addr, identityMatrixActionBodies(actions))
	}
}

// identityMatrixActionBodies renders the recorded action bodies as the line a
// failed check reads.
func identityMatrixActionBodies(actions []senderAction) []string {
	bodies := make([]string, 0, len(actions))
	for _, action := range actions {
		bodies = append(bodies, action.body)
	}
	return bodies
}

// runIdentityMatrixNegative requires every forbidden shape to fail by name: the
// observed load passes the same checks, and each mutated load leaves exactly
// the one shape it introduces.
func runIdentityMatrixNegative(t *testing.T, scene identityMatrixScene) {
	t.Helper()
	if violations := identityMatrixViolations(scene); len(violations) != 0 {
		t.Fatalf("the observed load already leaves the matrix: %v", violations)
	}
	cases := []struct {
		name    string
		mutated identityMatrixScene
		kind    string
	}{
		{"body_deduped", identityMatrixDedupedByBody(scene), identityBodyMerged},
		{"resent_consumed_twice", identityMatrixResentTwice(scene), identityResentTwice},
		{"cross_session_bleed", identityMatrixBleedingSessions(scene), identitySessionBleed},
		{"action_misaddressed", identityMatrixMisaddressedAction(scene), identityTargetWrong},
	}
	for _, rejected := range cases {
		kinds := identityMatrixViolationKinds(identityMatrixViolations(rejected.mutated))
		if len(kinds) != 1 || kinds[0] != rejected.kind {
			t.Fatalf("the %s load left the violations %v, want exactly the one %q", rejected.name, kinds, rejected.kind)
		}
	}
}

// runIdentityMatrixConsumer reads the load back from the formal entries: the
// boot assembly established both sessions, every read request the checks read
// is the request the provider served, CommitToSession answered the identities
// the caller stated, and every action landed at one of the two user sessions.
func runIdentityMatrixConsumer(t *testing.T, scene identityMatrixScene) {
	t.Helper()
	if len(scene.requests) != 10 {
		t.Fatalf("the provider served %d requests, want the eight entry decisions and the two worker decisions", len(scene.requests))
	}
	models := map[string]int{}
	for _, request := range scene.requests {
		models[request.Model]++
	}
	if len(models) != 2 || models["model-entry"] != 8 || models["model-worker"] != 2 {
		t.Fatalf("the served requests named the models %v, want eight entry decisions and two worker decisions", models)
	}
	if len(scene.active) != 2 {
		t.Fatalf("the boot assembly established %d user sessions %+v, want the two the matrix drives", len(scene.active), scene.active)
	}
	for _, session := range scene.sessions {
		if session.read.Index != session.readIndex || scene.requests[session.readIndex].Index != session.readIndex {
			t.Fatalf("the read request of %s carries the ordinal %d at the served position %d, want the request the provider served there",
				session.addr.SessionID, session.read.Index, session.readIndex)
		}
		if session.followRequest.Index != session.followIndex || scene.requests[session.followIndex].Index != session.followIndex {
			t.Fatalf("the follow-up request of %s carries the ordinal %d at the served position %d, want the request the provider served there",
				session.addr.SessionID, session.followRequest.Index, session.followIndex)
		}
		if session.read.Model != "model-entry" {
			t.Fatalf("the read request of %s named model %q, want the entry identity", session.addr.SessionID, session.read.Model)
		}
		if got := reproductionUserCopies(session.read, session.readInput); got != 1 {
			t.Fatalf("the read request of %s carried %d user copies of its own input %q, want one", session.addr.SessionID, got, session.readInput)
		}
		if held := identityMatrixEntryCount(session.entries, session.keys[0]); held != 1 {
			t.Fatalf("the ReplayEntries face of %s holds %d entries of the re-sent identity %s, want one",
				session.addr.SessionID, held, session.keys[0])
		}
		want := append(append([]event.EventID(nil), session.keys...), session.keys[0])
		if !reflect.DeepEqual(session.answered, want) {
			t.Fatalf("CommitToSession answered %v for %s, want the identities the caller stated %v",
				session.answered, session.addr.SessionID, want)
		}
		identityMatrixRequireAction(t, scene.actions, session.readAnswer, session.addr)
	}
	for index, action := range scene.actions {
		if !action.confirmed() {
			t.Fatalf("the action %d %q was not confirmed: %+v", index, action.body, action)
		}
		if action.target != scene.sessions[0].addr && action.target != scene.sessions[1].addr {
			t.Fatalf("the action %d %q landed at %+v, want one of the two user sessions", index, action.body, action.target)
		}
	}
}

// TestEventIntegrityDecisionFlowMatrix drives the multi-decision flow matrix over the Boot assembly through one observed load:
// - A user turn sends a note, keeps deciding after the send result, delegates to a subagent, and ends on its final answer.
// - A second session runs its own delegation; two terminal notices arrive while one notice-driven follow-up is parked.
// - The second notice is therefore retained and continued after the in-flight turn ends.
// - The three anchors share one observed load:
// - positive: the send result and the subagent acceptance drive the next decisions of the original invocation
// - positive: the busy notice is retained and continued once the in-flight turn ends, and every legal action stays once
// - negative: a dropped busy notice, a continuation opening a new invocation and a notice generating a second input each fail by name
// - negative: a lost subagent result, a repeated action and the three script departures each fail by name
// - consumer: Boot and Host.Invoke drive the real requests and actions, and the drain point reports every formal obligation settled
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
func TestEventIntegrityDecisionFlowMatrix(t *testing.T) {
	reproductionEnv(t)
	scene := driveDecisionFlowMatrix(t)
	t.Run("positive", func(t *testing.T) { runDecisionFlowPositive(t, scene) })
	t.Run("negative", func(t *testing.T) { runDecisionFlowNegative(t, scene) })
	t.Run("consumer", func(t *testing.T) { runDecisionFlowConsumer(t, scene) })
}

// decisionFlowFirstInput, decisionFlowFirstNote and the other decision-flow constants are the one load the matrix reads: the first session's send chain beside two delegations whose terminal notices interleave.
const (
	decisionFlowFirstInput      = "send the first flow note"
	decisionFlowFirstNote       = "the first flow note"
	decisionFlowFirstWorkerIn   = "compute the first flow total"
	decisionFlowFirstWorkerOut  = "the first flow worker reports 41"
	decisionFlowFirstFinal      = "the first flow settled its note and task"
	decisionFlowFirstFollow     = "the first flow notice reached its user"
	decisionFlowSecondInput     = "start the second flow task"
	decisionFlowSecondWorkerIn  = "compute the second flow total"
	decisionFlowSecondWorkerOut = "the second flow worker reports 42"
	decisionFlowSecondFinal     = "the second flow task was accepted"
	decisionFlowSecondFollow    = "the second flow notice reached its user"
	decisionFlowSendCall        = "call-decision-flow-send"
	decisionFlowFirstCall       = "call-decision-flow-first"
	decisionFlowSecondCall      = "call-decision-flow-second"
)

// decisionFlowChainIncomplete, decisionFlowNewInvocation and the other decision-flow constants are the forbidden shapes the matrix rejects by name.
const (
	decisionFlowChainIncomplete   = "decision_chain_incomplete"
	decisionFlowNewInvocation     = "continuation_new_invocation"
	decisionFlowNoticeDropped     = "notice_dropped"
	decisionFlowNoticeSource      = "notice_wrong_source"
	decisionFlowNoticeSecondInput = "notice_second_input"
	decisionFlowResultDropped     = "subagent_result_dropped"
	decisionFlowBusyNotRetained   = "busy_notice_not_retained"
	decisionFlowDuplicateAction   = "duplicate_action"
	decisionFlowActionMissing     = "action_missing"
	decisionFlowActionMisplaced   = "action_misplaced"
	decisionFlowUnexpectedAction  = "unexpected_action"
	decisionFlowCrossed           = "flow_crossed"
)

// decisionFlowScene is one observed load of the decision-flow matrix: the user
// turns and the notice-driven follow-ups beside the requests the provider
// served, the actions the sender recorded and the drain the formal close
// reported. The busy fields are read at the observation point, while the first
// follow-up is parked at the model and the second notice has already settled.
type decisionFlowScene struct {
	turns        []decisionFlowTurn
	flows        []decisionFlowNotice
	requests     []scriptedRequest
	entry        []scriptedRequest
	actions      []senderAction
	active       []event.Address
	drain        agent.DrainReport
	declared     []string
	busyRequests int
	busyInputs   int
}

// decisionFlowTurn is one user turn's consumption surface: the input it
// committed, the turn it drove, the final answer it delivered and the wrapped
// calls whose results drove its later decisions.
type decisionFlowTurn struct {
	addr       event.Address
	input      string
	finalBody  string
	decisions  int
	turn       agent.Turn
	inputFact  event.Event
	outputFact event.Event
	calls      []decisionFlowCall
}

// decisionFlowCall is one wrapped call a turn issued: the result fact the
// formal assembly committed under its call identity. A send call reaches no
// channel, so no call carries an outbound body of its own.
type decisionFlowCall struct {
	name   string
	callID string
	fact   event.Event
}

// decisionFlowNotice is one notice-driven flow: the settled delivery fact, the
// notice identity it reports, the follow-up range it committed and the request
// and action that range produced. inputFacts holds the facts that name the
// consuming range: the settled transport record names its call's own range, so
// it is never counted as a second input of the notice material.
type decisionFlowNotice struct {
	addr         event.Address
	input        string
	settle       event.Event
	notice       task.Notice
	material     string
	facts        []event.Event
	inputFacts   []event.Event
	noticeFact   event.Event
	request      scriptedRequest
	requestIndex int
	followBody   string
	outputFact   event.Event
}

// driveDecisionFlowMatrix drives the load once and reads it back: the two user
// turns, the two parked worker decisions, the first notice-driven follow-up
// parked at the model, the second notice settled in the busy window, and then
// the release of the in-flight follow-up that lets the retained notice run.
func driveDecisionFlowMatrix(t *testing.T) decisionFlowScene {
	t.Helper()
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the first flow sends its note", requestTail(decisionFlowFirstInput)).
		answers(settleToolCallAnswer("send", decisionFlowSendCall, `{"content":"`+decisionFlowFirstNote+`"}`))
	script.expect("model-entry", "the first flow continues after its send", requestCarriesToolResult(decisionFlowSendCall)).
		answers(settleToolCallAnswer("worker", decisionFlowFirstCall, `{"input":"`+decisionFlowFirstWorkerIn+`"}`))
	script.expect("model-entry", "the first flow answers after its delegation", requestCarriesToolResult(decisionFlowFirstCall)).
		answers(scriptedAnswerOf(decisionFlowFirstFinal)).
		delivers(decisionFlowFirstFinal)
	script.expect("model-entry", "the second flow delegates its task", requestTail(decisionFlowSecondInput)).
		answers(settleToolCallAnswer("worker", decisionFlowSecondCall, `{"input":"`+decisionFlowSecondWorkerIn+`"}`))
	script.expect("model-entry", "the second flow answers after its delegation", requestCarriesToolResult(decisionFlowSecondCall)).
		answers(scriptedAnswerOf(decisionFlowSecondFinal)).
		delivers(decisionFlowSecondFinal)
	firstWorker := script.expect("model-worker", "the first flow worker answers", requestTail(decisionFlowFirstWorkerIn)).
		answers(scriptedAnswerOf(decisionFlowFirstWorkerOut)).park()
	secondWorker := script.expect("model-worker", "the second flow worker answers", requestTail(decisionFlowSecondWorkerIn)).
		answers(scriptedAnswerOf(decisionFlowSecondWorkerOut)).park()
	firstFollow := script.expect("model-entry", "the first flow notice fires its follow-up", requestCarriesSettleNotice(decisionFlowFirstWorkerOut)).
		answers(scriptedAnswerOf(decisionFlowFirstFollow)).delivers(decisionFlowFirstFollow).park()
	secondFollow := script.expect("model-entry", "the second flow notice fires its follow-up", requestCarriesSettleNotice(decisionFlowSecondWorkerOut)).
		answers(scriptedAnswerOf(decisionFlowSecondFollow)).delivers(decisionFlowSecondFollow)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootReproductionHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	firstAddr := event.Address{AgentID: "user", SessionID: "session-decision-flow-first"}
	secondAddr := event.Address{AgentID: "user", SessionID: "session-decision-flow-second"}

	firstTurn := driveContinuationTurn(t, ctx, host, firstAddr, decisionFlowFirstInput, 3)
	script.await(t, firstWorker)
	secondTurn := driveContinuationTurn(t, ctx, host, secondAddr, decisionFlowSecondInput, 2)
	script.await(t, secondWorker)

	script.release(firstWorker)
	firstSettle, firstNotice, firstMaterial := continuationSettle(t, host, firstAddr, decisionFlowFirstWorkerOut)
	script.await(t, firstFollow)

	script.release(secondWorker)
	secondSettle, secondNotice, secondMaterial := continuationSettle(t, host, secondAddr, decisionFlowSecondWorkerOut)
	busyRequests := len(reproductionRequestsCarrying(provider.servedRequests(), secondMaterial))
	busyInputs := continuationNoticeFactCount(host.Run().SessionFacts(secondAddr), secondSettle.TaskID)

	script.release(firstFollow)
	reproductionWaitForBody(t, sender, decisionFlowFirstFollow, 30*time.Second)
	script.await(t, secondFollow)
	reproductionWaitForBody(t, sender, decisionFlowSecondFollow, 30*time.Second)

	requests := provider.servedRequests()
	firstRequest, firstIndex := continuationWaitForRequest(t, provider, firstMaterial, 30*time.Second)
	secondRequest, secondIndex := continuationWaitForRequest(t, provider, secondMaterial, 30*time.Second)
	firstFacts := host.Run().SessionFacts(firstAddr)
	secondFacts := host.Run().SessionFacts(secondAddr)

	firstTurnScene := decisionFlowTurnOf(t, firstAddr, firstTurn, firstFacts, decisionFlowFirstInput, decisionFlowFirstFinal, 3,
		[]decisionFlowCallSpec{
			{name: "send", callID: decisionFlowSendCall},
			{name: "worker", callID: decisionFlowFirstCall},
		})
	secondTurnScene := decisionFlowTurnOf(t, secondAddr, secondTurn, secondFacts, decisionFlowSecondInput, decisionFlowSecondFinal, 2,
		[]decisionFlowCallSpec{{name: "worker", callID: decisionFlowSecondCall}})
	firstFlow := decisionFlowNotice{
		addr: firstAddr, input: decisionFlowFirstInput,
		settle: firstSettle, notice: firstNotice, material: firstMaterial,
		facts: firstFacts, inputFacts: reproductionInputConsumers(reproductionInputFacts(firstFacts, firstMaterial)),
		noticeFact: continuationNoticeFact(t, firstFacts, firstSettle.TaskID),
		request:    firstRequest, requestIndex: firstIndex,
		followBody: decisionFlowFirstFollow,
		outputFact: decisionFlowAssistantFact(t, firstFacts, decisionFlowFirstFollow),
	}
	secondFlow := decisionFlowNotice{
		addr: secondAddr, input: decisionFlowSecondInput,
		settle: secondSettle, notice: secondNotice, material: secondMaterial,
		facts: secondFacts, inputFacts: reproductionInputConsumers(reproductionInputFacts(secondFacts, secondMaterial)),
		noticeFact: continuationNoticeFact(t, secondFacts, secondSettle.TaskID),
		request:    secondRequest, requestIndex: secondIndex,
		followBody: decisionFlowSecondFollow,
		outputFact: decisionFlowAssistantFact(t, secondFacts, decisionFlowSecondFollow),
	}

	scene := decisionFlowScene{
		turns:        []decisionFlowTurn{firstTurnScene, secondTurnScene},
		flows:        []decisionFlowNotice{firstFlow, secondFlow},
		requests:     requests,
		entry:        decisionFlowEntryRequests(requests),
		actions:      sender.actions(),
		active:       host.Run().ActiveSessions("user"),
		declared:     script.actionBodies(),
		busyRequests: busyRequests,
		busyInputs:   busyInputs,
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the decision-flow script left violations: %v", violations)
	}
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
	scene.drain = report
	return scene
}

// decisionFlowCallSpec declares one wrapped call of a turn: the name the check
// reads the call by beside the identity the admission frame carries.
type decisionFlowCallSpec struct {
	name   string
	callID string
}

// decisionFlowTurnOf reads one user turn's consumption surface back from the
// facts its session committed and the turn the drive answered with.
func decisionFlowTurnOf(t *testing.T, addr event.Address, turn agent.Turn, facts []event.Event, input, finalBody string, decisions int, calls []decisionFlowCallSpec) decisionFlowTurn {
	t.Helper()
	out := decisionFlowTurn{
		addr: addr, input: input, finalBody: finalBody, decisions: decisions, turn: turn,
		inputFact:  decisionFlowInputFact(t, facts, input),
		outputFact: decisionFlowAssistantFact(t, facts, finalBody),
	}
	for _, call := range calls {
		out.calls = append(out.calls, decisionFlowCall{
			name: call.name, callID: call.callID,
			fact: decisionFlowResultFact(t, facts, call.callID),
		})
	}
	return out
}

// decisionFlowInputFact reports the one external input fact the session
// committed for the user input.
func decisionFlowInputFact(t *testing.T, facts []event.Event, input string) event.Event {
	t.Helper()
	input = strings.TrimSpace(input)
	var matched []event.Event
	for _, fact := range facts {
		if fact.Type == event.TypeExternalInput && strings.TrimSpace(fact.Message.Content) == input {
			matched = append(matched, fact)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("external input facts carrying %q = %d, want exactly 1: %s", input, len(matched), reproductionFactsSummary(facts))
	}
	return matched[0]
}

// decisionFlowAssistantFact reports the one committed assistant-output fact the
// session carries for the body: the source identity a delivered answer reads.
func decisionFlowAssistantFact(t *testing.T, facts []event.Event, body string) event.Event {
	t.Helper()
	body = strings.TrimSpace(body)
	var matched []event.Event
	for _, fact := range facts {
		if fact.Type != event.TypeAgentOutput || len(fact.Message.ToolCalls) > 0 {
			continue
		}
		if strings.TrimSpace(fact.Message.Content) == body {
			matched = append(matched, fact)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("committed assistant-output facts carrying %q = %d, want exactly 1: %s", body, len(matched), reproductionFactsSummary(facts))
	}
	return matched[0]
}

// decisionFlowResultFact reports the one committed action-result fact the
// wrapped call left in the chain.
func decisionFlowResultFact(t *testing.T, facts []event.Event, callID string) event.Event {
	t.Helper()
	var matched []event.Event
	for _, fact := range facts {
		if fact.Type == event.TypeActionCommand && fact.CallID == callID {
			matched = append(matched, fact)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("committed action-result facts carrying the call identity %q = %d, want exactly 1: %s",
			callID, len(matched), reproductionFactsSummary(facts))
	}
	return matched[0]
}

// decisionFlowSegmentRange reports whether one identity is a range the
// consumption loop minted for the session: <session>#c<N>. A user turn and the
// notice-driven follow-up it continues run under such a range, while the facts
// they consume keep the identity of the entry that committed them.
func decisionFlowSegmentRange(id event.InvocationID, addr event.Address) bool {
	prefix := string(addr.SessionID) + "#c"
	if !strings.HasPrefix(string(id), prefix) {
		return false
	}
	count := strings.TrimPrefix(string(id), prefix)
	return count != "" && strings.Trim(count, "0123456789") == ""
}

// decisionFlowEntryRequests reports the entry agent's requests in served order,
// so the checks read the decision sequence without the worker requests.
func decisionFlowEntryRequests(requests []scriptedRequest) []scriptedRequest {
	out := make([]scriptedRequest, 0, len(requests))
	for _, request := range requests {
		if request.Model == "model-entry" {
			out = append(out, request)
		}
	}
	return out
}

// decisionFlowViolations reports every way one observed load leaves the matrix,
// each under the named forbidden shape it shows. An empty answer is the load the
// positive anchor accepts.
func decisionFlowViolations(scene decisionFlowScene) []string {
	if len(scene.turns) != 2 || len(scene.flows) != 2 {
		return []string{fmt.Sprintf("decision_flow_matrix: %d turns and %d notice flows observed, want the two and two the matrix drives",
			len(scene.turns), len(scene.flows))}
	}
	var violations []string
	for _, turn := range scene.turns {
		violations = append(violations, decisionFlowTurnViolations(turn)...)
	}
	for index, flow := range scene.flows {
		violations = append(violations, decisionFlowNoticeViolations(flow, scene.flows[1-index])...)
		if flow.noticeFact.InvocationID != "" && flow.noticeFact.InvocationID == scene.turns[index].turn.InvocationID {
			violations = append(violations, fmt.Sprintf("%s: the notice fact of %s merged into the user range %q, want a range of its own",
				decisionFlowNewInvocation, flow.addr.SessionID, scene.turns[index].turn.InvocationID))
		}
		if flow.outputFact.InvocationID != flow.noticeFact.InvocationID {
			if !decisionFlowSegmentRange(flow.outputFact.InvocationID, flow.addr) {
				violations = append(violations, fmt.Sprintf("%s: the follow-up answer %q was committed under the range %q, want the segment range the loop minted that read the notice %q",
					decisionFlowChainIncomplete, flow.followBody, flow.outputFact.InvocationID, flow.noticeFact.InvocationID))
			}
		}
	}
	violations = append(violations, decisionFlowWindowViolations(scene)...)
	violations = append(violations, decisionFlowBusyViolations(scene)...)
	violations = append(violations, decisionFlowActionViolations(scene)...)
	return violations
}

// decisionFlowTurnViolations reports every way one user turn left the chain its
// decisions declare: a turn that ended short, a continuation outside the
// original invocation, and a result committed outside the range that issued it.
func decisionFlowTurnViolations(turn decisionFlowTurn) []string {
	var violations []string
	if turn.turn.Terminal != model.KindCompleted || turn.turn.DecisionCount != turn.decisions {
		violations = append(violations, fmt.Sprintf("%s: the user turn %q = (%v, %d decisions), want the completed turn of the %d decisions its chain declares",
			decisionFlowChainIncomplete, turn.input, turn.turn.Terminal, turn.turn.DecisionCount, turn.decisions))
	}
	if turn.turn.InvocationID == "" || !decisionFlowSegmentRange(turn.turn.InvocationID, turn.addr) {
		violations = append(violations, fmt.Sprintf("%s: the user turn %q ran under the range %q, want the segment range the loop minted at %s",
			decisionFlowChainIncomplete, turn.input, turn.turn.InvocationID, turn.addr.SessionID))
	} else if turn.inputFact.InvocationID == turn.turn.InvocationID {
		violations = append(violations, fmt.Sprintf("%s: the input %q was committed under the segment range %q, want the submitter identity the entry handed it beside the range the turn ran under",
			decisionFlowChainIncomplete, turn.input, turn.inputFact.InvocationID))
	}
	if turn.outputFact.InvocationID != turn.turn.InvocationID {
		violations = append(violations, fmt.Sprintf("%s: the final answer %q was committed under the range %q, want the original invocation %q the decisions continued in",
			decisionFlowNewInvocation, turn.finalBody, turn.outputFact.InvocationID, turn.turn.InvocationID))
	}
	for _, call := range turn.calls {
		if call.fact.InvocationID != turn.turn.InvocationID {
			violations = append(violations, fmt.Sprintf("%s: the result of the %s call %q was committed under the range %q, want the original invocation %q that issued it",
				decisionFlowChainIncomplete, call.name, call.callID, call.fact.InvocationID, turn.turn.InvocationID))
		}
	}
	return violations
}

// decisionFlowNoticeViolations reports every way one notice flow left the
// consumption surface its identity declares: no input, a second input, a head
// fact of the wrong source, and material of the other session.
func decisionFlowNoticeViolations(flow, other decisionFlowNotice) []string {
	var violations []string
	switch inputs := len(flow.inputFacts); {
	case inputs == 0:
		violations = append(violations, fmt.Sprintf("%s: the notice %+v of %s generated no new user-input fact for %q, want the one feedback fact",
			decisionFlowNoticeDropped, flow.notice, flow.addr.SessionID, flow.material))
	case inputs > 1:
		violations = append(violations, fmt.Sprintf("%s: the notice %+v of %s generated %d new user-input facts for %q, want exactly one: %s",
			decisionFlowNoticeSecondInput, flow.notice, flow.addr.SessionID, inputs, flow.material, reproductionFactsSummary(flow.inputFacts)))
	default:
		input := flow.inputFacts[0]
		if input.Type != event.TypeFeedback || input.Meta[event.MetaKeyTriggerSource] != "task" ||
			input.TaskID != flow.settle.TaskID || input.Meta[event.MetaKeyTaskID] != string(flow.settle.TaskID) {
			violations = append(violations, fmt.Sprintf("%s: the notice input entered as %s (trigger %q, task %q), want the feedback fact of task %q: %s",
				decisionFlowNoticeSource, input.Type, input.Meta[event.MetaKeyTriggerSource], input.TaskID, flow.settle.TaskID, reproductionFactSummary(input)))
		}
		if want := agent.NoticeFactKey(input.InvocationID, flow.notice); input.Key != want {
			violations = append(violations, fmt.Sprintf("%s: the notice input key = %s, want the notice identity key %s",
				decisionFlowNoticeSource, input.Key, want))
		}
		if input.Owner != flow.addr {
			violations = append(violations, fmt.Sprintf("%s: the notice input landed at the owner %+v, want %+v",
				decisionFlowCrossed, input.Owner, flow.addr))
		}
	}
	if got := reproductionUserCopies(flow.request, flow.material); got != 1 {
		kind := decisionFlowNoticeDropped
		if got > 1 {
			kind = decisionFlowNoticeSecondInput
		}
		violations = append(violations, fmt.Sprintf("%s: the follow-up request carried %d user copies of its notice material %q, want exactly one",
			kind, got, flow.material))
	}
	if got := reproductionUserCopies(flow.request, flow.input); got != 1 {
		violations = append(violations, fmt.Sprintf("%s: the follow-up request carried %d user copies of its own session input %q, want the one the range committed",
			decisionFlowCrossed, got, flow.input))
	}
	for _, foreign := range []string{other.input, other.material} {
		if got := reproductionUserCopies(flow.request, foreign); got != 0 {
			violations = append(violations, fmt.Sprintf("%s: the follow-up request carried %d user copies of the other session's material %q",
				decisionFlowCrossed, got, foreign))
		}
	}
	if flow.noticeFact.Key != agent.NoticeFactKey(flow.noticeFact.InvocationID, flow.notice) {
		violations = append(violations, fmt.Sprintf("%s: the notice fact key = %s, want the notice identity key %s",
			decisionFlowNoticeSource, flow.noticeFact.Key, agent.NoticeFactKey(flow.noticeFact.InvocationID, flow.notice)))
	}
	if flow.noticeFact.Type != event.TypeFeedback || flow.noticeFact.Meta[event.MetaKeyTriggerSource] != "task" ||
		flow.noticeFact.TaskID != flow.settle.TaskID {
		violations = append(violations, fmt.Sprintf("%s: the notice fact = %s, want the feedback fact of task %s",
			decisionFlowNoticeSource, reproductionFactSummary(flow.noticeFact), flow.settle.TaskID))
	}
	return violations
}

// decisionFlowWindowViolations reports every step the declared decision chain
// of the two user turns is missing: the send result that drives the next
// decision, the subagent call that decision issues, and the subagent result the
// final decision reads.
func decisionFlowWindowViolations(scene decisionFlowScene) []string {
	if len(scene.entry) != 7 {
		return []string{fmt.Sprintf("%s: the entry agent served %d requests, want the three decisions of the first turn, the two of the second and the two notice follow-ups",
			decisionFlowChainIncomplete, len(scene.entry))}
	}
	var violations []string
	first, second := scene.entry[:3], scene.entry[3:5]
	if !requestTail(decisionFlowFirstInput)(first[0]) {
		violations = append(violations, fmt.Sprintf("%s: the first entry request does not end on the first flow input %q",
			decisionFlowChainIncomplete, decisionFlowFirstInput))
	}
	if !requestCarriesToolResult(decisionFlowSendCall)(first[1]) {
		violations = append(violations, fmt.Sprintf("%s: the decision after the send does not carry the result of the send call %q",
			decisionFlowChainIncomplete, decisionFlowSendCall))
	}
	if !reproductionCarriesToolCall(first[2], "worker", decisionFlowFirstCall, decisionFlowFirstWorkerIn) {
		violations = append(violations, fmt.Sprintf("%s: the decision after the send did not issue the subagent call %q",
			decisionFlowChainIncomplete, decisionFlowFirstCall))
	}
	if !requestCarriesToolResult(decisionFlowFirstCall)(first[2]) {
		violations = append(violations, fmt.Sprintf("%s: the last decision of the first turn does not carry the subagent result of %q",
			decisionFlowResultDropped, decisionFlowFirstCall))
	}
	if !requestTail(decisionFlowSecondInput)(second[0]) {
		violations = append(violations, fmt.Sprintf("%s: the third entry request does not end on the second flow input %q",
			decisionFlowChainIncomplete, decisionFlowSecondInput))
	}
	if !requestCarriesToolResult(decisionFlowSecondCall)(second[1]) {
		violations = append(violations, fmt.Sprintf("%s: the last decision of the second turn does not carry the subagent result of %q",
			decisionFlowResultDropped, decisionFlowSecondCall))
	}
	return violations
}

// decisionFlowBusyViolations reports the notice that opened beside the turn in
// flight: the driver must retain it until that turn ends.
func decisionFlowBusyViolations(scene decisionFlowScene) []string {
	if scene.busyRequests == 0 && scene.busyInputs == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%s: the second notice opened %d decisions and committed %d input facts while the first follow-up was parked, want it retained until that turn ends",
		decisionFlowBusyNotRetained, scene.busyRequests, scene.busyInputs)}
}

// decisionFlowActionViolations reports every legal action that landed at the
// wrong address, never landed or landed twice, beside every action the matrix
// never declared.
func decisionFlowActionViolations(scene decisionFlowScene) []string {
	specs := decisionFlowExpectedActions(scene)
	declared := make(map[string]bool, len(specs))
	for _, spec := range specs {
		declared[spec.body] = true
	}
	var violations []string
	for _, action := range scene.actions {
		if !declared[action.body] {
			violations = append(violations, fmt.Sprintf("%s: the action %q at %+v belongs to no decision of the matrix",
				decisionFlowUnexpectedAction, action.body, action.target))
		}
	}
	for _, spec := range specs {
		landed := 0
		for _, action := range scene.actions {
			if action.body != spec.body {
				continue
			}
			landed++
			if action.target != spec.addr || !action.confirmed() {
				violations = append(violations, fmt.Sprintf("%s: the action %q landed at %+v (confirmed %v), want %+v",
					decisionFlowActionMisplaced, spec.body, action.target, action.confirmed(), spec.addr))
			}
		}
		switch {
		case landed == 0:
			violations = append(violations, fmt.Sprintf("%s: the action %q (%s) was never recorded",
				decisionFlowActionMissing, spec.body, spec.what))
		case landed > 1:
			violations = append(violations, fmt.Sprintf("%s: the action %q (%s) was recorded %d times, want exactly once",
				decisionFlowDuplicateAction, spec.body, spec.what, landed))
		}
	}
	return violations
}

// decisionFlowActionSpec declares one legal action of the matrix: the body, the
// address it belongs to and the decision that produces it.
type decisionFlowActionSpec struct {
	body string
	addr event.Address
	what string
}

// decisionFlowExpectedActions reports the actions the observed load declares:
// the two turn answers and the two notice follow-ups. A send declares none: it
// answers its caller and reaches no channel.
func decisionFlowExpectedActions(scene decisionFlowScene) []decisionFlowActionSpec {
	first, second := scene.turns[0], scene.turns[1]
	specs := make([]decisionFlowActionSpec, 0, 4)
	specs = append(specs,
		decisionFlowActionSpec{body: first.finalBody, addr: first.addr, what: "first turn answer"},
		decisionFlowActionSpec{body: second.finalBody, addr: second.addr, what: "second turn answer"},
		decisionFlowActionSpec{body: scene.flows[0].followBody, addr: scene.flows[0].addr, what: "first notice follow-up"},
		decisionFlowActionSpec{body: scene.flows[1].followBody, addr: scene.flows[1].addr, what: "retained notice follow-up"},
	)
	return specs
}

// decisionFlowActionBodies renders the recorded action bodies in delivery order.
func decisionFlowActionBodies(actions []senderAction) []string {
	bodies := make([]string, 0, len(actions))
	for _, action := range actions {
		bodies = append(bodies, action.body)
	}
	return bodies
}

// cloneDecisionFlowScene copies one scene so a mutation never rewrites the load
// the positive anchor reads.
func cloneDecisionFlowScene(scene decisionFlowScene) decisionFlowScene {
	cloned := decisionFlowScene{
		actions:      append([]senderAction(nil), scene.actions...),
		active:       append([]event.Address(nil), scene.active...),
		drain:        scene.drain,
		declared:     append([]string(nil), scene.declared...),
		busyRequests: scene.busyRequests,
		busyInputs:   scene.busyInputs,
	}
	for _, request := range scene.requests {
		copied := request
		copied.Messages = append([]scriptedMessage(nil), request.Messages...)
		cloned.requests = append(cloned.requests, copied)
	}
	for _, request := range scene.entry {
		copied := request
		copied.Messages = append([]scriptedMessage(nil), request.Messages...)
		cloned.entry = append(cloned.entry, copied)
	}
	for _, turn := range scene.turns {
		copied := turn
		copied.calls = append([]decisionFlowCall(nil), turn.calls...)
		cloned.turns = append(cloned.turns, copied)
	}
	for _, flow := range scene.flows {
		copied := flow
		copied.facts = append([]event.Event(nil), flow.facts...)
		copied.inputFacts = append([]event.Event(nil), flow.inputFacts...)
		copied.request.Messages = append([]scriptedMessage(nil), flow.request.Messages...)
		cloned.flows = append(cloned.flows, copied)
	}
	return cloned
}

// decisionFlowWithoutNoticeInput returns the load a busy notice dropped in the
// driver would leave: the notice never generates a new user-input fact.
func decisionFlowWithoutNoticeInput(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	mutated.flows[1].inputFacts = nil
	return mutated
}

// decisionFlowForeignInvocation returns the load a continuation that opened a
// new invocation would leave: the turn's answer leaves the range its input
// committed under.
func decisionFlowForeignInvocation(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	mutated.turns[0].outputFact.InvocationID = "inv-decision-flow-foreign"
	return mutated
}

// decisionFlowSecondNoticeInput returns the load one notice consumed twice
// would leave: a second user-input fact and a second user copy in the request.
func decisionFlowSecondNoticeInput(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	flow := &mutated.flows[0]
	if len(flow.inputFacts) == 1 {
		duplicate := flow.inputFacts[0]
		duplicate.Key = "decision-flow-second-notice-input"
		duplicate.Type = event.TypeExternalInput
		flow.inputFacts = append(flow.inputFacts, duplicate)
	}
	flow.request.Messages = append(flow.request.Messages, scriptedMessage{Role: string(model.RoleUser), Content: flow.material})
	return mutated
}

// decisionFlowWithoutSubagentResult returns the load a lost subagent result would leave: the final decision of the first turn does not read it.
func decisionFlowWithoutSubagentResult(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	mutated.entry[2].Messages = decisionFlowMessagesWithoutToolResult(mutated.entry[2].Messages, decisionFlowFirstCall)
	return mutated
}

// decisionFlowDuplicatedAction returns the load a repeated delivery would
// leave: the first turn's answer reaches the sender twice.
func decisionFlowDuplicatedAction(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	for _, action := range mutated.actions {
		if action.body == mutated.turns[0].finalBody {
			mutated.actions = append(mutated.actions, action)
			break
		}
	}
	return mutated
}

// decisionFlowOpenedWhileBusy returns the load a notice firing beside the turn
// in flight would leave: the busy window already holds a decision and its input.
func decisionFlowOpenedWhileBusy(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	mutated.busyRequests = 1
	return mutated
}

// decisionFlowMissingAction returns the load a dropped follow-up action would
// leave: the retained notice's answer never reaches the sender.
func decisionFlowMissingAction(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	mutated.actions = decisionFlowActionsWithout(mutated.actions, mutated.flows[1].followBody)
	return mutated
}

// decisionFlowMisplacedAction returns the load a crossed delivery would leave:
// the retained notice's answer lands at the other session.
func decisionFlowMisplacedAction(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	for index := range mutated.actions {
		if mutated.actions[index].body == mutated.flows[1].followBody {
			mutated.actions[index].target = mutated.turns[0].addr
			break
		}
	}
	return mutated
}

// decisionFlowWithUnexpectedAction returns the load an undeclared delivery
// would leave: an action no decision of the matrix produces.
func decisionFlowWithUnexpectedAction(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	mutated.actions = append(mutated.actions, senderAction{target: mutated.turns[0].addr, body: "the undeclared flow action"})
	return mutated
}

// decisionFlowCrossedFlows returns the load two crossed notice flows would
// leave: each follow-up request carries the other session's input.
func decisionFlowCrossedFlows(scene decisionFlowScene) decisionFlowScene {
	mutated := cloneDecisionFlowScene(scene)
	mutated.flows[0].request.Messages = decisionFlowSwapUserCopy(mutated.flows[0].request.Messages, mutated.flows[0].input, mutated.flows[1].input)
	mutated.flows[1].request.Messages = decisionFlowSwapUserCopy(mutated.flows[1].request.Messages, mutated.flows[1].input, mutated.flows[0].input)
	return mutated
}

// decisionFlowMessagesWithoutToolResult reports the messages of one request
// without the result of the named call.
func decisionFlowMessagesWithoutToolResult(messages []scriptedMessage, callID string) []scriptedMessage {
	kept := make([]scriptedMessage, 0, len(messages))
	for _, message := range messages {
		if message.ToolCallID == callID {
			continue
		}
		kept = append(kept, message)
	}
	return kept
}

// decisionFlowActionsWithout reports the recorded actions without the body.
func decisionFlowActionsWithout(actions []senderAction, body string) []senderAction {
	kept := make([]senderAction, 0, len(actions))
	for _, action := range actions {
		if action.body == body {
			continue
		}
		kept = append(kept, action)
	}
	return kept
}

// decisionFlowSwapUserCopy reports the messages of one request with the first
// user copy of one input replaced by another.
func decisionFlowSwapUserCopy(messages []scriptedMessage, from, to string) []scriptedMessage {
	swapped := make([]scriptedMessage, 0, len(messages))
	swappedOne := false
	for _, message := range messages {
		if !swappedOne && message.Role == string(model.RoleUser) && strings.TrimSpace(message.Content) == from {
			message.Content = to
			swappedOne = true
		}
		swapped = append(swapped, message)
	}
	return swapped
}

// runDecisionFlowPositive reads the shared load back: the send result drives the
// next decision of the original invocation, that decision issues the subagent
// call, the busy notice is retained and continued after the in-flight turn
// ends, and every legal action is delivered once in the declared order.
func runDecisionFlowPositive(t *testing.T, scene decisionFlowScene) {
	t.Helper()
	if violations := decisionFlowViolations(scene); len(violations) != 0 {
		t.Fatalf("the observed decision-flow matrix left violations: %v", violations)
	}
	if got := decisionFlowActionBodies(scene.actions); !reflect.DeepEqual(got, scene.declared) {
		t.Fatalf("delivered bodies = %q, want the declared flow %q", got, scene.declared)
	}
	first := scene.turns[0]
	if got := first.turn.DecisionCount; got != 3 {
		t.Fatalf("the first turn ran %d decisions, want the send, the continuation after its result and the answer after the subagent acceptance", got)
	}
	carriesSend := requestCarriesToolResult(decisionFlowSendCall)(scene.entry[1])
	issuesWorker := reproductionCarriesToolCall(scene.entry[2], "worker", decisionFlowFirstCall, decisionFlowFirstWorkerIn)
	if !carriesSend || !issuesWorker {
		t.Fatalf("the decision after the send carried the send result %v and the next one issued the subagent call %v, want both: %+v / %+v",
			carriesSend, issuesWorker, scene.entry[1].Messages, scene.entry[2].Messages)
	}
	if first.outputFact.InvocationID != first.turn.InvocationID ||
		first.calls[0].fact.InvocationID != first.turn.InvocationID {
		t.Fatalf("the first turn ranged [answer %q, send result %q], want the segment range %q the decisions continued in",
			first.outputFact.InvocationID, first.calls[0].fact.InvocationID, first.turn.InvocationID)
	}
	if first.inputFact.InvocationID == first.turn.InvocationID || first.inputFact.InvocationID == "" {
		t.Fatalf("the committed input ranged %q beside the turn range %q, want the submitter identity the entry handed it",
			first.inputFact.InvocationID, first.turn.InvocationID)
	}
	if scene.flows[0].requestIndex >= scene.flows[1].requestIndex {
		t.Fatalf("the in-flight follow-up request index = %d and the retained one = %d, want the retained notice continued after the in-flight turn",
			scene.flows[0].requestIndex, scene.flows[1].requestIndex)
	}
	if scene.flows[0].noticeFact.InvocationID == scene.flows[1].noticeFact.InvocationID {
		t.Fatalf("the two notice flows committed into the same range %q, want one range each", scene.flows[0].noticeFact.InvocationID)
	}
	if scene.busyRequests != 0 || scene.busyInputs != 0 {
		t.Fatalf("the busy window held %d decisions and %d input facts, want the notice retained in the driver",
			scene.busyRequests, scene.busyInputs)
	}
	if got := continuationNoticeFactCount(scene.flows[1].facts, scene.flows[1].settle.TaskID); got != 1 {
		t.Fatalf("the retained notice committed %d feedback facts in its session, want exactly one", got)
	}
}

// runDecisionFlowNegative requires every forbidden shape to fail by name: the
// observed load passes the same checks, each mutated load leaves exactly the
// one shape it introduces, and the script departures of the 03.1 harness reject
// a request outside the script, a step never reached and a step reached twice.
func runDecisionFlowNegative(t *testing.T, scene decisionFlowScene) {
	t.Helper()
	if violations := decisionFlowViolations(scene); len(violations) != 0 {
		t.Fatalf("the observed load already leaves the matrix: %v", violations)
	}
	cases := []struct {
		name    string
		mutated decisionFlowScene
		kind    string
	}{
		{"busy_notice_dropped", decisionFlowWithoutNoticeInput(scene), decisionFlowNoticeDropped},
		{"continuation_new_invocation", decisionFlowForeignInvocation(scene), decisionFlowNewInvocation},
		{"notice_second_input", decisionFlowSecondNoticeInput(scene), decisionFlowNoticeSecondInput},
		{"subagent_result_dropped", decisionFlowWithoutSubagentResult(scene), decisionFlowResultDropped},
		{"busy_notice_not_retained", decisionFlowOpenedWhileBusy(scene), decisionFlowBusyNotRetained},
		{"action_duplicated", decisionFlowDuplicatedAction(scene), decisionFlowDuplicateAction},
		{"action_missing", decisionFlowMissingAction(scene), decisionFlowActionMissing},
		{"action_misplaced", decisionFlowMisplacedAction(scene), decisionFlowActionMisplaced},
		{"action_unexpected", decisionFlowWithUnexpectedAction(scene), decisionFlowUnexpectedAction},
		{"flows_crossed", decisionFlowCrossedFlows(scene), decisionFlowCrossed},
	}
	for _, rejected := range cases {
		kinds := identityMatrixViolationKinds(decisionFlowViolations(rejected.mutated))
		if len(kinds) != 1 || kinds[0] != rejected.kind {
			t.Fatalf("the %s load left the violations %v, want exactly the one %q", rejected.name, kinds, rejected.kind)
		}
	}
	decisionFlowScriptDepartures(t, scene)
}

// decisionFlowScriptDepartures replays the observed entry requests over the
// 03.1 harness: a request outside the script, a declared step the load never
// reaches and a step reached twice each fail under their own violation kind.
func decisionFlowScriptDepartures(t *testing.T, scene decisionFlowScene) {
	t.Helper()
	if len(scene.entry) < 2 {
		t.Fatalf("the observed load served %d entry requests, want at least the two the departures replay", len(scene.entry))
	}

	unexpected := newIntegrityScript()
	unexpected.expect("model-entry", "the first flow input", requestTail(decisionFlowFirstInput)).
		answers(scriptedAnswerOf(decisionFlowFirstFinal))
	if _, err := unexpected.decide(scene.entry[0]); err != nil {
		t.Fatalf("the declared first request was refused: %v", err)
	}
	if _, err := unexpected.decide(scene.entry[1]); err == nil {
		t.Fatalf("the request outside the script was answered, want the refusal")
	}
	if got := unexpected.audit(); len(got) != 1 || got[0].kind != integrityUnexpected {
		t.Fatalf("the request outside the script left %v, want exactly the one %q", got, integrityUnexpected)
	}

	missing := newIntegrityScript()
	missing.expect("model-entry", "the first flow input", requestTail(decisionFlowFirstInput)).
		answers(scriptedAnswerOf(decisionFlowFirstFinal))
	missing.expect("model-entry", "a decision the load never reached", requestTail("a user input the load never sent")).
		answers(scriptedAnswerOf(decisionFlowFirstFinal))
	if _, err := missing.decide(scene.entry[0]); err != nil {
		t.Fatalf("the declared first request was refused: %v", err)
	}
	if got := missing.audit(); len(got) != 1 || got[0].kind != integrityMissing {
		t.Fatalf("the declared but unreached step left %v, want exactly the one %q", got, integrityMissing)
	}

	duplicate := newIntegrityScript()
	duplicate.expect("model-entry", "the first flow input", requestTail(decisionFlowFirstInput)).
		answers(scriptedAnswerOf(decisionFlowFirstFinal))
	duplicate.expect("model-entry", "the decision after the send", requestCarriesToolResult(decisionFlowSendCall)).
		answers(scriptedAnswerOf(decisionFlowFirstFinal))
	if _, err := duplicate.decide(scene.entry[0]); err != nil {
		t.Fatalf("the declared first request was refused: %v", err)
	}
	if _, err := duplicate.decide(scene.entry[1]); err != nil {
		t.Fatalf("the declared continuation request was refused: %v", err)
	}
	if _, err := duplicate.decide(scene.entry[0]); err == nil {
		t.Fatalf("the repeated request of an answered step was answered, want the refusal")
	}
	if got := duplicate.audit(); len(got) != 1 || got[0].kind != integrityDuplicate {
		t.Fatalf("the repeated request left %v, want exactly the one %q", got, integrityDuplicate)
	}
}

// runDecisionFlowConsumer reads the load back from the formal entries: Boot
// established the two user sessions, Host.Invoke drove every request the
// provider served, the two notice follow-ups are the tail of the entry
// decisions, and the formal close reported every obligation settled.
func runDecisionFlowConsumer(t *testing.T, scene decisionFlowScene) {
	t.Helper()
	if len(scene.requests) != 9 {
		t.Fatalf("the provider served %d requests, want the seven entry decisions and the two worker decisions", len(scene.requests))
	}
	models := map[string]int{}
	for _, request := range scene.requests {
		models[request.Model]++
	}
	if len(models) != 2 || models["model-entry"] != 7 || models["model-worker"] != 2 {
		t.Fatalf("the served requests named the models %v, want seven entry decisions and two worker decisions", models)
	}
	if len(scene.active) != 2 {
		t.Fatalf("the boot assembly established %d user sessions %+v, want the two the matrix drives", len(scene.active), scene.active)
	}
	for index, request := range scene.requests {
		if request.Index != index {
			t.Fatalf("the request served at position %d carries the ordinal %d, want the provider's own sequence", index, request.Index)
		}
	}
	if len(scene.entry) != 7 {
		t.Fatalf("the entry agent served %d requests, want the seven decisions the matrix drives", len(scene.entry))
	}
	if !requestTail(scene.turns[0].input)(scene.entry[0]) || !requestTail(scene.turns[1].input)(scene.entry[3]) {
		t.Fatalf("the entry windows do not begin on the two user inputs: [%q, %q]",
			requestTailOf(scene.entry[0]), requestTailOf(scene.entry[3]))
	}
	if scene.entry[5].Index != scene.flows[0].requestIndex || scene.entry[6].Index != scene.flows[1].requestIndex {
		t.Fatalf("the entry tail carries the ordinals [%d %d], want the two follow-up requests at %d and %d",
			scene.entry[5].Index, scene.entry[6].Index, scene.flows[0].requestIndex, scene.flows[1].requestIndex)
	}
	for _, flow := range scene.flows {
		if len(flow.inputFacts) != 1 {
			t.Fatalf("the notice flow of %s committed %d new user-input facts, want the one the formal entry observed",
				flow.addr.SessionID, len(flow.inputFacts))
		}
		if flow.noticeFact.Owner != flow.addr || flow.inputFacts[0].Owner != flow.addr || flow.outputFact.Owner != flow.addr {
			t.Fatalf("the notice flow of %s landed outside its session: fact %+v input %+v output %+v",
				flow.addr.SessionID, flow.noticeFact.Owner, flow.inputFacts[0].Owner, flow.outputFact.Owner)
		}
		if flow.request.Model != "model-entry" {
			t.Fatalf("the follow-up request of %s named model %q, want the entry identity", flow.addr.SessionID, flow.request.Model)
		}
	}
	if !scene.drain.Drained || len(scene.drain.Unsettled) != 0 {
		t.Fatalf("the formal close reported %+v, want every obligation settled at the drain point", scene.drain)
	}
	for index, action := range scene.actions {
		if !action.confirmed() {
			t.Fatalf("the action %d %q was not confirmed: %+v", index, action.body, action)
		}
		if action.target != scene.turns[0].addr && action.target != scene.turns[1].addr {
			t.Fatalf("the action %d %q landed at %+v, want one of the two user sessions", index, action.body, action.target)
		}
	}
}

// requestTailOf renders the last message of one request as the line a failed
// check reads.
func requestTailOf(request scriptedRequest) string {
	if len(request.Messages) == 0 {
		return ""
	}
	last := request.Messages[len(request.Messages)-1]
	return last.Role + ": " + last.Content
}

// TestEventIntegrityFormalAssembly drives the default root assembly: the bare host document beside the formal sender option is the whole wiring a deployment adds.
// - One user turn over that assembly delivers its final answer through the assembled face exactly once, leaving no obligation open.
// - negative: a document granting no send tool boots and the user turn is refused at Host.Invoke.
// - negative: a document granting send is refused by Boot under the same named error, so the Boot gate cannot stand in for the invocation gate.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestEventIntegrityFormalAssembly(t *testing.T) {
	t.Run("formal_boot_delivers_turn", runFormalBootDeliversTurn)
	t.Run("boots_beside_no_sender", runBootsBesideNoSender)
}

// formalAssemblyAddr is the explicit user address every formal-assembly check
// drives: the entry identity the bare document declares beside one session.
var formalAssemblyAddr = event.Address{AgentID: "user", SessionID: "session-event-integrity-formal-assembly"}

// bootFormalAssemblyHost boots the default assembly over the bare host
// document: no tool is declared, no option is added beyond the formal sender,
// so what the check reads is the wiring Boot establishes on its own.
func bootFormalAssemblyHost(t *testing.T, provider *scriptedProvider, sender assembly.MessageSender) *assembly.Host {
	t.Helper()
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the formal assembly host: %v", err)
	}
	return host
}

// runFormalBootDeliversTurn requires the default assembly to carry one user
// turn end to end: the provider answers the input, the assembled run commits
// the assistant output, and deliverFresh hands exactly that body to the sender
// the deployment attached. The drain point then reports every formal obligation
// settled, so the turn leaves nothing owed behind it.
func runFormalBootDeliversTurn(t *testing.T) {
	reproductionEnv(t)
	const (
		input  = "answer the formal assembly turn"
		answer = "the formal assembly answer"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the entry answers the formal assembly turn", requestTail(input)).
		answers(scriptedAnswerOf(answer)).
		delivers(answer)
	provider.decide = script.decide

	sender := newDeliverySender(nil)
	host := bootFormalAssemblyHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	turn, err := driveIntegrityTurn(ctx, host, formalAssemblyAddr, input)
	if err != nil {
		t.Fatalf("the formal assembly turn ended by error: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 1 {
		t.Fatalf("the formal assembly turn = (%v, %d decisions), want the completed turn of its one decision",
			turn.Terminal, turn.DecisionCount)
	}

	if got := sender.bodies(); !reflect.DeepEqual(got, []string{answer}) {
		t.Fatalf("delivered bodies = %q, want the one final answer %q", got, answer)
	}
	action := deliveryActionOf(t, sender, answer)
	deliveryRequireAddress(t, action, formalAssemblyAddr)
	deliveryRequireConfirmed(t, action)
	if action.framed {
		t.Fatalf("the turn answer %q was delivered beside the call frame %+v; a turn answer carries no call identity",
			answer, action.frame)
	}

	fact := deliveryAssistantFact(t, host, formalAssemblyAddr, answer)
	if fact.InvocationID != turn.InvocationID {
		t.Fatalf("the delivered answer fact invocation = %q, want the turn range %q", fact.InvocationID, turn.InvocationID)
	}

	requests := provider.servedRequests()
	if len(requests) != 1 {
		t.Fatalf("the provider served %d requests, want the one decision of the turn", len(requests))
	}
	if requests[0].Model != "model-entry" {
		t.Fatalf("the turn request named model %q, want the entry document model", requests[0].Model)
	}
	for _, tool := range requests[0].Tools {
		if tool == "send" {
			t.Fatalf("the bare document declares no send tool, yet the request carried %q: the default assembly must not invent a channel face", tool)
		}
	}

	if got := script.actionBodies(); !reflect.DeepEqual(got, []string{answer}) {
		t.Fatalf("the script declared the outbound bodies %q, want the one final answer", got)
	}
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the formal assembly script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// runBootsBesideNoSender requires a host without an outbound sender to refuse
// by name at the gate that owns the refusal, never to invent a channel face and
// never to reach a provider: the bare document boots and the user turn is
// refused at Host.Invoke, and a document that grants send boots under the same
// terms, because a send reaches no channel and demands no sender (design
// DEL-6). The refusal stays at the invocation gate, where the turn would have
// produced output nothing can deliver.
func runBootsBesideNoSender(t *testing.T) {
	reproductionEnv(t)
	const input = "answer the turn no sender can deliver"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newScriptedProvider(t)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	host, err := assembly.Boot(configPath)
	if err != nil {
		t.Fatalf("boot the bare document beside no sender: %v", err)
	}
	if _, err := driveIntegrityTurn(ctx, host, formalAssemblyAddr, input); !errors.Is(err, assembly.ErrHostSenderMissing) {
		t.Fatalf("the user turn beside no sender ended by %v, want the named refusal %v", err, assembly.ErrHostSenderMissing)
	}
	if got := len(provider.recorded()); got != 0 {
		t.Fatalf("the provider served %d requests beside a refused user turn, want zero", got)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host booted beside no sender: %v", err)
	}

	grantedProvider := newScriptedProvider(t)
	grantedRoot := t.TempDir()
	grantedPath := writeScriptedHostDocument(t, grantedRoot, grantedProvider.endpoint(), "model-entry")
	writeHostDocumentFile(t, grantedPath, scriptedHostDocumentWithAgent(grantedRoot, grantedProvider.endpoint(), "model-entry",
		"    tools:\n"+
			"      - { kind: tool, id: send }\n"))
	granted, err := assembly.Boot(grantedPath)
	if err != nil {
		t.Fatalf("boot the document that grants the %q tool beside no sender: %v", "send", err)
	}
	if _, err := driveIntegrityTurn(ctx, granted, formalAssemblyAddr, input); !errors.Is(err, assembly.ErrHostSenderMissing) {
		t.Fatalf("the user turn beside the granted send tool ended by %v, want the named refusal %v", err, assembly.ErrHostSenderMissing)
	}
	if got := len(grantedProvider.recorded()); got != 0 {
		t.Fatalf("the provider served %d requests beside a refused user turn, want zero", got)
	}
	if err := granted.Close(ctx); err != nil {
		t.Fatalf("close the host booted beside the granted send tool: %v", err)
	}
}

// bootSmallWindowHost boots the entry document of one session chain whose model
// window is small enough that the age fold runs inside the check: the entry
// agent keeps the send tool beside the declared worker agent, and the 256-token
// window folds the projection after a handful of turns instead of after a long
// session. The window is the replay window the chain measures its material
// against, never a response length. The fold settings are the ones the 03.11
// long run declares — the 0.3 share of the window and one recent task kept
// whole — so a new segment start folds the segment before it, the accumulation
// rhythm the long-run failure was found under.
func bootSmallWindowHost(t *testing.T, provider *scriptedProvider, sender recordingSender) *assembly.Host {
	t.Helper()
	return bootIntegrityHostWithAgents(t, provider, sender,
		"  user:\n"+
			"    model: model-entry\n"+
			"    max_tokens: 256\n"+
			"    compress_threshold: 0.3\n"+
			"    keep_recent_tasks: 1\n"+
			"    tools:\n"+
			"      - { kind: tool, id: send }\n"+
			"      - { kind: agent, agent: worker }\n"+
			"  worker:\n"+
			"    model: model-worker\n")
}

// requestMentionsSettleNotice matches the request the follow-up turn of a
// settled task drives, whatever shape the notice reached it in: the full notice
// material names the task and its terminal, while the degraded summary carries
// the terminal word alone. The match is deliberately loose, so a check reads the
// shape the notice actually had instead of waiting out a script that never
// matches.
func requestMentionsSettleNotice(request scriptedRequest) bool {
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) && strings.Contains(message.Content, "settled") {
			return true
		}
	}
	return false
}

// smallWindowFollowUpRequest waits for the request the settled notice drove and
// answers its user material, in request order.
func smallWindowFollowUpRequest(t *testing.T, provider *scriptedProvider, timeout time.Duration) (scriptedRequest, []string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var served int
	for {
		requests := provider.servedRequests()
		served = len(requests)
		for _, request := range requests {
			if requestMentionsSettleNotice(request) {
				out := make([]string, 0, len(request.Messages))
				for _, message := range request.Messages {
					out = append(out, message.Role+": "+message.Content)
				}
				return request, out
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no request of the settled notice arrived within %s; model requests = %d", timeout, served)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// runNoticeVisibleUnderSmallWindow fills one session chain past the 256-token
// window until the age fold rewrote its projection, then delegates one task and
// reads the follow-up request its settled notice drives. The notice identity
// and the result body must reach the model there even though the fold shrank the
// sequence the boundary was measured on and the budget alone would present the
// notice as its one-word summary.
//
// - The notice lands while the entry segment still flies when the settled result and the notice reach the same pull: that one request carries both sources, so the segment answers once with the body the settled result drives.
// - A notice the closed segment never saw opens a range of its own and answers with the follow-up body; the shape is read off the request.
func runNoticeVisibleUnderSmallWindow(t *testing.T) {
	const (
		fillOne     = "the first note of the small window session"
		fillTwo     = "the second note of the small window session"
		fillThree   = "the third note of the small window session"
		input       = "delegate the small window task"
		workerInput = "compute the small window total"
		workerOut   = "the small window worker reports 77"
		accepted    = "the small window task was accepted"
		follow      = "the small window notice reached the user"
		callID      = "call-continuation-small-window"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	for index, fill := range []string{fillOne, fillTwo, fillThree} {
		script.expect("model-entry", fmt.Sprintf("the fill turn %d", index+1), requestTail(fill)).
			answers(scriptedAnswerOf(fmt.Sprintf("answer for the fill turn %d", index+1))).
			delivers(fmt.Sprintf("answer for the fill turn %d", index+1))
	}
	script.expect("model-entry", "the entry delegates the small window task", requestTail(input)).
		answers(settleToolCallAnswer("worker", callID, `{"input":"`+workerInput+`"}`))
	accept := script.expect("model-entry", "the entry accepts the small window task", requestCarriesToolResult(callID)).
		answers(scriptedAnswerOf(accepted)).
		delivers(accepted).
		park()
	worker := expectContinuationWorker(script, "the small window task", workerInput, workerOut).park()
	script.expect("model-entry", "the small window notice fires its follow-up", requestMentionsSettleNotice).
		answers(scriptedAnswerOf(follow)).
		delivers(follow).
		optional()
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootSmallWindowHost(t, provider, sender)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-notice-small-window"}

	for _, fill := range []string{fillOne, fillTwo, fillThree} {
		driveContinuationTurn(t, ctx, host, addr, fill, 1)
	}
	entries, registered := host.Run().ReplayEntries(addr.SessionID)
	if !registered {
		t.Fatalf("the session chain of the small window registered no projection, so no fold could run")
	}
	synthetic := 0
	for _, entry := range entries {
		if replay.SyntheticKey(entry.Key) {
			synthetic++
		}
	}
	if synthetic == 0 {
		t.Fatalf("the 256-token window folded nothing over the three fill turns: projection carries %d entries, none synthetic", len(entries))
	}

	driven := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, input)
		driven <- integrityOutcome{turn: turn, err: err}
	}()
	script.await(t, worker)
	script.release(worker)
	script.await(t, accept)
	script.release(accept)
	got := awaitIntegrityDrive(t, driven, "the small window delegation turn")
	if got.err != nil {
		t.Fatalf("the small window delegation turn ended by error: %v", got.err)
	}
	if got.turn.Terminal != model.KindCompleted || got.turn.DecisionCount != 2 {
		t.Fatalf("the small window delegation turn = (%v, %d decisions), want the completed turn of 2 decisions",
			got.turn.Terminal, got.turn.DecisionCount)
	}
	settle, notice, material := continuationSettle(t, host, addr, workerOut)

	request, materials := smallWindowFollowUpRequest(t, provider, 30*time.Second)
	if !requestCarriesSettleNotice(material)(request) {
		t.Fatalf("the follow-up request of the settled task %s did not carry the notice identity %s and the result body %q; its user material was %q. The notice fact is in the chain, so the loss is the visibility the window pressure left, not the ledger",
			settle.TaskID, notice.TaskID, material, materials)
	}
	continuationSingleCopy(t, request, material, "the small window follow-up request")

	facts := host.Run().SessionFacts(addr)
	followFact := continuationNoticeFact(t, facts, settle.TaskID)
	if want := agent.NoticeFactKey(followFact.InvocationID, notice); followFact.Key != want {
		t.Fatalf("the follow-up fact key = %s, want the notice identity key %s", followFact.Key, want)
	}
	batched := requestCarriesToolResult(callID)(request)
	reproductionWaitForBody(t, sender, continuationNoticeAnswerBody(follow, accepted, batched), 30*time.Second)
	continuationAnswerDelivered(t, sender, addr, accepted, follow, batched)
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the small window script left violations: %v", violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// projectionlessState is one declaration shape of the session chain under
// check: the window line the entry agent declares, and the mapped-sequence
// registration that shape reads. The window is the presentation budget and the
// fold measure, never whether the chain replays — every shape registers, and
// the assertion pins that shape against a regression that gates registration.
type projectionlessState struct {
	name       string
	window     string
	projection bool
}

// bootProjectionlessHost boots the entry document of one session chain whose
// window declaration is the given line verbatim: an empty line declares no
// window — the no-budget shape — and the loop reads the notice material from
// the committed facts in every shape.
func bootProjectionlessHost(t *testing.T, provider *scriptedProvider, sender recordingSender, window string) *assembly.Host {
	t.Helper()
	return bootIntegrityHostWithAgents(t, provider, sender,
		"  user:\n"+
			"    model: model-entry\n"+
			window+
			"    tools:\n"+
			"      - { kind: tool, id: send }\n"+
			"      - { kind: agent, agent: worker }\n"+
			"  worker:\n"+
			"    model: model-worker\n")
}

// runNoticeInputWithoutProjection drives one delegated task under each legal
// window declaration and requires the follow-up decision of the settled task to
// read the notice material in all of them. The chain shape is asserted beside
// the drive, so the check reads the assembly it names instead of inferring it:
// every declaration registers its mapped sequence — the window names the
// presentation budget and the fold measure, never whether the chain replays —
// and the assertion pins that against a regression that gates registration
// again.
func runNoticeInputWithoutProjection(t *testing.T) {
	states := []projectionlessState{
		{name: "no_declared_window", projection: true},
		{name: "zero_window", window: "    max_tokens: 0\n", projection: true},
		{name: "declared_window", window: "    max_tokens: 256\n", projection: true},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			runNoticeInputUnderState(t, state)
		})
	}
}

// runNoticeInputUnderState drives the delegation and the settled notice of one
// state, and requires the notice identity and its result body to reach the
// follow-up request exactly once.
//
// - The notice lands while the entry segment still flies when the settled result and the notice reach the same pull: that one request carries both sources, so the segment answers once with the body the settled result drives.
// - A notice the closed segment never saw opens a range of its own and answers with the follow-up body; the shape is read off the request, never off a timing guess.
func runNoticeInputUnderState(t *testing.T, state projectionlessState) {
	const (
		input       = "delegate the projectionless continuation task"
		workerInput = "compute the projectionless total"
		workerOut   = "the projectionless worker reports 19"
		accepted    = "the projectionless task was accepted"
		follow      = "the projectionless notice reached the user"
		callID      = "call-continuation-projectionless"
	)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	expectContinuationDelegation(script, "the projectionless task", input, callID, workerInput, accepted)
	worker := expectContinuationWorker(script, "the projectionless task", workerInput, workerOut)
	expectContinuationFollowUp(script, "the projectionless continuation", workerOut, follow)
	provider.decide = script.decide

	sender := newRecordingSender()
	host := bootProjectionlessHost(t, provider, sender, state.window)
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: event.SessionID("session-continuation-" + state.name)}

	driveContinuationTurn(t, ctx, host, addr, input, 2)
	script.await(t, worker)
	script.release(worker)

	settle, notice, material := continuationSettle(t, host, addr, workerOut)
	if _, registered := host.Run().ReplayEntries(addr.SessionID); registered != state.projection {
		t.Fatalf("the %s chain registered a projection = %t, want %t: the state under check is the assembly the declaration names",
			state.name, registered, state.projection)
	}

	request := projectionlessFollowUpRequest(t, provider, script, material, state.name)
	batched := requestCarriesToolResult(callID)(request)
	reproductionWaitForBody(t, sender, continuationNoticeAnswerBody(follow, accepted, batched), 30*time.Second)

	continuationSingleCopy(t, request, material, "the follow-up request of the "+state.name+" state")
	continuationHistory(t, request, input)
	continuationAnswerDelivered(t, sender, addr, accepted, follow, batched)

	facts := host.Run().SessionFacts(addr)
	followFact := continuationNoticeFact(t, facts, settle.TaskID)
	if want := agent.NoticeFactKey(followFact.InvocationID, notice); followFact.Key != want {
		t.Fatalf("the follow-up fact key = %s, want the notice identity key %s", followFact.Key, want)
	}
	t.Logf("state=%s projection=%t notice=%s fact_key=%s request_index=%d material=%q",
		state.name, state.projection, notice.FactKey(), followFact.Key, request.Index, strings.SplitN(material, "\n", 2)[0])
	if violations := script.audit(); len(violations) != 0 {
		t.Fatalf("the projectionless script of the %s state left violations: %v", state.name, violations)
	}
	closeIntegrityHost(t, ctx, host)
}

// projectionlessFollowUpRequest waits for the one request of the follow-up range
// that carries the notice material, and reports it. A request that matched no
// declared step ends the wait at once with the material it was missing: the
// refusal is the shape a follow-up request without the notice material leaves,
// and waiting out the timeout would report it as a slow check instead of a lost
// read path.
func projectionlessFollowUpRequest(t *testing.T, provider *scriptedProvider, script *integrityScript, material, state string) scriptedRequest {
	t.Helper()
	material = strings.TrimSpace(material)
	deadline := time.Now().Add(30 * time.Second)
	for {
		requests := provider.servedRequests()
		carrying := reproductionRequestsCarrying(requests, material)
		switch {
		case len(carrying) == 1:
			return requests[carrying[0]]
		case len(carrying) > 1:
			t.Fatalf("requests carrying the notice material %q in the %s state = %v, want exactly the one follow-up decision",
				material, state, carrying)
		}
		select {
		case <-script.violated:
			t.Fatalf("the follow-up decision of the %s state carried no user input of the notice material %q, so the request matched no declared step; served requests = %d, user material of the last request = %q",
				state, material, len(requests), userMaterialOf(requests))
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("no request carried the notice material %q in the %s state within %s; served requests = %d",
				material, state, 30*time.Second, len(requests))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// userMaterialOf renders the user messages of the last served request, so a
// failed check reads what the follow-up decision actually saw.
func userMaterialOf(requests []scriptedRequest) []string {
	if len(requests) == 0 {
		return nil
	}
	last := requests[len(requests)-1]
	out := make([]string, 0, len(last.Messages))
	for _, message := range last.Messages {
		if message.Role == "user" {
			out = append(out, message.Content)
		}
	}
	return out
}
