# C单元设计：能力契约与验证体系

## 当前判定

本文件是待执行设计门的输入，不是实现选择。候选7b0cde16与旧r333运行只作复核起点；当前没有已签发的新实现。旧文件原件见[历史快照](../../history/7b0cde16-before-replan/changes/13-verified-completion/design.md)。

## C1 需要分开的四类事实

| 类别 | 来源 | 不能推出 |
|---|---|---|
| 用户能力要求 | 用户明确目标、协议、仍有效主规格及其授权变更 | 当前实现一定提供它 |
| 运行事实 | 原始日志、真实argv/环境/输入、实际进程终局 | 设计最优或所有模式都通过 |
| 检查器结论 | 对指定性质/选择集的确定性解释 | 未读取的行为、整体DoD或独立审完成 |
| 发布状态 | 已提交候选、CI实跑、保护配置、归档和远端可见结果 | 本地wrapper绿即服务端拒绝已生效 |

13.101核全部旧编号151项和正文子要求，先纠正“只剩发布”叙述。发现能力与按已知key读取分别保留；安全上下文不能随旧CommandTool类删除而退役。config-surface维持已授权耦合；no-prefix只列本候选真正依赖的共同接缝和验收，禁止循环等待双方归档。

## C2 待证断言与最小反例

| 疑点 | 反证/对照 | 处置规则 | 本轮反证结果（命令／原始 exit／落点） |
|---|---|---|---|
| 父run/pass＋自报子pass足以证明全部锚 | 保持父日志有效，移除一个子锚实际run/pass，报告仍填pass；正式CLI应拒 | 先复现；统一原始日志解释，不增加第二套判词 | **缺口成立（三例实跑）**。`/tmp/c13102` 副本调用仓库现装 runner/checker（只读），逐例原始 exit：① 合法日志 `-k test_raw_child_events_determine_subtest_verdict` → **exit 0**（`event_anchors_checked`=135＝日志里 135 条子锚 `pass`；runner `attest_event_test` 全 `attested`；`--bundle` 10短/6长/1变异 → **exit 0**）。② 删 `00.2/TestEventIntegrityReproduction` 子锚 `control_normal_path` 的 run/pass、证据仍自报 `pass` → `-k test_parent_pass_cannot_invent_a_subtest_pass` → **exit 1**：`AssertionError: 0 == 0 : the checker accepted the self-reported pass of anchor (00.2/TestEventIntegrityReproduction,control_normal_path)`。③ 同一日志两名读者 → `-k test_same_log_has_one_verdict_across_consumers` → **exit 1**：`one log, two verdicts: the runner derives 'missing' … (state 'unattested': mandatory anchor(s) ['control_normal_path'] did not pass) while the checker accepted the self-reported pass (exit 0)`。落点：`check_runtime_evidence.py:2661-2701` 只读 `evidence.subtests` 自报，`index_event_log:1812`/`check_event_test_selector:1936` 只核父用例。**第三读者 DoD 不消费本面**：`check_dod_coverage.py:58` 的 `MUTATION_PROFILE` 与 `run_verdicts:668` 只收 `mutations` 报告，同一用例内实测对 run evidence 返回 `verdicts={}` 且 `refused` 非空 ⇒ 不能再把 DoD 写成"解释一致"的一方。 |
| P5词门killed证明死语义被禁止 | 去掉代码文件名而保留同样旧扫描语义，对比门结果 | 词门如实限于文风，行为由需求/源码审与行为锚承担 | **词门＝文本形态（静态实证，未另跑）**：`check_spec_hygiene.py:41-48` 的 `RULES` 仅 `go-file`／`function-call`／`numeric-constant` 三条正则，`scan_specs:78-107` 只扫 requirement 正文行；`golden/runtime/dod_predicates.json` P5 的 `measure=legacyRequirementText`，其变异 `MUT-DOD-P5-legacy-clause`（`check_event_integrity_mutations.py:1130-1150`）注入的正文句点名 `.go` 文件。把同一句改写为不含文件名/调用形/数字的旧扫描语义 → 三条规则全不命中。⇒ 被杀的变异只证"文本形态被拒"，不证"旧机制不能复活"。 |
| 两压缩口同对象双参是必要隔离 | 枚举独立消费者/替换/变化；比较单一内聚口、已有上下文传递、保留双口 | 不按方法上限选方案，不先改基线 | **无独立第二实现（静态实证）**：生产唯一实现 `ageFoldSessions`（`assembly/age_fold.go:221` 断言 `_ SessionFolds`），`assembly/compress_mount.go:23-24` 以 `NewCompressionConsumer(face, face)` 同一对象填 `compressSessionSource`（`compress_consumer.go:80-86`，2 方法）与 `compressProductStore`（`:91-105`，2 方法）；第二"使用者"只有测试装饰 `countingCompressSessions`（`assembly/compress_checkpoint_test.go:585` 内嵌 `compressProductStore`）与 `NewCompressionConsumer(nil,nil)`（`:293/:361/:463`）。测试不计入衡量 ⇒ 双口属自证。 |
| builtin目录转发等于全部可定制 | 用公开声明分别禁用/覆盖agent、run、host级builtin，核实际配置/调用方 | 明确范围后选设计；测试手工holder不能补生产因果 | **事实：不支持（静态读装配链＋消费者锚实跑）**。目录 7 项与 scope：`compress-consumer`(run)、`final-round`(run)、`reflection-driver`(host)、`settle-driver`(host)、`lease-observer`/`age-fold`/`telemetry-observer`(agent)（`plugin_catalog.go:110-206`）。唯一公开声明面是 **agent 级** `hostAgent.Plugins`（`host_document.go:103-106`→`:213`），由 `declaredPluginsOf:275-314` 消费；点名 run/host 级名**具名拒绝**（`:302-306`＋`carriedBy:318-326`）；`mountBuiltinPlugins:333-356` 与 `hostPlugins:361-374` 无条件挂载全部同类条目，无禁用入口。消费者锚实跑：`go test ./assembly -run '^(TestBuiltinCatalogMountsEveryFeature\|TestHostPluginsMountThroughTheCatalog\|TestPluginDeclarationDisablesABuiltinPlugin\|TestPluginDeclarationRefusesAnUnregisteredName)$' -count=1 -v` → `--- PASS` ×4、`ok github.com/SpellingDragon/tagent/assembly 0.373s`、原始 exit **0**。 |
| 无tier只是测试遗产可删 | 枚举公开装配入口、实际调用、测试以外的支持承诺 | 没有消费者不是充分删需求证据，模式取舍要独立说明 | **承诺有硬条款、锚不存在（静态实证）**：`changes/12-consumer-state-lifecycle/specs/session-checkpoint-material/spec.md` 的 Requirement 正文含"无 tier 模式 SHALL 保留既有内存行为，不暗增耐久承诺"；而 `TestRecoveryEntriesPreserveStateAndObligations`、`TestNoTierCommitPreservesApplicationOrder` 全仓 Go 树 grep **零命中**（与 C8.2.3-9 一致）。⇒ 取舍不能以"无消费者"删承诺，也不能把缺锚记成已验。 |
| wrapper --commit保证归档不可绕 | 用隔离候选模拟直接移动archive/直接archive，核日常检查是否真的被调用 | 没有日常消费者则未生效，不能凭命令存在放行 | **事实：未生效（静态实证）**：`scripts/gates/archive-preflight.sh:26-29` 自述"`openspec archive -y` or a plain `mv` writes the same tree, and no local script can stop a command that never calls it … until such a caller is configured nothing here is a hard block"；全仓 `archive-preflight` 的非文档命中只有脚本自身（0 个 CI／hook／门调用者，`.github/workflows/*.yml` 零命中）。⇒ 命令存在不等于拒绝面存在。 |
| 子域delta无CLI自动消费所以无需冲突负例 | 核CLI版本/根目录规则，显式合并输入，在隔离fixture做冲突/缺源/合法合并 | 不取消安全性质；也不建立无人消费的影子delta | **命令实测**：`openspec list --json` 只列顶层 change（`runtime-recovery-ideal` 记 `completedTasks 0/totalTasks 7`），不含任何 `NN-domain`；`openspec validate 13-verified-completion --json` → **exit 1**、`Unknown item '13-verified-completion'`；`openspec/changes/runtime-recovery-ideal/task-graph.json` **不存在**（`ls` No such file）。⇒ 二级 delta 的冲突只能在显式合并输入上验，且本变更当前无 graph 供规划门判。 |
| 代码后改注释仍可沿用旧bundle | 核指纹覆盖字节与时间顺序，改变一个被覆盖字节而不改报告 | 旧报告应拒；不补digest伪装新运行 | **既有拒绝面实跑（exit 0＝被拒成立）**：隔离副本 `-k test_consistent_stale_bundle_is_rejected` → **exit 0**；用例在十短六长一变异内部完全自洽后改 `tree/main.go` 一个字节，重跑 `--bundle` 得非零且输出 `does not match this tree's`／`describes another source tree`。时间顺序面由 `archive-preflight.sh` 的 `--expect-rev` 与 `evidence git_head`（`:113`）承担，其接线缺口见上一行。⇒ 字节覆盖成立；不覆盖的"注释/顺序"面由 `--expect-rev` 补，不补 digest。 |

本轮三例与既有 stale-bundle 例的**原始 exit 全部留在 C7.6 的凭据表**（隔离目录 `/tmp/c13102/`）。副本调用的是仓库现装 `run_runtime_scenarios.py`／`check_runtime_evidence.py`（只读），故 exit 反映**当前产品行为**；三例本身尚未并入仓库自测，**副本上的红不表示产品已具备该拒绝面**，绿也不表示需求已兑现——它只精确指出缺口位置。静态实证项逐格标明"未另跑"，不以读码冒充运行。

## C3 候选设计比较

**同尺定义（先冻结，再比较）**：`新增公共符号`＝新增导出类型/函数/字段/CLI 参数各计 1；`删除公共符号`同；`迁移消费者`＝必须同批改动的生产调用点或门/测试入口各计 1；`新建验证用例`＝本轮三例或等价新增例各计 1；`格式迁移面`＝必须同时改的产物流水线节点数。四栏都用这一把尺，不按"某步读几次"或用例条数挑偏好。

| 方案 | 能保留什么 | 能删除什么/需新增什么 | 同尺成本（新增/删除/迁移/新例/格式面） | 同尺结论与否决依据 |
|---|---|---|---|---|
| 保持现有分层、严格限定结论 | 既有 runner/checker 格式与 CLI | 不删解释器，补未消费字段；风险是同义解释继续漂移 | 0／0／0／3／0 | **不选**：它原样保留本轮实测的分歧（C2 第 1 行 ③ exit 1 的分岔点），"同义解释继续漂移"已由证据证实，不是风险假设 |
| 复用一个原始结果派生器 | 现有日志、源码绑定、矩阵、控制组及正式入口 | 让 runner 与 checker 调同一派生规则（`run_runtime_scenarios.event_subtest_states:1219-1242` 已是唯一派生点），删除 checker 侧"自报即可"的判词；格式不变 | 0／0／2／3／0 | **选用**：新增公共符号为 0（checker 已通过 `_event_runner():2012` 加载 runner，依赖单行、无环）；删除 1 条自报判词路径；迁移 2＝`check_event_evidence_subtests:2661` 一处判据＋其自测夹具一处。否决条件未触发：不引入循环依赖（runner 不 import checker）、不新增状态权威、各入口原有拒绝语义逐条保留 |
| 把结果清单简化为执行事实与索引 | 原始argv/日志/退出/候选/合同/工具/选择集 | 删除不可溯源的自报完成字段；格式迁移需要消费者同步 | ≥1 新公共格式／1 删字段／≥6 迁移／3／≥6 | **不选**：本轮缺口是**解释权归属**不是字段形状（C2 第 1 行 ②③ 在既有格式内已复现），改格式只增加迁移面（runner/checker/DoD/archive-preflight/lint/CI 及全部 fixture），并按 C3 原否决条件"只改格式但并行解释仍在"落回同一缺陷 |

能力装配侧同尺比较（三条候选，尺同上）：

| 候选 | 新增/删除/迁移/新例 | 结论 |
|---|---|---|
| 保留明确 scope 限制（`pluginScope` 三值＋`declaredPluginsOf` 具名拒绝） | 0／0／0／2 | **选用**：与实装一致（C2 builtin 行逐项 file:line），且 C8.1.2 的 2.2 只承诺 agent 声明点名/禁用/宿主贡献、run 级内置名具名拒绝 |
| 统一默认目录与调用方选择（新增运行级 `plugins:` 声明面） | 1 新公共字段／0／≥3 迁移（`host_document`／`decode`／`fingerprint`＋主规格 1 条）／1 | **否决**：无独立需求证据（无用户症状、无旧编号要求、主规格无条款），且不得"以测试需求迫使生产新增状态"（ORCHESTRATION 授权节） |
| 复用现有注册契约（`plugin.Instance`＋catalog 单一构建点） | 0／0／0／2 | **选用**：`compress_mount.go` 与 `plugin_catalog.go` 是 `architecture.json` 登记的 registry implementation，删/换即触挂载门 |

A拥有生命周期与义务语义，B拥有产物定位与读取，C只定义公开选择和验证，不在各处复制它们的规则。

## C4 现有工具的边界

`openspec status/instructions apply`只检查一级四件套；旧`check_runtime_plan.py`绑定旧域/编号集合，尚未成为新18叶的派发守卫。13.103先验证能否复用其图/状态/checkbox解析；如需适配，只增加当前需求的最小处理，不建新的规划平台。过渡期间由编排者按签发字段和任务前置逐项核验，不谎称机器已拒派。

`check_dod_coverage`的登记条数、`check_spec_hygiene`的词形、`check_concept_budget`的方法/字段数量只证明各自读取面。保留它们能捕获的真实错误，但不得据其绿去删未实现能力，也不得为了满足数量反增生产复杂度。

## C5 未来实现候选写面

证据面：`scripts/scenarios/run_runtime_scenarios.py`及其自测，`scripts/gates/check_runtime_evidence.py`、`check_dod_coverage.py`、`check_event_integrity_mutations.py`及对应自测，`runtime_checklib.py`、`check_runtime_plan.py`/自测，`lint.sh`、`preflight.sh`、`archive-preflight.sh`、`.github/workflows/ci.yml`。生产声明面：`assembly/plugin_catalog.go`、`compress_mount.go`、`prepare.go`、`host_document.go`、`run.go`、`summary_gist.go`、`compress_consumer.go`，`api/config/{config,decode,defaults,fingerprint}.go`及相应既有测试。这不是当前写授权，13.102逐叶签发后再缩定。

规格/文档面由13.105从CARRYOVER逐条列精确路径，包括已改主spec、受影响delta、golden、docs/api生成物和OPERATION；不能用`openspec/specs/**`通配授权。同步验证拟新增`scripts/gates/test_sync_runtime_specs.py`，具体真实工具及配套实现先在C7签发。旧ScopedReader待删面由B先证验证承接，C仅同步公开承诺与生成物。

### C5.1 候选清单核对（本轮逐个查存在与行数，命令见 C5.3）

| 归属叶 | 文件（行数） | 核对判定与理由 |
|---|---|---|
| 13.103 | `scripts/scenarios/run_runtime_scenarios.py`(1825)、`test_run_runtime_scenarios.py`(929) | 留。派生点 `event_subtest_states:1219-1242`／`attest_event_test:1245-1293` 是唯一锚判词来源，B 方案只让 checker 调用它，不改 runner 自身语义 |
| 13.103 | `scripts/gates/check_runtime_evidence.py`(3283)、`test_check_runtime_evidence.py`(1972) | 留且**是唯一必改解释器**：`check_event_evidence_subtests:2661-2701` 改为按日志反查（新例并入本文件，不新建文件） |
| 13.103 | `check_dod_coverage.py`(933)、`test_check_dod_coverage.py`(1172) | 留，**不改解释面**：只作 DoD 侧"不重复解释本日志"的负锚证据（C2 第 1 行实测 `verdicts={}`） |
| 13.103 | `check_event_integrity_mutations.py`(3086)、`test_check_event_integrity_mutations.py`(1408) | 留，本叶不改：变异判词四字段一致已具备；只作为"手填 killed 不成立"的既有拒绝面被消费者锚引用 |
| 13.103 | `runtime_checklib.py`(298) | 留，不改：`Report`/`load_jsonl_objects`/`sha256_file` 全部复用，禁止另写解析 |
| 13.103 | `check_runtime_plan.py`(1072)、`test_check_runtime_plan.py`(1095) | **适配**（C7.4 给出选型）：加第三个 `domain_profile`，旧两条 profile 逐字保留 |
| 13.103 | `scripts/lint.sh`(10)、`preflight.sh`(10)、`scripts/gates/preflight.sh`(159) | 留，不改。`gates/lint.sh:174-183` 与 `gates/preflight.sh:138-157` 已是真实日常入口；plan 门**因无 graph 而 skip**（`:181-182`），加 graph 后自动生效。`scripts/gates/lint.sh` 的归档接线改动见 C7.1 专行，不属本行 |
| 13.103 | `scripts/gates/archive-preflight.sh`(137) | 留且必须接线：`--commit` 现无调用者（C2 第 6 行），接线点为 CI 作业与本地门（C7.3） |
| 13.103 | `.github/workflows/ci.yml`(488)、`.github/workflows/runtime-contracts.yml`(237)、`scripts/gates/test_check_runtime_workflow.py`(1704) | 留。`runtime-contracts.yml:118-130` 的 active-graph 循环与 lint 同构 ⇒ 新增 graph 即被两处消费；`test_check_runtime_workflow.py:72/133/288` 断言该接线，改 plan 门时须同步 |
| 13.104 | `assembly/plugin_catalog.go`(403)、`plugin_catalog_test.go`(262) | 留。三处拟新增锚落 `plugin_catalog_test.go`；`declaredPluginsOf` 是 agent 级声明唯一判据 |
| 13.104 | `assembly/compress_mount.go`(29)、`compress_consumer.go`(702)、`compress_consumer_test.go`(143)、`compress_checkpoint_test.go`(610)、`age_fold.go`(306) | 留。C2 第 3 行裁"合并单一内聚口"落在这五处（构造点＋两个接口＋两处测试装饰） |
| 13.104 | `assembly/summary_gist.go`(143)、`summary_gist_test.go`(575) | 留。`TestTinySummaryBudgetIncludesPresentationOverhead` 拟新增于此 |
| 13.104 | `assembly/host_document.go`(1739)、`prepare.go`(403)、`run.go`(3976) | 留，**只读参考为主**：装配链证据来源；`host_document.go:103-106,:213` 是声明面，若加字段则触 decode/fingerprint（本设计不加） |
| 13.104 | `assembly/host_tools_test.go`(1228)、`host_test.go`(708) | 留。`TestExecutionIsolationSurvivesSessionRestart` 拟新增于 `host_tools_test.go`；`host_test.go` 持有既有锚 `TestHostBootCompressionObeysTheDeclaredSummaryBudget` |
| 13.104 | `api/config/config.go`(350)、`decode.go`(845)、`defaults.go`(51)、`fingerprint.go`(417)；镜像测试 `config_test.go`(178)、`decode_test.go`(592)、`defaults_test.go`(22)、`fingerprint_test.go`(395)、`strict_test.go`(182) | 留，**本设计不改**：C3 已否决新增运行级声明字段；仅当 13.104 变动 agent 声明语义时才需同步，同步面已按文件列出 |
| 13.104/13.106 | `tests/integration/acceptance/scenario_s11_test.go`(593)、`tests/integration/host/projection_assembly_test.go`(335)、`tests/integration/history/symptom_anchor_midband_test.go`(220) | 留。三枚既有消费者锚所在文件；`TestEarlyFactRemainsDiscoverableAfterRepeatedCompression` 拟新增于 history |
| 13.105 | CARRYOVER、本域 design/tasks、主spec与delta、`golden/runtime/{architecture,durable_truth,dod_predicates}.json`、`docs/OPERATION.md`、wiki/api 生成物；拟新增 `scripts/gates/test_sync_runtime_specs.py` | 留，路径由 13.105 逐条列；本叶不写 |

**C5 之外新增（C7 签发时必须补，不属扩目录授权）**：`openspec/changes/runtime-recovery-ideal/task-graph.json`（13.103 的判定对象，现不存在）、`scripts/gates/check_runtime_plan.py` 的第三 profile 常量（同文件，不新建模块）、13.104 的 `assembly/compress_checkpoint_test.go` 内嵌接口随合并改名（同文件）。

### C5.2 明确不在本变更写面的文件

`openspec/changes/archive/**`（冻结）、`examples/wechat-bot/.wechat-config/data`、用户 `--source` 根、外来未跟踪六件（C8.4-4）、`no-prefix-criterion`/`runtime-architecture-convergence`/`config-surface-normalization` 目录。以上只读取证。

### C5.3 核对命令（可复跑）

`for f in <上表路径>; do [ -f "$f" ] && wc -l < "$f" || echo MISS; done` —— 本轮 34 个候选文件**全部存在**、行数与表中一致；无 MISS。锚存在性另见 C7.5。

## C6 三锚及完成约束

优先复用现有 `test_check_runtime_evidence.py`、`test_check_dod_coverage.py`、`test_run_runtime_scenarios.py`、`test_check_event_integrity_mutations.py`、`test_check_runtime_plan.py`。新增反例应进入相应既有文件，名字和预期匹配在tasks中冻结。CLI读取报告不算真实模型/存储行为，正式Host Boot和消费路径另有锚；反之长测成功不能代替checker的负例。

真实资源缺失时明确未验：rustviking CLI、exec/tmux隔离用户、CI/保护配置。基于t.TempDir/真实引擎/确定性模型的组合可以证明其已覆盖路径，不假称现场真模型或全部生产档位。外部未跟踪文件不进入候选，正式干净副本的文件清单须包括本候选新文件而非简单`git ls-files`。

13.105收齐到期实现/单元验证并准备F2完整选择集、命令、资源与证据保存清单；13.106审这些结果和清单，不要求F2–F4先执行。F2跑本地完整矩阵及CI对应的可本地执行命令，F3作最终候选独立审；F4按具体授权提交/push后核实际CI、保护及交付。F4核验前，服务端保护始终记未验证/未生效，不宣称已拦截；未接本地正式入口或CI配置缺消费者属于本域缺陷，不能后移。

## C7 实施签发区（13.102 草签稿）

> **状态：草签（draft）。待独立设计审确认后生效。** 任一字段被独立审驳回即回本表重填，不以"下游已开工"倒推生效。生效条件四条同时成立：需求保全可论证、全局更简单可论证、三锚可复跑、写面只含本单所需文件。

| 签发字段 | 值 |
|---|---|
| 13.101目标/模式/旧项基线 | 基线＝C8 全节（151 编号／58 Requirement／212 Scenario／11 处漏项），候选 `7b0cde16` 与原症状直译（"多轮压缩后模型仍能**发现**并按票据召回该事实"）不变；公开模式清单＝C8.3 十四行。**不变**，不在本表重述。 |
| 所选装配/证据/同步方案及否决依据 | 证据面选 **C3 方案 B（复用一个原始结果派生器）**：锚判词一律取自 `run_runtime_scenarios.event_subtest_states:1219-1242`，checker 侧删"自报即可"判词，格式不变。装配面选"保留明确 scope 限制＋复用现有注册契约"。同步面选 13.105 的**显式合并输入**（不建影子 delta）。否决依据见 C3 同尺表三栏与能力装配表三栏。 |
| 接口、报告字段、配置/持久格式与迁移 | 逐项列于 C7.2。结论：evidence v2 格式**不变**、`SESSION_FORMAT`／持久格式**不变**、`config.AgentSpec`／`hostAgent` 字段**不变**、不新增库与外部依赖；**唯一删除**是 `compressSessionSource` 与 `compressProductStore` 合并为一个 4 方法内聚口；**唯一新增**是规划产物 `task-graph.json` 的 `domain_profile`／`leaves`／`signatures` 字段（非产品接口）。 |
| 13.103/104/105逐叶精确文件与全部消费者 | 逐叶文件清单＋每文件理由见 C7.1；全部真实消费者表见 C7.3；写面之外的候选一律降级为"只读参考"。 |
| 正/负/真实入口命令、构建标签、预期命中与回归集 | 逐叶锚与命令见 C7.5（正/负/消费者三面，含完整命令与预期命中数）；构建标签与相邻回归见 C7.7（C7.5 末行仅为 13.104 最小集）；负变异落点见 C7.6。 |
| 实际外部动作授权、回滚与可反转探针 | 本叶只读运行：不写产品/测试/CI/主规格，不提交、不 push、不 archive、不改权限。13.103/104/105 的外部动作授权与回滚触发见 C7.7；需要未授权外部修改即停并回报。 |
| 独立设计审：范围/结论/未查面 | 未执行（本表为草签）。送审范围＝C2 八行反证结果、C3 同尺表、C5 核对、C7 全文；未查面＝C7.8 逐项列出（真实存储 CLI、exec 隔离用户、远端 CI 与保护）。 |

### C7.1 逐叶精确写面与每文件理由

**13.103 证据解释与实际拒绝入口**（阶段：实施；前置 13.102 生效）

| 文件 | 动作 | 理由（缺它则何者不成立） |
|---|---|---|
| `scripts/gates/check_runtime_evidence.py` | 改 | `check_event_evidence_subtests:2661-2701` 改为按日志反查每个 `(parent,name)` 锚的 run/pass；删"自报 pass 即通过"分支。C2 第 1 行 ②③ 的直接落点 |
| `scripts/gates/test_check_runtime_evidence.py` | 改（并入三例） | 三例并入本文件，不新建文件；现有 `EventFixture` 已给日志/证据/树三面夹具 |
| `scripts/scenarios/run_runtime_scenarios.py` | **不改**（已核可达） | checker 侧 `_event_runner():2012-2028` 已按"导入生产者不复制"加载它，`event_subtest_states:1219`／`index_event_actions:1202` 是模块级函数，直接可调 ⇒ 派生点唯一且零改动。若实施中确需改签名，先回本表补签，不得顺手改 |
| `scripts/gates/check_runtime_plan.py` | 改 | 加第三 `domain_profile`（C7.4）；旧两 profile 逐字保留 |
| `scripts/gates/test_check_runtime_plan.py` | 改（并入三例） | 三例落此；既有 `LintObjectSelectionTest` 已真跑 `bash scripts/lint.sh --only contracts` |
| `openspec/changes/runtime-recovery-ideal/task-graph.json` | 新增 | 规划门判定对象；现文件不存在（C2 第 7 行实测），无它则 `gates/lint.sh:181` 永远 skip |
| `scripts/gates/archive-preflight.sh` | 改（仅接线可见性） | 保留 `--commit` 语义；把"无调用者"改为有调用者（C7.3 末两行） |
| `.github/workflows/ci.yml` | 改（一个作业） | `--commit "$GITHUB_SHA" --base "$BASE_SHA"` 的真实日常消费者；不改既有作业 |
| `scripts/scenarios/test_run_runtime_scenarios.py` | 改（并入一例） | `test_preflight_rejects_corrupt_generated_evidence`：隔离副本保留真实 preflight 控制流，在 runner 成功生成后、checker 读取前篡改产物 |
| `scripts/gates/test_check_dod_coverage.py` | 改（并入一例） | `test_archive_tree_change_without_evidence_is_refused`：经实际归档检查，不改消费者/预期结果、不加产品 hook |
| `scripts/gates/lint.sh` | 改（仅归档接线一段） | C7.3 方案②的唯一本地落点：检测工作树含 `openspec/*/archive/` 新增路径时跑 `archive-preflight.sh --commit`；不改其余段落，归档拒绝接入日常调用链 |
| **不改**：`check_dod_coverage.py`、`check_event_integrity_mutations.py`、`runtime_checklib.py`、`scripts/{lint,preflight}.sh`、`scripts/gates/preflight.sh` | 只读参考 | 它们各自的读取面已成立；改它们属"复制另一套解释器"（13.103 停止条件） |

**13.104 公开装配与行为承诺**（阶段：实施；前置 13.102 生效、12.104、06.104）

| 文件 | 动作 | 理由 |
|---|---|---|
| `assembly/plugin_catalog.go` | 改（仅注释/拒绝文案若需） | 声明面范围裁定落在 `declaredPluginsOf:302-306`；C2 第 4 行裁"不支持 run/host 禁用"，故此处**不加**过滤分支，只保证拒绝文案点名 scope |
| `assembly/plugin_catalog_test.go` | 改（并入两例） | `TestDeclaredPluginChoiceSurvivesRestart`（受支持选择真实生效＋不支持 scope 具名拒绝）；重启后仍生效 |
| `assembly/compress_consumer.go` | 改 | 两窄口合并为单一内聚口（4 方法），字段 2→1，构造参数 2→1 |
| `assembly/compress_mount.go` | 改（一处） | `NewCompressionConsumer(face, face)` → `NewCompressionConsumer(face)` |
| `assembly/compress_checkpoint_test.go` | 改（一处） | `countingCompressSessions` 内嵌的接口名随合并改；仍只装饰 store 两面 |
| `assembly/summary_gist.go` | 改（仅当呈现开销未计入） | `boundedSummaryGist:133`／`bandSummaryInstruction:43` 的预算口径须含呈现开销 |
| `assembly/summary_gist_test.go` | 改（并入一例） | `TestTinySummaryBudgetIncludesPresentationOverhead`（极小预算下界） |
| `assembly/host_tools_test.go` | 改（并入一例） | `TestExecutionIsolationSurvivesSessionRestart`；**只核可达面**：目录＋资源身份。用户/环境两面现接口不可达（C7.2 第 4 行），锚内不得伪造 |
| `tests/integration/history/symptom_anchor_midband_test.go` | 改（并入一例） | `TestEarlyFactRemainsDiscoverableAfterRepeatedCompression`：核真实模型请求的历史线索与后续召回，不改测试手工给 key |
| **不改**：`host_document.go`、`prepare.go`、`run.go`、`api/config/{config,decode,defaults,fingerprint}.go` 及五个镜像测试 | 只读参考 | C3 已否决新增运行级声明字段；改它们等于新增公共字段而无独立需求 |

**13.105 旧项、规格与候选收口**（阶段：对账/文档实施；前置 12.105、06.105、13.103、13.104）

| 文件 | 动作 | 理由 |
|---|---|---|
| `CARRYOVER.md`、本域 `design.md`/`tasks.md` | 改 | 到期旧项逐条落结论（实现/迁锚/接口满足/配置耦合/不同义项） |
| 主 spec 与 delta、`golden/runtime/{architecture,durable_truth,dod_predicates}.json`、`docs/OPERATION.md`、wiki 与 `docs/api` 生成物 | 改（逐条列举，禁通配） | 唯一合并输入；不得为迎合实现删行为 |
| `scripts/gates/test_sync_runtime_specs.py` | 新增 | 三例：合法并集成功／冲突拒绝／缺 rename 源拒绝；工具本体若需实现，先单独签精确文件 |
| **不改**：`openspec/changes/archive/**`、外来未跟踪六件、其它活动 change | — | 冻结面与外来面 |

### C7.2 接口／字段／格式／外部依赖变动一览

| 对象 | 变动 | 理由 |
|---|---|---|
| evidence.json（v2）字段集 | **不变** | C3 否决格式迁移；`subtests[].status` 语义由"自报"加强为"须与日志一致"（判据加强，不动格式） |
| `check_runtime_plan.py` CLI（`--change-dir/--json`） | **不变** | `gates/lint.sh:179` 与 `runtime-contracts.yml:126` 已按此调用，改参即断两处消费者 |
| `task-graph.json`（本变更规划产物） | **新增** 字段 `domain_profile`／`leaves[]`／`signatures[]` | 派发门需机器可读的叶集与签态；散文不可判定。非产品接口，不进 `api/`，不被产品代码读取 |
| `done` 叶的证据形状 | **复用**旧十域 profile 的 `{run_id,commands,exit_codes,artifact_paths}` | 不造第二套证据形状（旧 profile 已定义且被自测钉住） |
| `NewCompressionConsumer` 签名 | **删** 1 参数／**删** 2 接口→**增** 1 接口（4 方法） | 唯一生产实现同一对象；第二使用者是测试装饰（C2 第 3 行） |
| `ExecutionDecl`／`ExecSpec` | **不变**（不加执行用户/环境） | 加字段无独立需求且属部署可变面，须走配置字段与主规格；本轮只把"不可达"如实记入锚边界（C7.8） |
| `config.AgentSpec.Plugins`／`hostAgent.Plugins` | **不变** | C3 否决运行级声明面 |
| 持久格式／`SESSION_FORMAT`／storage tier 名 | **不变** | 本单元不触碰恢复格式 |
| 外部依赖（库、CLI、服务、网络） | **不新增** | 无新能力需求；`rustviking` 仍 ABSENT → file 档继续记未验 |
| Python 版本面 | **不变**：3.9.6 | 新代码不得用 3.10+ 语法（`match` 语句、运行时求值的联合类型注解） |

### C7.3 全部真实消费者表

| 消费面 | 真实入口 | 本轮状态 |
|---|---|---|
| 证据解释（单件） | `python3 scripts/gates/check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --evidence <run>/evidence.json --repo-root . --json` | 在树，实跑过（C2 三例） |
| 证据解释（整包） | `... --bundle <DIR> --json` | 在树，实跑过（10短/6长/1变异 exit 0） |
| 证据解释（合同） | `... --check-contract --contract golden/runtime/event_integrity_scenarios.json --plan-graph <graph> --json`（`gates/lint.sh:199,208`） | 在树；graph 缺则该段 skip |
| DoD | `python3 scripts/gates/check_dod_coverage.py --mutation-evidence <DIR>/mutations`（`gates/lint.sh:159`） | 在树；**不读本日志**（C2 第 1 行实测） |
| 变异 | `python3 scripts/gates/check_event_integrity_mutations.py --all --json --out <DIR>/mutations`（`gates/preflight.sh:138`） | 在树 |
| 规划门（本地） | `python3 scripts/gates/check_runtime_plan.py --change-dir <active change> --json`（`gates/lint.sh:179`） | 在树；**当前无对象而 skip**（`gates/lint.sh:181-182`） |
| 规划门（CI） | 同命令，`.github/workflows/runtime-contracts.yml:126` | 同上；加 graph 即生效，**无需改 CI 文件** |
| 本地串行链 | `env TAGENT_RUNTIME_EVIDENCE_DIR="$BUNDLE" bash scripts/lint.sh`；`env TAGENT_RUNTIME_EVIDENCE_DIR="$OUT" bash scripts/preflight.sh` | 在树，本轮未跑 |
| 归档 | `bash scripts/gates/archive-preflight.sh --evidence "$BUNDLE"`；`--commit REV --base REV` | 在树；**`--commit` 无调用者**（C2 第 6 行） |
| 插件声明面 | `hostAgent.Plugins` → `declaredPluginsOf` → `Run.agentPlugins`（`plugin_catalog.go:380`） | 在树；四锚实跑 exit 0 |
| 装配挂载 | `Run.mountBuiltinPlugins:333`、`hostPlugins:361`、`Prepare.buildBinding`（`prepare.go:124` 取 `s.plugins`） | 在树，只读核对 |

**归档拦截接线方案（C2 第 6 行的处置）**：保留 `archive-preflight.sh` 的 `--commit` 语义不动，补两处**真实调用者**——① `.github/workflows/ci.yml` 新增一个作业在 push/PR 上跑 `bash scripts/gates/archive-preflight.sh --commit "$GITHUB_SHA" --base "$(git rev-parse origin/${{ github.base_ref }})"`（提交带归档路径而证据不绑同一 revision 即拒）；② 本地 `scripts/gates/lint.sh` 检测归档路径变化并拒绝。**编排者追认（2026-11-21）**：② 的实现按提交/未提交分对象——已提交移动（`HEAD^..HEAD`）走 `--commit HEAD`；未提交移动（`diff HEAD` ∪ `ls-files --others`）无 revision 可判，走 `--evidence`，超出本段"跑同一命令"的字面，属必要偏离（无 revision 的对象必须以证据目录判定），记录于 C9.9-①，予以追认。否决替代：本地 git hook 需仓库外 `core.hooksPath`（未授权外部修改）；"靠纪律先跑"已由脚本自述否定（`:26-29`）。**F4 前一律记"未生效"**，CI 配置审查不等于服务端保护。**集成差异（编排者记录）**：共享工作树 `lint.sh --only contracts` 因外来未跟踪 `test_check_runtime_symptoms.py` 的静态注册表失配而红（324 例中唯一 1 例，文件 mtime 停留在 2026-11-10/11，非本变更加写）；候选副本（不含外来文件）同入口 exit 0。外来面不吸收不屏蔽，该红不阻塞本叶。

