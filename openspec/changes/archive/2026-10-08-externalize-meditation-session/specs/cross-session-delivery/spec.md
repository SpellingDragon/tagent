# cross-session-delivery Specification

## Purpose

进程内跨 agent 的窄投递面：外部化冥想的产出以 `meditation` 谱系回流到目标 agent 的既有输入入口。投递=寻址+授权，不是第二消息总线；目标侧零新代码。

## ADDED Requirements

### Requirement: 投递前白名单校验且空表拒绝一切

投递 API SHALL 在投递前校验目标 agent 名 ∈ 投递方配置的 `deliver_to` 白名单；白名单未配置（空表）SHALL 拒绝一切投递（fail-closed）。`deliver_to` 的每个目标的分区 SHALL 属于投递方观察面（装配期校验，盲投——向从未观察过的 agent 投递——具名拒绝）。被拒 SHALL 返回具名错误，MUST NOT 静默丢弃。

#### Scenario: 白名单外目标被拒

- **WHEN** 冥想 agent 向不在其 `deliver_to` 内的 agent 投递
- **THEN** 返回具名白名单拒绝错误，目标零感知

#### Scenario: 未配置白名单

- **WHEN** `deliver_to` 未配置（缺省空）
- **THEN** 任何投递都被具名拒绝——默认关且 fail-closed

### Requirement: 目标 loop 未运行具名拒绝

目标 agent 的常驻循环未运行时，投递 SHALL 返回具名"目标未运行"错误，由投递方自决重试/退避；MUST NOT 触发 one-shot 回退路径（绕过 loopActive 语义自行起新 Run 属禁止形态）、MUST NOT 落盘等待（不绑 reliability 开关）、MUST NOT 静默丢弃。

#### Scenario: 目标未启动

- **WHEN** 目标 agent 存在于常驻表但其 loop 未启动
- **THEN** 投递返回具名错误（含目标名与状态），投递方可观测并自决

#### Scenario: 不走 one-shot 回退

- **WHEN** 投递到达一个 loopActive=false 的目标
- **THEN** 不产生任何新 Run/新 goroutine——错误返回是唯一副作用

### Requirement: 投递收口于既有注入入口并保持 meditation 谱系

投递 SHALL 经目标 agent 的既有 `InjectMessageWithSource("meditation", …)` 单入口进入其 mailbox；目标侧的谱系语义 SHALL 全部沿用既有契约：消息在目标侧是 meditation 谱系输入——可投递宿主（外显）、遥测审计计自管流量、**不 re-arm 目标的 novelty 门**。投递消息 SHALL 自带足够上下文（来源 agent、所涉事件键），目标无需回查即可理解。

#### Scenario: 目标正常消费投递

- **WHEN** 目标 loop 运行中且收到一条投递
- **THEN** 消息经 mailbox 串行进入下一个 turn；该 turn 在目标遥测审计中计自管流量

#### Scenario: 投递不喂肥目标新鲜度

- **WHEN** 投递消息持续到达目标而无真实用户输入
- **THEN** 目标自身的冥想 novelty 门不被这些投递 re-arm——投递是供给不是刺激

#### Scenario: 与用户输入同批让位

- **WHEN** 投递消息与 `source=user` 输入同批进入目标事件循环
- **THEN** 沿用既有混合批次条款被移除且不补偿（用户优先）；投递 API 的成功语义 SHALL 为"已进入 mailbox"而非"已触发 turn"——让位是合法结局，不构成投递失败

### Requirement: 寻址范围限定进程内常驻表

投递的寻址面 SHALL 限定同进程内经组合根装配的 agent 常驻表；跨进程/跨机器投递 MUST NOT 由本能力承担（既有 HTTPAPI 面继续承担外部注入，本能力不新增网络面）。目标名不存在于常驻表 SHALL 返回具名错误。

#### Scenario: 目标名不存在

- **WHEN** 投递目标名不在进程内常驻表
- **THEN** 具名"未知目标"错误，无任何网络调用发生
