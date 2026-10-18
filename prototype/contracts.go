// 职责：模块间的事件、任务、配置、插件和模型请求数据契约。
// 未实现：网络协议、凭据、持久格式迁移；JSON 快照只保存数据，不保存执行函数。
// 接线：agent 消费 Event，state 持有 AgentConfig，config 解析 Registry，模型代理接收完整 ModelRequest。
// 契约: docs/wiki/agent/prototype-skeleton.md#six-pieces
package prototype

import "context"

// EventType 是事件类型标识；零值表示插件显式丢弃。
type EventType int

// EventInput 起的六个常量是核心分派的入口事件；EventModelDelta 另承载模型中间叙述。
const (
	EventInput EventType = iota + 1
	EventToolCall
	EventToolResult
	EventCompact
	EventReply
	EventTaskNotice
	// EventModelDelta 是模型的中间叙述（带工具调用时的 Output 与 Reasoning）：回送本管线进上下文投影，
	// 不驱动模型、不外投；final 答复仍走 EventReply。
	EventModelDelta
)

// ToolCall 是一次工具调用：名字与参数。
type ToolCall struct {
	Name string
	Args string
}

// Event 是管线上的唯一载体：类型、归属、来源，以及按类型使用的载荷字段。
type Event struct {
	Type    EventType
	OwnerID string
	Source  string
	Data    string
	Call    ToolCall
	TaskID  int
	Status  string
	Error   string
	Meta    map[string]string
}

// ToolFunc 是工具执行体：入参为调用参数，返回文本结果与错误。
type ToolFunc func(context.Context, string) (string, error)

// ModelRequest 是一次模型调用的完整请求面：身份、模型、系统提示、参数、技能、工具与输入。
type ModelRequest struct {
	AgentID      string
	ModelName    string
	SystemPrompt string
	Params       map[string]float64
	Skills       []string
	Tools        []string
	Inputs       []string
}

// ModelOutput 是模型产出：工具调用列表、final 文本与中间推理。
type ModelOutput struct {
	ToolCalls []ToolCall
	Output    string
	Reasoning string
}

// ModelFunc 是模型调用体，装配方按模型名注册到 Registry。
type ModelFunc func(context.Context, ModelRequest) (ModelOutput, error)

// ModelTrace 是一次模型调用的完整记录，交给 Record 供训练与监控消费。
type ModelTrace struct {
	Request  ModelRequest
	Response ModelOutput
	Error    string
}

// Plugin 只应用于 Kinds 中的事件。Before/After 可变更数据，返回 Type=0 表示丢弃。
type Plugin struct {
	Kinds  []EventType
	Before func(context.Context, Event) (Event, error)
	After  func(context.Context, Event) (Event, error)
}

// AgentConfig 是一个 agent 的声明面：身份、父子关系、模型与可选执行面。
type AgentConfig struct {
	ID           string
	ParentID     string
	Model        string
	SystemPrompt string
	Params       map[string]float64
	Skills       []string
	Tools        []string
	Plugins      []string
	// TaskWindow 是任务完成通知的条数聚合窗：0=即发；>0 攒满 window 条批量投递，Close 前 flush 残余。
	// 窗口不等待输入、不控制模型触发粒度（pull 自然聚合是唯一节奏）。
	TaskWindow int
}

// Config 是一次配置加载的声明面全集。
type Config struct {
	Agents []AgentConfig
}

// Change 表示架构定义的变化，不用于普通工具执行和任务进度。
type Change struct {
	Before Config
	After  Config
}

// StatePlan 是一次配置变更引发的 state 变更计划：声明面 diff（Before=当前生效定义重建，After=候选）、
// 将新建/更新的定义、将回收的 owner id（均按 id 升序），以及受影响 agent 的下一版生效 system prompt。
type StatePlan struct {
	Change  Change
	Adds    []AgentConfig
	Updates []AgentConfig
	Removes []string
	Prompts map[string]string
}

// Evolution 是架构变更的三段演进管道 Eval→Benchmark→ApplyGate。三段恒真：nil 段直接跳过
// （空 Evolution{} 即全恒真），任一段返回 error 即拒绝整体提交；不实现审批等待、重试或灰度。
// 运行时危险调用的拦截不属于 Evolution，由治理插件在事件管线承担（见 DenylistPlugin）。
type Evolution struct {
	Eval      func(StatePlan) error
	Benchmark func(StatePlan) error
	ApplyGate func(StatePlan) error
}

// Registry 将配置中的名字解析为本进程执行能力，不保存 agent 消费现场。
type Registry struct {
	Models  map[string]ModelFunc
	Tools   map[string]ToolFunc
	Skills  map[string]string
	Plugins map[string]func(*AgentState) Plugin
	Record  func(ModelTrace)
}

// Task 是 owner 任务表里的一条执行事实：状态、结果与错误。
type Task struct {
	ID      int
	OwnerID string
	Source  string
	Call    ToolCall
	Status  string
	Output  string
	Error   string
}
