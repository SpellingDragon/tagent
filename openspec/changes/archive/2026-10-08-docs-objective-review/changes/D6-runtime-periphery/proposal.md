# Proposal: D6 外围运行域评审

## Why

RL 集成是 README 场景四"每次 LLM 调用记录为轨迹，可直连 AReaL 训练"的兑现面——训练友好性判准（判准 T）的正面战场；持久投递与 wechat 运行面是工程落地证据；comment-gate 是文档治理工具面。

## What Changes

只读评审 4 篇：`docs/wiki/rl/rl-architecture.md`、`docs/wiki/reliability/durable-delivery.md`、`docs/wiki/examples/wechat-bot-runtime.md`、`docs/comment-gate-tooling.md`；≥2 个代码抽查；六维报告落盘 `docs/.dev/20261007-wiki-review-D6-runtime-periphery.md`。

## 边界与依赖

- **依赖**：一级 design.md D0 统一框架；轨迹事件的来源形态引用 D2/D4 报告（只读，未完成则自证并注明）。
- **被依赖方**：W2 汇总（"训练友好性结论"的主要证据来源之一）。
- **接口面**：报告路径与六维锚点契约同 D1。
- **禁止事项**：同 D1。
