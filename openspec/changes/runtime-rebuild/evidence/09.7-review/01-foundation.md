# 09.7-review · 块1｜00-foundation 域终态回溯

> **审查对象**：`runtime-rebuild` 下 `00-foundation` 域全部 **29 叶** ＝ `00.P0.1–00.P0.3`（骨架/评审门）＋ `00.1–00.6`（盘点/合同/golden/检查器/CI）＋ `00.7–00.20`（R 修订批）＋ `00.21–00.26`（概念收敛批）。
> **审查方式**：**只读**（唯一写入＝本文件）。逐叶比对 `task-graph.json`（verification／evidence／notes）、`changes/00-foundation/tasks.md` 行原文、`midterm-review-response.md`、`prototype/config.go`·`prototype/state.go`·`prototype/README.md`，并逐条实测实现落点。
> **数字纪律**：标「**自报**」＝`task-graph.json` 该叶 `evidence` 里的计数（本次未复跑原始命令）；标「**实测**」＝本次会话真实输出，命令随文给出（§0 可复跑）。凡两者冲突，均在文中并列，不择其一。
> **三轴**：`A`＝原型/规划要求且已实现；`B`＝原型/规划要求当前未实现或未闭合；`C`＝新增演化（原型与规划都没有的新面）。
> **未做的事（声明边界）**：① 未跑全量 `go test ./...`（只跑了 11 个点名锚，见 §0-9）；② 未跑 `go test -race`；③ 未复跑各叶 `evidence.commands` 的逐叶原始命令（多数不可复跑，见 §1 表注 5）；④ 未对 `docs/wiki/*` 做逐篇语义校对。因此本报告的判据是**落点结构＋锚存在性＋证据自洽性＋点名锚实测**，不等于"每叶测试都被重跑过"。
> **红线执行**：本文**不给 PASS 汇总**。逐叶判据与缺口并列；残留、陈旧、不可复跑、口径互斥项一律进 §5。

---

## §0 本次复跑基线（全部命令从仓库根执行）

| # | 命令 | 实测结果 |
|---|---|---|
| 1 | `GOPROXY=off go build ./...` | exit 0，无输出 |
| 2 | `GOPROXY=off go vet ./...` | exit 0，无输出 |
| 3 | `gofmt -l .`（排除 `prototype/`） | 空（0 文件） |
| 4 | `python3 -B -m unittest discover -s scripts -p 'test_check_runtime_*.py'` | `Ran 142 tests` / `OK` |
| 5 | `python3 scripts/check_runtime_plan.py --json` | `ok=True errors=0 warnings=81`；counts：`graph_tasks=119, graph_edges=453, checkboxes=119, covered_main_capabilities=120, requirements=128, scenarios=447, spec_files=62, domains_with_spec=10` |
| 6 | `python3 scripts/check_runtime_architecture.py --check-policy --json` | `warnings: []`（policy 无违例） |
| 7 | `python3 scripts/check_runtime_evidence.py --check-contract --plan-graph …/task-graph.json --json` | `warnings: []` |
| 8 | `python3 scripts/check_concept_budget.py --json` | `ok=True`；counts：`baseline_entries=68, baseline_exempted=68, baseline_stale=0, exported_interfaces=69, interfaces=74, structs=545, go_files=176, packages=18, findings=68`（`iface-methods=12, struct-fields=54, ctx-bare=2, contract-surface=0`）；`productVerified=null` |
| 9 | `GOPROXY=off go test ./tests/integration/... -count=1 -run 'TestModelTerminalProtocol｜TestStateOwnsPipeline｜TestSessionAuthority｜TestMailboxConsumption｜TestNativeToolAdmission｜TestIdentityAndCommitContract｜TestTaskLifecycle｜TestOriginalReferenceIsolation｜TestSearchQueryContract｜TestPublicRuntimeEntry｜TestFormalAssemblyRequiresGovernance' -v` | **11 个 `--- PASS`，0 `--- FAIL`**；`execution/history/host/state` 四包 `ok` |
| 10 | `python3 scripts/check_runtime_structure.py --json` | **`ok=False`，16 违例**（>500 行产品文件，最大 `query/snapshot.go` 996） |
| 11 | `python3 /tmp/verify.py`（00.22 逐字位移见证脚本，仓外留存） | `blocks: 67 problems: 3`（未命中 3 块＝`Run` struct / `Assemble` / `assemblyConfig`，均为后续 M 波演化面） |
| 12 | `wc -l run.go opts.go read.go run_invoke.go run_tool.go run_fact.go run_request.go run_commit.go` | 325 / 155 / 270 / 179 / 234 / 255 / 298 / 165（最大 325 ≤400） |
| 13 | `openspec validate runtime-rebuild --strict --no-interactive` | `Change 'runtime-rebuild' is valid` |
| 14 | `GOPROXY=off go vet ./examples/...` | exit 0 |
| 15 | `git status --short \| wc -l` | **255**（终态树全部改动未提交） |
| 16 | `grep -rn "// 契约:" --include='*.go' . \| grep -v _test.go \| wc -l` / 其中指向 `docs/wiki` 的条数 | **297 / 297** |
| 17 | `grep -rn -F "Build(" --include='*.go' .`（排除定义与注释） | **0 调用点** |
| 18 | `grep -rn -F "config.Load(" --include='*.go' .` | **2 处，均在 `tests/integration/config/config_contract_test.go:86,221`** |
| 19 | `grep -rl "The redline list" --include='*.go' .` / `grep -rn "MUST \|MUST NOT " …` | **48 文件 / 280 行** |
| 20 | `find examples/contract -name '*.go' \| wc -l` | **90 文件** |
| 21 | `grep -rn "ctx.Value(" --include='*.go' . \| grep -v _test.go` | **恰 2 处**：`plugin/policy.go:77`、`task/wrapper.go:54`（均在 accessor 实现体内，与 S3 基线一致） |
| 22 | `ls query/*_test.go` | 无（query 包内零测试；行为锚在 `tests/integration/inspection/` 6 个文件） |

---

## §1 逐孙任务表（29 行）

> 「落点」列 `file:line` 均为**实测**（`grep -n`／`sed -n` 定位）；「锚证据」列区分：**实测**＝本次复跑命中；**自报**＝`task-graph.json` 该叶 evidence 原文（含其 run_id），凡其命令不可复跑在 §1 表注 5 具名。

