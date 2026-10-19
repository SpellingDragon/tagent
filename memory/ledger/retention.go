// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// Retention leases.
//
// A retention lease is held by the shared resource owner — the recovery
// resource or the unsettled obligation the hold protects, never "an agent" —
// and protection is reference counted across holders: it stands until the last
// holder releases it, and every hold and release is idempotent. The guard is
// the single source of truth for that count; this layer registers what is held
// and the original time the key reclassifies under once released, and keeps no
// second lease system of its own.
package ledger

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// RetentionKind names why one registration holds its material: an unsettled
// delivery obligation, or material a recovery still has to take inventory of.
type RetentionKind string

const (
	// RetentionUnconfirmed is the kind of a registration protecting material
	// whose delivery is still unconfirmed.
	RetentionUnconfirmed RetentionKind = "unconfirmed_delivery"
	// RetentionRecovery is the kind of a registration protecting material a
	// recovery inventory has not accounted for yet.
	RetentionRecovery RetentionKind = "recovery_material"
)

// RetentionRecord is what one retention registration remembers about the
// material it protects: the kind of material, its frozen event type and the
// original recorded time. The recorded time is the timestamp the key returns to
// the age window under once its protection is released — a release never
// re-stamps the material, so it reclassifies exactly as old as it always was.
type RetentionRecord struct {
	// Kind names why the material is held.
	Kind RetentionKind
	// Type is the frozen event type of the material.
	Type string
	// RecordedAt is the original recorded time of the material, kept verbatim
	// and never re-stamped.
	RecordedAt time.Time
}

// Retention is the retention lease ledger over the shared guard: it registers
// what each hold protects and hands back the guard's leases, so a reclaimer
// asks one surface whether an original is still protected and on what original
// timestamp it will reclassify. Holders are the stable identities of recovery
// resources and unsettled obligations, so two holders on one key protect it
// independently and the protection is gone only once the last one releases.
//
// The ledger registers and retires holds: the guard is the one port that holds
// them, so the ledger never declares a second surface of the same name.
type Retention struct {
	mu      sync.Mutex
	guard   Guard
	records map[event.EventID]RetentionRecord
	leases  map[event.EventID]map[HolderID]ProtectionLease
}

// NewRetention returns a retention ledger over the given guard. The guard owns
// the protection state and its reference count; the ledger adds registrations
// and keeps no second truth.
func NewRetention(guard Guard) *Retention {
	return &Retention{
		guard:   guard,
		records: map[event.EventID]RetentionRecord{},
		leases:  map[event.EventID]map[HolderID]ProtectionLease{},
	}
}

// Register files one protection hold on the key for holder and returns the
// lease its release must carry back, recording what the hold protects. A repeat
// hold by the same holder while the hold stands is recognized as the same hold
// and counts once, and the registered original time of a key is kept from its
// first registration: a re-hold never re-stamps the material.
func (r *Retention) Register(ctx context.Context, key event.EventID, holder HolderID, record RetentionRecord) (ProtectionLease, error) {
	if key == "" {
		return "", ErrKeyRequired
	}
	lease, err := r.guard.Hold(ctx, OriginalRef{Key: key, RetentionPolicy: string(record.Kind)}, holder)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, registered := r.records[key]; !registered {
		r.records[key] = record
	}
	holders, held := r.leases[key]
	if !held {
		holders = map[HolderID]ProtectionLease{}
		r.leases[key] = holders
	}
	holders[holder] = lease
	return lease, nil
}

// Retire drops the hold of holder on the key under the lease that hold
// returned. Releasing a hold that is not standing is a no-op; a release whose
// lease is not the current lease is refused by name with ErrLeaseMismatch and
// leaves every hold standing. The registration is kept after release: the key
// reclassifies under its recorded original time, which a release never
// re-stamps.
func (r *Retention) Retire(ctx context.Context, key event.EventID, holder HolderID, lease ProtectionLease) error {
	if key == "" {
		return ErrKeyRequired
	}
	if err := r.guard.Release(ctx, OriginalRef{Key: key}, holder, lease); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if holders, held := r.leases[key]; held {
		if current, standing := holders[holder]; standing && current == lease {
			delete(holders, holder)
		}
		if len(holders) == 0 {
			delete(r.leases, key)
		}
	}
	return nil
}

// ReleaseHolder drops every hold one holder still stands on and returns how
// many were released. It is what a closing owner runs: it releases exactly that
// holder's holds, so a shared protection another holder keeps on the same key
// stays standing until that holder releases too.
func (r *Retention) ReleaseHolder(ctx context.Context, holder HolderID) (int, error) {
	if holder == "" {
		return 0, ErrHolderRequired
	}
	r.mu.Lock()
	type hold struct {
		key   event.EventID
		lease ProtectionLease
	}
	var standing []hold
	for key, holders := range r.leases {
		if lease, held := holders[holder]; held {
			standing = append(standing, hold{key: key, lease: lease})
		}
	}
	r.mu.Unlock()
	sort.Slice(standing, func(i, j int) bool { return standing[i].key < standing[j].key })
	released := 0
	for _, hold := range standing {
		if err := r.guard.Release(ctx, OriginalRef{Key: hold.key}, holder, hold.lease); err != nil {
			return released, err
		}
		released++
		r.mu.Lock()
		if holders, held := r.leases[hold.key]; held {
			delete(holders, holder)
			if len(holders) == 0 {
				delete(r.leases, hold.key)
			}
		}
		r.mu.Unlock()
	}
	return released, nil
}

// Held reports whether any holder still protects the key, so a reclaimer asks
// before it reclaims anything. Protection is reference counted in the guard: it
// stands until the last holder has released.
func (r *Retention) Held(ctx context.Context, key event.EventID) (bool, error) {
	return r.guard.Protected(ctx, OriginalRef{Key: key})
}

// Record returns the registration of one key: what its hold protects and the
// original recorded time it reclassifies under once released.
func (r *Retention) Record(key event.EventID) (RetentionRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, registered := r.records[key]
	return record, registered
}
