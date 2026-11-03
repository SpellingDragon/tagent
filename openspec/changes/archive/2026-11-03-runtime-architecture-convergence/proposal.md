# 运行时架构概念收敛与反馈闭环

## Why

用户原始症状是：“系统设计不断膨胀，偏离核心框架设计，问题越改越多”，并且“CI 流水线耗时非常长，几乎难以忍受”。PR #36 删除了若干旧入口，但常驻驱动、通知结清、实例身份和关闭判据仍存在并行表达；最新 CI 失败同时包含正确性与活性信号。继续做局部补丁会把修复支架固化为公共领域概念。

本变更先以六个领域的“文档—规格—代码”独立拷问建立事实，再由编排者逐条裁定。前序审计和本提案中的修法均为候选，不是免审结论。

## What Changes

- 建立唯一的一级编排面，按地址消费、消息语义、关闭、契约、CI、压缩六个领域形成二级子变更。
- 以用户症状直译锚为先导：同一公开通信入口不得绕开地址 Consumer；关闭不得借修复之名隐式改变业务任务；CI 不得靠扩大矩阵或超时冒充提速。
- 冻结公共概念面：新增导出概念必须对应独立领域事实；实现围栏、缓存、补偿事务和读投影默认内化。
- 以公共入口的红→绿证据做最小垂直切片，再逐域删除并行状态机和失效规格。
- 将 CI 门禁调度与产品语义修改分线；完整长程证明若在 merge queue 执行，必须仍在进入 main 前完成。
- 每个修复批次后由不带历史结论的新审者复审；只有显式“frontier 空”且证据完整才可收口。

## Capabilities

一级只保留 orchestration 契约，领域 delta 下沉到 `changes/<NN-domain>/specs/`。

### New Capabilities

无。不得把实现支架注册为新 capability。

### Modified Capabilities（W0 已裁定；归档前须以 merged-spec preview 复核）

- `consumer-loop-convergence`：地址唯一驱动、答案归属与恢复。
- `cross-session-delivery` / `unified-task-semantics` / `task-reentry`：send 回执、通知消费和重入边界。
- `async-task-execution` / `attention-primitive` / `approval-attention` / `attention-clearance`：关闭与审批在场语义。
- `persistent-event-loop`：失效规范的移除或迁移，不允许 superseded 与现行 SHALL 并存。
- `architecture-guardrails`：公共概念与生产消费者门禁。
- `evals-harness`：CI 分层、矩阵与证据完整性。
- `task-skeleton-compression`：容量触发与长程成本的任何语义变化单独裁定。

## Impact

- 与既有活动变更的关系：`success-semantics` 已落地代码是输入基线，不替其补勾收尾；`runtime-simplicity-upgrade`、`event-driven-consumption` 与 `post-training-data-loop` 的实现或规格写入，本变更激活期间不得并行派发相同运行时/规格面的工作，其有效症状与契约作为输入吸收，避免第二套编排权威。
- 当前 PR 与工作树作为证据来源；W0 不修改产品、测试、现有规格或 CI。
- 后续影响 `runtime/agent`、`runtime/task`、`assembly`、`tool/subagent`、公开模型工具面、主 specs 与 CI workflows。
- 允许 BREAKING 删除重复公共面，但不允许为兼容补丁同时保留两个权威。
- 归档前必须证明用户原话场景不再发生；全量测试绿不能替代该证明。