| 叶 | 要求（tasks.md 原文要点） | 落点（file:line，实测） | 轴 | 锚证据 |
|---|---|---|---|---|
| **00.P0.1** | 冻结骨架约定（stub 形态/编译断言/英文 Godoc/包注释/≤500 行/同文件内聚）入 `design.md` D9，并在 `ORCHESTRATION.md` 登记骨架评审门 | `openspec/changes/runtime-rebuild/design.md:111-126`（D9 两阶段与骨架约定）；`ORCHESTRATION.md:7,9,37,245`（门与解锁记录） | A（规划工装，非产品面） | 自报 `rev2phase-20261013`：unittest／`check_runtime_plan --json`／`openspec validate --strict` 三条 exit 0。**实测**：`openspec validate` = valid；`check_runtime_plan` ok/0 errors（§0-5,13） |
| **00.P0.2** | 扩展规划检查器：`NN.P0.M` 编号、skeleton 相位、阶段门**硬边**＋P0 负控 | `scripts/runtime_checklib.py`、`scripts/check_runtime_plan.py`、`scripts/test_check_runtime_plan.py` | A | 自报 `rev2phase-20261013`（同三条命令）。**实测**：142 tests OK（含 plan 自测，§0-4）；plan 对 119 task／453 edge／119 checkbox 全部一致（§0-5） |
| **00.P0.3** | 骨架评审门：全部域 P0 done 后**三道**（CodeReview 子代理＋主编排者复核＋用户确认），产出记录才解锁阶段二 | `openspec/changes/runtime-rebuild/skeleton-review.md`（54 行）；`ORCHESTRATION.md:9`（"00.P0.3 门已过（三道齐备）"） | A（流程门） | 自报 `gate-00P03-20261013`：三道记录 exit 0。**实测**：`skeleton-review.md` 存在且 54 行；ORCHESTRATION 明记解锁；`check_runtime_plan` 对相位/门的硬校验 0 errors（§0-5） |
| **00.1** | 盘点残件／用户保护面／两 module／旧检查器，逐文件登记"保留参考/原位重写/另需授权清理" | `openspec/changes/runtime-rebuild/baseline.md`（185 行） | **A＋B**（盘点成立；但"保留参考"的骨架文件 `tagent.go`/`build.go` 终态仍是 phase-1 stub，见 §5-G1） | 自报 `apply-00.1-20261013`：`git status --short`/`git ls-files`/`ls model state tagent tests` exit 0。**实测**：`git status --short` = 255 项未提交（§0-15）——终态无 git 基线可回溯，盘点结论只能对工作树成立 |
| **00.2** | 120 主规格按业务能力映射到新任务/消费者/验收，重复与过时条款**具名** | `coverage.md`（981 行） | A | 自报 `apply-00.2-20261013`：601 条逐条归属＋103 空洞三类裁决清零＋9 张力处置。**实测**：`covered_main_capabilities=120`、`requirements=128`、`scenarios=447`、`spec_files=62`（§0-5）；601 与 128/447 口径不同（主规格条目 vs delta 增量），文中未见单一换算式 → §5-G7 |
| **00.3** | 审定 G1 共享合同＋tRPC 复用矩阵、G2 五链；列生产消费者与最小签名探针；**没有探针不宣称 G1 通过** | `contracts.md`（118 行）、`design.md`（323 行） | **A＋B（未闭合）**：证据自述"G1 通过仍待全部域 P0 done，不宣称"；终态**无人宣称**，G1 状态停留 | 自报 `apply-00.3-20261013`：`openspec validate --strict` exit 0；D10 五链推演＋15 契约回填＋2 待核 M1/M2。**实测**：`openspec validate` valid（§0-13）；`contracts.md` 存在 |
| **00.4** | 建架构/场景/观测 golden，用**值/消息/身份关系**断言 | `golden/runtime/architecture.json`（12.8KB）、`golden/runtime/scenarios.json`（12.9KB） | A | 自报 `phase2-00.4`：`--check-policy` ok、`--check-contract` ok、10 scenarios、S03 值级断言实证。**实测**：scenarios 恰 10 条（S01–S10，`python3 -c` 解析）；两个 checker `warnings: []`（§0-6,7）；**但** 09.7 回溯已记 4 处 golden drift 未裁决（§5-G8） |
| **00.5** | 五件检查器（递归包覆盖/错误退出码/重复 JSON 键/Skip 空跑/错值/缺状态事件）＋负控子进程非零 | `scripts/runtime_checklib.py`、`check_runtime_plan.py`、`check_runtime_architecture.py`、`check_runtime_structure.py`、`check_runtime_evidence.py`、`test_check_runtime_{plan,architecture,structure,evidence}.py` | **A＋B**（五件齐；负控齐；**但** structure 门终态 `ok=False`／16 违例且**未入 CI**，`config/config.go` 530 行在列） | 自报 `phase2-00.5`：unittest OK（负控 exit 1 由测试内断言）。**实测**：142 tests OK（§0-4）；`check_runtime_structure --json` → `ok=False / 16`（§0-10）；`grep -n "structure" .github/workflows/*.yml` 只命中散文注释，**无 structure 步骤** |
| **00.6** | 迁移 CI/lint/race/API 门，覆盖 next 与**双 module**，Go≥1.24.1，不得提 baseline 或机械拆包换绿 | `.github/workflows/runtime-contracts.yml`（202 行，含 plan/architecture/concept/scenarios/build/vet/test/gofmt/comment/tracked-hygiene/strict，`go-version: ">=1.24.1"`）、`scripts/test_check_runtime_workflow.py` | **A＋B**（新资产成；"不提高 baseline"在 00.23/00.24 以"删行出清"替代降低（见 §5-G7）；旧 `ci.yml` 的 docs/api、lint、race 迁移面由 09.3 G09/G10 承接，本域不重复记账） | 自报 `phase2-00.6`：workflow 15 tests OK 含双负控；tracked-hygiene=0。**实测**：workflow 内 concept 步骤在 `:91`（自测）、`:109`（`--json`）；142 tests 含 workflow 自测（§0-4） |
| **00.7** | 终局归约边界声明：model 合同面 Godoc 定稿＋`persistent-event-loop` delta 措辞修正 | `model/terminal.go:1-32`（包 doc：唯一入口 `ReduceChannel`/`ReduceSeq`，消费方 MUST NOT 另立终局）；`changes/02-execution/specs/persistent-event-loop/spec.md:4`（delta 头段措辞） | A | 自报 `r1-00.7`：TestModelTerminalProtocol 11 子项 PASS **测试零改动**；persistent-event-loop 措辞核验 ReAct/框架既有零命中。**实测**：该锚 PASS（§0-9）；`grep -n "persistent" model/terminal.go` 零命中（"delta" 面确在 02 delta，文件内只留边界声明） |
| **00.8** | 派生代借用：`BindingRef`/`BorrowDerived`/排队保有协议 | `state/binding.go:189 (BindingRef)`、`:210 (Borrow interface)`、`:233 (BindingHandle)`、`:410 (BorrowDerived)`、`:468 (Release)`；`state/retire.go:84 (Publisher.Borrow)`、`:99 (BorrowDerived)`、`:113 (drainable)` | A | 自报 `r1-00.8`：3 测试 PASS 零改动；grep `BindingRef×15/BorrowDerived×5`；协议 Godoc 三条；**前次空返半成品（ErrBindingRefUnknown sentinel）经重派闭合**。**实测**：三锚 PASS（§0-9）；两文件行号如上 |
| **00.9** | 审批身份：`ApprovalID`+`ExpectedDigest`+作用域+等待状态+幂等 | `event/types.go:136-139 (ApprovalID)`；`host_bridge.go:151-158 (ApprovalID/ExpectedDigest)`；`task/spec.go:78-83 (StatusAwaitingApproval)`；**实落** `task/hold.go:186-203`（校验落点）、`task/wrapper.go:251 (DigestArgs)` | **A＋C**；**落点偏离**：tasks.md W 列写 `plugin/governance.go`，终态该文件零 `Approv` 命中 → 审批身份归 `task` 域（见 §3-⑤ 案例 B） | 自报 `r1-00.9`：TestNativeToolAdmission 7 PASS（含 approval resolves／mismatched refused）；grep `ApprovalID event=2/host_bridge=4`。**实测**：该锚 PASS（§0-9）；`grep ExpectedDigest` 生产侧命中 `task/hold.go:203`、`task/wrapper.go:251`（§0 之外实测） |
| **00.10** | 通知信封 `NoticeEnvelope` 冻结目标＋**`CommitRecord` 自包含扩展** | `task/commit.go:26 (NoticeEnvelope)`、`:44 (CommitRecord{MutationID,TaskID,ExpectedRevision,Status,Outbox})`；"自包含"被**分布**到 `task/restore.go (TaskRecord.Spec)`、`task/registry.go:29 (Notice.Event)`、`task/handoff.go (HandoffRecord)` | **A＋B（缩水）**：标题承诺"CommitRecord 自包含扩展"，终态 `CommitRecord` 5 字段**不含** Spec/终局事实/Handoff；事实自包含靠记录**组合**而非单记录 | 自报 `r1-00.10`：两测试 15 PASS（含 "freezes the notification target"）；grep `NoticeEnvelope=4`。**实测**：两锚 PASS（§0-9）；`CommitRecord` 字段集如上（`sed` 实测）；`TaskRecord{Spec,Fact}`、`Notice{…,Event}`、`HandoffRecord` 三处承接 |
| **00.11** | 工具前变换：`TransformCall` 窄接口与 Enricher 分界 | `plugin/phases.go:24-40`（四相位→四接口映射注释）、`:95`/`:101 (TransformCall)` | A | 自报 `r1-00.11`：TestNativeToolAdmission 7 PASS **零改动**；comment_policy 0 findings；grep `TransformCall plugin/phases.go=8`。**实测**：该锚 PASS（§0-9）；相位映射注释实测在 `:24-40` |
| **00.12** | 原文读作用域（`ReadScope`/`ScopedReader`，零域具名拒绝） | `memory/reference.go:80`/`:102 (ReadScope/ScopedReader)`、`:121 (ErrScopeRequired)` | A | 自报 `r2-00.12`：`a_read_runs_inside_the_scope_it_is_bound_to` PASS（裸 key 聚合保留/外域 Missing 不泄露/零域 ErrScopeRequired）；grep 30/4；**冻结文件 `original_store.go`/`localfile.go` 零字节改动（git 可核）**。**实测**：TestOriginalReferenceIsolation PASS（§0-9） |
| **00.13** | 保留保护租约 `HolderID`/`lease`（重试同 token／迟到旧 lease 不误释／重复释放 no-op） | `memory/guard.go:30 (Hold)`、`:39 (Release)`、`:51 (HolderID)`、`:59 (ProtectionLease)`、`:64 (ErrLeaseMismatch)`；`memory/retention.go:86 Register`／`:114 Retire`／`:138 ReleaseHolder` | A | 自报 `r2-00.13`：`protection_holds_outlive_one_holder` PASS；grep 7/3。**实测**：该锚 PASS（§0-9）；`retention.go` 三函数行号如上 |
| **00.14** | 检索内容 `Text`/`Strategy`/`Cursor`（空 Text 具名拒绝；页上限） | `memory/search/port.go:28 (ErrQueryTextMissing)`、`:37 (DefaultLimit=20)`、`:47 (Cursor)`、`:70 (Query)`、`:122 (Validate)`、`:132 (PageLimit)`、`:179 (Result)` | **A＋C**：`DefaultLimit=20` 自报"无据注明"→ 常量无规划依据（具名遗留，非缺陷但不可溯源） | 自报 `r2-00.14`：TestSearchQueryContract 3 子项 PASS；grep `Text=11／ErrQueryTextMissing+DefaultLimit=9`。**实测**：该锚 PASS（§0-9）；`DefaultLimit` 确认 = 20 |
| **00.15** | 字节有界分段 `SegmentRequest`/`Result`（零上限=64KiB 非无限；越界 sentinel） | `memory/segment_store.go:387 (SegmentRequest)`、`:402 (SegmentResult)`、`:414 (DefaultSegmentMaxBytes = 64<<10)`、`:430 (segmentBody)`；`workspace/externalize.go`（D12 同型补单，自报零残留） | A | 自报 `r2-00.15`：`byte_bounded_segments_stay_bounded_and_resume_exactly` PASS；grep `MaxBytes|Next|EOF=21`。**实测**：该锚 PASS（§0-9）；常量与类型行号如上 |
| **00.16** | mailbox 事实关联＋**连续前缀**消费 | `state/session.go:13 (Session)`、`:22-30 (Consumed, ClaimConsumed)`、`:61-68 (MailboxEntry 三时点)`；`state/mailbox.go:196-225 (Consumed 连续前缀)` | A | 自报 `r2-00.16`：16 PASS（SessionAuthority 6＋MailboxConsumption 5 含新增回溯/前缀＋StateOwnsPipeline 3）；grep `EventID|InvocationID=4`；Consumed 行为修正=R2.5 语义级联。**实测**：两锚 PASS（§0-9） |
| **00.17** | Query 载体：分页/水位/`Update`/逐义务推进者/`InstanceID` | `query/types.go:94 (ObjectRef)`、`:133 (Cursor)`、`:194 (SourcePosition)`、`:201-215 (水位三态)`；`query/subscribe.go:46-74 (UpdateOp/Update)`；`query/explain.go:78-131 (Advancer 四值与两处字段)`；`query/snapshot.go:162 (SessionState)`、`:405 (Projection.Snapshot)` | **A＋B（部分）**：本叶自报"query 包无测试（合同面修订，运行行为后续取证)"；终态 `ls query/*_test.go` 仍**零**，行为锚改由下游 `tests/integration/inspection/`（6 文件）间接覆盖——合同面已落，包内自证缺 | 自报 `r2-00.17`：build/vet/gofmt 0；grep `Cursor=13/WatermarkLagUnknown=3/NextAdvancer 零残留`。**实测**：`query/*_test.go` 无匹配（§0-22）；`tests/integration/inspection` 6 测试文件存在；四处落点行号如上 |
| **00.18** | RL 证据合同：快照引用/逐文件封账/reward **实例版本** | `rl/record.go:150-162 (SnapshotRef)`、`:223 (Snapshot 字段)`；`rl/reward.go:35-45 (RewardRecord/InstanceID/Revision)`；`rl/capture.go`（封账） | A | 自报 `r3-00.18`：build/vet/gofmt 0；四项合同面（`SnapshotRef+VersionedName`／`CaptureFileDigest+WriterSyncResult`／`InstanceID+Revision`／`RewardInstanceID`）全部实存；grep 7；`VisibleTools` 类型修订追认。**实测**：`SnapshotRef` 在 `record.go:162`、`RewardRecord` 在 `reward.go:35`、`InstanceID/Revision` 在 `:41/:45` |
| **00.19** | 自演进读面：`GateReport`／后验四态／`RefineRecord` 接回 | `evolution/gate.go:64 (GateVerdict)`、`:86 (EvidenceRef)`、`:101 (GateReport)`；`evolution/refine.go:58 (RefineRecord)`、`:134-153 (PosteriorVerdict 四态)`、`:170 (PosteriorReport)` | A | 自报 `r3-00.19`：`GateReport=7／PosteriorHealthy=2`；后验四态独立类型＋GateReport 四要素＋RefineRecord 接回（SHA/激活窗口/后验/GateBasis）实存；**命名自定（PosteriorVerdict/PosteriorReport）追认**。**实测**：四态 `healthy/degraded/insufficient/not-due` 在 `refine.go:139-153`；`PosteriorReport` 在 `:170`（不在 `posterior.go`，与叶名暗示的位置不同） |
| **00.20** | 根入口消费面：`Bridge()`／待投递读面／**模型工厂候选** | `tagent.go:35 (Runtime)`、`:82 (ErrBridgeNotAssembled)`、`:93 (Bridge→恒返 ErrBridgeNotAssembled)`；`build.go:22 (Deps 8 字段)`、`:78 (Build→返回 nil, errNotImplemented)`；`host_bridge.go:151-158 (ApprovalDecision)`、`DeliveryID`；**旁证**：`cli.go:88 (NewCLI，调用点仅 6 个测试文件)` | **A＋B（未闭合）**：`Bridge()` 恒失败；`Build` **零调用点**（§0-17）；`HostBridge` 唯一实现是 `examples/contract/root/HostBridge_example.go:52` 的**范例**；`ModelFactory` 自述"签名未定，phase two 决定"，至今缺席 | 自报 `r3-00.20`：10 PASS 全回归；grep `ErrBridgeNotAssembled=4／DeliveryID=9`；**ModelFactory 候选声明零实现零接线（自报已标注 G1 编译门≠原生对齐）**。**实测**：`grep -F "Build("` 0 调用；`HostBridge` 实现仅范例；`Bridge` 返 sentinel（§0-17）；`NewCLI` 6 个调用点全在测试 |
| **00.21** | 概念预算检查器（S1–S4 机器门）＋CI 步骤 | `scripts/check_concept_budget.py`（968 行，BASELINE 起 `:81`）、`scripts/test_check_concept_budget.py`；`.github/workflows/runtime-contracts.yml:91`（自测）、`:109`（`--json` 门） | **A＋C**（C＝"扩展边际成本"的机器化，原型无）；**B（口径）**：数字对账缺失，见 §5-G7 | 自报 `wS0-00.21`：29 例全绿（含 8 条负向锚：超标件被点名／空仓库不冒充绿）；selftest 4 规则＋棘轮三路径；`baseline 164 项含本波新产债 9 项`。**实测**：counts `baseline_entries=68／interface findings=12／struct=54／ctx=2／contract-surface=0`（§0-8）；**注释写 "S2 struct-fields — 53 项"（`check_concept_budget.py:95`），实测 54** → 自相矛盾 |
| **00.22** | `run.go` 拆文件族（≤400 行/文件），逐字位移 | `run.go:97 (Run)`、`:154 (Assemble)`、`:257 (Publish)`、`:287 (restoreChains)`、`:312 (Recovery)`、`:319 (newInstanceID)`；`opts.go:20 (publishContract)`、`:61-143`（10 Option）；`read.go:26/37/50/79/140/158/191/226/251/265`；`run_invoke.go:33/96/124/135/167`；`run_tool.go:26/46/98/122/128/146/153/172/178/203/232`；`run_fact.go:26/65/84/110/122/132/139/158/182/197/207/224/229/243`；`run_request.go:41/65/82/94/137/211/234/255`；`run_commit.go:27/59/74/85/109/131/148/153/158/163` | A | 自报 `wS1-00.22`：1740→311＋7 文件（opts 143/read 270/run_invoke 180/run_tool 235/run_fact 256/run_request 299/run_commit 166），67 块 problems=0，四回归锚 `-count=10` 全绿。**实测**：8 文件 325/155/270/179/234/255/298/165（§0-12）——**最大 325 非 311**（后续 M 波演化）；`/tmp/verify.py` 复跑 **67 blocks / 3 problems**，未命中 3 块＝`Run`/`Assemble`/`assemblyConfig`（§0-11）→ 64/67 逐字，差异块与 M 波改动一一对应；**脚本在 `/tmp`（仓外）→ 证据本身不可从仓复跑** |
| **00.23** | 红线清单化（MUST/MUST NOT 覆盖全窄口/端口） | 48 个生产文件含 "The redline list"；实测 280 行 `MUST `/`MUST NOT ` | A | 自报 `wS3-00.23`：**42 口/143 条**红线落盘（10 包 doc-only）；S4 红线维度 58-9；29 文件 comment-strip code-identical。**实测**：**48 文件/280 行**（§0-19）→ 数字漂移（后续域继续补红线）；D1 锚串"redline-doc 规则不存在"已自报修正 |
| **00.24** | 范例单点全仓（每导出接口一 canonical 范例） | `examples/contract/**`（实测 90 个 .go 文件）；`examples/contract/query/Assembly_example.go`（全链可跑＋`SessionState{1,1,1,1}` 断言）、`Detail_blocked_example.go`（00.26 W2 挂账项，**现已存在**） | A | 自报 `wS3-00.24`：58 新例（15 包 69 口＝58+04.22 的 11），0 孤儿 0 缺失；S4 出清 `gate=0/stale=0/warn=0`；9 口红线补差；`examples vet 0`＋comment_policy 0。**实测**：`exported_interfaces=69`、`contract-surface=0`（§0-8）；`go vet ./examples/...` exit 0（§0-14）；范例 **90 文件**（含后续域增量）→ 自报数对应"本叶时点" |
| **00.25** | `event.Event` 文档分组＋`ParsedMeta` 瘦身 | `event/types.go:78 (Event)`（四块 doc：identity/content/ownership/ledger，16 字段不变）；`event/meta.go:44-47 (ParsedMeta)`、`:74 (ParseEventMeta)` | **A＋C（口径勘误）**：叶名"瘦身"→ 实为"数据面豁免不瘦身、只做文档分组"（自报"口径勘误(波28)"）；分组形态并入块首字段 doc 首句（自报"偏离申报"） | 自报 `wS2-00.25`：comment-strip 等值 exit 0／非注释改动 0／字段数 16 不变；`ParsedMeta/Event` 数据面豁免**具名登记**（引入叶 00.25＋保真理由）；锚3 10/0。**实测**：baseline 第 99 行同型登记可见（`config/config.go:AgentSpec` 8 字段），ParsedMeta 豁免在 baseline 中具名 |
| **00.26** | 接口自足性补差（盲测七缺口：类型底座/装配图/负向范例/error 后果/Phase 映射/语义陷阱明文化） | `query/types.go:78-95`（ObjectRef 身份表 8 条 bullet）、`:194`（SourcePosition）；`query/explain.go`（Advancer 四值）；`query/snapshot.go:162`（SessionState 四读数来源）；`plugin/phases.go:24-40`（Phase→接口映射＋RiskPolicy 双面）；`plugin/governance.go:28-36`（空 `RiskLevel` 读 `RiskMedium`）；`examples/contract/query/Assembly_example.go`、`Detail_blocked_example.go`（负例） | A | 自报 `wS3-00.26`：七缺口 7/7；负向范例×3（`ErrCallIdentityChanged`/`ErrDetailOutsideScope`/`ErrDetailAbsent`）；Phase 映射＋"one ruling 不重复出分"；S4 前后零影响 69/0/0。**实测**：ObjectRef 身份表在 `:78-95`（8 条 bullet 逐条命中）；`RiskLevel` 零值语义注释在 `governance.go:28-36`；`Detail_blocked_example.go` 存在 |