### C7.4 13.103 对 `check_runtime_plan.py`：**适配**，非复用、非新平台

- **事实**：该门分发在 `check_runtime_plan.py:1061-1067`，只支持"无 `domain_profile`＝旧十域 runtime-rebuild 图"与 `domain_profile=event-integrity`（六域，`EVENT_PROFILE:126`，节点数从 graph 读）。两套常量都描述不了本变更的 **3 单元 × 18 叶 × 嵌套域**结构；且本变更**没有** `task-graph.json`（C2 第 7 行）。
- **选"适配"的理由**：① 判定对象（graph）与解析层（`runtime_checklib`、`collect_checkbox_ids:442`、`check_event_checkboxes:918`）可原样复用，不复制解释器；② CLI 契约 `--change-dir/--json` 不变，`gates/lint.sh:179` 与 `runtime-contracts.yml:126` 两个既有入口**零改动**接管；③ 第三条 profile 是最小增量（一个分支＋一组从 graph 读取的规则），与 04.5 给 `event-integrity` 加第二 profile 的先例同形。
- **被否**：纯复用（不传 profile）→ 静默套旧十域常量，得出与对象不符的判词（旧 profile 的 123 叶/阶段门常量会把 18 叶图判红或误判绿）；`event-integrity` profile → 六域/叶数形状不匹配；新规划平台 → 复制解析与图语义，正撞 13.103 停止条件"复制另一套解释器"。
- **新 profile 检查项（逐条可判定）**：`domain_profile=="runtime-recovery-ideal"`；`leaves` 恰 18 且 id 与三单元 `tasks.md` 的 checkbox id 双向相等；`depends_on` 引用已知叶、无自环、Kahn 无环，**且与三单元 `tasks.md` 各叶"前置"行双向相等**（依赖唯一真相在 tasks.md，graph 与之漂移即红，杜绝第二计划真相）；每叶 `verify[]` 至少一条且 `name` 非空；`done` 叶带旧 profile 同款证据四字段；`signatures` 中某叶 `state!="signed"` ⇒ 该叶不可派（负锚 `test_missing_design_signature_blocks_implementation`）。
- **不改**：旧十域与 `event-integrity` 两条 profile 的常量、规则与文案逐字保留（并存迁移，回退零 diff）。

### C7.5 三锚全名＋完整命令＋预期命中

**13.103（Python；无构建标签）**

| 面 | 命令 | 预期命中 |
|---|---|---|
| 正锚 | `python3 scripts/scenarios/test_run_runtime_scenarios.py -v` | 全文件用例数 **>0** 全绿（本轮不冻结旧总例数，取实际 `Ran N`）；原始 exit 0 |
| 正锚 | `python3 scripts/gates/test_check_runtime_evidence.py -v` | 同上；须含并入后的 `test_raw_child_events_determine_subtest_verdict` 恰 1 个顶层命中 |
| 正锚 | `python3 scripts/gates/test_check_event_integrity_mutations.py -v`、`python3 scripts/gates/test_check_dod_coverage.py -v`、`python3 scripts/gates/test_check_runtime_plan.py -v` | 各文件 `Ran N>0`、exit 0 |
| 负锚 | `python3 scripts/gates/test_check_runtime_evidence.py -v -k test_subtest_without_run_is_refused` | 恰 1 顶层用例；checker 非零且点名缺 run 的 `(parent,name)` |
| 负锚 | `python3 scripts/gates/test_check_runtime_evidence.py -v -k test_parent_pass_cannot_invent_a_subtest_pass` | 恰 1；同一构造（父日志有效、子行删除、证据自报 pass）→ 非零 |
| 负锚 | `python3 scripts/gates/test_check_runtime_evidence.py -v -k test_same_log_has_one_verdict_across_consumers` | 恰 1；runner 派生 `missing` 与 checker 判词一致（实施后均拒） |
| 负锚 | `python3 scripts/gates/test_check_runtime_plan.py -v -k test_unapproved_leaf_is_not_dispatchable` | 恰 1；`signatures` 未签叶 → 非零 |
| 负锚 | `python3 scripts/gates/test_check_runtime_plan.py -v -k test_unknown_or_missing_active_leaf_is_refused` | 恰 1；未知/缺失叶 id → 非零 |
| 负锚 | `python3 scripts/gates/test_check_runtime_plan.py -v -k test_missing_design_signature_blocks_implementation` | 恰 1；缺签态 → 非零 |
| 负锚（正式入口） | `python3 scripts/gates/test_check_dod_coverage.py -v -k test_archive_tree_change_without_evidence_is_refused` | 恰 1；经实际归档检查拒绝 |
| 负锚（正式入口） | `python3 scripts/scenarios/test_run_runtime_scenarios.py -v -k test_preflight_rejects_corrupt_generated_evidence` | 恰 1；隔离副本保留真实 preflight 控制流，篡改点在 runner 成功生成后、checker 读取前；非零且点名该产物 |
| 消费者 | `env TAGENT_RUNTIME_EVIDENCE_DIR="$BUNDLE" bash scripts/lint.sh` | 原始 exit 0；`$BUNDLE` 绑定本轮绝对产物目录 |
| 消费者 | `bash scripts/gates/archive-preflight.sh --evidence "$BUNDLE"` | 原始 exit 0，三段各自打印命令与退出码 |
| 消费者 | `env TAGENT_RUNTIME_EVIDENCE_DIR="$OUT" bash scripts/preflight.sh` | 原始 exit 0；`$OUT` 必须是本轮新目录（预灌非法目录随后被覆盖不算负控） |

**13.104（Go；`./assembly` 无构建标签；smoke 模块 `-tags smoke` 仅属其它变更入口）**

| 面 | 命令 | 预期命中 |
|---|---|---|
| 正锚 | `go test ./assembly -run '^(TestBuiltinCatalogMountsEveryFeature\|TestHostBootCompressionObeysTheDeclaredSummaryBudget)$' -count=10 -v` | 2×10 全 `--- PASS`；本轮已实测 1 枚（`TestBuiltinCatalogMountsEveryFeature` exit 0），另一枚现存于 `assembly/host_test.go` |
| 正锚 | `go test ./tests/integration/acceptance -run '^TestScenario11ConfigDrivenSubagentAndPipeline$' -count=10 -v` | 1×10 `--- PASS` |
| 负锚（拟新增） | `go test ./assembly -run '^(TestDeclaredPluginChoiceSurvivesRestart\|TestTinySummaryBudgetIncludesPresentationOverhead\|TestExecutionIsolationSurvivesSessionRestart)$' -count=10 -race -v` | 3×10；各子场景非零执行；缺隔离资源记未验（不得以 skip 记绿） |
| 发现负锚（拟新增） | `go test ./tests/integration/history -run '^TestEarlyFactRemainsDiscoverableAfterRepeatedCompression$' -count=10 -v` | 1×10；核真实模型请求的历史线索与召回 |
| 消费者 | `go test ./tests/integration/host -run '^TestProjectionAssemblyServesTheThreeUses$' -count=10 -v`；`go test ./tests/integration/history -run '^TestSymptomAnchorMidbandProcessRecalledByTicket$' -count=10 -v` | 各 1×10 |
| 相邻回归（最小集；完整清单见 C7.7） | `go build ./...`；`go test ./assembly -count=1` | 原始 exit 0；合并两窄口后整包不红；`check_concept_budget.py` 按 C7.7 另跑 |

**13.105（Python/openspec/godoc；无构建标签）**

| 面 | 命令 | 预期命中 |
|---|---|---|
| 正锚 | `openspec validate runtime-recovery-ideal --strict`；`bash scripts/gates/check-openspec.sh`；`python3 scripts/gates/check_spec_hygiene.py`；`python3 scripts/gates/check_doc_links.py`；`python3 scripts/gates/check_doc_budget.py`；`bash scripts/gates/gen_godoc.sh --check` | 各原始 exit 0，留实际扫描集 |
| 合并正锚（拟新增） | `python3 scripts/gates/test_sync_runtime_specs.py -v -k test_compatible_requirement_union_is_synced` | 恰 1；合法并集成功 |
| 合并负锚（拟新增） | `... -k test_conflicting_requirement_text_is_refused`；`... -k test_missing_rename_source_is_refused` | 各恰 1；冲突/缺源拒绝 |
| 消费者 | 隔离副本按 F4 预定 OpenSpec 命令同步后核主 spec 行为承接与归档输入 | 不在主树提前 archive |

**锚名存在性（13.102 准入项，C8.2.3-9 复核）**：七枚拟新增名在 Go 树**全部零命中**——`TestBootIndexEntriesBoundedAcrossGenerations`、`TestNoTierCommitPreservesApplicationOrder`、`TestRecoveryEntriesPreserveStateAndObligations`、`TestDeclaredPluginChoiceSurvivesRestart`、`TestExecutionIsolationSurvivesSessionRestart`、`TestEarlyFactRemainsDiscoverableAfterRepeatedCompression`、`TestTinySummaryBudgetIncludesPresentationOverhead`。五枚既有名已核在树：`TestBuiltinCatalogMountsEveryFeature`（assembly）、`TestHostBootCompressionObeysTheDeclaredSummaryBudget`（assembly/host_test.go）、`TestScenario11ConfigDrivenSubagentAndPipeline`（acceptance）、`TestProjectionAssemblyServesTheThreeUses`（host）、`TestSymptomAnchorMidbandProcessRecalledByTicket`（history）。六枚 Python 负锚名同样**零命中**（属拟新增）。派发时规则：`-k`／`-run` 选择器零匹配即失败，不得当通过。

### C7.6 负变异落点与凭据

| 叶 | 变异落点（copy-on-write 副本内） | 控制观测 |
|---|---|---|
| 13.103 | 删 `check_runtime_evidence.py` 的日志反查 → `test_subtest_without_run_is_refused`／`test_same_log_has_one_verdict_across_consumers` 必须转红；删 `signatures` 判据 → `test_missing_design_signature_blocks_implementation` 转红；删一条 `depends_on` → `test_unapproved_leaf_is_not_dispatchable` 转红 | 未变异副本同三例绿；控制绿与变异红同一次执行内取 |
| 13.103 | 移走 `task-graph.json` → 规划门回 `skip`，`gates/lint.sh:181` 的 skip 文案出现（证明是"有对象才判"而非恒绿） | — |
| 13.104 | 删 `declaredPluginsOf:302-306` 的 scope 拒绝分支 → `TestDeclaredPluginChoiceSurvivesRestart` 转红；`boundedSummaryGist` 恢复字面界 → tiny-budget 锚转红；去掉执行目录/资源身份承接 → `TestExecutionIsolationSurvivesSessionRestart` 转红 | `go test ./assembly -count=1` 控制绿 |
| 13.105 | 让同步工具取"最后一份"而非拒绝冲突 → 冲突负锚转红；删 rename 源校验 → 缺源负锚转红 | 合法并集正锚绿 |
| 全部 | 变异只在副本，禁止硬链后原地改；仓库内 `git status --short -- scripts/ assembly/ api/ tests/ golden/` 必须为空 | — |

隔离反证原始凭据（本叶，`/tmp/c13102/`，产品零改动）：

| 用例 | 原始 exit | 落点 |
|---|---|---|
| `test_raw_child_events_determine_subtest_verdict` | **0** | `out-pos-anchor.txt` |
| `test_parent_pass_cannot_invent_a_subtest_pass` | **1**（`0 == 0`：被接受） | `out-neg-anchor.txt` |
| `test_same_log_has_one_verdict_across_consumers` | **1**（runner `missing` vs checker pass） | `out-neg-verdict.txt` |
| `test_consistent_stale_bundle_is_rejected`（既有例） | **0**（拒绝成立） | `out-stale-bundle.txt` |
| 消费者锚 4 顶层 | **0**（`--- PASS`×4，`ok ... assembly 0.373s`） | `out-assembly-anchor.txt` |
| `openspec validate 13-verified-completion --json` | **1**（`Unknown item`） | `out-openspec-nested.txt` |

### C7.7 相邻回归、权限/数据边界与回滚触发

- **相邻回归**：13.103 必跑 `test_check_runtime_workflow.py`（断言 plan 门接线：`:72/133/288`）＋ `test_check_runtime_plan.py` 的 `LintObjectSelectionTest`（合成树里真跑 `lint.sh --only contracts`）；13.104 必跑 `go build ./...`、`go test ./assembly -count=1` 与 **`python3 scripts/gates/check_concept_budget.py`**（两窄口合并改变其计数面，且它在 `gates/lint.sh` 链内；baseline 只降不抬，抬基线即回设计门）；13.105 必跑六门（C7.5 第三表）。任一红先判归属（干净基线复验），不以下游全绿掩盖。
- **权限/数据边界**：只写本单白名单；真实数据 `examples/wechat-bot/.wechat-config/data` 与用户 `--source` 根不读不写；全部夹具用 `t.TempDir`／`tempfile`／自有合成根；不改 `.git` 索引/refs，不 commit/push/archive；外来未跟踪面只读。
- **回滚触发（任一即停并回设计门）**：① 13.103 若必须让 runner import checker 或复制 `index_event_actions` 才能统一判词；② 若新 profile 需要改旧两 profile 的常量或文案；③ 若归档接线需要仓库外配置（`core.hooksPath`）才能成立；④ 13.104 若合并两窄口需改 `plugin.Instance` 协议或 catalog 注册契约；⑤ 13.104 若执行用户/环境面被要求"补齐"而无主规格条款与配置字段授权；⑥ 13.105 若冲突需改写任一侧要求原文或抬 baseline。回滚＝丢弃该叶副本改动，仓库内零 diff，如实保留未完成状态。

### C7.8 未查面（送审时必须与结论同读）

真实存储 CLI `rustviking`（ABSENT → file 档未验）、exec/tmux 隔离用户与环境（接口不可达）、远端 CI 与分支保护（未运行/未生效）、完整 F2 矩阵（十短/六长/全量变异）、F3 最终审、F4 交付。以上均**不在本表完成面**。

### C7.9 C8.2.3 十一项漏项／待裁的处置去向

行数与 C8.2.3 表体一一对应（11 行）；行内子项共 **31** 个（3+1+1+3+4+5+2+3+6+2+1），逐项在下表"子项"列具名，去向只有一处。

| # | 子项（C8.2.3 原行内枚举） | 性质 | 处置去向 | 触发/合同落点 |
|---|---|---|---|---|
| 1 | 09.R3a 三条停线：零字节尾来源未证／该组合本次红是另一症状／该组合×3 绿未取得 | 已到期缺陷，不得后置 | **A/B 会签：12.105（义务链）＋06.105（存储面）** | 该组合长跑 ×3；C 不代写判定 |
| 2 | 09.R4"修复后必绿"未取证 | 验收面兑现一半 | **06.105＋F2 矩阵** | 每代成本锚在冻结候选转绿 |
| 3 | 09 域 4.4 验收面在单二进制内不可判定 | 需重裁，原面冻结 | **06.105** 出等价证明或改判据并记偏离 | 不得追认缩水后照勾 |
| 4 | 03.R3 三项偏差：`docs/wiki/agent/event-flow.md`、`docs/wiki/platform/org-hot-reload.md` 叙述滞后；规格一处 AND 子句无锚 | 文档/缺锚 | 文档归**13.105**；缺锚归**13.104**（拟新增锚进入 C7.5 名单） | 文档门＋新增锚 |
| 5 | 10.R5b/R5c/R5d/R5e 四处级联死判据 | 无人接的留删分叉 | **13.105** | 逐条在合并输入生成前给留删结论 |
| 6 | 08.R3"明确不删"五类：代际退役／`Retention.Retire`／`headless-retired`／cache-cold 测试／`migrate.go` 与分片布局拒绝 | 删除叶自身负向义务 | **06.105** | 该五类残留>0 且各有活消费者证据 |
| 7 | 11.R2"确定拒绝"第五值待复核；11.R3 逐键档多键拒绝的部署后果待批准 | 真待裁 | **移交 A（编排者裁定 → 写入 `12.105` 签发记录）** | 未批准前不得承诺该档可用 |
| 8 | 12.R3/02.2 三项：卸载 disposer 无生产调用方／贡献实例名与 builtin 名同址不可分别寻址／reload 后插件集替换未单独立锚 | 无叶无命令的未证面 | **A（`12.105`）**；②的"分别寻址"同时构成 13.104 的**不支持声明**（C7.2 第 7 行：声明面本身不变，故只能在主规格与本表声明"贡献实例名与 builtin 名同址时不可分别寻址"） | 真实入口核验或明确声明不支持 |
| 9 | 六处拟新增锚不存在 | 零匹配风险面 | **本叶已核（C7.5）**。归属拆分：`TestBootIndexEntriesBoundedAcrossGenerations` 归 **06.105**；`TestNoTierCommitPreservesApplicationOrder`、`TestRecoveryEntriesPreserveStateAndObligations` 归 **12.105**（两单元 tasks 已列，落点文件属 A/B 写面，不进 C7.1）；其余三枚归 **13.103／13.104** | 派发前逐个核存在性与选择器命中数；零匹配＝失败；13.105 对账六枚全部有主 |
| 10 | 05.R1 负锚原名退役重定两枚；06 `TestCostPhasesAreMeasuredWithoutOverlap` 已删 | 命令漂移 | **A/B：`12.105`／`06.105` 签发时重述命令** | 按旧命令复跑=零匹配 exit 0，禁当通过 |
| 11 | 13.R2 门把目录条目称作"Go 文件"的措辞缺陷 | 已承接仅闭环 | **13.105（10.R7 已记）** | 不重复派 |

**另有一项不在 C8.2.3 表内、但同属待裁**（源自 C8.3 行"exec／tmux 安全上下文"）：执行用户与环境字段在现接口不可达。处置：**13.104 不得实现用户/环境两面**，锚内只核目录与资源身份；若主规格要求补齐，则回设计门新增配置字段（本轮不签）。

独立审确认前，13.103/13.104 不可派。保留现有格式与接口的项，也必须以证据填写"不改变及其理由"，不能留空默认通过。状态核销规则与资源纪律统一引用[ORCHESTRATION](../../ORCHESTRATION.md)。

## C8 · 13.101 承接核定记录

> 本节产生方式：13.101 的全部活动＝**只读取证＋计划记录**。未改产品 Go、测试实现、CI、主规格、规则/skill、权限、git 索引/refs，未 commit/push/archive，未勾选 13.101 自身任务框（由编排者核验后代勾）。唯一写入面＝本节。

### C8.0 口径与方法（先冻结，再判定）

1. **计数口径**。「正文子要求」＝任务行正文中**除编号外的实质约束分句**（MUST/MUST NOT/不得/白名单/三锚/前置/上报项/停止条件）；纯凭据叙述与 exec log 不计。Scenario/Requirement 计数＝各 delta `spec.md` 中 `^#### Scenario:`／`^### Requirement:` 行数，命令 `grep -c '^#### Scenario:' changes/*/specs/*/spec.md`；本域 20 个 delta 文件合计 **58 Requirement / 212 Scenario**。
2. **到期阶段五选一**，后四类为**后置项**且必带承接叶与触发命令（写在同一格内，命令缩写见第 3 条）：
   - `实现+单元验证（叶）`：行为必须由该叶实现并取得正/负/消费者三锚；括号内为 CARRYOVER 指定的主承接叶（13 域因主承接是只读审叶，另标实施叶）。
   - `13.106审`：该条实质是独立审本身，审者须复现其事实。
   - `F2矩阵`：该条实质是同候选完整矩阵（十短/六长/mutations 全量），由 13.105 输出清单、F2 执行。
   - `F3终审`：该条实质是最终候选的独立整体审。
   - `F4外部实效/交付`：该条实质是远端 CI、分支保护、提交/push、归档与交付复核。
3. **触发命令缩写**：`PRE`＝`bash scripts/preflight.sh`；`LINT`＝`env TAGENT_RUNTIME_EVIDENCE_DIR="$BUNDLE" bash scripts/lint.sh`；`ARCH`＝`bash scripts/gates/archive-preflight.sh --evidence "$BUNDLE"`；`EVID`＝`python3 scripts/gates/check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --repo-root . --bundle "$BUNDLE" --json`；`MATRIX`＝`python3 scripts/scenarios/run_runtime_scenarios.py --scope event-long --seed <S> --window <W> --repeat 1 --timeout 6270`；`CI`＝`gh run view <run-id> --json headSha,conclusion,jobs`（需授权）。
4. **标记六选一**（互斥，按此顺序取第一条成立者）：
   - `驳回`：该条怀疑的前提已被当前源码或实验推翻，结论是"不改"。
   - `实测`：本节用**命令**直接测得了该条当前状态（git 取数、sha256 比对、符号残留计数、grep 存在性、资源探针）。
   - `代码反例`：已读路径可构造失败，但本轮未运行相应交错。
   - `建议`：该条当前形态是候选方向/设计分叉，需与保留方案同尺比较。
   - `历史`：唯一凭据是**更早 revision/树**的运行记录（`9547faca`／`0f84b2d1` 世代或 W 阶段）；按顶层 D1 证据等级只能追溯、不能给当前候选放行。
   - `目标`：用户已确认的行为不变量，当前无实现证据或证据不成立。
5. **边界**：所有 `历史` 标记项的勾选状态一律**不继承**（顶层 D6「旧[x]保留原样；新叶不得借旧勾选完成」）。`实测` 只覆盖该格所述的那一条性质，不外推为该叶完成。

### C8.1 151 编号逐项行为身份表

每个"用户可见行为身份"在删掉实现名后仍须说清用户/调用方观察到什么。**编号身份＝来源目录＋编号**；跨域同名（01 的 `1.1` 与 05 的 `1.1`）不是一项。

#### C8.1.1 01-three-hooks-golden（6 项；delta `three-hooks-extensibility` 4 Requirement / 12 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 01.R1 | 默认 builtin 装配与"声明真被消费"在新增消费者下仍成立，不为新消费者另开特性名分发表 | 3项：R1a∧R1b 才核销／保留 builtin 默认与声明真消费／不建特性名分发表 | 实现+单元验证（13.104） | 目标 |
| 01.R1a | 装配守护能逐例拒绝未注册项、硬编码成员与陈旧例外，且守护自身可独立提取验证 | 4项：脚本+自测+文档补丁+候选依赖清单／Python 正负锚与 Go 消费者锚／29 发射点变异 29/29 转红／真树读数 `go_files=32 consumers=1 errors=0` | 实现+单元验证（13.104） | 历史 |
| 01.R1b | 生命周期接线（禁用/卸载/换代）后守护仍拒，且拒绝保留原对象与未结义务 | 4项：先复跑 R1a 两条 Python／三枚 Go 锚 `-race -count=10`／拒绝保原对象·空工作可注销·换代同权威／共享 lint 由 13 集成 | 实现+单元验证（13.104） | 历史 |
| 1.1 | 三挂点登记面建成：不变量文本、注册表元数据与声明键三方交叉锚，缺一即红 | 3项：`architecture.json` mounting 节／`plugin_mounts.json` schema+元数据校验／`lint.sh` 三挂点段（自测在前、判树在后） | 实现+单元验证（13.104） | 历史 |
| 1.2 | 管道成员来源受限：单成员与复合字面量 append 被具名拒绝，例外只经受校验的注册表 | 3项：按类型锚推导容器（推导为空即红）／现树 7 处 3 文件点名／6 例自测＋陈旧例外即红 | 实现+单元验证（13.104） | 历史 |
| 1.3 | `plugins` 声明是活字段：键、宿主方言、映射与范围消费四条件同时成立 | 3项：C1–C4 逐条取证／5 例自测含"变更提示行不算消费方"／排除自映射与测试文件 | 实现+单元验证（13.104） | 历史 |

#### C8.1.2 02-plugin-dynamic-mount（5 项；delta `plugin-mounting` 3 Requirement / 9 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 02.R1 | builtin／宿主贡献／禁用／替换／待恢复五态只在同一个声明入口解析，丢失消费者的义务不被静默移除 | 5项：五态逐态引 file:line／白名单 3 文件／三锚／卸载欠账不得假称已存在／与 12.R3 联合交接 | 实现+单元验证（13.104） | 历史 |
| 2.1 | 默认启动即挂 builtin 目录 7 项，装配根不含特性名 | 3项：目录一行一插件带 scope／prepare→run 两趟／宿主遍历回填 driver/settleDriver | 实现+单元验证（13.104） | 历史 |
| 2.2 | 宿主文档可点名、可禁用、可承载宿主贡献实例；未注册名与域内置名具名拒绝 | 4项：`hostAgent.Plugins`+`mapDocument`／范围消费 spec.Plugins／缺省全挂·`-name` 禁用·幂等点亮／空名与 run 级内置名拒绝并报作用域 | 实现+单元验证（13.104） | 历史 |
| 2.3 | 七处硬编码构造清零而管线顺序不变 | 3项：删三文件 7 处具名构造与单成员/复合字面量 append／contributed→run→agent 顺序不变／更名避免撞 01 判据 | 实现+单元验证（13.104） | 历史 |
| 2.4 | 行为保真，并把未交付的压缩代际重建与条件注册显式登记而非隐去 | 4项：共同门 build+三包测试／该两处本波不交付须在 design 显式登记／部署 yaml 无需补声明（缺省全挂）／红项归因实测非本域 | 实现+单元验证（13.104） | 历史 |

#### C8.1.3 03-compression-configurable（9 项；delta `compression-pipeline` 2 Requirement / 12 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 03.R1 | 同一压缩请求不再产出两个产物；正常执行与重启共用同一处理进度 | 3项：先核"同请求多产物"是否违现行规格／归因不由本地绿或文件在 main 推定／正+负+消费者锚 `-race -count=10` | 实现+单元验证（13.104） | 历史 |
| 03.R2 | 压缩常驻态与快照构建量随未完量而非已结历史；乱序完成不丢不重 | 4项：三成本面（构建遍历/分配、常驻项数、序列化字节）／乱序未完保留／已答按身份读链、不以永久累积换绿／走序依赖实测（full 20/0·anchored 19/1·fallback 0/20） | 实现+单元验证（13.104） | 历史 |
| 03.R3 | 预算/租约/缺省/模型摘要与四类降级在真实宿主入口逐项成立 | 3项：原 3.1–3.4 逐项对账／三锚（核心声明＋降级负锚＋宿主消费者）／独立审须核宿主预算入口，不以核心声明路径替代 | 实现+单元验证（13.104） | 历史 |
| 03.R4 | 宿主 YAML 的 `summary_budget_bytes` 经 Boot 真实改变落链摘要长度 | 3项：`hostAgent`+`mapDocument` 原样透传（宿主不填默认）／三锚含 `Boot→loadHostDocument→mapDocument→真实压缩`／缺省仍归 `normalizeFoldWindow` | 实现+单元验证（13.104） | 历史 |
| 03.R5 | 压缩消费的两类依赖（读源/产物账）拆为两个窄口，行为零变化且概念预算不抬基线 | 3项：两接口各 2 方法／构造调用方级联（含替身 embed）／`check_concept_budget` error 消失且 baseline 未抬 | 实现+单元验证（13.104） | 历史 |
| 3.1 | 摘要预算进声明面：声明值原样、缺省 2048、拼错键被严格解码具名拒绝 | 3项：`AgentSpec`+`agentKeys`+`countField`+`defaults`／`FoldWindow` 承载与零值补缺省／拼错键即红 | 实现+单元验证（13.104） | 历史 |
| 3.2 | 摘要界不再有字面硬界：界经 plan 承载，提示词与裁剪同源，golden 重分类 | 4项：`AgeFoldPlan`+`summaryBudgetOf`（与 threshold 同形）／三处摘要面去字面 2048／`window_bounds.json` 新增两键并改写旧 reason／wiki 叙述同批 | 实现+单元验证（13.104） | 历史 |
| 3.3 | 摘要请求经租约（无租约回落原生模型）并携带声明采样；缺模型仍具名失败 | 3项：`leaseOf`+`withHostModelParams`／`ErrNoSummaryModel` 判据不放宽／provider 失败与终态非 completed 的降级口径不变 | 实现+单元验证（13.104） | 历史 |
| 3.4 | 全量消费者枚举与四包门禁；回退后"界不可配置"症状复红 | 3项：五层消费者（声明/指纹/折叠窗/执行/宿主文档）逐一回答／`AgeFoldPlan` 三字段解析同形／四包分号分隔执行＋回退复红 | 实现+单元验证（13.104） | 历史 |

#### C8.1.4 04-midband-ticket-recall（9 项；delta `event-key-visibility` 3 Requirement / 12 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 04.R3 | 同一通知身份在裸正文与带票据两种呈现下重复计数一致；别的调用不混入、真缺失仍报缺 | 3项：身份判定收成单一定义（先去票据再判四字段）／答复路径与计数路径同源／不新建会被忽略的测试文件 | 实现+单元验证（13.104） | 历史 |
| 04.R0 | 预算验收与另一变更的启动门之间不再互相等待归档 | 3项：给出排序＋两份启动条件同步／保留两变更独立身份、全面身份迁移单列不得并入／消费者锚＝预检入口读同一依赖图后拒绝未就绪派发 | 实现+单元验证（13.104） | 目标 |
| 04.R1 | 用已到模型的真实请求定位并修复票据呈现漏识别 | 3项：引用 09/design §八 两 CI 行（CI-4/CI-1，绑 `9547faca`+content_hash）／白名单 4 文件／拟新增识别锚与"别的调用材料"拒绝锚 | 实现+单元验证（13.104） | 历史 |
| 04.R2 | 呈现单一拥有者与全面无前缀改造仍归独立变更；双票据与预算未计量不得隐藏为已完成 | 3项：原 4.1–4.5＋S3＋召回工具全部消费者对账／双票据与预算未计量明记未完成／共享 request 实现不在本叶私改 | 实现+单元验证（13.104） | 目标 |
| 4.1 | 中段折叠产物逐行携带**被折条目自己**的原事件键票据（非合成键） | 3项：行首仍是来源锚／键取自被折过程条目的键／产物正文 MUST NOT 变 LLM 文本（类型门不动） | 实现+单元验证（13.104） | 历史 |
| 4.2 | 票据只有一种形态；已知票据可按 scope 唤回原文；降级绝不产半张票据 | 3项：单一生成点并导出／整行读法（旧形态与"只有锚无票据"仍答 missing）／降级梯按整行边界截断 | 实现+单元验证（13.104） | 历史 |
| 4.3 | 呈现在为已提交事实前置票据的同一处保持幂等：已带票据一字不改，产物条目不加合成票据 | 3项：已以票据开头即跳过／合成键产物不加／MUST NOT 重建回调式注入或按位置匹配消息 | 实现+单元验证（13.104） | 历史 |
| 4.4 | 多轮折叠后模型仍能**发现**该事实的存在（可发现性，不是"已知key才可读"），并凭票据自主取回原文（原症状的直译） | 3项：首跑必红且附非空转证明（折条数/请求数/前提可读）／回退前两条任一必红／消费者锚走正式请求入口＋真实召回 | 实现+单元验证（13.104） | 历史 |
| 4.5 | 票据相关六个函数全部调用方枚举、无漏消费者、无旧接缝复活 | 3项：全量 grep 枚举（含外部通知形态一致性只读核）／旧注入接缝仍零命中／build＋四包测试门 | 实现+单元验证（13.104） | 历史 |

#### C8.1.5 05-task-custody（8 项；delta `checkpoint-state` 1 + `session-checkpoint-material` 1 Requirement / 18 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 1.1 | 任务切片携三族收据（已消费/尝试/具名拒绝），空集显式而非缺键，异任务收据具名拒绝 | 3项：追加三字段不用 omitempty／wire 类型落 `runtime/task`（state→task 单向）／收据身份校验沿用 mutation 先例 | 实现+单元验证（12.105） | 历史 |
| 1.2 | 任务账户恢复改读切片（覆盖段零前缀读、尾段按身份读），删独立全扫派生而欠账恒等式不变 | 3项：抽出同一记录应用函数供覆盖段/尾段/运行期共用／欠账恒等式由统一入口维护／既有兜底读锚保留不改断言 | 实现+单元验证（12.105） | 历史 |
| 1.3 | 账户入表后移到切面解出之后、其余切片恢复之前；无切面走全链读兜底并留可检索日志 | 3项：构造只建空表＋boot 走空表+入表两段／信箱注册顺序不动／尾段读失败具名拒 boot | 实现+单元验证（12.105） | 历史 |
| 1.4 | 多代多任务下任务域加载量随存活账而非历史终态任务数 | 3项：真量取键与字节（非正文计数代理量）／先写就红·修复转绿·完整回滚必红／拒读错误路径不记度量 | 实现+单元验证（12.105） | 目标 |
| 05.R4 | 未预装已结任务时，旧信箱载荷仍按身份判已消费；不以恢复预装全历史换正确性 | 3项：先复现两次生成的真实切点／按身份读历史仍已结、真未结仍欠账／消费者锚归另一叶单一拥有者 | 实现+单元验证（12.105） | 历史 |
| 05.R1 | 任务域设计门：有效集、判重身份保留边界、乱序完成、与事实提交的衔接四问各有判据+反例+命令 | 3项：四问落 design 且与另两域合并成**一张**判据表／只读生产代码，禁写主 spec/门/golden/账本／候选未择定前不落地 | 实现+单元验证（12.105） | 历史 |
| 05.R2 | 收据成本降为"该任务自己的收据量"，且恢复与全扫基线在四面逐项相等 | 3项：按任务键索引／"未陈述"与"陈述空"必须可区分（否则升级后首次 boot 重新欠账）／MUST NOT 用 TTL 删未完义务或删三族 | 实现+单元验证（12.105） | 目标 |
| 05.R3 | 尾谓词三态正确：无框架检查点→走完整恢复；有检查点无更新→合法空集；有更新→精确尾键集 | 3项：按类型与空会话识别、不按键拼写／删"永不为空集"旧要求／与退位删除有顺序耦合（谓词须先于或同批） | 实现+单元验证（12.105） | 历史 |

