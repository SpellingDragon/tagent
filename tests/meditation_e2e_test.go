// meditation_e2e_test 钉住 外部化冥想在单进程双 agent 组织里的端到端链条：真实 novelty 判据产出的卡片经组合根投递面进入目标 turn，
// 自管产出不让两台门自激，投递与用户输入同批时让位。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package tagent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	targetLabel           = "E2E-TARGET-PROMPT"
	meditatorLabel        = "E2E-MEDITATOR-PROMPT"
	meditationCardText    = "跨域巩固卡片正文"
	meditationFireMarker  = "[meditation] 这是一次定时冥想事件"
	deliverySourceHeading = "[delivery] 来源 agent：meditator"
)

// medCall 是一次被服务的模型请求：调用者身份取自其 system prompt 的首个词元，文本取自全部消息。
type medCall struct {
	label string
	text  string
}

// meditationModel 记录每次请求的调用者与文本，并可把下一次调用停在闸门上，等测试放行才返回。
type meditationModel struct {
	mu      sync.Mutex
	calls   []medCall
	pending chan struct{}
}

func (m *meditationModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	var b strings.Builder
	for _, msg := range req.Messages {
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	m.mu.Lock()
	m.calls = append(m.calls, medCall{label: medLabel(medSystemOf(req)), text: b.String()})
	gate := m.pending
	m.pending = nil
	m.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "meditation-e2e-served",
	}}}}
	close(ch)
	return ch, nil
}

func (m *meditationModel) Info() model.Info { return model.Info{Name: "meditation-e2e-model"} }

// armNext 让下一次模型调用停在闸门上，返回的通道被测试关闭即放行。
func (m *meditationModel) armNext() chan struct{} {
	gate := make(chan struct{})
	m.mu.Lock()
	m.pending = gate
	m.mu.Unlock()
	return gate
}

func (m *meditationModel) count(label string) int {
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

func (m *meditationModel) contains(label, sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.label == label && strings.Contains(c.text, sub) {
			return true
		}
	}
	return false
}

func medSystemOf(req *model.Request) string {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem {
			return msg.Content
		}
	}
	return ""
}

func medLabel(sys string) string {
	if f := strings.Fields(sys); len(f) > 0 {
		return f[0]
	}
	return sys
}

// medEventually 等到条件成立：链条上的每一环都以宿主可见的模型请求或结算血统为观察点。
func medEventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 20*time.Second, 20*time.Millisecond, "timeout waiting for: %s", what)
}

// medLineageLog 收集入口循环结算出的血统值：投递在目标侧留下的凭据只能从真实事件流里取。
type medLineageLog struct {
	mu     sync.Mutex
	values []string
}

func (l *medLineageLog) record(v string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.values = append(l.values, v)
}

func (l *medLineageLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.values...)
}

// contains 报告入口是否以该血统结算过。
func (l *medLineageLog) contains(v string) bool {
	for _, got := range l.all() {
		if got == v {
			return true
		}
	}
	return false
}

// cardCollector 是冥想 manager 的注入面替身：一次触发产出的卡片在此可见，随后交给组合根投递。
type cardCollector struct {
	mu    sync.Mutex
	cards []model.Message
}

func (c *cardCollector) InjectMessageWithSource(_ string, msg model.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cards = append(c.cards, msg)
}

func (c *cardCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cards)
}

// latest 给出最近一次触发产出的卡片。
func (c *cardCollector) latest(t *testing.T) model.Message {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	require.NotEmpty(t, c.cards, "冥想 manager 还没有产出过卡片")
	return c.cards[len(c.cards)-1]
}

// meditationRig 是共享同一条事实链的双 agent 进程：循环运行着的入口 target 与挂外部观察冥想的 meditator。
type meditationRig struct {
	target    *agent.TagentAgent
	meditator *agent.TagentAgent
	model     *meditationModel
	lineages  *medLineageLog
	store     memory.MemoryStore
}

// newMeditationRig 装配双 agent：type memory 且同 path 即同一进程内实例，因此无需磁盘也 -short 安全。
// targetMeditation 决定入口自身是否挂 in-loop 冥想（观察面为空即自体维护形态）。
func newMeditationRig(t *testing.T, targetMeditation bool) *meditationRig {
	t.Helper()
	dir := t.TempDir()
	sharedPath := tagent.MemoryConfig{Type: "memory", Path: dir}
	targetMeditationConfig := tagent.MeditationConfig{}
	if targetMeditation {
		targetMeditationConfig = tagent.MeditationConfig{Enabled: true, Interval: "50ms", MinGap: "1ms"}
	}
	cfg := tagent.Config{
		Entry: "target",
		Agents: map[string]tagent.AgentConfig{
			"target": {
				SystemPrompt: tagent.PromptConfig{Inline: targetLabel},
				Memory:       sharedPath,
				Meditation:   targetMeditationConfig,
				Tools:        []tagent.ToolRef{{Kind: "agent", AgentID: "meditator", Description: "跨域策展冥想"}},
			},
			"meditator": {
				SystemPrompt: tagent.PromptConfig{Inline: meditatorLabel},
				Memory:       tagent.MemoryConfig{Type: "memory", Path: dir, ReadNamespaces: []string{"target"}},
				Meditation: tagent.MeditationConfig{
					Enabled:            true,
					Interval:           "1h",
					MinGap:             "1h",
					ObservedNamespaces: []string{"target"},
					DeliverTo:          []string{"target"},
				},
			},
		},
	}
	m := &meditationModel{}
	entry, err := tagent.New(cfg, tagent.WithModel(m))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	meditator := entry.ResidentTable()["meditator"]
	require.NotNil(t, meditator, "冥想 agent 必须在本进程常驻表里")
	out, err := entry.StartLoop("e2e-user", "e2e-target-session")
	require.NoError(t, err)
	lineages := &medLineageLog{}
	go func() {
		for evt := range out {
			if evt == nil {
				continue
			}
			if v, ok := evt.StateDelta[tagentevent.MetaKeyTriggerSource]; ok {
				lineages.record(string(v))
			}
		}
	}()
	return &meditationRig{target: entry, meditator: meditator, model: m, lineages: lineages, store: entry.MemStore()}
}