### 表注（逐条可复验）

1. **29 叶 status 全 `done`**（`task-graph.json`），`tasks.md` 29 行全 `- [x]`，`check_runtime_plan` 对 checkbox↔status 一致性 0 errors（§0-5）→ 勾选真实性与图一致。**但这只证"勾了且与图一致"，不证"实现了"**——后者由本表落点列与锚列承载。
2. **轴的分布（合计 29）**：纯 `A`＝**18** 叶；`A＋B`＝**7** 叶（00.1 陈旧残留／00.3 G1 未宣称／00.5 structure 门红／00.6 迁移面／00.10 CommitRecord 缩水／00.17 包内零测试／00.20 未闭合）；`A＋C`＝**4** 叶（00.9／00.14／00.21／00.25）。纯 `C` 叶＝0（本域为地基域，C 面以"深化既有"形式出现，不独立成叶）。
3. **完成类型分三种，不可混计**：① **证据型**（00.7/00.11/00.12/00.13/00.14/00.15/00.18/00.19：自报"测试零改动"或"合同面修订"，改动是 Godoc/签名）；② **补差型**（00.8/00.9/00.10/00.16/00.17/00.26：新增字段/协议/范例并级联）；③ **工装型**（00.4/00.5/00.6/00.21/00.22/00.23/00.24/00.25：检查器、拆文件、红线、范例、文档分组）。三者验收强度不同：①的"零改动 PASS"最弱（可能只是既有实现已满足），③的"code-identical"最强（可机器复核）。
4. **本域两处"自报与终态数值不一致"**：00.22 最大文件 311→325（仍 ≤400，不违约束）；00.23 42 口/143 条→48 文件/280 行；00.24 58 例→90 文件；00.21 baseline 164→68。前三处可由"后续域继续演化"解释（属正常增量），第四处**无法用任何现存文档对账**（见 §5-G7）。
5. **证据可复跑性分档**：**可复跑**＝00.4/00.5/00.6/00.21/00.22/00.23/00.24/00.25/00.26（命令含 checker 名或 `unittest discover`，本次已复跑对应面）；**部分可复跑**＝00.7–00.20（命令形如 `go test TestX -count=1`，**缺包路径**，需自行补 `./tests/integration/<域>`，且多数无 `-json`/计数断言）；**不可复跑**＝00.P0.1/00.P0.2/00.P0.3/00.1/00.2/00.3（P0 三条命令本次已代为复跑并给结果；00.1 的 `ls …` 与 00.P0.3 的"三道评审"无机器判据）；**仓外证据**＝00.22 的 `/tmp/verify.py`（本次能复跑是因为 /tmp 残留仍在，不是仓库性质）。

