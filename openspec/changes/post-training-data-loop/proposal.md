# 后训练数据闭环（M0-M2，在线 RL 留契约位）

## Why

tagent 要从"能跑"走向"能学"：把每次真实任务执行变成可验证、可追溯、可导出的训练数据。设计文档（docs/tagent-posttraining-design.docx + tagent-training-handoff.zip）已给出完整方案；本变更把它落为可实施的代码与门禁。核心风险是数据闭环各层（采集/验收/奖励/导出）一旦口径漂移，训练数据会系统性失真——因此契约、金样与 CI 门必须先于业务实现立严。

## What Changes

- **阶段 M0 余量**：修复设计第 3 节列出的、event-consumption-integrity 变更尚未覆盖的可靠性缺口（检索候选截断、完整请求预算、有效请求采集边界）。
- **阶段 M1 数据闭环**：训练观察面（有效请求固定、原始动作与变换、子代理 lineage、分段封存、reader 适配）、任务规格与独立验收器（TaskSpec、环境 Runner、Verifier Runner、校准）、奖励向量与资格门、SFT/DPO 导出与不可变发布。
- **阶段 M2 试采**：本地确定性替身的采集工厂（多 seed 采样、故障注入课程、八类扰动、试采指标与质量门、人工反馈最小字段）。
- **在线 RL 只留契约位**：trainer adapter 的 capability 声明字段与 schema 兼容位，不做实现、不冒充 token 资格。
- **骨架先行**：全部域先写类型/接口/stub 骨架（sentinel error + 编译断言），过双门（每域域内辩证评审 + 跨域契约集中评审）后才解锁实施；实施期任何契约偏移需显式评审。
- **CI/Lint/Golden 前置**：三份 JSON Schema 契约 + 12 负例 + 校验工具随 W0 进仓库并接入每 MR 门；金样不因实现演进而静默改写。

## Capabilities

### New Capabilities

- `training-data-contract`：tagent.training.v1 规范化记录契约（六 kind：episode/decision/attempt/tool_execution/verification/reward）、BlobRef 可回读引用、schema 分派与未知拒绝、负例集与校验器。
- `task-verification`：TaskSpec 版本化规格、split_group 隔离、环境 Runner 重置与快照、Verifier Runner 独立验收（pass/fail/unknown/invalid）、校准与对抗样例。
- `training-reward`：奖励向量（success/hard_constraints/artifact/recovery/cost）、sparse_v1 与 success_only_v1 标量策略、证据绑定与 null 语义、里程碑去重。
- `dataset-export`：资格门六层检查、SFT 样本（loss_mask/weight）、DPO 同前缀偏好对、不可变发布清单、确定性排序与摘要、撤回传播。
- `trial-factory`：多 seed 采样编排、故障注入纪律（八类扰动）、试采指标（可重建率/因果闭合/假通过率/泄漏/采集开销）、人工反馈工作台最小字段。

### Modified Capabilities

- `persistent-event-loop`（若 M0 余量触及）／`offline-training-export`（reader 分派改造现有导出面）——具体 delta 由二级域按对账结果定。
- `architecture-guardrails`：训练契约金样与骨架断言并入现有门。

## Impact

- 新增：`golden/training/`（或 contracts/）三份 Schema+负例、`scripts/validate_training_bundle.py`（或等价 Go 工具）、训练观察面采集代码（挂在现有 rl/host_capture 边界）、离线规范化层（单向读取 tagent.record.v1，不回写运行时）、任务/验收/奖励/导出的新包（位置由现有 owner 定，不建平行子系统）。
- 修改：`memory/search/store_searcher.go`（候选语义）、`memory/replay/replay_budget.go`（完整请求预算）、`runtime/agent/loop.go`（有效请求固定边界）、`rl/export_chain.go` 与 `scripts/convert_trajectories.py`（reader 适配）。
- 不改：业务事实账本语义（M0 已由 event-consumption-integrity 完成）、在线 RL 实现、真实模型服务调用（本次全部本地替身）。
- 用户已裁定：M0-M2 范围、本地替身先行、骨架双门（每域硬门+跨域集中评审）。

## 骨架双门（编排硬门，承接用户指令）

1. **W0 全域骨架**：六域各出类型/接口/stub（sentinel error、零业务逻辑、编译断言过）+ 跨域接口契约清单（共享签名级常数）。
2. **门一·集中契约评审**（跨域）：全部骨架的共享接口一次辩证评审——每个接口过三反事实（删掉谁坏？第二使用方独立存在？边界是否画在事件+三挂点的语义单元上？）+ 简单性检查（能否用"事件+生产/观察/消费三挂点"解释？解释不了即过度设计）。
3. **门二·域内骨架评审**（每域）：域内骨架独立过辩证评审（带着自己的问题自己找答案：评审者必须对每个 stub 提出"谁消费它、失败语义是什么、与最近现有抽象差在哪"并给出答案），过门才解锁该域实施。
4. **契约偏移检查**：实施期每个 PR 对照骨架接口 diff；偏移无评审记录即门红。

## 规划入口

- design.md（静态决策）／tasks.md（编排账目）／ORCHESTRATION.md（派发唯一依据）／coverage.md、findings.md（映射与风险）／task-graph.json。
