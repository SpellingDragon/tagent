// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// The delegated turn runs on the model its builder assembles, so the model calls
// of that turn happen below the assembly and never pass the run's execution
// loop. A builder carrying the run's trajectory capture hands every one of those
// calls to the same account the entry turns are captured on: the request view is
// fixed before the call starts, the response the call produced is admitted as one
// attempt beside it, and the delegated turn seals what it captured when it ends.
// A builder beside no capture hands the model on as it stands.
package subagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/rl"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// workerDecisionSeq mints the identity of one delegated model call: the identity
// is process-wide, so two workers of one run never share a decision.
var workerDecisionSeq atomic.Uint64

// errWorkerStreamMissing reports a worker model that answered a request with
// neither a stream nor an error: the call produced nothing, and its attempt is
// recorded as a named failure rather than as a completed turn.
var errWorkerStreamMissing = errors.New("subagent: the worker model answered with no stream")

// withCapture wraps one worker model with the trajectory capture the builder
// carries. A nil capture keeps the model as it stands, so the uncaptured
// delegated path adds no layer to the request.
func withCapture(inner trpcmodel.Model, worker string, capture rl.CaptureScope) trpcmodel.Model {
	if inner == nil || capture == nil {
		return inner
	}
	if worker == "" {
		worker = "worker"
	}
	return &capturedModel{inner: inner, worker: worker, capture: capture}
}

// capturedModel is one worker model observed by the run's trajectory capture: it
// serves exactly the stream the model it wraps served and keeps the account of
// every call it serves.
//
// The redline list (3 rules):
// MUST admit every attempt beside the response that produced it and never let a refused observation reach the worker's decision.
// MUST relay the served stream unchanged and never make the worker wait on a writer.
// MUST preserve the iterator capability of the wrapped model instead of bridging it behind a channel.
type capturedModel struct {
	// inner is the model the delegated turn actually runs on.
	inner trpcmodel.Model
	// worker names the assembly the captured calls belong to.
	worker string
	// capture is the run's trajectory capture every call is fixed and admitted through.
	capture rl.CaptureScope
}

var (
	_ trpcmodel.Model     = (*capturedModel)(nil)
	_ trpcmodel.IterModel = (*capturedModel)(nil)
)

// Info reports the descriptor of the wrapped model, so a model reference still
// resolves by the provider's model name.
func (m *capturedModel) Info() trpcmodel.Info {
	return m.inner.Info()
}

// GenerateContent serves one worker request with the stream the model served and
// keeps the account of that call: the request view is fixed before the inner
// model is called, and every response relayed to the worker is folded into the
// reduction the attempt records.
func (m *capturedModel) GenerateContent(ctx context.Context, request *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	observed := m.observe(ctx, request)
	stream, err := m.inner.GenerateContent(ctx, request)
	if err != nil || stream == nil {
		observed.fail(ctx, err)
		return stream, err
	}
	forwarded := make(chan *trpcmodel.Response)
	go func() {
		defer close(forwarded)
		observed.forward(ctx, stream, forwarded)
	}()
	return forwarded, nil
}

// GenerateContentIter serves one worker request in the caller goroutine: the
// sequence is lazy, an iterating model is delegated to rather than bridged, and
// every response yielded is folded into the account of the call.
func (m *capturedModel) GenerateContentIter(ctx context.Context, request *trpcmodel.Request) (trpcmodel.Seq[*trpcmodel.Response], error) {
	return func(yield func(*trpcmodel.Response) bool) {
		observed := m.observe(ctx, request)
		if iterator, ok := m.inner.(trpcmodel.IterModel); ok {
			seq, err := iterator.GenerateContentIter(ctx, request)
			if err != nil || seq == nil {
				observed.fail(ctx, err)
				return
			}
			seq(func(response *trpcmodel.Response) bool {
				observed.reducer.Observe(response)
				return yield(response)
			})
			observed.finish(ctx)
			return
		}
		stream, err := m.inner.GenerateContent(ctx, request)
		if err != nil || stream == nil {
			observed.fail(ctx, err)
			return
		}
		for {
			select {
			case <-ctx.Done():
				observed.reducer.Cancel()
				go drain(stream)
				observed.admit(ctx)
				return
			case response, open := <-stream:
				if !open {
					observed.finish(ctx)
					return
				}
				observed.reducer.Observe(response)
				if !yield(response) {
					go drain(stream)
					observed.finish(ctx)
					return
				}
			}
		}
	}, nil
}

