# 常驻输入、提交与生命周期设计（已废弃）

> 状态：SUPERSEDED。仅保留历史设计与判断过程，不是实施依据。使用 [新设计](../complete-resident-reliability-protocol/design.md)；本文件中“已完成”“待确认”等历史表述不再控制当前工作，不得据此恢复旧实现或迁移约束。

## Context

基线为 `dev@a16fdce90fec4395131d5f1410ff3d23f0d912cb`，工作树在提案开始时干净。审查覆盖 `aeb273d..a16fdce`；本变更覆盖全部十项发现。审查结论来自调用链与源码，已有聚焦测试通过不代表以下故障窗口已复现；实施先补 fail-before 回归。

约束来自 `README.md`、`docs/wiki/agent/agent-architecture.md` 及主规格 `persistent-event-loop`、`event-segment-store`、`event-sourced-projection`、`runtime-resource-ownership`。历史设计可解释动机，不代替当前主规格或代码。

用户已确认：多个已拉取事件合并成一次输入，算一个 turn；A+B 开始执行后到达的 C 属于下一批。该决定适用于 durable 和 volatile 常驻循环，不把每个 envelope 拆成独立 turn。

| 审查问题 | 当前边界缺陷 | 设计归属 | 验证终点 |
|---|---|---|---|
| F1 | inbox 丢 Metadata/时间/身份 | D2 | 宿主目标 chat 与内部任务扣留决定 |
| F2 | 输入写失败仍设置已提交标志并确认 | D3 | 输入事实、实际模型请求、receipt 与未确认文件对账 |
| F3 | idx 存在但缺 evt，修复返回成功却未补写 | D4 | 绕过缓存查询和独立进程 reopen |
| F4 | EventKey 事后写回且压紧成功槽位 | D2/D4 | 每个消息序号与原文身份一一对应 |
| F5 | TryPull/过滤路径的 claim 未进入完成集合 | D1/D3 | 下一批正确消费，被跳过项有终态证据 |
| F6 | release 重复执行影响其他共享者 | D5 | 存活共享者可读写，新代不受旧 Close 影响 |
| F7 | lease Close 绕过 engine | D5 | worker 停止，新 engine 只绑定新 backend |
| F8 | 重复提交增加 live-count | D4 | 计数、墓碑与容量淘汰结果一致 |
| F9 | recovery notice 丢失或进入历史 | D6 | 捕获实际请求，事实及 projection 不含提示 |
| F10 | countingKV 隐藏 Sync | D7 | 包装前后屏障和失败相同，基准重新生成 |

## Goals / Non-Goals

**Goals**
- 历史仍只有 FullEvent 一个真源；inbox 只承载尚未确认的输入和处理准备材料。
- 区分接收、事实提交、模型处理、处理结果提交和外部送达，不以一个成功标志代替全部阶段。
- 单消费者、固定批次、显式重放、同代 owner，减少跨层隐式状态。
- 修复正常、部分失败、并发、取消、崩溃和重启路径，验证到真实消费者边界。

**Non-Goals**
- 不改写上游 ReAct、工具调度或压缩算法，不新增队列服务、数据库、全局事务、运行时 tokenizer。
- 不承诺外部副作用或渠道发送 exactly-once；202 仍只表示接收。
- 不默认开启可靠模式、embedding 或其他可选子系统，不放宽默认 TTL、命名空间或权限。
- 不新增多会话调度器；同一冻结批次的来源优先级和 Metadata 合并规则保留，测试明确覆盖。不同 chat 的公平调度另案处理。
- 不执行实际升级、部署、付费调用、Git 提交/推送/发布或自动归档。

## Decisions

### D1 固定批次是唯一 turn 输入边界

- `runEventLoop` 是常驻路径唯一的 bus 消费者；一次 `Pull` 返回的有限事件集合立即冻结。保留现有 claim 上限 32 个 envelope，不将排空定义为无限等待所有生产者静默。
- `BuildInvocation` 按原序合并冻结批次中的有效消息，仅调用一次 turn 入口；现有传输重试/退化重试沿用同一批次，不生成额外业务 turn。
- 删除 `assembleRequest` 中的 `TryPull` 消费。`BeforeModel` 只读取已经提交的 projection，再组装临时运行态材料。执行期间到达的用户输入、task settle、monitor、meditation 保持 pending，下一次 Pull 才认领。
- 保留公开 `TryPull` 工具方法的兼容性，但常驻框架不再调用它中途消费；独立调用方自行承担所取数据的生命周期。
- 批次保存原始 claims 以及 `selected`/`skipped` 处置，不用过滤后的 slice 反推原始完成集合。混合批次 meditation 仍让路，不执行、不重新发布，完成证据见 D3。
- 原始消息不因 RoleSystem→RoleUser 转换被修改；仅事实/请求归一化副本使用 user role，任务消息不能提升为系统指令。

