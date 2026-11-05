// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package runners

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/bench"
	"github.com/SpellingDragon/tagent/bench/metrics"
	"github.com/SpellingDragon/tagent/bench/report"
)

// benchEndpoint is the ZAI OpenAI-compatible coding gateway the wechat-bot
// deployment already uses.
const benchEndpoint = "https://open.bigmodel.cn/api/coding/paas/v4"

// benchModel is the model id every runner samples.
const benchModel = "glm-5.3"

// benchKeyEnv names the environment variable that holds the API key.
const benchKeyEnv = "ZAI_API_KEY"

// caliberBaseline names the baseline delivery caliber: the task terminal.
const caliberBaseline = "task terminal: the loop closed on a completed assistant body"

// caliberTagent names the tagent delivery caliber: the channel Send receipt.
const caliberTagent = "sender channel receipt: the host accepted the body on its outbound face"

// caliberDSH names the DSH delivery caliber: the headless final event.
const caliberDSH = "headless final event: the CLI reported final text and exited zero"

// RunComparison runs baseline, tagent and DeepSeek Harness over the embedded
// task set and writes one run directory under runRoot. It returns the run
// directory; a skipped run (no API key) is written and returned without error.
func RunComparison(runRoot string, repeats int) (string, error) {
	if runRoot == "" {
		return "", fmt.Errorf("bench run: run root is empty")
	}
	if repeats < 1 {
		repeats = 1
	}
	startedAt := time.Now().UTC()
	runID := newRunID(startedAt)
	files := map[string][]byte{}
	record := map[string]any{
		"schema_version": 1,
		"run_id":         runID,
		"status":         metrics.StatusCompleted,
		"started_at":     startedAt.Format(time.RFC3339),
		"repeats":        repeats,
		"model":          modelRecord(),
	}
	writeSkipped := func(status, reason string) (string, error) {
		record["status"] = status
		record["reason"] = reason
		record["finished_at"] = time.Now().UTC().Format(time.RFC3339)
		record["api_calls"] = 0
		rawRecord, err := json.Marshal(record)
		if err != nil {
			return "", err
		}
		rawMetrics, err := json.Marshal(map[string]any{
			"schema_version": 1,
			"run_id":         runID,
			"status":         status,
			"reason":         reason,
			"model":          modelRecord(),
			"api_calls":      0,
			"runners":        []any{},
		})
		if err != nil {
			return "", err
		}
		rawReport, err := report.Render(rawMetrics)
		if err != nil {
			return "", err
		}
		files[bench.RunFileName] = rawRecord
		files[bench.MetricsFileName] = rawMetrics
		files[bench.ReportFileName] = rawReport
		files[bench.CapabilitiesFileName] = CapabilityMap()
		return bench.WriteRun(runRoot, runID, files)
	}
	if os.Getenv(benchKeyEnv) == "" {
		return writeSkipped(metrics.StatusSkipped, benchKeyEnv+" is not set: the run recorded a skip instead of spending quota")
	}
	tasks, err := bench.All()
	if err != nil {
		return "", err
	}
	record["tasks"] = len(tasks)
	root, err := repositoryRoot()
	if err != nil {
		return "", err
	}
	node, nodeArgs, err := DSHExecutable("")
	if err != nil {
		return writeSkipped(metrics.StatusBlocked, err.Error())
	}
	workRoot, err := os.MkdirTemp("", "bench-work-*")
	if err != nil {
		return "", fmt.Errorf("bench run: work root: %w", err)
	}
	defer os.RemoveAll(workRoot)
	bridge, err := BuildBridge(filepath.Join(workRoot, "bin"))
	if err != nil {
		return writeSkipped(metrics.StatusBlocked, err.Error())
	}
	baseline, err := NewBaseline(benchEndpoint, benchKeyEnv, benchModel)
	if err != nil {
		return writeSkipped(metrics.StatusBlocked, err.Error())
	}
	tagent, err := NewTagent(bridge, filepath.Join(root, "examples", "wechat-bot", "tagent.yaml"), benchEndpoint, benchKeyEnv, benchModel)
	if err != nil {
		return writeSkipped(metrics.StatusBlocked, err.Error())
	}
	dsh, err := NewDSH(node, nodeArgs, benchEndpoint, benchKeyEnv, benchModel)
	if err != nil {
		return writeSkipped(metrics.StatusBlocked, err.Error())
	}
	type runnerCase struct {
		name    string
		runner  runner
		caliber string
	}
	cases := []runnerCase{
		{name: "baseline", runner: baseline, caliber: caliberBaseline},
		{name: "tagent", runner: tagent, caliber: caliberTagent},
		{name: "dsh", runner: dsh, caliber: caliberDSH},
	}
	type outcome struct {
		entry    [][]byte
		details  map[string][]byte
		apiCalls int
		failure  string
	}
	outcomes := make([]outcome, len(cases))
	var wg sync.WaitGroup
	for index := range cases {
		current := cases[index]
		wg.Add(1)
		go func(index int, value runnerCase) {
			defer wg.Done()
			entry, details, apiCalls, failure := runCases(context.Background(), value.name, value.runner, tasks, repeats, workRoot)
			outcomes[index] = outcome{entry: entry, details: details, apiCalls: apiCalls, failure: failure}
		}(index, current)
	}
	wg.Wait()
	runnersRaw := make([]json.RawMessage, 0, len(cases))
	totalCalls := 0
	allDetails := map[string][]byte{}
	for index, runnerCase := range cases {
		result := outcomes[index]
		if result.failure != "" {
			return writeSkipped(metrics.StatusBlocked, result.failure)
		}
		raw, err := metrics.Aggregate(runnerCase.name, metrics.StatusCompleted, runnerCase.caliber, result.entry)
		if err != nil {
			return "", err
		}
		if err := metrics.ValidateRunner(raw); err != nil {
			return "", fmt.Errorf("bench run: runner %s metrics: %w", runnerCase.name, err)
		}
		runnersRaw = append(runnersRaw, raw)
		totalCalls += result.apiCalls
		for name, content := range result.details {
			allDetails[filepath.Join(bench.TaskDirName, runnerCase.name, name)] = content
		}
	}
	rawMetrics, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"run_id":         runID,
		"status":         metrics.StatusCompleted,
		"model":          modelRecord(),
		"api_calls":      totalCalls,
		"runners":        runnersRaw,
	})
	if err != nil {
		return "", err
	}
	if err := metrics.ValidateRun(rawMetrics); err != nil {
		return "", fmt.Errorf("bench run: metrics document: %w", err)
	}
	rawReport, err := report.Render(rawMetrics)
	if err != nil {
		return "", err
	}
	record["finished_at"] = time.Now().UTC().Format(time.RFC3339)
	record["api_calls"] = totalCalls
	record["runners"] = []string{"baseline", "tagent", "dsh"}
	rawRecord, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	files[bench.RunFileName] = rawRecord
	files[bench.MetricsFileName] = rawMetrics
	files[bench.ReportFileName] = rawReport
	files[bench.CapabilitiesFileName] = CapabilityMap()
	for name, content := range allDetails {
		files[name] = content
	}
	return bench.WriteRun(runRoot, runID, files)
}