// observation is one worker model call as the capture sees it: the capture
// scoped to the view of that call beside the decision and attempt identities it
// records under and the single reduction of the responses the call produced.
type observation struct {
	// capture is the capture scoped to the request view of this call.
	capture rl.Capture
	// decision is the identity the call is fixed and recorded under.
	decision event.DecisionID
	// attempt is the identity of the one observable attempt this call is.
	attempt event.AttemptID
	// reducer is the single reduction of the responses this call produced.
	reducer *model.Reducer
}

// observe fixes the request view of one worker model call before the request
// starts and returns the observation that records its response. A refused fix is
// a counted loss at the capture and never a failed call.
func (m *capturedModel) observe(ctx context.Context, request *trpcmodel.Request) *observation {
	decision := event.DecisionID(fmt.Sprintf("worker-%s-%d", m.worker, workerDecisionSeq.Add(1)))
	view := rl.ModelViewRef{Model: m.inner.Info().Name, VisibleTools: versioned(visibleTools(request))}
	scoped := m.capture.Scope(view, decision)
	var sent model.Request
	if request != nil {
		sent = *request
	}
	_, _ = scoped.Snapshot(ctx, sent)
	return &observation{
		capture:  scoped,
		decision: decision,
		attempt:  event.AttemptID("attempt-" + string(decision)),
		reducer:  model.NewReducer(nil),
	}
}

// admit records the attempt of this observation through the capture: a refused
// record is a counted loss at the capture's own ledger and never reaches the
// worker's decision.
func (o *observation) admit(ctx context.Context) {
	_, _ = o.capture.Record(ctx, rl.AttemptOf(o.attempt, o.decision, "", o.reducer.Outcome()))
}

// finish closes the reduction of one served call beside its attempt: a call the
// context cancelled closes as cancelled, and a served one closes on the terminal
// its responses produced.
func (o *observation) finish(ctx context.Context) {
	if ctx.Err() != nil {
		o.reducer.Cancel()
	} else {
		o.reducer.ProducerDone()
	}
	o.admit(ctx)
}

// fail records the attempt of a call that produced no stream: the synthesized
// failure response is reduced like any stream, so the terminal recorded stays the
// model reduction's own.
func (o *observation) fail(ctx context.Context, cause error) {
	if cause == nil {
		cause = errWorkerStreamMissing
	}
	o.reducer.Observe(&model.Response{
		Done:  true,
		Error: &model.ResponseError{Message: cause.Error(), Type: model.ErrorTypeStreamError},
	})
	o.reducer.ProducerDone()
	o.admit(ctx)
}

// forward relays one served stream to the worker: every response is folded into
// the observation before it is relayed, and a reader that stopped leaves the
// upstream drained, so the producer never writes into no reader.
func (o *observation) forward(ctx context.Context, stream <-chan *model.Response, out chan<- *model.Response) {
	for {
		select {
		case <-ctx.Done():
			o.reducer.Cancel()
			go drain(stream)
			o.admit(ctx)
			return
		case response, open := <-stream:
			if !open {
				o.finish(ctx)
				return
			}
			o.reducer.Observe(response)
			select {
			case out <- response:
			case <-ctx.Done():
				o.reducer.Cancel()
				go drain(stream)
				o.admit(ctx)
				return
			}
		}
	}
}

// drain consumes one abandoned stream to its end: the producer of a stream whose
// reader stopped still reaches the close it writes against.
func drain(stream <-chan *model.Response) {
	for range stream {
	}
}

// visibleTools reports the tools the request exposes in name order: the view a
// call is fixed against carries exactly the tool surface the model saw.
func visibleTools(request *trpcmodel.Request) []string {
	if request == nil || len(request.Tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(request.Tools))
	for name := range request.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// versioned records visible names as unversioned view members: the worker names
// each visible tool it runs with and invents no version beside a member that
// carries none.
func versioned(names []string) []rl.VersionedName {
	out := make([]rl.VersionedName, 0, len(names))
	for _, name := range names {
		out = append(out, rl.VersionedName{Name: name})
	}
	return out
}
