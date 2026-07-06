// Package agent provides tool agent registration for extensible agent composition.
//
// Tool agents are TagentAgent instances wrapped as CallableTool via AgentToolWrapper.
// This file provides the registration mechanism and the wrapper implementation.
//
// Registration flow:
//
//  1. Built-in factories are registered in tagent/builtin.go init()
//  2. Custom factories can be registered via RegisterToolAgent()
//  3. tagent.New() resolves ToolRef entries by building referenced agents
//
// AgentToolWrapper replaces the previous agenttool.NewTool() approach.
// It handles:
//   - Declaring event_key parameter in InputSchema (when EventParams includes it)
//   - Resolving event_key → fetching full event from parent MemStore
//   - Passing event data as external context to the sub-agent
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	tagenttool "github.com/SpellingDragon/tagent/tool"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ==================== External Context Serialization ====================
//
// ExternalContextEntry is the wire format for passing external event context
// across process boundaries (local → RuntimeState → A2A metadata → RuntimeState → remote).
//
// Only EventKey, EventType, and EventSummary are serialized — NOT the full Content.
// This keeps the payload compact (suitable for A2A metadata size limits) while
// preserving the information that injectExternalContext actually uses.
// Remote sub-agents that need full event content can query their own MemoryStore
// using the EventKey.

