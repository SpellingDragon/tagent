# ops-notice-delivery Specification

## Purpose
运维启动通知（重启恢复通报/系统告警）的投递契约：固定目标、立即唤醒、真实现场或显式降级。

归档溯源：本能力由 `send-cross-task-session`（07-ops-notice-delivery）新增，手工并入主 specs。
## Requirements
### Requirement: 固定目标

启动通知 SHALL 投递到预定目标会址（配置声明的运维会话，缺省=宿主运维会址常量）；MUST NOT 以空地址入 pending 由到达先后决定归属。多会话竞争时 SHALL 由预定目标收到。

#### Scenario: 预定目标收到

- **WHEN** 启动通知产生且存在多个活跃会话
- **THEN** 预定目标会话收到，其他会话零感知

### Requirement: 立即唤醒

无人输入时，启动通知 SHALL 驱动目标会话执行一轮（入链+驱动），MUST NOT 等待用户下一次对话才带入。

#### Scenario: 无人输入即处理

- **WHEN** 启动通知到达且目标会话空闲
- **THEN** 目标会话被驱动跑完一轮

### Requirement: 真实现场或显式降级

重启恢复通报 SHALL 携带上一次工作的近期现场（受范围限制的真实事件查询）；查询面不可用或读失败 SHALL 明确报告降级（既有降级文案），MUST NOT 以 nil 静默省略。

#### Scenario: 现场真实或降级清楚

- **WHEN** 持久存储恢复 / 无存储
- **THEN** 通报含有限条真实现场 / 通报含明确降级报告
