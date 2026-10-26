# thinking-plan-event-classification delta（02-execution）

> 复用登记（非规范，不改本条语义）：thinking_plan 分类基于原生 `Role`/`ToolCalls`；多模态 `ContentParts` 与并存 `Content` 保真，分类不剥离字段。
> 本 delta 只修改「thinking_plan event type classification」；其余 Requirement（thinking_plan as special event type、Event view transformation displays thinking_plan type）由主 spec `openspec/specs/thinking-plan-event-classification/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: thinking_plan event type classification

The system SHALL classify `RoleAssistant` messages that contain `ToolCalls` as `thinking_plan` event type, distinguishing them from the actual tool execution results (`RoleTool` → `action_command`).

分类 SHALL 仅依据原生 `Role` 与 `len(ToolCalls)`；assistant 同时携带 `Content`（叙述）与 `ToolCalls` 时 SHALL 仍分类为 `thinking_plan`，其 `Content` 与 `ContentParts`（多模态）MUST NOT 因分类被剥离或压平。MUST NOT 依据被重构的字段或投影后的视图判定类型。

#### Scenario: Assistant message with tool calls is classified as thinking_plan

- **WHEN** `ExtractEventType` receives a `model.Message` with `Role == RoleAssistant` and `len(ToolCalls) > 0`
- **THEN** the function SHALL return `"thinking_plan"`

#### Scenario: Assistant message without tool calls remains agent_output

- **WHEN** `ExtractEventType` receives a `model.Message` with `Role == RoleAssistant` and `len(ToolCalls) == 0`
- **THEN** the function SHALL return `"agent_output"`

#### Scenario: Tool result message remains action_command

- **WHEN** `ExtractEventType` receives a `model.Message` with `Role == RoleTool`
- **THEN** the function SHALL return `"action_command"`

#### Scenario: assistant 并存 content 与 tool_calls 仍为 thinking_plan

- **WHEN** 一条 `RoleAssistant` 消息同时携带非空 `Content` 与 `ToolCalls`
- **THEN** 分类 SHALL 为 `"thinking_plan"`，且其 `Content`/`ContentParts` 在事件载荷中原样保留

#### Scenario: 多模态内容不因分类丢失

- **WHEN** assistant 消息的 `ContentParts` 含多模态部件
- **THEN** 分类与事件构造 SHALL NOT 丢弃 `ContentParts`，原生载荷保真
