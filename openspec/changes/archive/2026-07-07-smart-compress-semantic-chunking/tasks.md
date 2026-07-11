## 1. ChunkSplitter — 语义感知切分器

- [x] 1.1 新建 `agent/chunk_splitter.go`，定义 `ChunkSplitter` 结构体和 `ContentType` 枚举（markdown/json/log/plain）
- [x] 1.2 实现 `detectContentType(content string) ContentType`：通过前缀/模式匹配检测内容类型
- [x] 1.3 实现 `splitMarkdown(content string, chunkSize int) []string`：按 `#`/`##` 标题边界切分，超过 chunk_size 时在句号处断开
- [x] 1.4 实现 `splitJSON(content string, chunkSize int) []string`：解析 JSON 后按顶层 key 切分
- [x] 1.5 实现 `splitLog(content string, chunkSize int) []string`：按时间戳模式或 `---`/`===` 分隔符切分
- [x] 1.6 实现 `splitPlainText(content string, chunkSize int) []string`：按双换行段落切分，超限时在句号/分号处断开
- [x] 1.7 实现 `Split(content string, chunkSize int) []Chunk` 统一入口，返回 `[]Chunk{Content, Summary}`
- [x] 1.8 新建 `agent/chunk_splitter_test.go`，覆盖所有切分策略和边界情况（空内容、超短内容、无结构内容）

## 2. buildCompressEvent 改进 — key + type + summary 列表

- [x] 2.1 新增 `collectCompressedEventInfo(oldSegments) []EventInfo` 方法：从 oldSegments 消息中解析 `[evt_KEY|type]` 前缀，返回 `[]EventInfo{Key, Type, Summary}`
- [x] 2.2 改进 `buildCompressEvent`：用 `collectCompressedEventInfo` 替换 `collectCompressedKeys`，输出格式从裸 key 列表改为 `- evt_<KEY> [<type>]: <summary>` 列表
- [x] 2.3 保留 recall 提示行：`使用 recall 工具检索对应 key 获取完整内容`
- [x] 2.4 处理无前缀消息：回退到 `[unknown]` 类型 + content 截取

## 3. extractExecutionState 迁移为 SmartCompressor 方法

- [x] 3.1 将 `extractExecutionState` 函数迁移为 `(sc *SmartCompressor) extractExecutionState(segments) string` 方法
- [x] 3.2 将 `maxExecStateChars`、`maxToolResultChars`、`maxToolArgsChars` 常量迁移为 SmartCompressor 字段
- [x] 3.3 新增 SmartCompressorOption：`WithMaxExecStateChars`、`WithMaxToolResultChars`、`WithMaxToolArgsChars`
- [x] 3.4 更新 `Compress` 方法中对 `extractExecutionState` 的调用为 `sc.extractExecutionState`
- [x] 3.5 删除 `smart_compress.go` 中的包级常量和独立函数
- [x] 3.6 更新所有引用旧常量/函数的测试

## 4. 配置化 — AgentConfig 压缩参数

- [x] 4.1 在 `config.go` 的 `AgentConfig` 中新增 `Compress CompressConfig` 字段
- [x] 4.2 定义 `CompressConfig` 结构体：`MaxToolResultChars`、`MaxExecStateChars`、`ChunkSize`、`ChunkSummaryLen`（均有 json/yaml tag）
- [x] 4.3 设置默认值：maxToolResultChars=500, maxExecStateChars=2000, chunkSize=1000, chunkSummaryLen=150
- [x] 4.4 在 `tagent.go` 的 `buildAgent` 中，将 CompressConfig 转换为 agent.CompressConfig 传入 TagentConfig
- [x] 4.5 在 `agent/tagent_agent.go` 的 `TagentConfig` 中新增 `Compress` 字段
- [x] 4.6 在 `buildCompressorOpts` 中根据 CompressConfig 添加对应的 SmartCompressorOption

## 5. 配置化 — TmuxMonitor 参数

- [x] 5.1 在 `tool/action/action_tool.go` 的 `ActionProperties` 中新增 `Monitor *MonitorConfig` 字段
- [x] 5.2 修改 `builtin.go` 的 `actionFactory`：从 properties 解析 monitor 配置，转换为 `action.WithActionMonitorConfig` option
- [x] 5.3 修改 `NewActionTool`：当 monitorConfig 非空时，使用配置值创建 TmuxMonitor
- [x] 5.4 新增 `parseMonitorConfig` 辅助函数，支持 YAML 字符串解析 duration（如 "10s"）

