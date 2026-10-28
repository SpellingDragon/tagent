package telemetryexample

import (
	"context"
	"fmt"
	"strings"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/telemetry"
)

// lineSink is the canonical framework logger of this sample: it keeps every
// line it took beside the level the line was written at, so the sample can read
// back what the observer rendered.
type lineSink struct {
	lines []string
}

// take keeps one line at the named level.
func (s *lineSink) take(level, text string) { s.lines = append(s.lines, level+": "+text) }

// Debug keeps one debug line.
func (s *lineSink) Debug(args ...any) { s.take("debug", fmt.Sprint(args...)) }

// Debugf keeps one formatted debug line.
func (s *lineSink) Debugf(format string, args ...any) { s.take("debug", fmt.Sprintf(format, args...)) }

// Info keeps one info line.
func (s *lineSink) Info(args ...any) { s.take("info", fmt.Sprint(args...)) }

// Infof keeps one formatted info line.
func (s *lineSink) Infof(format string, args ...any) { s.take("info", fmt.Sprintf(format, args...)) }

// Warn keeps one warning line.
func (s *lineSink) Warn(args ...any) { s.take("warn", fmt.Sprint(args...)) }

// Warnf keeps one formatted warning line.
func (s *lineSink) Warnf(format string, args ...any) { s.take("warn", fmt.Sprintf(format, args...)) }

// Error keeps one error line.
func (s *lineSink) Error(args ...any) { s.take("error", fmt.Sprint(args...)) }

// Errorf keeps one formatted error line.
func (s *lineSink) Errorf(format string, args ...any) { s.take("error", fmt.Sprintf(format, args...)) }

// Fatal keeps one fatal line.
func (s *lineSink) Fatal(args ...any) { s.take("fatal", fmt.Sprint(args...)) }

// Fatalf keeps one formatted fatal line.
func (s *lineSink) Fatalf(format string, args ...any) { s.take("fatal", fmt.Sprintf(format, args...)) }

// at reports every line of the named level.
func (s *lineSink) at(level string) []string {
	var out []string
	for _, line := range s.lines {
		if strings.HasPrefix(line, level+": ") {
			out = append(out, line)
		}
	}
	return out
}

// ExampleLogObserver renders one turn's facts through the framework logger: the
// turn boundary is an INFO line, the tool call of that turn is a DEBUG line
// carrying the tool and the task, and neither line renders the payload content.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExampleLogObserver() {
	sink := &lineSink{}
	observer := telemetry.NewLogObserver(sink)
	ctx := context.Background()
	turn := event.Event{
		Key:          "fact-turn",
		Type:         event.TypeExternalInput,
		Owner:        event.Address{AgentID: "agent-a", SessionID: "session-a"},
		InvocationID: "inv-1",
		Message:      trpcmodel.Message{Role: trpcmodel.RoleUser, Content: "hello"},
	}
	call := event.Event{
		Key:          "fact-tool",
		Type:         event.TypeActionCommand,
		Owner:        turn.Owner,
		InvocationID: turn.InvocationID,
		TaskID:       "task-1",
		CallID:       "call-1",
		Message:      trpcmodel.Message{Role: trpcmodel.RoleTool, ToolName: "echo", Content: "out-a"},
	}
	for _, evt := range []event.Event{turn, call} {
		if err := observer.Observe(ctx, evt); err != nil {
			return
		}
	}
	if len(sink.at("info")) != 1 || !strings.Contains(sink.at("info")[0], "turn start fact=fact-turn") {
		return
	}
	if len(sink.at("debug")) != 1 || !strings.Contains(sink.at("debug")[0], "tool=echo") {
		return
	}
	if strings.Contains(strings.Join(sink.lines, "\n"), "out-a") {
		return
	}
}
