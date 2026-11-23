// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The task registry's recoverable state as the framework checkpoint carries it:
// the work the registry is still driving — every task it admitted that holds no
// terminal fact, or that reached one still owing a delivery — stated whole
// beside the chain revision each record stands at. A task that finished with
// every obligation cleared is stated by nothing at all: its record stays on the
// chain, and a reader that names it reads it back by identity. The derived
// account therefore costs the open work alone, never the tasks the registry
// finished however many of them the storage holds, which is what keeps the
// checkpoint body equal at every depth of a finished history.
//
// The cut the account corresponds to is the position of the checkpoint fact in
// the storage's commit order: the links the storage committed past it are the
// tail a boot replays, and the store states that position itself, so the account
// carries no watermark of its own. The fact chain stays the truth of a task and
// the source both the tail and an on-demand read come from: the checkpoint is
// the derived snapshot that spares the covered part of it and nothing else.
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// RegistryCheckpoint is the state one task registry states to the framework: one
// entry per task the registry still drives, in task identity order. A task the
// registry finished is named by no entry at all.
type RegistryCheckpoint struct {
	// Tasks is the open account of the registry at the cut.
	Tasks []StatedTask `json:"tasks,omitempty"`
}

// StatedTask is one task record as a checkpoint states it: the declaration
// the task was admitted under, the state it holds, the chain revision that state
// corresponds to, and the obligations the registry still owes.
//
// The fields a chain fold cannot compute back stay out of it — the blocked
// reasons, the exit reason, the cancellation request, the stop evidence and the
// reported verdict — because no committed fact carries them and a rebuilt task
// reads them empty rather than inferred. The checkpoint restores exactly what
// the fold restores, so the two recovery paths answer the same account.
type StatedTask struct {
	// TaskID is the task this entry states.
	TaskID event.TaskID `json:"task_id"`
	// Spec is the admission material of the task: the declaration its
	// notifications route by and its duplicate admissions resolve against.
	Spec Spec `json:"spec,omitzero"`
	// Status is the state the task holds at the cut.
	Status Status `json:"status"`
	// Revision is the chain revision the state corresponds to: the record that
	// lands next expects this revision, so a boot folds only the links at or
	// past it and reads no link the entry already covers.
	Revision int64 `json:"revision"`
	// Round is the execution round the task stands in.
	Round int64 `json:"round,omitempty"`
	// AdmittedAt, ChangedAt, RunSince and TTLAnchor are the movement stamps the
	// record is classified by. ChangedAt is the stamp the terminal sweep reads, so
	// a restored task kept the deadline it always had instead of one counted from
	// the restart; a link the tail of a chain folds past the cut carries no
	// instant, so the stamp such a transition leaves is unknown and the sweep
	// keeps the record, exactly as it does for a task rebuilt from its chain alone.
	AdmittedAt time.Time `json:"admitted_at,omitzero"`
	ChangedAt  time.Time `json:"changed_at,omitzero"`
	RunSince   time.Time `json:"run_since,omitzero"`
	TTLAnchor  time.Time `json:"ttl_anchor,omitzero"`
	// Handoff is the routing an explicit handoff moved the future notifications
	// to, and HandoffRecord is the link that move recorded. A task no handoff
	// moved carries neither.
	Handoff       *HandoffTarget `json:"handoff,omitempty"`
	HandoffRecord HandoffRecord  `json:"handoff_record,omitzero"`
	// Mutations is every mutation the task committed, so a repeated mutation
	// after a restart is recognized as the one already stored instead of being
	// applied a second time.
	Mutations []CommitRecord `json:"mutations,omitempty"`
	// Obligations is every obligation the registry still holds for the task.
	Obligations []checkpointObligation `json:"obligations,omitempty"`
	// Consumed, Attempts and Rejected are the settle account of the task: the
	// consumption receipts it completed, the attempts its follow-up turns are and
	// the refusals its targets answered. They are stated beside the chain so a boot
	// reads the account of a task the checkpoint covers instead of scanning the
	// durable prefixes of the whole history, and they are stated as empty lists
	// rather than left out, because an empty account is the answer "this task
	// completed no consumption", which is a different statement from a checkpoint
	// that never stated the family at all.
	Consumed []StatedConsumption `json:"consumed,omitzero"`
	Attempts []StatedAttempt     `json:"attempts,omitzero"`
	Rejected []StatedRefusal     `json:"rejected,omitzero"`
}

