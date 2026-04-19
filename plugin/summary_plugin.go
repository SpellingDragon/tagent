package plugin

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/plugin"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// SummaryPlugin enriches events with type and summary information.
// It implements plugin.Plugin and is registered on the Runner.
//
// This is the "调用后" (post-call) responsibility:
// - Attaches event type to the event Tag
// - Generates and attaches event summary
// - Enables downstream consumers to understand event semantics
type SummaryPlugin struct{}

// NewSummaryPlugin creates a new SummaryPlugin.
func NewSummaryPlugin() *SummaryPlugin {
	return &SummaryPlugin{}
}

// Name implements plugin.Plugin.
func (p *SummaryPlugin) Name() string {
	return "summary"
}

// Register implements plugin.Plugin.
func (p *SummaryPlugin) Register(r *plugin.Registry) {
	r.OnEvent(p.onEvent)
}

// onEvent is the EventHook that enriches events with summary information.
func (p *SummaryPlugin) onEvent(
	ctx context.Context,
	inv *agent.Invocation,
	evt *event.Event,
) (*event.Event, error) {
	if evt == nil {
		return nil, nil
	}

	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return evt, nil
	}

	msg := evt.Response.Choices[0].Message

	// Extract event type
	eventType := tagentevent.ExtractEventType(msg)

	// Generate summary
	opts := tagentevent.DefaultOptionsForLLMContext()
	summary := tagentevent.GenerateEventSummary(msg, eventType, opts)

	// Attach to event Tag (append if existing)
	tag := eventType
	if summary != "" {
		tag = eventType + ":" + summary
	}

	if evt.Tag != "" {
		evt.Tag += ";" + tag
	} else {
		evt.Tag = tag
	}

	log.Debugf("SummaryPlugin: enriched event (type=%s, summary_len=%d)", eventType, len(summary))

	return evt, nil
}
