# architectural-subtraction · findings

本文件记录本变更实施过程中**跑过、量过、判过**的事实：真实命令、真实退出码、真实
归属（哪条红属于本变更、哪条不属于）。一条结论若没有可复跑的命令与退出码，就不写进来。

---

## 结构收拢·scripts（2026-10-27，基线 HEAD `c868230`）

### 裁定与边界

编排者裁定：`scripts/` 已成垃圾堆（32 个文件平铺，检查器与运行器、自测与资产混在一起），
按**职责**收拢为两个目录，`codetools/`、`comment_policy/`、`hooks/` 原地不动（模块路径与
钩子路径冻结：前者是 `go run ./scripts/codetools`、`go run ./scripts/comment_policy` 的
模块入口，后者由 git 钩子按路径安装）。

**不动根路径 `.go`**：根包拆文件由第三方评审判为「单纯拆文件不解决」，等归因结论另行处理。
本单只搬 `scripts/**` 与引用面。

### 文件去向

| 去向 | 内容 | 数量 |
|---|---|---|
| `scripts/gates/` | 检查器：`check-openspec.sh` `check_comment_only.sh` `check_concept_budget.py` `check_coverage_floor.sh` `check_dependency_debt.sh` `check_doc_budget.py` `check_event_integrity_mutations.py` `check_runtime_{architecture,evidence,plan,structure}.py` `check_test_merge.sh` `race_check.sh` `gen_godoc.sh` `runtime_checklib.py` `doc_budget.json` | 15 |
| `scripts/gates/` | 自测：`test_check_{concept_budget,doc_budget,event_integrity_mutations,runtime_architecture,runtime_evidence,runtime_plan,runtime_structure,runtime_workflow}.py` `test_check_package_layout.sh` `test_race_check.sh` | 10 |
| `scripts/gates/` | 门禁实体：`lint.sh` `preflight.sh`（文件名不变，供根 wrapper 转发） | 2 |
| `scripts/scenarios/` | 运行器：`run_runtime_scenarios.py` `verify_restart_prefix.py` `verify_runtime_acceptance.py` `verify_doc.py` `convert_trajectories.py` `find_task_output.sh` `logprobe.sh` `proc_fingerprint.sh` | 8 |
| `scripts/scenarios/` | 自测：`test_run_runtime_scenarios.py` `test_verify_restart_prefix.py` `test_verify_runtime_acceptance.py` `test_convert_trajectories.py` | 4 |
| `scripts/`（根） | thin wrapper：`lint.sh` `preflight.sh` → `exec bash "$here/gates/<同名>"` | 2 |
| 原地 | `codetools/`（13）`comment_policy/`（3+testdata 2）`hooks/pre-commit`（1） | 19 |
| 删除 | `comment_sweep.py`（零引用，仅归档 change 的 tasks 文本提及）`__pycache__/`（6 个 .pyc，未跟踪） | — |

`runtime_checklib.py` 是**一份**共享库（fingerprint/diff/contract 助手），留 `gates/`；
`scenarios/` 的运行器用显式 `sys.path.insert` 指向 `../gates` 导入，而不是复制第二份。

### wrapper 决策：留根，转发实体

两条候选：(A) 全仓引用改到 `scripts/gates/lint.sh`；(B) 根留 11 行 wrapper 转发。
取 (B)：`bash scripts/lint.sh` 与 `bash scripts/preflight.sh` 出现在 CI、`CONTRIBUTING.md`、
`docs/OPERATION.md`、`ISSUE_TEMPLATE`、`hooks/pre-commit` 与使用者的肌肉记忆里，(A) 要为一次
纯搬家改动支付全仓外部引用面的重写，(B) 只支付 22 行。且规则仍只有一份（在 `gates/`），
wrapper 不复制任何判据，不存在第二处漂移点。

实体脚本自身 `cd "$(dirname "$0")/../.."` 声明仓库根——原 `lint.sh` 信任调用方 cwd，
搬深一层后必须自解析，否则从任意目录调用会读错树。

### 必须同步的内部语义（搬动即失效的硬编码）

1. `__file__` 推根：`check_event_integrity_mutations.py`（`DEFAULT_ROOT`）、
   `check_runtime_evidence.py`（`EVENT_REPO_ROOT`）、`verify_doc.py`（`DEFAULT`）、
   5 个 gates 测试的 `REPO = SCRIPTS.parents[1]`、`test_check_runtime_workflow.py` 的
   `parents[2]`——全部从「上一级即仓库根」改为按新深度推根。