---

## §2 原型对照：`prototype/config.go`＋`prototype/state.go` vs 本域实现

> 原型侧引用均取自文件头"职责/未实现"声明与函数体（`prototype/README.md:5-28` 另有 13 条行为声明）。

### 2.1 配置面（`config.go`：职责＝"配置描述＋装配"）

| # | 原型特性声明 | 原型位置 | 本域落点（实测） | 判定 |
|---|---|---|---|---|
| P1 | **只做配置描述＋装配**；`LoadConfig` 只保证合法 JSON，引用校验另置 | `prototype/config.go:1-2, 20-30` | 描述＝`config/config.go:27 Declaration`／`:44 AgentSpec`／`:79 FileRef`；`config/source.go`（严格解码＋行号/字段路径）；装配＝`run.go:154 Assemble`＋`prepare.go:58 prepare` | **同构（切分）**：描述与装配分属两包，职责更清；但"引用校验"这一职责在切分中**掉地**（见 P6） |
| P2 | **严格解析**（原型只到"合法 JSON"） | `prototype/config.go:19` | `config/source.go`（YAML/JSON 严格解码；错误含 source/line/field-path/reason）；`config/config.go:104 Load` | **深化**（原型未实现"严格"）；**但** `config.Load` 生产调用点＝0（仅 2 处测试，§0-18）→ 该深化在生产面无消费者（§5-G3） |
| P3 | **`ReloadConfig` 一体化热加载**：解析→校验→`buildSystem` 固化 prompt→`state.Apply` | `prototype/config.go:35-45` | 拆为 `Assemble`（run.go:154，首代）＋`Run.Publish`（run.go:257，后续代）＋`state.Publisher.Publish`；**指纹幂等**：同指纹 → `PublishUnchanged`（run.go:274） | **切分＋深化，缺一体化入口**：原型一个调用完成的热加载路径，终态要求宿主自持 "Load→Assemble/Publish" 编排（真实宿主 `examples/wechat-bot/host_runtime.go:116-126` 正是自建 Declaration）→ **缺失（组合入口）** |
| P4 | **`NewAgent` 壳**：绑定 `ResolveTool`／`ModelCompletion`（请求期组装全字段）／声明插件 | `prototype/config.go:50-96` | `prepare.go:91 buildBinding`（model→tools→registerNativeTools→prompt→plugins 五步，逐步命名失败）；`assembleConfig`（opts.go，10 Option）；`agent/loop.go` 请求期组装 | **同构（壳→binding）＋深化**（原型的函数字段壳换成代际绑定+资源台账 `state.Leases`） |
| P5 | **工具允许集**：`declaresTool` 按 definition 过滤 `ResolveTool` | `prototype/config.go:80-94, 128-135` | **无等价物**：`AgentSpec.Tools` 的唯一生产消费者是 `evolution/criteria.go:69`（只读 `evaluation` 引用作评测夹具）；运行时工具集＝`NativeAgent.Tools`（host 提供）经 `run_tool.go:98 wrap` 统一准入 | **缺失（语义迁移未申报）**：原型"声明面允许集"在终态无运行时裁决点。是"有意把裁决交给 host"还是"漏接"，无文档可判 → §5-G2 |
| P6 | **`validateRegistry`**：model/skill/plugin/tool/`agent:<id>` 引用必须存在且非 nil | `prototype/config.go:139-174` | `prepare.go:68`（无 supplier → 命名拒"no supplier constructs the candidate binding of agent %q"）＋ `buildBinding` 逐步命名失败（model/tools/filesource/plugins）；**无"声明引用 vs 提供者"列表比对** | **部分/降级**：从"解析期拒绝"降为"构造期失败"。对 model/plugin 有效（构造会失败）；对 `Tools`/`ParentID`/`TaskPolicy` **完全无声**（三者只被解析+克隆+指纹，见 §5-G2） |

