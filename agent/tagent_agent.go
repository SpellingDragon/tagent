// Package agent provides tagent's core agent mechanism coordination.
//
// TagentAgent wires together:
//   - LLMAgent (framework-native React loop)
//   - Runner (framework orchestration with plugins)
//   - MemoryPlugin (OnEvent: event persistence + causal chain)
//   - ContextIntervention (BeforeModel: token budget + SmartCompress)
//
// Core principle: LLMAgent is the React loop skeleton,
// tagent's differential logic is injected via callback/plugin.
//
// TagentAgent implements agent.Agent, so it can be wrapped as agent.Tool
// for tool-agent composition.
//
// NOTE: This package does NOT depend on tagent/tool.
// Application-level wiring (KnowledgeAgent assembly, WireCommandTool, etc.)
// lives in the root tagent package.
package agent

import (
	"context"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
)

// Verify TagentAgent implements agent.Agent at compile time.
var _ agent.Agent = (*TagentAgent)(nil)

// TagentAgent is tagent's top-level Agent assembly.
// It implements agent.Agent so it can be used both as a standalone agent
// and as a tool-agent (wrapped via AgentToolWrapper).
type TagentAgent struct {
	llmAgent *llmagent.LLMAgent
	runner   runner.Runner
	memStore memory.MemoryStore
	config   *TagentConfig

	// Agent identity (for agent.Agent interface)
	name        string
	description string

	// Session context for event injection (set on first Run)
	lastUserID    string
	lastSessionID string

	// External events pending ingestion (set before Run)
	// These are converted to internal context messages at the start of the next run.
	pendingExternalEvents []memory.FullEvent
}

// TagentConfig holds configuration for creating a TagentAgent.
type TagentConfig struct {
	Model             model.Model        // Required: LLM model
	MemoryStore       memory.MemoryStore // Optional: external MemoryStore (default: InMemoryStore)
	SystemPrompt      string             // System prompt loaded from AGENTS.md/SOUL.md/USER.md/TOOLS.md
	Tools             []tool.Tool        // CallableTools to register
	MaxToolIterations int                // Default: 200
	MaxTokens         int                // Token budget for context (default: 8000)
	CompressThreshold float64            // Compression trigger threshold (default: 0.8)
	SummaryModel      model.Model        // Optional: for Stage 2 LLM summary
	Temperature       float64            // Optional: LLM temperature (default: 0.7)

	// Agent identity (for agent.Agent interface)
	Name        string // Default: "tagent"
	Description string // Default: "TagentAgent - AI assistant powered by tagent"
}

// Default configuration values
const (
	DefaultMaxToolIterations = 200
	DefaultMaxTokens         = 8000
	DefaultCompressThreshold = 0.8
	DefaultAgentName         = "tagent"
	DefaultAgentDescription  = "TagentAgent - AI assistant powered by tagent"
)

// NewTagentAgent creates a new TagentAgent with the given configuration.
func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if cfg.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Apply defaults
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = DefaultMaxToolIterations
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.CompressThreshold <= 0 || cfg.CompressThreshold > 1 {
		cfg.CompressThreshold = DefaultCompressThreshold
	}

	// 1. Create MemoryStore (use provided or default to InMemoryStore)
	var memStore memory.MemoryStore
	if cfg.MemoryStore != nil {
		memStore = cfg.MemoryStore
	} else {
		memStore = memory.NewInMemoryStore()
	}

	// 2. Create MemoryPlugin (OnEvent: event persistence + causal chain + StateDelta)
	memPlugin := plugin.NewMemoryPlugin(memStore)

	// 3. Create SmartCompressor
	compressorOpts := []SmartCompressorOption{}
	if cfg.SummaryModel != nil {
		compressorOpts = append(compressorOpts, WithSummaryModel(cfg.SummaryModel))
	}
	compressor := NewSmartCompressor(compressorOpts...)

	// 4. Create ContextIntervention (BeforeModel: token budget + compress)
	tokenCounter := NewDefaultTokenCounter()
	ci := NewContextIntervention(compressor, tokenCounter, cfg.MaxTokens, cfg.CompressThreshold)

	// 5. Create ModelCallbacks
	modelCB := model.NewCallbacks()
	modelCB.RegisterBeforeModel(ci.BeforeModel)

	// Apply identity defaults
	name := cfg.Name
	if name == "" {
		name = DefaultAgentName
	}
	description := cfg.Description
	if description == "" {
		description = DefaultAgentDescription
	}

	// 6. Create LLMAgent
	llmAgentOpts := []llmagent.Option{
		llmagent.WithModel(cfg.Model),
		llmagent.WithInstruction(cfg.SystemPrompt),
		llmagent.WithMaxToolIterations(cfg.MaxToolIterations),
		llmagent.WithModelCallbacks(modelCB),
	}
	if len(cfg.Tools) > 0 {
		llmAgentOpts = append(llmAgentOpts, llmagent.WithTools(cfg.Tools))
	}
	llmAgent := llmagent.New(name, llmAgentOpts...)

	// 7. Create Runner with MemoryPlugin and SummaryPlugin
	r := runner.NewRunner(name, llmAgent, runner.WithPlugins(
		plugin.NewSummaryPlugin(),
		memPlugin,
	))

	return &TagentAgent{
		llmAgent:    llmAgent,
		runner:      r,
		memStore:    memStore,
		config:      cfg,
		name:        name,
		description: description,
	}, nil
}

