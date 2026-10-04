# Proposal: 根包二次瘦身——零依赖件外迁、世代簿记归域、测试按编译判定归位

## Why

第一轮结构重组后根包仍有生产 13 文件/16,497 行、测试 25 文件/12,184 行。实测发现三类可动且应动的：

1. **三件零根包依赖文件纯属历史惯性留在根包**：`resources.go`(412 行，对外仅依赖 memory)、`asset_drift.go`(322，仅 evolution)、`consolidation_hint.go`(132，仅 event)——对 `runtimeConfig`/`buildAgent`/`Config` 零引用，外迁是纯移动。
2. **世代簿记外迁的前提已消失**：当初 D9 判 `org_hotreload.go` 不可迁的理由是"12 处依赖根包 Config"，C1 之后 Config 已在 `config` 包。文件内的簿记件（`orgSubset`/`computeOrgFingerprint`/`extractOrgSubset`/`hotSignature` 约 130 行 + `OrgStatus` 等 5 个公共类型约 65 行）属机制，应归 `agent/org`；`orgCoordinator`（swap/publish/告警）是「唯一编排发布权」的物理位置，留根。
3. **测试位置可按编译判定归位**：`tests/` 已是 `package tagent_test` 外部测试包。根包 25 个测试文件里，凡只用导出面者可搬；引用未导出符号者是真灰盒，留根并记录其依赖清单。此前静态扫描把字段名/局部名误判为未导出依赖，4/21 的分法作废——**可靠判据只有试编译**。

## What Changes

- **A 外迁三件**：`resources.go` → `agent/resources`（资源租约治理，root→agent 方向合法）；`asset_drift.go` → `evolution`（漂移审计器，evolution 不反向引用其符号，无环）；`consolidation_hint.go` → `memory`（整理提示是记忆域语义，memory→event 合法）。各自单测编译判定跟迁。
- **B 簿记归域**：五件簿记函数与 `Org*` 五类型迁 `agent/org`；根包类型别名保持 `tagent.OrgStatus` 等 API 不变；`orgCoordinator` 留根。修订既有裁决记录（D9"整文件留根"→ D10"簿记随机制、发布动作留根"的既定方向）。
- **C 测试归位**：逐文件"搬 `tests/`（package tagent_test）试编译"判定；可搬者搬走，不可搬者产出未导出依赖清单；是否为搬运扩 `testing.go` 测试导出面是**裁决项**，逐条过导出必要性判据，不擅自扩。
- **规格修订**：既有「组合根物理边界与追踪卫生」的职责清单写着"资源与提示词引用"属根包——资源治理外迁后该清单需修订（资源治理实现居 agent 域，根留装配注入点）。

## Impact

- Affected specs: `architecture-guardrails`（MODIFIED：组合根物理边界与追踪卫生——职责清单与世代簿记归属）
- Affected code: 三件外迁 + 调用点（`build_agent.go` 4 处 hintTracker、`wiring.go`/`tagent.go` 资源与审计接线）；`org_hotreload.go` 拆出 ~200 行；根包新增簿记别名文件；测试文件按判定移动
- 预期：根包生产 13→9~10 文件（~15,500 行），测试文件数按编译判定下降；行为零变更
