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

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
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
// and as a tool-agent (wrapped via agent.NewTool).
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

	return ta.runner.Run(ctx, userID, sessionID, message)
}

// RunSimple is a convenience method that creates an Invocation and calls Run.
// This preserves the original ergonomic API for direct usage.
func (ta *TagentAgent) RunSimple(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
) (<-chan *event.Event, error) {
	ta.lastUserID = userID
	ta.lastSessionID = sessionID
	return ta.runner.Run(ctx, userID, sessionID, message)
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
// a new agent iteration. This is the mechanism used by the root package's
// WireCommandTool to notify the agent when a tmux session state changes.
//
// The message is injected via Runner.Run() using the last known userID/sessionID.
// Events from the new iteration are drained to prevent goroutine leaks.
func (ta *TagentAgent) InjectMessage(msg model.Message) {
	if ta.lastUserID == "" || ta.lastSessionID == "" {
		return
	}

	ctx := context.Background()
	eventCh, err := ta.runner.Run(ctx, ta.lastUserID, ta.lastSessionID, msg)
	if err != nil {
		return
	}

	// Drain events to prevent goroutine leak
	go func() {
		for range eventCh {
		}
	}()
}
