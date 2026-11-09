# 平台子系统（组合根 · 配置面 · 治理闸 · 严格解码 · 投递账本 · 框架检查点）

`assembly` 是组合根：一份纯声明在这里变成活的运行时，进程内没有第二个装配入口。本篇的几个子系统不是并列的功能模块，而是**同一条装配链上的不同关卡**：配置面决定能声明什么，严格解码决定声明写错时怎么失败，治理闸决定一次调用放不放行，投递账本决定结果出去没有，观测/记忆/MCP 是链末的旁路与后端。共同纪律：能力缺席时按名拒绝或具名 gap，绝不给出"零读数"冒充空闲。

<a id="composition-root"></a>
## 一、组合根（`assembly/`）

模块根已经没有 Go 源：`golden/runtime/architecture.json` 的 `root_package` 是 `assembly`，23 个文件整体在 `assembly/` 下，**目录名即包名**；包边界与 `// 契约:` 索引都以它为准。

三个生命周期入口：`Boot(configPath, opts...)` 读一份宿主文档并带回 `*Host`；`Invoke(ctx, req)` 驱动一次回合；`Close(ctx)` 拆掉整个宿主。`Host` 拥有装配好的 run、结算驱动、反思调度与回环端点；出站消息面与审批面由调用方经 Option 注入，宿主不自己发明这些面——进程内的跨会话投递面则相反，它是宿主自己装配的一关（`host_tools.go`）。

host 簇按域分文件：

