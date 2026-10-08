// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/internal/strictyaml"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
)

// Config is the top-level tagent configuration: declarative, serializable
// (YAML/JSON), with runtime-only dependencies injected via Option functions.
//
// - Agent-centric design: each agent describes its own settings (model, memory, tools) and its communication intent; the top level holds the table keyed by agent name.
type Config struct {
	// XAnchors is an extension-reserved key ("x-" convention): YAML anchors
	// shared across the file are declared under it and ignored by the
	// framework. Declared so strict parsing accepts the convention
	//; the framework never reads it.
	XAnchors map[string]any `json:"x-anchors,omitempty" yaml:"x-anchors,omitempty"`

	// Entry specifies which agent in the Agents map is the top-level agent.
	// Defaults to "tagent" if empty.
	Entry string `json:"entry" yaml:"entry"`

	// ResidentMetaDir：常驻会话元数据目录
	//（默认 $TMPDIR/tagent-resident-meta；指向持久卷可跨机器重启审计/TTL sweep）。
	ResidentMetaDir string `json:"resident_meta_dir" yaml:"resident_meta_dir"`

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

	// MCPServers maps server name → MCP connection declaration. Servers are
	// loaded into the process-level MCP registry consumed by mcp_discover /
	// mcp_call. Editing this section in the
	// config file hot-syncs the registry (lazy mtime check) — no restart,
	// and no agent tool declaration changes (prompt prefix stays stable).
	MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty" yaml:"mcp_servers,omitempty"`

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

	// TrajectoryCapture 是 v2 SDK 请求采集块（trajectory_capture）的声明面。
	// 默认零值 = 关闭（旧录制路径逐字节现状不变）；enabled=true 时要求
	// trajectory_dump=true —— v2 层只长在会写轨迹的 recorder 之上（Validate 拒绝矛盾）。
	// 限额数值一律不在 config 复制：零值原样留给 rl 侧取默认（构造入口
	// rl.NewTrajectoryRecorderWithOptions + rl.WithCapture(cfg)，其内部 normalize 填默认；
	// 真源：rl.DefaultCaptureMaxRecordBytes、DefaultCaptureMaxPendingBytes、
	// DefaultCaptureMaxRunBytes、MaxCaptureOpenFiles，队列深度保持 256 不进配置面）。
	// 构造期配置，不假称热更。
	TrajectoryCapture CaptureBlock `json:"trajectory_capture,omitempty" yaml:"trajectory_capture,omitempty"`

	// WorkingDir 是 agent 的统一工作根目录 —— file tools 的 base_dir 与 exec 命令的 cwd 的共同基准。
	// 空(默认)= 继承进程工作目录(现状逐字节不变);非空则 file/exec 的相对路径均以此为根,且二者
	// 始终一致(保持模型"单一文件系统视图",见 workspace.go / action_tool.go 设计约定:分裂 base 会致
	// list_file 结果从 exec 不可达 + 路径幻觉)。典型用途:设为项目 clone 根(如 /home/user/codes),
	// 使 agent 能操作该目录下所有仓库;tagent 自身的配置/资源/数据路径**不受影响**(仍相对进程 cwd)。
	// 优先级:ToolRef.properties(base_dir/workspace) > WorkingDir > 进程 cwd。
	// 可经环境变量 TAGENT_WORKING_DIR 覆盖(部署时灵活指定 clone 根,免改 yaml;见 ApplyDefaults)。
	WorkingDir string `json:"working_dir,omitempty" yaml:"working_dir,omitempty"`

	// ConfigPath records the file this Config was loaded from (set by
	// LoadConfig; empty for programmatically constructed configs). It binds
	// the MCP registry's mcp_servers hot-sync to the source file.
	ConfigPath string `json:"-" yaml:"-"`

	// Governance 配置有界自治与审计（T-G）。默认零值 = 关闭（Enabled=false → 全放行，
	// 现状零行为变化）。开启后经 GovernanceGate 对工具调用做风险分级 + 预算 + goal + critical 批准。
	Governance GovernanceConfig `json:"governance,omitempty" yaml:"governance,omitempty"`

	// Evolution 配置 git 原生自进化。默认零值 = 关闭（现状）。
	// 开启后文件即真源（热重载直生效）+ refine register 登记（[self-improve] commit+评估窗口）。
	Evolution EvolutionConfig `json:"evolution,omitempty" yaml:"evolution,omitempty"`

	// Reliability 配置常驻可靠性（T-G）。默认零值 = 关闭（纯 channel bus，现状）。
	Reliability ReliabilityConfig `json:"reliability,omitempty" yaml:"reliability,omitempty"`
}

