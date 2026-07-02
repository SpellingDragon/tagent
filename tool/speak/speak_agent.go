package speak

import (
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/model"
	tagenttool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
)

// PromptConfig describes how to load a system prompt (bootstrap style).
type PromptConfig = prompt.CompositeConfig

// Config holds configuration for creating the Speak Agent.
//
// SpeakAgent is a TagentAgent stub for voice output. It is currently
// disabled and will be activated in a future version when a voice
// synthesis model is attached.
type Config struct {
	Model    model.Model        // Required: LLM model
	MemStore memory.MemoryStore // Optional: agent's own MemoryStore

	Tools []tagenttool.Tool // Optional: sub-tools (none by default)

	PromptDir string // Optional: base directory for prompt files (default: "resources/prompts")

	Prompt PromptConfig // Optional: overrides PromptDir + "speak_agent.md" if set

	MaxToolIterations int     // Default: 3
	MaxTokens         int     // Default: 1024
	Temperature       float64 // Default: 0.3
}

// NewAgent creates a TagentAgent stub for voice output.
func NewAgent(cfg Config) (*agent.TagentAgent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("speak agent: model is required")
	}

	promptDir := cfg.PromptDir
	if promptDir == "" {
		promptDir = "resources/prompts"
	}
	loader := prompt.NewLoader(promptDir)

	var systemPrompt string
	var err error
	if !cfg.Prompt.IsEmpty() {
		systemPrompt, err = loader.LoadComposite(cfg.Prompt.Inline, cfg.Prompt.Files, cfg.Prompt.Dir)
		if err != nil {
			return nil, fmt.Errorf("speak agent: load prompt: %w", err)
		}
	} else {
		systemPrompt, err = loader.LoadFromFile("speak_agent.md")
		if err != nil {
			return nil, fmt.Errorf("speak agent: load prompt: %w", err)
		}
	}

	var subTools []tagenttool.Tool
	if len(cfg.Tools) > 0 {
		subTools = append(subTools, cfg.Tools...)
	}

	maxToolIter := cfg.MaxToolIterations
	if maxToolIter <= 0 {
		maxToolIter = 3
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	agentCfg := &agent.TagentConfig{
		Name:              "speak",
		Description:       "Voice output agent (stub). Not yet implemented.",
		Model:             cfg.Model,
		MemoryStore:       cfg.MemStore,
		SystemPrompt:      systemPrompt,
		Tools:             subTools,
		MaxToolIterations: maxToolIter,
		MaxTokens:         maxTokens,
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("speak agent: create tagent agent: %w", err)
	}

	return ta, nil
}
