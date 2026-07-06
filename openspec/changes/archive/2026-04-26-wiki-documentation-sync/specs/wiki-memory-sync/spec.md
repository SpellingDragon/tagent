## ADDED Requirements

### Requirement: EventKey 定义为 Snowflake int64 而非 string
文档中 EventKey 所有出现处必须描述为 Snowflake int64 格式，包含完整的位布局图。

#### Scenario: 文档描述 EventKey 格式
- **WHEN** 读者阅读 §4.1 EventKey 章节
- **THEN** 看到的生成函数签名为 `NewSnowflakeEventKey(partitionID, sequence uint16) int64`
- **AND** EventKey 格式描述为 64-bit 整数而非 `"evt_{ts}_{seq}"` 字符串
- **AND** 包含位布局: PartitionID(11) + Timestamp(31) + Sequence(10) + Reserved(12)

#### Scenario: 文档示例使用 int64 EventKey
- **WHEN** 读者阅读因果链示例（§5）或代码片段
- **THEN** EventKey 示例值为 int64 数字格式（如 `1234567890123456`）而非 `"evt_1712000001000_000"`
- **AND** ParentKey 的零值表示为 `0` 而非 `""`

### Requirement: 数据结构包含 PartitionID 字段
FullEvent 和 EventReference 的结构体定义必须包含 PartitionID 字段。

#### Scenario: FullEvent 定义
- **WHEN** 读者阅读 §4.2 FullEvent 章节
- **THEN** FullEvent 的 EventKey 类型为 `int64`，ParentKey 类型为 `int64`
- **AND** 包含 `PartitionID int` 字段

#### Scenario: EventReference 定义
- **WHEN** 读者阅读 §4.3 EventReference 章节
- **THEN** EventReference 的 EventKey 类型为 `int64`
- **AND** 包含 `PartitionID int` 字段

### Requirement: MemoryStore 和文件存储反映分区设计
MemoryStore 的内部实现和文件布局描述必须反映按 PartitionID 分区的设计。

#### Scenario: InMemoryStore 内部结构
- **WHEN** 读者阅读 InMemoryStore 实现描述
- **THEN** 存储结构描述为 `map[int]map[int64]FullEvent`（二层 map）而非 `map[string]FullEvent`

#### Scenario: FileBackend 文件布局
- **WHEN** 读者阅读 FileBackend 实现描述
- **THEN** 文件路径包含分区子目录: `{dataDir}/{partitionID}/{eventKey}.json` 而非 `{dataDir}/{eventKey}.json`

### Requirement: StateDelta 包含 partition_id
MemoryPlugin 写入 StateDelta 的字段列表必须包含 `partition_id`。

#### Scenario: StateDelta 写入字段
- **WHEN** 读者阅读 MemoryPlugin OnEvent 描述
- **THEN** StateDelta 写入字段包括: `event_key`, `event_type`, `partition_id`
