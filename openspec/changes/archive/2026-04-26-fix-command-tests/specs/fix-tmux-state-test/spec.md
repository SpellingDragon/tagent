## ADDED Requirements

### Requirement: TmuxMonitor 状态检测使用轮询等待
`TestTmuxMonitor_StateDetection` 的状态检测必须使用轮询等待而非固定等待后单次检查。

#### Scenario: 轮询等待 completed 状态
- **WHEN** 运行 tmux_exec 模式的短命令
- **THEN** 测试使用 `time.Tick` 轮询 `session.Status`，直到状态变为 `completed` 或超时
- **AND** 超时上限设置为 10 秒
- **AND** 轮询成功时不会因固定 sleep 不足而失败
