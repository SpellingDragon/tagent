// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// Storage lifecycle: retention, tombstones, recovery inventory and reclamation
// coordination.
//
// TTL expiry and capacity eviction only tombstone a key; the physical destroy
// happens at compaction, and never to an original a retention lease still
// protects. The first destructive pass waits for the lease arming and the
// inventory barrier to lift — the recovery inventory runs before any
// reclamation — and a store that cannot protect or coordinate reclamation is
// reported by name on the durable recovery tier instead of being treated as
// unprotected success.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// StoredKey is one key as the lifecycle enumerates it: its canonical identity,
// its frozen event type, the partition it is filed under and the time it was
// recorded, which is what its age window is measured from.
type StoredKey struct {
	// Key is the canonical identity of the key.
	Key event.EventID
	// Type is the frozen event type of the material.
	Type string
	// Partition is the partition the key is filed under.
	Partition int
	// RecordedAt is the time the material was recorded.
	RecordedAt time.Time
}

// LifecycleCapabilities names the coordination capabilities of one store: the
// durable recovery tier it commits under, and whether it can protect originals
// against destruction and coordinate their reclamation. A tier that holds
// nothing across process exit says so instead of appearing durable, and a tier
// missing protection or reclamation coordination says so instead of faking
// protection that never took effect.
type LifecycleCapabilities struct {
	// DurableRecovery reports the durable recovery tier: the store commits
	// across process exit, so its material is potential recovery material.
	DurableRecovery bool
	// Protection reports that originals can be kept from physical destruction
	// while a retention lease stands.
	Protection bool
	// Reclamation reports that tombstoned keys can be physically reclaimed at
	// compaction.
	Reclamation bool
}

// LifecycleStore is the reclamation surface the storage lifecycle coordinates
// with: enumerate the keys it holds, tombstone an expired key, and physically
// reclaim the tombstoned ones at compaction. A failed enumeration is reported
// by name and never returned as a partial list, so a reclaimer never reconciles
// a partial view.
//
// The redline list (4 rules):
// MUST enumerate every key it holds, or fail by name.
// MUST keep an original readable until compaction once it is tombstoned.
// MUST NOT answer a failed enumeration as a partial list.
// MUST NOT take the capabilities of a store from the store's own claim: they are settled by the wiring that opens it.
type LifecycleStore interface {
	// EnumerateKeys lists every key the store holds, or fails by name: a
	// partial list is never returned as an enumeration.
	EnumerateKeys(ctx context.Context) ([]StoredKey, error)
	// Tombstone marks one key for reclamation at compaction. Tombstoning is not
	// a physical destroy: the original stays readable until compaction.
	Tombstone(ctx context.Context, key event.EventID, at time.Time) error
	// ReclaimTombstoned physically destroys the given tombstoned keys at
	// compaction and returns how many were destroyed.
	ReclaimTombstoned(ctx context.Context, keys []event.EventID) (int, error)
}

var (
	// ErrCapabilityIncomplete reports a durable recovery tier whose store lacks
	// the protection or reclamation coordination the tier promises: the system
	// refuses to start instead of treating "no protection" as success.
	ErrCapabilityIncomplete = errors.New("memory: storage lacks protection/reclamation coordination for the durable recovery tier")
	// ErrRecoveryIncomplete reports a destructive pass attempted before the
	// recovery inventory completed: potential recovery material is never let
	// through by the grace deadline alone.
	ErrRecoveryIncomplete = errors.New("memory: recovery inventory incomplete, destructive reclamation withheld")
	// ErrEnumerationIncomplete reports a failed key enumeration: the partial
	// view is never reconciled and no key is tombstoned off it.
	ErrEnumerationIncomplete = errors.New("memory: key enumeration failed, no partial view is reconciled")
	// ErrProtected reports a destructive action on a key a standing retention
	// lease still protects: only a refusal by name, never a silent destroy.
	ErrProtected = errors.New("memory: key is protected by a standing retention lease")
	// ErrLifecycleUnwired reports a lifecycle constructed without the retention
	// ledger or the store it coordinates.
	ErrLifecycleUnwired = errors.New("memory: lifecycle needs a retention ledger and a store")
)

