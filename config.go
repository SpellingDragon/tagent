package tagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SpellingDragon/tagent/prompt"
	"gopkg.in/yaml.v3"
)

// Config is the top-level tagent configuration.
// Declarative and serializable — loadable from YAML or JSON.
// Runtime-only dependencies (model instances, memory stores, etc.) are injected via Option functions.
//
// Example YAML:
//
//	name: tagent
//	model: glm-4-flash
//	prompt_dir: resources/prompts
//	system_prompt:
//	  files: [AGENTS.md, SOUL.md, USER.md, TOOLS.md]
//	tools:
//	  - id: knowledge
//	    kind: agent
//	    description_file: knowledge_tool_desc.md
type Config struct {
	Name      string `json:"name"       yaml:"name"`
	Model     string `json:"model"      yaml:"model"`      // Model name string (resolved at runtime)
	PromptDir string `json:"prompt_dir" yaml:"prompt_dir"` // Base directory for prompt file resolution

	// Top-level agent settings
	SystemPrompt      PromptConfig `json:"system_prompt"        yaml:"system_prompt"`
	MaxToolIterations int          `json:"max_tool_iterations"  yaml:"max_tool_iterations"`
	MaxTokens         int          `json:"max_tokens"           yaml:"max_tokens"`
	Temperature       float64      `json:"temperature"          yaml:"temperature"`
	CompressThreshold float64      `json:"compress_threshold"   yaml:"compress_threshold"`

	// Declarative tool list
	Tools []ToolConfig `json:"tools" yaml:"tools"`
}

// PromptConfig is an alias for prompt.CompositeConfig, providing bootstrap-style
// prompt loading aligned with nanobot's pattern (AGENTS.md, SOUL.md, USER.md, TOOLS.md).
//
// Prompt composition order: inline → files (in order) → directory scan.
type PromptConfig = prompt.CompositeConfig

// ToolKind distinguishes tool agents from plain tools.
type ToolKind string

const (
	// ToolKindAgent: TagentAgent + agenttool.NewTool() wrapper.
	// Has internal React loop, system prompt, and sub-tools.
	ToolKindAgent ToolKind = "agent"

	// ToolKindTool: directly implements CallableTool.
	// Pure execution tool with no internal React loop.
	ToolKindTool ToolKind = "tool"
)

// ToolConfig declares a tool to be created by New().
type ToolConfig struct {
	ID   string   `json:"id"   yaml:"id"`
	Kind ToolKind `json:"kind" yaml:"kind"` // "agent" or "tool"

	// Model override for tool agents (defaults to top-level model)
	Model string `json:"model,omitempty" yaml:"model,omitempty"`

	// Prompt config for tool agents
	Prompt PromptConfig `json:"prompt,omitempty" yaml:"prompt,omitempty"`

	// Tool description: inline or from file (relative to prompt_dir)
	Description     string `json:"description,omitempty"      yaml:"description,omitempty"`
	DescriptionFile string `json:"description_file,omitempty" yaml:"description_file,omitempty"`

	// Agent parameters (kind=agent)
	MaxToolIterations int     `json:"max_tool_iterations,omitempty" yaml:"max_tool_iterations,omitempty"`
	MaxTokens         int     `json:"max_tokens,omitempty"          yaml:"max_tokens,omitempty"`
	Temperature       float64 `json:"temperature,omitempty"         yaml:"temperature,omitempty"`

	// Command-specific (id=command, kind=tool)
	Workspace  string `json:"workspace,omitempty"    yaml:"workspace,omitempty"`
	RunAsUser  string `json:"run_as_user,omitempty"  yaml:"run_as_user,omitempty"`
	RunAsGroup string `json:"run_as_group,omitempty" yaml:"run_as_group,omitempty"`

	// Extension: custom factory path (for non-builtin tools/agents)
	Factory string         `json:"factory,omitempty" yaml:"factory,omitempty"`
	Config  map[string]any `json:"config,omitempty"  yaml:"config,omitempty"`
}

