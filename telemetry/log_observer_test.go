package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// logLine is one line a recording logger took: the level it was written at and
// the rendered text.
type logLine struct {
	level string
	text  string
}

// errRecordingLogger is what the failing logger panics with.
var errRecordingLogger = errors.New("the logger refused the write")

// recordingLogger is the framework logger of these checks: it records every
// line beside the level it was written at, and refuses the writes its counter
// still holds by panicking.
type recordingLogger struct {
	mu         sync.Mutex
	lines      []logLine
	panicsLeft int
}

// record takes one line at the named level, or refuses it by panicking.
func (l *recordingLogger) record(level, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.panicsLeft > 0 {
		l.panicsLeft--
		panic(errRecordingLogger)
	}
	l.lines = append(l.lines, logLine{level: level, text: text})
}

// Debug records one debug line.
func (l *recordingLogger) Debug(args ...any) { l.record("debug", fmt.Sprint(args...)) }

// Debugf records one formatted debug line.
func (l *recordingLogger) Debugf(format string, args ...any) {
	l.record("debug", fmt.Sprintf(format, args...))
}

// Info records one info line.
func (l *recordingLogger) Info(args ...any) { l.record("info", fmt.Sprint(args...)) }

// Infof records one formatted info line.
func (l *recordingLogger) Infof(format string, args ...any) {
	l.record("info", fmt.Sprintf(format, args...))
}

// Warn records one warning line.
func (l *recordingLogger) Warn(args ...any) { l.record("warn", fmt.Sprint(args...)) }

// Warnf records one formatted warning line.
func (l *recordingLogger) Warnf(format string, args ...any) {
	l.record("warn", fmt.Sprintf(format, args...))
}

// Error records one error line.
func (l *recordingLogger) Error(args ...any) { l.record("error", fmt.Sprint(args...)) }

// Errorf records one formatted error line.
func (l *recordingLogger) Errorf(format string, args ...any) {
	l.record("error", fmt.Sprintf(format, args...))
}

// Fatal records one fatal line without ending the test binary.
func (l *recordingLogger) Fatal(args ...any) { l.record("fatal", fmt.Sprint(args...)) }

// Fatalf records one formatted fatal line without ending the test binary.
func (l *recordingLogger) Fatalf(format string, args ...any) {
	l.record("fatal", fmt.Sprintf(format, args...))
}

// at reports the text of every line the logger took at the named level.
func (l *recordingLogger) at(level string) []string {
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

// taken reports the text of every line the logger took, whatever the level.
func (l *recordingLogger) taken() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.lines))
	for _, line := range l.lines {
		out = append(out, line.level+": "+line.text)
	}
	return out
}

// logTurnFacts returns the committed facts of one turn: its start, the tool call
// the model made, the delivery of that call, and the output that ended the turn.
func logTurnFacts() []event.Event {
	addr := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	return []event.Event{
		{
			Key:          "fact-turn",
			Type:         event.TypeExternalInput,
			Owner:        addr,
			InvocationID: "inv-1",
			Message:      trpcmodel.Message{Role: trpcmodel.RoleUser, Content: "hello"},
		},
		{
			Key:          "fact-tool",
			Type:         event.TypeActionCommand,
			Owner:        addr,
			InvocationID: "inv-1",
			TaskID:       "task-1",
			CallID:       "call-1",
			Message:      trpcmodel.Message{Role: trpcmodel.RoleTool, ToolName: "echo", Content: "out-a"},
		},
		{
			Key:          "fact-settle",
			Type:         event.TypeTaskSettled,
			Owner:        addr,
			Target:       addr,
			InvocationID: "inv-1",
			TaskID:       "task-1",
			Message:      trpcmodel.Message{Role: trpcmodel.RoleTool, Content: "settled"},
		},
		{
			Key:          "fact-out",
			Type:         event.TypeAgentOutput,
			Owner:        addr,
			InvocationID: "inv-1",
			TaskID:       "task-1",
			Message:      trpcmodel.Message{Role: trpcmodel.RoleAssistant, Content: "done"},
		},
	}
}