// Run implements agent.Agent interface.
// It creates an Invocation from the message and delegates to the runner.
// If there are pending external events, they are prepended as context messages.
func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	// Extract or generate userID and sessionID from Invocation
	userID := "tagent-user"
	sessionID := fmt.Sprintf("tagent-session-%s", inv.InvocationID)

	// Store session context for event injection
	ta.lastUserID = userID
	ta.lastSessionID = sessionID

	message := inv.Message
	if message.Content == "" {
		message = model.NewUserMessage("")
	}

	// Prepend external event context if pending
	if len(ta.pendingExternalEvents) > 0 {
		message = ta.injectExternalContext(message)
	}

	return ta.runner.Run(ctx, userID, sessionID, message)
}

// RunSimple is a convenience method that creates an Invocation and calls Run.
// This preserves the original ergonomic API for direct usage.
// If there are pending external events, they are prepended as context messages.
func (ta *TagentAgent) RunSimple(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
) (<-chan *event.Event, error) {
	ta.lastUserID = userID
	ta.lastSessionID = sessionID

	// Prepend external event context if pending
	if len(ta.pendingExternalEvents) > 0 {
		message = ta.injectExternalContext(message)
	}

	log.Infof("[RunSimple] starting agent run user=%s session=%s", userID, sessionID)
	ch, err := ta.runner.Run(ctx, userID, sessionID, message)
	if err != nil {
		log.Errorf("[RunSimple] runner.Run failed: %v", err)
	}
	return ch, err
}

// Tools implements agent.Agent interface.
func (ta *TagentAgent) Tools() []tool.Tool {
	return ta.llmAgent.Tools()
}

// Info implements agent.Agent interface.
func (ta *TagentAgent) Info() agent.Info {
	return agent.Info{
		Name:        ta.name,
		Description: ta.description,
	}
}

// SubAgents implements agent.Agent interface.
func (ta *TagentAgent) SubAgents() []agent.Agent {
	return ta.llmAgent.SubAgents()
}

// FindSubAgent implements agent.Agent interface.
func (ta *TagentAgent) FindSubAgent(name string) agent.Agent {
	return ta.llmAgent.FindSubAgent(name)
}

// Close closes the runner and releases resources.
func (ta *TagentAgent) Close() error {
	return ta.runner.Close()
}

// MemStore returns the MemoryStore for direct access (e.g., by RecallTool).
func (ta *TagentAgent) MemStore() memory.MemoryStore {
	return ta.memStore
}

// Runner returns the underlying Runner (for TmuxMonitor event injection).
func (ta *TagentAgent) Runner() runner.Runner {
	return ta.runner
}

// InjectMessage injects a system message into the current session to trigger
// a new agent iteration. This is the mechanism used by CommandTool's tmux
// notifications.
//
// IMPORTANT: Uses a 5-minute timeout context to prevent goroutine leaks.
// Events from the injection are drained to prevent blocking the runner's
// event loop. If the context times out, the injection is abandoned — the
// runner handles context cancellation gracefully via its event loop.
func (ta *TagentAgent) InjectMessage(msg model.Message) {
	if ta.lastUserID == "" || ta.lastSessionID == "" {
		return
	}

	// Use a bounded context — never use context.Background() without timeout
	// as it can cause goroutine leaks if the runner hangs.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	log.Infof("[InjectMessage] injecting system message into session=%s", ta.lastSessionID)

	eventCh, err := ta.runner.Run(ctx, ta.lastUserID, ta.lastSessionID, msg)
	if err != nil {
		log.Warnf("[InjectMessage] runner.Run failed: %v", err)
		return
	}

	// Drain events with timeout to prevent goroutine leak.
	// If the runner hangs, the context timeout will cancel it.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[InjectMessage] panic in event drain: %v", r)
			}
		}()
		count := 0
		for range eventCh {
			count++
		}
		log.Debugf("[InjectMessage] drained %d events from injection", count)
	}()
}

// IngestExternalEvents queues external events to be injected as context
// into the next Run/RunSimple call. This is the mechanism for passing
// context from a parent agent to a tool agent via AgentToolWrapper.
//
// The events are converted to a system message summarizing the external
// context and prepended to the user message. After injection, the pending
// events are cleared.
func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent) {
	ta.pendingExternalEvents = events
}

// injectExternalContext converts pending external events into a context message
// prepended to the user message. After injection, the pending events are cleared.
//
// Only EventSummary is injected — NOT the full Content. This keeps external context
// compact so sub-agents stay within their token budget. The sub-agent retrieves full
// event details via its own memory tools (memory_get, memory_query) if needed.
func (ta *TagentAgent) injectExternalContext(msg model.Message) model.Message {
	events := ta.pendingExternalEvents
	ta.pendingExternalEvents = nil // Clear after consumption

	if len(events) == 0 {
		return msg
	}

	// Build external context summary (EventSummary only — compact, no full Content)
	var contextBuilder string
	contextBuilder = "[External Context from Parent Agent]\n\n"
	for i, evt := range events {
		contextBuilder += fmt.Sprintf("Event %d: [%s] %s\n", i+1, evt.EventType, evt.EventSummary)
	}
	contextBuilder += "\n[End of External Context]\n\n"

	log.Infof("[InjectContext] injecting %d external events, context_len=%d", len(events), len(contextBuilder))

	// Prepend external context to the user message
	msg.Content = contextBuilder + msg.Content
	return msg
}
