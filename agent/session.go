// 契约: docs/wiki/agent/agent-architecture.md#core-components
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Run 实现 agent.Agent 接口：这是子 agent 调用路径（本地由 AgentToolWrapper、远程由 A2A 使用），
// 顶层使用必须走 StartLoop/InjectMessage/StopLoop。
//
// - 为本次调用新建 EventBus + AgentLoop，把初始消息作为 external_input 发布，返回 AgentLoop 的 outputCh；调用方读事件直到通道关闭（上下文取消或产出 agent_output）。
// - 上下文只在本次调用本地装配，绝不经过共享的 ta 状态，因此并发 Run 无法互相注入；入口有二：RuntimeState 携带序列化的 ExternalContextEntry JSON，或 direct 兼容入口经 IngestExternalEvents 在 Run 进入时原子排空以保持单槽交收语义。
// - per-call 视图覆盖同属这一族调用期输入：经 RuntimeState 随 invocation 到达，只在装配期改写本次调用的提示词/模型/工具面，随调用结束而失效，不写回常驻定义也不落任何共享代际面。
// - 租约拒绝发生在本调用计为 live 之前：私有 CM 直接 Close 且不注册，否则清理 goroutine 永不运行，LiveCMCount 不归零、owner Obligations 到不了零、退役排空挂死。
// - 终态 drain 的 defer 绑在 unbind 之前（LIFO 下后跑），把 loop-exit 到 unbind 窗口内落地的 settle 转发到共享总线。
func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	userID := "tagent-user"
	sessionID := fmt.Sprintf("tagent-session-%s", inv.InvocationID)

	message := inv.Message
	if message.Content == "" && len(message.ContentParts) == 0 {
		message = model.NewUserMessage("")
	}

	// 外部上下文只为这一次调用装配，绝不暂存到共享的 `ta` 状态上：同一实例上的两次并发
	// Run 不得互相注入。它经两条入口之一到达本次调用——RuntimeState 路径（远端/包装器，
	// 即 AgentToolWrapper 实际所走的那条），或 direct 兼容入口；后者的单槽交收语义靠「原子排空进同一个逐调用切片」来保持。
	var externalEvents []memory.FullEvent
	if inv.RunOptions.RuntimeState != nil {
		if raw, ok := inv.RunOptions.RuntimeState[ExternalContextKey]; ok {
			var data []byte
			switch v := raw.(type) {
			case json.RawMessage:
				data = v
			case []byte:
				data = v
			case string:
				data = []byte(v)
			}
			if len(data) > 0 {
				events, err := deserializeExternalContext(data)
				if err != nil {
					log.Warnf("[Run] failed to deserialize external context: %v", err)
				} else {
					externalEvents = append(externalEvents, events...)
				}
			}
		}
	}
	if legacy := ta.drainPendingExternalEvents(); len(legacy) > 0 {
		externalEvents = append(externalEvents, legacy...)
	}
	if len(externalEvents) > 0 {
		message = applyExternalContext(message, externalEvents)
	}

	if message.Content == "" && len(message.ContentParts) == 0 {
		return nil, fmt.Errorf("agent %q: delegation input is empty (no content and no media parts)", ta.name)
	}

	if ta.config == nil || ta.config.Model == nil {
		return nil, fmt.Errorf("agent %q: config or model is nil", ta.name)
	}

	invBus := NewEventBus()

	invOutputCh := make(chan *event.Event, 100)
	invProjection := compress.NewSessionProjection()
	invOnEvent := ta.makeOnEventCallback()
	maxToolIters := ta.config.MaxToolIterations
	if maxToolIters <= 0 {
		maxToolIters = DefaultSubAgentMaxToolIterations
	}
	invCfg := *ta.config
	// O2a 变参接缝：模型引用必须解析在「本次调用被选中时的那一代」冻结的快照上，而不是
	// 此刻恰好更新的活注册表。没有租约/不属主/该代未发布快照时保持 nil，语义与两参调用
	// 逐字节相同（回落到此处正在装配的视图）。
	var pinnedRefs *ModelRefSnapshot
	if cl, hasLease := execLeaseFromContext(ctx); hasLease && cl != nil && cl.belongsToOwnerOf(ta.contextManager) {
		if gen := cl.declaredRunConfig(); gen != nil {
			invCfg = *gen
		}
		pinnedRefs = cl.ModelReferences()
	}
	invCfg.MaxToolIterations = maxToolIters
	if invCfg.Name == "" {
		invCfg.Name = ta.name
	}
	overrides, ovErr := perCallOverridesFromInvocation(inv)
	if ovErr != nil {
		return nil, fmt.Errorf("agent %q: %w", ta.name, ovErr)
	}
	if overrides != nil {
		if err := applyPerCallOverrides(&invCfg, overrides, pinnedRefs); err != nil {
			return nil, fmt.Errorf("agent %q: per-call override refused at assembly: %w", ta.name, err)
		}
	}
	invCM := newContextManagerFromConfig(&invCfg, ta, ta.memPlugin, ta.sessionSvc, invBus, invOutputCh, invProjection, invOnEvent)
	invCM.SetUserIDSessionID(userID, sessionID)
	if ta.taskManager != nil {
		invCM.taskController = ta.taskManager
	}
	invCM.settleSinks = ta.settleSinks

	callerLease, inherited := execLeaseFromContext(ctx)
	var invLease *ExecLease
	switch {
	case inherited:
		invLease = callerLease.Derive(LeaseSubCall)
	case ta.contextManager != nil:
		invLease = ta.contextManager.AcquireLease(LeaseSubCall)
	}
	if err := invLease.Err(); err != nil {
		invCM.Close()
		return nil, fmt.Errorf("subagent %q invocation refused: %w", ta.name, err)
	}
	ta.registerLiveCM(invCM)

	inputEvent := newDelegationEvent(inv, message)
	go func() {
		defer invLease.Release()
		defer close(invOutputCh)
		defer ta.unregisterLiveCM(invCM)
		defer invCM.Close()
		invID := ""
		if inv != nil {
			invID = inv.InvocationID
		}
		ta.bindSettleBus(invID, invBus)
		defer drainSettleBusTo(invBus, ta.persistentBus)
		defer ta.unbindSettleBus(invID)
		firstCtx := withInvocationID(ctx, invID)
		invBus.Publish(inputEvent)
		ta.runAgentLoop(firstCtx, invBus, invCM, loopSpec{invocationID: invID})
	}()

	return invOutputCh, nil
}

