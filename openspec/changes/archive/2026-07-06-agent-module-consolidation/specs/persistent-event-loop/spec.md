## MODIFIED Requirements

### Requirement: TagentAgent.runEventLoop replaces AgentLoop

`TagentAgent` SHALL have a private `runEventLoop(ctx context.Context)` method that implements the event loop. `AgentLoop` as a separate type SHALL NOT exist. `AgentLoopConfig` SHALL NOT exist.

`runEventLoop` SHALL:
1. Pull a batch of events from `EventBus`.
2. Call `ContextManager.BuildInvocation` to merge events into one message.
3. Call `ContextManager.RunFlow` to execute the framework Flow.
4. Loop back to step 1.

`StartLoop` SHALL launch `runEventLoop` in a dedicated goroutine. `Run()` SHALL create a temporary `ContextManager` and launch `runEventLoop` in a goroutine.

#### Scenario: StartLoop launches runEventLoop

- **WHEN** `StartLoop(userID, sessionID)` is called
- **THEN** `runEventLoop` is launched in a goroutine
- **AND** `loopActive` is true
- **AND** `outputCh` is returned

#### Scenario: Run() creates temporary ContextManager and launches runEventLoop

- **WHEN** `TagentAgent.Run(ctx, inv)` is called
- **THEN** a temporary `ContextManager` is created
- **AND** `runEventLoop` is launched in a goroutine with the temporary ContextManager
- **AND** the wrapped channel closes after the first final response

### Requirement: makeOnEventCallback only does projection.Append

`makeOnEventCallback` SHALL only perform `projection.Append` via `BuildEventReference`. `sessionService.AppendEvent` and `MemoryPlugin.OnEvent` are handled by the framework Runner internally.

#### Scenario: onEvent appends to projection only

- **WHEN** `onEvent` is called with a framework event
- **THEN** `projection.Append` is called
- **AND** `sessionService.AppendEvent` is NOT called
- **AND** `memPlugin.OnEvent` is NOT called

### Requirement: Sub-agent Run() creates ContextManager

`TagentAgent.Run` SHALL create a `ContextManager` for each sub-agent invocation.

#### Scenario: Sub-agent creates ContextManager

- **WHEN** `TagentAgent.Run` is called with a user message
- **THEN** a `ContextManager` is created with temporary bus, projection, and config
- **AND** `runEventLoop` is called for execution
## MODIFIED Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：drain EventBus（Pull）→ `ContextManager.BuildInvocation` 合并为一条 model.Message → `ContextManager.RunFlow` 执行框架 Flow → 回到 drain。

Loop SHALL 仅在 loopCtx 被取消（StopLoop）时退出。

`AgentLoop.Run` SHALL NOT 包含 Step 1 onEvent 调用。框架 `runner.Run` 内部完成所有持久化（`sessionService.AppendEvent` + `Plugin.OnEvent`）。`RunFlow` 内部的 `onEvent` 仅做 `projection.Append`。

#### Scenario: Flow 结束后继续 drain

- **WHEN** `ContextManager.RunFlow` 返回
- **THEN** Loop 不退出
- **AND** Loop 回到 EventBus.Pull 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

### Requirement: AgentLoop 单一路径执行

`AgentLoop.Run` SHALL 只通过 `ContextManager` 执行模型调用。`AgentLoop` 结构体 SHALL 仅包含：`bus`、`contextManager`、`onEvent`、`outputCh`、`name`。

`AgentLoop.Run` SHALL NOT 包含 onEvent Step 1 调用（框架 runner.Run 内部处理所有持久化）。

#### Scenario: AgentLoop delegates to ContextManager

- **WHEN** AgentLoop.Run pulls a batch with external_input events
- **THEN** `ContextManager.BuildInvocation` merges the events into one message
- **AND** `ContextManager.RunFlow` is called with the message
- **AND** no separate onEvent step for bus events

### Requirement: Sub-agent Run() creates ContextManager

`TagentAgent.Run` SHALL create a `ContextManager` for each sub-agent invocation.

#### Scenario: Sub-agent creates ContextManager

- **WHEN** `TagentAgent.Run` is called with a user message
- **THEN** a `ContextManager` is created with temporary bus, projection, and config
- **AND** `AgentLoop.Run` uses `ContextManager.RunFlow` for execution
## MODIFIED Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：drain EventBus（阻塞 Pull 第一个事件 + non-blocking drain 剩余）→ onEvent 持久化 external_input（`sessionSvc.AppendEvent` + `projection.Append`，不调用 `memPlugin.OnEvent`）→ `ContextManager.BuildInvocation` 合并为一条 model.Message → `ContextManager.RunFlow` 执行框架 Flow → 回到 drain。

Loop SHALL 仅在 loopCtx 被取消（StopLoop）时退出。`ContextManager.RunFlow` 内部由 `trpc-agent-go` 框架处理 ReAct 循环、工具执行和迭代控制。`AgentLoop` 不包含 `callModel`、`handleResponse`、`dispatchToolUse` 或 `Preprocessor.Process` 调用。

#### Scenario: Flow 结束后继续 drain

- **WHEN** `ContextManager.RunFlow` 返回（Flow 在 final response 时结束）
- **THEN** Loop 不退出
- **AND** Loop 回到 EventBus.Pull 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

### Requirement: AgentLoop 单一路径执行

`AgentLoop.Run` SHALL 只通过 `ContextManager` 执行模型调用。`TagentConfig` SHALL NOT 包含 `UseFrameworkFlow` 字段。`AgentLoop` SHALL NOT 包含 `callModel`、`handleResponse`、`dispatchToolUse` 方法或 `flowAdapter` 字段。`AgentLoop` SHALL NOT 持有 `m`、`tools`、`toolMap`、`toolIterations`、`maxToolIters`、`systemPrompt`、`temperature`、`preprocessor`、`session` 字段。

`AgentLoop` 结构体 SHALL 仅包含：`bus`、`contextManager`、`onEvent`、`outputCh`、`name`。

#### Scenario: AgentLoop delegates to ContextManager

- **WHEN** AgentLoop.Run pulls a batch with external_input events
- **THEN** onEvent is called for each external_input event (AppendEvent + projection.Append only)
- **AND** `ContextManager.BuildInvocation` merges the events into one message
- **AND** `ContextManager.RunFlow` is called with the message
- **AND** no `callModel` or `handleResponse` is invoked

### Requirement: Sub-agent Run() creates ContextManager

`TagentAgent.Run` SHALL create a `ContextManager` for each sub-agent invocation instead of using the legacy `Preprocessor` + `callModel` path. The `ContextManager` SHALL use a fresh `EventBus`, fresh `SessionProjection`, and a single `RunFlow` invocation.

#### Scenario: Sub-agent creates ContextManager

- **WHEN** `TagentAgent.Run` is called with a user message
- **THEN** a `ContextManager` is created with temporary bus, projection, and config
- **AND** `AgentLoop.Run` uses `ContextManager.RunFlow` for execution
- **AND** no legacy `callModel` or `handleResponse` code is invoked
