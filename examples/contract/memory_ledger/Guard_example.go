package memoryledgerexample

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/event"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// leaseGuard is the canonical retention protection port: one hold per (original, holder),
// carried by the lease its release must bring back.
type leaseGuard struct {
	held map[string]ledger.ProtectionLease
	next int
}

func newLeaseGuard() *leaseGuard {
	return &leaseGuard{held: map[string]ledger.ProtectionLease{}}
}

func holdKey(ref ledger.OriginalRef, holder ledger.HolderID) string {
	return string(ref.Key) + "\x00" + string(holder)
}

// Hold registers one protection hold on the original of ref for holder and returns the
// lease its release must carry back.
func (g *leaseGuard) Hold(_ context.Context, ref ledger.OriginalRef, holder ledger.HolderID) (ledger.ProtectionLease, error) {
	if holder == "" {
		return "", ledger.ErrHolderRequired
	}
	key := holdKey(ref, holder)
	if lease, ok := g.held[key]; ok {
		return lease, nil
	}
	g.next++
	lease := ledger.ProtectionLease(fmt.Sprintf("lease-%d", g.next))
	g.held[key] = lease
	return lease, nil
}

// Release drops the hold of holder on the original of ref under the lease that hold
// returned, and refuses a lease that is not the hold's current one.
func (g *leaseGuard) Release(_ context.Context, ref ledger.OriginalRef, holder ledger.HolderID, lease ledger.ProtectionLease) error {
	key := holdKey(ref, holder)
	current, ok := g.held[key]
	if !ok {
		return nil
	}
	if current != lease {
		return ledger.ErrLeaseMismatch
	}
	delete(g.held, key)
	return nil
}

// Protected reports whether the original of ref is still held.
func (g *leaseGuard) Protected(_ context.Context, ref ledger.OriginalRef) (bool, error) {
	prefix := string(ref.Key) + "\x00"
	for key := range g.held {
		if strings.HasPrefix(key, prefix) {
			return true, nil
		}
	}
	return false, nil
}

var _ ledger.Guard = (*leaseGuard)(nil)

// ExampleGuard holds one original, reports it as protected, releases it under its lease
// and reports that a foreign lease is refused while the hold still stands.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func ExampleGuard() {
	ctx := context.Background()
	guard := newLeaseGuard()
	ref := ledger.OriginalRef{
		Key:             "event-1",
		Owner:           event.Address{AgentID: "assistant", SessionID: "session-1"},
		Format:          "text",
		RetentionPolicy: "durable",
	}

	lease, err := guard.Hold(ctx, ref, "reclaimer")
	if err != nil {
		return
	}
	if _, err := guard.Hold(ctx, ref, ""); !errors.Is(err, ledger.ErrHolderRequired) {
		return
	}
	protected, err := guard.Protected(ctx, ref)
	if err != nil || !protected {
		return
	}
	if err := guard.Release(ctx, ref, "reclaimer", ledger.ProtectionLease("lease-other")); !errors.Is(err, ledger.ErrLeaseMismatch) {
		return
	}
	if err := guard.Release(ctx, ref, "reclaimer", lease); err != nil {
		return
	}
	if protected, err := guard.Protected(ctx, ref); err != nil || protected {
		return
	}
}
