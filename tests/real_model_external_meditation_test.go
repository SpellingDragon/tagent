// real_model_external_meditation_test 钉住 外部化冥想在真实 provider 上的整条跨域策展链：真实模型写下的用户事实带着持久归因进共享事实链，
// 外部形态的 novelty 判据在同一条链上开门，策展回合的卡片落在冥想 agent 自身分区，再经组合根投递面进入目标的下一个真实回合。
// 契约: docs/wiki/agent/agent-architecture.md#meditation-two-forms
package tagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	trpcevent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	tagent "github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/testutil"
)

// realExtMedMaxCalls 一族是外部化冥想真实验收的调用预算：目标回合、策展回合、目标消费回合各一次
// 真实往返，多一次就说明链条上冒出了计划外的模型调用。单次输出上界、单请求估计上界、单次超时与
// 一轮排空上限沿用 realAccept* 一族的集中常量，本块只补充本场景特有的两条等待上界。
const (
	realExtMedMaxCalls  = 3
	realExtMedChainWait = 3 * time.Minute
	realExtMedProbeWait = 25 * time.Second
	// extMedUnroutableEndpoint 是非空转探针用的端点：本地保留端口，连接必然被拒。
	extMedUnroutableEndpoint = "http://127.0.0.1:1/v1"
)

// extMedTargetAgent 一族是本场景的身份与文案标记：两个 agent 的系统提示各以一个词元开头，
// 路由器据此把每次真实调用归到各自的实例与账目标签上。
const (
	extMedTargetAgent        = "target"
	extMedCurationAgent      = "meditator"
	extMedLoopUser           = "extmed-user"
	extMedTargetSession      = "extmed-target-session"
	extMedCurationSession    = "extmed-curation-session"
	extMedTargetToken        = "EXTMED-TARGET"
	extMedCurationToken      = "EXTMED-MEDITATOR"
	extMedCardToken          = "EXTMED-CARD"
	extMedTargetCall         = "extmed-target"
	extMedCurationCall       = "extmed-curation"
	extMedUserLineage        = "user"
	extMedCurationPromptFile = "extmed-curation.md"
	extMedUserFact           = "用户事实：真实模型场景里，交付窗口在本周五关闭"
)

// extMedDigestMarkers 是外部形态卡片提示里由生产代码渲染的观察摘要片段：全部命中才说明
// novelty 判据真的在事实链上开了门，并把盖着 user 归因的那条活动带进了这次真实请求。
var extMedDigestMarkers = []string{
	"观察分区概况",
	"分区 " + extMedTargetAgent,
	"最近非自管活动",
	"（trigger_source=" + extMedUserLineage + "）",
	meditationFireMarker,
}

// extMedCall 是一次被路由的真实请求：调用者身份取自其 system prompt 的首个词元，文本取自全部消息。
type extMedCall struct {
	token string
	text  string
}

// extMedRouter 把组织里两类 agent 的真实调用分到各自的真实实例上并按实例标签入账：未登记的
// 调用者与被用尽的预算都只会被拒发，不让一次计划外的调用悄悄花掉配额。
type extMedRouter struct {
	byToken map[string]*realAcceptModel
	ledger  *realAcceptLedger
	limit   int

	mu      sync.Mutex
	calls   []extMedCall
	refused []string
}

// GenerateContent 定位调用者、核对预算，再把请求原样交给被登记的那个真实实例。
func (r *extMedRouter) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	token := medLabel(medSystemOf(req))
	inner, known := r.byToken[token]
	if !known {
		r.note(fmt.Sprintf("调用者 %q 未在本场景登记", token))
		return nil, fmt.Errorf("extmed: caller %q is not registered in this scenario", token)
	}
	if served := r.ledger.count(); served >= r.limit {
		r.note(fmt.Sprintf("%s 在第 %d 次调用时预算已用尽", token, served+1))
		return nil, fmt.Errorf("extmed: real-call budget %d exhausted before caller %s", r.limit, token)
	}
	text := extMedRequestText(req)
	r.mu.Lock()
	r.calls = append(r.calls, extMedCall{token: token, text: text})
	r.mu.Unlock()
	return inner.GenerateContent(ctx, req)
}

// Info 报告路由器身份；入账的模型名由被包装的真实实例交出。
func (r *extMedRouter) Info() model.Info { return model.Info{Name: "extmed-real-router"} }

