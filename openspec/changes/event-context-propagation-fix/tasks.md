## 1. 自动注入 event_keys（auto-event-key-injection）

- [x] 1.1 在 `AgentToolWrapper` 结构体中新增 `parentProjection *SessionProjection` 字段
- [x] 1.2 修改 `NewAgentToolWrapper` 签名，增加 `parentProjection *SessionProjection` 参数（改为 SetParentProjection 方法，避免破坏现有签名）
- [x] 1.3 在 `AgentToolWrapper.Call` 中实现自动注入逻辑：当 LLM 未传 event_keys 且 eventParams 包含 "event_keys" 且 parentProjection 非 nil 时，取最近 5 个 EventKey 自动注入
- [x] 1.4 更新 `tagent.go` 中 `buildAgentToolRef` 的 `NewAgentToolWrapper` 调用，传入父 Agent 的 projection

## 2. 子 Agent drain 模式（subagent-event-drain）

- [x] 2.1 修改 `TagentAgent.Run()` 的 wrappedCh goroutine：收到最终响应后不立即 return，进入 500ms drain 模式
- [x] 2.2 drain 模式：使用 `select` + `time.After(500ms)` 从 `invOutputCh` 读取剩余事件转发到 `wrappedCh`，超时或 channel 关闭后退出
- [x] 2.3 验证：确保 drain 期间转发的尾部事件被 AgentToolWrapper.Call 正确消费

## 3. 资源清理（resource-lifecycle-cleanup）

- [x] 3.1 在 `TagentAgent.Run()` 的 runEventLoop goroutine 中添加 `defer invCM.Close()`，确保临时 Runner 资源在 runEventLoop 退出后被释放
- [x] 3.2 在 `TagentAgent.Close()` 中添加 `trajectoryRecorder.Close()` 调用（在 contextManager.Close() 之后）
- [x] 3.3 验证：`ContextManager.Close()` 不会关闭共享的 memPlugin/sessionSvc（检查实现，确保只关闭 Runner）

## 4. Prompt 引导（prompt 更新）

- [x] 4.1 在 `examples/wechat-bot/resources/prompts/TOOLS.md` 中增加 event_keys 使用指南段落
- [x] 4.2 在 `examples/wechat-bot/resources/prompts/knowledge_tool_desc.md` 中增加 event_keys 参数说明
- [x] 4.3 在 `examples/wechat-bot/resources/prompts/recall_tool_desc.md` 中增加 event_keys 参数说明

## 5. 验证与测试

- [x] 5.1 运行 `go build ./...` 确保编译通过
- [x] 5.2 运行 `go test ./tests/ -run TestInvariant -timeout 30s` 确保不变量测试通过
- [x] 5.3 运行 `go test ./agent/ -run "TestAgentTool|TestTrajectory" -timeout 60s` 确保 agent 测试通过
- [x] 5.4 编写测试：验证 LLM 未传 event_keys 时自动注入最近 5 个 EventKey
- [x] 5.5 编写测试：验证子 Agent 最终响应后 drain 模式转发尾部事件
- [x] 5.6 编写测试：验证 TagentAgent.Close() 调用 TrajectoryRecorder.Close()
- [x] 5.7 编写测试：验证子 Agent Run() 退出后 invCM.Close() 被调用
