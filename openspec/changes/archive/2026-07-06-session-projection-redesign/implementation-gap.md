# Session-Projection Redesign: 实现偏差矩阵

> 本文档基于 tasks.md 中已完成修订的文档，梳理当前生产代码与设计目标之间的差距，作为代码修订的输入。

## 一、三层模型校验

| 不变量 | 设计目标 | 当前实现 | 是否满足 |
|--------|---------|---------|----------|
| ① inputs 是投影（有界，读写同一份数据） | `Session.Events []EventReference`，onEvent 追加、Preprocessor 读取、Compact 清理，操作同一份数据 | `Session.Events []event.Event`（完整事件含 `*model.Response`）；AgentLoop 维护独立 session copy | 不满足 |
| ② Compact 修改投影不修改事件流 | Compact 清理 `Session.Events` 旧引用，不碰 `MemoryStore` 和 `EventBus` | 无 Compact 机制；SmartCompressor 只压缩 `messages` 视图 | 不满足 |
| ③ 工具结果回写 bus 不直接操作 inputs | `dispatchToolUse` goroutine → `bus.Publish(external_input)` → 下一轮 Pull 消费 | 工具结果确实回写 bus，但 AgentLoop 同时维护 session copy，存在间接操作 | 部分满足 |

## 二、原型隐含设计要点校验

| 设计要点 | 设计目标 | 当前实现 | 是否满足 |
|----------|---------|---------|----------|
| 所有输出回写 bus | handleResponse 的 tool_use 和 final 都回写 bus/outputCh | tool_use 回写 bus；final 只发到 outputCh，**未回写 bus** | 部分满足 |
| model 作为工具 | 原型 `tools["model"] = ModelCompletion`；生产中 `model.Model.GenerateContent` 独立但本质一致 | model 独立为 `AgentLoop.callModel` | 满足（需文档保持一致） |
| 批量处理 | Pull 第一个事件后非阻塞取出所有剩余事件组成 batch，一轮处理多个事件 | `EventBus.Pull` 已实现批量取出 | 满足 |

## 三、详细偏差矩阵

### 3.1 Session.Events 存储类型

| 维度 | 设计目标 | 当前实现 |
|------|---------|---------|
| 类型 | `[]memory.EventReference` | `[]event.Event`（框架事件） |
| 大小 | 几十字节/条（key + type + summary） | 包含完整 `*model.Response`，可达成百上千字节/条 |
| 增长 | 有界，可被 Compact 清理 | 无界，持续增长 |
| 关键代码 | 待实现 | `agent/agent_loop.go:184-188`, `agent/agent_loop.go:391-398` 直接 append 完整 `event.Event` |
| 影响 | 投影有界，内存可控 | 14 小时日志：Session 从 1 条增至 130+ 条，22000+ tokens |

### 3.2 AgentLoop 维护 session copy

| 维度 | 设计目标 | 当前实现 |
|------|---------|---------|
| 读写关系 | 读写同一份 `Session.Events` | onEvent 写 `sessionSvc` 内部 session；AgentLoop 写自己的 `al.session` copy |
| 原因 | Session 存 `EventReference`，append 轻量可靠 | `sessionSvc.GetSession/CreateSession` 返回 clone，且 Session 存完整事件 |
| 关键代码 | 待实现 | `agent/tagent_agent.go:617-643` (`makeOnEventCallback`)；`agent/agent_loop.go:169-189`, `381-399` |
| 影响 | 一致性好，无数据竞争 | 读写分离，存在一致性风险；两个 session 视图可能不同步 |

### 3.3 Compact 机制缺失

| 维度 | 设计目标 | 当前实现 |
|------|---------|---------|
| 触发条件 | `Session.Events` 对应的 messages 经 SmartCompress 后仍超 token 预算 | 无 Compact |
| 清理策略 | 按任务边界切分 `Session.Events`，保留最近 N 个完整任务，旧引用替换为 summary reference | 无 |
| 作用对象 | `Session.Events`（投影） | 无 |
| 与 SmartCompressor 协作 | SmartCompress 先（视图），Compact 后（投影） | 只有 SmartCompress |
| 关键代码 | 待新增 | `agent/preprocessor.go:119-167` 只有 SmartCompress 调用 |
| 影响 | Session 投影保持有界 | Session 永不清理，无限增长 |

