// 交付闭合（事件化）域检查：结算事实族、同笔原子、重试分类、恢复、迁移、
// 感知/表达/兜底。每条检查只从链上事实、Host 公共入口与收件方
// （MessageSender）取数，不读私有实现细节。
//
// 契约: docs/FACT-VOCABULARY.md#families
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
// 契约: docs/FACT-VOCABULARY.md#families
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// deliveryTestAgent is the entry agent every delivery check drives.
const deliveryTestAgent = event.AgentID("agent-a")

// deliveryTestAddress names one complete address at the delivery test agent.
func deliveryTestAddress(session string) event.Address {
	return event.Address{AgentID: deliveryTestAgent, SessionID: event.SessionID(session)}
}

// deliveryTestHost builds the host face the checks drive: the assembled run, the
// published entry and an empty review table.
func deliveryTestHost(ctx context.Context, run *Run, sender MessageSender) *Host {
	entry := deliveryTestAgent
	if published := run.publishedEntry(); published != "" {
		entry = published
	}
	return &Host{
		ctx:               ctx,
		run:               run,
		entry:             entry,
		sender:            sender,
		pendingDeliveries: map[string]*heldDelivery{},
		userAddresses:     map[event.SessionID]event.Address{},
		userTurns:         map[event.Address]int{},
	}
}

// deliveryTestRequestSummary renders the message shapes of one request for a
// failure message without dumping a whole prompt.
func deliveryTestRequestSummary(request model.Request) string {
	_, summary := boundaryAnchorRequestChars(request)
	return summary
}

// TestSettlementRoleMapped pins the assembly side of the one projection registration.
//
// - The family const this package writes is the frozen word the projection maps, and the role it maps to is the one the state line is read under.
func TestSettlementRoleMapped(t *testing.T) {
	if deliverySettlementType != "delivery.settlement" {
		t.Fatalf("the assembly settlement family reads %q, want the frozen word delivery.settlement", deliverySettlementType)
	}
	role, err := agent.RoleOf(deliverySettlementType)
	if err != nil {
		t.Fatalf("the family this package writes maps to no role: %v", err)
	}
	if role != model.RoleUser {
		t.Fatalf("the family this package writes maps to role %q, want the user role its state line is read under", role)
	}
	if _, err := agent.RoleOf(deliverySettlementType + ".unregistered"); !errors.Is(err, agent.ErrProjectionTypeUnmapped) {
		t.Fatalf("an unregistered type answered %v, want ErrProjectionTypeUnmapped", err)
	}
}

// deliveryTestRequestContents renders every message of one request, role beside
// content, so a failed perception check shows what the model actually read.
func deliveryTestRequestContents(request model.Request) string {
	var builder strings.Builder
	for index, message := range request.Messages {
		builder.WriteString(fmt.Sprintf("[%d %s] %q; ", index, message.Role, message.Content))
	}
	return builder.String()
}

// deliveryTestAttempt is one outbound attempt the check's channel took.
type deliveryTestAttempt struct {
	Target  event.Address
	Content string
}

// deliveryTestSender is the outbound face the delivery checks hand the host: it
// records every attempt and answers each one with the ruling the check installed,
// so a transient failure, a pure timeout and a permanent refusal are the check's
// own words and nothing is invented by the harness.
type deliveryTestSender struct {
	mu        sync.Mutex
	answer    func(attempt int, target event.Address, content string) error
	attempts  []deliveryTestAttempt
	delivered []deliveryTestAttempt
}

// newDeliveryTestSender returns a channel that answers every attempt with the
// installed ruling and accepts whatever the ruling does not refuse.
func newDeliveryTestSender(answer func(attempt int, target event.Address, content string) error) *deliveryTestSender {
	return &deliveryTestSender{answer: answer}
}

// Send records one attempt and answers it with the installed ruling.
func (s *deliveryTestSender) Send(_ context.Context, target event.Address, content string) error {
	s.mu.Lock()
	attempt := len(s.attempts) + 1
	s.attempts = append(s.attempts, deliveryTestAttempt{Target: target, Content: content})
	answer := s.answer
	s.mu.Unlock()
	if answer != nil {
		if err := answer(attempt, target, content); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.delivered = append(s.delivered, deliveryTestAttempt{Target: target, Content: content})
	s.mu.Unlock()
	return nil
}

// attemptCount reports how often the channel was asked to take one body.
func (s *deliveryTestSender) attemptCount(content string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, attempt := range s.attempts {
		if attempt.Content == content {
			count++
		}
	}
	return count
}

// attemptsFor reports every attempt the channel took for one body.
func (s *deliveryTestSender) attemptsFor(content string) []deliveryTestAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]deliveryTestAttempt, 0, len(s.attempts))
	for _, attempt := range s.attempts {
		if attempt.Content == content {
			out = append(out, attempt)
		}
	}
	return out
}

// deliveredCount reports how many copies of one body the channel accepted.
func (s *deliveryTestSender) deliveredCount(content string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, attempt := range s.delivered {
		if attempt.Content == content {
			count++
		}
	}
	return count
}

// deliveredContents reports every body the channel accepted, in order.
func (s *deliveryTestSender) deliveredContents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.delivered))
	for _, attempt := range s.delivered {
		out = append(out, attempt.Content)
	}
	return out
}

// deliveryTestDefiniteError is the channel's definite refusal: the connection was
// refused before any body left, so the failure is known-bad, never unknown.
type deliveryTestDefiniteError struct{}

// Error renders the definite failure.
func (deliveryTestDefiniteError) Error() string {
	return "the outbound channel refused the connection before the send began"
}

// Timeout reports false: a refusal is not a timeout.
func (deliveryTestDefiniteError) Timeout() bool { return false }

// Temporary reports the refusal as retryable.
func (deliveryTestDefiniteError) Temporary() bool { return true }

// deliveryTestTimeoutError is the send that may have landed: the channel timed
// out after the body was handed to it, so the result is unknown.
type deliveryTestTimeoutError struct{}

// Error renders the unknown failure.
func (deliveryTestTimeoutError) Error() string {
	return "the outbound channel timed out after the send began"
}

// Timeout reports true: the result of the send cannot be told apart from a lost
// body.
func (deliveryTestTimeoutError) Timeout() bool { return true }

// Temporary reports the failure as retryable by the channel's own words, which is
// exactly why the classifier must not trust that word for a timeout.
func (deliveryTestTimeoutError) Temporary() bool { return true }

// deliveryTestInvoke drives one invocation through the host's public entry.
func deliveryTestInvoke(t *testing.T, ctx context.Context, host *Host, addr event.Address, input string) agent.Turn {
	t.Helper()
	turn, err := host.Invoke(ctx, InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: input}},
	})
	if err != nil {
		t.Fatalf("drive the delivery turn: %v", err)
	}
	return turn
}

// deliveryTestWaitFor polls one condition until it holds or the budget expires,
// and reports whether it held.
func deliveryTestWaitFor(budget time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(budget)
	for !condition() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

// deliveryTestOutputOf reports the agent_output fact one invocation committed.
func deliveryTestOutputOf(t *testing.T, run *Run, addr event.Address, invocation event.InvocationID) event.Event {
	t.Helper()
	for _, fact := range run.SessionFacts(addr) {
		if fact.Type == event.TypeAgentOutput && fact.InvocationID == invocation {
			return fact
		}
	}
	t.Fatalf("the chain of %s holds no agent_output of %s", addr.SessionID, invocation)
	return event.Event{}
}

// deliveryTestRuler is the review ruling the checks install: it holds what the
// check names and allows everything else.
type deliveryTestRuler struct {
	mu    sync.Mutex
	holds map[string]bool
}

// Rule holds a body the check named and allows every other one.
func (r *deliveryTestRuler) Rule(_ context.Context, delivery plugin.Delivery) (plugin.Disposition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.holds[delivery.Content] {
		return plugin.DispositionHold, nil
	}
	return plugin.DispositionAllow, nil
}

// TestDeliverySettlementFactFamily pins the eleventh fact family: one settlement fact per (delivery identity, state token).
//
// - The identity is derived from the fact key that carries the body, and the key is stable across processes.
// - A replayed write is answered without a second fact, and a different content under the same key is refused by the chain's own conflict rule.
func TestDeliverySettlementFactFamily(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-settlement-family")
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))

	delivery := plugin.Delivery{Content: "the body the settlement states", Lineage: "user", Target: addr}
	const identity = "delivery-fresh-2-settlement-family"
	if err := host.writeDeliverySettlement(ctx, addr, identity, deliveryStateSent, delivery, time.Time{}, nil); err != nil {
		t.Fatalf("write the settlement fact: %v", err)
	}
	key := deliverySettlementKey(identity, deliveryStateSent)
	if want := event.EventID("delivery." + identity + ".sent"); key != want {
		t.Fatalf("the settlement key = %q, want the deterministic %q", key, want)
	}
	backIdentity, backState, ok := deliveryIdentityOfSettlementKey(key)
	if !ok || backIdentity != identity || backState != deliveryStateSent {
		t.Fatalf("the settlement key splits into (%q, %q, %v), want (%q, %q, true)", backIdentity, backState, ok, identity, deliveryStateSent)
	}
	evt, err := run.CommittedEvent(ctx, key)
	if err != nil {
		t.Fatalf("read the committed settlement fact back: %v", err)
	}
	if evt.Type != deliverySettlementType {
		t.Fatalf("the settlement fact type = %q, want %q", evt.Type, deliverySettlementType)
	}
	settlement, ok := settlementOf(evt)
	if !ok || settlement.identity != identity || settlement.state != deliveryStateSent {
		t.Fatalf("the committed settlement fact reads as %+v (ok=%v), want identity %q state %q", settlement, ok, identity, deliveryStateSent)
	}
	if settlement.delivery.Target != addr || settlement.delivery.Lineage != "user" {
		t.Fatalf("the settlement fact names target/lineage %v/%q, want %v/%q", settlement.delivery.Target, settlement.delivery.Lineage, addr, "user")
	}
	if !strings.Contains(evt.Message.Content, deliveryStateSent) || !strings.Contains(evt.Message.Content, identity) {
		t.Fatalf("the settlement line = %q, want the state and the identity", evt.Message.Content)
	}

	before := boundaryAnchorSettlementFacts(run.SessionFacts(addr))
	if err := host.writeDeliverySettlement(ctx, addr, identity, deliveryStateSent, delivery, time.Time{}, nil); err != nil {
		t.Fatalf("replay the settlement fact: %v", err)
	}
	if after := boundaryAnchorSettlementFacts(run.SessionFacts(addr)); after != before {
		t.Fatalf("the replayed settlement write added a second fact: %d facts, want %d", after, before)
	}

	conflict := evt
	conflict.Message = model.Message{Role: model.RoleUser, Content: "a second line under one settlement identity"}
	if _, err := run.CommitToSession(ctx, addr, conflict); !errors.Is(err, memory.ErrFactKeyConflict) {
		t.Fatalf("the same settlement key accepted a second content: %v, want ErrFactKeyConflict", err)
	}
}

