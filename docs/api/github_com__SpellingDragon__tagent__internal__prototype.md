package prototype // import "github.com/SpellingDragon/tagent/internal/prototype"

职责：事件消费壳——消费 AgentState 队列、按类型分派、驱动插件与模型，模型产出全部回队列。
未实现：背压、流式、持久化、强杀；壳只持 State 与函数字段，停止壳不关 owner 任务。 接线：装配方注入
Run/OnEvents/ModelCompletion/ResolveTool/Deliver/Compact；队列、上下文、任务在 AgentState。

职责：config.go 只做「配置描述 + 装配」——从 JSON 读取 Config、校验 model/tool/skill/plugin
的 Registry 引用、热加载时经 state.Apply 做架构变更，并为某个 id 构造可替换的 BaseTAgent
壳： 绑定 ResolveTool（definition 允许集 + recall / agent:<id>
适配）、ModelCompletion（请求时组装 ModelRequest 全字段并交 CallModel）与声明的插件。
未实现：YAML/文件监听/调度器、循环体、演进判定、任务状态、重试与失败自动恢复；Source 由输入 明确指定（默认 user），反思即自定义
Source 的输入，无专属控制器，也不做生产保护。 接线：state.go 提供 State/AgentState 与 Apply；AgentTask

职责：模块间的事件、任务、配置、插件和模型请求数据契约。 未实现：网络协议、凭据、持久格式迁移；JSON 快照只保存数据，不保存执行函数。 接线：agent

职责：以锁保护的顺序票据 map 保存事件原文，供 Recall 取回与 JSON 快照恢复。 未实现：落盘持久化、索引、淘汰策略、语义检索。

职责：调用框架已确定实例的模型并记录完整请求/输出/错误用于训练监控。 未实现：调度、重试、缓存、provider 检测、system 组装、流式与计费。

职责：按明确 Kinds 在事件前后选择性变换，并提供结果外置插件与折叠。 未实现：插件管理器、优先级、失败回滚、异步投递；顺序由调用方切片固定。
接线：agent 循环调 ApplyPlugins；config 用 ResultMemoryPlugin 装配；模型消费后调 CompactResults。

职责：state.go 维护每个 agent id 的消费现场——AgentState 持定义、生效 system prompt、待处理
队列、唤醒信号、消费者占用、上下文、Memory 与 TaskManager；State 以 id 注册现场，按整份新配置 做架构级 Apply（校验
id 唯一与父关系、由 currentConfig 与候选构建 StatePlan、依序经 Eval→Benchmark→ApplyGate 裁决，同
id 更新定义+prompt、缺 id 回收），并在演示检查点把可序列化 现场存成 JSON、跨进程 Load 重建（在途任务恢复 unknown，生效
prompt 一并回填）。 未实现：生产 WAL/事务/多代租约、任意时刻崩溃一致性、按年龄判僵尸、函数序列化、队列背压； 队列与上下文是内存态，只有显式
Save 才落盘，也不自动重执行任何副作用。 接线：BaseTAgent 经 Begin/Next/End 消费并 Publish 回流；TaskManager

职责：owner 任务表——登记工具/子 agent 调用，自持可取消 context，启动和完成时分别发送状态事件。
未实现：重试、TTL、强杀、跨进程重挂；不复制任务 ID 序列。 接线：AgentState 构造 TaskManager，通知一律经

VARIABLES

var (
	ErrOwnerRemoved    = errors.New("prototype: owner 已移除，拒绝路由")
	ErrAlreadyConsumed = errors.New("prototype: 现场已被消费壳占用")
	ErrUnknownAgent    = errors.New("prototype: 未知 agent id")
	ErrForeignOwner    = errors.New("prototype: 事件归属与现场不符")
)
    ErrOwnerRemoved 等现场级可见错误：拒绝而非静默丢弃。

var ErrMemoryMiss = errors.New("memory: miss")
    ErrMemoryMiss 表示按键未命中原文。

FUNCTIONS

func CompactResults(owner *AgentState)
    CompactResults 只把带 memory_key 的 ToolResult 折成短预览+票据，幂等，输入/回复不动。

func ReloadConfig(path string, state *State, registry Registry, evo Evolution) ([]string, error)
    ReloadConfig 热加载配置：解析后校验 model/tool/skill/plugin 引用，构造 resolve（经 buildSystem
    把 skill 内容与 tools 行固化成生效 prompt），再交 state.Apply 做架构级变更（同 id 复用现场、缺 id 回收）；
    不做文件监听。返回被回收的 owner id。

