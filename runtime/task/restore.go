package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
//
// TaskRecord is one link of the durable fact chain a cold start rebuilds a task
// registry from: the commit fact beside the admission material of the task it
// belongs to. The admission link — the task_spawned material — carries the Spec;
// every later link carries none, so the chain reuses the existing persistent
// identity material and declares no new orchestration persistence format.
type TaskRecord struct {
	// Spec is the admission material of the task (the task_spawned record). It
	// is carried by the admission link alone and is zero on every later link.
	Spec Spec
	// Fact is the self-contained commit fact this link stores, in exactly the
	// 02.2 commit-port shape.
	Fact CommitRecord
}

// EncodeTaskRecord returns the canonical bytes of one restore link. The record
// is self-contained: the bytes carry the fact beside the admission material
// that belongs to it.
func EncodeTaskRecord(record TaskRecord) ([]byte, error) {
	return json.Marshal(record)
}

// DecodeTaskRecord reads one restore link back from its canonical bytes.
func DecodeTaskRecord(body []byte) (TaskRecord, error) {
	var record TaskRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return TaskRecord{}, err
	}
	return record, nil
}

// OwnerBinding resolves the owner a restored target belongs to under the
// current orchestration binding. The resolution reads the tool set the current
// binding holds for the owner — never a top-level singleton — and refuses a
// target the owner does not hold. Its signature carries built-in and event
// types only, so the state layer's binding owners satisfy it structurally and
// no package edge is required in either direction.
//
// The redline list (3 rules):
// MUST resolve the owner from the tool set the current binding holds for that owner.
// MUST refuse a target the owner does not hold, by name.
// MUST NOT resolve an owner from a top-level singleton or from a most-recent target.
type OwnerBinding interface {
	// OwnerOf reports the owner that target belongs to under the current
	// orchestration binding, within the tool set of the named owner. A target
	// the owner does not hold is refused by name.
	OwnerOf(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error)
}

// ErrTargetRemoved reports a restored target that the current orchestration
// binding does not hold in the tool set of its owner. The re-delivery is
// refused by name: it is never faked as a success, never silently re-routed,
// and never resurrected through a stale wrapper, binding or lease.
var ErrTargetRemoved = errors.New("task: the target is removed from its owner's tool set under the current binding")

// ErrNoOwnerBinding reports a restore that could resolve no owner, because no
// owner binding was supplied to it.
var ErrNoOwnerBinding = errors.New("task: no owner binding is attached to this restore")

// ErrChainBroken reports a fact chain link whose expected revision does not
// follow the record it restores into, so the chain cannot be folded any
// further.
var ErrChainBroken = errors.New("task: the fact chain does not follow the record it restores into")

// ErrSpecMissing reports a fact chain whose admission link carries no admission
// material, so the task cannot be rebuilt from the chain alone. The chain is
// refused by name and reports no recovery: a chain is self-contained by
// construction and a rebuild from a link that carries no admission material is
// never attempted.
var ErrSpecMissing = errors.New("task: the fact chain carries no admission material")

// RestoreOutcome is the per-task result of one restore: the status the task was
// rebuilt at, the owner the current binding resolved its target to, and the
// named refusal of a re-delivery the binding refused.
type RestoreOutcome struct {
	// TaskID is the task the outcome is about.
	TaskID event.TaskID
	// Status is the status the task holds after the fold.
	Status Status
	// Owner is the owner the current binding resolved the task's target to; it
	// is empty when the resolution refused.
	Owner event.AgentID
	// Recovered reports whether this restore rebuilt the task: from the chain
	// links it folded, from the carrier a checkpoint stated, or from neither —
	// false reports an idempotent hit, where the registry already held the task
	// and the link added no second fact, or a chain the restore refused without
	// rebuilding anything.
	Recovered bool
	// Refused is the named refusal of this restore, set when the current
	// binding resolves no owner for the task's target or when the chain carries
	// no admission material. The task chain and its obligations are kept when
	// the task itself was rebuilt; errors.Is reports task.ErrTargetRemoved for a
	// target the binding removed and task.ErrSpecMissing for a chain carrying no
	// admission material, which rebuilds nothing and reports no recovery.
	Refused error
}

// RestoreReport is the report one restore returns, one outcome per task the
// chain covers, in first-appearance order.
type RestoreReport struct {
	// Tasks lists the per-task outcomes of the restore.
	Tasks []RestoreOutcome
}

