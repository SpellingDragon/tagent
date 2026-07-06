## ADDED Requirements

### Requirement: thinking_plan event type classification

The system SHALL classify `RoleAssistant` messages that contain `ToolCalls` as `thinking_plan` event type, distinguishing them from the actual tool execution results (`RoleTool` → `action_command`).

#### Scenario: Assistant message with tool calls is classified as thinking_plan

- **WHEN** `ExtractEventType` receives a `model.Message` with `Role == RoleAssistant` and `len(ToolCalls) > 0`
- **THEN** the function SHALL return `"thinking_plan"`

#### Scenario: Assistant message without tool calls remains agent_output

- **WHEN** `ExtractEventType` receives a `model.Message` with `Role == RoleAssistant` and `len(ToolCalls) == 0`
- **THEN** the function SHALL return `"agent_output"`

#### Scenario: Tool result message remains action_command

- **WHEN** `ExtractEventType` receives a `model.Message` with `Role == RoleTool`
- **THEN** the function SHALL return `"action_command"`

### Requirement: thinking_plan as special event type

The system SHALL treat `thinking_plan` as a special event type. `IsSpecialEventType("thinking_plan")` MUST return `true`, ensuring that `GenerateEventSummary` preserves the original content as the summary without format conversion.

#### Scenario: IsSpecialEventType returns true for thinking_plan

- **WHEN** `IsSpecialEventType` is called with `"thinking_plan"`
- **THEN** the function SHALL return `true`

#### Scenario: GenerateEventSummary preserves full content for thinking_plan

- **WHEN** `GenerateEventSummary` is called with `eventType == "thinking_plan"`
- **THEN** the function SHALL return `msg.Content` as-is (full original text, no truncation, no format conversion to tool call summary)

### Requirement: Event view transformation displays thinking_plan type

When `ContextIntervention.applyEventView` transforms a `thinking_plan` event for the LLM context, the event prefix SHALL follow the format `[evt_{key}|thinking_plan]`, consistent with all other event types.

#### Scenario: thinking_plan event gets correct view prefix

- **WHEN** `extractEventInfo` extracts metadata from an event where `StateDelta["event_type"]` resolves to `"thinking_plan"`
- **THEN** the prefix SHALL be `"[evt_{key}|thinking_plan]"` where `{key}` is the Snowflake EventKey decimal representation
