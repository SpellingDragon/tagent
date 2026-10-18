# 概念收敛战役（concept-slim）——实现者心智负担削减计划

> 2026-10-17 裁定（用户）：**破坏式收敛**（合并/收窄/删概念，已勾选面按需重验，本战役显式覆盖"旧机制不下线"在 API 简化面的适用）／**全仓规划按痛点排波**／**验收=静态上限+盲测双轨**。
> 判据（用户定）：多态实现开发者只应关心"要做什么事、与什么有关系"，零额外选择；一看就懂。

## 一、验收标尺（每条可复跑/可判定）

| # | 标尺 | 判定 |
|---|---|---|
| S1 | 多态接口 ≤2 方法；消费者侧碎接口归并后单口 ≤3 方法 | check_concept_budget.py（机器门） |
| S2 | 配置/参数结构 ≤5 导出字段；超出改零选择构造＋按需 WithX，或按语义块拆分。**类别豁免**：数据/记录 schema（序列化保真面，字段=数据语义本体非配置选择，如 event.Event 16f/AttemptRecord 等）不入 S2——波27 裁定，加性保真字段走具名豁免 | 同上 |
| S3 | ctx 隐式关系＝0：跨缝关系全部入签名或类型化 accessor＋ctx 契约表 | 机器门（禁裸 ctx.Value）＋评审 |
| S4 | 每抽象＝接口文件＋≤1 页 canonical 范例（一窄口一文件）＋MUST/MUST NOT 红线清单（≤6 条） | 机器门（范例存在性/红线格式） |
| S5 | 盲测锚：不知情 agent 仅读接口文件＋范例，写出编译过＋负向用例过的新实现＝达标 | 盲测报告（09.21 终验） |
| S6 | 既有回归锚不破或级联适配（强度不降）；全回归十包绿 | go test（回归锚） |

## 二、分域收敛方案（现状→目标，实测基线）

| 域 | 现状概念面（实测） | 收敛动作 | 目标 | 破坏面/重验面 |
|---|---|---|---|---|
| query（05） | 12 接口（AgentFace/SessionFace/OccupancyFace/TaskLister/TaskReader/NoticeLister…）＋InventoryFaces(8f) | 碎读面归并为 `Reading`（列举/读取单口）＋`Explainer`；InventoryFaces 按语义块拆 | ≤4 接口；8f 结构≤5f | TestRuntimeInventory/TestQueryAuthorizationAndCursor/TestExplainRuntimeBlockers 级联 |
| plugin（04） | 14 接口＋Policy(9f)＋ApprovalRequest(11f) | Policy→零选择构造（warn 档默认）＋WithX 加门；ApprovalRequest 拆 Subject/Evidence 块；五空相位限期接或删 | Policy 核心≤4f；请求≤3 块 | TestFormalLoopEnforcesPolicy/TestCriticalCallsHoldThroughFormalPath 级联；08.2 按新面实施 |
| 组合根（00） | run.go 1700 行＋9 Option 单文件 | 按供面拆文件族（run/build/opts/read），Option 分组文档化 | 单文件≤400 行 | 全部 host/execution 锚回归 |
| rl（06） | ExportManifest(11f)/SampleProvenance(11f)/CaptureLedger(8f) | 按语义块拆分（身份/账目/摘要） | 每结构≤5f | TestAuthorizedTrainingExport/TestExportTracesFormalRecords 级联 |
| tool/exec（04） | Explanation(10f) | 核心 3f＋扩展块 | ≤5f | TestExecutionResourceLifecycle 级联 |
| agent（02） | 7 接口（Recorder/HistoryFace/RequestFace/RequestSnapshotFace/Producer/Reconciler/ProducerWatch） | 评估合并（Producer×3 是否一缝三面）；ctx 契约表 | ≤5 接口 | execution/learning 锚回归 |
| memory（04） | 12 接口（Embedder/kvPort/OriginalStore/ScopedOriginalStore/Guard/LifecycleStore…） | 端口归并审视（读/写/保护三语义单元） | ≤7 接口 | 04.7/04.8/04.9 锚回归 |
| event（00） | Event(16f)/ParsedMeta(9f) | 16f 为事件语义本体——评估保留＋文档分组；ParsedMeta 为解析辅助可瘦身 | 文档分组；ParsedMeta≤5f | state/event 锚回归 |
| 跨域 | ctx 键值缝（TriggerSourceFrom/CallPassFrom/CallFrame） | 入签名或类型化 accessor＋ctx 契约表（S3） | 裸 ctx.Value=0 | capabilities/execution 锚回归 |
| 跨域 | 范例分散（测试内 4+ 个变体实现） | 每窄口/端口一 canonical 范例文件（examples/contract） | S4 达标 | — |
| 跨域 | 红线散文化 | MUST/MUST NOT 清单化入 Godoc | S4 达标 | — |

## 三、波次编排（与余下 16 叶穿插：未实施叶直接按收敛后新面）

```
W-S0 工装先行: 00.21 概念预算检查器+标尺入 contracts
W-S1 三痛点:   05.21 query 归并 ∥ 04.21 plugin Policy ∥ 00.22 组合根拆分
W-S2 面收敛:   04.22 窄口裁决+范例 ∥ 02.21 ctx 显式化 ∥ 06.21 大参数面拆分 ∥ 04.23/00.25/02.22 各域审视
W-S3 全仓收口: 00.23 红线清单 ∥ 00.24 范例全仓 → 09.21 盲测终验
穿插规则: 08.2/04.4 前置 04.21+04.22; 06.5/06.6 前置 06.21; 08.3-08.5 前置 00.22 —— 未实施叶零旧面返工
```

## 四、任务清单（C 系列，挂域；verify=静态锚+回归锚，09.21 负盲测锚）

| 叶 | 域 | 产出 | 前置 |
|---|---|---|---|
| 00.21 | 00 | scripts/check_concept_budget.py（S1-S4 规则）＋CI 步骤＋自测 | — |
| 05.21 | 05 | query 碎接口归并（12→≤4）＋InventoryFaces 瘦身 | 00.21 |
| 04.21 | 04 | Policy 零选择构造＋ApprovalRequest 拆块 | 00.21 |
| 04.22 | 04 | 五空相位限期接或删＋每窄口 canonical 范例 | 04.21 |
| 02.21 | 02 | ctx 契约表＋类型化 accessor/入签名 | 00.21 |
| 00.22 | 00 | run.go 拆文件族（≤400 行/文件） | 00.21 |
| 06.21 | 06 | **口径勘误（波28，S2 类别豁免级联）**：ExportManifest/SampleProvenance/CaptureLedger/Explanation/FeedbackSubmission=数据面不拆、走豁免登记；本叶=FeedbackEndpointConfig（配置面 6f→零选择构造）＋数据面豁免具名登记 | 00.21 |
| 00.23 | 00 | 红线清单化（MUST/MUST NOT 全窄口/端口） | 04.22 |
| 00.24 | 00 | 范例单点全仓（每接口一范例） | 04.22, 00.23 |
| 09.21 | 09 | 盲测终验（不知情 agent 双盲测：plugin 窄口＋query 读口） | 05.21, 00.22, 00.24 |
| 04.23 | 04 | memory 端口归并审视 | 00.21 |
| 00.25 | 00 | **口径勘误（波28，S2 类别豁免级联）**：ParsedMeta=解析结果本体（数据面）不瘦身；本叶=Event 16f 语义块文档分组（doc-only）＋数据面豁免具名登记 | 00.21 |
| 02.22 | 02 | agent 端口面审视合并 | 02.21 |