// TestObligationAtomicWithOutput pins the same-commit atomicity of the owed declaration.
//
// - The owed declaration rides on the agent_output fact that carries the body: one fact, one commit.
// - The identity the delivery settles under is derived from that fact's key, never minted beside it.
// - 已结算的事实不携带正文（义务已履行），但可读行仍指向同一份正文：身份 + 字节数，都是那条 agent_output 事实的量。
func TestObligationAtomicWithOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-obligation-atomic")
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, run, sender)
	turn := deliveryTestInvoke(t, ctx, host, addr, "drive the atomic obligation")

	output := deliveryTestOutputOf(t, run, addr, turn.InvocationID)
	if strings.TrimSpace(output.Message.Content) == "" {
		t.Fatalf("the output fact of %s carries no body: %+v", turn.InvocationID, output)
	}
	if output.Meta[deliveryOwedMeta] != "1" {
		t.Fatalf("the output fact does not declare the obligation: meta=%v", output.Meta)
	}
	if got := output.Meta[deliveryTargetAgentMeta]; got != string(addr.AgentID) {
		t.Fatalf("the declared target agent = %q, want %q", got, addr.AgentID)
	}
	if got := output.Meta[deliveryTargetSessionMeta]; got != string(addr.SessionID) {
		t.Fatalf("the declared target session = %q, want %q", got, addr.SessionID)
	}
	if strings.TrimSpace(output.Meta[deliveryLineageMeta]) == "" {
		t.Fatalf("the output fact declares no lineage: meta=%v", output.Meta)
	}
	identity := hostDeliveryFreshPrefix + string(output.Key)
	settled, ok := host.settlementStateOf(ctx, identity)
	if !ok || settled.state != deliveryStateSent {
		t.Fatalf("the obligation the output declared settled as %+v (ok=%v), want the sent state of %q", settled, ok, identity)
	}
	body := strings.TrimSpace(output.Message.Content)
	if !strings.Contains(settled.fact.Message.Content, identity) {
		t.Fatalf("the sent fact of %q reads %q, want the identity it settles", identity, settled.fact.Message.Content)
	}
	if want := fmt.Sprintf("(%d bytes)", len(body)); !strings.Contains(settled.fact.Message.Content, want) {
		t.Fatalf("the sent fact of %q reads %q, want the %q of the body it discharged", identity, settled.fact.Message.Content, want)
	}
	if delivered := sender.deliveredCount(strings.TrimSpace(output.Message.Content)); delivered != 1 {
		t.Fatalf("the body of the output fact reached the channel %d times, want 1", delivered)
	}
}

// TestDeliveryCommitFailureBlocksSend pins the other half of "两不立".
//
// - A commit the chain refuses leaves neither the body nor the account behind, and the turn answers as failed.
// - The recipient reads a notice instead of a body no account registered.
// - A later turn beside the healed boundary delivers normally, so the refusal is the commit's, not the account's.
func TestDeliveryCommitFailureBlocksSend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	addr := deliveryTestAddress("session-commit-failure")
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, run, sender)

	var mu sync.Mutex
	refuse := true
	refused := errors.New("the durable commit of the answer was refused by the storage tier")
	run.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		mu.Lock()
		refusing := refuse
		mu.Unlock()
		if refusing && evt.Type == event.TypeAgentOutput {
			return "", refused
		}
		return run.commitFactChecked(ctx, evt)
	})

	turn, err := host.Invoke(ctx, InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "drive the refused commit"}},
	})
	if err == nil && turn.Terminal != model.KindFailed {
		t.Fatalf("the turn beside a refused output commit ended as %v, want a failed turn", turn.Terminal)
	}
	if turn.Terminal != model.KindFailed {
		t.Fatalf("the turn beside a refused output commit ended as %v, want failed", turn.Terminal)
	}
	if bodies := generatedBodiesOf(run.SessionFacts(addr), turn.InvocationID); len(bodies) != 0 {
		t.Fatalf("the chain holds %d body/bodies beside a refused commit: %+v", len(bodies), bodies)
	}
	if settled := hostTestSettlements(t, host, addr); len(settled) != 0 {
		t.Fatalf("the account holds %d settlement fact(s) beside a refused commit: %+v", len(settled), settled)
	}
	if count := sender.deliveredCount("settled"); count != 0 {
		t.Fatalf("the channel took the body %d time(s) beside a refused commit, want 0", count)
	}
	notices := 0
	for _, content := range sender.deliveredContents() {
		if strings.TrimSpace(content) != "" && content != "settled" {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("the recipient read %d notice(s) beside a refused commit, want exactly 1: %q", notices, sender.deliveredContents())
	}

	mu.Lock()
	refuse = false
	mu.Unlock()
	healed := deliveryTestAddress("session-commit-healed")
	deliveryTestInvoke(t, ctx, host, healed, "drive the healed boundary")
	if count := sender.deliveredCount("settled"); count != 1 {
		t.Fatalf("the healed boundary delivered the body %d time(s), want 1", count)
	}
}

// TestUnknownResultNeverAutoRedriven pins the negative half of the classifier.
//
// - A pure timeout may have landed, so the account holds it as unknown and never resends it on its own.
// - The redrive the backoff would perform must not start; 退避窗口（1s）过后仍不得自动重发。
func TestUnknownResultNeverAutoRedriven(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-unknown-hold")
	sender := newDeliveryTestSender(func(attempt int, _ event.Address, _ string) error {
		if attempt == 1 {
			return deliveryTestTimeoutError{}
		}
		return nil
	})
	host := deliveryTestHost(ctx, run, sender)
	deliveryTestInvoke(t, ctx, host, addr, "drive the unknown result")

	identity := ""
	if !deliveryTestWaitFor(2*time.Second, func() bool {
		for id, st := range deliveryTestStates(host) {
			if st == heldStateOf(deliveryReasonUnknown) {
				identity = id
				return true
			}
		}
		return false
	}) {
		t.Fatalf("the timed-out delivery left no held(unknown) settlement fact: states=%v", deliveryTestStates(host))
	}
	time.Sleep(1500 * time.Millisecond)
	if attempts := sender.attemptCount("settled"); attempts != 1 {
		t.Fatalf("the timed-out body was attempted %d times, want exactly 1 (an unknown result is never auto-resent)", attempts)
	}
	if count := sender.deliveredCount("settled"); count != 0 {
		t.Fatalf("the timed-out body reached the channel %d time(s) on its own, want 0", count)
	}
	settled, ok := host.settlementStateOf(ctx, identity)
	if !ok || settled.state != heldStateOf(deliveryReasonUnknown) {
		t.Fatalf("the held obligation reads as %+v (ok=%v), want held(%s)", settled, ok, deliveryReasonUnknown)
	}
	if held := host.HeldDeliveries(); len(held) != 1 {
		t.Fatalf("the review table holds %d entr(ies), want the one unknown obligation: %+v", len(held), held)
	}
}

// TestRetryExhaustionFallsToHeld pins the bound of the redrive.
//
// - A channel that refuses every attempt receives the three retries, then the obligation is held as retry-exhausted.
// - retry-exhausted is a hold reason, never a settlement terminal of its own.
func TestRetryExhaustionFallsToHeld(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-retry-exhaustion")
	sender := newDeliveryTestSender(func(int, event.Address, string) error { return deliveryTestDefiniteError{} })
	host := deliveryTestHost(ctx, run, sender)
	host.deliveryBackoff = 5 * time.Millisecond
	deliveryTestInvoke(t, ctx, host, addr, "drive the exhaustion")

	identity := ""
	if !deliveryTestWaitFor(5*time.Second, func() bool {
		for id, st := range deliveryTestStates(host) {
			if st == heldStateOf(deliveryReasonRetryExhausted) {
				identity = id
				return true
			}
		}
		return false
	}) {
		t.Fatalf("the refused delivery never reached held(%s): states=%v attempts=%d",
			deliveryReasonRetryExhausted, deliveryTestStates(host), sender.attemptCount("settled"))
	}
	if attempts := sender.attemptCount("settled"); attempts != 1+deliveryRetryLimit {
		t.Fatalf("the channel was asked %d time(s), want the first attempt beside %d retries", attempts, deliveryRetryLimit)
	}
	settled, ok := host.settlementStateOf(ctx, identity)
	if !ok || settled.state != heldStateOf(deliveryReasonRetryExhausted) {
		t.Fatalf("the exhausted obligation reads as %+v (ok=%v), want held(%s)", settled, ok, deliveryReasonRetryExhausted)
	}
	if deliveryStateSettled(settled.state) {
		t.Fatalf("the exhausted state %q reads as a settlement terminal, want a held reason", settled.state)
	}
	if held := host.HeldDeliveries(); len(held) != 1 {
		t.Fatalf("the review table holds %d entr(ies), want the one exhausted obligation", len(held))
	}
}

// TestRetryGapWritesHeldRetrying pins the accounting of the gap.
//
// - The first retry happens only behind a held(retrying) settlement fact, written before the retry itself.
// - The sent receipt that follows reconciles it; the hold reason stays one of the four the family names.
// - The sender callback records the accepted body before the sent fact lands, so the wait is on the reconciliation itself.
func TestRetryGapWritesHeldRetrying(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-retry-gap")
	firstFailure := true
	var mu sync.Mutex
	sender := newDeliveryTestSender(func(int, event.Address, string) error {
		mu.Lock()
		defer mu.Unlock()
		if firstFailure {
			firstFailure = false
			return deliveryTestDefiniteError{}
		}
		return nil
	})
	host := deliveryTestHost(ctx, run, sender)
	host.deliveryBackoff = 10 * time.Millisecond
	deliveryTestInvoke(t, ctx, host, addr, "drive the retry gap")

	if !deliveryTestWaitFor(5*time.Second, func() bool { return sender.deliveredCount("settled") == 1 }) {
		t.Fatalf("the retried body was never accepted: attempts=%d delivered=%q states=%v",
			sender.attemptCount("settled"), sender.deliveredContents(), deliveryTestStates(host))
	}
	identity := ""
	for id := range deliveryTestStates(host) {
		identity = id
	}
	if identity == "" {
		t.Fatalf("the account names no delivery identity: states=%v", deliveryTestStates(host))
	}
	heldFact, err := run.CommittedEvent(ctx, deliverySettlementKey(identity, heldStateOf(deliveryReasonRetrying)))
	if err != nil {
		t.Fatalf("the chain holds no held(%s) fact of %s: %v", deliveryReasonRetrying, identity, err)
	}
	if settlement, ok := settlementOf(heldFact); !ok || settlement.state != heldStateOf(deliveryReasonRetrying) {
		t.Fatalf("the gap fact reads as %+v (ok=%v), want held(%s)", settlement, ok, deliveryReasonRetrying)
	}
	if !deliveryTestWaitFor(5*time.Second, func() bool {
		settled, ok := host.settlementStateOf(ctx, identity)
		return ok && settled.state == deliveryStateSent
	}) {
		settled, ok := host.settlementStateOf(ctx, identity)
		t.Fatalf("the reconciled obligation reads as %+v (ok=%v), want sent", settled, ok)
	}
	if !deliveryTestWaitFor(5*time.Second, func() bool {
		settled, ok := host.settlementStateOf(ctx, identity)
		return ok && settled.state == deliveryStateSent
	}) {
		settled, ok := host.settlementStateOf(ctx, identity)
		t.Fatalf("the reconciled obligation reads as %+v (ok=%v), want sent", settled, ok)
	}
	allowed := map[string]bool{
		heldStateOf(deliveryReasonApproval):       true,
		heldStateOf(deliveryReasonRetrying):       true,
		heldStateOf(deliveryReasonRetryExhausted): true,
		heldStateOf(deliveryReasonUnknown):        true,
	}
	for _, fact := range run.SessionFacts(addr) {
		settlement, ok := settlementOf(fact)
		if !ok || !strings.HasPrefix(settlement.state, deliveryStateHeld+".") {
			continue
		}
		if !allowed[settlement.state] {
			t.Fatalf("the chain holds a hold state outside the four reasons: %q", settlement.state)
		}
	}
}

