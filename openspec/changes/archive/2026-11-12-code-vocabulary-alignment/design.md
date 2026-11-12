# Design: code-vocabulary-alignment

> **甲 修订（取代旧「指纹棘轮＋泛义保留」方案）。** 勘误：旧 D6 把「GLOSSARY 保留泛义 carrier、仅检查点义退役」当立案前提，据此建 2377 行分类基线豁免 ~340 泛义 carrier。该前提系执行期 agent 决定被误标为 user_ruling（W8-VOCAB）；用户明确「我没有裁定过泛义保留」。GLOSSARY 行 3「描述机制一律用代码实名」未登记 carrier 为规范词，§五冻结仅 reincarnation 族。carrier 是多义旧词，应**全退役**，每处改其精确既有规范词。甲＝carrier 全退役＋纯短语禁门（无基线/分类/棘轮/镜像豁免），净负代码。
>
> 旧方案已落地的**检查点义** carrier→checkpoint 注释改名（journal/restore/memory:418/invoke/checkpoint/segment_store）与 folding generation→band promotion（age_segments）、stripFoldGenerations→stripRecorderGenerations（history/event_integrity_test）经复核映射正确，**保留**；甲增量＝泛义 carrier 退役＋门重写＋基线删除。立案输入仍是词汇审计（候选修法，R34）；用户原症状＝「代码词汇违反维护规范、与文档脱节、维护熵增」。

## D1 症状锚（立案前提，R33）

- 锚·词汇对齐门：`scripts/gates/check_vocabulary_alignment.py` **纯短语禁**，扫 `*.go`＋`docs/api/*.md`，命中 carrier（大小写不敏感子串）／generational|fold generation|folding generation／api/plugin 内 Product／读透视图|链权威答案 即红。**基线红＝当前树 390 命中**（341 .go＋49 docs/api 镜像），全退役＋godoc 重生成后绿（0）。
- 负锚：`test_check_vocabulary_alignment.py` 植入每类违规（carrier 标识符 commitCarrier／carrier 散文／跨行 folding generation／单行 folding generation／foldGeneration 驼峰／generationalBand／字符串字面量／api/plugin Product＋MaterialProduct／pluginProduct／退役词）门必拒 exit 1；合法形（gen:／instance_id／recorder generation／compress.product／小写 product／动词 carries/carried/carrying）不误拒。只打印不拒＝空转，自测先红。
- 消费者锚：门接 `scripts/gates/lint.sh` policy 块（test+check 对）＋`.github/workflows/ci.yml` Lint step 实跑 lint.sh；门红则 CI 红，非仅本地脚本。
- 工程目标只能追加，不得替掉此锚；工程全绿而锚红时以锚为准。
- 预测答案：完成后「代码词汇与文档脱节、熵增」不再静默发生——门在 CI 恒判定，新 carrier/禁用短语即红。证据＝门对清理前树红（390）、清理后绿（0）、仅回退清理门复红（因果）、负例自测真拒（非空转）。

## D2 子域表（spec 最小化：不新建能力）

**反增殖裁定（用户核心关切）**：规范词已各有其主——`session-checkpoint-material`／`deterministic-compress-level`／`fold-provenance`（recorder 世代＝instance_id／gen: 锚，合法）／`restart-recovery-vocabulary`（已禁退役词）／`fact-vocabulary`。新建 `code-vocabulary` 能力即重复＝用户所要防止的词汇增殖。故**全变更只一处 spec delta：`architecture-guardrails`**（02 持有）；01/03 纯实现子域，满足该 req＋既有机制 spec，无 spec 文本。甲把 carrier 统一到**既有**精确词（commit/DurableCommit/credential/URL/projection/envelope/fact/record），是统一相似概念词汇，非造新概念。

| # | 子域 | spec 锚 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | residual-cleanup | 满足 architecture-guardrails 词汇 req＋既有机制 spec；无 delta | carrier 全退役（泛义→精确词，见 D6 映射）；旧检查点义改名保留；godoc 重生成 | 02 门先红（症状锚证据） |
| 02 | vocabulary-gate | **architecture-guardrails**（唯一 delta） | 纯短语禁门（无基线/分类/棘轮/镜像）＋负例自测＋删 2377 行基线＋lint/CI 接线 | 无 |
| 03 | structural-golden | 既有；无 delta | 已完成（checkpointSliceMailbox 常量去重＋window_bounds golden＋SnapshotCheckpoint/RestoreCheckpoint 评估保留） | — |

spec 下沉到二级；一级 `specs/` 只留 orchestration。02 的 `specs/architecture-guardrails/spec.md` delta 归档转正合并入主 spec；01/03 无 `specs/`。

