# 压缩决策事件化与重放确定性复原（tagent-compress-event-sourcing）

## Why

tagent 换装/崩溃恢复时，WAL 事件层能完整重放（`SetReplayProjection→AppendProjectionRef`，build_agent.go:426-437），但压缩器的三样派生状态全在内存、零落盘（已实证）：`fullBoundary`（context_compressor.go:88）/ `thresholdBits`（L72）/ `meditationKeys`（L55）；`Compress` 方法体（L250-340）零落库。重启后首次 Compress 从零重推导：

- 卡片文本大概率逐字一致（由已存 EventSummary 确定性渲染），但 **full/compact 边界落点不保证重合**（自适应阈值轨迹漂移）→ 前缀缓存从边界偏移点整段失配，违背用户硬要求"恢复后 prompt 前缀缓存最大程度复用/基本一致"。
- **冥想保护集必丢**：重放路径无 MarkMeditationKey 调用（标记只在活路径 session.go:250 发生）→ "冥想内容不折叠"语义被静默打破。

两条正交补强：①注入源落库时在事件 metadata 盖 `trigger_source=meditation` 章（persistBusEvent 的 metadata 盖章处扩展），重放时按章派生 Mark；②压缩事件化——"时刻快照"恢复死亡瞬间的压缩器三态。

## What Changes

- **新增压缩事件**：`Compress` 成功且发生实际折叠（retained ≠ 原 refs）后，把压缩产出作为事件写入 WAL：payload 含 fullBoundary / threshold / meditationKeys 快照、被压缩 key 清单、卡片文本、RetainedRefs 全量（含 tool_chain 链与 prior summary 的 absorb 链路）。事件类型复用现有 `context_compress`（registry.go:208 已有 TTLDays:3, Synthetic: true），payload 用 JSON 元数据承载压缩快照。
- **重放状态机**：重放回调处状态机化——普通事件 → `AppendProjectionRef`（现状不变）；压缩事件 → ①按事件 payload 中 RetainedRefs **Replace 投影**（截断其覆盖的 key 区间，"清空"精确到该次压缩覆盖段，非全清）②Append 卡片 ref ③回灌压缩器三态（SetFullBoundary + UpdateThreshold + MarkMeditationKey×N）。
- **冥想标记正交补强**：注入源落库时盖 meditation 章，重放回调识别该章派生 MarkMeditationKey——与压缩事件的 meditationKeys 快照双保险，消除"活路径盖过、重放丢标记"盲区。
- **降级阶梯**：payload schema 完整 + 版本号；缺字段/版本不识别 → 显式降级回"重推导"路径（重放后首次 Compress 从零推导，与现状等价），禁止静默错用。
- **门禁**：build/vet/test 全绿 + push dev（tagent 仓库门禁）。

## Impact

- **代码**：`agent/compress/context_compressor.go`（快照导出与回灌入口）、`agent/compress/projection.go`（无结构变更，仅语义）、`agent/context_manager.go`（persistBusEvent 盖章扩展 + 压缩事件写入）、`agent/session.go`（makeOnEventCallback 冥想标记现状保留）、`tagent/build_agent.go`（重放回调状态机化）、`tagent/event/`（payload schema 常量与版本号定义）。
- **最终产物**：任何从 store 重建的路径（换装/OOM kill/新机器）免费获得确定性复原；长会话重放可快进（遇压缩事件跳过其覆盖段）；三态回灌后首次 Compress 确认现分区不重排 → 决策轨迹与旧进程逐字节重合。
- **测试**：三态回灌单测、重放状态机单测、降级路径单测、dogfood 验收（重放后首次装配 prompt 与死前最后一次装配逐字节 diff 为空）。
- **与 R10 的关系**：压缩事件化后重放与投影走同一入口（Replace 语义），R10（双重复原担忧）应随之消解，design.md 中论证。
- **范围外**：死亡瞬间未落盘内容的补救——由转世通报断点标记负责（`wechat-bot-reincarnation-notice` 变更），不在本变更范围。
