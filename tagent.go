// Package tagent provides the top-level composition root for tagent applications.
//
// The root package is responsible for application-level wiring that crosses
// package boundaries — specifically, assembling agent.Tool instances from
// agent + tool + prompt, and connecting tool callbacks back to agents.
//
// Dependency direction (all one-way, no cycles):
//
//	tagent (root) → agent → plugin → memory
//	tagent (root) → tool → memory
//	tagent (root) → prompt
//
// The agent package focuses on the trpc-agent-go core mechanism coordination:
// LLMAgent, Runner, MemoryPlugin, SmartCompressor, ContextIntervention.
// The tool package focuses on pure CallableTool implementations.
// This root package wires them together.
package tagent

import (
	"fmt"
	"log"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool"
	"trpc.group/trpc-go/trpc-agent-go/model"
	tagenttool "trpc.group/trpc-go/trpc-agent-go/tool"
	agenttool "trpc.group/trpc-go/trpc-agent-go/tool/agent"
	"trpc.group/trpc-go/trpc-agent-go/tool/duckduckgo"
)

// KnowledgeAgentConfig holds configuration for creating the Knowledge TagentAgent.
//
// Knowledge Agent is a TagentAgent instance configured for knowledge acquisition
// and translation. It uses internal sub-tools (skill_search, skill_load,
// mcp_discover, duckduckgo_search, memory_query) within an LLM React loop
// to intelligently acquire knowledge and translate it into executable plans.
//
// Architecture: KnowledgeAgent → TagentAgent (agent.Agent) → agent.Tool (CallableTool)
// This means Knowledge Agent gets all tagent core mechanisms automatically:
// MemoryPlugin, SmartCompressor, ContextIntervention, Runner orchestration.
type KnowledgeAgentConfig struct {
	Model       model.Model              // Required: LLM model
	MemStore    tool.MemoryStoreAccessor // Optional: shared memory access
	SkillRepo   tool.SkillRepository     // Optional: skill source
	MCPToolSets []tagenttool.ToolSet     // Optional: MCP tool sources
	PromptDir   string                   // Optional: base directory for prompt files (default: "resources/prompts")

	// Optional overrides
	MaxToolIterations int     // Default: 5 (knowledge acquisition needs few iterations)
	MaxTokens         int     // Default: 4096
	Temperature       float64 // Default: 0.3 (precision over creativity)
}

// NewKnowledgeAgent creates a TagentAgent configured for knowledge acquisition & translation.
// The returned *TagentAgent implements agent.Agent and can be wrapped via agenttool.NewTool()
// to become a CallableTool for the main TagentAgent.
//
// Usage:
//
//	knowledgeAgent, err := tagent.NewKnowledgeAgent(cfg)
//	knowledgeTool := agenttool.NewTool(knowledgeAgent,
//	    agenttool.WithDescription("Knowledge acquisition and translation tool..."),
//	)
//	mainAgent, _ := agent.NewTagentAgent(&agent.TagentConfig{
//	    Tools: []tool.Tool{knowledgeTool, recallTool, commandTool},
//	})
func NewKnowledgeAgent(cfg KnowledgeAgentConfig) (*agent.TagentAgent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("knowledge agent: model is required")
	}

	// 1. Load system prompt from file
	promptDir := cfg.PromptDir
	if promptDir == "" {
		promptDir = "resources/prompts"
	}
	loader := prompt.NewLoader(promptDir)
	systemPrompt, err := loader.LoadFromFile("knowledge_agent.md")
	if err != nil {
		return nil, fmt.Errorf("knowledge agent: load prompt: %w", err)
	}

	// 2. Assemble sub-tools
	subTools := buildKnowledgeSubTools(cfg)

	// 3. Apply defaults
	maxToolIter := cfg.MaxToolIterations
	if maxToolIter <= 0 {
		maxToolIter = 5
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	// 4. Create TagentAgent instance (inherits all tagent core mechanisms)
	agentCfg := &agent.TagentConfig{
		Name:              "knowledge",
		Description:       "Knowledge acquisition and translation agent. Discovers skills, MCP tools, and web resources; translates them into executable plans.",
		Model:             cfg.Model,
		SystemPrompt:      systemPrompt,
		Tools:             subTools,
		MaxToolIterations: maxToolIter,
		MaxTokens:         maxTokens,
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("knowledge agent: create tagent agent: %w", err)
	}

	return ta, nil
}

