package evolution

import "context"

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// Digest is the reflection loop's self-state summary: a deterministic, bounded
// rendering of the observed face since a watermark that a reflection turn
// reads without spending a model call.
//
// The rendering is bounded, so a face with a long backlog reports how many of
// its facts the bound left out instead of growing without limit, and the same
// face at the same watermark renders the same summary. A render that cannot
// reach the face reports that it is unavailable by name rather than presenting
// an empty summary as a quiet face.
type Digest interface {
	// Render returns the bounded self-state summary of the observed face since
	// the given watermark. When the face holds more than the bound renders, the
	// count left out is reported in the result instead of being dropped in
	// silence.
	Render(ctx context.Context, since Watermark) (DigestSummary, error)
}

// DigestSummary is the rendered self-state summary together with the count of
// facts the render bound left out.
type DigestSummary struct {
	// Body is the rendered summary of the observed face.
	Body string
	// Omitted is the number of facts the render bound left out of Body.
	Omitted int
}

// The declared method, interface kind and fields must keep exactly these shapes:
// a renamed field or a changed method signature stops this block from compiling.
var (
	_ func(Digest, context.Context, Watermark) (DigestSummary, error) = Digest.Render

	_ Digest = (Digest)(nil)

	_ = DigestSummary{
		Body:    "",
		Omitted: 0,
	}
)
