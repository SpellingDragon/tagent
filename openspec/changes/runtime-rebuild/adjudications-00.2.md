# 00.2 归属核验缺口与裁决登记（adjudication record）

> 定位：00.2「601 条 Requirement 归属核验」发现的缺口与主编排者裁决的**单点登记**。coverage.md §10 是核验数据（601 行归属表），本文是**裁决依据与结论**的独立存档——供 00.P0.3 评审门、阶段二实施与未来审计追溯，不与 coverage 互抄。
> 裁决人：主编排者（2026-10-13，apply 波 00.2 闭合时）；依据链：外部批准计划 §2「保留的用户要求/不做」＋ expectations §0 口径 ＋ 旧变更（rebuild-formal-runtime）已裁定事项。

## 一、发现的缺口（核验实证，非推测）

归属核验（标题级+声明级+17 条正文对位实证）发现：

| 缺口类 | 规模 | 实证 |
|---|---|---|
| **M 类归属空洞** | 103 条 / 19 能力 | 既无同名 delta、亦无域声明承载（§10.3 具名清单） |
| **M↔继承标签张力** | 9 能力 | §4 标 M（需修改）而域 proposal/design 声明「继承/只读/不改」 |
| **压缩族承载疑点** | 4 能力自称「以 03 delta 为准」但 03 域无同名 delta | 压缩语义疑似落在 event-sourced-projection 正文（未列名） |

## 二、裁决（三类处置 + 张力 + 收口）

### 裁决 A：M→H（机制退役，2 能力 21 条）

| 能力 | 条数 | 依据（反事实检验：新计划删掉此机制谁坏？） |
|---|---|---|
| framework-flow-adapter | 11 | 新计划无 flow adapter 概念（O 非目标：无第二工具循环）；行为面（请求装配/代绑定）由 02 persistent-event-loop delta 承载——机制死、行为活 |
| swappable-executor | 10 | 新计划用 state 生效绑定+借用（01 三对象）替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |

### 裁决 B：M→R（能力保留、从零实现，13 能力 65 条）

| 能力组 | 条数 | 归属任务 | 依据 |
|---|---|---|---|
| 压缩族 9 能力（task-skeleton-compression 22 / compaction-scheduler 3 / compress-quality-fix 4 / compression-user-message 3 / event-compaction 3 / view-transform-stateless 2 / summary-memory-rag 4 / relation-store-lru-rocks 4 / snowflake-overflow-handling 2） | 47 | 03.4（投影策略）/03.2（后端压实） | 新计划明文「不把 L0–L3 算法或每个阈值当架构要求」——旧算法条款是背景不是契约；能力本身（投影/预算/回放/压实）从零实现 |
| error-handling-consistency | 3 | 00.6 | 迁移适用检查规则时逐条评估 |
| production-wiring-fix | 5 | 08.1 | 根包从零装配；旧 setter 注入/Close 顺序修复仅背景 |
| readme-enhancement | 3 | 09.6 | 文档闭合时重写 |
| telemetry-channel | 7 | 05.3 | OTel/审计从零接入；旧通道身份条款仅背景 |

### 裁决 C：正文对位实证（4 能力 17 条——先实证后补登）

逐条实读预期 delta 正文，**语义确覆盖才补指向**：

| 能力 | 对位成立（保留 M） | 对位不成立（改 R） |
|---|---|---|
| resident-continuity | 3 → config-state-twinship（生效 prompt 由 state 持有×1、候选活绑定×2） | 6 → 01.5 |
| task-reentry | 0 | 5 → 02.7 |
| async-tool-event-fix | 1 → async-task-execution（按任务类型 settle 探测器） | 1 → 02.3 |
| event-key-visibility | 0 | 1 → 02.5 |

**纪律**：语义近邻但未列名者一律判不成立（防「语义对位」过誉——宁多改 R 从零、不冒充已覆盖）。

### 裁决 D：张力处置（9 项，以域声明为准）

- 7 项维持 M ＋ §4 备注「域声明继承，见 §10.2」：context-scoping、generation-bound-model-references、evals-harness、event-relation-store、example-file-memory-wiring、self-improvement-meditation、wechat-host-send-tool
- 2 项未决维持 M（域声明为非重合声明，不猜）：runtime-resource-ownership、task-registry-and-board

### 收口（主编排者补刀，2 行）

task-reentry / event-key-visibility：§10.2 全条改 R 后 §4 标签同步改 R（代理按白名单纪律未越权改 §4，由编排者收口）。

## 三、裁决后终态

- 601 条归属：R 244 · M 268 · H 60 · D 31（空洞 0、未处置 0）
- §4 主表 120 能力：R 59 · M 47 · H 9 · D 5
- 深度边界（诚实声明）：标题级+声明级+17 条正文级；1217 个 Scenario 未逐条审读——M 类 delta 覆盖的语义审定随各域 delta 撰写完成，本登记不替代

## 四、关联文件

- 核验数据：coverage.md §10（601 行归属表+空洞清单+H 抽查+D 核对）
- 裁决落点：coverage.md §4/§10.2/§10.3/§10.6（处置结果列）
- 勾选凭据：task-graph.json 00.2 evidence（apply-00.2-20261013）
