package knowledge // import "github.com/SpellingDragon/tagent/tool/knowledge"

Package knowledge 提供知识获取子 agent 及其子工具：技能检索与加载、MCP 工具发现、 web 搜索、历史知识查询。

web_search 走结构化搜索 API：返回标题/链接/摘要/媒体/发布日期，而抓取引擎 HTML 会 因对端改版而无声失效，故 API
是主用且更可靠的后端。API key 取自环境变量（变量名由 工具 api_key_env 属性配置）。降级与失败回报语义见文档。

FUNCTIONS

func BuildSubTools(cfg Config) []tool.Tool
    BuildSubTools assembles the sub-tool set for the Knowledge Agent.

func NewAgent(cfg Config) (*agent.TagentAgent, error)
    NewAgent creates a TagentAgent configured for knowledge acquisition &
    translation. The returned *agent.TagentAgent implements agent.Agent and can
    be wrapped via agenttool.NewTool() to become a CallableTool for the main
    TagentAgent.

func NewMCPDiscoverTool(toolSets []tool.ToolSet) tool.Tool
    NewMCPDiscoverTool creates a tool that discovers available MCP tools from
    a static toolset slice. Kept for the BuildSubTools compatibility path;
    the config-driven path uses NewMCPDiscoverToolWithRegistry.

func NewMCPDiscoverToolWithRegistry(reg tagenttool.MCPRegistry) tool.Tool
    NewMCPDiscoverToolWithRegistry creates a discover tool over the live MCP
    registry: the server set is read at CALL time, so runtime-registered servers
    become discoverable immediately without rebuilding any agent.

func NewMemoryQueryTool(memStore tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool
    NewMemoryQueryTool creates a tool that queries historical knowledge
    from memory. readPartitionIDs scopes the query to the agent's readable
    partitions (own namespace first + read_namespaces, injected at build time);
    on partition- isolated stores (FileSegmentStore) an empty list would scan
    nothing.

func NewSkillLoadTool(repo tagenttool.SkillRepository) tool.Tool
    NewSkillLoadTool creates a tool that loads skill content as a structured
    summary.

    - Levels: skill_search (front matter), skill_load (summary up to ~2500
    chars), command (full file). - Output stays compact and synthesizable;
    the full body is never dumped.

func NewSkillSearchTool(repo tagenttool.SkillRepository) tool.Tool
    NewSkillSearchTool creates a tool that searches the skill repository.

func NewTool(cfg Config) (tagenttool.Tool, error)
    NewTool is a convenience function that creates a KnowledgeAgent and wraps it
    as a CallableTool.

    - An empty Description with DescriptionFile set loads the text relative to
    PromptDir; both empty falls back to a built-in default. - The wrapper is a
    plain AgentToolWrapper without event_key resolution; full support comes from
    the root-package constructor.

func NewWebSearchTool() tool.CallableTool
    NewWebSearchTool creates a web_search tool with the default configuration.

func NewWebSearchToolWithConfig(cfg WebSearchConfig) tool.CallableTool
    NewWebSearchToolWithConfig creates a web_search tool with the given config.

func RegisterSubTools()
    RegisterSubTools registers all knowledge sub-tools as plain tools in the
    global tool registry. Called by tagent.RegisterBuiltinTools().

    Registered tools: - skill_search: search local skill repository -
    skill_load: load skill content with section-aware truncation - mcp_discover:
    discover available MCP tools - web_search: HTML scraping for general
    web content - duckduckgo_search: Instant Answer API for factual info -
    memory_query: query historical knowledge from memory

TYPES

type Config struct {
	// Model Required: LLM model
	Model model.Model
	// MemStore Optional: agent's own MemoryStore (if set, wired to MemoryPlugin + sub-tools)
	MemStore memory.MemoryStore
	// SkillRepo Optional: skill source
	SkillRepo tagentpkg.SkillRepository
	// MCPToolSets Optional: MCP tool sources
	MCPToolSets []tagenttool.ToolSet
	// PromptDir Optional: base directory for prompt files (default: "resources/prompts")
	PromptDir string
	// ReadPartitionIDs scopes partition-isolated queries (memory_query) to the
	// agent's readable partitions (own namespace first + read_namespaces).
	ReadPartitionIDs []int

	// Tools are the sub-tools available to this agent (e.g., skill_search, memory_query).
	// In the config-driven path, these are injected by buildAgent from the agent's
	// config tools list. If empty, BuildSubTools is called for backward compatibility.
	Tools []tagenttool.Tool

	// Prompt loading (bootstrap style)
	// Optional: overrides PromptDir + "knowledge_agent.md" if set
	Prompt PromptConfig

	// Description Tool description shown to the parent agent's LLM
	// Optional: inline description (overrides default)
	Description string
	// DescriptionFile Optional: description loaded from file (relative to PromptDir)
	DescriptionFile string

	// MaxToolIterations Optional overrides
	// Default: 5 (knowledge acquisition needs few iterations)
	MaxToolIterations int
	// MaxTokens Default: 4096
	MaxTokens int
	// Temperature Default: 0.3 (precision over creativity)
	Temperature float64
}
    Config holds configuration for creating the Knowledge Agent.

type ExecutionPlan struct {
	// Function "exec", "tmux_exec", "mcp_call"
	Function string `json:"function"`
	// Command for exec/tmux_exec
	Command string `json:"command,omitempty"`
	// MCPTool MCP tool name for mcp_call
	MCPTool string `json:"mcp_tool,omitempty"`
	// MCPArgs MCP tool arguments for mcp_call
	MCPArgs map[string]any `json:"mcp_args,omitempty"`
	// Environment variables
	Env map[string]string `json:"env,omitempty"`
	// Dir Working directory
	Dir string `json:"dir,omitempty"`
	// Timeout in seconds
	Timeout int `json:"timeout,omitempty"`
	// Description Human-readable description
	Description string `json:"description,omitempty"`
}
    ExecutionPlan describes a physical execution plan that ActionTool can
    directly run.

type KnowledgeResult struct {
	// Type "skill", "skill_content", "web", "mcp_tool", "historical_memory"
	Type string `json:"type"`
	// Title Human-readable title
	Title string `json:"title"`
	// Content Knowledge content
	Content string `json:"content"`
	// Source identifier
	Source string `json:"source,omitempty"`
	// ExecutionPlan Translated executable plan
	ExecutionPlan *ExecutionPlan `json:"execution_plan,omitempty"`
}
    KnowledgeResult represents a single piece of acquired knowledge.

type PromptConfig = prompt.CompositeConfig
    PromptConfig describes how to load a system prompt (bootstrap style).
    Re-exported from tagent root package for use by sub-packages.

type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Engine  string         `json:"engine"`
	Query   string         `json:"query"`
	Message string         `json:"message,omitempty"`
}
    SearchResponse represents the search response.

type SearchResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	// Source Media / source name (e.g. "搜狐")
	Source string `json:"source"`
}
    SearchResult represents a single search result.

type WebSearchConfig struct {
	// Endpoint is the Zhipu Web Search API URL.
	Endpoint string
	// APIKeyEnv is the environment variable holding the Zhipu API key.
	APIKeyEnv string
	// SearchEngine selects the Zhipu search engine (e.g. "search_std", "search_pro").
	SearchEngine string
	// Count is the number of results to request (Zhipu accepts 1-50).
	Count int
}
    WebSearchConfig configures the Zhipu-backed web_search tool.

func DefaultWebSearchConfig() WebSearchConfig
    DefaultWebSearchConfig returns the default configuration, using the public
    Zhipu Web Search endpoint and the ZAI_API_KEY env var shared with the zhipu
    model provider.
