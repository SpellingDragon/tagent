// Partition-local access and snapshot-commit benchmarks for the local KV and
// the segment store: same-condition before/after evidence for the storage
// efficiency gate. Public API only, so the identical file also compiles
// against the pre-change tree.
//
// 规格: docs/wiki/platform/evaluation-suites.md#offline-bench
package offline_bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
)

// benchPartitions is the partition spread of every fixture below; one run
// never exceeds the D17 budget (compact bodies keep the 100k cell well under
// 256MiB, and the 64KiB cell caps at 512 events).
const benchPartitions = 16

func benchGate(b *testing.B) {
	if os.Getenv(envRun) != "1" {
		b.Skipf("offline benchmark: set %s=1 to run (see file header)", envRun)
	}
}

// benchEventJSON mirrors the stored value shape the scan side decodes; the
// fields are the ones a header pass needs (key, type, timestamp, summary).
func benchEventJSON(pid, window int, seq int, key int64, body string) string {
	evt := map[string]any{
		"event_key":  key,
		"partition":  pid,
		"event_type": "agent_output",
		"timestamp":  time.Unix(int64(window*3600), 0).UnixMilli(),
		"content":    body,
	}
	raw, err := json.Marshal(evt)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// seedStore preloads events outside the timed region via the batch path plus
// one barrier; the store is then reopened cold so queries never read a
// process-local cache. Segment metadata is written ONCE after preloading with
// final per-window counts, never re-emitted per batch.
func seedStore(b *testing.B, dir string, total int, body string) (*kv.LocalFileKV, int) {
	b.Helper()
	backend := func() *kv.LocalFileKV {
		k, err := kv.NewLocalFileKV(dir)
		if err != nil {
			b.Fatalf("open kv: %v", err)
		}
		return k
	}
	store := backend()
	ops := make([]memory.KVOp, 0, 512)
	type windowKey struct {
		pid    int
		window int
	}
	perWindow := map[windowKey]int{}
	flush := func() error {
		if err := store.KVBatch(ops); err != nil {
			return err
		}
		ops = ops[:0]
		return store.Sync()
	}
	for i := 0; i < total; i++ {
		pid := i % benchPartitions
		window := (i / benchPartitions) % 24
		seq := (i / benchPartitions) / 24
		key := int64(1_000_000_000 + i)
		evtKey := fmt.Sprintf("%d:evt:%d:%d", pid, window, seq)
		idxKey := fmt.Sprintf("%d:idx:%d", pid, key)
		ops = append(ops,
			memory.KVOp{Type: "put", Key: evtKey, Value: benchEventJSON(pid, window, seq, key, body)},
			memory.KVOp{Type: "put", Key: idxKey, Value: fmt.Sprintf("%d:%d", window, seq)},
		)
		perWindow[windowKey{pid, window}]++
		if len(ops) >= 512 {
			if err := flush(); err != nil {
				b.Fatalf("seed flush: %v", err)
			}
		}
	}
	if len(ops) > 0 {
		if err := flush(); err != nil {
			b.Fatalf("seed tail flush: %v", err)
		}
	}
	for wk, count := range perWindow {
		meta := fmt.Sprintf(`{"pid":%d,"window_ts":%d,"layer":1,"event_count":%d,"min_time":%d,"max_time":%d,"sealed":false}`,
			wk.pid, wk.window, count, wk.window*3600_000, wk.window*3600_000)
		if err := store.KVPut(fmt.Sprintf("%d:meta:%d", wk.pid, wk.window), meta); err != nil {
			b.Fatalf("seed meta put: %v", err)
		}
	}
	if err := store.Sync(); err != nil {
		b.Fatalf("seed meta sync: %v", err)
	}
	if err := store.Close(); err != nil {
		b.Fatalf("seed close: %v", err)
	}
	return backend(), 24
}

// BenchmarkPartitionScan times a prefix scan against one partition while the
// store holds sixteen; the storage efficiency gate compares this against the
// pre-change tree run with the same fixture.
func BenchmarkPartitionScan(b *testing.B) {
	benchGate(b)
	for _, total := range []int{1000, 10000, 100000} {
		body := strings.Repeat("x", 1024)
		b.Run(fmt.Sprintf("events=%d", total), func(b *testing.B) {
			dir := filepath.Join(b.TempDir(), "kv")
			store, _ := seedStore(b, dir, total, body)
			defer func() { _ = store.Close() }()
			b.ReportMetric(float64(total), "events")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pairs, err := store.KVScan("5:evt:", 0)
				if err != nil || len(pairs) == 0 {
					b.Fatalf("scan: %v (n=%d)", err, len(pairs))
				}
			}
		})
	}
}

// BenchmarkSegmentQuery times an authorized partition query over a cold store
// (no keyword: the header path decides how much decoding a scan avoids).
func BenchmarkSegmentQuery(b *testing.B) {
	benchGate(b)
	for _, cell := range []struct {
		total int
		body  string
	}{
		{100000, strings.Repeat("x", 16)},
		{512, strings.Repeat("y", 64*1024)},
	} {
		b.Run(fmt.Sprintf("events=%d_body=%dKiB", cell.total, len(cell.body)/1024), func(b *testing.B) {
			dir := filepath.Join(b.TempDir(), "store")
			kvDir := filepath.Join(dir, "kv")
			backend, _ := seedStore(b, kvDir, cell.total, cell.body)
			store, err := memory.NewFileSegmentStore(backend, nil, dir, 1000)
			if err != nil {
				b.Fatalf("open store: %v", err)
			}
			defer func() { _ = store.Close() }()
			ids := make([]int, 0, benchPartitions)
			for p := 0; p < benchPartitions; p++ {
				ids = append(ids, p)
			}
			b.ReportMetric(float64(cell.total), "events")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				refs, err := store.QueryEvents(memory.QueryOptions{
					PartitionIDs: ids,
					OrderBy:      "timestamp_desc",
					Limit:        50,
				})
				if err != nil || len(refs) == 0 {
					b.Fatalf("query: %v (n=%d)", err, len(refs))
				}
			}
		})
	}
}

// BenchmarkSnapshotCommit times the per-event write-plus-barrier path the
// durable commit protocol runs in production (one KV mutation, one Sync).
func BenchmarkSnapshotCommit(b *testing.B) {
	benchGate(b)
	body := strings.Repeat("z", 1024)
	dir := filepath.Join(b.TempDir(), "kv")
	store, _ := seedStore(b, dir, 10000, body)
	defer func() { _ = store.Close() }()
	seq := 1_000_000
	b.ReportMetric(10000, "seeded")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq++
		pid := strconv.Itoa(seq % benchPartitions)
		key := fmt.Sprintf("%s:evt:23:%d", pid, seq)
		if err := store.KVPut(key, benchEventJSON(0, 23, seq, int64(seq), body)); err != nil {
			b.Fatalf("put: %v", err)
		}
		if err := store.Sync(); err != nil {
			b.Fatalf("sync: %v", err)
		}
	}
}
