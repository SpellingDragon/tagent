# adaptive-poll-scheduling Specification

## Purpose

Unify synchronous and asynchronous tool execution under a single per-task
adaptive poll schedule. The schedule's dense→sparse transition is the
sync→async boundary, replacing a standalone `sync_wait` timer and a fixed
global poll interval, so short work returns inline, long work acks and settles
later, and long-lived/service tasks are polled cheaply.

## Requirements

### Requirement: Per-task piecewise poll schedule

The monitor SHALL poll each task on a cadence derived from the task's age: a dense phase (short `dense_interval` for the first `dense_duration`) followed by a backoff phase (interval grows by `backoff_factor` up to `max_interval`). Schedule parameters SHALL be configurable, replacing a single fixed interval.

#### Scenario: Dense then backoff
- **WHEN** a task has been running longer than `dense_duration`
- **THEN** its poll interval grows geometrically by `backoff_factor` on each subsequent poll, capped at `max_interval`

#### Scenario: Quick command detected in dense phase
- **WHEN** a command completes within `dense_duration`
- **THEN** it is detected within one `dense_interval` (not gated by a coarse fixed interval)

### Requirement: Settle-or-detach contract

A `SettleDetector` SHALL expose a detach signal in addition to settle signals. The detach signal SHALL fire exactly once, when the dense phase ends without the task having reached a terminal or stable settle. The dense→sparse transition SHALL be the sync/async boundary.

#### Scenario: Detach at dense-phase end
- **WHEN** a task has neither completed nor stabilized by the end of its dense phase
- **THEN** the detector emits its detach signal exactly once

#### Scenario: Settle before detach
- **WHEN** a task settles (completed/stable/suspect) before the dense phase ends
- **THEN** the settle is delivered and no detach signal is emitted for that boundary

### Requirement: Task spawn waits on settle-or-detach without a sync_wait knob

`Spawn` SHALL return an inline result when a settle arrives before detach, and an ack (background-tracked) when detach arrives first. It SHALL NOT use a standalone `sync_wait` timer. Any settle arriving after detach SHALL be routed to the background settle handler (no lost settle).

#### Scenario: Inline on early settle
- **WHEN** a settle arrives before the detach signal
- **THEN** `Spawn` returns an inline result carrying that settle

#### Scenario: Ack on detach
- **WHEN** the detach signal arrives before any settle
- **THEN** `Spawn` returns an ack and the task is tracked in the background

#### Scenario: Post-detach settle not lost
- **WHEN** a settle arrives after detach has already been delivered
- **THEN** it is routed to the background settle handler (OnSettle), not dropped

### Requirement: Per-session adaptive scheduling

The monitor SHALL schedule each session's next poll independently by its age-derived interval, rather than polling all sessions on one global cadence. State detection, callbacks, and terminal-session removal behavior SHALL remain unchanged; only the timing of polls changes.

#### Scenario: Independent cadence
- **WHEN** two sessions have different ages
- **THEN** each is polled on its own age-derived interval, not a shared fixed one

#### Scenario: No starvation
- **WHEN** many sessions are tracked
- **THEN** every due session is polled without indefinite deferral

### Requirement: alive-detached sparsest cadence

An `alive-detached` (service-ready) task SHALL be polled at `max_interval`. Its process death SHALL still be detected (at `max_interval` granularity) and reported via the normal settle path.

#### Scenario: Service polled sparsely
- **WHEN** a task has transitioned to `alive-detached`
- **THEN** the monitor polls it at `max_interval` rather than the dense interval

#### Scenario: Death still detected
- **WHEN** an `alive-detached` service's process dies
- **THEN** the monitor detects it (within `max_interval`) and emits a completion settle

### Requirement: Default behavior preserved

Default schedule parameters SHALL preserve the prior default sync/async behavior: `dense_duration` approximately equals the retired `sync_wait`, so commands shorter than the dense phase still return inline and longer ones still ack. The parallel-window property SHALL hold — concurrent tasks each run their own dense phase, so aggregate blocking approximates the slowest task's `min(dense_duration, real_response)`, not the sum.

#### Scenario: Migration parity
- **WHEN** default parameters are used
- **THEN** a command's inline-vs-ack outcome matches the prior `sync_wait`-based behavior

#### Scenario: Parallel windows do not accumulate
- **WHEN** multiple tasks are spawned concurrently in one turn
- **THEN** total blocking approximates the slowest task's window, not the sum of all windows

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