#### C8.1.6 06-bounded-open-index（12 项；delta `bounded-recovery` 8 Requirement / 11 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 06.R3.a | 旧键前驱定位计完整成本轴（最新记录→目标键→前驱），不只量后一段 | 2项：正式历史查询路径同时计入两段／若需引擎接口先枚举既有可寻址能力并冻结最小共享面 | 实现+单元验证（06.105） | 历史 |
| 06.R5.a | 已删机制的旧屏障锚逐一映射到现行写入/关闭路径，阶段计量不从总屏障次数反推 | 2项：在 09.R2 里分别直接计量／结论为无需生产修改（读引擎自身状态即可命名阶段） | 实现+单元验证（06.105） | 历史 |
| 2.1 | 引擎自持索引切片＋派生加载，不从检查点派生（避循环依赖） | 3项：切面偏移＋存活键集跨度落盘／未启用时不整份重放／派生加载先独立成立才谈响亮失败 | 实现+单元验证（06.105） | 历史 |
| 2.2 | 开档只补切面之后窗口；撕裂尾语义不变；切面越过文件尾具名拒绝开档 | 3项：起始偏移由 0 改切面／序表只留存活键集+尾键并按逆帧序去重反写／截断语义不变 | 实现+单元验证（06.105） | 历史 |
| 2.3 | 静默跳页改为响亮失败：未持有时具名拒绝而非返回空值 | 2项：该分支改具名错误并与三处同口径／幸存者重建不改（已具名且凭正文合法重建） | 实现+单元验证（06.105） | 历史 |
| 2.4 | 索引规模不随历史增长；多代 boot 的索引条目有界 | 3项：点名的有界锚**本叶实测仍不存在**／实测读数 [0 43 94 145 196]→[0 2 2 2 2]／分母逐代增长 | 实现+单元验证（06.105） | 历史 |
| 2.5 | 五个读面函数的调用方全部枚举，保序两钉与计数断言零改 | 3项：五个函数逐一／grep 零漏消费者／build/test 不与 commit 串联 | 实现+单元验证（06.105） | 历史 |
| 06.R1 | 成本与引擎能力设计门：分阶段计量面、五问结论、引擎候选剖面与取舍 | 3项：七行表含必记量"实读键对数"／五问逐条答／只读生产代码，禁改恢复行为 | 实现+单元验证（06.105） | 历史 |
| 06.R2 | 索引维护下沉为引擎职责，不把退位重写当唯一触发 | 3项：SHALL NOT 以退位重写为唯一落盘点／加载前缀读按门取舍／不新增部署可变开关 | 实现+单元验证（06.105） | 历史 |
| 06.R3 | 点查询与尾查询不随历史增长（走查 API 有真正的早停出口） | 3项：早停出口取代哨兵／轨迹查询局部密度须量出并声明其轴／无法收窄须具名上报 | 实现+单元验证（06.105） | 历史 |
| 06.R4 | store 去物化（座位键一次点读、窗口元数据不常驻）且托管 boot 的行为可观测 | 3项：座位键与事件族同帧写入、开档一次点读／窗口元数据按需读／座位不可用具名拒绝 | 实现+单元验证（06.105） | 历史 |
| 06.R5 | 屏障预算覆盖检查点与关闭路径（症状锚转绿的必要条件） | 3项：重述零回放一次定宽（frame 7→3、closing 9→5）／零字节 flush 不取屏障对／绝对成本回不到量级须具名上报，不得以比值改善冒充达标 | 实现+单元验证（06.105） | 历史 |

#### C8.1.7 07-governance-custody（9 项；delta `checkpoint-state` 1 + `session-checkpoint-material` 1 Requirement / 15 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 3.1 | 治理收编为一个状态切片，携两流尾序号与审批态；快照纯读不阻塞 | 3项：实现三方法且快照不 flush/commit/等自身锁／恢复后派生索引逐项还原／经注册表注册 | 实现+单元验证（12.105） | 历史 |
| 3.2 | 治理恢复读切片+尾增量，消除全流双扫；切片缺失/损坏具名拒绝而非把尾序号静默清零 | 3项：缺失不得返回 0（否则重铸号覆盖已存储事件）／读切片结果与全扫逐项一致／恢复路径全流扫描计数为 0 | 实现+单元验证（12.105） | 历史 |
| 3.3 | 生产接线：账本/目标挂到治理并注册切片；审批簿重建改读切片而非目录全扫 | 3项：真实宿主 boot 后切片在状态接缝注册且账本/目标非空／审批簿 boot 不走目录全扫／生产调用方 ≥1（测试手工补接线不算） | 实现+单元验证（12.105） | 历史 |
| 3.4 | 三种缺口各有可红负锚：全扫回归、尾序号重置、真实宿主重启读切片 | 3项：回退全扫→有界恢复锚红／静默清零→序号连续性锚红／重启审批态与运行期逐项一致 | 实现+单元验证（12.105） | 历史 |
| 07.R1 | 治理域设计门：有效态、审批有效期跨重启、历史查询边界三题有判据与反例 | 3项：三题判据+反例＋29 处测试消费点与 3 处编译断言的处置清单／判据须区分"原身份可裁决"与"新身份 park"／与另两域共用一张判据表 | 实现+单元验证（12.105） | 历史 |
| 07.R2 | 治理有效态有界于开放项+尾；历史仍按身份可读；运行与恢复同一 fold | 4项：已决裁决权威取链、不取长期常驻切面／19 位键与尾序号不重置、未应用事实不被覆盖／禁删历史决定·禁 TTL 删义务·禁把计数改答尾序号／与另两域同文件须串行 | 实现+单元验证（12.105） | 目标 |
| 07.R2.1 | 已决审批按身份读链，快照/内存只留开放义务 | 3项：已结历史递增而开放量固定（快照/内存/按身份读回三面）／有效期前/到期/之后与别名四腿，不把"暂存新请求"当原身份存活／撤去文件镜像后原请求身份仍读到链上裁决 | 实现+单元验证（12.105） | 历史 |
| 07.R2.2 | 目标载荷只留在飞目标与序号；历史查询不静默变短，尾号不冒充条数 | 3项：历史查询三方法与列表逐项对账原 29 消费点／正锚核恢复后声明不重号且活动态与当前一致／不以"仅剩在飞项"替代原历史断言 | 实现+单元验证（12.105） | 历史 |
| 07.R2.3 | 审批绑定只恢复有效态+必要尾部；历史请求/裁决读取归索取者，链读失败不铸新请求 | 3项：三面读数（扫描/解码/分配、常驻项、切片字节）／链读失败具名拒绝且保留原义务／不得把全扫搬到首次判定宣称完成 | 实现+单元验证（12.105） | 历史 |

#### C8.1.8 08-fact-retirement（16 项；delta `storage-lifecycle-completion` 2 Requirement / 7 Scenario）

**读法**：本域由「建退位」反转为「删退位」（用户裁定，见本域 tasks 页首反转声明与顶层 design D6.13 勘误）。`6.1–6.5` 按其字面描述的是**已裁撤机制**，其关闭凭据是**删除证据**（08.R3/R4），不是实现该机制；`7.3–7.6` 的结论是"证伪、不改"。

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 08.R5 | 外部转换要求源离线；执行中源变化或目标半成品时不得报告可切换 | 3项：整源盖戳（长度+修改时）而非逐日志自校验／确定性同步点而非睡眠碰撞／干跑源零写、转换后独立开档逐身份/字节/提交序核验 | 实现+单元验证（06.105） | 历史 |
| 08.R3 | 退位分层机制与其测试整批删除，而已提交事实的可读性不下降 | 3项：按移除清单删约 25 个符号／3 个账本测试+1 历史文件整删、闸文件保留两枚锁锚但改名／删前取含已搬事实的根经外部脚本折入新根并按身份读回原字节 | 实现+单元验证（06.105） | 历史 |
| 08.R4 | 切面机制删除，并提供干跑默认、取写权恢复、按原提交序折入新根的外部折回工具 | 3项：删切面族与随之失效者／工具+升级说明记录运维前置与 file 档后果／S4 必须在 S3 之后（切面最后调用者是搬迁） | 实现+单元验证（06.105） | 历史 |
| 6.1 | （已裁撤机制）单一退位能力+全局提交序+原文族同批搬 | 3项：三方法由单一原子能力替代、不建分页形态／枚举/分页负锚随之一并退出验收面／配置开关偏离已具名申报 | 实现+单元验证（06.105） | 历史 |
| 6.2 | （已裁撤机制）排他安静点＝引擎独占＋装配侧停写准入 | 2项：未采用读写锁闸（同提交单元重入会自锁，实测死锁）／"重排未完成不进入交换"由先后顺序保证、无独立负锚 | 实现+单元验证（06.105） | 历史 |
| 6.3 | （已裁撤机制）搬迁不留活引用＋按身份透明回冷＋冷层损坏具名拒绝 | 3项：热命中/热未命中回冷/两层皆无三态／族拆在拷贝校验处具名拒绝／同一身份只陈述一次 | 实现+单元验证（06.105） | 历史 |
| 6.4 | 生命周期/保留/守卫接线：判据改为链可达＋义务租约 | 3项：龄期与容量 MUST NOT 单独触发搬迁／防第二判据源与既有存活键集并存／生产接线零调用即未完成 | 实现+单元验证（06.105） | 目标 |
| 6.5 | （已裁撤机制）日志真实字节有界于存活键集+尾，并有回滚必红证据 | 3项：量真实文件字节非正文计数／回滚两次红证据／外部真实长跑未跑（禁区） | 实现+单元验证（06.105） | 历史 |
| 7.1 | 日志重写后不再留下悬空句柄（重开早于切面写入） | 2项：失败路径落在"有效句柄+整读日志"／锚钉住句柄跟到它离开的那份日志 | 实现+单元验证（06.105） | 历史 |
| 7.2 | 搬迁成功后驻留集按已搬键逐个摘除，两进程视图一致 | 2项：锚钉住驻留集不再答复已搬键／回滚该摘除即红 | 实现+单元验证（06.105） | 历史 |
| 7.3 | 原店冷视图陈旧＝证伪，不改 | 2项：热层全量载入且从不清减、待读键必在载入面／临时探针已删 | 实现+单元验证（06.105） | 驳回 |
| 7.4 | 提交序号播种未含已搬序号＝证伪，不改 | 2项：计数与最大提交号密集相等故不落在冷序之下／不为病理性缺失窗口加补偿 | 实现+单元验证（06.105） | 驳回 |
| 7.5 | 事件枚举非全局提交序＝证伪，不改 | 2项：该函数契约即索引序、全局归并在另一函数／三处消费者均不依赖其全局序 | 实现+单元验证（06.105） | 驳回 |
| 7.6 | 搬迁不重置窗口/事件计数/读缓存＝证伪，不改 | 2项：事件计数语义即全量历史、窗口元数据字段无读者／读缓存有界且命中的是不可变字节 | 实现+单元验证（06.105） | 驳回 |
| 08.R2 | 以真实消费者判定保护能力的留删，不按 keep 成员关系判结构必要性 | 3项：每条原保护承诺的真实消费者+现有承担者+缺失/重复证据／生产文件只读／已证冗余交清理叶 | 实现+单元验证（06.105） | 历史 |
| 08.R1 | 搬迁单位对齐整事实提交后的键集，孤儿原文有具名处置，回退后热层只追加 | 3项：族拆不再是可构造状态／孤儿原文二选一并报告单列，禁止"留热算已搬迁"第三态／回退后重复提交仍判已存在 | 实现+单元验证（06.105） | 历史 |

#### C8.1.9 09-acceptance-real-measure（11 项；delta `bounded-recovery` 8 Requirement / 20 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 4.1 | 四个量取自一次真实 boot（开档读入字节、常驻索引规模、耐久加载扫描数、墙钟） | 3项：索引规模读常驻条目而非全史标量／读入字节不复用追加偏移／探针取不到量即具名失败 | 实现+单元验证（06.105） | 历史 |
| 4.2 | 多代+多任务夹具，任务事实经正式注册表落耐久族 | 3项：5 代重启骨架+结清任务／分母与全史对照同源／"逐代增长"自证口径改正以免与另一叶互斥 | 实现+单元验证（06.105） | 历史 |
| 4.3 | 启动成本跨代平坦的症状锚（三确定量硬判定+墙钟宽松上界） | 3项：末代/第二代比值有界且随代下降／四量全序列入日志／正文计数降为观测量不作判据 | 实现+单元验证（06.105） | 目标 |
| 4.4 | 全史读回归的负锚（boot 四量×2 小于全史对照四量） | 3项：对照走正式入口／完整回滚必红／**已实测该验收面在本二进制内不可判定**（开档时刻三量恒等）且所指定文件属他变更占用面 | 实现+单元验证（06.105） | 代码反例 |
| 09.R1 | 固定某个提交的原始 CI 失败清单（sha/作业/命令与选择器/退出码/首条失败原文） | 3项：三命令输出与表逐行相等／超时与断言红不得并成一类／禁重跑或触发工作流，以汇报或计数替代首因即红 | 实现+单元验证（06.105） | 历史 |
| 09.R2a | 独立阶段采样：启动/整代/提交/点读/目录维护/重绑定各自独立起止点 | 3项：安全校验单列、未归因保留为未归因不命名／八种注入逐条具名拒（重复记账、漏阶段）／零恢复行为变更、只读计量 | 实现+单元验证（06.105） | 历史 |
| 09.R2b | 墙钟校准＋可失败性能反例＋原 1.3× 判决并列 | 3项：十轮原始阶段与总耗时+噪声分布／负面读数照录（未归因残项 1.624–3.366× 不豁免、total 2/10 越界）／校准是证据门不是另取宽数的授权 | 实现+单元验证（06.105） | 历史 |
| 09.R3 | 同候选完整矩阵：十短、六长、一次变异全跑，并留终局与同条件对照 | 3项：每份记录受测模块与闭包指纹/argv/退出码/产物路径／未启用 soak 档位须列未执行／旧异机增长样本不作主干对照 | F2矩阵（承接13.105编排；触发 MATRIX＋PRE＋EVID） | 目标 |
| 09.R3a | 某长跑组合的崩溃一致性诊断与最小修（判据是 torn 漏项即拒零长非法帧） | 3项：根因已实测（零长帧被判为合法帧）／最小修一处判定+一个只读辅助／停线：零字节尾来源未证、该组合×3 绿未取得 | 实现+单元验证（06.105） | 历史 |
| 09.R3b | 重启代"整代输入未驱动"根因修复：陈旧产物不得被父侧当成本代自报 | 3项：落点先登记再改（两处）／修面仅两处+域两文件、产品域零改／不放宽既有精确计数与恰一次断言 | 实现+单元验证（06.105） | 历史 |
| 09.R4 | 原症状直译锚重定：长跑**每代总成本**平坦（启动平坦不可替代它） | 3项：深度按实测推导（84/168/252→取 252）／界 1.3 为候选、未沿用未校准的 2×／零冷计量引用＋三条控制案（平坦接受/线性具名拒/同条件离散在界内） | 实现+单元验证（06.105） | 历史 |

#### C8.1.10 10-contract-sync-dead-code（40 项；delta `code-documentation` 8 Requirement / 24 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 5.1.1 | 压缩综述条款与实现同向：机械读法为底、持模型时有界语义综述、失败降级回机械 | 3项：三处条款不动／类型闸只放综述产品／负锚以 grep 命中数取证 | 实现+单元验证（13.105） | 历史 |
| 5.1.2 | 历史 compaction 冷启动只读重建整条移除＋幂等条款去掉被删函数分句 | 2项：与删函数同批原子、不得先删后改／负锚 grep 在被删符号与最小键上主规格零残留 | 实现+单元验证（13.105） | 目标 |
| 5.1.3 | 两个失活能力整篇标 superseded | 3项：体例照既有 superseded／受概念棘轮检查／概念锚改绑前不得退役 | 实现+单元验证（13.105） | 目标 |
| 5.1.4 | 两份重复的装配条款收口到单一 owner，另一份标 superseded 指回（不双重立法） | 3项：死符号条款退役、现行接线转正／替代方案不可执行（被点名符号全树零命中）／规格卫生无新增违例 | 实现+单元验证（13.105） | 目标 |
| 5.1.5 | 四篇 legacy 条款退役，审查项对象改写为现行并发面与关系存储 | 3项：脚手架接线归另一域、本行只退役死条款／改写而非词替换／死符号零命中 | 实现+单元验证（13.105） | 目标 |
| 5.1.6 | 8 篇已 superseded 只核不重写，其中一篇附勘误句 | 3项：退役事实部分不改写／勘误句"持模型时由压缩插件产出有界语义综述"／解除条件＝导出面棘轮按实测处置不改基线 | 实现+单元验证（13.105） | 历史 |
| 5.1.7 | 主规格结构与门收口：有 Purpose、每条要求含 SHALL 与场景、主规格不出现 delta 语法 | 3项：机制细节落场景或 wiki 不落要求正文／stale 基线条目只在复核收紧后清／负锚 grep delta 标题零命中 | 实现+单元验证（13.105） | 历史 |
| 5.1.8 | 词汇登记与同步；随机制删除失效的两项借用词从文档清除而另一义项保留 | 2项：存储义"退位"清除、术语表岗位本义保留／能力名与域目录名保留 | 实现+单元验证（13.105） | 目标 |
| 5.2.1 | 持久真相 golden 的分区键形 reason 与实现同向，前缀值不动 | 2项：门只比值故负锚必须用 grep／消费者锚为键构造测试 | 实现+单元验证（13.105） | 目标 |
| 5.2.2 | 概念棘轮 note 追加勘误句，三个棘轮数不改写 | 2项：只增勘误句／若改导出面须实测另抬并记理由，不得静默改数 | 实现+单元验证（13.105） | 目标 |
| 5.3.1 | 删除两个死生产者及其两条编译期断言 | 3项：与规格移除同批／负锚全仓含测试零命中／消费者锚证明现行恢复入口不依赖被删函数 | 实现+单元验证（13.105） | 目标 |
| 5.3.2 | 级联回卷伴生错误与类型；三个明确保留项逐项有活消费方 | 2项：保留项 KEEP>0 实证／级联回卷不漏伴生引用 | 实现+单元验证（13.105） | 目标 |
| 5.3.3 | 测试锚先迁移后删函数，不留无覆盖窗口 | 3项：8 个非冲突文件逐条按处置表／孪生锚已存在故死锚随机制退役／独有子断言随类型一并退役且不得声称判据全等价 | 实现+单元验证（13.105） | 目标 |
| 5.3.4 | 4 个被占用文件的锚迁移（待另一变更落地后追加） | 2项：必须保留的现行锚只改建投影手法、断言逐字保留／不重折腿迁到现行恢复入口 | 实现+单元验证（13.105） | 目标 |
| 5.3.5 | 兼容面退役（生产调用方为 0，治理绑定实走正式座位路径） | 2项：两条形状断言删除且不新增替身断言／生产绑定报错即中止 boot | 实现+单元验证（13.105） | 历史 |
| 5.4.1 | 六个 Material 兼容端口的消费者审计（生产/契约示例/测试三分） | 3项：覆盖全部分派点不止直接调用方／先审计不动代码／六端口常量是活分派键 | 实现+单元验证（13.105） | 目标 |
| 5.4.2 | 裁定后处置甲（退役兼容派生）或乙（保留兼容读法），六端口常量两案均不退役 | 2项：设计级分叉须上报裁定／甲案须契约示例同批补声明 | 实现+单元验证（13.105） | 建议 |
| 5.5 | 域级复跑凭据（规格门/build/多包测试/独立模块 smoke/六个 Python 门） | 2项：各命令 exit 0／独立模块 smoke 需授权 | F2矩阵（承接13.105编排；触发 PRE） | 目标 |
| 5.6 | 上报项：嵌套子变更不被 CLI 直接寻址，其 delta 的严格校验只能在合入或归档时生效 | 2项：实测变更枚举只发现顶层 change、按名寻址报未知项／父变更编排规格已归位 | 13.106审（审者须复现寻址事实；触发 `openspec validate --changes --strict --json`） | 历史 |
| 5.7.1 | 带归属规则反向纠正：实现是带晋升（中带折票据段、老带整段入综述、近期窗口原样保留） | 2项：进行中段与近期窗口保持原样／综述正文改模型产出并有机械降级 | 实现+单元验证（13.105） | 历史 |
| 5.7.2 | 段龄阶梯被误写成折叠判据：改写为"保线性三带，MUST NOT 决定带归属" | 2项：负锚 grep 该句零命中／正锚钉住阶梯是共享读法 | 实现+单元验证（13.105） | 历史 |
| 5.7.3 | 事件键可见性整篇重写为"呈现面注入＋中带过程票据可自主召回"两条要求 | 3项：补三场景（空呈现不单独呈现/呈现不写回投影/降级绝不半张票据）／三锚名冻结／旧回调式接缝零命中 | 实现+单元验证（13.105） | 历史 |
| 5.7.4 | 会话检查点材料补任务托管与治理托管两面并纠正键形为分区+槽位双段 | 3项：退位面随删除不再补写（索引读侧契约逐字未动）／键形改为双段／两面条款补齐 | 实现+单元验证（13.105） | 历史 |
| 5.7.5 | 压缩管线补摘要预算与模型产出面（三条降级） | 2项：不变量措辞、机制细节落场景／正锚为严格校验加三枚测试 | 实现+单元验证（13.105） | 历史 |
| 5.7.6 | 死码复测（只复测不新增删除） | 3项：兼容面在 Go 树零命中而派生文档仍含／中带机械摘要生产者零消费函数集为空／派生文档重生成归文档叶 | 实现+单元验证（13.105） | 历史 |
| 10.R1 | 目标 P0–P8 与完成账双向对账：子要求↔承载叶↔锚全名↔**实际执行该锚的 CI 作业**↔勾选状态 | 3项：双向表落 design／门绿不等于被执行（门只比作业名）故须逐条人工裁定／九条变异真状态是"已登记未证杀" | 实现+单元验证（13.105） | 历史 |
| 10.R2 | 为计划余项逐条制定处置表（保留/删除/接线/替代＋依据＋实施叶＋精确写面） | 3项：29 行逐行覆盖旧编号与 R1/R2/R4／负锚"接线行须有非测试非编译断言的生产调用方"结论为接线行 0／blocked 行不得计入完成 | 实现+单元验证（13.105） | 历史 |
| 10.R4 | 实施已批准清理，并按"能力＋要求标题"合并所有域 delta 后写主规格 | 3项：同一能力跨域重复修改须唯一合并结果、冲突拒绝不取最后一份／已失效的历史重命名不执行／不新建不存在的能力目录以免第二真相 | 实现+单元验证（13.105） | 历史 |
| 10.R3 | 交接面与归档前验证：呈现前缀唯一 owner、配置耦合规则、源码与主规格同步、归档前拒绝闭证 | 3项：四门同时成立方可归档／负锚模拟证据缺项须被拒／不得提前归档 | F4外部实效/交付（承接13.105→F4；触发 ARCH + `openspec archive`） | 目标 |
| 10.R5 | 退位能力整体移除并同步规格（7 要求/31 场景），其余 8 篇逐条改写，五值回写 | 3项：不得跳过规格同步或校验放行／票据退役子项已转他叶／残余死条款本轮补齐 | 实现+单元验证（13.105） | 历史 |
| 10.R5b | 某能力整篇取证后判方向并整篇 superseded | 3项：符号与扩展族零命中⇒整体退役／改写清单与行数变化入账／文档预算只降锁步 | 实现+单元验证（13.105） | 历史 |
| 10.R5c | 单段死判据改写为现行窗口判据 | 2项：墓碑三族与前缀零命中⇒判删／等长行内替换 | 实现+单元验证（13.105） | 历史 |
| 10.R5d | 余下死条款逐条留删（计数要求＋两场景、年龄分句等） | 2项：计数条款判"活半"（链自述一次读出、否则索引枚举重建）／净删行数入账 | 实现+单元验证（13.105） | 历史 |
| 10.R5e | 四篇死规格逐条留删（一篇整退役，一篇判"部分活"故只删可替部分） | 3项：删断言不删无替代承载的活义务／行数变化入账／两分句换成现行可判场景 | 实现+单元验证（13.105） | 历史 |
| 10.R6 | 文档与词汇同步（多篇 openspec 文档＋wiki 陈旧叙述＋术语＋生成物重生成） | 3项：术语表岗位本义保留／不同义项排除表随报告给出／文档预算不收紧而红项逐数溯源到外来未跟踪面 | 实现+单元验证（13.105） | 历史 |
| 10.R7 | 门与 golden 同步＋小残留清偿（叶格式统一、死构造器删除、措辞） | 3项：checkbox/叶数按实测分布记／对象已消失的旧文案只记勘误不删／两处新假绿形态只登记指针不重做 | 实现+单元验证（13.105） | 历史 |
| 10.R8 | 持久面共享三方法终态与剩余迁移项核验 | 3项：生产引用计数与文件枚举／旧声明与别名零残留且无第三同族端口／有一处保留（活消费者在装配层） | 实现+单元验证（13.105） | 历史 |
| 10.R9a | 枚举旧项真实消费者并冻结迁锚/实施表（逐符号按七类归属） | 3项：全部命中行归属、未覆盖 0、测试冒充生产 0／三种变异均实测拒绝（漏项/把测试当生产/按名误删保留项）／"死生产者门绿"不等于零生产死码 | 实现+单元验证（13.105） | 历史 |
| 10.R9b | 迁移投影锚后清理旧生产者家族；若发现活生产消费者则保留并重分配 | 3项：幂等/材料释放/分页/分区隔离须仍有正式入口锚／退役断言单列、不声称等价／正式入口确定性红故未勾停线 | 实现+单元验证（13.105） | 目标 |
| 10.R9c | 原文六个读入口逐项收口，按"接口满足面"而非仅调用点计数判定保留 | 3项：三个入口有生产或公开语义使用方故保留／删某入口会使运行时断言静默转假／六入口逐项读回原字节的正锚 | 实现+单元验证（13.105） | 历史 |

#### C8.1.11 11-atomic-fact-commit（4 项；delta `event-segment-store` 1 + `fact-vocabulary` 1 + `partition-local-storage-access` 1 = 3 Requirement / 17 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 11.R1 | 事实提交设计门四问（单存储根写者/整事实承载/结果分类/原规范等价）各有证据与条件分叉 | 3项：第一问实测证伪⇒须给出具名条件分叉与失效范围／第二、四问任一不成立即停、不得开工／与另一域共写测试文件须合单或串行 | 实现+单元验证（12.105） | 历史 |
| 11.R2 | 一条事实一次提交同时落地原文族与事件族，判重读与写在同一原子单元内 | 3项：五值结果（新增/已存在/冲突/不确定＋确定拒绝，确定零落地不得塞进任何一值）／领域等价必须早于引擎字节门／槽位与事件计数语义零改 | 实现+单元验证（12.105） | 历史 |
| 11.R3 | 原子性对每个档位要么成立要么显式拒绝；引擎内部半批有以"第 N 键失败"为注入点的负测 | 3项：逐键档多键批具名拒绝且零键落地（部署后果须批准，不得承诺可用）／能力面声明与实测行为一致／真实消费者三处 | 实现+单元验证（12.105） | 历史 |
| 11.R4 | 提交结果逐类确认落地保证；确定零落地不误报不确定；未知结果不按进程内索引判不存在 | 3项：删名字白名单改按接缝契约反演（只有"不确定"归不确定）／裁决读须独立开档而非进程内／缺外部 CLI 须报告未验、不用跳过销案 | 实现+单元验证（12.105） | 历史 |

#### C8.1.12 12-consumer-state-lifecycle（9 项；delta `config-hot-reload` 1 + `consumer-loop-convergence` 1 + `runtime-resource-ownership` 1 + `session-checkpoint-material` 4 = 7 Requirement / 28 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 12.R1 | 已提交未消费边界与插件生命周期四态（挂载/禁用/卸载/替换）各自"应交接什么、无承接者如何拒绝" | 3项：判据与反事实、证据表复跑补证、三候选择一并说明被否理由／全库"提交成功后返回 error 被吞"按三类语义单元枚举／卸载验收先纠正再实施 | 实现+单元验证（12.105） | 历史 |
| 12.R2 | 切面不覆盖未应用事实；失败具名且义务保留；运行与恢复走同一派生并保提交序 | 4项：全局拒切面是保守策略非唯一设计、影响须量化／不新建持久应用水位／不承诺无接收方的外部恰好一次／与另两域同文件须串行或合单 | 实现+单元验证（12.105） | 目标 |
| 12.R2.1 | 恢复按总提交序应用幸存与非幸存事实（两集合一个序） | 2项：前一集合内部排序不等于两集合总序／负锚为"恢复错误排序"的变异须红 | 实现+单元验证（12.105） | 历史 |
| 12.R2.2 | 覆盖已折叠旧输出的同身份重试不重开轮次、不倒退呈现范围、不重复生成 | 2项：判重依据不等同当前投影保留键／须先实证旧键已被折叠移出再经正式提交重试 | 实现+单元验证（12.105） | 历史 |
| 12.R2.3 | 真实重复生成切点调查；同一通知跨进程至多一条耐久收据，失败仍欠且具名 | 3项：逐切点身份轨迹＋最小红锚／两个独立进程受控终止后重投同一通知身份、每种切点有非空守卫／不以同进程重建冒充跨进程 | 实现+单元验证（12.105） | 历史 |
| 12.R2.4 | 边界读成本与全局拒切面影响可校准（空区间不等于零额外读取） | 2项：量无关会话受阻数量/时长、拒切面次数、热层与恢复读量／量成本时不得跳过失败事实或用 TTL 删义务 | 实现+单元验证（12.105） | 历史 |
| 12.R2.5 | 送达标记写失败后义务保留而投递不停（"具名但不停"不再算静默语义改变） | 3项：机制勘误——提交的是送达标记而非消费收据／四查非空守卫（注入确发生/义务仍欠/投递完成/重启恰一条）／两处变异均红且复原逐字节一致 | 实现+单元验证（12.105） | 历史 |
| 12.R2.6 | 链增量回归的根因修复（干净基线绿而现树红⇒候选引入） | 2项：须用隔离基线叠文件二分、禁写仓库 git／不得削弱断言或抬界 | 实现+单元验证（12.105） | 历史 |
| 12.R3 | 挂载/禁用/卸载/替换的义务交接：具名拒绝后原持有对象、切片名与未完义务全部保留 | 4项：先纠正与策略相反的旧守卫，不放宽为"已删未删均可"／空或已结切片允许正常卸载、替换保同名与权威／disposer 当前无生产调用方须先核实际入口／不新增挂点或第二注册体系 | 实现+单元验证（12.105） | 目标 |

#### C8.1.13 13-verified-completion（13 项；delta `architecture-guardrails` 6 Requirement / 27 Scenario）

| 编号 | 用户可见行为身份 | 正文子要求/Scenario 计数与要点 | 到期阶段 | 标记 |
|---|---|---|---|---|
| 13.R1 | 证据链设计门：三个既有引擎的真实结果消费者与拒绝面＋三条"假 killed"判据 | 2项：只读复现（无证据参数须非零且严格）／差距表落 design | 实现+单元验证（13.102；主承接13.106） | 历史 |
| 13.R2 | 完成证据绑定源树指纹、命令参数、环境、期望全集、实际计数>0、量名单位区间与有效性；手填判词改为消费运行产物 | 3项：白名单须含生产者及其自测（原漏）／环境记录面受阻于生产者行／手填判词一律不得作凭据 | 实现+单元验证（13.103；主承接13.106） | 目标 |
| 13.R2a | 绑定所选模块的真实本地依赖闭包（本地替换/工作区、前后指纹、工具身份） | 2项：独立模块也绑定其 go.mod 指向的根源码／同波锚漂移须重指（三条 stale） | 实现+单元验证（13.103） | 历史 |
| 13.R2b | 证据消费时按真实根重算目标树（单份与整包共用同一把尺） | 2项：缺绑定即具名拒绝／夹具须说出它描述的那棵树，不得放宽判据 | 实现+单元验证（13.103） | 历史 |
| 13.R2c | 校准旗标只随"编译了定义它们的测试二进制"的那次调用传递 | 3项：机理实测（同包不带标签同样失败）／自测例数入账／长跑真产物归矩阵叶 | 实现+单元验证（13.103） | 历史 |
| 13.R2d | 变异锚随被改代码重指；控制树的竞争豁免不得掩盖业务断言失败 | 3项：豁免仅在全部失败可归因已登记上游竞争时才生效／三夹具负锚／环境归因单列不混入判据 | 实现+单元验证（13.103） | 历史 |
| 13.R2e | 证据消费者闭合（seed/window 实际 argv、门工具身份、判词一致、完成度全量结构校验） | 2项：复用生产者单一来源不复制第二公式／四处孤儿各补消费面 | 实现+单元验证（13.103） | 历史 |
| 13.R2f | 某门锚变异重指到在位的规格正文句（不降级变异） | 2项：锚串在文件内恰一次命中（双证）／变异树内违规计数唯一增量 | 实现+单元验证（13.103） | 历史 |
| 13.R3b | 正式入口夹具与文档对账（嵌套夹具不得跳过完成度或伪造绿＋四处文档残留） | 2项：缺证据环境两次真跑均非零且计划门从未被调到／文档预算与链接数落在既有物理行内 | 实现+单元验证（13.103） | 历史 |
| 13.R3 | 公共拒绝入口与归档/合并接入：本地预检、必跑作业、受控归档、服务端合并 | 3项：接线＋归档拒绝入口＋六反例＋绕行反例／分支保护与必需检查属外部权限（实测主分支保护缺失）／远端未真跑 | F4外部实效/交付（承接13.103→F4；触发 ARCH + CI） | 目标 |
| 13.R3a | 正式完成度证据输入与失败传播：门默认真实消费已绑定目标树的变异产物 | 3项：变量未给/无产物各具名红并非零／红绿归属实测（删参数 6/51 红、移顺序 4/51 红）／整包完整性判据留在预检 | 实现+单元验证（13.103） | 历史 |
| 13.R4a | 变异全量真跑生成完成度运行证据（全部启用变异被杀、控制树绿、门自报 ok） | 2项：产物带提交/合同摘要/源码绑定/门工具身份／同一产物交完成度与整包双消费 | F2矩阵（承接13.105编排；触发全量变异真跑） | 历史 |
| 13.R4 | 域级证据与阶段完成边界的复验（设计门/绑定/正式入口正负/逐症状证据/未生效外部入口清单） | 3项：三门自测＋正式入口正负结果／依赖两单元回归与其他域实施／写面仅本域两文件与隔离证据目录 | 13.106审（触发 EVID＋LINT＋ARCH） | 目标 |

#### C8.1.14 覆盖与分布（可复算）

- **覆盖计数**：C8.1 表体编号行＝**151**（逐域 6/5/9/9/8/12/9/16/11/40/4/9/13，与 CARRYOVER §一逐域数量逐项相等）。
- **到期阶段分布**：`实现+单元验证` **144**；`13.106审` **2**（5.6、13.R4）；`F2矩阵` **3**（09.R3、5.5、13.R4a）；`F3终审` **0**；`F4外部实效/交付` **2**（10.R3、13.R3）。合计 151，其中**后置 7 项**（3+2+2），每项均已带承接叶与触发命令。
- **标记分布**：`历史` **116**；`目标` **29**；`驳回` **4**（7.3–7.6）；`代码反例` **1**（09 域 4.4）；`建议` **1**（5.4.2）；`实测` **0**。
- **`实测` 为 0 的理由（不是漏标）**：按顶层 D1，`实测` 指"正式入口、非空前提、期望/实际、原始退出码与源码绑定齐备"的**已复现缺陷**。本节是只读取证叶，未运行任何产品测试或构建，故没有任何一条旧编号达到该等级。本节用命令取得的**事实**（git 取数、sha256 比对、符号残留计数、锚存在性、资源探针）全部记在 C8.5，并在 C8.2.3 逐条挂到具体编号；它们只证明"该条所述某一性质当前是什么"，不证明该条已兑现，因此不升格为 `实测`。
- **`F3终审` 为 0 的理由**：F3 的实质是"整个候选的独立最终审"，其对象是全体叶的合并结果而非任何单条旧编号；单条旧编号不承载"最终审"这一性质，故不单独挂 F3。该阶段仍存在于顶层 tasks 的 F3，并接受 C8.1 全表作为其输入面。
- 复算命令：`python3 /tmp/c8101-verify/mapcheck.py stats design.md`（口径与输出见 C8.6）。

### C8.2 子要求核对（编号之外的实质要求）

结论先行：**编号层面无漏项**（151 编号两侧对称，见 C8.6 覆盖检查）；**正文子要求层面**发现 11 处需要具名承接或需要裁定的实质项，单列于 C8.2.3，未静默扩大为新的实现单。

#### C8.2.1 十个非活动域：编号之外仍有效的实质子要求

