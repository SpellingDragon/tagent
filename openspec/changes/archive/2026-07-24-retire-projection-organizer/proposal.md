## Why

上下文摘要/压缩当前有两条路径，其中一条是多余的盲定时副本：

- **执行驱动（正路）**：`SmartCompressor` 在每个 turn 的 `BeforeModel` 运行，按 token 预算门控——低于预算且段数少就跳过，超预算才压缩最老的段。这本就是"随执行动态监测 + 按需压缩"。
- **盲定时（歪路）**：`ProjectionOrganizer` 用一个固定 **5m** ticker + 空闲 ≥1m 就调 `summary_model`（如 deepseek-v4-flash）预精炼旧 ref 摘要。它与执行驱动路径重复，且节奏与真实上下文压力**完全脱钩**——空闲也无脑烧 flash。

这违背"依据真实运行态行动"的原则（朝 L4 演进要避免这类盲机制）。应退役 organizer，让压缩**完全由执行驱动**。

## What Changes

- **移除 `ProjectionOrganizer`**：删除 `agent/projection_organizer.go` 及其在 `agent.go` 的装配、`lifecycle.go` 的 Start/Stop。
- 上下文压缩**仅**保留执行驱动路径（`SmartCompressor` @ `BeforeModel`，按 token 压力触发），不再有任何后台定时 LLM 调用。
- **保留 `summary_model`**：它继续供 `SmartCompressor` 的 Stage-2 摘要按需使用；退役后 summary_model 只在真实 token 压力下触发，不再有 5m 盲调。
- 接受权衡：压缩只在逼近预算时发生，个别 turn 可能一次压得多、延迟略高（organizer 原本想摊平此尖峰）——本变更明确选择"按需"而非"盲预压"。

## Capabilities

### New Capabilities
<!-- 无新增能力 -->

### Modified Capabilities
- `projection-organize`: **整体退役**（REMOVED）——空闲期主动投影整理机制被移除，其目标由执行驱动的按需压缩覆盖。

## Impact

- **代码**：删除 `agent/projection_organizer.go` + `agent/projection_organizer_test.go`（若有）；`agent.go` 移除 organizer 字段与初始化（`SummaryModel != nil` 分支中的 organizer 装配）；`lifecycle.go` 移除 organizer 的 Start/Stop。
- **行为**：不再有每 5m 的后台 `summary_model` 调用；空闲期零 LLM 开销。压缩时机=真实 token 压力。
- **保留**：`SmartCompressor`（含 Stage-2 `summary_model`）、`meditation` 的 idle 追踪（organizer 曾复用其 `LastEventTime`，移除后 meditation 不受影响）。
- **spec**：`openspec/specs/projection-organize/` 在归档时随 REMOVED delta 移除。
- **非目标**：执行驱动的渐进/摊薄压缩（方案 B）不在本变更内；如需再单独提。
