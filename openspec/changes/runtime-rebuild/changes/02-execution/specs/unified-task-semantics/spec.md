# unified-task-semantics delta（02-execution）

> 复用登记（非规范，不改本条语义）：工具、子 agent 与结果投递同为 owner 任务表管理的任务；任务 Registry 是 state 转移的**提交载体**，接手在状态转移**之前**。
> 本 delta 只修改「任务统一语义」「演进命名与治理插件分工」；本能力其余 Requirement（中间输出为内部输入事件、可选聚合窗不控制粒度）由主 spec `openspec/specs/unified-task-semantics/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 任务统一语义

工具、子 agent 与结果投递 SHALL 同为 owner 任务表管理的任务：投递经 Spawn 执行，成败以 ToolResult 通知事件回所属管线，消费循环不因投递失败终止。任务与管线的映射 SHALL 单点由 state 持有，任务通知一律经所属 AgentState 投递。

任务受理 SHALL 经**任务 Registry 作为 state 转移的提交载体**完成：Registry 在状态转移**之前**接入并接收一次受理，受理成功返回一次**受理回执（admission Receipt）**；后台结果 SHALL 作为**独立 observation** 回流，MUST NOT 冒充对某次 tool 调用的第二次同步 response。

三层结果 SHALL **分层分别表达、MUST NOT 以单一 enum 合并**（受理 ≠ 存储提交 ≠ 投递接纳）：

- **受理回执（admission Receipt）**：一次受理的结果（受理成功 / 受理失败），受理**不等于最终结果**；
- **存储提交结果**：`terminal + outbox` 一步同提交的结果，SHALL 分三态——**确定提交**（`committed`） / **确定未提交**（`not-committed`） / **未知**（`uncertain`，须可观察并告警），MUST NOT 一概宣称「双方未生效」；
- **投递接纳结果**：目标是否明确接纳该投递。幂等提交返回「已存在事实」**不等于通知已交付**；只有目标明确**接纳**才推进对应交付状态。

#### Scenario: 投递失败不杀循环

- **WHEN** Deliver 闭包返回错误
- **THEN** 循环继续消费，failed 通知进入上下文并驱动模型，模型可据通知收敛作答

#### Scenario: 管线关系单点

- **WHEN** 任务完成发出通知
- **THEN** 通知经所属 AgentState.Publish 投递，TaskManager 不持有第二份管线映射

#### Scenario: 受理先于状态转移

- **WHEN** 一次工具调用被受理失败
- **THEN** state 状态 SHALL NOT 已发生转移（受理在转移前），MUST NOT 出现「已转移未受理」幽灵

#### Scenario: terminal 与 outbox 一步同提交

- **WHEN** 一个任务进入终态并有待发通知
- **THEN** 终态与 outbox SHALL 一步同提交；持久档同提交成功后才对外可见与派发

#### Scenario: 存储提交三结果分别表达

- **WHEN** 一次 `terminal + outbox` 提交的结果为确定提交成功、确定未提交、或结果未知
- **THEN** 三者 SHALL 分别表达为 `committed` / `not-committed` / `uncertain`，MUST NOT 合并为「双方未生效」

#### Scenario: 受理回执独立于存储提交与投递接纳

- **WHEN** 一次工具调用被受理
- **THEN** 受理回执 SHALL 独立返回（受理不等于结果），MUST NOT 与存储提交结果或投递接纳结果用一个 enum 合并表达

#### Scenario: 幂等提交不等于已交付

- **WHEN** 同一变更重复提交并命中已存在事实
- **THEN** SHALL 返回已存在事实（幂等），MUST NOT 因此推进对应交付状态；只有目标明确接纳才推进

### Requirement: 演进命名与治理插件分工

架构变更管道 SHALL 命名为 Evolution（Eval→Benchmark→ApplyGate，语义不变）；运行时危险调用 SHALL 由治理插件在事件管线拦截——把命中调用变换为 denied ToolResult 事件，工具零执行、模型读到拒绝事实。

插件 SHALL 按相位分工：请求丰富 / 风险策略 / 视图选择 / 外置策略 / 提交后观测 / 最终呈现。执行前相位 SHALL 恰一次（`Before` 恰一次）；变换后的最终请求 SHALL 经核心复核身份与权限，并同时用于执行与记录；**拒绝**时工具零执行但产生具名配对结果；`After`（提交后观测/最终呈现）SHALL 仅呈现 **canonical commit 之后**的事实。

#### Scenario: 拦截即变换

- **WHEN** 模型调用命中 DenylistPlugin 名单的工具
- **THEN** 该工具函数零执行，模型上下文出现 denied 结果并据以继续

#### Scenario: Before 恰一次

- **WHEN** 一个调用经过执行前相位
- **THEN** 执行前策略 SHALL 恰执行一次，MUST NOT 重复运行

#### Scenario: After 不先于 canonical commit

- **WHEN** 一个调用被拒绝或提交失败
- **THEN** `After` 呈现 SHALL NOT 在被拒后仍呈现成功事实，MUST NOT 先于 canonical commit
