# async-tool-event-fix Specification

## Purpose

本规范定义 async-tool-event-fix 能力。When `InjectBusInputs` appends an event message to `args.Request.Messages`, if `evt.Message.Role == model.RoleSystem`, it SHALL create a copy of the message wit

> **状态核验（2026-09-09，三分支状态分化）**：① Req1/Req4（RoleSystem→RoleUser 归一化）：名为 `InjectBusInputs` 的函数已不存在（仅日志串与测试名仍沿用该称谓），但**转换本身并未消失**——注入机制演进为统一 `NewExternalInputEvent`（`agent/inject.go`，`EmitSystemAlert` 仍以 `RoleSystem` 入总线），消费侧与 canonical 侧各有一处"复制 Message 后转 user"的实现（见本页首条需求的承载坐标）。原记"消息层 Role 转换不再需要"是**误判**，已于 2026-10-01 按码面更正；② Req2/Req3（user/external_input 开始新段的边界语义）**已被 task-skeleton-compression 推翻**——段边界现为 agent_output 闭合回合（`SegmentMessages` 现实现），`isMessageTaskBoundary`/`isReferenceTaskBoundary` 符号已移除；③ Req5（非交互命令 stable 即 completed）**有效**，由 `tool/action/settle.go` 三档分类（completed/stable/suspect）实现；Req6 相关符号（findPendingUserMessage）已随压缩重构移除，但「压缩产物不含引导消息」的行为由骨架模型保持（卡片行无引导语）。

## Requirements

### Requirement: 冻结批次消费时归一化外部消息角色

系统注入的外部输入 SHALL 在下一次有限拉取中被认领（非唤醒式排空＋durable 认领，不阻塞事件循环）。准备 canonical fact 与实际请求消息时 MUST 复制 Message 再把 `model.RoleSystem` 转为 `model.RoleUser`，不得原地改写原件，也不得把系统注入提升为模型系统指令。原始 source_event、Content、完整消息与来源 SHALL 保持不变；本就是 user 的消息语义不变。同步工具结果仍由框架在当前 ReAct 中处理，MUST NOT 因总线的批次边界而延后。

> 承载坐标（现行码面）：消费侧 `agent/context_manager.go` 的 BeforeModel 回调（`msg := *evt.Message` 后按角色转换，日志串仍作 `[InjectBusInputs]`）；canonical 侧同文件的 `buildBusFact`（注释明示"durable fact 的 canonical 形态只决定一次，重放不重推"）；注入源头 `agent/inject.go` 的 `EmitSystemAlert` 仍以 `RoleSystem` 经 `NewExternalInputEvent` 入总线；行为钉测 `tests/inject_bus_inputs_test.go`。**名为 `InjectBusInputs` 的函数已不存在，但本命题的行为仍在——不得据函数名消失判定能力消失。**

#### Scenario: 异步任务结果在当前轮执行中到达

- **WHEN** 当前 turn 执行期间到达一条 system-role 的异步任务结果
- **THEN** 原件留队至下一次拉取，消费时使用 user-role 副本，当前轮不认领它

#### Scenario: 原始消息不被改写

- **WHEN** 一条 system 消息经历准备、提交与重启
- **THEN** 原 source_event 的角色仍为 system，canonical 与模型侧为 user，内容与来源不被原地修改

#### Scenario: 普通用户输入与同步工具结果不受影响

- **WHEN** 处理 user 输入，或当前 ReAct 的同步工具结果
- **THEN** user 语义保持；同步工具结果照常参与当前轮，不受异步总线边界影响

### Requirement: SegmentMessages uses user input as boundary

`isMessageTaskBoundary` SHALL return true when `msg.Role == model.RoleUser`, instead of the current `msg.Role == model.RoleAssistant && len(msg.ToolCalls) == 0`. This makes each user input the start of a new "conversation turn", with all associated tool calls, results, and agent responses grouped in one segment.

#### Scenario: User input starts new segment

