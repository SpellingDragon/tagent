## Why

plan agent 产出的"计划文档"不符合 openspec 规范：日志实证 6 个已归档 change **无一例外只有 `tasks.md`**，全部缺失 `proposal.md`，且 `openspec validate` **从未执行**（0 次）。根因是 `plan_agent.md` 的"创建计划"流程**硬编码**"直接写 tasks.md"，从不产 proposal、从不校验。同时发现 sub-agent 调用超时仅 120s，对多轮 openspec 创建流程过紧，易在正常工作中被截断。

## What Changes

- 重写 `plan_agent.md` 的"创建计划"流程，使产出符合 openspec 规范：
  - **A 级（默认，通用工作计划**：学习/知识库维护/评估报告）：直接用**内嵌的 openspec 官方模板**产出 `proposal.md` + `tasks.md`，无需查 instructions（模板短且稳定，省轮次、避超时）。
  - **B 级（软件规格变更**，涉及代码/接口/能力/行为变更）：额外产出 `specs/**` + `design.md`，其模板通过 `openspec instructions <artifact> --change <name>` 获取（复杂且随版本演进）。
  - artifact 发现/构建顺序通过 `openspec status --change <name> --json`（`openspec instructions` **必须带 `<artifact>` 参数**，不能单独 `--change`）。
- 让 plan agent **按任务性质自主决策 A/B 级别**，默认 A；拿不准选 A。
- **proposal.md 始终创建**，禁止产出仅含 `tasks.md` 的裸目录；`tasks.md` 遵循官方模板 `## N. 组名` + `- [ ] N.M`（新建未完成态）。
- **创建流程以 `openspec validate <name> --strict` 收尾** + 有限次修正。
- **放宽 sub-agent 调用超时**：`defaultSubAgentTimeout` 由 120s 提升至 600s——tagent 对 sub-agent（plan/knowledge/recall 等）的多轮工作应宽容；真实上界由各 agent 的 `max_tool_iterations` 决定，超时仅作 runaway 兜底。
- 保持 create "无探索"原则（复用现有自检 + init 前置）。

## Capabilities

### New Capabilities

（无新增 capability）

### Modified Capabilities

- `plan-agent`: create 流程改为"A 级内嵌官方模板、B 级查 instructions"；proposal.md 始终创建；tasks.md 遵循官方模板；以 validate 收尾；按任务性质自主决策合规级别。
- `subagent-turn-execution`: sub-agent 调用超时放宽，正常多轮工作不被超时截断。

## Impact

- 提示词：`examples/wechat-bot/resources/prompts/plan_agent.md`（创建流程重写 + 内嵌 A 级模板）。
- 代码：`agent/tool_agent.go`（`defaultSubAgentTimeout` 120s → 600s）。
- 规范：`openspec/specs/plan-agent/spec.md`、`openspec/specs/subagent-turn-execution/spec.md`。
- 测试：`tests/plan_agent_create_behavior_test.go`（断言产出 `proposal.md` + `tasks.md`、tasks.md 匹配官方模板、执行 `validate`；A 级不强制 `instructions`）。
- 无破坏性变更（仅强化 plan agent 创建行为 + 放宽超时）。
