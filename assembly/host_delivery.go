// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The delivery ledger of the host: the assistant bodies one drive generates become
// durable delivery obligations before any body leaves, a held body waits for the
// approval face, and a restart resumes every obligation the ledger still names. The
// body, the hold and the refusal each stand as one record under one identity, so a
// delivery settled twice is settled once.
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
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

// deliverFresh delivers the assistant bodies the invocation that just ended
// generated through the sender; a session the chain maps onto a projection
// commits no transcript history, so the facts are the one read face both session
// kinds share. The bodies are read by the identity of that invocation, never by a
// position of the chain: the chain is held to the hot window of its session, so a
// position read at an earlier moment names other material.
//   - A body wins over a failure: a turn that produced a deliverable answer delivers it.
//   - A failed turn without a body delivers the raw recorded cause; no cause stays silent.
//   - Each body stands as a durable delivery obligation before its send: a stop between the
//     record and the confirmed send is answered by a restart that sends the body, and the
//     obligation is settled under the identity the generation registered.
func (h *Host) deliverFresh(ctx context.Context, addr event.Address, sender MessageSender, lineage string, turn agent.Turn) {
	if sender == nil {
		return
	}
	bodies := generatedBodiesOf(h.run.SessionFacts(addr), turn.InvocationID)
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
		assemblyDebug("tagent: the invocation of %s produced no deliverable assistant body", addr.SessionID)
		return
	}
	if _, err := h.recordDeliveryOwed(ctx, addr, lineage, bodies); err != nil {
		assemblyWarn("tagent: the delivery obligations of %s were not registered: %v", addr.SessionID, err)
	}
	for _, body := range bodies {
		obligation, _ := freshObligationOf(body)
		delivery := plugin.Delivery{Content: body.content, Lineage: lineage, Target: addr}
		if _, cause := h.deliverObligation(ctx, obligation, sender, delivery); cause != "" {
			assemblyDebug("tagent: the delivery to %s stands owed: %s", addr.SessionID, cause)
		}
	}
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

// hostDeliveryPrefix locates the durable record of every delivery obligation
// the review table holds, on the same fact surface the durable consume
// receipts commit through.
const hostDeliveryPrefix = "tagent/task/v1/delivery:"

// hostDeliveryOwed names the record state of a body the host generated and has
// not delivered: the obligation a restart reads back and sends through the same
// review ruling the live path applies.
const hostDeliveryOwed = "owed"

// hostDeliveryFreshPrefix names the obligation of one generated body: the record
// is keyed by the identity of the fact that carries the body, so the obligation a
// restart registers and the one the live path registered name one record.
const hostDeliveryFreshPrefix = "delivery-fresh-"

// hostDeliveryHeld names the record state of a body the review parked before
// the sender ran.
const hostDeliveryHeld = "held"

// hostDeliveryRetry names the record state of a body the channel refused: the
// record of the redelivery the body owes.
const hostDeliveryRetry = "retry"

// hostDeliverySent names the record state of a body the sender accepted: the
// receipt of an obligation discharged.
const hostDeliverySent = "sent"

// hostDeliveryRefused names the record state of a body a refusal discarded:
// the receipt of an obligation given up beside its audit fact.
const hostDeliveryRefused = "refused"

// hostDeliveryHeadless names the record state of a body retired without a send
// because it was generated at an address that is not the entry: the host drives
// every other address headless, so its answer belongs to the caller that drove it
// and never to a message channel.
const hostDeliveryHeadless = "headless"

// hostDeliveryRecord is the durable form of one delivery obligation: the
// identity, content, lineage and target of the body beside the state the
// obligation stands in and the moment it was taken.
type hostDeliveryRecord struct {
	ID      string        `json:"id"`
	Content string        `json:"content"`
	Lineage string        `json:"lineage"`
	Target  event.Address `json:"target"`
	Status  string        `json:"status"`
	HeldAt  time.Time     `json:"held_at"`
}

