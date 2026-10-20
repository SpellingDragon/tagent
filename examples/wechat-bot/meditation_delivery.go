// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
//
// The delivery face of the scheduled reflection: the declaration resolves in
// the core (meditation.PlanOf), and what this host adds is its own loop session
// — the observing agent's reserved meditation session, which never occupies the
// business conversation — beside the one outbound path a fired reflection's
// result leaves through. A target no chat ever reached receives nothing and the
// delivery is named in the log instead of being retargeted to another session.
package main

import (
	"context"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// meditationLoop reports the loop session one plan's reflection is injected
// into: the observing agent's own identity on the session this host reserves
// for the scheduled reflection.
func meditationLoop(doc *hostDocument, plan *meditation.Plan) meditation.ReflectionLoop {
	return meditation.ReflectionLoop{
		Agent:   plan.Agent(),
		Session: sessionPrefix(doc) + "meditation",
	}
}

// meditationTargets maps the declared delivery names onto the identities the
// reflection driver delivers to.
func meditationTargets(names []string) []event.AgentID {
	ids := make([]event.AgentID, 0, len(names))
	for _, name := range names {
		ids = append(ids, event.AgentID(name))
	}
	return ids
}

// deliverReflection hands the assistant output the reflection turn produced to
// the declared targets: the fresh messages of the observing agent's own
// transcript, filtered to answers without tool calls, sent to every chat the
// target agent has bound. A target no chat ever reached receives nothing and the
// delivery is named in the log instead of being retargeted to another session.
func (h *hostRuntime) deliverReflection(ctx context.Context, source event.Address, targets []event.AgentID) {
	if h.sender == nil {
		log.Warnf("[meditation] %s fired beside no outbound path, the result is withheld", source.AgentID)
		return
	}
	history := h.run.SessionHistory(source)
	contents := assistantContents(h.router.TakeUndelivered(source, history))
	if len(contents) == 0 {
		log.Debugf("[meditation] %s produced no deliverable assistant output", source.AgentID)
		return
	}
	for _, target := range targets {
		addresses := h.router.BoundAddresses(target)
		if len(addresses) == 0 {
			log.Warnf("[meditation] target %q holds no bound chat, %d message(s) withheld", target, len(contents))
			continue
		}
		for _, addr := range addresses {
			for _, content := range contents {
				if _, err := h.sender.sendToUser(ctx, sendRequest{
					addr:          addr,
					triggerSource: meditation.LineageReflection,
					content:       content,
				}); err != nil {
					log.Warnf("[meditation] delivery to %s refused: %v", addr.SessionID, err)
				}
			}
		}
	}
}

// assistantContents filters one transcript slice down to the answers a delivery
// carries: assistant messages that call no tool and hold text.
func assistantContents(messages []tagentmodel.Message) []string {
	contents := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != tagentmodel.RoleAssistant || len(msg.ToolCalls) > 0 {
			continue
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			contents = append(contents, content)
		}
	}
	return contents
}
