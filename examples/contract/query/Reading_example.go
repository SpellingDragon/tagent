package queryexample

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// ownerReads is the canonical single read port: it reads the binding generation in
// effect, the session sites the state owner holds and the tasks the task owner holds, one
// semantic unit per method.
type ownerReads struct {
	agents []event.AgentID
	leases []state.LeaseRef
	sites  []query.SiteReading
	tasks  []query.TaskReading
}

// Published reads the binding generation currently in effect.
func (r *ownerReads) Published(context.Context) (query.Generation, error) {
	return query.Generation{
		Agents: append([]event.AgentID(nil), r.agents...),
		Leases: append([]state.LeaseRef(nil), r.leases...),
	}, nil
}

// Sites reads every session site the state owner holds.
func (r *ownerReads) Sites(context.Context) ([]query.SiteReading, error) {
	return append([]query.SiteReading(nil), r.sites...), nil
}

// Tasks reads every task the task owner holds.
func (r *ownerReads) Tasks(context.Context) ([]query.TaskReading, error) {
	return append([]query.TaskReading(nil), r.tasks...), nil
}

var _ query.Reading = (*ownerReads)(nil)

// ExampleReading gathers one reading of the three semantic units: the generation in
// effect, the session sites it serves and the tasks the task owner holds.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func ExampleReading() {
	ctx := context.Background()
	reads := &ownerReads{
		agents: []event.AgentID{"assistant"},
		leases: []state.LeaseRef{{Kind: "tmux", Key: "session-1"}},
		sites: []query.SiteReading{
			{
				Session: state.Session{Address: event.Address{AgentID: "assistant", SessionID: "session-1"}},
				Held:    true,
			},
		},
		tasks: []query.TaskReading{
			{TaskID: "task-1", Status: task.StatusRunning},
		},
	}

	published, err := reads.Published(ctx)
	if err != nil {
		return
	}
	sites, err := reads.Sites(ctx)
	if err != nil {
		return
	}
	tasks, err := reads.Tasks(ctx)
	if err != nil {
		return
	}
	_ = len(published.Agents)
	_ = len(sites)
	_ = len(tasks)
}
