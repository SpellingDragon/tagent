## Context

用户最新六点是本轮事实依据；旧 wiki 只用于定位概念，不作为照抄目标。磁盘 agent.go/plugins.go/model.go 已有未完成事件化修改，本轮以此方向重写适配，不恢复旧批次栅栏。用户补充选择跨进程恢复，任务和状态均归属 agent id，无 owner 时作为孤儿回收。

## Decisions

### D1 原型边界

- 核心只消费事件、调用任务 API、构造请求并发布模型产出。无同批收齐等待、无工具级治理、无排序模拟确定性、无票据知识。
- 输入原文与默认回复不折叠、不打票据。只有显式选择的结果插件把低密度结果外置；先让模型读原文，模型调用后将带 memory_key 的结果上下文改成短预览+票据。
- 插件是 Kinds + Before/After 自定义闭包，允许改 Data/Meta；没有默认全事件插件。核心分派不搬入插件。
- state 持有每个 agent id 的定义/父子关系、队列、上下文、Memory、TaskManager。执行壳只引用状态。普通任务转换不走治理；Config 新增/删除/替换通过 Govern(Change)。
- JSON 配置显式 Reload 即热加载，不做文件 watcher。相同 id 复用 state，换装时旧壳停止消费、新壳接同一队列；任务寿命属于 owner，不属于壳。
- JSON 快照只在演示检查点保存。恢复队列、上下文、memory与任务记录；running 变 unknown，保留 OwnerID/TaskID，不自动重新执行。真实/模拟资源通过 TaskManager.Complete(id,...) 回报重挂结果。Reload 后无 owner 的状态关闭、任务取消并回收；不是按年龄判僵尸。
- 不引入生产 WAL、事务、预算、批准队列、版本租约、强杀、重试系统。快照不是任意时刻崩溃一致性承诺。
- 只保留 agent 集成测试，不保留四个旧测试/Example文件。真实子进程验证快照恢复；模型/工具/资源使用 mock，但不 mock 核心管线。

### D2 子域与写面

| 域 | 文件 | 领域规格 | 边界 | 外部依赖 |
|---|---|---|---|---|
| 01-pipeline | agent.go, task_manager.go | event-task-pipeline | 循环/任务 API/子 agent 执行适配 | contracts.go，AgentState API，插件执行 API |
| 02-state | state.go, config.go | owner-state-recovery | agent id现场、架构治理、JSON解析/快照/回收、装配 | contracts.go，TaskManager/Memory API，BaseTAgent |
| 03-adapters | plugins.go, memory.go, model.go | selective-event-adapters | 选择性变换、票据内存、无状态代理 | contracts.go，AgentState.Context/ReplaceContext |

### D3 并发安排

W0：主代理冻结下列签名、读取现状。W1：三个域在独立 staging 目录并发写代码，不写活跃 prototype；每域仅 gofmt/语法检查，不把未集成代码的编译当成验收。W2：主代理逐文件集成；集成测试子代理与 README 子代理按固定接口并行，之后主代理统一编译/测试。波次只是汇报分组，解锁以依赖产物已集成为准，不要求 git 提交。本轮无提交授权。

Go `go list -deps ./prototype` 确认所有源文件同属一编译闭包，因此隔离的是写树；不是伪称同包多文件可独立编译。staging 为 `.qoder/staging/prototype-reset/<domain>/`，无网络、无设备、无包安装；主代理独占集成与一级账目。生产源码每域只允许对应文件，编译红项在 W2 统一归因。

### D4 冻结数据契约（contracts.go 由主代理提供）