// Default values
const (
	DefaultName           = "tagent"
	DefaultPromptDir      = "resources/prompts"
	DefaultMaxToolIter    = 200
	DefaultMaxTokens      = 8000
	DefaultTemperature    = 0.7
	DefaultCompressThresh = 0.8

	DefaultAgentMaxToolIter = 5
	DefaultAgentMaxTokens   = 4096
	DefaultAgentTemp        = 0.3
)

// DefaultConfig returns a Config with sensible defaults and the three core tools.
func DefaultConfig() Config {
	return Config{
		Name:              DefaultName,
		PromptDir:         DefaultPromptDir,
		MaxToolIterations: DefaultMaxToolIter,
		MaxTokens:         DefaultMaxTokens,
		Temperature:       DefaultTemperature,
		CompressThreshold: DefaultCompressThresh,
		SystemPrompt: PromptConfig{
			Files: []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md", "HEARTBEAT.md", "MEMORY.md"},
		},
		Tools: []ToolConfig{
			{
				ID:                "knowledge",
				Kind:              ToolKindAgent,
				Prompt:            PromptConfig{Files: []string{"knowledge_agent.md"}},
				DescriptionFile:   "knowledge_tool_desc.md",
				MaxToolIterations: DefaultAgentMaxToolIter,
				MaxTokens:         DefaultAgentMaxTokens,
				Temperature:       DefaultAgentTemp,
			},
			{
				ID:                "recall",
				Kind:              ToolKindAgent,
				Prompt:            PromptConfig{Files: []string{"recall_agent.md"}},
				DescriptionFile:   "recall_tool_desc.md",
				MaxToolIterations: DefaultAgentMaxToolIter,
				MaxTokens:         DefaultAgentMaxTokens,
			},
			{
				ID:              "command",
				Kind:            ToolKindTool,
				DescriptionFile: "command_tool_desc.md",
			},
		},
	}
}

// ApplyDefaults fills in zero/empty values with defaults.
func (c *Config) ApplyDefaults() {
	if c.Name == "" {
		c.Name = DefaultName
	}
	if c.PromptDir == "" {
		c.PromptDir = DefaultPromptDir
	}
	if c.MaxToolIterations <= 0 {
		c.MaxToolIterations = DefaultMaxToolIter
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	if c.Temperature <= 0 {
		c.Temperature = DefaultTemperature
	}
	if c.CompressThreshold <= 0 {
		c.CompressThreshold = DefaultCompressThresh
	}

	for i := range c.Tools {
		tc := &c.Tools[i]
		if tc.Kind == "" {
			tc.Kind = ToolKindAgent
		}
		if tc.Kind == ToolKindAgent {
			if tc.MaxToolIterations <= 0 {
				tc.MaxToolIterations = DefaultAgentMaxToolIter
			}
			if tc.MaxTokens <= 0 {
				tc.MaxTokens = DefaultAgentMaxTokens
			}
			if tc.Temperature <= 0 && tc.Model == "" {
				tc.Temperature = DefaultAgentTemp
			}
		}
	}
}

// Validate checks the config for errors after defaults are applied.
func (c *Config) Validate() error {
	if c.Model == "" {
		return fmt.Errorf("tagent config: model is required")
	}

	seenIDs := make(map[string]bool, len(c.Tools))
	for i, tc := range c.Tools {
		if tc.ID == "" {
			return fmt.Errorf("tagent config: tools[%d].id is required", i)
		}
		if tc.Kind != ToolKindAgent && tc.Kind != ToolKindTool {
			return fmt.Errorf("tagent config: tools[%d].kind must be %q or %q, got %q",
				i, ToolKindAgent, ToolKindTool, tc.Kind)
		}
		if seenIDs[tc.ID] {
			return fmt.Errorf("tagent config: duplicate tool id %q", tc.ID)
		}
		seenIDs[tc.ID] = true

		if tc.Kind == ToolKindAgent && tc.Description == "" && tc.DescriptionFile == "" {
			return fmt.Errorf("tagent config: tool agent %q requires description or description_file", tc.ID)
		}
	}

	return nil
}

// LoadConfig loads configuration from a YAML or JSON file.
// Format is auto-detected from the file extension (.yaml/.yml → YAML, .json → JSON).
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	cfg := &Config{}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse YAML config %s: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse JSON config %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("unsupported config file extension %q (use .yaml, .yml, or .json)", ext)
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
