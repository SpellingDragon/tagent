# 09.7-review · 块5｜07 evolution／08 host／09 acceptance 三域终态回溯

> **审查对象**：`runtime-rebuild` 下 `07-evolution`（7 叶）＋`08-host`（7 叶）＋`09-acceptance`（9 叶），共 **23 孙任务**。
> **审查方式**：**只读**。逐叶比对 `task-graph.json`（verification／evidence／notes）、三域 `tasks.md` 行原文、`midterm-review-response.md` §二／§二·1 补差合同与具名负锚、`prototype/state.go`＋`contracts.go`＋`config.go` 原型语义、`golden/runtime/scenarios.json` 观测合同、`evidence/09.3-gate-run.md`、`evidence/09.6-doc-closure.md`、`evidence/09.7-retrospective-review.md`（总报告，本块做逐叶细化，可引用其结论）。
> **定位**：这是 09.7 的**分块细化报告之块5**；与同目录块2（`02-state-execution.md`）交叉引用处会显式标注，不重复展开。块2 已判的"双入口"（其 G3）在本块从 **08 域职责**角度重新取证并给出处置选项。
> **数字纪律**：凡标「**自报**」＝`task-graph.json` 该叶 `evidence` 记录的计数（本次未按 `-count=10` 全量重跑）；凡标「**实测**」＝本次会话真实输出，命令随文附在 §附。本次对 23 叶的点名测试做了 `-count=1` 抽验（宿主／evolution／微信／acceptance 四包全 ok），并对 09.4 做了 **30 轮×30 事件＝900 调用 `-race`** 的加压复跑（163.185s，exit 0）；**未**声称重跑了各叶 `-count=10` 的原始计数。
> **证据指纹**：HEAD `56c432c`（分支 `next`）＋**255 个未提交改动**（整场战役未提交；09.3 记录为 253，本次读数 255，**差 2 项来源未核**——`git status --short` 对未跟踪目录按单条 `??` 计数，`evidence/` 整目录只占 1 行，故**本报告写入不改变该计数**）；Go `1.27.1 darwin/arm64`；平台 macOS 14.7.2。
> **三轴**：`A`＝原型／规划要求且已实现；`B`＝原型／规划要求当前未实现（缺口）；`C`＝新增演化（原型与规划都没有的新面）。
> **红线执行**：本文**不给 PASS 汇总**。逐叶判据与缺口并列；凡口径矛盾、不可复跑、证据易失、登记未销账、待裁项一律进 §5，不静默通过。09.7 总报告"理想工程判据=是"的结论**不被本文件复制为总计**——本文件的结论边界就是 §5 的缺口与待裁清单。

---

## §1 逐孙任务表（23 行）

> 「落点」的 `file:line` 均为**本次实测定位**（`grep -n`／`sed -n`）；「验收」列区分**自报**（evidence 计数）与**实测**（本次复跑）。

