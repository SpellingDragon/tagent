# decision-capture（06-learning 子变更 delta）

> 本文件为二级子变更 `06-learning` 对既有主 spec `openspec/specs/decision-capture/spec.md` 的 delta（主 spec **只读**）。
> `MODIFIED` 段为**完整源 Requirement 全量拷贝 + 本波新增约束**（非截取片段）；未列出者视为继承主 spec 现行有效约束，**不默认「无 delta = 改写/继承」**。

## Purpose

定义运行时决策采集：请求边界安全快照、原生响应/logprobs 保留、有界非阻塞录制与可证明封账。本波在既有终局采集语义上补充：请求前快照、secrets 不落盘、logprobs 三态、采集与 model 共享终局。

## MODIFIED Requirements

### Requirement: 封账完整性可证明
FlushAndWait SHALL 返回writer确认后的manifest；存在inflight/drop/error、未同步或无manifest MUST NOT 声称完整。统计读取不得依赖可能已满的数据队列。**本波新增**：writer **未封账**（crash/未 flush）时 MUST 记 `partial` 并保留原因，MUST NOT 宣称完整；发送准入与关闭排空 MUST 走同一 protocol（采集与 model 共享终局）；封账统计 SHALL 支持按文件摘要复核。

#### Scenario: 关闭与flush失败
- **WHEN** capture结束或flush失败
- **THEN** 成功封账可按文件摘要复核，失败保留unknown/partial及原因，不能伪全量。

#### Scenario: 崩溃且未封账记 partial（失败，本波新增）
- **WHEN** 进程在 writer 未封账时退出，重启后读取该 capture
- **THEN** 记录状态 MUST 为 `partial` 并附未封账原因，MUST NOT 被判为完整。

## ADDED Requirements

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
