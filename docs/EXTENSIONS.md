# tagent 扩展点（EXTENSIONS）

**这份文档面向扩展者**：读完之后应当知道三件事——每个扩展点上自己**只**需要实现什么、实现的东西**与什么有关系**、以及什么会被**具名拒绝**。它是长期事实文档，每一条都与源码注释同在：这里的名字、序列与拒绝原文都能在对应包内逐字核到。

**姿态一句话**：tagent 的扩展面是**窄口**——一个扩展点只承认一件事，越界的写法不是"没生效"，而是启动或调用时的具名拒绝。四处扩展点互不重叠：plugin 只管一次调用的 Before 趟与一次提交的 After 趟，event 只管词汇与投递身份，event-loop 只管消费与回填，`call_subagent` 只管把一个子 agent 装配成任务。

---
<a id="pipeline-phases"></a>
## 一、plugin：四个相位，每个相位一个窄接口

扩展者只需做两件事：实现 `plugin.Plugin`（报一个稳定名字**和恰好一个**相位），再实现该相位的那一个窄接口。相位既决定元素在 pipeline 里的次序，也决定它被派发进哪个接口集；`plugin.NewPipeline` 按声明序分组时逐个核对，越界即拒绝。名字是产物的溯源标签：f 的产物（presenter 的视图、环境块）可回查来源，g 的产物（新事实）自己就是源头——两类义务的差别正在这里，而一个名字只标识写它的那一个元素，重复即按破坏溯源拒绝。

| 相位 | 扩展者实现的接口 | 出现在哪一趟 | 与什么有关系 | 拒绝语义 |
|---|---|---|---|---|
| `PhaseCallTransform` | `TransformCall(ctx, *model.ToolCall) error` | Before 趟，admission 之前、副作用之前 | 这一次工具调用本身：可改工具名与参数，不改 `CallID` 与已解析的可信作用域 | 返回 error 即拒绝**整趟**：该调用不执行、不记录，也不产生 `CallPass`；移动 `CallID` 报 `plugin.ErrCallIdentityChanged` |
| `PhaseRiskPolicy` | `RiskAssessor.AssessRisk(ctx, *model.ToolCall) RiskLevel` 与 `Governor` | Before 趟，全部 transform 之后 | 同一个调用的风险等级与执行前裁决：等级只评级一次，裁决读它而不重算 | 相位内两个面可只实现其一或都实现，一个都不实现按"相位无实现"拒绝 |
| `PhasePostCommitObserve` | `ObserveCommit(ctx, event.EventID) error` | After 趟，先于全部 presenter | 一个**已提交**事实的只读观察 | 失败不返回错误、不回滚事实：失败落成 `view.ObservationGap`（插件名+事实+错误） |
| `PhaseFinalPresentation` | `Present(ctx, event.EventID) (string, bool)` | After 趟，最后 | 一个已提交事实的呈现文本：每个 presenter 各产一视图，视图带源事实与写它的插件名，可回查 | 丢弃只丢呈现（`view.RenderedView.Dropped`），不撤回事实；丢弃必须如实报出 |

两趟的边界是这段接口的全部意义：

- **Before 一趟**：每个调用恰好一次，先跑全部 transforms（各自在私有副本上改），再跑全部 risk faces，产出 `plugin.CallPass{Final, Risk}`。改后的 `Final` 是**被执行、也被记录**的那一个，且仍要过核心的受理路径（身份、权限、审批、去重都在核心，plugin 不自己执行调用）。没有风险面时 `Risk` 为空，裁决读作 `RiskMedium`——空等级不是一张免检通行证。
- **After 一趟**：提交之后才跑，先 observers 再 presenters，每个 presenter 各产一视图、按声明序追加（视图数即注册 presenter 数，可为 0），返回 `view.Presentation{Fact, Views, Gaps}` 且**没有 error 返回值**——这个趟的任何失败都不能成为动已提交事实的理由。被拒绝的调用与提交失败的调用不产生呈现。

