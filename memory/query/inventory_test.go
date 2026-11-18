package query

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// taskRefusalReading is a read port whose task source refuses with one named cause while the generation and site sources answer.
type taskRefusalReading struct {
	cause error
}

// Published answers one configured agent and no lease.
func (taskRefusalReading) Published(context.Context) (Generation, error) {
	return Generation{Agents: []event.AgentID{"agent-a"}}, nil
}

// Sites answers no site.
func (taskRefusalReading) Sites(context.Context) ([]SiteReading, error) {
	return nil, nil
}

// Tasks refuses with the cause the reading carries.
func (r taskRefusalReading) Tasks(context.Context) ([]TaskReading, error) {
	return nil, r.cause
}

// TestInventoryTaskSourceRefusalNamesTheDimensionsItFeeds pins the named refusal of a task source failure against the empty-detail degradation.
//   - the task, invocation and notice dimensions refuse with a SourceError carrying the source cause, since the task reading feeds all three
//   - the agent, session and lease dimensions answer their own readings untouched by the task refusal
//   - the total view refuses by name instead of freezing a partial reading as a normal one
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestInventoryTaskSourceRefusalNamesTheDimensionsItFeeds(t *testing.T) {
	inv := NewInventory(taskRefusalReading{cause: task.ErrSpecMissing})
	ctx := context.Background()
	readers := map[ObjectKind]func() error{
		ObjectTask:       func() error { _, err := inv.Tasks(ctx); return err },
		ObjectInvocation: func() error { _, err := inv.Invocations(ctx); return err },
		ObjectNotice:     func() error { _, err := inv.Notices(ctx); return err },
	}
	for _, dim := range []ObjectKind{ObjectTask, ObjectInvocation, ObjectNotice} {
		err := readers[dim]()
		var source *SourceError
		if !errors.As(err, &source) || source.Dimension != dim {
			t.Fatalf("%s refusal = %v, want a SourceError naming the dimension", dim, err)
		}
		if !errors.Is(err, task.ErrSpecMissing) || !errors.Is(err, ErrSourceUnreadable) {
			t.Fatalf("%s refusal = %v, want the source cause under the unreadable-source class", dim, err)
		}
	}
	agents, err := inv.Agents(ctx)
	if err != nil || len(agents) != 1 || !agents[0].Configured {
		t.Fatalf("agent reading = %+v, %v, want the configured agent answered beside the task refusal", agents, err)
	}
	if sessions, err := inv.Sessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("session reading = %+v, %v, want the empty site reading answered", sessions, err)
	}
	if leases, err := inv.Leases(ctx); err != nil || len(leases) != 0 {
		t.Fatalf("lease reading = %+v, %v, want the empty lease reading answered", leases, err)
	}
	if _, err := inv.View(ctx); !errors.Is(err, task.ErrSpecMissing) || !errors.Is(err, ErrSourceUnreadable) {
		t.Fatalf("total view refusal = %v, want the named refusal instead of a partial view", err)
	}
}