// GovernanceConfig 是 T-G 治理子系统的配置（映射到 governance.GateConfig + 各管理器）。
type GovernanceConfig struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Enforcement string `json:"enforcement,omitempty" yaml:"enforcement,omitempty"`
	// Dir 是 budget 与 approval 记录的持久化目录；空 = 纯内存（进程重启即失）。
	Dir string `json:"dir,omitempty" yaml:"dir,omitempty"`

	BudgetWindowMinutes int `json:"budget_window_minutes,omitempty" yaml:"budget_window_minutes,omitempty"`
	MaxHighRisk         int `json:"max_high_risk,omitempty" yaml:"max_high_risk,omitempty"`
	MaxMediumRisk       int `json:"max_medium_risk,omitempty" yaml:"max_medium_risk,omitempty"`

	// GoalRequiredFor 列出必须挂 active goal 才允许执行的 trigger source；空 = 不启用该门。
	GoalRequiredFor []string `json:"goal_required_for,omitempty" yaml:"goal_required_for,omitempty"`
}

// EvolutionConfig 是 git 原生自进化配置（bundle/发布道已退役，
// 文件即真源+git 版本层+建议式评估）。
type EvolutionConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
	// ProtectedPaths 是 refine register 的受控路径 patterns（段匹配：`**` 任意段序列/`*` 段内通配）。
	// 默认三目录：resources/prompts/**, skills/**, scripts/**（scripts 缺失则冥想脚本产物断链）。
	ProtectedPaths []string `json:"protected_paths,omitempty" yaml:"protected_paths,omitempty"`
	// JudgeDelaySeconds 是 register 后到评估的延迟窗，承接 canary_hold 的语义（0=立即评估）。
	JudgeDelaySeconds int `json:"judge_delay_seconds,omitempty" yaml:"judge_delay_seconds,omitempty"`

	// MaxDenialRate 与同组的 MaxCriticalRate、MaxNegFBRate 是 Guardrail 的三道独立阈值：
	// 每门单独可配，零值走 GuardrailConfig 默认，MaxNegFBRate 取负值表示显式禁用负反馈判据。
	MaxDenialRate   float64 `json:"max_denial_rate,omitempty" yaml:"max_denial_rate,omitempty"`
	MaxCriticalRate float64 `json:"max_critical_rate,omitempty" yaml:"max_critical_rate,omitempty"`
	MaxNegFBRate    float64 `json:"max_neg_fb_rate,omitempty" yaml:"max_neg_fb_rate,omitempty"`

	// Judge is the unified ModelRef for the evolution judge LLM; a zero value falls back
	// to the entry agent’s model. The judge’s judgment knobs and their zero-value
	// defaults are the constructor’s contract (evolution.NewLLMJudgeEvaluator).
	Judge ModelRef `json:"judge,omitempty" yaml:"judge,omitempty"`
	// JudgeModel 与同组五项平面判官参数（JudgeProvider、JudgeReasoningEffort、JudgeMinSamples、
	// JudgePassThreshold、JudgeTimeoutSeconds）是 Judge 的兼容别名，取值折入 Judge。
	// Deprecated: 平面字段只保留读取兼容。
	JudgeModel           string  `json:"judge_model,omitempty" yaml:"judge_model,omitempty"`
	JudgeProvider        string  `json:"judge_provider,omitempty" yaml:"judge_provider,omitempty"`
	JudgeReasoningEffort string  `json:"judge_reasoning_effort,omitempty" yaml:"judge_reasoning_effort,omitempty"`
	JudgeMinSamples      int     `json:"judge_min_samples,omitempty" yaml:"judge_min_samples,omitempty"`
	JudgePassThreshold   float64 `json:"judge_pass_threshold,omitempty" yaml:"judge_pass_threshold,omitempty"`
	JudgeTimeoutSeconds  int     `json:"judge_timeout_seconds,omitempty" yaml:"judge_timeout_seconds,omitempty"`
}

