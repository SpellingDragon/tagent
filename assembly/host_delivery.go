// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
// 契约: docs/FACT-VOCABULARY.md#families
//
// The delivery account of the host: the assistant bodies one drive generates
// become delivery obligations on the fact chain itself before any body leaves,
// and a restart resumes every obligation the chain still names.
//
// - owed 声明嵌在 agent_output 的 Meta 里，随答案同一笔提交：链上不会出现“答案已落链而义务未立”的形态。
// - 结算事实族 delivery.settlement 追加式落链：键 = delivery.<交付身份>.<终态>，终态 ∈ {held.<reason>, sent, refused, headless-retired} ∪ 迁移专用 owed；同（身份,终态）键重放不添第二条，同键异内容按既有事实键冲突拒收；held 的 reason ∈ {approval, retrying, retry-exhausted, unknown}。
// - DeliveryConsumer 是消费挂点：读链驱动的、自持计时的重试状态机（1s/4s/16s，上限 3 次；明确失败才自动重试，纯超时/不可辨归未知不重发）。
// - 恢复只读链：补送范围 = 无结算事实 ∪ held(retrying)，已确认 sent 不重发（at-least-once：发过未销账的窗口内可能重发一次，收方按交付身份去重）。
// - 旁路 KV 记录保留迁移读：迁移把记录转成事实并保留只读审计；事件化是唯一路径，没有回退账本，也没有开关。
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// failureNoticeMaxBytes bounds the raw cause one failure notice carries: a
// provider error may run long, so the notice keeps its first 2 KiB and marks
// the cut instead of letting the channel refuse the whole notice.
const failureNoticeMaxBytes = 2 << 10

// failureNoticeTruncated marks a failure notice whose cause was cut at
// failureNoticeMaxBytes.
const failureNoticeTruncated = "（已截断）"

// failureNoticeSummary is the summary prefix of the fact one failure notice
// leaves in the session chain: the audit line a later reader finds the notice
// by.
const failureNoticeSummary = "failure-notice"

// deliverySettlementType is the frozen event type of the delivery settlement
// family. It is the spelling the request side maps onto a role
// (runtime/agent/request_assembly.go, projectionTypeDeliverySettlement) and the
// lifecycle policy exempts from TTL (memory/ledger/lifecycle_config.go): the
// three spellings must stay byte-identical.
const deliverySettlementType = "delivery.settlement"

// deliveryFactPrefix heads every settlement fact key: the family locator a
// reader selects the family's returned set by.
const deliveryFactPrefix = "delivery."

// deliveryStateOwed, deliveryStateHeld, deliveryStateSent, deliveryStateRefused
// and deliveryStateHeadlessRetired are the settlement states. A fact key carries
// the state (a state transition is a new fact, never a rewrite): the four
// delivery-closure states beside the migration-only owed state, whose body a
// historical output left owed.
const (
	// deliveryStateOwed is the migration-only state of a historical output the
	// old account left owed and never sent. It is not a regular settlement
	// terminal: the recovery treats it as still owed.
	deliveryStateOwed = "owed"
	// deliveryStateHeld is the waiting state. Its token always carries the
	// reason (held.<reason>): the fact key cannot be rewritten, and two holds
	// of one identity under different reasons are two facts, not one fact whose
	// content changed.
	deliveryStateHeld = "held"
	// deliveryStateSent is the receipt of a discharged obligation: the channel
	// accepted the body and the account confirmed it.
	deliveryStateSent = "sent"
	// deliveryStateRefused is the receipt of an obligation given up by a
	// refusal, beside its audit fact.
	deliveryStateRefused = "refused"
	// deliveryStateHeadlessRetired is the receipt of a body generated at an
	// address that is not the entry: it was never going to a message channel,
	// so it is retired without a send instead of being delivered beside a
	// restart.
	deliveryStateHeadlessRetired = "headless-retired"
)

// deliveryReasonApproval, deliveryReasonRetrying, deliveryReasonRetryExhausted
// and deliveryReasonUnknown name the held reasons: the model distinguishes
// "waiting for approval", "retrying" and "given up" by them, and the delayed
// notice layers its wording by them.
const (
	deliveryReasonApproval       = "approval"
	deliveryReasonRetrying       = "retrying"
	deliveryReasonRetryExhausted = "retry-exhausted"
	deliveryReasonUnknown        = "unknown"
)

// deliveryRetryLimit is the bounded number of automatic redrives one obligation
// receives after its first definite failure.
const deliveryRetryLimit = 3

// deliveryBackoffUnit is the base wait of the redrive: the backoff sequence is
// 1s/4s/16s (unit shifted by twice the retries already performed).
const deliveryBackoffUnit = time.Second

// deliverySettlementSummary heads the audit line every settlement fact carries.
const deliverySettlementSummary = "delivery settlement"

// deliveryMetaTargetAgent and the other pass-through metadata entries one
// settlement fact carries are business metadata (event.MetaPrefix), never
// projected: the model reads the one-line content of a settlement fact, and the
// body a body-carrying state keeps for the recovery stays behind the read face.
const (
	deliveryMetaTargetAgent   = event.MetaPrefix + "delivery_target_agent"
	deliveryMetaTargetSession = event.MetaPrefix + "delivery_target_session"
	deliveryMetaIdentity      = event.MetaPrefix + "delivery_identity"
	deliveryMetaState         = event.MetaPrefix + "delivery_state"
	deliveryMetaReason        = event.MetaPrefix + "delivery_reason"
	deliveryMetaHeldAt        = event.MetaPrefix + "delivery_held_at"
	deliveryMetaLineage       = event.MetaPrefix + "delivery_lineage"
	deliveryMetaBody          = event.MetaPrefix + "delivery_body"
	deliveryMetaMigrated      = event.MetaPrefix + "delivery_migrated_record"
	deliveryMetaAction        = event.MetaPrefix + "delivery_action"
)

// deliveryOwedMeta and the other owed declaration entries an agent_output fact
// carries (runtime/agent loop.go stamps them, commitOutput) are the plain "1"
// marker, the target address the body is owed to, and the lineage the turn
// opened on. They must stay byte-identical to the producer's spellings.
const (
	deliveryOwedMeta          = event.MetaPrefix + "delivery_owed"
	deliveryTargetAgentMeta   = event.MetaPrefix + "delivery_target_agent"
	deliveryTargetSessionMeta = event.MetaPrefix + "delivery_target_session"
	deliveryLineageMeta       = event.MetaPrefix + "delivery_lineage"
)

// heldStateOf renders the settlement state token of one hold reason: the state
// segment carries the reason because the fact key is immutable.
func heldStateOf(reason string) string {
	if reason == "" {
		reason = deliveryReasonApproval
	}
	return deliveryStateHeld + "." + reason
}

// heldReasonOf reports the reason one held state token names.
func heldReasonOf(state string) (string, bool) {
	if !strings.HasPrefix(state, deliveryStateHeld+".") {
		return "", false
	}
	reason := strings.TrimPrefix(state, deliveryStateHeld+".")
	if reason == "" {
		return "", false
	}
	return reason, true
}

// deliveryStateKeepsBody reports whether the settlement fact of one state keeps
// the obligation's body beside its state: every unsettled state does, because
// the recovery resends the body from the fact alone, and a retired headless
// state does, because the body it refuses to send anymore stays on file.
func deliveryStateKeepsBody(state string) bool {
	return deliveryStateUnsettled(state) || state == deliveryStateHeadlessRetired
}

// deliveryStateUnsettled reports whether one settlement state still owes the
// body: the migration-only owed state and every held state. A sent, refused or
// retired state is settled.
func deliveryStateUnsettled(state string) bool {
	if state == deliveryStateOwed {
		return true
	}
	_, held := heldReasonOf(state)
	return held
}

// deliveryStateSettled reports whether one settlement state closes the
// obligation: the body is delivered (sent), given up (refused) or retired
// (headless-retired).
func deliveryStateSettled(state string) bool {
	switch state {
	case deliveryStateSent, deliveryStateRefused, deliveryStateHeadlessRetired:
		return true
	}
	return false
}

// deliverySettlementStates lists every state token one settlement fact can
// carry: the exact keys the recovery probes for one obligation outside the hot
// window, and the state line of an obligation it reconstructs.
func deliverySettlementStates() []string {
	return []string{
		deliveryStateOwed,
		heldStateOf(deliveryReasonApproval),
		heldStateOf(deliveryReasonRetrying),
		heldStateOf(deliveryReasonRetryExhausted),
		heldStateOf(deliveryReasonUnknown),
		deliveryStateSent,
		deliveryStateRefused,
		deliveryStateHeadlessRetired,
	}
}

// deliverySettlementKey composes the canonical key of one settlement fact: the
// family prefix beside the delivery identity beside the terminal state token.
// The identity is the pure string the delivery layer derives (a generated
// body's obligation is hostDeliveryFreshPrefix + the fact key), so the key is
// stable across processes: a restart reads the same settlement facts the first
// process wrote.
func deliverySettlementKey(identity, state string) event.EventID {
	return event.EventID(deliveryFactPrefix + identity + "." + state)
}

// deliveryRetryPrefix heads the state token of one retry attempt fact. The
// attempt facts share the settlement family (the same frozen type, the same TTL
// exemption) but are not settlement states: they record how many redrives the
// chain already scheduled so a restart continues the count instead of resetting
// it to zero. The key carries the attempt number, so one attempt is one
// immutable fact and a replayed schedule lands no second copy.
const deliveryRetryPrefix = "retry."

// deliveryRetryState renders the state token of one retry attempt fact.
func deliveryRetryState(attempt int) string {
	return deliveryRetryPrefix + strconv.Itoa(attempt)
}

// deliveryRetryAttemptOf reads one retry attempt fact back into its identity and
// attempt number, and reports whether the fact is one at all.
func deliveryRetryAttemptOf(evt event.Event) (string, int, bool) {
	if evt.Type != deliverySettlementType {
		return "", 0, false
	}
	identity, state, ok := deliveryIdentityOfSettlementKey(evt.Key)
	if !ok || !strings.HasPrefix(state, deliveryRetryPrefix) {
		return "", 0, false
	}
	attempt, err := strconv.Atoi(strings.TrimPrefix(state, deliveryRetryPrefix))
	if err != nil || attempt < 0 {
		return "", 0, false
	}
	return identity, attempt, true
}

