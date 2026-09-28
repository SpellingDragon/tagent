package plugin

import (
	"context"

	"github.com/SpellingDragon/tagent/memory"
)

// ProjectionSink 接收事件在持久化当刻产生的引用；agent 的 SessionProjection 实现它。
// 它是「写入即投影」的唯一同步点，使投影完成先于 BeforeModel 由构造保证。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#skip-set
type ProjectionSink interface {
	Append(ref memory.EventReference)
}

type projectionSinkKey struct{}

// WithProjectionSink 绑定本次调用的投影接收端；调用链 ctx 天然隔离主循环与子 agent。
func WithProjectionSink(ctx context.Context, sink ProjectionSink) context.Context {
	return context.WithValue(ctx, projectionSinkKey{}, sink)
}

// ProjectionSinkFrom 取回本调用的投影接收端；无则返回 (nil, false)。
func ProjectionSinkFrom(ctx context.Context) (ProjectionSink, bool) {
	sink, ok := ctx.Value(projectionSinkKey{}).(ProjectionSink)
	return sink, ok && sink != nil
}
