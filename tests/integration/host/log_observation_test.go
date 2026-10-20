// The log observation checks of the host: the log face of the observation
// bypass driven through the formal assembly entry, so the lines a turn writes
// are read from a real runtime instead of a hand-built fact.
package host

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/telemetry"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// observationLine is one line the observation logger took: the level it was
// written at and the rendered text.
type observationLine struct {
	level string
	text  string
}

// observationLogger is the framework logger these checks hand to the log
// observer: it records every line beside its level, and refuses the writes its
// counter still holds by panicking.
type observationLogger struct {
	mu         sync.Mutex
	lines      []observationLine
	panicsLeft int
}

// record takes one line at the named level, or refuses it by panicking.
func (l *observationLogger) record(level, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.panicsLeft > 0 {
		l.panicsLeft--
		panic("the logger refused the write")
	}
	l.lines = append(l.lines, observationLine{level: level, text: text})
}

// Debug records one debug line.
func (l *observationLogger) Debug(args ...any) { l.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (l *observationLogger) Debugf(format string, args ...any) {
	l.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (l *observationLogger) Info(args ...any) { l.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (l *observationLogger) Infof(format string, args ...any) {
	l.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (l *observationLogger) Warn(args ...any) { l.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (l *observationLogger) Warnf(format string, args ...any) {
	l.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (l *observationLogger) Error(args ...any) { l.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (l *observationLogger) Errorf(format string, args ...any) {
	l.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (l *observationLogger) Fatal(args ...any) { l.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (l *observationLogger) Fatalf(format string, args ...any) {
	l.record("fatal", fmt.Sprintf(format, args...))
}

// at reports the text of every line taken at the named level.
func (l *observationLogger) at(level string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if line.level == level {
			out = append(out, line.text)
		}
	}
	return out
}

// all reports every line taken, whatever the level.
func (l *observationLogger) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.lines))
	for _, line := range l.lines {
		out = append(out, line.level+": "+line.text)
	}
	return out
}

// mentions reports whether any line taken at the level carries every fragment.
func (l *observationLogger) mentions(level string, fragments ...string) bool {
	for _, text := range l.at(level) {
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(text, fragment) {
				holds = false
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// TestLogObservationThroughTheFormalEntry checks the log face of the observation bypass through the formal assembly entry.
// - the admitted tool call of the turn is one DEBUG line carrying tool, task and call
// - the turn boundaries are INFO lines: the entry input opens the turn and the assistant output closes it
// - no line renders the payload content the turn carried
// - a logger that fails does not block the turn and still leaves its facts presented
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestLogObservationThroughTheFormalEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("the admitted call writes its per-fact line", func(t *testing.T) {
		logger := &observationLogger{}
		run, tool := visibilityRun(t, ctx, tagent.WithTelemetry(telemetry.NewLogObserver(logger)))
		visibilityInvoke(t, ctx, run, "inv-log", "session-log")
		if tool.calls() != 1 {
			t.Fatalf("tool executions = %d, want the admitted call executed exactly once", tool.calls())
		}
		if !logger.mentions("debug", "tool call", "tool=echo", "task=", "call=call-1") {
			t.Fatalf("debug lines = %v, want the admitted tool call of echo", logger.at("debug"))
		}
		for _, line := range logger.all() {
			if strings.Contains(line, "out-a") {
				t.Fatalf("line = %q, want no payload content rendered", line)
			}
		}
	})

	t.Run("the turn boundaries are observed at INFO", func(t *testing.T) {
		logger := &observationLogger{}
		run, _ := visibilityRun(t, ctx, tagent.WithTelemetry(telemetry.NewLogObserver(logger)))
		visibilityInvoke(t, ctx, run, "inv-log-boundary", "session-log-boundary")
		if !logger.mentions("info", "turn start", "agent=agent-a", "invocation=inv-log-boundary") {
			t.Fatalf("info lines = %v, want the turn-start boundary of the invocation", logger.at("info"))
		}
		if !logger.mentions("info", "turn end", "agent=agent-a", "invocation=inv-log-boundary") {
			t.Fatalf("info lines = %v, want the turn-end boundary of the invocation", logger.at("info"))
		}
		if len(logger.at("info")) != 2 {
			t.Fatalf("info lines = %v, want the INFO stream bounded by the two turn boundaries", logger.at("info"))
		}
	})

	t.Run("a failing logger does not block the turn", func(t *testing.T) {
		logger := &observationLogger{panicsLeft: 1}
		run, _ := visibilityRun(t, ctx, tagent.WithTelemetry(telemetry.NewLogObserver(logger)))
		visibilityInvoke(t, ctx, run, "inv-log-fail", "session-log-fail")
		if !logger.mentions("debug", "recovered a failing logger") {
			t.Fatalf("debug lines = %v, want the recovered line in place of the refused one", logger.at("debug"))
		}
		if presentations := run.Presentations(); len(presentations) == 0 {
			t.Fatal("presentations = none, want the committed facts presented after the refused line")
		}
	})
}

// observedSpan records the name, the attributes and the end of one span the
// observation opened on the framework tracer.
type observedSpan struct {
	trace.Span
	name  string
	attrs []attribute.KeyValue
	ended bool
}

// SetName records the name the observation moved the span to.
func (s *observedSpan) SetName(name string) { s.name = name }

// SetAttributes records the attributes the observation rendered.
func (s *observedSpan) SetAttributes(kv ...attribute.KeyValue) { s.attrs = append(s.attrs, kv...) }

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

// observedTracer records every span the observation started through the
// framework tracer it stands in for.
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

// named reports the spans the observation started under the name.
func (t *observedTracer) named(name string) []*observedSpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []*observedSpan
	for _, span := range t.spans {
		if span.name == name {
			out = append(out, span)
		}
	}
	return out
}

// watchObservationFaces points the framework logger and the framework tracer at
// the given stand-ins for the duration of one check, so every face of the
// observation is read from one sink beside one tracer.
func watchObservationFaces(tb testing.TB, logger trpclog.Logger, tracer trace.Tracer) {
	tb.Helper()
	originalLogger, originalTracer := trpclog.Default, ftrace.Tracer
	trpclog.Default, ftrace.Tracer = logger, tracer
	tb.Cleanup(func() { trpclog.Default, ftrace.Tracer = originalLogger, originalTracer })
}

// TestFourFacesObservationThroughTheFormalEntry checks the four faces of the observation bypass through the formal assembly entry.
// - 事件流: the committed tool-call fact writes its DEBUG line after its commit
// - 插件相位: every executed face of the pipeline writes one DEBUG line carrying the phase, the plugin and its result
// - 事件消费: the drive writes its start, its decision and its end lines beside the invocation identity
// - 模型响应: each model request writes one INFO line and leaves the generation span to the framework that drives the model
// - the INFO stream stays bounded by the turn boundaries and the model responses, and no line renders payload content
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestFourFacesObservationThroughTheFormalEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger, tracer := &observationLogger{}, &observedTracer{}
	watchObservationFaces(t, logger, tracer)
	run, _ := visibilityRun(t, ctx, tagent.WithTelemetry(telemetry.NewLogObserver(logger)))
	visibilityInvoke(t, ctx, run, "inv-faces", "session-faces")

	if !logger.mentions("debug", "tool call", "tool=echo", "call=call-1") {
		t.Fatalf("debug lines = %v, want the committed tool-call fact of the turn", logger.at("debug"))
	}
	if !logger.mentions("debug", "plugin phase", "phase=post-commit-observe", "face=observe", "plugin=commit-observer", "result=ok") {
		t.Fatalf("debug lines = %v, want the dispatched post-commit face of the pipeline", logger.at("debug"))
	}
	if !logger.mentions("debug", "consumer drive start", "agent=agent-a", "session=session-faces", "invocation=inv-faces") {
		t.Fatalf("debug lines = %v, want the start line of the drive", logger.at("debug"))
	}
	if !logger.mentions("debug", "consumer drive end", "invocation=inv-faces", "decisions=2", "terminal=completed") {
		t.Fatalf("debug lines = %v, want the end line of the drive beside its two decisions", logger.at("debug"))
	}
	if !logger.mentions("info", "model response", "model=model-a", "terminal=completed", "responses=1") {
		t.Fatalf("info lines = %v, want the model-response line of a served request", logger.at("info"))
	}
	if lines := logger.at("info"); len(lines) != 4 {
		t.Fatalf("info lines = %v, want the two turn boundaries beside the two model responses", lines)
	}

	spans := tracer.named("chat model-a")
	if len(spans) != 0 {
		t.Fatalf("chat spans = %d, want the generation span left to the framework that drives the model", len(spans))
	}

	for _, line := range logger.all() {
		if strings.Contains(line, "out-a") || strings.Contains(line, "settled") {
			t.Fatalf("line = %q, want no payload or message content rendered", line)
		}
	}
}
