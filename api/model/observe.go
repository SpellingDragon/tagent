// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
//
// The model-response face of the observation bypass: one bounded line on the
// framework logger per model request, rendered from the Outcome the single
// reduction already derived. The generation span of that request belongs to
// whoever drives the model, not to this package: a span opened here would land
// a second, empty generation beside the framework's one, so this face renders
// the line only and leaves the span to its owner.
//
// The redline list (4 rules):
// MUST render after the terminal stands, from the reduced Outcome, and never re-fold the stream.
// MUST keep every rendering bounded: identities, counts and token totals, never prompt or completion content.
// MUST stay a bypass: a failing logger is a missing observation, never a failed request.
// MUST NOT open a span beside the framework generation span of the request it observes.
package model

import (
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
)

// callObservation is one model request under observation: the request start it
// measures latency from, the model name the stream named and the number of
// responses the stream produced.
type callObservation struct {
	started   time.Time
	model     string
	responses int
}

// beginCall opens the observation of one model request: the start its latency
// is measured from, and nothing else — the span of the request stays with the
// framework that drives the model.
func beginCall() *callObservation {
	return &callObservation{started: time.Now()}
}

// saw records one observed response: the request's model name once a response
// names one, and the number of responses the stream has produced so far.
func (c *callObservation) saw(resp *Response) {
	if c == nil {
		return
	}
	c.responses++
	if resp == nil || resp.Model == "" || resp.Model == c.model {
		return
	}
	c.model = resp.Model
}

// end renders the one line of one request's terminal. It runs once per
// request, after the terminal stands, and reads the Outcome as the reduction
// derived it: the Kind, the accumulated usage and the stream size.
func (c *callObservation) end(outcome Outcome) {
	if c == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			quietCallDebug("tagent: model observation recovered: %v", recovered)
		}
	}()
	elapsed := time.Since(c.started)
	quietCallDebug("tagent: model response model=%s terminal=%s prompt_tokens=%d completion_tokens=%d total_tokens=%d responses=%d latency_ms=%d",
		callModel(c.model), outcome.Kind, outcome.Usage.PromptTokens, outcome.Usage.CompletionTokens,
		outcome.Usage.TotalTokens, c.responses, elapsed.Milliseconds())
}

// callModel reports the model name one request was served by, or the named
// absence of a name when no response carried one.
func callModel(name string) string {
	if name == "" {
		return "unnamed"
	}
	return name
}

// quietCallDebug writes one observation line at INFO through the framework
// logger, and swallows a logger that fails there: a gap reported through a
// logger that cannot be written is not a reason to surface a failure to the
// request that already reached its terminal.
func quietCallDebug(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Infof(format, args...)
}