### 2.2 现场与架构变更面（`state.go`：职责＝"以 id 注册现场＋整份配置 Apply＋checkpoint"）

| # | 原型特性声明 | 原型位置 | 本域落点（实测） | 判定 |
|---|---|---|---|---|
| P7 | **现场一体化**：`AgentState{queue, context, wake, busy, removed, Tasks, Memory}` | `prototype/state.go:32-45` | **被切分**：`state.Session`（session.go:13，mailbox+消费位+决策投影）、`state.Occupancy`（同 session 互斥）、`state.Bindings`（代+借用+退役）、`run.history`（run.go:97，会话正文）、`agent/loop.go intake` | **深化但一体化丢失**：三块语义各自更严（多代/借用/退役/连续前缀），但"完整现场"不再单点持有——与块2（01/02 域）结论一致，跨块同判 |
| P8 | **Publish 归属校验**：空 OwnerID 代补、异主拒 `ErrForeignOwner`、已移除拒 | `prototype/state.go:70-88` | 归属由 `event.Address` 单射表达；`state/mailbox.go AcceptNotice` + `run_fact.go:84 bookNotice`；拒因是具名 sentinel 族 | **深化**（从"字段比对"升级为"地址即身份"；不可寻址即拒） |
| P9 | **`PublishBatch` 原子批**（单锁入队、一次唤醒） | `prototype/state.go:92-114` | `task/commit.go:26 NoticeEnvelope` 冻结目标＋`task` 通知条数窗；`run_fact.go:65 sink` | **深化**（信封冻结了"目标实例"，原型无此概念；00.10/02.8 承接） |
| P10 | **`Next` 整批取走**，空队列等唤醒 | `prototype/state.go:117-140` | `state/session.go:22-30 Consumed`（最长连续完成前缀，不越空洞）＋`state/mailbox.go:196-225` | **深化**：原型"取走即消费"；终态区分"接纳/认领/消费完成"三时点，空洞不得被前缀吞（01.3/00.16 承接） |
| P11 | **`Append/Context/ReplaceContext`＋Meta 隔离**（插件改不动持久原文） | `prototype/state.go:143-171` | `run.history` transcript＋`rl` 捕获的 Meta 保真（`model.Message` 承载）；`memory` 原文只经 `ReadScope` 读 | **同构（换载体）＋深化**（"隔离"从 clone 升级为只读作用域） |
| P12 | **`Begin/End` 独占消费**（同现场只一壳） | `prototype/state.go:174-192` | `state/occupancy.go:61 Occupy`／`:81 Vacate`；借用代 `state/binding.go:383`；`Run.closing`＋`run_invoke.go:96 Close`（关闭即拒新） | **深化**：从"现场互斥"升级为"同 session 至多一个在飞决策＋关闭拒新＋义务结算（settleOwed）" |
| P13 | **`State.Apply` 三段**：validate→StatePlan→**Eval→Benchmark→ApplyGate（nil 段恒真）**→同 id 换 def+prompt／新 id 建现场／缺 id detach+Close（锁外） | `prototype/state.go:270-310`、`:314-339`、`:351-362`、`:365-376` | `state/publish.go` `Publish`/`publishLocked`；`opts.go:20 publishContract = {Required: eval,benchmark,apply-gate}`；`state.PublishContract`＋`CheckVerdict`；退役 `state/retire.go:48 sweep`＋`binding.go:439/468 release` | **反转式深化（本域最重要一次语义反转）**：原型"nil 段恒真"→ 终态"**必需检查缺失即拒**"（fail-closed）。原型为演示放宽，生产必须收紧；01.5 负锚钉此 |
| P14 | **`validateConfig`**：id 非空唯一、父引用存在、父链不成环 | `prototype/state.go:379-406` | `config` 侧无父链校验（`ParentID` 只解析/指纹，§5-G2）；`state.NewStatePlan` 做 id/绑定校验 | **缺失（父链）**：原型明示的结构校验在终态无等价物；`ParentID` 成为纯声明字段 |
| P15 | **JSON checkpoint**：`Save`/`LoadState`，恢复时 running→unknown | `prototype/state.go:408-462` | `state.DurableCommit`＋`memory.TierFactLog`＋`run.go:287 restoreChains`＋`task.RecoveryMaterial`／`Retained`；`run_commit.go:27 CommitPublication` 自述 "durable ledger … not wired into this minimal entry yet" | **深化（任务/通知域）＋部分（会话正文域）**：任务与通知路由可恢复；**会话正文无持久账**——承 03/08 域，本域旁证 |
| P16 | **未实现边界**：WAL/事务/多代租约/崩溃一致/僵尸判定/函数序列化/队列背压 | `prototype/state.go:6-7` | 代际租约/借用/退役＝已深化；durable FactLog 部分承担 WAL 语义；**背压/崩溃一致仍未声称**（09.7 回溯"已决策接受边界"） | **边界继承＋部分超越**：继承项诚实（未声称即未声称）；超越项（多代租约）有机器锚 |

### 2.3 原型对照小结

- **同构**：描述/装配切分（P1）、壳→binding（P4）、归属校验（P8）、Meta 隔离（P11）。
- **深化**：严格解析（P2）、指纹幂等（P3）、原子批+信封（P9）、三时点消费（P10）、互斥+关闭（P12）、三段裁决 fail-closed（P13）、持久恢复（P15）、多代租约（P16）。
- **缺失**：① 声明面**工具允许集**（P5）；② **引用存在性**校验降级（P6）；③ **父链校验**（P14）；④ `ReloadConfig` **一体化热加载入口**（P3）。
- **判据**：缺的四项**都不是"能力不足"**，而是"责任在切分中掉地"——四处同源：原型把 config/state 当一个组合根，终态把组合根拆到 `Assemble`+`Run`+host，而"声明面裁决"这块职责被分给了 host，却**没有任何文档或测试声明这是有意的**。这是 §3-① 的核心论据。

---

## §3 辩证分析（穿透行为看本质）

### ① "组合根唯一装配"——本质，但它被**符号层**误读成了手段

