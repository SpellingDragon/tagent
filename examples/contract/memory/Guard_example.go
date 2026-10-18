package memoryexample

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// leaseGuard is the canonical retention protection port: one hold per (original, holder),
// carried by the lease its release must bring back.
type leaseGuard struct {
	held map[string]memory.ProtectionLease
	next int
}

func newLeaseGuard() *leaseGuard {
	return &leaseGuard{held: map[string]memory.ProtectionLease{}}
}

func holdKey(ref memory.OriginalRef, holder memory.HolderID) string {
	return string(ref.Key) + "\x00" + string(holder)
}

// Hold registers one protection hold on the original of ref for holder and returns the
// lease its release must carry back.
func (g *leaseGuard) Hold(_ context.Context, ref memory.OriginalRef, holder memory.HolderID) (memory.ProtectionLease, error) {
	if holder == "" {
		return "", memory.ErrHolderRequired
	}
	key := holdKey(ref, holder)
	if lease, ok := g.held[key]; ok {
		return lease, nil
	}
	g.next++
	lease := memory.ProtectionLease(fmt.Sprintf("lease-%d", g.next))
	g.held[key] = lease
	return lease, nil
}

// Release drops the hold of holder on the original of ref under the lease that hold
// returned, and refuses a lease that is not the hold's current one.
func (g *leaseGuard) Release(_ context.Context, ref memory.OriginalRef, holder memory.HolderID, lease memory.ProtectionLease) error {
	key := holdKey(ref, holder)
	current, ok := g.held[key]
	if !ok {
		return nil
	}
	if current != lease {
		return memory.ErrLeaseMismatch
	}
	delete(g.held, key)
	return nil
}

// Protected reports whether the original of ref is still held.
func (g *leaseGuard) Protected(_ context.Context, ref memory.OriginalRef) (bool, error) {
	prefix := string(ref.Key) + "\x00"
	for key := range g.held {
		if strings.HasPrefix(key, prefix) {
			return true, nil
		}
	}
	return false, nil
}

var _ memory.Guard = (*leaseGuard)(nil)

// ExampleGuard holds one original, reports it as protected, releases it under its lease
// and reports that a foreign lease is refused while the hold still stands.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func ExampleGuard() {
	ctx := context.Background()
	guard := newLeaseGuard()
	ref := memory.OriginalRef{
		Key:             "event-1",
		Owner:           event.Address{AgentID: "assistant", SessionID: "session-1"},
		Format:          "text",
		RetentionPolicy: "durable",
	}

	lease, err := guard.Hold(ctx, ref, "reclaimer")
	if err != nil {
		return
	}
	if _, err := guard.Hold(ctx, ref, ""); !errors.Is(err, memory.ErrHolderRequired) {
		return
	}
	protected, err := guard.Protected(ctx, ref)
	if err != nil || !protected {
		return
	}
	if err := guard.Release(ctx, ref, "reclaimer", memory.ProtectionLease("lease-other")); !errors.Is(err, memory.ErrLeaseMismatch) {
		return
	}
	if err := guard.Release(ctx, ref, "reclaimer", lease); err != nil {
		return
	}
	if protected, err := guard.Protected(ctx, ref); err != nil || protected {
		return
	}
}