// HeldDeliveries reports the bodies the host owes a target before delivery:
// the holds the review plugin parked and the redeliveries a refused send owes,
// oldest first. The result is a copy the caller owns: reading here changes no
// hold, and only ApproveDelivery or RefuseDelivery resolves one.
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
// hold for a later approval and is reported to the caller. Every hold is
// recorded beside the review table, so the approval resolves the body across a
// restart as well.
//
// The receipt of the approval lands only after the send it confirms returned: a
// receipt the tier refuses leaves the obligation owed and answers the landed
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
		return errors.Join(missing, h.restoreHeldDelivery(ctx, held, hostDeliveryRetry))
	}
	if err := sender.Send(ctx, held.delivery.Target, held.delivery.Content); err != nil {
		refused := fmt.Errorf("tagent: the approved delivery of %s was refused: %w", id, err)
		return errors.Join(refused, h.restoreHeldDelivery(ctx, held, hostDeliveryRetry))
	}
	if err := h.recordDeliveryState(ctx, held, hostDeliverySent); err != nil {
		landed := fmt.Errorf("tagent: the delivery of %s landed but its receipt did not: %w", id, err)
		return errors.Join(landed, h.restoreHeldDelivery(ctx, held, hostDeliveryHeld))
	}
	assemblyInfo("tagent: the held delivery of %s was approved and sent", id)
	return nil
}

// RefuseDelivery discards one held body: nothing is delivered, the hold is
// consumed, and an audit feedback fact without a model payload records the
// refusal at the target address. The refusal consumes the durable record as
// well, so no later boot owes the body again.
//
// Both commits are checked before the refusal publishes: a record or an audit
// fact the tier refused answers the caller by name and leaves the body in the
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
	if err := h.recordDeliveryState(ctx, held, hostDeliveryRefused); err != nil {
		unrecorded := fmt.Errorf("tagent: the refusal of the held delivery %s was not recorded: %w", id, err)
		return errors.Join(unrecorded, h.restoreHeldDelivery(ctx, held, hostDeliveryHeld))
	}
	if err := h.noteDeliveryRefusal(ctx, held); err != nil {
		return errors.Join(err, h.restoreHeldDelivery(ctx, held, hostDeliveryHeld))
	}
	assemblyInfo("tagent: the held delivery of %s was refused", id)
	return nil
}

// deliveryDurable reports the fact surface the delivery records commit
// through: the storage tier the run opened, and nil beside a run that keeps no
// tier, where the review table stays process-local.
func (h *Host) deliveryDurable() state.FactLog {
	if h == nil || h.run == nil || h.run.tier == nil {
		return nil
	}
	return ledger.TierFactLog(h.run.tier)
}

// recordDeliveryState commits the durable record of one delivery obligation
// under the state it stands in and completes the write barrier before it
// returns, so a caller publishes a state change only behind the record that
// carries it. A run beside no storage tier keeps the review table process-local
// and commits nothing.
func (h *Host) recordDeliveryState(ctx context.Context, held *heldDelivery, status string) error {
	log := h.deliveryDurable()
	if log == nil {
		return nil
	}
	body, err := json.Marshal(hostDeliveryRecord{
		ID:      held.id,
		Content: held.delivery.Content,
		Lineage: held.delivery.Lineage,
		Target:  held.delivery.Target,
		Status:  status,
		HeldAt:  held.heldAt,
	})
	if err != nil {
		return fmt.Errorf("tagent: the delivery record of %s does not marshal: %w", held.id, err)
	}
	if err := log.Put(ctx, hostDeliveryPrefix+held.id, body); err != nil {
		return fmt.Errorf("tagent: the delivery record of %s was refused: %w", held.id, err)
	}
	if err := log.Sync(ctx); err != nil {
		return fmt.Errorf("tagent: the delivery record of %s left no write barrier: %w", held.id, err)
	}
	return nil
}

