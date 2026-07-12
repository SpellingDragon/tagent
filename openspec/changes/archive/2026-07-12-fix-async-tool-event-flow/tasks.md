## 1. InjectBusInputs RoleSystem → RoleUser 转换

- [x] 1.1 在 `agent/context_manager.go` 的 `InjectBusInputs` 回调中，追加消息前拷贝 message 并将 RoleSystem 改为 RoleUser
- [x] 1.2 新增测试：验证 RoleSystem 消息被转换为 RoleUser，Content 不变，原始事件不被修改

## 2. 压缩以 user input 为切分边界

- [x] 2.1 修改 `agent/task_segmenter.go` 的 `isMessageTaskBoundary`：从 `RoleAssistant && no ToolCalls` 改为 `RoleUser`
- [x] 2.2 修改 `isReferenceTaskBoundary`：从 `TypeAgentOutput` 改为 `TypeExternalInput`
- [x] 2.3 更新 `SegmentMessages` 相关测试：验证以 user input 为边界
- [x] 2.4 更新 `SegmentReferences` 相关测试：验证以 external_input 为边界
- [x] 2.5 更新 `SmartCompressor` 相关测试中对分段行为的断言

## 3. 非交互式命令 stable 后直接完成

- [x] 3.1 在 `tool/action/tmux_monitor.go` 的 `detectSessionState` 中，`stableDuration >= threshold` 后增加 `!IsInteractive && !IsTUI` 检查，满足则直接返回 `SessionCompleted`
- [x] 3.2 新增测试：验证非交互式命令 stable 后返回 Completed
- [x] 3.3 新增测试：验证交互式命令 stable 后仍返回 Stable

## 4. 验证

- [x] 4.1 `go build ./...` 通过
- [x] 4.2 `go test ./agent/...` 全部通过
- [x] 4.3 确认压缩以 user input 为边界
- [x] 4.4 确认非交互式命令 stable 后不进入 fake_alive 检测
