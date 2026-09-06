package memory

import (
	"errors"
	"path/filepath"
	"testing"
)

// flakyStore 首次可配置失败、后成功（模拟 memory 退化→恢复）。
type flakyStore struct {
	*InMemoryStore
	fail bool
}

func (f *flakyStore) StoreEvent(k int64, ev FullEvent) error {
	if f.fail {
		return errors.New("memory degraded")
	}
	return f.InMemoryStore.StoreEvent(k, ev)
}

// failStore 恒失败（验证重放部分失败保留）。
type failStore struct {
	*InMemoryStore
}

func (f *failStore) StoreEvent(int64, FullEvent) error { return errors.New("still failing") }

// falseNegativeStore 模拟 W1 假阴性：首次 StoreEvent 实际写入成功但返回 error（如 KV 已写但
// rustviking CLI 响应解析失败），且拒绝重复 key 重写（同 FileSegmentStore "already exists" 守卫）。
type falseNegativeStore struct {
	*InMemoryStore
	written map[int64]bool
}

func (f *falseNegativeStore) StoreEvent(k int64, ev FullEvent) error {
	if f.written[k] {
		return errors.New("event key already exists (snowflake collision?): refusing to overwrite")
	}
	if f.written == nil {
		f.written = map[int64]bool{}
	}
	f.written[k] = true
	_ = f.InMemoryStore.StoreEvent(k, ev) // 实际写入成功
	return errors.New("rustviking: response parse failed (KV already written)") // 假阴性
}

// TestMemSpill_ReplayIdempotentFalseNegative 是 W1（§8.3）回归：假阴性失败（KV 已写但
// StoreEvent 误报 error）落 spill 后，重放必须收敛——GetEvent 预检命中即计成功移除，否则撞
// FileSegmentStore "already exists" 守卫致 spill 永久滞留、恢复每次失败。
func TestMemSpill_ReplayIdempotentFalseNegative(t *testing.T) {
	sp := NewMemSpill(filepath.Join(t.TempDir(), "w1.jsonl"))
	store := &falseNegativeStore{InMemoryStore: NewInMemoryStore()}
	k := NewSnowflakeEventKey(1, testBaseMs)
	ev := FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Timestamp: testBaseMs}
	// 首次 StoreEvent 假阴性失败（实际已写）→ 落盘兜底。
	if err := store.StoreEvent(k, ev); err == nil {
		t.Fatal("应假阴性失败")
	}
	if err := sp.Append(k, ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if sp.Len() != 1 {
		t.Fatalf("应落盘 1, got %d", sp.Len())
	}
	// 重放：GetEvent 预检命中（假阴性实已写）→ 幂等计成功，spill 归零（收敛，不撞 already-exists）。
	n, err := sp.Replay(store)
	if err != nil || n != 1 {
		t.Fatalf("W1: 假阴性重放应收敛(预检幂等), got n=%d err=%v", n, err)
	}
	if sp.Len() != 0 {
		t.Fatalf("W1: 重放后 spill 应归零(不永久滞留), got %d", sp.Len())
	}
}

func TestMemSpill_AppendReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem_spill.jsonl")
	sp := NewMemSpill(path)
	store := NewInMemoryStore()
	for i := 0; i < 3; i++ {
		k := NewSnowflakeEventKey(1, testBaseMs+int64(i))
		if err := sp.Append(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "spilled", Timestamp: testBaseMs}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if sp.Len() != 3 {
		t.Fatalf("应 3 兜底, got %d", sp.Len())
	}
	n, err := sp.Replay(store)
	if err != nil || n != 3 {
		t.Fatalf("Replay 应重放 3, got n=%d err=%v", n, err)
	}
	if sp.Len() != 0 {
		t.Fatalf("重放后应清空, got %d", sp.Len())
	}
	refs, _ := store.QueryEvents(QueryOptions{PartitionIDs: []int{1}})
	if len(refs) != 3 {
		t.Fatalf("重放后 store 应 3 事件, got %d", len(refs))
	}
}

func TestMemSpill_NilPathDisabled(t *testing.T) {
	if sp := NewMemSpill(""); sp != nil {
		t.Fatal("空 path 应返回 nil（禁用）")
	}
	var sp *MemSpill
	if err := sp.Append(1, FullEvent{}); err != nil {
		t.Fatal("nil Append 应 no-op 无错")
	}
	if n, _ := sp.Replay(NewInMemoryStore()); n != 0 {
		t.Fatal("nil Replay 应 0")
	}
	if sp.Len() != 0 {
		t.Fatal("nil Len 应 0")
	}
}

func TestMemSpill_ReplayPartialFailureRetains(t *testing.T) {
	sp := NewMemSpill(filepath.Join(t.TempDir(), "s.jsonl"))
	for i := 0; i < 2; i++ {
		k := NewSnowflakeEventKey(1, testBaseMs+int64(i))
		_ = sp.Append(k, FullEvent{EventKey: k, PartitionID: 1, Timestamp: testBaseMs})
	}
	// 恒失败 store 重放 → 全保留（不丢，待下次）。
	n, _ := sp.Replay(&failStore{InMemoryStore: NewInMemoryStore()})
	if n != 0 {
		t.Fatalf("恒失败 store 应重放 0, got %d", n)
	}
	if sp.Len() != 2 {
		t.Fatalf("重放失败应保留 2, got %d", sp.Len())
	}
}

// TestErrorTrackingStore_SpillAndReplay 验证步4 端到端：StoreEvent 失败 → 落盘兜底；
// inner 恢复后 ReplaySpilled → 重放回灌（事件不丢，at-least-once 延伸到存储层）。
func TestErrorTrackingStore_SpillAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ets_spill.jsonl")
	inner := &flakyStore{InMemoryStore: NewInMemoryStore(), fail: true}
	ets := NewErrorTrackingStore(inner, nil)
	ets.SetMemSpill(path)

	k := NewSnowflakeEventKey(1, testBaseMs)
	ev := FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "x", Timestamp: testBaseMs}
	// inner 失败 → 返回 err + 落盘兜底。
	if err := ets.StoreEvent(k, ev); err == nil {
		t.Fatal("inner 失败应返回 err")
	}
	if ets.MemSpillLen() != 1 {
		t.Fatalf("StoreEvent 失败应落盘 1 兜底, got %d", ets.MemSpillLen())
	}
	// inner 恢复 → 重放回灌。
	inner.fail = false
	n, err := ets.ReplaySpilled()
	if err != nil || n != 1 {
		t.Fatalf("重放应 1, got n=%d err=%v", n, err)
	}
	if ets.MemSpillLen() != 0 {
		t.Fatalf("重放后兜底应清空, got %d", ets.MemSpillLen())
	}
	if got, _ := inner.GetEvent(k); got == nil {
		t.Fatal("重放后 inner 应有该事件（回灌成功，事件不丢）")
	}
}

func TestErrorTrackingStore_NoSpillConfigured(t *testing.T) {
	// 未 SetMemSpill → StoreEvent 失败不落盘（MemSpillLen 0），仅上报（现状兼容）。
	inner := &flakyStore{InMemoryStore: NewInMemoryStore(), fail: true}
	ets := NewErrorTrackingStore(inner, nil)
	k := NewSnowflakeEventKey(1, testBaseMs)
	_ = ets.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, Timestamp: testBaseMs})
	if ets.MemSpillLen() != 0 {
		t.Fatal("未配 mem_spill 时不应落盘")
	}
	if n, _ := ets.ReplaySpilled(); n != 0 {
		t.Fatal("未配 mem_spill 时 ReplaySpilled 应 0")
	}
}