// ScanReport is what one TTL/capacity scan did: the keys it tombstoned, the
// keys a standing retention lease kept out of the scan, and the keys the policy
// exempts from TTL tombstoning entirely.
type ScanReport struct {
	// Tombstoned lists the keys this scan marked for reclamation at compaction.
	Tombstoned []event.EventID
	// Protected lists the keys a standing retention lease kept out of the scan:
	// within retention, TTL and capacity never tombstone what is protected.
	Protected []event.EventID
	// Exempt lists the keys the policy exempts from TTL tombstoning.
	Exempt []event.EventID
}

// armGraceBound bounds how long the first destructive pass waits for the
// recovery owner's lease to appear: a missing lease stops forgetting for a
// while, never forever on a tier that promises no process recovery.
const armGraceBound = 30 * time.Second

// Lifecycle coordinates the storage lifecycle with the retention leases: the
// recovery inventory runs before the first destructive pass, TTL and capacity
// tombstone rather than destroy, protected material is never tombstoned, and
// the physical destroy waits for compaction.
type Lifecycle struct {
	mu            sync.Mutex
	cfg           LifecycleConfig
	retention     *Retention
	store         LifecycleStore
	caps          LifecycleCapabilities
	recovered     map[event.EventID]StoredKey
	recoveryDone  bool
	armed         bool
	graceDeadline time.Time
	tombstones    map[event.EventID]time.Time
}

// NewLifecycle coordinates one storage lifecycle over the retention ledger, the
// store and the capabilities that store was opened with. On the durable
// recovery tier a store missing protection or reclamation coordination is
// refused by name with ErrCapabilityIncomplete (fail-closed): the tier promises
// recovery, so "no protection" is never treated as success. A non-durable tier
// keeps its capability gap explicit in the capabilities it was opened under,
// and never fakes protection that did not take effect.
func NewLifecycle(cfg LifecycleConfig, retention *Retention, store LifecycleStore, caps LifecycleCapabilities) (*Lifecycle, error) {
	if retention == nil || store == nil {
		return nil, ErrLifecycleUnwired
	}
	if caps.DurableRecovery && (!caps.Protection || !caps.Reclamation) {
		return nil, fmt.Errorf("%w: protection=%v reclamation=%v", ErrCapabilityIncomplete, caps.Protection, caps.Reclamation)
	}
	return &Lifecycle{
		cfg:        cfg.Normalized(),
		retention:  retention,
		store:      store,
		caps:       caps,
		recovered:  map[event.EventID]StoredKey{},
		tombstones: map[event.EventID]time.Time{},
	}, nil
}

// RegisterRecovered registers one recovered key during the recovery inventory:
// the restart registration that must precede every eviction. Registrations are
// taken before the barrier lifts, so material a restart still has to account
// for is never swept away by the first pass.
func (l *Lifecycle) RegisterRecovered(ctx context.Context, key StoredKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key.Key == "" {
		return ErrEventKeyRequired
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recovered[key.Key] = key
	return nil
}

// RecoveryComplete lifts the inventory barrier: the recovery inventory is done
// and the first destructive pass may run once the lease arming stands.
func (l *Lifecycle) RecoveryComplete() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recoveryDone = true
}

// ArmGrace arms the retention lease wait of the first destructive pass under
// the given deadline: destructive reclamation waits for the arming and the
// inventory barrier, and once the bounded grace expires only a tier that
// promises no process recovery and holds no registered recovery material lets
// go — a missing lease must not stop forgetting forever, while the durable
// tier's pending material is never let through by the clock alone.
func (l *Lifecycle) ArmGrace(deadline time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.armed = true
	l.graceDeadline = deadline
}

