// Package offline_bench holds the offline performance baseline for the
// resident hardening program (archived
// design D6, converged): event scale 1k/10k/100k (single Sync-barrier
// setting — the minimal localfile backend has NO fsync axis anymore, a second
// "fsync" column would measure the same bytes twice) × probe
// concurrency 1/10/100, recording p50/p95, allocs, RSS, KV scan volume and
// the chars/token estimator error against the pinned offline tokenizer
// fixture. It is NOT part of CI: run explicitly with
//
//	RUN_OFFLINE_BENCH=1 go test ./tests/offline_bench/ -run TestOfflineBenchmark -v -timeout 60m
//
// and point BENCH_REPORT=<path> to persist the JSON report for the 2.6
// regression comparison.
package offline_bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	envRun    = "RUN_OFFLINE_BENCH"
	envReport = "BENCH_REPORT"
	// sampleCap fsyncSampleCap bounds per-op fsynced writes per cell (100k × fsync on a
	// laptop would run past the point of signal); the report marks the cell
	// "sampled" so no number is presented as a full-scale measurement.
	// sampleCap bounds per-cell writes (/: the minimal backend has no
	// fsync axis; the cap remains to bound 100k-cell runtime; cells above it
	// are marked sampled=true, never presented as full-scale).
	sampleCap      = 20000
	probePerWorker = 10
)

func testBinaryName() string { return filepath.Base(os.Args[0]) }

func TestOfflineBenchmark(t *testing.T) {
	if os.Getenv(envRun) != "1" {
		t.Skipf("offline benchmark: set %s=1 to run (see file header for the command)", envRun)
	}
	report := map[string]any{"generated": time.Now().Format(time.RFC3339)}
	hostName, _ := os.Hostname()
	report["env"] = map[string]any{
		"go_version": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH,
		"num_cpu": runtime.NumCPU(), "host": hostName,
		"cgo_executable":     testBinaryName(),
		"memory_stats_kind":  "Go runtime MemStats only — deliberately NOT OS RSS (no OS sampler is used; see go_mem_* fields)",
		"storage_scales":     []int{1_000, 10_000, 100_000},
		"storage_sample_cap": sampleCap,
		"probe_conurrencies": []int{1, 10, 100},
		"probe_per_worker":   probePerWorker,
	}

	report["storage"] = storageMatrix(t)
	report["compression"] = compressionSweep(t)
	report["token_estimator"] = tokenEstimatorError(t)

	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	t.Logf("\n%s", raw)
	if path := os.Getenv(envReport); path != "" {
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatalf("write %s=%s: %v", envReport, path, err)
		}
		t.Logf("report written to %s", path)
	}
}

// durableKV countingKV wraps a KVStore and counts operations per method.
//
// F10 fix: FileSegmentStore
// consumes Sync and ListPartitionIDs via type assertion, not the memory.KVStore
// interface (which declares neither). Embedding only memory.KVStore would let
// those assertions fail on the wrapper, silently hiding the underlying
// LocalFileKV's event-level durability barrier and cold-partition enumeration —
// so the benchmark would measure flush-only latency and skip cold discovery.
// These methods forward both capabilities (and Sync errors) explicitly so the
// benchmark crosses the SAME commit path as production.
// durableKV is the REAL underlying contract the benchmark measures through
// : the durability barrier and cold-partition enumeration must exist on
// the backend itself. No-op capability fallbacks are deleted — a wrapper that
// quietly answers Sync()=nil when the backend lacks the barrier would report a
// durable commit that never happened, exactly the false-green D7 forbids.
type durableKV interface {
	memory.KVStore
	Sync() error
	ListPartitionIDs() []int
}

type countingKV struct {
	memory.KVStore
	durable                                     durableKV
	gets, puts, scans, ranges, batches, deletes atomic.Int64
	syncs                                       atomic.Int64
}

// newCountingKV fails fast: only a backend with the real barrier contract may
// be benchmarked through this wrapper.
func newCountingKV(kv memory.KVStore) *countingKV {
	d, ok := kv.(durableKV)
	if !ok {
		panic(fmt.Sprintf("offline_bench: backend %T lacks Sync()/ListPartitionIDs() — §9.1 deleted the no-op capability fallback; wire a durable-capable backend (LocalFileKV qualifies)", kv))
	}
	return &countingKV{KVStore: kv, durable: d}
}