// TestRetryAttemptsCarryActionIdentity pins that a retry repeats the original action.
//
// - The same target and body, under the same obligation identity.
// - No attempt follows the confirmed receipt; 未确认 sent 才重发，确认之后没有尝试。
func TestRetryAttemptsCarryActionIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-retry-identity")
	failed := false
	var mu sync.Mutex
	sender := newDeliveryTestSender(func(int, event.Address, string) error {
		mu.Lock()
		defer mu.Unlock()
		if !failed {
			failed = true
			return deliveryTestDefiniteError{}
		}
		return nil
	})
	host := deliveryTestHost(ctx, run, sender)
	host.deliveryBackoff = 10 * time.Millisecond
	turn := deliveryTestInvoke(t, ctx, host, addr, "drive the retry identity")
	if !deliveryTestWaitFor(5*time.Second, func() bool { return sender.deliveredCount("settled") == 1 }) {
		t.Fatalf("the retried body was never accepted: attempts=%q", sender.deliveredContents())
	}
	attempts := sender.attemptsFor("settled")
	if len(attempts) != 2 {
		t.Fatalf("the body was attempted %d time(s), want the refusal beside its one retry", len(attempts))
	}
	for index, attempt := range attempts {
		if attempt.Target != addr || attempt.Content != "settled" {
			t.Fatalf("attempt %d repeated %v/%q, want the original action %v/%q", index, attempt.Target, attempt.Content, addr, "settled")
		}
	}
	output := deliveryTestOutputOf(t, run, addr, turn.InvocationID)
	identity := hostDeliveryFreshPrefix + string(output.Key)
	if !deliveryTestWaitFor(5*time.Second, func() bool {
		settled, ok := host.settlementStateOf(ctx, identity)
		return ok && settled.state == deliveryStateSent
	}) {
		settled, ok := host.settlementStateOf(ctx, identity)
		t.Fatalf("the retried obligation of the declared identity reads as %+v (ok=%v), want sent", settled, ok)
	}
	time.Sleep(150 * time.Millisecond)
	if attempts := sender.attemptCount("settled"); attempts != 2 {
		t.Fatalf("the channel was asked %d time(s) after the confirmed receipt, want the 2 attempts of the retry", attempts)
	}
}

// TestDeliveryConsumerNotSelfTriggered pins the consumer face.
//
// - The redrive is a consumer whose timing the state machine holds, never a commit observer that replays settlement facts.
// - The settlement facts are the read face the redrive decides from, and the consumer is the one writer that closes them: it carries no observer face.
func TestDeliveryConsumerNotSelfTriggered(t *testing.T) {
	consumer := newDeliveryConsumer(&Host{})
	if _, listens := any(consumer).(interface {
		ObserveCommit(context.Context, event.EventID) error
	}); listens {
		t.Fatalf("the delivery consumer implements ObserveCommit: its backoff must be self-timed, not commit-triggered")
	}
}

// deliveryTestStopRetries stops the redrive of one check's host: a check that
// holds the backoff open stops the consumer behind it so no timer outlives the
// process it belongs to.
func deliveryTestStopRetries(host *Host) {
	if host == nil {
		return
	}
	host.mu.Lock()
	consumer := host.retries
	host.mu.Unlock()
	if consumer != nil {
		consumer.stopAll()
	}
}

// deliveryTestStates reads the account's state table under its own lock, so a
// check polling the account never races the consumer that writes it.
func deliveryTestStates(host *Host) map[string]string {
	if host == nil {
		return nil
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	states := make(map[string]string, len(host.deliveryStates))
	for identity, state := range host.deliveryStates {
		states[identity] = state
	}
	return states
}

// deliveryTestTwoAgentRun assembles a run declaring the entry agent beside one
// headless agent, so a check can name an address the host never delivers to.
func deliveryTestTwoAgentRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
			Done:    true,
		}
	}}
	decl := &config.Declaration{
		Entry:  string(deliveryTestAgent),
		Agents: []config.AgentSpec{{ID: string(deliveryTestAgent), Model: "model-a"}, {ID: "agent-b", Model: "model-b"}},
	}
	natives := map[event.AgentID]NativeAgent{
		deliveryTestAgent: {Model: provider},
		"agent-b":         {Model: provider},
	}
	options := append([]Option{WithGovernance(snapshotTestGovernance{})}, opts...)
	run, err := Assemble(ctx, decl, natives, options...)
	if err != nil {
		t.Fatalf("assemble the two-agent runtime: %v", err)
	}
	return run
}

// TestRebuildSkipsSettledBodies pins the settled side of the recovery.
//
// - A body the account already confirmed is never sent again beside a restart, because the sent settlement fact the chain holds closes it.
func TestRebuildSkipsSettledBodies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	first := snapshotTestRun(t, ctx, storage)
	addr := deliveryTestAddress("session-rebuild-settled")
	firstSender := newDeliveryTestSender(nil)
	firstHost := deliveryTestHost(ctx, first, firstSender)
	deliveryTestInvoke(t, ctx, firstHost, addr, "drive the settled rebuild")
	if delivered := firstSender.deliveredCount("settled"); delivered != 1 {
		t.Fatalf("the scene never formed: the first process delivered the body %d time(s), want 1", delivered)
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the first process: %v", err)
	}

	restarted := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, restarted, sender)
	if err := host.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the settled delivery: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if delivered := sender.deliveredCount("settled"); delivered != 0 {
		t.Fatalf("the restart delivered the settled body %d time(s), want 0", delivered)
	}
	if held := host.HeldDeliveries(); len(held) != 0 {
		t.Fatalf("the restart parked %d settled obligation(s), want none: %+v", len(held), held)
	}
}

// TestRecoveryResumesInterruptedRetry pins the resumed side of the recovery.
//
// - An obligation the chain states as held(retrying) continues its bounded backoff beside the restart instead of being stranded.
// - 发送落账在收据之后：等收据把义务从待办表里撤下，才算续跑完成。
func TestRecoveryResumesInterruptedRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	first := snapshotTestRun(t, ctx, storage)
	addr := deliveryTestAddress("session-resume-retry")
	firstSender := newDeliveryTestSender(func(int, event.Address, string) error { return deliveryTestDefiniteError{} })
	firstHost := deliveryTestHost(ctx, first, firstSender)
	firstHost.deliveryBackoff = time.Hour
	deliveryTestInvoke(t, ctx, firstHost, addr, "drive the interrupted retry")
	if !deliveryTestWaitFor(5*time.Second, func() bool {
		for _, state := range deliveryTestStates(firstHost) {
			if state == heldStateOf(deliveryReasonRetrying) {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("the scene never formed: the first process left no held(%s) state: %v", deliveryReasonRetrying, deliveryTestStates(firstHost))
	}
	if delivered := firstSender.deliveredCount("settled"); delivered != 0 {
		t.Fatalf("the scene never formed: the refused body reached the channel %d time(s)", delivered)
	}
	deliveryTestStopRetries(firstHost)
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the process that stopped inside the backoff: %v", err)
	}

	restarted := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, restarted, sender)
	host.deliveryBackoff = 20 * time.Millisecond
	if err := host.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the interrupted retry: %v", err)
	}
	if !deliveryTestWaitFor(5*time.Second, func() bool { return sender.deliveredCount("settled") == 1 }) {
		t.Fatalf("the restart never resumed the interrupted retry: attempts=%d states=%v",
			sender.attemptCount("settled"), deliveryTestStates(host))
	}
	if !deliveryTestWaitFor(5*time.Second, func() bool { return len(host.HeldDeliveries()) == 0 }) {
		t.Fatalf("the resumed retry left %d obligation(s) parked after it landed: %+v", len(host.HeldDeliveries()), host.HeldDeliveries())
	}
	deliveryTestStopRetries(host)
}

// TestMultiSessionRecoveryBounded pins the counting rule of the recovery.
//
// - The settlement facts and the owed declarations are read out of the hot fact window of the registered chains.
// - The returned matching entries are what counts: an unrelated agent_output never raises that count, and every unsettled obligation is inside the result.
// - Ten unrelated agent_output facts (no owed declaration) in a third session must not raise the returned matching count.
func TestMultiSessionRecoveryBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	first := snapshotTestRun(t, ctx, storage)
	sessionA := deliveryTestAddress("session-bounded-a")
	sessionB := deliveryTestAddress("session-bounded-b")
	sender := newDeliveryTestSender(func(int, event.Address, string) error { return deliveryTestDefiniteError{} })
	host := deliveryTestHost(ctx, first, sender)
	host.deliveryBackoff = time.Hour
	deliveryTestInvoke(t, ctx, host, sessionA, "drive bounded session a")
	deliveryTestInvoke(t, ctx, host, sessionB, "drive bounded session b")
	if !deliveryTestWaitFor(5*time.Second, func() bool { return len(deliveryTestStates(host)) == 2 }) {
		t.Fatalf("the scene never formed: the two held obligations are %v", deliveryTestStates(host))
	}
	identities := make([]string, 0, 2)
	for identity, obliged := range deliveryTestStates(host) {
		if !deliveryStateUnsettled(obliged) {
			t.Fatalf("the accounted obligation of %s states %q, want an unsettled state", identity, obliged)
		}
		identities = append(identities, identity)
	}

	noise := deliveryTestAddress("session-bounded-noise")
	for index := 0; index < 10; index++ {
		if _, err := first.commitFactChecked(ctx, event.Event{
			Type:      event.TypeAgentOutput,
			Timestamp: time.Now(),
			Message:   model.Message{Role: model.RoleAssistant, Content: fmt.Sprintf("unrelated output %d", index)},
			Owner:     noise,
			Source:    noise,
			Target:    noise,
		}); err != nil {
			t.Fatalf("inject the unrelated output %d: %v", index, err)
		}
	}
	deliveryTestStopRetries(host)
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the bounded process: %v", err)
	}

	restarted := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if partitions := restarted.registeredPartitions(); len(partitions) == 0 {
		t.Fatalf("the restarted run registered no chain partition, so no settlement fact could be read")
	}
	refs, err := restarted.factWindowQuery(ctx, memory.QueryOptions{
		PartitionIDs: restarted.registeredPartitions(),
	}.WithKeyPrefix(deliveryFactPrefix))
	if err != nil {
		t.Fatalf("query the settlement window: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("the settlement window returned %d matching entr(ies), want the 2 held facts of the two sessions: %+v", len(refs), refs)
	}
	for _, ref := range refs {
		if !strings.HasPrefix(string(ref.Key), deliveryFactPrefix) {
			t.Fatalf("the narrowed query returned a fact outside the family: %q", ref.Key)
		}
	}
	restartedHost := deliveryTestHost(ctx, restarted, newDeliveryTestSender(nil))
	restartedHost.deliveryBackoff = time.Hour
	if err := restartedHost.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the bounded sessions: %v", err)
	}
	for _, identity := range identities {
		obliged, known := restartedHost.deliveryStateOf(identity)
		if !known || !deliveryStateUnsettled(obliged) {
			t.Fatalf("the restart lost the unsettled obligation %s (state=%q known=%v)", identity, obliged, known)
		}
	}
	deliveryTestStopRetries(restartedHost)
}

