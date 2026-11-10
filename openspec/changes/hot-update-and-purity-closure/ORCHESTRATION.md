# hot-update-and-purity-closure 编排文档

## 一、结论

抵达 DoD 的唯一路径：W0 先立两锚红+exec 探针证明事件指引整代装配可行并冻结共享契约 → W1 并发清交付/读面/门禁文档 → W2 hygiene → W3 全清单消费者每代装配 → W4 混沌+收尾；当前最大风险＝storage 根变更的在途安全（5.7），探针失败则具名保留 restart 档并回写 proposal，不阻主线。

**甲案阶段（r47/r48 后，用户裁决「一口气做干净」）**：Wave1 夹带清零（S/C/D 三轨并行）→ Wave2 会话级协调者下沉＋`Stateful` 收编 g(x) → Wave3 有界投影 → 收口合并。当前最大风险＝Wave3 若再以「删已提交事实」或「跳过压缩判决集」换有界，会原样重演 12.12/12.13 的整段回退（证据见 12/tasks.md 五节判决理由）；故 Wave3 重开的前置是 answered 切片，不是压缩手段本身。

**W8 干净终点（2026-11-10 用户终裁「不起新变更，一笔做完，这就是这趟旅途的目的」——session-checkpoint-material-unification scope 并入本篇，不延后）**：三真因已修并 push（4280d208：WAL 逆扫去 sort／compactFactOrderLocked panic／smoke harness 缝）＋D 类真话纠正。剩余按触碰集不相交并发：

| 轨 | 孙任务 | 前置 | 触碰集 | 并发/状态 |
|---|---|---|---|---|
| CORE | answered 切片→B4 past-cut→cadence②③→carrier 有界化 | answered 切片先行 | assembly/compress_consumer.go·checkpoint.go·run.go·invoke.go | ✅ 已落地（36d328a4/7de9f08f） |
| RO | RestoreOwed 退役（邮箱全走切片，无载体 boot 空切面恢复） | 无 | runtime/state·assembly/settle_turn.go·run.go | ✅ 已落地（96290ef8） |
| BS | BandSummaryInstruction 测试钩子退回纯粹缝 | 无 | assembly/summary_gist.go＋四 harness | ✅ 已落地 |
| FR | 帧上限 64→32MiB | carrier 有界化 | memory/ledger/journal.go | ✅ 已落地 |
| ECH | eachCommittedEvent/EachSessionFact | — | — | 裁保留（06/4.c4 合法机制读面，删＝weakening） |
| P1 | 开账存活：未清义务必随 cut 存活（at-least-once，非 at-most-once） | 用户既有裁决「开账随 checkpoint 存活」 | assembly/host_delivery*.go·checkpoint.go·facts.go·run.go | 在跑（独立裁，非上报） |
| P2 | 回执计入折叠判据：模型可见 raw≤预算（D1） | P1（facts.go 重叠） | assembly/age_fold.go·facts.go·smoke_test.go | 待 P1 |
| VOCAB | 词汇统一（详见下节审计） | 功能 work 落地后 | 全 checkpoint/compression 注释＋标识符＋concept_bindings＋GLOSSARY | ✅ 已落地（执行记录见本节末） |
| UNIFY | 会话级协调者下沉＋Stateful 收编 g(x)＋六端口→Material | CORE/RO/P1/P2 | 全插件管线面 | 末轨 |

并发准入＝触碰集不相交。P1/P2 同触 facts.go 故串行；VOCAB 改名须等功能 work 落地避免互踩。最大风险＝answered 切片若跳判决集换有界即重演 12.13 回退（验收锚＝live≡锚定≡全量三方判决集相等）。

### W8-VOCAB 词汇统一审计（2026-11-10 用户「新词非常多，违背维护规范，统一相似概念」；先记录待一起派单）

规范依据：GLOSSARY §四「一句话说不清、且不含本页未出现的词，就不加词而写成既有能力的字段」；§五契约拼写冻结不随更名；concept_bindings（golden/runtime/architecture.json，45 概念）每新名须绑 canonical concept＋kind。checkpoint 一域新增约 10 概念。

