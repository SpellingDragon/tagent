# attention-primitive Specification

## Purpose

本规格定义 Attention 原语的领域契约：一次"向目标线程/人要一个答案"的完整生命
周期作为事实族落链，在场读回与清除全部由事实推导，不依赖内存态。

## Requirements

### Requirement: 一次要答案是一次 question 事实

向目标提出的一个问题 SHALL 以 `TypeAttentionQuestion` 事实落链，携带问题正文
（role=user）、目标身份（`attention.target`）、上下文血统（`attention.lineage`，
调用 ID/任务 ID/反思来源）与有效期（`attention.expires_ms`，0 表示随工作结束
清除）；同一问题身份的重复提交 SHALL 幂等（链上不出现第二条同身份 question）。

#### Scenario: 落链与幂等

- **WHEN** 以同一身份提交同一 question 两次
- **THEN** 链上 SHALL 恰有一条该身份的 question 事实，第二次提交读回第一次的键

#### Scenario: 血统随行

- **WHEN** 一个审批触发的 question 落链
- **THEN** 其 `attention.lineage` SHALL 携带被挂调用的身份与 digest，读回面可由血统反查

### Requirement: 裁决与清除各是一条事实

一次裁决 SHALL 以 `TypeAttentionAnswer` 事实落链（携带裁决正文与裁决人），一次
清除 SHALL 以 `TypeAttentionClearance` 事实落链（`attention.clearance` 取
answer|expiry|work-end）；answer 与 clearance MUST NOT 修改或删除已落的 question
事实（事实链只增）。

#### Scenario: 回答后清除

- **WHEN** 一个在场 question 收到其 answer
- **THEN** 在场读回面 SHALL 不再报告该 question，且链上 question 与 answer 两事实俱在

#### Scenario: 无 question 的 answer 被拒

- **WHEN** 提交一个 answer 而链上无对应 question 身份
- **THEN** 提交面 SHALL 以具名错误拒绝，链上 MUST NOT 落下孤儿 answer

### Requirement: 在场读回全部由事实推导

在场集 SHALL 从事实链推导：question 落链且无对应 answer、未过 `expires_ms`、
无 clearance 者在场；过期 SHALL 由读面按时间判定并在读侧报 expired，MUST NOT
依赖任何进程内存；重启后读回 SHALL 与停机前一致。

#### Scenario: 重启一致

- **WHEN** 停机前在场集非空，进程重启后读回
- **THEN** 在场集 SHALL 与停机前一致

#### Scenario: 过期读侧判定

- **WHEN** 一个 question 的 `expires_ms` 已过且无 answer
- **THEN** 读回面 SHALL 报告其 expired，且 MUST NOT 报告为在场待答

### Requirement: 对外面收敛为两个方法

attention 原语对消费者的面 SHALL 收敛为提交面与读回面两个方法；内部规则引擎
MUST NOT 导出（概念预算 S1：多态接口 ≤2 方法）。

#### Scenario: 概念预算

- **WHEN** 运行 `python3 scripts/gates/check_concept_budget.py --json`
- **THEN** attention 面的接口方法数 SHALL ≤2，无新增超标项
