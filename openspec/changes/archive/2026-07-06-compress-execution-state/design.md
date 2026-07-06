## Context

SmartCompress 压缩管道由两个阶段组成：
1. **Stage 1**：按 task boundary 丢弃旧 segments
2. **Stage 2**：对丢弃的 segments 调用 LLM 生成摘要

Stage 2 的摘要 prompt 指示"省略工具调用的原始输出和中间过程细节"，导致摘要丢失了工具调用的成功/失败状态。LLM 看不到之前 search_file 失败过、curl 成功过，于是反复尝试相同策略。

约束：
- 只修改 `smart_compress.go`，不改接口
- 不依赖额外 LLM 调用（执行状态是纯代码提取）
- 执行状态 message 不应显著增加 token（控制在 500 chars 以内）

## Goals / Non-Goals

**Goals:**
- 摘要 prompt 保留工具调用的成功/失败状态和关键返回值
- 追加结构化执行状态 message，100% 保证关键执行信息不丢失
- LLM 看到执行状态后不再重复调用失败的工具

**Non-Goals:**
- 不改变 SmartCompressor 接口
- 不改变 task boundary 判定逻辑
- 不改变压缩触发条件

## Decisions

### D1: 摘要 prompt 优化

**决策**：修改 `generateSummary` 的 prompt 第 2 条：

```
当前: "2. 省略工具调用的原始输出和中间过程细节"
改为: "2. 保留工具调用的成功/失败状态和关键返回值（如文件路径、命令输出摘要）"
```

**理由**：工具调用的成功/失败不是"中间过程细节"，是关键决策信息。

### D2: 结构化执行状态摘要 — extractExecutionState

**决策**：新增 `extractExecutionState(segments []*TaskSegment) string` 函数，从被压缩的 segments 中纯代码提取工具调用记录：

```go
func extractExecutionState(segments []*TaskSegment) string {
    var lines []string
    for _, seg := range segments {
        for _, msg := range seg.Messages {
            if msg.Role == model.RoleAssistant && len(msg.ToolCalls) > 0 {
                for _, tc := range msg.ToolCalls {
                    // 提取工具名和参数摘要
                    name := tc.Function.Name
                    args := truncate(string(tc.Function.Arguments), 80)
                    lines = append(lines, fmt.Sprintf("- 调用: %s(%s)", name, args))
                }
            }
            if msg.Role == model.RoleTool && msg.Content != "" {
                // 提取工具结果状态（成功/失败 + 关键返回值）
                result := truncate(msg.Content, 100)
                lines = append(lines, fmt.Sprintf("  → 结果: %s", result))
            }
        }
    }
    if len(lines) == 0 {
        return ""
    }
    return "[执行状态]\n" + strings.Join(lines, "\n")
}
```

输出示例：
```
[执行状态]
- 调用: search_file({"path":"/tmp","pattern":"*wechat*"})
  → 结果: Error: invalid path - absolute paths and '..' are not allowed: /tmp
- 调用: action({"command":"curl -s \"https://...\""})
  → 结果: {"session_id":"tagent-xxx","status":"running"}
- 调用: list_file({"path":"","with_size":true})
  → 结果: [{"name":"skills","size":4096,"is_dir":true},...
```

### D3: 执行状态 message 插入位置

**决策**：在 `Compress` 的结果中，执行状态 message 插入在 LLM 摘要之后、recent segments 之前：

```go
// Compress 结果构建:
result = append(result, systemMsg)          // [0] system prompt
result = append(result, compressEvent)       // [1] [context_compress]
result = append(result, summaryMsgs...)      // [2] [摘要批次]
result = append(result, execStateMsg)       // [3] [执行状态] ← 新增
result = append(result, recentSegMsgs...)    // [4-N] recent segments
result = append(result, userGuidance)       // [N+1] 引导消息
```

**理由**：
- 在 LLM 摘要之后：先看语义摘要，再看具体执行状态
- 在 recent segments 之前：作为历史上下文的一部分
- 控制在 500 chars 以内：工具结果截断为 100 chars，避免显著增加 token

### D4: 执行状态截断策略

**决策**：每条工具结果截断为 100 chars，总执行状态控制在 500 chars 以内。如果超过 500 chars，只保留最后 N 条（最近的执行状态更重要）。

```go
const maxExecStateChars = 500
const maxToolResultChars = 100

// 如果总长度超过限制，从头部截断（保留最近的）
if len(result) > maxExecStateChars {
    result = result[len(result)-maxExecStateChars:]
}
```

## Risks / Trade-offs

- **[执行状态增加 token]** → 控制在 500 chars 以内（约 250 tokens），相对 max_tokens=16000 可忽略。
- **[LLM 摘要与执行状态重复]** → 可能重复，但 LLM 摘要是语义层面的，执行状态是事实层面的，互补而非冗余。
- **[截断可能丢失关键信息]** → 每条工具结果截断为 100 chars，可能丢失长输出的关键部分。但完整的工具结果在 MemoryStore 中，LLM 可通过 recall 获取。