func (c *countingKV) KVGet(k string) (string, error) { c.gets.Add(1); return c.KVStore.KVGet(k) }
func (c *countingKV) KVPut(k, v string) error        { c.puts.Add(1); return c.KVStore.KVPut(k, v) }
func (c *countingKV) KVDelete(k string) error        { c.deletes.Add(1); return c.KVStore.KVDelete(k) }
func (c *countingKV) KVScan(p string, l int) ([]memory.KVPair, error) {
	c.scans.Add(1)
	return c.KVStore.KVScan(p, l)
}
func (c *countingKV) KVRange(s, e string, l int) ([]memory.KVPair, error) {
	c.ranges.Add(1)
	return c.KVStore.KVRange(s, e, l)
}
func (c *countingKV) KVBatch(ops []memory.KVOp) error {
	c.batches.Add(1)
	return c.KVStore.KVBatch(ops)
}

// Sync forwards the durability barrier to the underlying store  and counts
// the call — through the REQUIRED contract, never a swallowed no-op:
// without this the FileSegmentStore barrier assertion `s.kv.(interface{ Sync()
// error })` fails and the event-level commit barrier is silently skipped,
// measuring flush-only latency while reporting a durable commit.
func (c *countingKV) Sync() error {
	c.syncs.Add(1)
	return c.durable.Sync()
}

// ListPartitionIDs forwards cold-partition enumeration so FileSegmentStore
// Init() discovers partitions over the wrapper too , with no nil fallback.
func (c *countingKV) ListPartitionIDs() []int { return c.durable.ListPartitionIDs() }

// Compile-time capability lock: the wrapper implements the
// FULL durable contract, and the required underlying shape is explicit.
var _ durableKV = (*countingKV)(nil)

// errBarrierBoom marks an injected underlying-Sync failure.
var errBarrierBoom = errors.New("underlying Sync barrier failed")

// syncErrKV is a KVStore spy whose Sync always fails; the 6 KVStore methods are
// inherited from the (nil) embedded interface and never called by the test.
type syncErrKV struct{ memory.KVStore }

func (syncErrKV) Sync() error             { return errBarrierBoom }
func (syncErrKV) ListPartitionIDs() []int { return nil }

// plainKV deliberately LACKS the durable contract — used to prove the
// construction gate refuses a no-op-capable backend loudly.
type plainKV struct{ memory.KVStore }

// TestNewCountingKVRequiresDurableBackend: the deleted no-op fallback
// must come back as a LOUD failure at wiring time, never as a silent zero-
// barrier benchmark cell.
func TestNewCountingKVRequiresDurableBackend(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("§9.1: a backend without Sync/ListPartitionIDs must be refused at construction")
		}
	}()
	newCountingKV(plainKV{})
}

// TestCountingKVSyncErrorPropagates proves the wrapper forwards the underlying
// Sync error rather than swallowing it (F10/D7「禁止 no-op 假能力」): a no-op
// Sync would let the benchmark report a durability barrier that never happened.
// It also proves the call is counted even on failure.
func TestCountingKVSyncErrorPropagates(t *testing.T) {
	ckv := newCountingKV(syncErrKV{})
	if err := ckv.Sync(); !errors.Is(err, errBarrierBoom) {
		t.Fatalf("countingKV must propagate the underlying Sync error, got %v", err)
	}
	if got := ckv.syncs.Load(); got < 1 {
		t.Fatalf("Sync must be counted even when it fails, got %d", got)
	}
}

// snapshot: get, put, scan, range, batch, delete, sync —  adds the sync
// barrier count so every cell can prove the durable-commit path ran.
func (c *countingKV) snapshot() [7]int64 {
	return [7]int64{c.gets.Load(), c.puts.Load(), c.scans.Load(), c.ranges.Load(), c.batches.Load(), c.deletes.Load(), c.syncs.Load()}
}

type latencies []float64

