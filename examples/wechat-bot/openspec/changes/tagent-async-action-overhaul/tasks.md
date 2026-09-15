# tasks — tagent-async-action-overhaul

> 执行者：主 agent（2026-09-11 凌晨）。proposal.md 为规格源。
> 快照保护：改动前 .bak-overhaul/（6 文件）；git 工作树含此前 quiet_timeout 未提交改动。

## A — 三刀修复（结算语义）

- [x] A1 core: tmux_monitor.go detectSessionState — 删除 stable→Completed 误判（L655-668），alive+quiet 一律 SessionStable；真实退出走 pane-dead 分支（原样保留）
- [x] A1 fakedead: 非交互会话静默超阈值不再注入 heartbeat（stdin 污染 + 恒 no_response 必误杀）；改为 pane/进程状态判 liveness：死→Completed，活+显式 quiet_timeout→FakeDead 硬超时，活+默认→保持 Stable 不杀
- [x] A2 卡片: settleMarkerAndStatus 已有 ∞/⚠/✓/✗ 四态（无需改）——今晚显示 ✓ 的根因是 A1 的 Completed 误判，A1 修复后 ∞ alive-detached 自然生效；wechat-bot 渲染层无需改动
- [x] A3 pipe 保全: KillSession 不再 os.Remove——pipe 文件 rename 到 /tmp/tagent-archived-pipes/ 留现场，rename 失败降级删除

## B — 模式与原语

- [x] B1 mode 字段: SessionMode(ModeOneshot/Resident/Interactive) + TmuxCreateOptions.Mode + TmuxSession.Mode + ActionArgs.mode + Declaration schema(mode enum) + Call 校验（TUI 冲突拒绝）+ startSession 传递
- [x] B1 monitor: resident 会话静默 → SessionRunning（不触发任何 settle/kill），意外死亡仍经 pane-dead 分支结算
- [x] B2 命名注册表: name 参数 + 落盘 JSON + 去重/重查（TestNamedSession 4 用例全绿；validSessionName a-zA-Z0-9- ≤64）
- [x] B3 peek/send/stop 原语暴露（callSessionOp 分支 L755-881：peek 游标+截断重置+tail 裁剪、send TUI 拒绝、stop SIGTERM→grace→kill 阶梯）

## C — 事件订阅

- [x] C1 pattern watch: spawn 声明 watch 正则 → monitor 刷新喂 OnWatchOutput → 命中唤醒（快照差分模型抗滚动、5s 合并窗口、累计计数；TestDetector_Watch 2 用例 + task 侧 NoStateChange 全绿）
- [x] C2 liveness probe: probe/probe_interval/probe_failures 参数，probeLoop goroutine 连败 N 次 EmitProbeResult 报警（闩锁防重发+成功重置；TestEmitProbeResult 2 用例全绿；默认 30s×3 次）

## D — 生命周期加固

- [x] D1 跨重启重挂（resident_recovery.go：ResidentMeta JSON 落盘（mode/watch/probe）→ ReattachResidentSessions 启动对账（tmux list n-* ∩ meta）→ 重建 detector+monitor+probeLoop；pipe 无需接回——pipe-pane 在 tmux server 内持续追加，同路径直接续读；TestSaveResidentMeta/TestReattach 全绿）
- [x] D2 resident TTL 清扫 + 并发上限（SweepStaleResidents：24h TTL 未被任何重启收养→杀会话+清记录；maxResidentSessions=16 spawn 拒绝；nil-executor 鲁棒（测试暴露后修复）；TestSweepStale/TestCanSpawn 全绿）
- [x] D3 pipe 文件 10MB 轮转（rotatePipeFile copytruncate：拷 .1 后 truncate 活文件——O_APPEND fd 续写不丢，rename 会让 fd 写进旧 inode 的坑已避开；peek 游标经既有 len<cursor 规则自适应；TestRotatePipeFile 全绿）

## E — 验证与发布

- [x] 前置: 读 tests/async_* 三契约（e2e/routing/task）+ agent/task 契约测试清单
- [x] go build ./... 全绿
- [x] go vet ./...
- [x] go test ./tool/action/ ./agent/task/ 全绿（含既有 D4/ack/settle 契约回归）
- [x] examples/wechat-bot 构建成功（55MB 二进制，独立 module）
- [ ] restart-maintenance.sh 保险链重启 + healthz 探活闭环
- [ ] 端到端实证: sleep 90+echo 形状任务 → ack → ∞ alive-detached@60s → 进程仍活 → 真实退出后 ✓ completed

## 决策记录

