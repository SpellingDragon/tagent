# 设计：失败极性透传

> **评审尺**（本项目四判据，本设计的每个决策按此自检）：
> J1 职责层修复——修复点在职责缺失处而非症状显现处；J2 不变量优先——不变量守恒，规则追赶症状；J3 单层判定——同一语义只判一次；J4 消纳优先——让问题失去存在理由而非拦截形态。补丁唯一合法形态 = 通往纯化终态的脚手架 + 预埋拆除条件。

## Context

生产实证（09-30 wechat-bot）确认：结算通知链路本身无损（detector 通道容量 8、emitBackground 对终态 always emit、79 条通知零物理丢失），但失败极性在源头湮灭——`detectSessionState` 四处死亡分支一律 `SessionCompleted`，全仓零处读 `#{pane_dead_status}`；`SessionError` 唯一来源 `executor==nil` 不可达；`emitBackground` 对一切首个 `SettleStable` 发 `∞` 就绪通知。`SettleFailed`/`✗`/`★` 基建已存在且健康，缺的只是信号源。

关键约束：
- 框架自建会话恒带 `remain-on-exit on`，pane 死后会话保留——`pane_dead_status` 在 monitor 判定时刻必然可得（会话整体消失仅 tmux server 外力重启，属 unknown 退化路径）。
- monitor 层已消费 Mode 声明（`:616` resident 抑制 stable）；task 层零消费 `LifetimeOf`（全仓运行时消费点为零，仅持久化路径写入 Declarative）——[task/doc.go:9](agent/task/doc.go) 宣称「策略交给声明方」与现实脱节，本变更是第一个接通点。
- attention-budget-architecture（in-progress）L2 的 failed ★ 消费本变更接通的极性。

## Goals / Non-Goals

**Goals:**
- 事实层还债：退出码（含信号死）直达通知；异常路径（探测不可辨、kill 失败）不再默认成功极性。
- alive-detached 误用的止血（脚手架）与终态路线（信号矩阵）的确立。
- 空白载荷票据化、不吞码引导。

**Non-Goals:**
- 不改通知管线、投影、压缩——极性接通后 attention-budget 自然消费。
- 不改 `reconcileDetached`/`reconcileZombies`/`RetireOrphans` 回收极性（回收 ≠ 命令失败）。
- **本变更不执行信号矩阵坍缩**（oneshot 中间态信号移除、alive_detached 拆除）——由后续 `settle-signal-matrix` 变更执行；本变更交付其证据基础（消融覆盖矩阵）与脚手架。
- 不做 nohup/嵌套 tmux 检测（属 cognitive-asset-guard；且按 J4，本变更落地后走私的归因真空动机自然消退）。

## Decisions

**D1 退出码读取（J1：事实层职责补全）。** `TmuxExecutor.PaneDeadStatus(sessionID) (code int, known bool)`：`display-message -p '#{pane_dead_status}'`（与 `IsPaneDead` 同模式）；pane 活/读取失败 → `(0,false)`；pane 死 → `(code,true)`（tmux 约定信号死为负）。`detectSessionState` 死亡分支（输出捕获完成后）分流：`known && code != 0` → `SessionError`；否则维持 `SessionCompleted`。
*备选否决*：并入 `SessionAlive3`——三态探测职责膨胀，且 pane 状态与 session 存活是两个判定时刻的事实。

**D2 `SettleSignal` 显式 `ExitCode int` 字段（J2：可编程事实，不靠解析文本）。** 零值 0 与「成功退出 0」天然同值，存量信号无感；`SessionError` 的 Err 文本携带 `exited with code %d`；信号死呈现 `exit_code=-15 (signal)`。ExitCode **不进 Declarative**（restore 后由探测重新裁决）。

**D3 异常路径极性（J1：事实层停止说谎）。** probe UNKNOWN 超限、kill 三连败强拆 → `SessionError`（Err 注明 probe unresolvable / kill failed after retries，后者附逃逸提示）。「框架失明 ≠ 任务成功」。

**D4 job/service 分流——脚手架，附终态与拆除条件（J1/J4）。**
- **本期（脚手架）**：`emitBackground` 的 `SettleStable`/`SettleSuspect` 分支前置 `LifetimeOf == LifetimeService`：job 型不置 aliveDetached、不转移、不调 onSettle（零通知）；service 型现状逐句保持。`SettleWatch` 在判定之前（与生命周期无关）。`applyStatus` 不改（面板 stable/suspect 照常）。同步窗口路径不受影响。
- **终态（信号发射矩阵，本设计确立、后续变更执行）：**

| mode 声明 × 事实 | 进程死亡（带退出码） | 输出静默持续 N 秒 | TUI 无响应 |
|---|---|---|---|
| oneshot | 终态结算（唯一信号） | **无信号**（等死即可） | — |
| resident | 意外死亡通知 | 无信号（现状 `:616` 已如此） | — |
| interactive | 终态结算 | stable settle（resume 语义） | suspect（heartbeat/timeout） |

  矩阵下 `SettleStable` 仅 interactive 消费 → `alive_detached` 生产者归零 → 其全链路（状态机分支、∞ 通知、`DetachedAtMilli` 持久化、reconcileDetached 该路径、spec detachedAt 需求）成为可整体拆除的死代码。**拆除条件**：oneshot 中间态信号移除的防回归测试全绿（承接者测试，见消融矩阵右列）+ 一个观察周期的生产验证。