// Restore rebuilds the registry from one durable fact chain and admits each
// rebuilt task idempotently: a link the registry already folded adds no second
// fact and opens no second obligation, so a repeated restore of the same chain
// changes nothing. Each task's target is resolved against the current
// orchestration binding through owners — never through a top-level singleton —
// and a target the binding refuses keeps its whole task chain and every
// obligation open while the re-delivery is refused by name.
//
// The restore retains no wrapper, binding or lease: the chain and the owner
// binding are supplied per call and nothing captured by an earlier run is
// revived through it.
func (r *MemoryRegistry) Restore(ctx context.Context, chain []TaskRecord, owners OwnerBinding) (RestoreReport, error) {
	if owners == nil {
		return RestoreReport{}, ErrNoOwnerBinding
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	report := RestoreReport{Tasks: make([]RestoreOutcome, 0, len(chain))}
	index := make(map[event.TaskID]int, len(chain))
	for _, record := range chain {
		recovered, err := r.foldLocked(record)
		if err != nil {
			if !errors.Is(err, ErrSpecMissing) {
				return report, err
			}
			taskID := record.Fact.TaskID
			pos, seen := index[taskID]
			if !seen {
				pos = len(report.Tasks)
				index[taskID] = pos
				report.Tasks = append(report.Tasks, RestoreOutcome{TaskID: taskID})
			}
			if report.Tasks[pos].Refused == nil {
				report.Tasks[pos].Refused = fmt.Errorf("%w: task %s", ErrSpecMissing, taskID)
			}
			continue
		}
		taskID := record.Fact.TaskID
		pos, seen := index[taskID]
		if !seen {
			pos = len(report.Tasks)
			index[taskID] = pos
			report.Tasks = append(report.Tasks, RestoreOutcome{TaskID: taskID})
		}
		if recovered {
			report.Tasks[pos].Recovered = true
		}
		report.Tasks[pos].Status = r.tasks[taskID].status
	}
	r.resolveOutcomeOwnersLocked(ctx, &report, owners)
	return report, nil
}

// CheckpointRestored reports one outcome per task a framework checkpoint
// restored: the state the restored record holds and the owner the current
// orchestration binding resolves its target to, resolved exactly as Restore
// resolves the target of a rebuilt task. The outcome reports the recovery,
// because the carrier rebuilt the task exactly as a chain fold rebuilds it and
// the two paths answer the same account; a task the registry does not hold is
// left out of the report.
func (r *MemoryRegistry) CheckpointRestored(ctx context.Context, taskIDs []event.TaskID, owners OwnerBinding) (RestoreReport, error) {
	if owners == nil {
		return RestoreReport{}, ErrNoOwnerBinding
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	report := RestoreReport{Tasks: make([]RestoreOutcome, 0, len(taskIDs))}
	for _, taskID := range taskIDs {
		entry, held := r.tasks[taskID]
		if !held {
			continue
		}
		report.Tasks = append(report.Tasks, RestoreOutcome{TaskID: taskID, Status: entry.status, Recovered: true})
	}
	r.resolveOutcomeOwnersLocked(ctx, &report, owners)
	return report, nil
}

// resolveOutcomeOwnersLocked resolves the owner of every outcome of one report
// against the current orchestration binding, under the registry lock its caller
// holds: a target the binding does not hold keeps its task and is refused by
// name instead of being re-routed.
func (r *MemoryRegistry) resolveOutcomeOwnersLocked(ctx context.Context, report *RestoreReport, owners OwnerBinding) {
	for i := range report.Tasks {
		entry, ok := r.tasks[report.Tasks[i].TaskID]
		if !ok {
			continue
		}
		owner := event.AgentID(entry.spec.Source.AgentID)
		target := event.AgentID(entry.spec.Target.AgentID)
		if target == "" {
			report.Tasks[i].Owner = owner
			continue
		}
		resolved, err := owners.OwnerOf(ctx, owner, target)
		if err != nil {
			report.Tasks[i].Refused = err
			continue
		}
		report.Tasks[i].Owner = resolved
	}
}

// foldLocked applies one chain link to the registry. A link the registry
// already folded reports no recovery and adds no second fact; a fresh link
// rebuilds its task from the admission material the chain carries and derives
// its obligations from the facts alone.
//
// The fold is where a stored status and the task's round are rebuilt. A link
// written while progress was still a status stores the reserved ordinal that
// replayedStatus reads as the running phase, its progress notification staying
// on the chain; the round is the highest the chain really opened — a terminal
// notice names it, a settle or rollback mutation encodes the one it consumed,
// and a reactivation or resume opens exactly one new round. Notices a restart
// still owes are stripped from their outbox once the log confirms consumption,
// so the round reads from the mutations beside them too: a confirmed terminal
// is consumed, not unopened.
//
//   - a link carries no instant, so the movement stamp its transition leaves is unknown rather than the one a checkpoint stated before the cut: a record a fold moved is stamped exactly as a record rebuilt from the chain alone, and the sweep and the logical read answer the same thing on both recovery paths.
func (r *MemoryRegistry) foldLocked(record TaskRecord) (bool, error) {
	fact := record.Fact
	if fact.MutationID == "" {
		return false, ErrMutationMissing
	}
	if fact.Status == StatusUnknown {
		return false, ErrStatusUnknown
	}
	fact.Status = replayedStatus(fact.Status)
	taskID := fact.TaskID
	entry, held := r.tasks[taskID]
	recovered := false
	if !held {
		if record.Spec == (Spec{}) {
			return false, ErrSpecMissing
		}
		recovered = true
		entry = &memoryTask{
			spec:      record.Spec,
			status:    StatusAccepted,
			revision:  fact.ExpectedRevision,
			round:     1,
			mutations: map[MutationID]CommitRecord{},
		}
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: ObligationAwaitingTool, TaskID: taskID},
		})
		r.tasks[taskID] = entry
		if record.Spec.DedupKey != "" {
			r.admission.AdmitKey(record.Spec.DedupKey, record.Spec.CallID, taskID)
		}
	} else if prior, ok := entry.mutations[fact.MutationID]; ok {
		if prior.Status != fact.Status || !slices.Equal(prior.Outbox, fact.Outbox) {
			return false, ErrMutationConflict
		}
		return false, nil
	} else if entry.revision != fact.ExpectedRevision {
		return false, fmt.Errorf("%w: task %s expects revision %d, the chain carries %d",
			ErrChainBroken, taskID, entry.revision, fact.ExpectedRevision)
	}
	entry.markMovement(fact.Status, time.Time{})
	entry.status = fact.Status
	entry.revision = fact.ExpectedRevision + 1
	entry.mutations[fact.MutationID] = fact
	if target, carried := handoffTargetOf(fact.MutationID); carried {
		entry.applyHandoff(taskID, target)
	}
	entry.round = replayedRound(taskID, entry.round, fact)
	for _, envelope := range fact.Outbox {
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: ObligationAwaitingDelivery, TaskID: taskID, Notice: envelope.Notice},
			envelope:   envelope,
		})
		r.seq.Observe(envelope.Notice)
		if envelope.Notice.Round > entry.round {
			entry.round = envelope.Notice.Round
		}
	}
	if isTerminal(fact.Status) {
		entry.closeExecutionObligation("")
	}
	return recovered, nil
}

