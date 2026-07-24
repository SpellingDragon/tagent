## 1. Phase 0 — detach contract + schedule primitive (offline-testable)

- [x] 1.1 Add `Detached() <-chan struct{}` to `SettleDetector`; add a shared `detachAfter(dense_duration)` timer helper
- [x] 1.2 Generic/func detector emits detach after `dense_duration` (settle = fn returns); `-race` unit test
- [x] 1.3 Pure poll-schedule function `intervalForAge(age, params) time.Duration` (dense→backoff→cap); table-driven unit tests

## 2. Phase 1 — TaskManager retires sync_wait

- [x] 2.1 `Spawn` = `select { <-Settled() → inline ; <-Detached() → ack+track }`; remove `TaskManagerConfig.SyncWait`; preserve window-close / no-lost-settle (post-detach settle → OnSettle)
- [x] 2.2 Migrate Phase-0 tests (drop `SyncWait`, drive detach); deterministic `-race`: inline-on-settle, ack-on-detach, post-detach settle not lost

## 3. Phase 2 — TmuxMonitor per-session adaptive scheduling

- [x] 3.1 Replace global ticker + `checkAllSessions` with per-session next-poll-time; loop wakes at nearest due, polls only due sessions
- [x] 3.2 Reschedule each session by `intervalForAge`; `alive-detached` pinned to `max_interval`; keep `detectSessionState`/callbacks/terminal-removal unchanged
- [x] 3.3 Deterministic tests (mock inspector): dense→backoff progression, due-time selection, no starvation, alive-detached sparse

## 4. Phase 3 — tmux detector detach + integration

- [x] 4.1 `TmuxSettleDetector` emits detach at dense→sparse transition (wired to schedule); `-race` unit test
- [x] 4.2 `MonitorConfig` gains `{dense_interval, dense_duration, backoff_factor, max_interval}` replacing `Interval`; `builtin.go` parse + defaults
- [x] 4.3 Real tmux (full-mode) tests: quick command inline within dense, long command ack + later reclaim, service polled sparsely

## 5. Phase 4 — migration, e2e, docs, close-out

- [x] 5.1 Default params preserve prior sync/async behavior (`dense_duration` ≈ old `sync_wait`); confirm parallel-window property holds
- [x] 5.2 Full regression `go test ./... -short` + real-LLM async e2e parity (ack → settle → reclaim, no hang)
- [x] 5.3 Update README §异步任务层: adaptive polling supersedes `sync_wait`; document schedule params
- [x] 5.4 `openspec validate unified-adaptive-polling --strict`; Conventional Commits staged commit
