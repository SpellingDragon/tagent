package plugin

import "context"

// ==================== 归因章 ctx 载体（TC0 · 热配置/自进化地基）====================
//
// Attribution 是回合级归因章，写入 FullEvent.Metadata，使任意产出事件可回溯到产生
// 它时生效的版本上下文（bundle_id / rollout_id / agent）。这修复了「FullEvent.Metadata
// 在生产代码中从未被填充」的事实缺口（报告 §4.3 F1）——归因盖章是 T-EVO 自我改进
// （可归因/可回滚）与 T-B 可观测（事件维度切分）的共同地基。
//
// 载体模式仿 ProjectionSink（projection_sink.go）：RunFlow 每回合绑定，MemoryPlugin
// 在存储同步点读取写入 Metadata。两条持久化路径（插件管线 onEvent + persistBusEvent）
// 均须盖章，避免归因盲区（报告 R5）。
//
// 未注入归因时（AttributionFrom 返回 false），Metadata 仅含 MemoryPlugin 盖的基线
// provenance（agent_name），行为向后兼容。

// Attribution 是归因章键值对（写入 FullEvent.Metadata）。
type Attribution map[string]string

// attributionKey 是 ctx 载体键（每回合绑定，主循环与子 agent 天然隔离）。
type attributionKey struct{}

// WithAttribution 返回携带归因章的 ctx（RunFlow 每回合绑定）。空归因不注入（省分配）。
func WithAttribution(ctx context.Context, a Attribution) context.Context {
	if len(a) == 0 {
		return ctx
	}
	return context.WithValue(ctx, attributionKey{}, a)
}

// AttributionFrom 从 ctx 提取归因章（无则返回 nil,false）。
func AttributionFrom(ctx context.Context) (Attribution, bool) {
	a, ok := ctx.Value(attributionKey{}).(Attribution)
	return a, ok && len(a) > 0
}
