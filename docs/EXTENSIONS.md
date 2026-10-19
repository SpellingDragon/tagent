# tagent 扩展点（EXTENSIONS）

**这份文档面向扩展者**：读完之后应当知道三件事——每个扩展点上自己**只**需要实现什么、实现的东西**与什么有关系**、以及什么会被**具名拒绝**。它是长期事实文档，每一条都与源码注释同在：这里的名字、序列与拒绝原文都能在对应包内逐字核到。

**姿态一句话**：tagent 的扩展面是**窄口**——一个扩展点只承认一件事，越界的写法不是"没生效"，而是启动或调用时的具名拒绝。四处扩展点互不重叠：plugin 只管一次调用的 Before 趟与一次提交的 After 趟，event 只管词汇与投递身份，event-loop 只管消费与回填，`call_subagent` 只管把一个子 agent 装配成任务。

---

<a id="pipeline-phases"></a>
## 一、plugin：四个相位，每个相位一个窄接口

扩展者只需做两件事：实现 `plugin.Plugin`（报一个稳定名字**和恰好一个**相位），再实现该相位的那一个窄接口。相位既决定元素在 pipeline 里的次序，也决定它被派发进哪个接口集；`plugin.NewPipeline` 按声明序分组时逐个核对，越界即拒绝。

| 相位 | 扩展者实现的接口 | 出现在哪一趟 | 与什么有关系 | 拒绝语义 |
|---|---|---|---|---|
| `PhaseCallTransform` | `TransformCall(ctx, *model.ToolCall) error` | Before 趟，admission 之前、副作用之前 | 这一次工具调用本身：可改工具名与参数，不改 `CallID` 与已解析的可信作用域 | 返回 error 即拒绝**整趟**：该调用不执行、不记录，也不产生 `CallPass`；移动 `CallID` 报 `plugin.ErrCallIdentityChanged` |
| `PhaseRiskPolicy` | `RiskAssessor.AssessRisk(ctx, *model.ToolCall) RiskLevel` 与 `Governor` | Before 趟，全部 transform 之后 | 同一个调用的风险等级与执行前裁决：等级只评级一次，裁决读它而不重算 | 相位内两个面可只实现其一或都实现，一个都不实现按"相位无实现"拒绝 |
| `PhasePostCommitObserve` | `ObserveCommit(ctx, event.EventID) error` | After 趟，先于全部 presenter | 一个**已提交**事实的只读观察 | 失败不返回错误、不回滚事实：失败落成 `plugin.ObservationGap`（插件名+事实+错误） |
| `PhaseFinalPresentation` | `Present(ctx, event.EventID) (string, bool)` | After 趟，最后 | 一个已提交事实的呈现文本，以及通道是否丢弃它 | 丢弃只丢呈现（`RenderedView.Dropped`），不撤回事实；丢弃必须如实报出 |

两趟的边界是这段接口的全部意义：

- **Before 一趟**：每个调用恰好一次，先跑全部 transforms（各自在私有副本上改），再跑全部 risk faces，产出 `plugin.CallPass{Final, Risk}`。改后的 `Final` 是**被执行、也被记录**的那一个，且仍要过核心的受理路径（身份、权限、审批、去重都在核心，plugin 不自己执行调用）。没有风险面时 `Risk` 为空，裁决读作 `RiskMedium`——空等级不是一张免检通行证。
- **After 一趟**：提交之后才跑，先 observers 再 presenters，返回 `plugin.Presentation{Fact, Views, Gaps}` 且**没有 error 返回值**——这个趟的任何失败都不能成为动已提交事实的理由。被拒绝的调用与提交失败的调用不产生呈现。

装配上的三条拒绝（`plugin.NewPipeline` 的原文）：nil 元素报 `plugin: the pipeline holds a nil plugin`；相位不在四个声明相位内报 `plugin: %q serves phase %d, which is not one of the declared phases`；相位合法但没实现该相位的窄接口报 `plugin: %q serves phase %s but implements no narrow interface of that phase`。元素也不得服务两个相位：一个注册元素一个相位，相位的两个面可以同时带。

**与什么有关系**：`Pipeline` 每次派发从借用代（borrowed generation）的元素装配一次，请求中途不重组；所以扩展者看到的次序就是宿主贡献元素的次序，pipeline 不改写它。契约在 `plugin/phases.go` 与 `plugin/present.go`。

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

骨架类型（`event/meta.go` 的冻结字符串）与它们落进模型请求时的角色，由 `agent.RoleOf` 决定——只有消息自身没带角色时才按类型补：

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

**Notice 族**是任务通知的词汇（`task/registry.go`），扩展者只需认身份而不必碰载荷：`task.Notice{TaskID, Round, Kind, Seq, Event}`，`Kind` 取 `accepted` / `progress` / `settled` / `failed` / `cancelled` 之一。通知自己的身份是 **`TaskID + Round + Kind + Seq`**，`Event` 指向已提交的事实供消费者还原输入，但**不替代**通知身份：同一身份重复投递不记录第二个事实，重试保留原身份。投递路由另存 `task.NoticeEnvelope{Notice, Target, Source, TargetInstance}`，目标地址与目标实例在提交那一刻冻结，重建或移交的实例不会重路由一条已提交的通知。

---

<a id="event-consumption"></a>
## 三、event-loop：Ready → 消费 → Backflow → 投递

扩展者只需做两件事：把事件放进某个 invocation 的 ready 输入，并为终态通知准备一个投递面。消费点只有这一条链（`agent/loop.go`）：

1. **`Invocation.Ready []event.Event`** 是一次责任范围**独占**的输入。`Loop.Drive` 把它拼在会话历史之后交给 `aggregateReady`：事件的 `Message` 原样保留（多工具 assistant 消息与逐调用配对的工具消息不被拆开），只有自身没带角色的消息才用 `agent.RoleOf` 按类型补。
2. **消费即一次决策**。模型产出的工具调用经受理路径变成**受理回执**与**结算观察**，两者在下一次决策前进入 transcript；当一次决策既没有回执也没有观察时，这一趟以终态收束并提交 transcript。
3. **`Loop.Backflow(invocationID, notice)` 只驱动发起了该调用的那次 invocation 的下一次决策**，不驱动别的。它只接受 `settled` / `failed` / `cancelled` 三种终态：非终态通知没有可配对的结果，在这里被忽略，而不是被当作空结果。
4. **投递面由宿主给**：`task.WithEnvelopeDeliverer` 把 envelope 交给宿主，`Run` 的 sink 把工作通知记进邮箱（`run_fact.go`）。loop 自己不选传输、不猜目标。

**自定义事件类型就是运行期消息**：`Ready` 里没有类型白名单，一个不在上表里的类型（例如宿主自己的 `meditation_tick`）照样被消费成它自己的那条消息、照样能调用模型与工具、照样经 `Backflow` 回填。唯一不许的是"消费了但什么都没产出还静默返回"：空结果必须以具名错误拒绝投递，而不是投一条空消息——对照用例 `tests/integration/execution/custom_event_loop_test.go` 用 `errCustomEventNoResult` 固定这条语义，S12 用同一消费链固定通知定向（`tests/integration/acceptance/scenario_s12_test.go`）。

---

<a id="subagent-sources"></a>
## 四、call_subagent：两个装配源，二选一

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
## 五、预置工具的声明写法

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