// sawRequest 报告某个调用者的任一请求文本是否同时包含给定的全部片段。
func (r *extMedRouter) sawRequest(token string, subs ...string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c.token != token {
			continue
		}
		matched := true
		for _, sub := range subs {
			if !strings.Contains(c.text, sub) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// refusals 交出预算外尝试的说明，用于核对「一次也没多花」。
func (r *extMedRouter) refusals() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.refused...)
}

// note 登记一次被拒发的调用尝试。
func (r *extMedRouter) note(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refused = append(r.refused, reason)
}

// extMedRequestText 把一次请求的全部消息文本拼在一起，供观察每个 agent 的回合读到了什么内容。
func extMedRequestText(req *model.Request) string {
	var b strings.Builder
	for _, msg := range req.Messages {
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// extMedEvidence 是一次外部化冥想真实跑留下的全部可核对事实。取证路径不容中途失败：缺哪一环
// 由 missingEvidence 说清楚，同一段代码因此既能跑主跑也能跑探针。
type extMedEvidence struct {
	injectErr   string
	readErr     string
	deliveryErr string
	// userFact 是事实链上那条用户事件，落盘样本与归因断言都以它为准。
	userFact           *memory.FullEvent
	userFactLineage    string
	targetReply        string
	curationDigest     bool
	card               string
	cardLineage        string
	cardPartition      int
	targetNewWrites    int
	targetCompress     int
	cardSeenByTarget   bool
	headerSeenByTarget bool
	targetFinalReply   string
	lineages           []string
	served             []string
	refused            []string
	servedTokens       int
}

// missingEvidence 把断言面摊成一份缺口清单：任何一条非空都意味着这条真实链条没跑通。
func (e extMedEvidence) missingEvidence() []string {
	var gaps []string
	if e.injectErr != "" {
		gaps = append(gaps, "用户注入被拒: "+e.injectErr)
	}
	if e.readErr != "" {
		gaps = append(gaps, "事实链读不通: "+e.readErr)
	}
	if e.userFact == nil {
		gaps = append(gaps, "事实链上没有那条用户事件")
	}
	if e.userFactLineage != extMedUserLineage {
		gaps = append(gaps, fmt.Sprintf("用户事件上的持久 trigger_source 不是 %q（读到 %q）", extMedUserLineage, e.userFactLineage))
	}
	if e.targetReply == "" {
		gaps = append(gaps, "目标 agent 没有交出真实回合的输出")
	}
	if !e.curationDigest {
		gaps = append(gaps, "策展 agent 的真实请求里没有外部形态观察摘要：novelty 判据没在事实链上开门")
	}
	if e.card == "" {
		gaps = append(gaps, "冥想 agent 自身分区上没有策展回合的卡片")
	}
	if e.cardPartition != memory.PartitionIDFromName(extMedCurationAgent) {
		gaps = append(gaps, fmt.Sprintf("卡片不在冥想 agent 自身分区（读到分区 %d）", e.cardPartition))
	}
	if e.cardLineage != tagentevent.LineageMeditation {
		gaps = append(gaps, fmt.Sprintf("卡片谱系不是 %q（读到 %q）", tagentevent.LineageMeditation, e.cardLineage))
	}
	if e.targetNewWrites != 0 {
		gaps = append(gaps, fmt.Sprintf("策展回合往被观察分区写了 %d 条事件", e.targetNewWrites))
	}
	if e.targetCompress != 0 {
		gaps = append(gaps, fmt.Sprintf("外部化冥想在被观察分区产生了 %d 条压缩事件", e.targetCompress))
	}
	if e.deliveryErr != "" {
		gaps = append(gaps, "投递被拒: "+e.deliveryErr)
	}
	if !e.cardSeenByTarget {
		gaps = append(gaps, "目标的真实回合里没读到被投递的卡片正文")
	}
	if !e.headerSeenByTarget {
		gaps = append(gaps, "目标的真实回合里没有投递来源头")
	}
	if e.targetFinalReply == "" {
		gaps = append(gaps, "目标消费卡片的那一轮没有输出")
	}
	if len(e.served) != realExtMedMaxCalls {
		gaps = append(gaps, fmt.Sprintf("真实调用数是 %d，不是三段链条的 %d 次：%v", len(e.served), realExtMedMaxCalls, e.served))
	}
	if e.servedTokens <= 0 {
		gaps = append(gaps, "没有任何 token 入账，等于零调用")
	}
	if len(e.refused) != 0 {
		gaps = append(gaps, fmt.Sprintf("预算之外还尝试了 %d 次真实调用: %v", len(e.refused), e.refused))
	}
	if !extMedSettled(e.lineages, extMedTargetAgent+"="+extMedUserLineage) {
		gaps = append(gaps, fmt.Sprintf("目标没有以 %q 血统结算过用户回合：%v", extMedUserLineage, e.lineages))
	}
	if !extMedSettled(e.lineages, extMedTargetAgent+"="+tagentevent.LineageMeditation) {
		gaps = append(gaps, fmt.Sprintf("目标没有以 %q 血统结算过被投递的回合：%v", tagentevent.LineageMeditation, e.lineages))
	}
	return gaps
}

// extMedSettled 报告目标循环是否以该血统结算过一次。
func extMedSettled(lineages []string, want string) bool {
	for _, got := range lineages {
		if got == want {
			return true
		}
	}
	return false
}

// extMedScenario 是跑在真实 provider 上的双 agent 进程：常驻循环的入口 target 与挂外部观察冥想的
// 策展 agent，两者按同一 localfile 路径共享一条事实链。
type extMedScenario struct {
	t         *testing.T
	target    *agent.TagentAgent
	curator   *agent.TagentAgent
	store     memory.MemoryStore
	router    *extMedRouter
	ledger    *realAcceptLedger
	eventsDir string

	mu         sync.Mutex
	curationUp bool
	stopped    bool
	lineages   []string
}

// extMedGate 沿用真实模型验收的三态门：未置 1 即 Skip，这是本用例唯一合法的 Skip；置 1 后端点、
// 模型名、凭据缺任何一项都是 Fatal。凭据只从环境显式读取，不 source 任何 shell 配置；端点与模型名
// 先读环境变量，两处都未给出时回落本仓测试配置的默认值。
// 契约: docs/wiki/rl/rl-architecture.md#dual-stream-cli
func extMedGate(t *testing.T) realAcceptCredentials {
	t.Helper()
	if os.Getenv("TAGENT_REQUIRE_REAL_MODEL") != "1" {
		t.Skip("真实模型验收只在 TAGENT_REQUIRE_REAL_MODEL=1 时执行；其余档位不得把真实调用计成证据")
	}
	creds := realAcceptCredentials{
		Endpoint:  strings.TrimSpace(os.Getenv("TRPC_CLAW_API_ENDPOINT")),
		ModelName: strings.TrimSpace(os.Getenv("TRPC_CLAW_MODEL_NAME")),
		APIKey:    strings.TrimSpace(os.Getenv("ZAI_API_KEY")),
	}
	if creds.APIKey == "" {
		t.Fatal("TAGENT_REQUIRE_REAL_MODEL=1 但 ZAI_API_KEY 缺失：凭据只从环境显式读取，不 source shell")
	}
	if creds.Endpoint == "" || creds.ModelName == "" {
		fallback, err := testutil.LoadConfig()
		require.NoErrorf(t, err, "环境变量没给端点与模型名，本仓测试配置也装配不出来：%v", err)
		if creds.Endpoint == "" {
			creds.Endpoint = fallback.Endpoint
		}
		if creds.ModelName == "" {
			creds.ModelName = fallback.ModelName
		}
		t.Logf("extmed endpoint: %s model=%s (env override absent, repo test config default applies)", creds.Endpoint, creds.ModelName)
	}
	return creds
}

// extMedCurationPrompt 是给真实模型的卡片指令：产出形态由提示词规定，判据与观察内容一律出自生产渲染。
func extMedCurationPrompt() string {
	return strings.Join([]string{
		"把上面「最近非自管活动」那一条改写成一整行跨域提示卡片，整行以 " + extMedCardToken + " 开头。",
		"只输出这一行，不超过 40 个字，不要调用任何工具，不要复述其他内容。",
	}, "\n")
}

// newExtMedScenario 装配两条常驻循环中的入口一条，并把策展 agent 的空闲锚按生产耐久面预置：
// 门控的其余部分（novelty 判据、卡片、水位、投递）全部由生产代码现场决定。
func newExtMedScenario(t *testing.T, creds realAcceptCredentials, root string) *extMedScenario {
	t.Helper()
	promptDir := filepath.Join(root, "prompts")
	anchorDir := filepath.Join(root, "anchors")
	eventsDir := filepath.Join(root, "events")
	for _, dir := range []string{promptDir, anchorDir, eventsDir} {
		require.NoErrorf(t, os.MkdirAll(dir, 0o700), "验收目录必须可建 %s", dir)
	}
	require.NoError(t, os.WriteFile(filepath.Join(promptDir, extMedCurationPromptFile),
		[]byte(extMedCurationPrompt()), 0o600))

	anchorPath := filepath.Join(anchorDir, extMedCurationAgent+".json")
	anchors, err := reliability.NewAnchorStore(anchorPath)
	require.NoError(t, err)
	require.NoErrorf(t, anchors.Save(reliability.MeditationAnchors{
		LastTurnEnd: time.Now().Add(-time.Second).UnixMilli(),
	}), "空闲锚必须按生产耐久面预置 %s", anchorPath)

	ledger := &realAcceptLedger{}
	router := &extMedRouter{
		limit:  realExtMedMaxCalls,
		ledger: ledger,
		byToken: map[string]*realAcceptModel{
			extMedTargetToken:   newRealAcceptModel(creds, "", extMedTargetCall, ledger),
			extMedCurationToken: newRealAcceptModel(creds, "", extMedCurationCall, ledger),
		},
	}

	cfg := tagent.Config{
		Entry:       extMedTargetAgent,
		PromptDir:   promptDir,
		Reliability: tagent.ReliabilityConfig{MeditationAnchorDir: anchorDir},
		Agents: map[string]tagent.AgentConfig{
			extMedTargetAgent: {
				SystemPrompt:      tagent.PromptConfig{Inline: extMedTargetToken + " 只回一句话，不超过 20 个字，禁止调用任何工具。"},
				MaxToolIterations: 1,
				Memory:            tagent.MemoryConfig{Type: "localfile", Path: eventsDir},
				Tools: []tagent.ToolRef{{
					Kind:        tagent.ToolKindAgent,
					AgentID:     extMedCurationAgent,
					Description: "跨域策展冥想 agent（本场景禁止调用）",
					Async:       boolPtr(false),
				}},
			},
			extMedCurationAgent: {
				SystemPrompt:      tagent.PromptConfig{Inline: extMedCurationToken + " 只按冥想提示输出一行卡片，禁止调用任何工具。"},
				MaxToolIterations: 1,
				Memory: tagent.MemoryConfig{Type: "localfile", Path: eventsDir,
					ReadNamespaces: []string{extMedTargetAgent}},
				Meditation: tagent.MeditationConfig{
					Enabled:            true,
					Interval:           "50ms",
					MinGap:             "1ms",
					PromptFile:         extMedCurationPromptFile,
					ObservedNamespaces: []string{extMedTargetAgent},
					DeliverTo:          []string{extMedTargetAgent},
				},
			},
		},
	}

	entry, err := tagent.New(cfg, tagent.WithModel(router))
	require.NoError(t, err)
	curator := entry.ResidentTable()[extMedCurationAgent]
	require.NotNilf(t, curator, "策展 agent 必须在本进程常驻表里，门控与投递都按实例寻址")

	s := &extMedScenario{
		t: t, target: entry, curator: curator, store: entry.MemStore(),
		router: router, ledger: ledger, eventsDir: eventsDir,
	}
	out, err := entry.StartLoop(extMedLoopUser, extMedTargetSession)
	require.NoErrorf(t, err, "入口 target 的常驻循环起不来")
	s.collect(extMedTargetAgent, out)
	t.Cleanup(func() {
		if cerr := s.stop(); cerr != nil {
			t.Logf("extmed teardown: %v", cerr)
		}
	})
	return s
}

// startCuration 此刻才让策展 agent 的常驻循环跑起来：MeditationManager 随 StartLoop 启动，从恢复
// 下来的锚点起步，第一趟 novelty 扫描面对的就是已经入链的那条用户事实。
func (s *extMedScenario) startCuration() {
	out, err := s.curator.StartLoop(extMedLoopUser, extMedCurationSession)
	require.NoErrorf(s.t, err, "策展 agent 的常驻循环起不来")
	s.collect(extMedCurationAgent, out)
	s.mu.Lock()
	s.curationUp = true
	s.mu.Unlock()
}

// collect 排空一条循环输出通道：不排空会把持久循环的产出挤掉，血统与终态响应都在这里落地。
func (s *extMedScenario) collect(tag string, out <-chan *trpcevent.Event) {
	go func() {
		for evt := range out {
			if evt == nil {
				continue
			}
			if v, ok := evt.StateDelta[tagentevent.MetaKeyTriggerSource]; ok {
				s.mu.Lock()
				s.lineages = append(s.lineages, tag+"="+string(v))
				s.mu.Unlock()
			}
		}
	}()
}

// tally 交出某个分区已提交事件的全部事件键与压缩事件条数：零写入不变量按事件键集合判定。
func (s *extMedScenario) tally(agentName string) (map[int64]bool, int, error) {
	facts, err := extMedFacts(s.store, agentName)
	if err != nil {
		return nil, 0, err
	}
	keys := make(map[int64]bool, len(facts))
	compressed := 0
	for _, evt := range facts {
		keys[evt.EventKey] = true
		if evt.EventType == tagentevent.TypeContextCompressSummary {
			compressed++
		}
	}
	return keys, compressed, nil
}

// drive 依次走完三段链条并把证据收齐，每段都以生产侧可见的事实或请求为观察点：
//   - 目标的一次真实回合把用户事实写进共享事实链；
//   - 策展循环启动后的 novelty 扫描开出外部形态的卡片回合；
//   - 卡片经组合根投递面进入目标的下一个真实回合。
func (s *extMedScenario) drive(wait time.Duration) extMedEvidence {
	ev := extMedEvidence{}
	deadline := time.Now().Add(wait)

	if _, err := s.target.InjectMessageContext(context.Background(), extMedUserLineage,
		model.NewUserMessage(extMedUserFact)); err != nil {
		ev.injectErr = err.Error()
	}
	extMedUntil(deadline, func() bool {
		facts, err := extMedFacts(s.store, extMedTargetAgent)
		if err != nil {
			ev.readErr = err.Error()
			return false
		}
		if evt, ok := extMedUserInput(facts, extMedUserFact); ok {
			ev.userFact = &evt
			ev.userFactLineage = evt.Metadata[tagentevent.MetaKeyTriggerSource]
		}
		if evt, ok := extMedTerminal(facts, extMedUserLineage); ok {
			ev.targetReply = evt.Content
		}
		return ev.userFact != nil && ev.targetReply != ""
	})

	before, beforeCompress, terr := s.tally(extMedTargetAgent)
	if terr != nil {
		ev.readErr = terr.Error()
	}
	s.startCuration()
	extMedUntil(deadline, func() bool {
		ev.curationDigest = s.router.sawRequest(extMedCurationToken, extMedDigestMarkers...)
		facts, err := extMedFacts(s.store, extMedCurationAgent)
		if err != nil {
			ev.readErr = err.Error()
			return false
		}
		evt, found := extMedTerminal(facts, tagentevent.LineageMeditation)
		if found {
			ev.card = evt.Content
			ev.cardLineage = evt.Metadata[tagentevent.MetaKeyTriggerSource]
			ev.cardPartition = evt.PartitionID
		}
		return found
	})
	if after, afterCompress, err := s.tally(extMedTargetAgent); err == nil && before != nil {
		ev.targetNewWrites = extMedNewKeys(before, after)
		ev.targetCompress = afterCompress - beforeCompress
	}

	if ev.card != "" {
		if derr := tagent.DeliverToAgent(s.curator, extMedTargetAgent, extMedTargetSession,
			model.NewUserMessage(ev.card)); derr != nil {
			ev.deliveryErr = derr.Error()
		}
		extMedUntil(deadline, func() bool {
			ev.cardSeenByTarget = s.router.sawRequest(extMedTargetToken, ev.card)
			ev.headerSeenByTarget = s.router.sawRequest(extMedTargetToken, deliverySourceHeading)
			facts, err := extMedFacts(s.store, extMedTargetAgent)
			if err == nil {
				if evt, ok := extMedTerminal(facts, tagentevent.LineageMeditation); ok {
					ev.targetFinalReply = evt.Content
				}
			}
			return ev.cardSeenByTarget && ev.headerSeenByTarget && ev.targetFinalReply != ""
		})
	}

	ev.served = s.ledger.labels(s.ledger.since(0))
	prompt, completion := s.ledger.totalTokens()
	ev.servedTokens = prompt + completion
	ev.refused = s.router.refusals()
	s.mu.Lock()
	ev.lineages = append([]string(nil), s.lineages...)
	s.mu.Unlock()
	return ev
}

// stop 关掉两条常驻循环：带着 ticker 的策展循环留在后台会继续发起真实调用。
func (s *extMedScenario) stop() error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.stopped = true
	curationUp := s.curationUp
	s.mu.Unlock()
	if curationUp {
		s.curator.StopLoop()
	}
	return s.target.Close()
}

// extMedUntil 等到证据成立或窗口用尽：到期不判失败，把「缺哪一环」交给判定函数。
func extMedUntil(deadline time.Time, cond func() bool) bool {
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// extMedFacts 读回一个分区已提交的事实全文；取证路径不容失败，读不通就交给证据面说明。
func extMedFacts(store memory.MemoryStore, agentName string) ([]memory.FullEvent, error) {
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName(agentName)},
		Limit:        200,
	})
	if err != nil {
		return nil, err
	}
	out := make([]memory.FullEvent, 0, len(refs))
	for _, ref := range refs {
		evt, gerr := store.GetEvent(ref.EventKey)
		if gerr != nil || evt == nil {
			return nil, fmt.Errorf("extmed: committed fact %s unreadable: %v",
				tagentevent.FormatEventKey(ref.EventKey), gerr)
		}
		out = append(out, *evt)
	}
	return out, nil
}

