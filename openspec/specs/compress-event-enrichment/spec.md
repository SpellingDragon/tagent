## ADDED Requirements

### Requirement: buildCompressEvent outputs key + type + summary list

When SmartCompressor discards old task segments, `buildCompressEvent` SHALL extract each compressed event's EventKey, EventType, and a short content summary from the `[evt_KEY|type]` prefix injected by InjectEventKeys. The output message SHALL list each event as `- evt_<KEY> [<type>]: <summary>` followed by a recall hint. This enables the LLM to understand what was compressed and selectively recall specific events by key.

#### Scenario: Compressed events listed with type and summary

- **WHEN** SmartCompressor compresses 3 segments containing events with keys 1234, 1235, 1236
- **THEN** the compress message SHALL contain:
  ```
  [context_compress] 压缩了 3 个对话片段:
  - evt_1234 [external_input]: 用户请求获取文章
  - evt_1235 [thinking_plan]: 好的，我来帮你获取...
  - evt_1236 [action_command]: 调用工具: action({"command":"curl ..."})
  
  使用 recall 工具检索对应 key 获取完整内容。
  ```

#### Scenario: Events without prefix fall back to key-only listing

- **WHEN** a compressed message has no `[evt_KEY|type]` prefix (e.g., system message)
- **THEN** it SHALL be listed as `- evt_<KEY> [unknown]: <content[:summary_len]>`

#### Scenario: No memory store or projection side effects

- **WHEN** SmartCompressor compresses old segments
- **THEN** no new events SHALL be written to MemoryStore
- **AND** no new EventReferences SHALL be appended to SessionProjection
- **AND** only `args.Request.Messages` (the view) is modified
