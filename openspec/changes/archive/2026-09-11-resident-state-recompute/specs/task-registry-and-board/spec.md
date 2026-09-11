## ADDED Requirements

### Requirement: 任务板存活性对账在启动重连期而非查询热路径

任务板（TaskManager registry）的存活性对账 SHALL 在启动期 `ReattachResidentSessions` 内完成（连带从「重发现的 tmux 会话 + 持久 task 元数据」重建板条目），并可选由限速后台 sweep 维护；SHALL NOT 挂在 `List()` 查询路径或每次 `BeforeModel` 的热路径上。`List()` SHALL 为只读内存快照——无 shell out、无状态变更、无 onSettle 副作用。

存活性探测 SHALL 为三态 fail-safe：以 `tmux list-sessions` / `has-session` 为存活性真相——命令成功且会话在 = alive；命令成功且会话不在 = dead；**命令失败（tmux server 不可达 / exec 错误）= unknown ≠ dead**，SHALL 保守保留任务，不判死、不回收。判死 SHALL 经去抖确认窗（持续 dead 超过确认窗才回收），单次探测失败 SHALL NOT 触发终局回收。

#### Scenario: List 只读不在热路径 shell out

- **WHEN** `BeforeModel` 渲染任务板调用 `List()`
- **THEN** `List()` SHALL 返回内存快照，SHALL NOT shell out tmux、SHALL NOT 变更任务状态、SHALL NOT 发 onSettle

#### Scenario: tmux 不可达不误杀活会话

- **GIVEN** 一个 running 任务，其 tmux 会话实际存活
- **WHEN** 探测因 tmux server 短暂不可达而命令失败
- **THEN** 探测 SHALL 判为 unknown，任务 SHALL 被保守保留，SHALL NOT 被回收或 KillSession

#### Scenario: 启动重连连带重建任务板

- **WHEN** 进程重启，存在仍在运行的 named resident tmux 会话 + 持久 task 元数据
- **THEN** `ReattachResidentSessions` SHALL 重建 tmux 追踪并连带重建对应 task 板条目，使其可被 resume/reattach

### Requirement: 僵尸回收经正常 settle 路径且看板与通知状态一致

经去抖确认为死的任务 SHALL 走正常 onSettle 路径回收为终态 failed，携带非 nil `Err`；event_bus SHALL 将其渲染为「✗ failed」，SHALL NOT 落入 default 分支渲染为「✓ completed」。retire SHALL 静默该任务的 detector/watch（避免重复 onSettle 通知），且 applyStatus SHALL 有终态守卫——completed/failed/cancelled 终态 SHALL NOT 被后续信号（如迟到的 SettleStable）复活为 stable。

#### Scenario: 僵尸回收渲染为 failed 且看板一致

- **WHEN** 一个 running 任务被去抖确认死并回收
- **THEN** onSettle SHALL 带 `Kind=SettleFailed` 且 `Err` 非 nil
- **AND** event_bus 渲染 SHALL 为「✗ ... failed」，看板状态 SHALL 与通知一致（不出现看板 failed / 通知 completed 的分叉）

#### Scenario: 终态不被迟到信号复活

- **GIVEN** 一个已被 retire 为 failed 的任务
- **WHEN** 其后到达一个 SettleStable 信号
- **THEN** applyStatus 终态守卫 SHALL 拒绝将 failed 改回 stable，任务 SHALL 保持终态并按 TTL prune

#### Scenario: retire 不产生重复通知

- **GIVEN** 一个被启动重连或后台 sweep retire 的任务
- **WHEN** 其 detector/watch 随后又 poll 到会话结束
- **THEN** SHALL NOT 再发第二条 onSettle（retire 已静默 detector）