// armedFor reports whether the destructive pass may run: the inventory barrier is
// lifted, or the bounded grace expired with no durable recovery material left
// to register — grace expiring with the inventory still open lets go only on a
// tier that promises no process recovery and holds no registered material.
func (l *Lifecycle) armedFor(now time.Time) bool {
	if l.recoveryDone {
		return true
	}
	if !l.armed || !now.After(l.graceDeadline) {
		return false
	}
	return !l.caps.DurableRecovery && len(l.recovered) == 0
}

// Scan runs one TTL/capacity pass: expired and over-capacity keys are
// tombstoned for reclamation at compaction, never physically destroyed here,
// and a key a retention lease still protects is kept out of the scan entirely.
// A failed enumeration is reported by name and tombstones nothing, so a partial
// view is never reconciled.
func (l *Lifecycle) Scan(ctx context.Context, now time.Time) (ScanReport, error) {
	l.mu.Lock()
	if !l.armedFor(now) {
		l.mu.Unlock()
		return ScanReport{}, ErrRecoveryIncomplete
	}
	l.mu.Unlock()
	keys, err := l.store.EnumerateKeys(ctx)
	if err != nil {
		return ScanReport{}, fmt.Errorf("%w: %w", ErrEnumerationIncomplete, err)
	}
	report := ScanReport{}
	for _, key := range keys {
		protected, err := l.retention.Held(ctx, key.Key)
		if err != nil {
			return report, err
		}
		if protected {
			report.Protected = append(report.Protected, key.Key)
			continue
		}
		window, exempt := l.cfg.Window(key.Type)
		if exempt {
			report.Exempt = append(report.Exempt, key.Key)
			continue
		}
		if now.Sub(l.recordedAt(key)) <= window {
			continue
		}
		if err := l.tombstone(ctx, key.Key, now); err != nil {
			return report, err
		}
		report.Tombstoned = append(report.Tombstoned, key.Key)
	}
	if err := l.evictOverCapacity(ctx, keys, now, &report); err != nil {
		return report, err
	}
	return report, nil
}

// evictOverCapacity tombstones the oldest unprotected keys of a partition that
// runs over the configured capacity bound, oldest first, keeping only what the
// bound allows. The bound of zero keeps everything.
func (l *Lifecycle) evictOverCapacity(ctx context.Context, keys []StoredKey, now time.Time, report *ScanReport) error {
	bound := l.cfg.MaxEventsPerPartition
	if bound < 1 {
		return nil
	}
	byPartition := map[int][]StoredKey{}
	for _, key := range keys {
		if l.isTombstoned(key.Key) {
			continue
		}
		byPartition[key.Partition] = append(byPartition[key.Partition], key)
	}
	for _, group := range byPartition {
		if len(group) <= bound {
			continue
		}
		sort.Slice(group, func(i, j int) bool { return group[i].RecordedAt.Before(group[j].RecordedAt) })
		for _, key := range group[:len(group)-bound] {
			protected, err := l.retention.Held(ctx, key.Key)
			if err != nil {
				return err
			}
			if protected {
				continue
			}
			if l.isTombstoned(key.Key) {
				continue
			}
			if err := l.tombstone(ctx, key.Key, now); err != nil {
				return err
			}
			report.Tombstoned = append(report.Tombstoned, key.Key)
		}
	}
	return nil
}

// Delete is the explicit destroy request for one key: it tombstones the key,
// and a key a standing retention lease protects is refused by name with
// ErrProtected instead of being destroyed under the protection.
func (l *Lifecycle) Delete(ctx context.Context, key event.EventID, now time.Time) error {
	l.mu.Lock()
	armed := l.armedFor(now)
	l.mu.Unlock()
	if !armed {
		return ErrRecoveryIncomplete
	}
	protected, err := l.retention.Held(ctx, key)
	if err != nil {
		return err
	}
	if protected {
		return fmt.Errorf("%w: %s", ErrProtected, key)
	}
	return l.tombstone(ctx, key, now)
}

