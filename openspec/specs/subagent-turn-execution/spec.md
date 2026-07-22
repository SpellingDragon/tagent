# subagent-turn-execution Specification

## Purpose

定义 sub-agent 工具调用的执行语义:一次调用 = 一次 `RunFlow` = 一个完整 turn。turn 边界由 `RunFlow` 的自然返回定义,而非对输出事件流的内容探测。sub-agent 与顶层持久循环共享同一个 turn 原语 `RunFlow`,仅循环包裹方式不同(顶层 `for { Pull; RunFlow }` 反应式守护;sub-agent 直调一次)。

## Requirements

### Requirement: Sub-agent 调用执行恰好一个 turn

Sub-agent 工具调用(经 `AgentToolWrapper.Call` → `agent.Run()`)SHALL 通过**直接调用一次 `RunFlow`** 执行,而非运行持久事件循环 `runEventLoop` 后再探测事件流。一次 `RunFlow` = 一个完整 turn(单一输入 → 完整 ReAct 工具循环 → 最终响应)。turn 边界 SHALL 由 `RunFlow` 的返回定义,而非对输出事件的内容探测。

`Run()` SHALL NOT 使用 500ms drain 定时器、事件探测停止条件(`len(ToolCalls) == 0` 判断)或 `runCancel` 强制取消来确定 turn 结束。

#### Scenario: 多轮工具调用完整执行

- **WHEN** sub-agent 收到需要多次工具调用的请求(如 plan create 需依次执行 `openspec init`、`openspec new change`、写 tasks.md)
- **THEN** `RunFlow` 在单次调用内跑完所有工具轮次直到最终 assistant 响应
- **AND** sub-agent 不在任一中间工具结果处提前返回
- **AND** 输出 channel 在 `RunFlow` 返回后关闭

#### Scenario: 慢 LLM 不触发提前返回

- **WHEN** sub-agent 的 LLM 每轮响应耗时远超 500ms(如 glm-5.2 每轮约 16s)
- **THEN** sub-agent 仍执行到最终响应,不被任何定时器截断
- **AND** 首个工具调用后的后续轮次正常执行

#### Scenario: turn 边界由 RunFlow 返回定义

- **WHEN** `RunFlow` 内部的 event channel 关闭(Flow 在最终响应处结束)
- **THEN** 承载 `RunFlow` 的 goroutine 关闭 `invOutputCh`
- **AND** 调用方对输出 channel 的 `range` 循环自然结束
- **AND** 无需探测事件内容判断结束

### Requirement: 最终响应识别区分 assistant 响应与工具结果

判断一个事件是否为最终响应(`isFinalResponse`)SHALL 同时要求消息角色为 `assistant` **且** 无 tool_calls。工具结果事件(`Role=tool`)即使无 tool_calls,也 SHALL NOT 被识别为最终响应。

#### Scenario: 工具结果不被误判为最终响应

- **WHEN** 一个事件的消息 `Role=tool`(工具执行结果),无 tool_calls
- **THEN** `isFinalResponse` 返回 false
- **AND** `RunFlow` 不因此向 EventBus 回发 `agent_output` 事件

#### Scenario: assistant 最终消息被正确识别

- **WHEN** 一个事件的消息 `Role=assistant` 且无 tool_calls
- **THEN** `isFinalResponse` 返回 true
- **AND** `RunFlow` 向 EventBus 回发一个 `agent_output` 事件

#### Scenario: 带 tool_calls 的 assistant 响应不是最终响应

- **WHEN** 一个事件的消息 `Role=assistant` 且包含至少一个 tool_call
- **THEN** `isFinalResponse` 返回 false

### Requirement: Sub-agent 保持并发隔离与调用契约不变

每次 sub-agent 调用 SHALL 创建独立的 EventBus、SessionProjection 与 ContextManager(SmartCompressor 具有可变状态,不可跨并发调用共享)。`AgentToolWrapper.Call` 的输入参数与返回值契约 SHALL 保持不变。

#### Scenario: 每次调用组件隔离

- **WHEN** 同一 sub-agent 实例被并发或连续多次调用
- **THEN** 每次调用使用各自独立的 invBus / invProjection / invCM
- **AND** 调用之间不共享可变的压缩器状态

#### Scenario: 调用完成后恢复持久 bus

- **WHEN** 一次 sub-agent 调用的 `RunFlow` 返回
- **THEN** agent 的 activeBus 恢复为 persistentBus
- **AND** 后续 `InjectMessage` 路由回持久事件循环(若其处于活动状态)
