# 修复计划：数据流链路接通

> 前置说明：组 1-10 已在第一版实现中标记完成，但存在三个严重偏离。
> 本修复计划聚焦于接通数据流链路，不重复已完成的基础设施工作。
> 每个任务标注了具体文件路径、方法签名、修改位置和验证方式。

## 1. R1: 接通 onEvent 回调 — 持久化链路修复

> 依赖：无（当前代码可独立修改）
> 目标：AgentLoop 产出的所有事件通过 onEvent 回调写入 Session.Events 和 MemoryStore
> 完成标准：集成测试验证 session.Events 非空 + MemoryStore 有 FullEvent + 因果链正确

- [x] 1.1 TagentAgent.NewTagentAgent 设置 onEvent 回调
  - 文件：`agent/tagent_agent.go`
  - 位置：`NewTagentAgent` 函数，创建 `agentLoop` 之后（约 line 232-242）
  - 修改：在 `AgentLoopConfig` 中添加 `OnEvent` 字段
  ```go
  agentLoop := NewAgentLoop(AgentLoopConfig{
      Bus:          bus,
      Preprocessor: preprocessor,
      Model:        cfg.Model,
      Tools:        cfg.Tools,
      OutputCh:     outputCh,
      Name:         name,
      MaxToolIters: cfg.MaxToolIterations,
      SystemPrompt: cfg.SystemPrompt,
      Temperature:  cfg.Temperature,
      OnEvent:      ta.makeOnEventCallback(), // 新增
  })
  ```

- [x] 1.2 新增 `TagentAgent.makeOnEventCallback()` 方法
  - 文件：`agent/tagent_agent.go`
  - 方法签名：`func (ta *TagentAgent) makeOnEventCallback() func(evt *event.Event)`
  - 职责：
    1. 调用 `MemoryPlugin.OnEvent(ctx, inv, evt)` → 写入 MemoryStore + 设置 StateDelta
    2. 调用 `sessionSvc.AppendEvent(ctx, sess, evt)` → 写入 session.Events
  - 注意：MemoryPlugin.OnEvent 需要 `*agent.Invocation`。创建一个轻量 Invocation（仅 AgentName + Session 字段）供 plugin 使用。

- [x] 1.3 TagentAgent.Run 设置 onEvent 回调（sub-agent 路径）
  - 文件：`agent/tagent_agent.go`
  - 位置：`Run` 方法，创建 `invLoop` 之后（约 line 379-389）
  - 修改：在 `AgentLoopConfig` 中添加 `OnEvent: ta.makeSubAgentOnEventCallback()`
  - sub-agent 使用独立的 session，不共享父 agent 的 session

- [x] 1.4 新增 `TagentAgent.makeSubAgentOnEventCallback()` 方法
  - 文件：`agent/tagent_agent.go`
  - 方法签名：`func (ta *TagentAgent) makeSubAgentOnEventCallback() func(evt *event.Event)`
  - 职责：为 sub-agent 创建/获取独立 session，调用 MemoryPlugin.OnEvent + sessionSvc.AppendEvent

- [x] 1.5 AgentLoop.Run 在调用 Preprocessor 前调用 onEvent
  - 文件：`agent/agent_loop.go`
  - 位置：`Run` 方法，`preprocessor.Process` 调用之前（约 line 144）
  - 修改：遍历 batch 中的 external_input 事件，包装为 framework event，调用 onEvent
  ```go
  for _, evt := range events {
      if evt.Type == tagentevent.TypeExternalInput && evt.Message != nil {
          frameworkEvt := al.wrapAsFrameworkEvent(evt)
          if al.onEvent != nil {
              al.onEvent(frameworkEvt)
          }
      }
  }
  result := al.preprocessor.Process(ctx, events, al.session)
  ```

- [x] 1.6 新增 `AgentLoop.wrapAsFrameworkEvent()` 方法
  - 文件：`agent/agent_loop.go`
  - 方法签名：`func (al *AgentLoop) wrapAsFrameworkEvent(evt *AgentEvent) *event.Event`
  - 将 `AgentEvent` 转换为 `event.Event`，设置 `Response.Choices[0].Message`

- [x] 1.7 AgentLoop.handleResponse 对 model response 调用 onEvent
  - 文件：`agent/agent_loop.go`
  - 位置：`handleResponse` 方法（约 line 249-291）
  - 修改：在 emit agent_output 和 tool_call response 之前，调用 onEvent
  - tool_calls response 和 final response 都需要写入 session + MemoryStore

- [x] 1.8 StartLoop 创建/获取 session 并设置到 agentLoop
  - 文件：`agent/tagent_agent.go`
  - 位置：`StartLoop` 方法（约 line 603-644）
  - 修改：启动 agentLoop goroutine 之前，调用 `ta.getOrCreateSession()` 并 `ta.agentLoop.SetSession(sess)`

- [x] 1.9 新增 `TagentAgent.getOrCreateSession()` 方法
  - 文件：`agent/tagent_agent.go`
  - 方法签名：`func (ta *TagentAgent) getOrCreateSession() *session.Session`
  - 使用 `lastUserID` 和 `lastSessionID` 作为 key，通过 `sessionSvc.GetSession/CreateSession` 获取或创建 session

- [x] 1.10 新增 `agent/on_event_integration_test.go` 验证 onEvent 链路
  - 测试用例：
    - `TestOnEvent_SessionEventsPopulated`：验证 session.Events 有 user + assistant 事件
    - `TestOnEvent_MemoryStorePopulated`：验证 MemoryStore 有 FullEvent + 因果链
    - `TestOnEvent_ToolCallChain`：验证工具调用链有 4 个事件，因果链正确

## 2. R2: Preprocessor 从 Session.Events 构建完整 LLM Context

