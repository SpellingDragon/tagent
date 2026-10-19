// Package model folds one tool-call response stream into one terminal per model request; this file
// declares the terminal reduction contract that every stream consumer is held to.
//
// - Terminal reduction has exactly one entry point per stream shape: ReduceChannel for a response channel and ReduceSeq for a response sequence, both folding one model request into one Outcome, so channel and iterator consumers share exactly one terminal judgement.
// - A consumer — the agent execution loop — consumes the Outcome and MUST NOT write a second terminal judgement beside the reduction: no re-classifying responses into the six Signal states, no parallel request fold, and no terminal derived from stream mechanics (channel close, sequence exhaustion, cancellation).
// - The reduction is request-scoped: one call covers exactly one model request, and usage, body, trace, late responses and the terminal belong to that request alone and never accumulate across requests.
// - Every end path leaves its own evidence in the Outcome and never stands a second terminal: a nil stream handle fails the reduction as KindFailed with AlertNilStream; a context cancellation closes it as KindCancelled with a SignalCancelled trace item while the producer is not required to have stopped; a producer end records a SignalProducerDone trace item and resolves the standing terminal, or alerts AlertUnknownTerminal when no terminal evidence stands.
// - The three paths are told apart by these marks, not by guesswork on the stream.
package model

import (
	"context"
	"errors"
)

// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
//
// Kind identifies the unique terminal outcome that the single terminal reduction
// entry point derives for a model request.
//
// The terminal is contract-frozen: one request MUST reduce to exactly one Kind,
// the five values are mutually exclusive, and a consumer reads the Kind of the
// returned Outcome as the terminal fact instead of re-deriving it.
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
//
// The signals form the six-state evidence table of the reduction: every observed
// input reduces to exactly one Signal (the six are mutually exclusive and
// exhaustive per observation), and a Signal is evidence of an observation,
// never a terminal of its own — only a Kind is a terminal.
type Signal int

const (
	// SignalValidTerminal is the evidence of a complete, valid terminal
	// response: the only signal that stands KindCompleted.
	SignalValidTerminal Signal = iota
	// SignalUsageTail is the evidence of a usage-only tail packet: it deposits
	// usage and never replaces the body or overrides the standing terminal.
	SignalUsageTail
	// SignalPartialOnly is the evidence of a response carrying only partial
	// output: it stands a provisional KindPartialOnly that only the producer end
	// settles.
	SignalPartialOnly
	// SignalError is the evidence of an error response or a nil stream item: it
	// stands KindFailed and carries the error itself.
	SignalError
	// SignalCancelled is the evidence of a cancellation or timeout error, or of
	// a caller cancellation closing the reduction: it stands KindCancelled and
	// is distinct from both the error signal and the producer end.
	SignalCancelled
	// SignalProducerDone is the evidence of the producer end (a closed channel
	// or an exhausted sequence): it resolves the standing terminal and never
	// stands a second terminal.
	SignalProducerDone
)

// terminalKind classifies one observed native response into its terminal Kind;
// it is the per-response classifier the reduction drives, not the request-level
// entry point, so it stays internal to this package. The terminal judgement of a
// whole tool-call stream is derived only by ReduceChannel or ReduceSeq, which
// fold every response of one model request into one Outcome.
//
// A response bearing no terminal evidence of its own, such as a usage-only tail
// packet, classifies to KindUnknown with ErrNoTerminal; the surrounding
// reduction settles that unknown at the producer end or alerts it.
func terminalKind(resp *Response) (Kind, error) {
	switch signalOf(resp) {
	case SignalValidTerminal:
		return KindCompleted, nil
	case SignalPartialOnly:
		return KindPartialOnly, nil
	case SignalError:
		return KindFailed, responseErr(resp)
	case SignalCancelled:
		return KindCancelled, responseErr(resp)
	case SignalUsageTail:
		return KindUnknown, ErrNoTerminal
	}
	return KindUnknown, ErrNoTerminal
}

// ErrNilResponse reports a nil response item observed inside a model stream.
var ErrNilResponse = errors.New("model: nil response observed in stream")

// ErrNoTerminal reports an observation that carries no terminal evidence of its
// own.
var ErrNoTerminal = errors.New("model: no terminal evidence")

// AlertNilStream, AlertNilResponse, AlertLateResponse and AlertUnknownTerminal
// name the four reasons the terminal reduction alerts its caller.
const (
	// AlertNilStream is reported when the stream handle handed to a reduction
	// entry point is nil.
	AlertNilStream = "nil-stream"
	// AlertNilResponse is reported when a stream yields a nil response item.
	AlertNilResponse = "nil-response"
	// AlertLateResponse is reported when a response arrives after the terminal
	// already stands.
	AlertLateResponse = "late-response"
	// AlertUnknownTerminal is reported when the producer ends without terminal
	// evidence, so the terminal is unknown instead of completed.
	AlertUnknownTerminal = "unknown-terminal"
)

// Alert reports one observation that the terminal reduction surfaces to its
// caller instead of resolving silently.
type Alert struct {
	// Reason is one of the Alert constants declared beside this type.
	Reason string
	// Terminal is the terminal Kind standing when the alert fired.
	Terminal Kind
	// Detail carries the diagnostic text of the alerting observation.
	Detail string
}