// ReliabilityConfig 是 T-G 常驻可靠性配置（映射到 agent EventBus 的耐久受理层）。
type ReliabilityConfig struct {
	// BusSpillDir 是事件总线 durable inbox 的根目录。非空即启用 ReliableBus：每次 Publish
	// 在返回回执之前先落 inbox（全量持久受理，与队列忙闲无关），at-least-once，常驻不丢
	// 事件，重启可回收。空 = 纯 volatile 内存 channel，队列满不得冒充 accepted。每 agent
	// 用其下子目录（<BusSpillDir>/<agentName>）隔离。建议置于 workspace 下。
	BusSpillDir string `json:"bus_spill_dir,omitempty" yaml:"bus_spill_dir,omitempty"`

	// MeditationAnchorDir 是冥想门控锚点持久化根目录（非空启用 AnchorStore：跨重启保留
	// novelty/idle/last-meditation 三锚点，重启后不立即误触发冥想）。空 = 纯内存（现状）。
	// 每 agent 用 <MeditationAnchorDir>/<agentName>.json。
	MeditationAnchorDir string `json:"meditation_anchor_dir,omitempty" yaml:"meditation_anchor_dir,omitempty"`

	// DegradationEnabled 启用五依赖退化状态机（T-G DegradationManager，报告 D3）：
	// ErrorTrackingStore 最外层捕获存储错误(memory/disk/rustviking)+event_loop 捕获 model 失败，
	// 达阈值→degraded、探测成功→recovering→normal，状态迁移写 governance degraded 事件（可观测）。
	// 默认 false = 不启用（ErrorTrackingStore 不包裹，现状逐字节零行为变化）。
	DegradationEnabled bool `json:"degradation_enabled,omitempty" yaml:"degradation_enabled,omitempty"`

	// DegradationModelBackoff：model 依赖 degraded 时
	// runEventLoop 在下一 turn 前的退避停顿（duration 字符串，如 "5s"）。空/非法 = 关闭
	// （零行为变化）。警告级「闸不是墙」——退避只为免打已确认故障的端点，恢复即正常。
	DegradationModelBackoff string `json:"degradation_model_backoff,omitempty" yaml:"degradation_model_backoff,omitempty"`

	// DegradationMCPProbeEvery：DepMCP degraded 时 mcp_call 的熔断半开探测间隔——
	// 每 N 次调用放行 1 次真探测，其余直接返回熔断 result（含自纠材料）。0 = 关闭熔断。
	DegradationMCPProbeEvery int `json:"degradation_mcp_probe_every,omitempty" yaml:"degradation_mcp_probe_every,omitempty"`

	// DegradationDiskBlockSpawn：DepDisk degraded 时拒绝新任务 spawn（返回可读
	// 原因；进行中任务的 settle/轮询不受影响）。默认 false = 不拒绝。
	DegradationDiskBlockSpawn bool `json:"degradation_disk_block_spawn,omitempty" yaml:"degradation_disk_block_spawn,omitempty"`

	// MemSpillDir 是 memory 退化事件兜底目录（报告 D3 步4）：DegradationEnabled 且此非空时，
	// StoreEvent 失败的事件落 <MemSpillDir>/<agent>.jsonl，memory 恢复后自动重放（事件不丢，
	// at-least-once 延伸到存储层）。空 = 仅退化状态标记、不落盘兜底。建议置于 workspace 下。
	MemSpillDir string `json:"mem_spill_dir,omitempty" yaml:"mem_spill_dir,omitempty"`
}

