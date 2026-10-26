# 04 回归门：事件场景合同、归属检查、故障变异与映射闭环

> 状态：04.1/04.3/04.4/04.5 已实施并勾选；04.2 已开发、未勾选（生产验收待）。新合同、检查器扩展与自测已落地；产品测试来自 00/01/02/03，本域不新增产品测试。计划回写④后合同为 29 父/141 锚（28 绿 + 1 诊断），04.4 另登记三个后续变异反例（未启用）；EVID-04 的 mutation 补登记使变异自测当前为红，属 04.4 补变异待办。

## Why

现有回归资产只覆盖 `runtime-rebuild` 的 15 个金样场景：`run_runtime_scenarios.py` 只支持根模块、无 tags/race/独立重复执行；`check_runtime_evidence.py` 固定 15 个场景；`check_runtime_plan.py` 固定 10 个二级域与旧叶编号；架构检查只有包依赖边，没有事件写入/消费归属；没有故障变异门；`scripts/lint.sh` 在存在活动 graph 时仍检查归档 graph，属对象错位。更关键的是，若新合同只冻结 10 个 EI 场景并只覆盖 03 的父测试，就会漏掉 00/01/02 的各独立必跑负锚与 CLI 适配器，无法证明“每个域自己的错误路径都走不通”。

## What Changes

- **04.1 事件场景合同（显式清单，不固定十场景）**：新增 `golden/runtime/event_integrity_scenarios.json`。八类机制 E1—E8 固定；合同逐条登记**全部产品测试及其全部必跑子场景**：计划回写④后 graph 显式清单为 **28 个绿测产品测试 + 1 个诊断测试（00.2）+ 137 个必跑绿具名子场景 + 4 个诊断锚**（逐叶清单见 design D1；数量由 graph 清单长度导出，不写死 10）。合同 `subtests[]` 以 `(parent,name)` 为唯一键，每键只登记一条；`roles[]` 可多角色；`tests[].consumer_anchor` 只引用正锚行、不复制行；137/141 按唯一键计，不按角色引用求和；重复实体注册拒绝，合法多角色引用不拒绝。00.2 修复前红证单列；修复后四锚纳入最终 `event-short`，最终验收共 **29 个父测试 / 141 个具名锚**（`event-short` 28 父/138 锚、`event-long` 1 父/3 锚）。EI01—EI10 仅作为 03 的**整链组**，不得拿它替代 00/01/02/CLI 的独立测试与负锚。非产品叶（00.1/00.3/00.4、03.10、04.1—04.5、05.1—05.4）逐条映射 checker 或文档证据，不得要求 00 文档叶虚构产品测试。旧 `scenarios.json` 的 S01—S15 原样保留。
- **04.2 共用 runner 扩展**：扩展现有 `run_runtime_scenarios.py`、`runtime_checklib.py`、`check_runtime_evidence.py` 及其自测，支持 v2 合同的 `tests[]`/`whole_chain[]`/`non_product_leaves[]`/`scopes[]`、多模块、build tags、`-race`、`--repeat N` 独立重复（每次独立 UUID run-id/日志/证据，底层一律 `-count=1`）、`--seed`/`--window` 六组合、`--test-id` 精确子集（仅开发/生产验收用，CI 必须完整 scope）、`--out` 显式产物目录、源码树+运行配置指纹与零匹配判失败；新旧合同共用同一执行与对账引擎。04.2 的生产验收必须跑**真实 fixture/已就绪测试**（03.2—03.9 + 01.5/01.6/01.7 三个宿主装配）并产出十份独立证据，checker 自测里的合成 fixture 不能顶替；首跑不得拖到 05.1 才宣称 04.2 完成。计划回写④不扩其 `--test-id` 集：01.8/01.9/01.10a/01.10b 由各自叶十轮与 03.11/05 消费，不因新增叶扩大生产验收面。03 的验证命令是直接 `go test`，不调用 runner，因此不存在 04.2 → 03 → 04.2 的环。
- **04.3 直接调用归属检查**：保留 `golden/runtime/architecture.json` 与 `check_runtime_architecture.py` 的包依赖门；扩展 `scripts/codetools`（标准库解析/类型信息）核对冻结的直接调用所有者。可解析的违规直接拒绝；无法解析的调用登记 `uncovered` 并配独立检查，不冒充完整调用图，也不声称能静态识别任意“第二状态仓”语义。
- **04.4 故障变异门**：新增 `scripts/check_event_integrity_mutations.py` 与自测。五类候选变异（双路正文、忙时忽略、身份破坏、提前确认、错误路由）**只有在 00.3 证实后才启用**；00.3 新证实的实际缺陷不得漏登记。变异只能由原具名业务断言杀死；编译失败、超时、零测试命中、panic 无该断言均判验证失败。计划回写③另登记三个后续反例（存储失败报成功→01.8；交接前提前确认→01.2；进程仍活报完成→01.10a），只登记不启用；EVID-04 的 mutation 与三者均待后续单实际实施。
- **04.5 映射闭环与 lint 对象一致**：对齐叶—spec scenario—Golden—selector—子场景—消费者—档位；改名、缺叶、缺负锚、缺必跑子场景、空选择、证据复用、删除登记项使门失败；`lint.sh` 检查活动 graph 而非固定归档 graph，并补 runner/变异自测。`--bundle`/`--out` 是既有 runner/checker 的扩展模式，保留并与 05.2 同源。

