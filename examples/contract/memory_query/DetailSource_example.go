package memoryqueryexample

import (
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/query"
)

// frozenDetails is the canonical frozen detail source: it fixes the identity order of one
// gathering and materializes the snapshot object of one identity at a time. Its entries
// are keyed by the identity a reference names, so every field beside that identity is
// content and takes no part in the lookup.
type frozenDetails struct {
	order   []query.ObjectRef
	details map[string]query.Detail
}

// identityOf reports the identity one reference names: its kind beside the identity
// fields of that kind, and nothing else.
func identityOf(ref query.ObjectRef) string {
	switch ref.Kind {
	case query.ObjectAgent:
		return "agent/" + string(ref.Address.AgentID)
	case query.ObjectSession:
		return fmt.Sprintf("session/%s/%s/%s", ref.Address.AgentID, ref.Address.SessionID, ref.InstanceID)
	case query.ObjectInvocation:
		return "invocation/" + string(ref.InvocationID)
	case query.ObjectTask:
		return "task/" + string(ref.TaskID)
	case query.ObjectNotice:
		return fmt.Sprintf("notice/%s/%d/%s/%d", ref.Notice.TaskID, ref.Notice.Round, ref.Notice.Kind, ref.Notice.Seq)
	case query.ObjectLease:
		return fmt.Sprintf("lease/%s/%s", ref.Lease.Kind, ref.Lease.Key)
	}
	return string(ref.Kind)
}

// freezeDetails fixes one identity order into its frozen entries: one detail per object
// identity, holding the canonical reference of that identity.
func freezeDetails(order []query.ObjectRef) frozenDetails {
	details := make(map[string]query.Detail, len(order))
	for _, ref := range order {
		details[identityOf(ref)] = query.Detail{Ref: ref}
	}
	return frozenDetails{order: order, details: details}
}

// Order returns the stable identity order of every object the frozen reading holds.
func (d frozenDetails) Order() []query.ObjectRef {
	return append([]query.ObjectRef(nil), d.order...)
}

// DetailOf materializes the snapshot object of one identity and reports whether the
// frozen reading holds that identity at all.
func (d frozenDetails) DetailOf(ref query.ObjectRef) (query.Detail, bool) {
	detail, ok := d.details[identityOf(ref)]
	if !ok {
		return query.Detail{}, false
	}
	return detail, true
}

var _ query.DetailSource = frozenDetails{}

// ExampleDetailSource materializes one page of a frozen reading and reports an identity
// outside that reading as not held instead of serving an empty detail; a reference
// carrying content fields beside its identity answers with the same entry.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func ExampleDetailSource() {
	held := query.ObjectRef{Kind: query.ObjectTask, TaskID: "task-1"}
	missing := query.ObjectRef{Kind: query.ObjectTask, TaskID: "task-2"}
	source := freezeDetails([]query.ObjectRef{held})

	if len(source.Order()) != 1 {
		return
	}
	detail, ok := source.DetailOf(held)
	if !ok {
		return
	}
	if _, ok := source.DetailOf(missing); ok {
		return
	}

	sameHeld := query.ObjectRef{
		Kind:    query.ObjectTask,
		Address: event.Address{AgentID: "assistant", SessionID: "session-1"},
		TaskID:  "task-1",
	}
	if same, ok := source.DetailOf(sameHeld); !ok || !same.Ref.Address.IsZero() {
		return
	}
	_ = detail.Ref
}