### 3.4 MaxToolIterations 默认值与角色

| 维度 | 设计目标 | 当前实现 |
|------|---------|---------|
| 主控制阀门 | Compact | 无 Compact |
| 兜底阀门 | MaxToolIterations（子 agent 默认 10） | MaxToolIterations 默认 200，被当成唯一阀门 |
| 字段复用 | 复用 `agent.Invocation.MaxToolIterations` | 复用 `agent.Invocation.MaxToolIterations` |
| 关键代码 | 待修正 | `agent/tagent_agent.go:140` (`DefaultMaxToolIterations = 200`)；`config.go:315` (`DefaultMaxToolIter = 200`) |
| 影响 | 子 agent 有界收敛 | action 子 agent 14 次重复调用同一命令直到 10 分钟超时 |

### 3.5 AgentLoop dispatch 时机

| 维度 | 设计目标 | 当前实现 |
|------|---------|---------|
| dispatch 位置 | Step 5: `handleResponse` 中检测到 tool_calls 后立即 dispatch | Step 1: Pull 后立即 dispatch 所有 tool_use 事件 |
| 与原型对应 | 对应 `OnEvents` 返回值回写 bus 后立即触发工具执行 | 工具执行延迟到下一轮 Pull |
| 关键代码 | 待重构 | `agent/agent_loop.go:159-167` (Step 1 dispatch)；`agent/agent_loop.go:290-366` (`handleResponse` 只 publish 不 dispatch) |
| 影响 | LLM 决策与工具执行同步；事件处理步骤与原型一致 | LLM 决策与实际状态不同步；同一轮内 tool_use 不立即执行 |

### 3.6 Preprocessor 从 EventReference 按需拉取

| 维度 | 设计目标 | 当前实现 |
|------|---------|---------|
| 输入 | `Session.Events []EventReference` | `Session.Events []event.Event` |
| 构建 messages | 最近引用从 `MemoryStore` 拉取完整 Content；旧引用直接用 `EventSummary` | 直接从 `event.Event.Response.Choices[0].Message` 取完整内容 |
| 关键代码 | 待实现 | `agent/preprocessor.go:92-103` |
| 影响 | 投影轻量，按需加载 | Session 必须存完整事件 |

### 3.7 tagent 与框架结合边界

| 维度 | 设计目标 | 当前实现 |
|------|---------|---------|
| ReAct 循环 | 复用框架 `Flow.Run` / `LLMAgent.Run` | 自建 `AgentLoop.Run` |
| messages 构建 | 复用 `ContentRequestProcessor` | 自建 `Preprocessor.Process` |
| 工具执行+迭代控制 | 复用 `FunctionCallResponseProcessor` | 自建 `dispatchToolUse` |
| 压缩 hook | 复用 `BeforeModel` 回调 | `Preprocessor` 内调 `SmartCompressor.Compress` |
| 关键代码 | 待深层重构 | `agent/agent_loop.go`, `agent/preprocessor.go`, `agent/tool_agent.go` |
| 影响 | 可减少 ~1000 行代码，获得框架 tracing/telemetry/jsonrepair | 重复造轮子，缺少框架能力 |

## 四、偏差影响分级

| 级别 | 偏差项 | 影响 |
|------|--------|------|
| P0（阻塞） | Session.Events 无限增长 | 内存爆炸，上下文溢出，子 agent 循环失控 |
| P0（阻塞） | 无 Compact 机制 | Session 永不清理，违反不变量 2 |
| P1（严重） | AgentLoop 维护 session copy | 一致性风险，调试困难 |
| P1（严重） | MaxToolIterations=200 且无 Compact | 子 agent 无法收敛 |
| P1（严重） | dispatch 时机偏离 | 事件处理语义与原型不一致 |
| P2（中等） | final response 未回写 bus | 不变量 3 部分不满足（文档已说明所有输出回写 bus） |
| P3（长期） | 未复用框架 Flow | 重复造轮子，长期维护成本高 |

## 五、修复优先级建议

1. **第一阶段（必选，止损）**：Session.Events 改为 EventReference + Compact 机制 + MaxToolIterations 默认值修正
2. **第二阶段（必选，语义对齐）**：消除 AgentLoop session copy + 修正 dispatch 时机
3. **第三阶段（可选，架构优化）**：复用框架 Flow / ContentRequestProcessor / FunctionCallResponseProcessor

