// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package bench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"

	taskfixtures "github.com/SpellingDragon/tagent/bench/tasks"
)

// CategoryQA, CategoryFile, CategoryExec and CategoryMulti name the task
// categories; a category fixes the number of prompts a fixture declares:
// qa/file/exec are single-turn, multi is two or more dependent turns.
const (
	CategoryQA    = "qa"
	CategoryFile  = "file"
	CategoryExec  = "exec"
	CategoryMulti = "multi"
)

// ToolFileRead, ToolFileWrite and ToolExec name the capabilities a task may
// declare; runners map each capability onto their own tool surface, so the
// names are the mapping contract, not tool names.
const (
	ToolFileRead  = "file.read"
	ToolFileWrite = "file.write"
	ToolExec      = "exec"
)

var knownTools = map[string]bool{
	ToolFileRead:  true,
	ToolFileWrite: true,
	ToolExec:      true,
}

var taskIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*-[0-9]{2}$`)

// Task is one benchmark task declaration: the prompts a runner sends, the
// capabilities the task needs, the isolated setup it starts from, and the
// machine-readable verifier that decides success. A Task never carries a
// runner or a model; the same declaration drives all three runners.
type Task struct {
	// ID is the stable fixture id, equal to the fixture file base name.
	ID string
	// Category is one of the Category constants.
	Category string
	// Prompts are the user turns in order; single-turn tasks carry one.
	Prompts []string
	// Tools is the capability set the runner must cover.
	Tools []string
	// Verifier decides success from the workspace and the final response.
	Verifier Verifier

	setup    map[string]string
	examples fixtureExamples
}

// All returns the embedded 12-task fixture set, sorted by task id.
func All() ([]Task, error) {
	return LoadFS(taskfixtures.FS())
}

// Load reads task fixtures from a directory on disk.
func Load(dir string) ([]Task, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("bench: task fixture directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("bench: task fixture path %q is not a directory", dir)
	}
	return LoadFS(os.DirFS(dir))
}

// LoadFS reads every *.json fixture directly under fsys, in name order, and
// rejects duplicate ids, unknown fields, unsafe paths and incomplete verifiers.
func LoadFS(fsys fs.FS) ([]Task, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("bench: read task fixtures: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, errors.New("bench: no task fixtures")
	}
	tasks := make([]Task, 0, len(names))
	seen := make(map[string]string, len(names))
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("bench: read fixture %s: %w", name, err)
		}
		task, err := decodeTask(name, raw)
		if err != nil {
			return nil, err
		}
		if previous, ok := seen[task.ID]; ok {
			return nil, fmt.Errorf("bench: duplicate task id %q in %s and %s", task.ID, previous, name)
		}
		seen[task.ID] = name
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

// Prepare writes the task's declared setup files into workspace. The setup is
// part of the fixture, not model output: every runner starts from the same tree.
func (t Task) Prepare(workspace string) error {
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("bench task %s: workspace is empty", t.ID)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("bench task %s: create workspace: %w", t.ID, err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return fmt.Errorf("bench task %s: workspace: %w", t.ID, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("bench task %s: workspace %q is not a directory", t.ID, workspace)
	}
	for _, rel := range sortedKeys(t.setup) {
		path, err := workspaceWritePath(workspace, rel)
		if err != nil {
			return fmt.Errorf("bench task %s: setup %s: %w", t.ID, rel, err)
		}
		if err := os.WriteFile(path, []byte(t.setup[rel]), 0o644); err != nil {
			return fmt.Errorf("bench task %s: write setup %s: %w", t.ID, rel, err)
		}
	}
	return nil
}

// Verify runs the task's verifier over an artifact. It is the runner-facing
// shorthand; the verifier itself stays replaceable through the Task field.
func (t Task) Verify(workspace, output string) error {
	if t.Verifier == nil {
		return fmt.Errorf("bench task %s: no verifier", t.ID)
	}
	return t.Verifier.Verify(workspace, output)
}

type taskFixture struct {
	ID       string            `json:"id"`
	Category string            `json:"category"`
	Prompts  []string          `json:"prompts"`
	Tools    []string          `json:"tools"`
	Setup    map[string]string `json:"setup,omitempty"`
	Verifier verifierFixture   `json:"verifier"`
	Examples *fixtureExamples  `json:"examples"`
}

type fixtureExamples struct {
	Positive *fixtureExample `json:"positive"`
	Negative *fixtureExample `json:"negative"`
}

type fixtureExample struct {
	Output              string            `json:"output,omitempty"`
	Files               map[string]string `json:"files,omitempty"`
	ExpectErrorContains string            `json:"expect_error_contains,omitempty"`
}

func decodeTask(name string, raw []byte) (Task, error) {
	var fixture taskFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return Task{}, fmt.Errorf("bench: fixture %s: %w", name, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Task{}, fmt.Errorf("bench: fixture %s: trailing JSON data", name)
	}
	if err := fixture.validate(name); err != nil {
		return Task{}, err
	}
	verifier, err := fixture.Verifier.build()
	if err != nil {
		return Task{}, fmt.Errorf("bench: fixture %s: %w", name, err)
	}
	task := Task{
		ID:       fixture.ID,
		Category: fixture.Category,
		Prompts:  append([]string(nil), fixture.Prompts...),
		Tools:    append([]string(nil), fixture.Tools...),
		Verifier: verifier,
		examples: *fixture.Examples,
	}
	if len(fixture.Setup) > 0 {
		task.setup = make(map[string]string, len(fixture.Setup))
		for rel, content := range fixture.Setup {
			task.setup[rel] = content
		}
	}
	return task, nil
}

func (f *taskFixture) validate(name string) error {
	where := "bench: fixture " + name
	if !taskIDPattern.MatchString(f.ID) {
		return fmt.Errorf("%s: id %q must match %s", where, f.ID, taskIDPattern)
	}
	if want := strings.TrimSuffix(name, ".json"); f.ID != want {
		return fmt.Errorf("%s: id %q does not match file name %q", where, f.ID, want)
	}
	switch f.Category {
	case CategoryQA, CategoryFile, CategoryExec:
		if len(f.Prompts) != 1 {
			return fmt.Errorf("%s: category %s needs exactly 1 prompt, got %d", where, f.Category, len(f.Prompts))
		}
	case CategoryMulti:
		if len(f.Prompts) < 2 {
			return fmt.Errorf("%s: category %s needs at least 2 prompts, got %d", where, f.Category, len(f.Prompts))
		}
	default:
		return fmt.Errorf("%s: unknown category %q", where, f.Category)
	}
	for index, prompt := range f.Prompts {
		if strings.TrimSpace(prompt) == "" {
			return fmt.Errorf("%s: prompt %d is empty", where, index)
		}
	}
	used := make(map[string]bool, len(f.Tools))
	for _, tool := range f.Tools {
		if !knownTools[tool] {
			return fmt.Errorf("%s: unknown capability %q", where, tool)
		}
		if used[tool] {
			return fmt.Errorf("%s: duplicate capability %q", where, tool)
		}
		used[tool] = true
	}
	if f.Category == CategoryFile && !used[ToolFileWrite] {
		return fmt.Errorf("%s: file task must declare %q", where, ToolFileWrite)
	}
	if f.Category == CategoryExec && !used[ToolExec] {
		return fmt.Errorf("%s: exec task must declare %q", where, ToolExec)
	}
	for rel := range f.Setup {
		if _, err := safeRelativePath(rel); err != nil {
			return fmt.Errorf("%s: setup %w", where, err)
		}
	}
	if err := f.Verifier.validate(); err != nil {
		return fmt.Errorf("%s: verifier: %w", where, err)
	}
	if f.Examples == nil {
		return fmt.Errorf("%s: examples are required", where)
	}
	if f.Examples.Positive == nil {
		return fmt.Errorf("%s: examples.positive is required", where)
	}
	if f.Examples.Negative == nil {
		return fmt.Errorf("%s: examples.negative is required", where)
	}
	if err := f.Examples.Positive.validate(false); err != nil {
		return fmt.Errorf("%s: examples.positive: %w", where, err)
	}
	if err := f.Examples.Negative.validate(true); err != nil {
		return fmt.Errorf("%s: examples.negative: %w", where, err)
	}
	return nil
}

func (e *fixtureExample) validate(negative bool) error {
	if negative {
		if strings.TrimSpace(e.ExpectErrorContains) == "" {
			return errors.New("negative example must declare expect_error_contains")
		}
	} else if e.ExpectErrorContains != "" {
		return errors.New("positive example must not declare expect_error_contains")
	}
	for rel := range e.Files {
		if _, err := safeRelativePath(rel); err != nil {
			return err
		}
	}
	return nil
}
