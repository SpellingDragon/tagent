## Why

设计评审裁决落地：任务统一语义（工具/子 agent/投递同为任务）、Governance 更名 Evolution（演进只审架构变更，运行时拦截归治理插件）、中间输出恢复内部事件回送、TaskManager 与管线的关系统一到 state。现有原型在四处与裁决不一致：投递失败杀死循环、带工具调用时的 Output 误发 Reply、Governance 命名占用生产语义、TaskManager 自带 emit 闭包副本。

## What Changes

- Governance→Evolution 全量改名（contracts/state/config/测试/README）；演进门审 StatePlan 不变。
- invokeModel 分流：ToolCalls 非空时 Output/Reasoning 作为 EventModelDelta 回送管线（进投影、不驱动、不投递）；Reply 仅在无 ToolCalls 时发布。
- 投递任务化：EventReply 路由改为经 owner 任务表 Spawn 一个 deliver 任务（Call.Name="deliver"），Deliver 闭包即执行体；投递失败成为 failed ToolResult 通知事件进管线，循环不终止。
- TaskManager 与管线的关系单点化：emit 闭包替换为持 owner *AgentState 引用、投递时调用 owner.Publish；新增可选条数聚合窗（TaskWindow，默认 0 即发，攒满批量 flush；Close 兜底），窗口编译自 AgentConfig；pull 自然聚合仍是模型触发粒度的唯一节奏，窗口不承担粒度控制。
- 治理插件示例：DenylistPlugin（Before 把命中名单的 ToolCall 变换为 denied ToolResult 事件，核心按变换后类型路由——模型读到拒绝事实；HITL 为同机制接缝）。
- wiki README 顶部标注历史设计地位并列出与原型的分叉点。
- **BREAKING（prototype 内部）**：AgentConfig 增 TaskWindow；事件常量增 EventModelDelta；上述签名/语义变更，测试同步迁移。

## Capabilities

### New Capabilities
- `unified-task-semantics`: 任务统一语义（投递任务化、聚合窗、管线关系单点）与演进命名契约。

### Modified Capabilities
无生产主规格改动；wiki 仅加历史标注不改机制内容。

## Impact

prototype 源码/两份集成测试/README、docs/wiki/README.md 顶部标注。无定时器（条数窗），无新依赖，不提交不归档。投递结果作为 ToolResult 会驱动模型，重投循环风险由模型决策自律，原型注释标注、不做输出通道治理。