- **本质是什么**：唯一装配要保证的是**代际唯一性与释放顺序**——一个请求借到的绑定代只能由一个持有者发布、退役、排空；两处装配＝两个代际权威＝`BorrowDerived` 的同代保证与 `sweep` 的排空顺序不可证。这个本质在终态**成立且被机器守护**：`run.go:154 Assemble` 是唯一进入路径（导出**装配**函数仅此一个；`build.go:78 Build` 是死面，见 G1），`Bindings` 只经 `Publisher` 借用（`state/retire.go:84/99` 返回 `drainable`），`Publish` 是唯一发布口（`run.go:257`）。
- **手段是什么**："根包只应有一个导出装配符号"是表达该本质的**手段**。终态的手段失效了：`tagent.go:35 Runtime`＋`build.go:78 Build`＋`tagent.go:93 Bridge` 仍以导出符号存在，全部 `errNotImplemented`/`ErrBridgeNotAssembled`，`Build` **零调用点**（§0-17）。于是根包出现**两条平行的公共门面**（外加只被测试使用的 `NewCLI`），且两处包 doc 各自自称"唯一入口"（`tagent.go` 包 doc）与"唯一门"（`run.go` 包 doc）——**手段失效导致本质被文档层稀释**，读者无法从符号面判断哪条路是活的。
- **判据**：不能以"反正没人调 Build"结案。导出符号是契约承诺；一个恒失败的导出 `Build` 是对外承诺的**假事实**，且 `Deps` 的 8 个端口（`build.go:22-45`）是**未兑现的接口设计**。这属 B（未闭合），裁决见 §5-G1。

### ② 契约文档与 `// 契约:` 锚——**纯手段**，且该手段在终态已失效

- **手段的目的**：让每条 Godoc 断言可回溯到需求/设计正文（"断言—契约"双向可查）。
- **终态实测**：297 条生产 `// 契约:` 锚 **100%（297/297）指向 `docs/wiki/*`**（§0-16）；而 `docs/wiki/README.md:5` 自述："本目录 25 篇为历史设计记录：逐断言校对的是**已移除的生产实现**……当前架构探索以 `prototype/README.md` 为准"。
- **成因可溯**：`ORCHESTRATION.md:245` 第③条自述"comment_policy 索引行补齐（6 文件，**锚点 docs/wiki 既有章节**）"——即：为满足索引行规则，选择了**当时已存在的章节**，而当时唯一存在的章节体系就是历史 wiki。手段被"形式满足"了，语义没有被满足。
- **穿透**：**锚＝手段，Godoc 语义＝本质**。终态的 Godoc 红线（280 行）与范例（69 口）承载了本域的"可读契约"本质，因此本域**不因锚失效而判不合格**；但锚体系作为"可追溯性"资产已归零，与 09.6 独立结论一致（0 条指向现行架构文档）。裁决见 §5-G5。

### ③ Godoc MUST/MUST NOT 红线＋canonical 范例——手段，但承载本质（不可只删不建）

- 红线文本本身可换措辞（48 文件/280 行，§0-19）；**本质是"多实现者的准入成本"**：新实现者不必读源码即可写出可编译实现（00.26 的盲测依据）。
- 因此 S4 的判据设计正确：`范例 ∧ 红线` **合取**（`check_concept_budget.py` 的 contract-surface 规则）——只有红线没有范例＝"读了仍不知怎么写"；只有范例没有红线＝"能抄但不知边界"。实测 `contract-surface=0`，说明 69 个导出接口两侧都齐。
- **不可反向操作**：为把数字做小而删红线或删范例，等于拆掉准入成本的载体。00.24 的 `--rule S4 gate=0/stale=0/warn=0` 是"出清"，不是"删除"（红线补差 9 口），此点自报与实测一致（48 文件仍在）。

### ④ 桩（phase-1 stub）——阶段手段，但其**生命周期未定义**是本质问题

阶段一约定"零逻辑、`return zero, errors.New(...)`、禁止 panic"是**手段**，目的是让"骨架评审门"能在无逻辑态判定形状。手段的成本被延后支付：一旦进入阶段二，任何未被替换的桩都变成**假事实**。终态实测有 3 个桩仍是导出面：`Build`／`Runtime.CloseSession/Shutdown/Query`／`Runtime.Bridge`。这不是"骨架期遗留"，而是"**门之后没有回收动作**"——00.P0.3 门的判据（"P0 具备 build/vet/gofmt/断言绿且零逻辑"）只管进门，没有"出门即清桩"的条款。

### ⑤ 过往契约不作金标准——本域 6 个具体案例

| # | 契约/规划原文 | 产品实态 | 穿透归因 | 判定 |
|---|---|---|---|---|
| A | 00.10 标题："`CommitRecord` **自包含**扩展" | `CommitRecord` 5 字段（MutationID/TaskID/ExpectedRevision/Status/Outbox），Spec 在 `TaskRecord.Spec`、事件在 `Notice.Event`、Handoff 在 `HandoffRecord` | 事实自包含的本质是"**可从持久事实重建**"，不是"塞进一条结构体"；领域拆开后三处各自有单一归属（02.1/02.8 承接） | **产品成立，契约措辞应改**（不回退） |
| B | 00.9 W 列点名 `plugin/governance.go` | 审批身份实落 `task/hold.go:186-203`＋`task/wrapper.go:251` | 审批是**任务状态机**（parked→hold→decision），不是插件相位；身份+digest 绑定+幂等三项语义完整保留 | **落点迁移成立**；W 列应回写（现仍指向 governance.go，该文件零 `Approv` 命中） |
| C | 00.25 叶名"`ParsedMeta` **瘦身**" | 不瘦身；改为"数据面具名豁免＋文档分组"，分组说明并入块首字段 doc 首句 | S2 元规则（数据保真面不可拆）优先于"瘦身"目标；把分组注释塞进块首字段是对 comment_policy 的合法适配 | **口径勘误成立且已自报**（"波28 口径勘误"） |
| D | 原型 `runEvolution`：**nil 段恒真** | `publishContract.Required = {eval,benchmark,apply-gate}`，缺失即拒 | 原型的"恒真"是**演示用放宽**（当时无评估实现）；生产的本质是**治理不可跳过**。方向相反，语义更深 | **反转正确**（fail-closed 优于 fail-open） |
| E | 原型 `declaresTool`：声明允许集过滤工具 | 无运行时等价物（`AgentSpec.Tools` 只喂 evolution 评测夹具） | **不可归因为"演化"**：这是能力消失而非形态变化。是否"host 侧裁决即足够"没有文档依据 | **B（缺口）**，见 §5-G2 |
| F | golden S03 `model_message_roles` 期望 4 元（assistant 连排） | 6 消息（receipt＋observation 分立） | "受理≠执行"的本质（外部计划 4.2 节）；golden 是规划期按同步心智写的**观察想象** | **golden 应改，产品不回退**（09.7 回溯已同判；尚未裁决，见 §5-G8） |

**由 ⑤ 提炼的判据**：本域凡是"记录/字段/文件位置"层面的契约，一律**不是**金标准（A/B/C）；凡是"不可跳过的治理、不可绕过的身份、不可混淆的受理与执行"层面的契约才是本质（D/F）。E 是唯一不能用"演化"解释的缺失——因此它必须进待裁清单，而不是被 §2 的"深化"叙事吸收。

---

## §4 双维局部评（概念收敛前后 · 职责/多态）

### 4.1 简单：收敛前后量化（本域可判的项）

| 面 | 收敛前（据 concept-slim-plan／00.21 前） | 收敛后（本次实测） | 判定 |
|---|---|---|---|
| `run.go` 单文件 | 1740 行 | 8 文件族，最大 **325** ≤400（§0-12） | ✅ 达成（自报 311，终态 325，不违约束） |
| 导出接口 | 未受控 | `exported_interfaces=69`，S1 命中 **12**（全部具名基线） | ✅ 有界（12 项为"收窄需拆锚"的残留） |
| 导出结构 ≤5 字段 | 大面积超标 | 命中 **54**，全部具名豁免（豁免**类别声明**＝"数据保真面"；条目本身无逐项理由字段），其中含 `config/config.go:AgentSpec` 8 字段（`check_concept_budget.py:99`） | ✅ 类别区分成立；**但** 回溯表称"配置面零债"与 baseline 该行**互斥**（§5-G7） |
| 裸 `ctx.Value` | 3 处裸读 | **2** 处，均在 accessor 实现体（`plugin/policy.go:77`、`task/wrapper.go:54`） | ✅ S3 达成（调用点裸读=0） |
| 范例/红线（S4） | 0 范例/0 红线 | 48 文件 **280** 行红线；**90** 个范例文件；`contract-surface=0`；`go vet ./examples/...`=0 | ✅ S4 达成（红线数自报 143→实测 280，属后续域增量） |
| 冗余切片 | query 12 口→4；plugin 7 窄口→4 相；agent 7→5；memory 12→7 | 本域可见：`plugin/phases.go:24-40` 四相映射；`query` 四口；`memory/search` 单口 | ✅（归各域叶，本域只旁证） |

