## 任务清单

本变更分为 5 个阶段，按依赖顺序执行。每阶段独立可验证。

**依赖关系**：
```
阶段 1 (metadata 传播 API) 
  ↓
阶段 2 (ActionTool 纯异步 + tmux 稳定态过滤) 
  ↓
阶段 3 (tagent 层事件抑制)
  ↓
阶段 4 (wechat-bot 消费者重构)
  ↓
阶段 5 (集成测试 + 文档更新)
```

---

## 阶段 1: 事件元数据传播 API

> 目标: 让 InjectMessageWithMetadata 携带的 metadata 通过因果链传播到 event.StateDelta

### Task 1.1: TagentAgent 新增 InjectMessageWithMetadata

- [x] 在 `agent/tagent_agent.go` 中新增方法:
  ```go
  func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string) {
      if ta.meditationMgr != nil {
          ta.meditationMgr.UpdateLastEventTime(time.Now())
      }
      evt := NewExternalInputEvent(source, msg)
      // 将 metadata 复制到 AgentEvent.Metadata
      if evt.Metadata == nil {
          evt.Metadata = make(map[string]any)
      }
      for k, v := range metadata {
          if k == "" || v == "" { continue }
          evt.Metadata[k] = v
      }
      if ta.persistentBus != nil {
          ta.persistentBus.Publish(evt)
          return
      }
      // ... fallback 逻辑同 InjectMessageWithSource
  }
  ```
- [x] 保留 `InjectMessage` 和 `InjectMessageWithSource` 向后兼容
- [x] 添加单元测试: 验证 metadata 正确写入 AgentEvent.Metadata

### Task 1.2: runEventLoop 提取根 metadata

- [x] 在 `agent/tagent_agent.go` 的 `runEventLoop` 中:
  - Pull 后遍历 batch，提取 `external_input` 且非 agent_output/error source 的事件的 Metadata
  - 转换为 `map[string]string`（AgentEvent.Metadata 是 `map[string]any`）
  - 通过新增的 `cm.SetInvocationMetadata(md)` 传递给 ContextManager
- [x] 实现:
  ```go
  func extractRootMetadata(events []*AgentEvent) map[string]string {
      md := make(map[string]string)
      for _, evt := range events {
          if evt == nil || evt.Type != tagentevent.TypeExternalInput { continue }
          if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" { continue }
          for k, v := range evt.Metadata {
              if s, ok := v.(string); ok && s != "" {
                  md[k] = s
              }
          }
      }
      return md
  }
  ```

### Task 1.3: ContextManager 维护 currentMetadata

- [x] 在 `agent/context_manager.go` 的 `ContextManager` 结构体中新增:
  ```go
  currentMetadata map[string]string  // 当前 RunFlow 的 metadata
  metadataMu      sync.RWMutex
  ```
- [x] 新增方法:
  ```go
  func (cm *ContextManager) SetInvocationMetadata(md map[string]string) {
      cm.metadataMu.Lock()
      defer cm.metadataMu.Unlock()
      cm.currentMetadata = md
  }
  
  func (cm *ContextManager) GetInvocationMetadata() map[string]string {
      cm.metadataMu.RLock()
      defer cm.metadataMu.RUnlock()
      out := make(map[string]string, len(cm.currentMetadata))
      for k, v := range cm.currentMetadata {
          out[k] = v
      }
      return out
  }
  ```
- [x] 在 `RunFlow` 开始时不重置 currentMetadata（runEventLoop 在调 RunFlow 前已 SetInvocationMetadata）
- [x] 在 RunFlow 的 onEvent 回调中，将 metadata 写入 event.StateDelta

### Task 1.4: onEvent 回调写入 StateDelta

- [x] 在 `agent/tagent_agent.go` 的 `makeOnEventCallback` 中:
  ```go
  return func(evt *event.Event) {
      if evt == nil { return }
      if evt.StateDelta == nil {
          evt.StateDelta = make(map[string][]byte)
      }
      // 从 ContextManager 读取当前 metadata 并写入 StateDelta
      md := ta.contextManager.GetInvocationMetadata()
      for k, v := range md {
          key := k
          if !strings.HasPrefix(key, "meta_") {
              key = "meta_" + key
          }
          evt.StateDelta[key] = []byte(v)
      }
      // 原有逻辑（BuildEventReference + projection.Append）
      // ...
  }
  ```

