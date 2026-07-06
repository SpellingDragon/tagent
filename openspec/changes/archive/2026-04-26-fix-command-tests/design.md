## 决策

### 决策 1：WorkDir 路径断言 — 使用 `filepath.EvalSymlinks` 规范化

**选择**：对 `expected` 和 `got` 同时调用 `filepath.EvalSymlinks` 后再比较。

**备选方案**：
- 硬编码 `/private/var` 期望值 → macOS 特化，不可移植
- 使用 `strings.HasSuffix` → 够用但不够精确

**选择理由**：`filepath.EvalSymlinks` 是标准库提供的跨平台解决方案，既可处理 macOS 的 `/var→/private/var`，也可处理其他平台的符号链接。

### 决策 2：Timeout 机制 — 先排查根因再决定修复层级

**选择**：分两级排查
1. **测试层排查**：检查 command.go 中 timeout 参数是否正确转换为 `context.WithTimeout`
2. **执行层排查**：检查 `exec.CommandContext` 是否在 context 取消时正确返回错误

**选择理由**：如果根因在测试编写错误（如 timeout 参数未传递），只需改测试；如果根因在执行器未监听 context，需要改执行器——影响面完全不同。先定位再修复。

**备选方案**：直接重写 timeout 执行器
- 风险：可能过度设计

### 决策 3：TmuxMonitor 状态检测 — 轮询等待替代固定 sleep

**选择**：使用 `time.Tick` + 超时循环，轮询检查 `session.Status`，而非 `time.Sleep(2s)` 后单次检查。

**备选方案**：
- 增加 sleep 时间 → 治标不治本，CI 环境仍可能不稳定
- 使用 channel 通知 → 需要修改 TmuxMonitor 暴露回调，影响面大

**选择理由**：轮询等待是测试中最常用的异步状态检测模式，不改变业务代码结构，只改进测试健壮性。

## 风险

| 风险 | 可能性 | 影响 | 缓解 |
|------|--------|------|------|
| Timeout 根因在执行器层 | 中 | 高 | 先代码审查定位，确认影响面后再修 |
| 轮询等待引入超时 | 低 | 低 | 设置合理超时上限（10s） |
| WorkDir 修复引入新平台差异 | 低 | 低 | EvalSymlinks 是标准库跨平台函数 |

## 实施步骤

### Phase 1: 定位 Timeout 根因
1. 阅读 `tool/command/command.go` 中 exec 模式的 Call 方法
2. 检查 `context.WithTimeout` 或 `context.WithDeadline` 的创建和传递
3. 检查 `exec.CommandContext` 的 cancel 语义
4. 确认问题在测试层还是执行层

### Phase 2: 修复三个测试
1. `TestCommandTool_WorkDir`: 加入 EvalSymlinks
2. `TestCommandTool_Timeout`: 根据 Phase 1 结论修复
3. `TestTmuxMonitor_StateDetection`: 轮询等待替代 sleep

### Phase 3: 验证
- 运行 `go test ./tool/command/... -count=1` 确认全部通过
- 多次运行确认无 flaky