// TestSettlementFactsStayHotWhileUnsettled pins the pin.
//
// - A settlement fact of an obligation that still owes something is never trimmed out of the hot window, however far the chain grows past it.
// - An ordinary fact before it is trimmed away; 已结算的事实不上钉，它按普通窗口老化。
func TestSettlementFactsStayHotWhileUnsettled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-hot-window")
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))

	early, err := run.commitFactChecked(ctx, event.Event{
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Owner:     addr,
		Source:    addr,
		Target:    addr,
		Meta:      map[string]string{event.MetaKeyEventSummary: "the early fact"},
	})
	if err != nil {
		t.Fatalf("commit the early fact: %v", err)
	}
	const identity = "delivery-fresh-2-hot-window"
	delivery := plugin.Delivery{Content: "the body of the held obligation", Lineage: "user", Target: addr}
	if err := host.writeDeliverySettlement(ctx, addr, identity, heldStateOf(deliveryReasonApproval), delivery, time.Now(), nil); err != nil {
		t.Fatalf("write the held settlement fact: %v", err)
	}
	key := deliverySettlementKey(identity, heldStateOf(deliveryReasonApproval))
	if _, err := run.commitFactChecked(ctx, event.Event{
		Type:      event.TypeAgentOutput,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleAssistant, Content: "a later answer"},
		Owner:     addr,
		Source:    addr,
		Target:    addr,
	}); err != nil {
		t.Fatalf("commit the turn-end fact: %v", err)
	}
	for index := 0; index < factWindow+32; index++ {
		if _, err := run.commitFactChecked(ctx, event.Event{
			Type:      event.TypeFeedback,
			Timestamp: time.Now(),
			Owner:     addr,
			Source:    addr,
			Target:    addr,
			Meta:      map[string]string{event.MetaKeyEventSummary: fmt.Sprintf("filler %d", index)},
		}); err != nil {
			t.Fatalf("commit filler fact %d: %v", index, err)
		}
	}
	if _, held := run.factOf(early); held {
		t.Fatalf("the early fact inside the window is still live, so the trim never ran and the pin proves nothing")
	}
	if _, held := run.factOf(key); !held {
		t.Fatalf("the held settlement fact was trimmed out of the hot window: the unsettled obligation is no longer readable")
	}
	if err := host.writeDeliverySettlement(ctx, addr, "delivery-fresh-2-settled-window", deliveryStateSent,
		plugin.Delivery{Content: "a settled body", Lineage: "user", Target: addr}, time.Time{}, nil); err != nil {
		t.Fatalf("write the settled settlement fact: %v", err)
	}
	host.mu.Lock()
	_, pinned := host.deliveryPins["delivery-fresh-2-settled-window"]
	host.mu.Unlock()
	if pinned {
		t.Fatalf("the settled delivery was pinned in the hot window, want the pin released with its obligation")
	}
}

// TestDanglingTurnAfterDualFailure pins the turn the killed driver leaves behind.
//
// - The input is committed, the driver stops inside a tool round before the output commits, and the host that meets the chain afterwards sends no notice.
// - 杀驱动：段在 tool 轮打开、output 未提交时被就地放弃。
// - 负向：没有通知发出（被杀的驱动没有终态，host 也无义务可告知）。
// - 正向：悬空轮在链上可辨识——input 事实在，agent_output 不在。
// - 杀进程不跑收尾：被放弃的段里那个在飞任务没有收割者，关闭等宽限到期是预期，所以只在后台关一遍。
// - host 处理：重启恢复读同一条链，不发明一条通知，也不把悬空轮当已送达。
func TestDanglingTurnAfterDualFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	begun := make(chan struct{})
	release := make(chan struct{})
	tool := &deliveryTestBlockingTool{name: "hold", begun: begun, release: release}
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  string(deliveryTestAgent),
		Agents: []config.AgentSpec{{ID: string(deliveryTestAgent), Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{
		deliveryTestAgent: {
			Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
				return &model.Response{
					Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
						ID:       "call-hold",
						Type:     "function",
						Function: model.FunctionDefinitionParam{Name: tool.name, Arguments: []byte(`{}`)},
					}}}}},
					Done: true,
				}
			}},
			Tools: []trpctool.CallableTool{tool},
		},
	}, WithGovernance(snapshotTestGovernance{}), storage)
	if err != nil {
		t.Fatalf("assemble the dangling runtime: %v", err)
	}
	addr := deliveryTestAddress("session-dangling")
	hostCtx, stopHost := context.WithCancel(ctx)
	defer stopHost()
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(hostCtx, run, sender)
	const invocation = event.InvocationID("inv-dangling")
	type driveResult struct {
		turn agent.Turn
		err  error
	}
	done := make(chan driveResult, 1)
	go func() {
		turn, err := host.Invoke(ctx, InvokeRequest{
			InvocationID: invocation,
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "drive the dangling turn"}},
		})
		done <- driveResult{turn: turn, err: err}
	}()
	select {
	case <-begun:
	case <-time.After(10 * time.Second):
		t.Fatalf("the scene never formed: the driver never ran the tool round")
	}
	stopHost()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the killed driver never answered its entry")
	}
	close(release)

	if delivered := sender.deliveredContents(); len(delivered) != 0 {
		t.Fatalf("the host delivered %q beside a dangling turn, want nothing", delivered)
	}
	facts := run.SessionFacts(addr)
	inputs := 0
	outputs := 0
	for _, fact := range facts {
		if fact.InvocationID != invocation {
			continue
		}
		switch fact.Type {
		case event.TypeExternalInput:
			inputs++
		case event.TypeAgentOutput:
			outputs++
		}
	}
	if inputs == 0 || outputs != 0 {
		t.Fatalf("the dangling turn reads as inputs=%d outputs=%d on the chain, want the committed input beside no output: %s",
			inputs, outputs, boundaryAnchorFactsSummary(facts))
	}
	closed := make(chan struct{})
	go func() {
		_, _ = run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(time.Second)})
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the first run did not drain its writers before the restart; a fire-and-forget Close leaves a KV write in flight and TempDir cleanup fails with directory not empty")
	}

	restarted := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	restartedSender := newDeliveryTestSender(nil)
	restartedHost := deliveryTestHost(ctx, restarted, restartedSender)
	restartedHost.deliveryBackoff = time.Hour
	if err := restartedHost.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore beside the dangling turn: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if delivered := restartedSender.deliveredContents(); len(delivered) != 0 {
		t.Fatalf("the recovery delivered %q beside the dangling turn, want nothing", delivered)
	}
	restoredInputs := 0
	for _, fact := range restarted.SessionFacts(addr) {
		if fact.Type == event.TypeExternalInput && fact.InvocationID == invocation {
			restoredInputs++
		}
	}
	if restoredInputs == 0 {
		t.Fatalf("the restart lost the dangling input %s: %s", invocation, boundaryAnchorFactsSummary(restarted.SessionFacts(addr)))
	}
	deliveryTestStopRetries(restartedHost)
}

// deliveryTestBlockingTool is a tool whose call the check holds open, so the
// check can stop the driver inside a tool round.
type deliveryTestBlockingTool struct {
	name    string
	begun   chan struct{}
	release chan struct{}
	once    sync.Once
}

// Declaration returns the declaration naming the holding tool.
func (t *deliveryTestBlockingTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call reports the round began and holds until the check releases it.
func (t *deliveryTestBlockingTool) Call(context.Context, []byte) (any, error) {
	t.once.Do(func() { close(t.begun) })
	<-t.release
	return "held", nil
}

// deliveryTestCountingLog wraps one fact log and counts the reads a check is
// judging, so a scan of the retired prefix is counted where it happens.
type deliveryTestCountingLog struct {
	inner state.FactLog
	mu    sync.Mutex
	reads []string
}

// Put stages one key on the wrapped log.
func (l *deliveryTestCountingLog) Put(ctx context.Context, key string, value []byte) error {
	return l.inner.Put(ctx, key, value)
}

// Get reads one key from the wrapped log.
func (l *deliveryTestCountingLog) Get(ctx context.Context, key string) ([]byte, bool, error) {
	l.mu.Lock()
	l.reads = append(l.reads, "get:"+key)
	l.mu.Unlock()
	return l.inner.Get(ctx, key)
}

// Scan reads one prefix from the wrapped log and counts the read.
func (l *deliveryTestCountingLog) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	l.mu.Lock()
	l.reads = append(l.reads, "scan:"+prefix)
	l.mu.Unlock()
	return l.inner.Scan(ctx, prefix)
}

// Sync completes the write barrier of the wrapped log.
func (l *deliveryTestCountingLog) Sync(ctx context.Context) error {
	return l.inner.Sync(ctx)
}

// scanCount reports how often the retired prefix was read, and reset clears the
// count so one phase of a check is judged alone.
func (l *deliveryTestCountingLog) scanCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, read := range l.reads {
		if strings.HasPrefix(read, "scan:"+hostDeliveryPrefix) {
			count++
		}
	}
	return count
}

// reset clears every read the wrapper recorded.
func (l *deliveryTestCountingLog) reset() {
	l.mu.Lock()
	l.reads = nil
	l.mu.Unlock()
}

// deliveryTestLegacyRecords reads every retired delivery record the host's tier
// holds, ordered by identity.
func deliveryTestLegacyRecords(t *testing.T, ctx context.Context, host *Host) []hostDeliveryRecord {
	t.Helper()
	log := host.deliveryDurable()
	if log == nil {
		t.Fatalf("the assembled run opened no storage tier to read the retired records from")
	}
	held, err := log.Scan(ctx, hostDeliveryPrefix)
	if err != nil {
		t.Fatalf("scan the retired delivery records: %v", err)
	}
	keys := make([]string, 0, len(held))
	for key := range held {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]hostDeliveryRecord, 0, len(keys))
	for _, key := range keys {
		var record hostDeliveryRecord
		if err := json.Unmarshal(held[key], &record); err != nil {
			t.Fatalf("read the retired record %s back: %v", key, err)
		}
		records = append(records, record)
	}
	return records
}

// deliveryTestWriteLegacyRecords writes the six retired record states onto the
// host's tier, the account the migration reads.
func deliveryTestWriteLegacyRecords(t *testing.T, ctx context.Context, host *Host, records []hostDeliveryRecord) {
	t.Helper()
	log := host.deliveryDurable()
	if log == nil {
		t.Fatalf("the assembled run opened no storage tier to write the retired records to")
	}
	for _, record := range records {
		payload, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal the retired record %s: %v", record.ID, err)
		}
		if err := log.Put(ctx, hostDeliveryPrefix+record.ID, payload); err != nil {
			t.Fatalf("write the retired record %s: %v", record.ID, err)
		}
	}
	if err := log.Sync(ctx); err != nil {
		t.Fatalf("complete the retired records' write barrier: %v", err)
	}
}

