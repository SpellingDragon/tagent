## ADDED Requirements

### Requirement: 循环停止后可重启

StopLoop 后再次 StartLoop SHALL 可重启且不破坏消费者契约：每次 StartLoop SHALL 提供有效的输出通道（重启不得向消费者返回已关闭的通道），循环退出时 SHALL 恰好关闭当次启动所对应的通道一次（重复 Stop/Start 往返 MUST NOT 出现 close of closed channel 的 panic）。往返语义（Stop→Start→InjectMessage→事件被消费）SHALL 以 e2e 测试锁定；冥想管理器随 StartLoop 重启的现有行为（lifecycle.go:186-188）SHALL 一并锁定。

#### Scenario: Stop 后 Start 的往返

- **WHEN** StopLoop 返回后同实例再次 StartLoop 并 InjectMessage
- **THEN** 返回的输出通道有效，注入事件被消费且输出可读，无 panic

#### Scenario: 两轮完整往返后停止

- **WHEN** Start→Stop→Start→Stop 两轮完整往返
- **THEN** 第二次 StopLoop 平静返回，不出现对已关闭通道的二次 close panic
