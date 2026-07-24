# async-tool-event-fix Specification

## Purpose

本规范定义 async-tool-event-fix 能力。When `InjectBusInputs` appends an event message to `args.Request.Messages`, if `evt.Message.Role == model.RoleSystem`, it SHALL create a copy of the message wit

## Requirements

### Requirement: InjectBusInputs converts RoleSystem to RoleUser

When `InjectBusInputs` appends an event message to `args.Request.Messages`, if `evt.Message.Role == model.RoleSystem`, it SHALL create a copy of the message with Role changed to `model.RoleUser` before appending. This ensures `[action_tool_result]` and other system-injected messages are treated as external input by the LLM, not as system instructions. The original event message and its content are preserved unchanged.

#### Scenario: action_tool_result injected during ReAct

- **WHEN** handleStateChange injects a message with Role=RoleSystem and Content="[action_tool_result] ..."
- **AND** InjectBusInputs TryPull receives this event
- **THEN** the message appended to args.Request.Messages SHALL have Role=RoleUser
- **AND** the Content SHALL be unchanged

#### Scenario: User message not affected

- **WHEN** InjectBusInputs receives a message with Role=RoleUser
- **THEN** the message SHALL be appended as-is without Role conversion

#### Scenario: Original event not mutated

- **WHEN** InjectBusInputs converts a RoleSystem message
- **THEN** the original evt.Message.Role SHALL remain RoleSystem (copy, not mutate)

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

### Requirement: InjectBusInputs converts RoleSystem to RoleUser

When `InjectBusInputs` appends an event message to `args.Request.Messages`, if `evt.Message.Role == model.RoleSystem`, it SHALL convert the Role to `model.RoleUser` before appending. This ensures `[action_tool_result]` and other system-injected messages are treated as external input by the framework's ReAct loop, not as system instructions. The original message content (including `[action_tool_result]` prefix) is preserved.

#### Scenario: action_tool_result injected during ReAct

- **WHEN** handleStateChange injects a message with Role=RoleSystem and Content="[action_tool_result] ..."
- **AND** InjectBusInputs TryPull receives this event
- **THEN** the message appended to args.Request.Messages SHALL have Role=RoleUser
- **AND** the Content SHALL be unchanged

#### Scenario: User message not affected

- **WHEN** InjectBusInputs receives a message with Role=RoleUser
- **THEN** the message SHALL be appended as-is without Role conversion

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
