package queryexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// blockedProjection wires one task whose owner reports two blocked obligations:
// the projection carries each obligation as its own advancement entry, so a
// task waiting on an executor stop and on a storage action at once is explained
// by two entries and never by one shared advancer.
func blockedProjection() *query.Projection {
	reads := &ownerReads{
		agents: []event.AgentID{"assistant"},
		sites: []query.SiteReading{{
			Session: state.Session{
				Address:  event.Address{AgentID: "assistant", SessionID: "session-1"},
				Instance: "instance-1",
			},
		}},
		tasks: []query.TaskReading{{
			TaskID: "task-1",
			Spec: task.Spec{
				Source:     event.Address{AgentID: "assistant", SessionID: "session-1"},
				InstanceID: "instance-1",
			},
			Status: task.StatusRunning,
			Obligations: task.Obligations{
				Count: 2,
				Blocked: []task.BlockedObligation{
					{
						Notice: task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeAccepted, Seq: 1},
						Reason: "the named executor has not confirmed its stop",
					},
					{
						Notice: task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeSettled, Seq: 2},
						Reason: "the storage action is running",
					},
				},
			},
		}},
	}
	return query.NewProjection(query.NewInventory(reads), 2)
}

// ExampleDetail_blockedObligations reads the blocked list of one task detail
// through the real projection: each blocked obligation keeps its own reason and
// its own advancement entry, and the zero advance state is never read as
// settled.
//
// - It prints reason and state per blocked obligation: "the named executor has not confirmed its stop not-settled" and "the storage action is running not-settled".
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func ExampleDetail_blockedObligations() {
	ctx := context.Background()
	projection := blockedProjection()
	filter := scopedFilter()

	snapshot, cursor, err := projection.Snapshot(ctx, filter)
	if err != nil {
		return
	}

	ref := query.ObjectRef{
		Kind:       query.ObjectTask,
		Address:    event.Address{AgentID: "assistant", SessionID: "session-1"},
		InstanceID: "instance-1",
		TaskID:     "task-1",
	}
	detail, err := projection.Detail(snapshot.Revision, cursor, filter, ref)
	if err != nil {
		fmt.Println("detail:", err)
		return
	}
	for _, progress := range detail.Blocked {
		state := "not-settled"
		if progress.State == query.AdvanceSettled {
			state = "settled"
		}
		fmt.Println(progress.Obligation.Reason, state)
	}
}
