// 本文件是组合根对配置模型的再导出面：模型实体在 config 包，此处别名与薄包装
// 保持既有的 tagent.Config / tagent.LoadConfig 等公共 API 源码级不变。
// 契约: docs/wiki/agent/agent-architecture.md#package-layout
package tagent

import "github.com/SpellingDragon/tagent/config"

type (
	// Config 是整份 tagent 编排声明的根类型（组合根在此别名再导出，实体在 config 包）。
	Config = config.Config
	// GovernanceConfig 别名：治理子系统的声明面。
	GovernanceConfig = config.GovernanceConfig
	// EvolutionConfig 别名：自进化子系统的声明面。
	EvolutionConfig = config.EvolutionConfig
	// ReliabilityConfig 别名：可靠投递与总线落盘的声明面。
	ReliabilityConfig = config.ReliabilityConfig
	// MCPServerConfig 别名：单台 MCP server 的连接声明。
	MCPServerConfig = config.MCPServerConfig
	// ProviderConfig 别名：模型供应商端点声明。
	ProviderConfig = config.ProviderConfig
	// AgentConfig 别名：单个 agent 的声明（模型/记忆/工具/prompt）。
	AgentConfig = config.AgentConfig
	// ModelRef 别名：统一模型引用（provider/model/effort）。
	ModelRef = config.ModelRef
	// CompressConfig 别名：上下文压缩声明。
	CompressConfig = config.CompressConfig
	// MemoryConfig 别名：记忆与存储声明。
	MemoryConfig = config.MemoryConfig
	// ConsolidationConfig 别名：事件整理声明。
	ConsolidationConfig = config.ConsolidationConfig
	// MemoryEngineConfig 别名：记忆引擎选择与参数。
	MemoryEngineConfig = config.MemoryEngineConfig
	// EmbeddingConfig 别名：嵌入供应商声明。
	EmbeddingConfig = config.EmbeddingConfig
	// LifecycleConfig 别名：TTL 遗忘曲线声明。
	LifecycleConfig = config.LifecycleConfig
	// MeditationConfig 别名：冥想回合声明。
	MeditationConfig = config.MeditationConfig
	// ExtraParam 别名：路由级附加参数。
	ExtraParam = config.ExtraParam
	// ToolRef 别名：agent 的工具引用声明。
	ToolRef = config.ToolRef
	// RemoteConfig 别名：A2A 远端连接声明。
	RemoteConfig = config.RemoteConfig
	// PromptConfig 别名：组合式 prompt 声明。
	PromptConfig = config.PromptConfig
	// ToolKind 别名：工具引用的种类。
	ToolKind = config.ToolKind
)

// ToolKindAgent 常量别名：TagentAgent 包装为 CallableTool 的工具引用种类。
const ToolKindAgent = config.ToolKindAgent

// ToolKindTool 常量别名：直接实现 CallableTool 的工具引用种类。
const ToolKindTool = config.ToolKindTool

// DefaultConfig 返回内置默认编排声明（经 config 包实体）。
func DefaultConfig() Config { return config.DefaultConfig() }

// LoadConfig 从 YAML 路径装载并校验整份编排声明（经 config 包实体）。
func LoadConfig(path string) (*Config, error) { return config.LoadConfig(path) }
