package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
// Context can arrive via two paths:
//  1. RuntimeState path (remote/wrapper): inv.RunOptions.RuntimeState["external_context"]
//     contains serialized ExternalContextEntry JSON. This is the A2A-compatible path.
//  2. Struct field path (direct API): pendingExternalEvents set via IngestExternalEvents.
func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	// Extract or generate userID and sessionID from Invocation.
	userID := "tagent-user"
	sessionID := fmt.Sprintf("tagent-session-%s", inv.InvocationID)

	// Store session context for event injection.
	ta.setSessionContext(userID, sessionID)

	// Path 1: Read external context from RuntimeState (remote/wrapper path).
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
				} else if len(events) > 0 {
					ta.IngestExternalEvents(events)
				}
			}
		}
	}

	message := inv.Message
	if message.Content == "" {
		message = model.NewUserMessage("")
	}

	// Path 2: Prepend external event context if pending.
	if len(ta.pendingExternalEvents) > 0 {
		message = ta.injectExternalContext(message)
	}

	// Validate required fields before creating sub-agent AgentLoop.
	if ta.config == nil || ta.config.Model == nil {
		return nil, fmt.Errorf("agent %q: config or model is nil", ta.name)
	}

	// Create a fresh EventBus + AgentLoop for this invocation.
	// Each sub-agent invocation gets its own isolated bus and compressor
	// (SmartCompressor has mutable state and must not be shared across
	// concurrent goroutines).
	invBus := NewEventBus()
	ta.setActiveBus(invBus)

	invOutputCh := make(chan *event.Event, 100)
	invProjection := NewSessionProjection()
	invOnEvent := ta.makeOnEventCallback(sessionID, invProjection)
	maxToolIters := ta.config.MaxToolIterations
	if maxToolIters <= 0 {
		maxToolIters = DefaultSubAgentMaxToolIterations
	}
	invCfg := *ta.config
	invCfg.MaxToolIterations = maxToolIters
	if invCfg.Name == "" {
		invCfg.Name = ta.name
	}
	invCM := newContextManagerFromConfig(&invCfg, ta.memPlugin, ta.sessionSvc, invBus, invOutputCh, invProjection, invOnEvent)
	invCM.SetUserIDSessionID(ta.lastUserID, sessionID)

	// Sub-agent invocation semantics: a tool call is request-response — one
	// input, one turn, one result. RunFlow runs exactly one complete turn
	// (input → full ReAct tool loop → final response) and returns when the
	// turn is done. The turn boundary is RunFlow's natural return, NOT
	// event-stream inspection. Events flow to invOutputCh via RunFlow's
	// forwarding; the caller reads until invOutputCh closes.
	//
	// This shares the same turn primitive (RunFlow) as the persistent loop
	// (runEventLoop); the only difference is the persistent loop wraps it in
	// `for { Pull; RunFlow }` while a sub-agent calls it exactly once.
	invCM.SetTriggerSource("user")

	// Persist the driving request into the projection BEFORE RunFlow so it
	// becomes the first entry in the timeline (right after system), matching
	// the persistent-loop ordering. Without this the request stays as the
	// framework's unprefixed invocation seed and extractCurrentTurnMessages
	// appends it AFTER the accumulating ReAct history — burying it at the end.
	// filterUser then drops the framework's re-inserted unprefixed duplicate.
	if message.Content != "" {
		invCM.persistBusEvent(NewExternalInputEvent("user", message))
	}
	go func() {
		defer close(invOutputCh)        // signals turn end to the caller
		defer invCM.Close()             // release temporary Runner resources
		defer ta.restorePersistentBus() // restore activeBus to persistentBus
		if err := invCM.RunFlow(ctx, message); err != nil {
			log.Errorf("[Run] sub-agent %q RunFlow failed: %v", ta.name, err)
		}
	}()

	return invOutputCh, nil
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
// It performs two tasks:
// 1. Append EventReference to the projection
// 2. Propagate currentMetadata from ContextManager to event.StateDelta with "meta_" prefix
func (ta *TagentAgent) makeOnEventCallback(sessionID string, projection *SessionProjection) func(evt *event.Event) {
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
					if !strings.HasPrefix(key, "meta_") {
						key = "meta_" + key
					}
					evt.StateDelta[key] = []byte(v)
				}
			}
		}
		// Append to projection
		if projection != nil {
			if ref, ok := BuildEventReference(evt); ok {
				// Source-tracing (conversation-self-heal Phase 0.2): log the
				// framework event ID alongside the event_key at the persistence
				// boundary. If a duplicate event_key ever recurs, correlating
				// evt.ID discriminates the root cause: same evt.ID = framework
				// re-delivered one event on eventCh; different evt.ID + same key
				// = MemoryPlugin assigned a colliding key to two distinct events.
				log.Debugf("[onEvent] persist: evt_id=%s key=%d role=%s type=%s",
					evt.ID, ref.EventKey, ref.Role, ref.EventType)
				projection.Append(ref)
			}
		}

		// Anchor meditation idle-detection on actual agent OUTPUT: only a final
		// agent response counts as activity. "Idle" then means "no agent output
		// for MinGap", so meditation never fires while the agent is actively
		// producing turns (e.g. reclaiming background task_settled events), and
		// is not spuriously reset by injected inputs.
		if ta.meditationMgr != nil && isFinalResponse(evt) {
			ta.meditationMgr.UpdateLastEventTime(time.Now())
		}
	}
}
