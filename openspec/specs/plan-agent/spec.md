# plan-agent Specification

## Purpose

本规范定义 plan-agent 能力。The plan agent SHALL wrap `TagentAgent` with a custom `Run` method that inspects the `action` field from the invocation.

## Requirements

### Requirement: plan agent implements dual-mode Run via custom Run method

The plan agent SHALL wrap `TagentAgent` with a custom `Run` method that inspects the `action` field from the invocation. When `action == "progress"`, it SHALL bypass the LLM entirely — directly reading `openspec/changes/`, parsing `tasks.md` checkboxes, and returning a progress summary as a final response event. For all other actions (`create`, `update`, `archive`), it SHALL delegate to the standard `TagentAgent.Run` for ReAct execution.

progress 旁路在多计划并行下 SHALL 按 `name` 定位：有 `name` 时直取该 change；无 `name` 且恰有一个活跃 change 时取之；否则返回活跃计划清单请调用方指定。`name` 的解析 SHALL 支持 JSON 消息体（wrapper 打包路径）与纯文本 `name=<token>` 回退（resume_task 路径送达原始文本）。

#### Scenario: progress query bypasses model

- **WHEN** tagent calls `tool_call(plan, {action: "progress", name: "<plan-name>"})`
- **THEN** plan agent's custom Run SHALL detect `action == "progress"`
- **AND** SHALL NOT create EventBus, SHALL NOT start runEventLoop, SHALL NOT call LLM
- **AND** SHALL 读取 `openspec/changes/<plan-name>/tasks.md` 并返回勾选统计

#### Scenario: 多活跃计划且未指定 name

- **WHEN** progress 查询未携带 `name` 且存在多个活跃 change
- **THEN** plan agent SHALL 返回活跃计划清单（含各计划完成度概要）并请调用方指定
- **AND** SHALL NOT 猜测目标计划

#### Scenario: resume 文本路径按 name 定位

- **WHEN** 多活跃 change 下收到纯文本输入 `progress name=plan-b: 查看进度`（resume_task 路径）
- **THEN** plan agent SHALL 解析出 `plan-b` 并返回该计划的勾选统计，SHALL NOT 返回“请指定”提示

#### Scenario: create/update/archive delegates to standard ReAct

- **WHEN** tagent calls `tool_call(plan, {action: "create", request: "..."})`
- **THEN** plan agent's custom Run SHALL delegate to `TagentAgent.Run`
- **AND** standard ReAct loop SHALL execute with LLM reasoning

#### Scenario: unknown action defaults to ReAct

- **WHEN** the `action` field is missing or unrecognized
- **THEN** plan agent SHALL default to standard `TagentAgent.Run` (safe degradation)

### Requirement: plan tool uses structured action parameter

plan 工具的 InputSchema SHALL 声明结构化参数：`action`（enum: `create` / `update` / `archive` / `progress`）、`name`（string，计划名）与 `request`（string，自然语言描述）。参数经 ToolRef `extra_params` 声明、由 `AgentToolWrapper` 并入 InputSchema，并在 Call 时与 `request` 一起序列化为 JSON 消息体透传给 plan agent——SHALL NOT 被 wrapper 静默丢弃。`update` / `progress` / `archive` 调用 SHALL 携带 `name`。

#### Scenario: tool declaration includes action and name parameters

- **WHEN** plan 工具的 declaration 被注册
- **THEN** InputSchema SHALL 包含 `action`（enum）、`name`（string）与 `request`（string）属性

#### Scenario: 附加参数透传至 plan agent

- **WHEN** 父 agent 调用 `plan({action:"progress", name:"my-plan", request:"查看进度"})`
- **THEN** plan agent 收到的消息体 SHALL 为包含 `action` / `name` / `request` 的 JSON
- **AND** `extractAction` SHALL 解析出 `progress`，`extractName` SHALL 解析出 `my-plan`

#### Scenario: 未声明附加参数的子 agent 不受影响

- **WHEN** 其他未声明 `extra_params` 的子 agent 工具被调用
- **THEN** 其消息体 SHALL 仍为纯文本 `request`（行为不变）

### Requirement: tagent does not directly operate openspec files

tagent SHALL NOT have direct access to openspec-specific file operations; all openspec operations SHALL be routed through the plan tool.

#### Scenario: openspec operations routed through plan tool

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