### Task 1.5: 验证阶段 1

- [x] `go build ./...` 通过
- [x] 新增测试 `agent/metadata_propagation_test.go`:
  - TestInjectMessageWithMetadata: metadata 正确写入 AgentEvent.Metadata
  - TestInjectMessageWithMetadata_EmptyValues: 空值被忽略
  - TestExtractRootMetadata (5 子测试): 提取/过滤/覆盖逻辑
  - TestOnEventCallback_PropagatesMetadata: metadata 传播到 StateDelta["meta_*"]
  - TestOnEventCallback_NoMetadata: 无 metadata 时不添加 meta_ 键
  - TestOnEventCallback_AlreadyPrefixedMetadata: 已有 meta_ 前缀时不重复添加
- [x] `go test ./agent/ -run "TestInjectMetadata|TestExtractRootMetadata|TestOnEventCallback" -v` 全部 8 个测试通过
- [x] `go test ./... -short -count=1` 全部通过

---

## 阶段 2: ActionTool 纯异步 + tmux 稳定态过滤

> 目标: 删除 async 参数、删除 sync 分支、tmux 只在稳定态触发

### Task 2.1: 删除 ActionArgs.Async 字段

- [ ] 在 `tool/action/action_tool.go` 中:
  - 从 `ActionArgs` 结构体删除 `Async bool` 字段
  - 从 `Declaration()` 的 InputSchema.Properties 中删除 "async" 键
  - 修改 `description` 字段: 移除所有 async 相关说明
- [ ] `Call()` 方法简化:
  ```go
  func (ct *ActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
      var args ActionArgs
      if err := json.Unmarshal(jsonArgs, &args); err != nil {
          return nil, fmt.Errorf("action: parse args: %w", err)
      }
      if args.Command == "" {
          return nil, fmt.Errorf("action: command is required")
      }
      if ct.tmuxExecutor == nil {
          return nil, fmt.Errorf("action: tmux not available (install: brew install tmux)")
      }
      return ct.executeAsync(ctx, args)
  }
  ```
- [ ] 删除 `executeSync` 方法（如仍存在）
- [ ] 删除 sync 相关的日志和错误处理

### Task 2.2: TmuxAsyncPlaceholderStatus 常量

- [ ] 在 `tool/action/action_tool.go` 或独立文件中定义:
  ```go
  // TmuxAsyncPlaceholderStatus is the well-known status returned by Call()
  // when a tmux session is started. tagent framework identifies this pattern
  // and suppresses the event from outputCh / Projection.
  const TmuxAsyncPlaceholderStatus = "waiting_async_response"
  ```
- [ ] `executeAsync` 中使用此常量:
  ```go
  return &TmuxExecResponse{
      SessionID: session.ID,
      Status:    TmuxAsyncPlaceholderStatus,
  }, nil
  ```

### Task 2.3: TmuxMonitor 状态过滤

- [ ] 在 `tool/action/tmux_monitor.go` 中新增:
  ```go
  func isStableState(s SessionStatus) bool {
      switch s {
      case SessionStable, SessionCompleted, SessionError, SessionTimedOut:
          return true
      default:
          return false
      }
  }
  ```
- [ ] 修改 `checkSession` 中触发回调的条件（原代码 L440-450 附近）:
  ```go
  if newStatus != oldStatus && isStableState(newStatus) && tm.stateChangeCallback != nil {
      tm.stateChangeCallback(sessionID, oldStatus, newStatus, output)
  }
  ```
- [ ] 移除或注释掉 Running/FakeDead/FakeAlive → Running 之间的回调触发

### Task 2.4: 去重同稳定态的重复触发

- [ ] 在 `TmuxMonitor` 中新增字段 `lastNotifiedStatus map[string]SessionStatus`
- [ ] `checkSession` 中检查:
  ```go
  if last, ok := tm.lastNotifiedStatus[sessionID]; ok && last == newStatus {
      return  // 同状态不重复通知
  }
  tm.lastNotifiedStatus[sessionID] = newStatus
  // 触发回调...
  ```

### Task 2.5: 更新 action_tool_desc.md

- [ ] 修改 `resources/prompts/action_tool_desc.md`:
  - 移除所有 "async=true" / "sync mode" 相关说明
  - 强调"所有命令异步执行，结果通过 [action_tool_result] 事件到达"
  - 强调"不要连续调用相同命令等待结果"

