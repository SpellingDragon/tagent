> **本篇是当期契约**：代码注释 `// 契约: docs/wiki/agent/task-lifecycle.md#<锚>` 指向本页各节。实现面在 `runtime/task/`（`spec.go`/`lifecycle.go`/`admission.go`/`registry.go`/`manager.go`/`commit.go`/`handoff.go`/`logical.go`/`progress.go`/`restore.go`/`wrapper.go`/`cancel.go`/`hold.go`/`approval/`），执行现场的观测面在 `tool/exec/`。

# 任务层：记责任四问的账，不记执行现场

任务层管的是**任务本身**——受理、状态、通知、交付义务、恢复——不是它跑在哪个执行面上。责任模型统一停在四问：接受了什么工作？谁负责推进到明确结果？结果交给谁？哪一步仍未确认？tmux 退出判定、子 Agent 结束、远程恢复留给执行适配器。验收锚钉死：**接入一个新工具，适配器只需提供执行、取消及停止证据、结果正文恢复三面，宿主零补偿代码、核心零新生命周期状态**（`tests/integration/execution/new_tool_adapter_test.go`）。

<a id="status-machine"></a>
## 一、状态机：写权唯一，progress 是链上事实而非状态位
`Status` 枚举 `StatusUnknown`（零值）/`StatusAccepted`/`StatusRunning`/`StatusSettled`/`StatusFailed`/`StatusCancelled`/`StatusAwaitingApproval`，外加一枚墓碑 `statusProgressRetired`：序号保留（状态按 ordinal 存储），读回即 `Running`——progress 从此是链上事实，不再是一种状态。`Advance` 四类具名拒绝：目标为零值（`ErrStatusUnknown`）、目标为 `Accepted`（`ErrIllegalTransition`）、已有终结事实（`ErrTerminalState`——终结一次性，迟到的结算信号被拒而**不复活**已终结任务）、审批等待的进出违规；迁移先经 commit carrier 落成事实才可见，重复 `MutationID` 幂等、同 id 异内容按 `ErrMutationConflict` 拒，终结即关执行义务。`ProgressReported` 只读：全链扫描（**不按 round**——折叠会推进 round），要求 `status==Running` 且有 `NoticeProgress`，不写 status/revision/mutation/obligation。task id 随机化：`mintTaskID` = `task-` + 16 随机字节十六进制（`crypto/rand`），取不到随机源即 panic——不猜、不排序、不做可预测编号。

<a id="detach-suppression"></a>
## 二、脱离后的信号抑制：脱离态随执行现场外移，抑制判据留在这层
判据原样保留：任务一旦脱离前台，此后的稳定/可疑信号被抑制、不外发，**完成与失败永远外发**；抑制的动机可核查——否则一个长期静默的服务会让看板反复摆动、回收日志刷屏。当前承载点已换：`tool/exec/state.go` 的 `StatePhase`（`stable`/`suspect`/`exited`/`released`）是**对资源的观测，不是对工作的裁定**（stable 不是成功、半帧不是完成），`runtime/task/logical.go` 的 `LogicalStatus`（`watch`/`stable`/`suspect`/`expired`/`timed-out`）只标记、不改状态机、不自动处置。于是本层只剩一条抑制判据：**只有终结外发**（`settled`/`failed`/`cancelled`），`accepted`/`progress` 从不点火。

<a id="ttl-reclaim"></a>
## 三、统一 TTL：年龄是派生读数，不是处置动作
`Policy` 五个数：`WatchAfter`/`StableAfter`/`SuspectAfter`/`ExecutionTimeout`/`TTL`；三档具名策略 `default`（30s/2m/10m/30m/2h）、`responsive`（15s/1m/5m/15m/1h）、`patient`（2m/10m/30m/1h/4h），空名选 `default`，表外名字按 unknown 报告而不是读成缺省（拼错在解析处可见）。`TTL` 与 `ExecutionTimeout` 是 `classify` 的两条**派生**读数（`LogicalExpired`/`LogicalTimedOut`）——红线：逻辑状态=纯观测，只标记、不改状态机、不自动处置（不 kill、不中断），处置走 Governor 策略面。四条性质不变：与静默态无关（静默只影响标注）、从未脱离的任务也逃不过年龄墙、重入可续期但不覆盖显式声明、重建后的任务照样被读（没有移动戳的记录读作「在动」，绝不从本层没观察过的静默里分类）。默认寿命只有一个权威源：省略寿命的派生在**派生那一刻**从属主已提交的声明解析，执行工具不得自己再持一份可写默认值。

