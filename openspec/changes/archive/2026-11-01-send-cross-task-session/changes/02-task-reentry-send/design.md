# 02 技术要点

## 要点

1. **状态机**：registry 新增 `reactivate` 迁移（`StatusSettled`→活跃态），走 `Advance` 同款 mutation 载体（`MutationID("reactivate:<task>:<seq>")` + `ExpectedRevision` 乐观并发）；翻转落审计事实（Meta 记 from/to）。failed/cancelled 的 reactivate 拒绝（`ErrTaskReactivationRefused` 具名）。
2. **Round 语义**：重激活开新 Round（`entry.round++`）；`Notice` 身份=TaskID+Round+Kind+Seq 不变；新 Round settle 通知与旧 Round 绝不合并（`identityOf` 已含 Round，验证钉住）。
3. **占坑单胜**：重入与 resume 同款占坑（`pendLocked`/状态转移原子化），后到者获"轮次在飞"错误；陈旧信号按既有"旧 watch 退役"语义隔离。
4. **subagent 续行**：`serve` 由"一次 Serve→completeDelegation"改为可在重入时以新 Round 续行（Round 型执行器语义，`task-reentry` 既有 spec 的 subagent 出入口条款）；未重入时行为逐位不变。
5. **探针**（最大技术未知数前置）：2.1 先行，registry 级重激活+并发单胜一经绿，整域风险释放。

## 文件清单

- 改：`runtime/task/registry.go`、`manager.go`（迁移与占坑）、`runtime/agent/delegate.go`（续行）、`tool/subagent/tool.go`（serve 生命周期）。
- 测：`runtime/task/*_test.go`、`tests/integration/execution/`、`tests/integration/host/`。

## 风险与回退

- 风险：终态翻转与 `Delegator.Complete` 的 `isTerminalStatus` 拒绝面交互；`observeSettleDelivery` 对重复 settle 的幂等（`r.facts[fact.Key]` 已挡）。
- 回退：迁移为 additive（旧记录只读保全）；单域回滚不损 01。
