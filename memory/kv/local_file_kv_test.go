package kv

import (
	"github.com/SpellingDragon/tagent/memory"

	"os"
	"path/filepath"
	"testing"

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

	_, snapErr := os.Stat(filepath.Join(dir, "kv.json"))
	_, walErr := os.Stat(filepath.Join(dir, "kv.wal.jsonl"))
	require.True(t, snapErr == nil || walErr == nil, "neither snapshot nor WAL exists after Close")

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
