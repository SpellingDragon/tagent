## ADDED Requirements

### Requirement: CommandTool 实现 Close 方法

CommandTool SHALL 实现 io.Closer 接口，提供 Close() error 方法。Close() SHALL 调用 TmuxMonitor.Stop()（如果 monitor 正在运行），并清理所有 tracked tmux session。Close() MUST 幂等。

#### Scenario: Close 停止 TmuxMonitor

- **WHEN** CommandTool 的 TmuxMonitor 正在运行，调用 Close()
- **THEN** TmuxMonitor.Stop() 被调用
- **AND** monitor goroutine 退出
- **AND** 返回 nil（无错误）

#### Scenario: Close 在 tmux 不可用时无副作用

- **WHEN** CommandTool 的 tmuxExecutor 为 nil（tmux 不可用），调用 Close()
- **THEN** 不 panic
- **AND** 返回 nil

#### Scenario: Close 幂等

- **WHEN** 连续调用 Close() 两次
- **THEN** 第二次调用不 panic、不 error
- **AND** TmuxMonitor.Stop() 不会被重复调用

### Requirement: TagentAgent 关闭链包含 CommandTool

TagentAgent SHALL 维护已注册的 CommandTool 列表，Close() 时 SHALL 先关闭所有 CommandTool，再关闭 Runner。TagentAgent.Close() 的调用顺序为：CommandTool.Close() → Runner.Close()。

#### Scenario: Close 关闭所有 CommandTool

- **WHEN** TagentAgent 注册了 2 个 CommandTool，调用 Close()
- **THEN** 两个 CommandTool 的 Close() 都被调用
- **AND** Runner.Close() 在 CommandTool.Close() 之后被调用

#### Scenario: 无 CommandTool 时 Close 正常

- **WHEN** TagentAgent 没有注册 CommandTool，调用 Close()
- **THEN** 仅调用 Runner.Close()
- **AND** 不 panic

### Requirement: KillSession 失败保留 session 重试

handleFakeDead 中 KillSession 失败时 SHALL 不从 sessions map 中删除该 session。session 保留在 map 中，下一个检测周期重新尝试 KillSession。连续 3 次 KillSession 失败后 SHALL 强制删除 session 并记录 error 日志。

#### Scenario: KillSession 失败后保留 session

- **WHEN** handleFakeDead 调用 KillSession 返回 error
- **THEN** session 不从 sessions map 中删除
- **AND** session 的 killRetryCount 递增
- **AND** 下一个检测周期重新尝试 KillSession

#### Scenario: 连续 3 次失败后强制删除

- **WHEN** session 的 killRetryCount 达到 3
- **THEN** session 从 sessions map 中强制删除
- **AND** 记录 error 日志 "forced remove session after 3 kill retries"

### Requirement: TUI 会话超时回收

TUI 会话在 stableDuration 超过 fakeDeadDuration 时 SHALL 返回新状态 SessionTimedOut（而非 SessionRunning）。StateChangeCallback 通知 agent 后，session 从 monitor 移除。

#### Scenario: TUI 会话超时触发回收

- **WHEN** TUI 会话的 StableSince 超过 fakeDeadDuration
- **THEN** detectSessionState 返回 SessionTimedOut
- **AND** StateChangeCallback 被调用，通知 agent 会话已超时
- **AND** session 从 sessions map 中移除

#### Scenario: 非 TUI 会话不受影响

- **WHEN** 非 TUI 会话的 StableSince 超过 fakeDeadDuration
- **THEN** 走原有 fakeDead 检测路径（heartbeat → fakeAlive/fakeDead）
- **AND** 不返回 SessionTimedOut
