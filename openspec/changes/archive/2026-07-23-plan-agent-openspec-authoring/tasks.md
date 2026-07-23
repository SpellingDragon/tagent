## 1. 重写 plan_agent.md 创建流程

- [x] 1.1 在"创建计划"章节**内嵌 A 级官方模板**：`proposal.md`（Why/What Changes/Impact 骨架）+ `tasks.md`（`## N. 组名` + `- [ ] N.M`）
- [x] 1.2 加入 A/B 自主决策判据：默认 A（`proposal.md` + `tasks.md`）；仅当涉及代码/接口/能力/行为变更时选 B（补 `specs/**` + `design.md`）；拿不准选 A
- [x] 1.3 明确 proposal.md 始终创建，禁止产出仅含 `tasks.md` 的裸目录
- [x] 1.4 B 级 `specs/**`、`design.md` 通过 `openspec instructions <artifact> --change <name>` 获取模板；artifact 发现用 `openspec status --change <name> --json`；明确 `openspec instructions` **必须带 `<artifact>` 参数**（禁止单独 `--change`）
- [x] 1.5 强制 `tasks.md` 官方模板：`## N. <组名>` + `- [ ] N.M <描述>`，新建为未完成态
- [x] 1.6 创建流程以 `openspec validate <name> --strict` 收尾 + 有限次修正（非无限重试）
- [x] 1.7 保留现有自检（`command -v openspec || npm install`）+ `openspec init` 前置，维持"无探索"原则

## 2. 放宽 sub-agent 调用超时

- [x] 2.1 `agent/tool_agent.go`：`defaultSubAgentTimeout` 由 `120 * time.Second` 提升至 `600 * time.Second`，并更新注释说明"仅作 runaway 兜底，真实上界由 max_tool_iterations 决定"
- [x] 2.2 `go build ./agent/ && go vet ./agent/` 通过

## 3. 更新测试

- [x] 3.1 更新 `tests/plan_agent_create_behavior_test.go`：断言 mock `save_file` 记录的产出**至少含 `proposal.md` 与 `tasks.md`**（不再是裸 tasks.md）
- [x] 3.2 断言写入的 `tasks.md` 内容匹配官方模板（`## N.` 分组 + `- [ ] N.M` 复选框）
- [x] 3.3 断言 create 流程执行 `openspec validate`（带 `--strict`）
- [x] 3.4 移除/放宽"断言调用 openspec instructions"（A 级不强制 instructions）

## 4. 验证与数据清理

- [x] 4.1 `go build ./... && go vet ./agent/ ./tests/` 通过；`go test ./agent/ -count=1` + `go test ./tests/ -short -count=1` 通过
- [x] 4.2 真实 glm-5.2：`TRPC_CLAW_MODEL_NAME=glm-5.2 go test -v -run TestPlanAgentCreateBehavior_RealPrompt ./tests/ -timeout 180s`，确认 create 产 `proposal.md`+`tasks.md`、tasks.md 合模板、执行 `validate`
- [x] 4.3 数据清理：确认测试用 mock 工具不落盘；清理验证过程中真实产生的任何临时 openspec change

## 5. 收尾

- [x] 5.1 `openspec validate plan-agent-openspec-authoring --strict` 通过
- [x] 5.2 按 Conventional Commits 规范提交（scope: agent / wechat-bot），说明根因、方案 2（内嵌 A 模板 + B 查 instructions）与超时放宽