2. 基线与资产自指：`check_doc_budget.py` 的 `DEFAULT_BASELINE` → `scripts/gates/doc_budget.json`；
   `doc_budget.json` 的 `measured_by` 与告警文案同步。
3. `runtime_checklib` 裸导入：`gates/` 内 7 处同目录导入不变；`scenarios/` 侧改
   `sys.path.insert(0, <repo>/scripts/gates)`。
4. `proc-refs` 白名单：`scripts/codetools/check.go` 的 `procRefSkip` 键是**仓库相对路径**，
   12 个键随文件改名，否则门假红（漏改即「白名单指向不存在的路径」）。
5. 钩子：`scripts/hooks/pre-commit` 调 `check_test_merge.sh` 的路径 → `scripts/gates/`。
6. 生成面：`gen_godoc.sh` 内嵌自身路径进 `docs/api/index.md`，改后重生成（1 行变更）。
7. CI：`.github/workflows/ci.yml`、`runtime-contracts.yml` 的 13 处命令路径；
   `test_check_runtime_workflow.py` 的 `GATE_STEPS` 关键字与
   `python -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'` 精确串
   （workflow 门把 CI 里的命令当契约读，改一边不改另一边会红——这正是它该红的地方）。
8. 文档/规格/golden 的 18 个文件按「长串优先」做行内替换（不增删行，doc budget 行数中性）。

### 验收凭据

| 门 | 命令 | 结果 |
|---|---|---|
| 检查器自测（新路径） | `python3 -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'` | `Ran 233 tests … OK` |
| gates 全部自测 | `python3 -m unittest discover -s scripts/gates -p 'test_*.py'` | `Ran 305 tests … OK` |
| scenarios 自测 | `python3 -m unittest discover -s scripts/scenarios -p 'test_*.py'` | `Ran 171 tests … OK` |
| 包布局门负例 | `bash scripts/gates/test_check_package_layout.sh` | `OK (5 fixtures)`，exit 0 |
| doc budget（行数中性） | `python3 scripts/gates/check_doc_budget.py` | `202 files / 22737 lines`，`slack 0/0`，`RESULT: ok`，exit 0 |
| wrapper 转发（参数） | `bash scripts/preflight.sh --bogus` | exit 2，报错来自实体：`preflight.sh: unknown argument --bogus` |
| wrapper 自解析根 | 从 `examples/` 执行 `bash ../scripts/lint.sh --only fmt` | gofmt 与全部自测照跑（gates 305/233/test_race 15 例、scenarios 均 OK），证明实体不信任调用方 cwd |
| CI workflow 自测 | `python3 scripts/gates/test_check_runtime_workflow.py` | `Ran 44 tests … OK`，exit 0 |
| 三模块 `-short`（工作树） | `go test ./... -short -count=1`（根 + `examples/wechat-bot` + `examples/cli-host`） | `39 ok` / `1 ok` / `1 ok`，三者 exit 0 |
| **clone 级全门** | `git clone <repo> /tmp/tv-scripts && bash scripts/lint.sh` | **exit 0，`lint: ok`**——gofmt/modules/build/vet/注释棘轮/doc budget/selftests/架构政策/概念预算/实现覆盖/事件归属/产品包零发现/openspec strict/包布局负例/点号引用/提交范围 全部过（19 个 stage 无跳红） |
| clone 级三模块 | 同上 clone：`go test ./... -short -count=1` ×3 模块 | 根 `38 ok` exit 0（比工作树少 1 个包＝外来 `zzdualattr` 不存在）、`examples/*` 各 `ok`；scenarios 自测 `Ran 171 … OK` |

`discover -s scripts` 的原命令在搬家后**必然**落到 0 个测试（unittest 只递归带
`__init__.py` 的子目录，本仓脚本目录无包声明），因此 `-s scripts/gates` 是唯一诚实写法，
CI 与 `lint.sh` 两侧同步改并各有一条断言钉住。

### 邻接物（不属于本单，如实登记）

