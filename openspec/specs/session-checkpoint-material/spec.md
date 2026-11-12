# session-checkpoint-material Specification

## Purpose
统一框架恢复的事实源与插件材料声明：checkpoint 以会话为触发与切面粒度，经状态接缝的同一条 g(x) 派生路径恢复（运行期增量与回放重建一致），派生态以开放项加水位有界表达而不删除已提交事实；插件面以统一 `Material` 契约把时机与失败语义声明为策略，六端口收敛为兼容读法。

归档溯源：`session-checkpoint-material-unification` 的 delta 经归档转正为本 spec。
## Requirements
### Requirement: checkpoint 协调者按会话触发并写该会话的派生态

框架 SHALL 以会话为 checkpoint 的触发与切面粒度：协调者按该会话的上下文（压缩、输入轮变化、会话关闭）请求一次切面，切面 SHALL 只写该会话各消费者此刻的派生读态，SHALL NOT 为另一会话的状态付出读取或落链成本。已落链的 checkpoint 事实 SHALL 保持既有一身份一不可变事实与内容寻址校验；新建 checkpoint SHALL 复用 `checkpoint:<partitionSeq>` 键族的单调性。恢复 SHALL 读最近 checkpoint 并只对该会话尾部重跑派生，SHALL NOT 因恢复而重放 observation 副作用。

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

### Requirement: 插件 Material 声明以时机与失败语义为策略

插件面 SHALL 以统一 `Material` 契约声明它消费什么材料，并把时机（副作用前／提交后／读取时）与失败语义（拦截／保义务／上报欠账）作为声明中的策略表达。端口名 SHALL NOT 是语义的唯一载体；既有六端口 SHALL 作为兼容读法保留到迁移完成，迁移期 SHALL NOT 静默改变任一既有面的失败行为。

#### Scenario: 同一材料声明两种失败语义

- **WHEN** 两个面消费同一种材料并分别声明「保留义务」与「记缺口继续」
- **THEN** 分派 SHALL 按各自策略执行，SHALL NOT 由一个端口名推断失败语义

### Requirement: 派生态有界且不删除已提交事实

checkpoint 承载的派生态 SHALL 以开放项加水位表达，已结部分 SHALL 留在链上而不嵌入 checkpoint，checkpoint 大小 SHALL 有界于开放项数量。任何有界化手段 SHALL NOT 删除、覆盖或改写已提交事实；恢复期读全史的消费者 SHALL 仍能按身份读到其正文。

#### Scenario: 已结集合不进入 checkpoint

- **WHEN** 一个会话积累了大量已结交付或已答压缩判决
- **THEN** checkpoint SHALL 以水位加开放项表达这些集合，checkpoint 大小 SHALL 不随已结数量线性增长

#### Scenario: 有界化不打断全史读者

- **WHEN** 一个有界化实现试图退役或删除已被切面覆盖的事实
- **THEN** 该实现 SHALL 被拒绝，且按身份读全史的消费者 SHALL 仍能取回正文