// extMedUserInput 找出被盖章成 user 的那条输入事件。
func extMedUserInput(facts []memory.FullEvent, marker string) (memory.FullEvent, bool) {
	for _, evt := range facts {
		if evt.EventType != tagentevent.TypeExternalInput {
			continue
		}
		if !strings.Contains(evt.Content, marker) {
			continue
		}
		return evt, true
	}
	return memory.FullEvent{}, false
}

// extMedTerminal 找出给定谱系下最新一条有正文的回合输出。
func extMedTerminal(facts []memory.FullEvent, lineage string) (memory.FullEvent, bool) {
	var (
		newest memory.FullEvent
		found  bool
	)
	for _, evt := range facts {
		if evt.EventType != tagentevent.TypeAgentOutput {
			continue
		}
		if evt.Metadata[tagentevent.MetaKeyTriggerSource] != lineage {
			continue
		}
		if strings.TrimSpace(evt.Content) == "" {
			continue
		}
		if !found || evt.Timestamp >= newest.Timestamp {
			newest, found = evt, true
		}
	}
	return newest, found
}

// extMedNewKeys 数出快照之后新增的事件键。
func extMedNewKeys(before, after map[int64]bool) int {
	added := 0
	for key := range after {
		if !before[key] {
			added++
		}
	}
	return added
}

