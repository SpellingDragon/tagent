package plugin

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/plugin"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// MemoryPlugin 把框架事件管线的输出同步写入 MemoryStore，并在同一同步点投影到本调用的
// ProjectionSink。它按顺序跳过无载荷屏障事件、流式分片、退化空终态与本次尝试的精确输入回显；
// 因果父子关系按 (partition, session) 独立维护并有上界，经 RelationStore 承载，同一因果键的
// 「读父 → 分配 → 提交 → 关系 → 游标」在同一条键锁段内串行，不同因果键互不阻塞。
// 存储标识与归因随事件写回 StateDelta 与 FullEvent.Metadata：票据、投影与因果游标只在
// StoreEvent 成功之后发布，写失败或未接存储都不向下游提供取不回的持久票据。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#memory-plugin
type MemoryPlugin struct {
	memStore      memory.MemoryStore
	mu            sync.Mutex
	lastEventKeys map[string]int64
	causalGuards  map[string]*causalGuard
	// callIDResolver is the optional exact-key seam of D14-S2; nil means the
	// feature is off and the plugin behaves byte-for-byte as before it existed.
	callIDResolver CallIDResolver
}

// CallIDResolver resolves one model-call identity (the SDK response id carried
// by the event) to the call_id captured for it. Hit or no hit is decided by the
// installer's scope lookup: false means "unbound", and the writer must then
// stamp nothing. Taking the nearest call would attach feedback to a call that
// never produced it. The composition root injects this (agent bridges
// rl.CallIDForResponse) so rl stays a leaf and never imports plugin.
type CallIDResolver func(ctx context.Context, responseID string) (string, bool)

// MemoryPluginOption configures an optional seam of NewMemoryPlugin. Without
// options the constructor keeps its pre-seam shape: variadic, so every existing
// call site compiles and behaves exactly as before.
type MemoryPluginOption func(*MemoryPlugin)

// WithCallIDResolver installs the exact-key lookup that stamps
// tagentevent.MetaKeyCallID onto a committed fact. A nil resolver means off.
func WithCallIDResolver(r CallIDResolver) MemoryPluginOption {
	return func(p *MemoryPlugin) {
		p.callIDResolver = r
	}
}

// causalGuard 把一个 (partition,session) 因果域的提交段串行化。refs 是「正在提交或排队等待」
// 的活跃引用计数，由 p.mu 保护：活跃键因此不会被因果游标的上界回收淘汰掉它唯一的锚。
type causalGuard struct {
	key  string
	mu   sync.Mutex
	refs int
}

