// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The task registry's recoverable state as the framework checkpoint carries it:
// the account the registry holds — every task it admitted that the terminal
// sweep did not drop — stated whole, beside the chain revision each record
// stands at. The account is the size of the work the registry keeps, never the
// size of the chains it already settled, so a restarted process reads the tasks
// of the cut out of the carrier instead of folding every chain the storage ever
// accumulated. The fact chain stays the truth of a task and the source the tail
// replays from: the checkpoint is the derived snapshot that spares the covered
// part of it.
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
// entry per task record the registry holds, in task identity order.
type RegistryCheckpoint struct {
	// Tasks is the account of the registry at the cut.
	Tasks []TaskCheckpoint `json:"tasks,omitempty"`
}

// TaskCheckpoint is one task record as a checkpoint states it: the declaration
// the task was admitted under, the state it holds, the chain revision that state
// corresponds to, and the obligations the registry still owes.
//
// The fields a chain fold cannot compute back stay out of it — the blocked
// reasons, the exit reason, the cancellation request, the stop evidence and the
// reported verdict — because no committed fact carries them and a rebuilt task
// reads them empty rather than inferred. The checkpoint restores exactly what
// the fold restores, so the two recovery paths answer the same account.
type TaskCheckpoint struct {
	// TaskID is the task this entry states.
	TaskID event.TaskID `json:"task_id"`
	// Spec is the admission material of the task: the declaration its
	// notifications route by and its duplicate admissions resolve against.
	Spec Spec `json:"spec"`
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
	AdmittedAt time.Time `json:"admitted_at,omitempty"`
	ChangedAt  time.Time `json:"changed_at,omitempty"`
	RunSince   time.Time `json:"run_since,omitempty"`
	TTLAnchor  time.Time `json:"ttl_anchor,omitempty"`
	// Handoff is the routing an explicit handoff moved the future notifications
	// to, and HandoffRecord is the link that move recorded. A task no handoff
	// moved carries neither.
	Handoff       *HandoffTarget `json:"handoff,omitempty"`
	HandoffRecord HandoffRecord  `json:"handoff_record,omitempty"`
	// Mutations is every mutation the task committed, so a repeated mutation
	// after a restart is recognized as the one already stored instead of being
	// applied a second time.
	Mutations []CommitRecord `json:"mutations,omitempty"`
	// Obligations is every obligation the registry still holds for the task.
	Obligations []checkpointObligation `json:"obligations,omitempty"`
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

// SnapshotCheckpoint states every task record this registry holds, in task
// identity order, with the revision each record stands at.
func (r *MemoryRegistry) SnapshotCheckpoint(ctx context.Context) ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	r.mu.Lock()
	wire := RegistryCheckpoint{Tasks: make([]TaskCheckpoint, 0, len(r.tasks))}
	for taskID, entry := range r.tasks {
		if err := ctx.Err(); err != nil {
			r.mu.Unlock()
			return nil, err
		}
		wire.Tasks = append(wire.Tasks, entry.checkpointOf(taskID))
	}
	r.mu.Unlock()
	sort.Slice(wire.Tasks, func(i, j int) bool { return wire.Tasks[i].TaskID < wire.Tasks[j].TaskID })
	return json.Marshal(wire)
}

// RestoreCheckpoint puts the tasks a checkpoint states back as the account of
// this registry, and reports the revision each of them stands at through
// CheckpointCoverage, so the boot folds only the links that landed past it.
//
// The restore never overwrites a task the registry already holds: a task a chain
// fold rebuilt in this boot stands as the chain stated it, which is the state the
// checkpoint's entry is only a prefix of.
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
		for _, open := range entry.obligations {
			r.seq.Observe(open.envelope.Notice)
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

// checkpointOf states one task record as a checkpoint names it.
func (t *memoryTask) checkpointOf(taskID event.TaskID) TaskCheckpoint {
	stated := TaskCheckpoint{
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
func (stated TaskCheckpoint) record() (*memoryTask, error) {
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
	return entry, nil
}
