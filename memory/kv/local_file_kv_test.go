package kv

import (
	"github.com/SpellingDragon/tagent/memory"

	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

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

	_, snapErr := os.Stat(filepath.Join(dir, "kv-global.json"))
	require.NoError(t, snapErr, "non-partition namespaces land in kv-global.json, one snapshot file per bucket")
	_, legacyErr := os.Stat(filepath.Join(dir, "kv.json"))
	require.True(t, os.IsNotExist(legacyErr), "the legacy single kv.json layout must not reappear")

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

// TestLocalFileKV_SyncOnlyWritesDirtyBuckets 钉住脏桶隔离提交。
// - 仅脏分区快照被重写；单次提交的落盘成本只由所属分区键数决定。
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

// TestLocalFileKV_CrossProcessReadBack 钉住快照屏障的跨进程可见性。
// - 子进程 Sync 后退出，新进程读回全部分区键值；多桶布局下语义保持不变。
func TestLocalFileKV_CrossProcessReadBack(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "warm.json"), nil, 0o644), "a placeholder keeps the dir warm")

	kv1, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	require.NoError(t, kv1.KVPut("1:evt:a", "v1"))
	require.NoError(t, kv1.KVPut("2:meta:b", "v2"))
	require.NoError(t, kv1.KVPut("global:c", "v3"))
	require.NoError(t, kv1.Close(), "Close flushes every dirty bucket")

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

// TestLocalFileKV_LegacySingleSnapshotIgnored 钉住旧 kv.json 不迁移。
// - 存在即忽略（不装载、不报错）；pre-release 冷启动重建是声明的姿态。
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