| 叶 | 要求（tasks.md 原文要点） | 落点（file:line，实测） | 轴 | 验收结果与凭据 |
|---|---|---|---|---|
| **07.P0.1** | evolution 骨架：`StructuralGate`／声明类型／refine 接口签名（只声明） | `evolution/gate.go`、`evolution/refine.go:34 Refine`、`evolution/skeleton_assert.go:118-147`（6 个 skeleton 双） | A（骨架层，已被 07.4／07.2 覆盖） | `skeletonD-07p01`：build／vet／gofmt／零逻辑 grep（**自报**）。实测 `grep -rn 'phase 2: not implemented' evolution/ plugin/` → 6 处，**全部在 `skeleton_assert.go`**：P0 的"stub 全覆盖"锚今日仍可跑，但语义已从"产品未实现"变成"骨架双保留"——该锚不再能证明任何产品面状态 |
| **07.P0.2** | 反思观察／水位／摘要接口骨架（plugin／evolution 归属） | `plugin/observe.go:14 ObservationSource`、`evolution/watermark.go:96 NewAnchorStore`、`evolution/digest.go` | A（骨架层） | `skeletonE-07p02`（**自报**）；实测三处均已有产品实现；`NewAnchorStore` 是 07.P0.2 三件中**唯一有生产消费者**（`run.go:182`）的接口 |
| **07.1** | 反思主动输入：授权观察面（越界具名拒）／水位新鲜度 fail-closed／确定性自状态摘要／反思作为显式输入生产者注入本 agent 循环 | `evolution/observation.go:184 ObservationScan`、`:249 observe`、`evolution/reflection.go:187 NewReflection`、`:139 TickOutcome`、`evolution/reflection_scope.go`、`evolution/watermark.go`、`evolution/digest.go`；测 `tests/integration/evolution/reflection_scope_watermark_test.go:36/70/109/180/263/301/380` | A＋**B（生产接线）** | 正锚 `TestReflectionScopeAndWatermark` **80 pass / 0 fail**（**自报**，7 子测×10＋顶层）；本次 `-count=1` 复跑 ok（2.424s）。**实测 `NewReflection(` 全树零生产调用点**（仅定义行）；生产装配只剩 `run.go:182` 建 anchor store 供 `diagnostics.go:278 reflectionFace` 读水位 → 反思面在仓内是"机制＋测试"，**无生产消费者**（批次34 已裁"反思面收缩=可接受：生产面无 trigger_source 戳，不造测试后门"） |
| **07.2** | git 原生 refine 三操作：`register`（显式 pathspec＋结构化 commit）／`status`（台账）／`rollback`（标记校验）；越界拒绝且不动 git；不卷用户 index | `evolution/refine_git.go:117 NewGitRefine`、`:206 Rollback`、`evolution/refine.go:34`、`evolution/refine_scope.go`；测 `tests/integration/evolution/git_native_refine_test.go:124`（7 子测，临时仓） | A＋**B（生产接线）** | 正锚 **80 pass / 0 fail**（**自报**，M 回归重验：上游 03.3／01.6）；本次 `-count=1` 复跑 ok。**实测 `NewGitRefine(` 除定义与 `examples/contract` 范例零调用**；无 Run／CLI／宿主接线 → 生产消费者 0 |
| **07.3** | 激活时刻证据窗（**非** commit／register 时刻）＋真实消费版本归因＋四态（健康／劣化／样本不足／未到期）＋缺激活证据即样本不足 | `evolution/activation.go:36 Activation`、`:95 Consume`、`evolution/posterior.go:256 NewPosterior`（内部 `:258 NewActivationLedger`）、`evolution/judge.go`；测 `tests/integration/evolution/post_activation_evidence_test.go` | A＋**B（生产接线）** | 正锚 **80 pass / 0 fail**（**自报**，M 回归重验：上游 05.3）；本次复跑 ok。**实测 `NewPosterior(` 零生产调用点** → 生产消费者 0；四态与"样本不足不判健康"由测试钉住，但无宿主路径触发 |
| **07.4** | 结构评估门：Eval／Benchmark／ApplyGate 消费**同一**声明（OrgPlan）与**同代**执行绑定（StatePlan）；缺证据不报通过；门错误 `errors.Is/As` 因果保留；ApplyGate 消费既有评估证据不重发探针 | `evolution/structural.go:33 ErrDeclarationDiverged`／`:93 NewStructuralGate`／`:156` 拒错；`evolution/criteria.go:120/175/198`（判据 fixture 与 `ErrCriterionMissing`／`ErrCriterionNotMet`）；`evolution/policy.go:39 NewPolicy`／`:52 Check`（三段依序）；发布链 `state/publish.go:348 evaluate` → `governance.Check` → 三段 | A＋**C**（相对原型是严格化＋证据化） | 正锚 `TestStructuralEvaluation` **80 pass / 0 fail**；**负锚** `TestEvalRejectsWrongContentCompletion` **40 pass / 0 fail**（**自报**）。本次两测试 `-count=1` 复跑 ok。**实测唯一生产消费者**：`examples/wechat-bot/host_runtime.go:298` → `WithGovernance(evolution.NewPolicy(evolution.NewStructuralGate()))` → `Assemble` → `Publish`；且 `Assemble` 无治理即 `ErrGovernanceMissing`（01.4 负锚）→ **07.4 在正式装配是强制项，不是可选项** |
| **07.22** | evolution 大参数面拆块（`ObservationScan`／`ScannedFact`／`ReflectionConfig`／`TickOutcome`），拆后每结构 ≤5 字段；级联强度不降 | `evolution/observation.go:141 ScannedFact`（3 字段）、`:184 ObservationScan`（5）、`evolution/reflection.go:139 TickOutcome`（4）、`evolution/reflection_config.go:44 ReflectionConfig`（4，独立文件 54 行） | A | 静态锚**实测**：四结构 3／5／4／4 全 ≤5；`check_concept_budget --rule struct-fields` → 54 findings **全部 baseline 豁免**（`baseline_entries=54, baseline_exempted=54, baseline_stale=0`）。**但 tasks.md 的验证命令字面不可复跑**：`check_concept_budget.py --rule struct-fields evolution` → **exit 2**（`unrecognized arguments: evolution`）→ §5 H13。域级未清零：evolution 仍有 `posterior.go:Evaluation`（6）与 `refine.go:RefineRecord`（7）两处豁免 |
| **08.P0.1** | module 根 `package tagent` 公共 API 骨架：`Runtime`／`Build` 入口签名（只签名） | `tagent.go:35 Runtime`、`:52 CloseSession`／`:63 Shutdown`／`:74 Query`／`:93 Bridge`；`build.go:78 Build`；`skeleton_assert.go` | A（骨架）＋**B（从未被替换）** | `skeletonE-08p01`（**自报**）。**实测两入口今日仍 `return errNotImplemented`**；`Build(` 全树**零调用**（含测试与范例）；包 doc 仍称 "its only public entry"／"Build is the only way to obtain a runtime"（`tagent.go:3,32`），与活的 `Assemble`／`Run` 冲突 → §5 H6 |
| **08.P0.2** | 根 module 内微信宿主桥接口：只建根侧桥类型（wechat 修复属 08.2） | `host_bridge.go:29 HostBridge`；契约范例 `examples/contract/root/HostBridge_example.go:52` | A（骨架）＋**B（无装配）** | `skeletonF-08p02`（**自报**）。**实测** `Runtime.Bridge` 恒返回 `ErrBridgeNotAssembled`（`tagent.go:82/93`）；`HostBridge` 生产消费者 0（微信宿主走自有 `hostSendSeam`）→ §5 H7 |
| **08.1** | 在 02.5 根包装配入口上完善公开 SDK／CLI（构造／启动／关闭／诊断查询），不重复建立运行时；`/query` 请求主体认证 | `run.go:154 Assemble`、`:257 Publish`；`cli.go:38 ErrDiagnosticNotOpen`／`:43 ErrRequestNotAuthenticated`／`:88 NewCLI`／`:101 Main`／`:207 Serve`／`:242 authenticate`；测 `tests/integration/host/public_runtime_entry_test.go:36`（4 子测）、`diagnostic_endpoint_requires_authentication_test.go:121`（5 子测） | A（补差型） | 正锚 **50 pass**、负锚 **60 pass**（**自报**）；本次 `-count=1` 复跑两测试 ok。认证门先于路由（401 先于 404）、常数时间比对、未配认证不开诊断口、`/healthz` 同门。**注意**：正锚用的"唯一装配入口"是 `tagent.Assemble`（`public_runtime_entry_test.go:297`），不是 `Build` → 与 08.P0.1 的死桩并存 |
| **08.2** | 微信独立 module 迁移：用户／session 映射、入站去重、显式 `send` 与审批回执；旧 imports 按新设计迁移，不恢复旧符号 | `examples/wechat-bot/host_runtime.go:288 assembleHostFrom`（`:298` 治理）、`session_routing.go`、`approval_channel.go`；测 `session_routing_test.go:255`、守卫 `main_test.go:128 TestGuard_TestsNeverConstructRealChannel` | A（机制型）＋**流程缺口** | 正锚 `TestSessionRouting` **91 events（8 子测）**（**自报**）；本次复跑 ok。8 子测含会址单射／历史隔离／去重／缺身份拒／答复只落发言会话／显式 send 只达绑定会话／未绑定具名拒／审批摘要定位。**evidence 自登记**"入站拦截胶水 `main.go:301` 集成断言缺口 → 登记 08.4 补"，但 08.4 未销账（§5 H9） |
| **08.3** | 真实文件收发边界：类型识别、媒体发送、大结果副本（走 03.8 workspace 唯一副本）、拒可执行、失败具名回执、单文件失败不阻断 | `examples/wechat-bot/file_delivery.go`、`file_replica.go`、`main.go`；测 `file_delivery_test.go:394`（`assembleHostFrom` 真实装配）／`:434`（`host.Drive`） | A | 正锚 **4 子测×10 全绿**＋**四组变异反证**（`!Exceeds` 反转／`Materialize` 换 `Open`／删 `file-denied`／删 `file-failed` 均被抓，还原 `FILES_INTACT`）（**自报**）；本次复跑 ok。正式链为 inboundMessage→Drive→Invoke→deliverTurn→hostSender→fileDelivery，被测面=生产面 |
| **08.4** | 接 Query／反思／训练诊断实际开关，验证宿主启动／关闭／重启；关闭 OTel 不影响 Query | `diagnostics.go:69 Reflection`／`:89` 开关分派／`:278 reflectionFace`；`run.go`；`opts.go`；`cli.go`；测 `tests/integration/host/runtime_visibility_test.go:142/235/266/307/357/375`（6 子测） | A（机制型）＋**B（登记未销账）** | 正锚 **6 子测×10 全绿**＋**三组变异反证**（`diagnosticFace` 忽略开关／`lifecycleFace` 硬编码 `Closing:false`／`Diagnostics` 缓存首读，全被击落后还原）（**自报**）；本次复跑 ok。**未处理** 08.2 挂来的 `main.go` 入站胶水缺口；evidence 明示"run.go:210-211 NewDelegator+SetReconciler 接线行无变异锚，外部不可观测如实挂账" |
| **08.5** | 对已授权测试接收者验证真实文本／工具／文件与跨 session 往返（资源门，单次运行） | —（`TestRealWechat` 在全树**不存在**） | **B** | `task-graph` status=`planned`；实测 `grep -rn 'func TestRealWechat'` **零命中**；08 tasks.md `- [ ]`。R24 三态：未授权即 `BLOCKED`，不以 mock／替身冒充 → §5 H1 |
| **09.P0.1** | 场景 runner 类型骨架：observations／断言框架接口（仅类型，无 runner 逻辑） | `tests/integration/acceptance/runner_types.go`、`skeleton_assert.go` | A（骨架层） | `skeletonA-09p01`（**自报**）；类型面已被 09.1 的 runner／checker 全面覆盖 |
| **09.1** | 正式装配场景 runner＋golden 对照：真实观测、断言**值／消息角色序列／身份关系**、禁人工 PASS 汇总；`--scope complete` 全 10 严格，`deterministic` 只验 S01–S09 并显式列 `remaining_required` | `scripts/run_runtime_scenarios.py`、`scripts/check_runtime_evidence.py`、`tests/integration/acceptance/scenario_s01..s09_test.go`；合同 `golden/runtime/scenarios.json` | A（验收型）＋**C（机器对照体系）**＋**流程缺口（drift 未裁）** | **实测三复跑**：`--scope deterministic` → `run-20261018T095401Z-deterministic`，`required=9 attested=9 drift=3 unattested=0`，`remaining_required: S10`，**runner EXIT=3**、`verdict: drift`；`check_runtime_evidence --scope deterministic` → **EXIT=1**、**4 ERROR**（54/58 键相等，§1.1）；`--check-contract` → **EXIT=0**（形状合法≠行为闭合）。自测**实测**：runner 19、checker 54、plan 38 全 `OK` |
| **09.2** | 离线迁移与回退：只读旧数据导入新路径、缺失外置文件／不可恢复任务具名、中断重跑、回退不伪 restore 副作用；源字节不变 | `tests/integration/acceptance/offline_migration_test.go:226`（G2.1 源字节）／`:293`（G2.2 缺口具名）／`:338`（G2.3 中断重跑）／`:413`（G2.4 回退只读） | A（M 回归重验） | 正锚 `TestOfflineMigration` 4 子测；本次 `-count=1` 复跑 ok（0.521s）；M 回归（上游 03.3 重开，实现不判错零适配） |
| **09.3** | 两 module 完整 build／vet／test／lint＋第一方全包 race＋检查器负控；**逐条独立取原始退出码**；零失败不得由过滤输出得出；棘轮只降不升 | `evidence/09.3-gate-run.md`；`scripts/lint.sh`、`scripts/gen_godoc.sh`、`scripts/test_race_check.sh`；产品 race 两 module | A（门禁型）＋**B（2 门红）**＋**口径矛盾** | **16 门＝14 PASS／2 FAIL**（§1.2）：G09 `lint.sh` exit 1（`comment_policy: 494 finding(s); 494 beyond baseline`）、G10 `gen_godoc.sh --check` exit 1（27 STALE＋13 分歧）（**自报**）。本次**独立复现**：`lint.sh` EXIT=1、494 逐字；`gen_godoc` EXIT=1、`docs/api is out of date`；结构门 EXIT=1、16 文件>500。产品 race **实测**两 module exit 0、`DATA RACE` 零命中（root 688 pass／0 fail／8 skip；wechat 112／0／0）。**三处口径不一致**：evidence §0 自写"本单不可勾选（保持 planned）" vs tasks.md `- [x]` vs graph `done` vs ORCHESTRATION"完成" → §5 H5 |
| **09.4** | 常驻 soak、**进程崩溃恢复**、热更回滚、权限与资源泄漏（部署环境实跑）；未到位标 `BLOCKED`，不以 mock／优雅 Close／同进程 New 顶替；硬件断电不宣称 | `tests/integration/host/soak_continuity_test.go:114`（常驻）／`:160`（崩溃恢复）／`:234`（热更回滚）／`:278`（权限与资源泄漏） | A（3 PASS）＋**B（1 部分）** | **实测加压复跑**：`-race -args -rounds=30 -events-per-round=30`（＝900 调用）→ `ok 163.185s`，**EXIT=0**；默认 8×4 复跑 `ok 7.597s`。崩溃恢复：SIGKILL＋`ESRCH`＋无 `closed` 事件＋**异进程读回全部 900 任务逐一相同**＋空根恢复 0 负锚；权限资源泄漏：属主／无 world-writable／fd 与协程 31 采样有界。**热更回滚=前滚换代＋再发布旧内容（generation 递增）**，`Run` **无 Rollback 方法**（实测 10 个导出方法无此项）→ **专用 Rollback 通道 BLOCKED**（§5 H3）；硬件断电不宣称 |
| **09.5** | 各资源子场景必需验收（存储档／真实模型／MCP／A2A／embedding／collector／SFT／微信），按启用门逐项、互不等待；无门合法 Skip、有门缺凭据 Fatal、必验项 Skip 不结项 | —（用例存在，资源未授权；`TestRealWechat` 缺） | **B** | `task-graph` status=`planned`；09 tasks.md `- [ ]`；`go test -race ./... -short` **实测 8 个 skip 事件全为资源门**（`prototype.TestAgentRecoverySubprocess`、`TestRecallEvaluation`、`TestSearchRecallsFromRealStore/real endpoint`、`TestRealLLMJudgeArtifact`、`TestStorageCapabilities/rustviking`、`TestRealCollectorDelivery`、`TestSFTConsumer/tokenizer`、`host.TestSoakContinuity`）→ §5 H2 |
| **09.6** | 闭合：主规格 delta、文档／API 与消费者逐项闭合；**存在未裁项则如实保持未完成** | `evidence/09.6-doc-closure.md`（523 行，10 项未裁） | **B（三硬阻塞）** | `task-graph` status=`planned`；09 tasks.md `- [ ]`。本次**实测复跑** `check_runtime_plan --json`：`ok=true`、`spec_files=62, requirements=128, scenarios=447, checkboxes=119`、**`added=71 / modified=57 / removed=0`**；09.6 报告：71 ADDED 全部未入主规格、57 MODIFIED 正文 `mod_identical=0`、0 篇现行架构文档、296 条契约行 100% 指向历史 wiki、`docs/api` 27 stale＋13 分歧＋33 缺页、结构 16 文件>500（本次复现） → §5 H12 |
| **09.7** | 终态分块回溯 review（孙任务×领域×原型对照；辩证穿透不以契约为金标准；简单＋清晰双维） | `evidence/09.7-retrospective-review.md`（174 行）＋本块5 逐叶细化 | A（元验收） | `status=done`；总报告三轴分类＋双维评估，结论"理想工程判据=是"；**本文件是其逐叶细化，不复制其总判**，只把总报告一句式结论展开为可核对的 23 叶账与 13 项缺口 |
| **09.21** | 盲测终验（双锚两轮）：A＝仅读 plugin 接口＋范例实现新窄口；B＝仅读 query 读口＋范例实现新读源；判据＝编译过＋负向用例过；报告入 evidence | `/tmp/blind-a2/factid.go`（100 行）＋`factid_test.go`（4 测：1 正 3 负）；`/tmp/blind-b2/source.go`（340 行）＋`owners.go`（158 行）＋`source_test.go`（8 测，含越权拒／owner 拒／取消／冻结不跟随／副本语义） | A（C 类新验收） | 第一轮 A1／B1＝**部分达标（负结果入库）**：A 侧 `PostCommitObserver` 写不动、7 项"须读源码才懂"；B 侧 6 测可跑但 `SourcePosition` 类型底座／`ObjectRef` 相等语义／`Detail.Blocked` 缺范例。补差（00.26＋W2）后**第二轮 A2／B2＝达标**（**自报**：A2 1 正 3 负全过；B2 8 测全过＋变异反证两处）；本次**实测实物存在**（见落点）。**证据只在 `/tmp`（易失），仓内无盲测报告** → §5 H11 |

