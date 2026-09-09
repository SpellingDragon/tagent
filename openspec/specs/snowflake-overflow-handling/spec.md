# snowflake-overflow-handling Specification

## Purpose

本规范定义 snowflake-overflow-handling 能力。The Snowflake ID generator SHALL detect sequence exhaustion (12-bit sequence = 4096 IDs per millisecond) and block until the next millisecond before returning a

> **状态核验（2026-09-09，部分替代）**：本规范中「时钟回拨返回 `ErrClockBackwards` 错误并由调用方重试」的处置**已被更优方案替代**——`memory/types.go` `NewSnowflakeEventKey` 现将回拨**钉住在最后签发时间戳（pin-to-last）**，保证 key 单调不回退：这是压缩渲染冻结全窗锚点（`ref.EventKey >= boundary`，stable-context-compaction）对 key 单调性的硬依赖所驱动的（报错重试无法保证渲染锚点单调）。序列耗尽阻塞至下一毫秒的行为以 `memory/types.go` 现实现为准。本文与回拨报错相关的 SHALL 条款不再约束当前实现，保留作为设计决策记录。

## Requirements

### Requirement: Snowflake blocks on same-millisecond sequence exhaustion

The Snowflake ID generator SHALL detect sequence exhaustion (12-bit sequence = 4096 IDs per millisecond) and block until the next millisecond before returning a new ID. This SHALL prevent duplicate ID generation without changing the Snowflake bit layout or interface.

#### Scenario: Normal write rate never triggers blocking

- **WHEN** the generator produces IDs at 10 events/s
- **THEN** sequence SHALL never approach 4096 within a single millisecond; no sleep SHALL occur

#### Scenario: Burst beyond 4096 IDs in same millisecond blocks

- **WHEN** 5000 IDs are requested within a single millisecond
- **THEN** the first 4096 IDs SHALL be issued immediately
- **AND** the generator SHALL `time.Sleep(100µs)` in a loop until the next millisecond arrives
- **AND** the remaining 904 IDs SHALL then be issued with the new millisecond timestamp starting at seq=0

#### Scenario: Same millisecond detection after overflow

- **WHEN** sequence wraps to 0 within the same millisecond
- **THEN** the generator SHALL loop sleeping short intervals (100µs) until `time.Now().UnixMilli() > lastMs`, then update `lastMs` and continue

### Requirement: Clock regression aborts generation

If `time.Now().UnixMilli()` is strictly less than the previously recorded `lastMs` (system clock moved backwards), the generator SHALL return an error rather than issue a non-monotonic ID.

#### Scenario: Clock moved backwards returns error

- **WHEN** NTP adjusts the clock backward by 100ms and a subsequent `NextID()` observes `now < lastMs`
- **THEN** the call SHALL return a non-nil error `ErrClockBackwards`; the caller SHALL handle this by retrying after waiting