// extMedTriggerSourceSample 把事实链上那条用户事件的持久归因原样落进验收目录，供与读面样本对账。
func extMedTriggerSourceSample(t *testing.T, runDir string, evt memory.FullEvent) string {
	t.Helper()
	sample := map[string]any{
		"event_key":     tagentevent.FormatEventKey(evt.EventKey),
		"partition_id":  evt.PartitionID,
		"partition":     extMedTargetAgent,
		"event_type":    evt.EventType,
		"timestamp_ms":  evt.Timestamp,
		"content":       evt.Content,
		"metadata":      evt.Metadata,
		"lineage_key":   tagentevent.MetaKeyTriggerSource,
		"self_managed":  tagentevent.SelfManagedLineage(evt.Metadata[tagentevent.MetaKeyTriggerSource]),
		"deliverable":   tagentevent.DeliverableLineage(evt.Metadata[tagentevent.MetaKeyTriggerSource]),
		"anchor_source": "context_manager.buildBusFact",
	}
	raw, err := json.MarshalIndent(sample, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(runDir, "trigger-source-sample.json")
	require.NoErrorf(t, os.WriteFile(path, raw, 0o600), "落盘样本必须写得出 %s", path)
	return path
}

// TestRealModel_ExternalMeditation 真实模型下的外部化冥想整链验收：
// - 目标的一次真实回合把用户事实写进共享事实链，入库时盖上的 trigger_source 就是 novelty 判据读到的那枚章；
// - 外部形态的冥想 agent 在同一条链上开门，一次真实策展回合的卡片落在它自己的分区，被观察分区零写入也不产生压缩事件；
// - 卡片经组合根投递面进入目标的下一个真实回合，目标侧请求文本里同时出现卡片正文与投递来源头；
// - 全链恰好三次真实调用、调用者各归其位，预算之外一次也不许多花；链条跑完后没有任何一轮靠自管产出续命；
// - 端点不可路由的同构探针必须交出「零产出」的证据，主跑的判定才不是自证。
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func TestRealModel_ExternalMeditation(t *testing.T) {
	creds := extMedGate(t)
	runDir := realAcceptRunDir(t)

	main := newExtMedScenario(t, creds, filepath.Join(runDir, "external-meditation"))
	ev := main.drive(realExtMedChainWait)
	require.Emptyf(t, ev.missingEvidence(), "真实模型下的外部化冥想必须逐环交出证据：%v", ev.missingEvidence())

	require.Equalf(t, tagentevent.TypeExternalInput, ev.userFact.EventType,
		"盖 user 归因的那条记录必须是输入事件本身：%+v", ev.userFact)
	require.Zerof(t, ev.targetNewWrites, "策展回合不得往被观察分区写任何东西（新增 %d 条）", ev.targetNewWrites)
	require.Zerof(t, ev.targetCompress, "外部化冥想不持有目标的压缩权（新增 %d 条压缩事件）", ev.targetCompress)
	require.Containsf(t, ev.card, extMedCardToken, "真实策展回合要交出提示词规定的那行卡片：%q", ev.card)

	labels := map[string]int{}
	for _, label := range ev.served {
		labels[label]++
	}
	require.Equalf(t, map[string]int{extMedTargetCall: 2, extMedCurationCall: 1}, labels,
		"三段链条的每一次真实调用都要归到发起它的那个 agent：%v", ev.served)
	for _, call := range main.ledger.since(0) {
		require.LessOrEqualf(t, call.CompletionTokens, realAcceptMaxOutputTokens,
			"单次输出上界 %d 被击穿：%+v", realAcceptMaxOutputTokens, call)
		require.LessOrEqualf(t, call.PromptTokens, realAcceptMaxRequestTokens,
			"单请求估计上界 %d 被击穿：%+v", realAcceptMaxRequestTokens, call)
	}
	main.ledger.report(t, creds.ModelName)

	baseline := main.ledger.count()
	require.Neverf(t, func() bool { return main.ledger.count() > baseline }, 900*time.Millisecond, 50*time.Millisecond,
		"自管产出循环之后任何一次多出来的真实调用都是永动机")
	require.NoErrorf(t, main.stop(), "主跑的循环必须干净收尾")

	samplePath := extMedTriggerSourceSample(t, runDir, *ev.userFact)
	t.Logf("extmed trigger_source sample: %s=%q card_partition=%d",
		tagentevent.MetaKeyTriggerSource, ev.userFactLineage, ev.cardPartition)
	t.Logf("extmed evidence: sample=%s card=%q target_reply=%q final_reply=%q lineages=%v",
		samplePath, ev.card, ev.targetReply, ev.targetFinalReply, ev.lineages)

	probeCreds := creds
	probeCreds.Endpoint = extMedUnroutableEndpoint
	probe := newExtMedScenario(t, probeCreds, filepath.Join(runDir, "unroutable-probe"))
	pev := probe.drive(realExtMedProbeWait)
	require.NotEmptyf(t, pev.missingEvidence(),
		"端点不可路由也能交出完整证据，说明这套判定没有在自证：%v", pev.missingEvidence())
	require.Zerof(t, pev.servedTokens, "不可路由的端点上一次调用也不该有 token 入账：%+v", pev.served)
	t.Logf("extmed unroutable probe refused the chain with %d gap(s), calls=%d",
		len(pev.missingEvidence()), len(pev.served))
}
