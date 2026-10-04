package kv

import (
	"github.com/SpellingDragon/tagent/memory"

	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalFileKV_Interface verifies LocalFileKV satisfies the memory.KVStore interface.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestLocalFileKV_Interface(t *testing.T) {
	var _ memory.KVStore = (*LocalFileKV)(nil)
}

// TestLocalFileKV_CRUD tests basic put/get/delete operations.
func TestLocalFileKV_CRUD(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	err = kv.KVPut("key1", "value1")
	require.NoError(t, err)

	val, err := kv.KVGet("key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)

	_, err = kv.KVGet("nonexistent")
	assert.Error(t, err)

	err = kv.KVDelete("key1")
	require.NoError(t, err)

	_, err = kv.KVGet("key1")
	assert.Error(t, err)
}

// TestLocalFileKV_Scan tests prefix scanning with sorting.
func TestLocalFileKV_Scan(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	require.NoError(t, kv.KVPut("prefix:k3", "v3"))
	require.NoError(t, kv.KVPut("prefix:k1", "v1"))
	require.NoError(t, kv.KVPut("prefix:k2", "v2"))
	require.NoError(t, kv.KVPut("other:k1", "ov1"))

	pairs, err := kv.KVScan("prefix:", 0)
	require.NoError(t, err)
	assert.Len(t, pairs, 3)
	assert.Equal(t, "prefix:k1", pairs[0].Key)
	assert.Equal(t, "prefix:k2", pairs[1].Key)
	assert.Equal(t, "prefix:k3", pairs[2].Key)

	pairs, err = kv.KVScan("prefix:", 2)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)

	pairs, err = kv.KVScan("nomatch:", 0)
	require.NoError(t, err)
	assert.Empty(t, pairs)
}

// TestLocalFileKV_Range tests range scanning.
func TestLocalFileKV_Range(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	require.NoError(t, kv.KVPut("key:001", "v1"))
	require.NoError(t, kv.KVPut("key:002", "v2"))
	require.NoError(t, kv.KVPut("key:003", "v3"))
	require.NoError(t, kv.KVPut("key:004", "v4"))

	pairs, err := kv.KVRange("key:002", "key:004", 0)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)
	assert.Equal(t, "key:002", pairs[0].Key)
	assert.Equal(t, "key:003", pairs[1].Key)

	pairs, err = kv.KVRange("key:001", "key:004", 2)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)
}

// TestLocalFileKV_Batch tests batch operations.
func TestLocalFileKV_Batch(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	err = kv.KVBatch([]memory.KVOp{
		{Type: "put", Key: "b1", Value: "v1"},
		{Type: "put", Key: "b2", Value: "v2"},
		{Type: "put", Key: "b3", Value: "v3"},
	})
	require.NoError(t, err)

	val, err := kv.KVGet("b2")
	require.NoError(t, err)
	assert.Equal(t, "v2", val)

	err = kv.KVBatch([]memory.KVOp{
		{Type: "delete", Key: "b1"},
		{Type: "put", Key: "b4", Value: "v4"},
	})
	require.NoError(t, err)

	_, err = kv.KVGet("b1")
	assert.Error(t, err)

	val, err = kv.KVGet("b4")
	require.NoError(t, err)
	assert.Equal(t, "v4", val)
}

// TestLocalFileKV_Persistence verifies data survives across instances.
func TestLocalFileKV_Persistence(t *testing.T) {
	dir := t.TempDir()

	kv1, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	require.NoError(t, kv1.KVPut("persist:key1", "value1"))
	require.NoError(t, kv1.KVPut("persist:key2", "value2"))

	require.NoError(t, kv1.Sync())
	require.NoError(t, kv1.Close())

	_, snapErr := os.Stat(filepath.Join(dir, "kv-global.json"))
	require.NoError(t, snapErr, "non-partition namespaces land in kv-global.json, one snapshot file per bucket")
	_, legacyErr := os.Stat(filepath.Join(dir, "kv.json"))
	require.True(t, os.IsNotExist(legacyErr), "the legacy single kv.json layout must not reappear")

	kv2, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	defer kv2.Close()

	val, err := kv2.KVGet("persist:key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)

	val, err = kv2.KVGet("persist:key2")
	require.NoError(t, err)
	assert.Equal(t, "value2", val)

	pairs, err := kv2.KVScan("persist:", 0)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)
}

// TestLocalFileKV_EmptyFile tests that an empty kv.json file doesn't cause errors.
func TestLocalFileKV_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	kvPath := filepath.Join(dir, "kv.json")
	require.NoError(t, os.WriteFile(kvPath, []byte{}, 0644))

	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	require.NoError(t, kv.KVPut("test", "value"))
	val, err := kv.KVGet("test")
	require.NoError(t, err)
	assert.Equal(t, "value", val)
}