- A1 收敛为外科手术：任务层 alive-detached（D4）机制本就完整（含契约测试），根因是 monitor 把 alive+stable 映射成 Completed 使 D4 成为死代码——修 monitor 一处即激活全链
- stable→Stable 修正后，残留风险=依赖旧行为的测试用例（lifecycle/monitor_scenario/quiet_timeout），跑测后逐个分诊
- A1 后测试契约翻转（已实证）：全量回归 11 红灯全部为"测试钉住旧误判语义"而非实现缺陷，分诊两类——① 6 个 heartbeat→FakeDead/FakeAlive/restart 链路用例翻 fixture 为 IsInteractive:true（该链对交互会话仍是活代码，tmux_monitor.go 原样保留）；② 3 个"非交互+静默→Completed" + 2 个 QuietTimeout 默认等价用例翻断言为新语义（alive+quiet→Stable 不自动完成；默认 fake 路径仅限交互会话，显式 quiet_timeout 硬超时仍击杀）。plan 侧已抽读源码核实：tmux_monitor_test.go L213-219 断言 SessionStable（no auto-complete 注释）、L504/968/1002/1033/1099 IsInteractive=true、lifecycle_test.go L189、quiet_timeout_test.go L123-124（ConcurrentIsolation 双会话 IsInteractive=true）——与报账的 13 处编辑吻合。E 组勾选仍待绿灯实证（/tmp 日志不在 plan 只读范围内，以主 agent 下一轮报账为准）
- SettleStable 任务层语义不动（首次通报+后续抑制），与 monitor 修复天然契合
- E 组四项（build/vet/test/wechat-bot）核证通过（2026-09-11）：plan 侧独立抽读源码——tmux_monitor.go L640-674 非交互分支无 stable→Completed 残留（L602=进程真死、L661=pane-dead 真退、L695=heartbeat ok）、L504 + lifecycle_test.go L189 IsInteractive 翻转就位、L213-219 断言 SessionStable 带 no auto-complete 注释——与报账吻合；/tmp/action_test_v3.log 绝对路径在 plan 沙箱外，以源码实证 + 报账 EXIT=0 / grep -c FAIL=0 为准。流程滑点 2 次如实登记（setsid 重跑、管道退出码吞没），后续一律文件终态为准
- 剩余 2 项验收：restart-maintenance.sh 保险链重启 + healthz 探活闭环（脚本路径探明中）；端到端实证 sleep 90 形状任务 → ack → ∞ alive-detached@60s → 进程仍活 → 真实退出 ✓ completed。两项全 [x] 后方可 archive

- 2026-09-11 03:5x C1/C2 实现记录: watch 走快照差分（每刷新对全 pane 计数、与上次求差、负差钳 0——pane 滚动只会丢计数不会错切内容）；probe 用闩锁（失败报一次，成功重置）。全量回归 /tmp/full_regress.log EXIT=0（action 34.4s + task 1.25s）。
- 2026-09-11 plan 侧 B/C 组收官独立核验（全部通过，带行号实证）：B3 callSessionOp@action_tool.go L756-881（peek 游标+截断重置 L783-790+tail 裁剪 L803-807；stop SIGTERM→grace 200ms 轮询→kill-session 阶梯）；C1 SetWatch/OnWatchOutput@settle.go L209/L230（快照差分、负差钳 0、5s 合并窗口、累计计数）+ 接线 L390/L412 + task 层双分支 task_manager.go L435（emitBackground 纯通知 never change lifecycle）/L471（applyStatus keep as-is）+ watch_test.go 2 用例 + watch_notify_test.go 1 用例；C2 startProbeLoop L886+（默认 30s×3）+ EmitProbeResult 闩锁 L274-297 + probe_test.go 2 用例；B2 validSessionName L643 + NamedSessionName tmux_executor.go L350 + 重复 spawn 拒绝 L193-200 + named_session_test.go 4 用例。测试文件/用例数与报账逐项吻合。
- 两处偏差登记（不阻塞 D 组，需 D1 设计时消化）：① B2 "落盘 JSON 注册表"实际实现为确定性命名 n-<name> + tmux has-session 重查（无独立 JSON 落盘）——D1 跨重启对账须走 `tmux list-sessions` 列举 n-* 前缀会话而非读注册表文件，pipe 文件名（/tmp/tagent-pipe-n-<name>.log）可作附加对账源；② op=send 的 TUI 拒绝目前仅存在于 schema 描述（L215 "refused for TUI"），opSend 运行时无 mode/is_tui 反查（spawn 时 mode×is_tui 冲突校验 L298-299 与 resumeClosure 拒绝 L450-451 在位）——对已 spawn 的 TUI 会话 op=send 无护栏，小修可解（monitor 会话表 IsTUI 可查）。
- /tmp/full_regress.log 绝对路径在 plan 沙箱外（沿先例）：以源码级测试文件实证 + 报账 EXIT=0 为准。

- 2026-09-11 04:2x D 组收官: D1 恢复对账（对账源 = tmux list-sessions n-* 枚举 + /tmp/tagent-resident-meta/sess-<id>.json 元数据；plan 审计偏差①的"确定性命名无 JSON"已由新增元数据档补齐）；plan 偏差② send-TUI 运行时护栏已补（opSend 反查 monitor IsTUI，L~857）。全量回归 /tmp/full_regress_d.log VET=0 TEST_EXIT=0（action 34.3s + task 1.3s，含 D 组 6 新测试）。