**理由与替代方案**：动态追加要求把输入来源、claim、重试和 Ack 状态贯穿每次 BeforeModel，且与已确认的下一批语义冲突。冻结批次消除这条额外写入路径；代价是新输入等待当前 turn 结束。同步工具结果仍由框架正常参与当前 ReAct，不受 bus 边界调整影响。

### D2 无损 inbox 与写前准备

新目录为 `inbox-v2/`，Envelope 显式 `version=2`，沿用单文件原子重写、文件和目录屏障、接收全序及未确认容量上限。版本不识别或必要字段缺失不得作为有效输入继续消费。

| 持久字段 | 内容与所有者 |
|---|---|
| request_id / seq / state / attempts | 接收单元与顺序；由 inbox 管理 |
| messages[i].source_event | 原始 AgentEvent 的 ID、Type、Source、Timestamp、完整 Message 和业务 Metadata 的 JSON 快照；序号不可压紧 |
| messages[i].prepared_fact | 首次处理前冻结的规范化 FullEvent JSON，包括 EventKey、原始语义时间、摘要、来源与归因；不存在表示尚未准备 |
| receipt_key | 首次准备时分配的处理 receipt EventKey；仅代表预留身份，不代表完成 |
| completion | turn 结束后冻结的规范化处理 receipt 载荷及逐槽处置结果；不存在表示尚未形成终态 |

- reliability 叶包持有 `json.RawMessage`，不 import agent 或 memory；类型校验、规范化和 FullEvent 构造由 agent 层承担。JSON 不可表示的输入在 accepted 前明确拒绝，不静默删字段。协议按 JSON 值语义保真，不承诺任意 Go 动态类型身份。
- inbox 业务载荷与运行时 claim 分离。agent 内使用不参与 JSON 的 typed claim 引用（路径、request ID、槽位），不再将 `inbox_path` 等控制键混进 Metadata、Origin、模型上下文或宿主投递字段。
- 先深拷贝源事件并接收；首次 claim 后为整个 envelope 构造固定槽位的 prepared facts 与 receipt_key，完成一次耐久重写，才允许写入第一条事实。准备失败不调用 StoreEvent。
- prepared facts 冻结首次处理的归因与规范化结果；重放不得使用新时间、新 rollout、新 bundle 或重新生成的摘要覆盖既定输入事实。原始 source_event 保留原 role，规范化副本按既有外部输入规则转 user。规范化消息使用现有 FullEvent.Response 保存完整 Message；输入身份（request ID、消息序号、源事件 ID）及来源快照写入 FullEvent.Metadata，新增键在 event 包集中定义，不依赖 inbox 文件永久保留来源。
- 一个槽位失败不改变其他槽位序号。重试复用相同 key 和载荷；读到同 key 异内容、错分区或错 source identity 时显式冲突，保留原件并停止该 envelope 自动处理。
- 宿主沿既有 `extractTriggerSource`、`extractRootMetadata`、`meta_*` 和投递策略消费恢复后的业务来源。不得以 inbox 丢字段为由依赖最近活跃 chat 补偿；内部任务与缺失世系仍按既有策略扣留。

**理由与替代方案**：只保存固定 key 仍会因重启时间/归因变化而无法进行同内容核对；保存 prepared fact 将重放输入固定下来，且只在 outstanding 期间重复保存。v1 已可能丢失 Metadata、错位 keys，自动迁移无法可靠还原；不采用猜测补齐，也不引入永久第二份历史库。

### D3 提交结果驱动完成确认

`persistBusEvent` 拆出无副作用的事实准备与显式提交结果；可靠输入提交走 D4 的内部重放接口。结果包含 canonical fact 和是否可投影，错误必须返回到事件循环，不能只写日志。

