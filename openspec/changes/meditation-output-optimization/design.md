## Context

outputCh 是 tagent 持久事件循环对外暴露的输出通道。设计意图是让应用侧持续感知 Agent 的完整运行过程——包括思考（thinking_plan）、工具执行（action_command）和最终响应（agent_output）。

当前问题：WeChat bot 的 `consumeUntilFinal` 是"一次性消费"——收到一个 final response 就退出。但 `runEventLoop` 持续运行，冥想/TmuxMonitor 随时可能产出事件。消费者退出后产出的事件堆积 → outputCh 满溢 → 丢弃。

核心原则：**进入 outputCh 的事件必须被消费**。不是通过丢弃解决满溢，而是通过持续消费消除堆积。

约束：
- 不丢弃任何事件（阻塞写入 + 持续消费）
- 不限制冥想的工具调用次数（靠 prompt 引导）
- 不修改 SmartCompressor/Compactor 逻辑
- 保持 outputCh cap=100（消费者始终在消费，不需要增大）
- 保持冥想产出被 MemoryPlugin 持久化的能力

## Goals / Non-Goals

**Goals:**
- outputCh 阻塞写入：确保所有事件都被消费者接收
- 应用侧持续消费：按事件类型分发，不再"收到一个 final 就退出"
- 通过 prompt 引导模型判断信息充分性，避免冥想空转
- 更新文档反映持续消费的交互模式

**Non-Goals:**
- 不增大 outputCh cap（消费者始终在消费，100 足够）
- 不限制冥想的工具调用次数
- 不修改 SmartCompress 压缩逻辑
- 不修改冥想触发条件

## Decisions

### D1: outputCh 阻塞写入 — 所有事件都进入管道

**决策**：`RunFlow` 中写入 outputCh 时，所有事件都使用阻塞写入：

```go
select {
case cm.outputCh <- fwEvt:
case <-ctx.Done():
    // ctx 取消，放弃写入
}
```

不再区分 final/non-final，不再有 `default` 丢弃分支。

**理由**：
- outputCh 的设计意图是让应用侧感知完整运行过程
- 阻塞写入保证进入管道的事件都被消费
- 消费者始终在消费（D2），阻塞不会持续太久
- ctx 取消（StopLoop / 重试超时）是退出阻塞的安全阀

**安全保证**：
- `StartLoop` 返回 outputCh 给调用方，调用方必须持续消费
- `StopLoop` 关闭 outputCh → `runEventLoop` 退出 → 不再写入
- 阻塞写入在 ctx 取消时退出，不会永久阻塞

### D2: 持续消费 — 按事件类型分发

**决策**：WeChat bot 的 `consumeUntilFinal` 改为持续消费模式：

```go
// 持续消费 goroutine（StartLoop 后启动）
go func() {
    for evt := range outputCh {
        // 按事件类型分发
        if evt.IsFinalResponse() {
            // agent_output — 判断是否是用户消息的响应
            // 是 → 回复用户
            // 否（冥想等）→ 记录日志
        } else {
            // 中间事件 — 可选展示
            // thinking_plan → 继续打字指示
            // action_command → 日志显示执行进度
        }
    }
}()

// 用户消息处理：
// InjectMessage(用户消息) → 通知持续消费者"等待下一个 agent_output"
// 持续消费者收到 agent_output → 判断是否是当前用户消息的响应 → 回复
```

**理由**：
- 持续消费确保 outputCh 不会堆积
- 按事件类型分发让应用侧有完整可见性
- 冥想事件的 agent_output 被记录而非回复用户

**实现方式**：
- 在 `bot.OnMessage` 中 `InjectMessage` 后，通过一个 `responseCh chan string` 等待持续消费者发送的 agent_output
- 持续消费者 goroutine 在 `StartLoop` 后启动，持续读取 outputCh
- 收到 agent_output 时判断：如果当前有等待中的用户消息 → 回复；否则 → 记录日志

### D3: 冥想 prompt 重写 — 渐进式流程

**决策**：重写 meditation.md，改为"获取→判断→分析→行动"：

1. **获取**：recall 获取最近事件（1-2 次调用，有针对性）
2. **判断**：评估信息是否充分（"如果结果没有变化，直接进入分析"）
3. **分析**：基于已获取信息做分析，不再调 recall
4. **行动**：如需要，用 action 创建/更新 skill

关键指导：
- "如果 recall 返回了结果，基于结果直接分析，不要重复调用相同的查询"
- "每次 recall 调用应该有明确的不同目的"
- "冥想的目标是整理记忆和优化行为，不是无限制地拉取数据"

### D4: recall agent prompt — 信息充分性指导

增加段落：
- memory_recent 一次调用即返回最近事件，不需要反复调用
- 如果两次调用返回相同结果，说明没有新信息
- 优先使用 recall_query 按类型/关键词过滤

### D5: AGENTS.md — 全局工具调用约束

增加："Tool Call Discipline: 连续调用同一工具相同参数时改变策略或停止调用。"

### D6: recall_tool_desc.md 去重复

重写，去除上轮写入产生的重复内容。

## Risks / Trade-offs

