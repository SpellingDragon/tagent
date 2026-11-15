# session-checkpoint-material Specification

## Purpose
统一框架恢复的事实源与插件材料声明：checkpoint 以会话为触发与切面粒度，经状态接缝的同一条 g(x) 派生路径恢复（运行期增量与回放重建一致），派生态以开放项加水位有界表达而不删除已提交事实；插件面以统一 `Material` 契约把时机与失败语义声明为策略，六端口收敛为兼容读法。

归档溯源：`session-checkpoint-material-unification` 的 delta 经归档转正为本 spec。
## Requirements
### Requirement: checkpoint 协调者按会话触发并写该会话的派生态

框架 SHALL 以会话为 checkpoint 的触发与切面粒度：协调者按该会话的上下文（压缩、输入轮变化、会话关闭）请求一次切面，切面 SHALL 只写该会话各消费者此刻的派生读态，SHALL NOT 为另一会话的状态付出读取或落链成本。已落链的 checkpoint 事实 SHALL 保持既有一身份一不可变事实与内容寻址校验；新建 checkpoint SHALL 以分区加该分区槽位的双段键命名（`checkpoint:<partition>:<partitionSeq>`），两分区可站在同一槽位而键不撞，键 SHALL 随该分区槽位单调。恢复 SHALL 读最近 checkpoint 并只对该会话尾部重跑派生，SHALL NOT 因恢复而重放 observation 副作用。

#### Scenario: 会话级切面只写本会话状态

- **WHEN** 会话 A 到达其 checkpoint 触发条件而会话 B 无新事实
- **THEN** 落链的 checkpoint SHALL 只携带会话 A 的派生切片，读取与恢复成本 SHALL 不随会话 B 的历史增长

#### Scenario: 恢复只重跑本会话尾部

- **WHEN** 进程重启后读取到会话 A 的最近 checkpoint
- **THEN** 系统 SHALL 只对会话 A 在切面之后的尾部事实重跑 g(x)，且该重建 SHALL 不触发送达、结算或冥想等 observation 副作用

### Requirement: `Stateful` 收编消费者读态且不新增挂点

状态接缝 SHALL 同时承载消费者的派生读态：reducer 为该消费者的纯派生面，emitter 为该消费者的 observation 驱动者；同一事实的恢复重建 SHALL 走与运行期同一派生路径，SHALL NOT 为恢复另立一套派生实现。接入一个新的可恢复消费者 SHALL 只注册一个切片，SHALL NOT 要求新增挂点、端口或第二份真相。

#### Scenario: 新消费者只注册切片即获得恢复

- **WHEN** 一个新的派生消费者按接缝注册其 g(x) 与 snapshot/restore
- **THEN** 它 SHALL 经既有协调者获得切面与恢复，框架代码 SHALL NOT 为该消费者新增专用挂点或端口

#### Scenario: 恢复与运行期派生一致

- **WHEN** 同一会话分别经「运行期增量」与「checkpoint 加尾部重跑」得到派生状态
- **THEN** 两者的可观察结果 SHALL 相等，差异 SHALL 视为缺陷而非可接受偏差

### Requirement: 任务终态由 checkpoint 切片承载并有界恢复

任务终态 SHALL 作为框架 checkpoint 的注册切片承载，boot SHALL 经该切片恢复任务账户，而非全史重读任务事实族。切片 SHALL 逐任务陈述已消费收据、结算轮尝试与具名拒绝三族，并 SHALL 显式陈述空集，MUST NOT 以省略键表达「无收据」：扩携前落链的切片未携该键，与切片陈述无收据，二者 SHALL 可区分。未携该键的任务 SHALL 按未覆盖处理并读其链兜底，MUST NOT 把缺失键当无收据参与求值。

覆盖段与尾段 SHALL 经与运行期提交同一记录应用路径入表（单一派生路径），MUST NOT 为恢复另立派生实现；欠账 SHALL 恒等于外送报文减已消费减具名拒绝。账户入表 SHALL 在读取最新 checkpoint 之后、消费该账户切片的邮箱恢复之前完成，且 SHALL 含覆盖段与尾段二者。无 checkpoint 或未被切面点名的年轻链 SHALL 经尾段增量或全链读兜底。任务事件链 SHALL 仍是语义真相与回放源，切片只是加速恢复的派生快照。

#### Scenario: 任务恢复读切片非全扫

- **WHEN** 存储已落最新 checkpoint，其任务切片点名多个任务且各任务携三族收据
- **THEN** 账户恢复 SHALL 只读该切片，覆盖段 SHALL 零任务事实族前缀读
- **AND** 修订、事实、变更、规格、欠账、已消费、尝试与拒绝 SHALL 与全扫基线逐项相等

#### Scenario: load 计数不随历史任务数增长

- **WHEN** 在多代累积、历史已终态任务数递增而存活账不变的存储上重启
- **THEN** durable load 的键计数与字节计数（真量，非正文计数代理量）SHALL 保持平坦，SHALL NOT 随历史任务总数线性增长
- **AND** 其分母（索引规模／历史任务数）SHALL 已增长，使平坦成为恢复的声称而非链未增长的声称

