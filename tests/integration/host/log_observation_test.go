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

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/telemetry"
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
