// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// This file holds the one criterion that answers whether a run is at rest: the
// work it accepted and has not finished, beside the notifications no consumption
// confirmed. The close, the restart and the report read this criterion and no
// other, so the two accounts of a run — the task chain and the consumption
// ledger — are judged once.
//
// The criterion is a read: it moves no status, clears no obligation and delivers
// nothing. Every set it unions is read through the account that owns it, and the
// commit sequence of that account fences the read so the sets belong to one
// instant.
package task

import (
	"context"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
)

// UnsettledWork is one accepted task that has not reached a terminal: the work a
// run still owes an outcome for.
type UnsettledWork struct {
	// TaskID is the task the work runs under.
	TaskID event.TaskID
	// Status is the status the machine holds for it.
	Status Status
	// Owner is the invocation the work was admitted under.
	Owner event.InvocationID
	// Address is the address the work's notifications return to.
	Address event.Address
}

// UnsettledNotice is one notification no consumption confirmed: a notification
// the task still owes a delivery for, or one accepted into a mailbox that was
// never consumed.
type UnsettledNotice struct {
	// Notice is the stable identity of the notification.
	Notice Notice
	// Address is the address the notification belongs to: the target of a notice
	// a task still owes, or the site that accepted it into a mailbox.
	Address event.Address
	// Instance is the runtime instance that holds that address.
	Instance event.InstanceID
	// Source is the address the notification came from.
	Source event.Address
	// Reason is the readable reason the notification stands unconsumed.
	Reason string
}

// UnsettledInput is one input round a session chain holds with no answer: a
// driving fact the chain committed whose round no answer closed yet. It is the
// fourth account of the criterion: the work a run accepted as input and has not
// consumed, read off the chain rather than off a ledger kept beside it.
type UnsettledInput struct {
	// Address is the address of the site the input was committed at.
	Address event.Address
	// Instance is the runtime instance that owns that site.
	Instance event.InstanceID
	// Event is the identity of the driving fact the round opened on.
	Event event.EventID
	// Source is the address the input came from.
	Source event.Address
	// Reason is the readable reason the round stands unanswered.
	Reason string
}

// Unsettled is one evaluation of the rest criterion: the unsettled work, the
// unconsumed notices, the input rounds no answer closed, and the commit
// sequence the sets were read at.
//
// Unjudged is the named reason the criterion could not be read: an account that
// refused the read, or a commit sequence that moved under every attempt, leaves
// the run unjudged rather than rested.
type Unsettled struct {
	// Seq is the commit sequence the sets were read at.
	Seq uint64
	// Work lists the accepted work with no terminal, in task identity order.
	Work []UnsettledWork
	// Notices lists the notifications no consumption confirmed, in identity
	// order.
	Notices []UnsettledNotice
	// Inputs lists the input rounds the session chains hold with no answer, in
	// address and commit order.
	Inputs []UnsettledInput
	// Unjudged is the named reason the criterion was not judged, and empty when
	// it was.
	Unjudged string
}

// Resting reports whether a run is at rest: nothing it accepted is unfinished, no
// notification of it stands unconsumed, and no input round it holds stands
// unanswered. A criterion that could not be read is never at rest.
func (u Unsettled) Resting() bool {
	return u.Unjudged == "" && len(u.Work) == 0 && len(u.Notices) == 0 && len(u.Inputs) == 0
}

// UnsettledSource is the authoritative account the rest criterion reads: the
// task chain that answers the work it holds, the consumption ledger that answers
// the notifications no consumption confirmed — those a task still owes a
// delivery for and those a mailbox accepted with nobody consuming them — and the
// session chains that answer the input rounds they hold with no answer.
//
// The redline list (3 rules):
// MUST answer each set from the account that owns it, never from a copy kept beside it.
// MUST answer a set without a second ledger of its own: the commit sequence that fences a read is handed to ReadUnsettled by the account, not held here.
// MUST NOT answer a set from an in-process shadow ledger the chain cannot compute back.
type UnsettledSource interface {
	// Unfinished reports the tasks accepted and not terminal.
	Unfinished(ctx context.Context) ([]UnsettledWork, error)
	// Unconsumed reports the notifications no consumption confirmed, from the two
	// accounts that own them: the notifications a task still owes a delivery for
	// and the ones a mailbox accepted and nobody consumed. One notification may
	// stand in both, and the criterion states it once.
	Unconsumed(ctx context.Context) ([]UnsettledNotice, error)
	// Unanswered reports the input rounds a session chain holds with no answer:
	// the driving facts that stand past the round the chain last answered.
	Unanswered(ctx context.Context) ([]UnsettledInput, error)
}

// UnsettledFence reports the commit sequence of the account a read stands at, so
// the sets of one evaluation belong to one instant. A nil fence reads the sets
// once and answers them as the account stands.
type UnsettledFence func(ctx context.Context) uint64

// unsettledFenceAttempts bounds how often the criterion re-reads a commit
// sequence that moved under it: a read that keeps moving answers unjudged
// instead of reporting sets that belong to different instants.
const unsettledFenceAttempts = 3