// TestKVDeliveryMigratedToFacts pins the migration of the read-only six-state account onto the settlement facts.
//
// - The owed record is delivered once; the held and retry records wait again as held states.
// - The headless record is retired with its body kept, the receipts stay receipts, and a second boot migrates nothing twice.
// - 只读记录留档：迁移不删除审计。
// - 第二次重启：迁移幂等，不双发、不添第二条结算事实。
// - 六条历史事实（owed/held/retry/sent/refused 各一，另加 owed 那次补送后的 sent），第二次启动不添第七条。
func TestKVDeliveryMigratedToFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	first := deliveryTestTwoAgentRun(t, ctx, storage)
	addr := deliveryTestAddress("session-migration")
	headless := event.Address{AgentID: "agent-b", SessionID: event.SessionID("session-migration")}
	held := time.Now().Add(-time.Minute)
	deliveryTestWriteLegacyRecords(t, ctx, deliveryTestHost(ctx, first, nil), []hostDeliveryRecord{
		{ID: "delivery-000001", Content: "the owed legacy body", Lineage: "user", Target: addr, Status: hostDeliveryOwed, HeldAt: held},
		{ID: "delivery-000002", Content: "the held legacy body", Lineage: "user", Target: addr, Status: hostDeliveryHeld, HeldAt: held},
		{ID: "delivery-000003", Content: "the retried legacy body", Lineage: "user", Target: addr, Status: hostDeliveryRetry, HeldAt: held},
		{ID: "delivery-000004", Content: "the sent legacy body", Lineage: "user", Target: addr, Status: hostDeliverySent, HeldAt: held},
		{ID: "delivery-000005", Content: "the refused legacy body", Lineage: "user", Target: addr, Status: hostDeliveryRefused, HeldAt: held},
		{ID: "delivery-000006", Content: "the headless legacy body", Lineage: "user", Target: headless, Status: hostDeliveryHeadless, HeldAt: held},
	})

	sender := newDeliveryTestSender(nil)
	second := deliveryTestTwoAgentRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	host := deliveryTestHost(ctx, second, sender)
	host.deliveryBackoff = time.Hour
	if err := host.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("migrate the retired account: %v", err)
	}
	if delivered := sender.deliveredContents(); len(delivered) != 1 || delivered[0] != "the owed legacy body" {
		t.Fatalf("the migration delivered %q, want the one owed legacy body", delivered)
	}
	heldDeliveries := host.HeldDeliveries()
	if len(heldDeliveries) != 2 {
		t.Fatalf("the migration parked %d entr(ies), want the held and the retried record: %+v", len(heldDeliveries), heldDeliveries)
	}
	states := map[string]string{
		"delivery-000001": deliveryStateSent,
		"delivery-000002": heldStateOf(deliveryReasonApproval),
		"delivery-000003": heldStateOf(deliveryReasonRetryExhausted),
		"delivery-000004": deliveryStateSent,
		"delivery-000005": deliveryStateRefused,
		"delivery-000006": deliveryStateHeadlessRetired,
	}
	for identity, want := range states {
		settled, known := host.settlementStateOf(ctx, identity)
		if !known || settled.state != want {
			t.Fatalf("the migrated record %s reads as %+v (known=%v), want %q", identity, settled, known, want)
		}
	}
	retired, known := host.settlementStateOf(ctx, "delivery-000006")
	if !known || retired.delivery.Content != "the headless legacy body" {
		t.Fatalf("the retired headless record kept %+v (known=%v), want the body archived on the fact", retired, known)
	}
	if count := sender.attemptCount("the headless legacy body"); count != 0 {
		t.Fatalf("the headless record was sent %d time(s), want 0", count)
	}
	for _, body := range []string{"the held legacy body", "the retried legacy body", "the sent legacy body", "the refused legacy body"} {
		if count := sender.attemptCount(body); count != 0 {
			t.Fatalf("the %q record was sent %d time(s) beside the migration, want 0", body, count)
		}
	}
	if records := deliveryTestLegacyRecords(t, ctx, host); len(records) != 6 {
		t.Fatalf("the retired account holds %d record(s) after the migration, want the six kept as the audit", len(records))
	}
	deliveryTestStopRetries(host)
	if _, err := second.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the migrating process: %v", err)
	}

	third := deliveryTestTwoAgentRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	thirdSender := newDeliveryTestSender(nil)
	thirdHost := deliveryTestHost(ctx, third, thirdSender)
	thirdHost.deliveryBackoff = time.Hour
	if err := thirdHost.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("migrate the retired account a second time: %v", err)
	}
	if delivered := thirdSender.deliveredContents(); len(delivered) != 0 {
		t.Fatalf("the second boot delivered %q, want nothing new", delivered)
	}
	for identity, want := range states {
		settled, known := thirdHost.settlementStateOf(ctx, identity)
		if !known || settled.state != want {
			t.Fatalf("the second boot reads %s as %+v (known=%v), want %q", identity, settled, known, want)
		}
	}
	if count := boundaryAnchorSettlementFacts(third.SessionFacts(addr)); count != 6 {
		t.Fatalf("the second boot left %d settlement fact(s) at %s, want the 6 the migration and its one resend wrote", count, addr.SessionID)
	}
	if settled, known := thirdHost.settlementStateOf(ctx, "delivery-000001"); !known || settled.state != deliveryStateSent {
		t.Fatalf("the migrated owed record reads as %+v (known=%v), want its historical owed fact behind the sent state", settled, known)
	}
	deliveryTestStopRetries(thirdHost)
}

// TestKVRecordUnparseableRefusesBoot pins the refusal.
//
// - A read-only record the account cannot read refuses the boot by name instead of guessing a disposition the bytes do not state.
func TestKVRecordUnparseableRefusesBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	log := host.deliveryDurable()
	if log == nil {
		t.Fatalf("the assembled run opened no storage tier")
	}
	if err := log.Put(ctx, hostDeliveryPrefix+"delivery-000009", []byte("this is not a delivery record")); err != nil {
		t.Fatalf("write the unreadable record: %v", err)
	}
	if err := log.Sync(ctx); err != nil {
		t.Fatalf("complete the unreadable record's write barrier: %v", err)
	}
	err := host.restorePendingDeliveries(ctx)
	if err == nil || !strings.Contains(err.Error(), "does not unmarshal") {
		t.Fatalf("the boot beside an unreadable retired record answered %v, want the named refusal", err)
	}
}

// TestRecoveryNoLongerScansKVPrefix pins the read face of the recovery.
//
// - The one-time migration is the only read of the read-only prefix, and the recovery proper reads the fact chain alone.
// - The recovery proper (what every later boot and the live path share) never reads the read-only prefix: only the fact chain.
func TestRecoveryNoLongerScansKVPrefix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}))
	addr := deliveryTestAddress("session-no-kv-scan")
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	const identity = "delivery-fresh-2-no-kv-scan"
	if err := host.writeDeliverySettlement(ctx, addr, identity, heldStateOf(deliveryReasonApproval),
		plugin.Delivery{Content: "the body the recovery reads", Lineage: "user", Target: addr}, time.Now(), nil); err != nil {
		t.Fatalf("write the settlement fact: %v", err)
	}
	counter := &deliveryTestCountingLog{inner: host.deliveryDurable()}
	host.deliveryLog = counter
	if err := host.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restore the delivery account: %v", err)
	}
	if scans := counter.scanCount(); scans != 1 {
		t.Fatalf("the boot read the retired prefix %d time(s), want the one migration read", scans)
	}

	counter.reset()
	host.mu.Lock()
	host.deliveryStates = nil
	host.deliveryPins = nil
	host.mu.Unlock()
	if err := host.restoreEventifiedDeliveries(ctx); err != nil {
		t.Fatalf("restore the account from the chain: %v", err)
	}
	if scans := counter.scanCount(); scans != 0 {
		t.Fatalf("the recovery scanned the retired prefix %d time(s), want 0", scans)
	}
	if obliged, known := host.deliveryStateOf(identity); !known || obliged != heldStateOf(deliveryReasonApproval) {
		t.Fatalf("the recovery restored %q (known=%v) for %s, want the held state the chain states", obliged, known, identity)
	}
	deliveryTestStopRetries(host)
}

// TestHeldSettlementReachesModel pins the perception of the account.
//
// - A held settlement fact enters the request the model reads, exactly as a receipt of the same family does.
func TestHeldSettlementReachesModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	run, provider := boundaryAnchorAssemble(t, ctx, boundaryAnchorSpec(), boundaryAnchorAnswer(boundaryAnchorBody))
	addr := boundaryAnchorAddress("session-perception")
	failed := false
	sender := newDeliveryTestSender(func(int, event.Address, string) error {
		if !failed {
			failed = true
			return deliveryTestDefiniteError{}
		}
		return nil
	})
	host := deliveryTestHost(ctx, run, sender)
	host.deliveryBackoff = time.Hour
	deliveryTestInvoke(t, ctx, host, addr, "drive the held settlement")

	identity := ""
	if !deliveryTestWaitFor(5*time.Second, func() bool {
		for id, obliged := range deliveryTestStates(host) {
			if obliged == heldStateOf(deliveryReasonRetrying) {
				identity = id
				return true
			}
		}
		return false
	}) {
		t.Fatalf("the scene never formed: no held(%s) obligation: %v", deliveryReasonRetrying, deliveryTestStates(host))
	}
	deliveryTestInvoke(t, ctx, host, addr, "read the held settlement")
	provider.mu.Lock()
	requests := append([]model.Request(nil), provider.requests...)
	provider.mu.Unlock()
	if len(requests) == 0 {
		t.Fatalf("the second turn served no model request")
	}
	request := requests[len(requests)-1]
	found := false
	for _, message := range request.Messages {
		if strings.Contains(message.Content, identity) && strings.Contains(message.Content, "reason="+deliveryReasonRetrying) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the held settlement fact of %s is not in the request the model read: %s | %s", identity, deliveryTestRequestSummary(request), deliveryTestRequestContents(request))
	}
	deliveryTestStopRetries(host)
}

// TestDeliveryNoiseAbsentFromProjection pins the negative of the perception.
//
// - The body a settlement fact keeps for the recovery is accounting material and never enters the projection.
// - The state line of the same fact does.
func TestDeliveryNoiseAbsentFromProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run, provider := boundaryAnchorAssemble(t, ctx, boundaryAnchorSpec(), boundaryAnchorAnswer(boundaryAnchorBody))
	addr := boundaryAnchorAddress("session-noise")
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	const noise = "NOISE-SENTINEL-THIS-BODY-MUST-NOT-REACH-THE-MODEL"
	if err := host.writeDeliverySettlement(ctx, addr, "delivery-fresh-noise", heldStateOf(deliveryReasonApproval),
		plugin.Delivery{Content: noise, Lineage: "user", Target: addr}, time.Now(), nil); err != nil {
		t.Fatalf("write the noisy settlement fact: %v", err)
	}
	deliveryTestInvoke(t, ctx, host, addr, "read the projected settlement")
	provider.mu.Lock()
	requests := append([]model.Request(nil), provider.requests...)
	provider.mu.Unlock()
	if len(requests) == 0 {
		t.Fatalf("the turn served no model request")
	}
	request := requests[len(requests)-1]
	line := false
	for _, message := range request.Messages {
		if strings.Contains(message.Content, noise) {
			t.Fatalf("the accounting body of the settlement fact reached the model request: %q", message.Content)
		}
		if strings.Contains(message.Content, "delivery-fresh-noise") {
			line = true
		}
	}
	if !line {
		t.Fatalf("the settlement line is absent from the request, so the absence of the body proves nothing: %s", deliveryTestRequestSummary(request))
	}
}

