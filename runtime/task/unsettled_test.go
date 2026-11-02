package task

import (
	"context"
	"errors"
	"testing"
)

// TestUnsettledSingleSource pins the one criterion the close, the restart and the report read.
//
// - the criterion unions the accepted work with no terminal and the notifications no consumption confirmed
// - a notification that stands in both authoritative accounts is one entry, beside the routing it froze
// - the read is fenced by the commit sequence: a sequence that moved is re-read, and one that never holds answers unjudged
// - no second criterion is consulted: a shadow account reporting open obligations leaves a rested run rested
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestUnsettledSingleSource(t *testing.T) {
	ctx := context.Background()

	t.Run("the criterion unions unfinished work and unconsumed notices", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		source := &scriptedUnsettled{registry: registry, manager: NewMemoryManager(registry, nil)}

		read := ReadUnsettled(ctx, source, source.CommitSeq)
		if read.Resting() {
			t.Fatalf("criterion over one fresh task = %+v, want the admitted work listed", read)
		}
		if len(read.Work) != 1 || read.Work[0].TaskID != taskID || read.Work[0].Status != StatusAccepted {
			t.Fatalf("unfinished work = %+v, want the one admitted task", read.Work)
		}
		if read.Unjudged != "" {
			t.Fatalf("criterion unjudged by %q, want a judged read", read.Unjudged)
		}

		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
		}
		read = ReadUnsettled(ctx, source, source.CommitSeq)
		if len(read.Work) != 0 {
			t.Fatalf("unfinished work after the terminal = %+v, want the finished task gone", read.Work)
		}
		if len(read.Notices) != 1 {
			t.Fatalf("unconsumed notices = %+v, want the settled notification nobody accepted", read.Notices)
		}
		notice := read.Notices[0]
		if notice.Address != invariantTaskAddress || notice.Instance != "instance-a" || notice.Source != invariantTaskAddress {
			t.Fatalf("notice site = %+v, want the routing the declaration froze", notice)
		}
		if notice.Reason == "" {
			t.Fatal("the unconsumed notice carries no reason, want the wait it stands in")
		}
		if read.Resting() {
			t.Fatalf("criterion over an unaccepted notification = %+v, want the run kept from rest", read)
		}
	})

	t.Run("one notification standing in both accounts is one entry", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
		}
		envelope := invariantEnvelope(taskID, NoticeSettled, 0)
		source := &scriptedUnsettled{
			registry: registry,
			manager:  NewMemoryManager(registry, nil),
			mailbox: []UnsettledNotice{{
				Notice:   envelope.Notice,
				Address:  envelope.Target,
				Instance: envelope.TargetInstance,
				Source:   envelope.Source,
				Reason:   "the mailbox holds this notification unconsumed",
			}},
		}
		read := ReadUnsettled(ctx, source, source.CommitSeq)
		if len(read.Notices) != 1 {
			t.Fatalf("unconsumed notices = %+v, want the one notification the two accounts name", read.Notices)
		}
		if got := read.Notices[0].Reason; got != reasonNoticeUnconsumed {
			t.Fatalf("notice reason = %q, want the account that owes the delivery to name it", got)
		}
	})

	t.Run("the read is fenced by the commit sequence", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		source := &scriptedUnsettled{registry: registry, manager: NewMemoryManager(registry, nil), moves: 1}
		read := ReadUnsettled(ctx, source, source.CommitSeq)
		if read.Unjudged != "" {
			t.Fatalf("criterion over a sequence that settled = %+v, want the read judged", read)
		}
		if got := source.attempts; got != 2 {
			t.Fatalf("attempts of the read = %d, want the read repeated until its sequence held", got)
		}
		source = &scriptedUnsettled{registry: registry, manager: NewMemoryManager(registry, nil), moves: unsettledFenceAttempts * 4}
		read = ReadUnsettled(ctx, source, source.CommitSeq)
		if read.Unjudged != reasonCommitMovedUnderRead {
			t.Fatalf("criterion over a sequence that never held = %+v, want the named unjudged reason", read)
		}
		if read.Resting() {
			t.Fatal("a criterion that could not be read reported the run at rest, want it unjudged")
		}
	})

	t.Run("an account refusing the read leaves the run unjudged", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		refused := errors.New("the account refused the read")
		source := &scriptedUnsettled{registry: registry, manager: NewMemoryManager(registry, nil), refuse: refused}
		read := ReadUnsettled(ctx, source, source.CommitSeq)
		if read.Unjudged != refused.Error() {
			t.Fatalf("criterion over a refusing account = %+v, want the refusal named", read)
		}
		if read.Resting() {
			t.Fatal("a refused read reported the run at rest, want it unjudged")
		}
	})

	t.Run("an input round the chain holds with no answer keeps the run from rest", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		source := &scriptedUnsettled{
			registry: registry,
			manager:  NewMemoryManager(registry, nil),
			pending: []UnsettledInput{{
				Address:  invariantTaskAddress,
				Instance: "instance-a",
				Event:    "4-abcd",
				Source:   invariantTaskAddress,
				Reason:   "the session chain holds this input round with no answer",
			}},
		}
		read := ReadUnsettled(ctx, source, source.CommitSeq)
		if len(read.Inputs) != 1 || read.Inputs[0].Event != "4-abcd" {
			t.Fatalf("unanswered input rounds = %+v, want the round the chain holds listed beside its driving fact", read.Inputs)
		}
		if read.Resting() {
			t.Fatalf("criterion over an unanswered input round = %+v, want the run kept from rest", read)
		}
	})

	t.Run("no_second_criterion", func(t *testing.T) {
		registry, _ := invariantRegistry(t)
		taskID := invariantRegister(t, registry, "call-1", "dedup-1")
		if result, err := registry.Advance(ctx, taskID, StatusSettled, "settled", nil); err != nil || result != CommitCommitted {
			t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
		}
		source := &scriptedUnsettled{
			registry: registry,
			manager:  NewMemoryManager(registry, nil),
			shadow:   []Obligation{{Kind: ObligationAwaitingDelivery, TaskID: taskID}},
		}
		read := ReadUnsettled(ctx, source, source.CommitSeq)
		if len(read.Work) != 0 || len(read.Notices) != 0 {
			t.Fatalf("criterion beside a shadow account = %+v, want the authoritative sets alone judged", read)
		}
		if !read.Resting() {
			t.Fatalf("criterion beside a shadow account that reports work = %+v, want the run at rest: no second account decides it", read)
		}
	})
}

