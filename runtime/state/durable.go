package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
//
// factPrefix, specPrefix, consumePrefix, attemptPrefix and rejectPrefix locate
// the five durable record kinds the commit keeps on one log: the commit facts,
// the admission material of their tasks, the receipt of each logical
// consumption a notification completed, the attempt the follow-up turn of one
// notification is, and the feedback fact of each notification a target refused.
// Each reuses the existing persistent identity material (the task_spawned
// record, the settle records and the notice identity), so the durable commit
// needs no other database and no second consumption state.
const (
	factPrefix    = "tagent/task/v1/fact:"
	specPrefix    = "tagent/task/v1/spec:"
	consumePrefix = "tagent/task/v1/consume:"
	attemptPrefix = "tagent/task/v1/attempt:"
	rejectPrefix  = "tagent/task/v1/reject:"
)

// mutationKey is the idempotency identity of one mutation: the task it changes
// and the mutation identifier the record carries.
type mutationKey struct {
	task     event.TaskID
	mutation task.MutationID
}

// consumeRecord is the durable receipt of one completed logical consumption: the
// four-part notice identity it confirms beside the committed fact the notice
// points at. The identity alone decides which notice the receipt is about; the
// event reference stays content beside it.
type consumeRecord struct {
	TaskID event.TaskID    `json:"task_id"`
	Round  int64           `json:"round"`
	Kind   task.NoticeKind `json:"kind"`
	Seq    int64           `json:"seq"`
	Event  event.EventID   `json:"event,omitempty"`
}

// SettleAttempt is the durable record of the one follow-up turn a notification
// started: the terminal that turn ended on, and empty while the turn is in
// flight. The record answers one question for the settle pump — whether a turn
// already drove this notification to its terminal — so a process that starts
// over the same log closes the entry beside the landed turn instead of driving a
// second one. It says nothing about delivery: the consumption receipt is the
// record of a delivered answer, and the two records are read as one account.
type SettleAttempt struct {
	// Terminal is the terminal the driven turn ended on, empty while the turn
	// is in flight.
	Terminal string `json:"terminal,omitempty"`
}

// attemptRecorder is the durable record of the attempt one notification's
// follow-up turn is: it is staged beside the consumption receipts, so a mailbox
// restored over the same log reads the attempt the stopped process left.
type attemptRecorder interface {
	// recordAttempt stages one attempt record behind the sync barrier, and a
	// repeated record of the same content stages no second pair.
	recordAttempt(ctx context.Context, notice task.Notice, attempt SettleAttempt) error
	// attemptOf reports the attempt record of one notification and whether the
	// log holds one at all.
	attemptOf(notice task.Notice) (SettleAttempt, bool)
}

// Rejection is the durable feedback fact of one notification a target refused:
// the identity of the notification it answers, the routing the delivery was
// offered to, and the reason the target gave. The record is the consumption
// result of that notification, so a DurableCommit opened over the same log reads the
// refusal back and owes the notification no delivery a second time.
type Rejection struct {
	Notice   task.Notice      `json:"notice"`
	Target   event.Address    `json:"target"`
	Instance event.InstanceID `json:"instance"`
	Reason   string           `json:"reason"`
	At       time.Time        `json:"at"`
}

// Feedback reports the refusal as the feedback fact it is: an audit fact of the
// target address, carrying the notification it answers beside the reason in its
// metadata and no model payload in its message, so the fact enters no model
// request.
func (r Rejection) Feedback() event.Event {
	return event.Event{
		Type:      event.TypeFeedback,
		Timestamp: r.At,
		TaskID:    r.Notice.TaskID,
		Owner:     r.Target,
		Source:    r.Target,
		Target:    r.Target,
		Meta: map[string]string{
			event.MetaKeyEventSummary: r.Reason,
			event.MetaKeyTaskID:       string(r.Notice.TaskID),
			event.MetaKeyNoticeKind:   string(r.Notice.Kind),
		},
	}
}

// rejectionRecorder is the durable record of the notifications a target refused:
// it is staged beside the consumption receipts, so a mailbox restored over the
// same log reads the refusals the stopped process landed.
type rejectionRecorder interface {
	// recordRejection stages one refusal behind the sync barrier, and a repeated
	// refusal of the same notification stages no second one.
	recordRejection(ctx context.Context, rejection Rejection) error
	// refusalOf reports the refusal of one notification and whether the log
	// holds one at all.
	refusalOf(notice task.Notice) (Rejection, bool)
	// refusals reports every refusal the log holds.
	refusals() []Rejection
}

// ErrNoticeNotOwed reports an acceptance of a notification the commit holds no
// delivery obligation for, so there is no delivery state to advance.
var ErrNoticeNotOwed = errors.New("state: the notification carries no delivery obligation")

// ErrSpecConflict reports a declaration that would replace the admission
// material of a task with other content than the material already stored.
var ErrSpecConflict = errors.New("state: the task's admission material is already declared with other content")

// ErrSeatRefused reports a checkpoint whose task entry cannot state the account
// of a commit: an entry of no identity, a receipt filed under another task than
// the one it answers, or a record of another task than the entry names. The boot
// refuses such a checkpoint by name instead of seating an account it would answer
// with a silently dropped receipt.
var ErrSeatRefused = errors.New("state: the task checkpoint states an account this commit cannot hold")

