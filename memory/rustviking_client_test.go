package memory

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRustVikingClient_New tests client creation.
func TestRustVikingClient_New(t *testing.T) {
	client := NewRustVikingClient("", "/tmp/test-rustviking/config.toml")
	require.NotNil(t, client)
	assert.Equal(t, "rustviking", client.binaryPath)
	assert.Equal(t, "/tmp/test-rustviking/config.toml", client.configPath)

	client2 := NewRustVikingClient("/usr/local/bin/rv", "/data/kv/config.toml")
	assert.Equal(t, "/usr/local/bin/rv", client2.binaryPath)
	assert.Equal(t, "/data/kv/config.toml", client2.configPath)
}

// TestCLIResponse_JSON tests CLI response parsing.
func TestCLIResponse_JSON(t *testing.T) {
	successJSON := `{"success":true,"data":{"key":"test","value":"hello"}}`
	var resp CLIResponse
	err := json.Unmarshal([]byte(successJSON), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.NotNil(t, resp.Data)

	errorJSON := `{"success":false,"error":"key not found"}`
	err = json.Unmarshal([]byte(errorJSON), &resp)
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Equal(t, "key not found", resp.Error)
}

// TestKVPair_JSON tests KVPair JSON serialization.
func TestKVPair_JSON(t *testing.T) {
	pair := KVPair{Key: "mykey", Value: "myvalue"}
	data, err := json.Marshal(pair)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"key":"mykey"`)
	assert.Contains(t, string(data), `"value":"myvalue"`)
}

// TestKVOp_JSON tests KVOp JSON serialization.
func TestKVOp_JSON(t *testing.T) {
	putOp := KVOp{Type: "put", Key: "testkey", Value: "testvalue"}
	data, err := json.Marshal(putOp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"op":"put"`)

	deleteOp := KVOp{Type: "delete", Key: "todelete"}
	data, err = json.Marshal(deleteOp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"op":"delete"`)
}

// TestMockRustVikingClient_Interface tests that MockRustVikingClient satisfies the KVStore interface.
func TestMockRustVikingClient_Interface(t *testing.T) {
	mock := NewMockRustVikingClient()
	require.NotNil(t, mock)

	err := mock.KVPut("testkey", "testvalue")
	require.NoError(t, err)

	val, err := mock.KVGet("testkey")
	require.NoError(t, err)
	assert.Equal(t, "testvalue", val)

	_, err = mock.KVGet("nonexistent")
	assert.Error(t, err)

	// KVScan with matching prefix
	err = mock.KVPut("prefix:k1", "v1")
	require.NoError(t, err)
	err = mock.KVPut("prefix:k2", "v2")
	require.NoError(t, err)

	pairs, err := mock.KVScan("prefix:", 10)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)

	// KVRange
	pairs, err = mock.KVRange("prefix:", "prefiy", 10)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)

	// KVBatch
	err = mock.KVBatch([]KVOp{
		{Type: "put", Key: "bk1", Value: "bv1"},
		{Type: "put", Key: "bk2", Value: "bv2"},
	})
	require.NoError(t, err)

	// KVDelete
	err = mock.KVDelete("testkey")
	require.NoError(t, err)
	_, err = mock.KVGet("testkey")
	assert.Error(t, err)
}

// TestMockRustVikingClient_Concurrency tests concurrent access safety.
func TestMockRustVikingClient_Concurrency(t *testing.T) {
	mock := NewMockRustVikingClient()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "concurrent-key-" + strconv.Itoa(n)
			_ = mock.KVPut(key, "value")
			_, _ = mock.KVGet(key)
		}(i)
	}
	wg.Wait()
}

// findRustVikingBinary 查找可用的 rustviking 二进制路径。
func findRustVikingBinary() string {
	// 按优先级查找
	candidates := []string{
		"/Users/pengweiye/Documents/codes/rustviking/target/release/rustviking",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// writeRustVikingConfig 写入临时 rustviking 配置文件。
func writeRustVikingConfig(t *testing.T, dataDir string) string {
	t.Helper()
	configContent := `[storage]
path = "` + filepath.Join(dataDir, "rocksdb") + `"
create_if_missing = true
max_open_files = 100

[vector_store]
plugin = "memory"

[embedding]
plugin = "mock"
`
	configPath := filepath.Join(dataDir, "rustviking.toml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)
	return configPath
}

// TestRustVikingClient_Integration 测试 RustVikingClient 与实际 rustviking 二进制集成。
// 若二进制不可用则跳过。
func TestRustVikingClient_Integration(t *testing.T) {
	binary := findRustVikingBinary()
	if binary == "" {
		t.Skip("rustviking binary not found, skipping integration test")
	}

	// Verify binary is executable
	cmd := exec.Command(binary, "--help")
	err := cmd.Run()
	require.NoError(t, err, "rustviking binary must be executable")

	dir := t.TempDir()
	configPath := writeRustVikingConfig(t, dir)

	client := NewRustVikingClient(binary, configPath)
	require.NotNil(t, client)

	t.Run("PutAndGet", func(t *testing.T) {
		err := client.KVPut("hello", "world")
		require.NoError(t, err)

		val, err := client.KVGet("hello")
		require.NoError(t, err)
		assert.Equal(t, "world", val)
	})

	t.Run("GetNotFound", func(t *testing.T) {
		_, err := client.KVGet("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Scan", func(t *testing.T) {
		_ = client.KVPut("a:1", "v1")
		_ = client.KVPut("a:2", "v2")
		_ = client.KVPut("b:1", "v3")

		pairs, err := client.KVScan("a:", 10)
		require.NoError(t, err)
		assert.Len(t, pairs, 2)
	})

	t.Run("Delete", func(t *testing.T) {
		err := client.KVPut("todelete", "value")
		require.NoError(t, err)

		_, err = client.KVGet("todelete")
		require.NoError(t, err)

		err = client.KVDelete("todelete")
		require.NoError(t, err)

		_, err = client.KVGet("todelete")
		assert.Error(t, err)
	})

	t.Run("Batch", func(t *testing.T) {
		err := client.KVBatch([]KVOp{
			{Type: "put", Key: "batch:1", Value: "bv1"},
			{Type: "put", Key: "batch:2", Value: "bv2"},
		})
		require.NoError(t, err)

		val, err := client.KVGet("batch:1")
		require.NoError(t, err)
		assert.Equal(t, "bv1", val)
	})

	t.Run("KVRange", func(t *testing.T) {
		_ = client.KVPut("range:a", "ra")
		_ = client.KVPut("range:b", "rb")
		_ = client.KVPut("range:c", "rc")

		pairs, err := client.KVRange("range:a", "range:c", 10)
		require.NoError(t, err)
		// Should include range:a, range:b but NOT range:c (exclusive end)
		assert.Len(t, pairs, 2)
	})
}