// TestHeldReasonsDistinguishable pins the four-state model.
//
// - The three hold reasons are distinct states of distinct facts, the model reads each of them, and the review table still names all three.
// - 模型可见面同样可分辨：三条 reason 各自出现在请求里。
func TestHeldReasonsDistinguishable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run, provider := boundaryAnchorAssemble(t, ctx, boundaryAnchorSpec(), boundaryAnchorAnswer(boundaryAnchorBody))
	addr := boundaryAnchorAddress("session-reasons")
	host := deliveryTestHost(ctx, run, newDeliveryTestSender(nil))
	reasons := []string{deliveryReasonApproval, deliveryReasonRetrying, deliveryReasonRetryExhausted}
	keys := map[event.EventID]bool{}
	for index, reason := range reasons {
		identity := fmt.Sprintf("delivery-fresh-2-reason-%d", index)
		delivery := plugin.Delivery{Content: fmt.Sprintf("the body held for %s", reason), Lineage: "user", Target: addr}
		state := heldStateOf(reason)
		if err := host.writeDeliverySettlement(ctx, addr, identity, state, delivery, time.Now(), nil); err != nil {
			t.Fatalf("write the held(%s) fact: %v", reason, err)
		}
		host.parkHeld(identity, delivery, time.Now())
		keys[deliverySettlementKey(identity, state)] = true
		settled, known := host.settlementStateOf(ctx, identity)
		if !known || settled.state != state {
			t.Fatalf("the held(%s) obligation reads as %+v (known=%v)", reason, settled, known)
		}
		if back, held := heldReasonOf(settled.state); !held || back != reason {
			t.Fatalf("the state %q resolves to reason %q (held=%v), want %q", settled.state, back, held, reason)
		}
		if !strings.Contains(deliverySettlementLine(identity, state, delivery), "reason="+reason) {
			t.Fatalf("the line of %q does not name its reason: %q", state, deliverySettlementLine(identity, state, delivery))
		}
	}
	if len(keys) != len(reasons) {
		t.Fatalf("the three reasons share %d fact key(s), want three distinct keys", len(keys))
	}
	if heldDeliveries := host.HeldDeliveries(); len(heldDeliveries) != len(reasons) {
		t.Fatalf("the review table holds %d entr(ies), want the three reasons: %+v", len(heldDeliveries), heldDeliveries)
	}

	deliveryTestInvoke(t, ctx, host, addr, "read the three reasons")
	provider.mu.Lock()
	requests := append([]model.Request(nil), provider.requests...)
	provider.mu.Unlock()
	request := requests[len(requests)-1]
	for _, reason := range reasons {
		found := false
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "reason="+reason) {
				found = true
			}
		}
		if !found {
			t.Fatalf("the model request does not name the held reason %q: %s", reason, deliveryTestRequestSummary(request))
		}
	}
}

// TestNoSilentUndischargedObligation pins the two fallback branches and their negatives.
//
// - 支①：failed 终态 + 无正文 + 有原因 → 既有失败告知。
// - 支②：held 义务在身 + completed 无产出 → 恰一条延迟告知，不按失败措辞。
// - 负向：成功无正文且无未清义务 → 静默。
// - 负向：无终态（驱动被杀）→ 不发通知。
func TestNoSilentUndischargedObligation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-fallback")
	sender := newDeliveryTestSender(nil)

	failedHost := deliveryTestHost(ctx, run, sender)
	failedHost.deliverFresh(ctx, addr, sender, "user", agent.Turn{Terminal: model.KindFailed, Failure: "the provider named the cause"})
	delivered := sender.deliveredContents()
	if len(delivered) != 1 || !strings.Contains(delivered[0], "the provider named the cause") {
		t.Fatalf("the failed round delivered %q, want the one failure notice naming the cause", delivered)
	}

	heldHost := deliveryTestHost(ctx, run, sender)
	delivery := plugin.Delivery{Content: "the body held beside the silent round", Lineage: "user", Target: addr}
	if err := heldHost.writeDeliverySettlement(ctx, addr, "delivery-fresh-2-delayed", heldStateOf(deliveryReasonApproval), delivery, time.Now(), nil); err != nil {
		t.Fatalf("write the delayed-notice obligation: %v", err)
	}
	heldHost.parkHeld("delivery-fresh-2-delayed", delivery, time.Now())
	heldHost.deliverFresh(ctx, addr, sender, "user", agent.Turn{Terminal: model.KindCompleted})
	delivered = sender.deliveredContents()
	if len(delivered) != 2 {
		t.Fatalf("the silent round beside a held obligation delivered %q, want exactly one delayed notice", delivered[1:])
	}
	notice := delivered[1]
	if notice != delayedNoticeManual {
		t.Fatalf("the delayed notice = %q, want the layered %q", notice, delayedNoticeManual)
	}
	if strings.Contains(notice, "the provider named the cause") || strings.Contains(notice, "failed") {
		t.Fatalf("the delayed notice borrowed the failure wording: %q", notice)
	}

	silentHost := deliveryTestHost(ctx, run, sender)
	before := len(sender.deliveredContents())
	silentHost.deliverFresh(ctx, addr, sender, "user", agent.Turn{Terminal: model.KindCompleted})
	if after := len(sender.deliveredContents()); after != before {
		t.Fatalf("a successful silent round with no unsettled obligation delivered %q, want silence", sender.deliveredContents()[before:])
	}

	danglingHost := deliveryTestHost(ctx, run, sender)
	danglingHost.deliverFresh(ctx, addr, sender, "user", agent.Turn{Terminal: model.KindCancelled})
	if after := len(sender.deliveredContents()); after != before {
		t.Fatalf("a turn without a terminal answer delivered %q, want silence", sender.deliveredContents()[before:])
	}
}

// TestDelayedNoticeLayeredByReason pins the wording of the delayed notice.
//
// - A redrive in flight promises the resend, and every other hold hands the obligation to a person.
func TestDelayedNoticeLayeredByReason(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-delayed-layers")
	if delayedNoticeText(deliveryReasonRetrying) == delayedNoticeText(deliveryReasonApproval) {
		t.Fatalf("the retrying and the manual delayed notices share one wording")
	}
	for _, reason := range []string{deliveryReasonApproval, deliveryReasonRetrying, deliveryReasonRetryExhausted, deliveryReasonUnknown} {
		sender := newDeliveryTestSender(nil)
		host := deliveryTestHost(ctx, run, sender)
		identity := "delivery-fresh-2-layer-" + reason
		delivery := plugin.Delivery{Content: "the body held for " + reason, Lineage: "user", Target: addr}
		if err := host.writeDeliverySettlement(ctx, addr, identity, heldStateOf(reason), delivery, time.Now(), nil); err != nil {
			t.Fatalf("write the held(%s) obligation: %v", reason, err)
		}
		host.parkHeld(identity, delivery, time.Now())
		host.deliverFresh(ctx, addr, sender, "user", agent.Turn{Terminal: model.KindCompleted})
		delivered := sender.deliveredContents()
		want := delayedNoticeText(reason)
		if len(delivered) != 1 || delivered[0] != want {
			t.Fatalf("the held(%s) round delivered %q, want the one layered notice %q", reason, delivered, want)
		}
		expected := delayedNoticeManual
		if reason == deliveryReasonRetrying {
			expected = delayedNoticeRetrying
		}
		if want != expected {
			t.Fatalf("the held(%s) notice = %q, want %q", reason, want, expected)
		}
	}
}

// TestApproveRefuseOnFacts pins the decision faces on the settlement facts.
//
// - An approval sends once and records the receipt; a repeated decision names no hold.
// - 拒绝：不发送，记 refused 结算事实与审计事实。
func TestApproveRefuseOnFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-decisions")
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, run, sender)
	host.deliveryRuler = &deliveryTestRuler{holds: map[string]bool{"settled": true}}
	deliveryTestInvoke(t, ctx, host, addr, "drive the reviewed delivery")
	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("the review table holds %d entr(ies) beside one held body, want 1: %+v", len(held), held)
	}
	if err := host.ApproveDelivery(ctx, held[0].ID); err != nil {
		t.Fatalf("approve the held delivery: %v", err)
	}
	if delivered := sender.deliveredCount("settled"); delivered != 1 {
		t.Fatalf("the approval delivered the body %d time(s), want 1", delivered)
	}
	approved, known := host.settlementStateOf(ctx, held[0].ID)
	if !known || approved.state != deliveryStateSent {
		t.Fatalf("the approved obligation reads as %+v (known=%v), want sent", approved, known)
	}
	if err := host.ApproveDelivery(ctx, held[0].ID); !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("the repeated approval answered %v, want ErrDeliveryNotFound", err)
	}
	if delivered := sender.deliveredCount("settled"); delivered != 1 {
		t.Fatalf("the repeated approval delivered the body %d time(s), want the one copy", delivered)
	}

	deliveryTestInvoke(t, ctx, host, addr, "drive the refused delivery")
	held = host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("the review table holds %d entr(ies) beside the second hold, want 1: %+v", len(held), held)
	}
	if err := host.RefuseDelivery(ctx, held[0].ID); err != nil {
		t.Fatalf("refuse the held delivery: %v", err)
	}
	if delivered := sender.deliveredCount("settled"); delivered != 1 {
		t.Fatalf("the refusal delivered the body %d time(s), want the one approved copy", delivered)
	}
	refused, known := host.settlementStateOf(ctx, held[0].ID)
	if !known || refused.state != deliveryStateRefused {
		t.Fatalf("the refused obligation reads as %+v (known=%v), want refused", refused, known)
	}
	audited := false
	for _, fact := range run.SessionFacts(addr) {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaPrefix+"delivery_id"] == held[0].ID {
			audited = true
		}
	}
	if !audited {
		t.Fatalf("the refusal left no audit fact naming %s", held[0].ID)
	}
	if err := host.RefuseDelivery(ctx, held[0].ID); !errors.Is(err, ErrDeliveryNotFound) {
		t.Fatalf("the repeated refusal answered %v, want ErrDeliveryNotFound", err)
	}
	if heldDeliveries := host.HeldDeliveries(); len(heldDeliveries) != 0 {
		t.Fatalf("the review table holds %d entr(ies) after both decisions, want none: %+v", len(heldDeliveries), heldDeliveries)
	}
}

