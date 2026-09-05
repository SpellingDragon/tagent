package agent

import (
	"context"
	"testing"
)

// TestStartTurnSpan_WithLinkNoopSafe 验证 C9：turn span 带异步回流的 link 锚点时不 panic、
// 返回有效 span；非法 hex 锚点被守卫跳过（不退化、不崩）。noop provider 下 link 不可观测，
// 此处验证健壮性（hex 解析守卫 + span 构造 + 三投影 OTel 维度闭合的入口）。
func TestStartTurnSpan_WithLinkNoopSafe(t *testing.T) {
	// 合法 hex（TraceID 32 chars / SpanID 16 chars）→ 建 link，不 panic。
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

	// 非法 hex → 守卫跳过 link，仍返回 span（不退化）。
	_, span2 := startTurnSpan(context.Background(), turnSpanAttrs{
		LinkTraceID: "not-hex", LinkSpanID: "xyz",
	})
	if span2 == nil {
		t.Fatal("非法 hex link 应被跳过，仍返回 span")
	}
	endTurnSpan(span2, false)

	// 空 link（非异步回流 turn）→ 不建 link，正常 span（noop 安全）。
	_, span3 := startTurnSpan(context.Background(), turnSpanAttrs{AgentName: "a"})
	if span3 == nil {
		t.Fatal("空 link 应正常返回 span")
	}
	endTurnSpan(span3, false)
}
