## Why

极限测试中发现 SmartCompress 压缩后 LLM 丢失了关键执行状态信息，导致反复调用相同工具获取相同结果（重复 list_file、重复 curl、重复 skill_search）。

根因：摘要 prompt 说"省略工具调用的原始输出和中间过程细节"，导致摘要中丢失了工具调用的成功/失败状态和关键返回值。LLM 不知道之前 search_file 失败过、curl 成功过，于是反复尝试相同策略。

## What Changes

- **摘要 prompt 优化**：将"省略中间过程细节"改为"保留工具调用的成功/失败状态和关键返回值"，确保摘要中包含执行决策所需的关键信息。
- **结构化执行状态摘要**：在 Compress 输出中追加一个 `[执行状态]` system message，从被压缩的 segments 中纯代码提取每次工具调用的名称、参数和结果状态。不依赖 LLM 生成，100% 保证关键执行信息不丢失。

## Capabilities

### New Capabilities

- `execution-state-summary`: 从被压缩的 task segments 中提取结构化工具调用记录，追加到压缩结果中

### Modified Capabilities

- 无（SmartCompressor.Compress 内部追加执行状态 message，不改接口）

## Impact

**代码变更范围**：
- `agent/smart_compress.go` — 修改 `generateSummary` 的 prompt + 新增 `extractExecutionState` 函数 + 在 `Compress` 中追加执行状态 message

**不涉及**：
- SmartCompressor 接口和调用方式不变
- Compartor 逻辑不变
- ContextManager 不变
- MemoryPlugin/SummaryPlugin 不变
