## Why

当前 SmartCompressor 在压缩历史上下文时采用"丢弃旧段 + LLM 生成单条摘要"策略，被压缩的 EventKey 列表只以裸数字形式放在 `[context_compress]` 消息中，LLM 不知道每个 key 对应什么内容，无法精确 recall。同时，SmartCompressor 的截断参数和 TmuxMonitor 的检测周期均为硬编码，无法通过 YAML 配置。

## What Changes

- SmartCompressor 的 `buildCompressEvent` 从"裸 key 列表"改为"key + type + summary 列表"：从 oldSegments 消息的 `[evt_KEY|type]` 前缀和内容中提取每个被压缩事件的类型和摘要，LLM 据此判断该 recall 哪个 key。**不创建新 MemoryStore 事件，不修改 SessionProjection**——严格遵守不变量 2（SmartCompressor 是纯视图变换）。
- `extractExecutionState` 从独立函数迁移为 SmartCompressor 方法，截断参数从包级常量迁移为可配置字段。
- 压缩参数和 TmuxMonitor 检测周期通过 YAML 配置化：AgentConfig `compress` 段 + ActionProperties `monitor` 段。
- ChunkSplitter 作为 `extractExecutionState` 的内部工具保留，用于在截断长工具输出时按语义边界（段落/标题）切分，而非机械按字符数截断。

## Capabilities

### New Capabilities
- `compress-event-enrichment`: buildCompressEvent 从 oldSegments 提取每个 EventKey 的 type + summary，生成可读的压缩事件清单，LLM 据此按需 recall

### Modified Capabilities
- `event-compaction`: extractExecutionState 迁移为 SmartCompressor 方法，截断参数可配置；ChunkSplitter 用于语义截断
- `action-tool-config`: ActionProperties 新增 monitor 配置项；TmuxMonitor 检测周期从 DefaultMonitorConfig 硬编码改为 YAML 可配置

## Impact

- `agent/smart_compress.go`: buildCompressEvent 改进（提取 key+type+summary）；extractExecutionState 迁移为方法
- `agent/chunk_splitter.go`: 保留，用于 extractExecutionState 的语义截断
- `agent/context_manager.go`: BeforeLLM 诊断日志
- `agent/tagent_agent.go`: TagentConfig 新增 CompressConfig，buildCompressorOpts 传递配置
- `config.go`: AgentConfig 新增 CompressConfig
- `tool/action/action_tool.go`: ActionProperties 新增 Monitor 配置，NewActionTool 支持自定义 monitor
- `builtin.go`: parseMonitorConfig 从 properties 解析 monitor 配置
## Why

当前 SmartCompressor 在压缩历史上下文时采用"丢弃旧段 + LLM 生成单条摘要"策略，大段工具输出（如 curl 文章、npm install 日志）被压缩为一条粗粒度摘要或直接丢弃。Layer 3 MemoryStore 虽有完整数据，但 LLM 在 Layer 1 不知道该 recall 哪些 EventKey，导致信息丢失后的重复工具调用循环。同时，SmartCompressor 的截断参数（maxExecStateChars 等）和 TmuxMonitor 的检测周期均为硬编码，无法通过 YAML 配置。

## What Changes

- SmartCompressor 在压缩旧段时，对大段工具输出执行**语义感知切分**：按内容结构（段落/标题/日志分隔符）拆分为多个 chunk，每个 chunk 生成独立摘要并持久化到 MemoryStore，SessionProjection 只保留轻量 EventReference。LLM 通过 messages 中的 chunk 摘要列表了解被压缩内容的全貌，按需通过 recall 工具检索完整 chunk。
- SmartCompressor 成为历史上下文处理的**唯一入口**：压缩策略、切分逻辑、摘要生成、截断参数全部在 SmartCompressor 中统一管理，不再散落在 extractExecutionState 等独立函数中。
- 压缩参数和 TmuxMonitor 检测周期**配置化**：通过 agent 级别 YAML 配置（AgentConfig）和 tool 级别 properties（ActionProperties）暴露，不再硬编码。
- 切分块写入 **SessionProjection + MemoryStore**（方案 Y）：正常路径下 LLM 通过框架 ContentRequestProcessor 看到原始消息，压缩后 SmartCompressor 从 MemoryStore 拉取完整内容切分并写入新 EventReference，LLM 通过 recall 工具按需检索。

## Capabilities

### New Capabilities
- `semantic-chunking`: SmartCompressor 对大段工具输出执行语义感知切分，生成多 chunk EventReference 持久化到 MemoryStore，LLM 通过摘要列表按需 recall

### Modified Capabilities
- `event-compaction`: SmartCompressor 从"丢弃+单条摘要"改为"切分+多 chunk 持久化+摘要列表"；extractExecutionState 逻辑合并进 SmartCompressor 统一管理
- `action-tool-config`: ActionProperties 新增 monitor 和 compress 配置项；TmuxMonitor 检测周期从 DefaultMonitorConfig 硬编码改为 YAML 可配置

## Impact

- `agent/smart_compress.go`: 核心改动，新增 ChunkSplitter、摘要生成、MemoryStore 写入逻辑
- `agent/context_manager.go`: SmartCompressor 构造需注入 MemoryStore 和 SessionProjection 引用
- `agent/tagent_agent.go`: TagentConfig 新增压缩配置字段，透传到 SmartCompressor
- `config.go`: AgentConfig 新增 compress 配置段；ToolRef.ActionProperties 新增 monitor/compress 配置
- `tool/action/action_tool.go`: NewActionTool 从 properties 读取 monitor 配置
- `tool/action/tmux_monitor.go`: DefaultMonitorConfig 支持外部覆盖
- `agent/task_segmenter.go`: extractExecutionState 迁移至 SmartCompressor 内部方法
- 测试：新增 chunk 切分单测、配置化测试、端到端压缩+recall 验证
