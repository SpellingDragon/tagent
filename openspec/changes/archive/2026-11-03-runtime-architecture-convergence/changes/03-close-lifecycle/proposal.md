# 关闭与未结清生命周期收敛

父变更：runtime-architecture-convergence。W0-A03 的 Q1–Q4 已由编排者采纳；C1/C2 为关键阻断，M1–M5 纳入同一状态机修复而非逐点加补丁。

## Why

共享 ReadUnsettled 并没有形成唯一关闭状态机：Host 与 Run 各自循环，writer/consumer/landing 另作门槛；完整 Close 的 join 不受预算约束；远端还在 Close 内自动取消审批任务。概念增加，但关闭活性、业务保全和结果一致性都未成立。

## What Changes

- Close/Quiesce 不得新造审批 cancelled 终局；Cut 原样保留 AwaitingApproval 与 attention 事实，只允许已有义务的 flush、消费回执和符合条件的 landing。
- 一个串行关闭协调器、一个绝对预算覆盖拒入口、判读、join 和 teardown；无界 done 等待不得位于 Cut 返回之前。
- Cut 不释放仍有 writer 的资源，不伪装最终 closed；并发调用共享当前尝试，Cut 后可用新预算重试，真正完成后才缓存最终结果。
- rest 包含未终态任务、未逻辑消费通知、已受理未闭合输入，以及同快照中的 writer/landing；明确 failed completion 可闭合输入，取消/提交失败不能。
- 请求校验失败用 CloseUnreached+具名 error；Broken 只表示权威账读取/一致性失败。
- CloseRequest 只留一个 Deadline；DrainReport 删除 Drained 独立真源；Movement 等同步机制不作为公共领域概念。

## 边界与依赖

- **依赖**：01 的唯一 Consumer 和答案关联；02 的通知消费/失败语义与审批执行材料恢复。
- **被依赖方**：04 决定公共 API、规格唯一 owner 与概念绑定。
- **接口面**：正式 Host.Close/CloseReport、Run.Close、公开 Unsettled/Close 结果。
- **禁止事项**：不新增强制关闭 API、Owner/Coordinator 公共类型或另一份恢复账；不把 HoldBook 是否存在当作任务可恢复证明；不为通过测试释放仍在写的资源。
