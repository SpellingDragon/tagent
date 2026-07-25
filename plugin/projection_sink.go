package plugin

import (
	"context"

	"github.com/SpellingDragon/tagent/memory"
)

// ProjectionSink receives event references as they are persisted by the
// event-plugin pipeline. The agent's SessionProjection implements it.
//
// Write unification (unified-event-projection D1): the pipeline is the SINGLE
// synchronous point where a stored event is also projected. Because the
// framework flow waits for pipeline completion on tool-result events
// (RequiresCompletion + completion notice), "projection complete at
// BeforeModel" becomes guaranteed by construction rather than by timing.
type ProjectionSink interface {
	Append(ref memory.EventReference)
}

// projectionSinkKey is the context key carrying the current invocation's
// projection sink. Each RunFlow binds its own projection (persistent loop and
// sub-agent invocations are isolated by their call-chain contexts).
type projectionSinkKey struct{}

// WithProjectionSink returns a context carrying the projection sink for the
// current invocation.
func WithProjectionSink(ctx context.Context, sink ProjectionSink) context.Context {
	return context.WithValue(ctx, projectionSinkKey{}, sink)
}

// ProjectionSinkFrom extracts the projection sink from ctx, if any.
func ProjectionSinkFrom(ctx context.Context) (ProjectionSink, bool) {
	sink, ok := ctx.Value(projectionSinkKey{}).(ProjectionSink)
	return sink, ok && sink != nil
}