// Item is one observed input of the terminal reduction, recording the signal the
// input carries and the terminal standing after it.
type Item struct {
	// Signal is the six-state category the observed input reduces to.
	Signal Signal
	// Kind is the terminal Kind standing after this item; a usage tail carries
	// the standing terminal forward because it bears no terminal evidence of its
	// own.
	Kind Kind
	// Late reports whether the item arrived after the terminal already stood.
	Late bool
	// Dispatchable reports whether tool dispatch is permitted for this item: a
	// valid tool terminal is dispatchable, and every late item is not.
	Dispatchable bool
	// Err is the error the observed input itself carries, nil when it carries
	// none.
	Err error
}

// Outcome is the single terminal fact of one model request: one Kind beside the
// accumulated usage, the retained body, the observation trace and the late
// responses. It is request-scoped — one Outcome covers exactly one model
// request, and nothing accumulates across requests — and a consumer (the agent
// execution loop) consumes it as the terminal fact: it reads Kind and the
// recorded evidence and MUST NOT re-classify the six states or re-derive the
// terminal.
type Outcome struct {
	// Kind is the unique terminal of the request.
	Kind Kind
	// Usage is the usage accumulated across every observed response.
	Usage Usage
	// Body is the last response carrying valid content, or nil when none did.
	Body *Response
	// Trace holds every recorded item in observation order.
	Trace []Item
	// Late holds the non-tail responses observed after the terminal stood.
	Late []*Response
}

// Reducer folds one model request into its single terminal outcome. The channel
// consumer and the iterator consumer drive the same reducer, so both stream
// shapes share exactly one terminal judgement. One request is folded by exactly
// one Reducer, driven through ReduceChannel or ReduceSeq; a consumer MUST NOT
// keep a parallel accumulator or derive a second terminal beside the returned
// Outcome.
type Reducer struct {
	fold    usageFold
	kind    Kind
	set     bool
	partial bool
	done    bool
	trace   []Item
	late    []*Response
	onAlert func(Alert)
}

// NewReducer returns a reducer that reports every alert to onAlert while it is
// non-nil.
func NewReducer(onAlert func(Alert)) *Reducer {
	return &Reducer{onAlert: onAlert}
}

// Observe folds one observed response into the reduction and returns the item it
// added to the trace. Every response deposits its usage, while only a response
// carrying valid content may take the body slot before the terminal stands.
func (r *Reducer) Observe(resp *Response) Item {
	sig := signalOf(resp)
	r.fold.merge(resp)
	if r.standing() {
		return r.observeLate(resp, sig)
	}
	kind, err := terminalKind(resp)
	item := Item{Signal: sig, Kind: kind, Err: err}
	switch sig {
	case SignalUsageTail:
		item.Kind, item.Err = r.kind, nil
	case SignalPartialOnly:
		r.fold.retain(resp)
		r.kind, r.set, r.partial = kind, true, true
	default:
		r.fold.retain(resp)
		r.kind, r.set, r.partial = kind, true, false
		item.Dispatchable = sig == SignalValidTerminal && resp.IsToolCallResponse()
	}
	if resp == nil {
		r.alert(AlertNilResponse, "nil response observed in stream")
	}
	r.trace = append(r.trace, item)
	return item
}

// observeLate folds one response observed after the terminal stood: the usage
// still deposits, while the terminal itself never moves again.
func (r *Reducer) observeLate(resp *Response, sig Signal) Item {
	item := Item{Signal: sig, Kind: r.kind, Late: true}
	if sig != SignalUsageTail {
		item.Err = responseErr(resp)
		if resp != nil {
			r.late = append(r.late, resp)
		}
		r.alert(AlertLateResponse, "response observed after the terminal stood")
	}
	r.trace = append(r.trace, item)
	return item
}

// ProducerDone records the end of the producer and resolves a standing
// provisional terminal. A repeated call records nothing and changes nothing.
func (r *Reducer) ProducerDone() Item {
	item := Item{Signal: SignalProducerDone, Kind: r.kind}
	if r.done {
		return item
	}
	r.resolve()
	item.Kind = r.kind
	r.trace = append(r.trace, item)
	return item
}

// Cancel closes the reduction as KindCancelled unless a terminal already stands.
// The producer is not required to stop for the cancellation to take effect. A
// cancellation after the terminal stands changes nothing.
func (r *Reducer) Cancel() Item {
	item := Item{Signal: SignalCancelled, Kind: r.kind, Late: r.standing()}
	if r.standing() {
		return item
	}
	r.kind, r.set, r.partial = KindCancelled, true, false
	r.resolve()
	item.Kind = r.kind
	r.trace = append(r.trace, item)
	return item
}

// Outcome resolves the reduction and returns its single terminal fact. A
// reduction that never observed the producer end is resolved as if it had.
func (r *Reducer) Outcome() Outcome {
	if !r.done {
		r.resolve()
	}
	return Outcome{
		Kind:  r.kind,
		Usage: r.fold.totals(),
		Body:  r.fold.retained(),
		Trace: r.trace,
		Late:  r.late,
	}
}