1. **外来未跟踪目录 `zzdualattr/`**（含 `attribution_test.go`）在库内存在。它使四条门在当前
   工作树上必红，与本单无关（同一条输入、四处红）：
   - `comment_policy` 棘轮：10 findings 全在该文件（`REGRESSION free-standing` 6、
     `missing-test-responsibility` 1、`test-doc-not-one-sentence` 3）；
   - `check_runtime_architecture.py --implementation`：
     `unknown first-party package 'zzdualattr': not registered in architecture policy`；
   - `codetools name-check`：`ITERATION-NUMBER-NAME zzdualattr/attribution_test.go:
     TestE1ClaimAtomicity / TestE2DualPath / TestE3ConsumptionWindow`；
   - `gen_godoc --check`：`Only in <tmp>/gen: github_com__SpellingDragon__tagent__zzdualattr.md`。
   本单不删、不改、不 stage 该目录（属并发归因工作在途产物）；干净树（clone）上同四条全绿，
   见上表 clone 级全门。
2. `openspec/changes/fidelity-and-recovery/findings.md` 与 `evidence/` 里的旧路径**不追改**：
   那是过去式运行记录（「当时的实测命令」），改路径等于改写历史证据；且该文件当前有并发会话
   的未提交改动，动它会把他人部署混进本提交。登记于此，不静默遗漏。
3. `docs/.dev/` 下带日期的探索纪要与 `openspec/changes/archive/` 同理，属历史冻结记录，不追改。
4. **clone 级偶发一次红（已归因，非本单）**：首次在 clone 跑根 module `go test ./... -short` 时
   `tests/integration/host/TestExecFaceWiredFromDocument` 报了「closing request carries no
   confirmed release of tmux/…」——该轮与另一个测试批（scenarios 自测）在同机并发。复核：
   同树单测试 `-run TestExecFaceWiredFromDocument` 绿、同树单包 `-short` 绿（25.2s）、
   同树全量重跑 `38 ok` exit 0；基线 clone（`c868230`）全量 `38 ok` exit 0。本单 diff 内
   产品 Go 仅 `scripts/codetools/{check,check_test,package_layout}.go` 三个门禁工具文件，与
   `tests/integration/host` 无调用路径。结论：并发/共享 tmux 命名导致的偶发，与本单无关。

### 回滚

纯搬迁 + 引用同步，无行为变更：`git revert <本提交>` 即回到平铺布局。唯一的语义增量是
wrapper 与实体自解析根，revert 后两者一并消失，无残留状态。

---

## 波 1 集成裁定（2026-10-27，集成-1，HEAD 前置 8919acf）

三轨并发（A1: 1.1+1.3 ∥ B: 1.2 ∥ C1: 2.3）落地后，编排者集成修复与裁定：