// TestLogObservation pins the log face of the observation bypass.
// - turn boundaries are the only INFO lines, one per start and one per end
// - a tool call and a delivery are DEBUG lines carrying tool, task, call and target
// - a line renders the payload shape and never the content
// - a fact without an identity is a WARNING that still reports no failure
// - a logger that fails is recovered and the gap is retried at DEBUG, never returned
// - an observer without a logger refuses by name instead of reporting a silent line
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestLogObservation(t *testing.T) {
	ctx := context.Background()

	t.Run("turn boundaries are INFO and every per-fact detail is DEBUG", func(t *testing.T) {
		logger := &recordingLogger{}
		observer := NewLogObserver(logger)
		for _, evt := range logTurnFacts() {
			if err := observer.Observe(ctx, evt); err != nil {
				t.Fatalf("observe %q: %v", evt.Key, err)
			}
		}
		infos := logger.at("info")
		if len(infos) != 2 {
			t.Fatalf("info lines = %v, want one turn start and one turn end", infos)
		}
		if !strings.Contains(infos[0], "turn start fact=fact-turn") || !strings.Contains(infos[0], "agent=agent-a") {
			t.Fatalf("first info line = %q, want the turn start of agent-a", infos[0])
		}
		if !strings.Contains(infos[1], "turn end fact=fact-out") || !strings.Contains(infos[1], "invocation=inv-1") {
			t.Fatalf("second info line = %q, want the turn end of inv-1", infos[1])
		}
		debugs := logger.at("debug")
		if len(debugs) != 2 {
			t.Fatalf("debug lines = %v, want the tool call and the delivery", debugs)
		}
		if !strings.Contains(debugs[0], "tool call fact=fact-tool") ||
			!strings.Contains(debugs[0], "tool=echo") ||
			!strings.Contains(debugs[0], "task=task-1") ||
			!strings.Contains(debugs[0], "call=call-1") {
			t.Fatalf("first debug line = %q, want the tool, the task and the call", debugs[0])
		}
		if !strings.Contains(debugs[1], "delivery fact=fact-settle") ||
			!strings.Contains(debugs[1], "target=agent-a/session-a") {
			t.Fatalf("second debug line = %q, want the delivery target", debugs[1])
		}
		if !strings.Contains(debugs[0], "content=5B") {
			t.Fatalf("tool call line = %q, want the bounded payload shape", debugs[0])
		}
		for _, line := range logger.taken() {
			if strings.Contains(line, "out-a") || strings.Contains(line, "hello") {
				t.Fatalf("line = %q, want no payload content rendered", line)
			}
		}
	})

	t.Run("a fact without an identity is a warning that reports no failure", func(t *testing.T) {
		logger := &recordingLogger{}
		observer := NewLogObserver(logger)
		if err := observer.Observe(ctx, event.Event{Type: event.TypeAgentOutput}); err != nil {
			t.Fatalf("observe a fact without an identity = %v, want the gap recorded and not returned", err)
		}
		warns := logger.at("warn")
		if len(warns) != 1 || !strings.Contains(warns[0], "fact without an identity") {
			t.Fatalf("warning lines = %v, want the missing identity named", warns)
		}
		if len(logger.at("info")) != 0 {
			t.Fatalf("info lines = %v, want no turn line for a fact without an identity", logger.at("info"))
		}
	})

	t.Run("a logger that fails is recovered and the gap retried at DEBUG", func(t *testing.T) {
		logger := &recordingLogger{panicsLeft: 1}
		observer := NewLogObserver(logger)
		if err := observer.Observe(ctx, logTurnFacts()[0]); err != nil {
			t.Fatalf("observe through a failing logger = %v, want no failure returned to the caller", err)
		}
		debugs := logger.at("debug")
		if len(debugs) != 1 || !strings.Contains(debugs[0], "recovered a failing logger") {
			t.Fatalf("debug lines = %v, want the recovered gap recorded at debug", debugs)
		}
		if len(logger.at("info")) != 0 {
			t.Fatalf("info lines = %v, want the refused line not reported as delivered", logger.at("info"))
		}
	})

	t.Run("an observer without a logger refuses by name", func(t *testing.T) {
		var nilObserver *LogObserver
		if err := nilObserver.Observe(ctx, logTurnFacts()[0]); !errors.Is(err, ErrLogObserverUnwired) {
			t.Fatalf("observe on a nil observer = %v, want the unwired refusal", err)
		}
		if err := (&LogObserver{}).Observe(ctx, logTurnFacts()[0]); !errors.Is(err, ErrLogObserverUnwired) {
			t.Fatalf("observe on an unwired observer = %v, want the unwired refusal", err)
		}
	})
}
