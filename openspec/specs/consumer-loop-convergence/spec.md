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

宿主入口 `Host.Invoke` 与 `fireNoticeTurn`（含 `InjectEvent` 经它的注入）SHALL 提交事实并唤醒消费循环，MUST NOT 直接启动 `Loop.Drive`。同步调用方（`Host.Invoke`）SHALL 等待本次回答的 `agent_output` 事实落链或返回受理收据。运行时直驱面（`Run.Invoke`/`Run.InjectEvent`，冥想注入与测试入口）不在本条收敛面内：它们保留直驱语义，与消费循环并存（见 design「兼容」）。

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
- **THEN** 第一个提交事实并等待回答；第二个提交事实并返回受理收据；消费循环在下一次拉取时合并处理