- **WHEN** messages contain [system, user("hello"), assistant(tool_call), tool(result), assistant("reply"), user("next"), assistant("ok")]
- **THEN** SegmentMessages SHALL produce 3 segments:
  - segment 0: [system] (incomplete)
  - segment 1: [user("hello"), assistant(tool_call), tool(result), assistant("reply")] (complete — ended by next user input)
  - segment 2: [user("next"), assistant("ok")] (incomplete — no next user input)

#### Scenario: System messages grouped with following user input

- **WHEN** messages start with [system_prompt, user("hello"), ...]
- **THEN** system_prompt SHALL be in the same segment as user("hello")

### Requirement: SegmentReferences uses external_input as boundary

`isReferenceTaskBoundary` SHALL return true when `ref.EventType == TypeExternalInput`, instead of the current `ref.EventType == TypeAgentOutput`. This aligns EventReference segmentation with message segmentation.

#### Scenario: External input starts new task group

- **WHEN** references contain [external_input, thinking_plan, action_command, agent_output, external_input, thinking_plan]
- **THEN** SegmentReferences SHALL produce 2 task groups:
  - group 0: [external_input, thinking_plan, action_command, agent_output] (ended by next external_input)
  - group 1: [external_input, thinking_plan]

### Requirement: Non-interactive commands complete on stable

In `detectSessionState`, when `!session.IsInteractive && !session.IsTUI` and `stableDuration >= threshold`, the session SHALL return `SessionCompleted` immediately. It SHALL NOT proceed to `fakeDeadDuration` or heartbeat detection.

#### Scenario: Non-interactive command reaches stable

- **WHEN** a non-interactive, non-TUI session's output is stable for >= stable_duration
- **THEN** detectSessionState SHALL return SessionCompleted
- **AND** SHALL NOT enter fakeDeadDuration/heartbeat detection

#### Scenario: Interactive command reaches stable

- **WHEN** an interactive session's output is stable for >= stable_duration
- **THEN** detectSessionState SHALL return SessionStable
- **AND** the existing fakeDeadDuration logic SHALL apply

#### Scenario: TUI command reaches stable

- **WHEN** a TUI session's output is stable for >= stable_duration
- **THEN** detectSessionState SHALL return SessionStable
- **AND** the existing TUI timeout logic SHALL apply

### Requirement: Compress does not append guidance message

When `findPendingUserMessage` returns nil (no pending user message found), `Compress` SHALL NOT append any guidance message (e.g., "以上是对话历史摘要"). The LLM SHALL rely on the summary and recentSegments to determine next steps.

#### Scenario: No pending user message

- **WHEN** findPendingUserMessage returns nil
- **THEN** no guidance message SHALL be appended to the result
- **AND** the result SHALL end with the last message from recentSegments (or execState if no recentSegments)

#### Scenario: Pending user message found

- **WHEN** findPendingUserMessage returns a message
- **AND** the message is not a duplicate (event key dedup passes)
- **THEN** the pending user message SHALL be appended to the result

### Requirement: Non-interactive commands complete on stable

In `detectSessionState`, when `!session.IsInteractive && !session.IsTUI` and the output has been stable for `stableDuration >= threshold`, the session SHALL return `SessionCompleted` immediately, without waiting for `fakeDeadDuration`. This prevents non-interactive commands (e.g., `curl`, `ls`) from being incorrectly classified as `fakeAlive` and restarted.

#### Scenario: Non-interactive command reaches stable

- **WHEN** a non-interactive, non-TUI session's output is stable for >= stable_duration
- **THEN** detectSessionState SHALL return SessionCompleted
- **AND** SHALL NOT enter fakeDeadDuration/heartbeat detection

#### Scenario: Interactive command reaches stable

- **WHEN** an interactive session's output is stable for >= stable_duration
- **THEN** the existing logic SHALL apply (stable → fakeDeadDuration → heartbeat)
- **AND** SHALL NOT be affected by this change

#### Scenario: TUI command reaches stable

- **WHEN** a TUI session's output is stable for >= stable_duration
- **THEN** the existing TUI logic SHALL apply (stable → fakeDeadDuration → TimedOut)
- **AND** SHALL NOT be affected by this change
