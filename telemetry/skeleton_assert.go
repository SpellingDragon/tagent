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
)

// Interface kinds, asserted so that a later edit cannot turn a declared port
// into a concrete type without changing this file.
var (
	_ Observer = (Observer)(nil)
	_ Recorder = (Recorder)(nil)
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
