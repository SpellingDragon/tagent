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

A SettleDetector SHALL expose separate settle, detach and confirmed Stopped signals. Detach SHALL end the dense foreground phase without asserting producer completion. Ordinary observations MUST NOT cancel the dense timer or satisfy an unrelated wait condition. Cancel is a request; Stopped SHALL close only after actual producer shutdown is confirmed.

#### Scenario: Stable observation precedes detach
- **WHEN** an exit-waiting live command becomes output-stable during the dense phase
- **THEN** its observation is recorded and the dense deadline can still detach the caller

#### Scenario: Cancel without confirmed stop
- **WHEN** native termination fails
- **THEN** Stopped remains unconfirmed even though cancellation was requested

### Requirement: Task spawn waits on settle-or-detach without a sync_wait knob

Spawn SHALL wait for its declared wait outcome or detach, with no independent sync_wait timer. A nonmatching observation SHALL use the same reliable event path without completing the foreground wait. The window SHALL be identified by its original round; late handoff MUST NOT close another round's window.

#### Scenario: Matching readiness before detach
- **WHEN** explicit ready evidence arrives while a ready wait is dense
- **THEN** Spawn returns it inline while the service producer may remain alive

#### Scenario: Detach before matching outcome
- **WHEN** the dense phase ends before the declared condition is met
- **THEN** Spawn returns an honest background ACK and later signals remain deliverable

### Requirement: Per-session adaptive scheduling

The monitor SHALL schedule sessions independently by their age-derived interval and SHALL fence native capture, state changes, watch/probe callbacks and baseline changes by the immutable resource identity and producer round. Terminal detection SHALL retain output/pane evidence until the exact signal is accepted and acknowledged, rather than removing resources at first observation.

#### Scenario: Old native sample completes late
- **WHEN** capture or probe work begun for an old resource or producer round returns after rebind
- **THEN** it does not modify the new resource, baseline or round

#### Scenario: Terminal receiver is temporarily unavailable
- **WHEN** native exit is observed but the receipt is rejected
- **THEN** terminal evidence remains available for exact retry and recovery

### Requirement: alive-detached sparsest cadence

An `alive-detached` (service-ready) task SHALL be polled at `max_interval`. Its process death SHALL still be detected (at `max_interval` granularity) and reported via the normal settle path.

#### Scenario: Service polled sparsely
- **WHEN** a task has transitioned to `alive-detached`
- **THEN** the monitor polls it at `max_interval` rather than the dense interval

#### Scenario: Death still detected
- **WHEN** an `alive-detached` service's process dies
- **THEN** the monitor detects it (within `max_interval`) and emits a completion settle

### Requirement: Default behavior preserved

Default schedule durations and adaptive cadence SHALL remain bounded and configurable. The default dense duration SHALL preserve the usual short-command inline and long-command ACK timing when the declared outcome is reached. Outcome classification SHALL follow explicit wait semantics and native evidence rather than reproducing historical stable-as-completed behavior. Concurrent dense windows MUST NOT accumulate serial waiting time.

#### Scenario: Native quick completion remains inline
- **WHEN** a command natively exits within its default dense duration
- **THEN** its result returns inline, without waiting for a separate sync_wait timer

#### Scenario: Quiet live command reaches dense boundary
- **WHEN** an exit-waiting command remains alive through its dense duration
- **THEN** it returns an honest ACK even if its output was stable

#### Scenario: Parallel windows do not accumulate
- **WHEN** multiple tasks are spawned concurrently
- **THEN** aggregate foreground blocking approximates the slowest bounded window, not their sum
