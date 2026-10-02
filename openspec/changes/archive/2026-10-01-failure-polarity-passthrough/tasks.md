# 任务：失败极性透传（细化版）

> **跑偏防护总则**——实现中遇下列任一情况，停下回 design.md 重评，不得现场发挥：
> ① 需要改 event_bus 通知管线的既有语义（spill/有界化/单行轨迹形态）——本变更只**追加** `exit_code` 与极性分流，不重构构造器；
> ② 需要新增配置旋钮——零新配置是硬约束；
> ③ 需要把 ExitCode 写入 Declarative/持久化——设计明确不持久化（restore 后由探测重新裁决）；
> ④ 需要动 `reconcileDetached`/`reconcileZombies`/`RetireOrphans`/`applyStatus`——Non-Goal 红线；
> ⑤ 需要改 ApprovalManager/spawnGate——与本变更无关。
>
> **任务 ↔ 决策 ↔ 场景对照**：1.x↔D1↔「进程死亡按退出码分流极性」；2.x↔D3↔「探测不可辨超限/kill 失败强拆」；3.x↔D2↔「结算信号与通知携带 exit_code」；4.x↔D4↔async-task-execution delta「服务型任务转 alive-detached」；5.x↔D5↔「纯空白载荷通知降级」；6.x↔D6↔「工具描述引导退出码透传」。

## 0. 基线与红-绿流程锚

- [x] 0.1 基线确认：`go test ./tool/action/ ./agent/task/ ./agent/ -short -count=1` 全绿，记录将被改契约的测试清单：`TestActionTool_TmuxExitCode`（tmux_executor_test.go:269）、`TestNonTUI_QuietAlive_ExplicitQuietTimeout_HardTimeoutKill`、`TestSettleWatch_NoStateChange`、`spawnAliveDetachedTask` fixture 族（task_manager_test.go:936 一带）、`settle_test.go` 的 StatusToSettle 断言
- [x] 0.2 先写「红」测试族（此时失败=正确）：①exit 42 → ✗+exit_code=42 ②exit 0 → ✓（现状）③信号死 → ✗+`exit_code=-N (signal)` ④probe 超限 → ✗ ⑤kill 三连败 → ✗+逃逸提示 ⑥job（LifetimeOf=job）stable/suspect 零通知零转移 ⑦service stable 一次性 ∞（现状保持）⑧`␤×25` 载荷 → 单行票据 ⑨结果 JSON 含 exit_code 字段——每个测试注明对应 spec Scenario 名

## 1. executor 退出码读取（D1）

- [x] 1.1 `TmuxExecutor.PaneDeadStatus(sessionID) (code int, known bool)`：`display-message -p -t <id> '#{pane_dead_status}'`（与 IsPaneDead 同 buildTmuxCommand 模式，tmux_executor.go:354 邻位）；pane 活/命令 err → `(0,false)`；pane 死 → `(code,true)`（tmux 约定信号死为负）
- [x] 1.2 单测（真 tmux，skip short）：三种会话各验一次——`exit 0`→(0,true)、`exit 42`→(42,true)、`kill -TERM` 会话→(负数,true)、活会话→(0,false)

## 2. monitor 极性分流（D1/D3）

- [x] 2.1 `detectSessionState` 主死亡分支（tmux_monitor.go:570-611，`!processExists || isPaneDead` 且输出捕获完成后）：调 `PaneDeadStatus`——`known && code != 0` → `SessionError`；`code == 0 || !known` → `SessionCompleted`（现状）
- [x] 2.2 fake-dead 内两处 pane-dead 出口（:637-641 非 interactive 分支、:663-667 heartbeat 后分支）同样接入分流
- [x] 2.3 probe UNKNOWN 超限分支（:530-543）：`return SessionCompleted` → `return SessionError`（Err 语义在 detector 层拼「probe unresolvable」，monitor 侧仅改状态）
- [x] 2.4 `handleFakeDead` 三连败 force remove：session.Status 置 `SessionError`（checkSession 的 remove switch 已含 error 分支，确认 shouldRemove 路径不变）
- 红线：**不动** stableWindow/fakeDeadThreshold/stable 判定阈值、watch/probe 定时器、`SessionAlive3` 三态语义

## 3. 信号与通知透传（D2）

- [x] 3.1 `task.SettleSignal`（task_manager.go:97）增加 `ExitCode int`——零值 0 与「成功退出 0」天然同值，存量信号无需改；未知哨兵 -1 仅在注释与文档声明（无写入点则不出现）
- [x] 3.2 **退出码传递选型（本组暗礁，先做再继续）**：**pull 模式已采用且验证通过**——`TmuxSettleDetector` 加 `SetPaneStatusReader`（三构造点各接 `ct.tmuxExecutor.PaneDeadStatus`），`OnStateChange` 收 SessionError 时在 `reap()` **之前**拉取，known&&code!=0→填 ExitCode+Err「exited with code N」，否则「exit status unresolvable」。`TestDetectorPull_*` 绿，未触发回退 push 的条件
- [x] 3.3 `ActionToolResult` 加 `ExitCode int json:"exit_code,omitempty"`（omitempty 天然省略 code 0 成功）；`buildResultFromSignal` 填 `sig.ExitCode`
- [x] 3.4 通知注入（agent/event_bus.go **两处都要**）：确认 `settleMarkerAndStatus` 对 `Err!=nil` 已映射 ✗/failed（基建健康，未改）；两处构造器加 `formatExitCode` 追加。**选型细化**：追加条件取 `ExitCode != 0`（非「Err!=nil 或 ExitCode!=0」）——不可辨路径 code=0，若按原条件会误报 `exit_code=0`（暗示干净退出），故失败极性由 Err 承载 ✗、码缺失则不追加（`TestSettleNotify_UnresolvableNoMisleadingCode` 钉住）；信号死渲染 `exit_code=-N (signal)`。只追加字段，未改行形态/spill/有界化
- 红线：ExitCode 不进 Declarative；批量回收行格式除追加字段外逐字节保持

