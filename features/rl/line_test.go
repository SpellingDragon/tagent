package rl_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/bench"
	benchfixtures "github.com/SpellingDragon/tagent/bench/tasks"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/features/rl/sample"
)

// TestSampleLoopEndToEnd pins the five-task scripted loop from request read-back through independent acceptance to a sealed 5/5 readiness rate.
// - each task's request is fixed and read back with hash reconciliation
// - each artifact is judged independently of the execution's self-report
// - the verdict is in the annex before the seal and the rate counts all five samples
// - the positive fixture examples supply the artifacts, so no real model quota is spent
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestSampleLoopEndToEnd(t *testing.T) {
	ctx := context.Background()
	tasks := scriptedTasks(t)
	if len(tasks) != 5 {
		t.Fatalf("scripted sample set = %d tasks, want 5", len(tasks))
	}
	runDir := t.TempDir()
	recordDir := filepath.Join(runDir, "records")
	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		t.Fatalf("create the record directory: %v", err)
	}
	writer, err := rl.NewFileCaptureWriter(recordDir, 4)
	if err != nil {
		t.Fatalf("open the capture writer: %v", err)
	}
	sink, err := rl.NewCaptureSink("cap:sample-loop", rl.SinkConfig{}, writer)
	if err != nil {
		t.Fatalf("open the capture sink: %v", err)
	}
	capture, err := rl.NewTrajectoryCaptureWithBudget("cap:sample-loop", sink, rl.SnapshotPolicy{}, rl.CaptureBudget{SwapDir: filepath.Join(runDir, "swap")})
	if err != nil {
		t.Fatalf("open the trajectory capture: %v", err)
	}

	runEpisode := event.EpisodeID("episode:sample-loop")
	annexes := make([]rl.AnnexRecord, 0, len(tasks))
	readbacks := make([]bool, 0, len(tasks))
	refs := make([]rl.SnapshotRef, 0, len(tasks))
	reportSamples := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		positive := scriptedPositive(t, task)
		workspace := t.TempDir()
		if err := task.Prepare(workspace); err != nil {
			t.Fatalf("prepare %s: %v", task.ID, err)
		}
		writeScriptedArtifacts(t, workspace, positive.Files)

		decisionID := event.DecisionID("decision:" + task.ID)
		attemptID := event.AttemptID("attempt:" + task.ID)
		request := model.Request{Messages: scriptedMessages(task)}
		view := rl.ModelViewRef{
			Model:         "scripted",
			ModelVersion:  "scripted-v1",
			PromptVersion: "fixture-v1",
			VisibleTools:  scriptedTools(task),
		}
		fixed, err := capture.Scope(view, decisionID).Snapshot(ctx, request)
		if err != nil {
			t.Fatalf("%s: fix the request snapshot: %v", task.ID, err)
		}
		if err := sink.RecordAttempt(ctx, rl.AttemptRecord{
			Schema:     rl.SchemaCurrent,
			ID:         attemptID,
			DecisionID: decisionID,
			EpisodeID:  runEpisode,
			Response:   model.Response{},
			Terminal:   model.KindCompleted,
			Timestamp:  time.Now(),
		}); err != nil {
			t.Fatalf("%s: record the attempt: %v", task.ID, err)
		}
		if err := readbackProof(capture, fixed.Snapshot); err != nil {
			t.Fatalf("%s: request read-back failed before the seal: %v", task.ID, err)
		}
		annex := sample.Accept(attemptID, task, workspace, positive.Output)
		if annex.Verdict != sample.VerdictPass {
			t.Fatalf("%s: acceptance verdict = %q, want a pass over the scripted artifact", task.ID, annex.Verdict)
		}
		if annex.AttemptID != attemptID || annex.TaskID != task.ID {
			t.Fatalf("%s: annex identity = (%q, %q), want the sample identity", task.ID, annex.AttemptID, annex.TaskID)
		}
		annexes = append(annexes, annex)
		readbacks = append(readbacks, true)
		refs = append(refs, fixed.Snapshot)
		reportSamples = append(reportSamples, map[string]any{
			"attempt_id": attemptID,
			"task_id":    task.ID,
			"elements": map[string]bool{
				"request_readback":       true,
				"independent_acceptance": true,
				"verdict_in_annex":       true,
			},
			"verdict": annex.Verdict,
		})
	}

	total, complete, err := sample.Evaluate(ctx, sink, annexes, readbacks)
	if err != nil {
		t.Fatalf("evaluate the sample gate before the seal: %v", err)
	}
	if total != 5 || complete != 5 {
		t.Fatalf("gate counts = (%d, %d), want 5/5 complete samples", total, complete)
	}
	manifest, err := capture.Seal(ctx)
	if err != nil {
		t.Fatalf("seal the run: %v", err)
	}
	if !manifest.Sealed || manifest.Partial || manifest.Records != 10 || manifest.Dropped != 0 {
		t.Fatalf("manifest = %+v, want five attempt lines and five annex lines sealed with no loss", manifest)
	}
	for _, ref := range refs {
		if err := readbackProof(capture, ref); err != nil {
			t.Fatalf("request read-back failed after the seal: %v", err)
		}
	}
	attempts, statuses := storedLineCounts(t, filepath.Join(recordDir, rl.SinkRecordsFile))
	if attempts != 5 || statuses["pass"] != 5 {
		t.Fatalf("stored lines = attempts %d, verdicts %v, want five attempts and five pass verdicts", attempts, statuses)
	}

	rate := float64(complete) / float64(total)
	if rate != 1.0 {
		t.Fatalf("readiness rate = %v, want 100%% over all five samples", rate)
	}
	report := map[string]any{
		"run_id":        "sample-loop-scripted",
		"total":         total,
		"complete":      complete,
		"denominator":   total,
		"readiness":     rate,
		"manifest":      manifest,
		"sample_status": reportSamples,
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("encode the sample readiness report: %v", err)
	}
	reportPath := filepath.Join(runDir, "sample-readiness.json")
	if err := os.WriteFile(reportPath, content, 0o644); err != nil {
		t.Fatalf("write the sample readiness report: %v", err)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("read the sample readiness report back: %v", err)
	}
	t.Logf("sample readiness: total=%d complete=%d rate=%.0f%% report=%s", total, complete, rate*100, reportPath)
}

