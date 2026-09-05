package agent

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ==================== turn-as-trace（T-B · 统一可观测数据模型）====================
//
// 一个 turn = 一棵 trace：runEventLoop 每轮开 root span（tagent.turn），RunFlow 及其下
// 框架自动 span（trpc-agent-go llmflow/functioncall）成为子树。tagent 自有层此前完全
// 缺席 OTel（loopCtx=background），本文件补上 turn 骨架。
//
// 「一套数据模式、多场景投影」（指令2）的落点：turn span 的 trace_id/span_id 是唯一
// 关联锚——① 经 TC0 的 attribution 载体注入 → 落 FullEvent.Metadata（事件溯源投影）；
// ② 落 rl.TrajectoryRecorder 的 LLMCallRecord（RL 训练投影）；③ span 树本身（运维投影）。
// 三个世界由同一 trace_id/span_id 双向互链，一致性由单一锚点保证。
//
// noop 安全：未设 OTEL_EXPORTER_OTLP_ENDPOINT 时，otel 全局 TracerProvider 为 noop，
// tr.Start 返回 noop span（零分配、零导出、零行为变化）——现状语义逐字节保持。

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
	EventSources  []string // 批内事件的 Source 值（user/tmux/task/meditation/...）
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
	return tr.Start(ctx, TurnSpanName, trace.WithAttributes(attrs...))
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
