# consumer-loop-convergence Specification

## Purpose

本规范定义每会话常驻消费循环的收敛面：输入入口只提交事件并唤醒循环、工具派发后不等结果、回合输出闭合上下文段、运行状态取代地址 gate。

## Requirements

### Requirement: 每会话一个常驻消费循环

每个 `(agent, session)` 地址 SHALL 有一个常驻消费 goroutine，行为为：拉取已到达事件 → 按事件类型分派 → 有驱动事件时调用一次模型 → 回到拉取。消费循环 SHALL NOT 在一次模型调用返回后退出，SHALL NOT 等待全部派发工具完成。

#### Scenario: 慢工具期间新输入被回答
- **WHEN** 消费循环已派发一个慢工具且尚未返回，同时用户发送新消息
- **THEN** 该消息 SHALL 被下一次拉取读到并进入模型请求，SHALL NOT 等待慢工具返回

#### Scenario: 同会话不同来源合批
- **WHEN** 同一 session 的消费循环拉取到用户消息与一个已完成的任务终态结果，来源 InvocationID 不同
- **THEN** 两者 SHALL 允许进入同一次模型请求，各自来源关联 SHALL 保留

#### Scenario: 快结果先到先消费
- **WHEN** 消费循环派发了快慢两个工具，快结果先到达
- **THEN** 快结果 SHALL 被下一次拉取读到并驱动模型，SHALL NOT 等待慢结果

### Requirement: 输入入口只提交事件

所有正式输入入口，包括 `Host.Invoke`、`Run.Invoke`、`Run.InjectEvent`、`fireNoticeTurn` 与模型可见 send，SHALL 提交事实并唤醒目标地址的常驻消费循环，MUST NOT 保有第二驱动入口（直驱入口已内化为非导出），SHALL NOT 保留任何直驱例外。同步调用方 SHALL 等待其输入所属闭合段的不可变结果；send 回执 SHALL 仅表示目标事实已持久提交。

#### Scenario: 用户消息到达
- **WHEN** 用户发送消息经 `Host.Invoke` 提交
- **THEN** 消息 SHALL 落链并唤醒消费循环；消费循环的下一次拉取读到该消息并调用模型

#### Scenario: 冥想注入到达
- **WHEN** 冥想注入经宿主面提交
- **THEN** 注入 SHALL 落链并唤醒消费循环，MUST NOT 启动独立 Drive

### Requirement: 工具派发后不等结果

`dispatch` SHALL 派发全部工具调用后立即返回，MUST NOT 逐个等待结果。工具结果以事件回到消费循环的拉取面。

#### Scenario: 工具结果回到队列
- **WHEN** 一个已派发的工具完成并产生结果
- **THEN** 结果 SHALL 以事件回到消费循环的拉取面，下一次拉取读到并组装进模型上下文

### Requirement: A/B 闭合

模型产出不含工具调用的最终回复时，消费循环 SHALL 立即 commit `agent_output` 事实。该事实闭合当前 turn 的上下文段。后续到达的输入属于下一段。

#### Scenario: A 期间 B 落链
- **WHEN** 模型正在为问题 A 生成回答，用户补充 B 落链
- **THEN** 对 A 的回答闭合已完成部分（commit agent_output）
- **AND** B SHALL 属于下一段，在下一次消费批次中处理，MUST NOT 被已闭合段误标为已完成

### Requirement: gate 与 Occupancy 收敛

`addressGate`（token + inflight + hold/release/running）SHALL 删除。冥想让路 SHALL 读消费循环的运行状态（该会话是否有模型请求正在执行）。

#### Scenario: 冥想让路读消费状态
- **WHEN** 冥想 pass 评估时观察的会话有一个模型请求正在执行
- **THEN** 冥想 SHALL 让路（保持 episode，按既有节奏重评）

#### Scenario: 并发两请求同 session
- **WHEN** 两个并发 `Host.Invoke` 到达同一 session
- **THEN** 两者 SHALL 各自提交自己的事实，并获得实际消费该输入的闭合段之同一不可变结果；MUST NOT 存在只取得受理收据而无后续答案出口的调用方

### Requirement: 慢工具不阻塞新输入

慢工具在跑时，新的重要输入 SHALL 在规定边界进入下一次有效决策；工具结果 SHALL 回到正确责任范围（所属 Task），MUST NOT 因等待结果而阻塞消费循环。

#### Scenario: 慢工具在跑新输入仍进决策

- **WHEN** 一个慢工具未结束时新输入到达
- **THEN** 新输入按时进入下一次决策，慢工具结果随后回到其所属 Task

### Requirement: 多来源合批不混淆责任

同会话多来源输入 SHALL 按合批语义进一次决策，结果 SHALL 回到各自正确责任范围；合批 MUST NOT 混淆任务归属。

#### Scenario: 合批结果各归其位

- **WHEN** 同会话两个来源的输入合批处理
- **THEN** 各结果回到各自责任范围

### Requirement: 重入输入恰一次

经 `send{target:{task}}` 重入的输入 SHALL 作为消费循环普通输入进入下一次决策恰一次，MUST NOT 重复消费。

#### Scenario: 重入输入不重不漏

- **WHEN** 重入输入与普通输入同批到达
- **THEN** 重入输入恰被消费一次

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
