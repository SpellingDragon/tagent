## ADDED Requirements

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

### Requirement: tasks.md 遵循官方模板并以 validate 收尾

plan agent 写入的 `tasks.md` SHALL 遵循 schema 官方模板：以 `## N. <组名>` 作为分组标题、以 `- [ ] N.M <描述>` 作为可追踪复选框；新建计划时所有任务 SHALL 处于未完成态 `- [ ]`。创建流程 SHALL 以 `openspec validate <name> --strict` 收尾，并在校验失败时进行有限次修正。

#### Scenario: tasks.md 匹配官方复选框格式

- **WHEN** plan agent 写入 `tasks.md`
- **THEN** 内容 SHALL 使用 `## N. <组名>` 分组标题
- **AND** 每个任务 SHALL 为 `- [ ] N.M <描述>` 格式的复选框
- **AND** 新建时 SHALL 为未完成态 `- [ ]`

#### Scenario: create 以 strict 校验收尾

- **WHEN** plan agent 完成 artifact 创建
- **THEN** 它 SHALL 执行 `openspec validate <name> --strict`
- **AND** 校验失败时 SHALL 进行有限次修正而非直接返回