### Task 2.6: 更新 ActionTool 测试

- [ ] 修改 `tool/action/action_test.go`:
  - 所有测试中的 `"async": true` 移除（已是默认行为）
  - 所有测试中的 `"async": false` 改为不传该字段
  - 增加断言: `Call()` 返回值的 Status == TmuxAsyncPlaceholderStatus
- [ ] 修改 `tool/action/tmux_complex_test.go`:
  - 移除 `"async": true` 参数
- [ ] 新增 `tool/action/tmux_state_filter_test.go`:
  - 测试 Running → Stable 触发回调
  - 测试 Running → Running (输出变化) 不触发
  - 测试 Stable → Stable 不重复触发
  - 测试 Stable → Completed 触发

### Task 2.7: 验证阶段 2

- [ ] `go build ./...` 通过
- [ ] `go test ./tool/action/ -count=1 -timeout 120s` 全部通过
- [ ] 手动验证: 执行 `ls /tmp` 类快速命令，回调在 Completed 状态触发

---

## 阶段 3: tagent 层事件抑制

> 目标: RunFlow 中识别 tmux async 占位符事件并抑制传播

### Task 3.1: 实现 isTmuxAsyncPlaceholder 函数

- [ ] 在 `agent/context_manager.go` 或独立文件中:
  ```go
  const tmuxAsyncPlaceholder = "waiting_async_response"
  
  func isTmuxAsyncPlaceholder(evt *event.Event) bool {
      if evt == nil || evt.Response == nil { return false }
      for _, choice := range evt.Response.Choices {
          if choice.Message.Role != model.RoleTool { continue }
          if strings.Contains(choice.Message.Content, tmuxAsyncPlaceholder) {
              return true
          }
      }
      return false
  }
  ```

### Task 3.2: RunFlow 中过滤占位符事件

- [ ] 在 `agent/context_manager.go` 的 `RunFlow` 方法的 `for fwEvt := range eventCh` 循环中:
  ```go
  for fwEvt := range eventCh {
      // 抑制 tmux async 占位符事件
      if isTmuxAsyncPlaceholder(fwEvt) {
          log.Debugf("[RunFlow] suppressing tmux async placeholder for session=%s", 
                    extractSessionID(fwEvt))
          continue  // 跳过全部处理: 不 onEvent、不 outputCh、不 bus.Publish
      }
      
      // 原有逻辑
      if cm.onEvent != nil && fwEvt != nil {
          cm.onEvent(fwEvt)
      }
      // ...
  }
  ```
- [ ] 注意: `continue` 跳过意味着此事件完全不出现在 Projection 和 outputCh 中

### Task 3.3: 验证阶段 3

- [ ] `go build ./...` 通过
- [ ] 新增测试 `agent/tmux_placeholder_test.go`:
  - Mock model 返回 `TmuxExecResponse` 作为 tool result
  - 验证 outputCh 不收到此事件
  - 验证 Projection 不包含此事件的 ref
  - 验证 LLM 不会因此事件被再次触发（无"我已启动"响应）
- [ ] `go test ./agent/ -run "TestTmuxPlaceholder" -v` 通过

---

## 阶段 4: wechat-bot 消费者重构

> 目标: 删除 responseCh 反模式，Handler 立即返回，Consumer 单一决策

### Task 4.1: Handler 简化

