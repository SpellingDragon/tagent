package sample_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/bench"
	"github.com/SpellingDragon/tagent/features/rl/sample"
)

// TestIndependentAcceptanceIgnoresSelfReport pins that the verdict is read from task artifacts, not from the run's self-report.
// - a contradictory failure claim still passes when the artifact is present
// - a success claim still fails when the artifact is missing
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestIndependentAcceptanceIgnoresSelfReport(t *testing.T) {
	task := benchTask(t, "file-01")
	passWorkspace := t.TempDir()
	if err := task.Prepare(passWorkspace); err != nil {
		t.Fatalf("prepare the passing workspace: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(passWorkspace, "out"), 0o755); err != nil {
		t.Fatalf("create the artifact directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(passWorkspace, "out", "hello.txt"), []byte("hello bench"), 0o644); err != nil {
		t.Fatalf("write the passing artifact: %v", err)
	}
	passed := sample.Accept("attempt:self-report-denied", task, passWorkspace, "I could not create the file; the run failed.")
	if passed.AttemptID != "attempt:self-report-denied" || passed.TaskID != task.ID {
		t.Fatalf("acceptance identity = (%q, %q), want the attempt and the bench task", passed.AttemptID, passed.TaskID)
	}
	if passed.Verdict != sample.VerdictPass || passed.RuleVersion != sample.RuleVersionV1 {
		t.Fatalf("acceptance verdict = (%q, %q), want (%q, %q) from the artifacts alone", passed.Verdict, passed.RuleVersion, sample.VerdictPass, sample.RuleVersionV1)
	}
	if !hasArtifact(passed.Artifacts, "artifact:"+task.ID+"/workspace/out/hello.txt") {
		t.Fatalf("acceptance artifacts = %v, want the judged file referenced", passed.Artifacts)
	}

	failWorkspace := t.TempDir()
	if err := task.Prepare(failWorkspace); err != nil {
		t.Fatalf("prepare the failing workspace: %v", err)
	}
	failed := sample.Accept("attempt:self-report-claimed", task, failWorkspace, "SUCCESS: I created out/hello.txt with exactly the requested text.")
	if failed.Verdict != sample.VerdictFail {
		t.Fatalf("acceptance verdict = %q, want %q when the artifact is missing beside a successful self-report", failed.Verdict, sample.VerdictFail)
	}
	if !hasArtifact(failed.Artifacts, "artifact:"+task.ID+"/response/sha256:") {
		t.Fatalf("acceptance artifacts = %v, want the response digest referenced", failed.Artifacts)
	}
}

// benchTask loads one fixture from the embedded task set by id.
func benchTask(t *testing.T, id string) bench.Task {
	t.Helper()
	tasks, err := bench.All()
	if err != nil {
		t.Fatalf("load the bench task set: %v", err)
	}
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatalf("bench task %q not found in the embedded set", id)
	return bench.Task{}
}

// hasArtifact reports whether one artifact reference list carries the given
// prefix.
func hasArtifact(refs []string, prefix string) bool {
	for _, ref := range refs {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}