### Requirement: plan agent 创建流程产出符合 openspec 官方模板

plan agent 的创建流程产出的每个 artifact SHALL 符合 openspec 当前 schema 的官方模板格式。artifact 的发现与构建顺序 SHALL 通过 `openspec status --change <name> --json` 获取（`openspec instructions` 命令**必须携带 `<artifact>` 参数**，单独 `openspec instructions --change <name>` 会报错，SHALL NOT 使用）。

- 对 **A 级**（通用工作计划）需要的 `proposal.md`、`tasks.md`：plan agent MAY 直接使用提示词内嵌的官方模板产出，无需调用 `openspec instructions`（模板短且稳定，减少轮次）。
- 对 **B 级**（软件规格变更）额外需要的 `specs/**`、`design.md`：plan agent SHALL 通过 `openspec instructions <artifact> --change <name>` 获取模板后再产出（这些 artifact 复杂且随 openspec 版本演进）。

#### Scenario: 通过 status 发现 artifact 构建顺序

- **WHEN** plan agent 需要了解某 change 有哪些 artifact 及其依赖
- **THEN** 它 SHALL 使用 `openspec status --change <name> --json`
- **AND** SHALL NOT 使用不带 `<artifact>` 参数的 `openspec instructions --change <name>`

#### Scenario: B 级复杂 artifact 依据 instructions 模板生成

- **WHEN** plan agent 为 B 级计划产出 `specs/**` 或 `design.md`
- **THEN** 它 SHALL 先执行 `openspec instructions <artifact> --change <name>` 获取模板
- **AND** 依据返回的模板与规则组织内容

### Requirement: plan agent 按任务性质自主决策合规级别

plan agent SHALL 依据被请求计划的性质，自主决策产出的 openspec artifact 合规级别：

- 对**通用工作计划**（如学习、知识库维护、评估报告等非软件规格变更）：SHALL 至少产出 `proposal.md` + `tasks.md`，MAY 跳过 `specs/**` 与 `design.md`（此类计划无软件能力可规格化、无架构可设计）。
- 对**真实软件规格变更**（涉及代码、接口、能力或行为变更）：SHALL 产出完整 spec-driven artifact 集（`proposal.md` + `specs/**` + `design.md` + `tasks.md`）。

拿不准时 SHALL 默认 A 级。无论级别如何，`proposal.md` SHALL 始终被创建（openspec 的基础 artifact），create 结果 SHALL NOT 是仅含 `tasks.md` 的裸目录。

#### Scenario: 通用工作计划采用 A 级

- **WHEN** plan agent 收到通用工作计划的 create 请求（如"制定学习 Go 的计划"）
- **THEN** 它 SHALL 产出 `proposal.md` + `tasks.md`
- **AND** MAY 不创建 `specs/**` 与 `design.md`

#### Scenario: 软件规格变更采用 B 级

- **WHEN** plan agent 收到涉及软件能力/接口/行为变更的 create 请求
- **THEN** 它 SHALL 产出 `proposal.md` + `specs/**` + `design.md` + `tasks.md`

#### Scenario: 任何级别都不产出裸 tasks.md 目录

- **WHEN** plan agent 完成任一 create 请求
- **THEN** 该 change 目录 SHALL 至少同时包含 `proposal.md` 与 `tasks.md`
- **AND** SHALL NOT 出现仅有 `tasks.md`（缺 `proposal.md`）的情况

### Requirement: tasks.md 遵循官方模板并按级别收尾

plan agent 写入的 `tasks.md` SHALL 遵循 schema 官方模板：以 `## N. <组名>` 作为分组标题、以 `- [ ] N.M <描述>` 作为可追踪复选框；新建计划时所有任务 SHALL 处于未完成态 `- [ ]`（勾选权属于执行后的 update 报账，SHALL NOT 在创建时预勾选）。

创建流程收尾 SHALL 按合规级别分派：

- **A 级**（仅 proposal + tasks）：以 `spec(op="status", name=X, json=true)` 结构自检收尾，proposal 与 tasks 均为 done 即合规；SHALL NOT 调用 `validate`（openspec validate 无论是否 strict 均要求 specs deltas，A 级结构上必然失败）。
- **B 级**（含 specs/design）：以 `spec(op="validate", name=X)`（--strict）收尾。

校验/自检失败时 SHALL 进行有限次（≤2）修正，仍失败则返回当前状态与说明。

