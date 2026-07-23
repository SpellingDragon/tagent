## Context

plan agent 通过 `exec` 工具直接调 openspec CLI 管理计划（无 `skill_search`/`skill_load` 工具，`skills/` 下也无 openspec SKILL.md）。日志实证：create 从不产 `proposal.md`（6/6 归档 change 仅有 `tasks.md`）、`openspec validate` 0 次。

根因：`plan_agent.md` 的"创建计划"流程**硬编码**"直接写 tasks.md"，从不产 proposal、从不校验。openspec 仅内置 `spec-driven` 一种 schema（`proposal → specs → design → tasks`，`apply.requires=[tasks]`），但 plan agent 的实际用途覆盖"通用工作计划"（学习/知识库维护/评估报告）与"软件规格变更"两类，前者并无软件能力/架构可 spec。

实施期间实测到两个关键约束：

1. `openspec instructions` **必须带 `<artifact>` 参数**——单独 `openspec instructions --change <name>` 报错 `Missing required argument <artifact>`。artifact 发现应走 `openspec status --change <name> --json`。
2. `AgentToolWrapper` 的 `defaultSubAgentTimeout = 120s`（`tool_agent.go:205`）对多轮创建流程过紧，慢 LLM 下易被截断。
3. `openspec validate --strict` 对仅含 `proposal + specs`（无 design/tasks）的 change 通过——即 validate 只校验已存在 artifact 的格式，不强制全链。因此"proposal + tasks"最小合规集可通过 strict 校验。

## Goals / Non-Goals

- **Goal**: create 始终产出 `proposal.md`；A 级用内嵌官方模板产 `proposal + tasks`、B 级查 instructions 产 `specs + design`；tasks.md 遵循官方模板；以 `validate --strict` 收尾。
- **Goal**: plan agent 按任务性质自主决策 A/B，默认 A。
- **Goal**: 放宽 sub-agent 调用超时，使多轮工作不被截断。
- **Non-Goal**: 不改 openspec schema、不新增 schema；不给 plan agent 加 skill 工具（继续走 CLI）。
- **Non-Goal**: 不改 progress/update/archive 流程（仅重写 create）。

## Decisions

### 决策 1: A 级内嵌官方模板，B 级查 instructions（方案 2）

A 级（默认、最常见）需要的 `proposal.md`、`tasks.md` 模板短且稳定，**直接内嵌进 `plan_agent.md`**，plan agent 据此产出，无需 `openspec instructions` 调用——省 2-3 轮、规避超时。B 级额外的 `specs/**`、`design.md` 复杂且随版本演进，通过 `openspec instructions <artifact> --change <name>` 获取。artifact 发现走 `openspec status --json`（**不用**无 artifact 参数的 `openspec instructions`，因其报错）。

### 决策 2: 按任务性质自主决策 A/B（默认 A）

不硬编码级别——无法预知 request 属通用计划还是软件规格变更。prompt 给**明确判据**：仅当涉及"代码/接口/能力/行为变更"时选 B；其余默认 A。误判为 A 仍产出可通过 validate 的合规 change，是安全降级。

### 决策 3: proposal.md 始终创建 + tasks.md 官方模板 + validate 收尾

无论 A/B，`proposal.md` 都 SHALL 创建（消除"裸 tasks.md 目录"）。tasks.md 用 `## N. <组名>` + `- [ ] N.M`（新建未完成态）。create 以 `openspec validate <name> --strict` 收尾，失败做**有限次**修正（非无限重试）。A 级最小集通过 strict 已实测确认。

### 决策 4: 放宽 sub-agent 调用超时 120s → 600s

`defaultSubAgentTimeout` 提升至 600s。理由：sub-agent 的真实工作上界由各自 `max_tool_iterations` 决定（plan=15，最坏 ~15×25s≈375s），超时应仅作 runaway 兜底而非正常工作的枷锁。600s 覆盖合法多轮工作且仍能兜住真正挂死的调用。tagent 对 sub-agent 的多轮工作应普遍宽容，故改全局默认常量而非仅针对 plan。

## Risks / Trade-offs

- [LLM 误判 A/B 级别] → 默认 A（更轻、更常见）；prompt 给清晰判据；误判为 A 仍产出合规 change（proposal+tasks），可接受降级。
- [内嵌模板与 openspec 官方模板漂移] → A 级仅内嵌 proposal/tasks 两个**最稳定**的模板；B 级复杂 artifact 仍走 instructions 取最新模板；validate --strict 收尾兜底格式正确性。
- [600s 超时下 runaway 阻塞父 agent 较久] → sub-agent 有 `max_tool_iterations` 硬上界正常结束，action 工具自身有 monitor（stable/fake_dead）提前返回，600s 仅兜底真正挂死；相较"正常工作被 120s 误杀"，宽容更符合预期。
- [instructions 命令误用] → prompt 明确 `openspec instructions <artifact> --change <name>` 形态 + 用 status 发现 artifact，杜绝"缺 artifact 参数报错"。

## Migration Plan

prompt 变更（`plan_agent.md`）+ 一处常量改动（`tool_agent.go`），无数据迁移。历史裸 `tasks.md` change（已归档）不受影响。上线经重启加载新 prompt + 重新编译生效。回滚 = 还原两文件。

## Open Questions

无。核心不确定项（A 级能否通过 strict 校验、instructions 命令正确形态、超时常量位置）均已实测确认。
