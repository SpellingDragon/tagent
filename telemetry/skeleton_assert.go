package telemetry

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
//
// This file holds compile-time assertions only. The phase 2 implementation
// types that will satisfy these interfaces do not exist yet, so the assertions
// pin what the skeleton proves on its own: every declared method keeps its
// exact shape and every declared port stays an interface type. A renamed method
// or a changed parameter or result stops the method shape assertions from
// compiling.
var (
	_ func(Observer, context.Context, event.Event) error = Observer.Observe

	_ func(Recorder, context.Context, event.Event) error  = Recorder.Record
	_ func(Recorder, context.Context) (SealResult, error) = Recorder.FlushAndWait
	_ func(Recorder, context.Context) error               = Recorder.Close

	_ func(AuditSink, context.Context, AuditRecord) error = AuditSink.Audit

	_ func(*SDKObserver, context.Context, event.Event) error = (*SDKObserver).Observe
)

// Interface kinds, asserted so that a later edit cannot turn a declared port
// into a concrete type without changing this file.
var (
	_ Observer  = (Observer)(nil)
	_ Recorder  = (Recorder)(nil)
	_ AuditSink = (AuditSink)(nil)
)

// The post-commit observer keeps satisfying the observation port it serves,
// so a revision that drops it from the bypass stops this block from compiling.
var _ Observer = (*SDKObserver)(nil)

// The layered audit record keeps every documented field, and every layer name
// stays a constant of its declared type.
var _ = AuditRecord{
	Fact:     "",
	Attempts: []Attempt{{Layer: LayerTrace, Skipped: false}},
}

const (
	_ Layer = LayerTrace
	_ Layer = LayerMetric
)

// The declared seal statuses must stay compile-time constants: a variable or a
// computed value stops this block from compiling.
const (
	_ = SealStatusSealed
	_ = SealStatusPartial
	_ = SealStatusUnknown
)

// The declared result type keeps every documented field: a renamed field or a
// changed field type stops this literal from compiling.
var _ = SealResult{
	Status:  SealStatusUnknown,
	Reason:  "",
	Dropped: 0,
	Errors:  0,
}
