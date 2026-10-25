# 04 回归门：显式事件清单、归属检查、故障变异与映射闭环

> 本设计只描述取舍与精确落点；文内“拟新增/拟扩展”为设计时措辞，实施状态以本域 [tasks.md](tasks.md) 为准（04.1/04.3/04.4/04.5 已实施并勾选，04.2 已开发、待生产验收）。检查器输出永远不声称 `productVerified`；证据只证明“命令 + 选择器 + 产物”一致，不证明产品语义。

## D0 只读事实（决定方案的硬约束）

- 旧合同 `golden/runtime/scenarios.json` 为 schema v1、S01—S15；`check_runtime_evidence.py` 以 `EXPECTED_SCENARIOS = 15` 固定旧数量。
- `run_runtime_scenarios.py` 已有 `--scope/--contract/--root/--out/--timeout/--json`，`--out` 缺省落系统临时目录；但它只支持根模块单包、无 tags/race/repeat/seed/window，`run_id` 是“时间戳 + scope”，不能区分独立重复。
- `check_runtime_evidence.py` 没有 `--bundle`；`check_runtime_plan.py` 固定旧十域；`scripts/lint.sh` 在存在活动 `task-graph.json` 时仍检查 `archive/2026-10-18-runtime-rebuild`。（以上为设计时基线事实；04.5 已扩展：plan checker 按 graph `domain_profile` 读取六域/叶数/phase，evidence checker 增 `--bundle`，lint 改查活动 graph；旧十域常量保留。）
- 产品测试由 00—03 提供，本域不新增产品测试。计划回写④后显式清单为 **28 个绿测产品测试 + 1 个诊断测试（00.2）+ 137 个绿具名子场景 + 4 个诊断锚**；最终验收（`event-short` + `event-long`）为 **29 个父测试 / 141 个具名锚**（含 03.11 的 1 父 3 锚；诊断红证单列、不计绿锚）。数量由 graph 显式清单导出，不写死“十个场景”。

## D1 合同：显式测试清单，EI01—EI10 只作整链组

新文件 `golden/runtime/event_integrity_scenarios.json`，`schema_version: 2`、`profile: "event-integrity"`；旧 `scenarios.json` 与 S01—S15 逐字保留。

- `families`：E1—E8 固定八族，族名与 00.1 契约盘点一致。
- `tests[]`：每个产品测试一条，字段 `id`、`leaf`、`kind`（`product-green`/`diagnostic`）、`module`、`module_path`、`test_package`、`test_name`、`tags`、`race`、`tier`、`scope`、`subtests[]`、`consumer_anchor`。`id` 固定为 `leaf/TestName`（父测试级），与 graph `test_registry[].contract_id` 同形，例如 `01.5/TestEventIntegrityFormalAssembly`。
- `subtests[]`：`name`、`roles[]`（`positive`/`negative`/`consumer`/`control`，可多选）、`families`、`mandatory`。`(parent,name)` 为唯一键，每键只登记一条；`tests[].consumer_anchor` 只引用同父测试下 roles 含 `consumer` 的已登记行（可同时具有 positive 角色，也可为独立消费者场景），不复制 consumer 行。绿测的每个具名子场景都是 `mandatory: true`；诊断测试的 4 个具名锚在修复后同样 `mandatory: true` 并纳入最终 `event-short`（`tier` 含 `event-short-final`），修复前红证按 00.2 单独记录、不冒充绿证据。
- `whole_chain[]`：`id`（EI01—EI10）、`test_id`、`families`、`aggregate_only: true`、`note`。它只表示“03 的整链聚合组”，不新增测试、不新增子场景、不得替代任何 00/01/02/CLI 独立测试。
- `non_product_leaves[]`：`leaf`、`evidence_kind`、`checker`、`doc_ref`（`checker`/`doc_ref` 至少一个非空）。00.1/00.3/00.4、03.10、04.1—04.5、05.1—05.4 用它登记，不要求虚构产品测试。
- `scopes[]`：`event-short` 选全部绿测 + 00.2 诊断测试（03.11 除外），`repeat: 10`，即 28 父/138 锚；`event-long` 只选 03.11，`repeat: 1`，即 1 父/3 锚。00.2 修复前的红证单独记账，不进入 green bundle；修复后四锚随 `event-short` 十轮独立执行。
- `matrix`：`seeds [1,7,42]`、`windows [256,32768]`、`sessions 4`、`inputs_per_session 300`、`restarts_per_session 30`、`per_combo {inputs:1200, restarts:120}`、`total {inputs:7200, restarts:720}`；任何缩减或与总量不一致即拒绝。

