## Context

Sub-agent 调用当前路径:`AgentToolWrapper.Call` → `agent.Run()` → 起 goroutine 跑 `runEventLoop`(为顶层持久反应式循环设计的 `for { Pull; BuildInvocation; RunFlow }`)→ 用第二个 wrapper goroutine **探测 `invOutputCh` 事件流**猜 turn 边界。

`runEventLoop` 是无限循环:`bus.Pull(ctx)` 消费初始消息、`RunFlow` 跑完一个 turn 后,循环回到 `Pull` 永久阻塞(sub-agent 只有一个输入)。因此 wrapper 必须靠事件探测 + 500ms drain + `runCancel` 强杀来终止。

已通过确定性测试(`TestSubAgentRun_SlowLLM_ToolResultStops`)与真实 glm-5.2 日志坐实:探测逻辑把**工具结果事件**(`Role=tool`,无 tool_calls)误判为最终响应,慢 LLM 下 500ms drain 先超时 → sub-agent 在首个工具调用后即返回。

关键前置事实(已核实):
- `RunFlow`(context_manager.go)内部 `for fwEvt := range eventCh` 抽干 runner 的事件流,trpc-agent-go 在**一次 `RunFlow` 内跑完所有 ReAct 工具轮次直到最终响应**。RunFlow 返回 = 一个完整 turn 结束。
- 事件经 `cm.onEvent` + `cm.outputCh`(sub-agent 即 `invOutputCh`)自然流出,无需 runEventLoop 中转。
- sub-agent turn 期间**无外部组件向 invBus 发布**:`InjectMessage`/meditation 均走 `persistentBus`(见 inject.go 注释),ReAct 中途注入由 `BeforeModel` 的 `cm.bus.TryPull()` 处理(invBus 恒空)。
- `asyncTaskCheckers` 字段为死代码(仅注释引用),ActionTool 已改阻塞式,无后台异步结果需等待。

## Goals / Non-Goals

**Goals:**
- Sub-agent 与顶层 agent 统一在同一个 turn 原语 `RunFlow` 上:turn 边界由 `RunFlow` 自然返回定义,而非事件探测。
- 删除脆弱的事件探测停止逻辑、500ms drain 竞态、`runCancel` 强杀、死代码注释。
- 修复 `isFinalResponse` 的 role 缺陷。
- 保持并发隔离(每次调用独立 invBus/invProjection/invCM)与 `AgentToolWrapper.Call` 对外契约不变。

**Non-Goals:**
- 不改动顶层持久循环 `persistent-event-loop` 的守护语义(仍 `for { Pull; RunFlow }`,仅 StopLoop 退出)。
- 不给 sub-agent 引入跨调用状态/常驻守护进程(请求-响应边界不适合守护模型)。
- 不引入 `singleTurn` 模式标志——"一个 turn" 由结构表达(直调 RunFlow 一次,无循环)。
- 不改 `AgentToolWrapper` 的 context 传递(`remote-agent-communication` 行为不变)。

## Decisions

### 决策 1: sub-agent 直调 `RunFlow` 一次,而非 `runEventLoop` + 事件探测

`Run()` 保留前置准备(userID/sessionID、external context、config 校验、创建隔离组件 invBus/invProjection/invCM、`SetSubAgentMode(true)`、`setActiveBus(invBus)`),然后:

```
go func() {
    defer close(invOutputCh)
    defer invCM.Close()
    defer ta.restorePersistentBus()
    invCM.SetTriggerSource("user")           // 保留触发源标记
    if err := invCM.RunFlow(ctx, message); err != nil {
        log.Errorf("[Run] sub-agent %q RunFlow failed: %v", ta.name, err)
    }
}()
return invOutputCh, nil                       // 调用方读到 channel 关闭即 turn 结束
```

