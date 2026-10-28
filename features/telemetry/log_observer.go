// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// The log face of the observation bypass: the observer that renders each
// committed fact into one framework log line after the fact is durable. The
// level follows the fact type — a turn boundary is INFO and every per-fact
// detail is DEBUG — so the INFO stream stays bounded by the number of turns
// while a tool call, a delivery and a payload shape stay one line away.
//
// A line carries the fact identity, the address it belongs to, the tool name and
// a bounded payload shape; content is never rendered. A logger that fails is a
// missing line, never a blocked fact: the failure is recovered, the gap is
// retried at DEBUG, and the committed fact stays readable exactly as before.
package telemetry

import (
	"context"
	"errors"
	"fmt"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
)

// LogObserver is the post-commit observer over the framework log.Logger: one
// line per committed fact at the level that fact belongs to, so a turn boundary
// is visible at INFO while every per-fact detail stays at DEBUG. It reuses the
// framework logger instead of declaring a second logging face beside it.
//
// The redline list (4 rules):
// MUST run after the fact is durable, and never before it.
// MUST NOT render fact content: identity, address, tool name and a bounded payload shape only.
// MUST NOT hold back or reorder a committed fact for a slow or failing logger.
// MUST NOT roll back the committed fact or re-run the observed work to satisfy the log.
type LogObserver struct {
	logger trpclog.Logger
}

// ErrLogObserverUnwired reports a post-commit observation attempt on a log
// observer that carries no logger: a gap by name, not a silent no-op that would
// read as a delivered line.
var ErrLogObserverUnwired = errors.New("telemetry: the log observer is not wired")

// NewLogObserver wires one framework logger behind the post-commit log face. A
// nil logger lands on the framework default, so an assembly that names no logger
// observes through the same zap sink the framework logs to.
func NewLogObserver(logger trpclog.Logger) *LogObserver {
	if logger == nil {
		logger = trpclog.Default
	}
	return &LogObserver{logger: logger}
}

// Observe renders one committed fact into its line. The level is chosen by the
// fact's frozen type: a turn boundary is INFO and every per-fact detail is
// DEBUG, so the INFO stream stays bounded by the number of turns. A fact that
// carries no identity is a WARNING and is still not a refusal: it is a gap in
// the observation record, and the fact behind it stays as committed.
//
// A logger that fails is a missing line: the failure is recovered, the gap is
// retried at DEBUG, and no error reaches the caller. The observation bypass has
// no second channel for a log that cannot be written, and a formatting failure
// is never a reason to touch the committed fact or re-run the observed work.
func (o *LogObserver) Observe(_ context.Context, evt event.Event) (err error) {
	if o == nil || o.logger == nil {
		return ErrLogObserverUnwired
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			o.quietDebug("tagent: log observer recovered a failing logger: %v", recovered)
			err = nil
		}
	}()
	o.render(evt)
	return nil
}

// render writes the one line of one committed fact.
func (o *LogObserver) render(evt event.Event) {
	if evt.Key == "" || evt.Type == "" {
		o.logger.Warnf("tagent: fact without an identity key=%q type=%q payload=%s", evt.Key, evt.Type, factShape(evt))
		return
	}
	switch evt.Type {
	case event.TypeExternalInput:
		o.logger.Infof("tagent: turn start fact=%s agent=%s session=%s invocation=%s payload=%s",
			evt.Key, evt.Owner.AgentID, evt.Owner.SessionID, evt.InvocationID, factShape(evt))
	case event.TypeAgentOutput:
		o.logger.Infof("tagent: turn end fact=%s agent=%s session=%s invocation=%s task=%s payload=%s",
			evt.Key, evt.Owner.AgentID, evt.Owner.SessionID, evt.InvocationID, evt.TaskID, factShape(evt))
	case event.TypeActionCommand:
		o.logger.Debugf("tagent: tool call fact=%s tool=%s task=%s call=%s payload=%s",
			evt.Key, evt.Message.ToolName, evt.TaskID, evt.CallID, factShape(evt))
	case event.TypeTaskSettled:
		o.logger.Debugf("tagent: delivery fact=%s task=%s target=%s/%s payload=%s",
			evt.Key, evt.TaskID, evt.Target.AgentID, evt.Target.SessionID, factShape(evt))
	default:
		o.logger.Debugf("tagent: fact fact=%s type=%s task=%s payload=%s",
			evt.Key, evt.Type, evt.TaskID, factShape(evt))
	}
}

// factShape reports the bounded form of one fact's payload: the message role,
// the size of its content beside the calls it pairs with. The content itself is
// never rendered, so one line stays bounded whatever the payload holds.
func factShape(evt event.Event) string {
	size := "empty"
	if evt.Message.Content != "" {
		size = fmt.Sprintf("%dB", len(evt.Message.Content))
	}
	return fmt.Sprintf("role=%s content=%s calls=%d", evt.Message.Role, size, len(evt.Message.ToolCalls))
}

// quietDebug writes one line at DEBUG and swallows a logger that fails there
// too: a gap reported through a log that cannot be written is not a reason to
// surface a failure to the caller of the committed fact.
func (o *LogObserver) quietDebug(format string, args ...any) {
	defer func() { _ = recover() }()
	if o.logger != nil {
		o.logger.Debugf(format, args...)
	}
}