逐叶显式清单（测试数为绿测条目；诊断条目单列）：

| 叶 | 测试（模块/包） | tags | 绿子场景 |
|---|---|---|---|
| 00.2 | `TestEventIntegrityReproduction`（host） | — | 诊断：4 个具名子场景，不纳入绿矩阵 |
| 01.1 | `TestEventIntegrityNoticeIdentity`（execution） | — | 6 |
| 01.2 | `TestEventIntegrityConsumptionRecovery`（history） | — | 7（含补差 `handover_before_confirmation`） |
| 01.3 | `TestEventIntegrityTaskContinuation`（host） | — | 9（含补差 `notice_input_without_projection`） |
| 01.4 | `TestEventIntegrityDelivery`（host） | — | 7 |
| 01.5 | `TestEventIntegrityFormalAssembly`（host） | — | 2 |
| 01.6 | `TestEventIntegrityHostAdapters`（wechat） | smoke | 2 |
| 01.7 | `TestEventIntegrityCLIAdapter`（cli-host） | — | 2 |
| 01.8 | `TestEventIntegrityPersistence`（history） | — | 6 |
| 01.9 | `TestEventIntegrityRestoreUse`（history） | — | 5 |
| 01.10a | `TestEventIntegrityExecLifecycle`（exec，包内测试） | — | 6 |
| 01.10b | `TestEventIntegrityExecStateEvents`（execution） | — | 6 |
| 02.1 | `TestEventIntegrityMeditationSchedule`（evolution） | — | 6 |
| 02.2 | `TestEventIntegrityReflectionDelivery`（host） | — | 11 |
| 02.3 | `TestEventIntegrityProjection`（history） | — | 6 |
| 02.4 | `TestEventIntegrityReplacement`（host） | — | 5 |
| 02.5 | `TestEventIntegrityApproval`（capabilities） | — | 9 |
| 02.6 | `TestEventIntegrityApprovalWeChat`（wechat） | — | 6 |
| 02.7 | `TestEventIntegrityCapture`（learning） | — | 6 |
| 03.1 | `TestEventIntegrityHarness`（host） | — | 3 |
| 03.2 | `TestEventIntegrityIdentityMatrix`（host） | — | 3 |
| 03.3 | `TestEventIntegrityDecisionFlowMatrix`（host） | — | 3 |
| 03.4 | `TestEventIntegrityApprovalControl`（wechat） | smoke | 3 |
| 03.5 | `TestEventIntegrityReflectionDeliveryControl`（wechat） | smoke | 3 |
| 03.6 | `TestEventIntegrityFoldControl`（wechat） | smoke | 3 |
| 03.7 | `TestEventIntegrityReplacementControl`（wechat） | smoke | 3 |
| 03.8 | `TestEventIntegrityCaptureControl`（wechat） | smoke | 3 |
| 03.9 | `TestEventIntegrityRestart`（history） | — | 3 |
| 03.11 | `TestEventIntegrityLongRun`（wechat） | smoke | 3 |

> 03.10 为只读一次性校准（non_product），无产品测试、不建 registry 行；其独立父测试 `TestEventIntegrityLongRunCalibration` 登记于 graph `source_only_tests`（`off_contract`），三锚写在叶 verification、不进合同计数。

