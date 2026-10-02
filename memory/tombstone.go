package memory

import (
	"encoding/json"
	"fmt"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// TombstoneSet 记录已被合法删除（遗忘）的事件键：内存驻留并持久化到 KV，以便崩溃后重建，
// 且保证回放不会复活被遗忘的事实。删除时的级联父引用修复顺序是承重的，详见文档。
//
// 契约: docs/wiki/memory/memory-architecture.md#tombstone
type TombstoneSet struct {
	mu sync.RWMutex
	// keys 是已墓碑化的事件键集合。
	keys map[int64]bool
	// rel 用于级联修复子事件的父引用。
	rel RelationStore
	// kv 为 nil 时仅内存生效（合法的内存模式）。
	kv KVStore
	// pid 决定墓碑键的持久化命名空间。
	pid int
	// dirty 表示未落盘变更。
	dirty bool
}

// NewTombstoneSet 构造墓碑集；kv 可为 nil（仅内存），rel 必须可用以做级联修复。
func NewTombstoneSet(rel RelationStore, kv KVStore, pid int) *TombstoneSet {
	return &TombstoneSet{
		keys: make(map[int64]bool),
		rel:  rel,
		kv:   kv,
		pid:  pid,
	}
}

// MarkTombstone 标记事件为已遗忘，回放据此必须拒绝复活它。
// 承重约束：子事件的父引用级联必须发生在墓碑落账之后，反序会让级联把正在被删的键误当作
// 存活祖先；关系操作的局部失败只记日志、不回滚遗忘——遗忘本身必须生效。
func (ts *TombstoneSet) MarkTombstone(key int64) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}

	ts.mu.Lock()
	ts.keys[key] = true
	ts.dirty = true
	ts.mu.Unlock()
	children, err := ts.rel.GetChildren(key)
	if err != nil {
		log.Errorf("[Tombstone] GetChildren failed key=%d: %v", key, err)
	}

	if len(children) > 0 {
		ancestor := ts.findAliveAncestor(key)
		for _, child := range children {
			if ancestor != 0 {
				if err := ts.rel.SetParent(child, ancestor); err != nil {
					log.Errorf("[Tombstone] SetParent failed child=%d ancestor=%d: %v", child, ancestor, err)
				}
			} else {
				if err := ts.rel.SetParent(child, 0); err != nil {
					log.Errorf("[Tombstone] SetParent root failed child=%d: %v", child, err)
				}
			}
		}
	}
	if err := ts.rel.RemoveRelations(key); err != nil {
		log.Errorf("[Tombstone] RemoveRelations failed key=%d: %v", key, err)
	}
	return ts.persistKey(key)
}

// IsTombstone 报告键是否已被合法遗忘（回放据此拒绝复活）。
func (ts *TombstoneSet) IsTombstone(key int64) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.keys[key]
}

// RemoveTombstones 在压实吸收墓碑后成批移除内存与 KV 键，避免墓碑只增不减。
func (ts *TombstoneSet) RemoveTombstones(keys []int64) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	for _, key := range keys {
		delete(ts.keys, key)
	}

	if ts.kv != nil {
		batchOps := make([]KVOp, 0, len(keys))
		for _, key := range keys {
			tombKVKey := TombstoneKeyStr(ts.pid, key)
			batchOps = append(batchOps, KVOp{Type: "delete", Key: tombKVKey})
		}
		if len(batchOps) > 0 {
			return ts.kv.KVBatch(batchOps)
		}
	}
	return nil
}

// AllTombstones 返回全部墓碑键（顺序无关）。
func (ts *TombstoneSet) AllTombstones() []int64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	keys := make([]int64, 0, len(ts.keys))
	for k := range ts.keys {
		keys = append(keys, k)
	}
	return keys
}

// Count 返回墓碑键数量。
func (ts *TombstoneSet) Count() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.keys)
}

// Snapshot 返回可序列化的墓碑键副本（不暴露内部映射）。
func (ts *TombstoneSet) Snapshot() (map[int64]bool, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	snap := make(map[int64]bool, len(ts.keys))
	for k := range ts.keys {
		snap[k] = true
	}
	return snap, nil
}

// LoadSnapshot 从快照恢复并清除脏标记。
func (ts *TombstoneSet) LoadSnapshot(data map[int64]bool) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.keys = make(map[int64]bool, len(data))
	for k := range data {
		ts.keys[k] = true
	}
	ts.dirty = false
	return nil
}

// RecoverFromKV 启动时按 tomb 前缀扫描重建墓碑集合，忽略非本类型键与零键。
func (ts *TombstoneSet) RecoverFromKV() error {
	if ts.kv == nil {
		return nil
	}

	tombPrefix := TombstonePrefix(ts.pid)
	pairs, err := ts.kv.KVScan(tombPrefix, 0)
	if err != nil {
		return err
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	for _, pair := range pairs {
		pk, err := ParseKey(pair.Key)
		if err != nil || pk.KeyType != "tomb" {
			continue
		}
		if pk.EventKey != 0 {
			ts.keys[pk.EventKey] = true
		}
	}
	ts.dirty = false
	return nil
}

// IsDirty 报告是否存在未落盘的墓碑变更。
func (ts *TombstoneSet) IsDirty() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.dirty
}

// persistKey 落盘单个墓碑键；kv 为 nil 时跳过（仅内存模式）。
func (ts *TombstoneSet) persistKey(key int64) error {
	if ts.kv == nil {
		return nil
	}
	tombKVKey := TombstoneKeyStr(ts.pid, key)
	return ts.kv.KVPut(tombKVKey, "1")
}

// findAliveAncestor 沿父链找最近的存活（未墓碑化）祖先；带访问集合，父链成环也不会死循环。
func (ts *TombstoneSet) findAliveAncestor(key int64) int64 {
	visited := make(map[int64]bool)
	current := key
	for current != 0 && !visited[current] {
		visited[current] = true
		if !ts.IsTombstone(current) {
			return current
		}
		parent, err := ts.rel.GetParent(current)
		if err != nil {
			return 0
		}
		current = parent
	}
	return 0
}

// TombstoneSnapshot 是墓碑集的持久化快照形态。
type TombstoneSnapshot struct {
	Keys []int64 `json:"keys"`
}

// MarshalJSON 序列化当前墓碑键集合。
func (ts *TombstoneSet) MarshalJSON() ([]byte, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	snap := TombstoneSnapshot{
		Keys: make([]int64, 0, len(ts.keys)),
	}
	for k := range ts.keys {
		snap.Keys = append(snap.Keys, k)
	}
	return json.Marshal(snap)
}

// UnmarshalJSON 从快照恢复墓碑键集合（覆盖现有内容）。
func (ts *TombstoneSet) UnmarshalJSON(data []byte) error {
	var snap TombstoneSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.keys = make(map[int64]bool, len(snap.Keys))
	for _, k := range snap.Keys {
		ts.keys[k] = true
	}
	return nil
}