// NewMemoryPlugin 创建一个把事件写入 store 并同步投影的插件；因果链状态初始为空。
// 选项是变参的：不传即维持接线前的形态。
func NewMemoryPlugin(store memory.MemoryStore, opts ...MemoryPluginOption) *MemoryPlugin {
	p := &MemoryPlugin{
		memStore:      store,
		lastEventKeys: make(map[string]int64),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	return p
}

// Name 返回插件名 memory。
func (p *MemoryPlugin) Name() string {
	return "memory"
}

// Register 把本插件挂到框架的 OnEvent 钩子。
func (p *MemoryPlugin) Register(r *plugin.Registry) {
	r.OnEvent(p.onEvent)
}

// OnEvent 是内部事件钩子的导出形式，供测试与工具直接调用；生产路径经 Register 注入。
func (p *MemoryPlugin) OnEvent(
	ctx context.Context,
	inv *agent.Invocation,
	evt *event.Event,
) (*event.Event, error) {
	return p.onEvent(ctx, inv, evt)
}

// onEvent 执行「筛选 → 分配 → 构造 → 持久化 → 投影 → 回写标识 → 更新因果链」的完整顺序；
// 四道跳过闸在任何 key 分配与写入之前完成，分配之后的整段在同因果键的锁内串行。
func (p *MemoryPlugin) onEvent(
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

	if evt.Response.IsPartial {
		return evt, nil
	}

	if m := evt.Response.Choices[0].Message; m.Content == "" && tagentevent.ExtractEventType(m) == tagentevent.TypeAgentOutput {
		log.Debugf("[Memory] skip degenerate empty final (agent_output, no content/tool_calls)")
		return evt, nil
	}

	if cred, ok := EchoCredentialFrom(ctx); ok && isExpectedInputEcho(inv, evt, cred) {
		cred.Bind(evt.InvocationID)
		log.Debugf("[Memory] expected input echo (attempt=%s invocation=%s) already committed by the event loop — skipping store",
			cred.AttemptToken, evt.InvocationID)
		return evt, nil
	}

	agentName := p.extractAgentName(inv)
	partitionID := memory.PartitionIDFromName(agentName)

	eventType, eventSummary := p.inferEventInfo(evt)

	sessionID := ""
	if inv != nil && inv.Session != nil {
		sessionID = inv.Session.ID
	}
	causalKey := fmt.Sprintf("%d:%s", partitionID, sessionID)

	guard := p.acquireCausal(causalKey)
	defer p.releaseCausal(guard)

	parentKey := p.parentOfCausal(causalKey)
	eventKey := memory.NewSnowflakeEventKey(partitionID, 0)

	timestamp := extractTimestamp(evt)

	fullEvent := memory.FullEvent{
		EventKey:     eventKey,
		PartitionID:  partitionID,
		EventType:    eventType,
		EventSummary: eventSummary,
		Timestamp:    timestamp,
	}
	fullEvent.Metadata = make(map[string]string, 2)
	if agentName != "" {
		fullEvent.Metadata[tagentevent.MetaKeyAgentName] = agentName
	}
	if attr, ok := AttributionFrom(ctx); ok {
		for k, v := range attr {
			fullEvent.Metadata[k] = v
		}
	}

	if evt.Response != nil && len(evt.Response.Choices) > 0 {
		msg := evt.Response.Choices[0].Message
		fullEvent.Content = sanitizeAssistantContent(msg)
		fullEvent.ContentParts = msg.ContentParts
		fullEvent.ToolCalls = msg.ToolCalls
		fullEvent.ToolID = msg.ToolID
		fullEvent.Response = evt.Response
	}

	if p.callIDResolver != nil && evt.Response != nil && evt.Response.ID != "" {
		if callID, ok := p.callIDResolver(ctx, evt.Response.ID); ok && callID != "" {
			fullEvent.Metadata[tagentevent.MetaKeyCallID] = callID
		}
	}

	stored := false
	if p.memStore != nil {
		if err := p.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			log.Errorf("[Memory] store failed key=%d partition=%d: %v", eventKey, partitionID, err)
		} else {
			stored = true
			if parentKey != 0 {
				if rsp, ok := p.memStore.(memory.RelationStoreProvider); ok {
					if err := rsp.RelationStore().SetParent(eventKey, parentKey); err != nil {
						log.Errorf("[Memory] set parent failed key=%d parent=%d: %v", eventKey, parentKey, err)
					}
				}
			}
			log.Debugf("[Memory] stored key=%d partition=%d type=%s summary_len=%d",
				eventKey, partitionID, eventType, len(eventSummary))
		}
	}
	if !stored && p.memStore != nil {
		if c, ok := EchoCredentialFrom(ctx); ok {
			c.MarkRejected("memory store error during credentialed turn")
		}
	}
	if stored {
		if sink, ok := ProjectionSinkFrom(ctx); ok {
			sink.Append(memory.EventReference{
				EventKey:     eventKey,
				PartitionID:  partitionID,
				EventType:    eventType,
				EventSummary: eventSummary,
				Timestamp:    timestamp,
				Role:         string(evt.Response.Choices[0].Message.Role),
			})
		}
	}

	if evt.StateDelta == nil {
		evt.StateDelta = make(map[string][]byte)
	}
	if stored {
		evt.StateDelta[tagentevent.MetaKeyEventKey] = []byte(tagentevent.FormatEventKey(eventKey))
		evt.StateDelta[tagentevent.MetaKeyPartitionID] = []byte(strconv.Itoa(partitionID))
	} else {
		delete(evt.StateDelta, tagentevent.MetaKeyEventKey)
		delete(evt.StateDelta, tagentevent.MetaKeyPartitionID)
		log.Debugf("[Memory] no persistent ticket published (event not committed) partition=%d type=%s",
			partitionID, eventType)
	}
	evt.StateDelta[tagentevent.MetaKeyEventType] = []byte(eventType)
	evt.StateDelta[tagentevent.MetaKeyEventSummary] = []byte(eventSummary)

	if stored {
		p.advanceCausal(causalKey, eventKey)
	}

	return evt, nil
}

// acquireCausal 取回该因果域的串行锁。锁序固定为「短 map 锁登记引用 → 放 map 锁 → 取键锁」，
// 反向（持 p.mu 去等键锁）禁止，因此 p.mu 是叶锁，绝不横跨存储 I/O。
func (p *MemoryPlugin) acquireCausal(causalKey string) *causalGuard {
	p.mu.Lock()
	if p.causalGuards == nil {
		p.causalGuards = make(map[string]*causalGuard)
	}
	g := p.causalGuards[causalKey]
	if g == nil {
		g = &causalGuard{key: causalKey}
		p.causalGuards[causalKey] = g
	}
	g.refs++
	p.mu.Unlock()

	g.mu.Lock()
	return g
}