装配上的四条拒绝（`plugin.NewPipeline` 的原文）：nil 元素报 `plugin: the pipeline holds a nil plugin`；相位不在四个声明相位内报 `plugin: %q serves phase %d, which is not one of the declared phases`；相位合法但没实现该相位的窄接口报 `plugin: %q serves phase %s but implements no narrow interface of that phase`；同一个名字声明两次报 `plugin: %q is declared twice: a product's name must trace back to the one element that wrote it`。元素也不得服务两个相位：一个注册元素一个相位，相位的两个面可以同时带。

**与什么有关系**：`Pipeline` 每次派发从借用代（borrowed generation）的元素装配一次，请求中途不重组；所以扩展者看到的次序就是宿主贡献元素的次序，pipeline 不改写它。契约在 `api/plugin/phases.go` 与 `api/plugin/present.go`，呈现词汇（`Presentation`/`RenderedView`/`ObservationGap`）在 `api/view/presentation.go`，只引用 `event` 的事实身份。观测面复用框架面、不自造第二套：已提交事实由 `telemetry.LogObserver` 写进 `trpc-agent-go/log.Logger`，相位派发与消费器执行两面直接写 `log.Default`，模型响应只写 `log.Default` 的一行 INFO（generation span 归驱动模型的一方，本仓不再开），四类的观察点与 Langfuse 装配见 OPERATION §七·E。

---
<a id="event-vocabulary"></a>
## 二、event 词汇：一个事实的四个块，八种骨架类型，五类通知

扩展者只需记住一件事：**事件是不可变事实，身份由 `Key` 单独引用**。`event.Event` 的字段分四块，每块首字段的注释就是该块的契约：

| 块 | 字段 | 说明 |
|---|---|---|
| identity | `Key`、`Type`、`Timestamp` | 存储键、冻结类型、记录时间；引用一个事件只引 `Key` |
| content | `Meta`、`Message` | 元数据投影与原生 tRPC 模型消息，二者是一个事实的两面，互不为第二表示 |
| ownership | `Owner`、`Source`、`Target`、`InvocationID`、`TaskID`、`InstanceID`、`EpisodeID`、`DecisionID`、`AttemptID` | 归属与作用域；`Target` 必填且从不默认，`Source` 与 `Target` 分开保存 |
| ledger | `CallID`、`ApprovalID` | 原生调用标识逐字节保留；去重作用域是 `(InstanceID, InvocationID, DecisionID, CallID)` |

骨架类型（`api/event/meta.go` 的冻结字符串）与它们落进模型请求时的角色，由 `agent.RoleOf` 决定——只有消息自身没带角色时才按类型补：

| 类型常量 | 值 | `RoleOf` |
|---|---|---|
| `TypeExternalInput` | `external_input` | `user` |
| `TypeAgentOutput` | `agent_output` | `assistant` |
| `TypeActionCommand` | `action_command` | `tool` |
| `TypeThinkingPlan` | `thinking_plan` | `assistant` |
| `TypeContextCompress` | `context_compress` | `user` |
| `TypeTaskSettled` | `task_settled` | `tool` |
| `TypeFeedback` | `feedback` | `user` |
| `TypeGovernance` | `governance` | `user` |

**Notice 族**是任务通知的词汇（`runtime/task/registry.go`），扩展者只需认身份而不必碰载荷：`task.Notice{TaskID, Round, Kind, Seq, Event}`，`Kind` 取 `accepted` / `progress` / `settled` / `failed` / `cancelled` 之一。通知自己的身份是 **`TaskID + Round + Kind + Seq`**，`Event` 指向已提交的事实供消费者还原输入，但**不替代**通知身份：同一身份重复投递不记录第二个事实，重试保留原身份。投递路由另存 `task.Envelope{Notice, Target, Source, TargetInstance}`，目标地址与目标实例在提交那一刻冻结，重建或移交的实例不会重路由一条已提交的通知。

---
<a id="event-consumption"></a>
## 三、event-loop：Ready → 消费 → Backflow → 投递

扩展者只需做两件事：把事件放进某个 invocation 的 ready 输入，并为终态通知准备一个投递面。消费点只有这一条链（`runtime/agent/loop.go`）：

