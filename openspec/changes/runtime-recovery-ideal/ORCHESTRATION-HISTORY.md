# ORCHESTRATION · runtime-recovery-ideal

> 派发唯一依据。金字塔四层不得倒装：①结论 ②编排四问 ③明细 ④台账。每次派发前重读对齐，单完/波后回填台账。纪律载体是本文档非会话记忆（R27）。

## ① 结论

**当前状态（R262，2026-11-18；下方旧轮次数字仅为历史记录）**：未完成，提交、推送与归档暂停。用户已授权卡点操作，但 R35 仍有实质恢复缺陷、重复投递未定因和证据脱树；不能把“有意红”排除出放行面。

**本轮路径**：核验 G79 删除与覆盖承接；从 G78 留存探针和日志恢复调查；完成 12.R1 的实际落盘核销，推进 12.R2 应用失败阻止切面、再推进 12.R3 安全注销。代码定稿后再统一收证据和独立复审，不在每次局部绿后重跑整矩阵。

**已有成果的边界**：历史测试通过只证明当时树的相应场景。06.R3 尚有按键起点不可达的成本轴；12.R2/R3 仍有真实红；G79 删除的成本测试仍待独立确认覆盖承接。未授权削减这些工作来换取完成。

### 历史结论快照（R112–R210，不作当前派发依据）

- **抵达终态的路径**：三层去物化（日志层窗口已有界→停整篇重放删 `l.index`→store 常驻索引不物化全量）＋尾谓词接缝＋删切面族与外部折回脚本＋端口收口＋契约/文档/门同步 → 症状直译锚转绿 → 09 校准与全矩阵 → 独立拷问 frontier 空 → F1–F4。
- **最大风险**：**锚量代理量而生产前向不走新机制**（R89 实证：索引建了维护了组件测了但读侧零生产调用方）。故每叶的消费者锚必须是正式入口，且判据量 boot 真付的字节/墙钟，不量索引层自己的计数。
- **当前状态（R112 时点，取代 R93 时点旧述）**：**三层去物化已完成**（日志层窗口化＋`l.index` 删、store 常驻索引去物化、尾谓词接缝导出 `ledger.CommitOrder`），boot 症状锚三道计数界**全 0 且 PASS**，走查计数面 visits/valueBytes/valueReads 比值**均 1.00**。**唯一未达标＝长跑锚三阶段**（boot 1.63–2.24×、sessions 2.95×、frame 4.51×，界 1.3），**根因已统一判定＝checkpoint 重述已结项，违反主 spec「已结部分 SHALL 留在链上而不嵌入 checkpoint、大小 SHALL 有界于开放项数量」**（属因实测：body 1,118B/已结任务，tasks 切片占 80%、compress 占 19%）。06.R12 正在实施冻结修法（开放全文＋已结身份水位＋`Seat` 懒水合＋`restoreChains` 水位跳过，照 `delivery` 切片既有形态）。
- **门禁面（R115 按 R35 第一轮实测逐项更正，取代 R112 旧数字）**：`vocabulary`／`durable_truth`／`spec_hygiene`／`product_surface`／`doc_links`／`plugin_mounting`／`assembly_symbols`／`schema_allowlist` **exit 0**；`comment_policy` 产品面 **0 finding**、棘轮面 **1 beyond baseline**（仅 `scripts/fold-cold-root/main.go:1` doc-not-brief；症状锚两条已清）；**`concept_budget` exit 1＝10 error**（含本变更自己新落地未登记的 `CommitOrder`/`IndexEntry`/`ChainSource` 三个 D1、`new=58>56`、`exported_interfaces=87>85`、`StatedTask` 17 字段 > 基线 16）；**`doc_budget` exit 1（+2）**；**`dod_coverage` exit 1（P0–P8，22 行）**。**旧述「concept 剩 2 条归 10.R8b／comment 剩 3 条」是过期读数，已废**——欠账实在 R108/06.R12 新增名，不在 10.R8b。
- **🔴 中心论点当前判定（R210，取代 R112/R115 时点旧述；上两条门禁数字为历史时点读数，现态见 R208/R210）**：**只兑现「boot 索引走查有界」**（visits/valueReads 恒 4、valueBytes 252→268，9 次 run 一致），**未兑现「恢复成本随历史有界」**。G36 配对实验（深/浅根背靠背各 boot 21 次、奇偶换序、比值中位、3 run）确证 **depth252 boot＝1.643/1.735/1.724×，p10 全高于 1.3 界，随深度单调**（30→1.13、60→1.19、120→1.28、180→1.40–1.58）⇒ **增长是真的不是噪声**。**~78% 增量住在两条现有四量量不到的 O(历史) 读**：①**主因＝`journal.go:1490 eachRecordOnce` 逐批 pread 整张 slots 表**（前缀过滤在 visitor 内才做），scan 61.5µs(6 代)→2383.8µs(252 代)＝**38.7×**，零匹配反证随表容量而非命中数；**HEAD 是内存 map 遍历，本候选改成磁盘全表扫 ⇒ 本候选引入的回归**；boot 调用链 `assembly/commit.go:221 configChain.loadLocked`→`tier_facts.go:35 Scan`。②次因＝`:615` 开档全 log CRC（8.41MB、2.0–2.4ms，`openBytes` 量不到）；**副本实验确证它非主因**（改回水位起点比值仅 1.643→1.558，residue 增量不变），R182 保留裁定不受影响。**另勘误：先前把长跑 FAIL/FAIL/PASS 归因「机器离散」是错的**——`judgeFixtureDispersion` 量 total(boot+work) 而界逐 stage 判，boot 仅占 13–18% ⇒ **轴不对齐**，run1 Fatalf 在守卫上真增长根本没被判、run2 同噪声放过 1.297× boot。**G44 在修**（消除全表扫／补 `integrityBytesScanned`＋`slotTableBytesScanned` 入计数面／判据四点校准，**禁调大 1.3× 界禁删守卫禁引第二真相**）。**抵达 DoD 仍须：中心论点证成＋症状锚回滚即红两次证据（F3 须重做，R205：旧凭据的锚与机制全零命中）＋G8 P1 红线修复＋F1/F2/F4。**

**就绪队列（R112 时点按触碰集重算；06.R12 在跑，写 runtime/task＋runtime/state＋assembly 切片面）**：
| 叶 | 写集 | 与 06.R12 | 可派否 |
|---|---|---|---|
| **10.R7** golden 同步（`RelocateReport` 腐化绑定、S1 基线路径、变异登记表） | `golden/runtime/**` | **零 .go，完全不相交** | **可派（纯 Python 门）** |
| **R35 独立拷问**（自始未派＝流程债） | 无（只读） | **不相交** | **可派** |
| **09.R2 校准设计**（界推导、噪声判据、真实 checkpoint 切片采样） | 09 域 design（只读代码） | **不相交** | **可派** |
| fold-cold-root 注释门 1 条 | `scripts/fold-cold-root/main.go` 注释 | 不相交（**仅 gofmt，禁编译**） | 可派（小） |
| 06.R12v 变异证明＋全量复跑 | 同 06.R12 | **真依赖** | 待 06.R12 |
| 08.R4a 删切面族＋读路径具名拒绝＋漂移守卫＋损坏检测面 | `memory/ledger/{journal,journal_kv}.go` | 写不相交，但 **06.R12 验证面编译 ledger ⇒ 反向污染** | **待 06.R12** |
| 10.R8b 端口收口 | `runtime/state/durable.go` 等 | **写相交** | 待 06.R12 |
| 两枚既有红（`TestConsumptionFailureIsReportedByName`／`TestDurableCommitBarrier`） | runtime/task、runtime/state | **写相交** | 待 06.R12 |
| 09.R3 全矩阵、F1–F4 | — | — | 待必需叶核销 |

## ② 编排四问

### 次序

派发原子是就绪叶或同文件簇的叶集合，不是域。**并发准入＝触碰集不相交（改写文件 ∪ 构建闭包 ∪ 验证读取目标），不是「同包就串行」**。同包但写集不相交者，用「写单只跑文本门、编译验证留给后续独立验证单」的手法并发（V1/G1/G2/G3 四单并发已验证此手法）。**真依赖**（后叶须前叶的产物或读数）才串行。

**当前队列（R264）**：

| 工作 | 所辖叶/问题 | 写面 | 验证与释放 |
|---|---|---|---|
| ✅ 编排者 | 12.R2 J1 主体＋G83 三项 P1 修复（重试回退边界／拒绝不按链划界／锚界只界投影） | `runtime/state/checkpoint{,_test}.go`、`assembly/facts.go`、`assembly/run.go`、`memory/replay/replay_budget.go`、`memory/replay/skeleton_assert.go`、`assembly/checkpoint_test.go`、`assembly/run_test.go`、`assembly/anchored_boot_parity_test.go`、`docs/api`（生成） | 三处红锚先红后绿；state `-race -count=10` 绿；全量 assembly 仅剩 S5（12.R3）红；comment_policy 触碰包零 findings；裁定记录于 12 域 design.md §十八 |
| 12.R3 | 卸载拒绝与未完项数 | 卸载路径（S5 红锚已立） | `TestUnmountKeepsTheOwedItemsOfItsSlice` 转绿 |
| 重复投递受控复现 | G78 配额中断、G81 BLOCKED 的接续 | 仅 /tmp 探针与真实已结任务夹具；主树投递路径暂不改 | 复现终局（两次生成两输出事实的切点证据）后另立修复叶 |
| 09.R2 / F1–F4 | 阶段归因重建；契约零改动断言；R35 复审；归档 | 各域 | R35 frontier 非空即停提交（用户已授权卡点推进，不解除该停点） |

两只只读单可并行；编排者暂不修改其调查的投递路径与成本测试。12 域生产修复前按准确落点另立叶单，写/编译窗口与调查实跑错开。共享账目与跨域门文件仍由编排者单写。

**下表为 S3a 时点的历史队列，已失效，不作派发依据**：

| 叶 | 改写集 | 与 S3a 触碰集 | 可派否 | 验证入口 |
|---|---|---|---|---|
| **S3b** 症状锚重述＋openBytes 新语义 | `assembly/symptom_anchor*_test.go` | 写不相交，但**须 S3a 的读数** | **真依赖，待 S3a** | 症状锚三面读数 |
| **06.R3扩** store 常驻索引去物化 | `memory/ledger/segment_store.go`、`assembly/read.go` | **相交** | 待 S3a | `indexEntries` 界 |
| **08.R4a** 删切面族 | `memory/ledger/journal.go` | **相交** | 待 S3a | 零残留 grep |
| **08.R4b** 外部折回脚本 | **新建** `scripts/fold-cold-root/**` | **不相交** | **可现在派** | 干跑零写入＋对已有写者根具名拒＋折回后逐字符相等 |
| **05.R3-甲** 尾谓词接缝 | `atomic_store.go`/`journal_kv.go`/`config.go`/`rustviking_kv.go`＋6 替身＋assembly | **相交** | 待 S3a | `durableLoadCount` 界 |
| **A1** assembly 锚合批 | `assembly/*_test.go`（governance 重述＋05.R2/07.R2/01.R1/11.R2/12 锚） | **写不相交** | **可现在派**（文本门） | 编译验证留后续单 |
| **10.R5** spec/capability 删除 | `openspec/specs/**`、各域 `specs/**` | **完全不相交** | **可现在派** | openspec strict＋spec_hygiene |
| **10.R6** 退位/冷层叙述改写 | `docs/**`、08/09 域 design/tasks 叙述 | **不相交** | **可现在派**（索引叙述部分待 S3） | doc_budget/doc_links/doc-refs |
| **10.R8a** 端口语义归属推导 | `changes/10-*/design.md`（只读代码） | **不相交** | **可现在派** | 推导须先于 10.R8b 实施 |
| **D1** 关档竞态只读诊断 | 无（隔离树） | **不相交** | **可现在派** | `TestAnchoredBootMatchesFullBoot` 红原文归属 |
| **D2** tornTail 运维可见 | `scripts/recover-kv/main.go` | **不相交** | **可现在派** | dry-run 打印撕裂尾＋零写快照不回退 |
| **10.R7** golden/门同步＋N14 | `golden/runtime/**`、`runtime/state/durable.go` | 不相交，但**切面条目须 08.R4a** | 部分可派（N14＋非切面条目） | concept/durable_truth/product_surface |
| **10.R8b** 端口收口实施 | 新 api 叶子＋4 包＋examples＋golden | 与 A1 同触 assembly 包 | 待 10.R8a 推导＋A1 | 净减 2 接口、旧名零残留 |
| **09.R2/R3、F1–F4** | — | — | 待必需叶核销 | 全矩阵＋独立拷问 |

### 授权

**实施派发逐叶精确到文件白名单**，只含本单所需；生产代码可改（旧「本轮只改规划文件」条款已作废——那是规划期的授权，实施期早已开始）。共享面（`ORCHESTRATION.md`、`golden/runtime/architecture.json` 的棘轮数字、主 spec 契约）编排者单写或按名裁定后交单。数据根/端口/长跑资源不共用；真实用户数据始终禁读（`examples/wechat-bot/.wechat-config/data` 与用户 `--source` 根）。

**外来未跟踪文件零触碰**（每单必带）：`openspec/changes/runtime-architecture-convergence/`、`scripts/gates/check_event_semantics_symptoms.py`、`scripts/gates/runtime_budgets.py`、`scripts/gates/test_check_runtime_symptoms.py`、`examples/wechat-bot/skills/interview/agent-interview-helper/`。冻结归档 `openspec/changes/archive/**` 只附勘误不改写。

**门禁面＝全门**（R79 立）：`bash scripts/lint.sh` 是 CI 唯一入口。每单验证面必含该命令或逐条列全其中门；**只点 comment_policy 两 scope 不构成凭据**。纯文本单可只跑文本门但须具名声明「未跑编译面」。计时单运行期间其他单不得跑编译或全量测试。

**源树绑定**：每叶声明它量的是 HEAD `9547faca` 还是工作树；CI 原始记录一律绑 HEAD。基线对照须用**真 git worktree**（`git archive` 无 `.git` 会使 doc_budget/concept_budget fail-closed，读数不可用）。

**提交/推送/归档须用户明示授权**；build/test 与 commit 永不串联；不得抬基线掩盖 finding（门自带的「按名抬升＋理由记台账」协议除外，见 R83/R85）。

### 凭据

正/负/消费者锚来自每叶任务行；命令、实际命中测试数、原始退出码、源树身份、参数/环境、真实入口及结果文件逐项对齐。工作区时点、已提交本地、CI提交三者不混用。静态检查仅证明登记存在，动态结果证明行为；未实际执行的新增测试名不作证据。

反例必须真的应用、原版通过、变异在目标路径失败；编译失败、跳过、无关测试失败不算杀死目标变异。指标记录真实含义，库存键数量不冒充恢复读取量，计时不重复累计也不漏目录写入。所有量需独立证据，不靠一个更早失败的量替其过关。

**门禁面＝全门，不是手挑子集（R79 立）**：`bash scripts/lint.sh` 是 CI 唯一入口，串 gofmt／三模块 build+vet／comment_policy 两 scope／doc_budget／检查器自测／三挂点五守护＋各自测／**词汇对齐**／窗口登记／DoD 覆盖／运行时契约／event-ownership／godoc 新鲜度／tracked-hygiene／package-layout／dotted-refs／commit-scope 等。每张实施单的验证面必含该命令（或逐条列全其中门），**只点 comment_policy 两 scope 不构成门禁凭据**。纯文本改动的单可只跑文本门（词汇门、comment_policy、doc 门），但须具名声明「未跑编译面」并把它留给后续验证单。计时单运行期间，其他单不得跑编译或全量测试——负载会污染读数（R78④ 的歧义即由此生）。

### 释放等待表

| 资源/共享面 | 持有者 | 释放凭据 | 等待叶 |
|---|---|---|---|
| 事实提交签名与写者范围 | 编排者/G1 | 正反例及所有公开入口枚举 | 11.R2、12.R2、08.R1 |
| 状态清减/旧键语义 | 编排者/G2 | 水位+未完集合+有效状态+历史读一致 | 05.R2、07.R2、03.R2、12.R2 |
| 引擎与计量定义 | 编排者/G3 | 同条件剖面、成本归属、校准界限 | 06.R2、08.R1、09.R3 |
| 实际证据记录格式 | 编排者/G4 | 真实runner结果与拒绝反例 | 13.R2、13.R3 |
| 完整长测资源 | 运行单唯一持有者 | 实际进程/每job终态及制品 | 09.R3后续矩阵、最终独立审查 |

释放后当场检查等待叶；“波次完成”不是停点。无新证据时不重复重跑长任务，不用超时循环输出的exit 0冒充被监控程序完成。

## ③ 明细

本轮三份文档派发：A只写06/08/11四件套，B只写05/07/12四件套，C只写09/10/13四件套；主线程写一级及01–04。它们是规划产出单，不是域级实施授权；写集互不相交，禁止运行会改生产树的验证。

实施合单候选由实际叶得出：facts/run与提交、消费、恢复集成相交；durable/task/state的快照与消费相交；ledger的索引、退位、原子提交相交；scripts的运行器与证据聚合相交。派发前须得到 `go list -deps` 或等价静态依赖结果与精确写入集，当前不声称这些簇可并发。独立只读审查可并行，执行计时不能与修改同一实现的写任务并行。

规模沿用可跑原型＋全量项目门。性能预估尚未校准；新方案不附虚构完成时间。真实服务部署与用户数据不属于合成验收授权；临时合成根的独立模块测试属于完整验收。新旧测试/迁移/故障注入的成本和资源随执行单记录。

## ④ 台账（原始历史保留；当前状态由本节末尾勘误及上文决定）

### R0 立案（规划）

- 用户终裁：全修到干净终点＋清理废弃；物理有界乙；可跑原型＋全量门。症状锚 S1/S2 确认。
- 立档：一级 proposal/design/tasks/orchestration spec + 六域二级四件套（01-06）落盘。
- 实施期发现回写 design：D6.8（引擎索引自持，02 循环依赖闸门）/ D6.9（boot 入表时序）/ D6.10（能力名归正 session-checkpoint-material，task-skeleton:51 未反向只:74）；§五 indexEntries 改 len(s.index)（02/04 冲突裁定）。
- 编排者纠偏：orchestration spec 改 delta 格式（## ADDED Requirements）修 strict 红。
- 待核（W0 前）：05 报 16 继承 spec 中 8 已 superseded、活条款 10（含 memory-curation:24/:33/:38）；六 Material 端口仅 compatMaterialOf 生产零消费（审计后裁定）；4 测试锚被 config-surface 未提交变更占用（后置）。

### 停点（R31）

- **实施已授权**：用户明示「代码执行派发 openspec-apply 子代理完成」——「实施待审 design」停点解除，进 Phase 6 执行编排。F4 归档＋commit/push 仍须用户明示（未授权）；config-surface-normalization F4 授权不被本变更 extrapolate。
- 后续逐轮回填：W0 症状锚红凭据 → W1 合单 → W2 → W3 → W4 F1–F4＋S1/S2 红转绿两次证据。

### R1 执行启动（W0 派发）

- 用户授权实施（openspec-apply 子代理）。重读 ORCHESTRATION 二节四问对齐：次序 W0→W1 合单→W2 并行→W3→W4；共享面 §五已冻结（存活权威/四量 len(s.index)/引擎自持索引 D6.8/退位 identity-read/boot 入表 D6.9）。
- **W0 派发单 P0**：写症状直译锚 S1（TestJournalBytesBoundedByLiveSet，journal 真字节有界）+ S2（TestSymptomAnchorBootCostFlatAcrossGenerations，boot 四真量平坦；实际锚名去 Session 避 checkpoint_test.go:1231 同名冲突，见 R1-W0 核销）并复跑**红**（今天 O(全史)）。白名单＝新增独立测试文件（避开 config-surface 占用的 checkpoint_test.go 等 4 文件）；禁改生产代码、禁 git commit、禁触外来文件与 config-surface 未提交变更。凭据＝两锚 exit≠0 原始输出＋非空转证明（用例数>0）。
- W0 绿（红凭据成立）→ 当场派 W1 合单 P1（01+02+04，主线程串行原子）。

### R1-W0 核销（编排者亲验，凡报必验 R7）

- **S1 复跑红** ✅：`go test ./tests/integration/history/ -run '^TestJournalBytesBoundedByLiveSet$' -count=1` exit 1；journal 真字节 [15714 31464 47214 62964 78714] 线性增（≈4.99×），分母自证（EventCount=45=5×9 历史真增长），红因落字节增长非空转。
- **S2 复跑红** ✅：`go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1` exit 1；开档读字节 [0 132695 321759 549178 814866]、索引 [0 43 94 145 196]、durable 前缀键 [0 12 24 36 48] 三量线性，分母自证（20 任务真结清）。
- **白名单核** ✅：git status 新增恰 2 文件（assembly/symptom_anchor_recovery_test.go、tests/integration/history/symptom_anchor_journal_test.go），config-surface 占用面与外来文件未触；go build ./... exit 0。
- **锚名归正（R29 漂移回写）**：W0 为避 config-surface 占用的 checkpoint_test.go:1231 同名锚，S2 新锚名 `TestSymptomAnchorBootCostFlatAcrossGenerations`（去 Session）于 symptom_anchor_recovery_test.go。已回写 04 spec/tasks/design/proposal + 01·1.4 + 02·2.4 消费者锚指向新名；既有 Session 锚（正文计数代理量）保留不动作历史对照。04·4.3 由「改既有锚」改为「精化 W0 新锚断言面为四真量」。
- **W0 启动门达成**：S1/S2 红凭据成立＋§五契约已冻结 → 释放 W1。

### R2 充分并发派发（用户裁定：最大化并发，<3 才合单）

- **并发三轨同一回合拉起**（触碰集不相交）：
  - **P1 W1 合单**（01+02+04，boot 路径簇 run.go/checkpoint.go/durable.go/journal.go/segment_store.go）——同文件簇必须串行，三域无法拆 3 独立代理 → 合单（<3 例外，用户裁定）。
  - **P2 03 governance 托管**（features/evolution/governance/{denial,goal}.go + host.go:608 + approval_store.go + 新 governance 切片文件）——与 boot 路径簇不相交；Stateful 接缝已冻结（§五），03 注册新切片不依赖 W1 代码 → 提前并发（原 W2 门禁按用户并发裁定解除）。**禁触 run.go boot 路径簇**（若需改 run.go 上报编排者）。
  - **P3 05·5.4.1 六 Material 端口审计**（只读，零写入）——与所有写轨不相交 → 并发。审计表落 /tmp，不改代码（5.4.2 裁定待审计后）。
- 白名单（P1）：boot 路径簇生产文件＋新测试；**禁改 config-surface 占用 4 测试文件**与 checkpoint_test.go（既有 Session 锚保留）；禁触外来文件/冻结归档；禁 git commit。
- 关键闸门：D6.8 引擎索引自持（不从 checkpoint 派生）；D6.9 boot 入表时序；§五 indexEntries=len(s.index)。
- 凭据：每叶 verify 三要素 exit 0＋S2 转绿＋go build 每步可编译；完成度五查＋波后独立验证（R30）。
- 等待表：P1 绿（S2 转绿）→ 释放 05 转正/死码（5.1-5.3，待代码稳定）；P2 绿＋P1 绿 → 释放 W3 06 退位（依赖存活权威）。

### R2 回收（三轨战报＋编排者亲验）

- **P3（05·5.4.1 审计）完成** ✅：六 Material 端口审计表落 /tmp/recovery-05-audit/（241 行 123 处 file:line）；compatMaterialOf 生产运行期 0 次（7 生产类型全实现 Material()）；六端口常量活（两案 MUST NOT 退役）；**5.4.2 建议乙**（保留兼容读法＋勘误）——硬约束：session-checkpoint-material/spec.md:38 活条款「六端口 SHALL 保留到迁移完成」＋Instance 2→3 方法触 POLYMORPHIC_METHOD_LIMIT=2 棘轮＋收益仅删 20 余行。编排者待裁 5.4.2（倾向乙）。
- **P1（W1 合单）部分完成 4/13**：01·1.1（切片三族收据）/02·2.3（静默跳页改响亮）/04·4.1（四量探针）/04·4.2（多代多任务夹具）✅ 复跑绿；S2 仍红（02 核心未落地）。**设计阻塞 A（重大取向，停点）**：indexEntries 逐代线性增（43→94→145→196），有界必裁剪存活键集，裁剪即让被裁键 GetEvent 答 missing（identity-read 破），须 06 退位透明回冷层承接 → **02 与 06 固有耦合，S2 无法在 W1 单独转绿**。三选一待用户裁：甲 02+06 合波 / 乙 W1 临时热层兜底＋W3 冷层 / 丙 index 裁剪延 W3。
- **P2（03 governance）撞 150-turn 上限**：部分完成，TestGovernanceSliceWiredInProduction 挂死 9m07s（影响整包 CI）→ 编排者诊断＋窄派收尾（记忆：耦合簇撞上限改主线程诊断再窄派）。
- **编排者亲验**：go build ./... exit 0；concept_budget FAILED（StatedConsumption/StatedRefusal 无 concept binding＋domain_surface.new=53>47＋exported_interfaces=84>83）→ 编排者处理（绑定 live spec＋具名抬棘轮，M4 先例）。
- **待裁清单**：① 02/06 耦合排序（甲/乙/丙，重大取向，用户裁）；② 5.4.2 甲/乙（编排者倾向乙，可独立裁）；③ concept ratchet 抬升（编排者具名处理）；④ 04·4.4 验收面重裁（单进程内 boot×2<全史对照恒假，改 F3 级联回滚证据）；⑤ 尾键集接缝 §五 冻结（编排者单写）。

### R3 用户裁定甲：02+06 合波（重大取向已裁）

- **用户裁定甲**：02（有界索引裁剪）与 06（冷归档 identity-read）合为一个原子波——索引裁剪与冷层承接同批落地，S2 一次转绿，无「GetEvent 临时答 missing」中间态，验收面不缩水。
- **波次重构**：W1＝01+04（task 托管＋探针，P1 已部分完成）；**W2＝02+06 合波（索引裁剪＋冷层 identity-read＋排他安静点＋脚手架接线）∥ 03 governance（disjoint）**；W3＝05 契约同步/死码（待代码稳定）；W4＝F1–F4。
- **编排者独立裁**：② 5.4.2＝乙（保留兼容读法＋勘误，审计三硬约束）；④ 04·4.4 改 F3 级联回滚证据（单进程对照恒假，不新增二进制内对照锚）；③ concept ratchet 具名抬升＋新类型绑 live spec（M4 先例）；⑤ 尾键集接缝由编排者冻结签名（§五）。
- **P2 governance 撞上限**：编排者先诊断挂死测试（TestGovernanceSliceWiredInProduction）＋已完成面，再窄派收尾（记忆：耦合簇撞上限改主线程诊断再窄派）。
- **合波设计闸门（编排者冻结再派）**：索引切片格式＋裁剪输入面（存活权威 §五）＋冷层结构（<root>/cold 第二 journal 根）＋GetEvent 三态透明回冷＋排他安静点（Run 停写→drain→swap 持 journalKV.mu）＋保序逆帧序去重。合波大，按串行子步窄派（防 150-turn）。

### R3 编排者独立裁＋再派发

- **5.4.2＝乙（编排者裁，审计三硬约束）**：保留六端口兼容读法＋只落 5.2.2 勘误。依据 P3 审计：session-checkpoint-material/spec.md:38 活条款「六端口 SHALL 保留到迁移完成」（甲＝跨域契约变更）＋Instance 2→3 方法触 POLYMORPHIC_METHOD_LIMIT=2 棘轮＋甲收益仅删 20 余行而改造面 15 契约示例＋17 测试 face。六端口常量两案均不退役（活分派键）。compatMaterialOf 生产 0 调用但保留为合法可选声明形态。
- **04·4.4 重裁（编排者）**：单进程内 boot 与 seedFullReplay 共用 OpenTier，三量恒等⇒「boot×2<对照」恒假，不可判定。改为 F3 级联回滚证据（回滚 01/02/06 后同一 S2 命令必红），不新增二进制内对照锚。
- **concept ratchet（编排者处理）**：新类型 StatedConsumption/StatedRefusal/Receipts/SettleAccount/StateSeat 须绑 live spec（session-checkpoint-material）concept/kind；domain_surface.new/exported_interfaces 棘轮按具名抬升（M4 先例，记理由）。各域代理负责绑定本域新类型，编排者核棘轮抬升。
- **再派发（并发）**：P2-fix（03 governance 挂死诊断＋收尾，窄单）∥ P4（02+06 合波，按 D6.12 串行子步）。03 挂死根因待 P2-fix goroutine dump 定位（seat.Bind 流扫或 Checkpoint 切面锁）。

### R3 回收（P2-fix＋P4 战报＋编排者亲验）

- **P2-fix（03 governance）完成 4/4** ✅：**「挂死」是误判**——assembly 包真实工作量 260–334s（TestCloseStopsTheResidentsTheRunMounted 30s 等），治理测试 0.0–0.16s 是最快的几个；单跑/10 连/-race 全绿，无死锁。3.1-3.4 三锚凭据齐；StateSeat concept 绑定落 architecture.json。生产接线 host.go:575/624/799 真挂（脱离测试态）。
- **P4（02+06 合波）部分**：冷归档引擎核心落地（6.1/6.3 部分，5 正锚绿＋-race×10，搬迁后热层键 33→4，IndexEntries=1，EventCount=15 不变，保序钉绿）；**S1/S2 仍红**（生产接线未开）。能力形状偏离：以单一 RetireFacts(ctx,live) 替代 EnumerateKeys/Tombstone/ReclaimTombstoned 三方法（排他安静点内一步原子，避「已标记未搬」中间态）——编排者认可（合 D6.12 甲无中间态）。
- **编排者亲验**：go build exit 0；cold archive 5 测试 ok；governance ok（0.843s）；S1 仍红（预期）。
- **P4 设计阻塞②A（重大取向，停点待用户裁）**：冷/热两 journal 缺全局提交序——搬迁后热索引不点名已搬迁事实，EachIndexNewestFirst/NewestOfType 7 处生产消费者（checkpoint.go:829/865/985、run.go:2315/2576、read.go:114/280/317）需全局新→旧序。甲＝{pid}:idx: 值增单调提交序号（写路径格式变更，热冷按序号合序）；乙＝扩存活权威含所有 newest-first 所需事实（等于不搬迁，省不住字节）。编排者倾向甲。
- **编排者独立裁**：②B S1 夹具改走退位路径（同一 os.Stat 字节判据＋同一分母，验收面不缩，只改驱动路径——W0 夹具仅 StoreEvent 无真折叠/结清，驱动不了存活权威）；②C 装配层对耐久档默认开启退位（ledger 层默认仍关闭，spec 场景「ledger 未声明只追加」保留，S2 经装配默认开启转绿）；④3 wiki「六切片」→七（增 governance）归 05 契约同步；③3 seated-book 误接无生产触发路径，登记残余风险＋补 fail-loud 小闸；③2 architecture.json 共用写面 P2-fix/P4 各写本域绑定无冲突。
- **待用户裁**：②A 冷/热全序（甲单调序号/乙扩存活权威）；④1 03·3.3 BindStore 生产调用方口径（实际经 ledger.Bind/goals.Bind，BindStore 退化为兼容面；改任务行口径 or 退役 BindStore，涉验收面须用户裁）。

### R4 用户裁定＋追问

- **④1 BindStore＝用户裁「没用的就删」**：BindStore 兼容面生产 0 调用→退役删除（归 05 死码清理）；03·3.3 验收口径改为「ledger.Bind/goals.Bind 生产调用方≥1」（实际生产路径，验收意图不变）。
- **②A 用户追问「为什么退位后留 ADE」**：用户要先理解存活权威（liveness）再裁，不催决。编排者讲解中。

### R5 用户反问确立＋计划确认（D6.13/D6.14）

- **用户反问确立正确性/性能分层**：「真的不行吗？」——冷层透明可读时搬幸存者/blob/欠账都不破正确性，只影响读快慢。故**正确性＝identity-read＋全局提交序（甲）**；**存活权威是性能层非正确性门**；**退位判据简化为水位制**（水位前已结全搬冷、尾留热），存活权威降级可选性能优化（先不做）。落 design D6.13。
- **②A 裁定甲**（单调提交序号，唯一可行解）；**④1 BindStore 删**（D6.14，没用的就删，归 05）。
- **计划确认**：W0✅ → W1（01+04 部分）→ W2（02+06 合波：全局 seq＋水位制退位＋identity-read＋排他安静点＋脚手架接线 ∥ 03✅）→ W3（05 契约/死码）→ W4（F1–F4，归档/commit/push 须用户明示）。
- **再派发**：P4-cont（02+06 合波收尾，按 D6.13 水位制＋甲全局 seq）∥ P5-prep（05 spec 草案＋BindStore 删，disjoint）。

### R6 P4-cont 战报（02+06 收尾，按 D6.13）

- **① 全局提交序（甲）落地**：`{pid}:idx:` 值 JSON 增 `commit`（`segmentIndexCarried`）；`StoreEvent` 分配单调序号（`s.commitSeq+1`），`Load` 由 `len(published)+retired` 与已发布条目的序号取高水位（缺窗 meta 的条目不让序号回退）；`eachIndexEntryNewestFirst` 按序号归并热冷两序，`NewestOfType`/`EachIndexNewestFirst` 同径；`settled:commit` 记已搬最大序号，boot 在热层头越过该界时不打开冷层（冷层仍是懒开）。**凭据**：`go test ./memory/ledger/ -run 'TestColdArchive|TestColdTierReadSurvivesReopen|TestEachIndexNewestFirstStreamsTheCommitOrder|TestJournalStatesItsCommitOrderNewestFirst' -count=1` exit 0；新增锚 `TestColdArchiveRanksTheSettledCommitsBesideTheResidentOnes`（热层留最旧 fold、冷层持更新 fold → `NewestOfType` 答冷层那条，走查序三键全序；重启后热层头越过界 → `s.cold == nil` 证不打开冷层）。
- **② 水位制退位（D6.13）**：`RetireFacts(ctx, keepKeys)` 语义归正为「保留水位后的尾键集，其余事件族搬冷」（原 `live` 更名 `keep`）；`GetEvent`/`eventByKey`/`EachEvent`/`EachIndexNewestFirst` 冷层读与遍历已通。**原文族按 D6.13 边界未搬**（见下）。
- **③ 排他安静点**：`Run.settleGate`（提交读锁／退位写锁）＋`relocateSettledFacts`（写锁→`flight.await` 归零→读尾键集→`RetireFacts`）。**实测死锁与修正**：闸初置 `commitFactNoCadence`（派发单原口径）→ 全量 assembly 10 分钟超时 dump（`relocateSettledFacts`→`commitFence.await` 永不归零），根因＝该入口被「落链事实的结算」在闸内重入，Go RWMutex 有写者待时新读者阻塞 → 闸移到最外层 `commitFactChecked`，闸内重入不再等闸。**回写派发单口径**（R29 漂移）：闸位＝fats.go:84 原文与实现不符，实际落 facts.go `commitFactChecked`。
- **④ 装配接线**：`relocatableStorage` 对 journal/localfile 耐久档默认开启退位（②C，ledger 零值仍关）；`keptHotKeys` 以「尾（seq>offset）＋每条链最新 checkpoint＋框架 checkpoint（走到即停）」为保留集。**⑥ 未做**：lifecycle/retention/guard 接线与义务租约（原 6.4）未动。
- **⑤ 验收**：**S1 转绿（红→绿两次）**——`go test ./tests/integration/history/ -run '^TestJournalBytesBoundedByLiveSet$' -count=1` exit 0；夹具改走退位路径（`RetireFacts: true`＋每代 `RetireFacts(kept)`），同一 os.Stat 字节判据、同一分母（末代追加字节），实测 committed=[15822 31716 31126 31266 31266]、retained=[15822 15232 15372 15372 15372]、appended≈15894 恒定，bound(liveBytes×2=31788) 通过；红证据（临时使 `RetireFacts` 空转＝能力缺席）retained=[15822 31716 47610 63504 79398] 线性、失败信息点名 79398 vs 15894；身份读红线在同夹具内断言（第 0 代已搬事实按身份读回原字节）。
- **S2 部分转绿（indexEntries 转绿，journalBytes/durableLoadCount 仍红）**：`go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$'` 退位开 → `journalBytes=[0 75465 169010 281113 411748] indexEntries=[0 2 2 2 2] durableLoadCount=[0 12 24 36 48] wallClock` 平坦；退位关（回滚）→ `journalBytes=[0 134331 326123 557274 827666] indexEntries=[0 43 94 145 196]`。**红项归属（实测取证，非猜测）**：热点 journal 字节按族分解（三代根，`blob:` 145 键 94398 字节＋`ref:` 145 键 38710 字节＝79%；`16:` 事件六键 44026；`tagent/task/v1/*` 39 键 19320）⇒ ①原文族（`ref:`/`blob:`）不在搬迁面（06·6.3 未接），②task 耐久族随账户线性增长且属 01 托管范围（01·1.2 load 改读切片未落地，durableLoadCount 12/代即其证据）。**二者均不在本派发单白名单可达范围**（01 域禁触；原文族属 06 未接面），故 S2 不能在本单转绿——待编排者裁定：派 01 收尾＋06 原文族。
- **质量门**：`go build ./...` exit 0；`go vet ./assembly/ ./memory/ledger/` exit 0；`go test ./memory/ledger/ -count=1` ok 22.1s；`go test ./tests/integration/history/ -count=1` ok 16.6s；`go test ./assembly/ -count=1` 见 R6 核销（编排者复跑）。
- **白名单核**：改 `memory/ledger/{segment_store,journal_kv,config,journal}.go`、`assembly/{run,facts,checkpoint}.go`、`memory/ledger/cold_archive_test.go`、`tests/integration/history/symptom_anchor_journal_test.go`；未触 governance 面、config-surface 占用 4 测试文件、checkpoint_test.go；未 commit。
- **残留风险**：退位开启后 `EachIndexNewestFirst` 在 checkpoint 每次落盘时走全会史（含冷层），是「显式全史读消费者」成本而非 boot 成本，但 6.4 的 Retention/Guard 未接即无节流；`ErrColdArchiveNotQuiet` 与停写超时未实现（负锚待补）。

### R7 重制计划（用户裁「重制计划，一笔做完，工作次序你定」）

- **核心重框定**：三挂点动态拓展性为框架核心、作 golden 守护（D6.15）；压缩-投影-召回拉回三挂点轨道。退位/checkpoint 有界是轨道上一个结果。工作次序＝甲（先立守护判据 W1，再修特性收敛）。
- **域重编号＋新增**：旧 01→05 task-custody、02→06 bounded-open-index、03→07 governance-custody、04→09 acceptance、05→10 contract-sync、06→08 fact-retirement（目录已 mv，内容沿用）。新增 01 three-hooks-golden、02 plugin-dynamic-mount、03 compression-configurable、04 midband-ticket-recall（四件套已落盘）。
- **波次**：W0 启动门（S1/S2/S3 红＋golden 门红）→ W1 01 守护 → W2 02 动态挂载 → W3 03+04 合单 ∥ 05+06+07 boot 簇合单 → W4 08 退位合波 → W5 09 验收 ∥ 10 契约 → W6 F1–F4。
- **代理实证纠偏**：①01 守护现树红集 7 处/3 文件（run.go:615/618、tools.go:250/253/256、host.go:709/728），按形态判非行号；新门 check_plugin_mounting.py＋plugin_mounts.json＋architecture.json mounting 节。②02 plugin-mounting 无既存主 spec→ADDED（非 MODIFIED）；01 扫描须排除插件自挂载文件（避自证）；ageFold/finalRound 相邻具名构造本域不修。③03 summary_budget_bytes 声明量（常量留作策略缺省）；摘要经 rl.NewLeaseModel＋withHostModelParams。④04 delta 增 RENAMED＋ADDED；拟导出 replay.TicketOf（须 concept_budget 绑定）；task-skeleton-compression:51/:74 既有漂移交 10。
- **待实施期核**：05-10 旧域内部编号引用需对齐新号（cosmetic）；03/04 共触 age_segments.go 合单串行；01 守护与 02 自挂载的扫描排除须冻结签名。
- **停点**：规划重制完成，实施待 W0 启动门；F4 归档＋commit/push 须用户明示（R31）。


### R8 W0/W1 核销（编排者亲验）＋P0 覆盖缺口

- **W0 启动门达成** ✅：S1 TestJournalBytesBoundedByLiveSet exit 1（journal 字节线性增，原文族 ref:/blob: 占热层 79%，夹具已并入原文族）；S2 TestSymptomAnchorBootCostFlatAcrossGenerations exit 1（boot 字节线性增）；S3 TestSymptomAnchorMidbandProcessRecalledByTicket exit 1（请求喂摘要无票据，recall_get 本身可取全文）。go build exit 0。
- **W1 01 golden 完成** ✅：check_plugin_mounting.py（R5a 形态判＋R5b plugins 存活四条件）＋plugin_mounts.json＋architecture.json mounting 节；13 例自测全过；现树 exit 1（10 错＝7 R5a mounts＋3 R5b）；lint.sh:127/128 接线。go build exit 0。
- **P0 覆盖缺口（须裁）**：golden 门现树红集 7 处具名构造＝run.go:615/618（compress＋finalRound）、tools.go:250/253/256（ageFold＋observePlugin＋telemetry）、host.go:709/728（reflection＋settle）。**域 02 仅动态化 compress**，其余 6 处仍硬编码→P0 门不能全绿。需裁：全部动态化（完整 P0）vs compress 先行＋其余跟进。
- **既有红（非本变更引入）**：check_concept_budget exported_interfaces=84>83（StateSeat 等，03/07 域绑定后核）。


### R9 用户裁 P0 范围：全插件＋builtin 默认注册＋可自定义

- **用户裁定**：「全插件，但是需要有默认启动流程注册 builtin 插件，如果框架的使用者认为不需要，允许自定义拓展」。
- **落 D6.16**：7 处具名构造全部插件化（非仅 compress）；plugin_catalog.go 列 builtin＋boot 经注册表默认注册（装配根不具名构造）；plugins: 声明可禁用/覆盖/扩展 builtin。域 02 范围扩至全部 7 特性＋builtin 默认注册机制。
- **W2 派发**：域 02 实施（全插件化＋builtin catalog＋plugins 接通），目标 golden 门 R5a 7 处全绿＋R5b plugins 存活绿。


### R10 W2 核销（编排者亲验）：P0 golden 门全绿

- **W2 02 完成** ✅：check_plugin_mounting.py exit 0（mounts=0/consumers=1/errors=0，R5a 7 处＋R5b 全绿）；selftest exit 0；check_assembly_symbols exit 0；go build exit 0；builtin 挂载＋plugins 定制测试 ok。
- **builtin 机制**：plugin_catalog.go＋compress_mount.go，boot 经注册表默认注册 7 builtin（compress/finalRound/ageFold/observePlugin/telemetry/reflection/settle）；plugins: 禁用式定制（缺省全挂，-name 卸载，贡献实例可扩）。装配根不具名构造。
- **他域在途红项（非 02）**：TestCompressProductRestoresAfterRestart（域 06 退位接线在途）；S2 症状锚（设计红，待 05/06）。
- **W3 派发**：03+04 合单（压缩可配置＋中段票据，共触 age_segments）∥ 05+06+07 合单（task/index/governance 托管，boot 簇）。


### R11 W3 部分核销（两代理撞 150-turn，主线程诊断）

- **build green** ✅（代理保持可编译）。**S2 boot 有界转绿** ✅（05/06/07 recovery 核心完成）；**S3 中段票据转绿** ✅（04 核心完成）；S1 仍红（08 退位 W4）。
- **05+06+07 全绿** ✅：runtime/state/runtime/task/memory/ledger/governance 包测试全 ok。recovery 托管完成。
- **03+04 剩余**：memory/replay 2 红＝TestFoldPlanGolden（golden 漂移，提示 -update-fold-golden 仅限意图规则变更）＋TestAgeFoldFoldsTheMiddleBandIntoOneProduct（中段票据格式变更致旧断言漂移）。tool/recall/api/config 绿。需核意图后更新 golden/测试＋域 03 压缩可配置核验＋级联。
- **窄派**：03+04 收尾单（核意图→更新 golden/测试→域 03 核验→级联）。


### R12 W3 核销完成（窄派两单收口）

- **concept_budget 全绿** ✅：6 新类型绑定（StatedConsumption/StatedAttempt/StatedRefusal/Receipts→declaration/task-registry-rebuild；SettleAccount→read；RelocateReport→declaration/event-segment-store）；SettleAccount canonical 范例＋红线；棘轮按名抬升 AgentSpec 24→25/StatedTask 13→16/StorageConfig 6/new_max 47→54/exported_interfaces 83→84（理由注记）。check_concept_budget exit 0，自测 56 OK。
- **票据漂移收口** ✅：history 3 漂移（TestEventIntegrityProjection/RestoreUse/ProjectionReplayAndBudget）转绿（presentedTicket/withoutPresentedTicket 钉票据＋剥票比对原文）；host 呈现断言已在树内改妥（0 需改）；S3 保持绿。
- **W3 完成**：P0 golden 绿＋S2/S3 绿＋recovery 绿＋memory/replay 绿＋concept_budget 绿＋漂移收口。
- **剩余红归 W4/他域**：S1（W4 退位）；TestCompressProductRestoresAfterRestart＋TestSoakContinuity（退位/恢复在途，W4）；TestGateFailsClosedWithoutAttention（审批 boot 读目录，07 收尾核）。
- **W4 派发**：08 fact-retirement（全局序＋冷层 identity-read＋排他安静点＋脚手架），目标 S1 绿＋退位相关红转绿。


### R13 W4 核心完成：S1/S2/S3 全绿（里程碑）

- **S1 journal 有界转绿** ✅（exit 0）；S2 boot 有界绿；S3 中段票据绿。**三症状锚全绿**。P0 golden 门绿。build 绿。memory/ledger 绿（19s）。
- 退位核心完成（全局序＋冷层 identity-read＋排他安静点＋脚手架，W4 代理撞 limit 前 S1 已绿，回归扫尾未尽）。
- **剩余**：W4 回归扫尾（TestSoakContinuity 崩溃恢复 ErrSpecMissing、TestGateFailsClosedWithoutAttention 审批 boot 读目录、全量回归）；W5 09 验收 DoD＋10 契约同步（task-skeleton-compression:74 MUST NOT LLM 反向、event-key-visibility 转正、死码）；W6 F1–F4（归档/commit/push 待用户明示）。
- **窄派**：W4 回归扫尾 ∥ W5-10 契约同步（disjoint）。


### R14 W4/W5 收口＋doc_budget 具名上调

- **W4 回归扫尾完成** ✅：修两真缺陷（退位窗口键枚举 retireSettled(settle) 引擎自推 keep；RestoreBoot 不可读目录书空＋DeliveryGap 记名 boot 不死）。TestSoakContinuity/TestGateFailsClosedWithoutAttention/TestCompressProductRestoresAfterRestart 全绿。全量 assembly(295s)/host(213s)/memory/runtime 绿。S1/S2/S3 全绿。
- **W5-10 契约同步** ✅：6 主 spec 同步（task-skeleton-compression/event-key-visibility/session-checkpoint-material/compression-pipeline/compaction-scheduler 退役/deterministic-compress-level）；openspec validate 151/0；spec_hygiene/doc_links/concept_budget 绿；BindStore 零残留、无死生产者。
- **doc_budget 具名上调** ✅：21998→22153（+155，6 权威 spec 同步实现的合法增长，循 M4 先例，理由注记）。check_doc_budget exit 0。
- **剩余收尾**：08 冻结锚补齐（EnumerateKeys/Tombstone/ReclaimTombstoned 与 RetireFacts 单能力 reconcile、独占闸锚、recover-kv 消费者锚、S1 回滚负锚＝F3 红转绿证据）；10 余项（RebuildProjectionFromWAL 家族删——4 文件 config-surface 占用待其后、compression-recovery-ticket superseded 需先解 Stateful 绑定）；W6 F1–F4（全量门＋DoD＋归档，归档/commit/push 待用户明示）。


### R15 F3 证据完成＋全门绿，抵 F4 停点

- **F3 红转绿两次证据** ✅（隔离副本 /tmp/recovery-f3/tree，主树 md5 零写入）：S1/S2/S3 当前树 exit 0、回滚 exit 1、复原 exit 0；红因 behavioral（S1 退位空转字节线性、S2 全史重放、S3 摘要无票据）。run-notes.md §F3/§08（62 行）。
- **08 锚对账** ✅：能力形状＝RetireFacts 单能力＋identity-read＋排他安静点（三方法枚举/分页负锚具名退出验收面）；新增 TestColdArchiveHoldsTheExclusiveGate（-race×10）＋TestRecoverKVReadsARelocatedRoot（真跑 recover-kv）。memory/ledger/history 绿。
- **全门绿** ✅：build/concept_budget/spec_hygiene/doc_links/doc_budget/plugin_mounting/window_bounds/schema_allowlist/openspec validate 全 exit 0；S1/S2/S3 绿。
- **一级 F1/F2/F3 勾** ✅；F2 远端模块待部署核验；F4 待用户明示。
- **余项（不阻 F4 核心，登记）**：08·6.4 脚手架 oracle 接线；10 RebuildProjectionFromWAL 家族删（4 文件 config-surface 占用，待其后）＋compression-recovery-ticket superseded（先解 Stateful 绑定）；远端 examples/wechat-bot 长跑核验。
- **停点（R31）**：F4 归档＋commit/push 须用户明示授权，未执行。config-surface-normalization F4 授权不被本变更 extrapolate。

### R16 规划重整与完成结论撤销（当前）

- 先前归档后因PR #39持续失败已移回进行中；原F1/F2/F3与W阶段总体完成结论不再有效，当前tasks已重新打开。旧测试输出和R0–R15保留为历史，不能用其推导当前放行。
- 证据源固定为HEAD9547faca、CI runs43706847576/43706843157及代码读取。大窗口首个失败是带票据结算通知未被测试模型识别；零族键另属证据口径。小窗口同量输入/启动/重绑定随历史增长已证实，热点份额未测。重复压缩产物和Scenario03失败未定位，不标预存flaky。
- 撤销以下无根据判断：原S2没测时间；2x是原冻结墙钟标准；新键之外全是在飞重试且旧键永不重交；每次冷查询都重放全库；写了索引切片就已O(1)；13个检查器自测证明完备；wrapper或工作流作业名等于合并硬门。
- 保留原P0–P8和S1/S2/S3；新增G1整事实提交/G2当前态与消费/G3引擎成本/G4实际证据放行设计门。新增11/12/13子域，现有05–10补.R叶；新增设计是候选，未伪称用户已冻结签名。
- 本轮只修改规划和当前验收账；三份文档单A(06/08/11)、B(05/07/12)、C(09/10/13)并发，主线程一级与01–04。没有实施、重新归档或发布。
- 规划收口须独立审查目标覆盖、现有与新增测试区分、前置依赖、未知数与放行证据；新增测试未建立与G1–G4未通过时，仅形成规划初稿，不宣称实施已就绪。

### R17 独立规划审查第一轮及文档修订

- 审查结论：骨架可用，但原稿有六处阻断（领域等价与字节等价冲突、启动/整代计量混合且禁止合法前后对照、共享测试文件误判并发及G2循环、no-prefix依赖成环、旧项缺实施承接、归档入口可绕过）。不因OpenSpec结构通过宣告放行。
- 修订：11以SameFact定义领域重复并保留原字节，不确定与确定拒绝分开；09/13区分启动/整代/独占区间及A/B各自源码；06/11撤回并发许可，12.R1产出G2而非依赖G2；04.R0具名产出无环交接排序；10.R4实施旧项清理并合并同能力Requirement；13要求实际归档文件树变化由提交/服务端必需检查拒绝，不靠包装命令自觉调用。
- 以上是文档修正，待新一轮独立审查；本轮未实施、未跑完整产品验收，不更新任何功能为完成。

### R18 后续规划复核（2026-11-16）

- 第二轮另发现四处准备层阻断：G1前置索要生产合一后结果；09任务行残留旧计量/A-B口径；08租约由keep成员关系自证；10处置清单索要后续清理完成。逐项改为现状/原型可行性与后续实现验收分立，按真实消费者与成本决定留删，清理由10.R4承担。
- 第三轮独立只读审者确认「设计验证准备层 frontier 空」；扫描一级当前规划及03/04/06/08/09/10/11/12/13当前叶，确认首批取证/最小反例/设计原型不依赖后续生产实现。该结论只放行设计验证准备，不代表G1–G4已经通过或产品可以发布。
- 当前13子域、33个.R叶。原01–10成果与历史记录保留，新增11事实完整提交、12消费者状态生命周期、13真实证据放行。主规格、生产代码、脚本和外部权限本轮未改。
- 未验证项目：G1–G4、拟新增测试的实际红绿、最终候选全量矩阵、真实保护/权限接入、归档与发布。F1–F4全部保持未完成。

### R19 工作树候选盘点与四项裁定（2026-11-16，派发前对齐）

派发前重读二节时发现工作树非干净基线：HEAD `9547faca` 之上有 13 个未提交文件（`git diff --stat` 530 insertions）。逐项读码核实后裁定如下。**候选未被任何设计门判过，本节不把它当已冻结契约。**

**保留（真进展，待 G3 量）**：`memory/ledger/journal.go:587` 退位趟末给冷档放索引切片（`placeIndexCut`），配套新锚 `TestColdOpenReadsOnlyPastTheIndexCut`（`journal_index_cut_test.go:246`）带真负例（删切片→整档重放，:179-189）。这是「冷层开档随已搬历史无界增长」的候选解，方向对。**未判项**：`cutOf()`（journal.go:595）建整张索引映射、`writeJournalIndexCut` 整表 JSON 序列化，故每趟退位付 O(冷键数)——正是①最大风险「把正文全扫换成整索引全扫」。G3 必须量这一笔，不得因开档转平就判整体有界。

**N1 伪造裁决（成立，须改）**：`assembly/symptom_anchor_recovery_test.go:405` 注释称 2x 是「the measure the definition of done names」，:448/:463/:476 三处硬编码 `= 2`。但本变更 [design.md:162](design.md) 与 [proposal.md:42](proposal.md) 明写「未经校准的 2x 不是冻结裁决」，DoD 从未命名 2x。该注释把作者自定的倍数冒充用户裁决，属循环自证。**裁定**：删该断言性注释；2x 降为待校准候选值，由 09.R2 同条件校准与回归反例确定；不得以「DoD 命名」为据。S2 判据面从 boot 扩到整代（:458-484）这一项**保留**——它是对的，成本确实住在提交与退位而非 assemble。

**N2 自证结构（成立，须删；归因经 R20 勘误加强）**：候选给 `state.FactLog` 加了第五方法 `Keys`（`runtime/state/durable.go:43`）＋第五条红线（:33）＋`enumerateFamiliesLocked`（:470）＋`LoadStats.Families`，并给 `TierFacts` 加 `Keys`（`memory/ledger/tier_facts.go:63`）。全仓 grep 实证：`Families` 的生产消费者只有 `assembly/run.go:708-710` 一行 `assemblyInfo` 日志；S2 的 `durableLoadCount` 读的是 `Scans`（`symptom_anchor_recovery_test.go:113`），**不读 Families**；`TierFacts.Keys` 的唯一引用是 `skeleton_assert.go:120` 的形状断言（自指）。~~无独立第二使用方~~ **【R20 勘误】该结构确有一个外部消费者：CI 的 `durable_scan` attestation 读这行日志。故不是「零消费者」，而是「唯一消费者是一个已被本变更证伪的陈旧代理」——见 N6。结论不变且更强：喂它库存计数恰好 defeating 该代理的本意。** 且它给每次 boot 加 5 趟索引枚举——本变更要压的正是 boot 成本。**裁定删**：`FactLog.Keys`／红线#5／`enumerateFamiliesLocked`／`LoadStats.Families`／`TierFacts.Keys`／`skeleton_assert.go:120`／四个测试替身的 Keys（`durable_test.go:592`、`:1218`、`host_delivery_test.go:1170`、`examples/contract/runtime_state/FactLog_example.go:47`）／`run.go:708-710` 的 Families 拼接。**明确不动** `AtomicStore.Keys`：它有独立生产消费者（`migrate.go:87/139`、`rustviking_kv.go:128`、`segment_store.go:983`、`scripts/recover-kv/main.go:107/183/187`），是既有合法面。实施归 10.R4，**attestation 重写归 13.R2＋09.R2，两者必须同批**（只删结构会让 CI-5 继续红，只改 attestation 会留下自证结构）。

**N3 重复计量（成立，须修）**：`memory/ledger/segment_store.go:805` 的 `defer` 把整个 `coldEventLocked` 的耗时计入 `coldReadCost`，而该窗口包住了 `coldViewLocked`，后者在 :791 已把开档耗时计过一遍。开档时间被计两次，S2 却拿这个数在 :448 判 2x。**这正是 09.R2 已冻结的负锚 `TestColdReadCostCountsEachReadOnce` 的目标**，现取得源码实证。另 :939 只累加 `RelocateReport.ColdOpenCost`，退位的 cut 写不计——09.R2 另一负锚 `TestRetirementPassChargesItsCutWrite` 的目标，与上述保留项的 O(冷键数) 是同一笔。

**N4 编辑事故（成立，须修）**：`assembly/commit.go:358` 函数体与下一段注释粘连成一行（`return nil }// commitLedger is ...`）。comment_policy 面，随候选处置单一并修。

**N5 过程件散落工作树（R26，须逐个定性）**：未跟踪 `assembly/tmp_profile_cost_test.go`、`assembly/retirement_scan_repro_test.go`、`memory/ledger/retirement_scan_repro_test.go`、`assembly/presentation_symptom_test.go`。逐个判「正式锚（留并入库）／探针（移仓库外）」，不得默认全留或全删。**外来未跟踪文件不动**（`runtime-architecture-convergence/`、`check_event_semantics_symptoms.py`、`runtime_budgets.py`、`test_check_runtime_symptoms.py`、`skills/interview/agent-interview-helper/`）。

**派发影响**：`assembly/`、`memory/ledger/`、`runtime/state/` 三面在候选处置单落地前只读（见二节授权）。故本轮 Wave A 只派不写这三面的叶；03.R1（写 `assembly/compress_*.go`）与候选处置单串行，排在 Wave B。

### R20 Wave A 单A（09.R1）核销＋N6 陈旧代理（2026-11-16）

**完成度五查（编排者亲验，凡报必验 R7）**：
- 查1 勾选真实性 ✅：09.R1 **未勾**，`tasks.md` mtime 00:19 未动。未勾理由成立——消费者锚要求 04.R1 任务行按 `CI-1`/`CI-4` 引用行，而该文件不在单A写集，单A没有越权去改。这是正确纪律，不是遗漏。
- 查2 产物盘点 ✅：`09/design.md` §八 落盘（:69 标题、:95 源树绑定、:106 失败清单、:188 未验证项），行 id `CI-1`…`CI-8` 齐。写集未越界。
- 查3 数字溯源 ✅ **两处关键声称我独立复跑核实**：①`git show 9547faca:examples/wechat-bot/smoke_test.go` 第 3571 行原文确为 `smokeLongHostChild(t)`、3578 为 `smokeLongPrimary(t)`，均是调用点非断言；②`smoke_harness_test.go:4149` 确为 `t.Fatalf("answer generation %d session %s round %d: %v", …)`，与制品失败原文逐字相符。勘误成立。
- 查4 遗漏检测 ✅：未验证项如实登记三条（7/42 两片只到作业日志级、CI-5 归属未定位、旧行号保留为历史注记），未按已核处理。
- 查5 生产消费者实测 ✅：源树绑定不是声称而是复算——`git archive 9547faca examples/wechat-bot` 后按 `run_runtime_scenarios.py:_fingerprint_entries` 同算法重算 sha256，得 `baf3fdc5…fb8d233`，与制品 `content_hash` 逐字节相等；制品 `revision=b04a945e` 是 GitHub 为 pull_request 合成的合并提交、本地无此对象，故**不以 revision 声称等于 HEAD**。这正是二节源树绑定要求的三件分开记。

**单A 带回的三项新情报（已核，下传后续单）**：
1. **报位≠断言站点**：`t.Helper()` 使失败报位冒泡到调用点。CI 制品里的 `smoke_test.go:3571` 是冒泡后的位置，真断言在 `smoke_harness_test.go:4149`。04.R1 若按制品报位去找断言会扑空——已写入 §八 CI-4【勘误】，单C 读该节即得。
2. **`test` 作业后续步骤本次 skipped**：`Test (short)` 先红，其后 bot/cli-host 两模块、execution witnesses、`Lint` 全部 skipped。故**本 run 无 lint 绿证据**，原行「同批已跑」是误读，已勘误。任何「CI lint 绿」的声称不得引本 run。
3. **退出码分层**：CI-3 的 124 是内层 `go test` 码（`liveness.state=expired`），外层步骤报 1；两者不得混写。256 三片是超时、32768 是断言红，两类不互抵、不得据此称 flaky。

**N6 陈旧代理（成立，新发现，CI 强烈暗示的第三个遗漏问题）**：
- **现象**：CI-5 `durable_scan: unattested`，`loaded zero keys for ['spec:','consume:','fact:']`，`gens_without_durable_scan=8/9`（gen01–gen08 一条扫描日志都没有）。
- **判据原文**（`scripts/scenarios/run_runtime_scenarios.py:907-947`）：本意是「Attest that a shard's restarts really reloaded the durable chain」；代理是数 `tagent: durable load scan prefix=… keys=N` 日志行，要求五前缀齐全且三个数据前缀 keys>0。
- **根因**：检查点托管后的 boot 按身份读任务，**不再整族扫描**，故不产生 `Scans` 行。代理量的是本变更已经退役的机制。「非零族扫描键数 ⇒ 重启真重读了链」这条断言在新 boot 下**已被证伪**（元规则一）。
- **候选的错误修法**：`enumerateFamiliesLocked`（N2）让每次 boot 补发五条**库存计数**行，代理遂转绿。但库存计数不证明 boot 重读了任何东西——一个什么都没托管的 boot 也能发这五行。**满足代理的字面、 defeating 代理的本意**，正是「库存键数量不冒充恢复读取量」与「让检查器相信手填结果」两个禁令的交点。
- **裁定**：N2 删结构 ＋ **attestation 按新 boot 的真实可观测后果重写**（重启真重读了链的证据是它托管了哪些任务、按身份读回了什么，不是它扫了多少族）。两者**必须同批**：只删结构 CI-5 继续红，只改 attestation 留下自证结构。归属：结构删除 10.R4，量名与区间 09.R2，证据格式与拒绝面 13.R2。
- **须配负锚**：重写后的 attestation 必须能拒「boot 未托管任何任务却发满五行」与「制品丢失 genNN.log」两种假绿；沿用现有 `missing prefix line` 拒绝面不足以覆盖新形态。此负锚归 13.R2，拟新增、尚未建立。

**下传 03.R1 的情报（编排者读码所得，非猜测）**：CI-1 与 CI-2 是同一个测试 `TestRestartExecutesTheChainJudgmentOnce`，但**失败行不同**——`test` 作业红在 `assembly/recovery_judgment_test.go:117`（第二次提交后 2 个产物），`race` 作业红在 `:110`（重启后第一次提交即 2 个产物）。同一输入在两作业里重复产物出现的时机不同，说明这是**顺序敏感**而非纯确定性逻辑错，且无 DATA RACE 报告故不是数据竞态。该测试 :93 先断言「任何提交前产物数为 0」，故重复产物是在执行待判定的那次提交里产生的。03.R1 定位时须解释这个时机差，不得只给出一个「重复执行」的笼统归因；也不得因两作业行号不同就判 flaky。

### R21 Wave A 单C（04.R1）核销＋N7/N8/N9（2026-11-16）

**完成度五查（编排者亲验）**：
- 查1 勾选真实性 ✅ **两条锚我亲自复跑**：正/负锚 `go test -C examples/wechat-bot -tags smoke . -run '^(TestSettlementNoticeRecognizesPresentedFact|TestSettlementNoticeRejectsOtherCallMaterial)$' -count=10 -v` → **exit 0，90 RUN／90 PASS／0 FAIL**（非空转成立）；消费者锚 `go test ./tests/integration/acceptance -run '^TestScenario03ToolReceiptAndBackground$' -count=10 -v` → **exit 0，10 PASS／0 FAIL**。04.R1 已勾，勾得实。
- 查2 产物盘点 ✅：锚落在 `smoke_test.go:4745/4780`，helper `smokeSettleNoticeParts:556`／`smokePresentedMaterial:572`；`scenario_s03_test.go:349` `scenarioPresentedMaterial`、`:424` 新消费者锚。写集未越界（`assembly/`／`memory/ledger/`／`runtime/state/` 零写，`smoke_harness_test.go` 未改）。
- 查3 数字溯源 ✅ **一处量级差我追查后排除假绿**：消费者锚十次总耗时 0.364s，而 CI 失败版本耗时 15.00s。读码确认 `scenario_s03_test.go:97` 调 `scenarioAwaitBackgrounds`——失败时烧满等待预算（CI 的 15s 即此），成功即首轮返回。日志含真实 model response、逐次不同的 task 身份、完整 TAGENT-SCENARIO-RECORD，故是真跑不是空转。**量级差有解释，不是匹配器放宽。**
- 查4 遗漏检测 ✅：未执行 `TestEventIntegrityLongRun` 长矩阵并显式声明（归 09.R3），未冒充已跑。
- 查5 生产消费者实测 ✅：匹配器按「票据地址整 token 切＋材料逐字相等」判，不是宽松包含；`scenarioBackgroundOf:332` 先按 `message.ToolID` 认身份再比材料，故「别次调用的票据」「仅提及正文」均被拒（4 个反向子例全 PASS）。红证由最终落点回滚演练取得（`smoke_test.go:4766` 与 `scenario_s03_test.go:453`），复原后零残留。

**偏离裁定：批准。** 拟新增的 `examples/wechat-bot/settlement_presentation_test.go` 未落地，改入已跟踪的 `smoke_test.go`。理由经我独立实证成立（见 N7），锚名与命令逐字未变，验收面未缩水。

**N7 白名单忽略模式静默吞掉新测试文件（机制缺口，硬约束#11 形态）**：`examples/wechat-bot/.gitignore:5` 是 `*`（白名单模式，:1 注释自陈）。`git check-ignore -v examples/wechat-bot/settlement_presentation_test.go` → 命中 `:5:*`，exit 0。**即在该模块新建任何未命名的源文件都永不入库，CI 永不跑它，而本地 `go test` 照跑照绿**——这是一个静默假绿通道。现有 `scripts/gates/lint.sh:254` 的 `tracked-hygiene`（`scripts/codetools/hygiene.go:45 checkTrackedHygiene(files []string, …)`）按已跟踪文件核，覆盖「已跟踪⇒不得被忽略」，**不覆盖「白名单模块里的新源文件⇒必须被命名」**。裁定：①单C 的改道正确，不需补独立文件；②该缺口的机器门归 13.R2 判定与补（须先确认 `checkTrackedHygiene` 的 files 来源确为 `git ls-files`，不凭函数名断言）；③后续任何叶在该模块建文件，派发令必须显式要求「加白名单行＋git add」并单独授权。

**N8 重复投递续扫对带票据的重复盲（真缺口，须具名承接，不得溶解）**：`examples/wechat-bot/smoke_harness_test.go:4718` 的续扫仍是裸 `strings.HasPrefix(lasts[j], "tagent: task ")`，故**带票据的重复结算通知数不到**，`recon.NotificationRequests` 会少计。单C 正确识别并拒绝顺手改（需长矩阵才能验），但此项 MUST NOT 因此消失。裁定归 **04.R2**（该叶本就负责「对账所有呈现消费者」），且**不必等 110 分钟长矩阵**：续扫是对 `lasts []string` 的纯逻辑，可提单元级锚直接钉「带票据的重复须被计入」。对照：`:477` 用的是 `Contains` 故票据下仍命中，单C 未改是正确的。

**N9 注释过度声称（小残留，元规则一）**：`scenario_s03_test.go:344-345` 注释称票据「read as a whole so a key carrying any literal byte still splits at its own end」，但实现 `:354` 是 `strings.Index(rest, "]")`——取**首个** `]`，键内含 `]` 即提前切。生产键由 `mintEventID` 铸为 hex，不可构造该形态，故代码无缺陷、**注释是伪断言**。裁定：改注释为真话（按首个 `]` 切，键形态由铸造方保证不含 `]`），归候选处置单顺带修，不改实现。

**R19 文件数勘误**：R19 称「13 个未提交文件」，该数取自 `git diff --stat -- assembly memory examples runtime`，**漏了 `scripts/gates/check_event_integrity_mutations.py` 与 `scripts/gates/lint.sh`**（mtime 11-15 22:06/22:08，属更早会话的未提交改动，非本轮两单所写）。候选面实为 **15 个文件**。另：工作树 tracked 改动共 61 项，其余为 openspec 规划重整与解归档的文档面，不属生产候选。

### R22 Wave A 单B（13.R1）核销：G4＝FAIL（2026-11-16）

**完成度五查（编排者亲验）**：
- 查1 勾选真实性 ✅ **锚我亲自复跑**：`check_runtime_evidence.py`（无 `--evidence`）→ **exit 2**；`check_dod_coverage.py` → **exit 1**；`test_check_event_integrity_mutations.py` → **exit 0**。13.R1 已勾，R2/R3/R4 保持未勾（G4 FAIL 阻它们，正确）。
- 查3 数字溯源 ✅：「19 gap」核实——输出 20 行 `dod_coverage:`＝1 行表头 `19 gap(s)`＋19 项，逐项为「mutation 未登记」与「无 run 记录其为 killed」成对。
- 查2/查5 写集与消费者 ✅：`scripts/gates/*.py`、`lint.sh` mtime 全为 11-15 22:06–22:09，**单B 零写**；其动态探针只 import/读。三条洞的函数与 HEAD 逐字节相同（单B 以 `git diff` 核），而 `check_dod_coverage.py`＋`lint.sh` 接线＋`dod_predicates.json` 是**工作树独有**（未跟踪）——故 G4 结论绑工作树，CI 记录仍绑 HEAD，二者分开记，符合二节源树绑定。
- **三条洞我逐条读码确认，无一被驳回**：
  - **G4-1**（`check_event_integrity_mutations.py:932-934`）：`_ran_test` 只判 `Action=="run"` 且测试名相等，**不查 pass 终局、不查 skip**。控制树全跳过仍读作「真跑」。
  - **G4-2**（`:958-972`）：`_assertion_hit` 的 `related = test in failing or any(name.startswith(test+"/") …)`，故父测试因**任何**原因失败时，该测试任何 output 行含登记字样即算命中——含它自己的日志。杀死归属不是「该测试该断言的同一条失败记录」。
  - **G4-4**（`check_dod_coverage.py:455`）：`verdicts` 取自 `registry.get("mutation_verdicts")`，即**登记表手填**，不是 `check_event_integrity_mutations.py --out` 的运行产物。
- **消费者链断点（单B 追到，编排者采信其接线定位）**：产物由 `run_runtime_scenarios.py --out` 与 `check_event_integrity_mutations.py --out` 生成，但 `check_runtime_evidence.py --bundle` **只在 ci.yml:408 被消费**；而 bundle 与 `test` 作业**都不是必需检查**（`required_status_checks`＝ABSENT，我复核单B 的 `gh api` 结论一致），故红判决不阻合并。本地 `lint.sh:179/188` 只跑 `--check-contract`，从不跑证据门。归档侧 `archive -y`／直接 `mv` 无证据拒绝。

**G4 判决：FAIL。** 七项差距 G4-1…G4-7 已落 `13/design.md` §一（判决＋差距表）与 §三（动态确认），归属：G4-1/2/3/4→13.R2；G4-5/6/7→13.R3（G4-5 属外部权限，须明示授权，本轮只报「未生效」）。**检查器自测全绿（97/15/32/13 例）只证明已列反例被拒，不是产品验收，不覆盖规格全集**——此点单B 显式声明，编排者确认。

**Wave A 收口**：三单全部复核通过。09.R1 未勾（消费者锚待 04.R1 任务行按 CI 行号引用，属编排者单写的共享面）；04.R1 已勾（两条锚我复跑 exit 0）；13.R1 已勾（G4＝FAIL 是有效判决，不是叶失败）。**G1/G2/G3 仍未判，产品不可发布，F1–F4 全未完成。**

**Wave B 派发（触碰集不相交，2 单）**：单D1＝候选机械处置（N1/N2/N4/N5/N9，Go 面）；单E＝G4-1/2/3 三条假 killed 拒绝面（Python 面）。**只 2 单的解锁收益**：D1 写 `assembly/`＋`memory/ledger/`＋`runtime/state/`，E 写 `scripts/`，语言与构建闭包完全不相交；03.R1 与 D1 同属 `assembly` 包故必须等 D1，N6 与 09.R2 需先定「托管 boot 的真实可观测后果」故本轮不派。强行凑第三单会把同包叶并发，违 R21。

### R23 计划内依赖环解除＋09.R1 核销（2026-11-16，编排者单写共享面）

**发现环（编排者自查，非代理带回）**：09.R1 的消费者锚原写「表被 04.R1 与 **13.R2** 按行引用」；13.R2 前置含「09.R2 的量名/单位/区间定义」；09.R2 在二节次序表的前置是「09.R1 对账」。三者成环 **09.R1→13.R2→09.R2→09.R1**，任一叶都无法先勾。这是计划自身缺陷（R2 隐藏依赖形态），与 04.R0 要解的 no-prefix 跨变更环同类，但在本变更内部。

**解环**：把 09.R1 消费者锚的证据域消费点从**实施叶 13.R2** 移到**审计叶 13.R1**——13.R1 的前置本就是「09.R1 的失败清单已固定（证据格式的输入面）」，它是清单的真实消费者；13.R2 实施时仍须按行引用，但那是它自身核销项，不反阻本叶。**验收面未缩水**：清单仍须被证据域实际消费，只换消费点。已落 `09/tasks.md:20` 的 R23 注。

**补记引用（诚实标注）**：13.R1 产出未逐行标注引用（`grep CI-` 零命中）。编排者按已核证据在 `13/design.md §五` 补记映射表，**显式标注为编排者补记、非 13.R1 代理原始输出**：`CI-3`↔G4-1（产品侧缺终局已判 unattested，控制树侧 `_ran_test` 未守同一要求，正是要补的不对称）、`CI-5`↔N6＋G4-4、`CI-6`↔G4-5（门**会**具名拒绝，缺口在该作业不是必需检查，故判决能力与放行接线分开评价）。`CI-1`/`CI-2` 归 03.R1、`CI-4` 归 04.R1，本域不认领修复。

**09.R1 核销 ✅（编排者勾）**：正锚三条 `gh` 命令与表逐行相符（单A 复跑，编排者复核其源树绑定为 content_hash 复算而非声称）；负锚成立——256 超时（内层 124）与 32768 断言红（内层 1）分为 CI-3/CI-4 两类未合并，gen8 行同时给出请求正文原文与报位勘误（比原锚要求更强）；7/42 两片取到作业日志级原文，属负锚允许的「或作业日志」来源，行有效；消费者锚现由 04.R1（引 CI-4/CI-1）与 13 域（引 CI-3/CI-5/CI-6）双向成立。`openspec validate runtime-recovery-ideal --strict` → valid。

**就绪集更新**：09.R1 已勾 → **09.R2 解锁**（但其写集 `memory/ledger/segment_store_test.go`＋`assembly/symptom_anchor_recovery_test.go` 与单D1 同文件簇，故排在 D1 之后，不得并发）。13.R1 已勾但 G4＝FAIL → 13.R2 的三条假 killed 面由单E 先行实施（**不勾 13.R2**，其余绑定字段仍待 09.R2 量名定义）。

**N10 叶格式不一致使机器计数漏 21%（小残留，须修）**：编排者以 `^- \[[ x]\] [0-9]{2}\.R[0-9]` 数叶得 26，与计划声称的 33 不符。查证：05/07/12 三域的 7 叶用粗体格式 `- [ ] **05.R1 …**`，其余十域用裸格式 `- [ ] 05.R1 …`。**叶数确为 33，计划无误**，但任何按裸格式正则计数的机器门会静默漏掉 7/33（21%）——这正是「退出码零但零命中」同类的假绿通道。裁定：统一为裸格式（粗体移到标题文字内），或让计数门同时认两种形态并配「漏数即红」自测。归 10.R4 顺带修；**在修好前，任何叶计数声称必须附所用正则与逐域命中数**，不得只报总数。

### R24 Wave B 单D1 核销＋N11/N12（2026-11-16）

**完成度五查（编排者亲验）**：
- 查1/查2 ✅ **8 个文件对 HEAD 逐字节回落**（`git diff --stat HEAD -- <8 files>` 空）；生产残渣 grep（`Families|enumerateFamiliesLocked|factFamilyPrefixes|TierFacts\.Keys`，排除 _test.go）**零命中**；`AtomicStore.Keys`（atomic_store.go:55）与 7 个生产消费者完好，`go test ./runtime/state/ ./memory/ledger/ ./scripts/recover-kv/ -count=1` exit 0。N1 四个 2 一字未动、伪造 DoD 断言已删；N4 commit.go 回落 HEAD；N9 注释改述「split at the first `]`」＋铸键保证，实现未动。
- 查3 ✅ **S2 我在主树复跑**（探针移出后不再需要 overlay）：`go build ./...` exit 0、`go vet ./assembly/ ./memory/ledger/ ./runtime/state/` exit 0、三锚 exit 0／3 PASS。量逐字：`journalBytes=[0 110 110 110 110] indexEntries=[0 2 2 2 2] durableLoadCount=[0 0 0 0 0]`。
- 查4/查5 ✅ 未勾任何框（本单是编排者直管清理，非计划叶，正确）；未动 4 个探针（白名单外），用 overlay 量取并保 md5 一致——**白名单与「vet 须绿」冲突时选择不越权，是正确纪律**。

**编排者裁决：探针处置（可逆，不删）**。D1 的 N5 四路定性经我采信，处置如下：`assembly/retirement_scan_repro_test.go`（已破，引用被删的 `Families`，且断言的正是 N6 判为陈旧的代理）、`assembly/tmp_profile_cost_test.go`（`tmp_` 前缀、零断言、纯计时草稿）、`assembly/presentation_symptom_test.go`（与已跟踪 `presented_ticket_test.go:43` 核心断言重复，且注释引用别变更坐标）三者**已移至 `/tmp/recovery-probes/`**（移动非删除，可复原）。移出后主树 build/vet/S2 全绿，实证阻塞唯一来源即该探针。`memory/ledger/retirement_scan_repro_test.go` **保留**（真断言、不依赖被删符号、`go test ./memory/ledger/` exit 0），但它未跟踪且有 4 条注释 findings，须修形状后入库——**未跟踪的测试＝本地绿 CI 永不跑，属 N7 同类假绿通道**，登记待入库。

**N11 S2 的 durable 判定面恒真（假绿，与 N6 同根，须修）**：
- **实证**：`durableLoadCount=[0 0 0 0 0]`，而 :500 的判据是 `held > observed[1]*2` 即 `0 > 0`，**恒假故恒绿**。逐条核守卫：:366 `journalBytes==0`、:373 `indexEntries==0`、:376 `wallClock<=0`、:437 `journalBytes==0`、:440 `coldReadWallClock<=0` 各有具名失败，**唯独 durableLoadCount 无任何非零守卫**；`TestBootQuantityProbesReadTheRealBoot`（:364-387）同样只守前三量。故 S2 声称的「三确定量硬判定」实际只有两量在判。
- **归因勘误（D1 报告的一处偏差）**：D1 称「五个族行再也不会出现」暗示是其删除所致。编排者读码核实：`durableLoadCountOf:113` 读的是 `LoadStats().Scans`，而 `Scans` 只在 `durable.go:636` 的 `d.scan()`（整族扫路径）追加；托管 boot 按身份读、不走 `scan()`，故 `Scans` 在 D1 之前就已为空。**恒真面是 05 任务托管造成的，不是 N2 删除造成的**；N2 删的 `Families` 是 S2 从不读的另一字段。
- **与 N6 同根**：CI 的 `durable_scan` attestation 与 S2 的 `durableLoadCount` 量的是同一个已退役机制（整族扫描）。一处修复设计服务两处。
- **修法方向（编排者读码所得，可实施，不需新生产出口）**：零本身是**正确的有界行为**（检查点覆盖全部账户时 boot 确实一个 durable 键都不读）。缺的不是非零，而是**证明这个零是「非空账户上托管成功」而非「空存储什么都没读」**。`judgeBootFixture` 已核 `entry.accounted`（`durable.TaskIDs()`）逐代增长，故自证量已存在——S2 须把它提为判据前提：断言账户任务数 >0 且逐代增长，在此前提下读成本恒零才是有界证据。此守卫归 09.R2，与 N6 的 attestation 重写同批。

**N12 候选面引入 11 条注释 findings（CI 阻塞，HEAD 为 0）**：`lint.sh:197-203` 要求 `-no-baseline` 命中数**可解析且为 0**，否则 exit 1。当前 11 条，逐条经 `-v` 列出，**全在未提交候选面**（HEAD 经 c1a5d485 已清偿至 0）：`symptom_anchor_recovery_test.go:60/204/419`（doc-not-brief ×2、test-doc-line-too-long ×1）、`cold_archive_test.go:503`（free-standing）、`journal_index_cut_test.go:255/271/328`（too-long ×1、free-standing ×2）、`retirement_scan_repro_test.go:1/6/20/92`（unindexed-path-ref／missing-test-responsibility／doc-not-name-prefixed／test-doc-not-one-sentence）。**只改注释形状，不改任何断言与实现**。另：工具报 `examples/wechat-bot/knowledge_base` 是**被 git 忽略的 Go 文件、在任何 checkout 中都不存在**——N7 白名单缺口的真实实例，须定性（真源该入库／运行态该移走），归 13.R2 的门补强一并处理。

**就绪集更新**：D1 完成 → `assembly/` 面释放。**09.R2 解锁且可派**（09.R1 已勾）。下一单＝09.R2 计量校准（N3 双计/漏计＋N11 守卫＋七段口径＋阈值同条件校准）与 N12 注释清偿**合单**（共触 `cold_archive_test.go`／`symptom_anchor_recovery_test.go`，同文件簇必须合单）。03.R1 仍排其后（同属 assembly 包）。单E（Python）仍在跑，与 Go 面不相交。

### R25 Wave B 单E 核销：G4-1/3 闭、G4-2 部分闭（2026-11-16）

**完成度五查（编排者亲验）**：
- 查1/查3 ✅ **自测我复跑**：`python3 scripts/gates/test_check_event_integrity_mutations.py` → **exit 0，Ran 34 tests**（15→34 声称属实）。
- 查5 ✅ **`--replay` 新输入面的隔离经机制核实，非采信声称**：`check_event_integrity_mutations.py:1584` 回放时报告名为 `evidence.replay.json`；消费者 `check_runtime_evidence.py:2167-2182` 的 bundle 收集器**按文件名硬过滤**（`if name != "evidence.json"` 跳过，空目录具名报错）。故回放报告机制上进不了 bundle，不构成新假绿通道。单E 另加了 bundle 侧拒新判词的用例。
- 查2 ✅ 写集仅两个 Python 文件；`golden/`、`.github/`、`.go`、`check_dod_coverage.py`、`check_runtime_evidence.py`、`preflight.sh`、`lint.sh` 零写。**未跑 go**（因单F 在改 Go 面，任何 Go 结果不可信）——这是正确纪律，不是遗漏。

**逐条裁定（不整批采纳，也不整批驳回）**：
- **G4-1 控制树跳过＝闭** ✅。before exit 0／`verdict=green` → after exit 1／`red (skipped)`，判词含 `skipped/no_pass_terminal/zero_match` 三态，整轮变异不计数。
- **G4-3 补丁未进编译面＝部分闭** ⚠️。**构建标签形态有 CLI 级红绿对**（`//go:build ignore` → `not_applied`，具名指出标签不匹配）；**包闭包与跨模块两形态只有合成树单测**，单E 如实报告 CLI 层未复现（目标包经 6 文件传递可达，夹具 exit 0）。裁定：谓词已实现，但按「机制验证须完整＝穷举谓词×每谓词负变异」，**缺 CLI 级负例即该谓词证据不足**，登记为待补——须造一个选择器真不编译被改包的夹具，不得以合成单测冒充 CLI 红绿对。
- **G4-2 登记字样＋无关失败＝部分闭** ⚠️。已闭：聚合父／通过的同级／失败的祖先／选择器外测试均不算命中；源头日志路径已闭（登记校验拒绝字面量出现在日志调用行 `emitter_sites`）。**残留**：同一失败叶用例内另一条内联断言的消息恰含登记字面量。单E 判「`go test -json` 里逐字不可分」——**编排者不接受该不可分判定**：Log/Error 行同为 `file:line: text` 形态，**file:line 正是可分量**。裁定修法：登记表为每条 `expect_assertion` 增记**断言源位置**，命中须同时匹配该位置与字样，则同用例内不同行的断言可分。此为 registry schema 增量，归 13.R2 续做；残留形态（同一行多条断言）远小于现状，须显式登记而非声称已闭。
- **竞态豁免控制无 pass 终局仍判绿＝门对、规格须补** ⚠️。编排者核 `scripts/gates/race_check.sh:15-18,44,91-109`：豁免**仅在**「运行失败 ∧ 每个竞态签名都在登记表内 ∧ 无普通失败」三条同时成立时适用，是有界既有合法形态。故门的行为正确，**但 13 域 spec 的字面「所声明的选择器 MUST 有通过终局且无跳过」未覆盖该豁免**。裁定：**补规格的显式有界例外条款**（只限已登记签名、只限无普通失败），**不得反过来静默放宽门**。归 13.R2 同批 spec 同步。
- **活体行为未验＝待补** ⚠️。单E 未跑 go，故真实仓 11 个控制的绿/红未实测。**编排者已核 CI 影响**：`ci.yml:46/118/240/305` 四处安装 tmux，本机亦有 tmux，故 `tool/exec/event_integrity_test.go:29-31` 的 `IsTmuxAvailable()` 跳过在 CI 不触发——单E 担心的「无 tmux 机器整门转红」**不会使 CI 红**；该收紧在无 tmux 机器上转红是**正确行为**（跳过的控制树证明不了任何事）。活体复跑排在单F 完成后（Go 面释放）。

**G4 状态：仍 FAIL。** 七项差距中 G4-1 闭、G4-2/G4-3 部分闭、G4-4（手填判词）/G4-5（必需检查 ABSENT）/G4-6（归档绕过）/G4-7（本地预检不跑证据门）未动。**13.R2 未勾，正确。** 检查器自测 34 例绿只证明已列反例被拒，不是产品验收、不覆盖规格全集。

### R26 Wave C 派发：G1/G2 三单隔离树取证（2026-11-16）

09.R1 核销解锁了二节次序表第二行。**派 11.R1（G1）∥ 05.R1（G2 任务域）∥ 12.R1（G2 消费者/插件生命周期）**，三单各写本域 design.md，互不相交。

**为何走隔离树**：三叶的正锚均为「拟新增、尚未建立」的 Go 测试，需真跑；而单F 正在改 `memory/ledger/` 与 `assembly/symptom_anchor_recovery_test.go`，主树任何 Go 结果都不可信（R21 同仓闭包污染）。故三单统一用 `git archive 9547faca | tar -x -C /tmp/recovery-*/tree` 建**干净 HEAD 隔离树**跑原型，主树只读、禁写 .go、禁跑 go test/build。证据须显式声明取自隔离 HEAD 树而非工作树（二节源树绑定）。

**G3（06.R1）本轮不派**：其交付是分阶段成本读数，而单F 正在修冷读双计与漏计 cut 写——F 落地前取的成本读数会被 F 作废。等 F 完成后派，避免量两次。

**G2 共享面**：05/07/12 三叶共同产出 G2 判据表。本轮派 05 与 12，各只写本域 portion，**07.R1 留下一轮**；共享表由编排者集成（R22），不允许任一单写他域。

**下传的关键情报**：三单派发令均含 R19 N2／R20 N6／R24 N11 三条教训，明令不得重造「只喂日志行的生产出口」与「无法失败的判据」。11.R1 另含一条待证伪的注释断言：`memory/ledger/config.go:185` 声称同根第二实例被生产开档路径拒绝，但全仓 `Flock|LOCK_EX|lockfile` 生产零命中——**该声称当前未证实**，须由原型判定真伪，不得采信注释为权威（元规则一）。

### R27 单F 核销（部分完成）＋N13 计量命名（2026-11-16）

**完成度五查（编排者亲验，主树复跑）**：`go build ./...` **0**；两条新负锚 `TestColdReadCostCountsEachReadOnce`／`TestRetirementPassChargesItsCutWrite` **exit 0／2 PASS**；S2 三锚 **exit 0**。F 自报红证据成立（双计：meter 4.399ms vs 实际 2.231ms；漏计：写侧无出口），修法与绿证据我复跑确认。写集未越界，`journal.go` 未触，四个阈值字面 2 一字未改（仅把 durable 处提为同形 const，数值不变）。**09.R2 未勾，正确**——七段口径与阈值校准未做。

**N11 已闭 ✅**：新增 `judgeSeatedAccount`，S2 以 `judgeBootFixture` 为**前置判据**（账户非零且逐代增长才判任何 bound），探针测试加正守卫＋空根具名负例。量序 `durableAccount=[4 8 12 16 20]`，零读成本现坐落在逐代增长的非空账户上——**零不再是恒真，而是「非空账户上托管成功」的证据**。未加生产日志、未加接口方法，符合 R19 N2 教训。

**N13 新增计量出口名实不符（须修，归 09.R2 正单）**：F 新增生产方法 `ColdWriteCost()`（`segment_store.go:979`）与字段 `coldWriteCost`（:327）。**其注释诚实**（:321-326 逐项列出：冷复制与 barrier、cut 放置、**verify 重开**、**热档重写**），但**名字断言了一个内容不成立的类别**——「write face」含一次重开（读）与热档侧工作（非冷）。且其定义是「整趟 − 报告已述冷开档」，是**补集而非写类**。
- **与 N2 的区别（编排者辨明，不重复删除）**：`ColdReadCost()` 是**计划本身点名的出口**（09.R2 负锚 `TestColdReadCostCountsEachReadOnce` 逐字读它），故其存在有独立于自身的验收需求；`ColdWriteCost()` 按对称新增，服务于同一验收需求（09.R2 另一负锚要求写侧成本 >0）。**二者不是 N2 那种「只为喂一行日志」的自证结构**，不删。
- **但须修正两点**：①名字改为与内容相符（如「趟内除开档外的成本」），或拆成真正的写段与其他段；②F 声称「与 ColdReadCost 构成互斥两半」**不成立**——`ColdReadCost` 还含冷 miss 读（不属退位趟），故二者不是同一整体的两半。09.R2 正单要求「七段区间互不重叠、Σ≤总耗时」，**此二量的区间关系必须重新表达**，不得沿用「两半」说法。
- **F 如实披露的其余缺口（登记，归 09.R2 正单）**：失败退位两半都不计（沿既有 `ColdOpenCost` 行为）；cut 单段精确归因需 `RelocateReport` 加字段（`journal.go`，F 白名单外）；`coldIndexLocked` 的枚举读仍未计成本。

**N12 注释清偿 11→2，编排者裁决收口方式**：剩 2 条同为 `responsibility-fragmentation`——`retirement_scan_repro_test.go:1` 与 `retirement_snapshot_test.go:1` 同指 `memory-architecture.md#read-paths`，两文件皆无生产镜像。F 正确识别三条出口均越权或属洗白（并入未授权文件／改指别章致语义失真／加镜像生产文件＝自证结构），**未擅自洗白，是正确纪律**。**编排者裁决**：把未跟踪那个的唯一测试**并入已跟踪的 `retirement_snapshot_test.go`**，原文件移 `/tmp/recovery-probes/`。一举两得——一文件一责任消碎片化，且去掉一个未跟踪测试（N7 同类假绿通道）。已派窄单G 执行，并令其**只报告不处置**一项附带调查：该测试前提「durable load 按前缀枚举的任务事实须可读」在托管 boot 不再前缀扫之后，是否仍钉活要求（N6/N11 同根问题）。

**就绪集**：F 完成 → `memory/ledger/` 与 `assembly/symptom_anchor_recovery_test.go` 释放（窄单G 只触 `retirement_snapshot_test.go`，与三单隔离树取证不相交）。**06.R1（G3）现可派**——F 已修双计与漏计，成本读数不再会被作废；但须等 G 收口注释门后取，且派发令须带 N13 的区间重表达要求。03.R1 亦可派（assembly 面已释放）。

### R28 G1 判决核销：单拥有者实测证伪＋分叉丙修正为丙′（2026-11-16，编排者独立裁定）

**五查（编排者亲验）**：源树绑定合规——全部证据取自 `git archive 9547faca` 隔离树，主树零 go test／零 go build／零 .go 写。**红原型我亲自复跑**：`cd /tmp/recovery-g1/tree && go test ./tests/integration/history/ -run '^TestConcurrentWritersCannotInterleaveOneFactCommit$' -count=1 -v` → **FAIL，输出与代理所报逐字相同**。11.R1 未勾（负锚为红、消费者锚 file 腿 SKIP），正确。

**G1 ＝ FAIL（部分证实）**：G1.2 一 Batch 承载整事实 ✅、G1.3 四分类 ✅（带两处具名缺陷）、G1.4 原规范等价 ✅；**G1.1 单存储根单写者 ❌ 实测证伪**。

**失效范围（我复核确认，静默数据损坏）**：两写者选同一槽位 `{pid:1,window:1,seq:1}` 时 `1:evt:1:1` 与 `1:meta:1` 是共享键，**后写者正文覆盖先写者正文，而先写者的 `1:idx:` 仍指向该槽**；重开档 `GetEvent("g1-writer-a")` 返回 **B 的字节**。无错误、无告警。这是「一身份一不可变事实」的直接违反。

**`config.go:185` 是伪断言且自证（元规则一＋二，我读原文确认）**：原文称「A second runtime instance over one directory is refused by the production open path that owns that root, not by an exclusive lock here: **in-process checks of the same root appear in the test suite of this repository**」。全仓生产 `Flock|LOCK_EX|lockfile` **零命中**；开档路径唯一的拒绝是 `rejectLegacyShardLayout`（拒旧分片布局，与第二实例无关）。**该注释自己承认拒绝只存在于测试套件**——防御对象由结构自己制造。另 :184「两视图不能交错半帧」为真，但那是**弱得多的性质**，整帧追加不阻止逻辑覆盖；注释以弱性质冒充强保证。**须改写为真话**。

**分叉丙的洞（编排者驳回其跨进程部分）**：丙要求整事实提交在「引擎持锁的读—判断—写」一个临界区内，这部分对；但它把 `run.go:622` 与 `recover-kv/main.go:179` 两个**独立进程**的互斥写成「**运维约束，须写进 recover-kv 的文档面**」。**进程内锁不可能让两个进程互斥；把跨进程互斥交给文档就是约定，正是用户明令禁止的「不能通过约定、规范，要立机制」。** 且 §七.1 自认跨进程未测、各进程 `commitSeq` 独立故槽位可撞。

**编排者裁定：丙′＝OS 级建议写锁（flock），跨进程互斥由机制承担，不写运维约束。**
- **不是真分叉，故独立裁不伪 defer**：三条既有原则唯一确定此解——①一身份一事实（已确立的正确性不变量）要求跨进程排他；②立机制不立规范排除文档约束；③任务行明禁「以拒绝全部读取视图替代写入隔离」排除一概拒第二实例。**平台面我已核**：`ci.yml` 全部 `runs-on: ubuntu-latest`，生产代码无 Windows 路径（仅 `smoke_harness_test.go` 提及 GOOS），故 unix flock 覆盖实际平台，无需跨平台分叉。
- **丙′ 冻结前提（11.R2 派发依据）**：①锁按**存储根**取得，开档取**写权**时获取，覆盖**判重读与提交之间**整个临界区；②第二写者开档**具名响亮失败**（新错误名，指明该根已有写者），不得静默；③**只拒写权**——冷层开档（`segment_store.go:778`、`kv_original_store.go:198`）与诊断读视图不受影响，flock 建议锁天然满足；④进程死亡自动释放，**不得引入需陈旧锁恢复的路径**（这是选 flock 而非 `O_EXCL` 锁文件的理由）；⑤热根与冷根两把锁的**取得顺序须显式声明**防死锁；⑥`config.go:185` 注释改写为真话。
- **必配负锚（拟新增，尚未建立）**：**两个独立进程**各持 `OpenTier` 写同根同槽——修复前必红（正文丢失可复现，即现有原型的跨进程版），修复后第二进程必被具名拒绝。仅同进程两 goroutine 的红绿对**不足以**证明丙′，因为缺陷本体在跨进程。
- **外部可感知后果（须让用户知道）**：`recover-kv` 在运行时进程存活期间将**响亮失败**而非静默损坏。这是正确行为，但改变了运维工具的可观测行为，须写进其文档面与升级说明。

**G1.3 两处具名缺陷（成立，归 11.R2）**：①**不确定 ≠ 零写入**，且**进程内重读是假阴性**——barrier 后注入故障留下 3 个键已耐久，而进程内 `GetEvent` 答 `held=false`；只有**独立 `OpenTier`** 能判定。②**确定性零写入拒绝被误分类为不确定**——`journal_kv.go:139` 把**每个** append 错误（含超帧这种写前即可判定的拒绝）都包成 `ErrCommitUncertain`；其 doc 原文自陈「A failed append or sync … reported as ErrCommitUncertain」。超帧拒绝在 `journal.go:1088` 写前执行、0 键落地，应判确定拒绝而非不确定。

**G1.4 关键结论（成立，归 11.R2）**：`SameFact=true` 而 `Encode()` 字节不同（306 vs 316）的一对，引擎门 `segment_store.go:589` 是 `bytes.Equal` → 答 `ErrContentConflict`。故**领域等价门必须在引擎字节门之前**，否则领域上重复的合法重交被判内容冲突。退位后已搬身份重交仍判重复（经 `coldEventLocked:793`）✅。

**G1 状态：FAIL → 丙′ 已冻结，11.R2 可派**（须等 assembly/memory-ledger 面释放）。**G2/G3/G4 未判，产品不可发布。**

### R29 窄单G 核销：注释门归零＋N14 死构造器（2026-11-16）

**五查（编排者亲验）**：`comment_policy -no-baseline` → **0 finding(s)**（我复跑，`knowledge_base` 仍为警告不计数）；`go test ./memory/ledger/ -count=1` → **ok 21.4s**（我复跑）。合并逐字保全（代理给的两处字节级 diff 均空），`TestRetirementKeepsTheTaskFamiliesTheBootScans` 确已跑并 PASS，`-run Retirement` 命中 5 个用例非空转。移出文件落 `/tmp/recovery-probes/memory/ledger/`（原路径已被 assembly 探针占用，代理改路径未覆盖，正确）。**N12 闭 ✅**。

**N14 `NewDurableCommit` 生产零调用（死构造器，我独立复核确认）**：`grep -rn "NewDurableCommit(" --include="*.go" . | grep -v _test.go` **唯一命中是其自身定义 `runtime/state/durable.go:240`**。生产走 `NewEmptyDurableCommit`（`assembly/run.go:638`）→ `Seat`。故 :243 的**急切 `load()` 在生产永不执行**。
- **须辨明边界（不可扩大结论）**：`load()` **本身不是死码**——它仍经 `Seat` 的无切面回退可达（:410）。死的只是「构造即整族扫」这个入口。
- **与 N6/N11 的关系（编排者串起）**：生产有切面时 `Seat` 按**任务作用域前缀**读尾任务（`readTaskLocked`，:518/:531/:566），不调 `load()`；S2 夹具里检查点覆盖全账户故尾为空 → `Scans` 为空 → `durableLoadCount=0`。**这确证 N11 的零是正确有界行为**，也确证 **N6 的 attestation 量的是一条只在无切面回退才走的路径**——陈旧代理判定成立，非推测。
- **处置**：`NewDurableCommit` 删除归 **10.R4**；删前须核其测试调用方是否钉着「构造即加载」的旧契约（若是，随契约一并改，不得只删构造器留测试）。

**N15 合并后测试的前提措辞陈旧（小残留，须修）**：代理调查（我只复核其结论要点）——该测试的**断言钉的是活要求**（`durable.go:522` 在准入键缺失时把 spec 留空，故搬走那些键会破坏 seat），但**其常量注释与第一条 bullet 仍写「整族枚举」**，那是 `load()` 的机制、生产已不走。裁定：改措辞为「任务作用域前缀读」，**断言一字不动**。归 10.R4 顺带修。

**就绪集**：G 完成 → `memory/ledger/` 与 `assembly/` 全面释放（G2 两单只在隔离树写）。**03.R1 现派**（CI-1/CI-2 的 test+race 双红，最紧迫）。**06.R1（G3）排其后**——同属 assembly 包，且其消费者锚要在**含 F 修正的当前树**取两态读数，不能用 HEAD 隔离树（否则量到的是被 F 作废的旧计量）。

### R30 G2 任务域核销：两个 HEAD 缺陷确证＋乙被判据反对（2026-11-16）

**五查（编排者亲验）**：源树绑定合规（隔离 HEAD 树，主树零写零跑）。**缺陷 B 我亲自复跑**：`TestG2CoveredTaskMustNotReissueASettledNoticeIdentity` → **FAIL**，输出与代理所报一致。05.R1 未勾（T9 阻、A/B 未修、锚只在 /tmp），正确。

**缺陷 A 确证（我读两处原文）**：`runtime/state/durable.go:881-887` 的 `Commit` 对 `record.Outbox` **无条件**写 `d.owed`；而 `:375-388` 的 `applyRecordLocked` 先按 `d.consumed`／`d.rejected` **过滤**再写。故运行时与恢复两路对同一账户给出不同 owed 集（代理实测 3 vs 2）。**加重情节（我另核）**：`mailboxSliceWire`（`mailbox_checkpoint.go:32-34`）**只有 `Owed` 字段，没有 `Consumed`**——消费集不在任何切片里，故重启后 `applyRecordLocked` 的过滤依据需另由链重建，否则过滤形同虚设。

**缺陷 B 确证（我读两处原文＋复跑）**：`runtime/task/checkpoint.go:267-269` 只 `for _, open := range entry.obligations { r.seq.Observe(...) }`（**仅未结义务**）；`runtime/task/restore.go:293` 在 `for _, envelope := range fact.Outbox` 内 `r.seq.Observe(envelope.Notice)`（**每个信封**）。两条恢复路径给同一账户观察到**不同身份集**，故托管路径会**重发一个账户已结清的四元身份**（task/round/kind/seq）。**这是用户可感知的重复投递风险**，非仅内部不一致。
- **勘误**：代理战报简写为 `checkpoint.go:267`，编排者一度误读为 `runtime/state/`；准确路径是 **`runtime/task/`**（原型失败原文已给全路径）。

**A+B 合成未端到端证实（关键缺口，已派单证明）**：代理如实声明合成未证。二者单独成立不等于重启真会重复投递——须走完整路径（提交→结清→落检查点→重启托管→再投递）证明同一四元身份被发出两次。**不得以「两缺陷各自成立」推定合成成立**（元规则一：反事实须真实可达路径）。

**乙被判据反对（设计裁定，非仅前置不足）**：保留边界三候选中，(c)「身份不再可由重放到达」**永不到来**——任务五族从不退位（`segment_store.go` 的退位面只处理 idx/evt/meta/ref/blob），已退事件仍经冷层按身份可读（`:1215`/`:793`）。故 (a)(b) 均不安全，三族不可清减，**候选乙与判据相反而非缺前置**。成本缓解只能来自甲（按任务索引）。**此结论与 N6/N11/N14 一致**：任务族留热是活要求（`durable.go:522` 准入键缺失即把 spec 留空），不是遗留。

**乱序完成（实测，非推断）**：`foldOwed` 重赋 `AcceptSeq`（`memory.go:465-468`）是**每进程局部量**，不可作归约水位。实测消费 seq3 再 seq2 → position=0、owed={seq1}；**用最大序号作水位会丢 1/1**。故 G2 的水位必须是「水位＋未完集合」，不能是单最大序号。

**重复身份合法（确证，三层各自幂等）**：`assembly/facts.go:94-105`（同键同内容→原键不二次落记录；异内容→`ErrFactKeyConflict`）、`durable.go:850-856`（同 mutation→`CommitCommitted`，**不推进投递**）、`mailbox.go:161-173`/`:281-283`。`loop.go:1540-1549` 的稳定指令键走第一层，**重交合法，不是上游误发**——此点终结了先前「旧键重交即误发」的无根据断言。

**N11 前提精化（采纳）**：`Seated()` ∧ `TaskIDs()>0` 逐代增长 ∧ **`Scans` 为空**（须与「五前缀各 keys=0 的回退扫」区分——后者是扫了但空，前者是根本没扫）。F 已落的守卫须按此补强，归 09.R2 正单。

**任务账户症状锚红（实测）**：`TestTaskAccountStatedSizeTracksLiveWork` ×10 → **10 FAIL**，entries 8→40、bytes 2379→6943，而活任务只有 2。**检查点账随历史增长已量化确证**，是 03.R2/05.R2 的目标，非 flaky。

**G2 状态：任务域 PARTIALLY-PROVEN，整体未判**（07.R1 未派、12.R1 在跑、T9 交接阻于 11）。

### R31 G2 消费者域核销：FAIL＋计划前提被证伪（2026-11-16）

**五查（编排者亲验）**：源树绑定合规（隔离 HEAD 树，主树零写零跑，18 个读目标 `git diff --stat HEAD` 空）。12.R1 未勾，理由成立——锚只存在于隔离树，主树禁写故「拟新增、尚未建立」字面仍成立，**未缩水验收面**。

**C1 已提交未消费边界＝红（确证）**：切面 `checkpoint:16:16` 覆盖了 `behind-1-2`，boot 折叠 8 条事实、重放切面后那条，**从不重新应用被拒绝的那条**。即消费者落后于链时，其未应用的派生随切面推进而丢失。水位只取已落分区 seq（`assembly/checkpoint.go:305-311`），`flight.await`（:355）等的是提交**单元**不是成功，重放只取 `pastCut`（:1158-1164）。**mailbox 的 owed 集本身表示正确**（`mailbox_checkpoint.go:32-98`，身份去重、两源恢复）——须与 R30 的「Consumed 不在切片」分开看：owed 有，consumed 无。

**失败语义不对称（我读两处原文确证）**：`assembly/facts.go:152-154` 活路径 `states.Reduce` 失败只 `assemblyError` **吞掉**；`assembly/run.go:2995-2997` 重放路径同一调用**上抛并失败 boot**。同一派生、两种失败处置。这是真不一致，归 12.R2。

**吞异常按语义单元枚举（采纳）**：2 处真吞（`wrapper.go:312-314`、`facts.go:152-154`）＋1 处新发现 `runtime/agent/loop.go:371-375`（WARN 后仍投递 → 链上无收据）。投递完成单元已是正确模板（具名＋保留义务）。

**计划前提被证伪（须回写 proposal）**：12 域 proposal 声称「禁用插件会留下孤儿切片／panic」。**在声明路径上为假**——`plugins:` 只能禁用 **agent 作用域**插件，`-compress-consumer` 这类 run/host 级插件被**具名拒绝**（`assembly/plugin_catalog.go:302-306`，我读原文确证；绿锚 `plugin_mount_test.go:200`）。三个可禁用的 agent 插件**不持任何切片**；`mountBuiltinPlugins` 无条件执行故 `r.compress` 永不为 nil。**勘误已落该域 design**；proposal 待编排者回写。

**disposer 全生产丢弃（我独立复核，数字修正）**：`runtime/state/checkpoint.go:131` `Register` 返回 `(func(), error)`，:172-186 的闭包删 holder。全仓 **7 个切片**（projection/delivery/tasks/inputs/compress/mailbox/governance-StateSeat），**5 处注册点**（`run.go:577`/`:652`/`:655`、`governance_checkpoint.go:52`、`host_delivery_checkpoint.go:152`）**全部用 `_` 丢弃 disposer**，仅 `runtime/state/checkpoint_test.go:45-49` 调用它。代理战报称「7 个生产注册」，**准确说法是 7 个切片、5 处注册点**，结论（全丢弃）不变。故卸载丢失是**潜在红而非活路径**，12.R3 须先有生产宿主。**不删 disposer**：动态卸载是用户明定的三挂点核心目标，属应接线而非应删；但在接线前不得声称卸载路径已验证。

**候选择定：乙（应用失败即拒绝推进切面）**——复用既有整切面红线与其响亮路径。**甲被否**：需「已应用」的第二份表示、按分区 payload 映射，且要新造一个 R19-N2 形态的自证可观测。**编排者认可此推理**，与本轮反自证裁定一致。

**幂等＝绿（3/3）**：两次重启后派生状态字节相等、owed 身份相同；非空守卫确曾在坏夹具上触发（记录未隐藏）。

**G2 消费者域＝FAIL。07.R1 已派**（补齐 G2 最后一块，隔离树）。G2 整体仍待三域齐 + T9 交接。

### R32 03.R1 根因确证＋CI 双红归因终结＋走查不变量裁定（2026-11-16）

**五查（编排者亲验）**：03.R1 **未勾，正确**——修复面 `memory/ledger/segment_store.go` 既在白名单外又在「勿动」名单，代理按派发令 STOP 并上报，**这是正确纪律不是失败**。主树写集只 `assembly/recovery_judgment_test.go`（+127/-0，新负锚 `TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint` 在 :102）。候选修复只在隔离树验证。

**根因（我逐处读原文确证）**：`segment_store.go:1358-1361` 的 `eachIndexEntryNewestFirst` 开局只在锁内快照 **`total := len(s.order)` 与 `coldBound`**，随后逐步用 `at` 索引**活的** `s.order`，**步间释放 `s.mu`**。而 `RetireFacts` 在 :945-947 **整体替换** `s.index = index; s.order = order; s.windows = windows`（按 keepKeys 新建）。故退位趟落在两步之间时：①`at` 移位 → 同键二次交付；②键已出 `s.index` → :1356 注释所述的 `published()` 冷副本压制失效 → 冷侧再交付一次。
- **:110 vs :117 的时机差已解释**（我派发令的硬要求）：同一 walk、同一输入，差别只是退位趟落在哪一次读里——落在第一次读 → :110 红，落在第二次读 → :117 红。**调度问题，非输入差异，非 flaky，非数据竞态**（全仓状态访问都在 `s.mu` 下，故无 DATA RACE 报告，属逻辑竞态）。
- **实证与 CI 同一身份**：诊断树 `-race -count=8` → 4 FAIL、15 条 DUP，例 `DUP key=cmpout-6b13a5c5ae1de41f src=cold commit=78 total=15 orderNow=6`（order 中途 15→6），且失败正是 **`cmp-e401321cc0412ce0 answers 2 products`**——与 CI-1/CI-2 原文的判定身份逐字相同。**归因到此终结，不再是推测。**

**驳回代理一处过强论证（结论对、论证错）**：代理称「产品身份 `sha256(request+producer+key)` 与请求身份互相绑定，故同一请求两个不同产物**在数学上不可能**」。我读 `api/event/compress_product.go:74` 与 `compress_request.go:52` 原文：产品身份含 `key` 变量，**同一请求配不同 key 仍可得两个不同产品身份**，故「数学上不可能」不成立。**正确表述**：本次观测到的计数 2 是**同一产品键被交付两次**，由 DUP 诊断与 CI 身份逐字相符实证，而非由身份公式排除其他可能。结论不变，论证须以此为准。

**候选修复不采纳（两处不合格）**：代理在隔离树验证的三处改动（捕获 `resident := s.order` 切片头＋每步持锁读位＋每 walk `handed` 去重集）虽使正负锚 `-race -count=10` 全绿，但：
- **`handed` 去重集是症状掩蔽**：走查不一致才产生重复，加去重集是把不一致藏起来（元规则一：删掉它谁真的坏？坏的是走查本身）。**禁以去重集作为修复**。
- **代理自陈残留是「漏」**：pass 前已读 coldIndex、键随后退位的项会被整条漏掉。**把「多」换成「漏」同样是正确性违反**，不可接受。
- 代理另否决了一个变体（捕获 `s.index` map 头 → 10 次跑 6 次 DATA RACE，因 `StoreEvent` 原地写 map），此点采纳：**位置必须持锁读**。

**编排者裁定：走查不变量（03.R1 修复与 06.R2 共同判据）**
- **不变量**：一趟索引走查对每个身份**恰好交付一次**，与退位趟的时机无关；**既不得多也不得漏**。
- **方向（推荐，理由来自计划既有原则）**：06.R1 五问之一已是「**冷热只改布局**」。退位是布局变更，**不改事实集合也不改提交序**，故**热冷合并后的提交序在退位下不变**。走查应迭代这个**不变序**（按单调提交序号，或开局锁内快照合并序），正文按身份读、透明落冷层——而不是迭代可变热切片的**位置**。如此重复与漏都无从产生，**不需要去重集，也不需要新锁**。
- **备选（仅当 G3 证明快照成本不可接受）**：退位趟与走查互斥（存储级读写闸，走查取读、退位取写）。**须防 R6 已实测的死锁形态**：Go RWMutex 有写者待时新读者阻塞，闸位若可被闸内重入即永不归零；采此形必须附重入不死锁的实证。
- **取舍属 G3**：快照合并序是 O(全史) 内存，按序号迭代需跨层 seq→key 定位。**故先派 06.R1 取成本证据再定形，不以症状掩蔽抢绿。**

**就绪集**：03.R1 停在白名单边界 → 主树 `assembly/` 与 `memory/ledger/` 释放。**06.R1（G3）现派**，并把本条走查不变量作为其「冷热只改布局」一问的具名输入。03.R1 的修复排 G3 定形之后，与 06.R2 合单（同文件同不变量）。

### R33 G2 治理域核销：FAIL＋G2 整体判定（2026-11-16）

**五查（编排者亲验）**：源树绑定合规（隔离 HEAD 树，主树零写零跑）。07.R1 未勾，正确。**两处最重缺陷我读原文确证**（见下）。**引用路径毛病复发**：代理战报写 `approval_store.go:124/:170`、`checkpoint.go:267`，实际分别在 `runtime/task/approval/`、`runtime/task/`；编排者两次误读为 `features/evolution/governance/` 与 `runtime/state/`。**后续派发令须明令引用带完整包路径**。

**N16 重启续写覆盖已存储事件（违 P1 红线，最重，机制确证）**：
- `features/evolution/governance/denial.go:317-326` `putGovernanceEvent`：marshal 失败 return、`Put` 失败 return、**`_ = log.Sync(ctx)` 忽略 barrier 错**。其注释自陈「the durable half is **best effort**」——而内存侧 `evtSeq` 已自增，故流中留洞。
- `:384-396` `governanceTail`：`for seq := cut+1; ; seq++ { … if len(stored) == 0 { return tail, nil } }`——**遇洞即停**，报告的最新序号是洞前一个。重启续写从洞号起，遂**覆盖洞后已存储的事件**。代理在真实 localfile tier 实测键 4 正文 `tool=fourth` → `tool=after-restart-b`，×10 全红。
- **裁定**：耐久事件流**不得 best-effort**。写失败必须具名上报并阻止序号前进（或使续号以**已存储最大键**为准而非内存计数）。`governanceTail` 遇洞即停须改为具名报告洞并拒绝续写覆盖。**此缺陷今天被全量 `Records` 读恰好掩盖**，故**候选甲（清减历史读）不得先于洞语义具名化落地**——否则掩盖消失即数据损坏。

**N17 人的拒绝不跨重启（安全相关，机制确证）**：`runtime/task/approval/approval_store.go:170` `RestoreEvidence` 的过滤是 `if held.ID == "" || (held.ExpiresMs > 0 && now >= held.ExpiresMs) { continue }`——**按过期丢弃且不区分 denied**。故已过期的人类拒绝被丢，重启后 `Lookup` 答 nil → `policy.go:359` 铸新请求 → 批准即放行同一参数。
- **编排者采用的准确表述**：争点不是「过期该不该失效」（那是语义取舍），而是**活路径与重启路径行为不一致**——代理实测同一时刻同进程不铸新请求（死等），重启后却铸。**同一输入两条路径两种行为，即为缺陷**，与过期语义如何裁无关。
- 另两项红：身份不随切面存活、别名跨切面塌缩（生产可达 `runtime/agent/fuse.go:363`）。

**N18 审批不是派生加速而是唯一耐久载体（架构事实，与「链是唯一真相」冲突）**：`approval_store.go:124` `a.chain = newAttentionLedger()` **无条件构造**，且 opts 循环在 :121-123 先于该赋值执行，故无注入选项。attention 链纯内存 ⇒ **「链是真相」对 denial/goal 成立，对审批不成立**。
- **编排者裁定方向**：按已确立的「链是唯一真相」原则，审批应经链耐久，而非另立一份文件权威。此为 07.R2 的设计前提，**不是可延后项**——因为 N17 的修复若只补文件镜像，就是承认第二份真源。**须与 N17 同批设计**，不得先打文件补丁。

**历史查询边界＝约定而非机制，且恢复已静默变短**：真实 tier 持 3 条（洞在键 2），`Query(0)` 答 1、`Count()` 答 1，**无错误无窗口声明**（×10 红）。`Count() != DenialSeq`（malformed 正文跳过但仍取号，2 vs 3）⇒ **否掉候选甲的「Count() 改答尾序号」变体**。**消费点实测 29 处，任务行写「六处」——计划勘误，须回写 07 任务行**。

**两兄弟缺陷的治理域实例**：C1 不直接共享（`StateSeat` 无 `Reduce`，治理事件走 `TierFacts.Put` 不进 `commitFact`），但有**同族更重实例**即 N16。C6 **共享且更重**：写腿零返回零日志，读腿上抛至 `host.abortBoot`（`host.go:773`）。disposer 确以 `_` 丢弃，**不声称活路径缺陷**（与 R31 一致）。

**验收面勘误（代理上报不自裁，正确纪律）**：冻结负锚**按字面今天不红**——闸门铸新请求并 park，区分不了原 hold 是否存活；且须撤走文件镜像否则被第二来源 defeating。**编排者裁定**：负锚须改为断言「重启后同一参数不得被重新批准」这一可观测后果，而非断言 hold 对象存活；此改动**属验收面精化不属缩水**（原判据无法失败＝不是判据，R24 N11 同理），回写 07 任务行时须标明勘误理由。

**G2 整体判定：FAIL。** 三域齐——05 任务域 PARTIALLY-PROVEN（缺陷 A/B，合成待证）、12 消费者域 FAIL（C1/C2/C4/C6 红）、07 治理域 FAIL（N16/N17/N18＋历史边界）。**G2 共享判据表由编排者集成后方可派 05.R2/07.R2/12.R2/03.R2**。**G1 FAIL（丙′已冻结）、G3 在跑、G4 FAIL。四个门无一通过，实现叶仍不得派。**

### R34 A+B 合成判定：不合成，各自独立充分（2026-11-16）

**五查（编排者亲验）**：隔离 HEAD 树，主树零写零跑；三个生产文件为归因临时打补丁后**逐字节复原**（`git show 9547faca:` diff 验证）。未勾任何框，07/12 未触。

**判决：不合成。A 与 B 相互独立，各自单独充分。** 2×2 归因实测：A 中和 → forced PASS、public PASS、sequencer 仍 FAIL；B 中和 → forced 仍 FAIL、sequencer PASS。**互不需要**，故 R30「合成未证」的缺口到此闭合，且答案是否定的——**不得把两个缺陷叙述成一个合成缺陷**。

**A 单独即构成经公共入口的真实重复投递路径**（决定性观测，×3 同一）：gen1 第二次 `Advance` 经公共入口返回 `(1, nil)`、owedHolds=true；flush 后 unconsumed=0；**gen2 重启后 `unconsumed seats of an already-settled identity = 1（want 0）**——已结清身份被重新以待投递提供。
- **机制**：活路径有守卫——`m.accepted` → `ErrNoticeConflict`（`runtime/state/mailbox.go:161-173`），故 gen1 **不**重复。**该 map 不在任何切片里，重启即失去守卫**。
- **真实路径全程公共入口**：`assemble` → `CommitToSession`（run.go:1937）→ `registry.Register`（lifecycle.go:211）→ `Advance`（lifecycle.go:282）→ `DurableCommit.Commit`（durable.go:844）→ `manager.Flush`（manager.go:149）→ `Run.deliver`（tools.go:50）→ `AcceptNotice`/`Claim`/`Consume` → `Checkpoint`+`Close` → 新 `assemble` → `Seat` → `mailboxState.RestoreState`。**不是 helper 级演示**。

**R30 归因勘误（编排者自纠）**：R30 称「Consumed 不在切片里，故重启后过滤依据需另由链重建，否则过滤形同虚设」。**此加重情节不成立**——`durable.go:468-474` 的 `seatTaskLocked` 确从 `StateSettleAccount`/`ReceiptsOf` **重建 `d.consumed`**（我读原文确证），且在记录应用之前。**缺字段不是承重项；`owedOfWork` 从不查该集合才是**。代理实测：仅补这一处查询（A 仍在）即令 forced 探针转 PASS。
- **编排者自身一处工具错误亦记**：我以 `grep "func owedOfWork"` 判该函数不存在，实为带接收者方法 `func (s mailboxState) owedOfWork`（`mailbox_checkpoint.go:75`）。**我的 grep 错，代理引用对**。其体内只有 `seen` 身份去重，确无 consumed 查询。

**修复方向（小而定，不需新切片字段）**：`owedOfWork` 折叠前须以**已重建的 consumed/rejected 集**过滤，与 `applyRecordLocked:375-388` 同一判据。**不得以「给 mailbox 切片加 Consumed 字段」作为修复**——集合已在，缺的是查询。归 05.R2/12.R2 集成时同批。

**两者皆潜在红，不得判「安全」（关键限定）**：生产形态的终态通知 → PASS ×3，`Advance` 拒绝（`lifecycle.go:304`「the task already holds its terminal fact」）。**A 的触发只被一个偶然事实挡住**——当前每个生产 outbox 生产者都是终态（`cancel.go:92`、`wrapper.go:307`、`delegate.go:651`）；**B 的 `IssueNotice` 生产零调用**（我复核：唯一命中是其定义 `runtime/task/handoff.go:272`）。故**任何新增非终态 outbox 生产者即令 A 转活**。这不是「已安全」，是「尚未被触发」，须按缺陷修，不得按不可达关闭。

**未检（如实）**：跨进程夹具；真实 `SettleDriver.drain` 轮次（其尝试收据守卫 `settle_turn.go:542-554` 可能挡住 seat，未测）；全量回归。

### R35 G3 核销：PASS（计量门 discharged）＋走查定形甲＋cut 写无界确证（2026-11-16）

**五查（编排者亲验）**：工作树时点（HEAD＋已验证计量修正），与 CI 记录分开记；生产代码零写，探针经 `-overlay` 注入。三个新锚我复核确已落盘（`symptom_anchor_recovery_test.go:655/:768`、`segment_store_test.go:676`）。

**G3 ＝ PASS，但「恢复成本随历史有界」未达成**——须分开表述，不得因门 PASS 就宣称目标达成。

**N19 退位 cut 写确证 O(冷键数)、无界（R19 风险已实现，量化）**：100→6400 键（64×）下 JSON 6,594→428,871 B（65×）、`cutOf` 6.29→390.44 µs（62×）、marshal 30.69 µs→1.889 ms（61.5×），**66.0 B/键**；6400 键时该趟总计 3.451 ms。**R19 点名的「把正文全扫换成整索引全扫」不是假设，已实测成立**。**06.R2 必须拥有此项**——开档转平不等于整体有界。

**走查定形：甲（合并序快照），有实测依据，编排者采纳**：
- 缺陷在主树复现：**10/10 趟走查不一致、65 次重复**。快照形态在退位落于走查中途时交付 **52 条／52 身份／0 重复／0 遗漏**，**不用去重集、不用新锁**——正是 R32 裁定的不变量。
- 成本：甲 4.29→25.71 µs、堆 24,728 B；乙（seq 映射）7.29→27.54 µs、堆 49,192 B（**差 2.0×**）。
- **决定性论据（修正 R32 的成本顾虑）**：今日 `coldIndex` **已经**物化并排序整个冷索引（2.406→4.692 ms，8→287 条），故甲的增量比**已付成本小 100–1000×**。R32 说的「O(全史) 内存」绝对为真，但**冷半已付**，甲的**边际**内存只是热半即 O(存活集)。**编排者接受此修正**。
- **丙（读写闸）否决——重入死锁已复现**（写者待时阻塞被重入的读者；走查持闸 18.223 ms 而 `RetireFacts` 全程持 `s.mu`）。**R32 预警的 R6 陷阱实测成立**，故不采。

**N13 区间关系已按要求重表达**：一趟 25.437 ms ＝ 开档 6.111 ＋ 其余 19.289；冷 miss 读 2.057 ms **在每一趟之外**——**不是两半**。区间不重叠实证：`engineOpen+rebind == bootWall` 精确相等，Σ145.665 ms ≤ 总 1.0013 s。

**新发现：`durableLoadCount` 亦依赖退位开关**——退位关时 `[0 12 24 36 48]`（开时全零）。故 N11 的守卫须声明它判的是**退位开**这一态；冷读守卫会触发使 S2 无法判退位关态，此为正确行为不得放宽。

**五问结论（采纳）**：①切面只在退位时放（`journal.go:589/:765`），退位关⇒无切面⇒整读，选丙＋甲并**具名声明该降级**；②**保留整表前缀读，标 O(存活集)**——不是 O(1)、不是按需；③非常数操作具名：cut 写、全序走查、未折叠链扫、`Namespaces`、无切面开档；④`journalIndexCutVersion=1`，切面是可丢弃派生载体，**本轮无迁移**；⑤冷热只改布局 ✅（退位后 `EventCount` 仍 45），**缓存 ≠ O(1)**。

**N20 注释门回归（03.R1 引入，编排者已修）**：G 收口后本已 0，03.R1 的新负锚引入 2 条（`recovery_judgment_test.go:102` test-doc-not-one-sentence、`:124` free-standing）。编排者直接修：三行意图压成单行、游离注释移入 doc bullet。**修过程自身犯错两次并自纠**：Edit 的 old/new 参数弄反，把同一段注释重复插入至 4 份；读回实际状态后一次删净。**另学到该门的具体形状**：bullet **每条必须单行**，续行（`//   …`）会被判为散文段落而触发 doc-not-brief；上限 160 runes（`main.go:1341`）。修后 **0 findings**、`go build` 0、负锚仍按预期红（`handed … 2 times, want once`，缺陷未修故红是正确的）。

**G3 未测项（如实登记）**：增量维护的尾-only 前缀读是**推导非原型**；cut 单段精确归因需 `RelocateReport` 加字段（白名单外）；`coldIndexLocked` 枚举读仍未计成本；失败退位仍未计成本。

**四门判决齐**：**G1 FAIL（丙′已冻结）、G2 FAIL（三域齐）、G3 PASS（计量门）、G4 FAIL（7 差距闭 1 部分闭 2）**。**解锁**：11.R2（G1 丙′）与 06.R2＋03.R1 走查修复（G3）可派；G2 系（05.R2/07.R2/12.R2/03.R2）仍阻，须编排者先集成 G2 共享判据表。

### R36 走查修复核销：CI 双红根因已修＋分叉批准（2026-11-16）

**五查（编排者亲验，主树复跑）**：`go test ./assembly -run '^(TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint|TestRestartExecutesTheChainJudgmentOnce)$' -race -count=10 -v` → **exit 0，20 PASS，0 DATA RACE，0 FAIL**；新存储锚 `TestEachIndexNewestFirstStatesOneIdentityPerCommitAcrossARetirement` `-race -count=10` → **ok**；注释门 → **0 findings**。代理另报 `go test ./assembly/ -count=1` exit 0（305.5s，0 失败）、`./memory/ledger/` exit 0（22.8s）、build/vet 0。**03.R1 已勾**，偏离已声明（修复面在 `segment_store.go`，超出该行原白名单，但 R32 已裁定该面归本修复）。

**分叉批准（代理上报请裁而非擅自违反约束，正确纪律）**：
- **纯快照不完整**：代理首试只冻结热半的序、不带身份集，**10/10 仍红**。根因是冷半在 pass **之后**懒读，会重述冻结热半已命名的身份。即「合并序在退位下不变」对**集合**成立，但实现若混用「冻结热半＋活冷半」，看到的是 R ∪ C′ ⊇ R ∪ C，被搬的键在两半各出现一次。
- **批准的理由（精确区分，不是放宽约束）**：`stated` 与 `frozen` 在**同一个 `s.mu` 锁窗口内**建立（`segment_store.go:1361-1375`），此后**只读不写**（唯一另一处是 :1421 的成员查询）。故它是**快照的组成部分**，不是交付累积集。禁的是 `handed`/`seen` 那种「记录本次走查已交付什么」的动态集——它掩蔽序的不一致；`stated` 记录的是「快照时热半命名了哪些身份」，是**既有跨层身份判据的输入**，把该判据的输入与序一起冻结才是完整快照。原缺陷正是 `published()` 查**活** `s.index`，退位中途即失效。
- **编排者核实**：读实现确认 `stated` 无走查期写入；快照仅在 `s.retire && s.coldRoot != ""` 时取，未开退位走原路径——**安全**，因为 `StoreEvent` 只追加不移位，`at` 索引不失效，且快照后新提交不入本次走查属正确快照语义。
- **被否的替代**：在同一锁内急切读冷半——会把 `s.mu` 持过冷档开档＋枚举，代价落在每次 `NewestOfType` 调用上。否决正确。
- **不变量实证（非空转）**：`settled=19 moved=9 handed=20 handedAfterThePass=19 coldOnlyHanded=true`；断言交错身份数==对照、每身份计数==1、被搬的每个身份恰交付一次、且有一个纯冷身份被交付。修复前同夹具在隔离 HEAD 树 **5/5 FAIL、handed=19 of 20**（即漏 1）。**不多不漏两侧都有红绿对。**

**残留（登记给 06.R2 成本表）**：退位开时 `NewestOfType` 每次调用复制热序，**O(存活集)，非 O(历史)**。此项须进 G3 的具名操作成本表，不得因走查转绿就从成本账上消失。

**CI-1/CI-2 状态**：根因已修并经 `-race -count=10` 双锚复验。**但不得声称 CI 绿**——CI 未触发，且 CI-3（256 超时）、CI-4（票据，04.R1 已修待矩阵验）、CI-5（N6 陈旧代理）、CI-6（证据门）各自独立，须 09.R3 全量矩阵取通过终局。

### R37 计划回写：11.R2 前置承接丙′＋派单（2026-11-16）

**计划措辞缺陷（编排者自查）**：11.R2 任务行原写「依 11.R1 结论；**G1 未过不得开工**」、前置「11.R1 通过」。而 G1＝FAIL（G1.1 证伪），按字面 11.R2 永不可派——**但 11.R1 任务行自带分叉机制**（「G1.1 若证明单拥有者不成立，必须给出具名条件分叉与失效范围」）。两处措辞互相矛盾，属 R2 隐藏依赖形态。

**回写（不缩水，反而加严）**：11.R2 前置改为「在编排者已冻结的**分叉丙′** 下开工」，并把 R28 的六条前提列为硬前置；**新增负锚** `TestSecondProcessOnOneRootIsRefusedWriteRights`（两个**独立进程**同根同槽，修复前必红、修复后第二进程具名被拒），并明写**同进程两 goroutine 的红绿对不足以证明丙′**。平台面已核（CI 全 ubuntu、生产无 Windows 路径）。已落 `11/tasks.md:14`。

**派单**：11.R2 已派，按优先级①丙′ flock 写锁（静默损坏修复，最高）②整事实一 Batch③提交结果四分类（含 `journal_kv.go:139` 把确定性拒绝误包为不确定）④领域等价门先于引擎字节门。白名单含 `config.go`（注释改写＋锁取得）。明令不得扰动 R36 走查修复、R27/R35 计量、R24/R27 的 S2 守卫，不得抢做 06.R2 的 cut 写有界化。**06.R2 与 11.R2 同触 `segment_store.go`，串行**（计划已明写）。

**下一步（编排者自留）**：G2 共享判据表集成——三域证据已齐（R30/R31/R33/R34），须由编排者单写成一份判据表，方可派 05.R2/07.R2/12.R2/03.R2。集成要点已具：owedOfWork 补 consumed 过滤（R34，集合已重建、缺的是查询）、N16 洞语义须先于历史清减具名化、N17＋N18 审批经链须同批（不得只补文件镜像＝承认第二真源）、乱序须水位＋未完集合（单最大序号实测丢 1/1）、治理历史边界须由约定改机制。

### R38 丙′ flock 落地但树转红：既有资源泄漏被暴露（2026-11-16，当前状态）

**⚠️ 当前工作树红**，不得据任何局部绿声称整体可用。`go build ./...` 0，但 `go test ./assembly/ -count=1` **66 个顶层失败**、`./tests/integration/history/` **10 顶层/37 子失败**、`./memory/ledger/` **1 失败**，**全部 `ErrRootWriteLocked`**。

**根因（编排者独立复核）**：`Run.Close` **从不关闭存储 tier**（grep 其函数体只见 `r.mcpServers.Close()`），故任何同进程重启夹具都拿不到根锁。我复跑 `TestRestartSameSegmentAfterReplay` 确认失败原文即 `memory: another process holds write rights on this storage root: /var/folders/…/001`。**这是既有资源泄漏，锁把它暴露出来**——不是锁错了。

**丙′ 本体成立（跨进程红绿对是真的）**：子进程由重执行测试二进制实现（`exec.Command(os.Args[0], "-test.run=…")` ＋ env 传根），**不是同进程两 handle**。修复前 exit 1，原文 `the first writer's identity answers "the body the second process landed over the shared slot", want its own body`——跨进程静默正文丢失复现；修复后 exit 0，第二进程具名被拒。新错误逐字：`memory: another process holds write rights on this storage root`（`ErrRootWriteLocked`）。`config.go:185` 伪断言已改写为真话（一根一写者、组件视图不声明 WriteRights 即可 beside 活写者开读、内核持锁故进程退出即释放、无陈旧锁恢复）。

**R28 前提⑤修正（编排者接受）**：原要求「热根与冷根两把锁的取得顺序显式声明」。**冷日志在同一根之下**，退位趟在根锁内写它，故**一根一锁已足**，无第二把、无顺序问题。代理实测加第二把冷根锁会与同进程合法冷写互斥、弄红 `TestColdArchiveRefusesToOverwriteAnIdentity`，判为自证结构——**推理成立，我 R28 的⑤属过度指定，就此修正**。

**代理纪律正确**：它停在白名单边界并逐项列出所需越界改动，未擅自扩权。编排者据此**显式扩白名单派收口单**：`assembly/run.go`（Close 释放 tier，须幂等、须核 Close 后无读者、组件关闭出错也不得泄漏锁）、四个夹具文件（且明令不得以跳过或放宽断言避锁）、`scripts/recover-kv/main.go:179` 与 `migrate.go:83/:135`（**须先判定谁是写者谁是读者，只读开档不得声明 WriteRights**）、以及注释门收口。

**注释门 1 条（编排者核实文件归属）**：`cold_archive_gate_test.go:1` responsibility-fragmentation，与新 `config_test.go` 共指 `#store-instance-sharing`。**`cold_archive_gate_test.go` 是 HEAD 已跟踪文件，非本单新建**（代理误判为白名单外新文件）；干净解是一文件一责任——把新写权测试并入既有文件、移除新文件。

**11.R2 未完成，框未勾（正确）**：P1 丙′ 代码完成待收口；**P2 整事实一 Batch 未开始**（故两个冻结锚 `TestFactCommitLandsOriginalAndRecordInOneCommit`／`TestRefusedFactCommitPublishesNoKey` 尚不存在，零匹配 exit 0 不算证据）；P3 四分类部分——(b) 文档契约已修，**(a) 需越白名单**：超帧拒绝（`journal.go:1153`/`:813`）是未命名格式化错误，`journal_kv.go:139` 无从区分「写前确定拒绝」，正确解法是在 `journal.go` 加 `ErrJournalFrameTooLarge` 并以 `errors.Is` 分类，**登记待派**；P4 领域门次序 G1.4 已证现状正确（`facts.go:370-375`），未改未验。

**收口成功判据（派发令已钉）**：assembly／history／ledger／recover-kv 四套全 0，跨进程锚与走查锚仍绿，注释门 0。**未达此判据不得声称树可用。**

### R39 收口单撞上限＋N21 WriteRights 覆盖语义缺陷（2026-11-16，编排者诊断）

**收口单撞 150-turn 上限**，末句停在「即将接入 Run.Close」。编排者按「耦合簇撞上限改主线程诊断再窄派」亲自核现场，**实际进度比末句更远**：`Tier.Close` 已建（`config.go:330`，nil-safe、`t.closed` 幂等、经 Segments 一次关），`Run.Close` 已接入（`invoke.go:953` 经 `closeStorage`），且用 **named return ＋ defer `errors.Join`** 保证**任何结束路径都释放锁**（含 close 自身出错），符合派发令要求。`go build ./...` **0**，`go test ./memory/ledger/ -count=1` **绿 23.3s**。

**编排者白名单写错文件（自纠）**：`Run.Close` 实际住在 **`assembly/invoke.go`**，不是派发令写的 `assembly/run.go`。代理改在函数真实所在处，**不是越权**，是编排者白名单不精确。

**N21 真设计缺陷（编排者诊断，非夹具问题）**：`memory/ledger/config.go:281` 在 `OpenTier` 内**无条件覆盖** `cfg.WriteRights = cfg.Type == StorageJournal || cfg.Type == StorageLocalFile`。故**每个耐久 `OpenTier` 都取排他写锁，含纯读视图**，调用方显式声明 false 也被覆盖——**直接违反丙′ 冻结前提③「只拒写权，冷层开档与诊断读视图不受影响」**。
- **又一处伪断言注释**：`config.go:274` 称「不声明 WriteRights 的组件视图不持写权、可 beside 活写者开读」——**对 `OpenTier` 为假**（字段被覆盖），只对 `OpenAtomicStore`（:233 尊重该字段）为真。**这是本区域第二条伪断言注释**（前一条是 :185 的同根第二实例拒绝），须一并改写为真话。
- **recover-kv 两条路径反了**：`main.go:103` 是**读/扫描**路径却用 `OpenTier` → 误取写锁；`main.go:179` 是**restore 写者**却用 `OpenAtomicStore` 未声明 WriteRights → **不取锁**。丙′ 点名的第二活动写者因此未被覆盖。
- **后果实测**：`./scripts/recover-kv/` 约 6 个测试红，全经共享 helper `seedRoot`（`main_test.go:417`）／`openRoot`（:425）；`./tests/integration/history/` 10 顶层/37 子红，**耗时从 ~17s 涨到 ~318s**（有等待）。

**编排者自身命令错误（自纠，已写进派发令）**：我用 `go test … | tail -3; echo "X=$?"` 取退出码，**报的是 tail 的退出码不是测试的**，故三套都显示 0 而实际两红。改为重定向到文件后 `echo $?`。

**窄派纠正（已派）**：顺序钉死为①`OpenTier` 尊重调用方声明、去掉覆盖 ②只在真写者处声明 `WriteRights: true`（生产 boot、recover-kv:179；migrate.go:83/:135 须先判读写）③改写 :274 伪断言 ④**之后**才修夹具，且只修「真持一个写者又调另一个写者」的残留失败，优先改共享 helper。**明禁**：为消红给读视图声明写权、跳过测试、放宽断言。并要求先验快套（ledger/recover-kv）再花时间跑慢套，接近上限即停并如实报绿红——**部分而准确的战报远比跑尽上限留半改状态有用**。

### R40 N21 已修＋编排者角色诊断错误自纠（2026-11-16）

**N21 修复效果实测**：`OpenTier` 去掉无条件覆盖、声明原样透传；写权只在真写者处声明。**history 从 317.8s／10 顶层红 → 19.6s／1 红**；`memory/ledger` 绿（55 顶层＋28 子 PASS，20.2s）；`scripts/recover-kv` 绿（8＋4 PASS）；跨进程锚、走查锚 `-race -count=10`、`TestColdArchiveHoldsTheExclusiveGate` 全绿；**注释门 0 findings**（写权用例已并入既有 `cold_archive_gate_test.go`、新 `config_test.go` 删除，一文件一责任）。

**⚠️ 编排者诊断错误（自纠，须记住的毛病）**：我在派发令里断言「recover-kv `:103` 是只读扫描、`:179` 是 restore 写者」。**代理读函数体后反驳并附字节证据，我核实：代理对，我错。**
- `recoverOrphanOriginals`（`main.go:103`）在 `restore=true` 时于 **:162 执行 `tier.Store.Commit(...)`** → **它是写者**。
- `verifyRestored`（`:179`）只 `Keys` 回读 → **它是读者**。
- **错因**：我照搬上一份战报的角色判断，**没读函数体就写进派发令**。这正是用户反复纠正的「未追路径就断言」。代理若照令执行，会在同一函数内自锁拒绝。
- **代理的接线更优**：`WriteRights: restore` —— 干跑不取锁故可 beside 活运行时读，restore 取锁故被活运行时具名拒绝。**恰好是丙′③ 的意图**，编排者采纳。
- `migrate.go`：**:76 是写者**（复制目标，声明写权）；**:145 是读者**（`verifyMigratedRoot` 只 `Keys`/`Get`，其文档明写「opens the copy as a component view, which reads beside the writer」），不声明。

**剩余 7 处 assembly 失败（编排者跑全量取得精确位置，已派窄单）**：6 处同类＝夹具在前一视图未关时 assemble 新 run（`compress_checkpoint_test.go:203`、`host_delivery_checkpoint_test.go:751`、`host_delivery_test.go:1270`/`:2468`、`request_test.go:295`、`retired_state_hygiene_test.go:318`）；1 处非锁＝`TestCheckpointOffsetNotFactSeq` 的 **TempDir 清理失败** `unlinkat …/001/cold/kv: directory not empty`，**待查是否 `Tier.Close` 只关 Segments 而漏关缓存冷视图 `s.cold`**（真资源泄漏则修关闭路径，不得让测试容忍残留文件）。另 `compress_checkpoint_test.go:201` 先报「20 判定中 19 个在账里无判定事实」，须判是同一夹具问题还是独立缺陷。

**一条验收面问题（代理拒改断言并上报，正确纪律）**：`TestRestoreObservationThroughTheFormalEntry` —— 干净 close 现在落关闭检查点，重启走检查点恢复（`covered=1 chains=0`），而断言仍要 `chains=1 recovered=1`。编排者令其**对比 HEAD 版 `invoke.go`** 判定三态：(a) 测试偶然依赖 tier 从不被关闭，其期望描述的路径已不再发生＝验收面问题归编排者；(b) 落地引入了不该有的行为变化＝产品缺陷须修；(c) 其他附证据。**不得自行改断言。**

**代理另修（采纳）**：`invoke.go` 重复 close 不再对已结束视图补记检查点（否则二次 Close 报 `commit through a closed view`）；`rootFiles` 排除零字节 `.write-lock`（它是锁目标非日志字节）；删 `run_test.go` 未用 import（该文件接手时已不能编译）。

### R41 flock 落地收口：assembly 全量绿＋验收面裁定（2026-11-16）

**主要阻塞清除**：`go test ./assembly/ -count=1` → **exit 0，296.3s，236 顶层**。6 处夹具全修，一律用夹具**既有 `endRun`**（停 filer＋关 tier），不新造 helper，**断言文字零改动**。`memory/ledger` 0（55）、`recover-kv` 0（8）、走查两锚 `-race -count=10` 0、WriteRights 两锚 0、**注释门 0**。

**#7 真因＝编排者假设错了一半（代理纠正，采纳）**：我假设「`Tier.Close` 漏关缓存冷视图 `s.cold`」是 TempDir 清理失败的成因。**泄漏确实存在，但不是该 flake 的成因**——真因是 `restarted` 从未结束，其 filer（`checkpointEvery=1`）在测试函数返回后**继续写已被删除的根** `001/cold/kv`。证据：修复前 `-count=5` 有 2 次 cleanup 失败＋8 条 `were not relocated`，**且全部出现在 FAIL 之后**（打向已删根）；补 `endRun(restarted)` 后 `-count=10` → 0、该消息 0 条。**代理没有把两件事混为一谈**，明确注明 unix 下打开的 fd 不阻删，故泄漏另案。
- **泄漏另案已修（真缺陷，附 fd 计数证据）**：`FileSegmentStore.Close` 确不关 `s.cold`。探针（双 tier、冷 miss 后 fds=8）：无修复 Close 后 fds=**6**，有修复 fds=**5**（＝基线）。

**#1 的附带消息澄清（编排者先前疑为独立缺陷，不成立）**：`compress_checkpoint_test.go` 的「20 判定中 19 个在账里无判定事实」是 **`:209` 的 `t.Logf`，不是失败**；该测自身断言 `covered != 0` 正是「checkpoint 承载了它们」的证据。非夹具后果、非独立缺陷。

**验收面裁定（编排者读测试目的后裁，不凭名字猜）**：`TestRestoreObservationThroughTheFormalEntry` 的主题是**重启就链折叠所述的观测行**——其 doc 明写「states the tier it opened and **the chains it folded**」「a chain carrying no admission material is counted among **the chains dropped**」，断言核 `restore folded` 日志行含 `chains=1 recovered=1 dropped=0`（:76）与 `chains=1 recovered=0 dropped=1`（:105）。
- **代理判定 (a) 成立且附证据**：`fileClosingCheckpoint` 是 **HEAD 既有**（`git show 9547faca:assembly/checkpoint.go:799` 定义、HEAD `invoke.go` 无条件调用；本次 diff 只加 `closeFinished()` 守卫与 tier 关闭 defer）；HEAD 隔离树复跑同一子测 → **exit 0**（因 `first` 从未关闭故链被折叠）。故断言描述的是「从不关闭」的世界。
- **裁定：采 option 2，禁 option 1。** 改断言到 `covered=1 chains=0` **等于删掉该测的全部主题**（不再观测折叠），属验收面缩水，**明禁**。改为**夹具以不落关闭检查点的方式结束首个 run**（用既有 `endRun`，须先验证它确实避免关闭检查点），模拟「进程结束但未落干净关机检查点」这一真实重启场景，**保留折叠路径为被观测对象，断言逐字不动**。已派窄单，白名单只该一文件。

**当前树状态**：assembly／ledger／recover-kv 全绿，history 剩该 1 项（窄单在修）。**仍不得声称 CI 绿**——CI 未触发，CI-3 超时／CI-5 陈旧代理／CI-6 证据门各自独立待 09.R3 全量矩阵。

### R42 flock 落地完成（四套亲验全绿）＋N22 棘轮门红与归属（2026-11-16）

**flock 落地完成，编排者亲验四套**：`go build ./...` **0**；`go test ./assembly/ -count=1` **0（297.856s，236 顶层，0 FAIL）**；`./tests/integration/history/` **0（17.731s，18 顶层）**；`./memory/ledger/` **0**；`./scripts/recover-kv/` **0**。丙′ 六条前提全部落地，跨进程负锚红绿对成立。

**夹具偏离批准（理由成立）**：派发令指定的 `endRun` **不可达**——它在 `assembly/run_test.go:51`，未导出且属他包测试文件；而 `Run.Close` 是唯一导出关档路径且**无条件**调 `fileClosingCheckpoint`。代理改用**子进程跑首个 run、握手后 Kill**，flock 随进程退出释放。这**更真实地模拟「进程死亡未落关机检查点」**，且顺带实证了丙′④「进程死亡自动释放、无陈旧锁恢复」。**断言逐字未动**（代理以 grep 证明 diff 中无 `chains=1`/`restore folded`/`Recovered` 匹配），复用既有握手件未新造。history `-count=10` → 10 顶层/60 子 PASS。

**N22 注释棘轮门红——两个门只验了一个（新发现，CI 阻塞）**：
- 本仓有**两个** comment_policy 门：①**零容忍**门 over 产品包（`assembly api runtime memory features tool internal cli`）＝**0 findings 绿**；②**棘轮**门 over `POLICY_DIRS=(. examples/wechat-bot examples/cli-host)`（`lint.sh:36`，**含 `tests/`**）＝**42 findings、42 beyond baseline、exit 1**。CI 跑 `lint.sh`，故②红即阻合并。
- **归属已按干净基线复验分清**：`git archive 9547faca` 隔离树跑同一门 → **HEAD 本身 28 findings、28 beyond baseline、exit 1**（23 history／4 host／1 wechat-bot）。工作树 42（30 history／6 wechat-bot／4 host／2 acceptance）。故 **28 条是 HEAD 既有债、14 条是本轮新增**，二者都阻 CI，**不得以「非我引入」为由跳过 28 条**。
- **既有债为何一直看不见（与 CI-1 勘误闭环）**：该 CI run 的 `Lint` 步骤在 `Test (short)` 先红后**被 skip**（单A 在 09/design.md §八 CI-1【勘误】已实证）。故先前「comment_policy 155→0 已提交 c1a5d485」**只清了产品包零容忍门，棘轮门从未被验**。**这是「skipped ≠ 绿」的又一实例**，也是 CI 强烈暗示而此前被漏掉的一项。
- **已派单清偿全 42 条**，明禁：抬基线（`--update-baseline`）、改断言/阈值/测试逻辑、删测试或跳过、为消红把注释改得不真、为凑镜像新增生产文件、把锚改指无关 wiki 章。11 个文件逐个清偿，只改注释形状（含把过度声称改写为真话）。

**门形状知识（本轮学得，已写进派发令）**：test doc ＝ 一行意图＋**每条单行**的 bullet＋契约索引；`//   续行` 会被判散文段落触发 doc-not-brief；上限 160 runes（`main.go:1341`）；函数体内注释须在文档槽否则 free-standing；一责任不得跨文件分裂（responsibility-fragmentation）；索引用简体 `契约:` 非 `契約:`。

### R43 棘轮门归零＋N23 无条件释放 tier 是真缺陷（2026-11-16）

**棘轮门 42→0 ✅**：`go run ./scripts/comment_policy . examples/wechat-bot examples/cli-host` → **exit 0，0 findings，0 beyond baseline**；产品包零容忍门保持 **0**。11 文件逐个清偿，未抬基线、未改断言/界/阈值/逻辑。**真值修正采纳**：去掉迭代叙事（`no longer holds`→`does not hold`、`（不再按…派生）`→`（缺省不按…派生）`）；`smoke_test.go:4695` 的索引原指 **openspec change artifact**（禁指，归档后即死链）→ 改指已验证存在的 `docs/wiki/agent/event-flow.md#projection-lifecycle`。唯一非注释改动是把 5 处函数体说明迁入相邻 `t.Fatalf` 消息文本（门推荐的 free-standing 出口），断言条件零改。

**⚠️ 编排者验证范围缺口（自纠）**：我只验了 assembly／history／ledger／recover-kv **四套**就宣布「flock 落地完成」。**跑 `go test ./tests/... -count=1` 全树后发现三个包红**：`capabilities`、`execution`、`host`，共 7 个测试。**「四套绿」不等于「树绿」**——凡改动存储开档语义，验证面必须覆盖所有开存储根的包，不得抽样宣布。

**N23 无条件释放 tier＝真缺陷（编排者诊断，非夹具问题）**：`assembly/invoke.go:113-115` 的 `defer` **无条件**调 `r.closeStorage()`，**不看 `report.Outcome`**。而 **`CloseCut` 是正常返回，之后 run 仍须能提交**。
- **契约证据**（`tests/integration/host/quiesce_close_test.go:330-334` 测试自述）：「close beside a model still in flight answers **Cut as a normal return**」「the notification the released worker owes **lands as a refusal the account keeps**」「a restart over the same log **reads that refusal back**」。故 Cut 之后被释放 worker 欠的通知仍须**持久化为拒绝**；tier 被关则该提交失败、拒绝永不落地。
- **结果语义**（`invoke.go:610-619`）：`CloseBroken`＝`read.Unjudged != ""`；`CloseQuiesced`＝`Resting() && drained`；`CloseCut`＝其余。
- **实测后果**：`TestQuiesceRejectedCutAllowed`（`quiesce_close_test.go:491/:516` 拒绝集为空）、`TestWorkEndClearsNothingOnFailedDrain`（`attention_projection_test.go:358` 输出事实未提交）、`TestSendCrossSessionGates`（`:282`）、`TestNoticeConsumptionRecoveryMatrix`（`notice_restore_lattice_test.go:504` 超时，日志 `durable commit write failed … commit through a closed view`）。另 `TestCompressProductRestoresAfterRestart`／`TestDelegatedMaterialSurvivesRestart` 报写权冲突、`TestRecallEvaluation` 成因待判——**不得假定同因**。
- **裁定**：**只在 `CloseQuiesced` 释放 tier**（已静且已排空，不会再写）；`CloseCut`／`CloseBroken` 保留写权，进程退出由内核释放——**正是丙′④**。现行注释「a close that could not judge the run still gives its storage root up to the next writer」**理由为假**，须改写；`run.go:948-960` `closeStorage` 文档同查。
- **编排者负有部分责任（自纠）**：我在派发令写了「组件关闭出错也不得泄漏锁」，**未指定 outcome 条件**，代理遂过度泛化为「无论 close 如何结束都释放」。约束写得不完整，不能全归代理。

**已派精确修正单**：改 outcome 条件＋改写伪理由注释＋逐个判定 7 个失败的成因（outcome 相关／夹具顺序／其他），明禁弱化锁、给读视图声明写权、改断言。**验证面钉为 `./tests/...` 全树 ＋ 四套 ＋ 两门**，不再抽样。

### R44 释放时机是真分叉：裁一条不变量（2026-11-16）

**Quiesced-only 修好 4 个但打破 14 个**：`TestQuiesceRejectedCutAllowed`／`TestWorkEndClearsNothingOnFailedDrain`／`TestSendCrossSessionGates`／`TestNoticeConsumptionRecoveryMatrix` 转绿；但 **assembly 10 个 ＋ host 4 个转红**，全为 `assemble … another process holds write rights`，含走查锚 `TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint`（10/10 FAIL）。
- **同一 `Cut` 下两种相反需求**：①**有在飞工作**的 cut（飞行 worker 欠的通知须落成拒绝）→ 必须保留写权；②**只有静态未清账**的 cut（未答输入轮、未消费通知，如 `recovery_sites_test.go:138` 先提交无答案输入再 Close）→ 本进程不会再提交，重启需要写权。
- **代理纪律正确**：停在裁决点，未改断言、未越白名单、**未回退修复**（回退会掩盖发现）；两处夹具关闭加了 `CloseQuiesced` 前置断言而非仅查 err，使「前驱未落定」以具名原因暴露。

**编排者排除一个简单解（附根据）**：「`Run.Close` 一律不释放、由调用方显式关档」不可行——我核实 **`Run` 无导出 tier 访问**，`endRun`（`assembly/run_test.go:51`）能达 `run.tier` 只因同包；跨包夹具（host/capabilities）无法显式关档。而**新增只被测试消费的导出 API 正是 N2 自证结构**，禁。

**裁定：一条不变量，不是两个代理**
> **tier 恰在「本进程不会再向该根提交」时释放。**

「Close 返回时」与「Outcome==Quiesced 时」都是该不变量的**代理**，且都已被实测证伪。**候选判据**（Close 已算出的存活信号，非新造）：`len(flying)==0 && stopped`——无在飞 resident producer 且 writers 已停，则无论静态义务是否 remain 都释放；`stopCheckpointFiler()` 已在 Close 开头调用故 filer 不是后置提交源。**须实证**：若该判据漏掉某个后置提交源，查明并细化，但结果必须是**一条陈述的不变量**，**不得是逐测例外清单**（错误类别是发散集，断言面才收敛）。

**逐测归因（代理查明，采纳）**：`TestRecallEvaluation` **非 outcome 相关**——是夹具串序，`plain` 与 `counted` 两个 assembly 同时活在同一 root（日志仅一条 `/002` tier opened，第二次开档被拒于 `recall_evaluation_test.go:475`）。**编排者先前要求「不得假定同因、单独查」是对的。** `TestCompressProductRestoresAfterRestart` 同为串序。

**`TestDelegatedMaterialSurvivesRestart`＝独立设计问题（登记，不得改断言）**：裸 `Run`（无 Host 投递循环）**永远消费不了欠下的通知**，`Unsettled` 恒返回 `Notices:[{… the mailbox holds this notification unconsumed}]`（代理实测再驱动 caller 一轮、`Kind:settled` 通知仍在），故 Close 恒为 Cut。该文件已回滚零 diff。**这不是锁缺陷，是「裸 Run 能否结清通知义务」的语义问题**，须单独裁。

**验证面再扩（先前抽样仍不够）**：除 `./tests/...` 全树与四套外，加 `./runtime/... ./features/... ./tool/...`——它们也开存储根。已写进派发令。

### R45 释放不变量落地：全树绿（编排者逐项亲验）（2026-11-16）

**不变量（一条，已实现）**：**关闭恰在「本进程可能仍提交的东西无一件在飞」时把存储根交出——无 resident 消费循环仍在模型生产内、且无未确认停止的 writer 生产；两者任一仍在则保留，无论报告命名什么状态。**
```go
var flying []string                       // invoke.go:124
defer func() {
    if len(flying) > 0 { return }
    if stopped, _ := r.writers.Stopped(); !stopped { return }
    closeErr = errors.Join(closeErr, r.closeStorage())
}()
flying = r.loop.StopResidentsWithin(timeout)   // :137
```
**关键设计点**：writer ledger 在**释放时**读、不在判据处读，故最后一次读之后才开始的生产仍保留根。`Close` 红线列表加两条，`closeStorage` 文档同改写。

**为何能分开两种 Cut（机制解释，采纳）**：提交源是 `drive:`/`pass:`（`runtime/agent/consumer.go:236,474`）与 `model:`（`loop.go:1786`）注册。**静态未清账（未答输入、未消费通知）是耐久链状态，无 goroutine 无注册**，故两个读都说已释放。四个修复锚经 `host.Run().Close()` 且脚本 provider park 住模型，`markStopped` 不取消（`consumer.go:1913-1916`）→ loop 被命名 flying、writer 未关 → 保留，拒绝/输出事实在返回后落地。`TestSettleWakeUnified` 经 `host.Close` 先 `consumers.Stop()`（cancel＋join，`:1891-1908`）→ 被 park 的生产解绕、writer 确认 → 释放。

**编排者逐项亲验（全绿）**：`go build ./...` 0；**`go test ./tests/... -count=1` 0（13 包 ok，0 FAIL）**；**`./assembly/` 0（299.752s，236 测，0 FAIL）**；`./memory/ledger/` 0；`./scripts/recover-kv/` 0；**`./runtime/... ./features/... ./tool/...` 0（0 FAIL）**；走查两锚 `-race -count=10` 0；跨进程锚 0；**棘轮门 0 findings／0 beyond baseline**；产品包零容忍门 0。

**代理纠正上一份战报（采纳）**：`TestDelegatedMaterialSurvivesRestart` **不是设计问题**——它持两写者同根（首个 run 从未关，第二个 Assemble 同根）。夹具先关前驱即可（`delegation_material_restart_test.go:95`），**断言未动**。R44 登记的「裸 Run 能否结清通知义务」由此**撤销为夹具问题**。**代价登记**：该测 0.17s→2.2s，因裸 Run 永不消费欠账故烧满 2s close 预算——属小残留，归 10.R4 评估是否给夹具一个能结清的宿主。

**完整性限定（代理如实报，编排者补一点）**：判据完备性依赖 `Host.Close` 在 `run.Close` **之前** join 自己的生产者（`execWG.Wait`／`consumers.Stop`／`settle.Stop`／`retries.stopAll`）；未来若有生产者绕过 writer ledger 且不被 join，就会逃逸。**今天不存在这样的生产者。** 编排者补：**逃逸的失败模式是响亮的**（`commit through a closed view` 具名报错），**不是静默损坏**，故可检测、可归因——这使该限定可接受，但仍须登记为已知边界而非声称穷尽。

**11.R2 仍未完成**：P1 丙′ 已落地并全树验证；**P2 整事实一 Batch、P3 提交结果四分类（含 `journal_kv.go:139` 确定拒绝误包为不确定）、P4 领域等价门先于字节门**三项未做，框未勾。

### R46 11.R2 完成＋两处自裁批准（2026-11-16）

**编排者亲验全绿**：`go build` 0；`./assembly/` **0（232.348s）**；`./tests/...` **0（13 包，0 FAIL）**；`./memory/ledger/` 0；棘轮门 **0 findings／0 beyond baseline**。代理另报 `./runtime/... ./features/... ./tool/...` 0（17 包）、走查 `-race -count=10` 20/20 无竞态、跨进程锚 1/1、产品包门 0。**11.R2 已勾。**

**P2 落地形状（采纳）**：新增事实存储层整事实入口 `ledger.CommitFact(ctx, tier, FactCommit)`，五族合成**一个 Batch 一次 `kv.Commit`**；判重读与提交同在 `segments.mu`＋`originals.mu` 一个临界区，外层由丙′ 根 flock 挡第二写者。家族规划抽为无副作用的 `planEventLocked`/`planFactLocked`，发布抽为 `publishEventLocked`/`publishFactLocked`，`StoreEvent`/`Put` 改调之而公开行为不变。装配层只传 `FactCommit`（`facts.go:393`）不拼段布局；`journalKV`/`memKV` 不解释领域等价。**槽位与 `EventCount` 零改。**

**负锚红绿对成立**：回滚为两笔 → `fact_commit_atomic_test.go:224: the committing view holds [blob:refused-fact ref:refused-fact], want no key of a refused fact`（孤儿原文在、无 `{pid}:evt:`）；还原后 PASS。**非空转证明具备。**

**自裁一：第五值 `FactRefused` —— 批准，但须回写验收面**
- 四分类（新增／已存在／冲突／不确定）对「**已知零写入的确定拒绝**」**无槽位**：不是新增（什么都没落）、不是已存在、不是冲突（无身份/内容 clash）、不是不确定（确知零写入）。**塞进任何一个都会重造 G1.3 缺陷(a)**。故第五值是语义诚实，不是验收面缩水。
- **但不得静默漂移**：任务行与 spec 原写「四分类」，须回写为**五值并附理由**（超帧 `ErrJournalFrameTooLarge`、关闭后提交 `ErrCommitAfterClose` 归 Refused）。归 11.R3 同批或 10 域契约同步。

**自裁二：`landedSeq` 只在 `FactCommitted` 推进 —— 批准，但须补锚**
- **编排者读 diff 确认**：旧码 `err = StoreEvent(...); if err == nil { r.landedSeq[partition] = seq }`，而 `StoreEvent` 对重复返回 nil，故**旧码对冷层重复也白占一个号**。新码只在真落地时推进，`landedSeq` 从此如实追踪已落地量。
- **「槽位语义零改」的准确解读**：指槽位的**识别与排序**（partition/window/seq 单调且对新事实唯一），**不是「保留号段空洞」**。旧行为产生空洞，新行为不产生；两者对排序与唯一性等价，故不违约束。
- **须补锚**：「重复提交不占槽」目前无测试钉住，该纠正可无声回归。归 11.R3 补负锚。

**P4 次序本来就对，未改（G1.4 结论复核成立）**：补锚 `TestFactCommitJudgesTheDomainBeforeTheEngineBytes`——同一对 body `SameFact=true` 而字节不同，引擎答 `ErrContentConflict`、`persistFact` 答 nil、已提交字节不动。

**登记余项**：`file` 档仍逐键提交，多键具名拒绝归 **11.R3**（`AtomicStore` all-or-nothing 契约对每档成立或显式拒绝），不在本单白名单。

### R47 06.R2 完成：cut 写已收，但整趟退位仍无界＝法定停点（2026-11-16）

**编排者亲验**：`./memory/ledger/` 0；三锚 `TestColdCutStatesOnlyTheKeysItsPassMoved`／`TestOpenCostIsBoundedByTheCutItKeeps`／`TestColdOpenReadsOnlyPastTheIndexCut` **3 PASS**（旧锚仍绿，证帧重放界未被破坏）；棘轮门 **0**。代理另报全树绿（assembly 231.6s、tests 13 包、runtime+features+tool 17 包、走查组 `-race -count=10` 80/80 无竞态）。**06.R2 已勾。**

**cut 写已收为 O(本趟搬迁键数)**：`journal.index` 改一行一语句（JSON Lines），退位趟走 `appendJournalIndexCut` 只陈述 `l.cutOf(l.tail)`；热 `rewrite` 仍整份替换。红绿对成立：每趟写下 `[2190 2203 2241 2241 2241]`（冷史 5,140→25,700 B 仍增长）；回滚为整份重述 → `[2190 4334 6514 8694 10874]` 线性、exit 1，原文 `pass 2 wrote 6514 byte(s) of cut beside the 2190 of the first pass`。`journalIndexCutVersion` 仍 1，**旧单份切面天然是一行故无迁移**。

**⚠️ 但核心目标仍未达成，代理如实声明（编排者确认并升级为停点）**：
- **整趟退位仍 O(冷键数)**，成因是**两次冷开档**（`relocate` 复制前开、`verifyColdCopy` 校验前再开），**各付一次全冷索引物化**，与 cut 写无关。G3 实测一趟 `48.141ms`（`coldRead 13.763`＋`coldWrite 26.658`）。
- **关键区分**：**帧重放有界**（旧锚仍绿，我复跑确认）≠ **索引物化有界**。代理原话「**开档有界不等于趟有界**，本域不得据此宣称『恢复成本随历史有界』已达成」——**此自律正确，编排者确认不得把 06.R2 完成读作目标达成**。
- 收它须让**冷开档只读所需键**＝寻址式索引（键→span 单段读）或按需定位。G3 已在热路径否掉按需定位（枚举代价），冷路径无消费者支持该形态。**不在 06.R2 白名单与本案取舍内，须另立设计门。**

**代理自纠一处「量错面」（采纳并记为教训）**：红线初版量的是**文件增长**，而整份重述是**替换**故只增 2,180 B → **误绿**。改为量「本趟写下的末条语句字节」后红线真红。**这是「代理量错面导致假绿」的又一实例，由代理自己发现并修复**——与 N11（恒真界）、N6（陈旧代理）同类，须进 13 域的假绿形态清单。

**其余交付**：①切面维护面不变（唯一落盘点仍是退位），**具名退化**逐字为 `no index cut beside <dir> and this tier maintains no cut, so the open read the whole log`，经 `WithRetirement`→`cutMaintenance` 交引擎判定，故是配置事实；②`Load` **零改动**，成本表标「随存活键集、每键整表读，非 O(1)、非按需」；③13 行具名操作成本表落 `06/design.md §十五·一`；④冷热只改布局，未删事实。**未列入者视为未承诺。**

**余项（编排者处置）**：`cold_archive_test.go:537/:559` 文案仍写旧切面形状，不在该叶白名单故未改——归 10.R4 或 08.R1 顺带修。

**停点（R31 重大取向）**：本变更的**核心目标「恢复成本随历史有界」在退位趟上仍未达成**，达成它需要冷层寻址索引这一**超出当前 13 域计划的新设计决策**。已列候选与取舍交用户裁，不擅自扩范围。

### R48 用户裁定「不考虑工作量，最优雅的方向」＋G5 设计门（2026-11-16）

**用户裁定原话**：「不考虑工作量，最优雅的方向是什么」。故**不采便宜两步（乙缓存＋丙少开一次）**，改从纯度判据推导。

**编排者推导（交 G5 用读数检验，非既成结论）**：
- **病根在引擎的索引模型，不在冷层**：今天索引必须**全量物化**才能回答任何问题（开档＝重建整张表）。热层显得有界只因退位把热集 kept small——**那是靠搬走数据掩盖「索引必须全读」的结构缺陷**。冷层无物可搬故缺陷裸露。
- **乙丙是症状上省事、全局复杂度反增**：多一套缓存一致性、多一条热冷不对称规则。违「简单＝全局复杂度非某步少读几次」。
- **新纯度判据**：**派生载体不得强制全量读取。** 检查点是派生载体、索引切面也是派生载体，今天两者都强制全读。
- **方向**：索引本身**可寻址**（键→span 按需读，开档只读索引根＋尾），且**对两层统一成立**——符合「统一多态契约胜多端口」，不是给冷层开小灶。
- **推论（诚实说出，未裁）**：若开档与点读真有界，**退位存在的理由可能消失**——它今天既省不了磁盘（冷热同盘）也不界定枚举（走查仍跨冷全史，G3 已标）。最优雅形态可能是**一个日志＋可寻址索引＋无退位**。

**⚠️ 该推论反转两条已冻结裁决，编排者不擅自动**：①**D6.13** 水位制退位；②**S1** 症状锚 `TestJournalBytesBoundedByLiveSet`（钉热档**文件字节**有界）。且 **S1 本身可能量错面**——它量文件大小，而用户原症状是**成本**不是磁盘；append-only 日志天然增长，若开档成本有界则文件增长本身不是病。**与 N6 陈旧代理同类**。

**已派 G5 设计门（只调查＋隔离树原型，明禁自行反转 D6.13 或改 S1）**，五项交付：①**分层量索引物化成本**（两层、多个历史深度，**与帧重放分开**——前一单曾混淆二者）；②可寻址索引原型的**开档／点读／提交／按提交序枚举**四项读数（**枚举是已知反对理由**：G3 曾在热路径因枚举代价否掉按需定位，须判该反对对可寻址索引是否仍成立、独立的 append-only 序结构能否解）；③**退位还买到什么**（磁盘是否真同盘、枚举是否被界定，须实证）；④**S1 的面审计**（究竟量 os.Stat 文件字节还是开档重放字节，是症状还是代理，与 S2 对比）；⑤判决书分「已证／未证／何以settled」。
**度量纪律写进派发令**（本仓已被烧三次）：无法失败的界不是证据；不得量代理冒充本体（前单量「文件增长」而操作是「替换」故误绿）；区间不重叠不双计；区分实测与推导。

### R49 G5 判决：可寻址索引成立，退位的成本理由被取代（2026-11-16）

**源树身份**：rsync 当前工作树（HEAD 9547faca ＋ 34 未提交文件 +2855/−346 ＋未跟踪），排除 .git/examples/docs/.dev；ledger 文件 sha256 逐一相同；build/vet 0。主树零 .go 写、零 go 跑；**D6.13 与 S1 未动**。

**判决：可寻址索引方向成立（原型实测）。退位对成本症状变为不必要。是否反转 D6.13 属编排者/用户裁，G5 未自行反转。**

**① 物化成本分层实测（探针 A，4 深度，冷键 798→51198）**：**冷开档是索引物化不是帧重放**——cutParse 1.81→22.76ms、seed 56→1869µs，而 tailReplay ≤26µs。整份重放另量：readFrames 158µs→1.08ms（6.8×）vs fullPublish 1.22→20.88ms（17×）。**热开档在退位开时平（cutKeys=3，~1.6–3.9ms 地板）；退位关时 fullPublish 1.56→40.53ms（26×）——结构缺陷裸露**。整趟 wall 50ms→**2.10s**，ColdOpenCost 8.4→51.6ms（两次冷开档）。**热层的平是退位造成的假象，此点实测确证 R48 推导。**

**② 可寻址原型（探针 B，4 深度）**：open 3.03/3.02/1.72/3.25ms、point ~0.6–1.3ms、commit ~15–35ms、newestOfType ~75–121µs（walked=2）——**全平**。对照 materializingOpen 73→**4698ms** 线性，**证明该界可失败**（非恒真）。
- **枚举反对理由的裁定**：**对逐键定位成立**（变体 A：200ms→5.53s）；**对「独立 append-only 序结构＋内联 span」不成立**（变体 B：563µs→16.4ms，顺序读，比 A 快 ~200–350×）。**序结构解掉了 G3 当初否掉按需定位的理由。**
- **两种引擎的全量枚举都仍 O(历史)**；消失的是**开档物化＋冷开档**。此点须如实声明，不得宣称枚举有界。

**③ 退位还买到什么（探针 C，深度 6400）**：**同盘实证 hotDev=coldDev=16777233**，冷根＝`cfg.Root/cold`（`config.go:307`，唯一生产调用方；编排者读原文复核）。总字节 开 2.36MB vs 关 2.55MB——**那 −197KB 是冷复制重新分批，不是分层节省**。常驻索引 1 vs 6400；**枚举两边都访问 6400（未被界定）**；冷首点读 27.4ms（仍增长）。**买到的是：热档文件磁盘体积（＝S1 的面）、常驻索引 O(存活)、热开档 cut+tail——后两者被可寻址索引取代。**

**④ S1 的面（编排者读原文复核，两条都成立）**：`symptom_anchor_journal_test.go:89` 是 **`os.Stat(journalPath).Size()`——磁盘文件字节，不是成本**。其 doc :28 的理由「the bytes are the file the journal engine opens at boot (**openJournalLog replays 0..size**)」**已过期**：引擎现在重放 `cut.Offset..size`。**故 S1 钉的是退位的磁盘效果（一个已退役机制的代理），不是用户症状**——与 N6 同类。S2 量的是 JournalBytesOpen/IndexEntries/Scans/wallClock/ColdReadCost，是成本面且有非空守卫。**G5 建议把 S1 改锚到成本面或随 D6.13 一并退役，未自行改。**

**⑤ 未量项（如实）**：生产集成与不变量（R36 走查快照、崩溃、flock、原文族）；原型序等价 vs 真实热冷归并（构造上推导）；提交优化；崩溃一致性；真实 assembly boot 路径；失败退位成本。

**待用户裁的反转（编排者不擅自动）**：①D6.13 水位制退位是否退役；②S1 是否改锚成本面或退役；③**接受磁盘无界增长**——可寻址索引界定的是**成本**不是**磁盘**，去掉退位后热档文件随历史增长，若磁盘体积本身是要求则须另立压实机制（且不得破「链是唯一真相」）。**附带收益**：退位去掉后 `keptHotKeys` 的全量走查消费者也随之消失。

### R50 用户裁定：采纳可寻址索引，退位退役（2026-11-16，重大取向已裁）

**用户裁定原话（两次，逐字）**：①问「最优雅方向」时答「**不考虑工作量，最优雅的方向是什么**」；②G5 读数呈上后选「**采纳可寻址索引，退位退役（推荐）**」。

**裁定内容（据此冻结，不得再框甲乙）**：
- **采纳可寻址索引**：G5 变体 B——**独立 append-only 序结构 ＋ 内联 span**。开档只读索引根＋尾，点读按址取单段，提交 O(1)。实测跨 64× 历史全平，对照组 materializingOpen 73→4698ms 线性证明界可失败。**逐键定位（变体 A）已被否**（枚举 200ms→5.53s）。
- **D6.13 水位制退位反转：退位整套机制退役。** 其成本买点（常驻索引 O(存活)、热开档 cut+tail）被可寻址索引取代；唯一剩余买点「热档磁盘体积」不是用户症状。**同盘实证**（hotDev=coldDev=16777233，冷根＝`cfg.Root/cold`）故无分层节省；**枚举两边都访问 6400 条，退位从未界定枚举**。
- **S1 改锚成本面**：现面 `os.Stat(journal.log).Size()` 是磁盘字节，其 doc 理由「openJournalLog replays 0..size」已过期（引擎现重放 `cut.Offset..size`）。**与 N6 同类的陈旧代理**，须改锚或退役。
- **磁盘无界增长成为显式声明、单独拥有的关切**：压实/GC 另立，**不得破「链是唯一真相」**。本变更不承诺磁盘有界。
- **全量枚举仍 O(历史)**：两种引擎皆然，消失的是开档物化与冷开档。**不得宣称枚举有界**，须在具名操作成本表如实声明。

**编排者须先处理的安全问题（不得直接开删）**：**既有根的冷档可能已有数据**。退位读路径一旦移除，那些事实将不可读＝数据丢失。本仓已有先例两种形态：`memory/ledger/migrate.go` 的迁移，与 `rejectLegacyShardLayout`（`config.go:120-137`）的**具名拒绝旧布局**。**须先判定**：是否存在已填充冷档的根（含用户自己的 wechat-bot 部署是否跑过 dev 分支），据此选「一次性把冷事实折回热档」或「具名拒绝并指向迁移入口」。**未判定前不得移除冷读路径。**

**与硬约束的关系（须显式说明）**：全局规则「不下线旧机制——并存迁移」在此**被用户明示裁决覆盖**（用户明选「退位退役」）。但**数据可读性不在覆盖范围内**——退役机制不等于丢弃已提交事实，故上一条安全问题仍须先解。

**计划回写待办（编排者单写）**：D6.13 标反转并附裁定原话；08 域由「事实退位」改为「退位退役与冷档处置」；06 域交付改为可寻址索引；S1 改锚；G2 判据中依赖退位的部分（任务五族留热、`keptHotKeys`）须重核；具名操作成本表按新引擎重出。**历史证据只附勘误不改写。**

### R51 用户纠正：不是「退役」，是彻底删除零残留（2026-11-16，编排者用词与方案形状均错）

**用户纠正原话（逐字）**：「**准确的说不是"退役"，我们的所有变更都要0遗留0残留，不考虑框架兼容，最多考虑外部脚本辅助**」。

**编排者两处错误，自纠**：
1. **用词错**：我写「退位退役」，暗示 decommission-with-legacy-support。用户要的是**彻底删除**。
2. **方案形状错（更重）**：R50 我把「安全问题」框成三个框架内选项——一次性折回／**开档具名拒绝并指向迁移入口**／**保留只读 legacy 冷路径**。后两者**都是框架内的兼容分支，即残留**，被本裁定明确排除。我还要求 G6 产出「冷档检测谓词与具名错误」——**那正是要删的东西，不是要建的东西**。已停掉按旧框架派出的 G6 单，重派。

**据此冻结的形状**：
- **框架内零兼容分支**：不留 legacy 冷读路径、不留冷档检测谓词、不留 legacy 具名错误、不留退位配置开关。冷目录概念从框架完全消失。
- **零遗留零残留的覆盖面**：生产符号、配置字段（`RetireFacts`/`WithRetirement`/`WithColdRoot`/`cutMaintenance`）、**测试（删除而非适配）**、spec 条款、wiki/godoc 引用、具名操作成本表中的冷/退位行、以及**词汇**（存储义的「退位」借用须从文档清除；GLOSSARY:14 的「岗位交出去档案留在册」本义属另一义项，保留）。
- **数据搬运最多由外部脚本承担**：若操作者需要把既有冷档事实搬回，那是 `scripts/` 侧一次性工具的事，**不是框架机制**。框架不因既有数据而保留任何分支。
- **全局硬约束「不下线旧机制——并存迁移／回退后与基线零 diff」在此被用户明示裁决完全覆盖**（R50 只覆盖了机制下线、未覆盖兼容分支，现一并覆盖）。

**仍然成立的底线（不与零残留冲突）**：删除的是**机制**，不是**已提交事实的可达性要求**——但保障方式改为**外部脚本辅助**，而非框架内兼容层。G6 须据此给出外部脚本建议（可复用 `scripts/recover-kv` 形态），**不得把它做成框架内的开档分支**。

**S1 处置随此收紧**：退位既彻底删除，S1 的磁盘字节面失去被测机制，**改锚成本面或删除**，不得保留一个钉着已删机制的锚（那就是残留）。

### R53 叶级并发推进（2026-11-16，用户令「按叶充分并发推进」）

**叶账（附所用正则，N10 要求）**：`^- \[[ x]\] \*?\*?[0-9]{2}\.R[0-9]` 逐域扫描，**33 叶＝7 已勾 ＋ 26 未勾**。已勾：03.R1／04.R1／06.R1／06.R2／09.R1／11.R2／13.R1。

**发现残留（编排者自查）**：**05.R1／07.R1／12.R1／11.R1 四个设计门叶判决已出但框未勾**——锚只存在于隔离 `/tmp` 树。代理当时拒勾是正确纪律（锚不在仓里就不是证据），但**判决已出而叶长期开着本身是残留**。故派单把原型锚落地为仓内真测试。

**触碰集实测（`go list -deps`，非凭直觉）**：`memory/replay`／`tool/recall` **依赖** `memory/ledger`（故 04.R2 与 11.R3 相交，串行）；`runtime/state`／`runtime/task`／`features/evolution/governance` 对 `memory/ledger` **零依赖**（故 G2 锚落地与 11.R3 不相交）。

**本轮四单并发（触碰集两两不相交）**：
| 单 | 叶 | 面 | 关键约束 |
|---|---|---|---|
| G6 | 设计门（反转规划） | 隔离树＋06/design.md | 零残留形状；穷尽移除清单；**任何项不得标「为兼容保留」** |
| 11.R3 | 引擎一致性 | `memory/ledger/{rustviking_kv,config,atomic_store}.go`＋recover-kv | file 档逐键提交须满足 all-or-nothing **或具名拒绝**；补 11.R2 欠的两锚（五值回写、重复不占槽） |
| 13.R2 | 证据绑定 | `scripts/gates/*.py`＋`scripts/scenarios/*.py`＋两个 golden | A G4-2 残留（**按 file:line 归属**，驳回「不可分」）／B G4-3 闭包 CLI 负例／C 手填判词改读产物／D **N6 attestation 按新 boot 真实可观测重写**；**禁跑 go**（Go 面在改） |
| G2 锚落地 | 05.R1／07.R1／12.R1 | `runtime/state`／`runtime/task`／`governance` 测试 | **只落锚不修缺陷**（红锚是正确交付）；**明禁落地依赖退位/「任务五族留热」的判据**（R51 已反转），须列出被排除项 |

**计划回写已落（不依赖 G6 的部分）**：`design.md` D6.13 就地标反转并附裁定依据与原话出处，**前半「正确性＝identity-read＋全局提交序、存活权威是性能层」保留**——它正是反转的依据。08 域改性质、06 域交付改写、S1 处置、G2 判据重核**待 G6 清单**，届时一并回写。

**未派与理由（非悬置）**：04.R2 与 11.R3 同闭包故串行；01.R1／02.R1 触 `scripts/gates` 与 13.R2 相交故串行；05.R2／07.R2／12.R2／03.R2 待 G2 判据表集成（编排者单写）且部分依赖反转后的引擎；08.R1／08.R2 待 G6；09.R2 待新引擎定型（冷计量将删）；09.R3／13.R3／13.R4／F1–F4 属终验与放行，须全量矩阵与明示授权。

### R54 G2 三域锚落盘核销：17 落／11 故意红／0 勾（2026-11-16）

**编排者亲验**：`go test ./runtime/state/ ./runtime/task/ ./features/evolution/governance/ -count=1` → exit 1，**恰 11 FAIL，逐项均为具名故意红锚，无既有用例转红**；其写面注释门 `-no-baseline runtime/state runtime/task features/evolution/governance` → **0 findings**。

**红锚各钉一缺陷且非空转守卫齐**（`-count=3` 同因，代理给逐条原文）：缺口 A（runtime owed 3／recovery 2）、缺口 B（重发已结四段身份 `:1:progress:0`）、清收据即重新欠账、`stated()` 返全量（4→20）、人的拒绝不跨重启、身份改铸、别名塌缩、目录第二来源、写腿零报告／读腿致命、Count 2／DenialSeq 3、消费失败 0 行日志。

**注释门 3 findings 归因正确**：全在 `memory/ledger/atomic_store_test.go:312`（并发单 11.R3 的写面），代理以分 scope 实证（其写面 0、memory 3），**未把并发单的债算到自己头上，也未去改不属于自己的文件**。

**无一枚因 R51 排除（重要）**：`grep 'Retire|retire|cold|keptHot'` 在五份原型上**零命中**。代理另指出：05 答 2 里「五族永不搬冷」只是 (c) 论证的**前提**，删退位后平凡为真，**结论（三族不可清减）不变**。编排者采纳此辨明。

**R29 纪律做对了**：锚名全局改名（去 `TestG2*` 前缀）并**把三域 design/tasks 里的 `-run` 命令一并回写到新名**，避免旧正则零匹配静默 exit 0；判定与实测数字未改。落位改道避免 `responsibility-fragmentation`（并入同锚既有文件而非新建），`-run` 是包级正则故冻结命令不受影响。

**三叶未勾，理由逐条可核（正确纪律）**：
- **05.R1**：冻结正锚 `TestTaskAccountStatedSizeTracksLiveWork` 在 `assembly/`，主树 grep 零命中＝正锚缺席；另 R30 的 G2-T9 BLOCKED 未消。
- **07.R1**：**包内每枚冻结锚已落盘且按判决行为**；唯一残余是任务行的 `./assembly/` 生产接线消费者锚两条——代理**拒跑**，因 assembly 带未提交候选，用其结果判叶会把工作树候选当证据（违二节源树绑定）。**此条交编排者，处置见下。**
- **12.R1**：冻结正锚 S4（`TestConsumerBehindTheChainSurvivesARestart`）在 `assembly/`，未落＝验收面缺第一枚。

**编排者处置**：三叶的缺口**同一形状——assembly 侧锚未落**。待 11.R3 完成（memory/ledger 面释放）后派一单落 assembly 侧 G2 锚（05 正锚、12 的 S4、07 两条消费者锚），届时三叶方可勾。`TestGovernanceSliceRefusesToDropAnOpenApproval` 原样落盘未加强是对的——设计 §十五勘误 1 已裁它「按字面无法红」，**改写属编排者权限**，强化由 `TestOpenApprovalIdentityMustSurviveTheCut`（红）独立表达；该「无法红的锚」须按 R24 N11 同理处置（改判据或删），登记待办。

### R55 13.R2 撞上限：编排者诊断半改状态并窄派（2026-11-16）

**13.R2 撞 150-turn 上限**，末句停在「改判词来源中途」。编排者按「撞上限改主线程诊断再窄派」亲自核现场：

**三文件语法均完好**（`ast.parse` 逐一通过），非破碎半改。自测实态：
| 自测 | exit | 详情 |
|---|---|---|
| `test_check_runtime_evidence.py` | **0** | 绿 |
| `test_check_event_integrity_mutations.py` | **1** | Ran **47**（原 34），2 失败 |
| `test_check_dod_coverage.py` | **1** | 3/13 失败 |

**诊断（两类，成因不同）**：
- **dod 3 失败＝夹具未跟上，不是判据错**：G4-4（手填判词改读运行产物）**已正确落地**，但既有夹具不供运行产物，故新拒绝**先于**各测试原本要 exercised 的断言触发（期望 `is recorded 'not_killed'`／`the kill belongs to another assertion`，实得 `the verdict … stated only by this registry's own field`）。**修法是让夹具供产物，不得重排或关掉新拒绝。**
- **mutations 2 失败中有一枚是合法形态被打破**：`test_registry_accepts_a_literal_beside_a_shared_log` 是**保护合法形态的正测**（同轮多 case 共享日志），G4-2 的源位置匹配似把它一并拒了。**打破合法形态与漏洞本身同样坏**，派发令列为第一优先。另一枚是 happy-path 门测。

**窄派收尾单**，顺序钉死：①先修合法形态正测（放宽到接受合法形态、仍拒假绿）＋happy-path；②再让 dod 夹具供运行产物（**明禁削弱 G4-4**）；③才做剩余的 G4-3 闭包 CLI 负例与 N6 attestation。**明禁删/弱化保护合法形态的正测**；若期望确因合法行为变更而过期，须改期望并说明理由。

**流程教训（第二次撞上限，须改派发粒度）**：前次是 flock 收口单（14 处夹具＋语义修正），本次是 13.R2（四单元 A/B/C/D）。**多单元大单反复撞 150-turn**。此后**一单原则上只装一个单元**，四单元单须拆四单或明写「做到哪算哪、优先恢复一致态」——本次派发令已加后者。

### R56 13.R2 收尾核销：编排者诊断被驳回（2026-11-16）

**⚠️ 编排者诊断错误，代理反驳成立，我复核确认**：R55 我判「mutations 2 失败中一枚是 G4-2 源位置匹配打破了合法形态（共享日志）」。**依据只是测试名 `test_registry_accepts_a_literal_beside_a_shared_log`，我没读失败原文。** 实际原文是 `MUT-CAND-01.8-storage-success: stale mutation, the patch anchor occurs 0 times in assembly/facts.go`。
- **反事实证据（我独立复核）**：该锚 `tier.Originals.Put` 在 `HEAD:assembly/facts.go` 出现 **1 次**、工作树 **0 次**——因 11.R2 把 `persistFact` 改为 `ledger.CommitFact`（body 与 record 现一并提交）。**属 R21 共仓闭包污染，与 G4-2 无关。**
- **这是「从名字推成因、不读原文」的又一次复发**，与 R40 的 recover-kv 读写角色误判同类。教训：**失败原文优先于测试名**。
- **代理处置正确**：把 patch 重定向到新形体、删去已不存在的 companion 退出点（同一拒绝退出，非第二个），**缺陷 claim／`expect_assertion`／`expect_assertion_site` 逐字未改**。合法形态由具名用例验证仍受保护（`test_a_helper_bubbled_report_at_its_call_site_is_a_hit`、`test_a_nested_failing_case_is_a_hit` 接受；`test_another_assertion_of_the_same_failing_case_is_not_a_hit`、`test_literal_printed_by_a_passing_sibling_is_not_a_hit` 拒绝）。

**dod 3 失败确如 R55 诊断**（夹具未供运行产物）：`build()` 补写 `evidence/evidence.json`（`profile=event-integrity`、`mode=live`），`judge()` 传 `--mutation-evidence`。**G4-4 未弱化**，另增具名拒绝用例 `a verdict stated only by the registry field is red`，构成 before-red/after-green 对。

**R25 对 G4-3 的判断亦已过期（自纠）**：我曾记「包闭包与跨模块只有合成树单测」。现三形态**皆有 CLI 红绿对**：`test_a_selector_that_never_compiles_the_patched_package_is_rejected`（`not_in_compiled_face`／`outside the import closure`）∥ 同夹具绿例；跨模块 `test_a_patch_inside_a_nested_module_is_rejected`（`spans modules`）∥ 无嵌套 `go.mod` 绿例。

**编排者复跑四套自测全绿**：`test_check_runtime_evidence` **0／97**、`test_check_event_integrity_mutations` **0／47**、`test_check_dod_coverage` **0／14**、`test_run_runtime_scenarios` **0**。`check_dod_coverage.py` → **exit 1，20 gap**（比 R22 的 19 多 1，新增项即 G4-4 对手填判词的具名拒绝，**正确增长不是回归**）。

**N6 阻塞，理由正确（采纳）**：需**产品新增一条「托管 boot 按身份读回什么」的日志行**（`.go` 面，非其写集）。现存候选可观测 `assembly/checkpoint.go:420` 的 `relocated the facts the checkpoint settled settled=…` **本身属将删的退位面**，不构成合格替代。**代理拒造库存计数代理，正确。** 该日志行归反转后的引擎单（G6 清单落地时一并加），N6 随之解。

**G4 状态**：G4-1／2／3／4 **闭**（G4-2 含一行宽残留的显式声明用例）；G4-5（必需检查 ABSENT）／6（归档绕过）／7（本地预检不跑证据门）**未动**，属 13.R3 且需外部授权。**G4 仍非全闭。13.R2 未勾**，另欠 `check_runtime_evidence.py` 绑定面（本叶未写该文件，既有 97 例是否等价交编排者裁）、`race_check.sh` 豁免的有界例外条款、N7 白名单门。

### R57 G6 判决：可寻址索引设计成立＋两个新承重发现（2026-11-16）

**设计（`06/design.md §十七`，718 行，§一–§十六 未动）**：`journal.log` 保名保帧格式、**仍是唯一真相**；新增三个派生载体——`order.log`（每提交一行、内联值 span）、`idx/<namespace>/<hex(key)>`（**目录即持有键集**，删＝unlink）、`root.json`（`{seq,valuesSize,orderSize}`）。开档读 root＋stat，干净尾⇒**零写入**；否则丢弃派生 order 尾、由值尾经既有 `journalSpansAt`/`journalReplay` 重推并切撕裂帧。点读＝span 文件＋一次 `ReadAt`。提交＝帧→fsync→order 行→fsync→idx（每触目录一次 dir fsync）→root。枚举＝反向 64KB 块读、最新语句定持有性、**零值读**。

**崩溃一致性已原型（6/6 注入点收敛同一 root）**：停机的 5 键提交落 5/5 或 0/5；撕裂帧按水位切；**撕裂帧＋完整孤儿 order 语句⇒语句被丢弃**（负锚：order 永不得成第二真相）；root 字节被切⇒具名拒绝。不变量全保：原字节、全局提交序、一身份一事实、领域等价重交＝重复、flock 一根一写者＋R45 释放不变量、`CommitFact` 五值。

**⚠️ 新承重发现一（R48 推导漏了这层）**：**boot 有第二个物化面**。退位关时 `engineOpen` 5.80→29.15ms **且** `storeLoad` 2.02→10.40ms、`indexEntries` 400→6400（16× 深度）。**只做引擎可寻址索引，boot 仍 O(历史)**——`FileSegmentStore.Load` 自己也物化。§十七·三 设计了 store 去物化，但 `count`/`commitSeq`/`windows` 的载体形状**留为显式停点 S0，未猜**。

**⚠️ 新承重发现二（反转的正确性陷阱）**：`TailKeys`→`tailFactKeys`→`DurableCommit.Seat`，而 **`Seat` 按 `tail == nil` 分支**。删除退位后引擎尾常态是**空的非 nil 集**⇒走检查点路径且零年轻任务被读⇒**检查点之后的任务记录静默不入座**。须重锚到「最新框架检查点之后的有界 order 前缀」，并配负锚。**05 域须新增一叶承载此项。**

**原型成本（4× 轴）**：open 588→553µs、point 190→181µs、newestOfType 9.5→9.2µs、commit 20.0→20.2ms **全平**；对照 materializingOpen 430ms→1.29s、orderReplayOpen 948µs→3.04ms **增长**（证界可失败）。enum 762µs→3.06ms 线性，**如裁定所述不得宣称有界**。**⚠️ commit 20ms vs 今日 ~5ms＝原型有 4× 回归，采纳前须优化**；6400 深度基准点因预算未落，由 4× 轴＋G5 的 64× 承载声称。

**移除清单（零残留合规）**：~60 生产符号／7 文件；3 个 ledger 测试文件整删（1,500 行）＋1 history 文件；`cold_archive_gate_test.go` **保留其两枚 flock 锚但须改名**；7 测试＋helper `judgeOpenIsBounded`＋一条交错腿删；**1 个能力整体移除（7 Requirements／31 Scenarios）**＋8 spec 文件；28 openspec 文档；wiki **零**冷引用（3 行陈旧叙述改写）；GLOSSARY:14 本义保留；成本表 5 行冷/退位。**无任何项标「为兼容保留」。** 级联：`cutOf`/`reopen`/`published`/`IndexEntries()`（删后仅计量用）/`planFactLocked` 的 `settled` 标志。`settleGate` 与 `ErrColdArchiveNotQuiet` **零 Go 定义＝纯文档残留**。
**明确不删（不同义项/机制，避免过删）**：generation retirement、`Retention.Retire`、`headless-retired`、cache-cold 测试、`migrate.go` 与 shard-layout 拒绝。

**落地次序**：**S0 设计冻结 → S1 加索引（绿、共存）→ S2 store 去物化（S0 后才绿）→ S3 删退位及其测试（原子、不可拆）→ S4 删切面机制（必须在 S3 后：`placeIndexCut` 最后一个调用者是 `relocate`）→ S5 S1/S2 锚 → S6 spec/文档/门 → S7 全树复跑**。

**S1 症状锚：建议删除**。其面是磁盘字节、已属单独拥有的声明关切；改锚成本会与 S2 的 `journalBytes` 在更弱夹具上重复。若编排者保留，G6 给了可失败案例。

**外部脚本**：`scripts/fold-cold-root`（`recover-kv` 形态：默认干跑、`WriteRights: restore`、经新引擎校验），把冷事实按 `segmentIndexCarried.Commit`＋`settled:commit` 可恢复的原提交序折入**新根**。scratch 根用新根即诚实；**真实事实则该脚本是发布前的运维前置**。框架零分支。

**计划破损面**：08 整域；06 的 2.1/2.2/2.4＋06.R2 交付被取代；09 的 4.1/4.3/4.4/09.R2/R3＋S1；11 冷重复分支＋事实词汇；**05 须新增一叶（Seat 尾）**；**07 无 R51 评估**；10 词汇/死码；13 两个新假绿形态。

**并发漂移（须串行）**：G6 期间主树 `config.go` 被 11.R3 改动（`AtomicBatch`）——**S3 与 11.R3 必须串行**。

**未定项**：store 的 seat/window 载体形状（S0）；生产集成；**掉电未测（只测进程死亡）**；commit 成本未优化；6400 深度基准点。

### R58 11.R3 核销＋file 档后果裁定（2026-11-16）

**编排者亲验**：三锚 `-count=1 -v` → **exit 0，7 PASS**（3 顶层＋4 子，非空转）；`config.go:319` `AtomicBatch: cfg.Type != StorageFile`、`:299` memory 档 `true`。代理另报全树绿：build 0、vet 0、ledger 0（21.6s）、recover-kv 0、`tests/...` 0（13 包）、assembly 0（281.2s）、两门各 0 findings／0 beyond baseline。**11.R3 已勾。**

**file 档裁定：具名拒绝多键，不伪装原子（采纳）**。`rustvikingKV.Commit` 在任何 CLI 调用前拒 `len(Deletes)+len(Puts) > 1`，返回 `ErrBatchNotAtomic`（`atomic_store.go:78`），零键落地；空批仍只完成屏障、单键仍可用。另一支「满足契约」被否——CLI 无原子点，补偿回滚＝删已提交字节＋崩溃窗口，**违「链是唯一真相」**。故 all-or-nothing **对每档成立**（file 以拒绝兑现）。红绿对具备：守卫改回逐键 → exit 1（`the refused batch published "0:evt:1:1" = "first", want no key of it`），md5 复原 → exit 0。

**部署后果——编排者裁定：接受，并须写升级说明**。`StoreEvent` 3 键批、`Originals.Put` 2 键批 ⇒ `storage.type: file` 的部署在首个事实提交处响亮失败（锚 `TestTheFileTierNamesTheFactLayerItCannotCarry` 实测双店具名拒绝、CLI 零 mutation、`EventCount()=0`）。**编排者核实其影响面**：`StorageFile` 在 `memory/ledger` 之外**零非测试消费者**，shipped `tagent.yaml` 不用它——故这是**诚实的能力声明，不是破坏在用部署**。仍属外部可感知变更，须进升级说明；不得静默。

**「重复不占槽」负锚未落地，代理拒写假锚是对的**：`landedSeq` 在 `assembly/run.go:379`／`facts.go:400`（白名单外），而 ledger 侧等价断言**修复前即绿**（`commitSeq`/`EventCount` 本就不动）＝**假锚**，故不写。建议形状留 `design.md §八.5`，归后续 assembly 面单（与 G2 的 assembly 侧锚同批）。

**五值回写已落** `tasks.md:12`＋`design.md §七.3`（超帧/关闭后提交→Refused，四值不变）；**spec 不在其白名单，未同步，登记给 10 域**。

**如实登记两项**：①`TestNativeConversationRoundTrip` 首跑红（10s 未进模型请求），单测 0.368s、单包 48.97s、复跑 51.73s 全绿 → 13 包并行＋host 216s 负载抖动，非本叶（走 localfile 档），**未改断言或超时**；②file 真 CLI 腿 SKIP（无 `rustviking`，`storage_capabilities_test.go:333`），具名登记不冒充已跑。

### R59 四单并发（2026-11-16，用户令「充分并发」）

**并发度纠正**：R55 我写「一单一单元」时只想到拆小，**未同时提高并发**——用户指出「所以让你充分并发」。拆单元的目的是**更多单并行**，不是更少。据此重算空闲面并派满。

**闭包实测（`go list -deps`）**：`governance`/`approval`/`runtime/task` 对 `runtime/state` **零依赖**；但 `runtime/state` **依赖** `runtime/task` 本体，且 `approval`/`gov` 也依赖 `runtime/task` 本体。故 **05.R2 必须拆两半**：state 侧（缺陷 A＋`owedOfWork` 补 consumed 查询）与 task 侧（缺陷 B，`checkpoint.go:267` 只观察未结义务）分单，后者待 07.R2 完成再派。

| 单 | 面 | 单元 | 关键约束 |
|---|---|---|---|
| 05.R2a | `runtime/state/` | 缺陷 A＋A′ | **禁给 mailbox 切片加 Consumed 字段**（集合已重建，缺的是查询）；禁触 `runtime/task/` |
| 07.R2 | `governance/`＋`runtime/task/approval/` | N16→N17+N18 | **N16 优先且须真实 tier 复现**；N17 禁只补文件镜像（＝承认第二真源），须与 N18 同批设计 |
| S0 | 隔离树＋06/design.md | store 载体形状冻结 | G6 显式停点，**未猜**；须给 `Seat` 尾谓词与负锚（05 新叶） |
| S1 | `memory/ledger/{journal,journal_kv,config}.go` | 引擎可寻址索引（共存期） | **旧切面仍权威，新结构不得成第二真相**；**须优化 commit 4× 回归**（原型 20ms vs 今日 ~5ms）；**禁删退位/切面**（S3/S4 owns，S4 必在 S3 后） |

**S1 与 11.R3 已串行**（G6 报并发漂移：其隔离期间主树 `config.go` 被 11.R3 改动）；11.R3 已完成故 S1 现可派。

**待派队列（附理由，非悬置）**：05.R2b（task 侧缺陷 B）待 07.R2；S2 store 去物化待 S0；S3 删退位＋其测试（原子不可拆）待 S1；S4 删切面待 S3；assembly 侧 G2 锚＋「重复不占槽」负锚合批待 S1；04.R2 待 memory/ledger 面释放；01.R1/02.R1 待 scripts/gates 面释放；N6 待反转后引擎新增「托管 boot 按身份读回什么」日志行。

### R60 05.R2a 核销：判据收口为一条（2026-11-16）

**编排者亲验**：`go test ./runtime/state/ -count=1` → **0（0.344s）**；`mailbox_checkpoint.go` 中 `Consumed` **零命中**（未给切片加字段，符合裁定）；`settledLocked`（`durable.go:381`）确为唯一判据，被 `oweLocked:398`、`:415`、`:1145` 共用。

**修法（采纳，且比要求更纯）**：`Commit`（运行期）与 `applyRecordLocked`（恢复期）现同调唯一折叠函数 `oweLocked`，读唯一判据 `settledLocked ＝ consumed ∪ rejected`；**顺带把 `unowedLocked` 原本各写一遍的两条 map 查也换到同一判据**——原三处副本合为一条，无行为变化。这正是「同一判据不得留两份副本」的收口。
**A′**：`durableConsumption` 增 `settled(notice)` **查询方法**（非切面字段），`owedOfWork` 折叠前对**两个来源同样过滤**（切面载荷 `Owed` 与账户 `OwedNotices`），并对 `MemoryStore.consumptions` 可为 nil 做守卫。

**两锚红转绿且非恒真**：`TestRuntimeCommitMustNotReoweAConsumedNotice`（修前 `runtime commit owes 3 / recovery owes 2`）；新增 `TestMailboxSliceRestoreSkipsANoticeTheAccountSettledPastTheCut`（走 `StateRegistry.RestoreState`，与 `assembly/checkpoint.go:883` 同入口；修前 3 座位应 1）。**变异负锚**：把 `if settled(...) { return }` 降级为丢弃求值 → 同命令同原文红，复原后绿。`-count=10` 20/20、`-race -count=10` 无竞态。包内 69 RUN／0 FAIL（修前 exit 1）。

**缺口 B 未修未认领（正确）**：`TestCoveredTaskMustNotReissueASettledNoticeIdentity` 仍红，原文指向 `runtime/task/checkpoint.go:267-269`，在白名单外属 05.R2b。**A 的修复不改变它，两者不合成**（与 R34 一致）。

**⚠️ 最大未验证面（代理如实报，编排者登记为必办）**：**本包之外一次未跑**。`Commit` 不再为已结通知开账，**对下游消费者的影响须在写面释放后全树复跑**——这是行为变更，不是纯内部收口。归 S1 完成后的全树复跑批，**不得以本包绿代替**。

**05.R2 框未勾**（只完成 state 半边），正确。

**再派 01.R1**（scripts/gates 面已释放）：三挂点守护真实覆盖复核。**02.R1 阻于 12.R3**，不派。

### R61 07.R2 第一轮核销：N16/N17/N18 核心已修＋三枚红锚裁定（2026-11-16）

**编排者亲验**：两包 `-count=1` → exit 1，**恰 3 FAIL**（与代理所报逐枚相同）；N16 锚 `-count=10` → **ok**。build 0、vet 0、`-race` 无竞态、注释门 0。

**N16 已修（P1 红线，真实档位红绿对）**：写腿不再留洞、失败具名。未修树 exit 1、键 4 正文 `tool=fourth`→`tool=after-restart-b`（覆盖 1 条）；已修树 **exit 0 ×10**、存量正文逐字节不变、续写 5/6。负锚：`Sync` 未确认即停号、`Resolve` 未知 goal 不再取号。
**偏离批准**：计划「读腿报洞」→ 实际「**写腿使流不可能留洞**」。理由成立——冻结的读成本锚逐字符断言恢复只读 `cut+1…首次 miss` 且禁整流前缀读，读侧补扫即红。派发令本就允许另一条路，已记 design §十七。`Record` 保持 void（唯一调用方无法改变裁决，编译期形状断言钉住签名），具名落 ERROR 行。
**N17 已修**：已裁决请求跨有效期保留，×10 绿；`TestExpiredApprovalNeverRunsAfterRestart` 保持绿。
**N18 核心已修**：审批链经耐久面（`AttentionFactLog`／`tagent/attention/v1/fact:`／`WithApprovalFactLog`／`BindAttention`），注入点用**既有** `StateSeat.Bind`＋`assembly/governance_checkpoint.go:64`，**白名单外零改动**；构造顺序缺陷同批修掉。两锚红转绿 ×10。**未只补文件镜像，符合 R33 裁定。**

**三枚红锚——编排者逐条裁定（不整批采纳也不整批驳回）**：
1. **`TestDenialTailSeqIsNotTheRecordCount`＝锚的面自相矛盾，须重定（我读原文确认）**：其 doc 的 positive 写「尾序号与折叠记录数相同，故 Count() 可答尾」，negative 写「坏正文被跳过但仍取号，故两者分歧」；**夹具故意在键 2 写 `{not json` 造成分歧，而断言是 `if count != int(tail) { Fatalf }`——按构造必红**。代理说「转绿仅三条路（伪造记录／改答尾序号／翻转断言）均禁」是对的，**它不能自裁，但它漏了第四条：锚本身写错了**。
   **裁定**：重定不变量为——**`Count()` 与尾序号必须一致，且坏正文必须被具名而非静默跳过；`Count()` 不得被重定义为尾序号**。这既保住 R33 对候选甲的否决，又把「静默跳过」这个真缺陷变成可修的可失败判据，而不是把缺陷奉为期望行为。**归 07.R2 续单实施。**
2. **`TestOpenApprovalIdentityMustSurviveTheCut`＝夹具缺耐久载体，授权改夹具**：夹具建空 TempDir＋纯内存链、篡改只置 `Approvals=nil`，该身份在夹具里无任何耐久载体。**N18 既已落地 `AttentionFactLog`，夹具应给两本书注入同一 fact log**——这是夹具跟上产品能力，**断言意图（身份跨切面存活）逐字不动**。授权。
3. **`TestGovernanceSliceStatesTheValidStateNotTheHistory`＝07.R2 剩余范围，可做**：本轮未做载荷裁减。**R33 的「全量 Records 恰好掩盖洞」顾虑已随 N16 修复而消失**，故裁减现可落地。归续单。

**未验证面（登记必办）**：`./assembly/`、`./tests/integration/**` 未跑（并发单在写），故任务行两条生产消费者锚**不声称仍绿**。**07.R2 未勾**，正确。

### R62 01.R1 覆盖审计：门自身的验证曾不完整（2026-11-16）

**编排者亲验**：`test_check_plugin_mounting.py` → **exit 0**；`check_plugin_mounting.py` → **ok**，`go_files=30 containers=2 mounts=0 consumers=1 exceptions=0 registry_impl=2 present=2 errors=0`；5 个未覆盖面已记 `01/design.md §10`。

**编排者派发令框架错（自纠）**：我写「逐条核 ADR 0006 的 R1–R4」，但**R1–R4 住在别的门里**（本单白名单外）；本门实为 **R5＝R5a＋R5b C1–C4＋注册/注册表形面**。代理正确识别实际范围并按其构造违规，未照我的错框架硬凑。

**门自身验证曾不完整（实证，重要）**：把每条规则降级为 `print` 做变异——**修前 12 个变异有 6 个自测仍绿**。成因：①空推导用例与**整个注册/注册表形面零用例**；②C1 用例被「**打印出来的消息**」满足，而 exit 1 由**另一条规则**提供（现改为断言 `errors=2`）。**修后 20/20 变异使自测红**（基线 exit 0、24 例）。这正是「机制验证须完整＝穷举谓词×每谓词负变异」的实证：**正面枚举证明不了完整**。

**5 个已证未覆盖面（注入夹具证明，非推测）**：
1. `registry_implementation` 文件内的硬编码 append——**整文件豁免**藏住 `append(r.plugins, r.compress)`；今日休眠（去掉豁免只多出 `plugin_catalog.go:388` 一个 filter）。
2. C4 接受 `range native.Plugins` 遍历实例集——**实现量的是字段名，spec 要求名字集迭代**。
3. `var x []plugin.Instance`、4. `x := []plugin.Instance{…}`、5. **函数参数容器**均不被推导——**直接违反 spec 的「新增容器 SHALL 自动落入判据范围」**。
闭合方案（代理已实测安全）：把整文件豁免收窄为形状豁免；C4 改类型感知或把其证明移到 Go 侧；扩 `PIPELINE_LOCAL_RE`（实测只多识别 `instances`，违规集仍空）。

**合法形态仍被接受（具名）**：spread 转发、已解析集合的整体赋值、range 消费者、已登记例外、带 tag 结构字段仍被推导（字符串被掩码）、7 builtin 经 catalog＋agent 作用域禁用/扩展。

**01.R1 未勾**：消费者锚的 Go 半（`assembly/plugin_mount_test.go:71/:97`）本窗口禁跑 go，**未跑即不勾**，正确。归全树复跑批。

**派续单闭 5 个面**（scripts/gates 面已释放，与 S0/S1/05.R2b 不相交）。**其中 3 个是 spec SHALL 与实现反向**，属必须闭合而非可选加固。

### R63 05.R2b 核销：缺口 B 已修，新锚以变异证鉴别力（2026-11-16）

**编排者亲验**：`runtime/task/checkpoint.go:270-275` 现为 `for _, mutation := range stated.Mutations { for _, envelope := range mutation.Outbox { r.seq.Observe(envelope.Notice) } }`（我读原文确认）；`restore.go:288-293` **未改**（折叠路径本就观察每条 fact 的 outbox，已符合不变量）；目标锚 `-count=10` exit 0；下游 `./runtime/state/` **0**。

**不变量（采纳）**：恢复交给 sequencer 的身份集＝账户**已提交的全部**通知身份（task/round/kind/seq），**与服务端是否结清无关** ⇒ 两条恢复路径对同一账户发出同一身份序列，重启后 `IssueNotice` 不复用已用身份。**观察集不缩的论证**：义务信封恒出自某条已提交 mutation（`lifecycle.go:387-395`、`restore.go:281-295` 均先入 `mutations`）。

**新锚的设计值得记（正面榜样）**：`TestCoveredTaskKeepsTheIdentityOfANoticeItStillOwes` **两套代码常态皆绿**，其鉴别力由**变异**证明——把观察集改为「outbox − 未结义务」则**它红而冻结锚仍绿**。故它钉住的是「已结＋未结同源」，防的是**过度纠正**（只观察未结的会漏已结，只观察已结的会漏未结）。**一个两版皆绿的锚必须用变异证明有牙**，否则就是恒真界（R24 N11 同类）。

**包内其余 2 失败逐条归属（均修前同红）**：`TestConsumptionFailureIsReportedByName`＝12 域 G2-C2 冻结负锚（`12/tasks.md:85` 记故意红）；`TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`＝本叶清减面未实施。**非本单引入，未去「修」它们**，正确。

**05.R2 未勾（正确）**：该叶还含**候选甲**（按任务键索引／`ReceiptsOf` 降为 O(该任务)）与**清减面**，其后半冻结负锚仍红。②完成、①③未全。**注意**：候选甲的成本前提须按 R51 反转后的引擎重核（原论证依赖「五族留热」，删退位后平凡为真但索引形态变了）。

**再派 07.R2 续单**（`runtime/task` 面已释放）：按 R61 三条裁定闭三枚红锚。

### R64 01.R1 五面闭合＋上一单「20/20」声称被推翻（2026-11-16）

**编排者亲验**：`test_check_plugin_mounting.py` → **exit 0**；`check_plugin_mounting.py` → **ok**，`go_files=32 containers=5 mounts=0 consumers=1 exceptions=0 registry_impl=2 present=2 errors=0`（原 30/2，**注册表文件已纳入扫描**）。

**五面全闭，各有 before-accepted／after-rejected 对**（夹具注入忠实子集树，基线复现真树 30/2/0/1/0）：①整文件豁免→**形状豁免**（range 绑定非字面量源），注入 `append(r.plugins, r.compress)` 由 exit 0 转 `R5a … takes a concrete member 'r.compress'`；②C4 改**类型感知**，`range native.Plugins` 由 exit 0 转具名拒；③`var x []plugin.Instance`、④`x := []plugin.Instance{…}`、⑤函数参数容器均由 exit 0 转具名拒。

**⚠️ 上一单的「20/20 变异全红」声称不成立（本单实测推翻）**：真实基线是 **17/27**——**10 个 emission 根本没有用例**。本单闭了那 10 个，**另加它自己 C4 改动引入的 2 个「用例被邻居的 exit 满足」通道**（现断言 `errors=3`/`errors=2`）。**现 28/28**。自测 **24→42 例**。
- **教训升级**：R62 记的「修后 20/20」是**上一单的自报数，未经独立复算**。变异比例这种覆盖元门数字**必须由复算得出，不得采信自报**——与「凡报必验」同律，且**新改动可能自己开出新通道**（本单 C4 就开了 2 个），故每轮改门后须重跑全量降级变异。

**Gap 2 选型理由（采纳）**：类型感知留在门内，**不移到 Go 侧**——Go 锚落在本窗口外的文件且不可跑，即**不可验证的锚＝又一假绿通道**。可判定性在**真树规模**证明：把 `AgentSpec.Plugins` 改型为 `[]plugin.Instance` 则真消费者被拒（`consumers=0`、exit 1）。

**合法形态全保且收窄精确**：spread 转发、已声明实例赋值、range 消费者、已登记例外、**注册表实现文件内的已解析集合 filter（`:388`）**、带 tag 管道字段仍被推导。**对照实验**：清空 `registry_implementation` 后两版门都**恰好多出 `plugin_catalog.go:388`** ⇒ 收窄是精确的，不是放宽。另有「注册表实现外的转发元素被拒」钉住豁免不外泄。

**⚠️ 三个新残留只活在战报、未落持久载体（编排者 grep design 零命中，故在此登记并令补）**：
1. **laundering**：先绑定 slice 字面量再逐元素 re-emit，可绕过推导。
2. **分组参数** `(a, b []plugin.Instance)` 只识别最后一个名字。
3. 注册表条目指向**缺失文件**（沿用旧行为，未改）。
另 Go 消费者锚 `assembly/plugin_mount_test.go:71/:97` **仍未跑**，故 **01.R1 仍不勾**。`check_doc_budget` 报 −2 行来自他单编辑，其文件不在本单范围。
**处置**：三项归 01.R1 再续单（与 Go 锚复跑同批）；**须先写进 `01/design.md` 再动手**，不得只留会话。

### R65 07.R2 续单核销：三锚闭＋两处偏离批准（2026-11-16）

**编排者亲验**：两包 `-count=1` → **0**（governance 0.787s、approval 3.033s）；`approvalRequestPrefix = "tagent/approval/v1/request:"` 确已落 `attention_log.go:52`；新负锚 `TestRequestRecordKeepsTheIdentityItsKeyNames` 存在。代理另报 28 顶层／28 PASS＋24 子、`-race -count=10` 无竞态、注释门 0。

**#1 重定不变量已落地，两面可构造失败均实测**：`governanceEventsPast` 对坏正文**返回具名错误并放弃整次折叠**（`denial.go:382`），不再 `continue`；`Count()` 保持 `len(records)`。降级为 `continue` → 红并点名 `denial:…002`（`Count()=2 beside DenialSeq=3`）；`Count()` 改 `int(l.evtSeq)` → 红（`= 20 beside a cut that states a tail of 20 and no record`）。**未伪造记录，夹具三键原样。** 新判据有牙，符合 R61 要求。

**⚠️ #2：编排者裁定前提被实测推翻，批准其扩范围（自纠）**
- 我裁「夹具注入同一 fact log 即可，因 N18 已落 `AttentionFactLog`」。**实测只注入 fact log 则身份不可解析**：`approval: no approval request names that identity`。根因是**产品缺口**——`Decide/Lookup/Pending` 全从 `a.evidence` 起读，而 fact **只载 presence/ruling，不载请求本身**。
- 故它补了**请求记录族** `tagent/approval/v1/request:<asked>`（`Request` 写、构造与 `BindAttention` 折叠）＋新负锚（删判据即红）。**批准**：这本就是 N18「审批经链耐久、不另立文件权威」的组成部分，**上一轮的「N18 核心已修」实为不完整**。
- **我的错**：把「核心已修」当成「能力已存在」，据此裁了一个前提不成立的夹具修法。**教训：裁定「夹具跟上产品能力」前须先证该能力真的存在**，不能凭上一轮的完成声称。

**#3 形状批准（比「删记录」正确）**：非单纯删记录——那会使重启后 `Query(0)` **静默变短**，违 G2-G6，且 `governance_recovery_test.go:278` 明写「refusal kept across the restart」。实际形状：**切面只载尾 ＋ `foldHistory` 按座定切面从流派生索引**（签名未改）。载荷 `bytes 2818→533`、记录 0 条、approvals 恒 1、尾 4→20；**变异把 `Records` 加回 → 1004→2845 且锚红**。**未丢欠项**：恢复后未绑流 `Count()==0`、绑定后 `Count()==20`，已裁决审批照旧陈述（N17 绿）。

**验收面改写批准（属重述非缩水）**：`TestGovernanceSliceSnapshot` 两条记录断言改为「切面不载历史、绑流后同两条同序可查」——**意图（记录仍可得）保住，来源位移**，判据未缩。须记勘误。

**goal 已结项未移出载荷（正确纪律）**：G2-G12 仍未证，**不做未证裁减**。登记为 07 域余项。

**07.R2 未勾**：其消费者锚含 `./assembly/` 与 `./tests/integration/host/`（29 处消费点未改未跑），本窗口不可跑，**未跑即不勾**。归全树复跑批。

### R66 S0 冻结：座位入链，且纠正 G6 一处声称（2026-11-16）

**冻结形状（`06/design.md §十九`）**：`count`/`commitSeq` 搭**链内座位键** `store:seat = {count,commitSeq}`，**与事件族同帧写入**、开档**一次点读**取得。`windows` **不新增载体**——既有 `{pid}:meta:{window}` 链键按需读（`nextWindowMeta`，每提交一次点读），**常驻表删除**。座位不可用（缺失/损坏/`commitSeq<count`）⇒ 统一从 idx 条目重推（`max(count,maxPublishedCommit)`，镜像今日 Load），**仅在持写权时**以一帧重述。
- **增长轴**：开档 O(1)；窗读 O(1)/提交、开档零；提交 O(1)（**座位增加零个耐久点**）；重推一次性 O(历史)；枚举 O(历史) 不变。
- **被否两案**：链外座位文件（崩溃漂移强制全量重推＝正是该缺陷）；order 内联推导（**引擎不得解释 idx 语义**）。
- ** elegance 所在**：座位在链内故与事件同帧原子，**既非第二真相也不加耐久点**。

**实测（400/1600/6400，16×）**：`storeLoad` **310.8→185.0→168.0µs 平**；commit 1.43→0.86→0.735ms。**增长对照证界可失败**：materializingLoad **223ms→782ms→3.12s**；窗表整读 148→358→1386ms；全 order 尾 770µs→3.22→12.2ms；整表重写字节 **37,161→152,361→622,761B 线性**。差分：座位开档在 clean/orphan-tail/orphan-mid/orphan-recommit/crash/torn/rederive/corrupt 八夹具上**等于今日 Load**。

**崩溃一致性 6/6 收敛**；含座位的 6 键帧 **6/6 全有或全无**、座位==Load；撕裂尾±孤儿 order 语句⇒座位不变、撕裂键不在；无座位/坏座位重推；新根零写入。**仅进程死亡，掉电未测。**

**⚠️ 纠正 G6 一处声称（采纳）**：座位尾谓词＝按 order 新→旧收集键直到最新**框架**检查点，识别靠**所载 `type==TypeCheckpoint ∧ session==""`，不是键拼写**。**G6 的「零值读」声称错**——会话检查点与框架检查点**共用 `CheckpointKey` 拼写**，且 API 禁拼写识别。**找不到⇒nil（永不为空集）**，直接解掉 G6 发现的 `Seat` 按 `tail==nil` 分支陷阱。负锚已演示：水位尾命名 **0/3** 年轻任务；拼写停法**丢掉 taskA/taskB**（藏在更新的会话检查点之后）；nil 回退 walked=61。

**源树身份**：HEAD 9547faca；`segment_store.go` sha 与 G6 逐字节同（⇒S2 未开始）；`config.go` 在并发 S1 下漂移已记录；全 sha256 落 `/tmp/recovery-s0/ledger-source-sha256.txt`。

**未证项**：生产集成（真 Load/CommitFact/boot 红绿）；掉电；commit 成本未优化；**带外裸写活根产生的「陈旧但可解析」座位无守卫**（属协议违反，外部脚本必须写新根）；**座位接线属 05 域**。

**S2 解锁**（S0 已冻结），但与 S1 同触 `memory/ledger` 故**待 S1 完成**。

### R67 S1 核销：结构成立但载体形态有 5–6× 提交回归（2026-11-16）

**编排者亲验**：`./memory/ledger/` **0（26.771s）**；`-run AddressableIndex -count=1 -v` → **8 顶层 PASS**。代理另报 73 顶层＋37 子 0 FAIL、`-race -count=10` **140 PASS／0 DATA RACE**、`./tests/...` 0（13 包）、两注释门 0／0。**8 个具名变异各自转红**（如 `1 of 5 key(s) landed after the stop`、`the walk named the orphan key`）。

**共存兑现（采纳）**：`read/keys/namespaces/eachKeyNewestFirst/journalKV.Read` **一字未改**，**无任何生产读路径改走新结构**；`CommitFact` 五值、槽/`EventCount`、flock、R45 释放不变量、R36 走查快照均未触。读取面 clean open **6,495 字节恒定**（深度 10/40/160），控制案（删 root）**6,495→25,847→103,249 线性**⇒界可失败。

**⚠️ 生产接线未启用，且这是正确处置**：开启则 **4 枚装配锚红**（`TestRestartRecoveryPinsBeforeTrim` FAIL 120.5s `commit filler 1359: context deadline exceeded` 等）。**隔离法归因**：只关接线、同命令重跑 → PASS 13.03/16.03/18.14/1.11s。`TestGovernanceSliceWiredInProduction` 接线关仍红（另域并发叶）⇒归属清楚。**它没有改锚去迁就回归**，而是停下请裁——正确。

**⚠️ 提交成本 5–6× 回归，且 R57 归因被纠正**：24.1–26.1ms vs 今日 4.1–5.0ms。**主项不是 per-dir fsync**（仅 ~7ms/24ms），而是**冻结载体形态本身**——`idx/<ns>/<hex(key)>` **一键一文件**，APFS 实测 **1.4ms/键**，5 键≈14.7ms。**G6 的「目录即持有键集」在优雅上成立、在文件系统成本上不成立。**

**编排者裁定**：
- **选项1（接受 5–6× 并改装配锚）明禁**——改锚迁就回归正是本轮一直在拒的形态。
- **派测量单裁选项2（改载体形态）／选项3（idx 惰性物化），并加编排者从原则推导的第四案**：**`order.log` 每条语句已内联 span，且最新语句定持有性 ⇒ `idx` 与 `order.log` 是同一持有键集的两份表示**。按「不得有第二真相」「更少特异化代码」，**idx 应可整体删除**，`Keys`/`Namespaces` 从 order 派生。须量三案的提交/点读/枚举成本与崩溃一致性，按读数＋纯度裁，**不凭优雅偏好裁**。

**root.json 自裁批准**：改**原地写、不 rename、不 fsync**（偏离 §十七·二）。论证成立——order 每条语句内联 `valuesEnd` **自证水位**，root 只是加速器；order 与 idx 目录 fsync 保留（后者丢失无可检测面）。实测省 13ms（31→24）。崩溃 6 注入点→**5 个可区分字节态**（②④的 fsync 在进程死亡模型下与①③字节全等，**如实声明**），逐态收敛：`before_the_frame`=0/5，其余 4 态=5/5 且水位同一 root。
**`rejectLegacyShardLayout` 放行本构建的 `idx/`**，变异证明（删该行→`holds the retired shard layout …/idx`）。

**未做（登记）**：装配全量**未在关接线后重跑**（只逐枚复跑四枚）；掉电未测；S2/S3/S4 未动。**故不得声称树绿或反转完成。**

### R68 05.R2 成本面核销＋冻结锚判据重定（2026-11-16）

**编排者亲验**：`./runtime/state/` **0（0.389s）**；两新锚 `-count=10 -v` → **20 PASS**；提议不变量已落 `05/design.md §十七·4`。

**成本前提重核后成立，且与反转无关（实测）**：`ReceiptsOf` **只读三张内存表、零日志读**。单任务查询 20.9µs→168µs（修前）vs 9.8µs→8.5µs（修后）；**完整切面**（每任务调一次，即 `StateSettleAccount` 形状）**63.5µs→36.3ms 修前呈二次** vs **21.8µs→2.79ms 修后呈线性**。推导（未实测，已标）：`journalLog.keys` 遍历整索引并排序故按身份读回每任务是 O(全部键)，引擎 `Keys(prefix)` 是目录读 O(该前缀键数)。**故反转改变的是「省略收据族」那一面的成本（更便宜），不是让「按任务索引」作废。**

**已落地（采纳）**：`receiptsByTask` ＋ **唯一归档入口 `holdReceiptLocked`（12 处写入全经它）**；`ReceiptsOf` 改按任务查询。两新锚：`TestReceiptsOfAnswersTheReceiptsOfOneTask`（**5 个归档点逐条变异全红**）、`TestReceiptsOfCostsOnlyItsOwnTask`（同任务答案成本比 ≤4×，实测≈1×；**改回整账户走查则 92× 且红**）。落位并入同锚既有文件避免 responsibility-fragmentation。

**⚠️ 冻结锚没牙，且两判据互斥（代理两向隔离实验，我采纳）**：`TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`——
- 向一：把夹具 `[]` 改 `nil` ⇒ **锚通过，零生产改动** ⇒ **该锚可被夹具平凡满足＝没牙**（与 R54/R61 的锚病同类）。
- 向二：让 `Seat` 忽略载荷三族 ⇒ 锚通过，但 `TestDurableLoadReadsTheTaskSlice`、`TestDurableLoadRefusesAMismatchedReceiptIdentity`、`TestSeatedAccountAgreesWithIdentityRead` **三枚绿锚同时转红**。
- **两判据互斥**，且该文件在其白名单外，故它**未改写而上报**——正确。

**编排者裁定：批准提议不变量，并授权改锚与该夹具文件**
- **A 载荷权威**；**B 清减只能「省略」族、不能「陈述为空集」**；**C 成本面已落地**。
- **关键区分与 S0 跨域一致**：**`nil`＝未陈述（须从链派生）；`empty`＝陈述为无（权威）**。这正是 S0 座位尾谓词「找不到⇒nil，永不为空集」的同一区分（R66），**不是本域特设**。故 B 的可失败判据＝「省略族后仍能从链读回同一收据」，而「陈述为空集却链上有」必红。
- 授权写面加 `runtime/task/task_slice_receipt_anchor_test.go`；重定后须**两向都验**：省略族→绿且链读回一致；陈述空集而链上有→红；且三枚既有绿锚保持绿。

**05.R2 未勾**：③只完成成本面，清减面锚判据待重定实施。

### R69 01.R1 三残留闭：同族又找出三个变体（2026-11-16）

**编排者亲验**：自测 **exit 0**；真树门 **ok**（`go_files=32 containers=5 mounts=0 consumers=1 exceptions=0 registry_impl=2 present=2 errors=0`）；残留**已先写进持久载体** `01/design.md §10.4/§10.5`（grep 命中），§10.3 附勘误；**跨过程洗白如实登记未闭**（grep 命中）。

**三残留各闭，各有 before-accepted／after-rejected**：①洗白——转发判据对两种实参形态用同一前提（展开操作数／range 源不得是本文件从 `[]plugin.Instance{…}` 绑定的名字，含别名定点闭包与下标取基名；展开操作数不得是复合字面量）；②分组参数按 Go 分组语义收名字列表，`containers` 4→5；③悬空注册具名拒 `registry_implementation[0] 'assembly/absent_mount.go' does not exist`。

**⚠️ 它自己又找出同族三个变体（原报告未列）**：1c 展开一个名字、1d 展开复合字面量、1e 展开下标表达式——**实测同族可构造且修前被接受，一行为代价即绕过**。同批闭合，**未缩扫描面**（真树容器名集与文件集修前修后逐名相同）。这是「按语义单元枚举而非按表象类别」的正面执行。

**覆盖元门数字这次是复算的**：用例 **42→50**；变异比**实测 29/29**，且**用同算子复算修前得 28/28**（即复核了上一单的数，符合 R64 教训）。**未开新通道**：5 条谓词变异各只令自己的用例转红。

**合法形态全保并具名**：spread 转发、已声明实例赋值、range 消费者、已登记例外、注册表实现文件内的已解析集合 filter（真树 `plugin_catalog.go:388`，range 源 `contributed` 非字面量）、带 tag 管道字段仍被推导、**分组参数列表保住每个合法展开**；7 builtin 经 catalog 装载未被误判。

**夹具失真修正（非放宽）**：原默认 `registry_implementation` 指向不存在文件，闭合悬空检查后会使**全部用例多 1 错**，故改指真实存在的 `assembly/compress_mount.go`。

**未闭如实登记**：**跨过程洗白**（helper 返回字面量再转发）**不可判**，登记不假装闭合；`guard_checklib` 内元数据发射不在本门变异算子面内。**Go 消费者锚 `assembly/plugin_mount_test.go:71/:97` 仍未跑 ⇒ 01.R1 仍不勾**，归全树复跑批。

### R70 05.R2 锚重定核销：零生产改动，5 变异证有牙（2026-11-16）

**编排者亲验**：两新锚 `-count=10 -v` → **20 PASS**；三枚既有绿锚 `-count=1 -v` → **3 PASS**；勘误已落 `05/design.md §十七·4`（含变异矩阵）。

**旧判据没牙的机理查清（采纳）**：旧锚声称「清掉已结收据后不得重欠」，但其「清掉」写成 `Consumed = []StatedConsumption{}`——而 **`[]` 按 `checkpoint.go:75-86` 恰是权威空集**。故向一改夹具 `[]`→`nil` 即绿（生产零改动），向二让 `Seat` 忽略载荷则三枚绿锚同红。**判据互斥且可被夹具平凡满足。**

**新判据两枚（逐字落锚）**：
- 正 `TestTaskSliceReadsBackTheReceiptItDoesNotState`：生产者陈述收据、**载荷省略该族** ⇒ 座位不重欠已结通知，且**从该任务自己的链读回**该收据；负向＝同一载荷 over 无收据的链 ⇒ 重欠。
- 负 `TestTaskSliceTrustsAStatedEmptyReceiptSet`：账户**把该族陈述为空集**而链上持有收据 ⇒ 座位**零前缀读、零收据**，故被陈述否认的通知重新欠账。

**⚠️ 生产改动为零**：`Seat` 的 nil 分支（`durable.go:477-481`）与 `readTaskLocked` **本就具备该能力**，只改测试文件。**这印证 R68 裁定**：`nil≠empty` 区分在产品里已存在，**锚只是没在测它**——不是产品缺能力。

**5 个变异证有牙（隔离树，复原后 exit 0）**：M1 省略当陈述空→正锚红；M2 陈述空当省略（`len()==0`）→负锚红；**M3 旧 `[]`→`nil` 把戏→负锚红**；M4 `nil`→`[]`→正锚红（**任何单一夹具编辑只关半边**）；M5 `readTaskLocked` 丢收据→正锚红。

**05.R2 仍未勾（正确）**：该行冻结面含 `./assembly/` 正锚 `TestTaskAccountStatedSizeTracksLiveWork`，另 `TestSettledTaskStateStaysConsistentAcrossRestore`／`TestTaskAccountRefusesToClearAnOwedReceipt`／`TestSliceStatesUnstatedApartFromEmpty`／`TestRedeliveredNoticeAfterRestartSettlesOnce` **包内 grep 零命中＝未建**，且 assembly 本窗口禁跑。**未建未跑即不勾。**

**勘误纪律齐**：`design.md:495` 勘误含变异矩阵；`:121/:153/:221` 三处旧命令加退役指针；`:555` 旧阻塞标注已解；`tasks.md:26` **旧名替换为新正则**（防零匹配假绿）。

**待派归并**：05.R2 的 assembly 侧五枚锚 ＋ 07.R2 的两条 assembly/host 消费者锚 ＋ 01.R1 的 Go 消费者锚 ＋ 11.R2 欠的「重复不占槽」负锚 ＋ 12 域 S4 正锚 ⇒ **同一形状：assembly 面锚未建/未跑**。**待载体形态单完成、memory/ledger 面释放后合批一单**，届时多叶方可勾。

### R71 载体形态四案测量：三案皆不可落地，成本主项是 fsync 道数（2026-11-16）

**⚠️ 编排者第四案被实测判为半真（自纠）**：我推导「idx 与 order 是同一持有键集的两份表示，故 idx 可整体删」。**真的一半**：`Keys`/`Namespaces` 可由 order 派生且**不劣于**目录扫描（25.9 vs 28.7ms@32k 键）。**假的一半**：**`idx` 同时是点地址**——删后点读 **10.4ms@6400 且随历史线性**，absent 键同读数（控制），故 §十九·一 的**每提交判重点读退化为整份走查**，破 §十七·二「点读与深度无关」。**我只看到它作为持有集的一面，漏了它作为地址的一面。**

**四案对比（同隔离树中位数；绝对 ms 受并发负载影响，增长轴与对照才是判据）**：
| 案 | 提交5键 | 点读 newest/mid/oldest@6400 | absent 控制 | Keys@32k |
|---|---|---|---|---|
| 今日无索引 | **4.144ms** | — | — | — |
| S1 一键一文件 | 25.102（6.1×） | 0.92/0.96/**0.65** | 0.003 | 28.7 |
| 选项2 keys.log | 13.439（3.2×） | 0.02/6.36/**11.51** | 11.80 | 36.8 |
| 选项3 惰性物化 | 9.056（2.2×） | 0.01/6.02/**10.80** | 10.35 | 首枚举 **15,634** |
| 选项4 只 order | 9.051（2.2×） | 0.02/6.12/**10.43** | 10.53 | 25.9 |
开档控制（删 root 重取）字节 260,000→1,040,000→4,160,000（16× 深度＝16×，**界可失败**）；S1 控制墙钟 1.7s→6.1s→**23.8s**。

**⚠️ 关键分解（原语实测，改变问题定性）**：**一次 fsync≈5.0ms** ⇒ 今日＝**一道屏障**；**任何自带耐久点的派生载体＝两道＝2.2×**；S1 额外 15–21ms/5键＝**每键文件创建**；就地 40B 写 5 键仅 **0.007ms**。**选项4 只关 order 屏障：9.08→4.87ms（1.15×）**。⇒ **成本主项是屏障道数，不是数据结构形状。**

**裁定：三案皆不落地，主树保持 S1 原状（绿、未接线）——代理处置正确**。选项2/3 是同一持有集的**第二份表示**（纯度不过）；选项4 破点读深度无关；S1 破成本。
**建议形状（须另立设计门，推算提交 ≈1.0–1.2×）**：order 陈述水位/提交序/枚举 ＋ **容量有界槽表**做点地址（**无每键文件**）＋ **按载体字节预算屏障**（界＝一个 chunk）。接线点 `atomic_store.go`/`segment_store.go` 不在其白名单故未接线。

**崩溃一致性**：四形状各跑同套件 **73 顶层+37 子 0 FAIL**、`-race -count=10` **100 PASS／0 竞态**；撕裂＋孤儿语句注入四形状皆绿——**但 keys.log 首轮红**（`keys.log states no key line: unexpected end of JSON input`），需自加「完整行」规则；**order 由 root 水位截断白得该规则**（又一处 order 更纯的证据）。另 S1 崩溃锚把「键条目已落」写死为 `idx/` 新增文件，文件型载体需放宽夹具（断言未改）。

**`TestNativeConversationRoundTrip` 归属负载抖动（编排者复核）**：该单报红且单跑亦红 10.4s；**我在并发减少后复跑 exit 0、`-count=2` ok 0.391s** ⇒ 与 R58 归因一致，**非真回归**。其 .go 面 sha256 与会话开始逐字节同。

**未证**：2.2× 是否令四枚装配锚转绿（未接线未跑 assembly）；掉电；**槽表零原型**；8 个具名变异未按形状重跑。

### R72 新发现 N24：点查询被实现成全量枚举（2026-11-16，编排者追路径所得）

**用户问「全量枚举何时发生」，追 10 处生产调用点后得四类时机**：①**每次落检查点**（`eachFactPastCut` checkpoint.go:1126——**只想要尾却走查全史再过滤**；`newestSessionCheckpointRef`:970；`restoreSessionCheckpoints`:1006；`keptHotKeys`:438 **随退位删除而消失**，且是唯一用哨兵早停者）；②**每次 boot**（`seedAnchoredReplay` run.go:2617、`eachCommittedEvent` run.go:2356 全量重放回退）；③**每次事件查询**（`QueryEvents` read.go:114，全走查＋逐匹配键读正文）；④**每次 trace 工具解析父键**（`GetParent` read.go:280 ← tools.go:962，模型可触发、按次付费）。

**N24 缺陷本体**：`EachIndexNewestFirst` 的**公开回调只返回 `error`**（segment_store.go:1527-1534），包装层恒返回「继续」⇒ **唯一早停途径是返回哨兵错误 `errStopIndexWalk`，而只有 `keptHotKeys` 用了它**。于是：
- **`GetParent` 是点查询**（找一个键的父键），回调所有分支 `return nil` ⇒ **每次调用走查全史**。
- 其回退 `parentFromBodies`（read.go:317）更重：全走查收集该分区**全部** refs，再**逐条读正文** ⇒ O(历史) 走查 ＋ O(历史) 正文读。
- **与 idx 点地址问题同类**（点地址被实现成全量扫），但 idx 那个已测量并在修，**本条此前未登记**。

**「全量枚举仍 O(历史)」须拆两句（修正 R71 的笼统声明）**：**真需全量枚举者**（按序导出全链）确实 O(历史)，属本征；**但 `GetParent`／`eachFactPastCut` 这类点查询与尾查询不该在其中，它们目前是，属可修缺陷。**

**处置**：归反转后引擎单（S1c/S2 面）。可寻址 order 使 `GetParent` 可从该键位置**反向走到第一条同链条目即停**（成本由该链局部密度定，非全史）、`eachFactPastCut` 从水位起走到尾即停。**但必须同时给走查 API 一个真正的早停出口**（回调返回 bool 或等价），否则消费者仍会写成全走查——**只改引擎不改 API 出口解不干净**。须配负锚：点查询成本随历史增长即红。

### R73 阶段5自审：八项 A 类全部自裁并落地，叶数 34→42（2026-11-16）

**用户纠正**：「在我裁之前你需要先依据项目和变更原则自行判断一遍，再为我澄清并给出你的建议」＋「看起来你已经裁了，那很好，但是不应该卡着不动」。**编排者两处毛病**：①把「呈发现」当「请裁」；②裁完不执行。**八项原则均可裁，无一需用户裁**（真正需用户的只有 D6.13 反转本身，已给）。

| # | 缺陷 | 自裁依据 | 落地 |
|---|---|---|---|
| **A1** | **S2 症状锚在反转后永久红**——非空转守卫 `coldReadWallClock <= 0 → Fatalf` 要求读冷层，删退位后恒 0 | **5.11 症状锚不可替换**⇒重定是反转**前置条件**；且当前退位趟线性增长故新锚**今天即红**，满足「第一天红着」 | **09.R4** 立，**已派**（必须先于 S2 取红证据） |
| A2 | 08.R1/R2 仍是「建退位」 | 裁定已反转，叶须同向；改名断引用锚故**保留目录名＋标勘误** | 08 头部反转声明＋R1/R2 标作废＋**08.R3(S3)/08.R4(S4)** 立 |
| A3 | S0–S7 只活在 design/台账，无叶承载 | R10 目标必有承载、R27 载体是账目 | S2→**06.R4**；S3/S4→08.R3/R4；S5→各域锚叶；S6→10.R5/R6/R7；S7→09.R3/F2 |
| A4 | 06.R2 已勾但交付被取代 | **5.8 历史不可变、推翻记在后段**⇒**标勘误不取消勾选**（先前「重开或标勘误」是伪二选一） | 06 域末勘误指针，新交付归 06.R4/06.R3 |
| A5 | 05 的 Seat 尾谓词无叶 | R10；S0 已实测谓词不需重设计 | **05.R3** 立（含拼写停法丢 taskA/taskB 负锚） |
| A6 | N6 所需产品日志行无叶 | R10；消费者是 CI attestation 独立存在故**不违 N2** | 并入 **06.R4** 交付③ |
| A7 | 13 两个新假绿形态无叶 | R64 教训：只活战报＝残留 | 并入 **13.R2 续单**，明令先落 design |
| A8 | 10.R4 范围远小于实际移除量（~60 符号/8 spec/28 文档/1 能力 7 Req 31 Scenario） | **R4 偏大孙任务须拆**；本轮已撞上限两次 | 拆 **10.R5**(spec)/**10.R6**(文档词汇)/**10.R7**(门 golden＋N14/N15/N10 小残留)；符号删除归 08 |

**B 类**：09.R2 七段含冷读/冷写计量、11 冷重复分支——**冻结锚引用将消失之物**，随反转重定，不得留。

**关键次序（自审推导出的硬约束）**：**09.R4（红证据）→ 06.R4（S2＋接线＋日志行）→ 05.R3（Seat 尾谓词）→ 08.R3（S3 删退位，原子）→ 08.R4（S4 删切面＋外部脚本）→ 10.R5/R6/R7（S6）→ 09.R3/F2（S7）**。其中两条不可颠倒：①09.R4 必须先于 S2，否则取不到「反转前」的红证据；②**08.R4 必须在 08.R3 后**，因 `placeIndexCut` 最后调用者是 `relocate`。**05.R3 须在 08.R3 之前或同批**，否则删退位即触发静默漏座。

**S1c 已成功（另记）**：提交成本 **0.98×/1.09×/1.02×**（400/1600/6400），一道载体屏障覆盖 65 次五键提交；点读 absent 与 present 同阶（0.006 vs 0.007ms），控制案 absent 随深度 18× 增长；开档地址层 **64 B 恒定**；8 变异复跑 8/8 红；**接线后四枚装配锚隔离树全 PASS**（1.61/13.47/14.08/14.27s）。主树 ledger 0（76 顶层＋53 子）、`-race×10` 0 竞态、两门 0。**未接线**（共享面决定，只报不落）。两处自裁批准：`root.json` 并入槽表头（三载体降两件，同三个水位数不写两遍）；收回 `config.go` 的 `idx/` 例外（零残留）。**`namespaces()` 语义裁定：按持有键**（「曾写过」是 tombstone 空命名空间＝历史残留），**但须先枚举消费者证明无人依赖旧语义**，归 06.R4。**接线已授权**，须主树复跑全树确认。

**当前并发**：09.R4（assembly 症状锚）∥ 13.R2 续单（scripts/gates＋codetools）。触碰集不相交。

### R74 13.R2 续单核销：三闭一阻＋两处归因撤销（2026-11-16）

**编排者亲验**：`test_check_runtime_evidence` **exit 0／Ran 108**（97→108，未缩）；`go test ./scripts/codetools/` **ok 3.844s**；`knowledge_base/.git` 存在且其下 `*.go` **0 个**（我复跑 find 确认）；spec 中 `green_race_waived` 命中 2 处。另四套自测 47/14/32 全 0、`check_dod_coverage.py` exit 1／20 gap（与 R56 同数）、`openspec validate --strict` valid。

**① 绑定面：四组已等价覆盖，真缺口是命令面与量面完全未读**。既有 97 例已覆盖源树指纹、期望全测试集合（bundle equal-set 门）、实际执行（run+pass、无 fail/skip、包级终局）、有效性（控制树/合同摘要/同源/run_id 与 log 不得复用）。**但生产者早已写 `run_config.selectors`/`invocations`（argv、dir、packages、tags、race、exit_code）与 `liveness`，检查器一个字段都不读**。新增四检查，**8 条前接受→后拒绝**对（含 legal_baseline 前后皆 0）。
- **未闭＝环境记录面**：生产者 `subprocess.run` 继承环境且不记录。**编排者裁定：这是白名单缺口不是产品缺口**——`run_runtime_scenarios.py` 是**门脚本**，归 13 域，已补进 13.R2 写集合。**与 N6 同形态但不同归属**：N6 的日志行属产品 Go 面归 06.R4，本项属门脚本面归 13.R2，**不可混为一谈**。

**② 竞态豁免有界例外已写入 spec，门未动（符合 R25 裁定）**。条款三条件同时成立才记 `green_race_waived`：①该次运行确实失败；②每处 `WARNING: DATA RACE` 顶帧都落在已登记上游模块并命中 `RACE_WAIVER_SIGNATURES`；③无任何普通失败。**且明写「SHALL 引用与竞态门同一张签名表，MUST NOT 另立第二份裁量清单」＋「升级越过上游修复后 MUST 删签名并同时删本例外」**——防了第二真相与过期豁免两个坑。

**③ N7 已闭，机理查清**：`checkTrackedHygiene` 的 `files` 实为 **`git ls-files -z`（索引）**，第二条列表 `git ls-files -ci --exclude-standard`；**两条都看不见「未跟踪且被忽略」**。新增 `checkWhitelistUnnamedSource`，输入 `git ls-files -o -i --exclude-standard -z`，只判 `*.go`，接线在既有 `lint.sh:254`。CLI 红绿对：探针仓 exit 1 具名 `whitelist-unnamed-source` → 加白名单行后 exit 0；真树 exit 0、**零误伤**（全仓未跟踪且被忽略的 `.go` = 0）。

**⚠️ 归因撤销一（R24 N7）**：我曾记「`examples/wechat-bot/knowledge_base` 是 N7 白名单缺口的真实实例」。**撤销**——它是**自带 `.git` 的独立仓、内含 0 个 `.go`**，README:127 明写运行时不入库。那条警告是 **`comment_policy/main.go:171` 把目录条目称作「Go file」的措辞缺陷**，归 10.R7 顺带修。**N7 缺口本身仍真且已闭**，只是我举的实例错了。

**④ 两个假绿形态：先落 design §八 再闭（符合 R64 教训）**。FG-1 区间包含（内层界限越出外层→`nested interval`）、FG-2 自指长度（分母来自合同与 test-log 而非证据自报）。**变异比例的自指分支如实登记未闭**，不假装闭合。

**13.R2 仍未勾**：环境记录面（现已授权）＋ N6（待 06.R4）。**G4 未全绿**（G4-5/6/7 属 13.R3 需外部授权）。

### R75 13.R2 环境面闭＋判据方向纠反＋外来漂移归因（2026-11-16）

**编排者亲验**：`test_run_runtime_scenarios` **0／36**（32→36）；`test_check_runtime_evidence` **0／112**（108→112）；`test_check_dod_coverage` 0／14；`check_dod_coverage.py` exit 1／20 gap（同 R56）。

**环境变量集合 14 项，逐条按消费方取证（采纳，非凭空列）**：`GOFLAGS`/`GOWORK`（`go help environment`：命令行旗标在其后应用故覆盖它）、`GOMAXPROCS`（`runtime/proc.go:935` 只认正整数；`smoke_harness_test.go:1044` 当测量字段；`ci.yml:101` 钉 1）、`GODEBUG`（`assembly/host.go:425`）、`TAGENT_GCTRACE`/`BOOT_TRACE`/`HTTP_PORT`/`SESSION_ID`、`SMOKE_ROUNDS`、5 个 `TAGENT_CALIBRATION_*`。**未选 locale/时区——范围内用例无此依赖，不凭空列**。spawn 前捕获 `os.environ`，未设置不写、值不重写。

**凭证排除有正反证**：声明名含 `AUTH/CREDENTIAL/KEY/PASSWORD/SECRET/TOKEN` 即抛 `RunnerError`；正测放 4 个凭证变量→记录为空且 `evidence.json` **全文无其值**；反测塞 `TAGENT_REAL_JUDGE_API_KEY` 进声明表→拒。检查器判词只点变量名，`value-echoed=False`。

**⚠️ 判据方向纠反（重要，非放宽）**：旧判据把 `GOFLAGS` 与 argv 的优先关系**判反**——命令行覆盖 GOFLAGS，故旧判据**误拒真话**（`GOFLAGS=-count=1` 并列 argv）、**误放 `-short`**。新方向：`EVENT_RUN_SHAPING_FLAGS` 中任一旗标未被任一记录 argv 决定即拒。**是新增拒绝面，不是放宽**。三方对照（HEAD／R74／本树）实证：`GOFLAGS=-short` 而 argv 无 → 0/**0 误放**/1；正例完整一致 → 0/**1 误拒**/0。

**外来漂移两处，归因经我复核成立**：
- `test_check_event_integrity_mutations` **exit 1／ran 43（收 47）**：4 红全指向 `MUT-DOD-P4` 的锚 `assembly/symptom_anchor_recovery_test.go:518`，而**并发单 09.R4 正在重写该文件**（我复核 `git status` 确为 `M`；现字面量在 522、518 是 `const` 行、HEAD 在 390）。该单零 `.go` 改动 ⇒ **R21 闭包污染，非其缺陷**。
- `test_check_runtime_plan` 1 红＝外来 `lint.sh:144` 新增 `test_check_dod_coverage.py` 而沙箱拷贝表未跟上（二者均非其写集）。

**⚠️ 由此暴露一个结构脆弱（登记待办）**：**变异清单的锚钉在他叶会重写的测试文件行号上**，故任一叶重写该文件即令变异陈旧。本轮已两次撞此形态（R56 的 `MUT-CAND-01.8` 因 `CommitFact` 重构失效、本次 `MUT-DOD-P4` 因症状锚重定失效）。**处置**：①09.R4 落地后**必须复跑变异门并更新 `MUT-DOD-P4` 锚**，归 09.R4 收口条件；②**变异锚应改形状判据而非行号**（与 `check_plugin_mounting` 的 R5a 形状判同理），归 13.R2 或 10.R7 评估——**不得靠每次人工补锚维持**。

**13.R2 仍未勾**：环境面已闭，**只剩 N6**（产品日志行归 06.R4）。未造代理、未动 `.go`。

### R76 09.R4 核销：症状锚红成立，反转前置解锁＋一处归因撤销（2026-11-16）

**编排者亲验**：新锚 `-count=1` → **exit 1**，原文 `generation 247 beside generations 1-5: the middle generation of the last 5 costs 242.947334ms beside the 131.75…`。**红证据成立 ⇒ 反转前置解锁**。`-count=3` 3/3 同因（比值 1.655/1.694/1.954）。零冷计量引用（grep 0）。旧 S2 四量判据原样保留、冷读守卫已标依赖退位。assembly 全量 280.6s／240 测**仅 2 红**（本锚＋governance，见下）。两注释门 0／0。**09.R4 已勾**（绿半留 F3）。

**⚠️ 它自己抓到首跑假绿（正面榜样）**：depth 84／turns=2 时比值 1.26 < 界 1.3 → **EXIT=0**。**不是产品平坦，是深度不足＋判据被 ~75ms/代的固定提交成本稀释**。处置：实测重选形状与深度。
- **形状**：`turns=1, perGeneration=1`（**同工同酬**，比值与工作量无关）；参照窗跳 0 号（开空根）。
- **深度推导**：比值∝代序，而∝每代工作量之比**恒等**（增长项与固定项同比缩放）⇒ **只能靠代序**。实测 84 代 1.26/1.29/1.63（**与噪声同量级，不可判**）、168 代 1.43/1.72/1.99、252 代 1.61/2.12/2.30 ⇒ 取 **252＝CI-3 起点 84 的三倍**，单次 48s。**叶面原写「最小深度」，实测最小可判定 ≥168 而非 84，已具名上报 design §十二**——这是按读数偏离而非缩水。
- **界 1.3＝候选非冻结**（不沿用 2×）：噪声侧实测 1.025–1.130、增长侧 1.655–1.954，两侧余量 15%/27%。**校准含深度再推导归 09.R2**。
- **控制案三者在被判断言之前执行通过**：平坦序列接受、按 `restored` 缩放的线性序列具名拒（`grows with the chain`）、两独立根同条件离散 1.025/1.068/1.130 在界内。

**⚠️ 关键旁证（不进锚文件，但决定反转范围）**：`WithRetirement(false)` 下 boot **9.7ms→73.2ms**、每代总成本 **1.75×** ⇒ **仅删退位不足以转绿，绿的条件是可寻址索引**。**这确证反转必须含索引，不能只删退位**——与 R73 硬序（06.R4 先于 08.R3）一致，且给了它实测依据。

**⚠️ 归因撤销（我与其他单多次误判，须纠）**：`TestGovernanceSliceWiredInProduction` 红**不是「外来漂移／另域并发叶」**。我读原文确认：`assembly/governance_checkpoint_test.go:146` 断言 `len(state.Records) != 2` 即红，实得 `version 1 tail 2 over **0 records**`——**正是 07.R2 载荷裁减（切面只载尾、历史从流派生）的已知后果**，与 07.R2 已报并经我批准的 `TestGovernanceSliceSnapshot` 重述**完全同形**；只因该文件在 `assembly/` 超出其白名单故未改。**:151 的 `state.Records[0].ToolName` 亦同须重述。**
- **处置**：归 **assembly 锚合批单**，按同一「**重述非缩水**」口径改——「切面不载历史、绑流后同两条裁决同序可查」。**不得改回载历史以迁就旧断言**。
- **教训**：多个单把同一枚红反复归为「并发漂移」而无人追到原文。**红项归属须读失败原文，不得沿用上一单的归因**（与 R56 的「成因读原文非测试名」同律）。

**下一步（硬序）**：**06.R4（S2＋接线＋N6 日志行）现可派**——09.R4 红证据已取得、S1c 已落地、assembly 面已释放。

### R77 06.R4 撞上限：三项已落、一项未做、一处成本上升待解释（2026-11-16）

**06.R4 撞 150-turn（第三次）**，末句停在「写三枚新锚」。编排者诊断现场，**实际进度远超末句**：

**已落且我亲验**：
- **座位已实施**（`store:seat` 在 `segment_store.go` 5 处命中）；**接线已启用**（`config.go:249` `store.statesAddressMaintenance(cfg.WriteRights)`，**按写权门控**——只有写者维护地址，正确）。
- **N6 日志行已落**：`assembly/checkpoint.go:954` `assemblyInfo("tagent: boot seated the task account tasks=%d tail=%s reads=%d keys=%d", …)`——**报的是托管了多少任务、尾、读了多少键，不是库存计数**，符合裁定。
- **两枚新锚存在且 `-count=10` ok**（40.348s）。
- `go build` 0；`./memory/ledger/` **0（30.865s，0 FAIL）**；**`./assembly/` 仅 2 红且都是已知预期红**——`TestGovernanceSliceWiredInProduction`（R76 已归因为载荷裁减后果，待 assembly 锚合批单重述）＋ `TestSymptomAnchorPerGenerationCostFlatOverALongRun`（故意红至反转完成）。**四枚 S1c 装配锚已不再红**⇒接线在主树成立。

**未做／未证（须收尾）**：
1. **交付④ `namespaces()` 语义未改**——仍用 `namespaceSeen`（「曾写过」集），裁定要求改「只答持有键」且**先枚举消费者**。
2. **提交成本对基线的比值未测未报**（派发令硬要求；S1c 曾达 0.98×/1.09×/1.02×）。
3. **新锚的增长控制案未证**（派发令要求「须配 materializingLoad 增长控制」）。
4. **⚠️ 症状锚绝对成本上升约 57% 待解释**：09.R4 时 `242.9ms vs 131.75ms`（比值~1.84），06.R4 后 `359.9ms vs 206.7ms`（比值~1.74）。**比值略降但两侧绝对值同升 ~57%**，且该锚单次耗时 48.44s→75.15s。**两种可能须判别**：①并发负载（本单跑时另有测试在跑）；②座位＋接线确实加了每提交成本。**不得以「比值降了」当作改善**——须空闲复跑取读数，并给提交成本比值。若是②，则 S1c 的 0.98–1.09× 结论在叠加座位后不成立，须重估。

**处置**：派窄单收尾上述四项，**一单一单元原则下这四项同属「06.R4 的验证与第④交付」故合为一单**，但明令先做①②（快）再做③④（需跑）。**06.R4 暂不勾**。

**流程教训（第三次撞上限）**：06.R4 装了「实施座位＋启用接线＋加日志行＋改语义＋三枚新锚＋全树验证」六件事。**即便同属一叶，实施与全树验证也应分单**——全树验证本身耗 ~300s×多套，占满预算。此后**实施单与验证单分派**，验证单只跑不改。

### R78 06.R4 收口：四项全完，但查出一处真回归＝反转目标的阻塞项（2026-11-16）

**编排者亲验**：`namespaceSeen`/`noteNamespace` **零残留**；四锚 `-count=3` **ok**；`./memory/ledger/` **0（37.9s）**。代理另报 build/vet 0、assembly 仅两具名预期红（336.2s／240 测）、`tests/...` 0（13 包）、座位锚 `-race -count=10` 70 PASS 0 竞态、两注释门 0／0。**06.R4 已勾。**

**① `namespaces()` 语义已改（消费者枚举成立）**：全仓消费者为 `Load`(:365)、`coldEventLocked`(:1033)、`coldIndexLocked`(:1427)、G3 探针、`migrate_test:151`／装饰器夹具——**全为分区发现或迁移副本比对**；`memKV`/`rustVikingKV` 本就答持有键，`recover-kv` 不读。**无人依赖「曾写过」**。旧语义残留点确证为 **`rewrite` 从不重置 `namespaceSeen`**。**三结构（`namespaceSeen`/`namespaceOrder`/`noteNamespace`）整体删除**，符合零残留。锚 `TestJournalNamespacesAnswerOnlyTheHeldKeys` 两向 ×10 绿，变异去 `value.deleted` → 红。

**② 提交成本零回归、零新增耐久点**：三形状 200 次/深度同机，C／A＝**0.99／1.02／1.05×**（对 4.144ms 基线为 1.17/1.01/1.03×）。`Sync()` A=200、C=207（**1.035/提交**），`append()` 三形状皆 200（**1.00/提交**）⇒ **座位与事件族同帧，未加屏障**。

**③ 增长控制案齐**：删座位重推开档 **291.8µs→823µs→2.827ms**（16× 深度＝9.7×）；坏座位锚答案恒等链而**读条目 50→200→800**。变异具名转红（读座位两次→`read the seat 2 time(s)`；坏座位当零信任→`answered 0 event(s), want the 4`）。`commitSeq<count` 另有专锚。

**⚠️④ 真回归——同负载三方对照，负载不能解释（这是反转目标的阻塞项）**：
| 形状 | 首窗/末窗 | 锚耗时 |
|---|---|---|
| 本树（接线＋座位） | **223.93／379.96ms** | 77.75s |
| 改动前形状（同负载窗口） | **139.59／264.16ms** | 53.24s |
| 接线**无**座位 | **212.98／360.01ms** | 78.08s |
⇒ **座位≈0 成本，成本在接线**；落在 **`frame`×2.00 与 `closing`×2.19**，而 **`commits` 无回归**。
- **结论**：**S1c 的 0.98–1.09× 只在「一次提交」成立，不延伸到「一代」。一代绝对成本 ~1.53–1.60×。比值 1.89→1.70 不算改善**（代理明确拒绝把它当改善，正确）。
- **归因是推断**（按 fsync≈5ms 折算 frame/closing），**须直接量确证**。
- **机理假设（待验）**：S1c 的「按载体字节预算屏障」只覆盖了**提交路径**，未覆盖**检查点落盘（frame）与关闭（closing）路径**，故那两处仍每载体一道屏障 ⇒ 道数翻倍。与 R71「成本主项是屏障道数」一致。
- **处置**：**这是症状锚转绿的必要条件**，派专注单直接量 frame/closing 的屏障道数与耗时，把字节预算屏障规则**扩展到检查点与关闭路径**。**不得以「提交没回归」宣称整体没回归**——这正是「修一层≠复合有界」的又一实例。

**下一步序调整**：原硬序 06.R4→05.R3→08.R3。**现插入 06.R5（屏障预算覆盖 frame/closing）于 06.R4 之后、08.R3 之前**——因为删退位后 frame/closing 仍在，回归不会随删除消失。

### R79 词汇门红：已归档退役词被本轮写回，根因是派发令的门禁面是手挑子集（2026-11-16）

**用户一问「似乎有违规名词？」查出真缺陷，且是系统性的。**

**实测**：`python3 scripts/gates/check_vocabulary_alignment.py` **exit 1**，`violations=72 carrier=72`，其余三规则（compression-generation／plugin-product／retired-word）皆 0。该词由**已归档**变更 `code-vocabulary-alignment`（PR #38，commit `90fecc56`，2026-11-12）**全退役**，门是**纯短语禁：无基线、无豁免、无棘轮**，大小写不敏感子串命中即红。命中全部是本轮 S1/S1c/06.R4/07.R2 新写。

**根因不是词，是门的清单**：`scripts/lint.sh`（转发 `scripts/gates/lint.sh`）是 CI 唯一入口，串 40+ 道门；而此前每张派发令的验证面只点 `comment_policy` 两 scope 加几道手挑的门，**词汇门从未出现在任何一张令里**。故每单都「过了自己的门」而这道一直红。这是 R27 的反面——纪律没落在编排文档上。**已改 §② 凭据节：门禁面＝全门。**

**替换实名不自裁**：`docs/GLOSSARY.md:3` 原文已给权威映射——「多义旧词 `carrier` 亦全退役，按义改既有实名（`commit`／`DurableCommit`／`credential`／`URL`／`projection`／`envelope`／`fact`／`record`），不另造新词；描述机制一律用代码实名」。故 journal 面用 `order file`／`slot table`／`header`／`the index`（类型本名 `journalAddress`），标识符 `journalCarrierSyncBytes`→`journalIndexSyncBytes`、`closeCarriers`→`closeIndex`；审批面用 `record`／`the chain`。**不注册该词、不加豁免、不改门**——门是对的，词是错的。

**按触碰集拆两单**（journal.go 正被 06.R5 精密计时，并发编译会污染读数，即 R78④ 歧义之源）：
- **V1 审批面已完并亲验**：`runtime/task/approval/attention_log.go`(:5)、`attention_log_test.go`(:60/:177)、`features/evolution/governance/approval_channel_anchor_test.go`(:32/:229) 共 5 处，**全是注释散文或 `t.Fatalf` 消息串、零标识符**，断言条件与测试名一字未动。编排者复跑门：**三文件命中 0**。产品包零容忍面 `comment_policy -no-baseline runtime features` **exit 0／0 findings**；外层棘轮面改动前后皆 **0 findings**，未抬基线。三文件在 `git status` 是未跟踪新文件（07.R2 产物未入库），故取证靠 grep 与门读数而非 diff——代理如实声明了这一点。
- **V2 journal 面待 06.R5 释放**：**它是移动靶**——编排者首测 72（journal.go 51／journal_test.go 6），V1 期间复测 **96**（61／30），V1 后 **91**（61／30）。即 **06.R5 正在边写边加违规词**，确证其令亦无词汇门。V2 须在 06.R5 收口后取当时实际命中数，不得沿用 91。

**教训**：①门的清单必须取自仓库唯一入口，不得由编排者凭记忆挑子集；②计时单运行期间禁止其他单跑编译面；③已归档变更的退役词表是冻结裁决，新写代码前须先查门而非事后补。

### R80 05.R3 受阻上报成立；甲/乙不是真分叉，编排者独立裁甲并收紧三处（2026-11-16）

**上报质量高，取证可核**：探针经 `go test -overlay` 注入（仓内零落盘）实测 `durable_keys=2 index_walk_entries=0 durable_keys_seen_by_the_walk=0`，**exit 0**；机理读码实证而非推断——`TierFacts` 走 `tier.Segments.kv.Commit`（tier_facts.go:47）raw 提交，落引擎 order 与 value 区，**不进 store 事件索引**（`s.order` 只在 `publishEventLocked` segment_store.go:709 追加，`commitOrderOf` 经 `segmentIndexEntry` :565-577 过滤成事件族）。三条绕开逐条排除成立：`Keys`+`Read` 按键序（journal.go:2515 `sort.Strings`）无提交序；空尾改 `nil` 反 red S2 有界锚且违 R68；assembly 侧声明接口而真实引擎不满足＝R14 mock 冒充路径。**主树零 `.go` 写入，05.R3 不勾，正确。**

**乙案排除，两条独立冻结理由各自充分**（故不上呈用户，按「既有原则能裁的独立裁」）：
1. 06/design.md §十九·一 明写「**引擎不得解释 idx 语义**」，乙令引擎解释 `type==TypeCheckpoint ∧ session==""`，直接违之。
2. `journalKV.TailKeys`（journal_kv.go:123）是**退位切面的尾**，08.R3 整体删除退位——把新语义钉在待删符号上是建在沙上，且令 4 处 ledger 锚（journal_index_cut_test.go:184、retirement_snapshot_test.go:262、segment_store_test.go:713/:776）转红须同批重述，纯属自找。

**甲案采纳，三处收紧（冻结，实施单不得再议）**：

**① 接缝落 `AtomicStore` 接口，不是只落 `journalKV`。** 代理原提案只在 journal_kv.go 加 ~12 行导出方法——那会迫使 assembly 做引擎特定类型断言，直接违 `AtomicStore` docstring 的明承诺「switching engines needs no adaptation」与「可换引擎」纯度判据。**实现方实测 9 个**：生产 3（`memKV` config.go:429、`rustvikingKV` rustviking_kv.go:150、`journalKV` journal_kv.go:180）＋测试替身 6（`seatReads` segment_store_test.go:1017、`faultStore` atomic_store_test.go:90、`countingKV` kv_original_store_test.go:61、`filteredKV` :152、`orderedStore` :222、`factProbeEngine` fact_commit_batch_test.go:31）。无提交序的引擎按**既有先例** `StoreCapabilities.AtomicBatch`／`ErrBatchNotAtomic` 办理：**具名拒绝＋能力声明**，不得假实现（R14）。此非「为单使用方造抽象」——导出是既有统一契约的兑现，替代形态（类型断言）违冻结承诺。

**② visit 必带 value，签名冻结为 `EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error`。** 识别靠 typed 值；**键前缀判据被无前缀门与冻结裁决禁**（决定读 typed 状态不用字符串前缀）。故「只给键、让 assembly 按需 Read」不可行——它要么读全部要么解析键。成本＝走查到最新框架检查点的距离（节奏），非历史，合 S2 有界；§十九·五 已声明该成本。

**③ nil/empty 三态显式冻结，纠 §十八·4 末句歧义。** 代理写「永不返回空集」，与其 §十八·3 第 2 条（检查点即最新提交时尾**真**为空）自相矛盾。按 R68 裁定如下：
- 走查未找到框架检查点 ⇒ **nil**（未陈述 → 整份读兜底）
- 引擎不声明序能力 ⇒ **nil** ＋**具名退化**（不得静默；按 06.R2 上报条款写入 spec 具名操作表）
- 找到框架检查点且无更新任务事实 ⇒ **empty 非 nil**（陈述为无 → 权威，零年轻任务入座，读有界）

把第二态与第三态混为一谈，即同时破 S2 有界锚与漏座负锚。

**S0 内部矛盾裁定**：§十九·一 governs。§十九·五 的「引擎侧逐条目读携带值」是**原型**形态，生产把识别移 `assembly.tailFactKeys`，引擎只答「序＋原文」。06/design.md §十九·五 须附勘误——**待 06.R5 释放该文件后由编排者写**（避免与在跑单互踩）。

**解锁与次序**：05.R3-甲 写面含 journal_kv.go／atomic_store.go／config.go／rustviking_kv.go＋6 替身文件，与 V2（journal.go／journal_test.go）**同包**，构建闭包相交 ⇒ **不得并发**。硬序：**06.R5 → V2（纯文本，快，先让门转绿）→ 05.R3-甲（接缝＋谓词＋三锚）→ 独立验证单（全门）**。按第三次撞上限的教训，实施与全树验证分单。

### R81 全门盘点：lint 入口内另五道红，归属以真 HEAD worktree 对照判定（2026-11-16）

R79 的缺口不止词汇一道。编排者把 `scripts/gates/lint.sh` 内纯 Python 门全跑（零编译负载，不污染计时），并在**真 git worktree**（`git worktree add --detach /tmp/tagent-head-git HEAD`，用后已 remove）取基线。

**取证方法勘误**：先用 `git archive HEAD | tar -x`，但 `check_doc_budget` 需 `git ls-files`、`check_concept_budget` 需读冻结基线 commit，二者在无 `.git` 的归档树上 **fail-closed 报错**——**那不是「既有红」证据，是不可用读数**。改真 worktree 后才可判。凡基线对照须先确认门在该树上**有判定对象**。

| 门（在 lint.sh 内） | HEAD `9547faca` | 工作树 | 归属 |
|---|---|---|---|
| vocabulary | 4 | 91 | 4 出自本变更早先提交 `ec038779`（`git log -S` 实证），87 本轮引入；**门自该提交起一直红** |
| concept_budget | **exit 0** | 7 errors | **全部本轮引入** |
| durable_truth_registry | **exit 0** | 3 errors | **全部本轮引入** |
| product_surface | 0 err／1 warn／38 hooks | 1 err／1 warn／39 hooks | error 本轮引入；warn 既有 |
| doc_budget | 22274 > 22153（**+121**） | 22151 < 22153（**−2**） | 本轮已压 123 行，反向超 2 ⇒ **锁步记账项** |
| dod_coverage | **门脚本不存在** | 红（P0–P8） | 本变更新增门，F3 债 |

**范围外**：`check_no_prefix_criterion` 54 errors **不在 lint.sh**，属另一未落地变更的 W0 门（`registry_present=9 allowed=9`），不得混入本变更门禁账。

**逐项定性（编排者自裁）**：
1. **`StorageConfig` 7 字段超基线 6**——`WriteRights` 是 flock 单加的合法部署可变字段，`RetireFacts` 是退位开关、**08.R3 删除**后即 6 自解。**不单独修、不抬基线**，08.R3 收口须复跑确证。
2. **`FactCommitOutcome`（11.R2）／`AttentionFactLog`（07.R2）无 concept binding**——`AttentionFactLog` 是**有意结构端口**（attention_log.go:30-34 原文：签名只用内置类型，使 memory tier 的 fact log 结构满足，两向无需包边），内化会破坏该 avoidance ⇒ **绑定为 port，不内化**。落点 `golden/runtime/architecture.json` 的 `concept_bindings.bindings`。
3. **S1 三方法 > 2、S4 缺样例** `examples/contract/runtime_task_approval/AttentionFactLog_example.go`——随绑定补。
4. **ceiling 两项**：`domain_surface.new=56>54`、`exported_interfaces=85>84`。**勘误（编排者自查后纠）**：本条原写「前者由绑定解」——**错**。读码实证 `check_concept_budget.py:1047` `"domain_surface.new": len(new_names)`，计的是**全部新增导出名**，绑定与否不减计数。且 `concept_ratchet.note` 原文定协议：「两个数都是本波实测值，非收敛目标：后续波次每新增一个导出名，棘轮按名判红而不是抬升到新常态」，并记录本变更 W3 已**按名抬升** `new_max 47→54`、`exported_interfaces_max 83→84` 且附逐名清单与理由。故协议是**逐名裁定→按名抬升→理由记台账**，非一律禁抬（用户禁的是 comment_policy `--update-baseline` 那类掩盖 finding 的抬基线）。裁定见 R83。
5. **durable truth 3 errors**：`config.go` 写面（flock `.write-lock`）、`attentionFactPrefix`、`approvalRequestPrefix` 未登记 ⇒ `golden/runtime/durable_truth.json`，ADR 0006 R3 硬要求，机械登记。
6. **`WithApprovalFactLog` test-only export**——**生产零调用**（生产走 `features/evolution/governance/checkpoint.go:190` 的 `BindAttention`），只被同包 `attention_log_test.go` 与别包 `approval_channel_anchor_test.go:111` 用。**裁定：治理测试改走 `BindAttention`，选项降未导出 `withApprovalFactLog`**。此法把测试从测试专用构造器移到**生产接缝**，R29 消费者锚更强。**不记 waiver**。
7. **`MigrateShardedRoot` 失效基线条目**（既有 warn）⇒ 删，`scripts/gates/product_surface_baseline.json`。
8. **doc_budget −2** ⇒ `--tighten`（只降不升）与文档改动**同提交**；绿态待提交授权。
9. **dod_coverage P0–P8** ⇒ `MUT-DOD-P4-cold-unbounded` 的 `not_killed` 只由登记表自身字段陈述、无 run 产物 ⇒ **自证无效**；P0/P1/P2/P3/P5/P6/P7/P8 未注册。归 F3，**变异锚须改形状判据非行号**（已两次被行号漂移击中）。

**撤回一条自己的怀疑**：曾疑 `AttentionFactLog` 无生产消费者（包外只见测试）。grep 全仓后确认 `checkpoint.go:190` 即生产接线，**怀疑不成立，撤回**。与 R76 同律：断言消费者缺席前须 grep 全仓含跨包调用。

### R82 06.R5 收口：归因被实测部分改写；剩余 closing 差额裁定为「随 08.R3 自解」，不再优化（2026-11-16）

**06.R5 未勾，正确**——末窗绝对成本 271.05／304.79／323.87ms 对改前 262.36ms（+3.3%／+16.2%／+23.5%），未稳回 243–264。交付①②③完成且取证扎实。

**R78 的「每载体一道屏障」假设被实测部分推翻（代理主动纠，正面榜样）**：探针分列计数器后查明 **table=2 是「resetTable＋长任务键触发步长 96→192 全表重放」两道**，非每载体一道。主项是**每次重写→`rebuild()` 整表重述＋全量回放＋步长重放**。改法随之改为 `rewriteInto` 答出语句 → `reopened(statements)` → `restate`（零回放、一次定宽定容、不取自己的屏障）。**实测 frame 7→3 道、closing 9→5 道，重述回放帧数 16–43→0。**

**编排者裁定剩余差额归属（读码实证，非推断）**：`l.rewrite(keep, replaced)` 的**唯一生产调用方**是 `journal.go:682`，其上下文为 `copyToCold`／`verifyColdCopy`／`RelocateReport`／`settled` ⇒ **relocation（退位）趟**。08.R3 整体删除退位 ⇒ 无 rewrite ⇒ 无 `reopened`/`restate` ⇒ **closing 那「整份重述必付的一次屏障对＋1.5MB 表填充」随之消失**。故：
- **不再为 closing 加优化**。给待删路径加特异化结构违「更少特异化代码」，且 06.R5 已逐条排除三案（不写表头／原地补丁／开档从 cut 重述，design §二十四·四），排除成立。
- **硬序改为 06.R5 → V2 → 08.R3 → 重测症状锚**。05.R3-甲 与 08.R3 同包不相交否须再算（08.R3 删 journal.go 的 relocation 面，05.R3-甲 改 journal_kv.go/atomic_store.go，**同包 ⇒ 串行**）。
- **08.R3 收口须复跑三项确证自解**：症状锚绝对成本、`StorageConfig` 字段数（R81 第 1 条）、`exported_interfaces` 计数（R81 第 4 条）。**不得以「推断自解」代替复跑**。

**⚠️ 06.R5 新锚须随 08.R3 重定范围**：`TestCheckpointAndClosePayOneCarrierBarrier`（**V2 已改名 `TestCheckpointAndClosePayOneIndexBarrier`**，判据未动）之名原含退役词 `Carrier`，又钉「one barrier per carrier」这一**已被其自身实测改写的假设**；且其对象（rewrite→restate）在 08.R3 后不存在。**08.R3 须重述或随机制删除该锚**，不得留一枚钉着已删路径的绿锚。

**环境如实记**：本机 load 4.9–5.0（Qoder/VPN 常驻，非测试进程）⇒ 单跑读数不可判 ±10%；代理以相邻复跑＋症状锚自带离散控制（141.6/144.4、153.1/160.7、158.7/159.1）处置，**未把噪声当改善**。09.R2 校准须把该常驻负载纳入判据。

**门禁面缺口第三次确证**：06.R5 新建锚名含 `Carrier`，其令亦无词汇门。R79 的全门条款自此对后续每单生效。

### R83 两个新导出名逐名裁定：均成立，按名抬升棘轮（2026-11-16，编排者读码自裁）

**裁定依据是冻结架构，不是偏好。**

**① `AttentionFactLog`（07.R2，`runtime/task/approval/attention_log.go:35`）＝必需端口，内化不可行。**
- `golden/runtime/architecture.json` 的 `edges` 明写：`runtime/task/approval` 的 `forbidden` 含 **`memory`**；`features/evolution/governance` 的 `forbidden` 亦含 **`memory`**。
- grep 实证：`features/` 下生产 `.go` **零** `memory/ledger` 导入。
- 该接口签名只用内置类型（`Put(ctx,string,[]byte) error`／`Scan(ctx,string) (map[string][]byte,error)`／`Sync(ctx) error`），正是 attention_log.go:30-34 原文声明的意图：「使 memory tier 的 fact log 结构满足，两向均无需包边」。
- ⇒ **内化它等于要求 governance/approval 直接 import memory，违冻结包边**。故端口成立，`exported_interfaces_max 84→85` **按名抬升**，名＝`AttentionFactLog`，理由＝冻结包边下的唯一跨边界形态。
- S1「三方法 > 2」与 S4「缺规范样例」随绑定一并办理（样例落 `examples/contract/runtime_task_approval/AttentionFactLog_example.go`）。三方法是 Put/Scan/Sync 三个语义不同的动作，非可合并的冗余端口。

**② `FactCommitOutcome`（11.R2，`memory/ledger/segment_store.go:707-716`）＝合法公共契约。**
- 五分类 `FactCommitted`／`FactAlreadyPresent`／`FactConflict`／`FactUncertain`／`FactRefused` 是 G1 冻结的提交结果契约；其中 `FactAlreadyPresent` **不是错误**，故不能退化为 typed error 而必须是被调用方命名的导出枚举。
- 消费者 `assembly` 在 `edges` 中被允许 import `memory`，跨包命名该枚举是契约要求，非可内化的机制细节。
- ⇒ 绑定为 `read` kind（提交结果读面），`new_max` 按名抬升。

**抬升纪律（实施单必须遵守）**：
- **不得凭本条的「56／85」直接写数**。实施单须从门的实际输出**枚举新增导出名清单**，确认恰为这两个（若另有第三名，逐个裁定后再抬，不得笼统 +2）。
- `concept_ratchet.note` 须追加逐名清单与理由，格式沿用既有 W3 段（`54→56 (runtime-recovery-ideal …: FactCommitOutcome …, AttentionFactLog …)`、`84→85 (AttentionFactLog 是冻结包边下的结构端口)`）。
- 绑定须指向**活的主 spec**（门会校验 spec 存在与结构合法），不得指向本变更的 delta spec 或已归档路径。
- `removed_min=5`  floor 不动；08.R3 删除退位会带走 `RelocateReport`／`SettleAccount` 等名，**届时 removed 计数上升是好事，不得因此下调 floor**。

**并发切分（四单写面互不相交）**：V2＝`memory/ledger/journal.go`＋`journal_test.go`＋06 域两文档；G1＝`golden/runtime/durable_truth.json`＋`scripts/gates/product_surface_baseline.json`；G2＝`runtime/task/approval/approval_store.go`＋`attention_log_test.go`＋`features/evolution/governance/approval_channel_anchor_test.go`；G3＝`golden/runtime/architecture.json`＋新建 `examples/contract/runtime_task_approval/AttentionFactLog_example.go`。**G2/G3 同触 approval 包构建闭包 ⇒ 只跑文本门，编译验证统一留给后续验证单**（V2 正在改 ledger 标识符，此刻编译会撞中间态）。

### R84 真缺陷：审批请求记录的生产写腿从未接线，且静默报成功（2026-11-16，G1 查出／G2 停在检查点／G2′ 修复）

**这是本轮最严重的发现，性质是缺陷不是重构。** 两条注入路径设**不同字段**：`WithApprovalFactLog` 设 `a.factLog`（全仓唯一赋值点），`BindAttention` 只设 `attentionLedger.log` 而**从不设 `a.factLog`**；写腿 `recordRequestLocked` 读 `a.factLog`，**nil 时静默 return nil**。生产构造不传该选项、只走 `BindAttention` ⇒ **请求记录在生产永不落盘且报成功**，「approver 拿到的身份跨被丢弃的 cut 存活」从未接线。测试绿只因用了测试专用构造器——**R29「组件绿≠生产消费者成立」的又一实例，且这次是静默 nil 分支掩盖**。

**三单接力，行为均正确**：G1 登记持久真相时读码查出写腿缺口，如实写进 reason 并上报不擅改；G2 按令在检查点**零编辑停下**，独立给出测试级后果（`dropBook` 用全新 TempDir、cut 已清空，唯一座位来源就是该 record；gate 铸随机身份故无替代来源）＋一条 rebuild 顺序风险；G2′ 执行修复。

**裁定比 G2 的 A 出口更进一步**：A 只补 `a.factLog = log` 会**留下两条注入路径**，正是致病的特异化结构。按零残留与单一权威裁**A＋整体删除选项与构造时折叠块**，只留 `BindAttention` 一条接缝。

**G2′ 凭据（编排者待复核）**：`BindAttention` 在 `a.mu` 锁内发布 face；选项与构造块整体删除，级联检查确认 `recordGap`／`context` 未降级为死引用，`a.factLog` 全仓只剩写点 :159 与读点 :614；五处测试改走接缝，三处 consumer 注释改真话。**变异证明**：去掉 `a.factLog = log` → 三枚新锚＋`TestOpenApprovalIdentityMustSurviveTheCut` **具名转红**（原文含 `the identity the bound book answered outlives no process`、`the gate parked the call again under a newly minted identity`），恢复后绿。`approval` 10 测／`governance` 45 测全绿，治理锚 ×10 PASS，`product_surface` **0 error 0 warning**，旧名 `.go` 面零残留。
- **rebuild 顺序风险取证成立（不可达）**：`sameFact` 比 Type/Content/Meta **不比 Timestamp**；`fileQuestion` 与请求文件同出一次注册的 `Reason`/`ExpiresMs` ⇒ 逐字段同值；`Request` 对已知 id 直接返回 standing evidence、`rebuild` 先落 evidence ⇒ API 堵死分歧重注册。三锚覆盖未裁决／已裁决／seated 三序，×10 PASS=30。
- **自纠一项**：新建 `attention_bind_order_test.go` 被 `responsibility-fragmentation` 判红，按门给的出口(1)并入镜像文件 `approval_store_test.go` 并删新文件 ⇒ 注释门回 0。**这是门起了作用的正例。**
- **⚠️ 行为代价如实记**：生产每次注册由 1 道屏障变 **2 道**（attention 事实＋请求记录各一次 Put+Sync）。按 R71「成本主项是屏障道数」约 +5ms/注册。**这是正确性必需代价，但可优化**：两者同批 staged 后共享一道屏障即可回到 1 道。**登记为 09.R2 校准后的候选优化，不在本单做**（正确性先于成本）。
- **连带伪陈述已修**：G1 写的 `golden/runtime/durable_truth.json` reason 原句描述该缺口，修复后成伪 ⇒ 编排者已改写为真话（写腿由 `BindAttention` 承担）。**文档随代码走，脱节即修。**

### R85 用户裁定：三个同族持久面端口收口为共享叶子统一端口（2026-11-16）

**编排者自判后呈裁，用户选「共享叶子统一端口」。**

**事实**：`runtime/state.FactLog`（Put/Get/Scan/Sync，4 方法，既有基线）、`features/evolution/governance.GovernanceEventLog`（Put/Scan/Sync，PR #24 `0fcbfa26` 引入，基线 commit `3b727199` 已有 8 处命中，S1 有 2026-11-05 债务条目）、`runtime/task/approval.AttentionFactLog`（Put/Scan/Sync，**本轮 07.R2 新增未提交**）。**后两者三方法逐字同构，且由完全相同的两个生产类型实现**（`memory/ledger.TierFacts` tier_facts.go:30/47/60、`assembly.memoryFactLog` commit.go:312/331/344）。成因：approval 与 governance 的 `forbidden` 均含 `memory` 与 `runtime/state`，**缺一个共享叶子**，于是各自声明结构端口接收 assembly 递来的面。

**规划缺陷定性**：这是自审清单 **5.9「新增 capability 前的既有能力重叠检索」**漏做——新增端口前未检索既有同域同义能力。记为规划教训。

**裁定交付（新叶，排 08.R3 之后，不阻塞症状锚转绿）**：
- 统一端口放**所有消费者都可 import 的叶子**（`api/event` 或新建 api 叶子；`api/event` 是纯叶子只 import stdlib，端口签名只用 `context`＋内置类型故相容）。**语义归属须推导**：该端口是「事实耐久写读面」，不是事件词汇本身，故倾向新建叶子而非塞进 `api/event`——实施单须先给归属推导再动手，不得凭省事选。
- 删 `GovernanceEventLog` 与 `AttentionFactLog`，移 `state.FactLog`。**净减 2 个导出接口（85→83，低于原上限 84）⇒ G3 的 `exported_interfaces_max 84→85` 抬升须撤回并下调**，`new_max` 亦按实际新增名重算。
- S1 只需一条债务条目（4 方法，沿用既有 `FactLog` 理由），**不再需要为 `AttentionFactLog` 加条目**；S4 红线 marker 随端口迁移一并落进类型自己的连续 doc 块（G3 已实证：文件头红线被 `package` 行隔断，`doc_block()` 读不到）。
- 波及面须逐处枚举：`denial.go`／`attention_log.go`／`attention_store.go`／`runtime/state/durable.go`／`assembly/{commit,governance_checkpoint,host,host_delivery}.go`／`examples/contract/{runtime_state,runtime_task_approval,runtime_agent}`／golden 绑定与 S1 基线路径／主 spec 的能力名。**edges 若需调整须显式记入 architecture.json 并给理由**。
- **不得留兼容别名**（零残留）：旧名整体删除，不留 `type GovernanceEventLog = event.FactLog` 之类别名。

**当前 concept_budget 状态**：剩 S1（AttentionFactLog 3 方法）、S4（红线 marker）、S2（StorageConfig 7 字段，08.R3 自解）三条，**均由本叶与 08.R3 收口，不单独加债务条目**（加了即成待撤残留）。

### R86 08.R3 第四次撞上限；编排者诊断现场并亲自收尾生产面，剩余量预先枚举后窄派（2026-11-16）

**08.R3 撞 150-turn**，末句「现在处理 kv_original_store.go 与 config.go」。**编排者立即诊断而非重派**：

**现场实测**：`go build ./...` **exit 1，仅 2 条错误**（`config.go:317/318` 调已删的 `WithColdRoot`）⇒ 生产删除已近完成，非半删烂摊子。**第 0 步夹具根已取到**（`/tmp/recovery-08r3/fixture-root/` ＋ `fixture-meta.json` 22800 字节含逐键 sha256）——**08.R4 的输入保住了，这是本单最关键的一步，前单在撞限前完成了它**。

**级联判定（编排者读码，逐个 grep 计数）**：`errStopIndexWalk` 另有 4 处消费者（checkpoint.go:1002/1004/1038/1047）⇒ **保留**；`relocatableStorage` 删字段后成恒等函数 ⇒ 连唯一调用方 run.go:622 一并删；`coldDirName` 仍被**尚未删除的退位测试文件**引用 ⇒ 待测试面删完再判；`filepath` 在 config.go 另有 5 处使用 ⇒ import 保留。

**编排者亲自收尾的生产面（6 处编辑）**：`config.go` 删 `RetireFacts` 字段＋doc、删冷档接线块；`assembly/checkpoint.go` 删 `relocateSettledFacts` 调用、删 `relocateSettledFacts` 与 `keptHotKeys` 两函数及其 doc；`assembly/run.go` 删 `relocatableStorage` 并把调用方改为 `storage := *cfg.storage`。**结果 `go build ./...` exit 0（亲验）**。

**测试面一个文件都还没删**（`go build` 不编译测试故只报 2 错，`go vet` 才暴露）：`go vet ./memory/ledger/ ./assembly/` exit 1，两处具名——`cold_archive_gate_test.go:50` undefined `RelocateReport`、`recovery_judgment_test.go:141` `RetireFacts` undefined。四个退位测试文件仍在（retirement_snapshot 286 行／cold_archive 565／cold_archive_gate 290／journal_index_cut 649）。

**处置**：派 **08.R3b 窄单**只做测试面，派发令里**把剩余量预先枚举完毕**（已删清单、保留清单与理由、两处具名编译错、四个候选文件与行数、G6 清单位置、前单分析产物 `/tmp/recovery-08r3/*.txt` 路径、夹具根禁改），使其**不必重新探索**。

**流程教训（第四次撞限，须改派发法）**：原 08.R3 一单装了「取夹具根＋删 ~25 符号＋级联枚举＋删 1500 行测试＋改名＋9 条验证」。**撞限后的正确动作不是原样重派，而是编排者诊断现场→亲自收掉小而确定的部分→把剩余量枚举成清单再窄派**。探索成本是撞限主因，预先枚举即消除它。此后大删除单一律先由编排者取现场读数再拆。

### R87 08.R3b 收口；删退位暴露索引一处潜伏真缺陷＝槽表负载计数不耐久（2026-11-16）

**08.R3b 凭据（编排者待复核）**：`go build ./...` 0、`go vet ./memory/ledger/ ./assembly/ ./tests/...` 0、`./memory/ledger/` **0（29.9s，54 顶层／108 含子）**、25 符号 × 818 文件词边界 grep **生产 0／测试 0**、两注释门 **0／0**、词汇门 **0**。整删 `cold_archive_test.go`(565)／`retirement_snapshot_test.go`(286)／`recover_kv_relocated_root_test.go`(139)／`symptom_anchor_journal_test.go`；`cold_archive_gate_test.go` 改名 **`config_test.go`**（镜像 config.go，flock 正在该文件）只留两枚写权锚；`coldDirName` 级联删除。

**⚠️ 真缺陷（编排者读码确证，非代理推断）——`journalAddress.used` 不耐久**：
- 增长规则 `journal.go:1321` `if a.used*100 >= a.capacity*journalSlotLoadPercent(50)` → `grow(capacity*2)`；探针上限 `journalSlotMaxProbe = 128`（:127）。
- `a.used` 全部赋值点仅三处：`:1334` 插入 ++、`:1532` grow 重算、`:1553` resetTable 归零。
- `openJournalAddress`（:1058）初始化**不设 used**；`readHeader()`（:1754-1776）恢复 capacity/stride/三水位而**不恢复 used**——表头结构只有 Magic/Version/Capacity/Stride/Seq/ValuesSize/OrderSize/Sum，**无 Used 字段**（52 字节，Sum 覆盖 body[0:48]）。
- ⇒ **干净关档后重开一个已 ≥50% 满的表，计数从 0 起算，增长规则永不触发**，探针链撞 128 上限，`probe`(:1362)/`lookupOnce`(:1409) 报 `ErrJournalCorrupt: …/kv/slots holds no free slot within 128 of "16:idx:cmp-…"`。
- **为什么潜伏至今**：退位在时热键集被切面 bound 住，表从未真正 dense；**删退位后全部事实留热层，密度首次越过阈值**。这是「潜在红≠安全」「修一层≠复合有界」的又一实例，也确证反转必须连索引一起验而非只删机制。

**修法推导（冻结，06.R6 执行）**：表头是派生索引自身对「形状＋水位」的陈述，**负载计数属形状，应由表头陈述**；开档读一个表头即知负载，**不扫表**，保 S2 有界开档。两条替代已否：①开档扫表重算＝O(capacity) 读，违有界开档；②从 order 推＝order 陈述提交非存活键，推不出。实施＝表头加 `Used`、52→60 字节、Sum 覆盖 [0:48]→[0:56]、**Version bump 使旧表头被拒走既有 rebuild**（零残留，不留双版本兼容读）、`readHeader` 恢复并校验 `0 ≤ Used ≤ Capacity`、**枚举全部 `writeSlotHeaderAt`/`headerOf` 调用点逐个陈述 used**（漏一个即该路径重开后计数又归零）。

**assembly 7 枚红的归属（逐条读失败原文，未笼统归为并发漂移）**：
| 红 | 归属 |
|---|---|
| `TestBootFixtureGrowsTasksAcrossGenerations`／`TestSymptomAnchorBootCostFlatAcrossGenerations`／`TestSymptomAnchorSessionBootCostFlatAcrossGenerations`／`TestSymptomAnchorPerGenerationCostFlatOverALongRun` | **同一根因＝上述 used 缺陷**，四处均死在夹具 commit 步 ⇒ **06.R6** |
| `TestAnchoredBootMatchesFullBoot` | `anchored_boot_parity_test.go:225` `read …/kv/journal.log: file already closed`＝关档竞态，**另一机制，待独立诊断** |
| `TestGovernanceSliceWiredInProduction` | 已知，07.R2 载荷裁减后果 ⇒ assembly 锚合批单重述 |
| `TestBootSeatsTheMailboxAfterTheDurableAccount` | `task_custody_anchor_test.go:97` `the boot read no key of the task that landed past the cut`＝尾谓词取数接缝缺席 ⇒ **05.R3-甲**（R80 已冻结签名） |

**症状锚无可引用读数**：它在第 17 代夹具 commit 处即失败，未进入任何窗口计量。代理**拒绝编造首窗/末窗数字**，正确。

**编排者裁定 08.R3b 的六项自决（全部接受）**：
1. `journal_index_cut_test.go` **去退位化（649→55 行）而非整删**——与派发令字面冲突，但 `readJournalIndexCut` 仍有生产消费者（journal.go:654 开档路径），存留锚 `TestOpenReadsTheLogWholeBesideNoIndexCut` 仍有对象；其余 5 枚依赖已无定义的 `placeIndexCut`/`retireSettled`，钉空机制。**接受**，余归 08.R4。
2. `writeJournalIndexCut`/`appendJournalIndexCut` 现**零调用**但未删——属切面族，单删留半套机制。**接受，登记为 08.R4 必删项**。
3. 多删 `symptom_anchor_journal_test.go`（主题＝退位使 journal.log 有界；R50 已裁磁盘增长为「显式声明、不承诺有界」）⇒ 无存活判据损失。**接受**。
4. `TestJournalBytesBoundedByLiveSet` 整删依 06/design §十七·八。**接受**。
5. 另删 7 枚对象已消失的锚＋7 个级联 helper。**接受**，但**逐项记为覆盖面缩小**（见下）。
6. `symptom_anchor_recovery_test.go` 删冷读成本轴与两退位态对照、保留 journalBytes/indexEntries/durable/wallClock 四道界。**接受**——装置随机制删，存活判据一字未动。

**覆盖面缩小显式登记（不得追认为「仍成立」）**：失去「退位趟在 walk 中途落地」交错腿、冷读成本轴、两退位态对照整测、S1 journal 字节有界锚、切面放置侧 5 锚。**其对象均已不存在**；替代判据归 06.R5/09 域。**09 的验收矩阵不得再引用这些锚名**。存活的反转目标锚只剩 `TestSymptomAnchorPerGenerationCostFlatOverALongRun`（R33 症状直译锚，不可替换）。

**08.R3 复选框仍未勾**（其验证面含 `./assembly/` exit 0，当前 7 红）——代理拒绝谎报，正确。待 06.R6＋05.R3-甲＋锚合批后由独立验证单裁决。

**硬序更新**：**06.R6（used 耐久化，阻塞 4 红含目标锚）→ 08.R4（删切面＋外部折回脚本，用 `/tmp/recovery-08r3/fixture-root`）→ 05.R3-甲（尾谓词接缝）→ assembly 锚合批（含关档竞态诊断）→ 10.R5/R6/R7/R8 → 09.R2/R3 → F1–F4**。

### R88 06.R6 收口：used 已耐久化，两枚消费者锚转绿（2026-11-16）

**凭据**：表头加 `Used`，**放进既有 padding**（树上 `journalSlotHeaderBytes` 实为 **64** 非编排者以为的 52，[52:64] 是 12 字节 pad）⇒ Used[48:56]／Sum[56:60]／pad[60:64]，**常量与全部记录偏移零变化**，Sum 覆盖 [0:48]→[0:56]，`journalSlotVersion` **1→2**（旧表头被拒走既有 rebuild，零残留无兼容读）。代理申报此处与派发令文字不符但动面更小——**接受，且编排者原以为的 52 是读码不精，记自误**。
- **全部表头写点枚举无遗漏**：`writeSlotHeaderAt` 仅两调用点（:1517 grow 用重算 used、:1580 writeHeader 用实时 `a.used`）；`resetTable` 先归零再 writeHeader；flush/close/derive/rebuild 均汇到 :1580。`readHeader` :1772-1773 校验 `Used<0 || Used>Capacity` 即拒、:1779 恢复。
- **正锚** `TestAddressableIndexKeepsTheLoadOfATableItReopens`：三轮会话 520→1004→524 键把 2048 槽填满而本会话自计 524<1024 从不求增长 ⇒ **修前红原文** `holds no free slot within 128 of "0:load:01651"`；**变异证明**（去掉 `a.used = header.Used`）同因红；恢复绿。
- **负锚** `TestAddressableIndexStatesTheTableAgainBesideALoadNoTableHolds`：Used=Capacity+1 与 Used=-1 两子例（带重算合法 Sum）⇒ 拒表头、从日志重算 used=550、按身份读回 160 字节原值；删校验则两子例同时红。×10 PASS 20/20。
- `go build` 0、`go vet` 0、`./memory/ledger/` **0（56 顶层）**、`-race -count=3` **0 竞态（137.9s）**、两注释门 0／0、词汇门 0。
- **消费者三锚：2 绿 1 红**——`TestBootFixtureGrowsTasksAcrossGenerations` PASS、`TestSymptomAnchorSessionBootCostFlatAcrossGenerations` PASS（二者原死在夹具 commit 步）；`TestSymptomAnchorBootCostFlatAcrossGenerations` 仍红，**根因另在，见 R89**。
- **症状锚仍红**：两次运行首窗 91.0／191.2ms、末窗 492.5／506.1ms，比值 **5.41×／2.65×**（界 1.3）。末窗绝对值稳定、首窗受环境负载波动 ⇒ 比值不稳但两次均越界。**代理拒绝把波动当改善，正确。**
- **连带如实报**：`/tmp/recovery-08r3/fixture-root/kv/slots` 是 v1 旧格式（98368 字节，capacity 1024／stride 96），version bump 后首开该根会拒表头→rebuild（事实不丢，首启付一次全量重述）。**08.R4 若对该表头字节做对照，期望值须按 v2 重取。**

### R89 ⚠️ 反转的核心未接线：可寻址索引读侧零生产消费者（2026-11-16，编排者读码确证）

**这是本变更最严重的发现，性质是「核心产出物未接入生产前向」，不是成本回归。**

**证据（逐条 grep，非推断）**：
- `l.addr` 在生产**只有三个触点**：`journal.go:2035` `addr.note(spans, l.size)`（写维护）、`:2053` `addr.confirm()`（写维护）、`:2120` `addr.close()`（ teardown）。
- `journalAddress.read(key)`（:1617）**已完整实现**，其 doc 原文承诺「the read costs the value alone — never the frame that carries it and never a walk of the statements behind it」，**全仓零调用方**（`grep addr.read` 空）。`a.lookup` 的唯一调用方就是它自己（:1618）；`a.eachNewestFirst`（:1690）同样只见定义。
- **生产读路径仍是** `journalLog.read`（:2064）→ `l.index[key]`；`journal_kv.go:243` 亦读 `k.log.index`。
- **`l.index` 由开档整篇重放构建**：`openJournalLog` 的 `from` 取自 `readJournalIndexCut`（:654），而 `writeJournalIndexCut`（:851）**全仓零调用**（`placeIndexCut` 已随退位删除）⇒ `from` 恒 0 ⇒ `journalReplay(file, 0, size, …)` 整篇重放 ⇒ `l.openBytes = size - 0`。
- `statesAddressMaintenance(on)` 由 `cfg.WriteRights` 门控 ⇒ **只读视图根本没有 addr**。

**⇒ 结论**：可寻址索引**建了、每提交维护了、组件测了，但生产前向从不走它**。boot 仍整篇重放，随历史线性增长。**反转的核心承诺（有界开档）在生产读路径上未实现。**

**这解释了锚绿而症状红的分裂**：06.R2/S1c 的锚量的是 `a.openBytes`（索引层自己的开档字节），是**代理量**；症状锚量 `l.openBytes` 与墙钟，才是 boot 真付的读。**正是 DoD 覆盖门警告的「锚只判断代理量」病**，也是 P1 漏案「内核写完＋对拍全绿而生产前向从不走内核」的同形复发。

**哲学定性**：`l.index`（内存 map，重放构建）与 `a.slots`（磁盘槽表）**是 key→span 的两份陈述＝第二真相**。且 `l.index` 本身随历史无界增长（内存），违「派生载体不得迫使全量读」。故正确终态是**单一权威**：槽表答点读、order 文件答枚举、开档只读表头＋order 尾，`l.index` 删除。

**处置（新叶 06.R7，先取证后实施，两单分派）**：
- **06.R7a 设计取证（只读，隔离树）**：枚举 `l.index`／`l.order`／`l.tail` 的**全部**生产消费者与各自读形状；核 `addr` 能否逐形状服务（点读／墓碑／枚举／提交序／null 值）；核只读视图开 addr 的写权语义（维护仍按写权门控，读侧恒可用）；产出迁移设计与**一枚红锚**证明「boot 实际读字节随历史增长」且该锚量的是 `l.openBytes` 非 `a.openBytes`。**不得实施。**
- **06.R7b 实施**：按 7a 冻结的设计接线读侧、删 `l.index` 第二真相、开档不再整篇重放。**08.R4 删切面必须在 7b 之后**——否则删掉 `readJournalIndexCut` 会让开档连唯一的形式上的 `from` 都失去，而替代尚未接线。
- **硬序再更新**：**06.R7a → 06.R7b → 08.R4 → 05.R3-甲 → assembly 锚合批 → 10.R5–R8 → 09.R2/R3 → F1–F4**。06.R6 已完，不阻塞 7a。
- **09 域验收面须改**：凡以 `a.openBytes` 为判据的锚，须追加或改为以 `l.openBytes`／墙钟为判据；**代理量不得冒充 boot 成本**。此项归 09.R2 校准单一并办理。

### R90 06.R7a 取证收口：三处反驳全部核实成立，编排者纠自己定性；另查出一处数据损坏缺陷（2026-11-16）

**06.R7a 零 `.go` 改动，探针只在 `/tmp/recovery-06r7a/`，追加 06/design.md §二十五（151 行，既有内容一字未改）。三处反驳编排者逐条读码复核，全部成立：**

**反驳① 成立——本叶不能让症状锚转绿。** `assembly/symptom_anchor_recovery_test.go:483-495` 有**三道独立界**：`journalBytes`(:485)、`indexEntries`(:489，原文「publishes a resident index of N entries」＝**store 层常驻索引** `s.index`)、`durableLoadCount`(:493)。删 `l.index` 只解第一道；`s.index` 由 `commitOrderOf` 走引擎全键构建，随历史增长，**与 `l.index` 无关**。⇒ **DoD 只能写「journalBytes 判据转绿」，不得声称症状锚转绿**。
- **反转目标的完整条件因此是三层去物化**：日志层（06.R7）＋**store 层常驻索引**（归 06.R3/N24，须扩范围）＋耐久任务账扫描（05.R3-甲）。**退位原先正是靠搬迁 bound 住 store 常驻索引；删退位后该 bound 消失，必须由「不物化全量」替代，而不是由另一个搬迁机制替代。**

**反驳② 成立——「第二真相」用词不准，R89 该句勘误。** `l.index` **无持久形态**，是内存**重复物化**；真正被持久陈述两次的是 order 行与槽记录，而那一对是 06/design §十七·二／§二十一·一 **有意冻结的分工**（order 答序、槽表答点地址）。删 `l.index` 不减少任何持久陈述。⇒ **缺陷正确定性＝「派生载体迫使全量读 ＋ 内存随历史无界」**，非「持久第二真相」。R89 哲学定性段按此勘误，其余结论（读侧零消费者、必须接线）不变。

**反驳③ 成立且严重度最高——只读视图今日即会写日志并截断在写帧（数据损坏）。**
- `openJournalLog:642` 无条件 `os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)`；`:672` `complete != size` 即 `file.Truncate(complete)`。
- `scripts/recover-kv/main.go:104` `WriteRights: restore` ⇒ **dry-run 时 false，不持 flock**，却仍开 O_RDWR 并可截断；`:180` `OpenAtomicStore` 更完全不声明写权。
- ⇒ **活写者写到一半时开只读视图，会把在写的那帧读成撕裂尾并截掉，写者 `O_APPEND` 落在新 EOF**。而该工具 `:215` 自称「dry run, nothing written」——**伪陈述，且是对外运维承诺**。
- **另实测**：只读视图开 addr 也写盘——`openOrder:1114`/`openSlots:1124` 的 `O_CREATE`、`openJournalAddress:1088/1097/1103` 的 rebuild/Truncate/derive、`writeHeader:1579`，**三者全在 `acquireRootWriteLock` 之外，违 config.go:270 一写者不变量**。探针读数：第二个开档 `openBytes=288`（整篇重放）、槽表重写 98KB、order 截到 0 再重述；从未陈述索引的根开一次即落盘 slots 98368B＋order 69B。

**06.R7a 冻结的读面设计（编排者采纳）**：水位只取表头；`journalReplay` 只读 `[W=header.ValuesSize, S)` 一趟进**视图私有 overlay**（键→span、墓碑集、提交序）；`complete<S` **不截断**；四读形状先 overlay 后槽表；`stated()` 界改 `complete`。窗口由 `journalIndexSyncBytes`(64KiB) 界住⇒最多几百提交、**与历史无关**。停稳根实测 `openBytes=64`、两文件 size/mtime 不变。读面禁 `O_CREATE`/`Truncate`/`rebuild`；表头缺失或越过日志⇒具名 `ErrAddressUnstated`；**表头落后不是拒绝条件（那是窗口）**。
- **关键约束（实测依据，不得省）**：窗口**必须重放日志帧而非只读 order 行**——`fact_commit_batch_test.go:253` 要求独立开档读回一条 `ErrCommitUncertain` 的提交，**那条路径上 order 行根本不存在**。
- 墓碑/null 语义逐行比对**等价**（`journalSlotAbsent`↔`deleted:true` 均答 `(nil,false,nil)`；`span.Null`↔`valueNull` 均答 `(nil,true,nil)`）。**不等价的是新增守卫 `stated:1421`**：它按视图水位卡 span，读视图水位只来自表头 ⇒ 窗口内键会被答成「不持有」，**这是设计必须补的面**（overlay 先答即解）。
- 需新增一个方法：`Read{Prefix}` 的键＋值一趟交付（现组合对每键两次探测），建议 `eachHeld`；是否值得新增归实施单实测。
- 分派点是 `config.go:238` 那一个布尔；生产只写视图 `run.go:605`，**唯一生产只读视图 `recover-kv/main.go:104`**。

**迁移序列（冻结）**：**S1** 拆读写两面＋读面零写锚＋**并入日志截断缺陷修复** → **S2** 四读路径切 addr（保留 `l.index` 零读） → **S3** 停整篇重放＋整批删除（**必须原子**：开档只读窗口后 `l.index` 残缺，停重放与删读者不可拆）。
- **锚处置**：`:485` 改绑常量界（收紧）；**`:449` 必须重述**——停稳根正确地读 0 字节，原判据会把正确实现永久判红；`:489/:493` 本叶不解，归 store 层与 05.R3-甲。
- `openBytes` 新语义＝`S−W`，停稳根为 0。

**须实施单实测的未闭合项（不得假装已闭合）**：窗口真实长度（日志字节非索引字节）；`rollback:1241` 窗口内读面答案；只读面在真实链路的成本（零样本）；`keys(prefix)` 扫表绝对成本（1024×96，512 键翻倍）；掉电下「表头指窗口、槽记录未落盘」自愈成本；`eachHeld` 值不值得新增。

**硬序再更新**：**06.R7b-S1（含数据损坏修复，最高优先）→ S2 → S3 → 06.R3 扩范围（store 常驻索引去物化）→ 08.R4 → 05.R3-甲 → assembly 锚合批 → 10.R5–R8 → 09.R2/R3 → F1–F4**。

### R91 S1 收口：数据损坏已修；实测反驳「窗口日志字节有界」，新叶 06.R8 承担（2026-11-16）

**S1 凭据（编排者待复核）**：读写两面按 `WriteRights` 一个布尔分派（`config.go:232/238`）；读面日志 `O_RDONLY` 无 `O_CREATE` 无 `MkdirAll`、slots/order 走 `os.Open`，`statesIndexFiles()` 为 false 使三处修复路径不可达；写面今日路径一字未改。新具名错误 `ErrJournalAddressUnstated`（表头缺失/不可解码/索引文件不存在⇒拒；**表头落后不是拒绝条件，那是 window**）。`statesAddressMaintenance` 更名 `openAddressIndex`（名字若不改正即陈述假机理）。
- **正锚** `TestJournalReadViewWritesNothingBesideTheWriter`（4 子例：停稳根／表头落后根／从未陈述索引根／5.47MB 历史）×10 → **10 顶层＋40 子测 PASS**。落后根另断言 window 内每键读面答案 == 写视图 `l.index` 答案、删除键答不持有、**window span 越过水位仍被判 stated**。
- **负锚** `TestJournalReadViewLeavesTheTornTailOfTheWriter` ×10 PASS；**变异证明**（读面改回 O_RDWR＋无条件 Truncate）红原文 `the log holds 71 byte(s) after the reading view, want the 99 the writer left: the reading view cut the frame it found torn`；恢复绿。
- **消费者锚·真实 CLI**：`go run ./scripts/recover-kv --root …` 两根（含 `/tmp/recovery-08r3/fixture-root` 只读）dry-run 前后 **size+mtime 快照 diff 空**；自造根上打印「dry run, nothing written」**已成真话**。**对照实证**：同根副本交写面开档后 order/slots 的 mtime 前移 ⇒ 旧路径确实落盘，读面不动。
- `go build` 0、`go vet`（含 scripts）0、`./memory/ledger/` **0（118 RUN／58 顶层＋60 子）**、`TestFactCommitAnswersNewExistingConflictUncertain` PASS（**uncertain 提交无 order 行故窗口必须重放帧**这条约束成立）、`-race -count=3` **0 竞态**、两注释门 0／0、词汇门 0。
- 症状锚仍红（`journalBytes=[0 136617 331335 565596 839094]`），**代理明确声明本单不解 :489/:493，未声称转绿**，正确。

**⚠️ 实测反驳冻结设计一处量化主张（成立，编排者采纳）**：§二十五·五 拟把 `journalBytes` 改绑 `journalSlotHeaderBytes + journalIndexSyncBytes`（≈65.6KB）常量界。**实测窗口日志字节达 5,471,780 B**——64KiB 界住的是**索引字节**，而「索引字节少、日志字节多」的提交（大 value／checkpoint）使单个预算窗口的**日志字节无上界**。采样原文：`watermark 0, log end 5497280, 5497280 字节越水位, staged 64608`（预算将满时 window 最大）；flush 后 `watermark 5498300, … 4080 字节越水位, staged 6480`。
- ⇒ **正确表述**：window 由「一个索引预算内尚未陈述的提交」构成，**提交数有界，日志字节无界**；「与历史无关」仅指不随历史总量增长。
- **编排者自裁修法（新叶 06.R8）**：boot 真付的量是**日志字节**，故屏障触发须**同时**看日志字节——`flush` 的条件改为「staged 索引字节 ≥ 预算 **或** staged 日志字节 ≥ 日志字节预算」。替代已否：「每提交推水位但只在预算满时 fsync」会让表头陈述未耐久的字节，违耐久性。代价＝大 value 负载屏障道数上升，**这是本征取舍**（既要窗口有界又不付屏障不可能），按 R71「fsync 道数才是主项」须实测入 09.R2。
- **S3 的锚界因此不得写常量**，须待 06.R8 落地后按新触发规则重定；**06.R8 排在 S2 之后、S3 之前**。

**编排者裁定 S1 的越界与自决（全部接受）**：
1. **跨白名单改 `atomic_store_test.go`／`kv_original_store_test.go`**——强制「未声明写权者不得写」必然使 5 处既有夹具红（它们在全新临时根上未声明写权即 Commit，**违 config.go:64-70 已文档化的契约**，今日只因实现未强制而通过）。改法最小：给 journal/localfile 配置加 `WriteRights: true`、新增 `openReaderAt`/`reopenTier` 清写权、6 处「活写者旁再开一档」改用读面，**零断言改动**。替代只有「读面保留 O_RDWR」＝派单明禁的残留。**接受，且这是派发令白名单不足而非代理越权**，记编排者自误。
2. **`journal_test.go:1139` 断言重写**——原断言「未声明写权者不持 addr」与冻结设计「读视图拿到 addr 的读面而非没有 addr」直接冲突，属**被取代旧判据**而非迁就回归。**接受**。
3. **新增 OrderSize 拒绝（计划外 4 行）**——表头声明的 order 字节数 > order 实长时拒绝。理由成立：写面靠 rebuild 自愈，**读面无修复通道**，放行则 `eachNewestFirst` 静默交付**部分**事件（recover-kv 的 GetEvent 正走此路）＝静默丢已记录事实。**接受，且这是必要补面**。
4. **`tornTail` 用包内字段承载**——唯一生产读视图 recover-kv 在白名单外不可改，故未新增导出方法（避免只活在测试里的导出 API）。**接受形态，但登记缺口**：「具名报告」目前只对包内测试可见，**运维看不到**。须在后续叶给 recover-kv 加打印（归 10.R6 或 S3），否则该报告不满足消费者锚。

**未闭合项（不得假装闭合）**：`rollback:1241` 窗口内答案（需注入 state() 失败，本单无注入面）；生产只读视图逐次读盘字节计量；`keys(prefix)` 扫表绝对成本（S2 才切读者）；掉电序「表头指窗口、槽记录未落盘」自愈成本；`eachHeld` 是否新增（S2）。

**硬序**：**S2（四读路径切 addr）→ 06.R8（窗口日志字节界）→ S3（停整篇重放＋删 l.index，原子）→ 06.R3 扩范围（store 常驻索引）→ 08.R4 → 05.R3-甲 → assembly 锚合批 → 10.R5–R8 → 09.R2/R3 → F1–F4**。

### R92 S2 收口：读侧已接线，l.index 零生产读点；三项待裁编排者已裁（2026-11-16）

**S2 凭据**：四读路径＋提交序走查全部委派 addr（`journal.go:2336/2342/2355/2267`、`journal_kv.go:260`）；`journalKV.eachCommittedNewestFirst` 的逐键判存在/判墓碑过滤**整条删除**（`eachNewestFirst` 自带去重与墓碑排除）。**`l.index`/`l.order` 零生产读点 grep 正证**（正则 `range l\.index|len\(l\.index\)|:= l\.index\[|, held := l\.index|:= l\.order\[|range l\.order` 无输出 exit 1；余 10 处皆写/创建）。写侧 `indexRecord`/`noteOrder`/`append` 原样保留作 S3 对拍基准。
- **新增 `eachHeld` 有实测支撑（非直觉）**：6400 键一趟扫表 **1.57MB／2.45ms**，逐键点读 **15.53ms**，纯取 **4.96ms** ⇒ 探链 ≈10.6ms ≈ **1.65µs/键**、为整趟扫表 **4.3×**；512 键同形 5.4×。故「每键多一次探测」是前缀读主导成本，`keys`/`namespaces`/`Read{Prefix}` 三处共用 `eachHeld`，**净删 `foldWindowKeys`**。
- **修掉 addr 一处真缺陷**：order 文件单行内原按写入**正向**交付，与冻结提交序契约（同一提交内后写的键更新）不符，被冻结锚 `journal_kv_test.go:96` 抓出 ⇒ 改倒序。**这是组件锚有牙的正例**。
- **正锚对拍** `TestJournalReadPathsAnswerFromTheAddress`：三根（停稳写视图／停稳**只读**视图／表头落后含 300KiB 大 value）× 四形状 × 三前缀，与同次开档整篇重放的 `l.index`/`l.order` **逐键逐字节逐序**比较。
- **四次变异证明全红→还原→绿**：①`stated()` 界改回 `valuesSize` ⇒ `states a span past the watermark 0 the reading view rejects, want the window's own end 409941`；②window 墓碑答持有 ⇒ corrupt；③null 答不持有 ⇒ `reads ("", held=false) … ("", held=true)`；④表侧墓碑答持有 ⇒ corrupt。还原后 `diff -q` 与原文件一致。
- **成本实测（闭合 §二十五·七 两项）**：`keys(prefix)` 绝对成本＝`capacity × stride`，随**持有键数**增长（512→98KB／1024→192KB／6400→1.5MB），**与历史无关**（100 键／2000 次重写仍 1024 槽／98KB，且断言钉 `capacity != journalSlotCapacity` 即红）。**只读视图在 3,519,920 字节日志上开档只读 64 字节（表头），窗口 0**。
- `go build` 0、`go vet`（含 tests）0、`./memory/ledger/` **0（128 RUN／60 顶层）**、S1 两锚＋新锚 ×10 **30 顶层＋70 子测 PASS**、`-race -count=3` **0 竞态（165s）**、两注释门 0／0、词汇门 0。症状锚仍红（`journalBytes=[0 136617 331331 565604 839122]`），**代理明确声明未转绿**。

**编排者裁定三项**：
1. **(A) 消费者锚 `TestStorageCapabilities` 5 子测红 ⇒ 编排者亲自修并验绿**。成因：`tests/integration/history/storage_capabilities_test.go` 的 5 处（:105/185/217/241/264）在全新 TempDir 上构造 `StorageConfig{Type: StorageJournal, Root: root}` **未声明 WriteRights** 随后提交，违 `config.go:64-70` 已文档化契约；S1 的写权门使其开档即 `no such file`。**修法＝5 处加 `WriteRights: true`**（提交面本就应声明写权），**零断言改动**。修后 `go test ./tests/integration/history/ -run '^TestStorageCapabilities$' -count=1` → **exit 0**。**否掉代理提的 B 案（撤回写权门）**——那会推翻 S1 读面与其两锚。:301 的独立重开只读故不加写权，:270/325 未在红清单内故不动。
2. **(B) 写视图「未陈述提交」答案收窄 ⇒ 裁定为契约正确，非缺陷，不加机制**。`append` 成功而 `sync` 失败时帧已落盘但 `confirm` 未跑，读面答「不持有」；旧 `l.index`（append 即更新）答「持有」。**`atomic_store.go:50` 契约明文**：「the view that committed publishes nothing from a commit it did not see return success: **its own read answers absence even beside a batch that landed whole**」⇒ **旧行为才是违约**，S2 的收窄是正确执行。**否掉代理提的「confirm 失败置 broken 并 rebuild」建议**——那会让视图交付其提交未返回成功的字节，直接违 :47。**须把该语义写进 `journal_kv.go` 契约注释**（归 S3 一并）。
3. **(C) `config.go:238` `openAddressIndex` 现为幂等空转**（addr 已在 face 开档 :721 打开，`l.addr` 永不为 nil）⇒ **死调用，S3 删除**（零残留）。代理因 config.go 在白名单外未删，申报正确。

**S2 自决全部接受**：`keys`/`namespaces` 签名加 error（addr 读面可失败，旧内存实现不可失败，调用点已适配）；三处测试改为 face 新拒绝点／新造根方式（**同一断言强度**，其中 `TestAddressableIndexRefusesALogThatLostTheBytesItsHeaderStates` 改断言 `openJournalLog` 返回同一 `ErrJournalCorrupt` 同一报文，非弱化）；`eachNewestFirst` 倒序修正。

**硬序**：**06.R8（窗口日志字节界）→ S3（停整篇重放＋删 l.index＋删 config.go:238 死调用＋写 :50 契约注释，原子）→ 06.R3 扩范围（store 常驻索引）→ 08.R4 → 05.R3-甲 → assembly 锚合批 → 10.R5–R8 → 09.R2/R3 → F1–F4**。

### R93 06.R8 收口：窗口日志字节已有界；四项自决全部接受，其中一项纠编排者派发令之误（2026-11-16）

**凭据**：新增 `stagedLog`（journal.go:1128），语义＝索引上次陈述水位以来日志增长的字节＝开档要重放的窗口字节。`a.staged` 全部 6 处读写点逐处枚举并说明新计数同步方式；日志字节唯一增量点在 `append`(:2308)→`note`(:1399)，而 `note` 全仓唯一调用方、`l.file.Write(frame)` 亦唯一处 ⇒ 每提交路径只经此一点；`derive`/`rebuild` 走 `state()` 不经 note，结尾 `flush(true)` 归零。
- **常量推导（实测，非取整数）**：下界＝索引预算自己留下的最宽窗口实测值（③checkpoint 49,608、①小键 41,500）⇒ 须 ≥49,608 否则日志支先触发会劈开这两类负载抬高屏障；上界压力＝boot 真付的就是窗口，引擎既有读单位是一个 chunk（`journalOrderChunkBytes = 64KiB`）⇒ 取 **`journalLogSyncBytes = 64 << 10`**。**机制上界严格**：跨越任一预算的那次提交会把自己的水位推过去，故提交边界上窗口恒 < budget，无「预算＋一帧」溢出。
- **三负载读数**：①小键小值 2000 提交，屏障 **24→24**、窗口 **41,500→41,500**、耗时 4.709→4.825ms＝**1.025×**（改前自身重复跑摆幅 5.3%，故落在噪声内）；②大 value 300KiB×360，屏障 **1→360**、窗口 **134,780,772→0**、耗时 6.180→15.216ms＝**2.46×**；③checkpoint 形状 400 提交，屏障 **33→33**、窗口 49,608 不变、**1.00×**。
- **⇒ ① 摊销保住**（屏障与窗口逐位不变）。**② 确实变贵 2.46×，是本征取舍**（任何 ≥ 预算的单次提交只能自付一道索引屏障），**不得据此宣称整体无回归**，须入 09.R2。
- **锚** `TestJournalWindowStaysWithinTheLogBudget` 三子例 ×10 → **10 顶层＋30 子测 PASS**。三条断言：每提交 `stagedLog == log.size − statedValuesSize`（计数完备性）；采样点用**生产入口 `OpenAtomicStore` 开只读视图**断言 `window.end − valuesSize <= journalLogSyncBytes`；两累积负载须至少采到一次非零窗口（防空转）。
- **两处变异证明**：删第二支 ⇒ `the window holds 409666 byte(s) after 1 commit(s), over the 65536 the log budget covers`（两次同因红）；删 note 计数增量 ⇒ 三子例全红 `the index files leave 490 byte(s) unstated after commit 0 and the log counter holds 0, want the same`。
- 五枚既有锚 ×10 → **50 顶层＋70 子测 PASS**；`./memory/ledger/` **0（61 顶层＋71 子＝132）**；`-race -count=3` **0 竞态（167.5s）**；两注释门 0／0；词汇门 0。症状锚仍红（`journalBytes=[0 136617 331339 565604 839106]`），**明确声明未转绿**。

**编排者裁定四项自决（全部接受）**：
1. **改既有锚夹具＝合法适应，非迁就回归**。`TestJournalReadPathsAnswerFromTheAddress/watermark_behind_the_log` 原用 300KiB 提交制造表头落后，新预算使该提交当场陈述自己的水位 ⇒ **夹具前提被冻结设计反转**。改法：窗口键集 {history,alpha,null}→{alpha,null}（窗口非空改由两个小提交提供），**四形状对拍／`stated(span)` 越水位／墓碑断言全部原样保留**，并**新增两条**（300KiB 提交不在窗口内＋经表侧读回整 307,200 字节）。同处收紧一个潜伏空转（原 `span.Offset < stated` 对 null 值在 stated==0 时恒假才通过）。**主题未变且更强 ⇒ 接受**。其拒用的替代（预算取 ≥300KiB 让锚一字不动）理由成立：`journalMaxFrameBytes` 是 32MiB，「按最大提交取值」推不出任何具体数，且 boot 读宽 8 倍——**那是改设计迁就夹具，方向反了**。
2. **`stagedLog` 在 rollback 不递减 ⇒ 接受，且编排者派发令括注「rollback 时正确回退」是错的，记自误**。被丢弃提交的**帧仍在日志里**，读面窗口 `S−W` 仍含它们；递减即在失败路径**低估窗口**。保住不变量 `stagedLog == log.size − statedValuesSize`，已入锚。
3. **③ checkpoint 形状取 36 键×56B（≈2KiB）源自 `journalMaxFrameBytes` 注释记载，非生产真实切片** ⇒ 接受但**登记缺口**：09.R2 须取真实切片样本复核下界 49,608。若真实切片远超 2KiB 则退化为大 value 形状，**仍由同一第二支界住**，故界成立、只有屏障道数预测可能变。
4. **② 子例窗口断言改后恒 0**，其牙由计数完备性断言＋变异证明承担，已标 `accumulating: false` ⇒ 接受（如实标注优于假装非空转）。

**纠 R91 一处归因**：R91 把 5.47MB 窗口归给「大 value／checkpoint 两种形状」，**实测只有大 value 成立**；checkpoint 是索引主导（每提交 5,035 索引字节 vs 4,134 日志字节），第二支从不触发。R91 该句按此勘误。

**S3 拆两单（防第五次撞限）**：**S3a**＝停整篇重放＋删 `l.index`/`l.order`/`l.tail` 及级联死代码＋删 `config.go:238` 死调用＋把 `atomic_store.go:50` 语义写进 `journal_kv.go` 契约注释（**原子**：停重放与删读者不可拆，S2 已使读者为零故此刻可原子）；**S3b**＝`openBytes` 新语义（`S−W`）＋症状锚 `:449`/`:485` 重述＋读数。**S3a 期间症状锚可能因判据未重述而红，属预期，须具名不得粉饰。**

### R94 编排优化：结论层过期是并发退化根因；次序表按触碰集重算，六单并发（2026-11-16，用户指出「并发又不够充分」）

**用户指出成立。编排者自查出两处结构缺陷**：

1. **①结论层与②次序表严重过期**：仍写「G1–G4 未通过」「13 文件未提交候选」「本轮白名单只有规划文件，**不改生产**」——而现实是退位已删、索引读侧已接线、R93 在台账、生产已改数十单。**编排者一直从台账尾部派发，绕过了结论层**，这违金字塔（结论层才是派发入口），也使「授权」节与实际派发矛盾。⇒ **已重写 ①结论（路径/最大风险/当前状态/核心承诺未兑现）与 ②次序＋授权**，作废旧「只改规划文件」条款。
2. **把「同包」当成了「必须串行」**：R28 的并发准入是**触碰集不相交（改写文件 ∪ 构建闭包 ∪ 验证读取目标）**，不是同包。编排者用「同包故串行」把 08.R4b／10.R5／A1／10.R8a／D2 全排在 S3a 之后，**而它们的改写集与 S3a 完全不相交**。且「写单只跑文本门、编译验证留后续独立验证单」的手法已在 V1/G1/G2/G3 四单并发验证过，**却没推广到剩余叶**。

**重算后可现在并发的五单（已派，与 S3a 共六单）**：
| 单 | 改写集 | 与 S3a 不相交的依据 | 验证策略 |
|---|---|---|---|
| **08.R4b** 外部折回脚本 | **新建** `scripts/fold-cold-root/**` | 新目录，零既有文件改写 | 文本门＋干跑零写快照；编译撞 ledger 中间态则具名报归属待复跑 |
| **10.R5** 主 spec 退位条款删除 | `openspec/specs/**`、各域 `specs/**`、`proposal.md` | 纯 openspec，零 `.go` | openspec strict＋spec_hygiene＋doc_links＋doc_budget（纯 Python） |
| **A1** 治理切面锚重述 | `assembly/governance_checkpoint_test.go` | S3a 只跑不改 assembly | comment_policy＋词汇门；编译待复跑 |
| **10.R8a** 端口语义归属推导 | `changes/10-*/design.md`（只读代码） | 只追加文档 | concept_budget 读数作输入；零编译 |
| **D2** 撕裂尾运维可见 | `scripts/recover-kv/main.go` | 与 S3a 写集不相交 | product_surface＋词汇门；**优先只消费 ledger 既有导出面，若须新增则停下报告不自行改 ledger** |

**真依赖故仍串行的四叶**：S3b（须 S3a 的 openBytes 读数）、06.R3扩（同触 `segment_store.go`）、08.R4a（同触 `journal.go` 切面族）、05.R3-甲（同触 `atomic_store.go`/`journal_kv.go`/`config.go`）。

**主动延后一单**：D1 关档竞态只读诊断（`TestAnchoredBootMatchesFullBoot` 的 `file already closed`）——**S3a 正在改 close 路径，此刻诊断不可靠**，待 S3a 落地再派。这是「负载/中间态敏感的诊断须待写面稳定」的应用，非流程债。

**拆单纪律**：08.R4 拆 a（删切面族，触 journal.go）／b（外部脚本，新目录）——**原先当作一叶故被整体串行，拆开后 b 立即可并发**。10.R8 拆 a（语义归属推导，只读）／b（实施，触 4 包）——**推导是实施的前置且只读，可提前并发**。这是 R28「同文件簇合单、其余各自成单」的正向应用：**合单判据是触碰集，不是任务编号**。

**教训回写**：①编排文档的结论层与四问须**每波重算**，过期即等于没有载体（R27 的反面）；②并发退化时先查「是否把同包当串行」与「是否忘了写单/验证单分离手法」，再查真依赖；③一叶若含「触共享面的部分」与「不相交的部分」，**拆开即可释放并发**，不要整叶串行。

### R95 S3a 撞限但核心已完（编排者亲验）；D2 查出 R91 消费者锚假绿＋读面一处设计缺陷（2026-11-16）

**S3a 第五次撞 150-turn**，末句「adapt the out-of-whitelist forced test file」。**编排者诊断现场：核心交付其实已完成**——`go build ./...` **exit 0**；`l.index`/`l.order`/`l.tail` 生产**零残留**；开档 `from` 已由 `journalStatedWatermark(dir)` 取表头水位（**编排者首眼只看到 `from := int64(0)` 初始化行而误判「停重放未做」，读下文条件赋值后自纠**）；`checkReplayed` 只剩 spans/version 校验职责；`openBytes = size - from` 新语义已在；`config.go` 死调用已删；`journal_kv.go:148-150` 契约注释已写。**剩余＝测试面适配（vet 才暴露）＋两处调试残留**。
- **编排者亲自清掉两处 `fmt.Fprintf(os.Stderr, "TRACE …")` 生产调试残留**（open 与 rebuild 各一）。D2 独立也查出同一残留 ⇒ 两路互证。**`check_product_surface` 的 DIAG 词表不含 TRACE 故门是绿的**——门有盲区，登记。

**D2 收口（部分完成，正确停下）**：
- **优先方案不成立**（grep 三条依据）：`tornTail` 无任何导出面；`IndexCutNotice` 语义是「切面不可放置故整篇读」非撕裂尾；且 **`cutNotice` 现已无赋值点**（S3a 删了它）⇒ `IndexCutNotice()` 恒 `""`、`assembly/run.go:617` 的 WARN 消费方**已成空转**。design:582 明列它归 S3 删除 ⇒ 归 06.R9 一并删。
- **冻结新导出面 `TornTailBytesOpen() (int64, bool)`**，**必须带 bool**：`complete` 可为 0（整条日志只有一个半帧）而字段零值也代表「整篇皆整帧」⇒ 单值会把「全篇是撕裂尾」误报成「完好」。现存锚只覆盖 `complete>0`，该歧义今天无人钉。**接受其推导**。
- D2 自决修本包 7 处红（`WriteRights: true`＋关闭 seed 档，与 R91/R92 同族，零断言改动）⇒ **接受**。修后 `./scripts/recover-kv/` `-count=1/3`、`-race` 全 0。

**⚠️ R91 消费者锚假绿（编排者记自误）**：R91 记「dry-run 前后快照 diff 空 ⇒ 消费者锚成立」，但 D2 实测该命令**实际 exit 1**、报 `the address index of this journal is unstated`，**工具根本没完成扫描**。⇒ **零写快照 ≠ 工具成功**；消费者锚必须同时断言「工具完成了它职责内的扫描」，不只断言副作用为零。**R91 该条凭据降级为「零写成立、扫描未成立」，须由 06.R9 重取。**

**⚠️ 读面设计缺陷（编排者裁定修法，06.R9 执行）**：读面遇「索引未陈述／旧格式／表头撕裂／索引文件不存在」即 `ErrJournalAddressUnstated` **拒绝整个开档**。**这颠倒了权威**：链是唯一真相、索引是派生物，派生物不可信时诚实的退化是**读链**（整篇重放＋具名通知），不是拒读真相。且 `recover-kv` 的职责恰是恢复有问题的根 ⇒ **它在自己的目标根上失效**（功能回归）。
- **冻结分界**：「派生物不可信」⇒ **退化读链＋具名通知＋零写盘不变**；「真相已损坏」（表头声明字节数 > 日志实长）⇒ **仍 `ErrJournalCorrupt` 拒绝**。退化成本（整篇重放）须具名如实报告，不得假装仍有界。
- **连带**：`/tmp/recovery-08r3/fixture-root` 的 `kv/slots` 是 v1（Version 已 bump 到 2），今日被读面按名拒。**后续消费者锚自带新造根**；该 fixture 仍可作 08.R4b 折回脚本与退化路径的输入，但须拷副本、且其表头对照期望值按 v2 重取。

**06.R9 已派**（读面退化＋撕裂尾导出面＋recover-kv 打印＋`cutNotice`/`IndexCutNotice`/run.go:617 死面删除＋S3a 遗留测试面适配）。**当前并发**：06.R9、08.R4b、10.R5、A1、10.R8a 五单（D2 已收）。

### R96 A1 收口：治理切面锚按冻结口径重述，三处变异证明有牙（2026-11-16）

**凭据**：`assembly/governance_checkpoint_test.go` 单文件 66+/37−。判据从「切面里装有两条记录」改为「**切面只载尾**（`Version=1`/`DenialSeq=2`/`Records=0`）**＋绑流后两条裁决同序可查**（含 `Subtype=audit`）」。生产接线仍由生产面独立证明（`StateRegistry().Names()` 含 governance、boot 成功、重启 `DenialSeq` 2→3 续号、流键 1/2/3 同序不重键）。`-count=1` 与 ×10 全绿，`^TestGovernance` 四锚全 PASS，`go vet ./assembly/` 0，两文本门 0。**未改回载历史**。
- **三处变异证明**：①切面 payload 注入 `records` 键 ⇒ `version 1 tail 2 over 2 records, want … beside no record the cut keeps`；②装饰生产流互换键 1/3 ⇒ `ruling 0 read beside the cut = {ToolName:exec}, want the audit record of send`；③删键 2 ⇒ `rulings … = 2, want the 3 the gate recorded`。均已回退（grep `mutant|withRecord|understated` 零命中）。
- **如实记录一处无效变异**：把 `denial_seq` 降到 1 仍绿——bind 会折进 cut 之后的尾事件，最终状态不变，**这是生产语义正确而非锚失牙**；故改用删键作缺失证明。**这是「变异须落在真可观测后果上」的正例。**
- **自决全部接受**：①为使「同序」可判，把原两次同判 `exec`（记录全同 ⇒ 乱序变异检不出、锚会哑）扩为 `exec`→`read`→`send` 三条可区分裁决，属**必要输入扩展非弱化**；②读回用「生产切面 payload＋生产 durable face＋与 boot 同一对 `RestoreState`/`Bind` 调用」，**如实声明这是生产输入＋生产调用的探针，不是第二个生产消费者**，生产接线断言不依赖它；③`Records` 保留为 `[]json.RawMessage` 作「切面不载历史」的承载体（生产者若重载历史即非零转红），内层死字段删除；④`governanceStateOf` 拆为取 payload／解码两半并内联仅剩一处的包装，同文件零断言改动。
- **生产码变异不可行已具名**：要「让切面重载历史」须改 `features/evolution/governance/checkpoint.go`、要「让 bind 乱序」须改 `denial.go`，二者在白名单外 ⇒ 变异改落在**生产产物/生产流**这一被观测输入上，等价观测面。**接受**（且 10.R8b 端口收口会改这两文件，届时可补生产码变异）。
- 未撞 ledger 并发中间态（`go test ./assembly/ -run '^$'` 编译通过）。

**当前并发**：06.R9、08.R4b、10.R5、10.R8a 四单（D2／A1 已收）。**待派**：S3b（须 06.R9 后取 openBytes 读数）、06.R3扩、08.R4a、05.R3-甲、D1 关档竞态诊断、10.R6/R7/R8b。

### R97 10.R5 收口：契约面退位残留清偿；四项待裁编排者已裁，六项残留登记（2026-11-16）

**凭据**：`fact-retirement` 能力整删（7R／31S／208 行）＋空目录；七个条款名 `grep -rF` 全 **0**；主 spec 2 文件＋域 delta 5 文件逐条改写；七个 `proposal.md` 加勘误并就地作废交付声明（**未取消任何已勾叶**，合 5.8）。`openspec validate --strict` **0**、`check-openspec.sh` **0**（158 passed／2 failed 皆 in-flight 外来变更，门不阻断）、`check_spec_hygiene` **0 errors**、`check_doc_links` **0**、词汇门 **0**。**存活判据一字未弱化**（身份读恒返回原字节／不得删改已提交事实／checkpoint 有界于开放项／判据不得以布局或代理量冒充），只去搬迁前提。
- **排除表成立**：压实搬迁（`migrate.go`/段压实，另一机制）、`回退位置式闭合`（字面撞「退」「位」）、`SummaryPlugin 退位为元数据标注`（岗位让位本义，GLOSSARY:14 判例）、cache-cold、superseded 历史备注本体（冻结只加勘误）、checkpoint **切面**（快照点，与索引 cut 同形异义）、六端口常量（仍是活分派键，179 处命中，与退位无关）。

**编排者裁定四项**：
1. **`fact-retirement` 用「删 delta 文件」而非 `## REMOVED Requirements` ⇒ 接受**。理由成立：该能力**从未进入 `openspec/specs/`**，REMOVED 需 Reason+Migration 且会指向不存在的要求，若该域日后提升为顶层 change 将在归档期失败。**不采用其 2 分钟替代方案**。
2. **06/09 `bounded-recovery` 索引条款逐字未动 ⇒ 接受**（派发令明禁写索引读侧新契约，最终形态未定）。其列入待补清单正确。
3. **08 TTL 条「正文不可解析→具名拒绝」与主 spec「Event JSON unparseable→skipped」冲突 ⇒ 裁定为 legacy 死条款，归 10.R4/R7 删除而非调和**。依据：代理实证 `Compactor`/`LifecycleManager`/`checkTTL` 在 Go 树**零定义**，且 `Lifecycle.Scan` 不解析正文 ⇒ 该条款无对象，属零残留应删面，不是两存活契约的真冲突。
4. **`08/storage-lifecycle-completion` 清理面宽于 06 design §D（只点 2 处）⇒ 接受**。另删的「搬迁＝引擎单一能力／冷层逐字节校验／热层移除」与「08.R2 甲乙丙」条件句，其唯一对象是已删机制或已作废规划分支，留下即成反向约束。

**六项残留登记（须后续叶清偿，不得只活在战报）**：
- **待补新契约**（索引读侧形态定后）：①`06/bounded-recovery`「开档与索引由存活键集加尾派生」全条——切面载体存在性与文件名（`journal.index` 写入侧零调用＝永不落盘）、开档＝「根＋尾」还是「切片＋尾」、两枚切面 Scenario 存废；②**「有界于存活键集加尾」的分母重定**——退位删后存活键集＝全部落链事实，该分母现等价于历史，须随 S3a/06.R3 改新口径；③09 四真量 `journalBytes`/`indexEntries` 判据分母同步；④06 design §D 已冻结的「切面→根＋尾」改写与两 Scenario 删除由 S3b 承接。
- **指针漂移**：`05:35`、`06:40`、`08:17,:50`、`09:36`、`11:61`、`12:46` 仍以 `session-checkpoint-material/spec.md:44|:47` 指「不得删改已提交事实」，该条现在 `:114`。**裁定：spec 引用一律改条款名锚，不用行号**（与变异锚行号漂移同病，已两次被击中）。归 10.R6。
- **`.go` 注释残留**（本单禁区）：`assembly/run.go:321`（bootContext 的 relocation 叙述）、`memory/ledger/segment_store.go:221,507`、`memory/ledger/kv_original_store.go:167`（"a relocation moved into the cold journal"）、`assembly/symptom_anchor_longrun_test.go:245`。归 10.R6/R7，**注释描述已删机制即伪陈述，须改写为真话**。
- **`golden/runtime/architecture.json:1468` 的 `RelocateReport` 绑定已腐化**（类型已删）⇒ 归 10.R7。
- **doc_budget 现 −6**（改前 −2，本单净减 4：主 spec −6、compaction-scheduler +2）⇒ `--tighten` 须与提交同批，**待用户提交授权**。
- **concept_budget 剩 2 ERROR**（`AttentionFactLog` S4 红线 marker／S1 3 方法）⇒ 归 10.R8b；`StorageConfig` S2 那条**已随 08.R3 删 `RetireFacts` 自解**（本单读数已不含它，确证 R81 第 1 条预判成立）。

**当前并发**：06.R9、08.R4b、10.R8a 三单（D2／A1／10.R5 已收）。

### R98 10.R8a 推导收口：落点新建 api/durable、端口取 3 方法、new_max 纠为 54（2026-11-16）

**只读推导，零 `.go` 改动，追加 10 域 design §十二（223 行，§一–§十一 一字未动）。编排者确认三项**：

1. **落点＝新建 `api/durable`（确认，在用户裁定「api/event 或新建 api 叶子」范围内）**。冻结 edges 给出三消费者 allow 三元交集＝`{api/event, api/plugin}`（architecture.json edges 474/670/689）。`api/event` **语义不匹配**：其 reason「事件身份与事实载体」、唯一接口 `Fact`（fact.go:18）只问答身份与冻结类型、无 `context`/`error`；装下存储行为端口须改宽该包 reason ⇒ **边界跟实现走，否决**。`api/plugin` 是三拓展点插件协议，排除。放宽 governance/approval 收 `runtime/state` 须改两条冻结边且违其「不拥有状态」reason，**比新叶更坏，否决**。新叶名沿耐久词轴（`DurableCommit`/`durable_truth`/`durable-delivery`），类型仍 `FactLog` → `durable.FactLog`；备选 `api/fact`（与 `Fact` 同名异义）／`api/factlog`（叠字）／`api/store`（与 `runtime/state.Store`、`atomic_store` 引擎义冲突）均否。**须登记 `packages`＋`edges`＋根 allow＋四方 allow，理由入 architecture.json**。
2. **端口取 3 方法 `Put/Scan/Sync`，删 `Get`（接受细化）**。`Get` **生产零调用方**：接触端口的九个生产文件逐文件 `\.Get\(` 计数全 **0**；生产只用 `Scan`（durable.go:691／commit.go:220／host_delivery.go:2165）＋五对 `Put`+`Sync`，点读走 `Run.CommittedEvent`(:592) 不经此口。`Get` 全部调用方＝两实现（`TierFacts.Get` tier_facts.go:35、`memoryFactLog.Get` commit.go:331）＋测试/样例 6 处。⇒ 端口该陈述消费者真正要求的；S1 债务 4→3；代价＝删两实现＋`journal_test.go:312,316` 两条断言改经 `kv.Read` 复述（**判据逐字保留含「空体也算持有」**）＋`skeleton_assert.go:228` 自证断言删除。
3. **`new_max` 纠为 54（G3 抬升超发 1）**：实测 `new` 55→**54**、`removed` 5→6、`relocations` 1→2、`bindings` 56→55 ⇒ `exported_interfaces_max` 85→**83**、`new_max` 56→**54**。**`exported_interfaces` 计声明数非名字数**（今日名字 83、声明 85，勿混）。净减逐名：`FactLog` 1→1（搬迁净 0）、`GovernanceEventLog` 1→0、`AttentionFactLog` 1→0。合 R85「按实际新增名重算」。

**冻结迁移序列（每步可编译，不得先删声明）**：①建叶＋登记（`exported_interfaces` 临时 **86**，登记为预期中间态）→②消费者改指（state 6／governance 12／approval 9／assembly 5 处）→③删三接口＋两自证断言＋两 `Get` 实现→④样例搬迁与删→⑤golden 与门→⑥`gen_godoc.sh`＋wiki。
- **S4 红线预算恰在极限**：`REDLINE_MUST_RE` 连 `// MUST NOT` 行也计一次，`FactLog` 现有 must 4＋must_not 2＝**6＝`REDLINE_LIMIT`** ⇒ **统一端口一行不得加**；治理/审批另六条是生产者与折叠义务，留各自 doc（`attention_log.go:1-15` 文件头改归属）。**须以「加一行即红」变异证明该双计口径**。
- 样例路径由 `example_path()` 机械算得 `examples/contract/api_durable/FactLog_example.go`（含 `func ExampleFactLog()`），`git mv`＋改 package/import/断言＋删 `Get`，同目录补 `doc.go`；`features_evolution_governance/` 删样例后只剩 `doc.go` ⇒ 整目录删；`runtime_task_approval/` 只删 `AttentionFactLog_example.go`。
- **spec 面零命中**（`grep GovernanceEventLog|FactLog|AttentionFactLog openspec/specs/` 无输出）⇒ 无以端口名命名的条款。**纠一处前人定性**：`session-checkpoint-material:107` 的「六端口」指 `api/plugin` 插件面端口，**与本案无关，MUST NOT 误改**（10.R5 已正确排除）。
- 波及面全表在 design §十二·5；另含门 `check_concept_budget.py:128-131,142`、生成物 `docs/api/**`（lint.sh:242 核新鲜度）、wiki `durable-delivery.md:57`。

**10.R8b 实施须待 06.R9**：其写面含 `memory/ledger/tier_facts.go` 与 `assembly/{commit,host,host_delivery,governance_checkpoint}.go`，与 06.R9 的 `journal.go`/`journal_kv.go`/`run.go` **同包**；且本叶要求**每步可编译**，不能只跑文本门 ⇒ **真依赖，待 06.R9 释放**。五项开口项须实施单实测（门计数复跑／`check_runtime_architecture --implementation` 读数／`gen_godoc --check` 差异范围／`journal_test` 改法实跑／红线双计变异证明）。

**当前并发**：06.R9、08.R4b 两单（D2／A1／10.R5／10.R8a 已收）。**06.R9 收口后可即派**：10.R8b、S3b、D1 关档竞态诊断（三者写集互不相交：10.R8b 触 tier_facts/assembly 生产、S3b 触 assembly 症状锚测试、D1 只读）。

### R99 08.R4b 收口：外部折回脚本交付，独立工具复核逐字节相等（2026-11-16）

**凭据**：新建 `scripts/fold-cold-root/{main.go(786行),main_test.go(745行)}`，**11 顶层测全 PASS**（3.569s）。**独立 python 帧解析器复核**（非本程序解析器）对新根对照 `fixture-meta.json`：`folded root key count: 49`／`recorded keys missing or differing: []`／`keys beyond the recorded table: []`／`index frame order equals recorded commit order: True`／`settled residue: []`／`blob bytes equal every recorded original digest: True of 6`／`seat: {"count":16,"commitSeq":16}`。干跑前后源根 **STAT_IDENTICAL／SHA_IDENTICAL**、`TARGET_NOT_CREATED`；`-restore` 后源根同样不变。flock 负锚 `errors.Is(err, ledger.ErrRootWriteLocked)` 成立且目标零键。
- **提交序恢复依据**：`{pid}:idx:{event}` 值第三段的 `segmentIndexCarried.Commit`（segment_store.go:237，`planEventLocked` 写 `commit = s.commitSeq+1` :563）；交叉校验 `settled:commit`（夹具 15）＝冷档最大提交，热档最新 16 由热档 idx 陈述。按 Commit 升序每事件一帧 ⇒ 新根 `EachIndexNewestFirst` 逆序恰为原序（实测 `settled-fold-3 … settled-0-0`），无空洞无重复，重复即 `ErrSourceConflict`。
- **消费者锚＝正式入口 `-restore` 自校验**：新开 `OpenTier` → `Segments.GetEvent`（逐事件原字节）＋`EachIndexNewestFirst`（提交序）＋`Originals.Get`（原文按身份）＋`EventCount()==16`。

**编排者接受其两项关键设计判断**：
1. **逐字节原样搬运而非经 `StoreEvent` 重放**——per-event `EventTime` **未持久化**（只在 `{pid}:meta:` 留 min/max 包络），重放会**编造 EventTime** 并重算 meta，破坏「按身份读回原字节」。改为每事件一帧提交 body＋idx＋meta 三键原字节、seat 单独一帧、原文键 256 一批（照 `migrateBatchKeys`）。**正是「数据原样透传拒绝篡改」**。代价＝依赖源键字节自洽，故加逐键字节校验＋独立工具复核。
2. **源根只读不可达 ⇒ 只复制 `journal.log` 到 scratch**：源根 v1 表头被读面拒开，而写面打开会**在源根内重建派生索引**（违零写）；冷档目录干脆无 slots/order.log。故把每个 log 复制到 scratch、在副本里以写权开档让引擎自行重建、再读全部键 ⇒ **源根一字节不动，v1/v2 源一律可折**。
   - **⚠️ 登记后续纯化项**：该 scratch 副本是**对「读面拒绝派生索引未陈述之根」这一缺陷的绕法**。**06.R9 正在修该缺陷（退化为读链）**⇒ 06.R9 落地后须重估：若读面能直接退化读源根，则 scratch 副本属多余特异化结构，**应删并改为直接读源**（零残留／更少特异化代码）。归 08.R4a 或后续纯化单，**不得留绕法当终态**。

**偏离接受**：任务行写「折冷档事实」，实折**两档全部事实**（49 键＝16evt＋16idx＋4meta＋12原文＋1seat，非冷档 45）——新根须能独立服务，热档剩余键（含 seat、热窗口 meta）必须同批进去。**接受，任务行措辞须按此勘误**。窗口 meta 冲突取热档陈述并在 `plan.Superseded` 点名（冷档是拷贝时刻快照），其余任何键两份字节不同一律拒绝（一身份一不可变事实）；未知键族拒绝不静默丢弃；源稳定性检查复用 `ErrMigrateSourceUnchanged`；`-source == -root` 拒绝（否则写权打开会先重建源根）。

**未声称的验收面（正确）**：任务行正锚 `go test ./memory/ledger/ -count=1` exit 0 **报告时点红**，2 项属 06.R9 在途（`root_stating_no_index` 报「the reading view replayed the whole log … want the fallback named」⇒ **退化已实现但具名通知未落**；`header_stating_bytes_the_log_lost` 报 truncate invalid argument ⇒ 夹具在改）。**原验收面不缩水**，脚本面三锚均已实测绿，待集成单复跑 ledger。

**升级说明落点（编排者裁定，归 10.R6）**：①主落点 `docs/OPERATION.md` §七「风险面（接手必读）」新增节，四条＝本构建只读 `kv/journal.log` 故冷档有事实的根须先跑折回（默认干跑）／源根只读且须离线（拷贝期间变动具名拒绝）／`file` 档每call只落一键故事实三键批答 `ErrBatchNotAtomic`、file 档根无可折事实按「无热档日志」具名拒绝／目标根须空且一根一写者（flock）。②索引落点 `docs/wiki/memory/memory-architecture.md` §六 kv 层「已退役布局」邻位加一句。**CHANGELOG 条目同归 10.R6／发版单**。

**当前并发**：06.R9 一单（D2／A1／10.R5／10.R8a／08.R4b 已收）。**06.R9 收口后即派三单**：10.R8b、S3b、D1；随后 08.R4a（删切面族＋重估 scratch 绕法）、06.R3扩、05.R3-甲、10.R6/R7。

### R100 06.R9 撞限但实施已完，编排者补一个测试助手即全绿（2026-11-16）

**第六次撞 150-turn**，末句「用重命名后的锚复跑两处变异证明」⇒ 停在验证阶段。**编排者诊断现场（R86 手法）**：`go build ./...` **0**；`TornTailBytesOpen() (int64,bool)` 已实现（journal_kv.go:145）并被 recover-kv 真实消费（main.go:264-268 类型断言）；`cutNotice`／`cutStatement`／`IndexCutNotice`／`ErrJournalAddressUnstated` 生产**零残留**（退化读链已取代拒绝）；无 TRACE 残留（`assembly/run.go:578` 的 `TAGENT_BOOT_TRACE` 是既有 env 门控诊断，非残留）。
- **vet 只卡一个未定义测试助手** `journal_test.go:1846: undefined: journalTestFlipLastByte`。**编排者按同族风格亲自补上**（损坏末字节使帧校验和不成立，配合「另一版本表头」触发退化整篇重放并须具名拒绝 `ErrJournalCorrupt`）。
- **补后亲验全绿**：`go vet ./memory/ledger/ ./assembly/ ./scripts/...` **0**；`go test ./memory/ledger/ ./scripts/recover-kv/ ./scripts/fold-cold-root/ -count=1` **全 ok**（47.0s／3.1s／5.3s）。
- **唯一未完成＝两处变异证明未复跑**（退化分支改回拒绝 ⇒ 正锚须红；真损坏拒绝改成退化 ⇒ 负锚须红）。**登记为待验证项，归 S3b 或独立验证单**；锚本身已在且绿，但**未经变异证明的锚不得当作有牙**（R29）。

**ledger 写面已释放 ⇒ 即派三单并发**：10.R8b（端口收口实施，触 tier_facts/assembly 生产/runtime/state/governance/approval/examples/golden）、S3b（症状锚重述＋openBytes 读数＋补 06.R9 变异证明，触 assembly 症状锚测试）、D1（关档竞态只读诊断）。三者写集互不相交。**随后**：08.R4a（删切面族＋重估 08.R4b 的 scratch 绕法）、06.R3扩（store 常驻索引去物化）、05.R3-甲（尾谓词接缝）、10.R6/R7。

**勘误（编排者自纠，派单前重算触碰集）**：上句「三单可即派」**不成立**。**S3b 与 D1 已派，10.R8b 扣住**——10.R8b 虽与 S3b 写集不相交，但 **S3b 正在做症状锚计时测量**，而 10.R8b 是跨 4 包六步重构，其中间态与编译负载会污染读数（R78④「负载还是回归」歧义陷阱）。**R21 触碰集含验证读取目标，计时敏感面相交即须串行**，这不是「同包误当串行」。10.R8b 待 S3b 收口后派。

### R101 D1 反驳「关档竞态」归因成立；夹具缺陷编排者亲修；另立一处真产品缺陷（2026-11-16）

**D1 只读诊断，仓内零写入，反驳编排者此前归因且成立**：
- **不是竞态**：`endRun(t, anchored)`（关 tier）在 `productsByRequest`（读体）之前、**同 goroutine 程序序确定**、`-count=10` **10/10 同一 key 红**⇒ 无并发窗口可测。
- **是夹具缺陷**：工作树未提交的 4 行在 `anchored_boot_parity_test.go:163/:170` 新增 `endRun`（HEAD 版无），而 `:225` 仍从已关视图读体。overlay 把读提到 endRun 之前 ⇒ ×1／×10 均通过；删两行 endRun 则 `another process holds write rights` ⇒ endRun 必须留，只能提读。
- **旧新错误文本是同一缺陷的两个面**：昔日 `file already closed`（直达 ReadAt 报 I/O），索引重构后在 `journalAddress.lookup` 的 `a.slots==nil` 处短路 ⇒ `ErrSegmentBodyMissing`「committed event body is missing」。**文本变了不等于缺陷变了**。
- **产品释放不变量未违**：`closeStorage` 仅在 `flying==0` 且 writers 已停时执行（invoke.go:122-143）；测试的 `endRun` 绕过 `Run.Close` 直接关 tier。

**编排者亲修并验**：把 `answeredOnChain := productsByRequest(t, ctx, anchored)` 移到 `endRun(t, anchored)` 之前（与 `:156` 先读后关一致），删后置重复声明。`go test ./assembly/ -run '^TestAnchoredBootMatchesFullBoot$' -count=10` → **exit 0（9.823s）**，声明 grep 计数 1。

**⚠️ 另立真产品缺陷（新叶，归 08.R4a 合批）**：`journalKV.Read/Keys/Namespaces` 在视图已关时**报 `ErrSegmentBodyMissing`「committed body is missing」而非具名「视图已关」**⇒ **真损坏与假缺体不可分**，运维与调用方无法判别。修法＝仿既有 `ErrCommitAfterClose`（config.go:161）对读路径加具名拒绝。**须建负锚**：关档视图读未缓存体 ⇒ 断言具名拒绝而非 body-missing。
- **待验推断（D1 如实标未实测）**：`CommittedEvent`（run.go:2293）只快照 `tier` 不挡 `closeStorage`，并发关档可能撞同一假缺体。**须实测确证或否证**，不得当已确证。

**教训回写**：①**错误文本在重构后变形不等于换了缺陷**，须追调用序而非凭文本归因（本会话已多次凭名/凭文本误判）；②「竞态」归因须先验确定性——`-count=10` 同因即非竞态，不必跑 `-race`；③夹具绕过产品释放路径（`endRun` 直关 tier 而非 `Run.Close`）时，其红不得归为产品缺陷。

**当前并发**：S3b 一单（D1 已收）。**S3b 收口后即派**：10.R8b（端口收口）、08.R4a（删切面族＋读路径具名拒绝＋重估 scratch 绕法）——两者写集相交于 ledger 与 assembly，**须串行或合单**，派前重算。

### R102 S3b 收口：journalBytes 面转绿（三层去物化第一层完成）；编排者自引门禁回归已修（2026-11-16）

**凭据**：症状锚 `journalBytes=[0 0 0 0 0]` 全 < 65,536 ⇒ **该面转绿**。界改绑 `journalLogSyncBytes`(64KiB) 并给推导：`flush` 同时看 `staged`(索引字节)与 `stagedLog`(日志字节)两预算，**任一被越过的那次提交把自己的水位推过去** ⇒ 提交边界上窗口恒 < 预算；`close` 走 `flush(true)` 推水位到日志末 ⇒ 停稳根窗口 0。**§二十五 冻结的 `journalSlotHeaderBytes + journalIndexSyncBytes` 已被 06.R8 实测反驳，未采用**；**原 2× 相对界在新语义下退化为 `2×0=0` 的空转界**，这是必须改绑的直接理由。
- **`:449` 重述**：原「第二代 journalBytes 非零」把正确实现永久判红 ⇒ 新增 `generationBoot.journalLogBytes`（每代 boot 后 `os.Stat` 日志长度），改判「第二代日志长度非零＋到末代增长」。**历史的存在由日志长度陈述，观察点换载体语义不减，且比原单向更强**。
- **另发现两处同族被取代判据一并重述**（`judgeBootFixture` 末代判据、`TestBootQuantityProbesReadTheRealBoot`）；probe 改**双向**（停稳根答 0／删掉 slots 的根答整篇日志 `59491==59491`）⇒ **收紧非弱化，接受**。
- **三处变异证明**（四次原文＋还原 shasum 一致 `0acdb625…`、`grep "mutant S3b"` 零命中）：A 退化→拒绝 ⇒ `table_of_another_version`/`order_file_shorter_than_the_header_states` 红；B 真相损坏→退化 ⇒ `header_stating_bytes_the_log_lost` 红；C 旧测量语义 `openBytes=size` ⇒ 症状锚 `331339 > 65536` 红＋probe `59491` 红。**06.R9 遗留的变异证明债已清偿**。
- **openBytes 三种根读数**（新常驻锚 `TestJournalOpenReadStatesTheWindowPastTheWatermark`）：`settled=0 behind=146 beside=98511 large=0 largeLog=7373988 bound=65536`。**7.37MB 日志的停稳根开档读 0 字节**——这是反转核心承诺第一次在生产读路径上实测成立。
- `go build` 0、`go vet` 0、`./memory/ledger/` **0（141 RUN／63 顶层＋62 子）**、词汇门 0。

**三道界现状（明确）**：`journalBytes` **已解**（S3b）；`indexEntries` **仍红 `94 > 86`** ⇒ 06.R3扩（store 常驻索引 `s.index`）；`durableLoadCount` `[0 12 24 36 48]` 第二代起越界 ⇒ 05.R3-甲。长跑锚 **3.28×**（末窗 324.2ms／首窗 98.8ms，界 1.3）仍红，其自身 `boot=[13.5…48.6ms]` 线性增长来自 store 常驻索引 ⇒ 同归 06.R3扩。**症状锚整体未转绿，代理明确声明只解 journalBytes 面。**

**⚠️ 编排者自引门禁回归已修**：R100 我补的 `journalTestFlipLastByte` 文档含禁词 **"no longer"** ⇒ comment_policy 棘轮 `REGRESSION audit-marker: baseline 0 -> 1`。S3b 查出并具名归属（非它引入），**编排者已改为 "does not hold" 并复跑确证**。**教训：编排者亲收剩余时同样受全门约束，不得因是自己写的就免检。**

**裁定 S3b 两项自决**：
1. **变异施加于生产 `journal.go` 而非派发令括注的 `journal_test.go` ⇒ 接受，派发令括注错**（两个分支都是生产代码，测试文件改不了它们）。已按备份→改→跑→按字节还原手法，终态零 diff。
2. **`journalWindowBoundBytes = 64<<10` 在 assembly 测试内重复声明 ⇒ 暂接受，但须补机器漂移守卫**。理由：`journalLogSyncBytes` 是 ledger 包内私有，导出它只为测试会增公共面。**风险**：若预算日后调大，assembly 侧硬编码界变松⇒**静默弱化无人抓**（调小则假红会被抓）。**裁定修法**：在 ledger 侧 `TestJournalWindowStaysWithinTheLogBudget` 内**以字面量 65536 钉住预算数值**，任何改动即令该锚红，强制同批更新 assembly 界。**归 08.R4a 一并交付**，不得只留注释承诺。

**下一步（派前重算触碰集）**：10.R8b（端口收口，触 `api/durable` 新叶＋`runtime/state`＋governance＋approval＋`assembly/{commit,host,host_delivery,governance_checkpoint}.go`＋`memory/ledger/tier_facts.go`＋examples＋golden）与 08.R4a（删切面族＋读路径具名拒绝＋漂移守卫＋重估 scratch 绕法，触 `memory/ledger/{journal,journal_kv}.go`＋`scripts/fold-cold-root`）**写集相交于 ledger 与 assembly ⇒ 串行**。**先派 06.R3扩**（store 常驻索引去物化，触 `segment_store.go`＋`assembly/read.go`）——它是症状锚另两道界的关键路径，且与 10.R8b 的 assembly 写集不相交（read.go vs commit/host/host_delivery/governance_checkpoint），**但同包构建闭包相交且 06.R3扩 须计时** ⇒ 仍须与 10.R8b 串行。**故次序：06.R3扩（计时，独占）→ 10.R8b ∥ 08.R4a 视写集重算 → 05.R3-甲 → 10.R6/R7 → 09.R2/R3 → F1–F4**。

### R103 10.R6 撞限；编排者亲收只读普查，剩余两项窄派（2026-11-16）

**第七次撞 150-turn**，末句「Now deliverable 4 — the `.go` comment residue survey (read-only)」。**编排者诊断（R86 手法）**：交付 1（退位叙述改写）与交付 2（升级说明三处落点）**已完成**，`check_doc_links` **0**、`check_vocabulary_alignment` **0**。**剩两项**：
- **A doc_budget 由 −6 变 +14**（`measured 22167 > baseline 22153`）：前单加升级说明净增约 20 行，把原低于基线推成超基线。棘轮只降不升 ⇒ **须同批压掉 ≥14 行冗余**，禁上调基线、禁 `--tighten`（那是低于基线时的锁步，且须与提交同批）。
- **B 行号引用仍 29 处**（`spec\.md:[0-9]`，排除 ORCHESTRATION）⇒ 交付 3 未完。

**交付 4 由编排者亲自完成（只读 grep，零编译负载）**：`.go` 注释描述已删机制的残留 **13 处命中，其中 5 处是真伪陈述**：
| 位置 | 伪陈述原文要点 | 归属 |
|---|---|---|
| `memory/ledger/segment_store.go:609` | 「A fact a relocation moved into the cold journal is still a committed fact…」 | 08.R4a |
| `memory/ledger/kv_original_store.go:167` | 「An original a relocation moved into the cold journal…」 | 08.R4a |
| `memory/ledger/config.go:170` | 「the cold journal of a root lives under that root, and the pass that writes it…」 | 08.R4a |
| `assembly/run.go:321` | 「bootContext … A relocation pass…」 | 08.R4a |
| `assembly/checkpoint.go:890` | 「tailFactKeys answers the keys … past its own index cut」 | 08.R4a（切面删后该判据须重述） |
其余 8 处是 `journal.go:759/852-887` 的切面函数本体与其错误文案 ⇒ **属 08.R4a 的删除面，非注释残留**。**全部路由 08.R4a，本单不改 .go。**

**10.R6b 已派**（纯文档、零编译，与 06.R3扩 计时不相交）：只做 A 压行数与 B 改条款名锚。**当前并发**：06.R3扩、10.R6b 两单。

**撞限规律回写（第七次）**：本会话七次撞限全部发生在「一单装多交付项＋全树验证」或「探索成本未预先枚举」。**有效对策已验证**：①编排者先诊断 build/vet/grep 取现场读数；②小而确定的剩余亲自收（本次＝只读普查）；③剩余量枚举成清单窄派（本次＝A/B 两项，各带实测起点数字 22167/29）。**无效对策**：原样重派、或把实施与全树验证装同一单。

### R104 10.R6b：B 项全完（29→1），A 项受阻；编排者否掉三案另裁两条既有纪律（2026-11-16）

**B 项全完**：行号引用 **29→1**（剩 1 处在 `10-…/specs/code-documentation/spec.md:94`，属各域 specs 禁写面，归后续持该写权的单）。逐处改为**条款名锚**并先读目标 spec 确认现行标题原文。`check_doc_links` 0、词汇门 0、`check_spec_hygiene` 0 errors。
**A 项未达标**：`measured 22164 > baseline 22153`（**+11**）。已无损压 3 行（OPERATION §F 末段三处重述并入首段 −2、CHANGELOG 第 4/5 条合并 −1）。**代理正确拒绝「为计数器合并内容」**（§F 四前置各是独立拒绝条件，合并即删拒绝语义；且合并后仍 +8）⇒ **接受其反驳**。

**编排者先自纠一处假设**：曾判「现状文档不得描述已删机制 ⇒ 应整段删除而非改写」。**grep 实证该假设不成立**：docs 内仅 13 处命中（排除 `.dev`），且 10.R6 **已正确改写为勘误/现状**（如 memory-architecture.md:50「原并列的『搬迁』已无载体：退位分层机制整体删除…」），剩余提及皆必要（GLOSSARY 词定义、升级说明、勘误本体）。**无可整段删除的已删机制散文。**

**否掉代理请裁的三案**：①合并 §F/CHANGELOG 求绿＝以可读性换读数且仍不绿，否；③doc_budget **无按名抬升协议**（其规则明文「禁止上调基线」，与 concept_ratchet 不同），故「走门自带抬升协议」不存在，否。

**另裁两条本就成立的纪律（10.R6c 执行）**：
1. **CHANGELOG 一批＝一条**：common.md §八.4「大版本直接 diff，**中间 tag 归并**」。本批是一个变更批次，应归并为**一条**带四要素（内容/影响范围/触发条件/自查方法）的记录，而非 5 条各带四要素。这是**既有口径的正确应用**，不是为计数器合并。
2. **一处一事实（代理的案 2，采纳其内核）**：§F 里的**机制细节**（提交序来源、具名错误语义、自校验链）属 `scripts/fold-cold-root` 自己的 doc 注释／`-help`，**OPERATION 只留运维动作**（跑什么命令、四个前置拒绝条件）。机制细节在工具与手册两处重述本就是冗余。
3. **不得合并 §F 四前置**（各是独立拒绝条件）。

**预期**：两条合计应达 −11 量级；若仍不足，**如实报剩余差额与已压清单，不得为绿而删契约**，编排者再裁。
**写权**：`docs/**`、`CHANGELOG.md`、`scripts/fold-cold-root/main.go`（**仅 doc 注释与 -help 文本，零行为改动**）。**禁编译**（06.R3扩 正在计时）⇒ 只跑 Python 文本门，编译验证留后续单。

### R105 06.R3扩 撞限但第二层已完成：indexEntries 归零，症状锚只剩最后一道界（2026-11-16）

**第八次撞 150-turn**，末句「replace the `indexEntries == 0` guard with a bidirectional probe」⇒ 停在锚重述阶段。**编排者诊断并取读数（R86 手法）**：`go build ./...` **0**、`go vet ./memory/ledger/ ./assembly/` **0**。

**症状锚实测（编排者亲跑）**：
```
journalBytes=[0 0 0 0 0]  indexEntries=[0 0 0 0 0]  durableLoadCount=[0 12 24 36 48]
wallClock=[19.2 15.4 20.8 23.3 26.7]ms  generationWallClock=[319 357 387 368 390]ms
durableAccount=[4 8 12 16 20]  journalLogBytes=[718 138367 334289 569762 844472]
```
唯一红：`:701 the boot of generation 3 scans 36 durable prefix keys beside the 12 of generation 1, over the bound 24: the task load scans the history instead of the account`。

**⇒ 三层去物化已完成两层**：①日志层（`l.index` 删、开档只重放窗口）✓ ②**store 常驻索引层（`s.index`/`s.order` 去物化）✓ indexEntries 归零** ③耐久任务账扫描层 ✗ ⇒ **05.R3-甲**。

**⚠️ 待收尾（撞限遗留，归 05.R3-甲 或独立验证单）**：`indexEntries` 现恒 0，故 `TestBootQuantityProbesReadTheRealBoot` 若仍单向断言「0」则**空转无牙**（全量物化的坏实现也答 0 时不会被抓——须双向：停稳根答 0／另造可区分根答非零）。代理正要做此重述时撞限。**须核该锚现状并补双向，否则第二层的绿未经鉴别力证明。**

**05.R3-甲 已派**（最后一层，R80 冻结签名）。**当前并发**：05.R3-甲、10.R6c 两单（写集不相交：前者 ledger 接口＋assembly 尾谓词，后者 docs/CHANGELOG/fold-cold-root 注释）。

### R106 10.R6c 反驳编排者估计成立；doc_budget 残差 +5 另裁两条减量来源（2026-11-16）

**两条裁定已正确执行**：CHANGELOG 本批 4 条→**1 条**（四要素齐、可检索关键字逐项命中：`scripts/fold-cold-root`×3／`ErrRootWriteLocked`／`ErrBatchNotAtomic`／`TornTailBytesOpen`／退位删除／只读视图不再截断），块 7→4 行；机制细节（提交序来源、六个具名拒绝、自校验链）迁入 `scripts/fold-cold-root/main.go` 包注释，OPERATION §F **四前置一条未删未并**，10→7 行。`doc_links` 0、词汇门 0、`gofmt` 干净。
- **⚠️ 编排者估计被反驳且成立**：我曾估「两条合计可压 ≥11 行」——**基于长度而非段落数**。本仓文档**一行一段**，行数＝段落数；压缩措辞不减行，机制迁移**不产生行数收益**。本批不可再减下限＝手册 §F 7 行（标题+空行+四条件+空行）＋CHANGELOG 4 行＝**11**，已从毛增 17 压到 11。**记自误。**
- **现 measured 22158 vs baseline 22153＝+5**。代理拒绝两候选（`docs/notes/trpc-fork-patch-assessment.md` 重复命令对 −2 属「本批外历史证据」；4 篇 wiki「延伸」页脚 −3 属各页导航删即缩水）。

**编排者再裁（10.R6d 执行）**：
1. **「重复不是证据」⇒ 否掉其第一条拒绝理由**：`docs/notes/` **不是** `openspec/changes/archive/`（冻结归档），且逐字重复的命令对删一处即信息无损 ⇒ **可压 −2**（须先确认确属逐字重复，语义有差异则不是重复）。
2. **现状文档不得描述已删机制 ⇒ 整段删除**：退位整体、引擎内存索引 `l.index`/`l.order`/`l.tail`、只读视图 `O_RDWR`/`Truncate` 行为均已删；枚举 `docs/**`（排除 `.dev`/`archive`）里把它们陈述为**现行行为**的段落并删。**已正确改写为勘误/现状的保留不动**（memory-architecture.md:50 勘误本体、:163 折回升级说明均不得删）。同形异义（压实搬迁／cache-cold／generation retirement／`Retention.Retire`／`headless-retired`／checkpoint 切面）存活不得误删。
3. **wiki「延伸」页脚不得删**（接受其第二条拒绝：各页自带导航，删即缩水）。
4. **若仍不足 5 行，如实报残差，不得为绿删契约**；残差在**提交授权停点**一并裁（`--tighten` 须与提交同批，且当前是超基线非低于基线，tighten 不适用）。

**当前并发**：05.R3-甲（症状锚最后一道界）、10.R6d（文档残差）。

### R107 10.R6d：−2 达成，残差 +2 登记待提交停点裁；三处混载段落就地改真话（2026-11-16）

**凭据**：重复命令对确属逐字重复（唯一差异是结果注解 `# → 4`，语义无分叉）⇒ 删附处两行、改首行为指向 §2.1，**删除点选索引不选清单本体**（本体可直接复跑）⇒ −2，信息无损。`doc_links` 0、词汇门 0、`git diff --numstat` 仅白名单 4 文件、零 `.go`/CHANGELOG/openspec。
- **三处已删机制陈述就地改写为真话（行数零变化）**：`memory-architecture.md:145`「才写进内存索引」→「分区集合」（`journalKV` 无 index）；`:147`「`FileSegmentStore.order` 记下落盘次序」→「引擎经 `CommitOrder` 逆着 `order.log`」（该结构体无 order 字段）；`platform-subsystems.md:107`「按窗口→序列位→身份定序」→「按落盘先后定序」（**与 `NewestOfType` 的 JSDoc 逐字相反**，后者明言不按 window/seq/partition 比高低）。
- **编排者接受其「不整段删」判断**：三处均为**混载段落**，同行还带存活契约（`ErrSegmentBarrier` 屏障原子性、`Events()` 序、checkpoint 两级恢复），删整段即删契约 ⇒ **只改写不删，正确**。「现状文档不得描述已删机制」的裁定在此落地为**改写为真话**而非删段，因段落承载多事实。
- **排除表成立**：勘误本体与折回前置、checkpoint 切面、压实搬迁、`Retention.Retire`、`headless-retired`、generation retirement、cache-cold、`lifecycle.go` 在册未接线（现状为真）、带 historical-verbatim 横幅的定位文档、`docs/api/**` 生成物。

**残差裁定**：measured **22156** vs baseline 22153＝**+3**。
1. **−1 该删且与预算无关**：`memory-architecture.md:257` 缺口行称「`EventRecord.WindowTS` 注释仍提压实层」，**实测该注释已是「时间窗」⇒ 该行本身是伪陈述**，按「文档随代码走脱节即修」须删。代理因「压实删除早于本批、出授权面」未动——**编排者裁定授权删除**（伪陈述不因批次归属而豁免）。归下一文档单或编排者亲收。
2. **剩余 +2 登记为残差，在提交授权停点一并裁**。**关键事实须呈用户**：HEAD `9547faca` 自身 measured **22274**（超基线 **+121**），工作树现 **22156**＝**比 HEAD 净减 118 行**。故 +2 是**基线相对 HEAD 已过期**的产物（本变更早先提交增 121 行未锁步），**不是本批新增膨胀**。**不得为绿删契约/导航/勘误**；`--tighten` 只降不升且当前超基线故不适用。

**当前并发**：05.R3-甲 一单（10.R6d 已收）。**doc_budget 残差 +2 与提交授权同停点**。

### R108 ★里程碑：三层去物化完成，症状锚转绿；05.R3-甲 反驳 R80 冻结设计成立（2026-11-16）

**编排者待复核的核心读数（代理亲跑）**：
```
journalBytes=[0 0 0 0 0]  indexEntries=[0 0 0 0 0]  durableLoadCount=[0 0 0 0 0]
wallClock=[20.5 20.0 15.7 22.4 21.4]ms  durableAccount=[4 8 12 16 20]
--- PASS: TestSymptomAnchorBootCostFlatAcrossGenerations
--- PASS: TestBootFixtureGrowsTasksAcrossGenerations
--- PASS: TestBootQuantityProbesReadTheRealBoot
```
**三道界全归零 ⇒ 三层去物化完成，boot 成本不随历史增长在生产读路径上成立。** 消费者锚 `TestBootSeatsTheMailboxAfterTheDurableAccount` 亦由红转绿。三枚冻结锚各 ×10 PASS；`go build` 0、`go vet`（含 runtime/tests）0、`./memory/ledger/` 0（140 RUN／62 顶层）、`-race -count=3` **0 竞态（215.5s）**、词汇门 0。

**⚠️ 编排者接受其对 R80 的反驳（四条依据逐条可核，R80 该段勘误）**：
1. **R80「9 个实现方」清单过期**：现树 `AtomicStore` 实有 **16 个**实现方（漏列 `factCommitEngine`/`faultyEngine`/`partFaultStore`/`retroFaultStore`/`settlePendingFault`/`persistenceFaultStore`/`failingCommitStore`，多在 assembly/tests 白名单外）。按字面加必选方法须改 6 个白名单外文件各加转发器。
2. **会造出重复契约**：树内**早已有** `commitOrder`（segment_store.go:239，未导出），其 `eachNewestFirstValue(ctx, func(key string, value []byte)(bool,error)) error` **与 R80 冻结签名逐字同构、实现者同为 `{journalKV}`** ⇒ 加必选方法即「同一契约被命名两次」，**正是用户裁 10.R8 要收口的反模式**。
3. **冻结三态第 2 态的词本身就是能力接口语言**（「引擎**不声明**序能力」），与树内既有原文一致；必选方法只能表达成「每次调用都失败」。
4. R80 反对的是**引擎特定类型断言**；`ledger.CommitOrder` 是 **ledger 声明的导出契约**，assembly 断言契约非引擎类型 ⇒ 不违「可换引擎」。
- **落地形态**：导出既有 `commitOrder` 为 `CommitOrder`、方法名 `EachCommittedNewestFirst`，**一份契约两个消费者**（`FileSegmentStore` 包内＋`assembly.tailFactKeys` 跨包）；`memKV`/`rustvikingKV` 不声明 ⇒ **原生第 2 态，零替身改动**。另加纯解码口 `IndexEntry`＋`CommittedIndexEntry(key,value)`（**拼写归 ledger、语义留 assembly**，合无前缀判据）。
- **第 3 态判在任务面而非集合长度**：切面自身提交的兄弟键使长度恒非零，故「空」须按任务面判。**代理如实登记「字面 `len(tail)==0→nil` 变异不红」**，未假装该变异有牙——正确。
- **三处变异证明**：①谓词失识别 ⇒ `durableLoadCount=[0 12 24 36 48]` 红；②第 3 态答 nil ⇒ 座位锚＋消费者锚红；③第 2 态答 empty ⇒ `answered 0 key(s), want nil` 红。还原均 `cmp` 字节一致。
- **R105 登记的 probe 空转风险已不存在**：`symptom_anchor_recovery_test.go:411-440` 已是双向（停稳根非 0 即红／另造 seatless 根答 0 即红），06.R3扩 撞限前已完成。**核实后不重复改，正确。**
- **越界两文件接受**：`segment_store.go`（删重复 `commitOrder` 定义所必需）、`assembly/task_custody_anchor_test.go`（S1 写权门必然后果，按 R92 裁定 A 同一先例补 `WriteRights: true`＋`tier.Close()`，零断言改动）。

**⚠️ 两处新红＋一项门禁债（须后续叶清偿）**：
1. **`storage_capabilities_test.go:258` 红**：`reopen over a damaged record = <nil>, want ErrJournalCorrupt`。机理＝S3 窗口化开档的**真后果**：`corruptLastRecord` 翻末帧 CRC，而表头水位已达日志末 ⇒ 窗口空 ⇒ 损坏帧从不被读 ⇒ 不再报损坏。**这是有界开档对「损坏检测面」的真实缩减，须裁定**：开档是否仍须核末帧完整性（O(1) 有界）以守住耐久边界处的损坏具名拒绝；水位已覆盖区域的深处损坏不再开档复核属索引读的本征性质，**须显式声明而非承诺检测**。归新叶（先核 spec 承诺再实施）。
2. **长跑锚 6.4×**（首窗 102.96ms／末窗 658.55ms，界 133.85ms）——**比 06.R3扩 时的 3.28× 更差**。**⇒ R82「剩余差额随 08.R3 删退位自解」的预判被实测否证，记勘误**。boot 三道界既已全 0，增长必在**每代自身工作**（候选：held keys 线性增长使 `keys(prefix)` 扫表增长、或每代 checkpoint 的全量枚举）。**须诊断，不得再以推断代替复跑**。归新叶，是抵达 DoD 的下一关键路径。
3. **comment_policy 6 beyond baseline**：`symptom_anchor_recovery_test.go:636`、`segment_store_test.go:445/474/484/732`（06.R3扩 写面）＋`scripts/fold-cold-root/main.go:1`（10.R6c 写面）。**05.R3-甲 自身 0 beyond baseline**。归后续单清偿，**不得抬基线**。

**05.R3 框仍未勾**（代理无 openspec 写权）⇒ **编排者记账：待上述 1/2 清偿＋独立验证单复跑后勾**。

**下一步序**：**长跑锚 6.4× 诊断（关键路径）∥ 损坏检测面裁定 ∥ comment_policy 6 条清偿** → 10.R8b 端口收口 → 08.R4a 删切面族＋读路径具名拒绝＋漂移守卫 → 09.R2/R3 → F1–F4。

### R109 ★长跑诊断否证「增长不在 boot」：三道界量物化而漏走查，boot 锚有盲区（2026-11-16）

**只读诊断，零仓内写入（`git status` 仍 194 项，探针只在 `/tmp/recovery-longrun/`＋`-overlay`）。**

**⚠️ 编排者前提被否证**：R108 判「boot 三道界既全 0，增长必在每代自身工作」——**错**。分阶段实测（252 代）：
| 阶段 | g≈5 | g≈251 | × |
|---|---|---|---|
| **boot** | 17.7ms | **377ms** | **21** |
| commits | 41.1 | 215.8 | 5.2 |
| settle | 13.9 | 11.9 | 0.86 |
| sessions | 5.1 | 15.2 | 3.0 |
| frame | 6.0 | 18.8 | 3.1 |
| closing | 14.9 | 19.2 | 1.3 |
总 646ms，**boot 占 57%**、commits 33%。**⇒ 三道界（journalBytes/indexEntries/durableLoadCount）量的是 load 物化，`seedAnchoredReplay` 的走查不在其中 ⇒ 三绿不等于 boot 有界。这是「锚只判断代理量」在更深一层复发。**

**CPU profile 定位（252 代，64s 样本）**：`seedAnchoredReplay` **26.73s＝41.8%**（99% 是 `EachIndexNewestFirst`）；底层 `journalAddress.value` **34.15s＝53.3%**（92.9% `pread`）；`run.go:2593-2599` 全量收 `refs` 无早停。`CommitFact` 8.36s 中 **7.59s＝90.8%** 是 `kvOriginalStore.load()`（kv_original_store.go:97-112，每进程首提交把全部 `blob:`/`ref:` 前缀读光并逐条 Unmarshal）。`madvise` 19.3%＋alloc 11.9s＝上述两处每代把历史分配一遍的 GC。

**三处可修（编排者采纳，派单实施）**：
- **A1** `segment_store.go:1157-1174`：每次 pread 读地址表**每个键**的值（evt/meta/seat/blob/ref），而 :1161 只认 `idx` 键，余者读完即弃 ⇒ **约 4/5 pread 浪费**。引擎已有只走键的 `eachCommittedNewestFirst`（journal_kv.go:254）未用于此路 ⇒ 改键走查＋仅对 `idx` 键点读。
- **A2** `run.go:2593` `refs` 全量收集 ⇒ 加走查下界（折叠产品/最新 cut 之后即停）。其自述 :2587 只承诺「折叠链不读被覆盖条目的 body」，**走查本身仍 O(历史)**。
- **B** `kv_original_store` 改按身份点读，前缀 load 只留给真正需全集者。

**一项须裁定（C，本征 vs 可修）**：checkpoint body ∝ 任务账（`tasksStateSlice`→`registry.SnapshotCheckpoint`），实测 64 代会话 cp **1400B→20427B**、框架 cp **4156B→101683B**。退位删后账无限增长 ⇒ 每刀写入无限增长。**但主 spec 有「checkpoint 大小有界于开放项」条款** ⇒ **须先核该 body 是否违反既有条款**（若它重述已结算任务而非只开放项，则是可修缺陷不是本征）。**不得先假定为本征而放宽界。**

**锚形状三问题（编排者裁定）**：
1. **boot 锚须补第四道界＝走查 visit 数**（当前会是 2753 而非 0）。**这是本单核心判别锚**：A1/A2 修好后该数须有界，回滚即红。
2. **longrun 锚把 boot 与 work 合成 total，两处 O(历史) 混在一个 1.3 界里** ⇒ **按阶段分界**，**不得放宽 1.3**。
3. **每代 settle 使账 +1，字节项若属本征须单列**，否则同一界无法同时容纳本征项与缺陷项。**待 C 裁定后再定**。

**噪声处置（采纳）**：本机抖动超 ±10% 判据（本轮参考窗 89.0ms／末窗 646.1ms，上轮 102.96／658.55）⇒ **结论只用 64 点单调序列＋profile 占比，不用单点**。另立独测：单进程连续 1400 次提交 barrier 中位 **4.1–4.9ms 全程平**（⇒ 屏障非增长项）。

**否证清单（诊断主动纠编排者三个候选假设）**：①「增长只在每代工作不在 boot」否证；②「checkpoint 全量枚举」否证（`checkpointLocked/SessionLocked` 无枚举，增长是 body 字节；`tailFactKeys` 停在最新框架 cp，日志 tail=5 恒定）；③「槽表 doubling 主导」否证（barrier 全程平，deep profile 中 `grow` 无样本）；④「held keys 线性⇒前缀扫描」**部分证实但位置错**——不在 `keys(prefix)`（seat 已陈述、indexEntries=0），而在 originals 的 blob/ref 整读。

**派单**：A1＋A2＋B＋boot 第四道界（走查 visit 数）＋longrun 按阶段分界。**C 先核 spec 条款再裁，不得先假定本征**。comment_policy 6 条与损坏检测面另单。

### R110 06.R10 撞限但三处实施已落；boot 阶段 21×→3.52×，残差窄派（2026-11-16）

**第九次撞 150-turn**，末句「a ledger-level test that pins B's teeth on the real commit path」⇒ B 的锚未完成。**编排者亲验现场**：`go build ./...` **0**、`go vet ./memory/ledger/ ./assembly/` **0**、`TestSymptomAnchorBootCostFlatAcrossGenerations` 与 `TestBootQuantityProbesReadTheRealBoot` **PASS**。
- **longrun 按阶段分界已落地**（前单完成该项），红现精确到 boot 阶段：`symptom_anchor_longrun_test.go:339` `the boot stage … generation 247 … costs 47.968584ms beside the 13.625167ms of the first 5, over the bound of 17.712717ms` ⇒ **3.52×**（界 1.3）。**对比 R109：total 6.4×、boot 阶段 21× ⇒ 大幅改善但未达标**。测试耗时 ~75s→**33.2s**。
- **⇒ A1/A2 已生效**（boot 阶段 21×→3.52×），**B 的锚未建故其牙未证**。

**06.R10b 已派**（窄单，三项按序）：①核 A1/A2/B 落地状态给新旧对照，不重做已完成者；②补 B 的 ledger 层锚（每提交读字节跨深度恒定）＋变异证明；③定位并修 boot 阶段残差 3.52×（候选：A2 早停是否真生效／**第四道界 visit 数是否已建，未建则建**／C 项 checkpoint body 是否违反「有界于开放项」条款）。**修到 ≤1.3，或具名报剩余属本征并给 spec 依据（此时按阶段单列本征项，不得放宽 1.3）**。同时清偿 comment_policy 白名单内 5 条既有红。

**当前并发**：06.R10b 一单。**待派**：损坏检测面裁定（`storage_capabilities_test.go:258`）、10.R8b 端口收口、08.R4a 删切面族＋读路径具名拒绝＋漂移守卫、10.R7 golden 同步、09.R2/R3、F1–F4。**doc_budget 残差 +2 与提交授权同停点**。

### R111 06.R10b 撞限但实施已落：boot 阶段 3.52×→1.92×；残差焦点收敛到 C 项（2026-11-16）

**第十次撞 150-turn**，末句「mutation 2: roll back the bounded walk (A2)」⇒ 停在变异证明阶段。**编排者亲验**：`go build` **0**、`go vet` **0**、boot 症状锚与 probe 锚 **PASS**、**三道界保持全 0**。唯一红：
```
symptom_anchor_longrun_test.go:416: the boot stage … costs 27.899833ms beside the 14.553625ms
of the first 5, over the bound of 18.919712ms  ⇒ 1.92×（界 1.3）
```
**boot 阶段收敛轨迹：21×（诊断）→ 3.52×（A1/A2）→ 1.92×（06.R10b）**。测试耗时 34.0s。

**残差焦点收敛到 C 项（此前两次延后，本单专攻）**：checkpoint body ∝ 任务账（64 代会话 cp 1400B→20427B、框架 cp 4156B→101683B），而**主 spec 明写「checkpoint 大小有界于开放项」** ⇒ 若 `SnapshotCheckpoint` 重述已结算任务即**违反既有 SHALL＝可修缺陷，不是本征**。**06.R11 已派，单一焦点**：读条款原文定「开放项」→ 读码核重述范围 → 判定可修/本征/残差在别处 → 若修则建「body 有界于开放项」锚＋变异证明。**明禁扩面顺手改其它增长项、明禁放宽 1.3**；若残差不在 checkpoint 须具名报归属并停下。

**撞限规律再确认（第十次）**：焦点单一＋剩余量预先枚举的窄单仍撞限，说明**单叶工作量本身超预算**（实施＋锚＋变异证明＋全量复跑）。**后续对策**：把「实施＋建锚」与「变异证明＋复跑读数」再拆两单；变异证明本身耗多次全量跑，是撞限主因之一。

**当前并发**：06.R11 一单。

### R112 ★06.R11 判定：checkpoint 重述已结项违反既有 SHALL＝可修缺陷；残差三阶段同一根因（2026-11-16）

**spec 条款原文**（`openspec/specs/session-checkpoint-material/spec.md:116`，Requirement「派生态有界且不删除已提交事实」）：「checkpoint 承载的派生态 SHALL 以开放项加水位表达，**已结部分 SHALL 留在链上而不嵌入 checkpoint**，checkpoint 大小 SHALL 有界于开放项数量」；Scenario :118-121「一个会话积累了大量已结交付或已答压缩判决…**大小 SHALL 不随已结数量线性增长**」。**开放项＝尚未结算的工作**（任务未到终态、义务未结清）；已结＝已到终态的任务记录、已结交付、已答压缩判决。**`delivery` 切片已按此水位压缩（O(开账数)），`tasks` 切片没有。**

**读码判定：违反条款，可修缺陷非本征**。`tasksStateSlice.SnapshotState`→`MemoryRegistry.SnapshotCheckpoint` 遍历 `r.tasks` **全表无终态过滤**；`checkpointOf` 逐字段：`Obligations`←`t.obligations`（**唯一真开放项**）；`Spec/Status/Revision/Round/四枚时间戳/Handoff/HandoffRecord`←已结任务终态与移动戳；`Mutations`←全部已提交变更（含 settle 链接本身）；`Consumed/Attempts/Rejected`←`StateSettleAccount` 已结回执账。
- **属因实测（同 durable 历史只改账户大小，`withTaskTerminalTTL(1ns)` 使账户=N vs 1，各 30 次 boot 中位）**：depth30 **Δ0.21ms**／60 **Δ1.21ms**／120 **Δ3.51ms**，单调随已结数增长；body **1,118B/已结任务**（40 代＝44,728B）；gen45 run 切片 **tasks 50,330B(80%)、compress 12,031B(19%)**，projection/inputs/mailbox 平。

**⚠️ 编排者前提被纠正**：R111 判「唯一红 boot」**不成立**——`judgeGenerationStages` **首失败即返回**，掩盖同 red 另两阶段：**sessions 5.09→15.00（2.95×，compress 增长）、frame 5.13→23.15（4.51×，tasks+compress 写入）**；commits 0.98、settling 1.01、closing 1.60。**三道走查计数面（visits/valueBytes/valueReads）比值均 1.00** ⇒ **A1/A2/B 确已生效，残差是同一根因：checkpoint 重述已结项**，同时影响读（boot）与写（sessions/frame）。boot 两次实测 11.24→25.22ms（2.24×）、14.92→24.33ms（1.63×）。

**修复形状（06.R11 给出，编排者采纳为冻结设计）**：checkpoint 载「**开放任务全文＋已结任务身份/修订水位**」，`runtime/state/durable.go` 的 `Seat` **按水位懒水合**，`restoreChains` 按水位跳过。
- **为何不能简单删**（06.R11 读码实证，两条 naïve 修法均坏）：①已结只留身份水位而 `Seat` 仍对每条走 `readTaskLocked`（**每任务 4 次前缀扫描**）⇒ boot 反而更慢；②完全不入账 ⇒ `d.facts` 丢任务，`durable.TaskIDs()`、恢复报告、`judgeLongRunFixture`（要求 tasks==每代 1 条）同时破。**故必须懒水合＋水位跳过，不是单纯瘦身。**
- **compress 切片同病**（19%）⇒ 同批按同一形状修，**不得只修 tasks 留 compress**（否则 frame/sessions 仍红）。
- **哲学一致性**：这正是「已结部分留在链上、派生态只载开放项加水位」＝**不内联正文、按需从链读**，与索引层「纯索引＋统一读」同构。

**06.R11 未修的理由成立**：桩在消费侧（`runtime/state/durable.go`、`assembly/task_checkpoint.go`、compress 切片），全在其白名单外 ⇒ **按令停下报告，正确**。**编排者扩白名单派 06.R12 实施**。

**另登记两项既有红（非本单引入，须归属清偿）**：`TestConsumptionFailureIsReportedByName`（前单新增未提交，单跑亦红）、`TestDurableCommitBarrier`（journal.log 缺失）。**comment_policy 剩 3 beyond baseline**：`symptom_anchor_longrun_test.go:375`、`symptom_anchor_recovery_test.go:681`、`scripts/fold-cold-root/main.go`。

**派单纪律更新（第十次撞限后）**：06.R12 **只做实施＋建锚**，**变异证明与全量复跑读数另派 06.R12v**——变异证明本身要多次全量跑，是撞限主因。

### R113 并发重算（用户第二次指出并发下降）：结论层刷新＋就绪队列按触碰集重列＋R35 流程债清偿（2026-11-16）

**用户指出成立。编排者自查**：06.R11 收口后只派了 06.R12 一单，并发降到 1。**根因不是无就绪叶，而是编排者未重算「零编译单」这一类**——06.R12 写 runtime/task＋runtime/state＋assembly 切片面，凡验证面须编译这些包的单确实真冲突，但**纯 JSON／只读／纯文本单完全不相交**，可并发。

**另一处真流程债**：**R35 独立拷问环自始未派**。技能明写「每个环节收口、每个修复批次后与归档前，有独立只读审者分轮拷问」，而本会话已修数十项、收了十余单，**全程无独立审者**⇒ 所有「已修/已绿」都是实施者自证或编排者复核，**违 R35（修复自证放行）**。**本轮补派第一轮。**

**已刷新 ①结论层**（取代 R93 时点旧述）：三层去物化已完成、boot 三道计数界全 0、走查计数面比值 1.00；**唯一未达标＝长跑锚三阶段**，根因统一判定为 checkpoint 重述已结项违反主 spec；门禁面逐项状态；**「反转核心承诺已部分兑现——boot 有界成立，每代总成本有界尚未成立，不得以 boot 三绿宣称完成」**。

**就绪队列已按触碰集重列**（见 ②次序节表）：可立即并发＝10.R7（golden 纯 JSON）、R35 独立拷问（只读）、09.R2 校准设计（只读＋design）、fold-cold-root 注释门 1 条（仅 gofmt）；真依赖待 06.R12＝06.R12v、08.R4a（写不相交但**06.R12 验证面编译 ledger ⇒ 反向污染**）、10.R8b（写相交 runtime/state/durable.go）、两枚既有红（写相交）。

**本轮已派**：10.R7（golden 腐化条目清除＋N14 核实＋DoD 变异**注册侧**，**明禁伪造 killed／抬 ceiling／提前改 10.R8b 未实施的端口条目**）、R35 第一轮独立拷问（八个拷问面：零残留／消费者真实性／锚有牙／注释真话／静默失败同类模式／门禁盲区／契约一致／重复契约；**明禁采信台账自述，台账与现树不符是最高优先级 finding**）。**当前并发＝06.R12＋10.R7＋R35 三单。**

**教训回写**：并发退化第三次发生，前两次根因是「结论层过期」与「同包误当串行」，**本次根因是「未把零编译单单独枚举成一类」**。⇒ 就绪队列须按**验证面负载类型**分栏（编译面／纯文本面／只读面），只读与纯文本面几乎总能与任何实施单并发。

### R114 10.R7 收口（1 项落地 2 项正确受阻）；查出一处真覆盖面损失与 N14 定性纠正（2026-11-16）

**落地**：`golden/runtime/architecture.json:1468` 的 **`RelocateReport` 概念绑定整块删除**（`grep -rn "\bRelocateReport\b" --include="*.go" .` → **0 行 exit 1** 实证）；`domain_surface.bindings` **56→55**，`not required` warning 消失。`durable_truth`／`plugin_mounting`／`assembly_symbols`／`schema_allowlist`／`spec_hygiene` 全 **0**，三份 JSON 合法。
- **S1/S2 BASELINE 无可清项**（`baseline_entries=78, exempted=77, stale=0`）；`runtime/state/durable.go:FactLog` 4 方法条目**正确保留**（端口今日仍在，归 10.R8b）。
- **其余登记复核均存活不动**：`assembly.generation-retirement`（`run.go:1639` `gen-retire:`，5 处 Go 命中）、`examples/contract` 24 目录 87 样例抽查 12 个接口声明均 1（无孤儿）、`spec_hygiene` 的 `Compactor`/`LifecycleManager` 对应仍存活主 spec 散文。

**⚠️ 编排者两处派发前提被反驳，成立**：
1. **DoD 变异登记表不在 golden**：`check_dod_coverage.py:42` `DEFAULT_MUTATION_REGISTRY="scripts/gates/check_event_integrity_mutations.py"`，`MUTATIONS` 在 :157，`MUT-DOD*` 仅 :843 一条（P4）。该文件属 `scripts/**` 禁区，**且工作树已有他人 798 行未提交改动**⇒ 并发即 R21/R22 互踩。**代理按纪律停下不越界，正确。**
2. **「concept 只剩 AttentionFactLog 两条」已不成立**：并发叶新增 4 个导出名（`CommitOrder`/`ChainSource`/`IndexEntry`/`StatedTask` 一带）⇒ 现 **10 error**，`new=58>56`、`exported_interfaces=87>85`。**须按名绑定＋抬升，归新单（不是 10.R8b，因这些名来自 05.R3-甲/06.R12 而非端口收口）。**

**⚠️ 真覆盖面损失（须清偿，不得追认缩水）**：**P1 的锚 `TestColdArchiveRefusesToOverwriteAnIdentity` 已随 `memory/ledger/cold_archive_test.go` 删除而消失**，但「身份覆写拒绝」是**存活判据**（`CommitFact` 的 `FactConflict` 五分类之一）⇒ **应重述到存活机制上，不是随机制删**。按 R29「验收面被追认缩水＝该叶重开」，**归 08.R4a 或 DoD 专单补锚**。另 P5/P6 按其 note 本就无锚、P0/P2/P3/P4/P7/P8 有在树锚但未注册无 run ⇒ **九个「已杀死」侧全未闭合**，须真跑 `--out` 产 run 产物，**严禁手填 killed**（P4 的 `not_killed` 自证判决代理未动，正确——删它只是把一条 error 换成另一条并毁掉唯一实测记录）。

**N14 `NewDurableCommit` 定性纠正（编排者采纳）**：任务行称「生产零调用、唯一命中是自身定义」**不准确**。实测：全仓 84 行命中，非测试仅 4 行（`durable.go:244/250/264` 文档+定义、`skeleton_assert.go:230` 编译期自证）；**生产消费者确 0**（生产走 `NewEmptyDurableCommit`，`run.go:617` 唯一调用点）；**但测试消费者 80 处/16 文件**，且 `durable_seat_test.go:67` 用它作「整份日志」参照系与 `NewEmptyDurableCommit`+`Seat` 对照（**其 negative-of-the-read 正是两构造器之差**）。CI 门与脚本零引用。
- ⇒ **不是单纯死代码**：它承载一个**对照参照系**角色。按零残留应删（生产零消费者），但删它须改 80 处并去 `skeleton_assert.go:230`，且**须先给「整份日志 vs 席位」对照找到替代表达**（否则失去该负锚的参照系）。**归独立叶，先设计对照替代再删，不得裸删。**

**当前并发**：06.R12、R35 第一轮两单（10.R7 已收）。**新增待派**：concept 新名按名绑定＋抬升（golden 面，零编译，**可即派**）；DoD 变异注册＋真跑产 run（触 `scripts/gates/check_event_integrity_mutations.py`，**须待该文件他人改动落地**）；P1 锚重述；N14 对照替代设计＋删除。

### R115 ★R35 第一轮独立拷问：16 finding（2 阻断）＋10 项驳回；编排者逐项裁定（2026-11-16）

**审者只读、零写入、未采信台账任何自述，逐条实跑或 grep 取证。frontier 非空。**

**阻断项**：
- **F1 台账①门禁数字与现树不符 ⇒ 成立，编排者自误，已修**（见 ① 更正段）。concept 实测 **10 error** 非 2；comment 棘轮 **1 beyond** 非 3；dod_coverage 未列。**欠账在 R108/06.R12 新增名（`CommitOrder`/`IndexEntry`/`ChainSource`），不在 10.R8b**——旧账把漏登名记到了错误的叶上。**G4 正在办理绑定＋按名抬升**；`StatedTask` 17 字段 > 基线 16 须**具名裁定**（`Settled` 是 R112 冻结设计所必需 ⇒ 按名抬升并记理由，或换表达）。
- **F2 DoD 谓词 P1/P4 仍绑定已删机制与已删测试 ⇒ 成立，计划缺口**。`golden/runtime/dod_predicates.json` P1 scope 写「a retirement pass over a cold journal…」、anchor 绑 `TestColdArchiveRefusesToOverwriteAnIdentity`（**全仓零命中**）；P4 scope 写「full boot including retirement/turn/settlement」、mutation `MUT-DOD-P4-cold-unbounded`。**10.R7 范围只含腐化绑定与变异登记表，未含谓词定义本身** ⇒ **新叶**：P1 改绑存活的「一身份一不可变事实」拒绝锚（`CommitFact` 的 `FactConflict`）、P4 scope 改写、`MUT-DOD-P4-cold-unbounded` 退役并在登记表说明。**不修则 F3 永不可达，除非复活用户明令删除的机制。**

**须修项（编排者裁定）**：
- **F3 切面族 ~200 行零调用死代码**（`journal.go:68-78` 常量／`:158-191` 类型／`:759-890` 四函数，全仓含 examples 与测试零调用；`journal_index_cut_test.go` 已只剩 58 行测水位行为）⇒ **08.R4a 是零行为变更纯删除**，按行段精确删。**且 P6「无死代码」谓词 `anchor.test` 为空 ⇒ 没有任何门在量「生产者无消费者」**，本 finding 即现成负例 ⇒ **P6 须接真锚**（归 DoD 新叶）。
- **F4 ⚠️ 06.R12 在飞形态仍 O(已结数)，且时间比界会掩盖 ⇒ 成立，编排者裁定走 (a) 真水位化**。`runtime/task/checkpoint.go:224-247` 仍对每条已结任务产 `StatedTask{TaskID,Status,Revision,Settled:true}`，`r.settled` 只在 `hydrateLocked` 单条删、**无整表裁剪** ⇒ 常数从 1,118B 压到 ~70–110B（约 15×）但**总量仍线性**，违 spec:120-121「大小 SHALL 不随已结数量线性增长」。**而 DoD 锚是相对时间界（1.3×），线性小项可被固定成本淹没 ⇒「界落代理量」在同一机制上第二次复发（R109 同形）。**
  - **裁定 (a)**：checkpoint 只载「框架 cut 字节＋开放项」，**已结身份由提交序按需读**——`tailFactKeys` 已是该形态，照它做。删 `Settled` 字段所破的 `TaskIDs()`/恢复报告/`judgeLongRunFixture` 改从链按需取身份。
  - **否 (b)**（判本征＋改 spec 声明残差）：spec 明写「已结部分 SHALL 留在链上而不嵌入 checkpoint」，载身份＋状态＋修订**就是嵌入**，不是本征。
  - **并须建结构锚**：每提交 body 字节 / 已结任务数**须平坦**，**不得只用 1.3× 时间比冒充结构有界**。**归 06.R12c（06.R12 收口后）**。
- **F5 fold-cold-root scratch 副本＝陈旧绕法 ⇒ 成立，确证 R99 登记**。读面已修（`openJournalReadLog` `O_RDONLY`、无索引时 `wholeLog` 重放到自己的 window），反事实：`WriteRights:false` 直读源根即不再需人工副本/writer open/重建索引，`Read(Fetch{})` 仍从 window 答全键；脚本既有 size/mtime 不变负锚正是凭据。**删 scratch 与 `copyLog`，改读面开档，把「源根零改写」写成显式断言**。**脚本本身保留**（外部数据搬运是用户允许的唯一形态）。注释 :251 的理由已为假须删。
- **F6 三处产品注释仍描述已删搬迁/冷日志 ⇒ 成立**（`segment_store.go:591`、`kv_original_store.go:214`、`config.go:170`；行号较 R103 已漂移）。**`config.go:170` 的 flock 获取序论证建立在已无写者的第二日志上，删该分句结论仍成立 ⇒ 纯负担**。归 08.R4a。
- **F7 spec SHALL 与运维文档承认的行为不一致 ⇒ 成立，且不一致处由本变更自己写进 spec**。spec「按身份读已提交事实 SHALL 恒返回原字节、SHALL NOT 答 missing」vs `docs/OPERATION.md:161`「带 cold/kv/journal.log 的根会缺一部分历史且缺得静默」。**裁定：把「根由带冷档的旧构建写出」作为 SHALL 的前置条件写进 spec**（契约变更，一句话，不改代码，合用户「数据搬运最多外部脚本」口径）。**不得反向把文档改成不承认（那是验收面缩水）**。归 10.R5 续单。
- **F8 `ConfigChange` 绑定被删 ⇒ 部分驳回**。删它是 **G3 按门自身 warning 执行的**（门原文「not required: the name is unchanged since the baseline (remove the entry or it rots)」），D1 只判**新增**名，既有名绑定非必需 ⇒ **删除合规，非静默漏清**。**但审者指出的门覆盖面缺口成立**：无「活导出类型必须有绑定」的反向门 ⇒ **登记为门缺口**，是否补门归后续裁（不属本变更范围则转告持有者）。
- **F9 棘轮 note 幻影槽位**（仍以 `RelocateReport` 为抬升理由而该类型已删）⇒ 成立，**随 G4 同批把 `new_max` 收到实测值并在 note 后段附勘误**（历史段不动，合 5.8）。

**登记项 F10–F16（不阻断，勿当阻断抢修）**：F10 症状锚 :528 注释行合并（**两 scope 与 gofmt 均不报＝门盲区**）；F11 :770 用 `walkVisitBound` 约束 `indexWalkValueBytes` 而 :759 的 `walkValueReadBound` 用于 :774（今值同 2 故等价，**配对错位日后改其一即静默漏另一面**）；F12 boot 三零界的牙在**另一测试**的 degraded/seatless 负例（跨测试依赖须写明或内联）；**F13 `assembly/task_checkpoint.go:57` ChainSource 断言未命中即静默跳过接线 ⇒ 账上有的任务答 `ErrTaskNotFound`**（今日唯一实现已满足故不可达，但失败形态坏，建议 fail-loud）；**F14 读面全量回放降级只被 recover-kv 消费，boot 自己不报 ⇒ 生产路径无运维信号**（建议 boot 记一行 INFO/WARN）；**F15 `segment_store.go:545-548` 索引条目值解不出 ⇒ 静默答 missing**（spec 只覆盖「正文读不出」，此情形落进降级形状，建议具名 `ErrIndexEntryCorrupt` 或写进 spec 排除项）；F16 `judgePerGenerationCount` 参考窗计数 0 时 fail-closed（今日正确，将来合法 0 会误红）。

**审者驳回的 10 项（编排者采纳，避免误修）**：退位残留未清（27 符号 prod/test 全 0）／新导出面无生产消费者（`CommitOrder` 等由 `assembly/checkpoint.go:906-914`→`tailFactKeys`→`seatDurableAccount`→`Assemble` 生产 boot 链消费）／`IndexCutNotice` 死产者（已不存在，现行 `IndexUnstatedNotice` 有消费者）／`eachCommittedNewestFirst` 重复端口（签名不同：键 vs 键+值，底层同一 `eachNewestFirst`，不满足逐字同构判据）／同形异义误判／`readJournalIndexCut` 别处被用（全仓零调用）／recover-kv「nothing written」仍伪（已修）／boot 三零界空转（牙在另一测试，今日不空转）／`no_prefix_criterion.json` 的 `coldIndexLocked` 条目（属外来变更范围，只转告不越界修）／`StatedTask` S2 超基线是缺陷（`Settled` 为冻结设计所必需，属流程项须具名裁定）。

**审者明确未查（不得当作已确认）**：**未复跑任何动态锚**（06.R12 在写 runtime/task、runtime/state、assembly，编译撞中间态且长跑读数会被负载污染）⇒ **台账「三道界全 0」「走查比值 1.00」「boot 1.63–2.24×／sessions 2.95×／frame 4.51×」未经独立确认**，须 06.R12v 或独立验证单复跑。未跑 `go build ./...`／`go vet ./...`／`lint.sh` 端到端；未审各域勾选真实性（只统计 48 勾/65 空，方向安全＝欠报非虚报）；未端到端跑 fold-cold-root。

**下一步序（R115 更新）**：**06.R12 收口 → 06.R12c（F4 真水位化＋结构锚）∥ 08.R4a（F3 纯删除＋F6 注释真话＋读路径具名拒绝＋漂移守卫）∥ DoD 新叶（F2 谓词改写＋P6 接锚＋变异注册真跑）∥ F5 scratch 删除 ∥ F7 spec 前置** → 10.R8b → 09.R2/R3 → **R35 第二轮（修复后再审）** → F1–F4。**F4 若用 1.3× 时间比宣布「checkpoint 有界」即成下一轮阻断项。**

### R116 G4 收口：concept 10→5 error；编排者裁定三条「待其自解」不得提前抬基线（2026-11-16）

**G4 凭据**：门实测 58 新名／55 已绑／3 未绑，逐名给生产引用 grep 计数后绑定——`CommitOrder`→port/**session-checkpoint-material**（Requirement「任务终态由 checkpoint 切片承载并有界恢复」）、`ChainSource`→port/**同 spec**（「派生态有界且不删除已提交事实」，原文「已结部分 SHALL 留在链上而不嵌入 checkpoint」）、`IndexEntry`→declaration/**event-segment-store**（「Store event as JSON Lines segment by time window」）。**归属跟随「要求它的能力」而非类型所在包**，同 `AtomicStore`→`partition-local-storage-access` 先例。`new_max` 56→**58**、`exported_interfaces_max` 85→**87**（实测值），note 追加逐名理由、R83 段一字未动、`removed_min` 不动。58 绑定逐名复核**全部有 `type <Name>` 声明**且与新名集**双射**（missing/new-only/bindings-only 均空）。**D1 三条＋两条 ratchet error 消失**；`durable_truth`/`plugin_mounting`/`assembly_symbols`/`schema_allowlist`/`spec_hygiene` 全 0。
- **反驳编排者候选清单成立**：`StatedTask` **早已绑定**（R83 前即存在），其门红是 **S2 字段棘轮**非 D1 未绑定。记自误。
- **登记 spec 滞后**：`event-segment-store` 该条只述索引条目如何归档，**未述代码现存的 Type/Session/Commit 三个携带字段** ⇒ spec 落后于代码，归 10.R5 续单补。

**⚠️ 编排者裁定：剩 5 条中三条「待其自解」，不得提前抬基线或加债务条目**：
| 残留 | 裁定 | 理由 |
|---|---|---|
| **S2 `StatedTask` 17>16**（新增字段 `Settled`） | **hold，待 06.R12c** | **F4 真水位化可能整体去掉「每已结任务一条 StatedTask」**，`Settled` 随之消失 ⇒ 现在抬 16→17 是 premature，抬了还要撤 |
| **S1 `AttentionFactLog` 3>2** | **不加 BASELINE 条目，待 10.R8b** | 10.R8b **删除该端口** ⇒ S1  finding 随之消失；R85 已裁「不得为待撤端口加条目」 |
| **S4 `AttentionFactLog` 缺 doc 红线** | **待 10.R8b** | 同上，端口删除即消失 |
| **S4 `CommitOrder` 缺样例＋doc 红线** | **须补，归 05.R3-甲 收口单** | 端口存活且是 R108 采纳的形态；样例路径 `examples/contract/memory_ledger/CommitOrder_example.go`（含 `func ExampleCommitOrder()`），doc 块需 ≥1 `// MUST`＋≥1 `// MUST NOT`＋「红线」marker |
| **S4 `ChainSource` 缺样例＋doc 红线** | **须补，待 06.R12/06.R12c 落地后** | 样例 `examples/contract/runtime_task/ChainSource_example.go`；`runtime/task` 正被并发改写，须待其落地（R21/R22）。**且 F4 真水位化可能改其形状 ⇒ 样例须待形状定后写** |

**⇒ concept_budget 的 5 条残留全部有明确归属与触发条件，无一须现在抬基线。** 这是「按名抬升」协议的正确用法：**抬升只针对确已存活且裁定的名，待撤/待变形者不抬**。

**当前并发**：06.R12 一单（G4／R35 第一轮／10.R7 已收）。**06.R12 收口后即派**：06.R12c（F4 真水位化＋结构锚）、08.R4a（F3/F6＋读路径具名拒绝＋漂移守卫）、DoD 新叶（F2/P6）、F5 scratch 删除、F7 spec 前置、S4 两样例（待形状定）。

### R117 06.R12 收口：两切片水位化（1118B→78B）；自陈验收面收窄＝R35 F4，另报一处真功能回归（2026-11-16）

**凭据**：tasks 切片已结项只载 id/status/revision（`spec`/`mutations`/`obligations`/三枚 receipt/时间戳/handoff 全空），**每已结项 ≤96B**；`omitzero` 标签是关键——**不加则零值序列化把一条水印撑到 637B，加后 78B**（实测）。`Seat` 水位入账 **`LoadStats().Scans` 为空**、`TaskIDs()` 点名已结任务、`Revision` 等于水位；懒水合 `Records(ctx,id)` 首次点名才读**该任务**五个前缀（断言每 scan 前缀含该 taskID），回读与全量日志**逐字节相等**；水位条目夹带 record 者 `ErrSeatRefused` 具名拒。compress 同形状（已答判决只写 ID，restore 时 `At=0` 不造时刻），三枚原红锚转 ok。`judgeGenerationStages` **全阶段判后合并，1.3 未放宽**。boot 三道界保持 `[0 0 0 0 0]`、`visits=[0 4 4 4 4]`。build 0、vet 0、注释门三树 **0 finding**（清偿四条长行含 :375/:681）、棘轮唯一 beyond＝`fold-cold-root/main.go:1`（禁改）、词汇门 0。
- **「已结」判据自定 `isTerminal && len(obligations)==0`**（义务未结清的终态任务仍全文入账，保住未确认终态通知的重放）⇒ **接受，判据正确**。
- **`SnapshotCheckpoint` 须把 `r.settled` 一并写出**（首版漏写会让身份集不跨代传递，已自纠）⇒ 接受。
- **白名单外延两文件具名接受**：`runtime/task/lifecycle.go`（水印字段＋`Lookup` 懒水合）、`runtime/task/restore.go`（`CheckpointRestored` 报水位，否则恢复报告计数回退）——属冻结形状的必然消费侧。

**⚠️ 自陈验收面收窄＝R35 F4，编排者裁定须继续修，不得追认**：每已结任务仍留 **78B 身份水位** ⇒ 总量仍 O(已结数)，**违 spec:120-121「大小 SHALL 不随已结数量线性增长」**。代理理由「boot 必须点名每个已结任务（`TaskIDs()`/恢复报告/`judgeLongRunFixture`），身份不可从水位推出」。
- **编排者裁定**：R35 F4 的 (a) 案已给出路——**已结身份由提交序按需读，`tailFactKeys` 已是该形态**。故「身份不可从水位推出」不成立：**身份不必从水位推，可从链按需读**。
- **须逐个核三个消费者的真实需求**：①`judgeLongRunFixture` 要求 tasks==每代 1 条是**测试夹具需求，按「测试不纳入衡量」不得驱动生产设计**；②`TaskIDs()` 与 ③恢复报告 **是否真在 boot 关键路径上 eager 需要全部已结身份**？若否则改按需读，checkpoint 只载 cut 水位＋开放项。
- **若确证某生产消费者 eager 需要全部身份** ⇒ 那是**真本征**，须改 spec 显式声明残差（照「热层字节增长 SHALL 被显式声明」同款句式）**并建结构锚**（body 字节/已结任务数须平坦），**不得只用 1.3× 时间比冒充结构有界**。**归 06.R12c。**

**⚠️ 另一处真功能回归（代理自报，须修）**：restart 后 **`Resume/Cancel/Advance` 对水位任务答 `ErrTaskNotFound`**（只有 `Lookup` 触发水合）⇒ **账上有的任务对这些入口答不存在**，与 R35 F13 同形（静默/具名不存在掩盖未水合）。**修法：这些入口也过 `hydrateLocked`**。**归 06.R12c，属正确性优先于成本。**

**新红四项（代理推断归前单，未证——无干净 worktree 授权）**：`TestAnchoredBootMatchesFullBoot`（`Close`→`closeStorage` 关 tier 后测试再读 index，关闭句柄 `Stat` 返 EINVAL）、`TestDeliveryDurableReadErrorSurfaced`（journal.log 缺失）、`TestTierParent*`（GetParent 派生）、`TestRestartRecoveryPinsBeforeTrim`（early-trim 控制）。**须独立验证单在干净 worktree 对照定归属，不得沿用推断**（本会话已多次因凭推断归因而误判）。另两枚既红（`TestConsumptionFailureIsReportedByName`／`TestDurableCommitBarrier`）仍未修。

**未做项**：compress 切片独立 body-bound 锚（唯一可放处 `assembly/compress_checkpoint_test.go` 不在白名单）；新锚 ×10 与变异证明（划 06.R12v）。

**当前并发**：D2（DoD 谓词）一单。**即派 06.R12c**（F4 身份按需读裁定＋三消费者核实＋结构锚＋Resume/Cancel/Advance 水合回归）。**随后**：06.R12v（变异证明＋全量复跑＋新红归属干净 worktree 对照）、08.R4a、F5、F7、10.R8b、S4 两样例、09.R2/R3、R35 第二轮、F1–F4。

### R118 D2 收口：P1 改绑存活锚（阻断项 F2 部分解）；P6 提议新机器门补「死代码无门在量」缺口（2026-11-16）

**P1 已改绑并实证有牙**：新 scope「a commit that offers other bytes under an identity the chain already holds, judged beside the fact it holds, the keys it keeps and the sequence slot it took」；anchor＝**`TestFactCommitAnswersNewExistingConflictUncertain`**（`memory/ledger/fact_commit_batch_test.go:199`，全仓唯一）。**断言原文实证真测该判据**：`factProbeFact([]byte("another content under the one identity"))` 固定 `EventKey`/`Ref.Key` 只变字节 ⇒ 断言 `errors.Is(err, ErrContentConflict) && outcome == FactConflict`＋键集不变＋`EventCount()==1`。**非 happy path**。`measure_token` 由已删的 `ErrMigrateMismatch` 换绑存活的 **`ErrContentConflict`**（同名物非新造）。
- **P4** scope 去退位，改为「开档只重放索引所述水位到日志末端的窗口＋不发布常驻历史索引＋durable load 只为已座位任务付费＋该代 turn/结算/checkpoint」；失效变异 `MUT-DOD-P4-cold-unbounded` 标 **`retire_pending`**（替换目标 `cut.Offset < 0 || cut.Size > size` 全仓零命中 ⇒ 变异不可施用）。**登记表条目退役归持 `scripts/gates/check_event_integrity_mutations.py` 写权者**。
- **门读数 21→20 gap**，`P1: the anchor test … does not exist` **已消失**。**编排者派发令一处前提错**：`check_dod_coverage.py` **无 `--json` 参数**（argparse exit 2），记自误。
- **取证方式如实**：`golden/runtime/dod_predicates.json` 在工作树**未跟踪（`??`）**⇒ `git diff` 不可见，改以 Edit＋JSON 加载＋grep 锚存在取证。**不得因 diff 空而判未落盘。**

**⚠️ P6 缺口＝真门盲区，编排者采纳其提议为新叶**：P6「无死代码」谓词 `anchor.test` **为空** ⇒ **没有任何门在量「生产者无消费者」**。R35 F3 的切面族 ~200 行（`journal.go:68-78/158-191/759-890`，四个函数引用数 2/2/2/3 **全在 journal.go 自身**）正因**死代码可编译故 build/vet 盲**而逃逸。
- **提议**：新门 `scripts/gates/check_dead_producers.py` 对 converged 包跑 `deadcode -test ./...`＋golden 白名单；measure `deadProducerCalls` 与 deadcode 口径一致（函数级）；若须覆盖未用类型/常量则用 staticcheck U1000。**删除前红（三个不可达根）、F3 删 :759-890 后转绿**——现成负例。
- **归属**：需 `scripts/gates/**`＋`go.mod`（新工具依赖）＋CI job ⇒ **独立叶，且新工具依赖须先核项目「优先用成熟库而非手写」政策与 vendor 纪律**。

**残留（须后续叶，不得假装闭合）**：未登记变异 **8 条**（P0-P3、P5-P8）＋无 run 记录 **9 条**（P0-P8）⇒ 变异注册真跑叶（触 scripts，**须待该文件他人 798 行改动落地**）；**P5/P6 无锚**；`mutation_verdicts[0]`（P4 not_killed）随该变异失效须同批退役；**P1 `criterion` 文本仍含「退位搬迁非删除」**（design.md:16 已无此句，同类腐化，门只看标题故今绿）⇒ 归 DoD 叶同步。
- **审者建议采纳登记**：`assembly/fact_commit_atomic_test.go:252 TestFactCommitJudgesTheDomainBeforeTheEngineBytes` 更强地判「已提交字节不动（含重开根）」，**P1 若要覆盖「SHALL NOT 改」面可并列绑定**。

**当前并发**：06.R12c 一单（D2 已收）。

### R119 F7 收口：spec 冷档前置已加且未弱化存活判据；doc_budget 残差升至 +8 须提交停点裁（2026-11-16）

**F7 凭据**：`session-checkpoint-material/spec.md:116` Requirement 加前置——「**在本构建写出的根上**，按身份读一条已提交事实 SHALL 恒返回其原字节与原索引位置，SHALL NOT 答 missing；恢复期读全史的消费者 SHALL 仍能按身份读到其正文。根下的 `cold/kv/journal.log` 由旧构建写出，其冷档事实不在本构建读面内：该形态 SHALL 先经 `scripts/fold-cold-root` 折回新根再供本构建服务，折回前按身份读冷档事实答不存在，**该退化是已声明的运维前置，不构成本要求的违反**」。Scenario :125 WHEN 加同一前置；新增 Scenario :129-133「旧冷档根未折回时按身份读答不存在」＋折回命令。
- **存活判据未弱化**：「恒可读」对本构建根仍**无条件**，前置只排除须运维折回的旧形态。**未反向改 OPERATION.md 去不承认**（那是验收面缩水，明禁）。
- **命令映射实证**：`go run ./scripts/fold-cold-root -source <旧根> -root <新根>`（默认干跑）＋`-restore`（写后逐身份回读），旗标经 `main.go:200-203` 与 `docs/OPERATION.md:248` **双向核实一致**。
- `openspec validate --strict` 0、`check-openspec.sh` 0、`spec_hygiene` 0（279/286 未变）、`doc_links` 0、词汇门 0。
- **同类绝对承诺枚举**：**`delivery-closure/spec.md:9`「按交付身份前缀可回读全部状态（恒可回读）」同病 ⇒ 须加同一前置，归后续单**；`compaction-scheduler:5` 历史快照横幅冻结不改只登记；`session-fact-lifecycle:11`、`event-key-visibility:31/48` 判不受影响。
- **P1 criterion 腐化确认**：`dod_predicates.json:28` 仍含「退位搬迁非删除」，`check_dod_coverage.py:521` **只判 title 子串**故今绿 ⇒ 归 DoD 叶同步。

**⚠️ doc_budget 残差 +2 → +8（须提交停点裁，编排者不自行放宽）**：F7 加 6 行契约内容 ⇒ measured **22161** vs baseline 22153。
- **关键事实呈用户**：**HEAD `9547faca` 自身 measured 22274（超基线 +121）**，工作树现 22161＝**比 HEAD 净减 113 行**。故 +8 是**基线相对 HEAD 已过期**的产物（本变更早先提交增 121 行未锁步），**不是本批新增膨胀**；本批每一行都是必要契约（升级说明四前置、spec 冷档前置、CHANGELOG 四要素）。
- **门规则**：只降不升、**禁上调基线**、无按名抬升协议；`--tighten` 只降不升且当前超基线故不适用。
- **编排者裁定**：**不为绿删契约/导航/勘误**（前几单已三次拒绝以可读性换读数，均采纳）。**残差 +8 与提交授权同停点，须用户裁**：①授权再压 8 行无损冗余（编排者已枚举尽无损空间，前单报「不可再减下限」）；②或裁一次性基线对账（承认早先提交未锁步的 +121，把基线校准到实测并记理由）——**②属放宽门须用户明示，编排者不自行采用**。**登记为提交停点的待裁项，不假装已闭合。**

**当前并发**：06.R12c 一单（F7／D2 已收）。**待派**：06.R12v、08.R4a、F5、10.R8b、S4 两样例、delivery-closure 同前置、DoD 变异注册真跑＋P1 criterion＋P5/P6 锚、新 dead-producers 门、09.R2/R3、R35 第二轮、F1–F4。

### R120 06.R12c 反驳 (a) 成立，裁 (b) 表示下界＋结构锚；spec 残差声明属弱化 SHALL 须呈用户（2026-11-16）

**三入口水合已修且语义正确**：`Advance`/`Cancel`/`Resume` 经 `lifecycle.go:305`／`cancel.go:47`／`lifecycle.go:352` 的 `hydrateLocked` 取记录。**读码确认各入口对已终态任务的正确语义不同**：`Advance`→`ErrTerminalState`（:319）、`Cancel`→`ErrTerminalState`、`Resume` 对 `Cancelled`→`ErrResumeCancelled`、对 `Settled/Failed` 按既有语义**重入为新轮**（switch 只拦 Running/Cancelled）⇒ **不得统一改成拒绝**。正锚 `TestRestartEntryPointsReadAWatermarkedTaskFromItsChain` EXIT=0，负锚同测内 `stranger` 三入口仍答 `ErrTaskNotFound`（项级具名）。

**⚠️ 编排者 (a) 裁定被反驳，采纳 (b)**：代理同意「身份不可从水位推」的理由不成立（身份确可从链取），但给出更硬的依据——**boot 跳过判据 `coverage[taskID]`＋`durable.Revision` 需每已结任务 {身份,修订}**；从链按需取身份+修订+终态**须读每条 fact body**（index walk 值读 ∝ 历史字节），**严格劣于 78B/项**，会打穿 boot 平坦界。且 (a) 的取数改造须动 `run.go`/`read.go`/`commit.go`（均禁改）。⇒ **残差是表示下界，非实现懒惰**。**编排者采纳**。
- **三消费者核实结论**：①`judgeLongRunFixture` 定义/调用唯二在 `symptom_anchor_longrun_test.go:130/386`，**生产零调用 ⇒ 测试夹具需求不驱动设计**（编排者该点裁定成立）；②`TaskIDs()` 生产调用 5 处（`run.go:1107`、`checkpoint.go:877` 走 `DurableCommit.facts`；`read.go:460`、`host_attention.go:123`、`invoke.go:688` 走 `runCommit.held`），**boot 关键路径 `restoreChains` 确需每已结任务 {身份,修订}**；③恢复报告 `CheckpointRestored`（restore.go:186-208）→`run.go:1155 remember`→`read.go:460`→`memory/query/inventory.go:583` 与 `host_diagnostics.go:341`（DiagnosticRecovery.Status）。
- **结构锚已建（非时间比，满足 R115 F4 要求）**：`TestSettledWatermarkCostsTheSameAtEveryDepth` EXIT=0——**4/8/16 三深度边际字节相等且 ≤96，deep−shallow 恰为 1×12**。compress 侧 `TestCompressionSliceBodyIsBoundedByTheBatchItOwes` EXIT=0（已答项边际字节三深度相等、≤两枚身份字节；负锚：未答项因叠 reason/at 显著更贵）。
- **boot 三道界保持全 0**：`journalBytes/indexEntries/durableLoadCount=[0 0 0 0 0]`、`durableAccount=[4 8 12 16 20]`、`indexWalkValueBytes=[0 252 252 260 260]`。build 0、vet 0、注释门产品面 **0 finding**、棘轮唯一 beyond＝fold-cold-root（禁改）、词汇门 0。`runtime/task`+`runtime/state` 仅两枚既红。

**⚠️ 须呈用户裁的契约弱化（编排者不自行改 spec）**：(b) 意味着 **checkpoint 随已结任务总数线性增长（78B/项），永不再有界**。这与 spec 现写「大小 SHALL 有界于开放项数量」「SHALL 不随已结数量线性增长」**正面冲突**。代理建议的残差声明原文：
> checkpoint 的已结派生态 SHALL 以每已结项一枚恒定字节的身份水位表达（身份、终态、链修订），SHALL NOT 携带记录正文或回执；每已结项字节 SHALL 在各历史深度恒定，SHALL NOT 随深度增长；身份水位是 boot 跳过判据与恢复报告身份的来源，**MUST NOT 被声称为本要求已达成的有界**。
- **编排者立场**：该声明诚实（照「热层字节增长 SHALL 被显式声明」同款既有句式），且结构锚钉住了「每项恒定字节」这一可判据。**但它把一条绝对 SHALL 降为显式声明的残差，属验收面变更，须用户明示**。**不自行改 spec、不假装已闭合。**
- **须同时呈用户的替代思路（编排者提出，未取证）**：已结身份在**键**里、修订或可由提交序推 ⇒ 若只须身份+修订而**不需终态正文**，则走 order 文件**键走查**（不读值）可能既保 checkpoint 小又不打穿 boot 界（A1 已把该路改成键走查）。**代理的 (b) 论证假设了「须读 fact body」，该假设是否对「只取身份+修订」成立须再核**。**若成立则 (b) 是下界；若不成立则仍有 (a') 更优解。归 06.R12v 或独立取证单核这一条，再定 spec 是否须弱化。**

**自决接受**：白名单外延 `runtime/task/cancel.go`（`Cancel`/`RequestCancel` 唯一落点，仅水合 6 行）；`gofmt -w` 清自引尾空行；`lifecycle.go:338` doc-not-brief 自纠（第三段并入第二段）。**未做明示**：×10、变异证明、全量复跑、race ⇒ 06.R12v。

**当前无在跑单**。**即派**：①**「只取身份+修订是否须读 body」取证单**（决定 spec 是否须弱化，只读零编译）；②08.R4a（切面族 200 行纯删除＋F6 注释真话，触 ledger，与①不相交）。

### R121 ★用户裁「不考虑成本只考虑架构优雅与纯粹」⇒ 编排者依纯度判据推导裁 (c) 真水位化，spec 原 SHALL 不弱化（2026-11-17）

**取证单结论（硬实测）**：编排者质疑**部分证实**——身份确在键里（`tagent/task/v1/fact:<taskID>\x00<mutation>`），**修订＝该任务 fact 键计数**（语义＝已应用链接 `ExpectedRevision+1`，连续无删除）⇒ **前单「取身份+修订须读每条 body」对这两字段是错的**。但 **(a') 仍不成立**，三条硬证据：①**字节下界** 键走查 **278.0B/任务** vs 水位 **91.1B/任务**（≥3.0×，最瘦单链接 101B>91B）；②**`Keys(prefix)` 不是前缀定向读**——实为 `eachRecordOnce` **整表顺序读** `capacity×96B`＋前缀仅过滤＋全排序 ⇒ 读**全库键**含全部会话事件；③**终态无别的存活载体**（`store:seat` 只有全局 Count/CommitSeq，无 `settled:*` 键，idx 值只带 type/session/commit），而 `restore.go:199` 被覆盖段报告要它。深度序列 8→512：键字节/任务恒 278.0、Keys 耗时 91.7µs→2447µs（×27）⇒ **线性，打穿平坦界**。
- **⇒ 弱化理由若须用，必须换成这三条，不得照抄「取数须读 body」（已被实测推翻，留证会误导后续设计）。**
- 取证单另登记第三路（不主张）：boot 连枚举已结任务都不要 ⇒ checkpoint 可真有界，代价是 `read.go:456-460` 的 `Tasks()` 把 O(历史) 移到查询期。

**⚠️ 用户裁定：「回归项目迭代原则，不考虑成本只考虑架构设计优雅和纯粹」⇒ 成本不是裁量轴，编排者依纯度判据推导**：
1. **checkpoint 是派生读视图**（GLOSSARY「投影：由事实链装配出的有界读视图：只读，不写回事实链」）。每已结任务载 {身份,终态,修订} **不是水位，是已结态的物化索引**——链已陈述这三者，checkpoint 再陈述一遍 ⇒ **违「无第二真相」**；且随历史增长 ⇒ **违「派生载体不得迫使全量读」**。**91.1B/项只是把违反的常数压小，不改变性质**（R115 F4 已判「压常数 15×≠有界」，同律）。
2. **真水位是一个标量**（cut 位置），不是 N 条记录。`restoreChains` 之所以需 per-task {身份,修订} map，是因为它用**特异化的逐任务覆盖判据**；而框架检查点对事件早已用**统一 cut/offset**（`CheckpointOffset` 按分区）表达「此位置之前皆被覆盖」。**per-task revision map 与统一 cut 是同一语义的两套机制** ⇒ 违「更少特异化代码／统一多态契约胜多端口」，应由后者取代。
3. **故裁 (c) 的纯形态**：**checkpoint 只载开放项＋统一 cut 水位；已结任务完全留链上、按需按身份读；boot 不枚举已结任务**。`Tasks()` 读投影枚举全史是**本征 O(历史)**，按「O(n) 拆本征 vs 可修」**如实声明**即可，且**不在 boot 路径上**——把本征成本放在真需要它的消费者那里，而不是摊进每次 boot 与每刀 checkpoint。
4. **⇒ spec 原 SHALL 不需弱化**：「大小 SHALL 有界于开放项数量」「已结部分 SHALL 留在链上而不嵌入 checkpoint」**是对的，实现错了**。**撤销 R120 登记的「须呈用户裁契约弱化」——用户已裁纯度优先，故不弱化，改实现。**
5. **哲学一致性**：这与本变更已裁的「纯索引＋统一读」「链唯一真相」「检查点托管」同构——**索引层已做到不物化全量（`l.index` 删、store 常驻索引去物化），checkpoint 层必须同律**，否则三层去物化在第四层（派生态）破功。

**新叶 06.R13（(c) 真水位化）**：per-task coverage/revision map → 统一 cut 水位；checkpoint 只载开放项；已结任务按需按身份读；`restoreChains` 跳过判据改用 cut；`Tasks()` 的本征 O(历史) 显式声明。**结构锚**：checkpoint body 字节**不随已结任务数增长**（跨深度对照，非每项恒定——那是 (b) 的判据）。**boot 三道界与走查计数面不得回退**。

### R122 08.R4a 收口：切面族 177 行纯删除全绿；R101 措辞更正；bootContext 零读者（2026-11-17）

**凭据**：切面族三块精确删除（`:68-78` 两常量含注释／`:158-191` 两类型／`:759-890` 四函数，共 **177 行**），删前逐符号 `grep -rnw` 含 examples 与全部 `_test.go` 复核 **journal.go 之外命中全 0**（八符号逐个给总/外计数）。另删因此未用的 `"slices"` import。`journal_index_cut_test.go` **整文件删**（58 行），其唯一存活锚移入 `journal_test.go` 并改名 **`TestOpenReadsNoByteBesideAStatedWatermarkAtTheLogEnd`**（原名以已删机制命名属残留）。
- **F6 三处注释改真话**；另 grep 全 ledger 生产＋`assembly/run.go` 确认无其它伪陈述（LRU 语义的 "cold" 是存活的常驻集淘汰，非冷档，正确排除）。
- **读路径具名拒绝**：新错误 **`ErrReadAfterClose`**（仿 `ErrCommitAfterClose`），`Read`/`Keys`/`Namespaces` 在 `k.closed` 时具名拒；负锚 `TestJournalReadThroughAClosedViewStatesTheClosedView` 断言 `errors.Is(err, ErrReadAfterClose)` **且** `!errors.Is(err, ErrSegmentBodyMissing)`，三面同断言。**置于锁后、查常驻集之前 ⇒ 关档视图连缓存字节也不答**（提交已拒，读亦统一拒）。
- **漂移守卫已落**：`TestJournalWindowStaysWithinTheLogBudget` 首行 `if journalLogSyncBytes != 65536 { t.Fatalf(...) }` ⇒ 预算任何改动即红，强制同批更新 assembly 界。
- build 0、vet 0、**ledger 全包 143 RUN/143 PASS/0 FAIL**、boot 两枚 assembly 0 且**三界保持全 0**、残留守卫八符号 go 命中 **0**＋`JournalIndexCut|IndexCutNotice|l.index|journal.index` **0**、注释门产品面 **0 finding**、词汇门 0、product_surface 0；棘轮唯一 beyond＝fold-cold-root（禁改）；concept 五条残留与 R116 登记完全一致无新增。

**⚠️ R101 措辞更正（代理实证）**：`Keys`/`Namespaces` 关档后**并非报 `ErrSegmentBodyMissing`，而是静默答空**——`journalAddress.lookup` 在 `slots==nil` 时返 not held、`eachHeld` 在 `slots==nil` 时跳读；`ErrSegmentBodyMissing` 是 `FileSegmentStore` 消费面把「not held」翻译出来的形态。**两者都是「真损坏／假缺体不可分」，具名拒绝一并覆盖**。R101 该句按此勘误。

**新登记残留**：**`assembly/run.go` 的 `bootContext` 字段现零读者**（全仓仅声明与结构体字面量 3 处）⇒ 注释已改真话「Nothing in this package reads it」，但**删字段越出「仅注释」授权，代理正确停下** ⇒ **归新叶删除**（零残留）。另 `kv_original_store.go:214` 注释 182 rune >160，但三条存活判据压不进 160 且该文件既有 bullet 达 221/222、注释门 0 finding ⇒ **登记为门对该文件不生效的盲区**，不强压。

**当前无在跑单**。**即派 06.R13（(c) 真水位化）**；随后 06.R12v（变异证明＋全量复跑＋新红干净 worktree 归属）、F5 scratch 删除、10.R8b、bootContext 删除、S4 两样例、delivery-closure 前置、DoD 变异注册真跑＋P5/P6 锚、新 dead-producers 门、09.R2/R3、R35 第二轮、F1–F4。

### R123 06.R13 零改动停下，反驳成立；编排者自纠前提并多拆一层（boot 不该为没人索取的报告枚举历史）（2026-11-17）

**前单反驳三条，全部采纳**：
1. **水位承载三个角色不是一个**：①跳过判据（**可由 cut 表达**，框架先例成立）②**已结身份集载体**——`assembly/run.go:1106` `durable.TaskIDs()` **在 boot 路径上**且喂 1159-1162 恢复报告；`runtime/state/durable.go:523-542` `seatWatermarkLocked` 是已结任务进入 `TaskIDs()`(1121-1132) 与 `Revision()`(1063-1067) 的**唯一免读来源**，删水位即删身份集与修订 ③修订来源。**cut 是标量位置，无法解引用成身份**。
2. **单位不可比**：`chainPast`（run.go:1172-1181）按 `Fact.ExpectedRevision`（**每任务计数器**，restore.go:289）过滤，与全局 `CheckpointOffset`（`api/event/checkpoint.go:30-36`）不可直接比。
3. **报告终态改读链会打穿界**：`restore.go:198-200` 取 `stated.Status`，改读链触发 `d.scan`（durable.go:625-629,744）⇒ `durableLoadCount>0`，违 `symptom_anchor_recovery_test.go:745`。

**⚠️ 编排者前提错，已纠**：R121 断言「`Tasks()` 不在 boot 路径」——**`TaskIDs()` 确在 boot 路径（run.go:1106）**。故「把本征 O(历史) 移到查询期」不能简单成立，须先分清 boot 期必需与诊断期便利。

**编排者多拆一层的裁定（纯度推导，方向不变）**：**boot 不该为没人索取的报告去枚举历史**。恢复报告列举「有史以来全部已结任务」若属**诊断便利**而非 boot 必需，则该枚举归**按需查询**。故形状＝**`BootTaskIDs()`（open＋tail，有界）供 boot**＋**全量已结枚举移按需通道并显式声明本征 O(历史)**＋**checkpoint 只载开放项＋cut**。
- **但这取决于一个必须先证的事实**：**恢复报告的已结身份集有没有 boot 期消费者**（已知线索 `read.go:460`、`memory/query/inventory.go:583`、`host_diagnostics.go:341` DiagnosticRecovery.Status，须核实补全）。**有则须裁该消费者契约（另一取向），无则可实施。**

**06.R13b 已派**：第一步取证报告/`Recovery()` 全部消费者与调用时机；**仅在无 boot 期消费者需完整身份集时**进第二步实施。**扩权 `assembly/run.go`**（仅 restoreChains／报告取数／Recovery 一带）。**三个明令停下点**：①有 boot 期消费者真需完整身份集；②revision 与 cut 单位确不可通约（**不得造假换算**）；③报告终态按需取与 `durableLoadCount=0` 不可兼得。**结构锚须从 (b) 的「每项恒定字节」重述为 (c) 的「body 不随已结数增长」——取代非弱化，(c) 严格强于 (b)**。

**前单零改动故树未变**：build 0、vet 0、boot 五锚全绿三界全 0、注释门产品面 0、词汇门 0；两枚既红仍在（`TestConsumptionFailureIsReportedByName`／`TestDurableCommitBarrier`）。**未建锚、未重述 (b) 锚，明确声明未做**。

**当前并发**：06.R13b 一单。

### R124 ★06.R13b 落地 (c) 真水位化：checkpoint 已结项一条不写，结构锚逐字节相等；两个前提被取证否掉（2026-11-17）

**取证裁定：恢复报告无任何 boot 期消费者** ⇒ (c) 可实施。逐消费者：`Diagnostics`→`lifecycleFace`→`Recovery()`（`host_diagnostics.go:341`，:271 装配）**查询期**；`GET /diagnostics`（`cli/cli.go:271`）**HTTP 请求期**；`runReading.Tasks`（`read.go:460`→`memory/query/inventory.go:583`）查询期且走 `runCommit.TaskIDs()` **不是** `Recovery()`；`attentionWorkEnd`/`closeTasks`（`host_attention.go:123`、`invoke.go:688`）收尾期**只需仍有开放义务者**。boot 路径上真消费已结身份集的是另两处（registry 水合、`runCommit.held`），**均已改按身份读链**。

**⚠️ 两个前提被否，编排者采纳**：
1. **不加 `Cut` 标量**（代理自决，理由成立）：**cut 就是存储提交序**（checkpoint fact 自身位置），boot 已由 `tailFactKeys` 走出 tail（`assembly/checkpoint.go:902`），`Seat` 的 `tail` 就是 cut 的物化 ⇒ **再加标量是同一位置的第二真相**，且 registry 侧无法把标量换算成键。**合「无第二真相」，采纳。**
2. **「单位不可通约」是错的**：`durable.Revision(id)`（`applyRecordLocked` 写 `ExpectedRevision+1`）与 `StatedTask.Revision`（`entry.revision`）**都是该任务自身计数器**，`chainPast`（run.go:1172-1181）两侧**同单位**，注释已写死。**R123 登记的停点②不成立**——代理核实而非造假换算，正确。

**落地形状**：`RegistryCheckpoint{Tasks []StatedTask}`，**`StatedTask` 删 `Settled` 字段**，`SnapshotCheckpoint` 只写 `!settledAtCut()` 者（终态且无欠账**一条不写**）；`r.settled`／`seatWatermarkLocked`／durable `watermarks` 集合／`CheckpointRestored` 水位分支**全删，零残留无兼容分支**。`TaskIDs()` 现恰为「开放项∪tail」（有界零读）；全量枚举新增 **`HistoryTaskIDs(ctx)`** 只由 `assembly/read.go` 查询通道调用。终态在**查询期**经 `Lookup`→`hydrateLocked`（无水位，任意未持有身份读链）→`durable.Records`（按需 `readTaskLocked`）；链无 admission 改判 `ErrTaskNotFound`。**boot 期 `d.scan` 计数保持 0**。
- **本征 O(历史) 显式声明已落**（`HistoryTaskIDs` 与 `runReading.Tasks` 各一条，原文见战报）：「the price of a reading that names every task ever admitted, and it is paid by this query alone and never by a boot」。

**★结构锚达成 (c) 判据（严格强于 (b)）**：**`TestSettledTasksAddNoCheckpointByte`** `-count=1` PASS——同一 registry 在 **8/32/128/512 已结深度 body 逐字节相等**；`PruneTerminal` 丢掉全部 512 条后**仍逐字节相等**；开放项固定 2 条全程被写出（负锚：非空账户）。另重述四锚全 PASS，**三入口语义全保**（Advance/Cancel→`ErrTerminalState`、Resume(Cancelled)→`ErrResumeCancelled`、Resume(Settled)→新轮、陌生人→`ErrTaskNotFound`）。
- **boot 三界实测全 0**：`journalBytes/indexEntries/durableLoadCount=[0 0 0 0 0]`、`wallClock=[13.5 7.9 8.2 8.9 10.1]ms`、`indexWalkVisits=[0 4 4 4 4]`、`indexWalkValueBytes=[0 252 252 260 260]`、`journalLogBytes` 单调增长 ⇒ **走查比 1.00–1.03**。build 0、vet 0、注释门产品面 0、词汇门 0。

**⚠️ 剩余红全是「夹具仍按 (b) 契约断言」，属被取代判据须重述（非产品缺陷）**：
- `assembly/symptom_anchor_recovery_test.go`：`judgeBootFixture` 的 `entry.accounted = len(durable.TaskIDs())`（:295/:228）要求账户＝20 条已结任务；`judgeSeatedAccount`（:155，probe :483）要求账户非空。**(c) 下账户只含开放项∪tail，故这些判定须改为「已结任务不在账户、按链比对」，非空证据换 `HistoryTaskIDs`**。
- `assembly/task_checkpoint_test.go` 三个用例同按 (b) 断言「已结任务存在于账户」⇒ 改按链比对。
- `assembly/anchored_boot_parity_test.go:156` `read the order file … invalid argument`（关 tier 后走 `EachIndexNewestFirst`）——**代理已 A/B 证明与本次改动无关**（临时移除 settled 跳过仍同错，已还原）⇒ **独立缺陷，归 D1 类关档后读诊断**。
- 两枚既红仍在（`TestConsumptionFailureIsReportedByName`／`TestDurableCommitBarrier`）。

**自决接受**：不加 Cut 标量；`Recovery()` 保持 boot 折叠语义、全量走查询通道；结构锚用 `WithTerminalTTL`+`PruneTerminal`（不用禁改的 Option）；两测试改名（`…ByWatermarkOnly`→`…NoSettledTaskAtAll`、`…ReadAWatermarkedTask…`→`…ReadASettledTask…`）⇒ **派发令 `-run` 正则里旧名已无匹配，后续单须用新名**。

**即派 06.R13c**：扩白名单到上述三个测试文件，把 (b) 契约夹具重述为 (c)（**取代非弱化**：主题「boot 有界／账户可查」不变，判据从「账户含已结」改为「账户只含开放项＋已结按链可查」）。**这是 boot 症状锚转绿的最后一块。**

### R125 ★boot 症状锚转绿（编排者亲验）；06.R13c 撞限但树可编译，夹具重述须收尾核验（2026-11-17）

**第十一次撞 150-turn**，末句「Now rewriting case A's body」（正在改 `task_checkpoint_test.go` 用例正文）。**编排者诊断现场（R86 手法）**：
- `go build ./...` **0**、`go vet ./assembly/ ./runtime/...` **0** ⇒ **树可编译，无半删态**。
- **★`go test ./assembly/ -run 'TestSymptomAnchorBootCostFlatAcrossGenerations|TestBootQuantityProbesReadTheRealBoot|TestBootFixtureGrowsTasksAcrossGenerations' -count=1` → EXIT=0（ok 10.896s），三枚全 PASS。**

**⇒ boot 症状直译锚转绿**：结合 R124 实测三界 `journalBytes/indexEntries/durableLoadCount=[0 0 0 0 0]`、走查比 1.00–1.03、结构锚 `TestSettledTasksAddNoCheckpointByte` 在 8/32/128/512 已结深度 **body 逐字节相等**，**(c) 真水位化已兑现「重启成本不随历史增长」**。**四层去物化全部完成**（日志层／store 常驻索引／耐久任务账扫描／派生态 checkpoint）。

**⚠️ 未闭合项（撞限遗留，不得当作已完成）**：
1. **`assembly/task_checkpoint_test.go` 三个用例的重述停在「case A 正文改写中」** ⇒ 虽可编译，**断言是否已完整改到 (c) 契约、是否仍绿、是否被弱化，均未核验**。**须收尾单逐个核**：跑该文件全部用例、逐处比对旧→新断言、论证主题不变非弱化。
2. **变异证明未做**（派发令第 6 项）：让 checkpoint 重新写已结项 ⇒ 结构锚须红；让账户判定退回「含已结」⇒ 重述锚须红。**未经变异证明的锚不得当作有牙**（R29）。
3. **长跑锚 `TestSymptomAnchorPerGenerationCostFlatOverALongRun` 未复跑** ⇒ 每代总成本是否达标**未知**。(c) 解了 checkpoint 写入与 boot 读，但 R109 定位的 sessions/frame 阶段增长是否随之消解须实测。
4. `symptom_anchor_recovery_test.go:528` 注释行合并（R35 F10）未确认是否清偿。

**下一步序**：**06.R13d（夹具收尾核验＋变异证明＋长跑锚复跑）** → 06.R12v 合并入该单或另派（全量复跑＋四项新红干净 worktree 归属）→ F5 scratch 删除 → 10.R8b 端口收口 → bootContext 删除 → S4 两样例 → delivery-closure 前置 → DoD 变异注册真跑＋P5/P6 锚 → 新 dead-producers 门 → 09.R2/R3 → **R35 第二轮** → F1–F4。

**仍待用户裁两项**：doc_budget +8（基线相对 HEAD 过期，工作树净减 113 行）；提交/推送/归档授权。

### R126 06.R13d 闭合 3/4：变异证明齐全、夹具未弱化；长跑锚定位到 sessions/frame 未消解（2026-11-17）

**凭据**：`task_checkpoint_test.go` 四用例全 PASS（`TestTaskRestoreBoundedByCheckpoint` 5.21s／`…FoldsTheTailOfACoveredChain` 5.06s／`…WithoutCheckpointFoldsEveryChain` 0.08s／`TestTaskTailFoldLeavesNoSweepStamp` 5.07s）。六处重述逐处比对（`git diff` 126+/83−）：`statedTaskOf`→`chainStateOf`（按身份读链）、观察行 `covered=8`→`0`、report 11 条→`len==3`＋covered 不得出现＋`accountStates=false`＋逐条按链比对（11 条全量枚举移交 `runReading.Tasks` 真实查询消费者）、refold 比对改「链恰 2 条＋末链 Settled＋ExpectedRevision=1」、tail-stamp 删 `ChangedAt.IsZero()` 改**正负双锚（增强）**、三比对器→`sameSettledState`。**未发现弱化**：「账户无已结」由三道断言＋观察行把住（`chains=3,covered=0`）。
- **变异证明两处齐全且字节还原**：**A** 去掉 `settledAtCut()` 跳过 ⇒ `TestSettledTasksAddNoCheckpointByte` FAIL `beside 8 finished task(s) states 10 entr(ies), want the 2 open task(s) alone`；**B** `run.go:1107` 改用 `HistoryTaskIDs` 全量 ⇒ `TestTaskRestoreBoundedByCheckpoint` FAIL `restore folded chains=11 links=22 covered=0, want chains=3`，**同一变异令 boot 症状锚红**（`durableLoadCount=[0 20 40 60 80]`、夹具拒「gen1 seats 8 task(s), want 4」）。还原后 `sha256sum -c` 三文件全「成功」、零残留。
- boot 三锚 PASS、**三界全 0**、`durableAccount=[4 4 4 4 4]`、`historyTask=20`、`indexWalkVisits=[0 4 4 4 4]`。build 0、vet 0、注释门产品面 0、词汇门 0。:528 注释合并**已由前单清偿**（现 :592 单行完整）。

**⚠️ 编排者两处派发前提被反驳，成立**：
1. **第 7 条 `-run` 打错包＝假绿**：四枚契约锚在 `./runtime/task/`，`./assembly/` 下**零匹配**（`ok … [no tests to run]` exit 0）。按正确包 `-count=10` → **40 PASS/0 FAIL**。**教训：派发令的 `-run` 正则须连同包路径一起核实，否则「no tests to run」的 exit 0 会被当绿**（R29 空转病的又一形态）。
2. **comment_policy beyond 实为 12 非 1**：11 条 `audit-marker` 在 `features/rl/*`＋`scripts/comment_policy/main.go`（**这些文件工作树未改、HEAD 原状**）⇒ **`scripts/comment_policy/baseline.json` 两类记 0 的基线过期**，非本变更引入；＋1 条 fold-cold-root doc-not-brief。**归基线对账，与 doc_budget +8 同属「基线相对 HEAD 过期」类，一并呈用户裁**。

**⚠️ 长跑锚未达标，定位清楚（须新叶）**：
- **夹具本身仍按 (b) 契约**：`judgeLongRunFixture:145` 拒「generation 1 restored an account of 0 task(s), want the 1 the generations before it settled」⇒ **先红在夹具而非增长读数**。该文件不在前单白名单，**未做永久改动**；为取读数做临时改（仅 :145 期望 0＋逐阶段 log），跑完哈希还原零 diff。
- **临时读数（252 代/窗口 5/中位/界 1.3）**：**boot 8.12→16.09ms＝1.9× 红**；commits 1.0；settling 1.0；**sessions 5.01→15.18ms＝3.0× 红**；**frame 5.18→14.98ms＝2.8× 红**；closing 1.0；机噪参考 85.2 vs 87.7ms（同界内）。
- **⇒ (c) 只消解任务账户侧；sessions/frame 增长未消解**。承载体：`CheckpointSession`、`Checkpoint`、`noTrimChainRun` 打开成长日志。**未放宽 1.3**。
- **推测须实测验证（不得当结论）**：R112 曾判 compress 切片占 19% 并已改同形状，但 sessions 仍 3.0× ⇒ **会话链侧另有物化全量者**（候选：会话 checkpoint 载已答压缩判决全文、或 `noTrimChainRun` 的折叠走查）。**须 profile 定位，不得凭推测修**。

**即派 06.R14**：①`assembly/symptom_anchor_longrun_test.go:145` 夹具重述为 (c)（历史增长证据移 `HistoryTaskIDs`）；②profile 定位 sessions/frame 剩余增长项并修；③复跑各阶段比值。**这是长跑锚转绿的关键路径。**

### R127 ★★长跑锚转绿：两条症状直译锚全绿，反转核心目标达成（2026-11-17，编排者亲验）

**06.R14 第十二次撞 150-turn**，末句「Now the mutation proof for the compress fix (byte-identical restore afterwards)」⇒ **已定位并修了 compress 侧增长项，停在变异证明**。**编排者诊断现场并亲跑（R86 手法）**：
```
go build ./...                                                    → 0
go vet ./assembly/ ./runtime/...                                  → 0
go test ./assembly/ -run 'TestSymptomAnchorBootCostFlatAcrossGenerations|
  TestBootQuantityProbesReadTheRealBoot|TestBootFixtureGrowsTasksAcrossGenerations' -count=1 → 0
go test ./assembly/ -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1    → 0  (ok 23.001s)
```
**⇒ ★两条症状直译锚全部转绿。反转核心目标达成：重启成本与每代总成本均不再随历史增长。** 长跑锚耗时从 ~75s 降到 **23.0s**（旁证成本真降，非界放宽）。

**收敛轨迹（全程实测，非推断）**：长跑 total 6.4× → boot 阶段 21×→3.52×→1.92×→1.9× → **(c) 真水位化＋compress 侧修后全阶段达标**。四层去物化＋派生态真水位化全部落地：①日志层只重放表头水位到日志末端的窗口 ②store 常驻索引不物化 ③耐久任务账走尾谓词 ④**checkpoint 对已结项一条不写**（结构锚证 8/32/128/512 已结深度 body 逐字节相等）。

**⚠️ 未闭合项（撞限遗留，不得当作已完成）**：
1. **compress 修法的变异证明未做**（代理正要做时撞限）⇒ **该修法是否有牙未证**。**未经变异证明不得当作已闭合**（R29）。
2. **各阶段比值读数未取**（编排者只取了总退出码）⇒ 须补各阶段首窗/末窗序列，确认**不是靠某阶段侥幸压线**。
3. **夹具重述的逐处旧→新与主题不变论证未收到报告** ⇒ 须核 `judgeLongRunFixture:145` 等是否被弱化。
4. **全量复跑未做**：`./assembly/` 全量、`./tests/...`、`./runtime/...`、`-race`、新锚 ×10。
5. **四项新红的干净 worktree 归属对照未做**（`TestAnchoredBootMatchesFullBoot` 的 order file invalid argument、`TestDeliveryDurableReadErrorSurfaced`、`TestTierParent*`、`TestRestartRecoveryPinsBeforeTrim`）＋两枚既红（`TestConsumptionFailureIsReportedByName`／`TestDurableCommitBarrier`）。

**即派 06.R14v（独立验证单，只跑不改）**：补 compress 变异证明＋各阶段读数＋夹具重述核验＋全量复跑＋新红干净 worktree 归属。**实施与验证分单**（本会话十二次撞限的教训）。

**随后队列**：F5 scratch 绕法删除 → 10.R8b 三接口收口（`api/durable` 新叶、取 3 方法、净减 2 接口）→ bootContext 死字段删除 → S4 两样例（`CommitOrder`/`ChainSource`）→ delivery-closure 同前置 → DoD 变异注册真跑＋P1 criterion＋P5/P6 锚 → 新 dead-producers 门 → 09.R2 校准／09.R3 全矩阵 → **R35 第二轮独立拷问（修复后再审）** → F1–F4。

**仍待用户裁三项**：①doc_budget +8（基线相对 HEAD 过期，工作树净减 113 行）②comment_policy 基线 11 条 audit-marker（落在 HEAD 原状文件，基线两类记 0 过期）③提交/推送/归档授权。**①②同属「基线相对 HEAD 过期」类，建议一并裁一次性对账并记理由；属放宽门须用户明示，编排者不自行采用。**

### R128 ★编排者自纠：①②不是用户裁决点，是调查不足；只有提交授权真需用户（2026-11-17）

**用户反问「真的需要我来裁吗」⇒ 编排者重核，两项上报均不成立**：

**② comment_policy「11 条 audit-marker／基线过期」不成立**。编排者亲跑 `go run ./scripts/comment_policy . examples/wechat-bot examples/cli-host` → **2 finding／2 beyond baseline**，非 12。`grep audit-marker` **零命中** ⇒ 前单（06.R13d）该报告失效或口径错。实际两条：
1. **`scripts/fold-cold-root/main.go:1` doc-not-brief**：包注释带 3 段散文，超「2 段职责陈述」上限。**⚠️ 这条是编排者 R119 裁定造成的**——当时裁「机制细节归工具 doc/`-help`，手册只留运维动作」，而**门要求细节放 `docs/wiki` 段、包注释只留 2 段＋bullets＋索引**。**编排者裁错落点，须自纠：把机制细节移 `docs/wiki`（`memory-architecture.md` §六一带已有相关段），包注释收到 2 段＋bullets＋索引**。**不是用户裁决点。**
2. 另 1 条 `test-doc-line-too-long`（须定位）。

**① doc_budget +8：正当路径已试但未成，且调查未尽**。
- **已试**：`bash scripts/gates/gen_godoc.sh --check` → **exit 1，5+ 文件不一致**（`docs/api/` 确实过期，仍文档化已删符号）⇒ 跑 `gen_godoc.sh` **写 82 文件**（新鲜度门本就是 lint 要求，非放宽）。**但 measured 仍 22161（+8）**——删掉符号的文档被新增符号（`CommitOrder`/`HistoryTaskIDs`/`ErrReadAfterClose` 等）的文档**抵消**。**该路径不成立，编排者判错。**
- **⚠️ 调查未尽**：前几单只找 `docs/OPERATION.md` 与 `CHANGELOG` 两文件，而**计数面是 231 个文档**（`counts.documents=231`）。**未扫全面就报「无损空间已尽、须用户裁放宽门」＝把调查不足包装成决策点**。**须先全面扫 231 文档找真冗余（已删机制的整段描述、重复叙述、可合并段），再判是否真须放宽。**

**③ 提交/推送/归档真需用户明示**——外部、难回退、影响共享状态，属用户 standing 约束，非编排者可自裁。

**⇒ 修正分工**：①②编排者自收（先全面扫 231 文档，再修两条注释＋纠正 R119 的落点裁定）；**只有提交授权等用户**。**教训回写：上报「须用户裁」前必须先穷尽正当路径与调查面；把调查不足包装成决策点是伪 defer（违「既有原则能裁的独立裁不伪defer」）。**

### R129 用户误中断子代理；编排者核现场后只重派未落部分（2026-11-17）

**用户告知「不小心中断子代理执行」。编排者核现场判定哪些已落**：`go build ./...` **0**、`go vet ./assembly/ ./runtime/... ./memory/ledger/` **0**；**`comment_policy . examples/wechat-bot examples/cli-host` → 0 finding／0 beyond baseline** ⇒ **D3 的两条注释已修好**（包注释落点纠正＋test-doc-line-too-long）；**`check_doc_budget` 仍 measured 22161（+8）** ⇒ **D3 第 3 项 231 文档扫描未做**；`CHANGELOG.md`／`docs/OPERATION.md`／`docs/api/*` 已 M。06.R14v 验证单亦被中断。

**重派两单并发（写集不相交）**：
- **D3b**：唯一交付＝扫全 231 文档压 ≥8 行真冗余。**先读门扫描逻辑核实计数面含哪些路径**（前几单只扫 2 文件即报「空间已尽」＝调查不足）。明列已删机制清单与不得删清单（四前置/四要素/勘误本体/导航/GLOSSARY 岗位让位本义/同形异义）。**明禁重试已无效路径**（gen_godoc 已跑，行数被新增符号文档抵消）。**本仓一行一段⇒压措辞不减行，只有删段/并段减行**。
- **06.R14v**：五项未证补齐（compress 变异证明／各阶段比值／夹具防弱化核验／全量复跑＋竞态＋关键锚 ×10／六红项干净 worktree 归属）。**关键锚在 `./runtime/task/` 不在 assembly，打错包会「no tests to run」exit 0 假绿**（R126 教训已写入令）。

**中断恢复纪律回写**：子代理被中断后**不得原样重派**，须先核现场（build/vet/门读数/git status）判定**哪些交付已落、哪些未落**，只重派未落部分并带上已落事实，避免重做与互踩。本次 D3 三项已落两项，故 D3b 只派第 3 项。

### R130 概念/词汇审查（干净）＋纯度落实审计（8 项缺口仍开）＋计划整理（2026-11-17，目标驱动）

**用户目标**：整理计划、并行派发推进优雅完成；已派发则 review 已实现部分、核对违规概念与词汇、思考「不考虑实现成本只考虑架构优雅纯粹性」是否落实。

**① 概念/词汇审查——结论：干净（门禁＋门盲区双层扫）**：
- 门禁面：`check_vocabulary_alignment` **0 violations**（carrier/compression-generation/plugin-product/retired-word 四规则全 0）；`comment_policy` 两 scope **0 finding／0 beyond baseline**。
- **门盲区扫（中文生造词在 .go）**：`座位/载体/屏障/接线/冷归档/反转/退位/冷层/冷档` **全 0** ⇒ 代码注释未引入中文生造概念。
- **英文疑似生造/退役词**：`carrier` 0、`vessel` 0、`substrate` 0。`bearer` 9 处**核实为 HTTP `Bearer token`／`WWW-Authenticate: Bearer realm` 标准协议词**（`assembly/host.go:92`、`host_face.go:138/146/164/174`），**非 carrier 同义生造，合法**。
- **`seat`/`watermark`/`barrier` 核实为 HEAD 既有代码实名**：HEAD 计数 211/376/244 → 现 357/356/240 ⇒ **非本变更新造**，符合 GLOSSARY:3「描述机制一律用代码实名」。`水位` 在 GLOSSARY:45 框架检查点条已登记。
- **⇒ 无违规概念与词汇。** 先前担心的中文生造词只存在于**计划文档**（编排者台账用语），未进代码；台账属过程工件不受代码词汇门约束，但**后续宜统一用代码实名**（seat/watermark/barrier）而非中文别名。

**② 纯度落实审计——8 项缺口仍开，不得以「症状锚转绿」宣称纯度已落实**：
| # | 缺口 | 纯度判据 | 状态 |
|---|---|---|---|
| 1 | **三接口同构未收口**（`state.FactLog`/`GovernanceEventLog`/`AttentionFactLog`，后两者逐字同构＋同实现者） | 统一多态契约胜多端口 | **用户已裁 `api/durable` 新叶取 3 方法，10.R8b 未实施** |
| 2 | **fold-cold-root scratch 绕法** | 绕开在飞缺陷的临时结构须随缺陷修好删除 | **F5 已派（本轮）** |
| 3 | **`assembly/run.go` bootContext 死字段**（零读者） | 零残留 | 待派 |
| 4 | **P6「无死代码」谓词 anchor 为空** | 立机器机制不立规范；死代码可编译故 build/vet 盲 | 待派（新 dead-producers 门） |
| 5 | **DoD 谓词 P1 criterion 腐化＋P5/P6 无锚＋8 变异未注册＋9 无 run** | 谓词五字段各自腐化；自证无效 | 待派（触 scripts，须待他人改动落地） |
| 6 | **S4 两样例缺**（`CommitOrder`/`ChainSource`） | 契约面须有规范样例 | 待派（ChainSource 形状已定，可派） |
| 7 | **`delivery-closure/spec.md:9`「恒可回读」缺冷档前置** | spec 与行为一致 | 待派 |
| 8 | **concept_budget 5 条残留** | 按名抬升只针对确已存活者 | 3 条 hold 待自解（S2 Settled 已删⇒须复核是否自解、S1/S4 AttentionFactLog 待 10.R8b）；2 条 S4 样例同 #6 |

**⚠️ 审计发现须复核项**：R116 曾裁「S2 `StatedTask` 17>16 hold 待 06.R12c，因真水位化可能删 `Settled`」——**06.R13b 确已删 `Settled` 字段** ⇒ **该 S2 error 应已自解，须复跑确认并从残留清单移除**（不得留过期账）。

**③ 计划整理（当前并发与就绪队列）**：
- **在跑三单**：D3b（231 文档压 8 行，纯文档零编译）、06.R14v（五项未证补齐＋全量复跑＋六红项归属）、F5（删 scratch 绕法，scripts/fold-cold-root）。**三者写集互不相交**。
- **待 06.R14v 收口即派**：**10.R8b 三接口收口**（触 runtime/state＋governance＋approval＋assembly 生产＋examples＋golden，与验证单的全量复跑编译面冲突，故须待其收口）；**bootContext 删除**（触 assembly/run.go，同因待）。
- **可即派（零编译/不相交）**：#6 S4 两样例（examples/contract 新文件）、#7 delivery-closure 前置（openspec/specs）。**但 #6 的 `ChainSource` 样例须待 10.R8b 收口后写**（端口将迁 `api/durable`，现在写即待撤残留）⇒ **只有 `CommitOrder` 样例可现在写**；#7 可现在派。
- **随后**：#4 dead-producers 门 → #5 DoD 真跑 → 09.R2 校准／09.R3 全矩阵 → **R35 第二轮独立拷问** → F1–F4。

**⇒ 纯度未完全落实，不得标记目标完成。** 症状锚转绿只证「成本不随历史增长」，**不证「无重复契约/无死结构/无绕法/门面无盲区」**——后者是 #1–#8，须逐项闭合。

### R131 doc_budget 转绿（零差额）；S2 自解确证；概念/词汇审查干净（2026-11-17）

**① doc_budget 已绿，无需用户裁**：D3b 扫全计数面后 **measured 22161→22153，与基线零差额，exit 0**。
- **计数面核实**（门 `check_doc_budget.py:89-116`）：`git ls-files --cached --others --exclude-standard` 取 `.md/.markdown`，**扣 `docs/api/`、`examples/*/resources/prompts/`、`openspec/changes/`**。231 篇＝openspec/specs 152＋docs 42＋examples 21＋根 5＋resources/prompts 10＋internal/bench 1。**可写面仅 78 篇/8814 行**（2/3 是禁碰的主 spec）。
- **真冗余两处（净 −8）**：`memory-architecture.md:219`「调参」行把已删的 `SetRetrievalTuning`/`ErrRetrievalTuning` 当现行写入口（而 `store_searcher_test.go:112` 反断言该方法**不得存在**）⇒ 原地改写为真机理（0 行）；`examples/wechat-bot/skills/url-fetcher/TESTING.md:105-119`「未来改进」6 项与 `UPGRADE.md:210` 同表重述 ⇒ 收口到升级文档（**−8 行**）。
- **⚠️ 门是双向锁步**：**多删一行也判红**，故须精确 −8。**未用 `--tighten`/`--update-baseline`**。
- **⇒ 反驳前单「无损空间已尽」成立**：机制面确已扫净，但剩余冗余在**示例技能文档的重复路线图**，前几单只扫 OPERATION/CHANGELOG 故漏。**编排者 R128 自纠（「调查不足包装成决策点」）确证正确——①根本不需用户裁。**
- **登记待裁/待修**：`storage-durability-positioning.md:15-17` 引已删文件但整页带 **historical-verbatim 横幅** ⇒ 依约不改，具名保留。**另见现行页陈旧标识符（真话漂移，须修）**：`event-flow.md:17` `RecycleCount`（实为 `RecycleIdle`）、`memory-architecture.md:40/46/87` `kvBatch`、`event-flow.md:25`＋`GLOSSARY.md:21` `recordMapped`（实为 `foldMapped`）、`OPERATION.md:130` `CiWorkflowTest`、`memory-architecture.md:147/166` `commitOrder`（实名 `CommitOrder`）。**归后续文档单**。

**② S2 `StatedTask` 17>16 已自解确证**：`Settled` 在 `runtime/task/checkpoint.go` 计数 **0**（06.R13b 真水位化删了它），`check_concept_budget` **从 5 降到 4 error** ⇒ **R116 的 hold 预判成立，过期账已清**。剩 4 条：S4 `CommitOrder` 样例（**S4a 在跑**）、S4 `ChainSource` 样例（待 10.R8b）、S4 `AttentionFactLog` 红线＋S1 3>2（待 10.R8b 删该端口即消失）。

**③ 概念/词汇审查干净**（R130 已记，此处确认无新增）：中文生造词在 .go 全 0；`bearer` 是 HTTP 标准协议词；`seat`/`watermark`/`barrier` 均 HEAD 既有代码实名。

**当前并发四单**：06.R14v（验证五项＋全量复跑＋六红项归属）、F5（删 scratch 绕法）、S2（delivery-closure 前置）、S4a（CommitOrder 样例＋红线）。**写集互不相交**。

**剩余纯度缺口与就绪态**：#1 三接口收口（**待 06.R14v 收口**，编译面冲突）；#3 bootContext 死字段（同待）；#4 **P6 死代码门缺失（可即派，scripts/gates＋go.mod＋CI，与四单不相交）**；#5 DoD 真跑（待 scripts 他人改动落地）；#6 ChainSource 样例（待 #1）；#8 concept 剩 4 条（1 在跑、3 待 #1）。

### R132 S2 落地但推高 doc_budget；编排者纠「一律禁 --tighten」误判；五单并发（2026-11-17）

**S2 收口**：`delivery-closure/spec.md:9` 已加冷档前置（照 F7 口径），新增 Scenario 映射 fold-cold-root 命令；存活判据未弱化。`check-openspec.sh` 0、spec_hygiene 0、doc_links 0、词汇门 0。**前置成立实证**：`fold-cold-root/main.go:1-6` 明言「A build that reads one journal cannot serve a root whose cold journal…」、`OPERATION.md:161` 同口径、Go 树内 `cold` 仅 cold-start/cold value 无冷档读面。
- **⚠️ 但 doc_budget 22153→22159（+6）**：门是**双向锁步**（measured 须恰等 baseline，多删一行也红），D3b 刚压到零差额，S2 加 Scenario 又推高。
- **S2 枚举出第三处同病**：**`config-liveness:75`**「发布记录 MUST 重启后可回读」＋Scenario ⇒ 须加同一前置（发布记录落链于 `kv/journal.log`）。另复核 F7 三项判定成立；**附观察待裁**：旧构退位检查点若点名已搬键，`event-key-visibility:31` 票据可致召回答 missing，属条件暴露。

**⚠️ 编排者自纠一处过度保守**：此前多单派发令一律写「严禁 `--tighten`」——**误判**。`--tighten` 是门**自带的锁步协议**（只降不升，把真减少锁进基线，须与文档同提交），**不是放宽门**；禁的只是 `--update-baseline`（抬基线掩盖增长）。**正解流程**：同类前置一次加齐 → 压等量真冗余 → 若净减则 `--tighten` 锁步 → 与文档同提交。**已纠正并写入 D4 派发令。**

**D4 已派**（一次做齐避免反复推高）：①config-liveness 加同前置 ②再枚举同类绝对承诺确认无第四处（含复核 S2 七项判定）③压等量冗余使 measured ≤22153，净减则 `--tighten`；顺带修 D3b 登记的**陈旧标识符真话漂移**（`RecycleCount`→`RecycleIdle`、`kvBatch`、`recordMapped`→`foldMapped`、`CiWorkflowTest`、`commitOrder`→`CommitOrder`，**须先 grep 源码确认真名**）。

**G6 已派**（#4 P6 死代码门）：先调研工具（**查 go.mod 是否已有 x/tools/staticcheck，新增依赖须停下报告不擅自加**）→ 门本体＋golden 白名单 → **负例自测（须证明「只打印不拒绝」时自测红）** → 接 lint.sh → P6 接锚。**明禁手填 killed／删谓词消 error／硬造假门**。

**当前并发五单**：06.R14v（验证五项＋全量复跑＋六红项归属）、F5（删 scratch 绕法）、S4a（CommitOrder 样例＋红线）、G6（死代码门）、D4（spec 齐套＋锁步）。**写集互不相交**（assembly/runtime 测试跑 vs scripts/fold-cold-root vs examples/contract＋atomic_store doc vs scripts/gates 新文件 vs openspec/specs＋docs）。

**待 06.R14v 收口即派**：**10.R8b 三接口收口**（用户裁定的是「共享叶子统一端口」这一方向，见 R85；**落点 `api/durable`、取 3 方法、净减 2 接口是编排者据语义归属推导的执行判断，非用户原话**——R172 勘误，原写「用户已裁」扩大了裁决面）＋ **bootContext 死字段删除**（两者写集相交于 assembly，须合单或串行）。**随后**：ChainSource 样例、DoD 真跑、09.R2/R3、**R35 第二轮**、F1–F4。

**⇒ 纯度仍未完全落实，目标未达成**：#1/#3/#5/#6 未闭，#4 在跑，#2 在跑，#7 在跑，#8 剩 3 条待 #1。

### R133 F5 收口：scratch 绕法已删（纯度缺口 #2 闭合）（2026-11-17）

**前提核实通过**：`openJournalReadLog`→`openJournalLogFace(dir,false)` 跳过 MkdirAll、`O_RDONLY` 无 `O_CREATE`、torn 不 Truncate；`openJournalReadAddress` 索引缺失/无可用表头→`wholeLog` 整篇重放进自有 window 不写不改；**`ErrJournalAddressUnstated` 全仓零命中⇒拒绝路径已不存在**；`journalAddress.close→flush(true)` 在 `staged==0` 时首行返回，读视图关闭不落字节。夹具实证 hot slots `version=1`（build 为 2）、cold 无 slots/order ⇒ 两层都走 wholeLog。
**删除面**：`os.MkdirTemp`/`os.RemoveAll`/scratch 拷贝/`copyLog`/`io` 导入/假注释全清（`grep -rn "scratch\|copyLog" scripts/fold-cold-root/` exit 1）。新取数＝`OpenAtomicStore(WriteRights:false)`＋`Read(Fetch{})`，注释放当前机理。
**`ErrMigrateSourceUnchanged` 语义存续**：「拷贝期间源变动」→「**读期间源变动**」（open→read 段前后 `os.Stat` 比 size/mtime），仍对仍在写入的源具名拒。**该检查改前改后均无单测覆盖**（无确定性并发写者）⇒ **登记为未闭合测试面**。
**源根零改写显式断言已补强**：`snapshotRoot` 由「仅文件」扩为**目录项集合（含目录）＋size/mtime/sha256**；新增第 12 测 `TestFoldRootReadsARootWhoseIndexStatesNoWatermark`（旧版本 slots 头→先断言 `IndexUnstatedNotice()` 非空证明走退化解链→plan 与索引完好时逐项相同＋assertUnchanged）。
**真实 CLI 凭据**：干跑 exit 0（`16 event(s), 12 original key(s), 49 key(s) to state`，commit 1..16，源根 11 项完全一致，目标未创建）；`-restore` exit 0，折回根经独立只读探针取 49 键对 `fixture-meta.json` **49/49 逐键 sha256 相等**、idx 提交序 **1..16 与 commit_order 逐一相等**。
**十条验证全 0**：build/vet/`-count=1`（12 顶层＋2 子＝14 RUN）/`-count=3`/CLI 干跑/`-restore`/gofmt/comment_policy/词汇门/package-layout。**既有 11 测全保未回退**。

**纯度缺口进度**：#2 **闭合**；#4/#7 在跑（G6/D4）；#6 部分在跑（S4a CommitOrder）；**#1/#3/#5 未闭**（#1/#3 待 06.R14v 收口解编译面冲突，#5 待 scripts 面与全量复跑释放）。

**当前并发四单**：06.R14v、S4a、G6、D4。**06.R14v 收口即派 10.R8b＋bootContext 删除（合单或串行，写集相交于 assembly）**。

### R134 S4a 收口（concept 4→3）；10.R8b 三接口收口已派（最大纯度项）（2026-11-17）

**S4a 凭据**：新建 `examples/contract/memory_ledger/CommitOrder_example.go`（85 行，路径由门 `example_path()` 机械算得并用门自身函数实跑复核）；包名 `memoryledgerexample`，形态照 `runtime_state/FactLog_example.go`＋同目录 `LifecycleStore_example.go`（`契约:` 索引行＋`var _ ledger.CommitOrder = (*committedLog)(nil)` 自证＋`func ExampleCommitOrder()`）。内容真展示契约：五条提交含删键，全量走＝序新→旧每键一次、第二趟首键即停。
- **正锚非空转实证**：`-overlay` 装成测试跑 → PASS，探针打印实得 `[task:1=second task:2=only] [task:1]`（序/去重/删键跳过/止于 false 四项同时成立）。**负锚有牙**：把删键改成有值 ⇒ FAIL 原文 `want: [task:1=second task:2=only] [task:1]`。
- **doc 红线 4 条**（3 MUST＋1 MUST NOT，门双计口径实测 `must=4, must_not=1`，**留 1 行余量未用满 6**），四条均由 `journal.go:1763/1842`＋`eachNewestFirstValue` 实现实证（seen 去重、deletes 先入 seen、keep=false 返 nil、visit err 上返、缺值报 `ErrJournalCorrupt`），**非未验证断言**。
- **concept 4→3 error**，`CommitOrder` 零命中；剩 `AttentionFactLog` S4＋S1、`ChainSource` S4 **悉归 10.R8b**。
- 自决接受：同目录 `doc.go` 面清单补一项；重生成 `docs/api/` **只拷自己两份**（未替他人写 `fold-cold-root.md`，避免互踩）。

**⚠️ 编排者调整次序并说明理由**：原裁「10.R8b 待 06.R14v 收口解编译面冲突」。**现提前派 10.R8b**，理由：①它是**用户已裁的最大纯度项**，一次清掉剩余 3 条 concept error；②冻结序列要求**每步可编译**，故中间态短暂；③06.R14v 已被明令「编译撞中间态须读原文判归属」，不会误修。**代价是 06.R14v 可能报若干归属他单的编译红，须在其报告里甄别**。**bootContext 删除仍待 10.R8b 收口**（两者写集相交于 assembly）。

**当前并发四单**：06.R14v（验证五项＋全量复跑＋六红项归属）、G6（P6 死代码门＋负例自测）、D4（config-liveness 前置齐套＋doc 锁步＋陈旧标识符）、**10.R8b（三接口收口 `api/durable`）**。

**纯度缺口进度**：#2 闭合（F5）；#6 部分闭合（CommitOrder 样例已落，ChainSource 随 10.R8b）；#8 剩 3 条随 10.R8b 消失；**#1 在跑（10.R8b）**；#4 在跑（G6）；#7 在跑（D4）；**#3 bootContext、#5 DoD 真跑 未派**。

**门禁现态（编排者亲跑）**：build 0；vocabulary 0；concept 3 error（归 10.R8b）；doc_budget 22159（+6，D4 在压）；dod_coverage 红（归 #5）。

### R135 纯度落实复核（硬证据）：本征 O(历史) 确实只由索取者付费（2026-11-17）

**用户目标要求「思考是否不考虑实现成本只考虑架构优雅纯粹性落实执行」⇒ 编排者对已落地形态做消费时机复核，结论：已如实落地，非把成本挪到别处掩盖。**

- **`HistoryTaskIDs`（本征 O(历史)）生产消费者只有 `assembly/read.go:499` 一处**＝查询通道；其余命中全为测试（`symptom_anchor_recovery_test.go:362/539/575`、`symptom_anchor_longrun_test.go:105`、`durable_seat_test.go:808`）／注释／定义（`durable.go:1076/1093/1102`）。⇒ **「O(历史) 由索取历史的查询付费、boot 永不付」的声明与代码一致**。
- **`TaskIDs()`（有界面＝开放项∪tail）生产消费者 5 处全在有界面**：boot `run.go:1107`（restoreChains）、日志行 `checkpoint.go:877`、收尾 `host_attention.go:123`／`invoke.go:688`、查询 `read.go:501`。**无一处把全量枚举拉回 boot**。
- **`checkpoint.go:231` 的 `settledAtCut()` 跳过在位**，`Settled` 字段在该文件**零命中** ⇒ **已结项确实一条不写**（与结构锚「8/32/128/512 已结深度 body 逐字节相等」互证）。

**⇒ 纯度判据逐条对照**：①链唯一真相 ✓（已结态只留链上）②派生载体不迫使全量读 ✓（boot 三界全 0、走查比 1.00–1.03）③无第二真相 ✓（checkpoint 不再物化已结态；cut 标量未重复添加，R124）④更少特异化代码 ✓（per-task coverage map 已由统一 cut 取代）⑤本征成本归索取者并显式声明 ✓（本条复核）。**剩余纯度缺口是 #1 重复契约（在跑）、#3 死字段、#4 门盲区（在跑）、#5 DoD 自证、#6 ChainSource 样例**，非成本形态问题。

### R136 10.R8b 在冻结序列第①步；D5 派出闭合 DoD 注册侧；五单并发（2026-11-17）

**编排者亲核现场**：`go build ./...` **0**；**新叶 `api/durable/fact_log.go` 已建**（`type FactLog interface` 在 :24），**旧三接口仍在**（`runtime/state/durable.go:33`、`governance/denial.go:31`、`approval/attention_log.go:35`）⇒ **正是冻结序列第①步「建叶＋登记」的预期中间态**（先建后改指再删，不得先删声明）。G6 死代码门文件尚未落盘、D4 doc_budget 仍 22159 ⇒ 两单仍在途。

**D5 已派（#5 的可派部分）**：DoD 变异**注册侧**——按 schema 注册 P0/P1/P2/P3/P5/P6/P7/P8 八个（每个须 grep 实证替换目标在树存在、**P1 须对存活的 `CommitFact`/`ErrContentConflict` 而非已删冷档**、**形状/文本锚不用行号**）；退役 `MUT-DOD-P4-cold-unbounded`（替换目标已随切面族删除）；P4 `not_killed` 自证判决按 schema 处置。
- **写集与 G6 不相交**：D5 只写 `scripts/gates/check_event_integrity_mutations.py` 的 DoD 条目；G6 写 lint.sh／新门文件／dod_predicates.json。**明禁 D5 碰 dod_predicates.json**。
- **明禁三件**：手填 killed、假造锚名、删谓词消 error。**本单不做真跑**，「已杀死」侧留后续单（须待编译面释放）。
- **该文件已有前序单大量未提交改动** ⇒ 明令只改 DoD 条目、改前后逐 hunk 核对 delta。

**当前并发五单**：06.R14v（验证五项＋全量复跑＋六红项归属）、G6（P6 死代码门＋负例自测）、D4（config-liveness 前置齐套＋doc 锁步＋陈旧标识符）、10.R8b（三接口收口）、D5（DoD 注册侧）。

**仍未派**：**#3 bootContext 死字段删除**（待 10.R8b 收口，写集相交于 assembly）；**#5 真跑侧**（待编译面释放）；**#6 ChainSource 样例**（随 10.R8b）；**R35 第二轮**（待本批修复落地）；09.R2/R3、F1–F4。

**⇒ 目标未达成**：纯度 #1/#3/#4/#5/#6 未闭，验证五项未证，R35 第二轮未做，F1–F4 未做。

### R137 D4 收口（纯度 #7 闭合，doc_budget 绿且锁步）；10.R8b 在第④步预期中间态（2026-11-17）

**D4 凭据**：`config-liveness` 的「重启对账不漂移」req 加冷档前置（句式逐字照 delivery-closure），新增 Scenario 映射 fold-cold-root 命令，**存活判据未弱化**。**再枚举确认须改者仅此一处**：S2 七项复核维持原判，另查 12 处未入册者均类外（async-task-execution:275/280、event-segment-store:44/81/85、partition-local-storage-access:25、session-fact-lifecycle:77-81、example-file-memory-wiring:7、bounded-trajectory-capture:5 走 SwapDir 文件、recall-protocol:113 未命中须报告、event-key-visibility:31、storage-lifecycle-completion:62、attention-primitive:48、approval-attention:65）。
- **陈旧标识符四处修正（真话漂移）**：`memory-architecture.md:40/46/87` `kvBatch`→**`Batch`**（`atomic_store.go:36`，同页 141 行自述「三键的同一个 Batch」）；`:147/166` `commitOrder`→**`CommitOrder`**（atomic_store.go:83）；`event-flow.md:21/25`＋`GLOSSARY.md:21` `recordMapped`→**`foldMapped`**（assembly/facts.go:479）；`event-flow.md:17` `RecycleCount`→**`RecycleIdle`**（runtime/agent/recycle.go:59）。
- **⚠️ 反驳编排者一处误判，成立**：`OPERATION.md:130` 的 `CiWorkflowTest` **不陈旧**——`scripts/gates/test_check_runtime_workflow.py:1077 class CiWorkflowTest` 真实存在。**编排者 R131 把它列入陈旧清单是错的，已纠**（教训：列「陈旧标识符」前须逐个 grep 源码，不得凭印象）。
- **doc_budget 绿且锁步**：压 `url-fetcher/TESTING.md` §优势对比 **−16 行**（与 `UPGRADE.md` §核心改进＋§实际效果对比逐项重述同一事实，与 D3b 同文件「未来改进→UPGRADE」同型；**未碰 D3b 已压两处**）⇒ **measured 22149＝baseline 22149，exit 0**；跑 `--tighten`（22153→22149，净减 4，机械拒绝上升），**未用 `--update-baseline`**。**⇒ 纯度 #7 闭合，doc 面全绿。**
- 五门全 0：check-openspec / spec_hygiene（279 未增）/ doc_links / doc_budget / vocabulary。

**10.R8b 进度（编排者亲核）**：`api/durable/fact_log.go` 已建；**旧三接口已全删**（`type GovernanceEventLog interface`／`type AttentionFactLog interface` 全仓零命中）；消费者已改指（`runtime/state/durable.go:158/221/237` 用 `durable.FactLog`）。**`go build ./...` 现红，但红面仅限第④步样例**：`examples/contract/features_evolution_governance/GovernanceEventLog_example.go:59`、`runtime_state/FactLog_example.go:52`、`runtime_task_approval/AttentionFactLog_example.go:47` 三处 undefined＋assembly ⇒ **正是冻结序列「③删接口→④样例搬迁」之间的预期中间态，不干预**。

**当前并发四单**：06.R14v、G6（死代码门）、10.R8b（端口收口，第④步）、D5（DoD 注册侧）。**D4 已收**。

**纯度缺口进度**：#2 闭合、#7 闭合、#6 部分（CommitOrder 已落）；**#1 在跑（第④步）**、#4 在跑、#5 注册侧在跑；**#3 bootContext 未派（待 10.R8b）**。

### R138 编排者亲收 S1 债务条目合并：concept 2→1，自测 OK（2026-11-17）

**10.R8b 进度**：生产面已完（旧三接口全删、消费者改指 `durable.FactLog`），**旧名只剩 3 个 example 文件**（第④步样例搬迁在途）。`AttentionFactLog` 的 S4＋S1 两条 error **随端口删除消失**。

**编排者亲收（10.R8b 被明禁改门脚本，归编排者路由）**：`scripts/gates/check_concept_budget.py` 的 S1 BASELINE **删两条失效＋增一条统一**：
- 删 `("iface-methods","features/evolution/governance/denial.go:GovernanceEventLog",3)`（端口已删）
- 删 `("iface-methods","runtime/state/durable.go:FactLog",4)`（已迁 api/durable 且降为 3 方法）
- 增 **`("iface-methods","api/durable/fact_log.go:FactLog",3)`**，理由合并两域：一份耐久面三动作（写入一事实／前缀回放／同步屏障），拆窄口会让前缀回放绕开 Sync 屏障另立读路；两个生产实现者（`ledger.TierFacts`、`assembly.memoryFactLog`）在冻结包边下结构满足；**Get 生产零调用方故删**。
- **这是「按名抬升/合并」协议的正确用法**（R83/R85）：端口**确已存活且裁定**，非待撤者，故可登条目；理由入注释可追溯。

**验证**：`check_concept_budget` **2→1 error**（只剩 `ChainSource` S4 样例缺，10.R8b 白名单含 examples 已令其补）；**`test_check_concept_budget.py` exit 0 OK**（门自测未被弄红）。

**concept 收敛轨迹**：10 → 5（G4 绑定＋抬升）→ 4（S2 Settled 自解）→ 3（S4a CommitOrder 样例）→ 2（10.R8b 删 AttentionFactLog）→ **1**（本条合并）。

**当前并发四单**：06.R14v、G6（死代码门文件已建）、10.R8b（第④步）、D5（DoD 注册侧，MUT-DOD 计数仍 1 未落）。

### R139 ★纯度 #1/#3 闭合；G6 门查出 24 条真死生产者（编排者怀疑被证伪）；D5 注册侧落地（2026-11-17）

**① 10.R8b 收口（纯度 #1 闭合）**：`go build ./...` **0**；**`GovernanceEventLog`/`AttentionFactLog` 全仓零命中**（含 examples）⇒ 三接口收口为 `api/durable.FactLog`（3 方法）**零兼容别名**。concept 剩 1 error（`ChainSource` 样例）。
**② 编排者亲收 bootContext（纯度 #3 闭合）**：确证只有声明(:323)＋赋值(:537)＋注释(:321)、**零读者** ⇒ 三处全删，`grep bootContext` **全仓 0**，build/vet 0。
**③ G6 死代码门建成，编排者怀疑被证伪**：门按**可达性**而非提及计数（正确——切面族「互相调用故有引用、外部零引用」）。编排者曾疑 `openJournalReadLog` 是假阳性（因 F5 刚改读面），**grep 确证其零调用方**：F5 走 `OpenAtomicStore(WriteRights:false)`→`openJournalLogFace`，**不经此 wrapper**，它是面拆分重构后的死壳。`segmentEventSlot` 同样零调用；`MigrateArchive` 只被 `skeleton_assert.go:335` 自证断言引用（**门正确排除 skeleton_assert 为消费者**）。⇒ **门无假阳性，24 条成立**。
- G6 正确拒绝三件：**加 go.mod 依赖**（`deadcode` 对无 main 的库仓把全部导出符号判死）、**假造 P6 锚名**（`check_dod_coverage.py:224-251` 的 `anchor_body` 只扫 `*_test.go` 的 `func <name>(`，Python 门无法被 `anchor.test` 命名）、**用「已知死生产者基线」放宽**。
- **负例自测降级证明齐全**：把 `return 1` 改 `return 0`（只打印）⇒ 自测红 **7/15**，原文 `FAIL a family whose members only call each other is red: want red naming 'alpha/left has no consumer', got exit 0`；还原 15/15。含 fail-closed 用例。
- **两项自决编排者接受**：①测试算消费者（否则报 247 条测试专属 helper，而那已由 `check_product_surface` 的 38 条冻结钩子治理，避免双重报告）；②扫描面取 lint 产品面八目录（`internal` 只含测试，明示不静默跳过）。
- **P6 接锚阻塞须裁**：建议 A 扩 `check_dod_coverage.py` 的 anchor schema 收 `anchor.gate/selftest`（1 文件）／B 加 .go 包装。**编排者倾向 A**（门锚本就该能指门，只扫 _test.go 是 schema 缺口），归后续单。

**④ D5 注册侧落地**：6/8 注册（P0/P1/P2/P3/P7/P8，patch 锚全树 exactly-1 逐个 count 实证；**P1 已改绑存活 `ErrContentConflict`**）；**P4 退役**（schema 无 disabled/retired 态，故删条目＋就地注释记理由）；**P5/P6 正确停手**（anchor 为空，enabled 需真 Go 锚、pending 违自测 `pending==[]`，注册即须造锚名）。**dod_coverage 20→15 gap**。
- **⚠️ D5 查出 checker 真缺陷**：`check_dod_coverage.py:482` 用 `must_assert` 槽（**slug**）比 evidence 的 `expect_assertion`（**字面量**）⇒ **真跑必报「kill belongs to another assertion」**。**须修口径，归后续单**（否则真跑侧永远无法闭合）。
- **站点仍用行号是 schema 强制**（`assertion_site_problem` 对空站点直接判红）⇒「形状/文本锚」须改 checker，**与 R118 的漂移教训同源，归同一单**。
- `mutation_verdicts[0]`（P4 `not_killed` 自证）在 **golden**，D5 未动 ⇒ **须 golden 持有者随绑定同批退役**。

**⑤ G7 已派**：删 24 条真死生产者（逐条 grep 复核→删→级联迭代到门 exit 0），**明禁加豁免转绿**、明禁删 `MigrateShardedRoot`（`scripts/migrate-kv/main.go:24` 活消费者）、若发现假阳性或未接线目标须具名停下。

**纯度缺口进度**：**#1/#2/#3/#7 闭合**；#4 门已建但 P6 接锚阻塞＋24 条待删（G7 在跑）；#5 注册侧闭合、真跑侧待 checker 修口径；#6 剩 ChainSource 样例；#8 concept 剩 1 条。

**当前并发**：06.R14v、G7（＋10.R8b/D5/G6 已收）。**待派**：ChainSource 样例、dod checker 口径修（:482 slug vs 字面量＋行号锚改形状）、P6 anchor schema 扩、真跑侧、R35 第二轮、09.R2/R3、F1–F4。

### R140 10.R8b 撞限但①–⑤全落地（编排者亲验）；G8/S4b 派出收口 concept 与 DoD 门自身缺陷（2026-11-17）

**10.R8b 第十三次撞限，但编排者亲核步骤①–⑤全落地**：`go build ./...` **0**；`api/durable` **已登记 packages＋edges**（JSON 实证 True/True）；棘轮按名更新 **new_max 57／exported_interfaces_max 85**（＝G4 抬到 87 后减 2 个被删接口，**与 R98 预测的净减 2 一致**）；`check_feature_boundary`／`check_plugin_mounting`／`check_assembly_symbols` **全 0**；旧名全仓零残留。**只剩第⑥步的 `ChainSource` 样例**（concept 唯一 error）。

**G8 已派（DoD 门自身三缺陷，纯 Python 零编译，与 G7 的 .go 写集不相交）**：
1. **`:482` slug 比字面量**（致命：真跑必报「kill belongs to another assertion」⇒ **不修则真跑侧永远无法闭合**）。须先读既有 evidence 与 registry 实际形态定口径，**两种口径都有既存数据支持则请编排者裁**。
2. **站点锚强制行号** ⇒ 改形状/文本锚，**保留空站点判红**（不得放宽成可空）。
3. **anchor schema 只扫 `_test.go`** ⇒ 扩收 `anchor.gate`/`anchor.selftest`（**编排者裁定：门锚本就该能指门，只扫 _test.go 是 schema 缺口**），**并给 P6 接真门锚**；**假门名须判红**（负例）。
4. **P4 `not_killed` 自证判决随绑定同批退役**（在 golden，本单授权改）。
5. 门自测同步更新＋**降级证明**（拒绝改只打印时自测须红）。

**S4b 已派（concept 归零）**：补 `examples/contract/runtime_task/ChainSource_example.go`（路径由门 `example_path()` 算得并实跑复核）＋`ChainSource` doc 红线（**先数 must/must_not 与上限，不得超预算**，R98 教训）＋同目录 doc.go 面清单＋docs/api **只拷自己那几份**（避免替在途的 G7 落盘）。

**当前并发四单**：06.R14v（验证五项＋全量复跑＋六红项归属）、G7（删 24 条死生产者）、G8（DoD 门三缺陷）、S4b（ChainSource 样例）。

**纯度缺口进度**：**#1/#2/#3/#7 闭合**；#4 门已建＋24 条在删＋P6 接锚在修；#5 注册侧闭合、checker 口径在修、真跑侧待；#6 剩 ChainSource 在跑；#8 concept 剩 1 条在跑。**⇒ 全部 8 项均已有单在跑或已闭合，无未派项。**

**随后**：DoD 真跑侧（待 G8 修好口径）→ **R35 第二轮独立拷问** → 09.R2 校准／09.R3 全矩阵 → F1–F4 → **提交授权停点**。

### R141 整理计划：一级 tasks.md 域账重写（原引叶号已作废）；同能力多入口审查＝分层委托非重复（2026-11-17）

**① 账目脱节已修**：一级 `tasks.md` 原 **0 勾／12 空**，且域跟踪行仍引**已作废叶号**（`06.R1–R2`／`08.R1–R2` 随退位反转作废、`10.R1–R4` 已被 R5–R8 取代）；06 域实际已做到 R13 但账上只 3 勾。**按「裁决取代后文档改写一致」重写域账**（标勘误、历史证据保留、**未跑不勾**）：
- **[x] 06/08**（反转＋三层去物化＋派生态真水位化＋切面族删除＋外部折回脚本）——凭据：退位 27 符号与切面族八符号零命中、boot 三锚 PASS 三界全 0、长跑锚 PASS 23.0s、结构锚逐字节相等、折回 12/12 测＋49/49 sha256。
- **[x] 05**（尾谓词接缝 `ledger.CommitOrder`，三冻结锚 ×10）。
- **[x] 10**（R5 spec 删除／R6 系列文档／R7 golden／R8a+R8b 端口收口）——凭据：旧三接口名零命中、packages＋edges 已登记、三架构门 0、三处 spec 前置齐套、doc_budget 22149 绿锁步、vocabulary 0、**死代码门 exit 0（24 条已删）**。
- **[ ] 09**（R4 已落，R2 校准／R3 全矩阵待）、**[ ] 13**（R2/R3 待，DoD 真跑待 G8）、**[ ] 01/02/03/04/07/11/12**（本轮未重开实施，**其勾选须由 F1 逐项对账确认，不得由本重整推导为已完成**；07.R2 治理锚重述已由 A1 落地）。

**② 同能力多入口审查（纯度复核，结论：无违规）**：ledger 序走查有两条 arity 链（键版／键+值版），**各有真消费者**——`segment_store.go:1167` 走键版（A1 修法：只对 idx 键点读值）、`:1195` 走键+值版。每层只加自己的关注点（addr: span→value；log: addr 访问；kv: 引擎面；store: 事件索引解码），**底层共用 `eachNewestFirst`，无重复机件** ⇒ **是分层委托不是重复契约**，R35 的驳回成立。

**③ G7 收口确证**：`check_dead_producers` **exit 0、0 命中** ⇒ 24 条真死生产者全删，**纯度 #4 删除面闭合**。build 0、doc_budget 0、vocabulary 0。

**当前并发三单**：06.R14v、G8（DoD 门三缺陷）、S4b（ChainSource 样例）。**待派**：DoD 真跑侧（待 G8）、**R35 第二轮**（待本批落地）、09.R2/R3、F1–F4。

### R142 ⚠️⚠️ 撤回「两症状锚全绿」结论：06.R14v 查出 ABBA 死锁＋长跑不可复现＋归属误判＋(c) 生产消费者未跟上（2026-11-17）

**编排者必须撤回 R127/R139/R141 的「两条症状直译锚全绿」与「既红」归因。06.R14v 独立验证（只跑不改，变异按字节还原）推翻四条**：

**① ABBA 死锁（最严重，挂起整套）**：走查持 `journalKV.mu` 回调 `GetEvent` 求 `store.mu`；checkpoint filer 持 `store.mu`（GetEvent:869）求 `journalKV.mu`（journal_kv.go:66）⇒ **锁序反转**。`go test ./assembly/ -count=1` **600.384s 超时 panic**，`TestRestartExecutesTheChainJudgmentOnce` 挂 **7m49s**（HEAD 同用例 6.778s PASS）。**这是本变更引入的真死锁，不是负载问题。**

**② 长跑锚 `-count=1 → 0` 不可复现**：两次独立复跑各阶段比值——
| 阶段 | run1 | run2 |
|---|---|---|
| **boot** | 12012/14514＝**1.208** | 10841/16383＝**1.511 红** |
| commits | 0.888 | 1.055 |
| settling | 0.862 | 1.096 |
| sessions | 0.963 | 1.015 |
| frame | 0.823 | 0.965 |
| closing | 0.985 | 1.150 |
| total | 1.068 | 1.252 |
boot 十年期中位数 run1 10.7→14.1ms、run2 12.5→15.6ms（**单调增长**）⇒ **boot 阶段残余真实增长，编排者据单次 run 判「达标」是错的**。**compress 侧确已修好**（sessions/frame 两 run 均 ≤1.02），**残差在 boot**。

**③ 归属误判（编排者自误）**：干净 worktree `9547faca` 对照——**HEAD 同 8 用例批次 EXIT=0／74.9s**，`-list` 确认 8 名全在 ⇒ **assembly 七红＋挂起全部本变更引入**（`AnchoredBootMatchesFullBoot` 亦非既有，R124 的 A/B 未覆盖 index-walk 重写）。**「两枚既红」实为工作树新增红**：HEAD 下 runtime/task／runtime/state **包全绿**。包级：HEAD 仅 `execution`／`host` 红；工作树新增 `acceptance/capabilities/config/history/inspection` 红。

**④ (c) 水位化的生产消费者未跟上（真功能回归）**：`host/send_reentry_restart_test.go:97` `send_denied … the task table holds no record` ⇒ **重入仍按任务表判，未按身份读链**。另 `history/symptom_anchor_midband_test.go:195` `recall_get("evt_00-1-plan")=Found:false Reason:missing` 新症状锚红。

**⑤ 变异证明与夹具核验通过（正面）**：compress 修法（`owedJudgments` 跳过已答判决）**有牙**——删该跳过 ⇒ 结构锚红 `the 4 answered judgment(s) state the body 1996 byte(s), want nothing`＋长跑 sessions/frame 红；还原 `shasum -c` 4/4 OK。**夹具重述无弱化、五处增强**（journal 面改「闭合根==0＋降级根==整 log」、index 面由 2× 改**恰 0**＋无座根必须>0、wallClock 8× 收 2×、新增 walk 三面界＋正负锚）。四枚契约锚 ×10 PASS（包路径已核非空转）。

**⑥ 环境告警（影响归属判定）**：测量窗口内并发代理持续改写（git 变更文件 **212→260**，`runtime/state/durable.go` 15:52、`api/durable/fact_log.go` 15:44、`assembly/checkpoint.go` 15:54、`runtime/task/checkpoint.go` 15:58）⇒ **非 compress 侧红项须待并发落定后复跑定性**。**但 ABBA 死锁有 dump 定因，不受此影响。**

**⑦ 门禁现态**：comment_policy **1/1**（`runtime/state/durable.go:21` 裸 `// 契约:`，FactLog 文档段被并发删剩）；concept **1**（ChainSource，S4b 随后收口归零）；doc_budget 0；vocabulary 0；dod 15 gap；gen_godoc --check 1（governance 页陈旧残留）。

**⇒ 教训回写（编排者自误三处）**：①**据单次 `-count=1` 判「锚绿」不可靠**，须多次复跑取离散（本会话早已定此纪律却没对自己执行）；②**「既红」归因未经干净 worktree 对照就沿用**，违 R7/红项归属纪律；③**「症状锚绿」不等于「全套绿」**——全量复跑才暴露死锁与七红。**R127/R139/R141 的相应结论按此勘误。**

**即派（正确性优先）**：**G9 修 ABBA 死锁＋(c) 重入消费者＋boot 残差定位**。**待并发落定后**：全量复跑定性七红、R35 第二轮、09.R2/R3、F1–F4。**目标远未达成。**

### R143 G7 收口（24/24 删、0 假阳性、门 exit 0 未加豁免）；S4b 使 concept 归零；G10 派零残留收尾（2026-11-17）

**G7 凭据**：24 条逐条 grep 全仓（含他包/测试/examples/cli/bench，排除 vendor）确认只剩声明＋紧邻注释 ⇒ **全删，0 假阳性**。两条另有 `skeleton_assert` 自证断言已同批删。**级联 1 轮收敛**（首轮删后门即 exit 0，无第二轮命中）；随整族删除一并消失的 5 个私有方法门未单独报。生产者总数 3479→3447。**38 符号（含级联类型/常量）grep 全 0**。
- **关键复核**：`openJournalReadLog` 确为面拆分后空壳（读面实走 `openJournalLogFace(dir,false)`）；**`MigrateShardedRoot` 在 `memory/ledger/migrate.go`，`scripts/migrate-kv/main.go:24` 活消费者 ⇒ 未触碰**（编排者明禁生效）；`noticeSeatOf` 是 `noticeSeat` 的加锁包装，删包装不改行为。
- **`runtime/state/migrate.go` 580→70 行**整族清空（`ErrArchiveUnreadable`/`GapKind`/`MutationGap`/`MigrationReport`/`RollbackReport`/`MigrationGap`/`MigratedTask`/`MigratedOriginal`/`RollbackItem`/全部 archive 类型与常量）。连带删 import（`cli/opts.go` 的 `net/http`）、三处 skeleton_assert 块、3 处描述已删符号的注释。
- **门状态**：`check_dead_producers` **exit 0**（3447 生产函数全有消费者），**`golden/runtime/dead_producers.json` 零改动、未加任何豁免**。门自测 15/15。

**S4b 收口**：`ChainSource_example.go` 已建（门 `example_path()` 实跑复核），红线 0→3 条（门口径 must=3/must_not=1/total=4 ≤ 6），**`check_concept_budget` exit 0、0 error**（contract-surface 8→7）。初版 3 段落触 doc-not-brief，合并 2 段后归零。

**门禁现态（编排者亲跑）**：build 0；**concept 0／dead_producers 0／doc_budget 0／vocabulary 0／comment_policy 产品面 0**；**dod_coverage 15→13 gap**（G8 在修）；gen_godoc --check 红（stale governance 投影）。

**G7 上报三项，编排者裁定后派 G10**：
1. **4 个孤儿端口 `OriginalReader`/`OriginalMigrator`/`ArchiveTaskWriter`/`ArchiveTaskReader` ⇒ 裁定删（非接线）**。生产零消费者，唯一消费者是 examples 的编译期断言；**archive 迁移机制整族已删 ⇒ 这是机制删除后的残留端口，不是「未接线目标」**（未接线目标指有明确接线计划者，此处机制本身已废）。连同样例一并删。**须先 grep 复核，若有真生产消费者或存活实现则停下反驳。**
2. **stale 生成物投影** `docs/api/…features_evolution_governance.md`（对应目录已删，生成器只报不删）⇒ **手删**。
3. **`docs/wiki/rl/rl-architecture.md:64` 仍称已删的 `WithFeedbackEndpoint`** ⇒ 先 grep 源码确认真实现状再改写为真话。

**当前并发三单**：**G9（ABBA 死锁＋(c) 重入回归＋boot 残差，正确性最高优先）**、G8（DoD 门三缺陷）、G10（零残留收尾）。

**⚠️ 仍未闭合的阻断项（R142）**：ABBA 死锁（assembly 全量 600s 挂起）、长跑 boot 阶段 run2 1.511 破界、assembly 七红＋挂起全部本变更引入、(c) 重入消费者未跟上、`history/symptom_anchor_midband_test.go:195` 新症状锚红。**这些未修前不得声称达成。**

### R144 G8 收口（DoD 门三缺陷全修，dod 15→13）；G11 派消行号漂移（2026-11-17）

**G8 凭据（取证扎实）**：
- **① slug vs 字面量 blocker 确证**：`run_verdicts` 写 `expect_assertion`（registry 字面量，如 `want the 7 builtin plugins`），`check_mutation` 取 `anchor.must_assert`（slug，如 `every_builtin_feature_mounts_through_the_catalog`），二者直接比 ⇒ **逐对实测 8 对无一相等**，真跑必报 `belongs to another assertion`。**裁定以字面量为权威**（依据：`assertion_site_problem` 要求字面量是失败语句行子串、`_assertion_hit` 用字面量匹配；**slug 无法从字面量机械导出**）；`must_assert` 保留**非空硬门**，语义降为「判据给该断言起的名字」。**合单一真相，编排者采纳**。既有判决不受影响（仓内无 evidence.json，唯一 P4 判决本单退役；P0–P3/P7/P8 字面量各在锚函数 raw body 内 1 次，收紧后仍绿）。
- **② 定位锚改文本/形状锚**：字面量须落在**锚函数体内**（原为整文件，邻测试字面量也算）；run 须用 `test.name` 点名锚函数；**无 test 名＝空站点仍判红**。
- **③ schema 扩门锚＋P6 接真锚**：`anchor.gate`/`anchor.selftest` 须存在、须读到 `measure_token`、须有失败退出、selftest 须把退出码与 0 比较。P6 绑 `check_dead_producers.py`＋其自测。**负例 6 条全红**（不存在门路径/读不到量/只打印不失败/无名 selftest/selftest 缺失/selftest 不比退出码），并**封住「门锚借已注册变异白拿 kill」旁路**（判红 unattributable）。消费者锚：`ci.yml:109 bash scripts/lint.sh`→`lint.sh:150-151`。
- **④ P4 自证判决退役**：`mutation_verdicts`→`[]`（保留空数组守 schema 位），`stated only by this registry` error 消失。
- **⑤ 降级证明三类齐全**：selftest 不比退出码改 print ⇒ 红 `1/28 cases failed`；门不存在检查改 print ⇒ 红；字面量检查回退整文件 ⇒ 红 `a kill whose text another test of the same file states is red`。还原 28/28 exit 0。
- **dod 15→13 gap**；残留 6 条「no run records」＋P4/P5/P6 未注册（真跑/他单）。

**G8 上报待裁项，编排者裁定后派 G11**：`assertion_site_problem` 本体在 `check_event_integrity_mutations.py`（G8 禁区）仍强制行号，**D5 自测 4 条失败正是 `:1644/3810/4664/331` 各偏 1 行** ⇒ **裁定改形状锚 `<file>#<func>+<literal>`**（空站点照旧判红、字面量须在函数体内），**这是本会话两次被行号漂移击中的同一病类**。另 `MUT-DOD-P1` 夹具缺 `fact_commit_batch_test.go` 是**「未跟踪≠不存在」的假红**（自测按 `git ls-files` 复制）⇒ 须修自测口径读工作树真实文件，**不得删变异条目消红**。

**当前并发三单**：**G9（ABBA 死锁＋(c) 重入回归＋boot 残差，正确性最高优先）**、G10（4 孤儿端口＋stale 投影＋wiki 真话）、G11（站点形状锚＋自测口径）。

**门禁现态**：build 0；concept 0／dead_producers 0／doc_budget 0／vocabulary 0／comment_policy 产品面 0；dod 13 gap；gen_godoc 红（stale 投影，G10 在删）。**阻断项仍是 R142 五条，G9 在修。**

### R145 G10/G11 收口＋编排者复测；feedback 自证链立案派 G12；真跑侧硬依赖定明（2026-11-17）

**G10 收口，编排者独立复测（不采信汇报）**：`dead_producers` **EXIT=0**（3441 生产函数／32 包全有消费者）、`vocabulary` 0、`concept_budget` 0。R144 记的 dead_producers EXIT=1 是**G10 删除中途的瞬态**，非回归，撤销该疑虑。四端口（`OriginalReader`/`OriginalMigrator`/`ArchiveTaskWriter`/`ArchiveTaskReader`）＋`runtime/state/migrate.go` 整文件已删；`gen_godoc --check` 在 G10 时点 0，**编排者此刻复测 EXIT=1，差异只在 `memory__ledger.md`** ⇒ 归 G9 在飞改 `memory/ledger` 所致，**投影是共享面，待 G9 落地后由编排者统一重生成**，不派单抢写。

**G10 上报的 `CLI.feedback` 死面：编排者核实并收窄其范围**：
- **`diagnostics` 不是孤儿**——`cli/cli.go:173` 在 `start()` 内由 `assembly.DiagnosticSwitchesFromEnv()` 写入，env 是单一真相，删 `WithDiagnostics` 是**删第二真相，正确**。G10 若推及 diagnostics 即误判，此处勘误。
- **`feedback` 确是孤儿**，且证据比 G10 给的更硬：①`git grep -n WithFeedbackEndpoint 9547faca` ⇒ HEAD 上**零 Go 调用方**（仅自身声明＋godoc 投影＋wiki）；②`grep -rn "POST /feedback" --include='*.go' .` 全仓**只命中 `cli/cli.go:284`**；③`features/rl/feedback.go` **只有 `FeedbackAttribution` 结构声明，无 func、无 http.Handler** ⇒ **全仓不存在 feedback handler 生产者**。
- **裁定**：删选项正确（它本就是死选项），**残留是它身后那条自证链**——`cli/cli.go:68` 字段永不被写，`:276` Attribution 读回／`:283` nil 判／`:284` 挂载三处读的理由全由该字段自己制造（元规则二）。`go vet ./cli/ ./tests/integration/host/` = 0，无编译断裂，故是**静默不可达**而非显式失败。
- **G10 的 wiki 改写仍非真话**：新句「POST /feedback 仅在持有该处理器时挂载」暗示端点可用，而生产者根本不存在 ⇒ 立案纠真话。
- **派 G12**：白名单 `cli/cli.go`、`cli/opts.go`（仅若需）、`docs/api/…cli.md`（只回拷自己这一个）、`docs/wiki/rl/rl-architecture.md`、`examples/contract/cli/HostBridge_example.go`（仅若引用被删符号）＋确因本次删除需改的测试。**`assembly/**`、`features/**` 归 G9 独占，G12 只报跨包孤儿不删**（`assembly/host_diagnostics.go:171` 的 `Feedback *rl.FeedbackAttribution` 是首个候选）。

**G11 收口，三项自决编排者裁定**：
- **(a) 形状锚取 `<file>#<func>`、去掉建议里的 `+<literal>`——采纳**。理由正是单一真相：`expect_assertion` 已是字面量唯一登记处，站点再写一遍即第二真相；若另立「两字段须相等」规则则退化为自比，不立则为死数据。**编排者原建议含 literal 是错的，此处勘误。**
- **(b) 站点迁移取全量 20 条而非仅 DoD 名——采纳**：4 条漂移项恰非 DoD 名，只迁 DoD 则消漂移交付不可能达成。行为等价实证：新旧 `assertion_sites_of` 逐条对比，**16 条集合逐字节相同**，4 条 delta 各仅 N→N+1（1644→1645、3810→3811、4664→4665、331→332）。
- **(c) 保留「字面量须在失败构造内」为第四条红——采纳**（收紧非放宽）。
- **凭据**：D5 自测修前 exit 1（3 failures＋1 setUpClass ERROR，`Ran 43`，该 error 吞掉 4 用例）→ 修后 **exit 0，`Ran 48` OK**；降级证明（体定位 return 改 print）⇒ 红并具名，还原 `diff -q` 一致。`MUT-DOD-P1` 假红修为自测读工作树真实文件（`gate.source_files()`＋`copy_working_tree()`），**变异条目未删**。编排者复测：`test_check_dod_coverage` 28/28、`check_dod_coverage` 仍 **13 gap 未增**。
- **病类登记（本会话第三次）**：`git grep`／`git ls-files` **看不见未跟踪文件**。`TestFactCommitAnswersNewExistingConflictUncertain` 实在 `memory/ledger/fact_commit_batch_test.go`（未跟踪），编排者用 `git grep -ln "func …"` 得空即差点误判锚不存在，改 `grep -rln` 才命中。**凡判「零命中/不存在」必须用含未跟踪的全树 grep。**
- **台账勘误**：本文档 :2522「站点用行号是 schema 强制」已被 G11 取代，行号不再是 schema 要求；历史记述保留，以本条为准。

**DoD 真跑侧（13 gap）依赖定明——暂不派单**：
- 变异 runner `scripts/gates/check_event_integrity_mutations.py` **R26 合规**：`tempfile.mkdtemp` 副本树内替换、`restore_patch` 复原并校验、仓库工作树零写入零 index 变更、日志只写 `--out` 仓外目录 ⇒ **不与 G9/G12 写冲突**。
- **但硬依赖控制树全绿**（`control_tree_green`）：G9 的 ABBA 死锁未修前跑它必得假红。**故真跑侧排在 G9 之后，不得提前派。**
- 待办面已勘明：P4/P5/P6 三个变异**未注册**于冻结清单；**P5 既无 test 锚也无 gate 锚**（`ci_gate: openspec`，判据「主 spec 与代码陈述同一契约」）⇒ 须先给它绑一个真门锚；P0–P3/P7/P8 已注册但**无 run 记录**。九个锚测试编排者已逐个 grep 定位（P1 在未跟踪文件，见上）。
- **禁**：手填 killed、绑假锚、删谓词。`--replay` 只写 `evidence.replay.json`，机制上进不了 `--bundle`，**不得用回放冒充活运行证据**。

**当前并发两单**：**G9（ABBA 死锁＋(c) 重入回归＋boot 残差，正确性最高优先，仍在飞）**、**G12（feedback 自证链＋wiki 真话）**。

**⚠️ 阻断项仍未闭合（R142 五条＋本条新增）**：ABBA 死锁（编排者 90s 短超时复测仍挂，栈在 `assembly/checkpoint.go:651`）、长跑 boot run2 1.511 破界、assembly 七红＋挂起、(c) 重入消费者未跟上、`history/symptom_anchor_midband_test.go:195` 症状锚红；新增 `gen_godoc` 红（归 G9）、DoD 真跑侧 13 gap。**这些未修前不得声称达成，不得归档，不得提交。**

### R146 死锁转绿实证；assembly 十红四簇归因（两簇机理已确证）（2026-11-17）

**编排者亲测（非采信汇报）**：
- `go test ./assembly/ -run '^TestRestartExecutesTheChainJudgmentOnce$' -count=1 -timeout 45s` ⇒ **EXIT=0**。**ABBA 死锁已修**（R142/R144/R145 记的挂起撤销）。
- 锁序形态复核：`journal_kv.go:272-276`／`:295-299` 的「回调前 `k.mu.Unlock()`、`defer k.mu.Lock()`」写在**传给 `eachKeyNewestFirst*` 的闭包内**，故每次 visit 各自回锁，不是外层函数返回时才回锁 ⇒ **形态正确**，非「循环内 defer 累积」病。注释陈述与代码一致。
- `go test ./assembly/ -count=1 -timeout 300s` ⇒ **EXIT=1，255.078s 跑完，10 个 FAIL，无挂起无 panic**。原「七红＋挂起」的七是**被挂起截断的计数**，后半段从未跑到；现全量跑完暴露十条。**不得把 7→10 读成修复引入新红**，但亦不得声称已绿。

**十红四簇（逐条 file:line 取自实测日志）**：

**A 簇｜新根只读开档 ENOENT（3 条）**——`host_delivery_test.go:2793`（TestDeliveryDurableReadErrorSurfaced）、`run_test.go:834`（TestBootDoesNotReadOriginalShards）、`run_test.go:924`（TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice），另 G10 报的 `runtime/state` TestDurableCommitBarrier 两子测同症。
**机理已确证**：`memory/ledger/journal.go:589-593` `openJournalLogFace` 在 `writeRights=false` 时 flags 取 `os.O_RDONLY`，**不含 `O_CREATE`** ⇒ 日志文件尚不存在的新根开档即 `no such file or directory`。**待裁的设计问**：只读面遇空根应当「陈述空视图」还是「具名拒绝」？须逐个核这三条测试的原意图（`TestDeliveryDurableReadErrorSurfaced` 名字暗示它要的是**具名错误**而非 setup 失败），**不得一律改测试迁就实现，也不得一律加 O_CREATE 把只读面变成写面**。

**B 簇｜order 文件读取 EINVAL（2 条）**——`anchored_boot_parity_test.go:156`（TestAnchoredBootMatchesFullBoot，"count the filed products"）、`:312`（TestDefaultBootRebuildsFromTheAnchor，"count the indexed facts"），均 `read the order file of …/kv: invalid argument`。测试自助手直读 order.log，须核它用的偏移/长度是否仍合 `journalSlotVersion` 2 的布局。

**C 簇｜memory 档静默退回事件时间序（3 条，纯度级）**——`read_test.go:300`（GetParent(parent-a2)="" want "parent-a1"）、`read_test.go:486`（同形）、`run_test.go:628`（"ordered … by event time instead of commit sequence"）。三条日志均 `tier=memory`。
**机理已确证**：`memory/ledger/segment_store.go:1193` 用**可选类型断言** `if ordered, holds := s.kv.(CommitOrder); holds` 取能力，而 `EachCommittedNewestFirst` **只有 `journal_kv.go:291` 一个实现者**；memory 档的 kv 不实现 ⇒ 断言不成立即**静默走另一条排序路径**。
**勘误（R147 改写的就是本段）**：本段原写「可选能力＋静默兜底＝第二真相」，把**可选能力本身**判成缺陷，这是错的。`atomic_store.go:67-85` 契约原文明确裁定：「a key-ordered key-value engine keeps no such order and does not answer this capability at all, so a caller reads the answer through the type assertion instead of a method every engine carries」——**有些引擎本就不持有提交序这一quantity，故类型断言是语义单元上的真边界，不是疏漏**（元规则三）。契约原文同样是权威，判行为收窄是缺陷还是正确须先引它。
**待裁的只剩兜底语义**：断言不成立时消费者落到 `segment_store.go:1213` `publishedEntriesOf(ctx, statedPartitions(nil))`，这条路径答的**是不是同一个quantity**尚未取证——若它是「提交序的较低精度」则属退化读真相（可接受），若它是**事件时间序**则是另一个quantity冒充提交序（第二真相，须具名拒绝或显式声明）。**此簇归设计门，不得只修测试断言；取证派 G13，裁定归编排者。**

**D 簇｜恢复报告空／负对照失效（2 条）**——`host_delivery_test.go:2414`（TestRestoreFoldCounts，"recovery report = [], want the one chain rebuilt"）、`:2465`（TestRestartRecoveryPinsBeforeTrim，"the early-trim control kept the fact, so it did not exercise the defect"）。后者是**锚自带的负对照守卫在说话**：对照路径已被修好故不再复现缺陷 ⇒ 须重表达对照，**不是产品缺陷，也不得删守卫消红**。

**另**：`tests/integration/history` 的 `TestSymptomAnchorMidbandProcessRecalledByTicket` 编排者亲测仍 **EXIT=1**，`symptom_anchor_midband_test.go:195` `recall_get("evt_00-1-plan") = {Found:false Reason:missing}`，want 段折事实的已提交正文；`:153` 报 14 条目／2 段折产物／history summary=false。即 R142 第五条未闭合。

**派发约束**：A/B/C/D 四簇全落在 `assembly/**`、`memory/ledger/**`、`runtime/state/**`——**G9 独占面**，故**此刻不得并发派单**（R21/R22）。待 G9 战报落地后按簇派：C 簇须先出设计裁定（编排者裁，不伪 defer），A 簇须先核三条测试原意图，B/D 簇各成一单。

**当前并发两单**：G9（仍在飞）、G12（feedback 自证链）。**门禁现态**：build 0；concept 0／dead_producers 0／vocabulary 0；dod 13 gap；gen_godoc 红（`memory__ledger.md`，归 G9）；assembly 10 红；midband 症状锚红。**阻断未闭合，不得归档不得提交。**

### R147 C 簇自我勘误（契约原文是权威）；派 G13 只读取证，并发回到三单（2026-11-17）

**编排者自纠**：R146 把 `CommitOrder` 的**可选类型断言本身**判为纯度缺陷，越过了契约原文。`memory/ledger/atomic_store.go:67-85` 明写 key-ordered KV 引擎本就不持有提交序、故用类型断言而非人人必备的方法，并列 4 条 redline。**这是画在语义单元上的真边界**（元规则三：边界证据来自领域事实，不来自代码形态），不是疏漏。R146 该段已直接重写一致并标勘误，不另留矛盾文本。

**剩下的真问题只有兜底语义**：`segment_store.go:1213` 的 `publishedEntriesOf(ctx, statedPartitions(nil))` 答的是不是同一个quantity。判据已定：**若是提交序的较低精度 ⇒ 退化读真相，可接受；若是事件时间序冒充提交序 ⇒ 第二真相，须具名拒绝或显式声明它答的是别的量**。`run_test.go:628` 的失败原文已含 "by event time instead of commit sequence"，**倾向后者但未取证**，不得当已确认。

**派 G13（纯只读设计取证，与 G9 写面不冲突）**，六问：①memory 档默认引擎的真实类型与底层结构（从 `config.go:284`＋`OpenAtomicStore` 追到底；已提示 `kv_resident.go` 是 LRU 缓存、`guard_memory.go` 是 retention 保护，都不是引擎，防认错）；②它是否已持有提交落地顺序、能否纯读派生、成本 O(开放项) 还是 O(历史)；③若需新增结构，逐项对照四条纯度判据（链唯一真相／不得第二真相／更少特异化／本征 O(历史) 归索取者不摊进 boot），并给读写闭包与是否被迫全量常驻；④兜底路径实际按什么排序、与提交序是否同一quantity；⑤三条红测试的原意图与是否自带 tier 声明／负对照守卫（**不得建议改断言迁就实现**）；⑥`CommitOrder` 全部消费者与实现者枚举（**用含未跟踪的全树 grep**）。要求它给两条候选路径各自的代价与纯度后果、不替编排者裁、不造甲乙伪二分、明确列未查面、前提有错直接反驳。

**并发回升到三单**：G9（写 `assembly/**`＋`memory/ledger/**`＋`runtime/state/**`，死锁已绿、余簇在修）、G12（写 `cli/**`＋两处文档，feedback 自证链）、G13（只读 `memory/ledger` 设计取证）。**触碰集不相交**：G12 与 G9 无共同写文件；G13 零写入。A/B/D 三簇仍待 G9 落地后派，**此刻不得抢写**。

**门禁现态（编排者亲测）**：build 0；concept 0／dead_producers 0（3441 函数）／vocabulary 0；dod 13 gap；gen_godoc 红（`memory__ledger.md`，归 G9，待其落地后编排者统一重生成）；assembly 10 红四簇（R146）；midband 症状锚红。**死锁阻断已解除，其余未闭合，不得归档不得提交。**

### R148 G12 收口；G9 撞预算失败定性；C 簇裁定（序已在链上）；feedback 跨包孤儿范围裁定；独立词汇扫描（2026-11-17）

**G12 收口（编排者复核其归属实证）**：删 `cli/cli.go` 四处（`features/rl` import、`feedback` 字段、`Attribution()` 读回、`POST /feedback` 挂载）＋`handler()` godoc 去一句；`net/http` 保留（Serve/authenticate/handler 仍用）。wiki 改三行（60 标题四→三、62 路由清单、64 重写为真话：反馈提交面在本仓无实现，三计数无生产者，`DiagnosticTraining.Feedback` 全仓无写入点恒 nil）。
**归属实证扎实**：它把 `cli/cli.go` 临时换成 HEAD 版跑同 10 条 host 集成测 ⇒ 同样 10 FAIL；换回本版 ⇒ 同样 10 FAIL；失败文本全在存储/恢复面 ⇒ **非本单引入**，复原 byte-identical。`go test ./cli/...` = 0 但**零测试**（`[no test files]`），如实报不当绿。`dead_producers` 0（3445 函数）。

**G9 失败定性（不是实现失败）**：撞子代理回合预算上限（150）而死，末句「Located the boot residual precisely. Now the remaining verification.」。**编排者亲验其留下的树**：`go build ./...`=0、`go vet ./assembly/ ./memory/ledger/ ./runtime/state/`=0、死锁锚 `-timeout 45s`=**0**、`TRACE rebuild` 调试残留全仓**零命中**（已清）。⇒ **死锁修复已落地且树干净**，未收的是 A/B/D 三簇＋长跑残差验证。**按「harness failed ≠ 实现失败」处置：不重开死锁，派 G15 接续余簇。**

**C 簇裁定（G13 只读取证后编排者裁，不伪 defer）**：
- **决定性事实**：提交序这个量**本就写在链上**——每条索引值携带全店提交序号 `segmentIndexCarried.Commit`（`segment_store.go:650-655`），解码回 `indexPosition.commit`（`:941`），`:217-221` 注释明言 commit sequence orders every entry。兜底 `publishedEntriesOf` 按 partition↑/window↑/seq↑/key↑ 排（`:443-455`）再逆序（`:1218`）⇒ **它已读了含序号的真载体，只是按错字段排**。故这不是独立第二真相，是「读真载体、按错字段」。
- **裁定：保留可选能力与类型断言，把兜底改按 `indexPosition.commit` 降序**，并须满足 `CommitOrder` 的 4 条 redline（每键一次从新到旧／visit false 即停并返其错／答最新陈述的值／不在更旧提交处命名键）。零新增常驻结构、不造第二真相 ⇒ 合「派生不可信须退化读真相」与「更少特异化结构」。
- **路径一「让每个引擎都答以便删断言与兜底」被两个事实否掉**：rustviking（StorageFile 档）无实现；`atomic_store.go:71-73` 契约明裁键序引擎不答此能力。**故「修结构→删补偿」在此不适用**，不得给 memKV 加实现、不得把 CommitOrder 升为 AtomicStore 必备方法。
- **路径二「具名拒绝」单独不能收红**：三条红断言的是**消费者行为**（T1/T3 经 `windowChainRun` 默认 StorageMemory，`run_test.go:34`；T2 显式 `read_test.go:443`），拒绝只把静默错序变显式失败。且 `assembly/checkpoint.go:881` 的 `tailFactKeys` 已是「具名日志＋nil 全读」的具名降级先例（`:883-884`），段店兜底改拒绝会使 memory 档 GetParent/replay 直接不可用。
- **边界**：fact 键经 `TierFacts.Put`（`tier_facts.go:31`）直写引擎不经 commitSeq，值不带序号 ⇒ 派生序**覆盖不了 fact 键**，`checkpoint.go:881` 不受影响，不得顺手扩到 fact 键。
- **boot 成本须核**：`EachIndexNewestFirst` 另有消费者 `newestSessionCheckpointRef`（`checkpoint.go:918`）在 boot 路径上。G17 须证明 journal 档走能力路径不受影响、memory 档排序成本归谁，**若会把 O(n log n) 摊进耐久档 boot 则停下来报**。
- **派 G17**，白名单只有 `segment_store.go`＋`segment_store_test.go`；要求负锚（改回窗序须重新变红具名）＋`-count=5` 稳定性（排序改动涉及求值顺序）。

**feedback 跨包孤儿的范围裁定（不扩范围）**：G12 报 `assembly/host_diagnostics.go:171` `Feedback *rl.FeedbackAttribution` 零写入者、`features/rl/feedback.go:8` `FeedbackAttribution` 零生产者零消费者、`openspec/specs/feedback-binding/spec.md:26` 的 `POST /feedback` 场景无实现。
**编排者核 HEAD 后裁定：这些在 HEAD 就已死，非本变更残留**——HEAD 上 `WithFeedbackEndpoint` 零 Go 调用方，故 `c.feedback` 恒 nil，那条读回**在 HEAD 也从未写入过**该字段；handler 生产者在 HEAD 也不存在。本变更只删了一个死选项，**没有新造这些孤儿**。按 F1「不夹带其他变更」，**本单不删跨包孤儿、不改 feedback-binding 主 spec**；立案为**既有缺口**，归独立变更处置（when：本变更归档后；why：它先于本变更存在，删它须同时退役一条已接受的主 spec 条款，属另一个契约面裁定）。**这不是伪 defer：范围纪律是既有原则，且已给 when/why。**

**独立词汇与概念核查（门绿≠无违规，编排者自扫）**：
- 本轮新增 Go 注释 **2213 行**，扫泄漏形态（裁决引用／审计码／§N／变更叙述 used to/no longer/previously/本变更/原来现在）⇒ **仅 3 处命中「本次」，读原文全是领域语义**（「本次调用的结果正文」vs「别次调用」的结算归属），**零真泄漏**。
- 新增约束注释 **137 条**（必须/不可/不得/MUST/never/always）。
- `check_concept_budget`：**`baseline_stale: 0`** ⇒ 78 条 BASELINE 无指向已删结构的陈旧豁免（该门对 stale 只 warn 不 error，是盲区，故编排者另 grep 已删符号名复核：只命中 `:129` 解释合并理由的注释，非豁免行）。
- 现存 baselined warn 是**显式申报的债**非违规：S4 七条缺 canonical 范例、S3 三条 bare `ctx.Value`、S1/S2 若干超形状。**其中 `assembly/settle_turn.go:SettleWake` 实际 4 方法而 baseline 记 5 ⇒ 有 `--tighten` 只降锁步的空间**（登记为可用收紧，不在本轮实施，避免扩范围）。

**当前并发三单**：G14（DoD 真跑侧注册与绑锚，写 `golden/runtime/dod_predicates.json`＋`check_event_integrity_mutations.py`）、G15（A/B/D 三簇＋长跑残差，写 `journal.go`/`journal_kv.go`＋assembly 测试）、G17（C 簇兜底改提交序，写 `segment_store.go`）。**触碰集不相交**；G15 被明令禁碰 segment_store.go 与 C 簇三条测试，G17 被明令禁碰 assembly 与 journal.go。

**门禁现态**：build 0／vet 0／死锁锚 0；concept 0（stale 0）／dead_producers 0（3445）／vocabulary 0／doc_links 0；dod 13 gap（G14 在收注册侧）；gen_godoc 红（`memory__ledger.md`，待 G15 落地后编排者统一重生成）；assembly 10 红（G15 收 7、G17 收 3）；midband 症状锚红（**尚无单归属，待派**）。**不得归档不得提交。**

### R149 F2 完整门禁面勘成可执行清单（编排者只读实证，防「跳过当完成」）（2026-11-17）

**入口唯一性已核**：`scripts/lint.sh` 只是转发器（`exec bash "$here/gates/lint.sh" "$@"`），规则全在 `scripts/gates/lint.sh`；CI `test` job :109 跑的就是 `bash scripts/lint.sh` ⇒ **本地与 CI 同一命令，不存在两个口径**。

**lint.sh 内含门（逐条抄自源码，收尾须全绿）**：gofmt（三模块 POLICY_DIRS=`. examples/wechat-bot examples/cli-host`）／`go mod verify`／三模块 build／三模块 vet／comment_policy 棘轮／`check_doc_budget.py`／自测组（`test_check_runtime_*` discover、concept、doc_budget、product_surface、doc_links、feature_boundary、config_liveness、`test_verify_restart_prefix.py`、`test_race_check.sh`）／三挂点五对（schema_allowlist、assembly_symbols、durable_truth_registry、spec_hygiene、plugin_mounting，各先自测后判现树）／vocabulary 对／window_bounds 对／**dod_coverage 对**／**dead_producers 对**／runtime_plan（条件）／architecture policy／concept budget／architecture implementation／event-ownership（`go run ./scripts/codetools`）／scenarios contract（条件）／comment_policy 产品面零容忍（`-no-baseline assembly api runtime memory features tool internal cli`，**解析不到数字按 unparsed 拒收**）／openspec strict（CLI 存在才跑）／文本前缀解析禁（grep HasPrefix/TrimPrefix/strings.Split × .Content/.Summary/Meta[）／name-check／doc-refs／**gen_godoc --check**／proc-refs／tracked-hygiene／package-layout＋其负例自测／dotted-refs／commit-scope。

**⚠️ F2 硬依赖（现红即全量 lint 必红）**：`check_dod_coverage.py`（13 gap）与 `gen_godoc.sh --check`（`memory__ledger.md` 陈化）**都在 lint.sh 内**。故 **G14 注册侧＋九变异实跑＋存储面落地后的投影重生成，三者未齐前 `bash scripts/lint.sh` 不可能 exit 0**。不得用「只跑部分门绿」声称 F2 达成。

**CI 十一个 job（编排者逐个读源码确证）**：
| job | 实跑命令 | 规模 | 归属 |
|---|---|---|---|
| test | 三模块各 build/vet/`go test ./... -short -count=1`＋`GOMAXPROCS=1 go test ./tests/integration/execution/ -short -count=1`＋`bash scripts/lint.sh` | 需 tmux（缺则 exec/async 用例 skip，**skip 不算证据**） | 本地可跑 |
| race | `./scripts/gates/race_check.sh` 覆盖 22 个包树（含全部 tests/integration 子套）＋`go -C examples/wechat-bot test -race -count=1 -p 1 ./...` | 昂贵 | 本地可跑，耗时 |
| validators | `test_verify_restart_prefix.py`＋`test_race_check.sh` | 小 | 已在 lint.sh 内 |
| openspec | `./scripts/gates/check-openspec.sh`（CI 自装 `@fission-ai/openspec@1.2.0`） | 小 | **本地须先确认 CLI 是否安装**；未装则 lint.sh 如实标注跳过，**不得把「没装工具」当通过** |
| soak | `go test ./tests/integration/host -run '^TestSoakContinuity$' -count=1 -v -timeout 45m -args -rounds=30 -events-per-round=30` | 45m | CI 为主 |
| event-integrity-short | `run_runtime_scenarios.py --scope event-short --repeat-index $REPEAT_INDEX` | **matrix repeat-index 1..10＝十短** | CI 为主 |
| event-integrity-long | 同 runner `--scope event-long --seed $SEED --window $WINDOW --repeat 1 --timeout 6270 --generation-logs …` | **matrix seed[1,7,42]×window[256,32768]＝六长**，每 shard 104.5 分钟上限；带 `TAGENT_CALIBRATION_RUN_DIR` | CI 为主，**这是 09.R2 校准面** |
| event-integrity-mutations | `check_event_integrity_mutations.py --all --json --out …` | 控制树须先全绿 | **G14 之后可本地跑** |
| event-integrity-required | `needs:[short,long,mutations]`＋`check_runtime_evidence.py --bundle … --contract golden/runtime/event_integrity_scenarios.json` | 聚合 | 须三份同基线证据 |
| event-semantics-symptoms | `go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' -count=1 -json -timeout 600s` | 20m job 上限 | 锚在 `examples/wechat-bot/smoke_test.go`（已确认存在） |

**两处自我纠错（先疑后证，不凭印象）**：
- 疑「`event-semantics-symptoms` job 依赖外来未跟踪门 `check_event_semantics_symptoms.py`」⇒ **读 run 块否掉**：它实跑 bot 模块 smoke 症状锚，不调用该 Python 门。ci.yml 本变更**零改动**（`git diff --stat` 空），该 job 在 HEAD 已存在。
- 疑「无活动 task-graph 使两门 skip 属漏验」⇒ **核为设计上的无对象**：`ls openspec/changes/*/task-graph.json` 零命中，lint.sh 明写「no active task-graph.json; the plan contract gate has no object, skipped」；而 event-integrity 各 job 直接用 `golden/runtime/event_integrity_scenarios.json` 喂 runner，**判定对象存在**。故该 skip 是「无对象」不是「跳过测试」，但**收尾报告须如实标注它是无对象态，不得写成绿**。

**F2 执行纪律（收尾单据此核）**：跳过、零测试、超时、缺日志、错提交均不是完成；`go test ./cli/...` 现态是 **0 但 `[no test files]`**，须如实报零测试而非绿；bot smoke 与 soak 可能触用户数据根，**禁读 `examples/wechat-bot/.wechat-config/data` 与任何真实 `--source`**，须用临时合成数据根，且原用户真实数据禁区不豁免临时合成数据的模块验收。

**当前并发四单**：G14（DoD 注册侧）、G15（A/B/D 簇＋长跑残差）、G17（C 簇兜底提交序）、G18（midband 症状锚只读根因）。**九变异实跑与全量 lint 排在 G15/G17 落地、控制树全绿之后**，此刻跑必假红。

### R150 重入落座缺陷确证仍红（第四条阻断未闭合）；派 G19；又一次假绿被当场抓住（2026-11-17）

**编排者亲测**：`tests/integration/host/send_reentry_restart_test.go`（mtime Nov 4，**本变更未改过它**，表达既有契约）
```
-run '^(TestReenteredTaskServesAfterRestart|TestReentryOfNoTaskServesNoRound)$' -count=1 -timeout 240s ⇒ EXIT=1
:97 the re-entry receipt after the restart = "[send_denied] the task table holds no record of
    task-f328aea5a43bb8c1a47c81d6c3c493f6, and a body names a task of this host", want the bare "ok"
```
⇒ **R142 第四条阻断「(c) 重入消费者未跟上」定性须勘误**：它不是「测试没跟上契约」，而是**产品恢复正确性缺陷**——重启后任务表无该任务记录，重入被拒。上下文日志见 `boot seated the task account tasks=0 tail=none reads=5 keys=0`（**须核归属，日志可能来自并行测试**）。最可能根因面是四层去物质化的第③层**耐久任务账户尾谓词**或 `TaskIDs()` 有界化把重入任务排除，但**这是假设须取证**。

**假绿当场抓住（本会话第 N 次同病类）**：编排者先用 `-run 'TestSendReentry'` 跑，得 **EXIT=0 但 `[no tests to run]`**——零匹配不是通过。真名是 `TestReenteredTaskServesAfterRestart`／`TestReentryOfNoTaskServesNoRound`，改对正则后才见真红。**凡 `-run` 必核输出非 `[no tests to run]`，已写进 G19 派发令作硬纪律。**

**派 G19**（写入白名单 `runtime/state/durable.go`＋`durable_seat_test.go`、`assembly/checkpoint.go`、`assembly/task_checkpoint.go`、`runtime/task/checkpoint.go`、`api/durable/fact_log.go`、reentry 测试仅许补负锚）。硬约束：**不得改 :97 断言迁就实现**（它是用户可见契约）、**不得把已结态搬回检查点**、**不得放宽 `TaskIDs()` 有界界**；若根因落在 G15/G17/G14 面则停下来报不抢写；若正解须发明「已结任务重入复活」语义则停下来报不自裁。要求负锚（回退修复须重新变红具名）＋`-count=3`＋`TestReentryOfNoTaskServesNoRound` 不回归。

**R142 五条阻断归属现已全有主**：①死锁 ✅已闭合（编排者亲测 0）②长跑 boot 残差→G15 ③assembly 十红→G15 收 7／G17 收 3 ④重入落座→G19 ⑤midband 症状锚→G18 只读根因（判是否与 G17 同源后定合单或另派）。另 DoD 13 gap→G14 注册侧＋后续实跑；godoc 投影→编排者待存储面落地统一重生成。

**当前并发五单**：G14／G15／G17／G18（只读）／G19。触碰集不相交：G19 明禁 `memory/ledger/**` 与 `assembly/*_test.go`，G15 明禁 `segment_store.go` 与 `runtime/**`，G17 明禁 `assembly/**` 与 `journal.go`。

### R151 G14 收口（两处机制缺口确证）；门代理裁定；计划账目假绿记录已重写（2026-11-17）

**G14 收口，编排者复测确证其两项发现**：
- **P5 已绑真门锚**：`gate=check_spec_hygiene.py`、`selftest=test_check_spec_hygiene.py`、`measure_token=scan_specs`（实证 :78 算出 violations/requirement_count）。`legacyRequirementText` 全树零命中（仅 golden 自身）⇒ 按 P6 先例，`measure` 是概念量、`measure_token` 是计算函数名，**绑定为真，无需改 design**。G14 未编造 token，合消红三禁。
- **⚠️ 机制缺口一（编排者复测确证）**：绑 P5 后**新生一条假红** `P5: the selftest … never compares a gate exit against zero`。根因：`check_dod_coverage.py:68` `EXIT_COMPARISON = re.compile(r"\b(?:code|returncode|status)\s*!=\s*0\b")` 是**变量名白名单代理**，而 `test_check_spec_hygiene.py:90` 是 `return proc.returncode, proc.stdout`、比较处用 `rc != 0`(:165/:194)／`check_rc != 0`(:168)／`rc != 1`(:182/:221) ⇒ **语义成立而门看不见**。gap 总数仍 13（闭「binds neither」1 条、开「selftest」1 条）。
- **⚠️ 机制缺口二（G14 四证，编排者采纳）**：变异 runner **不支持门锚变异**——`go_test_command`:1943 只跑 go test；`validate_registry`:1761 要求断言位是 test_file 内 Go func＋失败构造；`compiled_face_problem`:2257 要求补丁在 Go import 闭包（spec.md 永不编译⇒not_applied）；`check_dod_coverage.py:571` 明拒门锚 kill 归因。故 **P5/P6 变异无法注册，硬塞即死条目**。G14 **拒绝硬塞是对的**。它已取证两条注入真使门红（P5 注 event-lifecycle/spec.md:12 ⇒ `R4 go-file in requirement 'Time-based TTL expiration'`；P6 注 tool/spec/spec.go ⇒ `tool/spec/DeadProducerProbe has no consumer`），且**两注入均已复原零 diff、git status 与开工清单逐行一致**。
- **P4 重指取证（未实施，正确）**：两候选锚文本各恰命中一次——A 删 `journal.go:604-607` 水位跳过块 ⇒ open 全量回放，失败原文 `the open reads the history behind the watermark instead of the window past it`；B 删 `run.go:2587-2594` bootIndexWalk cut 分支 ⇒ 全索引走，失败原文 `the walk follows the history the chain holds instead of stopping at the cut its newest checkpoint states`。**两者皆在活跃改动区**（journal.go 水位块是未提交 diff 的新增行；run.go hunk @@ -2584,6 +2579,90 @@ 覆盖 bootIndexWalk）⇒ **重指须待 G15 落地**，G14 未声明「G9 不碰」是诚实的。

**编排者裁定（两处，均按「重写代理非喂它」）**：
1. **`EXIT_COMPARISON` 改判绑定来源，不改名、不放宽**。改名 `rc`→`returncode` 是**喂代理**（下一个名字又破）；放宽成接受任意 `x != 0` 是**削弱门**（无关比较冒充退出码比较）。正解：识别「从进程结果取退出码」的绑定（含跨 helper 传递），再要求这些绑定名之一与 0 比较。**四条负锚必须齐全**：只打印不比较／比较无关变量／只比 `!= 1`／helper 返回常量而非 `proc.returncode`，各须仍红。跨 helper 追踪若不可靠须停下来报局限，不得假装闭合。
2. **扩 runner 增门判决模式**（副本树跑具名 Python 门、`exit != 0`＋失败子串命中、归因门路径），扩 schema/validate/face 使非 Go 目标不判 not_applied，**但 Go 变异既有检查不得放宽**，**G8 建的「门锚不得借已注册 Go 变异白拿 kill」旁路封堵不得拆**，R26 写树纪律不得破。
**派 G20 合单承接两交付**，明令交付 1 先做先验再做交付 2（防 runner 改到一半连既有 Go 变异也跑不了＝回归），预算将尽须分开如实报不留半改状态。**本单不跑九变异全量**（控制树仍红）。

**计划账目重写（`tasks.md`，编排者单写）**：原「当前状态（R141）」段含**假绿记录**——写着「两条症状直译锚全绿（boot 三界全 0、长跑各阶段达标）」，而该声称已在 R142 撤销（出自单次 `-count=1`，run2 boot 1.511 破界），且 midband 症状锚现红。另「concept 剩 1 条」（实为 0）、「DoD 15 gap」（实为 13）、「在跑四单 06.R14v/G8/S4b」（早已收口）均陈化。**已按亲测凭据整段重写为 R151 并标勘误**，13 域行同步改写（G8/G11 已收口、现余真跑侧）。**不留矛盾文本，历史凭据只附勘误。**

**当前并发五单**：G15（A/B/D 簇＋长跑残差）、G17（C 簇兜底提交序）、G18（midband 只读根因）、G19（重入落座）、G20（DoD 门锚两机制）。G14 已收口，其写面（`check_dod_coverage.py`／`check_event_integrity_mutations.py`／golden）移交 G20，无重叠。
**门禁现态（编排者亲测）**：dod **13 gap**；dead_producers 0（3445 函数）；spec_hygiene 0（violations 279）；test_dod_coverage 28/28；test_spec_hygiene 11/11；build/vet/死锁锚 0。**assembly 10 红、midband 锚红、重入落座红、gen_godoc 红均未闭合，不得归档不得提交。**

### R152 F1 反夹带部分审计（整树 diff 扫描，两个可疑文件已排除）（2026-11-17）

**整树规模（编排者实测）**：`git diff --stat` ⇒ **193 文件，22349 插入，6578 删除**。按目录聚合：openspec/changes 43、memory/ledger 19、tests/integration 17、docs/api 17、scripts/gates 10、runtime/task 9、runtime/state 9、features/evolution 9、examples/contract 7、docs/wiki 5、openspec/specs 4、examples/wechat-bot 4、runtime/agent 3、scripts/scenarios 2、scripts/recover-kv 2、scripts/codetools 2、golden/runtime 2、docs/notes 1、docs/OPERATION.md 1、docs/GLOSSARY.md 1、cli 2、assembly 若干。**规模与四层去物化＋门建设＋文档投影一致**。

**两个疑似夹带文件已逐个读 diff 排除**：
- `docs/notes/trpc-fork-patch-assessment.md`（4 +---）：删「4 commit 可溯」的两条重复 git 命令，改为 `# 4 commit 的清单与计数见 §2.1（同一命令，不在此重述）` ⇒ **一处一事实去重**，属 doc_budget 棘轮收敛面，非夹带。
- `examples/wechat-bot/skills/url-fetcher/TESTING.md`（26 +----）：删「优势对比」整节（旧版 url_fetcher.js ❌ 四条 vs 新版 smart_fetcher.js ✅ 六条）＋「已知问题和改进方向」改「已知问题」⇒ **删变更叙述与冗余对照**，属文档卫生／doc_budget 面（与 D3b「样例技能文档冗余」裁定同源），非夹带。

**features/evolution 九文件**（governance/checkpoint.go＋其测试、denial.go、goal.go、三处 skeleton_assert.go、meditation/watermark.go、posterior.go）与 **runtime/agent 三文件**（consumer.go、fuse.go、loop.go）⇒ 面与 07 治理托管、事件日志退役、恢复/重入路径一致，**未见无关特性改动**。

**⚠️ 未查面（不得当已确认）**：本审计只做了**目录聚合＋两个可疑文件逐行读＋两组文件名面比对**，**未逐行审 193 文件的核心 diff**。F1 要求的「不夹带」完整结论须待收尾单做 holistic 核心 diff 审查（尤其 memory/ledger 19 文件与 assembly 面的 22349 插入）。**本条只是部分排除，不是 F1 通过。**

**另核**：`.github/workflows/ci.yml` 本变更**零改动**（`git diff --stat` 空）⇒ 未夹带 CI 改动，合「不改 CI」授权边界。

**当前并发五单**：G15／G17／G18（只读）／G19／G20。**待编排者后续做**：存储面落地后统一重生成 godoc 投影（共享面单写）→ 控制树全绿后派九变异实跑 → 全量 lint＋CI 矩阵 → R35 第二轮独立放行审 → F1 完整反夹带＋逐项对账 → F2/F3/F4。**归档与 commit/push 须用户明示授权。**

### R153 编排者自修：概念门「同名门两 scope」假红（域地板无对象仍被判决）（2026-11-17）

**发现路径**：核纯度缺口 #6/#8 时跑 `check_concept_budget.py --rule S4`，得 **EXIT=1** `ERROR: concept ratchet: domain_surface.removed=0 below the frozen floor 5`，而**全量跑 EXIT=0**。同名门两 scope 判决相反 ⇒ 必有一边误判。

**根因确证（读码＋两 scope 计数对比）**：
- `check_concept_budget.py:1326-1329`：`domain_counts` 初始为 `{}`，**只在 `RULE_DOMAIN in rules` 时**才调 `domain_surface_report` 填它。
- `:1358` **无条件**调 `check_ratchet(report, arch_path, metrics, domain_counts)`。
- `:1385-1386` 原写 `removed = domain_counts.get("domain_surface.removed", 0)`、`new = ….get(…, 0)` ⇒ 规则 scope 不含 D1 时取默认 **0**，`0 < floor 5` 即**假红**；同理 `new=0` 会平凡通过天花板（**假绿面**）。
- **两 scope 计数实测对比**：`--rule S4` 与全量的 `exported_interfaces=81`／`interfaces=117`／`go_files=286`／`packages=37` **完全相同** ⇒ `collect_findings` 的 metrics 是**全 scope**，只过滤 findings。**故接口天花板两 scope 同量，无误判；唯一缺陷是域派生的 removed/new 两档。**

**修复（编排者自做，小而确定，不派单）**：把两档地板改为**只在域报告真被计算时判**（`if "domain_surface.removed" in domain_counts`），未计算时**具名 warn**「this rule scope measures no domain surface, so the removed floor 5 and the new ceiling 57 are unjudged」，且 `held` note 改印 `removed/new unjudged (no domain surface in this scope)`——**不得声称判过它没判的地板**。接口天花板**照常无条件判**（metrics 全 scope）。
**这与门自身既有语义一致**：`:1381` 原文「A golden file without the concept_ratchet key is a missing ratchet, not a passing one: it warns by name so the unenforced floors stay visible」——**无对象＝具名告警，不是静默通过也不是失败**。

**负锚与降级证明（齐全）**：
- 新增两用例：①`test_unmeasured_domain_scope_warns_instead_of_failing`（空 domain_counts 须 ok＋warn 具名 unjudged＋note 不得声称判过）②`test_unmeasured_domain_scope_still_enforces_the_interface_ceiling`（空 domain_counts＋ceiling 压到 69 仍须红，证明**没连带关掉整个棘轮**）。
- 自测 **56→58，58/58 EXIT=0**。
- **降级证明**：把守卫改回 `if True:`＋`.get(…,0)`（即旧行为）⇒ **EXIT=1**，`test_unmeasured_domain_scope_warns_instead_of_failing` 具名 FAIL（另一条仍 ok，因降级只动域两档，符合预期）。
- **还原实证**：`diff -q` 与备份 byte-identical，复跑 **58/58 EXIT=0**。
- 两 scope 复测：`--rule S4` **EXIT=0**（warn 具名＋note 如实）；全量 **EXIT=0**，note `concept ratchet held: removed>=5, new<=57, interfaces<=85`。

**纯度缺口盘点（编排者亲验，取代 R141 的陈旧声称）**：#1 三接口收口 ✅（`api/durable.FactLog`）；#2 scratch 绕法 ✅（F5 已删）；#3 `bootContext` ✅（全仓 grep **零命中**）；#6 ChainSource 样例 ✅（`examples/contract/runtime_task/ChainSource_example.go:24` 有 `var _ task.ChainSource`）；#7 ✅；#8 concept ✅（全量 0，`baseline_stale: 0`）。**#4 未闭**（P6 门锚已绑但变异注册阻于 runner 不支持门判决 ⇒ G20）；**#5 未闭**（DoD 真跑侧 13 gap ⇒ G20 注册侧＋后续实跑）。
**⚠️ 纯度判据②「派生载体不迫使全量读」的证据已失效**：R137 曾以「boot 三界全 0、走查比 1.00–1.03」判 ✓，但那出自**已撤销的单次绿**；长跑 boot run2 1.511 破界 ⇒ **该判据现处未验证态，须待 G15 的 `-count=3` 逐阶段数字重新收证**，不得沿用旧 ✓。

**改文件（编排者本轮自写）**：`scripts/gates/check_concept_budget.py`、`scripts/gates/test_check_concept_budget.py`、`openspec/changes/runtime-recovery-ideal/tasks.md`（R151 重写）、本文档。**与五单触碰集不相交**（G20 拥有 dod_coverage/mutations/golden，不拥有 concept_budget）。

### R154 G17 收口（C 簇三红转绿，编排者亲测）；新立纯度缺口 #9：兜底档早停失效且症状锚未量该面（2026-11-17）

**G17 凭据（编排者独立复测，不采信汇报）**：
- `go test ./assembly/ -run '^(TestTierParentFollowsCommitOrder|TestTierParentFallsBackToBodiesForMissingSession|TestFullReplayUsesCommitOrder)$' -count=1` ⇒ **EXIT=0**。**C 簇三红闭合。**
- 新正锚 `TestEachIndexNewestFirstRanksCarriedCommitsWithoutEngineOrder`（`segment_store_test.go:591`）⇒ **EXIT=0**。
- G17 自报：负锚（比较符翻向 `<`）使三红**具名复现**，还原 `diff -q`=0；三正锚 `-count=5`＝0、`-count=5 -race`＝0；`memory/ledger` 全量＝0（53s）；build/vet/三门禁＝0。
- **A/B 归属实证**：换回修复前原文件复跑，assembly 同 5 条仍红 ⇒ 余红非 G17 引入。
- 改动面：`segment_store.go:1219-1223` 兜底加 `sort.SliceStable` 按 `position.commit` 降序、循环改正向（:1224）；同步修正 `:1154-1159` 函数 doc 一句（原「one pass over it」已失实）——**这是注释真话纪律的正确执行**。
- **assembly FAIL 已从 10 降到 5**（G17 时点读数）：余 `TestAnchoredBootMatchesFullBoot`、`TestDefaultBootRebuildsFromTheAnchor`（B 簇）、`TestRestoreFoldCounts`、`TestRestartRecoveryPinsBeforeTrim`（D 簇）、`TestSymptomAnchorPerGenerationCostFlatOverALongRun`（长跑）。**A 簇三条已不在列表 ⇒ G15 似已收 A 簇，待其战报确认，编排者尚未亲测该簇。**

**⚠️ 新立纯度缺口 #9（编排者追路径发现，G17 未报此面）**：
- **事实**：`assembly/checkpoint.go:918-924` 的 `newestSessionCheckpointRef` 调 `EachIndexNewestFirst`，命中第一个匹配 checkpoint 即 `return errStopIndexWalk` ⇒ **它靠早停取前缀**。journal 档走 `CommitOrder` 能力路径，早停真能约束读取。
- **但兜底路径**（memory 档与 **StorageFile/rustviking 耐久档**）先 `publishedEntriesOf` **全量物化＋排序**，然后才走查 ⇒ **早停不再约束读取量**，boot 付全索引。
- **归属**：G17 诚实报出「publishedEntriesOf 本就全量物化＋`sort.Slice`（O(n log n) 已在），本次仅增一次同级稳定排序」⇒ **全量读是先于本变更的既有形态，非 G17 引入**；G17 只增常数因子，未引入新复杂度类。
- **为何是缺口而非已闭**：纯度判据②「派生载体不迫使全量读」与⑤「本征 O(历史) 归索取者、不摊进 boot」在**兜底档不成立**，而**两条症状锚都跑 journal/localfile 档 ⇒ 该面从未被量到**。按 F3「不声称有限自测证明无限完备」，**不得用 journal 档的绿声称判据②全域成立**。
- **是否本征**：键序引擎的提交序只写在值里，要命名最新提交须读全部值 ⇒ 对该类引擎**疑似本征**。但 boot 是索取者，本征成本摊进 boot 与判据⑤冲突 ⇒ **须裁：要么 boot 改走另一真相（如槽表/头水位已陈述的框架 checkpoint，见 `checkpoint.go:901` 日志「the commit order states no framework checkpoint」暗示存在该机制），要么显式声明该档 boot 的本征界**。**这是设计级问题，编排者尚未裁，不得当已闭。**
- **处置**：立案为 #9，**待 G15/G19 落地、控制树绿后派只读取证单**核「file 档 boot 是否真有另一真相可走」，再裁。**本轮不扩范围实施。**

**纯度缺口现态**：#1/#2/#3/#6/#7/#8 ✅闭合（编排者亲验）；#4 阻于 runner 门判决模式（G20）；#5 阻于真跑（G20＋后续）；**#9 新立，未裁**。**判据②的证据仍是未验证态**（R153 已记：旧 ✓ 出自已撤销的单次绿）。

**当前并发四单**：G15（A/B/D＋长跑）、G18（midband 只读）、G19（重入落座）、G20（DoD 门锚两机制）。G17 已收口，`segment_store.go` 写面释放。

### R155 A 簇/C 簇/重入三处阻断编排者亲测闭合；G19 实现 review 合格；懒水合级联面立案派 G22（2026-11-17）

**编排者独立复测（不采信汇报，逐项实跑）**：
- **A 簇闭合**：`-run '^(TestDeliveryDurableReadErrorSurfaced|TestBootDoesNotReadOriginalShards|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice)$' -count=1` ⇒ **EXIT=0**。归因确证：近 20 分钟改动文件含 `memory/ledger/journal.go`/`journal_kv.go` ⇒ **G15 所修**。
- **C 簇闭合**：三锚 ⇒ **EXIT=0**；G17 新正锚 `TestEachIndexNewestFirstRanksCarriedCommitsWithoutEngineOrder` ⇒ **EXIT=0**。改动文件 `segment_store.go`＋其测试 ⇒ G17。
- **重入落座闭合**：`-run '^(TestReenteredTaskServesAfterRestart|TestReentryOfNoTaskServesNoRound)$' -count=1` ⇒ **EXIT=0**（原 :97 报 `send_denied … holds no record`）。**R142 第四条阻断闭合。**
- **midband 症状锚仍红**：`:153` 14 条目/2 段折产物/summary=false、`:195` `recall_get("evt_00-1-plan") Found:false Reason:missing`，原文与 R146 一致 ⇒ G18 仍在只读诊断，**修复单未派**。
- **归因方法**：用 `find . -name '*.go' -newermt '-20 minutes'` 定位实际改动文件，**不靠 mtime 猜测**。据此确认重入修复落在 `runtime/task/reactivate.go`（G19），而非我先疑的 G15/G17。**`tool/spec/spec.go` 的 `git diff` 为空 ⇒ G20 的变异注入已复原，零残留。**

**G19 实现 review（编排者读 diff，合格）**：`Reactivate`（约 :60）与 `Reentrant`（约 :99）把 `entry, ok := r.tasks[taskID]` 改为 `entry, ok, err := r.hydrateLocked(ctx, taskID)`，注释陈述「A task the newest checkpoint stated no entry for is read back from its chain first, so a restart re-enters the settled outcome the record holds instead of answering that the task is unknown」。
**两条冻结不变量均守住**：①**未把已结态搬回检查点**——它从链上按需读回，检查点仍只携开放项；②**未放宽 `TaskIDs()` 有界界**——`runtime/state/durable.go:1055` 仍只遍历常驻 `d.facts`，非历史扫描。
**且合两条既有原则**：「懒水合须覆盖全部入口」与「派生不可信须退化读真相非拒绝」（原实现直接答 `ErrTaskNotFound` 是拒绝，现改为读真相）。`hydrateLocked` 是**既有机制**（`runtime/task/checkpoint.go:363-370`），`cancel.go:47`、`lifecycle.go:277/298/350` 早已消费 ⇒ G19 是**扩覆盖面而非新建结构**，合「更少特异化结构」。

**⚠️ 新立级联审计面（派 G22 只读）**：全仓 `runtime/task` 非测试文件里仍有十余处直接读 `r.tasks[taskID]`——`handoff.go:275`、`lifecycle.go:537`、`logical.go:228/250`、`progress.go:50`、`restore.go:168/190/204/249`、`wrapper.go:323`、`checkpoint.go:371/386`、**`reactivate.go:141`（G19 diff 只改 :60/:99，此处未改）**。按「懒水合须覆盖全部入口」与级联检查纪律，**每个入口须判定是否可能在重启后收到已结任务**。
**G22 四问**：①逐入口判「须水合而未水合（缺陷）／不须水合（给理由）／无法确定」，**明禁一律判须水合**（恢复流程内部读常驻 map 是正确的，盲目加水合会把 O(历史) 摊进 boot，违反已裁不变量）；②`reactivate.go:141` 是否 G19 级联漏项；③`hydrateLocked` 的成本、是否物化写回（须区分「内存 registry map 暂存」与「耐久检查点载体」，后者不得携已结态）、**是否持 `r.mu` 做 IO 而与 `d.mu`/`k.mu` 形成锁环（G9 刚修的 ABBA 同类风险）**；④哪些缺陷入口当前无锚量着。
**瞬时态警示已写进派发令**：G19 可能正在做负锚证明（临时回退再还原），故 G22 须间隔复读或标注读到哪一态，**不得把回退态当最终实现下结论**——编排者本轮 grep 就见 `reactivate.go:60/99` 仍显示旧形，与 diff 矛盾，疑为回退窗口。

**当前并发六单**：G15（B/D 簇＋长跑）、G18（midband 只读）、G19（重入，已见绿，疑在做负锚）、G20（DoD 门锚两机制）、G21（兜底档 boot 成本只读）、G22（懒水合级联只读）。另有编排者后台跑 assembly 全量取当前 FAIL 全集。
**阻断现态**：死锁 ✅／A 簇 ✅／C 簇 ✅／重入 ✅；**未闭**：B 簇 2＋D 簇 2＋长跑（G15）、midband（G18 诊断中）、DoD 13 gap（G20）、gen_godoc（待存储面落地）、纯度 #9（G21 取证中）。**不得归档不得提交。**

### R156 assembly 五红实测详情；长跑「结构有界／噪声破界」须分开；comment_policy 回归已修 4 条；G18 根因确证派 G23（2026-11-17）

**⚠️ 又一次假绿陷阱（编排者自己踩并当场抓住）**：后台 bash 通知报「exit code 0」，但那是**包装 shell 的退出码**（末命令是 grep），**不是 `go test` 的**。读日志才见真态：`FAIL github.com/SpellingDragon/tagent/assembly 238.296s`，**5 FAIL**。另一次：日志还是 0 行时 `grep -c "^--- FAIL"` 返回 0，**空日志的零红不是绿**。**凡后台跑测试，须读日志终局行（`^ok`/`^FAIL`）而非通知退出码。**

**assembly 五红逐条（编排者实测原文）**：
1. `TestAnchoredBootMatchesFullBoot` — **`anchored_boot_parity_test.go:173/:175` 有 `DBG refs=… landed=… report=…`／`DBG ref typ=…` 调试打印**，是 G15 留下的**调试残留，须删**（零残留标准）。
2. `TestDefaultBootRebuildsFromTheAnchor` — `:340 the full walk decoded 89 facts beside the 88 the chain indexes and the 8 the boot decoded, want one ledger of one chain`。**89 vs 88 差一条**，是真正确性信号（全量走查比链索引多解出一条事实），不是噪声。
3. `TestRestoreFoldCounts` — `host_delivery_test.go:2414 recovery report = [], want the one chain rebuilt`（D 簇）。
4. `TestRestartRecoveryPinsBeforeTrim` — `:2465 the early-trim control kept the fact, so it did not exercise the defect`（D 簇负对照失效，**须重表达对照不得删守卫**）。
5. `TestSymptomAnchorPerGenerationCostFlatOverALongRun` — **关键区分**：
   - **结构量有界成立**：`indexWalkVisits=[0 4 4 4 … 4]` **252 代恒为 4**；`indexWalkValueReads` 同形恒 4；`indexWalkValueBytes` 244→268（仅随值宽度微增，非随历史）。⇒ **索引走查的去物质化在结构上确实有界。**
   - **破界的是噪声比对**：`:426 two independent roots of one shape paid 104.08425ms and 84.074458ms for identical work at the reference depth`（1.24×）。这是**两个独立根做相同工作的墙钟离散**，属 **09.R2 校准面（噪声判据）**，不是结构增长。
   - **待 G15 解释的量**：`restored=[0 7 14 25 … 2753]` 每代约 +11 线性增长、`history=[1..252]`。须辨明 `restored` 是**累计计数**还是**每代 boot 真付的恢复量**——若是后者则 boot 仍 O(历史)，判据②不成立。**编排者不下结论，归 G15 取证。**
   - **纪律**：**不得放宽界**（禁），负载敏感红须**空闲复跑**；若确属机器噪声，正解是 09.R2 给出**可辩护的噪声判据**（如同根多次采样的分位比而非两独立根单次比），**不是把界调大**。

**comment_policy 产品面回归 0→5，编排者已修 4 条**：
- 5 条全来自 G17/G19 新代码：`segment_store.go:1219` free-standing（**编排者令 G17「写一行 WHY」时未说明本仓禁 free-standing，是令的缺口**）、`segment_store_test.go:583` **audit-marker `no longer`**、`:591`／`:1131` test-doc-not-one-sentence、`runtime/task/reactivate_test.go:400` test-doc-not-one-sentence（G19 面，待其收口）。
- **编排者自扫漏检的原因**：我只扫了三个**生产文件**的 `//` 行，**没扫测试文件**，故漏掉 `no longer`。**门比我的手工扫描覆盖广，不得以自扫零命中声称无泄漏。**
- 规则原文已读（`scripts/comment_policy/main.go:1360-1395`）：测试 doc **首行须单物理行意图，后续行必须以 `- ` 开头**，另有 `testDocMaxLineRunes` 长度上限。据此把两处折行意图合并为单行、`no longer holds`→`does not hold`、删 free-standing 一行。
- **复测**：`comment_policy -no-baseline -v memory` ⇒ **0 finding**；`go build ./memory/ledger/` ⇒ 0。**余 1 条在 G19 面。**

**G18 收口：midband 根因确证为产品缺陷（本候选引入），派 G23**：
- **根因＝memory 档两个引擎**：`OpenTier` 给 `Originals: NewMemoryOriginalStore()`（自带独立 memKV，`config.go:290`＋`original_store.go:100`），而候选把写路径从 HEAD 的 `tier.Originals.Put` 改为 `ledger.CommitFact`（`assembly/facts.go:393`），后者把 `blob:`/`ref:` 连同事件家族写进 **`segments.kv`**（`segment_store.go:807-820`）⇒ Originals 自己的 kv 永远空；`publishFactLocked` 只填进程内 byKey、`loaded` 仍 false，首次 `ReadSegmentScoped`→`ensureLoaded`→`load()` 以**空结果整体覆盖** byKey（`kv_original_store.go:157`）⇒ 全部原文 missing。**durable 档共用引擎故免疫；新增 CommitFact 测试全用 journal/localfile，memory 档无覆盖**——这是漏网直因。
- **G18 反驳了我的前提（采纳）**：`:195` 注释「the read path holds the fact today」**已不成立**；该锚可见性语义 HEAD 已实现且绿，**当前红不是「中段折叠不可按票召回」症状，而是前提被存储回归打破**。另 `:153` 是 `t.Logf` 非断言，`:206`/`:213` 因 `:195` fatal **从未执行**，修好后须首次观测。
- **与 C 簇不同源**（`GetEvent` 按 `P:idx:KEY` 点读，不经错序兜底）⇒ **不合单，另派 G23**。
- **裁定**：令 memory 档 Originals 与 Segments **共用同一引擎**（链唯一真相、与 durable 档同构、统一契约胜多端口）。**明禁改 `load()` 去 merge**——那是掩盖第二引擎而非消除它。
- **G18 另报新红面（归 G15）**：`tests/integration/history` 另有 **9 个测试红于 journal「commit outcome uncertain: invalid argument」**，根因未查。
- **G18 未查面（不得当已确认）**：HEAD 隔离树未跑（归属为推断⚠️）、修后 :206/:213 未观测、branch1/branch3 归属为排除法推断。

**当前并发**：G15（B/D 簇＋长跑＋DBG 残留＋history 9 红）、G19（重入，已绿，余 comment_policy 1 条）、G20（DoD 门锚，gap 已 13→12）、G21（兜底档 boot 成本只读）、G22（懒水合级联只读）、G23（memory 档双引擎）。
**门禁现态**：build 0／concept 0／dead_producers 0／vocabulary 0／comment_policy memory 面 0（余 runtime/task 1 条）；**dod 12 gap**；gen_godoc 红（**现两文件**：`memory__ledger.md`＋`runtime__task.md`）；assembly 5 红；midband 红；history 另 9 红。**不得归档不得提交。**

### R157 长跑锚定性确证：负对照通过、失败在机器噪声前置守卫；`restored` 是分母不是成本（2026-11-17）

**编排者读码确证（`assembly/symptom_anchor_longrun_test.go`），撤销 R156 留的疑问**：

**① `restored` 线性增长是设计意图，不是缺陷**：`:91` `entry.restored = segments.EventCount()` ⇒ 它是**store 里的事实总数＝历史规模**，不是 boot 付的工作量。`:166-167` **明断言它必须增长**：「want a chain that grew so the cost is held against a growing history」。`:299-300` 说明其用途：`historyProportionalCost` 以它为**分母**构造负对照序列。**故 `restored=[0 7 14 … 2753]` 的线性增长是被钉住的前提，不违反判据②。R156 把它列为「待辨明是否 boot 真付量」的疑问就此关闭。**

**② 断言顺序与各自结果（读 :400-432 确证）**：
1. `judgeLongRunFixture` 夹具有效性 → **通过**（否则更早 fatal）
2. `judgePerGenerationCost(flat, …)` 判据自检「须接受平坦序列」→ **通过**
3. **负对照**：`carried := historyProportionalCost(run, cost, window)`，要求 `judgePerGenerationCost(carried, …)` **必须拒绝**且错误含 `grows with the chain` → **通过**。**这是真负锚：判据确实能识别并具名拒绝「成本随历史增长」的序列，不是假锚。**
4. `firstShallow`/`secondShallow` 两个独立根同形走查 → `judgeFixtureDispersion(firstMiddle, secondMiddle, …)` → **失败于此（:426）**，错误原文 **「the bound decides the dispersion of this machine rather than the walk」**（104.08ms vs 84.07ms）
5. `judgeCountedFaces`（计数面是否随历史增长）→ **因 :426 fatal 从未执行**

**③ 定性结论**：长跑红**不是「产品成本随历史增长」的断言失败**，而是**机器离散度前置守卫**在真正的增长判决之前触发——**测试自己在说「本机噪声太大，此界判不了走查」**。且 `indexWalkVisits`/`indexWalkValueReads` 252 代恒 4 已独立表明索引走查结构有界。
**④ 处置纪律（不得放宽界）**：正解属 **09.R2 校准面**——要么**空闲复跑**降离散，要么把离散守卫改成**可辩护的统计量**（如同根多次采样取分位比，而非两独立根各单次比）。**禁把 `perGenerationCostBoundTenths` 调大**；**禁删 `judgeFixtureDispersion` 守卫**（它是防「界判的是机器不是走查」的真守卫）。
**⑤ 仍未验证的面（不得当已绿）**：`judgeCountedFaces` 从未执行 ⇒ **「计数面不随历史增长」这一条本轮未取得证据**，须在离散守卫通过后补跑。判据②的完整收证仍待此步。

**⇒ 对目标第三句（纯度）的当前结论**：去物质化的**结构效果有独立证据**（索引走查恒 4、checkpoint 只载开放项、已结态深度 8/32/128/512 body 逐字节相等〈R141 前凭据，须复核〉）；**墙钟面的完整收证被机器噪声前置守卫挡住**，不是被产品缺陷挡住。**负对照机制本身已验证有效**，故一旦离散通过，增长判决是可辩护的。

**当前并发六单**：G15／G19／G20／G21（只读）／G22（只读）／G23。**编排者本轮未派新单**，因长跑定性已清、交 G15 与后续 09.R2 承接。

### R158 G21 纠正编排者两条前提；纯度 #9 裁定为「补声明与锚」派 G24；现态快照（2026-11-17）

**G21 收口，纠正 R154 立案所引的两条前提（编排者采纳并勘误）**：
1. **`newestSessionCheckpointRef`（`assembly/checkpoint.go` 约 :913）零生产调用方**——仅 `checkpoint_test.go:1412`，`git grep` HEAD 同。**boot 实走 `restoreSessionCheckpoints`（约 :948 ← `run.go:1083` `restoreCheckpointState`）**。R154 写「它在 boot 路径上」**已过时，勘误**。
2. **rustviking 耐久 boot 现构建不可达**：`StoreEvent` 恒 4 键批（`segment_store.go:611`），`rustvikingKV.Commit` 拒 >1 键（约 :150），`storage_capabilities_test.go:346` 钉死 `ErrBatchNotAtomic`；11.R3 已记「file 档事实层整体不可用，待编排者批准」。memory 档非耐久，durable 门控的 boot 恢复整体跳过（`run.go:608/677`）。⇒ **「耐久兜底档 boot 付全量物化」无可达实例**，R154 的严重度估计须下调。

**G21 其余确证**：
- **无另一真相可走**：slots 水位＝重放字节位（`journal.go:662`）、order.log＝逐提交语句（:68），**均不陈述 checkpoint 位置标量**；`store:seat` 仅 Count/CommitSeq/Partitions（:659）。R154 猜「槽表/头水位可能已陈述框架 checkpoint 位置」**被否掉**。两句日志同在 `tailFactKeys`，描述走查机制非预置锚。
- **本征性**：键＝`分区:idx:事件键`（:648），session/type/commit 全在值里（:650）⇒ 前缀扫描无法按会话/类型收窄，排序须读全部值。**现布局下本征**；但**非原理本征**（journal order.log 即同一查询有界前缀的存在证明），修复形态须受纯度裁（R121 禁复述链已陈述者）。
- **判据原文权威**：R121.3/R135「本征成本归索取者**并显式声明**」、R123「boot 不该为没人索取的报告枚举历史」⇒ 本意是「真需要＋声明」，**非绝对禁 boot 付 O(历史)**。06 design 约 :541 已裁 nil 兜底、成本表 §十九·七 已声明「Seat nil 兜底 O(历史)」，**但 `EachIndexNewestFirst` 兜底全量物化未列入表 ⇒ 未声明**。
- **既有锚不足**：`indexOnlyKV` 负对照（`segment_store_test.go:482`）只断言物化分配＞有界走查＋一 chunk（**存在性下界，无深度界**），单深度 2000、量 `NewestOfType` 非 boot；G17 锚只验序不量成本。

**⇒ 编排者裁定 #9＝「本征成本未显式声明界」，派 G24 收口**（非「可修结构缺陷」因无现时受害者；非「已覆盖」因锚无界）。两交付：①06 design 成本表补兜底行，如实声明 O(已发布条目) 全量物化＋排序、早停失效、本征、**现无可达耐久受害者并给钉住该态的锚名**、日后批准 file 档则此界重开；②`segment_store_test.go` 补**兜底深度界锚**，**钉住「兜底确实付 O(深度)」这个线性事实**（不是假装它有界），与声明互为见证，**须带可失败控制案**。**禁调大既有界、禁删既有声明、禁改生产面 `segment_store.go`**（若计数缺口须改它则停下来报）。

**⚠️ 新立死生产者候选（待收尾审计，本轮不派）**：`newestSessionCheckpointRef` **零生产调用方、仅测试调用**。`check_dead_producers` 现 EXIT=0 是**因为该门明定「测试调用算消费者」**（其 docstring：a test file, its calls are real, so a test oracle is a consumer）⇒ **门按设计抓不到「只有测试调用的生产函数」**。按 common.md §五.2「测试不纳入衡量」，这是**潜在残留**，须在 F1 收尾审计里逐个判定：是真死代码该删，还是有意的测试专用 oracle。**不得因门绿就声称无死代码。**

**现态快照（编排者本轮定向实测）**：midband 症状锚 **仍红**（G23 在修）；B/D 簇四锚中 **3 红**（G15 在修，日志含 DBG 二进制匹配⇒调试打印仍在）；`check_dod_coverage` **12 gap**；`comment_policy` 产品面 **1 finding**（余 `runtime/task/reactivate_test.go:400`，G19 面；memory 面已由编排者清到 0）；`gen_godoc --check` **红，1 文件**（较上轮 2 文件减一）。
**当前并发七单**：G15／G19／G20／G22（只读）／G23／G24。（G17/G18/G21 已收口。）
**不得归档不得提交**：assembly 余红、midband 红、history 另 9 红、dod 12 gap、godoc 陈化、comment_policy 1 条、#9 声明与锚未落、`judgeCountedFaces` 从未执行。

### R159 停放账（回合预算将尽，工作安全停放，目标未达成）（2026-11-17）

**⚠️ 本变更未达成，不得归档、不得提交、不得推送。工作树含大量未提交候选，本轮全程零 git 写操作。**

**最终门禁快照（编排者亲测，非采信汇报）**：`go build ./...`=**0**；`check_concept_budget`=**0**；`check_dead_producers`=**0**；`check_vocabulary_alignment`=**0**；`check_doc_budget`=**0**；`check_dod_coverage`=**1（12 gap）**；`comment_policy` 产品面=**1 finding**（`runtime/task/reactivate_test.go:400` test-doc-not-one-sentence）；`gen_godoc --check`=**1（两文件：`memory__ledger.md`、`runtime__task.md`）**。
**工作树规模**：`git diff --stat` ⇒ **196 文件／22854 插入／6610 删除**；`git status --short` ⇒ **270 条**。

**G19 收口＋越界自决裁定（编排者采纳）**：
- 根因确证＝「读到了但重入入口未水合」：事实**确在存储**，boot 落座 `tasks=0 tail=4 reads=0 keys=0` 是**已裁不变量的正确行为**（检查点只携开放项、尾谓词正确排除 cut 前已结任务）；缺陷在 `reactivate.go:60/:99` 直读内存表，而兄弟入口 `lifecycle.go:277/298/350`、`cancel.go:47` 均经 `hydrateLocked`。
- **越界自决采纳**：产品改动落在白名单外的 `runtime/task/reactivate.go`。理由成立——根因确在该文件，白名单内无任何修复能在不违两条冻结不变量下触及根因，且**本文档 :2140/:2182 早已裁「这些入口也过 hydrateLocked」**（06.R12c 修了 Advance/Cancel/Resume，漏 Reentrant/Reactivate），该文件无在飞单写。**是编排者的白名单过窄，勘误自己的令**（同 R156 的 free-standing 令缺口）。
- 凭据：正锚双测 0、`-count=3` 6 PASS、负锚回退复红具名＋还原 `diff -q` 一致、host 套 **9 FAIL vs 基线 10 FAIL**（修好恰 1 条零新增）、三门禁 0。**两条不变量守住**：检查点未改、boot 仍 `tasks=0 reads=0`，水合是查询期按需读由命名该任务的读者付 O(单任务)。

**已闭合阻断（编排者逐项亲测）**：①ABBA 死锁 ②A 簇新根只读开档 ENOENT 3 条 ③C 簇 memory 档兜底错序 3 条 ④重入落座。**另修**：概念门「同名门两 scope」假红（编排者自修＋2 负锚＋降级证明，56→58 用例）、comment_policy memory 面 4 条（编排者自修，该面归 0）。

**在飞单（预算尽时可能未收，接续者须先核其实际落盘再判完成）**：
| 单 | 面 | 交付状态 |
|---|---|---|
| G15 | B 簇 2＋D 簇 2＋长跑离散＋`anchored_boot_parity_test.go:173/175` DBG 残留＋history 另 9 红 | 未收，A 簇已落 |
| G20 | DoD 门锚两机制（EXIT_COMPARISON 改判绑定来源、runner 增门判决模式） | 未收，gap 已 13→12 |
| G22 | 懒水合级联只读审计（十余处直读 `r.tasks`＋锁环风险） | 未收 |
| G23 | memory 档双引擎（midband 根因） | 未收，`config.go` 已在改 |
| G24 | 纯度 #9 成本表声明＋兜底深度界锚 | 未收 |

**剩余硬项与具体下一步（按依赖序）**：
1. **收 G15/G20/G22/G23/G24 战报并逐项亲测复核**（门禁数字须重测，不采信汇报）。
2. **清 DBG 残留**（`anchored_boot_parity_test.go:173/175`）＋**comment_policy 余 1 条**（`reactivate_test.go:400`，测试 doc 首行须单物理行意图）。
3. **midband 症状锚转绿**后须首次观测 `:206/:213`（此前因 `:195` fatal 从未执行）。
4. **长跑离散守卫**：空闲复跑或改可辩护统计量（09.R2 校准），**禁调大界禁删守卫**；通过后**补跑 `judgeCountedFaces`**——它从未执行，故判据②「计数面不随历史增长」**本轮无证据**。
5. **godoc 统一重生成**（共享面编排者单写，须待存储面全部落地）：`bash scripts/gates/gen_godoc.sh` 后回拷，再 `--check` 须 0。
6. **DoD 真跑侧**：G20 落地后跑九变异实跑（`check_event_integrity_mutations.py --all --json --out <仓外目录>`），**硬依赖控制树全绿**；P4 须重指存活反例（两候选锚文本在 G15 改动区，须待其落地）。
7. **全量门禁**：`bash scripts/lint.sh` 须 exit 0（含 dod 与 godoc，故 5/6 未齐前不可能绿）＋CI 十一 job 面（十短/六长/soak/race/openspec，清单见 R149）。
8. **F1 完整反夹带**：本轮只做了目录聚合＋两可疑文件排除（R152），**196 文件核心 diff 未逐行审**；另须判 `newestSessionCheckpointRef`（零生产调用方、仅测试调用，死代码门按设计抓不到）是真死代码还是有意 oracle。
9. **R35 第二轮独立放行审**（只读拷问，须含驳回清单与明确未查面）→ **F2/F3/F4**。
10. **归档与 commit/push 须用户明示授权**，本轮不执行。

**未查面（不得当已确认）**：`judgeCountedFaces` 从未执行；history 包 9 红根因未查；G22 锁环结论未出；#9 声明与锚未落；F1 核心 diff 未逐行审；R35 第二轮未做；09.R2/R3 未做。

### R160 停放前最后一收：comment_policy 产品面归零（编排者亲收）（2026-11-17）

G19 已收口故其文件空出，编排者亲收余下 1 条 finding：`runtime/task/reactivate_test.go:392-394` 的测试 doc 意图折成三物理行，违反「首行须单物理行意图、后续行须以 `- ` 开头」（规则原文 `scripts/comment_policy/main.go:1360-1395`）。**改为单行意图＋把原解释性散文降为一条 bullet**，信息无损、断言零改动。
**复测**：`comment_policy -no-baseline` 产品面八包 ⇒ **0 finding**（起点 1）；`go build ./...` ⇒ 0；新锚 `TestRestartReentryReadASettledTaskFromItsChain` 单跑 ⇒ 见本轮实测退出码（非 `[no tests to run]`）。
⇒ **R159 快照里的三个红门现降为两个**：`check_dod_coverage`（12 gap）与 `gen_godoc --check`（`memory__ledger.md`、`runtime__task.md`）。**comment_policy 产品面已绿。**
**其余停放状态与剩余硬项一律以 R159 为准，本条只更新 comment_policy 一项。目标仍未达成，不得归档不得提交。**

### R161 G22 收口：八个按名索取面须水合而未水合（新立案，未派）；锁序无环；编排者 grep 过窄被纠正（2026-11-17）

**G22 只读审计结论（含实测，非纯读码）**：
- **⚠️ 新缺陷面（须派单，本轮预算尽未派）**：**8 个按名索取面须水合而未水合**——`manager.go` 的 `Obligations`(:62)/`Handoff`(:93)/`Flush`(:153)、`handoff.go` 的 `HandoffOf`(:58)/`IssueNotice`(:275)、`cancel.go` `RecordStop`(:76)、`progress.go` `ProgressReported`(:50)、`logical.go` `Logical`(:228)。八面实测重启后对已结任务均答 `ErrTaskNotFound`。
  - **其中 `Obligations` 是现时可达缺陷**：唯一历史查询方 `assembly/read.go:473` 先 `Lookup` 物化才 `:477` 读，**靠隐式顺序兜住**⇒ 须同时消掉该顺序依赖。
  - 其余七面**全仓无生产调用方**（仅测试/契约例/skeleton_assert），故症状不可达，但按「懒水合须覆盖全部入口」仍须收。
  - **最小修复面**：八面改走 `hydrateLocked`（Manager 面持的就是 `r.mu`，可直接调）＋八面各补「重启已结正锚＋陌生 id 负锚」＋补「水合后快照零条」锚（`checkpoint_test.go:591` 水合 20 个后未再快照，该不变量当前无锚）。
- **`reactivate.go:141` 不是级联漏项**：它是 `RollbackReactivation`，唯一调用方 `host_tools.go:406` `unwindReentry` 只在 `Reactivate` 成功后触发，水合已把 entry 写入 `r.tasks`（`restore.go:266`）⇒ 必常驻。实测先 Reactivate 再 Rollback＝nil，直接 Rollback＝`ErrTaskNotFound`（语义本该拒）。**R155/R158 留的疑问关闭。**
- **锁序无环（实测）**：`r.mu`→`d.mu`（`durable.go:1124`）→`k.mu`（`journal_kv.go:66`）→磁盘 ReadAt，**持锁 IO 确实存在**，但 `DurableCommit` 不持 registry 引用、`uncertain` 回调生产为 nil（`run.go:612`）、`memory/ledger` 不 import 任何 runtime 包、`journalKV` visit 前先放 `k.mu`（:278/:304）、`noteProgress` 非阻塞 channel；且 `r.mu`→`d.mu` 边早由 `applyCommitLocked`（`lifecycle.go:401`）存在，**水合未新增边**。`-race` 跑水合/检查点/重入族＋host 两锚 ⇒ **EXIT 0，无 DATA RACE 无挂死**。
- **两条不变量实测守住**：水合只写内存 map（`restore.go:266`）非耐久载体；`SnapshotCheckpoint` 按 `settledAtCut` 跳过，**实测水合后再快照＝0 条**。
- **⚠️ 水合成本须留意**：`Records`→`readTaskLocked`（`durable.go:558`）做 **5 次前缀 scan**，`eachHeld`（`journal.go:1688`）→`eachRecordOnce`（:1501）**整表顺序读 `a.capacity` 槽**再 HasPrefix 过滤 ⇒ 首次按名索取付 **O(历史键容量)×5**，之后 `d.facts` 命中（:1126）不再读。**这是「本征 O(历史) 归索取者」的实例，须按 R121.3/R135 显式声明界**，与 #9 同类，归 G24 的成本表一并声明或另立。
- **编排者前提被纠正（采纳）**：我的 grep 只匹配 `r.tasks[`，**漏了 `m.registry.tasks[…]`／`registry.tasks[…]` 形态**，故漏 HandoffOf/Obligations/Handoff/Flush/RecordStop/acceptDelivery 及 Retained/RecoveryMaterial/Unfinished/Owed。**判「全部入口」须枚举全部接收者形态，不得只 grep 一种。**
- **不须水合的面已逐个给理由**（恢复/水合本体、遍历常驻集且终态本就跳过、仅本进程新 admit 的 wrapper 面）——**加水合会把 O(历史) 摊进 boot/close，故不得一律加**。
- **两条稳定红（复跑仍红，非瞬态，未归因）**：`runtime/state` `TestDurableCommitBarrier`（`invalid argument`）、`runtime/task` `TestConsumptionFailureIsReportedByName`（Wrapper.execute 无错误行），均落在未提交候选 diff 面（`durable.go` 306 行、`unsettled_test.go` +135 行），**非水合面，须另归因**。
- **G22 未查面**：`runtime/task/approval`、attention.go、hold.go、commit.go 内部读；assembly 其余 slice；memory/query 投影；索引走查实测计时。

**⇒ 接续者优先级更新**：R159 第 1 步「收五单战报」现余 **G15/G20/G23/G24** 四单（G22 已收）。**新增待派单**：八面水合＋锚（含消 `read.go:473→477` 隐式顺序依赖）、水合成本界声明、两条稳定红归因。

**停放终态（编排者最后一次亲测）**：`go build ./...`=**0**、`go vet ./memory/ledger/ ./runtime/task/ ./assembly/`=**0** ⇒ **停放树可构建可 vet，非半改断裂态**。HEAD 仍 `9547faca`（**零 git 写操作**），`git status --short` 270 条未提交。台账 2993→本条。**目标未达成，不得归档不得提交不得推送。**

### R162 G23 收口：midband 症状锚转绿（最后一条症状锚闭合）；两项零残留遗留立案（2026-11-17）

**编排者亲测复核**：`go test ./tests/integration/history/ -run '^TestSymptomAnchorMidbandProcessRecalledByTicket$' -count=1` ⇒ **EXIT=0**（基线 1）。`go build ./...` ⇒ 0。**R142 第五条阻断闭合，两条症状直译锚现均绿。**

**G23 修法（纯度上最优，采纳其自决）**：**删 `config.go:284-295` 的 memory 特异分支**（-12 行），使 memory 落入与 durable **逐字同构**的共用路径（`newKVOriginalStore(store,nil)`＋`newFileSegmentStore(store)`），Capabilities 对 memory 算得 Durable=false/AtomicBatch=true 与原分支一致 ⇒ **一引擎一真相**，未触 `load()` 覆盖语义（明禁遵守）。取分支删除而非原地改，合「统一契约胜多端口」「更少特异化结构」。
**根因链实证**：`config.go:223-224` memory→`newMemKV()`（引擎A）；原分支 `Originals: NewMemoryOriginalStore()`→`original_store.go:100` `newEmptyKVOriginalStore(newMemKV(),nil)`（**独立引擎B**）；写路径 `assembly/facts.go:393`→`CommitFact`→`segment_store.go:807-820` 写进 A；读路径 `reference.go:251/265`→`ensureLoaded`→`kv_original_store.go:157` `load()` 从恒空的 B **整体覆盖** byKey ⇒ missing。durable 走 `config.go:296` 共用 A 故免疫；既有 CommitFact 测试全用 LocalFile，**memory 档无覆盖是漏网直因**。
**新锚凭据**：`TestCommitFactMemoryTierRecallsTheOriginalByBareKey`（`kv_original_store_test.go` 末尾）`-count=3` **3/3 EXIT=0**；负锚回退分支 ⇒ EXIT=1 具名（`:498 the scoped read of "memory-original" = {Outcome:2 Body:[]}`），还原 `diff -q` 一致。
**`:206/:213` 首次执行结果（R159 第 3 步就此闭合）**：症状锚 `-v` PASS，`:198` 日志「premise holds…228 byte(s)」证实 `:195` 前提恢复；`:206`（4 个 folded 键 ticket 全在请求）与 `:213/:216`（按 ticket 召回 Found＋正文逐字）**均绿，无独立缺陷**。`:195` 前提注释修复后已属实，**未改该测试**。
**history 包 9 红根因确证（归 G15）**：源头 `journal_kv.go:199/203` `ErrCommitUncertain` 包 EINVAL，落在 `journal.go +2243`／`journal_kv.go +222` 候选改动面；durable 路径与 G23 改前逐字相同故非其引入，与 G18 预报吻合。

**⚠️ 两项零残留遗留（立案，本轮预算尽未派）**：
1. **`NewMemoryOriginalStore` 生产调用方归零 ⇒ 成死结构，按零残留应删**，但删除被禁改面阻断：`segment_store.go:831` `factOriginalsOf` 的 `*MemoryOriginalStore` case、`reference.go:241` 与 `tool/recall/skeleton_assert.go:25` 编译期断言、**5 个测试文件**调用它（`scenario_s07:38`；`original_reference_isolation:35/61/94/127/170/185/228`；`tool/recall` 的 `key_test:25`/`get_public_test:95`/`get_test:82`）。**须同批**：删构造器＋简化 `factOriginalsOf` 为单 case＋迁这 5 测试。`newMemKV`/`newEmptyKVOriginalStore` 仍活不得连带删。**`dead_producers` 门计测试为消费者故仍绿——门绿≠无死代码（R158 已记同病类）。**
2. **文档漂移**：`docs/wiki/memory/memory-architecture.md:175` memory 行仍写 `NewMemoryOriginalStore()`，应改为共用 store 的一对；`:171`「同一后端」契约现已被代码兑现。

**门禁现态**：build 0；concept 0／dead_producers 0（3447/3447）／vocabulary 0／comment_policy memory 面 0；**两条症状锚均绿**；红项余：assembly B/D 簇＋长跑离散（G15）、history 9 红（G15，根因已明）、dod 12 gap（G20）、godoc 两文件陈化。
**在飞余三单**：G15／G20／G24。**目标仍未达成，不得归档不得提交不得推送。**

### R163 G24 收口：纯度 #9 闭合（成本表声明＋兜底深度界锚）；编排者又一条前提被纠正（2026-11-17）

**G24 两交付均落，编排者采纳**：
- **成本表 §十九·七 新增一行**（既有六行逐字未动）：`EachIndexNewestFirst` 兜底＝**O(已发布索引条目) 全量物化＋排序；早停只约束 visit 数不约束读取量；现键布局下本征，非可修缺陷**，附机理／本征因（键 :647，type/session/commit 在值 :651-655）／三档无受害者论证＋锚名／观测缺口／重开条件（file 档事实层获批则界重开，修复面在引擎序能力）／判据引 R121.3、R135⑤、R123 原文。
- **新锚 `TestEachIndexNewestFirstFallbackMaterializesTheWholeIndex`**：memKV 上首 visit 即停，断言 reads≥depth＋10× 深度读数落 9–11× 线性带＋visits=1＋首 visit 为最新 commit。**深度 200/2000 实测 reads 225/2250，严格线性，`-count=3` 三次逐字相同**。控制案具名红：`read (41, 266) entries at depths (200, 2000), want every published index entry… the fallback materializes the whole index before it ranks, so the stop never bounds the reads`。**锚钉住的是「兜底确实付 O(深度)」这个线性事实，与声明互为见证，不是假装有界。**
- **其自决采纳**：控制案用**测试侧有界前缀引擎包裹**而非临时改生产 `segment_store.go`——因 G15/G23 正并发编译同包，临时改生产会污染其证据；语义等价。还原 `diff -q` exit 0。**这是正确的并发隔离判断。**

**⚠️ 编排者前提被纠正（采纳并勘误 R158/R161）**：我写「memory 档 durable 门控的 boot 恢复整体跳过（`run.go:608/677`）」**不确**——`:608` 仅 warn，`:677` 的门是 `durable != nil`（memory 档亦真），**restore 会执行**。真机理：memory 档非耐久（`config.go:296`）跨进程持零，故 boot 兜底**零条目可物化**。**结论（无现时耐久受害者）不变，但机理须按真话写**，design 行已按真机理落。

**⚠️ 观测缺口确证（新立案）**：`indexWalkValueReads/Bytes` **只在两条引擎序路径计数**（`segment_store.go:1183-1184/:1204-1205`），**兜底只计 `indexWalkVisits`（:1226）**⇒ 兜底路径的读取量在生产侧无观测。G24 改用既有**测试侧 `seatReads` 接缝计数器**（`segment_store_test.go:684`）绕过，未改生产。**该缺口本身须另立单补生产侧计数或显式声明不补的理由**，不得因测试侧能测就声称生产可观测。

**验证凭据**：build 0；新锚 `-count=1`/`-count=3` 均 0（`===RUN` 实证非 no-tests）；控制红 1＋还原 diff 0＋复跑绿；`memory/ledger` 全包 0（85.9s）；comment_policy memory 面 0；concept ok/0、vocabulary 0、doc_links 0。**零 git 写，`segment_store.go` 未改，白名单外零触碰。**

**⇒ 纯度缺口 #9 闭合**（声明＋锚齐）。**在飞余两单：G15／G20。**
**剩余项一律以 R159 依赖序为准，另加本条新立的「兜底路径生产侧观测缺口」与 R161/R162 的八面水合、`NewMemoryOriginalStore` 删除、wiki:175 漂移、水合成本界声明、两条稳定红归因。**目标未达成，不得归档不得提交不得推送。**

### R164 全部在飞单收口：G20＋G15 战报与编排者复测；停放账终版（2026-11-17）

**编排者复测（不采信汇报）**：`check_dod_coverage` ⇒ **10 gap**（G20 报 13→10 属实）；`test_check_dod_coverage` ⇒ **EXIT=0**（28→39 用例）；**`test_check_event_integrity_mutations` ⇒ EXIT=1，7 条 FAIL/ERROR，Ran 48**（G20 自报的碰撞属实，未消）。

**G20 收口（两机制均落）**：
- `EXIT_COMPARISON` 三名白名单改为 **ast 绑定来源判定**：`.returncode` 属性、`subprocess.call`/`os.system`、helper 返回传递（不动点解析，含 tuple 槽位——`rc, output = run_checker()` 跨层承认 `rc`）。仅字面 0 比较计数，只 `!= 1` 仍红。**四负锚实跑均 exit 1**（print-only／`count != 0`／只 `rc != 1`／helper 返回常量）。**P5 假红闭合，P6 原绿保持。** 局限如实报：taint 按函数作用域，嵌套函数不继承外层绑定；`rc != expected_rc`（默认 0）不计；嵌套 tuple 不追踪。
- **runner 增门判决模式**：副本树内跑具名 Python 门，exit≠0 ∧ 登记子串 ⇒ killed，归因 `record.gate.path`；schema 增 `gate_path`/`gate_selftest`，门条目禁声明 Go 测试；**Go face/selector/test-file 拒实测未放宽**；**G8 的「门锚不得借 Go 变异白拿 kill」封堵保留**；登记文本须门源承载，委托 runner `gate_text_states`（单一真相）。**P5/P6 已注册并真跑 killed**，原文 `R4 go-file in requirement 'Time-based TTL expiration'`／`tool/spec/DeadProducerProbe has no consumer`。
- **真碰撞已解**：`dead_producers` 消费面走 root 全部 Go 文件，`LeaseLosses` 仅 examples 消费而 runner 副本排除 examples ⇒ 控制必红；改为选含门变异时纳入 examples（用户数据 gitignore 不入列）。
- **⚠️ 遗留新红（须另派，G20 白名单外）**：`test_check_event_integrity_mutations.py` **7 例红**——它们对每条注册变异合成 **Go** kill 流（`literal_site` 用 Go 函数形状解析 Python 门）。G20 **拒绝两条歧路**：Go helper 双语化＝喂代理、拆 registry＝测试驱动＋第二真相。**正解＝给 `judged_records`/`literal_site` 加门分支**（门条目合成 exit 1＋raw_text 含 literal）。41/48 过覆盖全部 Go 单元判决，7 红是**夹具类别错误非 Go 检查放宽**。
- G20 另报 `test_check_runtime_*` discover **296 测试 3 红均既有**（在飞 lint.sh 新段 vs 提交夹具缺文件、`api/durable` 未入 race 集、外来 symptoms 登记不符），均不加载其文件。

**G15 收口（assembly FAIL 10→3）**：
- **A 簇 3 条全绿**。真因不是「只读面缺 O_CREATE」这么简单：`TestDeliveryDurableReadErrorSurfaced` 注入 `dropPut(":evt:")` 要的是缺 **body** 的具名 `ErrSegmentBodyMissing`，A2/A3 是**播种写者**却未声明 `WriteRights`。产品修复按裁定：只读面遇未创建 log **陈述空视图、不建文件**；**以 `order.log`/`slots` 是否存在区分两成因**——无索引⇒年轻根空视图，有索引⇒log 曾存在被删⇒具名 `ErrJournalCorrupt`（**索引派生自 log，其存在是独立于「空视图」结构的领域证据**，合元规则二/三）。probe 实证两态。测试侧给确为写者处补 `WriteRights:true`（非改锚）。
- **B 簇：编排者前提被反驳（采纳勘误 R146）**——helper **非直读 order.log、亦非旧布局假设**，乃经 `Segments.EachIndexNewestFirst`→引擎 `CommitOrder` 接缝。EINVAL 真因：walk 跑在 `source.Close` 后的**闭视图**上，`addr.close()` 置 `a.order=nil` 而 `orderSize>0` ⇒ nil 句柄 `ReadAt` ⇒ `ErrInvalid`。**`ErrReadAfterClose` 已在 Read/Keys/Namespaces，独 walk 缺＝不对称缺陷**，两 walk 已补守卫。**B2 绿**（计数改经闭后只读视图 `tierIndexSize`，因 Close 会 file checkpoint 使索引 +1，即 R156 的 89 vs 88 之谜）。
- **余 3 红全根在禁改装配层，须另单**：
  1. `anchored_boot_parity_test.go:186`（B1）——**被表层 EINVAL 掩盖的装配缺陷**：boot 已消费 `restoredProjection`（`takeRestoredProjection` 破坏性），测试二次手动 `seedAnchoredReplay` 时 `restoredProjection=0/replays=1`，而 `bootIndexWalk` 仍信 standing cut ⇒ 有界 walk 只回 2 个 checkpoint ⇒ Anchors=0/Checkpoints=0。面在 `run.go`/`checkpoint.go`。
  2. `host_delivery_test.go:2414`（D1）——**引擎独立实证正确**（读/写面、各子前缀、commit 前后均读 4 个 task 键），boot durable load 却读 0 ⇒ **boot 集成缺陷**，非消费者没读报告。
  3. `host_delivery_test.go:2465`（D2）——**负对照被掩盖**：`pinned=false` 但 fact 存活，因它是 checkpoint survivor 位于 projection boundary 0（cut=0，链 40 < `noWindowResidentFacts` 2048），trim 永不及它，pin 未被行使。**重表达须夹具重设计**（settlement 须置于 restored survivor 窗口外使 cut 可达），超 test-only 安全编辑。**守卫保留未删、断言未改。**
- **长跑 `-count=3`：FAIL/FAIL/PASS**。**`judgeCountedFaces` 通过 ⇒ R157/R159 列为「从未执行、判据②无证据」的面现已取得证据：计数面平。** 参考深度离散小（~3%：93.99/93.07、91.05/93.94、93.65/96.71ms）。boot 阶段 gen247 vs gen1-5 中位：run1 **1.39×**、run2 **1.59×**（吻合前单 ~1.511）、run3 <1.3× 界。**归因＝mild 亚线性增长**（252 代仅 1.4–1.6×，**非 ~252× 本征 O(历史)**），近界机器可变；**亚线性 boot 成本与噪声的分离须 profiling**，超该单预算。负对照 `historyProportionalCost` 通过。
- **DBG 残留已净零**（`host_delivery_test.go` 调试加删净零，diff 无调试行）。
- G15 改文件：`memory/ledger/journal.go`、`journal_kv.go`、`assembly/run_test.go`、`assembly/anchored_boot_parity_test.go`。零 git 写。

**⇒ 停放终态（全部单已收，无在飞）**：
- **绿**：build／vet／concept／dead_producers（3447/32 包）／vocabulary／doc_budget／comment_policy 产品面／**两条症状直译锚**／A 簇／C 簇／重入／B2／`judgeCountedFaces`／长跑负对照／dod 自测 39/39。
- **红**：`check_dod_coverage` **10 gap**（余为真跑记录与 P4 退役重绑）；`test_check_event_integrity_mutations` **7 例**（夹具须加门分支）；`gen_godoc --check`（两文件陈化）；assembly **3 红**（B1:186／D1:2414／D2:2465，根在装配层）；长跑墙钟 **2/3 run 破界**（亚线性＋机器可变）；`test_check_runtime_*` 3 红（既有）；history 9 红（`journal_kv.go:199/203` ErrCommitUncertain 包 EINVAL，**G15 已改 journal_kv.go，须复测是否已消**）。
- **待派单（依赖序）**：①装配层三红（`run.go`/`checkpoint.go`/`facts.go`/`runtime/state`，含 `takeRestoredProjection` 破坏性消费与 standing cut 的交互）②mutations 自测门分支（7 红）③八面水合＋消 `read.go:473→477` 隐式顺序（R161）④`NewMemoryOriginalStore` 零残留删除＋5 测试迁移＋wiki:175 漂移（R162）⑤兜底路径生产侧观测缺口（R163）⑥水合成本界声明（R161）⑦长跑亚线性成本 profiling 与 09.R2 噪声判据⑧P4 退役重绑⑨九变异实跑（控制树须先全绿）⑩godoc 重生成⑪两条稳定红归因（`TestDurableCommitBarrier`／`TestConsumptionFailureIsReportedByName`）⑪history 9 红复测。
- **然后**：全量 `bash scripts/lint.sh`＋CI 矩阵（R149 清单）→ F1 完整反夹带（196 文件核心 diff 未逐行审）＋`newestSessionCheckpointRef` 死代码判定 → **R35 第二轮独立放行审** → F2/F3/F4 → **归档与 commit/push 须用户明示授权**。

**目标未达成。零 git 写操作，HEAD 仍 `9547faca`，270 条改动未提交，树可构建可 vet。本条为停放账终版，接续者从 R159 依赖序＋本条待派单清单冷启动。**

### R165 目标恢复（40 回合制）；现态重测；派 G25/G26/G27 三单（2026-11-17）

**编排者重测现态（距 R164 约 3 小时，无在飞单，树未变）**：`go build ./...`=0；`go test ./assembly/ -count=1 -timeout 400s` ⇒ **3 FAIL**（`TestAnchoredBootMatchesFullBoot`、`TestRestoreFoldCounts`、`TestRestartRecoveryPinsBeforeTrim`，与 G15 报一致）；`check_dod_coverage` ⇒ **10 gap**；`gen_godoc --check` ⇒ 红**两文件**。
**`tests/integration/history` 仍 9 红 ⇒ G15 改 `journal_kv.go` 并未消它们**，R164 留的「须复测是否已消」就此确证为**未消**。九条逐名与原文：
- **6 条同根** `memory: commit outcome uncertain: invalid argument`：`durable_task_round_trip_test.go:101`、`event_integrity_test.go:119/:2795/:4313`、`projection_replay_and_budget_test.go:170`、`restore_observation_test.go:74`。
- **2 条** `recovery outcomes = []`：`event_integrity_test.go:4912`、`restore_refuses_chain_without_spec_test.go:101`（疑与 assembly D1 同源）。
- **⚠️ 1 条耐久安全回归**：`storage_capabilities_test.go:258` **`reopen over a damaged record = <nil>, want memory: journal frame is corrupt`** ⇒ **损坏记录重开不再被拒**。疑与 G15 的 A 簇「空视图 vs 曾存在被删」二成因区分有关——**可能把「记录损坏」这第三成因误归入空视图或绕过帧校验**。**此条优先级最高**（损坏数据被当正常接受）。

**派三单（触碰集不相交）**：
- **G25**：`memory/ledger/journal.go`＋`journal_kv.go`及其测试。修①损坏检测回归（三成因须各有判据与具名结果，判据须来自独立领域事实）②提交 EINVAL（须区分「视图已闭」与「参数非法」）。**明禁放宽损坏检测/跳过帧校验/削弱 :258 断言**；须确认 `TestStorageCapabilities` 的 `ErrBatchNotAtomic` 子测试（约 :320/:346，被 G24 成本表引为 file 档不可达证据）**仍绿**。
- **G26**：`assembly/run.go`/`checkpoint.go`/`facts.go`＋`runtime/state/durable.go`（仅若需）＋两个 assembly 测试。收三红。**红一须先判产品缺陷还是夹具人为态**（`takeRestoredProjection` 破坏性消费＋二次手动播种；按「测试夹具需求不得驱动生产设计」，若只有人工播种才产生此态则修夹具走正式入口，不得给生产加兜底分支）；红二引擎已实证正确故为 boot 集成缺陷，须看是否连带收 history 2 条；红三**须重表达对照使 pin 真被行使**（settlement 置于 restored survivor 窗口外使 cut 可达），**守卫保留断言不削弱**。四条冻结不变量写进令。
- **G27**：**仅** `scripts/gates/test_check_event_integrity_mutations.py`。给 `judged_records`/`literal_site` 加门分支（G20 建议、编排者采纳），**Go 条目既有检查不得放宽**，41/48 既有用例须继续全绿；补五条门分支负锚。**明禁走 G20 已拒的两条歧路**（Go helper 双语化＝喂代理、拆 registry＝第二真相）；若发现是 runner 真实缺陷则停下来报不得用夹具掩盖。

**并发度判定**：其余待派项（八面水合→`runtime/task`＋`assembly/read.go`；`NewMemoryOriginalStore` 删除→`memory/ledger`；兜底观测缺口→`segment_store.go`；长跑 profiling→assembly；P4 重绑→锚文本在 `journal.go`；godoc→须待代码全落地；两条稳定红→`runtime/state`）**全部与三单写面或构建闭包相交**，故此刻三单即最大不相交并发，**不为凑数而派相交单**。

**目标未达成，不得归档不得提交不得推送。零 git 写，HEAD 仍 `9547faca`。**

### R166 编排者亲收 wiki 契约漂移（G23 遗留项之一闭合）（2026-11-17）

`docs/wiki/memory/memory-architecture.md` §七 表格 memory 行原写 `newFileSegmentStore(newMemKV())` + `NewMemoryOriginalStore()`，**与 G23 删特异分支后的代码不符**。已改为 `newKVOriginalStore(store, nil)` + `newFileSegmentStore(store)`，**与 localfile/journal 行逐字同形**——这正是「一引擎一真相」在文档上的兑现：该页开头「返回**同一后端的原文仓与段存储**」的契约此前对 memory 档是**假话**（两引擎），现已被代码与文档同时兑现。
**复测**：`check_doc_links`=0、`check_doc_budget`=0（棘轮未破）、`check_vocabulary_alignment`=0。
**`docs/api/…memory__ledger.md:886` 仍有 `NewMemoryOriginalStore`**——那是**生成投影忠实反映源码**（该构造器尚未删），**不是漂移**；待零残留删除单落地后随 godoc 重生成消失。**G23 遗留两项现余一项**：`NewMemoryOriginalStore` 删除（被 `segment_store.go:831`、`reference.go:241`、`tool/recall/skeleton_assert.go:25` 与 5 个测试文件阻断，须同批迁）。
**三单仍在飞**：G25／G26／G27。

### R167 全门普查发现两个此前未测的红门；durable_truth 级联漏项编排者亲收；product_surface 独立印证 NewMemoryOriginalStore 须删（2026-11-17）

**编排者跑完 lint.sh 内全部不依赖 Go 编译态的门（20 道，逐个报退出码）**：
- **绿（18）**：`test_check_schema_allowlist`/`check_schema_allowlist`、`test_check_assembly_symbols`/`check_assembly_symbols`、`test_check_spec_hygiene`/`check_spec_hygiene`、`test_check_plugin_mounting`/`check_plugin_mounting`、`test_check_window_bounds`/`check_window_bounds`、`test_check_product_surface`、`test_check_feature_boundary`/`check_feature_boundary`、`test_check_config_liveness`/`check_config_liveness`、`test_check_doc_links`/`check_doc_links`、`test_verify_restart_prefix`、`test_race_check`、`test_check_durable_truth_registry`。
- **另测三门绿**：comment_policy **棘轮模式**覆盖三模块（`. examples/wechat-bot examples/cli-host`）⇒ `0 finding(s); 0 beyond baseline; 0 ratchet slot(s) can be lowered`（**此前只跑过产品面测量模式，两者 scope 不同，本轮补齐**）；`check_doc_budget` ⇒ ok，`slack.lines: 0`（锁步紧）；`name-check` ⇒ no iteration-numbered test identifiers。
- **🔴 两个此前从未测过的红门**：`check_durable_truth_registry`=1（5 errors）、`check_product_surface`=1（1 error）。**二者都在 lint.sh 内 ⇒ F2 阻断面比 R149/R164 记的更大**，R149 清单虽列出它们但从未实跑，**「清单里有」不等于「测过」**。

**红门一：`durable_truth.json` 级联漏项（编排者亲收，已绿）**：
- 5 errors 全指向 **G10 已删的 `runtime/state/migrate.go`**：`surfaces[11]`（id `runtime.state.migration`，files 只有该文件，prefix `ArchiveSourcePrefix="archive:"`）＋其 prefix 两条＋`content_addressed_artifacts[0]`（id `runtime.state.migration-archive`，ticket_fact `archive:`，reason 述 `ArchiveDigest`）。
- **反事实检验先做**：`grep -rn` 全仓非测试 ⇒ **`ArchiveSourcePrefix`／`ArchiveDigest`／`"archive:"` 三者生产零命中**，`runtime/state/migrate.go` 文件不存在。⇒ 该机制已整体删除（08.R3 退位删除＋G10 删 migrate.go），**这两条登记描述的是不存在的机制，是登记表里的第二真相**。删它是**恢复真话**，不是删谓词消红。
- 该 golden 的 `user_ruling: "boundary-closure r19"` 是**建立登记表的裁决**，其 `reason` 明定范围是「Every persistent truth surface and durable key namespace **in production code**」⇒ 已非生产代码的条目**本就违反登记表自身范围**，删除与裁决一致。
- **处置**：删 `surfaces[11]` 整块＋`content_addressed_artifacts` 清空为 `[]`（保留键守 schema 位，同 G8 对 `mutation_verdicts` 的做法）。
- **复测**：JSON 合法（surfaces 17→16、caa 1→0）；`check_durable_truth_registry` ⇒ **EXIT=0，errors=0**；`test_check_durable_truth_registry` ⇒ **仍 EXIT=0**（未把该 surface 当夹具）；被删 id 在 golden 与自测里**零引用**。

**红门二：`product_surface` 判 `NewMemoryOriginalStore` 为 test-only 导出函数（待派，被 G25 构建闭包阻断）**：
- 原文：`new test-only exported function 'NewMemoryOriginalStore' in memory/ledger/original_store.go: a CI fix must not add public API (ORCHESTRATION redline); internalize it or record a named waiver`。
- **这是 G23 删掉其最后生产调用方的直接后果**，与 R162 立案的「零残留应删」**互相印证**：现在不只是编排者的纯度判断，**门已强制要求内化或删**。
- **不得走 named waiver 旁路**——该函数确已无生产消费者，豁免它等于把死结构合法化。**正解是删**（同批简化 `segment_store.go` 的 `factOriginalsOf` 为单 case、迁 `reference.go:241`／`tool/recall/skeleton_assert.go:25` 编译期断言与 5 个测试文件）。
- **暂不派**：其写面在 `memory/ledger` 包，与 G25 构建闭包相交，待 G25 落地后派。

**⇒ F2 阻断面更新（四个红门）**：`check_dod_coverage`（10 gap）、`gen_godoc --check`（两文件）、`check_product_surface`（1）、~~`check_durable_truth_registry`~~（**本轮已收**）。另 Go 侧红：assembly 3、history 9、mutations 自测 7、`test_check_runtime_*` 3（既有）。
**四单在飞**：G25／G26／G27／G28（F1 只读对账）。**目标未达成，不得归档不得提交。**

### R168 G27 收口（mutations 自测 54/54）；门普查续：四个新红门定性归因；G10 一处假绿勘误（2026-11-17）

**G27 收口（编排者采纳）**：`test_check_event_integrity_mutations.py` **Ran 54 OK，0 红**（48→54，+6 新用例）。7 红根因确证为**夹具类别错误**：7 例 traceback 全终止于 `literal_site` 第 88 行同一断言，Go 函数形状解析器（`^func\s`）套在 Python 门文件上。
- **门分支实现**：`literal_site` 门条目改读 `gate.gate_assertion_sites`（门自己的解析器）；`killed_result` 门条目合成 `RunResult(exit_code=1, raw_text=登记literal, command=gate.gate_command(...))` 镜像 `run_gate_check` 形状；`judged_records` 经 `killed_result` 自动获得门分支。**Go 分支逐字未动**，48 既有用例语义不变。
- **runner 门模式无缺陷（活跑取证，非 replay）**：`--mutation MUT-DOD-P5… --mutation MUT-DOD-P6…` ⇒ EXIT=0，两控制 green、两变异 killed、归因 `gate.path`、mode=live。**未改 runner**。
- 五负锚齐全（双测量／门缺失／照绿 gate_green／文案不命中 assertion_not_matched／func 不存在），降级证明（门分支 exit 1→0）⇒ 8 红具名，还原 `diff -q` 一致。dod gap **仍 10 未变**，dod 自测 39/39。
- 其自决采纳：`green_controls` 未加 gate 字段（消费方均不要求，最小变更）。

**门普查续（编排者实跑，补齐 R167 未覆盖面）**：
- **绿**：`check_runtime_architecture --check-policy`=0、`--implementation`=0；**文本前缀解析禁**（grep HasPrefix/TrimPrefix/strings.Split × .Content/.Summary/Meta[，排除测试与 examples）⇒ **零命中**；`dotted-refs`=0；`commit-scope`=0；`doc-refs`=0（HISTORICAL-DESCRIPTION-REF 是信息行非错误）；`test_check_package_layout`=0。
- **⚠️ 编排者自己制造过两条假红并当场纠正**：`doc-refs`/`proc-refs` 首跑报 `unknown subcommand "doc-refs docs/wiki docs/*.md"` ⇒ **zsh 不对未加引号的变量做词分割**，整串被当子命令名。**这是调用错误不是门红**，拆开重跑后 doc-refs=0。**教训：门红先核调用形态，再看判据。**
- **🔴 三个真红门（此前从未测过）**：
  1. **`proc-refs`=1**：`scripts/gates/test_check_no_prefix_criterion.py:24` 的 `AUTHORITY = "openspec/changes/no-prefix-criterion/design.md"` ⇒ 脚本引用变更过程工件。**该文件是未跟踪（`??`）、从未提交，属另一个未归档变更 `no-prefix-criterion`**，非本变更面。**跨变更阻断，须裁定归属**（本变更不夹带改它，但 lint 要绿就得处置）。
  2. **`tracked-hygiene`=13 条 `tracked-path-missing`**：索引持有而工作树没有的路径，全是**本变更删除但未入索引**的文件（`docs/api/…features_evolution_governance.md`、4 个 `examples/contract/runtime_state/*_example.go`、`memory/ledger/cold_archive_gate_test.go`/`cold_archive_test.go`/`journal_index_cut_test.go` 等）。**这不是代码缺陷，是暂存态**——`git status` 里它们是未暂存删除（` D`）。**须在提交前把删除入索引才转绿**，故 **F2 的全量 lint 必须排在暂存之后、提交之前**。本轮不做 git 写。
  3. **`package-layout`=17 条 `test-mirror-missing`**：测试文件无同名生产镜像。**归因已确证为 HEAD 既有红**：规则文件 `scripts/codetools/package_layout.go` **本变更零改动**且 **HEAD 版已含 `test-mirror` 规则**；被 flag 的 `assembly/plugin_mount_test.go`、`api/config/summary_budget_test.go`、`memory/replay/history_summary_budget_test.go`、`runtime/state/durable_seat_test.go` 等**在 HEAD 已存在且未改**，而它们的生产镜像（`plugin_mount.go`/`summary_budget.go`/`history_summary_budget.go`/`durable_seat.go`/`seat_tail.go`/`replay_ref.go`/`ticket_production.go`）**在 HEAD 也全不存在** ⇒ **HEAD 的 lint 本就红**，与 tasks.md「已有CI红证据」一致。**其中 2 条是本变更新增未跟踪测试**（`assembly/symptom_anchor_longrun_test.go`、`memory/ledger/fact_commit_batch_test.go`），属本变更须处置的增量。
- **⚠️ G10 假绿勘误**：G10 战报称 `package-layout`=0（5 文件）。**编排者实测 17 条红，且 HEAD 既有**⇒ **该声称为假绿，撤销**。病因待查（可能它跑了窄 scope 或读了自测退出码当门面退出码——`test_check_package_layout` 确实=0，**自测绿≠门面绿**，这是本会话反复出现的病类）。
- **另注**：`scripts/codetools/hygiene.go` 本变更**新增 33 行** `checkWhitelistUnnamedSource`（whitelist 模式 .gitignore 使新 Go 文件对 `git status` 隐形、CI 永不编译而本地 `go test` 照绿）——**这是本变更合法新增的门规则，与 test-mirror 无关**，不得混为「规则被收紧导致新红」。

**⇒ F2 阻断面（更新，六个红门）**：`check_dod_coverage`（10 gap）、`gen_godoc --check`（两文件）、`check_product_surface`（1）、`proc-refs`（1，跨变更）、`tracked-hygiene`（13，暂存态）、`package-layout`（17，HEAD 既有＋2 新增）。**已收**：`check_durable_truth_registry`（R167）。
**三单在飞**：G25／G26／G28。**目标未达成，不得归档不得提交。**

### R169 lint.sh 门面普查完成：F2 完整测量基线（编排者逐门实跑）（2026-11-17）

**本轮补测并收口**：
- `event-ownership --check` ⇒ **0**（symbols 14／owners 6／packages 38／direct 33／allowed 16；`uncovered` 行是信息非错误，退出码 0）。
- **`check-openspec.sh` ⇒ 0**：**openspec CLI 本机已安装**，`validate --all --strict` 主 spec 全绿。⇒ R149 记的「本地须先确认 CLI 是否安装，未装则如实标注跳过」**已确认为已装且真跑绿**，不是跳过。
- `go mod verify` ⇒ **all modules verified**。
- **gofmt 原 3 文件未格式化，编排者亲收 2 个**：`tests/integration/history/event_integrity_test.go`（**本变更已改，属我们的增量**）与 `tests/integration/capabilities/skill_load_body_test.go`（本变更未改，属既有）。`gofmt -w` 后**剩 1 个**：`assembly/host_delivery_test.go`（**G26 在飞写面，不抢改**，待其收口）。

**⇒ F2 完整测量基线（lint.sh 全门逐条实测，非清单推定）**：

**绿（已实测退出码 0）**：gofmt（余 1 在飞文件）／go mod verify／comment_policy **棘轮模式三模块**（0 findings, 0 beyond baseline, 0 slots lowerable）／comment_policy 产品面测量（0）／doc_budget（slack 0 锁步）／checker 自测组（runtime discover 除外，见红）／三挂点五对（schema_allowlist、assembly_symbols、durable_truth_registry〈R167 收〉、spec_hygiene、plugin_mounting，各含自测）／vocabulary 对／window_bounds 对／dead_producers 对（3447 函数）／concept_budget（stale 0）／dod 自测（39/39）／**mutations 自测（54/54，G27 收）**／architecture policy＋implementation／event-ownership／doc_links 对／feature_boundary 对／config_liveness 对／product_surface **自测**／restart_prefix／race_classifier／package-layout **自测**／name-check／doc-refs／dotted-refs／commit-scope／**openspec strict**。

**红（七项，逐个定性）**：
| 门 | 量 | 定性 | 归属 |
|---|---|---|---|
| `check_dod_coverage` | 10 gap | 真跑记录缺＋P4 退役重绑 | 待控制树绿后实跑 |
| `gen_godoc --check` | 2 文件 | 投影陈化 | 编排者待代码全落地统一重生成 |
| `check_product_surface` | 1 | `NewMemoryOriginalStore` 成 test-only 导出 | 待 G25 落地后派删除单 |
| `proc-refs` | 1 | 未跟踪文件引另一变更 design.md | **跨变更，须裁归属** |
| `tracked-hygiene` | 13 | 删除未入索引（暂存态） | **须暂存后才绿，排在提交前** |
| `package-layout` | 17 | test-mirror-missing，**HEAD 既有红**＋2 本变更新增 | 待 G25/G26 落地后派 |
| `test_check_runtime_*` discover | 296 中 3 红 | 既有（lint 合成树夹具缺文件／api/durable 未入 race 集／外来 symptom registry 不符） | 须逐个归因 |
| gofmt | 1 | `assembly/host_delivery_test.go` | G26 在飞 |

**Go 测试侧红（另计）**：assembly 3（G26 在修）／history 9（G25 在修 6+1，G26 疑收 2）／长跑墙钟 2/3 run 破界（亚线性＋机器离散，须 profiling 与 09.R2 噪声判据）／`runtime/state` `TestDurableCommitBarrier`＋`runtime/task` `TestConsumptionFailureIsReportedByName` 两条稳定红未归因。

**方法教训三条（本轮新增）**：①**门红先核调用形态**（zsh 不做未引号变量词分割，制造过 doc-refs/proc-refs 两条假红）；②**自测绿≠门面绿**（G10 的 package-layout 假绿疑因此）；③**「清单里有」≠「测过」**（R149 列了 durable_truth/product_surface/proc-refs/tracked-hygiene/package-layout 却从未实跑，本轮才查出四红）。

**三单在飞**：G25／G26／G28。**目标未达成，不得归档不得提交。**

### R170 编排者亲收 race 集覆盖缺口（本变更责任）；runtime 自测 3 红→2 红；余两条定性（2026-11-17）

**三条红逐个归因（编排者实跑取原文）**：
1. **`test_every_first_party_package_is_in_the_race_set` ⇒ 本变更责任，已亲收**。原文：`the race job does not cover first-party package(s) ['api/durable']: the main spec requires every registered package (subpackages included), not the modified-batch subset`。**`api/durable` 是本变更新建并已在 `golden/runtime/architecture.json` 注册的一等包**（pattern :110、清单 :339），故 race 集必须覆盖它。**处置＝在 `.github/workflows/ci.yml:147` 的 `race_check.sh` 参数列表补 `./api/durable/...`**。**这是遵从主 spec 的覆盖要求，不是改 CI 政策**（ci.yml 此前本变更零改动，本轮首次改，改动仅此一处一行）。**复测**：`test_check_runtime_s5_ci` ⇒ **EXIT=0（Ran 5）**；discover **3 红→2 红**。
2. **`test_registry_matches_the_module`（`test_check_runtime_symptoms`）⇒ 外来禁改面**。断言 `anchors == sorted(checker.SYMPTOM_PARENTS)`，涉事 checker 与测试**均在禁改清单**（`scripts/gates/check_event_semantics_symptoms.py`、`test_check_runtime_symptoms.py`，外来未跟踪）。**本变更不处置，立案为外来阻断**，须其持有者收口。
3. **`test_active_graph_is_checked_not_archive`（`test_check_runtime_plan.LintObjectSelectionTest`）⇒ 级联红，待查**。它在**合成树里跑 lint.sh**，断言 `returncode == 0` 实得 **2**，输出末行 `lint: checker selftests` ⇒ **lint 在 checker 自测段失败**，故本条是**下游级联**而非独立缺陷。疑与合成树缺未跟踪文件有关（同 `MUT-DOD-P1` 病类），须读该测试的建树方式确证。

**⇒ F2 阻断面更新**：runtime discover **2 红**（1 外来禁改＋1 级联待查）；其余六项红门同 R169。**另 `proc-refs` 仍 1**（`test_check_no_prefix_criterion.py` 未跟踪、属另一未归档变更），ci.yml 改动未影响它。
**三单在飞**：G25／G26／G28。**目标未达成，不得归档不得提交。**

### R171 编排者亲收合成树夹具级联漏项：runtime discover 3 红→1 红（余 1 属外来禁改面）（2026-11-17）

**根因确证**：`test_check_runtime_plan.py` 的 `LintObjectSelectionTest.setUp` 在 TMPDIR 合成树里**为 lint.sh 调用的每道门逐个铺 stub**（PY_STUB／SELFTEST_STUB 两张显式名单），再真跑 `bash scripts/lint.sh --only contracts`。**本变更给 lint.sh 新增的四道门在两张名单里覆盖为零**：
| 门 | lint.sh 调用次数 | 夹具 stub 命中 |
|---|---|---|
| `check_dod_coverage.py` | 2 | **0** |
| `test_check_dod_coverage.py` | 1 | **0** |
| `check_dead_producers.py` | 2 | **0** |
| `test_check_dead_producers.py` | 1 | **0** |
⇒ lint 跑到 `python3 scripts/gates/check_dod_coverage.py` 时**文件不存在退出 2**，与观测到的 `AssertionError: 2 != 0`、输出末行 `lint: checker selftests` 完全吻合。**这是「给 lint 加门」的级联检查漏项**（加门须同批补合成树 stub），不是产品缺陷也不是该测试自身判据错。

**处置（编排者亲收，小而确定）**：把四个名字分别补进 PY_STUB 名单（两个 check_）与 SELFTEST_STUB 名单（两个 test_check_）。**未改该测试的任何断言**，只补夹具覆盖面。
**复测**：`test_check_runtime_plan.py` ⇒ **EXIT=0，Ran 43**；`python3 -m unittest discover -s scripts/gates -p "test_check_runtime_*.py"` ⇒ **Ran 296，红从 3→1**。
**余 1 红＝`test_registry_matches_the_module`（`test_check_runtime_symptoms`）**，涉事 checker 与测试**均在禁改清单**（外来未跟踪 `check_event_semantics_symptoms.py`／`test_check_runtime_symptoms.py`）⇒ **本变更不处置，立案为外来阻断**，须其持有者收口。**不得为了让 discover 绿而改它或删该用例。**

**⇒ 编排者本轮自收清单（四项，均小而确定、不派单）**：①`durable_truth.json` 删已删机制的两条登记（R167）②wiki memory 行契约漂移（R166）③ci.yml race 集补 `./api/durable/...`（R170）④合成树夹具补四门 stub（本条）。另 gofmt 亲收 2 文件（R169）。
**⇒ F2 阻断面更新（六项）**：`check_dod_coverage` 10 gap／`gen_godoc` 2 文件／`check_product_surface` 1／`proc-refs` 1（跨变更未跟踪文件）／`tracked-hygiene` 13（暂存态）／`package-layout` 17（HEAD 既有＋2 新增）；runtime discover **1 红属外来禁改面**。
**三单在飞**：G25／G26／G28。**目标未达成，不得归档不得提交。**

### R172 G28 的 F1 对账收口：四类真问题，编排者亲收三类；裁决面扩大两处已勘误（2026-11-17）

**G28 只读对账结论（22 已勾叶：16✅／6⚠️／0❌）**：七域中 01/02/03 全✅；04 两⚠️（消费者锚今红属 G25/G26 靶）；07 两⚠️（同因，另 3.1 断言面经 07.R2 裁定改写故旧描述过期）；11 两⚠️；**12 域零已勾叶**（三 R 叶均未勾且记录诚实，对账为空集）。**六⚠️的主因是今日 EINVAL／损坏检测红，非被审叶自身缺陷。**

**编排者亲收三类（本轮已修并验证）**：
1. **夹带剔除**：`examples/wechat-bot/skills/url-fetcher/UPGRADE.md` 加「（如知乎、掘金）」1 行——**与恢复无关且是内容增补非删减卫生**，G28 指出编排者 R152 的排除名单只列了 TESTING.md 漏了它。**已 Edit 还原为 HEAD 原文**。⇒ R152 的「两个可疑文件已排除」须扩为**三个**，本条补齐。
2. **DoD 判据三处字段腐化**（`golden/runtime/dod_predicates.json`，门只比标题包含故抓不到）：
   - **P1 `criterion` 含已删机制**：原写「退位搬迁非删除，逻辑链恒完整可按身份重放」，而退位机制 R50/R51 已整体删除、design 已改。**已改为与 design 逐字一致**：「链唯一真相，一身份一不可变事实，SHALL NOT 删/改已提交事实；逻辑链恒完整、恒可按身份重放。」
   - **P4 `ci_gate` 指错 job**：原 `event-integrity-long`，但该 job 跑场景 runner **不跑 assembly 锚**；锚 `TestSymptomAnchorBootCostFlatAcrossGenerations`（`symptom_anchor_recovery_test.go:747`）**无 `testing.Short()` 跳过**，故实在 `test` job 的 `go test ./... -short`。**已改 `test`**。
   - **P5 `ci_gate` 指错 job**：原 `openspec`，但该 job 只跑 `check-openspec.sh`（ci.yml:194）；锚门 `check_spec_hygiene.py` 实在 `lint.sh:125-126` ⇒ `test` job（ci.yml:109）。**已改 `test`**。
   - **复测**：三处替换各恰命中一次（原子替换前 assert count==1）；`check_dod_coverage` **仍 10 gap**（这些是门看不见的字段腐化，修好不改 gap 数，正确）；`test_check_dod_coverage` **EXIT=0**。
3. **⚠️ 裁决面扩大两处已勘误（按「绝不伪造用户裁定」）**：G28 查出本文档 :2422 写「用户已裁 `api/durable` 新叶取 3 方法、净减 2 接口」、`10-contract-sync-dead-code/design.md:233` 写「用户裁定的净减 2（85→83）」。**核 R85 用户原话只选了「共享叶子统一端口」这一方向**；**落点 `api/durable`、3 方法、净减 2 都是编排者据语义归属推导的执行判断**（R85 自己就写了「语义归属须推导」）。**两处均已就地改写为准确归因并标 R172 勘误**，不留矛盾文本。G28 另核：②「回归项目迭代原则」归因正确（R121 引原话逐字、(c) 真水位化明标编排者推导）；N1 伪造裁决（2x 冒充 DoD）已纠（锚今写 uncalibrated）；**未发现用户真裁决被降级**。

**G28 查出但须另派/另裁的四项（本轮未收，写面与在飞单相交或需承接实体）**：
- **🔴 11.R2 的消费者锚已丢失**：`TestRecoverKVReadsARelocatedRoot` 被 **08.R3b 整文件删除**（git status `D`），**无同名后继** ⇒ 该叶的消费者验证面今日为空。**须补后继锚或明确裁定该面已由何锚承接**，不得因旧勾选推定仍成立。
- **11.R3 上报的 `landedSeq` 负锚仍未落**（`fact_commit_atomic_test.go` 三测无此断言），**无承接叶**。
- **07.R1 的 g2govtier 两 tier 锚（G2-G6/G2-G8）「须另开一单」台账零承接记录**（实质部分被 N16 写腿修复覆盖，主树锚仍缺）。
- **转移项无承接实体**：feedback 孤儿 R148 的 when/why 齐，但**无变更目录、无 LEDGER 承接条目** ⇒ **F4 归档须携带否则丢失**。
**另**：G28 指出顶层 `tasks.md` 的 R151 段「13 gap／midband 红」**已过期**（今 10 gap、midband PASS），须按 R164/R165 更新。

**G28 未查面（不得当已确认）**：全量 assembly/host/history/capabilities 包（只跑点名锚）、`TestEventIntegrityLongRun` 与六长 CI 矩阵、wechat-bot 全 smoke、其余约 170 个 diff 文件、05/06/08/09/10/13 域。

**proc-refs 红的归属裁定（编排者调查后裁）**：涉事三个文件（`scripts/gates/check_no_prefix_criterion.py`、`test_check_no_prefix_criterion.py`、`golden/runtime/no_prefix_criterion.json`）**全部未跟踪（`??`）**，属**另一个活动未归档变更 `no-prefix-criterion`**（其目录含 ORCHESTRATION/design/frozen-signature/proposal/specs/tasks 全套），且该判据**尚未转正进主 spec**（`openspec/specs/*prefix*` 零命中）。⇒ **它不是本变更候选的一部分**（不会被本变更提交携带），**本变更不夹带改它**。**正解归该变更自己**：把契约陈述进 docs/wiki 或主 spec 后把 `AUTHORITY` 指过去。**F2 评估须显式标注此红属另一变更的未提交工作，不得记为本变更缺陷，也不得为凑绿改它。**

**两单在飞**：G25／G26。**目标未达成，不得归档不得提交。**

### R173 G25 成效亲测：history 9 红→2 红（含安全回归闭合）；tasks.md 账目全面更新（2026-11-17）

**编排者亲测（不采信汇报）**：
- `go test ./tests/integration/history/ -run '^TestStorageCapabilities$' -count=1` ⇒ **EXIT=0**。**损坏记录重开不再被拒的安全回归已闭合**（R165 立案的最高优先项），且该测试的 `ErrBatchNotAtomic` 子测试（G24 成本表引为 file 档不可达证据）**同测全绿**，故 G24 的声明证据未被动摇。
- `go test ./tests/integration/history/ -count=1 -timeout 500s` ⇒ **EXIT=1，2 FAIL**（起点 9），**套件耗时从约 400s 降到 15.477s**——原 `TestEventIntegrityConsumptionRecovery`(90s)、`TestEventIntegrityRestart`(270s) 现快速通过，佐证 **6 条 `commit outcome uncertain: invalid argument` 同根红已被 G25 收掉**。
- **余 2 红**：`TestEventIntegrityRestoreUse`、`TestRestoreRefusesChainWithoutSpec`，均属 `recovery outcomes = []` 族 ⇒ **与 assembly D1（`host_delivery_test.go:2414`）疑同源，归 G26**（其令已要求核是否连带收）。

**⇒ Go 测试侧红现共 5 条，全部指向同一装配层根因面**：assembly 3（`TestAnchoredBootMatchesFullBoot`／`TestRestoreFoldCounts`／`TestRestartRecoveryPinsBeforeTrim`）＋history 2。**G26 是当前唯一关键路径。**

**`tasks.md` 账目全面更新（编排者单写，G28 指出的过期项已清）**：
- 13 域行的「13 gap／P4P5P6 未注册／G14 在收」→ **10 gap／P5P6 已注册并真跑 killed／G27 补夹具门分支 54/54／P4 待重绑**，并记 DoD 三处字段腐化已修。
- 「当前状态」段整块重写：**已闭合**列五阻断＋两症状锚＋损坏检测回归＋约 30 道绿门＋本轮编排者自收七项；**未闭合**列 assembly 3／dod 10 gap／godoc 2／product_surface 1／proc-refs 1（跨变更）／tracked-hygiene 13（暂存态）／package-layout 17（HEAD 既有＋2 新增）／长跑墙钟 2/3 破界（亚线性＋离散，`judgeCountedFaces` 与负对照均已通过）；**在跑两单**；**待派十二项按依赖序**（含 G28 查出的 11.R2 丢失锚、11.R3 landedSeq、07.R1 tier 锚、feedback 转移无承接实体）。
- **F2 顺序硬约束写明**：`tracked-hygiene` 须把删除入索引才转绿 ⇒ **全量 lint 必须排在暂存之后、提交之前**。

**两单在飞**：G25（余提交面收尾）／G26（装配层三红＋疑连带 history 2）。**目标未达成，不得归档不得提交。**

### R174 G28 最严重⚠️（11.R2 消费者锚丢失）经取证闭合：删的是已退役机制的锚，折回脚本正式入口锚存活（2026-11-17）

**G28 立案**：`TestRecoverKVReadsARelocatedRoot` 被 08.R3b 整文件删除（`tests/integration/history/recover_kv_relocated_root_test.go`，git status ` D`）且无同名后继 ⇒ 疑该叶消费者验证面今日为空。

**编排者取证后裁定：不是丢失验证面，是正确级联删除。**四步证据：
1. **读被删文件 HEAD 原文**（146 行）：其 doc 自述「**is the consumer anchor of fact retirement（runtime-recovery-ideal 08-fact-retirement 6.3）**」，夹具用 `ledger.StorageConfig{Type: StorageJournal, Root: root, **RetireFacts: true**}`，测的是「已结事实被**搬迁**后的根，正式修复入口 `scripts/recover-kv` 干跑 exit 0」。
2. **其subject已整体消失**：`grep -rn "Relocate\|RelocateReport"` 生产零命中；**`RetireFacts` 全仓（含 config）零命中** ⇒ 退位/搬迁机制已被 08.R3 整体删除（与 R141「08.R1/R2 及原 6.x 作废不得派发」一致）。**锚的量已不存在，删它是恢复真话。**
3. **08 域 tasks.md 仍在未勾叶 6.3／08.R1 里引它作消费者锚**—— those leaves 已标作废，引用属历史记录，**不得据其未勾推定验证面缺失**。
4. **折回脚本自身的正式入口消费者锚存活**：`scripts/recover-kv/main_test.go` 有 9 个测试，其中 **`TestRecoverKVCommandExitCodes`（:371）真 `go build -o binary .`（:721）后 `exec.CommandContext(ctx, binary, args...)`（:732）** ⇒ 它量的正是「shipped main 作为正式入口」这一消费者面，**与被删锚的脚本入口面重合，而搬迁面已退役**。
**⇒ 裁定：11.R2 的⚠️撤销，改判✅（其消费者面由 `TestRecoverKVCommandExitCodes` 承接，搬迁面随机制退役而作废）。** 编排者实跑该锚与全包取退出码记于本轮（见下）。**不得因旧勾选或旧锚名推定完成，也不得因锚名消失推定丢失——须查它量什么、那个量今天是否还在、谁在量。**

**方法教训（新增）**：**「锚被删」不等于「验证面丢失」**。须三步判：①读被删锚的 doc 与夹具，确定它量的quantity；②查该 quantity 今天是否还存在（机制退役⇒锚作废是正确级联）；③若 quantity 仍在，查是否有后继锚在量它（可能换名换包）。**G28 只做了「同名后继是否存在」这一步，故误判为丢失。**

**两单在飞**：G25／G26。**目标未达成，不得归档不得提交。**

### R175 07 域四条正确性缺陷立案为未闭合纯度缺口（含 P1 红线）；三条验收面勘误＋一条设计分叉已裁并落 design（2026-11-17）

**编排者读 `07-governance-custody/design.md` §十五确证：本域 G2 部分＝FAIL，四个真正确性缺口在隔离树以可复跑退出码实证，但从未在主树修复或落锚**：
- **G2-G8 已存储治理事件被重铸号覆盖 ⇒ 触 P1「SHALL NOT 删/改已提交事实」红线**（单一真相）。
- **G2-G10 切面权威被目录第二来源 defeating ⇒ 就是第二真相**（`Lookup`→`rebuild` 从 `<dir>/approvals` 补回，量到的不是切面）。
- **G2-G3 人的拒绝跨重启失效**；**G2-G6 历史读静默变短**。
隔离树 `go test ./g2govtier/ -run '^(TestG2GovTailHoleRekeysOverAStoredEvent|TestG2GovHistoryQueryNamesItsBoundary)$' -count=10 -v` ⇒ **1，20 RUN/20 FAIL**（真实 localfile 档）。**主树 `grep g2govtier|G2-G6|G2-G8` 零命中 ⇒ 两枚锚与修复均未落主树。**
**⇒ 立案：这四条是本变更未闭合的纯度缺口。取证只证明缺陷存在，不等于已修；不得因「设计门已取证」把它当已闭合。** 尤其 G8/G10 直接违反目标第三句要求的单一真相/无第二真相。

**编排者裁定五条，已落 `07-governance-custody/design.md` §十五之补（不只活在战报）**：
1. **冻结负锚 `TestGovernanceSliceRefusesToDropAnOpenApproval` 须改写为「原审批身份仍可裁决」**。现判据「仍 park 或具名拒绝」**恒成立**（闸门对缺失审批铸新请求并 park，policy.go:359-361），**区分不了原 hold 存活与新铸 hold**；实测闸门点名新铸 `approval-0d9b937a…` 而非原 `g2-open-hold`，正是假锚证据。按「anchor 为空＝没门在量它」，**永不可能红的断言不是在量东西**。G2-G4 已独立表达新判据且 RED ×10 ⇒ 有区分力。**这是收紧不是放宽**，不触「不得改锚迁就回归」（那条禁的是为产品转绿而削弱断言）。
2. **该锚必须显式撤走 `<dir>/approvals`**，否则第二来源 defeating（即 G2-G10）；沿用 `checkpoint_test.go:504` 既有纪律。**夹具须自证「量的是切面」而非仅「结果是 park」。**
3. **处置清单「六处」按实测改写为 29 测试消费点＋3 编译期形状断言**（依数字溯源/未跑不勾，以 §十一答 3 逐文件表为准）。
4. **G2-G3 设计分叉取「按身份可查的链上 answer」**，不取「切面保留已结项直到显式清结」。推导：链唯一真相（P1）＋已冻结「检查点只携开放项」＋**已有同形先例**（任务账已结项由 `hydrateLocked` 按名索取时从链读回，G19/G22 已实证「水合后快照仍 0 条」）⇒ 治理审批已结 answer 走同一形态：**闸门需要时按身份从链读，不让切面常驻**。候选乙不得实施的既有理由仍成立（已结未过期项决定闸门 policy.go:336/:342），本裁定正是让闸门**读得到**而非裁掉。**实施前置**：先取证链上确实携带可判定的拒绝 answer（若今天只写切面不落链，须先补落链＝产品改动）；按身份读的成本归闸门不摊 boot。
5. **`g2govtier/` 新顶层包改道批准但暂不派单**：两枚 tier 锚引 `memory/ledger` 真实 localfile 档而该包正被 G25 重写；且 G2-G8 须与洞语义具名化同批（既有放行建议「07.R2 候选甲不得先于 G2-G8 落地」，因今天全量 `Records` 是洞的掩盖层，拆掉它就把重启丢历史＋重铸号覆盖变成永久缺陷）。**待存储面收口后另开一单**，写面含 `g2govtier/`＋`features/evolution/governance/g2_valid_state_anchor_test.go`，同批处置 G2-G10。

**07.R1 仍不勾**（主树无该两文件，隔离树证据不得冒充主树落盘）——这是正确纪律，G28 记的「无承接记录」现由本条裁定五承接。
**⇒ 待派清单新增一项（高优先，纯度级）**：治理四缺陷修复＋十一枚锚落主树＋第二来源处置。**它不是成本问题，是 P1 红线与第二真相问题。**
**两单在飞**：G25／G26。**目标未达成，不得归档不得提交。**

### R176 纯度五判据对主树系统审计（目标第三句的逐项结论，非反应式打地鼠）（2026-11-17）

**判据原文见 R137／R121.3／R135。逐条对今日主树给结论与证据等级**：

**① 链唯一真相（已结态只留链上）——⚠️ 部分成立，治理面有主树确证违例**
- ✅ 任务账：检查点只携开放项，已结项由 `hydrateLocked` 按名索取时从链读回；G22 实证「水合后再快照＝0 条」，G19 实证 boot 仍 `tasks=0 reads=0`。
- ✅ memory 档两引擎已消（G23 删特异分支，与 durable 逐字同构）。
- **🔴 治理面主树确证**：`features/evolution/governance/policy.go:359-361`——`req == nil` 时 **`p.approvals.Request(..., newApprovalRequestID(), ...)` 铸新请求**。⇒ 切面查不到原审批就**另铸一个身份**，原拒绝/批准的终局性不由链上按身份可查的 answer 承载。**这是编排者本轮亲读主树代码确证，非仅隔离树推断。** 它同时是 G2-G3（人的拒绝跨重启失效）与假锚（区分不了原 hold 与新铸 hold）的同一机理。
- ⚠️ G2-G8（已存储治理事件被重铸号覆盖，触 P1 SHALL NOT 改已提交事实）：**隔离树 20 RUN/20 FAIL 实证**，主树写入路径尚未由编排者逐行定位 ⇒ 标⚠️不标✅，**不得因未定位就当不存在**。

**② 派生载体不迫使全量读——✅ 耐久路径成立，兜底路径本征且已显式声明**
- ✅ journal/localfile 档走 `CommitOrder` 能力：`indexWalkVisits`／`indexWalkValueReads` **252 代恒 4**；**`judgeCountedFaces` 已通过**（G15 `-count=3` 中 PASS 那次）；长跑负对照 `historyProportionalCost` 通过（判据能具名拒绝随历史增长的序列）。
- ✅ 兜底路径（引擎不声明序）全量物化＋排序、早停不约束读取量：**G24 已补成本表声明＋深度界锚**（深度 200/2000 实测 reads 225/2250 严格线性，控制案具名红），**钉住线性事实而非假装有界**。
- ⚠️ 墙钟面长跑 `-count=3` FAIL/FAIL/PASS：boot 252 代 **1.4–1.6× 亚线性**＋参考深度离散 ~3%，**非 ~252× 本征 O(历史)**；亚线性成本与噪声的分离须 profiling（09.R2）。**不得调大界、不得删离散守卫。**

**③ 无第二真相——⚠️ 一处已消，两处仍在**
- ✅ memory 档 Originals/Segments 共用一引擎（G23），**未走 `load()` merge 掩盖**。
- **🔴 G2-G10 治理切面权威被目录第二来源 defeating**：`Lookup`→`rebuild` 从 `<dir>/approvals` 补回原 hold，使「量切面」的测试实际量到目录。第二来源在 `runtime/task/approval` 面（governance 只调 `Lookup`，policy.go:334）。**R175 裁定二已要求夹具显式撤走该目录，但产品面的第二来源本身未处置。**
- **🔴 `NewMemoryOriginalStore` 仍作为 test-only 导出函数存在**（`check_product_surface` 判红），生产调用方已归零 ⇒ 死结构未删，属残留（R162/R167 立案，待 G25 落地后派）。

**④ 更少特异化代码——✅ 本轮显著收敛**
- memory 特异分支删除（-12 行，与 durable 同构）；三个同族持久面端口收口为 `api/durable.FactLog` 一个；`runtime/state/migrate.go` 整文件＋四孤儿端口删；退位机制与切面族整体删；`cli` feedback 自证链删；24 条真死生产者删。
- ⚠️ 但**八面按名索取未走水合**（R161）是覆盖缺口而非多余结构；`newestSessionCheckpointRef` 零生产调用方仅测试调用，**死代码门按设计抓不到**，须 F1 判定。

**⑤ 本征成本归索取者并显式声明——⚠️ 一处已声明，一处未声明**
- ✅ 兜底全量物化：G24 成本表已声明本征界＋无现时耐久受害者＋重开条件。
- **🔴 懒水合成本未声明**：G22 实证 `Records`→`readTaskLocked` 做 **5 次前缀 scan**，`eachHeld`→`eachRecordOnce` **整表顺序读 `a.capacity` 槽**再过滤 ⇒ 首次按名索取付 **O(历史键容量)×5**，之后 `d.facts` 命中不再读。**未列入任何成本表 ⇒ 按「未列入视为未承诺」属未声明**（R161 立案）。

**⇒ 总结论（对目标第三句）**：** purity 未完全落实。** ②④ 基本成立且有机械证据；①③⑤ 各有**主树确证或实证过的违例/未声明项**，集中在**治理审批面**（铸新身份、目录第二来源、重铸号覆盖）与**水合成本声明**、**死结构残留**三处。**这些不是成本问题，是单一真相/第二真相/显式声明问题，按用户「不考虑成本只考虑优雅纯粹」的裁定必须收，不得以成本读数正当化保留。**
**修复归属**：治理面待 G25/G26 落地后按 R175 五条裁定派单（含 `g2govtier/` 新包）；水合成本声明与八面水合可合单；`NewMemoryOriginalStore` 删除单独一单。
**两单在飞**：G25／G26（G26 本轮又收 `TestAnchoredBootMatchesFullBoot`，assembly 余 D1/D2 两条）。**目标未达成，不得归档不得提交。**

### R177 G25 收口（history 9→2 亲测互证）；但其修法引入 boot O(历史) 疑点，立案派 G29 取证（2026-11-17）

**G25 收口，编排者采纳其反驳**：损坏检测回归的真凶**不是**编排者疑的「只读面三成因逻辑」，而是**水位跳过帧 CRC 校验**——整洁关闭经 `addr.close→flush(true)` 把水位推到 log 末尾，重开窗口为空 ⇒ 水位之下的损坏帧从不被校验 ⇒ 静默接受。**HEAD 无此回归**（旧 index-cut 只在退休重写时写，普通关闭不写 cut，故全量回放）。
提交 EINVAL 真凶＝**nil 写句柄**：`WriteRights=false` 开只读面时新根 `log.file=nil`，`Commit→append→l.file.Write` 返 `os.ErrInvalid`，于 `journal_kv.go:199` 包成 `ErrCommitUncertain`。**非「视图已闭」、非偏移非法。**
修法：①开档对全 log 做 CRC 完整性回放，索引回放仍只读窗口；三成因各有判据（尚未创建＝log 缺且无 order.log/slots⇒空视图；曾存在被删＝log 缺但有索引⇒`ErrJournalCorrupt`；记录损坏＝log 在且 CRC 败⇒`ErrJournalCorrupt`）②`takeWriteFaceLocked` 首个带键提交懒取写面（先开写面再释读面，失败保留读面），**只读开档不建文件的承诺不变**。
**凭据**：build/vet 0；`TestStorageCapabilities` 0；ledger 全套 0；**history FAIL 9→2**（编排者独立亲测同数，且套件耗时 400s→15.5s 互证根因已修）；两负锚回退具名红＋还原 `diff -q` 一致；正锚 `-count=3` 0、`-race -count=2` 0；comment_policy/concept/dead_producers/vocabulary 均 0。**`ErrBatchNotAtomic` 子测试因本机无 rustviking 二进制 SKIP（资源门控，基线即如此，非红）**⇒ G24 成本表引它作证据须标注「该子测试在本机 SKIP，CI 有二进制时才真跑」。
**G25 自决采纳**：`openBytes`/`JournalBytesOpen` 明确为「索引回放窗口」量，完整性校验另计，doc 已澄清（:581-582）——**这是诚实声明而非隐藏**。另其修好提交后暴露 `restore_refuses` 第二子测试红于 **:172**（query 拒读 invocation 维，源 `memory/query/inventory.go:151` 包 `runtime/task.ErrSpecMissing`）而非基线 :144 EINVAL ⇒ **下游装配/query 语义问题，归 G26，非存储层**。

**⚠️ 编排者立案：该修法把本征历史成本摊进 boot，与判据⑤冲突**
- **确证**：`journal.go:615` `journalReplay(file, 0, size, no-op)` ⇒ **每次开档读全 log**。doc :581-582 明言完整性校验读全 log、不是 `openBytes` 那个窗口。
- **冲突**：判据⑤「本征 O(历史) 归索取者并显式声明，**不得摊进每次 boot**」。G25 的理由「CRC32C 硬件加速使墙钟界不破」是**成本读数**，按用户裁定「不考虑成本只考虑优雅纯粹」**不得正当化违纯度形态**。（与 R176 判据②⑤的结论一致：墙钟绿不等于纯度成立。）
- **可能更纯的解（待取证）**：帧自描述 CRC（doc :4-7），**写路径 :494 计算 CRC、读路径 :514 校验 CRC**，水位只推过已 `Sync()` 的帧（:1546/:1630-1633）⇒ **水位之下字节在写入时正确**，其后损坏只在被读到时暴露。**若每条读路径都过 CRC 校验，则读时校验已足以保证损坏永不被静默接受**，无需开档全扫；差别只是拒绝时机。
- **派 G29 只读取证五问**：①枚举全部读路径逐条判是否过 CRC（含点读/批量扫描/外部脚本/任何绕过帧解析的 `ReadAt` 直读）②水位推进是否保证之下帧已完整写入并 Sync（崩溃/torn tail/部分写路径）③**torn tail 与损坏的区分**（安全关键：不得把损坏降级为尾部不完整而静默丢弃）④新锚断言的是「开档这个动作须拒绝」还是「损坏须被拒绝」，若改读时校验须如何重表达仍钉住同一安全性质，并查是否有别锚/主 spec 冻结「开档即校验全 log」⑤两方案成本归属量化对比（**注意懒水合首次已付 O(历史键容量)×5，方案乙会否再叠一层**）。
- **纪律**：**这是安全契约与纯度的真分叉，不得为纯度削弱安全性质**（损坏必须具名拒绝，绝不返回空视图或 `(false,nil)`）。G29 只取证不裁；若证据只支持一条路须明说另一条被什么事实否掉。**编排者待其报告后裁，必要时呈用户。**

**当前并发两单**：G26（装配层 D1/D2，本轮已收 `TestAnchoredBootMatchesFullBoot`，assembly 3→2）、G29（只读）。**目标未达成，不得归档不得提交。**

### R178 G25 收口释放 memory/ledger 写面；派 G30 收 product_surface 红门（零残留删除）（2026-11-17）

**派 G30**：删 `NewMemoryOriginalStore`（G23 删 memory 特异分支后其生产调用方归零，`check_product_surface` 已判红强制「internalize it or record a named waiver」）。**编排者裁定：删，不走 waiver 旁路**——豁免死结构等于把它合法化，违「零残留＝框架内不留任何兼容分支」。
白名单：`memory/ledger/original_store.go`、`segment_store.go`（**仅 `factOriginalsOf` 及其直接级联**）、`reference.go`（仅断言块）、`tool/recall/skeleton_assert.go`（仅相关断言块，**不得整文件删**，它是每包恰一份的正名骨架）、G23 点名的 5 个测试文件＋复核发现的其它引用者、`kv_original_store.go`（仅若构造入口需改，**不得改 `load()` 覆盖语义**）。
令内关键约束：①**`newMemKV`/`newEmptyKVOriginalStore` 仍活不得连带删** ②`MemoryOriginalStore` **类型本身**删还是留须给证据（若 `newEmptyKVOriginalStore` 仍返回它则只删构造器）③`factOriginalsOf` 简化为单 case 后若退化为无意义包装，须**先枚举全部调用方**再决定内联，不得顺手扩范围 ④迁测试须**保持原断言语义**，若某测试量的本就是「独立原文仓与段存储分离」这一**已被判为缺陷并删除的形态**，须报编排者裁定是否作废，**不得自行删它** ⑤负锚：临时把构造器加回须使门重新具名红，还原 `diff -q` 一致。
**禁改** `journal.go`/`journal_kv.go`（G29 正取证其开档全 log 校验的纯度问题，可能有后续单）、`config.go`、assembly/runtime（G26 面）。

**并发三单**：G26（装配层 D1/D2＋history 余 2 红＋新暴露的 :172 query 语义）、G29（只读，完整性校验读时化可行性）、G30（memory/ledger 死结构删除）。**触碰集不相交**：G30 写面与 G26 的 assembly/runtime 无交集，G29 零写入。
**仍未派（写面相交或需前置）**：八面懒水合（`runtime/task`＋`assembly/read.go`，与 G26 同 assembly 包构建闭包）；治理四缺陷＋十一锚落主树＋`g2govtier/` 新包（须待存储面收口，且 G2-G8 须与洞语义具名化同批）；水合成本界声明（可与八面水合合单）；兜底路径生产侧观测缺口（`segment_store.go`，G30 正在改该文件的 `factOriginalsOf`，须待其收口）；`package-layout` 17 条（跨多包，须待 G26/G30 落地）；P4 退役重绑（锚文本在 `journal.go`/`run.go`，须待 G29 裁定与 G26 收口）；九变异实跑（须控制树全绿）；godoc 统一重生成（须待全部代码落地）。
**目标未达成，不得归档不得提交。零 git 写，HEAD 仍 `9547faca`。**

### R179 完成审计：目标三句逐项映射到证据（结论＝未达成，缺口具名）（2026-11-17）

**目标原文三句**：①整理计划，并行派发推进任务优雅完成 ②如果任务已经派发，review 已实现部分，核对是否有违规概念和词汇 ③思考是否不考虑实现成本只考虑架构优雅纯粹性落实执行。

**句① 整理计划／并行派发／优雅完成**
| 要求 | 证据 | 判定 |
|---|---|---|
| 整理计划 | `tasks.md` 当前状态段按实测整块重写（R173）；13 域行过期数字已清；F2 完整门禁面逐门实测清单（R149/R169）；待派十二项按依赖序＋每项前置原因（R178）；台账 R0–R178 连续 | **✅ 达成** |
| 并行派发 | 全程按触碰集不相交派单；全相交时明写「N 单即最大并发不凑数」（R165/R178）；本轮三单 G26/G29/G30 写面互不相交 | **✅ 达成** |
| 任务优雅完成 | assembly **2 红**（D1/D2）＋history **2 红**（同族）；六门红（dod 10 gap／godoc 2／product_surface 1〈G30 在收〉／proc-refs 1〈跨变更〉／tracked-hygiene 13〈暂存态〉／package-layout 17）；治理四缺陷未修；F1 完整反夹带未做；R35 第二轮未做；F2/F3/F4 未做 | **❌ 未达成** |

**句② review 已实现部分／核对违规概念词汇**
| 要求 | 证据 | 判定 |
|---|---|---|
| review 已实现部分 | 逐单读 diff 并亲测复核：G17（兜底改提交序，负锚翻向复现三红）、G19（`hydrateLocked` 扩覆盖面，两不变量守住）、G23（删特异分支使 memory 与 durable 同构）、G25（水位跳过 CRC 真凶＋nil 写句柄）；**A/B 同树对照归因**多次；**抓到并纠正多次假绿**（后台通知 exit 是包装 shell 码／空日志零红／`-run` 零匹配／自测绿≠门面绿／zsh 不分词／G10 的 package-layout 假绿） | **✅ 达成** |
| 违规概念 | `check_concept_budget` **0**，`baseline_stale: 0`（78 条豁免无陈旧者）；三处 DoD 判据字段腐化已修（R172）；`check_durable_truth_registry` 已删机制登记已清（R167） | **✅ 达成** |
| 违规词汇 | `check_vocabulary_alignment` **0**；`comment_policy` **棘轮三模块 0 findings／产品面 0**；`name-check` 0；2213 行新增注释扫泄漏仅 3 处「本次」且全为领域语义 ⇒ 零真泄漏（**但曾漏扫测试文件被门抓到 `no longer`，已修并记教训：自扫窄于门不得声称已清**） | **✅ 达成** |
| 裁决归因真话 | G28 查出裁决面扩大两处（落点/3 方法/净减 2 被写成用户已裁），**已就地勘误**（R172）；N1 伪造裁决已纠 | **✅ 达成** |

**句③ 不考虑成本只考虑优雅纯粹并落实执行**（逐项见 R176 五判据审计）
| 判据 | 状态 | 缺口 |
|---|---|---|
| ①链唯一真相 | **⚠️ 部分** | 治理面 `policy.go:359-361` 查不到审批即**铸新身份**（主树亲读确证）；G2-G8 重铸号覆盖已存储治理事件触 P1 红线（隔离树 20/20 FAIL 实证，主树写入路径待定位） |
| ②派生载体不迫使全量读 | **✅ 耐久路径** | journal 档 `indexWalkVisits` 252 代恒 4、`judgeCountedFaces` 通过、负对照通过；兜底全量物化已由 G24 声明本征界＋深度锚钉线性事实。**⚠️ 但 G25 新加的开档全 log CRC 使 boot 付 O(历史)，G29 正取证能否改读时校验** |
| ③无第二真相 | **⚠️ 部分** | memory 档两引擎已消✅；**治理目录第二来源 defeating 切面权威未处置**；`NewMemoryOriginalStore` 死结构仍在（G30 在删） |
| ④更少特异化代码 | **✅ 显著收敛** | memory 特异分支删、三端口收口为一、migrate.go 整文件＋四孤儿端口删、退位机制整体删、cli feedback 自证链删、24 死生产者删。⚠️ 八面按名索取未走水合是覆盖缺口；`newestSessionCheckpointRef` 零生产调用方待 F1 判 |
| ⑤本征成本归索取者并显式声明 | **⚠️ 部分** | 兜底界已声明✅；**懒水合首次付 O(历史键容量)×5 未列入任何成本表＝未声明**；开档全 log CRC 摊进 boot（同②） |
| 成本不得正当化违纯度形态 | **✅ 已执行** | 明确驳回「CRC32C 硬件加速使墙钟绿」作为保留 boot O(历史) 的理由（R177）；驳回以 waiver 豁免死结构（R178）；长跑界**禁调大禁删守卫** |

**⇒ 审计结论：目标未达成。** 句①的「整理计划／并行派发」与句②全部达成且有机械证据；**句①的「优雅完成」与句③的「落实执行」未达成**，缺口具名为：
1. **装配层恢复缺陷**（assembly 2＋history 2 同族，G26 在修）
2. **治理四正确性缺陷**（G3 拒绝跨重启失效／G6 历史读静默变短／**G8 触 P1 红线**／**G10 第二真相**），R175 五条裁定已落 design，待存储面收口后派单
3. **开档全 log CRC 的 boot O(历史)**（G29 取证中，安全与纯度真分叉，不得为纯度削弱安全）
4. **八面懒水合未覆盖**（`Obligations` 现时可达靠隐式顺序兜住）＋**水合成本未声明**
5. **死结构残留**（G30 在删）＋`newestSessionCheckpointRef` 待判
6. **六门红**（其中 proc-refs 属另一变更、tracked-hygiene 属暂存态、package-layout 属 HEAD 既有＋2 新增）
7. **dod 10 gap 真跑侧**（须控制树全绿）＋P4 退役重绑
8. **F1 完整反夹带**（196 文件核心 diff 未逐行审）／**R35 第二轮独立放行审**／**F2 全量门禁**／**F3 逐项反例核销**／**F4 归档拒绝有效性**
9. **转移项无承接实体**（feedback 孤儿，F4 归档须携带否则丢失）

**不得以「大部分门绿」「五阻断闭合」「两症状锚绿」作为达成证据**——它们只覆盖句②与句③的一部分，**句③的 ①③⑤ 三判据各有主树确证或实证过的违例**。**归档与 commit/push 须用户明示授权，本轮不执行。**

### R180 回归核：两症状锚在存储/装配改动后仍绿；assembly 2→1 红，余红全属 D1 同族（2026-11-17）

**编排者亲测回归核（不靠旧证据）**：G25 改开档完整性校验、G26 改装配层之后——
- `TestSymptomAnchorBootCostFlatAcrossGenerations` `-count=1 -timeout 250s` ⇒ **EXIT=0**（12.694s）。**⇒ 开档加全 log CRC 未破 boot 墙钟界**（与 G25 自报一致，编排者独立复核）。**但这只是成本读数，不解除 R177 立案的判据⑤冲突**（boot 付 O(历史) 字节读），G29 取证中。
- `TestSymptomAnchorMidbandProcessRecalledByTicket` ⇒ **EXIT=0**（0.460s）。**两条症状直译锚仍绿。**
- assembly 定向两红 ⇒ **1 FAIL**：余 `TestRestoreFoldCounts`（`host_delivery_test.go:2414 recovery report = [], want the one chain rebuilt`）。**`TestRestartRecoveryPinsBeforeTrim` 已由 G26 收掉**（D2 负对照重表达成功）。

**⇒ Go 测试侧余红共 3 条，全属 D1 同族（boot 恢复报告为空）**：assembly `TestRestoreFoldCounts` ＋ history `TestEventIntegrityRestoreUse`（:4912）＋ `TestRestoreRefusesChainWithoutSpec`（:101，另 G25 修好提交后暴露的第二子测试 :172 query 拒读 invocation 维）。**G26 是唯一关键路径，跨包同族即同根，一张单收。**

**门禁现态**：`check_product_surface` 仍 **1**（G30 在删 `NewMemoryOriginalStore`，尚未落盘）；其余同 R179。
**三单在飞**：G26／G29（只读）／G30。**目标未达成（R179 审计九项缺口），不得归档不得提交。零 git 写。**

### R181 派 G31 收治理可先行最小面（R175 裁定的执行单）；并发四单（2026-11-17）

**G25 收口后 `features/evolution/governance` 不在任何在飞写面内**，R175 五条裁定已落 design，故派 G31 执行其中**可先行三面＋两条锚纪律**：
1. `Query(0)`/`Count()` 窗口具名化（G2-G6 历史读静默变短转绿）
2. 写腿失败具名化（G2-G9，**沿用既有通道不新造只喂日志的出口**）
3. 审批身份与别名跨切面存活（G2-G4/G5，G3 的可先行部分）——**须先取证链上是否携带可判定的拒绝 answer**（R175 裁定四的实施前置）；**若链上不携带则停下来报「须先补落链」，不得自行发明落链语义**（设计级改动须编排者冻结）
4. 加强假锚 `TestGovernanceSliceRefusesToDropAnOpenApproval` 判据为「原审批身份仍可裁决」（收紧非放宽）
5. 该锚夹具**显式撤走 `<dir>/approvals`** 并自证「量的是切面」而非仅「结果是 park」

**明禁**：不做 G2-G8 重铸号覆盖完整修复、不做 G2-G10 目录第二来源产品面处置、不建 `g2govtier/` 新包——**须另单同批**，因 design 明载「07.R2 候选甲不得先于 G2-G8 洞语义具名化落地，今天全量 `Records` 是洞的掩盖层，拆掉它就把重启丢历史＋重铸号覆盖变成永久缺陷」。**令内要求：若三面中任何一面触碰该掩盖层，停下来报不得拆。** 另候选乙（只裁审批面为未过期未结）不得实施（已结未过期项决定闸门 policy.go:336/:342）。
**落锚纪律**：新锚须落主树、每面正锚＋负锚且负锚真能红、**不得落恒成立假锚**、**新增测试文件不得使 package-layout 的 test-mirror-missing 多于起点 17 条**、不得新增 test-only 导出（product_surface）。隔离树原型只作参考，**不得把隔离树证据当主树落盘**。

**并发四单，触碰集不相交**：G26（assembly＋runtime/state）／G29（只读 memory/ledger/journal）／G30（memory/ledger 死结构＋tool/recall）／G31（features/evolution/governance；若需改 `runtime/task/approval` 须先报）。
**⇒ 纯度缺口①③的治理面开始有执行单**，但 G8/G10 仍待另单同批，**R179 审计的未达成判定不变**。
**目标未达成，不得归档不得提交。零 git 写。**

### R182 G29 决定性反驳编排者前提：保留开档全 log 校验（安全必需），但须声明成本＋修恒绿假锚；另查出既有数据丢失洞（2026-11-17）

**G29 反驳编排者前提（采纳并勘误 R177）**：我引的 `journal.go:514` CRC 校验在 **`journalPayloadOf`，全树含未跟踪 grep 零生产调用方**（仅 `journal_test.go:595`）。**生产读路径的 CRC 在 `:554`（`journalReplay` 内）**。⇒「写路径算 CRC、读路径 :514 校验，故读时校验已足够」这个推导**前提错**。

**方案乙（仅删全扫、靠读时校验）不安全，三个事实否掉它**：
1. **存在唯一且承重的绕过点**：`addr.value:1654` → `a.log.file.ReadAt(token, span.Offset):1660` ＋ `json.Unmarshal`，**无 CRC**。它是**一切取值的唯一出口**（点读 `journal_kv.go:279→log.read:2165→addr.read:1644`；前缀 `:80→valueOfSpanLocked:263`；遍历 `:329→eachNewestFirstValue:1861`）。`recover-kv`/`fold-cold-root` 全走 `OpenTier`/`OpenAtomicStore` 无独立字节读。⇒ **「每条读路径都过 CRC」今日为假**。
2. **锚的变异落在值粒度之外**：`journalTestFlipLastByte:1924` 翻的是文件末字节＝payload 的 `}`，**落在一切 value span 之外** ⇒ 值粒度读时校验**原理上看不见它**，重表达后该变异恒绿＝**假锚**。故校验粒度必须是**帧**而非值。
3. **冻结契约面要求开档即拒**：`journal.go:4-9` 包 doc（HEAD 逐字同）、`wiki:162`「完整帧校验不过即拒开」（HEAD 已有）、`golden/durable_truth.json:128`、`TestJournalFraming/rejects_corrupt_middle_frame:119`、`storage_capabilities_test.go:239`。**改时机须裁这些面，不是改一个锚。**
**另**：水位只在 `fileCommit:1283` 前进，其调用方 `confirm:1199` 在 `file.Sync()` 之后、`derive:1164` 在 CRC 回放之后 ⇒ **无推过未 sync 字节的路径**（编排者该句前提成立）；**但「写入时正确」≠「此刻完整」**，水位不能为之下字节的当前完整性背书 ⇒ 该前提**不支持**乙的结论。
**乙的代价也不是小改**：`journalAddressSpan:790` 只有 `{Offset,Length,Null}` 无帧起点 ⇒ 帧级 CRC 须读整帧 F，点读 V→F（按 64B 值/5 键提交推导 **~6×**；checkpoint 帧近 32MiB 时读一个值付整帧），且须 **`journalSlotVersion` 2→3 格式升级**（span/spanOf/encodeSlotRecord/decodeSlotRecord/journalSpansAt/orderStatementOf 全链）。替代变体（值 CRC 入 span）保 O(V) 但**在同一批字节上立第二份校验和，撞判据③④**。

**⇒ 编排者裁定：保留甲（G25 的开档全 log 完整性校验）。** 这不是「以成本正当化违纯度」，而是**安全必需的帧级校验＋冻结契约要求开档即拒**；乙被三个事实否掉。**但判据⑤仍须满足：该 O(日志字节) 成本必须显式声明，且量它的锚必须真能红。**

**🔴 随之确证的两处必修（派单）**：
1. **恒绿假锚**：`assembly/symptom_anchor_recovery_test.go:465` 断言 `quantities.journalBytes != 0` 即 fatal，而 `JournalBytesOpen` **只报索引回放窗口** ⇒ 停稳根报 0 而**实读 H（全 log）**，**该锚量不到主成本、恒绿**。按「锚须钉线性事实非假装有界」，须**把该锚明确限定为索引窗口量，并另立一个钉住完整性校验字节随日志线性增长的量与锚**（不得删原锚削弱它，也不得让它继续冒充总成本）。
2. **成本表未声明**：开档完整性校验的 O(日志字节) **未列入任何成本表** ⇒ 按「未列入视为未承诺」属未声明，须补（与 G24 兜底行同表）。
**✅ 编排者已亲收**：`docs/wiki/memory/memory-architecture.md:253` 原写「打开只重放…窗口（停稳根实测读 0 字节），**不再重放整份日志**」——**已被 G25 变成假话**，已改写为真话（索引回放只读窗口，但开档另对全 log 每完整帧做 CRC，因水位只陈述写入时已 sync、不背书此刻完整性；故开档实读字节随日志增长，`JournalBytesOpen` 只报窗口量不报完整性量），并把该成本列入显式声明面。

**🔴 另立案：既有数据丢失洞（HEAD 同形，非本变更引入，但属安全级）**
- `journal.go:544`：`length > journalMaxFrameBytes || size-offset-journalFrameHeaderBytes < int64(length)` ⇒ **`return offset, nil`（一律当 torn tail）**。
- **后果**：**长度域被翻大（单比特 0x00→0x80 即可）被降级为「尾部不完整」**，写面 `:623` `Truncate` **静默切掉该帧及其后全部历史** ⇒ 数据丢失且无具名拒绝。
- **判据不一致**：`journalPayloadOf:506` 对同一情形（`length > journalMaxFrameBytes`）判 **`ErrJournalCorrupt`**。**两函数对同一损坏给相反判决。**
- **归属**：`HEAD:855` 同形 ⇒ **既有缺口**。按 F1 不夹带，**本变更不顺手修**，但**它是安全级数据丢失，须高优先另立变更**（when：本变更归档后立即；why：它先于本变更存在，且修它须统一两函数判据＋区分「真 torn tail」与「长度域损坏」，属存储格式语义裁定）。**G29 未做变异实跑（仅代码＋HEAD 对照），故标⚠️实证待补。**
- **注意**：甲的全扫把该洞的暴露面扩到全历史（乙只暴露在窗口）——**这是保留甲的已知代价，须一并声明**，不得只说甲安全。

**四单在飞**：G26／G30／G31／G32（F1 核心 diff 只读）。**目标未达成，不得归档不得提交。**

### R183 编排者亲收：开档完整性校验成本已显式声明（判据⑤该条闭合），假锚仍待派（2026-11-17）

**已落 `06-bounded-open-index/design.md` §十九·七 成本表**（与 G24 兜底行同表，既有行逐字未动）：
- 新增行「store 开档完整性校验（全 log 帧 CRC）」＝**O(日志字节)，每次开档付**；明写它与索引回放窗口是**两个量**，窗口停稳根为 0 而本行不为 0；实测依据指 `journal.go:615`，并**点名 `symptom_anchor_recovery_test.go:465` 的 `journalBytes==0` 锚量不到本行成本、恒绿**，标 R182 立案待派；**明写「墙钟绿是成本读数，不解除本行声明义务」**。
- 附声明块五条：①为何保留全扫（G29 三事实：承重绕过点 `addr.value` 无 CRC／变异落在 value span 之外故事值粒度校验必造假锚／冻结契约面要求开档即拒）②水位为何不能背书（只在 Sync 后前进，但「写入时正确」≠「此刻完整」）③改读时校验的代价（span 无帧起点⇒点读 V→F 约 6×、checkpoint 帧近 32MiB；须 slotVersion 2→3 格式升级；值 CRC 入 span 变体**立第二份校验和撞判据③④**）④**已知代价须一并声明**：`:544` 长度域翻大被当 torn tail→写面 Truncate 静默切该帧及其后全部历史，而 `:506` 同情形判 `ErrJournalCorrupt`，两函数相反；HEAD 同形属既有缺口，**全扫把暴露面从窗口扩到全历史**⑤判据引 R135⑤/R121.3，**本行是摊进 boot 的成本故必须声明而非声称有界**，索取者是「开档须拒绝损坏」这一安全契约本身。
**复测**：`check_doc_budget` 0（棘轮未破）、`check_doc_links` 0、`check_vocabulary_alignment` 0。

**⇒ 判据⑤的该条缺口闭合（已声明）；但配套假锚未修，故「量它的锚真能红」这一半仍未达成**，R182 立案的锚修复待派（写面 `assembly/symptom_anchor_recovery_test.go`，与 G26 同 assembly 包构建闭包，须待其收口）。
**四单在飞**：G26／G30／G31／G32。**目标未达成（R179 九项缺口），不得归档不得提交。零 git 写。**

### R184 G26 收口两条（夹具态，取证生产调用方）；红二撞冻结不变量＝真设计分叉，精确立案待裁（关键路径）（2026-11-17）

**G26 收口，编排者采纳其两条自决与一条反驳**：
- **红一 `TestAnchoredBootMatchesFullBoot`＝夹具人为态，非产品缺陷**（**反驳 G15 的定性，采纳**）。取证：`seedAnchoredReplay` 的生产唯一调用方是 `seedReplay`（`run.go:2449`，boot 一次），且 boot 内 `bootIndexWalk` **先于** `takeRestoredProjection`，故首次重建时 projection 仍在、有界 walk 健全。**只有测试在已 boot 的 run 上二次手动播种**才产生「projection 已消费＋standing cut」态，**无生产消费者走此路径**。按「夹具需求不得驱动生产设计」修夹具（source 改 `endRun`＋四个对比 run `stopCheckpointFiler`），**未给生产加兜底分支**。负锚：还原 source.Close ⇒ 红 `:190`，修复还原 md5 一致；正锚 `-count=3` PASS。
- **红三 `TestRestartRecoveryPinsBeforeTrim`＝夹具重设计成功，pin 真被行使**。原负对照因检查点 survivor 使 cut=0 永不及 settlement；修 `first.stopCheckpointFiler()` ⇒ 无框架检查点 ⇒ 负对照 boot 座全链 ⇒ `fallbackCutLocked` cut 可达 settlement ⇒ 无 pin 真被 trim。**守卫与断言原样保留**。负锚：去掉 `restoreUnconsumedEvents`（pin）⇒ 具名红 `:2474 trimmed before its recovery pin could protect it`，**证 pin 确为保护源**；还原 md5 一致。
- **两条均只改夹具，零产品改动**；四条冻结不变量逐条自证未违（检查点内容未改、`durable.go` 未改、`noWindowResidentFacts` 原值、断言守卫原样、无 `--update-baseline`、懒水合未一律加）。

**🔴 红二 `TestRestoreFoldCounts`＝真设计分叉，G26 正确停下来报，编排者立案待裁（这是当前唯一关键路径）**
- **探针实证**：`first.Close` 后新开 tier 读得 spec=1/fact=2/consume=1（**4 键，引擎正确**）；second boot `tasks=0 tail=5 reads=0 keys=0`。
- **根因**：工具任务**已结**（delivery sent）⇒ `settledAtCut`（`runtime/task/checkpoint.go`）使框架检查点 tasks slice=`{}`；其 durable 键落在框架检查点**之前**，而 `tailFactKeys` 只取检查点**之后**的键 ⇒ Seat 座 0 任务 ⇒ Recovery 空。
- **锚断言原文（编排者亲读 `host_delivery_test.go:2413-2418`）**：①`second.Recovery()` 的 `Tasks` 恰 1 条且 `Recovered=true`（"want the one chain rebuilt"）②日志须含 `restore folded`/`chains=0`/`links=0`/**`covered=1`**（"want the fold beside the chains the checkpoint covered"）。
- **冲突**：要 `covered=1` 就须检查点携带该**已结**任务 ⇒ **直撞冻结不变量「检查点只携带开放项」**；改用全量 eager load ⇒ 撞「`TaskIDs()` 有界」与「懒水合不得一律加 O(历史)」。落点在 `runtime/task`/`task_checkpoint.go`（G26 禁改面）或须放宽不变量。
- **编排者取证补充（关键区分）**：`covered` 属 **compress product 的材料覆盖轴**（`assembly/compress_consumer.go` 的 `metaKeyCompressCovered`/`product.Covered`），**与「任务是否开放」不是同一轴**。故冲突**可能不是真的**——须先查清「已结任务的**交付材料**是否仍须被检查点覆盖」与「检查点只携**开放任务项**」是否可同时成立。**这是待裁的核心问题，编排者尚未查清，不得凭现有证据伪造裁定。**
- **另须裁**：该锚第②条**断言日志文本**（`observer.mentions(...)`），按「不新造只喂日志的出口」与「见证锚意图的可观测后果非实现形态」，**须判它是否应改为断言可观测后果**（如按身份读回该已结链的材料）而非日志字面。
- **history 两条同根未连带收**（`TestRestoreRefusesChainWithoutSpec`/`TestEventIntegrityRestoreUse`）：G26 实证「有框架检查点则空、无则 full-load 读到键」，**因红二未修故未连带**。
- **⇒ 待裁问题具名**：①已结任务的交付材料是否须被检查点覆盖（`covered` 轴 vs 开放项轴是否可分离）②若可分离，落点在 `tailFactKeys`/`settledAtCut`/Seat 的哪一处 ③若不可分离，是锚表达了已退役的形态须重表达，还是不变量须补例外 ④锚的日志断言是否改为可观测后果。**四问须先取证再裁，不得为转绿放宽不变量或削弱锚。**

**现态快照（编排者亲测）**：`go build ./...`=0；`check_concept_budget`/`check_dead_producers`/`check_vocabulary_alignment`=0；**`check_product_surface`=1**（G30 在删，`tool/recall/key_test.go`/`get_test.go` 已在改）；**`check_dod_coverage`=1**；assembly 全量 **1 FAIL**（`TestRestoreFoldCounts`）＋长跑锚属 journal CRC boot 成本面（R182/R183 已声明，假锚待修）；history **2 红**（同根 blocked）。树 **200 文件／23786 插入／6665 删除**，HEAD 仍 `9547faca`，**零 git 写**。
**三单在飞**：G30（死结构删除）／G31（治理可先行三面）／G32（F1 核心 diff 只读）。
**目标未达成（R179 九项缺口＋本条红二分叉）。不得归档不得提交。**

### R185 红二分叉已裁：锚表达了与冻结设计相反的形态（非不变量须放宽）；取证决定性（2026-11-17）

**决定性证据（编排者亲读 `runtime/task/checkpoint.go:323-330`）**：
```go
// settledAtCut reports whether this task finished before the cut: a task holding
// a terminal status with every obligation the work opened cleared is a task the
// run finished, so its record stays on its chain and the checkpoint states
// nothing of it. A terminal task still owing a delivery keeps its record stated,
// because that obligation is work the next process still holds.
func (t *memoryTask) settledAtCut() bool { return isTerminal(t.status) && len(t.obligations) == 0 }
```
⇒ **冻结不变量的真语义是「仍欠义务（next process 仍持有的工作）的任务才进检查点」，不是朴素的「只携开放项」**。**终态但仍欠交付的任务照样被携带**。另 `assembly/task_checkpoint.go:8` 明写检查点携「covered part of **every open chain** and nothing else」。

**⇒ 裁定：`TestRestoreFoldCounts` 的期望与冻结设计相反，属陈旧锚，不是不变量须放宽。**
- 该测试场景是工具任务**已结且 delivery 已 sent** ⇒ 义务已清 ⇒ `settledAtCut()` 真 ⇒ 检查点**按设计就该对它陈述零**，`tailFactKeys` 不取检查点之前的键也**按设计正确**。G26 探针实证引擎读得到 4 键（事实在链上），**故事实未丢，只是不被 eager 覆盖**。
- 故 `Recovery().Tasks` 须恰 1 条 `Recovered=true` 与日志 `covered=1` **这两条断言要求的是已退役的 eager 覆盖形态**。**不得为转绿放宽 `settledAtCut`／把已结态搬回检查点／改全量 eager load**（三者各撞一条冻结不变量）。
- **重表达方向（须保住可观测后果，不得削弱）**：①**重启后该已结任务的链仍可按身份读回**（懒水合，G19/G22 已落地并实证「水合后快照仍 0 条」）——这是用户可见后果，须成为正锚；②**恢复报告对一个义务已清的终态任务陈述零**（即 `settledAtCut` 的语义本身须被钉住，防将来有人把已结态搬回检查点）；③**负锚**：若水合被去掉，按身份读该已结链须具名失败（证①真被量着）；④原第②条**断言日志字面**（`observer.mentions("covered=1")`）按「见证可观测后果非实现形态／不新造只喂日志的出口」**应改为断言可观测后果**，不得继续钉日志文本。
- **⚠️ 实施前置（须先取证，不得直接改锚）**：确认该测试场景里任务的 `obligations` **确实为空**（若 delivery 已 sent 但仍有未清义务，则 `settledAtCut` 为假、检查点本应携带它，那就是**产品缺陷而非陈旧锚**，裁定方向反转）。**G26 报「delivery sent」但未直接探针 `len(obligations)`，故此点必须补证。**
- **history 两条同根**（`TestRestoreRefusesChainWithoutSpec`/`TestEventIntegrityRestoreUse`）须按同一裁定复核：它们期望的「recovery outcomes 含该任务」是否同样属陈旧 eager 形态，还是各自另有独立根因（G25 曾报 `restore_refuses` 第二子测试红于 :172 query 拒读 invocation 维，**那可能是另一条独立缺陷，不得一并归入本裁定**）。

**⇒ 关键路径解锁**：红二不是「无解的设计冲突」，而是「锚陈旧＋须补一个前置取证」。**待派单**：补证 `len(obligations)`→按上述四点重表达锚＋补负锚→复核 history 两条是否同根。**写面** `assembly/host_delivery_test.go`＋`tests/integration/history/` 两文件，可能需 `runtime/task` 只读取证。**G26 已收口，assembly 写面释放。**
**三单仍在飞**：G30／G31／G32。**目标未达成，不得归档不得提交。零 git 写。**

### R186 派 G33 执行 R185 裁定（关键路径）；四单在飞（2026-11-17）

**G26 收口释放 assembly 写面**，与 G30（memory/ledger＋tool/recall）、G31（governance）、G32（只读）不相交，故派 **G33** 执行 R185：
- **第一步是前置取证，结果可能反转裁定**：探针实测该场景任务的 `len(obligations)`。**为空⇒裁定成立按重表达做；非空⇒`settledAtCut` 应为假、检查点本应携带它⇒是产品缺陷而非陈旧锚，停下来报不得改锚。**（G26 只报「delivery sent」未直接探针义务数，故此点必须补证。）
- 重表达四点：①正锚「重启后已结任务的链仍可按身份读回」（懒水合，用户可见后果）②正锚「恢复报告对义务已清的终态任务陈述零」（**钉住 `settledAtCut` 语义本身，防将来把已结态搬回检查点**）③负锚「去掉水合后按身份读须具名失败」④**原断言日志字面 `observer.mentions("covered=1")` 改为断言可观测后果**（依「见证可观测后果非实现形态／不新造只喂日志的出口」）。
- **明禁**：放宽 `settledAtCut`／把已结态搬回检查点／改全量 eager load／放宽 `TaskIDs()` 或任何窗口界／删守卫／抬基线。**本单是锚重表达不是产品修复，禁改一切生产 `.go`**；若取证表明须改产品则停下来报。
- **history 两条须逐个复核不得一并归入**：`restore_refuses` 的第二子测试红于 :172（query 拒读 invocation 维，源 `memory/query/inventory.go:151`）**可能是独立缺陷**，须分别取证。
- 回归面含**已收口三条锚须仍全绿**（`TestSymptomAnchorBootCostFlatAcrossGenerations`/`TestRestartRecoveryPinsBeforeTrim`/`TestAnchoredBootMatchesFullBoot`），且 package-layout test-mirror-missing **不得多于起点 17**、comment_policy 0、不得留调试打印。

**四单在飞**：G30／G31／G32（只读）／G33。
**现态（R184 快照）**：build 0；concept/dead_producers/vocabulary 0；product_surface 1（G30 在收）；dod 1；assembly 1 红（G33 在收）＋长跑离散面；history 2 红（G33 复核）。树 200 文件／23786 插入，HEAD `9547faca`，**零 git 写**。
**目标未达成（R179 九项缺口）。不得归档不得提交。**

### R187 停放交接账（回合预算尽，四单在飞，目标未达成）（2026-11-17）

**⚠️ 目标未达成，不得归档、不得提交、不得推送。全程零 git 写操作，HEAD 仍 `9547faca`。**

**停放终态（编排者亲测）**：`go build ./...`=**0**、`go vet`（assembly/memory/ledger/runtime/task/governance）=**0** ⇒ **树可构建可 vet，非半改断裂态**。工作树约 **200 文件／23786 插入／6665 删除**，`git status --short` 约 270 条未提交。

**四单在飞（预算尽时可能未收，接续者须先核实际落盘再判完成，门禁数字须重测不采信汇报）**：
| 单 | 写面 | 交付目标 | 收口时须核 |
|---|---|---|---|
| G30 | `memory/ledger/original_store.go`/`segment_store.go`(仅 factOriginalsOf)/`reference.go`/`kv_original_store.go`/`tool/recall/skeleton_assert.go`＋5 测试 | 删 `NewMemoryOriginalStore` 死结构，关 `check_product_surface` 红 | 该门须 0；`newMemKV`/`newEmptyKVOriginalStore` 未被连带删；类型本身删/留有证据；5 测试语义未削弱；负锚（加回构造器须使门具名红） |
| G31 | `features/evolution/governance/**` | R175 可先行三面＋两条锚纪律 | **链上是否携带拒绝 answer 的取证结论**（不携带须停不得发明落链语义）；假锚判据已加强为「原审批身份仍可裁决」；夹具已撤 `<dir>/approvals` 并自证量的是切面；**未拆全量 `Records` 掩盖层**；package-layout ≤17 |
| G32 | 只读 | F1 核心 diff 按 hunk 反夹带 | 夹带嫌疑清单／重写历史凭据／兼容分支残留／注释真话／重复端口／`newestSessionCheckpointRef` 判定／转移项是否被顺手改 |
| G33 | `assembly/host_delivery_test.go`＋history 两测试 | R185 裁定：重表达陈旧恢复锚 | **`len(obligations)` 探针值**（非空则裁定反转须停）；正锚①按身份读回②报告对已结陈述零；负锚；日志断言改为可观测后果；history 两条逐个归属；**已收口三锚仍全绿** |

**本轮（resume 后）已闭合**：五条阻断（死锁／A 簇／C 簇／重入／midband）＋损坏检测安全回归＋提交 EINVAL 6 条同根；assembly 10→1、history 9→2、runtime 自测 3→1（余属外来禁改面）、mutations 自测 7→0（54/54）、durable_truth 5→0、comment_policy 5→0、gofmt 3→1（在飞文件）。**两条症状直译锚回归重跑仍绿。**
**编排者本轮亲收**：durable_truth 已删机制登记／wiki memory 行契约漂移／ci.yml race 集补 `api/durable`／合成树夹具补四门 stub／DoD 三处字段腐化（P1 文本含已删机制、P4·P5 ci_gate 指错 job）／夹带 `UPGRADE.md` 一行剔除／裁决面扩大两处勘误／gofmt 两文件／wiki:253 假话／开档完整性校验成本表声明。

**已裁待执行（裁定已落盘，接续者直接派单即可）**：
- **R185**（红二锚陈旧）→ G33 在飞，**前置取证可能反转**。
- **R175 五条**（治理）→ G31 在做可先行三面；**G2-G8 重铸号覆盖（P1 红线）＋G2-G10 目录第二来源＋`g2govtier/` 两枚 tier 锚须另单同批**，因全量 `Records` 是洞的掩盖层，先拆会把缺陷变永久。
- **R182/R183**：开档全 log 校验**保留**（安全＋冻结契约，乙被三事实否掉），成本已声明；**但恒绿假锚 `symptom_anchor_recovery_test.go:465` 的 `journalBytes==0` 未修**（量不到完整性校验成本），须限定其语义＋另立钉线性事实的量与锚。**写面 assembly，须待 G33 收口。**

**剩余硬项（依赖序）**：①收四单战报并逐项重测 ②修上述假锚 ③八面懒水合（`Obligations` 现时可达靠 `read.go:473→477` 隐式顺序兜住）＋水合成本 O(历史键容量)×5 声明 ④治理 G8/G10 同批单 ⑤兜底路径生产侧观测缺口 ⑥`package-layout` 17（HEAD 既有＋2 新增）⑦`proc-refs` 1（**属另一活动变更 `no-prefix-criterion` 的未跟踪工作，非本候选，不夹带改它**）⑧`tracked-hygiene` 13（**暂存态，须把删除入索引才绿⇒全量 lint 排在暂存之后、提交之前**）⑨P4 退役重绑 ⑩九变异实跑（须控制树全绿）⑪godoc 统一重生成（须待全部代码落地）⑫两条稳定红归因（`TestDurableCommitBarrier`／`TestConsumptionFailureIsReportedByName`）⑬长跑亚线性成本 profiling／09.R2 噪声判据（**禁调大界禁删离散守卫**）→ 全量 `bash scripts/lint.sh`＋CI 矩阵（R149/R169 清单）→ **F1 完整反夹带**（G32 在审）→ **R35 第二轮独立放行审** → F2/F3/F4。

**另立案（既有缺口，归独立变更，when/why 已写）**：①feedback 跨包孤儿（HEAD 已死，**转移项须立承接实体否则 F4 归档丢失**）②**`journal.go:544` 长度域翻大被当 torn tail → 写面 Truncate 静默切该帧及其后全部历史**，而 `:506` 同情形判 `ErrJournalCorrupt`，**两函数相反判决，安全级数据丢失，HEAD 同形**；保留开档全扫把其暴露面扩到全历史（已声明）。

**R179 完成审计判定不变：目标未达成。** 句①「整理计划／并行派发」✅、句②「review＋违规概念词汇核对」✅；**句①「优雅完成」❌、句③「纯度落实执行」❌**（五判据②④成立，①③⑤各有主树确证或实证过的违例/未声明项）。**不得以门绿／阻断闭合／症状锚绿当达成证据。**
**冷启动读法**：R179（三句审计）→ R184/R185（关键路径裁定）→ R182/R183（完整性校验裁定与成本声明）→ R175（治理五裁定，已落 07 design §十五之补）→ 本条（在飞单表＋依赖序）。

### R188 G30 部分收口＋反驳编排者前提：死结构删除阻于「未接线能力唯一导出面」真分叉，R178 的「不走 waiver」裁定须重议（2026-11-17）

**G30 已完成（编排者采纳）**：全引用点枚举（含未跟踪）无清单外命中；`tool/recall` **三测试已迁**为 `OpenTier(StorageMemory)` + `tier.Originals.(ledger.ScopedOriginalStore)`（生产同款断言见 `assembly/tools.go:168`），**断言零改动**，且顺带消掉 `corruptSurface` 的「段存储一个引擎、原文另一个空仓」第二引擎。实跑：build 0、vet 0、`memory/ledger` 0（51.1s）、`tool/recall` 0、history **2 FAIL＝起点无新增**、comment_policy memory/tool 0、concept/dead_producers(3447)/vocabulary 0、`test_check_product_surface` 0。**只写 3 个白名单测试文件，零越界。**

**🔴 核心交付未达成：`check_product_surface` 仍 1。阻于两处硬阻断（G30 取证，编排者采纳）**：
- **类型判定应删但承担真实角色**：`MemoryOriginalStore` **无一个自有方法**（纯 `struct{ *kvOriginalStore }`），`newEmptyKVOriginalStore` 返回 `*kvOriginalStore` **不返回它**，生产零实例化。**但它是外部包唯一能拿到 `BlobCount()`/`References()`/窗口注册的导出具体类型**——这两个方法是未导出类型上的导出方法，`OriginalStore`{Put,Get,ReadSegment} 与 `ScopedOriginalStore`{GetScoped,ReadSegmentScoped} **都不声明它们**。
- **(a) `References`/`BlobCount` 无导出接口可达**；且 `BlobCount()=len(byKey)`（有引用的键）**不等于**数 `blob:` 键 ⇒ 经 `tier.Store` 重表达**不等价**。
- **(b) `WithRetentionWindow` 绑定该类型，是唯一窗口注册出口**；isolation 子测 3/4 的 `RecallExpired`/`ErrOriginalExpired` 全靠它。**`OpenTier` 对所有档传 `nil` windows（HEAD 亦然）⇒ 删钩子后 expiry 在生产与测试均不可达。**
- **G30 反驳编排者令内前提（采纳并勘误 R178/R187）**：我令它迁测试用 `newKVOriginalStore(store, nil)`，但**该构造器未导出、外部包不可达**，且 **`nil` windows 会抹掉 expiry 断言**。故「迁 5 测试」不是机械迁移。

**⚠️ 编排者须重议自己的裁定**：R178 裁「删，不走 named waiver 旁路」，理由是「该函数确已无生产消费者，豁免它等于把死结构合法化」。**该理由的前提不完整**——G30 证据表明它不是纯死结构，而是**一个已文档化为「在册未接线」的能力的唯一导出面**（`docs/wiki/memory/memory-architecture.md` 缺口表明写「原文过期没有执行面：租约与生命周期在册未接线，`retention_policy` 取值为 `keep`」）。按既有原则「**零消费者≠该删（未接线目标→接线）**」，**它可能是待接线脚手架而非死结构**，则 named waiver 反而是正当处置。**R178 裁定须按此新证据重议，不得沿用。**

**待裁分叉（G30 给三案，编排者尚未裁）**：
- **(C) G30 推荐**：两子测移入 `package ledger` **内部测试**（`newKVOriginalStore(newMemKV(), windows)` 是单一真相），同批删构造器＋类型＋`MemoryOption`＋`WithRetentionWindow`＋两断言＋单 case，并清 baseline stale 项（需 `scripts/` 白名单）。**代价**：失去外部消费者锚性质，且**删掉待接线能力的唯一注册出口**。
- **(B)** `StorageConfig` 加 windows 字段（`config.go`，新公开字段须 ORCHESTRATION 理由）⇒ 把窗口注册变成配置面，**可能正是「接线」的正解**，但属新公开面须裁。
- **(A) G30 反对，编排者认同反对**：把 `MemoryOption` 改 `func(OriginalStore)` ⇒ 把构造期配置变成活体 store 的运行期 mutator，新增锁与静默 no-op 面。**亦反对 alias/改名喂门。**
- **编排者倾向（未裁，供接续者参考）**：先判「retention/expiry 是待接线目标还是已放弃」——若待接线，(B) 把它接进配置面才是纯度正解（消除测试专用导出面＝消除第二形态），(C) 是删掉待接线能力；若已放弃，(C) 正确。**此判定须先读 `retention_policy`/`NewLifecycle`/`Retention` 的在册状态与主 spec 条款，不得凭 wiki 一句缺口表就裁。**
- **连带**：`tool/recall/skeleton_assert.go:25` 钉的正是**生产从不使用的测试专用类型**（生产走 `tier.Originals` 断言），删类型后该断言无导出具体满足者可钉；`docs/wiki/tool/tool-architecture.md:101` 点名该类型随之漂移（docs 在 G30 禁改面）。`factOriginalsOf` 删 memory case 后退化为单 case 包装，按「单使用方不抽象」应内联为一次类型断言（唯一调用方 `segment_store.go:783` `CommitFact`），**与上述同批**。

**⇒ 接续者第一步**：裁 (B)/(C) 分叉（先取证 retention 在册状态），再同批落删除＋迁移＋断言＋内联＋docs＋baseline。**G30 已收口，`memory/ledger` 与 `tool/recall` 写面释放。**
**其余在飞**：G31（治理）／G32（F1 只读）／G33（锚重表达）。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R189 G32 的 F1 核心 diff 审查：推翻编排者两处说法，查出七处残留与一处未授权契约弱化（2026-11-17）

**🔴 推翻编排者说法一：「comment_policy 0」当前不成立。** G32 实跑 `go run ./scripts/comment_policy . examples/cli-host examples/wechat-bot` ⇒ **exit 1，free-standing 0→10**，10 条全为 `assembly/host_delivery_test.go:2395–2422` 的 `// DBGPROBE` ＋ DBGPROBE `t.Logf`，**HEAD 零命中 ⇒ 本轮新增，属 G26 在飞调试残留**；同文件 import `api/durable` 插在 `api/config` 前＝字典序破。**编排者此前多次测的是产品面 `-no-baseline assembly api runtime …`，scope 窄于该门实际覆盖面 ⇒ 又一次「自扫窄于门不得声称已清」，且这次是编排者自己犯。** 勘误 R169/R179/R187 的「comment_policy 0」声称。

**🔴 推翻编排者说法二：G25 的 `takeWriteFaceLocked` 修法须重议。** G32 取证：读视图提交**静默升级写面且不取 `rootWriteLock`**（锁只在 `OpenAtomicStore` 的 WriteRights 分支），而 `config.go` OpenTier doc 明写「One directory carries one writer…two writers never commit over one slot」⇒ **注释与机理不符**。生产枚举：`run.go` 强制 true／migrate true／fold-cold-root readTier 只读／recover-kv `WriteRights=restore` 且干跑不写 ⇒ **零生产消费者，唯一消费者是它自己的新锚**。G32 建议：**删；读视图提交改具名拒绝**。**编排者 R177 采纳该修法时未核这两点，须重议**（与 R182 保留全 log 校验的裁定独立，那是完整性校验时机，这是写面升级）。

**🟠 C 簇裁定的落点须复核**：G32 取证 `segment_store.eachIndexEntryNewestFirst` 的 **`CommitOrder` 分支生产不可达**——journalKV 两端口皆实现且 `indexKeyOrder` 先中、memKV 皆不实现、只实现 CommitOrder 者仅 `examples/contract`。建议**删分支、端口留**（`tailFactKeys` 真消费）。⇒ R147 裁定「改兜底排序」修的是**兜底**（正确），但**能力分支本身是冗余**这一点编排者未查，须同批处置。

**🔴 未授权契约弱化（须裁，最高优先）**：三主 spec（`config-liveness`／`delivery-closure`／`session-checkpoint-material`）把无条件「按身份恒可读 SHALL NOT 答 missing」改为「**本构建写出的根上**」＋旧冷档须先 `fold-cold-root`。G32 只找到 design/CHANGELOG 叙述，**未见用户原话** ⇒ 标「提及未确认」。**按「spec 对实现错则改实现，不弱化 SHALL」与「绝不伪造用户裁定」，此改动须溯源用户授权；若无授权则属削弱契约以迁就实现，须回滚或补裁。**

**其余残留（逐个点名，G32 建议）**：`openJournalKV`/`openJournalLog` HEAD 有生产调用方本轮降级为仅测试⇒回卷；`taskSliceFactLog.Get` 文件内零调用⇒删；**根目录两个 Mach-O 二进制 `fold-cold-root`/`recover-kv`（14.5MB）`git check-ignore` exit 1＝未忽略，`add -A` 会扫入⇒提交前必清**；`TestRecoverKVReadsARelocatedRoot` 无后继（**R174 已裁：它量已退役的 RetireFacts 面，脚本入口锚 `TestRecoverKVCommandExitCodes` 存活实跑 0，故此项已由 R174 闭合，G32 未读该条**）。

**专项结论**：**重复端口收干净**（含 `Sync(ctx) error` 的生产端口声明唯一＝`api/durable.FactLog`，三旧声明连 doc 与 example 全删，棘轮 58→57、接口 87→85 为下调且逐名记因，残留同构 0）。**`newestSessionCheckpointRef` HEAD 就已死**，归档 design 曾裁它接 boot 同遍 walk 但**实现从未接线**⇒**不是有意测试 oracle**，建议归 P6/10 删（连测试调用），门因「测试算消费者」抓不到。**转移项零改动✅**，但主 spec 仍反向（载 `POST /feedback` 而 cli 路由已无），承接实体仍无；另核 `WithDiagnostics` HEAD 零调用方而 `c.diagnostics` 仍由 cli.go 写读⇒**未新造孤儿，R148 站得住**。**重写历史凭据：无**（run-notes HEAD 62 行逐字保留＋追加勘误 8 行，archive 其余 1802 文件零 diff）。**夹带：审过范围内 0 条**；一项须补归属说明（`config-surface-normalization` 目录移回进行中＋orchestration spec −123 行，经核与 `ec038779^` 逐字节相同＝精确撤销归档合并，属另一变更边界，提交面须显式声明两笔）。
**可勘误**：ORCHESTRATION:2074「P1 锚消失」已有后继 `TestFactCommitAnswersNewExistingConflictUncertain`（已绑 dod_predicates.json）。

**⚠️ 审查覆盖率仅约 20%**：总 **782 hunk**（Go 472／非 Go 310）＋约 40 未跟踪新文件；逐 hunk 读≈95（12%）＋符号级分类≈60（8%）。**未审**：memory/ledger 全部测试（journal_test +2161 等≈100 hunk）、assembly 其余测试与 4 新测试文件、**runtime/task 全部**、state mailbox* 与 durable.go 后半、governance 逐 hunk、**scripts/gates/*.py（+1295 等≈90 hunk）**、recover-kv/fold-cold-root/scenarios、golden 另 4 json、docs/api 与 OPERATION/event-flow/memory-architecture、tests/integration≈20 文件、openspec 规划文档、tool/recall 三测试。**⇒ F1 不得声称完成。**

**⇒ 对「是否打转」的证据补充**：本轮两处编排者采纳的修法各留一处新残留（写面升级无锁且零消费者／能力分支冗余），一处编排者反复声称的门绿实为 scope 窄化，一处契约弱化未溯源授权。**这四项都是「修复级联面未预先算进派发单」的实例，与 R188 的分叉同源。**
**在飞**：G31（治理）／G33（锚重表达）。**目标未达成，不得归档不得提交。零 git 写。**

### R190 G31 收口：治理第 1 面＋两条锚纪律落地；纠正编排者两处过期前提；G8/G10 仍缺（2026-11-17）

**🔴 纠正编排者前提一（R175/R181/R189 均须勘误）**：我反复声称「治理四缺陷主树未修、grep 零命中」。**G31 取证：07.R2 候选（N16/N17/N18/R61）已在工作树，第 2 面（写腿失败具名化 G9）与第 3 面（审批身份与别名跨切面存活 G4/G5）已实现且绿**。我 grep 的是 `g2govtier`/`G2-G6`/`G2-G8` 这些**锚名与包名**，它们确实零命中（锚未落主树），**但产品修复已在**——**「锚未落盘」≠「缺陷未修」**，我把两者混为一谈。⇒ R176 判据①③的治理面结论须按此收窄：**G3/G4/G5/G9 的产品面已修，缺的是主树锚与 G6/G8/G10**。
**🔴 纠正编排者前提二**：我令内明禁「拆全量 `Records` 掩盖层」，**G31 上报 R61 候选已拆它**（`stateSliceWire` 无 Records，design §19 裁定 3）⇒ 该前提亦过期。G31 面 1 只加具名，不拆不补，未越界。
**✅ 裁定四前置满足**：链上**确实携带**可判定 answer——`Decide`→`fileAnswer`→`RecordAttention`→屏障后写 `tagent/attention/v1/fact:`；R61 请求记录族 `tagent/approval/v1/request:<asked>` 承载身份；`BindAttention` 折叠二者。**无须补落链**，R175 裁定四可直接执行。

**本轮实际增量**：
- **面 1（G6 历史读静默变短）**：`foldHistory` 增窗口具名——座定窗口内缺号、尾读未达的已存键，逐键经**既有** `governanceHistoryUnreadable` ERROR 通道具名（**不新造出口**）。新锚 `TestGovernanceHistoryQueryNamesItsBoundary`（三子测试：窗外键具名／窗内洞具名／整流无洞零具名）。变异①删窗外具名→红（点名键 3）；变异②静默缺号具名→红（Count=3 beside DenialSeq=4）；还原 diff 全 0。
- **锚纪律一（R175 裁定一）已落**：G11 判据加强为「`Decide("open-hold")` 仍可裁决＋闸门理由点名 open-hold」。**变异③红原文正是假锚病灶**：park 于新铸 `approval-d2825d1e…`，**旧判据对此恒 PASS**。⇒ R175 裁定的「恒成立负锚＝假锚」得到实证。
- **锚纪律二（裁定二）已落**：夹具自证链＝文件先存在→撤走→`stat IsNotExist`→篡改恢复后 `Pending()==0`（Bind 前）→身份经链解析；控制组独立 face 防污染。
- **诚实上报一处变异不红**：变异⑤撤 `StateSeat.Bind` 接线 ⇒ **G4 不红**，因其夹具直调 `BindAttention` **绕过生产接线**；加强版 G11 走 `restarted.Bind` 才量到该接线（变异③红）。G4 直接变异载体在 `attention_log.go`（白名单外），**不越界，如实报而非掩盖**。⇒ **G4 的消费者锚仍有绕过接线的缺口，须另补**。

**验证凭据**：build 0／vet 0／七锚 ×1 与 ×10 全 0（**70 PASS/0 FAIL，无 no-tests**）／四变异各具名红＋还原 diff 全 0／gov 全包 0 且 `-race` ×3＝0 竞态／evolution 全树 0／**package-layout test-mirror-missing=17＝起点零新增**／comment_policy 0／concept 0／dead 0／vocab 0（其引入的 2 处退役词 carrier 已自改）／product 1＝G30 面非本单／assembly 两症状锚 0（未破 G26 面）。**零越界写、零 git 写、零新文件。**

**G8/G10 仍缺（须另单同批，前置＝存储面收口）**：
- **G8**：**具名≠修复**——既有洞下重启写者仍自重铸号覆盖（**P1 红线**），须**洞语义设计冻结＋真实 localfile tier 锚**同批。
- **G10**：座后路径 N18 已绿（本轮 ×10 复核），余＝**tier 锚落盘＋无切面 boot 的目录角色处置**，须与 G8 同批。

**⇒ 对「是否打转」的再校正**：编排者此前把「锚未落主树」误读为「缺陷未修」，导致 R176/R179 把治理面严重度高估。**实际治理面比我声称的更接近闭合**（四面已修，余 G6 本轮收、G8/G10 待同批）。**这是本轮第五次编排者从不足证据起判**（前四：可选能力当缺陷／:514 当必经点／不变量简读／死结构无豁免余地），**同型病根＝以二手摘要或单一 grep 代替权威全文与多面枚举**。
**在飞**：G33（锚重表达）。**目标未达成，不得归档不得提交。零 git 写。**

### R191 反驳 G32 的「未授权契约弱化」：三主 spec 改动正是用户零残留裁定的实现；comment_policy 已归零（2026-11-17）

**🔴 G32 的「未授权契约弱化」标记被反驳（编排者取证，采纳并勘误 R189）**：
`08-fact-retirement/tasks.md` 头部 banner 明写——
> ## ⚠️ 本域性质已反转（2026-11-16，**用户裁定**；阶段5自审 A2 回写）
> **用户裁定原话**：「采纳可寻址索引，退位退役」，随后纠正「**准确的说不是"退役"，我们的所有变更都要0遗留0残留，不考虑框架兼容，最多考虑外部脚本辅助**」。
> **零残留形状**：框架内**不留任何兼容分支**——无 legacy 冷读路径、无冷档检测谓词、无 legacy 具名错误、无退位配置开关。既有冷档数据由**外部脚本** `scripts/fold-cold-root` 折入**新根**（真实事实的发布前运维前置），框架零分支。
> **下方原「红线贯穿每一行：搬迁非删除」仍然成立且更强**：删除的是**机制**，不是**已提交事实**；任何步骤不得使已提交事实不可读。

⇒ **三主 spec 的改动正是该裁定的忠实实现**：删掉「透明回冷层」要求＝删掉 legacy 冷读路径（用户明禁）；冷档须先经外部脚本折回＝用户明定的「最多考虑外部脚本辅助」＋「发布前运维前置」。**G32 只查 design/CHANGELOG 叙述未查域 banner，故误标「未见用户原话」——与编排者本轮五次同型病根一致（搜索面不足即下判）。**
**且该改动含两条强化，不是单纯弱化**（编排者逐行读 diff 确证）：①新增「热层字节的增长 SHALL 被显式声明，**MUST NOT 被声称为本要求已达成的有界**」②新增「索引点名而正文读不出或校验失败 SHALL **具名拒绝**，MUST NOT 降级为「不存在」」。**恒可读红线对「本构建写出的根」完整保留**，且 `恢复期读全史的消费者 SHALL 仍能按身份读到其正文` 逐字未删。
**⇒ 裁定：三主 spec 改动授权充分、方向正确，不得回滚。** R189 的「须回滚或补裁」撤销。

**✅ comment_policy 全 scope 已归零**：编排者实跑 `go run ./scripts/comment_policy . examples/cli-host examples/wechat-bot` ⇒ **0 finding(s); 0 beyond baseline; 0 ratchet slot(s) can be lowered**。⇒ G32 报的 10 条 `// DBGPROBE` 残留**已被在飞单清掉**，R189 对编排者的那条勘误**本身已过期**（当时成立，现已不成立）。**教训：勘误也有时效，须重测而非沿用。**

**下一步关键项（R189 提出，编排者正在自核）**：G25 的 `takeWriteFaceLocked` 是否真「不取 rootWriteLock 且零生产消费者」——**须自己读代码与锁路径核实，不采信 G32 单方取证**（本轮已多次证明单来源不足）。
**在飞**：G33（锚重表达）。**目标未达成，不得归档不得提交。零 git 写。**

### R192 编排者自核确证 G32 的写面升级缺陷并裁定删除；派 G34（安全＋零残留，非门卫生）（2026-11-17）

**编排者亲自取证（不采信 G32 单方报告，因本轮已多次证明单来源不足）**：
- `memory/ledger/journal_kv.go:224-238` `takeWriteFaceLocked()`：`openJournalLogFace(k.log.dir, true)` → 关旧读面 → 换新写面。**全程不取 `rootWriteLock`**。
- `rootWriteLock` 只在 `config.go:230` 的 `if cfg.WriteRights` 分支取；`config.go:270` doc 明写「One directory carries one writer…a second such open, in this process or another, is refused by name with `ErrRootWriteLocked`, so two writers never commit over one slot」。⇒ **读视图升级绕过该锁，不变量被破**：持锁写者＋升级写者可同时写同一 root。
- **唯一调用方**＝`journal_kv.go:191`（grep 全仓含未跟踪只此一处＋定义与 doc）。**生产侧 `assembly/run.go:600` 强制 `storage.WriteRights = true`**；`recover-kv` 用 `WriteRights: restore`；`fold-cold-root` readTier 只读不写 ⇒ **零生产路径经读视图提交**，唯一消费者是 G25 自己的新锚。
- **它是 G25 为修 6 条 EINVAL 红而加**，而那 6 红真因是**测试与装饰引擎经默认开（无 WriteRights）却提交＝写者误声明为读者**。G25 选择让读视图静默升级，**等于把夹具的错误声明合法化，并造出违反不变量的死机制**。

**⇒ 裁定（与 G15 修 A 簇同形，先例已落地）**：①删 `takeWriteFaceLocked` 及其调用 ②**读视图提交改具名拒绝**（复用该包既有具名拒绝风格，不得静默 no-op／返回 nil／降级只读成功）③**真写者的测试补 `WriteRights: true`**（改开档声明≠改断言）④G25 的新锚 `TestJournalCommitThroughAReadFaceTakesTheWriteFace` **改为钉「读视图提交被具名拒绝」**＋负锚（把拒绝改回静默升级须具名红）——**这是把锚从量错误机制改为量正确契约，不是删锚消红** ⑤级联核 `config.go:270` doc 与机理一致。
**明禁**：放宽「一目录一写者」、给读视图加任何写路径、削弱断言。

**派 G34**，白名单 `journal_kv.go`＋`journal_kv_test.go`＋`config.go`（仅 doc）＋三个 history 测试（`durable_task_round_trip_test.go`/`projection_replay_and_budget_test.go`/`restore_observation_test.go`）。
**⚠️ 并发排除**：G33 在飞写 `assembly/host_delivery_test.go`、`history/restore_refuses_chain_without_spec_test.go`、**`history/event_integrity_test.go`** ⇒ G34 **绝对不可写这三文件**；6 条 EINVAL 里 `event_integrity_test.go:119/:2795/:4313` 属 G33 面，**留待 G33 收口后另派**，令内要求明确列出未修三处及预期仍红，**不得为数字好看而碰它或削断言**。另禁改 `journal.go`（R182 已裁保留其完整性校验，假锚属另一单）。
**验证面含锁不变量实证**（持写权开档后第二个写权开档须被 `ErrRootWriteLocked` 拒，且读视图无法绕过它提交）＋**comment_policy 全 scope**（不是产品面窄 scope，R189/R191 教训）＋零残留 grep。

**⇒ 这是关键路径级修复而非门卫生**：它同时消除一个**安全洞**（锁被绕过）与一个**死结构**（零生产消费者），并把一个量错误机制的锚改为量正确契约。**符合用户「不考虑成本只考虑优雅纯粹」的裁定方向。**
**提交前必清项（R189 立案，仍未处置）**：仓根 `fold-cold-root`/`recover-kv` 两个 Mach-O（14.5MB）`git check-ignore` exit 1＝未忽略，`add -A` 会扫入。**须先判 .gitignore 是否 whitelist 模式再决定加条目还是清文件，不得盲改。**
**在飞**：G33（锚重表达）／G34（写面拒绝）。**目标未达成，不得归档不得提交。零 git 写。**

### R193 R188 的 B/C 分叉已裁：取「接线」非「删」——store.windows 是未接线能力而非死结构（2026-11-17）

**编排者亲自取证（四问齐全，不采信单方战报）**：
1. **`store.windows` 被生产逻辑消费**：`kv_original_store.go:509` `window, registered := s.windows[record.ref.RetentionPolicy]` → `reference.go:286` `result.Outcome = RecallExpired`、`:309` `return SegmentResult{}, ErrOriginalExpired`。**故过期机制在生产代码里活着，不是死码。**
2. **但无生产填充路径**：`OpenTier` 对**所有档**传 `nil` windows（HEAD 亦然）；唯一填充点是 `kv_original_store.go:81` `store.windows[policy] = window`，其上游只有 `WithRetentionWindow`（`original_store.go:44`，`MemoryOption`，测试专用）。
3. **`LifecycleConfig` 是另一根轴，不是同一量的第二份**：它携 `GlobalTTLDays`/`TypeTTL map[string]int`/`CheckInterval`/`MaxEventsPerPartition`（带 json/yaml 标签），管的是**事件 TTL 墓碑化**；`store.windows` 是 `policy string → time.Duration`，管的是**原文召回过期**（按 `record.ref.RetentionPolicy`）。**grep 确证无任何 `TypeTTL`/`GlobalTTLDays` → `store.windows` 的转换代码**（那些字段只出现在 `lifecycle_config.go` 自身的定义/默认/归一化）。⇒ **二者不同轴，不构成第二真相**，编排者原先的怀疑被否掉。
4. **`NewLifecycle` 在册**：`skeleton_assert.go:149` 编译期钉住其签名 ⇒ 是**已登记契约面**；wiki 缺口表亦明写「原文过期没有执行面：租约与生命周期**在册未接线**，`retention_policy` 取值为 `keep`…接线 `NewLifecycle` + `Retention`；**接线前不得声称『按策略过期』**」。

**⇒ 裁定：取 (B) 方向「接线」，不取 (C)「删」。** 依据「**零消费者≠该删（未接线目标→接线）**」：`RecallExpired`/`ErrOriginalExpired` 是活着的生产机制，缺的只是**窗口注册的生产入口**。删 `WithRetentionWindow` 而不补生产入口＝**把一个在册未接线能力变成永久不可达**，正是该原则禁止的。
**最小接线面（本变更须做，因红门是本变更删 memory 分支造成的）**：给 `StorageConfig` 加原文召回窗口字段（policy→duration，形态与 `LifecycleConfig` 的 yaml/json 标签风格一致），由 `OpenTier` 传进 `newKVOriginalStore(store, windows)`，**同批删** `NewMemoryOriginalStore`／`MemoryOriginalStore`／`MemoryOption`／`WithRetentionWindow` 与 `factOriginalsOf` 的 memory case（内联为一次类型断言，唯一调用方 `segment_store.go:783`）＋两处编译期断言（`reference.go:241`、`tool/recall/skeleton_assert.go:25`）。
**⇒ 这同时消除 test-only 导出面（关 `check_product_surface` 红）与测试专用构造形态，是纯度正解而非豁免旁路。** R178「不走 waiver」的结论**在此新证据下仍成立但理由更新**：不是「它是死结构」，而是「**接线后测试专用形态自然消失**」。
**新公开字段须记理由**（门要求）：理由＝消除 test-only 导出面＋使在册未接线的原文召回过期机制获得生产入口，逐名记入台账与 06 design。
**两子测迁移**：s07 / isolation 改用**生产同款配置入口**（`OpenTier` 带窗口字段），**断言零削弱**；`RecallExpired`/`ErrOriginalExpired` 由此在生产形态下可达，比原测试专用钩子更强。
**⚠️ 范围纪律**：本单**只做窗口注册接线**，**不做 `NewLifecycle`+`Retention` 的完整 TTL 执行面接线**（那是 wiki 缺口表列的另一件事，属独立变更，不得夹带）。**接线前不得声称「按策略过期」已达成**——wiki 那句纪律仍适用，须如实更新为「窗口注册已接线，TTL 执行面仍未接线」。
**并发**：G34 正在写 `config.go`（仅 doc）⇒ **本单须待 G34 收口后派**，否则同文件互踩。裁定已就绪，接续者直接派即可。

**另亲收（本轮）**：`.gitignore` 加 `/fold-cold-root`、`/recover-kv` 两条显式条目（标准 Go 模板非 whitelist 模式，无扩展名 Mach-O 不被任何既有规则捕获）。**复测**：`git check-ignore -v` 两者均命中 `.gitignore:9/:10`，`git status` 对两者零输出 ⇒ **14.5MB 二进制不再会被 `add -A` 扫入**。`tracked-hygiene` 仍 13（未暂存删除，属提交前项，未变）。
**在飞**：G33（锚重表达）／G34（写面具名拒绝）。**目标未达成，不得归档不得提交。零 git 写。**

### R194 派 G35（P4 重绑）＋G36（中心论点诊断）；四单在飞，攻的是论点不是门卫生（2026-11-17）

**派 G35（dod P4 两条 gap）**：编排者先核两候选锚文本**均存活**——A `journal.go:612-613` 水位跳过块（对应失败原文 `symptom_anchor_recovery_test.go:805`「the open reads the history behind the watermark instead of the window past it」）；B `run.go:2586/2589` `bootIndexWalk`→`walkIndexPastCut`（对应 `:834`「the walk follows the history the chain holds instead of stopping at the cut its newest checkpoint states」）。写面 `scripts/gates/check_event_integrity_mutations.py`＋`golden/dod_predicates.json` 已空出（G20/G27 收口）。
令内要点：**新 id 不得复用已退役的 `MUT-DOD-P4-cold-unbounded`**；`expect_assertion` 取亲跑真实原文子串；站点用形状锚不重复写字面量；**须注意 G25 新加全 log CRC 而 `JournalBytesOpen` 只报索引窗口，故候选 A 是否仍可被检测须实证不得推导**；删 golden 里已失效的 `retire_pending`；退役注释改写为重绑记录但**保留历史事实**；**若控制树红致无法判 killed，如实报「注册已落、真跑阻于控制树红」，禁手填 killed、禁 `--replay` 冒充**（注册本身即闭「未注册」那条 gap，是确定能交付的部分）。

**派 G36（只读，攻中心论点）**：台账 :11 明写「**每代总成本有界尚未成立**，不得以 boot 三绿宣称完成」——这是变更的核心承诺，比门卫生更要紧。五问：①**亚线性增长是真还是噪声**（须同根多次重复取分位 vs 跨深度对比，给样本量，不得单次下结论）②若真，**住在哪一行**（四量分解；**特别注意 `JournalBytesOpen` 不报全 log CRC 量 ⇒ 若增长住在那里则现有四量量不到它**，与已立案的恒绿假锚同源）③**排除/确认 G25 全 log CRC 的贡献**（副本树里去该回放对比曲线；编排者注意到 G25 之前 run2 已 ~1.511 与之后 1.39/1.59 同量级 ⇒ 疑先于该改动，须实证）④**离散守卫判据是否可辩护**（现为两独立根各单次采样之比；若不足给可辩护替代草案，**禁建议调大界或删守卫**）⑤**中心论点当前可辩护表述**（一句结论＋支撑数字＋还不能声称什么；区分 boot 索引走查有界〈已证〉／boot 字节读有界〈**现被全 log CRC 破**〉／每代总成本有界〈未证〉／墙钟 1.3× 界内〈2/3 run 不成立〉）。

**⇒ 本轮派发取向已按自我批评校正**：不再做门卫生小修（那非关键路径、有机会成本），四单分别攻**锚陈旧（G33）／安全洞＋死结构（G34）／判据反例缺失（G35）／中心论点未证（G36）**。
**在飞四单**：G33／G34／G35／G36（只读）。触碰集：G33 assembly+history 两文件、G34 journal_kv+config(doc)+history 三文件、G35 scripts/gates+golden、G36 零写入 ⇒ **不相交**。
**待 G34 收口即派**：R193 裁定的原文召回窗口接线（关 `check_product_surface` 红＋消除 test-only 形态）。
**目标未达成，不得归档不得提交。零 git 写。**

### R195 派 G37 收八面懒水合（runtime/task 写面已空出）；五单在飞且触碰集不相交（2026-11-17）

**G34 进度亲核**：`takeWriteFaceLocked` 在 `journal_kv.go` 命中数已 **0** ⇒ 机制已删（其具名拒绝与测试声明修正待其战报）。G33 在改 `history/event_integrity_test.go`。dod 仍 10 gap、product_surface 仍 1（待 R193 接线单）。

**派 G37**：`runtime/task` 写面随 G26/G31 收口而空出，收 G22 审计判定的**八个按名索取面须水合而未水合**——`manager.go` 的 **`Obligations`（现时可达缺陷）**/`Handoff`/`Flush`、`handoff.go` 的 `HandoffOf`/`IssueNotice`、`cancel.go` `RecordStop`、`progress.go` `ProgressReported`、`logical.go` `Logical`。八面实测重启后对已结任务均答 `ErrTaskNotFound`。
令内关键约束：
- **须枚举全部接收者形态**（`r.tasks[` 之外还有 `m.registry.tasks[…]` 等，G22 指出编排者原 grep 漏了这些）。
- **明确不须水合的面逐个列出不得一律加**：恢复/水合本体（`RestoreCheckpoint`、`hydrateLocked` 自身、`restore.go` 五处）、**遍历常驻集且终态本就跳过的面**（`Watch`/`Unfinished`/`Owed`/`Retained`/`RecoveryMaterial`/`activeCountLocked`/`pruneTerminalLocked`——**加水合＝把 O(历史) 摊进 boot/close**）、仅本进程新 admit 的 wrapper 面、`RollbackReactivation`（只在 Reactivate 成功后触发故必常驻）。
- **`Obligations` 的隐式顺序依赖**：`assembly/read.go:473→477` 先 Lookup 物化再读。**该文件不在白名单**（G33 在飞 assembly 面）⇒ G37 只修 `Obligations` 自身使其不依赖调用方顺序，并实测「不先 Lookup 直接调」也答对；`read.go` 那处是否可消由编排者另派。
- **锁序已验证无环**（G22 `-race` EXIT 0，`r.mu`→`d.mu`→`k.mu` 边早由 `applyCommitLocked` 存在，水合未新增边），但 G37 须自跑 `-race -count=2` 复证。
- **成本实测须报数字**（首次按名索取付 O(历史键容量)×5：`readTaskLocked` 5 次前缀 scan、`eachRecordOnce` 整表顺序读 `a.capacity` 槽；之后 `d.facts` 命中不再读）⇒ **编排者据此补成本表声明**（判据⑤该条仍未声明）。
- 每面正锚＋负锚（陌生身份仍具名拒绝，**不得把水合变成任何身份都答成功**）；package-layout test-mirror-missing 不得多于起点 17；comment_policy 全 scope 0。

**五单在飞，触碰集不相交**：G33（assembly 一测试＋history 两测试）／G34（journal_kv＋config doc＋history 三测试）／G35（scripts/gates＋golden）／G36（零写入）／G37（runtime/task 五生产文件＋其测试）。
**待派（前置未满足）**：R193 窗口接线（须待 G34 收口释放 `config.go`）；恒绿假锚 `journalBytes==0` 修复（须待 G33 收口释放 assembly 包，且 G36 诊断结论可能改变其修法）。
**目标未达成，不得归档不得提交。零 git 写。**

### R196 纠正 G32 的删除建议：`newestSessionCheckpointRef` 是测试专用消费者锚，须先改锚走生产路径再删（2026-11-17）

**编排者亲自取证（不采信 G32 单方建议）**：
- `newestSessionCheckpointRef`（`assembly/checkpoint.go:913`）全仓**唯一调用方**＝`assembly/checkpoint_test.go:1412`。
- 该调用点是 **`TestSessionCheckpointNamesTheChainWithoutEnteringIt`（:1384）的消费者锚**：它断言「the index answered the chain's checkpoint」与「the index answers the checkpoint %s, want the newest cut %s the chain filed」⇒ **量的是「session cut 点名该链 checkpoint 而不进入链」这一真实性质**。
- **生产路径 `restoreSessionCheckpoints`（:948）在 assembly 测试里零直接覆盖**（grep 零命中）；它以一次 `EachIndexNewestFirst` 走查收集**所有** session 的最新 checkpoint，是 boot 实际用的批量版，单 session 变体已被取代。

**⇒ G32 建议「归 P6/10 删（连测试调用）」须修正**：直接删函数＋测试调用会**丢掉一个真实性质的锚**，而生产路径本身无覆盖 ⇒ 净损失。
**且它正是「测试手工补接线≠生产消费者」反模式的实例**：按 R29 消费者锚定义（正式公共入口/真实路径调用证据），**一个生产从不调用的内部函数被测试调用，不构成合格消费者锚**。
**正确处置（两步，须同批）**：①**把该测试改为经生产路径消费**（`restoreSessionCheckpoints` 经 boot，或 `Run.CheckpointSession` 等正式入口），**保住原断言的性质**（cut 点名该链 checkpoint 而不进入链）＋补陌生 session 负锚；②**然后删** `newestSessionCheckpointRef`。**不得先删函数再补锚**（那会留一段无锚窗口），**也不得只删函数保留测试调用**（编译断裂）。
**归属**：P6/10 域（死生产者删＋未接线脚手架接线或删）。**写面 `assembly/checkpoint.go`＋`checkpoint_test.go` 现空出**（G26 收口，G33 只写 `host_delivery_test.go`），**可派单**；但编排者本轮五单在飞，且此单须先设计生产入口的等价断言，**列为下一波待派**，不仓促自收。

**⇒ 方法教训（本轮第六次同型）**：G32 判「零生产调用方⇒删」时**未查该调用点是不是某锚的消费者**。**「零生产消费者」不等于「可删」——须再问「测试调用它是在量什么性质，那个性质有无别的锚在量」**。这与 R174「锚被删≠面丢失须三步判」是同一原则的另一面：**删结构前须先证其承载的验证面有承接**。
**五单在飞**：G33／G34／G35／G36（只读）／G37。**目标未达成，不得归档不得提交。零 git 写。**

### R197 G33 收掉最后一条 assembly 红（定向实证）；树现处 G34 半改窗口，全量读数须待其收口（2026-11-17）

**✅ 关键路径闭合的有效证据**：编排者定向实跑 `go test ./assembly/ -run '^(TestRestoreFoldCounts|TestSessionCheckpointNamesTheChainWithoutEnteringIt)$' -count=1 -timeout 200s` ⇒ **EXIT=0，`ok … 0.640s`**。**`TestRestoreFoldCounts`（D1，`recovery report = []`）已转绿** ⇒ G33 按 R185 裁定重表达成功，**assembly 三条红全部收口**（红一/红三由 G26 修夹具，红二由 G33 重表达锚）。
**⚠️ 但随后全量跑读到的是在飞瞬态，不得当回归**：`go test ./assembly/ -count=1` 与 `./tests/integration/history/` 均 **[build failed]**，原文唯一：
```
memory/ledger/journal_kv.go:198:15: k.takeWriteFaceLocked undefined (type *journalKV has no field or method takeWriteFaceLocked)
```
⇒ **G34 已删方法但尚未改调用点**，属其半改窗口。**编排者不抢改**（白名单互斥），待 G34 战报后重测全量。
**方法纪律（本轮再次确认）**：**在飞单半改期的全量套件读数不是权威现态**；定向跑某锚通过是该锚在该时刻的有效证据，但**不得据此声称全套件绿**，也不得把半改期 build failed 记为回归。**收口后须重测全量再下结论。**
**另注**：后台 bash 通知的「exit code 0」再次是**包装 shell 的码**（末命令 grep），日志真态是 build failed ⇒ **凡后台跑测试必读日志终局行**，此病类本会话已第 N 次出现。

**五单在飞**：G33（疑已收口，待战报）／G34（半改中）／G35（P4 重绑）／G36（中心论点只读诊断）／G37（八面水合）。
**待派（前置未满足）**：R193 窗口接线（待 G34 释放 config.go）；恒绿假锚修复（待 G33 收口＋G36 结论）；R196 的 `newestSessionCheckpointRef` 两步处置（待 assembly 包释放）。
**目标未达成，不得归档不得提交。零 git 写。**

### R198 派 G38 续审 F1 未覆盖高风险面（只读）；四写两读共六单，触碰集不相交（2026-11-17）

**F1 覆盖率现状**：G32 审 782 hunk 中约 20%（逐 hunk 95＋符号级 60）。**未审面里写面已空出者派 G38 续审**，按风险序：①`memory/ledger/` 全部测试（`journal_test.go` +2161 等≈100 hunk，**最大未审面**）②`scripts/gates/*.py`（+1295 等≈90 hunk，除 G35 在写的两个）③`runtime/state/` mailbox* 与 `durable.go` 后半、governance 逐 hunk ④`recover-kv`/`fold-cold-root`/`scenarios` ⑤`golden` 另 4 json ⑥`docs/api/*`＋OPERATION＋event-flow＋memory-architecture（**文档真话**）⑦assembly 其余测试与 4 新测试文件 ⑧`tool/recall` 三测试。
**令内已写入两处对 G32 结论的推翻**（防其沿用错误前提）：①三主 spec 改动**有用户裁定原话授权**（08 域 banner），含两条强化，**不得回滚** ②`newestSessionCheckpointRef` **须先改锚走生产路径再删**，不得连测试调用直接删。
**专项三件**：①**新门拒绝力逐个核**（`check_dead_producers`／`check_dod_coverage` 门锚分支／mutations 门判决模式／`hygiene.go` 的 `checkWhitelistUnnamedSource`）——自测是否含负例且负例真红，有无「只打印却退出 0」或规则不可达 ②**锚是否量对轴**（可观测后果 vs 实现形态/日志字面；有无夹具需求驱动生产设计）③`runtime/task` 八面水合只读核判定依据（G37 在写，不评审未完成实现）。
**因树处 G34 半改窗口（build failed），令内明禁跑 `go build`/`go test` 并据其下结论**，只读审为主。
**要求报 hunk 分母与占比**，与 G32 的 20% 合并算总覆盖率，**不得让读者当已全审**。

**六单在飞**：写＝G33（assembly 一测试＋history 两测试，疑已收口）／G34（journal_kv＋config＋history 三测试，半改中）／G35（scripts/gates 两文件＋golden）／G37（runtime/task 五生产＋其测试）；读＝G36（中心论点诊断）／G38（F1 续审）。**触碰集不相交。**
**目标未达成，不得归档不得提交。零 git 写。**

### R199 G34 实现与裁定逐字相符（编排者亲读代码核）；半改窗口已过，build 归零（2026-11-17）

**编排者亲读 `memory/ledger/journal_kv.go:190-200` 核实**：
```go
if k.closed && (len(batch.Puts) > 0 || len(batch.Deletes) > 0) { return ErrCommitAfterClose }
if len(batch.Puts) > 0 || len(batch.Deletes) > 0 {
    if k.log.addr != nil && k.log.addr.window != nil { return ErrCommitWithoutWriteRights }
```
⇒ **与 R192 裁定逐字相符**：①`takeWriteFaceLocked` 已删（grep count 0）②**读视图带写提交改具名拒绝 `ErrCommitWithoutWriteRights`**（新错误名表达「此视图不持写权」，非泛化失败）③闭视图提交仍 `ErrCommitAfterClose`（既有具名拒绝未被削弱）④**无静默 no-op、无返回 nil、无降级只读成功**。**「一目录一写者」不变量恢复成立**——读视图不再能绕过 `rootWriteLock` 提交。
**`go build ./...` ⇒ 0**，R197 记的半改窗口 build failed **已度过**，属瞬态非回归（该判断正确）。
**待 G34 战报复核**：三个 history 测试的写权声明修正、锚从「量错误机制」改为「量正确契约」＋负锚、`config.go:270` doc 与机理一致性、锁不变量实证、`event_integrity_test.go` 三处未修的归属声明。

**⇒ 待派解锁条件更新**：R193 窗口接线单需 `config.go`，**G34 白名单含该文件（仅 doc）且仍在飞** ⇒ **须待其战报确认收口后再派**，不得抢写。编排者本轮不派该单，改以后台全量跑取权威数（assembly＋history）。
**六单在飞**：G33／G34／G35／G37 写，G36／G38 读。
**目标未达成，不得归档不得提交。零 git 写。**

### R200 纯度判据①治理面升级为「有机械证据成立」（编排者亲验，非采信 G31 自报）（2026-11-17）

**编排者独立实跑**：`go test ./features/evolution/governance/ -count=1 -timeout 300s` ⇒ **EXIT=0，0 FAIL，`ok … 0.793s`**。
**并亲读加强后的锚确认它量对了轴**（`approval_channel_anchor_test.go`）：
- `:58` `book.Request(ctx, "open-hold", "shell_exec", …)` 建立**原审批身份**
- `:81` `keepBook.Decide("open-hold", approval.ApprovalApproved, "operator")` ⇒ **对原身份裁决**，不是对新铸身份
- `:91` `mirror := filepath.Join(dir, "approvals", "open-hold.json")` ⇒ **文件镜像按 R175 裁定二撤走**，夹具自证量的是切面而非目录第二来源
- `:127` `if !strings.Contains(decision.Reason, "open-hold")` ⇒ **断言闸门理由点名原身份**
⇒ **R175 裁定一要求的「原审批身份仍可裁决」已真实落地且通过**，不再是恒成立假锚。G31 报的变异③红原文（park 于新铸 `approval-d2825d1e…`）正是旧判据的病灶，现判据能区分。
**`policy.go:359-361` 的铸新身份路径仍在，但语义已正确**：`req == nil` 现在只发生在**确无先前请求**时（已决审批的身份经 R61 请求记录族＋`BindAttention` 从链解析回来），故铸新 ID 是对** genuinely 新请求**的正确行为，**不再是「查不到就另铸一个」的身份丢失**。

**⇒ 判据①（链唯一真相）治理面：从 R176 的「⚠️ 部分成立、主树确证违例」升级为「✅ 有机械证据成立」。** 依据：原身份经链解析回来并可裁决、闸门理由点名原身份、目录第二来源被夹具撤走且 G10 座后路径 N18 已绿（G31 ×10 复核）。
**⚠️ 但判据①整体仍不得声称全成立**：G2-G8（已存储治理事件被重铸号覆盖，**P1 红线**）**仍未修**——G31 明报「具名≠修复：既有洞下重启写者仍自重铸号覆盖，须洞语义设计冻结＋真实 localfile tier 锚同批」。**该条是判据① remaining 的唯一确证违例，须另单同批 G2-G10 的 tier 锚落盘。**

**纯度五判据现态更新（取代 R176 表）**：①**治理审批身份面✅／G8 重铸号覆盖🔴仍违 P1** ②耐久路径✅＋兜底本征已声明✅／**boot 字节读被全 log CRC 破🔴（G36 诊断中）** ③memory 双引擎已消✅／治理目录第二来源座后面✅／`NewMemoryOriginalStore` test-only 面待接线消（R193 已裁）／`newestSessionCheckpointRef` 待两步处置（R196） ④✅显著收敛 ⑤兜底界已声明✅／开档完整性校验已声明✅／**水合成本未声明🔴（G37 实测中）**／**恒绿假锚未修🔴**。
**后台全量（assembly＋history）仍在跑**，日志尚空，**不得据空日志下结论**。
**六单在飞**：G33／G34／G35／G37 写，G36／G38 读。**目标未达成，不得归档不得提交。零 git 写。**

### R201 G8（P1 红线）洞语义裁定冻结：洞不是链终点＋denial 写入须走同形按身份判重（2026-11-17）

**编排者读权威原文确证机理**（`07-governance-custody/design.md:204` G2-G8 行）：
> 切面@2 → 键 3 写被拒 → 键 4/5 落库 → 无新切面 → 重启走尾停在洞前 → 续写键 3/4，**键 4 正文被覆盖**

**两个独立缺陷叠加**：
1. **尾走查把「被拒写留下的空号」当成链终点** ⇒ 洞后已提交事实被视为未写，续写从洞号开始。
2. **`denial.go:143-159` 的 `Record`「只追加，无按身份判重」**（design:101 原文）⇒ **绕过了 P1 已有的同键冲突保护**，故重铸号是**静默覆盖**而非具名拒收。

**⇒ 编排者裁定（洞语义冻结，从 P1 与「统一契约胜多端口」推导，非新发明）**：
- **裁定一：洞不构成链的终点。链的真相是已提交事实的最高号，不是第一个空号。** 尾走查遇洞**不得停并把洞后视为未写**；须**具名报告洞**并据已提交最高号定续写起点（或具名拒绝续写要求显式处置）。**这与 G31 本轮落地的面 1（`foldHistory` 窗口具名化：座定窗口内缺号、尾读未达的已存键，逐键经既有 `governanceHistoryUnreadable` ERROR 通道具名）是同一语义在读侧的实例**，写侧须同形。
- **裁定二：denial 记录写入须走与其它事实同形的「按身份判重＋同键异内容具名拒收」**（P1 已冻结该语义：事实键=身份+终态，同键重放不添第二条、同键异内容按既有同键冲突拒收）。**不得保留「只追加无判重」的特异形态** ⇒ 重铸号覆盖变成**具名拒绝**而非静默覆盖。**这是消除特异化代码（判据④）与恢复链唯一真相（判据①）的同一动作。**
- **裁定三：两条须同批落地**，且 **G2-G8 的 tier 锚（真实 localfile）须落主树**钉住它（`g2govtier/` 新包改道已由 R175 裁定五批准）。**非空转守卫按 design:213 原文**：「G2-G6/G2-G8 核 tier 确持 3/4 键且洞确不存在、被拒写恰 1 次」——**任一守卫不成立即具名失败，判据不会恒真**。
- **裁定四（顺序硬约束，沿用 design:175/:242）**：**07.R2 候选甲不得先于本裁定落地**。今天全量 `Records` 是洞的掩盖层（洞后记录由切面而非尾回放供给）；**R61 已拆该掩盖层**（G31 上报），故**洞语义具名化现在是紧迫项而非可延后项**——掩盖层已不在，缺陷已暴露。
- **⚠️ 未量项（design:219 原文，须如实带上）**：G8 的**生产触发频率未量**——洞由被拒的 `Put`/`Sync` 造成，原型用注入构造，**未量真实存储上 `Put` 失败发生率**，也未走完整宿主 boot→崩溃→重启路径（原型是两次 `OpenTier` 同根＋逻辑重启）。**定它需跨进程夹具**：真实宿主第一代注入写失败并落旧切面、第二代 boot 后续写。**实施单须补该夹具或如实声明仍为注入构造。**

**⇒ 这是判据① remaining 的唯一确证违例（P1 红线），裁定已就绪可派单。** 前置：`memory/ledger` 写面须待 G34 收口（`g2govtier/` 引它）；`features/evolution/governance` 现已空出（G31 收口）。**写面**：`g2govtier/`（新包）＋`features/evolution/governance/denial.go`＋其锚＋`g2_valid_state_anchor_test.go`。
**dod gap 亲测 10→9**（G35 注册 P4 反例闭「未注册」那条）。**后台全量仍在跑，日志尚空，不据空日志下结论。**
**六单在飞**：G33／G34／G35／G37 写，G36／G38 读。**目标未达成，不得归档不得提交。零 git 写。**

### R202 G33 收口：R185 裁定经探针确证、三锚重表达绿、日志断言改为可观测；assembly 仅余长跑 1 红；甄别出一条独立缺陷（2026-11-17）

**✅ 前置取证确证裁定方向（非反转）**：探针实测 `TestRestoreFoldCounts` 场景任务 **`obligations=0`、status=4(StatusSettled)**，second boot `recovery=[] coverage=map[]`，**按身份 Lookup 水合成功 status=4** ⇒ `settledAtCut()` 真、检查点按设计陈述零、**事实未丢只是不被 eager 覆盖**。**R185 裁定成立，是陈旧锚非产品缺陷。**
**三锚重表达（与裁定四点逐条对应）**：①读回＝`chainStateOf` 与首跑 `sameSettledState` 全等（spec/status/revision/mutations）②**不变量锚**＝`len(report.Tasks)!=0` 即 fatal（**钉住「义务已清的终态任务不被陈述」，防将来把已结态搬回检查点**）③负锚＝移除再挂接后具名红 `host_delivery_test.go:2444 name the task … by identity: task: no task with that identity`，还原 md5 `0f38b251…` 一致；内置负对照 `RestoreChainSource(nil)`⇒`ErrTaskNotFound` ④**日志断言改为可观测后果**＝`durableLoadCountOf(second.commit.inner)==0`（commit 自带 LoadStats 读统计，**非日志文本**）——已结链 boot 零扫描、按身份读才付。
**模式感知自决采纳**：`TestEventIntegrityRestoreUse` 探针分叉——**seed-graceful**（有检查点）recovery=[]、**seed-kill**（无检查点 full-load）recovery=[task Recovered:true]，两模式读回皆成立 ⇒ 锚按模式分别断言（graceful `recovered` 须假、kill 须真），**这是钉住设计分叉而非削弱**。
**四冻结不变量自证**：未触 `runtime/task/checkpoint.go`（`settledAtCut` 未放宽）、正锚②防已结态搬回、未改全量 eager（`TaskIDs()` 有界界 untouched）、无 `--update-baseline`（基线 17 未抬）。**零生产改动、零 git 写、零越界。**

**现态（G33 实测＋编排者复核）**：**assembly 全量仅 1 FAIL＝`TestSymptomAnchorPerGenerationCostFlatOverALongRun`**（墙钟离散：boot 16.3ms vs 10.4ms 超界 13.5ms；**`indexWalkVisits`/`reads` 恒平 4**）⇒ **D1 同族三红全闭，关键路径只剩中心论点（G36 诊断中）**。
**history 全量 7 顶层 FAIL，G33 逐个甄别且明确不声称全套件绿**：①子测试2＝**独立缺陷（停下未改，落点禁改面）**②`TestEventIntegrityPersistence` 红于生产 `ErrCommitWithoutWriteRights`（`journal_kv.go:43`，G34 新拒绝）⇒ **并发瞬态**③`TestDurableTaskRoundTrip`/`TestProjectionReplayAndBudget`/`TestRestoreObservationThroughTheFormalEntry` **单独跑绿**⇒套件争用④`TestEventIntegrityConsumptionRecovery`/`TestEventIntegrityRestart` 子进程超时（30/90/150/270s）。

**🔴 新独立缺陷（G33 甄别，编排者立案派单）**：损坏链（**无 admission material、StatusAccepted**）使 `runReading.Tasks`（`assembly/read.go:466`）的 Lookup 传播 `ErrSpecMissing` → `Inventory.readTasks`（`memory/query/inventory.go:586`）`g.fail(err, ObjectInvocation, ObjectTask, ObjectNotice)` **三维齐败** → Query 硬错，而测试期望空 details。
**⚠️ 须先判契约方向再修**：该测试名是 `TestRestoreRefusesChainWithoutSpec`（**「拒绝无 spec 的链」**），故**具名拒绝可能正是期望行为**，而子测试2 期望空 details 可能是**陈旧期望或另一场景**。**不得默认「硬错＝缺陷」就改成返回空**——那可能把具名拒绝降级为静默空，违「派生不可信须退化读真相非拒绝」的反面（**该拒绝时须拒绝**）。**实施单须先读该子测试全文与主 spec 条款判方向。**

**dod gap 10→9**（G35 注册 P4 反例）。**R201 已冻结 G8 洞语义裁定**（P1 红线，判据①唯一确证违例），待 `memory/ledger` 写面释放即可派。
**在飞**：G34／G35／G37 写，G36／G38 读；G33 已收口释放 assembly 写面。**目标未达成，不得归档不得提交。零 git 写。**

### R203 assembly 全量独立确证仅余长跑 1 红（功能性红项全闭）；派 G40 收 G8 P1 红线（2026-11-17）

**编排者后台全量独立确证**：`go test ./assembly/ -count=1 -timeout 600s` ⇒ **FAIL 255.603s，`--- FAIL` 计数 1，唯一条＝`TestSymptomAnchorPerGenerationCostFlatOverALongRun`（26.05s）**。与 G33 自报一致。
**⇒ 里程碑：assembly 的功能性红项全部闭合**（原 10 红：A 簇 3／B 簇 2／C 簇 3／D 簇 2 → 0），**只剩中心论点的墙钟锚**（boot 16.3ms vs 10.4ms 超 1.3× 界，而 `indexWalkVisits`/`reads` 恒平 4）。**该条归 G36 诊断（亚线性增长是真是噪声、住在哪一行、全 log CRC 是否主因、离散守卫判据是否可辩护）。**
**不得据此声称变更达成**：中心论点未证＋G8 P1 红线未修＋判据③⑤各有未清项＋F1–F4 未做。

**派 G40 收 G8（R201 裁定已冻结）**：写面 `g2govtier/`（新顶层包，R175 裁定五已批准改道）＋`features/evolution/governance/denial.go`＋其锚＋`g2_valid_state_anchor_test.go`。**与在飞单写面不相交**（G34 写 memory/ledger 但 G40 只引它不写它；G37 写 runtime/task；G35 写 scripts/gates＋golden；G39 写 memory/query＋assembly/read.go＋history 一测试）。
**四条裁定写进令**：①洞不构成链终点，尾走查遇洞不得停并把洞后视为未写，须具名报告洞并据已提交最高号定续写起点 ②denial 写入须走同形「按身份判重＋同键异内容具名拒收」（P1 已冻结语义），消除「只追加无判重」特异形态 ③两条同批＋tier 锚落主树，非空转守卫按 design:213「核 tier 确持 3/4 键且洞确不存在、被拒写恰 1 次」④**掩盖层已被 R61 拆掉故为紧迫项**；另 design:219 的未量项（生产触发频率未量、原型是注入构造非跨进程夹具）须补或如实声明。
**在飞六单**：G34／G35／G37／G39／G40 写，G36／G38 读（G33 已收口）。
**目标未达成，不得归档不得提交。零 git 写。**

### R204 G35 收口（P4 两 gap 真闭）；勘误编排者「控制树须全绿」的过严判断；派 G41 全量真跑收余 8 gap（2026-11-17）

**G35 收口，编排者采纳其自决与关键澄清**：
- **选 B 弃 A 的理由成立**：B（删 `run.go:2587-2594` cut 分支）是**行为性反例**——`bootIndexWalk` 恒走 `walkIndexWhole`，`indexWalkVisits` 随历史线性增长（探针实测 visits=[0,45,96,147,198] **确定性线性**，gen2 named 96 > bound 90 在 `:834` 具名失败，墙钟仍平未先触发）。**A 的 `from` 仅喂 `openBytes` 度量面**（真实回放在 `openAddressIndex` 内另读水位），反例语义偏弱且须实证 G25 全 log CRC 不掩盖 ⇒ 弃 A 正确。
- 新 id **`MUT-DOD-P4-window-unbounded`**，`evidence_kind=named_assertion`（既有正式测试具名断言实杀，探针＋official runner 双证）。golden 只改 `mutation.id` 并删 `retire_pending`，**criterion/anchor/measure/scope/ci_gate/note 未动**；退役注释改写为重绑记录但**保留旧锚随退位消失的历史事实**。
- **live 真跑判决**：`mode=live`、`control_tree_green=True`、`status=killed`、`reason=named_assertion`、`assertion_hit=True`、`counted_killed=True`、`assertion_locations=[symptom_anchor_recovery_test.go:834]`、`exit_code=1`、`RUNNER_EXIT=0`。
- **两负例实证**：①改 `expect_assertion` 为 `:813` durable 面文本 ⇒ `status=not_killed reason=assertion_not_matched assertion_hit=False`（mutant 仍红但注册文本不命中）②锚加后缀零命中 ⇒ `stale mutation, the patch anchor occurs 0 times in assembly/run.go`，`gate.ok=False`、`mutations=[]`，**patch 失败非静默通过**。均内存 monkeypatch 零仓写。
- **R26 实证**：工作树两处锚块 count 各 1（未写）、`file_digest(tree)==file_digest(root)` 通过、go.mod/go.sum 未动、无 git add、日志只写 TMPDIR 与 /tmp。

**🔑 勘误编排者一处过严判断（重要，解锁真跑）**：我反复写「九变异实跑**硬依赖控制树全绿**」。**G35 实证：`control_tree_green` 是按该变异的锚选择器跑控制，不是跑全套件**——「控制对此变异绿（control 只跑锚选择器；assembly/history 余红属别的选择器，不在本控制内）」。**故 assembly 余 1 红（长跑墙钟）与 history 余红不阻真跑**。⇒ **R164/R178/R187 等处的「硬依赖控制树全绿」须一律勘误为「按锚选择器判控制绿」**，真跑**现在即可执行**，不必等全部红项闭合。

**派 G41 全量真跑收余 8 gap**：dod 现 **9 gap**（编排者亲测），余 8 全是 P0/P1/P2/P3/P5/P6/P7/P8 的「no run records」。该门有 **`--mutation-evidence PATH`** 旗标读 `evidence.json`（G35 实测带旗标 9→8）。runner 有 `--all --json --out`。
令内要点：①先 `--list` 报九变异清单与 enabled/pending ②全量 live 真跑逐变异报判决 ③带证据复跑 dod 报 gap 逐条归因 ④**未 killed 者分四类处置**：注册信息腐化（可修但须亲跑取真实原文）／**产品真缺陷（停下报不得改注册掩盖）**／**假锚恒绿（停下报）**／**在飞瞬态（复跑确认，不得当注册腐化去改）** ⑤**证据目录必须在仓库外**，报绝对路径＋接续者复跑确切命令 ⑥**核 CI 形态一致性**（`event-integrity-mutations` job 用 `--out $RUNNER_TEMP` 上传 artifact，`event-integrity-required` 用 `check_runtime_evidence.py --bundle` 聚合）——**本地产物能否被 bundle 消费影响 F2 能否声称门绿**。
**⚠️ 已写明在飞风险**：runner 复制当前工作树，在飞单正改生产代码 ⇒ 副本可能含半改态，**须对失败变异复跑确认是否瞬态并声明受影响读数**。

**七单在飞**：G34／G37／G39／G40／G41 写，G36／G38 读。触碰集不相交（G41 写面 scripts/gates＋golden 已由 G35 释放）。
**目标未达成，不得归档不得提交。零 git 写。**

### R205 🔴 F3 现有证据已陈旧不得计入完成：S1 锚与两处回滚机制均已删除，须按当前锚集重做（2026-11-17）

**编排者读 `run-notes.md` §F3 发现它已做过一次规范的隔离回滚实验**（整树副本 `/tmp/recovery-f3/tree`，rsync 排除 `.git`/`examples`/`docs`/`openspec`/`logs`；回滚只改副本；复原以 md5 与主树逐字节相等为凭；主树三文件 md5 会话前后一致）。**方法正确，但证据对象已消失。**

**亲测确证（grep 全仓含未跟踪，命中数）**：
| 符号 | 命中 | 含义 |
|---|---|---|
| `RetireFacts` | **0** | S1 回滚手法所引机制已删（退位反转，用户裁定） |
| `relocatableStorage` | **0** | S2 回滚手法所引装配函数已删 |
| `TestJournalBytesBoundedByLiveSet` | **0** | **S1 锚本身已不存在** |

⇒ **F3 表 S1 行整行陈旧**（锚与机制双删）；**S2 行的回滚手法陈旧**（锚 `TestSymptomAnchorBootCostFlatAcrossGenerations` 仍在且编排者本轮亲验 exit 0，但「`relocatableStorage` 原样返回」这个回滚手法已不可执行）；S3 行手法 `foldTicketLineOf` 须复核是否仍在。
**且 S1 的退役是正确的、不是遗漏**：台账 :824 早已指出「**S1 本身可能量错面**——它量文件大小，而用户原症状是**成本**不是磁盘；append-only 日志天然增长，若开档成本有界则文件增长本身不是病」，:847 列「**接受磁盘无界增长**」为待用户裁项，而退位反转裁定（08 域 banner 用户原话）已采纳可寻址索引⇒**磁盘增长被接受，S1 那个「文件字节有界」判据随之退役**。其红原文「journal.log holds 356558 real bytes (os.Stat)…grow linearly with the settled history」量的正是被接受的磁盘轴。
**另 S2 的绿量数字也已过期**：notes 记 `journalBytes=[0 110 110 110 110] indexEntries=[0 2 2 2 2] durableLoadCount=[0 0 0 0 0]`，而 G25 加了开档全 log CRC 后 `JournalBytesOpen` 只报索引窗口（R182/R183 已声明是两个量），**该数字不再代表 boot 实读字节**。

**⇒ 裁定：F3 未完成，现有 §F3 须标为历史证据并附勘误（不得改写原文，按「历史只附勘误」纪律），另按当前锚集重做。**
**重做要求（待派，前置＝树稳定，现七单在飞不宜做隔离副本）**：
1. **先定当前症状锚集**：S1 已退役须明确记录退役理由与替代判据（成本轴由 S2＋长跑锚承担）；S2 boot 成本锚、S3 midband 召回锚仍在；**长跑锚 `TestSymptomAnchorPerGenerationCostFlatOverALongRun` 是否纳入 F3 须裁**（它是中心论点的锚，现 2/3 run 破界，G36 诊断中）。
2. **每个锚须给当前可执行的回滚手法**（不得引已删符号），且**回滚红须是行为红非编译错**（原 notes 这点做对了，沿用）。
3. **分母自证**沿用原纪律：红日志须同时陈述「该量线性增」与「对照量恒定」，证非空转。
4. **隔离法沿用**：整树副本＋md5 复原凭据＋主树零写入（因实施全部未提交，`git worktree` 于 HEAD 不含修复，故不能用 worktree）。
5. **两次证据入 notes**：回滚红＋复原绿，附 run-id/命令/退出码/原文。
6. **⚠️ 须待树稳定**：runner/副本会复制当前工作树，七单在飞时副本含半改态 ⇒ **F3 重做必须排在所有写单收口之后**，否则证据不可信。

**⇒ 完成审计更新（取代 R179 的 F3 判定）**：F3 **未完成**，且**先前台账未识别其证据已陈旧**——这是编排者本轮第七次「未核对象是否仍存在就沿用旧结论」的同型病根（前六见 R190/R196/R200 等）。**教训：引用既有凭据前须先 grep 其锚名与机制名是否仍在树。**
**两症状锚本轮亲验仍绿**（S2 boot exit 0、S3 midband exit 0），**但这只是「当前树绿」一条腿，F3 要的「回滚红＋复原绿」两条腿须重做**。
**七单在飞**：G34／G37／G39／G40／G41 写，G36／G38 读。**目标未达成，不得归档不得提交。零 git 写。**

### R206 G34 收口（安全裁定成立并已落地）；反驳编排者「唯一消费者是其新锚」——实为 8 包约 20 条夹具承重；派 G42 补声明（2026-11-17）

**✅ 安全修复已落地，编排者采纳**：删 `takeWriteFaceLocked` 及调用；读面判据用 **`k.log.addr.window != nil`**（`journal.go:868` 读视图专属，**单一真相不新增字段**）；新错误 **`ErrCommitWithoutWriteRights = "memory: commit through a view that holds no write rights"`**，与 `ErrCommitAfterClose` 同风格，**写前拒绝、不包 `ErrCommitUncertain`**（守 `atomic_store.go:49` 契约）；空批仍走屏障。**锁实证**：持写权开档后第二写权开档 `ErrRootWriteLocked`（本进程＋跨进程锚均 PASS），读视图旁路提交被具名拒绝且写者后续提交完好 ⇒ **「一目录一写者」不变量恢复**。
**锚重写**：`TestJournalCommitThroughAReadFaceIsRefusedByName` 钉具名拒绝＋非 uncertain＋无键落地＋不建文件＋deletes 同拒＋活写者旁二写开被锁拒＋写者路径完好。负锚：还原静默升级 ⇒ 具名红 `journal_kv_test.go:171 commit through the reading view = <nil>, want …`，`diff -q` 还原一致。
**验证**：build 0／vet 0／ledger 全套 0(59s)／锚 count1=0 count3=0／两症状锚 PASS／concept 0 dead 0 vocab 0／代码零残留。

**🔴 反驳编排者前提（第八次同型病根）**：我与 G32 均称「零生产消费者，**唯一消费者是它自己的新锚**」。**G34 删后实测：8 包约 20 条夹具经读视图提交**，该机制实为它们的承重墙——**皆写者误声明为读者，非生产路径**。⇒ **安全裁定成立（生产确零依赖），但「唯一消费者」错**：我只 grep 了 `takeWriteFaceLocked` 的调用方与生产侧 WriteRights，**未枚举「哪些夹具经读视图提交」**。**教训：判「零消费者」须枚举该机制承载的全部行为路径，不只是符号调用方。**
**G34 已修其白名单内三测试**（durable_task 5 处、projection 1 处、restore_observation 1 处补 `WriteRights: true`，**断言零改动**，均在 tier.Close 后重开无锁冲突），**未越界修其余**。

**🔴 新增 20 红（全带具名拒绝，同 G15 A 簇形态，派 G42）**：`runtime/state` `TestDurableCommitBarrier`；`tests/integration/host` 4（owed_headless/fact_fidelity/reentry/settle_pending 故障 rigs 内引擎读面）；`capabilities` 5；`execution` 2（handoff/settle_wake）；`acceptance` 2；`config` 1；`inspection` 5。**另 G34 甄别非其红**：host 既有 5 红、execution `TestNativeConversationRoundTrip`（零 WithStorage，guard 不可达；runtime/task 多文件基线后被在飞单改动）。
**另 comment_policy 现 1 finding**：在**未跟踪** `runtime/task/manager_test.go:113`（`no longer`，HEAD 无此文件，**G37 在飞新文件**），G34 越 whitelist 不可修 ⇒ **归 G37 收口时自清**（其令已含注释形状硬约束）。

**派 G42**：8 包夹具补 `WriteRights: true` 声明（**改开档声明≠改断言**，与 G15 A 簇/G34 同形）。**排除 G37 在飞的 `runtime/task/**` 与 G39 在飞的 `history/restore_refuses_chain_without_spec_test.go`、`memory/query/**`、`assembly/read.go`**；`event_integrity_test.go` 三处属 G33 已收口面，**G42 可修**（G33 已释放）。
**在飞**：G37／G39／G40／G41／G42 写，G36／G38 读。**目标未达成，不得归档不得提交。零 git 写。**

### R207 G34 收口释放 memory/ledger 与 config.go；派 G43 执行 R193 窗口接线（关 product_surface 红门）（2026-11-17）

**派 G43**（R193 裁定已就绪，前置满足）：给 `StorageConfig` 加原文召回窗口字段（policy→duration，与 `LifecycleConfig` 的 json/yaml 风格一致），由 `OpenTier` 传进 `newKVOriginalStore(store, windows)`；**同批删** `NewMemoryOriginalStore`／`MemoryOriginalStore`／`MemoryOption`／`WithRetentionWindow` ＋ `factOriginalsOf` 的 memory case（唯一调用方 `segment_store.go:783`，删后单 case ⇒ 按「单使用方不抽象」内联为一次类型断言）＋两处编译期断言块（`reference.go:241`、`tool/recall/skeleton_assert.go:25`，**不得整文件删 skeleton_assert**）；迁剩余两测试（`scenario_s07`、`original_reference_isolation`）走生产配置入口，**断言零削弱**，`RecallExpired`/`ErrOriginalExpired` 子测须真跑非 SKIP。
**令内三个易错点已写明**：①**`BlobCount`/`References` 删类型后外部包如何可达**——须先取证其生产消费者，若只有测试用则改内部测试或经既有导出面等价表达，**不得为它们新造导出接口**（那会新增公共面撞同一门）；等价不可得则停下报 ②**范围硬约束：只做窗口注册接线，不做 `NewLifecycle`+`Retention` 完整 TTL 执行面接线**（属独立变更不得夹带），**接线前不得声称「按策略过期」已达成**，wiki 缺口表须更新为「窗口注册已接线，TTL 执行面仍未接线」 ③**新公开字段须记理由**（消除 test-only 导出面＋使在册未接线机制获得生产入口），供编排者落台账与 06 design；**不得自行抬 concept_budget 的 D1/new_max 棘轮**，若红须按名抬升协议报。
**负锚要求**：临时把构造器加回须使 `check_product_surface` 重新具名红，还原 `diff -q` 一致。**核心交付＝该门 EXIT=0**（起点 1）。

**八单在飞**：写＝G37（runtime/task 八面水合）／G39（无 spec 链查询契约方向）／G40（G8 P1 红线洞语义）／G41（九变异全量真跑）／G42（8 包夹具写权声明）／G43（窗口接线）；读＝G36（中心论点诊断）／G38（F1 续审）。**写面互不相交**，但**同仓并发已实证会造成套件争用与瞬态红**（G33/G34 均报「单独跑绿而套件红」），故各单令内均要求**单独跑确认＋区分瞬态与真红＋不得声称全套件绿**。
**编排者注意**：八单并发下全量套件读数不可作权威现态，**收口后须待写单全部落地再重测全量**（同 R205 对 F3 重做的前置要求）。
**剩余未派（前置未满足）**：恒绿假锚 `journalBytes==0` 修复（待 G36 结论＋assembly 包释放）；`newestSessionCheckpointRef` 两步处置（待 assembly 包释放）；水合成本声明（待 G37 实测数字）；package-layout 17（跨多包须待写单收口）；godoc 重生成（须待全部代码落地）；F3 重做（须待树稳定）；R35 第二轮／F1 完整反夹带／F2 全量／F4 归档（须待前述全部）。
**目标未达成，不得归档不得提交。零 git 写。**

### R208 完成审计更新＋交接账（取代 R179；八单在飞，预算将尽）（2026-11-17）

**目标三句逐项判定（据本轮全部亲测证据更新）**：

**句①「整理计划，并行派发推进任务优雅完成」**
- 整理计划 **✅**：`tasks.md` 按实测重写；F2 门禁面逐门实测清单（R149/R169）；待派项按依赖序＋每项前置原因；台账 R0–R208 连续；**八单并发写面互不相交**，全相交时明写「N 单即最大并发不凑数」。
- 并行派发 **✅**：本轮 resume 后派 G33–G43 共 11 单，每单附白名单／禁改面／冻结不变量／注释形状约束／负锚要求／预算纪律。
- 优雅完成 **❌**：见下方缺口清单。

**句②「review 已实现部分，核对是否有违规概念和词汇」 ✅ 达成**
- 逐单读 diff＋亲测复核（G17/G19/G23/G25/G26/G31/G33/G34/G35）；多次 A/B 同树对照归因；**抓到并纠正八类假绿/误判**（后台通知 exit 是包装 shell 码／空日志零红／`-run` 零匹配／自测绿≠门面绿／zsh 不分词／comment_policy 窄 scope／锚未落盘≠缺陷未修／既有凭据整体陈旧）。
- 词汇面：`check_vocabulary_alignment` 0、`check_concept_budget` 0（stale 0）、`comment_policy` 全 scope 曾测 0（现 1 finding 在 G37 未跟踪新文件，归其自清）、`name-check` 0、2213 行新增注释零真泄漏。
- **裁决归因真话**：两处裁决面扩大已勘误（R172）；三主 spec 改动经域 banner 用户原话确证为忠实实现（R191），**G32 的「未授权弱化」被反驳**。

**句③「不考虑成本只考虑架构优雅纯粹性落实执行」 ❌ 未完全落实**（五判据，R200/R205 更新）
| 判据 | 状态 | 缺口 |
|---|---|---|
| ①链唯一真相 | **⚠️** | 治理审批身份面✅（亲跑＋亲读锚断言确证）；**G8 重铸号覆盖仍违 P1 红线**（G40 在修，R201 洞语义已冻结） |
| ②派生载体不迫使全量读 | **⚠️** | 索引走查✅（252 代恒 4、`judgeCountedFaces` 通过、负对照通过）；兜底本征已声明✅；**boot 字节读被开档全 log CRC 破**（R182 裁保留因安全＋冻结契约，成本已声明 R183，**但恒绿假锚 `journalBytes==0` 未修**）；**中心论点「每代总成本有界」未证**（长跑 2/3 run 破界，G36 诊断中） |
| ③无第二真相 | **⚠️** | memory 双引擎已消✅；治理目录第二来源座后面✅；**test-only 导出面待接线消**（G43 在做）；`newestSessionCheckpointRef` 待两步处置（R196） |
| ④更少特异化代码 | **✅** | 三端口收口为一、memory 特异分支删、migrate.go＋四孤儿端口删、退位机制整体删、cli feedback 自证链删、24 死生产者删、无锁写面升级删（G34） |
| ⑤本征成本归索取者并显式声明 | **⚠️** | 兜底界✅、开档完整性校验✅（R183）；**懒水合 O(历史键容量)×5 未声明**（G37 实测中） |

**🔴 未达成缺口清单（具名，按依赖序）**
1. **中心论点未证**：长跑锚 2/3 run 破界（boot 1.4–1.6× 亚线性＋机器离散）⇒ G36 诊断（真/噪声、住哪一行、全 log CRC 是否主因、离散守卫判据是否可辩护）。**这是变更的核心承诺，不得以 boot 三绿宣称完成。**
2. **G8 P1 红线**（G40 在修）＋G10 tier 锚落盘须同批。
3. **恒绿假锚** `assembly/symptom_anchor_recovery_test.go:465` 的 `journalBytes==0`（量不到完整性校验成本）⇒ 待 G36 结论＋assembly 包释放。
4. **八面懒水合**（G37 在修，`Obligations` 现时可达靠隐式顺序）＋**水合成本声明**（待其数字）＋`assembly/read.go:473→477` 隐式顺序消除（另派）。
5. **窗口接线**（G43 在做，关 `check_product_surface`）。
6. **8 包约 20 条夹具写权声明**（G42 在做，G34 安全修复暴露）。
7. **无 spec 链查询契约方向**（G39 在判，**不得默认硬错＝缺陷**）。
8. **九变异全量真跑**（G41 在做，收 dod 余 8 gap；**控制树按锚选择器判绿，非全套件**——R204 勘误）。
9. **`newestSessionCheckpointRef` 两步处置**（R196：先迁锚到生产路径再删，**不得直接删**）。
10. **`package-layout` 17 条**（HEAD 既有＋2 新增，跨多包须待写单收口）。
11. **`proc-refs` 1**（属另一活动变更 `no-prefix-criterion` 的未跟踪工作，**非本候选，不夹带改它**）。
12. **`tracked-hygiene` 13**（删除未入索引，**暂存态⇒全量 lint 须排在暂存之后、提交之前**）。
13. **godoc 重生成**（`memory__ledger.md`＋`runtime__task.md`，须待全部代码落地，编排者单写）。
14. **🔴 F3 须重做**（R205：现有 run-notes §F3 的 S1 锚与两处回滚机制**全零命中**，证据整体陈旧；须按当前锚集＋当前可执行回滚手法重做，**且须待树稳定**因隔离副本会复制半改态）。
15. **F1 完整反夹带**（G32 审 20%＋G38 续审中；**须报合并覆盖率**）。
16. **R35 第二轮独立放行审**／**F2 全量门禁**／**F4 归档拒绝有效性**（须待前述全部）。
17. **两条既有缺口归独立变更**：feedback 跨包孤儿（**转移项须立承接实体否则归档丢失**）、`journal.go:544` 长度域翻大被当 torn tail → **Truncate 静默切掉后续全历史**（安全级数据丢失，两函数相反判决，HEAD 同形；**保留开档全扫把其暴露面扩到全历史，已声明**）。

**在飞八单（预算尽时可能未收，接续者须先核实际落盘再判完成，门禁数字须重测）**：
| 单 | 写面 | 目标 |
|---|---|---|
| G36 | 只读 | 中心论点诊断（亚线性真/噪声、住哪行、CRC 是否主因、离散判据） |
| G37 | `runtime/task` 五生产＋其测试 | 八面懒水合＋成本实测 |
| G38 | 只读 | F1 续审未覆盖高风险面 |
| G39 | `memory/query/inventory.go`＋`assembly/read.go`＋history 一测试 | 先判契约方向再处置 |
| G40 | `g2govtier/`（新包）＋governance `denial.go`＋锚 | G8 P1 红线（R201 四裁定） |
| G41 | `scripts/gates/check_event_integrity_mutations.py`＋`golden/dod_predicates.json` | 九变异全量 live 真跑收 dod 余 8 gap |
| G42 | `runtime/state`＋6 个 integration 子套＋`event_integrity_test.go` | 8 包夹具补 `WriteRights: true` |
| G43 | `memory/ledger` 五文件＋两 skeleton_assert＋两测试＋两 wiki | 窗口接线消 test-only 面（关 product_surface） |

**已收口单**：G33（三锚重表达，assembly 功能性红全闭）／G34（无锁写面升级删＋具名拒绝，锁不变量恢复）／G35（P4 两 gap 真闭，dod 10→9）。
**门禁现态（编排者最近亲测）**：build 0；concept/dead_producers/vocabulary/doc_budget/durable_truth 0；**dod 1（9 gap）／product_surface 1**；comment_policy 1（G37 未跟踪文件）；godoc 红两文件；proc-refs 1／tracked-hygiene 13／package-layout 17。
**⚠️ 八单并发下全量套件读数不可作权威现态**（已实证套件争用与瞬态红），**须待写单全部落地再重测**。
**目标未达成。不得归档、不得提交、不得推送。全程零 git 写，HEAD 仍 `9547faca`。**
**冷启动读法**：R208（本条，审计＋缺口＋在飞表）→ R205（F3 陈旧须重做）→ R204（控制树口径勘误）→ R201（G8 洞语义裁定）→ R193（窗口接线裁定）→ R196（死函数两步处置）→ R185（陈旧锚裁定）→ R182/R183（完整性校验裁定与成本声明）→ R179（旧审计，已被本条取代）。

### R209 G38 的 F1 续审收口：合并覆盖 44%、审过面夹带 0、新门拒绝力全成立；查出三处文档假话与一条跨变更级联（2026-11-17）

**覆盖率（带分母，不得当已全审）**：当前树 `git diff HEAD` **808 hunk**（Go 497／非 Go 311，较 G32 时 782 已增长因在飞单续写）。G38 逐 hunk/逐文件审 **≈193（24%）**，与 G32 的 20% 合并 **≈44%**（范围互斥重叠极小）。
**✅ 审过面夹带 0 条**：全部改动可归属（四层去物质化／检查点只携仍欠项／FactLog 收口且红线迁移加严／退位删除且 4 个旧测试文件删除**有承接**：索引切割面→水位锚组、冷档→fold-cold-root／WriteRights endRun 级联＋跨进程 flock 锚／门建设／契约同步）。
**✅ 重写历史凭据：审过面无**（两份 run-notes.md 回迁**零删除行**）。⚠️ 回迁 RM 件（ORCHESTRATION/tasks/design）内容与 orchestration 主 spec −123 **未逐行审，不声称已清**。
**✅ R196 裁定被遵守**：`newestSessionCheckpointRef` 函数与测试调用**均保留**，未执行删除（符合「先迁锚再删」）。
**✅ G37 八面水合判定依据经 G38 独立亲验成立**：8 个按名索取面位置属实，**现树均已走 `hydrateLocked`**（manager:65/102/167 等）⇒ G37 已落盘。

**🔴 三处文档假话（须修，一处重）**：
1. **重｜`docs/wiki/memory/memory-architecture.md` §3.2**：仍写旧三步写入「步2 `Originals.Put`（facts.go:237）→步3 StoreEvent」「步2成步3败留孤儿 ref/blob」。**实际 `persistFact`（facts.go:355）走 `ledger.CommitFact` 单批六键，失败零键落地（有锚）**。**§十二缺口表 :259「原文仓先于段提交落地」与契约表 :41 同引 facts.go:237，三处皆假**（facts.go 已无 `Originals.Put`）。**⚠️ 该文件正在 G43 白名单内（其只被令改缺口表 retention 一行）⇒ 编排者须把 §3.2/契约表 :41 两处追加给 G43 或另派，不得抢写。**
2. **中｜`denial_ledger_anchor_test.go:49`**：测试名 `…IsNamedNeitherToCallerNorLog` **与所钉 invariant（具名或不计数）相反**；失败文案引 `denial.go:317-326 returns void` 描述**已删机理**＋行号漂移。**属 G40 白名单（governance 锚）⇒ 追加给 G40 或另派。**
3. **轻｜`atomic_store_test.go` `reopenTier`**：双 doc 并存，首条「reopen a process restart performs」已假（实为 `WriteRights=false` 伴生读视图）。
4. **轻｜`doc_budget.json`**：基线 22153→22149 下调**未具名入 note**（违「数字溯源」）。
**另两处轻锚病**：`TestAddressableIndexKeepsItsAddressIndependentOfTheKeyFamily` 探测链在测试内自模拟，仅钉生产哈希函数＋probe 常量 ⇒ **生产改探测策略不红**；`TestSecondProcessOnOneRootIsRefusedWriteRights` 注释称「refused by name」但**断言只查无 "admitted"＋正文未覆盖，未断言具名拒绝文案**。

**🔴 跨变更级联（须告知另一变更）**：`no-prefix-criterion`（另一未跟踪变更）的 allow 表仍列 **`segment_store.go:coldIndexLocked`——该函数已被本候选删除**。其门未接 lint/CI，**接线前须级联更新否则 stale 红**。**本变更不夹带改它，但须立案告知。**

**✅ 新门拒绝力全部成立（G38 逐个亲跑自测）**：`check_dead_producers` **含负例且真红 15/15**（互调家族/skeleton/注释提及/过期豁免/零对象均按退出码判红）；`check_dod_coverage` 门锚分支 **39/39 含 print-only 负例，退出码按 AST 来源识别非名字**（R171 裁定的绑定来源判定已落地）；mutations 门判决逻辑健全（绿/超时/文案不命中均 NOT_KILLED，控制须绿，含 gate_green 负例）；`hygiene.checkWhitelistUnnamedSource` findings 非空⇒exit 1（函数级负例有，**退出码共径无独立自测**＝轻缺口）；`check_runtime_evidence` 新分支 **112 自测 OK 含 liveness/凭证负例，加强非放宽**；`check_plugin_mounting` **加强**（派生集只增、literal 洗白阻断、豁免由文件缩为形状）；`concept_budget` ratchet **未测 scope 改具名 warn＝R171「无对象＝具名 warn」规则落地，自测钉双向，非放宽**。

**仍未审面（≈56%，不得当已全审）**：runtime/task 54（G37）、tests/integration 73、assembly 生产 49＋host_delivery_test 11＋其余测试残量≈30、docs/api 残量≈85、openspec 91（含 RM 内容）、mutations 门残量 46＋自测 9、durable.go 前半 14＋durable_test 6、recover-kv main_test 13、mounting 残量 9、ledger 生产残量≈60、examples 16、cli 6、ci.yml 1、dod_predicates.json。
**G38 对中心论点的结论**：锚**已量对轴**（逐阶段墙钟＋计数面＋三控制），但 **252 代实跑未验**（build 瞬时态禁跑）⇒ **仍未证**，与 R208 判定一致。

**⇒ F1 判定更新**：**审过 44% 面夹带 0、历史凭据未重写、新门拒绝力全成立**；但**覆盖率不足声称 F1 完成**，且**三处文档假话＋一条跨变更级联待处置**。
**在飞七单**：G36／G37／G39／G40／G41／G42／G43（G38 已收口）。**目标未达成，不得归档不得提交。零 git 写。**

### R210 🔴 中心论点被严格实验否证：真主因是本候选把内存 map 遍历改成磁盘全表扫（38.7×），非 CRC；G37 收口＋水合成本已声明（2026-11-17）

**G36 诊断（配对法，n=21 对×3 run，奇偶换序，取比值中位；本机 load 5.3–18.1／12 核故全部取配对中位＋复跑）——编排者采纳其全部结论**：

**① 增长是真的，不是噪声**：depth6 对照 0.999/0.999/0.932；**depth252＝1.643/1.735/1.724**，**p10＝1.443/1.473/1.452（三 run 十分位全高于 1.3 界）**。单调：30→1.13、60→1.19、120→1.28、180→1.40–1.58。真锚 `-count=3`＝FAIL/PASS/FAIL，run3 boot 窗中位 15.522 vs 9.476ms＝1.638× 互证。前提核对全部成立（`restored`＝`EventCount()` 是**分母**、+11.00/代严格线性、:166 明断言须增长；visits 恒 4；负对照与 `judgeCountedFaces` 三 run 均先过）。

**② ~78% 的 boot 增量住在两条 O(历史) 读，而现有四量全量不到它们**：
- **🔴 主因｜`journal.go:1490 eachRecordOnce`**：`for base:=0; base<a.capacity` **逐批 pread 整张 slots 表**，`eachHeld`(:1677) **在 visitor 内才滤 prefix**。boot 调用链＝`assembly/commit.go:221 configChain.loadLocked`→`tier_facts.go:35 Scan`。实测 scan 中位 **61.5µs（6 代，slots 196,672B）→2383.8µs（252 代，6,291,520B）＝38.7×**。**零匹配反证**：`configPairs=0` 时 scan 仍 14.96µs(0 键)→576.4µs(6000 键) ⇒ **随表容量而非命中数增长**。**🔑 HEAD 是内存 map 遍历（HEAD journal.go:1151），本候选改成磁盘全表扫 ⇒ 这是本候选引入的回归，不是既有债。**
- **次因｜`journal.go:615` 开档全 log CRC**（G25 加）：实扫 8.41MB、2.0–2.4ms；`openBytes=size-from`(:630)＝0 故 `JournalBytesOpen` 量不到它。
- 合计 ~4.2ms／boot 增量 5.3–6.3ms＝**~78%**。余 ~1ms 在 order.log（26KB→1.28MB）与发布追加（❓未逐行归因）。

**③ G25 的 CRC 非主因（编排者先前判断成立）**：副本只把 :615 的 `0` 改回 `from`，depth252 比值 1.643/1.735/1.724→**1.558/1.549/1.426**，tierOpen 比 1.98/1.97/1.78→1.22/1.11/1.12，**residue 增量不变（+3.42 vs +3.48ms）** ⇒ CRC 只削 ~0.17 比值，**boot 增长先于它存在**。**R182 保留该校验的裁定不受影响**（安全＋冻结契约），但**它不是中心论点未达成的主因**。

**④ 离散守卫判据不可辩护（轴不对齐）**：`judgeFixtureDispersion`(longrun:318) 量 `middleCost`(:366)＝**total(boot+work)**，而界(:180＝13) 是**逐 stage** 判；**boot 仅占 total 13–18%**。实测 total 离散 1.011/1.141/**1.640**：**run1 Fatalf 在守卫上、真增长根本没被判；run2 同噪声放过 1.297× 的 boot**。同深度单样本 p90/p10 达 1.41 ⇒ 单次采样分不开噪声与增长。**⇒ 先前「FAIL/FAIL/PASS 是机器离散」的归因须勘误：那是判据量错轴，不是机器噪声掩盖真增长。**

**⑤ 中心论点当前可辩护表述（取代一切先前乐观表述）**：
- **能声称**：**boot 索引走查面有界**（visits/valueReads 恒 4、valueBytes 252→268，9 次 run 一致）✅
- **不能声称**：boot 字节读有界 ❌（①②破）／每代总成本有界 ❌（total 1.08× 只因 boot 占比小，未证）／墙钟 1.3× 内 ❌（2/3 红，p10 1.44–1.47）
- **一句**：**只兑现「索引走查有界」，未兑现「恢复成本随历史有界」；boot 在 252 代实测 1.64–1.74×，~78% 来自四量观测不到的 O(历史) 字节读。**

**⇒ 派 G44（三交付）**：①**消除 boot 侧全表扫**（先取证 boot 那次 Scan 真正需要什么、能否用既有寻址按前缀收窄；**若本征不可收窄则停下报候选形态，不得放宽界或假装有界**；**不得引入第二真相/常驻索引，若认为必须则停下报**）②**补两个观测量** `integrityBytesScanned`（:615 实扫字节，**勿改 `openBytes` 语义**否则 `TestJournalOpenReadStatesTheWindowPastTheWatermark` 红）＋`slotTableBytesScanned`（:1490 capacity×stride），入 `longRunCountedFaces` 与 recovery 四量，**锚钉线性事实或钉有界＋负锚**③**判据校准四点**（守卫改量 boot 同轴／配对中位 ≥21 对换序／显式噪声带＝同深度对照 p10–p90 且要求 held p10>对照 p90／计数面优先），**禁调大 1.3× 界、禁删守卫**。
**⚠️ 附带发现须 G44 取证不自行改**：**每次 boot 追加 1 条 config fact**（252 代根再 boot-only 120 次→configPairs 372＝252+120；restored +1、journal +5.4KB/boot）；`publish.go:459` unchanged 短路需 `effective.Generation!=0`，boot 是否从耐久链回填未确认 ⇒ **boot 可能非只读、config 链随 boot 次数线性增长**，且「同根重复 boot」非严格等同工作（配对每对各 +1，比值略被压缩）。

**✅ G37 收口（八面懒水合）**：八面统一 `hydrateLocked`，与既有三面逐字同构；**接收者形态枚举三种**（`r.tasks`／`m.registry.tasks`／局部 `registry.tasks`，原 grep 漏第三种）；**纠正 G22 一处审计：`Flush` 确有生产调用方**（`invoke.go:676 offerOwed`、`delegate.go:668`），只强化修复不推翻。八正锚＋八负锚，回退水合各具名红、还原 `diff -q` **8/8**；`-count=1`/`-count=3` 全绿；`-race`×2 零竞态；**`Obligations` 不先 Lookup 直调实测答对**（隐式顺序依赖已破，`read.go:473` 的 Lookup 仍服务 spec/status 字段本身，是否可消另派）；comment_policy 全 scope **0**（其新文件 `no longer` 已自清）；package-layout **17→17**；`TestCheckpointStatesNoSettledTaskAtAll` 绿证已结不回 checkpoint；hydrate 调用点 14＝既有 6＋新 8，**遍历面零加**。
**成本实测（G37，journal tier 独立程序，生产接线）**：200 已结＝首次按名 **456µs／5 次前缀 scan**，每 scan 顺序读整槽表 **4096 槽×96B=393216B，×5≈1.97MB**；命中 **250ns／0 scan**。1000 已结＝槽表 16384（4×）、首次 **2.6ms（≈4×）**、×5≈7.86MB；命中 167ns。**checkpoint 体两深度恒 973B**。
**⇒ 编排者已把该成本补进 06 design §十九·七 成本表**（新行「已结任务按名索取（懒水合首次）」＋声明块：机理 5 次前缀 scan 各顺序读整槽表／**归索取者不摊进 boot**（boot 只落座仍欠义务任务，八面按名索取者付，遍历面一律不加水合）／**现布局本征**，若要有界修复面在槽表按前缀可收窄而非放宽声明／判据引 R121.3、R135⑤／**此前未列入＝未声明，现据实测补**）。**判据⑤最后一条未声明项就此闭合。** doc_links/vocabulary 复测 0。
**另亲收**：`doc_budget.json` 基线 22153→22149 的下调**补具名 note**（−4 行＝删 governance 契约范例目录＋url-fetcher 对照叙述节，`--tighten` 锁进），**baseline 值未动**（assert 校验），门与自测均 0。

**在飞七单**：G39／G40／G41／G42／G43／G44 写，G36／G37／G38 已收口。
**🔴 目标未达成，且中心论点已被严格实验否证——不得以「索引走查有界」「症状锚绿」「功能性红全闭」宣称达成。不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R211 结论层与顶层计划账按 R210 否证结论重写一致（金字塔①层不得留过期乐观表述）（2026-11-17）

**按「裁决取代后文档改写一致」三处就地重写，不留矛盾文本**：
1. **本文档①结论层**：原「反转核心承诺已部分兑现…每代总成本有界尚未成立」条**整条重写为 R210 判定**——只兑现「boot 索引走查有界」、未兑现「恢复成本随历史有界」，附配对实验数字（depth252 1.643/1.735/1.724、p10 全高于界、单调序列）、主因（`eachRecordOnce` 磁盘全表扫 38.7×，**HEAD 是内存 map 遍历⇒本候选引入的回归**）、次因非主因的副本实验证据、**「机器离散」归因的勘误（判据轴不对齐）**、G44 三交付与三禁。并标注上两条门禁数字为 R112/R115 历史时点读数，现态见 R208/R210。
2. **`tasks.md` 当前状态段**：新增「🔴 中心论点未达成」条置首，原「未闭合」条重写为 R208/R210 时点（dod 9 gap、product_surface 待接线、G34 暴露 20 夹具、G8 P1 红线、**assembly 功能性红已全闭仅余长跑**）。**旧「亚线性＋机器离散」表述已清（grep 0 命中）**。
3. **`tasks.md` 06/08 已勾叶凭据**：原「长跑锚 PASS（23.0s）」**附勘误 R210**（单次 `-count=1` 读数已废，配对实验确证增长为真）；**勾选保留**（三层去物化与退位删除本身凭据仍成立），但明写「长跑达标这一项不成立、中心论点未达成」——依「已勾叶被取代＝标勘误保持勾选非取消」纪律。
**复测**：doc_budget 0（棘轮未破）／doc_links 0／vocabulary 0／build 0。
**⇒ 派发唯一依据（①结论层）现与实测一致**，接续者读结论层即知中心论点未达成，不会被旧乐观表述误导。
**在飞六单**：G39／G40／G41／G42／G43／G44。**目标未达成，不得归档不得提交。零 git 写。**

### R212 G39 收口（判对契约方向：锚陈旧非产品缺陷）；🔴 G43 加 map 字段破坏 StorageConfig 可比较性＝级联缺陷（2026-11-17）

**✅ G39 收口，编排者采纳其判定与取证**：
- **判定②锚陈旧**（非产品缺陷）。**三条 spec 原文同指具名拒绝**：`runtime-inspection`「存储失败可辨」＝「WHEN 某源存储不可读 THEN 返回**具名错误与受影响维度**，而非空结果伪装正常」；`event-sourced-projection`「空结果伴随错误」＝「WHEN 首次扫描即错误且结果为空 THEN status=failed，不记录『空事实链正常恢复』」；`task-lifecycle.md#restore-rebuild`＝「声明缺失 ErrSpecMissing——**不猜替代、不静默回落**」。⇒ **子测试2 期望空 details 正是 spec 禁止的「损坏降级空视图」**。
- **三维扩散非缺陷（编排者原疑被否证）**：`readInvocations`/`readNotices` **恰从 `g.tasks`/`g.owed` 投影**（spec.InvocationID、obligations、outbox），Tasks 源败则两维**真实不完整**；Inventory 契约「refuses the whole view by name instead of answering a partial view as a normal one」与 spec「受影响维度」对齐 ⇒ `g.fail` 维度清单＝Tasks 源喂养的维度集合，**非过度扩散**。
- **处置**：重表达子测试2——fixture 补 `WriteRights:true` 并先 Close 再 Assemble（一根一写者，与子测试1 同形）；**Recovery 具名拒绝断言逐字保留**；Query 改断言 `err!=nil`＋`errors.Is` ErrSpecMissing/ErrSourceUnreadable＋`refusedDimensions` 含 invocation/task/notice＋Details 空。**零生产改动**。新锚 `TestInventoryTaskSourceRefusalNamesTheDimensionsItFeeds`（三维具名拒绝＋agent/session/lease 不受污染＋View 整拒）。
- **负锚双向**：删 `readTasks` 的 `g.fail`（降级静默空）⇒ 单元红「task refusal = <nil>」、集成红「refused dimensions = [attention]」；还原 md5 `f70efa37…` 与起点一致。**未把任何具名拒绝改为空返回。**
- 验证：build 0／vet 0／count1=count3=0（真 RUN 无 no-tests）／query 套件 0／assembly 两锚 0／comment_policy 0／package-layout 17==17／三 python 门 0。history 全套件 3 顶级红全在 `event_integrity_test.go` 且原文均「holds no write rights」⇒ **属 G42 迁移批，非本单**；起点 7→3 顶级，红集整体换因为同一根因。

**🔴 G43 级联缺陷（编排者亲测 build 发现，须 G43 处置或另派）**：
```
assembly/host_document.go:1648:5: invalid operation: *left != *right (struct containing map[string]"time".Duration cannot be compared)
assembly/run.go:3170:14: invalid operation: plan.storage != candidate (同上)
assembly/run.go:3188:23: invalid operation: plan.storage != top (同上)
```
**根因**：R193 裁定给 `StorageConfig` 加原文召回窗口字段（`policy → duration`，即 `map[string]time.Duration`），**map 字段使整个结构体不可比较**，打断三处既有 `!=` 比较（用于「配置未变」短路判定，与 G36 附带发现的 `publish.go:459 unchanged 短路` 同族）。
**⇒ 这是编排者派发令未预见的级联面**（「修复的级联面须预先写进派发令」教训的又一实例）。**候选处置（须 G43 或接续者裁，不得随意选）**：
- **甲**：窗口字段改用**可比较形态**（如排序后的 `[]struct{Policy string; Window time.Duration}` 切片仍不可比较；或字符串编码——**但那是双重编码，违「策略输出＝业务结构体、摒弃逻辑标记与双重编码」**）⇒ **须慎**。
- **乙**：三处比较改为**显式逐字段比较或 `reflect.DeepEqual`**——**但 `reflect.DeepEqual` 对 map 语义正确却失去编译期可比较性保证**，且可能掩盖「配置未变」判定的其它字段腐化。
- **丙**：**窗口注册不进 `StorageConfig`**，改由独立的可比较配置载体或构造期参数传入（**保持 StorageConfig 可比较**）⇒ 可能最纯，但偏离 R193 裁定的落点，**须编排者重议**。
**⚠️ 编排者不在此仓促裁**：三案各有纯度代价，且 G43 仍在飞可能自行处置。**须待 G43 战报看它如何处置，若它选了甲/乙须复核其纯度代价；若它停下报则按丙重议 R193 落点。**
**当前树 build 红属 G43 半改/级联态，非本编排者改动所致**（编排者本轮只改 openspec 文档与 doc_budget.json note）。

**在飞五单**：G40／G41／G42／G43／G44（G36／G37／G38／G39 已收口）。
**目标未达成，不得归档不得提交。零 git 写。**

### R213 最终交接账（取代 R208 的在飞表与缺口清单；预算尽，五单在飞）（2026-11-17）

**⚠️ 目标未达成。不得归档、不得提交、不得推送。全程零 git 写，HEAD 仍 `9547faca`。**
**🔴 当前树 build 红**（G43 级联：`StorageConfig` 加 map 字段破坏可比较性，三处 `!=` 断裂，见 R212）⇒ **接续者第一步是核 G43 是否已处置，未处置则按 R212 三案裁（编排者未仓促裁，各有纯度代价）**。

**① 最重要结论：中心论点被严格实验否证（R210，G36 配对法 21 对×3 run）**
- **只兑现「boot 索引走查有界」**（visits/valueReads 恒 4、valueBytes 252→268，9 run 一致）；**未兑现「恢复成本随历史有界」**。
- depth252 boot **1.643/1.735/1.724×**，p10 **1.443/1.473/1.452** 全高于 1.3 界，随深度单调（30→1.13／60→1.19／120→1.28／180→1.40–1.58）⇒ **增长为真非噪声**。
- **~78% 增量来自两条四量量不到的 O(历史) 读**：**主因＝`journal.go:1490 eachRecordOnce` 磁盘全表扫**（61.5µs→2383.8µs＝**38.7×**，零匹配反证随表容量增长；**HEAD 是内存 map 遍历 ⇒ 本候选引入的回归**；boot 链 `commit.go:221 configChain.loadLocked`→`tier_facts.go:35 Scan`）；次因＝`:615` 开档全 log CRC（**副本实验确证非主因**，R182 保留裁定不受影响）。
- **勘误**：先前「机器离散」归因错——`judgeFixtureDispersion` 量 total(boot+work) 而界逐 stage 判，boot 仅占 13–18% ⇒ **轴不对齐**，run1 真增长根本没被判。
- **G44 在修**（消全表扫／补 `integrityBytesScanned`＋`slotTableBytesScanned` 入计数面／判据四点校准；**禁调大 1.3× 界、禁删守卫、禁引第二真相**；若本征不可收窄须停下报）。

**② 已收口单（本轮 resume 后）**
| 单 | 交付 | 关键凭据 |
|---|---|---|
| G33 | 三陈旧锚重表达 | 探针确证 `obligations=0`⇒`settledAtCut` 真（R185 裁定成立）；日志断言改为 `durableLoadCountOf==0`（可观测非日志文本）；**assembly 功能性红 10→0** |
| G34 | 删无锁写面升级＋具名拒绝 | `ErrCommitWithoutWriteRights`；锁不变量恢复（本进程＋跨进程锚 PASS）；**暴露 8 包约 20 夹具须补写权**（反驳编排者「唯一消费者是其新锚」） |
| G35 | P4 反例重绑＋live 真跑 killed | 新 id `MUT-DOD-P4-window-unbounded`（选 B 行为性反例，visits [0,45,96,147,198] 确定性线性）；**dod 10→9**；**纠正「控制树须全绿」过严前置（实为按锚选择器）** |
| G36 | 中心论点诊断 | 见① |
| G37 | 八面懒水合 | 8 正锚＋8 负锚回退具名红 8/8；`Obligations` 隐式顺序依赖已破；**成本实测**（200 已结首次 456µs／5 scan×393216B≈1.97MB，命中 250ns）⇒ 编排者已补成本表声明，**判据⑤最后一条闭合** |
| G38 | F1 续审 | **合并覆盖 44%（808 hunk）**，审过面**夹带 0**、历史凭据未重写、**新门拒绝力全部亲跑成立**；查出 wiki §3.2 等三处文档假话＋跨变更级联 |
| G39 | 无 spec 链查询契约方向 | **判②锚陈旧**（三条 spec 原文均要求具名拒绝）；三维扩散**非**过度扩散（两维真从 Tasks 源投影）；重表达＋新锚＋双向负锚，**零生产改动** |

**③ 在飞五单（预算尽时可能未收，接续者须先核实际落盘再判完成，门禁数字须重测）**
| 单 | 写面 | 目标 | 收口须核 |
|---|---|---|---|
| G40 | `g2govtier/`（新包）＋governance `denial.go`＋锚 | **G8 P1 红线**（R201 四裁定：洞≠链终点／denial 走同形按身份判重／同批＋tier 锚／掩盖层已被 R61 拆故紧迫） | 20 PASS/0 FAIL（原 RED×10）；非空转守卫逐个实证；跨进程夹具补了还是如实声明仍注入构造；新包登记与 package-layout ≤17 |
| G41 | `scripts/gates/check_event_integrity_mutations.py`＋`golden/dod_predicates.json` | 九变异全量 live 真跑收 dod 余 8 gap | 逐变异判决表；带 `--mutation-evidence` 的 gap 归因；未 killed 者四类分辨（注册腐化／产品缺陷／假锚／在飞瞬态）；证据目录仓外＋CI bundle 形态一致性 |
| G42 | `runtime/state`＋6 integration 子套＋`event_integrity_test.go` | 8 包约 20 夹具补 `WriteRights: true` | **断言零改动逐 hunk 实证**；root 生命周期核（不得两写者同时活）；非本类红点名 |
| G43 | `memory/ledger` 五文件＋两 skeleton_assert＋两测试＋两 wiki | 窗口接线消 test-only 面（关 product_surface） | **🔴 map 字段破坏 StorageConfig 可比较性（R212）如何处置**；`BlobCount`/`References` 可达性（不得新造导出接口）；**不得声称「按策略过期」已达成**；wiki §3.2 三处假话是否一并修（G38 查出，原令只含 retention 一行） |
| G44 | `memory/ledger/journal.go`＋`journal_test.go`＋`tier_facts.go`＋`assembly/commit.go`＋两症状锚 | **中心论点主因**（消全表扫＋补两观测量＋判据校准） | 配对实验修复前后比值；`openBytes` 语义未改（`TestJournalOpenReadStatesTheWindowPastTheWatermark` 须仍绿）；**config fact 每 boot 追加是设计意图还是缺陷**的取证结论 |

**④ 剩余缺口（具名，依赖序）**
1. **中心论点**（G44）——**最高优先，未证则变更核心承诺不成立**
2. **G8 P1 红线**（G40）＋G10 tier 锚同批
3. **G43 级联 build 红**（R212 三案待裁）
4. **恒绿假锚** `symptom_anchor_recovery_test.go:465` `journalBytes==0`（G44 交付 2 应覆盖；若未覆盖须另派）
5. **`newestSessionCheckpointRef` 两步处置**（R196：先迁锚到生产路径再删，**不得直接删**；生产路径 `restoreSessionCheckpoints` 零直接测试覆盖须先补）
6. **`assembly/read.go:473→477` 隐式顺序是否可消**（G37 已破依赖，另派）
7. **wiki 三处文档假话**（G38：§3.2 旧三步写入／§十二缺口表 :259／契约表 :41，均引已删的 `Originals.Put`/facts.go:237；实际 `persistFact` 走 `CommitFact` 单批六键失败零键落地）＋`denial_ledger_anchor_test.go:49` 测试名与 invariant 相反＋`atomic_store_test.go reopenTier` 双 doc 首条已假
8. **两处轻锚病**（G38）：`TestAddressableIndexKeepsItsAddressIndependentOfTheKeyFamily` 探测链自模拟⇒生产改策略不红；`TestSecondProcessOnOneRootIsRefusedWriteRights` 声称具名而**不断言具名文案**
9. **`package-layout` 17**（HEAD 既有＋2 新增，跨多包须待写单收口）
10. **`proc-refs` 1**（属另一变更 `no-prefix-criterion` 未跟踪工作，**非本候选不夹带改**）；**反向级联**：该变更 allow 表仍列本候选已删的 `segment_store.go:coldIndexLocked`，**须立案告知**
11. **`tracked-hygiene` 13**（暂存态⇒**全量 lint 须排在暂存之后、提交之前**）
12. **godoc 重生成**（两文件，须待全部代码落地，编排者单写）
13. **🔴 F3 须重做**（R205：旧 run-notes §F3 的 S1 锚 `TestJournalBytesBoundedByLiveSet` 与 `RetireFacts`/`relocatableStorage` **全零命中**；S1 退役是正确的——它量磁盘字节而用户症状是成本，退位反转已接受磁盘增长；须按当前锚集＋当前可执行回滚手法重做，**且须待树稳定**因隔离副本复制半改态）
14. **F1 完整反夹带**（覆盖 44%，余 56% 面清单见 R209）
15. **R35 第二轮独立放行审**／**F2 全量门禁**／**F4 归档拒绝有效性**
16. **两条既有缺口归独立变更**：feedback 跨包孤儿（**转移项须立承接实体否则归档丢失**）；`journal.go:544` 长度域翻大被当 torn tail→**Truncate 静默切掉后续全历史**（安全级数据丢失，`:506` 同情形判 `ErrJournalCorrupt` 两函数相反，HEAD 同形；**保留开档全扫把暴露面扩到全历史，已声明**）
17. **`hygiene.checkWhitelistUnnamedSource` 退出码共径无独立自测**（G38 轻缺口）

**⑤ 纯度五判据现态**：①⚠️（治理身份面✅／**G8 P1 红线🔴**）②⚠️（索引走查✅／**boot 字节读🔴 中心论点未证**）③⚠️（双引擎已消✅／test-only 面待接线／死函数待两步）④✅（三端口收口、memory 分支删、退位删、无锁写面升级删等）⑤✅（兜底界＋开档完整性＋**懒水合成本均已声明**）
**⑥ 门禁现态**：build **红（G43 级联）**；concept/dead_producers/vocabulary/doc_budget/doc_links/durable_truth 0；dod **9 gap**；product_surface 1；comment_policy 0（G37/G39 各自清）；godoc 红两文件；proc-refs 1／tracked-hygiene 13／package-layout 17。
**⑦ 高并发纪律**：六至八单并发期**全量套件读数一律非权威**（已实证套件争用与瞬态红）；**复制工作树类实验（变异 runner／F3 隔离副本／归属对照）须排全部写单收口后**。
**冷启动读法（R214 置首，因它纠正了 R213 的两处判断并新增两个阻断）**：**R214（G41 收口：八变异 killed／dod 带证据 9→1／**P4 未被 G35 闭合**／**「控制树按锚选择器」只对单跑成立，`--all` 仍须全绿**／P7 结构性假反例／CI bundle 只收单份证据阻断 F2／concept_budget 因 StorageConfig 7 字段红）→ R213（在飞表与 17 项缺口，**其 G41 行已收口、dod 数字与 P4 归因以 R214 为准**）→ R210（中心论点否证）→ R212（G43 级联三案）→ R205（F3 须重做）→ R201（G8 洞语义）→ R196（死函数两步）→ R193（窗口接线）→ R185（陈旧锚）→ R182/R183（完整性校验与成本声明）→ R209（F1 覆盖与文档假话）**。
**⚠️ R204 那条「控制树口径勘误」须按 R214 限定为单跑口径**，`--all` 口径恢复「硬依赖全部控制绿」原判。

### R214 G41 收口：八变异 live killed、dod 带证据 9→1；🔴 纠正编排者两处判断＋P7 结构性假反例＋CI 形态阻断 F2（2026-11-17）

**✅ 交付**：`--list` 23 条全 enabled／0 pending（跑后复核未把任何 enabled 改 pending）。**九个 DoD 变异逐个 live 真跑（mode 全 live，无 --replay）**：P0/P1/P2/P3/P4/P5/P6/P8 **八条 killed**（`reason=named_assertion`、`assertion_hit=True`、`counted_killed=True`、`control_tree_green=True`、exit 1，`assertion_locations` 均单点命中）；**P7 `not_killed`／`test_green`**。
**`check_dod_coverage.py --mutation-evidence /tmp/dod-mutation-evidence-20261117-213335/authoritative` ⇒ 9 gap→1 gap**，减少的 8 条各由对应 live `counted_killed` 记录闭合，余 1 为 P7。**证据目录仓外**（102 文件 1.5M，含九个逐变异 `evidence.json`＋control／mutations）。自测 dod 39/39、mutations 54/54 均 0。

**🔴 纠正编排者判断一：P4 未被 G35 闭合**。起点 9 gap **含 P4**——G35 闭的是「not registered」那条，**「no run records」那条仍在**，其 kill 不在门默认读取的任何证据里。**R204/R208/R213 写「dod 10→9＝G35 闭 P4 两条」须勘误为「闭注册一条，真跑记录一条由 G41 闭」。**

**🔴 纠正编排者判断二（关键，R204 的勘误本身过头了）**：「控制树绿按锚选择器、故 assembly/history 余红不阻真跑」**只对 `--mutation` 单跑成立**。**`run_gate:2807-2808` 对本次全部控制取 all-green 才跑变异** ⇒ **`--all` 下编排者原判「硬依赖控制树全绿」是对的**，G35 的纠正**不可外推到 `--all`**。实测 `--all` EXIT=1、**零变异被跑**：19 个控制中 `TestEventIntegrityConsumptionRecovery`（5 子测红，91.7s）与 `TestEventIntegrityPersistence`（`storage_failure_not_reported_as_success`）红 ⇒ `control_tree_green=False`、`mutations: []`。**⇒ R204 那条勘误须限定为「单跑口径」，`--all` 口径恢复原判。**（另：本次 assembly 长跑墙钟控制**绿**18.0s，「余 1 红」未复现。）

**🔴 P7＝结构性假反例（G41 按令停下报，编排者立案须裁）**：两次 live 均 `test_green`，`apply_patch` 强制唯一命中故补丁确已落。根因：**`foldedProcessKeys`（`symptom_anchor_midband_test.go:61-63`）只返回已离开投影的键，而 `presentedProjectionContent` 只对投影内条目调用** ⇒ **被变异的代码路径对锚所检的每个键都不执行**。票据实由**第二生产者 `foldTicketLineOf`（`memory/replay/age_segments.go:699`）**写进折叠产物自身行，另有 `TestSegmentFoldCarriesTheTicketOfEveryFoldedEntry` 独立钉住。**该反例在结构上不可能杀死其锚**，非产品缺陷、非文本漂移、非瞬态。**G41 未自行改绑（改绑属语义重定向，超出「文本腐化」授权）——正确。**
**⇒ 编排者待裁**：P7 须**重指到真生产者 `foldTicketLineOf` 的反例**，或**重表达锚使其量被变异的那条路径**。**不得删谓词、不得把 enabled 改 pending 消红、不得手填 killed。** 另须查「两个票据生产者」是否 itself 是重复结构（判据④）。

**✅ P1 补丁修正（G41 自决，编排者追认）**：原 `new=""` 删掉字节比较块后 `existing` 与 `bytes` 双双失去唯一使用方 ⇒ **`compile_failure`，该补丁从未编译通过**（查 HEAD 证 `existing` 当时亦单使用），**非上游漂移非瞬态**。改为把拒绝换成 `return &segmentEventPlan{duplicate: true}, nil`（保留比较、只废拒绝，逐字对齐注册描述「Answer other bytes … as the duplicate it holds」），复跑 killed、命中原文即注册文本。**锚真、产品无缺陷**。**追认理由**：属「补丁不可编译」的注册缺陷修正，非削弱判据，且附亲跑原文与 HEAD 对照。

**🔴 CI 形态不一致（阻断 F2 声称门绿，须另派）**：`event-integrity-mutations` job 用 `--all --json --out`，产出**单份** `evidence.json`；而 `check_runtime_evidence.py --bundle` 在 `:2773` **硬判 `len(mutations) != 1`**。实测喂九份 ⇒ `bundle must hold exactly one mutation evidence file, found 9`。**dod 门 rglob 收多份、bundle 门只收一份，二者消费形态不同**；且九份报告带**六个不同 source_digest**（在飞改动所致），`check_mutation_baseline` 亦判混合基线。⇒ **本地产物可闭 dod 8 gap，但不能令 CI `event-integrity-required` 绿**；唯一 CI 兼容产物是单份 `--all`，而它当前因两条非 DoD history 控制红而零变异。**⇒ F2 须待：①两条 history 控制转绿（G42 面）②树可编译（G43 级联）③单份 `--all` 真跑成功 ④P7 重指。**

**🔴 新增门红（G43 级联第二处）**：`check_concept_budget.py` **EXIT=1**——`StorageConfig` **7 导出字段 > 基线 6**（新增 `WriteRights`/`RetentionWindows`，git diff 证为在飞新增）。**与 R212 的可比较性断裂同源**：G43 加字段既破可比较性又破 S2 形状棘轮。**须按名抬升协议报编排者裁，不得自行抬基线。**

**R26 实证**：`git status` 283→289 行、**GONE 0 条**，NEW 6 条全属在飞写单；零 git 写、index 未动、证据全在 /tmp。
**在飞四单**：G40／G42／G43／G44（G41 收口）。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R215 G43 级联三处亲收＋两道门按名归正；G40 收口；🔴 撤销 R175 裁定五的新顶层包改道；🔴 package-layout 17 红全属本 PR（2026-11-17）

**✅ 编排者亲收（G43 停在 assembly 禁面，留树 build-red＝不安全停放，故亲收）**：
- `assembly/host_document.go:1648` `*left != *right` → `!reflect.DeepEqual(*left, *right)`。**该文件相邻行本就用 `reflect.DeepEqual(opened.Engine(), next.Engine())`** ⇒ 不是引入新形态而是与既有形态对齐，编排者先前的纯度疑虑消解。
- `assembly/run.go:3171`／`:3189` 同改（import 补 `reflect`）。**`:3180 *plan.engine != engine` 不改**——比的是另一个无 map 的结构，`!=` 仍合法。
- **`go build ./...` EXIT=0**（修前为红）；`go vet ./memory/ledger/ ./assembly/` EXIT=0。

**✅ 两道门按名归正（非放宽）**：
- `check_concept_budget.py:232` **按名抬升 6→7**，理由随条登记：`RetentionWindows` 是原文召回窗口的**生产注册入口**（`config.go:291 OpenTier → newKVOriginalStore(store, cfg.RetentionWindows)`，grep 实证生产路径存活），取代已删的测试专用 `WithRetentionWindow`；窗口与 Type/Root 同生命周期，另立配置即造第二份存储定位。**门输出逐字列出七格具名**（`baselined at 7`），非静默扩张。
- `product_surface_baseline.json` **删陈旧项 `WithRetentionWindow`**（全仓 `.go` 零命中；`NewMemoryOriginalStore`／`MemoryOriginalStore`／`MemoryOption`／`factOriginalsOf` 亦全零 ⇒ R193 裁定 (B) 的删除面已零残留），note 追加撤回理由。
- 实测：concept **EXIT=0**、product_surface **EXIT=0（0 error 0 warning，37 冻结钩子）**、config_liveness **0**、schema_allowlist **0**；自测 concept **58 OK**、surface **9 OK**。

**✅ 纯度亲收：删 `RetentionWindows` 的 json/yaml 标签**。取证：`ledger.StorageConfig` **全仓无解析器**（生产构造点全是 Go 字面量：`config.go`／`migrate.go`／`assembly/run.go:3166,3188`／`host.go:651`／两 scripts／两 examples），**yaml 面是 `doc.Storage`**，由 `assembly/host.go:651` 翻译成 `ledger.StorageConfig` ⇒ 标签**结构上永不可达**，且六个同族字段一律无标签。派发令原写「形态与 LifecycleConfig 的标签风格一致」，**实测 LifecycleConfig 同样无解析器**——类比的对象自身就是装饰，故编排者勘误自己的令。删后 build/vet/concept/liveness/schema 全 0。wiki `memory-architecture.md:254` 已由 G43 如实写明「**装配面未注册窗口**」，与本裁定一致。

**✅ G40 收口（G8 P1 红线已修）**：`features/evolution/governance/denial.go` 本地 `errGovernanceEventConflict`（冻结包边禁引 memory，循 `errApprovalFactConflict`／`errHoldFactConflict` 先例）＋`Record→writeGovernanceEvent` 按身份判重＋新 `resolveContinuationTail`（扫最高已提交 seq，洞由既有 `governanceHistoryUnreadable`／`errWindowSequenceMissing` 具名）。三锚落在既有 `denial_ledger_anchor_test.go`；负锚 md5 复原 `b7f81b4c6d66a6e836db40359d1aa7b8`；真实 localfile tier 探针 `/tmp/g2tierprobe` PASS×3。**如实声明：跨进程夹具仍未建，仍是注入构造。** G40 另反驳 design:213 措辞「洞确不存在」与洞夹具矛盾（须勘误该句），并自修毫秒时间戳致 flake（2/10→0/12×10）。

**🔴 撤销 R175 裁定五的「新顶层包 `g2govtier/`」改道（编排者自纠，取证在案）**：G40 报「落新包必在 golden unknown-package 与 scripts mirror 两面添红故未落」。**核码后两条前提都不成立**：
- `golden/runtime/architecture.json` `scope.exclude_dir_names` **含 `tests`** ⇒ `tests/` 下任何包**不进架构注册面**，无需新登记边。
- `scripts/codetools/package_layout.go:41` `mirrorExemptTrees` **含 `tests/integration/`** ⇒ 该树**整树免镜像**，无需新豁免项。
- **既有先例**：`tests/integration/capabilities/governance_recovery_test.go` **同时 import `features/evolution/governance` 与 `memory/ledger`**（逐文件核）。
⇒ **裁定：两枚 tier 锚（G2-G6/G2-G8）落 `tests/integration/capabilities/`，取消新顶层包**。这不反转裁定五的实质（与 governance 包分离、只用导出面——governance 冻结包边禁引 memory 才是分离的真理由），只把落点从顶层挪进已免登记的集成树，**新增特异化结构为零**（判据④）。`g2govtier/` 不再建。

**🔴 新立案：`package-layout` 门 17 条红，逐条归属证实全属本 PR，F2 硬阻断**：
- 权威跑法 `go run ./scripts/codetools package-layout . examples/wechat-bot examples/cli-host`（`lint.sh:36 POLICY_DIRS`）⇒ **EXIT=1，17 条 `test-mirror-missing`**；`lint.sh:267-270` 是 **hard exit 1**，非 warn-only。
- **归属逐条实证**：10 条文件在 HEAD 已存在且其生产同名文件在 HEAD **从来不存在**（`git cat-file -e` 逐条），故 HEAD 即红；但 `origin/main` **逐条全无**这 10 个文件 ⇒ **全部由 PR #39 早先提交引入，无一来自上游** ⇒ **不夹带原则不适用，17 条全在本变更范围内，F2 须清完**。
- 7 条为在飞新增：`assembly/fact_commit_atomic_test.go`／`assembly/seat_tail_test.go`／`assembly/symptom_anchor_longrun_test.go`／`governance/approval_channel_anchor_test.go`／`governance/denial_ledger_anchor_test.go`／`memory/ledger/fact_commit_batch_test.go`／`runtime/task/task_slice_receipt_anchor_test.go`。
- **G40 报的「mirror 17→18」须勘误其含义**：17 是**本门当前 finding 数**，不是 `mirrorExemptFiles` 条目数（后者实测 **23** 条）。G40 的结论（落新顶层包会 +1）方向对，但落点已由上条裁定改道，该 +1 不再发生。
- **处置口径**：优先**改名镜像其所守的生产文件**；只有主题确实跨文件成一面（`mirrorExemptFiles` 注释的判据）才具名登记豁免，且须写明为何改名会说错主题。**不得整树加豁免、不得放宽 exact-match 判据**。

**在飞**：G42／G44（G40／G41／G43 收口）。**待派**：mirror 17 清偿（须先算与 G42/G44 触碰集）、P7 重指真生产者 `foldTicketLineOf`、CI 单份 `--all` 形态、`newestSessionCheckpointRef` 两步、wiki 三处文档假话、F3 重做。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R216 G42 收口（24 夹具红全修、零断言改动）；`--all` 18/19 控制绿，唯一红是 G44 在飞半成品；裁定 `Recovery()` 空报告＝契约正确、夹具期望陈旧（2026-11-17）

**✅ G42 收口**：实测 24 条顶层红（state 1／host 4／caps 5／exec 3／acc 2／cfg 1／insp 5／hist 3），逐条核因均为 `ErrCommitWithoutWriteRights`；governance「goal events=0」与 adapter「recovery=[]」曾疑为独立缺陷，**ERROR 日志实证为其下游症状**。16 文件补 `WriteRights: true`，**git diff 逐 hunk 核：仅 WriteRights 行／Close 顺序／助手签名与 doc，断言零改动**。同 root 重开一律先关旧写者，无两写者并活。自决三处（`recOpenLog`/`wakeAccountLog` 改返回 tier、`adapterRig` 增 tier 字段）只为暴露 Close 柄、调用方皆同文件内、无新抽象层——**编排者追认**。
**互证**：`^TestEventIntegrity` **363s→16s**；编排者独立复跑两条 CI 阻断控制 `TestEventIntegrityConsumptionRecovery`／`TestEventIntegrityPersistence` ⇒ **EXIT=0，15 PASS／0 FAIL**，`-v` 逐子测具名跑到（**非零匹配空转**，两函数定义在 `event_integrity_test.go:75`／`:3826`）。⇒ **R214 所列 F2 前置①②已满足**。

**✅ `--all` CI 形态真跑（`python3 scripts/gates/check_event_integrity_mutations.py --all --json --out /tmp/g45-mut-all`）**：**EXIT=1**，`control_tree_green=False`、`mutations=[]`。**19 控制中 18 green／1 red**，唯一红是 `check_dead_producers.py` 在**副本树**上判：`memory/ledger/IntegrityBytesScanned` 与 `SlotTableBytesScanned` **has no consumer**（`journal.go`）。编排者在主树同门 **EXIT=0（3454 函数全有消费者）** ⇒ 差量是 **G44 在飞刚写下这两个量、消费者尚未接线**。**判定：在飞瞬态，非缺陷。不得代删这两个函数消红**（它们正是 R210 派 G44 要补的两个被测量）。**⇒ F2 前置③只待 G44 接线完成。**

**🔴 裁定：`Recovery()` 对 cut 前已结任务答空＝契约正确，夹具期望陈旧（不是产品缺陷）**。争议点：`tests/integration/execution/new_tool_adapter_test.go:133` 期望 `len(report.Tasks)==1`，实测 `recovery report = []`；host `RuntimeVisibility` 同症状。
**取证（按契约原文切裁定面，不把已裁部分判成缺陷）**：
- `assembly/run.go:1198-1205` 现行契约原文：「**A task that finished before the cut states no outcome here — the checkpoint that cut carries names none of them, so the boot never enumerated them** — and the whole history behind the report is the read a caller asks for through the task account's own enumeration, **which is linear in that history**」。
- **自证风险已查**：HEAD 该注释只写「one outcome per task the chains covered」，**排除句是本变更新写** ⇒ 不能仅凭注释定案，故上溯 spec。
- **spec 权威**：`changes/05-task-custody/specs/session-checkpoint-material/spec.md:5,7` —— boot SHALL 经切片恢复任务账户、**而非全史重读任务事实族**；「任务事件链 SHALL 仍是语义真相与回放源」；`checkpoint-state/spec.md:13` ——「任何有界化 SHALL NOT 删除、覆盖或改写已提交事实，**按身份读全史的消费者 SHALL 仍能取回正文**」。
- ⇒ 令 `Recovery()` 枚举 cut 前已结任务＝令 boot 全史重读，**直接违反 spec 的有界恢复条款**，亦违判据⑤（本征 O(历史) 成本须归索取者）。**产品行为正确。**
**处置（派单执行，不得自行放宽）**：把 `:133` 改为**钉住空报告**（「cut 前已结清且无欠账 ⇒ 报告不点名它」），并**保留并强化链读腿**：`:143` `listObjects(ObjectTask)` 仍答该已结任务、`:140` adapter 0 次重跑、notice 全 Retired。**这比原断言更强**——同时钉住「恢复有界」与「链按身份可读」两条 spec 条款。host `RuntimeVisibility` 同口径。**注释须引 spec 条款名，不得只以新注释自证。**
**G42 停下报的判断正确**：它识别出「修它须改断言」超出其「零断言改动」纪律，故停线请裁——编排者予以肯定并在此裁定。

**在飞**：G44（写 `memory/ledger/journal.go` 等）／G45（只读 mirror-17 改名方案）／G46（只读 P7 票据生产者诊断）。**已收口**：G40／G41／G42／G43。**待派**：Recovery 期望归正（exec＋host，触碰集与在飞不相交）、mirror 17 清偿（待 G45 方案）、host Attention×3＋SoakContinuity（closed view 类，另因）。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R217 wiki 三处文档假话亲收（体量棘轮同批压回）；host 全量红逐条取原文并分四类，第二/三类裁定已出（2026-11-17）

**✅ 编排者亲收 wiki 三处假话（G38 立案，`docs/wiki/memory/memory-architecture.md`）**：
- `:41` 硬契约 3 与 `:103` §3.2 第 2 步均引 **`tier.Originals.Put`（`assembly/facts.go:237`）**——**该调用点已不存在**。实测生产路径：`assembly/facts.go:393` `ledger.CommitFact(ctx, tier, FactCommit{Record, Ref, Body})`，`memory/ledger/segment_store.go:765` **一次批同写原文族与事件族**。§3.2 表按真实五步重写（预检／锁内定序／同批提交＋outcome 分派／无提交后向量步骤），结论②由「第2步成功第3步失败留孤儿」改为**事实路径不存在该中间态**（引 `segment_store.go:756-762` 契约原文）。
- §十二 缺口行「段提交失败可能留下孤儿原文」改为如实：**事实路径已无此缺口；不经 `CommitFact` 的原文写入点仍无此保证**（不夸大为全仓已无孤儿）。
- 硬契约 8 的保障机制补「事实两族同批（`ledger.CommitFact`）」。
- **体量棘轮**：首轮 `check_doc_budget` **EXIT=1（22150 > 22149，+1）**。**未抬基线**，同批把「提交」与「outcome 分派」并成一个语义单元（本就是一次提交及其结果）压回 ⇒ **EXIT=0**。`check_doc_links` **EXIT=0**。

**✅ host 全量真跑取原文（`go test ./tests/integration/host/ -count=1`，EXIT=1，197.0s）**，红分四类：
| 类 | 红项与原文 | 裁定 |
|---|---|---|
| A | `TestRuntimeVisibility/重启连续` `runtime_visibility_test.go:442` `recovery outcomes = [], want the one task the persisted chain covers` | **同 R216 裁定**：产品正确、夹具期望陈旧。已派 G47。 |
| B | `TestAttentionClearsNoticeOnWorkEnd:159`／`TestAttentionNoticeQuestionClearsOnWorkEnd:225`／`TestSummaryAttentionClearsWhenRead`／`TestSoakContinuity/常驻 soak:109`：`memory: read through a closed view`（invocation/task/notice 三维全拒） | **新裁定见下** |
| C | `TestSoakContinuity/崩溃恢复:209` `recovered tasks = [6 个], want the 32 tasks durable before the crash` | **同 A 同源**：kill -9 后只有上次 cut 仍欠义务者进报告；32 条已结清者不被枚举。**按 A 同口径**：钉住「报告＝欠账集」＋「32 条按身份经查询面仍可读」。 |
| D | `TestSoakContinuity/常驻 soak:121` `resident child exit = 1`／`/热更回滚:241` `hot child exit = 1` | **未裁定**：子进程退出 1，日志被截断未见真错。**疑为 B 的下游**（子进程在 Close 后读→具名拒→exit 1），须取子进程完整 stderr 后判。 |

**🔴 裁定（B 类）：`ErrReadAfterClose` 拒绝是正确且正是本变更要的安全性质，夹具在已关闭视图上读＝夹具缺陷。**
**取证**：`memory/ledger/config.go:172-175` 定义 `ErrReadAfterClose`；`journal_kv.go:75/109/124/286/312` 在读与枚举两类入口一致拒绝。**`git grep HEAD` 证实该错误在 HEAD 完全不存在** ⇒ 本变更新增。**其本意**：关闭后的视图若把读答成「无此键」，与「store 本来就空」**不可区分**——那正是本变更要消灭的静默丢失形状。故拒绝不是回归，是把一个静默错答换成具名失败。
**生产路径核查**：`Run.Close` 是终态，其后无任何生产读路径（真实操作方是再 boot 一个 host）；夹具 `host.Close(ctx)` 后 `host.Run().Query(...)` 是**测试独有形状**。
**处置口径（派单执行）**：观测「工作结束后」的状态须在 drain 之后**另开一个同根读视图**再读（G42 已在 `seedEvalCorpus` 用过同形，编排者已追认）；**不得**让 Close 保留半开视图（写者已 drained 而读者仍活＝第二状态，违判据④），**不得**放宽 `ErrReadAfterClose`。若某断言其实只关心 drain **之前**的状态，则把读移到 Close 之前——按断言语义选，不一律重开。
**G47 派发时 B 类尚未裁定，令其停线报**——本条即其停线所待的裁定，须以后续单交付（G47 在飞不可中途改令）。

**在飞**：G44／G45／G46／G47。**已收口**：G40／G41／G42／G43。**待派**：B 类四夹具重开读视图（待 G47 收口释放 host 写面）、C 类按 A 口径、D 类取子进程 stderr 后判、mirror 17 清偿（待 G45）、P7 重指（待 G46）。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R218 G46 收口：两票据生产者非重复端口（不合口）；P7 取甲案重指真生产者并派 G48；🔴 勘误 R214/R215 的锚路径与机理；comment_policy 5 红全在 G44 在飞面（2026-11-17）

**✅ G46 问一结论：不是重复端口，是两个语义单元，不得合口。**
- `presentedProjectionContent`（`assembly/request.go:1361-1377`）生产调用方**仅 1 处** `request.go:1166`（`projectedSegment` 循环）← `projectedMainContext:1127` ← `assemble:830`。职责＝**仍在投影内**的条目转模型消息时前置地址，不写回投影。
- `foldTicketLineOf`（`memory/replay/age_segments.go:699-705`）生产调用方**仅 1 处** `:553`（`foldProcessDetail`）← `FoldByAge:502`／`PlanByAge`（`age_plan.go:109`）。职责＝过程原文**离开投影**时把地址写进派生产物行（材料面，参与计量）。
- 签名虽同为 `ProjectionEntry→string`，**实现集合不同**（前者＝标签＋`[evt_` 幂等守卫＋整条正文前置；后者＝provenance＋票据＋`type: summary` 单行，无票据回落 `foldLineOf`）。**领域边界＝「原文是否还在投影里」**，来自需求本身而非代码形态 ⇒ 判据④不触发合口。
- 票据形式唯一生产者是 `TicketOf`（`memory/replay/replay_budget.go:334`），另有 `noticeCard:211`／`tool/subagent/handoff.go:172` 两消费者，各属通知卡与委派 refs。

**🔴 勘误 R214/R215 两处（G46 取证，编排者照抄了 G41 的错）**：
1. **锚路径错**：中段症状锚实在 **`tests/integration/history/symptom_anchor_midband_test.go`**（注册表 `:1045`/`:1065` 引用它）；**`assembly/symptom_anchor_midband_test.go` 不存在**。`foldedProcessKeys` 在 `:54-67`。R214/R215 凡写 `assembly/symptom_anchor_midband_test.go` 处均按此勘误。
2. **机理不准**：不是「被变异路径对锚所检每键都不执行」。该函数**对产物条目也执行**，只是产物键为合成键、`TicketOf` 返空（`replay_budget.go:335`），原码在 `if ticket == ""` 早返输出 `content`，**与变异体逐字节等价**；差异只落在保留的事实键条目（02/03 轮），而这些键按定义被排除。**正确表述：变异体在锚所检票据的载体上输出等价。** 结论（结构性假反例）不变。

**✅ 裁定：P7 取甲案——只改变异目标，锚／`expect_assertion`／site 全不动。已派 G48。**
新 target `memory/replay/age_segments.go`；old＝`foldTicketLineOf` 内 `ticket := TicketOf(...)` 至 `return provenancePrefixOf(entry) + ticket + " " + entry.Type + ": " + entry.Summary` 整块；new＝`return foldLineOf(entry)`。**必红链路**：产物行失票据 → `carried` 不含 `[evt_00-1-plan|thinking_plan]` → 锚 `:206` → **`:207` `t.Fatalf("the request carries no ticket %q for the folded entry %s ...")`**，site 实算 `(symptom_anchor_midband_test.go, 207)`。**不取乙案**（改锚去量投影内票据）：那是呈现面要求、非 P7「中段被折过程」的量，乙即绑代理量（注册 `reason` 明禁），且 `no-prefix-criterion/01` 域已计划把该注入删成透传。

**⚠️ G46 附带发现（代码读证，未实跑，立案不夹带）**：`presentWithin`（`replay_budget.go:268-271`）降级视图尾部已带票据，呈现面再前置一张 ⇒ **一条消息两张票据**；`request_test.go:986` 的期望同式自算故恒绿（**假绿形状**）。`no-prefix-criterion/01/design.md:9` 已立案 ⇒ **归该变更，不在本变更修**（when：该变更执行时；why：呈现面单一拥有者是其裁定面）。

**🔴 comment_policy 真跑 EXIT=1，5 findings 全在一个文件**：`assembly/symptom_anchor_recovery_test.go` —— `free-standing` 0→1（`:926`）＋`test-doc-line-too-long` 0→4（`:487` `TestBootQuantityProbesReadTheRealBoot` 等）。该文件正是 **G44 在飞写面**（R210 派它补 boot 被测量探针）。**判定：在飞注释形状债，待 G44 收口后按其自查或另派收口单清偿；不得抬基线。**
**跑法勘误（编排者自己踩）**：裸 `go run ./scripts/comment_policy` 用扫描集 `[.]`，与基线的 `[. examples/cli-host examples/wechat-bot]` 不符 ⇒ 报「scan set differs」而**非真 finding**。按 `lint.sh` 口径补三目录后才得真读数。**同名门两 scope 陷阱再实证。**
**另核**：`check_vocabulary_alignment` **EXIT=0**（825 go 文件／83 api 文档／0 违规）；`check_dead_producers` 主树 **EXIT=0**（3454 函数全有消费者，G42 报的 1 条为在飞瞬态）。

**在飞**：G44／G45／G47／G48。**已收口**：G40／G41／G42／G43／G46。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R219 G48 收口：P7 已 live killed（第九条闭合）；F3 锚集今日重定位实证——旧三锚两机制全零命中，F3 硬依赖 G44（2026-11-17）

**✅ G48 收口（P7 重指甲案落地并真跑确证）**：
- 注册改动**仅 P7 一条**，其余 22 条零改动（`select_mutations` 23/23 enabled）。target `assembly/request.go` → `memory/replay/age_segments.go`；old＝`foldTicketLineOf` 内 `ticket := TicketOf(...)` 至 `return provenancePrefixOf(entry) + ticket + " " + entry.Type + ": " + entry.Summary` 整块（**python 计数 occurrences=1，行 700**，门 `apply_patch` 强制唯一命中为二证）；new＝`return foldLineOf(entry)`。description 全文重写为新目标所量之物，旧描述删净。`expect_assertion`／`expect_assertion_site`／`status=enabled` 未动。
- **live 真跑逐字段**：`mode=live`／`reason=named_assertion`／`assertion_hit=True`／`counted_killed=True`／`control_tree_green=True`／`verdict=killed`／变异 `exit_code=1`／`assertion_locations=['symptom_anchor_midband_test.go:207']`（单点）／`gate.ok=true`。失败原文与 G46 推演**逐字一致**：`the request carries no ticket "[evt_00-1-plan|thinking_plan]" for the folded entry 00-1-plan (thinking_plan: "plan 00")`。
- **自测零改动**：改前改后均 `Ran 54 tests OK`；grep 证测试文件无断言钉旧注册；`validate_registry==[]`、`assertion_site_problem(P7)==''`。
- **dod 读数如实报未闭**：只含 P7 一条证据时 `--mutation-evidence /tmp/g48-p7-evidence` ⇒ **8 gap**（P0–P6/P8 报 no run records），无证据基线 9 gap。**九条各自都已 live killed，但合并证据须一次 `--all` 真跑**——G48 未据此声称 DoD 已闭，正确。
- ⇒ **九条 DoD 变异全部已被真跑杀死**（八条由 G41、第九条由 G48）。**F2 的变异面只待一次全控制绿的 `--all`**。

**⏸ `--all` 暂不跑（编排者裁定，非流程债）**：按「**复制工作树类实验须排全部写单收口后**」——变异 runner 复制整树，G44／G47 仍在写，此刻读数非权威。**待 G44＋G47 收口后一次跑成**，同时满足 CI 单份形态与 dod 合并证据。**when**：两单收口即刻；**why**：避免用半成品树的读数冒充验收凭据。

**🔴 F3 锚集今日重定位（R205 结论复核仍成立，且已取到新落点）**：
- **旧凭据全失效**：`TestJournalBytesBoundedByLiveSet`（S1）全仓 **零命中**；`RetireFacts` 生产面 **零命中**；`relocatableStorage` **零命中**。⇒ 旧 run-notes 的三锚两机制证据**不得计入完成**。
- **今日在树的症状锚落点（grep 实证）**：`assembly/symptom_anchor_recovery_test.go:827 TestSymptomAnchorBootCostFlatAcrossGenerations`（S2 后继）／`assembly/symptom_anchor_longrun_test.go:488 TestSymptomAnchorPerGenerationCostFlatOverALongRun`／`assembly/checkpoint_test.go:1237 TestSymptomAnchorSessionBootCostFlatAcrossGenerations`／`tests/integration/history/symptom_anchor_midband_test.go:141 TestSymptomAnchorMidbandProcessRecalledByTicket`（S3 后继）／`memory/replay/age_segments_test.go:693,1280,1428`（折叠 provenance／压缩有界／压缩收敛）。
- **🔴 F3 硬依赖 G44**：R210 已否证中心论点——boot 成本随历史增长（depth252 比值 1.64×，越 1.3 界），而现有锚只量四个量、量不到那两条 O(历史) 读。**G44 正在改被测量面本身**（`IntegrityBytesScanned`／`SlotTableBytesScanned` 已建，消费者目前只在 `memory/ledger/journal_test.go:2778-2851`，assembly 侧探针在写）。**在锚的量测面变更期间做回滚实验＝拿正在变的尺子量**，故 F3 必须待 G44 收口。
- **⚠️ 门绿≠已接线**：`check_dead_producers` 此刻 EXIT=0，但两个新量的消费者**目前只有测试**。按「死代码门测试算消费者」，门绿不证明它们已进 boot 锚的计量面。**G44 的实质交付是配对实验证明 boot 有界，不是门转绿。**

**在飞**：G44／G45／G47。**已收口**：G40／G41／G42／G43／G46／G48。**待派（均待写面释放）**：`--all` 单份真跑、F3 按新锚集重做、B/C/D 类 host 夹具、mirror 17 清偿（待 G45）、comment_policy 5 红（G44 面）。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R220 G45 收口（17 条逐条可判定）；编排者亲收 3 条具名豁免 17→14；派 G49 收四个无在飞写者的包；跨变更散文级联立案（2026-11-17）

**✅ G45 交付：17 条全部可判定，无缺证据项。14 甲（改名/合并/拆分）＋3 乙（具名豁免）。**
**✅ 编排者亲收 3 条豁免（`scripts/codetools/package_layout.go`，无在飞写者）**，理由随条登记在该块既有注释形状内：
- `assembly/symptom_anchor_recovery_test.go`：一次 boot 的四个真量＋完整性/槽表/索引走查，跨 run／checkpoint／facts／commit／settle_turn／host_delivery 与 ledger 引擎；先例 `hotupdate_anchor_test.go`。
- `assembly/symptom_anchor_longrun_test.go`：一整代的成本面，跨同上一组文件＋durable load 账。
- `runtime/task/task_slice_receipt_anchor_test.go`：`task.StateSettleAccount` × `state.DurableCommit.Seat` 的跨包接缝；**`state` 导入 `task`（`durable.go:18`），故 internal `package task` 的测试不能再导入 `state`**，锚必须留 external `task_test`，而该目录唯一词干 `checkpoint.go` 已被 internal `checkpoint_test.go` 占用 ⇒ **取不到可镜像的名**；先例 `genregistry_rebuild_test.go`。
**实测**：layout finding **17→14**；`test_check_package_layout.sh` **OK（5 夹具，负例仍 exit 非零 ⇒ 拒绝力未削弱）**；`go test ./scripts/codetools/` **ok 6.9s**。
**⚠️ 陷阱立案（G45 提示，编排者采纳）**：`_real` 后缀虽可过镜像，但其**冻结语义是 real-API 变体**，用它承载 external 包变体属 laundering ⇒ **禁用**。

**✅ 裁定：`gov/approval_channel_anchor_test.go` 取甲（并入 `checkpoint_test.go`）**。G45 自报此条依据最弱（靠同目录先例而非单一词干独占主题）。编排者按其**领域事实**改判依据：六例**皆 mutate/restore `StateSeat`**（`checkpoint.go:105/130`），approval book 与 `Policy.Judge` 是**驱动**不是**主题** ⇒ 主题是 seat 的 checkpoint 行为，`checkpoint.go` 正确。**不靠「同目录有相似文件」这种代码形态立论**（元规则三）。

**✅ 派 G49（批一，四个包 6 条）**：`api/config/`（拆二：`decode_test.go`＋新建 `defaults_test.go`）／`features/evolution/governance/`（approval 并入 `checkpoint_test.go`；denial 改名 `denial_test.go`）／`memory/replay/`（history_summary_budget 并入 `age_segments_test.go`；ticket_production 拆入 `age_segments_test.go`＋`replay_budget_test.go`）／`runtime/state/`（durable_seat 并入 `durable_test.go`）。**核心验收＝用例数守恒**（搬运前后逐包 `func Test` 计数相等）＋每个搬过的函数在新文件里单跑具名跑到。
**⏸ 批二待 G44**：`assembly/` 7 条＋`memory/ledger/fact_commit_batch_test.go` 1 条——其**构建闭包**含 G44 正在写的 `journal.go`／两枚症状锚，按 R21 触碰集须待其收口。**两枚症状锚取豁免保持原名 ⇒ 与 G44 写面零冲突、变异门零级联**，这是取乙而非甲的关键收益。

**🔴 改名级联面（G45 全仓 grep 取证，须同批清偿）**：
- **唯一机械锚**：`scripts/gates/check_event_integrity_mutations.py`，5 文件×3 字段（`evidence_ref`／`test_file`／`expect_assertion_site`）共 15 行。其中 4 条改名/合并 ⇒ **必改 12 行**：plugin_mount 866/896/900、fact_commit_batch 909/930/934（批二）；durable_seat 974/996/1000、history_summary_budget 1078/1097/1101（**批一，已派**）。recovery 1009/1032/1036 **不动**（已豁免）。
- `.github/`／`golden/`／`docs/`／`CHANGELOG.md`／全部 `.go` 源码：**零路径引用**（grep 实证）。CI 按 `-run` 函数名选锚，改名不断锚。
- **散文锚（静默脱节）**：`doc-refs` 门只扫 `docs/wiki docs/*.md`（`lint.sh:242`），**不扫 openspec** ⇒ 散文引用改错不会自动报红，须人工同批回写。本变更 ORCHESTRATION.md **62 行**＋各叶 design/tasks/proposal/spec；按词干计 recovery 65／plugin_mount 16／durable_seat 15／longrun 14／fact_commit_batch 13／fact_commit_atomic 8／approval·denial·presented_ticket 各 7／ticket_production 6／task_slice 5／task_custody 4／seat_tail 2／summary_budget·history_summary_budget 各 1／replay_ref 0。**账目由编排者单写（R17），域文档随批回写。**
- **🔴 跨变更级联（立案告知，不夹带修）**：`openspec/changes/no-prefix-criterion/changes/01-presentation-single-owner/` **6 行**引用 `ticket_production`／`presented_ticket`。该变更**活跃非归档**，其 design 引用的路径将因本变更改名而变假。**裁定：这 6 行属本变更改名的级联面，须同批只改路径字符串、不动其任何裁定文本**（依据「一处机制改动会让多份文档同时变假须按旧调用点聚类扫」）；批二执行时一并处理，并在本账登记。

**在飞**：G44／G47／G49。**已收口**：G40／G41／G42／G43／G45／G46／G48。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R221 G44 收口（容量回归已消除，但中心论点仍不成立）；G49 收口（layout 14→8，用例 112→112 守恒）；改名映射表立案；派 G50 校验时机分叉／G51 批二（2026-11-17）

**✅ G44 收口——本候选自造的回归已消除，中心论点仍不成立（诚实表述，编排者采纳为其正式结论）**：
- **机制**：`loadAddressedLocked` 走 CommitOrder newest-first **止于首个链键** → seq N → **点读 1..N**；洞＝**具名拒绝**（不静默截断）；空链走查全 order（缺席证明，本征，已声明）。`TierFacts` 加 `Get`＋`CommitOrder()(CommitOrder,bool)` 能力 accessor，`configChain` 断言可选接口（先例 `runCommit.Declare`、`checkpoint.go:881`）。**无常驻结构、无第二真相。**
- **决定性读数是计数面不是墙钟**：深根 boot **槽表扫 6,291,520B 级 → 0**；integrity ≈8.6MB **不变**。墙钟配对（21 对×3 轮奇偶换序）修前中位 1.641/1.651/1.492 vs 修后 1.823/1.541/1.352 —— **本机噪声下重叠**（p90 达 1.7–2.2、尖峰 11×），**故墙钟不构成「已修好」的证据，计数面才是**。
- **两个新量已钉住事实**：`integrityBytesScanned` 钉**线性**（逐代 == journalLogBytes 1:1；引擎锚 200→2000 提交＝30400→304000B，window 恒 0）；`slotTableBytesScanned` 钉**零**（首 seated 代起；点读 +0、零命中扫 == capacity×stride、两容量两读数）。负锚红原文：`the boot of generation 1 enumerated 196608 slot table byte(s), want none: …a full-table scan pays the capacity of every family the log stores whatever the chain holds`（变异 overlay，`diff -q=0`）。
- **守卫已改 boot 轴**（R210 指出的轴不对齐已修）：21 对换序配对中位、p10–p90 显式噪声带、**计数面先判**；**1.3× 界未动、守卫未删**，另加「偏置中位 1.5」具名拒绝控制实证拒力。
- **🔴 可声称／不可声称（正式口径）**：**能**——本候选引入的容量驱动回归已消除（boot 槽表枚举=0，负锚具名）；索引回放窗口/走查/账户载有界；本征成本可量且钉线性。**不能**——**恢复成本随历史有界**：开档全 log CRC ＋链载本征 O(历史) 仍在，boot 墙钟深度 252 仍增 ~1.4–1.7×。
- **config fact +1/boot 已定性**：是 `Kind=recordpublication` 记录——assemble 默认 Publish、新 publisher `effective.Generation==0` 故 `publish.go:459` 短路不触发；**生产 Host boot 走 `reconcileBootConfig` 采纳一致链、零追加**（设计意图）。夹具「boot-only」实为 boot+publish，配对每对各 +1，差值保留使比值略压缩。**未改，如实声明。**
- **追认三处自决**：accessor 定义于 `journal.go`（`journal_kv.go` 不在其精确白名单）；守卫统计按 G36 草案②改中位（首轮 p90 门 1/3 跑被噪声拒，control p90 1.405；**改统计口径而非放宽界，且另加偏置控制实证拒力 ⇒ 不是改锚迁就回归**）；`journalLogBytesOf` 未建根答 0、stat 移至 boot 前（测量点修正，未削弱断言）。
- **🔴 G44 附带发现（新立案，须另派）**：Host boot 的 **`migrateRetiredDeliveries`（`assembly/host_delivery.go:2165`）是另一条 O(capacity) 全表扫**，在其白名单外未改，**新计数器已使其可见**。与刚修的那条同类 ⇒ 属本变更范围。

**✅ G49 收口（批一 6 条）**：`api/config`（拆二）／`governance`（approval 并入 checkpoint_test.go、denial 改名 denial_test.go）／`memory/replay`（history_summary_budget 并入 age_segments_test.go、ticket_production 拆入 age_segments_test.go＋replay_budget_test.go）／`runtime/state`（durable_seat 并入 durable_test.go）。
**用例数守恒实证**：api/config 19→19、governance 25→25、memory/replay 36→36、runtime/state 32→32，**合计 112→112**。锚定单跑全 PASS（config 2/2、governance 12/12、replay 4/4、state 10/10）。**零碰撞**（诊断预言成立）。
**门级联 6 处按字符串定位改毕**（非行号盲改；`evidence_ref` 跨行拼接那处亦正确命中），残留 grep=0；自测 **54 OK 且未改任何断言**。四包 `-count=1` 全 ok；`go build`／`go vet` 0；gofmt 7 文件全空。
**layout 14→8**，剩余 8 条＝assembly 7＋`memory/ledger/fact_commit_batch_test.go`（**已派 G51**）。

**✅ 改名映射表（ canonical，历史条目按此解析，不改写审计记录）**：
| 旧路径 | 新落点 |
|---|---|
| `api/config/summary_budget_test.go` | 声明例→`api/config/decode_test.go`；缺省例→`api/config/defaults_test.go` |
| `features/evolution/governance/approval_channel_anchor_test.go` | `features/evolution/governance/checkpoint_test.go` |
| `features/evolution/governance/denial_ledger_anchor_test.go` | `features/evolution/governance/denial_test.go` |
| `memory/replay/history_summary_budget_test.go` | `memory/replay/age_segments_test.go` |
| `memory/replay/ticket_production_test.go` | 前两例＋`ticketsIn`→`age_segments_test.go`；`TestTicketTextIsTheRecallTicketOfTheEntry`→`replay_budget_test.go` |
| `runtime/state/durable_seat_test.go` | `runtime/state/durable_test.go` |
| assembly 7 条＋`memory/ledger/fact_commit_batch_test.go` | **批二已落（G51，见 R224）**：`fact_commit_atomic`→`facts_test.go`；`plugin_mount` 拆二→`plugin_catalog_test.go`（8 例）＋`host_document_test.go`（1 例）；`presented_ticket`→`request_test.go`；`replay_ref`→`run_test.go`；`seat_tail`→`checkpoint_test.go`；`summary_budget` 拆三→`request_test.go`(1)＋`summary_gist_test.go`(3)＋`compress_consumer_test.go`(1)；`task_custody_anchor`→`checkpoint_test.go`；`memory/ledger/fact_commit_batch`→`segment_store_test.go` |
**历史审计条目（「本轮新增」「落位理由」「源树绑定」「偏离计划」等）一律只按本表解析，不回改**；**前瞻性白名单/计划已改路径**：`changes/04-midband-ticket-recall/tasks.md:8` 白名单、`no-prefix-criterion/changes/01-presentation-single-owner/{tasks.md:8,design.md:37,design.md:54,proposal.md:26}`（**跨变更级联，只改路径与函数名、不动其任何裁定文本**；`ticket_production_test.go:155` 的尾部票据断言实为 `TestTicketTextIsTheRecallTicketOfTheEntry`，已核 HEAD 原文，故改写为函数名而非猜行号）。

**✅ 派 G50（只读）：校验时机真分叉取证**。CRC 现为论点唯一剩余障碍，而 **R182 保留它的裁定作出时 CRC 尚未被量化为剩余主项** ⇒ 前提已变，须重议而非只勘误。令其取五证：R182 原文理由的准确性质（「boot 时须发现全部损坏」vs「水位之下也须校验」，二者对「移到读时」的反对力度完全不同）／全部读路径枚举＋**是否存在不读帧就下结论的路径**（安全性判决点）／boot CRC 字节量推演与成本转移对象／**第三条路**（只校验水位之上窗口＋存活账点读帧）／链载 O(N) 的 N 究竟是存活任务数还是历史任务数（**不得把 O(N) 一律当缺陷**）。
**✅ 派 G51（批二 8 条）**：assembly 7＋memory/ledger 1；**禁写 `scripts/`**（其 6 行门级联由编排者在 G49 收口后单写，避免共享面双写）。

**在飞**：G47／G50／G51。**已收口**：G40／G41／G42／G43／G44／G45／G46／G48／G49。**目标未达成——中心论点仍不成立，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R222 G47 收口（A 类 2/2 绿＋四组真负锚）；closed view 根因经反证确证；派 G52 执行 B/C 类；编排者亲收 G49 的两处注释级联（2026-11-17）

**✅ G47 收口（A 类）**：
- `tests/integration/execution/new_tool_adapter_test.go:138` 改为钉**空报告**，文案 `want no outcome for the task settled before the cut, while the chain still answers it by identity: the boot seats the account from the checkpoint slice instead of re-reading the task fact families`；`:146` 链读腿**加钉 exact TaskID**（taskID 由 cut 前一次 `listObjects` 取得）⇒ 比原断言更强。子测标题与头注同步为如实描述。
- `tests/integration/host/runtime_visibility_test.go:441`/`:457` 同口径，新增 `restored.Details[0].State.TaskStatus != task.StatusSettled`。**`TestRuntimeVisibility` 已转绿，连跑 3 次绿**；exec 连跑 5 次绿。
- **四组真负锚（`-overlay` 挂 /tmp 补丁，主树零写）**：A 令 `Recovery()` 凭空点名已结任务 ⇒ exec:139＋vis:442 双红；B 令按身份读返回漂移 TaskID ⇒ exec:146＋vis:455 红；**C 令 `closeStorage()` 返回 nil ⇒ 三条 attention 全绿、soak 只剩崩溃腿红**；D 令读面答错 status ⇒ vis:458 红。**md5 逐一等于开工基线**（`assembly/run.go` 7ce323e1、`assembly/read.go` 13349e52、`attention_lifecycle_test.go` 47d21d27、`soak_continuity_test.go` 6e1b1b14），`diff` 无输出。

**✅ closed view 根因确证（负锚 C 是决定性反证，非静态推断）**：本分支新增 `assembly/run.go:931 closeStorage() → r.tier.Close()`（**HEAD 无此调用**），Close 后 `Run.Query` 读已结束视图被 `ErrReadAfterClose`（`memory/ledger/journal_kv.go:75`）具名拒绝。**与 A 类不同源**，R217 的 B 类裁定成立。
**⚠️ 一个不成立的类比（G47 提出，编排者驳回并说明理由）**：`Run.Diagnostics` 在 Close 后可读（既有绿）似为先例，但它读的是**进程内 run 自身状态**，`Run.Query` 读的是 **durable 存储** ⇒ **先例不可移植**，不得据此要求产品保留存储读面。
**✅ C 类同源确证**：G47 手工复现 crash 子进程→kill -9→reader，得 `recovery=6` 而**同一读者按身份读 `read=32`、与 ready 32 逐 id 相等** ⇒ 按 A 类同口径改（钉「报告＝欠账集」＋「32 条按身份全可读」两条腿），**不得把 32 改成 6 了事**。**已派 G52**（白名单 `attention_lifecycle_test.go`／`attention_projection_test.go`／`soak_continuity_test.go`；B 类按断言语义选「drain 后另开只读视图」或「读移到 Close 前」，先例 `seedEvalCorpus`）。

**✅ 编排者亲收 G49 的两处注释级联**：`api/config/decode_test.go` 与 `memory/replay/replay_budget_test.go` 各一处 `free-standing`（`契约:` 行与函数 doc 之间空行断开）。按该仓既有形状并入同一 doc slot（doc 在前、`契约:` 收尾）。
**门教义实证（编排者第一次改错，门纠正）**：把原文件级散文段一并搬进 doc slot 后触发 **`test-doc-not-one-sentence`**——门要求测试头注**只能是一句意图＋可选 bullet＋索引**，散文须移入断言消息或 wiki。核该段所述事实已由 bullet 与 `age_segments_test.go` 的 `TestSegmentFoldCarriesTheTicketOfEveryFoldedEntry` 承载、且「唯一生产者」已在断言文案 `one form, written by one producer` 内 ⇒ **删冗余段，无事实丢失**。复跑该锚 `-v` **PASS**。
**🔴 编排者流程缺陷自认**：G49／G51 派发令写了注释形状规则，但**验证面未列 `comment_policy` 实跑** ⇒ 搬运类单每收一单长出注释红。后续搬运/合并类单的验证面**必须含** `go run ./scripts/comment_policy . examples/cli-host examples/wechat-bot`（三目录）。
**当前 comment_policy 3 红全在 G51 在飞面**：`assembly/request_test.go:1493` free-standing、`assembly/plugin_mount_test.go:1` responsibility-fragmentation（该文件正被拆二，尚未删）⇒ **在飞瞬态，待 G51 收口后编排者复核并亲收残留**。

**在飞**：G50／G51／G52。**已收口**：G40–G49 中的 G40／G41／G42／G43／G44／G45／G46／G47／G48／G49。**目标未达成——中心论点仍不成立（CRC 时机分叉待 G50 取证），不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R223 🔴 G50 推翻编排者重开 CRC 的提议（墓碑帧反证）；真正增长源＝配置链载 O(历史 boot)；G44 的「已消除」须收窄——成本搬到计量 scope 外（2026-11-17）

**🔴 编排者自纠：R221 提出「CRC 时机前提已变须重议」——该提议被 G50 以代码证据推翻，CRC 留在开档，R182 裁定维持且理由更强。**
**决定性反证（三条不读帧就下结论的路径）**：`lookupOnce`（`memory/ledger/journal.go:1432` 空槽／`:1438` `journalSlotAbsent`／`:1441` `!stated`）答 absent；`eachHeld:1696` 按 state 过滤；`eachNewestFirst:1846-1848` 把 Deletes 记 seen 后跳过 ⇒ **墓碑帧永不被任何值读触及**。移到读时后，损坏的删除帧无人读、无具名拒绝，键答成「无此键」——**正是本变更要消灭的静默丢失形状**。**今日开档全扫是这些 O(1) 索引答案的唯一背书。**
**R182 三条理由的反对力度经分解不均**（G50 取证，编排者采纳）：事实①「`addr.value` 无 CRC」是现状陈述，只否掉「读时校验已足够」，不反对补校验；事实②「翻末字节落在一切 value span 之外」只反对**值粒度**，不反对帧粒度读时校验；**事实③「冻结契约面要求开档即拒」是唯一真正反对移时机者**，该面今日仍在：`journal.go:4-9` 包 doc、`docs/wiki/memory/memory-architecture.md:162`、`golden/runtime/durable_truth.json:128`、`journal_test.go:119-144`、`tests/integration/history/storage_capabilities_test.go:239`。**水位论本身逻辑上支持读时校验（读时即「此刻」），不构成反对。**
**第三条路（窗口＋存活账点读帧，其余留读时）亦不成立**：absence 由最新陈述帧背书，而 `segment_store.go:664-670` 每事件恒 4 键（evtKey+idxKey 写一次不改、metaKey+seatKey 重写）⇒「每键最新陈述帧」集合 ≈ 全部事件帧 ≈ 全日志 ⇒ **要安全须扫近乎全量，要省就漏墓碑**。

**🔴 真正的增长源已定位：配置链载 O(历史 boot)，不是 CRC。**
`loadAddressedLocked`（约 `journal.go:288`）`for at:=1; at<seq` 做 **N-1 次 Get 点读**，**N＝配置链事实数＝历史 boot 数**（非存活任务数）。增长链（G50 读证）：`run.go:521` bindings 每次新建 → `:636 NewPublisher`（`publish.go:307` 明载 effective 零）→ `:664 Publish` **先于** `:679/:686` 一切 restore → `publish.go:458` unchanged 短路要求 `p.effective.Generation != 0`，boot 恒 0 ⇒ 每 boot 走 `commitRecord:468`→`CommitPublication`→`chain.append`；链只追加无压实。
**而生产只用 `latest(kind)` 取最新一条**，全量 `snapshot` **仅 `assembly/commit_test.go:128` 消费** ⇒ **boot 在载没人读的历史**。
**对照（不得把 O(N) 一律当缺陷）**：`runtime/state/durable.go Seat:451` 遍历 `wire.Tasks`（检查点只携开放项）＋`:464` young 尾段 ⇒ **那个 N 是存活账，符合论点，不是缺陷**；`d.load:268` 五前缀全扫是已声明兜底。

**🔴 G44「容量回归已消除」须收窄（第十三种锚病实证：新增成本路径落在计量 scope 外使旧锚空转）**：
槽表扫 6,291,520B→0 **属实**（65536×96+64），但**替代物是 N 次点读**，而**点读按设计不计 `slotScanBytes`**（`journal.go:927-928`），`valueReads`（`:924`）**无生产访问器**（仅 `journal_test.go:821/1088`）⇒ **五个 counted face 一个都量不到它**。**O(历史) 从「字节」搬到「次数」，对锚隐形。** ⇒ R221 记的「本候选引入的容量驱动回归已消除」**须收窄为「槽表字节枚举已消除；等量的历史依赖以点读次数形式仍在，且今日不可量」**。
**另勘误行号漂移**（G50 核）：全扫 `:615`→**`:623`**、`addr.value` `:1654/1660`→**`:1669/1675`**、`:544`→**`:552`**、`:506`→**`:514`**。
**另勘误 CRC 的量级归因**：本账 `:3973/:4037` 已实测 **CRC 只削 ~0.17 比值、非主因**；G44 后它是剩余最大**字节**项，**是否为最大墙钟项未经分离实测** ⇒ 不得声称「CRC 是剩余主因」。

**✅ 派 G53（只读＋计时探针）收两个未决点**：
- **问一（事实冲突）**：G44 称「生产 Host boot 走 `reconcileBootConfig`、零追加」，G50 称「每 boot 必追加」。**两者可能都对、只是入口不同（`Assemble` vs `Boot`）**。令其读全两入口＋**实测 K=1,2,5,10 连续 boot 的链事实数与点读次数**，并指出**症状锚用的是哪个入口**——**若锚走生产不走的入口，那是夹具形状缺陷须立案**（夹具不得驱动生产设计）。
- **问二（设计裁定取证）**：配置链 fold 是否「按 kind 最后一条胜出」？只读每 kind 最后一条是否与全链应用逐字节等价（**注意某 kind 可能只在第 1 条陈述过，从尾往前最坏仍 O(N)**）？是否受「单一派生路径」spec 条款约束？**配置链为何没走既有 checkpoint 机制——设计遗漏还是明文排除？**
- **问三**：点读次数如何以**最小改动**进计量面（优先并入既有 face，不为单一用途新增结构），且**必须被哪个锚的哪条断言消费才算真接线**（测试消费只让 dead_producers 绿，不证明接线）。
- **问四**：G50 未实证的疑似既有缺口——`orderStatementOf:1262` 写纯 JSON 行**无 CRC**、`:1843` 只 Unmarshal，**开档全扫也不覆盖 order 文件**；翻 `span.Offset` 会具名拒绝、静默错读还是 panic？

**在飞**：G51／G52／G53。**已收口**：G40–G50 中的十单。**目标未达成——中心论点仍不成立，真正增长源刚定位待裁，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R224 G51 收口：package-layout 与 comment_policy 双双归绿（两道硬门清除）；门 6 行亲收；前瞻性白名单四处回写（2026-11-17）

**✅ G51 收口（批二 8 条）**：8 源文件 → **28 例＋15 helper**，块级按字节切搬未手打一字。`summary_gist_test.go` 实为已存在（475 行，镜像 `summary_gist.go`）故是并入而非新建——**勘误 R220 方案里「新建」的措辞**。
**用例数守恒**：`assembly` **246→246**、`memory/ledger` **73→73**。逐目标：facts 17→20、plugin_catalog 新建 8、host_document 新建 1、request 16→20、run 15→16、checkpoint 28→32、summary_gist 9→12、compress_consumer 1→2、segment_store 18→21。
**28 个搬运函数逐个单跑**：`-run '^名$' -count=1 -v` **28/28 RUN=1 PASS=1 FAIL=0**（零匹配空转零发生）。
**逐字性**：与 HEAD 比 26/27 块逐字相同；1 处差在 `WriteRights: true` 与 `tier.Close()`，**是本分支在 HEAD 之后自己改的**（搬的是工作树字节，未改断言）；3 个 HEAD 无版本的文件由「按字节切块＋守恒＋单跑全绿」担保。

**✅ 两道硬门归绿（编排者按权威跑法复测，非采信战报）**：
- `go run ./scripts/codetools package-layout . examples/wechat-bot examples/cli-host` ⇒ **EXIT=0**，`813 file(s); names, skeleton pins and test mirrors hold`。**17→14→8→0 全程闭合。**
- `go run ./scripts/comment_policy . examples/cli-host examples/wechat-bot` ⇒ **EXIT=0**，`0 finding(s); 0 beyond baseline`。**G51 用的是 `-no-baseline assembly memory`（另一 scope），编排者按基线三目录重跑才是权威读数**——同名门两 scope 陷阱第三次实证。
- `go build ./...` 0；`go vet ./assembly ./memory/ledger` 0；`go test ./memory/ledger -count=1` **ok 66.2s**；`go test ./assembly -count=1` **ok 257.2s，0 FAIL**。

**✅ 门 6 行编排者亲收（G51 禁写 scripts/，共享面单写）**：`check_event_integrity_mutations.py` 的 `evidence_ref`／`test_file`／`expect_assertion_site` 三字段×2 组 → `assembly/plugin_catalog_test.go#TestBuiltinCatalogMountsEveryFeature` 与 `memory/ledger/segment_store_test.go#TestFactCommitAnswersNewExistingConflictUncertain`。**先 grep 核实两函数确在新文件各 1 处**再改。**13 个旧文件名在该门残留 grep=0**；自测 **54 OK**。

**✅ 前瞻性白名单四处回写（历史条目按 R221 映射表解析，不回改）**：`changes/02-plugin-dynamic-mount/tasks.md:5`（02.R1 未勾）→`plugin_catalog_test.go`；`changes/05-task-custody/tasks.md:219`（05.R3 未勾）→`durable_test.go`＋`assembly/checkpoint_test.go`；`changes/11-atomic-fact-commit/tasks.md:54` 与 `design.md:249` 的**待派补锚**指向→`assembly/facts_test.go`。**核实 `tests/integration/host/presented_ticket_test.go` 确实存在**（与 assembly 同名文件不同路径），故 04.R2 白名单无需改。

**✅ 追认 G51 三处自决（注释槽位）**：`presented_ticket_test.go` 文件头契约行落到首例 index 槽、3 行散文丢弃（其主张已由三例各自 doc 承载）；`fact_commit_batch_test.go` 的 `#overview` 头改为 `factProbeEngine` doc 首行；`facts/replay_ref/task_custody` 三处重复契约行丢弃。**代价如实登记**：`#context-management` 的代码侧引用现仅存于该 index 行（一处引用即足够，doc 侧锚不孤）。

**⚠️ 保留的不确定性（G51 如实报，编排者立案）**：中间一轮 `TestSymptomAnchorPerGenerationCostFlatOverALongRun`（`symptom_anchor_longrun_test.go:504`）曾红，最终轮 0 FAIL、单跑 PASS(35.3s)。该文件 mtime 在搬运前后完全相同 ⇒ 非内容改动。**但搬运改变了症状锚之前的用例执行序列，理论上可影响其进程态，同树 A/B 无法排除（禁 git 回滚）** ⇒ **立案为待验项：长跑锚须 `-count=3` 复跑确认非顺序敏感**，纳入 F3／CI 面。

**⚠️ 未清项（编排者登记）**：8 个删除与 2 个新文件**未入索引**（`D` 未暂存／`??`），`tracked-hygiene` 须在暂存后才可能绿 ⇒ **全量 lint 必须排在暂存之后、提交之前**。gofmt 仅 `assembly/host_delivery_test.go` 一处（mtime 21:02 早于本单，非 G51 所改）。

**在飞**：G52／G53。**已收口**：G40–G51 中的十二单。**目标未达成——中心论点仍不成立（配置链 O(历史 boot) 待 G53 取证后裁），不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R225 🔴 G53 实测定位真因：boot 配置采纳有两条派生路径（Assemble 每 boot +1／Boot 恒 3）；裁定在源头统一；🔴 order.log 无完整性背书＝本变更自造安全缺陷（2026-11-17）

**✅ G53 实测（/tmp 探针，localfile 新根，`valueReads` 经 `journal.go:924` 计数器读）**：
| 入口 | K=1/2/5/10 配置链事实数 N | valueReads |
|---|---|---|
| `assembly.Assemble`（`run.go:492`） | 1,2,5,10（**每 boot +1**） | 2i−1（第 10 boot=19） |
| `assembly.Boot`（`host.go:465`） | **恒 3**（首 boot 落 decl+record+result） | 首 boot 3，其后**恒 6** |
- Assemble：无 reconcile → `run.go:664 Publish` → `publish.go:458` 短路要求 `Generation!=0` 而 `NewPublisher` 恒 0（`:307`）⇒ 必走 `:468`→`:566 CommitPublication`→`commit.go:61 append`。
- Boot：`host.go:718` 设 `bootReconcile` → `run.go:660` 分支 → `host_document.go:1051`，指纹＋记录一致（`:1092-1094`）→ adopt → `publish.go:367 Adopt`「no record is committed」⇒ **零追加，根本不进 Publish**。
- `bootReconcile` 是 **Option**（`run.go:1360-1374`），只有 Host 入口设它。
- **2N−1 的构成**：链载（N−1 Get ＋1 walk 值读，`commit.go:288`）＋**空根 checkpoint 搜索 walk 再读 N 值**（walk 绕过 resident，`journal.go:2116→1874`）；**有根夹具后者止于 cut，只前者增长**。

**🔴 勘误三处（G53 核，编排者照抄过错）**：
1. **G50「N＝历史 boot 数」仅 Assemble 成立**，Boot 恒 3（实测）；其增长链对 Assemble 准确但**外推一切 boot 错**。**G44 关于两入口的说法准确**，R223 记的「事实冲突」就此结案：两人各说一个入口。
2. **`loadAddressedLocked` 在 `assembly/commit.go:264/288`，不是 `journal.go:288`**——G50 与编排者派发令均误文件。
3. G50 漏了空根第二 O(N) 项（checkpoint 搜索 walk），有根夹具该项有界。

**🔴 入口错配立案（夹具形状缺陷，非行为伪造）**：**两枚症状锚都走 Assemble**（`noTrimChainRun`，`anchored_boot_parity_test.go:289`；recovery:364、longrun:94）。**Assemble 不是「生产不走」**——`examples/cli-host/main.go:161` 生产用它；但 `examples/wechat-bot/main.go:36` 走 Boot、N 恒 3、**无 O(boot 历史) 项**。⇒ **锚忠实于 cli-host 形状，不能代表 Host Boot**；中心论点若针对 Host boot，须按此收窄表述。

**✅ 裁定：在源头统一 boot 配置采纳，而不是优化载法。已派 G54。**
**理由（纯粹判据推导）**：「boot 时哪份配置生效」今日有**两条派生路径**——Host reconcile/adopt、Assemble 盲 publish。同一逻辑操作产生不同耐久结果＝重复机制（判据④更少特异化代码、判据①单一真相）。**把 reconcile 提为 boot 默认行为而非可选钩子**，则 +1/boot 在源头消失、N 恒定、链载自然有界。
**不取「只读每 kind 末帧」**：G53 已证 fold 确是「从尾按 kind 最后一条胜出、事实自包含无增量语义」（`commit.go:353`），生产消费者全用 `latest`（`commit.go:592/620/627/680`、`host_reload.go:237/378/425`、`host_document.go:1059/1063/1179`），`snapshot` 仅 `commit_test.go:128` ⇒ 末帧法**对生产消费者逐字节等价**，但仍留两处缺口：**(a) 缺席证明**——Assemble 从不写 declaration/result，某 kind 可能仅在首帧 ⇒ 从尾走到头最坏仍 O(N) order 行；**(b) 洞拒绝**（`commit.go:294`）对未读旧帧失背书。**故它是优化不是消除，降为备选。**
**spec 面已核不撞**：「单一派生路径」条款（`session-checkpoint-material/spec.md:7`）**限定任务事件链**，配置链属 config-liveness/config-hot-reload 不受覆盖；`config-liveness` spec:9 只要求从链上事实可回读。**未找到「配置链不进 checkpoint」的明文裁定**；结构原因是切片为事件链投影（`run.go:554`）而配置事实是独立 kv 族、先于 restore 载（`:617` vs `:679`）。checkpoint 路可行但须早读或重排 reconcile 且洞检测面缩减须声明 ⇒ **列为第三备选，非本轮取路**。
**成本声明缺口立案**：R221 只声明「空链走查本征」，**非空 N 点读未入成本表＝未声明**（判据⑤）。

**✅ 第六个计量面裁定：导出既有 `valueReads`，不新建结构。** G53 逐个核过五 face **皆不可并入**：`slotScan` doc `:927` **明文排除点读**且钉零、`integrity` 钉 1:1、`walk` 比例被不点名 entry 的点读打破、`journalBytes` 开档固定、`indexEntries` 钉零。最小＝把**既有** `valueReads`（`journal.go:924/1673`，`journal_test.go:821/1088` 已消费）导出为 accessor（形状照 `:2236`），入 `bootQuantities`＋`longRunCountedFaces`（约 `:298`）。**真接线＝`longrun:528 judgeCountedFaces`（约 `:311`）平坦界＋`recovery:882-896` 新断言二者都消费它**；修前该面在 Assemble 夹具是 2N−1 ⇒ 必红。**dead_producers 因测试算消费者会绿，门绿不证明接线。**

**🔴 新立案（安全级，本变更自造）：`order.log` 无完整性背书，walk 与点读同键异答。**
**范围判定已实证**：`git show HEAD:memory/ledger/journal.go` 中 `order.log`/`orderStatementOf` **零命中** ⇒ **order.log 是本变更 06 域可寻址索引新增的载体**，故不是既有缺口，**属本变更必修**。
**G53 实测（/tmp 探针，等长 token offset 互换／+1，行长不变）**：两变体 **open 均 OK**（integrity 981B 只覆盖 journal.log，order.log 189B **不在面**）；点读均正确（slot 不受影响）；**互换 ⇒ walk 静默错读**（keyA=BBBB／keyB=AAAA），boot OK；**+1 ⇒ walk 具名 `ErrJournalCorrupt`**（`journal.go:1679` 解析败），boot 拒于链载而非开档；无 panic。
⇒ **order 损坏开档不拒；轻则静默错读，且 walk 与点读对同一键给不同答案**——正是本变更要消灭的形状（第二真相无背书）。**待另派**（与 G54 共享 `journal.go`，须待其收口后派，避免共享面双写）。

**在飞**：G52／G54。**已收口**：G40–G53 中的十三单。**目标未达成——中心论点的关键路径单刚派，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R226 G52 收口（host 145 测仅 1 红）；最后一腿由 spec 原文直接裁定「未排空 MUST NOT 清除」，派 G55（2026-11-17）

**✅ G52 收口**：B 类三处均取「另开同根视图再读」（断言观测的都是工作结束后的状态）：`attention_lifecycle_test.go:154/:226` 经新 helper `reopenDrainedHost`（`attention_projection_test.go:24`）；`soak_continuity_test.go:376/488/522`三条子进程腿经 `soakTasksAfterClose`（`:626`，同根再开一 runtime、读毕 Close 交还 root，依据 `run.go:921` closeStorage 契约「根交还后第二个 runtime 方可进入」）。**子进程代码全在该文件内，属白名单，无需停线。**
**C 类两腿（`:208-223`）**：欠账腿 `len(owed)==0 || len(owed)>=len(durable)` 并逐 id 用 `soakNamesID` 反查防编造；可读腿 `!sameSoakIDs(read, durable)` 钉 32 条按身份全可读。**32 未改成 6。**
**验证**：build/vet 0；`go test ./tests/integration/host/ -count=1` **145 测仅 1 红**（即停线腿）；两条 attention 与 soak 各单跑×3 全 PASS（非 flaky）；comment_policy 三目录 **0 finding**。
**负锚三组（`-overlay`，主树零写，md5 前后 diff 空）**：①`Recovery()` 追加全 32 ⇒ `:211 recovered tasks = 38 of the 32 … only what the cut still owed` 红；②读面只报欠账 6 ⇒ `:223 tasks read after the restart = [6…] … readable by identity [32…]` 红；③`closeStorage→nil` ⇒ summary 腿 ok，反证其红唯一成因是关闭视图。
**✅ 代理自纠一处（编排者肯定）**：②的首个变异「丢前 4 条」被**前置守卫 `:171` 拦下、非主题红**，故重做为「只报欠账 6」。**这正是「前置守卫红≠主题红」的正确处置**。

**✅ 最后一腿裁定：spec 原文直接给出，无需推导。已派 G55。**
**红**：`TestSummaryAttentionClearsWhenRead` 第三腿（约 `:452`）在 `host.Close` 后读，期望无人读的 summary 在场数 0。
**编排者亲验（不抄战报）**：读该测试全文，其**自身头注第三条 bullet** 写的是「once the host closes over a **drained** work」，而 G52 实测 `CloseReport = Outcome:cut Drained:false`——**那条无人读的 reflection 投递本身就是未偿义务，排空不可能完成** ⇒ **夹具自述前提不自洽**。清除门控在 `assembly/host_attention.go:97` 是 `ended && drained`。
**spec 权威原文（`openspec/specs/attention-clearance/spec.md:44-49`，编排者亲读）**：「宿主 `Close` 排空成功后，attention 读面 SHALL 整体退出在场（全有或全无：排空未完成则一条不清）；**排空失败或未排空 MUST NOT 清除**…MUST NOT 为推导在场补造 question/clearance 假对…一次工作结束后的清空是**该次读数**：重启是新工作，仍无 answer 的 question 恢复在场」。同 spec 另有具名 Scenario **「排空失败不清」**。
⇒ **该腿断言与 spec 的 MUST NOT 直接相反＝夹具错误，产品「未排空不清除」正确。** 取 G52 提的 (A) 方向但**收窄为具体形状**：断言「仍在场」**并显式钉住 `CloseReport.Drained == false`**（不钉前提则前提漂了断言仍绿），头注 bullet 改为如实描述。
**驳回 (B)「给生产一条活过 Close 的读面」**：违 R217 已裁的安全性质。**另驳回「重开视图后读」**：重启是新工作、spec 明说问题恢复在场，重开只会掩盖「未排空不清除」这条被钉性质。
**核心鉴别力要求**：G55 须以 `-overlay` 令清除门控忽略 `drained` ⇒ 新断言必红，证明它真钉住该性质而非恒真。

**✅ 顺带核实一处易误判处**：spec 的症状锚 Scenario 点名 `TestAttentionClearsNoticeOnWorkEnd`，而 G52 把它改成重开视图后读——**这不违 spec**：spec 同段明写「`ObjectNotice` 相位不由本变更承诺——它由合并线的排空消费处置」⇒ **通知相位在排空时耐久消费**，故重开后确实已 retired；而**未答的 question** 才恢复在场。两者不同根轴，不可混判。

**在飞**：G54／G55。**已收口**：G40–G53 中的十三单＋G52。**目标未达成——中心论点关键路径（G54）在飞，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R227 G55 收口（host 145 全绿）；🔴 它交出两条更重要的发现：attention 清除门控是第二真相（违 spec MUST NOT）＋「排空成功⇒清除」这条 spec 承诺结构上不可观测（2026-11-17）

**✅ G55 收口**：`attention_projection_test.go:490-495` 接住 `DrainReport` 并**显式钉 `report.Drained == false`**；`:497-498` 断言 summary **仍在场**，文案点名「未排空的 close 不得清除在场」。头注意图句与第三 bullet 改为如实描述，`契约:` 行未动、形状保持。**host 全量 `-count=1 -v` PASS=145／FAIL=0**；定向 ×3 全 PASS；本文件 comment_policy 0 finding。
**负锚真做**：`-overlay` 把门控 `if ended && drained` 改成 `if ended` ⇒ `:498` 红（`the unread summary beside a work end that did not drain = [], want … still standing`）；主树 md5 前后不变（`host_attention.go a50edcda…`、本测试 `bc5d5998…`）。**证明新断言真钉住该性质、非恒真。**

**🔴 发现一（产品缺陷，必修，本变更范围）：attention 清除门控与 close 报告是同一事实的两个判据。**
- **门控**（`assembly/host_attention.go:112-136`）：只枚举 `run.commit.TaskIDs()` 逐个查 `manager.Obligations`，**只看 task 义务**。
- **close 的权威判据**（`assembly/invoke.go:150`）：`drained := len(cut) == 0 && read.Resting() && stopped && !landing` —— **四项**（无被切审批等待、unsettled 读数静息、写者已停、无在落欠账）；`Unsettled` 由 `mergeUnsettled(cut, unsettledObligations(read), WriterObligations(writers), landingObligations(landing))` 合成（`:152`）。
- **G55 实测反例**：「无人读的投递＋在飞轮次、tasks=0」时 close 报 `Outcome:cut Drained:false` 且把该投递列入 `Unsettled`（原文含 `the session chain holds this input round with no answer`），**而门控判 drained=true 并清除** ⇒ **直接违 spec `attention-clearance/spec.md:45`「排空失败或未排空 MUST NOT 清除」**。
- **🔴 门控自身注释是伪断言**（元规则一实证）：`:109-111` 写「The drain is judged **the way the close itself judges it**」——**不成立**，close 判四项而它只判一项。
- **✅ 冻结修法方向（纯粹判据①③④）**：门控**读 close 已算出的同一权威判据**，不得自行重算四项（重算即第二真相）。`Run` 今日只有 `closing bool`（`run.go:461`），**无 verdict 字段** ⇒ 须让 close 把判据结果存下、门控读它；close 尚未算出时该值为零值 false，恰好给出「排空未完成一条不清」，与 spec 一致。**同批把伪断言注释换成真话。**
- **⏸ 触碰集冲突**：修法需写 `assembly/run.go`（加字段）与 `invoke.go`，**`run.go` 在 G54 白名单内** ⇒ **须待 G54 收口后派**，不得共享面双写。
- **必修负锚**：以 G55 实测的那个形状（**投递未结、tasks=0**）建案，证明修后门控拒绝清除；**保留 G55 现有那案**（有未决 task 义务），两案覆盖两个不同的未结来源。

**🔴 发现二（验收面结构性空洞，须立案不得静默略过）：「排空成功⇒attention 整体退出在场」这条 spec 承诺，经公共读面结构上不可观测。**
**机理（编排者读码核实）**：`invoke.go:128-131` 的 defer 是 `if stopped, _ := r.writers.Stopped(); !stopped { return }` ⇒ **只有在写者已停时才 `closeStorage()`**。故：
- close **成功排空** ⇒ 写者已停 ⇒ `closeStorage` 执行 ⇒ 读面被 `ErrReadAfterClose` 关闭 ⇒ **无法再读 attention 验证「已清除」**。
- close **被切**（有在飞工作）⇒ 写者未停 ⇒ **跳过 closeStorage** ⇒ 读面仍开 ⇒ 可读，但此时 `drained=false`，**本来就不该清除**。
⇒ **能读的时候必不该清，该清的时候读不了。** G55 正是因此必须把夹具构造成「在飞轮次＋未决 task 义务」才能观测。
**这不是夹具笨，是承诺缺可观测缝**。两条出路待裁（**不在本轮自决**）：①给「工作结束那一读」一个正式可观测缝（close 过程中的读数出口），使该承诺可被锚钉；②承认该承诺只能经**耐久相位**间接观测（notice 类由排空消费落地，G52 已用此路），并**把 approval/summary 类的不可观测性显式写进 spec 的验证边界**。**不得**因为测不到就删条款或假称已覆盖。
**G55 如实声明未覆盖该腿**——正确，编排者据此立案。

**⚠️ G55 两处自决须后续单复核**：①把 `host.Close` 换成 `host.Run().Close` 以取回 `DrainReport`（称同文件 `:359` 有先例）——**但 `host.Close` 会先 join host 自己的生产者（execWG／consumers／settle／retries），直接调 `run.Close` 跳过该 join**，须核这对本腿被钉性质有无影响；②夹具新增 `send` 工具与 critical 风险规则、`provider.answer` 换 `provider.decide`，以构造未决 task 义务——**这是为绕过发现一而加的夹具复杂度，发现一修好后应回退到更简形状**（投递未结、tasks=0），否则夹具长期承载一个已修缺陷的绕行。

**在飞**：G54。**已收口**：G40–G53 十三单＋G52／G55。**待派（触碰集待释放）**：attention 门控第二真相修复（待 G54 释放 run.go）、order.log 完整性背书（待 G54 释放 journal.go）、`migrateRetiredDeliveries` O(capacity) 全表扫、`--all` 单份证据、F3 重做、`newestSessionCheckpointRef` 两步、godoc 重生成、暂存后全量 lint。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R228 G54 撞 150 轮预算（非实现失败）；编排者亲验现场＝干净一致态；🔴 裁定 `step: 1` 界把可移除缺陷声明成了本征成本；派 G56 接续（2026-11-17）

**✅ 编排者亲验现场（不采信停放账，逐项重测）**：`go build ./...` **0**；recovery 锚 **绿 14.3s**；longrun 锚 **绿 36.1s**；comment_policy 三目录 **0 finding**；package-layout **EXIT=0（813 文件）**。⇒ **G54 的回退已完成，树是干净一致态，不是半改**（与 R212 那次「停放时树不可编译」不同）。
**Part 2 已落且验证**：`memory/ledger/journal.go:2253 ValueReads() int64`；longrun 已有 `addressedValueReads`（`:46/:104/:110`）入 `longRunCountedFaces`（`:311`）。**Part 1 已回退**：`run.go:660` 仍 `if cfg.bootReconcile != nil`，钩子仍可选（`:1365`）。
**G54 末句只留「Reverting Part 1 (measured regression on the boot read path), keeping Part 2」，零数字** ⇒ 该 regression 声明**未经证实**，接续单须先复现量化。

**🔴 裁定：`addressed value reads` 面的 `step: 1` 界不可作为终态。**
**亲读判据**（`symptom_anchor_longrun_test.go:336-351`）：`step` 面判的是**增量中位**，界＝`step × boundTenths / 10` ＝ 1×13/10 ＝ **1** ⇒ **每 restart 允许加 1 次点读**。该面注释明写「it grows by **the configuration fact that restart appends** and by nothing of the history behind the chain」「a face held flat … would refuse that one read per restart」⇒ **G54 是有意把 \+1/boot 写进界内并声明为本征**。
**为何不可接受**：G53 **已实测** `assembly.Boot` 走 reconcile/adopt、**N 恒 3、零追加**；`Assemble` 无 reconcile 故每 boot +1。**一条生产入口不付的成本不是本征成本**——判据⑤要求「**真需要**＋显式声明」。把可移除的缺陷声明成成本＝**把缺陷合法化**（同「代理为修测试红而加生产机制」的反向形态）。
**该面本身是对的**：它钉「绝不为链背后的历史付费」，这是真性质；**问题只在界的松紧**。Part 1 落地后须收紧为平坦/零。
**✅ 派 G56 接续**，令其：①**先复现量化** G54 声称的 regression（K=1,2,5,10 × 统一前后 × {N, ValueReads, 墙钟}），复现不出就明说「未能复现」，**不得因前任说过就采信**；②二选一——**甲** 完成 Part 1 并收紧界（给收紧前红/后绿两份读数，Host 行为逐字节不变）／**乙** 证明本征（须给 regression 数字＋撞哪条条款原文＋**回答「为什么 Boot 能零追加而 Assemble 不能」**这个核心问题），乙方可暂留 `step: 1` 但成本条目须落账；③**禁第三条路**：不得调大 step、不得把该面移出 counted faces、不得改 `judgeCountedFaces` 判据来让它绿。
**🔴 另查出一处接线不全**：grep 实证 `ValueReads` **目前只被 longrun 消费**，`symptom_anchor_recovery_test.go` **未消费它**，而派发令要求两处都消费 ⇒ G56 第三步补 recovery 侧接线（无论甲乙）。

**在飞**：G56。**已收口**：G40–G53 十三单＋G52／G55（G54 撞预算，Part 2 留用、Part 1 待接续）。**待派**：attention 门控第二真相修复、order.log 完整性背书、`migrateRetiredDeliveries` O(capacity) 扫、`--all` 单份证据、F3 重做、`newestSessionCheckpointRef` 两步、godoc 重生成、暂存后全量 lint。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R229 G56 收口：统一采纳实测可行且更便宜，真障碍是「只要键却消费带值能力」；🔴 勘误编排者两处判断；派 G57 合单（2026-11-18）

**✅ G56 复现并量化了 G54 未给数字的 regression，且证明统一本身无罪**（`/tmp/g56/probe_test.go`，`-overlay` 挂入，主树零写）：
| 入口/态 | K=1 | K=2 | K=5 | K=10 |
|---|---|---|---|---|
| Assemble·统一前 配置链事实数 | 1 | 1,2 | 1..5 | 1..10（+1/boot） |
| Assemble·统一前 ValueReads | 1 | 1,3 | 1,3,5,7,9 | …19（+2/boot） |
| **Assemble·统一后 事实数** | 1 | 1,1 | 全 1 | ×10 全 1（**平**） |
| **Assemble·统一后 ValueReads** | 1 | 1,2 | 1,2,2,2,2 | 全 2（**平**） |
| Boot·两态 | 3 | 3,3 | 3… | 3…（**逐字不变**） |
**统一后 Assemble 墙钟 4–5ms ＜ 统一前 8–20ms ⇒ 采纳本身不贵。**
**真机理（trace 实证）**：`assembly/commit.go loadAddressedLocked` 用 `CommitOrder.EachCommittedNewestFirst` 找「最新配置事实键」，**该走法每途经一个键就读一个值**。统一前每 boot 追加一条 publication ⇒ 链顶恒为全局最新 ⇒ 只走 1 步；统一后链顶沉入历史 ⇒ 走完「自上次发布以来提交的全部键」（`before-latest-record=1 → after=41/75/129/183/237`）。longrun `addressedValueReads` 步长 **+1/gen → +54/gen**。

**🔴 勘误一（推翻 R225/R228 的「Boot 不付此成本」）**：G56 归属实验——用统一态造出「链顶被埋」的根（6 代历史）再走 **Boot 入口**：`reads=419`（同根紧接第二次 boot=89；空根=3）⇒ **Boot 同样付这笔账**。既有夹具只是两次 boot 之间不提交事件才没暴露。**⇒ 「一条生产入口不付的成本不是本征」这条推理的前提不成立**：两入口都付，只是空根/无新事件时看不出来。**编排者据旧前提下的「把缺陷合法化」定性须收窄**——`step: 1` 界确实太松（它容纳的是「链顶发现按提交序走且每键读一值」这个真缺陷），但**该缺陷不是 Assemble 独有**。
**🔴 勘误二（编排者派发令前提错）**：R228 称「`ValueReads` 只被 longrun 消费、recovery 未接线」。**亲 grep 实证 recovery 侧早已接线**：`symptom_anchor_recovery_test.go` 的 `addressedValueReadsOf:175`、测量 `:257/:269`、断言 `:1008-1016`（**2× 界**）、活性探针 `:624-642`、无引擎具名拒绝 `:699`，series `[1 264 305 306 307]`。**问题不是未接线，是界太松。** G56 未新增 recovery 断言并说明「缺陷未修前再钉一条 +1 只会把缺陷钉得更死」——**编排者采纳该自决**。

**✅ 裁定：问题定性＝消费者用错了能力，不是能力多余。已派 G57 合单。**
**编排者亲读契约面**（`memory/ledger/atomic_store.go:67-88`）：`CommitOrder` 是**可选能力接口**（key-ordered 引擎不答，调用方靠类型断言）；其 doc **自己就声明**「naming it costs the records walked **and the values those records state**」；四红线含「**MUST answer the value the newest statement left under the key, so a caller never reads it twice**」。⇒ **带值是该契约的承诺，不是缺陷**；而**配置链顶发现只需要键**，为不需要的值付费。
**两个走法已存在**（亲 grep）：`journal_kv.go:279 eachCommittedNewestFirst(visit func(key string))` **只给键、未导出**；`:305 EachCommittedNewestFirst(visit func(key string, value []byte))` 带值、导出。
**方向**：给「只陈述提交序的键」一个正式导出面 → 链顶发现改用 → 统一 boot 采纳（G56 已证可行）→ **两面同批收紧**（longrun `step:1`→zero/平坦；recovery 2× 界同批）→ 同批改注释真话。
**三条歧路已否**（G56 逐条论证，编排者采纳）：①`Keys` 前缀枚举 `journal.go:1512 slotScanBytes += records*a.stride` 付整张槽表 ⇒ 违 longrun 的 slotScan **zero 面**；②自造探顶（`Get` 失手免费）**失去「链顶丢失」的具名拒绝**，洞落探针位置即静默截短；③head 键＝第二真相。
**G57 第一步须自判导出面形状三选一**（扩 `CommitOrder` / 新立可选能力接口 / 把既有未导出实现提为 `journalKV` 导出方法经既有断言取）。编排者倾向第三（实现已存在、断言形态已存在、不新增接口），但**令其读码后自判、不得顺着倾向走**——若静态类型是接口而非 `*journalKV`，第三案是否等于第二案须它回答。

**✅ G56 验证面（命令直取 exit code 非管道）**：build 0；vet 0；`go test ./assembly/ -count=1` **0 FAIL（253.6s）**；recovery ×3 全 0；longrun ×3 全 0；history 两例 0；host **145 PASS／0 FAIL**；负锚 md5 前后一致（`commit.go 395becdd…`／`run.go 7ce323e1…`／`host_document.go a0e99744…`），探针只存在于 overlay。**树已逐字回退到发现时状态**（longrun series 逐字符复现 `1 63 72 93 94 95…`）。
**⚠️ G56 用 `-no-baseline <9 目录>` 跑两门**——另一 scope，编排者已在 G57 令中明禁并给出权威跑法。

**在飞**：G57。**已收口**：G40–G53 十三单＋G52／G55／G56。**待派**：attention 门控第二真相修复（待 G57 释放 run.go）、order.log 完整性背书（待 G57 释放 journal_kv.go）、`migrateRetiredDeliveries` O(capacity) 扫、`--all` 单份证据、F3 重做、`newestSessionCheckpointRef` 两步、godoc 重生成、暂存后全量 lint。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R230 ✅ G57 收口：可移除的历史依赖全部消除，两面收紧到平坦；中心论点的准确表述已可给出；派 G58／G59 收两条安全缺陷（2026-11-18）

**✅ G57 判形取 (C)，理由有实证**：`CommitOrder` 全仓仅两实现者（`journal_kv.go:305` ＋契约样例 `examples/contract/memory_ledger/CommitOrder_example.go:25`）、三消费者（`assembly/commit.go:255`、`assembly/checkpoint.go:881`、`memory/ledger/segment_store.go:1181`）。**(A) 否**：加方法即让样例实现失效（`examples/contract/` 不在白名单）且与其四红线冲突。**(B) 结构上不可行**：新导出接口会加 `exported_interfaces` 计数，**棘轮上限 85 已用满**（`golden/runtime/architecture.json:5`，按名抬升须写 golden 而 golden 禁写）。**(C) 是**：只把既有未导出实现提为导出方法，且**该实现早已有独立第二消费者** `indexKeyOrder`（`segment_store.go:1137`，索引走查只需键）⇒ **不是为单使用方造抽象**（元规则二成立）。消费者侧窄口 `addressedKeyOrder` 落 `assembly/commit.go`（未导出，与既有 `addressedChainLog` 同处），**未新增任何导出类型/接口**，concept_budget ok。
**新导出面契约 doc（`journal_kv.go:295-302`）**：`EachCommittedKeyNewestFirst` …「**The walk pays the records it walks and reads no value**…a caller that reads the value of each key it names asks `EachCommittedNewestFirst`」。**`CommitOrder` 四红线一字未改。**
**链顶发现改法**（`assembly/commit.go:264-310`）：键走查停在首个链前缀键 → 由键解析 seq → `Get(newestKey)`（**发现只付这 1 个值**）；**洞拒绝原样保留**（`:307` "a hole is a damaged ledger"）。
**统一采纳**：`run.go:663-671` Host 路不变，否则 `adoptRecordedPublication`（`commit.go:702-731`，判据＝链上最新 publication record 的 fingerprint 等于本 boot 声明指纹）；`host_document.go:1135-1172` 的 `adoptBootGeneration` 参数放宽为 `GenerationFaces`，**Host 调用与语义逐字未变**。

**✅ 两面收紧（红/绿各一份，编排者已复核 longrun 实跑）**：
- recovery（`:1002-1018`）：**2× 界 → 相等界**。修前 `[1 36 37 38 39]` 红 → 修后 `[1 35 35 35 35]` 绿。
- longrun（`:306-312`）：**`step: 1` → 平坦面**。修前末窗中位 280 vs 参考 34（界 44）红 → 修后**全 252 代 `addressedValueReads` 恒 31** 绿。
- **`countedFace.step` 机制整条退役**（编排者 grep 复核 `face.step|step:` **零残留**）：其被量对象＝「每次重启追加的那条事实」已不存在，新面更紧（平坦 1.3× 取代「每重启容许 +1.3」）⇒ **零残留正确，非削弱**。
**实测表（K＝连续 boot 次数，末次 boot）**：assemble 修前 N/读 = 1/1、2/49、5/80、10/85 → **修后 1/1、1/22、1/30、1/30**。链顶被埋 6 代根：assemble (7,82)→(1,30)；Boot (11,133)→**(4,120)**；空根两入口前后不变。
**三组负锚全红（`-overlay`，主树零写，md5 一致）**：①撤采纳 ⇒ recovery `read 37 value(s) … beside the 36 … want the same count` ＋ longrun `reads 280 beside the 34 … over the bound of 44`；②洞静默跳过 ⇒ `the load answered 1 fact(s) of a chain missing the fact …:00000000000000000001, want the hole refused by name`；③新面回退成带值走法 ⇒ ledger `the key walk read 3 value(s) of the 3 key(s) it named, want none` ＋ assembly `the load read 9 value(s) for the 2 fact(s) of the chain`。
**验证面**：build/vet 0；ledger ok 66.8s；**assembly ok 252.1s / 0 FAIL**；两锚 ×3 全 PASS；history 两控制 ok；**host 145 PASS / 0 FAIL**；comment_policy 三目录 0；package-layout 813 files EXIT 0；`tool/subagent` ok。

**✅ 中心论点的准确表述（编排者亲跑 longrun -v 读出判据原文，据此定稿）**：
锚的判据原文是 **`the boot growth at depth 252 is separated from the machine: held p10 2.217 over control p90 1.235, and the counted faces hold it to the declared reads`**。⇒ 它**不声称墙钟有界**，它声称两件事：①增长**真实且与机器噪声分离**（held p10 > control p90）；②增长**全部归因于已声明的计量面**。
**故正式表述**：**可移除的历史依赖已全部消除**——配置链事实数与链载点读双平（N 10→1、读 85→30）、槽表枚举 0、索引走查有界、链顶发现只付 1 值；**剩余增长是已声明的本征项**（开档全 log CRC，R182 裁定保留，理由是墓碑/缺席路径的唯一背书＋冻结契约「开档即拒」），且**锚已证明不存在未声明的增长**。**这正是判据⑤「本征 O(历史)＝真需要＋显式声明」的达成形态，不是「boot O(1)」。** 深度 252 墙钟仍约 2.2×，**如实声明，不得声称有界**。

**✅ 编排者亲收 G57 漏改的一处伪断言**：`symptom_anchor_longrun_test.go:407` 原写「each boot **appends** the publication record its assemble commits, so the pair is **not strictly identical work**…the ratio reads slightly compressed」——采纳后不成立。改为「each boot **adopts** the publication record the chain already holds, so the pair **is identical work**: the difference the depth carries is the whole ratio」。
**⚠️ 追认 G57 三处自决**：(a) 采纳判据落 publication record 而非 declaration 事实（实测 Assemble 路只落 record，落 declaration 会永不采纳）；(b) 删 `step` 机制（见上，零残留）；(c) `TestCostPhasesAreMeasuredWithoutOverlap` 的 `rebind ≥ 0` 守卫改为「两侧均测得」——理由：该守卫拿**fixture 收尾根**上单独测的 open 比末次 boot，采纳后 boot 更快（8.5ms vs open 10.1ms）使前提失效，**sum 重叠判据原样保留**。**编排者立案待复核**：这是「因修复使守卫失效而改守卫」的形状，须在 R35 独立审中专项核它是否掩盖了真的重叠。

**✅ 派 G58／G59（触碰集不相交，并发）**：
- **G58**：attention 门控第二真相（`host_attention.go` 读 close 的同一 verdict，禁重算四项）＋同批改伪断言注释＋**回退 G55 为绕过缺陷而加的夹具复杂度**（回到 tasks=0 原始反例形状，保留有 task 义务那案作第二覆盖）＋核 `host.Close` vs `run.Close` 跳过生产者 join 的影响＋核实「排空成功⇒清除」不可观测性并给出应写进 spec 验证边界的条目原文。
- **G59**：order.log 完整性背书。令其先判三形状（加 CRC／读侧结构校验／walk 改用与点读同一权威），**并预先写进令的级联面**：加 CRC 会把 order 字节带进 `integrityBytesScanned`，而该面现钉「1:1 等于 journalLogBytes」⇒ **须同批改分母与注释与成本声明，不得只改一侧**；另明写必须保住「开档即拒」与三个计量面语义。**核心鉴别力＝等长 offset 互换那个「静默错读」变体修后必须在开档具名拒绝。**

**🔴 未清项（G57 如实报，编排者登记）**：`gen_godoc.sh --check` **现态红**（docs/api 落后 governance/task/recall/ledger 四包）；`gofmt -l` 有 **7 个非 G57 文件**不格式（`assembly/host_delivery_test.go`、`memory/query/inventory_test.go`、`runtime/task/{cancel,handoff,manager}_test.go`、`tests/integration/execution/settle_wake_account_test.go`、`tests/integration/host/delivery_headless_test.go`）。两项均为编排者收尾面。

**在飞**：G58／G59。**已收口**：G40–G53 十三单＋G52／G55／G56／G57。**待派**：`migrateRetiredDeliveries` O(capacity) 扫、`--all` 单份证据、F3 重做、`newestSessionCheckpointRef` 两步、godoc 重生成、gofmt 7 文件、暂存后全量 lint、R35 第二轮独立放行审。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R231 G58 收口（门控第二真相已消）；🔴 它交出的负锚指向「清除条款可能为空」——立案待证，暂不落其建议的验证边界（2026-11-18）

**✅ G58 收口**：
- `assembly/run.go:462-468` 新增 `closeDrained bool`（紧邻 `closing`）。**先 grep 核过既有字段不承载该 verdict**——`closeReturned` 对 Cut 也为真，故不可复用。写入点 `invoke.go:151-153` 紧随 `drained` 计算、**与 `report.Drained` 同一变量**（同一真相，非重算）。
- 门控 `host_attention.go:104-119` 改为同一把 `r.mu` 读回 `run.closing, run.closeDrained`，**删掉逐 task `Obligations` 重算**与 `errors` 导入。零值 false＝未排空⇒一条不清，**无兜底**。
- **伪断言换真话**：旧「The drain is judged the way the close itself judges it: every task the commit port lists answers no open obligation…」→ 新「Both readings are the close's own: the verdict is the one the close stored off the criterion it reported, so this face repeats none of its terms, and a close that has not reached a verdict yet reads as a drain that did not finish.」
- **夹具回退＋两案**：第三腿去掉 `send` 工具/critical 规则/decide 分支，回到**tasks=0 原始反例形状**，断 `Outcome==CloseCut && !Drained` 并追加「每条 Unsettled `TaskID==""`」**钉住前提**；原复杂形状另立 `TestSummaryAttentionStandsOnUnsettledTaskObligation`，追加「至少一条 Unsettled 带 TaskID」钉住其前提。**两案各钉未结来源形状，防夹具漂移使负锚恒绿——正确纪律。**
- 新建 `assembly/host_attention_test.go`（镜像生产词干）钉「读回 close 判据、未存则不排空」。
- **`host.Close` vs `run.Close` 核查有结论且是硬冲突非偏好**：`host.Close` 先 join host 生产者⇒写者全停⇒`invoke.go:124-132` defer 走 `closeStorage()`⇒读面关闭；既有 `Host.CloseReport()` 能拿报告但读不到在场 ⇒ **join 与被钉性质直接冲突**，故保留 `run.Close`。**编排者追认。**
- **自决三处追认**：去掉 `attentionWorkEnd` 的 ctx 与 error 返回（旧唯一 error 源已删）；**未加 `run.closing &&` 短路**（`closeDrained` 只由 close 写、close 必先置 `closing`，调用点已 `ended && drained`，加短路是恒真防御 ⇒ 符合「不为不可能场景加防御」）；两案各加未结来源形状断言。
- **门红自修**：`comment_policy` 报 `index-root: index target must live under docs/`（新测试引 openspec 路径）→ 改引 `docs/wiki/agent/execution-generations.md#lifecycle-convergence` ⇒ 0。**契约索引必须指 docs/ 不得指 openspec，此为该仓门教义，登记备用。**

**🔴 立案（比已修缺陷更重要）：清除条款可能是空条款，其机制可能自证。**
**G58 如实报的决定性负锚**：**令 `attentionWorkEnd` 恒返回 `(false,false)`（永不清除），8 个 attention 集成测试全绿。**
**其解释与依据链**：`drained := len(cut)==0 && read.Resting() && stopped && !landing`（`invoke.go:150`），而 `runtime/task/unsettled.go:96` 的 `Resting()` 要求 **Inputs/Notices/Work 皆空** ⇒ **drained ⟹ 三类在场本就为空 ⇒ 清除无对象可去**。
**若蕴含成立**：`approvalQuestions(ctx, cleared)` 的 `cleared` 参数、`if ended && drained { return questions, nil }` 早返及 notice/summary 侧对应处置，是**效果已被自身前提保证的结构**（撞元规则二），应删；其服务的 spec 条款「排空成功后 attention 读面 SHALL 整体退出在场」**是空条款**，退役属**另一契约面裁定**（已接受主规格）。
**若蕴含不成立**：清除分支有真实作用，**只是缺锚**，须补一条能杀死该变异的锚。
**✅ 派 G60 只读取证五项**：逐类核蕴含（**approval 读的是审批账不是 `read`**，须核「待答但未被 cut」是否存在；notice/summary 是否有 `Resting()` 不覆盖的来源如过期读侧判或 durable 相位不同步）／反例构造尝试／**自己复跑该变异并给真实计数，另核 G58 新锚 `TestAttentionWorkEndReadsTheCloseVerdict` 是否也被绿掉**／若成立则枚举删除面与受影响 spec 条款原文／判「加验证边界」是否等于把空条款合法化。
**⏸ 编排者裁定：暂不落 G58 建议的「验证边界」条目。** 理由：若条款为空，加一段「本条款只验证两点」的边界文字＝**把空条款合法化**，与「不得因测不到就删条款或假称已覆盖」是同一纪律的两面。**待 G60 证成后再定：空则立案退役（另契约面），非空则补锚。**

**⚠️ 并发读数非权威再实证**：G58 报并发跑两套件时 longrun 红（48.31s，1.3× 相对界），**单跑 27.97s 绿、独立全套件复跑 0 FAIL** ⇒ 负载污染非回归。另其一次 `grep -c` 零匹配使复合命令 exit 1（host 套件实际 ok 229.4s）——**两个已知陷阱同轮复现，登记备用。**

**在飞**：G59／G60。**已收口**：G40–G53 十三单＋G52／G55／G56／G57／G58。**待派**：`migrateRetiredDeliveries` O(capacity) 扫、`--all` 单份证据、F3 重做、`newestSessionCheckpointRef` 两步、godoc 重生成、gofmt 7 文件、暂存后全量 lint、R35 第二轮。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R232 ✅ G59 收口：order.log 静默错读已消；🔴 编排者勘误自己令里的「开档即拒」要求——真相字节拒开、派生载体重述，兄弟先例已证（2026-11-18）

**🔴 编排者自纠（本条最重要）**：G59 派发令写「你的修法须让 order 损坏也在开档被拒」。**该要求错了**——编排者引冻结契约面时**未先核该契约的主语是真相字节还是派生索引**。
**亲验兄弟先例（承重证据）**：`memory/ledger/journal.go:1355-1361` —— 槽表记录解码失败（`errSlotUnplaceable`）且 `a.statesIndexFiles()` 时，走 **`a.rebuild()` 然后重试**，**不拒开**。槽表与 order.log **同为可从日志重建的派生载体** ⇒ **「真相字节损坏拒开、派生载体损坏从真相重述」才是该仓一贯的规则**，且它与判据①（链是唯一真相）③（无第二真相）一致：派生载体不配享有「损坏即致命」的地位。
**⇒ 采纳 G59 的偏离，勘误编排者令。** G59 另给两条实测支撑：(a) 开档全校验 order 与 `06/specs/bounded-recovery/spec.md`「开档 SHALL 只重放切面偏移之后的尾」＋Scenario「开档读入字节 SHALL NOT 随累积历史线性增长」相撞；(b) 短值夹具下 **order.log 135B > journal.log 111B**，开档加读≈再付一遍 integrity，而改分母的两锚在其禁写面 ⇒ 级联只做一半＝R223 判过的锚病。**(B) 读侧结构校验被实测否掉**：等长互换后两 offset 均在界内、两 token 均合法，边界/单调校验抓不住。

**✅ G59 修法**：`journal.go:151` 校验和宽度；`:1287 journalOrderLine`（`%08x `＋JSON＋`\n`，**文本形避免 0x0a 被行切分裂**）；`:1300 journalOrderStatementOfLine`（长度/空格/CRC/JSON 四检，`:1283 errOrderUnplaceable`）；`:1322 fileCommit` 改用之；`:1836 eachNewestFirst` 拆 wrapper＋`:1858 eachNewestFirstOnce`（复用 `seen`：写面 rebuild 后续走不重投、读面 wholeLog 后 `orderSize=0`）；`:1057 wholeLog` 重置 window。
**核心鉴别力达成（修前/修后对照，`-overlay`，主树 md5 未变 `1fbe59ae`）**：
- **修前**：等长互换 ⇒ open ok、点读 AAAA/BBBB、**walk alpha=BBBB beta=AAAA（静默错读）**；+1 ⇒ open ok、walk 具名 `ErrJournalCorrupt: invalid character 'Q'`。
- **修后**：两变体 ⇒ **walk 与点读同为 AAAA/BBBB**，写面把 order 重述回 `8082d9ea…`。**静默错读与两路异答同时消除。**
- 关掉校验比对 ⇒ 新锚红：`walk 1 named …,0:evt:00001=second, want …,0:evt:00001=first`。新锚 `journal_test.go:1035` 另断言读面 `assertWholeLogNotice`（**回退具名**，经既有 `journal_kv.go:147-152 IndexUnstatedNotice`）与零写。
**未升 `journalSlotVersion`**：旧无校验和格式的行被解码拒绝后自愈 ⇒ **无兼容分支**（零残留）。
**验证面**：build/vet 0；ledger **ok 77.0s / 0 FAIL**；assembly **ok 257.8s / 0 FAIL**；history 三测 ok 7.1s；两锚 PASS（integrity==journalLogBytes 1:1、slot 0、walk 面平，**分母未改故三计量面原样绿**）；comment_policy 0；package-layout **EXIT=0（814 文件）**。

**✅ 编排者亲收文档真话三处**（G59 禁写 docs/openspec）：`06/design.md:503` 载体行→「逐行校验和 ＋ 一行一提交语句…行校验失败即从日志重述，与槽表同形」；`06/tasks.md:178`「order.log（水位截断白得）」→「逐行语句校验和，失败即从日志重述」；`docs/wiki/memory/memory-architecture.md:160` 写入行补「每行带校验和」＋「**行/记录校验失败即从日志重述而非拒开**（拒开只针对真相字节 `journal.log`）」。实测 doc_budget **0**、doc_links **0**、package-layout **0**。

**⚠️ 立案（共享缺口，非 G59 引入）**：**写面 rebuild 静默**——G59 如实报 order 写面重述无通知通道。**但槽表的 `a.rebuild()`（`:1358`）同样静默** ⇒ 这是**两个派生载体共有的既有形状**，不是 G59 造成的不对称。**裁定：不在本轮单修一侧**（单修会造新的不对称）；立案为「派生载体重述的可见性」共享缺口，**when**：本变更收尾的 R35 独立审若判其阻断则本轮补，否则归独立变更；**why**：静默重述不改变正确答案（重述源是真相），但操作方学不到「索引曾损坏」，属可观测性而非正确性。读面已具名，故严重度低于写面。

**⚠️ G59 未查面（登记）**：跨 64KiB chunk 的校验重组无专属用例；stated 区间内半写末行、**校验和数字本身被改**、旧无校验和格式自愈三者走同一分支但无专测。**⇒ 立案为 F3／R35 的必查项**（同一分支承载多成因而无分别判据，正是「空/缺失多成因须各有判据」的形状）。

**在飞**：G60。**已收口**：G40–G53 十三单＋G52／G55／G56／G57／G58／G59。**待派**：`migrateRetiredDeliveries` O(capacity) 扫、`--all` 单份证据、F3 重做、`newestSessionCheckpointRef` 两步、godoc 重生成、gofmt 7 文件、暂存后全量 lint、R35 第二轮。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R233 🔴 G60 构造出反例，推翻三条结论（G58 的解释／编排者的空条款倾向／编排者 R227 的「不可观测」立案）；清除行为零锚，派 G61 补（2026-11-18）

**✅ 裁决：蕴含不成立。清除分支有真实作用，只是缺锚。不得删。**
**逐类来源核实（G60 取证，编排者采纳）**：
- **approval**：`host_attention.go:128`→`approval_store.go:441`→`attention.go:252`，读 **ApprovalManager 自己的 attention 链**，**与 `read` 无关、`Resting()` 完全不覆盖** ⇒ **蕴含不成立的根据在此**。
- **notice**：`host_attention.go:146/150`→`read.go:486 commit.Outbox`（全任务史）＋`read.go:426 store.Site` 信箱；而 criterion 侧走 `invoke.go:421 manager.Owed`（registry 义务索引、按 Refused 过滤）＋`memory.go:332` ⇒ **两个不同索引**，非结构覆盖，**G60 标为未证**。
- **summary**：`host_attention.go:201-229` 读 `run.facts`；established session 的未读投递即未答输入轮（`invoke.go:462`）⇒ 实测成立。

**✅ 反例构造成功（生产注册口，非注入）**：`book.Request` → `run.Checkpoint()`（stated approvals=1）→ close → 撤请求文件 → 生产 `Boot` 重开 ⇒ `resting=true, work/notices/inputs=0` ⇒ `Close` 报 **`drained=true outcome=quiesced unsettled=[]`**，而 **`AttentionPresence=1`**（未过期，TTL 30min）⇒ 清除分支 live 时 `AttentionQuestions=0, err=nil`。**同一 run 内直接注册亦得 `drained=true ∧ presence=1`。**

**🔴 三条被推翻的结论（逐条勘误）**：
1. **G58 的解释错**：全绿真因**不是**「drained⇒无对象可去」，而是**两条 notice work-end 锚经 `reopenDrainedHost`（`attention_projection_test.go:26-38`）读的是重开的新 run（`closing=false`）⇒ 清除分支根本没执行**。**第十三类锚病：反例路径与锚输入集不相交。** 另 G58 报「8 个」实为 **10 个**。**⚠️ 该不相交是编排者 R217 裁「另开读视图」的级联产物**——重开腿钉的是耐久消费（正确且须保留），但它使清除分支失去覆盖。
2. **编排者 R231 的「空条款⇒立案退役」倾向错**：条款不为空，**不该退役，该补锚**。G60 明确反驳编排者倾向并给理由，**编排者采纳**。
3. **🔴 编排者 R227 的「排空成功⇒清除结构上不可观测」立案错**：**早返本身就是可观测缝**——G60 实测 drained close 后 `AttentionQuestions=0, err=nil`；**去掉早返即报 `memory: read through a closed view`** ⇒ **早返是关闭后读面唯一成功路径**。故该承诺**可观测**，R227 那条立案撤销。

**✅ 两组变异的真实计数（G60 实测，编排者采纳为验收基准）**：
- **M1**（门控恒 `(false,false)`）：host **146 PASS/0 FAIL**、attention **10 全 PASS**；assembly 选择器 **FAIL** ⇒ **`TestAttentionWorkEndReadsTheCloseVerdict` 杀死 M1，门控读数已有锚**。
- **M2**（保留门控、**删清除分支**）：host **146/0**、assembly **ok** ⇒ **清除行为零锚**。新锚只钉门控读数、不钉清除效果。**G58 未区分这两件事，登记为其报告的不准确处。**

**✅ 裁定：不落 G58 建议的「验证边界」条目（理由与编排者原倾向相反）**。条款不为空 ⇒ 那段边界把**唯一可观测的清除效果读数**判为不可用，**等于把缺锚合法化**。**正确处置＝补能杀死 M2 的锚。已派 G61**（白名单只三个测试文件、**禁改任何生产 .go**）：①主锚钉清除效果（`drained=true ∧ approval presence≥1` ⇒ `AttentionQuestions` 答 0 且 err=nil，须杀 M2）②第二锚钉早返是关闭后唯一成功读路径（**两侧对照**：同一视图 `Query` 仍具名拒绝，证明早返只答它自己那一维而非救活整个读面，须杀「删早返」变异）③修既有两条 notice 锚的不相交（**保留重开腿**，另加真实工作结束那一读的断言；若 notice 类该形状不可达则如实说明、只保留 approval 主锚，**不得为凑覆盖造不可达断言**）。

**⚠️ 新立案（疑空条款，须另契约面裁，G60 不自判）**：**唯「反思投递」那条**——「未被读到的摘要 SHALL 在读面随工作结束退出在场」＋Scenario「未读到则随工作结束清」的 **WHEN 实测不可达**（未读投递⇒永不 drained）。**这与 G55 收口时那条腿的前提不自洽同源**，现获独立确认。**when**：本变更收尾 R35 若判其阻断则本轮议，否则归独立契约面变更；**why**：退役已接受主 spec 条款属另一裁定面。**其余条款（「排空成功后 SHALL 整体退出在场」「排空失败 MUST NOT 清除」及 Scenario「排空失败不清」）均仍有效可观测。**

**⚠️ G60 未查面（登记）**：notice 反例未端到端构造（仅结构证据：`MemoryCommit.owed` 全文无删除点 `commit.go:212/246`、refuse 只写 `mailbox.go:137`、criterion 按 Refused 过滤 ⇒ **记忆提交路径被拒通知永挂**，此本身疑为缺陷，须另核）；summary 的 restart/seed 形状未构造；未跑 assembly 全包与其他 integration 包；未核投影侧与人侧 Pending。

**在飞**：G61。**已收口**：G40–G53 十三单＋G52／G55／G56／G57／G58／G59／G60。**待派**：`migrateRetiredDeliveries` O(capacity) 扫、notice 被拒通知永挂疑点核实、`--all` 单份证据、F3 重做、`newestSessionCheckpointRef` 两步、godoc 重生成、gofmt 7 文件、暂存后全量 lint、R35 第二轮。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R234 ✅ G61 收口：清除效果零锚已补，三组变异各有判别力；全部派发单收口，`--all` 前置满足已启动；gofmt 亲收归零（2026-11-18）

**✅ G61 收口（补锚单，禁改生产 .go 已遵守）**：
- **主锚** `assembly/host_attention_test.go:44 TestAttentionQuestionsClearsApprovalOnDrainedClose`：生产 `Boot`(localfile durable) ＋ 生产注册口 `book.Request`(critical) → `host.Close` → `CloseReport{quiesced, drained:true, unsettled:0}` **且 `AttentionPresence=1`** → 读面答 **0 条、err=nil**（`:57-63`）。**取「同 run 直接注册」而非「撤请求文件重开」**：无外部干预、不造第二来源。消费者锚＝生产 wiring `run.go:656 hostReading{runReading{run:r}, approvals:cfg.approvals}`，测试按同形接线读同一未导出面。
- **第二锚** `:72`：**三侧对照**——`Tasks` → `errors.Is(ledger.ErrReadAfterClose)`（`:79`）；`run.Query(ObjectTask)` → 同具名拒（`:85`）；`AttentionQuestions` err=nil、0 条（`:89-94`）。overlay 实测：Query 的错误列 invocation/task/notice 三维而 **attention 维未列** ⇒ **早返只救它自己那一维，不是把整个读面救活**。
- **notice 锚不相交处置**：**重开腿保留**（钉耐久消费），另加「关闭后该根不可读故须重开」的具名拒绝断言（`attention_lifecycle_test.go:228-232`），并**把头注第 3 条改真**（原写 "the same read"，实为重开视图）。**notice 类清除效果判定不可达**（drained ⇒ `Resting()` 已覆盖 mailbox/欠账 ⇒ noticeQuestions 本就 0；且关闭后整投影只答 attention 维）⇒ **按令 fallback 只留 approval 主锚，未为凑覆盖造不可达断言。正确。**

**✅ 三组变异各有判别力（G61 自加第三组，编排者高度肯定）**：
- **M2**（删早返＋`cleared` 传参）：`host_attention_test.go:59: the attention read face after the drained close = memory: read through a closed view, want the questions of a cleared work end` 红；第二锚同红；既有 gate 锚仍 PASS。
- **M2'**（只删早返）：两锚同文案红。
- **M2a（G61 令外自加）**（保留门控与早返、**只把 `cleared` 传 false**）：`:62: attention questions after the drained close = [{Kind:approval Target:approval Lineage:tool=exec digest=0cf328… approval=approval-clearance-1 …}], want none` 红。**这一组是关键**——M2/M2' 都靠错误码红，单凭它们不能证明主锚对 **presence 本身**有判别力；M2a 证明它确实钉住了清除效果而非只钉错误码。**「变异按面分组」的正确执行。**
- **M1 回归**：`:27: the work end beside a close that stored a drained verdict = (false, false)` ⇒ 既有锚仍杀 M1。
- md5：`assembly/host_attention.go = 339968b1…` 前后一致；四文件 before/after `diff` EXIT=0；探针文件落盘不存在。
**验证面**：build/vet 0；**host 146 PASS／0 FAIL（204.9s）**；**assembly ok 247.7s／0 FAIL**；三测各 ×3 全 PASS；comment_policy 三目录 **0**；package-layout **814 files EXIT 0**。前提复核三次一致：`{outcome:"quiesced", drained:true, unsettled:0} beside 1 approval question(s) still present`。

**✅ 编排者亲收 gofmt**：7 个非本轮代理所写的文件（`assembly/host_delivery_test.go`、`memory/query/inventory_test.go`、`runtime/task/{cancel,handoff,manager}_test.go`、`tests/integration/execution/settle_wake_account_test.go`、`tests/integration/host/delivery_headless_test.go`）`gofmt -w` 后 **`gofmt -l` 计数 0**，`go build ./...` **0**。
**⚠️ 编排者操作次序失误自认**：gofmt 写在 `--all` 复制整树实验**启动之后**。虽为纯格式无语义改动，但正确次序是**先清格式再启动复制树实验**。**若 `--all` 报 source_digest 混合基线，须重跑而非解释。** 登记为操作纪律。

**✅ `--all` 单份证据已启动**（`python3 scripts/gates/check_event_integrity_mutations.py --all --json --out /tmp/g62-mut-all`）：**全部派发单已收口、无在飞写者** ⇒ R219 立的「复制工作树类实验须排全部写单收口后」前置**首次满足**。上一次（R216）19 控制中 18 绿、唯一红是 G44 在飞半成品；本次待读数。

**在飞**：`--all` 真跑（编排者直跑，非代理）。**已收口**：G40–G61 全部派发单。**待派/待做**：`migrateRetiredDeliveries` O(capacity) 扫、notice 被拒通知永挂疑点、F3 按新锚集重做、`newestSessionCheckpointRef` 两步、godoc 重生成（现态红，落后四包）、暂存后全量 lint、R35 第二轮独立放行审、F1 剩余覆盖、F4 归档（须用户明示授权）。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R235 ✅ `--all` 首次全控制绿：19 控制绿／23 变异 live／21 killed；两条 `assertion_not_matched` 逐条取原文定性，均非产品缺陷，派 G62（2026-11-18）

**✅ 里程碑读数**（`--all --json --out /tmp/g62-mut-all`）：**`control_tree_green=True`，19 控制全 green，`mode=live`、`replay=None`，23 变异全跑，21 killed／2 not_killed**。**这是 CI 兼容的单份证据形态首次成立**（R214 立案的「dod 门 rglob 收多份而 CI bundle 硬判单份」阻断，其前置已满足）。门进程 EXIT=1 仅因两条未 killed。
**⚠️ source_digest 未报混合基线** ⇒ R234 自认的「gofmt 写在复制树实验启动之后」未造成证据污染，但次序纪律仍登记。

**🔴 两条 `assertion_not_matched` 逐条定性（编排者亲取 run.jsonl 原文，非采信摘要）**：
**① MUT-DOD-P4-window-unbounded ＝ 前置守卫先红，主题断言未执行。**
实测：变异确已注入（`indexWalkVisits=[0 45 96 147 198]` 线性增长），但 **`symptom_anchor_recovery_test.go:931` 墙钟守卫先 `t.Fatalf`**：`the wall clock of the assemble grows with the history: the boot of generation 4 took 68.471042ms beside the 29.560209ms of generation 1, over the bound 59.120418ms of 2 times the second boot` ⇒ **abort 在 :931，注册主题断言在 `:973`（`the walk follows the history the chain holds instead of stopping at the cut its newest checkpoint states`）根本没执行**。
**裁定：把 recovery 锚的计量面判据移到墙钟守卫之前。** 计量面确定且指名具体缺陷，墙钟是噪声大的聚合量；**先判精确面**才能得到与主题匹配的具名 kill。longrun 已「计数面先判」，recovery 须同形。**墙钟守卫保留、仍须通过、2× 界不动**，只后移求值次序 ⇒ **非削弱**。**这是「前置守卫红≠主题红」的正面处置。**
**② MUT-EVID-02-dual-material-input ＝ 注册 site 漂移，语义上已杀。**
实测：注册 `expect_assertion="user copies of"`、site `event_integrity_test.go:563`。变异下**多处断言真红且文案确含该串**，firing 位置 **:1202/:1289/:1369/:1450/:1551/:1612/:1719/:1808/:5417**（如 `:1202: the follow-up request carried 2 user copies of "…", want exactly 1`）；而 **:563 那条未 firing**。⇒ **`assertion_hit=False` 只因 site 不匹配，文案匹配**。
**裁定：把 site 重指到实际 firing 位置**（取测试执行序中最早 firing 者，须实跑确证），**`expect_assertion` 文案不改**，**不得改宽松匹配或改门的选择逻辑**。

**✅ 派 G62**（白名单只 `check_event_integrity_mutations.py` ＋ `symptom_anchor_recovery_test.go`，**禁改生产 .go**）：两条各自单跑 live 确证 `named_assertion`/`assertion_hit=True`/`counted_killed=True`，**最后一次 `--all` 要求 19 控制全绿 ＋ 23 变异全 killed**（CI 单份证据产物）；另加负锚「恢复旧次序 ⇒ P4 回到 assertion_not_matched」证明次序改动确是 kill 的原因；顺手 grep 全部 `expect_assertion_site` 是否指向真实断言行（**只报不改**）。

**在飞**：G62。**已收口**：G40–G61 全部派发单。**待派/待做**：`migrateRetiredDeliveries` O(capacity) 扫、notice 被拒通知永挂疑点、F3 按新锚集重做、`newestSessionCheckpointRef` 两步、godoc 重生成、暂存后全量 lint、R35 第二轮、F1 剩余覆盖、F4 归档（须用户明示授权）。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R236 ✅✅ 两道核心验收面闭合：`--all` 23/23 killed ＋ `dod_coverage 9/9 bound and proven`；十短已启动，六长入交接账（2026-11-18）

**✅ G62 收口，`--all` 终局达成**：`/tmp/g62c-mut-all/evidence.json` —— **`mutations` 23 条全 killed、`control` 19 条全 green、`control_tree_green=True`、`mode=live`、`replay=None`、`gate.ok=True`，门进程 exit 0**。**这是 CI 兼容的单份 mutation 证据产物。**
**✅ DoD 面彻底闭合（编排者亲跑）**：`python3 scripts/gates/check_dod_coverage.py --mutation-evidence /tmp/g62c-mut-all` ⇒ **EXIT=0，`dod_coverage: 9/9 criteria bound and proven`**。**历程：13 gap → 9 → 1 → 0。**
**两条修正的实测确证**：
- **P4**：walk 计数面整块（注释＋两 const＋参考代非零守卫＋循环，30 行）从 957-984 移到 **910-939**，主题 Fatalf 现 **:927**；墙钟两守卫**原样保留**（`:941-952`／`:954-965`，仍 2×）只是后判。依赖核查：块只用 `observed/series/generations`（`:890-892` 已绑定），局部变量无交叉。单跑 live：`killed/named_assertion/hit=True/counted_killed=True/exit 1/ctg=True`，location `:927`，原文 `generation 2 named 96 index entr(ies) … beside the 45 … over the bound 90 … the walk follows the history …`。
- **EVID-02 根因比「site 漂移」更深**：旧 site `#runDuplicateNoticeMaterial` 属 **`TestEventIntegrityReproduction`**，而选择器 `^TestEventIntegrityTaskContinuation$` **从不跑它** ⇒ **结构上不可能匹配**（又一次「反例路径与锚输入集不相交」，这回在注册侧）。改指 `#continuationSingleCopy`（拥有该字面量的助手；`t.Helper()` 下 go test 打印**调用方行**，门解析出 19 个可打印位置含最早 firing 的 `:1202`）。单跑 live 同样全字段 killed。
**✅ 负锚诚实且反证有力**：恢复旧次序 ⇒ **3/3 轮均在 :931 墙钟先 abort、无 walk 记录 → not_killed**。**G62 另附记：另一次探针墙钟未破界、:974 命中** ⇒ **墙钟是噪声聚合量、不能靠它检出该缺陷**，这恰好**加强**了「计数面先判」的裁定理由。主树 md5 前后一致（测试 `7e0dfd62…`／`run.go b318b5b7…`／门 `85fa7882…`）。
**验证面**：build/vet 0；selftest **54 OK**（复跑仍 54）；定向锚 ×3 全 0；**assembly 0 FAIL（251.8s）**；**host 146 PASS／0 FAIL（197.2s）**；comment_policy **0**；package-layout **814 files EXIT 0**。
**⚠️ G62 只报不改的注册审计**：23 项 shape 全合法、函数均可达选择器；两处无害宽度——MUT-EVID-03 的 `:4052` 属选择器外的 `TestEventIntegrityDecisionFlowMatrix`；helper 型 site 含助手自身 Fatalf 行（EVID-02 `:1104`、EVID-03 `:967`），`t.Helper` 下永不打印。**不影响判别力，登记不修。**

**✅ F2 剩余面已勘成确切命令（编排者读 `.github/workflows/ci.yml`）**：
- **十短**（`ci.yml:229/250`）：`python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat-index N --out <dir>`，N=1..10，`-count=1 -race -json`。**已启动**（编排者顺序跑，出 `/tmp/g63-bundle/short-1..10`，日志 `/tmp/g63-short.log` 逐份记 `SHORT_${i}_EXIT`）。
- **六长**（`ci.yml:325`）：`--scope event-long --seed S --window W --repeat 1 --timeout 6270 --generation-logs <dir>/generations --out <dir>`，S∈{1,7,42}×W∈{256,32768}。**每份 timeout 6270s（约 104 分钟）** ⇒ 按 R32「长跑单阻塞」不在本轮阻塞，**入交接账**。
- **bundle 门**（`ci.yml:408`）：`python3 scripts/gates/check_runtime_evidence.py --bundle <bundle> --contract golden/runtime/event_integrity_scenarios.json --json`，**须收齐十短＋六长＋mutations 一份共 17 份**（`--bundle` 硬判 mutations 恰 1 份，R214 立案）。mutations 那份已由 `/tmp/g62c-mut-all` 提供。

**🧭 交接账（冷启动读法：R236 → R235 → R233 → R230）**
**已闭合（有今日凭据）**：中心论点关键路径（只给键的新导出面＋统一 boot 采纳，N 10→1、读 85→30、两面收紧 252 代恒 31）／`--all` 23/23 killed／**dod 9/9**／layout 0（814 文件）／comment_policy 0／gofmt 0／assembly 0 FAIL／host 146 全绿／history 两控制绿／order.log 静默错读消除／attention 门控第二真相消除＋清除效果补锚（M2/M2'/M2a 三组各有判别力）。
**待做（逐条可执行）**：①十短读数复核（在跑）②**六长 6 份**（命令见上，长跑）③bundle 门收 17 份④**godoc 重生成**（`gen_godoc.sh --check` 现态红，落后 governance/task/recall/ledger 四包；编排者单写）⑤**暂存后全量 `bash scripts/lint.sh`**（8 个删除＋若干新文件未入索引，`tracked-hygiene` 须暂存后才可能绿）⑥**F3 按新锚集重做**（旧凭据三锚两机制全零命中；新落点见 R219；须回滚证红→复原证绿两次证据）⑦**R35 第二轮独立放行审**（须专项核 R230 立案的 `rebind ≥ 0` 守卫改动是否掩盖真重叠）⑧`migrateRetiredDeliveries`（`assembly/host_delivery.go:2165`）O(capacity) 全表扫⑨notice「被拒通知永挂」疑点（`MemoryCommit.owed` 无删除点、refuse 只写 `mailbox.go:137`、criterion 按 Refused 过滤）⑩`newestSessionCheckpointRef` 两步（先迁锚到生产路径 `restoreSessionCheckpoints` 再删）⑪「反思投递未读则随工作结束清」疑空条款（另契约面裁）⑫派生载体写面 rebuild 静默（槽表与 order 共有）⑬F1 剩余覆盖⑭**F4 归档＋commit/push 须用户明示授权**。
**正式口径（不得夸大）**：**可移除的历史依赖已全部消除，剩余增长是已声明的本征项（开档全 log CRC），锚已证明不存在未声明的增长；深度 252 墙钟仍约 2.2×，不声称有界。**

**在飞**：十短顺序跑（编排者直跑）。**已收口**：G40–G62 全部派发单。**目标未达成——F2 尚缺六长＋bundle＋全量 lint，F3/F4 未做，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R237 ✅ 十短 10/10 绿；godoc 重生成归绿（四包陈旧已清）；六长首份探针在跑（2026-11-18）

**✅ 十短全绿（编排者直跑，逐份记退出码）**：`SHORT_1..10_EXIT` **全 0**，`/tmp/g63-bundle/short-1..10` 各一份 `evidence.json`，**共 10 份**。命令即 `ci.yml:250` 原形（`--scope event-short --repeat-index N`）。
**✅ mutations 那份已并入 bundle**：`cp -r /tmp/g62c-mut-all /tmp/g63-bundle/mutations` ⇒ bundle 现有 **11 份** evidence，尚缺六长。
**✅ godoc 重生成归绿（编排者单写面）**：`bash scripts/gates/gen_godoc.sh` ⇒ **EXIT=0，wrote 83 files to docs/api**；`--check` ⇒ **EXIT=0，`docs/api matches the source (82 packages)`**。**R230 登记的「落后 governance/task/recall/ledger 四包」已清。**
**连带三门复测全 0**：`check_doc_budget` **0**、`check_doc_links` **0**、`check_vocabulary_alignment` **0**（`go_files=816 api_docs=83 violations=0 carrier=0 retired-word=0`；go 文件数 825→816 与镜像合并一致）。
**⏳ 六长首份探针在跑**（seed 1 / window 256，`--timeout 6270`），用于**实测真实耗时**再定其余五份排法。**在跑期间禁改任何 Go 源文件**——`run_runtime_scenarios.py` 就地跑 `go test`，改源会使读数非权威（同 R234 自认的次序纪律）。故 `migrateRetiredDeliveries` 等生产修复**待长跑收口后再派**。

**在飞**：六长首份探针。**已收口**：G40–G62 全部派发单。**F2 进度**：mutations 单份 ✅／dod 9/9 ✅／十短 ✅／godoc ✅／六长 ⏳／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R238 🔴 六长首份探针红，查出疑似严重正确性缺陷：close 撞预算后 boot 报「已提交事件缺正文」；派 G63 定归属（2026-11-18）

**🔴 阻断红（编排者直跑，非代理转述）**：`--scope event-long --seed 1 --window 256 --repeat 1 --timeout 6270` ⇒ **EXIT=1，`repeats: passed=0/1`，`verdict=unattested`**。用例是 **`wechat-bot::.::TestEventIntegrityLongRun`**（examples/wechat-bot 模块 `smoke_test.go`）。**实测耗时仅 35.2s**（远小于 6270s ceiling）⇒ **不是超时，是真失败**。
**子进程失败链（逐字，取自 `/tmp/g63-bundle/long-s1-w256/repeat-1/test-log.jsonl`）**：
```
tagent: boot seated the task account tasks=7 tail=9 reads=5 keys=29
tagent: restore folded chains=7 links=13 covered=0 recovered=7 dropped=0 elapsed=103µs
tagent: the close reached its budget before the final checkpoint was filed: context deadline exceeded
smoke_test.go:3606: boot the host: memory: committed event body is missing: 65-f4b92fc8af0820525a877e8acb667527
--- FAIL: TestEventIntegrityLongRun (5.54s)
```
外层：`smoke_test.go:3613: calibration generation 9 never became ready: … liveness=child-exit: the generation exited before it reported ready: exit status 1`。
**第二独立问题**：`durable_scan: unattested logs=0 generation log directory … holds no logs/ subdirectory`。

**为何这条红严重**：该仓硬契约 3 与 §3.2——一条事实经 **`ledger.CommitFact`（`segment_store.go:765`）一次批同写原文族与事件族** ⇒ **已提交的事件不应缺正文**。故 `committed event body is missing` 只有两种解释：
- **(甲) 真损坏**：某写入路径仍「事件已提交而正文未落」，或 close 撞预算留半批 ⇒ **原子性漏洞**。
- **(乙) 假拒绝**：正文在日志里但**读路径找不到** ⇒ 本变更读侧改动引入的缺陷。
**注意 `covered=0`**：最终 checkpoint 未落 ⇒ boot 走**兜底全链重放**，该路径与常态路径不同，**正是懒水合/重述/键走查三处新改动的交汇面**。

**✅ 派 G63（只读＋overlay）取四证**：
①**归属**（最重要）：HEAD 干净态是否也红——**禁 git 回滚主树**，用 `-overlay` 把三处改动覆盖成 `git show HEAD:<path>` 内容；若 HEAD 也红须再核 `origin/main` 有无该用例（**PR 早先提交的债仍是本 PR 的债**）；若 HEAD 绿则**逐处二分**定位。
②那个键的正文**字节级在不在**（`blob:`/`ref:` 是否存在、`journal.log` 有无其帧、槽表/order 是否指向它）⇒ 直接判甲/乙。
③close 撞预算语义 ＋ **枚举全部事件写入生产调用点**（`StoreEvent`/`Commit(`，逐个说明写哪些族），找不经 `CommitFact` 的路径。
④`logs=0` 是**子进程早退的下游症状**还是 **`--generation-logs` 跑法错**——**须分别判，不得混为一谈**。
**三处重点怀疑对象已写进令**：order.log 逐行校验和＋损坏重述（`seen` 复用是否跳键、重述后 span 是否指错）／只给键的提交序走查＋链顶发现改用它／懒水合（是否存在「事件族已读而原文族未水合」的窗口）。

**⚠️ 编排者纪律**：G63 在跑期间**禁改任何 Go 源文件**（它要跑用例与 overlay 对照）。`migrateRetiredDeliveries` 等生产修复待其收口后再派。

**在飞**：G63。**已收口**：G40–G62 全部派发单。**F2 进度**：mutations 单份 ✅／dod 9/9 ✅／十短 10/10 ✅／godoc ✅／**六长 🔴 首份红，阻断**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R239 🔴 G63 定案：真凶是编排者从未列出的第四处改动（`store:seat`），判定假拒绝——派生聚合成了第二真相；派 G64 两侧同批修（2026-11-18）

**✅ 归属已定：本变更引入，HEAD 无此红。** G63 用 `-overlay` 把 **304 个跟踪文件覆回 `git show HEAD:` 字节 ＋ 隐藏 20 个未跟踪 .go**（未用 worktree、未复制整树，主树零写）：
- **HEAD**：gen09 正常 ready（`gen09.json.ready` 存在），连续 **53 代** ready 齐备，全部子日志**零** `committed event body is missing`；其 `EXIT=1` 仅因探针设的 `-test.timeout=20m` 撞线（`panic: test timed out after 20m0s`），**非产品断言**。
- **工作区**：**3/3 复跑**均 EXIT=1，同一位置 gen09 boot，原文逐字一致。

**🔴 真凶＝第四处改动，编排者三处怀疑对象全部落空**：`memory/ledger/segment_store.go` 的 **`store:seat` 机制**（`storeSeat`／`withPartition`／`readSeat`／`Load`／`publishedPositionLocked`）。**HEAD 该文件无这些符号**；HEAD 的 `Load` 枚举 namespaces 建全量 `s.index[key]`，`GetEvent` 查表**与分区无关**。`assembly/checkpoint.go:1072 eachFactPastCut` 两版**逐字相同** ⇒ **回归全在 ledger 读侧**。
**三处嫌疑逐个排除（G63 给证据）**：order.log 校验和**未触发重述**（`addr.notice=""`，行解析正常）；`EachCommittedKeyNewestFirst` **仅服务 configChain**（`commit.go:224-225`，前缀 `tagent/config/v1/fact:`），事件读不用；懒水合在 blob/ref 面，**本红在其上游**。

**✅ 判定＝(乙) 假拒绝，正文确在日志（字节级证据 `/tmp/g63-probe/dump-65-07e3a0150f6826ea7f644357d366ac18.txt`）**：
- order line 849（seq 850）**一帧同写六键**：`358:evt:1794942655792:1` offset 3416084 len 5778、`358:idx:65-07e3…` offset 3421925 len 126、`358:meta:…`、`store:seat`、`blob:`、`ref:`。
- `slot[505] state=Held name="358:idx:65-07e3…" span={3421925,126} stated=true`；`journal.log` 4416781B `torn=false tornTail=0`。
- 但 **`s.partitions=[155 262]`** ⇒ `GetEvent` 只探 155/262 的 idx 键 → 均 held=false → `checkpoint.go:1093` 包成 `ErrSegmentBodyMissing`。**分区 358 从未被探。**
- **座位回退实证**：seq850 `count842 [262,358]` → seq851 `count842 [262]` → seq862 `count849 [155,262]`。**count 不增而分区丢失。**
- **触发链**：`plan.faults storage_deny@gen8 round24` → `open …/slots.stage: permission denied`（`gen08.log:691/695/701`）→ `grow` 失败 → `ErrCommitUncertain` → `CommitFact` 按契约**不发布内存态**，**但帧已落盘** → 下一笔用**旧内存态**重算座位并覆盖 ⇒ **永久抹掉 358**。
**(甲) 真损坏已排除**：事件族唯一写者 `segment_store.go:646-647 planEventLocked`；`StoreEvent`（**生产零调用方**，仅 `examples/contract/tool_recall/Table_example.go:26`）与 `CommitFact`（唯一生产调用方 `assembly/facts.go:393`）**都一帧同写 evt+idx+meta+seat**。其余生产 `Commit` 全是单键族或 blob/ref 或空批 barrier 或工具（`tier_facts.go:32/71`、`kv_original_store.go:235`、`segment_store.go:1243`、`migrate.go:100`、两 scripts）。

**🔴 勘误编排者 R238 两条前提**：
1. **「close 撞预算留半批」不成立**——该告警 gen00–08 零、仅 gen09 有，产生点 `checkpoint.go:705 fileClosingCheckpoint` 由 `host.go:897 abortBoot` 在 **Boot 已失败之后**调用 ⇒ **纯下游症状**。
2. **`logs=0` 是编排者跑法错，不是下游症状**：runner **只记录 env 不注入**（`run_go_test` 无 env 参数），harness 读 **`TAGENT_CALIBRATION_RUN_DIR`** 或默认 `$TMPDIR/event-consumption-integrity/03.11/run-s1-w256-<ts>/logs/`。真实目录 `…/run-s1-w256-20261118-024158` **有 gen00–gen09 十份日志**；对其调 `durable_scan_attestation` 得 **state=attested、五前缀计数齐**。⇒ **六长跑法须自己 export 该 env**，已写进 G64 令。
**另定位勘误**：`smoke_test.go:3606/3613` 是 `t.Helper()` 归因到调用行，真实产生点 `smoke_harness_test.go:1365` 与 `:3469`。

**✅ 裁定：两侧同一根轴——派生聚合不得成为第二真相。已派 G64。**
- **读侧（主修）**：`GetEvent` 不得以座位分区集为界。**关键观察：调用方已经知道分区**——`eachFactPastCut` 已持有 `ref.partition`，**把它传下去**（或增按分区直读入口），点读直接探该分区 idx 键、与索引走查同域；座位**只作加速**，缺分区**回退按 ref 分区直读，不得答 absent**。
- **写侧（同批）**：uncertain 后**不得用未发布内存态重算覆盖已落帧的座位**；更新须并入日志已陈述的分区（union，不丢分区）或从真相重述。
- **🔴 关键张力（已写进令，不得违反）**：座位分区集**存在的理由就是避免全 namespaces 枚举**（HEAD 那样是 O(capacity)，本变更刚消除）。**故禁取「删分区集、改回枚举」这条路**——那是把刚修的成本缺陷换回来。**正确解是「调用方自带分区 ⇒ 直读该分区」，零额外扫描、无第二真相。** 计量面须仍绿以证未换回扫描成本。
- **禁第三条路**：不得把 `ErrSegmentBodyMissing` 改静默跳过、不得让缺分区答「无此事件」、不得放宽 `CommitFact` 的 uncertain 语义。

**⚠️ G63 未查面（登记）**：HEAD 未跑完 121 代（gen53 之后未知）；未跑 `origin/main` 对照；未查 fsync 失败/磁盘满等其它 uncertain 成因是否同型；**未查座位回退是否污染 `EventCount`/`NewestOfType` 排序等下游**（⇒ 立案为 G64 之后的必查项）。

**在飞**：G64。**已收口**：G40–G63 全部派发单。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 10/10 ✅／godoc ✅／**六长 🔴 待 G64 修后重跑**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R240 ✅ G64 收口：座位第二真相两侧已修，长跑 121/121 代零「缺正文」；🔴 旧红遮蔽的另一条红浮出（facts 倒退），派 G65 定归属（2026-11-18）

**✅ G64 修法（两侧同一根轴，未换回扫描成本）**：
- **读侧**：新增按分区直读入口 **`GetEventInPartition`（`memory/ledger/segment_store.go:904`）**，直接探 `{partition}:idx:{key}`，**与索引走查同域、零集合依赖**；`GetEvent`（`:868`）保留座位为**加速器**。接线两处**已持分区**的生产调用点：`assembly/checkpoint.go:1088`（`eachFactPastCut`，生产链 `host_delivery.go:1822`，**每次 boot 恢复**）与 `:990`（`restoreSessionCheckpoint`）。
- **写侧**：**`seatOfCommitLocked`（`segment_store.go:693`，被 `:665` 调用）以 union 写座位**——分区＝已发布 ∪ 座位已陈述 ∪ 本帧；count/commitSeq 取二者较大者再进位 ⇒ **uncertain 落帧不再被覆盖抹掉**。
- 抽出 `storedAtLocked`（`:931`）供两读面共用，`EachEventOfPartitions` 的重复包裹随之删除（**错误文案逐字不变**）。
**✅ 两锚两组负锚，且互不误杀（判别力正确）**：`TestSegmentReadByPartitionAnswersWhatTheStoreNeverPublished`（`segment_store_test.go:1502`）／`TestSegmentSeatStatesThePartitionALandedCommitCouldNotPublish`（`:1536`）。**M1**（令 `GetEventInPartition` 回退为 `publishedPositionLocked`）⇒ `the read of the fact in partition 1 = held false, <nil>, want the body the unsettled commit landed`；**M2**（座位回到内存态覆盖）⇒ `the fresh open counts 1 event(s), want the two frames the log holds`。**M1 只杀①、M2 只杀②。**
**✅ 长跑原红转绿（核心验收）**：修前止于 gen09（`committed event body is missing: 65-f4b92fc8…`）；**修后 121/121 代日志齐备、全部 121 份零该错误**，`durable_scan: attested logs=121`。
**✅ 计量面全部仍钉住（证未换回扫描成本）**：`slotTableBytesScanned=[589824 0 0 0 0]`（钉零未动）、`integrityBytesScanned` == journalLogBytes 1:1、`indexWalk*=[0 4 4 4 4]/[0 252 252 260 260]/[0 4 4 4 4]`、**`addressedValueReads=[1 35 35 35 35]` 平坦**。
**验证面**：build/vet 0；ledger 0（73.8s）；**assembly 0 FAIL（240.8s）**；**host 146 PASS／0 FAIL（206s）**；history 0（15.4s）；两锚 PASS；comment_policy **0**；package-layout **0（814 文件）**。md5 四文件前后一致，主树零改（overlay 实验）。
**✅ 编排者亲收 godoc 级联**（G64 新增导出方法使其陈旧）：`gen_godoc.sh` **EXIT=0**、`--check` **EXIT=0（82 packages）**、doc_budget **0**、vocabulary **0**。

**🔴 旧红遮蔽的另一条红浮出（F2 现存唯一阻断）**：`smoke_test.go:3716: facts went backwards under the storage deny: 24→23`，采集于 **gen08 round24**（注入 `plan.faults storage_deny@gen8 round24`），故 `repeats: passed=0/1`。
**G64 的归属证据只到「修前修后同样红」**：修前同一 harness 目录 `…/run-s1-w256-20261118-024158/reports/gen08.json` 亦 `facts_before=24, facts_after=23` 逐字相同。**但两者都在同一未提交工作区 ⇒ 未证 HEAD 归属。编排者不采信「与本单无关」此外推。**
**✅ 派 G65 取四证**：①**HEAD 归属**（overlay 覆回全部跟踪文件＋隐藏全部未跟踪 .go，**须自己重新枚举不照抄 G63 的数字**；只需跑到 gen08 round24 即可判，如实报跑到第几代；若既有须再核 `origin/main`）②**采集口径**（读 harness 源码：数的是链上已提交事实总数，还是活窗口/投影条目数？**口径决定这条红是事实丢失还是窗口滑动合法**，不得凭「facts」一词猜）③deny 轮落帧与否＋**若还有另一读面在用未发布内存态则指名**（G64 已修座位 union，本红若仍在说明还有同根轴的第二处）④`restoreCheckpoint` 读权威闭包取证（见下）。

**⏸ 编排者待裁项（G64 正确停线，令要求「拿不到分区须停下报」）**：`assembly/checkpoint.go:757 restoreCheckpoint` 的 ref 由 `NewestOfType` 构造、**`partition` 为零值**（`:749`），故仍以座位集为界。两条候选闭包：①扩 `NewestOfType` 返回位置（含分区）②assembly 侧复用 `EachIndexNewestFirst` 取分区。**G65 须答**：`NewestOfType` 是否**已持有分区**（若持有，返回它是零成本非新能力）／改签名是否撞**导出面棘轮**（`exported_interfaces` 已 85/85 满，**方法签名改动 vs 新接口是否同一计数**须读判据确认）／其余 **14 个 `GetEvent` 调用点**（`run.go` 8、`read.go` 4、`facts.go` 2）**各自是否已持分区**——这决定「统一读权威」是否零成本可达。
**⚠️ G64 另报环境抖动**：`TestExecFaceWiredFromDocument`（`host_wiring_test.go:342`，tmux exec 面）一次 verbose 全量 FAIL，单跑与第二次全量均 PASS ⇒ 登记为抖动，非本单。

**在飞**：G65。**已收口**：G40–G64 全部派发单。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 10/10 ✅／godoc ✅／**六长 🔴 原红已绿、余 facts 倒退红待归属**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R241 G65 定案：facts 倒退红＝断言量错轴（活窗口计数当耐久总数），产品零丢失；勘误三处含编排者两处；派 G66／G67（2026-11-18）

**✅ HEAD 归属已定：本变更引入，HEAD 绿。** G65 用 `-overlay` 覆回 **230 个 M/D 跟踪文件 ＋ 隐藏 20 个未跟踪 .go**（实跑确认 `go test` 进程 env 携带 overlay），gen08 round24 得 **`facts_before=25, facts_after=30, outcome=answered`**（deny 下提交成功），跑到 **gen27** 未触发该 Fatalf；worktree 同点 **24→23 `failed_named`**。⇒ **无需核 `origin/main`**。
**🔴 勘误 G64「与本单无关、只是旧红不再遮蔽它」——错**（HEAD 绿，红由未提交工作区引入）。**但其「产品零丢失」的结论仍成立**，见下。

**✅ 裁定：红的是断言口径，不是产品。**
**采集口径（G65 读码核实）**：**`CommittedFacts`（`assembly/run.go:2056`）= `len(r.facts) - controlFacts`** —— **活窗口内存计数，不是链上总数**。每次成功提交后 `trimFacts`（`assembly/facts.go:131` → `trimFactsLocked:207` delete）**按投影窗口释放旧事实，耐久链不动**。
⇒ **24→23 是合法窗口滑动**，被释放事实**均已 `FactCommitted` 落盘**；G64 长跑 **121/121 代零 `committed event body is missing`** 佐证无丢失。
**deny 轮实况**：输出事实帧**已 append 落日志**，但 slot 表 grow 开 `slots.stage` 被 deny 的 0o500 目录拒 → `ErrCommitUncertain` → **未 publish**（`segment_store.go:850`、`journal_kv.go:210`）。产品 `failed_named` ＋ 零丢失，**合 `CommitFact` 契约**（uncertain 意为「只有未参与该次提交的一次开档能判落了什么」，落帧不发布是设计内）。
**🔴 勘误编排者令中证三假设**：我猜「-1 ＝ 读未发布内存态少 1」——**不准**。**未发布帧从未入 `r.facts`，故不减计数**；那 -1 实为**第 61/62 轮成功提交触发 trim 释放了 1 条已落盘旧事实**。
**🔴 勘误编排者 `GetEvent` 计数**：我写 run.go 8／read.go 4，**实为 run.go 7／read.go 5**（合 14）。
**根因（G65 锁定）**：本变更新增 **disk slot-table 索引**（HEAD 为内存 map ＋ JSON cut，`grep slots.stage|grow` 在 HEAD **零命中**），grow 须开新文件故被 deny 拒 → uncertain。**这是新机制的固有故障面，不是实现错误。**

**✅ 派 G66（断言改钉真付量，白名单只 `examples/wechat-bot/smoke_test.go`）**：该断言**意图对**（存储故障不得丢事实）但**代理量错**（用活窗口计数单调代替耐久链无丢失）。改钉**按 identity 读回全部已提交事实、正文逐字节相等**；**不得**只删比较或改 `>=`（那会丢掉被钉性质）；活窗口计数可留作**记录**但不再作判据；同批改附近头注。**核心验收＝长跑 `passed=1/1` ＋ `durable_scan attested logs=121`**，另须给「修前工作区行为下新断言绿」的读数以证这条红确是口径问题。**另明确要求区分 SKIP 与 PASS**（该包可能有需 API key 的 self-skip 用例）。
**✅ 派 G67（读权威统一，只做零成本可达者）**：
- **`NewestOfType` 内部已持分区**（`segment_store.go:1144`，闭包约 `:1156` 已握 `position.partition`）⇒ **返回它零成本、非新能力**；**改签名不撞棘轮**（`check_concept_budget.py:737` 只数**导出接口类型**，它是 `*FileSegmentStore` 具体方法、不在任何导出接口内、唯一生产调用方 `checkpoint.go:745`）。令其返回位置使 `restoreCheckpoint` 按分区直读。
- **9 处持分区的 `GetEvent` 迁到 `GetEventInPartition`**：`run.go` 2355/2760/2843（index-walk refs）、2974/3009（anchorRef 有 partition）；`facts.go` 370/405（owner 算 partition）；`read.go` 326/358（partition 参数/walk）。
- **4 处 identity 读面保留 `GetEvent`**（`run.go` 2325 `CommittedEvent` opaque id、2881 `restoreSurvivors` anchorEntry 无 partition 字段；`read.go` 173/201/235 query opaque key）——**它们真拿不到分区**，座位集是不做全扫的唯一索引，**G64 的 union 修好后座位不再丢分区故安全**。**🔴 须在 `GetEvent` doc 显式声明该边界**（持分区者 SHALL 用 `GetEventInPartition`），**不得假装读权威已完全统一**。**禁从 key 拼写解析分区**（该仓明禁拼写识别）。
**⏸ grow 鲁棒性（预分配/原地扩/未落帧先具名拒绝）列为可选改进，非本轮必修**：产品当下 `failed_named` ＋ 零丢失已合规；**when**：若 R35 独立审判其阻断则本轮补，否则归独立变更；**why**：uncertain 落帧是契约设计内行为，改它属契约面而非缺陷修复。

**在飞**：G66／G67。**已收口**：G40–G65 全部派发单。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 10/10 ✅／godoc ✅／**六长 ⏳ 待 G66 断言归正后重跑**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R242 G67 收口（8/9 迁移＋三变异全红新锚）；裁定其申报的行为变化为**缺陷修复**；派 G68 收三项（2026-11-18）

**✅ G67 交付**：`NewestOfType` 签名 `(ctx, typ) (event.EventID, bool, error)` → **`(event.EventID, int, bool, error)`**（`segment_store.go:1148`，分区随身份返回，内部闭包本已握 `position.partition` ⇒ 零成本）；唯一生产调用方 `checkpoint.go:746 newestCheckpointRef` 适配并构造 `anchorRef{key, partition}`，`restoreCheckpoint`（`:760`）改 `GetEventInPartition`（`:761`）——**G64 停线处已闭环**。
**已迁 8 处**：`run.go` 2355（eachCommittedEvent，`refs[i].partition`）／2760（锚点回放）／2843（seedConfigurationFacts）／2974（seedAccountFact，新增形参）；`facts.go` 371（persistFact 预检，owner→partition 上移至读前）／405（冲突重读）；`read.go` 326／358（partition 形参）。
**保留 identity 面**：`run.go` 2325（opaque id）／2881（anchorEntry 无分区字段）、`read.go` 173/201/235（query opaque key）。**`GetEvent` doc 已加边界原文**：`This read serves the callers an identity reaches without a partition: the seat is the only index such a caller has… A caller that holds the partition SHALL name it with GetEventInPartition`。**边界显式声明，未假装统一——符合裁定。**
**新锚** `checkpoint_test.go:334 TestRestoreReadsTheCutPastASeatThatOmitsItsPartition`：夹具**捕获当时座位值再写回**（不手写 JSON，避免把 `storeSeat` 线上形态复制进 assembly），**前置断言 `GetEvent`=absence 而 `GetEventInPartition(PA)`=命中**（夹具不自证）。**三变异全红**（M1 restore 回 `GetEvent`／M2 分区填 0／M3 `NewestOfType` 返 0），红原文同为 `the restore of the root = (held false, … memory: committed event body is missing: checkpoint:16:4)`。
**门态全绿**：build/vet 0；ledger ok 62.3s；assembly **0 FAIL** 234.4s；host **146 PASS/0 FAIL**；history 0 FAIL；两症状锚 PASS 且**计量面仍钉**（`slotTableBytesScanned=[589824 0 0 0 0]`、`addressedValueReads=[1 35 35 35 35]`、integrity 1:1；longrun ~230 代全程 slot=0／addressed 平 31／integrity 线性）；**concept_budget 0**（证未撞导出面棘轮）；**dead_producers 0（3471/3471）**；comment_policy 0；package-layout 0；runtime_architecture 0；assembly_symbols 0。md5 七文件前后一致。
**✅ 编排者亲收 godoc 级联**：`gen_godoc.sh` 0、`--check` **0（82 packages）**、doc_budget 0、doc_links 0。

**✅ 裁定一：`facts.go` 的行为变化是缺陷修复，采纳，不保留原宽免。**
G67 申报：两处迁移后，**同一 key 落在另一分区**时旧法「同字节→静默 nil、异字节→`ErrFactKeyConflict`」，现两式都由 `CommitFact` 权威拒 **`ErrContentConflict`**。
**理由（判据①③）**：分区由 `Owner.AgentID` 派生，**同 key 不同分区＝同一身份站了两个事实**；旧法「同字节就静默接受」等于**允许跨分区重复事实**（第二真相），正是本变更要消灭的形状。改由单一权威拒绝是必然。**⇒ 不是回归，是修掉一处静默接受。**
**✅ 编排者同批改文档真话**：`docs/wiki/memory/memory-architecture.md` §3.2 第 1 步已改为 `GetEventInPartition(partition, key)` 预检并写明「**同一 key 落在另一分区时预检不命中**，由第 3 步 `CommitFact` 权威判重具名拒绝（`ErrContentConflict`）——一个身份只站一个事实，跨分区同键不得被分区内的幂等判断静默放过」。
**⇒ G68 须补锚**：同 key 两分区提交，**字节相同与不同两形状都须具名拒绝并断言具体错误名**（不得只断言非 nil），负锚令预检跨分区可见或 `CommitFact` 放过同字节 ⇒ 必红。

**✅ 裁定二：第 9 处阻塞解白名单，但禁盲传 0。** `run.go:3009 replayRef` 的唯一测试调用点 `run_test.go:983` 已纳入 G68 白名单。**G67 建议「补一个 `0`」被驳回**——**零值分区正是本轮要修的缺陷形状**；G68 须读夹具确认事实真在哪个分区（给 owner→`PartitionIDFromName` 实算值），传那个真分区。

**✅ 裁定三：`restoreSurvivors` 残留边界先取证再定。** G67 实测座位缺分区时**整 boot 仍拒**（`run.go:2881 restoreSurvivors` 走 `GetEvent`，红于 `the checkpoint names a survivor the chain states no entry for: agent-a120000`）。**编排者判断**：G64 union 修好后座位自然不丢分区，G67 是**人为写回旧座位**才复现 ⇒ 只在人为回退下可达。**但「潜在红≠安全」**：旧 build 写的根、落帧与写座位之间崩溃都可能让座位陈旧。**令 G68 取证 `anchorEntry` 是否已携带足以得到分区的 typed 信息**（owner／address，**禁拼写解析**）：已携带则零成本迁移＋补锚；不携带则**停下报**并给 when/why，**不得为它新增 checkpoint 载荷字段**（改持久格式属另一裁定面）。**该残留是具名拒绝非静默丢失，故不修也不违安全性质，但不得静默略过。**

**在飞**：G66／G68。**已收口**：G40–G65＋G67。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 10/10 ✅／godoc ✅／**六长 ⏳ 待 G66**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R243 G66 撞预算但断言已落地；mtime 归属澄清两处「疑似越界」实为既有改动；派 G69 跑长跑验证（2026-11-18）

**✅ G66 撞 150 轮预算（非实现失败），编排者亲验现场**：断言改动**已落地**——`examples/wechat-bot/smoke_test.go` **+256 行**，新增 `smokeLongDenyKeptCommittedFacts`（约 `:3806`）与 `smokeLongDurableChainTexts`，新文案在 `:3832`。
**结构正确（编排者亲读）**：读 plan timeline 取 deny 轮**之前**该 session 的输入 → **两道非空转守卫**（`len(want)==0` 与 `identities==0` 各 Fatalf，文案「the deny round pins nothing」）→ 经存储层身份读取耐久链正文集合 → 逐条断言可读 → **活窗计数只 `t.Logf` 记录不作判据**。符合 R241 裁定。
**🔴 mtime 归属澄清（编排者差点误判越界）**：`git status` 另显 `smoke_harness_test.go`（4 行注释改）与 `skills/url-fetcher/TESTING.md`（26 行）为 M，**初看像 G66 越界**。`stat` 实测 mtime：`smoke_test.go` **1794946402**（最新，G66 所写）／`TESTING.md` **1794901565**（早 ~12.5h）／`smoke_harness_test.go` **1794774673**（早 ~48h）。**G66 只跑 26 分钟 ⇒ 后两者均为既有改动，非 G66 越界。G66 只写白名单内一文件。**「归因用 find -newermt／stat」纪律再次挡住误判。
**⚠️ 跑法陷阱登记**：`go vet ./examples/wechat-bot/` 在主 module 下报 **`main module does not contain package`** —— 该示例是**独立 module**，go 命令须在其目录内跑（或 `-C`）。**这是跑法问题不是代码问题。**
**⏸ G66 停在「更新临时探针以在真夹具上验证耐久链读机制」** ⇒ **长跑验证与负锚未做**。

**✅ 派 G69 接续**（白名单仍只 `smoke_test.go`，**明禁动那两个既有改动文件**，**禁重开 G66 的断言设计**）：
- **主任务**：长跑真跑（正确 env 跑法已写进令），要求 **`passed=1/1` ＋ `durable_scan attested logs=121`**；跑不完须如实报已完成代数。
- **负锚**：令 deny 轮前某条已提交事实在读回时丢失 ⇒ 新断言必红。
- **🔴 编排者查出一处潜在弱点并令其核**：新断言用 **`texts[text]` 正文集合成员判断**而非逐身份比对 ⇒ **若 plan 存在两条相同正文的输入，丢一条仍会绿**。令其核 plan timeline 输入正文是否互异：可能重复则**改逐身份比对**（属正确性修补，白名单内），互异则说明理由保留现形。
- 令其**分别给 PASS／SKIP／FAIL 计数**（该包有需 API key 的 self-skip 用例，不得把 SKIP 当绿）。
- **在飞提醒已写进令**：G68 在写 assembly／memory/ledger，撞其半成品编译错须如实报瞬态、不得代改，定向绿不授权全套件绿声称。

**在飞**：G68／G69。**已收口**：G40–G65＋G67（G66 撞预算，断言留用、验证待接续）。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 10/10 ✅／godoc ✅／**六长 ⏳ 断言已改待真跑**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R244 ✅✅ F2 长跑阻断已清：121 代跑满、三次独立绿、断言非空转已证；旧轴非确定性获反向印证；孤儿探针已清（2026-11-18）

**✅ G69 收口，长跑真跑达成**：
- **自跑**：`verdict: match`、**`repeats: passed=1/1`**、**`durable_scan: attested logs=121`**、`EXIT=0`、run_id `a577f4a8-53b2-4934-886e-6f97fe5285a1`、**`--- PASS: TestEventIntegrityLongRun (304.46s)`**，positive/negative/consumer 三子测全 pass。
- **继承 run3**（G66 于 04:27 已在飞，G69 守「不双跑」先继承再自跑）：同样 `passed=1/1`、`attested logs=121`、PASS 379.40s；run2 的 test-log 亦 `attested logs=121`。⇒ **三次独立绿**。
- **断言非空转已证（实测日志原文）**：`smoke_test.go:3717: [negative] smoke-chat-a holds 2213 identities; the 24 inputs driven before round 24 read back byte for byte after the storage deny`。
- **负锚必红**：`-overlay` 令 `smokeLongDurableChainTexts` 对 `SMOKE-R10000 ` 正文 `return nil` ⇒ `the input "SMOKE-R10000 business note" driven before the storage deny is not readable by identity after it: …`，`--- FAIL (357.08s)`，`NEG_EXIT=1`。主树 `smoke_test.go a18d7abb…` md5 前后一致，`smoke_harness_test.go 5af0b5c9…` 未动。
**🔴 旧轴非确定性获反向印证（决定性）**：**同一份代码三跑，活窗读数为 `24→23`、`24→23`、`24→33`（delta −1/−1/+9）** ⇒ 原断言钉活窗计数**构造上就是非确定的**，改钉耐久链是正确裁定。**这比「产品零丢失」更硬地证明了红在断言侧。**
**✅ 正文互异核查（编排者令其核的潜在弱点）**：run3 `plan.json` **1200 inputs / 1200 distinct**，单会话 300/300 无重复；生成式 `marker+" "+phrase`，marker=`SMOKE-R<seed><seq>` **全局唯一**；链上其余正文（`answer <m>`／`delegate ack <m>`／`LONG <m>`／tool_call content=""）无一与输入正文相等 ⇒ **集合成员判据与逐身份比对在本夹具等价**，故**不改**（亦守冻结设计）。**结论有据，非省事。**
**✅ wechat-bot 包计数（在其独立 module 目录内跑）**：`ok wechat-bot 7.743s`，**PASS 35（含子测 57）、SKIP 0、FAIL 0**，exit 0。**smoke 文件在 `//go:build smoke` 后**，合 `preflight:64` 口径 ⇒ 该包默认跑不含 smoke，长跑由 scenario runner 带 tag 驱动。
**其余门**：wechat-bot build/vet 0；主 module build 0；comment_policy **0**；package-layout **0（814 文件）**。

**✅ 编排者处置孤儿探针（G69 报，编排者核实后裁）**：`ps` 实证 **PID 50156** = `/var/folders/…/go-build2205109620/b001/wechat-bot.test -test.run=^(TestEventIntegrityLongRun)$ -test.count=1 -test.timeout=1h44m30s -seed=1 -window=256`，**PPID=1（孤儿）、已跑 58:50、仍有子进程 33978**。⇒ **本轮自己长跑探针的遗留**（早先某次 runner 父进程已死），非用户工作。**它在持续拉子进程污染墙钟读数**（本轮已两次观测并发假红）。**裁定：终止本轮自造实验遗留**，先子后父，`ps`/`pgrep` 复核**均已消失**。已完成的证据目录（`/tmp/g69-long/s1w256`、`/tmp/g66-long/run3`）不受影响。

**🧭 F2 剩余（阻断已清，只余机械收尾）**：①**六长其余五份**（seed×window 组合，命令见 R236；首份 s1w256 已绿）②**bundle 门收 17 份**（十短 10 ＋ 六长 6 ＋ mutations 1，mutations 那份在 `/tmp/g62c-mut-all`）③**暂存后全量 `bash scripts/lint.sh`**（8 个删除＋若干新文件未入索引，`tracked-hygiene` 须暂存后才可能绿）。
**其余待做**：F3 按新锚集重做（旧凭据三锚两机制全零命中，新落点见 R219）／R35 第二轮独立放行审（**须专项核 R230 立案的 `rebind ≥ 0` 守卫改动**）／`migrateRetiredDeliveries` O(capacity) 扫／notice「被拒通知永挂」疑点／`newestSessionCheckpointRef` 两步／「反思投递未读则随工作结束清」疑空条款（另契约面）／派生载体写面 rebuild 静默（槽表与 order 共有）／F1 剩余覆盖／**F4 归档＋commit/push 须用户明示授权**。

**在飞**：G68。**已收口**：G40–G65＋G67＋G69（G66 断言留用、验证由 G69 完成）。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 10/10 ✅／godoc ✅／**六长首份 ✅（余五份待跑）**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R245 ✅ G68 收口：读权威三项全闭（含残留边界）；发现未接线孤儿门；余五份长跑已启动（2026-11-18）

**✅ G68 三项全实施**：
- **第 9 处**：`replayRef` 加 `partition int` 形参、内部改 `GetEventInPartition`；两生产调用点传 `refs[i].partition`／`ref.partition`（`anchorRef` 本就携带）。**测试未传 0**：`TestReplayRefNamesAMissingBody` 先由 owner `agent-a` 提交事实，再用 `seqOfFact` 从索引读回落盘分区，**断言等于 `PartitionIDFromName("agent-a")`=16**（agent-b=201 互证），传该真值；缺失 key 仍具名 `ErrSegmentBodyMissing`、`Decoded=0`。**编排者「禁盲传 0」的要求被执行且给了实算凭据。**
- **第二项新锚** `TestFactCommitRefusesOneIdentityInTwoPartitions`（表驱动两形状）：均断言 **`errors.Is(err, ledger.ErrContentConflict)` 且非 `memory.ErrFactKeyConflict`**，并查 count=1、异分区无记录、重开 root 字节与原文未动。**如实声明一处结构限制**：`persistFact` 结构上不可表「同字节跨分区」（body 含 Owner），故该形状走 `ledger.CommitFact` 直入。**负锚 M1**（预检回 `GetEvent`）红：`the identity committed in partition 201 answered memory: the event key already names another fact: two-partition-identity, want memory: key already names other bytes`；**M2**（`planEventLocked` 去分区判据）红：`the commit of the same bytes in partition 201 answered 1, want the conflict the tier judges`。
- **✅ 第三项残留边界已闭（编排者 R242 立案的那条）**：`anchorEntry` 字段仅 `Key/Type/Summary/Content/Time`，**不含 owner/address/partition**；**但载荷所属链槽 `anchorSlot.partition` 是 typed 非拼写来源且即 survivor 事实分区**——分区由 `PartitionIDFromName(owner.AgentID)` 派生（`facts.go:370`）、product owner=链 agent（`compressProductEvent`）、投影片按 Address 键（`restoreProjectionSlice`）、`restoredSeedOf` 本就以 `PartitionIDFromName(addr.AgentID)==slot.partition` 取槽。⇒ 判「已携带」，**零成本迁到 `GetEventInPartition(survivor.Key, slot.partition)`，未加 checkpoint 载荷字段、未解析键拼写**。新锚 `TestRestoreHydratesSurvivorsPastASeatThatOmitsTheirPartition` 走**真 boot 入口**（restoreCheckpointState→seedAnchoredReplay，座位人为回退到只含 201）：boot 绿、Checkpoints=1、survivor 由分区 16 读回。**M3 负锚红原文 = G67 原症状** `assembly: the checkpoint names a survivor the chain states no entry for: agent-a120000`。
**门态全绿**：build/vet 0；ledger ok 64.1s；**assembly ok 223.0s／0 FAIL**；**host 146 PASS／0 FAIL**；history ok 16.8s；boot 成本锚 PASS（`slotTableBytesScanned=[589824 0 0 0 0]`、`addressedValueReads=[1 35 35 35 35]` 平、integrity 逐代 1:1）；midband 锚 PASS（14 entries／2 折产品／recall 228B）；**concept_budget 0（`exported_interfaces` 81/85）**；dead_producers 0（3471/3471）；comment_policy 0；package-layout 0（814 文件）。三测试各 ×3 全 PASS。md5 三文件前后一致。
**⚠️ 勘误一处旧读数**：G57 曾报 `exported_interfaces` **85/85 已满**并据此否掉「新立可选能力接口」；**G68 实测为 81/85**。**G57 当时的结论（取 C 案）仍成立**（C 案更省结构），但**其「结构上不可行」的理由须收窄为「当时读数如此，今日复核为 81/85」**——不得再引「85/85 满」作为硬阻断。
**✅ 追认 G68 自决两处**：①第三项按「链槽携带分区」判已携带而迁移（附反转探针 M3）；②`run_test.go` 首版越 1000 行结构门限（findings 68），把夹具 helper 挪入 `facts_test.go` 回落 999 行、findings 67（**等于其入场前状态**）。

**🔴 新发现：`check_runtime_structure.py` 是未接线的孤儿门。** 编排者全仓 grep（`.github/workflows/` 与 `scripts/` 递归）**零调用点**（除自身与其自测）；直跑 **EXIT=1，67 条 ERROR**，全为**既有体量债**（如 `api/config/decode.go: 845 lines > 500`、`assembly/checkpoint.go: 1310 lines > 500`、`assembly/checkpoint_test.go: 2676 lines > 1000`）。
**裁定：非 F2 阻断**（F2 ＝ CI 命令 exit 0，而它不在任何执行入口）。**立案两条**：①**门本身未接线**＝「anchor 为空＝没门在量它」的形态，须裁它该接进 lint 还是退役；②67 条体量债须核归属（HEAD/`origin/main` 是否已红）再定范围。**when**：本变更收尾 R35 独立审时一并裁；**why**：接线会立刻引入 67 条红，属另一契约面（体量棘轮）裁定，不得夹带。

**✅ 余五份长跑已启动**（编排者直跑，顺序）：(1,32768)／(7,256)／(7,32768)／(42,256)／(42,32768)，每份自 export `TAGENT_CALIBRATION_RUN_DIR` 且 `--generation-logs` 指同一目录，出 `/tmp/g63-bundle/long-s{S}-w{W}`，日志 `/tmp/g70-long.log` 逐份记 `LONG_S{S}_W{W}_EXIT`。首份 s1w256 的绿证据已从 `/tmp/g69-long/s1w256` 复制进 bundle（**替换掉早前那份红的**）。**bundle 现有：十短 10 ＋ mutations 1 ＋ 长 1 = 12 份，待补 5 份。**

**在飞**：余五份长跑（编排者直跑）。**已收口**：G40–G69 全部派发单（G54／G66 撞预算者均已由接续单完成）。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 ✅／godoc ✅／gofmt ✅／**六长 1/6 ✅ 余 5 在跑**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

**🔴 编排者跑法失误自认（zsh 分词陷阱第 N 次复现）**：首次启动余五份用 `for combo in "1 32768" …; do set -- $combo; s=$1; w=$2` —— **zsh 不对未加引号的变量做分词**，故 `s="1 32768"`、`w=""` ⇒ `--seed "1 32768" --window ""` 参数非法，**五份全 exit 2、一个都没跑**（bundle 仍 12 份、日志零 verdict 行）。**exit 2 是本地用法错，重试永不会成功**。**已改显式参数展开** `${pair%%:*}` / `${pair##*:}`（`for pair in 1:32768 7:256 …`）重launch，日志 `/tmp/g71-long.log`。**教训登记：循环里传多参数一律用显式分隔符＋参数展开，不靠 IFS 分词。**

### R246 ✅ 六长全部跑满 121 代：五份 attested，第六份撞轴（spec 要求零读 vs 守卫要求非零）；派 G70 让守卫分辨两成因（2026-11-18）

**✅ 六长矩阵实测（改跑法后全部真跑，每份 121 代跑满）**：
| seed/window | EXIT | repeats | durable_scan |
|---|---|---|---|
| 1/256 | 0 | passed=1/1 | attested logs=121 |
| 1/32768 | 0 | passed=1/1 | attested logs=121 |
| 7/256 | 0 | passed=1/1 | attested logs=121 |
| 7/32768 | 0 | passed=1/1 | attested logs=121 |
| 42/256 | 0 | passed=1/1 | attested logs=121 |
| **42/32768** | **1** | **passed=0/1** | **unattested logs=121**，reason=`durable load scan loaded zero keys for ['tagent/task/v1/spec:', 'tagent/task/v1/consume:', 'tagent/task/v1/fact:']` |
**bundle 现有 17 份**（十短 10 ＋ 六长 6 ＋ mutations 1），路径 `/tmp/g63-bundle`。

**🔴 定性：两条正当要求撞轴，不是产品缺陷。**
- **spec 要求零读**（编排者亲读原文）：`changes/05-task-custody/specs/checkpoint-state/spec.md:7`——「…**覆盖段 SHALL 零任务事实族前缀读**，尾段 SHALL 按任务身份读其链，恢复成本 SHALL 有界于存活账加切面后增量」；`:18` Scenario THEN 同。
- **守卫要求非零**：`scripts/scenarios/run_runtime_scenarios.py:939 durable_scan_attestation`，其 doc（`:944`）自述意图为「…or when a data-carrying prefix loaded zero keys (**the restart ran**…)」⇒ **它要防的是「扫描根本没跑／证据缺失」，不是要求产品必须读键**；判据在 `:976`。
⇒ **大 window 下 checkpoint 覆盖全部，boot 合法零前缀读，守卫却判 unattested。守卫的判据分不开「面没跑」与「面跑了且正确读零」两种成因。**
**✅ 裁定：让守卫二分，不得放宽成「零键一律放过」**（那会把真失效一起放过）。**这是「空/缺失多成因须各有判据」的又一实例**，与本轮 `ErrReadAfterClose`／`committed event body is missing` 同型。
**✅ 派 G70**（白名单只 runner ＋ 其 liveness 自测）：①先枚举可用正面信号（scan 行本身／`boot seated the task account` 行／checkpoint 覆盖声明行，须给 file:line 与真实日志原文）②改判据为二分：**无任何 scan 行/日志缺失/截断 ⇒ 仍 unattested**；**scan 行存在且 keys=0 且有正面信号证明走有界路径 ⇒ attested 并如实记录「有界路径零读」**；**拿不到区分信号则停下报，不得默认放过**③同批改 doc 与自测，**新增两案**（面没跑仍被拒／面跑了读零被接受），**不得删原负案**。
**验证面要求**：对**既有六份证据逐个重跑 attestation**（不重跑长跑）须**六份全 attested**；负锚取一份删掉 scan 行的副本 ⇒ **仍 unattested**；并跑 **bundle 门**（`check_runtime_evidence.py --bundle /tmp/g63-bundle --contract golden/runtime/event_integrity_scenarios.json --json`）给 EXIT 与错误清单。**禁重跑长跑**（证据已在，每份 300–380s）。

**在飞**：G70。**已收口**：G40–G69 全部派发单。**F2 进度**：mutations ✅／dod 9/9 ✅／十短 10/10 ✅／六长 6/6 跑满（5 attested，第 6 待守卫二分）／godoc ✅／gofmt ✅／**bundle 17 份已齐待门跑**／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R247 G70 收口（Python 守卫二分，六份 attestation 全绿）；🔴 同一撞轴 Go 侧还有第二处＋短十份指纹陈旧；派 G71 收口并刷新证据（2026-11-18）

**✅ G70 交付（二分严谨，拒绝力保住）**：
- **正面信号取自产品同源自述**：座账户行 `tagent: boot seated the task account tasks=N tail=T|none reads=R keys=K`（producer `assembly/checkpoint.go:855`，其 doc `:836-841` 明写「a seated account that read no key states zero, which is the checkpoint covering every task」）；路径语义 `tailFactKeys`（`checkpoint.go:880`）返 nil ⇒ 全量扫并写「the commit order states no framework checkpoint…」，非 nil ⇒ 切片座、族前缀零读 ＋ `readTaskLocked`（`runtime/state/durable.go:558`）逐任务读。
- **新判据三判**（`run_runtime_scenarios.py:169/946/960/969`）：缺段 ⇒ 红（原判据保留）／族前缀零且某代无座账户行或解释不了 ⇒ 红（reason 含 `zero keys` ＋文件名与行）／无任何一代 `tasks>0` ⇒ 红（「restarts proved nothing」保留）；通过则记 `zero_reads`（`:1027`）。
- **自测 9→13**（新增 4 案，**原 6 负案零改动，diff 只增不删**）；**四组变异**各自证拒绝力：全放过⇒005 红／删 `tasks>0`⇒007 红／不查座账户行⇒005 红／删整零分支⇒既有 `test_zero_data_prefix_is_red_and_named` 红。
- **六份 attestation 全 attested**；42/32768 那份三前缀各记 `{"bounded_seats":120,"empty_account_seats":1,"deepest_seat_keys":36}`——**如实记录有界路径零读，不是放宽**。
- **两负锚分辨两成因**：删 scan 行留座账户行 ⇒ `durable load scan is missing the prefix line(s) […]`；留 scan 行删座账户行 ⇒ `loaded zero keys for […] and the log of ['gen00.log'] states no seat that explains it`。

**🔴 G70 正确停线报出：F2 最后红未关，同一撞轴 Go 侧还有第二处。**
`examples/wechat-bot/smoke_test.go:3794-3797`：`counts[prefix] <= 0` ⇒ Fatalf `every durable load read 0 keys under …: the restarts proved nothing about that chain`。其计数在 `smoke_harness_test.go:3173` **按原始前缀归并**，**逐任务身份读落到 `…spec:task-<id>` 不计入族前缀**；而 `:3784-3792` **已单独判缺行** ⇒ **该零判据是纯冗余的轴要求**。红份实测：**族前缀非零 0 条、任务身份读 510 条**。
**✅ 裁定：该 Go 判据同样量错轴。** 有界路径的**真实读形状是逐任务读**，「族前缀零」不证明「面没跑」。令其钉「耐久载荷面确被行使」，接受**族前缀扫有键（兜底路径）∪ 逐任务身份读有键（有界路径）**；**二者皆零且无座账户行解释 ⇒ 仍具名拒绝**；缺行判据保留。**不得删断言、不得改 `>=0`、不得只删比较。** 实现取向二选一（改共享计数归并 vs 在 smoke 侧改合计判据），**优先取不动共享计数语义者**，两案都有级联则停下报。

**🔴 bundle 门暴露第二问题：短十份证据指纹陈旧。** 门 **EXIT=1，12 条**——11 条源于 42/32768 的 go test 失败；**1 条指纹不一致**：`wechat-bot` 模块两值，**短十份 `sha256:11c000903f…`** vs **长六份及当前树 `sha256:ba8a829604…`**（同 revision `9547faca`）⇒ **短十份是在 smoke_test.go 改动之前收的**。**这正是「证据产物须逐个消费者核形态＋digest 须一致」的实证。**
**✅ G71 第二任务＝刷新证据**：修好 Go 判据后**重收短十份**（当前树）、**重跑 42/32768**、复制其余五长＋mutations 进 `/tmp/g72-bundle`，**并比对五长指纹——若亦不符须一并重跑**（改了 smoke_test.go 很可能不符），**不得为消红改证据文件**；最后跑 bundle 门给 EXIT 与错误清单。**令中已写明循环传参禁 `set -- $var`（zsh 不分词）。**

**在飞**：G71。**已收口**：G40–G70 全部派发单。**F2 进度**：mutations ✅／dod 9/9 ✅／六长跑满 6/6＋attestation 6/6 ✅／**Go 侧轴判据待修**／**短十份待按当前树重收**／bundle ⏸／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R248 G71 收口：Go 侧轴红已收＋17 份证据全刷新；bundle 12→2 条，根因是我们自己改 facts.go 造成的变异锚陈旧（编排者漏写级联面）；派 G72（2026-11-18）

**✅ G71 判据改法取②（不动共享计数），级联枚举有据**：若改 `smoke_harness_test.go:3173` 的归并，**`missing`（靠键存在性判「行在不在」）会被连带改成「只有身份读也算行在」＝违反「缺行判据保留不动」**；该函数另有 `TestLongScanEvidenceNamesMissingPrefix` 与 python 守卫镜像两消费者。②的级联仅本 subtest。
**新判据**：subtest 体抽成 `smokeLongRestartEvidence(t, report)`，**缺行 Fatalf 原文逐字保留**；数据族须 `smokeLongScanKeysRead(counts, prefix) > 0`（**族前缀键 ＋ 以其为前缀的身份读键**）；仍零则须 seat 行满足「存在非空账户 seat」且「存在解释零读的 seat（`tail≠none` 或 `tasks=0 && tail=none`）」，否则具名拒绝 `every durable load read 0 keys under %s and no seat states the account that covers it: the restarts proved nothing about that chain`。**与 python 侧 `zero_read_is_explained` 对齐 ＋ 多一道「须有非空账户 seat」。**
**✅ 42/32768 转绿**：exit 0／verdict match／durable_scan attested／logs=121／280.3s，`restart-evidence` PASS。**旧红族前缀 max=0、身份读 102 行/族；新跑族前缀 55/54/109。**
**✅ 短十份逐份 EXIT=0、verdict=match。**
**🔴 指纹全不符 ⇒ 17 份全部重跑（G71 自决，编排者追认）**：旧短十 `wechat-bot=11c00090…`、旧六长 `ba8a8296…`、mutations `.=5d7058fe…`；**当前树 `.=d2854f00…`／`wechat-bot=eaac2923…`** ⇒ 无一相符。新六长 276–341s 全 attested 121。**s1-w256 前两次红（gen17 boot 60s 未就绪；positive `settle-ack SMOKE-R10197 x2`）第三次绿，均与判据无关**——如实报，编排者登记为**该夹具的启动就绪抖动**，须 R35 核其是否掩盖真问题。mutations `.` 漂移源于 02:22–04:47 他人主 module 改动。
**✅ 两负锚（`-overlay`，主树零写）**：零形状 ⇒ `every durable load read 0 keys under tagent/task/v1/spec: and no seat states the account that covers it…` FAIL；缺行 ⇒ `carry no durable load scan line for [tagent/task/v1/fact:] over 1 generations…` FAIL。md5 `smoke_test.go=25f7b5e9…`、`smoke_harness_test.go=5af0b5c9…` 前后 diff 空（**他人那 4 行注释未动**）。
**其余门**：wechat-bot build/vet 0；`go test ./... -count=1` 0（**PASS 58／SKIP 1=`cmd/bench-bridge` 无测试／FAIL 0**）；comment_policy 0；package-layout 0（814 文件）。**另报一条 HEAD 既有**：`go vet -tags smoke` 有 `smoke_test.go:2650 unreachable`（非本轮引入，登记）。

**🔴 bundle 门 12→2 条，根因是编排者漏写的级联面**：
两条均出自 mutations 证据（`the control tree must be green…`／`mutations must be a non-empty list`）。**根因**：**`MUT-CAND-01.8-storage-success: patch anchor occurs 0 times in assembly/facts.go`** —— 该文件已重构为 `ledger.CommitFact` 的 outcome switch，注册里 `default: return err …// sameFactOf` 锚文本消失。**这是本轮把 `facts.go:371/405` 从 `GetEvent` 迁到 `GetEventInPartition` 的级联**（G67/G68），**编排者未把变异登记表写进那两单的级联面 ⇒ 又一处「每收一单长出新单」的自造流程债，自认。**
**✅ 派 G72**（白名单只变异登记表＋其自测，**明禁为迎合旧锚而改产品代码**——锚跟随产品不是产品跟随锚）：①读全该条注册弄清其语义意图（storage success 路径）②在當前 `persistFact` outcome switch 找语义等价落点，old 逐字取自当前文件且唯一命中（python 计数实证）③new 须行为性破坏语义且可编译④`reason`/`expect_assertion`/site 若漂一并如实重指⑤**只改这条，其余 22 条一字不动**⑥**顺手扫其余 22 条 patch anchor 是否也 0 命中**（本轮改了 run/facts/checkpoint/read/segment_store，可能还有），列出全部并一并重指（每条给语义等价论证）。
**验证面要求**：`--all` 重跑须 **19 控制全绿＋23 变异全 killed**；**bundle 门目标 EXIT=0**；并给 **17 份指纹一致性汇总**（两模块各自单值）。

**在飞**：G72。**已收口**：G40–G71 全部派发单。**F2 进度**：dod 9/9 ✅／六长 6/6 attested ✅／短十 ✅／Go 轴判据 ✅／17 份证据已按当前树刷新 ✅／**mutations 锚待重指＋bundle 门待归零**／暂存后全量 lint ⏸。**目标未达成，不得归档不得提交。零 git 写，HEAD `9547faca`。**

### R249 ✅✅ F2 证据面全部归零：`--all` 23/23 killed ＋ **bundle 门 EXIT=0**（17 份／9 包 180 测试）；G72 纠正两处误判；全量 lint 在跑（2026-11-18）

**✅ G72 收口，F2 最后阻断已消**：
- **`--all` 终局**：**EXIT=0**、`mode=live`、`replay=null`、**`control_tree_green=true`、19/19 控制 green**（无 skipped 无 timeout）、**23/23 变异 killed**、`assertion_hit` 全 true、`pending=[]`、`not_evidence_backed=[]`、**`gate.ok=true`、`gate.failures=[]`**。01.8 读数 `verdict=killed`／`exit_code=1`／`assertion_locations=["event_integrity_test.go:4668"]`。
- **✅ bundle 门 EXIT=0**：`check_runtime_evidence.py --bundle /tmp/g72-bundle` ⇒ **`ok=true`、`errors=[]`**，counts **17 份（10 short ＋ 6 long ＋ 1 mutation）、9 包 180 测试**。**R214 立案的「CI 单份形态」阻断至此彻底闭合。**
- **指纹一致性**：17 份逐模块单值——`.`=`sha256:eaac2923…`、`examples/cli-host`=`sha256:e44c9ab7…`、`examples/wechat-bot`=`sha256:d2854f00…`，三者与当前树实算逐字相同；`source_digest` 复算 `sha256:f927af0e…` 与记录一致（**指纹只覆盖 `.go/.mod/.sum`，故改 `.py` 不漂**）。

**🔴 G72 纠正两处误判（编排者照抄过错，逐条勘误）**：
1. **G71「主锚消失」是误判**：`default: return err …// sameFactOf` 在当前 `assembly/facts.go:413-418` **命中 1 次、完好**。门对每条补丁各打一次同样文案，那两条重复消息来自 **companions[1]/[2]**。**真正陈旧的是两条读锚**（`GetEvent` 已改名 `GetEventInPartition`，`:371`／`:405`）。⇒ R248 记的「主锚文本消失」须按此勘误；**改动仅那两条 companion**，主锚与第三 companion（`if !held { return err }`）一字未动。
2. **编排者令中指纹映射写反**：我写 `.=d2854f00…`／`wechat-bot=eaac2923…`，**实为 `.=eaac2923…`／`wechat-bot=d2854f00…`**（G72 实算更正）。**教训：转述他人指纹读数须标明哪个模块，不得凭位置记忆对调。**
**✅ 语义等价论证**：迁移只改读的调用形态（加分区参数、改名），返回三元组 `(stored, held, err)` 与「拒绝即 error」语义不变；补丁仍落在同四个拒绝出口，缺陷主张（被拒段落答成 committed）不变。**可编译性先 `go build -overlay` 预验 `BUILD_EXIT=0`（主树零写）再经 `--all` 复证。**
**✅ 全 23 条扫描**：逐补丁对当前树计数，**非 1 命中仅那 2 条**，修后 `non-1 anchors: []` ⇒ **其余零漂移**。
**✅ 自测未改**：`Ran 54 tests, OK`。**改前那次显示 50 是夹具类 abort 少跑 4 条，非用例减少**——又一处「计数下降须核成因」的实证。
**✅ 追认 G72 自决两处**：①把主锚与两 companion 注释里陈旧的「the read that flushes staged writes」改为「the presence read under the fact's partition」（如实描述，非变更叙事）；②**一次纯注释换行后据 `source_digest` 判定证据会脱树，已原样回退**并复算 digest 与证据逐字一致——**正确纪律：指纹覆盖 .go，任何 .go 字节改动都会让 17 份证据脱树。**

**⏳ 全量 lint 在跑**（`bash scripts/lint.sh`，编排者直跑，日志 `/tmp/g73-lint.log`）。**已知预期红**：`tracked-hygiene` 须暂存后才可能绿（8 个删除＋若干新文件未入索引）。
**🛑 法定停点预告**：F2 的最后一项「**暂存后**全量 lint」需要 `git add`（index 写）。本轮全程守「零 git 写」，**暂存虽非 commit/push，但跨过该线须用户明示**。故编排者将先给**未暂存态的真实 lint 读数**，把 `tracked-hygiene` 的红具名列为暂存后待验项，**不擅自暂存**。

**在飞**：全量 lint（编排者直跑）。**已收口**：G40–G72 全部派发单。**F2 进度**：dod 9/9 ✅／`--all` 23/23 ✅／**bundle EXIT=0 ✅**／六长 6/6 attested ✅／短十 ✅／godoc ✅／gofmt ✅／comment_policy ✅／package-layout ✅／**全量 lint ⏳（tracked-hygiene 待暂存）**。**目标未达成——F3 未重做、R35 第二轮未派、F4 归档须授权。零 git 写，HEAD `9547faca`。**

### R250 🛑 全量 lint 读数取得：仅两条红，逐条归属清楚（一外来源、一暂存态），均非产品缺陷；法定停点，写完整交接账（2026-11-18）

**✅ 全量 `bash scripts/lint.sh` 实测 EXIT=1**（日志 `/tmp/g73-lint.log`）。**已过的门**（逐行核）：gofmt／modules／build／vet／comment policy (ratchet)／markdown doc budget (ratchet)。**红仅两处**：

**红一：checker selftests —— 外来源，不属本变更，禁触。**
`python3 -m unittest discover -s scripts/gates -p test_check_runtime_*.py` ⇒ **Ran 300 tests, FAILED (failures=1)**，唯一失败 `test_check_runtime_symptoms.SymptomCheckerTest.test_registry_matches_the_module`：模块实有 7 个 `TestLocalSmokeSymptom*` 锚而 `checker.SYMPTOM_PARENTS` 只列 6，多出 **`TestLocalSmokeSymptomBootDeployed`**。
**归属三条实证**：①`git status` 示 `scripts/gates/test_check_runtime_symptoms.py` 为 **`??` 未跟踪**；②`git ls-files scripts/gates/ | grep -c symptom` = **0** ⇒ **checker 与其自测全未跟踪**；③`TestLocalSmokeSymptomBootDeployed` 在 **HEAD 已有**（`git grep -c HEAD` = 2）且工作树同为 2 ⇒ **我们未动它**。
⇒ **该红由外来未跟踪工作（`runtime-architecture-convergence` 面）的注册表与树不同步造成**，且 `lint.sh` 的 discover 模式会把外来未跟踪自测扫进来。**裁定：不属本变更，禁触（全程禁触清单内），立案告知。** 这是「另一变更未跟踪工作引起门红不属本候选」的实证。

**红二：`tracked-hygiene` —— 暂存态红，须 index 写才能清。**
`go run ./scripts/codetools tracked-hygiene` ⇒ **EXIT=1**，`tracked-path-missing (the index holds a path the working tree does not)`，具名三条：`runtime/state/migrate.go`、`tests/integration/history/recover_kv_relocated_root_test.go`、`tests/integration/history/symptom_anchor_journal_test.go`（另有本轮镜像改名删除的 8 个测试文件同理）。
⇒ **正是 R230 预告的「删除未入索引」**。**清它须 `git add`（index 写）**。

**🛑 法定停点（R31「提交归档需明示」）**：本轮全程守 **零 git 写**。F2 最后一项「**暂存后**全量 lint」需 `git add`；**暂存虽非 commit/push，但跨过该线须用户明示授权**，编排者不擅自暂存。**两条红均已具名归属，无一是产品缺陷。**

---
## 🧭 完整交接账（冷启动读法：R250 → R249 → R246 → R241 → R236 → R230）

### 一、目标三句逐句审计（不得以「大部分门绿」当达成）
| 目标句 | 状态 | 证据 / 缺口 |
|---|---|---|
| ①整理计划、并行派发推进任务优雅完成 | **部分达成** | 计划三级结构＋ORCHESTRATION 台账 R0–R250 完整；G40–G72 共 30+ 单叶级并发派发全部收口（含两单撞预算由接续单完成）。**但 F3/F4 未做 ⇒ 「完成」未达成。** |
| ②review 已实现部分、核违规概念与词汇 | **达成** | `check_vocabulary_alignment` **0**（816 go／83 api／violations 0／carrier 0／retired-word 0）；`comment_policy` **0 finding 0 越基线**；本轮多处伪断言换真话（`host_attention` 门控注释、`pairedBootRatios` 注释、wiki §3.2／硬契约 3／缺口行、`GetEvent` 边界声明）。 |
| ③只考虑架构优雅纯粹性落实执行 | **实质达成，余项具名** | 见下「纯粹判据逐条」。 |

### 二、纯粹判据逐条（今日证据）
- **①链唯一真相**：✅ 座位/order 两个派生载体损坏均**从真相重述**而非拒开；`store:seat` 由读权威降为加速器；跨分区同键改由 `CommitFact` 单一权威拒绝。
- **②派生载体不迫使全量读**：✅ boot 槽表枚举 **0**、配置链 N **10→1**、链载点读 **85→30**、链顶发现只付 1 值；`addressedValueReads` 252 代恒 **31**。
- **③无第二真相**：✅ attention 门控读 close 同一 verdict（删逐 task 重算）；座位 union 不丢分区；`ErrReadAfterClose` 拒绝而非答「无此键」。
- **④更少特异化代码**：✅ 统一 boot 采纳（删「只有 Host 设钩子」）；只给键的走查**导出既有实现**而非新接口；`countedFace.step` 机制整条退役；14 处读点 9 迁 5 保留并**显式声明边界**。
- **⑤本征成本归索取者并显式声明**：✅ 开档 CRC 钉 **1:1 线性**、`slotScan` 钉零、`addressedValueReads` 钉平坦；锚判据原文证明「增长真实且**全部归因于已声明面**」。**正式口径：可移除项全消除，剩余是已声明本征 CRC，深度 252 墙钟仍约 2.2×，不声称有界。**

### 三、已闭合（今日凭据，可直接引用）
`--all` **EXIT=0／19 控制全绿／23 变异全 killed／gate.ok=true**（`/tmp/g72-bundle/mutations`）／**`dod_coverage 9/9 bound and proven`**／**bundle 门 EXIT=0**（17 份＝10 短＋6 长＋1 变异，9 包 180 测试，指纹逐模块单值、digest 复算一致）／六长 **6/6 跑满 121 代全 attested**／短十 **10/10**／assembly **0 FAIL**／host **146 PASS 0 FAIL**／history 0 FAIL／ledger ok／两症状锚 PASS 且计量面全钉／godoc `--check` **0**／gofmt **0**／comment_policy **0**／package-layout **0（814 文件）**／concept_budget **0**／dead_producers **0（3471/3471）**／doc_budget/doc_links/vocabulary **0**。

### 四、待做（逐条可执行，不缩 scope）
1. **🛑 须用户授权**：`git add`（清 tracked-hygiene）→ 暂存后重跑全量 lint → commit/push → **F4 openspec archive**。
2. **F3 按新锚集重做**：旧凭据三锚两机制全零命中（`TestJournalBytesBoundedByLiveSet`／`RetireFacts`／`relocatableStorage`）；新落点见 R219；须**回滚证红→复原证绿两次证据**；**须待树稳定**（复制整树类实验）。
3. **R35 第二轮独立放行审**，须专项核四项：①R230 立案的 `rebind ≥ 0` 守卫改「两侧均测得」是否掩盖真重叠 ②s1-w256 前两次跑的启动就绪抖动（第三次绿）③`go vet -tags smoke` 的 `smoke_test.go:2650 unreachable`（HEAD 既有）④G71 抽出的 `smokeLongRestartEvidence` 判别力。
4. **外来红立案告知**：`test_check_runtime_symptoms.py` 注册表缺 `TestLocalSmokeSymptomBootDeployed`（禁触，属 `runtime-architecture-convergence`）。
5. **未接线孤儿门**：`check_runtime_structure.py` 零调用点、67 条既有体量债 ⇒ 裁「接进 lint 还是退役」＋债的归属（另契约面）。
6. `migrateRetiredDeliveries`（`assembly/host_delivery.go:2165`）O(capacity) 全表扫。
7. notice「被拒通知永挂」疑点（`MemoryCommit.owed` 无删除点、refuse 只写 `mailbox.go:137`、criterion 按 Refused 过滤）。
8. `newestSessionCheckpointRef` 两步（先迁锚到生产路径 `restoreSessionCheckpoints` 再删）。
9. 「反思投递未读则随工作结束清」**疑空条款**（WHEN 实测不可达）⇒ 另契约面裁。
10. 派生载体**写面 rebuild 静默**（槽表与 order 共有）⇒ 共享缺口，R35 判是否阻断。
11. **F1 剩余覆盖**（合并覆盖 44% 之外的面）。
12. 四处 identity 读面（`run.go:2325/2881`、`read.go:173/201/235`）仍以座位为索引——**已在 `GetEvent` doc 显式声明边界**，非缺陷但是纯度余项。

**目标未达成。零 git 写，HEAD `9547faca`，工作树可编译、全部已接线门除上述两条外全绿。归档与提交待用户明示授权。**

### R251 停点只覆盖「暂存/提交/归档」，实施面继续推进：派 G73（06.R3 点/尾查询有界）与 G74（12.R1 设计门，唯一未启动域）（2026-11-18）

**✅ 次序裁定**：F2 停于「暂存须授权」，F3/F4 又依赖代码定型 ⇒ **先把剩余实施面做完，再做 F3／R35／F4**，否则验收证据会因后续改动反复过期（「验收锚旧绿不随上游改动延续」）。
**✅ 派 G73（06.R3）**：**「点查询与尾查询不得随历史增长」——无此叶则中心论点是假终点。** 六条交付：①**给走查 API 真正的早停出口**（今公开回调只返回 error、包装层恒继续、唯一停法是哨兵 `errStopIndexWalk` 且只有 `keptHotKeys` 用；**只改引擎不改出口，消费者仍会写成全走查**）⇒ 加继续/停止出口、迁全部包装层、**删哨兵停法零残留**；②`GetParent` 反向走到第一条同链条目即停，回退 `parentFromBodies` **不得再收集全分区 refs 逐条读正文**；③`eachFactPastCut` 从水位起走到尾即停（**今走查全史再过滤**）；④`newestSessionCheckpointRef`／`restoreSessionCheckpoints`／`seedAnchoredReplay` 走到最新框架检查点即停；⑤`QueryEvents` 按命名空间收窄，**不能收窄则在成本表如实声明 O(匹配集)**；⑥**成本表把「本征 O(历史)」与「可修项」分列**（按序导出全链/跨分区全量查询属本征；座位损坏重推与无切面整读属一次性具名降级）。
**验证面已含四枚拟新增锚的全名与 `-run` 正则**（含「须有随历史增长的控制案证明界可失败」「把早停出口去掉即红」两条负向要求）＋ 全门回归＋计量面读数。**级联面预先写进令**：godoc 申报／dead_producers／comment_policy 实跑／新测试文件镜像／成本表同批。
**✅ 派 G74（12.R1 设计门，12 域 0/3 唯一未启动）**：六项交付（J1/J2 判据与反事实、E12-1..8 复跑补证、三候选择一与被否理由、生命周期四态交接定义表、**两枚今天必须红的症状直译锚 S4/S5**、「error 被吞」站点按**结算/消费/投递完成三类语义单元**枚举而非代码形态扫）。**两锚钉的是尚未实现的行为，明令不得为让它们绿而改生产代码。** 新增两测试文件会命中 `package-layout` ⇒ **令其停下报豁免/改名建议，不得自行改门**。
**⚠️ 并发约束已写进令**：G74 只读 G73 在写的文件；撞半成品编译错报瞬态不得代改；**判据表由编排者合并，三域不得各写一版水位定义**（12.R1 与 05.R1／07.R1 共同产出 G2）。

**在飞**：G73／G74。**已收口**：G40–G72 全部派发单。**目标未达成——06.R3／12 域在实施，F2 待暂存授权，F3／R35／F4 待实施收口后做。零 git 写，HEAD `9547faca`。**

### R252 G74 收口：12 域设计门完成（取乙·应用失败拒绝切面），两枚症状锚今天真红；两枚门红待 G73 释放后并入镜像文件（2026-11-18）

**✅ G74 交付（12.R1 设计门，本叶**不勾**——设计门带待定项，正确纪律）**：
- **三候选择一＝乙·应用失败拒绝切面**；否甲（第二份「已应用到哪」＝**第二真相**＋仍停摆）、否丙（恢复期第二读路径，违同一派生）。落本域 `design.md` §十一。
- **生命周期四态定义表＋「具名拒绝并指出未完成项数」裁定**：挂载＝无需交接（pending 队列）；禁用＝交接或具名拒绝；卸载＝具名拒绝（≤未完成项数）；替换＝同切片名同一权威。取具名拒绝因**无承接者注册面、派生型切片无链上应用位置可回落**。
- **两枚症状直译锚今天真红（复跑命中 1、exit 1）**：S4 `the restarted consumer never applied the committed fact behind-1-2: the cut checkpoint:16:16 states the watermark 16:16, the boot folded 8 fact(s) and replayed behind-past-the-cut, yet behind-1-2 is neither in the restored payload nor in the replay`；S5 `the open obligation the-notice-still-owed vanished with the unmount… second cut … states [compress inputs mailbox projection tasks]… naming no pending slice`；负锚 `…reported by no line… still owes 1 obligation(s), and Wrapper.execute returned bare — 0 error line(s)`。
- **「error 被吞」按三类语义单元枚举**：单元一（结算）**2 真吞**（`wrapper.go:312-314` 及前置两处）；单元二（消费）**1 真吞**（`facts.go:152-154`，与恢复期上抛构成 **C6 不对称**）；单元三（投递完成）**0 真吞**（`host_delivery.go` 20 处 warn＋2 处 error 全「具名＋保留」）。**新增面 `settle_turn.go:522-598` 复用单元三模板、`consumer.go` 26 处裸 return 全为守卫、`denial.go`/`journal.go` 无吞点 ⇒ 3 万行增量未新增第四类真吞点。**
- **E12-1..8 逐项实测复核**，含此前未核四项的结论（主树实测真丢／仍不可测／实测具名拒绝绿）。**新增勘误**：`plugin_mount_test.go` 已删，三个声明面测试居 **`plugin_catalog_test.go:106/:144/:156`**（与 R221 映射表一致）。
**✅ 追认其自决**：未按白名单新建 `runtime/task/consume_failure_anchor_test.go`——**冻结名已存于 `unsettled_test.go:292`**，同名新建会整包编译失败；命令与测试全名逐字未变、命中 1、红因不变。
**消费者锚 3/3 PASS、跨会话原验收 2/2 PASS、声明面 3/3 PASS、build/vet 0。**
**⚠️ 在飞瞬态处置正确**：`read.go:5` 与 `checkpoint.go:1081` 两度半成品编译错（G73 在写），**未代改、自愈后全量复跑一致、不声明全套件绿**。

**⏸ 两枚门红待裁（G74 正确停线未自处置）**：`package-layout` 全库唯一 finding＝`consumer_lifecycle_anchor_test.go: test-mirror-missing`；`comment_policy` 同文件 `responsibility-fragmentation`。G74 建议二选一：**并入 `assembly/checkpoint_test.go`（零豁免）** 或 `mirrorExemptFiles`＋基线槽位（两处禁写）。
**✅ 编排者裁定：取并入镜像文件**（零新结构，判据④）。**⏸ 触碰集待释放**：`assembly/checkpoint_test.go` 在 **G73 白名单内** ⇒ **待 G73 收口后由编排者亲收或另派小单**。**若届时发现该锚主题确跨文件（assembly＋runtime/task＋runtime/state）而并入会让文件名说错主题，再改判豁免**——判据以「主题是否成一面」为准，不以省事为准。

**在飞**：G73。**已收口**：G40–G72＋G74。**目标未达成。零 git 写，HEAD `9547faca`。**

### R253 ✅ 用户授权停点推进（原话「我授权卡点推进」，2026-11-18）；次序裁定：暂存须待 G73 收口，不得把半改态入索引

**✅ 授权溯源**：用户于 2026-11-18 对 R250 所报停点（`git add` 暂存 → 全量 lint → commit/push → F4 archive）答复 **「我授权卡点推进」**。**该答复覆盖该停点全部四项**（暂存／全量 lint 复跑／提交推送／归档），不再逐项请示。**归档仍须走 `openspec archive` 正常流程与 `scripts/gates/check-openspec.sh` 门禁，不得用 `--skip-specs`／`--no-validate` 绕过**（本仓 `openspec/agents.md` 红线）。

**✅ 次序裁定（防止授权后立即犯新错）**：**G73（06.R3）仍在写** `memory/ledger/segment_store.go` 与 `assembly/{read,checkpoint,run}.go` 及若干 assembly 测试。**此刻 `git add` 会把半改态入索引** ⇒ 故**暂存排在 G73 收口之后**，不是收到授权就立即暂存。
**收口后的执行序列**（逐步，每步独立验证，**build/test 与 commit 永不串联**）：
1. 编排者复核 G73 收口战报＋亲测 build／两症状锚／两门。
2. **亲收 G74 留下的两枚门红**：把 `assembly/consumer_lifecycle_anchor_test.go` 并入 `assembly/checkpoint_test.go`（零豁免，判据④）；若读后发现其主题确跨文件成一面则改判具名豁免。
3. `git add`（**逐文件具名 add，不用 `-A`／`.`**，排除外来未跟踪文件与 `.wechat-config/data`）。
4. 重跑全量 `bash scripts/lint.sh` ⇒ 目标仅剩「外来未跟踪自测」那一条已知红（**该红不属本变更**，须在提交说明里如实注明）。
5. **F3 按新锚集重做**（回滚证红→复原证绿两次证据，树须稳定）。
6. **R35 第二轮独立放行审**（含 R250 列的四项专项复核）。
7. `git commit`／push；**F4 `openspec archive`** ＋ 归档后复跑 `check-openspec.sh`。

**在飞**：G73。**已收口**：G40–G72＋G74。**目标未达成。零 git 写（待 G73 收口后按上述序列开始），HEAD `9547faca`。**

### R254 G73 撞预算但实施已落地（编排者亲验）；亲收 G74 两枚门红（并入镜像文件，锚仍红）；godoc 再归绿（2026-11-18）

**✅ G73 撞 150 轮预算（末句停在写 `tool/recall/trace_test.go`），编排者亲验：实施其实已落地**。
- `go build ./...` **0**。
- **哨兵停法零残留**：`errStopIndexWalk` 在 `memory/ledger/segment_store.go` 与 `assembly/*.go` **零命中** ⇒ 早停出口改造完成且旧机制删净。
- **四枚拟新增锚全部建立且 PASS**：`TestParentLookupCostsItsOwnChainNotTheHistory`／`TestTailWalkStopsAtTheWatermarkAndTheTailEnd`／`TestAPointQueryThatWalksTheWholeHistoryIsRed`（assembly 三锚，`-run` 并集实跑 **3 PASS／0 FAIL**）＋ `TestTraceOfDepthDPaysDLocalLookupsNotDHistoryWalks`（**`tool/recall` 包内**实跑 **1 PASS**）。
- **concept_budget 0**（未撞导出面棘轮）、**dead_producers 0（3473/3473）**。
**⏸ 撞预算未做完的**：负锚的 overlay 实证、全量回归、成本表核验 ⇒ 编排者接续核验中。

**✅ 编排者亲收 G74 的两枚门红（取并入镜像文件，零豁免）**：
- 读全 `assembly/consumer_lifecycle_anchor_test.go`（279 行，两枚锚 S4/S5＋helper），**先逐符号核碰撞**（`refusingConsumer`／`owedSlice`／`sliceContains` 等在 `checkpoint_test.go` 均 0 定义；`foldRecorder`/`windowChainRun` 等是包内他文件定义的既有 helper，可直接复用）＋**核其 12 个 import 在目标文件已全部存在**。
- `tail -n +20` 按字节追加（跳过文件级注释与 package/import 头），**两枚锚各带自己的 `契约:` 行**故文件级那条冗余丢弃。删源文件。
- **实证**：`checkpoint_test.go` 内两锚各 1 处；源文件不存在；build **0**、vet **0**；**两锚仍红**（`--- FAIL` 2 条，S4 原文 `the restarted consumer never applied the committed fact behind-1-2: the cut checkpoint:16:16 states the watermark 16:16 …`，现位于 `checkpoint_test.go:2900`）⇒ **合并没有让红变绿，也没有改变红因**。
- **两门归绿**：comment_policy **0 finding**、package-layout **0（815 文件）**。
**⚠️ 登记：这两枚锚是「钉尚未实现行为」的**有意红**（12.R1 设计门产物，实现属 12.R2）⇒ 此后 assembly 全量会带这两条预期红，**不得为消红而删锚或改生产代码**。

**✅ godoc 再归绿**（G73 改 `segment_store.go` 使其陈旧）：`gen_godoc.sh` **0**、`--check` **0（82 packages）**。

**在飞**：三包全量回归（编排者直跑）。**已收口**：G40–G74 全部派发单。**目标未达成。零 git 写，HEAD `9547faca`。**

### R255 G73 成本表已落（本征/可修分列＋如实声明点查询轴不可全有界）；编排者亲收一条控制案失校（深度拉开，三次稳健证成）（2026-11-18）

**✅ G73 的成本表已落本域 design.md**（编排者亲读）：
- `:1726` 跨分区全量查询（调用方显式命名**全部**分区）＝**本征 O(历史)**；空授权集答空答案零读，全量只出现在调用方显式枚举全部。
- `:1727` 席位重推＝**一次性具名降级**；`:1728` 无切面整读＝**一次性具名降级**；`:1729` 兜底物化（引擎不声明序能力）＝本征现布局。
- **`:1733` 一条重要的诚实声明**：交付②字面「从该键位置反向走」**在现布局不可达**——引擎只声明「从最新提交倒走」的序（`CommitOrder`／`indexKeyOrder`），**没有按键起点寻址**。落地的是可达等价形：**成本＝该键到同链前驱之间的提交数，不随前驱之后（更旧）的历史增长**；「链根无前驱」因此是本征整读，**已单列一行并把「引擎起点寻址」登记为修复面**，**不声称点查询全轴有界**。**编排者追认该收窄**——它没有为迎合叶面文字而假装达成。

**🔴 三包回归暴露一条非预期红，编排者亲收**：
`TestStoreSeatIsReadWithOnePointReadAtOpen/the_aggregate_a_derived_count_pays_grows_with_the_chain`（`segment_store_test.go:1004`）：`the open that derives the count paid [689.25µs 1.159625ms 1.198667ms] over the depths [40 160 640], want the deepest to cost more than twice the shallowest`。
**定性**：这是**「证明界可失败」的控制案自己没证成**。锚为**本轮新增**（`git grep HEAD` 零命中）。深度 40/160/640 下**亚毫秒量级**的固定开档开销压过边际成本，最深仅最浅 **1.74× < 2×**；而**计数面（枚举条目随链增长）是过的**。
**处置（不凑读数、不删判据）**：**只把该子测的深度拉开**到 `100/800/3200`，**两条断言（墙钟 2× 与枚举增长）都保留**。实测 `-count=3`：三次分别为 **686µs→11.17ms（16.3×）／317µs→6.98ms（22×）／603µs→10.48ms（17.4×）**，**全远超 2×、EXIT=0、0 FAIL** ⇒ 控制案现在**稳健**证成该界可失败。
**教训登记**：本轮反复出现「墙钟在亚毫秒量级不可靠」与「计数面才是决定性证据」；**控制案的深度须拉开到边际成本压过固定开销**，否则证明不了界可失败。

**在飞**：三包全量回归（复跑确认只剩两条有意红）。**已收口**：G40–G74 全部派发单。**目标未达成。零 git 写，HEAD `9547faca`。**

### R256 ✅ 三包回归只剩两条有意红；按用户授权暂存（逐路径排除外来件），全量 lint 复跑中（2026-11-18）

**✅ 三包回归终态**（`/tmp/g77-regress.log`）：`memory/ledger` **ok 105.3s**、`tool/recall` **ok 0.567s**、`assembly` FAIL **仅两条**——`TestConsumerBehindTheChainSurvivesARestart` 与 `TestUnmountKeepsTheOwedItemsOfItsSlice`，**即 12 域那两枚有意红**（钉未实现行为，实现属 12.R2）。**无非预期红。**

**✅ 按 R253 授权执行暂存（用户原话「我授权卡点推进」）**：
- **先看全量变更形态**：187 M／23 D／43 ??，其中 **31 R ＋ 43 RM ＋ 1 RD 说明大量重命名早已在索引**。
- **逐路径排除 6 条外来/他变更件**：`examples/wechat-bot/skills/interview/agent-interview-helper/`、`openspec/changes/runtime-architecture-convergence/`、`openspec/changes/no-prefix-criterion/`（**另一活跃变更，夹带禁**）、`scripts/gates/check_event_semantics_symptoms.py`、`scripts/gates/runtime_budgets.py`、`scripts/gates/test_check_runtime_symptoms.py`。
- **暂存前扫雷**：对 37 条待纳入路径逐个查 **>200KB 大文件**与**可执行位** ⇒ **均无**（早前那两个 14.5MB 二进制已由 `.gitignore` 的 `/fold-cold-root`／`/recover-kv` 排除）。
- `git add -u` ＋ 逐路径 `git add -- <p>`。**结果**：索引 **71 A／43 D／187 M／55 R**；未跟踪**只剩那 6 条**，与排除清单一一对应。
**⏳ 全量 lint 复跑中**（日志 `/tmp/g78-lint.log`），预期 `tracked-hygiene` 转绿，仅余「外来未跟踪自测」那一条已知红。

**在飞**：全量 lint。**已收口**：G40–G74 全部派发单。**目标未达成。HEAD `9547faca`，暂存已完成（用户授权），提交与归档按序列在后。**

### R257 暂存后全量 lint 红在 gofmt（合并带入）；亲收并记一条证据级联次序（2026-11-18）

**✅ 暂存后全量 lint 读数**：**EXIT=1，红在第一道门 gofmt**，唯一文件 **`assembly/checkpoint_test.go`**（即 R254 我合并两枚锚的目标文件）。`gofmt -l .` 全仓仅此一文件。
**处置**：`gofmt -w` 后全仓 `gofmt -l` **零命中**；重新 `git add` 该文件；复跑两锚 `-run` 并集 ⇒ **仍 2 条 FAIL（红因未变）**。
**🔴 编排者操作教训（本轮第二次同型）**：**gofmt 须在每次编辑之后、暂存之前跑**，而我在 R234 清过一次格式后**又做了合并编辑**，没有重跑 ⇒ 红。登记：**每次写动作后 `gofmt -l .` 是落盘校验的一部分，不是一次性收尾动作。**
**⚠️ 证据级联次序（重要）**：G72 实测**证据指纹只覆盖 `.go/.mod/.sum`** ⇒ **任何 .go 字节改动（含 gofmt）都会让已收的 17 份证据脱树**。故**证据重收必须排在全部代码改动定稿之后**，不能边改边收。本轮已两次因中途改 .go 而重收，登记为次序纪律：**先定稿，再收证据，再提交。**

**在飞**：全量 lint 复跑。**已收口**：G40–G74 全部派发单。**目标未达成。HEAD `9547faca`。**

### R258 ✅ 本变更自身门面全绿：两条残红均归属外来未跟踪工作；撤回我误纳的另一变更三文件；派 F3 与 R35（2026-11-18）

**✅ 全量 lint 终态读数**：gofmt／modules／build／vet／comment policy／markdown doc budget 全过；红在 **checker selftests**（300 测 1 失败）。**因该步 hard-exit，下游门须逐个直跑取证**，编排者已逐个跑完：
| 门 | EXIT | 备注 |
|---|---|---|
| event-ownership --check | **0** | symbols 14／owners 6／packages 38 |
| name-check | **0** | 无迭代编号测试标识 |
| **tracked-hygiene** | **0** | **暂存后转绿**（R250 的暂存态红已清） |
| dotted-refs | **0** | |
| commit-scope | **0** | |
| doc-refs | **0** | |
| package-layout 自测 | **0** | 5 夹具负例仍拒绝 |
| gen_godoc --check | **0** | 82 packages |
| check-openspec | **0** | 主 spec 全绿 |
| **proc-refs** | **1** | 唯一 finding：`PROCESS-ARTIFACT-CITE scripts/gates/test_check_no_prefix_criterion.py:24: AUTHORITY = "openspec/changes/no-prefix-criterion/design.md"` |
⇒ **本变更自身门面全绿**；两条残红均为**外来未跟踪工作**：①checker selftests 的 `test_check_runtime_symptoms.py`（`??`，`git ls-files` 零 symptom 文件）②proc-refs 指向的 `test_check_no_prefix_criterion.py`（同属 `no-prefix-criterion` 变更）。**均禁触，立案告知。**

**🔴 编排者自纠一处暂存夹带**：纳入索引时发现 **`scripts/gates/check_no_prefix_criterion.py`、`test_check_no_prefix_criterion.py`、`golden/runtime/no_prefix_criterion.json` 属另一活跃变更 `no-prefix-criterion`**，被我纳入。已 `git restore --staged` 撤回三个文件。**现未跟踪恰 8 条，全为外来/他变更件**（interview helper／两 change 目录／五个 gates py）。**教训：排除清单须按「变更归属」而不仅是「禁触名单」过滤——另一变更的工作产物同样不得夹带。**
**🔴 又犯一次 zsh 分词陷阱**：逐个直跑门时用 `go run ./scripts/codetools $g`（未加引号）⇒ **三道「红」全是 `unknown subcommand` 用法错 exit 2，一个门都没真跑**。改显式命令后才得真读数。**这是本轮第三次同型，已记为「凡 shell 变量含多参数必须显式展开或逐条写死」。**

**✅ 派 G75（F3 症状直译锚红转绿两次证据）**：主树零写，回滚只发生在 `/tmp` 整树副本；**至少三条锚**各做「当前绿→副本基线→**只改生产代码**回滚机制→红（**失败文案须点名被量那个量**，红在别处即换机制不得凑）→复原 md5 逐字节相等→绿」。**明禁改锚迁就回归、禁手填结论、禁挑软锚。**
**✅ 派 G76（R35 第二轮独立放行审）**：四项专项复核（`rebind≥0` 改「两侧均测得」是否恒真／长跑启动就绪抖动是否真缺陷／`smoke_test.go:2650 unreachable` 是否 HEAD 既有／`smokeLongRestartEvidence` 是否真分得开两种读形状）＋自主剩余面扫描（门红归属／夹带复核／纯度残留／断言恒真）＋**须给「frontier 空/非空」与驳回清单**。

**在飞**：G75／G76。**已收口**：G40–G74 全部派发单。**目标未达成——F3 ✅，余 R35 与提交；F4 归档须待其余域完成（12 域两枚有意红仍在）。HEAD `9547faca`。**

### R260 🛑 R35 第二轮独立放行审**驳回放行，frontier 非空**；四条高严重度发现含两条编排者判断被推翻；提交暂停直至阻断项闭合（2026-11-18）

**🔴 独立审结论（G76，编排者复核其推理成立）**：**「frontier 非空」：两条实质恢复红、s1-w256 两类间歇红、恒真的成本锚、根模块证据脱树；另有提交卫生红。**

**🔴 四项专项复核的结果**：
1. **成本守卫修改不成立（高，阻断）**：`assembly/symptom_anchor_recovery_test.go:1134-1180` 中 **`rebind = boot - open` ⇒ `open + rebind == boot` 是代数恒等式**；各段顺序执行且计入 `sum` 的区间均含于 `totalSpan` ⇒ **`sum > totalSpan` 在正常时钟下不可达**。`rebind<0` 已不再拒绝；**8.5ms boot 对 10.1ms open 即反例**。⇒ **「相位无重叠」判据恒真**。编排者 R230 立案的担忧成立且比预想更糟——**不是变弱，是结构性恒真**。已派 **G77**（令其在当前测量面下找**真可失败**的不变量；**若结构上无法拒绝任何缺陷则停下报，不得为留测试而写恒真断言**；**不得简单把 `rebind≥0` 加回**）。
2. **长跑启动红不能归因环境抖动（高，阻断）**：首份 `reports/gen17.json:5-10` 显示 **boot 仅 413.6ms** ⇒ 不是「60s 未就绪」；真正失败是 `logs/gen17.log:204-211` 的**重绑 60 秒无应答、关闭时仍有 1 个未结项**；第二份 `logs/gen17.log:285` 与 `gen18.log:82` **各发送一次 `settle-ack SMOKE-R10197`** ⇒ **重启后的实际重复投递**。**第三次绿不能消除这两次间歇功能红。** 已派 **G78** 诊断（先只读、报修法与写入范围待放行，**禁先改代码**）。
3. `smoke_test.go:2650 unreachable` **确为 HEAD 既有**（`git show HEAD` 在 `:2612-2613` 已有同段死代码）⇒ 低，不归本变更阻断。**注意：默认 vet 绿不代表 smoke-tag vet 绿。**
4. `smokeLongRestartEvidence` **基本判别力成立**（族键与身份读子前缀合计、双零须 explained && seated），但**缺常驻控制案**（仅由长跑与临时 overlay 覆盖，无直接单测）⇒ 低，不单独阻断。

**🔴 两条编排者判断被推翻（逐条勘误）**：
- **「bundle 门 EXIT=0 ⇒ 现树 17 份证据有效」不成立**：`check_runtime_evidence.py:2594-2634` **只比较包内证据彼此一致，不与现树比较**。而**现根模块指纹已变为 `03dd70f8…`，bundle 仍记 `eaac2923…`** ⇒ **十短与 mutation 证据已脱树**（六长 wechat 指纹仍匹配）。**这正是 R257 立案的「任何 .go 字节改动使证据脱树」的实证——我在收完证据后又 gofmt 了 `checkpoint_test.go`。** ⇒ **证据须在全部代码定稿后重收**，且**该门对「证据是否描述现树」是盲的**，不能拿它的绿当验收凭据。
- **「两条有意红不进入放行 frontier」被驳回**：G76 定向复现 `assembly/checkpoint_test.go:2830-2903`（已提交事实重启后永不应用）与 `:2949-3011`（卸载后义务消失），其领域依据是 `changes/12-consumer-state-lifecycle/design.md:10-13` 的 J1/J2 ⇒ **是真实未实现行为，不是可忽略测试债**。**编排者采纳：12 域必须实施，不得以「有意红」为由放行。**

**✅ 编排者亲收提交卫生红**：`git diff --cached --check` 实红两处 EOF 多余空行（`docs/api/…api_durable.md:12`、`changes/01-three-hooks-golden/design.md:192`）⇒ 已截尾并重新暂存，**`git diff --cached --check` EXIT=0**。
**✅ 夹带复核通过**（G76 独立核）：外来路径 staged 为空、三件 no-prefix 已撤出 index、tracked-hygiene=0。

**🛑 提交暂停**：**frontier 非空且含高严重度项 ⇒ 不提交**（用户授权的是「卡点推进」，不是「带已知高严重度缺陷提交」）。**待 G77／G78 收口、12 域实施、证据按定稿树重收后再议。**

**在飞**：G77／G78。**已收口**：G40–G75＋G76。**目标未达成。HEAD `9547faca`。**

### R261 G77 证成成本锚「结构上不可能失败」；裁定不取换货币重写，在降级与退役间按覆盖证据二选一，派 G79（2026-11-18）

**✅ G77 停线（正确）并交回决定性证据**：
- **恒真之证**：`sum≤totalSpan` 的四项是**同一 goroutine 时间线上互不重叠的顺序切片**、全含于 `[totalStarted, totalSpan]`，单调时钟下对任意执行成立，**无生产变异可违反**；`:1178` 是 `:1134` 的代数改写。
- **叠加变异实测**：令 `GetEvent` 付 2000 次索引枚举 ⇒ `pointRead` **10.25µs→253.8ms（25000×）**、`rebind` −2.04ms，**测试仍 EXIT=0**；干净三次跑第二次自然出现 `rebind=−903µs` 亦 PASS ⇒ **既拒绝不了缺陷，又会随机假红假绿**。
- **量独立性**：独立＝`bootWall`/`engineOpen`/`pointRead`/`commitSpan`/`checkpointSpan`/`totalSpan`；派生＝`rebind`、`other=totalSpan−sum`，**且 `sum` 又含 `bootWall` 本身**。
- **另实测**：`journalBytes=[0 0 0]`、`indexEntries=[0 0 0]` ⇒ **五相位里两相恒为 0**（「五相位各自被测量」这句也部分不成立）。
- **未把 `rebind≥0` 加回**（注释与本机实测均证其合法且会 flaky）；**未删谓词**；主树零写，md5 前后一致（`segment_store.go 52628800…`、`config.go 7cd7aea6…`、本测试 `7e0dfd62…`）。
- **G77 报「唯一可失败的查验面是引擎计数器归属」**（变异下 `slotScanBytes` 增量 786,628,608 vs 干净 0），但那等于**把锚的货币由墙钟换成计数器、须改锚名** ⇒ 编排者裁定**不取**：那会改锚的主题，属另立一锚而非修这条。

**✅ 编排者裁定（不回避，给方向与判据）**：在「**降级为夹具形状测试**」与「**退役**」之间**按覆盖证据二选一**，已派 **G79**：
- **先核覆盖**：该测试驱动的测量面**是否被同包其他锚同样驱动**？若别人也驱动 ⇒ 独立价值只剩恒真断言 ⇒ **退役**并给「删除后哪些量仍有覆盖」证据；若它是唯一驱动者 ⇒ **降级保留**。
- **降级保留的硬要求**：**删掉全部恒真断言**（`rebind`／`sum≤totalSpan`／`other` 一类派生量）；**改测试名与头注如实陈述它钉什么**（禁写「不重叠」这类已证不可失败的性质）；**保留的每条断言都必须可失败**，负锚逐一构造反面；**两相恒为 0** 若是真实形状须如实钉、若是测量缺失须说清。
- **改名引用点只报不改**（`scripts/`／`openspec/` 禁写，编排者落账）。
- **退役的硬要求**：**不得因「没用」就删**，须先证覆盖有承接。

**在飞**：G78（重复投递诊断）／G79（成本锚处置）。**已收口**：G40–G77 全部派发单。**目标未达成。HEAD `9547faca`，提交仍暂停。**

### R262 当前队列回写与第12域开始实修（2026-11-18）

- **现场**：HEAD 仍为 `9547faca`。G79 工作区 diff 为删除180行恒真测试及其四个专用 helper、`strconv` import，未改四个既有计数锚。G78 因配额中断，没有可核销的诊断终局；不把工具失败当产品结论，也不修改配额/权限。
- **G80 独立复核**：删除伪证据本身可接受，但完整阶段归因没有承接；旧 `prefixPairsRead` 是测试另作扫描，不是当前 Load 读量，NEG3 又先撞其他断言。06设计/任务已补勘误，禁再执行已删除测试的旧选择器；09.R2保持未完成。
- **G81 接续盘点**：留存 `/tmp/g78` 系列探针/根可复用，但无完整复现终局；HEAD跑到60代后timeout不是PASS。失败根显示 R10197由两次生成、两个输出事实各自送入替身通道；不能靠交付身份去重解释，也不外推真实微信重复接收。`DurableCommit.settled` 仅读内存的疑点与真实切点尚须补证，暂不改投递生产路径。
- **第12域基线亲验**：S4 `behind-1-2` 未应用且重启跳过，S5卸载后未完项消失，两者同因红；三条既有消费者锚成功。`TestConsumptionFailureIsReportedByName`独立复现零错误行且仍欠义务。编排者给 `wrapper.go` 的结算拒绝/信箱拒收两分支增加具名错误日志，后者 `-race -count=10`通过；义务删除路径未改。此为12.R2报告子面，不宣告整叶完成。
- **G82派发**：只改 `runtime/state/checkpoint{,_test}.go`、`assembly/facts.go`、`assembly/checkpoint_test.go`。按乙案记录未解决应用错误、拒绝覆盖它的切面、同身份完整应用成功后解除；不加持久水位，不碰12.R3。S4旧前置“失败后仍成功落cut”与乙案不容，改钉cut拒绝＋从先前成功cut恢复的完整场景，不丢最终状态断言。精确测试和负锚后再决定整体验证。
- **账目修正**：首页与②节已更新当前队列，旧S3a/G44“在跑”表仅留历史；一级tasks新增当前未完成表。上一轮以勾选数把12域说成完全未启动、把本PR说成只差授权，均不再作为状态结论。

**当前在飞**：G82（12.R2 J1实施）。G80/G81只读单已返回。提交、推送、归档保持暂停；既有矩阵不代表现树。

### R264 G83 三项 P1 亲修：重试边界、拒绝划界、锚界分管（2026-11-18）

- **P1#1（复现→修）**：`TestRetryLeavesPresentedRangeAtTheLatestRound` 双子测红证 `foldMapped` 重折旧输出回退 `SessionPresented`。修法＝`Append` 返回「是否新携带」，边界只在新携带时移动；`skeleton_assert.go` 签名冻结同步。两子测转绿。
- **P1#3（裁定→修）**：拒绝按 `factSession` 过滤被推翻——投递切片按 Target 读、记录按 Owner 归，同一事实两链归属。裁定**拒绝不按链划界**：任一未应用事实拒一切 cut。中途试加的 `holder` 字段与 `RetryUnapplied` 亲验后删除（首错短路使 holder 判据不充分；按 holder 精确划界无生产第二消费者；Reduce 从头幂等已覆盖重试）。`TestSessionCutRefusesOnlyAnUnappliedFactOfItsChain` 重写、assembly `TestCutNeverCoversAnUnappliedFact` 改钉。
- **P1#2（复现→修＋裁定）**：`TestAnchorRebuildFoldsTheRangeNoCheckpointVouches` 双子测红证锚分支用锚界划 seam 折叠致被拒事实重建整段漏折。裁定**锚界只界投影、cut 界才界 seam**：`seedCoveredRange` 在幸存者水化前按序补折 (seam 界, 锚界]；`seedAccountFact` 更名 `seedFactWithoutProjection`；behind 分类界改 seam 界。稳态区间为空零额外成本。
- **断言重锚（非夹具迁就）**：`TestAnchoredRebuildEqualsFullWalk`／`TestAnchoredBootMatchesFullBoot` 的「锚解码 < 全走」旧断言以不安全跳过为前提，改钉「无担保时＝全走」；严格省读由真实 boot（Close 落 cut）的既有两测担保。曾试给 parity 夹具补落 checkpoint——boot 已消费恢复种子、二次直调双双归零，改回原形态。
- **验证**：三处红锚先红后绿；state `-race -count=10`、`memory/replay` 全绿；全量 assembly 复跑中（此前仅 S5 红）；comment_policy 触碰包零 findings（门 exit 1 为外来文件既有 scan-set 配置红）；`docs/api` 重生成。裁定全文见 12 域 design.md §十八。本轮零 git 写，提交/推送/归档仍停（R35 frontier 非空：12.R3 S5、重复投递根因、09.R2）。

### R263 G82中断后亲收应用失败保护与并发反例（2026-11-18）

- **现场与进度**：G82到轮数上限，无终局战报；四个文件已有实现和测试。编排者实跑四条状态单测及两条通知失败测试通过，S4及三个新增Cut/Restart场景初测通过。不能据此勾整叶。
- **亲验发现的回归**：`TestConcurrentRetryDoesNotClearANewerFailure` 用通道控制同身份并发，无睡眠；旧实现让较早成功清掉后来拒绝，snapshot返回nil，测试实红。现改为捕获调用开始时的失败记录，仅当记录未被替换才清除；复用registry.mu，回调期间不持该锁，不加全局串行Reduce锁。五条状态测试 `-race -count=10`通过。
- **行为范围收紧**：恢复原有首个Reduce错误即返回，不采用G82“失败后仍调用全部holder”的扩大行为；重试应用已提交的held事实，不用调用者重新给的等价事件；去掉额外错误锁与单使用方包装helper。会话测试holder仅记录真正成功的应用并按身份去重。
- **仍在验证**：全量assembly运行中；G83只读独立审J1的失败清除、会话范围、快照时序及S4重表达。12.R3卸载红仍属未解决功能，重复投递调查和09.R2归因不随本项推进核销。本轮未提交、未推送、未归档。

### R259 ✅ F3 完成：三枚症状锚「绿→红→绿」闭环，复原逐字节相等；G75 两处纠正编排者的令（2026-11-18）

**✅ 三锚闭环（主树零写，回滚只在 `/tmp` 整树副本）**：
- **`TestSymptomAnchorBootCostFlatAcrossGenerations`**：绿读数 `addressedValueReads=[1 35 35 35 35]`、integrity 逐代==journalLogBytes、slot 钉零。回滚**只给键的走查→带值走查** ⇒ **exit 1**，红原文 `:1016: the boot of generation 2 read 527 value(s) by address beside the 284 generation 1 read, want the same count: the boot reads the facts of the history behind it instead of the facts of the chain it serves`；红读数 `[4 284 527 770 1013]`（逐代 +243）。复原 PASS 9.51s，`journal.go` md5 `1fbe59ae…` 与主树 `cmp` byte-identical。
- **`TestSymptomAnchorPerGenerationCostFlatOverALongRun`**：绿读数 252 代 integrity 与 journalLogBytes 每代逐个相等、addressedValueReads 第 2 代起恒 31、slot 恒 0。回滚**整 log 完整性校验→只校水位之后窗口** ⇒ **exit 1**，红原文 `:538: a counted face of the boot grows with the chain: generation 1 read 0 integrity bytes scanned beside the 19623 its denominator states, want one to one: the face is the declared intrinsic of the open and tracks the history itself`。复原 PASS 29.53s，md5 同。
- **`TestSymptomAnchorMidbandProcessRecalledByTicket`**：绿读数 14 entries／2 fold products／`recall_get("evt_00-1-plan") answered 228 byte(s)`。回滚**折叠产物行写票据→不写** ⇒ **exit 1**，红原文 `:207: the request carries no ticket "[evt_00-1-plan|thinking_plan]" for the folded entry 00-1-plan … the model is handed the fold product's summary and no handle on the process text, so it cannot recall what the fold consumed`。复原 PASS，`age_segments.go` md5 `21f22e96…` 与主树一致。
**三红均为断言红且点名被量面，非编译错、非别断言先红。**

**✅ G75 两处纠正编排者的令（采纳）**：
1. **A2 的回滚点我指错了**：令写「退回每 boot 发布」，但**那不动任何被量面**（log 长度与 integrity 同步增长仍 1:1，配置链加一条 fact 不进任何计数面）⇒ 它改用令里点名的引擎侧第二机制「integrityBytesScanned 钉线性」作回滚面，红文案正是该面的线性断言。**这正是「变异须真落在被量的面上」的正确执行。**
2. **A1 的回滚点下移**：令写在 `assembly/commit.go` 的 `loadAddressedLocked`，它改到 `memory/ledger/journal.go` 的 `eachKeyNewestFirst`——**同一「只给键」语义在引擎侧一行可回滚，改动面更小，且红仍点名同一 `addressedValueReads` 面**。
**✅ 第四锚具名放弃（正确纪律，不凑红）**：`TestSymptomAnchorSessionBootCostFlatAcrossGenerations` 的被量面是 `Run.CommittedFacts()` 解码体计数；把 `GetEventInPartition` 退回座位集界**只增读次数、不改解码体数** ⇒ 红不会点名该量 ⇒ **属凑红，按令不做**。
**✅ 副本完整性绕证**：`rsync` 对两个 iCloud 占位文件（`docs/.dev/*.docx`／`*.zip`）报 `Operation timed out`；以**全树 794 个 `.go` 的 md5 聚合 `f491d586…`** 证明副本完整，会话前后主树/副本一致、复原后同值。
**主树零写证据**：会话前后 `git status --porcelain | md5` 同为 `4a3de597…`，HEAD 同 `9547faca`，九份相关文件 md5 逐条相同。全程零 git 写。

**在飞**：G76（R35 第二轮）。**已收口**：G40–G75 全部派发单。**目标未达成——F3 ✅，余 R35 与提交；F4 归档须待其余域完成（12 域两枚有意红仍在）。HEAD `9547faca`。**
