# `tool/action` 机械清扫中被隔离的判据行（待按 C0 折入 docs/wiki）

来源：`gofmt` 前的 free-standing 删除扫描，关键词双语匹配（禁/不得/必须/否则/原因/契约/静默/竞态/防/误/唯一/绝不/只能/须 与 must/never/only/single source/guard/silent/race/prevent/instead of/without）。
状态：下列带 ← 标记的条目已折入文档；余下仍需逐条判定。原用途：

**下一批必须逐条判定"归文档 / 归 doc 槽 / 确认无价值"**，不得就此消失。

- `tool/action/action_tool.go:1070` // Graceful phase: SIGTERM the pane process (if resolvable).
- `tool/action/action_tool.go:200` // never be killed, and each holds a pty. Disable via
- `tool/action/action_tool.go:205` // R3 2.6（唯一挂载点）：多 agent org 中仅首个实例执行重挂（重复重挂=
- `tool/action/action_tool.go:379`  ← 已折入 docs/wiki/tool/tmux-action.md // call addresses an EXISTING session (peek/send/stop) instead of spawning
- `tool/action/action_tool.go:394` // quiet_timeout validation: must be either 0 (default) or >= the stability  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/action_tool.go:401` // ALWAYS in force — 0/omitted falls back to the configured default, so the only  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/action_tool.go:411`  ← 已折入 docs/wiki/tool/tmux-action.md // diagnosable in the bot log, not silently returned as a plain tool error.
- `tool/action/action_tool.go:454` // 渗透（可读原因，模型可稍后重试或改同步小命令）。
- `tool/action/action_tool.go:465`  ← 已折入 docs/wiki/tool/tmux-action.md // 5.4（design-report-closeout）：disk degraded 禁新 spawn——拒绝以 result
- `tool/action/action_tool.go:516` }) // QuietTimeout >0 only: zero value must stay zero so the monitor falls  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/action_tool.go:922` // (read-only) deliberately never calls this, and op=stop is terminal.
- `tool/action/declarative.go:146` // Unrecoverable projection: display-only task (no closures). Still bound the  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/declarative.go:148` // restarts instead of lingering on the board forever.  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/declarative.go:210` // so the rebuilt task keeps its reaper anchor across a restart instead of
- `tool/action/resident_recovery.go:153` // stamps LastAdoptedAt; the sweep that follows only reaps sessions nobody
- `tool/action/resident_recovery.go:181` // refresh the freshness anchor so the sweep never reaps it.  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/resident_recovery.go:185` // Already tracked (multi-instance share guard): still adopted —  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/resident_recovery.go:269` // guard: another agent instance may have adopted it). nil monitor →
- `tool/action/resident_recovery.go:293` // is wired — tests may run record-only) and drop its record:
- `tool/action/settle.go:248` delta = 0 // pane scrolled/truncated — lost lines, never fake hits  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/settle.go:82` mu       sync.Mutex      // guards detach + baseline (round state)
- `tool/action/settle.go:99` probeMu     sync.Mutex // guards probe failure bookkeeping (C2)
- `tool/action/tmux_executor.go:151` SessionTimedOut  SessionStatus = "timed_out" // TUI session exceeded fakeDeadDuration without output change
- `tool/action/tmux_executor.go:198` // or stopped, never silently double-spawned (two dev servers on one  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_executor.go:198` // port is the classic footgun this guard exists for).  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_executor.go:199` // Duplicate protection: a named session must be explicitly restarted  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_executor.go:224`  ← 已折入 docs/wiki/tool/tmux-action.md // HEAD of the stream, never the tail -- and consumers assert on tails.  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_executor.go:231`  ← 已折入 docs/wiki/tool/tmux-action.md // mount race from a cross-process RTT to <1ms. The pipe log records raw
- `tool/action/tmux_executor.go:237`  ← 已折入 docs/wiki/tool/tmux-action.md // the captured stderr so the real cause is never hidden as "exit status 1".
- `tool/action/tmux_executor.go:276` // bounds growth. A missing pipe file (never attached) is fine.
- `tool/action/tmux_executor.go:291`  ← 已折入 docs/wiki/tool/tmux-action.md // instead of deleting it: it is the only complete record of what the
- `tool/action/tmux_executor.go:450` // kill -0 checks if process exists without sending a signal
- `tool/action/tmux_executor.go:537` continue // named sessions are owned (reattach/TTL sweep), never orphans
- `tool/action/tmux_monitor.go:444` // 3. New state must differ from the last notified state (prevent duplicates)  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_monitor.go:446` // 2. Both old and new states must be meaningful (not FakeDead/FakeAlive)  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_monitor.go:448` // 1. A callback (global or per-session) must be registered  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_monitor.go:515` // assume-dead→Completed→杀会话，tmux 抖动即误杀常驻会话）。
- `tool/action/tmux_monitor.go:561` // LastOutput (the last Running-poll snapshot) instead of clobbering  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_monitor.go:561` // it — consumers must get the true final tail (e.g. END_MARKER).  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_monitor.go:564` processExists = false // list-sessions 已确定性判死：不再被 display-message err 误导  ← 已折入 docs/wiki/tool/tmux-action.md
- `tool/action/tmux_monitor.go:577` // coding agents) never reach this branch -- their live-poll
- `tool/action/tmux_monitor.go:579` // Race guard: with remain-on-exit, capture-pane can race the pane
- `tool/action/tmux_monitor.go:613` // Convergence guard: a capture taken while tmux is still flushing
- `tool/action/tmux_monitor.go:638` // foreground process's stdin and (b) is never executed anyway (the
- `tool/action/tmux_monitor.go:669` // Long-silent-alive sessions later crossing the fake-dead
- `tool/action/tmux_monitor.go:675` // Fake dead: stable for too long without output change
- `tool/action/tmux_monitor.go:681` // layer's alive-detached semantics (D4), which never fired for
- `tool/action/tmux_monitor.go:696` // long-silent-but-ALIVE workloads (compiles, downloads, resident
- `tool/action/tmux_monitor_scenario_test.go:198` // TUI simulation: prints a static screen (output never changes)
- `tool/action/tui_timeout_test.go:120` // 契约：静默+存活+无显式超时 → 保持 Stable，绝不击杀
- `tool/action/tui_timeout_test.go:161` QuietTimeout: explicitTimeout, // 显式声明：静默超过此阈值视为假死（Duration >0 = opt-in）
- `tool/action/tui_timeout_test.go:64` // 依据：静默是长任务的常态（编译/训练/长 sleep），旧"静默→heartbeat→杀"链路会误杀健康任务
- `tool/action/tui_timeout_test.go:64` // 契约翻转（A1）：非交互会话静默超时但进程存活时，默认保持 Stable 不再自动击杀。
- `tool/action/tui_timeout_test.go:73` // ==================== Task 5.5 (revised 2026-09-11): 非交互会话静默≠假死 ====================