1. 冻结原始批次并计算处置；原始 claims 始终保留。
2. 完成 D2 准备后按序提交 selected 输入。全部 selected 输入提交成功才启动合并 turn；部分提交成功时保留原批次重试，不运行缺输入的模型请求，也不让新输入越过它。
3. 同点投影只对已提交 canonical facts 执行，按 EventKey 幂等。`MemoryPlugin` 仅跳过当前 invocation 对应的已提交合并输入回显；替换以首事件 Path/RequestID 及整个 turn user-role 推断的宽泛标志。assistant、tool 和其他 invocation 的事件不受影响。
4. 取消/关闭时无终态的 batch 保留 claim，冷启动回 pending。瞬时写入失败保留有界批次，沿现有 100/200/400ms 退避并以 400ms 封顶等待恢复，等待可取消；重复失败限频记录并暴露诊断，不忙循环、不扩张内存。确定的身份/格式冲突隔离保留并报告，不能伪装成功。
5. turn 正常结束记 `completed`；现有重试耗尽或框架返回确定失败记 `failed`，保留错误摘要，不标 completed。不能仅以 RunFlow 返回 nil 判断模型成功，需从实际响应错误记录终态。显式失败是已处理结果，不等于送达成功；进程取消不编造失败终态。
6. skipped meditation 或有效空输入不执行模型、不进入 projection，其原始消息身份与原因进入该 envelope 的 completion receipt。mixed envelope 必须为每个槽位提供 `processed` 或 `skipped` 结果，不能只确认被选中的部分；nil/不合法 payload 在接收阶段拒绝。
7. 每个 envelope 的 completion 先写入原文件并耐久，再经固定 receipt_key 提交不可投影的 FullEvent `inbox_receipt`；完成后 RecordReceipt→Ack。所有 selected 输入必须已有 canonical fact，skipped 必须有理由。不能用 receipt 写成功掩盖输入写失败。
8. 重启时：无 completion 的 claim 继续/重试输入处理；completion 已耐久但事实 receipt 尚未落库，只补交相同 completion；事实 receipt 已存在则核对 outstanding 身份和结果后只补确认，不再执行模型。外部工具在 completion 耐久前的副作用仍可能重复。
9. Ack 删除与目录同步错误均返回；删除后同步失败保留内存未确认状态并允许重复确认，同步成功后才释放 receipt 保留约束。冷启动面对文件存在/不存在两种状态均可收敛。

receipt 查询索引只覆盖 outstanding IDs。启动从 outstanding envelope 的固定 receipt_key 直接核对事实，不能依赖 projection 的 snapshot/tail 扫描顺带发现 receipt：预分配 key 可能早于最新 compaction 边界。receipt 在对应 inbox 未耐久清理前免于 TTL/容量淘汰，清理后沿既有 30 天窗口；不另造常驻历史去重表。mem_spill 不是可靠接收的成功凭据：D4 内部重放错误由 inbox 保留原件，不再额外 spill 该输入形成第二重放 owner；其他写入的既有 mem_spill 行为保留。

批次冻结是单次运行的调度边界，不新增跨进程 turn checkpoint。崩溃恢复可以按接收序重新合并尚无 completion 的 envelope；固定的是逐消息身份与已完成证据，不承诺重现原批组合或模型调用 exactly-once。

**理由与替代方案**：不允许“先 Ack 再处理”、依赖日志的成功或全局 FactsPrePersisted 开关。内存 batch 只是本次执行记录，重启证据仍来自 inbox 和唯一事实链，不持久化独立 turn checkpoint。

#### D3 实施细化（Section 4 落地机制，**待审阅确认，尚未实现**）

Section 3 收官后对 4.3–4.8 做端到端走查，发现现码与设计有一处地基偏差，且多个机制需在实现前定死，故先记录裁决、不改代码。

