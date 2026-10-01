# 失败极性透传：退出码直达结算通知，终结乐观结算倾向

## Why

远端生产实证（09-30 wechat-bot，7 小时窗口）：79 条 `[task settled]` 通知中失败极性（✗ failed）为 **0 条**，而同期真实失败至少 4 起（pip EXIT=1、两次下载死亡、karaoke 两连崩）——远端观察到的「异步执行静默失败、疑似未通知」即源于此。静态审计定位根因不是事件管线丢通知（通知每次都发），而是**失败极性在源头被系统性湮灭**：

1. `detectSessionState` 四处进程死亡分支一律 `return SessionCompleted`，全仓**零处读取** tmux 原生 `#{pane_dead_status}`（退出码）；`SessionError` 唯一产生条件是 `executor==nil`（启动配置错误），生产不可达，`settle.go` 的 `error → SettleCompleted+Err` 注入路径因此是死代码。命令失败、信号死、探测失明一律报成功——「乐观结算倾向」。
2. `emitBackground` 对第一个 `SettleStable` 无条件转 alive-detached 并发 `∞` 就绪通知。考古确认该机制为**语义漂移层积岩**：SessionStable 出生是「防误杀的标记」（quiet-vs-dead 翻转），升格为信号（Req5「stable 即 completed」替身），命令复杂化后降格为中间态但未摘除，再被升格解释为「服务就绪」——而 monitor 层 `:616` 对 resident 抑制 stable，**∞ 通知的设计对象从未收到过它，触发者全部是 oneshot job**（`sleep 180`、pip 安装实测收到「就绪」）。该机制承载的三个功能（转后台确认/结果回写/就绪观测）已分别被 ack、SettleCompleted always-emit、watch/probe 完整承接。

后果：`SettleFailed`/`✗`/`★` 整套失败极性基建生产零触发；attention-budget-architecture L2「failed ★」约定依赖本变更接通极性。

## What Changes

**事实层还债（终态，本变更交付）：**
- 退出码读取：`TmuxExecutor.PaneDeadStatus`；`detectSessionState` 死亡分支按退出码分流——非零/信号死 → `SessionError`（携带退出码），零 → `SessionCompleted`。
- 异常路径极性：probe 不可辨超限、kill 三连败强拆从 `SessionCompleted` 改为失败极性（「框架失明 ≠ 任务成功」）。
- `SettleSignal.ExitCode` 字段 + action 结果 `exit_code` + 通知文本 `exit_code=N` 透传。
- 空白载荷通知降级为单行票据。
- 工具 schema 引导退出码透传（不 `echo EXIT=$?` 吞码）。

**脚手架（通往终态的过渡，预埋拆除条件）：**
- `emitBackground` 按 `LifetimeOf` 分流：job 型 stable/suspect 不转 alive-detached、不发 ∞。**这是脚手架而非终态**：终态是信号发射矩阵（见 design「终态」节）——oneshot 不发射中间态信号，`SettleStable` 仅 interactive 消费，`alive_detached` 全链路因生产者归零而可整体拆除。本变更只做 task 层最小分流（小步、可回滚、零 spec 破坏），矩阵坍缩由后续拆除变更（`settle-signal-matrix`）执行。

**测试契约更新**：`TestActionTool_TmuxExitCode`（exit 42 → 失败极性）、job 型转 alive-detached 的 fixture 族按行为变更流程改写。

## Capabilities

### New Capabilities

- `failure-polarity`: 失败极性的判定与透传契约——`pane_dead_status` 读取、进程死亡/探测不可辨/kill 失败三类路径的极性规则、`exit_code` 字段契约、通知载荷最小信息量。

### Modified Capabilities

- `async-task-execution`: 「服务型任务转 alive-detached」需求收紧为按 `LifetimeOf` 分流（job 不转不发就绪通知；**方向性标注**：终态下该需求随 alive_detached 拆除而消亡，detachedAt 持久化部分届时删除）；「task_settled 为通知类 input 事件」需求增加失败极性表达（✗ 与 `exit_code`）。

## Impact

- **代码落点**：`tool/action/tmux_executor.go`、`tmux_monitor.go`、`settle.go`、`action_tool.go`、`agent/task/task_manager.go`（emitBackground 分流——脚手架）、`agent/event_bus.go`（通知 exit_code 注入）、`resources/prompts/action_tool_desc.md`。
- **行为变更（需评审）**：非零退出的通知极性 `✓→✗`；job 型不再产生 ∞ 通知；下游（远端约定、`tests/` 契约、attention-budget 1.8 任务）同步。
- **依赖关系**：attention-budget-architecture L2 failed ★ 依赖本变更（已在其 proposal/tasks 标注）；本变更不依赖它。
- **架构方向**：本变更的脚手架拆除条件 =「oneshot 中间态信号移除验证通过」，触发后启动 `settle-signal-matrix` 拆除变更（证据基础：design 的消融覆盖矩阵）。