1. **`Invocation.Ready []event.Event`** 是一次责任范围**独占**的输入。`Loop.Drive` 把它拼在会话历史之后交给 `aggregateReady`：事件的 `Message` 原样保留（多工具 assistant 消息与逐调用配对的工具消息不被拆开），只有自身没带角色的消息才用 `agent.RoleOf` 按类型补。
2. **消费即一次决策**。模型产出的工具调用经受理路径变成**受理回执**与**结算观察**，两者在下一次决策前进入 transcript；当一次决策既没有回执也没有观察时，这一趟以终态收束并提交 transcript。
3. **`Loop.Backflow(invocationID, notice)` 只驱动发起了该调用的那次 invocation 的下一次决策**，不驱动别的。它只接受 `settled` / `failed` / `cancelled` 三种终态：非终态通知没有可配对的结果，在这里被忽略，而不是被当作空结果。
4. **投递面由宿主给**：`task.WithEnvelopeDeliverer` 把 envelope 交给宿主，`Run` 的 sink 把工作通知记进邮箱（`run_fact.go`）。loop 自己不选传输、不猜目标。

**自定义事件类型就是运行期消息**：`Ready` 里没有类型白名单，一个不在上表里的类型（例如宿主自己的 `meditation_tick`）照样被消费成它自己的那条消息、照样能调用模型与工具、照样经 `Backflow` 回填。唯一不许的是"消费了但什么都没产出还静默返回"：空结果必须以具名错误拒绝投递，而不是投一条空消息——对照用例 `tests/integration/execution/custom_event_loop_test.go` 用 `errCustomEventNoResult` 固定这条语义，S12 用同一消费链固定通知定向（`tests/integration/acceptance/scenario_s12_test.go`）。

---
<a id="consumer-protocol"></a>
## 四、消费器协议：三个实例、六个面，一条协议

消费 event 的不止一处：回合驱动、时间驱动与宿主的外部驱动各跑一个循环。它们**共用一条协议**，差别只在驱动源（`runtime/agent/loop.go` 与 `evolution/meditation/reflection.go`）：

1. **读事实**：按身份引用（`event.EventID`）读一条已提交的事实，读的是事实本身，不是它的副本。
2. **消费**：用这条事实做一次执行——回合驱动的一次决策、时间驱动的一次反思判定、外部驱动的一轮宿主消费。
3. **产新事实**：产物以**自己的身份**提交到持久层，并在自己的证据里引用读到的身份（决策记录的 `InputEvents`、反思扫描的判定依据）；产物与源事实是两条事实。
4. **不许的两件事**：不许修改源事实——事实不可变、产出即锁定；不许绕过持久层——产物必须作为事实提交，只在内存里返回一个值不算产出。

| 实例 | 驱动源 | 读的事实 | 产出的新事实（族） |
|---|---|---|---|
| `Loop.Drive`（回合驱动，`runtime/agent/loop.go`） | 一次 invocation 的 `Ready` 输入，以及终态通知的 `Backflow` 回填 | ready 输入里的事件，身份即 `event.EventID` | 决策记录与尝试记录（`rl.DecisionRecord` / `rl.AttemptRecord`，经 `agent.Recorder` 在产生点提交） |
| `Reflection.Tick`（时间驱动，`evolution/meditation/reflection.go`） | 宿主按节拍调用 `Tick`，节奏下限由 `ReflectionCadence` 判定 | 事实链上 watermark 之后、lineage 非自管的已提交事实（观察面） | 反思输入事件（`event.TypeExternalInput`，lineage 为 reflection）；它再交给回合驱动消费 |
| 宿主循环（外部驱动） | 宿主自己的调度与传输（`Run` 的 sink、`task.WithEnvelopeDeliverer`） | 宿主选中并交进 `Ready` 的事实 | 宿主自己提交的事实：框架在此只给边界，不给循环 |

回合实例与时间实例**不互相收编**：`Tick` 没有 Invocation 与决策语义，把它塞进 `Loop` 的一个相位会造出一个不存在的相位。两个框架内实例由同一条协议用例固定——各读一条事实、各产一条新事实、源事实不变（`tests/integration/evolution/consumer_protocol_test.go`）。

