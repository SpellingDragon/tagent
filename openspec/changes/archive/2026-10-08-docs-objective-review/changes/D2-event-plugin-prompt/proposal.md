# Proposal: D2 事件插件 prompt 域评审

## Why

事件系统是 tagent"事件驱动替代 ReAct"的身份声明；plugin-architecture.md 高达 1030 行（MemoryPlugin 承担持久化+因果链+同点投影三大职责）；prompt 装配决定 LLM 实际所见上下文——判准 R（推理友好）的主战场。

## What Changes

只读评审 3 篇：`docs/wiki/event/event-architecture.md`、`docs/wiki/plugin/plugin-architecture.md`、`docs/wiki/prompt/prompt-architecture.md`；≥2 个代码抽查；六维报告落盘 `docs/.dev/20261007-wiki-review-D2-event-plugin-prompt.md`。

## 边界与依赖

- **依赖**：一级 design.md D0 统一框架（接口常数）。
- **被依赖方**：W2 汇总；D4（agent 引擎）评审 EventBus Pull 语义时会引用本域事件契约结论，冲突编排者裁决。
- **接口面**：报告路径与六维锚点契约同 D1。
- **禁止事项**：同 D1（只读、不越域、不下跨域结论）。
