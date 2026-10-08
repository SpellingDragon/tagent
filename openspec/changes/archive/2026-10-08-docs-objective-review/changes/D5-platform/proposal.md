# Proposal: D5 平台子系统域评审

## Why

平台子系统是"过度设计"质疑的最大嫌疑域：治理/自进化/可靠性/热更/资源所有权五大子系统**默认全关**，却占据 wiki 六篇文档与大量代码面（agent/org、agent/resources、evolution、agent/reliability）。维护成本账必须在这里算清。

## What Changes

只读评审 7 篇：`docs/wiki/platform/platform-subsystems.md`、`agent-behavior-matrix.md`、`org-hot-reload.md`、`resource-ownership.md`、`cognitive-asset-guard.md`、`reincarnation-notice.md`、`docs/wiki/evolution/evolution-architecture.md`（【P1 教训回写】自审补入，自进化总纲的展开篇）；≥2 个代码抽查；六维报告落盘 `docs/.dev/20261007-wiki-review-D5-platform.md`。

## 边界与依赖

- **依赖**：一级 design.md D0 统一框架；执行代际引擎侧引用 D4 报告（只读，未完成则自证并注明）。
- **被依赖方**：W2 汇总（尤其"过度设计清单"一节的主要证据来源）。
- **接口面**：报告路径与六维锚点契约同 D1。
- **禁止事项**：同 D1。