// runCases executes one runner over every task and repeat and returns its
// attempt documents.
func runCases(ctx context.Context, name string, value runner, tasks []bench.Task, repeats int, workRoot string) ([][]byte, map[string][]byte, int, string) {
	attempts := make([][]byte, 0, len(tasks)*repeats)
	details := map[string][]byte{}
	apiCalls := 0
	for _, task := range tasks {
		for repeat := 1; repeat <= repeats; repeat++ {
			workspace := filepath.Join(workRoot, name, fmt.Sprintf("%s-%d", task.ID, repeat))
			started := time.Now().UTC()
			start := time.Now()
			if err := task.Prepare(workspace); err != nil {
				return nil, nil, 0, fmt.Sprintf("bench run: %s/%s workspace: %v", name, task.ID, err)
			}
			rawResult, runErr := value.Run(ctx, task, workspace)
			elapsed := milliseconds(time.Since(start))
			output := ""
			delivered := false
			runnerStatus := ""
			success := false
			errorText := ""
			verifierError := ""
			apiCallsThis := 0
			promptTokens := 0
			completionTokens := 0
			coldFirst := elapsed
			hot := []hotSample{}
			if runErr != nil {
				errorText = runErr.Error()
			} else {
				decoded, decodeErr := decodeResult(rawResult)
				if decodeErr != nil {
					errorText = decodeErr.Error()
				} else {
					output = decoded.Output
					delivered = decoded.Delivered
					runnerStatus = decoded.Status
					apiCallsThis = decoded.APICalls
					promptTokens = decoded.PromptTokens
					completionTokens = decoded.CompletionTokens
					coldFirst = decoded.FirstTokenMS
					hot = decoded.Hot
					errorText = decoded.Error
					if decoded.Status != "ok" && errorText == "" {
						errorText = "runner reported status " + decoded.Status
					}
				}
			}
			verifyErr := task.Verify(workspace, output)
			if verifyErr != nil {
				verifierError = verifyErr.Error()
			}
			success = runErr == nil && runnerStatus == "ok" && delivered && verifierError == ""
			apiCalls += apiCallsThis
			attempt, attemptErr := metrics.NewAttempt(map[string]any{
				"runner":            name,
				"task_id":           task.ID,
				"category":          task.Category,
				"repeat":            repeat,
				"success":           success,
				"delivered":         delivered,
				"api_calls":         apiCallsThis,
				"prompt_tokens":     promptTokens,
				"completion_tokens": completionTokens,
				"approval_calls":    0,
				"blocked_reports":   0,
				"cold":              map[string]any{"first_token_ms": coldFirst, "complete_ms": elapsed},
				"hot":               hot,
				"error":             errorText,
				"verifier_error":    verifierError,
				"output":            output,
				"started_at":        started.Format(time.RFC3339Nano),
				"finished_at":       time.Now().UTC().Format(time.RFC3339Nano),
			})
			if attemptErr != nil {
				return nil, nil, 0, fmt.Sprintf("bench run: %s/%s attempt: %v", name, task.ID, attemptErr)
			}
			attempts = append(attempts, attempt)
			details[fmt.Sprintf("%s-%d.json", task.ID, repeat)] = attempt
			os.RemoveAll(workspace)
		}
	}
	return attempts, details, apiCalls, ""
}

// modelRecord is the model block every run document carries.
func modelRecord() map[string]any {
	return map[string]any{
		"provider":    "zhipu",
		"model":       benchModel,
		"endpoint":    benchEndpoint,
		"temperature": relayTemperature,
		"max_tokens":  relayMaxTokens,
	}
}

// newRunID mints one UTC run identity.
func newRunID(at time.Time) string {
	var suffix [3]byte
	_, _ = rand.Read(suffix[:])
	return "bench-" + at.Format("20060102T150405Z") + "-" + hex.EncodeToString(suffix[:])
}
