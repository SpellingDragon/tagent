package tagent

import (
	"encoding/json"
	"fmt"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	"github.com/SpellingDragon/tagent/tool/recall"

	tagenttool "trpc.group/trpc-go/trpc-agent-go/tool"
)

func init() {
	agent.RegisterToolAgent("knowledge", knowledgeFactory)
	agent.RegisterToolAgent("recall", recallFactory)
	agent.RegisterPlainTool("action", actionFactory)
}

// knowledgeFactory creates a KnowledgeAgent via knowledge.NewAgent.
// In the new architecture, each tool agent creates its own isolated MemStore.
func knowledgeFactory(cfg agent.ToolAgentFactoryConfig) (*agent.TagentAgent, error) {
	knowledgeCfg := knowledge.Config{
		Model:             cfg.Model,
		MaxToolIterations: cfg.MaxToolIterations,
		MaxTokens:         cfg.MaxTokens,
		Temperature:       cfg.Temperature,
		Description:       cfg.Description,
	}

	// Use the factory-provided MemoryStore if available, otherwise agent will create its own
	if cfg.MemoryStore != nil {
		knowledgeCfg.MemStore = cfg.MemoryStore // agent's own store, NOT parent's
	}

	// Forward SkillRepo for knowledge agent sub-tools (skill_search, skill_load)
	if cfg.SkillRepo != nil {
		knowledgeCfg.SkillRepo = cfg.SkillRepo
	}

	// Forward MCPToolSets for MCP tool discovery (mcp_discover)
	if len(cfg.MCPToolSets) > 0 {
		knowledgeCfg.MCPToolSets = cfg.MCPToolSets
	}

	ta, err := knowledge.NewAgent(knowledgeCfg)
	if err != nil {
		return nil, fmt.Errorf("knowledge factory: %w", err)
	}
	return ta, nil
}

// recallFactory creates a RecallAgent via recall.NewAgent.
// In the new architecture, each tool agent creates its own isolated MemStore.
func recallFactory(cfg agent.ToolAgentFactoryConfig) (*agent.TagentAgent, error) {
	recallCfg := recall.Config{
		Model:             cfg.Model,
		MaxToolIterations: cfg.MaxToolIterations,
		MaxTokens:         cfg.MaxTokens,
		Description:       cfg.Description,
		ReadPartitionIDs:  cfg.ReadPartitionIDs,
	}

	// Use the factory-provided MemoryStore if available
	if cfg.MemoryStore != nil {
		recallCfg.MemStore = cfg.MemoryStore // agent's own store, NOT parent's
	}

	ta, err := recall.NewAgent(recallCfg)
	if err != nil {
		return nil, fmt.Errorf("recall factory: %w", err)
	}
	return ta, nil
}

// actionFactory creates an ActionTool (registered as "action") via action.NewActionTool.
// It deserializes the tool-specific Properties into ActionProperties.
func actionFactory(cfg agent.PlainToolFactoryConfig) (tagenttool.CallableTool, error) {
	opts := []action.ActionToolOption{}

	if cfg.Description != "" {
		opts = append(opts, action.WithDescription(cfg.Description))
	}

	// Deserialize tool-specific properties
	var props action.ActionProperties
	if err := decodeProperties(cfg.Properties, &props); err != nil {
		return nil, fmt.Errorf("action factory: invalid properties: %w", err)
	}

	if props.Workspace != "" {
		opts = append(opts, action.WithActionWorkspace(props.Workspace))
	}
	if props.RunAsUser != "" {
		opts = append(opts, action.WithActionRunAsUser(props.RunAsUser))
	}
	if props.RunAsGroup != "" {
		opts = append(opts, action.WithActionRunAsGroup(props.RunAsGroup))
	}

	return action.NewActionTool(opts...), nil
}

// decodeProperties deserializes a map[string]any into a typed struct
// by round-tripping through JSON. This is the standard Go pattern for
// converting mapstructure/YAML maps into typed configurations.
func decodeProperties(props map[string]any, target any) error {
	if len(props) == 0 {
		return nil
	}
	data, err := json.Marshal(props)
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal properties: %w", err)
	}
	return nil
}