// Reclaim physically destroys the tombstoned keys at compaction. Keys are only
// ever destroyed here, so a release of protection never makes an original
// immediately unreadable: the original stays readable between its tombstone and
// this pass.
func (l *Lifecycle) Reclaim(ctx context.Context, now time.Time) (int, error) {
	l.mu.Lock()
	if !l.armedFor(now) {
		l.mu.Unlock()
		return 0, ErrRecoveryIncomplete
	}
	keys := make([]event.EventID, 0, len(l.tombstones))
	for key := range l.tombstones {
		keys = append(keys, key)
	}
	l.mu.Unlock()
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	destroyed, err := l.store.ReclaimTombstoned(ctx, keys)
	if err != nil {
		return destroyed, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		delete(l.tombstones, key)
	}
	return destroyed, nil
}

// Tombstoned reports whether one key is marked for reclamation at compaction.
func (l *Lifecycle) Tombstoned(key event.EventID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.isTombstoned(key)
}

func (l *Lifecycle) isTombstoned(key event.EventID) bool {
	_, marked := l.tombstones[key]
	return marked
}

func (l *Lifecycle) tombstone(ctx context.Context, key event.EventID, at time.Time) error {
	if err := l.store.Tombstone(ctx, key, at); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tombstones[key] = at
	return nil
}

// recordedAt returns the time one key's age window is measured from: the
// original recorded time its retention registration kept, which a release never
// re-stamps, and the stored time otherwise. A released key reclassifies exactly
// as old as it always was.
func (l *Lifecycle) recordedAt(key StoredKey) time.Time {
	if record, registered := l.retention.Record(key.Key); registered {
		return record.RecordedAt
	}
	return key.RecordedAt
}

// MemoryLifecycleStore is the in-memory lifecycle store: the reference
// implementation of the reclamation surface, holding only what the process
// holds and declaring no recovery across process exit.
type MemoryLifecycleStore struct {
	mu         sync.Mutex
	caps       LifecycleCapabilities
	keys       map[event.EventID]StoredKey
	tombstones map[event.EventID]time.Time
}

// NewMemoryLifecycleStore returns an empty in-memory lifecycle store under the
// given declared capabilities.
func NewMemoryLifecycleStore(caps LifecycleCapabilities) *MemoryLifecycleStore {
	return &MemoryLifecycleStore{
		caps:       caps,
		keys:       map[event.EventID]StoredKey{},
		tombstones: map[event.EventID]time.Time{},
	}
}

// Put files one key under the store's enumeration.
func (s *MemoryLifecycleStore) Put(key StoredKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key.Key] = key
}

// Capabilities reports the capabilities the store was opened under, so the
// wiring that constructs a lifecycle hands them back beside the store.
func (s *MemoryLifecycleStore) Capabilities() LifecycleCapabilities {
	return s.caps
}

// EnumerateKeys implements LifecycleStore.EnumerateKeys.
func (s *MemoryLifecycleStore) EnumerateKeys(ctx context.Context) ([]StoredKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.keys))
	for key := range s.keys {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	out := make([]StoredKey, 0, len(keys))
	for _, key := range keys {
		out = append(out, s.keys[event.EventID(key)])
	}
	return out, nil
}

// Tombstone implements LifecycleStore.Tombstone.
func (s *MemoryLifecycleStore) Tombstone(ctx context.Context, key event.EventID, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tombstones[key] = at
	return nil
}

// ReclaimTombstoned implements LifecycleStore.ReclaimTombstoned: only a key
// that was tombstoned is physically destroyed, and destroying it once removes
// it from the store.
func (s *MemoryLifecycleStore) ReclaimTombstoned(ctx context.Context, keys []event.EventID) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	destroyed := 0
	for _, key := range keys {
		if _, marked := s.tombstones[key]; !marked {
			continue
		}
		delete(s.tombstones, key)
		delete(s.keys, key)
		destroyed++
	}
	return destroyed, nil
}

// Tombstoned reports the tombstone time of one key, and whether it is marked.
func (s *MemoryLifecycleStore) Tombstoned(key event.EventID) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, marked := s.tombstones[key]
	return at, marked
}