1. **事实链身份载体（4.3 前置，实为 4.6 依赖）**：`buildBusEvent`/`buildBusFact` 处理带 claim 的 durable 输入时，MUST 把集中身份键写入 `FullEvent.Metadata`——`MetaKeyInboxRequestID=claim.RequestID`、`MetaKeyInboxSlot=itoa(claim.Slot)`、`MetaKeySourceEventID=evt.ID`（D2 L67）。现码只盖 agent_name/trigger_source/rollout_id/bundle_id，未盖这三键（3.3 仅定义常量）。这是 4.6「按 receipt_key 直接核对事实」能在事实链自证的先决条件。
2. **提交路径改走 ReplayEvent（对齐 D3 step1 / D4 L96-99）**：`persistBusEvent` 的 claim 分支以 `memStore.(EventReplayer).ReplayEvent(key, canonicalFact)` 替换现 3.4 的 `StoreEvent`+`GetEvent` 近似。结果映射：`ReplayNew`/`ReplayRepaired`→同点投影+（task 源）feedback；`ReplayAlreadyCommitted`→跳过投影与 feedback、返 true（幂等）；typed **conflict**→`log.Errorf`+返 false（claim 保留、不 Ack），绝不把同键异内容当重放吞掉。`replayed` 由 `ReplayResult` 分类得出，删除 `GetEvent` 守卫。无 claim 的 volatile 分支仍走 `StoreEvent`，行为不变。（3.5 已在构造期 gate `EventReplayer` 能力，此改动顺带让该 gate 名实相符。）
3. **invocation 级回显标识（4.3）**：`DurableInbound` 的 `FactsPrePersisted`+首事件 `Path`/`RequestID`+「整 turn role==user」宽泛标志替换为显式 **EchoIdentity**：事件循环在本 turn 全部 selected 事实提交成功后，由「已提交批次的 (request_id,slot) 集合」派生一个 per-turn token，经 `AppendEventHook`/StateDelta 盖到那条**合并输入 user 消息**上；`MemoryPlugin` 仅当 user 事件的 StateDelta EchoIdentity 与 ctx 内 `DurableInbound.EchoIdentity` 相等才跳过。效果：assistant、tool、其它 user 输入、以及非本 invocation 管线注入（context-guard、recovery notice 请求尾消息）一律不受影响；「其它 invocation」因 ctx per-invocation 注入 + turn 末 clear 天然隔离。
4. **completion 记录结构（4.4）**：`Envelope.Completion` 定为规范化 JSON：`{batch_result: completed|failed, error_summary: string, slots: [{slot, disposition: processed|skipped, fact_keys:[hex…]|reason}]}`。selected 槽列其 canonical fact EventKeys；skipped（meditation 让路/有效空/过滤）列原因。经既有 `RecordCompletion` 落盘（同载荷幂等、异载荷 `ErrCompletionConflict`，3.1 已具备）。取消不写 completed、不伪造终态（D3 step5）。
5. **两阶段序（4.5，D3 step7）**：`finishDurableBatch` 收敛为——(a) 依 turn 结果组各 envelope completion；(b) `RecordCompletion(path)` 耐久；(c) 以固定 `receipt_key` 提交**不可投影** 的 `inbox_receipt` FullEvent（Metadata 带 `MetaKeyInboxRequestID`/`receipt_key`）；(d) `RecordReceipt`；(e) `Ack`。任一步失败保留 envelope、下次重启按 step8 续做；completion 或 receipt 写失败不得重跑已耐久 completion；未完成输入绝不删除。
6. **TTL/容量（4.7）**：outstanding receipt_key 在其 envelope 未耐久清理（Ack 未成功）前免于 TTL/压实淘汰，清理后回归既有 30 天窗口；不新建常驻去重表（D3 L87）。Ack 删除/目录同步失败在当前进程与重启均可重试，同步成功前不提前释放确认约束（D3 step9）。

> 待确认要点：(a) EchoIdentity 经 StateDelta 承载 vs 经 invocation ctx 承载；(b) `inbox_receipt` 事实「不可投影」如何在 ProjectionSink 侧显式拦停（EventType 白名单 or sink 侧判定）；(c) 提交路径切 ReplayEvent 后，volatile→durable 混合批的部分失败退避（D3 step4 的 100/200/400ms）是否复用现 loop。确认后方按 4.3→4.8 依序实现。

### D4 公共不可变写入与内部重放分开