## 6. ContextManager 注入

- [x] 6.1 确认 `ContextManagerConfig` 已有 `MemStore` 和 `Projection` 字段
- [x] 6.2 在 `newContextManagerFromConfig` 中将 memStore 和 projection 注入到 SmartCompressor
- [x] 6.3 SmartCompressor 的 BeforeModel 回调中能访问 memStore 和 projection（保留供未来使用，当前 buildCompressEvent 不需要写入）

## 7. 测试与验证

- [x] 7.1 新增 `buildCompressEvent` 改进单测：构造含 `[evt_KEY|type]` 前缀的 oldSegments，验证输出包含 key+type+summary 列表
- [x] 7.2 新增无前缀消息回退测试：验证 `[unknown]` 类型 + content 截取
- [x] 7.3 新增配置化单测：验证 WithMaxExecStateChars/WithMaxToolResultChars 等 option 生效
- [x] 7.4 新增 ActionProperties monitor 配置化单测：验证 parseMonitorConfig 正确解析 YAML
- [x] 7.5 回归测试：`go test ./agent/... ./plugin/... ./event/...` 全部通过
## 1. ChunkSplitter — 语义感知切分器

- [x] 1.1 新建 `agent/chunk_splitter.go`，定义 `ChunkSplitter` 结构体和 `ContentType` 枚举（markdown/json/log/plain）
- [x] 1.2 实现 `detectContentType(content string) ContentType`：通过前缀/模式匹配检测内容类型
- [x] 1.3 实现 `splitMarkdown(content string, chunkSize int) []string`：按 `#`/`##` 标题边界切分，超过 chunk_size 时在句号处断开
- [x] 1.4 实现 `splitJSON(content string, chunkSize int) []string`：解析 JSON 后按顶层 key 切分
- [x] 1.5 实现 `splitLog(content string, chunkSize int) []string`：按时间戳模式或 `---`/`===` 分隔符切分
- [x] 1.6 实现 `splitPlainText(content string, chunkSize int) []string`：按双换行段落切分，超限时在句号/分号处断开
- [x] 1.7 实现 `Split(content string, chunkSize int) []Chunk` 统一入口，返回 `[]Chunk{Content, Summary}`
- [x] 1.8 新建 `agent/chunk_splitter_test.go`，覆盖所有切分策略和边界情况（空内容、超短内容、无结构内容）

## 2. buildCompressEvent 改进 — key + type + summary 列表

- [x] 2.1 新增 `collectCompressedEventInfo(oldSegments) []EventInfo` 方法：从 oldSegments 消息中解析 `[evt_KEY|type]` 前缀，返回 `[]EventInfo{Key, Type, Summary}`
- [x] 2.2 改进 `buildCompressEvent`：用 `collectCompressedEventInfo` 替换 `collectCompressedKeys`，输出格式从裸 key 列表改为 `- evt_<KEY> [<type>]: <summary>` 列表
- [x] 2.3 保留 recall 提示行：`使用 recall 工具检索对应 key 获取完整内容`
- [x] 2.4 处理无前缀消息：回退到 `[unknown]` 类型 + content 截取

## 3. extractExecutionState 迁移为 SmartCompressor 方法

- [x] 3.1 将 `extractExecutionState` 函数迁移为 `(sc *SmartCompressor) extractExecutionState(segments) string` 方法
- [x] 3.2 将 `maxExecStateChars`、`maxToolResultChars`、`maxToolArgsChars` 常量迁移为 SmartCompressor 字段
- [x] 3.3 新增 SmartCompressorOption：`WithMaxExecStateChars`、`WithMaxToolResultChars`、`WithMaxToolArgsChars`
- [x] 3.4 更新 `Compress` 方法中对 `extractExecutionState` 的调用为 `sc.extractExecutionState`
- [x] 3.5 删除 `smart_compress.go` 中的包级常量和独立函数
- [x] 3.6 更新所有引用旧常量/函数的测试

## 4. 配置化 — AgentConfig 压缩参数

- [x] 4.1 在 `config.go` 的 `AgentConfig` 中新增 `Compress CompressConfig` 字段
- [x] 4.2 定义 `CompressConfig` 结构体：`MaxToolResultChars`、`MaxExecStateChars`、`ChunkSize`、`ChunkSummaryLen`（均有 json/yaml tag）
- [x] 4.3 设置默认值：maxToolResultChars=500, maxExecStateChars=2000, chunkSize=1000, chunkSummaryLen=150
- [x] 4.4 在 `tagent.go` 的 `buildAgent` 中，将 CompressConfig 转换为 agent.CompressConfig 传入 TagentConfig
- [x] 4.5 在 `agent/tagent_agent.go` 的 `TagentConfig` 中新增 `Compress` 字段
- [x] 4.6 在 `buildCompressorOpts` 中根据 CompressConfig 添加对应的 SmartCompressorOption