三层问题与统一映射：
- **层1 新概念过量**：CheckpointSlice/CheckpointOffset/CheckpointPayload 本是 Checkpoint 组成部分（按 §四 可降为字段而非独立概念）；**TaskCheckpoint 与 RegistryCheckpoint 同义**（皆「checkpoint 所载任务态」）→ 合一。真新概念 Checkpoint/Stateful/band 按 §四 论证保留并登记 GLOSSARY。
- **层2 同义词**：carrier↔checkpoint（LoadScan/LoadStats 描述用 "the carrier rebuild"，主概念 Checkpoint）→ carrier 并入 checkpoint；cut↔boundary↔accountCut（cut 是登记规范词，boundary 留指锚幸存者边界、accountCut→compress cut）；account↔ledger（StateRegistry "ordered account" vs 投递 ledger canonical）→ 按「派生态持有 vs 读回面」澄清或统一。
- **层3 撞车/复用**：anchorRef/anchorEntry/anchorSlot 本属压缩折叠锚（domain06），被 checkpoint 复用（newestCheckpointRef 返回 anchorRef）→ 解耦：checkpoint 引用用 checkpointRef，或明确 anchor 升格通用「链上定位引用」并登记；band 晋升用 "generational/fold generation" 与 recorder/session/config generation 撞 → 改 band promotion，退出 generation 词。

执行约束：同步 concept_bindings 登记＋代码标识符＋GLOSSARY；§五冻结拼写（事件元数据 compress_covered/checkpoint:<seq>、日志关键字、测试名、磁盘标记）MUST NOT 改；改名走显式变更。验收：concept_budget 门绿＋GLOSSARY 无未登记新词＋grep 同义词残留零。

