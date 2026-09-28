// 本文件负责 trace 锚点的诚实性：noop 与无效 SpanContext 只能降级为空值，link 为空不建，
// 事件来源去重，结算事件携带锚点。
// 契约: docs/wiki/agent/event-flow.md#trace-anchor
package agent

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"go.opentelemetry.io/otel/trace"
)

// TestStartTurnSpan_NoopSafe 钉住 未配置 OTLP（全局 noop provider）时 startTurnSpan 零 panic、返回可用 ctx 与 noop span——现状语义不变（T-B 不变量：noop 零行为变化）。
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
	if tid, sid := spanTraceIDs(spanCtx); tid != "" || sid != "" {
		t.Fatalf("noop 下 trace id 应为空, got trace=%q span=%q", tid, sid)
	}
	endTurnSpan(span, true)
}

// TestSpanTraceIDs_RealSpanContext 钉住 从携带真实 SpanContext 的 ctx 提取 trace_id/span_id （T-B「一套数据模式」的关联锚：此 id 将注入事件 Metadata 与 trajectory）。
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
		{Source: "user"},
		nil,
		{Source: ""},
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

// TestStartTurnSpan_WithLinkNoopSafe 钉住 带异步回流锚点创建回合 span 时不 panic 且返回有效 span：非法十六进制锚点被守卫跳过（既不降级也不崩），noop provider 下 link 不可观测，故本测试只验证健壮性。
func TestStartTurnSpan_WithLinkNoopSafe(t *testing.T) {
	ctx, span := startTurnSpan(context.Background(), turnSpanAttrs{
		AgentName:   "tagent",
		LinkTraceID: "0123456789abcdef0123456789abcdef",
		LinkSpanID:  "0123456789abcdef",
	})
	if span == nil {
		t.Fatal("带合法 link 应返回 span")
	}
	endTurnSpan(span, false)
	_ = ctx

	_, span2 := startTurnSpan(context.Background(), turnSpanAttrs{
		LinkTraceID: "not-hex", LinkSpanID: "xyz",
	})
	if span2 == nil {
		t.Fatal("非法 hex link 应被跳过，仍返回 span")
	}
	endTurnSpan(span2, false)

	_, span3 := startTurnSpan(context.Background(), turnSpanAttrs{AgentName: "a"})
	if span3 == nil {
		t.Fatal("空 link 应正常返回 span")
	}
	endTurnSpan(span3, false)
}

// TestNewTaskSettledEvent_CarriesTraceAnchor 钉住 管道末端要接上：任务来源袋含跟踪标识与跨度标识，结算时原样写进结算事件的元数据。
// - 回合入口从跨度捕获这两个标识并盖章到派生来源袋；
// - 于是异步结算能关联回触发它的回合跟踪，而 task 包无需理解跟踪语义。
func TestNewTaskSettledEvent_CarriesTraceAnchor(t *testing.T) {
	tk := &task.Task{ID: "t1", Spec: task.TaskSpec{Desc: "x", Origin: map[string]string{
		tagentevent.MetaKeyTraceID: "trace-abc",
		tagentevent.MetaKeySpanID:  "span-def",
		"chat_id":                  "u1",
	}}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "done"}, 0, "")

	if evt.Metadata[tagentevent.MetaKeyTraceID] != "trace-abc" {
		t.Fatalf("task_settled 应携带原 turn trace_id, got %v", evt.Metadata)
	}
	if evt.Metadata[tagentevent.MetaKeySpanID] != "span-def" {
		t.Fatalf("task_settled 应携带原 turn span_id, got %v", evt.Metadata)
	}
	if evt.Metadata["chat_id"] != "u1" {
		t.Fatalf("trace 锚点应与 origin baggage 共存, got %v", evt.Metadata)
	}
}

// TestNewTaskSettledEvent_NoTraceAnchorSafe 钉住 无 trace 锚点（未启用 OTLP/noop span）时 task_settled 事件正常构建（trace_id 缺省不报错，向后兼容）。
func TestNewTaskSettledEvent_NoTraceAnchorSafe(t *testing.T) {
	tk := &task.Task{ID: "t2", Spec: task.TaskSpec{Desc: "x", Origin: map[string]string{"chat_id": "u1"}}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "done"}, 0, "")
	if _, has := evt.Metadata[tagentevent.MetaKeyTraceID]; has {
		t.Fatal("无 trace 锚点时不应凭空出现 trace_id")
	}
	if evt.Metadata["chat_id"] != "u1" {
		t.Fatal("origin baggage 应正常携带")
	}
}