TYPES

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
    AgentConfig 是一个 agent 的声明面：身份、父子关系、模型与可选执行面。

type AgentState struct {
	ID     string
	Tasks  *TaskManager
	Memory *Memory

	// Has unexported fields.
}
    AgentState 是一个 agent id 的完整消费现场。Tasks/Memory 只在创建时绑定，热加载同 id 不换指针、只更新定义与生效
    prompt。mu 只保护队列/上下文/标志，绝不包住用户回调或 Tasks.Close。

func (a *AgentState) Append(e Event)
    Append 把一条事件追加到上下文尾部（核心消费后记录），隔离 Meta 以免插件改动持久原文。

func (a *AgentState) Begin() error
    Begin 独占消费：现场已有消费壳或已被移除时拒绝，保证同一现场只有一个壳在消费。

func (a *AgentState) Context() []Event
    Context 返回上下文快照（隔离 Meta）。

func (a *AgentState) Definition() AgentConfig
    Definition 返回当前生效定义（浅拷贝，调用方只读）。

func (a *AgentState) End()
    End 释放消费占用，与 Begin 配对（重复调用幂等）。

func (a *AgentState) Next(ctx context.Context) ([]Event, error)
    Next 取走当前整批事件；ctx 取消或现场被回收时返回错误，空队列则等唤醒。

func (a *AgentState) Publish(e Event) error
    Publish 补全/校验归属后追加事件并唤醒消费者；owner 被回收或归属不符时拒绝。 唤醒是容量 1
    的脉冲：已有未消费脉冲时跳过唤醒，事件已入队，不丢。

func (a *AgentState) PublishBatch(events []Event) error
    PublishBatch 批量原子发布：整批在单次锁内入队、一次唤醒——消费端 pull 无法切入批中间，
    攒批通知因此"一次消费批次同时可见"。归属校验与 Publish 同规则（通知方已带 OwnerID，不代补）。

func (a *AgentState) ReplaceContext(events []Event)
    ReplaceContext 整份替换上下文（折叠刚用过的结果后回写），隔离 Meta。

func (a *AgentState) SystemPrompt() string
    SystemPrompt 返回当前生效的 system prompt（Apply 时经 resolve 写入；nil resolve 取声明原文）。

type BaseTAgent struct {
	State *AgentState

	Run             func(context.Context) error
	OnEvents        func(context.Context, []Event) error
	ModelCompletion func(context.Context, []string) (ModelOutput, error)
	ResolveTool     func(string) ToolFunc
	Plugins         []Plugin
	// Deliver 是投递任务的执行体（与 ToolFunc 同形）：EventReply 路由不直调它，而是经 owner 任务表
	// Spawn 一条 Call.Name="deliver" 的任务执行；成败以 ToolResult 通知回管线，循环不因投递失败终止。
	Deliver ToolFunc
	Compact func()
}
    BaseTAgent 是事件驱动壳：只持 State 引用与装配注入的函数字段，不持队列/上下文/任务。

func NewAgent(id string, state *State, registry Registry, deliver ToolFunc) (*BaseTAgent, error)
    NewAgent 为 id 装配执行壳：只绑定函数字段与插件，不实现循环、演进或任务状态。模型实例、 工具适配与轨迹记录都在请求/解析时经
    Registry 取得。deliver 为投递任务执行体（ToolFunc 形态）： EventReply 路由经 owner 任务表 Spawn 一条
    deliver 任务执行它，成败以 ToolResult 通知回管线。

func (a *BaseTAgent) DefaultOnEvents(ctx context.Context, events []Event) error
    DefaultOnEvents 逐事件：Before 插件 → 核心按类型分派并先执行 → After 插件 → 内核追加上下文/投递。
    一批已到达事件中有 Input 或 ToolResult 时调用一次模型，不等待其他任务；ToolCall 交任务 API，Reply 只投递。

    - 插件置 Type=0 即显式丢弃；路由由 Before 后的类型决定，After 变换不改变路由。 - ToolCall 与 Compact
    在 After 前就执行核心动作，变换不能撤回已发生的派发。 - EventReply 经 owner 任务表 Spawn 一条 deliver
    任务执行，成败以 ToolResult 回管线；投递失败不终止循环，重投与否属于模型决策。 - TaskNotice 只留状态；ModelDelta
    是模型中间叙述，进上下文投影，不驱动、不投递。

func (a *BaseTAgent) DefaultRun(ctx context.Context) error
    DefaultRun 独占消费本 owner 队列：Begin/End 由 State 管理，Next 阻塞取事件。

func (a *BaseTAgent) Input(ctx context.Context, text string) error
    Input 注入一条外部输入事件（来源默认 user）。