## 边界与依赖

- **依赖（接口常数 vs 实现依赖）**：00.3 是契约冻结（实现依赖）；04.1 → 04.2 是开发顺序依赖；**04.2 的最终绿证前置为 03.2—03.9 产物与 01.5/01.6/01.7 三个宿主装配**，不是 05.1。03 的验证命令是直接 `go test`，不调用 04.2 的 runner，因此不存在 `04.2 → 03 → 04.2` 的环。04.4 复用 01.5、03.2—03.9 的正式测试作为“杀死断言”的消费者（实现依赖），并以 04.2 的 runner/检查器为接口；04.5 依赖 04.1—04.4 全部落地。旧 15 场景合同与旧十域 plan 规则是**接口常数**，不得被新合同改写。
- **被依赖方**：05.1 每 MR job、05.2 required 汇总、05.3 文档命令均消费本域合同、runner、变异检查器与映射门；F2 全量门禁。
- **接口面**：合同字段 `tests[]`（`id` = `leaf/TestName`（父测试级，与 graph `test_registry[].contract_id` 同形）/`leaf`/`kind`/`module`/`module_path`/`test_package`/`test_name`/`tags`/`race`/`tier`/`scope`/`subtests[]`/`consumer_anchor`）、`subtests[]`（`name`/`roles[]`/`families`/`mandatory`；`(parent,name)` 唯一，roles 可多选，`consumer_anchor` 引用正锚行不复制）、`whole_chain[]`（`id`/`test_id`/`families`/`aggregate_only`）、`non_product_leaves[]`（`leaf`/`evidence_kind`/`checker`/`doc_ref`）、`scopes[]`、`matrix`；证据 v2 字段 `run_id`/`scope`/`repeat_index`/`repeat_total`/`contract_digest`/`module_fingerprints`/`run_config`/`go_test_exit_code`/`test_log`/`subtests`/`verdict`；runner CLI（`--contract`、`--scope`、`--repeat`、`--seed`、`--window`、`--test-id`、`--out`）；`check_runtime_evidence.py --bundle <dir>`；codetools 新子命令 `event-ownership`；变异检查器 CLI `--all`/`--out`。不是产品 API，不新增运行时依赖。
- **禁止事项**：不建平行评估系统或第二份合同解析器；不新增第三方依赖；不固定十场景；不另造第二份 `EXPECTED_EVENT_MANIFEST` 元组真源（以 graph 冻结清单 + 源码 `TestEventIntegrity*` 发现清单 + 执行日志交叉对齐）；不宣称机器可防“删合同且同时改清单”的任意两处篡改（交独立 diff 审查）；不以 EI01—EI10 代替 00/01/02/CLI 独立测试；不让合成 fixture 冒充生产 runner 验收；不以名字匹配/字符串相邻冒充完整调用图或“第二状态仓”语义；不自动学习允许边；不把编译失败、超时、无测试命中算作“成功杀死”；不改写旧 S01—S15 或其历史归属；不全局放宽编号/数量/场景约束；不为新测试文件申请 test-mirror 例外；不在检查器里声称产品 PASS；不把原始运行产物写进仓库或 change 规划目录（证据摘要与 `log_ref` 可统一记入根 `ORCHESTRATION.md` 台账）。

## Capabilities

### Modified Capabilities

- `architecture-guardrails`：新增事件场景合同与共用 runner、直接调用归属、故障变异、映射闭环与 lint 对象一致的义务；delta 位于本域 `specs/architecture-guardrails/spec.md`。包依赖门、Golden 与既有质量基线不放宽。

## Impact

- 未来实施改 `golden/runtime/`、`scripts/` 与 `scripts/codetools/`；不触碰产品运行时、微信/CLI 宿主与主规格。
- 新增合同与检查器必须在无活动 `task-graph.json` 时如实跳过并说明，不得把“没有判定对象”报告成通过；归档目录不是检查对象。
- 旧 `scenarios.json` 的 15 项与 deterministic 行为保持不变；新合同使用同一 runner/检查器引擎，不另起评估系统。
- 原始运行产物用 `--out` 指向仓库外临时目录或 CI 指定的 artifact 目录；证据摘要与 `log_ref` 可统一记入根 `ORCHESTRATION.md` 台账，不另建每叶 summary 文件；仓库内未核验忽略规则的目录不得被宣称为已被忽略。
