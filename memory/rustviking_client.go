package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// ==================== RustVikingClient ====================
//
// RustVikingClient 封装对 rustviking CLI 的调用。
// 通过 exec.Cmd 执行 rustviking 二进制，传递 JSON 格式输入，解析 JSON 输出。
//
// CLI 接口约定（来自 RustViking）:
//   - 统一 JSON 输出: {"success": true, "data": ...} 或 {"success": false, "error": ...}
//   - 退出码: 0=成功, 1=用户错误, 2=系统错误

// CLIResponse 表示 RustViking CLI 的统一 JSON 响应。
type CLIResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// KVPair 表示一个键值对。
type KVPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KVOp 表示一个批量操作。
type KVOp struct {
	Type  string `json:"op"` // "put" or "delete"
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// RustVikingClient 封装对 rustviking CLI 的调用。
type RustVikingClient struct {
	binaryPath string // rustviking 二进制路径（默认 "rustviking"）
	configPath string // 配置文件路径
	jsonOutput bool   // 是否启用 JSON 输出
}

// NewRustVikingClient 创建 RustVikingClient。
// binaryPath: rustviking 二进制路径（空值使用 "rustviking"）
// configPath: 配置文件路径（config.toml），用于指定存储目录等设置
func NewRustVikingClient(binaryPath, configPath string) *RustVikingClient {
	if binaryPath == "" {
		binaryPath = "rustviking"
	}
	return &RustVikingClient{
		binaryPath: binaryPath,
		configPath: configPath,
		jsonOutput: true,
	}
}

// buildArgs 构建 CLI 参数。
func (c *RustVikingClient) buildArgs(subcommand string, extraArgs ...string) []string {
	args := []string{c.binaryPath}
	if c.configPath != "" {
		args = append(args, "-c", c.configPath)
	}
	if c.jsonOutput {
		args = append(args, "-o", "json")
	}
	subArgs := strings.Split(subcommand, " ")
	args = append(args, subArgs...)
	args = append(args, extraArgs...)
	return args
}

// run 执行 CLI 命令并解析 JSON 输出。
func (c *RustVikingClient) run(args []string) (*CLIResponse, error) {
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.Output()
	if err != nil {
		// 尝试获取 stderr 以获取更多错误信息
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("rustviking CLI failed (exit %d): %s",
				exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("rustviking CLI execution failed: %w", err)
	}

	var resp CLIResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse rustviking response: %w (output: %s)", err, string(output))
	}

	if !resp.Success {
		return &resp, fmt.Errorf("rustviking error: %s", resp.Error)
	}

	return &resp, nil
}

// runWithStdin 执行 CLI 命令并通过 stdin 传入数据。
func (c *RustVikingClient) runWithStdin(args []string, stdinData []byte) (*CLIResponse, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(stdinData)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("rustviking CLI failed (exit %d): %s",
				exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("rustviking CLI execution failed: %w", err)
	}

	var resp CLIResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse rustviking response: %w (output: %s)", err, string(output))
	}

	if !resp.Success {
		return &resp, fmt.Errorf("rustviking error: %s", resp.Error)
	}

	return &resp, nil
}

// ==================== KV 操作 ====================

// KVPut 写入单个 KV。
func (c *RustVikingClient) KVPut(key, value string) error {
	args := c.buildArgs("kv put", "-k", key, "-v", value)
	_, err := c.run(args)
	return err
}

