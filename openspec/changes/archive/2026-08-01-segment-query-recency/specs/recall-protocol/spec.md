# recall-protocol Delta

## ADDED Requirements

### Requirement: 查询类召回结果的诚实截断提示

查询类召回工具（`memory_query`、`memory_recent`、`memory_recall` 的 query 模式）在返回结果数恰好等于 limit 时，SHALL 在返回 message 中注明结果可能被截断（如"已达 limit，更旧的匹配未返回；可缩小时间范围或增大 limit"），SHALL NOT 让调用方（LLM）把"返回 N 条"误读为"全量只有 N 条"。结果数少于 limit 时 SHALL NOT 附加该提示。

#### Scenario: 达到 limit 时提示可能截断

- **GIVEN** 库中匹配事件多于 limit
- **WHEN** 查询类召回工具返回恰好 limit 条结果
- **THEN** 返回 message SHALL 包含截断提示与缩小范围/翻页的建议

#### Scenario: 未达 limit 时不提示

- **GIVEN** 库中匹配事件少于 limit
- **WHEN** 查询类召回工具返回全部匹配
- **THEN** 返回 message SHALL NOT 包含截断提示