func (l latencies) summarize() map[string]any {
	if len(l) == 0 {
		return map[string]any{"n": 0}
	}
	s := append(latencies(nil), l...)
	sort.Float64s(s)
	pct := func(p float64) float64 { return s[int(math.Min(float64(len(s)-1), math.Round(p*float64(len(s)))))] }
	var sum float64
	for _, v := range s {
		sum += v
	}
	return map[string]any{
		"n": len(s), "mean_ms": round3(sum / float64(len(s))),
		"p50_ms": round3(pct(0.50)), "p95_ms": round3(pct(0.95)), "max_ms": round3(s[len(s)-1]),
	}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// diffSnap is the per-op delta between two countingKV snapshots.
func diffSnap(after, before [7]int64) [7]int64 {
	var d [7]int64
	for i := range d {
		d[i] = after[i] - before[i]
	}
	return d
}

func fileSize(t *testing.T, path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return fi.Size()
}

// goMem reports GO RUNTIME heap statistics — explicitly not OS RSS:
// runtime.MemStats cannot see RSS contributions outside the Go heap (page
// tables, cgo, kernel buffers), so the field names say what they measure.
func goMem() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]any{"heap_inuse_mib": round3(float64(m.HeapInuse) / (1 << 20)), "sys_mib": round3(float64(m.Sys) / (1 << 20))}
}

