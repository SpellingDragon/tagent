package plugin

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/plugin"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// SummaryPlugin 只做事件类型与元数据标注：它附加的 event_summary 是原文视图
// （多数类型为原文，action_command 为一行工具调用行），不承担内容摘要——内容级摘要
// 发生在压缩与策展阶段。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#summary-plugin
type SummaryPlugin struct{}

// NewSummaryPlugin 创建一个无状态的 SummaryPlugin。
func NewSummaryPlugin() *SummaryPlugin {
	return &SummaryPlugin{}
}

// Name 返回插件名 summary。
func (p *SummaryPlugin) Name() string {
	return "summary"
}

// Register 把本插件挂到框架的 OnEvent 钩子。
func (p *SummaryPlugin) Register(r *plugin.Registry) {
	r.OnEvent(p.onEvent)
}

// onEvent 为事件追加「类型:摘要视图」标注；无 choices 的事件原样返回。
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

	eventType := tagentevent.ExtractEventType(msg)

	opts := tagentevent.DefaultOptionsForLLMContext()
	summary := tagentevent.GenerateEventSummary(msg, eventType, opts)

	tag := eventType
	if summary != "" {
		tag = eventType + ":" + summary
	}

	if evt.Tag != "" {
		evt.Tag += ";" + tag
	} else {
		evt.Tag = tag
	}

	log.Debugf("[Summary] enriched type=%s summary_len=%d", eventType, len(summary))

	return evt, nil
}
