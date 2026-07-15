package agent

import (


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

// RunSimple is removed. Top-level usage must use StartLoop/InjectMessage/StopLoop.
// Sub-agent invocation goes through agent.Run() via AgentToolWrapper.Call().

// Tools implements agent.Agent interface.

// Info implements agent.Agent interface.

// SubAgents implements agent.Agent interface.
// In the event-driven architecture, sub-agents are managed via AgentToolWrapper,
// not via the framework's sub-agent mechanism.

// FindSubAgent implements agent.Agent interface.


// MemStore returns the MemoryStore for direct access (e.g., by RecallTool).

// Runner returns the underlying Runner from ContextManager.

// SetToolParentProjection wires the agent's SessionProjection to all
// AgentToolWrapper instances in the tool list. This enables auto-inject
// of event_keys when LLM does not pass them.
// Must be called after NewTagentAgent (which creates the projection).

// InjectMessage injects a user message into the agent's persistent EventBus.
// It is a convenience wrapper around InjectMessageWithSource with source="user".

// InjectMessageWithSource injects a message with a source label that
// identifies the origin (e.g., "user", "meditation", "async_result").
// The source is propagated to outputCh events via StateDelta["trigger_source"]
// so consumers can deterministically dispatch responses without inferring.
//
// Messages ALWAYS go to persistentBus — never to invBus. This ensures that
// user messages sent during sub-agent execution are not lost when the
// sub-agent's invBus is discarded. The BeforeModel InjectBusInputs callback
// on the persistent ContextManager will TryPull these messages and inject them
// into the next ReAct iteration.

// InjectMessageWithMetadata injects a message with a source label and
// arbitrary metadata. The metadata is propagated to all events derived
// from this message via event.StateDelta with "meta_" prefix.
//
// Common metadata keys:
//   - "chat_id": target user/session identifier for response routing
//   - "user_name": human-readable user identifier for logs
//   - "channel": communication channel (wechat, discord, etc.)

// setActiveBus sets the current active bus for event injection.
// Called by StartLoop (sets persistentBus) and Run() (sets invBus).

// restorePersistentBus switches the active bus back to the persistent bus.
// Called when a sub-agent Run() completes so InjectMessage resumes routing
// to the persistent AgentLoop (if active).

// setSessionContext sets the userID and sessionID (thread-safe).

// getOrCreateSession returns the session for the given sessionID (or the
// last-known sessionID if empty). Creates the session if it does not exist.

// makeOnEventCallback creates the onEvent callback for StartLoop and Run().
// It performs two tasks:
// 1. Append EventReference to the projection
// 2. Propagate currentMetadata from ContextManager to event.StateDelta with "meta_" prefix

// IngestExternalEvents queues external events to be injected as context
// into the next Run call. This is the mechanism for passing
// context from a parent agent to a tool agent via AgentToolWrapper.
//
// The events are converted to a system message summarizing the external
// context and prepended to the user message. After injection, the pending
// events are cleared.

// injectExternalContext converts pending external events into a context message
// prepended to the user message. After injection, the pending events are cleared.
//
// Only EventSummary is injected — NOT the full Content. This keeps external context
// compact so sub-agents stay within their token budget. The sub-agent retrieves full
// event details via its own memory tools (memory_get, memory_query) if needed.

// ---------------------------------------------------------------------------
// Persistent Event Loop — 持久事件循环
//
// runEventLoop is the core event loop, mirroring the prototype's DefaultRun.
// It pulls events from EventBus, merges them via ContextManager.BuildInvocation,
// and executes the framework Flow via ContextManager.RunFlow.
// ---------------------------------------------------------------------------

// runEventLoop is the core event loop (prototype's DefaultRun equivalent).
// It blocks until ctx is cancelled.
// On RunFlow failure, uses exponential backoff retry (100ms→200ms→400ms, max 3).
// After exhausting retries, publishes an error event to EventBus and continues.

// StartLoop starts the persistent event loop.
// It creates an EventBus, launches an AgentLoop goroutine, and returns
// the outputCh for callers to receive agent_output events.
//
// Subsequent calls with the same agent return the existing outputCh.
// The outputCh is closed when StopLoop is called.

// truncateString truncates s to at most n characters, appending "..." if truncated.

// ---------------------------------------------------------------------------
// SwappableModel — 可运行时替换的 model.Model 包装器
//
// 用于 HTTPAPI 接收 AReaL adapter 传入的 llm_base_url 时，
// 将 LLM 请求重定向到 AReaL proxy（端口动态分配）。
// 不改变事件机制（persistent loop / InjectMessage / outputCh 不变），
// 仅替换底层 model.Model 实例。
// ---------------------------------------------------------------------------


// NewA2AServer creates an A2A server that exposes the given TagentAgent.
