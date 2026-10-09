// delivery_test 钉住 组合根投递面的宿主可见行为：授权、观察面、寻址、未运行拒绝与谱系收口，断言一律在目标一侧取材。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
package tagent

import (
	"bytes"
	"context"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// deliveryFireMarker 是目标自身冥想触发后必然出现在其模型请求里的那一行头。
const deliveryFireMarker = "[meditation] 这是一次定时冥想事件"

// deliveryModel 记录每次被服务请求的调用者身份与全部消息文本：一条投递唯一可见的宿主面就是它进入的那次 turn。
type deliveryModel struct {
	mu    sync.Mutex
	calls []deliveryCall
}

type deliveryCall struct {
	label string
	text  string
}

func (m *deliveryModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	var b strings.Builder
	for _, msg := range req.Messages {
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	m.mu.Lock()
	m.calls = append(m.calls, deliveryCall{label: delegLabel(delegSystemOf(req)), text: b.String()})
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: "delivery-turn-served",
	}}}}
	close(ch)
	return ch, nil
}

func (m *deliveryModel) Info() model.Info { return model.Info{Name: "delivery-model"} }

// count 报告某个调用者被服务的次数。
func (m *deliveryModel) count(label string) int {
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
func (m *deliveryModel) contains(label, sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.label == label && strings.Contains(c.text, sub) {
			return true
		}
	}
	return false
}

// triggerLog 收集入口循环输出里结算出的血统值：投递在目标侧留下的凭据只能从真实事件流里取。
type triggerLog struct {
	mu     sync.Mutex
	values []string
}

func (l *triggerLog) record(v string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.values = append(l.values, v)
}

// all 给出已记录的血统值副本。
func (l *triggerLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.values...)
}

// contains 报告目标是否以该血统结算过。
func (l *triggerLog) contains(v string) bool {
	for _, got := range l.all() {
		if got == v {
			return true
		}
	}
	return false
}

// deliveryRig 是一套三 agent 常驻拓扑：循环运行着的入口 target、投递方 meditator、常驻但循环未起的 peer。
type deliveryRig struct {
	model     *deliveryModel
	target    *agent.TagentAgent
	meditator *agent.TagentAgent
	peer      *agent.TagentAgent
	triggers  *triggerLog
}

// deliveryRigSpec 声明投递方的读授权、观察面与投递白名单，以及入口自身是否挂冥想（不配观察面＝缺省自察）。
type deliveryRigSpec struct {
	read             []string
	observed         []string
	deliverTo        []string
	targetMeditation bool
}