| 域 | 编号之外的实质子要求（仍属该行、无独立 checkbox） | 当前承接 |
|---|---|---|
| 01 | 每叶共同门（`go build`/`go test`/挂载门 exit 0）；触碰面只含 `scripts/gates/` 与 `golden/runtime/`（与任何特性代码不相交）；"门禁在现树红是设计状态"不得靠登记全量例外或降判据压掉；R1a 的候选依赖清单与"真树绿的三个先决面"；R1b 的共享 lint 由 13 集成 | 13.104 正文 + 13 域 lint 接线 |
| 02 | 每叶共同门三命令；触碰面明确**不改** `scripts/gates/`、`golden/`、`api/config/{decode,clone,fingerprint}.go`、部署 yaml；"不得把丢失消费者的义务静默移除" | 13.104 |
| 03 | "每叶修复后由非实施者独立只读审查三锚与受影响调用方"；03.R2 的四条语义拒绝项（不得用最大序号/前缀水位跳未完、不得用 TTL 抹义务、不得以保留缓存冒充清减）；03.R3 登记的三项偏差 | 13.104 + 13.106；三项偏差见 C8.2.3-4 |
| 04 | 合单原子与禁触面（不得触 `summary_gist.go`/`compress_consumer.go`/`api/config`）；S3 症状锚的写就与转绿归属；票据格式属共享面、新增导出面须先登记概念预算；4.5 的 grep 面 | 13.104 |
| 05 | 与另两域合单原子、S2 只消费不重定义；1.4 的"本轮不撤回验收面、改换夹具驱动路径"；每叶共同门 | 12.105 |
| 06 | 与 01/04 合单原子；2.1 的载体层与签名属共享面、设计冻结前不动工；索引规模口径裁定（常驻条目而非全史标量）；S2 只引用不重定义 | 06.105 |
| 07 | 与 boot 路径簇不相交的并行许可（前置 W1 绿）；承 `Stateful` 接缝签名不动；07.R1 与另两域**共用一张** G2 表（三域不得各写一版水位定义） | 12.105 |
| 08 | "红线贯穿每一行：搬迁/删除的是机制不是已提交事实，任何步骤不得使已提交事实不可读"；W3 波次与 W4 回归扫尾；"已知启动证据不是本域通过证据"；08.R3 的"明确不删"清单；08.R4 的级联遗漏须具名上报 | 06.105；"明确不删"清单见 C8.2.3-6 |
| 09 | 与 01/02 合单原子；W0 启动门（症状断言写就即红，不得为转绿放宽断言）；四量名冻结；"原四量语义保留，现出口若已变义则分列新量、不能沿旧名暗换判据" | 06.105 |
| 10 | W2 并行与前置 W1 绿；spec 转正待代码稳定（草案先写）；"build/test 与 commit 永不串联"；每处编辑成功后 grep 实证落盘；5.6 明写"本域不写父级文件" | 13.105 |

#### C8.2.2 三个活动域（原正文在 `history/7b0cde16-before-replan/changes/`）：编号之外的实质子要求

| 域 | 编号之外的实质子要求 | 当前承接 |
|---|---|---|
| 12 | 前置链 12.R1 →（G2 共用表）→ 12.R2 → 12.R3，"没有 R1 裁定就不派发 R2/R3 的实现叶，不按汇报解锁"；每叶共同门（build/vet 两命令 + 每枚冻结锚逐名命中>0，组合命令部分命中不算凭据）；"每叶修复后由非实施者独立只读审查"；跨会话/跨进程原验收保留（会话切面不得把全局账塞进去） | 12.105 + 12.106 |
| 06 | 与另两域合单原子；设计闸门（载体层属共享面）；`indexEntries` 口径已裁定且须同步四处；S2 只引用不重定义；"旧 2.1–2.5 不作前置（其状态待复验），本域当前完成面只看 R1/R2" | 06.105 |
| 13 | "新 Python 测试与标'拟新增'的 Go 测试均未建立未执行；现有 Go 锚本轮仅核过定义"；每个 `-k` 选择器预期恰好 1 个顶层用例、子场景全部非空执行；选择器/文件/消费者变化先重签，不以零匹配或早退计通过；命令从仓库根、独立 wechat 模块用 `go test -C examples/wechat-bot -tags smoke . ...`；旧 9/9 不自动成立 | 13.103/13.105/13.106 |

#### C8.2.3 新发现漏项（CARRYOVER 未点名、或点名了编号但无承接叶/无触发命令）

| # | 来源（域＋编号） | 实质项 | 为何算漏项 | 建议承接叶与触发 |
|---|---|---|---|---|
| 1 | 09.R3a | 三条停线：①零字节尾来源未证；②该组合本次红是**另一症状**（整代输入未驱动＋义务未结清＋收尾预算耗尽），活锁与损坏帧因果未定；③该组合×3 绿未取得 | 已到期缺陷，不得改标后置；CARRYOVER 只接 09 编号未接其停线项 | `06.105`（存储面）＋`12.105`（任务义务链）；触发＝该组合长跑 ×3 |
| 2 | 09.R4 | "修复后必绿"这一半未取证（反转退位后每代成本是否平坦） | 验收面只兑现一半，属已到期未完成 | `06.105`＋`F2矩阵`；触发＝每代成本锚在冻结候选上转绿 |
| 3 | 09 域 4.4 | 该条验收面在单一二进制内**不可判定**（boot 与全史对照走同一开档，三量恒等），且指定文件属他变更占用面 | 需重裁而非静默缩水；原验收面冻结 | `06.105` 出等价证明或改判据并记偏离；**不得追认缩水后照勾** |
| 4 | 03.R3 | 三项偏差：`docs/wiki/agent/event-flow.md` 与 `docs/wiki/platform/org-hot-reload.md` 两处叙述滞后；规格中一处 AND 子句无锚 | 三项都无独立叶、无命令 | `13.105`（文档）＋`13.104`（缺锚）；触发＝文档门与新增锚 |
| 5 | 10.R5b/R5c/R5d/R5e | 四叶各自登记的"级联发现（未改，超本叶写面，交主 agent 裁归属）"：同一主规格内另有多处死判据、某能力两场景被判死、审查标准篇"部分活"留⑦删⑧的分叉 | 各叶明写交归属但无人接 | `13.105`；触发＝逐条在合并输入生成前给出留删结论 |
| 6 | 08.R3 | "明确不删"清单：代际退役、`Retention.Retire`、`headless-retired`、cache-cold 测试、`migrate.go` 与分片布局拒绝 | 删除叶自身的负向义务，漏删即伤活义项 | `06.105`；触发＝该五类符号残留>0 且各有活消费者证据 |
| 7 | 11.R2 / 11.R3 | ①"确定拒绝"作为第五值请编排者复核；②逐键档多键拒绝的**部署后果**（该档事实层整体不可用）仍待批准 | 两处明写"待裁"且无归属 | 编排者裁定 → 写入 `12.105` 的签发记录；未批准前不得承诺该档可用 |
| 8 | 12.R3 / 02.2 | ①卸载 disposer 当前无生产调用方，"禁用/卸载"的用户可见承诺缺真实入口；②贡献实例名与 builtin 名同址时无法分别寻址；③reload 后插件集替换未单独立锚 | 三项均为"无叶、无命令"的未证面 | `12.105`（①②③）；触发＝真实入口核验或明确声明不支持 |
| 9 | 06 域 2.4；12.105；13.104 | 四处**拟新增锚**经本节实测**不存在**：`TestBootIndexEntriesBoundedAcrossGenerations`、`TestNoTierCommitPreservesApplicationOrder`、`TestRecoveryEntriesPreserveStateAndObligations`、`TestDeclaredPluginChoiceSurvivesRestart`／`TestExecutionIsolationSurvivesSessionRestart`／`TestEarlyFactRemainsDiscoverableAfterRepeatedCompression` | 名字写在任务行但树内没有，属"零匹配 exit 0"风险面 | `13.102` 的 C7 准入项：签发前逐个核存在性与选择器命中数 |
| 10 | 05.R1；06 域 | 两处**命令漂移**未在 CARRYOVER 记录：05.R1 的负锚原名已退役并重定两枚；06 的 `TestCostPhasesAreMeasuredWithoutOverlap` 已删除、旧选择器会零匹配 | 按旧命令复跑会"零匹配 exit 0"，被误读为通过 | `12.105`／`06.105` 在签发时重述命令；本节已记漂移事实 |
| 11 | 13.R2（已承接，仅闭环） | 某门脚本把目录条目称作"Go 文件"的措辞缺陷已被 10.R7 承接 | 仅登记，不重复派 | `13.105`（10.R7 已记） |

### C8.3 公开模式与部署入口清单

"已验/未验"只指**本叶用命令核到的事实**；未运行的执行面一律记未验，不以存在性代替行为证据。

| 模式/入口 | 真实入口路径 | 配置面 | 资源条件 | 已验/未验（本叶证据） |
|---|---|---|---|---|
| memory 档（进程内） | `memory/ledger/config.go:23` `StorageMemory`；开档经 `assembly/run.go` `Assemble`→`ledger.OpenTier` | 宿主 `storage.type: memory` | 无外部资源 | 已验（代码声明）：能力面 `Durable=false`、`AtomicBatch=true`（`config.go:303` 三目）；未验（行为）：本叶未跑 |
| journal 档（唯一持久本地引擎） | `config.go:34` `StorageJournal`；根下 `kv/` 单一追加日志，一次提交=一帧、日志同步是唯一屏障 | `storage.type: journal`＋`root` | 根目录可写 | 已验（注释契约在树）；未验（行为） |
| localfile（退役名，别名到 journal） | `config.go:30`：本构建按 journal 打开，且**按名拒绝**仍持退役分片布局的根；迁移入口 `MigrateShardedRoot` | `storage.type: localfile` | 同 journal；旧布局需先迁移 | 已验（在树且拒绝路径在注释中具名）；未验（行为） |
| file 档（外部逐键 CLI） | `config.go:37` `StorageFile`；`errRustVikingMissing`（`config.go:154`）；能力面 `AtomicBatch=false`（`:303` 三目）→ 多键批具名拒绝 `ErrBatchNotAtomic` | `storage.type: file`＋`RustVikingBinary`／`RustVikingConfig` | **外部 CLI**：本机 `command -v rustviking` → **exit 1（ABSENT）** | **未验（资源缺失）**。部署后果＝该档事实层在首个事实提交处响亮失败，仍待批准（C8.2.3-7） |
| 无 tier（无可用锚／有效锚／允许降级三分支＋检查点子分支） | 12 域 tasks 点名的两条锚（`TestRecoveryEntriesPreserveStateAndObligations`、`TestNoTierCommitPreservesApplicationOrder`） | 未声明 `storage` 时不暗增耐久承诺（`12/specs/session-checkpoint-material:37`） | 无 | **未验且锚不存在**（C8.2.3-9）；承诺本身是有主规格条款的公开行为 |
| 多 agent / 多 session | `assembly/host_document.go:62` `Agents map[string]hostAgent`；`:777` `memory.PartitionsOf(id, ReadNamespaces)`；跨会话义务锚 `TestCrossSessionObligationsStayInTheirTargetSlice` 与 `TestGateFailsClosedWithoutAttention`（既有，本叶未跑） | `agents:` 映射 + 每 agent 的 `memory.read_namespaces` | 无 | 已验（入口与分区取样在树）；未验（跨会话/跨进程行为） |
| exec / tmux 安全上下文 | 声明面 `tool/exec/decl.go:20-40` `ExecutionDecl{Command,Args,Dir,Resource}`；宿主声明面 `api/config/config.go:246` `ExecSpec{Monitor}` **只有监控节奏**；可用性探针 `tool/exec/tmux.go:20` `IsTmuxAvailable`（PATH 探活，不可被构造替代） | 宿主 `exec.monitor.*`；无执行用户/环境键 | 本机 `tmux` 存在（`/opt/homebrew/bin/tmux`） | **部分未验且存在代码反例**：当前接口只有"工作目录"与"资源身份"，**没有执行用户与环境字段**（`grep -w runAsUser` = 0）；13.104 拟新增锚要求"经真实工具核执行用户/环境/目录"，其中**用户与环境两面在现接口上不可达** → 须先裁"补齐能力面"还是"明确不支持并拒绝"，不得以旧类删除推出能力退役（D1 H13） |
| smoke 入口 | `examples/wechat-bot/*_test.go` 带 `//go:build smoke`（9 个文件，含 `smoke_test.go`／`smoke_harness_test.go`／`smoke_exec_test.go` 等）；命令 `go test -C examples/wechat-bot -tags smoke . ...` | 独立 Go 模块（其 `go.mod` 指向根源码）；长跑参数 `-args -seed/-window` 只对承载包生效 | 合成根；真机长跑禁区含用户数据目录 | 未验（本叶禁跑）；禁区与参数口径已在 13.R2c 记录 |
| 集成测试入口 | `tests/integration/{acceptance,capabilities,config,evolution,execution,history,host,hotupdate,inspection,journey,learning,state}`；已核存在的锚：`TestScenario11ConfigDrivenSubagentAndPipeline`（acceptance）、`TestProjectionAssemblyServesTheThreeUses`（host）、`TestSymptomAnchorMidbandProcessRecalledByTicket`（history）、`TestBuiltinCatalogMountsEveryFeature`（assembly）、`TestHostBootCompressionObeysTheDeclaredSummaryBudget`（assembly） | 各包自带夹具（多为合成根+脚本化模型） | 部分需真实存储/执行资源 | 已验（锚存在性）；未验（行为） |
| 门入口（本地） | 转发壳 `scripts/lint.sh`(528B)→`scripts/gates/lint.sh`(15314B)；`scripts/preflight.sh`(498B)→`scripts/gates/preflight.sh`(8201B)；`scripts/gates/archive-preflight.sh`(6513B) | `TAGENT_RUNTIME_EVIDENCE_DIR` 必设，否则具名红 | 无 | 已验（文件存在性及字节数）；未验（执行） |
| 运维/修复入口 | `scripts/recover-kv`、`scripts/fold-cold-root`、`scripts/migrate-kv` | 各工具自有参数（干跑默认/取写权） | 需离线源与可写目标 | 未验（本叶禁跑） |

### C8.4 跨变更接缝

**原则**：只列"本候选真正依赖的共同前置"，并用"无环"表述把互等问题解开；不吸收、不删除、不屏蔽外来变更的面。

1. **no-prefix-criterion**
   - 对方声明的启动门：`runtime-recovery-ideal` 归档入 main（`no-prefix-criterion/proposal.md` 首段）。
   - 本候选真正需要的接缝（实测在树）：票据唯一生成点 `memory/replay/replay_budget.go:338` `TicketOf`（全仓 23 处引用）＋计量预算化呈现 `replay_budget.go:261` `presentWithin`＋呈现侧前置注入 `assembly/request.go:1361` `presentedProjectionContent`（前缀守卫在 `:1366`）。
   - **真实共同前置**（无环）：(a) 票据形态与生成点唯一；(b) 预算计量覆盖呈现开销。两条都在**本变更内**可验（13.104 的 `TestTinySummaryBudgetIncludesPresentationOverhead` 拟新增），因此**本变更加入 main 不依赖 no-prefix 先落地**。
   - **不属于本变更、也不得等待**：durable 身份的 typed 化与结构门（no-prefix 的 02–10 域）。04.R0 原文"前置 09.R1 与 10.R1"与对方启动门构成互等；解除方式＝把 04.R0 的前置改写成"所需接缝已在共同候选集成并验过"，使本变更的完成面不再引用对方的归档状态；对方启动门保持其独立身份不变（CARRYOVER 第二节已如此规定）。
2. **runtime-architecture-convergence**
   - 该变更文档**未**引用 `runtime-recovery-ideal`（本叶 grep 零命中），其编排以自身基线为唯一实施基线、并以"仅放行 pilot、其余叶 blocked"自我约束。
   - 真实共同前置：同一候选工作树 + "不批量实施"纪律 + 独立复审才能收口。**不得**把其 32 叶 blocked 当作本变更的阻塞，也不得把本变更的 F 阶段当作它的解锁条件。
3. **config-surface-normalization**
   - 其 proposal/ORCHESTRATION **无**本变更名、无"退位"、无 `relocatableStorage`／`RetireFacts` 引用（本叶 grep 零命中）→ 耦合是**单向的**：本变更旧任务行把 `assembly/request_test.go`、`tests/integration/host/age_fold_test.go` 等标为"该变更占用面（派发单禁改）"。
   - 真实共同前置＝文件写面（`assembly/request.go`、`memory/replay/age_segments.go`、请求预算面）与键名契约。无环表述：本变更在 13.102/13.104 签发写面时按对方 W1 的完成状态**串行排程**，不等待其归档；对方亦不等待本变更。
4. **外来未跟踪面**（只读，不吸收/不删除/不屏蔽）：`openspec/changes/{runtime-architecture-convergence,no-prefix-criterion}/`、`golden/runtime/no_prefix_criterion.json`、`scripts/gates/{check_event_semantics_symptoms,runtime_budgets,test_check_runtime_symptoms,check_no_prefix_criterion,test_check_no_prefix_criterion}.py`、`examples/wechat-bot/skills/interview/agent-interview-helper/`。同一工作树上的红不能靠口头"外来"放行：须在实际候选副本按正式入口验证并保留集成差异说明（ORCHESTRATION 授权节）。

### C8.5 环境身份记录

**① Git 取证（仓外只读视图；仓库 `.git` 元数据为 iCloud dataless，禁止修复/改写）**

统一前缀（视图已验证存在）：
`env GIT_DIR=/tmp/tagent-audit-git-qiww4hrj GIT_WORK_TREE=/Users/pengweiye/Documents/codes/tagent GIT_INDEX_FILE=/tmp/tagent-audit-git-qiww4hrj/index GIT_OPTIONAL_LOCKS=0 git <cmd>`

| # | 命令 | exit | 原始输出摘要 |
|---|---|---:|---|
| 1 | `diff --name-status 90fecc56… 7b0cde16… -- openspec/specs` | **0** | 21 行，全为 `M`（architecture-review-standards／compaction-scheduler／compression-pipeline／config-hot-reload／config-liveness／delivery-closure／deterministic-compress-level／error-handling-consistency／event-key-visibility／event-lifecycle／event-segment-store／event-sourced-projection／llm-summary-generator／memory-curation／production-wiring-fix／production-wiring／rustviking-config／session-checkpoint-material／storage-lifecycle-completion／task-skeleton-compression／tombstone-partition-isolation） |
| 2 | `status --short` | **0** | 75 行＝**64** 条 ` M`（全部在当前变更目录内）＋**11** 条 `??`（`examples/wechat-bot/skills/interview/agent-interview-helper/`、`golden/runtime/no_prefix_criterion.json`、`openspec/changes/no-prefix-criterion/`、`openspec/changes/runtime-architecture-convergence/`、本变更的 `CARRYOVER.md` 与 `history/`、五个 `scripts/gates/*` 外来文件） |
| 3 | `diff --name-only HEAD -- . ':!openspec/changes/runtime-recovery-ideal'` | **0** | 输出 **64** 行且**全部**落在被排除目录内 ⇒ **变更目录之外零改动**（`grep -vc '^openspec/changes/runtime-recovery-ideal/' ` → **0**）。**路径规格勘误（必须记）**：在 zsh 下该 `:!` 形式被转义为 `:\!…`，git 视其为字面路径（无命中），于是只剩 `.` 生效而列出全部 64 行；改用 `:^` 或 `:(exclude)` 两种形式实测均返回**空**（0 行、exit 0）。两种读法对"目录外零改动"这一结论一致，故结论不受影响；**但后续派单不得照抄 `:!` 形式并据其空/非空判成败**。 |

**② 视图与仓库身份**

- `/tmp/tagent-audit-git-qiww4hrj/HEAD`＝`7b0cde162627ad37f1d364bbf3508b3bd0518431`（候选）；`index`＝487927 B（**非空**，故不由"HEAD 构造出的空 index"反推暂存为空）；`objects` 软链到仓库 `.git/objects`。
- `git rev-parse HEAD` 与 `git rev-parse 90fecc56…` 均 **exit 0**，基线提交展开为 `90fecc56 purify: 代码词汇对齐…与 checkpoint/迁移运行时修复 (#38)`。
- 仓库侧实测：`.git/HEAD` 的 `stat` 为 `size=20 blocks=0 flags=hidden,compressed,dataless`；`.git/index` **不存在**（`ls` 报 No such file）。⇒ 与 ORCHESTRATION「环境安全」节一致：读取零字节不等于文件真的空，全程未对用户 `.git` 做任何写/修复。

**③ 工具链与资源**

- `go version go1.27.1 darwin/arm64`；`Python 3.9.6`；`openspec 1.2.0`；`git version 2.39.5 (Apple Git-154)`。
- 资源探针：`rustviking` **ABSENT**（exit 1）；`tmux`＝`/opt/homebrew/bin/tmux`；`gh`＝`/opt/homebrew/bin/gh`。

**④ 合成数据根与读写范围声明**

- **产品树只读**：本叶对 `/Users/pengweiye/Documents/codes/tagent` 下任何产品 `.go`、测试、CI 配置、主规格、`golden/`、`scripts/`、权限与 git 元数据**零写入**。
- **唯一写入**：本文件（`openspec/changes/runtime-recovery-ideal/changes/13-verified-completion/design.md`）的新增 C8 节。
- **测试产物**：本叶不运行产品测试/构建，故无测试产物；全部临时文件限于**自有 `/tmp` 目录**：`/tmp/c8101-git1-diffname.txt`、`/tmp/c8101-git2-status.txt`、`/tmp/c8101-git3b-r3.txt`、`/tmp/c8101-git3-final.txt`、`/tmp/c8101-verify/`（负锚草稿与 `mapcheck.py`）。未使用任何真实数据根，未读 `examples/wechat-bot/.wechat-config/data`，未读用户 `--source` 根。

**⑤ 源文件完整性抽样（工作树 sha256 对比候选 blob sha256）**

| 文件 | 工作树 sha256 | 候选 blob sha256 | 结果 |
|---|---|---|---|
| `assembly/run.go` | `910a253d9feb1f93…2d55dcd4` | 同 | MATCH |
| `memory/ledger/segment_store.go` | `70de3383ac2110c9…398a5006` | 同 | MATCH |
| `memory/ledger/journal.go` | `cdf3f2f0c5f4ef92…fc4d695f6` | 同 | MATCH |
| `runtime/state/durable.go` | `ccbde0dee31aeb8a…f12b725e` | 同 | MATCH |
| `tool/recall/key.go` | `ea82c64fa3b48ba3…bae8c26b` | 同 | MATCH |
| `memory/replay/replay_budget.go` | `5155d9e5760922f4…23d92cd1` | 同 | MATCH |

命令：`shasum -a 256 <path>` 对比 `git cat-file blob 7b0cde16:<path> | shasum -a 256`；结果 **6/6 MATCH**，即工作树在这 6 个受测关键文件上与候选逐字节相同（抽样，不替代全量）。

**⑥ 符号残留实测（只读 grep，排除 `openspec/`）**

零残留：`RetireFacts`、`relocate`、`keepKeysLocked`、`keptHotKeys`、`RelocateReport`、`withRetirement`、`placeIndexCut`、`journalIndexCut`、`RebuildProjectionFromWAL`、`BindStore`、`NewDurableCommit`、`LifecycleManager`、`TombstoneSet`、`runAsUser` **均为 0**；在树：`CommitFact`=22、`FactRefused`=14、`ErrBatchNotAtomic`=16、`TicketOf`=23、`presentedProjectionContent`=14、`presentWithin`=4。
命令：`for s in <符号>; do grep -rn --include='*.go' -w "$s" . | grep -v '^./openspec/' | wc -l; done`。
含义边界：零残留**只**支持"这些名字不再出现在 Go 树"（08.R3/R4、10.R9b、5.3.5 的删除面）；**不支持**"被删名字承载的行为也一并退役"（D1 H13）。`runAsUser`=0 与 C8.3 的 exec 安全上下文合并读，正是 H13 的代码反例。

**⑦ 本节结论的适用边界**

- 以上全部是**只读＋计划记录**证据：不替代任何叶的三锚，不替代 F2 矩阵，不替代 F3 独立审，不替代 F4 的远端 CI/保护/交付。
- 未核面如实列出：任何产品测试、构建、长跑、真实存储 CLI、真实执行资源、远端 CI 与分支保护均**未运行**；21 个主规格语义 diff 只留档未逐条判语义。

### C8.6 负锚自测（映射表可被三反例检出）

工具：`/tmp/c8101-verify/mapcheck.py`（一次性草稿，**不进仓库**）。两块输入：`CARRYOVER.md` §一的每域编号全集，与本文件 C8.1 对应小节表体。三条判据：

- **检查① 覆盖**：逐域比较编号集合（01 的 `1.1` 与 05 的 `1.1` 视为不同项，故按 C8.1.N 小节绑定域再比多重集），对称差必须为空。
- **检查② 身份不得降级**：任何一行身份格出现 `已知key可读` 时必须同时出现 `可发现`（即只容许"二者并列且区分"，不容许替换）；另要求全表至少保留一条 `可发现` 语义。
- **检查③ 未验不得写成通过**：C8.3 每行必须给出 `已验`/`未验` 之一；声明 `已验` 必须给出**证据种类**之一（`代码声明`/`注释契约`/`锚存在性`/`在树`/`文件存在性`）；资源实测为 `ABSENT` 的行不得声明 `已验`。

**检出证据（逐条实跑，原始输出）**

| 反例 | 构造（在 `/tmp` 副本上，仓库零改动） | 检出结果 |
|---|---|---|
| 删一个旧编号 | 从副本删去 `01.R1b` 行 | `FAIL 覆盖: CARRYOVER 有而表内无 -> ['01.R1b']（域 01-three-hooks-golden）` / `RESULT: FAILED (1 finding(s))` / exit **1** |
| 把"可发现"换成"已知key可读" | 把 4.4 行身份改写为"按已知key可读该事实" | `FAIL 身份降级: 行 4.4 以「已知key可读」取代「可发现」` ＋ `FAIL 身份降级: 全表无任一条保留「可发现」语义` / `RESULT: FAILED (2 finding(s))` / exit **1** |
| 把未验模式写成通过 | 把 C8.3 file 档行的 `未验（资源缺失）` 改为 `已验` | `FAIL 未验冒充通过: C8.3 file 档（外部逐键 CLI） 行资源实测 ABSENT 却声明已验` ＋ `… 行声明已验却无触发命令/凭据` / `RESULT: FAILED (2 finding(s))` / exit **1** |

对照：未变异的同一文件 `python3 mapcheck.py check base.md` → `RESULT: ok`，exit **0**。

**过程中被自己的检查抓到的两处真缺陷（如实记，不隐去）**：①初版全表无任一条保留"可发现"语义 → 4.4 的身份被改写为"可发现性不是已知key才可读"，H13 的区分才落地；②C8.3 门入口行原写"已验（四个文件存在及字节数）"缺证据种类 → 改为"已验（文件存在性及字节数）"。两处都是检查器先红、正文后改，不是事后修饰。

三条均为**人工规划覆盖反证**，不冒称已有自动门：脚本只是一次性草稿，产物留在 `/tmp/c8101-verify/`（`mapcheck.py`、`base.md`、`m1.md`、`m2.md`、`m3.md`），不属于本变更交付物；后续若要成为正式门，须按 13.102/13.103 重新签发文件与消费者。

## C9 · 13.103 执行记录

> 本节产生方式：13.103 的剩余实施（新负锚、第三 profile 收尾、接线自测）与全部验证运行。未改产品 Go、主规格、`openspec/changes/archive/**`、外来未跟踪六件；未 commit/push/archive。唯一写入面＝C7.1 的 13.103 文件、本文件与 `tasks.md` 的记录指针。**13.103 的 checkbox 仍不勾**，由编排者核验后代勾。

### C9.0 口径与环境事实（必须与本表同读）

1. 「负锚恰 1 命中」＝该 `-k` 选择器下顶层用例数恰 **1**（零匹配或 2+ 都不合规）；「要咬」＝删掉对应判据后同一用例转红，且未变异控制同一次交付内为绿。
2. 本工作树 `.git/HEAD` 读为零字节（`wc -c .git/HEAD` 报 20 字节，`od -c .git/HEAD` 读出为空；`git -C . rev-parse HEAD` 报 `fatal: not a git repository (or any of the parent directories): .git`）。按 [ORCHESTRATION](../../ORCHESTRATION.md)「环境安全」，**不重建 `.git/HEAD`/index/lock 来压掉报错**：任何读 revision 的用例在本树必红，故红项一律另取仓外只读 git 目录复验（`GIT_DIR=/tmp/gitprobe/.git GIT_WORK_TREE=<repo>`，先核 `git -C /tmp/gitprobe rev-parse HEAD` 为 `99e2c4e2…`），红/绿分列，不把环境红记成产品绿。
3. 全部变异只在 `/tmp` 副本上做（`/tmp/mut-pf`、`/tmp/mut-ev`、`/tmp/mut-plan`、`/tmp/rrprobe`、`/tmp/lintarch`），仓库产品、测试、CI、archive 零改动；`/tmp` 只作执行 scratch，不作为交付物引用。

### C9.1 逐项实施状态（C7.1 的 13.103 十一个文件）

| 文件 | 动作 | 状态 | 证据 |
|---|---|---|---|
| `scripts/gates/check_runtime_evidence.py` | 改 | **实施** | 判词改由日志派生：`check_event_evidence_subtests` 增 `earned = observed.get(key, "missing")`，`status != earned` 即拒并点名该 `(parent,name)`；派生点 import `run_runtime_scenarios.event_subtest_states`（不复制解释器）。相对 `/tmp/mut13103` 副本 `+5／−0`——该副本取自本文件已改过一部分之后，故 **+5 只是最后一段判词块，不是总变更量** |
| `scripts/gates/test_check_runtime_evidence.py` | 改（并入三例） | **实施** | 三枚负锚在树（`test_subtest_without_run_is_refused`、`test_parent_pass_cannot_invent_a_subtest_pass`、`test_same_log_has_one_verdict_across_consumers`）；全文件 130 例 exit 0 |
| `scripts/scenarios/run_runtime_scenarios.py` | **不改** | **零改动** | 与 13.102 前的副本逐字节相同（`diff` 0 行差异）；派生函数以 import 消费，未回填签名 |
| `scripts/gates/check_runtime_plan.py` | 改 | **实施** | 第三 profile `runtime-recovery-ideal`：`RUNTIME_RECOVERY_PROFILE`/三单元/十八叶/F1–F4/签态/派发门。相对 13.102 前副本 **+401／−1**（唯一删除行见 C9.9-②） |
| `scripts/gates/test_check_runtime_plan.py` | 改（并入三例＋夹具） | **实施** | 合成十八叶/33 边夹具（三单元三种「前置」写法都写入）＋4 例：双向相等、未知/缺 `active_leaf`、未勾前置不可派、未签不可派。全文件 49 例 exit 0 |
| `openspec/changes/runtime-recovery-ideal/task-graph.json` | 新增 | **实施** | 18 leaves／33 edges／F1–F4／18 条 `signatures`（6 signed、12 pending）／`active_leaf`；plan 门对**真图**判绿（C9.6） |
| `scripts/gates/archive-preflight.sh` | 改（仅接线可见性） | **不改** | 与 13.102 前副本逐字节相同；`--commit` 语义未动，改的是调用方（下两行） |
| `.github/workflows/ci.yml` | 改（一个作业） | **实施** | 新增 `archive-preflight` 作业：`fetch-depth: 0`，PR 用 `--commit "$CANDIDATE_SHA" --base "$(git rev-parse origin/$PR_BASE_REF)"`，push 用 `--commit "$CANDIDATE_SHA"`；空值显式落空，不内联表达式 |
| `scripts/gates/lint.sh` | 改（仅归档接线一段） | **实施（修正半成品缺陷）** | 相对 13.102 前副本 `+25／−0`（C7.1 原批 +15，本叶修缺陷再 +10，均在该段内）。见 C9.7：原状「检测工作树 vs `HEAD`」而「判据 `--commit HEAD`」对象错位，自证为空转；改为两对象两模式 |
| `scripts/scenarios/test_run_runtime_scenarios.py` | 改（并入一例） | **实施** | 相对 13.102 前副本 `+81／−0`；新增 `test_preflight_rejects_corrupt_generated_evidence`（C9.3/C9.4）。全文件 41 例：本树 3 例环境红（C9.0-2），仓外只读 revision 下 41/41 绿 |
| `scripts/gates/test_check_dod_coverage.py` | 改（并入一例） | **实施** | 相对 13.102 前副本 `+83／−0`；`test_archive_tree_change_without_evidence_is_refused`：真 git 树里造归档提交，控制（非归档路径）判「adds no path」为绿、负例 exit 1 且点名路径；全量 54/54 cases |
| **不改**：`check_dod_coverage.py`、`check_event_integrity_mutations.py`、`runtime_checklib.py`、`scripts/{lint,preflight}.sh`、`scripts/gates/preflight.sh` | — | **零改动** | 逐字节同 13.102 前副本 |

### C9.2 正锚全量运行（原始 exit 与计数）

| 命令 | exit | 计数 | 备注 |
|---|---|---|---|
| `python3 scripts/gates/test_check_runtime_plan.py -v` | **0** | `Ran 49 tests` `OK` | 45→49，新增恰为第三 profile 的 4 例 |
| `python3 scripts/gates/test_check_runtime_evidence.py -v` | **0** | `Ran 130 tests` `OK` | 含三枚新负锚 |
| `python3 scripts/gates/test_check_dod_coverage.py -v` | **0** | `54/54 cases passed` | 主干为自建用例串；`-k` 时交 unittest（C9.3） |
| `python3 scripts/gates/test_check_event_integrity_mutations.py -v` | **1** | `Ran 56 tests` `FAILED (failures=1, errors=2)` | **环境红**：`ClosureCliTest.setUpClass` 走 `git ls-files -z …` → `fatal: not a git repository`；另两例因 revision 为 `None`。非 13.103 引入（未改该文件，见 C9.8） |
| 同上，仓外只读 revision 复验 | **1** | `Ran 60 tests` `FAILED (failures=1)` | 仅剩 `test_a_patch_inside_a_nested_module_is_rejected`（`0 == 0`）：该例自建夹具树，`GIT_DIR` 指向仓外使夹具内 git 读到错误树，属探针副作用，如实记未验、待健康 git 树复跑 |
| `python3 scripts/scenarios/test_run_runtime_scenarios.py -v` | **1** | `Ran 41 tests` `FAILED (failures=3)` | **环境红 3 例全在 `EndToEndTest`**，均 `cannot read the repository revision`（C9.0-2） |
| 同上，仓外只读 revision 复验 | **0** | `Ran 41 tests` `OK` | 同一份文件、同一个 runner，仅补上可读 revision → 全绿，红项归环境 |
| `python3 scripts/gates/test_check_runtime_workflow.py -v` | **0** | `Ran 55 tests` `OK` | C7.7 相邻回归（plan 门接线） |
| `bash scripts/gates/lint.sh --only contracts`（`/tmp/lintreal` 隔离树：真 `scripts/lint.sh`＋`scripts/gates/lint.sh`＋真 plan 门与解析层＋真本变更目录与主规格，其余网关为桩） | **0** | `lint: runtime plan contract` → `$ python3 scripts/gates/check_runtime_plan.py --change-dir openspec/changes/runtime-recovery-ideal --json` 恰 **1** 次，判词 `"ok": true`、`"graph_leaves": 18`、`"graph_edges": 33`、`"errors": []` | 新 profile 经**真实入口**判真图绿（不只在直调 CLI 上绿） |
| `python3 scripts/gates/check_runtime_plan.py --change-dir openspec/changes/runtime-recovery-ideal --json`（`gates/lint.sh:181` 的同一 argv） | **0** | `graph_leaves 18`、`graph_edges 33`、`final_tasks 4`、`checkboxes 18`、`signatures_signed 6`／`signatures_pending 12`、`active_leaf 1`、`errors 0`；`leaves_checked` 先后读到 4 与 5（06 单元并发勾了 `06.102`，mtime 晚于本叶运行；它是本叶的**输入**，不是本叶的产出） | 新 profile 对真图判绿 |

### C9.3 负锚：恰 1 顶层命中（8 枚全跑）

