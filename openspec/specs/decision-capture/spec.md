# decision-capture Specification

## Purpose
TBD - created by archiving change capture-decision-snapshots. Update Purpose after archive.
## Requirements

### Requirement: 可选SDK决策快照
启用trajectory_capture后 SHALL 记录v2 SDK请求、冻结工具声明、响应序列、终态及owner；旧trajectory_dump模式保持可用。MUST 标明capture_scope，不能冒称provider wire或隐藏提示完整。

#### Scenario: 请求对象随后被修改
- **WHEN** 调用后框架或另一路请求改变原messages/schema
- **THEN** 已采集快照不变化，模型调用仍用本次声明。

### Requirement: 精确可选归因
call_id SHALL 独立于trace生成并在调用scope内按invocation/response/tool-call精确关联；无证据必须unbound，冲突必须ambiguous。事实关联 MUST 发生在成功提交边界，不补写旧事实。

#### Scenario: 多agent与空响应身份
- **WHEN** 两个子调用并发且其中一个响应没有可用ID
- **THEN** 有证据者精确归属，无ID者unbound，不继承最近父调用。

### Requirement: 非阻塞有界录制
默认模型路径 SHALL 不等待训练磁盘；单条记录及全部采集在途副本/累积分片/队列/序列化缓冲总字节 SHALL 有上界，满队列、超限、关闭和写入错误分别计数。响应采集 MUST 不因累计无限分片突破上界；同时打开文件最多16个，单capture落盘上限默认512MiB，达限停止新采集并报告partial，不自动删除旧文件或轮转绕过限额。

#### Scenario: 慢磁盘与大响应
- **WHEN** writer停滞且模型持续输出
- **THEN** 数据可显式丢弃/截为不完整记录，内存有界、模型继续，统计可靠。

### Requirement: 封账完整性可证明
FlushAndWait SHALL 返回writer确认后的manifest；存在inflight/drop/error、未同步或无manifest MUST NOT 声称完整。统计读取不得依赖可能已满的数据队列。writer **未封账**（crash/未 flush）时 MUST 记 `partial` 并保留原因，MUST NOT 宣称完整；发送准入与关闭排空 MUST 走同一 protocol（采集与 model 共享终局）；封账统计 SHALL 支持按文件摘要复核。

#### Scenario: 关闭与flush失败
- **WHEN** capture结束或flush失败
- **THEN** 成功封账可按文件摘要复核，失败保留unknown/partial及原因，不能伪全量。

#### Scenario: 崩溃且未封账记 partial（失败）
- **WHEN** 进程在 writer 未封账时退出，重启后读取该 capture
- **THEN** 记录状态 MUST 为 `partial` 并附未封账原因，MUST NOT 被判为完整。

### Requirement: 采集不改变运行权责
采集 MUST NOT 强制OTel、改变TTL、自动训练、改变任务路由或记录凭据；关闭新模式不得创建归因scope/manifest。

#### Scenario: 关闭与隐私边界
- **WHEN** trajectory_capture关闭或仅运行合成验收
- **THEN** 原行为保持；启用输出按私有权限且无认证头/密钥。

### Requirement: 请求边界安全快照
采集 SHALL 在**请求开始前**固定本次实际模型、生成参数与 prompt/skill/tool 可见集合及其版本；MUST NOT 通过调用后重跑 provider options 反推或重建快照。`Headers` 与 URL 中的 secrets MUST NOT 落盘；原生日志与 `ExtraFields` SHALL 支持**可配置安全投影**，且投影 MUST NOT 改写业务正文——**保持状态原文传输全保真**。

#### Scenario: 请求对象随后被修改（正常）
- **WHEN** 调用后框架或另一路请求改变原 messages/schema
- **THEN** 已采集快照不变化，模型调用仍用本次声明。

#### Scenario: 重跑 provider options 被拒绝（失败）
- **WHEN** 尝试以调用后重跑 provider options 的方式构造快照
- **THEN** MUST 拒绝该路径并在验收中显式失败，快照 MUST 取请求前固定值。

#### Scenario: secrets 不落盘且投影保真（失败）
- **WHEN** 请求 Header 或 URL 携带 token/密钥
- **THEN** 采集产物 MUST NOT 含该 secret；扫描断言通过，且投影后正文与原始状态逐字节一致。

