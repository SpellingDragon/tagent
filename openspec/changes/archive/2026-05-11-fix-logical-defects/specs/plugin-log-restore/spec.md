## ADDED Requirements

### Requirement: MemoryPlugin 存储事件日志使用 Debug 级别
MemoryPlugin 存储事件成功时的日志 SHALL 使用 `log.Debugf` 而非 `log.Infof`，减少生产环境高频日志噪音。

#### Scenario: 存储成功时使用 Debugf
- **WHEN** MemoryPlugin 成功将 FullEvent 写入 MemoryStore
- **THEN** 日志 SHALL 使用 `log.Debugf` 级别
- **AND** 消息格式保持 `[Memory] stored key=%d partition=%d type=%s summary_len=%d`

#### Scenario: 存储失败时仍使用 Errorf
- **WHEN** MemoryPlugin 写入 MemoryStore 失败
- **THEN** 日志 SHALL 使用 `log.Errorf` 级别
- **AND** 包含失败原因和 event_key 信息