// scriptedFixture is the part of one bench task fixture a scripted run needs:
// the task identity and the positive example the loop executes without a model.
type scriptedFixture struct {
	ID       string `json:"id"`
	Examples struct {
		Positive scriptedArtifact `json:"positive"`
	} `json:"examples"`
}

// scriptedArtifact is the artifact outcome one scripted task execution writes:
// the final response and the files the independent verifier reads.
type scriptedArtifact struct {
	Output string            `json:"output"`
	Files  map[string]string `json:"files"`
}

// scriptedTasks loads the five bench task fixtures the scripted run covers: one
// per category that needs a durable artifact, one multi-turn task and a second
// qa task.
func scriptedTasks(t *testing.T) []bench.Task {
	t.Helper()
	tasks, err := bench.All()
	if err != nil {
		t.Fatalf("load the bench task set: %v", err)
	}
	byID := make(map[string]bench.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	ids := []string{"qa-01", "file-01", "exec-01", "multi-01", "qa-02"}
	selected := make([]bench.Task, 0, len(ids))
	for _, id := range ids {
		task, ok := byID[id]
		if !ok {
			t.Fatalf("bench task %q is absent from the embedded set", id)
		}
		selected = append(selected, task)
	}
	return selected
}

// scriptedPositive reads the positive fixture example of one task: it is the
// task's declared artifact outcome, not a model claim.
func scriptedPositive(t *testing.T, task bench.Task) scriptedArtifact {
	t.Helper()
	raw, err := benchfixtures.FS().Open(task.ID + ".json")
	if err != nil {
		t.Fatalf("%s: open the fixture: %v", task.ID, err)
	}
	defer raw.Close()
	var fixture scriptedFixture
	if err := json.NewDecoder(raw).Decode(&fixture); err != nil {
		t.Fatalf("%s: decode the fixture: %v", task.ID, err)
	}
	if fixture.ID != task.ID {
		t.Fatalf("%s: fixture id = %q", task.ID, fixture.ID)
	}
	return fixture.Examples.Positive
}

// writeScriptedArtifacts writes the positive example's files into the task
// workspace, so the independent verifier judges a real artifact tree.
func writeScriptedArtifacts(t *testing.T, workspace string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(workspace, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the artifact directory for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write the artifact %s: %v", rel, err)
		}
	}
}

// scriptedMessages turns the task prompts into the request messages the
// capture fixes for the sample.
func scriptedMessages(task bench.Task) []model.Message {
	messages := make([]model.Message, 0, len(task.Prompts))
	for _, prompt := range task.Prompts {
		messages = append(messages, model.Message{Role: model.RoleUser, Content: prompt})
	}
	return messages
}

// scriptedTools names the task's required capabilities as the visible tool
// members the request view was declared with.
func scriptedTools(task bench.Task) []rl.VersionedName {
	tools := make([]rl.VersionedName, 0, len(task.Tools))
	for _, tool := range task.Tools {
		tools = append(tools, rl.VersionedName{Name: tool, Version: "v1"})
	}
	return tools
}

// storedLineCounts reads the sealed record file back and reports the attempt
// lines and the annex verdicts it holds.
func storedLineCounts(t *testing.T, path string) (int, map[string]int) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the sealed record file: %v", err)
	}
	attempts := 0
	verdicts := map[string]int{}
	for _, raw := range splitLines(string(content)) {
		if raw == "" {
			continue
		}
		line, err := rl.ParseLine([]byte(raw))
		if err != nil {
			t.Fatalf("parse the sealed line: %v", err)
		}
		switch line.Kind {
		case rl.RecordKindAttempt:
			attempts++
		case rl.RecordKindAnnex:
			if annex := line.Annex(); annex != nil {
				verdicts[annex.Verdict]++
			}
		default:
			t.Fatalf("sealed line kind = %q, want a recorded sample line", line.Kind)
		}
	}
	return attempts, verdicts
}

// readbackProof reads one fixed request snapshot back through its reference
// and reconciles the read-back bytes with the recorded digest. It proves the
// sample's request element from the stored snapshot, not from a file that
// merely exists.
func readbackProof(capture *rl.TrajectoryCapture, ref rl.SnapshotRef) error {
	snapshot, err := capture.SnapshotOf(ref)
	if err != nil {
		return err
	}
	content, err := rl.MarshalSnapshot(snapshot)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(content)
	if got := hex.EncodeToString(sum[:]); got != ref.Digest {
		return fmt.Errorf("read-back digest %s, recorded %s", got, ref.Digest)
	}
	return nil
}

// splitLines splits one record file into its non-empty lines.
func splitLines(content string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			line := content[start:i]
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(content) && content[start:] != "" {
		lines = append(lines, content[start:])
	}
	return lines
}
