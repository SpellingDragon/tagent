# C单元任务：能力契约—验证体系

只执行本页13.101–13.106；旧R账在[历史快照](../../history/7b0cde16-before-replan/changes/13-verified-completion/tasks.md)。本页全部未执行，写面是候选，实施还要design C7和顶层D4完整签发。共同授权/并发/证据纪律只在[ORCHESTRATION](../../ORCHESTRATION.md)。命令从仓库根执行；独立wechat模块用`go test -C examples/wechat-bot -tags smoke . ...`。

新Python测试及标“拟新增”的Go测试均未建立未执行；现有Go锚本轮仅核过定义。每个Python `-k`选择器预期恰好1个顶层用例，子场景全部非空执行；选择器、文件或消费者变化先重签，不以零匹配或早退计通过。

## 13.101 目标、承接与候选核定

- [x] 13.101 核定原症状、全部旧要求、公开模式与当前候选。（2026-11-21 完成：C8 记录 407 行，151 项逐域等值、五类到期阶段与六类标记闭合、11 处漏项入册；11 项待裁阻塞移交对应承接叶；编排者已独立复核分域计数与写面范围。）
  - 阶段：只读取证/计划记录；前置：无。本轮规划结束后首个可派叶，不执行产品修改。
  - 产出：把顶层D1和CARRYOVER的151旧编号连同正文子要求映射为逐项行为身份；列公开模式、部署入口、配置/引擎/安全上下文、资源条件和已有未验面；标“目标/代码反例/实测/建议/驳回/历史”。固定源码及环境身份、合成数据根、读写范围、源文件完整性，不采信dataless读零或HEAD生成索引的空暂存。
  - 到期阶段：逐项区分实现/单元验证、13.106独立审、F2完整矩阵、F3最终审、F4外部实效/交付。后置项提前具名、列拥有者与验收，不要求它先完成自己的前置；已到期缺陷不能改标后置。
  - 读取：CARRYOVER、原proposal/design及快照，主规格与PR基线语义diff；不改其它变更或冻结archive。记录写面：本域design/tasks与CARRYOVER（由编排者集成）。
  - 正锚：逐来源比对所有旧编号和Scenario/未验声明，完整映射无孤儿；只读`git diff <实际PR基线> <候选> --name-status`与`git status --short`的原始结果留档，正常Git不可用时只用经验证仓外对象视图，不修用户元数据。
  - 负锚：在临时表移除一个旧编号、把“可发现”换成“已知key可读”、把未验模式写为通过，独立审逐项指出；这是人工规划覆盖反证，不冒称已有自动门。
  - 消费者锚：A/B设计输入逐项引用本表同一行为ID；04/no-prefix交接以真实共同前置解环，config-surface保持已授权耦合。
  - 停止：源内容不完整/目标取舍未明确时保持相应项未证，不静默移出范围；提交方案而非让用户选择必经步骤。

## 13.102 能力与验证设计门

