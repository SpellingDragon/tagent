// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
//
// The model-response face of the observation bypass: one chat span on the
// framework tracer beside one bounded line on the framework logger per model
// request, both rendered from the Outcome the single reduction already derived.
//
// The redline list (4 rules):
// MUST render after the terminal stands, from the reduced Outcome, and never re-fold the stream.
// MUST keep every rendering bounded: identities, counts and token totals, never prompt or completion content.
// MUST stay a bypass: a failing tracer or logger is a missing observation, never a failed request.
// MUST NOT mint a second terminal beside the reduction it observes.
package model

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// callOperation is the chat operation name the framework semconv carries for one
// model request: the operation a Langfuse span processor reads as a generation.
const callOperation = "chat"

// callObservation is one model request under observation: the span the framework
// tracer opened for it, the request start it measures latency from, the model
// name the stream named and the number of responses the stream produced.
type callObservation struct {
	span      trace.Span
	started   time.Time
	model     string
	responses int
}

// beginCall opens the observation of one model request on the caller's context,
// so the span is a child of whatever request it belongs to.
func beginCall(ctx context.Context) *callObservation {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	return &callObservation{span: startCallSpan(ctx), started: started}
}

// startCallSpan opens the chat span on the framework tracer, and answers no span
// when no tracer can open one: the tracer is a bypass, so its absence or its
// failure is a missing span and never a failed request.
func startCallSpan(ctx context.Context) (span trace.Span) {
	defer func() {
		if recover() != nil {
			span = nil
		}
	}()
	if ftrace.Tracer == nil {
		return nil
	}
	_, span = ftrace.Tracer.Start(ctx, callOperation)
	return span
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
	if c.span != nil {
		c.span.SetName(callOperation + " " + resp.Model)
	}
}

// end renders the span and the one line of one request's terminal. It runs once
// per request, after the terminal stands, and reads the Outcome as the reduction
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
	if c.span != nil {
		defer c.span.End()
		c.span.SetAttributes(callAttributes(outcome, c.model)...)
		if outcome.Kind == KindFailed {
			c.span.SetStatus(codes.Error, "the model request failed")
		}
	}
	quietCallDebug("tagent: model response model=%s terminal=%s prompt_tokens=%d completion_tokens=%d total_tokens=%d responses=%d latency_ms=%d",
		callModel(c.model), outcome.Kind, outcome.Usage.PromptTokens, outcome.Usage.CompletionTokens,
		outcome.Usage.TotalTokens, c.responses, elapsed.Milliseconds())
}

// callAttributes renders the bounded span attributes of one request's terminal:
// the chat operation, the model once a response names one, the token totals and
// the cached prompt tokens when the provider reported any.
func callAttributes(outcome Outcome, modelName string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(string(semconvtrace.KeyGenAIOperationName), callOperation),
		attribute.Int64(string(semconvtrace.KeyGenAIUsageInputTokens), int64(outcome.Usage.PromptTokens)),
		attribute.Int64(string(semconvtrace.KeyGenAIUsageOutputTokens), int64(outcome.Usage.CompletionTokens)),
	}
	if modelName != "" {
		attrs = append(attrs, attribute.String(string(semconvtrace.KeyGenAIRequestModel), modelName))
	}
	if cached := outcome.Usage.PromptTokensDetails.CachedTokens; cached > 0 {
		attrs = append(attrs, attribute.Int64(string(semconvtrace.KeyGenAIUsageInputTokensCached), int64(cached)))
	}
	return attrs
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
