package tagent

import (
	"fmt"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/tool/command"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	"github.com/SpellingDragon/tagent/tool/recall"

	tagenttool "trpc.group/trpc-go/trpc-agent-go/tool"
	agenttool "trpc.group/trpc-go/trpc-agent-go/tool/agent"
)

func init() {
	agent.RegisterToolAgent("knowledge", knowledgeFactory)
	agent.RegisterToolAgent("recall", recallFactory)
	agent.RegisterPlainTool("command", commandFactory)
}

// knowledgeFactory creates a KnowledgeAgent via knowledge.NewAgent.
func knowledgeFactory(cfg agent.ToolAgentFactoryConfig) (*agent.TagentAgent, error) {
	knowledgeCfg := knowledge.Config{
		Model:             cfg.Model,
		MemStore:          cfg.MemStore, // Tool agent queries parent's memory for event context
		MaxToolIterations: cfg.MaxToolIterations,
		MaxTokens:         cfg.MaxTokens,
		Temperature:       cfg.Temperature,
		Description:       cfg.Description,
	}

	ta, err := knowledge.NewAgent(knowledgeCfg)
	if err != nil {
		return nil, fmt.Errorf("knowledge factory: %w", err)
	}
	return ta, nil
}

// recallFactory creates a RecallAgent via recall.NewAgent.
func recallFactory(cfg agent.ToolAgentFactoryConfig) (*agent.TagentAgent, error) {
	recallCfg := recall.Config{
		Model:             cfg.Model,
		MemStore:          cfg.MemStore, // Tool agent queries parent's memory for event context
		MaxToolIterations: cfg.MaxToolIterations,
		MaxTokens:         cfg.MaxTokens,
		Description:       cfg.Description,
	}

	ta, err := recall.NewAgent(recallCfg)
	if err != nil {
		return nil, fmt.Errorf("recall factory: %w", err)
	}
	return ta, nil
}

// commandFactory creates a CommandTool via command.NewCommandTool.
func commandFactory(cfg agent.PlainToolFactoryConfig) (tagenttool.CallableTool, error) {
	opts := []command.CommandToolOption{}

	if cfg.Description != "" {
		opts = append(opts, command.WithDescription(cfg.Description))
	}

	if cfg.Config != nil {
		if ws, ok := cfg.Config["workspace"].(string); ok && ws != "" {
			opts = append(opts, command.WithCommandWorkspace(ws))
		}
		if user, ok := cfg.Config["run_as_user"].(string); ok && user != "" {
			opts = append(opts, command.WithCommandRunAsUser(user))
		}
		if group, ok := cfg.Config["run_as_group"].(string); ok && group != "" {
			opts = append(opts, command.WithCommandRunAsGroup(group))
		}
	}

	return command.NewCommandTool(opts...), nil
}

// wrapToolAgent wraps a TagentAgent as an agenttool.Tool for registration.
func wrapToolAgent(ta *agent.TagentAgent, description string) tagenttool.Tool {
	opts := []agenttool.Option{}
	if description != "" {
		opts = append(opts, agenttool.WithDescription(description))
	}
	return agenttool.NewTool(ta, opts...)
}