- **裁定①（1.1 负锚构造等价）**：D6 原文「注入拒写 tier→age_fold 提交失败」不可构造——fold 事实 `Owner.AgentID==""` 在 persistFact 早退于 tier 之前（反事实实证）。负锚改为公共入口两类真实拒写（无事件时间/同键异内容），不变量同向（拒绝可达+活账本零写入+具名错误），具名测试与 `-run` 正则逐字符不变。非缩水，登记于任务行。
- **裁定②（read.go 计划缺口）**：design D0 列 read.go 为 1.2 消费者，但白名单无归属——跨轨编译红 5 处（facts.go×3/read.go×2 直接字段引用）。编排者按 R8 修复：`body.Content→body.Message.Content`、`body.Summary→body.Meta[event.MetaKeyEventSummary]`；memory/query_events.go Godoc 同步（stored→derived 措辞）。
- **裁定③（1.3 重定基线）**：双路径第二面（r.history）在 8919acf 已不可达——CommitHistory 的映射门先于本变更存在、AttachReplay 注册门已不存在。本叶实为删死代码：负锚 TestSingleHistoryPath 基线即绿（行为不回归锚），红转绿由结构断言（history[ 2→0）承载。SessionHistory=单面空、CommitHistory=文档化 no-op，与 host 既有冻结锚（断言 SessionHistory==0）一致；HistoryFace 契约文档补 mapped-sequence 例外（loop.go）。tools.go ageFold 挂载门 `MaxTokens>0` 保留——它是呈现/fold 门，G7 子测 presentation_is_unlimited_at_zero 钉住。
- **裁定④（2.3 driver 失败落点）**：`Pipeline.After` 无 error 返回（skeleton_assert+根包两调用点钉住），driver 失败以 gap+导出 sentinel `plugin.ErrObservationObligationRetained`（errors.Is 可判）落 presentation 读面；义务重试路径（mailbox 账）未动。机制为未导出 `driverObservation` 接口+`DrivesObligation()` 标记，S4 概念预算实测无新债。
- **行为变化登记（1.2）**：混有旧代 event_key body 的 state 冷启动具名硬失败（ErrEventBody 上抛），需删状态重建——D3.2 no-legacy 裁定的必然推论；当前格式 body 无损（summary 在 meta、content 在 message）。
- **2.1 边界登记**：SettleDriver pump 跨会话串行（drain 逐 seat 驱动）=已知边界；减法变更不加机制，并行化另行立案。C1 增量注释措辞 retained→owed 以保结构断言 `grep -cE 'retained|firing|rearm' settle_turn.go`=0 逐字成立。
- **词法门**：新测试文件 comment_policy 5 finding（audit-marker "legacy" 措辞/测试文档单句/free-standing 注释）由编排者清零；`TestDecodeEventBodyLegacyRefused` 测试名保留（标识符不在注释门面）。
- **归属待裁（随波2-A2）**：rl LeaseObserver 分类——Driver 需自有持久账户（现无）vs Observer-only+边界登记。

**波 1 集成凭据**（逐条原始退出码）：build RC=0；gofmt 空；vet RC=0；TestFactFidelity/RequestFidelity(冷启动)/TestHistoryRestoresWithoutBudget/TestFoldCommitFailureSurfaces|TestSingleHistoryPath/TestDecodeEventBodyLegacyRefused/TestObserverLayering 全 RC=0；TestSettleWakeSingleDrive -count=3 RC=0（10.1s）；runtime/agent+telemetry+rl -short RC=0；全 host 29.8s RC=0；全仓 short 38 包 RC=0；history[/snapshots grep=0、settle grep=0。

---

## 波 2 集成裁定（2026-10-27，集成-2，波前 HEAD a63429c）

双轨（A2: 2.2 ∥ C2: 3.1）+ 补差单 A2b 落地后的编排者裁定与跨域修复：

- **裁定⑤（2.2-P1 语义收窄）**：capture 关闭时快照读面由「有」变「空」——接受方案 A：快照=capture 材料（评审减法原文「过去模型实际看到什么由请求快照记录」的正式读法），无条件 Run.snapshots 正是被删的重复权威；formal 三夹具补挂 WithCapture（断言一字未改）。
- **裁定⑥（2.2-P2 能力缺口）**：VisibleSkills 经 `agent.BoundaryView.VisibleSkills` 传入（A2b 越白名单一处 request_assembly.go，追认：避免 loop 侧第三份技能命名规则复制）；唯一新增导出字段，概念预算实测无新债。
- **死面删除**：runtime/agent 的 RequestSnapshotFace/SetRequestSnapshotFace/fixRequestSnapshot 族全仓零引用后删除（loop.go 净 −42 行）；`Recorder.RecordDecision` 仍只传 visibleTools（decision 记录面非 capture 面，签名被钉住）——遗留边界登记，不动。
- **LeaseObserver=Observer-only**（A2 裁）：无自有持久重试账户（五项接线直发 Replace，失败只上抛记 gap）——按 C1 机制「仅自有 durable 义务账户方可标记 Driver」；replacement 失败的补装版义务无自动账户，边界留此。
- **3.1 跨白名单三处由编排者落地**：evolution/governance/policy_test 删 6 行（闭表覆盖经 NewFlow→validatePort 承载，M3b 变异实证）；host log_observation 日志断言迁移（phase=consume→port=fact->observation）；wiki §1.1 两处+EXTENSIONS.md 去相位改写、docs/api 重生成（GEN_RC=0）。
- **doc-budget 棘轮重锁（先例㉕）**：+27 全来自归档/发布交付面（fidelity 归档 orchestration 三 requirement 并入=口径边界迁移；v0.4.0 CHANGELOG/README=前批漏锁），本变更对语料净 0——具名记账后直接落基线 22764（doc_budget.json rebaseline_reason 追加）。
- **变异伴锚迁移（先例裁定③）**：MUT-CAND-01.8-storage-success 伴锚 2 `[]byte(body.Content)`→`[]byte(body.Message.Content)`（集成字段派生所致）；主锚仍健在（count=1）；迁移后主树全量 11/11 killed（evidence /tmp/w2-mutations，MUT_RC=0）。
- **继承红登记**：check_runtime_structure（tmux_executor.go 547>500）为评审基线前既有（720df14/bc1a3ad 早于 8afa6cfc），不在 CI/lint 门面、不在本变更 diff——登记不修（拆文件=增量工作，越减法案范围）。
- **波 1 门禁终态**：worktree(a63429c) 13 门 RC=0；lint 红=doc-budget（已重锁）、mutations 红=伴锚陈旧（已迁移+主树 11/11）——两者均已在主树修复复验；long 六片后台进行中（结果落 evidence）。

**波 2 集成凭据**：build 0；snapshots grep=0；RequestSnapshotFace 族 grep=0；plugin.Phase/RouteOf 全仓 grep=0；TestRequestSnapshotsSingleSource/TestFormalRequestSnapshotHasNoSecrets（三子测红→绿）/RequestFidelity|TestRuntimeVisibility/settle/api-plugin/evolution 三包/telemetry/rl/runtime-agent 全 RC=0；全仓 short 39 包 RC=0；全 host RC=0；变异 11/11；doc-budget RESULT ok。

---

## 波 3 集成裁定（2026-10-27，集成-3，波前 HEAD 379885a）

A3（3.2）落地后编排者迁移与裁定：

- **裁定⑦（3.2 跨白名单五件由编排者落地）**：①history/projection_replay_and_budget_test 删 Decision 查表段，二次装配语义改「同预算现算同一视图」（断言强度保持：同视图+不含全文）；②acceptance/scenario_s07 观测块改按 `entry.Content==original` 判 full，观测键 `presentation_full_text_iff_decision_full`→`presentation_full_text_iff_full_room`；③golden/runtime/scenarios.json 同键重命名（验收契约随语义，D3.4 登记）；④主规格 event-sourced-projection 两处「原文、模型视图与呈现决策分层」直写为「原文与模型视图分层（视图按当前事实、声明与预算现算）」+change 内 MODIFIED delta 逐字一致（归档零 diff，fidelity 先例）；⑤FACT-VOCABULARY 删族 6 行（编号留空位不重排，稳 ID 优于重排）+投影账本段改「呈现不落账」、GLOSSARY「结算」词条改任务结算语义。
- **settle 家族整体删除追认**：A3 反事实甄别成立（RecordDecision 唯一调用者是装配自身、Decision 生产消费者 0、replay.settle 无事件族注册、fold 恢复不读决策）——「保留机制解耦复用」会是零消费方的空转状态，违反减法本义，整体删除正确。
- **A3 自行决策追认**：noticeCard 下限形态（ticket 先占位→状态→材料头，bound=min(room,300)）；负锚扩为三测；projectionBudget 读 Original 带 keyless 回退；测试并入既有文件（词法门镜像要求）。

**波 3 集成凭据**：build 0；history/acceptance 迁移后 vet 0、TestProjectionReplayAndBudget ok、acceptance 全 ok；docs/api 重生成 GEN=0；delta `openspec validate --strict` 过；G9 numstat 184/0 零改动。

---

## 收尾补记（2026-10-27）

- **03.11 长跑红片最终定性（整夜矩阵）**：18 跑 3 红（s42-w32768×1 / s42-w256×2，同签名 settle-ack 60s 超时）全部重跑恢复绿，含**冻结红树原片三跑转绿**与等价行为树通过——非确定时序 flake；中途「终套两连红→暂定回归→二分（基线/波2 绿）→等价树绿→冻结树绿」全程留档 F2 证据（小样本误判的自纠记录）。WATCH：3/15 vs 0/2 不足以排除本变更轻微加大 settle 时序压力，CI 复现则测 60s 界裕度。
- **另案 doc-budget-lockstep（用户裁定「应该是0」）**：源于本案波 2 集成的 +27 漂移与手工重锁——严格零差额（两向 error+--tighten 同提交锁步）+ 钩子死路径修复+markdown 暂存守卫，独立立案独立归档（7387d28/2ec21f2），不占本案面。
- **lint 中间态教训（R16 形）**：openspec archive 搬移文件后、git add 前跑 lint → index 持旧径触发 tracked-hygiene 红伪影；净树复验绿。归档流程中 lint 须在 add 之后跑。
