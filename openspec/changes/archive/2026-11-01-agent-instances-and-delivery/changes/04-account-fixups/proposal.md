# 04 account-fixups：账目瑕疵与 route 端口注记预留（G5）

父变更：agent-instances-and-delivery（依赖：无）。

## Why

设计—实现探讨的收尾项：①上一变更 `send-cross-task-session` 的 09 域 `9.1` 叶勾选状态与父级账目不一致（父级已勾 09、9.2/9.3 说明引用 9.1 实证，但 9.1 行未勾）；②`assembly/run.go` 的 `CommitToSession` 注释仍写"反思回合投递摘要的主要通道"（现已多用途）；③`route` 端口按 Q14=b 注记预留（不占恰六契约位）。

## What Changes

- 09 域 9.1 勾选对齐（账目修正，凭据=W3-C 实证报告）。
- `CommitToSession` 注释更新为当前多用途事实。
- `api/plugin/material.go` 注释 + 设计文档登记"第七端口候选 `route`（事件→归属判断）"及准入条件（工作关联核心机制成型后开口）；恰六闭合契约与 `validatePort` **不动**（D12）。
- D13 取舍归档：Router 五决定等价性论证结论、暂不触发 5 处局部语义保留、FinalRound→HITL 远期注记——落设计文档记录。

## 边界与依赖

- **依赖**：无。**被依赖方**：无。
- **接口面**：注释与账目文本（零行为改动）。
- **禁止事项**：不动任何产品行为；不占端口位（Q14=b）；不动 `validatePort`。