### 1.1 09.1 展开：golden 对照的 4 处 drift（3 场景 / 4 观测键 / 6 行逐字差异）

**实测命令**：`python3 scripts/run_runtime_scenarios.py --scope deterministic --out /tmp/review097b`
**实测输出**：`run_id: run-20261018T095401Z-deterministic`；`scope: deterministic`；`remaining_required: S10`；`verdict: drift`；`scenarios: required=9 attested=9 drift=3 unattested=0`；**runner EXIT=3**。
**第二信源**：`python3 scripts/check_runtime_evidence.py --evidence /tmp/review097b/evidence.json --test-log /tmp/review097b/test-log.jsonl --scope deterministic` → **EXIT=1**，`RESULT: FAILED`，**4 ERROR**（`counts.observation_values=70`、确定性档 58 键、相等 54 键）。

| # | 场景 | 对照轴 | 观测键 | golden 期望 | 产品实测 | 归因类型（详见 §3.1） |
|---|---|---|---|---|---|---|
| 1 | S03 | message-role-sequence | `model_message_roles` | `[user, assistant, tool, assistant]`（4 元） | `[user, assistant, tool, tool, user, user]`（6 元；差异行 2 条：`[3]` 与 `[len]`） | **契约过时**（同步心智）＋**契约形态脆弱**（固定向量） |
| 2 | S03 | identity-relation | `receipts[1].decision` | `decision-b` | `decision-a` | **关系基数错**（把 1:N 写成 1:1） |
| 3 | S08 | identity-relation | `blocker_detail_rows[0].advancer` 与 `.blocker` | `task-a` / `storage-degraded` | `notice-transport` / `task: the receiving instance is closed and its undelivered notifications are retained` | **观测面选择不当**（自由文本进身份轴＋闭词表类别当任务 id）＋**场景意图错位**（场景从未构造 storage 降级） |
| 4 | S09 | value | `tokenizer_capability` | `unknown` | `absent` | **词表外值**（`unknown` 不在产品闭集） |

> 键数自洽：`54 相等 + 4 不等 = 58` 确定性键；`58 + 12（S10）= 70` 合同观测值。即"4 处"是 **4 个观测键**、**6 行逐字差异**、**3 个场景**；三个口径都成立，不可混用。

### 1.2 09.3 展开：16 门逐条（14 PASS / 2 FAIL）

| # | 门禁 | exit | 结论 | 本次复核 |
|---|---|---|---|---|
| G01 | `go mod verify` | 0 | PASS | — |
| G02 | `go build ./...` | 0 | PASS | — |
| G03 | `go vet ./...` | 0 | PASS | — |
| G04 | `go test ./... -short -count=1 -json` | 0 | PASS（688 pass／0 fail／7 skip，**自报**） | 本次 `-race` 实测 688 pass／0 fail／8 skip 事件 |
| G05 | `GOMAXPROCS=1 go test . -short` | 0 | PASS（**空转**：根包 `[no test files]`） | 本次枚举 18 个产品包**内部测试 0 个**（§5 H10） |
| G06 | `go -C examples/wechat-bot build` | 0 | PASS | — |
| G07 | `go -C examples/wechat-bot vet` | 0 | PASS | — |
| G08 | `go -C examples/wechat-bot test -short` | 0 | PASS（112 pass／0 fail／0 skip，**自报**） | 本次 `-race` 实测 112 pass／0 fail |
| G09 | `bash scripts/lint.sh` | **1** | **FAIL** | **实测复现**：EXIT=1、`comment_policy: 494 finding(s); 494 beyond baseline` |
| G10 | `bash scripts/gen_godoc.sh --check` | **1** | **FAIL** | **实测复现**：EXIT=1、27 STALE＋13 分歧＋33 新增面 |
| G11 | `bash scripts/test_race_check.sh` | 0 | PASS（检查器自测，**不等于**产品 race） | — |
| G12 | `python3 scripts/test_verify_restart_prefix.py` | 0 | PASS | — |
| G13 | `bash scripts/check-openspec.sh` | 0 | PASS（121 items） | — |
| G14 | `openspec validate --all --strict` | 0 | PASS（121 items） | — |
| G15 | `go test -race ./... -short`（根） | 0 | PASS | **实测** 688 pass／0 fail／**DATA RACE 零命中** |
| G16 | `go -C examples/wechat-bot test -race ./... -short` | 0 | PASS | **实测** 112 pass／0 fail／零 DATA RACE |

> 口径矛盾（须裁）：`09.3-gate-run.md` §0 原文"**09.3 门禁未全绿，本单不可勾选（保持 planned）**"，而 `09-acceptance/tasks.md:26` 已是 `- [x]`、`task-graph` 是 `done`、ORCHESTRATION 批次37 记"完成"。三者中至少两处必须改；本报告不代裁。
> 另：G04／G15 的 688 是**集成包**计数——18 个产品包内部 0 个 `*_test.go`（本次实测），"产品包零包内测试"由 09.3 §5.2 自报，本次复核为 18/18（09.3 记 16，属包枚举口径差）。

### 1.3 09.4 展开：soak 四件

| 件 | 子测（实测行号） | 本次结果 | 证据要点 | 判读 |
|---|---|---|---|---|
| ① 常驻 soak | `soak_continuity_test.go:114` | **PASS** | 长跑 **30×30＝900 调用 `-race` → 163.185s, EXIT=0**；轮号连续、pid 连续、`Close` `Drained` 且 `Unsettled=0` | A |
| ② 进程崩溃恢复 | `:160` | **PASS** | `kill -9`（`SIGKILL`）→ `ESRCH` 探活 → 无 `closed` 事件 → **异进程读回 900/900 任务逐一相同** → 空根恢复 0 负锚 | A（硬件断电不宣称） |
| ③ 热更回滚 | `:234` | **PASS（语义仅为"前滚＋内容回退"）** | `gen1→gen2→gen3` 同现场连续；"回退"= 再发布旧内容，**generation 递增** | A（非 Rollback 通道） |
| ④ 权限与资源泄漏 | `:278` | **PASS** | 存储属主／无 world-writable；fd 与协程 31 采样有界（2／6） | A |
| 附加：**专用 Rollback 通道** | — | **BLOCKED** | `Run` 导出方法实测 10 个，**无 `Rollback`**；域内有 `state.Publisher.Rollback:272`／`state.Router.Rollback:254`／`state.RollbackArchive:268`／`evolution.Refine.Rollback:34` | **B** → §5 H3 |

### 1.4 09.21 展开：盲测 A1／A2／B1／B2

