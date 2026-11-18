## MODIFIED Requirements

### Requirement: 任务终态由 checkpoint 切片承载并有界恢复

任务终态 SHALL 作为框架 checkpoint 的注册切片承载，boot SHALL 经该切片恢复任务账户，而非全史重读任务事实族。切片 SHALL 逐任务陈述已消费收据、结算轮尝试与具名拒绝三族，并 SHALL 显式陈述空集，MUST NOT 以省略键表达「无收据」：扩携前落链的切片未携该键，与切片陈述无收据，二者 SHALL 可区分。未携该键的任务 SHALL 按未覆盖处理并读其链兜底，MUST NOT 把缺失键当无收据参与求值。

覆盖段与尾段 SHALL 经与运行期提交同一记录应用路径入表（单一派生路径），MUST NOT 为恢复另立派生实现；欠账 SHALL 恒等于外送报文减已消费减具名拒绝。账户入表 SHALL 在读取最新 checkpoint 之后、消费该账户切片的邮箱恢复之前完成，且 SHALL 含覆盖段与尾段二者。无 checkpoint 或未被切面点名的年轻链 SHALL 经尾段增量或全链读兜底。任务事件链 SHALL 仍是语义真相与回放源，切片只是加速恢复的派生快照。

任务账户的陈述成本 SHALL 随**有效工作规模**计费：一次切面陈述某任务的收据 SHALL 只读该任务自己的收据面，MUST NOT 为陈述一个任务而遍历全库收据集合；已结历史本身可增长，MUST NOT 以「切片常数大小」的声称代替计费口径。

清减（不再陈述已结收据）SHALL 以「按身份判重仍可答」为唯一判据：一条收据表达的通知身份 SHALL 可被判定为已处理，直到按身份重放不再可能到达该身份为止；MUST NOT 以时间阈值（TTL）删除未完义务，MUST NOT 删除 `attempts` 或 `rejected` 族来降低陈述成本。清减后的「未陈述」SHALL 与「陈述为空集」在载荷上仍可区分。

任务账户 SHALL 是 registry 全局账：它 MAY 不进入按会话陈述的切面，会话切面 MUST NOT 因此被要求承载任务账；历史查询（已结任务与收据）SHALL 按身份可查，MUST NOT 以「启动时全量恢复」作为历史查询的实现。

#### Scenario: 任务恢复读切片非全扫

- **WHEN** 存储已落最新 checkpoint，其 tasks 切片点名 N 个任务且各任务携三族收据
- **THEN** 账户恢复 SHALL 只读该切片，覆盖段 SHALL 零任务事实族前缀读
- **AND** revisions/facts/mutations/specs/欠账/已消费/尝试/拒绝 SHALL 与全扫基线逐项相等

#### Scenario: 陈述成本按有效工作计费

- **WHEN** 相同有效工作（同一批 tasks 与同一批未结通知）在已结收据不断增长的存储上重复多代
- **THEN** 切面陈述的收据条目数与字节 SHALL 随有效工作计费，MUST NOT 随已结收据总数线性增长
- **AND** 该断言 SHALL 附分母（任务数、未结收据数）以证明「平坦」不是「链没长」的伪装

#### Scenario: load 计数不随历史任务数增长

- **WHEN** 在多代累积、历史已终态任务数递增而存活账不变的存储上重启
- **THEN** durable load 的键计数与字节计数（真量，非正文计数代理量）SHALL 保持平坦
- **AND** 其分母（索引规模／历史任务数）SHALL 已增长

#### Scenario: 年轻链与无 checkpoint 兜底仍一致

- **WHEN** 某任务链未被最新 checkpoint 点名（年轻链），或存储无 checkpoint
- **THEN** SHALL 经切面后增量／全链读兜底恢复该链，链可算回的字段 SHALL 与切片路径逐项一致
- **AND** SHALL NOT 因有界化丢链，SHALL NOT 把已终态清扫丢弃的任务重新折叠回账

#### Scenario: 重启后 settle 幂等

- **WHEN** 一通知的已消费收据、结算轮尝试或具名拒绝在切面之前落库，进程重启
- **THEN** 该通知 SHALL NOT 再次欠账，SHALL NOT 再驱动第二次结算轮
- **AND** 三族 SHALL 由切片恢复，SHALL NOT 经全史重读取得

#### Scenario: 清减后跨进程重投仍至多一次

- **WHEN** 一条收据因清减不再被切片陈述，同一通知身份在另一进程或另一次启动上被重投
- **THEN** 系统 SHALL 仍判定该通知已处理，SHALL 至多产生一条收据、MUST NOT 二次结算
- **AND** 若判据不足以保证该判定，清减 SHALL 被拒绝而不是放行

#### Scenario: 乱序完成不抹洞

- **WHEN** 后序通知先行完成、前序通知仍欠账
- **THEN** 前序通知 SHALL 仍处欠账集内，consumption 位置 SHALL 停在洞前
- **AND** 以连续前缀水位表达的清减 MUST NOT 覆盖该洞

#### Scenario: 尾段欠账不因时序后移丢失

- **WHEN** 切面之后提交的任务事实携带未消费通知，进程重启
- **THEN** 该通知 SHALL 在 mailbox 切片恢复后仍欠账可投递，座与实例 SHALL 按冻结路由开出

#### Scenario: 旧切片未携收据键

- **WHEN** boot 读到扩携前落链的 tasks 切片（无三族键），或清减后未陈述某任务的收据
- **THEN** 该任务 SHALL 按未覆盖处理并读其链兜底（前者）／按判重面判定（后者），SHALL 记一行可检索日志
- **AND** SHALL NOT 把缺失键当「无收据」而把已消费通知重新欠账

#### Scenario: 坏切片与拒读具名失败

- **WHEN** 切片陈述的收据身份与其任务不符，或尾段读被拒绝
- **THEN** boot SHALL 具名拒绝，SHALL NOT 静默丢收据、SHALL NOT 按空账启动

#### Scenario: 会话切面不承载任务账

- **WHEN** 一次按会话的切面被触发
- **THEN** 任务账 SHALL 不被要求进入该会话切面（任务账是 registry 全局账）
- **AND** 该会话的其它切片恢复 MUST NOT 因任务账缺席而错算欠账
