# plan-agent Delta

## MODIFIED Requirements

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

### Requirement: plan agent implements dual-mode Run via custom Run method

The plan agent SHALL wrap `TagentAgent` with a custom `Run` method that inspects the `action` field from the invocation. When `action == "progress"`, it SHALL bypass the LLM entirely — directly reading `openspec/changes/`, parsing `tasks.md` checkboxes, and returning a progress summary as a final response event. For all other actions (`create`, `update`, `archive`), it SHALL delegate to the standard `TagentAgent.Run` for ReAct execution.

progress 旁路在多计划并行下 SHALL 按 `name` 定位：有 `name` 时直取该 change；无 `name` 且恰有一个活跃 change 时取之；否则返回活跃计划清单请调用方指定。

#### Scenario: progress query bypasses model

- **WHEN** tagent calls `tool_call(plan, {action: "progress", name: "<plan-name>"})`
- **THEN** plan agent's custom Run SHALL detect `action == "progress"`
- **AND** SHALL NOT create EventBus, SHALL NOT start runEventLoop, SHALL NOT call LLM
- **AND** SHALL 读取 `openspec/changes/<plan-name>/tasks.md` 并返回勾选统计

#### Scenario: 多活跃计划且未指定 name

- **WHEN** progress 查询未携带 `name` 且存在多个活跃 change
- **THEN** plan agent SHALL 返回活跃计划清单（含各计划完成度概要）并请调用方指定
- **AND** SHALL NOT 猜测目标计划

#### Scenario: create/update/archive delegates to standard ReAct

- **WHEN** tagent calls `tool_call(plan, {action: "create", request: "..."})`
- **THEN** plan agent's custom Run SHALL delegate to `TagentAgent.Run`
- **AND** standard ReAct loop SHALL execute with LLM reasoning

#### Scenario: unknown action defaults to ReAct

- **WHEN** the `action` field is missing or unrecognized
- **THEN** plan agent SHALL default to standard `TagentAgent.Run` (safe degradation)

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

## ADDED Requirements

### Requirement: plan agent 产出物边界（不代工）

plan agent 的产出物 SHALL 仅为计划与规格文档（proposal / tasks / design / specs）。收到"产出报告 / 执行分析 / 完成工作"类派活请求时，plan agent SHALL 产出**执行该工作的计划**并在返回中显式说明边界，SHALL NOT 将工作成果写入 change 目录或任何位置。`plan_tool_desc.md`（父 agent 可见面）SHALL 在显著位置声明该边界与 action 契约表（各 action 的触发时机、必带参数、期望返回、传输方式）。

#### Scenario: 派活请求被转化为计划

- **WHEN** 父 agent 请求 plan"深度分析某文档并产出评估报告"
- **THEN** plan agent SHALL 产出"执行该评估"的 proposal + tasks（任务全 `- [ ]`）
- **AND** SHALL NOT 自行写出评估报告成果文件

#### Scenario: 父 agent 可见面声明边界

- **WHEN** 父 agent LLM 读取 plan 工具描述
- **THEN** 描述 SHALL 包含"plan 产出计划文档而非工作成果"的边界声明
- **AND** SHALL 包含各 action 的契约表

### Requirement: 同名计划任务单飞

`AgentToolWrapper` 为 plan 类子 agent spawn 任务时，若调用携带非空 `name`，幂等 Key SHALL 为 `agentName + ":" + name`；同名 change 的并发 spawn SHALL 被任务层去重短路（返回既有任务与 Deduped 标记），后到调用方 SHALL 收到含既有 task id 与 resume 指引的提示。未携带 `name` 的调用维持按 `request` 去重。

#### Scenario: 同名并发 spawn 去重

- **WHEN** 两次携带相同 `name` 的 plan 调用并发发生
- **THEN** 恰有一个新任务被创建
- **AND** 后到者 SHALL 收到既有 task id 与"用 resume_task 续行"的指引，SHALL NOT 产生第二个并发 Run

### Requirement: 文件工具路径基准双列对照

plan agent 的系统提示词 SHALL 以对照表形式同时声明读写两套路径基准：读类工具（read_file / list_file / search_\*）基准为进程 cwd（路径形如 `openspec/changes/...`）；写类工具（save_file / replace_content）基准为 openspec/ 沙箱根（路径形如 `changes/...`，不带 `openspec/` 前缀）。提示词 SHALL NOT 仅声明单侧规则。

#### Scenario: 读写基准同时可见

- **WHEN** plan agent 的系统提示词被加载
- **THEN** 其中 SHALL 存在读/写路径基准的双列对照（含各自示例路径）

#### Scenario: 归档章不引用不存在的工具

- **WHEN** plan agent 的系统提示词描述归档核查手段
- **THEN** SHALL NOT 引用 plan 工具集中不存在的工具（如 `action`/shell）
