package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/event"
)

// ErrHolderRequired reports a protection hold that names no holder, so its
// release could never be recognized as the same hold.
var ErrHolderRequired = errors.New("memory: protection hold names no holder")

// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// MemoryGuard is the in-memory retention protection: it keeps one hold per key
// and holder, hands each hold the lease its release must carry back, and
// reports to a reclaimer whether any holder still protects an original.
// Protection is counted across holders, so a hold is gone only when the last
// holder releases it.
type MemoryGuard struct {
	mu    sync.Mutex
	holds map[event.EventID]map[HolderID]ProtectionLease
	// issued counts the leases handed out, so every hold generation carries a
	// lease of its own and a lease from before a re-hold matches nothing.
	issued uint64
}

// NewMemoryGuard returns an empty in-memory retention protection.
func NewMemoryGuard() *MemoryGuard {
	return &MemoryGuard{holds: map[event.EventID]map[HolderID]ProtectionLease{}}
}

var _ Guard = (*MemoryGuard)(nil)

// Hold registers one protection hold on the original of ref for holder and
// returns the lease its release must carry back. Holding the same original
// again as the same holder while the hold stands stays one hold and hands back
// the lease it already holds, so one release by that holder clears it; a hold
// made after that release is a new hold under a fresh lease.
func (g *MemoryGuard) Hold(ctx context.Context, ref OriginalRef, holder HolderID) (ProtectionLease, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if ref.Key == "" {
		return "", ErrKeyRequired
	}
	if holder == "" {
		return "", ErrHolderRequired
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	holders, held := g.holds[ref.Key]
	if !held {
		holders = map[HolderID]ProtectionLease{}
		g.holds[ref.Key] = holders
	}
	if lease, standing := holders[holder]; standing {
		return lease, nil
	}
	g.issued++
	lease := ProtectionLease(fmt.Sprintf("lease-%d", g.issued))
	holders[holder] = lease
	return lease, nil
}

// Release drops the hold of holder on the original of ref under lease.
// Releasing a hold that has already been released is a no-op, and a release
// whose lease is not the current lease of the named hold is refused by name with
// ErrLeaseMismatch and leaves every hold standing: a late lease from before a
// re-hold, or a lease belonging to another hold, releases nothing.
func (g *MemoryGuard) Release(ctx context.Context, ref OriginalRef, holder HolderID, lease ProtectionLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref.Key == "" {
		return ErrKeyRequired
	}
	if holder == "" {
		return ErrHolderRequired
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	holders := g.holds[ref.Key]
	current, standing := holders[holder]
	if !standing {
		return nil
	}
	if current != lease {
		return ErrLeaseMismatch
	}
	delete(holders, holder)
	if len(holders) == 0 {
		delete(g.holds, ref.Key)
	}
	return nil
}

// Protected reports whether any holder still protects the original of ref, so a
// reclaimer asks before it reclaims anything.
func (g *MemoryGuard) Protected(ctx context.Context, ref OriginalRef) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.holds[ref.Key]) > 0, nil
}
