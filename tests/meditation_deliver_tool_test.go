// meditation_deliver_tool_test 钉住 冥想侧经 deliver 工具自主回流的端到端闭环：脚本化模型在反思回合发起投递，
// 卡片经投递缝以真实形态进入目标的下一回合；白名单外的目标被结果文本弹回、回合继续且不发生跨 agent 写入。
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
package tagent_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	deliverTargetLabel = "E2E-DELIVER-TARGET-PROMPT"
	deliverMedLabel    = "E2E-DELIVER-MEDITATOR-PROMPT"
)

// deliverScriptModel 给冥想者侧的回合编排一次 deliver 工具调用，其余调用者与后续回合计文本。
type deliverScriptModel struct {
	mu       sync.Mutex
	calls    []medCall
	args     string
	medTurns int
}

func (m *deliverScriptModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	var b strings.Builder
	for _, msg := range req.Messages {
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	label := medLabel(medSystemOf(req))
	m.mu.Lock()
	m.calls = append(m.calls, medCall{label: label, text: b.String()})
	var resp *model.Response
	if label == deliverMedLabel {
		m.medTurns++
		if m.medTurns == 1 {
			resp = &model.Response{ID: "deliver-call-1", Done: true, Choices: []model.Choice{{
				Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
					Type: "function", ID: "deliver-call-1",
					Function: model.FunctionDefinitionParam{Name: "deliver", Arguments: []byte(m.args)},
				}}},
			}}}
		} else {
			resp = &model.Response{Done: true, Choices: []model.Choice{{
				Message: model.Message{Role: model.RoleAssistant, Content: "meditation-turn-continued"},
			}}}
		}
	} else {
		resp = &model.Response{Done: true, Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: "deliver-e2e-served"},
		}}}
	}
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *deliverScriptModel) Info() model.Info { return model.Info{Name: "deliver-e2e-model"} }

// script 设定冥想者首个回合要发起的 deliver 调用参数。
func (m *deliverScriptModel) script(args string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.args = args
}

// count 报告某个调用者被服务的次数。
func (m *deliverScriptModel) count(label string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if c.label == label {
			n++
		}
	}
	return n
}

// contains 报告某个调用者的任何一次请求文本里是否出现过 sub。
func (m *deliverScriptModel) contains(label, sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.label == label && strings.Contains(c.text, sub) {
			return true
		}
	}
	return false
}

// deliverE2ERig 是投递方常驻且自持反思循环的双 agent 进程：入口 target 循环运行，meditator 挂白名单。
type deliverE2ERig struct {
	model     *deliverScriptModel
	target    *agent.TagentAgent
	meditator *agent.TagentAgent
}

// newDeliverE2ERig 装配 target 与 meditator 并起两条常驻循环：反思线使用 meditation 保留名。
func newDeliverE2ERig(t *testing.T, deliverTo []string) *deliverE2ERig {
	t.Helper()
	dir := t.TempDir()
	cfg := tagent.Config{
		Entry: "target",
		Agents: map[string]tagent.AgentConfig{
			"target": {
				SystemPrompt: tagent.PromptConfig{Inline: deliverTargetLabel},
				Memory:       tagent.MemoryConfig{Type: "memory", Path: dir},
				Tools:        []tagent.ToolRef{{Kind: "agent", AgentID: "meditator", Description: "反思冥想者"}},
			},
			"meditator": {
				SystemPrompt: tagent.PromptConfig{Inline: deliverMedLabel},
				Memory:       tagent.MemoryConfig{Type: "memory", Path: dir, ReadNamespaces: []string{"target"}},
				Meditation: tagent.MeditationConfig{
					Enabled:            true,
					Interval:           "1h",
					MinGap:             "1h",
					ObservedNamespaces: []string{"target"},
					DeliverTo:          deliverTo,
				},
			},
		},
	}
	m := &deliverScriptModel{}
	entry, err := tagent.New(cfg, tagent.WithModel(m))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	meditator := entry.ResidentTable()["meditator"]
	require.NotNil(t, meditator, "冥想者必须在本进程常驻表里")

	targetOut, err := entry.StartLoop("u", "deliver-e2e-target-session")
	require.NoError(t, err)
	go func() {
		for range targetOut {
		}
	}()
	medOut, err := meditator.StartLoop("u", "meditation")
	require.NoError(t, err)
	go func() {
		for range medOut {
		}
	}()
	return &deliverE2ERig{model: m, target: entry, meditator: meditator}
}

// TestDeliverToolAutonomousDeliveryClosesLoop 钉住 反思回合自主发起的 deliver 投递闭环。
// - 目标在白名单且循环活跃：卡片进入目标 mailbox，目标下一回合可见正文与来源头。
// - 自述头带投递方身份与 meditation 保留会话名。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToolAutonomousDeliveryClosesLoop(t *testing.T) {
	rig := newDeliverE2ERig(t, []string{"target"})
	rig.model.script(`{"target":"target","content":"自主投递卡片正文：周五前收口"}`)

	_, err := rig.meditator.InjectMessageContext(context.Background(), "u",
		model.NewUserMessage("反思回合开始：把巩固结论投回去"))
	require.NoError(t, err)

	medEventually(t, "the deliver receipt reached the meditator model", func() bool {
		return rig.model.contains(deliverMedLabel, "[delivery_ok] 已投递 target=target")
	})
	medEventually(t, "the reflection turn continued after the tool result", func() bool {
		return rig.model.count(deliverMedLabel) >= 2
	})

	medEventually(t, "the target served a turn carrying the delivered card", func() bool {
		return rig.model.contains(deliverTargetLabel, "自主投递卡片正文：周五前收口")
	})
	require.True(t, rig.model.contains(deliverTargetLabel, "[delivery] 来源 agent：meditator"),
		"投递消息须自带来源 agent")
	require.True(t, rig.model.contains(deliverTargetLabel, "目标会话：meditation"),
		"自述头须带装配登记的冥想线保留会话名")
}

// TestDeliverToolRefusalKeepsTurnAlive 钉住 白名单外目标的拒绝面。
// - 模型读到 [delivery_denied] 具名文本与白名单现状，反思回合继续。
// - 被拒的投递不发生任何跨 agent 写入。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToolRefusalKeepsTurnAlive(t *testing.T) {
	rig := newDeliverE2ERig(t, []string{"target"})
	rig.model.script(`{"target":"ghost","content":"幽灵投放正文"}`)

	_, err := rig.meditator.InjectMessageContext(context.Background(), "u",
		model.NewUserMessage("反思回合开始：尝试投递白名单外目标"))
	require.NoError(t, err)

	medEventually(t, "the refusal text reached the meditator model", func() bool {
		return rig.model.contains(deliverMedLabel, "[delivery_denied]")
	})
	require.True(t, rig.model.contains(deliverMedLabel, "你的白名单是 [target]"),
		"拒绝文案须点名可用的白名单")
	require.True(t, rig.model.contains(deliverMedLabel, "not authorized"),
		"拒绝文案须携带投递缝的具名原因")
	medEventually(t, "the reflection turn continued after the refusal", func() bool {
		return rig.model.count(deliverMedLabel) >= 2
	})

	_, err = rig.target.InjectMessageContext(context.Background(), "u",
		model.NewUserMessage("目标侧取证回合"))
	require.NoError(t, err)
	medEventually(t, "the target served the evidence turn", func() bool {
		return rig.model.contains(deliverTargetLabel, "目标侧取证回合")
	})
	require.False(t, rig.model.contains(deliverTargetLabel, "幽灵投放正文"),
		"被拒的投递不得发生任何跨 agent 写入")
}
