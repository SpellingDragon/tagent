# checkpoint-state delta

## ADDED Requirements

### Requirement: 框架统一 checkpoint 事实承载可恢复状态

框架 SHALL 以统一的 `checkpoint` 事实族承载全部可恢复状态切片（会话投影幸存者、宿主全局送达/结算账含开账、邮箱欠账、未应答输入、任务终态），经框架状态接缝由消费者注册、按名聚合，每条 checkpoint 事实 SHALL 携带一致性切面的分区 Seq 水位（Offset）。checkpoint 事实 SHALL 经事实提交入口落链、键按偏移单调（`checkpoint:<partitionSeq>`），一身份一不可变事实。投影恢复 SHALL 以继承的折叠锚为主路径（折叠锚即投影的 checkpoint，按各自锚边界回放，不被全局 Offset 裁剪）；压缩事实 SHALL 回归纯投影职责，仅作为 checkpoint 触发源之一。

#### Scenario: 落链 checkpoint 携带偏移与切片
- **WHEN** 框架协调者在安静点到达 checkpoint 节奏（提交计数/折叠触发/close 收敛）
- **THEN** SHALL 落一条 type=checkpoint 事实，其 Offset 等于该切面的分区 Seq 水位，载荷含投影锚、送达账开账清单与已结水位、邮箱欠账、未应答输入、任务终态

#### Scenario: 最新 checkpoint 读取不随历史增长
- **WHEN** boot 读取最新 checkpoint
- **THEN** SHALL 经索引 newest-first 过滤 type=checkpoint 取首条，读取成本 SHALL 不随累积历史线性增长

### Requirement: 恢复为最近 checkpoint 加偏移后回放

boot SHALL 读最新 checkpoint 事实、载入各状态切片，并回放分区 Seq 大于 Offset 的已提交事实经 `seedFact` 隔离重建（只写内存账本、不跑 After pass、不触发送达/settle/meditation 外部副作用）重新派生偏移后状态。系统 SHALL NOT 在 boot 对每个活跃会话全量读 durable 链来判定未结轮次，SHALL NOT 全史重读送达账，SHALL NOT 全量重投历来欠账；未结判定 SHALL 读恢复后的内存态。无 checkpoint 的年轻会话 SHALL 退化为本会话链全量回放；坏 checkpoint SHALL 具名拒绝 boot。

#### Scenario: 重启成本有界
- **WHEN** 在累积多代历史的存储上重启
- **THEN** boot 恢复成本 SHALL 由最近 checkpoint 与偏移后增量决定，SHALL 不随代数超线性增长

#### Scenario: 开账随 checkpoint 存续
- **WHEN** 折叠时刻存在未清送达义务
- **THEN** 该义务（含 body、目标、lineage、attempt、hold 状态）SHALL 原样进入 checkpoint，折叠 SHALL NOT 使其丢失，重启后 SHALL 被发现且只处理一次

### Requirement: 一致性切面与已结水位

checkpoint 的状态快照 SHALL 在 Offset 之后、框架排水到安静点（无在飞提交与状态应用）时、且**不持任何框架锁**的条件下读取；`SnapshotState` SHALL 为纯读，SHALL NOT flush durable、SHALL NOT commit、SHALL NOT 等待自身提交所需的锁。已结送达集合 SHALL 以分区 Seq 高水位加开账清单表达（「landingSeq ≤ Offset 的输出除开账清单外全部已结」），SHALL NOT 随历史全量嵌入。晚于 Offset 的结算 SHALL 由回放覆盖，早于 Offset 的结算 SHALL 在快照内。

#### Scenario: 结算与 checkpoint 并发
- **WHEN** 一笔送达结算与 checkpoint 聚合并发
- **THEN** 若其 landingSeq ≤ Offset 则 SHALL 已在快照，若 > Offset 则 SHALL 由回放重新派生，二者不重不漏

### Requirement: 任务终态有界恢复

任务终态 SHALL 作为框架 checkpoint 的注册切片承载，boot SHALL 经 checkpoint 恢复任务态而非全史重读全部任务链；registry 的 `restoreChains` 全史读 SHALL 降为无 checkpoint 年轻链的兜底，不再是主路径。任务事件链 SHALL 仍是语义真相与回放源，checkpoint 任务切片只是加速恢复的派生快照。

#### Scenario: 任务恢复不随历史任务数增长
- **WHEN** 在累积大量已终态任务的存储上重启
- **THEN** boot 任务恢复成本 SHALL 由 checkpoint 任务切片与偏移后增量决定，SHALL 不随历史任务总数线性增长

#### Scenario: 年轻链兜底
- **WHEN** 某任务链尚无 checkpoint 覆盖
- **THEN** SHALL 经 restoreChains 全量回放该链恢复，结果与 checkpoint 路径逐项一致

### Requirement: 退役独立票据快照路线

系统 SHALL 退役 RecoveryTicket 独立 KV 快照路线（fileBootRecoveryTicket/WriteRecoveryTicket/recoveryTicketWriter 及其 blob+ticket 键），恢复 SHALL 统一经 checkpoint 事实族；durable-truth 登记 SHALL 移除票据条目并登记 checkpoint surface。

#### Scenario: 票据路线零残留
- **WHEN** 退役完成
- **THEN** grep RecoveryTicket/WriteRecoveryTicket SHALL 零残留，恢复路径 SHALL 只经 checkpoint 事实族