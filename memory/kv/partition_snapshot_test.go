// 本文件钉住 分区快照布局的三条契约：Sync 只提交脏桶（写放大与全库规模解耦）、
// 跨进程读回全量语义、旧 kv.json 不迁移且被忽略。
package kv

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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

	// 仅分区 2 有新写入：下一次 Sync 只有 kv-2.json 重写，kv-1.json 不动——
	// 单次提交的落盘成本只由所属分区键数决定（旧全库单快照在此线性放大）。
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

// TestLocalFileKV_CrossProcessReadBack 钉住 子进程 Sync 后退出，新进程读回
// 全部分区键值——快照屏障的跨进程可见性语义在多桶布局下保持不变。
func TestLocalFileKV_CrossProcessReadBack(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "legacy.json"), nil, 0o644)) // placeholder to keep dir warm

	kv1, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	require.NoError(t, kv1.KVPut("1:evt:a", "v1"))
	require.NoError(t, kv1.KVPut("2:meta:b", "v2"))
	require.NoError(t, kv1.KVPut("global:c", "v3"))
	require.NoError(t, kv1.Close()) // Close 落盘全部脏桶

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

// TestLocalFileKV_LegacySingleSnapshotIgnored 钉住 旧 kv.json 不迁移：存在即
// 忽略（不装载、不报错），pre-release 冷启动重建是声明的姿态。
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
