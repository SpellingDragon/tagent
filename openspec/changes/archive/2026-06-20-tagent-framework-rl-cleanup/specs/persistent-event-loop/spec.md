## MODIFIED Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：drain mailbox（阻塞等第一个事件 + non-blocking drain 剩余）→ mergeBatch 合并为一条 model.Message → runner.Run() → 转发事件到 outputCh → 回到 drain。Loop SHALL NOT 在 Run 结束（Flow break）时退出。Loop SHALL 仅在 loopCtx 被取消（StopLoop）时退出。

Loop SHALL NOT 执行任何 trajectory 采集、reward 计算或 trajectory 存储逻辑。Loop 仅负责事件转发、日志记录和 OTLP span 属性设置。

#### Scenario: Run 结束后继续 drain

- **WHEN** runner.Run() 返回的 event channel 关闭（Flow 在 IsFinalResponse 时 break）
- **THEN** Loop 不退出
- **AND** Loop 回到 drain mailbox 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

#### Scenario: Loop 不采集 trajectory

- **WHEN** Loop 处理完一个 batch 的事件
- **THEN** 不创建 Trajectory 记录
- **AND** 不调用任何 RewardFunc
- **AND** 不调用任何 TrajectoryStore.Add
- **AND** 仅记录日志（batch 完成、duration、events、tokens）和 OTLP span 属性

## REMOVED Requirements

### Requirement: 复用 Runner 全部管道

**Reason**: 移除 trajectory 采集后，Loop 不再需要 collector 和 trajectory 相关的管道逻辑。Runner 管道复用（Session、Plugin、BeforeModel）保持不变，仅移除 trajectory 采集部分。

**Migration**: Loop 仍复用 Runner 的全部管道（Session 管理、Plugin.OnEvent、BeforeModel、AppendEventHook），仅移除 trajectory collector 和 storeTrajectory 调用。