## 第二批（81 轮，`--do sweep` 删除 392 行中命中判据词的 35 行）

- `tool/action/action_test.go:256` // A configured default applies only when ttl is omitted (0). §6.4 (6.4
- `tool/action/declarative_test.go:106` // answers, never a zero/unbounded lifetime.
- `tool/action/declarative_test.go:17` ct := &ActionTool{} // zero: SpecFromDeclarative only builds closures, never invokes them
- `tool/action/declarative_test.go:99` // Rotate the record only — no write into the tool.
- `tool/action/generation_tracker_test.go:43` // The exact harm shape the re-arm prevents: the manager holding the stale
- `tool/action/generation_tracker_test.go:9` // 换代后必须把 tracker 重挂到**当前代**的工具上（build_agent 的 wireAgent 早有
- `tool/action/generation_tracker_test.go:9` // 此注释：「executorOnly 热重建换代 ActionTool 后，旧闭包指向旧 monitor，须重接
- `tool/action/poll_schedule_test.go:119` s2.IsInteractive = true // A1: default fake-dead path (heartbeat) is interactive-only now
- `tool/action/poll_schedule_test.go:57` // override that protects silent-but-legal tasks (long downloads, compiles,
- `tool/action/resident_recovery_test.go:118` // named 会话必须存活（cleanup 排除 n-；NamedSessionName("gatetest-svc")="n-gatetest-svc"）。
- `tool/action/resident_recovery_test.go:121` // 双条件枚举必须命中 n-。
- `tool/action/resident_recovery_test.go:133` // resident_session 事件发射、唯一挂载点、TaskID 桥（suspect→running）。
- `tool/action/resident_recovery_test.go:258` // Oneshot sessions must NOT persist.
- `tool/action/session_lifecycle_test.go:194` IsInteractive: true, // A1: heartbeat→fake-dead retry cycle is interactive-only now
- `tool/action/session_lifecycle_test.go:60` // ActionTool without tmux should still Close() without error
- `tool/action/settle_test.go:201` // PTY: "fork failed: Device not configured"), so they run only where tmux works.
- `tool/action/settle_test.go:201` // reaped. They skip when tmux cannot create a session (e.g. a sandbox without a
- `tool/action/settle_test.go:247` detector.Cancel() // idempotent — reapOnce/closeOnce guard against double-kill
- `tool/action/settle_test.go:274` // Short dense phase so Spawn detaches (ack) quickly instead of blocking.
- `tool/action/settle_test.go:300` // Past the grace TTL → List() triggers prune, freeing the registry entry.
- `tool/action/tmux_executor_test.go:115` // When runAsUser is set, set-environment commands must also go through sudo
- `tool/action/tmux_executor_test.go:283` // The trailing "END_MARKER" must be preserved in the tail we hand to the LLM.
- `tool/action/tmux_executor_test.go:287` // If the raw output exceeded the 2000-char inline limit, the tool must
- `tool/action/tmux_executor_test.go:305` // NOTE: valid names are never exercised through Call here — on a machine with
- `tool/action/tmux_executor_test.go:345` // the guard provably fired pre-creation.
- `tool/action/tmux_executor_test.go:369` // Invalid names must be rejected by Call in the validation phase, before
- `tool/action/tmux_monitor_test.go:1387` // Re-add plainly (no callback) and transition; the old per-session cb must not fire.
- `tool/action/tmux_monitor_test.go:236` // alive+quiet without explicit quiet_timeout is NOT completion).
- `tool/action/tmux_monitor_test.go:520` // The session should stay Running forever, never reaching stable or triggering heartbeat.
- `tool/action/tmux_monitor_test.go:539` session.IsInteractive = true // A1: heartbeat→fake-dead path is interactive-only now
- `tool/action/tmux_monitor_test.go:544` // return TimedOut instead of triggering heartbeat/send-keys.
- `tool/action/tmux_monitor_test.go:624` // Heartbeat must NOT be sent for TUI
- `tool/action/tmux_monitor_test.go:675` // Heartbeat should never have been sent
- `tool/action/tmux_monitor_test.go:823` // Phase 5: TUI sessions now return SessionTimedOut (removed) instead of SessionRunning.
- `tool/action/tmux_monitor_test.go:835` // OK — callbacks only fire on transitions