// NewKnowledgeTool is a convenience function that creates a KnowledgeAgent
// and wraps it as a CallableTool ready for registration.
func NewKnowledgeTool(cfg KnowledgeAgentConfig) (tagenttool.Tool, error) {
	knowledgeAgent, err := NewKnowledgeAgent(cfg)
	if err != nil {
		return nil, err
	}

	return agenttool.NewTool(knowledgeAgent,
		agenttool.WithDescription(`Knowledge acquisition and translation tool. Acquires knowledge needed to complete tasks and translates it into executable plans.

Supports:
1. Capability discovery: search local skills and MCP tools
2. Factual knowledge: web search for facts, concepts, documentation
3. Translation: convert skill content into executable commands (ExecutionPlan)
4. Historical knowledge: query past knowledge events from memory

When the result contains execution_plan, use the command tool to execute it:
- execution_plan.function="exec" → command(mode="exec", command=execution_plan.command)
- execution_plan.function="tmux_exec" → command(mode="tmux_exec", command=execution_plan.command)
- execution_plan.function="mcp_call" → command(mode="exec", command=execution_plan.command)`),
	), nil
}

// buildKnowledgeSubTools assembles the sub-tool set for the Knowledge Agent.
func buildKnowledgeSubTools(cfg KnowledgeAgentConfig) []tagenttool.Tool {
	var tools []tagenttool.Tool

	if cfg.SkillRepo != nil {
		tools = append(tools, tool.NewSkillSearchTool(cfg.SkillRepo))
		tools = append(tools, tool.NewSkillLoadTool(cfg.SkillRepo))
	}

	if len(cfg.MCPToolSets) > 0 {
		tools = append(tools, tool.NewMCPDiscoverTool(cfg.MCPToolSets))
	}

	// Web search: always available via duckduckgo
	tools = append(tools, duckduckgo.NewTool())

	if cfg.MemStore != nil {
		tools = append(tools, tool.NewMemoryQueryTool(cfg.MemStore))
	}

	return tools
}

// WireCommandTool connects a CommandTool's onStateChange callback to TagentAgent.
// When a tmux session state changes (e.g., command completed), TagentAgent injects
// a system_input event to trigger a new agent iteration.
//
// This wiring lives in the root package because it crosses the agent↔tool boundary:
// agent doesn't depend on tool, and tool doesn't depend on agent.
// The root package is the only place that can see both.
func WireCommandTool(ta *agent.TagentAgent, cmdTool *tool.CommandTool) {
	cmdTool.SetOnStateChange(func(sessionID, oldStatus, newStatus, output string) {
		handleTmuxStateChange(ta, sessionID, oldStatus, newStatus, output)
	})
}

// handleTmuxStateChange injects a system_input message to trigger agent re-evaluation.
func handleTmuxStateChange(ta *agent.TagentAgent, sessionID, oldStatus, newStatus, output string) {
	log.Printf("[TagentAgent] tmux state change: session=%s %s -> %s", sessionID, oldStatus, newStatus)

	// Build system_input message describing the state change
	content := fmt.Sprintf("[system] tmux session %s state changed: %s -> %s", sessionID, oldStatus, newStatus)
	if output != "" {
		// Truncate long output for injection
		if len(output) > 2000 {
			output = output[:2000] + "...(truncated)"
		}
		content += fmt.Sprintf("\nOutput:\n%s", output)
	}

	msg := model.Message{
		Role:    model.RoleSystem,
		Content: content,
	}

	// Inject via TagentAgent.RunSimple()
	ta.InjectMessage(msg)
}