// CaptureBlock 是 trajectory_capture 的声明块。字段面与 rl.CaptureConfig 同名同形，
// 但刻意不做类型别名：别名会把 queue_size 一并放进配置文件，而 S3 定的是「队列继续
// 256」——采集队列不是配置项。config 只声明与校验语义非法值，默认值的真源在 rl。
type CaptureBlock struct {
	// Enabled 打开 v2 SDK 请求采集；true 要求 trajectory_dump=true。
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// MaxRecordBytes 限定单条序列化记录上界；0 = rl 默认。
	MaxRecordBytes int64 `json:"max_record_bytes,omitempty" yaml:"max_record_bytes,omitempty"`
	// MaxPendingBytes 限定采集拥有的在途总额（在途副本+响应累积+排队记录+序列化缓冲）；0 = rl 默认。
	MaxPendingBytes int64 `json:"max_pending_bytes,omitempty" yaml:"max_pending_bytes,omitempty"`
	// MaxRunBytes 限定单次 capture 的落盘总量；0 = rl 默认。
	MaxRunBytes int64 `json:"max_run_bytes,omitempty" yaml:"max_run_bytes,omitempty"`
	// MaxOpenFiles 限定同时打开的采集文件数；0 = rl 默认。
	// 装配面显式拒绝 > MaxCaptureOpenFiles 的声明（不静默夹紧）：rl.normalize 的夹紧只兜
	// 「库内直用」，配置里写了更大的数必须启动即错，否则用户会以为拿到了更大的限额。
	MaxOpenFiles int `json:"max_open_files,omitempty" yaml:"max_open_files,omitempty"`
}

// MaxCaptureOpenFiles 是声明面接受的同时打开采集文件数上限。引用 rl 的常数而非另写
// 一个数字——上限的真源仍只有一处。
const MaxCaptureOpenFiles = rl.MaxCaptureOpenFiles

// ErrCaptureRequiresDump and its siblings are the config surface's named refusal
// sentinels: an illegal value under a new key fails startup outright, the message
// names the yaml path and the actual value, and callers can errors.Is the sentinel.
var (
	// ErrCaptureRequiresDump: trajectory_capture.enabled 却没有 trajectory_dump。
	ErrCaptureRequiresDump = errors.New("trajectory_capture.enabled requires trajectory_dump=true")
	// ErrCaptureNegativeLimit: 任一采集限额为负——负数从不被解释成「不限」。
	ErrCaptureNegativeLimit = errors.New("trajectory_capture limit must not be negative")
	// ErrCaptureOpenFilesExceeded: max_open_files 超过上限——显式拒绝，不夹紧。
	ErrCaptureOpenFilesExceeded = errors.New("trajectory_capture.max_open_files exceeds the supported ceiling; rejected, not clamped")
	// ErrSummaryTimeoutNegative: compress.summary_timeout_seconds 为负——它不等于「无时限」。
	ErrSummaryTimeoutNegative = errors.New("compress.summary_timeout_seconds must not be negative")
	// ErrSummaryTimeoutTooLarge: 超上限（含 0 以外的正值域）显式拒绝不夹紧——上限存在的
	// 理由是约束模型路径内最坏等待，静默夹紧会让越界拼写看起来像生效配置。
	ErrSummaryTimeoutTooLarge = errors.New("compress.summary_timeout_seconds exceeds the supported ceiling")
	// MaxSummaryTimeoutSeconds 是同步摘要时限的配置上限（D14：正值 ≤120）。
	MaxSummaryTimeoutSeconds = 120
)

// MCPServerConfig declares one MCP server connection (top-level mcp_servers).
// Alias of tool/mcp.ServerConfig so the MCP registry's config hot-sync
// re-parses the same shape without importing the root package.
type MCPServerConfig = toolmcp.ServerConfig