// deliveryConfig 把一次用例的声明渲染成三 agent 组织：入口 target 把 meditator 与 peer 一并收进常驻表。
func deliveryConfig(spec deliveryRigSpec) Config {
	targetMeditation := MeditationConfig{}
	if spec.targetMeditation {
		targetMeditation = MeditationConfig{Enabled: true, Interval: "50ms", MinGap: "1ms"}
	}
	return Config{
		Entry: "target",
		Agents: map[string]AgentConfig{
			"target": {
				SystemPrompt: PromptConfig{Inline: "TARGET-PROMPT"},
				Memory:       MemoryConfig{Type: "memory"},
				Meditation:   targetMeditation,
				Tools: []ToolRef{
					{Kind: "agent", AgentID: "meditator", Description: "跨域策展冥想"},
					{Kind: "agent", AgentID: "peer", Description: "同进程常驻伙伴"},
				},
			},
			"meditator": {
				SystemPrompt: PromptConfig{Inline: "MEDITATOR-PROMPT"},
				Memory:       MemoryConfig{Type: "memory", ReadNamespaces: spec.read},
				Meditation: MeditationConfig{
					Enabled:            true,
					Interval:           "1h",
					MinGap:             "1h",
					ObservedNamespaces: spec.observed,
					DeliverTo:          spec.deliverTo,
				},
			},
			"peer": {
				SystemPrompt: PromptConfig{Inline: "PEER-PROMPT"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
}

// newDeliveryRig 装配三 agent 并启动入口常驻循环，循环输出先汇成血统日志再丢弃。
func newDeliveryRig(t *testing.T, spec deliveryRigSpec) *deliveryRig {
	t.Helper()
	m := &deliveryModel{}
	entry, err := New(deliveryConfig(spec), WithModel(m))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	resident := entry.ResidentTable()
	meditator := resident["meditator"]
	peer := resident["peer"]
	require.NotNil(t, meditator, "投递方必须在本进程常驻表里")
	require.NotNil(t, peer, "伙伴 agent 必须在本进程常驻表里")

	out, err := entry.StartLoop("u", "delivery-session")
	require.NoError(t, err)
	triggers := &triggerLog{}
	go func() {
		for evt := range out {
			if evt == nil {
				continue
			}
			if v, ok := evt.StateDelta[tagentevent.MetaKeyTriggerSource]; ok {
				triggers.record(string(v))
			}
		}
	}()
	return &deliveryRig{model: m, target: entry, meditator: meditator, peer: peer, triggers: triggers}
}

// serveProbe 让入口真实服务一轮并等到它的请求露面：被拒的投递若曾进过信箱，必然在此后某次请求里现身。
func (r *deliveryRig) serveProbe(t *testing.T, tag string) {
	t.Helper()
	_, err := r.target.InjectMessageContext(context.Background(), "user", model.NewUserMessage("probe-"+tag))
	require.NoError(t, err)
	waitFor(t, "target served probe "+tag, func() bool {
		return r.model.contains("TARGET-PROMPT", "probe-"+tag)
	})
}

// liveEventLoops 数出当前存活的事件循环协程：被拒的投递若回退成新循环，这里必然多出一个。
func liveEventLoops() int {
	buf := make([]byte, 1<<24)
	n := runtime.Stack(buf, true)
	return bytes.Count(buf[:n], []byte("runEventLoop"))
}

// TestDeliverToAgent_Allowlist 钉住 投递授权只来自投递方声明的 deliver_to 白名单，空表白名单拒绝一切。
// - 未声明白名单：投递方没有 deliver_to 时任何目标都被拒，报错点名投递方与被拒目标。
// - 白名单外目标：白名单只覆盖 peer 时投给运行中的 target 即拒，target 事后零感知。
// - 运行期盲投：白名单内的目标分区落在观察面之外时，拒绝落在观察面判据而非白名单判据。
// - 装配期盲投：同一份声明根本装配不出来，组合根在构造阶段具名拒绝启动。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToAgent_Allowlist(t *testing.T) {
	t.Run("未声明白名单拒绝一切投递", func(t *testing.T) {
		rig := newDeliveryRig(t, deliveryRigSpec{read: []string{"target"}, observed: []string{"target"}})
		rig.serveProbe(t, "empty-allowlist")
		err := DeliverToAgent(rig.meditator, "target", "session-x", model.NewUserMessage("refused-by-empty-allowlist"))
		require.ErrorIs(t, err, ErrDeliveryNotAllowed)
		require.Contains(t, err.Error(), "meditator", "拒绝须点名没有声明白名单的投递方")
		require.Contains(t, err.Error(), "target", "拒绝须点名被拒的目标")
		rig.serveProbe(t, "empty-allowlist-tail")
		require.False(t, rig.model.contains("TARGET-PROMPT", "refused-by-empty-allowlist"), "被拒的投递不得进入目标的任何一次请求")
	})

	t.Run("白名单外的运行中目标被拒", func(t *testing.T) {
		rig := newDeliveryRig(t, deliveryRigSpec{read: []string{"peer"}, observed: []string{"peer"}, deliverTo: []string{"peer"}})
		rig.serveProbe(t, "outside-allowlist")
		err := DeliverToAgent(rig.meditator, "target", "session-x", model.NewUserMessage("refused-outside-allowlist"))
		require.ErrorIs(t, err, ErrDeliveryNotAllowed)
		require.Contains(t, err.Error(), "peer", "拒绝须点名白名单实际覆盖的目标")
		rig.serveProbe(t, "outside-allowlist-tail")
		require.False(t, rig.model.contains("TARGET-PROMPT", "refused-outside-allowlist"), "白名单外的运行中目标零感知")
		require.Zero(t, rig.model.count("PEER-PROMPT"), "白名单内的未运行目标也不得被顺带跑起来")
	})

	t.Run("白名单目标落在观察面之外即盲投", func(t *testing.T) {
		rig := newDeliveryRig(t, deliveryRigSpec{read: []string{"target"}, observed: []string{"target"}, deliverTo: []string{"target"}})
		registerDeliveryAuthority(rig.meditator, agent.MeditationConfig{
			Enabled:            true,
			ObservedNamespaces: []string{"peer"},
			DeliverTo:          []string{"target"},
		})
		err := DeliverToAgent(rig.meditator, "target", "session-x", model.NewUserMessage("refused-blind-target"))
		require.ErrorIs(t, err, ErrDeliveryBlindTarget)
		require.NotErrorIs(t, err, ErrDeliveryNotAllowed, "盲投拒绝落在观察面判据上，不是白名单判据")
		require.Contains(t, err.Error(), "peer", "拒绝须点名投递方实际观察的分区")
		rig.serveProbe(t, "blind-target-tail")
		require.False(t, rig.model.contains("TARGET-PROMPT", "refused-blind-target"), "盲投不得进入目标信箱")
	})

	t.Run("装配期盲投声明拒绝启动", func(t *testing.T) {
		blind := deliveryRigSpec{read: []string{"target"}, observed: []string{"target"}, deliverTo: []string{"ghost"}}
		_, err := New(deliveryConfig(blind), WithModel(&deliveryModel{}))
		require.Error(t, err, "deliver_to 落在观察面之外的组织必须装配不出来")
		require.Contains(t, err.Error(), "ghost", "拒绝须点名盲投的目标")
		require.Contains(t, err.Error(), "blind delivery", "拒绝须说明它落在观察面之外")
	})
}

// TestDeliverToAgent_UnknownTarget 钉住 寻址面只覆盖同进程常驻表：白名单与观察面都齐备的陌生名字仍被具名拒绝。
// - ghost 有读授权、在观察面内、也在白名单内，却从未被装配进本进程，拒绝因此落在未知目标判据上。
// - 报错点名常驻表实况，宿主据此分辨名字写错与进程未起。
// - 寻址失败不得退化成投递给别的常驻 agent。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToAgent_UnknownTarget(t *testing.T) {
	rig := newDeliveryRig(t, deliveryRigSpec{read: []string{"ghost"}, observed: []string{"ghost"}, deliverTo: []string{"ghost"}})
	rig.serveProbe(t, "unknown-target")
	err := DeliverToAgent(rig.meditator, "ghost", "session-x", model.NewUserMessage("refused-unknown-target"))
	require.ErrorIs(t, err, ErrUnknownDeliveryTarget)
	require.Contains(t, err.Error(), "ghost", "拒绝须点名未知目标")
	require.Contains(t, err.Error(), "peer", "拒绝须给出常驻表实况")
	rig.serveProbe(t, "unknown-target-tail")
	require.False(t, rig.model.contains("TARGET-PROMPT", "refused-unknown-target"), "未知目标的投递不得落到运行中的入口")
	require.Zero(t, rig.model.count("PEER-PROMPT"), "寻址失败不得顺带启动别的常驻 agent")
}

// TestDeliverToAgent_NotRunning 钉住 常驻但循环未起的目标被具名拒绝，错误携带目标名与其循环状态。
// - peer 在白名单与观察面内、寻址成功，但 loopActive=false，拒绝因此落在未运行判据上。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToAgent_NotRunning(t *testing.T) {
	rig := newDeliveryRig(t, deliveryRigSpec{read: []string{"peer"}, observed: []string{"peer"}, deliverTo: []string{"peer"}})
	require.False(t, rig.peer.IsLoopActive(), "本用例的前提是 peer 的常驻循环没有运行")
	err := DeliverToAgent(rig.meditator, "peer", "session-x", model.NewUserMessage("refused-not-running"))
	require.ErrorIs(t, err, ErrDeliveryTargetNotRunning)
	require.Contains(t, err.Error(), "peer", "拒绝须点名未运行的目标")
	require.Zero(t, rig.model.count("PEER-PROMPT"), "未运行目标的投递不得让它跑起来")
}

// TestDeliverToAgent_NoOneShotFallback 钉住 未运行拒绝的唯一副作用就是错误本身：既不回退成一次性 Run，也不起循环、不置关闭位。
// - 连续拒绝前后，存活事件循环协程数不变、peer 的循环状态与关闭位不变、peer 的模型调用数不变。
// - 被拒内容在入口其后的轮次里也不现身，证明它没有被转存到任何等待面后再补投。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToAgent_NoOneShotFallback(t *testing.T) {
	rig := newDeliveryRig(t, deliveryRigSpec{read: []string{"peer"}, observed: []string{"peer"}, deliverTo: []string{"peer"}})
	rig.serveProbe(t, "one-shot")
	loopsBefore := liveEventLoops()
	callsBefore := rig.model.count("PEER-PROMPT")
	refused := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		payload := "no-one-shot-" + strconv.Itoa(i)
		refused = append(refused, payload)
		require.ErrorIs(t, DeliverToAgent(rig.meditator, "peer", "session-x", model.NewUserMessage(payload)),
			ErrDeliveryTargetNotRunning)
	}
	require.Equal(t, loopsBefore, liveEventLoops(), "被拒的投递不得留下新的事件循环协程")
	require.False(t, rig.peer.IsLoopActive(), "被拒的投递不得启动 peer 的常驻循环")
	require.False(t, rig.peer.CloseStarted(), "被拒的投递不得触碰 peer 的生命周期")
	require.Equal(t, callsBefore, rig.model.count("PEER-PROMPT"), "被拒的投递不得让 peer 跑任何一次 Run")
	rig.serveProbe(t, "one-shot-tail")
	for _, payload := range refused {
		require.False(t, rig.model.contains("TARGET-PROMPT", payload), "被拒内容不得被转存后补投给别的目标")
	}
}

