> **本篇第一节是当期契约**：Go 注释里的 `// 契约: docs/wiki/plugin/plugin-architecture.md#overview` 指向的正是 §1.1。第二节起是已移除实现的压缩重写，原版可按 [docs/archive/wiki/PRE-REWRITE-MANIFEST.md](../../archive/wiki/PRE-REWRITE-MANIFEST.md) 记录的提交取回；当期包面以 `api/plugin` 码面为准（[docs/api/](../../api/index.md)），当前架构入口见 [docs/wiki/README.md](../README.md#当前架构速览)。

# tagent/plugin：两相位材料流与窄口扩展点

`tagent/plugin` 的形态是 **`api/plugin` 声明的一条材料流**：tagent 通过它在框架事件流的三个位置注入差异化能力——执行前的一次调用变换与一次风险裁决，请求装配时的一次环境块贡献，提交后的一次观测、一次呈现与一次压缩触发。本页只留契约、判据与历史对照；过程叙事与代码清单在原版归档中。

## 一、材料流（当期契约）

<a id="overview"></a>
### 1.1 词汇与窄接口

一个 **plugin 是注册单元**（`plugin.Instance`）：它报告一个稳定名字与**恰好一个** `plugin.Port`，并实现该端口的那**一个窄接口**。端口是一个具名常量——`plugin.PortCallTransform` 到 `plugin.PortFactCompression` 六个之一，常量名直接拼出它代表的转向：读的那一面材料挨着答的那一面产物，闭集即派发依据；`plugin.NewFlow` 按声明序收编它们——**窄接口与派发趟都是端口的读法，不是时间位置**。

| 端口（具名 `plugin.Port` 常量） | 窄接口 | 时点与判据 |
|---|---|---|
| `call->call` | `TransformCall` | 副作用之前、准入之前，每 call 一次；改工具名/参数，**不得改 call 身份（CallID 与已解析的信任域）** |
| `call->ruling` | `RiskAssessor` + `Governor` | 同一裁定的两张脸：`AssessRisk` 出 grade，`Governor` 在 grade 上出唯一执行前裁定；grade 只读一次，绝不重算 |
| `request->environment` | `EnvironmentInject` | 请求装配的 InjectEnvironment 趟；每块是一个产物，回查发起它的请求；不写运行时、不改请求 |
| `fact->observation` | `PostCommitObserver` | 事实的规范提交**之后**，只读旁路；失败只记具名 gap，不回滚、不重跑、不写事实 |
| `fact->view` | `FinalPresenter` | 为渠道渲染呈现文本并回答是否被丢弃；**丢弃必须报告**，不撤回事实 |
| `fact->compression` | `CompressTrigger` | 已归档压缩请求的 TriggerCompress 趟；请求身份与 subject 保持原样，本面不压缩、不退役请求 |

六个端口就是闭合表：`call->call`、`call->ruling`、`request->environment` 自读到的材料长出一个产物，`fact->observation`、`fact->view`、`fact->compression` 对一个已提交材料作答。闭合表之外的名字一律按名拒绝，连读材料、产产物分开声明的写法也不再存在。

红线（码面 `Instance` doc 的三条）：必须报告自己服务的那一个端口并实现它的窄接口；不得报告未实现其窄接口的端口；**一个注册元素只服务一个端口**（风险端口可同时带两张脸）。

`Before` 的语义：每个 transform 在自己的**私有副本**上改正，随后风险面在改正后的 call 上打分，pass 回答最终 call 与**最强** grade。任一 transform 报错即拒绝整个 pass（后续面不跑、不裁定、不执行、不记录）；`final.ID != call.ID` 以 `ErrCallIdentityChanged` 具名拒绝。空 grade 不是一种 grade，裁定读作 `RiskMedium`，因此旁边没有风险面的 call 仍按普通变更调用被裁。

`After` 的语义：先跑全部 observer，再跑全部 presenter；观测失败进 `view.Presentation.Gaps`，presenter 丢弃只丢视图。**pass 不返回 error**——这里的失败只能是 gap 或 dropped view，绝不是碰已提交事实的理由。

`TriggerCompress` 与 `InjectEnvironment` 各自成趟：前者驱动一个已归档压缩请求的映射链（失败即拒绝该请求，材料不压缩、请求保持待处理），后者按声明序追加环境块（空面必须如实报空，单面失败即拒绝这次装配）。

**装配上的四条拒绝**（`plugin.NewFlow` 原文）：nil 实例、端口不在闭合表内、端口合法但没实现该端口的窄接口、同一个名字声明两次——各自按名拒绝，不做静默修剪，因为一个产物的名字必须溯回写它的那一个元素。

<a id="memory-plugin"></a>
## 二、MemoryPlugin — 事件持久化（历史机制）

`MemoryPlugin` 是原版的事件持久化插件：推断类型、生成 Snowflake EventKey、盖归因章、写因果链、把持久票据写回 `StateDelta`。当期已移除；以下是其**语义判据**，仍是对"事件如何成为事实"的有效约束。

### 2.1 四道跳过闸在任何分配之前

入口顺序做完四道跳过判定（逐条条件见 §六），**全部发生在分配 EventKey 之前**：任何一道命中就直接返回原事件——**不落库、不投影、不发票据**。回显因此不会被双写成两条事实，半条事件也不会占据因果链的一格。

### 2.2 提交序列

分配之后的整段落在**同一条因果键的锁段内**串行：

| 序 | 动作 | 失败/缺失时的行为 |
|---|---|---|
| 1 | 读该因果域最后已提交的键作父（无记录返回 0） | 0＝首次／重启后／游标被淘汰：**不猜父**，缺口由回溯端具名报出 |
| 2 | 分配 Snowflake EventKey，构造 `FullEvent`（assistant 正文经 `sanitizeAssistantContent` 剥模型伪造的 `[evt_...]` 前缀，其余角色逐字存） | — |
| 3 | 盖归因章：先 `agent_name`，再叠加 `AttributionFrom(ctx)` 的 rollout_id/trace_id/span_id/bundle_id | 归因缺失不阻断提交 |
| 4 | `StoreEvent` | 失败即 `stored=false`：ERROR 留痕；带凭据的回合 `MarkRejected` |
| 5 | 仅 `stored`：`MemoryRelations.SetParent` | 关系失败**不回滚内容** |
| 6 | 仅 `stored`：向本调用的 `ProjectionSink` `Append` EventReference | 未接 sink 时跳过——**投影与存储同点，绝不先投影后存储** |
| 7 | 提交闸：按 `stored` 发布或撤回持久票据 | 见 §四 |
| 8 | 仅 `stored`：推进因果游标 | 失败键永不成为下一条的父 |

判据都经真实框架回显验证：少跳会双写，多跳会丢真事实。`isExpectedInputEcho` 要求根调用、`Author=="user"`、消息角色为 user 且去空白后与凭据里的合并输入逐字相等——子调用与助手/工具事件一律不满足，走正常存储路径。

<a id="causal-mech"></a>
## 三、因果链机制

每个事件经 `MemoryRelations.SetParent(childKey, parentKey)` 维护前驱，构成有向链；父关系**不进事件字段**。

- **同因果键线性化**：父键读取、事件键分配、落库、关系写入、游标推进落在同一条顺序里。两条并发提交若各读各的父，会造出分叉链或指向虚事件的父引用。串行单元是 `causalGuard`（每个 `<partition>:<session>` 一条），**不同因果键互不阻塞**。
- **锁序**：持 `p.mu` 只登记引用计数 → 放 `p.mu` → 取键锁；`p.mu` 是叶锁，绝不横跨存储 I/O。
- **淘汰保护**：`refs` 同时是保护凭据——游标 map 超上界回收最久未更新的键时跳过 `refs>0` 的因果键；淘汰一条在途链的锚等于让它失去父。
- **关系写失败不回滚内容**：事实与票据照常有效（内容已提交这件事为真），只缺一条因果边；说成"提交失败"要撤回一条真实存在的事实。回溯端读到断边时以具名 partial 报出（见[记忆架构](../memory/memory-architecture.md#causal-chain)）。

<a id="commit-gate"></a>
## 四、提交闸：票据、投影与游标只在提交成功之后发布

`event_key` 与 `partition_id` 写在 `StateDelta` 里是**持久票据**——下游拿它去 `GetEvent` 取原文、拿它作"这条历史可以回补"的承诺，因此只能随成功的提交发布：

| 发布物 | `stored=true` | `stored=false`（写失败或未接存储） |
|---|---|---|
| `StateDelta[event_key]`、`StateDelta[partition_id]` | 写入（hex 契约） | **连本插件在同一 map 里写入的同名字段一并 delete**，只 Debugf 留痕 |
| 本调用投影 `sink.Append` | 追加 | 不追加（投影里永不出现取不回的事实） |
| 因果游标 `lastEventKeys` | 推进到本键 | 不推进（失败键从未落库，作父即断链） |
| `event_type` / `event_summary` | 写入 | **照常写入** |

难点全在最后两行的分界：票据与分类同处一个 map、同一次落库调用。判据不是"哪个字段重要"，而是**该字段是否主张"已持久"——主张的走闸，只作分类的照旧返回**。把分类一并撤掉，读者会把"存储故障"误读成"这条事件没有类型"，反而更难诊断。

`call_id` 是可选注入面（`WithCallIDResolver`），**只在解析器已装且 `evt.Response.ID` 非空时问一次**；返回 false、返回空串、未装配三种情况一律**不写这个键**——取"最近一次调用"会把反馈挂到一次从未产生它的调用上。它写在 `FullEvent.Metadata`、**不进 `StateDelta`**，且盖章发生在提交闸之前（随事实本体一次落库，不是事后补写）。

<a id="summary-plugin"></a>
## 五、SummaryPlugin — Tag 与元数据标注

原版在 `MemoryPlugin` 之前执行，给事件追加可读 `Tag`（`evt.Tag += ";" + tag`，支持多插件追加，不覆盖）。职责边界一句话：**原文视图，非内容总结**——内容级总结收归压缩固化时刻。

| EventType | 摘要策略 | 原因 |
|---|---|---|
| `external_input` / `agent_output` / `thinking_plan` | **原文全文** | 保留用户意图、回复与完整思考（含工具调用决策） |
| `action_command` | 工具调用摘要（`"调用工具: name(args)"`，多个逗号分隔） | 工具结果信息密度高，单行省 token |
| 其他 | **原文全文** | fallback 保安全 |

**严格拒绝非设计折损**：摘要本身已是设计内的信息折损（原文→摘要），截断是设计外的双重折损，会破坏 SmartCompress 的压缩质量。原版因此完全移除了 `DefaultMaxContentLength`/`DefaultMaxArgsLength`/`formatContent` 等截断面；内容超限交给多次 SmartCompress 循环。

事件类型推断：`RoleUser`／`RoleSystem`（TmuxMonitor 经 `Runner.Run()` 注入）→ `external_input`；`RoleAssistant` 带 `ToolCalls` → `thinking_plan`；`RoleAssistant` → `agent_output`；`RoleTool` → `action_command`；无 Response 或无 Choices → `external_input`。System Prompt 由 InstructionProcessor 注入 Request，**不参与事件流**，不因压缩丢失。

<a id="skip-set"></a>
## 六、跳过集：哪些事件不入存储也不进投影

`MemoryPlugin.onEvent` 在分配任何 key、做任何写入**之前**先过四道闸，顺序即语义：

| 闸 | 条件 | 为什么必须跳 |
|---|---|---|
| 无载荷 | `evt == nil`，或 `Response == nil`、`Choices` 为空 | runner/flow 会发同步用的屏障事件；入库会被推断成空内容的 external_input，在投影里变成误导性的 user 占位，挤掉真实上下文 |
| 流式分片 | `Response.IsPartial` | 只有聚合后的事件才入库/进投影；中间 delta 内容为空、tool_calls 未聚合 |
| 退化空终态 | 推断为 `agent_output` 且 `Content` 为空 | 不带任何信息；入库会在投影与历史里留一条空 assistant 消息。工具调用轮（有 tool_calls）与非空终态不受影响 |
| 精确回显 | 见 §七 | 事件循环已把该输入作为事实提交，重复入库即双写 |

**跳过与持久化/投影是同一同步点**：存储成功之后才在相同位置调用 `ProjectionSink.Append`，因此"投影完成于 `BeforeModel`"由构造保证而非时序巧合；投影自身按 EventKey 幂等，重投递无害。存储标识随后写回 `Event.StateDelta`，键名由 `tagent/event` 一处定义（见[元数据键的归属](../event/event-architecture.md#metadata-keys)）。

<a id="echo-credential"></a>
## 七、精确回显凭据 `EchoCredential`

事件循环在调模型前提交本批的逐消息事实，框架随后把**合并后的输入**经插件管线回显；只有这一次回显应当跳过入库，因此按**每次 runner 尝试**新造一枚凭据（不再有"整回合/首个信封/任意 user"这类粗粒度状态）。

- 字段：`AttemptToken`（重试即新造，事实复用）、`Agent`、`Session`、`MergedMessage`（规范化比较目标）、`CommittedKeys`（本批已持久化的事实键）。
- `isExpectedInputEcho` 要求同时满足：**根调用**（无父调用，只有根会回显回合输入）、`Author == "user"`、消息角色为 user、内容与 `MergedMessage` 在去空白规范化后相等。
- 取舍：仅图无文的 durable 输入其 `Content` 为空，内容相等也会匹配"空的 user 事件"。这**可接受且必要**——不跳会把循环已提交的合并图像输入回显重复写入；被多跳掉的只是不含信息的空事件。parts 级精确性由端到端测验证，不在此猜测（未验证的 parts 匹配会带来"少跳"→双写风险）。
- `Bind` 记录首次匹配所在的**根调用 id**（幂等，后续匹配不覆盖）——`event.Event` 不携带逐事件 id，"本次尝试的输入回显"的稳定身份只能是根调用 id。
- `Verified()` 为真要求"已绑定且未被拒绝"。**门禁语义**：本回合装入了凭据但 `Verified()` 为假时，模型入口被挡住；`MarkRejected(reason)` 把凭据降级为不可验证（粘滞，首个原因胜出）：框架对插件错误只记日志并继续，所以被吞掉的存储失败必须让本回合**过不了提交门（fail-closed）**。`nil` store 的旁路场景不算错误，不触发降级。

<a id="attribution-carrier"></a>
## 八、归因章与其载体

`Attribution` 是回合级键值对，写入 `FullEvent.Metadata`，使任意产出事件可回溯到产生它的版本上下文（`bundle_id`/`rollout_id`/`agent`）。它填补了"`FullEvent.Metadata` 在生产代码中从未被填充"这一事实缺口，是"可归因/可回滚"与事件维度可观测的共同地基。

- 载体模式与 `ProjectionSink` 相同：`RunFlow` 每回合绑定，插件在存储同步点读取并写入；**两条持久化路径**（插件管线 `onEvent` 与 `persistBusEvent`）都必须盖章，否则出现归因盲区。
- 基线章为 `agent_name`（来自 invocation，立即可用）；ctx 归因叠加其上。未注入归因时只盖基线，行为向后兼容；空归因不写入 ctx（省一次分配）。
- 子 ctx 的归因不污染父 ctx：每次调用的绑定由各自 call-chain ctx 隔离。

<a id="causal-chain"></a>
## 九、因果链边界与助手内容的存储边界

### 9.1 因果链按 (partition, session) 独立且必须有界

`lastEventKeys` 以 `"partitionID:sessionID"` 为键维护各自的因果链，避免子 agent 与跨会话事件互相破坏父子关系。该 map **必须有上界**（`maxLastEventKeys` = 4096）：长寿命 agent 会不断累积会话键，无上界即泄漏。溢出时按事件 key 淘汰**最小值**者——分区内 int64 事件 key 随时间单调，最小 key 即最久未更新的链。

这条查找语义是整个因果存储的承重契约：**查找一个因果键，只能返回该键最后一次写入的 key，或者 0（不存在）——绝不返回别的会话的 key**。淘汰只允许把一条链降级为「无父的新链」，绝不允许把它悄悄接到某个不相关的前驱上；"被驱逐后再复活"的会话必须从 0 起链。

### 9.2 助手内容的存储边界

模型会在输出里**编造** `[evt_...]` 前缀（模仿投影呈现格式）。这类伪造前缀若被存下，会在后续压缩/召回里被当作真实引用参与解析。故在存储边界统一剥离伪造前缀（**只作用于 assistant 正文，其他角色逐字保留**），并记 warn 以便发现提示词被模仿的情况。

### 9.3 Session 与 MemoryStore 的分工（原版结论，仍然成立）

Session 是框架提供的通用会话管理（完整 `event.Event`、`StateDelta` 累积、单 Session 范围）；MemoryStore 是 tagent 的差异化能力（`MemoryRelations` 因果图、`GetEvent(key)` 直接定位、按类型/时间/跨 Session 检索、`FullEvent.ToolCalls` 不随 LLM 视图变化）。**两者互补而非替代**：`MemoryPlugin` 是桥（FullEvent 入 MemoryStore，EventKey/EventType 经 `StateDelta` 合并进 `Session.State`），SmartCompress 只改发给 LLM 的 messages 视图，两者都不被修改。

## 十、历史机制对照

| 原版机制 | 当期状态 |
|---|---|
| `MemoryPlugin` / `SummaryPlugin` 两个 OnEvent 钩子 | 事件持久化与摘要视图归 `memory` 的写入路径，`plugin` 只留两相位六端口的扩展点 |
| `ProjectionSink`、`StateDelta[event_key]` 持久票据 | 归入 `memory` 同点投影与事件编号契约，判据（不猜父/不回滚/同点）保留 |
| SmartCompress 两阶段压缩、TokenCounter | 当期压缩归 `runtime/agent` 的压缩面；本页只留"摘要=原文视图、截断是设计外折损"的边界 |
| `QueryOptions`/`MemoryStore` 存储实现细节 | 归 `memory` 篇与 [docs/api/](../../api/index.md)，本页不复制 |

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|------|-----------|---------|
| **插件顺序契约隐式** | 管线内顺序由注册顺序决定，跨相位依赖（如变换必须先于裁定）由常量顺序保证；新增元素时需人工核对 | 注册时声明 requires/provides，装配期校验 |
| **summary 双计算** | 原版 MemoryPlugin/SummaryPlugin 各计算一次（纯函数结果一致） | 由单一视图构造点产出，消费方复用 |
