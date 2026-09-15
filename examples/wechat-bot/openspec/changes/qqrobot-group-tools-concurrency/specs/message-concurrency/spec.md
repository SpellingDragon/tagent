## Purpose

消息处理并发模型：per-group 串行 + 跨群并行 + 有界 worker 池，防止消息风暴下乱序回复与资源失控。

## ADDED Requirements

### Requirement: per-group 串行

同一群（group_openid）的消息 SHALL 按到达顺序串行处理（FIFO）；串行保证回复顺序与消息顺序一致，避免同群并发 LLM 调用互相穿插。

#### Scenario: 同群消息风暴

- **WHEN** 同一群短时间涌入大量消息
- **THEN** 按到达顺序排队串行处理，回复顺序与触发顺序一致；队列有界（超限丢弃最旧或拒绝新消息并记日志）

### Requirement: 跨群并行与有界 worker 池

不同群的消息 SHALL 并行处理；并行度 MUST 受有界 worker 池约束（池大小可配置），worker 池满时新群任务入队等待而非无限 goroutine 扩张。

#### Scenario: 多群同时触发

- **WHEN** 多个群同时有消息触发聊天管线
- **THEN** 至多 min(群数, 池大小) 个处理在并行，其余排队

#### Scenario: worker 池满载

- **WHEN** 活跃处理数已达池上限且队列已满
- **THEN** 不再创建新 goroutine；新任务按既定策略（丢弃 / 拒绝）处理并记日志，进程内存与 goroutine 数有界

### Requirement: 处理超时与隔离

单条消息处理 SHALL 有超时上限（含 LLM 调用与工具执行）；超时或 panic 的处理 MUST 被隔离（不影响同群后续消息与整个进程）。

#### Scenario: 单条处理超时

- **WHEN** 一条消息的处理耗时超过上限（LLM 卡死 / 工具阻塞）
- **THEN** 该处理被取消，同群队列继续处理下一条，进程不崩溃