// startExternalMeditation 让生产 MeditationManager 以外部观察形态跑起来：novelty 判据、观察摘要与卡片文案全部出自生产代码。
func startExternalMeditation(t *testing.T, reader memory.MemoryStore, cards *cardCollector) *agent.MeditationManager {
	t.Helper()
	mgr := agent.NewMeditationManager(agent.MeditationConfig{
		Enabled:            true,
		Interval:           50 * time.Millisecond,
		MinGap:             time.Millisecond,
		PromptText:         meditationCardText,
		ObservedNamespaces: []string{"target"},
	}, cards)
	mgr.SetNoveltyReader(reader)
	mgr.UpdateLastTurnEnd(time.Now())
	mgr.Start()
	t.Cleanup(mgr.Stop)
	return mgr
}

// TestExternalMeditationEndToEnd 钉住 目标写用户事件到冥想产出再被目标消费的完整链条，以及目标遥测把投递轮次计为自管。
// - 冥想触发由生产判据在共享事实链上做出：卡片带外部形态的观察分区摘要，测试不复写任何判据。
// - 投递成功后目标的一轮请求里同时出现卡片正文与投递面的来源头，目标不回查即知这条来自哪个 agent。
// - 目标的结算血统全部进生产同款审计器：投递轮次的血统计入自管样本，用户轮次如实稀释占比。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestExternalMeditationEndToEnd(t *testing.T) {
	rig := newMeditationRig(t, false)
	cards := &cardCollector{}
	startExternalMeditation(t, rig.store, cards)

	_, err := rig.target.InjectMessageContext(context.Background(), "user",
		model.NewUserMessage("用户事实：交付窗口必须周五关闭"))
	require.NoError(t, err)
	medEventually(t, "target served the user fact", func() bool {
		return rig.model.contains(targetLabel, "交付窗口必须周五关闭")
	})

	medEventually(t, "the external novelty gate fires on the observed user fact", func() bool {
		return cards.count() >= 1
	})
	card := cards.latest(t)
	require.Contains(t, card.Content, meditationFireMarker, "产出必须是真实冥想卡片")
	require.Contains(t, card.Content, "观察分区概况", "外部形态的卡片带跨域观察摘要")
	require.Contains(t, card.Content, "分区 target", "卡片摘要点名被观察分区")
	require.Contains(t, card.Content, "最近非自管活动：[", "卡片自带定位所涉事件的事件键引用")
	require.Regexp(t, `最近非自管活动：\[[0-9a-f]+\]`, card.Content, "事件键为可解析格式，目标不必回查即可定位")

	require.NoError(t, tagent.DeliverToAgent(rig.meditator, "target", "e2e-target-session", card))
	medEventually(t, "target consumed the delivered card", func() bool {
		return rig.model.contains(targetLabel, meditationCardText)
	})
	require.True(t, rig.model.contains(targetLabel, deliverySourceHeading), "投递消息须自带来源 agent")
	require.True(t, rig.model.contains(targetLabel, "目标会话：e2e-target-session"), "投递消息须自带目标会话")

	medEventually(t, "target settled both the user turn and the delivered turn", func() bool {
		return rig.lineages.contains("user") && rig.lineages.contains(tagentevent.LineageMeditation)
	})
	recorded := rig.lineages.all()
	auditor := agent.NewSelfTelemetryAuditor(nil)
	for _, lineage := range recorded {
		auditor.ObserveSettle(map[string]any{tagentevent.MetaKeyTriggerSource: lineage})
	}
	_, ratio, samples := auditor.Snapshot()
	require.Equal(t, len(recorded), samples, "目标结算出的每一条血统都进遥测审计")
	selfManaged := 0
	for _, lineage := range recorded {
		if tagentevent.SelfManagedLineage(lineage) {
			selfManaged++
		}
	}
	require.Positive(t, selfManaged, "投递轮次的血统按单源判为自管")
	require.InDelta(t, float64(selfManaged)/float64(samples), ratio, 1e-9, "自管占比只由单源判定得出")
	require.Less(t, ratio, 1.0, "存在用户轮次时自管占比必须被稀释")
}

