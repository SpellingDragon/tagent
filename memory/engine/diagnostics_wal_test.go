package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
)

// TestDiagnostics_WALQuarantinedThroughChain (§8.11⑤ real regression, 7th
// review round): the F3 quarantine counter must reach the diagnostics
// snapshot through the FULL production decorator chain —
// ErrorTrackingStore(engineBridge(FileSegmentStore(LocalFileKV))).
// Fail-before: the chain broke at FileSegmentStore (no passthrough) and the
// old test only asserted the bare KV, so it passed while diagnostics read 0.
func TestDiagnostics_WALQuarantinedThroughChain(t *testing.T) {
	dir := t.TempDir()
	// Hand-write a WAL with a corrupted mid line + good tail.
	wal := "{\"o\":\"p\",\"k\":\"bad1\",\"v\":\"x\"}\n" +
		"{\"CORRUPT\n" +
		"{\"o\":\"p\",\"k\":\"k1\",\"v\":\"v1\"}\n"
	if err := os.WriteFile(filepath.Join(dir, "kv.wal.jsonl"), []byte(wal), 0o600); err != nil {
		t.Fatal(err)
	}
	kvStore, err := kv.NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer kvStore.Close()
	if kvStore.WalQuarantined() != 1 {
		t.Fatalf("kv quarantine = %d, want 1", kvStore.WalQuarantined())
	}

	seg, err := memory.NewFileSegmentStore(kvStore, nil, ":memory:", 100)
	if err != nil {
		t.Fatal(err)
	}
	bridged := NewEngineBridge(seg, nil)              // capacity-only style bridge
	ets := memory.NewErrorTrackingStore(bridged, nil) // outermost decorator

	diag := NewMemoryDiagnostics(nil, ets)
	snap := diag.Snapshot()
	if snap.WALQuarantined != 1 {
		t.Fatalf("diagnostics must see quarantine through the chain, got %d", snap.WALQuarantined)
	}
}