// StatedConsumption is one completed logical consumption of a task as a
// checkpoint states it: the notice identity the receipt answers beside the
// committed event the notice pointed at.
type StatedConsumption struct {
	// Notice is the four-part identity of the notification the receipt confirms.
	Notice Notice `json:"notice"`
	// Event is the committed fact the notification pointed at.
	Event event.EventID `json:"event,omitempty"`
}

// StatedAttempt is the attempt of one notification's follow-up turn as a
// checkpoint states it: the notice it belongs to beside the terminal that turn
// ended on, empty while the turn is in flight.
type StatedAttempt struct {
	// Notice is the four-part identity of the notification the attempt belongs to.
	Notice Notice `json:"notice"`
	// Terminal is the terminal the driven turn ended on.
	Terminal string `json:"terminal,omitempty"`
}

// StatedRefusal is the feedback fact of one notification a target refused as a
// checkpoint states it: the notice it answers beside the routing the delivery was
// offered to, the instance that offered it, the reason the target gave and the
// instant the refusal landed.
type StatedRefusal struct {
	// Notice is the four-part identity of the notification the target refused.
	Notice Notice `json:"notice"`
	// Target is the routing the delivery was offered to.
	Target event.Address `json:"target"`
	// Instance is the instance that offered the delivery.
	Instance event.InstanceID `json:"instance"`
	// Reason is the reason the target gave.
	Reason string `json:"reason"`
	// At is the instant the refusal landed.
	At time.Time `json:"at"`
}

// Receipts is the settle account one task's durable record holds: the
// consumption receipts it completed, the attempts its follow-up turns are and the
// refusals its targets answered.
type Receipts struct {
	Consumed []StatedConsumption
	Attempts []StatedAttempt
	Rejected []StatedRefusal
}

// SettleAccount is the durable receipt account a checkpoint states: the receipts
// one task holds. A commit over the durable log answers it; a checkpoint beside no
// durable commit states the empty account of every task it names.
//
// The redline list (3 rules):
// 红线: 结算账是只读面——只读已提交的 durable 记录，不写账、不改任务状态。
// MUST answer every task the checkpoint names, empty families stated as empty lists.
// MUST NOT answer a receipt of a task other than the one its notice belongs to.
// MUST NOT scan a durable prefix a checkpoint receipt already covers.
type SettleAccount interface {
	// ReceiptsOf answers the receipts one task holds, ordered by notice identity.
	ReceiptsOf(taskID event.TaskID) Receipts
}

// StateSettleAccount answers the account a registry states with the settle
// account of every task it names: the consumption receipts, the attempts and the
// refusals the durable record of that task holds. A boot that reads the checkpoint
// then reads the receipts of the tasks it covers out of the checkpoint instead of
// scanning the durable prefixes of a history the tasks already settled, and the
// account it states is the whole account of those tasks.
//
// A task beside no receipt states the three families as empty lists: a checkpoint
// that stated nothing would be indistinguishable from one written before the
// families existed, and the boot would have to scan the prefixes to tell them
// apart. A nil account, and a payload stating no task, answer the payload
// unchanged.
func StateSettleAccount(ctx context.Context, payload []byte, account SettleAccount) ([]byte, error) {
	if account == nil {
		return payload, nil
	}
	var wire RegistryCheckpoint
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, err
	}
	if len(wire.Tasks) == 0 {
		return payload, nil
	}
	for at := range wire.Tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		receipts := account.ReceiptsOf(wire.Tasks[at].TaskID)
		wire.Tasks[at].Consumed = statedReceipts(receipts.Consumed)
		wire.Tasks[at].Attempts = statedReceipts(receipts.Attempts)
		wire.Tasks[at].Rejected = statedReceipts(receipts.Rejected)
	}
	return json.Marshal(wire)
}

