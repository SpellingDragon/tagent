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

VARIABLES

var (
	// ErrResourceConflict reports the same path already open with an incompatible fingerprint.
	ErrResourceConflict = errors.New("resource conflict: path already open with an incompatible config")
	// ErrStoreLocked reports another process holding the directory's single-writer lock.
	ErrStoreLocked = errors.New("store is locked by another process (single-writer)")
	// ErrResourcePoisoned reports that a generation at this path did not confirm
	// its stop (or did not confirm its lock release), so the registry keeps a
	// poisoned entry sealing the path. The
	// seal is an explicit entry — holding the store/engine/lockfile strong
	// references and the failure result — never the accidental leak of a handle
	// nobody can observe.
	ErrResourcePoisoned = errors.New("resource poisoned: previous generation on this path did not confirm a safe stop; path sealed against new generations")
	// ErrReclaimUnconfirmed reports that construction released a
	// partially-built resource WITHOUT a confirmed reclaim. The open closure
	// wraps it so acquire must NOT free the writer lock — an unconfirmed
	// reclaim seals the path exactly like an unconfirmed worker stop.
	ErrReclaimUnconfirmed = errors.New("reclaim of partially built resource unconfirmed")
)

FUNCTIONS

func DefaultAssetPatterns() []string
    DefaultAssetPatterns 返回漂移审计的受控清单（同源真源转发，wiring 唯一入口）。

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

type AssetAuditor struct {
	// Has unexported fields.
}
    AssetAuditor 周期扫描认知资产并比对基线；漂移经 report 回调入事实链。 report 为 nil
    时仅日志（降级安全）。并发约定：scanAndReport 由 ticker 与启动 路径先后调用，内部以 mu 串行化快照读写。

func NewAssetAuditor(wd string, patterns, extraFiles []string, report func([]AssetChange)) *AssetAuditor
    NewAssetAuditor 构造审计器。wd 为空回退进程 cwd；patterns 为受控清单
    （evolution.DefaultProtectedPaths 同源传入）；extraFiles 是清单外补充文件 （主配置
    ConfigPath，可空）。interval<=0 时使用 assetAuditInterval。

func (a *AssetAuditor) Close() error
    Close 停止后台循环并同步等待其完全退出（幂等）。Close 返回后保证无任何快照写入， 避免调用方的资源清理（如
    t.TempDir）与尾随写竞态。

func (a *AssetAuditor) Start() error
    Start 起后台循环：先做一次启动比对（上一代快照 → 漂移事件 → 新基线；无历史 静默建基线），随后进入周期 ticker。初始比对放在
    goroutine 内——审计不得阻塞 agent 构造路径（文件哈希是有界 I/O，不该串进装配关键路径）。返回 error 仅用于
    保留签名兼容（当前不产生）。

type AssetChange struct {
	File      string
	OldHash   string
	NewHash   string
	Size      int64
	Timestamp int64
}
    AssetChange 是一次漂移的最小证据单元：旧/新内容指纹 + 检测时刻（unix ms）。 OldHash 为空表示新增，NewHash
    为空表示删除。

func DiffAssetSnapshots(prev, cur map[string]FileEntry) []AssetChange
    DiffAssetSnapshots 是纯函数比对引擎：内容 hash 不同=变更；新增/删除文件也算 变更（OldHash/NewHash
    留空表意）。顺序稳定（按文件名字典序），事件可复现。

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

type ConsolidationHintTracker struct {
	// Has unexported fields.
}
    ConsolidationHintTracker 是 per-agent 的巩固容量触发器（并发安全）。 消费 engineBridge
    的写入旁路计数（CapacityHookProvider）： 每分区的**边界事件**（external_input /
    agent_output，即任务回合的意图与产出）计数 超过 capacity_threshold 时，经 onHint 发一条
    consolidation_hint 渗透消息——建议式， 执行权仍在 LLM + memory_consolidate 工具。snooze
    窗内不重复打扰 （内存态；重启后重新积累——最多多提示一次，可接受）。

    不变量（容量观察真源）：本 tracker 的 counts 是**建议式 delta**，仅供 LLM 提示， MUST NOT
    驱动容量淘汰——淘汰执行权的唯一真源是 store 的绝对 per-partition eventCount（`recomputePartition`
    由完整记录链得出，unknown 分区不淘汰，见 memory/lifecycle.go::checkCapacity）。因此本 delta
    重启归零、巩固后随提示复位（Track 触发 onHint 即将 counts[pid]=0），与绝对真源分叉不构成淘汰误删风险（既有
    TestCapacityHint_TriggerAndSnooze 锁定提示即复位、非边界不计数；锁定淘汰读绝对）。 repaired/already
    重放也不经此处二次增量——engineBridge.ReplayEvent 对 Already 跳过 capacityHook（见
    engine_bridge_idempotency_test.go）。

func NewConsolidationHintTracker(threshold int, snooze time.Duration) *ConsolidationHintTracker
    NewConsolidationHintTracker 构造触发器。threshold<=0 返回 nil（关闭，零行为变化）。 onHint
    可后设（SetOnHint）——装配期 agent 尚未构造。

func (t *ConsolidationHintTracker) CandidatesText(partitionID int) string
    CandidatesText渲染该分区的可巩固候选段（冥想 digest 附加）。无候选返回空串（digest 不变）。建议式：仅列 key
    与计数，执行权在 LLM。

func (t *ConsolidationHintTracker) SetOnHint(fn func(partitionID, count int))
    SetOnHint 回填提示回调（装配期，NewTagentAgent 之后）。

func (t *ConsolidationHintTracker) Track(eventKey int64, partitionID int, eventType string)
    Track 是写入旁路计数入口（engineBridge capacityHook 签名）。仅边界事件计数； 非阻塞、永不失败（旁路产物）。
    Within the snooze window the count is kept, so the next boundary event
    after the window expires hints again. While onHint is unset (the assembly
    window between construction and SetOnHint) nothing is reset and no snooze
    is recorded: the count survives, and the first boundary event after wiring
    emits the delayed hint. On a hint, counts and recent reset together so the
    candidate list stays aligned.

type EmbeddingConfig = config.EmbeddingConfig
    EmbeddingConfig 别名：嵌入供应商声明。

type EvolutionConfig = config.EvolutionConfig
    EvolutionConfig 别名：自进化子系统的声明面。

type ExtraParam = config.ExtraParam
    ExtraParam 别名：路由级附加参数。

type FileEntry struct {
	Hash  string `json:"hash"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}
    FileEntry 是单个资产文件的内容指纹。

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

type RuntimeResources struct {
	// Has unexported fields.
}
    RuntimeResources is the owner registry for shared persistent stores
    (concurrency-safe). One entry per (kind, canonical path); every consumer
    acquires a lease, and the LAST lease release closes the store and frees
    the directory lock, so the next New gets a genuinely reopened instance.
    Incompatible fingerprints on the same path are rejected — never a second
    writer, never silent first-config-wins. A cross-process flock on a lockfile
    inside the directory enforces single-writer. Isolated stores (empty path)
    bypass the registry entirely: each New owns its instance exclusively.

func NewRuntimeResources() *RuntimeResources
    NewRuntimeResources creates an empty registry. The process-wide default is
    defaultResources; tests may inject isolated registries.

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
