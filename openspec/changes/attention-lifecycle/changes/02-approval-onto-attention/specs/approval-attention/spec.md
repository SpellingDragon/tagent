# approval-attention Specification

## Purpose

本规格定义审批迁入 Attention 后的领域契约：审批是 attention 的一种（kind=
approval），digest 绑定与三态裁决语义保持，真源为 attention 事实，文件面降为
外部写入口。

## Requirements

### Requirement: 审批登记即 attention 提问

一次调用挂起 SHALL 提交一条 kind=approval 的 attention question，其 lineage
SHALL 携带工具名、参数 digest 与调用身份；同 (tool, digest) 的重复登记 SHALL
读回同一条 question（幂等），MUST NOT 产生第二条在场问题。

#### Scenario: 同参重复挂起幂等

- **WHEN** 同一 (tool, argsDigest) 在裁决前被再次挂起
- **THEN** 在场集 SHALL 仍恰有一条该审批问题，且两次登记读回同一 question 身份

#### Scenario: 异参各自成题

- **WHEN** 同一工具以不同参数被挂起两次
- **THEN** 在场集 SHALL 有两条各自绑定自己 digest 的 question

### Requirement: 裁决是 answer 且绑定不放宽

一次裁决 SHALL 提交一条 attention answer；answer 的 lineage 与其 question 的
lineage SHALL 逐字匹配才生效，参数变更后的调用 MUST NOT 读到旧裁决的放行；
过期读侧判定 SHALL 保持"过期不是裁决"。

#### Scenario: 改参不复用旧裁决

- **WHEN** 调用 A(digest-1) 已 approved，同一工具以 digest-2 再次调用
- **THEN** 门 SHALL 挂起并产生新 question，MUST NOT 读到 digest-1 的放行

#### Scenario: 无簿 fail-closed

- **WHEN** 超限或高危调用到达而 attention 提交面不可用
- **THEN** 整批 SHALL 被拒绝执行（fail-closed），MUST NOT 回退为直接执行或截断

### Requirement: 裁决落定即清除在场

approved 或 denied 的 answer 落链后，该审批 SHALL 不再出现在场读回（Pending
面与 attention 在场集一致）；经文件面到达的外部裁决 SHALL 在落 answer 后撤下
其请求文件，而本进程裁决 SHALL 保留其文件作为通道审计记录；两种文件都 MUST
NOT 参与在场判定，外部 approver 的既有写入口（JSON 文件）SHALL 继续可用且
裁决路径不变。

#### Scenario: 症状锚——回答后不再在场

- **WHEN** 一个审批被批准并执行
- **THEN** 审批目录与在场读回面 SHALL 均不再报告该请求（此即症状锚
  `TestAttentionClearsApprovalOnAnswer` 的绿态）

#### Scenario: 外部文件裁决仍通

- **WHEN** 外部 approver 向请求 JSON 文件写入裁决
- **THEN** 裁决 SHALL 经既有 rescan 路径落为 attention answer 并清除在场

### Requirement: 内存表不得为真源

`HoldBook` 与 fuse seam 的挂起登记 SHALL 以 attention 为真源；任何仅存在于进程
内存的挂起/裁决状态 MUST NOT 影响重启后的门裁决（重启后读回与停机前一致）。

#### Scenario: 重启后裁决一致

- **WHEN** 挂起待裁时进程重启
- **THEN** 门对同一 (tool, digest) 的裁决读回 SHALL 与停机前一致，重复挂起
  MUST NOT 产生第二问