// statedReceipts answers one receipt family as a checkpoint states it: the
// receipts held, or an empty list when the task holds none.
func statedReceipts[T any](held []T) []T {
	if len(held) == 0 {
		return []T{}
	}
	return held
}

// checkpointObligation is one obligation a checkpoint states: the class it waits
// in, the notification it points at and the envelope that notification froze its
// routing into.
type checkpointObligation struct {
	// Kind is the class the obligation waits in.
	Kind ObligationKind `json:"kind"`
	// Notice is the notification the obligation points at; its zero value marks
	// an obligation that points at the task itself.
	Notice Notice `json:"notice,omitempty"`
	// Envelope is the delivery the notification froze at its transition, and the
	// zero value marks an obligation that carries no delivery of its own.
	Envelope Envelope `json:"envelope,omitempty"`
}

// SnapshotCheckpoint states every task this registry still drives, in task
// identity order: the tasks holding no terminal fact, and the tasks that reached
// one still owing a delivery, each whole beside the revision its record stands
// at. A task the registry finished states no entry: its record stays on the
// chain and is read back by identity, so the account costs the open work alone
// whatever the registry holds beside it.
func (r *MemoryRegistry) SnapshotCheckpoint(ctx context.Context) ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	r.mu.Lock()
	wire := RegistryCheckpoint{Tasks: make([]StatedTask, 0, len(r.tasks))}
	for taskID, entry := range r.tasks {
		if err := ctx.Err(); err != nil {
			r.mu.Unlock()
			return nil, err
		}
		if entry.settledAtCut() {
			continue
		}
		wire.Tasks = append(wire.Tasks, entry.checkpointOf(taskID))
	}
	r.mu.Unlock()
	sort.Slice(wire.Tasks, func(i, j int) bool { return wire.Tasks[i].TaskID < wire.Tasks[j].TaskID })
	return json.Marshal(wire)
}

// RestoreCheckpoint puts the open tasks a checkpoint states back as the account
// of this registry, and reports the revision each of them stands at through
// CheckpointCoverage, so the boot folds only the links that landed past it. A
// task the checkpoint never stated — one that finished before the cut — stays
// out of the account, and a reader that names it reads its chain by identity.
//
// The restore never overwrites a task the registry already holds: a task a chain
// fold rebuilt in this boot stands as the chain stated it, which is the state the
// checkpoint's entry is only a prefix of. It hands the sequencer every notice
// identity the stated mutations carry — the identities the account committed,
// open or closed — so both recovery paths answer one identity sequence: a notice
// issued after a restart never repeats an identity the account already used.
func (r *MemoryRegistry) RestoreCheckpoint(ctx context.Context, payload []byte) error {
	if r == nil {
		return nil
	}
	var wire RegistryCheckpoint
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	restored := make(map[event.TaskID]int64, len(wire.Tasks))
	for _, stated := range wire.Tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if stated.TaskID == "" {
			return fmt.Errorf("task: the checkpoint states a task with no identity")
		}
		if stated.Status == StatusUnknown {
			return fmt.Errorf("task: the checkpoint states the task %s at no determinable status", stated.TaskID)
		}
		if stated.Revision < 0 {
			return fmt.Errorf("task: the checkpoint states the task %s at revision %d", stated.TaskID, stated.Revision)
		}
		restored[stated.TaskID] = stated.Revision
		r.mu.Lock()
		if _, held := r.tasks[stated.TaskID]; held {
			r.mu.Unlock()
			continue
		}
		entry, err := stated.record()
		if err != nil {
			r.mu.Unlock()
			return err
		}
		r.tasks[stated.TaskID] = entry
		if entry.spec.DedupKey != "" {
			r.admission.AdmitKey(entry.spec.DedupKey, entry.spec.CallID, stated.TaskID)
		}
		for _, mutation := range stated.Mutations {
			for _, envelope := range mutation.Outbox {
				r.seq.Observe(envelope.Notice)
			}
		}
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.restored = restored
	r.mu.Unlock()
	return nil
}