// TestDeliverToAgent_LineageMeditationSelfTelemetry 钉住 投递在目标侧只以 meditation 血统落地，且该血统按单源判定计为自管遥测。
// - 目标请求里带着投递面盖上的来源头（来源 agent 与目标会话），目标不回查即可理解这条消息从哪来。
// - 目标结算事件里真实出现的血统值喂给生产同款审计器：全部样本计为自管，一个都不算成用户发起的交互。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToAgent_LineageMeditationSelfTelemetry(t *testing.T) {
	rig := newDeliveryRig(t, deliveryRigSpec{read: []string{"target"}, observed: []string{"target"}, deliverTo: []string{"target"}})
	require.NoError(t, DeliverToAgent(rig.meditator, "target", "cross-session-42", model.NewUserMessage("delivered-payload-alpha")))
	waitFor(t, "target served the delivered payload", func() bool {
		return rig.model.contains("TARGET-PROMPT", "delivered-payload-alpha")
	})
	require.True(t, rig.model.contains("TARGET-PROMPT", deliveryHeader+" 来源 agent：meditator"), "投递消息须自带来源 agent")
	require.True(t, rig.model.contains("TARGET-PROMPT", "目标会话：cross-session-42"), "投递消息须自带目标会话")
	waitFor(t, "target settled the delivery under the meditation lineage", func() bool {
		return rig.triggers.contains(tagentevent.LineageMeditation)
	})
	recorded := rig.triggers.all()
	require.NotContains(t, recorded, "user", "投递不得被目标结算成用户发起的交互")
	auditor := agent.NewSelfTelemetryAuditor(nil)
	for _, lineage := range recorded {
		auditor.ObserveSettle(map[string]any{tagentevent.MetaKeyTriggerSource: lineage})
	}
	level, ratio, samples := auditor.Snapshot()
	require.Positive(t, samples, "目标侧的结算血统必须进入遥测审计")
	require.InDelta(t, 1.0, ratio, 1e-9, "目标侧结算出的每一条血统都按单源判定计为自管")
	require.Zero(t, level, "样本远未到遥测审计的升档门槛")
	require.True(t, tagentevent.SelfManagedLineage(tagentevent.LineageMeditation), "meditation 谱系对遥测审计是自管流量")
	require.True(t, tagentevent.DeliverableLineage(tagentevent.LineageMeditation), "同一条谱系对宿主可见，才谈得上投递")
}

