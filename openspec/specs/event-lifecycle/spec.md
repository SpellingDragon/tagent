# event-lifecycle Specification

## Purpose

本规范定义 event-lifecycle 能力。The system SHALL support a time-to-live (TTL) policy that marks events as tombstoned when their age exceeds a configurable threshold.

## Requirements

### Requirement: Time-based TTL expiration

The system SHALL support a time-to-live (TTL) policy that marks events as tombstoned when their age exceeds a configurable threshold. The TTL SHALL be configurable per partition and per event type. Expired events SHALL be marked tombstone but NOT immediately deleted—physical deletion SHALL occur during the next compaction cycle. The TTL scanner SHALL correctly extract the EventKey from the event's JSON value (not from the KV key) to perform tombstone checks, and SHALL use the event's actual `timestamp` field (not the window timestamp) for age calculation.

#### Scenario: Event expires by global TTL

- **WHEN** the system scans events and finds an event older than the configured `ttl_days` (default: 7)
- **THEN** the event SHALL be marked as tombstoned using its EventKey extracted from the event JSON value

#### Scenario: Event type-specific TTL overrides global TTL

- **WHEN** `external_input` has `type_ttl: 30` and the event is 10 days old (within type TTL but beyond global TTL of 7)
- **THEN** the event SHALL NOT be tombstoned (type-specific TTL takes precedence)

#### Scenario: Low-value event expires sooner

- **WHEN** `context_compress` has `type_ttl: 3` and the event is 4 days old
- **THEN** the event SHALL be marked as tombstoned

#### Scenario: TTL scanner correctly obtains EventKey from event value

- **WHEN** the TTL scanner processes a KV pair `{pid}:evt:{window_ts}:{seq}` → `{"event_key": 1777198738547555000, "event_type": "...", "timestamp": 1710678000000, ...}`
- **THEN** the scanner SHALL extract `event_key` from the JSON value (not from the KV key) and use it for `IsTombstone()` check and `MarkTombstone()` call

#### Scenario: TTL age calculated from actual timestamp

- **WHEN** the TTL scanner processes an event with `timestamp: 1710678000000` (07:00:00) stored in window `1710676800` (07:00:00 aligned)
- **THEN** the event age SHALL be calculated using the actual `timestamp` field (1710678000000), NOT the window timestamp (1710676800 * 1000)

### Requirement: Capacity-based eviction

The system SHALL support a maximum event count or maximum disk size per partition. When a partition exceeds the configured threshold, the oldest events SHALL be marked tombstone until the partition is back within limits. The eviction scanner SHALL correctly extract the EventKey from the event's JSON value (not from the KV key).

#### Scenario: Evict oldest events on capacity overflow

- **WHEN** a partition exceeds `max_events: 100000` and has 100,050 events
- **THEN** the oldest 50+ events SHALL be marked tombstone, bringing the live event count below the threshold

#### Scenario: Eviction preserves causal chain integrity

- **WHEN** the oldest event (E1) is marked tombstone and is the parent of E2 (which is not being evicted)
- **THEN** E2's parent reference SHALL be repaired to point to E1's nearest alive ancestor before E1 is tombstoned

#### Scenario: Eviction scanner correctly obtains EventKey

- **WHEN** the eviction scanner processes events to find the oldest ones
- **THEN** the scanner SHALL extract `event_key` from each event's JSON value and use it for `IsTombstone()` check and `MarkTombstone()` call

### Requirement: 生命周期策略经声明式配置注入

`LifecycleConfig` SHALL 可经 agent 的 memory 配置（YAML）注入，至少覆盖 `global_ttl_days`、`type_ttl`（按事件类型覆写，负值 = 豁免）、`check_interval`、`max_events_per_partition`。组合根 SHALL NOT 硬编码 `DefaultLifecycleConfig()`——未配置项回退默认值，显式配置优先。

#### Scenario: YAML 覆写全局 TTL

- **WHEN** agent 的 memory 配置声明 `global_ttl_days: 30`
- **THEN** LifecycleManager SHALL 以 30 天为全局 TTL，而非默认 7 天

#### Scenario: 未配置时沿用默认

- **WHEN** 配置中未声明任何生命周期字段
- **THEN** SHALL 沿用默认值（全局 7 天、类型 TTL 默认表、检查间隔 1 小时、容量不限）

#### Scenario: 可显式关闭 TTL

- **WHEN** 配置声明 `global_ttl_days: -1`（或等效的关闭语义）
- **THEN** TTL 扫描 SHALL NOT 标记任何事件为墓碑

### Requirement: TTL 扫描为周期性全量扫描

`LifecycleManager.checkTTL` SHALL 在每个检查周期遍历各分区的段与事件，按事件真实 `timestamp` 与其类型 TTL 判定过期并标记墓碑。扫描 SHALL 保持只读于事件内容（仅写 tomb 键），SHALL NOT 原地修改或删除事件。

该实现的成本为 O(分区内事件数)/周期；有界成本的增量扫描（游标或按段年龄剪枝）为已知缺口，记录于 memory 架构文档，SHALL 在单独变更中评估。

#### Scenario: 周期扫描标记过期事件

- **WHEN** 检查周期到达，分区内存在按其类型 TTL 已过期的事件
- **THEN** 这些事件 SHALL 被标记为墓碑，物理删除 SHALL 推迟到下一次压实

#### Scenario: 扫描不修改事件内容

- **WHEN** TTL 扫描完成一轮
- **THEN** 除 tomb 键外 SHALL NOT 产生任何对 evt/idx/meta 键的写入

> 取代原“TTL scan progresses via per-partition cursor”——该条款依赖的 `{pid}:ttl_cursor`（capability `ttl-cursor-scan`）从未实现，本 change 撤回该能力并如实描述当前行为。

### Requirement: 容量淘汰基于进程内事件计数与最旧段优先

`evictOldest` SHALL 依据 `PartitionState.eventCount`（进程生命期内计数，见 `event-segment-store`）判定超容量，并 SHALL 按段窗口从最旧到较新的顺序淘汰，达到目标数量即停止，SHALL NOT 继续扫描更新的段。

#### Scenario: 淘汰自最旧段开始且达标即停

- **GIVEN** 某分区超出 `max_events_per_partition`
- **WHEN** `evictOldest` 运行
- **THEN** SHALL 自最旧窗口开始标记墓碑，达到需淘汰数量后 SHALL 停止，不再扫描更新窗口

#### Scenario: 固化物豁免容量淘汰

- **WHEN** 淘汰过程中遇到 `context_compress_summary` 类型事件
- **THEN** 该事件 SHALL NOT 被标记墓碑（与 TTL 豁免同规则：原文可忘、固化物长存）

> 取代原“Capacity eviction avoids full-partition scan”——原条款依赖 `ttl_cursor.next_scan_window`（从未实现）。