- `MemoryStore` 主接口不变，公共 `StoreEvent` 对已存在身份返回 `ErrDuplicateEventKey`，同内容也不增加计数或旁路 hook。两个内置后端一致。
- 在 memory 定义窄能力 `ReplayEvent(key, canonicalFact) (ReplayResult, error)`，只供 inbox/mem_spill 等恢复调用。结果区分新提交、补齐、已存在，并返回实际 canonical fact；typed not-found、duplicate/conflict 与 I/O 不混淆。
- 内置文件/内存后端实现该能力，engineBridge、ErrorTrackingStore 显式透传。可靠模式对缺少该能力的自定义 store 在构造时明确拒绝，volatile 路径不变；不采用 `GetEvent` 成功即认定完成的弱降级。
- 文件后端在分区写入协调下执行身份核对、必要补写、屏障和发布。已有 idx 时读取其真实槽位；缺 evt 必须补写；evt 存在而 idx 缺失时先找回同 key 原槽位，不创建第二原文。恢复查询覆盖 key 的原时间窗及该分区已登记段；不同内容或身份冲突拒绝。必要 meta 无论 seq 是否 0 都核验，已有 layer/封口信息不能被无条件重置。
- 完整同内容重放仍通过后端屏障确认，返回已有事实；绝不把同 key 的任意内容拿来生成新摘要。墓碑代表合法遗忘，恢复不得无条件复活已删除历史，应返回明确缺失/冲突供上层保留待处理项。
- 不新增永久 commit marker 或全历史内存去重集合。live-count 由现有原文/索引/meta/墓碑导出：正常新提交增量 +1，公共重复及已确认重放 +0；部分写入失败使该分区计数进入 unknown，容量淘汰暂停。
- 修复路径或首次从失败恢复的成功屏障后，在分区写入协调下重算受影响分区的逻辑存活计数再恢复 known。扫描只在修复/不确定恢复时发生，不在每次正常事件或 BeforeModel 热路径发生。读失败继续 unknown，不用猜测的增量补偿。
- 启动计数重建采用同一逻辑：只计原文、索引和段发现关系一致、未墓碑的 canonical EventKey；重复压实槽位只计一次。半写入保留待重放，不计成已提交事实。
- 提交/修复/计数重建与同分区删除、墓碑及压实发布的锁序统一；不持全局 registry 锁做 I/O。普通查询性能不在本变更重构。
- engine 的索引确保按 key 幂等，capacity hook 不把已存在重放计成新增；ErrorTrackingStore 对存储错误照常上报，但 ReplayEvent 不将可靠输入二次写入 mem_spill。mem_spill 自身重放使用同一 canonical 核对路径；自定义不支持能力的后端保持明确错误，不悄悄丢项。

**理由与替代方案**：以 LRU cache 命中判断已提交会在淘汰/重启后失效；永久提交索引会引入迁移与新真源；对未知修复一律 +1 会驱动错误淘汰。选择失败冷路径重算，用可控恢复成本换确定性，不增加正常写入的全库扫描。

### D5 同代资源 owner 与一次性释放

- `resourceEntry` 拥有同代 backend、可选 engine、生命周期组件和 writer lock。取消 `namedEngines` 的独立全局生命周期；共享粒度仍是 canonical path、后端及现有配置指纹，不新增跨 agent 共享策略。
- 每次 acquire 返回捕获具体 entry 身份的一次性 release。旧 release 不能按路径查找并减少新代计数；重复及并发释放均只减一次。`TagentAgent.Close` 同步并缓存第一次关闭结果，避免重复执行其他 closers。
- 资源先持 writer lock、打开 backend、恢复 tombstone/计数、构造 engine/回调，再启动后台生产者和发布 entry。构建失败清理从 acquire 成功后立即登记，覆盖 engine wiring 失败，不能等后续阶段才 defer。
- 每个 agent 的 capacity hook、ErrorTrackingStore、read namespaces 仍为独立包装；借用桥不拥有 shared engine/backend 的 Close 权限。独享空 path 的 owner 复用同样关闭次序，但不进入路径 registry。
- 关闭先停接收/生产者并等待或取消 in-flight turn、runner，再停会调用 engine 的生命周期扫描/压实生产者，再停并等待 engine worker，随后 flush/close backend，最后释放 writer lock 和 entry。
- 同一路径 acquire 与最后 release 使用同一 per-key 协调，等待旧代关闭完成才打开新代；慢关闭不占 registry 全局锁，不阻塞其他路径。
- 关闭错误反馈给首次 Close 调用并保留诊断；不能仅吞掉错误后宣称资源安全关闭。无法确认 worker 已停止时保留该路径为不可重新打开状态及写锁，避免新旧 writer 并存；进程退出由 OS 释放锁。
- embedding 初始化失败仍按既有策略降级关键词检索并保留 capacity hook，不影响主事实提交；同代不反复创建悬挂 engine，reopen 才重新尝试构建。

