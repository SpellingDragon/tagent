package memory

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestLocalFileKV_CloseDurability: writes below flushThreshold sit in the
// deferred-flush window; Close must synchronously persist them so a graceful
// exit never loses acknowledged writes. The assertion is the essential one —
// a fresh instance over the same dir reads the data back (snapshot or WAL,
// wherever it physically lives).
func TestLocalFileKV_CloseDurability(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.KVPut("k1", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := kv.KVPut("k2", "v2"); err != nil {
		t.Fatal(err)
	}
	if err := kv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	re, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer re.Close()
	for k, want := range map[string]string{"k1": "v1", "k2": "v2"} {
		got, err := re.KVGet(k)
		if err != nil || got != want {
			t.Errorf("after reopen %s=%q err=%v, want %q — deferred writes lost", k, got, err, want)
		}
	}
}

// TestLocalFileKV_ConcurrentPutClose: the final flush must hold the mutex —
// a bare flush raced concurrent writers on the data map (run with -race).
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

// TestLocalFileKV_WALReplayAndTornTail: ops appended to the WAL replay on
// startup; a torn final line (crash mid-append) is tolerated while data
// before it survives.
func TestLocalFileKV_WALReplayAndTornTail(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = kv.KVPut("a", "1")
	_ = kv.KVPut("b", "2")
	_ = kv.KVDelete("a")
	if err := kv.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash mid-append: torn half line at the WAL tail.
	walPath := filepath.Join(dir, "kv.wal.jsonl")
	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"o":"p","k":"c","v":"tru`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	re, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("reopen with torn WAL tail must succeed: %v", err)
	}
	defer re.Close()
	if v, err := re.KVGet("b"); err != nil || v != "2" {
		t.Errorf("b=%q err=%v, want 2", v, err)
	}
	if _, err := re.KVGet("a"); err == nil {
		t.Errorf("delete op must replay: a should be gone")
	}
	if _, err := re.KVGet("c"); err == nil {
		t.Errorf("torn tail op must be ignored, c should not exist")
	}
}

// TestLocalFileKV_CompactMergesWALIntoSnapshot: Compact folds WAL ops into
// the snapshot and truncates the WAL; data is intact afterwards and on reopen.
func TestLocalFileKV_CompactMergesWALIntoSnapshot(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = kv.KVPut("x", "1")
	_ = kv.KVPut("y", "2")
	_ = kv.KVDelete("x")
	if err := kv.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kv.wal.jsonl")); !os.IsNotExist(err) {
		t.Errorf("WAL must be truncated after compaction")
	}
	if err := kv.Close(); err != nil {
		t.Fatal(err)
	}

	re, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	if v, err := re.KVGet("y"); err != nil || v != "2" {
		t.Errorf("y=%q err=%v, want 2", v, err)
	}
	if _, err := re.KVGet("x"); err == nil {
		t.Errorf("x was deleted before compaction, must not resurrect")
	}
}

// TestLocalFileKV_LegacySingleFileLayout: an old-layout dir (kv.json only,
// no WAL) loads transparently.
func TestLocalFileKV_LegacySingleFileLayout(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kv.json"), []byte(`{"old":"data"}`), 0644); err != nil {
		t.Fatal(err)
	}
	kv, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	if v, err := kv.KVGet("old"); err != nil || v != "data" {
		t.Errorf("legacy snapshot must load, got %q err=%v", v, err)
	}
}
