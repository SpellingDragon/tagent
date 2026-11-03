# framework-flow-adapter Specification

## Purpose

本规范定义 framework-flow-adapter 能力。`FrameworkFlowAdapter` SHALL accept a `[]*AgentEvent` batch, a `*session.Session`, and a `*SessionProjection`.

## Requirements

### Requirement: Adapter converts a batch of AgentEvents into a framework Invocation

`FrameworkFlowAdapter` SHALL accept a `[]*AgentEvent` batch, a `*session.Session`, and a `*SessionProjection`. It SHALL construct an `agent.Invocation` whose `Message` is the mergeBatch result of all external_input events in the batch (filtering out `Source == "agent_output"` echoes), and whose `Session` points to the provided session. The adapter SHALL NOT mutate the EventBus or projection directly during conversion.

#### Scenario: Single user message batch

- **WHEN** the adapter receives a batch containing one `external_input` event with content "hello"
- **THEN** it produces an `agent.Invocation` with `Message.Content == "hello"` and `Message.Role == RoleUser`
- **AND** `Invocation.Session` is the provided session

#### Scenario: Multi-event batch merge

- **WHEN** the adapter receives a batch with a system event "tmux done" and a user event "result?"
- **THEN** it produces an `agent.Invocation` with `Message.Content == "tmux done\n\n---\n\nresult?"` and `Message.Role == RoleUser`

#### Scenario: Agent_output echo is filtered from Invocation

- **WHEN** the adapter receives a batch containing an `external_input` event with `Source == "agent_output"`
- **THEN** that event SHALL NOT be included in the merged `Invocation.Message`
- **AND** the event SHALL still be persisted via `onEvent` before the adapter runs

### Requirement: Adapter executes the framework Flow and forwards outputs

`FrameworkFlowAdapter.RunFlow` SHALL call `LLMAgent.Run` / `Flow.Run` with the constructed invocation. It SHALL consume the returned event channel and:

1. Forward every event to `TagentAgent.outputCh`.
2. Write `agent_output` events (assistant responses without tool_calls) back to the `EventBus` as `external_input` events with `Source == "agent_output"` so they are persisted and projected.
3. Return only after the event channel is closed.

#### Scenario: Final response from Flow

- **WHEN** the framework Flow returns a single assistant response event without tool_calls
- **THEN** the event is sent to `outputCh`
- **AND** an `external_input` event with `Source == "agent_output"` is published to the bus

#### Scenario: Tool call from Flow

- **WHEN** the framework Flow returns an assistant response containing tool_calls
- **THEN** the assistant response is sent to `outputCh`
- **AND** the adapter does NOT publish it back to the bus as agent_output
- **AND** the framework executes the tools synchronously inside Flow.Run

### Requirement: Adapter preserves tmux asynchronous semantics

When a tool call inside the framework Flow returns a tmux-async marker, `FrameworkFlowAdapter` SHALL let the Flow complete without blocking. The tmux result SHALL arrive later via `InjectMessage` / `EventBus`, trigger the next bus Pull, and be fed into a new `Flow.Run` invocation.

#### Scenario: Tmux async tool call

- **WHEN** a tool returns an `IsTmuxAsync()` marker
- **THEN** the current Flow.Run returns
- **AND** the adapter does not publish a tool_result to the bus
- **AND** when `InjectMessage` later publishes the tmux result, the adapter invokes Flow.Run again with the updated projection

### Requirement: Adapter supports sub-agent invocation mode

For `TagentAgent.Run`, `FrameworkFlowAdapter` SHALL use a fresh `EventBus`, fresh `SessionProjection`, and a single `Flow.Run` invocation. It SHALL stop consuming the Flow event channel after the first non-tool-call response (final response) and close the wrapped output channel.

#### Scenario: Sub-agent single-turn Flow

- **WHEN** `TagentAgent.Run` is called with a user message
- **THEN** the adapter creates a temporary bus and projection
- **AND** invokes Flow.Run once
- **AND** closes the output channel after the first final response

### Requirement: Adapter converts a batch of AgentEvents into a framework Invocation

`FrameworkFlowAdapter` SHALL accept a `[]*AgentEvent` batch, a `*session.Session`, and a `*SessionProjection`. It SHALL construct an `agent.Invocation` whose `Message` is the mergeBatch result of all external_input events in the batch, and whose `Session` points to the provided session. The adapter SHALL NOT mutate the EventBus or projection directly during conversion.

#### Scenario: Single user message batch

- **WHEN** the adapter receives a batch containing one `external_input` event with content "hello"
- **THEN** it produces an `agent.Invocation` with `Message.Content == "hello"` and `Message.Role == RoleUser`
- **AND** `Invocation.Session` is the provided session

#### Scenario: Multi-event batch merge

- **WHEN** the adapter receives a batch with a system event "tmux done" and a user event "result?"
- **THEN** it produces an `agent.Invocation` with `Message.Content == "tmux done\n\n---\n\nresult?"` and `Message.Role == RoleUser`

### Requirement: Adapter executes the framework Flow and forwards outputs

`FrameworkFlowAdapter.RunFlow` SHALL call `LLMAgent.Run` / `Flow.Run` with the constructed invocation. It SHALL consume the returned event channel and:

1. Forward every event to `TagentAgent.outputCh`.
2. Write `agent_output` events back to the `EventBus` as `external_input` events with `Source == "agent_output"` so they are persisted and projected.
3. Return only after the event channel is closed.

#### Scenario: Final response from Flow

- **WHEN** the framework Flow returns a single assistant response event
- **THEN** the event is sent to `outputCh`
- **AND** an `external_input` event with `Source == "agent_output"` is published to the bus

#### Scenario: Tool call from Flow

- **WHEN** the framework Flow returns an assistant response containing tool_calls
- **THEN** the assistant response is sent to `outputCh`
- **AND** the adapter does NOT publish it back to the bus as agent_output
- **AND** the framework executes the tools synchronously inside Flow.Run

### Requirement: Adapter preserves tmux asynchronous semantics

When a tool call inside the framework Flow returns a tmux-async marker, `FrameworkFlowAdapter` SHALL let the Flow complete without blocking. The tmux result SHALL arrive later via `InjectMessage` / `EventBus`, trigger the next bus Pull, and be fed into a new `Flow.Run` invocation.

#### Scenario: Tmux async tool call

- **WHEN** a tool returns an `IsTmuxAsync()` marker
- **THEN** the current Flow.Run returns
- **AND** the adapter does not publish a tool_result to the bus
- **AND** when `InjectMessage` later publishes the tmux result, the adapter invokes Flow.Run again with the updated projection

### Requirement: Adapter supports sub-agent invocation mode

For `TagentAgent.Run`, `FrameworkFlowAdapter` SHALL use a fresh `EventBus`, fresh `SessionProjection`, and a single `Flow.Run` invocation. It SHALL stop consuming the Flow event channel after the first non-tool-call response (final response) and close the wrapped output channel.

#### Scenario: Sub-agent single-turn Flow

- **WHEN** `TagentAgent.Run` is called with a user message
- **THEN** the adapter creates a temporary bus and projection
- **AND** invokes Flow.Run once
- **AND** closes the output channel after the first final response
