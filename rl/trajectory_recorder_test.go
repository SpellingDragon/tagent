package rl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestTrajectoryRecorder_BasicRecording(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockModel{info: model.Info{Name: "test-model"}}

	tr, err := NewTrajectoryRecorder(mock, tmpDir, "https://test.example.com/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("user-1", "session-1")

	ctx := context.Background()
	req := &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "hello"},
		},
	}
	ch, err := tr.GenerateContent(ctx, req)
	require.NoError(t, err)

	resp := <-ch
	assert.Equal(t, "test-model", resp.Model)

	require.NoError(t, tr.Close())

	jsonlPath := filepath.Join(tmpDir, "session-1.jsonl")
	data, err := os.ReadFile(jsonlPath)
	require.NoError(t, err)

	lines := splitJSONL(data)
	require.Len(t, lines, 1, "expected 1 JSONL line")

	var record TrajectoryRecord
	require.NoError(t, json.Unmarshal(lines[0], &record))
	assert.Equal(t, "session-1", record.SessionID)
	assert.Equal(t, "user-1", record.UserID)
	assert.Equal(t, 0, record.BatchIndex)
	assert.Equal(t, "https://test.example.com/v1", record.Metadata.ModelEndpoint)
	assert.Equal(t, "test-model", record.LLMCall.Request.Model)
}

func TestTrajectoryRecorder_ChannelFullDoesNotBlock(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockModel{info: model.Info{Name: "test-model"}}
	tr, err := NewTrajectoryRecorder(mock, tmpDir, "https://test.example.com/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("user-1", "session-flood")

	ctx := context.Background()
	req := &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "flood"},
		},
	}

	for i := 0; i < 300; i++ {
		ch, err := tr.GenerateContent(ctx, req)
		require.NoError(t, err)
		<-ch
	}

	require.NoError(t, tr.Close())
}

func TestTrajectoryRecorder_CloseFlush(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockModel{info: model.Info{Name: "flush-model"}}
	tr, err := NewTrajectoryRecorder(mock, tmpDir, "https://test.example.com/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("user-flush", "session-flush")

	ctx := context.Background()
	req := &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "flush-test"},
		},
	}

	for i := 0; i < 5; i++ {
		ch, err := tr.GenerateContent(ctx, req)
		require.NoError(t, err)
		<-ch
	}

	require.NoError(t, tr.Close())

	jsonlPath := filepath.Join(tmpDir, "session-flush.jsonl")
	data, err := os.ReadFile(jsonlPath)
	require.NoError(t, err)

	lines := splitJSONL(data)
	assert.Len(t, lines, 5, "expected 5 JSONL lines after Close flush")

	for i, line := range lines {
		var record TrajectoryRecord
		require.NoError(t, json.Unmarshal(line, &record), "line %d", i)
		assert.Equal(t, i, record.BatchIndex, "batch index %d", i)
	}
}

// TODO: Re-enable after moving SwappableModel to rl package
/*
func TestTrajectoryRecorder_WithSwappableModel(t *testing.T) {
	tmpDir := t.TempDir()
	original := &mockModel{info: model.Info{Name: "original-model"}}
	swapped := &mockModel{info: model.Info{Name: "swapped-model"}}

	sm := NewSwappableModel(original)
	tr, err := NewTrajectoryRecorder(sm, tmpDir, "https://original.example.com/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("user-swap", "session-swap")

	ctx := context.Background()
	req := &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "before-swap"},
		},
	}

	ch, err := tr.GenerateContent(ctx, req)
	require.NoError(t, err)
	<-ch

	sm.Swap(swapped)
	tr.SetModelEndpoint("https://swapped.example.com/v1")

	ch, err = tr.GenerateContent(ctx, req)
	require.NoError(t, err)
	<-ch

	require.NoError(t, tr.Close())

	jsonlPath := filepath.Join(tmpDir, "session-swap.jsonl")
	data, err := os.ReadFile(jsonlPath)
	require.NoError(t, err)

	lines := splitJSONL(data)
	require.Len(t, lines, 2)

	var rec0, rec1 TrajectoryRecord
	require.NoError(t, json.Unmarshal(lines[0], &rec0))
	require.NoError(t, json.Unmarshal(lines[1], &rec1))

	assert.Equal(t, "https://original.example.com/v1", rec0.Metadata.ModelEndpoint)
	assert.Equal(t, "https://swapped.example.com/v1", rec1.Metadata.ModelEndpoint)
}
*/

func TestTrajectoryRecorder_Info(t *testing.T) {
	tmpDir := t.TempDir()
	mock := &mockModel{info: model.Info{Name: "info-model"}}
	tr, err := NewTrajectoryRecorder(mock, tmpDir, "endpoint")
	require.NoError(t, err)
	defer tr.Close()

	info := tr.Info()
	assert.Equal(t, "info-model", info.Name)
}

func splitJSONL(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