// DurableCommit is the durable implementation of the task commit port: it stores
// one self-contained record per mutation identity through a durable fact log behind the
// sync barrier, and holds every committed notification as an open delivery
// obligation. A repeated record of a mutation identity already stored returns
// the fact already stored and records no second one — and never advances a
// delivery state, because an idempotent hit reports a stored fact and not a
// delivered notification. Only an explicit acceptance by the target moves a
// delivery, through Accept. It also keeps the receipt of each logical
// consumption a notification completed, so a DurableCommit opened over the same log
// owes no delivery for a notice the log confirms as consumed.
//
// DurableCommit holds no wrapper, binding or lease: the log it stores through is
// supplied at construction and nothing an earlier run captured is revived.
type DurableCommit struct {
	mu        sync.Mutex
	log       durable.FactLog
	uncertain task.UncertainProbe
	revisions map[event.TaskID]int64
	facts     map[event.TaskID][]task.CommitRecord
	specs     map[event.TaskID]task.Spec
	mutations map[mutationKey]task.CommitRecord
	owed      map[noticeKey]task.OutboxNotice
	// consumed is the receipt of each logical consumption a notification completed,
	// beside the committed fact the notice pointed at: the receipt is what answers
	// the delivery, and the fact identity is content the checkpoint states beside it.
	consumed map[noticeKey]event.EventID
	attempts map[noticeKey]SettleAttempt
	rejected map[noticeKey]Rejection
	// receiptsByTask files each receipt identity under the task it answers, so one
	// task's settle account costs its own receipts and never the receipts of the
	// tasks the account holds beside it. It is written by the one function every
	// receipt write goes through, and read by ReceiptsOf alone.
	receiptsByTask map[event.TaskID]map[noticeKey]struct{}
	loadStats      LoadStats
	// seated reports that Seat read the account of this commit, so a second seat
	// states nothing twice.
	seated bool
}

// LoadScan is the read cost of one prefix scan made while rebuilding
// DurableCommit at startup — the store this port writes through, never the
// framework checkpoint. Keys is the number of pairs returned, Bytes their total
// value length, and Elapsed the wall time of the read alone — folding the
// pairs into the commit tables happens after the measurement and is covered
// by the restore observation instead. It is a measurement, not a durable
// record.
type LoadScan struct {
	Prefix  string
	Keys    int
	Bytes   int64
	Elapsed time.Duration
}

// LoadStats is the per-prefix read cost of the last load. The five entries
// appear in scan order; a load that returned early holds fewer entries.
type LoadStats struct {
	Scans []LoadScan
}

// LoadStats reports the measured read cost of the load that built this
// DurableCommit. It is read-only; the caller cannot mutate the commit through it.
func (d *DurableCommit) LoadStats() LoadStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := LoadStats{Scans: make([]LoadScan, len(d.loadStats.Scans))}
	copy(out.Scans, d.loadStats.Scans)
	return out
}

// DurableCommit satisfies the task commit port.
var _ task.Commit = (*DurableCommit)(nil)

// NewDurableCommit returns a DurableCommit over one fact log and
// loads every fact and consumption receipt the log already holds, so a second
// process over the same log sees exactly the facts the first one committed and
// owes no delivery for a notice that process already consumed. uncertain, when
// it is not nil, marks matching commit attempts as uncertain, so all three
// commit outcomes stay observable.
func NewDurableCommit(log durable.FactLog, uncertain task.UncertainProbe) (*DurableCommit, error) {
	d := newDurableCommit(log)
	d.uncertain = uncertain
	if err := d.load(context.Background()); err != nil {
		durableError("tagent: durable commit load failed err=%v", err)
		return nil, err
	}
	return d, nil
}

// NewEmptyDurableCommit answers the commit of one fact log with no record read
// into it: every table is empty and the caller states the account afterwards
// through Seat, which reads the account a checkpoint states instead of the
// durable prefixes of the whole history. A boot that holds a checkpoint reads it
// that way; NewDurableCommit is the entry for a caller that holds none and pays
// the whole log.
func NewEmptyDurableCommit(log durable.FactLog, uncertain task.UncertainProbe) *DurableCommit {
	d := newDurableCommit(log)
	d.uncertain = uncertain
	return d
}

// newDurableCommit answers one empty durable commit over the named log.
func newDurableCommit(log durable.FactLog) *DurableCommit {
	return &DurableCommit{
		log:            log,
		revisions:      map[event.TaskID]int64{},
		facts:          map[event.TaskID][]task.CommitRecord{},
		specs:          map[event.TaskID]task.Spec{},
		mutations:      map[mutationKey]task.CommitRecord{},
		owed:           map[noticeKey]task.OutboxNotice{},
		consumed:       map[noticeKey]event.EventID{},
		attempts:       map[noticeKey]SettleAttempt{},
		rejected:       map[noticeKey]Rejection{},
		receiptsByTask: map[event.TaskID]map[noticeKey]struct{}{},
	}
}

// load folds every record the log already holds back into DurableCommit's
// tables.
// A committed notification is loaded owed and pending again unless the log holds
// the receipt of its logical consumption: the storage layer records no
// acceptance, so only the target's explicit acceptance moves one and an
// unconfirmed delivery is replayed rather than trusted, while a consumption the
// log confirms is never owed a second time.
func (d *DurableCommit) load(ctx context.Context) error {
	specs, err := d.scan(ctx, specPrefix)
	if err != nil {
		return err
	}
	for key, body := range specs {
		var spec task.Spec
		if err := json.Unmarshal(body, &spec); err != nil {
			return err
		}
		d.specs[event.TaskID(key[len(specPrefix):])] = spec
	}
	receipts, err := d.scan(ctx, consumePrefix)
	if err != nil {
		return err
	}
	for _, body := range receipts {
		var receipt consumeRecord
		if err := json.Unmarshal(body, &receipt); err != nil {
			return err
		}
		notice := noticeKey{task: receipt.TaskID, round: receipt.Round,
			kind: receipt.Kind, seq: receipt.Seq}
		d.consumed[notice] = receipt.Event
		d.holdReceiptLocked(notice)
	}
	attempts, err := d.scan(ctx, attemptPrefix)
	if err != nil {
		return err
	}
	for _, body := range attempts {
		var record attemptRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return err
		}
		notice := noticeKey{task: record.TaskID, round: record.Round,
			kind: record.Kind, seq: record.Seq}
		d.attempts[notice] = record.Attempt
		d.holdReceiptLocked(notice)
	}
	rejections, err := d.scan(ctx, rejectPrefix)
	if err != nil {
		return err
	}
	for _, body := range rejections {
		var rejection Rejection
		if err := json.Unmarshal(body, &rejection); err != nil {
			return err
		}
		notice := noticeKeyOf(rejection.Notice)
		d.rejected[notice] = rejection
		d.holdReceiptLocked(notice)
	}
	facts, err := d.scan(ctx, factPrefix)
	if err != nil {
		return err
	}
	byTask := map[event.TaskID][]task.CommitRecord{}
	for _, body := range facts {
		record, err := task.DecodeTaskRecord(body)
		if err != nil {
			return err
		}
		byTask[record.Fact.TaskID] = append(byTask[record.Fact.TaskID], record.Fact)
	}
	for _, records := range byTask {
		sort.Slice(records, func(i, j int) bool {
			return records[i].ExpectedRevision < records[j].ExpectedRevision
		})
		for _, record := range records {
			d.applyRecordLocked(record)
		}
	}
	return nil
}