// deliveryIdentityOfSettlementKey splits one settlement fact key back into the
// delivery identity and the state token. The identity never carries a dot (it
// is a prefix beside one canonical event key or a host-local sequence word), so
// the first dot after the identity opens the state token and a held token keeps
// its own dot after it.
func deliveryIdentityOfSettlementKey(key event.EventID) (string, string, bool) {
	text := string(key)
	if !strings.HasPrefix(text, deliveryFactPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(text, deliveryFactPrefix)
	at := strings.IndexByte(rest, '.')
	if at <= 0 || at == len(rest)-1 {
		return "", "", false
	}
	return rest[:at], rest[at+1:], true
}

// addressLabel renders one address for a fact line or an audit note.
func addressLabel(addr event.Address) string {
	return string(addr.AgentID) + "@" + string(addr.SessionID)
}

// deliverySettlementLine renders the one readable line a settlement fact
// carries: the state, the identity, the target and the body size. It is
// deterministic — nothing volatile enters it — so a replayed fact and the fact
// it replays spell one line, and the model reads a state change instead of a
// log.
func deliverySettlementLine(identity, state string, delivery plugin.Delivery) string {
	line := "delivery " + strings.ReplaceAll(state, ".", " ") + ": " + identity
	if delivery.Target.IsComplete() {
		line += " -> " + addressLabel(delivery.Target)
	}
	line += fmt.Sprintf(" (%d bytes)", len(delivery.Content))
	if reason, held := heldReasonOf(state); held {
		line += " reason=" + reason
	}
	return line
}

// deliverySettlement is one reconstructed settlement fact: the identity, the
// state token, the fact it was read from, the delivery the fact names and the
// hold moment a held state recorded.
type deliverySettlement struct {
	identity string
	state    string
	fact     event.Event
	delivery plugin.Delivery
	heldAt   time.Time
}

// settlementOf reconstructs one settlement fact, or reports that the fact is
// not a settlement fact of this family.
func settlementOf(evt event.Event) (deliverySettlement, bool) {
	if evt.Type != deliverySettlementType {
		return deliverySettlement{}, false
	}
	identity, state, ok := deliveryIdentityOfSettlementKey(evt.Key)
	if !ok {
		return deliverySettlement{}, false
	}
	if strings.HasPrefix(state, deliveryRetryPrefix) {
		return deliverySettlement{}, false
	}
	target := event.Address{
		AgentID:   event.AgentID(evt.Meta[deliveryMetaTargetAgent]),
		SessionID: event.SessionID(evt.Meta[deliveryMetaTargetSession]),
	}
	if target.IsZero() {
		target = evt.Owner
	}
	heldAt, _ := time.Parse(time.RFC3339Nano, evt.Meta[deliveryMetaHeldAt])
	return deliverySettlement{
		identity: identity,
		state:    state,
		fact:     evt,
		delivery: plugin.Delivery{
			Content: evt.Meta[deliveryMetaBody],
			Lineage: evt.Meta[deliveryMetaLineage],
			Target:  target,
		},
		heldAt: heldAt,
	}, true
}

// hostDeliveryPrefix locates the read-only delivery record of every obligation:
// the six-state account the settlement facts supersede. It stays readable as
// audit and is the migration input; no live path writes it.
const hostDeliveryPrefix = "tagent/task/v1/delivery:"

// hostDeliveryFreshPrefix names the obligation of one generated body: the
// record is keyed by the identity of the fact that carries the body, so the
// obligation a restart reads and the one the live path read name one identity.
const hostDeliveryFreshPrefix = "delivery-fresh-"

// hostDeliveryOwed, hostDeliveryHeld, hostDeliveryRetry, hostDeliverySent,
// hostDeliveryRefused and hostDeliveryHeadless are the read-only record states
// (the six-state migration input).
const (
	hostDeliveryOwed     = "owed"
	hostDeliveryHeld     = "held"
	hostDeliveryRetry    = "retry"
	hostDeliverySent     = "sent"
	hostDeliveryRefused  = "refused"
	hostDeliveryHeadless = "headless"
)

// hostDeliveryRecord is the read-only durable form of one delivery obligation:
// the migration input. The settlement account never writes it.
type hostDeliveryRecord struct {
	ID      string        `json:"id"`
	Content string        `json:"content"`
	Lineage string        `json:"lineage"`
	Target  event.Address `json:"target"`
	Status  string        `json:"status"`
	HeldAt  time.Time     `json:"held_at"`
}

// HeldDelivery is one outbound body the review plugin held before the sending
// plugin delivered it: the identity an approval or refusal names, the delivery
// semantic the ruler read, and the moment the hold was recorded.
type HeldDelivery struct {
	// ID names this hold and is the identity ApproveDelivery and
	// RefuseDelivery resolve.
	ID string

	// Delivery is the semantic the ruler held.
	Delivery plugin.Delivery

	// HeldAt is the moment the host recorded the hold.
	HeldAt time.Time
}

// heldDelivery is the host-side record of one held delivery: the public
// identity beside the order the hold was taken in.
type heldDelivery struct {
	id       string
	delivery plugin.Delivery
	heldAt   time.Time
}

// HeldDeliveries reports the bodies the host owes a target before delivery:
// the holds the review plugin parked and the redeliveries a refused send owes,
// oldest first. The result is a copy the caller owns: reading here changes no
// hold, and only ApproveDelivery or RefuseDelivery resolves one. The table is
// rebuilt from the settlement facts on every boot, so a hold survives a restart
// under the identity it was recorded with.
func (h *Host) HeldDeliveries() []HeldDelivery {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	held := make([]HeldDelivery, 0, len(h.pendingDeliveries))
	for _, entry := range h.pendingDeliveries {
		held = append(held, HeldDelivery{ID: entry.id, Delivery: entry.delivery, HeldAt: entry.heldAt})
	}
	h.mu.Unlock()
	sort.Slice(held, func(i, j int) bool { return held[i].ID < held[j].ID })
	return held
}

// ApproveDelivery re-delivers one held body exactly once: the body is sent to
// the target it named, and the hold is consumed, so a repeated approval names
// no hold and delivers no second copy. A send the channel refuses restores the
// hold for a later decision and is reported to the caller; a definite refusal
// also re-enters the redrive, so an approval that meets a transient outage is
// carried by the same bounded backoff the live path uses.
//
// The receipt of the approval lands only after the send it confirms returned: a
// receipt the account refuses leaves the obligation owed and answers the landed
// delivery by name, so the caller learns the body reached the channel and the
// account did not — a restart delivers the body once more, which is the one
// extra copy an unconfirmed receipt tolerates.
func (h *Host) ApproveDelivery(ctx context.Context, id string) error {
	if h == nil {
		return ErrHostClosed
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return ErrHostClosed
	}
	held, ok := h.pendingDeliveries[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDeliveryNotFound, id)
	}
	sender := h.sender
	delete(h.pendingDeliveries, id)
	h.mu.Unlock()
	if sender == nil {
		missing := fmt.Errorf("%w: the held delivery of %s", ErrHostSenderMissing, id)
		return errors.Join(missing, h.restoreHeldDelivery(ctx, held, heldStateOf(deliveryReasonApproval)))
	}
	if err := sender.Send(ctx, held.delivery.Target, held.delivery.Content); err != nil {
		refused := fmt.Errorf("tagent: the approved delivery of %s was refused: %w", id, err)
		state, definite := deliveryFailureState(err)
		if definite {
			h.scheduleRedrive(ctx, id, held.delivery)
		}
		return errors.Join(refused, h.restoreHeldDelivery(ctx, held, state))
	}
	if err := h.settleDelivery(ctx, id, held.delivery); err != nil {
		landed := fmt.Errorf("tagent: the delivery of %s landed but its receipt did not: %w", id, err)
		return errors.Join(landed, h.restoreHeldDelivery(ctx, held, heldStateOf(deliveryReasonApproval)))
	}
	assemblyInfo("tagent: the held delivery of %s was approved and sent", id)
	return nil
}

// RefuseDelivery discards one held body: nothing is delivered, the hold is
// consumed, and an audit feedback fact without a model payload records the
// refusal at the target address. The settlement fact records the refusal as
// well, so no later boot owes the body again.
//
// Both commits are checked before the refusal publishes: a receipt or an audit
// fact the chain refused answers the caller by name and leaves the body in the
// review table, so a refusal no account accepted is never read as a decision.
func (h *Host) RefuseDelivery(ctx context.Context, id string) error {
	if h == nil {
		return ErrHostClosed
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return ErrHostClosed
	}
	held, ok := h.pendingDeliveries[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDeliveryNotFound, id)
	}
	delete(h.pendingDeliveries, id)
	h.mu.Unlock()
	if err := h.writeDeliveryState(ctx, id, held.delivery, deliveryStateRefused, held.heldAt); err != nil {
		unrecorded := fmt.Errorf("tagent: the refusal of the held delivery %s was not recorded: %w", id, err)
		return errors.Join(unrecorded, h.restoreHeldDelivery(ctx, held, heldStateOf(deliveryReasonApproval)))
	}
	h.cancelRedrive(id)
	if err := h.noteDeliveryRefusal(ctx, held); err != nil {
		return errors.Join(err, h.restoreHeldDelivery(ctx, held, heldStateOf(deliveryReasonApproval)))
	}
	assemblyInfo("tagent: the held delivery of %s was refused", id)
	return nil
}

// deliveryDurable reports the fact surface the retired delivery records are read
// from: the storage tier the run opened, and nil beside a run that keeps no
// tier. The migration is its one reader; the live account reads and writes the
// chain instead.
func (h *Host) deliveryDurable() state.FactLog {
	if h == nil {
		return nil
	}
	if h.deliveryLog != nil {
		return h.deliveryLog
	}
	if h.run == nil || h.run.tier == nil {
		return nil
	}
	return ledger.TierFactLog(h.run.tier)
}

// settlementFactOf reports one settlement fact by its exact key: the live fact
// when this process still holds it, and the committed body the storage tier
// keeps otherwise. A fact the hot window trimmed is still settled — the newest
// state of an obligation is never read as "no settlement" just because the
// window moved — so the probe is a point read on the chain, never a scan.
//
// A key the chain genuinely does not hold answers held=false with no error; a
// read that failed (corruption, IO) answers a named error instead, so a caller
// never mistakes an unreadable settlement for an absent one and resends a body
// the account already closed.
func (h *Host) settlementFactOf(ctx context.Context, key event.EventID) (event.Event, bool, error) {
	if h == nil || h.run == nil || key == "" {
		return event.Event{}, false, nil
	}
	if evt, held := h.run.factOf(key); held {
		return evt, true, nil
	}
	if h.run.tier == nil {
		return event.Event{}, false, nil
	}
	restored, err := h.run.CommittedEvent(ctx, key)
	if err != nil {
		if errors.Is(err, ErrCommittedFactMissing) {
			return event.Event{}, false, nil
		}
		return event.Event{}, false, fmt.Errorf("tagent: the settlement fact %s could not be read: %w", key, err)
	}
	return restored, true, nil
}

