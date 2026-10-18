# feedback-binding（06-learning 子变更 delta）

> 本文件为二级子变更 `06-learning` 对既有主 spec `openspec/specs/feedback-binding/spec.md` 的 delta（主 spec **只读**）。
> `MODIFIED` 段为**完整源 Requirement 全量拷贝 + 本波新增约束**；未列出者视为继承主 spec 现行有效约束，**不默认「无 delta = 继承」**。

## Purpose

反馈绑定能力：经 EventTypeSpec 注册 feedback 事件、因果绑定到被评产出、guardrail 负反馈判据。本波在既有语义上补充 **feedback / reward 分开建模与版本化**，以及**模型替换闭环**（替换租约 + 绑定到模型版本，重建失败不发布错误绑定）。

## MODIFIED Requirements

### Requirement: 回执-反馈因果绑定

feedback 事件 MUST 经 RelationStore.SetParent 绑定到其评价的产出事件（parent），Content 为结构化 JSON（verdict/rating/note/source），零新索引。**本波新增**：reward MUST 为从 feedback **派生的版本化**记录，与 feedback 事件**分开建模**；MUST NOT 自动把任务终态映射为 reward。

#### Scenario: 任务结算自动反馈
- **WHEN** TaskManager settle 为 completed 或 failed
- **THEN** 对 spawn turn 的 agent_output 自动写入 task_settle 来源的 feedback 事件（completed→positive / failed→negative）；settle 为 suspect 时不写

#### Scenario: API 反馈
- **WHEN** 客户端 `POST /feedback`（event_key + verdict + note）
- **THEN** 写入 api 来源 feedback 事件并因果绑定该 event_key；event_key 不存在时返回显式错误

#### Scenario: 结算不直接产生 reward（失败，本波新增）
- **WHEN** 任务 settle 写入 feedback 事件
- **THEN** MUST NOT 同时把终态直接落为 reward；reward MUST 经版本化派生路径生成。

## ADDED Requirements

### Requirement: feedback/reward 分开且版本化
feedback SHALL 为反馈事件；reward SHALL 为从 feedback **派生的版本化记录**，携带派生规则版本与来源 feedback 引用；二者 MUST 分开建模与存储；MUST NOT 将任务终态自动映射为 reward，MUST NOT 以 trace 存在与否决定业务身份。

#### Scenario: 版本化派生（正常）
- **WHEN** 一条 feedback 到达并触发 reward 派生
- **THEN** reward 记录 MUST 带派生规则版本与来源 feedback 引用，历史 reward MUST NOT 被就地改写。

#### Scenario: 终态臆造奖励被拒（失败）
- **WHEN** 仅有任务终态而无 feedback 证据时尝试生成 reward
- **THEN** MUST 拒绝并标 `unrated`，MUST NOT 由终态臆造。

#### Scenario: 并发派生各版本独立（并发）
- **WHEN** 同一 decision 并发收到多条反馈派生
- **THEN** 各 reward 版本 MUST 独立可追溯，MUST NOT 因并发覆盖。

#### Scenario: 重启后派生规则版本保持（重启）
- **WHEN** 进程重启后重读 reward
- **THEN** 派生规则版本与来源引用 MUST 与写入时一致。

### Requirement: 模型替换闭环
模型替换 SHALL 以**租约**贯穿实际请求与流结束；替换 SHALL 使 feedback/reward 绑定到对应**模型版本**；重建失败 MUST NOT 发布错误绑定。

#### Scenario: 替换后新请求绑定新版本（正常）
- **WHEN** 模型替换租约生效后发起新请求
- **THEN** 该请求的 feedback/reward 绑定 MUST 指向新模型版本。

#### Scenario: 重建失败不发布（失败）
- **WHEN** 替换重建失败
- **THEN** MUST NOT 发布错误绑定，MUST 保持原模型且记录失败原因。

#### Scenario: 在途请求持旧版本（并发）
- **WHEN** 替换发生时有请求正在进行
- **THEN** 该请求 MUST 持续绑定其起始时的模型版本直至流结束，MUST NOT 中途改绑。

#### Scenario: 重启后租约恢复（重启）
- **WHEN** 进程重启后恢复替换租约
- **THEN** 绑定 MUST 与持久化版本一致，MUST NOT 出现跨版本串绑。