**理由与替代方案**：每个 agent 关闭 engine 会伤及其他共享者；只给 release 加 once 不能修复旧 engine 重用。一个 owner 覆盖完整依赖图，仍保留既有装饰器功能与状态/执行分离，不全面重写 buildAgent。

### D6 恢复提示只进入实际请求尾部

- 删除事件循环对 invocation.Content 的 recovery notice 拼接。
- 在统一 BeforeModel 中，历史 projection 渲染、压缩以及 live task board 完成后，追加一次短运行态 recovery 消息；保持 system、历史和工具声明的既有字节，使用 user-role 临时消息，不提升指令层级。
- 仅在请求确实完成装配、即将提交模型时消费 notice；输入写入失败、空批跳过、装配前取消不消费。每个冷启动恢复结果只在首次提交的请求出现；provider 收到请求后的失败不要求重发 notice，也不宣称模型已理解。
- 不经 MemoryPlugin 写入输入历史、不 Append projection、不写独立 checkpoint。diagnostics 的 RecoveryResult 持续可读；full/正常空库不增加提示或 token。
- 覆盖 durable、volatile、one-shot、executor 热更和无有效历史的请求；运行态提示不能绕过输入提交 gate，也不能成为恢复成功的替代证据。

**理由与替代方案**：修改 runner 入参既可能被 projection 装配覆盖，也可能被事件插件存成事实。最终请求装配是已有临时看板的同一边界，不需要新事件或提示总线。

### D7 测量能力保真与证据重建

- countingKV 使用包含 `KVStore` 和 `Sync() error` 的明确接口，并显式转发 Sync 与错误；当前 LocalFileKV 还需保留实际使用的分区枚举能力。编译期断言与失败 spy 测试锁定装饰器能力，不能用无操作 Sync 冒充实现。
- 加一条包装后单事件写入、无 Close 子进程退出、独立进程读回测试，确认 benchmark 与生产一样经过事件屏障。fsync=false 仍执行 Flush 级 Sync，报告不混为掉电耐久。
- 重跑 1k/10k/100k × fsync on/off × 并发探测 1/10/100。保留现有 20k 采样上限，但报告用实际 written 标注，不称为 100k 外推；Get 与 Query 延迟分别统计；未采集 OS RSS 时字段只叫 Go HeapInuse/Sys。
- 报告记录源码 commit/dirty 状态、Go/OS/文件系统、配置、样本数、Sync 调用数及完成状态。修复前无事件屏障数据不能直接触发修复后的 20% 同语义回归阈值。
- 保留旧 JSON 原件，在 REPORT.md 标注旧 storage 结论失效原因并链接新实测。压缩与 tokenizer 数据单独保留其证据范围；不为了统一报告而虚构重测或线上收益。

## Risks / Trade-offs

| 风险/代价 | 缓解与边界 |
|---|---|
| 固定批次增加新输入等待 | 用户已确认；不阻断同步工具结果，取消仍走现有控制通道 |
| 可靠输入 I/O 增加 | prepared facts 按 envelope 一次写前准备；基准分模式测量，不降低屏障换吞吐 |
| repair 分区重算较慢 | 仅失败/重启修复冷路径执行，正常写入保持增量；unknown 时暂停容量淘汰 |
| v1 已丢失来源，无法安全自动升级 | 明确排空门；无法排空则停止升级，由维护者处理，不能猜测修复 |
| 自定义 store 未实现重放能力 | reliable 构造显式错误；MemoryStore 主接口与 volatile 行为不变，迁移说明列出能力要求 |
| 关闭/并发锁序回归 | per-key/per-partition 作用域、共享者/新代测试、定向 race，不跨层持全局锁做 I/O |
| 两阶段 completion 并非外部事务 | 完成前副作用至少一次；渠道发送状态不作为 Ack 的伪证明 |
| 历史规格存在过时条款 | 本次仅替换与消费边界直接冲突的 role 注入条款，不顺手重写其他历史能力 |

### 不变量与运行时五轴核验

