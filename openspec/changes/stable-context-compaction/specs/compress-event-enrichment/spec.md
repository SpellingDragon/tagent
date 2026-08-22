## MODIFIED Requirements

### Requirement: Compress notice warns against repeating archived operations

The compress notice SHALL include a generic warning that the agent SHALL NOT re-execute operations whose outputs were compressed, regardless of whether the source was file reads, model outputs, search results, or API calls. The notice SHALL NOT enumerate specific tool names or call syntax (e.g. `recall({...})`, `search_content`, `read_file` with parameters) — tool availability is a per-agent assembly decision, and framework-generated text SHALL only carry facts and tickets (event keys), never advertise capabilities. Recovery guidance lives in tool declarations, which are always consistent with the actual assembly.

#### Scenario: Generic warning present

- **WHEN** `buildCompressEvent` is generated
- **THEN** the notice SHALL contain text equivalent to `**不要重复执行已被压缩的操作来获取相同内容**`
- **AND** it SHALL list the compressed events with their `evt_` key tickets (existing key/type/summary listing)
- **AND** it SHALL NOT contain tool-name references such as `search_content` or parameterized call examples