// recordDeliveryOwed registers the delivery obligation of every generated body
// the account does not hold yet, in one batch behind one write barrier, and
// reports every body whose obligation still stands owed when it returns: the
// bodies it just registered beside the ones an earlier registration left owed
// because no send ever discharged them. The hand-off sends that whole set, so an
// obligation stranded between its record and its send is redriven by the process
// that meets it again instead of waiting for a restart. A body whose record
// already carries a later state — held, retry, sent or refused — is left as it
// stands, so one generation registers one obligation however often the hand-off
// runs and a discharged body is never delivered a second time here. The write
// barrier is taken only by a batch that staged a record: a set made entirely of
// records an earlier call already committed carries nothing new to sync.
//
// Only a body generated at the entry address takes an obligation at all: every
// other address is driven headless, which means its answer is read by the caller
// that drove it and never pushed to a message channel. That is the same reader
// the live delivery applies, so the record and the send agree on which addresses
// own a delivery. A run beside no storage tier registers nothing: its deliveries
// are process-local.
func (h *Host) recordDeliveryOwed(ctx context.Context, addr event.Address, lineage string, bodies []freshBody) ([]freshBody, error) {
	log := h.deliveryDurable()
	if log == nil || len(bodies) == 0 || addr.AgentID != h.entry {
		return nil, nil
	}
	owed := make([]freshBody, 0, len(bodies))
	staged := false
	for _, body := range bodies {
		id, ok := freshObligationOf(body)
		if !ok {
			continue
		}
		key := hostDeliveryPrefix + id
		payload, held, err := log.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("tagent: read the delivery obligation %s: %w", id, err)
		}
		if held {
			if owedDeliveryRecord(payload) {
				owed = append(owed, body)
			}
			continue
		}
		staged = true
		payload, err = json.Marshal(hostDeliveryRecord{
			ID:      id,
			Content: body.content,
			Lineage: lineage,
			Target:  addr,
			Status:  hostDeliveryOwed,
			HeldAt:  time.Now(),
		})
		if err != nil {
			return nil, fmt.Errorf("tagent: the delivery obligation %s does not marshal: %w", id, err)
		}
		if err := log.Put(ctx, key, payload); err != nil {
			return nil, fmt.Errorf("tagent: the delivery obligation %s was refused: %w", id, err)
		}
		owed = append(owed, body)
	}
	if staged {
		if err := log.Sync(ctx); err != nil {
			return nil, fmt.Errorf("tagent: the delivery obligations of %s left no write barrier: %w", addr.SessionID, err)
		}
	}
	if len(owed) == 0 {
		return nil, nil
	}
	return owed, nil
}

// owedDeliveryRecord reports whether the durable record of one delivery
// obligation still stands owed: a body the account registered and no send has
// discharged. Bytes the record cannot be read out of stand for no obligation
// here, because this call only decides whether to send again; the boot that
// meets the same bytes refuses to start beside them by name instead of guessing
// a disposition the record does not state.
func owedDeliveryRecord(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	var record hostDeliveryRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return false
	}
	return record.Status == hostDeliveryOwed
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
func (h *Host) handoffSettledBodies(ctx context.Context, addr event.Address, notice task.Notice) error {
	if h == nil || h.run == nil {
		return nil
	}
	bodies := generatedBodiesOf(h.run.SessionFacts(addr), noticeInvocation(notice))
	owed, err := h.recordDeliveryOwed(ctx, addr, deliveryLineageOf("task"), bodies)
	if err != nil {
		return err
	}
	for _, body := range owed {
		obligation, _ := freshObligationOf(body)
		delivery := plugin.Delivery{Content: body.content, Lineage: deliveryLineageOf("task"), Target: addr}
		if _, cause := h.deliverObligation(ctx, obligation, h.sender, delivery); cause != "" {
			assemblyDebug("tagent: the hand-off delivery to %s stands owed: %s", addr.SessionID, cause)
		}
	}
	return nil
}

// restoreHeldDelivery returns one held body to the waiting table after a
// decision could not be carried out, so a later approval can resolve it, and
// records the obligation beside the state it stands in. The table entry is
// published before the record is attempted: an obligation the account does not
// hold still waits in front of the caller.
func (h *Host) restoreHeldDelivery(ctx context.Context, held *heldDelivery, status string) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("%w: the held delivery of %s", ErrHostClosed, held.id)
	}
	if h.pendingDeliveries == nil {
		h.pendingDeliveries = map[string]*heldDelivery{}
	}
	h.pendingDeliveries[held.id] = held
	h.mu.Unlock()
	return h.recordDeliveryState(ctx, held, status)
}