func storageMatrix(t *testing.T) map[string]any {
	scales := []int{1_000, 10_000, 100_000}
	cells := []map[string]any{}
	content := strings.Repeat("压测事件内容，包含中英文 mixed content 与 payload。", 8)

	for _, scale := range scales {
		{
			writes := scale
			sampled := false
			if writes > sampleCap {
				writes = sampleCap
				sampled = true
			}
			dir := t.TempDir()
			inner, err := kv.NewLocalFileKV(dir)
			if err != nil {
				t.Fatalf("NewLocalFileKV: %v", err)
			}
			ckv := newCountingKV(inner)
			store, err := memory.NewFileSegmentStore(ckv, nil, dir, 1000)
			if err != nil {
				t.Fatalf("NewFileSegmentStore: %v", err)
			}
			base := int64(1_700_000_000_000)
			keys := make([]int64, 0, writes)
			writeBefore := ckv.snapshot()

			var memBefore, memAfter runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&memBefore)
			writeStart := time.Now()
			wl := make(latencies, 0, writes)
			for i := 0; i < writes; i++ {
				key := memory.NewSnowflakeEventKey(1, base+int64(i)*10)
				keys = append(keys, key)
				t0 := time.Now()
				if err := store.StoreEvent(key, memory.FullEvent{
					EventKey: key, PartitionID: 1, EventType: "external_input",
					EventSummary: fmt.Sprintf("[%06d] %s", i, content),
					Content:      fmt.Sprintf("[%06d] %s", i, content), Timestamp: base + int64(i)*10,
				}); err != nil {
					t.Fatalf("StoreEvent: %v", err)
				}
				wl = append(wl, float64(time.Since(t0).Microseconds())/1000.0)
			}
			runtime.ReadMemStats(&memAfter)
			writeOps := diffSnap(ckv.snapshot(), writeBefore)
			segFiles, _ := filepath.Glob(filepath.Join(dir, "*.json"))
			tmpLeft, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
			cell := map[string]any{
				"scale":   scale,
				"written": writes, "requested_scale_full": scale, "sampled": sampled,
				"write":                  wl.summarize(),
				"write_thr":              round3(float64(writes) / time.Since(writeStart).Seconds()),
				"allocs_bytes_per_write": round3(float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / float64(writes)),
				"commit_summary": map[string]any{
					"kv_gets": writeOps[0], "kv_puts": writeOps[1], "kv_scans": writeOps[2],
					"kv_ranges": writeOps[3], "kv_batches": writeOps[4], "kv_deletes": writeOps[5],
					"sync_barriers": writeOps[6], "sync_barriers_per_write": round3(float64(writeOps[6]) / float64(writes)),
					"segments_on_disk": len(segFiles), "dirty_tmp_orphans": len(tmpLeft),
					"kv_snapshot_bytes":       fileSize(t, filepath.Join(dir, "kv.json")),
					"partitions_discoverable": len(ckv.ListPartitionIDs()),
				},
			}

			probeResults := map[string]any{}
			for _, conc := range []int{1, 10, 100} {
				var mu sync.Mutex
				var getL, queryL latencies
				var getWinNs, queryWinNs int64
				before := ckv.snapshot()
				pst := time.Now()
				var wg sync.WaitGroup
				perWorker := probePerWorker
				for w := 0; w < conc; w++ {
					wg.Add(1)
					go func(seed int64) {
						defer wg.Done()
						r := rand.New(rand.NewSource(seed))
						lg, lq := make(latencies, 0, perWorker), make(latencies, 0, perWorker)
						var wg_, wq_ int64
						for p := 0; p < perWorker; p++ {
							k := keys[r.Intn(len(keys))]
							t0 := time.Now()
							if _, err := store.GetEvent(k); err != nil {
								t.Errorf("GetEvent: %v", err)
							}
							d0 := time.Since(t0)
							lg = append(lg, float64(d0.Microseconds())/1000.0)
							wg_ += int64(d0)
							span := int64(60_000)
							st := base + int64(r.Intn(len(keys)))*10
							t1 := time.Now()
							if _, err := store.QueryEvents(memory.QueryOptions{
								PartitionID: 1, StartTime: st, EndTime: st + span,
							}); err != nil {
								t.Errorf("QueryEvents: %v", err)
							}
							d1 := time.Since(t1)
							lq = append(lq, float64(d1.Microseconds())/1000.0)
							wq_ += int64(d1)
						}
						mu.Lock()
						getL, queryL = append(getL, lg...), append(queryL, lq...)
						getWinNs, queryWinNs = getWinNs+wg_, queryWinNs+wq_
						mu.Unlock()
					}(int64(w) + 1)
				}
				wg.Wait()
				after := ckv.snapshot()
				n := int64(conc * perWorker)
				one := func(l latencies, wallNs int64, kvs, kve int64) map[string]any {
					return map[string]any{
						"probes":        n,
						"latency":       l.summarize(),
						"throughput_ps": round3(float64(n) / (float64(wallNs) / 1e9)),
						"kv_ops_per_probe": map[string]any{
							"point_get": round3(float64(after[kvs]-before[kvs]) / float64(n)),
							"scan":      round3(float64(after[2]-before[2]) / float64(n)),
							"range":     round3(float64(after[3]-before[3]) / float64(n)),
						},
					}
				}
				probeResults[fmt.Sprint(conc)] = map[string]any{
					"get_point_read": one(getL, getWinNs, 0, 0),
					"query_window":   one(queryL, queryWinNs, 1, 1),
					"sync_barriers":  after[6] - before[6],
					"total_wall_s":   round3(time.Since(pst).Seconds()),
				}
			}
			cell["probes"] = probeResults
			cell["go_mem_runtime"] = goMem()
			cells = append(cells, cell)
			_ = inner.Close()
		}
	}
	return map[string]any{"cells": cells, "note": "fork/s is not exercised by this memory/compress workload (tmux probes live in the action domain)"}
}

func compressionSweep(t *testing.T) []map[string]any {
	out := []map[string]any{}
	for _, scale := range []int{1_000, 10_000, 100_000} {
		store := memory.NewInMemoryStore()
		refs := make([]memory.EventReference, 0, scale)
		base := int64(1_700_000_000_000)
		body := strings.Repeat("回合内容 with mixed zh/en tokens for realistic estimation. ", 4)
		for i := 0; i < scale; i++ {
			key := int64(1_000_000 + i)
			evtType := "external_input"
			if i%4 == 3 {
				evtType = "agent_output"
			}
			refs = append(refs, memory.EventReference{
				EventKey: key, PartitionID: 1, EventType: evtType,
				EventSummary: fmt.Sprintf("[%06d] %s", i, body), Timestamp: base + int64(i)*1000,
			})
			if err := store.StoreEvent(key, memory.FullEvent{
				EventKey: key, PartitionID: 1, EventType: evtType,
				EventSummary: refs[i].EventSummary, Content: refs[i].EventSummary, Timestamp: refs[i].Timestamp,
			}); err != nil {
				t.Fatalf("StoreEvent: %v", err)
			}
		}
		sc := compress.NewSmartCompressor(compress.WithKeepRecentTasks(2), compress.WithMaxTokens(4000))
		cc := compress.NewContextCompressor(sc, store, compress.NewDefaultTokenCounter(), 4000, 0.8, 2)

		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		res := cc.Compress(context.Background(), refs)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		out = append(out, map[string]any{
			"refs": scale, "duration_ms": elapsed.Milliseconds(),
			"allocs_bytes_per_round": round3(float64(after.TotalAlloc-before.TotalAlloc) / float64(scale)),
			"retained_refs":          len(res.RetainedRefs),
			"compressed":             res.Compressed,
			"go_mem_runtime":         goMem(),
		})
	}
	return out
}

