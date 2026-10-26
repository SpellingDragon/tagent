package taskexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// bindingOwners is the canonical owner resolution under the current orchestration binding: it
// reads the tool set the binding holds for the owner and refuses a target that set does not
// hold.
type bindingOwners struct {
	tools map[event.AgentID][]event.AgentID
}

// OwnerOf reports the owner that target belongs to under the current binding, within the
// tool set of the named owner.
func (b *bindingOwners) OwnerOf(_ context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error) {
	for _, held := range b.tools[owner] {
		if held == target {
			return owner, nil
		}
	}
	return "", task.ErrTargetRemoved
}

var _ task.OwnerBinding = (*bindingOwners)(nil)

// ExampleOwnerBinding resolves the owner of one restored target from the tool set the current
// binding holds, and reports a target that set does not hold by name.
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func ExampleOwnerBinding() {
	ctx := context.Background()
	owners := &bindingOwners{tools: map[event.AgentID][]event.AgentID{
		"assistant": {"worker"},
	}}

	owner, err := owners.OwnerOf(ctx, "assistant", "worker")
	if err != nil || owner != "assistant" {
		return
	}
	if _, err := owners.OwnerOf(ctx, "assistant", "stranger"); err != task.ErrTargetRemoved {
		return
	}
}