// TestLocalFileKV_Concurrent tests concurrent access safety.
func TestLocalFileKV_Concurrent(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = kv.KVPut("concurrent", "value")
		}
	}()

	for i := 0; i < 100; i++ {
		_, _ = kv.KVScan("concurrent", 0)
	}

	<-done
}

// TestLocalFileKV_ListPartitionIDs pins how partition ids are derived from persisted keys.
// - Any key in a partition namespace ({pid}:evt|idx|meta|tomb) proves that partition.
// - Non-partition namespaces such as global:* and unparsable prefixes are ignored.
func TestLocalFileKV_ListPartitionIDs(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	for _, k := range []string{
		"7:evt:1710676800:1",
		"7:meta:1710676800",
		"42:tomb:deadbeef",
		"global:active_partitions",
		"not-a-pid:evt:1",
	} {
		if err := kv.KVPut(k, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := kv.Sync(); err != nil {
		t.Fatal(err)
	}
	got := kv.ListPartitionIDs()
	if len(got) != 2 || got[0] != 7 || got[1] != 42 {
		t.Fatalf("ListPartitionIDs = %v, want [7 42]", got)
	}
}

// TestLocalFileKV_SyncOnlyWritesDirtyBuckets 钉住脏桶隔离提交。
// - 仅脏分区快照被重写；单次提交的落盘成本只由所属分区键数决定。
func TestLocalFileKV_SyncOnlyWritesDirtyBuckets(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	require.NoError(t, kv.KVPut("1:evt:a", "p1-a"))
	require.NoError(t, kv.KVPut("2:evt:b", "p2-a"))
	require.NoError(t, kv.Sync())

	p1 := filepath.Join(dir, "kv-1.json")
	p2 := filepath.Join(dir, "kv-2.json")
	require.FileExists(t, p1)
	require.FileExists(t, p2)
	m1, err := os.Stat(p1)
	require.NoError(t, err)
	m2, err := os.Stat(p2)
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, kv.KVPut("2:evt:c", "p2-b"))
	require.NoError(t, kv.Sync())

	n1, err := os.Stat(p1)
	require.NoError(t, err)
	n2, err := os.Stat(p2)
	require.NoError(t, err)
	require.True(t, n1.ModTime().Equal(m1.ModTime()), "an untouched partition's snapshot must not be rewritten")
	require.True(t, n2.ModTime().After(m2.ModTime()), "the dirty partition's snapshot must be rewritten")
	require.NoError(t, kv.Close())
}

func TestLocalFileKV_EmptiedBucketFileRemoved(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	require.NoError(t, kv.KVPut("3:idx:x", "1"))
	require.NoError(t, kv.Sync())
	require.Equal(t, []int{3}, kv.ListPartitionIDs())

	require.NoError(t, kv.KVDelete("3:idx:x"))
	require.NoError(t, kv.Sync())
	require.NoFileExists(t, filepath.Join(dir, "kv-3.json"),
		"an emptied partition must stop presenting itself as persisted")
	require.Empty(t, kv.ListPartitionIDs())
	require.NoError(t, kv.Close())
}

// TestLocalFileKV_CrossProcessReadBack 钉住快照屏障的跨进程可见性。
// - 子进程 Sync 后退出，新进程读回全部分区键值；多桶布局下语义保持不变。
func TestLocalFileKV_CrossProcessReadBack(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "warm.json"), nil, 0o644), "a placeholder keeps the dir warm")

	kv1, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	require.NoError(t, kv1.KVPut("1:evt:a", "v1"))
	require.NoError(t, kv1.KVPut("2:meta:b", "v2"))
	require.NoError(t, kv1.KVPut("global:c", "v3"))
	require.NoError(t, kv1.Close(), "Close flushes every dirty bucket")

	kv2, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	defer kv2.Close()
	v, err := kv2.KVGet("1:evt:a")
	require.NoError(t, err)
	require.Equal(t, "v1", v)
	v, err = kv2.KVGet("2:meta:b")
	require.NoError(t, err)
	require.Equal(t, "v2", v)
	v, err = kv2.KVGet("global:c")
	require.NoError(t, err)
	require.Equal(t, "v3", v)
	require.Equal(t, []int{1, 2}, kv2.ListPartitionIDs())
}

// TestLocalFileKV_LegacySingleSnapshotIgnored 钉住旧 kv.json 不迁移。
// - 存在即忽略（不装载、不报错）；pre-release 冷启动重建是声明的姿态。
func TestLocalFileKV_LegacySingleSnapshotIgnored(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kv.json"),
		[]byte(`{"7:evt:old":"legacy-value"}`), 0o644))

	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	defer kv.Close()

	require.Empty(t, kv.ListPartitionIDs(), "the legacy kv.json must not load as partitions")
	_, err = kv.KVGet("7:evt:old")
	require.Error(t, err, "legacy keys are gone by design (no migration)")
}