// CheckpointCoverage reports the chain revision every task a checkpoint restored
// stands at, and is nil beside a registry this boot restored no checkpoint into.
// A boot reads it to fold only the links at or past the stated revision, so the
// covered part of a chain is never read again.
func (r *MemoryRegistry) CheckpointCoverage() map[event.TaskID]int64 {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.restored) == 0 {
		return nil
	}
	coverage := make(map[event.TaskID]int64, len(r.restored))
	for taskID, revision := range r.restored {
		coverage[taskID] = revision
	}
	return coverage
}

// settledAtCut reports whether this task finished before the cut: a task holding
// a terminal status with every obligation the work opened cleared is a task the
// run finished, so its record stays on its chain and the checkpoint states
// nothing of it. A terminal task still owing a delivery keeps its record stated,
// because that obligation is work the next process still holds.
func (t *memoryTask) settledAtCut() bool {
	return isTerminal(t.status) && len(t.obligations) == 0
}

// ChainSource answers the durable fact chain of one task by its identity: the
// read a checkpoint spares for the tasks it states whole, and the read a task
// the checkpoint never stated answers the first time a reader names it. An
// identity the source holds no link for answers no chain, which is how a task
// this storage never admitted reads as unknown. Its signature carries built-in
// and event types only, so the storage layer's commit satisfies it structurally
// and no package edge is required in either direction.
//
// The redline list (3 rules):
// MUST answer the fact chain of one task in commit order, its admission link first.
// MUST carry the admission material on the admission link alone.
// MUST NOT answer a chain that carries no admission material.
type ChainSource interface {
	// Records answers the durable fact chain of one task in commit order, the
	// admission link carrying the declaration the task was admitted under.
	Records(ctx context.Context, taskID event.TaskID) ([]TaskRecord, error)
}

// RestoreChainSource attaches the source this registry reads a task from when a
// reader names one the checkpoint never stated. A registry beside none answers
// the tasks the checkpoint stated and refuses every other identity, which is the
// account a run over the in-memory commit restores.
func (r *MemoryRegistry) RestoreChainSource(chains ChainSource) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.chains = chains
	r.mu.Unlock()
}

// hydrateLocked rebuilds one task the registry does not hold by reading its chain,
// the first time a reader names it instead of at the restore every boot pays: a
// task the checkpoint left on its chain — one that finished before the cut, or one
// whose record the process never restored — is answered from the record it
// committed, and one identity the source states no link for is answered as
// unknown. A registry beside no source answers the tasks it holds and refuses
// every other identity. The caller holds the lock.
func (r *MemoryRegistry) hydrateLocked(ctx context.Context, taskID event.TaskID) (*memoryTask, bool, error) {
	if entry, held := r.tasks[taskID]; held {
		return entry, true, nil
	}
	if r.chains == nil {
		return nil, false, nil
	}
	chain, err := r.chains.Records(ctx, taskID)
	if err != nil {
		return nil, false, err
	}
	for _, link := range chain {
		if _, err := r.foldLocked(link); err != nil {
			return nil, false, err
		}
	}
	entry, held := r.tasks[taskID]
	if !held {
		return nil, false, nil
	}
	if entry.spec.DedupKey != "" {
		r.admission.AdmitKey(entry.spec.DedupKey, entry.spec.CallID, taskID)
	}
	return entry, true, nil
}

// checkpointOf states one task record whole as a checkpoint names it: the record
// while the task holds work the registry still owes. The caller skips the tasks
// that finished at the cut, whose record stays on the chain.
func (t *memoryTask) checkpointOf(taskID event.TaskID) StatedTask {
	stated := StatedTask{
		TaskID:        taskID,
		Spec:          t.spec,
		Status:        t.status,
		Revision:      t.revision,
		Round:         t.round,
		AdmittedAt:    t.admittedAt,
		ChangedAt:     t.changedAt,
		RunSince:      t.runSince,
		TTLAnchor:     t.ttlAnchor,
		HandoffRecord: t.handoffRecord,
		Mutations:     make([]CommitRecord, 0, len(t.mutations)),
		Obligations:   make([]checkpointObligation, 0, len(t.obligations)),
		Consumed:      []StatedConsumption{},
		Attempts:      []StatedAttempt{},
		Rejected:      []StatedRefusal{},
	}
	if t.handoff != nil {
		moved := *t.handoff
		stated.Handoff = &moved
	}
	identities := make([]MutationID, 0, len(t.mutations))
	for identity := range t.mutations {
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i] < identities[j] })
	for _, identity := range identities {
		stated.Mutations = append(stated.Mutations, t.mutations[identity])
	}
	for _, open := range t.obligations {
		stated.Obligations = append(stated.Obligations, checkpointObligation{
			Kind:     open.obligation.Kind,
			Notice:   open.obligation.Notice,
			Envelope: open.envelope,
		})
	}
	return stated
}

