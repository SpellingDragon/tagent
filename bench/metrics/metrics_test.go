// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package metrics

import (
	"encoding/json"
	"testing"
)

// attemptJSON builds one canonical attempt record for Aggregate.
func attemptJSON(t *testing.T, runner, taskID string, repeat int, cold sample, hot []sample) []byte {
	t.Helper()
	raw := map[string]any{
		"runner":  runner,
		"task_id": taskID,
		"repeat":  repeat,
		"success": true,
		"cold":    cold,
		"hot":     hot,
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal attempt: %v", err)
	}
	return b
}

// latencyDoc is the decoded shape of Aggregate's latency block; it keeps the
// JSON shape assertion explicit (cold/hot first_token_ms + complete_ms).
type latencyDoc struct {
	Metrics struct {
		Latency struct {
			Cold struct {
				Samples      int     `json:"samples"`
				FirstTokenMS float64 `json:"first_token_ms"`
				CompleteMS   float64 `json:"complete_ms"`
			} `json:"cold"`
			Hot struct {
				Samples      int     `json:"samples"`
				FirstTokenMS float64 `json:"first_token_ms"`
				CompleteMS   float64 `json:"complete_ms"`
			} `json:"hot"`
		} `json:"latency"`
	} `json:"metrics"`
}

func decodeLatency(t *testing.T, out []byte) latencyDoc {
	t.Helper()
	var doc latencyDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode aggregate result: %v", err)
	}
	return doc
}

// TestAggregateLatencyIsMedian pins that cold latency aggregates to the median, not the mean.
// - 3 repeats 10/20/300 -> 20 (mean 110); complete 100/200/300 -> 200
// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
func TestAggregateLatencyIsMedian(t *testing.T) {
	const runner = "tagent"
	attempts := [][]byte{
		attemptJSON(t, runner, "qa-01", 1, sample{FirstTokenMS: 10, CompleteMS: 100}, nil),
		attemptJSON(t, runner, "qa-01", 2, sample{FirstTokenMS: 20, CompleteMS: 200}, nil),
		attemptJSON(t, runner, "qa-01", 3, sample{FirstTokenMS: 300, CompleteMS: 300}, nil),
	}
	out, err := Aggregate(runner, StatusCompleted, "sender-confirmed", attempts)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	doc := decodeLatency(t, out)
	if got := doc.Metrics.Latency.Cold.FirstTokenMS; got != 20 {
		t.Fatalf("cold first_token_ms = %v, want median 20 (mean would be 110)", got)
	}
	if got := doc.Metrics.Latency.Cold.CompleteMS; got != 200 {
		t.Fatalf("cold complete_ms = %v, want median 200 (mean would be 200)", got)
	}
	if got := doc.Metrics.Latency.Cold.Samples; got != 3 {
		t.Fatalf("cold samples = %d, want 3", got)
	}
}

// TestAggregateHotLatencyIsMedian pins that hot latency aggregates to the median across all hot samples.
// - 100/20/3000 -> 100 (mean 1040); complete -> 1000
// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
func TestAggregateHotLatencyIsMedian(t *testing.T) {
	const runner = "dsh"
	attempts := [][]byte{
		attemptJSON(t, runner, "qa-02", 1, sample{FirstTokenMS: 5, CompleteMS: 50}, []sample{{FirstTokenMS: 100, CompleteMS: 1000}}),
		attemptJSON(t, runner, "qa-02", 2, sample{FirstTokenMS: 5, CompleteMS: 50}, []sample{{FirstTokenMS: 20, CompleteMS: 200}}),
		attemptJSON(t, runner, "qa-02", 3, sample{FirstTokenMS: 5, CompleteMS: 50}, []sample{{FirstTokenMS: 3000, CompleteMS: 30000}}),
	}
	out, err := Aggregate(runner, StatusCompleted, "sender-confirmed", attempts)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	doc := decodeLatency(t, out)
	if got := doc.Metrics.Latency.Hot.FirstTokenMS; got != 100 {
		t.Fatalf("hot first_token_ms = %v, want median 100 (mean would be 1040)", got)
	}
	if got := doc.Metrics.Latency.Hot.CompleteMS; got != 1000 {
		t.Fatalf("hot complete_ms = %v, want median 1000", got)
	}
	if got := doc.Metrics.Latency.Hot.Samples; got != 3 {
		t.Fatalf("hot samples = %d, want 3", got)
	}
}

// TestMedian pins the helper directly: empty, single, odd outlier, even averaging and unordered input.
// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
func TestMedian(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{42}, 42},
		{"odd-three-outlier", []float64{300, 10, 20}, 20},
		{"even-avg-two-middles", []float64{1, 2, 3, 4}, 2.5},
		{"even-unsorted", []float64{9, 1, 7, 3}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := median(tc.in); got != tc.want {
				t.Fatalf("median(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