// TestDeliverToAgent_NoveltyNotRearmedByDelivery 钉住 目标消费投递不重开自身的冥想 novelty 门：投递是供给，不是刺激。
// - 入口挂着冥想（缺省自察观察面；间隔 50ms、min_gap 1ms），两条投递被真实消费后跨若干个间隔仍不触发一次冥想。
// - 同一装配下注入一条真实用户输入，冥想随即触发，证明上一条否定断言不是门本来就该关着。
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestDeliverToAgent_NoveltyNotRearmedByDelivery(t *testing.T) {
	rig := newDeliveryRig(t, deliveryRigSpec{
		read:             []string{"target"},
		observed:         []string{"target"},
		deliverTo:        []string{"target"},
		targetMeditation: true,
	})
	require.NoError(t, DeliverToAgent(rig.meditator, "target", "session-n", model.NewUserMessage("delivery-should-not-arm-1")))
	require.NoError(t, DeliverToAgent(rig.meditator, "target", "session-n", model.NewUserMessage("delivery-should-not-arm-2")))
	waitFor(t, "target consumed both deliveries", func() bool {
		return rig.model.contains("TARGET-PROMPT", "delivery-should-not-arm-2")
	})
	require.Never(t, func() bool {
		return rig.model.contains("TARGET-PROMPT", deliveryFireMarker)
	}, 400*time.Millisecond, 40*time.Millisecond, "消费投递不得重开目标自己的冥想 novelty 门")
	_, err := rig.target.InjectMessageContext(context.Background(), "user", model.NewUserMessage("user-arms-novelty"))
	require.NoError(t, err)
	waitFor(t, "a real user injection arms the target's own meditation", func() bool {
		return rig.model.contains("TARGET-PROMPT", deliveryFireMarker)
	})
}