## 4. alive-detached 按 Lifetime 分流（D4 · **脚手架**——终态见 design「信号发射矩阵」，坍缩归 settle-signal-matrix 变更）

- [x] 4.1 `emitBackground`（task_manager.go:828）`SettleStable`/`SettleSuspect` 分支前置 `LifetimeOf(task.Spec) == LifetimeService` 判定：job 型 → **不置** `task.aliveDetached`、**不转移**状态、解锁后**直接 return**（不调 onSettle——零通知）；service 型 → 现状逐句保持。**脚手架约束：若实现中发现某场景确需 oneshot 中间态通知，停下回 design 矩阵重评，禁止就地加分支**
- [x] 4.2 边界确认（只改 emitBackground）：同步窗口路径（firstSettle，Spawn 内联结算）不受分流影响；`SettleWatch` 分支在 Lifetime 判定**之前**（watch 通知与生命周期类无关）；`applyStatus` 不改（面板 stable/suspect 照常）
- [x] 4.3 测试契约更新：`spawnAliveDetachedTask` fixture 的 spec 加 `Lifetime: LifetimeService`（或 Kind 改 generic 走默认 service 推断）；`TestSettleWatch_NoStateChange` 等按新语义改断言；新增 job 型（Kind: command / subagent）stable 后 `List()` 可见 TaskStable 但 onSettle 零调用
- 红线：job 型 suspect 同批静默（design Open Question 的落地倾向，若实现中发现远端有依赖证据则停下上报）

## 5. 空白载荷降级（D5）

- [x] 5.1 `newTaskSettledEvent` 与 `newBatchRetiredSummaryEvent`：`strings.TrimSpace(sig.Output) == "" && sig.Err == nil` 时结果段替换为 ` →（无输出）`（跳过 spill 判定——空白必不超限，短路即可）；有 Err 时照旧输出 Err 行
- [x] 5.2 测试：`␤×25` 输入 → 单行票据断言（无空白正文）；带 Err + 空输出 → Err 行保留

## 6. 模型面引导（D6）

- [x] 6.1 action_tool.go Declaration 的 `quiet_timeout`/`ttl` 描述（:305/:309）各追加一句：退出码由框架捕获（含信号死），命令应直接失败，不要以 `; echo "EXIT=$?"` 吞码；`TestDeclarationExposesTTL` 族不回归
- 红线：不新增参数、不改既有参数枚举与校验

## 7. 红转绿与全量回归

- [x] 7.1 改写 `TestActionTool_TmuxExitCode`：exit 42 → 断言 status=error（失败极性）+ `exit_code=42` + before_error 保留（真 tmux 跑通过，实测 `status="error" exit_code=42`）
- [x] 7.2 红测试族全绿（`failure_polarity_test.go` detector pull×4 + monitor 分流×4 + handleFakeDead×2；`failure_polarity_notify_test.go` event_bus exit_code/blank×8；`task_manager_test.go` job/service 分流×2；executor `TestPaneDeadStatus`）；存量测试按行为变更更新（spawnAliveDetachedTask/AliveDetached×3/ReconcileDetached×2/BindDetector/Resume_IllegalStates 加 `LifetimeService`；KillRetry force-remove 断言改 SessionError；ProbeUnknownGate_ConsecutiveLimit 改失败极性）
- [x] 7.3 `go build ./...` ✓；`go vet ./...` ✓；`go test ./tool/action/ ./agent/task/ ./agent/governance/ -race` ✓；触达 6 包（含 event/根包）`-p 1 -short` 全绿。**注**：`TestCommandParsing` 在整包无 `-short` 并发跑偶发 flake（真 tmux server 争用，单跑绿；命令 exit 0 不受极性改动影响）
- [x] 7.4 远端约定通告（**已发送**，并入 cg 5.4 同一封 msg_m9Lw3VM…，第二节即为极性新语义/job不发∞/不吞码/exit_code=-N 解读）：✗/exit_code 新语义、job 不再发 ∞、「不要吞码」约定、收到 `exit_code=-N (signal)` 的解读
  > 草稿已成文（与 cg 5.4 合并一封）：`../cognitive-asset-guard/delivery-email-draft.md`；收件人 weiyepeng@agent.qq.com 已授权并送达。内容：✗/exit_code 新语义、job 不再发 ∞、不要吞码约定、exit_code=-N (signal) 解读
- [x] 7.5 确认 attention-budget-architecture tasks 1.8 依赖标注仍准确：实现严守 design 选型（pull 模式，`SetPaneStatusReader`，未回退 push、未偏离）→ 该条无需更新；且 fp 落地即 fulfil 该依赖（命令非零退出现已产生失败极性，★ 可消费）
- [ ] 7.6 脚手架观察期任务（**未来·生产验证**）：上线后记录一个生产周期观察结论（job 型 ∞ 消失后行为、有无 oneshot 中间态通知真实需求），作为 `settle-signal-matrix` 拆除变更启动输入——非本次可实现，留待部署后
