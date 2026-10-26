package stateexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// generationHolder is the canonical per-request borrowing face: it hands out the generation
// effective when the request begins and keeps every generation it published.
type generationHolder struct {
	instance   event.InstanceID
	generation uint64
	bindings   map[event.AgentID]state.AgentBinding
	released   map[uint64]struct{}
}

// Borrow returns a handle to the binding effective for one agent at the moment the request
// begins executing.
func (h *generationHolder) Borrow(_ context.Context, agentID event.AgentID) (*state.BindingHandle, error) {
	binding, ok := h.bindings[agentID]
	if !ok {
		return nil, state.ErrBindingRefUnknown
	}
	return &state.BindingHandle{
		Binding: binding,
		Ref:     state.BindingRef{AgentID: agentID, InstanceID: h.instance, Generation: h.generation},
	}, nil
}

// BorrowDerived returns a handle to the binding of the generation the reference's borrow
// took, and refuses a reference this holder never issued.
func (h *generationHolder) BorrowDerived(_ context.Context, ref state.BindingRef) (*state.BindingHandle, error) {
	if ref.InstanceID != h.instance {
		return nil, state.ErrBindingRefUnknown
	}
	binding, ok := h.bindings[ref.AgentID]
	if !ok {
		return nil, state.ErrBindingRefUnknown
	}
	if ref.Generation != h.generation {
		if _, released := h.released[ref.Generation]; released {
			return nil, state.ErrBindingGenerationReleased
		}
		return nil, state.ErrBindingRefUnknown
	}
	return &state.BindingHandle{Binding: binding, Ref: ref}, nil
}

var _ state.Borrow = (*generationHolder)(nil)

// ExampleBorrow borrows the generation in effect for one agent, serves a delegated request
// that same generation and reports a reference minted by another holder as unknown.
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func ExampleBorrow() {
	ctx := context.Background()
	holder := &generationHolder{
		instance:   "instance-1",
		generation: 3,
		bindings:   map[event.AgentID]state.AgentBinding{"assistant": {}},
		released:   map[uint64]struct{}{2: {}},
	}

	handle, err := holder.Borrow(ctx, "assistant")
	if err != nil {
		return
	}
	derived, err := holder.BorrowDerived(ctx, handle.Ref)
	if err != nil || derived.Ref.Generation != handle.Ref.Generation {
		return
	}
	foreign := state.BindingRef{AgentID: "assistant", InstanceID: "instance-other", Generation: 3}
	if _, err := holder.BorrowDerived(ctx, foreign); err != state.ErrBindingRefUnknown {
		return
	}
	stale := state.BindingRef{AgentID: "assistant", InstanceID: "instance-1", Generation: 2}
	if _, err := holder.BorrowDerived(ctx, stale); err != state.ErrBindingGenerationReleased {
		return
	}
}