| 维度 | 本设计约束 |
|---|---|
| 真源/执行权/默认态 | FullEvent 历史真源；inbox 仅 outstanding；外部操作仍由宿主/人授权；可选能力默认不变 |
| 通路到被看见 | 输入→prepared fact→提交→projection→实际请求→原有路由→宿主决定；receipt 只证明处理，不证明送达 |
| 不可见边界 | 空输入、混合 meditation、I/O、身份冲突、取消、无消费者都不能被成功标志吞掉 |
| 高频成本 | 无 BeforeModel inbox I/O；正常提交不全库扫描；计数/索引为派生状态 |
| 重启恢复 | 固定消息槽、prepared fact、completion 与事实 receipt 对账；恢复不依赖旧进程 cache |
| 构造次序 | writer lock→backend 恢复→engine/回调→worker；关闭逆依赖，不复用旧代 |

## Migration Plan

1. 先补十项 fail-before 测试和批次边界测试，再实施存储/资源两个独立工作包；两者与组合根交叉处顺序整合。
2. 不改变 FullEvent、EventKey 和现有 KV/WAL 键格式，不增加永久提交标记。可靠 inbox 升为 v2；启动先检查旧 `.spill` 和 v1 中未确认项，包括未处置隔离项，存在则拒绝，不移动/删除原件。
3. 升级操作由维护者在停止接收、排空、备份后执行。已有 v1 数据因缺来源无法安全处理时，保留数据并报告阻塞，禁止用最近活跃 chat 或数组位置猜测。
4. 回滚旧二进制前停止新接收，确认 v2 未完成及隔离项都已处置并备份；否则由支持 v2 的版本继续消费或停止回滚。旧二进制不会自动理解 v2，操作文档必须明确该限制。
5. 更新随载宿主接收/投递测试、recovery 请求捕获、资源测试与升级演练；同步 benchmark 解释和迁移文档，不改写历史归档报告的完成记录。
6. 提案只写 delta；实现和验证后另行调用正常 OpenSpec archive 同步主规格，不使用 skip-specs/no-validate。长跑、真实 provider、部署证据分开，未执行即待验。

## Test Plan

| 修改面 | 必须覆盖的现有测试与新增场景 |
|---|---|
| inbox / EventBus | `agent/reliability/inbox_test.go`、`agent/inbox_receipt_test.go` 及 bus 测试：Metadata/role/时间/消息体往返、A/B 部分失败、固定身份、格式拒绝、claim/prepare/commit/completion/receipt/Ack 窗口 |
| turn / 插件 / 请求 | `agent` 与 `plugin` 相关单元及真实 runner 集成：A+B 一个 turn、C 下一 turn、重试不重计、过滤终态、提交失败不调用模型、不误跳其他 user 事件、恢复提示仅请求可见 |
| segment / 计数 | `memory/storage_contract_test.go`、`segment_store_barrier_test.go`、`lifecycle_test.go`：公共重复拒绝、显式补槽、idx/meta 丢失、缓存淘汰、重启、同键并发、墓碑不复活、失败计数 unknown、恢复只计一次 |
| owner / engine | `resources_ownership_test.go`、`resources_lock_test.go`、engine 测试：A/B 共享重复 Close、旧 release 对新代、同路径并发 reopen、其他路径不阻塞、mock embedding worker 退出、构建失败与降级 |
| 宿主投递 | `examples/wechat-bot` 独立模块：durable chat_id 往返到发送目标；meditation 派生任务非空 final 不发送；不调用真实渠道 |
| 综合恢复 | `tests/resident_e2e_test.go`、`upgrade_rollback_drill_test.go`：实际启用 inbox+localfile，独立进程受控终止，accepted/fact/receipt/请求/投递决定全链对账 |
| benchmark | wrapper 能力/错误测试、独立进程读回、完整矩阵重新采集与报告语义检查 |

实施先按最终 diff 映射并运行全部受影响测试；根与 wechat-bot 分别 build/vet/short，修改到的并发包执行定向 race。独立进程终止不称掉电实证；72h 长跑、真实模型/渠道和部署沿既有准出规格单列待验，不作为本地修复已完成的伪证据。

## Open Questions

无阻塞本地实现的产品选择：范围为全部十项，批次已确定为 turn 开始时冻结。真实性能数值、72h 运行环境及生产升级窗口尚无实测/授权，实施仅记录真实结果与阻塞项，不据此扩展依赖或操作真实数据。