## `tool/action` 第三批（88 轮 tdoc 51 处中命中判据词的 20 行）

- `tool/action/declarative_test.go:47` // A record without the key (pre-ttl) leaves TTL unset → the manager default governs.
- `tool/action/declarative_test.go:47` // keeps the model's chosen anchor instead of collapsing to the default floor.
- `tool/action/declarative_test.go:47` // must replay back into the rebuilt TaskSpec.TTL across a restart, so the reaper
- `tool/action/declarative_test.go:79` // a numeric-only change to task_default_ttl reached the TaskManager (which pulls)
- `tool/action/declarative_test.go:95` // (a tool built outside any org composition root): the construction default must
- `tool/action/poll_schedule_test.go:121` // session. Uses a nil-instrumented ActionTool — validation must fire before
- `tool/action/poll_schedule_test.go:144` // one with an override, one default — must be judged with their own thresholds.
- `tool/action/poll_schedule_test.go:166` // override must pass the threshold check and NOT return TimedOut while under
- `tool/action/poll_schedule_test.go:166` // the override; past the override it must return TimedOut (not heartbeat).
- `tool/action/poll_schedule_test.go:30` // grows and never exceeds MaxInterval.
- `tool/action/poll_schedule_test.go:81` // (150s) must NOT be judged fake-dead.
- `tool/action/poll_schedule_test.go:99` // threshold must equal the global fakeDeadDuration exactly — at threshold-1
- `tool/action/resident_recovery_test.go:245` // but leaves fresh ones alone. Uses a fake now; kills only touch metadata of
- `tool/action/resident_recovery_test.go:81` // 无 tmux 时以源级契约（NamedSessionName 前缀 vs cleanup 过滤条件）静态自洽。
- `tool/action/session_lifecycle_test.go:285` // (count grows on the NEXT emit), preventing wake-up storms on log floods.
- `tool/action/settle_test.go:163` // round state — a fresh detach window and a new output baseline — without
- `tool/action/settle_test.go:247` // in the TaskManager — after the task completes and its grace elapses, prune
- `tool/action/tmux_executor_test.go:169` // constructor that never returns nil).
- `tool/action/tmux_executor_test.go:169` // must reflect the system truth — with a PATH that has no tmux binary it must
- `tool/action/tmux_monitor_scenario_test.go:449` // This is why heartbeat must NOT use send-keys for TUI sessions.

