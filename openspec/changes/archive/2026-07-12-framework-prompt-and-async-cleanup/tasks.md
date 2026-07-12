## 1. 框架 system prompt 注入

- [x] 1.1 在 `agent/tagent_agent.go` 或 `agent/context_manager.go` 中定义 `FrameworkPrompt` 常量，包含异步工具说明、事件标识说明、上下文压缩说明
- [x] 1.2 在 `newContextManagerFromConfig` 中，将 `FrameworkPrompt` 前置到 `cfg.SystemPrompt`
- [x] 1.3 新增测试验证 system prompt 包含框架说明

## 2. 删除 AsyncTaskChecker

- [x] 2.1 删除 `AsyncTaskChecker` 接口、`asyncTaskCheckers` 字段、`RegisterAsyncTaskChecker`、`hasPendingAsyncTasks` 方法
- [x] 2.2 删除 `Run()` 中的 `hasPendingAsyncTasks()` 检查和 `persistentBus` 临时重定向逻辑
- [x] 2.3 删除 `ActionTool.HasPendingAsyncTasks` 方法
- [x] 2.4 删除 `tagent.go` 中的 `RegisterAsyncTaskChecker(actionTool)` 调用
- [x] 2.5 删除 `async_task_checker_test.go`

## 3. handleStateChange 注入到 entry agent

- [x] 3.1 在 `tagent.go` 的 `New()` 中，entry agent 构建后，收集所有 agent 的 ActionTool，调用 `SetMessageInjector(entryAgent)`
- [x] 3.2 移除 `buildAgent` 中的 `actionTool.SetMessageInjector(ta)` 和 `ta.RegisterAsyncTaskChecker(actionTool)`（已随 Task 2 删除）
- [x] 3.3 确认 `InjectMessage` 在 StartLoop 模式下注入到 persistentBus（entry agent 的行为）

## 4. 简化 waiting_async_response

- [x] 4.1 删除 `TmuxExecResponse.Message` 字段
- [x] 4.2 `executeAsync` 返回值简化为 `{SessionID, Status:"waiting_async_response"}`

## 5. 测试与验证

- [x] 5.1 `go build ./...` 通过
- [x] 5.2 `go test ./agent/... ./plugin/... ./event/...` 全部通过
