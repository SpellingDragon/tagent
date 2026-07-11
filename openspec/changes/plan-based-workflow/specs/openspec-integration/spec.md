## ADDED Requirements

### Requirement: plan sub-agent manages openspec work plans

A `plan` sub-agent SHALL be registered as a tool for the tagent entry agent. It SHALL have its own LLM, system prompt, and tools (`exec`, `read_file`, `save_file`). tagent calls it via `AgentToolWrapper` with a natural language `request` describing what to plan. The plan agent SHALL use `exec` to run `openspec new change` / `openspec archive`, and `save_file` to write/update `proposal.md` and `tasks.md`. tagent SHALL NOT directly operate openspec files.

#### Scenario: tagent calls plan to create a plan

- **WHEN** tagent LLM decides a complex task needs a structured plan
- **AND** calls `tool_call(plan, {request: "为获取网站内容创建计划"})`
- **THEN** plan agent SHALL run its ReAct loop: call `exec("openspec new change ...")`, write `proposal.md` and `tasks.md` via `save_file`
- **AND** return a summary like "计划已创建，3个任务: 1.获取HTML 2.提取正文 3.总结"

#### Scenario: tagent calls plan to update progress

- **WHEN** tagent completes a task and calls `tool_call(plan, {request: "任务1已完成，更新进度"})`
- **THEN** plan agent SHALL read `tasks.md` via `read_file`, update the checkbox `- [ ]` → `- [x]` via `save_file`
- **AND** return the updated progress summary

#### Scenario: tagent calls plan to archive

- **WHEN** all tasks are complete and tagent calls `tool_call(plan, {request: "所有任务完成，归档计划"})`
- **THEN** plan agent SHALL call `exec("openspec archive ...")`
- **AND** return "计划已归档"

#### Scenario: tagent does not directly operate openspec files

- **WHEN** tagent's tool list is configured
- **THEN** tagent SHALL NOT have direct access to openspec-specific file operations
- **AND** all openspec operations SHALL go through the plan sub-agent

### Requirement: PlanProgressTracker injects active openspec change progress

PlanProgressTracker SHALL register as a BeforeModel callback on the tagent entry agent. Before each LLM call, it SHALL scan `openspec/changes/` (excluding `archive/`) for active changes. If exactly one active change exists, it SHALL read its `tasks.md`, parse checkbox states, and append a progress summary system message. If zero or multiple active changes exist, no message SHALL be injected. PlanProgressTracker reads files directly (not through plan agent) for minimal overhead.

#### Scenario: Single active change with mixed progress

- **WHEN** BeforeModel fires and `openspec/changes/` contains exactly one active change
- **AND** its tasks.md has 5 tasks: 2 checked, 3 unchecked
- **THEN** a system message SHALL be appended with change name, progress count, and each task's status marker

#### Scenario: No active changes

- **WHEN** `openspec/changes/` contains no active changes (only archive/)
- **THEN** no progress summary message SHALL be added

#### Scenario: Multiple active changes

- **WHEN** `openspec/changes/` contains 2+ active changes
- **THEN** no progress summary message SHALL be added

### Requirement: OpenSpecDir configurable via AgentConfig

AgentConfig SHALL accept an `OpenSpecDir` field (string, default `"."`). PlanProgressTracker SHALL use this path to locate `openspec/changes/`.

#### Scenario: Custom openspec directory

- **WHEN** AgentConfig.OpenSpecDir is set to "/data/myproject"
- **THEN** PlanProgressTracker SHALL scan `/data/myproject/openspec/changes/`

### Requirement: FrameworkPrompt describes plan agent usage

The FrameworkPrompt constant SHALL include a section explaining: call the `plan` tool for complex multi-step tasks, plan agent creates openspec proposals and tasks, framework auto-injects progress, call plan to update/archive.

#### Scenario: FrameworkPrompt includes plan section

- **WHEN** any agent is created with FrameworkPrompt
- **THEN** the prompt SHALL contain guidance about using the plan tool for complex tasks
## ADDED Requirements

### Requirement: PlanProgressTracker injects active openspec change progress

PlanProgressTracker SHALL register as a BeforeModel callback that, before each LLM call, scans `openspec/changes/` (excluding `archive/`) for active changes. If exactly one active change exists, it SHALL read its `tasks.md`, parse checkbox states (`- [ ]` / `- [x]`), and append a progress summary system message to the end of messages. If zero or multiple active changes exist, no message SHALL be injected.

#### Scenario: Single active change with mixed progress

- **WHEN** BeforeModel fires and `openspec/changes/` contains exactly one active change "fetch-content"
- **AND** its tasks.md has 5 tasks: 2 checked, 1 unchecked (in progress), 2 unchecked
- **THEN** a system message SHALL be appended containing the change name, progress count (2/5), and each task's status marker

#### Scenario: No active changes

- **WHEN** BeforeModel fires and `openspec/changes/` contains no active changes (only archive/)
- **THEN** no progress summary message SHALL be added

#### Scenario: Multiple active changes

- **WHEN** BeforeModel fires and `openspec/changes/` contains 2+ active changes
- **THEN** no progress summary message SHALL be added (ambiguous state, let LLM handle)

#### Scenario: tasks.md missing or unreadable

- **WHEN** an active change directory exists but tasks.md is missing or unreadable
- **THEN** PlanProgressTracker SHALL log a warning and skip injection (no error propagated)

### Requirement: OpenSpecDir configurable via AgentConfig

AgentConfig SHALL accept an `OpenSpecDir` field (string, default `"."`). PlanProgressTracker SHALL use this path to locate `openspec/changes/`. This allows different deployment environments to point to different openspec roots.

#### Scenario: Custom openspec directory

- **WHEN** AgentConfig.OpenSpecDir is set to "/data/myproject"
- **THEN** PlanProgressTracker SHALL scan `/data/myproject/openspec/changes/` for active changes

#### Scenario: Default openspec directory

- **WHEN** AgentConfig.OpenSpecDir is empty
- **THEN** PlanProgressTracker SHALL scan `./openspec/changes/` (current working directory)

### Requirement: FrameworkPrompt describes openspec plan mechanism

The FrameworkPrompt constant SHALL include a section explaining the openspec plan mechanism: use `exec` to run `openspec new change`, use `save_file` to update tasks.md checkboxes, use `exec` to run `openspec archive`, and that the framework auto-injects progress summaries.

#### Scenario: FrameworkPrompt includes openspec section

- **WHEN** any agent is created with FrameworkPrompt prepended to system prompt
- **THEN** the prompt SHALL contain guidance about:
  - Creating plans via `openspec new change` for complex multi-step tasks
  - Updating task checkboxes via `save_file` after completion
  - Archiving via `openspec archive` when all tasks done
  - Framework auto-injects active plan progress before each LLM call