## `build_cycle_test.go`（89 轮压缩，摘除的论证行 —— 需按 C0 归入构建期文档）

- `build_cycle_test.go` // agents referencing each other through config (A→B→A or self-reference)
- `build_cycle_test.go` // must fail with an explicit cycle error at build time — the build cache
- `build_cycle_test.go` // only dedupes COMPLETED agents, so without the path check a cycle recurses
- `build_cycle_test.go` // until the stack overflows.

## 定级结论（123 轮，探针与 85 同一实现）

本次对这两份账共 **212 条**跑探针：**203 条原文件仍留痕**（判据由代码或现存注释承载），**9 条无文本痕迹**。9 条逐条人工判定，依据是**读周边上下文是否连贯**＋判据是否落在代码里，而不是只看计数：

- 全部 9 条均为**被删自由注释块里的句子碎片**，删除后周围注释与代码**读起来连贯、无悬空句**（已抽查 `examples/wechat-bot/dedup.go`、`tests/invariants_test.go` 等处）。
- 其中判据并未丢失：`dedup.go` 的"不静默覆盖、留作事后分析"落在 `os.Rename(s.path, s.path+".corrupt")` 这行代码上；`tui_timeout_test.go` 的"非交互会话静默≠假死"已在 `docs/wiki/tool/tmux-action.md#quiet-vs-dead`；其余为分节横幅（`==== Task 5.5 ====`）与测试内说明碎片，无契约内容。
- 结论：**本两份账无待还原的真缺口**。

⚠️ 同时纠正我自己的一个检测缺陷：第一版"孤儿 doc"探针检查的是**注释组内任意一行**，因而误报 505 条（多行 doc 的续行本就可能以 the/and 开头）；改为只看**组首行**后为 **0**。⇒ 教训：报出任何"损伤计数"之前，先确认探针的判据位置正确；否则会把检测器缺陷说成代码缺陷。
