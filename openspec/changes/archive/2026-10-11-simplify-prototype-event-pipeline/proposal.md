## Why

按用户最新六点重新审视架构，而非延续上一轮 MVP 的生产保护与批次收齐假设。目标是用可运行的最小占位澄清事件、任务、agent 身份、插件和模型请求之间的关系，并通过 agent 入口的集成场景验证。

## What Changes

- 核心处理工具调用事件，任务变化通知所属 agent；删除批次收齐、派发排序栅栏及普通工具调用治理。
- 输入原样保留，不默认标记和折叠；插件按事件选择前后变换，低密度结果存 memory 并在使用后替换成票据。
- state 以 agent id 持有队列、上下文、任务和关系；治理只审架构定义的新增、替换、删除。
- config 从 JSON 文件热加载定义、以显式 Registry 解析模型/工具/skill；无状态模型代理接收完整请求并记录输入输出及错误。
- 增加 JSON 快照的跨进程恢复：所有者存在则恢复现场；不存在的 owner 及任务按孤儿回收。真实执行现场只留按任务 ID 回报的接缝，不自动重跑副作用。
- **BREAKING（prototype 内部）**：简化接口；删除旧四个单元测试/示例文件，改为 agent 集成测试及其子进程辅助场景。
- 更新每文件职责/未实现/接线说明与 prototype/README 架构图。

## Capabilities

### New Capabilities
- `orchestration`: 本变更的签名冻结、隔离并发与集成验收。领域规格下沉 changes/01-pipeline、02-state、03-adapters。

### Modified Capabilities
无生产主规格改动。上一轮未归档 prototype-mvp 的批次栅栏、工具治理、输入压缩要求被本轮用户明确取代；其历史产物不改写。

## Impact

仅 prototype 源码、README、集成测试与本 change。标准库 mock，不接生产模块、不改依赖、不提交、不归档。原型有意不做生产级幂等投递、崩溃事务、强杀、预算与自动审批。
