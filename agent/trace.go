// 契约: docs/wiki/agent/event-flow.md#trace-anchor
package agent

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	// tagentTracerName 是 tagent 自有层 span 的 tracer instrumentation scope。
	tagentTracerName = "github.com/SpellingDragon/tagent"
	// TurnSpanName 是 turn root span 名。
	TurnSpanName = "tagent.turn"
)

// turnSpanAttrs 是开启 turn span 的输入维度（事件驱动独有维度进属性，可查询）。
type turnSpanAttrs struct {
	AgentName     string
	TriggerSource string
	ChatID        string
	UserID        string
	BatchSize     int
	EventSources  []string

	// LinkTraceID/LinkSpanID 是异步任务回流的因果链接（C9）：task_settled 事件携带其 spawn
	// turn 的 trace 锚点（经 Origin→Metadata 管道），新 turn span 据此建 OTel span link，使
	// trace 后端里 spawn/settle 两棵 tree 相连——闭合三投影的 OTel span 维度（此前 Metadata/
	// trajectory 两投影已互链，span 树断开）。空则不建 link（noop 安全，非异步回流 turn 无此字段）。
	LinkTraceID string
	LinkSpanID  string
}

// startTurnSpan 开启 turn root span，返回携带 span 的 ctx（供 RunFlow 传播，框架 span
// 自动挂为子树）与 span（turn 末 End）。ctx 的取消/超时语义不变（仅注入 SpanContext）。
func startTurnSpan(ctx context.Context, a turnSpanAttrs) (context.Context, trace.Span) {
	tr := otel.Tracer(tagentTracerName)
	attrs := make([]attribute.KeyValue, 0, 6)
	if a.AgentName != "" {
		attrs = append(attrs, attribute.String("tagent.agent.name", a.AgentName))
	}
	if a.TriggerSource != "" {
		attrs = append(attrs, attribute.String("tagent.turn.trigger_source", a.TriggerSource))
	}
	if a.ChatID != "" {
		attrs = append(attrs, attribute.String("tagent.turn.chat_id", a.ChatID))
	}
	if a.UserID != "" {
		attrs = append(attrs, attribute.String("tagent.turn.user_id", a.UserID))
	}
	if a.BatchSize > 0 {
		attrs = append(attrs, attribute.Int("tagent.turn.batch_size", a.BatchSize))
	}
	if len(a.EventSources) > 0 {
		attrs = append(attrs, attribute.StringSlice("tagent.turn.event_sources", a.EventSources))
	}
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	if a.LinkTraceID != "" && a.LinkSpanID != "" {
		if tid, err := trace.TraceIDFromHex(a.LinkTraceID); err == nil {
			if sid, err := trace.SpanIDFromHex(a.LinkSpanID); err == nil {
				sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, Remote: true})
				opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sc}))
			}
		}
	}
	return tr.Start(ctx, TurnSpanName, opts...)
}

// spanTraceIDs 从 ctx 提取当前 span 的 trace_id/span_id（hex），供 attribution 注入与
// trajectory 关联。noop span 返回零值（IsValid=false）→ 空字符串（调用方据此省略字段）。
func spanTraceIDs(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}

// endTurnSpan 关闭 turn span，可选标记退化重试（同一 turn 语义，不另开 root span）。
func endTurnSpan(span trace.Span, degenerateRetry bool) {
	if span == nil {
		return
	}
	if degenerateRetry {
		span.SetAttributes(attribute.Bool("tagent.turn.degenerate_retry", true))
	}
	span.End()
}

// eventSources 提取批内事件的 Source 值（去重，保序）——turn span 的事件来源维度。
func eventSources(events []*AgentEvent) []string {
	seen := make(map[string]bool, len(events))
	out := make([]string, 0, len(events))
	for _, evt := range events {
		if evt == nil || evt.Source == "" || seen[evt.Source] {
			continue
		}
		seen[evt.Source] = true
		out = append(out, evt.Source)
	}
	return out
}
