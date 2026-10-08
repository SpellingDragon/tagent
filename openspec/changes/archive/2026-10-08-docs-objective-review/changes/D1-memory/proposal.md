# Proposal: D1 记忆存储域评审

## Why

记忆是 tagent 的立身之本（README 首句即"记忆驱动的长期运行 Agent 框架"），memory-architecture.md 是全 wiki 最长一篇（1628 行）；票据召回"零幻觉"、TTL 遗忘曲线、LSM 分层是 README 四场景承诺的核心支撑机制。该域评审质量直接决定总报告"预期特性兑现度"结论。

## What Changes

只读评审 3 篇：`docs/wiki/memory/memory-architecture.md`、`docs/storage-durability-positioning.md`、`docs/wiki/platform/evaluation-suites.md`；对 ≥2 个关键断言做源码抽查；产出统一六维报告落盘 `docs/.dev/20261007-wiki-review-D1-memory.md`。

## 边界与依赖

- **依赖**：无实现依赖；统一评审框架（六维+判准 R/T+评分纪律）来自一级 design.md D0/D6（接口常数，编排者已定稿）。
- **被依赖方**：W2 汇总消费本域报告的六维评分与尖锐问题；D3（工具任务）的 recall 工具评审会引用本域的召回语义结论，冲突由编排者裁决。
- **接口面**：报告文件路径与章节锚点（六维+尖锐三问）为对外契约，不得增删改名。
- **禁止事项**：不修改任何源码与既有文档；不评审 D2-D6 的文章；不在报告中下跨域结论（留给 W2）。
