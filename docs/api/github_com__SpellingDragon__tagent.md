package tagent // import "github.com/SpellingDragon/tagent"

Package tagent provides the top-level composition root for tagent applications:
it encapsulates agent instantiation and wires cross-boundary dependencies.

- Tools are usable only when both registered and declared for

本文件是组合根对配置模型的再导出面：模型实体在 config 包，此处别名与薄包装 保持既有的

Package tagent — ToolRegistry wraps the global tool registration maps from
agent/tool_agent.go and provides a unified interface for:
  - Registering built-in tools (exec + knowledge/recall sub-tools)
  - Querying factories by ID
  - Validating that config-referenced tools are registered

Package tagent provides the top-level composition root: it encapsulates agent
instantiation, assembles a TagentAgent from declarative Config and injects
runtime dependencies via Options.

- Dependency direction is one-way and cycle-free; the root points

testing.go provides exported helpers for integration tests in tests/. They
expose internal APIs for comprehensive testing; production code must not depend

CONSTANTS

const DefaultPromptsPrefix = "resources/prompts"
    DefaultPromptsPrefix is the path prefix under which the embedded defaults
    live.

const ToolKindAgent = config.ToolKindAgent
    ToolKindAgent 常量别名：TagentAgent 包装为 CallableTool 的工具引用种类。

const ToolKindTool = config.ToolKindTool
    ToolKindTool 常量别名：直接实现 CallableTool 的工具引用种类。

FUNCTIONS

func DefaultPromptsFS() embed.FS
    DefaultPromptsFS returns the embedded framework default prompts. The tree is
    rooted at DefaultPromptsPrefix (a prompt file is e.g. recall_tool_desc.md).

func New(cfg Config, opts ...Option) (*agent.TagentAgent, error)
    New creates a fully-wired TagentAgent from declarative Config plus runtime
    Options.

    - It handles every cross-boundary wiring internally: builtin tool
    registration, validation that configured tools are registered, entry-agent
    resolution, per-agent MemoryStore creation, per-agent buildAgent assembly,

func RegisterBuiltinTools() error
    RegisterBuiltinTools registers all built-in tools into the ToolRegistry.

    - Called once before config validation; sync.Once makes repeated calls
    safe. - Registered plain tools: exec; the file sub-tools (read_file,
    save_file, list_file, search_file, search_content, read_multiple_files,
    replace_content); the knowledge sub-tools (skill_search, skill_load,
    mcp_discover, web_search, duckduckgo_search, memory_query); the recall
    sub-tools (recall_query, recall_get, recall_recent, recall_trace); mcp_call;
    and the memory curation sub-tools memory_consolidate and memory_health,
    whose dependencies come from the per-agent MemStore.

func TestingBuildAgent(
	name string,
	acfg AgentConfig,
	cfg Config,
	m model.Model,
	skillRepo tagenttool.SkillRepository,
	mcpToolSets []trpctool.ToolSet,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
) (*agent.TagentAgent, error)
    TestingBuildAgent creates a TagentAgent using the internal build pipeline.
    Test-only — do NOT use in production code.

TYPES

type AgentConfig = config.AgentConfig
    AgentConfig 别名：单个 agent 的声明（模型/记忆/工具/prompt）。

type CompressConfig = config.CompressConfig
    CompressConfig 别名：上下文压缩声明。

type Config = config.Config
    Config 是整份 tagent 编排声明的根类型（组合根在此别名再导出，实体在 config 包）。

func DefaultConfig() Config
    DefaultConfig 返回内置默认编排声明（经 config 包实体）。

func LoadConfig(path string) (*Config, error)
    LoadConfig 从 YAML 路径装载并校验整份编排声明（经 config 包实体）。

type ConsolidationConfig = config.ConsolidationConfig
    ConsolidationConfig 别名：事件整理声明。

type EmbeddingConfig = config.EmbeddingConfig
    EmbeddingConfig 别名：嵌入供应商声明。

type EvolutionConfig = config.EvolutionConfig
    EvolutionConfig 别名：自进化子系统的声明面。

type ExtraParam = config.ExtraParam
    ExtraParam 别名：路由级附加参数。

type GovernanceConfig = config.GovernanceConfig
    GovernanceConfig 别名：治理子系统的声明面。

