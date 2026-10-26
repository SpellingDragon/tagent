# failure-notice Specification

## Purpose

真失败的原始原因告知：模型请求真的失败且本次回合没有可交付的助手正文时，用户收到 provider 的原始错误文本，而不是静默、也不是一句无原因的兜底话术。反向边界同样属于本能力：有正文、取消、完成与宿主/协议错误四类情形必须不产生任何失败话术。

## Requirements

### Requirement: 真失败且无可交付正文时直投原始错误

当一次回合的终态为 `failed`、该回合没有可交付的 assistant 正文、且归约记录了错误原因时，系统 SHALL 经宿主既有的 `MessageSender` 直投该**原始错误文本**（provider/框架报错原文），MUST NOT 改写为原因码、分类短语或柔化系统话术。投递 MUST 恰发生一次；投递被拒 MUST 只记警告，MUST NOT 改变回合已结束的事实。触发条件 MUST 只认 `failed` 终态：`cancelled`、`completed`、`partial-only`、`unknown` 与宿主/协议错误 MUST NOT 触发。

#### Scenario: 原始错误原文抵达用户

- **WHEN** 一次用户回合以 `failed` 收尾、无可交付正文，且归约记录了 `500` 原因
- **THEN** 该会话 MUST 收到恰一条外发消息，正文 MUST 含原始错误的 `500` 标识
- **AND** 该正文 MUST NOT 等于任何固定兜底话术（不得出现旧的 `执行出错：本轮模型回合失败` 文案）

#### Scenario: 有正文时正文优先

- **WHEN** 一次回合的终态为 `failed`，但该回合已产出可交付的 assistant 正文
- **THEN** 系统 MUST 投递该正文，且 MUST NOT 追加任何错误话术
- **AND** 失败原因 MUST 只出现在日志与事实留痕中

#### Scenario: 取消与宿主错误不告知

- **WHEN** 回合以 `cancelled` 结束，或 `Invoke` 以宿主/协议错误返回（地址缺失、宿主关闭等）
- **THEN** 该会话 MUST NOT 收到失败告知
- **AND** 不得把宿主错误渲染成"模型回合失败"

### Requirement: 失败告知必须留痕且不得进入模型

每次真的投出失败告知，系统 SHALL 在会话事实链上提交一条**无模型载荷**的事实：类型为 `feedback`，summary 以 `failure-notice` 开头并携带原因首行，元数据携带 `terminal=failed`、原始原因与失败的 invocation 身份。该事实 MUST NOT 携带 assistant 或 user 角色的消息载荷，MUST NOT 使用通知元数据（`MetaKeyNoticeKind`/`MetaKeyTaskID`），因此 MUST NOT 进入任何模型请求、历史 transcript 或回放投影。事实提交失败 MUST NOT 阻断告知投递，但 MUST 记警告。

#### Scenario: 事实留痕且无载荷

- **WHEN** 一条失败告知被投出
- **THEN** 会话事实链上 MUST 出现一条 summary 前缀为 `failure-notice` 的事实，其 `Message.Content` MUST 为空
- **AND** 该事实的元数据 MUST 含 `terminal=failed` 与原始原因

#### Scenario: 留痕事实不驱动新回合

- **WHEN** 失败事实提交后模型再次被请求
- **THEN** 该请求 MUST NOT 含失败原因文本，也 MUST NOT 因该事实额外产生一次模型调用
- **AND** 该事实 MUST NOT 在回放投影中变成一条消息

#### Scenario: 留痕失败不阻断告知

- **WHEN** 事实提交被拒绝
- **THEN** 原始错误 MUST 仍被投递给用户
- **AND** 系统 MUST 留下一条警告日志

### Requirement: 失败原因必须进观测日志

模型请求的终端观测行在终态为 `failed` 且归约记录了非空原因时，SHALL 于同一行追加 `error=<原始原因>`（转义为单行）。该行 MUST 保持"一次请求一行"的既有契约：MUST NOT 为失败另起一条 INFO 行。成功终态的行 MUST NOT 出现该字段；失败但原因为空（如 nil stream）MUST NOT 渲染空的 `error` 字段。

#### Scenario: 失败行带原因

- **WHEN** 一次模型请求归约为 `failed`，原因文本含 `500`
- **THEN** 该请求的 INFO 观测行 MUST 同时含 `terminal=failed` 与 `error=` 及 `500` 原文
- **AND** 该行 MUST 仍是一条单行（原因中的换行 MUST 被转义）

#### Scenario: 成功行无原因字段

- **WHEN** 一次模型请求归约为 `completed`
- **THEN** 该请求的 INFO 观测行 MUST NOT 含 `error=`

#### Scenario: 原因缺失时不渲染空字段

- **WHEN** 一次模型请求归约为 `failed`，但归约未记录任何错误文本
- **THEN** 该行 MUST NOT 渲染 `error=""` 或等价的空字段
- **AND** 该缺口 MUST 由归约自身的告警可观测，MUST NOT 由失败告知编造一句兜底原因

### Requirement: 成功与静默边界不得被失败话术污染

系统 SHALL 保证失败告知只属于"`failed` 且无正文"这一种情形：成功的回合、成功但无正文的回合、以及因预算耗尽而正常收尾的回合 MUST NOT 产生任何失败话术或失败事实。旧的无原因兜底文案 MUST NOT 在任何一层（框架或应用层）回归。

#### Scenario: 成功回合零失败话术

- **WHEN** 一次用户回合以 `completed` 结束并产出正文
- **THEN** 该会话 MUST 只收到该正文，MUST NOT 收到失败告知
- **AND** 该回合 MUST NOT 留下 `failure-notice` 事实

#### Scenario: 成功但无正文保持静默

- **WHEN** 一次回合以 `completed` 结束但没有任何可交付正文（如只做了工具调用）
- **THEN** 该会话 MUST NOT 收到失败告知
- **AND** 该情形 MUST 只留既有 debug 记录

#### Scenario: 旧兜底文案不得回归

- **WHEN** 审阅框架与应用层的全部外发点
- **THEN** 任一外发点 MUST NOT 再出现无原因的固定失败话术
- **AND** 应用层 MUST NOT 在框架直投之外追加第二份失败文案（"恰一条"由框架保证）
