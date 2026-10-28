package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
//
// FactLog is the durable fact surface one durable commit carrier stores
// through: put, get, prefix scan and the sync barrier a commit reports its
// success behind. Its signatures carry built-in types only, so any backend —
// the memory tier's key-value port among them — satisfies it structurally and
// no package edge is required in either direction.
//
// The redline list (4 rules):
// MUST stage one key and its value without claiming it durable.
// MUST complete the write barrier over everything staged before anyone reports durable success.
// MUST NOT report staged bytes as stored.
// MUST NOT answer a prefix scan with a subset of the pairs under that prefix.
type FactLog interface {
	// Put stages one key and its value.
	Put(ctx context.Context, key string, value []byte) error
	// Get returns the value stored under key and whether one is held.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Scan returns every stored pair under the key prefix as key and value.
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)
	// Sync completes the write barrier over everything staged so far, so a
	// caller reports durable success only behind a barrier that returned.
	Sync(ctx context.Context) error
}

// factPrefix, specPrefix, consumePrefix and attemptPrefix locate the four
// durable record kinds the carrier keeps on one log: the commit facts, the
// admission material of their tasks, the receipt of each logical consumption a
// notification completed, and the attempt the follow-up turn of one notification
// is. Each reuses the existing persistent identity material (the task_spawned
// record, the settle records and the notice identity), so the durable commit
// needs no other database and no second consumption state.
const (
	factPrefix    = "tagent/task/v1/fact:"
	specPrefix    = "tagent/task/v1/spec:"
	consumePrefix = "tagent/task/v1/consume:"
	attemptPrefix = "tagent/task/v1/attempt:"
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

// ErrNoticeNotOwed reports an acceptance of a notification the carrier holds no
// delivery obligation for, so there is no delivery state to advance.
var ErrNoticeNotOwed = errors.New("state: the notification carries no delivery obligation")

// ErrSpecConflict reports a declaration that would replace the admission
// material of a task with other content than the material already stored.
var ErrSpecConflict = errors.New("state: the task's admission material is already declared with other content")

// DurableCommit is the durable carrier behind the task commit port: it stores
// one self-contained record per mutation identity through a FactLog behind the
// sync barrier, and holds every committed notification as an open delivery
// obligation. A repeated record of a mutation identity already stored returns
// the fact already stored and records no second one — and never advances a
// delivery state, because an idempotent hit reports a stored fact and not a
// delivered notification. Only an explicit acceptance by the target moves a
// delivery, through Accept. The carrier also keeps the receipt of each logical
// consumption a notification completed, so a carrier opened over the same log
// owes no delivery for a notice the log confirms as consumed.
//
// The carrier holds no wrapper, binding or lease: the log it stores through is
// supplied at construction and nothing an earlier run captured is revived.
type DurableCommit struct {
	mu        sync.Mutex
	log       FactLog
	uncertain task.UncertainProbe
	revisions map[event.TaskID]int64
	facts     map[event.TaskID][]task.CommitRecord
	specs     map[event.TaskID]task.Spec
	mutations map[mutationKey]task.CommitRecord
	owed      map[noticeKey]task.OutboxNotice
	consumed  map[noticeKey]struct{}
	attempts  map[noticeKey]SettleAttempt
}

// DurableCommit satisfies the task commit port.
var _ task.Commit = (*DurableCommit)(nil)

// NewDurableCommit returns a durable commit carrier over one fact log and
// loads every fact and consumption receipt the log already holds, so a second
// process over the same log sees exactly the facts the first one committed and
// owes no delivery for a notice that process already consumed. uncertain, when
// it is not nil, marks matching commit attempts as uncertain, so all three
// commit outcomes stay observable.
func NewDurableCommit(log FactLog, uncertain task.UncertainProbe) (*DurableCommit, error) {
	d := &DurableCommit{
		log:       log,
		uncertain: uncertain,
		revisions: map[event.TaskID]int64{},
		facts:     map[event.TaskID][]task.CommitRecord{},
		specs:     map[event.TaskID]task.Spec{},
		mutations: map[mutationKey]task.CommitRecord{},
		owed:      map[noticeKey]task.OutboxNotice{},
		consumed:  map[noticeKey]struct{}{},
		attempts:  map[noticeKey]SettleAttempt{},
	}
	if err := d.load(context.Background()); err != nil {
		durableError("tagent: durable commit load failed err=%v", err)
		return nil, err
	}
	return d, nil
}

// load folds every record the log already holds back into the carrier's tables.
// A committed notification is loaded owed and pending again unless the log holds
// the receipt of its logical consumption: the storage layer records no
// acceptance, so only the target's explicit acceptance moves one and an
// unconfirmed delivery is replayed rather than trusted, while a consumption the
// log confirms is never owed a second time.
func (d *DurableCommit) load(ctx context.Context) error {
	specs, err := d.log.Scan(ctx, specPrefix)
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
	receipts, err := d.log.Scan(ctx, consumePrefix)
	if err != nil {
		return err
	}
	for _, body := range receipts {
		var receipt consumeRecord
		if err := json.Unmarshal(body, &receipt); err != nil {
			return err
		}
		d.consumed[noticeKey{task: receipt.TaskID, round: receipt.Round,
			kind: receipt.Kind, seq: receipt.Seq}] = struct{}{}
	}
	attempts, err := d.log.Scan(ctx, attemptPrefix)
	if err != nil {
		return err
	}
	for _, body := range attempts {
		var record attemptRecord
		if err := json.Unmarshal(body, &record); err != nil {
			return err
		}
		d.attempts[noticeKey{task: record.TaskID, round: record.Round,
			kind: record.Kind, seq: record.Seq}] = record.Attempt
	}
	facts, err := d.log.Scan(ctx, factPrefix)
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
	for taskID, records := range byTask {
		sort.Slice(records, func(i, j int) bool {
			return records[i].ExpectedRevision < records[j].ExpectedRevision
		})
		for _, record := range records {
			d.facts[taskID] = append(d.facts[taskID], record)
			d.mutations[mutationKey{task: taskID, mutation: record.MutationID}] = record
			if revision := record.ExpectedRevision + 1; revision > d.revisions[taskID] {
				d.revisions[taskID] = revision
			}
			for _, envelope := range record.Outbox {
				if _, consumed := d.consumed[noticeKeyOf(envelope.Notice)]; consumed {
					continue
				}
				d.owed[noticeKey{task: envelope.Notice.TaskID, round: envelope.Notice.Round,
					kind: envelope.Notice.Kind, seq: envelope.Notice.Seq}] = task.OutboxNotice{
					Notice:   envelope,
					Delivery: task.DeliveryPending,
				}
			}
		}
	}
	return nil
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
// the sync barrier, so a carrier opened over the same log reads the terminal the
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
	return nil
}

// attemptOf reports the attempt record of one notification, and whether the
// carrier holds one at all: no record means no turn ever started for the
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
// behind the sync barrier, so a carrier opened over the same log reads that
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
	d.consumed[key] = struct{}{}
	delete(d.owed, key)
	return nil
}