type LifecycleConfig = config.LifecycleConfig
    LifecycleConfig 别名：TTL 遗忘曲线声明。

type MCPServerConfig = config.MCPServerConfig
    MCPServerConfig 别名：单台 MCP server 的连接声明。

type MeditationConfig = config.MeditationConfig
    MeditationConfig 别名：冥想回合声明。

type MemoryConfig = config.MemoryConfig
    MemoryConfig 别名：记忆与存储声明。

type MemoryEngineConfig = config.MemoryEngineConfig
    MemoryEngineConfig 别名：记忆引擎选择与参数。

type ModelRef = config.ModelRef
    ModelRef 别名：统一模型引用（provider/model/effort）。

type Option func(*runtimeConfig)
    Option injects runtime-only dependencies that cannot be serialized.

func WithApprovalChannel(ch governance.ApprovalChannel) Option
    WithApprovalChannel 注入外部审批送达通道——审批请求经 Deliver 渠道直投（如微信 SendTextToUser），不依赖
    agent 转述。evolution/governance 未启用时为 no-op。可多次调用（多通道尽力投递）。

func WithConfigPath(path string) Option
    WithConfigPath records the on-disk path the Config was loaded from
    (agent-config-hot-reload, incremental A). When set, the entry agent arms
    a lazy org-config watcher: before each LLM call it stats the file and on
    change re-parses + fingerprints the org whitelist subset — migratable
    numeric params (compress_threshold) are hot-applied; structural diffs are
    logged as restart-required until snapshot rebuild (incremental B).

func WithMCPToolSets(ts []trpctool.ToolSet) Option
    WithMCPToolSets injects pre-built MCP toolsets. They are merged into the
    process-level MCP registry under their Name() (alongside YAML-declared
    mcp_servers), becoming visible to mcp_discover/mcp_call immediately.

func WithModel(m model.Model) Option
    WithModel sets the resolved model instance (required). This is the default
    model; individual agents can override via AgentConfig.Model.

func WithModelOverrides(overrides map[string]model.Model) Option
    WithModelOverrides injects pre-resolved model instances for specific agents.
    This supports scenarios like SwappableModel for entry agent (AReaL proxy).
    The map key is the agent name, the value is the model instance to use.

func WithSkillRepo(sr tool.SkillRepository) Option
    WithSkillRepo sets the skill repository for knowledge agent.

type OrgAgentApply struct {
	Name            string  `json:"name"`
	Outcome         string  `json:"outcome"`
	ThresholdPct    float64 `json:"thresholdPct,omitempty"`
	MaxTokens       int     `json:"maxTokens,omitempty"`
	KeepRecentTasks int     `json:"keepRecentTasks,omitempty"`
}
    OrgAgentApply 是一个 agent 在本轮数值热更中的回执，Outcome 只有两种真实结果。

    - applied：它属于本代可路由拓扑，参数已下发到它的真实对象。 - draining：本代不路由它（被移除或已降级为旧
    owner），不碰它，数值字段保持零值，含义是本轮未评估。

type OrgCloseState struct {
	Initiated       bool `json:"initiated"`
	ResourcesExited bool `json:"resourcesExited"`
}
    OrgCloseState keeps 「关闭已发起」and「资源已退出」as the two distinct facts they are.
    Initiated flips on the first Close call; ResourcesExited turns true only
    once every deferred exit has run — or immediately when nothing was ever
    deferred (an inline close that took every exit is not "incomplete").
    Collapsing the two would let a bounded return be read as a finished
    teardown, which is exactly the misreading refused.

type OrgFailure struct {
	// Generation 失败时所在代（候选被拒，代不前进）
	Generation int `json:"generation"`
	// Desired 被拒候选的 desired 指纹前缀（可为空）
	Desired string    `json:"desired"`
	Error   string    `json:"error"`
	At      time.Time `json:"at"`
}
    OrgFailure / OrgStatus 是代际诊断的有界形状（D9：成功与失败给同一语义检查的 明确可诊断结果，不新增抓取协议、不新增历史）。

    Fingerprint/Desired 只能当**不透明诊断标签**：按 resident-continuity 的声明， 指纹/序号不是应用可见
    identity，任何执行路径不得据它选版（本仓也无这样的 读取点）。Desired 是“磁盘上那份配置算出来的指纹”：它与 Fingerprint
    不等才是 运维真正要看到的事实（“我改了，为什么没生效”）；解析/指纹本身失败时为空。