| 选择器（文件见行首） | `Ran …` | exit | 拒绝行为（该例断言要点） |
|---|---|---|---|
| `test_check_runtime_evidence.py -v -k test_subtest_without_run_is_refused` | **1** | 0 | 父 pass 有效、子行删除 → 非零并点名 `states 'pass' … show 'missing'` |
| `… -k test_parent_pass_cannot_invent_a_subtest_pass` | **1** | 0 | 子行改名后父 pass 仍在 → 非零，父通过不替子锚作保 |
| `… -k test_same_log_has_one_verdict_across_consumers` | **1** | 0 | runner `attest_event_test` 判 `unattested` 与 checker 派生同值，checker 同拒 |
| `test_check_runtime_plan.py -v -k test_unapproved_leaf_is_not_dispatchable` | **1** | 0 | 未勾前置的叶不可派，点名 `prerequisite(s) ['06.104','12.104'] not ticked` |
| `… -k test_unknown_or_missing_active_leaf_is_refused` | **1** | 0 | 未知叶非零点名；缺 `active_leaf` 键亦非零；`null` 才是「无在飞叶」 |
| `… -k test_missing_design_signature_blocks_implementation` | **1** | 0 | 未签叶既不可派也不可用 `in_progress` 冒充 |
| `test_check_dod_coverage.py -v -k test_archive_tree_change_without_evidence_is_refused` | **1** | 0 | 真 git 树：控制提交（非归档路径）判「adds no path」；归档提交 exit 1 点名路径＋`REFUSED`；空证据目录点名 `holds no mutations/evidence.json` |
| `test_run_runtime_scenarios.py -v -k test_preflight_rejects_corrupt_generated_evidence` | **1** | 0 | 见 C9.4：控制绿→改日志→摘要拒（点名日志路径）→摘要同步成自洽假→判词拒（点名锚） |

### C9.4 负锚「要咬」：变异红／控制绿（同一次交付内取）

| 变异落点（`/tmp` 副本） | 变异后 | 未变异控制 |
|---|---|---|
| 删 `check_runtime_evidence.py` 的日志反查（5 行） | `test_subtest_without_run_is_refused` exit **1** `FAILED (failures=1)`；`test_same_log_has_one_verdict_across_consumers` exit **1** `FAILED (failures=1)` | 同树同两例 exit **0** `OK` |
| 同上，跑新锚 `test_preflight_rejects_corrupt_generated_evidence` | exit **1** `FAILED (failures=1)`（自洽的假产物被受理解，`ok: true`） | exit **0** `OK` |
| 删 `check_runtime_plan.py` 的签态判据（`in_progress` 无签＋`no design signature` 两处） | `test_missing_design_signature_blocks_implementation` exit **1** | 未变异副本 4 例 exit **0** `Ran 4 tests OK` |
| 删 `check_runtime_plan.py` 的前置未勾判据（`unmet` 三条） | `test_unapproved_leaf_is_not_dispatchable` exit **1**，实测断言 `0 == 0` | 同上 exit **0** |

新锚（`test_preflight_rejects_corrupt_generated_evidence`）的构造与边界：生产段用 `gates/preflight.sh` 自己的 event-short argv（`--contract … --scope event-short --repeat 10 --out "$event_out/event-short"`，合同 `repeat` 随之取 10），消费段用**同一个** checker 的单件入口（`--contract … --evidence … --repo-root … --json`）。篡改点在 runner 成功生成之后：用例先断 10 轮全 attest 与 `repeat-1..10` 目录，再把某一轮产物复制成隔离副本，控制组在副本上先跑一遍为绿；随后删掉一枚锚的原始子行（evidence 一字未动）→ 摘要不符被拒并点名日志路径；再把 `test_log_sha256` 同步成与篡改后日志一致（自洽的假），此时唯一拒绝理由就是判词本身，输出点名 `({test_id},{name}) states 'pass' … show 'missing'`。**未覆盖面**：`--bundle` 形式（preflight 的 binding 步）另需 6 个长跑分片与 1 份变异产物，夹具不可达；preflight 的 produce→bind 次序另由 `test_check_dod_coverage.py` 的 `test_preflight_generates_evidence_before_dod_gate`（真 `scripts/preflight.sh`＋桩网关，判 argv 次序）覆盖，两处合读才是完整面。

### C9.5 旧两 profile 判词不变（三条独立证据）

1. **代码面**：`check_runtime_plan.py` 相对 13.102 前副本 `+401／−1`，唯一删除行是**共享分发处**的 `unknown domain_profile` 文案（见 C9.9-②）；旧十域与 `event-integrity` 两套常量、规则、文案零改动。
2. **用例面**：`test_check_runtime_plan.py` 相对同一副本 `+226／−0`——旧例逐字节保留，新增的只有第三 profile 的 4 例（45→49）。
3. **行为面**：把 `PlanCheckerTest`/`EventProfileTest` 在**未改动的旧 checker** 上跑——`Ran 38 tests OK`（exit 0）＋`Ran 4 tests OK`（exit 0），与当前 checker 下同两类的判词一致。

### C9.6 依赖双向相等与派发门（真实计划上复核，`/tmp/rrprobe` 副本）

| 构造 | exit | 检出 |
|---|---|---|
| 未变异副本（控制） | **0** | `ok True`、`edges 33`、`leaves 18` |
| 只在 `changes/12-consumer-state-lifecycle/tasks.md` 的「前置」行加一枚叶 id（图不动） | **1** | `leaf 12.102 declares 前置 ['06.101','12.101','12.106','13.102'] but task-graph depends_on ['06.101','12.101','13.102'] (the dependency source is tasks.md; drift is a second plan)` |
| 只在 `task-graph.json` 改 `13.103.depends_on`（tasks.md 不动） | **1** | `leaf 13.103 declares 前置 ['13.102'] but task-graph depends_on ['13.101']` |
| 恢复 | **0** | 无错误 |

即「双向相等」在真实三单元 tasks.md 与真实图上都成立，且两个方向都能被检出；`signatures` 未签叶不可派由 C9.4 第三行证明。

### C9.7 归档接线（lint 段）与自测

半成品原状有两处缺陷，均已在同段内修正（只改该段，其余段落未动）：

- **对象错位**：检测用 `git diff --diff-filter=A HEAD`（工作树 vs `HEAD`），判据却传 `--commit HEAD`（`HEAD` vs `HEAD^`）——检测到的那次移动永远没有提交可判，门自证为空转。
- **未跟踪不可见**：`git diff HEAD` 看不到未跟踪的新文件，`openspec archive` 刚落盘的归档路径因此不进判据。

现接线（`scripts/gates/lint.sh`，仅该段）：先判**本次提交**带来的归档路径（`HEAD^..HEAD`，与 CI 作业同一命令 `--commit HEAD`），再判**工作树**里的归档移动（`git diff HEAD` ∪ `git ls-files --others`，因还没有 revision 可判，改用同一门的 `--evidence "$evidence_dir"` 判产物与本树是否绑定）；两者都无则印「no object, skipped」；非 git 树如实跳过。

`/tmp/lintarch` 最小树（真 `scripts/lint.sh`＋`scripts/gates/lint.sh`＋`archive-preflight.sh`，其余网关为桩，`go` 为桩）四态实测：

| 状态 | exit | 观察 |
|---|---|---|
| A 无归档移动 | **0** | `(no path added under the openspec archive tree; the archive gate has no object, skipped)` |
| B 归档路径未提交 | **0** | `$ bash scripts/gates/archive-preflight.sh --evidence /tmp/lintarch/run/evidence/run` → `1/3…3/3` → `all gates green` |
| C 归档提交＋产物属另一 revision | **1** | `adds 1 archived path(s): openspec/changes/archive/2026-01-01-fixture/proposal.md` → `the run products describe revision …, not …` → `REFUSED — gates failed: commit-identity` |
| D 归档提交＋产物绑该 revision | **0** | `all gates green for …; openspec archive <change> may proceed` |

C 行即本轮要求的「合成无证据归档路径变更时 `gates/lint.sh` 拒绝」；仓库 `openspec/changes/archive/**` 未被触碰。CI 侧消费者见 C9.1 的 `ci.yml` 行；**服务端 required check 与远端实效不在本节完成面**（C7.8、F4）。

### C9.8 stale 与未验面（如实标注）

| 项 | 状态 | 说明 |
|---|---|---|
| `test_check_event_integrity_mutations.py` 的 3 例环境红 | **未验** | 根因是 `.git/HEAD` 不可读；仓外只读 revision 下 59/60，剩 1 例为探针副作用（`GIT_DIR` 渗进夹具树）。**必须在健康 git 检出复跑**，不在本叶记绿 |
| `scripts/scenarios/test_run_runtime_scenarios.py` 本树 3 例红 | **环境红（已归因）** | 同一文件补可读 revision 后 41/41 绿（C9.2）；本树红不记绿，也不改 `.git` |
| preflight `--bundle` 形式 / 长跑分片 / 全量变异 | **未跑** | 需真实长跑产物与 6 分片；属 F2 矩阵，不属 13.103 三锚 |
| `env TAGENT_RUNTIME_EVIDENCE_DIR="$BUNDLE" bash scripts/lint.sh`、`bash scripts/gates/archive-preflight.sh --evidence "$BUNDLE"` | **未跑** | 同上，需本轮真实 bundle |
| 远端 CI 作业与分支保护 | **未生效** | 配置审查不等于服务端已拦截（F4 核销） |
| `/tmp` 基线副本（`/tmp/mut13103` 等 13.102 前文件副本、`/tmp/13-103-*.txt` 抓取、`/tmp/gitprobe`） | **scratch，非交付物** | 本节的「+401/−1」「+226/−0」与旧 checker 复跑依赖它们；它们一旦消失，该两条需按当时证据等级重取，不得当作永久凭据 |

### C9.9 偏离与自决（凡「没问但自己定了」的事）

1. **`scripts/gates/lint.sh` 归档段修正**（C9.7）：不是纯实现，是把半成品的空转接线改成两对象两模式；`--evidence` 分支超出 C7.3「跑同一命令」的字面，理由是未提交的移动没有 revision 可判，而门在两个形态上都必须有对象（否则检测面与判据面继续错位）。已把新增分支的语义、四态自测与理由记在此，供独立审裁定。
2. **`check_runtime_plan.py` 共享分发文案一行改写**：`unknown domain_profile` 的错误文本原只列「legacy ＋ event」两 profile，加第三 profile 后若不改即为假话；只改这一行、不触及任何 profile 的规则与判词（C9.5-1）。
3. **新负锚的消费段用 `--evidence` 而非 `--bundle`**（C9.4 末段）：`--bundle` 需 16 份长跑/变异产物，夹具不可达；`--evidence` 是同一 checker 的正式入口（`runtime-contracts.yml:233` 与 C7.3 第一行）。次序面由 DoD 的 preflight 用例覆盖。若独立审要求本例必须走 `--bundle`，则需重新签发夹具形态（会引入 16 份合成产物），届时回设计门。
4. **测试文件新增 `hashlib`/`shutil` 两个标准库 import**：只为新例的摘要复算与隔离副本复制，未引入外部依赖（C7.2「不新增库」仍成立）。

## C10 · 13.104 执行记录

> 产生方式：13.104 的实施会话（首会话）＋同日**补跑会话**（把 H05 取证改成经目录装配、复跑全部锚／相邻回归／负变异、复核 D4.2:143）。范围＝C7.1 的 13.104 行 ＋ 12.104 D9.8.6-③ 移交的两件事（`plugin_catalog.go:149` 补 `withSettleFactCommit` 生产装配、D4.2:143 接线点笔误订正）。**未勾选 13.104**；未 git 写、未 commit/push/archive。
>
> 写面：首会话＝`assembly/{plugin_catalog,plugin_catalog_test,summary_gist,summary_gist_test,compress_consumer,host_tools_test}.go`、`tests/integration/history/symptom_anchor_midband_test.go`、本文件、本域 `tasks.md`、12 域 `design.md` 单行订正；补跑会话＝`assembly/host_test.go`（仅 H05 取证，C10.3/C10.9-6）、本文件、本域 `tasks.md`。生产零越界、测试只增不改（`compress_consumer_test.go` 曾试改后**完全还原**，见 3.2）；H05 只换装配来源，断言面不变。

### C10.0 口径与环境事实（必须与本表同读）

- 候选：工作树（`git rev-parse HEAD` = `7b0cde162627ad37f1d364bbf3508b3bd0518431`；`git status` 仍把全树报为已删除，属既有空 index 环境态，不据其判改动，改动以内容＋`git diff` 文件级核对）。
- 工具：`go1.27.1 darwin/arm64`；cwd `/Users/pengweiye/Documents/codes/tagent`；全部命令前置 `env GOFLAGS=-buildvcs=false`，不接管道掩盖退出码。`python3` 两会话实测不同：首会话 3.9.6、**补跑会话 3.12.8**（同机 PATH 不同源）；`check_concept_budget.py` 两次结论一致（exit 0、`new: 57` 未抬），故不影响判词。
- 日志 scratch：首会话 `/tmp/rr-13-104/logs/`，补跑会话 `/tmp/rr-13-104-f/logs/`（均不作持久凭据）；下表每行标注取数会话。
- 负变异一律 `go test -overlay` 指向 `/tmp` 副本：首会话 `/tmp/rr-13-104/mut/`，补跑会话 `/tmp/rr-13-104-f/mut/`；仓库内零 diff。

### C10.1 逐项实施状态（C7.1 的 13.104 行）

| 文件 | 动作 | 落点 | 结论 |
|---|---|---|---|
| `assembly/plugin_catalog.go` | 改（settle 接线） | `plugin_catalog.go:163` `withSettleFactCommit(seam.run.commitFactChecked)` | **已接**。D9.8.6-③ 的缺口闭合：`NewSettleDriver` 的唯一生产构造点拿到了 run 自己的提交面 |
| `assembly/plugin_catalog.go` | 改（scope 拒绝文案若需） | `declaredPluginsOf:275-314` ＋ `carriedBy:318-326` | **零改动**。拒绝文案已点名 scope（`agent %q declares the %s-level plugin %q, which %s…`）；C2 第 4 行裁"不加过滤分支"故不加 |
| `assembly/plugin_catalog_test.go` | 改（并入两例＋移交锚） | `TestDeclaredPluginChoiceSurvivesRestart:275`、`TestSettleFactRepairRunsOnTheCatalogAssembly:325`、helper `assertDeclaredPluginChoice:294` | **已增**（三个顶层名，见 C10.2 的偏离说明） |
| `assembly/summary_gist.go` | 改（呈现开销计入） | `summaryTextRoomOf:143`、`boundedSummaryGist:159`（改为按条目取 room）、`summaryPresentedProbe:135` | **已改**。room 由 `presentedProjectionContent` 自身测出，不在此重述标签 |
| `assembly/summary_gist_test.go` | 改（并入一例） | `TestTinySummaryBudgetIncludesPresentationOverhead:584` | **已增** |
| `assembly/host_tools_test.go` | 改（并入一例） | `TestExecutionIsolationSurvivesSessionRestart:1304`＋`recordingExecBackend:1235`＋`fixtureDirName:1353` | **已增**；只核目录与资源身份，用户/环境两面**不伪造**（C7.2 第 4 行） |
| `tests/integration/history/symptom_anchor_midband_test.go` | 改（并入一例） | `TestEarlyFactRemainsDiscoverableAfterRepeatedCompression:237`＋票据正则 `:225` | **已增**；召回身份全部从请求正文解出，不手工给 key |
| `assembly/compress_consumer.go` | 改 | `summarizeProduct:367` 改传条目（呈现开销的落点），文档补一句 | **已改一处** |
| **未改**：`host_document.go`、`prepare.go`、`run.go`、`api/config/**` 与五个镜像测试 | — | — | **零改动**（`git diff` 文件级核对） |

### C10.2 三锚全量运行（原始 exit 与计数）

> 下表全部为**补跑会话（`/tmp/rr-13-104-f/logs/`）在同一工作树上重跑一次**的原始结果；首会话（`/tmp/rr-13-104/logs/`）在同名命令上计数一致（`pos-*`／`neg-anchors`／`consumer-*`／H05 成对行逐行相同）。两处如实差异：首会话发现锚未带 `-race`（本轮补上）；H05 **单行**命令首会话未单独跑（当时只跑成对命令，且当时探针尚未改为目录装配，见 C10.3）。

| 面 | 命令 | 原始 exit | 命中（补跑会话） |
|---|---|---|---|
| 正锚 | `go test ./assembly -run '^(TestBuiltinCatalogMountsEveryFeature\|TestHostBootCompressionObeysTheDeclaredSummaryBudget)$' -count=10 -v` | **0** | `=== RUN` 20／`--- PASS` 20／`--- FAIL` 0（两枚各 10/10） |
| 正锚 | `go test ./tests/integration/acceptance -run '^TestScenario11ConfigDrivenSubagentAndPipeline$' -count=10 -v` | **0** | RUN 10／PASS 10／FAIL 0 |
| 负锚（3×10，`-race`） | `go test ./assembly -run '^(TestDeclaredPluginChoiceSurvivesRestart\|TestTinySummaryBudgetIncludesPresentationOverhead\|TestExecutionIsolationSurvivesSessionRestart)$' -race -count=10 -v` | **0** | RUN 30／PASS 30／FAIL 0／`--- SKIP` 0／`DATA RACE` **0**（三枚各 10/10） |
| 发现负锚（`-race`，本轮补加） | `go test ./tests/integration/history -run '^TestEarlyFactRemainsDiscoverableAfterRepeatedCompression$' -race -count=10 -v` | **0** | RUN 10／PASS 10／FAIL 0／`DATA RACE` 0 |
| 消费者锚 | `go test ./tests/integration/host -run '^TestProjectionAssemblyServesTheThreeUses$' -count=10 -v` | **0** | RUN 40（父 10＋子 30）／PASS 40／FAIL 0 |
| 消费者锚 | `go test ./tests/integration/history -run '^TestSymptomAnchorMidbandProcessRecalledByTicket$' -count=10 -v` | **0** | RUN 10／PASS 10／FAIL 0 |
| **H05 生产接线（经 catalog 装配，本表 C10.3）** | `go test ./assembly -run '^TestBackflowFailureRecoversWithoutManualSettle$' -race -count=10 -v` | **0** | RUN 10／PASS 10／FAIL 0／`DATA RACE` 0；每次运行 **3** 次模型调用（首进程 2＋重启后的通知回合 1），`settle follow-up … ended by error` 告警 **0** |
| H05 成对 | `go test ./assembly -run '^(TestBackflowFailureRecoversWithoutManualSettle\|TestSettleFactRepairRunsOnTheCatalogAssembly)$' -race -count=10 -v` | **0** | RUN 20／PASS 20／FAIL 0／`DATA RACE` 0 |
| 相邻回归 | `go build ./...`（`env GOFLAGS=-buildvcs=false`） | **0** | 零输出（0 行） |
| 相邻回归 | `go test ./assembly -count=1` | **0** | `ok … assembly 348.061s`，包内 `--- FAIL` 0（整包结论，见 C10.8-1） |
| 相邻回归 | `python3 scripts/gates/check_concept_budget.py` | **0** | `counts.domain_surface.new: 57`（冻结上限 57，**未抬**）／`RESULT: ok` |

### C10.3 H05 经 catalog 装配的取证（D9.8.6-③ 的正锚）

- **探针**：`TestBackflowFailureRecoversWithoutManualSettle`（`assembly/host_test.go:852`）。**本轮改动**：它原先在测试里自接 `NewSettleDriver(..., withSettleFactCommit(booted.commitFactChecked))` 并自写 `fire` 闭包；现改为在重启后的 `*Run` 上走 `Boot` 的同一装配路径取泵——`hostPlugins(&hostRunPluginSeam{run, host, port, decl})`（`host.go:690-705` 的 `WithPlugins` 回调调用的就是它，`:698`），取 `seam.settle`：`fire` 是目录项装的生产 `seam.host().fireNoticeTurn`，提交面是目录项装的生产 `withSettleFactCommit(seam.run.commitFactChecked)`（`plugin_catalog.go:163`），scope 由 `settleScopeOfDocument(decl)` 从装配声明解出。测试**不再构造 driver、fire、commit 三面中的任何一个**，也不再 `DeclareScope`。
- **断言（未弱化）**：原生工具→投递事实被拒写（首进程 `SetFactCommit` 只拒 `TypeTaskSettled`）→ 欠账、链上 0 投递事实、0 回执 → checkpoint＋关进程 → 重启读回同一根 → 目录装配的泵自主补上事实并驱动一次通知回合 → 链上投递事实恰 1、消费回执恰 1、通知回合恰 1（按链上的通知 `invocation` 去重计数，不再由测试自记 fire 次数）。
- **停泵不打断驱动**：首版按「欠账清空」即 `Stop()`，实测每次运行都打 `the settle follow-up … ended by error: context canceled`（回执面先结清、fire 仍在 `WaitAnswer`）。现改为 `waitScopeGate` 等 `consumer.Resting()`（该地址循环无飞行段、无待跑 pass）**且**欠账清空后才 `Stop()`；`-race -count=10` 告警 0。
- **回合真实发生**：每次运行 3 次模型调用——首进程 tool call＋答复 2 次、重启后通知回合 1 次（`model response` 计数，10 次运行逐次均为 3），故「1 个通知回合」不是只凭链上一条材料事件推的。
- **它咬得动**：删掉 `plugin_catalog.go:163` 的 `withSettleFactCommit` 后，本探针 `-count=1` **exit 1**，`host_test.go:915` 报 `waited 10s for the catalog-assembled pump to drive the owed notification to rest`；未变异副本同锚 `-race -count=10` 10/10 绿（C10.2）。
- **与既有目录锚的分工**：`TestSettleFactRepairRunsOnTheCatalogAssembly`（`plugin_catalog_test.go:325`，前会话新增、本轮保留）只钉「事实修补属装配、回合属宿主」的窄面（seam 的 `host` 为 nil，故 fire 失败、0 回执）；H05 钉整条自主恢复链。两枚在同一条 `-race -count=10` 命令里各 10/10（C10.2 的 H05 成对行），删提交面的变异两枚同时转红（C10.4 第 1 行）。

### C10.4 负变异（隔离副本／控制绿同一次交付内取）

补跑会话在 `/tmp/rr-13-104-f/mut/` 重做四案，`go test -overlay` 指向副本、仓库零改动；控制绿取同一交付内未变异副本的 10/10（C10.2）。

| # | 变异（副本落点） | 变异后原始 exit／命中行 | 控制绿 |
|---|---|---|---|
| 1 | 删 catalog 的 `withSettleFactCommit`（生产接线回退；`mut/plugin_catalog.go`，diff 1 行） | `TestBackflowFailureRecoversWithoutManualSettle` **1**／`host_test.go:915: waited 10s for the catalog-assembled pump to drive the owed notification to rest`；同变异下 `TestSettleFactRepairRunsOnTheCatalogAssembly` **1**／`plugin_catalog_test.go:384: waited 10s … to supply the fact the refused write left owed` | 未变异：H05 `-race -count=10` 10/10、目录锚 10/10 |
| 2 | 不支持的 scope 静默忽略（`declaredPluginsOf` 的拒绝分支改 `continue`；`mut/plugin_catalog_scope.go`，−4/+1 行） | `TestDeclaredPluginChoiceSurvivesRestart` **1**／`plugin_catalog_test.go:281: the host-level declaration = <nil>, want a refusal naming the plugin and its scope` | 未变异：该锚 `-race -count=10` 10/10 |
| 3 | `summaryTextRoomOf` 退回字面 `budget`（呈现开销不计；`mut/summary_gist.go`，−3/+2 行） | `TestTinySummaryBudgetIncludesPresentationOverhead` **1**／`summary_gist_test.go:599: the presented summary carries 81 bytes ("history summary: …"), want at most the declared 64` | 未变异：该锚 `-race -count=10` 10/10 |
| 4 | `historySummaryLine` 的历史综述行只留键、不留一行读（发现线索被键替代；`mut/age_segments.go`，1 行） | `TestEarlyFactRemainsDiscoverableAfterRepeatedCompression` **1**／`symptom_anchor_midband_test.go:278: the request carries none of the early read "ask 00" of 00-0-in` | 未变异：该锚 `-race -count=10` 10/10 |

仓库内无变异残留：四案都以 `-overlay` 只读 `/tmp` 副本，`assembly/plugin_catalog.go`、`assembly/summary_gist.go`、`memory/replay/age_segments.go` 的 mtime 均早于本轮（内容未被改写），本轮唯一产品树改动是 `assembly/host_test.go`（C10.10）。

### C10.5 发现锚的实测形态（4.4 的直译）

`5` 个闭合回合（窗口 `1600` 令牌）时：投影留着 **1 张历史综述＋1 个段折叠产物**，折走了 `00-1-plan`、`00-2-call` 等过程条目。真实 `Run.Invoke` 请求里：

- `00-0-in` 的正文一字不在请求内，但历史综述行 `external_input: ask 00` **在**——模型据此知道该事实存在（可发现性，不是"已知 key 才可读"）；
- 请求同时给出 `[evt_02-1-plan|thinking_plan]` 等票据，`recall_get` 逐条读回**提交原文**并与链上事实逐字节相等；
- 上面的 key 全部从请求正文用正则解出，检查不持任何"要用的 key"。

### C10.6 预算口径的落地形状（C7.1「预算口径须含呈现开销」）

- 呈现面 `presentedProjectionContent` 对历史综述会前置 `entry.Summary + ": "`（`"history summary: "`，17 字节），而对合成键不发票据。故该族条目的呈现开销＝该标签。
- `summaryTextRoomOf` **用 `presentedProjectionContent` 自身测量**该标签长度（喂一个既不空也不以 `[evt_` 开头的探针），不在此另写一份标签常量，避免第二真相。
- `boundedSummaryGist` 按 room 截断（呈现后不超预算）；`bandSummaryInstruction` 仍陈述**声明预算本身**——它就是"呈现后的消息不得超过"的那个数。两处因此都在同一预算口径内计入呈现开销，而不各自报一个不一致的数。
- 极小预算（`budget = 0`）下 room 为 0，gist 被截成空串 ⇒ `summarizeProduct` 保留机械形态，不产出超预算消息。

### C10.7 D4.2:143 订正（跨单元记录单行修正，编排者已授权）

- **原文**（12 域 D4.2 第 143 行末列）：`NewSettleDriver` 需多一个"提交投递事实"的 face，调用方 `assembly/host.go` 与 `assembly/settle_turn_test.go`。
- **实际**：`NewSettleDriver` 的生产构造点只有 `assembly/plugin_catalog.go:149` 的 `hostPluginScope` 目录项；`host.go` 只经 `hostPlugins(seam)` 取 `seam.settle`（`host.go:698-707`），不在那里构造。
- **处置**：该行 `assembly/host.go` 改为"`assembly/settle_turn.go` 的 hostPluginScope 目录项（`assembly/plugin_catalog.go:149`）"并留一条日期更正说明；**该文件其余内容零改动**，属 D9.8.6-③ 明写的移交债。
- **复核（补跑会话）**：该行现文为 `… 调用方 assembly/settle_turn.go 的 hostPluginScope 目录项（assembly/plugin_catalog.go:149）与 assembly/settle_turn_test.go。**2026-11-21 更正（13.104 接线时由编排者授权）**：原写 assembly/host.go 是笔误——…`，行号仍是 **143**；本轮未再改该文件任何行（本轮写面只有 `assembly/host_test.go`、本域 design/tasks）。

### C10.8 stale 与未验面（如实标注，不得读成已完成）

1. **整包回归**：`go test ./assembly -count=1` 的原始 exit（0，`ok … 348.061s`，包内 `--- FAIL` 0）见 C10.2；本表不据其绿声称 `./tests/integration/**` 全绿——`tests/integration/{history,host,capabilities}` 仍有 12.104 残项七红（另一单在修），本叶只跑自己的选择器，集成全包结论留给残项单。
2. **端口合并未执行（本叶最大缺口，须裁定）**：C7.1/C7.2 记的"两窄口合并为 4 方法内聚口／构造参数 2→1"**未实施**。理由是它的级联面超出本叶写面：`assembly/skeleton_assert.go:141-144` 持有 `_ func(compressSessionSource, compressProductStore) *CompressionConsumer = NewCompressionConsumer` 的签名断言，另有 `compress_mount.go:24`、`compress_checkpoint_test.go:65/514/557/587-601/807/842`、`anchored_boot_parity_test.go:249`、`recovery_judgment_test.go:109/172`、`compress_consumer_test.go:23-24` 共 6 个文件须同批改；而 12 域 D4.2 末段明写"签名变化牵连的 skeleton_assert、文档或门不能以'编译要求'事后追认，先补精确签发及拥有者"。故按"不越界、不后续追认"处置，**留待编排者补签写面后另派**；C7.2 的"唯一删除"因此未兑现，不得据本表勾选 13.104。
3. **用户/环境两面不可达**：`ExecutionDecl` 只有 `Command/Args/Dir/Resource`（`tool/exec/decl.go:20-40`），`config.ExecSpec` 只有 `Monitor`。锚内只核目录与资源身份，**未伪造**任何用户/环境断言（C7.2 第 4 行、C7.9 末段）。
4. **更深压缩下的可发现性未覆盖（新发现，需裁定）**：同一夹具 `6` 个回合时，最早回合的过程细节只以**合成键引用**残留（`history summary (-hist-…)`、`segment fold (-seg-…)`），合成键不可召回（`TicketOf` 对合成键答空）。本锚只钉到"最早一次折叠后线索仍在"这一深度；该更深形态是**真缺口**（`memory/replay` 写面，本叶无权），不得读成本叶"已覆盖"。
5. **`-race` 未覆盖的锚**：两条正锚与两条消费者锚按签名只跑 `-count=10`（无 `-race`）；`-race` 覆盖 C7.5 指定的三枚负锚、H05 两枚，以及**本轮补加**的发现锚（C10.2 第 4 行）。
6. **未跑**：F2 完整矩阵、F3 终审、F4 交付与远端 CI/保护（C7.8 同口径，不在本表完成面）。

### C10.9 逐项偏离与自决（凡"没问但自己定了"的事都在此申报）

1. **`compress_consumer_test.go` 曾改后完全还原**：为让"呈现开销"落在 `bandSummaryInstruction` 与 `boundedSummaryGist` 两处，先试过把 gist face 的入参改为 room，使 `TestCompressionSummaryCutsToTheDeclaredBudget` 与既有正锚 `TestHostBootCompressionObeysTheDeclaredSummaryBudget`（`host_test.go:461` 断言"指令陈述声明预算"）同时变红；判定"改签名锚去迁就实现"不可接受，改为**指令陈述声明预算、截断按 room**，两枚既有锚原样转绿，该测试文件 `git diff` 为空。
2. **新增了第三个顶层锚名** `TestSettleFactRepairRunsOnTheCatalogAssembly`：C7.1 只写"并入两例"，但 D9.8.6-③ 要求 H05 取证**经 catalog 装配**，而当时既有探针还是测试自接线（本轮已按第 6 条改掉，故本条降为窄面补充）；把这条断言塞进 `TestDeclaredPluginChoiceSurvivesRestart` 会让两个无关主题共用一个名字。故单列一名，并把它并进 H05 那条命令一起跑（C10.2 的 H05 成对行），不改任何既有断言。
3. **`host_tools_test.go` 的"跨重启"用 `Adopt` 驱动**：先按 `Acquire` 写，实测重启后的 `Acquire` 会**再开一次**执行（新 manager 不持该键）；按 `tool/exec/registry.go` 的既有口径（外部会话跨代存活、manager 记账随代逝去、下一代**重探**而非重建），正解是 `Adopt`。已在锚内以 `Explain(...).Held` 与"第二次 `Adopt` 答 `ErrResourceHeld`"两侧钉住。
4. **发现锚钉在 5 个回合**：`6` 回合起最早回合只剩合成键引用（C10.8-4）。选 5 是"最早一次压缩后线索仍在"的最深可用态，不是把要求改小；更深形态单列为待裁缺口。
5. **`fixtureDirName` 只为给声明一个稳定参数**：`Args` 需要一个不随 `t.TempDir()` 基名漂移的值，未引入新夹具文件。
6. **H05 探针改经目录装配（补跑会话，编排者本轮明确授权写 `assembly/host_test.go`）**：把既有 `TestBackflowFailureRecoversWithoutManualSettle` 里自接的 `NewSettleDriver(..., withSettleFactCommit(booted.commitFactChecked))`＋自写 `fire` 闭包替换为 `hostPlugins(seam)` 取 `seam.settle`（C10.3）。这是**对既有检查的构造改动**，非断言放宽：投递事实／消费回执／通知回合三件断言全保留，`turns` 的取数从"测试自记 fire 次数"改为"链上通知 invocation 去重计数"（读产出而非读尝试，且 10 次运行的模型调用数各为 3 佐证回合真实发生）。第 2 条的 `TestSettleFactRepairRunsOnTheCatalogAssembly` 因此**降为窄面补充**（不再承担 H05 主取证），本轮保留不删——删它会动 `plugin_catalog_test.go`，超出授权写面。
7. **`deliveryFactObligationDeclaration` 抽出**：H05 的 seam 要读装配声明才能让目录按文档解 scope，故把原先内联在 `deliveryFactObligationAssemble` 的 `config.Declaration` 抽成一个 helper 供两处共用，避免同一目标集出现第二份副本；`deliveryFactObligationRun/Boot` 的签名未动。
8. **`noticeTurnsOf` 是新加的单使用方 helper**：只被 H05 用；放在 `host_test.go` 的既有取数 helper 群（`deliveryFactsOf`/`factTypesOf`）旁边，不导出、不进生产。

### C10.10 写面自查

- 生产：`assembly/plugin_catalog.go`（1 hunk）、`assembly/summary_gist.go`（加两函数＋改一函数签名与三段文档）、`assembly/compress_consumer.go`（1 处调用＋一句文档）。生产 Go 中无其它改动。
- 测试：`assembly/plugin_catalog_test.go`、`assembly/summary_gist_test.go`、`assembly/host_tools_test.go`、`tests/integration/history/symptom_anchor_midband_test.go`；补跑会话另改 `assembly/host_test.go`（H05 探针：文档三条 bullet 之一、驱动块 8 行→装配块 19 行、新增 `deliveryFactObligationDeclaration`／`noticeTurnsOf` 两个 helper）。既有测试**零删除、零断言放宽**；H05 只换装配来源，断言面（事实／回执／回合各 1）不变。
- 记录：本文件、本域 `tasks.md`、12 域 `design.md` 单行。
- 未碰：`scripts/**`、`golden/**`、`api/**`、`.github/**`、主规格、`openspec/changes/archive/**`、外来未跟踪六件、`examples/wechat-bot/.wechat-config/data`、git 索引/refs。

## C11 · 13.105 收口记录

（13.106 审③补两行，编排者：F2 清单增——race 本地等价 `bash scripts/gates/race_check.sh`（CI race 作业对应面）；执行前 `go list`/测试构建标签核命中（F2 行既有要求落到清单行）。档位行 rustviking 勘误：D13.8 兄弟只读构建＋PATH 注入后 `TestStorageCapabilities` 80/80 零 skip，原"ABSENT 具名 skip"作废。）



### C11.12 R1 勘误（2026-11-22，编排者裁定）

R1（"03.R5 两窄口合并"）为 F1 清单误记：03.R5 原裁定是**拆分**（4 方法 `compressSessions` → 两 2 方法口），真树即其成果（两窄口在位、S1 概念预算门 exit 0、14 findings 全 baselined、负锚"拆分后 4>3 error 消失"在树成立）。"合并为单一内聚口"仅是 C2 第 3 行的比较候选，13.102 未采纳、13.104 未实施；C10.8-2 的"延期"条目一并勘误为"比较未采纳、维持拆分"。R1 按已兑现关闭；实施单（/tmp/r1probe 预演）正确拒施三重越界（写面 8 文件含 skeleton_assert 禁事后追认、4 方法口必然 S1 红、负锚反转），零产品改动。### C11.6 comment_policy 清理（2026-11-22，编排者接手收尾）