- 合计 28 个绿测产品测试（每叶一父测试，分布在 28 个产品叶；含 01.5/01.6/01.7 三个宿主装配叶、01.8/01.9 持久化与恢复、01.10a 执行资源查询面、01.10b 执行状态事件与接线，以及 02.5/02.6 两个审批叶）、1 个诊断测试（00.2）、137 个绿具名子场景、4 个诊断锚；最终 `event-short` 为 28 父/138 锚（含 00.2 修复后四锚），`event-long` 为 03.11 的 1 父/3 锚，全量 29 父/141 锚；以上计数均按唯一 `(parent,name)` 键计，不按 positive/negative/consumer 角色引用求和，数量由 graph 清单长度计算。
- `whole_chain` 的十组：EI01 → 03.2；EI02 → 03.3；EI03 → 03.4；EI04 → 03.5；EI05 → 03.6；EI06 → 03.7；EI07 → 03.8；EI08 → 03.4（控制消费者整链锚）；EI09 → 03.9；EI10 → 03.11。每组 `aggregate_only: true`，只用于整链验收报告，不计入测试/子场景计数，也不得成为某叶的唯一证据。
- 合同检查器不另造第二份 `EXPECTED_EVENT_MANIFEST` 元组真源：以 `task-graph.json` 冻结清单（`test_registry` + `leaves[].verification`）、源码 `TestEventIntegrity*` 父测试/`t.Run` 发现清单与执行日志三方交叉对齐。缺一条、改名、改 roles、把 `mandatory` 降为 false、增加未登记条目均非零；重复 `(parent,name)` 实体注册拒绝，合法多角色（如 positive+consumer）不拒绝；计数按唯一 `(parent,name)` 键由 graph/发现清单长度导出，不另填常数。旧 v1 `EXPECTED_SCENARIOS = 15` 规则原样保留。仅删除合同而 graph/源码仍在时机器可拒；同时删合同与 graph 条目无法由机器判定为“任意两处篡改”，由独立 diff 审查拦截，文档与检查器不得声称机器可防双删。
- 拒绝规则：缺族、某族只被 `whole_chain` 覆盖、重复 test/subtest（同一 `(parent,name)` 第二条；合法多角色引用不算重复）、空 selector、产品测试缺 positive/negative 角色、产品测试缺 consumer anchor、非遗留叶无 checker/doc_ref、`tier`/`scope` 不一致、矩阵缩减、旧 S01—S15 被改写，均失败。
- 被否方案：只冻结 EI01—EI10 十个“场景”并用它覆盖 03 父测试——会漏掉 00/01/02/CLI 的独立必跑负锚；把每个子测试拆成独立合同条目并复制选择器——无法证明同一测试内的身份隔离；为 00 文档叶虚构产品测试。

## D2 runner 与证据检查器：同一引擎，新旧分流

