package rl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestTrajectoryRecorder_BasicRecording 钉住基本落盘字段：会话/用户/批次号/端点/请求模型名。
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
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

/*
本用例整体停用（复活或删除待定，见 doc-drift-ledger.md D-13）：它把 SwappableModel 与记录器
组合装配，而该组合的前置条件——两者同处 rl 包——现已满足。
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

// TestTrajectoryRecorder_IteratorLazyRecords 钉住迭代入口不隐藏能力且保持惰性，且恰好落一条记录。
func TestTrajectoryRecorder_IteratorLazyRecords(t *testing.T) {
	tmpDir := t.TempDir()
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("a"), mkIterResp("b")}}
	tr, err := NewTrajectoryRecorder(base, tmpDir, "https://x/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("u", "sess-iter")

	seq, err := tr.GenerateContentIter(context.Background(), &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "go"}},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "creating the iterator must not call the model")
	require.Equal(t, int32(0), atomic.LoadInt32(&base.iterEntry))

	var got []string
	seq(func(r *model.Response) bool {
		if r != nil && len(r.Choices) > 0 {
			got = append(got, r.Choices[0].Message.Content)
		}
		return true
	})
	require.Equal(t, []string{"a", "b"}, got, "iterator must yield every response")
	require.Equal(t, int32(1), atomic.LoadInt32(&base.chanEntry), "iteration fires the model once")

	require.NoError(t, tr.Close())
	data, err := os.ReadFile(filepath.Join(tmpDir, "sess-iter.jsonl"))
	require.NoError(t, err)
	require.Len(t, splitJSONL(data), 1, "iterator path writes exactly one trajectory record")
}

// TestTrajectoryRecorderModelWrapper_Iterator 钉住子 agent 包装器同样保留迭代入口与惰性。
func TestTrajectoryRecorderModelWrapper_Iterator(t *testing.T) {
	tmpDir := t.TempDir()
	base := &iterCapableBase{responses: []*model.Response{mkIterResp("z")}}
	tr, err := NewTrajectoryRecorder(&iterCapableBase{}, tmpDir, "https://x/v1")
	require.NoError(t, err)
	tr.SetSessionInfo("u", "sess-wrap")
	w := NewTrajectoryRecorderModelWrapper(base, tr)

	seq, err := w.GenerateContentIter(context.Background(), &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "go"}},
	})
	require.NoError(t, err)
	require.Equal(t, int32(0), atomic.LoadInt32(&base.chanEntry), "lazy: not fired at creation")

	n := 0
	seq(func(*model.Response) bool { n++; return true })
	require.Equal(t, 1, n)
	require.Equal(t, int32(1), atomic.LoadInt32(&base.chanEntry), "iteration fires the wrapped model")
	require.NoError(t, tr.Close())
}

func TestTraceIDsFromCtx_Noop(t *testing.T) {
	if tid, sid := traceIDsFromCtx(context.Background()); tid != "" || sid != "" {
		t.Fatalf("无 span 应返回空, got %q/%q", tid, sid)
	}
}

func TestTraceIDsFromCtx_RealSpanContext(t *testing.T) {
	tid, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	sid, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}))
	gotTrace, gotSpan := traceIDsFromCtx(ctx)
	if gotTrace != tid.String() || gotSpan != sid.String() {
		t.Fatalf("trace 关联错: got %q/%q want %q/%q", gotTrace, gotSpan, tid.String(), sid.String())
	}
}

// TestLLMCallRecord_TraceFieldsBackwardCompat 钉住 trace 键为 omitempty：未启用时省略、旧格式仍可解。
func TestLLMCallRecord_TraceFieldsBackwardCompat(t *testing.T) {
	empty, err := json.Marshal(LLMCallRecord{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(empty), "trace_id") || strings.Contains(string(empty), "span_id") {
		t.Fatalf("空 trace 字段应 omitempty, got %s", empty)
	}
	withTrace, err := json.Marshal(LLMCallRecord{TraceID: "abc123", SpanID: "def456"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(withTrace), `"trace_id":"abc123"`) || !strings.Contains(string(withTrace), `"span_id":"def456"`) {
		t.Fatalf("trace 字段应出现, got %s", withTrace)
	}
	// 旧格式（无 trace 字段）仍可反序列化（消费者向后兼容）。
	var decoded LLMCallRecord
	if err := json.Unmarshal([]byte(`{"request":{"model":"glm"},"response":{}}`), &decoded); err != nil {
		t.Fatalf("旧格式反序列化失败: %v", err)
	}
	if decoded.TraceID != "" || decoded.SpanID != "" {
		t.Fatal("旧格式应解析出空 trace 字段")
	}
}
