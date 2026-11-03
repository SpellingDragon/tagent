# async-task-execution Delta

## MODIFIED Requirements

### Requirement: 静止收束三态

关闭 SHALL 由一个串行协调器表达：停止外部注入 → 在同一快照中判断工作、通知、未闭合输入、writer 与 landing → 以同一个绝对预算覆盖所有 join/teardown → 返回 Quiesced/Cut/Broken。Close MUST NOT 新造任务或审批终局；Cut SHALL 原样保留可恢复余量，包括 AwaitingApproval，并在 writer 尚未停止时保留其资源所有权。请求校验失败 SHALL 返回 CloseUnreached 与具名 error；Broken 仅用于权威账不可读或一致性失败。

#### Scenario: 预算到点 Cut
- **WHEN** 静止到点仍有未消费、未终态、未闭合或未停止项
- **THEN** 返回 Cut 与具名余量，不伪释放仍在写的资源，后续可以新预算重试关闭

#### Scenario: 审批等待不被 Close 取消
- **WHEN** Close 遇到本进程有 hold 或重启后无 hold 的 AwaitingApproval
- **THEN** 两者均保持原任务与 attention 事实，预算到点返回 Cut，不写 cancelled 或伪造 answer

#### Scenario: 明确失败 completion 闭合输入
- **WHEN** 输入的 failed 消费结果已按消息语义持久记录且其他义务完成
- **THEN** 该输入不再使关闭保持 Cut；取消或 output/commit 失败而无 completion 的输入仍保持未闭合

#### Scenario: 并发与重复关闭
- **WHEN** 多个调用并发关闭同一 Host/Run
- **THEN** 它们共享当前关闭尝试，资源 teardown 至多一次；Cut 可用新预算重试，最终 closed 后才幂等返回最终结果