**执行记录（W8-VOCAB 落地，2026-11-11）**：按上映射逐条执行，先注释后标识符。
- 层2 carrier→checkpoint：仅改「checkpoint 事实」义（assembly/checkpoint*.go、host_delivery_checkpoint.go、runtime/state/checkpoint.go 及其测试、run.go/facts.go/compress_consumer.go/host.go 的 checkpoint 段）；**commit／store carrier 义（`commitCarrier`/`DurableCommit`/`MemoryCommit`、runtime/state/durable.go·memory.go、runtime/task/*）保留不动**——那是「提交承载面」另一个概念，改成 checkpoint 即把两个概念焊死。
- 层1 合一：`TaskCheckpoint`→`StatedTask`（`RegistryCheckpoint` 立为唯一任务账术语，逐条只叫「一条被声明的任务」）；concept_bindings 键同名改，concept_budget S2 基线行改名，new=47/removed=5 未动棘轮。
- 层2 cut：`accountCut`→`compressCut`（标识符＋注释），boundary 只留「锚幸存者边界」。
- 层2 account↔ledger：account＝派生态持有者（送达账/压缩账），ledger＝读回面；`DeliveryLedger` 注释与 GLOSSARY 第 12 行就地写死这条分工。
- 层3 anchor：取低风险支——`anchorRef` 注释立为「链上定位引用」（折叠锚与 checkpoint 引用共用），不新建 `checkpointRef` 类型、不动折叠锚机制。
- 层3 band 晋升：`generational`/`fold generation` 全退（memory/replay 注释、两处 integration 失败文案、三篇 wiki 的「世代递推」），`generation` 只留记录世代/会话世代两义（GLOSSARY 合为一行写死）。
- GLOSSARY：新增 `Stateful`（并入第 12 行定义）、`band 晋升`（并入压缩产物行）、`Material`（新行）；两行世代合一行，总行数零差额（doc-budget 棘轮未动）。
- 测试名：改名的都是自面 test/helper 名（`TestBootRestoresTheCheckpointOnce` 等），§五冻结的四条测试名/事件元数据/磁盘标记/日志关键字零改动（diff 无 `compress_covered`/`compress_key`/`checkpoint:<seq>`/`reincarnation` 变更）。


## 二、编排四问

### 2.1 次序（就绪孙任务表；派发原子=孙任务）

| 孙任务 | 域 | 前置 | 状态 | 触碰集 | 派发单 |
|---|---|---|---|---|---|
| 1.1/2.1/5.1 锚红 | 01/02/05 | 立案 | ready | 各自新增测试文件 | W0-1 |
| 5.2 探针 | 05 | 5.1 | ready | tool/exec+assembly/run.go(探针隔离) | W0-2 |
| 5.3 契约冻结 | 05 | 5.2 | ready | design D6（不产代码） | W0-3 |
| 1.1-1.8 | 01 | restart-recovery 归档（host_delivery 活跃编辑重叠） | blocked | host_delivery/host_tools/loop | W1-1 |
| 2.2-2.5 | 02 | 2.1 | ready | read/get/localfile | W1-2 |
| 4.1-4.4 | 04 | 无 | ready | fuse/request_assembly/window_bounds/gates/docs | W1-3 |
| 3.1-3.5 | 03 | restart-recovery 归档 | blocked | commit/host/host_capture/run/store_searcher/recovery | W2-1 |
| 5.15/5.16 注册表+归位 | 05 | 5.3+03 入 main | blocked | assembly 注册表缝(编排者)+各消费者包 | W3-0 |
| 5.4-5.12 | 05 | 5.15/5.16 入 main | blocked | 各消费者包+登记调用（run.go 缝归编排者） | W3-* |
| 5.13/5.14/F | 05/一级 | W3 全绿 | blocked | integration/chaos/wiki | W4-1 |

并发准入=触碰集不相交（实测 `go list -deps`）；W1 三单触碰集互斥已核（01 delivery 簇 / 02 read 簇 / 04 gates 簇）。本轮做不了：03/05（启动门未过）。

### 2.2 授权

每单白名单精确到上表触碰集文件；禁 git commit/checkout、禁动他域文件；过程件落 /tmp/<change>-<单号>/。每单含"自清白名单内 finding"条款（门命令 `scripts/gates/lint.sh` 随令下发，自面清零判据随令）；共享面/全局 finding 归编排者；编排者终跑门是唯一终态判据（agent 各自绿≠整体绿）。共享面 run.go 装配缝、host_reload.go、api/event、api/config 归编排者单写（05 消费者经 D6 冻结签名只改本包+登记调用）。偏离分级：白名单内替身/mock 预授权；扩 409 保留档、改 D6 签名、storage 不可热换裁决=上报。

### 2.3 凭据

五查按派发单核；每条孙任务 verify 三锚 exit 0（正锚 -run 逐字符+非空转用例数>0；负锚具名硬门；消费者锚=Host.Reload/Run.Invoke/recall_get/boot 公共入口+负例）。必亲验数字：409 清单 grep 计数=0、混沌重复次数≥10、race 报告空、锚 A/B 红绿退出码、F1 diff 空。

### 2.4 释放（锁必携等待表）

| 锁/占用 | 释放条件 | 等待表（释放后当场补派） |
|---|---|---|
| restart-recovery 归档门 | 该 change archive | 补派 W2-1(03)；03 入 main 后补派 W3-* |
| run.go 单写锁 | 编排者落 D6 缝 | 补派 05 各消费者单 |
| 探针设备/chaos 槽 | 5.2 / 5.13 结束 | 补派 W3 消费者 / W4 收尾 |

## 三、明细

派发单-孙任务矩阵见 2.1；合单判据：同域同文件簇合单（01 的 host_delivery 簇含 1.2/1.3/1.4/1.7）；单均<2 不拆。预算：W1 三单并发、W3 消费者按包并发上限=不相交包数；混沌 5.13 为长跑单后台跑不占并发槽（R32）。档位与真实路径欠账：storage 远端 daemon 真实路径若缺凭据按 R24 三态（无门 Skip/有门缺凭据 Fatal/门开非空转）。

**W5（合并阻断增补）domain 12-checkpoint-state**：LongRun boot 二次扫描挂死（Unsettled 逐站点全史读，O(站点×事件)）阻断 PR#37。用户裁决统一 checkpoint 事实族（Flink 框架状态管理），取代 06 D3「快照装全状态否决」一条、继承 D4 折叠锚一致性切面。契约冻结于 12/design.md D1-D7。派发：12.2-12.6 耦合簇（api/event+assembly/run+host_delivery+runtime/state 共享）合单派一个 openspec-apply 顺序实施；12.7 测试契约冻结后可并行；编排者终跑门为唯一终态判据。触碰集白名单＝12/tasks.md 所列；禁动他域。合并门＝LongRun 六分片全绿 + 每代 boot 成本平坦。

**W6（甲案三波）domain 12 终态收口**：Wave1 夹带清零 → Wave2 会话级＋g(x) → Wave3 有界投影 → 收口合并；派发原子仍为孙任务，准入＝触碰集不相交。

| 波 | 就绪孙任务/对象 | 前置 | 触碰集 | 派发单 | 验收锚（缺一不放行） |
|---|---|---|---|---|---|
| Wave1-S 语义夹带清零 | 12.3b/12.10 静点与结算语义的残留核对；r48-1（reload「有效声明」应为随代指针事实，既有缺陷）单列 | 12.3b/12.10 已绿 | assembly/checkpoint.go、host_delivery.go、host_reload.go | W6-1 | 正锚：`TestCheckpointWaitsForTheQuietPointOfThePipeline`；负锚：删 `r.flight.await` 必红；消费者锚：LongRun 真实宿主过 gen60 |
| Wave1-C cadence 收口 | 12.14 已记（保留 markFlightHeld 旁路、回退 close/fold 接线）；12.15 触发器②③另行接线 | 12.14 绿 | assembly/checkpoint.go、compress_consumer | W6-2 | `go test ./assembly/ -count=1` 全绿；`TestCheckpointEmittedAtCadenceAndFold`/`TestRestoreFoldCounts` 不因新增载体改判 |
| Wave1-D 文档台账 | 本单：wiki 四页收编＋GLOSSARY＋本文件三波计划＋新变更骨架 | 无 | docs/wiki/、docs/GLOSSARY.md、openspec/ | W6-3 | 改后 grep 实证；`openspec validate hot-update-and-purity-closure --strict` 绿 |
| Wave2 会话级＋g(x) | `session-checkpoint-material-unification`：协调者会话级下沉、`Stateful` 收编消费者读态（reducer=g(x) 纯派生面、emitter=observation 驱动者）、六端口收敛 Material 契约 | Wave1 全绿入 dev | 新变更自述白名单 | W6-4 | 新变更 proposal/tasks 三锚冻结后派发；症状直译锚第一天红 |
| Wave3 有界投影 | 12.12 B3 载体有界（不得删已提交事实）、12.13 B4 投影 past-cut（先补 `compress.answered` 为 `Stateful` 切片） | Wave2 的 answered 切片落地 | memory/ledger、assembly/run.go 投影种子 | W6-5 | 三方 parity（锚定/回退/全量）判决集相等；`TestTierQueryMatchesObservedRead`/`TestObservedEventsReadsBeyondHotWindow` 绿 |
| 收口合并 | 12.8 编排者终跑门、12.9 8.1 卡 superseded 标记、F1-F4 | Wave1/Wave2/Wave3 全绿 | 门命令与台账 | W6-6 | 12.8 逐条 exit 0；F1 diff 空；`openspec validate --strict` 绿 |

释放在 Wave1 三轨：S/C 同改 `assembly/checkpoint.go`，触碰集相交 ⇒ 合单串行或 S 先落再派 C；D 与二者不相交可并行。Wave2 开工前 Wave1-S/C 的 `assembly/` 写锁必须释放（等待表：12.8 之外不得留未落盘编辑）。Wave3 的 12.12/12.13 已按「回退＋立卡」记于 12/tasks.md，重开时先读其判决理由与后续纯化判据，不得以新测试期望替代回退事实。

**W7 合并门根因修复 domain 13-bounded-compression**：长程校准六分片全红的真正根因＝压缩产物无界（88MB 超帧上限）＋三段龄名存实亡（级别算了但 FoldByAge 只分新/非新，中+老塌进一条摘要）＋摘要累积全文（historySummaryLine 对上一产物抄 entry.Content）。用户裁决：三段龄按**世代递推**（新=recent 保留／中=上次新这次折叠过程细节／老=上次中老这次摘要）；老带 **LLM 语义摘要优先**（落摘要事实保恢复确定，无配置降级机械有界 recent-K＋key 引用，失败降级/欠账）；摘要**有界幂等**（fold(fold(x))==fold(x)，上一产物不抄全文）。此域取代 Wave3 的 B3/B4 局部回退方案（12.12/12.13），从根上落地有界压缩。契约冻结于 13/design.md D1-D7；耦合核心 13.1-13.4 合单串行，测试/golden 13.5-13.7 随后。合并门＝LongRun 六分片整代 PASS＋产物有界＋boot 平坦。方法论教训（用户）：反复修 CI 失败暗示架构设计不清，须整体思考定位核心而非逐 symptom 打补丁。

## 四、台账

| 轮 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|
| 规划 | 自审见下 | — | — | — | 基线 dev 036c3500；两锚基线红证据=host_reload.go:790(409)/host_delivery.go:547+1481(trim) | — |
| 终审轮 | R35 两轮：首轮三阻断(doc-budget/meditation 水位代际/文档脱节)由 R4 修；复审余 R35-1(拒绝候选水位滞留)已修+TestRefusedMeditationOverNoMeditationBootClearsWatermark 钉；concept 基线重指可达 main(3b727199)、removed_min 校准 5；CI 待绿后 squash | 见 /tmp/hupc-R4、C5、R35 报告 | 登记例外 anchors/approvals/RL 为受治理债(review_by 2027-05-05) | 6.3/6.4 勾 | CI watch bd0rgbyxs | — |
| W5 域12 进度 | 12.1-12.7a/12.7c/12.7d、12.10/12.11/12.14 已勾（事件类型+载荷、Stateful 接缝+安静点协调者、五切片接线、恢复重写删三全史读者、RecoveryTicket 退役、B1 水位、B2 NewestOfType、C3 收口） | 12.8 编排者终跑门未跑；12.7b LongRun 卡在 r48-1（reload 回退点既有缺陷）；合并门＝LongRun 六分片全绿+每代 boot 成本平坦 | 8.1 由 12.4/12.4b 开账嵌入 checkpoint 构造消解，8.1b 待 12.8 的 `TestEventIntegrityTaskContinuation` 复跑；12.12/12.13 已回退立卡，重开前置见 W6 Wave3；06 D3 一条已由 r46 勘误取代 | 12.3b `TestCheckpointWaitsForTheQuietPointOfThePipeline` 绿 ×3 -race；12.10/12.11 各自 `-run` 绿；12.14 `go test ./assembly/ -count=1` 全绿 | 12.8 终跑门；12.9 收尾回写；W6 三波 | —（12/tasks.md 写锁仍归 domain12 实施代理） |
| W6-D 文档台账 | 文档收编完成：wiki 四页＋GLOSSARY 收敛到当前实现（B1 水位／B2 `NewestOfType`／安静点无锁／`seedFact` 隔离），目标态（会话级下沉＋`Stateful` 收编 g(x)）标于平台篇 §六；三波计划与 12.12/12.13 回退立卡核一致；新变更骨架已建 | 未跑代码门（本单禁碰代码，assembly/runtime 另代理在改） | 无 | ORCHESTRATION W6 表＋台账；12/tasks.md 小节编号修正；`docs/wiki/{platform/platform-subsystems,memory/memory-architecture,agent/agent-architecture,mechanism-tour}.md`；`docs/GLOSSARY.md`；新变更 proposal/tasks/spec | 两变更 `validate --strict` 均绿；执行期文档随 W6 各波回写 | — |
| W7 域13 整合+裁定 | 477b3229 后 CI 广泛回归经 history∥host 两路 openspec-apply 整合绿（主线程先诊断每处根因再派，81/116 turn 收绿）；多数＝测试钉旧行为改期望钉世代不变量；LLM 摘要增模型调用按用户「可以直接 mock」在 provider 缝答材料 gist 不计正式请求；comment_policy 9→0 | 全量 `go test ./... -short -count=1` 48 包绿；host/history/journey/assembly 包 `-count=1` 绿 | **band 跳级甲案裁定**（用户「不考虑开发成本甲更纯粹」）：band 读材料自带压缩标记（carriesFoldProduct）非另立世代台账，D2 勘误；跳级边界＝全局 summary 滚入邻 turn 段（splitSegments 只切 output），立 D8＋13.11 令折叠产物自成语义单元修之 | 13.11 派 openspec-apply（症状直译锚 TestGlobalSummaryStandsApartFromTheRecentTurn 红→绿＋负锚幂等＋消费者锚五包绿）；domain12 soak 真回归（checkpoint 陈述+尾链任务漏记 Recovered 致报告 false+query 丢任务）主线程一处统一修绿 | 13.11 绿后：gen_godoc 重生（BandSummaryInstruction 导出）→13.9 全门→提交（排除外来未跟踪）→push→CI 六长分片→squash→归档 | 13/tasks.md 写锁归 13.11 实施代理；外来 test_check_runtime_symptoms.py 暂移 /tmp 跑 lint、提交前恢复 |
| W8-VOCAB 词汇统一 | 三层逐条落地：carrier→checkpoint（仅 checkpoint 义，commit/store 义保留）、`TaskCheckpoint`→`StatedTask`（任务账合一）、`accountCut`→`compressCut`、account/ledger 分工写死、anchorRef 立为链上定位引用、generational→band 晋升、GLOSSARY 补 `Stateful`/`band`/`Material`（行数零差额） | `go build ./...` 0；`go test ./assembly/ ./runtime/state/ ./memory/replay/ ./api/plugin/ ./runtime/task/ ./tests/integration/history/ -count=1` 全绿；`go run ./scripts/comment_policy -no-baseline assembly runtime api memory` 0 finding；`gofmt -l` 空；`go vet ./...` 0 | 遗留口径：commit←→store carrier 义与 checkpoint 义分立，未强行合并（合并即焊死两个概念）；`TestAgeFoldPromotesTheGenerationsAcrossFolds` 等测试名保留，避免改写已记录的验证命令 | concept_budget 绿（new=47/removed=5 未动、绑定键随名改、ratchet note 附勘误）；doc_budget 零差额；`gen_godoc.sh --check` 绿 | 同批发现：`scripts/gates/test_check_runtime_symptoms.py` 红（`TestLocalSmokeSymptomBootDeployed` 不在外来未跟踪 checker 的冻结注册表），与本单触碰集无交，归外来回填 | — |

（执行期逐轮回填；本档为派发唯一依据，派发前重读二节。）