// Commit implements the task commit port. A record of a mutation identity the
// carrier already stores returns the fact already stored and records no second
// fact — and advances no delivery state: the notifications of the stored fact
// stay owed and pending until their targets explicitly accept them. The same
// identity beside other content is refused by name, as is a record expecting
// another revision. A record is staged behind the sync barrier before the
// carrier reports it committed, so a barrier that fails reports an uncertain
// outcome instead of a durable success.
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
	for _, envelope := range record.Outbox {
		d.owed[noticeKey{task: envelope.Notice.TaskID, round: envelope.Notice.Round,
			kind: envelope.Notice.Kind, seq: envelope.Notice.Seq}] = task.OutboxNotice{
			Notice:   envelope,
			Delivery: task.DeliveryPending,
		}
	}
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
// carrier holds no obligation for is refused by name.
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

// Facts returns the records the carrier stored for one task, in commit order.
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

// OwedNotices reports every notification the carrier still owes delivery on,
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

// TaskIDs returns every task the carrier holds facts for, in numeric order.
func (d *DurableCommit) TaskIDs() []event.TaskID {
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

// Records returns the durable fact chain of one task as restore links, in
// commit order: the admission link carries the stored task_spawned material,
// every later link carries its fact alone. A notification whose consumption
// receipt the log holds is left out of the outbox of its link, so a rebuild
// opens no delivery obligation for a notice already confirmed as consumed.
func (d *DurableCommit) Records(ctx context.Context, taskID event.TaskID) ([]task.TaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
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

// unowedLocked returns one stored fact as a restore link: the notifications of
// its outbox whose consumption receipt the log holds are left out, so a rebuild
// opens no obligation for them. The stored record itself keeps the outbox it
// committed, so a repeated commit compares against the content the transition
// carried.
func (d *DurableCommit) unowedLocked(fact task.CommitRecord) task.CommitRecord {
	if len(fact.Outbox) == 0 {
		return fact
	}
	outbox := make([]task.Envelope, 0, len(fact.Outbox))
	for _, envelope := range fact.Outbox {
		if _, consumed := d.consumed[noticeKeyOf(envelope.Notice)]; consumed {
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

// durableError renders one line of the persistence face at ERROR: a write, a
// barrier or a load the durable carrier could not complete, beside the task and
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