- v1：保持现行为（根模块、无 tags/race/repeat、`-count=1`、同一命令形态），不改旧 CLI 语义。
- v2：按 `tests[].module`/`module_path` 用 `go -C <module> test <package> -run '^(…)$' -count=1 [-tags …] [-race] -json`；包路径按该模块 module path 精确归一化；合同路径经 `TAGENT_SCENARIO_CONTRACT` 传给测试。
- `--repeat N`：N 次**独立** `go test -count=1` 调用，每次独立目录 `repeat-<i>/`、独立 UUID `run_id`、独立 `test-log.jsonl` 与 `evidence.json`；N 次全部成功才通过。`event-short` 合同声明 repeat=10；`event-long` repeat=1。禁止任何 `-count=N`（N>1）。
- `--seed`/`--window`：值必须在合同 `matrix` 内，追加 `-args -seed=<n> -window=<w>`；六组合由 CI matrix 或本地循环逐次调用，每次独立证据；不得把六组合塞进一次 `go test`。
- `--test-id <leaf>/<TestName>`（可重复）：合同 `tests[].id` 即 `leaf/TestName`（父测试级），graph `test_registry[]` 以同名 `contract_id` 登记；未登记或传子场景名即用法错误。它只用于开发/04.2 生产验收的已就绪子集；CI 的 `event-short`/`event-long` 必须用完整 scope，不得用 `--test-id` 缩小 MR 面。同一父测试的三锚随该次执行一并核验，不按 `--test-id` 乘三。
- `--out`：任务与 CI 一律显式传仓库外临时目录（本地 `${TMPDIR:-/tmp}/…`，CI `${{ runner.temp }}/…`）；原始日志/证据写入该目录，证据摘要与 `log_ref` 可回填根 `ORCHESTRATION.md` 台账，不另建每叶 summary 文件；未传时沿用现有系统临时目录，仍不得把原始日志写入仓库或 `openspec/changes/`。
- 零匹配即失败：即使 `go test` 因无匹配用例退出 0，只要 JSON 中没有该测试的 `run` 终局，就判 unattested；不信任进程退出码单独作证。
- 证据 schema（v2）：
  ```json
  {"schema_version":2,"profile":"event-integrity","run_id":"<uuid>",
   "scope":"event-short","repeat_index":1,"repeat_total":10,
   "contract_digest":"sha256:…","test_ids":["01.1/TestEventIntegrityNoticeIdentity"],
   "module_fingerprints":[{"module_path":"…","dir":".","revision":"…","content_hash":"…"}],
   "run_config":{"seed":null,"window":null,"tags":[],"race":true},
   "go_test_exit_code":0,"test_log":"/tmp/…/test-log.jsonl","test_log_sha256":"…",
   "subtests":[{"test_id":"…","name":"positive","status":"pass"}],"verdict":"match"}
  ```
- 指纹覆盖受影响第一方源码、必要未跟踪测试、合成配置/fixture、合同、各模块 `go.mod/go.sum`；不只记 HEAD。`run_config` 区分 repeat index、seed/window、tags、race 与选择器集合。
- `check_runtime_evidence.py`：v1 保持 `EXPECTED_SCENARIOS = 15` 与旧字段；v2 以 graph 清单/源码发现/执行日志交叉校验八族覆盖、`mandatory` 子场景齐全、scope/tier 一致、`whole_chain` 只引用已登记 test_id。record 的 run/pass/终局/重复 run/skip/截断规则原样复用；同一执行凭据（run_id/日志真实路径/artifact）不得冒充不同独立轮次或不同 matrix 组合——同一轮内多个 case 可共享同一条日志，但每个 case 必须按具名 selector 独立核验；后补 pass 不能覆盖先前 fail。
- 被否方案：另写 `event_runner.py`/第二套证据检查器；用 `--test-id` 作为 CI 缩小面；把 `run_id` 继续做成“时间戳+scope”导致十轮无法区分。

## D3 plan 检查：按变更声明 profile，旧十域规则冻结

- `task-graph.json` 的 `domain_profile` 是字符串标识（当前 `"event-integrity"`），不是对象；plan checker 内置固定 profile 规则：该标识指向六域（00—05）校验，旧十域常量保持独立。叶数、id 范围、阶段门状态与 phases 一律从 graph 自身的 `counts`/`leaves`/`phases` 读取，profile 不复制这些状态。新变更按该规则校验 id 覆盖、域归属、叶 checkbox 双向一致、依赖无重复/未知/自环/环。
- 无 `domain_profile` 的旧变更（如 `runtime-rebuild`）继续走冻结常量：10 域、65 实施 + 24 骨架 + 13 收敛、`00.P0.3` 阶段门；不得为了新变更全局放宽。
- `coverage.md`：旧变更覆盖全部主规格能力；新变更按 `coverage.md` 与 graph 的显式能力条目校验（本变更三份主能力 + 其余域声明），多/少/重复均失败；不在 profile 里另存一份能力清单。
- 六域子变更本身直接以 `openspec change validate event-consumption-integrity/changes/<domain> --strict --json --no-interactive` 校验（00 已实测可用），不临时复制到顶层、不改主规格；plan checker 不重复实现该 CLI 校验。
- `check_artifacts`/`check_specs`/`collect_checkbox_ids` 的域列表来自 profile，不再写死 `DOMAINS`；新域必须有 proposal/design/tasks 与至少一份 spec，ADDED/MODIFIED/REMOVED 结构规则原样复用。
- 被否方案：把新变更伪装成旧十域编号；复制一份 `check_runtime_plan_event.py`。