// scriptedUnsettled is a rest criterion source over a real registry and manager,
// beside the scripted mailbox, fence and shadow accounts of one case.
type scriptedUnsettled struct {
	registry *MemoryRegistry
	manager  *MemoryManager
	// mailbox is the account the unconsumed set is read from.
	mailbox []UnsettledNotice
	// pending is the account the unanswered input rounds are read from.
	pending []UnsettledInput
	// moves is how many set reads the sequence moves under, so the fence sees the
	// account move under that many attempts. The case that never holds sets it
	// past one move per moving set of every attempt (three), so no attempt of the
	// three it takes finds the sequence settled.
	moves int
	// refuse is the refusal the unfinished read reports.
	refuse error
	// shadow is an account beside the authoritative ones, which the criterion
	// never reads.
	shadow []Obligation

	seq      uint64
	attempts int
}

// CommitSeq reports the commit sequence the account stands at.
func (s *scriptedUnsettled) CommitSeq(context.Context) uint64 {
	return s.seq
}

// moveSeq moves the commit sequence once while the case asks the fence to see a
// moving account.
func (s *scriptedUnsettled) moveSeq() {
	if s.moves <= 0 {
		return
	}
	s.moves--
	s.seq++
}

// Unfinished reports the work the registry holds with no terminal, or the refusal
// the case names.
func (s *scriptedUnsettled) Unfinished(ctx context.Context) ([]UnsettledWork, error) {
	s.attempts++
	if s.refuse != nil {
		return nil, s.refuse
	}
	work, err := s.registry.Unfinished(ctx)
	s.moveSeq()
	return work, err
}

// Unconsumed reports the notifications no consumption confirmed, from the task
// face the case drives and the mailbox it scripts.
func (s *scriptedUnsettled) Unconsumed(ctx context.Context) ([]UnsettledNotice, error) {
	s.moveSeq()
	owed, err := s.manager.Owed(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UnsettledNotice, 0, len(owed)+len(s.mailbox))
	out = append(out, owed...)
	out = append(out, s.mailbox...)
	return out, nil
}

// Unanswered reports the input rounds the case scripts.
func (s *scriptedUnsettled) Unanswered(context.Context) ([]UnsettledInput, error) {
	s.moveSeq()
	return s.pending, nil
}