```go
type EventType int
const (EventInput EventType = 1; EventToolCall EventType = 2; EventToolResult EventType = 3; EventCompact EventType = 4; EventReply EventType = 5; EventTaskNotice EventType = 6)
type ToolCall struct { Name string; Args string }
type Event struct { Type EventType; OwnerID string; Source string; Data string; Call ToolCall; TaskID int; Status string; Error string; Meta map[string]string }
type ToolFunc func(context.Context, string) (string, error)
type ModelRequest struct { AgentID string; ModelName string; SystemPrompt string; Params map[string]float64; Skills []string; Tools []string; Inputs []string }
type ModelOutput struct { ToolCalls []ToolCall; Output string; Reasoning string }
type ModelFunc func(context.Context, ModelRequest) (ModelOutput, error)
type ModelTrace struct { Request ModelRequest; Response ModelOutput; Error string }
type Plugin struct { Kinds []EventType; Before func(context.Context, Event)(Event,error); After func(context.Context, Event)(Event,error) }
type AgentConfig struct { ID string; ParentID string; Model string; SystemPrompt string; Params map[string]float64; Skills []string; Tools []string; Plugins []string }
type Config struct { Agents []AgentConfig }
type Change struct { Before Config; After Config }
type Registry struct { Models map[string]ModelFunc; Tools map[string]ToolFunc; Skills map[string]string; Plugins map[string]func(*AgentState)Plugin; Record func(ModelTrace) }
type Task struct { ID int; OwnerID string; Source string; Call ToolCall; Status string; Output string; Error string }
```

JSON 用 Go 字段默认名称即可，示例一律同口径。Event.Error 为可序列化错误文本，工具函数仍返回 error；没有 callback、mutex 或 goroutine 进入快照。类型只在 contracts.go 声明一次。

### D5 冻结调用契约与归属

**01 域提供**：
- `type BaseTAgent struct { State *AgentState; Run func(context.Context)error; OnEvents func(context.Context,[]Event)error; ModelCompletion func(context.Context,[]string)(ModelOutput,error); ResolveTool func(string)ToolFunc; Plugins []Plugin; Deliver func(context.Context,Event)error; Compact func() }`
- `(a *BaseTAgent) Input(ctx,text)error`, `Publish(ctx,Event)error`, `DefaultRun(ctx)error`, `DefaultOnEvents(ctx,[]Event)error`。
- Run 通过 State.Begin()/End() 独占消费，Next(ctx)等待事件；停止壳不关 owner.Tasks。插件 Before → 核心类型分派 → After → 核心追加上下文/投递；Input与ToolResult驱动模型一次，TaskNotice/Reply不触发模型；ToolCall只经任务API执行；模型产物全 Publish 回队列。模型调用后 Compact 只处理刚用过的结果。
- `NewTaskManager(ownerID string, emit func(Event)) *TaskManager`；`Spawn(call ToolCall, run ToolFunc, source string) int`；`Complete(id int, output string, err error)`；`Snapshot() []Task`；`Restore([]Task)`；`Close()`。TaskManager自有可取消任务context；Spawn发running通知，Complete更新该任务再发ToolResult，不等待其他任务。Restore running→unknown发TaskNotice；Complete可接受unknown作为mock重挂结果。不复制任务ID序列。
- `AgentTask(state *State,id string,registry Registry) ToolFunc`：配置中 `agent:<id>` 引用交给此适配器，子也是BaseTAgent，任务仍属于子。限定一个child id同时一个调用，取首个子Reply作为该委派结果并停子消费壳（child自身TaskManager由State保有，非强杀）。其余并发/多轮协议留接缝，不加路由系统。