## D3 执行轨（甲：触碰集不相交并发）

| 轨 | 内容 | 文件簇 | 解锁 |
|---|---|---|---|
| C | 门重写 550→~150＋删基线＋自测 480→~120＋lint 注释 | check_vocabulary_alignment.py／test_…py／vocabulary_alignment.json(删)／lint.sh:126-129 | 立案即跑 |
| A | 生产 .go carrier 退役（97 命中/24 文件） | D4 A 簇 | C 门红锚捕获后 |
| B1 | 测试 .go（state/assembly/agent，113 命中） | durable_test.go／task_checkpoint_test.go／host_tools_test.go／handoff_test.go | 同 A |
| B2 | 测试 .go（task/integration，131 命中） | runtime/task/*_test.go／tests/integration/** | 同 A |
| D | golden/wiki/GLOSSARY 漂移 | architecture.json:1206/1211／durable_truth.json:204／wiki task-lifecycle.md:9／GLOSSARY carrier 退役注 | A/B 后（概念镜像对齐） |
| E | godoc 重生成 | docs/api/*.md（派生，49 镜像命中） | A+B 全部 .go 注释定稿后，编排者单写 |

**红锚捕获**：C 完成后、A/B 派发前，编排者跑新门于当前树＝390 红（症状现），存证入 run-notes。波次仅汇报分组，解锁＝依赖就绪（R3）。

## D4 合并规则（触碰集不相交）

- **A 簇（生产，非测试）**：`runtime/task/lifecycle.go`(commit 字段改名)／`runtime/state/durable.go`(newCarrier→newDurableCommit)／`assembly/commit.go`(commitCarrier→commitLedger)／`tool/subagent/tool.go`(局部 carrier→result)／`features/rl/capture.go`(credential/URL)／`features/rl/export_chain.go`／`memory/query/{snapshot,skeleton_assert,subscribe}.go`／`runtime/state/{memory,store,mailbox,skeleton_assert}.go`／`runtime/task/{commit,spec}.go`／`runtime/agent/{handoff,delegate}.go`／`assembly/{facts,run,compress_consumer,read,host_attention,skeleton_assert}.go`／`api/event/types.go`。
- **B1 簇**：`runtime/state/durable_test.go`／`assembly/task_checkpoint_test.go`／`assembly/host_tools_test.go`／`runtime/agent/handoff_test.go`。
- **B2 簇**：`runtime/task/{lifecycle,reactivate,logical,restore,checkpoint}_test.go`／`tests/integration/**`（identity_commit_contract／task_lifecycle／handoff_future_notifications／history·host·execution event_integrity／state delegation_lineage_record／inspection runtime_inventory／execution native_tool_admission·new_tool_adapter／acceptance scenario_s08／host request_boundary）。
- **C 簇**：`check_vocabulary_alignment.py`／`test_check_vocabulary_alignment.py`／`vocabulary_alignment.json`(删)／`lint.sh`。
- **不相交实证**：A 全非测试、B 全测试，文件级不相交；级联符号均包内私有——commitCarrier 仅 assembly/commit.go(:346/350/362)；newCarrier 仅 durable.go(:227/236/237)；seedCarrier 仅 durable_test.go(:1239/1242/1330)；MemoryRegistry.carrier 字段仅 lifecycle.go(:245/380)；logicalRig.carrier 仅 logical_test.go(:85/139/164)→**无跨轨级联**。导出 API 零改动。
- **golden 耦合裁定**：architecture.json/durable_truth.json 的 concept/reason 散文是**描述性注册文本**，check_runtime_architecture.py（结构/边/event-ownership 调用面）与 check_durable_truth_registry.py（前缀 name+value 注册）**不逐字比对代码注释**→D 独立于 A 无门耦合；A 不改任何前缀常量，注册保持有效。概念镜像 durable.go，A 后对齐。docs/api **派生**自代码注释（gen_godoc.sh「derived, never hand-edited」），E 必须 A+B 后单写。

## D5 三级验收

- 孙任务：exit 0＋正锚（点名命令逐字符一致）＋负锚（具名错误路径）＋消费者锚（真实路径调用证据）。
- 子域：01＝门 carrier 规则全绿＋受影响 go test 绿＋godoc --check 绿＋零 carrier 残留（grep）；02＝负例自测真拒每类＋合法不误拒＋门接 lint/CI＋基线已删＋净负；03＝已绿。
- 变更级 DoD：①门红转绿证据入 run-notes（清理前 390 红＝症状现／清理后 0 绿／**仅回退清理**门复红＝因果／负例自测真拒＝非空转。门是本变更新建，完整回滚会移除门，故因果取「回退清理」）；②全量 `lint.sh` exit 0（真实仓根，dotted-refs 需 .git 为目录）；③`git diff <基线>..HEAD -- docs/api` 仅含 godoc 重生成；④冻结归档仅附勘误未改写原文；⑤外来未跟踪文件零触碰；⑥**净负代码**（门 550→~150、自测 480→~120、基线 2377→0，改名不增行）。

## D6 核心：前提勘误＋更名映射＋门形态

### 前提勘误（元规则一/二）

旧前提「GLOSSARY 保留泛义 carrier、仅检查点义退役」**无 GLOSSARY 依据**：行 3「描述机制一律用代码实名（FactLog/DurableCommit/restoreChains/compress.product）」未登记 carrier；§五冻结仅 reincarnation 族。carrier 是多义旧词（事实载体/提交承载/凭据/URL/查询载荷/检查点），统一退役＝消除多义、统一相似概念词汇（用户核心关切），非增殖。每处改其**精确既有规范词**，不造新概念。旧基线 JSON:4 的「user_ruling 泛义保留」文本随基线删除而消失；design/run-notes/02-spec 一致重写并附勘误。

### 更名映射（派发权威；目标词皆既有，级联已核）

**标识符（包内私有）**：
- `lifecycle.go:157` 字段 `carrier Commit`→`commit Commit`；`:187` 参 `NewMemoryRegistry(carrier Commit`→`commit Commit`；`:189` `carrier: carrier`→`commit: commit`；`:245` `r.carrier.(specDeclarer)`→`r.commit`；`:380` `r.carrier.Commit`→`r.commit.Commit`。
- `durable.go:237` `func newCarrier(log FactLog)`→`newDurableCommit`；`:227` 调用 `d := newCarrier(log)`→`newDurableCommit(log)`。（与导出 NewDurableCommit 不撞。）
- `assembly/commit.go:350` `type commitCarrier interface`→`commitLedger`（据 :356 既有散文「stored in the commit ledger behind it」）；`:362` `inner commitCarrier`→`inner commitLedger`。
- `tool/subagent/tool.go:508` 局部 `if carrier, ok := d.delegation.(ResultDelegation)`→`result`；`:509` `carrier.CompleteWithResult`→`result.CompleteWithResult`。
- 测试：`carrier` 局部(*MemoryCommit/*DurableCommit)→`commit`；`seedCarrier`→`seedCommit`；`logicalRig.carrier` 字段→`commit`；`reactivationFact(carrier *MemoryCommit`/`reactivationRollbackFact(carrier *MemoryCommit` 参→`commit`。

**散文（按义类）**：
- 提交端口后端：「in-memory carrier behind the Commit port」→「in-memory implementation behind the Commit port」(commit.go:153)；「The memory carrier keeps」→「It keeps」(commit.go:156/memory.go:77)；「the carrier」→「the commit」/DurableCommit；「durable carrier behind the task commit port」→「durable implementation of the task commit port」(durable.go:159)；「commit carrier」→「commit port」；「carrier tables」→「commit tables」(durable.go:191)。
- Store 面：memory.go:74「in-memory carrier behind the Store face」→「in-memory implementation behind the Store face」。
- 凭据：capture.go:145/204/210/412「credential carrier」→「credential」；:424「transport credential carrier」→「transport credential」；export_chain.go:928「transport carriers」→「transport credentials」。
- URL：capture.go:177/296/472「URL carrier」→「URL」；:231「carriers」→「URLs」。
- 投影：query/skeleton_assert.go:13/16/232「query/inventory carriers」→「projections」；subscribe.go:44「carriers」→「payloads」/:104「range carrier」→「missing range」；snapshot.go:781「frozen carriers」→「frozen Detail values」/:980「word-sized carrier field」→「word-sized field」。
- 信封：handoff.go:17「every carrier」→「every representation」；handoff_test.go:16/132「carrier」→「envelope」。
- 事实/记录：types.go:4「fact carrier」→「fact contract」；lifecycle.go:114「no fact carrier」→「no fact」；run.go:3519/compress_consumer.go:33「one durable carrier of」→「one durable record of」；facts.go:352「no durable carrier to write」→「no durable ledger to write」；host/event_integrity_test.go:326「the carrier」→「the record」。
- 账户：mailbox.go:58「the same carrier」→「the same account」。
- 首请求：host/event_integrity_test.go:714/715＋execution/event_integrity_test.go:1405「first carrier」→「first request」。
- 任务层：read.go:365/runtime_inventory_test.go:69「task carriers」→「task layer」；read.go:369「commit carrier」→「commit」。
- 判据：request_boundary_test.go:261「a criterion carrier」→「a criterion」。
- 检查点义（**旧已改，保留**）：journal/restore/memory:418/invoke/checkpoint/segment_store carrier→checkpoint/cut。
- 余「commit carrier」/「durable carrier」散文→「commit port」/「commit」/「durable commit」/DurableCommit（spec.go/store.go/delegate.go/host_attention.go/各 skeleton_assert.go/lifecycle.go 散文/各测试）。

### 门形态（纯短语禁，无基线）

| watch | 规则形态 | 范围 | 合法形（不匹配禁式，自然不拒） |
|---|---|---|---|
| carrier | 大小写不敏感**子串禁** | `*.go`＋`docs/api/*.md` | 动词 carries/carried/carrying 不含 carrier 子串 |
| compression-generation | **短语禁** `generational\|fold\s*generation\|folding\s*generation`（注释续行先归并散文再匹配，跨行不漏；单行只计一次不重复） | `*.go`＋`docs/api` | gen:／instance_id／recorder generation |
| plugin-product | 大小写敏感**子串** `Product` | 仅 `api/plugin/` | compress.product 事实族（不在 api/plugin）／小写 product 散文 |
| retired-word | **短语禁** `读透视图\|链权威答案` | `*.go`＋`docs/api` | — |

**无豁免/分类/棘轮/镜像**：命中即红。删 `load_registry`/`hit_key_order`/`baseline_entry`/`write_baseline`/`verdict`/`EXEMPT_CLASSES`/`CLASSIFICATIONS`/`DEFAULT_REGISTRY`/fingerprint sha256/`--registry`/`--write-baseline`/镜像逻辑。保留 `scan_files`/`prosaic`/`joined_pieces`/`piece_line`/`go_prose_paragraphs`/`doc_paragraphs`/`scan_line`/`scan_prose`/`scan_tree`(简化)。exit 0 净／1 命中。carrier/Product 单 token 可逐行扫；compression/retired 短语走归并散文扫（含单行+跨行，不重复计数）。

### 被否方案

- **旧甲·指纹棘轮＋人工分类基线**：2377 行基线＋550 行门＝术语收敛却巨量净增（用户审计症状）；分类把「泛义保留」伪前提固化；棘轮摩擦无收益（carrier 应全退役而非评审保留）。否。
- 上下文感知（carrier 邻近 checkpoint/slice 才拒）：brittle，关键词共现≠语义，漏检/误拒难控。否。
- ad-hoc 复合标识符黑名单（只禁 carrierFrameValueBytes 等已知形）：窄，新变体漏检，门沦为黑名单而非不变量。否。
- **纯短语禁（甲选择）**：carrier 全退役后无合法子串命中，门简单恒判定；compression/Product/retired 短语禁皆 GLOSSARY 明文点名（行 41／族 8／restart-recovery-vocabulary）。跨行归并**保留**（age_segments 实证漂移形，删＝缩水验收）。

## D7 约束（不可越）

- **外来未跟踪文件零触碰**：`examples/wechat-bot/skills/interview/agent-interview-helper/`／`openspec/changes/runtime-architecture-convergence/`／`scripts/gates/check_event_semantics_symptoms.py`／`runtime_budgets.py`／`test_check_runtime_symptoms.py`。foreign migrate-kv 簇（`examples/wechat-bot/{main.go,tagent.yaml,README.md}`／`scripts/migrate-kv/`／`docs/api/…migrate-kv.md`）留树不动，词汇 commit 排除。
- **冻结契约拼写不更名**（GLOSSARY §五 reincarnation 族＋TestScenario05CloseHandoffReincarnation／TestReincarnationNoticePreserved）；冻结归档只附勘误不改写（`attribution-carrier` 系冻结归档散文非活标识符，不在门面，留不动）。
- **Phase2 运行时修复零触碰**（已完成）：`api/event/checkpoint.go` 分区键／`assembly/checkpoint.go`／`memory/ledger/migrate.go` verify／`scripts/recover-kv/`。
- commit-scope：docs 提交禁夹带代码路径（.go/.sh/.py/.yml/.yaml）；godoc 重生成(.md)可随代码提交。
- doc-budget 棘轮只降不升；删基线/缩门/缩自测净负。
- **不提交/不推送/不归档**（A3 发布授权为法定停点，待用户明示；docs 推送授权不延伸至词汇）。