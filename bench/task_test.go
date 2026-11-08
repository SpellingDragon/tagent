package bench

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestBenchTasksAllVerifiable verifies every fixture's positive example passes and its negative example is rejected by the declared verifier.
// - all twelve fixtures load and each category holds three tasks
// - every verifier kind (response, file, command) is exercised
// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
func TestBenchTasksAllVerifiable(t *testing.T) {
	tasks, err := All()
	if err != nil {
		t.Fatalf("load embedded task fixtures: %v", err)
	}
	if len(tasks) != 12 {
		t.Fatalf("task set has %d tasks, want 12", len(tasks))
	}
	byCategory := make(map[string]int)
	kinds := make(map[string]int)
	for _, task := range tasks {
		task := task
		byCategory[task.Category]++
		verifier, ok := task.Verifier.(*specVerifier)
		if !ok || verifier == nil {
			t.Fatalf("task %s: verifier is %T, want *specVerifier", task.ID, task.Verifier)
		}
		if verifier.response != nil {
			kinds["response"]++
		}
		if verifier.file != nil {
			kinds["file"]++
		}
		if verifier.command != nil {
			kinds["command"]++
		}
		t.Run(task.ID, func(t *testing.T) {
			verifyTaskExamples(t, task)
		})
	}
	for _, category := range []string{CategoryQA, CategoryFile, CategoryExec, CategoryMulti} {
		if byCategory[category] != 3 {
			t.Fatalf("category %s has %d tasks, want 3", category, byCategory[category])
		}
	}
	for _, kind := range []string{"response", "file", "command"} {
		if kinds[kind] == 0 {
			t.Fatalf("no task exercises the %s verifier", kind)
		}
	}
	t.Logf("verified 12 tasks: qa=3 file=3 exec=3 multi=3; verifier checks response=%d file=%d command=%d",
		kinds["response"], kinds["file"], kinds["command"])
}

func verifyTaskExamples(t *testing.T, task Task) {
	t.Helper()
	if len(task.Prompts) == 0 {
		t.Fatal("task has no prompts")
	}
	if task.Verifier == nil {
		t.Fatal("task has no verifier")
	}
	prepared := t.TempDir()
	if err := task.Prepare(prepared); err != nil {
		t.Fatalf("prepare fixture setup: %v", err)
	}
	for _, rel := range sortedKeys(task.setup) {
		if _, err := os.Stat(filepath.Join(prepared, rel)); err != nil {
			t.Fatalf("setup file %s missing after Prepare: %v", rel, err)
		}
	}
	positiveDir := writeExampleWorkspace(t, task.examples.Positive)
	if err := task.Verify(positiveDir, task.examples.Positive.Output); err != nil {
		t.Fatalf("positive example rejected: %v", err)
	}
	negativeDir := writeExampleWorkspace(t, task.examples.Negative)
	err := task.Verify(negativeDir, task.examples.Negative.Output)
	if err == nil {
		t.Fatal("negative example accepted")
	}
	if want := task.examples.Negative.ExpectErrorContains; !strings.Contains(err.Error(), want) {
		t.Fatalf("negative example rejected for wrong reason: want %q in %q", want, err.Error())
	}
}

func writeExampleWorkspace(t *testing.T, example *fixtureExample) string {
	t.Helper()
	dir := t.TempDir()
	keys := make([]string, 0, len(example.Files))
	for rel := range example.Files {
		keys = append(keys, rel)
	}
	sort.Strings(keys)
	for _, rel := range keys {
		path, err := workspaceWritePath(dir, rel)
		if err != nil {
			t.Fatalf("example file %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(example.Files[rel]), 0o644); err != nil {
			t.Fatalf("write example file %s: %v", rel, err)
		}
	}
	return dir
}