第二辑把同一条协议钉在框架内的**六个消费面**上：它们不各跑一个循环，而是各读一处已提交的事实身份，按自己的义务产/投递/裁决/收尾/观察/导出。六个面各有一个同名协议用例 `TestSecondWaveConsumersShareTheProtocol`，分住在 `tests/integration/` 的 inspection、state、execution、host、learning 五个归属包。

| 面 | 归属（包·入口） | 驱动源 | 产出事实族 | 禁止项 |
|---|---|---|---|---|
| Subscribe（推消费） | `query`·`Projection.Follow` | 一次 `Snapshot` 建立的订阅（`Subscription{Revision, Filter}`）之后的增量 | 无：增量是本代投影的读视图（upsert/delete/gap/reset），不登记族 | 不改写源；gap 与 reset 必须具名，不许静默跳过；不许跨代续订 |
| Deliverer（投递消费） | `task`·`EnvelopeDeliverer`（`WithEnvelopeDeliverer`） | `Flush` 面对的投递义务（`ObligationAwaitingDelivery`） | `task` 通知族的投递状态（同一通知身份不新增行，义务清账在台账） | 不重路由已冻结的 envelope；无显式接受即不清义务；拒绝必须留下原因 |
| Apply（裁决消费） | `state`·`Publisher.Publish` / `Rollback` | 候选（`Candidate`）与 `Governance.Check` 的裁决 | `state` 发布台账（`PublicationRecord`，代际＋指纹）；不新增任务事实链行 | 同指纹不得重复发布（`PublishUnchanged`）；不得跳过 `PublishContract.Required`；不改写候选 |
| Shutdown（收尾消费） | `agent`·`Shutdown.Close` | 关停请求（`CloseRequest{Address, Deadline}`）的结算趟 | 无新任务事实：`DrainReport` 是读数（`Drained`/`Unsettled`） | 不把 false 或超时读成结束；不越过 deadline 静默收尾；关停中不受理新输入 |
| Observer（观察消费） | `telemetry`·`SDKObserver.Observe`（`telemetry.NewCommitObserver` 的 After 趟；抽象端口在 `plugin`，实现归 telemetry 域） | 已提交事实的 `event.EventID`，提交之后 | 零：`AuditRecord` 是观测证据，不产事实 | 不产事实、不回滚、不重跑；不触碰源事实；OTel 关闭只记 skipped，审计仍须落地 |
| Exporter（导出消费） | `rl`·`TrainingExporter.Export` / `SealSnapshot` | 授权调用（`ExportRequest{PartitionIDs}`，授权先于任何读） | `rl` 制品事实：导出快照（`Provenance.ExportID` 是快照身份，行内引用源事实的 `EventKey`） | 不越权读；不改写源事实；不覆盖已封印身份（`ErrExportConflict`）；不把部分当完整 |

---
<a id="subagent-sources"></a>
## 五、call_subagent：两个装配源，二选一

扩展者只需给**恰好一个**源：一个 agents 文件，或一组显式参数。两者同给不是"后者覆盖前者"，而是具名拒绝。

**源一：agents 文件**（frontmatter 是闭集 `name` / `description` / `model` / `prompt` / `skills` / `tools` / `mcp`，正文即提示词）：

```markdown
---
name: reviewer
description: reviews one change
model: model-reviewer
prompt: ./prompts/reviewer.md
tools: [read_file, search_content]
---
review the change and answer with the findings
```

`prompt` 以 `./`、`../` 或 `/` 开头时按文件读取，否则按字面文本使用。文件形状与字段都由 `tool/subagent/agentsfile.go` 的严格解码判定。

**源二：显式参数**（调用参数，不是声明文件）：

```json
{
  "name": "reviewer",
  "description": "reviews one change",
  "model": "model-reviewer",
  "prompt": "review the change and answer with the findings",
  "skills": ["code-review"],
  "tools": ["read_file", "search_content"],
  "mcp": [],
  "input": "the change under review"
}
```