| 轮/锚 | 面 | 判据 | 结果 | 残余 |
|---|---|---|---|---|
| **A1** | plugin `TransformCall` 面 | 编译过＋负向用例过 | **部分达标**（负结果入库） | `After` 侧 `PostCommitObserver`／`FinalPresenter` 写不动（`Presentation`／`event.EventID` 类型底座不在契约面）；7 项"须读源码才懂"（`PhaseRiskPolicy` 双面歧义／transform error 后果未写／空 `RiskLevel` 语义陷阱等） |
| **A2** | plugin `PostCommitObserver`（最难面） | 同上 | **达标** | 4 测（1 正 3 负：gap 断言／注册拒绝／形状拒）全过；报告"没有读任何框架源码写出可编译实现"；残余 3 点登记（After 签名指引／空 `EventID` 语义／`FactLookup` 关系） |
| **B1** | query 读源 | 同上 | **部分达标** | 可编译可跑（6 测＋变异反证），但 `SourcePosition` 命名类型硬阻塞／`ObjectRef` 相等语义与 `Order` 序契约缺失／`Detail.Blocked` 无范例／授权错误分层靠读实现 |
| **B2** | query 三层（Reading／ReadingSource／DetailSource＋真实投影） | 同上 | **达标** | 8 测全过含负例（越权具名拒／owner 拒／取消／冻结不跟随／副本语义）＋变异反证两处；第一轮三缺口全消 |

> **可复验性缺口**：A2／B2 的实物在 `/tmp/blind-a2`、`/tmp/blind-b2`（本次实测存在：100 行＋4 测；340＋158 行＋8 测），但 `task-graph` 的 `artifact_paths` 也指向 `/tmp`，**仓内无盲测报告**。`/tmp` 一旦清理，两轮盲测只剩 `ORCHESTRATION` 的一句结论 → §5 H11。

### 表注（逐条可复验）

1. **23 叶 status**：`done` **20**（07 域 7／08 域 6／09 域 7）、`planned` **3**（08.5／09.5／09.6）。"全 done"的域内叙事在 07 域成立，08／09 不成立。
2. **完成类型不可混计**：本块含 ①补差型（07.4／08.1，中检重开）②M 回归重验型（07.2／07.3／09.2，实现零适配）③机制型（07.1／07.22／08.2／08.3／08.4）④骨架型（5 个 P0）⑤门禁型（09.3／09.4）⑥验收型（09.1／09.21／09.7）。六种强度的验收含义不同，**不得合并为"同等完成"**。
3. **第五查（生产消费者实测）结果**：07.4＝1 处真实生产消费者；**07.1／07.2／07.3＝0**；08 域的生产消费者是宿主本身（08.2／08.3 经 `assembleHostFrom`＋`Drive`，08.4 经 `Run.Diagnostics`），08.1 的正锚经 `Assemble`；09 域自身即验收面。故"23 叶全部具备生产消费者"**不成立**，其中 07 域 3 叶为测试／范例级消费者。
4. **点名测试存在性与抽验**：23 叶点名的测试**除 `TestRealWechat` 外全部存在**（与 08.5／09.5 planned 一致）。本次 `-count=1` 抽验：host 三测试 ok（0.289s）、evolution 五测试 ok（2.424s）、wechat 两测试 ok（0.312s）、acceptance `TestOfflineMigration` ok（0.521s）。
5. **数字自洽**：`54+4=58`；`58+12=70`；`14+2=16`；soak `900=30×30`；盲测 B2 `8` 测与实物一致；checker 自测 `19+54+38=111`。所有数字均给命令（§附）。

---

## §2 原型对照

### 2.1 特性级对照：演进判定

| 原型特性 | 原型位置 | 实现落点 | 判定 |
|---|---|---|---|
| `Evolution{Eval, Benchmark, ApplyGate func(StatePlan) error}`——三段**只有入参、无证据、无报告、无因果** | `prototype/contracts.go:109-112` | `evolution.StructuralGate`：`Eval/Benchmark/ApplyGate(ctx, OrgPlan, StatePlan) (GateReport, error)`；`GateReport` 携 verdict／reason／scope／`EvidenceRefs` | **A 改造**：入参从"一个 Plan"变为"**声明＋同代绑定**一对"；返回值从 `error` 变为"**裁决＋证据引用**"；错误从字符串变为 `errors.Is/As` 可达链 |
| `runEvolution` 依序跑三段，**nil 段恒真跳过**，任一段 error 即中止 | `prototype/state.go:351-362` | `evolution/policy.go:52 Check` 依序 Eval→Benchmark→ApplyGate，把每段结果映射为 `state.CheckAnswer`；`state/publish.go:348 evaluate` 再按 `PublishContract{Required}` 判"缺检查即拒" | **A＋C（反转）**：原型的"没配就不校验"被反转为"**必需检查缺失即拒绝发布**"（`ErrRequiredCheckMissing`）；这是本块最重要的一次语义反转 |
| 演进判定**原型明示未实现**（`config.go` 头注"未实现：… 演进判定 …"） | `prototype/config.go:5` | 07 域整套：`observation.go`／`reflection*.go`／`activation.go`／`posterior.go`／`judge.go`／`refine*.go` | **C（真新增）**：07 域不是"恢复原型"，而是**原型点名未做的能力**；三轴分类必须记 C，不能记 A |
| 原型的 `State.Apply` 走"同 id 换 def＋prompt／新 id 建现场／缺 id detach" | `prototype/state.go:267-330` | `state/publish.go:261 Publish`→`publishLocked`（基线复核→同指纹不换代→`evaluate`→`commitRecord`→`Hold`→retire） | **A（承块2）**：机制归 01 域；本块只旁证 07.4 嵌在 `evaluate` 的治理缝上，而非另起第二条发布路径 |
| 原型无"代"概念：`Apply` 换 def 即对在飞请求生效 | `prototype/state.go`（无 Binding／Generation 类型） | `state.AgentBinding{AgentID, InstanceID, Generation}`＋`Borrow/BorrowDerived/Release`；`Policy.Check(ctx, plan state.StatePlan)` 收的正是同代绑定 | **C（真新增）**：07.4 的"同代"不是修辞——`structural.go:156` 的 `ErrDeclarationDiverged` 就是"绑定代与声明不一致即拒"的具名证据 |

### 2.2 专节：从"演示钩子"到"真机判据"的四级严格化

| 级别 | 判据形态 | 谁能通过 | 谁在拦 | 证据 |
|---|---|---|---|---|
| **L0 原型** | `nil` 段恒真；没有 Eval 段＝自动通过 | 任何候选 | 无人 | `prototype/state.go:351-362` |
| **L1 被否的中间态** | 只看"模型调用是否 `Completed`"（探针式） | **内容错误但跑完的候选也能过** | 无人 | `midterm-review-response.md` §二 07.4 补差合同原文："不以 Completed 探针冒充" |
| **L2 判据式 Eval／Benchmark** | 声明内 fixture：`EvaluationCriterionRef="evaluation"`＋`id/task/expect/baseline`；对每个 `PlanChange` 取候选 binding 的模型真跑，按 face 比对 expect／baseline | 内容正确的候选 | `ErrCriterionMissing`（无判据）／`ErrCriterionNotMet`（内容不符） | `evolution/criteria.go:120/175/198`；负锚 `TestEvalRejectsWrongContentCompletion` 40 pass（`structural_test.go:737`） |
| **L3 证据消费式 ApplyGate** | **不再重发探针**，消费已记录的 `evidenceRecord`；发布面按 `Required` 判缺 | 有证据且证据通过的候选 | `ErrRequiredCheckMissing`／`ErrCheckRefused`；旧代不变 | `structural_test.go:772`（"consumes the recorded evaluation evidence and drives no probe"）＋`:822`（错误内容发布被拒且不生效）；`state/publish.go:348-384` |

**严格化的本质**：原型的三段是**"钩子"**（给宿主一个插手点，做不做随你）；实现的三段是**"裁决"**（有判据、有证据、有拒绝、有旧代不变量）。四个升级点缺一不可：①判据必须**声明内**（不得各门自建影子配置，D07-6）②判据必须**真跑候选模型**（不得用探针冒充）③ApplyGate 必须**消费证据**（不得重发探针，否则门自身成为第二事实源）④拒绝必须**具名且旧代不变**（否则"拒了但已经生效"）。

**严格化的代价（如实）**：①判据载体是 `Tools` 里的 `EvaluationCriterionRef` fixture——**声明必须自带评估用例**，否则 `ErrCriterionMissing`；②三段的真实模型路径只有 1 个生产消费者（`wechat-bot`），其余正式装配必须用测试替身（`rejectPolicy`／`passPolicy`），真实模型面挂 09.5；③`ApplyGate` 消费证据意味着**证据必须先落账**，把"评估"与"发布"耦合到同一条 durable 链上（与 01.5 的 `PublicationRecord` 同源）。

### 2.3 07 域演化的"面在库里、宿主未接"

原型的 `config.go:5` 把"演进判定"列为未实现；实现把 07 域做成了**库能力**（`evolution` 包的公开构造器），但仓内生产装配只接了 07.4：

| 能力 | 公开构造器 | 生产装配调用点 | 消费者级别 |
|---|---|---|---|
| 结构评估门（07.4） | `NewStructuralGate`／`NewPolicy` | `examples/wechat-bot/host_runtime.go:298` | **生产** |
| 反思主动输入（07.1） | `NewReflection` | **无**（只有 `run.go:182` 的 `NewAnchorStore` 供水位给诊断） | 测试（`reflection_scope_watermark_test.go:380` 用真实 `agent.NewLoop`） |
| git 原生 refine（07.2） | `NewGitRefine` | **无** | 契约范例＋测试 |
| 后验评估（07.3） | `NewPosterior` | **无** | 测试 |