// applyRecordLocked folds one committed record into the tables a recovery reads:
// the fact chain of its task, the mutation identity it may be repeated under, the
// revision its successor expects and the notifications it opened beside the ones
// a receipt already closed.
//
// One record is applied here whichever path read it — a runtime commit, a
// checkpoint that covered the record, and the tail a boot replays past that
// checkpoint — so the account a restart restores is the account the process
// committed, never a second derivation of it. A record of a mutation identity the
// tables already hold applies nothing: a checkpoint that states it beside the tail
// that repeats it seats the record once. The caller holds the lock.
func (d *DurableCommit) applyRecordLocked(record task.CommitRecord) {
	key := mutationKey{task: record.TaskID, mutation: record.MutationID}
	if _, held := d.mutations[key]; held {
		return
	}
	d.facts[record.TaskID] = append(d.facts[record.TaskID], record)
	d.mutations[key] = record
	if revision := record.ExpectedRevision + 1; revision > d.revisions[record.TaskID] {
		d.revisions[record.TaskID] = revision
	}
	d.oweLocked(record)
}

// settledLocked reports whether one notification identity is already answered: a
// consumption receipt or a refusal is that notification's consumption result, so
// neither leaves a delivery obligation open. The caller holds the lock.
func (d *DurableCommit) settledLocked(notice noticeKey) bool {
	if _, consumed := d.consumed[notice]; consumed {
		return true
	}
	_, rejected := d.rejected[notice]
	return rejected
}

// holdReceiptLocked files one receipt identity under the task it answers. Every
// write of the three receipt families goes through it, so a task holds a receipt
// of the account exactly when ReceiptsOf answers it. The caller holds the lock.
func (d *DurableCommit) holdReceiptLocked(notice noticeKey) {
	held, known := d.receiptsByTask[notice.task]
	if !known {
		held = map[noticeKey]struct{}{}
		d.receiptsByTask[notice.task] = held
	}
	held[notice] = struct{}{}
}

// oweLocked opens the delivery obligations one committed record carries: every
// notification of its outbox no receipt and no refusal has already closed. The
// runtime commit and the recovery seat of the same record both fold it through
// here, so one account answers one owed set whichever path built it — a record
// committing a notification the account already settled re-owes nothing. The
// caller holds the lock.
func (d *DurableCommit) oweLocked(record task.CommitRecord) {
	for _, envelope := range record.Outbox {
		notice := noticeKeyOf(envelope.Notice)
		if d.settledLocked(notice) {
			continue
		}
		d.owed[notice] = task.OutboxNotice{Notice: envelope, Delivery: task.DeliveryPending}
	}
}

// settled reports whether the account already answered one notification: the
// consumption receipt or the refusal the log holds for it. A restore reads the
// account through it so a payload stating a notice the account settled past the
// cut opens no delivery for it.
func (d *DurableCommit) settled(notice task.Notice) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.settledLocked(noticeKeyOf(notice))
}

// Seat reads the account this commit holds from the tasks one checkpoint states
// beside the records the storage committed past that cut, and states it without
// scanning the durable prefixes of a history those tasks already settled. The read
// is stated once: a repeated Seat answers nothing, so the framework checkpoint and
// the session checkpoints of one boot seat one account.
//
//   - The covered part is seated from the entries themselves: the admission material, the mutation identities a repeat resolves against, the revision the next record expects and the consumption receipts, attempts and refusals the task holds, so a task a checkpoint states costs no durable key of any family and the load does not grow with the tasks the storage already settled. A task that finished before the cut states no entry, so the account holds no identity of it until a reader names it.
//   - The tail is what `tail` names: the keys the storage committed past the cut, whose tasks are read by their own identity — the admission key, the fact prefix of that task and its three receipt prefixes — so a chain younger than the cut costs its own length and never the history behind it; `tail` beside no cut is nil, and nil is not the empty set, because nil is a storage that states no cut at all.
//   - The fallback reads the whole log: a caller that holds no checkpoint payload beside a storage that states no cut is a boot with no cut to read from, and an entry that states no receipt family is read back from its chain the same way, because a checkpoint written before those families existed cannot state the account of one — reading its missing keys as "no receipt" would owe every consumption it completed a second time.
func (d *DurableCommit) Seat(ctx context.Context, stated []byte, tail map[string]struct{}) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seated {
		return nil
	}
	if len(stated) == 0 || tail == nil {
		if err := d.load(ctx); err != nil {
			durableError("tagent: durable commit load failed err=%v", err)
			return err
		}
		d.seated = true
		return nil
	}
	var wire task.RegistryCheckpoint
	if err := json.Unmarshal(stated, &wire); err != nil {
		return fmt.Errorf("%w: the checkpoint states no readable task account: %w", ErrSeatRefused, err)
	}
	young := tailTaskIDs(tail)
	for _, entry := range wire.Tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Consumed == nil || entry.Attempts == nil || entry.Rejected == nil {
			durableWarn("tagent: durable seat found a task slice that states no settle account task=%s reading its chain instead", entry.TaskID)
			young[entry.TaskID] = struct{}{}
			continue
		}
		if err := d.seatTaskLocked(entry); err != nil {
			return err
		}
	}
	for _, taskID := range sortedTaskIDs(young) {
		if err := d.readTaskLocked(ctx, taskID); err != nil {
			return err
		}
	}
	d.seated = true
	return nil
}

// Seated reports whether this commit read the account it holds.
func (d *DurableCommit) Seated() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seated
}