// holdDelivery parks one body the host owes a target: the obligation keeps the
// identity its generation registered, or takes a host-local one beside a body no
// generation named, and the durable record beside the table entry carries the
// state the hold stands in. The caller delivers nothing.
//
// A record the tier refused is reported to the caller beside the published
// entry: the body stays visible to a decision, and the account stays behind it.
func (h *Host) holdDelivery(ctx context.Context, obligation string, delivery plugin.Delivery, reason, status string) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		assemblyWarn("tagent: the held delivery to %s was dropped beside a closed host", delivery.Target.SessionID)
		return fmt.Errorf("%w: the held delivery to %s", ErrHostClosed, delivery.Target.SessionID)
	}
	if h.pendingDeliveries == nil {
		h.pendingDeliveries = map[string]*heldDelivery{}
	}
	id := obligation
	if id == "" {
		h.heldSeq++
		id = fmt.Sprintf("delivery-%06d", h.heldSeq)
	}
	held := &heldDelivery{id: id, delivery: delivery, heldAt: time.Now()}
	h.pendingDeliveries[id] = held
	h.mu.Unlock()
	assemblyInfo("tagent: the delivery to %s was held: %s", delivery.Target.SessionID, reason)
	if err := h.recordDeliveryState(ctx, held, status); err != nil {
		return fmt.Errorf("tagent: the hold of %s left no record: %w", id, err)
	}
	return nil
}

// restorePendingDeliveries rebuilds the review table from the durable delivery
// records of the storage tier: every obligation still owed stands restored
// under the identity and hold moment it was recorded with, and the sequence
// behind the identities continues past the restored ones. A body the account
// holds as owed — generated and never sent — is sent here through the review
// ruling the live path applies, so a restart delivers what a stop left behind
// instead of dropping it; an owed body whose target is not the entry address is
// retired without a send instead, because a headless address's answer belongs to
// the caller that drove it and never to a message channel. A record that names no
// identity, or a body that does not unmarshal, refuses the boot by name: the host
// never starts beside an obligation it cannot honor.
func (h *Host) restorePendingDeliveries(ctx context.Context) error {
	log := h.deliveryDurable()
	if log == nil {
		return nil
	}
	records, err := log.Scan(ctx, hostDeliveryPrefix)
	if err != nil {
		return fmt.Errorf("tagent: read the delivery records: %w", err)
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	unattended := make([]hostDeliveryRecord, 0, len(keys))
	for _, key := range keys {
		var record hostDeliveryRecord
		if err := json.Unmarshal(records[key], &record); err != nil {
			return fmt.Errorf("tagent: the delivery record %s does not unmarshal: %w", key, err)
		}
		if record.ID == "" {
			return fmt.Errorf("tagent: the delivery record %s names no identity", key)
		}
		if seq := deliveryHeldSeq(record.ID); seq > h.heldSeq {
			h.heldSeq = seq
		}
		switch record.Status {
		case hostDeliveryOwed:
			if record.Target.AgentID != h.entry {
				h.retireHeadlessDelivery(ctx, record)
				continue
			}
			unattended = append(unattended, record)
			continue
		case hostDeliveryHeld, hostDeliveryRetry:
		default:
			continue
		}
		held := &heldDelivery{
			id:       record.ID,
			delivery: plugin.Delivery{Content: record.Content, Lineage: record.Lineage, Target: record.Target},
			heldAt:   record.HeldAt,
		}
		h.mu.Lock()
		if h.pendingDeliveries == nil {
			h.pendingDeliveries = map[string]*heldDelivery{}
		}
		h.pendingDeliveries[held.id] = held
		h.mu.Unlock()
		assemblyInfo("tagent: the delivery %s stands owed beside the restart: %s", record.ID, record.Status)
	}
	for _, record := range unattended {
		delivery := plugin.Delivery{Content: record.Content, Lineage: record.Lineage, Target: record.Target}
		if _, cause := h.deliverObligation(ctx, record.ID, h.sender, delivery); cause != "" {
			assemblyWarn("tagent: the owed delivery of %s still stands owed: %s", record.ID, cause)
			continue
		}
		assemblyInfo("tagent: the owed delivery of %s was delivered beside the restart", record.ID)
	}
	return nil
}

// retireHeadlessDelivery gives up the owed record of a body generated at an
// address that is not the entry: the host answers a headless address for the
// caller that drove it and never pushes its body to a message channel, so an
// obligation another generation registered there is closed under its own state
// instead of being delivered beside the restart. The record keeps the body, so a
// reader sees what was given up and why. A record the tier refuses is warned and
// left owed: nothing is sent here, and the next boot retires it again.
func (h *Host) retireHeadlessDelivery(ctx context.Context, record hostDeliveryRecord) {
	held := &heldDelivery{
		id:       record.ID,
		delivery: plugin.Delivery{Content: record.Content, Lineage: record.Lineage, Target: record.Target},
		heldAt:   record.HeldAt,
	}
	if err := h.recordDeliveryState(ctx, held, hostDeliveryHeadless); err != nil {
		assemblyWarn("tagent: the owed delivery of %s at the headless address %s was not retired: %v", record.ID, record.Target.AgentID, err)
		return
	}
	assemblyInfo("tagent: the owed delivery of %s at the headless address %s was retired without a send", record.ID, record.Target.AgentID)
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
// An obligation the sender accepted is recorded as sent behind its write
// barrier: a receipt the tier refused leaves the obligation owed, so a restart
// delivers the body once more rather than reading an unconfirmed send as
// discharged. That unconfirmed receipt is named in the second result beside the
// landed outcome, so a caller learns the body reached the channel and the account
// did not — the same note a hold carries when its own record did not land.
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
			return deliveryHeld, cause + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, cause, hostDeliveryHeld))
		case disposition == plugin.DispositionHold:
			cause := "the review held the delivery"
			return deliveryHeld, cause + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, cause, hostDeliveryHeld))
		case disposition == plugin.DispositionAllow, disposition == plugin.DispositionRecord:
		default:
			assemblyWarn("tagent: the review of the delivery to %s returned %q", delivery.Target.SessionID, disposition)
			cause := fmt.Sprintf("the delivery review returned %q", disposition)
			return deliveryHeld, cause + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, cause, hostDeliveryHeld))
		}
	}
	if err := sender.Send(ctx, delivery.Target, delivery.Content); err != nil {
		assemblyWarn("tagent: the host delivery to %s was refused: %v", delivery.Target.SessionID, err)
		reason := "the channel refused the delivery: " + err.Error()
		return deliveryRefused, err.Error() + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, reason, hostDeliveryRetry))
	}
	if obligation != "" {
		held := &heldDelivery{id: obligation, delivery: delivery, heldAt: time.Now()}
		if err := h.recordDeliveryState(ctx, held, hostDeliverySent); err != nil {
			assemblyWarn("tagent: the delivery of %s landed but its receipt did not: %v", obligation, err)
			return deliverySent, sentRecordNote(err)
		}
	}
	return deliverySent, ""
}

