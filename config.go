package tagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"gopkg.in/yaml.v3"
)

// Config is the top-level tagent configuration.
// Declarative and serializable — loadable from YAML or JSON.
// Runtime-only dependencies (model instances, memory stores, etc.) are injected via Option functions.
//
// The configuration follows an agent-centric design: each agent describes its own
// settings (model, memory, tools) and its communication intent (which agents it calls).
// The top-level Config holds a map of agent configs, keyed by agent name.
//
// Example YAML:
//
//	agents:
//	  tagent:
//	    model: glm-4-flash
//	    prompt_dir: resources/prompts
//	    system_prompt:
//	      files: [AGENTS.md, SOUL.md, USER.md, TOOLS.md]
//	    memory:
//	      type: file
//	      path: /data/tagent/events
//	    tools:
//	      - agent: knowledge
//	        description_file: knowledge_tool_desc.md
//	        event_params: [event_key]
//	      - agent: recall
//	        description_file: recall_tool_desc.md
//	        event_params: [event_key]
//	      - kind: tool
//	        id: action
//	        description_file: action_tool_desc.md
//	  knowledge:
//	    model: glm-4-flash
//	    prompt:
//	      files: [knowledge_agent.md]
//	    memory:
//	      type: memory
//	    max_tool_iterations: 5
//	    max_tokens: 4096
//	  recall:
//	    model: glm-4-flash
//	    prompt:
//	      files: [recall_agent.md]
//	    memory:
//	      type: memory
//	    max_tool_iterations: 5
type Config struct {
	// Entry specifies which agent in the Agents map is the top-level agent.
	// Defaults to "tagent" if empty.
	Entry string `json:"entry" yaml:"entry"`

	// Agents maps agent name → AgentConfig. Each agent is independently configured.
	Agents map[string]AgentConfig `json:"agents" yaml:"agents"`

	// PromptDir is the global base directory for prompt file resolution.
	// Individual agents can override this via their own PromptDir field.
	PromptDir string `json:"prompt_dir" yaml:"prompt_dir"`

	// Model is the global default model name (resolved at runtime).
	// Individual agents can override this via their own Model field.
	Model string `json:"model" yaml:"model"`

	// Provider is the global default model provider name (e.g., "openai", "anthropic").
	// Defaults to "openai" if empty. Agents can override via AgentConfig.Provider.
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`

	// Providers maps provider name → connection info (endpoint, api_key_env).
	// Each agent references a provider by name to resolve its model instance.
	// Example:
	//   providers:
	//     openai:
	//       api_endpoint: "https://open.bigmodel.cn/api/paas/v4"
	//       api_key_env: "ZAI_API_KEY"
	//     anthropic:
	//       api_endpoint: "https://api.anthropic.com"
	//       api_key_env: "ANTHROPIC_API_KEY"
	Providers map[string]ProviderConfig `json:"providers,omitempty" yaml:"providers,omitempty"`

	// ===== Runtime configuration (was in config.yaml) =====

	// APIEndpoint is the LLM API base URL (e.g., "https://open.bigmodel.cn/api/paas/v4").
	APIEndpoint string `json:"api_endpoint,omitempty" yaml:"api_endpoint,omitempty"`

	// APIKeyEnv is the environment variable name holding the API key.
	// Defaults to "ZAI_API_KEY" if empty.
	APIKeyEnv string `json:"api_key_env,omitempty" yaml:"api_key_env,omitempty"`

	// LogLevel controls framework (trpc-agent-go/log) verbosity.
	// One of: "debug", "info", "warn", "error".
	// Can be overridden by the LOG_LEVEL environment variable.
	LogLevel string `json:"log_level,omitempty" yaml:"log_level,omitempty"`

	// RequestTimeoutSeconds is the per-request timeout in seconds (0 = default 3600).
	RequestTimeoutSeconds int `json:"request_timeout_seconds,omitempty" yaml:"request_timeout_seconds,omitempty"`

	// App holds application-specific configuration (e.g., wechat bot settings).
	// Each application deserializes this into its own typed struct.
	// This keeps Config generic — no app-specific fields pollute the shared structure.
	App map[string]any `json:"app,omitempty" yaml:"app,omitempty"`

	// TrajectoryDump enables recording every LLM call to JSONL files on disk.
	// Default: false. When true, a TrajectoryRecorder wraps the model.
	TrajectoryDump bool `json:"trajectory_dump,omitempty" yaml:"trajectory_dump,omitempty"`

	// TrajectoryDir is the directory for trajectory JSONL files.
	// Default: "data/trajectories". Each session gets its own file: {dir}/{session_id}.jsonl
	TrajectoryDir string `json:"trajectory_dir,omitempty" yaml:"trajectory_dir,omitempty"`
}

// ProviderConfig holds connection info for a model provider.
// Used in Config.Providers to declare provider endpoints and credentials.
type ProviderConfig struct {
	// Provider is the protocol implementation to use (e.g., "openai", "anthropic", "gemini").
	// Most domestic models (GLM, DeepSeek, Moonshot, etc.) use OpenAI-compatible protocol,
	// so this field should be "openai" with different api_endpoint to distinguish providers.
	// Defaults to the provider registry key name if not specified.
	// e.g., "openai" for OpenAI-compatible APIs (OpenAI/ZhiPu/DeepSeek/Moonshot/Baichuan/Qwen),
	//       "anthropic" for Anthropic Claude,
	//       "gemini" for Google Gemini.
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`

	// APIEndpoint is the base URL for the provider's API.
	// e.g., "https://open.bigmodel.cn/api/paas/v4" for ZhiPu,
	//       "https://api.anthropic.com" for Anthropic.
	APIEndpoint string `json:"api_endpoint" yaml:"api_endpoint"`

	// APIKeyEnv is the environment variable name holding the API key for this provider.
	// e.g., "ZAI_API_KEY", "ANTHROPIC_API_KEY".
	APIKeyEnv string `json:"api_key_env,omitempty" yaml:"api_key_env,omitempty"`
}

