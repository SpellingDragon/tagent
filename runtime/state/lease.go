package state

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type leaseEntry struct {
	ref     LeaseRef
	release func(context.Context) error
	done    bool
}

// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
//
// Leases is the resource ledger one candidate preparation accumulates: every
// candidate-exclusive resource is registered beside the release that frees it
// at the moment it is acquired, and a preparation that fails frees what it
// acquired in the reverse of the acquisition order and once each.
//
// The ledger carries only what the candidate itself acquired. A shared face or
// a resource already in service is registered nowhere here, so a failed
// preparation never touches it.
type Leases struct {
	mu      sync.Mutex
	entries []leaseEntry
}

// NewLeases returns an empty candidate resource ledger.
func NewLeases() *Leases {
	return &Leases{}
}

// Register records one acquired resource beside the release that frees it and
// returns the registration the release path names it by. The release may be
// nil for a resource that owns nothing to free; such a registration is skipped
// by ReleaseAll.
func (l *Leases) Register(kind, key string, release func(context.Context) error) LeaseRef {
	l.mu.Lock()
	defer l.mu.Unlock()
	ref := LeaseRef{Kind: kind, Key: key, Ref: fmt.Sprintf("lease-%d", len(l.entries)+1)}
	l.entries = append(l.entries, leaseEntry{ref: ref, release: release})
	return ref
}

// Refs returns the registrations in the order the resources were acquired.
func (l *Leases) Refs() []LeaseRef {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LeaseRef, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, e.ref)
	}
	return out
}

// ReleaseAll frees every registered resource in the reverse of the acquisition
// order and exactly once each. A release that fails does not stop the releases
// behind it: its error is joined with the others into the returned error.
func (l *Leases) ReleaseAll(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var errs []error
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := &l.entries[i]
		if e.done {
			continue
		}
		e.done = true
		if e.release == nil {
			continue
		}
		if err := e.release(ctx); err != nil {
			errs = append(errs, fmt.Errorf("release %s %s (%s): %w", e.ref.Kind, e.ref.Key, e.ref.Ref, err))
		}
	}
	return errors.Join(errs...)
}