**达成判据（S1–S4）**：S1＝有界未清零（12 项具名豁免）；S2＝54 项具名豁免（数据面）；S3＝**0 调用点裸读**，2 处合法落点；S4＝**0 债**（范例∧红线全齐）。四门均由 CI 步骤守护（`.github/workflows/runtime-contracts.yml:91,109`），且 selftest 8 条负向锚防"空仓库冒充绿"。

**收敛的边界（如实）**：S1 与 S2 是**豁免制**而非**消解**——12＋54＝66 项债务仍在树上，只是"具名可溯"。豁免制的风险是"基线变成白名单"；本域的防退化手段是 `baseline_stale=0`（失效项必须 warn）＋`--self-check` 三路径。该防线成立，但**豁免理由的语义正确性不可机器验证**（`productVerified=null`，检查器自述"field/redline semantics still need review"）。

### 4.2 职责评（本域交付的 8 文件族）

| 文件 | 行数 | 单一职责（实测函数面） | 评价 |
|---|---|---|---|
| `run.go` | 325 | 装配与发布生命周期：`Assemble`:154／`Publish`:257／`restoreChains`:287／`Recovery`:312 | ✅ 职责单点；`Run` 40+ 私有字段（`:97`）使该文件成为**唯一共享状态容器** |
| `opts.go` | 155 | 依赖注入面：`publishContract`:20＋10 `With*` | ✅ 零选择起步（原型 `Evolution`/`Registry` 都得整包传入） |
| `read.go` | 270 | 只读投影：`Query/Explain/Recall`＋`runReading`/`execLedger`/`ExecLeaseExplainer` | ✅ 读面与写面隔离（只读视图不外借写口） |
| `run_invoke.go` | 179 | 调用与关闭：`Invoke`:33／`Close`:96／`settleOwed`:135 | ✅ 关闭语义单点（拒新→结算→真停） |
| `run_tool.go` | 234 | 工具受理：`deliver`/`suppliers`/`wrap`:98（唯一准入点）/`policyRuling`:203 | ✅ **三族工具（contributed/native set/loader）同经 wrap** 是 S2 反模式的正解 |
| `run_fact.go` | 255 | 事实与呈现：`record`/`sink`/`bookNotice`/`resolveFact`/`takePresentation` | ✅ 呈现（After）与提交事实分离 |
| `run_request.go` | 298 | 请求边界与快照：`requestBoundary`:255／`snapshotRequest`:41／`fileFixer`:211 | ✅ 文件内容在请求边界读（`contentVersion` ≠ 结构代） |
| `run_commit.go` | 165 | 提交面与所有权：`CommitPublication`:27／`runCommit`／`runOwners`／`closeFace` | ⚠️ 职责清楚，但 `CommitPublication` 注释自述持久账"not wired into this minimal entry yet"→ 该文件承载一个**未接通的接缝** |

**切分原则评价**：按**生命周期相位**（装配→调用→受理→事实→请求→提交→读面）切分，而非按实体切分。优点：每个文件对应一个可独立叙述的时序段；缺点：`Run` 成为跨相位的宽对象（40+ 字段），"文件族"降低了单文件复杂度但**没有降低装配对象的内聚成本**——这与块2 G6（现场一体化丢失）是同一枚硬币的两面。

### 4.3 多态评

| 多态面 | 本域落点 | 方法数 | 消费者（生产） | 判定 |
|---|---|---|---|---|
| 插件四相 | `plugin/phases.go`（TransformCall／RiskAssessor+Governor／PostCommitObserver／FinalPresenter） | 1–2 | `agent/loop.go` pipeline＋`run_tool.go:203` | ✅ 窄口＋相位有序；"one ruling 不重复出分"明文化（00.26） |
| 治理 | `state.Governance`（`state/publish.go`，4 条红线） | 1 决策 | `opts.go:61 WithGovernance`；`Assemble` 无 governance 即 `ErrGovernanceMissing` | ✅ 无桩可实现（fail-closed 装配门） |
| 审批 | `task.Approver` | 1 | `opts.go:69 WithApprover`；`run_tool.go:232 denyWithoutApprover`（strict 无 approver 拒） | ✅ 拒绝路径具名 |
| 策略/执行分级 | `plugin.Governor`／`plugin.Enforcement` | 1–2 | `opts.go:80/88` | ✅ 双面不重复出分 |
| 检索/嵌入 | `memory.Embedder`／`memory/search.Searcher` | 1 | `opts.go:106`；`ErrRetrievalUnwired` | ✅ 未接线即具名拒（不静默降级） |
| 读源 | `query` 四口（Reading/ReadingSource/DetailSource/LeaseExplainer） | 1 | `read.go`＋`examples/contract/query/Assembly_example.go` 全链 | ✅ 有 canonical 范例＋全链可跑 |
| **根包宿主面** | `HostBridge`（`host_bridge.go`，4 条红线） | 4 | **无生产实现**；唯一实现＝`examples/contract/root/HostBridge_example.go:52` | ⚠️ **范例冒充消费者**：多态面未被真实宿主选型验证。真实宿主走 `Assemble`＋`Invoke`（`examples/wechat-bot/host_runtime.go:307` 附近），Bridge 未被任何宿主采用 |

**结论**：本域的多态面**设计上**达标（窄口＋范例＋红线＋具名拒），**验证上**分两档——插件/治理/审批/读源有生产消费者；`HostBridge` 无。后者正是"接口自足性"（00.26）能证"能写实现"，但不能证"有人需要它"的边界。

---

## §5 缺口与待裁清单（本域）

> 每条给出：现象（可复跑）／本质／归属／待裁选项。**未裁决不改判定**：下列 G1–G9 均按"未闭合"计入本域 B 面。

