# Orchestration 契约

## ADDED Requirements

### Requirement: 先交接后销账

生成结果的交付义务 SHALL 在发送前持久登记；生成义务 SHALL 在交付义务登记成功后才消费。

#### Scenario: 崩溃后不漏送

- **WHEN** Sender 未确认时进程被强杀并重启
- **THEN** 已生成结果 SHALL 被恢复发送，待交付账本销账

#### Scenario: 发送已落地而回执未落按至少一次容忍

- **WHEN** Sender 已接受正文而 sent 回执未落
- **THEN** 该义务 SHALL 保持未销，重启 SHALL 恰好重投一次（该次重投的 sent 回执落地时；回执未落按至少一次容忍），该重复投递 SHALL 被容忍为至少一次语义的一部分；回执落地 SHALL 是阻止再次投递的唯一凭据

理由 SHALL 与声明同处记载：发送与「发送已落」的持久回执是两个崩溃点，任何实现都只能在两者之间选一个顺序；先发送后回执必然留下「已送达但未销账」的窗口，把该窗口读成漏送会迫使实现选择先回执后发送，从而在相反窗口里真漏送。本契约因此把该窗口的重复投递定为可接受代价，而不是违约。

### Requirement: 交付义务只登记入口地址的正文

交付义务 SHALL 只为入口 agent（entry）地址生成的正文登记。其余地址 SHALL 按 headless 语义驱动：其正文由驱动它的调用方读取，SHALL NOT 进入消息渠道。登记面（settle 终态）、交接面（handoff）与恢复面（boot restore）SHALL 读同一地址门禁，与实时投递面的入口判定同源。

已登记的非入口地址正文义务（上一版或兄弟进程所留）SHALL 在恢复时被跳过投递，并以具名状态 `headless` 销账；该记录 SHALL 保留正文与目标，供读者审计，SHALL NOT 因此被投递。

#### Scenario: 非入口地址的正文不登记也不投递

- **WHEN** 非入口 agent 地址上的 follow-up turn 完成并生成正文
- **THEN** SHALL NOT 登记交付义务，重启后消息渠道 SHALL 收不到该正文

#### Scenario: 遗留的非入口义务恢复时具名销账

- **WHEN** 恢复读到 target 为非入口地址的 owed 记录
- **THEN** SHALL NOT 投递该正文，该记录状态 SHALL 转为 `headless`

### Requirement: 未销义务在进程内补投

消费回执尚未落地的未销交付义务（批量登记中途失败、兄弟进程登记后崩溃、通道暂不可用）SHALL NOT 滞留到下次重启：交接面 SHALL 在该次通知的终态落地时，对其所驱动的通知 invocation 生成的正文与记录仍处 owed 状态的正文一并补投，SHALL NOT 只发送本次新登记的正文。

补投面 SHALL 由该次交接的通知身份界定：交接面只读该通知 invocation 生成的正文，因此只有该通知的消费回执尚未落地的义务落在此面内。消费回执已经落地而发送未发生的义务 SHALL NOT 被读作本 Requirement 的滞留：该通知不再被唤醒，其实例的恢复面是重启（见「残余窗口显式声明」）。

补投 SHALL 以记录状态为准：状态已为 `held`/`retry`/`sent`/`refused`/`headless` 的正文 SHALL NOT 被交接面重投。补投与 boot restore 的并发 SHALL 由 sent 回执的幂等交付与上条的至少一次容忍界住。

#### Scenario: 已登记未发送的义务由下次交接补投

- **WHEN** 某正文的义务已登记为 owed 而发送未发生，同一进程内该通知的下一次交接读到该记录
- **THEN** 该正文 SHALL 被补投一次，其记录 SHALL 转为 `sent`（通道接受且 sent 回执落地时；裁决挂起转 `held`、通道拒收转 `retry`、回执未落按至少一次容忍）

#### Scenario: 已销账的义务不被补投

- **WHEN** 交接读到的记录状态不是 owed
- **THEN** 该正文 SHALL NOT 被重投

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

两个「登记已完成、发送未发生」的窗口 SHALL 同处声明，其恢复面是重启，而不是进程内的补投面：

- 「消费回执已落但发送未发生」：通知的消费回执落地之后、正文发送之前进程停止，该通知不再被唤醒，义务记录仍为 owed，由重启的恢复面（boot restore）补投。
- 「用户轮登记后发送前」：用户 turn 的交付义务登记之后、发送之前进程停止，同一进程内没有会读到该义务的交接面，同样由重启的恢复面补投。

两者 SHALL 被读作已登记义务的恢复，措辞与 `settleTerminal` 的注释一致：登记与回执之间的停顿由重启投递的 owed 正文作答（"a stop between the two is answered by the owed body a restart delivers"）。

#### Scenario: 回执已落而发送未发生的义务由重启补投

- **WHEN** 通知的消费回执已落地而正文发送未发生，进程随后被强杀并重启
- **THEN** 该义务 SHALL 由重启的恢复面补投，本契约 SHALL NOT 把它读作进程内补投面的滞留

#### Scenario: 用户轮登记后发送前停止的义务由重启补投

- **WHEN** 用户 turn 的交付义务已登记而正文发送未发生，进程随后被强杀并重启
- **THEN** 该义务 SHALL 由重启的恢复面补投，进程内的交接面 SHALL NOT 被读作该义务的补投面

#### Scenario: 强杀后已登记义务恰一次投递

- **WHEN** Sender 未确认时进程被强杀并重启
- **THEN** 强杀前投递 SHALL 为 0 条，重启后该结果 SHALL 恰投递 1 条（该次重投的 sent 回执落地时；回执未落按至少一次容忍），持久账 `tagent/task/v1/delivery:` 前缀下的 owed 条目 SHALL 归零（该次重投的 sent 回执落地时），该结果的生成义务 SHALL 恰被消费一次

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
