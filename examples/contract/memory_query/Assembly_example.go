package memoryqueryexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// assembledProjection is the whole read path of one projection wired end to end: the
// owner read port, the inventory that gathers through it and the projection over the
// frozen readings of that inventory.
func assembledProjection() *query.Projection {
	reads := &ownerReads{
		agents: []event.AgentID{"assistant", "intruder"},
		sites: []query.SiteReading{{
			Session: state.Session{
				Address:  event.Address{AgentID: "assistant", SessionID: "session-1"},
				Instance: "instance-1",
				Mailbox: []state.MailboxEntry{{
					Notice:    task.Notice{TaskID: "task-1", Round: 1, Kind: task.NoticeAccepted, Seq: 1},
					AcceptSeq: 1,
					Claim:     state.ClaimConsumed,
					Receipt:   state.ConsumeReceipt{Completed: true},
				}},
				Consumed: 1,
			},
			Held: true,
		}},
		tasks: []query.TaskReading{{
			TaskID: "task-1",
			Spec: task.Spec{
				Source:     event.Address{AgentID: "assistant", SessionID: "session-1"},
				InstanceID: "instance-1",
			},
			Status: task.StatusRunning,
		}},
	}
	return query.NewProjection(query.NewInventory(reads), 2)
}

// scopedFilter is the one filter the reads of an example run under: the scope of the one
// agent the caller already holds.
func scopedFilter() query.ListFilter {
	return query.ListFilter{Scope: query.AuthorizationScope{Agents: []event.AgentID{"assistant"}}}
}

// ExampleReading_assembly reads through the assembled path: one snapshot beside its
// cursor, one page of that revision, one session detail and one agent detail asked for by
// a reference that carries content fields. The session detail reads the mailbox of the
// one state.Session the site holds, the agent asked for beside a session answers with the
// canonical reference of that agent identity, and the entries answer in the stable
// identity order of the reading.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func ExampleReading_assembly() {
	ctx := context.Background()
	projection := assembledProjection()
	filter := scopedFilter()

	snapshot, cursor, err := projection.Snapshot(ctx, filter)
	if err != nil {
		return
	}
	page, err := projection.List(snapshot.Revision, cursor, filter, query.ObjectRef{}, 10)
	if err != nil || len(page) != len(snapshot.Details) {
		return
	}
	if len(snapshot.Watermarks) != 1 || snapshot.Watermarks[0].LagState != query.WatermarkLagUnknown {
		return
	}

	site := query.ObjectRef{
		Kind:       query.ObjectSession,
		Address:    event.Address{AgentID: "assistant", SessionID: "session-1"},
		InstanceID: "instance-1",
	}
	session, err := projection.Detail(snapshot.Revision, cursor, filter, site)
	if err != nil {
		return
	}
	if session.State.Session != (query.SessionState{Accepted: 1, Claimed: 1, Completed: 1, Consumed: 1}) {
		return
	}

	agent := query.ObjectRef{
		Kind:    query.ObjectAgent,
		Address: event.Address{AgentID: "assistant", SessionID: "session-1"},
	}
	sameAgent, err := projection.Detail(snapshot.Revision, cursor, filter, agent)
	if err != nil || sameAgent.Ref.Address.SessionID != "" {
		return
	}

	rank := map[query.ObjectKind]int{
		query.ObjectAgent:      0,
		query.ObjectSession:    1,
		query.ObjectInvocation: 2,
		query.ObjectTask:       3,
		query.ObjectNotice:     4,
		query.ObjectLease:      5,
	}
	last := -1
	for _, entry := range snapshot.Details {
		position, ok := rank[entry.Ref.Kind]
		if !ok || position < last {
			return
		}
		last = position
	}
}
