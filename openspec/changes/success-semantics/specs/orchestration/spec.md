# Orchestration 契约

## ADDED Requirements

### Requirement: 先交接后销账

生成结果的交付义务 SHALL 在发送前持久登记；生成义务 SHALL 在交付义务登记成功后才消费。

#### Scenario: 崩溃后不漏送

- **WHEN** Sender 未确认时进程被强杀并重启
- **THEN** 已生成结果 SHALL 被恢复发送，待交付账本销账

### Requirement: 状态提交 checked

交付状态的持久化失败 SHALL 以具名错误返回且不发布内存态变化。

#### Scenario: 持久化失败不吞错

- **WHEN** 交付状态写入失败
- **THEN** 接口 SHALL 返回错误且内存态保持

### Requirement: 旧格式具名拒绝

上一版 key/content/summary 格式字节 SHALL 被 DecodeEventBody 具名拒绝。

判据 SHALL 由原始 JSON 的顶层键存在性给出：顶层无 `message` 键即不是本 schema，覆盖 key/content/summary 与 event_key 两种旧形态。判据 SHALL NOT 由解码后的结构体零值给出——空 message 是本 schema 内的合法审计事实，零值判据会把它一并误拒。

#### Scenario: 不再静默丢正文

- **WHEN** 旧格式字节解码
- **THEN** SHALL 返回 ErrEventBody，正文不再静默为空

### Requirement: 残余窗口显式声明

用户 turn 的「invoke 返回→交付登记」微窗口（同一调用栈内）SHALL 被显式声明为残余窗口，本契约 SHALL NOT 据此作无条件的「不漏送」承诺。

理由 SHALL 与声明同处记载：结果只有在 invoke 返回之后才存在，而登记只能在拿到结果之后才可写；进程在该窗口内被杀时账上尚无这条义务、正文也尚无持久副本可读，重启能看见的只有登记完成之后的事实，所以该窗口不存在可恢复源。契约承诺的是**已登记义务**的恢复，不是窗口本身的覆盖。

#### Scenario: 强杀后已登记义务恰一次投递

- **WHEN** Sender 未确认时进程被强杀并重启
- **THEN** 强杀前投递 SHALL 为 0 条，重启后该结果 SHALL 恰投递 1 条，持久账 `tagent/task/v1/delivery:` 前缀下的 owed 条目 SHALL 归零，该结果的生成义务 SHALL 恰被消费一次

#### Scenario: 微窗口内被杀不作恢复承诺

- **WHEN** 进程在 invoke 已返回而交付义务尚未登记的微窗口内被杀
- **THEN** 该结果 SHALL 计为残余窗口内的丢失，本契约不作恢复承诺

### Requirement: final 跨重启不承诺

执行最终输出的 final 表 SHALL 被声明为进程内运行时状态：跨重启不承诺保留。重启后读取 final SHALL 返回 ErrFinalOutputUnrecorded，该错误 SHALL 被读作可解释错误而非成功语义的违约；final 的持久化登记 SHALL 归入下一轮实现，不在本契约的承诺面内。

#### Scenario: 重启后读 final 属可解释错误

- **WHEN** 进程重启后读取上一进程写下的 final
- **THEN** SHALL 返回 ErrFinalOutputUnrecorded，且该返回 SHALL NOT 被读作本契约的违约

#### Scenario: 进程内 final 照常可读

- **WHEN** 同一进程内 final 写入后读取
- **THEN** SHALL 返回已写入的最终输出

### Requirement: 无持久层装配的降级

未装配持久层（storage tier）的运行实例 SHALL 被声明为进程内降级：交付状态与待交付义务 SHALL NOT 承诺跨重启保留，重启后从进程内空态开始。本 Requirement SHALL 被读作上述各条的边界，SHALL NOT 被读作无条件承诺。

#### Scenario: 无 tier 装配下重启

- **WHEN** 未装配持久层的实例重启
- **THEN** 交付状态 SHALL 从进程内空态开始，重启前登记的交付义务不作恢复承诺