// seatTaskLocked seats one task a checkpoint states: its admission material, its
// settle account beside it, and the records it committed. A receipt the entry
// files under another task than the one it answers, and a record of another task,
// refuse the seat by name: an account seated with a silently dropped receipt owes
// the notification it closed a second delivery. The account holds the identity
// whether or not the entry carried a record, so a task the checkpoint states is
// answered by this account and never read by identity as one it never seated. The
// caller holds the lock.
func (d *DurableCommit) seatTaskLocked(entry task.StatedTask) error {
	taskID := entry.TaskID
	if taskID == "" {
		return fmt.Errorf("%w: an entry states no task identity", ErrSeatRefused)
	}
	if entry.Revision < 0 {
		return fmt.Errorf("%w: the task %s is stated at revision %d", ErrSeatRefused, taskID, entry.Revision)
	}
	d.specs[taskID] = entry.Spec
	for _, consumed := range entry.Consumed {
		if consumed.Notice.TaskID != taskID {
			return fmt.Errorf("%w: the task %s states a consumption receipt of the task %s",
				ErrSeatRefused, taskID, consumed.Notice.TaskID)
		}
		notice := noticeKeyOf(consumed.Notice)
		d.consumed[notice] = consumed.Event
		d.holdReceiptLocked(notice)
	}
	for _, attempt := range entry.Attempts {
		if attempt.Notice.TaskID != taskID {
			return fmt.Errorf("%w: the task %s states an attempt of the task %s",
				ErrSeatRefused, taskID, attempt.Notice.TaskID)
		}
		notice := noticeKeyOf(attempt.Notice)
		d.attempts[notice] = SettleAttempt{Terminal: attempt.Terminal}
		d.holdReceiptLocked(notice)
	}
	for _, refusal := range entry.Rejected {
		if refusal.Notice.TaskID != taskID {
			return fmt.Errorf("%w: the task %s states a refusal of the task %s",
				ErrSeatRefused, taskID, refusal.Notice.TaskID)
		}
		notice := noticeKeyOf(refusal.Notice)
		d.rejected[notice] = Rejection{
			Notice: refusal.Notice, Target: refusal.Target, Instance: refusal.Instance,
			Reason: refusal.Reason, At: refusal.At,
		}
		d.holdReceiptLocked(notice)
	}
	records := make([]task.CommitRecord, len(entry.Mutations))
	copy(records, entry.Mutations)
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].ExpectedRevision != records[j].ExpectedRevision {
			return records[i].ExpectedRevision < records[j].ExpectedRevision
		}
		return records[i].MutationID < records[j].MutationID
	})
	for _, record := range records {
		if record.TaskID != taskID {
			return fmt.Errorf("%w: the task %s states the record %s of the task %s",
				ErrSeatRefused, taskID, record.MutationID, record.TaskID)
		}
		if record.MutationID == "" {
			return fmt.Errorf("%w: the task %s states a record of no mutation identity", ErrSeatRefused, taskID)
		}
		d.applyRecordLocked(record)
	}
	if _, held := d.facts[taskID]; !held {
		d.facts[taskID] = nil
	}
	return nil
}

// readTaskLocked reads one task by its own identity: the admission key, the fact
// prefix of that task and the three receipt prefixes of its notifications. It is
// the read a chain younger than the cut costs, and it is recorded in the load
// stats beside every other read this commit pays. The caller holds the lock.
func (d *DurableCommit) readTaskLocked(ctx context.Context, taskID event.TaskID) error {
	specs, err := d.scan(ctx, specPrefix+string(taskID))
	if err != nil {
		return err
	}
	if body, held := specs[specPrefix+string(taskID)]; held {
		var spec task.Spec
		if err := json.Unmarshal(body, &spec); err != nil {
			return err
		}
		if _, seated := d.specs[taskID]; !seated {
			d.specs[taskID] = spec
		}
	}
	receipts, err := d.scan(ctx, consumePrefix+string(taskID)+"\x00")
	if err != nil {
		return err
	}
	for _, body := range receipts {
		var receipt consumeRecord
		if err := json.Unmarshal(body, &receipt); err != nil {
			return err
		}
		notice := noticeKey{task: receipt.TaskID, round: receipt.Round,
			kind: receipt.Kind, seq: receipt.Seq}
		d.consumed[notice] = receipt.Event
		d.holdReceiptLocked(notice)
	}
	attempts, err := d.scan(ctx, attemptPrefix+string(taskID)+"\x00")
	if err != nil {
		return err
	}
	for _, body := range attempts {
		var record attemptRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return err
		}
		notice := noticeKey{task: record.TaskID, round: record.Round,
			kind: record.Kind, seq: record.Seq}
		d.attempts[notice] = record.Attempt
		d.holdReceiptLocked(notice)
	}
	rejections, err := d.scan(ctx, rejectPrefix+string(taskID)+"\x00")
	if err != nil {
		return err
	}
	for _, body := range rejections {
		var rejection Rejection
		if err := json.Unmarshal(body, &rejection); err != nil {
			return err
		}
		notice := noticeKeyOf(rejection.Notice)
		d.rejected[notice] = rejection
		d.holdReceiptLocked(notice)
	}
	facts, err := d.scan(ctx, factPrefix+string(taskID)+"\x00")
	if err != nil {
		return err
	}
	records := make([]task.CommitRecord, 0, len(facts))
	for _, body := range facts {
		record, err := task.DecodeTaskRecord(body)
		if err != nil {
			return err
		}
		records = append(records, record.Fact)
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].ExpectedRevision < records[j].ExpectedRevision
	})
	for _, record := range records {
		d.applyRecordLocked(record)
	}
	return nil
}