**判读**：这不是"07 域没做"，而是"07 域的**能力面已完成、装配面只接了一处**"。按 D07-1"反思在 state 借出的同代插件管线内按阶段接入"的设计原文，07.1 的装配面**应当**有生产接点；现状与设计原文有差距。批次34 只裁了"反思面收缩=可接受"，未覆盖 07.2／07.3 → §5 H8。

---

## §3 辩证分析

### 3.1 golden 4 处 drift 的逐处特性归因（不以契约为金标准的核心案例）

**先给结论**：4 处 drift 中，**没有一处是"产品缺特性"**；但**只有 2 处可以简单说"golden 过时"**（S03 roles、S09 词表），另 2 处若只改值就会把错误固化（S03 receipts 基数、S08 观测面）。

#### ① S03 `model_message_roles`：契约过时 ＋ 契约形态脆弱

- **产品实态**：`[user, assistant, tool, tool, user, user]`（6 元）。第 2 元 `assistant` 携带**两个** tool_call；第 3、4 元是两条**受理回执**（role=tool，`ToolID` 保真）；第 5、6 元是两条**后台结算观察**（role=user，作为新输入驱动下一决策）。
- **归因**：golden 的 4 元 `[user, assistant, tool, assistant]` 是**规划期按同步心智写的观察想象**（一次调用→一个 tool 结果→一个 assistant 收尾）。02.5 的 M2 补差合同明文要求"Receipt 进 transcript；后台结果独立观察回流"，因此一次带双 call 的决策必然产生 **assistant（含双 call）＋2 receipt＋2 observation**，而非 4 元。块2 §3.2 已就本项给出同源归因，本块补充**形态层面**的判断：
- **不能只改值**：把 `[user, assistant, tool, tool, user, user]` 抄进 golden，等于让 golden 成为"当前实现快照"，**观测器就退化成期望值复制器**（09.1 任务行明文禁止）。角色向量还依赖"本轮有几个 call／几个后台结算"，是**场景参数的下游函数**，不是不变量。
- **建议改法（待裁）**：把该观测项改成**相对结构断言**——`assistant(tool_calls=N)` 后必须出现 N 条 `role=tool` 回执，再出现 N 条 `role=user` 观察；或按 call 切片声明。若坚持固定向量，必须同时冻结场景的 call 数（当前 S03 恰为 2）。

#### ② S03 `receipts[1].decision`：关系基数错（1:1 vs 1:N）

- **产品实态**：两条 receipt 的 `decision` 都是 `decision-a`。测试 `scenario_s03_test.go:92-99` 显式计算"**携带 issuing turn 的那一个 decision**"（并断言 `issuingCount==1`），再把同一 `DecisionID` 标给两条 receipt。
- **归因**：`agent.Decision`（`agent/invocation.go:73`）的语义是"**一次模型决策**"；一个 decision 的 assistant 消息可携带**多个** call。故 `decision → calls` 是 **1:N**，golden 把 receipts 写成"每 call 一个不同 decision"是 **1:1 的臆想分配**。
- **不能只改值**：把第二条改成 `decision-a` 后，`receipts` 数组仍是"call 行"，信息上无法表达"这两条同属一个决策"——**基数没有被表达出来**，下一个场景（3 个 call）还会再错一次。
- **建议改法（待裁）**：golden 改为 `decisions: [{decision: "decision-a", calls: ["call-a","call-b"], tasks: [...]}]` 的 1:N 形态；或保留 receipts 行但增加 `issuing_decision_count: 1` 断言。

#### ③ S08 `blocker_detail_rows[0].advancer/.blocker`：观测面选择不当 ＋ 场景意图错位

- **产品实态**：`advancer="notice-transport"`；`blocker="task: the receiving instance is closed and its undelivered notifications are retained"`。
- **产品语义（实测）**：`query.Advancer` 是**闭词表**——`executor`／`diagnosis`／`explicit-stop`／`notice-transport`（`query/explain.go:78-89`），语义是"**下一推进方类别**"，**不是任务 id**；`Blocker.Reason` 的文档原文是"**kept exactly as its owner records it**"（`query/explain.go` 字段注释），即**自由文本**，测试 `scenario_s08_test.go:234` 直接 `"blocker": blocker.Reason` 原样搬运。
- **更硬的发现（本次新取证）**：S08 的场景**根本没有构造 storage 降级**。`scenarioBlockedTask`（`scenario_s08_test.go:76-124`）构造的是"**通知欠给已关闭实例**"——`closedInstance = "instance-closed"`，`manager.CloseInstance` 后 `Flush`，于是 `Obligations.Blocked[0].Reason` 就是那句"receiving instance is closed…"。`memory.OpenTier(...StorageLocalFile...)` 是健康的本地档，**没有任何降级注入**。
- **归因**：这不是"S08 的 golden 值过时"，而是**两条独立错误叠在一起**：① golden 描述的是**另一个场景**（storage-degraded 从未被构造）；② 即便场景对上了，把**自由文本**放进 `identity-relation` 轴也必漂（文案一改就红，改文案又不改语义），而 `advancer` 的期望 `task-a` 是**类别错误**（把"推进方类别"当"任务 id"）。
- **不能怎么改**：**不能**把整句英文文案写进 golden；**不能**把 `advancer` 期望改成 `notice-transport` 就收工（那只修了类别错误的一半，`blocker` 仍是自由文本）。
- **建议改法（待裁，二选一）**：
  - **(a) 场景对齐契约**：真的注入一次存储降级，并由产品在 `Blocker` 上增加**稳定 code**（闭集，如 `blocker_kind: storage-degraded`），golden 断言 code＋advancer 类别＋object／owner＋`blocker_statistic_matches_detail`；
  - **(b) 契约对齐场景**：承认 S08 验的是"关闭实例未投递通知"，golden 只断言 `advancer` 类别、`object`／`owner` 身份与统计自洽，**自由文本移出身份轴**（或只断言"非空且含 owner 记录的短语"）。

#### ④ S09 `tokenizer_capability`：词表外值

- **产品实态**：`absent`。产品闭集见 `rl/sft_consumer.go:18/29/31`：`real`／`deterministic`／`absent`。
- **归因**：golden 的 `unknown` **不是产品的任何一档**。S09 的测试注释写明"no tokenizer capability is claimed where none was verified"（`scenario_s09_test.go:33`）——`absent` 正是"未验证就不得声称"的正确表达；`unknown` 是规划期凭空预留的档位。
- **建议改法**：golden 改 `absent`，并把"未验证不得声称"提升为**负向断言**（不得出现 `real`／`deterministic`），否则改值只是让红灯变绿，语义没有被锁住。

#### 归因汇总

| 归因类型 | 处数 | 处置方向 | 风险 |
|---|---|---|---|
| 契约过时（同步心智） | 1（S03 roles） | **改契约**，但改成相对结构而非快照 | 抄观测值会让 golden 失去判据性 |
| 关系基数错 | 1（S03 receipts） | **改契约形态**（1:N 显式化） | 只改值会复发 |
| 观测面选择不当＋场景错位 | 1（S08 两字段） | **先裁场景意图**，再决定产品加 code 还是契约降维 | 直接抄文案＝把自由文本变成契约 |
| 词表外值 | 1（S09） | **改契约值**＋补负向断言 | 只改值不锁语义 |

### 3.2 "不以契约为金标准"的可操作判据

从这 4 处（＋原型 nil 恒真）可以提炼四条规则，供 09.7 后续块复用：

- **R1 先定位语义位置，再谈一致性。** 同一份数据在不同语义位置可以有不同形态：活观察用 `RoleUser`（它是新输入），回放用 `RoleTool`（它是历史原貌）——块2 已裁"一致性不能跨语义位置强加"。S03 的角色序列必须先回答"这是回放视图还是活 transcript"，再谈 4 元还是 6 元。
- **R2 身份轴只收闭集／稳定标识，自由文本只能进展示轴。** S08 的 `blocker` 是 owner 记录的**自由文本**，`advancer` 是**闭集类别**；把前者放 `identity-relation`、把后者期望成任务 id，是**轴与值的双重错配**。这条规则同时解释了为什么 `ErrDeclarationDiverged` 用类型化错误而不是字符串（07.4 的设计同源）。
- **R3 冲突先判"产品是否缺特性"。** 缺→产品错（如 07.4 补差前的"Completed 探针"）；不缺且契约表达错位→契约错（本块 4 处全属此类）；**不得**用"契约写了"倒推"产品该这样"。
- **R4 契约本身可错，纪律必须留裁改通道。** S08 证明：golden 可以在**场景都没对上**的情况下存在整整一个波次。若 09.x 的纪律只有"必须过 golden"而没有"golden 可被裁改并留归因"的通道，纪律就会**固化错误期望**。09.7 的裁决面（本报告 §3.1／§5 H4）就是这条通道；R30 的偏差评估是同一通道的另一种形态。

**三个"契约被否决"的实证**（不以契约为金标准的核心案例）：

1. **原型 `nil` 段恒真**被 01.5 反转成"缺必需检查即拒"——原型行为被**明确否决**，不是被继承（块2 §3.3 已列，本块从 07.4 侧再证：`ErrCriterionMissing` 是硬拒）。
2. **S08 golden 整行**（`storage-degraded`／`task-a`）被本次取证证明"场景从未构造"——契约描述的是**不存在的场景**，必须整行重写而非改值。
3. **S03 `receipts` 的 1:1** 被 `Decisions` 语义否决——一次决策可发多 call，契约的**关系基数**错，不是值错。

### 3.3 09.x 验收纪律（R30／盲测）的特性本质＝多实现方生态的准入成本

