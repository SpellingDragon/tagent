// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package bench_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/bench"
	"github.com/SpellingDragon/tagent/bench/metrics"
	"github.com/SpellingDragon/tagent/bench/report"
	"github.com/SpellingDragon/tagent/bench/runners"
)

// taskRunner is the shared task interface every party satisfies.
type taskRunner interface {
	Run(ctx context.Context, task bench.Task, workspace string) ([]byte, error)
}

// fakeModel is the zero-quota model face: it answers every chat request with
// the fixed body and counts how many requests reached it.
type fakeModel struct {
	server   *httptest.Server
	answer   string
	mu       sync.Mutex
	requests int
}

func newFakeModel(t *testing.T, answer string) *fakeModel {
	t.Helper()
	fake := &fakeModel{answer: answer}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeModel) url() string { return f.server.URL }

func (f *fakeModel) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeModel) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"glm-5.3","object":"model"}]}`))
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()
	var request struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(raw, &request)
	if request.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []map[string]any{
			{"id": "bench-1", "object": "chat.completion.chunk", "created": 1, "model": "glm-5.3",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": f.answer}, "finish_reason": nil}}},
			{"id": "bench-1", "object": "chat.completion.chunk", "created": 1, "model": "glm-5.3",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}},
			{"id": "bench-1", "object": "chat.completion.chunk", "created": 1, "model": "glm-5.3",
				"choices": []any{}, "usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 2, "total_tokens": 14}},
		}
		for _, chunk := range chunks {
			encoded, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "bench-1", "object": "chat.completion", "created": 1, "model": "glm-5.3",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": f.answer}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 2, "total_tokens": 14},
	})
}

// resultEnvelope is the runner result surface the checks read.
type resultEnvelope struct {
	Protocol         string  `json:"protocol"`
	Runner           string  `json:"runner"`
	TaskID           string  `json:"task_id"`
	Status           string  `json:"status"`
	Output           string  `json:"output"`
	Delivered        bool    `json:"delivered"`
	APICalls         int     `json:"api_calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	FirstTokenMS     float64 `json:"first_token_ms"`
	CompleteMS       float64 `json:"complete_ms"`
	Error            string  `json:"error"`
}

func decodeResult(t *testing.T, raw []byte) resultEnvelope {
	t.Helper()
	var value resultEnvelope
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode result: %v: %s", err, raw)
	}
	return value
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the repository root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func findTask(t *testing.T, id string) bench.Task {
	t.Helper()
	tasks, err := bench.All()
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("task %s is not embedded", id)
	return bench.Task{}
}

