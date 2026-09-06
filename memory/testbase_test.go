package memory

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// testBaseMs 是记忆包测试共用的固定基准毫秒时间戳（确定性 EventKey 生成）。
// memory/engine 子包测试持有一份同名副本（Go 测试不跨包共享 helper）。
const testBaseMs = int64(1750000000000)

// TypeExternalInputProbe 是测试专用的探针事件类型（不进生产类型注册表）。
const TypeExternalInputProbe = "external_input"

// mockKV 是核心包测试共用的内存 KVStore 假件（替代已迁至 memory/kv 的
// LocalFileKV/RustVikingClient——白盒测试不得 import 子包）。分层/压实/TTL
// 语义测试不依赖落盘细节；落盘持久化语义的测试位于 memory/kv 子包。
type mockKV struct {
	mu   sync.Mutex
	data map[string]string
}

func newMockKV() *mockKV { return &mockKV{data: map[string]string{}} }

func (m *mockKV) KVPut(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockKV) KVGet(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return v, nil
}

func (m *mockKV) KVDelete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockKV) KVScan(prefix string, limit int) ([]KVPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []KVPair
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, KVPair{Key: k, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mockKV) KVRange(start, end string, limit int) ([]KVPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []KVPair
	for k, v := range m.data {
		if k >= start && (end == "" || k < end) {
			out = append(out, KVPair{Key: k, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mockKV) KVBatch(ops []KVOp) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, op := range ops {
		if op.Type == "delete" {
			delete(m.data, op.Key)
		} else {
			m.data[op.Key] = op.Value
		}
	}
	return nil
}

func (m *mockKV) Close() error { return nil }