type tokenFixture struct {
	Tokenizer       string `json:"tokenizer"`
	TiktokenVersion string `json:"tiktoken_version"`
	Corpora         map[string]struct {
		Count   int `json:"count"`
		Samples []struct {
			Text   string `json:"text"`
			Tokens int    `json:"tokens"`
		} `json:"samples"`
	} `json:"corpora"`
}

func tokenEstimatorError(t *testing.T) map[string]any {
	raw, err := os.ReadFile(filepath.Join("testdata", "token_fixture.json"))
	if err != nil {
		t.Fatalf("read fixture (regenerate via gen_token_fixture.py): %v", err)
	}
	var f tokenFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	tc := compress.NewDefaultTokenCounter()
	out := map[string]any{"reference": f.Tokenizer, "tiktoken_version": f.TiktokenVersion}
	for name, corp := range f.Corpora {
		errs := make([]float64, 0, len(corp.Samples))
		ratios := make([]float64, 0, len(corp.Samples))
		for _, s := range corp.Samples {
			est := tc.Estimate([]model.Message{{Role: model.RoleUser, Content: s.Text}})
			errs = append(errs, math.Abs(float64(est-s.Tokens))/float64(s.Tokens)*100.0)
			ratios = append(ratios, float64(est)/float64(s.Tokens))
		}
		sort.Float64s(errs)
		sort.Float64s(ratios)
		p := func(a []float64, q float64) float64 {
			return a[int(math.Min(float64(len(a)-1), math.Round(q*float64(len(a)))))]
		}
		out[name] = map[string]any{
			"samples":         len(corp.Samples),
			"abs_err_pct_p50": round3(p(errs, 0.5)), "abs_err_pct_p90": round3(p(errs, 0.9)),
			"est_over_ref_p50": round3(p(ratios, 0.5)), "est_over_ref_p90": round3(p(ratios, 0.9)),
		}
	}
	return out
}

// benchBarrierMarker is the single event's content, read back by a fresh process.
const benchBarrierMarker = "bench-wrapper-barrier-marker"

