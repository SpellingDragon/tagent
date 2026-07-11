## Context

原型设计中 `inputs []string` 是事件流的投影，消息内容是原始事件数据，不可变。`Compact` 直接清空投影：`inputs = inputs[:0]`。

生产实现中，`InjectEventKeys` (Callback 0) 修改了 `args.Request.Messages` 的 Content 字段——在每条非 system/tool 消息前追加 `[evt_KEY|type]` 前缀。这违反了消息不可变性原则。虽然修改的是发给 LLM 的请求（不是 session.Events），但 LLM 在输出中模仿了前缀格式，导致后续消息内容包含重复前缀。

`protectPendingAsyncSegments` 基于 `{status:running}` 检测"未完成异步工具结果"。但 ActionTool 的 `TmuxExecResponse` 初始返回永远是 `status:running`——真正的完成通知通过 `InjectMessage` 异步到达。因此几乎所有含 tool result 的段都被"保护"，SmartCompressor 无法丢弃任何旧段，token 无法降低。

## Goals / Non-Goals

**Goals:**
- InjectEventKeys 幂等：已有前缀的消息不重复注入
- 删除 protectPendingAsyncSegments：恢复 SmartCompressor 正常丢弃旧段的能力
- old_segments=0 时 SmartCompressor 直接返回原始消息，不添加空 compress 消息
- 修正 wechat-bot 示例配置中的不合理值

**Non-Goals:**
- 不改变 InjectEventKeys 的位置匹配机制（refIdx 递增匹配 messages）
- 不改变框架 ContentRequestProcessor 的消息构建逻辑
- 不改变 ActionTool 的异步执行模型
- 不改变 SmartCompressor 的压缩策略（仍然是按任务边界丢弃旧段）

## Decisions

### Decision 1: InjectEventKeys 幂等检查

**选择**: 在 `injectEventKeyPrefixes` 中，对每条消息检查 Content 是否已以 `[evt_` 开头，是则跳过。

**理由**: LLM 在输出中会模仿它看到的上下文格式。如果 LLM 输出 `[evt_1234|external_input] 我来帮你`，这条消息被框架存入 session.Events，下一轮被 ContentRequestProcessor 读取。InjectEventKeys 再次注入前缀 → 双重前缀。幂等检查确保每条消息最多被注入一次前缀。

**实现**:
```go
if strings.HasPrefix(msg.Content, "[evt_") {
    continue  // 已有前缀，跳过
}
```

### Decision 2: 删除 protectPendingAsyncSegments

**选择**: 从 `Compress` 方法中删除 `protectPendingAsyncSegments` 调用和相关函数。

**理由**:
- `hasPendingAsyncResult` 检查 `{status:running}`，但 ActionTool 的 `TmuxExecResponse` 永远返回此值
- 真正的完成通知通过 `InjectMessage(RoleSystem)` 异步到达，不改变已有的 `{status:running}` 消息
- 保护逻辑导致 SmartCompressor 无法丢弃旧段 → token 无法降低 → ReAct 循环失控
- 原型的 `Compact` 无条件清空投影，没有"保护"逻辑

### Decision 3: old_segments=0 时提前返回

**选择**: 在 Step 4a（原 protect 调用位置）之后，如果 `len(oldSegments) == 0`，直接返回原始 messages。

**理由**: 当 oldSegments 为空时，SmartCompressor 无事可做。继续执行会生成 `[context_compress] 压缩了 0 个对话片段` 消息，不仅浪费 token，还让 LLM 困惑。

### Decision 4: 配置修正

**选择**: wechat-bot tagent.yaml 中：
- `max_tool_iterations: 200 → 50`（主 agent）
- `keep_recent_tasks: 8 → 2`

**理由**: `keep_recent_tasks: 8` 导致 SmartCompressor 在段数 < 8 时几乎无法丢弃任何段。`max_tool_iterations: 200` 允许 ReAct 循环持续过久（日志中看到 ~90 次迭代）。修正为默认值。

## Risks / Trade-offs

- [删除 protect 后异步结果可能被丢弃] → ActionTool 的 `{status:running}` 会被 SmartCompressor 丢弃 → 缓解：真正的异步结果通过 InjectMessage 到达 EventBus，进入下一轮 runEventLoop，不依赖旧段中的 `{status:running}` 文本
- [幂等检查可能误判] → 如果用户消息本身以 `[evt_` 开头 → 缓解：这是极罕见的边界情况，且不影响功能（只是该消息不被注入前缀）
- [配置修正影响现有行为] → max_tool_iterations 从 200 降到 50 → 缓解：50 是系统默认值，对于复杂任务用户可显式配置更高值