#### Scenario: 重启后 settle 幂等

- **WHEN** 一通知的已消费收据、结算轮尝试或具名拒绝在切面之前落库，进程重启
- **THEN** 该通知 SHALL NOT 再次欠账，SHALL NOT 再驱动第二次结算轮
- **AND** 三族 SHALL 由切片恢复，SHALL NOT 经全史重读取得

#### Scenario: 旧切片未携收据键

- **WHEN** boot 读到扩携前落链的任务切片（无三族键）
- **THEN** 该任务 SHALL 按未覆盖处理并读其链兜底，SHALL 记一行可检索日志
- **AND** SHALL NOT 把缺失键当「无收据」而把已消费通知重新欠账

#### Scenario: 年轻链兜底仍一致

- **WHEN** 某任务链未被最新 checkpoint 点名，或存储无 checkpoint
- **THEN** SHALL 经尾段增量或全链读兜底恢复该链，链可算回的字段 SHALL 与切片路径逐项一致
- **AND** SHALL NOT 因有界化丢链，SHALL NOT 把已终态清扫丢弃的任务重新折叠回账

#### Scenario: 坏切片与拒读具名失败

- **WHEN** 切片陈述的收据身份与其任务不符，或尾段读被拒绝
- **THEN** boot SHALL 具名拒绝，SHALL NOT 静默丢收据、SHALL NOT 按空账启动

### Requirement: governance 恢复读切片而非全流扫

governance 可恢复态 SHALL 作为框架 checkpoint 的注册切片承载（两流尾序号、审批簿与派生索引），经同一状态接缝注册、按名聚合，并在生产宿主接线，SHALL NOT 停在测试态。恢复 SHALL 读切片派生的尾序号与索引加切面后的尾回放，MUST NOT 全流扫事件账本，也 MUST NOT 全目录扫审批簿。治理事件键 SHALL 保持定长零填充使字典序等于操作序；尾序号 SHALL 取自切片续号，MUST NOT 静默重置致复用已写键；切片缺失或损坏 SHALL 具名拒绝 boot，SHALL NOT 按零值重置。审批态恢复 SHALL 与运行期一致：已结与过期请求不复活。治理事件链 SHALL 仍是语义真相与回放源，切片只是加速恢复的派生快照。

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
- **AND** 恢复结果 SHALL 与运行期一致（已结请求不复活、过期请求不复活、attention 链对账不重不漏）

### Requirement: 插件 Material 声明以时机与失败语义为策略

插件面 SHALL 以统一 `Material` 契约声明它消费什么材料，并把时机（副作用前／提交后／读取时）与失败语义（拦截／保义务／上报欠账）作为声明中的策略表达。端口名 SHALL NOT 是语义的唯一载体；既有六端口 SHALL 作为兼容读法保留到迁移完成，迁移期 SHALL NOT 静默改变任一既有面的失败行为。

#### Scenario: 同一材料声明两种失败语义

- **WHEN** 两个面消费同一种材料并分别声明「保留义务」与「记缺口继续」
- **THEN** 分派 SHALL 按各自策略执行，SHALL NOT 由一个端口名推断失败语义

### Requirement: 派生态有界且不删除已提交事实

checkpoint 承载的派生态 SHALL 以开放项加水位表达，已结部分 SHALL 留在链上而不嵌入 checkpoint，checkpoint 大小 SHALL 有界于开放项数量。热层 SHALL 同样有界：切面水位之前已结的事实 SHALL 搬迁出热层并仍可按身份读，热层 SHALL 只保留水位之后的尾段与各链最新切面点名的键。退位判据 SHALL 是水位制，MUST NOT 依赖容量或龄期阈值。任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实：已搬迁的事实按身份读 SHALL 仍返回原字节（透明回冷层），SHALL NOT 答 missing；恢复期读全史的消费者 SHALL 仍能按身份读到其正文。

#### Scenario: 已结集合不进入 checkpoint

- **WHEN** 一个会话积累了大量已结交付或已答压缩判决
- **THEN** checkpoint SHALL 以水位加开放项表达这些集合，checkpoint 大小 SHALL 不随已结数量线性增长

#### Scenario: 水位前已结事实搬迁而尾段留热

- **WHEN** 一次切面之后热层持有水位之前的已结事实与水位之后的尾段事实
- **THEN** 水位之前的已结事实 SHALL 被搬迁出热层，尾段事实与各链最新切面点名的键 SHALL 留在热层
- **AND** 热层字节 SHALL 有界于存活键集，SHALL NOT 随历史线性增长

#### Scenario: 已搬迁事实按身份仍可读

- **WHEN** 按身份读一条已搬迁的事实
- **THEN** SHALL 返回该事实的原字节与原索引位置，SHALL NOT 答 missing
- **AND** 冷层持有而正文读不出或校验失败 SHALL 具名拒绝，MUST NOT 降级为「不存在」

#### Scenario: 有界化不打断全史读者

- **WHEN** 一个有界化实现试图退役或删除已被切面覆盖的事实
- **THEN** 该实现 SHALL 被拒绝，且按身份读全史的消费者 SHALL 仍能取回正文

