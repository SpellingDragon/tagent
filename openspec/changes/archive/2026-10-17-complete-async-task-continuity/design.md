# Design

## Identity and boundaries

Task ID标识工作入口；intent round标识被受理的尝试；producer round标识实际执行；RoundOwner是本轮receiver归属，Origin保持派生来源不可变。InputBatchID携带发起本轮的opaque输入归属，失败intent与仍活producer各保留自己的token。信号SourceID由task+round+seq组成，时间、WaitFor、输入批次、producer绑定声明和内容冻结。条件满足不等于producer已结束；Final结束观察等待但不把suspect变成completed。

## Admission, outbox and cleanup

生产路径是gate/dedup→intent accepted→prepare→binding accepted→activate。信号先固定raw事实和预留receipt，再经明确receiver受理，最后ACK原producer证据。批量与补发也承担每条raw受理/ACK，不以void回调或bus成功掩盖拒收。失败在同一身份上退避重试。

## Logical invocation recovery

本地child保存continuation checkpoint、私有projection、输入处置、等待和stream游标、结果缓存、父task原round地址。恢复重建所有receiver与资源driver后才启用pending和deadline。未资源化intent可沿原intent补完prepare；有资源者只attach。中断工具结果缺口显式reconcile，不重跑初始goal。ready服务在goal完成时交接明确scope。

## Bounds

本地等待不消耗整调用600s deadline，模型active turn仍有限、Task TTL仍有限；远端A2A维持transport deadline。单最近deadline timer替代无输入时不会收敛的被动TTL。Close超时与CloseDone最终退出不同；所有Stopped确认仍是实际producer凭据。保留既有存储耐久边界和副作用至少一次风险。

## Validation

任务层race与针对性case已执行；完整仓库、独立bot模块、lint、真实tmux及长跑状态见docs/async-task-continuity-report.md，集成负责人补齐后再发布。