<a id="board-rendering"></a>
## 四、义务面：只报事实与剩余寿命，不做仲裁
`Obligations` 一次答出 `Count`/`Owner`/`Blocked`/`ExitReason`/`Logical`/`Open`，义务分两类：等工具（`ObligationAwaitingTool`）与等交付（`ObligationAwaitingDelivery`）。它**不替模型仲裁**：不自动取消、不自动续期、不替谁决定「继续等还是再派」。读数是现算的、不持久化、因而不参与上下文压缩——完整账本在链上，任何渲染都只是提示面；渲染本身归 `fact->view` 的 `FinalPresenter`，本层只交可核查的事实与剩余寿命，未声明 TTL 的任务取生效策略的那一档。

<a id="spawn-dedup-origin"></a>
## 五、派生入口：幂等键、回执只说 ok、零补偿
去重键由 `CallIdentity` 的 `InstanceID`/`InvocationID`/`DecisionID`/`CallID` 以 `\x00` 连成（`DedupKey`），空键即关闭去重；`AdmissionKind` 三值 `Fresh`/`Dedup`/`Rejected`，而回执正文恒为 `"ok"`（`AdmissionReceiptText`）——**task id、状态词、结算承诺永不进模型**。调用身份经 ctx 的 `CallFrame` 传递（`WithCallFrame`/`CallFrameFrom` 是唯一 typed accessor），绝不暂存在共享实例状态上。取消是请求不是凭证：`RequestCancel` 只记请求并回 `CancelReceipt{Requested}`，终结要等执行器报 `StopEvidence{Stopped}`，未确认停止不动状态。来源戳随声明走（`Spec.TriggerSource`，委派为 `event.LineageDelegation`），本层只透传不解读。

<a id="finalize-lineage"></a>
## 六、终结与谱系：终结是闭表，回收路径不得自造口径
终结词汇闭表 `settled`/`failed`/`cancelled`（`terminalSettleKinds`）。所有回收与退役路径都走同一次终结动作外发失败结论，不允许各路径自造通知口径。谱系禁令：**回收/退役外发的结算信号不得继承任务原有的触发谱系**——否则下游会把一次回收读成一次用户发起的正常答复。终局通知的兜底是补交接：`Handoff` 只迁移此后的通知，已创建的通知保持其 envelope 冻结的路由（`Target`/`Source`/`TargetInstance` 三者冻结），未被应答的那条只把交付义务改记为已交接；接收实例已关闭时按 `ErrInstanceClosed` 保留——不丢弃、不改投、不投进后来复用同一地址的实例。

<a id="restore-rebuild"></a>
## 七、从事实重建：不重启观察、幂等不覆盖
`TaskRecord{Spec,Fact}`：只有 admission 那条链携带 `Spec`，其余只有事实。重建沿用 id、声明、派生时刻与状态，并**不重启观察**（窗口标记为已关闭），终态判定与 TTL 读数照常生效。两条性质：**幂等且不覆盖**（同 id 已在表内直接返回既有任务，索引键只在无人占用时登记；空 id 与 nil 管理器安全），**终态必须存活**（否则消费者重启后外部读到的「已完成」凭空消失）。`OwnerBinding` 四条按名拒绝：目标已移除 `ErrTargetRemoved`、无所属绑定 `ErrNoOwnerBinding`、链断 `ErrChainBroken`、声明缺失 `ErrSpecMissing`——不猜替代、不静默回落。

<a id="resume-states"></a>
## 八、重入的合法来源态：同一个 task id，重新走一遍标准回合
`Resume` 把一条已声明输入喂进既有任务，在同一 task id 下推进到 `Running` 并新开一条等工具义务；重复声明同一输入（同 `input.Key`）返回已存事实，不开第二条义务。合法来源态由当前状态决定，不由「看起来忙不忙」决定：`Running` 非法（`ErrRoundInFlight`，有一轮在途，只能等）、`Cancelled` 非法（`ErrResumeCancelled`，应重新派生）、`AwaitingApproval` 非法（先给裁决）、`Accepted`/`Settled`/`Failed` 合法（还没开跑，或上一轮已有明确结果；按轮执行的执行器重入即自然续接）。判定与认领在该任务的锁下先于执行面发生，并行的重入单点胜出，落败方被告知任务正在运行。

## 已知缺口与演进方向
| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| 义务面没有「还有多少条没显示」的信号 | `Obligations` 全量返回，截断只发生在渲染侧 | 渲染侧报截断计数，模型才知道自己看的不是全集 |
| 没有探活器时僵尸只能靠 TTL 读数兜底 | 逻辑状态只标记不处置是有意为之（避免误杀），代价是静默任务留在义务面上 | 一次「无探活部署」的实测说明，写清这层兜底的代价 |
| 续期与显式 TTL 的优先级只在测试里钉住 | 测试是唯一凭据 | 面向宿主的一页说明，含热更新到达与未到达两腿 |