// KVGet 获取单个 KV 的值。
// 注意: rustviking CLI 在 key 不存在时返回 null value（而非错误）。
func (c *RustVikingClient) KVGet(key string) (string, error) {
	args := c.buildArgs("kv get", "-k", key)
	resp, err := c.run(args)
	if err != nil {
		return "", err
	}
	// rustviking 返回 {"key": "...", "value": VALUE}，VALUE 可能是 null 或字符串
	var result struct {
		Key   string  `json:"key"`
		Value *string `json:"value"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("failed to parse KV get response: %w", err)
	}
	if result.Value == nil {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return *result.Value, nil
}

// KVDelete 删除单个 KV。
func (c *RustVikingClient) KVDelete(key string) error {
	args := c.buildArgs("kv del", "-k", key)
	_, err := c.run(args)
	return err
}

// KVScan 前缀扫描。
func (c *RustVikingClient) KVScan(prefix string, limit int) ([]KVPair, error) {
	args := c.buildArgs("kv scan", "-p", prefix)
	if limit > 0 {
		args = append(args, "-l", fmt.Sprintf("%d", limit))
	}
	resp, err := c.run(args)
	if err != nil {
		return nil, err
	}
	// rustviking 返回嵌套结构: {"count": N, "entries": [...], "prefix": ...}
	var scanResult struct {
		Count   int      `json:"count"`
		Entries []KVPair `json:"entries"`
	}
	if err := json.Unmarshal(resp.Data, &scanResult); err != nil {
		return nil, fmt.Errorf("failed to parse KV scan results: %w", err)
	}
	return scanResult.Entries, nil
}

// KVRange 范围扫描。
// 注意: rustviking CLI 不直接支持 range 操作，使用 KVScan 扫描公共前缀后过滤。
func (c *RustVikingClient) KVRange(start, end string, limit int) ([]KVPair, error) {
	// 用 start 和 end 的最长公共前缀扫描
	prefix := longestCommonPrefix(start, end)
	if prefix == "" {
		// 无公共前缀，回退到空前缀扫描（全量扫描，慎用）
		return nil, fmt.Errorf("KVRange requires non-empty common prefix between start and end")
	}
	pairs, err := c.KVScan(prefix, 0)
	if err != nil {
		return nil, err
	}
	// 客户端过滤：只保留 start <= key < end 的结果
	var filtered []KVPair
	for _, p := range pairs {
		if p.Key >= start && (end == "" || p.Key < end) {
			filtered = append(filtered, p)
		}
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

// longestCommonPrefix 返回两个字符串的最长公共前缀。
func longestCommonPrefix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	i := 0
	for i < minLen && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// KVBatch 批量写入（通过 stdin pipe）。
// rustviking 期望 JSON 格式: [{"op":"put","key":"k1","value":"v1"},{"op":"delete","key":"k2"}]
func (c *RustVikingClient) KVBatch(ops []KVOp) error {
	data, err := json.Marshal(ops)
	if err != nil {
		return fmt.Errorf("failed to marshal batch ops: %w", err)
	}
	args := c.buildArgs("kv batch", "-f", "-")
	_, err = c.runWithStdin(args, data)
	return err
}

// ==================== 向量操作（预留） ====================

// VectorInsert 插入向量。
func (c *RustVikingClient) VectorInsert(id uint64, vector []float32, level uint8) error {
	vecJSON, err := json.Marshal(vector)
	if err != nil {
		return fmt.Errorf("failed to marshal vector: %w", err)
	}
	args := c.buildArgs("vector insert",
		"-i", fmt.Sprintf("%d", id),
		"-v", string(vecJSON),
		"-l", fmt.Sprintf("%d", level))
	_, err = c.run(args)
	return err
}

// VectorSearch 语义搜索。
func (c *RustVikingClient) VectorSearch(query []float32, k int) ([]uint64, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}
	args := c.buildArgs("vector search",
		"-q", string(queryJSON),
		"-k", fmt.Sprintf("%d", k))
	resp, err := c.run(args)
	if err != nil {
		return nil, err
	}
	var ids []uint64
	if err := json.Unmarshal(resp.Data, &ids); err != nil {
		return nil, fmt.Errorf("failed to parse vector search results: %w", err)
	}
	return ids, nil
}

// ==================== Embedding（预留） ====================

// Embed 文本转向量。
func (c *RustVikingClient) Embed(texts []string) ([][]float32, error) {
	textJSON, err := json.Marshal(texts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal texts: %w", err)
	}
	args := c.buildArgs("embed", "-t", string(textJSON))
	resp, err := c.run(args)
	if err != nil {
		return nil, err
	}
	var embeddings [][]float32
	if err := json.Unmarshal(resp.Data, &embeddings); err != nil {
		return nil, fmt.Errorf("failed to parse embeddings: %w", err)
	}
	return embeddings, nil
}

// ==================== Mock Client（测试用） ====================

// MockRustVikingClient 是 RustVikingClient 的内存 mock，用于开发和测试。
type MockRustVikingClient struct {
	mu   sync.Mutex
	data map[string]string
}

// NewMockRustVikingClient 创建 MockRustVikingClient。
func NewMockRustVikingClient() *MockRustVikingClient {
	return &MockRustVikingClient{
		data: make(map[string]string),
	}
}

func (m *MockRustVikingClient) KVPut(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *MockRustVikingClient) KVGet(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return value, nil
}

func (m *MockRustVikingClient) KVDelete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *MockRustVikingClient) KVScan(prefix string, limit int) ([]KVPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []KVPair
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			results = append(results, KVPair{Key: k, Value: v})
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func (m *MockRustVikingClient) KVRange(start, end string, limit int) ([]KVPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var results []KVPair
	for k, v := range m.data {
		if k >= start && k < end {
			results = append(results, KVPair{Key: k, Value: v})
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func (m *MockRustVikingClient) KVBatch(ops []KVOp) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, op := range ops {
		switch op.Type {
		case "put":
			m.data[op.Key] = op.Value
		case "delete":
			delete(m.data, op.Key)
		}
	}
	return nil
}

// KVStore 接口抽象了 RustViking KV 操作，便于测试时替换。
type KVStore interface {
	KVPut(key, value string) error
	KVGet(key string) (string, error)
	KVDelete(key string) error
	KVScan(prefix string, limit int) ([]KVPair, error)
	KVRange(start, end string, limit int) ([]KVPair, error)
	KVBatch(ops []KVOp) error
}