#### Scenario: tasks.md 匹配官方复选框格式

- **WHEN** plan agent 写入 `tasks.md`
- **THEN** 内容 SHALL 使用 `## N. <组名>` 分组标题
- **AND** 每个任务 SHALL 为 `- [ ] N.M <描述>` 格式的复选框
- **AND** 新建时 SHALL 全部为未完成态 `- [ ]`

#### Scenario: A 级以 status 自检收尾

- **WHEN** plan agent 完成 A 级计划的 artifact 创建
- **THEN** 它 SHALL 执行 `spec(op="status", name=X, json=true)` 确认 proposal 与 tasks 均为 done
- **AND** SHALL NOT 执行 `validate`

#### Scenario: B 级以 strict 校验收尾

- **WHEN** plan agent 完成 B 级计划的 artifact 创建
- **THEN** 它 SHALL 执行 `spec(op="validate", name=X)`（--strict）
- **AND** 校验失败时 SHALL 进行有限次（≤2）修正而非无限重试

### Requirement: plan agent 产出物边界（不代工）

plan agent 的产出物 SHALL 仅为计划与规格文档（proposal / tasks / design / specs）。收到“产出报告 / 执行分析 / 完成工作”类派活请求时，plan agent SHALL 产出**执行该工作的计划**并在返回中显式说明边界，SHALL NOT 将工作成果写入 change 目录或任何位置。`plan_tool_desc.md`（父 agent 可见面）SHALL 在显著位置声明该边界与 action 契约表（各 action 的触发时机、必带参数、期望返回、传输方式）。

#### Scenario: 派活请求被转化为计划

- **WHEN** 父 agent 请求 plan“深度分析某文档并产出评估报告”
- **THEN** plan agent SHALL 产出“执行该评估”的 proposal + tasks（任务全 `- [ ]`）
- **AND** SHALL NOT 自行写出评估报告成果文件

#### Scenario: 父 agent 可见面声明边界

- **WHEN** 父 agent LLM 读取 plan 工具描述
- **THEN** 描述 SHALL 包含“plan 产出计划文档而非工作成果”的边界声明
- **AND** SHALL 包含各 action 的契约表

### Requirement: 同名计划任务单飞

`AgentToolWrapper` 为 plan 类子 agent spawn 任务时，若调用携带非空 `name`，幂等 Key SHALL 为 `agentName + ":" + name`（relaunch 轮次 SHALL 透传同一 Key）；同名 change 的并发 spawn SHALL 被任务层去重短路（返回既有任务与 Deduped 标记），后到调用方 SHALL 收到含既有 task id 的提示，引导先等待 task_settled、结算后再 resume（去重发生时任务必为 running，直接 resume 会被“轮次在飞”拒绝）。未携带 `name` 的调用维持按 `request` 去重。

#### Scenario: 同名并发 spawn 去重

- **WHEN** 两次携带相同 `name` 的 plan 调用并发发生
- **THEN** 恰有一个任务被跟踪
- **AND** 后到者 SHALL 收到既有 task id 与“先等 task_settled、结算后再 resume_task 续行”的指引，SHALL NOT 产生第二个并发 Run

#### Scenario: relaunch 保持 name 键

- **WHEN** 对携带 name 创建的 plan 任务执行 relaunch
- **THEN** 新任务的幂等 Key SHALL 仍为 `agentName:name`，SHALL NOT 回退到 request 文本键

### Requirement: 文件工具路径基准双列对照

plan agent 的系统提示词 SHALL 以对照表形式同时声明读写两套路径基准：读类工具（read_file / list_file / search_\*）基准为进程 cwd（路径形如 `openspec/changes/...`）；写类工具（save_file / replace_content）基准为 openspec/ 沙箱根（路径形如 `changes/...`，不带 `openspec/` 前缀）。提示词 SHALL NOT 仅声明单侧规则，且 SHALL NOT 引用 plan 工具集中不存在的工具（如 `action`/shell）。

#### Scenario: 读写基准同时可见

- **WHEN** plan agent 的系统提示词被加载
- **THEN** 其中 SHALL 存在读/写路径基准的双列对照（含各自示例路径）

#### Scenario: 归档章不引用不存在的工具

- **WHEN** plan agent 的系统提示词描述归档核查手段
- **THEN** SHALL NOT 引用 plan 工具集中不存在的工具（如 `action`/shell）
