package taskexample

import (
	"context"

	"github.com/SpellingDragon/tagent/runtime/task"
)

// unsettledAccount is the canonical rest account: it answers the work it holds
// with no terminal, the notifications it still owes a delivery for and the
// notifications a mailbox accepted with nobody consuming them, each from the
// index that owns it, beside the commit sequence it stands at.
type unsettledAccount struct {
	work       []task.UnsettledWork
	owed       []task.UnsettledNotice
	unconsumed []task.UnsettledNotice
	seq        uint64
}

// Unfinished reports the tasks this account holds with no terminal.
func (a *unsettledAccount) Unfinished(context.Context) ([]task.UnsettledWork, error) {
	return a.work, nil
}

// Owed reports the notifications the tasks of this account still owe a delivery
// for.
func (a *unsettledAccount) Owed(context.Context) ([]task.UnsettledNotice, error) {
	return a.owed, nil
}

// Unconsumed reports the notifications a mailbox accepted and no consumption
// confirmed.
func (a *unsettledAccount) Unconsumed(context.Context) ([]task.UnsettledNotice, error) {
	return a.unconsumed, nil
}

var _ task.UnsettledSource = (*unsettledAccount)(nil)

// at reports the commit sequence this account stands at, so the sets of one read
// belong to one instant.
func (a *unsettledAccount) at(context.Context) uint64 { return a.seq }

// ExampleUnsettledSource reads one rest criterion at the commit sequence the
// account stands at: the account answers at rest while it holds nothing open and
// names the one task it still holds once the work is admitted.
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func ExampleUnsettledSource() {
	ctx := context.Background()
	account := &unsettledAccount{seq: 1}
	if !task.ReadUnsettled(ctx, account, account.at).Resting() {
		return
	}
	account.work = []task.UnsettledWork{{TaskID: "task-1", Status: task.StatusRunning}}
	read := task.ReadUnsettled(ctx, account, account.at)
	if read.Resting() || len(read.Work) != 1 {
		return
	}
}
