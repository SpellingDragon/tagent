## ADDED Requirements

### Requirement: 子 Agent ContextManager 在 runEventLoop 退出后 Close

TagentAgent.Run() 的 runEventLoop goroutine SHALL 在 `runEventLoop` 退出后调用 `invCM.Close()`，释放临时 Runner 资源。`invCM.Close()` 通过 `defer` 执行，确保在 `invOutputCh` 关闭前 Runner 被先关闭。

#### Scenario: 子 Agent 正常完成后 invCM 关闭

- **WHEN** 子 Agent 的 runEventLoop 因 ctx 取消而退出
- **THEN** `defer invCM.Close()` 被执行
- **AND** Runner 内部资源被释放
- **AND** 随后 `defer close(invOutputCh)` 执行

#### Scenario: 子 Agent panic 时 invCM 关闭

- **WHEN** runEventLoop 因 panic 退出
- **THEN** `defer invCM.Close()` 仍然被执行（defer 语义）
- **AND** 资源不泄漏

### Requirement: TrajectoryRecorder 在 TagentAgent.Close() 中关闭

TagentAgent.Close() SHALL 在 `contextManager.Close()` 之后、返回前调用 `trajectoryRecorder.Close()`（如果 `ta.trajectoryRecorder` 非 nil）。这确保 writeLoop goroutine flush 所有缓冲数据并关闭文件句柄。

#### Scenario: Close 时 TrajectoryRecorder 已设置

- **WHEN** 调用 `TagentAgent.Close()` 且 `ta.trajectoryRecorder` 非 nil
- **THEN** 在 contextManager.Close() 之后调用 `trajectoryRecorder.Close()`
- **AND** TrajectoryRecorder 的 writeLoop goroutine flush 并退出
- **AND** 所有打开的 JSONL 文件被 Sync 并关闭

#### Scenario: Close 时 TrajectoryRecorder 未设置

- **WHEN** 调用 `TagentAgent.Close()` 且 `ta.trajectoryRecorder` 为 nil
- **THEN** 跳过 TrajectoryRecorder 关闭（不报错）

#### Scenario: TrajectoryRecorder.Close() 失败

- **WHEN** `trajectoryRecorder.Close()` 返回 error
- **THEN** error 被收集到 errs 列表中
- **AND** Close() 继续执行其他清理
- **AND** 最终返回聚合的 errors
