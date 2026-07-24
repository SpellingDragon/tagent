## REMOVED Requirements

### Requirement: 空闲期主动投影整理

**Reason**: 与执行驱动的按需压缩重复。`SmartCompressor` 已在每个 turn 的 `BeforeModel` 按 token 预算门控地压缩上下文（低于预算跳过、超预算压最老段），本就是"随执行动态监测 + 按需压缩"。而本机制用固定 5m ticker + 空闲盲轮询调用 `summary_model` 预精炼旧 ref 摘要，节奏与真实上下文压力脱钩、空闲也无脑消耗 LLM，违背"依据真实运行态行动"的原则。

**Migration**: 无调用方需迁移。上下文缩小改由执行驱动的 `SmartCompressor` 在真实 token 压力下按需完成；`summary_model` 保留，继续供其 Stage-2 摘要使用（仅在压力触发时调用，不再有 5m 盲调）。实现上移除 `ProjectionOrganizer` 及其在 `agent.go` / `lifecycle.go` 的装配后，空闲期不再产生任何后台 LLM 调用。`meditation` 的 idle 追踪不受影响（organizer 仅是其消费者）。