// settlementStateOf reports the newest settlement state of one obligation, from
// the live window first and from the committed chain behind it. The probe walks
// the eight state keys of the family — a point read each — so an obligation
// whose facts left the hot window is still read as it stands instead of being
// mistaken for an unregistered one.
func (h *Host) settlementStateOf(ctx context.Context, identity string) (deliverySettlement, bool, error) {
	if h == nil || identity == "" {
		return deliverySettlement{}, false, nil
	}
	var newest deliverySettlement
	found := false
	for _, state := range deliverySettlementStates() {
		evt, held, err := h.settlementFactOf(ctx, deliverySettlementKey(identity, state))
		if err != nil {
			return deliverySettlement{}, false, err
		}
		if !held {
			continue
		}
		settlement, ok := settlementOf(evt)
		if !ok {
			continue
		}
		if !found || settlement.fact.Timestamp.After(newest.fact.Timestamp) {
			newest = settlement
			found = true
		}
	}
	return newest, found, nil
}

// deliveryStateOf reports the latest settlement state this host already knows
// for one obligation and whether it knows one at all. The table only spares the
// chain read: a state it does not hold is probed from the chain by the caller.
func (h *Host) deliveryStateOf(identity string) (string, bool) {
	if h == nil || identity == "" {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	state, held := h.deliveryStates[identity]
	return state, held
}

// restoredAccount reports whether this host's account was restored from a
// checkpoint carrier, so the state table stands for the open account alone: the
// settled history of such an account is its cut, and one identity the table does
// not hold is answered by the settlement facts the chain keeps. An account this
// process stated itself names every identity it wrote, so it needs no probe.
func (h *Host) restoredAccount() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.restoredDelivery
}

// noteDeliveryState records the latest settlement state this host wrote or
// restored for one obligation, beside the fact key that states it, and moves
// the hot-window pin of the obligation onto the newest unsettled fact: the
// state an obligation stands in is exactly the fact the window must keep.
func (h *Host) noteDeliveryState(identity, state string) {
	if h == nil || identity == "" {
		return
	}
	key := deliverySettlementKey(identity, state)
	h.mu.Lock()
	if h.deliveryStates == nil {
		h.deliveryStates = map[string]string{}
	}
	h.deliveryStates[identity] = state
	previous := h.deliveryPins[identity]
	if deliveryStateUnsettled(state) {
		if h.deliveryPins == nil {
			h.deliveryPins = map[string]event.EventID{}
		}
		h.deliveryPins[identity] = key
	} else {
		delete(h.deliveryPins, identity)
	}
	h.mu.Unlock()
	if h.run != nil {
		if previous != "" && previous != key {
			h.run.unpinFact(previous)
		}
		if deliveryStateUnsettled(state) {
			h.run.pinFact(key)
		}
	}
}

// writeDeliverySettlement appends one settlement fact of one obligation to the
// target's chain under its canonical (identity, state) key. The write is
// idempotent by that key: a fact the chain already holds is answered without a
// second commit, so a replayed hold, a repeated approval or a second boot
// migrates nothing twice; a different content under the same key is refused by
// the chain's own conflict rule, never silently overwritten.
//
// The fact commits through the run's ordinary session commit entry, so it is in
// the same chain, the same replay sequence and the same projection as any other
// fact of the target's session: the model reads a state change of its own
// delivery, and the projection maps it under the family's frozen type.
//
//   - The settlement fact and the state token noted for it are one unit of work: the account applies the state after the fact lands, so the whole write holds the run's quiet-point fence and a cut never reads the account beside a settlement the chain already carries.
//   - The commit below reaches the cadence from inside that unit, so the cut it asks for is filed aside rather than waited for here: a cut drains the pipeline this write is part of.
func (h *Host) writeDeliverySettlement(ctx context.Context, target event.Address, identity, state string, delivery plugin.Delivery, heldAt time.Time, extra map[string]string) error {
	if h == nil || h.run == nil {
		return nil
	}
	if identity == "" {
		return errors.New("tagent: the delivery settlement names no identity")
	}
	h.run.flight.enter()
	defer h.run.flight.leave()
	ctx = markFlightHeld(ctx)
	key := deliverySettlementKey(identity, state)
	if _, held, err := h.settlementFactOf(ctx, key); err != nil {
		return err
	} else if held {
		h.noteDeliveryState(identity, state)
		return nil
	}
	meta := map[string]string{
		event.MetaKeyEventSummary:  deliverySettlementSummary,
		event.MetaKeyTriggerSource: "host",
		deliveryMetaIdentity:       identity,
		deliveryMetaState:          state,
		deliveryMetaTargetAgent:    string(delivery.Target.AgentID),
		deliveryMetaTargetSession:  string(delivery.Target.SessionID),
		deliveryMetaLineage:        delivery.Lineage,
	}
	if reason, held := heldReasonOf(state); held {
		meta[deliveryMetaReason] = reason
	}
	if !heldAt.IsZero() {
		meta[deliveryMetaHeldAt] = heldAt.UTC().Format(time.RFC3339Nano)
	}
	if deliveryStateKeepsBody(state) && delivery.Content != "" {
		meta[deliveryMetaBody] = delivery.Content
	}
	for name, value := range extra {
		meta[name] = value
	}
	evt := event.Event{
		Key:       key,
		Type:      deliverySettlementType,
		Timestamp: time.Now(),
		Message: model.Message{
			Role:    model.RoleUser,
			Content: deliverySettlementLine(identity, state, delivery),
		},
		Meta:   meta,
		Owner:  target,
		Source: target,
		Target: target,
	}
	if _, err := h.run.CommitToSession(ctx, target, evt); err != nil {
		return fmt.Errorf("tagent: the delivery settlement of %s (%s) did not commit: %w", identity, state, err)
	}
	h.noteDeliveryState(identity, state)
	return nil
}

// writeDeliveryState records one state of one obligation as its settlement fact.
// It is the one record entry every face of the account (the live send, the hold,
// the approval, the refusal, the migration) goes through, so the account never
// disagrees with itself about who wrote what.
func (h *Host) writeDeliveryState(ctx context.Context, identity string, delivery plugin.Delivery, state string, heldAt time.Time) error {
	return h.writeDeliverySettlement(ctx, delivery.Target, identity, state, delivery, heldAt, nil)
}

// settlementStateOfRetiredRecord maps one retired record state onto the settlement
// state the migration writes: owed and headless survive as themselves, the
// hold and retry records become held states, and the two receipts keep their
// names.
func settlementStateOfRetiredRecord(status string) (string, bool) {
	switch status {
	case hostDeliveryOwed:
		return deliveryStateOwed, true
	case hostDeliveryHeld:
		return heldStateOf(deliveryReasonApproval), true
	case hostDeliveryRetry:
		return heldStateOf(deliveryReasonRetryExhausted), true
	case hostDeliverySent:
		return deliveryStateSent, true
	case hostDeliveryRefused:
		return deliveryStateRefused, true
	case hostDeliveryHeadless:
		return deliveryStateHeadlessRetired, true
	default:
		return "", false
	}
}

// parkHeld registers one obligation in the review table under its identity,
// keeping the moment and the delivery the account recorded. An entry already
// parked keeps its recorded moment: the table states when the hold was taken,
// not when it was last read.
func (h *Host) parkHeld(identity string, delivery plugin.Delivery, heldAt time.Time) {
	if h == nil || identity == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pendingDeliveries == nil {
		h.pendingDeliveries = map[string]*heldDelivery{}
	}
	if _, held := h.pendingDeliveries[identity]; held {
		return
	}
	if heldAt.IsZero() {
		heldAt = time.Now()
	}
	h.pendingDeliveries[identity] = &heldDelivery{id: identity, delivery: delivery, heldAt: heldAt}
}

// dropHeld removes one obligation from the review table.
func (h *Host) dropHeld(identity string) {
	if h == nil || identity == "" {
		return
	}
	h.mu.Lock()
	delete(h.pendingDeliveries, identity)
	h.mu.Unlock()
}

// holdObligation parks one undelivered body under its identity and records the
// hold state on the account. An empty identity mints a host-local one (the
// sequence the table has always used), so a hold of a body no generation named
// still has an identity to approve or refuse. The settlement fact is written
// before the entry is parked: a stop between the two leaves an obligation the
// restart restores, never a hold no account knows.
func (h *Host) holdObligation(ctx context.Context, identity string, delivery plugin.Delivery, state string) (string, error) {
	if h == nil {
		return "", ErrHostClosed
	}
	heldAt := time.Now()
	id := identity
	if id == "" {
		id = h.mintHeldIdentity()
	}
	if err := h.writeDeliveryState(ctx, id, delivery, state, heldAt); err != nil {
		return id, err
	}
	h.parkHeld(id, delivery, heldAt)
	return id, nil
}

// retireHeadlessDelivery gives up the obligation of a body generated at an
// address that is not the entry: the host answers a headless address for the
// caller that drove it and never pushes its body to a message channel, so the
// obligation is closed as headless-retired instead of being delivered beside a
// restart. The settlement fact keeps the body for the audit.
func (h *Host) retireHeadlessDelivery(ctx context.Context, addr event.Address, identity string, delivery plugin.Delivery) {
	if err := h.writeDeliverySettlement(ctx, addr, identity, deliveryStateHeadlessRetired, delivery, time.Time{}, nil); err != nil {
		assemblyWarn("tagent: the owed delivery of %s at the headless address %s was not retired: %v", identity, addr.AgentID, err)
		return
	}
	assemblyInfo("tagent: the owed delivery of %s at the headless address %s was retired without a send", identity, addr.AgentID)
}

// settleDelivery closes one obligation as sent: the receipt lands on the chain,
// the review table drops the hold and the redrive of the identity stops. The
// receipt is written before the table entry is dropped, so a stop in between
// leaves an obligation the restart reads as settled rather than one it cannot
// see.
func (h *Host) settleDelivery(ctx context.Context, identity string, delivery plugin.Delivery) error {
	if err := h.writeDeliveryState(ctx, identity, delivery, deliveryStateSent, time.Time{}); err != nil {
		return err
	}
	h.dropHeld(identity)
	h.cancelRedrive(identity)
	return nil
}

