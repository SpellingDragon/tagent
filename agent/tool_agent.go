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
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ==================== AgentToolWrapper ====================
//
// AgentToolWrapper wraps a TagentAgent as a plain CallableTool.
// It replaces agenttool.NewTool() with tagent-specific logic:
//
//   - InputSchema declares event_keys parameter (list of Snowflake EventKeys)
//   - On Call: extracts event_keys from args, fetches full events from parent MemStore,
//     and passes the event data as external context to the sub-agent
//   - The LLM selects relevant event_keys from its context and passes them to the tool,
//     enabling the tool to retrieve full event details that were compressed away
//   - This prevents the LLM from breaking context isolation — the LLM only outputs
//     numeric keys, but the actual event content is resolved server-side

type AgentToolWrapper struct {
	agent       *TagentAgent
	desc        string
	eventParams []string           // Which event-derived params to declare (e.g., "event_key")
	parentStore memory.MemoryStore // Parent agent's MemStore for resolving event_key
}

// NewAgentToolWrapper creates a new AgentToolWrapper.
//   - agent: the sub-agent to wrap
//   - desc: tool description shown to parent agent's LLM
//   - eventParams: which event-derived parameters to declare and resolve
//   - parentStore: parent agent's MemStore for resolving event_key to full event data
func NewAgentToolWrapper(
	agent *TagentAgent,
	desc string,
	eventParams []string,
	parentStore memory.MemoryStore,
) *AgentToolWrapper {
	return &AgentToolWrapper{
		agent:       agent,
		desc:        desc,
		eventParams: eventParams,
		parentStore: parentStore,
	}
}

// Declaration implements trpctool.Tool.
func (w *AgentToolWrapper) Declaration() *trpctool.Declaration {
	decl := &trpctool.Declaration{
		Name:        w.agent.name,
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

// Call implements trpctool.CallableTool.
// It:
//  1. Parses JSON args to extract event_keys
//  2. If event_keys are present and parentStore is available, fetches full event data
//  3. Passes the event data as external context to the sub-agent via IngestExternalEvents
//  4. Runs the sub-agent and returns its final output
func (w *AgentToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	// Parse args
	var args map[string]interface{}
	if len(jsonArgs) > 0 {
		if err := json.Unmarshal(jsonArgs, &args); err != nil {
			return nil, fmt.Errorf("agent tool %q: parse args: %w", w.agent.name, err)
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

		for _, key := range keys {
			evt, err := w.parentStore.GetEvent(key)
			if err == nil && evt != nil {
				externalEvents = append(externalEvents, *evt)
			}
		}
	}

	// === Boundary log: tool INPUT ===
	log.Infof("[TRACE] tool_enter agent=%s request_len=%d event_keys=%d external_events=%d",
		w.agent.name, len(request), len(keys), len(externalEvents))

	// Build the user message
	msg := model.NewUserMessage(request)

	// Inject external events as context before running
	if len(externalEvents) > 0 {
		w.agent.IngestExternalEvents(externalEvents)
	}

	// === Boundary log: run sub-agent with timing ===
	startTime := time.Now()

	// Run the sub-agent
	eventCh, err := w.agent.RunSimple(ctx, "tool-agent-user", fmt.Sprintf("tool-agent-session-%s", w.agent.name), msg)
	if err != nil {
		return nil, fmt.Errorf("agent tool %q: run failed: %w", w.agent.name, err)
	}

	// Collect the final output from the sub-agent
	var finalOutput string
	var toolCallCount int
	for evt := range eventCh {
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			choice := evt.Response.Choices[len(evt.Response.Choices)-1]
			// Log internal tool calls for audit visibility
			if len(choice.Message.ToolCalls) > 0 {
				toolCallCount++
				var names []string
				for _, tc := range choice.Message.ToolCalls {
					names = append(names, fmt.Sprintf("%s(%s)", tc.Function.Name, truncate(string(tc.Function.Arguments), 80)))
				}
				log.Infof("[ToolAgent:%s] round %d tool call: %s", w.agent.name, toolCallCount, strings.Join(names, ", "))
			}
			// Track tool responses
			if choice.Message.Role == model.RoleTool && choice.Message.Content != "" {
				log.Debugf("[ToolAgent:%s] round %d tool response: %s", w.agent.name, toolCallCount, truncate(choice.Message.Content, 120))
			}
			// Final output: assistant message without tool calls
			if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 {
				finalOutput = choice.Message.Content
			}
		}
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Infof("[TRACE] tool_exit agent=%s output_len=%d tool_calls=%d elapsed=%v",
		w.agent.name, len(finalOutput), toolCallCount, elapsed)

	if finalOutput == "" {
		finalOutput = "tool agent completed without output"
	}

	return finalOutput, nil
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
