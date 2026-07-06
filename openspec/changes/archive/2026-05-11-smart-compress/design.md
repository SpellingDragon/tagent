## Context

### 当前状态

`SmartCompressor.Compress()` 在 `ContextIntervention.BeforeModel` 的 Phase 2 被调用。此时消息已被 Phase 1 的 `applyEventView` 添加了 `[evt_<SnowflakeKey>|<eventType>]` 前缀。

压缩流程：
1. `splitByTaskBoundary` 按 assistant(无 tool_calls) 边界分段
2. 保留最近 N 段，旧段进入 `oldSegments`
3. `collectCompressedKeys` 从旧段中提取 event_key → **当前已损坏**
4. `buildCompressEvent` 将 key 列表写入 `context_compress` 系统消息

### Bug 根因

`collectCompressedKeys` (L144-194) 的匹配逻辑：
```
旧段消息(已带前缀 [evt_123|task]) → content:role 指纹 → 匹配 Session.Events(无前缀) → ❌ 永远不命中
```

因为 `applyEventView` 修改的是 `args.Request.Messages`（LLM 视图），Session.Events 保持原始格式。指纹必然不同。

### 为什么不能删除 SmartCompressor

| 阶段 | 功能 | 可删? | 理由 |
|------|------|--------|------|
| Stage 1 段丢弃 | 按 task 边界裁剪上下文，控制 token 预算 | **不可删** | 唯一 token 预算执行机制，删除后长对话必然超限 |
| Stage 2 LLM 摘要 | 生成被压缩段落的摘要 | 可降级 | 已支持 `summaryModel=nil` 时跳过 |
| `collectCompressedKeys` | 收集压缩段的 event_key 列表 | 需修复 | 当前损坏，修复后 LLM 可通过 key 回溯历史 |

## Goals / Non-Goals

**Goals:**
- 修复 `collectCompressedKeys`，使压缩段 event_key 列表正确生成
- 清理 `buildEventMessageIndex`（仅此处使用，不再需要）
- 确认 SmartCompressor 不可删除，保留全部阶段

**Non-Goals:**
- 不修改事件视图前缀格式 `[evt_<KEY>|<type>]`
- 不修改压缩触发阈值
- 不修改 Stage 2 摘要逻辑
- 不引入新依赖

## Decisions

### Decision 1: 前缀解析 vs 修复指纹匹配

| 方案 | 描述 | 评估 |
|------|------|------|
| A: 修复指纹 | 在匹配前去前缀再生成指纹 | 复杂度高，需额外处理前缀剥离 |
| B: 前缀解析 ✅ | 从 `[evt_<KEY>\|` 直接解析 int64 key | 精确、高效、与事件视图格式天然耦合 |
| C: 删除该方法 | 不列出 event_key，LLM 无法回溯 | 丧失核心能力，不可接受 |

**选择 B**：前缀解析与 Phase 1 事件视图格式强耦合，但两者同属 `agent` 包，单一变更点。解析逻辑仅 ~15 行。

### Decision 2: 解析实现细节

```go
// 从消息 content 前缀提取 event_key
// 格式: "[evt_123456789|task] 原始内容..."
func parseEventKeyFromPrefix(content string) int64 {
    const prefix = "[evt_"
    if !strings.HasPrefix(content, prefix) {
        return 0
    }
    barPos := strings.IndexByte(content[5:], '|')
    if barPos < 0 {
        return 0
    }
    keyStr := content[5 : 5+barPos]
    key, err := strconv.ParseInt(keyStr, 10, 64)
    if err != nil {
        return 0
    }
    return key
}
```

无 regex，无分配开销，O(n) 字符扫描。

### Decision 3: `buildEventMessageIndex` 清理

该方法（[context_intervention.go:220-232](file:///Users/pengweiye/Documents/codes/tagent/agent/context_intervention.go#L220-L232)）仅在 `applyEventView` 中使用，而 `applyEventView` 仅用于事件视图前缀设置。压缩事件 key 收集切换到前缀解析后，不再需要任何基于 Session.Events 的索引。清理范围：
- 删除 `buildEventMessageIndex` 函数
- 删除 `applyEventView` 中对 `evtMsgIndex` 的调用
- `applyEventView` 重建为更简单的逐消息匹配逻辑

等待——`applyEventView` 本身**仍在使用**（Phase 1 事件视图转换），它使用 `buildEventMessageIndex` 来匹配消息与事件。所以不能直接删除。

重新评估：`buildEventMessageIndex` 在 `applyEventView` 中使用。我们修复的是 `collectCompressedKeys`，它使用的是完全不同的匹配逻辑。所以 `buildEventMessageIndex` **不在此次修复范围内**，也不应被删除。

**修正**: 仅修改 `collectCompressedKeys` 方法。`buildEventMessageIndex` 保持不变。

## Risks / Trade-offs

| 风险 | 等级 | 缓解 |
|------|------|------|
| 前缀格式未来变更导致解析失效 | 低 | 前缀格式在 [context_intervention.go:214](file:///Users/pengweiye/Documents/codes/tagent/agent/context_intervention.go#L214) 单点定义；两处共享同一个 agent 包，变更加单元测试覆盖 |
| key 解析失败静默返回 0 | 低 | 返回 0 被 `if key > 0` 过滤，效果等同于当前行为（key 缺失），不会引入新错误 |
| 非事件消息意外匹配前缀 | 极低 | LLM 生成的消息不会包含 `[evt_<数字>\|` 格式，除非引用事件 key（这正是我们想要的行为） |
