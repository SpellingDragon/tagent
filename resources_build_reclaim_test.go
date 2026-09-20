package tagent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// §6.5 — construction failure reclamation: every resource obtained during a
// build is released in reverse order, AND a reclaim that cannot be CONFIRMED
// must not leave the writer open (the path seals like an unconfirmed worker
// stop), while a cleanly-released failed build stays freely retryable.

// TestBuildFailure_CleanReclaimStaysRetryable: an open() failure whose
// partially built resources were reclaimed successfully releases the writer
// lock — the next acquire at the same path must be able to open fresh.
func TestBuildFailure_CleanReclaimStaysRetryable(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	buildErr := errors.New("kv died at startup")

	_, _, _, err := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{}, buildErr
	})
	require.ErrorIs(t, err, buildErr)

	// Lock freed → reopen succeeds.
	_, _, rel, err2 := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err2, "a cleanly reclaimed failed build must not seal the path")
	require.NoError(t, rel())
}

// TestBuildFailure_UnconfirmedReclaimSealsWriter: when the release of a
// partially built resource itself fails (ErrReclaimUnconfirmed), the writer
// lock is HELD and the path is sealed with a poisoned entry carrying the
// cause — a new generation must never open alongside a possibly half-live
// backend (design 决策7: 无法安全回收同样保持 poisoned).
func TestBuildFailure_UnconfirmedReclaimSealsWriter(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	buildErr := errors.New("segment store init failed")
	closeErr := errors.New("kv close hung")

	_, _, _, err := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{}, fmt.Errorf("%w; %w", buildErr,
			fmt.Errorf("%w: kv close: %v", ErrReclaimUnconfirmed, closeErr))
	})
	require.ErrorIs(t, err, ErrReclaimUnconfirmed, "the original build error still reaches the caller")

	key := resourceKey{kind: "localfile", path: canonicalize(dir)}
	rr.mu.Lock()
	e := rr.entries[key]
	rr.mu.Unlock()
	require.NotNil(t, e, "unconfirmed reclaim must seal via an explicit poisoned entry")
	require.True(t, e.poisoned)

	_, _, _, err2 := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		t.Fatal("open must NOT run again on a sealed path")
		return openedResource{}, nil
	})
	require.ErrorIs(t, err2, ErrResourcePoisoned)

	// The writer lock stayed held — the seal is real, not bookkeeping only.
	probe, perr := os.OpenFile(filepath.Join(canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, flockExclusive(probe), "an unconfirmed reclaim must keep holding the writer lock")
}
