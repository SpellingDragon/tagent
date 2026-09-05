## 能力: unified-output-consumer

wechat-bot 采用单一消费者架构：Handler 只做事件投递，Consumer 从事件 metadata 提取路由信息主动发送响应。

## 需求

### Handler 简化

Handler（`bot.OnMessage`）职责：
1. 记录接收日志
2. 校验消息合法性
3. 启动 typing indicator（非阻塞）
4. 调用 `ta.InjectMessageWithMetadata("user", msg, {"chat_id": FromUserID, "user_name": UserName})`
5. **立即返回 nil**（不等待响应）

**禁止行为**：
- 不阻塞等待任何 channel
- 不通过全局变量记录"当前用户"（`replyTarget` / `lastUser` 删除）
- 不直接调用 `bot.SendTextResponse` 发送响应（响应由 Consumer 负责）

### Consumer 单一决策

Consumer（消费 `outputCh` 的 goroutine）：

```go
for evt := range outputCh {
    // 只处理 final response（agent_output）
    if !evt.IsFinalResponse() {
        // 非终响应仅记日志（thinking_plan / action_command 等）
        logInterimEvent(evt)
        continue
    }
    
    content := evt.Response.Choices[0].Message.Content
    triggerSource := getStateDelta(evt, "trigger_source")
    chatID := getStateDelta(evt, "meta_chat_id")
    
    switch triggerSource {
    case "meditation":
        log.Infof("[Consumer] meditation output (silent): %s", truncate(content))
        // 静默：不发送
        
    case "error":
        log.Warnf("[Consumer] error trigger, silent: %s", truncate(content))
        // 静默：错误不打扰用户
        
    default:  // "user", "async_result", 或其他
        if chatID == "" {
            log.Warnf("[Consumer] no meta_chat_id, dropping response: %s", truncate(content))
            continue
        }
        stopTyping(chatID)
        err := bot.SendTextToUser(ctx, chatID, content)
        if err != nil {
            log.Errorf("[Consumer] SendTextToUser failed: %v", err)
        }
    }
}
```

### Typing Indicator 时序

- Handler 收到消息 → 立即 `bot.StartTyping(chatID)` + 记录到 `typingActive map[string]time.Time`
- Consumer 发送响应前 → `bot.StopTyping(chatID)` + 从 map 中删除
- 超时保护：定时 goroutine 检查 `typingActive` 中超过 60s 的条目，自动 StopTyping（防止 typing 无限持续）

### 删除清单

以下代码/字段必须删除：
- `responseCh chan string`
- `replyTarget atomic.Pointer[string]`
- `lastUser atomic.Pointer[string]`
- Handler 中的 `<-responseCh` 等待逻辑
- Consumer 中的 `responseCh <- content` 分发路径

### 保留清单

- `outputCh` — tagent 的事件管线出口
- `typingActive` — 简化为按 chat_id 的 map（不再是全局 atomic bool）
- 事件日志（`[Event] ID=...`）保留用于调试

### 约束

- Consumer 是响应发送的唯一路径
- 每个响应必须携带有效 chat_id 才发送
- 未识别的 triggerSource 默认视为需要发送（安全默认）