- **[阻塞写入可能延迟 RunFlow 退出]** → 消费者始终在消费，阻塞通常很短。ctx 取消是安全退出。
- **[持续消费 goroutine 生命周期]** → 在 StartLoop 后启动，StopLoop（outputCh 关闭）后退出。与 runEventLoop 生命周期一致。
- **[冥想 agent_output 和用户 agent_output 区分]** → 消费者通过判断"当前是否有等待中的用户消息"来区分。如果没有等待中的用户消息，agent_output 是冥想/内部触发的，记录日志。
- **[prompt 引导不保证 LLM 遵循]** → 但 AGENTS.md 全局约束 + 冥想 prompt 明确指导应大幅减少空转。
## Context

冥想是 tagent 的工程化记忆整理机制，目标包括记忆整理、行为优化和 skill 积累。极限测试中发现冥想触发后存在空转、事件丢失和 prompt 重复三个问题。

冥想的多轮执行是符合预期的——模型需要多次调用工具完成分析+skill 操作。关键是要确保每轮调用都有新信息或新进展，不空转。

约束：
- 不限制冥想的工具调用次数（模型应有自主判断能力）
- 不修改 SmartCompressor/Compactor 逻辑
- 不修改 MeditationManager 触发逻辑
- 保持冥想产出被 MemoryPlugin 持久化的能力

## Goals / Non-Goals

**Goals:**
- 通过 prompt 引导模型判断信息充分性，避免反复调用相同查询
- 保证 final response 不被 outputCh 满溢丢弃
- 增大 outputCh 容量减少非关键事件丢弃
- 区分冥想响应和用户响应，使消费者可正确处理

**Non-Goals:**
- 不限制冥想的工具调用次数（靠 prompt 引导而非硬性限制）
- 不修改 SmartCompress 压缩逻辑（频繁 summary 是预期行为）
- 不修改冥想触发条件（interval/min_gap 不变）

## Decisions

### D1: outputCh 写入策略 — final response 阻塞，非 final 可丢弃

**决策**：`RunFlow` 中写入 outputCh 时区分事件类型：
- **Final response**（`isFinalResponse(fwEvt) == true`）：使用 `select` + `ctx.Done()` 阻塞写入，确保消费者收到
- **非 final 事件**（thinking_plan、action_command 等）：保持当前非阻塞 `select + default` 丢弃策略

```go
if isFinalResponse(fwEvt) {
    select {
    case cm.outputCh <- fwEvt:
    case <-ctx.Done():
        // ctx 取消，放弃写入
    }
} else {
    select {
    case cm.outputCh <- fwEvt:
    default:
        log.Warnf("[ContextManager:%s] outputCh full, dropping non-final event", cm.name)
    }
}
```

**理由**：
- Final response 是消费者等待的关键事件，不能丢弃
- 非 final 事件（中间推理、工具调用）丢失不影响功能正确性
- 阻塞写入在 ctx 取消时退出，不会永久阻塞

### D2: outputCh cap 增大到 256

**决策**：`NewTagentAgent` 中 outputCh cap 从 100 增到 256。

**理由**：
- 冥想多轮执行（10+ 次工具调用）可能产出 40-60 个中间事件
- 256 足够覆盖一轮完整的冥想+用户交互
- 不使用无界 channel，避免内存风险

### D3: 冥想 prompt 重写 — 渐进式流程

**决策**：重写 meditation.md，改为"获取→判断→分析→行动"的渐进式流程：

1. **获取**：recall 获取最近事件（1-2 次调用，有针对性）
2. **判断**：评估信息是否充分，明确"如果结果没有变化，直接进入分析"
3. **分析**：基于已获取信息做分析，不再调 recall
4. **行动**：如需要，用 action 创建/更新 skill

关键新增指导：
- "如果 recall 返回了结果，基于结果直接分析，不要重复调用相同的查询获取相同结果"
- "每次 recall 调用应该有明确的不同目的（如：第一次获取最近事件，第二次按类型过滤特定事件）"
- "冥想的目标是整理记忆和优化行为，不是无限制地拉取数据"

### D4: recall agent prompt — 信息充分性指导

**决策**：在 recall_agent.md 中增加"信息充分性判断"段落：
- memory_recent 一次调用即返回最近事件，不需要反复调用
- 如果两次调用返回相同结果，说明没有新信息
- 优先使用 recall_query 按类型/关键词过滤，而非 memory_recent 获取全部

### D5: AGENTS.md — 全局工具调用约束

**决策**：在 AGENTS.md 的 Mandatory Constraints 中增加：
- "Tool Call Discipline: 连续调用同一工具不超过 3 次相同参数。如果相同参数返回相同结果，改变策略或停止调用。"

### D6: recall_tool_desc.md 去重复

**决策**：重写 recall_tool_desc.md，去除上轮写入产生的重复内容。

## Risks / Trade-offs

- **[阻塞写入 final response 可能延迟 RunFlow 退出]** → 如果消费者已退出，阻塞在 `ctx.Done()` 上。但 ctx 会在 `runEventLoop` 的重试逻辑或 StopLoop 时取消，不会永久阻塞。
- **[cap=256 增加内存]** → 每个事件约 1-2KB，256 个约 512KB，可接受。
- **[prompt 引导不保证 LLM 遵循]** → LLM 可能仍然空转。但 AGENTS.md 的全局约束 + 冥想 prompt 的明确指导应该大幅减少空转概率。