## D4 直接调用归属：只判可解析的直接调用

- `golden/runtime/architecture.json` 保留现有 `packages`/允许边；新增 `event_ownership` 块：`schema_version`、`rows[]`（`id`、`owner`（包.函数/方法）、`allowed_callers`、`forbidden_callers`、`reason`）。表是人工冻结输入，MUST NOT 从现存代码自动学习。
- `scripts/codetools/event_ownership.go`（拟新增）：
  - 用标准库 `go list -json` 取包与文件，`go/parser`/`go/ast` 解析，`go/types` + `types.Info.Uses` 解析直接调用目标；不引入 `golang.org/x/tools` 或任何新依赖。
  - 只核对 `event_ownership.rows` 中冻结 owner 的**直接调用点**；调用者按包/函数记录。`allowed_callers` 放行，`forbidden_callers` 命中即非零。
  - owner 无法解析、类型信息缺失、接口/反射/间接调用一律计入 `uncovered` 并如实输出；MUST NOT 计为已覆盖，MUST NOT 用名字相似匹配补洞。
  - 输出 JSON：`rows`、`uncovered`、`violations`、`exit`。`violations` 非空或冻结 owner 未解析即失败；`uncovered` 每个调用点写 `file:line`、调用者与原因，由 04.3 的独立复核记录逐条判定可达性，不静默放行。
- 包依赖方向仍由 `check_runtime_architecture.py --implementation` 判定；本检查不替代、不修改包边判据。`scripts/lint.sh` 增加 `go run ./scripts/codetools event-ownership --check`。
- 不声称能静态识别任意“第二状态仓”语义：本检查只回答“冻结 owner 的可解析直接调用者是否在允许集合内”；语义级重复状态判断留给 00.3/独立评审，不在工具输出中伪装成已覆盖。
- `scripts/codetools/event_ownership_test.go`（拟新增）覆盖：合法直接调用通过；对冻结 owner 的禁止直接调用被拒；无法解析调用进入 `uncovered` 且不计数为覆盖；名字相似但非目标的调用不被误判；冻结 owner 本身无法解析时失败。
- 被否方案：用字符串相邻/正文前缀猜语义；自制解析器；声称已构建完整调用图或可识别任意第二状态仓。

## D5 故障变异：只启用被证实的形态，只认具名业务断言