func (a *BaseTAgent) Publish(ctx context.Context, e Event) error
    Publish 把事件投回本壳所属 owner 队列；归属由 AgentState 补全/校验。

type Change struct {
	Before Config
	After  Config
}
    Change 表示架构定义的变化，不用于普通工具执行和任务进度。

type Config struct {
	Agents []AgentConfig
}
    Config 是一次配置加载的声明面全集。

func LoadConfig(path string) (Config, error)
    LoadConfig 读取 JSON 配置文档，只保证是合法 JSON；引用校验需要 Registry，见 ReloadConfig。

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
    Event 是管线上的唯一载体：类型、归属、来源，以及按类型使用的载荷字段。

func ApplyPlugins(ctx context.Context, plugins []Plugin, e Event, before bool) (Event, error)
    ApplyPlugins 对显式声明 Kinds 的插件依次应用 Before/After；空 Kinds 与 Type==0 不执行。

type EventType int
    EventType 是事件类型标识；零值表示插件显式丢弃。

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
    EventInput 起的六个常量是核心分派的入口事件；EventModelDelta 另承载模型中间叙述。

type Evolution struct {
	Eval      func(StatePlan) error
	Benchmark func(StatePlan) error
	ApplyGate func(StatePlan) error
}
    Evolution 是架构变更的三段演进管道 Eval→Benchmark→ApplyGate。三段恒真：nil 段直接跳过 （空
    Evolution{} 即全恒真），任一段返回 error 即拒绝整体提交；不实现审批等待、重试或灰度。 运行时危险调用的拦截不属于
    Evolution，由治理插件在事件管线承担（见 DenylistPlugin）。

type Memory struct {
	// Has unexported fields.
}
    Memory 是按顺序票据索引原文的并发安全表。

func NewMemory() *Memory
    NewMemory 建立空表。

func (m *Memory) Put(e Event) string
    Put 存入事件副本并返回顺序票据，外部改动不影响表内。

func (m *Memory) Recall(_ context.Context, key string) (string, error)
    Recall 按键返回原文；缺键返回 ErrMemoryMiss。

func (m *Memory) Restore(snap map[string]Event)
    Restore 载入原文表并按最大票据续号。

func (m *Memory) Snapshot() map[string]Event
    Snapshot 深拷贝原文表供 JSON 序列化。

type ModelFunc func(context.Context, ModelRequest) (ModelOutput, error)
    ModelFunc 是模型调用体，装配方按模型名注册到 Registry。

type ModelOutput struct {
	ToolCalls []ToolCall
	Output    string
	Reasoning string
}
    ModelOutput 是模型产出：工具调用列表、final 文本与中间推理。

func CallModel(ctx context.Context, model ModelFunc, request ModelRequest, record func(ModelTrace)) (ModelOutput, error)
    CallModel 调用指定模型并记录一次成功或失败轨迹；本身无状态，不改动原请求，也不访问 agent state。

type ModelRequest struct {
	AgentID      string
	ModelName    string
	SystemPrompt string
	Params       map[string]float64
	Skills       []string
	Tools        []string
	Inputs       []string
}
    ModelRequest 是一次模型调用的完整请求面：身份、模型、系统提示、参数、技能、工具与输入。

type ModelTrace struct {
	Request  ModelRequest
	Response ModelOutput
	Error    string
}
    ModelTrace 是一次模型调用的完整记录，交给 Record 供训练与监控消费。

type Plugin struct {
	Kinds  []EventType
	Before func(context.Context, Event) (Event, error)
	After  func(context.Context, Event) (Event, error)
}
    Plugin 只应用于 Kinds 中的事件。Before/After 可变更数据，返回 Type=0 表示丢弃。

func DenylistPlugin(owner *AgentState, names ...string) Plugin
    DenylistPlugin 是治理插件示例：对命中名单的 ToolCall，Before 把该事件变换为 denied ToolResult
    （保留 Call/Data，置 Status="denied"、Error=拒绝原因）。核心按 Before 之后的 Type 路由，于是走
    ToolResult 路径进上下文并驱动模型——工具零执行、模型读到拒绝事实、据以继续。未命中原样放行由核心 spawn。owner
    为现场接缝（HITL 同机制：变换前先问人），原型不实现。

func ResultMemoryPlugin(store *Memory, eligible func(Event) bool) Plugin
    ResultMemoryPlugin 只对 eligible 的 ToolResult 存全文并注 memory_key；输入不处理，也不压缩正文。

