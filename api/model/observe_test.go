// 契约: docs/wiki/agent/agent-architecture.md#framework-boundary
//
// The model-response observation checks: the one bounded line the framework
// logger takes for one model request's terminal, read from a real reduction
// instead of a hand-built outcome, beside the span the reduction never opens —
// the generation span of a request stays with the framework that drives it.
package model

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// observedTracer stands in for the framework tracer the observation reads: it
// records the name of every span started through it, so a check can hold the
// observation to opening none of its own.
type observedTracer struct {
	trace.Tracer
	mu    sync.Mutex
	spans []string
}

// Start records one started span's name and answers a span nothing reads.
func (t *observedTracer) Start(ctx context.Context, name string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.spans = append(t.spans, name)
	return ctx, noop.Span{}
}

// started reports the names of the spans started through the stand-in.
func (t *observedTracer) started() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.spans...)
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
	return len(l.linesMentioning(level, fragments...)) > 0
}

// linesMentioning returns every recorded line at the level carrying every
// fragment, in the order the observation took them.
func (l *observedLogger) linesMentioning(level string, fragments ...string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	matched := make([]string, 0, len(l.lines))
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
			matched = append(matched, line)
		}
	}
	return matched
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
// - one model request opens no span of its own: the generation span stays with the framework that drives the model
// - the terminal is rendered as one INFO line carrying the model, the terminal and the token totals
// - the line carries the latency of the request and the number of responses the stream produced
// - a failed request renders its failed terminal beside the named absence of a model
// - a failed request names its raw cause on the same single line, escaped onto one line
// - a completed request carries no cause field, and a failure without recorded text renders no empty field
// - a logger that fails does not block the reduction and still leaves the terminal standing
func TestModelCallObservation(t *testing.T) {
	t.Run("one request is one INFO line and no span of its own", func(t *testing.T) {
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
		if spans := tracer.started(); len(spans) != 0 {
			t.Fatalf("spans = %v, want no span beside the framework generation span of the request", spans)
		}
		if !logger.mentions("info", "model response", "model=model-a", "terminal=completed", "prompt_tokens=7", "completion_tokens=3", "responses=1", "latency_ms=") {
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
		if spans := tracer.started(); len(spans) != 0 {
			t.Fatalf("spans = %v, want no span beside the framework generation span of the request", spans)
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
		if spans := tracer.started(); len(spans) != 0 {
			t.Fatalf("spans = %v, want no span beside the framework generation span of the request", spans)
		}
		if !logger.mentions("info", "model response", "model=model-c", "terminal=completed", "responses=3") {
			t.Fatalf("info lines = %v, want the one line of the stream carrying its response count", logger.lines)
		}
	})

	t.Run("a failed request renders its failed terminal", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		ch := make(chan *Response, 1)
		ch <- &Response{Error: &ResponseError{Type: ErrorTypeStreamError}}
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindFailed {
			t.Fatalf("terminal = %s, want the failed terminal of the error response", outcome.Kind)
		}
		if spans := tracer.started(); len(spans) != 0 {
			t.Fatalf("spans = %v, want no span beside the framework generation span of the request", spans)
		}
		if !logger.mentions("info", "model response", "terminal=failed", "model=unnamed") {
			t.Fatalf("info lines = %v, want the failed terminal named in place of an unnamed model", logger.lines)
		}
	})

	t.Run("a failed request names its cause", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		ch := make(chan *Response, 1)
		ch <- &Response{Error: &ResponseError{Type: ErrorTypeAPIError, Message: "provider answered 500\nretry later"}}
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindFailed {
			t.Fatalf("terminal = %s, want the failed terminal of the error response", outcome.Kind)
		}
		lines := logger.linesMentioning("info", "model response")
		if len(lines) != 1 {
			t.Fatalf("info lines = %v, want exactly one model-response line for one request", logger.lines)
		}
		if !strings.Contains(lines[0], "terminal=failed") || !strings.Contains(lines[0], "error=") || !strings.Contains(lines[0], "500") {
			t.Fatalf("info line = %q, want the failed terminal beside the raw cause", lines[0])
		}
		if strings.Contains(lines[0], "\n") || !strings.Contains(lines[0], `\n`) {
			t.Fatalf("info line = %q, want the reason escaped onto the same single line", lines[0])
		}
	})

	t.Run("a completed request names no cause", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		ch := make(chan *Response, 1)
		ch <- invariantAnswer("done", 2, 1)
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindCompleted {
			t.Fatalf("terminal = %s, want the completed terminal of the answer", outcome.Kind)
		}
		if lines := logger.linesMentioning("info", "model response", "error="); len(lines) != 0 {
			t.Fatalf("info lines = %v, want no cause field beside a completed terminal", lines)
		}
	})

	t.Run("a failure without a message renders no empty cause", func(t *testing.T) {
		tracer, logger := &observedTracer{}, &observedLogger{}
		watchObservation(t, tracer, logger)
		ch := make(chan *Response, 1)
		ch <- &Response{Error: &ResponseError{Type: ErrorTypeStreamError}}
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindFailed {
			t.Fatalf("terminal = %s, want the failed terminal of the error response", outcome.Kind)
		}
		if !logger.mentions("info", "terminal=failed") {
			t.Fatalf("info lines = %v, want the failed terminal standing", logger.lines)
		}
		if lines := logger.linesMentioning("info", "model response", "error="); len(lines) != 0 {
			t.Fatalf("info lines = %v, want no empty cause field", lines)
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