**本质一句话**：tagent 的扩展面（plugin 相位、query 读源、Governor、Embedder、Executor、DetailSource）是**多实现方**的；每一个新实现者都要跨过同一道认知门槛。09.x 的纪律不是"测试覆盖率工程"，而是**把这道门槛显性化、可测量、可收敛**的工程投资。

**成本侧（本次实测／自报）**：

| 成本项 | 量 | 说明 |
|---|---|---|
| 机器门 | **16 门**（09.3） | 每条独立进程、独立退出码；14 PASS／2 FAIL，2 红为存量债 |
| 观测合同 | **70 键**（18 合同字段／10 场景；确定性 58） | 三轴（value／message-role-sequence／identity-relation）机器对照 |
| 检查器自测 | **111**（runner 19＋evidence 54＋plan 38） | 检查器自身也要被负控，否则"检查器红/绿"不可信 |
| 盲测 | **2 锚×2 轮** | 第一轮部分达标→补差（00.26 七缺口＋W2 Blocked 范例）→第二轮达标 |
| R30 偏差评估 | **17 轮**（总报告口径） | 每波"有条件勾选→discharge" |

**收益侧（实证）**：A2 的原始报告是"**没有读任何框架源码写出可编译实现**"——这是"接口自足"的唯一硬证据，任何静态检查都替代不了；B2 三层 8 测含越权／owner／取消／冻结不跟随／副本语义，证明**负向语义**也能只靠契约面写对。第一轮 7 项"须读源码才懂"→第二轮全消，说明补差是**有效投资**而非文档美化。

**成本曲线（第一轮→第二轮）**：补的是 **7 个具名缺口 + 1 个 W2 挂账**（`Detail.Blocked` 范例），换来的是"双锚达标"。边际成本递减的证据是：A2 选的是**最难面**（`PostCommitObserver`，`Presentation`／`After` 不在接口文件里），仍能一次写对——说明第一轮的补差打在了**类型底座与范例**这两个真正的瓶颈上，而不是打散点。

**纪律的第二重本质**：**用机器对照替代人工 PASS 汇总**。`09.1` 的任务行明写"禁用人工 PASS 汇总"；`check_runtime_evidence` 对"子测试 Skip／包级 Fail／未知断言／缺 case／错值"一律失败。这条纪律防的不是"人偷懒"，而是**观测器退化为期望值复制器**——一旦允许人工汇总，"跑出来什么"和"期望是什么"就失去独立性。S08 的教训正好相反：**契约自身也会错**，所以纪律必须是"机器对照＋可裁改的契约"，而不是"机器对照＋不可质疑的契约"。

---

## §4 双维局部评

### 4.1 简单维（概念统一、收敛）——实测数字

| 面 | 实测值 | 命令 | 判读 |
|---|---|---|---|
| 全仓接口／包／go 文件／结构体 | **74／18／176／545** | `check_concept_budget --rule struct-fields --json` | 宏观规模（块2 已引 74/18/176，本块补 545 structs） |
| 结构体超 5 字段 | **54 findings，54 全 baseline 豁免，`baseline_stale=0`** | 同上 | 豁免是**具名登记**，不是静默放行；但"豁免 54"意味着**简单维的总账仍是 54 项未清** |
| 07.22 四结构 | `ScannedFact 3`／`ObservationScan 5`／`TickOutcome 4`／`ReflectionConfig 4`；`reflection_config.go` 54 行 | `grep -A12 'type ... struct'` | **叶级达标**：四结构 10→5／7→3／9→4／6→4（ORCHESTRATION 批次27 自报），96 断言零删 |
| evolution 域残留 | `posterior.go:Evaluation 6`、`refine.go:RefineRecord 7` | 同上（findings 过滤 `evolution/`） | **域级未清零**：07.22 的"拆块"是**按叶定点**，不是域级收敛 → §5 H14 |
| 根包公开面 | **18 个导出函数**（13 个 `With*`＋`Assemble`／`Build`／`NewCLI`／`ExecLeaseExplainer`／`DiagnosticSwitchesFromEnv`） | `grep -hn '^func [A-Z]' *.go` | 其中 **`Build` 死桩**；`Run` 10 个导出方法、`CLI` 2 个（`Main`／`Serve`） |
| 08 宿主规模 | 微信 module **20 个 go 文件（12 非测试＋8 测试）** | `ls examples/wechat-bot/*.go` | 独立 module、独立门禁（D08-2） |
| 09 验收面 | **3 个脚本**（runner／evidence／plan）＋**1 个 golden**＋**70 键**＋**16 门**＋**4 件 soak**＋**2 锚盲测** | 见 §附 | 工具数少、职责单点，是简单维的正例 |

**判读**：07.22 是**局部正例**（四结构一步降到 ≤5、级联零删），但"evolution 参数面拆块"的域级目标未达成（仍有 6／7 字段两处）；08 的公开面在**数量上**很收敛（13 个 `With*` 是选项模式，不是 13 个概念），但**双门面**（`Build`/`Runtime` vs `Assemble`/`Run`）是简单维的硬伤——同一件事有两个入口、其中一个是死的；09 的"三脚本＋一合同"是简单维最好的部分。

### 4.2 清晰维（职责明确、抽象单一、丰富多态）

**职责明确**

| 域 | 职责边界（实测） | 判读 |
|---|---|---|
| 07 | `structural.go`（门）／`criteria.go`（判据）／`policy.go`（治理适配）／`refine*.go`（git）／`reflection*.go`（反思）／`activation.go`＋`posterior.go`＋`judge.go`（后验）——**一文件一义** | **文件级清晰**；但 `observation`／`reflection`／`posterior` 三者的边界只在包 doc 里，**没有消费者验证**（生产接线 0）→ 清晰度"未经消费验真" |
| 08 | 只做"装配／驱动／暴露"：`Assemble`＋`Run`＋`CLI`＋`Diagnostics`；不实现 state／agent／task 机制（D08-1／D08-5） | **边界清晰**，但**四个死／重面**污染：`Build`／`Runtime`（死桩）、`HostBridge`（未装配）、`Deps.Gate`（死端口，`build.go:29`）；`main.go:333` 的入站胶水零覆盖 |
| 09 | runner（观测）／checker（判定）／golden（合同）**三者分离**；资源门三态（无门 Skip／有门缺凭据 Fatal／必验 Skip 不结项） | **分离度最好**；但 09.3 的"门禁"与"收尾账"边界模糊（14/16 仍被勾选），09.6 又把"未裁项"与"闭合"分开——**同一份验收叙事有两种完成口径** |

**抽象单一**

- 07.4：`StructuralGate` 三段同签名、同入参、同报告类型；`Policy.Check` 只做"映射＋`errors.Join`"，不夹带策略。
- 08.1：`CLI.authenticate` 是**单一认证点**（`cli.go:242`），401 先于 404，`/healthz` 同门——"无端点旁路"由测试钉住。
- 09.1：`check_runtime_evidence` 只做"合同 vs 证据"的机器对照，不做"合理推断"；`--check-contract` 与运行证据**分模式**（形状 vs 行为），这是很干净的一刀。

**丰富多态**

- 扩展面：`DetailSource`（`blockedDetails`／`freezeDetails` 等多实现）、`Governor`（真策略／测试替身）、`Embedder`（Hash／真实端点）、`Executor`（fake／真实）、`Recorder`、`ObservedFacts`（`examples/contract/evolution/ObservedFacts_example.go` 示范"条件检索"）。
- **实证**：A2 在 `PostCommitObserver`（最难面）上"不读源码写出可编译实现"；B2 在三层读源上 8 测全过。多态不是"接口多"，而是"**新实现者能只靠契约面写对**"——这一点有盲测硬证据。

**清晰维的扣分项（本块）**：

1. `tagent.go` 的包 doc 与代码**互相矛盾**（doc 说 `Build` 是唯一入口，代码里 `Assemble` 才是；`Build` 零调用）。这不是"文档陈旧"级别的问题——它会让**新宿主作者按 doc 写出跑不起来的代码**（与盲测同类的准入成本）。
2. `HostBridge` 有契约范例、有接口、有 `Runtime.Bridge` 入口，但**永远返回 `ErrBridgeNotAssembled`**；微信宿主另走 `hostSendSeam`。这是"**声明面存在、实现面不存在、消费者面绕开**"的三重脱节。
3. 09.3 的"14 PASS／2 FAIL 已勾选"让"门禁"这个词在验收叙事里**失去唯一含义**：读者无法从勾选状态判断"门禁是否全绿"。

---

## §5 缺口与待裁

> 本节**不是 PASS 的反面清单**，而是本块全部的未闭合项与裁决点。类型：`B`＝要求未实现；`流程`＝口径／证据／登记问题；`待裁`＝需用户或编排者定方向。

### H1｜08.5 微信真实往返（**B**，资源门）
- 现状：`TestRealWechat` 全树不存在；`08.5` planned；`08.2` 的守卫 `TestGuard_TestsNeverConstructRealChannel` 钉死测试永不构造真实通道。
- 本质：**真实外部副作用**（发送）不可用 mock 替代；R24 三态已规定"未授权即 BLOCKED"。
- 建议：保持 planned，待"发送授权＋真实接收者"到位；在此之前 09.6 不得判"宿主闭合"。

### H2｜09.5 资源总门＋S10（**B**，资源门）
- 现状：`09.5` planned；8 个子项（存储档／真实模型／MCP／A2A／embedding／collector／SFT／微信）用例均在，资源启用变量未设；S10（真实 SFT 批次）归此项。
- 本质：**真实资源是外部依赖**，不是实现缺口；但"正式必验项不得以 Skip 结项"意味着 09 域在资源到位前**不能宣称全绿**。
- 建议：按 R24 逐项记录"门存在／凭据缺失"，不以 mock 结项；S10 与 08.5 分别挂账，不合并。