两个源共享同一条继承规则（`tool/subagent/assemble.go`）：**声明了的字段覆盖父级同名字段，没声明的字段继承父级**；`name` 永不继承，缺 `name` 即拒绝。`input` 是委派回合的起始请求，留空则只跑装配出的 instruction。

**拒绝矩阵**（全部以 `[call_subagent_denied] <原因>; no task was admitted` 作答，且零任务）：

| 情形 | 具名原因 |
|---|---|
| 两个源同给 | `both an agents file and explicit parameters were given` |
| 一个源都没给 | `no assembly source was given` |
| agents 文件或提示文件读不到 | `the file cannot be read` |
| 没有 `name` | `the name is required` |
| 字段形状不是该形态声明的形状 | `the field has the wrong shape` |
| frontmatter 出现闭集外的键 | `unknown field` |
| frontmatter 围栏/映射/YAML 形状不成立 | `the agents file shape is invalid` |
| 参数名不在工具声明内 | `call_subagent: the arguments do not match the declaration` |
| 委派面拒绝（未受理） | 委派面自己的具名错误 |

**受理不是结果**：一次合法调用立刻返回 `task.AdmissionReceipt`（任务身份），委派回合在受理旁边跑；进度与终态以通知回来，目标是发起调用的那个地址。装配完成后的子 agent 按**它自己的** model / prompt / tools 运行，父的声明只提供继承缺省。

---
<a id="preset-tools"></a>
## 六、预置工具的声明写法

扩展者只需在声明的 `tools:` 里引用预置名，参数名是闭集；引用一个预置名之外的引用不会被拒，它留给宿主贡献的工具解析。

```yaml
entry: agent-preset
agents:
  - id: agent-preset
    model: model-preset
    files:
      - path: ./prompts/agent.md
        kind: prompt
    tools:
      - name: knowledge
        params:
          source: ./docs
          max_results: "3"
          min_score: "0.2"
      - name: recall
      - name: call_subagent
```

| 引用名 | 参数 | 与什么有关系 |
|---|---|---|
| `knowledge` | `source`（本地目录或文件，逗号分隔）、`max_results`、`min_score`、`tool`（`knowledge_search` 或 `code_search`） | tRPC knowledge 栈：源读入内存向量库，工具答案编码成文本回给模型；远程 `endpoint` 未接线 |
| `recall` | 无 | 运行期注入的记忆检索面（`PresetOptions.Recall`），不是本包自己的检索 |
| `call_subagent` | 无 | 组合根接好的 `call_subagent` 工具（`PresetOptions.Subagent`） |

拒绝都点名：`preset: the declared tool carries no wiring`（声明了 `recall` 或 `call_subagent` 却没有对应面）、`knowledge: the reference names no local source`、`knowledge: a remote endpoint is not wired`、`knowledge: the reference names no known tool`、`preset: the parameter value cannot be read`。契约在 `tool/subagent/presets.go`，装配面核对见 `tests/integration/config/preset_tools_test.go`。

**当前边界（写在这里以免扩展者猜）**：pipeline 元素由宿主经 `tagent.NativeAgent.Plugins` 贡献，声明里的 `plugins:` 名单是声明侧的次序意图，`tagent.Assemble` 不把它解析成元素。所以"声明序"要生效，宿主按同一名单贡献元素；S11 正是这么驱动的（`tests/integration/acceptance/scenario_s11_test.go`）。运行手册与门禁清单见 `docs/OPERATION.md`。

---

<a id="reduction-map"></a>
## 七、化归判据对照表：17 面三问归位

新增能力先过 `docs/FACT-VOCABULARY.md` 的三问（**是不是事实 / 谁消费 / 什么时态**），每一面只归到一个轴：**事实**（产出即锁定的族）、**消费**（只读或投递别人的事实）、**时态**（存续/在途/回放/产物）、**执行器**（跑一次执行）、**边界**（端口与组合）。口径：module 根加 16 个一级产品族（`tool` 的 exec/mcp/remote/subagent 四个适配器算一族，`memory` 的存储档与检索子包算子包；`prototype` 是历史参照，不在产品面内）。