前单撞限前已完成全部 77 条注释形状修正（纯注释、零行为改动）。编排者终验：`bash scripts/lint.sh --only policy` 的 comment ratchet 段 `RESULT: ok`（`0 finding(s); 0 beyond baseline; 0 ratchet slot(s) can be lowered`，未抬基线未加豁免）；该段 exit 1 的唯一残余＝外来未跟踪 `test_check_runtime_symptoms` 自测的注册表失配（已知集成差异，候选副本绿，C7.3 在案，不改不屏蔽）。行为等价抽验：`go build` 绿＋`TestRecoveryEntriesPreserveStateAndObligations`/`TestLegalClusteredKeysSurviveCommitAndReopen`/`TestBackflowFailureRecoversWithoutManualSettle` 各 `-count=1` 绿；`check_comment_only.sh` 唯一 CODE-CHANGED 命中 `send_reentry_rig_test.go` 系 D13 断言更新的合法代码变更，非清理违规。外来未跟踪面零改动。

> **产生方式**：13.105 的全部活动＝到期旧项对账＋受控文档/合并入口实施＋验证输入冻结。未改产品 Go、测试实现、CI、主规格正文语义、delta、golden、`docs/OPERATION.md`、`docs/wiki`、冻结 archive；未 commit/push/archive；**13.105 的 checkbox 不勾**，由编排者在 06.106 判决落 `CARRYOVER` 后核。

### C11.0 口径、写面与工具身份

1. **结论口径**：`兑现`＝主承接叶在当前候选上已交付并有其记录中的三锚/门；`部分兑现`＝同一项内至少一条子义务未证；`未完成`＝有具名义务未交付（含待裁）；`待 <阶段> 核销`＝该条实质是后置阶段本身。旧 checkbox 与旧绿一律不继承（顶层 D6）。
2. **判定输入**：C8.1 的 151 行行为身份、各单元 `tasks.md` 的勾选与执行记录、本叶当场复跑的六个门与两条旁证门、隔离副本上的归档消费实测。`实测` 只覆盖其后写明的那一条性质，不外推为整项完成。
3. **写面（本次实际改动，逐文件）**：
   - 改：`CARRYOVER.md`（新增 §四：151 行结论＋C8.2.3 对策＋残余清单）、本文件（本节）、`changes/13-verified-completion/tasks.md`（13.105 行记录，不勾）。
   - 新增：`scripts/gates/test_sync_runtime_specs.py`。
   - 改（生成物）：`docs/api/` 7 页——`assembly`、`features__evolution__governance`、`memory__ledger`、`runtime__agent`、`runtime__state`、`runtime__task`、`runtime__task__approval`；其余 76 页与 HEAD 逐字节相同。
   - 零改动（核对后判定，理由见 C11.5）：`docs/OPERATION.md`、`docs/wiki/**`、`golden/runtime/{architecture,durable_truth,dod_predicates}.json`、全部主 spec 与 delta。
4. **工具身份**：`openspec 1.2.0`（`openspec --version` exit 0，固定值写进新测试的 `setUpModule`）；`python3 3.9.6`；`go1.27.1 darwin/arm64`（仅 `gen_godoc.sh` 使用，其自带 `GOTOOLCHAIN=go1.24.1`）；`git 2.39.5`。
5. **Git 读数方式（重要）**：仓内 index 只有 1 条 tracked 条目、`git status` 把整树报为已删除，故"改了什么"一律用 `git cat-file -p HEAD:<path>` 与工作树逐字节比对，不用 `git status`/`git diff HEAD` 判改动。本叶所有 `docs/api` 改动都这样核过（7 改 / 76 同）。
6. **隔离目录**：`/tmp/c13105/{gates,f4probe,godoc-gen,mut}` 只作执行 scratch，不作持久凭据；其中 `f4probe` 是完整 `openspec/specs` 的副本（151 能力），用于归档消费实测。

### C11.1 到期旧项核销（151 项）

逐行结论落 `CARRYOVER` §四（含行为身份列，便于独立复核时对照它到底在说什么），本节只记分布与归属。

| 来源目录（主承接） | 数量 | 结论分布 |
|---|---:|---|
| 01-three-hooks-golden（13.104） | 6 | 兑现 6 |
| 02-plugin-dynamic-mount（13.104） | 5 | 兑现 5 |
| 03-compression-configurable（13.104） | 9 | 兑现 8；未完成 1（03.R5 两窄口合并，C10.8-2） |
| 04-midband-ticket-recall（13.104） | 9 | 兑现 8；部分兑现 1（04.R2 双票据形态无独立锚、无前缀改造归独立变更） |
| 05-task-custody（12.105） | 8 | 兑现 8 |
| 06-bounded-open-index（06.105） | 12 | 待 06.106 判决 12 |
| 07-governance-custody（12.105） | 9 | 兑现 9 |
| 08-fact-retirement（06.105） | 16 | 待 06.106 判决 16 |
| 09-acceptance-real-measure（06.105） | 11 | 待 06.106 判决 7；兑现 1；部分兑现 1；未完成 1；待 F2 1 |
| 10-contract-sync-dead-code（13.105） | 40 | 兑现 35；部分兑现 1；未完成 1；待 F2 1；待 13.106 1；待 F4 1 |
| 11-atomic-fact-commit（12.105） | 4 | 兑现 3；未完成 1（11.R3 部署后果未批准） |
| 12-consumer-state-lifecycle（12.105） | 9 | 兑现 8；部分兑现 1（12.R3 ①③） |
| 13-verified-completion（13.106） | 13 | 兑现 9；待 13.106 2；待 F2 1；待 F4 1 |
| **合计** | **151** | 兑现 **100**／部分兑现 **4**／未完成 **4**／待 06.106 **35**／待 13.106 **3**／待 F2 **3**／待 F4 **2** |

**未完成 4 项与部分兑现 4 项的具名理由**见 `CARRYOVER` §四.16（R1–R13），不在此重复；其中 R6（P0–P8 判据源丢失）与 R7（`IndexEntry` 死类型）是本叶新查出的两条，证据见 C11.5。

### C11.2 C8.2.3 十一项与 C7.9 的 31 个子项

逐项现状见 `CARRYOVER` §四.14，31 个子项去向复盘见 §四.15。结论汇总：**兑现 5 行**（03.R3 三项、10.R5b–R5e 四处级联、六锚之五、命令漂移两组、13.R2 措辞）、**部分兑现 2 行**（09.R3a 的 A 侧、12.R3/02.2 三项）、**未完成 2 行**（09.R4 的 F2 复取、11.R3 部署后果）、**待 06.106 2 行**（09 域 4.4 等价证明、六锚之缺的那一枚）。**没有第 32 个子项**，也没有把任何子项拆到新叶。

两处顺序风险已消解，记录在案：①`09.R3a` 的停线项只关 B 侧存储面，A 侧义务链由 12.105 D13.6 已闭，不因 06.106 未判而把 A 侧重新打开；②`10.R5d/R5e` 的净删只降锁步（`doc_budget --tighten` 两次，21889→21878→21673），未被本叶回抬（C11.5 的 A5 读数仍是 21673）。

### C11.3 F2 完整验证输入清单（命令／模式／档位／证据保存）

冻结口径：F2 在同一候选上跑**冻结候选的完整矩阵**，一份证据目录贯穿 produce→bind→consume；入口即 `scripts/gates/preflight.sh`（它自己就是 CI 对应命令的组合），下表是该入口的逐段展开，供 F2 逐条记 exit 与产物。**不新增命令，不改界。**

| # | 命令（工作树根，参数原样） | 模式/档位 | 证据与判据 |
|---:|---|---|---|
| 1 | `env TAGENT_RUNTIME_EVIDENCE_DIR="<ABS>/f2-<run>" bash scripts/preflight.sh` | 全链；禁用 `--skip-smoke`/`--skip-event-long`（用了即"不具备完整 MR 等价证据"） | 打印的证据目录即后续所有步骤的 `$EV`；目录一跑一份不覆盖 |
| 2 | `python3 scripts/gates/test_check_config_liveness.py`；`python3 scripts/gates/check_config_liveness.py` | 配置链两钉 | 两者 exit 0；第二装载/发布路径出现即红 |
| 3 | `python3 scripts/scenarios/run_runtime_scenarios.py --scope deterministic` | golden deterministic（mock） | exit 0；差异按三轴打印 |
| 4 | `go test ./... -count=1`；`(cd examples/wechat-bot && go test ./... -count=1)`；`(cd examples/cli-host && go test ./... -count=1)` | 三个 module 各自全量 | 各 exit 0；根 `./...` 不覆盖示例 module |
| 5 | `(cd examples/wechat-bot && go test -tags smoke ./... -run TestLocalSmoke -count=1 -timeout 75m)` | 独立 module 的 smoke；需具体授权 | 未授权时如实记未跑，不以缺档换绿 |
| 6 | `python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10 --out "$EV/event-short"` | **十短**：`repeat 10`、29 顶层父测、135 具名子锚，每次独立进程 | 10 份 run 产物齐全；缺片/零命中不算通过 |
| 7 | 六长（六片各一进程）：`seed ∈ {1,7,42} × window ∈ {256,32768}`，`--scope event-long --seed <s> --window <w> --repeat 1 --timeout 6270 --generation-logs "$EV/event-long/seed-<s>-window-<w>/generations" --out "$EV/event-long/seed-<s>-window-<w>"` | **六长**；单片 1200 输入/120 重启，六片合计 7200/720（**单片不得写成总量**） | 六份产物齐全；`TAGENT_CALIBRATION_RUN_DIR` 与 `--generation-logs` 指向同一目录，失败轮原样留 |
| 8 | `python3 scripts/gates/check_event_integrity_mutations.py --all --json --out "$EV/mutations"` | 全量冻结变异（只启用已证实变异） | `killed` 必须有具名业务断言命中；`--mutation` 子集不是等价 |
| 9 | `python3 scripts/gates/check_runtime_evidence.py --contract golden/runtime/event_integrity_scenarios.json --bundle "$EV" --repo-root . --json` | 整包绑定**当前树**（不是自身自洽） | exit 0；缺长跑分片时本步如实跳过并保留"不完整"陈述 |
| 10 | `env TAGENT_RUNTIME_EVIDENCE_DIR="$EV" bash scripts/gates/lint.sh`；`bash scripts/gates/archive-preflight.sh --evidence "$EV"` | 消费面：DoD 读上面的变异产物；归档读同一 `$EV` | 两命令 exit 0；DoD 不读登记表、不读"最近一份绿" |
| 11 | 档位：`memory`／`journal`／`localfile` 别名／`file` | 06.105 实测：memory/journal 绿；localfile 别名经 `memory/ledger` 引擎矩阵七子案 ×10 绿（`TestStorageCapabilities` 内**无别名子案**＝已记缺口）；`file` 真 rustviking：`command -v rustviking` **exit 1（ABSENT）** → 具名 skip ×10、**未验**，不用替身 | 每档单列读数；skip 不计通过 |
| 12 | 冷热分列：冷编译／冷开档／热态各自独立 `GOCACHE`/`GOTMPDIR`/`TMPDIR`，不删共享缓存 | 06.105 实测：冷编译 9525 ms；冷态代际墙钟 ≈ 热态 1.35×、计数面一致 | 冷热不同条件不得互相代替；失败轮保留 |
| 13 | 计量隔离：性能锚与正式计时**串行**；`-race` 数据不混入时长比较 | 12.105 首轮两条并发致性能锚红、隔离复跑转绿＝并发污染实证 | 计时期间不得并行其它单（本叶也因此只跑 Python/文档门，不跑任何 Go 测试） |
| 14 | 三观测轴：拒 cut 持续时长／期间新增事实数／提交等 fold | **未验**（12 域 D8.7.1-⑦ 如实登记） | F2 必取；06.106 亦可判其归属，但不得因未列而视作已测 |
| 15 | flake 清单：`TestNativeConversationRoundTrip`（`tests/integration/execution`） | 签名 `native_conversation_roundtrip_test.go:93: settled observation(s) of [call-1] never reached a model request in 10s`；候选 5/10、HEAD 树 2/10、单跑 3/3 绿＝负载诱发 | 失败样本保留；不 skip、不改超时、不并入正式计时 |
| 16 | boot/total 判决位 | **待 06.106**：06.105 深 252 实测 2.0–4.7× 参照深度（保留的全日志 CRC 线性项）；A1-cold 单轮 1.58–1.68× 未复现（31 次中 1 次） | 界未放宽；判决只能是"已归因闭合"或"留 F2"，不得自行改界 |
| 17 | 证据保存 | 固定绝对 `$EV`；记 argv/cwd/env/工具版本/源码身份；日志无终局、只有父 pass、子测试自报 pass 均不算通过 | 源码身份用 `git rev-parse HEAD` + `git cat-file`（index 不可用）；bundle 需十短＋六长＋变异齐 |

**F2 前的已知阻断（不得绕过）**：`python3 scripts/gates/check_dod_coverage.py` 现在 exit 1，原因是判据源丢失而非判据不满足——`criteria_source.paths` 指向的 `openspec/changes/runtime-recovery-ideal/design.md` 在 2026-11-21 重整中已不含 `- **P0 …**：` 形式的判据行（`grep -c '^- \*\*P[0-9]' design.md` → **0**），`*-runtime-recovery-ideal` 归档件也不存在，故报 `the criteria source states no criterion … matches none of …`。判据原文 9 行仍在 `history/7b0cde16-before-replan/design.md`（`grep -c` → 9）。回填归 **13.101→F1**；回填前 F2 的 lint 段必红，**不得**用删判据、手填 `mutation_verdicts` 或抬 baseline 换绿。

### C11.4 唯一规范合并输入

#### C11.4.1 规则（机器可判，先冻结）

对每条 delta requirement 与同名主 spec requirement 比较（正文去反引号、按行 strip、按 `。.!?` 切句，场景名取 `^#### Scenario:`）：

- 主能力目录不存在且操作为 ADDED → **并入（新建能力）**；操作为 MODIFIED/RENAMED → **拒（缺源）**（CLI 自己也会拒："target spec does not exist; only ADDED requirements are allowed for new specs"）。
- 主 spec 无该标题且操作为 ADDED → **并入（新增要求）**；操作为 REMOVED → **拒（缺源）**。
- 标题在主、正文归一化后逐字相同 → **并入（无操作）**。
- 主句集 ∪ 主场景名集 ⊆ delta 侧 → **并入（delta ⊇ 主）**：允许 delta 整体替换，主侧断言面零丢失。
- 反向包含 → **拒覆盖（主已更全/更严）**。
- 两侧各有独有句（且不属于上两类）→ **冲突拒（待裁）**：**不取最后一份**，主 spec 保持现值，delta 独有活义务入 C11.4.3 清单。

复跑该判定的最小脚本（只读，纯 stdlib；把 `ROOT` 指到仓库根即得下表）：

```python
import re, os, glob
ROOT = '/Users/pengweiye/Documents/codes/tagent'
CH, SPECS = ROOT + '/openspec/changes/runtime-recovery-ideal/changes', ROOT + '/openspec/specs'
def parse(p):
    out, cur, sec, buf = [], None, None, []
    for ln in open(p, encoding='utf-8').read().splitlines():
        m = re.match(r'^## (ADDED|MODIFIED|REMOVED|RENAMED) Requirements\s*$', ln)
        if m:
            if cur: out.append((sec, cur, '\n'.join(buf)))
            cur, buf, sec = None, [], m.group(1); continue
        m = re.match(r'^### Requirement: (.+)$', ln)
        if m:
            if cur: out.append((sec, cur, '\n'.join(buf)))
            cur, buf = m.group(1).strip(), []; continue
        if cur is not None: buf.append(ln)
    if cur: out.append((sec, cur, '\n'.join(buf)))
    return out
norm = lambda s: ' '.join(l.strip() for l in s.replace('`', '').splitlines() if l.strip())
claus = lambda b: set(x.strip() for x in re.split(r'(?<=[。.!?])', norm(b)) if x.strip() and not x.strip().startswith('#### Scenario:'))
scen = lambda b: set(re.findall(r'^#### Scenario: (.+)$', b, re.M))
for d in sorted(glob.glob(CH + '/*/specs/*/spec.md')):
    cap = os.path.basename(os.path.dirname(d))
    mp = os.path.join(SPECS, cap, 'spec.md')
    main = {t: b for _, t, b in parse(mp)} if os.path.exists(mp) else None
    for sec, title, body in parse(d):
        if main is None: v = 'NEW-CAP' if sec == 'ADDED' else 'REFUSE-NO-CAP'
        elif title not in main: v = 'ADD' if sec == 'ADDED' else 'REFUSE-NO-TITLE'
        elif norm(main[title]) == norm(body): v = 'NOOP'
        elif claus(main[title]) <= claus(body) and scen(main[title]) <= scen(body): v = 'DELTA-SUPERSET'
        elif claus(body) <= claus(main[title]) and scen(body) <= scen(main[title]): v = 'MAIN-NEWER'
        else: v = 'CONFLICT'
        print('%-30s %-32s %-8s %-6s %s' % (d.split('/')[-3], cap, sec, v, title))
