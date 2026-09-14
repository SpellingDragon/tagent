package rl

import (
	"context"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// closeableModel counts Close calls (implementation-hardening 5.2 test).
type closeableModel struct {
	closed int
}

func (m *closeableModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	close(ch)
	return ch, nil
}
func (m *closeableModel) Info() model.Info { return model.Info{Name: "closeable"} }
func (m *closeableModel) Close() error     { m.closed++; return nil }

// plainModel has no Close — the recycle sweep must tolerate it.
type plainModel struct{}

func (plainModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	close(ch)
	return ch, nil
}
func (plainModel) Info() model.Info { return model.Info{Name: "plain"} }

// TestSwappableModel_OldModelRecycledAfterSwap: swapped-out closeable models
// are Closed once no in-flight call remains; Close-less models are dropped
// silently; repeated sweeps are idempotent.
func TestSwappableModel_OldModelRecycledAfterSwap(t *testing.T) {
	first := &closeableModel{}
	sm := NewSwappableModel(first)

	sm.Swap(plainModel{}) // first → retired, in-flight 0 → closed on sweep
	if first.closed != 1 {
		t.Fatalf("first.closed = %d, want 1 after swap with no in-flight", first.closed)
	}
	second := &closeableModel{}
	sm.Swap(second)
	if second.closed != 0 {
		t.Fatal("current model must never be closed by Swap")
	}
	// Swap to nil-inner replacement guard: swapping the same instance keeps
	// semantics (old==new still retired once).
	sm.Swap(second)
	if second.closed != 0 {
		t.Fatalf("current model closed = %d, want 0", second.closed)
	}
}

// TestSwappableModel_InFlightGatesRecycle: a swapped-out model is not closed
// while a GenerateContent call that captured it is still running.
func TestSwappableModel_InFlightGatesRecycle(t *testing.T) {
	first := &closeableModel{}
	sm := NewSwappableModel(first)

	sm.inFlight.Store(1) // simulate a call that captured `first`
	sm.Swap(plainModel{})
	if first.closed != 0 {
		t.Fatalf("retired model closed during in-flight call: %d", first.closed)
	}
	sm.inFlight.Store(0)
	sm.sweepRetired()
	if first.closed != 1 {
		t.Fatalf("retired model closed = %d, want 1 after quiescence", first.closed)
	}
}