| # | 缺口 | 证据（可复跑） | 本质 | 归属／待裁 |
|---|---|---|---|---|
| **G1** | **根包四个公共面，两个是假事实**：**活**＝`Assemble`（生产消费者＝`examples/wechat-bot/host_runtime.go:307`）＋`NewCLI`（`grep -F "NewCLI("` → 调用点仅 6 个**测试**文件，零生产宿主）；**死**＝`Build`（**零调用点**）＋`Runtime.{CloseSession,Shutdown,Query}`（恒 `errNotImplemented`）＋`Runtime.Bridge`（恒 `ErrBridgeNotAssembled`）；`tagent.go` 包 doc 自称"唯一公共入口"而 `run.go` 包 doc 自称"唯一门"；`HostBridge` 无生产实现（仅范例） | `grep -F "Build(" --include='*.go' .` → 0（§0-17）；`grep -F "NewCLI("` → 6 测试文件＋1 定义；`sed -n '35,95p' tagent.go`；`grep -rn ") AdmitInbound("` → 仅 example | 阶段一"零逻辑桩"的生命周期未定义；导出符号成为**假事实**，且两条"唯一入口"誓言互斥 | 00.20（根入口消费面）／待裁：**(a)** 删 `build.go`/`tagent.go` 的非活面（保留 `host_bridge.go` 类型），包 doc 收敛到 `Assemble`；**(b)** 把 `Build` 接成 `Assemble` 的薄包装（`Deps`→`NativeAgent` 的转换层），并给 `Bridge` 一个真实实现；**(c)** 显式标 `// Deprecated: 未接通` 并进 09.5 挂账。建议 (b) 或 (a)，不建议保留现状 |
| **G2** | **声明面三个字段零运行时消费者**：`AgentSpec.Tools`（除 `evolution/criteria.go:69` 的 `evaluation` 夹具）、`ParentID`（无父存在/成环校验）、`TaskPolicy`（仅 `state/channels.go:76` 一个 Dimension 名） | `grep -rn "\.Tools\b"` → 生产侧仅 `evolution/criteria.go:69`；`grep -rn "ParentID"` → 仅 config 解析/指纹；`grep -rn "TaskPolicy"` → 仅 config＋Dimension 名 | 原型 `validateRegistry`/`declaresTool`/`validateConfig` 的**声明面裁决职责在切分中掉地**；"host 裁决即可"无文档依据 | 00.P0.1＋00.20（合同面）／01.1（解析面）／待裁：**(a)** 明示"声明面只承载数据与指纹，引用裁决属 host 契约"并写进 `contracts.md`＋加一条负锚（错配声明必须被宿主拒）；**(b)** 在 `prepare` 补"声明引用 ⊆ 提供者"的比对校验（含父链成环）；**(c)** 从 `AgentSpec` 删除无消费者字段（会破坏契约面，需 01.1 开叶）。建议 (b) 对 `ParentID`/`Tools`，(a) 对 `TaskPolicy` |
| **G3** | **`config.Load` 无生产消费者**：严格解析（含行号/字段路径错误）只有 2 处测试调用；真实宿主 `examples/wechat-bot/host_runtime.go:116-126` **手工构造** `config.Declaration` | `grep -F "config.Load(" --include='*.go' .` → 仅 `config_contract_test.go:86,221`（§0-18）；`sed -n '116,126p' examples/wechat-bot/host_runtime.go` | 本域 00.1/00.3 断言"配置描述"是产品面，但**严格解析能力无生产价值证明**——这是五查中"生产消费者实测"的直接缺口 | 01.1（实现）＋08（宿主）／待裁：**(a)** 让 wechat 宿主改走 `config.Load`（改 YAML→声明文档形态）；**(b)** 明确"宿主自持声明构造"为设计，则 `Load` 降级为工具库并在文档声明非产品路径。建议 (a)——否则"严格解析"只是测试资产 |
| **G4** | **结构门红且无守**：`check_runtime_structure` `ok=False`/16 违例，`config/config.go`（530）在列；该门**未接入 CI**；00.22 的 ≤400 只覆盖 run 族 | `python3 scripts/check_runtime_structure.py --json` → `ok False / 16`（§0-10）；`grep -n "structure" .github/workflows/*.yml` → 仅散文注释 | 00.5 建了结构门，但"红门不入 CI"＝**门存在而防线不存在**；同时 00.6 的"不机械拆包换绿"约束使拆包式修复需授权 | 00.5／00.6／09.3／待裁：**(a)** 以 baseline-ratchet 方式入 CI（先按现状登记 16 项、只降不升，与 concept-budget 同构）；**(b)** 授权拆包逐项清零（`query/snapshot.go` 996 需拆 2 文件）；**(c)** 明示"≤500 仅约束 run 族"并把其他包从规则范围移出。建议 (a)＋(b) 选包执行 |
| **G5** | **契约锚 100% 指向历史文档**：297/297 生产 `// 契约:` 指向 `docs/wiki/*`，而该目录自述校对的是"**已移除的生产实现**"；成因见 `ORCHESTRATION.md:245`"锚点 docs/wiki 既有章节" | §0-16；`sed -n '1,10p' docs/wiki/README.md`；`sed -n '245p' openspec/changes/runtime-rebuild/ORCHESTRATION.md` | 可追溯性资产已归零；Godoc 语义（本质）尚存，锚（手段）失效 | 00.6/00.23/09.6／待裁：**(a)** 建"现行架构文档"根（如 `docs/architecture/*`），把 297 条锚分批重指（可脚本化：按包→章）；**(b)** 明确锚只作"历史出处"，在 `comment_policy` 里改称 `// 由来:` 并另立现行锚规则。建议 (a)，与 09.6 的闭合动作合并 |
| **G6** | **`Rollback` 公共出口未接**：`state.Publisher.Rollback` 只有测试调用；`Run` 无 Rollback 口 | `grep -F "Rollback(" --include='*.go' .` → 生产侧仅 evolution 的 `refine.Rollback`（不同物）；`state` 侧全在 `tests/integration/state/*` | 09.4 soak 实证的产品面缺口；本域从"根入口消费面"视角复证：`Bridge` 面也未暴露回滚 | 00.20／09.5（已登记）／待裁：**(a)** `Run` 增 `Rollback(ctx, decl)` 薄口（转 `Publisher.Rollback`）；**(b)** 并入 G1 的 `Build/Bridge` 收口动作一并处理。建议 (a)，成本低且闭合 09.4 缺口 |
| **G7** | **概念预算数字三处互斥，无对账表**：① 00.21 自报 `baseline 164 项`，实测 **68**；② `check_concept_budget.py:95` 注释"S2 **53** 项"，实测 **54**；③ 09.7 回溯表称"S2 53 项豁免＋**配置面零债**"，而 baseline `:99` 正登记 `config/config.go:AgentSpec` **8 字段** | §0-8；`grep -n "S2 struct-fields\|config/config.go:AgentSpec" scripts/check_concept_budget.py`；`sed -n '140,146p' evidence/09.7-retrospective-review.md` | "164→68"无法用任何现存文档对账（S4 出清 58＋新债 9＋S1 12＋S2 53＋S3 2 ≠ 164）；豁免制一旦失去对账，基线即白名单 | 00.21／09.7／待裁：**(a)** 补一张"基线演进对账表"（每波：新增/出清/失效，合计=期末），并在 checker 里加 `baseline_entries` 与文档数字的一致性断言；**(b)** 修正三处文字（53→54；"配置面零债"删除或改为"配置面 1 项具名豁免"）。**两者都要做**；这不是文字洁癖，是 ratchet 机制的可信基础 |
| **G8** | **golden 4 处 drift 未裁决**：S03 roles/receipts、S08 blocker/advancer 词表、S09 tokenizer 三态 | `evidence/09.7-retrospective-review.md:104-115`（归因表）；`python3 -c` 解析 `golden/runtime/scenarios.json` → 10 条，仍为规划期期望 | golden 是"观测想象"，产品是"领域事实"；不裁决则 09.1 的对照永远带 4 个已知偏差 | 09.1／09.7／待裁：**(a)** 按归因表改 golden 对齐产品（推荐，4 处均有领域事实支撑）；**(b)** 产品回退（无归因支持，不建议）。**需用户批**（09.7 明记"待用户批"） |
| **G9** | **00.3 G1 状态未闭合**：证据自述"G1 通过仍待全部域 P0 done，不宣称"；终态全部 P0 done 已达成，但无任何文档宣称 G1 通过 | `task-graph.json` 00.3 evidence 原文；`openspec/changes/runtime-rebuild/contracts.md`（118 行）无"G1 通过"结论 | 一个"条件已满足但结论未落"的合同门：既不能算通过（无宣称），也不能算失败（条件已成立） | 00.3／待裁：**(a)** 由主编排者补一句 G1 判定（附探针清单核对）；**(b)** 若探针不足则开补差叶。**不能保留"待核"状态进归档** |

**不在本清单的项（已由他块/他域承接，避免重复记账）**：会话正文持久账未接（`run_commit.go:27`，归 03/08，块2 已记 G7）；`agent/invocation.go:45 Aggregate()` 桩（归 02，块2 G1）；`state/store.go:10` 陈旧文案（归 01，块2 G4）；真实资源面（模型/embedding/collector/tokenizer/S10）挂 09.5；队列背压/崩溃一致属已决策接受边界。

---

## 结论（不作汇总替代证据）

- 本域 **29/29** 叶在图上 `done`、在文件上 `- [x]`、且与 `check_runtime_plan` 的硬校验一致（§0-5）；逐叶落点全部可定位（§1），无"只勾不落"的叶。
- **A 面（原型/规划要求且已实现）＝18 叶纯 `A` ＋ 4 叶 `A＋C`**（其余 7 叶为 `A＋B`，见 §1 表注 2）：原型 config/state 的 16 条特性声明中，**同构 4／深化 8／部分 2／缺失 4**（§2.3）；四处缺失同源——"声明面裁决职责在组合根切分中掉地"（G2）与"一体化热加载入口消失"（G3）。
- **B 面（未闭合）＝9 项**，全部具名且可复跑（§5），其中 **G1／G3／G5** 是"符号/锚/消费者"三类假事实，**G4／G7** 是"门与账"的可信度问题，**G8／G9** 是"待裁/待宣称"的流程债。
- **C 面（新增演化）**：概念预算机器门（S1–S4＋棘轮＋8 负向锚）、fail-closed 三段裁决（`Required` 反转到原型的 nil 恒真）、8 文件族按生命周期相位切分、69 口范例∧红线合取——均为原型所无，且有机器判据。
- **不作 PASS 判定**：本域"实现面"大体成立、"承诺面"存在 3 处假事实与 1 处账不平。是否可归档，取决于 §5 中 G1/G3/G4/G7 的裁决结果；G5/G8/G9 建议与 09.6/09.1/00.3 的收尾动作合并处理。