```

#### C11.4.2 58 条 delta requirement 的逐条判定（冻结）

分布：**并入 43**（新建能力 23＋新增要求 14＋delta⊇主 4＋逐字无操作 2）／**拒 15**（冲突 10＋主更新 2＋缺源 3）。合计 58，与 C8.0 的"58 Requirement"逐数相等。

| # | 域 | 能力 | 操作 | Requirement | 判定 |
|---:|---|---|---|---|---|
| 1 | 01-three-hooks-golden | three-hooks-extensibility | ADDED | 装配根无具体特性硬编码 | 并入（新建能力） |
| 2 | 01-three-hooks-golden | three-hooks-extensibility | ADDED | 特性经插件注册表动态挂载 | 并入（新建能力） |
| 3 | 01-three-hooks-golden | three-hooks-extensibility | ADDED | plugins 声明有生产消费方 | 并入（新建能力） |
| 4 | 01-three-hooks-golden | three-hooks-extensibility | ADDED | 守护门硬编码即红 | 并入（新建能力） |
| 5 | 02-plugin-dynamic-mount | plugin-mounting | ADDED | 特性经 builtin 插件目录挂载，默认启动流程注册 | 并入（新建能力） |
| 6 | 02-plugin-dynamic-mount | plugin-mounting | ADDED | `plugins:` 声明定制内置并响亮失败 | 并入（新建能力） |
| 7 | 02-plugin-dynamic-mount | plugin-mounting | ADDED | 装配根无特性硬编码 | 并入（新建能力） |
| 8 | 03-compression-configurable | compression-pipeline | MODIFIED | 压缩的判定与执行各成事实 | **拒**覆盖（主为较新文本；delta 独有内容为机制标识/旧命名） |
| 9 | 03-compression-configurable | compression-pipeline | ADDED | 压缩恢复状态只陈述仍需继续的处理 | 并入（新增要求） |
| 10 | 04-midband-ticket-recall | event-key-visibility | MODIFIED | 事件键在请求呈现面注入 | **拒**覆盖（主为较新文本；delta 独有内容为机制标识/旧命名） |
| 11 | 04-midband-ticket-recall | event-key-visibility | ADDED | 中段过程以事件键票据呈现并可自主召回 | 并入（新增要求） |
| 12 | 04-midband-ticket-recall | event-key-visibility | ADDED | 真实呈现消费者认识已批准的票据 | 并入（新增要求） |
| 13 | 05-task-custody | checkpoint-state | MODIFIED | 任务终态有界恢复 | **拒**（缺源：无主能力） |
| 14 | 05-task-custody | session-checkpoint-material | MODIFIED | 任务终态由 checkpoint 切片承载并有界恢复 | **冲突拒**（两侧各有独有活义务，待裁） |
| 15 | 06-bounded-open-index | bounded-recovery | ADDED | 历史事实保持身份、字节与提交顺序 | 并入（新建能力） |
| 16 | 06-bounded-open-index | bounded-recovery | ADDED | 派生索引失效不改变真实读取答案 | 并入（新建能力） |
| 17 | 06-bounded-open-index | bounded-recovery | ADDED | 完整性和只读保证不因加速降低 | 并入（新建能力） |
| 18 | 06-bounded-open-index | bounded-recovery | ADDED | 原文召回维持授权与分段保真 | 并入（新建能力） |
| 19 | 06-bounded-open-index | bounded-recovery | ADDED | 压缩恢复读取原产物而不重生成 | 并入（新建能力） |
| 20 | 06-bounded-open-index | bounded-recovery | ADDED | 真实读取成本按完整操作归因 | 并入（新建能力） |
| 21 | 06-bounded-open-index | bounded-recovery | ADDED | 外部转换完整保留事实并拒绝不完整目标 | 并入（新建能力） |
| 22 | 06-bounded-open-index | bounded-recovery | ADDED | 完整成本验证不依赖常开计量 | 并入（新建能力） |
| 23 | 07-governance-custody | checkpoint-state | MODIFIED | 框架统一 checkpoint 事实承载可恢复状态 | **拒**（缺源：无主能力） |
| 24 | 07-governance-custody | session-checkpoint-material | MODIFIED | governance 恢复读切片而非全流扫 | 并入（delta ⊇ 主：主句与主场景全含） |
| 25 | 08-fact-retirement | storage-lifecycle-completion | REMOVED | TTL check uses actual event timestamp | **拒**（缺源：主 spec 无此标题） |
| 26 | 08-fact-retirement | storage-lifecycle-completion | MODIFIED | 保留保护与回收协调 | 并入（逐字相同＝无操作） |
| 27 | 09-acceptance-real-measure | bounded-recovery | ADDED | boot 验收 SHALL 量四真量 | 并入（新建能力） |
| 28 | 09-acceptance-real-measure | bounded-recovery | ADDED | 度量探针 SHALL NOT 扰动 boot | 并入（新建能力） |
| 29 | 09-acceptance-real-measure | bounded-recovery | ADDED | 验收夹具 SHALL 多代多任务 | 并入（新建能力） |
| 30 | 09-acceptance-real-measure | bounded-recovery | ADDED | S2 症状直译锚 SHALL 第一天红 | 并入（新建能力） |
| 31 | 09-acceptance-real-measure | bounded-recovery | ADDED | 负锚 SHALL 使全史读回归红 | 并入（新建能力） |
| 32 | 09-acceptance-real-measure | bounded-recovery | ADDED | 分阶段恢复成本 SHALL 分区间可复算 | 并入（新建能力） |
| 33 | 09-acceptance-real-measure | bounded-recovery | ADDED | 验收 SHALL 以原始失败凭据固定回归面 | 并入（新建能力） |
| 34 | 09-acceptance-real-measure | bounded-recovery | ADDED | 最终候选 SHALL 在完整矩阵上同条件放行 | 并入（新建能力） |
| 35 | 10-contract-sync-dead-code | code-documentation | ADDED | 主 spec 与代码同向 | 并入（新增要求） |
| 36 | 10-contract-sync-dead-code | code-documentation | ADDED | legacy 条款按语义单元退役 | 并入（新增要求） |
| 37 | 10-contract-sync-dead-code | code-documentation | ADDED | golden 记载与代码同步 | 并入（新增要求） |
| 38 | 10-contract-sync-dead-code | code-documentation | ADDED | 死生产者清零且覆盖不丢 | 并入（新增要求） |
| 39 | 10-contract-sync-dead-code | code-documentation | ADDED | 兼容读法退役须先审计消费者 | 并入（新增要求） |
| 40 | 10-contract-sync-dead-code | code-documentation | ADDED | 判据与完成账 SHALL 双向对账 | 并入（新增要求） |
| 41 | 10-contract-sync-dead-code | code-documentation | ADDED | 计划余项 SHALL 逐条处置并给残留零证 | 并入（新增要求） |
| 42 | 10-contract-sync-dead-code | code-documentation | ADDED | 交接与归档前验证 SHALL 闭证 | 并入（新增要求） |
| 43 | 11-atomic-fact-commit | event-segment-store | MODIFIED | 后端不可变与隔离一致性 | 并入（逐字相同＝无操作） |
| 44 | 11-atomic-fact-commit | fact-vocabulary | MODIFIED | 事实类别契约 | 并入（delta ⊇ 主：主句与主场景全含） |
| 45 | 11-atomic-fact-commit | partition-local-storage-access | MODIFIED | 同步屏障与磁盘格式不变 | 并入（delta ⊇ 主：主句与主场景全含） |
| 46 | 12-consumer-state-lifecycle | config-hot-reload | MODIFIED | 执行代绑定完整性 | **冲突拒**（两侧各有独有活义务，待裁） |
| 47 | 12-consumer-state-lifecycle | consumer-loop-convergence | MODIFIED | 重入输入恰一次 | **冲突拒**（两侧各有独有活义务，待裁） |
| 48 | 12-consumer-state-lifecycle | runtime-resource-ownership | MODIFIED | 递归获取责任与关闭责任唯一 | **冲突拒**（两侧各有独有活义务，待裁） |
| 49 | 12-consumer-state-lifecycle | session-checkpoint-material | MODIFIED | 派生态有界且不删除已提交事实 | **冲突拒**（两侧各有独有活义务，待裁） |
| 50 | 12-consumer-state-lifecycle | session-checkpoint-material | MODIFIED | `Stateful` 收编消费者读态且不新增挂点 | **冲突拒**（两侧各有独有活义务，待裁） |
| 51 | 12-consumer-state-lifecycle | session-checkpoint-material | MODIFIED | 任务终态由 checkpoint 切片承载并有界恢复 | **冲突拒**（两侧各有独有活义务，待裁） |
| 52 | 12-consumer-state-lifecycle | session-checkpoint-material | MODIFIED | governance 恢复读切片而非全流扫 | **冲突拒**（两侧各有独有活义务，待裁） |
| 53 | 13-verified-completion | architecture-guardrails | MODIFIED | 检查资产区分三类证据 | 并入（delta ⊇ 主：主句与主场景全含） |
| 54 | 13-verified-completion | architecture-guardrails | MODIFIED | 故障变异必须由具名业务断言杀死 | **冲突拒**（两侧各有独有活义务，待裁） |
| 55 | 13-verified-completion | architecture-guardrails | MODIFIED | 质量门迁移不得放宽判据 | **冲突拒**（两侧各有独有活义务，待裁） |
| 56 | 13-verified-completion | architecture-guardrails | ADDED | 默认装配与调用方选择可验证 | 并入（新增要求） |
| 57 | 13-verified-completion | architecture-guardrails | ADDED | 历史发现与已知身份读取分别验收 | 并入（新增要求） |
| 58 | 13-verified-completion | architecture-guardrails | ADDED | 规范合并与归档拒绝有真实消费者 | 并入（新增要求） |

#### C11.4.3 拒收项独有义务清单（不得静默丢弃）

**A. 缺源拒（3 条，主 spec 无落点）**

1. `05/specs/checkpoint-state`：MODIFIED「任务终态有界恢复」——`openspec/specs/checkpoint-state` 不存在。该 delta 首段自述"保留为历史时点证据"，现行条款由 `session-checkpoint-material` 承接（05 的另一条 MODIFIED）。
2. `07/specs/checkpoint-state`：MODIFIED「框架统一 checkpoint 事实承载可恢复状态」——同上，自述为历史时点证据。
3. `08/specs/storage-lifecycle-completion`：REMOVED「TTL check uses actual event timestamp」——主 spec 无此标题（该能力已由 10.R5 系列改写）。该 delta 的另一条 MODIFIED「保留保护与回收协调」已逐字在主（无操作），故 **08 的 delta 整体不并入**。

**B. 主更新更严（2 条，主 spec 为较新文本）**

1. `03`「压缩的判定与执行各成事实」：主多「摘要提示词 SHALL 按该预算陈述字节上界」与"重试读回已提交产物、预算同源不等于模型重述同文"；delta 独有内容为机制标识（`FoldWindow.SummaryBudgetBytes`、`ErrNoSummaryModel`、`DefaultSummaryBudgetBytes`）与旧命名（`history_summary`），按 10 域 5.1.7"机制细节落场景或 wiki 不落要求正文"本就不该进正文。
2. `04`「事件键在请求呈现面注入」：主多两场景——「空呈现不单独呈现票据」「呈现不写回投影」（10 域 5.7.3 要求补的三场景之二）；delta 独有内容是 `assembly/request_test.go:274-276` 之类载体坐标，属被有意移出正文的部分。

**C. 冲突拒（10 条，两侧各有独有活义务；主 spec 保持现值）**

| 域/要求 | 主 spec 独有（条数） | delta 独有活义务（要点） |
|---|---|---|
| 05「任务终态由 checkpoint 切片承载并有界恢复」 | 0（主句全被 delta 含） | 清减判据＝按身份判重可答（MUST NOT 以 TTL 删未完义务）；「未陈述」与「陈述空集」载荷可区分；任务账户为全局账、会话切面不被要求承载任务账；**陈述成本按有效工作规模计费**（主 spec 全无「有效工作」字样） |
| 12 同标题（860 字节版） | 6 场景＋7 句 | 未陈述/空集/损坏三分；全局任务账户不入会话 cut；**无 tier 模式保留既有内存行为** |
| 12「派生态有界且不删除已提交事实」 | 4 场景＋7 句（开放项＋水位、冷档折回运维前置、有界化不打断全史读者） | 原字节/身份/原序作为原子提交单元；领域等价重交不重复占号；切片随有效工作计费 |
| 12「`Stateful` 收编消费者读态」 | 2 场景＋2 句（reducer/emitter、只注册一个切片） | 范围按真实状态依赖决定（不指定队列/游标/Repeat/全局拒 cut）；成功快照＋后续重放合计覆盖；未应用事实不得既算已覆盖又失去补应用路径 |
| 12「governance 恢复读切片而非全流扫」 | 4 场景＋5 句 | 提交未确认的审批/目标修改不作为已生效依据；审批原身份与事实有效期、拒绝与过期不因重启复活；Query/Count/List 承诺历史或具名拒绝不完整读取 |
| 12「执行代绑定完整性」 | 2 场景＋2 句（wrapper parentProjection、被钉委派按回合身份取答案） | 消费者集合变化经既有声明面处理、无承接则拒绝发布并保留旧代对象；builtin 与扩展同一生命周期语义（**不扩大当前可禁用/替换范围**） |
| 12「重入输入恰一次」 | 1 场景＋1 句 | 同一通知身份跨重启/重投至多一条逻辑消费收据（不承诺外部 exactly-once）；提交/应用/进程内交接/投递义务/attempt/收据各按实际完成条件确认 |
| 12「递归获取责任与关闭责任唯一」 | 1 场景＋2 句（候选级资源日志覆盖递归部分成功、父子接管转移责任） | 持可恢复态的消费者不因卸载/禁用/替换丢失未完义务；操作失败保留原持有者对象与资源，不先删后报 |
| 13「故障变异必须由具名业务断言杀死」 | 1 句（"未证实候选不计 killed；缺映射使门失败，不为凑五类虚构变异"） | 竞态例外须引用与竞态门同一签名表、按上游版本失效即删；控制树必须真跑（有通过终局且无跳过）；杀死归属限"该测试该断言的同一条失败记录"；变异只证其实际命中的性质 |
| 13「质量门迁移不得放宽判据」 | 1 句（"全量产品门保持未完成，不删除旧门"） | 放行入口四处（本地预检/CI 必跑/归档/合并）任一未消费即不准出；归档在证据核验后进行、直接 archive 或 mv 不得绕过；必需检查未配置须如实报告未生效；判据收紧不得以迁移为名放宽 |

**这 10 条是主 spec 欠账，不是 delta 的错**：主 spec 在 2026-11-21 由 10 域同步成"较新且已删机制细节"的文本，但 05/12/13 这批 delta 携带的活义务没有全部进去（例：「有效工作」「全局任务账户」「会话 cut」「无 tier 模式」在主 spec 全库 0 命中）。处置只有两条路，**都超出本叶授权**：①在主 spec 补这些义务（规格作者活，须 13.106/F1 记账）；②裁定某条义务已由他处承载或不再需要（须具名依据）。本叶**不改主 spec、不覆盖、不并最后一份**。

#### C11.4.4 F4 归档程序（在隔离副本上已实跑）

1. **二级 change 不被 CLI 寻址**（`openspec validate 13-verified-completion --json` → exit 1 `Unknown item`，C2 第 7 行实测；本叶复核 `openspec list --json` 只列顶层 change）。故 F4 的输入是把二级 delta 按本清单**提升为顶层 change**（结构无损，CARRYOVER 前言），或把同一能力的 delta 合并进一个合成 change；`openspec/changes/archive/**` 不动。
2. **顺序**：先顶层 `orchestration`（8 条 ADDED，标题与主 spec 66 条无碰撞），再各域并入项；拒收项**不进入**任何归档 change。
3. **隔离副本实测（消费者锚）**：把 `openspec/specs` 复制到 `/tmp/c13105/f4probe`，按清单造 9 个合成 change 并逐个 `openspec archive <name> -y`：`orchestration` **+8**、`three-hooks-extensibility` **+4**（新建）、`plugin-mounting` **+3**（新建）、`bounded-recovery` **create +16**（06 的 8＋09 的 8，合一个输入）、`session-checkpoint-material` **~1**（07 governance）、`code-documentation` **+8**、`fact-vocabulary` **~1**、`partition-local-storage-access` **~1**、`architecture-guardrails` **+3 ~3**；九次均打印 `Specs updated successfully.`。
4. **实跑后的结构核对**：`bounded-recovery` 16 requirement、重复 0；`code-documentation` 12→20 且既有 12 块逐字不变；`orchestration` 66→74 同理；`three-hooks-extensibility` 0→4、`plugin-mounting` 0→3；6 条替换项按**场景名**核对零丢失（`architecture-guardrails` 2→7/4→10/1→6、`fact-vocabulary` 2→5、`partition-local-storage-access` 1→5、`session-checkpoint-material` governance 4→8，`missing=[]`；两处场景正文只有空行与句末标点差异，无断言弱化）；未进入合并输入的 `compression-pipeline`、`event-key-visibility`、`storage-lifecycle-completion`、`event-segment-store` **逐字节不变**；九张被触及的能力页 `openspec validate <cap> --strict` 全部 exit 0。
5. **拒收项实跑复核**：`checkpoint-state` 两份 → `target spec does not exist; only ADDED requirements are allowed for new specs` ＋ `Aborted. No files were changed.`；08 的 delta → `REMOVED failed for header "### Requirement: TTL check uses actual event timestamp" - not found` ＋同上。三者主 spec 字节不变。
6. **不改主树**：以上全部在 `/tmp` 副本执行；主树 `openspec/changes/archive/**` 未触碰，主 spec 未改。

#### C11.4.5 新增测试与其三锚（`scripts/gates/test_sync_runtime_specs.py`）

- 三例（各恰 1 顶层命中，`-k` 选择器逐字符一致）：`test_compatible_requirement_union_is_synced`（合法并集：两次 ADDED 落到同一新能力＋ADDED 落到既有能力旁边；并断言本仓"同一能力被两份 ADDED delta 喂入时标题互斥"这一并集前提，且**非空转**——当前 inspected=1（`bounded-recovery`））；`test_conflicting_requirement_text_is_refused`；`test_missing_rename_source_is_refused`。全文件 `Ran 3 tests … OK`。
- **工具身份冻结**：`setUpModule` 跑 `openspec --version`，非 `1.2.0` 即失败（CLI 固定值同 `docs/OPERATION.md`）。
- **两条必须同读的事实**：①`openspec archive` 在**所有拒绝路径上仍 exit 0**（打印具名错误后 return），故断言读"具名拒绝文本＋主 spec 字节不变＋change 未被移入 archive"，**不拿 exit code 当判词**；②CLI 对 MODIFIED 是整块替换（last-write-wins），**没有语义冲突检测**——这正是 C11.4.3 C 类必须由清单拒绝、而不是交给 CLI 的原因。
- **变异（同批控制绿）**：并集例把第二个 ADDED 的标题改成与第一个同名 → CLI 拒绝（`ADDED failed for header … - already exists`）、用例 exit 1；冲突例与缺源例在"permissive 同步工具"shim 下（`--version` 答 1.2.0、`archive` 直接追加并打印成功）分别 exit 1（`'Requirement present in both MODIFIED and ADDED' not found`／`'RENAMED failed' not found`）。未变异控制：全文件 exit 0。

### C11.5 门 exit、旁证门与文档/golden 处置

**六个正锚（本叶当场复跑，原始 exit）**

| 命令 | exit | 关键读数 |
|---|---:|---|
| `openspec validate runtime-recovery-ideal --strict` | **0** | `Change 'runtime-recovery-ideal' is valid` |
| `bash scripts/gates/check-openspec.sh` | **0** | `Totals: 158 passed, 2 failed (160 items)`；2 failed 为 in-flight change `event-driven-consumption`／`post-training-data-loop`（非门控）；`OK: all main specs pass validate --strict` |
| `python3 scripts/gates/check_spec_hygiene.py` | **0** | `specs=151 requirements=752 violations=242 baseline=250 stale=8 errors=0` |
| `python3 scripts/gates/check_doc_links.py` | **0** | `42 document(s), 130 link(s), 56 anchor(s), 0 error(s)` |
| `python3 scripts/gates/check_doc_budget.py` | **0** | baseline＝measured＝231 files／21673 lines，`slack 0` |
| `bash scripts/gates/gen_godoc.sh --check` | **1 → 0** | 重生成前 7 页不一致；重生成后 `docs/api matches the source (82 packages)` |

**旁证门（不在本叶六门内，但为判定"是否已清"而跑）**：`check_dead_producers.py` **0**（`every one of 3513 production function(s) … has a consumer`）；`check_concept_budget.py` **0**（`bindings 57／new 57／removed 34／relocations 5／net_facts 3`）；`check_dod_coverage.py` **1**（判据源缺失，见 C11.3 末段——**登记，不修**）。

**文件级处置**

| 对象 | 处置 | 证据／理由 |
|---|---|---|
| `docs/api/*.md` | **重生成**（7 页变） | 逐页 diff 只含本变更各叶的注释落点：`assembly`（settle 选项与投递事实谱系）、`governance`（Declare/Resolve 先写链后改内存）、`memory/ledger`（新增 `EachIndexNewestFirstFrom`、删 `CommittedIndexEntry`）、`runtime/agent`（结果材料进范围 intake）、`runtime/state`（Attempt 读失败具名）、`runtime/task` 与 `runtime/task/approval`（状态/收据注释）。76 页与 HEAD 逐字节相同 |
| `docs/wiki/agent/event-flow.md`、`docs/wiki/platform/org-hot-reload.md` | **零改动** | 03.R3 两项叙述滞后已在位：`DefaultAgeSummaryTextLimit` 在 `docs/wiki/` **零命中**；`org-hot-reload.md:45` 已列 `summary_budget_bytes` |
| `docs/OPERATION.md` | **零改动** | 与 HEAD 逐字节相同（`git cat-file -p HEAD:docs/OPERATION.md` diff 为空）。新增的 `archive-preflight` 入口确未在该文出现，但无旧要求点名它、且该文在预算内（slack 0）：增行需具名上调、删行属散文对冲，二者都不属于本叶授权 → 记 C11.6 未完成项，不冒充已同步 |
| `golden/runtime/architecture.json` | **零改动（`IndexEntry` 行保留）** | 裁定 B 的三件是一体：`check_concept_budget.py` 对"新增名无绑定"直接 error（D1 段的 `for name in new_names` → `bindings.get(name)` 分支：无绑定即 error），故**只删 binding 会让门红**；删类型本体（`memory/ledger/segment_store.go` 的 `type IndexEntry struct` doc 块＋字段）不在本叶写面（C7.2 的"唯一删除"是压缩端口）。两者必须同批 → 记 C11.6，待补签 |
| `golden/runtime/durable_truth.json` | **零改动** | 17 个 surface 与源码前缀一致：`generationRetirePrefix="gen-retire:"` 实测活于 `assembly/run.go:1667`（08「明确不删」之一）；`tomb/relocat/RetireFacts` 命中 0 |
| `golden/runtime/dod_predicates.json` | **零改动** | `criteria_source` 失配是**判据源**丢行（C11.3），不是 registry 写错；改它＝伪造判据或手填判词，禁止 |
| 主 spec / delta | **零改动** | 合并清单冻结在 C11.4；冲突与缺源一律拒绝，不覆盖、不并最后一份 |

### C11.6 未完成与待裁（与 `CARRYOVER` §四.16 同号）

R1（03.R5 端口合并）／R2（11.R3 部署后果）／R3（12.R3 ①③）／R4（06 域缺锚）／R5（09 域 4.4）／**R6（P0–P8 判据源丢失，`check_dod_coverage.py` exit 1）**／**R7（`IndexEntry` 死类型＋binding 待补签）**／R8（04.R2）／R9（三观测轴未验）／R10（boot/total 与冷尖峰）／R11（F2 与两正式入口未跑）／R12（远端 CI 与保护未生效）／**R13（`docs/OPERATION.md` 未记 archive-preflight 入口）**。本叶新查出的是 R6、R7、R13；其余由各叶记录承接。

### C11.7 偏离与自决（凡"没问但自己定了"的事）

1. **判定脚本放 `/tmp` 而非入库**：C11.4.1 的规则与脚本是一次性对账工具，未成为受审门；把它入 `scripts/gates/` 需要独立签发（C7.1 只签了 `test_sync_runtime_specs.py`）。
2. **新测试含一条仓库侧前提检查**：并集正锚里加了"同一能力被两份 ADDED delta 喂入时标题互斥"并断言 inspected≥1。理由：这是"合法并集"在本仓的真前提，不检查则该正锚只验证合成夹具；已用"标注重置"变异证明它咬得动（C11.4.5）。不改变三例数量，`-k` 各仍恰 1。
3. **`setUpModule` 冻结 CLI 版本**：C7.5 要求"冻结工具版本及实际 argv"，把它做成模块级前置而非第 4 个测试，避免多出一个未签名的选择器。
4. **负锚断言读产物状态而非 exit code**：实测 CLI 拒绝也 exit 0（C11.4.5），照抄"exit≠0 即拒绝"会得到假绿；这是对"命令存在≠拒绝面存在"（C2 第 6 行）同类问题的处置。
5. **不删 `IndexEntry` binding**：见 C11.5；宁可留一条"未完成"，也不做会让 D1 门红的半删。
6. **不改 `docs/OPERATION.md`**：见 C11.5；预算 slack 0，增行要具名上调，属他人授权。
7. **未跑任何 Go 测试或构建**：本叶验证面只有 Python/文档门（并行调度约定：06.106 成本审独占计量）；`gen_godoc.sh` 内部调用的 `go list`/`go doc` 属文档门，不是测试，也不产生被测进程计时。

### C11.8 写面自查与越界检查

- 生产/测试/CI/主 spec/delta/golden/OPERATION/wiki/archive：**零改动**。逐文件核对方式＝`git cat-file -p HEAD:<path>` 与工作树比对（index 不可用）＋针对性 grep。
- 本叶改动文件＝`CARRYOVER.md`、本文件、`changes/13-verified-completion/tasks.md`、`scripts/gates/test_sync_runtime_specs.py`（新增）、`docs/api/` 7 页（生成物）。
- 未 git 写：无 `add`/`commit`/`push`/`archive`；`openspec/changes/archive/**` 未被触碰（隔离副本里的 archive 在 `/tmp/c13105/f4probe/openspec/changes/archive/`）。
- **13.105 不勾**：本行完成谓词含"CARRYOVER 行落地后由编排者核勾"；B 侧 35 行仍标"待 06.106 判决"。

### C11.9 comment_policy 清理（派令原称 C11.6；该号已被「未完成与待裁」占用，顺延至此）

> **产生方式**：`bash scripts/lint.sh --only policy` 的 comment_policy 段在 2026-11-22 复跑为 exit 1 —— `go run ./scripts/comment_policy -v . examples/wechat-bot examples/cli-host` 报 **77 finding、5 槽基线全 0**（`scripts/comment_policy/baseline.json` 为空基线，故 77 条全部计为本变更新增）。本叶只改注释文本形状：**零代码、零断言、零字符串字面量改动**；**不改任何 checkbox**。

#### C11.9.1 工具身份与派令勘误

1. 派令给的 `python3 scripts/gates/check_comment_policy.py` **不存在**。该门是 Go 程序 `./scripts/comment_policy`（`-v` 逐条、`-no-baseline` 只测不棘轮、`-strict` 空基线），由 `scripts/gates/lint.sh` 两处调用：policy 段的 ratchet（扫描集 `. examples/wechat-bot examples/cli-host`）与产品包段零容忍（`assembly api runtime memory features tool internal cli`）。本叶按真实入口执行。
2. 派令预留的「外来未跟踪面文件命中则不改」条件**未触发**：77 条全部落在本变更内的 Go 产品/测试文件，**外来面 0 条**（`scripts/gates/{check_event_semantics_symptoms,runtime_budgets,test_check_runtime_symptoms,check_no_prefix_criterion,test_check_no_prefix_criterion}.py` 与 `openspec/changes/{runtime-architecture-convergence,no-prefix-criterion}/` 零命中）。
3. 派令给的目标 `bash scripts/lint.sh --only policy` **exit 0 不可能达成**，原因不在 policy 段：policy 段两小节均绿（见 C11.9.3），但脚本在随后的 `checker selftests` 段红在 `test_check_runtime_symptoms.SymptomCheckerTest::test_registry_matches_the_module`（注册表多点 `TestLocalSmokeSymptomBootDeployed`）。这两个文件是 ORCHESTRATION ②授权第 5 条点名禁触的外来未跟踪面，10 域 design 亦已登记同一红（`10-contract-sync-dead-code/design.md` 的 lint 不宣称段）。本叶零触碰、不吸收不屏蔽，如实分列"policy 段绿／脚本整体红在外部面"。

#### C11.9.2 逐条 finding → 处置（77 条）

四条规则与处置代码：**R**＝按形状重写（一句意图行 + 可选 `- ` bullet + 索引行，测试头注每行 ≤160 字符）；**F**＝体内自由注释压缩为一条或两条 bullet 并入所属声明头注后删除；**D**＝体内注释所述事实已由头注 bullet 或紧邻断言消息承担，直接删除；**M**＝注释块移位（错位的整段头注移到其函数前）。

| # | 位置（file:line） | 规则 | 处置 |
|---:|---|---|---|
| 1 | assembly/checkpoint_test.go:2846 | test-doc-not-one-sentence | R（意图行合一；5 bullet 各保持单行，另增 1 条 fixture bullet 承接 #3） |
| 2 | assembly/checkpoint_test.go:2868 | free-standing | D（头注 bullet「窗口内落地的 task 由 tail 命名」已述） |
| 3 | assembly/checkpoint_test.go:2925 | free-standing | F（bullet「the fixture drops the quiet point of a filed cut whose slices stay in the root」） |
| 4 | assembly/checkpoint_test.go:2956 | free-standing | D（头注 bullet「quiet point 命名无 filed cut ⇒ nil」已述） |
| 5 | assembly/checkpoint_test.go:5083 | doc-not-brief | R |
| 6 | assembly/checkpoint_test.go:5095 | test-doc-not-one-sentence | R（2 条缠绕 bullet 各拆为 2，补 2 条承接 #7/#8） |
| 7 | assembly/checkpoint_test.go:5136 | free-standing | F（bullet「inside the window…」） |
| 8 | assembly/checkpoint_test.go:5168 | free-standing | F（bullet「the control settles one more task past the cut…」） |
| 9 | assembly/checkpoint_test.go:5230 | free-standing | F（矩阵段头注折叠为 2 条 bullet 入 #10 的测试头注） |
| 10 | assembly/checkpoint_test.go:5632 | doc-not-brief | R（8 段散文 → 14 条 bullet） |
| 11 | assembly/checkpoint_test.go:5661 | test-doc-not-one-sentence | R |
| 12 | assembly/commit_test.go:1061 | test-doc-not-one-sentence + test-doc-line-too-long | R（意图行合一；169/167 字符的两条 bullet 各拆为 2） |
| 13 | assembly/commit_test.go:1096 | free-standing | D（头注 bullet「chain plus binary digits」已述） |
| 14 | assembly/compress_checkpoint_test.go:635 | doc-not-brief | R（意图行合一 + 9 条 bullet） |
| 15 | assembly/compress_checkpoint_test.go:654 | test-doc-not-one-sentence | R |
| 16 | assembly/facts.go:112 | free-standing | F（2 条 bullet：两次身份读各自含义 + 两读均在 ordered unit 之前） |
| 17 | assembly/facts.go:132 | free-standing | F（bullet「FactCommitted: …」） |
| 18 | assembly/facts.go:134 | free-standing | F（bullet「FactAlreadyPresent: …」） |
| 19 | assembly/facts.go:143 | free-standing | F（bullet「FactConflict: …」） |
| 20 | assembly/facts.go:146 | free-standing | F（bullet「FactUncertain: …」） |
| 21 | assembly/facts.go:152 | free-standing | F（bullet「an outcome this entry does not classify…」） |
| 22 | assembly/facts_test.go:2026 | doc-not-brief | R |
| 23 | assembly/facts_test.go:2039 | test-doc-not-one-sentence | R（5 条 bullet + 1 条承接 #24） |
| 24 | assembly/facts_test.go:2046 | free-standing | F（bullet「the count of those reads…」） |
| 25 | assembly/facts_test.go:2068 | free-standing | D（头注 bullet「landing fence 被 parked fold 持有」已述） |
| 26 | assembly/facts_test.go:2106 | doc-not-brief | R |
| 27 | assembly/facts_test.go:2117 | test-doc-not-one-sentence | R（3 条 bullet + 4 条承接 #28/#29） |
| 28 | assembly/facts_test.go:2137 | free-standing | F（2 条 bullet：双读拆分 + landing lock 圈定） |
| 29 | assembly/facts_test.go:2171 | free-standing | F（2 条 bullet：唤醒无序 ⇒ 结果按集合读） |
| 30 | assembly/facts_test.go:2243 | doc-not-brief | R |
| 31 | assembly/facts_test.go:2255 | test-doc-not-one-sentence | R |
| 32 | assembly/host_test.go:762 | doc-not-brief | R |
| 33 | assembly/host_test.go:773 | test-doc-not-one-sentence | R（3 条 bullet + 1 条承接 #34） |
| 34 | assembly/host_test.go:786 | free-standing | F（bullet「a boot that refuses the account by name…」） |
| 35 | assembly/host_test.go:838 | doc-not-brief | R |
| 36 | assembly/host_test.go:852 | test-doc-not-one-sentence | R（5 条 bullet + 2 条承接 #37） |
| 37 | assembly/host_test.go:895 | free-standing | F（2 条 bullet：产品自带 recovery + 声明 scope） |
| 38 | assembly/read_test.go:680 | test-doc-line-too-long + test-doc-not-one-sentence | R（177 字符 bullet 拆为 2；补 1 条承接 #39） |
| 39 | assembly/read_test.go:706 | free-standing | F（bullet「the control reads the same root through a view that states no index…」） |
| 40 | assembly/read_test.go:748 | doc-not-brief | R |
| 41 | assembly/read_test.go:763 | test-doc-not-one-sentence | R（3 条缠绕 bullet 各拆为 2） |
| 42 | assembly/read_test.go:842 | free-standing | D（头注 bullet「order-line index 丢失后复述、同 lookup 同答案」已述） |
| 43 | assembly/read_test.go:1055 | free-standing | D（断言消息已述「each step enters the order at the commit that stated its own key」） |
| 44 | assembly/symptom_anchor_longrun_test.go:933 | doc-not-brief | R（含 1 条 bullet 拆分） |
| 45 | assembly/symptom_anchor_longrun_test.go:1237 | doc-not-brief | R |
| 46 | assembly/symptom_anchor_longrun_test.go:1279 | doc-not-brief | R |
| 47 | assembly/symptom_anchor_longrun_test.go:1319 | doc-not-brief | R |
| 48 | assembly/symptom_anchor_longrun_test.go:1337 | test-doc-not-one-sentence | R（1 条 83 字符 bullet 拆为 2） |
| 49 | features/evolution/governance/checkpoint_test.go:1443 | doc-not-brief | R |
| 50 | features/evolution/governance/checkpoint_test.go:1453 | test-doc-not-one-sentence | R |
| 51 | memory/ledger/journal_kv.go:295 | doc-not-brief | R（4 段散文 → 5 条 bullet） |
| 52 | memory/ledger/journal_test.go:1095 | doc-not-brief | R |
| 53 | memory/ledger/journal_test.go:1106 | test-doc-not-one-sentence | R（173 字符 bullet 拆为 2） |
| 54 | memory/ledger/journal_test.go:1148 | doc-not-brief | R |
| 55 | memory/ledger/journal_test.go:1157 | test-doc-not-one-sentence | R（173 字符 bullet 拆为 2） |
| 56 | memory/ledger/journal_test.go:1191 | free-standing | D（头注 bullet「表增长使 slot 位移」已述，fixture 选择逻辑由代码表达） |
| 57 | memory/ledger/journal_test.go:1644 | free-standing | D（头注 bullet「tombstoned 与未写 key 均读作 absence」已述） |
| 58 | memory/ledger/kv_original_store_test.go:777 | doc-not-brief | M（该段是错位头注：与下一测试头注同组，移到其函数 906 前） |
| 59 | memory/ledger/kv_original_store_test.go:794 | doc-not-name-prefixed | M（移位后组首行恢复为 `TestScopedReadResolvesTheNamedIdentity`） |
| 60 | memory/ledger/kv_original_store_test.go:794 | test-doc-not-one-sentence | R（3 条缠绕 bullet 各拆为 1 行并压缩） |
| 61 | memory/ledger/segment_store.go:1189 | doc-not-brief | R（4 段散文 → 5 条 bullet） |
| 62 | runtime/agent/consumer_test.go:2020 | free-standing | D（头注 bullet「settle line 单独成行、观察从 result fact 读回」已述） |
| 63 | runtime/state/durable.go:391 | doc-not-brief | R（3 段散文 → 8 条 bullet，事实零删） |
| 64 | runtime/state/durable_test.go:977 | doc-not-brief | R |
| 65 | runtime/state/durable_test.go:986 | test-doc-not-one-sentence | R |
| 66 | runtime/state/durable_test.go:1064 | doc-not-brief | R |
| 67 | runtime/state/durable_test.go:1073 | test-doc-not-one-sentence | R |
| 68 | runtime/task/approval/approval_store_test.go:472 | doc-not-brief | R |
| 69 | runtime/task/approval/approval_store_test.go:482 | test-doc-not-one-sentence | R |
| 70 | runtime/task/delivery_fact_test.go:11 | doc-not-brief | R |
| 71 | runtime/task/delivery_fact_test.go:21 | test-doc-not-one-sentence | R |
| 72 | tests/integration/execution/delivery_contract_test.go:193 | test-doc-line-too-long | R（240 字符 bullet 拆为 2） |
| 73 | tests/integration/execution/receipt_background_separation_test.go:30 | test-doc-line-too-long | R（176 字符 bullet 拆为 2） |
| 74 | tests/integration/execution/receipt_background_separation_test.go:230 | free-standing | D（头注 bullet「settle line 立在正文旁、不构成第二次响应」已述） |
| 75 | tests/integration/execution/receipt_background_separation_test.go:289 | free-standing | D（头注 bullet「一条通知一个身份」已述） |

分布核对：**R 48／F 16／D 11／M 2**（合计 77，与五槽基线回归数 24+21+27+4+1 逐数相等）。文件面：20 个 Go 文件，全部在本变更内；无脚本、无 CI、无主规格、无 golden。

#### C11.9.3 终态、抽验与越界

**门终态（本叶当场复跑，原始 exit）**

| 命令 | exit | 关键读数 |
|---|---:|---|
| `go run ./scripts/comment_policy -v . examples/wechat-bot examples/cli-host` | **0** | `comment_policy: 0 finding(s); 0 beyond baseline; 0 ratchet slot(s) can be lowered`（唯一附带行是既有的 `1 git-ignored Go file(s) … examples/wechat-bot/knowledge_base`，非 finding、本叶零触碰） |
| `go run ./scripts/comment_policy -no-baseline assembly api runtime memory features tool internal cli` | **0** | `0 finding(s) over [api assembly cli features internal memory runtime tool]` |
| `python3 scripts/gates/check_doc_budget.py` | **0** | `231 files / 21673 lines`，`slack 0`，`RESULT: ok` |
| `bash scripts/lint.sh --only policy` | **1（红在 policy 段之后）** | policy 段两小节均如上绿；红项＝`checker selftests` 段的外来未跟踪 `test_check_runtime_symptoms.py::test_registry_matches_the_module`（见 C11.9.1-3），本叶零触碰 |
| `go build ./...`（`GOFLAGS=-buildvcs=false`） | **0** | 无输出 |
| `gofmt -l`（上列 20 文件） | **0** | 空输出 |

**注释外零改动（机器判，非口头）**

```
go build -o /tmp/cp-codetools ./scripts/codetools
/tmp/cp-codetools comment-check --base-root /tmp/cp-before --head-root . <20 个相对路径>
→ comment-check: 20 file(s), code identical under comment strip        (exit 0)
```

`/tmp/cp-before` 是编辑前逐文件快照（20 个文件，`cp` 于 02:39:25Z），故该判据比的是**本次编辑前后**，不掺 HEAD 与工作树的既有差异（本树 HEAD 与工作树本就不等）。结论：代码、断言、字面量、import、构建标签全部逐字节相同，仅注释文本变化。

**行为抽验（`-count=1`，8 包 26 例全 PASS）**

- `./assembly`（13 例，22.093s）：`TestQuietMarkerBoundsTheTailToTheWindow`／`TestTaskSnapshotReplaysInterleavedCommit`／`TestRecoveryEntriesPreserveStateAndObligations`／`TestConfigChainDiscoveryCostsTheChainNotTheHistory`／`TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel`／`TestConcurrentCommitPreservesApplicationOrder`／`TestConcurrentSameFactKeepsOneApplication`／`TestNoTierCommitPreservesApplicationOrder`／`TestUnreadableReceiptDoesNotDriveAgain`／`TestBackflowFailureRecoversWithoutManualSettle`／`TestPredecessorQueryBoundsTheWalkToTheChain`／`TestHistoricalReadsAvoidUnrelatedHistory`／`TestRecoveryCostDetectsGrowthWithInstrumentationDisabled`
- `./memory/ledger`（5 例，3.682s）／`./features/evolution/governance`（1 例）／`./runtime/state`（2 例）／`./runtime/task`（1 例）／`./runtime/task/approval`（1 例）／`./runtime/agent`（1 例）／`./tests/integration/execution`（2 例）
- 选择器先以 `go test -list` 核过 13 个名字逐字符命中，非零匹配（避免空转 exit 0）。

**写面与未碰**

- 写面：上列 20 个 Go 文件的**注释文本**＋本文件（C11.9）。
- 未碰：任务勾选、`scripts/**`（含两个 comment_policy 调用点与外来未跟踪五件）、`.github/**`、主规格与 delta、`golden/**`、`docs/**`（含 `docs/api` 生成物：本次只改注释措辞，未重生成）、`examples/**`、git index/refs（本树 index 仍为破损态，按 ORCHESTRATION「不重建 index」保留，未 `add`/`commit`）。

### C11.10 R6/R7 修复（派令原称 C11.7；该号已被「偏离与自决」占用，顺延至此）

**R6 缺口形态**：判据源不是 registry，而是 `golden/runtime/dod_predicates.json` 的 `criteria_source.paths` 点名的 `openspec/changes/runtime-recovery-ideal/design.md`（pattern `^\- \*\*(?P<id>P\d+) (?P<title>[^*]+)\*\*：(?P<text>.+)$`）。R265 重整重写 design.md 时把 9 行 `- **P0..P8 …**：…` 判据条目整段丢失，门在读取阶段即 `GateError`：`dod_coverage: the criteria source states no criterion: … matches none of …/design.md`（exit 1）。此前从未走到判据/锚/变异三层判定，故 C11.5 记的「判据源缺失」是读取失败，不是判据不满足。

**回填**：design.md D1 增 `### 定义完成判据（P0–P8）`（判据源声明一句＋9 条），条目文字逐字符取自 `history/7b0cde16-before-replan/design.md:15-23`（与 `HEAD:design.md` 同字节）。只回填判据条目，不带回重整前的计数与勾选状态；`P4` 文内的 `09.R2` 是判据原文的历史来路指针，当前主承载见同节「目标—执行—验收矩阵」P4 行（06.101–06.106），判据文字不改。`golden/runtime/dod_predicates.json` **零改动**：其 `criterion` 已含 9 条标题，缺的是 design 侧条目；改 registry 才是伪造判据。

**终态**：`python3 scripts/gates/check_dod_coverage.py` 不再在判据源处失败——9 条判据全部被 registry 绑定、9 个锚测试各恰 1 处且失败路径判到登记量测（零 anchor 缺口）；余下恰 9 条 `no run records mutation … as killed` 是证据面缺口（该调用未带 `--mutation-evidence`，正式入口 `scripts/lint.sh` 要求 F2 bundle），门 exit 1。门自测 `python3 scripts/gates/test_check_dod_coverage.py` **exit 0，54/54 cases passed**（派令允许的验收面）。实跑 exit 0 待 F2 产出 `$TAGENT_RUNTIME_EVIDENCE_DIR/mutations` 后由该入口核销。

**R7 删除清单**（同批，缺一即门红）：
- `memory/ledger/segment_store.go:240-251` 的 `IndexEntry` 类型连同 2 行 doc 与 5 个字段注释整块删除。唯一候选消费者 `CommittedIndexEntry` 已在 12.106 阻断④删除（D12.8），故该类型零使用。
- `golden/runtime/architecture.json` 的 `concept_bindings.bindings["IndexEntry"]` 4 行删除（`bindings` 57→56）。
- 残差 grep（`.go`/`.json`，排除历史记录）：`\bIndexEntry\b` 仅剩 `task-graph.json` 的一句历史记录（「IndexEntry 归 13.105」）；`segmentIndexEntry`／`eachIndexEntryNewestFirst`／`encodeIndexEntry` 是同前缀的别的标识符，不在删除面。

**门（本单当场复跑，原始 exit）**

| 命令 | exit | 关键读数 |
|---|---:|---|
| `python3 scripts/gates/check_concept_budget.py` | **0** | `bindings 56／new 56／removed 34／relocations 5／interfaces 79／exported_interfaces 79`；`concept ratchet held: removed>=5, new<=57, interfaces<=85`（只降未抬） |
| `python3 scripts/gates/check_dead_producers.py` | **0** | `every one of 3509 production function(s) in 32 package(s) has a consumer` |
| `bash scripts/gates/gen_godoc.sh --check` | **0** | `docs/api matches the source (82 packages)` |
| `python3 scripts/gates/check_runtime_architecture.py` | **0** | `RESULT: ok`（golden 改动后复跑） |
| `go build -buildvcs=false ./...` | **0** | 无输出 |
| `go test ./memory/ledger -count=1` | **0** | `ok … 88.661s` |
| `openspec validate runtime-recovery-ideal --strict` | **0** | `Change 'runtime-recovery-ideal' is valid` |
| `python3 scripts/gates/check_doc_links.py` | **0** | `42 document(s), 130 link(s), 56 anchor(s), 0 error(s)` |
| `bash scripts/gates/check-openspec.sh` | **0** | `OK: all main specs pass validate --strict`；2 ✗ 为在飞 change（门控外语义同前） |
| `python3 scripts/gates/check_dod_coverage.py` | **1** | 判据源已读到；9 条判据/9 锚零缺口，剩 9 条 `no run records mutation … as killed`（证据面，见上） |
| `python3 scripts/gates/test_check_dod_coverage.py` | **0** | `54/54 cases passed` |

**docs/api 刷新范围**：重生成前该页已与源漂移（基线 `gen_godoc.sh --check` exit 1，唯 `memory/ledger` 页不一致）。重生成写 83 页，相对重生成前逐字节只变 1 页 `docs/api/github_com__SpellingDragon__tagent__memory__ledger.md`，共三处：①`type IndexEntry` 与 doc 删除（R7）；②`func (l TierFacts) CommitOrder()` 退役面陈旧条目删除＋`TierFacts` 包 doc 重排；③`EachIndexNewestFirstFrom` doc 重排。`EachCommittedKeyNewestFirst` 在该页重生成前后均 0 命中（13.105 已刷新）；`CommitOrder` 接口类型仍在该页（`atomic_store.go` 的活声明），只删 `TierFacts` 的同名方法。其余 6 页与 HEAD 的差异是 13.105 既有重生成，本次逐字节未变。

**偏离与自决**：
1. 节号 C11.7 已被「偏离与自决」占用，按 C11.9 先例顺延为 C11.10，标题保留派令原名。
2. 判据条目按原文回填，不替换判据措辞；`P2/P3/P4` 的旧来路指针（如 `09.R2`）原样保留，当前承载由同节矩阵给出。
3. 未收紧 `concept_ratchet.new_max`（57→56）：实测只降不抬已满足，收紧属 reviewed tightening，不在本单。
4. 未跑 9 条 DOD 变异去自造 `--mutation-evidence`：证据必须由同一冻结树的 F2 bundle 产生，本单改 `.go` 字节后即失效；自填判决会被门具名拒绝。

**写面与越界**：写面＝`openspec/changes/runtime-recovery-ideal/design.md`（D1 判据节）、`memory/ledger/segment_store.go`（仅删类型）、`golden/runtime/architecture.json`（仅删 binding）、`docs/api/…memory__ledger.md`（生成物）、本文件、本域 `tasks.md`（13.105 记录行）。零改动：`golden/runtime/dod_predicates.json`、其余 `golden/**`、主 spec/delta、`docs/OPERATION.md`、`docs/wiki/**`、`scripts/**`、`.github/**`、`examples/**`、`CARRYOVER.md`。未 git 写。

### C11.13 F2 deterministic 漂移裁定（2026-11-22，执行者裁定；停报）

F2 步 3（C11.3 第 3 行）在本候选 `7b0cde16` 上 **exit 3**：`required=14 attested=14 drift=3 unattested=0`，两次独立进程逐键相同，共 6 条观测漂移（S02 一条、S13 两条、S14 三条）。逐观测溯源后 **0 条满足 (a)**（已签设计预期的值变化），故 **golden 一行未改**：漂移由两处**已签语义**的落地引起，而验收契约（golden 值＋场景用例）未随签。

| # | 观测（golden → 观测） | 成因 hunk（引入提交） | 裁定 | golden 变更行 |
|---|---|---|---|---|
| 1 | S02 `shared_session_history` true→false | `assembly/request.go:1361 presentedProjectionContent`（`ec038779`）：呈现面为带已提交事实键的条目前置 `[evt_<KEY>|<type>] ` | 非 (a)：主规格 `event-key-visibility:11,17-20` 签的正是本条；属性仍成立（第二次请求实际首条 `[evt_…|external_input] turn-a`），`scenario_s02_test.go:69-77` 用精确相等读法 ⇒ 需一行测试面授权，golden 记 false 即假命题 | 无 |
| 2–3 | S13 `folded_summary_leads_projection` true→false、`part_count_closed` 5→4 | `memory/replay/age_segments.go:239-247`（`bandOfSegment`）／`:497-514`（中带逐段折）／`:516-523`（仅老带非空才铸综述），`59feb537` | 非 (a)：主规格 `task-skeleton-compression:125`（本变更 10.5.7.2 改写并具正/负锚）签的是折叠模型，未签本场景期望值；改 false／4＝追认验收面缩小（Skill 红线：原验收面冻结） | 无 |
| 4–6 | S14 `segment_fold` 1→3、`history_summary` 1→0、`history_summary_leads` true→false | 同上（4 回合、`KeepRecent=1` ⇒ 3 个中带段各 1 产物，无段携带产物 ⇒ 无老带） | 非 (a)：树内同签测试 `tests/integration/history/age_band_request_test.go:133-143` 断言「首折 3 产物、`MidFolded=0`」且 exit 0，与 golden 的 1 段＋1 综述相反 | 无 |

**因果定位**（`/tmp` worktree A/B ＋ `git bisect run`，主树零写）：被测对象是 `7b0cde16` 的**脏工作树**（`git diff HEAD --stat` 生产/测试/合同面 68 文件、openspec 面 177 文件，mtime ≤ 11-22 02:49，早于本单 03:50）；A/B 的 HEAD 侧取自 clean `7b0cde16`，**clean 与脏树三场景观测逐键相同**。`3b727199`（59feb537 的父）＝golden 值（`true`／`5·true`／`1·1·true`）；`59feb537` 起 S13/S14 转 `4·false`／`3·0·false`；S02 的 first-bad 由二分定为 `ec038779`；`0f84b2d1` 与 HEAD 三场景逐键相同 ⇒ **7b0cde16 的改动与夹具改写（rebuild→Append）非成因**。派令候选 ①–④ 具名排除：① 安静点键族 `tagent/checkpoint/v1/quiet:` 是存储派生键、**不是事实、不入链**（`assembly/checkpoint.go:891-896`），且 `59feb537` 零命中；② 交付事实 `settle-%x`（`runtime/task/delivery_fact.go:30-32`）只在真实投递路径落链，S13/S14 投影由 16 条固定事实直接 `Append`（探针 `entries=16`），`segment_fold=3` 恰等于「中带 3 段各 1 产物」；③ H02 由 HEAD == `0f84b2d1` 排除；④ **两态同观测**——`sessionFiled`/`sessionCuts`/`sessionSlices` 在被测工作树已删（`assembly/{run,checkpoint}.go` 零命中）、clean `7b0cde16` 仍在（`assembly/run.go:344-350`、`assembly/checkpoint.go:448,1015`），两态三场景观测逐键相同，故该删除无行为影响。

**新查出的缺陷**：golden S14 的 `anchor_tests` 点名 `memory/replay:TestAgeFoldFoldsTheNewBandIntoOneProduct`，该测试在树内 `*_test.go` 零命中（随带晋升退役），anchor 清单过期；另两条 anchor 在树且绿。

**门与终局**：`run_runtime_scenarios.py --scope deterministic` **exit 3 ×2**（产物 `~/Documents/codes/tagent-f2-evidence/2026-11-22-f2/f2-drift-replay/{run-20261121T195838Z-deterministic,run-2}`）；`check_runtime_evidence.py --check-contract` **exit 0**（15 场景／91 观测值／`RESULT: ok`）。**F2 步 3 仍红，F2 不得勾**，不得以改 golden 值换绿。

**待裁（场景面授权）**：A（推荐）——S02 读法改前缀容忍＋S13/S14 夹具按带晋升重基（折叠至 settled 或两折后再观测），golden 按新模型记真值并同步 title/anchor；B——显式重签为一折态（`false`／4／3／0／`false`）并具名声明「综述领投」的验收由 `tests/integration/history/age_band_request_test.go` 承接，交 F3 复核是否可接受。

**证据**：`~/Documents/codes/tagent-f2-evidence/2026-11-22-f2/drift-adjudication.md`（首次 exit 3 原文、两次复跑、A/B 表、逐观测证据、候选项排除、选项与越界自查）。

#### C11.13.1 重录执行（编排者裁定选项 A 落地，2026-11-22）

裁定：golden deterministic 重录为本候选（`7b0cde16` 工作树）在**带晋升折叠模型**下的真值，S02 读法按**已签呈现前缀**语义更新。行为判据不弱化：S02 仍要求原内容整串相等（仅容忍一张已签票据）；S13/S14 的驱动与端点一个未改，只把 golden 记成真实观测值；`check_runtime_evidence` 的 recorded==observed 咬合面不变（`compare_observations` 逐键 `typed_equal`，无容差）。综述／部件验收面不随本场景出局，由树内具名锚承接（见下「承接声明」）。

**golden 逐行变更（`golden/runtime/scenarios.json`，8 行对，全落在 S13／S14 块内；S01–S12、S15 零改动）**

| # | `scenarios[]` 行 | 旧 | 新 | 签发依据 |
|---|---|---|---|---|
| 1 | S13 `title` | 「…→折叠摘要→投影→…每个消息恰属一部件且**五部件齐全**」 | 「…→投影→…每个消息恰属一部件；**首折态记四部件**，综述部件与领投影由晋升折后的 history 锚承接」 | `task-skeleton-compression:125`（带归属由带晋升决定）＋`tests/integration/history/age_band_request_test.go:112-118,133-143` |
| 2 | S13 `observations.folded_summary_leads_projection` | `true` | `false` | 同上：首折只铸中带 `segment_fold`，无综述可领投 |
| 3 | S13 `observations.part_count_closed` | `5` | `4` | 同上：无综述部件时闭集四部件；「每个消息恰属一部件」仍由 `scenarioPartOf`／`scenarioParts` 逐条 fail-fast 断言 |
| 4 | S14 `title` | 「段龄折叠产出（新→中一段进程折、中+老→老一份历史摘要）且摘要领投影」 | 「首折把中带三段的进程细节各折一段产物（不铸综述）；晋升折把该材料收成老带一份历史摘要并领投影」 | `task-skeleton-compression:125`＋`memory/replay/age_segments.go:239-247,497-523` |
| 5 | S14 `anchor_tests[0]` | `memory/replay:TestAgeFoldFoldsTheNewBandIntoOneProduct` | `memory/replay:TestAgeFoldFoldsTheMiddleBandIntoOneProduct` | 旧名树内零命中（随带晋升退役，C11.13「新查出的缺陷」）；新名在 `memory/replay/age_segments_test.go:310`，同包三例 `-run '^(TestAgeFoldFoldsTheMiddleBandIntoOneProduct\|TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary\|TestAgeFoldWaitsForTheVolumeThreshold)$' -count=1` **exit 0**（三 `--- PASS`） |
| 6 | S14 `observations.age_fold_products.segment_fold` | `1` | `3` | `task-skeleton-compression:125`：4 回合、`KeepRecent=1` ⇒ 中带 3 段各 1 产物 |
| 7 | S14 `observations.age_fold_products.history_summary` | `1` | `0` | 同上：无段携带产物 ⇒ 无老带，`age_segments.go:516`「仅老带非空才铸综述」未成立 |
| 8 | S14 `observations.history_summary_leads` | `true` | `false` | 同上：无综述可领投 |

**S02 读法对照（`tests/integration/acceptance/scenario_s02_test.go`；旧断言→新断言，一行）**

| 位置 | 旧断言 | 新断言 |
|---|---|---|
| `:69-76`（判读循环） | `message.Content == "turn-a"`（整串精确相等；票据注入后读到 `false`） | `scenarioPresentedContent(message.Content) == "turn-a"`（剥离**一张**已签票据 `[evt_<KEY>\|<type>] ` 后整串相等；无票据者原样比较；键空／类型空／无分隔空格／半截票据一律不截断） |
| `:168-193`（新增） | — | `scenarioPresentedContent` 帮助函数，`契约: docs/wiki/agent/event-flow.md#projection-lifecycle` |

golden S02 一行未改（`shared_session_history` 仍记 `true`）：属性本身成立，读数由票据前置引起，改测试读法即归真，不把「历史未共享」的假命题写进合同。

**承接声明（综述／部件验收面不在本场景出局）**

- 「晋升折铸一条老带综述并领请求」：`tests/integration/history:TestAgeBandsReachTheRequest`（`:145-163` 折至 settled 且 `Products[0].Type==history_summary`；`:165-171` `entries[0]` 为综述、新回合四条目原样；`:173-184` 正式请求首个非 recovery 消息以 `history summary: ` 开头）；实测 `go test ./tests/integration/history -run '^(TestAgeBandsReachTheRequest|TestAgeFoldUnderThresholdLeavesTheRequestAsItStands)$' -count=1` **exit 0**（两 `--- PASS`）。
- 「首折每段一条 `segment_fold`、`MidFolded=0`、无综述」：同文件 `:133-143`＋`memory/replay/age_segments_test.go:310`（`TestAgeFoldFoldsTheMiddleBandIntoOneProduct`）、`:402`（`TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary`），均已实跑 exit 0。
- 「闭集：每个消息恰属一部件、无未归类消息」：仍由 S13 驱动自身 `scenarioParts` 断言（一折态四部件）。
- 残留（如实记，不追认）：`part_count_closed=5`（五部件同时在场）不再由任何单一驱动断言；要恢复此面需在 history／acceptance 侧新增 settled 态闭集用例，本单写面不含测试新增，留 F3 裁定。

**复跑终局（同一树，原始 exit，不经管道）**

| 命令（原始 argv） | exit | 读数 | 产物 |
|---|---:|---|---|
| `env GOFLAGS=-buildvcs=false python3 scripts/scenarios/run_runtime_scenarios.py --scope deterministic --out <EVD>/run-3` | **0** | `verdict: match`；S01–S15 逐场景 equal；`required=14 attested=14 drift=0 unattested=0`；`S02 5/S13 4/S14 3 observation(s) equal` | `f2-rerecord/run-3/{evidence.json,test-log.jsonl}`（`run-20261121T200608Z-deterministic`） |
| 同上，`--out <EVD>/run-4` | **0** | 逐字同上（重复性） | `f2-rerecord/run-4/`（`run-20261121T200614Z-deterministic`） |
| `python3 scripts/gates/check_runtime_evidence.py --check-contract` | **0** | `scenario_count 15／observation_values 91／RESULT: ok` | — |
| `python3 scripts/gates/check_runtime_evidence.py --evidence …run-1/evidence.json --test-log …run-1/test-log.jsonl --scope deterministic` | **0** | `scenarios_verified 14／scenarios_required 14／RESULT: ok`（recorded==observed 咬合） | — |

两次终局 `source_fingerprint.content_hash` 逐字相同 `sha256:9863a0eb…`，且与当前树 `run_runtime_scenarios.source_fingerprint('.')` 复算一致（v1 指纹覆盖 `tests/integration/acceptance/scenario*.go` 全部字节）。`run-1/run-2`（`sha256:d3fa99de…`）是**注释行修订前**的产物，按「绑定内容变更即标 stale」留档并重跑，不作终局引用。

**负锚（主树零写，`-overlay` 挂 `/tmp`）**：`scenarioPresentedContent` 的严格性由具名用例咬合——空键、空类型、缺分隔空格、半截票据、`answer:turn-a` 五例均不截断，`[evt_…|external_input] turn-ab` 剥离后为 `turn-ab` 不匹配 `turn-a`。控制（真实现＋负锚）**exit 0**；变异（把帮助函数换成「剥到首个空格」的宽松读法）**exit 1**，首个失败点 `scenarioPresentedContent("[evt_|external_input] turn-a") = "turn-a"`。

**写面与越界**：写面＝`golden/runtime/scenarios.json`（S13/S14 两块的 8 行对）、`tests/integration/acceptance/scenario_s02_test.go`（判读行＋帮助函数＋`strings` import）、本文件、`~/Documents/codes/tagent-f2-evidence/2026-11-22-f2/drift-adjudication.md`。零改动：生产 Go（`assembly/`、`memory/replay/`、`runtime/`、`api/`）、其余测试文件（**含 `scenario_s13_test.go`／`scenario_s14_test.go`**）、其余 `golden/**`、主 spec／delta、`scripts/**`、`docs/**`、`.github/**`；未 commit／push／archive；F2 不勾。**实证**：`find . -type f -newermt "2026-11-22 03:55" -not -path "./.git/*"` 恰 3 条（即上列仓内三文件）；被测树在会话开始时已带在飞改动（C11.13 记 68 生产/测试/合同文件），故写面以 mtime 计，不以 `git diff HEAD` 计。

**新查出的残项（不在本单写面）**：①`scenario_s14_test.go:14-17` 与 `scenario_s13_test.go:16-20` 的用例 doc 注释仍写旧模型（「一个 segment fold ＋ 一个 history summary」「folded history summary leads」），与新记值相反，属注释真话债，需测试面写权；②`anchor_tests` 不被 `check_runtime_evidence` 校验（C11.13 已记），本次换名靠 `grep` 零命中／在树且绿实证；③见上「承接声明」残留项 `part_count_closed=5`。

### C11.14 F2 smoke 五失败裁定（2026-11-22，执行者裁定；停报）

F2 的 `wechat-bot smoke (tag)` 步在本候选上红五枚（原文 `bdertsggg.output` 465–522）。**A/B 先于裁定**：`git archive HEAD` 取 clean `7b0cde16` 到 `/tmp/smoke-head`，同六名同参数复跑——**1–4 同样红、5 绿**；主树改后全量拷贝 `/tmp/smoke-cur`（无 `.wechat-config/`）六名复跑——仅 `TestLocalSmoke` 红（matrix 8 绿、matrix 12 红）。故 1–4 非本轮在飞改动引入，而是"已签语义落地、验收面未随签"，与 C11.13 同族；5 是环境面。

| # | 观测 | 签发依据 | 裁定 | 处置 |
|---|---|---|---|---|
| 1 | `Wave2Data` receipt＝`[evt_settle-<hex>\|task_settled] tagent: task <id> settled`，want `ok` | `assembly/request.go:1361 presentedProjectionContent`（票据前置，`ec038779`，C11.13 行1 已裁）＋`12/design.md:899,1130`「D9.10.5-1② 每个被结算调用的投递事实进模型请求（role=tool，票种 `task_settled`，正文＝结算行）」＋`runtime/agent/request_assembly.go` 角色表 `TypeTaskSettled→RoleTool` | 过时读法（同 callID 下多行：准入 `action_command`＋结算行） | 读法改"剥一张已签票据后正文＝`ok`"取准入回执，另补钉结算行；原断言与 `[call_subagent_denied]` 阴性守卫保留 |
| 2 | `Wave2Errors/refused_replacement…` `reload status = 500, want 200` | `openspec/specs/config-hot-reload/spec.md:159`「回执返回失败与具名原因，MUST NOT 返回成功」＋`tests/integration/host/event_integrity_test.go:3321-3325`（want 500＋`install-failed`，主模块绿）＋smoke 自身 `smokeIntegrityReplacementRefused`（判 `result.err`） | 过时断言（`want 200` 已被主规格明文否定） | `postReload` 原样交回状态与正文；改判 500＋`install-failed`＋正文具名 `local/smoke-entry → smoke-errors-missing/smoke-entry-b`；未发布／未丢变更／`ErrLeaseRebuildFailed` 具名／拒收日志／held 版本续服逐条保留 |
| 3 | `Wave2Recall` `commit through a view that holds no write rights` | `memory/ledger/config.go:65-71`（`WriteRights` 声明面）、`journal_kv.go:199`（拒绝名）、`assembly/run.go:613`（生产写面）、`journal_kv_test.go:205-222`／`segment_store_test.go:1698-1732`（树内写侧一律声明）、`config.go:310`（Close 让根） | 测试用旧写面（非只读视面语义回归） | 种子开面声明 `WriteRights: true` 并在返回前 `Close`（缺 Close 时同根第二写者按 `ErrRootWriteLocked` 被拒，首跑即复现）；`smoke_test.go:3921` 的只读开面不动 |
| 4 | `TestLocalSmoke` matrix 8「folded no history summary carrying EARLY-MARKER (products=1)」 | `task-skeleton-compression:123`（带归属由带晋升）＋`:97`（综述正文服从声明预算）＋`:117`（被折原文仍可召回）＋`memory/replay/age_segments.go:606-610,637-641,239-247`；同签锚 `tests/integration/history/age_band_request_test.go:113-160` | 过时断言（综述存在，正文按预算只留最新读法、更早材料以既有产物的有界引用概括） | 改判"最早一轮的 `external_input` 已移出投影（压缩真发生）＋模型请求渲染了折叠综述"；"折后仍可召回""晋升折铸综述领请求"由 `TestAgeBandsReachTheRequest` 与规格 `:117` 承接 |
| 5 | `SymptomNoSendTool`／`SymptomBootDeployed` `holds the retired shard layout, open a new directory: .wechat-config/data/kv/tagent%2Fconfig%2Fv1%2Ffact` | `memory/ledger/config.go:133-148 rejectLegacyShardLayout`＋`examples/wechat-bot/tagent.yaml:58-63`（该根仍旧分片布局时新构建拒开，迁到新根后再指过去） | 环境阻断（非候选引入；clean HEAD 副本无该目录即绿） | **不动原目录**（未读内容、未改、未删）；F2 该面记**未验**；补救需用户裁：`scripts/migrate-kv` 迁新根并改 `memory.path`，或该锚改 Boot 一份只改数据根的同文文档。反证：`/tmp/smoke-cur` 上两名 exit 0（1.116s） |

**目录取证（只列名与 mtime）**：`data/{kv.json(161MB,08-26),kv.wal.jsonl,relations.journal}`、`data/{approvals,meditation,kv}/`（11-06）、`data/{relations,trajectories}/`（10-22/10-28）、`.wechat-config/{seen.json,cursor.txt,data-v2/}`（10-28/11-11）；`data/kv/` 一级 9 项含 `tagent%2Fconfig%2Fv1%2Ffact` 等按名分片目录。时间戳无一落入测试运行窗（11-22 04:17），且形态为真实部署根（161 MB 历史 kv、审批／冥想／轨迹账）⇒ 判"非套件自生 fixture"，按用户真实数据处理。

**复跑终局（主树，原始 exit）**：`Wave2Recall` **exit 0**（0.995s）、`Wave2Errors` **exit 0**（7.382s）、`Wave2Data` **exit 0**（0.896s）；`TestLocalSmoke` **exit 1**——matrix 1/2/3/4/5/6/8/9/10/11a/11b 全绿（matrix 8 三会话 `entries=5/6/5 synthetic=1`），红下移到 **matrix 12**。`/tmp/smoke-cur` 六名：仅 `TestLocalSmoke` 红，Data／Errors／Recall／Symptom×2 绿。`go vet -tags smoke .` 唯一 finding＝`smoke_test.go:2690 unreachable code`（既有，`smokeIntegrityReplacementRefused` 的 `return` 后旧读法块，随新增帮助函数由 2658 下移）。

**新查出的第六枚（不在本单写面，如实记，不处置）**：matrix 8 转绿后 `TestLocalSmoke` 的红下移到 matrix 12——`attempts.jsonl = 950 lines against 664 model calls`。**归属＝既有**：clean HEAD 副本只放宽 matrix 8 两条判据（`-overlay`，`/tmp/probe-head`）后同红（`950 lines against 657 model calls`）。**成因实测**（`/tmp/f2-probe3`）：`records=950 = attempt 475 + request 475`，calls=665——`attempts.jsonl` 自 `59feb537` 起同时装请求证据行（`features/rl/sink.go:297 RecordRequestEvidence`／`line.go:27`），而该检查仍按"一次调用一行"比对；attempt 行数（475）与调用数（665）本身的差 190 与压缩消费的模型调用同量级（压缩回合非消费决策，不落 attempt）。**未改理由**：是否把"每次模型调用都进轨迹"收窄为"每次决策都进轨迹"属验收面改动，须由 rl／12 域裁定并配负锚；按"验收面被追认缩水＝该叶重开"，本单不自行改。**F2 smoke 面因此仍红**（与环境阻断并列），F2 不勾。

**写面与越界**：写面＝`examples/wechat-bot/{smoke_data_test.go,smoke_errors_test.go,smoke_harness_test.go,smoke_recall_test.go,smoke_test.go}`、本文件、`~/Documents/codes/tagent-f2-evidence/2026-11-22-f2/smoke-adjudication.md`。零改动：生产 Go（`assembly/`、`runtime/`、`memory/`、`api/`、`features/`、`tool/`）、`golden/**`、主 spec／delta、`scripts/**`、`docs/**`、`.github/**`、`examples/wechat-bot/.wechat-config/**`、`CARRYOVER.md`、`ORCHESTRATION.md`；未 commit／push／archive；F2 不勾。scratch 全在仓外：`/tmp/{smoke-head,smoke-cur,f2-probe,f2-probe2,f2-probe3,probe-head}`。

### C11.15 F2 smoke 第六枚（matrix 12 轨迹判据）裁定与处置（2026-11-22，rl 语义裁定；已处置）

C11.14 的「新查出的第六枚」（`attempts.jsonl = 950 lines against 664 model calls`）当时按「验收面改动须由 rl／12 域裁定」停报。本单接令裁定并处置。**结论：轨迹的声明语义是"每一次消费决策可重建"，不是"每一次模型调用可重建"**；检查判据按语义改正，**不放宽为子集包含**。生产零改。

**语义依据（rl 域自读，非推测）**：`features/rl/produce.go:23`——记录面三生产点「one episode boundary, one model decision, one observable model attempt each write their record through this face」；`features/rl/record.go:287`——「A retry below the decision boundary is not a second attempt」；`runtime/agent/loop.go:1709`——`attemptID := event.AttemptID("attempt-" + decisionID)`，一次决策铸一次 attempt；请求证据行的同族自述：`features/rl/line.go:26-28`（「the request evidence of one decision, landed before the request starts」）、`features/rl/sink.go:288-298`（`RecordRequestEvidence` 收一条决策的请求证据）、`features/rl/capture.go:572-574`（「the durable request evidence of one model decision」）。故轨迹的记账单元＝**决策**：一决策＝一请求证据行 ＋ 一 attempt 行。

**压缩回合按设计排除**：老带语义摘要是折叠拿会话自己的模型求 gist（`assembly/summary_gist.go:69-104`，`bandSummaries.SummaryGist` 直接 `serving.GenerateContent`，不经 `runtime/agent` 的决策/attempt 面），其产物按 `assembly/compress_consumer.go:335`（`event.CompressProductFor`）落成折叠产物事实——可重建面是那条事实，不是轨迹行。台架自身早有同一判据：`examples/wechat-bot/smoke_harness_test.go` 的 `smokeBandSummaryMaterial`（注释：「摘要请求不是一轮决策，不消耗决策面也不认领轮次标记」），且响应脚本就靠它回答摘要请求；本单沿用该判据，未新造豁免。

**判据对照（`examples/wechat-bot/smoke_test.go` `runSmokeTrajectoryCheck`）**：

| 面 | 旧判据 | 新判据 |
|---|---|---|
| attempt | 文件**总行数** == 模型调用数（把 request 行与压缩调用都算进分母） | **attempt 行数** == 决策数（`provider.count() − 经 smokeBandSummaryMaterial 认出的摘要请求`），入口与委派子 agent 一同计入 |
| 请求证据 | matrix 12 原本**无任何证据行判据**（同族 `smokeIntegrityCapturePositive` 只要求 ≥1 行） | 证据行数 == 决策数，且按 `decision_id` 与 attempt 行**双向逐一对上**：重复证据、孤证、缺证各自具名 |
| 文件种类 | 无（总行数掩盖种类构成） | `smokeIntegrityRecordKinds` 拒绝 attempt／request 之外的 kind；`len(lines) == trajectory.Records` |
| seal | `seal.Records` == 最后一次装配的模型调用数 | `seal.Records` == 2 × 最后一次装配的决策数（一决策一 attempt ＋ 一证据） |

**控制绿（主树）**：`env GOFLAGS=-buildvcs=false go test -C examples/wechat-bot -tags smoke . -run '^TestLocalSmoke$' -count=1 -timeout 75m`（原始 argv，不经管道）→ **exit 0**，`ok wechat-bot 31.234s`；同命令加 `-v` 取到的读数：`[matrix 12] trajectory accounted: lines=950 attempts=475 request-evidence=475 model-calls=665 band-summaries=190 decisions=475 entry-calls=430 sub-agent-calls=45 seal-records=322 last-boot-decisions=161 files=1 sealed=true`。**190 恰为 665 与 475 之差**，旧口径的全部差额由压缩摘要调用解释，无一条决策缺账；列数面 475 ＝ entry 430 ＋ worker 45。

**负锚（`-overlay` 挂 `/tmp/rr-m12/`，主树零写；每枚一次全量 `TestLocalSmoke`）**：

| 变异 | 期望 | 实测（原始 exit） |
|---|---|---|
| M1 删一条 attempt 行（读回总行数同减，伪装"从未写入"） | 红 @ attempt 行数 | **exit 1**：`attempt lines = 474 against 475 decisions (662 model calls, 187 …)` |
| M2 删一条 request evidence 行 | 红 @ 证据行数 | **exit 1**：`request evidence lines = 474 against 475 decisions (666 model calls, 191 band summaries)` |
| M3 一条证据行的 `decision_id` 改成无 attempt 的决策（行数不变） | 红 @ 逐一对上 | **exit 1**：`request evidence names decision decision-mutated-away, which carries no attempt line`（行数面全绿，只有 join 咬住） |
| M4 等值改单侧 `attempts > decisions` ＋ 删一条 attempt 行 | 仍红 | **exit 1**：同一 join 断言——该方向由 join 独立兜住，与计数方向无关 |
| M5 决策分母改回 `calls`（不减摘要） | 红 @ attempt 行数 | **exit 1**：`attempt lines = 475 against 663 decisions (663 model calls, 188 of them band summaries …)` |
| M6 **整条决策缺失**（attempt 行与证据行同删，账面自洽） | 红 @ attempt 行数 | **exit 1**：`attempt lines = 474 against 475 decisions`——join 全绿，只有等值能咬住 |
| M7＝M6 ＋ 两侧计数都改单侧（`>`） | 绿（证明子集放宽会吞缺陷） | **exit 0**：整条缺失决策被静默接受，等值与双向 join 因此不是装饰 |

**跨跑读数（同一变异批的旁证）**：八次运行（控制 ＋ M1–M7）里 `model-calls` 662–667、`band-summaries` 187–192 各自浮动，而 `decisions` 恒为 **475**、`seal-records` 恒为 **322**（＝2×161）。浮动全部落在摘要面上，正是摘要分类正确的独立佐证：折叠执行次数随提交时序浮动，决策面不浮动。

**复跑（其余三名，主树，原始 exit）**：`-run '^(TestLocalSmokeWave2Data|TestLocalSmokeWave2Errors|TestLocalSmokeWave2Recall)$'` → **exit 0**（`ok wechat-bot 8.641s`）。`go vet -tags smoke .` 唯一 finding＝`smoke_test.go:2753 unreachable code`，即 C11.14 记的既有项（`smokeIntegrityReplacementRefused` 的 `return` 后旧读法块），位置由改动前的 2690 下移 63 行（本单在该函数之前新增 63 行），非新引入。

**写面与越界**：写面＝`examples/wechat-bot/smoke_test.go`（`runSmokeTrajectoryCheck` ＋ 新帮助函数 `smokeBandSummaryCalls`）、本文件、`~/Documents/codes/tagent-f2-evidence/2026-11-22-f2/smoke-adjudication.md`。零改动：生产 Go（`features/rl/`、`assembly/`、`runtime/`、`memory/`、`api/`、`tool/`）、其余 smoke 测试文件（含 `smoke_harness_test.go`／`smoke_data_test.go`／`smoke_errors_test.go`／`smoke_recall_test.go`）、`golden/**`、主 spec／delta、`scripts/**`、`docs/**`、`.github/**`、`examples/wechat-bot/.wechat-config/**`、`CARRYOVER.md`、`ORCHESTRATION.md`；未 commit／push／archive；**F2 仍未勾**（smoke 面第 5 枚的数据目录环境阻断照旧未验，且本单只裁 matrix 12 一枚）。scratch 全在仓外：`/tmp/rr-m12/`（7 份变异副本 ＋ 7 份 overlay ＋ 各跑日志）。

### C11.16 F2 六长回归：无屏障标记（2026-11-22，编排者裁定，归 06/12 域）

F2 的 event-long 分片 `seed-1×window-256` 在候选树确定性红两次（结算跟进被截＋收尾预算超时），无标记探针与之对照转绿；根因是 `assembly/checkpoint.go` 的 `stateQuietPoint` 在 `r.landing` 锁内每 cut 多付一次 `Commit` 屏障（`memory/ledger/atomic_store.go:45`「A commit takes exactly one barrier」）。裁定＝标记改**无屏障追加**（顺序先于快照、耐久搭后续提交的 fsync、撕裂/缺席为既有读法），存储侧新增单方法窄口 `AppendOrdered`（消费者侧声明，**不新增导出命名类型**，故 D1 概念预算零新绑定、两个棘轮上限不动）。**本行只作 C 侧承接**：签发与不变量论证见 06 域 D6 的 F2 补签行与 12 域 D8.9.6，实施与逐项复跑凭据见 12 域 D9.11；F2 分片结果与勾选仍归编排者，C 侧不改任何判据、不追认缩小验收面。

### C11.17 变异锚随修复迁移与 F2 收尾门重取（2026-11-22，13.103 面）

**现象（原文 `b5lzvai6u.output` 135–331）**：`check_event_integrity_mutations.py --all` exit 1，六条 stale 报文归三条变异——`MUT-EVID-03-busy-notice-drop` 锚在 `assembly/settle_turn.go` 0 命中；`MUT-CAND-01.8-storage-success`（四补丁，×4 报）与 `MUT-CAND-01.2-handover-confirm` 锚在 `assembly/facts.go` 0 命中。成因＝12.103/12.104 改写了它们锚定的三处（H02b 五值结局分派、H05 保 claim、门槛收口为 `terminalDelivery`），锚文本随实现漂移；**fault 本身未被设计消除**，故按"同 fault 换锚"迁移：不删条目、不降级为 `not_evidence_backed`、不动 `expect_assertion` 与选择器。

**逐条旧锚 → 新锚**

| 变异 | 旧锚（位置／文本） | 新锚（位置／文本） | 位置理由 | 判决 |
|---|---|---|---|---|
| MUT-EVID-03-busy-notice-drop（patch 1；patch 2–4 逐字节未变） | `ObserveCommit` 尾：`if !terminalSettleKinds[noticeKindOf(fact)] {…}` ＋ `d.Wake()` ＋ `return nil` | 同函数同语句：`if !terminalDelivery(fact) {…}` ＋ 同一 `d.Wake()` ＋ `return nil` | 门槛由裸表读收口为单一谓词（`settle_turn.go:699` 折了 family＋task＋表），被"忙时"吞掉的仍是同一个 `d.Wake()`；patch 2–4（busy 字段、drain 置忙、一轮即出）原锚仍在 | killed：`event_integrity_test.go:1283: no request carried the notice material … within 30s; model requests = 7` |
| MUT-CAND-01.8-storage-success | 四锚：`default: return err`（`persistFact`）＋ presence read（`heldErr`）＋ conflict read（`getErr`）＋ `if !held { return err }` | 两锚：`default: return outcome, err` → `return ledger.FactCommitted, nil`；conflict read `return ledger.FactConflict, getErr` → `FactCommitted, nil` | 写步回传五值后 `default` 是 `FactUncertain/FactRefused` 的公共出口，答成 `FactCommitted` 即"拒写被当已提交"（分派入口只在 `FactCommitted` 上登记＋fold）；presence read 已移出写步进 `chainRecord`（写前读，变异下其拒绝照常上抛，故不入补丁组）；旧 `!held` 出口改带具名结局，不再属"存储拒绝" | killed：`event_integrity_test.go:4703: the turn beside a failing storage tier returned no failure` |
| MUT-CAND-01.2-handover-confirm | `sink` 内：`if refusal := Backflow(…); refusal != nil { return r.retainNoticeOwed(…) }` ＋ `return r.store.Consume(…)` | 同函数：`if refusal := Backflow(…); refusal != nil { assemblyDebug(…); return nil }` ＋ 同一 `Consume` 尾；变异体＝先 `Consume` 后 `Backflow` | H05 改成"已交出的结果把 claim 留在本进程"，释放 claim 的形状消失；被交换顺序的两条语句未变 | killed：`event_integrity_test.go:332: … is not recoverable after the restart …` |

**"fault 已消除"逐条判定**：三条均**不成立**。01.8 尤其不是"存储成功前发布"型：已签实现是五值结局分派（`commitFactNoCadence` 只在 `FactCommitted` 分支登记＋fold），把拒绝答成 `FactCommitted` 仍造出"未耐久的可见"，故仍可注入并被具名断言杀死——不是等价变异，不得退役。

**gofmt**：`examples/wechat-bot/smoke_test.go` 仅三行注释缩进（`// -` → `//   -`，diff 8 行、行数不变），零语义；`gofmt -l . examples/wechat-bot examples/cli-host` 为空。

**由此产生的重取（代价与依据）**：该文件字节属 wechat-bot 模块指纹，runner 证据按模块摘要绑定现树，故 gofmt 后 6 长＋10 短全部因 `examples/wechat-bot` 摘要失配标 stale。按 `preflight.sh` 同 argv 重取：6 长全 rc=0（`verdict: match`，新 wechat 摘要 `sha256:f6d8f809…`、根模块 `sha256:ce4adef…` 不变），10 短 rc=0。其中 repeat-2 首轮红（`TestEventIntegrityTaskContinuation/notice_visible_under_small_window`：close 20s 预算耗尽、`drain report … Reason: the mailbox holds this notification unconsumed`），重跑绿；随后该 parent 用例 `-count=5 -race` 5/5 绿、子测单独 0.5s 绿——判为**负载相关 flake（11 轮 1 红）**，不改产品/测试、不放宽判据，失败样本存 `superseded-2026-11-22-f2-bundle/flake-repeat-2/`。旧证据全存同目录 `pre-gofmt-evidence/`。

**整包卫生**：13 份越界/重复 `evidence.json` 移出包根（7 份 `d94b2de` 旧长跑、2 份 `f2-drift-replay`、4 份 `f2-rerecord`），包内现为 6 长＋10 短＋1 变异；变异产物目录 `mutations-fix` → `mutations`（lint 的 DoD 步读 `<evidence>/mutations`，而整包只许恰一份变异证据，两处只能共用一处）。

**三命令终局**（原始 exit，非管道码）：① 变异门 `--all --out …/mutations-fix` → **0**：`control_tree_green true`、23/23 `killed`、`pending []`、`not_evidence_backed []`、门工具摘要 `sha256:da028555…`；② 整包绑定 → **0**：`17` 份证据（`6` 长／`10` 短／`1` 变异）、`event_log_packages 9`／`event_log_tests 180`；③ `lint.sh` → **1**：`gofmt`／`modules`／`build`（三模块）／`vet`（三模块）四段已过，卡在 `comment policy (ratchet)`——7 finding 全属在飞候选自有注释债（`assembly/facts.go:142` free-standing×3、`assembly/facts_test.go:2037/2048`、`scenario_s14_test.go:18`、`smoke_test.go:849`，无一处在本单改动行上），非本单写面，且修它们要改 `.go` 字节会使刚重取的证据再脱树，故**停报待裁**；`--only fmt` 单跑走到 checker selftest 时唯一红是**外来** `test_check_runtime_symptoms.py::test_registry_matches_the_module`（C7.3 集成差异），DoD 门单独实跑 `9/9 criteria bound and proven` exit 0。

**写面与越界**：写面＝`scripts/gates/check_event_integrity_mutations.py`（三锚迁移＋注释）、`examples/wechat-bot/smoke_test.go`（gofmt）、本文件、仓外证据目录。零改动：生产 Go、其余测试、`golden/**`、主 spec／delta、`docs/**`、`ORCHESTRATION.md`；未 commit／push／archive；F2 不勾。

### C11.18 注释债清零（2026-11-22，F2 最后阻断项）

C11.17 停报的 `comment policy (ratchet)` 7 finding 中 `scenario_s14_test.go:18` 已由前单消化；本单实跑 `go run ./scripts/comment_policy . examples/wechat-bot examples/cli-host` 得 6 finding（free-standing×3＋doc-not-brief／test-doc-not-one-sentence／unindexed-path-ref 各 1），逐条清零。**不动基线**：未跑 `-update-baseline`，`scripts/comment_policy/baseline.json` 零改动。

**逐条旧 → 新**

| # | 位置 | 规则 | 旧 | 新 |
|---|---|---|---|---|
| 1 | `assembly/facts.go:142` | free-standing | `FactAlreadyPresent` 分支内 3 行内联注释（`An earlier attempt left the fact durable with no stated outcome…`） | 整组删除。同一理由已是 `commitFactNoCadence` 文档槽既有 bullet（`a fact the chain holds beside no registration of this process is the landing of a commit whose barrier named no outcome…`），属"代码/文档已自述"，不加重复行 |
| 2 | `assembly/facts_test.go:2037` | doc-not-brief | 3 条 bullet 各跨两行，续行被计为 3 段散文（计数 4 > 2） | 每条 bullet 收成单行；控制项拆为两条 bullet（`the control is… folds nothing` ＋ `a seam state so left is…`）；意图句与 `契约:` 行不变 |
| 3 | `assembly/facts_test.go:2047` | test-doc-not-one-sentence | bullet 缩进 3 格、续行 5 格，续行不以 `- ` 开头 | 全部 bullet 列 0 单行（首行意图句不动），`契约:` 行保留 |
| 4 | `examples/wechat-bot/smoke_test.go:849` | free-standing ＋ unindexed-path-ref | `runSmokeFoldChecks` 循环内 4 行中文注释，含裸 `docs/wiki/memory/memory-architecture.md` | 内联组删除；预算语义并入函数文档槽一条 bullet，路径改为索引行 `契约: docs/wiki/memory/memory-architecture.md#hard-contracts`（`<a id="hard-contracts">` 于该文档实证存在） |
| 5 | `examples/wechat-bot/smoke_test.go:1029` | free-standing | `runSmokeTrajectoryCheck` 内 2 行中文注释（`证据行按决策身份与 attempt 行逐一对上…行数相等掩不住一处重复证据加一处缺证的互换`） | 内联组删除；并入函数文档槽 bullet：`the pairing is read in both directions by decision identity: equal totals cannot hide one duplicated evidence line beside one decision with none` |

**三验证终局**（原始 exit，非管道码）：① `go run ./scripts/comment_policy . examples/wechat-bot examples/cli-host` → **0**（`0 finding(s); 0 beyond baseline`）；② `gofmt -l . examples/wechat-bot examples/cli-host` 空输出；`go build -buildvcs=false ./...` → **0**；③ 抽跑 `go test ./assembly -run '^TestFactLandedBesideAnUncertainBarrierIsStatedByItsRetry$' -count=1` → **0**（`ok … 0.424s`）；`go test -tags smoke -run '^(TestLocalSmokeWave2Data|TestLocalSmokeWave2Errors|TestLocalSmokeWave2Recall)$' -count=1 .`（wechat-bot 模块）→ **0**（`ok wechat-bot 8.595s`）；`go test -tags smoke -run '^$' -count=1 .` → **0**（编译）；`go vet -tags smoke .` → **1**，唯一 finding 为 C11.14 已记的既有 `unreachable code`（`smoke_test.go:2751`，行号随本单净删 2 行由 2753 下移，非新引入）。

**注释归属证明**：`bash scripts/gates/check_comment_only.sh HEAD` → **1**（`56 violation(s)`），因整棵在飞候选相对 HEAD 本就有代码差异（56 文件），该基线不能隔离本单 delta。改以本单改动前的三文件重建基线（`/tmp/pre-edit`，逐处反向替换并断言恰 1 次命中），跑同一工具：`codetools comment-check --base-root /tmp/pre-edit --head-root . assembly/facts.go assembly/facts_test.go examples/wechat-bot/smoke_test.go` → **0**，`comment-check: 3 file(s), code identical under comment strip`。

**由此产生的重取**：`.go` 字节变动使根模块与 `examples/wechat-bot` 模块的证据按摘要脱树（C11.17 已记同一机理），6 长＋10 短＋1 变异的证据重取归编排者，C 侧不代跑、不追认。

**写面与越界**：写面＝`assembly/facts.go`、`assembly/facts_test.go`、`examples/wechat-bot/smoke_test.go`（仅注释文本）、本文件、仓外 `/tmp/pre-edit`（证据基线）。零改动：判据与棘轮基线、其余 Go 文件、`docs/**`、主 spec／delta、`golden/**`、`ORCHESTRATION.md`；未 commit／push／archive；F2 不勾。