// AgentConfig describes a single agent's configuration.
// Each agent only cares about itself and who it communicates with.
type AgentConfig struct {
	// Model is the LLM model name (resolved at runtime). Falls back to Config.Model.
	Model string `json:"model,omitempty" yaml:"model,omitempty"`

	// Provider overrides the global default provider for this agent.
	// References a key in Config.Providers. Falls back to Config.Provider if empty.
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`

	// PromptDir is the base directory for this agent's prompt files.
	// Falls back to Config.PromptDir.
	PromptDir string `json:"prompt_dir,omitempty" yaml:"prompt_dir,omitempty"`

	// SystemPrompt configures how to load this agent's system prompt.
	SystemPrompt PromptConfig `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"`

	// Memory configures this agent's own memory store.
	// Each agent has its own isolated storage. Defaults to in-memory store.
	Memory MemoryConfig `json:"memory,omitempty" yaml:"memory,omitempty"`

	// Tools declares which tools this agent uses.
	// Tools can reference other agents (agent kind) or plain tools (tool kind).
	Tools []ToolRef `json:"tools" yaml:"tools"`

	// Agent parameters
	MaxToolIterations int     `json:"max_tool_iterations,omitempty" yaml:"max_tool_iterations,omitempty"`
	MaxTokens         int     `json:"max_tokens,omitempty"          yaml:"max_tokens,omitempty"`
	Temperature       float64 `json:"temperature,omitempty"         yaml:"temperature,omitempty"`
	CompressThreshold float64 `json:"compress_threshold,omitempty"  yaml:"compress_threshold,omitempty"`
	KeepRecentTasks   int     `json:"keep_recent_tasks,omitempty"   yaml:"keep_recent_tasks,omitempty"`

	// TaskTerminalTTL is the grace period an exited task (completed/failed/
	// cancelled/dead) is retained before pruning, as a duration string
	// (e.g. "2m", "30m"). It bounds the resume_task window for terminal
	// subagent tasks. Empty/invalid → default "2m".
	TaskTerminalTTL string `json:"task_terminal_ttl,omitempty" yaml:"task_terminal_ttl,omitempty"`
	// ResumeContextRounds caps how many prior rounds the subagent task-chain
	// restorer injects on resume (default 3).
	ResumeContextRounds int            `json:"resume_context_rounds,omitempty" yaml:"resume_context_rounds,omitempty"`
	Compress            CompressConfig `json:"compress,omitempty" yaml:"compress,omitempty"`

	// Generation controls thinking/reasoning mode for the LLM.
	// When set, these fields are merged into model.GenerationConfig.
	ThinkingEnabled *bool   `json:"thinking_enabled,omitempty"  yaml:"thinking_enabled,omitempty"`
	ThinkingTokens  *int    `json:"thinking_tokens,omitempty"   yaml:"thinking_tokens,omitempty"`
	ReasoningEffort *string `json:"reasoning_effort,omitempty"  yaml:"reasoning_effort,omitempty"`
	// ReasoningContentMode controls how reasoning_content from history is handled.
	// "keep_all" (keep everything), "discard_previous" (default, keep current turn only),
	// "discard_all" (strip all reasoning_content).
	ReasoningContentMode string `json:"reasoning_content_mode,omitempty" yaml:"reasoning_content_mode,omitempty"`

	// Meditation configures the periodic meditation/heartbeat mechanism.
	// Only effective when the agent is started via StartLoop.
	Meditation MeditationConfig `json:"meditation,omitempty" yaml:"meditation,omitempty"`

	// WorkspaceRoot is the unified on-disk scratch root for this agent
	// (default: .tagent-workspace). Oversized tool outputs go to <root>/tool-output;
	// the tmux command working directory is <root>/exec. A periodic cleaner bounds
	// the accumulated files.
	WorkspaceRoot string `json:"workspace_root,omitempty" yaml:"workspace_root,omitempty"`

	// Description for agent.Agent interface (used when this agent is a sub-agent)
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// CompressConfig configures SmartCompressor parameters.
type CompressConfig struct {
	MaxToolResultChars int `json:"max_tool_result_chars,omitempty" yaml:"max_tool_result_chars,omitempty"`
	MaxExecStateChars  int `json:"max_exec_state_chars,omitempty"  yaml:"max_exec_state_chars,omitempty"`
	ChunkSummaryLen    int `json:"chunk_summary_len,omitempty"      yaml:"chunk_summary_len,omitempty"`

	// SkeletonSegmentation toggles agent_output-boundary skeleton compression
	// (task-skeleton-compression). Default (nil) is enabled; set false to
	// fall back to the legacy user-boundary segmentation.
	SkeletonSegmentation *bool `json:"skeleton_segmentation,omitempty" yaml:"skeleton_segmentation,omitempty"`

	// MaxNoticeChars caps the compress-notice text length (default 800).
	MaxNoticeChars int `json:"max_notice_chars,omitempty" yaml:"max_notice_chars,omitempty"`
	// CompactKeysListed caps the keys listed in the rolling compaction
	// summary (default 32); older events stay retrievable via recall.
	CompactKeysListed int `json:"compact_keys_listed,omitempty" yaml:"compact_keys_listed,omitempty"`
	// RecentFullCount is how many most-recent refs resolve with full content
	// from MemoryStore. Unset (0) derives keep_recent_tasks × 4 so the most
	// recent complete turns resolve full as a whole; explicit values win.
	RecentFullCount int `json:"recent_full_count,omitempty" yaml:"recent_full_count,omitempty"`
	// CardMaxChars caps the index-card section of the rolling compaction
	// summary (default 6000); beyond it old card lines are LLM-condensed
	// (with summary_model) or sink into an "earlier n items" counter.
	CardMaxChars int `json:"card_max_chars,omitempty" yaml:"card_max_chars,omitempty"`
	// ArchiveCacheCap bounds the per-process L3 archive cache entries
	// (default 256; the archives themselves persist in MemoryStore).
	ArchiveCacheCap int `json:"archive_cache_cap,omitempty" yaml:"archive_cache_cap,omitempty"`

	// MaxSummaryInputChars is the splitting threshold for a single summary call's
	// input (0 = package default 40000). A giant segment exceeding it is split
	// into smaller message-groups (each summarized separately); a single
	// oversized message is sent as-is (never content-truncated).
	MaxSummaryInputChars int `json:"max_summary_input_chars,omitempty" yaml:"max_summary_input_chars,omitempty"`

	// SummaryMaxTokens is the floor for the output-token budget reserved on each
	// summary LLM call (0 = package default 8192). Reasoning models spend part
	// of max_tokens on their thinking chain; too small a budget returns empty
	// Content and degrades compression. The per-call budget scales up with the
	// summary size but never below this floor.
	SummaryMaxTokens int `json:"summary_max_tokens,omitempty" yaml:"summary_max_tokens,omitempty"`

	// SummaryModel is the model name for LLM summary compression.
	// Falls back to the agent's main model if empty.
	SummaryModel string `json:"summary_model,omitempty" yaml:"summary_model,omitempty"`
	// SummaryProvider is the provider name for the summary model.
	// Falls back to the agent's provider if empty.
	SummaryProvider string `json:"summary_provider,omitempty" yaml:"summary_provider,omitempty"`
}

// MemoryConfig configures an agent's memory store.
// Each agent has its own isolated storage instance.
type MemoryConfig struct {
	// Type selects the memory store implementation:
	//   "memory"    — in-memory store (default, lost on process exit)
	//   "file"      — file-backed persistent store (requires rustviking CLI)
	//   "localfile" — file-backed persistent store (JSON file KV, no external deps)
	Type string `json:"type" yaml:"type"`

	// Path is the storage location identifier:
	//   - For "file"/"localfile" type: filesystem directory path
	//   - For "memory" type: logical store identifier — agents with the same
	//     type: memory and same path share a single InMemoryStore instance
	//   Empty value means an isolated store (no sharing).
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// ReadNamespaces lists agent names whose storage partitions this agent
	// is allowed to read. Each name is converted to a PartitionID at build time.
	// For example, recall can read tagent's events by declaring:
	//   read_namespaces: [tagent]
	// This enables cross-agent memory access across partitions.
	ReadNamespaces []string `json:"read_namespaces,omitempty" yaml:"read_namespaces,omitempty"`

	// RustVikingBinary sets the rustviking CLI binary path for "file" type stores.
	// Empty value uses "rustviking" (looked up via PATH).
	RustVikingBinary string `json:"rustviking_binary,omitempty" yaml:"rustviking_binary,omitempty"`

	// Lifecycle configures TTL / capacity-based forgetting for this store.
	// Nil keeps the built-in defaults (global TTL 7d, per-type table, 1h checks).
	Lifecycle *LifecycleConfig `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
}

// LifecycleConfig declares the forgetting policy over YAML/JSON. Unset fields
// fall back to memory.DefaultLifecycleConfig values.
type LifecycleConfig struct {
	// GlobalTTLDays is the default time-to-live in days (default: 7).
	// Negative = disable TTL entirely (no event is ever tombstoned by age).
	GlobalTTLDays *int `json:"global_ttl_days,omitempty" yaml:"global_ttl_days,omitempty"`

	// TypeTTL overrides the global TTL per event type (days).
	// Negative value = exempt (curated artifacts never expire).
	TypeTTL map[string]int `json:"type_ttl,omitempty" yaml:"type_ttl,omitempty"`

	// CheckInterval is how often the lifecycle scanner runs (e.g., "1h"). Default: "1h".
	CheckInterval string `json:"check_interval,omitempty" yaml:"check_interval,omitempty"`

	// MaxEventsPerPartition caps events per partition (0 = unlimited, default).
	MaxEventsPerPartition *int `json:"max_events_per_partition,omitempty" yaml:"max_events_per_partition,omitempty"`
}

// MeditationConfig configures the periodic meditation/heartbeat mechanism.
// Uses string durations (e.g., "30m", "2h") for YAML/JSON serialization.
// tagent.go converts these to time.Duration for agent.MeditationConfig.
type MeditationConfig struct {
	// Enabled activates the meditation ticker.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Interval is the check interval (e.g., "30m"). Default: "30m".
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`

	// MinGap is the minimum idle duration before meditation fires (e.g., "2h"). Default: "2h".
	MinGap string `json:"min_gap,omitempty" yaml:"min_gap,omitempty"`

	// PromptFile is the meditation prompt file (relative to prompt_dir). Default: "meditation.md".
	PromptFile string `json:"prompt_file,omitempty" yaml:"prompt_file,omitempty"`
}

// ExtraParam re-exports agent.ExtraParam for YAML/JSON config declaration
// (ToolRef.extra_params).
type ExtraParam = agent.ExtraParam

// ToolRef declares a tool that an agent uses.
// For agent-kind tools, the AgentID field references another AgentConfig in the Agents map.
// For tool-kind tools, the ID field identifies the plain tool factory.
type ToolRef struct {
	// Kind distinguishes agent tools from plain tools. Defaults to "agent".
	Kind ToolKind `json:"kind" yaml:"kind"`

	// AgentID references another agent in the Agents map (kind=agent).
	// The referenced agent becomes a CallableTool for this agent.
	AgentID string `json:"agent,omitempty" yaml:"agent,omitempty"`

	// ID is the tool identifier for plain tools (kind=tool).
	ID string `json:"id,omitempty" yaml:"id,omitempty"`

	// Tool description: inline or from file (relative to prompt_dir)
	Description     string `json:"description,omitempty"      yaml:"description,omitempty"`
	DescriptionFile string `json:"description_file,omitempty" yaml:"description_file,omitempty"`

	// EventParams declares which event-derived parameters this tool requires.
	// When the parent agent's LLM outputs a tool call, it includes these parameter values
	// (e.g., "event_key"). The tool wrapper then resolves them: for event_key, it fetches
	// the complete event data from the parent agent's MemStore and passes it as external
	// context to the tool agent. This prevents the LLM from breaking context isolation —
	// the LLM only outputs a numeric key, but the actual event content is resolved server-side.
	EventParams []string `json:"event_params,omitempty" yaml:"event_params,omitempty"`

	// ExtraParams declares additional routing-level parameters for agent-kind
	// tools (plan-interaction-contract D2). Each declared parameter is added to
	// the tool's InputSchema and, when present in a call, packed together with
	// request into a JSON message body passed to the sub-agent (e.g. plan's
	// action/name). Tools without extra_params keep the plain-text request
	// message unchanged.
	ExtraParams []ExtraParam `json:"extra_params,omitempty" yaml:"extra_params,omitempty"`

	// Async controls whether an agent-kind tool may run through the async task
	// layer (sync-wait window → inline result or background ack + task_settled).
	// nil/true = async allowed (default); false = always run synchronously —
	// an operator knob to reduce cognitive load on weaker models that struggle
	// with ack/notification semantics.
	Async *bool `json:"async,omitempty" yaml:"async,omitempty"`

	// NOTE: agent runtime parameters (max_tool_iterations, max_tokens,
	// temperature) are configured ONLY on the referenced agent's own
	// AgentConfig entry — a ToolRef declares the reference relationship, not
	// the agent's behavior. Earlier versions declared those fields here too,
	// but they were never wired into assembly (silently dead config that
	// contradicted the AgentConfig values); they have been removed to keep a
	// single configuration point per semantic.

	// Properties holds tool-specific configuration that each tool factory
	// deserializes into its own typed struct. This keeps ToolRef generic
	// — no tool-specific fields pollute the shared structure.
	//
	// Example (action tool):
	//
	//	properties:
	//	  workspace: /tmp/tagent-workspace
	//	  run_as_user: tagent-runner
	//	  run_as_group: tagent-runner
	Properties map[string]any `json:"properties,omitempty" yaml:"properties,omitempty"`

	// Remote declares that this agent tool is a remote A2A agent.
	// When set, tagent creates an a2aagent.A2AAgent instead of a local TagentAgent.
	// The URL is the agent card endpoint (e.g., "http://knowledge-service:8088").
	// Context is passed via RuntimeState → A2A metadata (auto-mapped by trpc framework).
	//
	// This field embodies the configuration layer separation:
	//   - tagent YAML: agent definition (model, prompt, etc.) — here
	//   - ToolRef.Remote.URL: connection info ("where is this agent?") — here
	//   - trpc Go options: communication details (A2A protocol, TransferStateKey) — internal
	Remote *RemoteConfig `json:"remote,omitempty" yaml:"remote,omitempty"`

	// Extension: custom factory path (for non-builtin tools/agents)
	Factory string `json:"factory,omitempty" yaml:"factory,omitempty"`
}

// RemoteConfig declares A2A connection info for a remote sub-agent.
// tagent YAML only declares the URL; trpc communication options
// (TransferStateKey, streaming, etc.) are derived internally by tagent.go.
type RemoteConfig struct {
	// URL is the A2A agent card endpoint (e.g., "http://knowledge-service:8088").
	// The remote agent must expose an A2A server with agent card at /.well-known/agent.json.
	URL string `json:"url" yaml:"url"`
}

// PromptConfig is an alias for prompt.CompositeConfig, providing bootstrap-style
// prompt loading aligned with nanobot's pattern (AGENTS.md, SOUL.md, USER.md, TOOLS.md).
//
// Prompt composition order: inline → files (in order) → directory scan.
type PromptConfig = prompt.CompositeConfig

// ToolKind distinguishes tool agents from plain tools.
type ToolKind string

const (
	// ToolKindAgent: TagentAgent wrapped as CallableTool.
	// Has internal React loop, system prompt, and sub-tools.
	ToolKindAgent ToolKind = "agent"

	// ToolKindTool: directly implements CallableTool.
	// Pure execution tool with no internal React loop.
	ToolKindTool ToolKind = "tool"
)

// Default values
const (
	DefaultEntry          = "tagent"
	DefaultPromptDir      = "resources/prompts"
	DefaultMaxToolIter    = 50
	DefaultMaxTokens      = 8000
	DefaultTemperature    = 0.7
	DefaultCompressThresh = 0.8

	DefaultAgentMaxToolIter = 10
	DefaultAgentMaxTokens   = 4096
	DefaultAgentTemp        = 0.3
)

// DefaultConfig returns a Config with sensible defaults and the three core agents.
func DefaultConfig() Config {
	return Config{
		Entry:     DefaultEntry,
		PromptDir: DefaultPromptDir,
		Agents: map[string]AgentConfig{
			"tagent": {
				PromptDir: DefaultPromptDir,
				SystemPrompt: PromptConfig{
					Files: []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md", "HEARTBEAT.md", "MEMORY.md"},
				},
				MaxToolIterations: DefaultMaxToolIter,
				MaxTokens:         DefaultMaxTokens,
				Temperature:       DefaultTemperature,
				CompressThreshold: DefaultCompressThresh,
				Tools: []ToolRef{
					{
						Kind:            ToolKindAgent,
						AgentID:         "knowledge",
						DescriptionFile: "knowledge_tool_desc.md",
						EventParams:     []string{"event_key"},
					},
					{
						Kind:            ToolKindAgent,
						AgentID:         "recall",
						DescriptionFile: "recall_tool_desc.md",
						EventParams:     []string{"event_key"},
					},
					{
						Kind:            ToolKindTool,
						ID:              "action",
						DescriptionFile: "action_tool_desc.md",
					},
				},
			},
			"knowledge": {
				PromptDir:         DefaultPromptDir,
				SystemPrompt:      PromptConfig{Files: []string{"knowledge_agent.md"}},
				MaxToolIterations: DefaultAgentMaxToolIter,
				MaxTokens:         DefaultAgentMaxTokens,
				Temperature:       DefaultAgentTemp,
				Tools: []ToolRef{
					{Kind: ToolKindTool, ID: "skill_search"},
					{Kind: ToolKindTool, ID: "skill_load"},
					{Kind: ToolKindTool, ID: "mcp_discover"},
					{Kind: ToolKindTool, ID: "web_search"},
					{Kind: ToolKindTool, ID: "duckduckgo_search"},
					{Kind: ToolKindTool, ID: "memory_query"},
				},
			},
			"recall": {
				PromptDir:         DefaultPromptDir,
				SystemPrompt:      PromptConfig{Files: []string{"recall_agent.md"}},
				MaxToolIterations: DefaultAgentMaxToolIter,
				MaxTokens:         DefaultAgentMaxTokens,
				Tools: []ToolRef{
					{Kind: ToolKindTool, ID: "recall_query"},
					{Kind: ToolKindTool, ID: "recall_get"},
					{Kind: ToolKindTool, ID: "recall_recent"},
					{Kind: ToolKindTool, ID: "recall_trace"},
				},
			},
		},
	}
}

// ApplyDefaults fills in zero/empty values with defaults.
func (c *Config) ApplyDefaults() {
	if c.Entry == "" {
		c.Entry = DefaultEntry
	}
	if c.PromptDir == "" {
		c.PromptDir = DefaultPromptDir
	}
	if c.APIKeyEnv == "" {
		c.APIKeyEnv = "ZAI_API_KEY"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.RequestTimeoutSeconds <= 0 {
		c.RequestTimeoutSeconds = 3600
	}
	if c.TrajectoryDir == "" {
		c.TrajectoryDir = "data/trajectories"
	}
	if c.Provider == "" {
		c.Provider = "openai"
	}

	// LOG_LEVEL env var overrides config file
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}

	for name := range c.Agents {
		ac := c.Agents[name]
		ac.applyDefaults(name, c)
		c.Agents[name] = ac
	}
}

// applyDefaults fills in zero/empty values for an AgentConfig.
func (ac *AgentConfig) applyDefaults(name string, parent *Config) {
	if ac.PromptDir == "" {
		ac.PromptDir = parent.PromptDir
	}
	if ac.MaxToolIterations <= 0 {
		// Entry agent uses larger defaults
		if name == parent.Entry {
			ac.MaxToolIterations = DefaultMaxToolIter
		} else {
			ac.MaxToolIterations = DefaultAgentMaxToolIter
		}
	}
	if ac.MaxTokens <= 0 {
		if name == parent.Entry {
			ac.MaxTokens = DefaultMaxTokens
		} else {
			ac.MaxTokens = DefaultAgentMaxTokens
		}
	}
	if ac.Temperature <= 0 {
		if name == parent.Entry {
			ac.Temperature = DefaultTemperature
		} else {
			ac.Temperature = DefaultAgentTemp
		}
	}
	if ac.CompressThreshold <= 0 && name == parent.Entry {
		ac.CompressThreshold = DefaultCompressThresh
	}
	if ac.Memory.Type == "" {
		ac.Memory.Type = "memory" // Default to in-memory store
	}

	for i := range ac.Tools {
		tr := &ac.Tools[i]
		if tr.Kind == "" {
			tr.Kind = ToolKindAgent
		}
	}
}

// Validate checks the config for errors after defaults are applied.
func (c *Config) Validate() error {
	if len(c.Agents) == 0 {
		return fmt.Errorf("tagent config: at least one agent is required")
	}

	// Validate entry agent exists
	if _, ok := c.Agents[c.Entry]; !ok {
		return fmt.Errorf("tagent config: entry agent %q not found in agents map", c.Entry)
	}

	// Validate each agent
	for name, ac := range c.Agents {
		if err := ac.validate(name); err != nil {
			return err
		}
	}

	// Validate tool agent references
	for name, ac := range c.Agents {
		for i, tr := range ac.Tools {
			if tr.Kind == ToolKindAgent && tr.AgentID != "" {
				if _, ok := c.Agents[tr.AgentID]; !ok {
					return fmt.Errorf("tagent config: agent %q tool[%d] references unknown agent %q",
						name, i, tr.AgentID)
				}
			}
		}
	}

	return nil
}

// validate checks an AgentConfig for errors.
func (ac *AgentConfig) validate(name string) error {
	seenIDs := make(map[string]bool)
	for i, tr := range ac.Tools {
		if tr.Kind == ToolKindAgent {
			if tr.AgentID == "" {
				return fmt.Errorf("agent %q: tools[%d] agent kind requires agent id", name, i)
			}
			if tr.Description == "" && tr.DescriptionFile == "" {
				return fmt.Errorf("agent %q: tool agent %q requires description or description_file", name, tr.AgentID)
			}
			if seenIDs[tr.AgentID] {
				return fmt.Errorf("agent %q: duplicate tool agent %q", name, tr.AgentID)
			}
			seenIDs[tr.AgentID] = true
		}
		if tr.Kind == ToolKindTool {
			if tr.ID == "" {
				return fmt.Errorf("agent %q: tools[%d] tool kind requires id", name, i)
			}
			if seenIDs[tr.ID] {
				return fmt.Errorf("agent %q: duplicate tool id %q", name, tr.ID)
			}
			seenIDs[tr.ID] = true
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

// APIKey returns the API key from the environment variable specified by APIKeyEnv.
func (c *Config) APIKey() string {
	return os.Getenv(c.APIKeyEnv)
}

// ResolveAgentProvider returns the resolved API endpoint and API key environment
// variable name for the given agent. It honors the agent's provider override
// (AgentConfig.Provider) and falls back to the global provider settings.
// Pass an empty agentName to resolve the global provider.
func (c *Config) ResolveAgentProvider(agentName string) (endpoint, apiKeyEnv string, err error) {
	providerName := c.Provider
	if agentName != "" {
		acfg, ok := c.Agents[agentName]
		if !ok {
			return "", "", fmt.Errorf("agent %q not found in config", agentName)
		}
		if acfg.Provider != "" {
			providerName = acfg.Provider
		}
	}

	endpoint = c.APIEndpoint
	apiKeyEnv = c.APIKeyEnv
	if pcfg, ok := c.Providers[providerName]; ok {
		if pcfg.APIEndpoint != "" {
			endpoint = pcfg.APIEndpoint
		}
		if pcfg.APIKeyEnv != "" {
			apiKeyEnv = pcfg.APIKeyEnv
		}
	}
	return endpoint, apiKeyEnv, nil
}