**02 域提供**：
- `NewState() *State`；`(s *State) Agent(id string)(*AgentState,error)`；`Apply(cfg Config, govern func(Change)error)([]string,error)`；`Save(path string)error`；`LoadState(path string)(*State,error)`；`(s *State) Close()`。
- `AgentState` 暴露 `ID string; Tasks *TaskManager; Memory *Memory`（引用只在创建时绑定）。方法 `Definition() AgentConfig; Publish(Event)error; Next(context.Context)([]Event,error); Append(Event); Context() []Event; ReplaceContext([]Event); Begin()error; End()`。
- 队列/wake/consumer占用/定义/上下文均由AgentState持有。被移除owner拒绝Publish并关闭Tasks；Apply相同id只更新definition，不替换现场；先校验/治理再应用整份定义，不必实现多代事务。
- `LoadConfig(path string)(Config,error)`；`ReloadConfig(path string,state *State,registry Registry,govern func(Change)error)([]string,error)`；`NewAgent(id string,state *State,registry Registry,deliver func(context.Context,Event)error)(*BaseTAgent,error)`。
- 配置解析校验模型/工具/skill/plugin引用，普通tools从registry解析、`recall`接owner.Memory.Recall、`agent:<id>`接AgentTask。ModelCompletion在请求时确定模型实例、参数、system（配置prompt+skill内容+可用tool名称）、请求元数据，交CallModel。不由proxy访问state。
- NewAgent为声明的plugins创建实例并接 `CompactResults(owner)`；未选择memory插件就无memory_key，不折叠。Source由输入明确指定（默认user），不再维护冥想专属控制器；反思就是自定义Source的输入事件。
- Save/Load 用最小JSON DTO（每agent定义+待处理队列+上下文+tasks+memory），恢复后加载新配置以id合并/回收；不序列化函数。模型调用进行中检查点不承诺恢复，示例在消费边界保存；任务记录允许在途，真实资源重挂留API。

**03 域提供**：
- `NewMemory() *Memory`；`Put(Event) string`；`Recall(context.Context,string)(string,error)`；`Snapshot() map[string]Event`；`Restore(map[string]Event)`。存原始Data，顺序票据字符串即可（重启续号）；不强制所有事件存入。
- `ApplyPlugins(ctx context.Context,plugins []Plugin,e Event,before bool)(Event,error)`：只匹配明确Kinds；空Kinds不执行，调用方显式列出全类型。闭包可改Data/Meta，Type==0可跳过；默认插件不改变归属/种类。
- `ResultMemoryPlugin(store *Memory, eligible func(Event)bool) Plugin`：Kinds只有ToolResult，Before将选中结果全文存memory并注Meta["memory_key"]，输入不处理。原文这一轮给模型；Core调CompactResults后再外置。
- `CompactResults(owner *AgentState)`：只处理带memory_key的ToolResult，把Data改为短预览+`[memory:<key>]`，原始输入/回复/未选结果不动，反复调用幂等。不从正文解析假标记。
- `CallModel(ctx context.Context,model ModelFunc,request ModelRequest,record func(ModelTrace))(ModelOutput,error)`：调用指定模型并记录完整请求/输出/错误；本身无状态。复制可变slice/map用于轨迹保真，记录点不能反过来修改实际请求。无调度/重试/缓存。

## 验收与取舍

- 输入持续增长是有意边界，不再称全局有界上下文；执行结果的正文外置，不承诺票据条数有界。
- 任务结果到达顺序就是外部事件事实，验证快任务结果能触发模型且慢任务不阻塞新输入，不强造跨调度固定全序。
- 拒绝普通工具门、批次栅栏、全部事件标记以及仅为了测试的生命周期钩子。
- 一份 agent_integration_test.go 从 JSON配置→NewAgent/Run/Input→真实模块调用→Reply 观察；另一 agent_recovery_integration_test.go 可专管真实子进程恢复。禁止按helper逐函数测或保留旧单测改名。
- 目标-场景：①慢任务时新输入/快结果仍被消费；②低密度原文首次可见、随后票据化且recall可取回，输入始终原样；③治理拦架构变更但不拦普通工具；④同id热加载复用现场与晚到结果；⑤真子进程保存/加载、owner存在恢复任务，缺owner回收；⑥前后插件选择/变换；⑦代理收到准备好的model/params/system/skill/tools并录制成功/失败；⑧子agent本身通过同一管线执行。
- 集成验证 `GOPROXY=off go test ./prototype -count=10` 与 `-race -count=10`；重复验证不扩展生产保护。README只声明实际通过场景。

## D6 合并规则与 DoD

主代理集成staging时逐文件核实签名；不修改旧变更历史、不运行git reset、不提交或归档。域完成只表示文件语法/契约交付，不虚报集成通过；一级F任务由主代理实际复跑后勾选。所有过程staging文件最后用DeleteFile清理。代码、注释、README以用户最新语义为同一个基线。
