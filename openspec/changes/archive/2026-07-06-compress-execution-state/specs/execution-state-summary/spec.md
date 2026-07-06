## ADDED Requirements

### Requirement: 结构化执行状态摘要

SmartCompressor.Compress SHALL 从被压缩的 old segments 中提取结构化工具调用记录，追加为一个 `[执行状态]` system message 到压缩结果中。提取逻辑为纯代码（不依赖 LLM），100% 保证关键执行信息不丢失。

执行状态 message 的内容格式：
```
[执行状态]
- 调用: {tool_name}({args_summary})
  → 结果: {result_summary}
```

每条工具结果截断为 100 chars。总执行状态控制在 500 chars 以内，超过时保留最近的记录。

#### Scenario: 压缩结果包含执行状态

- **WHEN** SmartCompressor.Compress 丢弃 1 个 segment，该 segment 包含 `search_file` 调用（失败）和 `curl` 调用（成功）
- **THEN** 压缩结果中追加一个 system message
- **AND** 内容包含 "[执行状态]" 标记
- **AND** 包含 "调用: search_file(...)" 和 "→ 结果: Error: ..."
- **AND** 包含 "调用: action(...)" 和 "→ 结果: ..."

#### Scenario: 没有工具调用时不含执行状态

- **WHEN** 被压缩的 segments 中没有任何工具调用（只有 user/assistant 对话）
- **THEN** 不追加执行状态 message

#### Scenario: 执行状态超过 500 chars 时截断

- **WHEN** 被压缩的 segments 包含大量工具调用，执行状态总长超过 500 chars
- **THEN** 保留最近的记录，截断较早的
- **AND** 总长度不超过 500 chars

### Requirement: 摘要 prompt 保留执行状态

generateSummary 的 prompt SHALL 指示 LLM 保留工具调用的成功/失败状态和关键返回值，而非省略为"中间过程细节"。

#### Scenario: 摘要中包含工具调用状态

- **WHEN** LLM 生成摘要，输入包含 search_file 失败和 curl 成功
- **THEN** 摘要中保留 "search_file 失败" 和 "curl 成功" 的状态信息
- **AND** 保留关键返回值（如文件路径、命令输出摘要）
