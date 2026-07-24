## Why

The async task layer (change `async-task-management`) uses **two independent timing knobs**:

- `TmuxMonitor.Interval` — a fixed global poll cadence (3s) for *detecting* state.
- `TaskManager.sync_wait` — a fixed window (10s) `Call()` *blocks* before returning an ack.

This is redundant and rigid:

- **Conceptually artificial**: "sync vs async" is a hard boundary bolted on top of polling. A command simply runs; we poll it. If a result comes quickly it feels synchronous; if not, we already said "running" and notify later. The only real question is *when to stop blocking and send the ack* — which a polling schedule can express naturally.
- **Fixed cadence is wasteful and slow at both ends**: 3s polling is too coarse for a 0.5s command (it waits up to a poll for detection) yet needlessly frequent for a service that will run for hours (or an `alive-detached` task). A long-running agent polls thousands of long-lived sessions every 3s forever.

## What Changes

Replace the fixed `Interval` + separate `sync_wait` with a **per-task adaptive (piecewise) poll schedule**, and move the sync→async decision into the detector:

- **Piecewise schedule**: poll densely right after spawn (fast detection of quick commands), then **back off** (sparse polling for long-running / `alive-detached` tasks). Schedule is a function of task age.
- **Detector emits settle-*or*-detach**: at the dense→sparse transition, a not-yet-settled task emits a **detach** signal. This transition point IS the sync→async boundary — no separate `sync_wait`.
- **`TaskManager.Spawn` waits for the first of {settle, detach}**: settle → inline result; detach → ack + background tracking. The `sync_wait` knob is retired.
- **`TmuxMonitor` per-session scheduling**: the single global ticker becomes per-session next-poll scheduling driven by the piecewise function.

Net effect: one adaptive schedule governs both detection cadence and the sync/async boundary — a natural unification — plus faster quick-command detection and far less polling overhead for long/service tasks.

## Capabilities

### New Capabilities
- `adaptive-poll-scheduling`: per-task piecewise (dense→sparse/backoff) poll schedule; the `SettleDetector` settle-or-detach contract where the dense→sparse transition is the sync/async boundary; retirement of the fixed `sync_wait` knob in favor of the detach signal.

### Modified Capabilities
<!-- async-task-execution (from change async-task-management) is not yet archived,
     so it is not listed here as a formal Modified capability. This change
     supersedes its sync-wait window with the detach signal; reconcile at
     implementation time, after async-task-management is archived. -->

## Impact

- **`agent/task_manager.go`**: `SettleDetector` gains a detach signal (e.g. `Detached() <-chan struct{}`); `Spawn` selects settle-or-detach instead of a `sync_wait` timer; `TaskManagerConfig.SyncWait` removed/deprecated. Generic/func detector emits detach after a fixed dense window.
- **`tool/action/tmux_monitor.go`**: global fixed-interval poll loop → per-session adaptive scheduling (next-poll-time by age); `MonitorConfig` gains schedule parameters (dense interval, dense duration, backoff factor, max interval), largely replacing a single `Interval`.
- **`tool/action/settle.go`**: `TmuxSettleDetector` drives detach at the dense→sparse transition; integrates with `alive-detached` (sparsest cadence).
- **Depends on** `async-task-management` being archived first (this change evolves its polling model).
- **Migration**: dense-phase duration ≈ old `sync_wait` (~10s) so default sync/async behavior is preserved; backoff is additive.
