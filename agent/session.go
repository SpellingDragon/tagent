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

// Run implements agent.Agent interface.
//
// In the event-driven architecture, Run is the sub-agent invocation path
// (used by AgentToolWrapper for local sub-agent calls and A2A for remote calls).
// Top-level usage must use StartLoop/InjectMessage/StopLoop instead.
//
// Run creates a fresh EventBus + AgentLoop for this invocation, publishes
// the initial message as external_input, and returns the AgentLoop's
// outputCh. The caller reads events until the channel closes (context
// cancelled or agent_output produced).
//
// Context can arrive via two paths, BOTH assembled into this call's message
// locally (never through shared `ta` state — §7.1 D2 removes the implicit
// activeBus/pendingExternalEvents passing so concurrent Runs cannot cross-
// inject):
//  1. RuntimeState path (remote/wrapper): inv.RunOptions.RuntimeState["external_context"]
//     contains serialized ExternalContextEntry JSON. This is the A2A-compatible path.
//  2. Legacy direct API: events handed to IngestExternalEvents, drained atomically
//     into this call at Run entry (single-handoff semantics preserved).
func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	// Per-call session context, derived from the Invocation. These stay LOCAL:
	// a sub-call must NOT stomp the shared ta.lastUserID/lastSessionID (family-3
	// 去共享写) — concurrent Runs, or a Run while the entry owner's StartLoop is
	// live, would otherwise clobber the owner's idle session context. The private
	// invocation CM below is given this call's context directly (not via shared ta state).
	userID := "tagent-user"
	sessionID := fmt.Sprintf("tagent-session-%s", inv.InvocationID)

	message := inv.Message
	// Normalize only a genuinely empty message: an image/file-only input has empty
	// Content but valid ContentParts, which MUST be preserved (a media-only
	// delegation is legitimate input — replacing it with NewUserMessage("") would
	// drop the parts before the event is even built).
	if message.Content == "" && len(message.ContentParts) == 0 {
		message = model.NewUserMessage("")
	}

	// External context is assembled for THIS invocation only and never stashed on
	// shared `ta` state (§7.1 D2 去隐式传参): two concurrent Run calls on one
	// instance must not cross-inject. It reaches this call through either the
	// RuntimeState path (remote/wrapper — the path AgentToolWrapper actually uses)
	// or the legacy direct Ingest API, whose single-handoff semantics are preserved
	// by draining it atomically into the same per-call slice.
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

	// Reject a delegation that still has neither content nor media parts after
	// external-context assembly. Because B-2 routes Run through the shared
	// processTurn — whose durable batch semantics SKIP an empty invocation rather
	// than call the model — an empty input would otherwise close the caller's
	// channel with zero events and no explanation, silently breaking the
	// request/response "one input, one turn, one result" contract. Fail explicitly
	// at this boundary adapter instead (before any CM/lease/bus is created).
	if message.Content == "" && len(message.ContentParts) == 0 {
		return nil, fmt.Errorf("agent %q: delegation input is empty (no content and no media parts)", ta.name)
	}

	// Validate required fields before creating sub-agent AgentLoop.
	if ta.config == nil || ta.config.Model == nil {
		return nil, fmt.Errorf("agent %q: config or model is nil", ta.name)
	}

	// Create a fresh EventBus + AgentLoop for this invocation.
	// Each sub-agent invocation gets its own isolated bus and compressor
	// (compress.SmartCompressor has mutable state and must not be shared across
	// concurrent goroutines). The bus rides the private CM (cm.bus) — it is NOT
	// installed as the shared `ta.activeBus` (§7.1 D2: Run must not rewrite the
	// callee's shared active bus, which a concurrent Run would then observe).
	invBus := NewEventBus()

	invOutputCh := make(chan *event.Event, 100)
	invProjection := compress.NewSessionProjection()
	invOnEvent := ta.makeOnEventCallback()
	maxToolIters := ta.config.MaxToolIterations
	if maxToolIters <= 0 {
		maxToolIters = DefaultSubAgentMaxToolIterations
	}
	// 3.2 trunk: a DECLARED invocation — the wrapper resolved this call on the
	// generation ITS face declared and armed the lease below — assembles the
	// per-call context manager from THAT generation's assembled config, never
	// from the owner's construction-time config (which every later publish
	// leaves stale; this is precisely what the transitional shell used to paper
	// over by carrying a whole duplicate agent). Any other inherited lease (a
	// caller's turn pin, tests) or a fresh external Run keeps the legacy source.
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
	// §6.4/D4：以 owner 的有效热参快照播种私有 compressor（新调用初始即有效），
	// 并注册进存活集合（在途调用随下一次压缩/预算边界取热更新值）。注销与
	// invCM.Close 同 defer——注册面严格随调用生命周期回收，绝不留历史列表。
	ta.registerLiveCM(invCM)
	invCM.SetUserIDSessionID(userID, sessionID)
	// §7.2/D3.3 (本 agent 任务域闭环): the private invocation CM MUST carry THIS
	// agent's own taskController. Without it, RunFlow's spawner injection
	// (context_manager) is skipped and whatever spawner the CALLER left on the
	// context leaks through — so a task the callee spawns would register in the
	// PARENT's task domain ("父 spawner 遮蔽"), silently degrading the callee to the
	// caller's manager. Wiring B's own manager makes the flow re-inject B's
	// spawner, which overrides the inherited one for the whole call subtree: a
	// derived call inherits the caller's version/source, never its task manager.
	// B's taskManager is a per-agent singleton (agent.go New), so concurrent Runs
	// correctly share B's one domain; settles route to B's own bus. Guard the
	// concrete pointer before boxing: assigning a nil *TaskManager into the
	// TaskController interface yields a typed-nil interface that slips past the
	// `== nil` guards (injectLiveTaskBoard) and panics on List().
	if ta.taskManager != nil {
		invCM.taskController = ta.taskManager
	}
	// S3m-b: the private CM reaches the owner's settle-routing table so a
	// delegation turn's background spawns are booked (countingSpawner) and their
	//越窗 settles route back to THIS call's sink instead of the shared bus.
	invCM.settleSinks = ta.settleSinks

	// §3.2/§4.1 (D5): this invocation is a DERIVED execution. It takes an extra
	// reference on the generation its caller is running under — before any work
	// starts — so the pinned generation cannot be reclaimed while this call (or
	// anything below it) is live, and a mid-call publication cannot split the
	// call tree across generations. An external Run with no inherited lease is the
	// other D5 row: it acquires the generation now in force. Either way the
	// reference is released only at the invocation tail, after the private
	// executor is closed — RunFlow's return plus the fork's producer-done
	// credential is what makes "tail" mean "the producer actually stopped".
	callerLease, inherited := execLeaseFromContext(ctx)
	var invLease *ExecLease
	switch {
	case inherited:
		invLease = callerLease.Derive(LeaseSubCall)
	case ta.contextManager != nil:
		invLease = ta.contextManager.AcquireLease(LeaseSubCall)
	}
	if err := invLease.Err(); err != nil {
		// A stale wrapper may still name an owner whose generation converged shut. The
		// gate declines the reference; the invocation must fail here rather than publish
		// input into a dead pipeline (§3.2「关闭后的 … Inject … 被拒」).
		return nil, fmt.Errorf("subagent %q invocation refused: %w", ta.name, err)
	}

	// Sub-agent invocation semantics: a tool call is request-response — one input,
	// one result. §7.1 D2 / B-2 + S3m-c.2: the delegation input enters through the
	// SAME pipeline the persistent loop consumes — it is PUBLISHED to this call's
	// own bus and drained by the shared shell's first iteration (processTurn), NOT
	// run on a direct fast-path call. So "作为被调方" and "直连宿主" share one
	// transport, one consume loop, and one turn primitive; the first answer and every
	//越窗 continuation are just successive iterations of ONE loop. The message
	// (external context already applied above, per-call) is carried as a single
	// volatile external_input event; the shell assembles it via BuildInvocation.
	//
	// The batch carries no durable claim, so processTurn's submit/finish gate commits
	// and acks nothing (interpretation A: a derived sub-call is never a durable
	// envelope) — yet it still binds projection / trigger source / metadata and runs
	// RunFlow under the CALLER-PINNED lease (invLease), exactly like a top-level turn.
	// invLease releases idempotently at endTurn; the tail defer below is the safety
	// net for the pre-lease early returns (empty batch).
	inputEvent := newDelegationEvent(inv, message)
	go func() {
		defer invLease.Release()         // §4.1: FIRST defer = LAST release; idempotent with processTurn's endTurn
		defer close(invOutputCh)         // signals turn end to the caller — ONLY after the tail, so越窗 continuations still reach it
		defer ta.unregisterLiveCM(invCM) // §6.4: registration bound to the call, not a history list
		defer invCM.Close()              // release temporary Runner resources
		// S3m-c (I-1): bind this call's OWN bus to its delegation id BEFORE any turn
		// spawns, and unbind FIRST on return (LIFO, above the CM/ch defers) so a late
		// settle after we stop consuming falls back to the shared bus rather than a
		// dead binding.
		invID := ""
		if inv != nil {
			invID = inv.InvocationID
		}
		ta.bindSettleBus(invID, invBus)
		defer ta.unbindSettleBus(invID)
		// S3m-c.2 (I-3 字面化): the input is published to invBus and consumed by the
		// shared shell's FIRST iteration — no direct processTurn fast path. firstCtx
		// carries the invocation id so the shell holds the delegation's identity for
		// EVERY turn it runs (a越窗 continuation batch carries no extractable id after
		// the W-2 gate, so re-deriving it from events would lose attribution on a
		// continuation turn's own spawns). The shell quiesces when the delivery-
		// accounting barrier drains: for a request/response sub-call that spawns no
		// background task, pending stays 0 → after the first turn the shell's TryPull
		// finds the bus empty → the channel closes exactly when the single turn ends
		// (byte-for-byte the pre-S3m behavior).
		firstCtx := withInvocationID(ctx, invID)
		invBus.Publish(inputEvent)
		ta.runAgentLoop(firstCtx, invBus, invCM, loopSpec{invocationID: invID})
	}()

	return invOutputCh, nil
}