type Registry struct {
	Models  map[string]ModelFunc
	Tools   map[string]ToolFunc
	Skills  map[string]string
	Plugins map[string]func(*AgentState) Plugin
	Record  func(ModelTrace)
}
    Registry 将配置中的名字解析为本进程执行能力，不保存 agent 消费现场。

type State struct {
	// Has unexported fields.
}
    State 以 agent id 注册消费现场；生效定义即各 AgentState.Definition，不另存镜像。

func LoadState(path string) (*State, error)
    LoadState 从 JSON 快照重建 State：建现场与 owner 映射、恢复生效 prompt/队列/上下文/Memory，再经
    TaskManager.Restore(01) 把在途任务恢复 unknown（可能发 notice）。无 owner 的回收留给随后
    ReloadConfig 的 Apply；不自动重执行副作用。

func NewState() *State
    NewState 建立空的现场注册表。

func (s *State) Agent(id string) (*AgentState, error)
    Agent 按 id 取出消费现场。

func (s *State) Apply(cfg Config, resolve func(AgentConfig) string, evo Evolution) ([]string, error)
    Apply 以整份新配置重建架构：结构校验后，由当前生效定义（Before）与候选（After）构建 StatePlan，依序交
    Eval→Benchmark→ApplyGate 裁决（nil 段恒真）；通过后同 id 更新定义与生效 prompt、新 id 建现场、旧 id
    标记移除并回收。任一段拒绝即返回，定义、生效 prompt 与现场保持旧值。 TaskManager.Close 会等在途任务，一律在 State
    锁外执行。返回本次被回收的 owner id（升序）。

    - 同 id 换装只换定义与生效 prompt，现场不动。 - 新建现场尚未发布，写入生效 prompt 不需要加锁。 -
    孤儿回收在锁外取消任务上下文并等待在途任务收尾。

func (s *State) Close()
    Close 释放整个注册表：标记所有现场移除并在锁外关闭各 TaskManager（宿主关闭）。

func (s *State) Save(path string) error
    Save 在演示检查点采集每个现场并写成 JSON 快照（按 id 排序便于比对）。

type StatePlan struct {
	Change  Change
	Adds    []AgentConfig
	Updates []AgentConfig
	Removes []string
	Prompts map[string]string
}
    StatePlan 是一次配置变更引发的 state 变更计划：声明面 diff（Before=当前生效定义重建，After=候选）、
    将新建/更新的定义、将回收的 owner id（均按 id 升序），以及受影响 agent 的下一版生效 system prompt。

type Task struct {
	ID      int
	OwnerID string
	Source  string
	Call    ToolCall
	Status  string
	Output  string
	Error   string
}
    Task 是 owner 任务表里的一条执行事实：状态、结果与错误。

type TaskManager struct {
	// Has unexported fields.
}
    TaskManager 自持 owner 的任务与可取消 context；普通停止壳不关任务，只有 Close 取消并等待。 任务与管线的映射只有
    owner 一份真值；window>0 时完成通知按条数攒批投递。

func NewTaskManager(owner *AgentState, window int) *TaskManager
    NewTaskManager 建立 owner 任务表并自持可取消 context；window 为完成通知条数聚合窗（0=即发）。

func (m *TaskManager) Close()
    Close 取消本任务表所有任务、等待在途执行结束，再 flush 条数窗残余通知（孤儿回收/宿主关闭）。

func (m *TaskManager) Complete(id int, output string, err error)
    Complete 结算 running/unknown 任务再发结果事件；terminal 记录不重复改写旧事实。

func (m *TaskManager) Restore(tasks []Task)
    Restore 载入任务记录：保留归属，running 转 unknown 并发通知，丢弃 owner 不匹配的任务。

func (m *TaskManager) Snapshot() []Task
    Snapshot 返回任务记录拷贝，供 JSON 快照。

func (m *TaskManager) Spawn(call ToolCall, run ToolFunc, source string) int
    Spawn 登记任务并发 running 通知，返回新任务 ID；run 为 nil 视为未知工具失败， owner 已关闭（ctx
    取消）时登记为取消而不开工。

type ToolCall struct {
	Name string
	Args string
}
    ToolCall 是一次工具调用：名字与参数。

type ToolFunc func(context.Context, string) (string, error)
    ToolFunc 是工具执行体：入参为调用参数，返回文本结果与错误。

func AgentTask(state *State, id string, registry Registry) ToolFunc
    AgentTask 把 `agent:<id>` 委派适配成父可用工具：子用同一 NewAgent/Run 循环执行， 首个 Reply
    作为父任务结果；取到后停子消费壳，但子 owner 资源（TaskManager）由 State 保有。