// record answers the task record one checkpoint entry states.
func (stated StatedTask) record() (*memoryTask, error) {
	entry := &memoryTask{
		spec:          stated.Spec,
		status:        stated.Status,
		revision:      stated.Revision,
		round:         stated.Round,
		mutations:     make(map[MutationID]CommitRecord, len(stated.Mutations)),
		admittedAt:    stated.AdmittedAt,
		changedAt:     stated.ChangedAt,
		runSince:      stated.RunSince,
		ttlAnchor:     stated.TTLAnchor,
		handoffRecord: stated.HandoffRecord,
	}
	if stated.Revision < 0 {
		return nil, fmt.Errorf("task: the checkpoint states the task %s at revision %d", stated.TaskID, stated.Revision)
	}
	if stated.Round < 0 {
		return nil, fmt.Errorf("task: the checkpoint states the task %s in round %d", stated.TaskID, stated.Round)
	}
	if stated.Handoff != nil {
		moved := *stated.Handoff
		entry.handoff = &moved
	}
	for _, record := range stated.Mutations {
		if record.MutationID == "" {
			return nil, fmt.Errorf("task: the checkpoint states a mutation of the task %s with no identity", stated.TaskID)
		}
		if record.TaskID != stated.TaskID {
			return nil, fmt.Errorf("task: the checkpoint states the mutation %s of the task %s under %s",
				record.MutationID, record.TaskID, stated.TaskID)
		}
		entry.mutations[record.MutationID] = record
	}
	for _, open := range stated.Obligations {
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: open.Kind, TaskID: stated.TaskID, Notice: open.Notice},
			envelope:   open.Envelope,
		})
	}
	for _, receipt := range stated.Consumed {
		if err := receipt.statedUnder(stated.TaskID, "consumption receipt"); err != nil {
			return nil, err
		}
	}
	for _, attempt := range stated.Attempts {
		if err := attempt.statedUnder(stated.TaskID, "attempt"); err != nil {
			return nil, err
		}
	}
	for _, refusal := range stated.Rejected {
		if err := refusal.statedUnder(stated.TaskID, "refusal"); err != nil {
			return nil, err
		}
	}
	return entry, nil
}

// statedUnder refuses a receipt the checkpoint states under another task than the
// one it answers: a receipt filed under the wrong entry is a settle account the
// boot would lose without a word, so the restore names the mismatch instead of
// dropping the receipt.
func (consumed StatedConsumption) statedUnder(taskID event.TaskID, family string) error {
	return receiptStatedUnder(taskID, consumed.Notice, family)
}

// statedUnder refuses an attempt the checkpoint states under another task than
// the one it answers.
func (attempt StatedAttempt) statedUnder(taskID event.TaskID, family string) error {
	return receiptStatedUnder(taskID, attempt.Notice, family)
}

// statedUnder refuses a refusal the checkpoint states under another task than the
// one it answers.
func (refusal StatedRefusal) statedUnder(taskID event.TaskID, family string) error {
	return receiptStatedUnder(taskID, refusal.Notice, family)
}

// receiptStatedUnder answers nil when one receipt's notice belongs to the task
// the checkpoint states it under, and names the mismatch otherwise. A receipt
// whose own identity is empty is refused too: a notice identity is the four parts
// a receipt is read back by, so one without a task cannot be answered to anyone.
func receiptStatedUnder(taskID event.TaskID, notice Notice, family string) error {
	if notice.TaskID != taskID {
		return fmt.Errorf("task: the checkpoint states the %s of the task %s under %s", family, notice.TaskID, taskID)
	}
	return nil
}