> 依赖：R1（onEvent 接通后 session.Events 才有数据）
> 目标：Preprocessor 从 session.Events 构建完整 messages，而非只处理新 batch
> 完成标准：token 超限时压缩作用于完整历史（含旧消息），与原 ContextIntervention 行为一致

- [x] 2.1 修改 `Preprocessor.Process` 签名和实现
  - 文件：`agent/preprocessor.go`
  - 当前签名：`func (p *Preprocessor) Process(ctx context.Context, events []*AgentEvent) ProcessResult`
  - 新签名：`func (p *Preprocessor) Process(ctx context.Context, batch []*AgentEvent, sess *session.Session) ProcessResult`
  - shouldCallModel 从 batch 判断（是否有 external_input）
  - messages 从 `sess.Events` 构建（完整历史）
  - event_key 前缀注入从 session.Events 读取
  - token 预算检查和 SmartCompress 作用于完整 messages

- [x] 2.2 删除 `AgentLoop.history` 字段和相关逻辑
  - 文件：`agent/agent_loop.go`
  - 删除：`history []model.Message` 字段
  - 删除：`trimHistory` 函数和 `maxHistoryMessages` 常量
  - 删除：Run 方法中所有 `al.history` 引用（append/reset/trim）
  - 修改后：`resp, err := al.callModel(ctx, result.Messages)`，无需再拼接 history

- [x] 2.3 更新 `AgentLoop.Run` 中 Preprocessor.Process 调用
  - 文件：`agent/agent_loop.go`
  - 位置：`Run` 方法（约 line 144）
  - 修改：`result := al.preprocessor.Process(ctx, events, al.session)`

- [x] 2.4 更新 `agent/preprocessor_test.go` 适配新签名
  - 所有测试用例改为构造 mock `session.Session` 并传入
  - 新增 `TestPreprocessor_BuildsFromSession`：验证从 session.Events 构建完整 messages
  - 新增 `TestPreprocessor_CompressOnFullHistory`：验证压缩作用于完整历史

- [x] 2.5 更新 `agent/agent_loop_test.go` 和 `agent/tool_dispatch_test.go`
  - mock Preprocessor 需要返回完整 messages（从 mock session 构建）
  - 所有调用 `preprocessor.Process` 的地方适配新签名

## 3. R3: 清理遗留问题和偏离

> 依赖：R1 + R2
> 目标：清除第一版实现中的残留代码和偏离

- [x] 3.1 删除 `agent/context_intervention.go`
  - 文件：`agent/context_intervention.go` — 整个文件删除
  - 前置检查：`grep -r "ContextIntervention\|BeforeModel\|injectEventKeyPrefixes[^F]" --include="*.go"` 确认无调用点

- [x] 3.2 处理 `agent/poc_test.go`
  - 检查是否依赖 BeforeModel callback（已删除的 LLMAgent 机制）
  - 如果依赖，标记为 `//go:build poc` 或删除

- [x] 3.3 修复 `dispatchSubAgent` 结果格式
  - 文件：`agent/agent_loop.go`
  - 位置：`dispatchSubAgent` 方法（约 line 450）
  - 修改：将 `%v` 格式改为 JSON 序列化
  ```go
  if b, err := json.Marshal(result); err == nil {
      content = string(b)
  } else {
      content = fmt.Sprintf("[agent %s] %v", name, result)
  }
  ```

- [x] 3.4 为 `dispatchSubAgent` 添加超时保护
  - 文件：`agent/agent_loop.go`
  - 位置：`dispatchSubAgent` 方法（约 line 437）
  - 修改：使用 `context.WithTimeout(ctx, 10*time.Minute)`

- [x] 3.5 删除 `ActionArgs.Mode` 字段
  - 文件：`tool/action/action_tool.go`
  - 位置：`ActionArgs` 结构体（约 line 341-348）
  - 修改：删除 `Mode string` 字段

- [x] 3.6 更新 `agent/tagent_agent.go` 文件头注释
  - 文件：`agent/tagent_agent.go`
  - 位置：文件头部注释（line 1-16）
  - 修改：更新为事件驱动架构描述，移除 LLMAgent/ContextIntervention 引用

- [x] 3.7 处理 `identityOnlyAgent`
  - 文件：`agent/tagent_agent.go`
  - 位置：`identityOnlyAgent` 结构体（约 line 277-290）
  - 检查 runner 是否仍需要 agent.Agent 参数
  - 如果 runner 仅用于 Info()，保留并更新注释；否则删除

- [x] 3.8 验证 tool_use 发布到 bus 的噪音
  - 文件：`agent/agent_loop.go`
  - 位置：`handleResponse` 方法（约 line 269-274）
  - 保留 publish 到 bus（用于触发后续 dispatch），确保 Preprocessor 正确跳过 tool_use 事件

## 4. R4: 集成测试验证

> 依赖：R1 + R2 + R3
> 目标：端到端验证数据流链路

- [x] 4.1 新增 `tests/causal_chain_test.go`
  - 测试 `TestCausalChain_EndToEnd`：验证 user → assistant 的因果链
  - 测试 `TestCausalChain_WithToolCall`：验证 4 事件工具调用链

- [x] 4.2 新增 `tests/compression_test.go`
  - 测试 `TestCompression_FullHistory`：验证压缩作用于完整历史，session.Events 不被修改

- [x] 4.3 运行全部测试
  ```bash
  go build ./...
  go test -timeout 5m -count=1 ./agent/ ./tool/... ./event/ ./plugin/ ./memory/ ./prompt/ . ./tests/ -short
  go test -timeout 10m -count=1 ./tests/
  ```

- [x] 4.4 更新 design.md / tasks.md 状态并提交
  - 标记所有任务为 `[x]`
  - 本地 commit 所有变更