| 文件 | 域 |
|---|---|
| `host.go` | Boot / 驱动 / settle / 生命周期与关闭次序 |
| `host_document.go` | 方言解码与装配（文档 → 共享声明 → 运行部件） |
| `host_delivery.go` | 交付账本（义务登记、挂起、重发、销账） |
| `host_tools.go` | send 工具、跨会话投递面与投递账目、反思计划、注入端口 |
| `host_face.go` | HTTP face：健康探针、任务端点、`/reload` |
| `host_diagnostics.go` | 诊断面：统一运行读数 + 反思 / 训练 / 观测三面 + 注意力读面（`host_attention.go` 的 `hostReading`） |
| `host_model.go` | 采样参数（temperature / thinking / reasoning_effort）的请求包装 |
| `host_capture.go` | 轨迹采集：一次账户覆盖整次运行 |
| `host_reload.go` | 热更新三档判据（见[组织级热重载](./org-hot-reload.md)） |
| `checkpoint.go` · `checkpoint_inputs.go` · `task_checkpoint.go` · `host_delivery_checkpoint.go` | 框架 checkpoint 协调者与五个状态切片（见[第六节](#framework-checkpoint)） |

装配链本体在其余文件：`run.go`（唯一公开装配入口与发布）、`prepare.go`（候选部件构造与资源登记）、`request.go`（请求装配与回放窗）、`invoke.go`（回合与关闭）、`tools.go`（工具受理闸与裁决适配）、`facts.go` / `commit.go` / `read.go`（事实与投影）、`settle_turn.go`（事件驱动的结算回合）。

候选构造顺序固定：model → tools → prompt → plugins，外层再套工具受理 wrap。每个部件在**获取那一刻**把释放函数登记进候选账目，因此失败按获取逆序回收；共享面与在用资源不在任何账目里。

<a id="config-surface"></a>
## 二、配置面（`api/config` · `api/document`）

`Declaration` 是共享的声明词汇：`Entry` + `Agents []AgentSpec` + `Refs []ToolRef` + `Baseline` + `Servers map[string]MCPServerSpec`。`AgentSpec` 以 agent 为中心：自己声明的 `Model`、`Tools`、`Plugins`、`TaskPolicy`、`Files`、`Params` 等模型/预算字段，以及四个域段 `Memory` / `Meditation` / `File` / `Exec`。

工具私有参数走 `ToolRef{Name, Params map[string]string, Endpoint}`：共享结构不被工具私有字段污染，远程引用用 `Endpoint` 表达，本地可解析的引用留空。提示词与 skill 是 `FileRef{Path, Kind}`——**正文不在声明里**，只声明"哪个文件是提示词/skill"，正文在请求边界按文件读（所以改正文不需要重新发布，见[热更新三档](./org-hot-reload.md#restart-required-dimensions)）。

`api/document.Document` 是运行时视图那一层：`Entry` + `Agents` + `Servers`，`Order()` 给稳定顺序（entry 优先），`Declaration()` 派生共享声明。宿主方言（`assembly/host_document.go`）是它在宿主侧的读法：核心共享段逐字节按 `api/document` 读，`app` 段保持不透明、原样携带。

声明面在三处被读，**读的时刻决定热更档位**：Boot（构造消费点，改了须重启）、发布（换代重建绑定）、请求边界（提示词与 skill 正文、回放窗——下一回合就生效）。

<a id="governance-gate"></a>
## 三、治理闸（`features/evolution/governance`）

治理不是"包在工具外面的装饰器"，而是插件材料流 `call->ruling` 端口上的一个实例：`Policy` 报 `Port() = plugin.PortCallRuling`，实现 `Governor.Judge(pass)`。

一次调用过闸的完整链：

| 步 | 发生处 | 产物 |
|---|---|---|
| 变换 | `Pipeline.Before` 的 transform 在私有副本上改正 call | 最终 call |
| 打分 | 同一趟的 risk face 出 grade | 最强 grade（只读一次，不重算） |
| 传导 | 派发层把这一趟压进 context（`plugin.WithCallPass`） | 一次调用一次 pass |
| 裁决 | 受理 wrap 的适配读回 pass → `Governor.Judge` | `Decision` |
| 分类 | 适配器把裁决翻成任务受理结论 | `RulingDeny` / `RulingHold` / `RulingRun` |

三态语义：

- **拒**是终局：调用不执行，模型读到与调用配对的**工具结果**（带规则与理由），而不是 Go error——被拒的理由必须出现在模型读得到的地方，否则它既看不见为什么失败，也没有自纠入口。
- **挂起**（`hold`）是等待不是拒绝：调用交给审批面，宿主回 `HoldReceipt`，模型读到"parked awaiting approval"；批准或拒绝之后走同一条受理路径。
- **放行**（`allow` / `record`）：`record` 额外要求这次执行进审计台账。

两条 fail-closed：context 上没有 pass 时拒绝裁决（不猜），`strict` 级别且没有审批面时拒绝一切调用（不默认放行）。分级判据取**工具声明名**、不取注册别名：风险规则按工具声明名做最长前缀匹配，未命中落默认档——注册时的装配别名根本不进分级面。

<a id="strict-decode"></a>
## 四、严格解码（`api/config`）

一个策略、两个入口：`decodeDeclaration` 守护**声明词汇**自己；`DecodeStrict(source, data, v)` 把同一套拒绝词汇给**调用方自有 schema**（宿主文档这种带连接注册表、治理段、应用段的方言）用——它按 `v` 的 yaml 标签递归走完整个结构，宿主不再自己写第二套严格度。

| 输入 | 行为 | 为什么 |
|---|---|---|
| schema 外的键 | 失败，报**源 / 行 / 字段路径**（`ErrUnknownConfigKey`） | 拼错的键若被忽略，用户会以为改动了行为 |
| 同一个键写两次 | 失败 | 后写的赢等于静默丢掉前者 |
| YAML 首个文档之后的尾随文档 | 失败 | 静默忽略第二份文档会让用户以为其中配置已生效 |
| JSON 首个值之后的尾随内容 | 失败 | 拼接两份配置不可能是用户意图 |
| 完全空的文档 | 成功，解出零值 | "没写"与"写错"必须区分 |
| `.yaml` / `.yml` / `.json` 之外的扩展名 | 按名拒绝 | 猜格式等于让一份 YAML 被静默解成零值 |

同族的封闭性纪律：工具操作名是闭集（`tool/spec` 的 `Op`），集合外的操作在任何后端被触达之前按名拒绝——已退役的旧操作名重新出现时，宁可报错也不静默接管。

## 五、投递与交付账本

一次模型产出的正文，在离开宿主之前先成为**链上的交付义务**（`assembly/host_delivery.go`）：owed 声明随输出事实同笔落链（先登记后发送，崩溃窗口才不漏送），状态迁移以 `delivery.settlement` 事实按「身份＋状态」键追加——四结算态 `held`（reason ∈ approval/retrying/retry-exhausted/unknown，批准后只交一次）/`sent`/`refused`/`headless-retired`（非入口地址具名销账，不假装投递成功）＋迁移专用 `owed`；重启恢复经[框架 checkpoint](#framework-checkpoint) 的 delivery 切片，详见[持久投递篇](../reliability/durable-delivery.md#envelope-states)。

进程内通信走另一条缝，模型面唯一入口是 `send`（职责=跨 task、会话通信，正文永不出网）：`target` 写 `{"task": …}` 时重入本进程服务的 task；写 `{"agent": …, "session": …}` 时走宿主自己的投递面 `hostMeditationPort.DeliverToSession`（`assembly/host_tools.go`），先过四道门，每道都按名拒绝：

| 门 | 判据 | 不过门时 |
|---|---|---|
| 名单 | 宿主文档声明的 `deliver_to` 名单：名单缺席、发起方不在名单内、目标不在名单内，各自拒绝 | 具名说清缺的是哪一层，不默认放行 |
| 观察面 | 反思的观察面必须够得到目标，否则写过去也看不见 | 拒绝，不猜目标 |
| 绑定 | 目标会话必须是本 run 已经绑定的会话 | 拒绝，投递面不替任何人开新品 |
| 存活 | 宿主未停机，且存储能为该地址建立席位 | 拒绝，不另起一个席位 |

过门之后正文以**这次调用自己的身份**提交进目标链：事实身份就是投递身份，同一份正文在同一调用身份下重放不落第二条；随后按目标忙闲分流——空闲目标被驱动一轮，忙目标在自己的下一条决策边界上读这条事实。**发起方从不阻塞在目标的回合上**，所以忙目标不会把投递它的那次调用拖成死锁。

**两族投递，一本账**：agent 自己发的正文落成 `external_input`，框架的结算通知落成 `task_settled`——两族的事实类型分立，却盖同一套五键（`delivery.target` / `delivery.identity` / `delivery.receipt` / `delivery.source` / `delivery.denial`，见[事件篇 §二](../event/event-architecture.md#const-list)），因此"一条投递"不按来路分两种读法。`Run.DeliveryLedger()` 是这本账的读面：`Records` 按源事件、目标与身份稳定排序，`BySource` 读一个源事件到达过哪些目标，`ByTarget` 读一个地址收到过什么。一条投递身份只对应一条记录，查询不解析正文——账目是投影，事实才是账。

跨会话的**插件派发**走同一条规则，不另立一套：`SettleDriver.DeclareScope` 声明这份名册属于谁、点了哪些目标，`settleScopeOfDocument` 取的就是宿主文档声明的 agent 名册；驱动旁边没有声明、或声明为空名单时**一切派发被拒**（fail-closed），越界目标具名拒绝且目标零感知。

两类记录的分工是"谁把话送出去"与"这条话有没有被交出去"，术语口径见[存储概念图](../../demos/storage-concept-map.html)的交付账本与席位两卡。

<a id="framework-checkpoint"></a>
## 六、框架 checkpoint：恢复的唯一载体

重启恢复统一经框架级 **checkpoint 事实族**（Flink 式框架状态管理）：`event.TypeCheckpoint = "checkpoint"`（`api/event/checkpoint.go`），键 `checkpoint:<partitionSeq>` 按分区 Seq 单调，载荷 `CheckpointPayload` 携带一致性切面水位 `Offset`（各分区 `landedSeq`）与注册式命名切片，一身份一不可变事实、读回按载荷重算内容寻址 ID。消费者经状态接缝 `Stateful{StateName/SnapshotState/RestoreState}`（`runtime/state/checkpoint.go`）以 `Run.RegisterStateSlice` 注册，注册序即切片序、重名/无名具名拒；框架协调者 `Run.Checkpoint`（`assembly/checkpoint.go`）取切面走**安静点无锁**模型：先读水位（`landedSeq` 切面），再排水到安静点——`commitFence` 计的是「提交至其状态落定」的整段在飞工作，协调者等它归零（不是持锁关闭提交门）——然后在**不持任何 run 锁**的前提下逐个纯读各切片，聚合经 `commitFactNoCadence` 一次普通追加落链（仅 journal 追加的短暂分区序列锁），任一切片快照失败整条拒落。一致性来自「单写者按序＋安静点暂停」：事实的落链与其对持有者派生状态的应用同属一个在飞单元，安静点即无半应用；先于水位落定的必已应用（在快照内），晚于水位的由回放覆盖（按身份幂等，最多重复一次、绝不漏跳）。节奏由 `noteCheckpointCommit` 在提交时评估：提交只标记到期并唤醒独立 filer goroutine（`Run.fileCheckpoints`），切面由它排水到安静点取，全程不持跨持有者的锁——持锁提交的切片持有者（任务链 durable 提交、Flush 投递）不会被自己在临界区内问状态，提交栈里也没有任何一处取切面。接缝契约要求 `SnapshotState` **纯读**：只陈述已派生的内存态，禁 flush/commit、禁等待自身提交所需的锁；状态跟随自身已提交事实的持有者在该提交的在飞单元内应用，绝不拖到其外（`writeDeliverySettlement` 即按此把结算落链与账户状态记为一单元）。节奏由框架所有：每 `CheckpointCommitInterval = 64` 条提交或距上次 `CheckpointPeriod = 30s`（`noteCheckpointCommit`，两常量登记 `golden/runtime/window_bounds.json`，与回放窗口解耦）；`CheckpointAfterFold` 提供折叠产物落链后刷新载体的入口，折叠与 close 的触发接线仍在收口中（当前节奏只由提交计数与周期承担）。五个切片：`projection`（run：每会话投影幸存者，恢复以折叠锚为主路径、幸存者按身份从链水合，全局 Offset 不裁剪投影链）、`delivery`（Host：结算态＋开账清单含 body/目标/lineage/heldAt/重试 attempt，已结集合水位压缩 O(开账数)）、`mailbox`（`MemoryStore`：欠账通知按冻结路由逐条回邮箱）、`inputs`（run：未应答输入轮，覆盖区内的欠驱动不丢）、`tasks`（`MemoryRegistry`：任务账户＋链修订，boot 不再全史重读任务链）。

boot（`assembly/run.go`）：`newestCheckpointRef` 经 `Segments.NewestOfType` 单遍扫驻留索引取 type=checkpoint 的最新身份——不拷贝不排序、跨分区按窗口→序列位→身份定序，只答身份，body 由调用方按身份读一次——→ `RestoreState` 交还切片（切片先于持有者到达则暂存、挂载即恢复；装配收尾 `PendingRestore` 非空具名拒 boot）→ `eachFactPastCut` 只回放 `landingSeq > Offset` 的已提交事实经 `seedFact` 隔离重建（只写本 run 的内存账本与投影，不调 `commitFactChecked`、不跑 After pass，故恢复期不触发送达/settle/meditation 副作用；同身份已驻留即复用，不二次登记），不读覆盖区正文；任务链按 `CheckpointCoverage` 只折修订之后的链接，`restoreChains` 全史读降为无 checkpoint 年轻链兜底；未结判定（`Unsettled`/`Unanswered`）读恢复后内存态。恢复成本由 checkpoint 加偏移后增量决定，不随历史超线性增长；无 checkpoint 的年轻会话退化为本链全量回放（链小，有界）；**坏 checkpoint 具名拒 boot**（`ErrCheckpointUnreadable`），不退全量。一致性三条件由测试钉死：晚于 Offset 的结算靠回放覆盖、早于 Offset 的必在快照，不重不漏；折叠时刻未清义务（含 body）原样进 checkpoint；已结集合用分区 Seq 高水位＋开账清单表达。RecoveryTicket 独立票据快照路线已退役，送达账全史扫、邮箱全量重投（`RestoreOwed`）与逐站点 durable 扫均已删除。目标态（协调者下沉会话级、`Stateful` 收编 g(x) 读态、六端口收敛统一 `Material` 契约、派生态有界、持久原子只在 `AtomicStore.Commit` 接缝）见 `openspec/changes/session-checkpoint-material-unification/`。

## 七、观测、反思、记忆、MCP（旁路与后端）

- **观测**（`features/telemetry`）：commit observer / log observer / OTEL 三面，全部在事实提交之后读，是只读旁路。
- **反思与演进**（`features/evolution`）：激活判据、评估门（gate / judge / posterior）、冥想调度、治理策略面；只在开关打开时装配，默认关＝零行为变化。
- **记忆**（`memory`）：分区、账本、重放、查询与检索、嵌入器；查询面是只读投影，事实链是唯一来源（链管身份与因果、原文仓管字节、账本管责任）。
- **MCP**（`tool/mcp`）：`mcp_servers` 声明 → 每代发布重绑（停答即停、新增即答、没变保持连接）→ `mcp_call` 网关；声明变化进指纹，因此必换代、必重绑。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 治理只覆盖原生工具受理面 | 闸在受理 wrap 上，凡经该 wrap 的调用都过闸；绕过 wrap 直接调用的路径不受保护（当前没有这样的生产路径，但也没有结构约束阻止新增） | 把"注册即过闸"变成装配期不变量（骨架断言） |
| 交付账本与投递账目是两本账 | 交付账本管"结果的持久义务"（出站），投递账目管"正文落进了谁的链"（进程内），两侧共用一套投递身份写法，但读面尚未合并 | 诊断面把两侧按同一地址合读 |
| 诊断面的三面开关各自独立 | 关闭的面以理由作答而不是零读数；但没有"某面关闭导致另一面读数失真"的联动声明 | 在统一运行读数里带上各面开关状态 |
| 双严格入口靠纪律对齐 | `decodeDeclaration` 与 `DecodeStrict` 共享拒绝词汇（同包、同错误族），但两者的字段遍历是两段代码 | 由一份 schema 走查同时服务两入口 |

延伸：[存储概念图](../../demos/storage-concept-map.html)（12 个存储概念与 L1/L2/L3 受众分层）· [存储时光机](../../demos/storage-time-travel.html)
