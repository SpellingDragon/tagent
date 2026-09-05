package rl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

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

// TestLLMCallRecord_TraceFieldsBackwardCompat 验证审查 T2 关注点：trace_id/span_id 为
// omitempty 增量字段——noop（未启用 OTLP）时 JSON 不含这些键，旧 RL 消费者（AReaL）
// 解析既有格式不受影响；启用时字段出现，实现 trajectory↔trace 互链。
func TestLLMCallRecord_TraceFieldsBackwardCompat(t *testing.T) {
	// 无 trace id → JSON 省略（向后兼容）。
	empty, err := json.Marshal(LLMCallRecord{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(empty), "trace_id") || strings.Contains(string(empty), "span_id") {
		t.Fatalf("空 trace 字段应 omitempty, got %s", empty)
	}
	// 有 trace id → JSON 含字段（互链锚点）。
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