// TestDeliveryConsumerPathEvidence pins the consumer path end to end through the host's public entry.
//
// - A definite refusal is redriven automatically, the body lands, and the account closes with the sent receipt the chain states.
func TestDeliveryConsumerPathEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := snapshotTestRun(t, ctx)
	addr := deliveryTestAddress("session-consumer-path")
	failed := false
	var mu sync.Mutex
	sender := newDeliveryTestSender(func(int, event.Address, string) error {
		mu.Lock()
		defer mu.Unlock()
		if !failed {
			failed = true
			return deliveryTestDefiniteError{}
		}
		return nil
	})
	host := deliveryTestHost(ctx, run, sender)
	host.deliveryBackoff = 10 * time.Millisecond
	deliveryTestInvoke(t, ctx, host, addr, "drive the consumer path")
	if !deliveryTestWaitFor(5*time.Second, func() bool { return sender.deliveredCount("settled") == 1 }) {
		t.Fatalf("the consumer never redrove the refused body: attempts=%d states=%v", sender.attemptCount("settled"), deliveryTestStates(host))
	}
	if attempts := sender.attemptCount("settled"); attempts != 2 {
		t.Fatalf("the consumer path attempted the body %d time(s), want the refusal beside its one redrive", attempts)
	}
	if !deliveryTestWaitFor(5*time.Second, func() bool { return len(host.HeldDeliveries()) == 0 }) {
		t.Fatalf("the consumer path left the obligation parked after its redrive landed: %+v", host.HeldDeliveries())
	}
	for identity, obliged := range deliveryTestStates(host) {
		settled, known := host.settlementStateOf(ctx, identity)
		if !known || settled.state != obliged {
			t.Fatalf("the account of %s reads %+v (known=%v), want the %q it states", identity, settled, known, obliged)
		}
		if settled.state != deliveryStateSent {
			t.Fatalf("the redriven obligation %s ends as %q, want sent", identity, settled.state)
		}
	}
	if held := host.HeldDeliveries(); len(held) != 0 {
		t.Fatalf("the consumer path left %d obligation(s) parked after the redrive landed: %+v", len(held), held)
	}
	deliveryTestStopRetries(host)
}

// TestDeliveryEventificationIsUnconditional pins that any TAGENT_EXPERIMENT_* value is inert: delivery writes the fact, never the retired KV record.
func TestDeliveryEventificationIsUnconditional(t *testing.T) {
	for _, name := range []string{
		"TAGENT_EXPERIMENT_DELIVERY_EVENTIFIED",
		"TAGENT_EXPERIMENT_REQUEST_BUDGET",
		"TAGENT_EXPERIMENT_EXEC_GENERATION",
	} {
		t.Setenv(name, "off")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	run := snapshotTestRun(t, ctx, storage)
	addr := deliveryTestAddress("session-unconditional")
	sender := newDeliveryTestSender(nil)
	host := deliveryTestHost(ctx, run, sender)
	deliveryTestInvoke(t, ctx, host, addr, "drive the one delivery path")
	settled := hostTestSettlements(t, host, addr)
	if len(settled) != 1 || settled[0].state != deliveryStateSent {
		t.Fatalf("the live path wrote %d settlement fact(s) %+v, want the one sent receipt", len(settled), settled)
	}
	if records := deliveryTestLegacyRecords(t, ctx, host); len(records) != 0 {
		t.Fatalf("the live path wrote %d retired KV record(s) %+v beside the settlement fact, want none", len(records), records)
	}
}

// boundaryAnchorAgent is the entry agent every anchor below assembles: one
// declared agent driven at its own user address.
const boundaryAnchorAgent = event.AgentID("agent-anchor")

// boundaryAnchorBody is the assistant body the driven turn generates and the
// recipient must (or must not) read, depending on the symptom.
const boundaryAnchorBody = "boundary closure anchor: the answer the recipient must finally read"

// boundaryAnchorFollowUpBody names a different body for the restart scene, so a
// later turn can never stand in for the body the stop left unregistered.
const boundaryAnchorFollowUpBody = "boundary closure anchor: a later turn beside the restart"

// boundaryAnchorWindow is the history window the budget anchor declares: 32
// tokens, the same window the request-budget delta names.
const boundaryAnchorWindow = 32

// TestSymptomAnchorDeliveryRegistrationStopsSend pins that a body whose obligation commit failed never leaves.
//
// - 提交边界拒绝 agent_output 时，正文与义务同笔失败：收件方必须收到 0 份正文与恰 1 条失败通知。
// - 场景：经 Host 正式入口驱动一轮真实 turn（提交边界被注入拒绝），终态必须为 failed，链上不得出现该 invocation 的 agent_output 正文。
// - 注入点在提交边界：写链拒绝 agent_output 这一笔，即义务无法登记且正文不落链。
// - 场景形成检查：该 invocation 的 agent_output 正文不在链上，所以“未发送”不是因为没有正文。
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSymptomAnchorDeliveryRegistrationStopsSend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	run, _ := boundaryAnchorAssemble(t, ctx, boundaryAnchorSpec(),
		boundaryAnchorAnswer(boundaryAnchorBody), storage)
	addr := boundaryAnchorAddress("session-anchor-registration")
	const invocation = event.InvocationID("inv-anchor-registration")

	refused := errors.New("the durable commit of the answer was refused at the commit boundary")
	run.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		if evt.Type == event.TypeAgentOutput {
			return "", refused
		}
		return run.commitFactChecked(ctx, evt)
	})

	sender := newBoundaryAnchorSender()
	host := boundaryAnchorHost(ctx, run, sender)
	turn, invokeErr := host.Invoke(ctx, InvokeRequest{
		InvocationID: invocation,
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "drive the registration anchor"}},
	})
	if turn.Terminal != model.KindFailed {
		t.Fatalf("the scene never formed: 提交边界拒绝 agent_output 后 turn 终态 = %v（want failed）, invoke err=%v",
			turn.Terminal, invokeErr)
	}
	if bodies := generatedBodiesOf(run.SessionFacts(addr), invocation); len(bodies) != 0 {
		t.Fatalf("the scene never formed: 提交被拒后链上仍有 %d 份正文 %+v, want 0", len(bodies), bodies)
	}

	bodyCount := sender.deliveredCount(boundaryAnchorBody)
	delivered := sender.deliveredContents()
	notices := make([]string, 0, len(delivered))
	for _, content := range delivered {
		if content != boundaryAnchorBody && strings.TrimSpace(content) != "" {
			notices = append(notices, content)
		}
	}
	if bodyCount != 0 || len(notices) != 1 {
		t.Fatalf("提交边界拒绝 agent_output 时 sender 对正文的 Send 调用数 = %d（want 0）、失败通知 = %d 条（want 恰 1 条）：义务与正文同笔提交失败时必须停发正文且提示可达\n正文=%q\nsender 收到的全部内容=%q\n结算事实=%d 条\n链上事实=%s",
			bodyCount, len(notices), boundaryAnchorBody, delivered,
			boundaryAnchorSettlementFacts(run.SessionFacts(addr)),
			boundaryAnchorFactsSummary(run.SessionFacts(addr)))
	}
}

// TestSymptomAnchorDeliveryWindowSurvivesRestart pins that a committed body still reaches the recipient after a restart.
//
// - 正文已落链而交付义务未登记时，同一存储根上的重启恢复必须补送，且只送一次（补建幂等，不双发）。
// - 场景：一个真实 run 驱动出正文并落盘后退出（从未经过宿主交付面）；用同一存储根重建 run 与 Host，恢复入口跑完后收件方读到该正文。
// - 重启场景在同一存储根上重建 run 与 Host；恢复入口是 Boot 也调用的 host.restorePendingDeliveries。
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSymptomAnchorDeliveryWindowSurvivesRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	first, _ := boundaryAnchorAssemble(t, ctx, boundaryAnchorSpec(),
		boundaryAnchorAnswer(boundaryAnchorBody), storage)
	addr := boundaryAnchorAddress("session-anchor-restart")
	const invocation = event.InvocationID("inv-anchor-restart")
	turn := boundaryAnchorDrive(t, ctx, first, addr, invocation, "drive the restart anchor")
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("the scene never formed: the driven turn ended as %v, want a completed turn with a deliverable body", turn.Terminal)
	}
	bodies := generatedBodiesOf(first.SessionFacts(addr), invocation)
	if len(bodies) != 1 || bodies[0].content != boundaryAnchorBody {
		t.Fatalf("the scene never formed: 首个进程从链上读到 %d 份正文 %+v, want the one body %q",
			len(bodies), bodies, boundaryAnchorBody)
	}
	report, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)})
	if err != nil {
		t.Fatalf("the scene never formed: the first process closed beside: %v (report %+v)", err, report)
	}

	restarted, _ := boundaryAnchorAssemble(t, ctx, boundaryAnchorSpec(),
		boundaryAnchorAnswer(boundaryAnchorFollowUpBody), storage)
	restoredFacts := restarted.SessionFacts(addr)
	if !boundaryAnchorFactsCarry(restoredFacts, boundaryAnchorBody) {
		t.Fatalf("the scene never formed: 重启后的链上读不到正文 %q，facts=%s",
			boundaryAnchorBody, boundaryAnchorFactsSummary(restoredFacts))
	}
	sender := newBoundaryAnchorSender()
	host := boundaryAnchorHost(ctx, restarted, sender)
	if err := host.restorePendingDeliveries(ctx); err != nil {
		t.Fatalf("restart restore refused: %v", err)
	}

	wait := 3 * time.Second
	deadline := time.Now().Add(wait)
	for sender.deliveredCount(boundaryAnchorBody) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := sender.deliveredCount(boundaryAnchorBody)
	if got != 1 {
		t.Fatalf("agent_output 事实已在首个进程落盘、交付义务未登记；重启后 %s 内收件方收到该正文 %d 份，want 1（重启恢复必须读链补送并送达）——W0 基线红=缺陷在位\n正文=%q\nsender 收到的全部内容=%q\n结算事实=%d 条\n重启链上的事实=%s",
			wait, got, boundaryAnchorBody, sender.deliveredContents(),
			boundaryAnchorSettlementFacts(restoredFacts),
			boundaryAnchorFactsSummary(restoredFacts))
	}
}

// TestSymptomAnchorRetryAutoRedrive pins that an explicit transient failure is redriven without ApproveDelivery.
//
// - 场景：首次 Send 返回明确的暂时性错误（一个可判定的 net.Error 形状），随后通道恢复；系统必须在退避窗口内自动重发。
// - 不调用 ApproveDelivery，收件方最终必须收到正文恰一次。
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestSymptomAnchorRetryAutoRedrive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	run, _ := boundaryAnchorAssemble(t, ctx, boundaryAnchorSpec(),
		boundaryAnchorAnswer(boundaryAnchorBody))
	sender := newBoundaryAnchorSender()
	sender.transient = boundaryAnchorTransientError{}
	sender.failOnce = map[string]int{boundaryAnchorBody: 1}
	host := boundaryAnchorHost(ctx, run, sender)
	addr := boundaryAnchorAddress("session-anchor-retry")
	if _, err := host.Invoke(ctx, InvokeRequest{
		InvocationID: "inv-anchor-retry",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "drive the retry anchor"}},
	}); err != nil {
		t.Fatalf("drive the retry anchor: %v", err)
	}
	if attempts := sender.attemptCount(boundaryAnchorBody); attempts == 0 {
		t.Fatalf("the scene never formed: the sender was never asked to take the body %q", boundaryAnchorBody)
	}

	wait := 5 * time.Second
	deadline := time.Now().Add(wait)
	for sender.deliveredCount(boundaryAnchorBody) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := sender.deliveredCount(boundaryAnchorBody)
	if got != 1 {
		t.Fatalf("首次 Send 返回明确的暂时性错误后 %s 内收件方收到正文 %d 份，want 1（系统应在退避后自动重发，无需人工 ApproveDelivery）——W0 基线红=缺陷在位\n正文=%q\nSend 尝试=%q\n成功送达=%q\n仍待人工处置的持有=%d 条",
			wait, got, boundaryAnchorBody, sender.attemptedContents(), sender.deliveredContents(), len(host.HeldDeliveries()))
	}
}

