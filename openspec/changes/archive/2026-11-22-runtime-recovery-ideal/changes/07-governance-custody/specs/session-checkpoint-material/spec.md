> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../../../CARRYOVER.md)逐项接收，不因转历史而取消。

## MODIFIED Requirements

### Requirement: governance 恢复读切片而非全流扫

governance 可恢复态 SHALL 作为框架 checkpoint 的注册切片承载（两流尾序号、审批簿与派生索引），经同一状态接缝注册、按名聚合，并在生产宿主接线，SHALL NOT 停在测试态。恢复 SHALL 读切片派生的尾序号与索引加切面后的尾回放，MUST NOT 全流扫事件账本，也 MUST NOT 全目录扫审批簿。治理事件键 SHALL 保持定长零填充使字典序等于操作序；尾序号 SHALL 取自切片续号，MUST NOT 静默重置致复用已写键；切片缺失或损坏 SHALL 具名拒绝 boot，SHALL NOT 按零值重置。审批态恢复 SHALL 与运行期一致：已结与过期请求不复活。治理事件链 SHALL 仍是语义真相与回放源，切片只是加速恢复的派生快照。

切片陈述 SHALL 限于**有效态**：两流尾序号、未结且未过期的审批请求、未终态的目标，以及「不复活」判定所必需的最小身份。已结的 denial 记录、已终态目标与已结审批证据 SHALL 视为历史：SHALL 留在链上按身份可查，MUST NOT 被切片全量复制；切片大小 SHALL 随有效工作规模计费，MUST NOT 随治理历史线性增长。任何清减 MUST NOT 删除、覆盖或改写已提交的治理事件。

审批有效期 SHALL 以链上事实携带的有效期为准，并在**读取时**判定：已过期请求 SHALL NOT 因恢复而复活；切片 MAY 不陈述已过期项，但「该请求已过期」SHALL 仍可按身份查到。恢复 MUST NOT 因已结审批证据而重放会改变判定结果的事实，除非该重放被证明幂等且不构成第二真相。

历史查询 SHALL 有具名边界：`Query(limit)`／`Count()`／目标列表在切片只陈述有效态之后，MUST NOT 静默返回「历史上没有」；窗口之外的查询 SHALL 按身份查链补齐或具名拒绝，二者择一由实现声明，且语义变更 SHALL 显式登记并同步其消费方。

#### Scenario: 生产接线非测试态

- **WHEN** 真实宿主 boot 装配 governance
- **THEN** governance SHALL 挂 ledger 与 goals 并经生产状态接缝把其切片注册到框架 checkpoint
- **AND** ledger 与 goals 的生产调用方 SHALL 至少一处，SHALL NOT 仅由测试或编译期断言持有

#### Scenario: 尾序号读切片非全流扫

- **WHEN** 在累积大量 governance 事件的存储上重启
- **THEN** 恢复 SHALL 读切片的尾序号与派生索引加切面后尾回放，SHALL NOT 经全流扫
- **AND** 恢复成本 SHALL 不随 governance 事件总数线性增长

#### Scenario: 序号连续性不重置

- **WHEN** 重启后的 ledger 与 goals 续写下一条 governance 事件
- **THEN** 尾序号 SHALL 取自切片并等于已存储最大值，事件键 SHALL 保持定长零填充，SHALL NOT 复用已写键
- **AND** 切片缺失或损坏 SHALL 具名拒绝 boot，MUST NOT 静默按零值重置致覆盖已提交事实

#### Scenario: 审批态恢复一致

- **WHEN** 宿主重启恢复审批簿
- **THEN** 审批态 SHALL 从 governance 切片派生恢复加尾回放，SHALL NOT 经全目录扫
- **AND** 恢复结果 SHALL 与运行期一致：已结请求不复活、过期请求不复活、attention 链对账不重不漏

#### Scenario: 切片随有效工作计费而非随历史

- **WHEN** 相同有效工作（同一批未结审批与同一批未终态目标）在 denial 历史不断增长的存储上重复多代
- **THEN** 切片字节与陈述条目 SHALL 随有效工作计费，MUST NOT 随已结 denial 记录、已终态目标与已结审批线性增长
- **AND** 该断言 SHALL 附分母（未结项数）以证明「平坦」不是「治理没长」的伪装

#### Scenario: 有效态不被清减掉

- **WHEN** 一条未过期 pending 审批或一个未终态目标因清减未被陈述
- **THEN** 恢复后该调用 SHALL 仍被 park（或具名拒绝），SHALL NOT 因「切片里没有」而被静默放行
- **AND** 清减实现 MUST NOT 以时间阈值删除仍有效的请求

#### Scenario: 有效期跨重启

- **WHEN** 一条审批请求的有效期在进程停机期间过去
- **THEN** 重启后该请求 SHALL NOT 复活为可放行，SHALL NOT 因切片陈述了它而被重新判定为有效
- **AND** 「已过期」这一结论 SHALL 仍可按身份查到，MUST NOT 答「从未存在」

#### Scenario: 历史查询边界具名

- **WHEN** 一次查询落在切片窗口之外（要求超出窗口的 denial 记录或计数）
- **THEN** 实现 SHALL 按身份查链补齐或具名拒绝，MUST NOT 静默返回空集或窗口内条目
- **AND** 该语义变更 SHALL 显式登记并同步其消费方，MUST NOT 以修改断言文案的方式通过