// tailTaskIDs answers the tasks the tail keys belong to: the identities a
// storage names past its cut, one per key of the five durable families. A key of
// no task family names no task, because a tail is read for the work it carries and
// never as a source of task identities.
func tailTaskIDs(tail map[string]struct{}) map[event.TaskID]struct{} {
	ids := make(map[event.TaskID]struct{}, len(tail))
	for key := range tail {
		if rest, held := strings.CutPrefix(key, specPrefix); held {
			ids[event.TaskID(rest)] = struct{}{}
			continue
		}
		for _, prefix := range []string{factPrefix, consumePrefix, attemptPrefix, rejectPrefix} {
			rest, held := strings.CutPrefix(key, prefix)
			if !held {
				continue
			}
			if at := strings.Index(rest, "\x00"); at > 0 {
				ids[event.TaskID(rest[:at])] = struct{}{}
			}
			break
		}
	}
	return ids
}

// sortedTaskIDs answers the identities of one set in a stable order, so a seat
// reads the same tail in the same order whatever the map walked.
func sortedTaskIDs(ids map[event.TaskID]struct{}) []event.TaskID {
	out := make([]event.TaskID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// scan reads one prefix and records its cost into the load stats. The
// measurement covers only the read; a failed scan records nothing, so the
// stats always describe a load that reached the end of that prefix.
func (d *DurableCommit) scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	started := time.Now()
	pairs, err := d.log.Scan(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var bytes int64
	for _, body := range pairs {
		bytes += int64(len(body))
	}
	d.loadStats.Scans = append(d.loadStats.Scans, LoadScan{
		Prefix: prefix, Keys: len(pairs), Bytes: bytes, Elapsed: time.Since(started),
	})
	return pairs, nil
}

// attemptRecord is the durable form of one attempt: the four-part notice
// identity it belongs to beside the terminal the driven turn ended on.
type attemptRecord struct {
	TaskID  event.TaskID    `json:"task_id"`
	Round   int64           `json:"round"`
	Kind    task.NoticeKind `json:"kind"`
	Seq     int64           `json:"seq"`
	Attempt SettleAttempt   `json:"attempt"`
}

// recordAttempt stores the attempt of one notification's follow-up turn behind
// the sync barrier, so a DurableCommit opened over the same log reads the terminal the
// stopped process's turn ended on. The record is keyed by the notice's
// four-part identity and a repeated record of the same content stages no second
// pair. A barrier that did not complete reports the write failure and confirms
// nothing, so the caller never reports an attempt the log does not hold.
func (d *DurableCommit) recordAttempt(ctx context.Context, notice task.Notice, attempt SettleAttempt) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if notice.TaskID == "" {
		return ErrNoticeIdentityMissing
	}
	key := noticeKeyOf(notice)
	if held, done := d.attempts[key]; done && held == attempt {
		return nil
	}
	body, err := json.Marshal(attemptRecord{TaskID: notice.TaskID, Round: notice.Round,
		Kind: notice.Kind, Seq: notice.Seq, Attempt: attempt})
	if err != nil {
		durableError("tagent: durable attempt encode failed task=%s notice=%s err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	if err := d.log.Put(ctx, attemptKey(notice), body); err != nil {
		durableError("tagent: durable attempt write failed task=%s notice=%s err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	if err := d.log.Sync(ctx); err != nil {
		durableError("tagent: durable attempt barrier failed task=%s notice=%s err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	d.attempts[key] = attempt
	d.holdReceiptLocked(key)
	return nil
}

// attemptOf reports the attempt record of one notification, and whether the
// commit holds one at all: no record means no turn ever started for the
// notification, and a record beside an empty terminal means the turn started
// and has not reported a terminal yet.
func (d *DurableCommit) attemptOf(notice task.Notice) (SettleAttempt, bool) {
	if d == nil {
		return SettleAttempt{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	attempt, ok := d.attempts[noticeKeyOf(notice)]
	return attempt, ok
}

// recordConsumption stores the receipt of one completed logical consumption
// behind the sync barrier, so a DurableCommit opened over the same log reads that
// notice as consumed and owes it no delivery. The receipt is keyed by the
// notice's four-part identity: a repeated receipt of the same identity stages no
// second pair and reports success. A receipt whose barrier did not complete
// reports the write failure and confirms nothing, so the caller keeps the
// notice owed rather than closing it on an unconfirmed write.
func (d *DurableCommit) recordConsumption(ctx context.Context, notice task.Notice) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if notice.TaskID == "" {
		return ErrNoticeIdentityMissing
	}
	key := noticeKeyOf(notice)
	if _, done := d.consumed[key]; done {
		return nil
	}
	body, err := json.Marshal(consumeRecord{TaskID: notice.TaskID, Round: notice.Round,
		Kind: notice.Kind, Seq: notice.Seq, Event: notice.Event})
	if err != nil {
		durableError("tagent: durable consumption encode failed task=%s notice=%s err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	if err := d.log.Put(ctx, consumeKey(notice), body); err != nil {
		durableError("tagent: durable consumption write failed task=%s notice=%s result=unconsumed err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	if err := d.log.Sync(ctx); err != nil {
		durableError("tagent: durable consumption barrier failed task=%s notice=%s result=unconsumed err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	d.consumed[key] = notice.Event
	d.holdReceiptLocked(key)
	delete(d.owed, key)
	return nil
}

// recordRejection stores the feedback fact of one notification a target refused
// behind the sync barrier, so a DurableCommit opened over the same log reads that
// refusal and owes the notification no delivery a second time. The refusal is
// keyed by the notice's four-part identity: a repeated refusal of the same
// identity stages no second fact and reports success. A barrier that did not
// complete reports the write failure and confirms nothing, so the caller keeps
// the notification owed rather than closing it on an unconfirmed write.
func (d *DurableCommit) recordRejection(ctx context.Context, rejection Rejection) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	notice := rejection.Notice
	if notice.TaskID == "" {
		return ErrNoticeIdentityMissing
	}
	key := noticeKeyOf(notice)
	if _, done := d.rejected[key]; done {
		return nil
	}
	body, err := json.Marshal(rejection)
	if err != nil {
		durableError("tagent: durable refusal encode failed task=%s notice=%s err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	if err := d.log.Put(ctx, rejectKey(notice), body); err != nil {
		durableError("tagent: durable refusal write failed task=%s notice=%s result=owed err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	if err := d.log.Sync(ctx); err != nil {
		durableError("tagent: durable refusal barrier failed task=%s notice=%s result=owed err=%v",
			notice.TaskID, notice.FactKey(), err)
		return err
	}
	d.rejected[key] = rejection
	d.holdReceiptLocked(key)
	delete(d.owed, key)
	return nil
}

// Rejections reports every refusal the commit holds, in the stable task, round,
// kind and sequence order of the notifications they answer.
func (d *DurableCommit) Rejections() []Rejection {
	return d.refusals()
}

// refusalOf reports the refusal one notification carries, and whether the log
// holds one at all: a notification with no refusal is still owed its delivery.
func (d *DurableCommit) refusalOf(notice task.Notice) (Rejection, bool) {
	if d == nil {
		return Rejection{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rejection, held := d.rejected[noticeKeyOf(notice)]
	return rejection, held
}

// refusals reports every refusal the commit holds, in the stable task, round,
// kind and sequence order of the notifications they answer.
func (d *DurableCommit) refusals() []Rejection {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	refusals := make([]Rejection, 0, len(d.rejected))
	for _, rejection := range d.rejected {
		refusals = append(refusals, rejection)
	}
	sort.Slice(refusals, func(i, j int) bool {
		left, right := refusals[i].Notice, refusals[j].Notice
		if left.TaskID != right.TaskID {
			return left.TaskID < right.TaskID
		}
		if left.Round != right.Round {
			return left.Round < right.Round
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Seq < right.Seq
	})
	return refusals
}

// Commit implements the task commit port. A record of a mutation identity the
// commit already stores returns the fact already stored and records no second
// fact — and advances no delivery state: the notifications of the stored fact
// stay owed and pending until their targets explicitly accept them. The same
// identity beside other content is refused by name, as is a record expecting
// another revision. The record opens a delivery obligation for each notification
// of its outbox no consumption receipt and no refusal has already closed, which
// is the obligation a recovery seat of the same record opens, so one account
// answers one owed set whichever path built it. A record is staged behind the
// sync barrier before the commit reports it committed, so a barrier that fails
// reports an uncertain outcome instead of a durable success.
func (d *DurableCommit) Commit(ctx context.Context, record task.CommitRecord) (task.CommitResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if record.MutationID == "" {
		return task.CommitNotCommitted, task.ErrMutationMissing
	}
	key := mutationKey{task: record.TaskID, mutation: record.MutationID}
	if prior, ok := d.mutations[key]; ok {
		if !sameRecord(prior, record) {
			return task.CommitNotCommitted, task.ErrMutationConflict
		}
		return task.CommitCommitted, nil
	}
	if d.uncertain != nil && d.uncertain(record) {
		return task.CommitUncertain, nil
	}
	if current := d.revisions[record.TaskID]; current != record.ExpectedRevision {
		return task.CommitNotCommitted, task.ErrRevisionConflict
	}
	body, err := task.EncodeTaskRecord(task.TaskRecord{Spec: d.specs[record.TaskID], Fact: record})
	if err != nil {
		durableError("tagent: durable commit encode failed task=%s mutation=%s err=%v", record.TaskID, record.MutationID, err)
		return task.CommitNotCommitted, err
	}
	if err := d.log.Put(ctx, factKey(record.TaskID, record.MutationID), body); err != nil {
		durableError("tagent: durable commit write failed task=%s mutation=%s result=uncommitted err=%v",
			record.TaskID, record.MutationID, err)
		return task.CommitNotCommitted, err
	}
	if err := d.log.Sync(ctx); err != nil {
		durableError("tagent: durable commit barrier failed task=%s mutation=%s result=uncertain err=%v",
			record.TaskID, record.MutationID, err)
		return task.CommitUncertain, err
	}
	d.revisions[record.TaskID] = record.ExpectedRevision + 1
	d.facts[record.TaskID] = append(d.facts[record.TaskID], record)
	d.mutations[key] = record
	d.oweLocked(record)
	return task.CommitCommitted, nil
}

// Declare stores the admission material of one task — the task_spawned record —
// beside the commit facts of the same log, so a restore rebuilds the task from
// material the chain already carries. The trigger-source stamp of the admission
// travels inside that material: a restore reads the stamp the admission
// recorded instead of deriving one again. The declaration is idempotent for the
// same content and refuses to replace stored material with other content.
func (d *DurableCommit) Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if prior, ok := d.specs[taskID]; ok && prior != spec {
		return ErrSpecConflict
	}
	body, err := json.Marshal(spec)
	if err != nil {
		durableError("tagent: durable declare encode failed task=%s err=%v", taskID, err)
		return err
	}
	if err := d.log.Put(ctx, specPrefix+string(taskID), body); err != nil {
		durableError("tagent: durable declare write failed task=%s result=undeclared err=%v", taskID, err)
		return err
	}
	if err := d.log.Sync(ctx); err != nil {
		durableError("tagent: durable declare barrier failed task=%s result=undeclared err=%v", taskID, err)
		return err
	}
	d.specs[taskID] = spec
	return nil
}

// Accept advances the delivery state of one notification to accepted. This is
// the only advance there is: a commit never moves a delivery, and an idempotent
// commit repeat leaves every notification owed and pending. A notification the
// commit holds no obligation for is refused by name.
func (d *DurableCommit) Accept(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := noticeKey{task: notice.TaskID, round: notice.Round, kind: notice.Kind, seq: notice.Seq}
	entry, ok := d.owed[key]
	if !ok {
		return task.DeliveryPending, ErrNoticeNotOwed
	}
	entry.Delivery = task.DeliveryAccepted
	d.owed[key] = entry
	return task.DeliveryAccepted, nil
}

// Facts returns the records the commit stored for one task, in commit order.
func (d *DurableCommit) Facts(taskID event.TaskID) []task.CommitRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]task.CommitRecord(nil), d.facts[taskID]...)
}

// Revision returns the revision the task is at after the records stored for it.
func (d *DurableCommit) Revision(taskID event.TaskID) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.revisions[taskID]
}

// Outbox returns the notifications one task still owes delivery, each paired
// with its delivery-layer state, ordered by round, kind and sequence.
func (d *DurableCommit) Outbox(taskID event.TaskID) []task.OutboxNotice {
	d.mu.Lock()
	defer d.mu.Unlock()
	var owed []task.OutboxNotice
	for key, entry := range d.owed {
		if key.task == taskID {
			owed = append(owed, entry)
		}
	}
	sort.Slice(owed, func(i, j int) bool {
		left, right := owed[i].Notice.Notice, owed[j].Notice.Notice
		if left.Round != right.Round {
			return left.Round < right.Round
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Seq < right.Seq
	})
	return owed
}

// OwedNotices reports every notification the commit still owes delivery on,
// in the stable task, round, kind and sequence order: the open obligations of
// the account, beside which the ones a consumption receipt confirms never
// appear again.
func (d *DurableCommit) OwedNotices() []task.OutboxNotice {
	d.mu.Lock()
	defer d.mu.Unlock()
	owed := make([]task.OutboxNotice, 0, len(d.owed))
	for _, entry := range d.owed {
		owed = append(owed, entry)
	}
	sort.Slice(owed, func(i, j int) bool {
		left, right := owed[i].Notice.Notice, owed[j].Notice.Notice
		if left.TaskID != right.TaskID {
			return left.TaskID < right.TaskID
		}
		if left.Round != right.Round {
			return left.Round < right.Round
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Seq < right.Seq
	})
	return owed
}

// TaskIDs returns every task this account holds, in identity order: the tasks a
// checkpoint stated beside the tasks the storage committed past its cut. The set
// is the open work of the account and costs no read of its own, so a boot asks it
// to name the chains it must fold and no chain of a task that finished before the
// cut appears in the answer. The whole history behind that account is
// HistoryTaskIDs, which is the read of a caller that asks for it.
func (d *DurableCommit) TaskIDs() []event.TaskID {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := make([]event.TaskID, 0, len(d.facts))
	for taskID := range d.facts {
		ids = append(ids, taskID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
	return ids
}

// HistoryTaskIDs reports every task identity the log holds a fact for, across the
// whole history the storage carries: the tasks this account holds beside every
// task a checkpoint left on its chain.
//
// The read is the whole fact family of the log, so its cost is linear in the
// history — O(history) — and it belongs to a caller that asks for the history: a
// boot reads the account its checkpoint states and the tail past it and pays
// neither. A caller that names this set reads each task back by identity through
// Records, so the set alone costs the fact bodies the scan returns.
func (d *DurableCommit) HistoryTaskIDs(ctx context.Context) ([]event.TaskID, error) {
	if d == nil {
		return nil, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	facts, err := d.scan(ctx, factPrefix)
	if err != nil {
		return nil, err
	}
	seen := make(map[event.TaskID]struct{}, len(facts))
	for key := range facts {
		rest, held := strings.CutPrefix(key, factPrefix)
		if !held {
			continue
		}
		if at := strings.Index(rest, "\x00"); at > 0 {
			seen[event.TaskID(rest[:at])] = struct{}{}
		}
	}
	ids := make([]event.TaskID, 0, len(seen))
	for taskID := range seen {
		ids = append(ids, taskID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
	return ids, nil
}

// Records returns the durable fact chain of one task as restore links, in
// commit order: the admission link carries the stored task_spawned material,
// every later link carries its fact alone. A notification whose consumption
// receipt the log holds is left out of the outbox of its link, so a rebuild
// opens no delivery obligation for a notice already confirmed as consumed.
//
// A task this account does not hold is read by its own identity here, the first
// time a reader asks for its chain: a task that finished before the cut states no
// entry, so its record is answered on demand and no boot pays for a task nobody
// names. An identity the log holds no link for answers no chain.
func (d *DurableCommit) Records(ctx context.Context, taskID event.TaskID) ([]task.TaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, held := d.facts[taskID]; !held {
		if err := d.readTaskLocked(ctx, taskID); err != nil {
			return nil, err
		}
	}
	records := d.facts[taskID]
	chain := make([]task.TaskRecord, 0, len(records))
	for i, fact := range records {
		link := task.TaskRecord{Fact: d.unowedLocked(fact)}
		if i == 0 {
			link.Spec = d.specs[taskID]
		}
		chain = append(chain, link)
	}
	return chain, nil
}

// ReceiptsOf answers the settle account one task's durable record holds: the
// consumption receipts it completed, the attempts its follow-up turns are and the
// refusals its targets answered, each ordered by the notice identity it names. A
// checkpoint states the account through it, so a boot reads the receipts of the
// tasks the checkpoint covers instead of scanning the durable prefixes of every
// task the storage ever held. The answer is filed per task, so it costs the
// receipts of that task and never those of the tasks the account holds beside it.
// The answer is a copy: the caller cannot change the account through it.
func (d *DurableCommit) ReceiptsOf(taskID event.TaskID) task.Receipts {
	if d == nil {
		return task.Receipts{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	receipts := task.Receipts{}
	for key := range d.receiptsByTask[taskID] {
		notice := task.Notice{TaskID: key.task, Round: key.round, Kind: key.kind, Seq: key.seq}
		if committed, consumed := d.consumed[key]; consumed {
			receipts.Consumed = append(receipts.Consumed, task.StatedConsumption{
				Notice: notice,
				Event:  committed,
			})
		}
		if attempt, held := d.attempts[key]; held {
			receipts.Attempts = append(receipts.Attempts, task.StatedAttempt{
				Notice:   notice,
				Terminal: attempt.Terminal,
			})
		}
		if rejection, refused := d.rejected[key]; refused {
			receipts.Rejected = append(receipts.Rejected, task.StatedRefusal{
				Notice: rejection.Notice, Target: rejection.Target, Instance: rejection.Instance,
				Reason: rejection.Reason, At: rejection.At,
			})
		}
	}
	sort.Slice(receipts.Consumed, func(i, j int) bool {
		return noticeBefore(receipts.Consumed[i].Notice, receipts.Consumed[j].Notice)
	})
	sort.Slice(receipts.Attempts, func(i, j int) bool {
		return noticeBefore(receipts.Attempts[i].Notice, receipts.Attempts[j].Notice)
	})
	sort.Slice(receipts.Rejected, func(i, j int) bool {
		return noticeBefore(receipts.Rejected[i].Notice, receipts.Rejected[j].Notice)
	})
	return receipts
}

// noticeBefore reports whether one notification identity stands before another in
// the stable task, round, kind and sequence order every receipt family is stated
// in.
func noticeBefore(left, right task.Notice) bool {
	if left.TaskID != right.TaskID {
		return left.TaskID < right.TaskID
	}
	if left.Round != right.Round {
		return left.Round < right.Round
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.Seq < right.Seq
}

// unowedLocked returns one stored fact as a restore link: the notifications of
// its outbox whose consumption receipt the log holds are left out, so a rebuild
// opens no obligation for them; so is the notification a target refused, because
// the refusal the log holds is that notification's consumption result and a
// rebuild owes it no delivery a second time. The stored record itself keeps the
// outbox it committed, so a repeated commit compares against the content the
// transition carried.
func (d *DurableCommit) unowedLocked(fact task.CommitRecord) task.CommitRecord {
	if len(fact.Outbox) == 0 {
		return fact
	}
	outbox := make([]task.Envelope, 0, len(fact.Outbox))
	for _, envelope := range fact.Outbox {
		if d.settledLocked(noticeKeyOf(envelope.Notice)) {
			continue
		}
		outbox = append(outbox, envelope)
	}
	if len(outbox) == len(fact.Outbox) {
		return fact
	}
	fact.Outbox = outbox
	return fact
}

// sameRecord reports whether a stored record and a repeated one carry the same
// content: the same expected revision, the same status change and the same
// outbox.
func sameRecord(stored, record task.CommitRecord) bool {
	return stored.ExpectedRevision == record.ExpectedRevision &&
		stored.Status == record.Status &&
		equalOutbox(stored.Outbox, record.Outbox)
}

// equalOutbox reports whether two outboxes carry the same envelopes in the same
// order.
func equalOutbox(left, right []task.Envelope) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// factKey locates one commit fact on the log by the task it changes and the
// mutation identity it carries.
func factKey(taskID event.TaskID, mutation task.MutationID) string {
	return factPrefix + string(taskID) + "\x00" + string(mutation)
}

// consumeKey locates the consumption receipt of one notification on the log by
// the four parts of the notice identity it confirms.
func consumeKey(notice task.Notice) string {
	return consumePrefix + string(notice.TaskID) + "\x00" + strconv.FormatInt(notice.Round, 10) +
		"\x00" + string(notice.Kind) + "\x00" + strconv.FormatInt(notice.Seq, 10)
}

// attemptKey locates the attempt of one notification's follow-up turn on the log
// by the same four parts, so the record and its receipt name one identity.
func attemptKey(notice task.Notice) string {
	return attemptPrefix + string(notice.TaskID) + "\x00" + strconv.FormatInt(notice.Round, 10) +
		"\x00" + string(notice.Kind) + "\x00" + strconv.FormatInt(notice.Seq, 10)
}

// rejectKey locates the feedback fact of one notification a target refused on the
// log by the same four parts, so the refusal names the notification it answers.
func rejectKey(notice task.Notice) string {
	return rejectPrefix + string(notice.TaskID) + "\x00" + strconv.FormatInt(notice.Round, 10) +
		"\x00" + string(notice.Kind) + "\x00" + strconv.FormatInt(notice.Seq, 10)
}

// durableError renders one line of the persistence face at ERROR: a write, a
// barrier or a load the durable commit could not complete, beside the task and
// the mutation it was about and the outcome the caller is answered with. A
// failure here changes what the process may claim to have stored, which is a
// semantic change nobody should have to grep for, so the level is ERROR and
// never a quieter one.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a changed outcome.
// MUST write one line per failed write, failed barrier or failed load.
// MUST NOT render the record body, the arguments or the stored bytes.
func durableError(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Errorf(format, args...)
}

// durableWarn renders one line at WARN: a recovery decision that keeps the
// account correct while costing a read a bounded path would have skipped — a task
// slice written before it stated a settle account, read back from its chain
// instead. It is written once per affected task and per boot, never per request,
// so the retrieval hint is bounded by the account rather than by traffic.
func durableWarn(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Warnf(format, args...)
}

// BindingOwners resolves a restored target's owner against the current
// orchestration binding: the tool set the current effective generation holds
// for the owner. A target the owner's current tool set does not hold is
// refused by name with task.ErrTargetRemoved, so a restore or a replay refuses
// the re-delivery rather than faking it, silently re-routing it, or serving it
// from a stale generation. The holder is supplied at construction and nothing
// is read from a top-level singleton.
type BindingOwners struct {
	bindings *Bindings
}

// BindingOwners satisfies the owner resolution the restore and the replay
// resolve through.
var _ task.OwnerBinding = (*BindingOwners)(nil)

// NewBindingOwners returns an owner resolution over one binding holder's
// current effective generation.
func NewBindingOwners(bindings *Bindings) *BindingOwners {
	return &BindingOwners{bindings: bindings}
}

// OwnerOf reports the owner that target belongs to under the current
// orchestration binding: the owner whose current tool set still holds the
// target. An owner the binding does not hold and a target the owner's tool
// set does not hold are both refused with task.ErrTargetRemoved beside the
// cause, since either means the current binding carries no such target for
// that owner.
func (o *BindingOwners) OwnerOf(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error) {
	if o == nil || o.bindings == nil {
		return "", ErrBindingHolderMissing
	}
	handle, err := o.bindings.Borrow(ctx, owner)
	if err != nil {
		return "", fmt.Errorf("%w: owner %s target %s: %v", task.ErrTargetRemoved, owner, target, err)
	}
	defer handle.Release(ctx)
	for _, tool := range handle.Binding.Tools {
		if tool == nil {
			continue
		}
		declaration := tool.Declaration()
		if declaration != nil && declaration.Name == string(target) {
			return owner, nil
		}
	}
	return "", fmt.Errorf("%w: owner %s holds no target %s in its current tool set", task.ErrTargetRemoved, owner, target)
}