// ProviderConfig holds connection info for a model provider.
// Used in Config.Providers to declare provider endpoints and credentials.
type ProviderConfig struct {
	// Provider is the protocol implementation to use (e.g., "openai", "anthropic", "gemini").
	// Most domestic models (GLM, DeepSeek, Moonshot, etc.) use OpenAI-compatible protocol,
	// so this field should be "openai" with different api_endpoint to distinguish providers.
	// Defaults to the provider registry key name if not specified.
	// e.g., "openai" for OpenAI-compatible APIs (OpenAI, ZhiPu, DeepSeek, Moonshot,
	//       Baichuan, Qwen, Tencent TokenHub),
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

	// MaxToolIterations 是该 agent 单轮允许的工具迭代上限；同组的 MaxTokens、Temperature、
	// CompressThreshold、KeepRecentTasks 一并构成该 agent 自身的运行参数。
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

	// TaskDefaultTTL is the unified reaper's fallback absolute lifetime as a
	// duration string (e.g. "30m", "2h"): applied to any task whose model-side
	// `ttl` is unset, so no task is immortal (async-task-lifetime 10.5). Empty →
	// the task package default (10m). There is no way to disable age reclaim.
	TaskDefaultTTL string `json:"task_default_ttl,omitempty" yaml:"task_default_ttl,omitempty"`
	// ResumeContextRounds caps how many prior rounds the subagent task-chain
	// restorer injects on resume (default 3).
	ResumeContextRounds int            `json:"resume_context_rounds,omitempty" yaml:"resume_context_rounds,omitempty"`
	Compress            CompressConfig `json:"compress,omitempty" yaml:"compress,omitempty"`

	// ThinkingEnabled 与同组的 ThinkingTokens、ReasoningEffort 控制思考/推理模式：
	// 任一被设置时并入 model.GenerationConfig。
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

// ModelRef is the unified declarative spec for a direct (non-agent) model
// call site: which provider/model to use and how to generate. It mirrors the
// generation knobs agents get via AgentConfig so every LLM call site is
// configurable with one vocabulary. (tagent-unify-model-call-config.)
type ModelRef struct {
	Provider             string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Model                string   `json:"model,omitempty" yaml:"model,omitempty"`
	Temperature          *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens            *int     `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	ThinkingEnabled      *bool    `json:"thinking_enabled,omitempty" yaml:"thinking_enabled,omitempty"`
	ThinkingTokens       *int     `json:"thinking_tokens,omitempty" yaml:"thinking_tokens,omitempty"`
	ReasoningEffort      *string  `json:"reasoning_effort,omitempty" yaml:"reasoning_effort,omitempty"`
	ReasoningContentMode string   `json:"reasoning_content_mode,omitempty" yaml:"reasoning_content_mode,omitempty"`
}

// IsZero reports whether the ref carries no explicit declaration at all.
func (m ModelRef) IsZero() bool {
	return m.Provider == "" && m.Model == "" && m.Temperature == nil && m.MaxTokens == nil &&
		m.ThinkingEnabled == nil && m.ThinkingTokens == nil && m.ReasoningEffort == nil &&
		m.ReasoningContentMode == ""
}

// CompressConfig configures SmartCompressor parameters.
type CompressConfig struct {
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

	// SummaryMaxTokens is the floor for the output-token budget reserved on each
	// summary LLM call (0 = package default 8192). Reasoning models spend part
	// of max_tokens on their thinking chain; too small a budget returns empty
	// Content and degrades compression. The per-call budget scales up with the
	// summary size but never below this floor.
	SummaryMaxTokens int `json:"summary_max_tokens,omitempty" yaml:"summary_max_tokens,omitempty"`

	// SummaryTimeoutSeconds bounds ONE real fold's synchronous summary calls
	// (index-card condensation and the rolling narrative share a single
	// sub-context deadline, so a stalled summary can never cost the round twice
	// its budget). 0 = the compress package default (compress.DefaultSummaryTimeout,
	// 5s) — the number is deliberately not duplicated here. Negative is a named
	// validation error, never "no deadline". It joins the summary family inside the
	// per-agent structural fingerprint (org.agentSubset.Compress), i.e. a
	// rebuild-bound construction value, not a fourth hot channel.
	SummaryTimeoutSeconds int `json:"summary_timeout_seconds,omitempty" yaml:"summary_timeout_seconds,omitempty"`

	// SummaryEffort is the flat alias for summary.reasoning_effort
	// (deprecated — folded by FoldModelRefAliases). The field must exist for
	// strict parsing to accept the alias key.
	SummaryEffort string `json:"summary_effort,omitempty" yaml:"summary_effort,omitempty"`
	// SummaryModel is the model name for LLM summary compression.
	// Falls back to the agent's main model if empty.
	// Deprecated: declare compress.summary (ModelRef) instead; folded at load.
	SummaryModel string `json:"summary_model,omitempty" yaml:"summary_model,omitempty"`
	// SummaryProvider is the provider name for the summary model.
	// Falls back to the agent's provider if empty.
	// Deprecated: declare compress.summary (ModelRef) instead; folded at load.
	SummaryProvider string `json:"summary_provider,omitempty" yaml:"summary_provider,omitempty"`
	// Summary is the unified ModelRef declaration for the summary call site
	// (model + generation knobs incl. reasoning_effort). When both this and
	// the flat alias fields are present, Summary wins per-field at fold time.
	Summary ModelRef `json:"summary,omitempty" yaml:"summary,omitempty"`
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

	// Engine 配置记忆引擎（语义/混合检索，T-A 解耦缝）。缺省 nil = 引擎关闭，
	// recall 行为与现状逐字节一致（纯关键词）。配置后：写入旁路异步嵌入索引，
	// recall query 走 关键词∪向量 RRF 融合（工具声明区不变，prefix-cache 不受影响）。
	Engine *MemoryEngineConfig `json:"engine,omitempty" yaml:"engine,omitempty"`
}

// ConsolidationConfig：巩固建议式触发 + 硬门控。
// 触发是建议（渗透/冥想 hint），执行权与质量门在 LLM + 工具硬校验（D2 核心主张）。
type ConsolidationConfig struct {
	// CapacityThreshold：分区未巩固边界事件计数超此值 → 发 consolidation_hint 渗透
	// 消息（非自动执行）。0 = 容量路关闭。
	CapacityThreshold int `json:"capacity_threshold,omitempty" yaml:"capacity_threshold,omitempty"`
	// MinSourceEvents：memory_consolidate 硬门控——实际取回的源事件数不足即显式拒绝
	// （防证据缺失的记忆伪造）。0 = 不校验。默认建议 3。
	MinSourceEvents int `json:"min_source_events,omitempty" yaml:"min_source_events,omitempty"`
	// Snooze：hint 被拒/已发后的静默窗（duration 字符串，如 "24h"），防重复打扰。
	// 空 = 每次超阈都提示。
	Snooze string `json:"snooze,omitempty" yaml:"snooze,omitempty"`
}

// Validate 校验巩固配置：负值非法；Snooze 非空
// 时必须是合法 duration。零值全部合法（= 触发关闭/不校验，现状行为）。
func (c ConsolidationConfig) Validate() error {
	if c.CapacityThreshold < 0 {
		return fmt.Errorf("consolidation.capacity_threshold must be >= 0")
	}
	if c.MinSourceEvents < 0 {
		return fmt.Errorf("consolidation.min_source_events must be >= 0")
	}
	if c.Snooze != "" {
		if _, err := time.ParseDuration(c.Snooze); err != nil {
			return fmt.Errorf("consolidation.snooze %q: %w", c.Snooze, err)
		}
	}
	return nil
}

// MemoryEngineConfig 配置记忆引擎（向量后端选择与融合调参）。
type MemoryEngineConfig struct {
	// Backend 选择向量后端："memory"（MVP 内存索引，默认）或 "rustviking"。
	// 注意（审查 Nit7）：MVP 阶段两者等价——均为内存向量索引；差异在持久化：store 为
	// file/localfile 时向量自动序列化入其底层 KV（rustviking/LocalFile）+ 启动重建。
	// rustviking 原生 HNSW/IVF 索引持久化（接入 ivf_persist）为 rustviking backlog
	// （见 f1-rustviking-capability-report.md：rustviking index CLI 进程内易失）。
	Backend string `json:"backend,omitempty" yaml:"backend,omitempty"`
	// Embedding 配置嵌入器。nil = 无向量，引擎不接线（行为同现状纯关键词）。
	Embedding *EmbeddingConfig `json:"embedding,omitempty" yaml:"embedding,omitempty"`
	// Consolidation 配置巩固的建议式触发与硬门控。
	// 零值 = 触发关闭（纯 manual，现状行为不变）；MinSourceEvents>0 时
	// memory_consolidate 对实际取回源数不足的调用显式拒绝。
	Consolidation *ConsolidationConfig `json:"consolidation,omitempty" yaml:"consolidation,omitempty"`
	// VectorTopK / KeywordTopK / RRFK 融合调参（0 取引擎默认 20/20/60）。
	VectorTopK  int `json:"vector_top_k,omitempty" yaml:"vector_top_k,omitempty"`
	KeywordTopK int `json:"keyword_top_k,omitempty" yaml:"keyword_top_k,omitempty"`
	RRFK        int `json:"rrf_k,omitempty" yaml:"rrf_k,omitempty"`
}

// EmbeddingConfig 配置文本嵌入器（zhipu embedding-3，openai 兼容 /embeddings）。
type EmbeddingConfig struct {
	// Provider："zhipu"（默认，openai 兼容 HTTP）或 "mock"（确定性哈希，测试/离线）。
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	// Model 默认 "embedding-3"。
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// APIKeyEnv 默认 "ZAI_API_KEY"（GLM Coding Plan）。缺失 = 嵌入关闭（优雅降级）。
	APIKeyEnv string `json:"api_key_env,omitempty" yaml:"api_key_env,omitempty"`
	// Endpoint 默认 zhipu embeddings 端点（open.bigmodel.cn/api/paas/v4/embeddings）。
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	// Dimensions 请求维度（embedding-3 支持 512/1024/2048）；0 = 模型默认。
	Dimensions int `json:"dimensions,omitempty" yaml:"dimensions,omitempty"`
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

	// ObservedNamespaces 声明外部观察形态冥想的跨分区观察面；缺省空=in-loop 自体维护
	// （observed 回落 read_namespaces 及 observed 越界授权的校验由组合根装配期承担，
	// 本处仅承载声明与解析）。构造期读取，走结构换代、不静默热生效。
	ObservedNamespaces []string `json:"observed_namespaces,omitempty" yaml:"observed_namespaces,omitempty"`

	// DeliverTo 声明冥想产出可投递的目标 agent 白名单；缺省空=拒绝一切投递（fail-closed）。
	// 构造期读取走换代；deliver_to 白名单/越界校验落在投递 API 与组合根，本处仅承载声明与解析。
	DeliverTo []string `json:"deliver_to,omitempty" yaml:"deliver_to,omitempty"`
}

// ExtraParam re-exports agent.ExtraParam for YAML/JSON config declaration
// (ToolRef.extra_params).
type ExtraParam = agent.ExtraParam

// ToolRef declares a tool that an agent uses.
// For agent-kind tools, the AgentID field references another AgentConfig in the Agents map.
// For tool-kind tools, the ID field identifies the plain tool factory.
// It declares only the reference relationship: an agent's runtime parameters
// (max_tool_iterations, max_tokens, temperature) live on its own AgentConfig entry.
type ToolRef struct {
	// Kind distinguishes agent tools from plain tools. Defaults to "agent".
	Kind ToolKind `json:"kind" yaml:"kind"`

	// AgentID references another agent in the Agents map (kind=agent).
	// The referenced agent becomes a CallableTool for this agent.
	AgentID string `json:"agent,omitempty" yaml:"agent,omitempty"`

	// ID is the tool identifier for plain tools (kind=tool).
	ID string `json:"id,omitempty" yaml:"id,omitempty"`

	// Description 是工具描述正文（inline 形态）；文件形态见同组的 DescriptionFile，
	// 其路径相对 prompt_dir。
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
	// tools. Each declared parameter is added to
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

	// Properties holds tool-specific configuration that each tool factory
	// deserializes into its own typed struct, keeping ToolRef generic.
	//
	// - Example keys of the exec tool: workspace, run_as_user, run_as_group.
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

	// Factory is the custom factory path for non-builtin tools and agents.
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

// IsRemoteRef reports whether this ToolRef declares a remote A2A target: the
// decision demands a non-blank Remote URL. The bar is deliberate: this is the ONE
// predicate both the validation domain and the construction domain
// (buildAgentToolRef's A2A branch) use — two separate spellings of "is this
// remote" is exactly how validation came to demand a local definition that
// construction never asks for, while a Remote block with a blank URL sailed
// through validation and was then quietly built as a LOCAL agent, contrary to
// what it declared.
func (tr ToolRef) IsRemoteRef() bool {
	return tr.Remote != nil && strings.TrimSpace(tr.Remote.URL) != ""
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

// DefaultEntry 等常量是 DefaultConfig 采用的缺省值：入口名、prompt 目录，
// 以及主 agent 与子 agent 各自的迭代/令牌/温度/压缩阈值上限。
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
						ID:              "exec",
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
	c.FoldModelRefAliases()
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

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}

	if v := os.Getenv("TAGENT_WORKING_DIR"); v != "" {
		c.WorkingDir = v
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
		ac.Memory.Type = "memory"
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

	if _, ok := c.Agents[c.Entry]; !ok {
		return fmt.Errorf("tagent config: entry agent %q not found in agents map", c.Entry)
	}

	for name, ac := range c.Agents {
		if err := ac.validate(name); err != nil {
			return err
		}
	}

	for name, ac := range c.Agents {
		for i, tr := range ac.Tools {
			if tr.Kind == ToolKindAgent && tr.AgentID != "" && !tr.IsRemoteRef() {
				if _, ok := c.Agents[tr.AgentID]; !ok {
					return fmt.Errorf("tagent config: agent %q tool[%d] references unknown agent %q",
						name, i, tr.AgentID)
				}
			}
		}
	}

	for name, sc := range c.MCPServers {
		if err := sc.Validate(name); err != nil {
			return fmt.Errorf("tagent config: %w", err)
		}
	}

	if err := c.TrajectoryCapture.validate(c.TrajectoryDump); err != nil {
		return err
	}

	return nil
}

// validate checks the capture block the way the composition root must: a negative
// bound is never re-read as "unlimited", an above-ceiling handle count is rejected
// instead of clamped (rl's clamp only covers direct in-library use), and enabling
// v2 capture on top of nothing is a contradiction. The limits are checked even when
// the block is off, so a typo can never hide inside a disabled block.
func (b CaptureBlock) validate(trajectoryDump bool) error {
	limits := []struct {
		name string
		v    int64
	}{
		{"max_record_bytes", b.MaxRecordBytes},
		{"max_pending_bytes", b.MaxPendingBytes},
		{"max_run_bytes", b.MaxRunBytes},
		{"max_open_files", int64(b.MaxOpenFiles)},
	}
	for _, l := range limits {
		if l.v < 0 {
			return fmt.Errorf("tagent config: %w: %s = %d", ErrCaptureNegativeLimit, l.name, l.v)
		}
	}
	if b.MaxOpenFiles > MaxCaptureOpenFiles {
		return fmt.Errorf("tagent config: %w: max_open_files = %d, ceiling %d",
			ErrCaptureOpenFilesExceeded, b.MaxOpenFiles, MaxCaptureOpenFiles)
	}
	if b.Enabled && !trajectoryDump {
		return fmt.Errorf("tagent config: %w", ErrCaptureRequiresDump)
	}
	return nil
}

// validate bounds the synchronous summary deadline: negative fails startup (it is
// not "no deadline"); values above 120s are rejected outright rather than clamped —
// the ceiling exists to bound a worst-case wait inside the model path, and a silent
// clamp would make a typo look like a working configuration.
func (c CompressConfig) validate(agentName string) error {
	if c.SummaryTimeoutSeconds < 0 {
		return fmt.Errorf("tagent config: agent %q: %w: summary_timeout_seconds = %d",
			agentName, ErrSummaryTimeoutNegative, c.SummaryTimeoutSeconds)
	}
	if c.SummaryTimeoutSeconds > MaxSummaryTimeoutSeconds {
		return fmt.Errorf("tagent config: agent %q: %w: summary_timeout_seconds = %d, ceiling %d",
			agentName, ErrSummaryTimeoutTooLarge, c.SummaryTimeoutSeconds, MaxSummaryTimeoutSeconds)
	}
	return nil
}

// validate checks an AgentConfig for errors.
func (ac *AgentConfig) validate(name string) error {
	if err := ac.Compress.validate(name); err != nil {
		return err
	}
	seenIDs := make(map[string]bool)
	for i, tr := range ac.Tools {
		if tr.Kind == ToolKindAgent {
			if tr.AgentID == "" {
				return fmt.Errorf("agent %q: tools[%d] agent kind requires agent id", name, i)
			}
			if tr.Remote != nil && !tr.IsRemoteRef() {
				return fmt.Errorf("agent %q: tool agent %q declares remote but requires a url", name, tr.AgentID)
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
	case ".yaml", ".yml", ".json":
		if err := strictyaml.DecodeByExt(path, data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("unsupported config file extension %q (use .yaml, .yml, or .json)", ext)
	}

	if abs, err := filepath.Abs(path); err == nil {
		cfg.ConfigPath = abs
	} else {
		cfg.ConfigPath = path
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