- 新 `scripts/check_event_integrity_mutations.py`，变异清单作为 Python 常量随工具保存（不依赖本 change 目录存活）：`id`、`basis`（00.3 已证实缺陷 id）、`description`、`file_replacements`、`test_module`/`test_package`、`test_selector`、`expect_assertion`。
- **五类候选**（双路正文注入、忙时忽略后到通知、通知身份/字段破坏、提前确认消费、跨会话错误路由）只有在 00.3 或后续复现证实后才启用；00.3 新证实的实际缺陷必须登记为新的 mutation id，不得漏；未证实形态不得为了凑数开启。
- **计划回写③后续登记（未启用，实际变异实施在后续单）**：存储失败却报提交成功（锚 01.8 `storage_failure_not_reported_as_success`）、交接前提前确认导致未交接结果丢失（锚 01.2 补差 `handover_before_confirmation` 的负向断言）、进程仍活却报完成（锚 01.10a `stable_is_not_success` / `partial_output_is_not_completion`）。三者只登记候选，不得在实施前计入 killed 或声称已覆盖；EVID-04 的 mutation 同样属 04.4 补变异待办。
- 自测读取 `evidence/00.3-frozen-contract.json`，断言每个 `confirmed` 缺陷恰有一个 mutation `basis` 指向它；缺映射即失败。00.3 未确认的候选在报告中列为 `not_evidence_backed`，不计入“已杀死”。
- 流程：先跑控制树（未变异）确认选择器通过；再把当前工作树复制到临时隔离目录，应用替换，跑同一 `go test -run '^…$' -count=1`；仅当进程非零**且**输出包含 `expect_assertion` 具名业务断言时记为 killed。
- 编译失败、超时、零测试命中、panic 但无该断言、测试本身被改绿均 MUST 判该变异“验证失败”，不得计为成功杀死；未知 id、清单与源码锚点失配、控制树不绿均使总门失败。
- 证据记录基线源树摘要、实际 overlay/补丁摘要、实际命令、原始退出码与命中断言；不伪装与控制树同内容。原始报告与证据写入 `--out` 指定的仓库外临时目录（CI 用 `${{ runner.temp }}/event-integrity-mutations`）；证据摘要与 `log_ref` 可回填根 `ORCHESTRATION.md` 台账。`--json` 输出机器可读汇总。
- 被否方案：`-run` 空匹配也 exit 0 时按“无失败”放行；把编译失败/超时当“杀死”；在真实工作树上打补丁后忘记还原；为未证实候选编造 mutation。

## D6 映射闭环、bundle 与 lint 对象

- 叶—测试映射：00—05 共 42 叶，每个叶仅采用产品测试或非产品凭据中的一种归类；计划回写④后每个产品叶恰关联一个唯一父测试（01.5 根装配、01.6 微信宿主装配、01.7 CLI 宿主装配、01.8/01.9 持久化与恢复、01.10a 查询面、01.10b 状态事件与接线各一测），非产品叶在 `non_product_leaves[]` 登记一次。每个测试只归属一个叶，非产品叶用 checker/doc_ref，不得互相冒充。每个 `tests[].leaf` 必须在活动 graph 存在；每个 `whole_chain[].test_id` 必须存在且 `aggregate_only: true`。
- 子场景映射：每个 `mandatory: true` 子场景必须在所选测试文件的 `t.Run("<name>"` 中真实存在；每个产品测试至少 1 个 positive、1 个 negative 角色，且 `consumer_anchor` 指向同父测试下 roles 含 `consumer` 的已登记行（可同时具有 positive 角色，也可为独立消费者场景）（不另增重复行）；多角色行仍只计一个唯一键；八族每族至少被一个 mandatory 子场景覆盖。改名/删除/空 selector/零匹配均失败。
- 数量与删除：合同与 graph 冻结清单、源码发现清单、执行日志三方交叉对齐；`event_test_count`/`event_subtest_count` 按唯一 `(parent,name)` 键由 graph/发现清单计算并在报告中打印，roles 引用不重复计数；仅删合同条目而 graph/源码仍在即失败；同时删合同与 graph 条目的 diff 由独立审查拒收，检查器不声称机器可防任意两处篡改。不得用“固定十个 EI 场景”作为数量口径。
- `check_runtime_evidence.py --bundle <dir>` 汇总模式：收集多份 `evidence.json`，按 `scope`/`run_config` 校验——
  - short 恰好 10 份，`run_id`/日志真实路径互不相同，`repeat_index` 覆盖 1..10，全部终局通过；
  - long 的 `(seed, window)` 集合恰为 `{1,7,42} × {256,32768}` 六项，每项一份，缺一/重复/额外均失败；
  - mutations 恰一份，控制树绿、每个登记 mutation 由 `expect_assertion` 杀死、`not_evidence_backed` 为空；
  - 所有证据的基线源树摘要一致，运行配置按 repeat/seed/window 区分；
  - 缺 artifact、空目录、截断、skip、重复 run、后补 pass 覆盖先 fail 均失败。
  该模式供 05.2 required 复用，不新增第二套矩阵/指纹规则。
