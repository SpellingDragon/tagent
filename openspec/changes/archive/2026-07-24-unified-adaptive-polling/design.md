## Context

Change `async-task-management` established the task layer with two fixed timing knobs: `TmuxMonitor.Interval` (global 3s poll) and `TaskManager.sync_wait` (10s block-then-ack). The sync/async boundary is `sync_wait`; detection granularity is `Interval`. Detectors emit settle signals (`completed`/`stable`/`suspect`); `Spawn` blocks on a `sync_wait` timer racing the first settle. This change evolves that model into a single adaptive poll schedule.

## Goals / Non-Goals

**Goals:**
- One adaptive per-task schedule governing both detection cadence and the sync/async boundary.
- Faster detection of quick commands (dense early polling) and much lower overhead for long/`alive-detached` tasks (backoff).
- Remove the standalone `sync_wait` knob; the dense→sparse transition becomes the ack point.
- Preserve current default behavior (dense-phase ≈ old `sync_wait`) and the parallel-window property.

**Non-Goals:**
- Changing settle semantics (the three settle kinds and `alive-detached` stay as-is).
- Changing the task_settled reclaim path, the board, or the LLM task tools.
- Sub-second real-time monitoring or event-driven (non-poll) detection.

## Decisions

### D1: Per-task piecewise poll schedule

Polling cadence is a function of task age: a **dense phase** (short interval `dense_interval`, e.g. 1s) for the first `dense_duration` (e.g. 10s), then a **backoff phase** (interval grows ×`backoff_factor` up to `max_interval`, e.g. 2× up to 60s). `MonitorConfig` gains `{dense_interval, dense_duration, backoff_factor, max_interval}`, replacing the single `Interval`. `alive-detached` tasks pin to `max_interval` (sparsest).

### D2: `SettleDetector` settle-or-detach contract

The detector exposes, in addition to `Settled()`, a **detach** signal: `Detached() <-chan struct{}`, fired once when the dense phase ends without a terminal/stable settle. This transition point IS the sync→async boundary. `Detached()` closing (or emitting once) tells the task layer "stop blocking; I'll notify later via the normal settle/OnSettle path."

### D3: `TaskManager` retires `sync_wait`

`Spawn` becomes `select { case sig := <-Settled(): inline(sig) ; case <-Detached(): ack + track }` — no `sync_wait` timer. `TaskManagerConfig.SyncWait` is removed. The boundary now lives entirely in the detector's schedule. The window-close/no-lost-settle invariant is preserved: detach closes the window, and any settle arriving after routes to `OnSettle` (background), exactly as timeout did before.

### D4: `TmuxMonitor` per-session adaptive scheduling

Replace the single global ticker + `checkAllSessions` with per-session **next-poll-time**. The loop wakes at the nearest due time, polls only due sessions, and reschedules each by its age-derived interval (D1). This keeps the existing state-detection logic (`detectSessionState`, callbacks, terminal removal) unchanged — only *when* a session is polled changes. A min-heap or simple scan over sessions (small N) computes the next wake.

### D5: Detach for non-tmux detectors

- **Generic/func detector**: emits detach after a fixed `dense_duration` if `fn` hasn't returned (settle = fn returns).
- **Sub-agent detector** (if/when Phase 3 lands): detach after a fixed dense window; settle = RunFlow returns.

A small shared helper (`detachAfter(dense_duration)`) provides the timer-based detach so each detector needs minimal code.

### D6: alive-detached synergy

Once a task is `alive-detached` (service ready), its session polls at `max_interval` — the monitor stops frequent polling of known-alive services, directly addressing the "poll thousands of long-lived sessions every 3s" waste. Process death is still caught (at `max_interval` granularity) → reclaim/notify.

### D7: Migration & back-compat

Default `dense_duration` = old `sync_wait` (~10s) and `dense_interval` ≈ old `Interval` (or tighter), so default sync/async behavior and quick-command latency are preserved or improved. Parallel tool windows still hold (each task's dense phase runs concurrently; blocking ≈ `max_i min(dense_duration, real_i)`). Session reaping (completed/error → kill) is unchanged.

## Risks / Trade-offs

- **[R1] Monitor refactor**: per-session scheduling replaces a simple ticker — the highest-risk piece. Mitigation: keep `detectSessionState`/callbacks intact; add focused tests for schedule progression (dense→backoff), due-time selection, and no-starvation.
- **[R2] Detector contract change**: adding `Detached()` touches the validated Phase-0 primitive and every detector. Mitigation: additive interface method with a default (fixed-window) helper; migrate detectors one by one with `-race` tests.
- **[R3] Boundary reframed, not removed**: `sync_wait` becomes `dense_duration` — conceptual unification, but a boundary still exists. Accepted: the win is one knob + adaptive cadence, not zero boundaries.
- **[R4] Backoff latency for late completion**: a task finishing at t=40s in the backoff phase is detected at up to `max_interval` granularity (e.g. +60s), slower than the old fixed 3s. Accepted trade-off (long tasks are already async; the reclaim turn is not latency-critical). Tunable via `max_interval`.
- **[R5] Sequencing**: depends on `async-task-management` archived first; implementing before that risks spec/code drift.
