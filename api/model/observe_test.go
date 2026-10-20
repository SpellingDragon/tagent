// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
//
// The model-response observation checks: the span the framework tracer records
// for one model request and the one bounded line the framework logger takes for
// its terminal, read from a real reduction instead of a hand-built outcome.
package model

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// observedSpan records the attributes, the name and the status of one span the
// observation opened, beside whether it was ended.
type observedSpan struct {
	trace.Span
	name   string
	attrs  []attribute.KeyValue
	status codes.Code
	ended  bool
}

// SetName records the name the observation moved the span to.
func (s *observedSpan) SetName(name string) { s.name = name }

// SetAttributes records the attributes the observation rendered.
func (s *observedSpan) SetAttributes(kv ...attribute.KeyValue) { s.attrs = append(s.attrs, kv...) }

// SetStatus records the status the observation set.
func (s *observedSpan) SetStatus(code codes.Code, _ string) { s.status = code }

// End records that the observation closed the span.
func (s *observedSpan) End(...trace.SpanEndOption) { s.ended = true }

// attribute reports the rendered value of one attribute key.
func (s *observedSpan) attribute(key string) (attribute.Value, bool) {
	for _, kv := range s.attrs {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// observedTracer records every span the observation started.
type observedTracer struct {
	trace.Tracer
	mu    sync.Mutex
	spans []*observedSpan
}

// Start records one span and answers it as the started span.
func (t *observedTracer) Start(ctx context.Context, name string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	t.mu.Lock()
	defer t.mu.Unlock()
	span := &observedSpan{name: name}
	t.spans = append(t.spans, span)
	return ctx, span
}

// only reports the single span the observation started.
func (t *observedTracer) only(tb testing.TB) *observedSpan {
	tb.Helper()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.spans) != 1 {
		tb.Fatalf("spans = %d, want exactly one span per model request", len(t.spans))
	}
	return t.spans[0]
}

// observedLogger records every line the observation took, beside the level it
// was taken at, and refuses the writes a configured panic counter still holds.
type observedLogger struct {
	mu         sync.Mutex
	lines      []string
	panicsLeft int
}

// take records one rendered line or refuses it by panicking.
func (l *observedLogger) take(level, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.panicsLeft > 0 {
		l.panicsLeft--
		panic("the logger refused the write")
	}
	l.lines = append(l.lines, level+": "+text)
}

// Debug records one debug line.
func (l *observedLogger) Debug(args ...any) { l.take("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (l *observedLogger) Debugf(format string, args ...any) {
	l.take("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (l *observedLogger) Info(args ...any) { l.take("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (l *observedLogger) Infof(format string, args ...any) {
	l.take("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (l *observedLogger) Warn(args ...any) { l.take("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (l *observedLogger) Warnf(format string, args ...any) {
	l.take("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (l *observedLogger) Error(args ...any) { l.take("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (l *observedLogger) Errorf(format string, args ...any) {
	l.take("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (l *observedLogger) Fatal(args ...any) { l.take("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (l *observedLogger) Fatalf(format string, args ...any) {
	l.take("fatal", fmt.Sprintf(format, args...))
}

// mentions reports whether one line at the level carries every fragment.
func (l *observedLogger) mentions(level string, fragments ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if !strings.HasPrefix(line, level+": ") {
			continue
		}
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// watchObservation points the model observation at the given tracer and logger
// for the duration of one check.
func watchObservation(tb testing.TB, tracer trace.Tracer, logger trpclog.Logger) {
	tb.Helper()
	originalTracer, originalLogger := ftrace.Tracer, trpclog.Default
	ftrace.Tracer, trpclog.Default = tracer, logger
	tb.Cleanup(func() { ftrace.Tracer, trpclog.Default = originalTracer, originalLogger })
}

// TestModelCallObservation checks the model-response face of the observation bypass.
// - one model request opens exactly one span, ends it and names it after the model the stream named
// - the span carries the chat operation, the model and the token totals of the reduced outcome
// - the terminal is rendered as one INFO line carrying the model, the terminal and the token totals
// - a failed request marks the span as an error
// - a logger that fails does not block the reduction and still leaves the terminal standing
func TestModelCallObservation(t *testing.T) {
	t.Run("one request is one span and one INFO line", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		answer := invariantAnswer("hello", 7, 3)
		answer.Model = "model-a"
		ch := make(chan *Response, 1)
		ch <- answer
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindCompleted {
			t.Fatalf("terminal = %s, want the completed terminal of the answer", outcome.Kind)
		}
		span := tracer.only(t)
		if span.name != "chat model-a" {
			t.Fatalf("span name = %q, want the chat span named after the model", span.name)
		}
		if !span.ended {
			t.Fatal("span ended = false, want the span closed once the terminal stood")
		}
		if got, ok := span.attribute(string(semconvtrace.KeyGenAIOperationName)); !ok || got.AsString() != "chat" {
			t.Fatalf("gen_ai.operation.name = %v (present %t), want chat", got.AsString(), ok)
		}
		if got, ok := span.attribute(string(semconvtrace.KeyGenAIRequestModel)); !ok || got.AsString() != "model-a" {
			t.Fatalf("gen_ai.request.model = %v (present %t), want model-a", got.AsString(), ok)
		}
		if got, ok := span.attribute(string(semconvtrace.KeyGenAIUsageInputTokens)); !ok || got.AsInt64() != 7 {
			t.Fatalf("gen_ai.usage.input_tokens = %v (present %t), want 7", got.AsInt64(), ok)
		}
		if got, ok := span.attribute(string(semconvtrace.KeyGenAIUsageOutputTokens)); !ok || got.AsInt64() != 3 {
			t.Fatalf("gen_ai.usage.output_tokens = %v (present %t), want 3", got.AsInt64(), ok)
		}
		if !logger.mentions("info", "model response", "model=model-a", "terminal=completed", "prompt_tokens=7", "completion_tokens=3", "responses=1") {
			t.Fatalf("info lines = %v, want the one bounded model-response line of the request", logger.lines)
		}
	})

	t.Run("the iterator shape is observed the same way", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		answer := invariantAnswer("hello", 5, 2)
		answer.Model = "model-b"

		outcome := ReduceSeq(context.Background(), func(yield func(*Response) bool) {
			yield(answer)
		}, nil)

		if outcome.Kind != KindCompleted {
			t.Fatalf("terminal = %s, want the completed terminal of the answer", outcome.Kind)
		}
		if span := tracer.only(t); span.name != "chat model-b" {
			t.Fatalf("span name = %q, want the chat span named after the model", span.name)
		}
		if !logger.mentions("info", "model response", "model=model-b", "prompt_tokens=5", "completion_tokens=2") {
			t.Fatalf("info lines = %v, want the one bounded model-response line of the request", logger.lines)
		}
	})

	t.Run("a streamed request counts every response it produced", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		first := invariantPartial("hel", 3, 1)
		first.Model = "model-c"
		second := invariantPartial("lo", 3, 2)
		second.Model = "model-c"
		last := invariantAnswer("hello", 3, 2)
		last.Model = "model-c"
		ch := make(chan *Response, 3)
		ch <- first
		ch <- second
		ch <- last
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindCompleted {
			t.Fatalf("terminal = %s, want the completed terminal of the stream", outcome.Kind)
		}
		if span := tracer.only(t); span.name != "chat model-c" {
			t.Fatalf("span name = %q, want the one chat span of the streamed request", span.name)
		}
		if !logger.mentions("info", "model response", "model=model-c", "terminal=completed", "responses=3") {
			t.Fatalf("info lines = %v, want the one line of the stream carrying its response count", logger.lines)
		}
	})

	t.Run("a failed request marks the span as an error", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		ch := make(chan *Response, 1)
		ch <- &Response{Error: &ResponseError{Type: ErrorTypeStreamError}}
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindFailed {
			t.Fatalf("terminal = %s, want the failed terminal of the error response", outcome.Kind)
		}
		if span := tracer.only(t); span.status != codes.Error {
			t.Fatalf("span status = %v, want the error status of a failed request", span.status)
		}
		if !logger.mentions("info", "model response", "terminal=failed", "model=unnamed") {
			t.Fatalf("info lines = %v, want the failed terminal named in place of an unnamed model", logger.lines)
		}
	})

	t.Run("a logger that fails does not block the reduction", func(t *testing.T) {
		watchObservation(t, &observedTracer{}, &observedLogger{panicsLeft: 1})
		ch := make(chan *Response, 1)
		ch <- invariantAnswer("hello", 1, 1)
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindCompleted || outcome.Body == nil {
			t.Fatalf("outcome = (%s, body present %t), want the committed terminal beside its body", outcome.Kind, outcome.Body != nil)
		}
	})
}