| # | 面（包） | ① 是不是事实 | ② 谁消费它 | ③ 什么时态 | 归位轴 |
|---|---|---|---|---|---|
| 1 | `tagent`（module 根） | 否：只装配与发布 | 宿主导入入口；`Assemble` 读声明 | 无（组合） | 边界 |
| 2 | `agent` | 否：消费 ready 与回填，产物经 `rl.Recorder` 提交 | 回合驱动自己消费；宿主读回执与终态 | 在途（决策） | 执行器 |
| 3 | `config` | 是：声明族（规范指纹身份） | 组合根与热重载候选；`state` 读绑定 | 存续 | 事实 |
| 4 | `event` | 是：核心事件族（`Key` 身份） | 全部消费面的输入词汇 | 事实本体 | 事实 |
| 5 | `evolution` | 否：产物是 `TypeExternalInput` 事件，归 `event` 族 | `Tick` 读事实链；`Loop` 再消费它产出的输入 | 回放（watermark 之后） | 消费 |
| 6 | `governance` | 否：`Decision` 是裁决读数，不产事实 | 执行器读裁决；审批通道读 hold；审计面读事件日志 | 观测 | 消费 |
| 7 | `memory` | 是：引用票据族（原文 `Key` 身份） | 组装与检索面读；策展消费读压缩请求 | 存续 | 时态 |
| 8 | `model` | 否：锁定 tRPC 原生模型协议 | `agent` 的单次决策调用 | 无（边界） | 边界 |
| 9 | `plugin` | 否：三拓展点的窄口抽象（相位、Observer/AuditSink、Governor），实现分派到各领域 | pipeline 按相位派发；组合根注入各领域实现 | 观测 | 消费 |
| 10 | `query` | 否：只读投影（快照/游标/订阅） | 模型/SDK/CLI/诊断/看板/训练 | 回放 | 时态 |
| 11 | `rl` | 是：训练记录三族＋导出快照制品 | 训练导出、SFT 消费、看板 | 在途（记录）/ 制品（快照） | 事实 |
| 12 | `state` | 否：绑定、邮箱、发布台账的唯一持有者 | `agent`/`task`/`query` 读绑定与发布代 | 存续 | 时态 |
| 13 | `task` | 是：通知族（四元组身份） | `Deliverer`、`Loop` 回填、关停收尾 | 在途（义务） | 事实 |
| 14 | `telemetry` | 否：观测旁路，只读消费 | audit sink 与 OTel 后端读记录 | 观测 | 消费 |
| 15 | `tool`（exec/mcp/remote/subagent） | 否：执行适配；调用记录落 `event` | `agent` 经工具面执行调用 | 无（执行边界） | 执行器 |
| 16 | `view` | 否：呈现词汇（`Presentation`/`RenderedView`） | 宿主通道读呈现 | 产物 | 时态 |
| 17 | `memory/replica` | 否：临时副本，不持真源 | `memory` 与 `tool` 读副本 | 存续（临时） | 时态 |

对照结论：本变更只重排压缩三件（01，落在 `memory`/`memory/replica`/`tool` 三个面内），其余面按上表归位、不改行为；六个消费面的声明见第四节，产物的事实族归属见 `docs/FACT-VOCABULARY.md` 的附表。三问答不出来就不是新能力，而是既有能力的字段——这是本表要拦的那类偏差。

---
<a id="rl-verifier"></a>
## 八、rl Verifier：开放域判分

`rl.Verifier` 是开放域判分窄口：`Verify(ctx, EpisodeRecord) (Verification, error)` 一个 episode 记录进、一个 `Verification{Score, Passed, Reasons}` 出，不比对固定答案集。默认实现 `HeuristicVerifier` 只看记录自带的可验证信号（身份、会话证据、lineage 引用一致性与依赖票据），离线可跑；`WithLLMJudge` 注入模型判分，`NewVerificationConsumer` 把判分写成 `RewardRecord`（`Value` 是分，`Verdict.Passed`/`Verdict.Reasons` 是判决），不新增第四种记录族。契约在 `rl/verifier.go`，范例见 `examples/contract/rl/Verifier_example.go`。