// writeStub writes a headless CLI stub that answers the NDJSON event stream.
func writeStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsh-stub.sh")
	body := `#!/bin/sh
echo '{"type":"session","sessionId":"stub-session"}'
echo '{"type":"status","phase":"turn_start","turn":1}'
echo '{"type":"status","phase":"step_start","turn":1,"step":1}'
echo '{"type":"text","text":"391"}'
echo '{"type":"status","phase":"step_end","turn":1,"step":1,"usage":{"inputTokens":12,"outputTokens":2,"totalTokens":14}}'
echo '{"type":"status","phase":"turn_end","turn":1,"reason":{"kind":"completed"}}'
echo '{"type":"final","sessionId":"stub-session","reason":{"kind":"completed"},"text":"391"}'
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func newParties(t *testing.T, endpoint string) map[string]taskRunner {
	t.Helper()
	t.Setenv("BENCH_FAKE_KEY", "dummy")
	baseline, err := runners.NewBaseline(endpoint, "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	bridge, err := runners.BuildBridge(filepath.Join(t.TempDir(), "bin"))
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}
	tagent, err := runners.NewTagent(bridge, filepath.Join(repositoryRoot(t), "examples", "wechat-bot", "tagent.yaml"), endpoint, "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("tagent: %v", err)
	}
	dsh, err := runners.NewDSH(writeStub(t), nil, endpoint, "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("dsh: %v", err)
	}
	return map[string]taskRunner{"baseline": baseline, "tagent": tagent, "dsh": dsh}
}

func TestBenchHarnessScriptedEndToEnd(t *testing.T) {
	fake := newFakeModel(t, "391")
	task := findTask(t, "qa-01")
	parties := newParties(t, fake.url())
	root := t.TempDir()
	aggregates := make([]json.RawMessage, 0, len(parties))
	for _, name := range []string{"baseline", "tagent", "dsh"} {
		workspace := filepath.Join(root, name)
		if err := task.Prepare(workspace); err != nil {
			t.Fatalf("%s prepare: %v", name, err)
		}
		raw, err := parties[name].Run(context.Background(), task, workspace)
		if err != nil {
			t.Fatalf("%s run: %v", name, err)
		}
		result := decodeResult(t, raw)
		if result.Protocol != "bench-runner-v1" || result.Runner != name || result.TaskID != task.ID {
			t.Fatalf("%s envelope identity: %+v", name, result)
		}
		if result.Status != "ok" || !result.Delivered {
			t.Fatalf("%s did not deliver: %+v", name, result)
		}
		if result.Output != "391" {
			t.Fatalf("%s output = %q, want 391", name, result.Output)
		}
		if result.APICalls < 1 || result.PromptTokens != 12 || result.CompletionTokens != 2 {
			t.Fatalf("%s usage: %+v", name, result)
		}
		if result.FirstTokenMS <= 0 || result.CompleteMS <= 0 {
			t.Fatalf("%s timings: %+v", name, result)
		}
		if err := task.Verify(workspace, result.Output); err != nil {
			t.Fatalf("%s verifier: %v", name, err)
		}
		attempt, err := metrics.NewAttempt(map[string]any{
			"runner":            name,
			"task_id":           task.ID,
			"category":          task.Category,
			"repeat":            1,
			"success":           true,
			"delivered":         true,
			"api_calls":         result.APICalls,
			"prompt_tokens":     result.PromptTokens,
			"completion_tokens": result.CompletionTokens,
			"approval_calls":    0,
			"blocked_reports":   0,
			"cold":              map[string]any{"first_token_ms": result.FirstTokenMS, "complete_ms": result.CompleteMS},
			"hot":               []any{},
		})
		if err != nil {
			t.Fatalf("%s attempt: %v", name, err)
		}
		aggregate, err := metrics.Aggregate(name, metrics.StatusCompleted, "scripted", [][]byte{attempt})
		if err != nil {
			t.Fatalf("%s aggregate: %v", name, err)
		}
		aggregates = append(aggregates, json.RawMessage(aggregate))
	}
	document, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"run_id":         "bench-scripted",
		"status":         metrics.StatusCompleted,
		"runners":        aggregates,
	})
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	if err := metrics.ValidateRun(document); err != nil {
		t.Fatalf("metrics document: %v", err)
	}
	rendered, err := report.Render(document)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	for _, name := range []string{"baseline", "tagent", "dsh"} {
		if !strings.Contains(string(rendered), name) {
			t.Fatalf("report misses %s:\n%s", name, rendered)
		}
	}
	if got := fake.count(); got < 2 {
		t.Fatalf("fake model received %d requests, want at least 2", got)
	}
}

func TestRunnerProtocolSubprocess(t *testing.T) {
	fake := newFakeModel(t, "391")
	t.Setenv("BENCH_FAKE_KEY", "dummy")
	bridge, err := runners.BuildBridge(t.TempDir())
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}
	config := filepath.Join(repositoryRoot(t), "examples", "wechat-bot", "tagent.yaml")
	describe, err := exec.Command(bridge, "--describe", "--wechat-config", config).Output()
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	var advertised struct {
		Protocol     string              `json:"protocol"`
		Runner       string              `json:"runner"`
		Tools        []string            `json:"tools"`
		Capabilities map[string][]string `json:"capabilities"`
	}
	if err := json.Unmarshal(describe, &advertised); err != nil {
		t.Fatalf("describe shape: %v: %s", err, describe)
	}
	if advertised.Protocol != "bench-runner-v1" || advertised.Runner != "tagent" {
		t.Fatalf("describe identity: %+v", advertised)
	}
	for _, want := range []string{"read_file", "save_file", "exec_run", "exec_release"} {
		if !contains(advertised.Tools, want) {
			t.Fatalf("describe tools miss %s: %v", want, advertised.Tools)
		}
	}
	tagent, err := runners.NewTagent(bridge, config, fake.url(), "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("tagent: %v", err)
	}
	task := findTask(t, "qa-01")
	workspace := t.TempDir()
	if err := task.Prepare(workspace); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	raw, err := tagent.Run(context.Background(), task, workspace)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	result := decodeResult(t, raw)
	if result.Status != "ok" || !result.Delivered || result.Output != "391" || result.APICalls != 1 {
		t.Fatalf("bridge result: %+v", result)
	}
	if err := task.Verify(workspace, result.Output); err != nil {
		t.Fatalf("verifier: %v", err)
	}
	if got := fake.count(); got != 1 {
		t.Fatalf("fake model received %d requests, want 1", got)
	}
}

func TestBaselineRunnerRejectsInvalidTask(t *testing.T) {
	fake := newFakeModel(t, "391")
	t.Setenv("BENCH_FAKE_KEY", "dummy")
	baseline, err := runners.NewBaseline(fake.url(), "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	for _, invalid := range []bench.Task{
		{ID: "empty"},
		{ID: "no-verifier", Prompts: []string{"hello"}},
		{ID: "blank-prompt", Prompts: []string{"  "}, Verifier: findTask(t, "qa-01").Verifier},
	} {
		if _, err := baseline.Run(context.Background(), invalid, t.TempDir()); err == nil {
			t.Fatalf("baseline accepted invalid task %+v", invalid)
		}
	}
	if got := fake.count(); got != 0 {
		t.Fatalf("invalid tasks reached the model %d times", got)
	}
}

func TestRunnersShareTaskInterface(t *testing.T) {
	fake := newFakeModel(t, "391")
	t.Setenv("BENCH_FAKE_KEY", "dummy")
	baseline, err := runners.NewBaseline(fake.url(), "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	bridge, err := runners.BuildBridge(t.TempDir())
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}
	config := filepath.Join(repositoryRoot(t), "examples", "wechat-bot", "tagent.yaml")
	tagent, err := runners.NewTagent(bridge, config, fake.url(), "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("tagent: %v", err)
	}
	dsh, err := runners.NewDSH(writeStub(t), nil, fake.url(), "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("dsh: %v", err)
	}
	parties := map[string]taskRunner{"baseline": baseline, "tagent": tagent, "dsh": dsh}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(runners.CapabilityMap(), &document); err != nil {
		t.Fatalf("capability map: %v", err)
	}
	mapping := map[string]map[string][]string{}
	for _, name := range []string{"baseline", "tagent", "dsh"} {
		var value map[string][]string
		if err := json.Unmarshal(document[name], &value); err != nil {
			t.Fatalf("capability map of %s: %v", name, err)
		}
		mapping[name] = value
	}
	describe, err := exec.Command(bridge, "--describe", "--wechat-config", config).Output()
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	var advertised struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(describe, &advertised); err != nil {
		t.Fatalf("describe shape: %v", err)
	}
	faces := map[string][]string{
		"baseline": runners.BaselineToolFace(),
		"tagent":   advertised.Tools,
		"dsh":      runners.DSHToolFace(),
	}
	keys := []string{"file.read", "file.write", "exec"}
	for _, name := range []string{"baseline", "tagent", "dsh"} {
		if _, ok := parties[name]; !ok {
			t.Fatalf("party %s is missing", name)
		}
		mapped := make([]string, 0, 8)
		for _, key := range keys {
			tools := mapping[name][key]
			if len(tools) == 0 {
				t.Fatalf("%s capability %s is empty", name, key)
			}
			mapped = append(mapped, tools...)
		}
		sort.Strings(mapped)
		face := append([]string(nil), faces[name]...)
		sort.Strings(face)
		if strings.Join(mapped, ",") != strings.Join(face, ",") {
			t.Fatalf("%s capability-to-face mismatch: mapped %v, face %v", name, mapped, face)
		}
	}
}

func TestDSHRunnerExecutesRealTask(t *testing.T) {
	if os.Getenv("BENCH_DSH_REAL") != "1" {
		t.Skip("BENCH_DSH_REAL is not set: the real DeepSeek Harness execution is opt-in")
	}
	node, arguments, err := runners.DSHExecutable("")
	if err != nil {
		t.Skipf("DeepSeek Harness checkout is absent: %v", err)
	}
	checkout := os.Getenv("BENCH_DSH_CHECKOUT")
	if checkout == "" {
		checkout = "/Users/pengweiye/Documents/codes/deepseek-harness"
	}
	before := gitStatus(t, checkout)
	fake := newFakeModel(t, "391")
	t.Setenv("BENCH_FAKE_KEY", "dummy")
	task := findTask(t, "qa-01")
	workspace := t.TempDir()
	if err := task.Prepare(workspace); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	dsh, err := runners.NewDSH(node, arguments, fake.url(), "BENCH_FAKE_KEY", "glm-5.3")
	if err != nil {
		t.Fatalf("dsh: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	raw, err := dsh.Run(ctx, task, workspace)
	if err != nil {
		t.Fatalf("real dsh run: %v", err)
	}
	result := decodeResult(t, raw)
	if result.Status != "ok" || !result.Delivered || result.Output != "391" {
		t.Fatalf("real dsh result: %+v", result)
	}
	if result.APICalls < 1 || result.PromptTokens != 12 || result.CompletionTokens != 2 {
		t.Fatalf("real dsh usage: %+v", result)
	}
	if err := task.Verify(workspace, result.Output); err != nil {
		t.Fatalf("real dsh verifier: %v", err)
	}
	if after := gitStatus(t, checkout); after != before {
		t.Fatalf("DeepSeek Harness checkout changed: before %q, after %q", before, after)
	}
}

func TestRunComparisonRecordsSkip(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	dir, err := runners.RunComparison(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("run comparison: %v", err)
	}
	rawRecord, err := os.ReadFile(filepath.Join(dir, bench.RunFileName))
	if err != nil {
		t.Fatalf("run record: %v", err)
	}
	var record struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rawRecord, &record); err != nil {
		t.Fatalf("run record shape: %v", err)
	}
	if record.Status != metrics.StatusSkipped || !strings.Contains(record.Reason, "ZAI_API_KEY") {
		t.Fatalf("skip record: %+v", record)
	}
	rawMetrics, err := os.ReadFile(filepath.Join(dir, bench.MetricsFileName))
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if err := metrics.ValidateRun(rawMetrics); err != nil {
		t.Fatalf("skipped metrics: %v", err)
	}
	rawReport, err := os.ReadFile(filepath.Join(dir, bench.ReportFileName))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(string(rawReport), "skipped") {
		t.Fatalf("report misses the skip status: %s", rawReport)
	}
}

func TestRunDirContainsAllThreeRunners(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "bench", "runs")
	if os.Getenv("BENCH_REAL_RUN") != "1" {
		if dir := latestCompleteRun(t, root); dir != "" {
			assertCompletedRun(t, dir, 0)
			return
		}
	}
	if os.Getenv("ZAI_API_KEY") == "" {
		t.Skip("ZAI_API_KEY is not set and no completed run exists: the change ledger records this checkpoint blocked")
	}
	if os.Getenv("BENCH_REAL_RUN") != "1" {
		t.Skip("set BENCH_REAL_RUN=1 to spend quota on the real three-party comparison")
	}
	dir, err := runners.RunComparison(root, 3)
	if err != nil {
		t.Fatalf("run comparison: %v", err)
	}
	assertCompletedRun(t, dir, 3)
}

func gitStatus(t *testing.T, checkout string) string {
	t.Helper()
	command := exec.Command("git", "-C", checkout, "status", "--porcelain")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Skipf("cannot read the DeepSeek Harness checkout status: %v: %s", err, output)
	}
	return string(output)
}

func latestCompleteRun(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	latest := ""
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "bench-") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		raw, err := os.ReadFile(filepath.Join(dir, bench.RunFileName))
		if err != nil {
			continue
		}
		var record struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &record); err != nil || record.Status != metrics.StatusCompleted {
			continue
		}
		if entry.Name() > latest {
			latest = entry.Name()
		}
	}
	if latest == "" {
		return ""
	}
	return filepath.Join(root, latest)
}

func assertCompletedRun(t *testing.T, dir string, repeats int) {
	t.Helper()
	name := filepath.Base(dir)
	rawRecord, err := os.ReadFile(filepath.Join(dir, bench.RunFileName))
	if err != nil {
		t.Fatalf("run record: %v", err)
	}
	var record struct {
		RunID   string `json:"run_id"`
		Status  string `json:"status"`
		Repeats int    `json:"repeats"`
		Tasks   int    `json:"tasks"`
	}
	if err := json.Unmarshal(rawRecord, &record); err != nil {
		t.Fatalf("run record shape: %v", err)
	}
	if record.Status != metrics.StatusCompleted {
		t.Fatalf("run status = %q, want completed", record.Status)
	}
	if record.RunID != name || !strings.HasPrefix(record.RunID, "bench-") {
		t.Fatalf("run id %q does not name directory %q", record.RunID, name)
	}
	if repeats > 0 && record.Repeats != repeats {
		t.Fatalf("run repeats = %d, want %d", record.Repeats, repeats)
	}
	if record.Tasks != 12 {
		t.Fatalf("run tasks = %d, want 12", record.Tasks)
	}
	rawMetrics, err := os.ReadFile(filepath.Join(dir, bench.MetricsFileName))
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if err := metrics.ValidateRun(rawMetrics); err != nil {
		t.Fatalf("metrics document: %v", err)
	}
	var document struct {
		Runners []struct {
			Runner string `json:"runner"`
		} `json:"runners"`
	}
	if err := json.Unmarshal(rawMetrics, &document); err != nil {
		t.Fatalf("metrics shape: %v", err)
	}
	seen := map[string]bool{}
	for _, entry := range document.Runners {
		seen[entry.Runner] = true
	}
	for _, want := range []string{"baseline", "tagent", "dsh"} {
		if !seen[want] {
			t.Fatalf("metrics miss runner %s", want)
		}
	}
	rawReport, err := os.ReadFile(filepath.Join(dir, bench.ReportFileName))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	for _, want := range []string{"baseline", "tagent", "dsh"} {
		if !strings.Contains(string(rawReport), want) {
			t.Fatalf("report misses %s", want)
		}
	}
	if repeats > 0 {
		for _, runner := range []string{"baseline", "tagent", "dsh"} {
			entries, err := os.ReadDir(filepath.Join(dir, bench.TaskDirName, runner))
			if err != nil {
				t.Fatalf("details of %s: %v", runner, err)
			}
			if len(entries) != record.Tasks*repeats {
				t.Fatalf("%s details = %d, want %d", runner, len(entries), record.Tasks*repeats)
			}
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