- `scripts/lint.sh`：存在活动 `openspec/changes/*/task-graph.json` 时检查该活动 graph（按 profile），不再固定检查归档 graph；无活动 graph 时打印明确 skip，不伪造通过。增加 runner/变异/plan/evidence 自测的 `unittest discover`，保留现有 `test_check_runtime_*.py`。
- 负控：改名一个叶、删一个叶、删一个 negative 子场景、删一个 non-product 映射、把 selector 置空、重复 `(parent,name)` 实体注册、把同一执行凭据冒充不同独立轮次/不同 matrix 组合、五组合冒充六组合、九次冒充十次、混基线摘要、活动 graph 存在却检查归档 graph——每种都必须使门失败；同一行 `roles=[positive,consumer]` 的 dual-role-consumer 必须被接受且唯一键计数不增。
- 被否方案：只做“键名对齐”不做值/身份关系；把旧 15 场景检查脚本硬编码扩展到新图；把 `EI01—EI10` 当独立测试计数。

## D7 精确文件

拟新增：

- `golden/runtime/event_integrity_scenarios.json`
- `scripts/check_event_integrity_mutations.py`
- `scripts/test_check_event_integrity_mutations.py`
- `scripts/codetools/event_ownership.go`
- `scripts/codetools/event_ownership_test.go`

拟扩展：

- `scripts/run_runtime_scenarios.py`、`scripts/runtime_checklib.py`、`scripts/check_runtime_evidence.py`
- `scripts/check_runtime_plan.py`
- `scripts/test_run_runtime_scenarios.py`、`scripts/test_check_runtime_evidence.py`、`scripts/test_check_runtime_plan.py`
- `golden/runtime/architecture.json`、`scripts/check_runtime_architecture.py`、`scripts/test_check_runtime_architecture.py`
- `scripts/codetools/main.go`（注册 `event-ownership` 子命令）
- `scripts/lint.sh`

Golden 写入归属：合同内容由编排者核准；04.1/04.3 由编排者或其本单具名委派的唯一写者在 `golden-runtime` 锁下按核准内容单写；其余代理只交建议，不得直接改 Golden。

零改动：`golden/runtime/scenarios.json` 的 S01—S15、产品源码、宿主模块、主规格、CI workflow（05 域负责）。

## D8 风险与回退

| 风险 | 处置 |
|---|---|
| 新合同把旧 15 场景规则挤掉 | v1/v2 分版；旧测试逐字保留；旧负控继续跑 |
| 只冻结十个 EI 场景漏掉 00/01/02 负锚 | 合同登记全部产品测试、137 个绿子场景 + 4 个诊断锚；EI 仅 `aggregate_only` |
| 审查删除登记项 | 合同/graph/源码/日志交叉对齐，计数由 graph 导出；仅删合同即红，双删 diff 独立审查拒收 |
| 类型信息不完整导致误判 | 无法解析一律 `uncovered` 并暴露，不算覆盖；冻结 owner 未解析直接失败 |
| 变异被编译错误/超时“假杀死” | 必须命中 `expect_assertion`；编译失败/超时/零命中/无断言 panic 判验证失败 |
| 未证实变异凑数 | 五类候选须 00.3/复现证实；00.3 新增证实缺陷必须登记；计划回写③的三个后续反例只登记不启用 |
| 隔离树复制成本高 | 只复制被测模块与必要文件；控制树先跑；临时目录清理由工具负责 |
| 映射门过严造成改名即红 | 这正是目的；改名需同步合同/graph/测试，不接受“先绿后补” |
| lint 在无 graph 时误报 | 明确打印 skip 并只跑合同/自测；不把 skip 当产品通过 |

回退：本域只改检查资产与 Golden；回退即还原文件，产品行为不受影响。
