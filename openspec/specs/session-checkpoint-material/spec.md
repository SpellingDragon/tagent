# session-checkpoint-material Specification

## Purpose
统一框架恢复的事实源与插件材料声明：checkpoint 以会话为触发与切面粒度，经状态接缝的同一条 g(x) 派生路径恢复（运行期增量与回放重建一致），派生态以开放项加水位有界表达而不删除已提交事实；插件面以统一 `Material` 契约把时机与失败语义声明为策略，六端口仍是插件声明与分派的活契约，只声明端口的面经兼容读法取得其材料。

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

切片陈述 SHALL 限于**有效态**：两流尾序号、未结且未过期的审批请求、未终态的目标，以及「不复活」判定所必需的最小身份。已结的 denial 记录、已终态目标与已结审批证据 SHALL 视为历史：SHALL 留在链上按身份可查，MUST NOT 被切片全量复制；切片大小 SHALL 随有效工作规模计费，MUST NOT 随治理历史线性增长。任何清减 MUST NOT 删除、覆盖或改写已提交的治理事件。

审批有效期 SHALL 以链上事实携带的有效期为准，并在**读取时**判定：已过期请求 SHALL NOT 因恢复而复活；切片 MAY 不陈述已过期项，但「该请求已过期」SHALL 仍可按身份查到。恢复 MUST NOT 因已结审批证据而重放会改变判定结果的事实，除非该重放被证明幂等且不构成第二真相。

历史查询 SHALL 有具名边界：三个读入口在切片只陈述有效态之后，MUST NOT 静默返回「历史上没有」；窗口之外的查询 SHALL 按身份查链补齐或具名拒绝，二者择一由实现声明，且语义变更 SHALL 显式登记并同步其消费方。

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
- **AND** 被约束的三个读入口 SHALL 是 `Query(limit)`／`Count()`／目标列表
- **AND** 该语义变更 SHALL 显式登记并同步其消费方，MUST NOT 以修改断言文案的方式通过

### Requirement: 插件 Material 声明以时机与失败语义为策略

插件面 SHALL 以统一 `Material` 契约声明它消费什么材料，并把时机（副作用前／提交后／读取时）与失败语义（拦截／保义务／上报欠账）作为声明中的策略表达。端口名 SHALL NOT 是语义的唯一载体；六端口 SHALL 仍是插件声明与分派所依据的活契约，按端口分派的行为 MUST NOT 因声明方式而改变。只声明端口而未声明材料的面 SHALL 仍按其端口取得材料与该端口的时机策略，该兼容读法 SHALL 作为公共契约的可选声明形态保留，MUST NOT 因生产面已全部声明材料而被删除；任一面 MUST NOT 因其走兼容读法而静默改变既有失败行为。

#### Scenario: 同一材料声明两种失败语义

- **WHEN** 两个面消费同一种材料并分别声明「保留义务」与「记缺口继续」
- **THEN** 分派 SHALL 按各自策略执行，SHALL NOT 由一个端口名推断失败语义

### Requirement: 派生态有界且不删除已提交事实

checkpoint 承载的派生态 SHALL 以开放项加水位表达，已结部分 SHALL 留在链上而不嵌入 checkpoint，checkpoint 大小 SHALL 有界于开放项数量。任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实：在本构建写出的根上，按身份读一条已提交事实 SHALL 恒返回其原字节与原索引位置，SHALL NOT 答 missing；恢复期读全史的消费者 SHALL 仍能按身份读到其正文。根下的 `cold/kv/journal.log` 由旧构建写出，其冷档事实不在本构建读面内：该形态 SHALL 先经 `scripts/fold-cold-root` 折回新根再供本构建服务，折回前按身份读冷档事实答不存在，该退化是已声明的运维前置，不构成本要求的违反。热层字节的增长 SHALL 被显式声明，MUST NOT 被声称为本要求已达成的有界。

#### Scenario: 已结集合不进入 checkpoint

- **WHEN** 一个会话积累了大量已结交付或已答压缩判决
- **THEN** checkpoint SHALL 以水位加开放项表达这些集合，checkpoint 大小 SHALL 不随已结数量线性增长

#### Scenario: 已提交事实按身份恒可读

- **WHEN** 在本构建写出的根上按身份读一条已提交的事实，无论它落在哪一代、是否被某次切面覆盖
- **THEN** SHALL 返回该事实的原字节与原索引位置，SHALL NOT 答 missing
- **AND** 索引点名而正文读不出或校验失败 SHALL 具名拒绝，MUST NOT 降级为「不存在」

#### Scenario: 旧冷档根未折回时按身份读答不存在

- **WHEN** 根下存在旧构建写出的 `cold/kv/journal.log` 而该根尚未折回
- **THEN** 本构建按身份读冷档里的事实 SHALL 答不存在，该形态 SHALL 被读作已声明的运维前置，MUST NOT 被读作本要求的违反
- **AND** 该形态 SHALL 先以 `go run ./scripts/fold-cold-root -source <旧根> -root <新根>` 折回新根（默认干跑只报待折键数，加 `-restore` 才写入），折回后按身份读 SHALL 返回原字节与源提交序

#### Scenario: 有界化不打断全史读者

- **WHEN** 一个有界化实现试图删除、覆盖或改写已被切面覆盖的事实
- **THEN** 该实现 SHALL 被拒绝，且按身份读全史的消费者 SHALL 仍能取回正文