// restoreHeldDelivery puts one body back into the review table under one hold
// state after a send or a receipt failed: the obligation stays owed, and the
// hold it was approved from is answerable again.
func (h *Host) restoreHeldDelivery(ctx context.Context, held *heldDelivery, state string) error {
	if held == nil {
		return nil
	}
	h.parkHeld(held.id, held.delivery, held.heldAt)
	return h.writeDeliveryState(ctx, held.id, held.delivery, state, held.heldAt)
}

// mintHeldIdentity mints the next host-local delivery identity: the sequence
// word the review table has always used for a body no generation named.
func (h *Host) mintHeldIdentity() string {
	h.mu.Lock()
	h.heldSeq++
	seq := h.heldSeq
	h.mu.Unlock()
	return "delivery-" + strconv.FormatUint(seq, 10)
}

// deliveryOutcome is the terminal state of one reviewed delivery: the sender
// accepted the body, the review parked it, or the channel refused it.
type deliveryOutcome int

const (
	// deliverySent reports a body the sender accepted.
	deliverySent deliveryOutcome = iota
	// deliveryHeld reports a body the review parked before the sender ran.
	deliveryHeld
	// deliveryRefused reports a body the sender refused.
	deliveryRefused
)

// hostDeliverFunc is the one delivery outlet of the host: every outbound body —
// the answer of a turn, the notice of a failed turn and the body a declared send
// tool delivers — reaches the sender through it.
type hostDeliverFunc func(ctx context.Context, delivery plugin.Delivery) (deliveryOutcome, string)

// deliverOne sends one outbound body that no generation registered an obligation
// for, under the identity the delivery takes when it is parked.
func (h *Host) deliverOne(ctx context.Context, sender MessageSender, delivery plugin.Delivery) (deliveryOutcome, string) {
	return h.deliverObligation(ctx, "", sender, delivery)
}

// deliverObligation sends one outbound body through the review plugin's ruling
// under the identity of the obligation it discharges: an allowed or recorded
// delivery is sent, a hold parks the body under that same obligation, and a
// ruling that could not be rendered parks it too instead of delivering
// unreviewed content. A send the channel refuses parks the body as the
// redelivery it owes. The second result names the cause of a hold or a refusal
// for the caller's receipt; a delivered body carries none beside the
// unconfirmed-receipt note below.
//
// An obligation the sender accepted is settled on the account behind its write
// barrier: a receipt the chain refused leaves the obligation owed, so a restart
// delivers the body once more rather than reading an unconfirmed send as
// discharged. That unconfirmed receipt is named in the second result beside the
// landed outcome, so a caller learns the body reached the channel and the
// account did not — the same note a hold carries when its own record did not
// land.
//
// - The classification of a refusal is what decides the redrive:
// - A definite refusal (the channel answered, and answered no) enters the bounded backoff of the DeliveryConsumer.
// - A failure indistinguishable from a timeout — the send may have landed — is held as unknown and never resent on its own.
func (h *Host) deliverObligation(ctx context.Context, obligation string, sender MessageSender, delivery plugin.Delivery) (deliveryOutcome, string) {
	if sender == nil {
		return deliveryRefused, "the host carries no outbound message sender"
	}
	if ruler := h.deliveryRuler; ruler != nil {
		disposition, err := ruler.Rule(ctx, delivery)
		switch {
		case err != nil:
			assemblyWarn("tagent: the review of the delivery to %s failed: %v", delivery.Target.SessionID, err)
			cause := "the delivery review failed: " + err.Error()
			_, holdErr := h.holdDelivery(ctx, obligation, delivery, cause, heldStateOf(deliveryReasonApproval))
			return deliveryHeld, cause + holdRecordNote(holdErr)
		case disposition == plugin.DispositionHold:
			cause := "the review held the delivery"
			_, holdErr := h.holdDelivery(ctx, obligation, delivery, cause, heldStateOf(deliveryReasonApproval))
			return deliveryHeld, cause + holdRecordNote(holdErr)
		case disposition == plugin.DispositionAllow, disposition == plugin.DispositionRecord:
		default:
			assemblyWarn("tagent: the review of the delivery to %s returned %q", delivery.Target.SessionID, disposition)
			cause := fmt.Sprintf("the delivery review returned %q", disposition)
			_, holdErr := h.holdDelivery(ctx, obligation, delivery, cause, heldStateOf(deliveryReasonApproval))
			return deliveryHeld, cause + holdRecordNote(holdErr)
		}
	}
	if err := sender.Send(ctx, delivery.Target, delivery.Content); err != nil {
		assemblyWarn("tagent: the host delivery to %s was refused: %v", delivery.Target.SessionID, err)
		cause := "the channel refused the delivery: " + err.Error()
		state, definite := deliveryFailureState(err)
		id, holdErr := h.holdDelivery(ctx, obligation, delivery, cause, state)
		scheduled := definite && holdErr == nil
		if scheduled {
			h.scheduleRedrive(ctx, id, delivery)
		}
		return deliveryRefused, err.Error() + retryNote(scheduled) + holdRecordNote(holdErr)
	}
	if obligation != "" {
		if err := h.settleDelivery(ctx, obligation, delivery); err != nil {
			assemblyWarn("tagent: the delivery of %s landed but its receipt did not: %v", obligation, err)
			return deliverySent, sentRecordNote(err)
		}
	}
	return deliverySent, ""
}

// deliveryFailureState maps one channel error onto the hold state the account
// records and whether the failure is definite:
// - A deadline, a cancellation or any other error that may have landed is held as unknown; the account never resends it on its own.
// - A network error that is not a timeout is the channel's definite refusal, which the bounded backoff of the DeliveryConsumer may redrive.
func deliveryFailureState(err error) (string, bool) {
	if err == nil {
		return heldStateOf(deliveryReasonUnknown), false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && !netErr.Timeout() {
		return heldStateOf(deliveryReasonRetrying), true
	}
	return heldStateOf(deliveryReasonUnknown), false
}

// retryNote renders the receipt note of an obligation that entered the redrive.
func retryNote(scheduled bool) string {
	if !scheduled {
		return ""
	}
	return "; the delivery is being redelivered"
}

// deliverFresh delivers the assistant bodies the invocation that just ended
// generated through the sender; a session the chain maps onto a projection
// commits no transcript history, so the facts are the one read face both session
// kinds share. The bodies are read by the identity of that invocation, never by a
// position of the chain: the chain is held to the hot window of its session, so a
// position read at an earlier moment names other material.
// - A body wins over a failure: a turn that produced a deliverable answer delivers it.
// - A failed turn without a body delivers the raw recorded cause; no cause stays silent.
// - Every body is an obligation of the fact chain before its send: the owed declaration committed with the body is the registration, and a restart sends it under the identity the declaration derives.
// - The live path sends every body whose obligation no settlement fact closes, and never one that carries a hold state: a hold waits for its decision.
// - Only the entry address delivers: a body generated at a headless address is retired without a send, because it belongs to the caller that drove it and never to a channel.
// - A turn with no user-visible output and an unsettled held obligation of that round delivers the delayed notice instead, once per round, never in failure wording.
func (h *Host) deliverFresh(ctx context.Context, addr event.Address, sender MessageSender, lineage string, turn agent.Turn) {
	if h == nil || h.run == nil {
		return
	}
	bodies := generatedBodiesOf(h.run.SessionFacts(addr), turn.InvocationID)
	if !h.deliversAt(addr) {
		h.retireHeadlessBodies(ctx, addr, lineage, bodies)
		return
	}
	if sender == nil {
		return
	}
	if len(bodies) == 0 {
		if turn.Terminal == model.KindFailed {
			if notice := failureNoticeText(turn.Failure); notice != "" {
				h.noteFailureNotice(ctx, addr, turn)
				h.deliverOne(ctx, sender, plugin.Delivery{Content: notice, Lineage: lineage, Target: addr})
				return
			}
			assemblyDebug("tagent: the invocation of %s failed without a recorded cause", addr.SessionID)
			return
		}
		h.deliverDelayedNotice(ctx, addr, sender, lineage)
		return
	}
	for _, body := range bodies {
		identity, ok := freshObligationOf(body)
		if !ok {
			continue
		}
		known, err := h.deliveryKnown(ctx, identity)
		if err != nil {
			assemblyWarn("tagent: the delivery of %s stands unread and is not resent: %v", identity, err)
			continue
		}
		if known {
			continue
		}
		if !h.claimDelivery(identity) {
			continue
		}
		delivery := plugin.Delivery{Content: body.content, Lineage: lineage, Target: addr}
		if _, cause := h.deliverObligation(ctx, identity, sender, delivery); cause != "" {
			assemblyDebug("tagent: the delivery to %s stands owed: %s", addr.SessionID, cause)
		}
		h.releaseDelivery(identity)
	}
}

// deliverySettledOnChain reports whether the chain states one of the three
// terminal settlements of one obligation — sent, refused or headless-retired —
// and answers which. The three keys are read by exact identity, so the answer
// never depends on the order two settlements of one obligation happen to carry:
// a terminal settlement closes the obligation, and no later state reopens it.
// It is the read a restored account makes for an identity its own table does not
// hold, because such an account states its open work and leaves the settled set
// to the cut.
func (h *Host) deliverySettledOnChain(ctx context.Context, identity string) (string, bool, error) {
	if h == nil || identity == "" {
		return "", false, nil
	}
	for _, state := range []string{deliveryStateSent, deliveryStateRefused, deliveryStateHeadlessRetired} {
		evt, held, err := h.settlementFactOf(ctx, deliverySettlementKey(identity, state))
		if err != nil {
			return "", false, err
		}
		if !held {
			continue
		}
		if _, stated := settlementOf(evt); !stated {
			continue
		}
		return state, true, nil
	}
	return "", false, nil
}

// deliveryKnown reports whether the account already states one obligation: a
// body whose identity carries any settlement fact — held, sent, refused or
// retired — is the account's, and the live path neither sends it again nor
// holds it a second time. A settlement read that failed answers a named error
// so the caller leaves the body unread instead of resending it as absent.
func (h *Host) deliveryKnown(ctx context.Context, identity string) (bool, error) {
	if _, known := h.deliveryStateOf(identity); known {
		return true, nil
	}
	_, known, err := h.settlementStateOf(ctx, identity)
	return known, err
}

// retireHeadlessBodies retires the obligation of every body generated at an
// address that is not the entry: nothing was ever going to a channel from
// there, so each body the account does not already state is closed as
// headless-retired, body and all, instead of being delivered beside a restart.
func (h *Host) retireHeadlessBodies(ctx context.Context, addr event.Address, lineage string, bodies []freshBody) {
	for _, body := range bodies {
		identity, ok := freshObligationOf(body)
		if !ok {
			continue
		}
		known, err := h.deliveryKnown(ctx, identity)
		if err != nil {
			assemblyWarn("tagent: the headless delivery of %s stands unread and is not retired: %v", identity, err)
			continue
		}
		if known {
			continue
		}
		h.retireHeadlessDelivery(ctx, addr, identity, plugin.Delivery{Content: body.content, Lineage: lineage, Target: addr})
	}
}

// delayedNoticeRetrying is the delayed notice of an obligation the account is
// still redriving: the wording promises the resend instead of announcing a
// failure, because the bounded backoff still owns it.
const delayedNoticeRetrying = "上一条回复的发送正在重试，稍后会自动补送。"

// delayedNoticeManual is the delayed notice of an obligation the account gave up
// on or cannot decide: the wording hands it to a person, and it never borrows
// the failure wording (no cause, no terminal, no failure audit fact beside it).
const delayedNoticeManual = "上一条回复的发送遇到阻碍，已转人工处置。"

// delayedNoticeText renders the one delayed notice one hold reason calls for.
func delayedNoticeText(reason string) string {
	if reason == deliveryReasonRetrying {
		return delayedNoticeRetrying
	}
	return delayedNoticeManual
}

// deliverDelayedNotice delivers the delayed notice of one round that ended
// beside no user-visible output while an obligation of that address still
// stands unsettled and held: exactly one notice per round, never one beside a
// body the round delivered and never one for a turn no obligation dangles from.
// The reason the obligation stands in chooses the wording — a redrive in flight
// promises the resend, every other hold hands the obligation to a person.
func (h *Host) deliverDelayedNotice(ctx context.Context, addr event.Address, sender MessageSender, lineage string) {
	reason, held := h.heldReasonAt(addr)
	if !held {
		return
	}
	assemblyInfo("tagent: the round of %s ended beside no output; the held obligation is announced (%s)", addr.SessionID, reason)
	h.deliverOne(ctx, sender, plugin.Delivery{Content: delayedNoticeText(reason), Lineage: lineage, Target: addr})
}

// heldReasonAt reports the hold reason of one unsettled obligation owed to an
// address, preferring a hold that needs a person over a redrive already in
// flight so the notice the round delivers states the strongest reason the
// account knows.
func (h *Host) heldReasonAt(addr event.Address) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	manual := ""
	retrying := ""
	for id, held := range h.pendingDeliveries {
		if held.delivery.Target != addr {
			continue
		}
		reason, isHeld := heldReasonOf(h.deliveryStates[id])
		if !isHeld {
			continue
		}
		if reason == deliveryReasonRetrying {
			retrying = reason
			continue
		}
		manual = reason
		break
	}
	if manual != "" {
		return manual, true
	}
	if retrying != "" {
		return retrying, true
	}
	return "", false
}