// newDelegationEvent wraps a request/response sub-call's assembled message as a
// single volatile external_input event and stamps the correlation
// handle — this invocation's ID — as CONTROL metadata.
// The handle lets a resident owner route a background follow-up back to the right
// waiting request later; because it is a controlMetaKeys entry it never reaches
// model-visible text nor is forwarded as user meta_* the model could spoof. The
// message carries no durable claim, so the event stays volatile (interpretation A:
// a derived sub-call is never a durable envelope).
func newDelegationEvent(inv *agent.Invocation, message model.Message) *AgentEvent {
	evt := NewExternalInputEvent("user", message)
	if inv != nil && inv.InvocationID != "" {
		evt.Metadata[metaKeyInvocationID] = inv.InvocationID
	}
	return evt
}

// Tools implements agent.Agent interface.
func (ta *TagentAgent) Tools() []tool.Tool {
	if ta.config != nil {
		return ta.config.Tools
	}
	return nil
}

// Info implements agent.Agent interface.
func (ta *TagentAgent) Info() agent.Info {
	return agent.Info{
		Name:        ta.name,
		Description: ta.description,
	}
}

// SubAgents implements agent.Agent interface.
// In the event-driven architecture, sub-agents are managed via AgentToolWrapper,
// not via the framework's sub-agent mechanism.
func (ta *TagentAgent) SubAgents() []agent.Agent {
	return nil
}

// FindSubAgent implements agent.Agent interface.
func (ta *TagentAgent) FindSubAgent(name string) agent.Agent {
	return nil
}

// setActiveBus sets the current active bus for event injection.
// Called by StartLoop (sets persistentBus) and Run() (sets invBus).
func (ta *TagentAgent) setActiveBus(bus *EventBus) {
	ta.activeBusMu.Lock()
	ta.activeBus = bus
	ta.activeBusMu.Unlock()
}

// restorePersistentBus switches the active bus back to the persistent bus.
// Called when a sub-agent Run() completes so InjectMessage resumes routing
// to the persistent AgentLoop (if active).
func (ta *TagentAgent) restorePersistentBus() {
	ta.setActiveBus(ta.persistentBus)
}

// setSessionContext sets the userID and sessionID (thread-safe).
func (ta *TagentAgent) setSessionContext(userID, sessionID string) {
	ta.sessionMu.Lock()
	defer ta.sessionMu.Unlock()
	ta.lastUserID = userID
	ta.lastSessionID = sessionID
}

// getOrCreateSession returns the session for the given sessionID (or the
// last-known sessionID if empty). Creates the session if it does not exist.
func (ta *TagentAgent) getOrCreateSession(sessionID ...string) *session.Session {
	if ta.sessionSvc == nil {
		return nil
	}
	sid := ta.lastSessionID
	if len(sessionID) > 0 && sessionID[0] != "" {
		sid = sessionID[0]
	}
	key := session.Key{
		AppName:   ta.name,
		UserID:    ta.lastUserID,
		SessionID: sid,
	}
	ctx := context.Background()
	sess, err := ta.sessionSvc.GetSession(ctx, key)
	if err != nil || sess == nil {
		sess, err = ta.sessionSvc.CreateSession(ctx, key, nil)
		if err != nil {
			log.Errorf("[getOrCreateSession] CreateSession failed: %v", err)
			return nil
		}
	}
	return sess
}

// makeOnEventCallback creates the onEvent callback for StartLoop and Run().
// It is a pure DELIVERY-side callback: projection
// writes happen in the event-plugin pipeline (MemoryPlugin → ProjectionSink),
// not here. This callback only:
// 1. Propagates currentMetadata from ContextManager to event.StateDelta ("meta_" prefix)
// 2. Marks meditation final outputs for ★-highlighted compaction cards
//
// Meditation gating is intentionally ABSENT here:
// the idle anchor is updated by runEventLoop at turn end (lineage-agnostic),
// and the novelty anchor at the injection points (input-side) — no
// output-side lineage filtering is needed anymore.
func (ta *TagentAgent) makeOnEventCallback() func(evt *event.Event) {
	return func(evt *event.Event) {
		if evt == nil {
			return
		}
		if ta.contextManager != nil {
			md := ta.contextManager.GetInvocationMetadata()
			if len(md) > 0 {
				if evt.StateDelta == nil {
					evt.StateDelta = make(map[string][]byte)
				}
				for k, v := range md {
					key := k
					if !strings.HasPrefix(key, tagentevent.MetaPrefix) {
						key = tagentevent.MetaPrefix + key
					}
					evt.StateDelta[key] = []byte(v)
				}
			}
		}

		if ta.contextManager != nil && isFinalResponse(evt) &&
			string(evt.StateDelta[tagentevent.MetaKeyTriggerSource]) == "meditation" {
			meta := tagentevent.ParseEventMeta(evt)
			if meta.EventKey != 0 && ta.contextManager.contextCompressor != nil {
				ta.contextManager.contextCompressor.MarkMeditationKey(meta.EventKey)
			}
		}
	}
}
