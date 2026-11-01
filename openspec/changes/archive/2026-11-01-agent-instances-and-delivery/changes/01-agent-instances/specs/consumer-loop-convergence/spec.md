# consumer-loop-convergence Specification（delta：常驻实例推广到每 agent）

> 本文件为 MODIFIED delta。原条款"每会话一个常驻消费循环"的适用范围从入口 agent 扩为**每个 agent 地址**（Q5=a/Q9=a）；不等派发工具完成、决策有界等条款不变。

## ADDED Requirements

### Requirement: 每 agent 地址一个常驻消费实例

每个 agent 地址 SHALL 有一个常驻消费实例（每地址恰一，生命周期随 run 起停）；实例 SHALL 拉取已到达事件按类型分派、有驱动事件时调用一次模型、回到拉取。同步直驱路径（调用方阻塞等待目标回合完成）MUST NOT 作为常规驱动路径存在。

#### Scenario: 非入口地址被事件驱动

- **WHEN** 事件投递到非入口 agent 的会话且无任何调用方阻塞等待
- **THEN** 该地址的常驻实例驱动一轮并产出结果事实

#### Scenario: 每地址恰一实例

- **WHEN** 同一地址并发活动
- **THEN** 恰有一个消费实例，重复 Ensure 不产生第二实例

### Requirement: 委派经事件管线

子代理委派 SHALL 表达为向目标实例投递输入事件、结果以事件回流发起方责任范围；task 受理回执、Notice 身份与 settle 恰好一次语义 SHALL 与既有实现一致。main–sub 是数据编排关系（事件管线），MUST NOT 以调用阻塞关系实现。

#### Scenario: 委派不阻塞发起方

- **WHEN** main 经 call_subagent 委派
- **THEN** 发起方回合继续，sub 实例执行结果以事件回流并恰驱动发起方一次