// handoffSettledBodies registers the delivery obligation of the bodies a landed
// follow-up turn generated and sends every obligation that still stands owed
// through the review ruling the live path applies. The settle driver calls it
// before it consumes the mailbox entry that turn answered: the receipt forbids
// every replay of the notification, so the bodies of a turn whose process stopped
// between its terminal fact and its send are carried by the account instead of by
// a turn that never runs again.
//
// The send face is the whole owed set, not only the bodies this call registered:
// an obligation an earlier registration left owed — a batch whose later record
// the account refused, a sibling process that stopped after its record — is
// redriven here instead of being stranded in this process until the next boot,
// and an obligation a restart already delivered, parked or discarded carries a
// later state and is left alone. A redrive that lands beside the boot's own send
// of the same body is the one extra copy an unconfirmed receipt tolerates; both
// write the same receipt, and the record it leaves is what stops a third. An
// obligation the account refuses is reported to the driver, which keeps the entry
// owed instead of closing it.
//
//   - The settled history a checkpoint restores is its cut, not a token per identity, so one identity the restored table does not hold is answered by the terminal settlements the chain keeps. The probe stays bounded by the bodies of this turn.
func (h *Host) handoffSettledBodies(ctx context.Context, addr event.Address, notice task.Notice) error {
	if h == nil || h.run == nil {
		return nil
	}
	bodies := generatedBodiesOf(h.run.SessionFacts(addr), noticeInvocation(notice))
	lineage := deliveryLineageOf("task")
	if !h.deliversAt(addr) {
		h.retireHeadlessBodies(ctx, addr, lineage, bodies)
		return nil
	}
	for _, body := range bodies {
		identity, ok := freshObligationOf(body)
		if !ok {
			continue
		}
		state, known := h.deliveryStateOf(identity)
		if !known && h.restoredAccount() {
			_, settled, err := h.deliverySettledOnChain(ctx, identity)
			if err != nil {
				assemblyWarn("tagent: the delivery of %s stands unread and is not sent again: %v", identity, err)
				continue
			}
			if settled {
				continue
			}
		}
		if known && deliveryStateSettled(state) {
			continue
		}
		if known && deliveryStateUnsettled(state) && state != deliveryStateOwed {
			continue
		}
		if !h.claimDelivery(identity) {
			continue
		}
		delivery := plugin.Delivery{Content: body.content, Lineage: lineage, Target: addr}
		if _, cause := h.deliverObligation(ctx, identity, h.sender, delivery); cause != "" {
			assemblyDebug("tagent: the hand-off delivery to %s stands owed: %s", addr.SessionID, cause)
		}
		h.releaseDelivery(identity)
	}
	return nil
}

// deliveryRetry is one obligation the consumer redrives: the delivery semantic
// the account recorded, the retries already performed, and the moment the next
// one is due.
type deliveryRetry struct {
	delivery plugin.Delivery
	attempt  int
	due      time.Time
}

// deliveryConsumer is the one consumer the delivery account answers to: it is
// one of the chain consumers (消费挂点) and it is deliberately not a commit
// observer — it implements no ObserveCommit — because the redrive it performs is
// timed by the state machine itself (1s/4s/16s, three tries), never replayed
// from the commit stream. A live path that schedules, a recovery that resumes
// and an approval that meets a transient outage all feed the same queue, so one
// obligation has one redrive however many faces met it.
type deliveryConsumer struct {
	host *Host

	mu      sync.Mutex
	queue   map[string]*deliveryRetry
	started bool
	stopped bool
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
}

