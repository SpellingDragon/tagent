package agent

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// TestStartTurnSpan_NoopSafe 验证未配置 OTLP（全局 noop provider）时 startTurnSpan
// 零 panic、返回可用 ctx 与 noop span——现状语义不变（T-B 不变量：noop 零行为变化）。
func TestStartTurnSpan_NoopSafe(t *testing.T) {
	ctx := context.Background()
	spanCtx, span := startTurnSpan(ctx, turnSpanAttrs{
		AgentName:     "tagent",
		TriggerSource: "user",
		ChatID:        "c1",
		BatchSize:     2,
		EventSources:  []string{"user"},
	})
	if span == nil {
		t.Fatal("span 不应为 nil（noop span 亦是有效对象）")
	}
	if spanCtx == nil {
		t.Fatal("spanCtx 不应为 nil")
	}
	// noop provider → span context 无效 → trace id 空。
	if tid, sid := spanTraceIDs(spanCtx); tid != "" || sid != "" {
		t.Fatalf("noop 下 trace id 应为空, got trace=%q span=%q", tid, sid)
	}
	// endTurnSpan 不 panic（含退化标记路径）。
	endTurnSpan(span, true)
}

// TestSpanTraceIDs_RealSpanContext 验证从携带真实 SpanContext 的 ctx 提取 trace_id/span_id
// （T-B「一套数据模式」的关联锚：此 id 将注入事件 Metadata 与 trajectory）。
func TestSpanTraceIDs_RealSpanContext(t *testing.T) {
	tid, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	sid, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	gotTrace, gotSpan := spanTraceIDs(ctx)
	if gotTrace != tid.String() {
		t.Errorf("trace_id=%q 期望 %q", gotTrace, tid.String())
	}
	if gotSpan != sid.String() {
		t.Errorf("span_id=%q 期望 %q", gotSpan, sid.String())
	}
}

// TestSpanTraceIDs_InvalidContext 验证无 span 的 ctx 返回空（调用方据此省略字段）。
func TestSpanTraceIDs_InvalidContext(t *testing.T) {
	if tid, sid := spanTraceIDs(context.Background()); tid != "" || sid != "" {
		t.Fatalf("无 span 应返回空, got %q/%q", tid, sid)
	}
}

// TestEventSources_Dedup 验证事件来源去重保序。
func TestEventSources_Dedup(t *testing.T) {
	events := []*AgentEvent{
		{Source: "user"},
		{Source: "task"},
		{Source: "user"}, // 重复
		nil,              // nil 防御
		{Source: ""},     // 空跳过
	}
	got := eventSources(events)
	want := []string{"user", "task"}
	if len(got) != len(want) {
		t.Fatalf("eventSources=%v 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("eventSources[%d]=%q 期望 %q", i, got[i], want[i])
		}
	}
}

// TestEndTurnSpan_NilSafe 验证 nil span 不 panic（防御）。
func TestEndTurnSpan_NilSafe(t *testing.T) {
	endTurnSpan(nil, false)
	endTurnSpan(nil, true)
}
