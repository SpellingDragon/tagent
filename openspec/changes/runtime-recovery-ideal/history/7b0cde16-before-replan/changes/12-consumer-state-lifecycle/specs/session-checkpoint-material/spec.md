## MODIFIED Requirements

### Requirement: 派生态有界且不删除已提交事实

checkpoint 承载的派生态 SHALL 以开放项加水位表达，已结部分 SHALL 留在链上而不嵌入 checkpoint，checkpoint 大小 SHALL 有界于开放项数量。任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实：按身份读一条已提交事实 SHALL 恒返回其原字节与原索引位置，SHALL NOT 答 missing；恢复期读全史的消费者 SHALL 仍能按身份读到其正文。热层字节的增长 SHALL 被显式声明，MUST NOT 被声称为本要求已达成的有界。

水位 SHALL 只覆盖**已被每个挂载在该状态接缝上的消费者应用**的事实：一条事实被某个消费者的派生应用拒绝时，切面 SHALL NOT 把该事实纳入水位（保持水位不前移，或拒绝该切面，二者择一由实现声明其失败语义），使重启后的重放仍能覆盖它；MUST NOT 出现「切面覆盖一条其消费者从未应用的事实且此后无重放路径」的状态。

清减 SHALL NOT 以删除「按身份判重所需的已结身份」为代价：一条收据、判决或审批证据表达的身份，SHALL 一直可答到按身份重放不再可能到达该身份为止；MUST NOT 以时间阈值（TTL）删除未完义务，也不得靠删除已结身份换取常数级成本。切片字节 SHALL 随有效工作规模计费；已结历史本身可增长，任何实现 MUST NOT 以「所有状态 O(1)」的声称代替计费口径，也 MUST NOT 以删掉已结集合冒充达成。

#### Scenario: 已结集合不进入 checkpoint

- **WHEN** 一个会话积累了大量已结交付或已答压缩判决
- **THEN** checkpoint SHALL 以水位加开放项表达这些集合，checkpoint 大小 SHALL 不随已结数量线性增长

#### Scenario: 已提交事实按身份恒可读

- **WHEN** 按身份读一条已提交的事实，无论它落在哪一代、是否被某次切面覆盖
- **THEN** SHALL 返回该事实的原字节与原索引位置，SHALL NOT 答 missing
- **AND** 索引点名而正文读不出或校验失败 SHALL 具名拒绝，MUST NOT 降级为「不存在」

#### Scenario: 有界化不打断全史读者

- **WHEN** 一个有界化实现试图删除、覆盖或改写已被切面覆盖的事实
- **THEN** 该实现 SHALL 被拒绝，且按身份读全史的消费者 SHALL 仍能取回正文

#### Scenario: 应用失败的事实不被水位覆盖

- **WHEN** 一条事实已提交，而某个挂载消费者的派生应用返回错误，随后到达一次切面
- **THEN** 该事实 SHALL NOT 落在切面水位之内，或该切面 SHALL 具名失败
- **AND** 重启后的重放 SHALL 重新把该事实交给该消费者，MUST NOT 出现「已提交、从未应用、不再重放」的静默落后

#### Scenario: 清减不破按身份判重

- **WHEN** 一条已结收据/判决/审批证据的身份因清减而不再被切片陈述，随后同一身份的重试或重投到达
- **THEN** 系统 SHALL 仍能按身份判定它已处理，MUST NOT 二次执行、MUST NOT 重新欠账
- **AND** 清减 MUST NOT 以 TTL 删除任何未完义务

#### Scenario: 当前态按有效工作计费

- **WHEN** 相同有效工作在不断增长的已结历史之上重复多代
- **THEN** 切片字节与陈述成本 SHALL 随有效工作规模计费，SHALL NOT 要求已结历史被压成常数
- **AND** 任何性能主张 SHALL 附同条件基线与计量口径，MUST NOT 以「全部状态 O(1)」一句代替

### Requirement: `Stateful` 收编消费者读态且不新增挂点

状态接缝 SHALL 同时承载消费者的派生读态：reducer 为该消费者的纯派生面，emitter 为该消费者的 observation 驱动者；同一事实的恢复重建 SHALL 走与运行期同一派生路径，SHALL NOT 为恢复另立一套派生实现。接入一个新的可恢复消费者 SHALL 只注册一个切片，SHALL NOT 要求新增挂点、端口或第二份真相。

注销（disposer）一个切片 SHALL NOT 静默丢弃该切片已陈述的状态与其未完成的义务：有未完义务时 SHALL 具名交接给承接者，或具名拒绝注销并指出未完成项数；MUST NOT 以「先删持有者再无人知道」的方式卸载。切片从挂载集消失后，boot 读到该切片名 SHALL 具名拒绝（既有 pending 语义 SHALL 覆盖生命周期路径，而不只在装配末尾检查一次）。

消费者的挂载、禁用、卸载、替换 SHALL 经既有注册声明表达（builtin 目录、`plugins:` 声明、本接缝的 `Register`），SHALL NOT 新增第四挂点、SHALL NOT 新建第二注册体系；builtin 与扩展消费方 SHALL 走同一条声明路径，MUST NOT 因来源不同而有两套生命周语义。

#### Scenario: 新消费者只注册切片即获得恢复

- **WHEN** 一个新的派生消费者按接缝注册其 g(x) 与 snapshot/restore
- **THEN** 它 SHALL 经既有协调者获得切面与恢复，框架代码 SHALL NOT 为该消费者新增专用挂点或端口

#### Scenario: 恢复与运行期派生一致

- **WHEN** 同一会话分别经「运行期增量」与「checkpoint 加尾部重跑」得到派生状态
- **THEN** 两者的可观察结果 SHALL 相等，差异 SHALL 视为缺陷而非可接受偏差

#### Scenario: 卸载有未完成义务的切片

- **WHEN** 一个切片仍陈述着未完成的义务时被注销
- **THEN** 注销 SHALL 具名拒绝或把义务交接给具名承接者，MUST NOT 静默删除持有者
- **AND** 拒绝信息 SHALL 指出未完成项数，使运维可定位

#### Scenario: 无承接者的禁用被拒绝

- **WHEN** 一次声明变化禁用一个带可恢复态的消费者，而它的未完义务没有承接者
- **THEN** 该变化 SHALL 具名拒绝并保留旧态，MUST NOT 让义务凭空消失
- **AND** 拒绝路径 SHALL 与扩展消费方的拒绝路径相同（builtin 与扩展同一份声明）

#### Scenario: 代际替换绑定同一权威

- **WHEN** 结构热更换入同一消费者名的新实例
- **THEN** 新实例 SHALL 绑定同一切片名与同一状态权威，切片的已陈述态 SHALL NOT 随代际重建
- **AND** 恢复与运行期仍 SHALL 走同一派生路径，MUST NOT 为代际迁移另立通道
