package state

import (
	"context"
	"errors"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
//
// retirement is the resource retirement ledger of the published generations:
// every published generation registers the candidate-exclusive resources its
// construction acquired beside the release that frees them, and a generation's
// resources are freed once that generation retires — superseded by a newer
// publication and holding no request any more.
//
// A publication retires nothing early: a generation with a request still under
// way keeps its resources until the last hold is released, and that release
// advances the drain on its own. The shared faces and the resources of a
// generation still in service stay out of every release this ledger runs.
type retirement struct {
	mu       sync.Mutex
	bindings *Bindings
	pending  map[Generation]*Leases
}

// newRetirement returns an empty retirement ledger over one binding holder.
func newRetirement(bindings *Bindings) *retirement {
	return &retirement{bindings: bindings, pending: map[Generation]*Leases{}}
}

// track registers the resources of one published generation beside the release
// that frees them once the generation retires.
func (r *retirement) track(gen Generation, resources *Leases) {
	if resources == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending[gen] = resources
}

// sweep frees the resources of every retired generation exactly once and joins
// the release failures it finds; a release that fails does not stop the
// releases beside it.
func (r *retirement) sweep(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	for gen, resources := range r.pending {
		if !r.retired(gen) {
			continue
		}
		delete(r.pending, gen)
		if resources == nil {
			continue
		}
		if err := resources.ReleaseAll(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// retired reports whether one generation has retired: superseded by a newer
// publication and released by the holder, or one the holder carries at all and
// can therefore never serve again.
func (r *retirement) retired(gen Generation) bool {
	if r.bindings == nil {
		return true
	}
	r.bindings.mu.Lock()
	defer r.bindings.mu.Unlock()
	held, ok := r.bindings.issued[uint64(gen)]
	return !ok || held.released
}

// Borrow implements Borrow and opens one request's use of the binding effective
// at the moment the call arrives, through this entry. The returned handle
// carries the retirement drain beside the borrow: releasing it ends the use and
// advances the retirement of the generation the request held.
func (p *Publisher) Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error) {
	if p.bindings == nil {
		return nil, ErrBindingHolderMissing
	}
	h, err := p.bindings.Borrow(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return p.drainable(h), nil
}

// BorrowDerived implements Borrow and opens a delegated request's use of the
// generation its initiating borrow took, through this entry and with the
// retirement drain beside it. A reference this holder never issued is refused
// by name, exactly as it is on the borrowing face.
func (p *Publisher) BorrowDerived(ctx context.Context, ref BindingRef) (*BindingHandle, error) {
	if p.bindings == nil {
		return nil, ErrBindingHolderMissing
	}
	h, err := p.bindings.BorrowDerived(ctx, ref)
	if err != nil {
		return nil, err
	}
	return p.drainable(h), nil
}

// drainable wraps one borrowed handle so the release of the request's hold
// carries the retirement drain: the hold ends once, and the retirement of the
// generation it held advances beside it.
func (p *Publisher) drainable(h *BindingHandle) *BindingHandle {
	return &BindingHandle{
		Binding: h.Binding,
		Ref:     h.Ref,
		release: func(ctx context.Context) error {
			return errors.Join(h.Release(ctx), p.retire.sweep(ctx))
		},
	}
}
