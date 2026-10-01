# compress-quality-fix Specification

## Purpose

本规范定义 compress-quality-fix 能力：压缩质量修复沉淀下的四条行为契约——引用解析的角色推断、事件引用的角色填充、投影按事件键的幂等去重、错误日志的区分度。各契约的实现符号已随压缩重构更名/重组，行为以现行载体为准（见下符号对照）。

## Requirements

> **符号对照（2026-09-09 核验，2026-10-02 修订）**：本文 Requirement 所指的实现符号已随压缩重构更名/重组，行为契约由现载体承载——`resolveReferenceToMessage` → `ContextCompressor.resolveRef`（Role 决定逻辑在其内：context_compress 摘要 → user 侧注记，正 key 事件按 ref.Role/EventType；见 context_compressor.go 注释）；`BuildEventReference` 的 Role 填充 → `plugin/memory_plugin.go` 事件管线（`inferEventInfo` 推断 + Response Role 直取，投影 sink 同点追加）；`findPendingUserMessage` 按 event key 去重 → `SessionProjection.Append` 的 EventKey 级幂等去重（`agent/compress/projection.go`，该条需求已按现行载体改写）；`generateSummary`（LLM 摘要超长拆分）→ 已随 legacy 管线退役（现 `synthesizeRollingNarrative` 编译期常量限长）。除已改写的去重条目外，原文保留作为质量修复决策记录。
> 2026-10-02 收敛：本页曾把全部需求整体重复一遍（历史吸收 append 而非 modify 所致）且 Purpose 行为截断残句，已收敛为单份并补全。

### Requirement: resolveReferenceToMessage infers Role from EventType

When `full.Response == nil` (event has no LLM response), `resolveReferenceToMessage` SHALL infer the message Role from `ref.EventType` using a deterministic mapping: external_input→user, agent_output→assistant, action_command→tool, thinking_plan→assistant. If `ref.EventType` is also empty, SHALL default to RoleUser (safe degradation). This SHALL NOT produce messages with empty Role.

#### Scenario: Event without Response gets correct Role

- **WHEN** GetEvent returns a FullEvent with Response=nil
- **AND** the EventReference has EventType="external_input"
- **THEN** the message Role SHALL be "user"
- **AND** the message Content SHALL be ref.EventSummary

#### Scenario: EventReference has empty EventType and Role

- **WHEN** both ref.EventType and ref.Role are empty
- **THEN** the message Role SHALL default to "user"

### Requirement: BuildEventReference infers Role when Response is nil

`BuildEventReference` SHALL set `ref.Role` from `evt.StateDelta["event_type"]` when `evt.Response == nil`, using the same EventType→Role mapping. This ensures all EventReferences have a non-empty Role.

#### Scenario: Event without Response gets Role from EventType

- **WHEN** BuildEventReference processes an event with StateDelta["event_type"]="external_input"
- **AND** evt.Response is nil
- **THEN** ref.Role SHALL be "user"

### Requirement: SessionProjection Append is idempotent by event key

同一事件不得两次进入装配上下文：`SessionProjection.Append`（`agent/compress/projection.go`）SHALL 对 `EventKey > 0` 且已投影过的引用跳过追加并记一条告警；`EventKey == 0`（无键）的引用 SHALL NOT 参与去重、总是追加。

#### Scenario: 同键第二次追加被跳过

- **WHEN** Append 收到 EventKey=1297370957781938176 的引用，而该键已在投影内
- **THEN** 该次追加 SHALL 被跳过，引用表不变

#### Scenario: 不同键正常追加

- **WHEN** Append 收到 EventKey=9999 的引用，投影内无该键
- **THEN** 该引用 SHALL 被追加

#### Scenario: 无键引用不参与去重

- **WHEN** Append 收到 EventKey==0 的引用
- **THEN** 该引用 SHALL 总是被追加（无键可去重）

### Requirement: resolveReferenceToMessage logs distinguish error types

The warn log in `resolveReferenceToMessage` SHALL distinguish between "GetEvent returned error" and "GetEvent succeeded but Response is nil".

#### Scenario: GetEvent returns error

- **WHEN** GetEvent returns (nil, error)
- **THEN** log SHALL say "GetEvent failed for key=N: <error>"

#### Scenario: GetEvent succeeds but no Response

- **WHEN** GetEvent returns (full, nil) but full.Response is nil
- **THEN** log SHALL say "event key=N has no Response, falling back to EventType inference"