- [x] 13.102 比较公开装配与最小证据体系，并签发逐叶实施合同。（2026-11-21 完成：C2/C3/C5/C7 落稿；隔离反例证实 H12 两缺口；独立设计审 5 阻断＋复审 3 残余共 8 处修正全部落盘并 grep 清零——lint.sh 归档接线入 13.103 写面、三锚去向拆分、DoD 空判词如实化、depends_on 双向相等、check_concept_budget 回归、两处措辞与指针同步。C7 自本勾选起生效。）
  - 阶段：设计/隔离反例；前置：13.101。
  - 产出：design C2/C3每个候选的角色、成本、删除/合并项、兼容影响及否决证据；与A/B会合提交位置、历史寻址和失败分类；填写C7，不预选新通用框架或新库。
  - 候选取证文件：`scripts/gates/test_check_runtime_evidence.py`、`scripts/gates/test_check_dod_coverage.py`、`scripts/gates/test_check_runtime_plan.py`、`scripts/scenarios/test_run_runtime_scenarios.py`、`assembly/plugin_catalog_test.go`；先在隔离副本加最小反证，产品实现不动。记录本域design/tasks。
  - 正锚（拟新增）：`python3 scripts/gates/test_check_runtime_evidence.py -v -k test_raw_child_events_determine_subtest_verdict`；合法原始run/pass被接受且runner与checker解释一致（DoD 对 run evidence 返回空判词，见 C2 既有行为证据）。
  - 负锚（拟新增）：`python3 scripts/gates/test_check_runtime_evidence.py -v -k test_parent_pass_cannot_invent_a_subtest_pass`；父日志有效而子日志缺失、自报pass，经正式checker CLI拒绝。`python3 scripts/gates/test_check_runtime_evidence.py -v -k test_same_log_has_one_verdict_across_consumers`钉同一日志在 runner 与 checker 的解释一致；DoD 对 run evidence 现返回空判词（`run_verdicts` `verdicts={}`，既有行为证据记于 C2，不新增第 4 例）。
  - 消费者锚（既有＋待扩）：`go test ./assembly -run '^(TestBuiltinCatalogMountsEveryFeature|TestHostPluginsMountThroughTheCatalog|TestPluginDeclarationDisablesABuiltinPlugin|TestPluginDeclarationRefusesAnUnregisteredName)$' -count=1 -v`（4顶层）；以真实公开入口探索run/host级禁用覆盖，未支持先记事实，不让手工holder注入替代。
  - 准入：独立设计审认可需求保全和全局更简单；签发13.103/104/105所需精确文件、字段/配置、全部消费者和负控。方法上限只作为待解释规则，不是拆接口理由。
  - 停止：缺失行为是应实现还是应显式不支持无法从目标裁定时，列真实取舍；不能因旧实现不存在删目标。
  - **执行记录（2026-11-21，本框不勾）**：设计门＋隔离反证已落，等独立设计审。① C2 八行逐疑点补"本轮反证结果"列：三例实跑（`test_raw_child_events_determine_subtest_verdict` 原始 exit **0**；`test_parent_pass_cannot_invent_a_subtest_pass` **1**＝被接受，缺口成立；`test_same_log_has_one_verdict_across_consumers` **1**＝runner 判 `missing` 而 checker 判 pass），另跑既有 `test_consistent_stale_bundle_is_rejected`（exit **0**＝拒绝成立）与 4 顶层 Go 消费者锚（`--- PASS`×4，exit **0**）；静态实证项逐格标"未另跑"。② C3 补同尺成本（新增/删除/迁移/新例/格式面）与选型结论：选方案 B＋保留 scope 限制，否决格式迁移与运行级声明字段。③ C5 逐个复核 34 个候选文件存在与行数，无 MISS；新增 `task-graph.json` 一项。④ C7 七字段全部由"未填"改为草签值，含逐叶写面/全部消费者表/三锚全名与命令/负变异落点/相邻回归/回滚触发/接口变动 10 项/C8.2.3 十一项处置去向（子项 31 个）/`check_runtime_plan.py` 选"适配"及理由。⑤ 隔离副本 `/tmp/c13102/`（`test_check_runtime_evidence.py` 拷贝＋隔离 shim＋三例），仓库产品/测试/CI/主规格零修改。详见[design C2/C3/C5/C7](design.md)。

## 13.103 证据解释与实际拒绝入口

