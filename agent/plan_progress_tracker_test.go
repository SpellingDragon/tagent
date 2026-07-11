package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestScanActiveChanges_NoDirectory tests when openspec/changes/ doesn't exist.
func TestScanActiveChanges_NoDirectory(t *testing.T) {
	tracker := NewPlanProgressTracker("/nonexistent")
	changes := tracker.scanActiveChanges()
	assert.Empty(t, changes)
}

// TestScanActiveChanges_SingleActive tests with exactly 1 active change.
func TestScanActiveChanges_SingleActive(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeDir(t, tmpDir, "my-change")

	tracker := NewPlanProgressTracker(tmpDir)
	changes := tracker.scanActiveChanges()
	assert.Equal(t, []string{"my-change"}, changes)
}

// TestScanActiveChanges_ExcludesArchive tests that archive/ is excluded.
func TestScanActiveChanges_ExcludesArchive(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeDir(t, tmpDir, "active-1")
	createChangeDir(t, tmpDir, "archived-1")
	// Create archive directory that should be excluded
	os.MkdirAll(filepath.Join(tmpDir, "openspec", "changes", "archive"), 0755)

	tracker := NewPlanProgressTracker(tmpDir)
	changes := tracker.scanActiveChanges()
	assert.Len(t, changes, 2)
	assert.Contains(t, changes, "active-1")
	assert.Contains(t, changes, "archived-1")
}

// TestScanActiveChanges_MultipleActive tests with 2+ active changes.
func TestScanActiveChanges_MultipleActive(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeDir(t, tmpDir, "change-1")
	createChangeDir(t, tmpDir, "change-2")

	tracker := NewPlanProgressTracker(tmpDir)
	changes := tracker.scanActiveChanges()
	assert.Len(t, changes, 2)
}

// TestParseTasksMd_Normal tests parsing a well-formed tasks.md.
func TestParseTasksMd_Normal(t *testing.T) {
	tmpDir := t.TempDir()
	createTasksFile(t, tmpDir, "test-change", `## 1. Setup
- [x] 1.1 Read config
- [ ] 1.2 Init connection

## 2. Data
- [ ] 2.1 Call API
`)

	tracker := NewPlanProgressTracker(tmpDir)
	tasks, err := tracker.parseTasksMd("test-change")
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
	assert.Equal(t, "1.1", tasks[0].ID)
	assert.Equal(t, "Read config", tasks[0].Title)
	assert.True(t, tasks[0].Done)
	assert.Equal(t, "1.2", tasks[1].ID)
	assert.False(t, tasks[1].Done)
	assert.Equal(t, "2.1", tasks[2].ID)
	assert.False(t, tasks[2].Done)
}

// TestParseTasksMd_MissingFile tests when tasks.md doesn't exist.
func TestParseTasksMd_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeDir(t, tmpDir, "empty-change")

	tracker := NewPlanProgressTracker(tmpDir)
	tasks, err := tracker.parseTasksMd("empty-change")
	assert.Error(t, err)
	assert.Nil(t, tasks)
}

// TestParseTasksMd_EmptyFile tests an empty tasks.md.
func TestParseTasksMd_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	createTasksFile(t, tmpDir, "empty-tasks", "")

	tracker := NewPlanProgressTracker(tmpDir)
	tasks, err := tracker.parseTasksMd("empty-tasks")
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

// TestBuildProgressSummary tests the summary format.
func TestBuildProgressSummary(t *testing.T) {
	tracker := NewPlanProgressTracker(".")
	tasks := []TaskItem{
		{ID: "1.1", Title: "Read config", Done: true},
		{ID: "1.2", Title: "Init connection", Done: false},
		{ID: "2.1", Title: "Call API", Done: false},
	}

	summary := tracker.buildProgressSummary("my-change", tasks)
	assert.Contains(t, summary, "[active_plan] my-change (1/3 完成)")
	assert.Contains(t, summary, "✓ 1.1 Read config")
	assert.Contains(t, summary, "⏳ 1.2 Init connection")
	assert.Contains(t, summary, "⏳ 2.1 Call API")
}

// TestInjectProgress_SingleActive tests BeforeModel injection with 1 active change.
func TestInjectProgress_SingleActive(t *testing.T) {
	tmpDir := t.TempDir()
	createTasksFile(t, tmpDir, "active-plan", `## 1. Tasks
- [x] 1.1 Done task
- [ ] 1.2 Pending task
`)

	tracker := NewPlanProgressTracker(tmpDir)
	args := &model.BeforeModelArgs{
		Request: &model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "hello"},
			},
		},
	}
	tracker.InjectProgress(args)

	// Should have 2 messages now: original + plan summary
	assert.Len(t, args.Request.Messages, 2)
	lastMsg := args.Request.Messages[1]
	assert.Equal(t, model.RoleSystem, lastMsg.Role)
	assert.Contains(t, lastMsg.Content, "[active_plan] active-plan (1/2 完成)")
	assert.Contains(t, lastMsg.Content, "✓ 1.1 Done task")
	assert.Contains(t, lastMsg.Content, "⏳ 1.2 Pending task")
}

// TestInjectProgress_NoActiveChanges tests no injection when 0 active changes.
func TestInjectProgress_NoActiveChanges(t *testing.T) {
	tracker := NewPlanProgressTracker("/nonexistent")
	args := &model.BeforeModelArgs{
		Request: &model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "hello"},
			},
		},
	}
	tracker.InjectProgress(args)

	// Should still have 1 message (no injection)
	assert.Len(t, args.Request.Messages, 1)
}

// TestInjectProgress_MultipleActiveChanges tests no injection with 2+ active changes.
func TestInjectProgress_MultipleActiveChanges(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeDir(t, tmpDir, "change-1")
	createChangeDir(t, tmpDir, "change-2")

	tracker := NewPlanProgressTracker(tmpDir)
	args := &model.BeforeModelArgs{
		Request: &model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "hello"},
			},
		},
	}
	tracker.InjectProgress(args)

	// Should still have 1 message (ambiguous, no injection)
	assert.Len(t, args.Request.Messages, 1)
}

// TestParseTaskID tests ID extraction from task text.
func TestParseTaskID(t *testing.T) {
	tests := []struct {
		input   string
		wantID  string
		wantTxt string
	}{
		{"1.1 Read config", "1.1", "Read config"},
		{"2.3.4 Deep nested", "2.3.4", "Deep nested"},
		{"10 Some task", "10", "Some task"},
		{"No ID here", "", "No ID here"},
		{"  leading spaces", "", "  leading spaces"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			id, title := parseTaskID(tt.input)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantTxt, title)
		})
	}
}

// === Helpers ===

func createChangeDir(t *testing.T, base, name string) {
	t.Helper()
	dir := filepath.Join(base, "openspec", "changes", name)
	require.NoError(t, os.MkdirAll(dir, 0755))
}

func createTasksFile(t *testing.T, base, changeName, content string) {
	t.Helper()
	dir := filepath.Join(base, "openspec", "changes", changeName)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(content), 0644))
}
