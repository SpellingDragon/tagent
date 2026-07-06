## Why

极限测试中发现冥想事件触发后三个运行机制问题：

1. **冥想空转**：冥想 prompt 引导 LLM "先回顾所有事件再分析"，但缺少"信息已充分"的判断指导。LLM 反复调用 `memory_recent` 获取相同结果（12 次完全相同的 3560 chars），空转 40+ 分钟。冥想应是有意义的记忆整理+行为优化+skill 积累，不是无限制的数据拉取。

2. **outputCh 满溢丢事件**：WeChat bot 的 `consumeUntilFinal` 是"一次性消费"——收到一个 final response 就退出。但 `runEventLoop` 是持续运行的，冥想、TmuxMonitor 回调等随时可能产出事件。后续多轮 recall 调用产出的事件无消费者，outputCh（cap=100）快速填满并丢弃事件——包括最终响应。

3. **recall_tool_desc.md 内容重复**：上轮写入产生了两份重复描述，LLM 可能看到困惑的 prompt。

## 根因分析

outputCh 的设计意图是**让应用侧持续感知 Agent 运行状态的窗口**——所有进入管道的事件都应被消费。当前问题不是"outputCh 太小"或"该丢弃什么"，而是**消费模式不对**：

- `consumeUntilFinal` 收到一个 final response 就退出 → 后续事件无消费者 → 堆积 → 满溢
- 正确模式：消费者持续消费，按事件类型做不同处理（agent_output 回复用户，thinking_plan 显示"正在思考"，action_command 显示执行进度）

## What Changes

- **outputCh 写入策略改为阻塞写入**：所有事件阻塞写入 outputCh（`select` + `ctx.Done()`），确保进入管道的事件都被消费，不丢失。保持 cap=100（消费者始终在消费，不需要增大）。
- **WeChat bot 消费模式改为持续消费**：`consumeUntilFinal` 改为持续消费 goroutine，按事件类型分发（agent_output → 回复用户，中间事件 → 日志/打字指示）。
- **冥想 prompt 重写**：改为"获取→判断充分性→分析→行动"的渐进式流程。增加"如果返回结果没有变化，说明没有更多可获取的信息，直接进入分析阶段"的指导。
- **recall agent prompt 优化**：增加信息充分性判断指导，明确"不要反复调用相同查询"。
- **recall_tool_desc.md 去重复**：修复上轮写入 bug。
- **AGENTS.md 增加工具调用约束**：同一工具连续调用相同参数时改变策略或停止。

## Capabilities

### New Capabilities

- `output-ch-blocking-write`: outputCh 阻塞写入策略——所有事件阻塞写入，ctx 取消时退出
- `continuous-event-consumption`: 应用侧持续消费 outputCh，按事件类型分发处理

### Modified Capabilities

- `persistent-event-loop`: 更新 RunFlow 中 outputCh 写入逻辑（阻塞写入）

## Impact

**代码变更范围**：
- `agent/context_manager.go` — `RunFlow` 中 outputCh 写入改为阻塞写入（`select` + `ctx.Done()`）
- `examples/wechat-bot/main.go` — `consumeUntilFinal` 改为持续消费 + 按事件类型分发
- `examples/wechat-bot/resources/prompts/meditation.md` — 重写冥想流程
- `examples/wechat-bot/resources/prompts/recall_agent.md` — 增加信息充分性指导
- `examples/wechat-bot/resources/prompts/recall_tool_desc.md` — 去重复内容
- `examples/wechat-bot/resources/prompts/AGENTS.md` — 增加工具调用约束

**文档变更范围**：
- `README.md` — 更新场景一（持久事件循环）描述，反映持续消费模式；更新"各模块视角"增加 outputCh 消费者视角

**不涉及**：
- SmartCompressor/Compactor 逻辑不变
- MemoryPlugin/SummaryPlugin 不变
- MeditationManager 触发逻辑不变
- outputCh cap 保持 100
## Why

极限测试中发现冥想事件触发后三个运行机制问题：

1. **冥想空转**：冥想 prompt 引导 LLM "先回顾所有事件再分析"，但缺少"信息已充分"的判断指导。LLM 反复调用 `memory_recent` 获取相同结果（12 次完全相同的 3560 chars），空转 40+ 分钟。冥想应是有意义的记忆整理+行为优化+skill 积累，不是无限制的数据拉取。

2. **outputCh 满溢丢事件**：冥想触发的 RunFlow 产出的中间事件（thinking_plan、action_command）写入 outputCh，但 WeChat bot 的 `consumeUntilFinal` 可能在冥想的第一轮就返回了。后续多轮 recall 调用产出的事件无消费者，outputCh（cap=100）快速填满并丢弃事件——包括最终响应。

3. **recall_tool_desc.md 内容重复**：上轮写入产生了两份重复描述，LLM 可能看到困惑的 prompt。

## What Changes

- **冥想 prompt 重写**：改为"获取→判断充分性→分析→行动"的渐进式流程。明确每次 recall 调用的目的，增加"如果返回结果没有变化，说明没有更多可获取的信息，直接进入分析阶段"的指导。
- **recall agent prompt 优化**：增加信息充分性判断指导，明确"不要反复调用相同查询"。
- **outputCh 写入策略**：final response 阻塞写入（确保消费者收到），非 final 事件可丢弃。增大 cap 到 256。
- **冥想事件标记**：冥想事件的 final response 标记 Source 为 `meditation_output`，使消费者可区分冥想响应和用户响应。
- **recall_tool_desc.md 去重复**：修复上轮写入 bug。
- **AGENTS.md 增加工具调用约束**：同一工具连续调用不超过 3 次相同参数。

## Capabilities

### New Capabilities

- `output-ch-write-strategy`: outputCh 写入策略——final response 阻塞写入，非 final 事件可丢弃，cap 增大
- `meditation-event-tagging`: 冥想事件 final response 标记 Source 为 meditation_output

### Modified Capabilities

- `persistent-event-loop`: 更新 RunFlow 中 outputCh 写入逻辑（阻塞写入 final response）

## Impact

**代码变更范围**：
- `agent/context_manager.go` — `RunFlow` 中 outputCh 写入策略改为：final response 阻塞写入，非 final 非阻塞
- `agent/tagent_agent.go` — outputCh cap 从 100 增到 256
- `examples/wechat-bot/resources/prompts/meditation.md` — 重写冥想流程
- `examples/wechat-bot/resources/prompts/recall_agent.md` — 增加信息充分性指导
- `examples/wechat-bot/resources/prompts/recall_tool_desc.md` — 去重复内容
- `examples/wechat-bot/resources/prompts/AGENTS.md` — 增加工具调用约束

**不涉及**：
- SmartCompressor/Compactor 逻辑不变
- MemoryPlugin/SummaryPlugin 不变
- MeditationManager 触发逻辑不变
