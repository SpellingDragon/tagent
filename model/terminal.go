package model

import "errors"

// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
//
// Kind identifies the unique terminal outcome that the single terminal reduction
// entry point derives for a model request.
//
// The terminal is contract-frozen: one request MUST reduce to exactly one Kind.
// KindUnknown is the zero value, so an unset or undeterminable terminal is never
// silently reported as completed.
type Kind int

const (
	// KindUnknown is the zero value; it means the terminal could not be
	// determined. Callers MUST treat it as an alert and MUST NOT downgrade it to
	// KindCompleted.
	KindUnknown Kind = iota
	// KindCompleted is the terminal of a request that produced a complete,
	// non-partial answer.
	KindCompleted
	// KindFailed is the terminal of a request whose stream carried an error, or
	// whose entry failed before a stream could be established.
	KindFailed
	// KindCancelled is the terminal of a request cancelled by its caller.
	KindCancelled
	// KindPartialOnly is the terminal of a stream that ended after producing only
	// partial output.
	KindPartialOnly
)

// Signal identifies one input category that the single terminal reduction path
// must observe and evidence separately while it derives the terminal Kind.
type Signal int

const (
	// SignalValidTerminal is a complete, valid terminal response.
	SignalValidTerminal Signal = iota
	// SignalUsageTail is a usage-only tail packet, which never overrides the
	// response body.
	SignalUsageTail
	// SignalPartialOnly is a response that carries only partial output.
	SignalPartialOnly
	// SignalError is a response carrying an API-level or stream-level error.
	SignalError
	// SignalCancelled is a response carrying a cancellation or timeout error.
	SignalCancelled
	// SignalProducerDone is the end-of-stream signal from the producer, which is
	// not by itself a second terminal.
	SignalProducerDone
)

// TerminalKind is the single terminal reduction entry point: it classifies one
// native model response into its terminal Kind, so channel and iterator consumers
// share exactly one terminal judgement.
//
// The reduction is not implemented in the phase 1 skeleton and reports
// KindUnknown until phase 2 supplies the implementation.
func TerminalKind(resp *Response) (Kind, error) {
	return KindUnknown, errors.New("phase 2: not implemented")
}