// ExternalContextEntry is the serializable representation of an external event
// for cross-process context passing via RuntimeState.
type ExternalContextEntry struct {
	EventKey     int64  `json:"event_key"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
}

// ExternalContextKey is the RuntimeState key used to pass external context
// through the Invocation → A2A metadata → Invocation chain.
// Exported so that tagent.go can use it with a2aagent.WithTransferStateKey.
const ExternalContextKey = "external_context"

// serializeExternalContext converts FullEvents into compact JSON entries
// suitable for RuntimeState transport. Only EventKey/EventType/EventSummary
// are included — Content is intentionally excluded to keep the payload small.
func serializeExternalContext(events []memory.FullEvent) ([]byte, error) {
	entries := make([]ExternalContextEntry, len(events))
	for i, evt := range events {
		entries[i] = ExternalContextEntry{
			EventKey:     evt.EventKey,
			EventType:    evt.EventType,
			EventSummary: evt.EventSummary,
		}
	}
	return json.Marshal(entries)
}

// deserializeExternalContext converts JSON bytes back into FullEvents.
// Content is left empty — the remote sub-agent only needs EventSummary
// for context injection (injectExternalContext uses EventSummary only).
func deserializeExternalContext(data []byte) ([]memory.FullEvent, error) {
	var entries []ExternalContextEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("deserialize external context: %w", err)
	}
	events := make([]memory.FullEvent, len(entries))
	for i, e := range entries {
		events[i] = memory.FullEvent{
			EventKey:     e.EventKey,
			EventType:    e.EventType,
			EventSummary: e.EventSummary,
		}
	}
	return events, nil
}

// ==================== AgentToolWrapper ====================
//
// AgentToolWrapper wraps an agent.Agent (local TagentAgent or remote A2AAgent)
// as a plain CallableTool. It handles:
//
//   - InputSchema declares event_keys parameter (list of Snowflake EventKeys)
//   - On Call: extracts event_keys from args, fetches full events from parent MemStore,
//     serializes them into RuntimeState["external_context"], and calls agent.Run
//   - The LLM selects relevant event_keys from its context and passes them to the tool,
//     enabling the tool to retrieve full event details that were compressed away
//   - This prevents the LLM from breaking context isolation — the LLM only outputs
//     numeric keys, but the actual event content is resolved server-side
//   - Context delivery is unified: RuntimeState works for both local (direct Run)
//     and remote (A2A metadata auto-mapping) sub-agents

type AgentToolWrapper struct {
	agent            agent.Agent // unified: *TagentAgent (local) or *a2aagent.A2AAgent (remote)
	desc             string
	eventParams      []string           // Which event-derived params to declare (e.g., "event_key")
	parentStore      memory.MemoryStore // Parent agent's MemStore for resolving event_key
	parentProjection *SessionProjection // Parent agent's projection for auto-inject fallback
}

// autoInjectMaxEvents is the maximum number of recent events to auto-inject
// when LLM does not pass event_keys.
const autoInjectMaxEvents = 5

// NewAgentToolWrapper creates a new AgentToolWrapper.
//   - ag: the sub-agent to wrap (must implement agent.Agent — local TagentAgent or remote A2AAgent)
//   - desc: tool description shown to parent agent's LLM
//   - eventParams: which event-derived parameters to declare and resolve
//   - parentStore: parent agent's MemStore for resolving event_key to full event data
func NewAgentToolWrapper(
	ag agent.Agent,
	desc string,
	eventParams []string,
	parentStore memory.MemoryStore,
) *AgentToolWrapper {
	return &AgentToolWrapper{
		agent:       ag,
		desc:        desc,
		eventParams: eventParams,
		parentStore: parentStore,
	}
}

// SetParentProjection sets the parent agent's SessionProjection for auto-inject fallback.
// When LLM does not pass event_keys, the wrapper auto-injects the most recent
// autoInjectMaxEvents EventKeys from the parent projection.
func (w *AgentToolWrapper) SetParentProjection(p *SessionProjection) {
	w.parentProjection = p
}

// Declaration implements trpctool.Tool.
func (w *AgentToolWrapper) Declaration() *trpctool.Declaration {
	decl := &trpctool.Declaration{
		Name:        w.agent.Info().Name,
		Description: w.desc,
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{},
			Required:   []string{"request"},
		},
	}

	// Standard request parameter
	decl.InputSchema.Properties["request"] = &trpctool.Schema{
		Type:        "string",
		Description: "The request or instruction to process",
	}

	// Declare event-derived parameters
	for _, param := range w.eventParams {
		switch param {
		case "event_key", "event_keys":
			// Always expose as event_keys (array) for consistency.
			// The LLM selects relevant event keys from its context and passes them
			// as a list, enabling the tool to retrieve full event details.
			decl.InputSchema.Properties["event_keys"] = &trpctool.Schema{
				Type:        "array",
				Description: "[LLM-selected] Array of Snowflake EventKeys for related events from the conversation context. Pass the event_keys mentioned in the context summary so the tool can retrieve full event details.",
				Items: &trpctool.Schema{
					Type: "integer",
				},
			}
		}
	}

	return decl
}

// defaultSubAgentTimeout is the default timeout for sub-agent calls.
const defaultSubAgentTimeout = 120 * time.Second

// isRemoteAgent checks if the wrapped agent is a remote A2AAgent.
func isRemoteAgent(ag agent.Agent) bool {
	return fmt.Sprintf("%T", ag) == "*a2aagent.A2AAgent"
}

// Call implements trpctool.CallableTool.
// It:
//  1. Parses JSON args to extract event_keys
//  2. If event_keys are present and parentStore is available, fetches full event data
//  3. Serializes the events into RuntimeState["external_context"] (compact JSON)
//  4. Constructs an Invocation and calls agent.Run with timeout — unified for local and remote
//  5. For remote A2A agents, retries once on failure with 500ms backoff
//  6. Collects the sub-agent's final output from the event stream
func (w *AgentToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	agentName := w.agent.Info().Name

	// Parse args
	var args map[string]interface{}
	if len(jsonArgs) > 0 {
		if err := json.Unmarshal(jsonArgs, &args); err != nil {
			return nil, fmt.Errorf("agent tool %q: parse args: %w", agentName, err)
		}
	}

	// Extract request text
	request, _ := args["request"].(string)

	// Resolve event_keys → full event context
	var keys []int64
	var externalEvents []memory.FullEvent
	if w.parentStore != nil {
		// Collect all event keys from event_keys array
		// Support both: event_keys (array) and event_key (single int, backward compat)
		if eventKeysRaw, ok := args["event_keys"]; ok {
			switch v := eventKeysRaw.(type) {
			case []interface{}:
				for _, item := range v {
					if key := toInt64Key(item); key > 0 {
						keys = append(keys, key)
					}
				}
			case float64: // Single int passed directly
				if key := toInt64Key(v); key > 0 {
					keys = append(keys, key)
				}
			}
		}
		// Backward compat: also check event_key (single)
		if eventKeyFloat, ok := args["event_key"]; ok {
			if key := toInt64Key(eventKeyFloat); key > 0 {
				keys = append(keys, key)
			}
		}

		// Auto-inject: if LLM did not pass event_keys and we have a parentProjection,
		// automatically inject the most recent N event keys as fallback context.
		if len(keys) == 0 && w.parentProjection != nil && w.hasEventKeysParam() {
			keys = w.autoInjectEventKeys()
			if len(keys) > 0 {
				log.Infof("[AgentToolWrapper] auto-injected %d event_keys for agent %q", len(keys), agentName)
			}
		}

		for _, key := range keys {
			evt, err := w.parentStore.GetEvent(key)
			if err == nil && evt != nil {
				externalEvents = append(externalEvents, *evt)
			}
		}
	}

	// === Boundary log: tool INPUT ===
	log.Infof("[TRACE] tool_enter agent=%s request_len=%d event_keys=%d external_events=%d",
		agentName, len(request), len(keys), len(externalEvents))

	// Build the Invocation with RuntimeState carrying external context.
	runOpts := agent.RunOptions{}
	if len(externalEvents) > 0 {
		serialized, err := serializeExternalContext(externalEvents)
		if err != nil {
			return nil, fmt.Errorf("agent tool %q: serialize external context: %w", agentName, err)
		}
		if runOpts.RuntimeState == nil {
			runOpts.RuntimeState = map[string]any{}
		}
		runOpts.RuntimeState[ExternalContextKey] = json.RawMessage(serialized)
	}

	inv := agent.NewInvocation(
		agent.WithInvocationMessage(model.NewUserMessage(request)),
		agent.WithInvocationRunOptions(runOpts),
	)

	// === Run sub-agent with timeout + retry ===
	startTime := time.Now()

	eventCh, err := w.runWithTimeoutAndRetry(ctx, inv, agentName)
	if err != nil {
		return nil, fmt.Errorf("agent tool %q: run failed: %w", agentName, err)
	}

	// Collect the final output from the sub-agent
	var finalOutput string
	var toolCallCount int
	for evt := range eventCh {
		var resp *model.Response
		if evt.Response != nil {
			resp = evt.Response.Clone()
		}
		if resp != nil && len(resp.Choices) > 0 {
			choice := resp.Choices[len(resp.Choices)-1]
			if len(choice.Message.ToolCalls) > 0 {
				toolCallCount++
				var names []string
				for _, tc := range choice.Message.ToolCalls {
					names = append(names, fmt.Sprintf("%s(%s)", tc.Function.Name, truncate(string(tc.Function.Arguments), 80)))
				}
				log.Infof("[ToolAgent:%s] round %d tool call: %s", agentName, toolCallCount, strings.Join(names, ", "))
			}
			if choice.Message.Role == model.RoleTool && choice.Message.Content != "" {
				log.Debugf("[ToolAgent:%s] round %d tool response: %s", agentName, toolCallCount, truncate(choice.Message.Content, 120))
			}
			if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 {
				finalOutput = choice.Message.Content
			}
		}
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Infof("[TRACE] tool_exit agent=%s output_len=%d tool_calls=%d elapsed=%v",
		agentName, len(finalOutput), toolCallCount, elapsed)

	if finalOutput == "" {
		finalOutput = "tool agent completed without output"
	}

	return finalOutput, nil
}

// runWithTimeoutAndRetry wraps agent.Run with a context timeout and,
// for remote A2A agents, retries once on failure with 500ms backoff.
//
// IMPORTANT: agent.Run is async — it returns an event channel immediately and
// the sub-agent produces events in a background goroutine. The cancel function
// must NOT be deferred here, because that would cancel the context as soon as
// this function returns (before the caller finishes consuming the channel).
// Instead, we wrap the returned channel in a goroutine that calls cancel after
// the channel is closed.
func (w *AgentToolWrapper) runWithTimeoutAndRetry(ctx context.Context, inv *agent.Invocation, agentName string) (<-chan *event.Event, error) {
	remote := isRemoteAgent(w.agent)

	runCtx, cancel := context.WithTimeout(ctx, defaultSubAgentTimeout)

	eventCh, err := w.agent.Run(runCtx, inv)
	if err != nil {
		cancel()
		if !remote {
			// Local failure — no retry
			return nil, err
		}

		// Remote A2A failure — retry once after 500ms
		log.Warnf("[AgentToolWrapper] remote agent %q failed (%v), retrying in 500ms", agentName, err)
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		retryCtx, retryCancel := context.WithTimeout(ctx, defaultSubAgentTimeout)
		retryCh, retryErr := w.agent.Run(retryCtx, inv)
		if retryErr != nil {
			retryCancel()
			return nil, retryErr
		}
		// Wrap retry channel: cancel context after consumption
		wrapped := make(chan *event.Event, cap(retryCh))
		go func() {
			defer retryCancel()
			defer close(wrapped)
			for evt := range retryCh {
				wrapped <- evt
			}
		}()
		return wrapped, nil
	}

	// Wrap channel: cancel context after consumption to enforce timeout
	wrapped := make(chan *event.Event, cap(eventCh))
	go func() {
		defer cancel()
		defer close(wrapped)
		for evt := range eventCh {
			wrapped <- evt
		}
	}()
	return wrapped, nil
}

// hasEventKeysParam checks if eventParams includes "event_keys" or "event_key".
func (w *AgentToolWrapper) hasEventKeysParam() bool {
	for _, p := range w.eventParams {
		if p == "event_key" || p == "event_keys" {
			return true
		}
	}
	return false
}

// autoInjectEventKeys returns the most recent N EventKeys from parentProjection.
// Skips EventKey == 0. Returns nil if projection is empty or nil.
func (w *AgentToolWrapper) autoInjectEventKeys() []int64 {
	if w.parentProjection == nil {
		return nil
	}
	refs := w.parentProjection.GetAll()
	if len(refs) == 0 {
		return nil
	}
	// Take the most recent N events
	start := 0
	if len(refs) > autoInjectMaxEvents {
		start = len(refs) - autoInjectMaxEvents
	}
	var keys []int64
	for _, ref := range refs[start:] {
		if ref.EventKey > 0 {
			keys = append(keys, ref.EventKey)
		}
	}
	return keys
}

// ==================== Tool Agent Factory Registry ====================
//
// The factory registry is retained for backward compatibility with existing
// tool agent factories (knowledge, recall). These factories create TagentAgent
// instances that are then wrapped by AgentToolWrapper in tagent.New().
//
// NOTE: In the new agent-centric config model, the primary path for creating
// tool agents is via the Agents map in Config. The factory registry is still
// useful for programmatic registration of custom tool agents.

// ToolAgentFactory creates a tool agent (TagentAgent) from the given config.
// The returned TagentAgent will be wrapped via AgentToolWrapper by the caller
// to become a CallableTool for the parent agent.
type ToolAgentFactory func(cfg ToolAgentFactoryConfig) (*TagentAgent, error)

// ToolAgentFactoryConfig provides everything a factory needs to create a TagentAgent.
//
// In the new architecture, each tool agent has its own isolated MemStore.
// The parent agent's MemStore is NOT passed here — context is delivered via
// the AgentToolWrapper's event_key resolution at call time.
type ToolAgentFactoryConfig struct {
	// ID is the tool agent identifier (e.g., "knowledge", "recall")
	ID string

	// Model is the LLM model for the tool agent (resolved from config)
	Model model.Model

	// SystemPrompt is the loaded system prompt (already resolved from PromptConfig)
	SystemPrompt string

	// Description is the tool description shown to the parent agent's LLM
	Description string

	// SubTools are the pre-built sub-tools for this agent
	SubTools []trpctool.Tool

	// MemoryStore is the tool agent's own memory store (isolated from parent).
	// The factory should use this (or create its own) for the agent's internal storage.
	// Context from the parent is delivered via AgentToolWrapper at call time, not via MemStore.
	MemoryStore memory.MemoryStore

	// ReadPartitionIDs lists PartitionIDs this agent is allowed to read in addition
	// to its own namespace. Injected from MemoryConfig.ReadNamespaces at build time.
	// Used by recall agent's sub-tools to query across agent partitions.
	ReadPartitionIDs []int

	// SkillRepo is the skill repository for knowledge agent (optional).
	SkillRepo tagenttool.SkillRepository

	// MCPToolSets are MCP tool sources for tool discovery (optional).
	MCPToolSets []trpctool.ToolSet

	// Agent parameters
	MaxToolIterations int
	MaxTokens         int
	Temperature       float64
}

var (
	toolAgentFactories   = map[string]ToolAgentFactory{}
	toolAgentFactoriesMu sync.RWMutex
)

// RegisterToolAgent registers a factory for creating tool agents by ID.
func RegisterToolAgent(id string, factory ToolAgentFactory) {
	toolAgentFactoriesMu.Lock()
	defer toolAgentFactoriesMu.Unlock()

	if _, exists := toolAgentFactories[id]; exists {
		panic(fmt.Sprintf("tool agent factory %q already registered", id))
	}
	toolAgentFactories[id] = factory
}

// GetToolAgentFactory returns the factory for the given ID.
func GetToolAgentFactory(id string) (ToolAgentFactory, bool) {
	toolAgentFactoriesMu.RLock()
	defer toolAgentFactoriesMu.RUnlock()

	f, ok := toolAgentFactories[id]
	return f, ok
}

// ==================== Plain Tool Factory Registry ====================

// PlainToolFactory creates a plain tool (implements tool.CallableTool) from the given config.
type PlainToolFactory func(cfg PlainToolFactoryConfig) (trpctool.CallableTool, error)

// PlainToolFactoryConfig provides everything a factory needs to create a plain tool.
type PlainToolFactoryConfig struct {
	ID          string
	Description string
	Properties  map[string]any // Tool-specific config, deserialized by each factory

	// Runtime dependencies (optional, injected by buildAgent).
	// Most plain tools (e.g., exec) ignore these fields.
	// Sub-tools that need runtime objects (e.g., skill_search needs SkillRepo,
	// memory_query needs MemStore) extract them from here.
	MemStore         memory.MemoryStore         // For memory-dependent tools
	SkillRepo        tagenttool.SkillRepository // For skill-dependent tools
	MCPToolSets      []trpctool.ToolSet         // For MCP-dependent tools
	ReadPartitionIDs []int                      // For recall tools that query cross-namespace
}

var (
	plainToolFactories   = map[string]PlainToolFactory{}
	plainToolFactoriesMu sync.RWMutex
)

// RegisterPlainTool registers a factory for creating plain tools by ID.
func RegisterPlainTool(id string, factory PlainToolFactory) {
	plainToolFactoriesMu.Lock()
	defer plainToolFactoriesMu.Unlock()

	if _, exists := plainToolFactories[id]; exists {
		panic(fmt.Sprintf("plain tool factory %q already registered", id))
	}
	plainToolFactories[id] = factory
}

// GetPlainToolFactory returns the factory for the given ID.
func GetPlainToolFactory(id string) (PlainToolFactory, bool) {
	plainToolFactoriesMu.RLock()
	defer plainToolFactoriesMu.RUnlock()

	f, ok := plainToolFactories[id]
	return f, ok
}

// toInt64Key converts a JSON-parsed value to an int64 event key.
// JSON numbers from json.Unmarshal parse as float64 by default.
func toInt64Key(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case json.Number:
		i, err := val.Int64()
		if err == nil {
			return i
		}
	}
	return 0
}

// truncate truncates a string to maxLen characters, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
