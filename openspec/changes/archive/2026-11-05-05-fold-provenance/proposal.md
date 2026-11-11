# 折叠可追溯（fold-provenance）

父变更：boundary-closure（W1；依赖 W0 锚⑦红着基线）。

## Why

折叠摘要行 `entry.Type+": "+entry.Summary`（age_segments.go:419）剥除时间维度，跨重启回放的历史呈无时序团块——远端宿主 11-04 实证其诱发"老版本轨迹化石"误判；折叠摘要也不携带 recorder 世代标识，同类误判会复发（远端待裁决项 1/2）。

## What Changes

1. 折叠摘要行携带事件时刻（天级+时:分）与 recorder 世代标识（**完整信封 instance_id**，不截断）；**segment-fold 产物 Content 同样携带源行 provenance，historySummaryLine 对 segment-fold 产物改用其 Content**（第三处公式，保证二次折叠后源行仍可辨）。
2. ProjectionEntry 透传时刻/世代元数据（源头已具备：事件 100% 带显式时间戳）；**世代落未导出字段 `instanceID`（F11），经 `Generation()` 方法读取、`WithInstanceID` 方法设置，不新增导出字段**。
3. ~~AGENTS.md 注记修正~~（grill r1 F18：HEAD d6bfc363 已于 2026-11-04 修正两处注记，本域撤销该改动，仅保留防回归负断言）。

## 边界与依赖

- **依赖**：无实现依赖。
- **被依赖方**：无（呈现面改进，消费方是模型与人工读者）。
- **接口面**：不改 api/**；不改存储格式（摘要行是投影产物，重新折叠即再生）。
- **禁止事项**：不动 assembly/ 除 facts.go 的 mappedEntryOf 透传；不动 tool/exec、memory/ledger；不改变折叠触发阈值语义。
