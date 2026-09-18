# WP3 owner / 执行代职责 可维护性复评登记 (resident-remaining-hardening 4.5)

> **本文件是「登记」不是「施工」**。任务 4.5 明确要求：**不改核心代码**——本 change 的
> 长跑/E2E/fuzz/race 证据锚定当前代码形态，任何结构重构都会使这些证据失效，须重跑全部批次
> C。故本文只登记剩余耦合/重复与重构候选、评估成本收益、给出安全的未来切入窗口，供后续
> 独立 change 裁决。所有观察以**符号名**定位（遵守 wiki-code-sync「禁行号」纪律，行号必腐）。

## 1. 复评范围（WP3 = owner / 执行代职责）

「owner/执行代」指 R4 热更确立的 **ownership 契约**与**执行器代际换装**两条职责线，落点：

| 面 | 文件 | 关键符号 |
|----|------|---------|
| build ownership 契约 | `build_agent.go` | `buildMode` 三谓词 `isExecutorShell`/`ownsPersistentState`/`bindsProcessShared`、`buildAgentDFS`、`buildAgentToolRef`、`buildPlainToolRef` |
| 执行代持有/换缝 | `agent/context_manager.go` | `ContextManager`、`executorMu`、`SwapExecutor`、`RebuildExecutor`、`buildRunner`、`retiredRunners`/`RetireRunner`/`sweepRetiredRunners` |
| 代际编排/回滚 | `tagent.go` | `Reload`、ring-2 代际快照、`WithConfigPath`、`Rollback` |
| 结构指纹 | `org_hotreload.go` | `computeOrgFingerprint`、`computeMemoryFingerprint` |

## 2. 登记：剩余大文件耦合与重复（现状，已核验）

### C-1 `ContextManager` 是多职责汇聚点（≈34 字段，单类型 ≥4 概念角色）

`agent/context_manager.go`（本仓最大 Go 文件）内 `ContextManager` 同时承载：

- **(A) 常驻耐久态 owner**（换执行器时**永不换**）：`memStore`、`sessionSvc`、`bus`、`projection`、`outputCh`、`tokenCounter`、`maxTokens`/`thresholdPct`、`compressor` 及 BeforeModel 闭包；
- **(B) 执行代持有 + 退役回收**：`runner`（写经 `executorMu`）、`retiredRunners`+`retireMu`+`runnerInFlight`（换下的旧 runner 待无在飞 turn 引用再 Close）；
- **(C) per-turn 可变上下文**：`triggerSource`、`turnProductive`、`currentMetadata`+`metadataMu`；
- **(D) 装配快照与横切接线**：`execCfg`（冷启动快照，`RebuildExecutor` 据此「执行面覆盖/状态面强复用」合并）、`orgReloader`、`bundleIDFn`、`taskController`、`overflowDir`。

**耦合风险**：角色 (A)(B) 的「状态面不换 / 执行面可换」不变量目前**靠约定 + 注释**维持（见 `RebuildExecutor` 内「merged := cm.execCfg; 状态面强制复用」），非编译期隔离。新增一类可换字段若误入 (A) 集，会重演 `RebuildExecutor` 注释记载的「2026-09-13 事故：新壳 BeforeModel 闭包捕获新壳空 cm → 请求装配接空投影」类缺陷。

**重构候选**（后续独立 change）：抽出 `executorHandle`（持 `runner`+`executorMu`+退役队列）与 `residentState`（持耐久态）两内嵌结构，把「可换/不可换」升为**类型边界**——正是 `buildMode` 三谓词把散落的 ownership 判断类型化这一既有方向的延续。

### C-2 `buildAgentDFS` 为 god-function（≈642 行、9 参（8 定 + 1 变参）、10 处 mode 谓词、6 处时长解析）

`build_agent.go` 的 `buildAgentDFS` 单函数贯穿：store 解析→engine 包裹→degradation 包裹→提示词加载→evolution 绑定→模型解析→分区计算→工厂分派→工具装配→治理包裹→`TagentConfig` 组装→冥想配置→R1/R2/R3 重建→spill 双写→就绪信号→closer 登记。

**已做对**：三条构建路径（冷启动 resident / 热重建 executor shell）**共用同一函数**，「行为学一致、防双路径漂移」；ownership 已用 `buildMode` 谓词类型化。

