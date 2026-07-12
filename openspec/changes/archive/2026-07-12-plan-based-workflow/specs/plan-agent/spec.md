## ADDED Requirements

### Requirement: plan agent implements dual-mode Run via custom Run method

The plan agent SHALL wrap `TagentAgent` with a custom `Run` method that inspects the `action` field from the invocation. When `action == "progress"`, it SHALL bypass the LLM entirely — directly reading `openspec/changes/` for the active change, parsing `tasks.md` checkboxes, and returning a progress summary as a final response event. For all other actions (`create`, `update`, `archive`), it SHALL delegate to the standard `TagentAgent.Run` for ReAct execution.

#### Scenario: progress query bypasses model

- **WHEN** tagent calls `tool_call(plan, {action: "progress"})`
- **THEN** plan agent's custom Run SHALL detect `action == "progress"`
- **AND** SHALL NOT create EventBus, SHALL NOT start runEventLoop, SHALL NOT call LLM
- **AND** SHALL directly read `openspec/changes/` for the active change
- **AND** SHALL parse tasks.md checkboxes and return progress summary as event channel

#### Scenario: create/update/archive delegates to standard ReAct

- **WHEN** tagent calls `tool_call(plan, {action: "create", request: "..."})`
- **THEN** plan agent's custom Run SHALL delegate to `TagentAgent.Run`
- **AND** standard ReAct loop SHALL execute with LLM reasoning

#### Scenario: unknown action defaults to ReAct

- **WHEN** the `action` field is missing or unrecognized
- **THEN** plan agent SHALL default to standard `TagentAgent.Run` (safe degradation)

### Requirement: plan tool uses structured action parameter

The plan tool description SHALL declare an `action` parameter (enum: `create`, `update`, `archive`, `progress`) and a `request` parameter (string). The tagent LLM SHALL use `action` to explicitly specify the operation type, enabling plan agent's custom Run to route requests without LLM parsing.

#### Scenario: tool declaration includes action parameter

- **WHEN** the plan tool declaration is registered
- **THEN** InputSchema SHALL include `action` (enum) and `request` (string) properties

### Requirement: tagent does not directly operate openspec files

- **WHEN** tagent's tool list is configured
- **THEN** tagent SHALL NOT have direct access to openspec-specific file operations
- **AND** all openspec operations SHALL go through the plan tool

### Requirement: PlanProgressTracker removed

The `PlanProgressTracker` BeforeModel callback, `OpenSpecDir` configuration field, and all related code SHALL be removed. Progress injection into tagent's context SHALL NOT happen automatically.

#### Scenario: no BeforeModel callback for plan progress

- **WHEN** tagent's BeforeModel callback chain executes
- **THEN** no plan progress summary SHALL be injected into messages
- **AND** no `OpenSpecDir` field SHALL exist in AgentConfig or TagentConfig

### Requirement: FrameworkPrompt does not hardcode plan tool

The FrameworkPrompt SHALL NOT mention the plan tool by name. Plan tool usage guidance SHALL be entirely contained in `plan_tool_desc.md`.

#### Scenario: FrameworkPrompt is tool-agnostic

- **WHEN** FrameworkPrompt is prepended to an agent's system prompt
- **THEN** it SHALL NOT contain the string "plan" as a tool name reference
## ADDED Requirements

### Requirement: plan sub-agent manages openspec work plans via dual-mode operation

A `plan` sub-agent SHALL be registered as a tool for the tagent entry agent. It SHALL have its own LLM, system prompt, and tools (`exec`, `read_file`, `save_file`, `list_file`). tagent calls it via `AgentToolWrapper` with a natural language `request`. The plan agent SHALL distinguish two operation modes:

- **Model-required operations**: creating plans, updating progress, archiving — these require LLM reasoning to generate proposal content and task lists
- **Direct-return operations**: querying progress — this is pure engineering logic (read tasks.md, parse checkboxes, return summary) and SHOULD bypass model inference when implemented

tagent SHALL NOT directly operate openspec files. All openspec operations SHALL go through the plan sub-agent.

#### Scenario: tagent calls plan to create a plan (model-required)

- **WHEN** tagent calls `tool_call(plan, {request: "为获取网站内容创建计划"})`
- **THEN** plan agent SHALL run its ReAct loop: call `exec("openspec new change ...")`, write `proposal.md` and `tasks.md` via `save_file`
- **AND** return a summary like "计划已创建，3个任务: 1.获取HTML 2.提取正文 3.总结"

#### Scenario: tagent calls plan to update progress (model-required)

- **WHEN** tagent calls `tool_call(plan, {request: "任务1.1已完成，更新进度"})`
- **THEN** plan agent SHALL read `tasks.md`, update the checkbox, write back
- **AND** return the updated progress summary

#### Scenario: tagent calls plan to archive (model-required)

- **WHEN** tagent calls `tool_call(plan, {request: "所有任务完成，归档计划"})`
- **THEN** plan agent SHALL call `exec("openspec archive ...")`
- **AND** return "计划已归档"

#### Scenario: tagent calls plan to query progress (direct-return)

- **WHEN** tagent calls `tool_call(plan, {request: "当前进度"})`
- **THEN** plan agent SHALL read tasks.md and return progress summary
- **AND** the response SHALL contain change name, completed/total count, and task status list

#### Scenario: tagent does not directly operate openspec files

- **WHEN** tagent's tool list is configured
- **THEN** tagent SHALL NOT have direct access to openspec-specific file operations
- **AND** all openspec operations SHALL go through the plan sub-agent

### Requirement: plan_tool_desc.md documents dual-mode operation

The plan tool description file SHALL clearly document both operation modes, so the tagent LLM can decide when to call plan and what to expect:
- Model-required operations: create, update, archive — plan agent uses LLM reasoning
- Direct-return operation: query progress — plan agent reads files and returns summary

The description SHALL include concrete call examples for each mode.

#### Scenario: tool description includes both modes

- **WHEN** tagent LLM reads the plan tool description
- **THEN** the description SHALL contain a section for model-required operations and a section for direct-return operations
- **AND** each section SHALL include example call syntax

### Requirement: PlanProgressTracker removed

The `PlanProgressTracker` BeforeModel callback, `OpenSpecDir` configuration field, and all related code SHALL be removed. Progress injection into tagent's context SHALL NOT happen automatically — tagent queries progress on demand via `tool_call(plan, ...)`.

#### Scenario: no BeforeModel callback for plan progress

- **WHEN** tagent's BeforeModel callback chain executes
- **THEN** no plan progress summary SHALL be injected into messages
- **AND** no `OpenSpecDir` field SHALL exist in AgentConfig or TagentConfig

### Requirement: FrameworkPrompt does not hardcode plan tool

The FrameworkPrompt SHALL NOT mention the plan tool by name. Plan tool usage guidance SHALL be entirely contained in `plan_tool_desc.md`. FrameworkPrompt MAY mention that some tools provide structured planning capabilities, but SHALL NOT reference specific tool names.

#### Scenario: FrameworkPrompt is tool-agnostic

- **WHEN** FrameworkPrompt is prepended to an agent's system prompt
- **THEN** it SHALL NOT contain the string "plan" as a tool name reference
- **AND** plan usage guidance SHALL only exist in the tool description file