- [x] 13.103 实施选定的原始结果解释和正式派发/归档拒绝路径。（2026-11-21 完成，分两单执行：判词统一＋第三 profile＋task-graph＋CI/归档接线＋preflight 腐化负例；8 枚负锚全部可咬（变异红/控制绿）；真入口 lint 契约段经候选副本 exit 0；共享树红仅外来 symptoms 一例，集成差异已记 C7.3；`--evidence` 分支偏离经编排者追认。编排者复跑 plan 门 exit 0、18 叶/33 边/签态一致。git 环境致 3+1 例红已归因健康副本复验，1 例记未验待健康检出。）
  - **执行记录（2026-11-21，本框不勾，待编排者核验）**：C7.1 十一个文件里十项实施、两项零改动（`run_runtime_scenarios.py`、`archive-preflight.sh`）＋五项「不改」仍零改动；新负锚 `test_preflight_rejects_corrupt_generated_evidence` 在树并已咬（变异红/控制绿）；第三 profile 十八叶真图判绿（`errors 0`、`edges 33`、`signatures 6/12`）；依赖双向相等在真实三单元 tasks.md 与图上两方向均被检出；`gates/lint.sh` 归档段半成品空转缺陷（检测面与判据面对象错位＋未跟踪路径不可见）已修并四态自测。**环境红如实另列**：本树 `.git/HEAD` 不可读致 3 例 `EndToEndTest` 与 3 例变异自测红，仓外只读 revision 复验后前者 41/41 绿。逐项命令 exit／计数、负锚恰 1 命中、旧两 profile 判词不变三证、stale 与偏离自决详见 [design C9](design.md#c9--13103-执行记录)。
  - 阶段：实施；前置：13.102且C7签发完整；未签时不可派。
  - 精确候选文件：`scripts/scenarios/{run_runtime_scenarios,test_run_runtime_scenarios}.py`；`scripts/gates/{check_runtime_evidence,test_check_runtime_evidence,check_dod_coverage,test_check_dod_coverage,check_event_integrity_mutations,test_check_event_integrity_mutations,runtime_checklib,check_runtime_plan,test_check_runtime_plan}.py`；`scripts/{lint,preflight}.sh`、`scripts/gates/{lint,preflight,archive-preflight}.sh`、`.github/workflows/ci.yml`。选择实际最小写面；新增适配器先签发。
  - 产出：原始日志/argv/身份/选择集统一解释；拒子pass伪报/手填killed/红控制/skip/超时/零跑/缺片/错树；现有plan门适配18叶、依赖和签发，未接线不冒已拒派；本地正式入口和CI配置有真实消费者，服务端实效留F4核验。
  - 正锚：`python3 scripts/scenarios/test_run_runtime_scenarios.py -v`、`python3 scripts/gates/test_check_runtime_evidence.py -v`、`python3 scripts/gates/test_check_event_integrity_mutations.py -v`、`python3 scripts/gates/test_check_dod_coverage.py -v`、`python3 scripts/gates/test_check_runtime_plan.py -v`；全文件选择，逐例实际执行与原始exit留档，不冻结旧总例数。
  - 负锚（拟新增）：`python3 scripts/gates/test_check_runtime_evidence.py -v -k test_subtest_without_run_is_refused`；`python3 scripts/gates/test_check_runtime_plan.py -v -k test_unapproved_leaf_is_not_dispatchable`；`python3 scripts/gates/test_check_runtime_plan.py -v -k test_unknown_or_missing_active_leaf_is_refused`；`python3 scripts/gates/test_check_runtime_plan.py -v -k test_missing_design_signature_blocks_implementation`。
  - 正式入口负锚（拟新增）：`python3 scripts/gates/test_check_dod_coverage.py -v -k test_archive_tree_change_without_evidence_is_refused`；`python3 scripts/scenarios/test_run_runtime_scenarios.py -v -k test_preflight_rejects_corrupt_generated_evidence`。前者经实际归档检查，后者在隔离副本保留真实preflight控制流，仅在runner成功生成后、checker读取前篡改目标产物，确认正式消费者非零且点名该产物；不改消费者/预期结果、不加产品测试hook。具体生成器落点及隔离文件先在C7签发。
  - 消费者命令：`env TAGENT_RUNTIME_EVIDENCE_DIR="$BUNDLE" bash scripts/lint.sh`、`bash scripts/gates/archive-preflight.sh --evidence "$BUNDLE"`。`BUNDLE`由本轮绝对产物目录绑定；合法控制与子日志缺终局、伪判词、失败控制、缺分片、错树、未签叶六类负例分别记录其实际消费者，不宣称每个入口都读取全部六类。
  - preflight命令：`env TAGENT_RUNTIME_EVIDENCE_DIR="$OUT" bash scripts/preflight.sh`；`OUT`是本轮新目录，不能把预灌非法目录随后被生产步骤覆盖当成负控。移除隔离副本的真实消费调用后，上述对应负锚应因未拒绝而失败。完整生产矩阵仍归F2，隔离边界测试不冒称全链运行。
  - 停止：复制另一套解释器、破坏既有拒绝、入口负例只到helper或需未授权外部修改时回设计门。CI配置审查不等于服务端保护生效。

## 13.104 公开装配与行为承诺

- [x] 13.104 实施已选插件选择范围，并保全压缩、发现、召回及安全能力。（2026-11-21 完成于两单：`withSettleFactCommit` 生产接线（plugin_catalog.go:163）＋H05 取证改经真实 catalog 装配（10/10 -race）；四新锚全绿（插件选择跨重启/极小预算含呈现/隔离可达面/早期事实可发现——禁手工给 key）；五组负变异全咬；`check_concept_budget` 未抬；两压缩窄口合并与用户/环境接口如实记未验（前者越本叶写面、后者接口不可达不伪造）。）
  - **执行记录（2026-11-21，本框不勾，待编排者核验）**：C7.1 的 13.104 行九项里六项实施、一项零改动（`plugin_catalog.go` 的 scope 拒绝文案已点名故不加分支）、两项未动（见下）；12.104 D9.8.6-③ 移交的两件事已闭（`plugin_catalog.go:164` 补 `withSettleFactCommit`、D4.2:143 接线点笔误订正）。三锚 20/20、3×10 `-race` 30/30、发现锚 10/10、消费者锚 40/40 与 10/10、H05 两枚 `-race` 20/20；负变异三案（接线回退／呈现开销不计／发现线索换成键）各红、控制绿；`go build ./...`、`check_concept_budget.py`（`new: 57` 未抬）exit 0。**未兑现面**：C7.1/C7.2 的"两压缩窄口合并为 4 方法内聚口"未执行——其级联需 `assembly/skeleton_assert.go:141-144` 与另 5 个文件，超出本叶写面且按 12 域 D4.2 末段不得"以编译要求事后追认"，留待补签写面；用户/环境两面接口不可达，锚内只核目录与资源身份。逐项落点、原始 exit/计数、变异证据与自决条目详见 [design C10](design.md#c10--13104-执行记录)。
  - **补跑记录（2026-11-21 补跑会话；本框仍不勾，待编排者核验）**：H05 探针 `TestBackflowFailureRecoversWithoutManualSettle` 由测试自接线改为**经 `hostPlugins` 目录装配**取泵——`seam.settle`，driver／fire／commit 三面全来自生产目录项（`fireNoticeTurn`、`withSettleFactCommit(seam.run.commitFactChecked)`）、scope 由装配声明解出、零手调；`-race -count=10` → exit 0、RUN 10／PASS 10／`DATA RACE` 0，且每次 3 次模型调用（通知回合真实发生）。删 `plugin_catalog.go:163` 的提交面后同锚 exit 1（`host_test.go:915` 点名等待超时）、同变异下目录锚亦 exit 1；控制绿同一次交付内取。四锚与相邻回归全部复跑：正锚 20/20 与 10/10；负锚 3×10 `-race` 30/30（skip 0）；发现锚 `-race` 10/10；消费者锚 40/40 与 10/10；`go build ./...` exit 0（零输出）；`go test ./assembly -count=1` exit 0（`ok … 348.061s`，包内 `--- FAIL` 0）；`check_concept_budget.py` exit 0（`new: 57` 未抬）。负变异四案（提交面回退／scope 静默忽略／呈现开销不计／发现线索只留键）各 exit 1 且控制绿。D4.2:143 订正已在位（行号仍 143，未再改该文件）。未兑现面与上条同：两压缩窄口合并仍待补签写面；用户/环境两面仍不可达。详见 [design C10](design.md#c10--13104-执行记录)。
  - 阶段：实施；前置：13.102、12.104、06.104；C7精确签发。
  - 精确候选文件：`assembly/{plugin_catalog,compress_mount,prepare,host_document,run,summary_gist,compress_consumer}.go`及对应既有测试，`assembly/host_tools_test.go`；`api/config/{config,decode,defaults,fingerprint}.go`及镜像测试；`tests/integration/{acceptance/scenario_s11_test.go,host/projection_assembly_test.go,history/symptom_anchor_midband_test.go}`。安全/呈现需其它生产文件时先按真实消费者补签，不能扩成目录授权。补跑会话另经编排者授权写 `assembly/host_test.go`（仅 H05 取证所需最小调整，见 design C10.3/C10.9-6）。
  - 产出：默认可用与可选择分离，声明到运行/恢复的支持范围符合13.102；预算/缺省/极小值/租约采样/降级原样传递；模型能发现应保留历史线索及按scope读取；执行安全上下文跨重启承接。双参/内联正文按设计证据决定，不按词或条数决定。
  - 正锚（既有）：`go test ./assembly -run '^(TestBuiltinCatalogMountsEveryFeature|TestHostBootCompressionObeysTheDeclaredSummaryBudget)$' -count=10 -v`（2×10）；`go test ./tests/integration/acceptance -run '^TestScenario11ConfigDrivenSubagentAndPipeline$' -count=10 -v`（1×10）。
  - 负锚（拟新增）：`TestDeclaredPluginChoiceSurvivesRestart`落`assembly/plugin_catalog_test.go`，核不支持scope拒绝、受支持选择真实生效；`TestTinySummaryBudgetIncludesPresentationOverhead`落`assembly/summary_gist_test.go`；`TestExecutionIsolationSurvivesSessionRestart`落`assembly/host_tools_test.go`，经真实工具核执行用户/环境/目录。
    `go test ./assembly -run '^(TestDeclaredPluginChoiceSurvivesRestart|TestTinySummaryBudgetIncludesPresentationOverhead|TestExecutionIsolationSurvivesSessionRestart)$' -count=10 -race -v`（3×10，各子场景非零；缺隔离资源记未验）。
  - 发现负锚（拟新增）：`TestEarlyFactRemainsDiscoverableAfterRepeatedCompression`落`tests/integration/history/symptom_anchor_midband_test.go`；`go test ./tests/integration/history -run '^TestEarlyFactRemainsDiscoverableAfterRepeatedCompression$' -count=10 -v`（1×10）。核实际模型请求的历史线索和后续召回，不能改成测试手工给key。
  - 消费者锚（既有）：`go test ./tests/integration/host -run '^TestProjectionAssemblyServesTheThreeUses$' -count=10 -v`、`go test ./tests/integration/history -run '^TestSymptomAnchorMidbandProcessRecalledByTicket$' -count=10 -v`（各1×10）；真实安全执行资源不可用时不以替身冒充。
  - 停止：发现要求必须缩范围、新外部资源或no-prefix的未授权写面，先记录真实依赖并裁范围，不能删除要求或等双方归档解环。

## 13.105 旧项、规格和候选收口

- [x] 13.105 核销到期旧项并形成唯一规范合并与最终验证输入。（2026-11-22 勾：151 项对账＋合并清单 58 条冻结＋F2 清单＋六门绿＋同步三例；06.106 终判 35 行经 CARRYOVER §四.1 回填；R6→F1 前置、R7 同批删待补签、R13 待 13.106 复核。）
  - 阶段：对账/受控文档与合并入口实施；前置：12.105、06.105、13.103、13.104。（13.106 审①：勾选编辑时前置行脱失致 plan 门红，2026-11-22 编排者复原文行。）
  - **执行记录（2026-11-22，本框不勾，待 06.106 判决落 CARRYOVER 后由编排者核）**：见 [design C11](design.md#c11--13105-收口记录)。①CARRYOVER §四 落 151 项逐行结论（兑现 100／部分兑现 4／未完成 4／待 06.106 判决 35／待 13.106 3／待 F2 3／待 F4 2），C8.2.3 十一项与 C7.9 的 31 个子项逐项对账（§四.14/§四.15），残余 R1–R13 具名（§四.16）。②唯一规范合并输入冻结：58 条 delta requirement 判定＝并入 43（新建能力 23／新增要求 14／delta⊇主 4／逐字无操作 2）、拒 15（冲突 10／主更新更严 2／缺源 3）；冲突项的 delta 独有活义务逐条列出（含「有效工作」「全局任务账户」「无 tier」等主 spec 全库 0 命中的义务），不改主 spec、不并最后一份。③F4 归档程序在隔离副本实跑（9 个合成 change，`bounded-recovery` create +16、`orchestration` +8 等；未进入合并输入的四能力逐字节不变；拒收三件各自具名拒绝且主 spec 不变）。④新测试 `scripts/gates/test_sync_runtime_specs.py` 三例各恰 1 命中、全文件 `Ran 3 OK`；变异（标题碰撞、permissive 工具）逐例转红、控制绿另报。⑤六门原始 exit：`openspec validate --strict` 0、`check-openspec` 0（158 passed/2 in-flight）、`check_spec_hygiene` 0、`check_doc_links` 0、`check_doc_budget` 0、`gen_godoc --check` **1→0**（`docs/api` 7 页重生成，76 页与 HEAD 逐字节同）。旁证：dead_producers 0、concept_budget 0、`check_dod_coverage` **1**（P0–P8 判据源在重整后丢失，非判据不满足）。⑥写面＝CARRYOVER、本域 design/tasks、新测试、`docs/api` 7 页；产品 Go/测试/CI/主 spec/delta/golden/OPERATION/wiki/archive 零改动；未 git 写。**未完成面**：03.R5、11.R3 部署后果、12.R3①③、06 域缺锚、09 域 4.4、P0–P8 判据源、`IndexEntry` 死类型＋binding、04.R2、三观测轴、boot/total、F2 两入口、OPERATION 的 archive-preflight 说明、远端 CI/保护。
  - **R6/R7 前置修复（2026-11-22，本框不勾；13.105 记录的两条新残项）**：判据源回填后 `check_dod_coverage.py` 已过判据与锚两层（余 9 条为未带 `--mutation-evidence` 的证据面缺口，待 F2），门自测 54/54；`IndexEntry` 死类型＋golden binding 同批删除、`docs/api` 重生成。门读数与写面见 [design C11.10](design.md)。
  - 产出：CARRYOVER中到本阶段的实现/单元验证逐项有实际结论；删除、保留、迁锚、接口满足、配置耦合与不同义项逐条实证。13.106审验及F2–F4事项明确待其阶段核销，逐项列承接者、触发和命令，不要求后置审验先完成，不把未验计兑现。
  - 产出：主规范/文档/golden不为迎合实现删行为；形成完整F2命令/模式/证据保存清单，全部源码含注释定稿。整体交付仍未完成。
  - 候选写面：CARRYOVER、本域design/tasks；精确涉及的主spec/delta、`golden/runtime/{architecture,durable_truth,dod_predicates}.json`、`docs/OPERATION.md`、已查明的wiki与docs/api生成物；拟新增`scripts/gates/test_sync_runtime_specs.py`（C7签发后创建）。执行前枚举文件，不用全spec/docs授权，不改冻结archive，不提高baseline换绿。
  - 正锚：`openspec validate runtime-recovery-ideal --strict`、`bash scripts/gates/check-openspec.sh`、`python3 scripts/gates/check_spec_hygiene.py`、`python3 scripts/gates/check_doc_links.py`、`python3 scripts/gates/check_doc_budget.py`、`bash scripts/gates/gen_godoc.sh --check`；取原始exit和实际扫描集，结构绿不替语义审。
  - 合并正/负锚（拟新增）：`python3 scripts/gates/test_sync_runtime_specs.py -v -k test_compatible_requirement_union_is_synced`、`python3 scripts/gates/test_sync_runtime_specs.py -v -k test_conflicting_requirement_text_is_refused`、`python3 scripts/gates/test_sync_runtime_specs.py -v -k test_missing_rename_source_is_refused`。一次性合成change经C7选定的真实同步工具运行，合法并集成功，冲突/缺源拒绝。C7冻结工具版本及实际argv；工具若需配套实现，先签精确文件，不能用只验证自测数据的helper替代同步。
  - 消费者锚：隔离副本按F4预定OpenSpec命令同步，核主spec行为承接与归档输入；不在主树提前archive。外部CLI不能保证的冲突在唯一合并输入生成前拒绝，不最后写覆盖；最终清单交13.106审。
  - 停止：到期旧项只有转交无结果、静态删除冒充功能裁撤、标题冲突或真实入口未核，保持未完成；不把发现的新缺陷移成后置审验。

## 13.106 能力与证据独立审

- [x] 13.106 独立审查能力承接、证据解释与全部正式消费者。（2026-11-22 勾：分母全过——151 行对账自洽、判词/归档负锚恰 1、CI 接线在位实效归 F4、F2 清单十项在列、R6/R7/R13/R4 归属确证、P5 词门边界表述正确；4 阻断（13.105 前置行脱失、06.102 签态漏账、C11.3 缺 race/go-list 两行、§四.16 R5/R10 陈标）均编排者级修正落盘，plan 门复跑绿。C frontier 空；A/B 已关——三单元达关闭条件。）
  - 阶段：非实施者只读审；前置：12.106、06.106、13.105。
  - 产出：按design D1/CARRYOVER评估到期结果与F2–F4完整验收清单；核原始证据来源、完整候选hunk分母、驳回项/未查面、本地消费者与CI配置接线。发现回拥有叶，修后独立复查。
  - 正锚：第三方用`python3 scripts/gates/test_check_runtime_evidence.py -v`、`python3 scripts/gates/test_check_dod_coverage.py -v`复核完整自测，并从持久原始材料复算合法结果。按13.103已签发的隔离入口控制运行`env TAGENT_RUNTIME_EVIDENCE_DIR="$BUNDLE" bash scripts/lint.sh`及`bash scripts/gates/archive-preflight.sh --evidence "$BUNDLE"`；这里的合成控制只证拒绝规则，不冒充产品矩阵，也不要求F2最终bundle先产出。
  - 负锚：独立隔离制品移除子测试终局、分片、来源绑定后，经`python3 scripts/gates/check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --bundle "$BUNDLE" --repo-root . --json`具名拒绝；去掉一条旧行为承接由逐项独立审指出，不冒称静态门证明全部语义。
  - 完成：本域到期实现/验证/独立审的缺陷为零，F2完整矩阵、F3最终审、F4实际CI/保护/交付仍逐项未核销。未接本地入口或CI配置无人消费阻断本叶；服务端实际运行/保护在F4按授权核验，此前不宣称已拦截。后置项不成为自己的前置，整体完成仍等F4全部目标核销。