// TestBenchWrapperBarrierDurableWithoutClose proves the offline benchmark wraps
// the KV with the SAME event-level durability barrier production uses
// . The benchmark stores every
// event through countingKV; before the F10 fix countingKV exposed no Sync, so
// FileSegmentStore's `s.kv.(interface{ Sync() error })` assertion failed, the
// commit barrier was skipped, and a single acknowledged write stayed in
// LocalFileKV's in-memory pending buffer (KVPut is async acceptance) — lost on
// an unclean exit.
//
// The child replicates the benchmark's exact wrapping (countingKV over
// LocalFileKV), stores ONE event, records the countingKV.syncs delta, then
// terminates WITHOUT Close. The parent asserts (1) the wrapper's Sync was
// reached (delta >= 1: the barrier ran through the wrapper, not around it) and
// (2) a fresh, independent store over the same directory reads the event back —
// so the benchmark and production cross the same event barrier.
//
// fsync boundary (D7「报告不混为掉电耐久」):
//
//	(converged per the localfile-minimization ruling): the minimal backend
//
// has NO fsync/WAL machinery — WithFSync is accepted-and-ignored, so an
// "fsync-on" cell would attest the SAME bytes twice under two names (a false
// two-axis claim, deleted). What IS certified here: the Sync barrier is a real
// atomic tmp+rename durable commit — visible to a fresh, independent process
// after an UNCLEAN exit (no Close, no flush tick), with the actual barrier
// count and original-text/index evidence. NO power-loss durability is claimed;
// that dimension is deferred to the rustviking-backed stage.
func TestBenchWrapperBarrierDurableWithoutClose(t *testing.T) {
	if os.Getenv("TAGENT_BENCH_BARRIER_SUBPROC") == "1" {
		runBenchBarrierChild()
		return
	}

	cases := []struct {
		name string
	}{
		{"sync-barrier-atomic-rename"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			syncsFile := filepath.Join(t.TempDir(), "syncs.txt")

			cmd := exec.Command(os.Args[0], "-test.run", "^TestBenchWrapperBarrierDurableWithoutClose$", "-test.v")
			cmd.Env = append(os.Environ(),
				"TAGENT_BENCH_BARRIER_SUBPROC=1",
				"TAGENT_BENCH_BARRIER_DIR="+dir,
				"TAGENT_BENCH_BARRIER_SYNCS_FILE="+syncsFile,
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child did not acknowledge the barrier write (err=%v): %s", err, out)
			}

			raw, err := os.ReadFile(syncsFile)
			if err != nil {
				t.Fatalf("read syncs marker: %v", err)
			}
			var syncs int64
			if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &syncs); err != nil {
				t.Fatalf("parse syncs marker %q: %v", raw, err)
			}
			if syncs < 1 {
				t.Fatalf("countingKV did not forward the event barrier (syncs=%d): benchmark path diverges from production", syncs)
			}

			inner, err := kv.NewLocalFileKV(dir)
			if err != nil {
				t.Fatalf("reopen kv: %v", err)
			}
			store, err := memory.NewFileSegmentStore(inner, nil, dir, 1000)
			if err != nil {
				t.Fatalf("reopen store: %v", err)
			}
			pid := memory.PartitionIDFromName("benchbarrier")
			refs, err := store.QueryEvents(memory.QueryOptions{
				PartitionIDs: []int{pid},
				Keyword:      benchBarrierMarker,
			})
			if err != nil || len(refs) == 0 {
				t.Fatalf("wrapper-barrier event lost after unclean exit (fsync field removed §9.2): refs=%d err=%v", len(refs), err)
			}
			evt, err := store.GetEvent(refs[0].EventKey)
			if err != nil {
				t.Fatalf("GetEvent via idx after reopen: %v", err)
			}
			if evt.Content != benchBarrierMarker || evt.Metadata["m"] != "1" {
				t.Fatalf("event corrupted across restart: %+v", evt)
			}
		})
	}
}

// runBenchBarrierChild replicates the benchmark wrapping, stores one event,
// records the syncs delta, and exits WITHOUT Close so only a forwarded barrier
// can persist the write. Env-only inputs; never returns.
func runBenchBarrierChild() {
	dir := os.Getenv("TAGENT_BENCH_BARRIER_DIR")
	syncsFile := os.Getenv("TAGENT_BENCH_BARRIER_SYNCS_FILE")

	inner, err := kv.NewLocalFileKV(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child: open kv:", err)
		os.Exit(2)
	}
	ckv := newCountingKV(inner)
	store, err := memory.NewFileSegmentStore(ckv, nil, dir, 1000)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child: open store:", err)
		os.Exit(2)
	}

	pid := memory.PartitionIDFromName("benchbarrier")
	key := memory.NewSnowflakeEventKey(pid, 0)
	before := ckv.syncs.Load()
	if err := store.StoreEvent(key, memory.FullEvent{
		EventKey:     key,
		PartitionID:  pid,
		EventType:    "external_input",
		EventSummary: benchBarrierMarker,
		Content:      benchBarrierMarker,
		Timestamp:    1700000000000,
		Metadata:     map[string]string{"m": "1"},
	}); err != nil {
		fmt.Fprintln(os.Stderr, "child: StoreEvent:", err)
		os.Exit(3)
	}
	delta := ckv.syncs.Load() - before
	if err := os.WriteFile(syncsFile, []byte(fmt.Sprintf("%d", delta)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "child: write syncs marker:", err)
		os.Exit(4)
	}
	os.Exit(0)
}
