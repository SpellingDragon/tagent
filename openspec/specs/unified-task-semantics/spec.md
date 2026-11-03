# unified-task-semantics Specification

## Purpose
启动通知与任务同享"接纳≠交付≠确认"语义：标记完成的唯一依据是消费确认，重投同身份幂等。

归档溯源：三层分离语义由 `unify-task-evolution` 归档建立；通知接纳、投递元数据契约与投递账目条款由 `send-cross-task-session`（06）与 `agent-instances-and-delivery`（02）手工并入。
## Requirements
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

### Requirement: 中间输出为内部输入事件

带工具调用时的模型输出与推理 SHALL 作为 EventModelDelta 回送本 agent 事件管线消费（进上下文投影，不驱动模型、不外投）；Reply SHALL 仅在无工具调用时发布。

#### Scenario: 中间叙述可见但不收口
- **WHEN** 模型同时返回 ToolCalls 与 Output
- **THEN** Output 以 EventModelDelta 进上下文供后续轮次可见，不进 Deliver；子 agent 以无工具调用的 Reply 作最终回报

### Requirement: 可选聚合窗不控制粒度

任务完成通知 SHALL 支持按条数聚合（窗口默认 0 即发，攒满批量投递，关闭时 flush 残余）；模型触发粒度 SHALL 保持 pull 自然聚合，窗口不等待输入、不试图控制整体节奏。

#### Scenario: 窗口攒批
- **WHEN** TaskWindow=2 且两个任务先后完成
- **THEN** 两条通知合并投递，一次消费批次同时可见；窗口为 0 时逐条即发

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

### Requirement: 通知接纳与消费确认分离

启动通知 SHALL 先入稳定身份的持久接纳记录；`.notified`/`.consumed` 标记 SHALL 仅在消费确认（输入提交成功）后完成。入箱、取走 MUST NOT 被当作完成。重投 SHALL 复用同一接纳身份，MUST NOT 产生重复有效处理。

#### Scenario: 确认后才标记

- **WHEN** 通知入箱但输入提交失败
- **THEN** 标记不完成，通知在册可重投；恢复后恰被消费一次

#### Scenario: 重复恢复幂等

- **WHEN** 进程在标记完成后再次重启
- **THEN** 同身份通知不产生第二次有效处理

### Requirement: 投递元数据契约同构

agent 主动投递与框架状态变更投递 SHALL 是两类不同事实（类型保留区分），但其投递元数据契约 SHALL 同构：目标、投递身份、回执键、来源分账键字段集合一致（键走 `MetaKey*` 单点定义），去重与回执规则一致。下游 SHALL 可按同一账目口径消费两路投递。

#### Scenario: 两路投递一账可查

- **WHEN** 同一条源事件分别经 send 与经 settle 路径投到同一目标
- **THEN** 账目中两条记录元数据字段集合同构、去重规则一致，事实类型各自保留

### Requirement: 投递账目

一条源事件产生的多次单目标投递 SHALL 有可查询账目：逐目标的投递身份与回执状态可查可对账，查询 MUST NOT 要求解析正文。账目记录 SHALL 持久（随事实层），逐目标恰一次语义由投递身份去重保证。

#### Scenario: 多目标逐目标可对账

- **WHEN** 一条事件投给三个会话（三次单目标调用）
- **THEN** 账目列出三个目标各自的投递身份与回执，重复投递不产生第二条有效记录