#### Scenario: 并发请求各自快照（并发）
- **WHEN** 同一进程并发发起两个不同模型/参数的请求
- **THEN** 两条快照分别绑定各自请求前的模型与参数，互不串写。

#### Scenario: 重启后快照不可重解释（重启）
- **WHEN** 采集进程重启后重新读取历史快照
- **THEN** 快照所绑定的模型/参数/可见集合版本 MUST 与写入时一致。

### Requirement: logprobs 三态
Native logprobs SHALL 仅在**响应确实返回**时标记为 `present`；provider 能力**声明** `unsupported` 时 SHALL 可追溯该声明来源；**无证据**时 MUST 标 `unknown`，MUST NOT 默认当作 `not_returned` 或 `unsupported`。

#### Scenario: 确实返回才 present（正常）
- **WHEN** 模型响应携带 logprobs 且被采集
- **THEN** 记录 MUST 标 `present` 并保留原始值。

#### Scenario: unsupported 需支持声明（失败）
- **WHEN** 记录标 `unsupported` 但无 provider 支持声明来源
- **THEN** 验收 MUST 失败，不得以缺省推断充当声明。

#### Scenario: 无证据不默认 not_returned（失败）
- **WHEN** 记录无 logprobs 证据且无声明
- **THEN** 状态 MUST 为 `unknown`，MUST NOT 标 `not_returned`。

#### Scenario: 重启后三态保持（重启）
- **WHEN** 采集进程重启后重新读取历史记录
- **THEN** 每条记录的 logprobs 三态 MUST 与写入时一致，不被重解释。

### Requirement: 采集与模型共享终局
采集包装 SHALL 与模型包装**共享同一终局**；发送准入与关闭排空 MUST 走**同一 protocol**；终局未 seal 时 MUST 记 `partial`，MUST NOT 自动删除旧文件或轮转绕过限额。

#### Scenario: 关闭与在途发送并发（并发）
- **WHEN** capture 关闭与在途发送并发发生
- **THEN** 准入与排空按同一 protocol 串行判定，MUST NOT 出现部分封账被当成完整。

#### Scenario: 未 seal 即关闭（失败）
- **WHEN** 关闭时仍有未 seal 的在途记录
- **THEN** MUST 记 `partial` 并给出丢失/在途计数，MUST NOT 虚构完整统计。

### Requirement: 记录与真实请求/尝试逐项对应

每一次真实发生的模型请求 MUST 对应一条或一组可核对的采集记录；未发生的请求 MUST NOT 产生记录，MUST NOT 预写或以重试补齐虚构请求。每一次真实发生的尝试（含失败、错误、准入被拒与流中断）SHALL 要么被记录为具名失败尝试（携带错误与终态），要么进入显式损失账目（`RecordingLosses` 语义）；MUST NOT 记为成功，MUST NOT 静默消失。采集计数 SHALL 可逐项对账：真实请求/尝试集合与「记录 + 显式损失」集合 MUST 按调用身份一一对应，MUST NOT 以「至少有一条记录」或总量非零代替逐项对应。记录与请求的关联 MUST 以该次调用已有的身份（invocation / call_id）为准，MUST NOT 以时间顺序或内容相似度匹配。

#### Scenario: 真实请求逐项有记录

- **WHEN** 一个进程内发生 N 次真实模型请求且采集全程可用
- **THEN** 每个请求 MUST 在记录中按调用身份找到其对应项（可为该请求的一组分片），无请求被遗漏
- **AND** MUST NOT 出现无对应请求的记录

#### Scenario: 失败尝试如实记录（失败）

- **WHEN** 一次模型尝试以错误或流中断结束
- **THEN** 该尝试 SHALL 被记录为具名失败（含错误与终态），MUST NOT 记为成功
- **AND** 其计数 SHALL 出现在失败/错误统计中，可与真实尝试数对账

#### Scenario: 准入被拒进入损失账目（失败）

- **WHEN** 采集准入被拒绝或队列满导致该次尝试无法落盘
- **THEN** 该次尝试 MUST 进入显式损失账目，MUST NOT 静默消失
- **AND** 损失计数 SHALL 与真实尝试数、记录数三者可对账

#### Scenario: 未发生请求不产生记录（失败）

- **WHEN** 请求在发出前被拒绝、取消或从未发起
- **THEN** MUST NOT 产生对应的模型请求记录，也 MUST NOT 计入尝试总数
- **AND** 采集计数 SHALL 与真实请求数一致