## 5. 配置化 — TmuxMonitor 参数

- [x] 5.1 在 `tool/action/action_tool.go` 的 `ActionProperties` 中新增 `Monitor *MonitorConfig` 字段
- [x] 5.2 修改 `builtin.go` 的 `actionFactory`：从 properties 解析 monitor 配置，转换为 `action.WithActionMonitorConfig` option
- [x] 5.3 修改 `NewActionTool`：当 monitorConfig 非空时，使用配置值创建 TmuxMonitor
- [x] 5.4 新增 `parseMonitorConfig` 辅助函数，支持 YAML 字符串解析 duration（如 "10s"）

## 6. ContextManager 注入

- [x] 6.1 确认 `ContextManagerConfig` 已有 `MemStore` 和 `Projection` 字段
- [x] 6.2 在 `newContextManagerFromConfig` 中将 memStore 和 projection 注入到 SmartCompressor
- [x] 6.3 SmartCompressor 的 BeforeModel 回调中能访问 memStore 和 projection（保留供未来使用，当前 buildCompressEvent 不需要写入）

## 7. 测试与验证

- [x] 7.1 新增 `buildCompressEvent` 改进单测：构造含 `[evt_KEY|type]` 前缀的 oldSegments，验证输出包含 key+type+summary 列表
- [x] 7.2 新增无前缀消息回退测试：验证 `[unknown]` 类型 + content 截取
- [x] 7.3 新增配置化单测：验证 WithMaxExecStateChars/WithMaxToolResultChars 等 option 生效
- [x] 7.4 新增 ActionProperties monitor 配置化单测：验证 parseMonitorConfig 正确解析 YAML
- [x] 7.5 回归测试：`go test ./agent/... ./plugin/... ./event/...` 全部通过
## 1. ChunkSplitter — 语义感知切分器

- [x] 1.1 新建 `agent/chunk_splitter.go`，定义 `ChunkSplitter` 结构体和 `ContentType` 枚举（markdown/json/log/plain）
- [x] 1.2 实现 `detectContentType(content string) ContentType`：通过前缀/模式匹配检测内容类型
- [x] 1.3 实现 `splitMarkdown(content string, chunkSize int) []string`：按 `#`/`##` 标题边界切分，超过 chunk_size 时在句号处断开
- [x] 1.4 实现 `splitJSON(content string, chunkSize int) []string`：解析 JSON 后按顶层 key 切分
- [x] 1.5 实现 `splitLog(content string, chunkSize int) []string`：按时间戳模式或 `---`/`===` 分隔符切分
- [x] 1.6 实现 `splitPlainText(content string, chunkSize int) []string`：按双换行段落切分，超限时在句号/分号处断开
- [x] 1.7 实现 `Split(content string, chunkSize int) []Chunk` 统一入口，返回 `[]Chunk{Content, Summary}`
- [x] 1.8 新建 `agent/chunk_splitter_test.go`，覆盖所有切分策略和边界情况（空内容、超短内容、无结构内容）

## 2. SmartCompressor 集成 ChunkSplitter

- [x] 2.1 在 `SmartCompressor` 结构体中新增字段：`chunkSize`、`chunkSummaryLen`、`memStore memory.MemoryStore`、`projection *SessionProjection`
- [x] 2.2 新增 SmartCompressorOption：`WithChunkSize`、`WithChunkSummaryLen`、`WithMemStore`、`WithProjection`
- [x] 2.3 在 `Compress` 方法中，Step 4（oldSegments/recentSegments 分割）之后、Step 5（summarizeBatches）之前，新增 chunk 处理逻辑：遍历 oldSegments 中的 RoleTool 消息，内容超过 chunkSize 时调用 ChunkSplitter.Split
- [x] 2.4 每个 chunk 调用 `memStore.StoreEvent` 持久化为 `tool_result_chunk` 类型 FullEvent，设置 parent EventKey 为原始事件的 key（从消息前缀 `[evt_KEY|type]` 解析）
- [ ] 2.5 调用 `projection.Append` 将 chunk 的 EventReference 追加到 SessionProjection
- [ ] 2.6 生成 chunk 摘要列表 system message，格式：`[压缩] 工具结果已切分为 N 个块:` + 每行 `- chunk_<KEY>: <summary>` + `使用 recall 工具检索对应 key 获取完整内容`
- [ ] 2.7 将 chunk 摘要列表 message 插入到压缩后的 messages 中（替代被丢弃的原始 tool result）