### H3｜Rollback 公共出口（**B＋待裁**，产品面）
- 现状：域内**已有 4 个出口**——`state.Publisher.Rollback`（`state/publish.go:272`）、`state.Router.Rollback`（`state/channels.go:254`）、`state.RollbackArchive`（`state/migrate.go:268`）、`evolution.Refine.Rollback`（`evolution/refine.go:34`／`refine_git.go:206`）；**根公共面 0 个**（`Run` 实测 10 个导出方法，无 `Rollback`）。09.4 的热更"回退"是**前滚＋再发布旧内容**（generation 递增），不是回滚到旧代。
- 本质：**能力在域内、可达性在根面缺失**。这不是"没实现"，而是"**没有消费者能触达**"——按 09.6 的"消费者可触达"口径，闭合不成立。
- 待裁选项：
  - **(a)** `Run.Publish` 增 `Rollback` 分支（最小面，复用 `Publisher.Rollback`）；
  - **(b)** 新增 `Run.Rollback(ctx, target)` 显式方法（语义最清晰，但要定义"回滚到哪一代／哪些任务跟着回滚"）；
  - **(c)** 明确"回滚仅是域内 API，根面不暴露"，并在 09.6 把该面判为"不闭合但已裁"（**不建议**：09.4 的 BLOCKED 会永久悬空）。
- 建议：先裁 **(b)** 的语义（回滚目标代、在飞请求、任务义务三件事如何处置），再决定实现面；**不要**用"再发布旧内容"冒充回滚（当前 soak 子测已如实标注，不能升级为"回滚已验"）。

### H4｜golden 4 处 drift 的裁决方向（**待裁**，验收合同）
- 现状：runner `EXIT=3`、checker `EXIT=1`、4 个观测键不一致；09.1 evidence 明写"归 09.7 终态回溯 review 面裁决"。
- 本质：**契约可错**（§3.2 R4）。4 处均非"产品缺特性"，但处置方式不同：
  - S03 roles：**改契约形态**（相对结构／按 call 切片），不抄快照；
  - S03 receipts：**改基数表达**（1:N），不只改值；
  - S08：**先裁场景意图**（是否真的验 storage 降级）；若否，契约只断言 advancer 类别＋object／owner＋统计自洽，自由文本移出身份轴；若是，产品需增稳定 `blocker_kind` code；
  - S09：改 `absent`＋补"未验证不得声称"负向断言。
- 建议：**不要**用一次 `git commit` 把 4 处一起改成"当前观测值"；应按上表**分四条裁决**，每条附归因与"为什么不是产品错"。裁决后重跑 `run_runtime_scenarios`＋`check_runtime_evidence`，只有 `EXIT=0` 才可谈 09.1 的 complete 档（且 S10 仍归 09.5）。

### H5｜09.3 勾选口径矛盾（**流程**，必须回写）
- 现状：`evidence/09.3-gate-run.md` §0 写"**本单不可勾选（保持 planned）**"；`changes/09-acceptance/tasks.md:26` 为 `- [x]`；`task-graph.json` 为 `done`；ORCHESTRATION 批次37 记"完成"。
- 本质：**同一事实有四种状态**。即便编排者后来决定"14/16 可接受、2 红入 F2 收尾账"，也必须**回写 evidence §0**（或追加"编排者裁定"段），否则 evidence 与账目互相打脸，R30 的偏差评估失去依据。
- 建议：保留 14/16 的事实，把 §0 改为"14 PASS／2 FAIL；2 FAIL 入 F2 全绿前置；**编排者裁定：以收尾账挂账，勾选但不得宣称门禁全绿**"，并在 tasks.md 行内加同一句。

### H6｜双门面：`Build`／`Runtime` 死桩＋`Deps.Gate` 死端口（**B**，08 域最硬的一条）
- 现状（实测）：
  - `build.go:78 Build` → `return nil, errNotImplemented`；`Build(` 全树**零调用**（含测试）。
  - `tagent.go:52/63/74` 三个 `Runtime` 方法全 `errNotImplemented`；`:93 Bridge` 恒 `ErrBridgeNotAssembled`。
  - 包 doc `tagent.go:3` "its only public entry"、`:32` "Build is the only way to obtain a runtime"——与活的 `Assemble`（`run.go:154`）／`Run` 冲突；`08.1` 的正锚自己也用 `Assemble`（`public_runtime_entry_test.go:297`）。
  - `build.go:29 Deps.Gate evolution.StructuralGate` 只存在于这条死路径上，**零消费**；07.4 的真实消费走 `WithGovernance`。
- 本质：**阶段一骨架从未被阶段二替换**。`08.P0.1` 的"只签名"在骨架期是合规的，但 08.1 落地后**没有回头处理**它，于是"唯一公共门面"在文档上是 `Build`、在代码上是 `Assemble`。
- 建议（三选一，需裁）：
  - **(a)** 实现 `Build` 为 `Assemble` 的薄适配（保留签名，内部装配），并把 doc 改成"`Assemble` 是唯一装配入口，`Build` 是它的兼容别名"；
  - **(b)** 删除 `Build`／`Runtime`／`Deps.Gate`，doc 只留 `Assemble`／`Run`（最干净，但属公开面破坏性变更，需确认无外部消费者）；
  - **(c)** 保留死桩但在 doc 首行加"**phase 1 skeleton, not the assembly entry; use Assemble**"，并在 09.6 记"公开面双入口"为未裁项。
- **不建议**保持现状（doc 与代码矛盾＝新宿主作者的第一个坑）。

### H7｜`HostBridge` 三重脱节（**B**，08.P0.2）
- 现状：接口存在（`host_bridge.go:29`）、契约范例存在（`examples/contract/root/HostBridge_example.go:52`）、入口存在（`Runtime.Bridge`），但**恒拒**；微信宿主用自有 `hostSendSeam`，不经 `HostBridge`。
- 本质：**声明面／实现面／消费面三缺二**。它不会让测试红（因为没人调用），但会让"宿主桥"这个概念在文档上存在、在运行时不存在。
- 建议：与 H6 合并裁决——若保留接口，则至少让 `Assemble` 产出一个真实 bridge 并在 08.2 的宿主里试消费一次；若不做，删除接口与范例，避免"死契约"。

### H8｜07.1／07.2／07.3 生产消费者为 0（**B／待裁**，与第五查冲突）
- 现状（实测）：`NewReflection`／`NewGitRefine`／`NewPosterior` 均**无生产调用点**；07.1 只剩 `NewAnchorStore` 供水位给诊断；07.2／07.3 只有测试与契约范例。
- 已裁部分：批次34"反思面收缩=可接受（生产面无 trigger_source 戳，不造测试后门）"——只覆盖 07.1。
- 未裁部分：07.2（git refine）与 07.3（后验评估）**没有任何生产接线**，而 tasks.md 07.2 的验收是"真实测试（临时仓）"、07.3 是"真实测试"，二者都**没有要求生产消费者**。按中检第五查"无正式公共入口调用路径＋负向反例的完成声明一律无效"，这两叶的完成声明**在口径上站不住**。
- 待裁选项：
  - **(a)** 承认 07.2／07.3 是"**库能力**"（由宿主按需接入），在 tasks.md 与 09.7 里把"生产消费者"降级为"契约范例消费者"，并明确记录这是口径豁免；
  - **(b)** 在微信宿主接一条真实路径（例如把 refine 的 `status` 挂到诊断面、把后验评估挂到发布后回调），补齐生产消费者；
  - **(c)** 重开两叶补"生产消费者锚"。
- 建议：先做 **(a) 的显式裁定**（成本最低、诚实），不要让"已完成"与"第五查"两个口径并存而不解释。

### H9｜08.2 的登记未销账：`main.go:333` 入站胶水零覆盖（**流程**）
- 现状：08.2 evidence 原文"入站拦截胶水 `main.go:301` 集成断言缺口登记 08.4 补"；08.4 evidence **未提及**；实测 `main.go:333 bot.OnMessage(func(...))` 是 `main()` 内联闭包（`gateway.Resolve`→`host.Admit(DedupKey)`→`ClassifyInbound`→`IntakeMedia`→`host.Drive`），**无任何测试引用**（`main_test.go` 只测 `HostSender`）。
- 本质：**登记转移但接收方未认领**。胶水本身很薄，但它串起了审批、去重、分类、媒体入库四个有语义的步骤；任一接错，测试不会红。
- 建议：要么在 08.4（或新叶）补一条"经 `OnMessage` 回调驱动一条入站消息"的集成测试，要么在 08.2／08.4 evidence 里**显式撤销该登记并说明理由**（例如"该胶水由 `assembleHostFrom`＋`Drive` 的分测覆盖，main 内联闭包仅为接线"）。**不能让登记悬空**。

### H10｜产品包内部测试 0（18/18）（**结构／待裁**）
- 现状（实测）：排除 `tests/`、`scripts/`、`examples/contract`、`prototype` 后，**18 个第一方产品包，0 个有包内 `*_test.go`**；所有测试在 `tests/integration/**`。09.3 §5.2 自报"16 个产品包内部 0 个"，与本次 18 的差异是包枚举口径（我计入根包与 `tool/*` 三个）。
- 本质：这是**黑盒验收**的设计选择（经公共入口驱动，符合 D08-1／禁 import 内部），优点是"测试即消费者"，缺点是**未导出不变量无法单测**、包级回归只能靠集成层兜底。
- 待裁：接受该结构（并在 09.6 记为"已决策边界"），或对高风险包（`state`／`agent`／`task`／`evolution`）补少量白盒单测。**不建议**把"688 pass"读成"18 个包都被单测覆盖"。

