## Context

`agent/projection_organizer.go` 提供 `ProjectionOrganizer`：独立 goroutine，`CheckInterval`（默认 5m）ticker + `MinIdleGap`（默认 1m）空闲门槛，空闲时用 `summaryModel` 精炼 projection 中较旧、较长的 ref 摘要。它在 `agent.go`（`cfg.SummaryModel != nil` 时构造）装配、在 `lifecycle.go` 随事件循环 Start/Stop，并复用 `meditationMgr.LastEventTime()` 做 idle 判定。

同一目标（缩小上下文）已由 `SmartCompressor`（`BeforeModel`，按 token 预算门控）在执行路径上按需完成。organizer 是与之重复的盲定时预压。

## Goals / Non-Goals

**Goals:**
- 移除 `ProjectionOrganizer` 及其全部装配，消除每 5m 的后台 `summary_model` 盲调用。
- 上下文压缩仅由执行驱动路径（`SmartCompressor`）负责。
- 保持 `summary_model`、`SmartCompressor`、`meditation` 行为不变。

**Non-Goals:**
- 执行驱动的渐进/摊薄压缩（方案 B）。
- 改动 `SmartCompressor` 的压缩算法或阈值。

## Decisions

- **D1 整体删除而非开关**：organizer 相对执行驱动压缩无独有价值，保留开关只会留下死代码与配置面。直接删除源文件 + 装配 + 测试。
- **D2 保留 `summary_model` 语义**：`cfg.SummaryModel` 继续经 `WithSummaryModel` 注入 `SmartCompressor` 的 Stage-2（`agent.go` 中该分支保留），仅删掉同一分支里 organizer 的构造。
- **D3 idle 追踪不受影响**：organizer 是 `meditationMgr.LastEventTime()` 的消费者而非提供者；删除消费者后 meditation 自身逻辑与 `lastEventTime` 维护不变。
- **D4 清理引用**：删除 `ta.organizer` 字段、`NewProjectionOrganizer` 调用、`lifecycle.go` 的 `organizer.Start()/Stop()`，确保无悬空引用（编译期兜底）。
- **D5 spec 退役**：以 `## REMOVED Requirements` + Reason/Migration 移除 `projection-organize` 能力；归档时其主 spec 目录随之删除。

## Risks / Trade-offs

- **压缩延迟尖峰**：压缩只在逼近/超预算时发生，个别 turn 可能一次压较多段、延迟略增。已按方案 A 显式接受——换取空闲期零 LLM 开销与机制单一。若尖峰在实测中成问题，另开方案 B（执行驱动渐进压缩）。
- **旧 ref 摘要不再被动精炼**：projection 中较长的旧摘要不再被提前压缩；但它们在真正进入 token 压力时会被 `SmartCompressor` 处理，功能不缺失，只是时机后移到"需要时"。
- **删除面**：涉及三处（源文件、agent 装配、lifecycle）；靠 `go build ./...` + 现有测试兜底，确保无残留引用。
