## MODIFIED Requirements

### Requirement: 批量 drain mailbox

Loop SHALL 阻塞等待第一个有效事件，在一次有限 Pull 中按接收序收集可用输入并立即冻结本次批次；durable 路径保持每次最多 32 个 envelope，不拆分单个 envelope 的消息顺序。有效批次 SHALL 至少包含一条事件。多个消息合并为一次输入并计作一个 turn，现有重试不产生新的业务 turn。Loop SHALL 是常驻框架唯一的 bus 消费者；BeforeModel SHALL NOT 认领或追加执行中新到的 bus 事件，新事件 MUST 留到下一批。同步工具返回仍由框架参与当前 ReAct。

#### Scenario: 单事件 drain
- **WHEN** mailbox 只有一条有效消息
- **THEN** 当前批次只包含该消息，并触发一个 turn

#### Scenario: 批量 drain 多事件
- **WHEN** mailbox 中 msg1、msg2、msg3 已进入本次 Pull 的批次
- **THEN** 三条消息按序合并为一次输入，计作一个 turn，不拆成三个 turn

#### Scenario: 等待第一个事件
- **WHEN** mailbox 为空
- **THEN** Pull 阻塞直到有效事件到达或 context 取消

#### Scenario: 执行中新输入属于下一批
- **WHEN** A+B 已冻结并开始执行，C 在当前 turn 的两次模型调用之间到达
- **THEN** 当前 turn 的实际模型请求不包含 C，C 不被中途 claim，下一次 Pull 才消费 C
- **AND** durable 与 volatile 模式采用相同批次边界

#### Scenario: 重试不扩充批次
- **WHEN** 当前 A+B 批次遇传输或退化重试，期间 C 到达
- **THEN** 重试仍只处理 A+B，C 留待下一 turn，重试不增加业务 turn 计数

### Requirement: 可靠输入全序持久化

显式可靠模式 SHALL 将所有输入先写入有界 inbox-v2，Envelope 携带 version=2，文件和目录屏障完成后才返回 durable。发布序列 SHALL 由单一串行化点分配，消费者按该序处理，channel 只作唤醒。默认未确认上限 2560，满额/不可写/初始化失败 SHALL 拒绝，不回退 volatile。一次批量提交 SHALL 以单 envelope 接收，保持各消息固定槽位；每个槽位 MUST 无损保存原始 ID、Type、Source、Timestamp、完整 Message 与可 JSON 表示的业务 Metadata。inbox 路径、槽位与确认控制信息 SHALL 与业务 Metadata 分离，不进入 Origin、模型或投递字段。

#### Scenario: 低负载可靠输入仍可恢复
- **WHEN** 只有一个输入收到 durable receipt 后进程终止
- **THEN** 新进程恢复同一原始消息、来源、时间和 Metadata，不依赖填满 channel

#### Scenario: 并发与背压不超车
- **WHEN** 多生产者在积压临界点并发提交
- **THEN** 所有成功 receipt 有全序；超额明确拒绝，新事件不绕过旧持久项

#### Scenario: 路由及世系保真
- **WHEN** 输入带 chat_id、trigger_source、任务 Origin 和控制之外的业务 Metadata 经 durable 发布、claim 及重启
- **THEN** 下游路由与内部任务扣留决定和原始输入一致，不依赖最近活跃 chat 补偿丢失字段
- **AND** inbox 内部路径与控制键不出现在派生任务 Origin 中

#### Scenario: 输入不可序列化
- **WHEN** 发布的业务载荷无法按协议 JSON 编码
- **THEN** 返回明确接收错误，不删除字段后报告 accepted，也不回退 volatile

### Requirement: 输入处理确认与幂等

claim SHALL 不删除原件。系统 MUST 在任何输入事实写入前，为整个 envelope 的固定消息槽位耐久保存 EventKey 与规范化 FullEvent 载荷，并预留固定处理 receipt 身份；重放使用相同 key、来源和载荷，不重算时间或归因，不按成功项压紧槽位。公共重复写入与内部核对重放 SHALL 分离；同 key 异内容、错分区、读取 I/O 或无法完成屏障 SHALL 返回错误，不作为已提交。投影 SHALL 只引用成功提交的 canonical facts，按 key 幂等。

冻结批次所有 selected 输入成功提交后 SHALL 才执行合并 turn；部分失败 MUST 保留同批输入可取消地退避重试，不调用缺失输入的模型请求，不设置未经验证的已提交回显标志。插件跳过范围 SHALL 仅为该 invocation 对应的已提交合并输入，不跳过其他输入或输出事件。