- [x] 修改 `examples/wechat-bot/main.go` 的 `bot.OnMessage` 回调:
  - 删除 `responseCh` 等待逻辑
  - 删除 `replyTarget.Store()` 和 `lastUser.Store()`
  - Handler 立即返回，不等待响应
  - 使用 `InjectMessageWithMetadata("user", msg, {"chat_id": fromUserID, "user_name": fromUserID})`
  - 启动 typing indicator 并记录到 `typingActive` map（以 chat_id 为 key）
  bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
      text := msg.Text()
      if text == "" { return nil }
      
      // 启动 typing indicator (非阻塞)
      _ = bot.StartTyping(ctx, msg.FromUserID)
      typingActive.Store(msg.FromUserID, time.Now())
      
      // 投递事件到 tagent (立即返回)
      ta.InjectMessageWithMetadata("user", model.Message{
          Role:    model.RoleUser,
          Content: text,
      }, map[string]string{
          "chat_id":   msg.FromUserID,
          "user_name": msg.FromUserName,
      })
      
      return nil  // Handler 立即返回, 不等待响应
  })
  ```

### Task 4.2: 删除 responseCh / replyTarget / lastUser

- [x] 删除 `responseCh := make(chan string, 1)` 声明
- [x] 删除 `replyTarget atomic.Pointer[string]`
- [x] 删除 `lastUser atomic.Pointer[string]`
- [x] 删除 Handler 中 `<-responseCh` 等待逻辑
- [x] 删除 Consumer 中 `case responseCh <- content:` 分支

### Task 4.3: Consumer 重写为单一决策点

- [x] 修改 `outputCh` 消费者 goroutine:
  - 提取 `triggerSource` 和 `chatID`/`userName` 从 `StateDelta`
  - 单一决策点: `switch triggerSource` 路由到 meditation/error/user/async_result
  - meditation/error 类型只记录日志，不发送给用户
  - user/async_result 类型从 `meta_chat_id` 提取目标用户并发送
  - 长文本（>2000 字符）使用 `SendLongText` 或截断后发送

### Task 4.4: typing indicator 超时保护

- [x] `typingActive` 改为 `sync.Map` (chat_id → time.Time)
- [x] Consumer 发送响应前检查并停止 typing indicator
- [x] 60 秒超时保护（Consumer 中实现）

### Task 4.5: 验证阶段 4

- [x] `cd examples/wechat-bot && go build .` 编译通过
- [x] `go test ./... -short -count=1` 全部通过
- [ ] 手动测试（待执行）:
  - 单用户: 发消息 → 收到响应
  - 单用户: 触发 async 命令 → 命令结果到达时收到响应
  - 冥想触发的输出不发送给用户（只记日志）

---

## 阶段 5: 集成测试 + 文档更新

> 目标: 端到端验证 + 文档同步

### Task 5.1: 多用户并发消息测试

- [x] 新增 `tests/multi_user_dispatch_test.go`:
  - 模拟两个用户 A 和 B 几乎同时发消息（间隔 100ms）
  - 验证 A 的响应发到 chat_id=A，B 的响应发到 chat_id=B
  - 无串线

### Task 5.2: async 结果路由测试

- [x] 新增 `tests/async_result_routing_test.go`:
  - 用户 A 触发 async 命令
  - Mock tmux 完成 → 注入 action_tool_result 事件
  - 验证 async_result 触发的 agent_output 携带 meta_chat_id=A

### Task 5.3: 更新 README 和 wiki

- [x] `README.md`: 更新 "消费模式" 章节，删除 responseCh 的描述，改为单一决策点模式
- [x] `docs/wiki/agent/agent-architecture.md`: 无需更新（无 responseCh 引用）
- [x] `examples/wechat-bot/README.md` (不存在，跳过)

### Task 5.4: 最终验证

- [x] `go build ./...` 无错误
- [x] `go vet ./...` 无警告
- [x] `go test ./... -short -count=1 -timeout 180s` 全部通过
- [x] `cd examples/wechat-bot && go build .` 通过
- [x] `tests/invariants_test.go` 通过（三个不变量保持）
- [ ] 手动运行 wechat-bot 至少 5 轮对话（含异步命令），验证（待人工执行）:
  - 所有 agent_output 正确发送
  - 冥想输出不打扰用户
  - 命令启动时无冗余"我已启动"响应
  - tmux 中间态不产生事件

---

## 迁移说明（BREAKING）

对使用 tagent 的下游应用:

1. **API 变化**:
   - `ta.InjectMessage(msg)` 保留（不带 metadata）
   - `ta.InjectMessageWithSource(source, msg)` 保留
   - `ta.InjectMessageWithMetadata(source, msg, metadata)` 新增（推荐）
   - Handler 应尽可能使用 metadata 版本传递路由信息

2. **配置变化**:
   - `ActionArgs.Async` 字段已删除。工具调用中传 `async` 字段会被忽略（不报错，向前兼容）
   - action_tool_desc.md 需要更新以移除 async 说明

3. **部署要求**:
   - **必须安装 tmux**（否则 action 工具无法工作）
   - macOS: `brew install tmux`
   - Ubuntu: `apt install tmux`
