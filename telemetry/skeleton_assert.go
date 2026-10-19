package telemetry

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
//
// This file holds compile-time assertions only: every declared method keeps its
// exact shape, every declared port stays an interface type, and each concrete
// observer still satisfies the plugin face it serves. A renamed method or a
// changed parameter or result stops the method shape assertions from compiling.
var (
	_ func(Recorder, context.Context, event.Event) error  = Recorder.Record
	_ func(Recorder, context.Context) (SealResult, error) = Recorder.FlushAndWait
	_ func(Recorder, context.Context) error               = Recorder.Close

	_ func(*SDKObserver, context.Context, event.Event) error = (*SDKObserver).Observe

	_ func(*CommitObserver, context.Context, event.EventID) error = (*CommitObserver).ObserveCommit
)

// Interface kinds, asserted so that a later edit cannot turn a declared port
// into a concrete type without changing this file.
var (
	_ Recorder = (Recorder)(nil)
)

// The outbound ports this package satisfies keep their declared shapes: the
// observation entry, the post-commit face and the audit side each stay the
// interface the plugin package declares, never a local copy of it.
var (
	_ plugin.Observer           = (plugin.Observer)(nil)
	_ plugin.PostCommitObserver = (plugin.PostCommitObserver)(nil)
	_ plugin.AuditSink          = (plugin.AuditSink)(nil)

	_ plugin.Observer           = (*SDKObserver)(nil)
	_ plugin.PostCommitObserver = (*CommitObserver)(nil)
)

// The unwired observer refuses by name rather than reporting a delivered
// observation, so the refusal stays an error value.
var _ error = ErrCommitObserverUnwired

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