// TestExternalMeditation_NoPerpetualMotion 钉住 只有自管产出循环时两台冥想门都保持静默。
// - 一次真实用户事实先让入口的 in-loop 门触发一次，再让外部观察门触发一次，两个计数因此都不是空转得来。
// - 此后循环的只有 meditation 卡片投递与 consolidation_hint 提示：跨十个冥想间隔，两侧计数都停在基线。
// - 自管产出对目标只是供给，对策展者只是水位之后的非新鲜证据，谁都不能把对方的 novelty 重新打开。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestExternalMeditation_NoPerpetualMotion(t *testing.T) {
	rig := newMeditationRig(t, true)
	cards := &cardCollector{}

	_, err := rig.target.InjectMessageContext(context.Background(), "user",
		model.NewUserMessage("用户事实：唯一一次非自管刺激"))
	require.NoError(t, err)
	medEventually(t, "target served the user fact", func() bool {
		return rig.model.contains(targetLabel, "唯一一次非自管刺激")
	})
	medEventually(t, "the target's own in-loop meditation fires", func() bool {
		return rig.model.contains(targetLabel, meditationFireMarker)
	})

	startExternalMeditation(t, rig.store, cards)
	medEventually(t, "the external form fires once on the same user fact", func() bool {
		return cards.count() >= 1
	})
	card := cards.latest(t)
	require.NoError(t, tagent.DeliverToAgent(rig.meditator, "target", "e2e-target-session", card))
	medEventually(t, "target consumed the delivered card", func() bool {
		return rig.model.contains(targetLabel, meditationCardText)
	})
	rig.target.InjectMessageWithSource(tagentevent.LineageConsolidationHint,
		model.NewUserMessage("巩固提示：自管流量"))
	medEventually(t, "target consumed the consolidation hint", func() bool {
		return rig.model.contains(targetLabel, "巩固提示：自管流量")
	})

	second := card
	second.Content += "\n第二张卡片：证明自管产出仍可继续投递"
	require.NoError(t, tagent.DeliverToAgent(rig.meditator, "target", "e2e-target-session", second),
		"再投一张自管产出仍要成功")
	medEventually(t, "target consumed the second delivered card", func() bool {
		return rig.model.contains(targetLabel, "第二张卡片")
	})

	fires := cards.count()
	calls := rig.model.count(targetLabel)
	require.Equal(t, 1, fires, "外部形态到此只触发过一次")

	require.Never(t, func() bool { return cards.count() > fires },
		500*time.Millisecond, 40*time.Millisecond, "只有自管产出循环时外部形态不得再触发")
	require.Never(t, func() bool { return rig.model.count(targetLabel) > calls },
		500*time.Millisecond, 40*time.Millisecond, "消费自管产出不得让任何一侧再多产出一轮")
}

// TestExternalMeditation_YieldToUser 钉住 投递与用户输入同批时沿用混合批次条款：投递被移除且不补偿，返回语义只是已进入 mailbox。
// - 投递发生在目标停在 turn 一的闸门期间：返回 nil 时目标的模型调用次数没有增长，成功不等于已被消费。
// - 放行后同一批里用户输入被服务、投递被移除，其后任何一轮的请求文本里都没有卡片正文，移除即终局。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestExternalMeditation_YieldToUser(t *testing.T) {
	rig := newMeditationRig(t, false)
	gate := rig.model.armNext()

	_, err := rig.target.InjectMessageContext(context.Background(), "user",
		model.NewUserMessage("用户输入：turn 一的驱动消息"))
	require.NoError(t, err)
	medEventually(t, "target is parked inside turn one", func() bool {
		return rig.model.contains(targetLabel, "turn 一的驱动消息")
	})
	require.Equal(t, 1, rig.model.count(targetLabel), "闸门未放行时目标只应在 turn 一里")

	require.NoError(t, tagent.DeliverToAgent(rig.meditator, "target", "e2e-target-session",
		model.NewUserMessage("冥想卡片：应与用户输入同批让位")), "投递成功的语义只是已进入 mailbox")
	require.Equal(t, 1, rig.model.count(targetLabel), "投递不得让目标在 turn 一之外多跑一次模型调用")

	_, err = rig.target.InjectMessageContext(context.Background(), "user",
		model.NewUserMessage("用户输入：同批里必须胜出的那一条"))
	require.NoError(t, err)
	close(gate)

	medEventually(t, "the user input sharing the batch got served", func() bool {
		return rig.model.contains(targetLabel, "同批里必须胜出的那一条")
	})
	require.Equal(t, 2, rig.model.count(targetLabel), "被移除的投递不应换来任何补偿轮次")

	_, err = rig.target.InjectMessageContext(context.Background(), "user",
		model.NewUserMessage("用户输入：再走一轮取证"))
	require.NoError(t, err)
	medEventually(t, "the evidence turn got served", func() bool {
		return rig.model.count(targetLabel) >= 3
	})
	require.False(t, rig.model.contains(targetLabel, "应与用户输入同批让位"), "同批让位的投递不得被重新排队补投")
}
