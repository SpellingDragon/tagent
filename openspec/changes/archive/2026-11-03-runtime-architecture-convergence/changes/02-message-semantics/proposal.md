# send、通知与重入语义收敛

父变更：runtime-architecture-convergence。W0-A02 的 Q1–Q5 已由编排者采纳，C1–C3 经关键代码亲核成立。

## Why

同一通知在 live sink 与 unattended driver 中于不同时点 Consume；settle fact 提交失败仍可 Accepted 并清掉唯一义务；re-entry 在公开 ok 后还能回滚任务并追加 denied。不同失败路径因此需要越来越多补偿概念。

## What Changes

- send 的 ok 统一为目标输入事实以稳定身份持久提交，不是目标回答完成；call_subagent 的 ok 仍是 admission，二者不合并状态机。
- 通知只有在消费结果与连续位置持久记录后结清；completed/failed/cancelled 都可表示“已处理但结果不同”。进程停止、存储失败或未形成消费结果才保持 owed。
- 外部 sender/handoff 是独立的派生外发义务，其失败不重开已消费通知。
- 输入提交前的 re-entry 失败可内部补偿并拒绝；提交后不得回滚旧 settled 或追加 denied 来否认 ok，只能记录本轮真实结果并恢复。
- 存活任务直接注入，settled 才 Reactivate；failed/cancelled/running/suspect 具名拒绝。重启后由持久任务/地址/Round/绑定重建，不以内存 continuation 决定合法性。
- 两族投递在提交时写同构 Delivery* 元数据；DeliveryLedger 是事实读投影，进程内 receipts 不得改变重放答案。

## 边界与依赖

- **依赖**：01 的唯一地址驱动与答案关联；03 决定 Close 如何呈现已处理失败和外发 owed。
- **被依赖方**：03 的 Unsettled、04 的词汇/API 收缩。
- **接口面**：正式 Host/send、任务通知恢复、真实重启、DeliveryLedger 公共读面。
- **禁止事项**：不新增统一 Admission/Delivery/Consumption 超级状态机；不新增 retry ledger、AnswerStore 或补偿公共 API；不以外部渠道 exactly-once 作虚假承诺。
