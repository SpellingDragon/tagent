package evolution

import (
	"context"
	"time"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// Watermark is the execution watermark of the reflection loop: the moment the
// last reflection turn advanced it to, which heads the freshness window and
// bounds how soon the next reflection may run.
//
// Injection alone never moves the watermark; only a reflection turn that is
// actually consumed advances it, to the moment the turn was injected at. A
// reflection that yields leaves the watermark in place, so the facts that
// reflection covered stay no older than the watermark instead of being burned
// out of the window, and a start with no watermark passes once and then locks
// itself. The watermark is advanced by consumption only, and the freshness
// judgment excludes the agent's own managed lineage, so a notification the
// agent sends about its own work never makes the agent reflect on itself again.
type Watermark struct {
	// Moment is the moment the last consumed reflection turn advanced the
	// watermark to. Its zero value marks a start with no prior execution.
	Moment time.Time
}

// WatermarkGate is the reflection loop's freshness gate over the execution
// watermark.
//
// The gate reads the watermark and advances it after a reflection turn is
// consumed. A watermark that is missing, or that cannot be read or parsed,
// keeps the gate closed and is reported by name; the gate is never opened on a
// fresh default or on a fallback criterion when the watermark cannot be
// established.
type WatermarkGate interface {
	// Current returns the current execution watermark. A watermark that cannot
	// be established is reported as an error, and the gate stays closed.
	Current(ctx context.Context) (Watermark, error)

	// Advance moves the watermark to the moment a consumed reflection turn was
	// injected at, and leaves it unmoved when that turn was deferred.
	Advance(ctx context.Context, moment time.Time) error
}

// The declared field, methods and interface kind must keep exactly these
// shapes: a renamed field or a changed method signature stops this block from
// compiling.
var (
	_ = Watermark{Moment: time.Time{}}

	_ func(WatermarkGate, context.Context) (Watermark, error) = WatermarkGate.Current

	_ func(WatermarkGate, context.Context, time.Time) error = WatermarkGate.Advance

	_ WatermarkGate = (WatermarkGate)(nil)
)
