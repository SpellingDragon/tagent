## MODIFIED Requirements

### Requirement: 前驱跟踪结构为按分区独立
MemoryPlugin 的前驱事件跟踪必须描述为按 PartititionID 独立跟踪。

#### Scenario: 结构体字段
- **WHEN** 读者阅读 §4 MemoryPlugin 结构体定义
- **THEN** 前驱跟踪字段为 `lastEventKeys map[int]int64` 而非 `lastEventKey string`
- **AND** 并发保护描述更新为面向分区级别的锁语义

#### Scenario: OnEvent 流程中的前驱获取
- **WHEN** 读者阅读 §5 OnEvent 流程的 step 4
- **THEN** 前驱获取代码为 `parentKey := p.lastEventKeys[partitionID]` 而非 `parentKey := p.lastEventKey`

### Requirement: EventKey 生成使用 Snowflake 格式
文档中的 EventKey 生成代码必须反映 Snowflake int64 的实际。

#### Scenario: OnEvent 流程中的 EventKey 生成
- **WHEN** 读者阅读 §5 OnEvent 流程的 step 1-2
- **THEN** EventKey 生成为 `eventKey := memory.NewSnowflakeEventKey(partitionID, 0)` 而非 `eventKey := memory.NewEventKey(timestamp, 0)`

### Requirement: StateDelta 包含 partition_id
MemoryPlugin 的 StateDelta 写入字段列表必须包含 partition_id。

#### Scenario: StateDelta 写入
- **WHEN** 读者阅读 \OnEvent 流程中 StateDelta 的写入
- **THEN** 写入字段包括 `event_key`, `event_type`, `partition_id`
