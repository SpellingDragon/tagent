## MODIFIED Requirements

### Requirement: 摘要素材为下层固化物

批量摘要（L3）的输入 SHALL 遵循素材律:段摘要素材=段内事件原文（第 0→1 层,唯一全文接触点）;卡片整理素材=卡片行（第 2 层固化物）。段摘要产出 SHALL 缓存（缓存键=段内容哈希,内容不变则跨轮命中）,同段跨轮 SHALL NOT 重摘、SHALL NOT 重复入库;产出 SHALL 挂 RelationStore 因果链并保留来源 key 集合。

#### Scenario: 同段跨轮不重摘

- **WHEN** 段 S 已在上轮归档为摘要 s,本轮压缩再次覆盖 S 范围
- **THEN** SHALL 复用 s,SHALL NOT 再次调用 LLM 对 S 重摘