// releaseCausal 交还键锁并撤掉活跃引用；引用归零即删除记录，长寿命 agent 不会累积因果键。
func (p *MemoryPlugin) releaseCausal(g *causalGuard) {
	g.mu.Unlock()

	p.mu.Lock()
	g.refs--
	if g.refs == 0 && p.causalGuards[g.key] == g {
		delete(p.causalGuards, g.key)
	}
	p.mu.Unlock()
}

// parentOfCausal 读该因果域最后已提交的键；键不存在（首次、重启或已淘汰）返回 0 表示无可信锚。
func (p *MemoryPlugin) parentOfCausal(causalKey string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastEventKeys[causalKey]
}

// advanceCausal 把游标推进到刚提交成功的键，并在超出上界时回收最久未更新的因果链。
func (p *MemoryPlugin) advanceCausal(causalKey string, eventKey int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastEventKeys[causalKey] = eventKey
	if len(p.lastEventKeys) > maxLastEventKeys {
		p.evictOldestLastEventKeysLocked()
	}
}

// maxLastEventKeys 是因果链 map 的上界：长寿命 agent 会持续累积 partition:session 键。
const maxLastEventKeys = 4096

// evictOldestLastEventKeysLocked 淘汰事件 key 最小（即最久未更新）的因果链直到回到上界；
// 调用方必须持有 p.mu。正在提交或排队等待的因果键被跳过——淘汰它等于抽掉一条在途链的锚。
// 每次都溢出为 O(n)，n 不超过上界。
func (p *MemoryPlugin) evictOldestLastEventKeysLocked() {
	for len(p.lastEventKeys) > maxLastEventKeys {
		oldestKey := ""
		var oldestVal int64
		first := true
		for k, v := range p.lastEventKeys {
			if g, ok := p.causalGuards[k]; ok && g.refs > 0 {
				continue
			}
			if first || v < oldestVal {
				oldestKey, oldestVal, first = k, v, false
			}
		}
		if oldestKey == "" {
			return
		}
		delete(p.lastEventKeys, oldestKey)
	}
}

var fakeEvtPrefixRe = regexp.MustCompile(`^(\[evt_-?(0[xX])?[0-9a-fA-F]+\|[a-z_]+\]\s*)+`)

// sanitizeAssistantContent 只剥 assistant 正文里模型编造的 [evt_...] 前缀；其他角色逐字存储。
func sanitizeAssistantContent(msg model.Message) string {
	if msg.Role != model.RoleAssistant || msg.Content == "" {
		return msg.Content
	}
	cleaned := fakeEvtPrefixRe.ReplaceAllString(msg.Content, "")
	if cleaned != msg.Content {
		log.Warnf("[Memory] stripped model-fabricated [evt_...] prefix from assistant output")
	}
	return cleaned
}

// extractAgentName 取 invocation 的 agent 名，缺失时回退 "unknown"（对应默认分区）。
func (p *MemoryPlugin) extractAgentName(inv *agent.Invocation) string {
	if inv == nil {
		return "unknown"
	}
	if inv.AgentName != "" {
		return inv.AgentName
	}
	return "unknown"
}

// isExpectedInputEcho 判定 (inv, evt) 是否本次尝试期望的合并输入回显：要求根调用、
// Author 为 user、消息角色为 user 且内容与凭据的合并输入在去空白后相等。
// 每个条件都经真实框架回显验证成立，故不会少跳（少跳会双写）；子调用与助手/工具事件不满足条件，
// 走正常存储路径。凭据或入参缺失时不算匹配。
func isExpectedInputEcho(inv *agent.Invocation, evt *event.Event, cred *EchoCredential) bool {
	if cred == nil || inv == nil || inv.GetParentInvocation() != nil {
		return false
	}
	if evt.Author != "user" || evt.Response == nil || len(evt.Response.Choices) == 0 {
		return false
	}
	m := evt.Response.Choices[0].Message
	if m.Role != model.RoleUser {
		return false
	}
	return normalizeEchoContent(m.Content) == normalizeEchoContent(cred.MergedMessage)
}

// normalizeEchoContent 去除首尾空白，合并体本身逐字比较。
func normalizeEchoContent(s string) string { return strings.TrimSpace(s) }

// extractTimestamp 返回事件的毫秒时间戳；nil 事件为 0。
func extractTimestamp(evt *event.Event) int64 {
	if evt == nil {
		return 0
	}
	return evt.Timestamp.UnixMilli()
}

// inferEventInfo 复用 tagent/event 的统一分类与摘要视图，与 SummaryPlugin 保持一致。
func (p *MemoryPlugin) inferEventInfo(evt *event.Event) (string, string) {
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return tagentevent.TypeExternalInput, ""
	}
	msg := evt.Response.Choices[0].Message
	eventType := tagentevent.ExtractEventType(msg)
	opts := tagentevent.DefaultOptionsForLLMContext()
	summary := tagentevent.GenerateEventSummary(msg, eventType, opts)
	return eventType, summary
}
