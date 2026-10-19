> **历史文档（historical-verbatim）**：本篇描述已移除的生产实现；当前架构见 [docs/wiki/README.md](../README.md#当前架构速览)。原版逐字保存在 git 历史（取回：`git show e4e884a48c6b9a5a70fcd13265e28d3aa55a67c8:docs/wiki/event/event-architecture.md`），指针清单见 [docs/archive/wiki/PRE-REWRITE-MANIFEST.md](../../archive/wiki/PRE-REWRITE-MANIFEST.md)；本篇是同一语义面的压缩重写，只保留契约、机制对照与判据。

# tagent/event 事件契约：类型注册表、元数据与投影

`tagent/event` 是 tagent 的**事件类型与元数据契约**包，为 `MemoryPlugin` 和 `SummaryPlugin` 提供统一的事件分类、`event_summary` 视图生成与事件元数据单点保障。它是纯工具层、**不持有任何状态**：所有函数都是纯函数（给定相同输入，总是产生相同输出），可并发安全调用。

<a id="overview"></a>
## 一、语义契约（红线）

### 1.1 `event_summary` 是元数据视图，不是内容总结

> **退位语义（历史上曾称 unified-memory-curation，记忆固化变更）**：尽管函数名含 "Summary"，它**不是内容总结**——多数事件类型下它就是原文，action_command 下是机械化的工具调用行。它用于展示与 recall 列表；内容级总结只在压缩固化时刻发生（骨架模型下：多段压缩 → 卡片行工程化提取，超限时 `curateCards` LLM 浓缩；legacy 路径保留 L3 → 段摘要 → 卡片行，素材律）。

`GenerateEventSummary` produces 的是 `event_summary` **元数据视图**，不是内容摘要：多数类型是原文逐字视图，`action_command` 是一行机械工具调用行。内容级压缩/综述属压缩与策展管线。历史函数名容易误读，故在此固定语义。

**严禁内容截断**：超出上下文的内容由多轮压缩处理，任何非设计的信息折损都会污染压缩质量（见「严格拒绝非设计折损」一节）。

### 1.2 严格拒绝非设计折损

**严格拒绝非设计折损**：内容级总结收归压缩固化时刻（素材律）；截断是设计外双重折损，会破坏压缩质量。

| 类别 | 示例 | 是否允许 |
|------|------|---------|
| **设计内折损** | EventSummary 生成时从完整内容到摘要 | 允许（压缩质量由压缩器保证） |
| **设计内折损** | 压缩阶段 LLM 生成摘要 | 允许（两阶段机制保真） |
| **设计外折损** | 截断超出限制的文本 | **禁止**（双重折损，破坏压缩质量） |
| **设计外折损** | 参数长度截断工具参数 | **禁止**（参数信息丢失） |

截断阈值与 `formatContent` 一类实现已被完全移除：溢出只走压缩路径（检测 Token 超阈值 → 压缩 → 重新发给 LLM），不靠截断。

### 1.3 类型静态属性的唯一权威源

`registry.go` 的 `EventTypeSpec` 注册表承担一个事件类型的全部静态属性：渲染角色、是否原文优先、摘要形态、是否压缩骨架、是否低价值、类型级 TTL、是否合成投影引用、是否纳入向量索引、是否可召回、是否永不进投影。**新增一个类型只需注册一条 spec**，摘要/骨架/TTL/低价值/角色/嵌入/召回全链路自动生效；既有函数（`IsSpecialEventType`、`GenerateEventSummary`、`EventTypeToRole`、`IsSkeletonMessage`）与跨包变量（记忆包的 `LowValueEventTypes`、lifecycle 的 `TypeTTL` 默认）一律委托或派生自它，不再各自维护枚举表。未注册类型经 `specOrDefault` 回退到 `defaultSpec`（角色 user、非 special、骨架保守 `true`、TTL 继承、可召回）。

| 字段 | 零值含义 | 消费方 |
|---|---|---|
| `TTLDays` | `0` 继承全局默认；`-1` 豁免遗忘；`>0` 显式天数 | memory 生命周期 |
| `Synthetic` | 合成投影引用，使用负 `EventKey`，非落库真实事件 | 存储、召回、渲染据正负 key 区分语义 |
| `Embeddable` / `Recallable` | 是否进入向量索引 / 票据能否取回原文 | 分别在 memory 与 tool/recall 消费，**声明归属注册表**（否则新增类型要改两个包） |
| `NonProjection` | 事实链内部记录，永不作为投影引用 | 经 `IsNonProjectionRecord` 统一判定 |

**类型名目的唯一真源**：`types.go` 的内置族常量、`wf_facts.go` 的 `wf.*` 家族、`inbox_receipt.go` 的回执类型；三处都在各自文件 `init()` 自注册。文档面**不复述取值与计数**——复制一份常量列表必然在新增类型时漏项。

### 1.4 非投影判定的单一入口

`IsNonProjectionRecord(eventType, metadata)` 是"某事件能否进投影"的**唯一判定源**：类型维度取注册表的 `NonProjection`，外加携带 `task_inline_record` 标记的事件（终态 settle 已在回合内作为 tool result 返回，再进投影即双呈现）。**正常提交、在线 spill 回补、冷启动重建三条 append 路径必须共用它**，不得再各写类型或标记枚举——历史上各写各的，正是投影与事实链漂移的来源。未知类型保守进投影（`defaultSpec.NonProjection = false`）。旧数据的兼容排除分支已随受管重置裁决删除：运行时只认当前格式，旧数据须经清点与显式 reset 处置。

<a id="timeline-prefix"></a>
### 1.5 时间线前缀是写读同包的一处契约

每条历史行的前缀形如 `[evt_<KEY>|<type>]`，`KEY` 用规范小写十六进制（负 key 保留前导 `-`）。**写入端 `FormatEventPrefix` 与读取端 `ParseEventKeyAndType` 同在 `event/timeline.go`**：生产方（时间线渲染）与消费方（压缩、保留引用扫描、召回取回）因此不可能各自演化出不同格式。`ParseEventKey` 还容忍模型回显票据时常见的形态（`0x` 前缀、`evt_` 前缀、完整的 `[evt_HEX|type]`、尾随 `|type` 或 `]`），并把无法解析者作为普通文本处理。召回键解析另有原生 fuzz 目标：`go test ./event/ -run FuzzParseEventKey -fuzz FuzzParseEventKey -fuzztime=30s`；确定性有界往返与全函数性断言在常规 `go test` 中执行，fuzz 只用于深挖，不作为门禁。

<a id="internal-retention"></a>
### 1.6 内部记录类型的保留不得被缩短

两类事件注册的目的**只是被动排除**，不是活跃功能：

- **`wf.*` 家族**：事实链是 workflow 运行时状态的唯一真源，包括信号在内的状态都必须先落为事实才产生效果；durable workflow 引擎已撤回，这些类型现存唯一作用是让历史 `wf.*` 记录继续被投影/召回/嵌入排除。因此其 `TTLDays` **必须保持 0**：写成正数会在 `DefaultTypeTTL` 里新增条目，**静默缩短**那些按全局/显式策略写入的历史记录的保留期。
- **`inbox_receipt`**：一条输入信封被确认消费的记账事实，真源是事实链而非 inbox 文件。它的 TTL 就是 request-id 的 **30 天去重窗口**——过期后同一 request-id 重投**不保证**幂等，这是明确边界而非缺陷。

## 二、类型推断与分类

<a id="const-list"></a>
### 2.1 按消息角色推断

`ExtractEventType(msg)` 只做角色到类型的映射；`IsSpecialEventType` 判断是否走原文视图。所有非 `agent_output` / `action_command` 的角色统一归为 `external_input`。

| msg.Role | ToolCalls | EventType | 说明 |
|----------|-----------|-----------|------|
| `RoleUser` | — | `external_input` | 用户输入 |
| `RoleSystem` | — | `external_input` | TmuxMonitor 注入 |
| `RoleAssistant` | `len > 0` | `thinking_plan` | Agent 思考/计划（带工具调用） |
| `RoleAssistant` | `len == 0` | `agent_output` | Agent 最终回复 |
| `RoleTool` | — | `action_command` | 工具执行结果 |
| 其他 | — | `external_input` | Fallback |

`RoleSystem` 的双重身份：System Prompt 由 InstructionProcessor 在初始化时注入，**不参与事件流**；TmuxMonitor 注入经 `InjectMessage` 进入 EventBus，**参与事件流**并分类为 `external_input`。

一条既不属于推断规则、也不属于注册表字段的事实须在文档面固定：`external_input` 收纳一切"非 Agent 回复、非工具结果"的输入——用户消息、宿主 API 注入、系统状态通知，以及后台任务结算通知。**结算通知靠 `[task settled]` 文本前缀识别**（压缩包的 `settleNoticePrefix`），它不是事件类型名；连续结算通知折叠成票据卡片的规则见 `settle_fold`。

### 2.2 特殊事件与普通事件

| 事件类型 | IsSpecialEventType | 摘要策略 | 原因 |
|---------|-------------------|---------|------|
| `external_input` | **true** | 原文全文 | 用户意图需完整保留 |
| `agent_output` | **true** | 原文全文 | Agent 回复需完整保留 |
| `thinking_plan` | **true** | 原文全文 | Agent 思考过程含工具调用决策 |
| `action_command` | false | 工具调用摘要 | 工具调用信息密度高 |
| `thinking_recall` | false | 原文全文 | 记忆召回内容需完整 |
| `thinking_knowledge` | false | 原文全文 | 知识检索内容需完整 |
| `context_compress` | false | 原文全文 | 压缩通知内容需完整 |
| 其他 | false | 原文全文 | Fallback |

**核心原则**：`thinking_plan` 和 `external_input`/`agent_output` 一样使用原文全文摘要策略，`action_command` 使用工具调用摘要。

<a id="summary-naming"></a>
## 三、摘要视图的生成机制

`GenerateEventSummary(msg, eventType, opts)` 委托注册表 spec，按以下顺序判定：

1. 纯工具调用的 `thinking_plan`（无正文、有调用）其视图取"调用 <工具名>"，使老化渲染仍带工具身份而非空占位，也让工具链折叠能直接从摘要取到名字，无需回读全文。
2. `spec.Special` 为真：返回原文全文（无截断）。
3. `action_command`：一行机械工具调用摘要（`调用工具: name(args)`；`StructuredFormat=true` 时多行展开）。
4. 其余：`spec.ToolLineSummary(msg)`，未配置则原文。

工具结果若是 JSON，摘要提取 `status`/`session`/`count`/`message`/`error` 与前若干条目名，避免把大段 JSON 原样存进摘要——否则召回把摘要再序列化进响应会造成嵌套 JSON 转义。非 JSON 结果原样返回。

`EventSummaryOptions` 只有 `StructuredFormat` 一个开关：`DefaultOptionsForLLMContext()` 为 false（高频、省 token），`DefaultOptionsForCompression()` 为 true（低频、清晰）。`FormatEventDescription` 为压缩器生成结构化的事件描述（完整信息，不截断）；`EstimateTokens` 是启发式估算（约每 3 个字符 1 token），上下文管理器另用更保守的 `CharsPerToken = 2.0`。

<a id="registry-authority"></a>
## 四、注册表、投影与类型存在理由

<a id="type-rationale"></a>
### 4.1 每个内置类型的存在理由

| 类型 | 为什么单独存在 |
|---|---|
| `context_compress` | 滚动摘要的合成引用（负 key），是"当前综述"的持续视图 |
| `tool_chain` | 一组完整工具对折叠成一条合成引用（负 key）。与 `context_compress` 分开，是为了让"保留引用"扫描不把工具链折叠计入综述数量 |
| `settle_fold` | 连续多条结算通知折叠成一张票据卡片（合成负 key）：只影响投影视图，底层结算事件仍在事实链中，可按卡片行内 key 逐条召回 |
| `context_compress_summary` | 策展固化的段落摘要产物，属长期记忆：豁免 TTL 与驱逐 |
| `task_spawned` | 任务派生的事实链记录，是跨重启重建活动任务集的**唯一**数据源；永不进投影（任务板按内存注册表现算）|
| `resident_session` | 常驻会话生命周期（全参派生／终态结局）的注册与审计记录；不进投影 |
| `consolidation` | 证据门控的巩固产物：携带源事件 key 收据列表与服务端计算的指纹，可回放验证并**防止模型自造记忆**；正 key、豁免 TTL |
| `governance` | 治理记录采用**单类型＋`subtype`** 而非五个类型：注册有成本，而审计查询天然按单类型过滤 |
| `feedback` | 反馈/评分/任务成败经因果边绑定到具体产出事件，**零新索引**；`subtype` 区分来源 |
| `inbox_receipt`、`wf.*` | 见 1.6：为被动排除与记账而注册 |
| `cognitive_asset_changed` | 认知资产漂移的**批次证据记录**：一次扫描把若干文件变更压成一条（摘要给数量与文件列表，正文逐行给旧新指纹/size/mtime），因此不能借用任何单事件类型——它的粒度是"一批"。注册为 skeleton＋可嵌入＋可召回、TTL 30 天：漂移必须**能被事后问出来**；写侧经任务记录沉入事实链而不进投影 |

### 4.2 三层数据表示与投影约束

事件流经三层：EventBus `AgentEvent`（临时）→ `SessionProjection`（有界引用）→ `MemoryStore`（不可变完整事实）。`runEventLoop` 是 EventBus 的**唯一消费者**；它批量拉取、`BuildInvocation` 合并、`RunFlow` 执行。框架 Runner 在 `RunFlow` 内产生事件流，`onEvent` 只做投递侧回调（meta_* 透传 + meditation ★ 标记），投影写入统一到插件管线（`StoreEvent` 成功的同一同步点经 ProjectionSink 追加 `EventReference`）。

关键约束：`SessionProjection` 只保存轻量 `EventReference`；完整事件由 `MemoryPlugin.OnEvent` 持久化；压缩器只修改发往 LLM 的消息列表，不修改投影；压实只清理投影旧引用，不删 MemoryStore；MemoryStore 是唯一完整事件链。

## 五、元数据契约

<a id="metadata-keys"></a>
### 5.1 键的归属与注入点

事件元数据是**框架职责**：每个键在 `event` 包定义一次，注入点引用常量，消费方经 `ParseEventMeta` 解析而不直接读 `StateDelta` 原始串。键按来源分四类：

| 类别 | 键 | 写入方 |
|---|---|---|
| 存储标识 | `event_key`（`MetaKeyEventKey`）、`partition_id`（`MetaKeyPartitionID`）、`event_type`（`MetaKeyEventType`）、`event_summary`（`MetaKeyEventSummary`） | 事件持久化插件在落库时写 |
| 分发锚点 | `trigger_source` | 每回合由运行流程设在所有转发事件上，供消费方确定性分派；入库时另抄进 `FullEvent.Metadata`（`agent/context_manager.go`），于是它成为**跨重启、跨分区仍可读**的持久归因——宿主投递门与新鲜度判据都经 `event` 包的谓词从它派生，不自建清单（判据之家见 [agent 引擎篇 §2.14](../agent/agent-architecture.md#meditation-curator)、白名单见[谱系可见性](../reliability/durable-delivery.md#lineage-visibility)） |
| 透传业务 | `meta_` 前缀（如 `meta_chat_id`） | 从 invocation 根元数据传播到投递出的事件 |
| 调用关联 | `call_id`（`MetaKeyCallID`） | **写 = `MemoryPlugin`**（经组合根注入的 `CallIDResolver`，按事件自带的 SDK 响应 ID 精确命中才盖；未命中／空串／未装配一律不写，绝不取"最近一次调用"）；**读 = 离线训练导出**（从 `FullEvent.Metadata` 原样取用）。只进 `Metadata`，不进 `StateDelta`、不带 `meta_` 前缀，因此事件解析面不消费它 |

归因与可观测键（`agent_name`、`bundle_id`、`rollout_id`、`trace_id`、`span_id`）写在 `FullEvent.Metadata` 上，使产出事件可回溯到生效版本。`trace_id`/`span_id` 与 turn span 同源，**但"三个投影共用一个锚点"只在配了 OTLP 端点时成立**：默认 noop provider 下这两个字段为空串，事件存储也没有按 metadata 过滤的查询面——跨投影的关联主路是 `parent_key`/`call_id` 的离线 join，不是 trace。

治理子类型键 `subtype`（`denial`/`goal`/`approval`/`degraded`/`audit`）的权威定义也在本包：治理账本写、进化取证读同一常量。**跨包复制字面量会静默漂移**，一旦漂移取证侧的拒绝计数归零，快道回滚防线随之失效——这类漂移没有编译期信号，只能靠"单一声明处"避免。

### 5.2 溯源快照与保留

`source_snapshot` 用一条 JSON 承载 durable 输入的原始 `source` 与**完整业务 Metadata**。这样做的原因是：业务键不得进入受控控制命名空间（否则运行时声明状态会泄漏进模型上下文与宿主投递字段），而跨重启对账又必须能还原到宿主身份（`chat_id`、任务世系等）。运行时 claim 状态改由类型化非 JSON 字段承载，只有这些**身份键**跨入持久化事实。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|------|-----------|---------|
| **事件类型无版本化** | 类型语义演进（如 summary 语义从"总结"变"原文视图"）无 schema 版本标记，历史事件按当前语义解读 | 事件 Metadata 加 schema_version；读取侧按版本适配 |
| **summary 双计算** | MemoryPlugin 与 SummaryPlugin 对同一事件各调一次 `GenerateEventSummary`（结果一致，纯函数），重复计算是小额性能债 | StateDelta 传递复用一次计算 |
