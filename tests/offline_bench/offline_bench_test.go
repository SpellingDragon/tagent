// Package offline_bench holds the offline performance baseline for the
// resident hardening program (resident-remaining-hardening 2.5, archived
// design D6): event scale 1k/10k/100k × fsync two settings × probe
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
	"fmt"
	"math"
	"math/rand"
	"os"
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
	// fsyncSampleCap bounds per-op fsynced writes per cell (100k × fsync on a
	// laptop would run past the point of signal); the report marks the cell
	// "sampled" so no number is presented as a full-scale measurement.
	fsyncSampleCap = 20000
)

func TestOfflineBenchmark(t *testing.T) {
	if os.Getenv(envRun) != "1" {
		t.Skipf("offline benchmark: set %s=1 to run (see file header for the command)", envRun)
	}
	report := map[string]any{"generated": time.Now().Format(time.RFC3339)}

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

// ---------------------------------------------------------------------------
// storage matrix: scale × fsync × probe concurrency
// ---------------------------------------------------------------------------

// countingKV wraps a KVStore and counts operations per method.
type countingKV struct {
	memory.KVStore
	gets, puts, scans, ranges, batches, deletes atomic.Int64
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

func (c *countingKV) snapshot() [6]int64 {
	return [6]int64{c.gets.Load(), c.puts.Load(), c.scans.Load(), c.ranges.Load(), c.batches.Load(), c.deletes.Load()}
}

type latencies []float64 // milliseconds

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

func rss() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]any{"heap_inuse_mib": round3(float64(m.HeapInuse) / (1 << 20)), "sys_mib": round3(float64(m.Sys) / (1 << 20))}
}

func storageMatrix(t *testing.T) map[string]any {
	scales := []int{1_000, 10_000, 100_000}
	cells := []map[string]any{}
	content := strings.Repeat("压测事件内容，包含中英文 mixed content 与 payload。", 8) // ~200 chars

	for _, scale := range scales {
		for _, fsync := range []bool{true, false} {
			writes := scale
			sampled := false
			if fsync && writes > fsyncSampleCap {
				writes = fsyncSampleCap
				sampled = true
			}
			dir := t.TempDir()
			inner, err := kv.NewLocalFileKV(dir, kv.WithFSync(fsync))
			if err != nil {
				t.Fatalf("NewLocalFileKV: %v", err)
			}
			ckv := &countingKV{KVStore: inner}
			store, err := memory.NewFileSegmentStore(ckv, nil, dir, 1000)
			if err != nil {
				t.Fatalf("NewFileSegmentStore: %v", err)
			}
			base := int64(1_700_000_000_000)
			keys := make([]int64, 0, writes)

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
			cell := map[string]any{
				"scale": scale, "fsync": fsync, "written": writes, "sampled": sampled,
				"write":                  wl.summarize(),
				"write_thr":              round3(float64(writes) / time.Since(writeStart).Seconds()),
				"allocs_bytes_per_write": round3(float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / float64(writes)),
			}

			// Probe reads: point GetEvent + time-window QueryEvents, at
			// concurrency 1/10/100 (fixed total probes per worker count so
			// contention, not workload size, is the swept dimension).
			probeResults := map[string]any{}
			for _, conc := range []int{1, 10, 100} {
				var mu sync.Mutex
				var rl latencies
				before := ckv.snapshot()
				pst := time.Now()
				var wg sync.WaitGroup
				perWorker := 10
				for w := 0; w < conc; w++ {
					wg.Add(1)
					go func(seed int64) {
						defer wg.Done()
						r := rand.New(rand.NewSource(seed))
						local := make(latencies, 0, perWorker*2)
						for p := 0; p < perWorker; p++ {
							k := keys[r.Intn(len(keys))]
							t0 := time.Now()
							if _, err := store.GetEvent(k); err != nil {
								t.Errorf("GetEvent: %v", err)
							}
							local = append(local, float64(time.Since(t0).Microseconds())/1000.0)
							span := int64(60_000) // 1-minute window scan
							st := base + int64(r.Intn(len(keys)))*10
							t1 := time.Now()
							if _, err := store.QueryEvents(memory.QueryOptions{
								PartitionID: 1, StartTime: st, EndTime: st + span,
							}); err != nil {
								t.Errorf("QueryEvents: %v", err)
							}
							local = append(local, float64(time.Since(t1).Microseconds())/1000.0)
						}
						mu.Lock()
						rl = append(rl, local...)
						mu.Unlock()
					}(int64(w) + 1)
				}
				wg.Wait()
				after := ckv.snapshot()
				probeCount := int64(conc * perWorker * 2)
				probeResults[fmt.Sprint(conc)] = map[string]any{
					"probes":        probeCount,
					"latency":       rl.summarize(),
					"throughput_ps": round3(float64(probeCount) / time.Since(pst).Seconds()),
					"kv_ops_per_probe": map[string]any{
						"get":   round3(float64(after[0]-before[0]) / float64(probeCount)),
						"scan":  round3(float64(after[2]-before[2]) / float64(probeCount)),
						"range": round3(float64(after[3]-before[3]) / float64(probeCount)),
					},
				}
			}
			cell["probes"] = probeResults
			cell["rss"] = rss()
			cells = append(cells, cell)
			_ = inner.Close()
		}
	}
	return map[string]any{"cells": cells, "note": "fork/s is not exercised by this memory/compress workload (tmux probes live in the action domain)"}
}

// ---------------------------------------------------------------------------
// compression sweep: Compress latency / allocs at ref scale
// ---------------------------------------------------------------------------

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
		// Tiny budget so the compaction path always engages at every scale.
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
			"rss":                    rss(),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// token estimator error vs pinned offline fixture
// ---------------------------------------------------------------------------

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
	tc := compress.NewDefaultTokenCounter() // CharsPerToken = 2.0
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