// resolve settles the terminal: a standing terminal keeps its value, a
// provisional partial-only observation resolves to KindPartialOnly, and a
// reduction without any terminal evidence resolves to KindUnknown with an alert.
func (r *Reducer) resolve() {
	if r.done {
		return
	}
	r.done = true
	switch {
	case r.standing():
	case r.set:
		r.kind, r.partial = KindPartialOnly, false
	default:
		r.kind, r.set = KindUnknown, true
		r.alert(AlertUnknownTerminal, "producer ended without terminal evidence")
	}
}

// standing reports whether a final terminal stands: a provisional partial-only
// observation is not final until the producer end resolves it.
func (r *Reducer) standing() bool {
	return r.set && !r.partial
}

// failNilStream closes the reduction as KindFailed for a nil stream handle and
// alerts its caller.
func (r *Reducer) failNilStream() {
	r.kind, r.set, r.done = KindFailed, true, true
	r.alert(AlertNilStream, "nil stream handed to the reduction entry")
}

// alert reports one observation to the alert callback while it is non-nil.
func (r *Reducer) alert(reason, detail string) {
	if r.onAlert == nil {
		return
	}
	r.onAlert(Alert{Reason: reason, Terminal: r.kind, Detail: detail})
}

// signalOf classifies one native response into the six-state signal table: a nil
// item or an error response is an error signal, a cancellation error is its own
// signal, a response without valid content is a usage tail, a response without
// complete content is partial-only, and a complete non-partial response is the
// valid terminal.
func signalOf(resp *Response) Signal {
	if resp == nil {
		return SignalError
	}
	if resp.Error != nil {
		if resp.Error.Type == ErrorTypeCancelled {
			return SignalCancelled
		}
		return SignalError
	}
	if !resp.IsValidContent() {
		return SignalUsageTail
	}
	if resp.IsPartial || !resp.Done {
		return SignalPartialOnly
	}
	return SignalValidTerminal
}

// responseErr returns the error an observed input carries: ErrNilResponse for a
// nil item and the response error for a failed one.
func responseErr(resp *Response) error {
	if resp == nil {
		return ErrNilResponse
	}
	if resp.Error != nil {
		return resp.Error
	}
	return nil
}

// ReduceChannel is one of the two terminal reduction entry points: it consumes
// a response channel of one tool-call stream into that request's single
// terminal outcome, and reports every alert to onAlert while it is non-nil. It
// is request-scoped: one call covers exactly one model request, and the same
// responses reduced through ReduceSeq yield the same terminal. End paths leave
// distinct evidence and never stand a second terminal: a nil channel fails the
// reduction as KindFailed with alert reason AlertNilStream; a cancelled context
// closes it as KindCancelled (a standing terminal is never moved) with a
// SignalCancelled trace item, without waiting for the channel to close and
// without requiring the producer to stop; a closed channel records a
// SignalProducerDone trace item that resolves the standing terminal — or alerts
// AlertUnknownTerminal when no terminal evidence stands. The consumer reads the
// returned Outcome as the terminal fact and MUST NOT write a second terminal
// judgement beside it.
func ReduceChannel(ctx context.Context, ch <-chan *Response, onAlert func(Alert)) Outcome {
	r := NewReducer(onAlert)
	if ch == nil {
		r.failNilStream()
		return r.Outcome()
	}
	for {
		select {
		case <-ctx.Done():
			r.Cancel()
			return r.Outcome()
		default:
		}
		select {
		case <-ctx.Done():
			r.Cancel()
			return r.Outcome()
		case resp, ok := <-ch:
			if !ok {
				r.ProducerDone()
				return r.Outcome()
			}
			r.Observe(resp)
		}
	}
}

// ReduceSeq is the other terminal reduction entry point: it consumes a response
// sequence of one tool-call stream into that request's single terminal outcome,
// and reports every alert to onAlert while it is non-nil. It is request-scoped:
// one call covers exactly one model request, and the same responses reduced
// through ReduceChannel yield the same terminal. End paths leave distinct
// evidence and never stand a second terminal: a nil sequence fails the
// reduction as KindFailed with alert reason AlertNilStream; a cancelled context
// stops the consumption as KindCancelled (a standing terminal is never moved)
// with a SignalCancelled trace item, without waiting for the sequence to end and
// without requiring the producer to stop; an exhausted sequence records a
// SignalProducerDone trace item that resolves the standing terminal — or alerts
// AlertUnknownTerminal when no terminal evidence stands. The consumer reads the
// returned Outcome as the terminal fact and MUST NOT write a second terminal
// judgement beside it.
func ReduceSeq(ctx context.Context, seq Seq[*Response], onAlert func(Alert)) Outcome {
	r := NewReducer(onAlert)
	if seq == nil {
		r.failNilStream()
		return r.Outcome()
	}
	seq(func(resp *Response) bool {
		if ctx.Err() != nil {
			return false
		}
		r.Observe(resp)
		return true
	})
	if ctx.Err() != nil {
		r.Cancel()
	} else {
		r.ProducerDone()
	}
	return r.Outcome()
}
