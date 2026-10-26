package agentexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// landedCalls is the canonical close ledger: it books one fresh admission, lands that
// call's terminal once the obligations it waits on cleared, and reports the bookings it
// still holds.
type landedCalls struct {
	booked map[event.TaskID]task.Status
}

func newLandedCalls() *landedCalls {
	return &landedCalls{booked: map[event.TaskID]task.Status{}}
}

// Reconcile lands the terminal of one admitted call when its obligations cleared and
// reports the status the record stands at afterwards.
func (c *landedCalls) Reconcile(_ context.Context, taskID event.TaskID) (task.Status, error) {
	status, ok := c.booked[taskID]
	if !ok {
		return task.StatusUnknown, task.ErrTaskNotFound
	}
	if status == task.StatusSettled || status == task.StatusFailed || status == task.StatusCancelled {
		delete(c.booked, taskID)
		return status, nil
	}
	return status, nil
}

// Pending reports how many fresh admissions the ledger still books.
func (c *landedCalls) Pending() int { return len(c.booked) }

var _ agent.Reconciler = newLandedCalls()

// ExampleReconciler books one admitted call, lands its terminal and reports that no
// booking stays open once the landing ran.
//
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func ExampleReconciler() {
	ctx := context.Background()
	ledger := newLandedCalls()
	ledger.booked["task-1"] = task.StatusSettled

	status, err := ledger.Reconcile(ctx, "task-1")
	if err != nil {
		return
	}
	pending := ledger.Pending()
	_ = status
	if pending != 0 {
		return
	}
}
