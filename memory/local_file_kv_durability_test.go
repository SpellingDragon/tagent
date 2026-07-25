package memory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestLocalFileKV_CloseDurability: writes below flushThreshold sit in the
// deferred-flush window; Close must synchronously flush them so a graceful
// exit never loses acknowledged writes (regression for the unclosed-KV gap).
func TestLocalFileKV_CloseDurability(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Fewer than flushThreshold writes → dirty, not yet on disk.
	if err := kv.KVPut("k1", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := kv.KVPut("k2", "v2"); err != nil {
		t.Fatal(err)
	}
	if err := kv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "kv.json"))
	if err != nil {
		t.Fatalf("kv.json must exist after Close: %v", err)
	}
	for _, want := range []string{"k1", "v2"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("kv.json missing %q after Close — deferred writes lost", want)
		}
	}
}

// TestLocalFileKV_ConcurrentPutClose: the final flush must hold the mutex —
// bare flushLocked raced concurrent writers on the data map (run with -race).
func TestLocalFileKV_ConcurrentPutClose(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = kv.KVPut(string(rune('a'+n)), "v")
			}
		}(i)
	}
	_ = kv.Close()
	wg.Wait()
}