- **为什么本期只做脚手架**：坍缩影响面大（restore/面板/spec 多处），需独立变更评审；脚手架小步可回滚，且先行消除 job 型 ∞ 噪声（attention-budget 的即时收益）。

**D5 空白载荷降级（J2）。** 通知构造（`newTaskSettledEvent`/`newBatchRetiredSummaryEvent` 两处）：`TrimSpace(output)=="" && Err==nil` → 结果段为 ` →（无输出）` 单行票据。

**D6 退出码透传引导。** schema 的 `quiet_timeout`/`ttl` 描述追加：退出码由框架捕获（含信号死），命令应直接失败，不要 `; echo "EXIT=$?"` 吞码。

## 终态消融覆盖矩阵（`settle-signal-matrix` 拆除变更的证据基础）

机制考古（git -S + spec 核验注记）：SessionStable 出生于 quiet-vs-dead 翻转（「防误杀的标记」）；`StatusToSettle` 将其升格为信号（async-tool-event-fix Req5「stable 即 completed」的替身）；命令复杂化后降格为中间态但未摘除信号；`emitBackground` 再升格解释为「服务就绪」。每一代解决过真实问题（误杀→短命令体验→异步语义），但解法均为给旧机制叠加解释——语义漂移层积岩，且已自动拆除过外围补丁（决策 10.5 删 stale-detached wall）。

**逐功能承接表**（拆除前检查：每个出生问题必须有点名承接者）：

| 机制/功能 | 出生问题 | 承接者（终态下） | 判定 |
|---|---|---|---|
| SessionStable 状态（monitor 判定层） | 防误杀（静默+活着不杀） | **保留不动**——判定（不杀/降频轮询）与信号（上报）分离，拆除只动信号 | ✓ 判定层零变化 |
| SettleStable 信号·dense 内 stable≈completed（Req5） | 短命令快速同步返回 | dense 窗口内真完成（SessionCompleted+退出码）inline——50b17ec 已实现替身使命的接管 | ✓ 承认既成事实 |
| 长静默命令 dense 窗口体验 | 同上 | detach → ack（「稳定或完成后自动回写」） | ✓ 方向更正确 |
| dev server 输出稳定永不退 | 服务启动确认 | resident 现状即 detach+ack（`:616`）；起没起来由 watch/probe（显式声明通道）回答 | ✓ 对齐既有正确行为 |
| alive_detached 功能 a：转后台确认 | 「先应答」 | ack 消息本身 | ✓ |
| 功能 b：结果回写唤醒 | 「完成后通知」 | SettleCompleted always-emit（与 alive_detached 无关，保留） | ✓ |
| 功能 c：∞ 就绪通知 | 服务就绪观测 | 设计对象从未收到过（`:616` 使 resident 不可能触发），实际功能恒为零；就绪正解 = watch/probe | ✓ 零功能损失 |
| reconcileDetached 的 Alive probe 回收 | 会话死 board 残留 | reconcileZombies（不依赖 alive_detached） | ✓ |
| DetachedAtMilli 持久化 | detached 年龄用真值 | 对象随机制消亡，spec 需求随拆除删除 | ✓ 问题本体消亡 |

**结论：消融全覆盖**——三个机制的功能早已被 dense/detach 二段式、ack、watch/probe、zombie reconcile 完整承接，它们是纯语义噪音源（emitBackground 抢答 stable、dense 内假 inline）。拆除 PR 的验收点从本表右列导出防回归测试。

## Risks / Trade-offs

- [行为变更：下游把 ✓ 当「无异常」] → 远端约定与 `tests/` 契约同步；`TestActionTool_TmuxExitCode` 按行为变更流程改写。
- [job 静默后远端失去「还在跑」感知] → 看板可查；终态下该感知需求本身消亡（ack 已承诺回写）。
- [脚手架与终态的偏离风险] → 本节矩阵为唯一终态定义；脚手架实现若与矩阵假设冲突（如某场景确实需要 oneshot 中间态通知），停下回本设计重评，禁止就地加分支。
- [tmux 版本：pane_dead_status 需 ≥2.6] → 依赖面内；`known=false` 退化保持现状语义。
- [信号死负数语义] → 通知统一 `exit_code=-15 (signal)` 形态，文档化于 capability spec。

## Migration Plan

1. 落地顺序：executor 读取 → monitor 分流 → signal 字段 → task 层分流（脚手架）/通知过滤 → 结果结构/schema → 测试契约（同 PR 原子）。
2. 回滚：改动收敛于 tool/action 与信号构造，revert 单 commit 恢复旧语义；无持久化格式变更。
3. 终态触发：脚手架观察一个生产周期后，以本设计「终态消融覆盖矩阵」为证据基础启动 `settle-signal-matrix` 变更（oneshot 信号移除 + alive_detached 全链路拆除 + spec 需求删除）。

## Open Questions

- job 型 `SettleSuspect`（TUI timed_out 路径）是否同样静默——倾向随 D4 一致处理（矩阵中 oneshot 无 suspect）。
- `reconcileDetached` 终态坍缩后的 board 残留兜底是否全部由 reconcileZombies 覆盖——`settle-signal-matrix` 变更时验证。
