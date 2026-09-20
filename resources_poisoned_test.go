package tagent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SpellingDragon/tagent/memory"
)

// TestPoisoned_ExplicitEntrySealsPathAcrossGC — §6.4 (spec scenario「关闭失败
// 后的垃圾回收」+ design 决策7): when the final release cannot confirm the
// engine worker stopped, the registry keeps an EXPLICIT poisoned entry —
// strong references to store/engine/lockfile plus the failure result — and
// every same-path acquire fails with that recorded error, while unrelated
// paths keep working. Holding the writer lock must never depend on an
// unreachable fd that "GC happens not to reclaim".
func TestPoisoned_ExplicitEntrySealsPathAcrossGC(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	var seq []string
	engErr := errors.New("engine worker stuck")
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})

	var sealed *seqStore
	openFn := func() (openedResource, error) {
		s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
		sealed = s
		return openedResource{store: s, engine: &seqEngine{seq: &seq, err: engErr}}, nil
	}
	_, _, rel, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)

	require.ErrorIs(t, rel(), engErr, "the close failure must reach the releasing caller")

	// The entry survives explicitly — poisoned, holding the ORIGINAL store
	// and the live lockfile, with the failure recorded.
	key := resourceKey{kind: "localfile", path: canonicalize(dir)}
	rr.mu.Lock()
	e := rr.entries[key]
	rr.mu.Unlock()
	require.NotNil(t, e, "§6.4: the poisoned entry must be RETAINED, not detached + silently leaked")
	require.True(t, e.poisoned)
	require.Same(t, memory.MemoryStore(sealed), e.store, "entry keeps the strong store reference")
	require.NotNil(t, e.lockFile, "entry keeps the lockfile reference")
	require.ErrorIs(t, e.closeErr, engErr)

	// GC must not weaken the seal in any way.
	runtime.GC()
	runtime.GC()

	_, _, _, err2 := rr.acquire("localfile", dir, fp, openFn)
	require.ErrorIs(t, err2, ErrResourcePoisoned, "same-path acquire must fail EXPLICITLY (poisoned), not via a flock race or a resurrected generation")
	require.ErrorContains(t, err2, engErr.Error(), "the sealing error carries the recorded failure to the caller")

	// A different fingerprint on the same sealed path still reports poisoned
	// (the seal outranks conflict bookkeeping).
	_, _, _, err3 := rr.acquire("localfile", dir, "v1|other|fp", openFn)
	require.ErrorIs(t, err3, ErrResourcePoisoned)

	// Unrelated paths are unaffected.
	other := t.TempDir()
	fpOther := fingerprintMemory(MemoryConfig{Type: "localfile", Path: other})
	_, _, relOther, err4 := rr.acquire("localfile", other, fpOther, func() (openedResource, error) {
		s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
		return openedResource{store: s}, nil
	})
	require.NoError(t, err4, "poisoning one path must not seal the registry")
	require.NoError(t, relOther())

	// The flock is still held after GC — probe from a fresh handle (in-process
	// flocks on distinct open file descriptions conflict, cf. TestWriterLock).
	probe, perr := os.OpenFile(filepath.Join(canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, flockExclusive(probe), "the poisoned entry must still hold the writer lock after GC")
}