- **删除**:初始消息的 `invBus.Publish`(改为直接把 `message` 传给 `RunFlow`)、wrapper 探测 goroutine、`if ToolCalls==0` 块、500ms drain 定时器、`runCancel`、asyncTaskCheckers 注释。
- `invOutputCh` 直接返回给调用方;RunFlow 返回 → goroutine `close(invOutputCh)` → 调用方 `for range` 自然结束。turn 边界 = channel 关闭 = RunFlow 返回。

**为何不用共享 `runTurn` 助手 / singleTurn 标志(替代方案)**:runEventLoop 的 `for` 循环本身就是"持久守护"语义;sub-agent 不需要循环,只需 turn 原语。直调 `RunFlow` 让"一个 turn"成为结构性事实(没有循环),而非循环体内的条件分支——比 singleTurn 标志更彻底地统一。

**为何保留 invBus(不删)**:`BeforeModel` Step 1 会 `cm.bus.TryPull()`;保留 invBus 使其有合法(空)bus 可拉,并让任何未来向 activeBus 发布的组件与 persistentBus 隔离。invBus 在 turn 期间恒空,仅承载自过滤的 agent_output 回声,无副作用。

### 决策 2: 修复 `isFinalResponse` 区分 assistant 响应与工具结果

`isFinalResponse`(context_manager.go)当前仅 `len(ToolCalls)==0`。工具结果(`Role=tool`)同样无 tool_calls,会被误判为最终响应,导致 `RunFlow` 向 bus 回发多余 `agent_output`。修正为:

```
return choice.Message.Role == model.RoleAssistant && len(choice.Message.ToolCalls) == 0
```

此修复对两条路径都正确(顶层循环也用 RunFlow → isFinalResponse)。

### 决策 3: 不为 sub-agent 保留 RunFlow 重试

`runEventLoop` 有 3 次退避重试;sub-agent 是一次性工具调用,RunFlow 失败时 goroutine 记录日志并关闭 channel,调用方(parent LLM)可重发工具调用。不在 sub-agent 内重放 RunFlow,避免已执行工具的副作用被重复触发(如 `openspec new change` 跑两次)。

## Risks / Trade-offs

- [直调 RunFlow 丢失 runEventLoop 的批量合并 BuildInvocation] → sub-agent 只有单一初始输入,批量合并本就退化为单条消息;直传 `message` 等价。已核实。
- [invBus 保留但不再被 Pull,初始消息不再入 bus] → 初始消息经 RunFlow→runner.Run 作为 invocation message 到达 LLM(框架 insertInvocationMessage),与原路径一致;不依赖 bus 持久化。
- [取消 500ms drain 可能丢失 tail 事件(如 MemoryPlugin 持久化)] → tail 事件在 RunFlow 的 `for range eventCh` 内已同步转发;RunFlow 返回时事件已全部发出,close channel 不丢事件。MemoryPlugin 持久化由其自身回调完成,不依赖 drain 窗口。
- [并发安全] → 每次 Call 仍创建独立 invBus/invProjection/invCM(SmartCompressor 隔离),与原设计一致,不受影响。
- [顶层循环回归风险] → 决策 2 的 isFinalResponse 修复影响顶层 RunFlow 的 agent_output 回发时机;需回归 persistent-event-loop 相关测试确认 agent_output 仍在真正最终响应时发出。

## Migration Plan

1. 修改 `agent/context_manager.go` 的 `isFinalResponse`(决策 2)。
2. 重写 `agent/session.go` 的 `Run()`(决策 1、3)。
3. 运行确定性测试 `TestSubAgentRun_ToolResultStopsPrematurely` + `TestSubAgentRun_SlowLLM_ToolResultStops`(应通过)。
4. 运行 `go test ./agent/`(回归,含 persistent-event-loop 相关)。
5. 真实 LLM 端到端:`TestPlanAgentCreateBehavior_RealPrompt`(glm-5.2)确认 plan create 执行到 `openspec new change` + 写 tasks.md。
6. 回滚策略:改动集中在两个函数,`git revert` 单个 commit 即可。

## Open Questions

- 无。方案已通过测试与日志充分验证,前置事实均已核实。
