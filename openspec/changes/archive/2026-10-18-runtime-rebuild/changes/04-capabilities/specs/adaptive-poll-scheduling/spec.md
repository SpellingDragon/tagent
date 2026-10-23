# adaptive-poll-scheduling Specification

## Purpose

本 delta 使轮询节奏与重建执行路径的**停止凭证 / 资源租约**对齐：密集→退避的同步/异步边界不变，但每会话独立调度且与租约证据衔接。

## ADDED Requirements

### Requirement: 轮询与停止凭证/租约对齐

monitor SHALL 以每会话年龄派生的间隔**独立**调度轮询；`alive-detached` 以 `max_interval` 稀疏轮询并仍能检出死亡。轮询读数 SHALL 与 exec/tmux 的停止凭证一致（同一三态 `alive/dead/unknown`），MUST NOT 把 `unknown` 直接判死。

#### Scenario: 独立节奏（并发）

- **WHEN** 两个会话年龄不同
- **THEN** 各自按年龄派生间隔轮询，非共享固定间隔
- **AND** 无饥饿：每个到期会话都被轮询，不被无限推迟

#### Scenario: alive-detached 稀疏轮询仍可检出死亡

- **WHEN** 一个 `alive-detached` 服务进程死亡
- **THEN** monitor 在 `max_interval` 粒度内检出并发出完成 settle

#### Scenario: unknown 与 dead 区分（失败）

- **WHEN** 轮询探测命令返回 error
- **THEN** 计入 `unknown` 而不触发死亡路径；仅确认列表不含会话时判 `dead`

### Requirement: 同步/异步边界即密集→退避边界

`Spawn` SHALL 遵守**一次受理**协议：调用方只收到**一次受理回执（Receipt）**，MUST NOT 因 settle 在 detach 前到达而另走「同步等待窗口内内联最终结果」分支；最终 settle 一律经**单一后台完成通道（OnSettle）恰好投递一次**。MUST NOT 使用独立 `sync_wait` 计时器；detach 后的 settle 不得丢失。

#### Scenario: detach 后 settle 不丢

- **WHEN** settle 在 detach 已投递之后到达
- **THEN** 路由到后台 settle 处理器（OnSettle），不丢弃

#### Scenario: 单次回执与恰好一次结算（一次 Receipt）

- **WHEN** 使用默认参数发起 `Spawn`
- **THEN** 调用方只收到一次受理回执，最终结果经 OnSettle **恰好投递一次**，不存在 inline 与 ack 两条完成路径
- **AND** 并发任务的聚合阻塞≈最慢任务的窗口，而非各窗口之和
