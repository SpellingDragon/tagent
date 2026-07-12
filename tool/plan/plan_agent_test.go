package plan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestExtractAction_JSON parses action from JSON message content.
func TestExtractAction_JSON(t *testing.T) {
	inv := &agent.Invocation{
		Message: model.Message{Content: `{"action":"progress","request":"test"}`},
	}
	assert.Equal(t, "progress", extractAction(inv))
}

func TestExtractAction_KeywordPrefix(t *testing.T) {
	inv := &agent.Invocation{
		Message: model.Message{Content: "progress"},
	}
	assert.Equal(t, "progress", extractAction(inv))
}

func TestExtractAction_Empty(t *testing.T) {
	inv := &agent.Invocation{
		Message: model.Message{Content: ""},
	}
	assert.Equal(t, "", extractAction(inv))
}

func TestExtractAction_Unknown(t *testing.T) {
	inv := &agent.Invocation{
		Message: model.Message{Content: "some random text"},
	}
	assert.Equal(t, "", extractAction(inv))
}

// TestBuildProgressSummary_NoActiveChanges tests with no openspec/changes/ directory.
func TestBuildProgressSummary_NoActiveChanges(t *testing.T) {
	pa := NewPlanAgent(nil, "/nonexistent")
	summary := pa.buildProgressSummary()
	assert.Contains(t, summary, "没有活跃")
}

func TestBuildProgressSummary_SingleActiveChange(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeWithTasks(t, tmpDir, "test-plan", `## 1. Tasks
- [x] 1.1 Done task
- [ ] 1.2 Pending task
`)

	pa := NewPlanAgent(nil, tmpDir)
	summary := pa.buildProgressSummary()
	assert.Contains(t, summary, "test-plan")
	assert.Contains(t, summary, "1/2 完成")
	assert.Contains(t, summary, "✓ 1.1 Done task")
	assert.Contains(t, summary, "⏳ 1.2 Pending task")
}

func TestBuildProgressSummary_MultipleActiveChanges(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeWithTasks(t, tmpDir, "change-1", "- [ ] task")
	createChangeWithTasks(t, tmpDir, "change-2", "- [ ] task")

	pa := NewPlanAgent(nil, tmpDir)
	summary := pa.buildProgressSummary()
	assert.Contains(t, summary, "多个活跃")
}

// TestRunProgressQuery verifies that progress query returns an event with
// the summary as content, without calling LLM.
func TestRunProgressQuery(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeWithTasks(t, tmpDir, "test-plan", "- [x] 1.1 Done\n- [ ] 1.2 Pending\n")

	pa := NewPlanAgent(nil, tmpDir)
	inv := &agent.Invocation{
		Message: model.Message{Content: `{"action":"progress"}`},
	}

	ch, err := pa.runProgressQuery(context.Background(), inv)
	require.NoError(t, err)

	var evt *event.Event
	for e := range ch {
		evt = e
	}
	require.NotNil(t, evt)
	assert.NotNil(t, evt.Response)
	assert.Len(t, evt.Response.Choices, 1)
	assert.Contains(t, evt.Response.Choices[0].Message.Content, "test-plan")
	assert.Contains(t, evt.Response.Choices[0].Message.Content, "1/2 完成")
}

// TestRun_ProgressAction tests that Run routes progress to runProgressQuery.
func TestRun_ProgressAction(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeWithTasks(t, tmpDir, "test", "- [x] task\n")

	pa := NewPlanAgent(nil, tmpDir) // TagentAgent is nil — progress path doesn't use it
	inv := &agent.Invocation{
		Message: model.Message{Content: `{"action":"progress"}`},
	}

	ch, err := pa.Run(context.Background(), inv)
	require.NoError(t, err)

	var evt *event.Event
	for e := range ch {
		evt = e
	}
	require.NotNil(t, evt)
	assert.Contains(t, evt.Response.Choices[0].Message.Content, "test")
}

// TestParseTasksMd tests parsing of tasks.md.
func TestParseTasksMd(t *testing.T) {
	tmpDir := t.TempDir()
	createChangeWithTasks(t, tmpDir, "test", `## 1. Setup
- [x] 1.1 Read config
- [ ] 1.2 Init connection

## 2. Data
- [ ] 2.1 Call API
`)

	pa := NewPlanAgent(nil, tmpDir)
	tasks, err := pa.parseTasksMd("test")
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
	assert.True(t, tasks[0].Done)
	assert.Equal(t, "1.1", tasks[0].ID)
	assert.Equal(t, "Read config", tasks[0].Title)
	assert.False(t, tasks[1].Done)
	assert.False(t, tasks[2].Done)
}

func TestParseTasksMd_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "openspec", "changes", "empty"), 0755)

	pa := NewPlanAgent(nil, tmpDir)
	tasks, err := pa.parseTasksMd("empty")
	assert.Error(t, err)
	assert.Nil(t, tasks)
}

// TestParseTaskID tests ID extraction.
func TestParseTaskID(t *testing.T) {
	tests := []struct {
		input  string
		wantID string
		wantT  string
	}{
		{"1.1 Read config", "1.1", "Read config"},
		{"2.3.4 Deep nested", "2.3.4", "Deep nested"},
		{"No ID here", "", "No ID here"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			id, title := parseTaskID(tt.input)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantT, title)
		})
	}
}

func TestBuildProgressEvent(t *testing.T) {
	evt := buildProgressEvent("test summary")
	assert.NotNil(t, evt.Response)
	assert.Len(t, evt.Response.Choices, 1)
	assert.Equal(t, model.RoleAssistant, evt.Response.Choices[0].Message.Role)
	assert.Equal(t, "test summary", evt.Response.Choices[0].Message.Content)
	assert.Len(t, evt.Response.Choices[0].Message.ToolCalls, 0) // No tool calls — final response
}

// === Helpers ===

func createChangeWithTasks(t *testing.T, base, name, content string) {
	t.Helper()
	dir := filepath.Join(base, "openspec", "changes", name)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(content), 0644))
}