// replayedRound reports the execution round a chain stands at after one fact:
// a reactivation or resume mutation opens one round, a settle or rollback
// mutation names the round it consumed, and the task keeps the highest round it
// has seen. The round is the current one, so a later re-entry opens the one
// after it; returning the next round here would count the re-entry itself a
// second time.
func replayedRound(taskID event.TaskID, round int64, fact CommitRecord) int64 {
	if opensRound(taskID, fact.MutationID) {
		round++
	}
	if carried, ok := roundOfMutation(taskID, fact.MutationID); ok && carried > round {
		round = carried
	}
	return round
}

// opensRound reports whether one mutation identity opens a new execution round
// of the named task: a reactivation of a settled task and a resume of an
// existing one each move the task into a round of its own.
func opensRound(taskID event.TaskID, mutation MutationID) bool {
	text := string(mutation)
	task := string(taskID) + ":"
	return strings.HasPrefix(text, reactivatePrefix+task) || strings.HasPrefix(text, "resume:"+task)
}

// roundOfMutation reports the execution round one mutation identity encodes, if
// it encodes one: a settle names the round whose terminal it records and a
// reactivation rollback names the round it puts back, because both stay
// consumed and the next re-entry opens the following round. An identity that
// carries no round (an archive settle with none, a handoff or a stop) reports none.
func roundOfMutation(taskID event.TaskID, mutation MutationID) (int64, bool) {
	text := string(mutation)
	for _, prefix := range []string{"settle:", rollbackPrefix} {
		head := prefix + string(taskID) + ":"
		if !strings.HasPrefix(text, head) {
			continue
		}
		round, err := strconv.ParseInt(strings.TrimPrefix(text, head), 10, 64)
		if err != nil || round < 1 {
			return 0, false
		}
		return round, true
	}
	return 0, false
}