// sentRecordNote renders the receipt note one landed send adds when its record
// did not reach the account: the body was accepted by the channel and the
// obligation still stands owed, so a restart delivers it once more. The note is
// the counterpart of holdRecordNote, so a caller reads an unconfirmed receipt the
// same way it reads an unrecorded hold.
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

// deliveryLineageKey is the context key one running turn's trigger-source
// stamp is carried under, from the drive down to the send tool.
type deliveryLineageKey struct{}

// withDeliveryLineage stamps the trigger-source line of the running turn on the
// context the drive hands to its calls, so a cross-agent body names the line its
// turn names instead of inventing one.
func withDeliveryLineage(ctx context.Context, lineage string) context.Context {
	return context.WithValue(ctx, deliveryLineageKey{}, lineage)
}

// deliveryLineageFrom reports the line the running turn stamped and whether one
// is set.
func deliveryLineageFrom(ctx context.Context) (string, bool) {
	lineage, ok := ctx.Value(deliveryLineageKey{}).(string)
	if !ok || lineage == "" {
		return "", false
	}
	return lineage, true
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

// noteDeliveryRefusal commits the audit fact of one refused delivery: a
// feedback fact owned by the target address, carrying the hold identity and the
// delivery dimensions in its metadata and no model payload in its message, so
// the fact enters no model request. A refused commit answers the caller by name:
// a refusal is a decision the account states, not one the log states for it.
func (h *Host) noteDeliveryRefusal(ctx context.Context, held *heldDelivery) error {
	if h == nil || h.run == nil {
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
	bodies := make([]freshBody, 0, 2)
	for _, evt := range facts {
		if evt.InvocationID != invocation {
			continue
		}
		if body, ok := assistantBodyOf(evt); ok {
			bodies = append(bodies, body)
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
