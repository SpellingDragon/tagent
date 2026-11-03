# 地址消费技术决策

## D1 唯一驱动（接受 W0-A01 F1/F2）

所有正式输入都经地址 Consumer。Loop 只保留单决策共用内核，不再作为公共回合驱动面。Run 在没有 Host 挂载集合时使用 Loop 自身的 resident 集合，不保留低级直驱模式。被否：保留 Run 直驱兼容、send 按 busy 分流；两者都会让同一事实存在两个消费者。

## D2 答案归属与可恢复结果（接受 F4/F7，答案设计审后 BREAKING）

公共 Turn 只保留稳定结果字段；删除含活 Binding/Request 的 Decisions []Decision，改为 DecisionCount。决策详情留在执行事实与既有观察面，不为恢复序列化活对象。agent_output 在同一关闭提交中记录实际 input_events、Terminal、Failure、FinalRound、Trigger 与 DecisionCount；缺失/未知终态不得默认 completed。恢复只读事实链，不重跑、不再 delivery/After/notice 迁移。只有完整可恢复的闭合才可淘汰缓存；无 output 的内部失败不伪造闭合，abandon 后重启允许新段回答。

输入 key 与闭合输出的关系来自事实链，进程内 map 仅是缓存。cache miss 必须读链重建或继续等待真实未消费输入；不得因淘汰改等下一段或邻段答案。同段多个输入的等待者共享同一不可变结果。

## D3 地址所有权（设计审已通过，实现待末轮放行）

Loop.instances 仅是同址唯一注册，Consumers.live 仅是 set 生命周期索引。此前的 lifecycle 锁方案被否决：Stop 持锁 join 会与 Consumer 内 Wake 死锁。

裁定的新边界：Loop 只保存一个 current Consumers；SetConsumers 仅允许 nil→非 nil 的一次性装配并返回实际集合，之后 nil/replacement 均不得擦除 current；停止集合永久留作 tombstone。WakeAddress、SubmitDelegated、Backflow 都读取同一 current，existing-instance 命中也检查 stopped。Stop 在不持 Loop/生命周期锁的情况下 join，residentLoops 与 lifecycle 两套状态删除。不得新增公共 Owner/Registry/锁状态类型，也不得在持有任何状态锁时等待 Consumer.done。严格一次性后不做陈旧集合重试。Backflow 停止后的 Consume 禁止并保留 owed，归 02；MessageSender 重入 Host.Close 必须具名拒绝或有界返回，归 03。

设计审与工具内 Wake/Stop、SubmitDelegated/SetConsumers 同类公共交错均已通过；停线历史保留在一级台账，当前实现仅在末轮 frontier 空后放行。

## D4 执行态与结果（接受 F5）

segment 是私有执行态，Turn 是闭合结果，SegmentAnswer 只是等待返回包。不得为三者再建平行持久账。Consumer.Drive 的命名在删除 Loop.Drive 后由 04 域决定，先不做无收益改名。

## D5 重试边界

显式 Drive 一个已命名事实时，是否可再次驱动由“该输入是否已有闭合答案”判断，而不是由永久 taken 标记判断。failed/cancelled 通知的业务重试边界待 02 域定稿；本域不增加 retry ledger。

## D6 规格唯一性（接受 F6）

地址消费只认 consumer-loop-convergence。persistent-event-loop 的旧 RunFlow/mailbox/StopLoop 条款由 04 域迁移或删除，禁止 superseded 注记下继续保留并行 Requirements。

## D7 Resting 的 pass 窗口（m-5）

`Resting` 的判据加 `pendingPass`（已入队/在跑的 pass 数）：`Wake()` 真正入队时 +1（channel 满的合并 Wake 不重复计），mount 的 boot pass +1（run 因 halted 跳过须退还），consume 入口 −1，`drainWake` 归零；`Resting=busy||flying||pendingPass>0||len(wake)>0`。禁止用轮询/定时器/容量窗口补窗口（红线）。锚=`TestRestingCoversTheWakeTakenBeforeThePass` + 既有 pass/关闭锚；该计数不持久、不跨进程。

## D8 awaited 生命周期（m-6，待 lead 裁定 D-W6-A）

`awaited` 现只 Store 无 Delete，键不含 round；`TaskConsumed` 被 `fireNoticeTurn` 读作“已由飞行段结清”，`TaskOrphaned/TaskAwaited` 亦被读作可驱动。清理不得早于对应 terminal notice 的消费，否则同一 notice 可二次驱动；不得引入轮询/定时/容量窗口（红线）。候选删除点：(a) 本地址把 terminal notice 收作 driving material 时（pullDriving）；(b) host 结算 notice turn 后（需 Consumer 公共方法）。先用红锚证实/证伪“旧轮 consumed 抑制后续轮 notice”，再定 (a)/(b) 与键是否加 round；未裁定前不派发。

## 风险与回退

- 统一 Run 入口可能改变低层调用时序 → 保留同步等待结果的外部语义，以公共 Run 测试证明。
- 链上答案关联若缺少输入键 → 扩展现有 agent_output 元数据，不新增事实族；先审计现有写入与恢复。
- 修复引入同址双写 → 停止扩域，回到唯一驱动探针；同一机制三次修复仍失败则停线。