**登记的问题**：
- **10 处** `mode.isExecutorShell()/ownsPersistentState()/bindsProcessShared()` 分支散落于函数全身——每新增一个常驻子系统，作者须在 god-function 内**逐点**判断该子系统属「壳跳过 / 常驻才建 / 进程级 once」，漏点即代际泄漏（如 evolution `BindRuntime` 若不在 `bindsProcessShared` 内即「换代后评估从空壳 store 取证据、静默失明」，注释已警示）。
- **6 处**近乎同构的 `time.ParseDuration → err → log.Warnf 兜底` 块（`TaskTerminalTTL`/`TaskStaleAfter`/`TaskJobDeadline`/`TaskMaxDetachedAge` 及其 remap）。

**重构候选**：(a) 6 处时长解析收敛为一个 `parseDurField(name, raw, apply)` 小助手（**低风险、可局部**，但仍是核心文件改动，故本 change 不动）；(b) 把「按 mode 分支的资源获取/绑定」抽成 `buildPlan` 声明表（每资源标注 owned-by/shell-skips/process-once），令 god-function 主体退化为对表的遍历——把「逐点判断」变「集中声明」，与三谓词同旨。

### C-3 ownership 语义分散在两个抽象层

ownership 契约同时活在 `build_agent.go`（buildMode）与 `agent/context_manager.go`（SwapExecutor/RebuildExecutor 的状态面/执行面二分）。二者用**不同词汇**表达同一「谁拥有耐久态、谁只是临时执行壳」概念，读者须跨包对照。

**重构候选**：在根包建立单一 `ownership` 文档/类型词汇（或 package doc），两包引用同一术语；属命名/文档级，风险低但收益亦偏软，优先级低于 C-1/C-2。

### C-4 `batch retire` 的 `finishBatch()` 未 `defer`（3.9 独立审查 Round 2 登记的既存健壮性观察，非修复级）

`agent/task/task_manager.go` 的 `RetireOrphans`（:1180-1200）/`reconcileZombies`（:1231-1253）以 `finishBatch := tm.beginBatchRetire()` … 循环 … `finishBatch()` 手动收尾，**未 `defer`**。若候选循环中途 panic，`tm.batchCollect` 会残留指向永不交付的孤儿 collector，此后所有并发 reconcile 均判「nested」并向其追加 → 结算静默丢失。属 `c14cdbc` 批量特性设计层（非本次并发修复引入），触发前提是循环内 panic（本身即灾难级），故 R1 已 PASS、R2 明确不重开。**跟进候选**（独立 change）：调用点改 `defer finishBatch()`，或 `beginBatchRetire` 提供 `func(){…}()` 闭包式 API 强制收尾。当前不改以免扰动 3.7 长跑证据基线。

## 3. 为何本 change 不动（证据失效账）

批次 C 已产出并被本 change 依赖的证据，任一核心结构重构都会作废、须全量重跑：

| 证据 | 载体 | 重构后是否失效 |
|------|------|--------------|
| 30 轮快速 E2E | `tests/resident_e2e_test.go`（3.2） | 数据流路径若变即须重跑 |
| 任务全链 E2E | `agent/task_chain_e2e_test.go`（3.3） | `ContextManager.persistBusEvent`/装配闭包若移即断 |
| 开关组合回归 | governance/evolution/meditation `switch_combo`（3.4） | build 装配顺序若变须重验 |
| 有界 fuzz | memory/event/compress/plugin（3.6） | 与被重构符号无直接耦合，影响较小 |
| 3.7 72h 长跑 | **BLOCKED 未验** | 结构大改后仍须在有授权长跑窗口前完成，顺序不可颠倒 |
| 3.9 独立审查（两轮，已通过） | `REVIEW-PACKAGE.md` §7 | 审的是**当前代码形态**（含并发修复）；结构重构后须重新独立审查，勿以旧签署背书新形态 |

**结论**：C-1/C-2 均为**真实但未致害**的可维护性债（现状功能正确、race/E2E 全绿），非缺陷。重构应作为**独立 change**，在 3.7/3.9 长跑与审查证据落定**之后**排期，避免以未收敛的证据基线去承接结构性改动。

## 4. 观察项（并入 `EXIT.md` §5 的 30 天清单）

- 生产若再现「换代后某子系统静默失明/接空状态」类事件，即命中 C-1/C-2 的漏点风险，应提前启动 `executorHandle`/`residentState` 拆分的独立 change。
- 若 `buildAgentDFS` 因新需求继续增长（超过当前规模一个量级），god-function 的「逐点 mode 判断」维护成本将超过重构成本——届时 `buildPlan` 声明表化收益转正。
