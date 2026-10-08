package tagent // import "github.com/SpellingDragon/tagent"

Package tagent provides the top-level composition root for tagent applications:
it encapsulates agent instantiation and wires cross-boundary dependencies.

- Tools are usable only when both registered and declared for

本文件是组合根对配置模型的再导出面：模型实体在 config 包，此处别名与薄包装 保持既有的

delivery.go 是组合根的跨 agent 投递面：外部化冥想的产出经授权、寻址与具名拒绝，

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

VARIABLES

var ErrDeliveryBlindTarget = errors.New("tagent: delivery target partition is outside the sender's observation surface")
    ErrDeliveryBlindTarget 表示白名单目标的分区落在投递方观察面之外：投递方没观察过它。

var ErrDeliveryNotAllowed = errors.New("tagent: delivery is not authorized by the sender's deliver_to allowlist")
    ErrDeliveryNotAllowed 表示投递未获授权：目标名不在投递方的 deliver_to 白名单内，空白名单拒绝一切。

var ErrDeliveryTargetNotRunning = errors.New("tagent: delivery target's persistent loop is not running")
    ErrDeliveryTargetNotRunning 表示目标常驻但其持久循环未运行，错误携带目标名与循环状态。

var ErrUnknownDeliveryTarget = errors.New("tagent: delivery target is not in the process resident table")
    ErrUnknownDeliveryTarget 表示目标名不在本进程常驻表：寻址面只覆盖同进程装配出的 agent。

FUNCTIONS

func DefaultPromptsFS() embed.FS
    DefaultPromptsFS returns the embedded framework default prompts. The tree is
    rooted at DefaultPromptsPrefix (a prompt file is e.g. recall_tool_desc.md).

func DeliverToAgent(from *agent.TagentAgent, targetAgent, sessionID string, msg model.Message) error
    DeliverToAgent 把一条产出从投递方 agent 送进目标 agent 的 mailbox，谱系固定为 meditation。

      - 目标名必须属于投递方的 deliver_to 白名单；白名单缺省为空，拒绝一切投递。
      - 白名单目标的分区必须属于投递方观察面，盲投具名拒绝。
      - 寻址只走同进程常驻表：未知目标具名拒绝，不发生任何网络调用。
      - 目标循环未运行只返回错误：不起新 Run、不落盘等待、不静默丢弃。
      - 成功语义是已进入 mailbox；与用户输入同批时被移除是合法结局，不构成投递失败。
      - 消息自带来源头（来源 agent 与目标会话），目标无需回查即可理解来源。

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

type OrgAgentApply = org.OrgAgentApply
    OrgAgentApply 是单个 agent 在一次世代应用中的结果记录。

type OrgCloseState = org.OrgCloseState
    OrgCloseState 是关闭路径的阶段性事实。

type OrgFailure = org.OrgFailure
    OrgFailure 是一次世代失败的不可变记录：指纹、代序、原因与各 agent 的应用面。

type OrgLiveDebt = org.OrgLiveDebt
    OrgLiveDebt 是仍在旧代存活的 agent 清单（存活债）。

type OrgStatus = org.OrgStatus
    OrgStatus 是热更协调器的诊断快照。

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
