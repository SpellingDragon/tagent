# 关闭生命周期技术决策

## D1 关闭不得选择业务终局

Close 只停止接纳、推进既有义务并报告余量。AwaitingApproval 即使在重启后没有旧 HoldBook，也保持原状态并 Cut；需要取消时必须走显式业务取消和审批 answer/clearance，不得由 Close 代替。

## D2 单协调器与单预算

Host 负责资源顺序，Run 提供共享判据/阶段钩子；独立 Run.Close 复用同一协调逻辑。所有 Stop/join 都必须可选择 ctx/deadline。到点返回 Cut，并保留仍有 writer 的资源所有权和具名报告。被否：在 Quiesce 有 timer 但 Close 随后无界 join。

## D3 尝试结果与资源状态分离

CloseOutcome 描述一次尝试；内部 open/closing/closed 表达资源生命周期，不新增公共状态名。并发调用共享当前尝试；Cut 允许后续重试，最终 closed 后才幂等返回同一结果。Broken 不因输入校验产生。

## D4 完整静止读数

持久读数包含工作、通知、未闭合输入；进程内 writer/landing 必须与它们在同一关闭快照中判定。failed/cancelled 是否有明确 completion 依 02 的持久消费结果语义，不能只看 agent_output 类型猜测。输入已提交但 output/commit 失败必须保持 owed。

## D5 一致性机制内化

不继续扩展导出 Movement/WithMovement/SetMovement。优先从同一事实提交序号恢复出的投影读取；若过渡期仍需 fence，它必须是私有实现且在每个写点有可控交错测试。不得用 before/after 同值证明一个写点漏 Advance 的快照正确。

## D6 公共面收缩

CloseRequest 仅保留 Address 与 Deadline time.Time；Resolve 内化或删除。DrainReport 仅保留 Outcome 与 Unsettled，Drained 由 Outcome 推导而非独立字段。是否将 DrainReport 正名 CloseReport 由 04 依据实际公共消费者决定，不为改名而改名。

## 风险与回退

- Cut 后资源仍存活是刻意的安全余量 → CloseReport 必须具名，后续 Close 可重试；不得声称进程资源已全部释放。
- 单快照改造过大 → 先以两个公共症状锚切断隐式取消和无界 join，再收读面；每步独立复审。
- 审批恢复不仅是 answer → 02 必须证明工具/参数/CallFrame 可恢复，否则只 Cut，不伪装可续。
