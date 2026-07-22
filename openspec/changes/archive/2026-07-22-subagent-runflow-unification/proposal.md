## Why

Sub-agent 调用(如 plan/knowledge/recall 被主 agent 当工具调用)当前通过 `agent.Run()` 起一个 goroutine 跑持久事件循环 `runEventLoop`,再**靠逐个探测事件流**猜测"turn 何时结束"(第一个无 tool_calls 的响应 + 500ms drain 定时器 + 强制 cancel context)。这个启发式有致命缺陷:它无法区分**工具结果事件**(`Role=tool`,同样没有 tool_calls)与**最终响应**,把工具结果误判为 turn 结束。慢 LLM(如 glm-5.2 每轮 ~16s)下 500ms drain 定时器必然先超时,导致 sub-agent 在**第一个工具调用后就返回**——plan agent 只跑了 `openspec init` 就退出,从未执行 `openspec new change`。

根因是架构不统一:**顶层 agent 与 sub-agent 都用同一个 turn 原语 `RunFlow`(一次输入→完整工具循环→最终响应),但 sub-agent 没有直接使用 `RunFlow` 的自然返回作为 turn 边界,而是套了一层为"持久反应式守护进程"设计的 `runEventLoop`,再反过来用事件探测伪造停止信号。**

## What Changes

- Sub-agent 的 `Run()` 不再起 goroutine 跑 `runEventLoop` + 事件探测,而是**直接调用一次 `RunFlow`**。`RunFlow` 阻塞至完整 turn 结束(所有工具轮次跑完 + 最终响应),事件自然流入输出 channel,`RunFlow` 返回即 turn 结束——这是与顶层 loop 共享的同一个 turn 原语。
- **删除**事件探测停止逻辑(`if len(ToolCalls) == 0` 块)、500ms drain 定时器、`runCancel` 强制取消、以及已失效的 `asyncTaskCheckers` 相关注释(该字段为死代码,ActionTool 已改为阻塞式)。
- 修复 `isFinalResponse` 的 role 缺陷:当前仅判断 `len(ToolCalls) == 0`,会把工具结果(`Role=tool`)误判为最终响应,导致 `RunFlow` 向 bus 回发多余的 `agent_output`。修正为仅 `Role=assistant` 且无 tool_calls 才算最终响应。
- 顶层持久循环(`persistent-event-loop`)行为**不变**:仍是 `for { Pull; RunFlow }` 反应式守护,仅在 `StopLoop` 取消 ctx 时退出。
- **BREAKING**: 无对外 API 破坏。`AgentToolWrapper.Call` 签名与返回契约不变;仅内部 turn 边界机制改变。

## Capabilities

### New Capabilities

- `subagent-turn-execution`: 规定 sub-agent 工具调用的执行语义——一次调用 = 一次 `RunFlow` = 一个完整 turn;turn 边界由 `RunFlow` 的自然返回定义,而非事件流探测;最终响应的识别必须区分 assistant 响应与工具结果。

### Modified Capabilities

<!-- 无 spec 级需求变更:remote-agent-communication 关注 context 传递,persistent-event-loop 关注顶层守护语义,二者行为不变。isFinalResponse 修复属于新 capability 内的正确性要求。 -->

## Impact

- **代码**:
  - `agent/session.go` — `Run()` 重写(删除 goroutine + 事件探测 + drain,改为直调 `RunFlow`)。
  - `agent/context_manager.go` — `isFinalResponse` 增加 `Role=assistant` 判断。
- **测试**:
  - `agent/session_subagent_toolstop_test.go` — 已有确定性复现测试(`TestSubAgentRun_SlowLLM_ToolResultStops`),修复后应通过。
  - `tests/plan_agent_create_behavior_test.go` — 真实 LLM 端到端验证 plan create 完整执行。
- **行为**: plan agent 及所有多轮工具的 sub-agent 可完整执行到最终响应,不再被慢 LLM + drain 竞态截断。
- **依赖**: 无新增依赖。