// newDelegationEvent wraps a request/response sub-call's assembled message as a
// single volatile external_input event (§7.1 D2 / B-2) and stamps the correlation
// handle — this invocation's ID — as CONTROL metadata (§7.3 D4, S1 groundwork).
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

// RunSimple is removed. Top-level usage must use StartLoop/InjectMessage/StopLoop.
// Sub-agent invocation goes through agent.Run() via AgentToolWrapper.Call().

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
// It is a pure DELIVERY-side callback (unified-event-projection D1): projection
// writes happen in the event-plugin pipeline (MemoryPlugin → ProjectionSink),
// not here. This callback only:
// 1. Propagates currentMetadata from ContextManager to event.StateDelta ("meta_" prefix)
// 2. Marks meditation final outputs for ★-highlighted compaction cards
//
// Meditation gating is intentionally ABSENT here (meditation-gate-split):
// the idle anchor is updated by runEventLoop at turn end (lineage-agnostic),
// and the novelty anchor at the injection points (input-side) — no
// output-side lineage filtering is needed anymore.
func (ta *TagentAgent) makeOnEventCallback() func(evt *event.Event) {
	return func(evt *event.Event) {
		if evt == nil {
			return
		}
		// Propagate metadata from ContextManager to event.StateDelta
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

		// Meditation outputs become ★-highlighted index cards when their
		// events are later compacted (reflection anchors in long-term memory).
		if ta.contextManager != nil && isFinalResponse(evt) &&
			string(evt.StateDelta[tagentevent.MetaKeyTriggerSource]) == "meditation" {
			meta := tagentevent.ParseEventMeta(evt)
			if meta.EventKey != 0 && ta.contextManager.contextCompressor != nil {
				ta.contextManager.contextCompressor.MarkMeditationKey(meta.EventKey)
			}
		}
	}
}
