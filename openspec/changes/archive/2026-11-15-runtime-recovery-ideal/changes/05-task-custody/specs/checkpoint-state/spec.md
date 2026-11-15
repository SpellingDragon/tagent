## MODIFIED Requirements

### Requirement: 任务终态有界恢复

任务终态 SHALL 作为框架 checkpoint 的注册切片承载，boot SHALL 经 checkpoint 恢复任务态而非全史重读全部任务链。任务提交账户（`DurableCommit`）的恢复 SHALL 读该切片陈述的覆盖段，SHALL NOT 在 boot 无条件全扫任务事实族五前缀（fact/spec/consume/attempt/reject）；覆盖段 SHALL 零任务事实族前缀读，尾段 SHALL 按任务身份读其链，恢复成本 SHALL 有界于存活账加切面后增量。registry 的 `restoreChains` 全史读 SHALL 降为兜底（无 checkpoint 的年轻链、旧切片未携收据键的任务），不再是主路径。

切片 SHALL 携每任务的已消费收据、结算轮尝试与具名拒绝三族（settle 幂等所需）。三族字段 SHALL 显式陈述空集，SHALL NOT 以省略键表达「无收据」，使「扩携前落链的切片未携该键」与「切片陈述无收据」可区分；未携该键的任务 SHALL 按未覆盖处理并读其链兜底，SHALL NOT 把缺失键当无收据参与求值。

覆盖段与尾段 SHALL 经与运行期提交同一记录应用路径入表（单一 g(x)），SHALL NOT 为恢复另立派生实现；欠账 SHALL 恒等于 outbox 减已消费减具名拒绝，由同一组函数在运行期与恢复期维护。账户入表 SHALL 在读取最新 checkpoint 之后、消费该账户的切片（mailbox）恢复之前完成，且 SHALL 含覆盖段与尾段二者。尾段任务事实与年轻链身份 SHALL 由切面后增量供给。无 checkpoint（Offset=0）时 SHALL 退化为全链读兜底。

任务事件链 SHALL 仍是语义真相与回放源，checkpoint 任务切片只是加速恢复的派生快照；任何有界化 SHALL NOT 删除、覆盖或改写已提交事实，按身份读全史的消费者 SHALL 仍能取回正文。

#### Scenario: 任务恢复读切片非全扫

- **WHEN** 存储已落最新 checkpoint，其 tasks 切片点名 N 个任务且各任务携三族收据
- **THEN** 账户恢复 SHALL 只读该切片，覆盖段 SHALL 零任务事实族前缀读
- **AND** revisions/facts/mutations/specs/欠账/已消费/尝试/拒绝 SHALL 与全扫基线逐项相等

#### Scenario: load 计数不随历史任务数增长

- **WHEN** 在多代累积、历史已终态任务数递增而存活账不变的存储上重启
- **THEN** durable load 的键计数与字节计数（真量，非正文计数代理量）SHALL 保持平坦，SHALL 不随历史任务总数线性增长
- **AND** 其分母（索引规模／历史任务数）SHALL 已增长，使平坦是恢复的声称而非链未增长的声称

#### Scenario: 年轻链兜底仍一致

- **WHEN** 某任务链未被最新 checkpoint 点名（年轻链），或存储无 checkpoint
- **THEN** SHALL 经切面后增量／全链读兜底恢复该链，链可算回的字段 SHALL 与切片路径逐项一致
- **AND** SHALL NOT 因有界化丢链，SHALL NOT 把已终态清扫丢弃的任务重新折叠回账

#### Scenario: 重启后 settle 幂等

- **WHEN** 一通知的已消费收据、结算轮尝试或具名拒绝在切面之前落库，进程重启
- **THEN** 该通知 SHALL NOT 再次欠账，SHALL NOT 再驱动第二次结算轮
- **AND** 三族 SHALL 由切片恢复，SHALL NOT 经全史重读取得

#### Scenario: 尾段欠账不因时序后移丢失

- **WHEN** 切面之后提交的任务事实携带未消费通知，进程重启
- **THEN** 该通知 SHALL 在 mailbox 切片恢复后仍欠账可投递，座与实例 SHALL 按冻结路由开出
- **AND** SHALL NOT 因账户入表晚于 mailbox 恢复而静默丢失

#### Scenario: 旧切片未携收据键

- **WHEN** boot 读到扩携前落链的 tasks 切片（无三族键）
- **THEN** 该任务 SHALL 按未覆盖处理并读其链兜底，SHALL 记一行可检索日志
- **AND** SHALL NOT 把缺失键当「无收据」而把已消费通知重新欠账

#### Scenario: 坏切片与拒读具名失败

- **WHEN** 切片陈述的收据身份与其任务不符，或尾段读被拒绝
- **THEN** boot SHALL 具名拒绝，SHALL NOT 静默丢收据、SHALL NOT 按空账启动