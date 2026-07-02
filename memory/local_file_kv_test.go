package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalFileKV_Interface verifies LocalFileKV satisfies the KVStore interface.
func TestLocalFileKV_Interface(t *testing.T) {
	var _ KVStore = (*LocalFileKV)(nil)
}

// TestLocalFileKV_CRUD tests basic put/get/delete operations.
func TestLocalFileKV_CRUD(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	// Put
	err = kv.KVPut("key1", "value1")
	require.NoError(t, err)

	// Get
	val, err := kv.KVGet("key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)

	// Get non-existent
	_, err = kv.KVGet("nonexistent")
	assert.Error(t, err)

	// Delete
	err = kv.KVDelete("key1")
	require.NoError(t, err)

	// Verify deleted
	_, err = kv.KVGet("key1")
	assert.Error(t, err)
}

// TestLocalFileKV_Scan tests prefix scanning with sorting.
func TestLocalFileKV_Scan(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	// Insert keys with different prefixes
	require.NoError(t, kv.KVPut("prefix:k3", "v3"))
	require.NoError(t, kv.KVPut("prefix:k1", "v1"))
	require.NoError(t, kv.KVPut("prefix:k2", "v2"))
	require.NoError(t, kv.KVPut("other:k1", "ov1"))

	// Scan prefix
	pairs, err := kv.KVScan("prefix:", 0)
	require.NoError(t, err)
	assert.Len(t, pairs, 3)
	// Verify sorted
	assert.Equal(t, "prefix:k1", pairs[0].Key)
	assert.Equal(t, "prefix:k2", pairs[1].Key)
	assert.Equal(t, "prefix:k3", pairs[2].Key)

	// Scan with limit
	pairs, err = kv.KVScan("prefix:", 2)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)

	// Scan with no matches
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

	// Range [key:002, key:004) → should return key:002, key:003
	pairs, err := kv.KVRange("key:002", "key:004", 0)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)
	assert.Equal(t, "key:002", pairs[0].Key)
	assert.Equal(t, "key:003", pairs[1].Key)

	// Range with limit
	pairs, err = kv.KVRange("key:001", "key:004", 2)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)
}

// TestLocalFileKV_Batch tests batch operations.
func TestLocalFileKV_Batch(t *testing.T) {
	dir := t.TempDir()
	kv, err := NewLocalFileKV(dir)
	require.NoError(t, err)

	// Batch put
	err = kv.KVBatch([]KVOp{
		{Type: "put", Key: "b1", Value: "v1"},
		{Type: "put", Key: "b2", Value: "v2"},
		{Type: "put", Key: "b3", Value: "v3"},
	})
	require.NoError(t, err)

	// Verify
	val, err := kv.KVGet("b2")
	require.NoError(t, err)
	assert.Equal(t, "v2", val)

	// Batch with delete
	err = kv.KVBatch([]KVOp{
		{Type: "delete", Key: "b1"},
		{Type: "put", Key: "b4", Value: "v4"},
	})
	require.NoError(t, err)

	// Verify b1 deleted
	_, err = kv.KVGet("b1")
	assert.Error(t, err)

	// Verify b4 added
	val, err = kv.KVGet("b4")
	require.NoError(t, err)
	assert.Equal(t, "v4", val)
}

// TestLocalFileKV_Persistence verifies data survives across instances.
func TestLocalFileKV_Persistence(t *testing.T) {
	dir := t.TempDir()

	// First instance: write data
	kv1, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	require.NoError(t, kv1.KVPut("persist:key1", "value1"))
	require.NoError(t, kv1.KVPut("persist:key2", "value2"))

	// Force flush to disk (deferred flush is async)
	require.NoError(t, kv1.Sync())
	require.NoError(t, kv1.Close())

	// Verify kv.json exists
	kvPath := filepath.Join(dir, "kv.json")
	_, err = os.Stat(kvPath)
	require.NoError(t, err)

	// Second instance: should load existing data
	kv2, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	defer kv2.Close()

	val, err := kv2.KVGet("persist:key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)

	val, err = kv2.KVGet("persist:key2")
	require.NoError(t, err)
	assert.Equal(t, "value2", val)

	// Verify scan works on restored data
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

	// Should be able to put and get normally
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
	// Writer goroutine
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = kv.KVPut("concurrent", "value")
		}
	}()

	// Reader goroutine (runs concurrently)
	for i := 0; i < 100; i++ {
		_, _ = kv.KVScan("concurrent", 0)
	}

	<-done
}
