## 1. 定位 Timeout 根因

- [x] 1.1 阅读 `tool/command/command.go` — 追踪 exec 模式下 timeout 参数的传递链路
- [x] 1.2 检查 `context.WithTimeout` 是否正确创建并传入 `exec.CommandContext`
- [x] 1.3 判断根因在测试层还是执行器代码层

## 2. 修复测试

- [x] 2.1 `TestCommandTool_WorkDir` — 对 expected 和 got 路径使用 `filepath.EvalSymlinks` 规范化后比较
- [x] 2.2 `TestCommandTool_Timeout` — 根因在执行器层，`Execute` 始终返回 nil error，修复 context error 传递
- [x] 2.3 `TestTmuxMonitor_StateDetection` — `time.Sleep(2s)` → 轮询等待（超时 10s）

## 3. 验证

- [x] 3.1 运行 `go test ./tool/command/... -count=1 -v` 确认 3 个用例全部通过
- [x] 3.2 连续运行 3 次确认无 flaky（`go test ./tool/command/... -count=3`）
