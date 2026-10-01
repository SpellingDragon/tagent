// 本文件钉住 静默错误清零的四处回归：压实 idx 删除失败保墓碑、删段扫描失败聚合
// 上抛、孤儿定位列表失败 fail-loud、spill 释放在重写落盘之后恰好一次。
package memory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// batchFailKV makes every KVBatch call fail — the shape of a transient
// backend error during a compaction removal.
type batchFailKV struct {
	*mockKV
}

func (b *batchFailKV) KVBatch(ops []KVOp) error {
	return errors.New("transient batch failure (review repro)")
}

// TestFinalizeTombstones_IdxRemovalFailureKeepsTombstone 钉住 idx 删除没落地时
// 墓碑必须保留：它既是下轮幂等重试的记号，也是 ErrEventForgotten 复活防线；
// 删除失败却清墓碑会同时失去两者。
func TestFinalizeTombstones_IdxRemovalFailureKeepsTombstone(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	kv := newMockKV()
	store, err := NewFileSegmentStore(kv, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, kv, 1)
	store.tombstones = ts

	key := NewSnowflakeEventKey(1, 1704067200000)
	require.NoError(t, ts.MarkTombstone(key))

	c := NewCompactor(store, kv, rel, ts, CompactionConfig{})
	c.kv = &batchFailKV{mockKV: kv}
	c.finalizeTombstones(1, []int64{key})

	require.True(t, ts.IsTombstone(key), "failed idx removal must keep the tombstone (retry marker + resurrection guard)")
	_, _, err = store.ReplayEvent(key, FullEvent{EventKey: key, PartitionID: 1, EventType: "test"})
	require.ErrorIs(t, err, ErrEventForgotten, "the kept tombstone still refuses replay-based resurrection")

	// 幂等重试：kv 恢复后同一 finalize 走完（idx 删除+墓碑清除）。
	c.kv = kv
	c.finalizeTombstones(1, []int64{key})
	require.False(t, ts.IsTombstone(key), "the retried finalize completes the disposition")
}

// TestDeleteSegments_ScanFailureAggregates 钉住 窗口扫描失败不再静默跳过：
// 错误聚合上抛，删除动作对"实际删了什么"诚实。
func TestDeleteSegments_ScanFailureAggregates(t *testing.T) {
	window := int64(1704067200000)
	kv := &scanFailOnceKV{mockKV: newMockKV(), failPrefix: SegmentEventPrefix(1, window)}
	c := NewCompactor(nil, kv, nil, nil, CompactionConfig{})

	err := c.deleteSegments(1, []int64{window})
	require.ErrorContains(t, err, "delete-segments scan")
}

// TestLocateOrphanEvtSlot_SegmentListFailureLoud 钉住 段列表读取失败上抛而非
// 只信 hint 窗口——静默降级会把别的段里的孤儿误报为不存在。
func TestLocateOrphanEvtSlot_SegmentListFailureLoud(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	window := WindowTimestamp(1704067200000, DefaultWindowSize)
	kv := &scanFailOnceKV{mockKV: newMockKV(), failPrefix: MetaPrefix(1)}
	store, err := NewFileSegmentStore(kv, rel, ":memory:", 100)
	require.NoError(t, err)

	_, _, _, err = store.locateOrphanEvtSlot(1, window, NewSnowflakeEventKey(1, 1704067200000))
	require.ErrorContains(t, err, "orphan-evt segment list failed pid=1")
}

// TestMemSpill_ReleaseHoldsUntilRewriteLands 钉住 E-P1-1 的释放时机：重放成功但
// spill 重写（原件移除）失败时，所有键的保留租约必须仍未释放——下一轮的
// AlreadyCommitted 重放会走完整路径并恰好释放一次；提前释放会让重试变成双重
// 释放、递减他人租约（ref 泄漏）。
func TestMemSpill_ReleaseHoldsUntilRewriteLands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spill.jsonl")
	key := NewSnowflakeEventKey(1, 1704067200*1000)
	ev := FullEvent{EventKey: key, PartitionID: 1, EventType: "external_input", Timestamp: 1704067200000}

	g := newCountGuard()
	sp := NewMemSpill(path)
	sp.SetGuard(g)
	require.NoError(t, sp.Append(key, ev))

	replayer := NewInMemoryStore()

	// Block the rewrite tail: the tmp create under a read-only dir must fail.
	require.NoError(t, os.Chmod(dir, 0o555))
	n, err := sp.ReplayWithNotify(replayer, nil)
	require.Error(t, err, "a failed spill rewrite must surface")
	require.Equal(t, 1, n, "the replay itself landed")
	require.Equal(t, 1, g.holders(key), "release must stay booked while the spill still carries the original")
	require.NoError(t, os.Chmod(dir, 0o755))

	// Retry: the replay hits AlreadyCommitted, the rewrite lands, and the key
	// is released EXACTLY once.
	n2, err2 := sp.ReplayWithNotify(replayer, nil)
	require.NoError(t, err2)
	require.Equal(t, 1, n2)
	require.Equal(t, 0, g.holders(key), "the finally-landed removal releases the hold once")

	// A third round sees an empty spill list: nothing re-releases, refs stay at zero.
	n3, err3 := sp.ReplayWithNotify(replayer, nil)
	require.NoError(t, err3)
	require.Zero(t, n3)
	require.Equal(t, 0, g.holders(key), "no double release across rounds")
}

// TestInMemRelationStore_CloseReleasesAndIsIdempotent 钉住 journal fd 的释放契约：
// Close 后重复 Close 为 no-op（幂等），追加路径静默短路（journal 已置 nil）。
// wiring 的构建失败分支依赖这一幂等形——失败路径与 store.Close 可能各调一次。
func TestInMemRelationStore_CloseReleasesAndIsIdempotent(t *testing.T) {
	rel, err := NewInMemRelationStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, rel.SetParent(2, 1))
	require.NoError(t, rel.Close())
	require.NoError(t, rel.Close(), "second Close is a no-op, not an error on a closed fd")

	// 关闭后的写入不炸不泄漏：append 短路为 no-op。
	require.NoError(t, rel.SetParent(3, 1))
}