type OrgLiveDebt struct {
	CapturedAt         time.Time          `json:"capturedAt"`
	Executors          agent.ExecutorRefs `json:"executors"`
	PendingRetirements []map[string]any   `json:"pendingRetirements"`
}
    OrgLiveDebt is the LIVE half of the diagnostics payload. Unlike OrgStatus
    — which one `coord.status()` call produces as one atomic read — the figures
    below are read off the running reference accounting AT THE MOMENT the
    payload was asked for, so they describe "debt right now", not "what the
    committed record says". They are therefore grouped apart from the record
    fields and carry their own capture instant, which is what lets a reader tell
    the two kinds apart instead of mistaking a stitched-together view for an
    atomic success snapshot. No execution path reads any of it.

type OrgStatus struct {
	Generation int64 `json:"generation"`
	// Revision 完整应用计数（含 numeric-only），非路由源
	Revision    int64  `json:"revision"`
	Fingerprint string `json:"fingerprint"`
	Desired     string `json:"desired"`
	// LastApplied 完整配置最近成功应用时间（含 numeric-only）
	LastApplied time.Time `json:"lastAppliedAt"`
	// LastPublished 最近结构发布／回滚时间
	LastPublished time.Time   `json:"lastPublishedAt"`
	LastFailure   *OrgFailure `json:"lastFailure,omitempty"`
	// Agents 本轮逐 agent 回执（整块替换，无历史）
	Agents []OrgAgentApply `json:"agents"`
}
    OrgStatus 是一次原子读出的代际诊断快照：已发布代、两个时间戳、最后一次被拒候选， 以及本轮逐 agent 回执。

type PromptConfig = config.PromptConfig
    PromptConfig 别名：组合式 prompt 声明。

type ProviderConfig = config.ProviderConfig
    ProviderConfig 别名：模型供应商端点声明。

type ReliabilityConfig = config.ReliabilityConfig
    ReliabilityConfig 别名：可靠投递与总线落盘的声明面。

type RemoteConfig = config.RemoteConfig
    RemoteConfig 别名：A2A 远端连接声明。

type ToolKind = config.ToolKind
    ToolKind 别名：工具引用的种类。

type ToolRef = config.ToolRef
    ToolRef 别名：agent 的工具引用声明。

type ToolRegistry struct{}
    ToolRegistry is a facade over the agent package's global tool registration
    maps. It provides a unified entry point for tool registration, lookup,
    and validation.

    The actual factory maps live in agent/tool_agent.go as package-level
    variables. ToolRegistry delegates to those maps so callers can register
    tools via either the ToolRegistry API or agent.RegisterPlainTool /
    agent.RegisterToolAgent directly.

func GetRegistry() *ToolRegistry
    GetRegistry returns the global ToolRegistry singleton.

func (r *ToolRegistry) GetPlainToolFactory(id string) (agent.PlainToolFactory, bool)
    GetPlainToolFactory returns the factory for the given plain tool ID.

func (r *ToolRegistry) GetToolAgentFactory(id string) (agent.ToolAgentFactory, bool)
    GetToolAgentFactory returns the factory for the given tool agent ID.

func (r *ToolRegistry) RegisterPlainTool(id string, factory agent.PlainToolFactory)
    RegisterPlainTool registers a plain tool factory. Delegates to
    agent.RegisterPlainTool.

func (r *ToolRegistry) RegisterToolAgent(id string, factory agent.ToolAgentFactory)
    RegisterToolAgent registers a tool agent factory. Delegates to
    agent.RegisterToolAgent.

func (r *ToolRegistry) ValidateToolAccess(cfg *Config) error
    ValidateToolAccess checks that all config-referenced plain tools (kind:
    tool) are registered in the ToolRegistry. Returns an error on the first
    unregistered tool.

    Agent-kind tools (kind: agent) are not checked here — they reference
    other agents in the Config.Agents map, which is validated separately in
    Config.Validate().