// TestSymptomAnchorUnifiedRequestBudget pins that the declared window bounds the final request as one total.
//
// - 场景：经真实 AgentSpec 与正式装配链驱动一轮，历史窗口声明 32 token，固定提示约 10KB。
// - 最终请求必须被统一预算约束：要么降级到窗口内，要么显式拒发；不得静默全量进入。
// - 显式拒发是合法出口，但“没有请求”本身还不够，拒绝必须被观测到。
//
// 契约: docs/wiki/agent/agent-architecture.md#request-budget
func TestSymptomAnchorUnifiedRequestBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const fixedPromptBytes = 10 << 10
	line := "the fixed prompt line of the boundary closure anchor. "
	promptBody := strings.Repeat(line, fixedPromptBytes/len(line)+1)[:fixedPromptBytes]
	root := t.TempDir()
	promptPath := filepath.Join(root, "system.md")
	if err := os.WriteFile(promptPath, []byte(promptBody), 0o644); err != nil {
		t.Fatalf("write the fixed prompt: %v", err)
	}

	spec := boundaryAnchorSpec()
	spec.MaxTokens = boundaryAnchorWindow
	spec.Files = []config.FileRef{{Path: promptPath, Kind: config.FileKindPrompt}}
	run, provider := boundaryAnchorAssemble(t, ctx, spec, boundaryAnchorAnswer("the request was served"))
	addr := boundaryAnchorAddress("session-anchor-budget")
	turn, invokeErr := run.Invoke(ctx, InvokeRequest{
		InvocationID: "inv-anchor-budget",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "drive the budget anchor"}},
	})

	provider.mu.Lock()
	requests := append([]model.Request(nil), provider.requests...)
	provider.mu.Unlock()

	if len(requests) == 0 {
		cause := strings.TrimSpace(strings.Join(nonEmptyStrings(
			errText(invokeErr), turn.Failure,
		), " "))
		if !boundaryAnchorNamesBudgetRefusal(cause) {
			t.Fatalf("no model request was issued and no explicit budget refusal was reported: invoke err=%v, turn terminal=%v, failure=%q",
				invokeErr, turn.Terminal, turn.Failure)
		}
		t.Logf("the request was explicitly refused at the budget boundary: %s", cause)
		return
	}

	request := requests[len(requests)-1]
	chars, summary := boundaryAnchorRequestChars(request)
	tokens := (chars + replay.BudgetCharsPerToken - 1) / replay.BudgetCharsPerToken
	if tokens > boundaryAnchorWindow {
		t.Fatalf("历史窗口声明 %d token、固定提示 %d bytes；最终模型请求估算 %d token（%d chars, messages=%d, tools=%d），want <= %d：固定提示全量进入请求，预算只约束了历史投影——W0 基线红=缺陷在位\n请求消息=%s\n固定提示开头=%q",
			boundaryAnchorWindow, len(promptBody), tokens, chars, len(request.Messages), len(request.Tools),
			boundaryAnchorWindow, summary, boundaryAnchorPreview(promptBody, 120))
	}
}

// boundaryAnchorSpec is the one declared agent every delivery anchor assembles.
func boundaryAnchorSpec() config.AgentSpec {
	return config.AgentSpec{ID: string(boundaryAnchorAgent), Model: "model-anchor"}
}

// boundaryAnchorAssemble assembles one run over a scripted provider whose every
// decision answers with the body the closure returns, with the anchor's default
// governance and the caller's options.
func boundaryAnchorAssemble(t *testing.T, ctx context.Context, spec config.AgentSpec,
	answer func(*model.Request) *model.Response, opts ...Option) (*Run, *snapshotTestModel) {
	t.Helper()
	if spec.ID == "" {
		spec.ID = string(boundaryAnchorAgent)
	}
	if spec.Model == "" {
		spec.Model = "model-anchor"
	}
	provider := &snapshotTestModel{answer: answer}
	decl := &config.Declaration{Entry: spec.ID, Agents: []config.AgentSpec{spec}}
	options := append([]Option{WithGovernance(snapshotTestGovernance{})}, opts...)
	run, err := Assemble(ctx, decl, map[event.AgentID]NativeAgent{
		event.AgentID(spec.ID): {Model: provider},
	}, options...)
	if err != nil {
		t.Fatalf("assemble the anchor runtime: %v", err)
	}
	return run, provider
}

// boundaryAnchorAnswer returns a scripted decision that streams one assistant
// body and completes.
func boundaryAnchorAnswer(body string) func(*model.Request) *model.Response {
	return func(*model.Request) *model.Response {
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: body}}},
			Done:    true,
		}
	}
}

// boundaryAnchorAddress names one complete address at the anchor agent.
func boundaryAnchorAddress(session string) event.Address {
	return event.Address{AgentID: boundaryAnchorAgent, SessionID: event.SessionID(session)}
}

// boundaryAnchorDrive drives one invocation through the formal run entry and
// answers the turn it served.
func boundaryAnchorDrive(t *testing.T, ctx context.Context, run *Run, addr event.Address,
	invocation event.InvocationID, input string) agent.Turn {
	t.Helper()
	turn, err := run.Invoke(ctx, InvokeRequest{
		InvocationID: invocation,
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	})
	if err != nil {
		t.Fatalf("drive the anchor turn: %v", err)
	}
	return turn
}

// boundaryAnchorHost builds the host face the anchors drive: the assembled run,
// the published entry and an empty review table. The outbound sender is the one
// the anchor hands in, so delivery observations read only what the recipient saw.
func boundaryAnchorHost(ctx context.Context, run *Run, sender MessageSender) *Host {
	entry := boundaryAnchorAgent
	if published := run.publishedEntry(); published != "" {
		entry = published
	}
	return &Host{
		ctx:               ctx,
		run:               run,
		entry:             entry,
		sender:            sender,
		pendingDeliveries: map[string]*heldDelivery{},
		userAddresses:     map[event.SessionID]event.Address{},
		userTurns:         map[event.Address]int{},
	}
}

// boundaryAnchorTransientError is an explicit definite channel failure: the
// channel refused the action outright (connection reset before any send), so
// the failure is known-bad, never unknown. It deliberately does NOT report the
// timeout face: a timeout-shaped error means "result unknown" and belongs to
// the manual-disposition negative anchor (TestUnknownResultNeverAutoRedriven),
// not to this auto-retry anchor (grill r1 F9: the two classes must be
// disjoint or no classifier can be pinned by tests).
type boundaryAnchorTransientError struct{}

// Error renders the definite failure the anchor hands the sender.
func (boundaryAnchorTransientError) Error() string {
	return "the outbound channel refused the connection before the send began (definite, not a timeout)"
}

// Timeout reports false: this failure is definite, not a timeout.
func (boundaryAnchorTransientError) Timeout() bool { return false }

// Temporary reports the definite failure as retryable.
func (boundaryAnchorTransientError) Temporary() bool { return true }

// boundaryAnchorSender is the recipient stand-in: it records every attempt and
// every body it accepted, and can fail the first n attempts at one body with a
// declared error.
type boundaryAnchorSender struct {
	mu        sync.Mutex
	transient error
	failOnce  map[string]int
	attempts  []string
	delivered []string
}

// newBoundaryAnchorSender returns a recipient that accepts every body.
func newBoundaryAnchorSender() *boundaryAnchorSender {
	return &boundaryAnchorSender{failOnce: map[string]int{}}
}

// Send records one attempt and answers the declared transient failure until the
// failure budget of that body is spent; every later attempt is accepted.
func (s *boundaryAnchorSender) Send(_ context.Context, _ event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, content)
	if s.failOnce[content] > 0 {
		s.failOnce[content]--
		if s.transient != nil {
			return s.transient
		}
		return errors.New("the outbound channel refused the delivery")
	}
	s.delivered = append(s.delivered, content)
	return nil
}

// attemptCount reports how often the sender was asked to take one body.
func (s *boundaryAnchorSender) attemptCount(body string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, content := range s.attempts {
		if content == body {
			count++
		}
	}
	return count
}

// deliveredCount reports how many copies of one body the recipient accepted.
func (s *boundaryAnchorSender) deliveredCount(body string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, content := range s.delivered {
		if content == body {
			count++
		}
	}
	return count
}

// attemptedContents reports every body the sender was asked to take, in order.
func (s *boundaryAnchorSender) attemptedContents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.attempts...)
}

// deliveredContents reports every body the recipient accepted, in order.
func (s *boundaryAnchorSender) deliveredContents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.delivered...)
}

// boundaryAnchorFactsCarry reports whether one committed chain holds the named
// assistant body as a fact the model/user face can read.
func boundaryAnchorFactsCarry(facts []event.Event, body string) bool {
	for _, fact := range facts {
		if fact.Message.Role == model.RoleAssistant && strings.TrimSpace(fact.Message.Content) == body {
			return true
		}
	}
	return false
}

// boundaryAnchorSettlementFacts reports how many settlement facts one chain
// holds, so a failure message reads the account beside the bodies.
func boundaryAnchorSettlementFacts(facts []event.Event) int {
	count := 0
	for _, fact := range facts {
		if fact.Type == deliverySettlementType {
			count++
		}
	}
	return count
}

// boundaryAnchorFactsSummary renders the types and bodies of one chain for a
// failure message without dumping a whole prompt.
func boundaryAnchorFactsSummary(facts []event.Event) string {
	parts := make([]string, 0, len(facts))
	for _, fact := range facts {
		parts = append(parts, fmt.Sprintf("%s:%q", fact.Type, boundaryAnchorPreview(fact.Message.Content, 60)))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// boundaryAnchorRequestChars reports the character size of the final request
// over all known input faces and a compact per-message summary. Tools are
// measured by their declaration name, description and input schema.
func boundaryAnchorRequestChars(request model.Request) (int, string) {
	chars := 0
	var summary strings.Builder
	for _, message := range request.Messages {
		chars += len(message.Content) + len(message.ReasoningContent)
		for _, call := range message.ToolCalls {
			chars += len(call.Function.Name) + len(call.Function.Arguments)
		}
		fmt.Fprintf(&summary, "[%s:%d]", message.Role, len(message.Content))
	}
	for _, tool := range request.Tools {
		if tool == nil {
			continue
		}
		declaration := tool.Declaration()
		if declaration == nil {
			continue
		}
		chars += len(declaration.Name) + len(declaration.Description)
		if raw, err := json.Marshal(declaration.InputSchema); err == nil {
			chars += len(raw)
		}
	}
	return chars, summary.String()
}

// boundaryAnchorNamesBudgetRefusal reports whether one failure names the budget
// boundary explicitly enough to stand for the "refuse rather than silently send
// the whole request" outcome the request-budget delta allows.
func boundaryAnchorNamesBudgetRefusal(cause string) bool {
	lower := strings.ToLower(cause)
	for _, marker := range []string{"budget", "token", "window", "limit", "exceed", "refus", "reject", "预算", "超限", "拒绝"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// boundaryAnchorPreview bounds one body for a failure message.
func boundaryAnchorPreview(body string, max int) string {
	if len(body) <= max {
		return body
	}
	return body[:max] + "…"
}

// errText renders an error for a message, and the empty string beside no error.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// nonEmptyStrings drops the empty entries of one list.
func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