// refScan reproduces the pre-optimisation full-library prefix semantics: flatten
// every bucket, filter by HasPrefix, lexicographic sort, then truncate to limit.
// It is the oracle the partition-local KVScan must match bit-for-bit.
func refScan(k *LocalFileKV, prefix string, limit int) []memory.KVPair {
	var all []memory.KVPair
	for _, m := range k.parts {
		for key, val := range m {
			all = append(all, memory.KVPair{Key: key, Value: val})
		}
	}
	for key, val := range k.global {
		all = append(all, memory.KVPair{Key: key, Value: val})
	}
	var out []memory.KVPair
	for _, p := range all {
		if strings.HasPrefix(p.Key, prefix) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// refRange is the full-library oracle for KVRange over [start, end).
func refRange(k *LocalFileKV, start, end string, limit int) []memory.KVPair {
	var all []memory.KVPair
	for _, m := range k.parts {
		for key, val := range m {
			all = append(all, memory.KVPair{Key: key, Value: val})
		}
	}
	for key, val := range k.global {
		all = append(all, memory.KVPair{Key: key, Value: val})
	}
	var out []memory.KVPair
	for _, p := range all {
		if p.Key >= start && p.Key < end {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func eqPairs(a, b []memory.KVPair) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestLocalFileKV_PartitionScanEquivalence 钉住分区定向 Scan/Range 与原全库扫描参照逐位一致。
//   - 词典序一致、limit 在排序后截断；可证明属单命名空间的查询只访问一个桶
//   - 宇宙含多分区、前导零键、无冒号键、非法整数头（落 global）与真 global 键，足以绊倒草率的收窄
//   - 随机跨桶前缀/范围再以固定 seed 复核一遍
func TestLocalFileKV_PartitionScanEquivalence(t *testing.T) {
	dir := t.TempDir()
	k, err := NewLocalFileKV(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = k.Close() })

	universe := map[string]string{
		"1:a": "1a", "1:b": "1b", "1:evt:100:0": "e", "1:evt:100:1": "f", "1:evt:200:0": "g",
		"2:a": "2a", "2:meta:100": "m", "2:evt:100:0": "h",
		"10:x": "10x", "10:evt:50:3": "j",
		"42:evt:0:0": "k", "42:idx:999": "l",
		"01:weird":     "leading-zero",
		"1":            "colonless",
		"1;semi":       "global-semi",
		"global:evt:1": "g1", "global:z": "gz",
		"abc:x": "global-abc", "-1:neg": "neg",
		"": "emptykey",
	}
	for key, val := range universe {
		require.NoError(t, k.KVPut(key, val))
	}

	limits := []int{0, 1, 2, 3, 100}
	prefixCases := []struct {
		name       string
		prefix     string
		wantSingle bool
	}{
		{"full pid 1", "1:", true},
		{"full pid 2", "2:", true},
		{"full pid 10", "10:", true},
		{"full pid 42", "42:", true},
		{"segment prefix", "1:evt:100:", true},
		{"global namespace", "global:", true},
		{"leading-zero pid", "01:", true},
		{"non-numeric head", "abc:", true},
		{"leading colon", ":", true},
		{"fuzzy digit 1", "1", false},
		{"empty prefix", "", false},
	}
	for _, tc := range prefixCases {
		for _, limit := range limits {
			got, gerr := k.KVScan(tc.prefix, limit)
			require.NoError(t, gerr)
			want := refScan(k, tc.prefix, limit)
			require.True(t, eqPairs(got, want),
				"KVScan(%q,%d): got=%v want=%v", tc.prefix, limit, got, want)
			if tc.wantSingle {
				assert.Equal(t, 1, k.bucketsTouched,
					"KVScan(%q) must touch exactly one bucket (case %q)", tc.prefix, tc.name)
			} else {
				assert.Greater(t, k.bucketsTouched, 1,
					"KVScan(%q) must use the conservative fallback (case %q)", tc.prefix, tc.name)
			}
		}
	}

	rangeCases := []struct {
		name       string
		start, end string
		wantSingle bool
	}{
		{"inside pid 1", "1:", "1;", true},
		{"inside pid1 subrange", "1:a", "1:c", true},
		{"inside pid 2", "2:", "2;", true},
		{"inside pid 10", "10:", "10;", true},
		{"empty interval", "5:", "5:", true},
		{"cross bucket 1 to 2", "1:", "2;", false},
		{"fuzzy straddle", "1", "2", false},
		{"global region", "global:", "global~", false},
		{"whole store", "", "\xff\xff\xff", false},
	}
	for _, tc := range rangeCases {
		for _, limit := range limits {
			got, gerr := k.KVRange(tc.start, tc.end, limit)
			require.NoError(t, gerr)
			want := refRange(k, tc.start, tc.end, limit)
			require.True(t, eqPairs(got, want),
				"KVRange(%q,%q,%d): got=%v want=%v", tc.start, tc.end, limit, got, want)
			if tc.wantSingle {
				assert.Equal(t, 1, k.bucketsTouched,
					"KVRange(%q,%q) must touch one bucket (case %q)", tc.start, tc.end, tc.name)
			}
		}
	}

	rng := rand.New(rand.NewSource(20261007))
	keys := make([]string, 0, len(universe))
	for key := range universe {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i := 0; i < 400; i++ {
		a := keys[rng.Intn(len(keys))]
		cut := rng.Intn(len(a) + 1)
		prefix := a[:cut]
		plim := rng.Intn(4)
		got, gerr := k.KVScan(prefix, plim)
		require.NoError(t, gerr)
		require.True(t, eqPairs(got, refScan(k, prefix, plim)),
			"random KVScan(%q,%d)", prefix, plim)

		lo := a
		hi := keys[rng.Intn(len(keys))]
		if lo > hi {
			lo, hi = hi, lo
		}
		rlim := rng.Intn(4)
		rg, rerr := k.KVRange(lo, hi, rlim)
		require.NoError(t, rerr)
		require.True(t, eqPairs(rg, refRange(k, lo, hi, rlim)),
			"random KVRange(%q,%q,%d)", lo, hi, rlim)
	}
}