// ReadUnsettled evaluates the rest criterion under the commit sequence fence: the
// sequence is read before the sets and again after them, and only a sequence that
// held answers the sets as one snapshot of the account. A sequence that moved is
// re-read, and one that never holds leaves the run unjudged by name. A read beside
// no fence evaluates the sets once, for an account whose own reads are already one
// snapshot.
func ReadUnsettled(ctx context.Context, source UnsettledSource, fence UnsettledFence) Unsettled {
	if fence == nil {
		last, err := readSets(ctx, source, 0)
		if err != nil {
			return Unsettled{Unjudged: err.Error()}
		}
		return last
	}
	var last Unsettled
	for attempt := 0; attempt < unsettledFenceAttempts; attempt++ {
		before := fence(ctx)
		read, err := readSets(ctx, source, before)
		if err != nil {
			return Unsettled{Seq: before, Unjudged: err.Error()}
		}
		last = read
		last.Seq = fence(ctx)
		if before == last.Seq {
			return last
		}
	}
	last.Unjudged = reasonCommitMovedUnderRead
	last.Seq = fence(ctx)
	return last
}

// readSets reads every set of one evaluation at the sequence the caller read
// before them.
func readSets(ctx context.Context, source UnsettledSource, seq uint64) (Unsettled, error) {
	work, err := source.Unfinished(ctx)
	if err != nil {
		return Unsettled{}, err
	}
	unconsumed, err := source.Unconsumed(ctx)
	if err != nil {
		return Unsettled{}, err
	}
	inputs, err := source.Unanswered(ctx)
	if err != nil {
		return Unsettled{}, err
	}
	return Unsettled{Seq: seq, Work: work, Notices: mergeNotices(unconsumed), Inputs: inputs}, nil
}

// reasonCommitMovedUnderRead is the named reason a criterion answers when the
// commit sequence of its account moved under every read attempt.
const reasonCommitMovedUnderRead = "the commit sequence of the account moved under the read"

// mergeNotices unions the notifications a task still owes with the ones a mailbox
// holds unconsumed, each notification once: a notification that stands in both
// accounts is one entry, carrying the reason it is unconsumed in.
func mergeNotices(sets ...[]UnsettledNotice) []UnsettledNotice {
	merged := make([]UnsettledNotice, 0, len(sets[0]))
	seen := make(map[string]int, len(sets[0]))
	for _, set := range sets {
		for _, notice := range set {
			key := notice.Notice.FactKey()
			if at, held := seen[key]; held {
				if merged[at].Reason == "" {
					merged[at].Reason = notice.Reason
				}
				if !merged[at].Address.IsComplete() {
					merged[at].Address, merged[at].Instance = notice.Address, notice.Instance
				}
				continue
			}
			seen[key] = len(merged)
			merged = append(merged, notice)
		}
	}
	sortNotices(merged)
	return merged
}

// sortNotices orders notifications by the site they await, then by their stable
// identity, so one account always answers in the same order.
func sortNotices(notices []UnsettledNotice) {
	sort.Slice(notices, func(i, j int) bool {
		left, right := notices[i], notices[j]
		if left.Address.AgentID != right.Address.AgentID {
			return left.Address.AgentID < right.Address.AgentID
		}
		if left.Address.SessionID != right.Address.SessionID {
			return left.Address.SessionID < right.Address.SessionID
		}
		return left.Notice.FactKey() < right.Notice.FactKey()
	})
}

// Unfinished reports the work this registry holds with no terminal, read from the
// index it keeps per task identity: a task that reached a terminal fact is
// finished, and a task whose status is not determinable stays listed because its
// outcome was never read.
func (r *MemoryRegistry) Unfinished(ctx context.Context) ([]UnsettledWork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	work := make([]UnsettledWork, 0, len(r.tasks))
	for id, entry := range r.tasks {
		if isTerminal(entry.status) {
			continue
		}
		work = append(work, UnsettledWork{
			TaskID:  id,
			Status:  entry.status,
			Owner:   entry.spec.InvocationID,
			Address: entry.spec.Target,
		})
	}
	sort.Slice(work, func(i, j int) bool { return work[i].TaskID < work[j].TaskID })
	return work, nil
}

// Owed reports the notifications the tasks of this manager still owe a delivery
// for, read from the obligation index it keeps per task: a notification a target
// explicitly accepted is settled, and one whose delivery is still open is owed
// whatever the transport did with it.
func (m *MemoryManager) Owed(ctx context.Context) ([]UnsettledNotice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	var owed []UnsettledNotice
	for _, entry := range m.registry.tasks {
		for _, held := range entry.obligations {
			if !owesDelivery(held.obligation.Kind) {
				continue
			}
			reason := reasonNoticeUnconsumed
			for _, blocked := range entry.blocked {
				if blocked.Notice == held.obligation.Notice {
					reason = blocked.Reason
					break
				}
			}
			owed = append(owed, UnsettledNotice{
				Notice:   held.obligation.Notice,
				Address:  held.envelope.Target,
				Instance: held.envelope.TargetInstance,
				Source:   held.envelope.Source,
				Reason:   reason,
			})
		}
	}
	sortNotices(owed)
	return owed, nil
}

// reasonNoticeUnconsumed names the wait a notification stands in while no target
// has accepted its delivery.
const reasonNoticeUnconsumed = "no target has accepted the delivery of this notification"
