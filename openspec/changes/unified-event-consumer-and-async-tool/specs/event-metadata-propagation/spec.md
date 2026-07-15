## 能力: event-metadata-propagation

事件级元数据从 InjectMessage 入口传播到事件因果链上的所有派生事件。

## 需求

### 新增 API

```go
// InjectMessageWithMetadata injects a message with a source label and
// arbitrary metadata. The metadata is propagated to all events derived
// from this message via event.StateDelta with "meta_" prefix.
//
// Common metadata keys:
//   - "chat_id": target user/session identifier for response routing
//   - "user_name": human-readable user identifier for logs
//   - "channel": communication channel (wechat, discord, etc.)
func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string)
```

### 传播规则

1. `InjectMessageWithMetadata` 时，metadata 被存入 `AgentEvent.Metadata`
2. `runEventLoop.Pull` 后，从 batch 中提取 external_input 事件的 metadata 作为"根 metadata"（多个 external_input 时，取最新的一个）
3. `ContextManager` 在 `RunFlow` 期间维护 `currentMetadata map[string]string` 字段
4. `MemoryPlugin.OnEvent` 时，将 `ContextManager.currentMetadata` 中的所有键值以 `meta_` 前缀写入 `evt.StateDelta`
5. Consumer 从 `evt.StateDelta["meta_chat_id"]` 等键读取路由信息

### 键名约束

- 所有 metadata 键在 StateDelta 中带 `meta_` 前缀
- 保留 metadata 值类型为 string（简化传播）
- 冲突处理：如果 metadata key 已经带 `meta_` 前缀，不重复添加

### 边界情况

- 无 metadata 的 `InjectMessage` 调用向后兼容（metadata 为 nil map）
- 冥想事件默认无 metadata（不需要路由）
- 空 metadata 键或值：忽略该键（不写入 StateDelta）

### 接口

```go
// AgentEvent (已存在于 event_bus.go)
type AgentEvent struct {
    // ... 现有字段 ...
    Metadata map[string]any  // 已有字段，本变更扩展其使用
}

// ContextManager 新增字段
type ContextManager struct {
    // ... 现有字段 ...
    currentMetadata map[string]string  // 当前 RunFlow 的 metadata
}

// ContextManager 新增方法
func (cm *ContextManager) SetInvocationMetadata(md map[string]string)
```

### 约束

- metadata 传播不影响事件的核心内容（Content/EventKey/EventType）
- metadata 是"透明通道"——tagent 框架不解释具体键值，仅负责传播
- Consumer 是唯一解释 metadata 的地方
