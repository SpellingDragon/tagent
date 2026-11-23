# Proposal：运行架构收敛（runtime-coherence）

## Why

用户对两周 `runtime-recovery-ideal` 的验收判断是：恢复正确性已显著加强，但“复杂的实现暴露成复杂的架构”尚未解决——扩展者仍要理解多个公共入口的不同接线、恢复路径的业务特判与存储旁路，才能接入一个普通插件或消费者。

2026-11-22 只读审计给出静态证据（未做故障注入的部分逐项标注）：

- `Boot` 应用调用者 options 后重建自身列表，不转交 `WithPlugins`/代际注册；贡献经正式宿主入口不生效（`assembly/host.go:479`、`:632`、`:690`）。
- 审批 `Decide` 先改内存 `Status` 再提交回答；提交失败不还原，而快照按 `Status` 筛选、运行期 `Pending` 按事实链判定（`runtime/task/approval/approval_store.go:457`、`:266`、`:656`）。
- 共享 `FactLog` 无点读，已知完整键仍 `Scan`（`api/durable/fact_log.go:24`、`runtime/state/durable.go:807`、`features/evolution/governance/denial.go:644`）。
- 任务在线经 `applyCommitLocked`、恢复经 `foldLocked` 分别解释状态与轮次（`runtime/task/lifecycle.go:400`、`runtime/task/restore.go:239`）。
- 协调者直接初始化任务账户、并特判压缩与投影的覆盖关系（`assembly/checkpoint.go:850`、`assembly/run.go:2985`）；新消费者缺切片时读取边界未声明。

旧变更 `runtime-recovery-ideal` 已于 2026-11-22 归档（commit `893065d7`＋`8f772962`，含 151 项承接账与 F1–F4 记录）。本变更不改写冻结归档，只承接仍未闭合且与本目标相关的义务，并登记后继指针。

用户原话（2026-11-22）：“我最担心的，还是把复杂的实现暴露为复杂的架构……整个机制预期应当是充分优雅的，那架构就应当是足够简单的。”

## What Changes

- **状态派生与切面覆盖契约**：每个可恢复消费者只保留一份状态派生规则；在线提交与恢复回放走同一有类型应用；明确切面覆盖、缺片、空切片、损坏与尾部的语义。
- **存储读取契约**：共享持久面直接表达点读与扫描，统一“不存在／读失败”；已知身份不扫描无关历史；派生索引失效不改变真实答案；`CommitOrder` 保持可选能力，比较同能力透明包装而非异类引擎等价。
- **插件装配**：`Assemble`、`Boot`、reload 对贡献实例与代际注册走同一接线；顺序、作用域、失败清理、换代归属明确；材料声明与实际分派一致，保留六端口与兼容读法。
- **消费者收敛**：任务/邮箱、审批/目标、投影/压缩、治理各自的恢复特判收回拥有者；被替代的双解释与专用分支删除。
- **存储与成本**：点读统一、索引定位校验与安全回退；成本仍由引擎兑现接口，不要求消费者理解文件布局。
- **放行**：在冻结候选上完成生命周期、故障、长期成本、三 module、race、冒烟、十短、六长、变异与真实 CI；旧账未闭合项逐条承接。

不改变：事实不可变与按身份回放；至少一次＋幂等消费；执行安全边界；日志 CRC 线性成本为已接受项；六端口与兼容读法；`CommitOrder` 可选性；不实现 `no-prefix-criterion`。

## Capabilities

### New Capabilities

- `recovery-derivation-contract`：运行与恢复同一有类型状态应用、切面覆盖与缺片语义、恢复期零副作用。
- `durable-store-point-reads`：共享持久契约的点读/扫描语义、错误区分、索引失效答案等价与成本归引擎。
- `task-recovery-vertical`：任务—通知—checkpoint—邮箱的首个真实纵向样板与审批失败一致性。
- `consumer-recovery-convergence`：任务/审批/治理/投影/压缩收敛到已证明机制，删除被替代路径。
- `coherence-release`：生命周期、故障、长期成本、完整矩阵、独立审与真实 CI 的放行证据。

### Modified Capabilities

- `plugin-mounting`：新增“正式入口同一接线、组合顺序与失败清理”要求；既有 builtin/声明/无硬编码要求不变。

## Symptom Anchors

**S1（第一天红）**：一个宿主经公开入口贡献一个观察插件与一个有状态消费者，用正式 `Boot` 启动并重启后：插件真被调用、消费者状态真被恢复，框架代码零改动。现状红：`Boot` 不承接 `WithPlugins`。拟新增 `TestSymptomAnchorHostBootMountsContributedPlugin`。

**S2（候选，阶段 0 判定）**：审批回答写失败后重启，该请求仍为未决且未放行；若阶段 0 反证不成立则关闭该怀疑并保留反证。拟新增 `TestSymptomAnchorFailedRulingKeepsTheHold`。

“本变更全部完成并放行后，用户这句话还会发生吗？”预计答案：不会——S1 红转绿，且回滚即红；S2 以阶段 0 结论为准。

## Impact

`api/durable`、`api/plugin`、`memory/ledger`、`assembly`、`runtime/state`、`runtime/task`、`runtime/task/approval`、`features/evolution/governance`、`docs/GLOSSARY.md`、`docs/wiki`、主规格增量、CI/golden。无新增外部依赖、付费服务或权限。

已知未闭合输入：2026-11-22 CI run `45004855439`（候选 `52d7b664`）在 test/race/event-integrity-short/required/archive-preflight 红；归档作业缺 OpenSpec CLI 与 evidence-binding 失败。产品红项归阶段 0 定性，接线问题单独修，不在本提案中宣称已绿。
