package kv

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// Fault-injection tests for the LocalFileKV durability boundaries
// (resident-readiness-plan 2.2): every syscall on the barrier path
// (open / flush+sync / first-WAL dir sync / snapshot rename / dir sync /
// remove) is injectable via k.ops, and every injected failure must
// (a) propagate out of Sync()/Compact() as a non-nil error and
// (b) RETAIN the pending ops so a later Sync can complete the barrier.
// No real data directory is touched.

var errBoom = errors.New("injected boom")

// newFaultKV builds a healthy KV over a temp dir, then swaps the injectable
// syscall surface.
func newFaultKV(t *testing.T, mutate func(*fileOps)) *LocalFileKV {
	t.Helper()
	k, err := NewLocalFileKV(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalFileKV: %v", err)
	}
	ops := defaultFileOps
	mutate(&ops)
	k.ops = ops
	return k
}

func TestFault_WALSyncFails_PropagatesAndRetainsPending(t *testing.T) {
	k := newFaultKV(t, func(o *fileOps) { o.syncFile = func(*os.File) error { return errBoom } })
	defer k.Close()
	if err := k.KVPut("k", "v"); err != nil {
		t.Fatalf("KVPut (async acceptance): %v", err)
	}
	err := k.Sync()
	if err == nil || !strings.Contains(err.Error(), "fsync kv wal") {
		t.Fatalf("barrier must fail with the injected sync error, got: %v", err)
	}
	// Pending retained: heal the fault and the SAME ops complete the barrier.
	k.ops = defaultFileOps
	if err := k.Sync(); err != nil {
		t.Fatalf("Sync after heal: %v", err)
	}
}

// The retained-pending proof needs the SAME directory — split out to keep
// the temp dir accessible.
func TestFault_HealedSyncPersistsRetainedOps(t *testing.T) {
	dir := t.TempDir()
	k, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("NewLocalFileKV: %v", err)
	}
	ops := defaultFileOps
	ops.syncFile = func(*os.File) error { return errBoom }
	k.ops = ops

	if err := k.KVPut("k", "v"); err != nil {
		t.Fatalf("KVPut: %v", err)
	}
	if err := k.Sync(); err == nil {
		t.Fatal("expected barrier failure")
	}
	k.ops = defaultFileOps
	if err := k.Sync(); err != nil {
		t.Fatalf("Sync after heal: %v", err)
	}
	if err := k.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	fresh, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer fresh.Close()
	if v, err := fresh.KVGet("k"); err != nil || v != "v" {
		t.Fatalf("retained op lost after healed barrier: v=%q err=%v", v, err)
	}
}

func TestFault_DeferredFlushFailureRecordedInLastError(t *testing.T) {
	dir := t.TempDir()
	k, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("NewLocalFileKV: %v", err)
	}
	defer k.Close()
	ops := defaultFileOps
	ops.syncFile = func(*os.File) error { return errBoom }
	k.ops = ops

	// Cross the flush threshold: KVPut stays an async acceptance (nil), the
	// failure lands in LastError and pending is retained (2.4).
	for i := 0; i < flushThreshold; i++ {
		if err := k.KVPut(string(rune('a'+i%26)), "v"); err != nil {
			t.Fatalf("KVPut: %v", err)
		}
	}
	if k.LastError() == nil {
		t.Fatal("threshold flush failure must be observable via LastError")
	}
}

func TestFault_FirstWALDirSyncFails_BarrierFails(t *testing.T) {
	dir := t.TempDir()
	k, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("NewLocalFileKV: %v", err)
	}
	defer k.Close()
	if !k.walNeedsDirSync {
		t.Fatal("fresh store must flag first-WAL dir sync")
	}
	ops := defaultFileOps
	ops.syncDir = func(string) error { return errBoom }
	k.ops = ops
	if err := k.KVPut("k", "v"); err != nil {
		t.Fatalf("KVPut: %v", err)
	}
	if err := k.Sync(); err == nil || !strings.Contains(err.Error(), "fsync kv wal dir") {
		t.Fatalf("first-WAL dir-sync failure must fail the barrier, got: %v", err)
	}
	// Flag retained across failure: heal → retry completes the dir sync.
	if !k.walNeedsDirSync {
		t.Fatal("dir-sync flag must survive a failed attempt for retry")
	}
	k.ops = defaultFileOps
	if err := k.Sync(); err != nil {
		t.Fatalf("Sync after heal: %v", err)
	}
	if k.walNeedsDirSync {
		t.Fatal("dir-sync flag must clear after success")
	}
}

func TestFault_SnapshotRenameFails_CompactFails(t *testing.T) {
	k := newFaultKV(t, func(o *fileOps) { o.rename = func(string, string) error { return errBoom } })
	defer k.Close()
	if err := k.KVPut("k", "v"); err != nil {
		t.Fatalf("KVPut: %v", err)
	}
	if err := k.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := k.Compact(); err == nil || !strings.Contains(err.Error(), "rename kv snapshot") {
		t.Fatalf("compact rename failure must propagate, got: %v", err)
	}
}

func TestFault_SnapshotDirSyncFails_CompactFails(t *testing.T) {
	k := newFaultKV(t, func(o *fileOps) { o.syncDir = func(string) error { return errBoom } })
	defer k.Close()
	if err := k.KVPut("k", "v"); err != nil {
		t.Fatalf("KVPut: %v", err)
	}
	if err := k.Sync(); err == nil {
		t.Fatal("expected first-WAL dir-sync failure (same injected syncDir)")
	}
	// Clear the WAL dir-sync flag to isolate the compact path, then fail the
	// snapshot rename-dir sync via Compact.
	k.walNeedsDirSync = false
	if err := k.Compact(); err == nil || !strings.Contains(err.Error(), "fsync kv snapshot dir") {
		t.Fatalf("snapshot dir-sync failure must propagate, got: %v", err)
	}
}

func TestFault_WALRemoveFails_CompactFails(t *testing.T) {
	k := newFaultKV(t, func(o *fileOps) { o.remove = func(string) error { return errBoom } })
	defer k.Close()
	if err := k.KVPut("k", "v"); err != nil {
		t.Fatalf("KVPut: %v", err)
	}
	if err := k.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := k.Compact(); err == nil || !strings.Contains(err.Error(), "truncate kv wal") {
		t.Fatalf("WAL remove failure must propagate, got: %v", err)
	}
}

func TestFault_WALOpenFails_BarrierFails(t *testing.T) {
	k := newFaultKV(t, func(o *fileOps) {
		o.openFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errBoom }
	})
	defer k.Close()
	if err := k.KVPut("k", "v"); err != nil {
		t.Fatalf("KVPut: %v", err)
	}
	if err := k.Sync(); err == nil || !strings.Contains(err.Error(), "open kv wal") {
		t.Fatalf("open failure must fail the barrier, got: %v", err)
	}
}