## 3. extractExecutionState 迁移为 SmartCompressor 方法

- [x] 3.1 将 `extractExecutionState` 函数迁移为 `(sc *SmartCompressor) extractExecutionState(segments) string` 方法
- [x] 3.2 将 `maxExecStateChars`、`maxToolResultChars`、`maxToolArgsChars` 常量迁移为 SmartCompressor 字段
- [x] 3.3 新增 SmartCompressorOption：`WithMaxExecStateChars`、`WithMaxToolResultChars`、`WithMaxToolArgsChars`
- [x] 3.4 更新 `Compress` 方法中对 `extractExecutionState` 的调用为 `sc.extractExecutionState`
- [x] 3.5 删除 `smart_compress.go` 中的包级常量 `maxExecStateChars`、`maxToolResultChars`、`maxToolArgsChars` 和独立函数 `extractExecutionState`
- [x] 3.6 更新所有引用旧常量/函数的测试

## 4. 配置化 — AgentConfig 压缩参数

- [x] 4.1 在 `config.go` 的 `AgentConfig` 中新增 `Compress CompressConfig` 字段
- [x] 4.2 定义 `CompressConfig` 结构体：`MaxToolResultChars`、`MaxExecStateChars`、`ChunkSize`、`ChunkSummaryLen`（均有 json/yaml tag）
- [x] 4.3 设置默认值（在 `applyDefaults` 或 `NewTagentAgent` 中）：maxToolResultChars=500, maxExecStateChars=2000, chunkSize=1000, chunkSummaryLen=150
- [x] 4.4 在 `tagent.go` 的 `buildAgent` 中，将 CompressConfig 转换为 SmartCompressorOption 传入 `TagentConfig`
- [x] 4.5 在 `agent/tagent_agent.go` 的 `TagentConfig` 中新增 `Compress CompressConfig` 字段
- [x] 4.6 在 `buildCompressorOpts` 中根据 CompressConfig 添加对应的 SmartCompressorOption

## 5. 配置化 — TmuxMonitor 参数

- [x] 5.1 在 `tool/action/action_tool.go` 的 `ActionProperties` 中新增 `Monitor MonitorConfig` 字段（json/yaml tag）
- [x] 5.2 修改 `builtin.go` 的 `actionFactory`：从 properties 解析 monitor 配置，转换为 `action.WithMonitorConfig` option
- [x] 5.3 修改 `NewActionTool`：当 ActionToolOption 包含 monitor 配置时，使用配置值创建 TmuxMonitor 而非 DefaultMonitorConfig
- [x] 5.4 更新 `MonitorConfig` 的 duration 字段支持 YAML 字符串解析（如 "10s"）

## 6. ContextManager 注入 MemoryStore 和 Projection

- [x] 6.1 在 `ContextManagerConfig` 中新增 `MemStore` 和 `Projection` 字段（如果还没有）
- [x] 6.2 在 `NewContextManager` 中，将 memStore 和 projection 传递给 SmartCompressor 构造
- [x] 6.3 确认 SmartCompressor 的 BeforeModel 回调中能访问 memStore 和 projection（用于 chunk 持久化和 EventReference 追加）

## 7. 测试与验证

- [x] 7.1 新增 SmartCompressor chunk 切分单测：mock MemoryStore + SessionProjection，验证大段 tool result 被正确切分、持久化、追加到 projection
- [x] 7.2 新增 SmartCompressor 配置化单测：验证 WithChunkSize/WithMaxExecStateChars 等 option 生效
- [x] 7.3 新增 ActionProperties monitor 配置化单测：验证 YAML 解析和 TmuxMonitor 参数覆盖
- [x] 7.4 新增端到端压缩测试：构造包含大段 tool result 的消息序列，压缩后验证 messages 中包含 chunk 摘要列表、MemoryStore 中有 chunk 事件、projection 中有 chunk EventReference
- [ ] 7.5 验证因果链：chunk 事件的 parent 指向原始 tool result 的 EventKey
- [x] 7.6 回归测试：`go test ./agent/... ./plugin/... ./event/... ./tool/action/...` 全部通过
