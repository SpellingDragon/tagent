# failure-notice Specification（delta，MODIFIED 于既有能力）

> 5.9 裁决（r9 Blocker 1）：MODIFIED 头逐字匹配主 spec `openspec/specs/failure-notice/spec.md` 的「### Requirement: 成功与静默边界不得被失败话术污染」。本 delta **全文保留既有条款**，仅在「成功但无正文保持静默」场景的 THEN 增豁免句：未清 held 交付义务存在时按 delivery-closure 能力的延迟告知恰发一条（非失败话术）。此修改落实用户既有裁决（转人工必告知），非新决策；不新增 failure-notice 通道、不改 failed 触发条件。

## MODIFIED Requirements

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
- **AND** 除非存在未清 held 交付义务——此时按 delivery-closure 能力的延迟告知恰发一条（非失败话术）

#### Scenario: 旧兜底文案不得回归

- **WHEN** 审阅框架与应用层的全部外发点
- **THEN** 任一外发点 MUST NOT 再出现无原因的固定失败话术
- **AND** 应用层 MUST NOT 在框架直投之外追加第二份失败文案（"恰一条"由框架保证）