### H11｜09.21 盲测证据只在 `/tmp`（**流程**，证据易失）
- 现状：`task-graph` 的 `artifact_paths` 指向 `/tmp/blind-a2/*`、`/tmp/blind-b2/*`；本次实测文件仍在（100 行／4 测；340＋158 行／8 测），但 `/tmp` 会被系统清理，**仓内无盲测报告**。
- 本质：**验收证据的持久性不足**。第一轮的"负结果入库"也只在 task-graph 的 notes 里，没有独立报告。
- 建议：把两轮盲测的**派发单／产物摘要／判据核对表**落入 `evidence/09.21-blind-tests.md`（产物本身可不入仓，但"谁在什么约束下写出了什么、怎么判的"必须入仓）。

### H12｜09.6 三硬阻塞与 10 项未裁（**B**，闭合前置）
- 现状（09.6 报告＋本次复跑）：①主规格 71 ADDED 全未入、57 MODIFIED 正文 0 相同（`check_runtime_plan` 实测 `added=71/modified=57/removed=0`）；②0 篇现行架构文档，296 条契约行 100% 指向历史 wiki；③`docs/api` 27 stale（27/27 已删包）＋13 分歧＋33 缺页、产品包 fresh=0；④结构 16 文件>500（本次复现，largest 996）；⑤11 个 M 能力零 delta；⑥14 份 spec 引用 24 个已删名；⑦69→74 口数漂移；⑧golden 4 drift；⑨Rollback 根面缺失；⑩资源门未授权。
- 本质：**收尾账**，不是实现缺口；但"存在未裁项则如实保持未完成"是 09.6 自己的判据。
- 建议：按 09.6 §6 的 10 项逐项裁（其中 ⑧→H4、⑨→H3、⑩→H1/H2 已在本文单列）；主规格归档**另行授权**，不得在未授权时把 delta 当主规格。

### H13｜07.22 验证命令不可复跑（**流程**，同块2 G5 类）
- 现状：tasks.md／task-graph 的 07.22 验证命令 `python3 scripts/check_concept_budget.py --rule struct-fields evolution` → **exit 2**（`unrecognized arguments: evolution`）；脚本无位置参数。
- 本质：**验证锚写的是意图（只看 evolution）而非可执行命令**。实际检查是全仓 `--rule struct-fields`，再人工过滤。
- 建议：把命令改成可执行的 `python3 scripts/check_concept_budget.py --rule struct-fields --json`＋一行过滤说明；或给脚本加 `--scope <dir>`（属工具改动，需授权）。**不要**让不可执行的命令留在 verification 里。

### H14｜概念预算域级未清零（**数据／待裁**）
- 现状：54 findings 全豁免；其中本域相关 4 处——`evolution/posterior.go:Evaluation 6`、`evolution/refine.go:RefineRecord 7`、`plugin/denial.go:DenialRecord 9`、`plugin/goal.go:Goal 6`。
- 本质：07.22 只对**四个点名结构**负责（已达标）；域级"参数面收敛"并未完成。豁免是**具名可溯**的（`baseline_stale=0`），但豁免不等于收敛。
- 建议：在 09.7 的双维账里把"54 项豁免"与"07.22 四结构"**分开记**，不要把"07.22 完成"读成"evolution 参数面完成"；是否继续拆 `Evaluation`／`RefineRecord` 属后续优化，不阻塞当前验收。

### 缺口汇总（不构成 PASS，仅索引）

| # | 缺口 | 类型 | 归属/前置 |
|---|---|---|---|
| H1 | 08.5 微信真实往返 | B | 发送授权＋接收者 |
| H2 | 09.5 资源总门＋S10 | B | 资源凭据（R24） |
| H3 | Rollback 公共出口 | B＋待裁 | 根面设计 |
| H4 | golden 4 处裁决 | 待裁 | 用户/编排者 |
| H5 | 09.3 勾选口径矛盾 | 流程 | 回写 evidence／tasks |
| H6 | 双门面 `Build`/`Runtime`＋`Deps.Gate` | B | 08 域 |
| H7 | `HostBridge` 三重脱节 | B | 08.P0.2／08.2 |
| H8 | 07.1/07.2/07.3 生产消费者 0 | B／待裁 | 07 域口径 |
| H9 | `main.go` 入站胶水登记未销账 | 流程 | 08.2→08.4 |
| H10 | 产品包内部测试 0（18/18） | 结构／待裁 | 09 域验收结构 |
| H11 | 盲测证据只在 `/tmp` | 流程 | 09.21 |
| H12 | 09.6 三硬阻塞＋10 未裁 | B | 收尾授权 |
| H13 | 07.22 验证命令不可复跑 | 流程 | 07.22 |
| H14 | 概念预算域级未清零 | 数据／待裁 | 07.22 边界 |

---

## 附：本次审查的复跑命令清单（全部只读，产物写 `/tmp`）

```sh
# ── 09.1 场景对照（三复跑，均 drift） ─────────────────────────────
python3 scripts/run_runtime_scenarios.py --scope deterministic --out /tmp/review097b   # EXIT=3, drift
python3 scripts/check_runtime_evidence.py --evidence /tmp/review097b/evidence.json \
        --test-log /tmp/review097b/test-log.jsonl --scope deterministic                # EXIT=1, 4 ERROR
python3 scripts/check_runtime_evidence.py --check-contract --json                       # EXIT=0（形状）

# ── 检查器自测 ────────────────────────────────────────────────────
python3 scripts/test_run_runtime_scenarios.py    # Ran 19 tests  OK
python3 scripts/test_check_runtime_evidence.py   # Ran 54 tests  OK
python3 scripts/test_check_runtime_plan.py       # Ran 38 tests  OK

# ── 09.3 门禁复现 ────────────────────────────────────────────────
bash scripts/lint.sh                             # EXIT=1, comment_policy 494 beyond baseline
bash scripts/gen_godoc.sh --check                # EXIT=1, docs/api out of date
python3 scripts/check_runtime_structure.py       # EXIT=1, 16 files > 500, largest 996

# ── 产品 race（两 module） ───────────────────────────────────────
go test -race ./... -short -count=1 -json                                  # EXIT=0, 688 pass/0 fail
go -C examples/wechat-bot test -race ./... -short -count=1 -json           # EXIT=0, 112 pass/0 fail

# ── 09.4 soak（加压） ────────────────────────────────────────────
go test -race ./tests/integration/host -run '^TestSoakContinuity$' -count=1 \
        -args -rounds=30 -events-per-round=30                              # EXIT=0, ok 163.185s

# ── 点名测试抽验（-count=1） ─────────────────────────────────────
go test ./tests/integration/host -run '^(TestPublicRuntimeEntry|TestDiagnosticEndpointRequiresAuthentication|TestRuntimeVisibility)$' -count=1
go test ./tests/integration/evolution -run '^(TestReflectionScopeAndWatermark|TestStructuralEvaluation|TestEvalRejectsWrongContentCompletion|TestPostActivationEvidence|TestGitNativeRefine)$' -count=1
go -C examples/wechat-bot test ./... -run '^(TestSessionRouting|TestFileRoundTrip)$' -count=1
go test ./tests/integration/acceptance -run '^TestOfflineMigration$' -count=1

# ── 07.22／概念预算 ──────────────────────────────────────────────
python3 scripts/check_concept_budget.py --rule struct-fields --json        # 54/54 豁免, stale=0
python3 scripts/check_concept_budget.py --rule struct-fields evolution     # EXIT=2（不可复跑, H13）

# ── 09.6 规划面 ──────────────────────────────────────────────────
python3 scripts/check_runtime_plan.py --change-dir openspec/changes/runtime-rebuild --json
```

**台账（数字 → 来源）**：

| 数字 | 值 | 来源 |
|---|---|---|
| 23 叶 done／planned | 20／3 | `task-graph.json` 逐叶 status |
| 确定性键相等／总数 | 54／58（＋S10 12＝70） | runner＋checker 实测 |
| drift | 3 场景／4 键／6 行 | runner 实测（run-…095401Z） |
| 门禁 | 14 PASS／2 FAIL／16 门 | 09.3 自报＋本次 3 处复现 |
| soak | 4 件＝3 PASS＋1 部分（Rollback BLOCKED） | 09.4 自报＋本次 900 调用复跑 |
| 盲测 | A1 部分／A2 达标；B1 部分／B2 达标 | 09.21 自报＋`/tmp` 实物 |
| 产品包内部测试 | 0／18 | 本次枚举（脚本见 §4.1 说明） |
| 结构体豁免 | 54／54，`baseline_stale=0` | `check_concept_budget --json` |
| 主规格 delta | 71 ADDED／57 MODIFIED／0 REMOVED | `check_runtime_plan --json` 实测 |

**审查边界（如实）**：
- 本次**没有**重跑 23 叶各自的 `-count=10` 原始计数；标「自报」的计数沿用 evidence，标「实测」的是本次 `-count=1` 或加压复跑。
- 本次**没有**执行 09.5／08.5 的资源面（无凭据），相关条目全部按 `BLOCKED`／`planned` 记录，未用 mock 顶替。
- 本次**没有**改动任何产品代码、测试、脚本、`task-graph.json`、`tasks.md`、`ORCHESTRATION.md`；唯一写入是本文件。
- 本块**不给总判**：上列 14 项缺口与 4 处 golden 归因即结论边界；09.7 总报告的"理想工程判据=是"不构成本块的 PASS 汇总。
