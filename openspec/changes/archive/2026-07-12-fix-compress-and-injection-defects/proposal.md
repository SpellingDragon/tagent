## Why

运行日志暴露了 SmartCompressor 和 InjectEventKeys 的多个缺陷导致 ReAct 循环失控：SmartCompressor 触发但无法压缩（token 反而增加），InjectEventKeys 前缀被 LLM 模仿并叠加到后续消息中，配置值（max_tool_iterations=200, keep_recent_tasks=8）助长了循环持续时间。这些问题违反了原型的不变量——消息不可变性被破坏，Compact 无法有效清空投影。

## What Changes

- **InjectEventKeys 幂等化**：注入 `[evt_KEY|type]` 前缀前检查消息是否已含前缀，有则跳过。防止 LLM 输出中模仿的前缀格式被重复注入。
- **删除 protectPendingAsyncSegments**：该逻辑基于错误假设（`{status:running}` 代表"还在运行"），实际上 ActionTool 的 TmuxExecResponse 永远返回 `status:running`，导致几乎所有含 tool result 的段被保护，SmartCompressor 无法丢弃任何旧段。
- **SmartCompressor old_segments=0 时直接返回**：当所有 oldSegments 被 protect（或本就为空）时，不生成无意义的 `[context_compress] 压缩了 0 个对话片段` 消息。
- **配置修正**：tagent.yaml 中 `max_tool_iterations: 200 → 50`，`keep_recent_tasks: 8 → 2`。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities
- `event-key-visibility`: InjectEventKeys 幂等化——已有 `[evt_` 前缀的消息不再重复注入
- `view-transform-stateless`: SmartCompressor 在 old_segments=0 时不添加空 compress 消息；删除 protectPendingAsyncSegments

## Impact

- `agent/context_manager.go`: injectEventKeyPrefixes 添加幂等检查
- `agent/smart_compress.go`: 删除 protectPendingAsyncSegments 和 hasPendingAsyncResult；Compress 方法在 oldSegments 为空时提前返回
- `agent/chunk_splitter.go`: 删除 protectPendingAsyncSegments 的调用点（hasPendingAsyncResult 函数保留但不再被 Compress 调用）
- `examples/wechat-bot/tagent.yaml`: max_tool_iterations 200→50, keep_recent_tasks 8→2
- 测试：更新引用 protectPendingAsyncSegments 的测试