每个原始 claim SHALL 获得逐槽处置：正常选中项具有输入事实及处理结果，被过滤 meditation/有效空输入具有 skipped 原因且不进入模型投影。turn 完成后 SHALL 冻结 completion，先耐久保存于 envelope，再提交不可投影的 inbox_receipt 事实，最后 RecordReceipt 与 Ack；failed MUST 与 completed 区分，取消且未形成终态不得伪造完成。completion 已耐久而事实 receipt 未提交时 SHALL 只补交同一 completion；已有匹配事实 receipt 时 SHALL 只补确认，不重复执行。

对应 inbox 尚未耐久确认清理时，处理 receipt SHALL 免于 TTL/容量淘汰；Ack 删除及目录同步成功后按 30 天窗口处理。删除或目录同步失败 MUST 保留可重试确认状态。去重索引 SHALL 只覆盖 outstanding IDs，并从各 envelope 的固定 receipt key 对事实直接核验，不依赖 projection 的 compaction/tail 扫描；超过 30 天的客户端重提交不承诺幂等。工具副作用和渠道发送 SHALL 明确为非 exactly-once 边界；202 仍只证明接收。

#### Scenario: 取出后入库前崩溃
- **WHEN** claim 完成但事实提交未完成即终止
- **THEN** 原输入仍存在，恢复后使用已有 prepared 身份，或先耐久准备后重试，不丢失消息

#### Scenario: 入库后执行前崩溃
- **WHEN** 原始事件已提交但尚无完成证据
- **THEN** 恢复后继续或重试处理，原始事件与投影不重复追加，不产生新 key

#### Scenario: receipt 后 ack 失败
- **WHEN** 处理完成 receipt 已耐久但删除 inbox 或目录同步失败
- **THEN** 当前进程及重启后均只重试确认清理，不再次执行已确认输入

#### Scenario: 旧格式或损坏项
- **WHEN** 存在未排空旧 spill/v1 或不可读取的新 inbox 项
- **THEN** 旧格式拒绝启动并提示排空迁移；损坏项保留隔离与告警，不静默删除或报告完成

#### Scenario: 写前准备失败
- **WHEN** prepared facts 的文件或目录屏障失败
- **THEN** 不开始任何输入事实写入，不执行模型，不确认 envelope

#### Scenario: 多消息部分失败不串槽
- **WHEN** envelope 含 A、B，其中一个输入事实提交失败、另一个已提交
- **THEN** A、B 的固定 key、源 ID 和内容配对不变，重试仅补齐缺失提交；全部成功前不启动合并 turn

#### Scenario: 输入失败但 receipt 存储可用
- **WHEN** 输入 StoreEvent/ReplayEvent 失败，而单独的 receipt 写入本可成功
- **THEN** 不生成成功 completion、不 Ack、不删除原件，模型不在缺输入状态下执行

#### Scenario: 同键异内容
- **WHEN** 重放 key 已对应不同内容或其他来源的事实
- **THEN** 明确报告身份冲突并保留待处置输入，不引用该错误原文形成投影

#### Scenario: 过滤输入有明确终态
- **WHEN** 混合批次中的 meditation 被跳过，或有效空输入无需执行模型
- **THEN** 原始 claim 仍进入完成对账，receipt 保存 skipped 理由；不反复僵尸化，不将该消息加入 projection

#### Scenario: completion 后事实 receipt 前崩溃
- **WHEN** completion 已耐久，但 inbox_receipt 尚未提交
- **THEN** 重启只补交相同 receipt 载荷再确认，不能把 prepared 输入身份误当作已有 completion

#### Scenario: 模型失败与取消
- **WHEN** 模型明确失败或传输重试耗尽
- **THEN** 完成记录标为 failed 而非 completed，并保存错误摘要
- **AND** 若只是关闭取消且尚无 completion，则保留未完成 claim，不假称已处理

## ADDED Requirements

### Requirement: 可靠模式格式与重放能力准入

可靠模式 SHALL 在启动接收前核验 store 的显式重放能力；缺少能力时 MUST 返回配置错误，不用 GetEvent 成功替代提交核验。升级 SHALL 不自动猜测 v1 丢失的 Metadata 或错位 EventKeys；未确认旧格式及未处置隔离项 MUST 阻止启用 v2。回滚指引 SHALL 明确旧二进制不理解 v2，未完成项不得被忽略。

#### Scenario: 自定义后端无重放能力
- **WHEN** 自定义 store 未实现内部重放能力但启用可靠 inbox
- **THEN** 构造明确失败；未启用可靠模式时不强迫该后端实现新主接口

#### Scenario: 未排空升级或回滚
- **WHEN** v1 有未完成项准备升级，或 v2 有未完成项准备降级
- **THEN** 升级检查/回滚演练阻断操作，要求排空或继续由支持该格式的版本消费，保留所有原件
