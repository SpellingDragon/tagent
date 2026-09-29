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

// Run 实现 agent.Agent 接口。
//
// 在事件驱动架构下，Run 是子 agent 调用路径（本地子调用由 AgentToolWrapper 使用，
// 远程调用由 A2A 使用）；顶层使用必须走 StartLoop/InjectMessage/StopLoop。
//
// Run 为本次调用新建 EventBus + AgentLoop，把初始消息作为 external_input 发布，并返回
// AgentLoop 的 outputCh；调用方持续读事件直到通道关闭（上下文取消，或产出 agent_output）。
//
// 上下文可经两条入口到达，且都在本次调用本地装配，绝不经过共享的 `ta` 状态——隐式的
// activeBus/pendingExternalEvents 传递已取消，因此并发 Run 无法互相注入：
//  1. RuntimeState 路径（远端/包装器，即 A2A 兼容那条）：
//     inv.RunOptions.RuntimeState["external_context"] 内是序列化的 ExternalContextEntry JSON；
//  2. direct 兼容入口：事件先交给 IngestExternalEvents，在 Run 进入时原子排空进本次调用，
//     保持单槽交收语义。
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
	if cl, hasLease := execLeaseFromContext(ctx); hasLease && cl != nil && cl.belongsToOwnerOf(ta.contextManager) {
		if gen := cl.declaredRunConfig(); gen != nil {
			invCfg = *gen
		}
	}
	invCfg.MaxToolIterations = maxToolIters
	if invCfg.Name == "" {
		invCfg.Name = ta.name
	}
	invCM := newContextManagerFromConfig(&invCfg, ta, ta.memPlugin, ta.sessionSvc, invBus, invOutputCh, invProjection, invOnEvent)
	ta.registerLiveCM(invCM)
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
		return nil, fmt.Errorf("subagent %q invocation refused: %w", ta.name, err)
	}

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
