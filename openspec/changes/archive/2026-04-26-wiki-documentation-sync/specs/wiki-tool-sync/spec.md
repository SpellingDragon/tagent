## MODIFIED Requirements

### Requirement: Snowflake/PartitionID 标注为已实现
文档中所有将 Snowflake EventKey 和 PartitionID 描述为"改进计划"或"未来设计"的段落必须更新为"已实现"。

#### Scenario: 计划状态更新
- **WHEN** 读者阅读 tool wiki 中涉及 Snowflake/PartitionID 的章节
- **THEN** 不出现"改进计划"、"未来设计"、"计划中"等词语描述这些已实现特性
- **AND** 明确标注当前已全面实现

### Requirement: RecallAgent 架构描述准确
RecallAgent 的架构描述必须反映当前实际为完整 TagentAgent 的实现。

#### Scenario: RecallAgent 描述
- **WHEN** 读者阅读 §3 组件关系图中 RecallAgent 的描述
- **THEN** 标注为"完整 TagentAgent + 内部 React 循环"而非"普通 CallableTool"
- **AND** 描述其子工具清单（memory_query/get/summarize/recent）

### Requirement: event_key 注入方式准确
event_key 的传递方式描述必须反映实际由 AgentToolWrapper 自行处理的机制。

#### Scenario: event_key 注入描述
- **WHEN** 读者阅读工具事件上下文传递的说明
- **THEN** 描述为 AgentToolWrapper 声明 event_keys 参数、自行解析 parentStore，而非"框架级自动注入"
