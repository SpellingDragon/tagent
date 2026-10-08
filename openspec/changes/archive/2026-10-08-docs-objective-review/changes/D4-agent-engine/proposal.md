# Proposal: D4 agent 引擎域评审

## Why

agent 引擎是"事件驱动替代同步 ReAct"主张的实体：runEventLoop、turn 原语、冥想、子 Agent 同构协作、执行代际全在此域。判准 R（模型每步可见什么）的最终权责方。

## What Changes

只读评审 5 篇：`docs/wiki/agent/agent-architecture.md`、`docs/wiki/agent/event-flow.md`、`docs/wiki/agent/execution-generations.md`、`docs/wiki/agent/governance-enforcement.md`、`docs/wiki/agent/prototype-skeleton.md`；≥2 个代码抽查；六维报告落盘 `docs/.dev/20261007-wiki-review-D4-agent-engine.md`。

## 边界与依赖

- **依赖**：一级 design.md D0 统一框架；事件契约细节引用 D2 报告（只读引用，未完成则自证并注明）。
- **被依赖方**：W2 汇总；D5 评审执行代际/热更时引用本域 execution-generations 结论。
- **接口面**：报告路径与六维锚点契约同 D1。
- **禁止事项**：同 D1。