// newDeliveryConsumer builds the redrive consumer of one host.
func newDeliveryConsumer(host *Host) *deliveryConsumer {
	return &deliveryConsumer{
		host:  host,
		queue: map[string]*deliveryRetry{},
		wake:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// start launches the timer goroutine once: the consumer runs until the host
// stops it, the host context ends or the process does.
func (c *deliveryConsumer) start() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.started || c.stopped {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.mu.Unlock()
	go c.run()
}

// stopAll ends the redrive: no further send starts, and an entry already due
// may still complete the send it began.
func (c *deliveryConsumer) stopAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	started := c.started
	c.mu.Unlock()
	close(c.stop)
	if started {
		<-c.done
	}
}

// schedule queues one obligation for its attempt-th retry: the wait is the
// family backoff shifted by twice the retries already performed, so the
// sequence a first definite failure starts is 1s, 4s, 16s.
func (c *deliveryConsumer) schedule(identity string, delivery plugin.Delivery, attempt int) {
	if c == nil || identity == "" || attempt >= deliveryRetryLimit {
		return
	}
	wait := c.host.deliveryBackoffWait(attempt)
	c.mu.Lock()
	if c.queue == nil {
		c.queue = map[string]*deliveryRetry{}
	}
	c.queue[identity] = &deliveryRetry{delivery: delivery, attempt: attempt, due: time.Now().Add(wait)}
	c.mu.Unlock()
	c.start()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// cancel withdraws one obligation: a settled or refused identity leaves the
// redrive queue.
func (c *deliveryConsumer) cancel(identity string) {
	if c == nil || identity == "" {
		return
	}
	c.mu.Lock()
	delete(c.queue, identity)
	c.mu.Unlock()
}

// run is the timer loop: it sleeps until the earliest due entry, fires every
// entry the moment made due, and is woken by a schedule, by the host stop and by
// the end of the host context.
func (c *deliveryConsumer) run() {
	defer close(c.done)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for {
		wait, scheduled := c.nextWait()
		if !scheduled {
			select {
			case <-c.wake:
				continue
			case <-c.stop:
				return
			case <-c.hostDone():
				return
			}
		}
		timer.Reset(wait)
		select {
		case <-timer.C:
		case <-c.wake:
			timer.Stop()
			continue
		case <-c.stop:
			timer.Stop()
			return
		case <-c.hostDone():
			timer.Stop()
			return
		}
		c.deliverDue()
	}
}

// nextWait reports the wait until the earliest due entry and whether any entry
// is queued at all.
func (c *deliveryConsumer) nextWait() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := time.Duration(0)
	scheduled := false
	for _, entry := range c.queue {
		until := time.Until(entry.due)
		if until < 0 {
			until = 0
		}
		if !scheduled || until < wait {
			wait = until
			scheduled = true
		}
	}
	return wait, scheduled
}

// hostDone reports the end of the host context, or nil beside a host that names
// none: a host built as a literal by a check carries no context to wait on.
func (c *deliveryConsumer) hostDone() <-chan struct{} {
	if c == nil || c.host == nil {
		return nil
	}
	c.host.mu.Lock()
	defer c.host.mu.Unlock()
	if c.host.ctx == nil {
		return nil
	}
	return c.host.ctx.Done()
}

// deliverDue fires every entry whose moment passed: the entries are taken off
// the queue under the lock and sent one by one beside it, so a slow channel
// never blocks a scheduling path.
func (c *deliveryConsumer) deliverDue() {
	now := time.Now()
	c.mu.Lock()
	due := make([]*deliveryRetry, 0, len(c.queue))
	identities := make([]string, 0, len(c.queue))
	for identity, entry := range c.queue {
		if entry.due.After(now) {
			continue
		}
		identities = append(identities, identity)
		due = append(due, entry)
	}
	for _, identity := range identities {
		delete(c.queue, identity)
	}
	c.mu.Unlock()
	for index, identity := range identities {
		c.fire(identity, due[index])
	}
}

// fire performs one retry of one obligation: the send, the state the account
// records behind it, and either the next scheduled attempt or the state the
// account is left in. A retry that lands settles the obligation; a definite
// refusal retries inside the bound and then records retry-exhausted; a failure
// that cannot be told apart from a timeout records unknown and stops, because
// the send may have landed.
func (c *deliveryConsumer) fire(identity string, entry *deliveryRetry) {
	host := c.host
	if host == nil || entry == nil {
		return
	}
	host.mu.Lock()
	halted := host.closed || host.halted
	sender := host.sender
	ctx := host.ctx
	host.mu.Unlock()
	if halted || sender == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if state, held := host.deliveryStateOf(identity); held && deliveryStateSettled(state) {
		return
	}
	if err := sender.Send(ctx, entry.delivery.Target, entry.delivery.Content); err != nil {
		assemblyWarn("tagent: the redelivery of %s failed: %v", identity, err)
		host.recordRetryFailure(ctx, identity, entry, err)
		return
	}
	if err := host.settleDelivery(ctx, identity, entry.delivery); err != nil {
		assemblyWarn("tagent: the redelivery of %s landed but its receipt did not: %v", identity, err)
		return
	}
	assemblyInfo("tagent: the redelivery of %s succeeded after %d retries", identity, entry.attempt)
}

// retryConsumer reports the redrive consumer of this host, building and starting
// it on first use: a host that never owes a body never starts a timer.
func (h *Host) retryConsumer() *deliveryConsumer {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	if h.retries == nil {
		h.retries = newDeliveryConsumer(h)
	}
	consumer := h.retries
	h.mu.Unlock()
	consumer.start()
	return consumer
}

// scheduleRedrive queues one obligation for the next automatic attempt. The
// attempt continues from the highest retry fact the chain holds, so an approval
// that meets a transient outage after a restart resumes the recorded count
// instead of resetting it; the attempt fact lands before the schedule, so a
// crash between the two leaves a count the next boot reads. A read error or a
// commit error leaves the obligation parked and unscheduled rather than retried
// blind, and a count already at the bound is refused by name.
func (h *Host) scheduleRedrive(ctx context.Context, identity string, delivery plugin.Delivery) {
	if h == nil || identity == "" {
		return
	}
	attempt, err := h.recordedRetryAttempt(ctx, identity)
	if err != nil {
		assemblyWarn("tagent: the redelivery of %s reads no attempt count and is not scheduled: %v", identity, err)
		return
	}
	if attempt >= deliveryRetryLimit {
		assemblyInfo("tagent: the redelivery of %s refused: the chain records %d attempts at the bound", identity, attempt)
		return
	}
	if err := h.writeDeliveryRetryAttempt(ctx, delivery.Target, identity, attempt, delivery); err != nil {
		assemblyWarn("tagent: the retry attempt of %s did not commit and is not scheduled: %v", identity, err)
		return
	}
	h.retryConsumer().schedule(identity, delivery, attempt)
}

// recordedRetryAttempt reports the highest retry attempt fact the chain holds
// for one identity: the count a restart or a later approval resumes from. A read
// error is surfaced so the caller never schedules a blind retry beside an
// unreadable account.
func (h *Host) recordedRetryAttempt(ctx context.Context, identity string) (int, error) {
	highest := 0
	for attempt := 0; attempt < deliveryRetryLimit; attempt++ {
		_, held, err := h.settlementFactOf(ctx, deliverySettlementKey(identity, deliveryRetryState(attempt)))
		if err != nil {
			return 0, err
		}
		if held {
			highest = attempt
		}
	}
	return highest, nil
}

// writeDeliveryRetryAttempt lands one retry attempt fact under its immutable
// (identity, attempt) key, idempotently: a fact the chain already holds answers
// without a second commit. The fact records the count only — the body stays with
// the held(retrying) settlement fact the recovery resends from — and it never
// touches the state table or the hot-window pin, which the settlement states own.
func (h *Host) writeDeliveryRetryAttempt(ctx context.Context, target event.Address, identity string, attempt int, delivery plugin.Delivery) error {
	if h == nil || h.run == nil {
		return nil
	}
	key := deliverySettlementKey(identity, deliveryRetryState(attempt))
	if _, held, err := h.settlementFactOf(ctx, key); err != nil {
		return err
	} else if held {
		return nil
	}
	evt := event.Event{
		Key:       key,
		Type:      deliverySettlementType,
		Timestamp: time.Now(),
		Message: model.Message{
			Role:    model.RoleUser,
			Content: fmt.Sprintf("delivery retry %d: %s (%d bytes)", attempt, identity, len(delivery.Content)),
		},
		Meta: map[string]string{
			event.MetaKeyEventSummary:  deliverySettlementSummary,
			event.MetaKeyTriggerSource: "host",
			deliveryMetaIdentity:       identity,
			deliveryMetaState:          deliveryRetryState(attempt),
			deliveryMetaTargetAgent:    string(delivery.Target.AgentID),
			deliveryMetaTargetSession:  string(delivery.Target.SessionID),
		},
		Owner:  target,
		Source: target,
		Target: target,
	}
	if _, err := h.run.CommitToSession(ctx, target, evt); err != nil {
		return fmt.Errorf("tagent: the delivery retry attempt %d of %s did not commit: %w", attempt, identity, err)
	}
	return nil
}

// cancelRedrive withdraws one obligation from the redrive.
func (h *Host) cancelRedrive(identity string) {
	if h == nil || identity == "" {
		return
	}
	h.mu.Lock()
	consumer := h.retries
	h.mu.Unlock()
	if consumer != nil {
		consumer.cancel(identity)
	}
}

// deliveryBackoffWait reports the wait before the attempt-th retry: the family
// unit shifted by twice the retries already performed (1s, 4s, 16s), or the
// dial one check set.
func (h *Host) deliveryBackoffWait(attempt int) time.Duration {
	unit := deliveryBackoffUnit
	if h != nil && h.deliveryBackoff > 0 {
		unit = h.deliveryBackoff
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > deliveryRetryLimit-1 {
		attempt = deliveryRetryLimit - 1
	}
	return unit << (2 * attempt)
}

// recordRetryFailure records one failed retry: a definite refusal inside the
// bound is recorded as retrying and scheduled again, while a refusal that used
// the last attempt is recorded as retry-exhausted and a failure indistinguishable
// from a timeout as unknown. Either way the obligation stays parked, so a person
// can still decide it.
func (h *Host) recordRetryFailure(ctx context.Context, identity string, entry *deliveryRetry, err error) {
	_, definite := deliveryFailureState(err)
	if definite && entry.attempt+1 < deliveryRetryLimit {
		next := entry.attempt + 1
		if writeErr := h.writeDeliveryState(ctx, identity, entry.delivery, heldStateOf(deliveryReasonRetrying), time.Time{}); writeErr != nil {
			assemblyWarn("tagent: the retry state of %s did not commit: %v", identity, writeErr)
		}
		h.parkHeld(identity, entry.delivery, time.Time{})
		if writeErr := h.writeDeliveryRetryAttempt(ctx, entry.delivery.Target, identity, next, entry.delivery); writeErr != nil {
			assemblyWarn("tagent: the retry attempt of %s did not commit and is not scheduled: %v", identity, writeErr)
			return
		}
		h.retryConsumer().schedule(identity, entry.delivery, next)
		return
	}
	reason := deliveryReasonUnknown
	if definite {
		reason = deliveryReasonRetryExhausted
	}
	if writeErr := h.writeDeliveryState(ctx, identity, entry.delivery, heldStateOf(reason), time.Time{}); writeErr != nil {
		assemblyWarn("tagent: the hold state of %s did not commit: %v", identity, writeErr)
	}
	h.parkHeld(identity, entry.delivery, time.Time{})
	assemblyInfo("tagent: the redelivery of %s stopped as %s", identity, reason)
}

// restoreUnconsumedEvents is the first step of the restart-recovery
// consumption logic: the deliveries and notifications a stopped run left
// unconsumed are loaded back where their frozen routing names them, so the
// work waits for the next decision instead of dying with the process. The
// host is not yet open for service while this runs. The second step — the
// in-memory state from the snapshot or the replay — finishes inside the
// assembly; the completion point (the ticket) is filed only after this step
// returns, so the two steps and their completion stay in one boot sequence.
func (h *Host) restoreUnconsumedEvents(ctx context.Context, settleDriver interface{ Wake() }) error {
	if err := h.restorePendingDeliveries(ctx); err != nil {
		return err
	}
	return nil
}

// restorePendingDeliveries rebuilds the delivery account of this host from the
// fact chain alone:
// - Settlement facts and owed declarations are read once from the registered durable chains, never limited to the hot fact window.
// - The latest committed settlement of each identity decides its restored state; exact reads still resolve settled identities by key.
// - An obligation no settlement fact states is still owed: it is sent through the review ruling the live path applies, or retired when its target is not the entry.
// - An obligation held for approval waits again under the identity and moment the chain recorded, a held(retrying) obligation resumes its bounded backoff, and a sent, refused or retired one is left as it stands.
// - A settlement fact outside the window is still read by its exact key, so a settled obligation is never mistaken for an unregistered one and never resent.
//
// A settlement fact that cannot be reconstructed, or a record that cannot be unmarshaled, refuses the boot by name: the host never starts beside an obligation it cannot honor.
func (h *Host) restorePendingDeliveries(ctx context.Context) error {
	if h == nil || h.run == nil {
		return nil
	}
	if err := h.migrateRetiredDeliveries(ctx); err != nil {
		return err
	}
	return h.restoreEventifiedDeliveries(ctx)
}

// deliveryRecoveryFacts returns the latest settlement per obligation beside
// every owed declaration and the highest retry attempt recorded per obligation.
// The account the newest checkpoint restored is the state the facts below its cut
// produced, so only the facts that landed past the cut are read again: the
// obligations of a long-lived store cost what is still in flight, never the whole
// chain the account already settled. A store holding no checkpoint, and an
// in-memory run beside no durable history, answer the facts the run still holds.
func (h *Host) deliveryRecoveryFacts(ctx context.Context) (map[string]deliverySettlement, []deliverySettlement, map[string]int, error) {
	states := map[string]deliverySettlement{}
	declared := make([]deliverySettlement, 0, 8)
	attempts := map[string]int{}
	r := h.run
	r.mu.Lock()
	tier := r.tier
	r.mu.Unlock()
	if h.restoredDelivery {
		h.collectRestoredAccount(states, &declared, attempts)
	}
	if tier == nil {
		for _, evt := range r.factWindowEvents() {
			collectDeliveryRecoveryFact(evt, states, &declared, attempts)
		}
		return states, declared, attempts, nil
	}
	allowed := map[int]bool{}
	for _, partition := range r.registeredPartitions() {
		allowed[partition] = true
	}
	err := r.eachFactPastCut(ctx, tier, r.checkpointCut(checkpointSliceDelivery), func(stored ledger.StoredEvent) error {
		if !allowed[stored.PartitionID] {
			return nil
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return fmt.Errorf("tagent: replay the committed delivery fact %s: %w", stored.Key, err)
		}
		evt := body.Event()
		evt.Key = stored.Key
		collectDeliveryRecoveryFact(evt, states, &declared, attempts)
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return states, declared, attempts, nil
}

// collectRestoredAccount folds the account the checkpoint restored into the
// recovery set: every settlement state the account remembers beside the
// obligations it still owes with the delivery and hold moment they carry, and
// the redrive each of them stands at, so a restart resumes the bounded backoff
// where the process before it stopped instead of starting it over.
func (h *Host) collectRestoredAccount(states map[string]deliverySettlement, declared *[]deliverySettlement, attempts map[string]int) {
	h.mu.Lock()
	identities := make([]string, 0, len(h.deliveryStates))
	for identity, state := range h.deliveryStates {
		identities = append(identities, identity)
		states[identity] = deliverySettlement{identity: identity, state: state}
	}
	sort.Strings(identities)
	for identity, held := range h.pendingDeliveries {
		if held == nil {
			continue
		}
		*declared = append(*declared, deliverySettlement{
			identity: identity,
			state:    h.deliveryStates[identity],
			delivery: held.delivery,
			heldAt:   held.heldAt,
		})
	}
	h.mu.Unlock()
	for _, retry := range h.retryConsumer().pendingRedrives() {
		if retry.Identity == "" || retry.Attempt <= attempts[retry.Identity] {
			continue
		}
		attempts[retry.Identity] = retry.Attempt
	}
	sort.Slice(*declared, func(i, j int) bool { return (*declared)[i].identity < (*declared)[j].identity })
}

// collectDeliveryRecoveryFact folds one committed fact into the recovery set.
func collectDeliveryRecoveryFact(evt event.Event, states map[string]deliverySettlement, declared *[]deliverySettlement, attempts map[string]int) {
	if identity, attempt, ok := deliveryRetryAttemptOf(evt); ok {
		if attempt > attempts[identity] {
			attempts[identity] = attempt
		}
	}
	if settlement, ok := settlementOf(evt); ok {
		states[settlement.identity] = settlement
	}
	if evt.Type != event.TypeAgentOutput || evt.Meta[deliveryOwedMeta] != "1" {
		return
	}
	identity, ok := freshObligationOf(freshBody{fact: evt.Key})
	if !ok {
		return
	}
	target := event.Address{
		AgentID:   event.AgentID(evt.Meta[deliveryTargetAgentMeta]),
		SessionID: event.SessionID(evt.Meta[deliveryTargetSessionMeta]),
	}
	if target.IsZero() {
		target = evt.Owner
	}
	body, _ := assistantBodyOf(evt)
	*declared = append(*declared, deliverySettlement{
		identity: identity,
		delivery: plugin.Delivery{
			Content: body.content,
			Lineage: evt.Meta[deliveryLineageMeta],
			Target:  target,
		},
	})
}

// restoreEventifiedDeliveries restores the account from the chain the run
// replayed: every owed declaration the window holds is weighed against the
// newest settlement fact of its identity, and every settlement fact no
// declaration of the window explains is restored as the obligation it states.
//
//   - a restored account states the settlement it held at the cut and can trail the settlement fact that landed with it, so the exact key read of the one obligation decides its state: a restored owed entry whose settlement already landed is never sent a second time.
//   - the same read decides the state of every obligation the account states as open with no owed declaration beside it, because a carrier filed by the commit that settled the obligation states the state that commit had not yet applied: the open identities are the work in flight, so the probe stays bounded by them and never by the settled history.
func (h *Host) restoreEventifiedDeliveries(ctx context.Context) error {
	window, declared, attempts, err := h.deliveryRecoveryFacts(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	for identity := range window {
		if seq := deliveryHeldSeq(identity); seq > h.heldSeq {
			h.heldSeq = seq
		}
	}
	h.mu.Unlock()
	seen := make(map[string]bool, len(declared))
	resumed := make(map[string]bool, len(window))
	for _, declaration := range declared {
		seen[declaration.identity] = true
		settlement, known := window[declaration.identity]
		if h.restoredDelivery || !known {
			var err error
			exact, stated, err := h.settlementStateOf(ctx, declaration.identity)
			if err != nil {
				return err
			}
			if stated && (!known || exact.fact.Timestamp.After(settlement.fact.Timestamp)) {
				settlement, known = exact, true
			}
		}
		if !known {
			settlement = declaration
		} else if settlement.delivery.Content == "" {
			settlement.delivery.Content = declaration.delivery.Content
		}
		if settlement.delivery.Target.IsZero() || !settlement.delivery.Target.IsComplete() {
			settlement.delivery.Target = declaration.delivery.Target
		}
		if err := h.restoreObligation(ctx, settlement); err != nil {
			return err
		}
		if settlement.state == heldStateOf(deliveryReasonRetrying) {
			resumed[settlement.identity] = true
		}
	}
	identities := make([]string, 0, len(window))
	for identity := range window {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		if seen[identity] {
			continue
		}
		settlement := window[identity]
		if !deliveryStateSettled(settlement.state) {
			exact, stated, err := h.settlementStateOf(ctx, identity)
			if err != nil {
				return err
			}
			if stated && (settlement.fact.Timestamp.IsZero() || exact.fact.Timestamp.After(settlement.fact.Timestamp)) {
				settlement = exact
			}
		}
		if err := h.restoreObligation(ctx, settlement); err != nil {
			return err
		}
		if settlement.state == heldStateOf(deliveryReasonRetrying) {
			resumed[identity] = true
		}
	}
	for identity := range resumed {
		if state, known := h.deliveryStateOf(identity); !known || state != heldStateOf(deliveryReasonRetrying) {
			continue
		}
		held, parked := h.parkedDelivery(identity)
		if !parked {
			continue
		}
		attempt := attempts[identity]
		if attempt >= deliveryRetryLimit {
			if writeErr := h.writeDeliveryState(ctx, identity, held.delivery, heldStateOf(deliveryReasonRetryExhausted), time.Time{}); writeErr != nil {
				assemblyWarn("tagent: the exhausted retry state of %s did not commit beside the restart: %v", identity, writeErr)
			}
			assemblyInfo("tagent: the redelivery of %s refused beside the restart: the chain records %d attempts at the bound", identity, attempt)
			continue
		}
		h.retryConsumer().schedule(identity, held.delivery, attempt)
	}
	return nil
}

// restoreObligation restores one obligation from the settlement fact that states
// it: a settled state is left as it stands (the account only remembers it), a
// held state is parked under the identity and hold moment the chain recorded, and
// an owed state — or an obligation no settlement fact states yet — is delivered
// through the review ruling, or retired when its target is not the entry.
//
//   - a settled obligation owes nothing whatever the declaration the account restored says: its settlement landed past the cut the declaration was stated at, so the hold is dropped instead of waiting for a send a restart would make a second time.
func (h *Host) restoreObligation(ctx context.Context, settlement deliverySettlement) error {
	state := settlement.state
	if deliveryStateSettled(state) {
		h.dropHeld(settlement.identity)
		h.noteDeliveryState(settlement.identity, state)
		return nil
	}
	if state != "" && state != deliveryStateOwed {
		heldAt := settlement.heldAt
		if heldAt.IsZero() {
			heldAt = settlement.fact.Timestamp
		}
		h.parkHeld(settlement.identity, settlement.delivery, heldAt)
		h.noteDeliveryState(settlement.identity, state)
		assemblyInfo("tagent: the delivery %s stands %s beside the restart", settlement.identity, state)
		return nil
	}
	delivery := settlement.delivery
	if delivery.Content == "" {
		return fmt.Errorf("tagent: the owed delivery %s carries no body and no settlement fact states it", settlement.identity)
	}
	if delivery.Target.AgentID != h.entryAgent() {
		h.retireHeadlessDelivery(ctx, delivery.Target, settlement.identity, delivery)
		return nil
	}
	if !h.claimDelivery(settlement.identity) {
		return nil
	}
	defer h.releaseDelivery(settlement.identity)
	if h.sender == nil {
		assemblyWarn("tagent: the owed delivery of %s waits for an outbound sender", settlement.identity)
		return nil
	}
	if _, cause := h.deliverObligation(ctx, settlement.identity, h.sender, delivery); cause != "" {
		assemblyWarn("tagent: the owed delivery of %s still stands owed: %s", settlement.identity, cause)
		return nil
	}
	assemblyInfo("tagent: the owed delivery of %s was delivered beside the restart", settlement.identity)
	return nil
}

// parkedDelivery reports the review-table entry of one identity.
func (h *Host) parkedDelivery(identity string) (*heldDelivery, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	held, parked := h.pendingDeliveries[identity]
	return held, parked
}

// claimDelivery claims one obligation for one live path of this process: the
// closed segment and the hand-off never send one obligation at the same time.
// The claim is held for the length of one send attempt and released behind it —
// the settlement fact the attempt records is what keeps a later path away — so a
// claim never outlives the attempt that took it.
func (h *Host) claimDelivery(identity string) bool {
	if h == nil || identity == "" {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.deliveryClaims == nil {
		h.deliveryClaims = map[string]bool{}
	}
	if h.deliveryClaims[identity] {
		return false
	}
	h.deliveryClaims[identity] = true
	return true
}

// releaseDelivery releases the claim of one send attempt that returned.
func (h *Host) releaseDelivery(identity string) {
	if h == nil || identity == "" {
		return
	}
	h.mu.Lock()
	delete(h.deliveryClaims, identity)
	h.mu.Unlock()
}

// migrateRetiredDeliveries moves the read-only records of the storage tier onto
// the settlement facts, once, before the account is read:
// - Every record is read (a record that does not unmarshal refuses the boot by name), and its six-state status maps onto a settlement state under the same delivery identity, idempotently: a fact the chain already holds is answered without a second commit, so a second boot migrates nothing twice.
// - A record naming no identity also refuses the boot: an obligation no identity can name is one no approval, refusal or receipt can resolve.
// - The records themselves are left in place as the read-only audit of the migration (this path never writes them again).
//
// The migration is the one read of the read-only account: no live path writes it, and no settlement path reads it back.
func (h *Host) migrateRetiredDeliveries(ctx context.Context) error {
	log := h.deliveryDurable()
	if log == nil {
		return nil
	}
	records, err := log.Scan(ctx, hostDeliveryPrefix)
	if err != nil {
		return fmt.Errorf("tagent: read the retired delivery records: %w", err)
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var record hostDeliveryRecord
		if err := json.Unmarshal(records[key], &record); err != nil {
			return fmt.Errorf("tagent: the delivery record %s does not unmarshal: %w", key, err)
		}
		state, known := settlementStateOfRetiredRecord(record.Status)
		if !known {
			continue
		}
		if record.ID == "" {
			return fmt.Errorf("tagent: the delivery record %s names no identity", key)
		}
		if seq := deliveryHeldSeq(record.ID); seq > h.heldSeq {
			h.heldSeq = seq
		}
		delivery := plugin.Delivery{Content: record.Content, Lineage: record.Lineage, Target: record.Target}
		extra := map[string]string{deliveryMetaMigrated: key}
		if err := h.writeDeliverySettlement(ctx, record.Target, record.ID, state, delivery, record.HeldAt, extra); err != nil {
			return fmt.Errorf("tagent: the delivery record %s did not migrate: %w", key, err)
		}
		assemblyInfo("tagent: the retired delivery record %s migrated as %s", record.ID, state)
	}
	return nil
}

// holdDelivery parks one body the host owes a target and records the state the
// hold stands in as its settlement fact: the obligation keeps the identity its
// generation registered, or takes a host-local one beside a body no generation
// named. The fact is written before the entry is parked, so a stop between the
// two leaves an obligation the restart restores rather than a hold no account
// knows; a fact the chain refused is reported beside the parked entry, so the
// body stays visible to a decision and the account stays behind it.
func (h *Host) holdDelivery(ctx context.Context, obligation string, delivery plugin.Delivery, reason, state string) (string, error) {
	if h == nil {
		return "", ErrHostClosed
	}
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		assemblyWarn("tagent: the held delivery to %s was dropped beside a closed host", delivery.Target.SessionID)
		return "", fmt.Errorf("%w: the held delivery to %s", ErrHostClosed, delivery.Target.SessionID)
	}
	id, err := h.holdObligation(ctx, obligation, delivery, state)
	assemblyInfo("tagent: the delivery to %s was held as %s: %s", delivery.Target.SessionID, state, reason)
	if err != nil {
		return id, fmt.Errorf("tagent: the hold of %s left no record: %w", id, err)
	}
	return id, nil
}

// sentRecordNote renders the receipt note of a body whose send landed and whose
// receipt did not: the counterpart of holdRecordNote, so a caller reads an
// unconfirmed receipt the same way it reads an unrecorded hold.
func sentRecordNote(err error) string {
	if err == nil {
		return ""
	}
	return "the delivery landed but its receipt did not: " + err.Error()
}

// holdRecordNote renders the receipt note one hold adds when its record did not
// land, and nothing beside a hold the account accepted.
func holdRecordNote(err error) string {
	if err == nil {
		return ""
	}
	return "; " + err.Error()
}

// noteDeliveryRefusal commits the audit fact of one refused delivery: a
// feedback fact owned by the target address, carrying the hold identity and the
// delivery dimensions in its metadata and no model payload in its message, so
// the fact enters no model request. A refused commit answers the caller by name:
// a refusal is a decision the account states, not one the log states for it.
func (h *Host) noteDeliveryRefusal(ctx context.Context, held *heldDelivery) error {
	if h == nil || h.run == nil || held == nil {
		return nil
	}
	if _, err := h.run.CommitToSession(ctx, held.delivery.Target, event.Event{
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource:          "host",
			event.MetaKeyEventSummary:           "delivery refused",
			event.MetaPrefix + "delivery_id":    held.id,
			event.MetaPrefix + "lineage":        held.delivery.Lineage,
			event.MetaPrefix + "target_agent":   string(held.delivery.Target.AgentID),
			event.MetaPrefix + "target_session": string(held.delivery.Target.SessionID),
			event.MetaPrefix + "content_bytes":  strconv.Itoa(len(held.delivery.Content)),
		},
	}); err != nil {
		return fmt.Errorf("tagent: the refusal of the held delivery %s left no fact: %w", held.id, err)
	}
	return nil
}

// failureNoticeText renders the body one failed turn delivers: the raw cause as
// the reduction recorded it, bounded to failureNoticeMaxBytes at a rune boundary
// and marked when it was cut. It never invents a reason: an empty cause answers
// an empty body the caller does not send.
func failureNoticeText(cause string) string {
	text := strings.TrimSpace(cause)
	if text == "" || len(text) <= failureNoticeMaxBytes {
		return text
	}
	cut := failureNoticeMaxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + failureNoticeTruncated
}

// noteFailureNotice commits the audit fact of one delivered failure notice: a feedback
// fact owned by the address, carrying the raw cause and the terminal in its metadata and
// no model payload in its message.
// - No message content: the fact maps onto no projection entry and enters no model request.
// - No notice kind and no task id: the notice consumers drive follow-up turns from those keys.
// - A refused commit is a warning beside the delivery, never a reason to withhold the notice.
func (h *Host) noteFailureNotice(ctx context.Context, addr event.Address, turn agent.Turn) {
	if h == nil || h.run == nil {
		return
	}
	if _, err := h.run.CommitToSession(ctx, addr, event.Event{
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource:          "host",
			event.MetaKeyEventSummary:           failureNoticeSummaryOf(turn.Failure),
			event.MetaPrefix + "terminal":       turn.Terminal.String(),
			event.MetaPrefix + "failure_reason": turn.Failure,
		},
		InvocationID: turn.InvocationID,
	}); err != nil {
		assemblyWarn("tagent: the failure notice of %s left no fact: %v", addr.SessionID, err)
	}
}

// failureNoticeSummaryOf renders the audit line of one failure notice: the
// summary prefix beside the first line of the cause, bounded by the same rune
// bound the projection summary uses. The durable body keeps this line alone, so
// the bound decides how much of the cause a later reader finds there.
func failureNoticeSummaryOf(cause string) string {
	line := cause
	if at := strings.IndexByte(line, '\n'); at >= 0 {
		line = line[:at]
	}
	line = strings.TrimSpace(line)
	summary := failureNoticeSummary
	if line != "" {
		summary += ": " + line
	}
	runes := []rune(summary)
	if len(runes) <= mappedSummaryRunes {
		return summary
	}
	return string(runes[:mappedSummaryRunes]) + "…"
}

// deliveryLineageKey is the context key one running turn's trigger-source
// stamp is carried under, from the drive down to the send tool.
type deliveryLineageKey struct{}

// withDeliveryLineage stamps the trigger-source line of the running turn on the
// context the drive hands to its calls, so a cross-agent body names the line its
// turn names instead of inventing one.
func withDeliveryLineage(ctx context.Context, lineage string) context.Context {
	return context.WithValue(ctx, deliveryLineageKey{}, lineage)
}

// deliveryLineageOf reports the lineage one turn body carries: the trigger
// source the invocation stated, or the user stamp a user-address turn carries
// when the invocation names none.
func deliveryLineageOf(triggerSource string) string {
	if stamp := strings.TrimSpace(triggerSource); stamp != "" {
		return stamp
	}
	return "user"
}

// freshBody is one assistant body a drive generated beside the identity of the
// fact that carries it: the identity names the delivery obligation of that body,
// so the registration beside the generation and the hand-off a restart reads
// back name one record instead of two.
type freshBody struct {
	// fact is the key of the committed fact the body was generated in.
	fact event.EventID
	// content is the trimmed body the fact carries.
	content string
}

// assistantBodyOf reports the plain assistant body one committed fact carries:
// the turn-end answer of a drive, never a tool call and never an empty body.
func assistantBodyOf(evt event.Event) (freshBody, bool) {
	msg := evt.Message
	if msg.Role != model.RoleAssistant || len(msg.ToolCalls) > 0 {
		return freshBody{}, false
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return freshBody{}, false
	}
	return freshBody{fact: evt.Key, content: content}, true
}

// generatedBodiesOf reports the plain assistant bodies one invocation generated:
// the bodies the turn committed under its own four-part identity, read back from
// the chain a restart rebuilt. A drive names its facts by invocation identity, so
// the bodies of the turn are recoverable after the process that generated them is
// gone, and the delivery of a closed segment, the hand-off of a settled notice and
// the registration a follow-up turn leaves all read the same set.
func generatedBodiesOf(facts []event.Event, invocation event.InvocationID) []freshBody {
	if invocation == "" {
		return nil
	}
	inputs := map[event.EventID]bool{}
	for _, evt := range facts {
		if evt.InvocationID == invocation && evt.Type != event.TypeAgentOutput && evt.Key != "" {
			inputs[evt.Key] = true
		}
	}
	bodies := make([]freshBody, 0, 2)
	seen := map[event.EventID]bool{}
	for _, evt := range facts {
		related := evt.InvocationID == invocation
		if evt.Type == event.TypeAgentOutput {
			var consumed []event.EventID
			if err := json.Unmarshal([]byte(evt.Meta[event.MetaKeyInputEvents]), &consumed); err == nil {
				for _, key := range consumed {
					related = related || inputs[key]
				}
			}
		}
		if !related || (evt.Key != "" && seen[evt.Key]) {
			continue
		}
		if body, ok := assistantBodyOf(evt); ok {
			bodies = append(bodies, body)
			if evt.Key != "" {
				seen[evt.Key] = true
			}
		}
	}
	return bodies
}

// freshObligationOf reports the delivery obligation identity of one generated
// body and whether the body carries one at all: a fact committed without a key
// names no obligation, and its body is delivered without a durable record.
func freshObligationOf(body freshBody) (string, bool) {
	if body.fact == "" {
		return "", false
	}
	return hostDeliveryFreshPrefix + string(body.fact), true
}

// deliveryHeldSeq reports the order number inside one delivery identity, and
// zero beside an identity that carries none.
func deliveryHeldSeq(id string) uint64 {
	seq, err := strconv.ParseUint(strings.TrimPrefix(id, "delivery-"), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}
