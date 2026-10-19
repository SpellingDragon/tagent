// 职责：以锁保护的顺序票据 map 保存事件原文，供 Recall 取回与 JSON 快照恢复。
// 未实现：落盘持久化、索引、淘汰策略、语义检索。
// 接线：config 装配 recall 工具与 ResultMemoryPlugin；CompactResults 依 memory_key 折叠。
// 契约: docs/wiki/agent/prototype-skeleton.md#six-pieces
package prototype

import (
	"context"
	"errors"
	"strconv"
	"sync"
)

// ErrMemoryMiss 表示按键未命中原文。
var ErrMemoryMiss = errors.New("memory: miss")

// Memory 是按顺序票据索引原文的并发安全表。
type Memory struct {
	mu   sync.Mutex
	seq  int
	data map[string]Event
}

// NewMemory 建立空表。
func NewMemory() *Memory { return &Memory{data: map[string]Event{}} }

// Put 存入事件副本并返回顺序票据，外部改动不影响表内。
func (m *Memory) Put(e Event) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	key := strconv.Itoa(m.seq)
	m.data[key] = cloneEvent(e)
	return key
}

// Recall 按键返回原文；缺键返回 ErrMemoryMiss。
func (m *Memory) Recall(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.data[key]
	if !ok {
		return "", ErrMemoryMiss
	}
	return e.Data, nil
}

// Snapshot 深拷贝原文表供 JSON 序列化。
func (m *Memory) Snapshot() map[string]Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Event, len(m.data))
	for k, v := range m.data {
		out[k] = cloneEvent(v)
	}
	return out
}

// Restore 载入原文表并按最大票据续号。
func (m *Memory) Restore(snap map[string]Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string]Event, len(snap))
	m.seq = 0
	for k, v := range snap {
		m.data[k] = cloneEvent(v)
		if n, err := strconv.Atoi(k); err == nil && n > m.seq {
			m.seq = n
		}
	}
}

// cloneEvent 复制事件与 Meta map，隔离外部可变状态。
func cloneEvent(e Event) Event {
	if e.Meta != nil {
		meta := make(map[string]string, len(e.Meta))
		for k, v := range e.Meta {
			meta[k] = v
		}
		e.Meta = meta
	}
	return e
}
