# 04 · 05-inspection / 06-learning 终态回溯 review

> 判据（用户定 2026-10-18）：按孙任务逐叶回溯，**过往契约与行为不作金标准**——穿透行为看本质；理想工程双维＝**简单**（概念统一、收敛）＋**清晰**（职责明确、抽象单一、丰富多态）。
> 范围：05-inspection 7 叶（05.1-05.4、05.P0.1、05.P0.2、05.21）＋ 06-learning 11 叶（06.1-06.8、06.P0.1、06.P0.2、06.21），共 **18 叶**。
> 对照物：`task-graph.json`（两域逐叶 verification/evidence）、`changes/{05-inspection,06-learning}/tasks.md`、`midterm-review-response.md` §二、`prototype/{model,memory,contracts}.go`、实现落点 `query/` `telemetry/` `rl/` `Run.Diagnostics`（08.4）。
> 方法：**只读**。本文件是本会话唯一写入；全部结论后附**可复跑命令**与本次会话真实输出；未回写 tasks.md / task-graph / 任何产品或测试文件。
> 纪律：**不做 PASS 汇总**。以下判定按"要求面是否成立 + 正式入口是否消费 + 负锚是否存在"三问逐叶给出，缺口具名编号（G1–G10）挂入 §五。

---

## 〇、全局实测快照（本会话，仓库根）

| 命令 | 真实输出 |
|---|---|
| `go build ./...` | `BUILD_EXIT=0` |
| `go test ./... -count=1` | **13 个包 `ok`**，无失败行（`go list ./...` = 46 包，其余 `no test files`；`examples/wechat-bot` 是独立 module，不在其中） |
| `go test -race ./tests/integration/inspection -count=1` | `ok … 1.553s` |
| `go test -race ./tests/integration/learning -count=1` | `ok … 1.812s` |
| `go test -race ./tests/integration/inspection -run '^TestRuntimeInventory$' -count=10 -json \| grep -c '"Action":"pass"'` | `81`（80 子测/顶层 + 1 包级） |
| `go test -race ./tests/integration/learning -run '^TestSFTCleaningContract$' -count=10 -json \| grep -c pass` | `21`（**1 子测**×10 + 顶层×10 + 包级 1；与 evidence "16 子测"不符，见 G6） |
| `go test ./tests/integration/learning -run '^TestSFTConsumer$' -count=1 -json` | **25 pass + 1 skip**（skip = `…/the_real_tokenizer_and_artifact_face_is_reported_unseen_until_its_gate_is_set`；见 G1） |
| `python3 scripts/check_concept_budget.py --rule iface-methods` | `RESULT: ok`；`query/` **0 命中** |
| `python3 scripts/check_concept_budget.py --rule struct-fields` | `RESULT: ok`（含 baseline warn 若干）；`FeedbackEndpointConfig` Go 代码 **0 命中** |
| `python3 scripts/check_runtime_plan.py` | `RESULT: ok` |
| `python3 scripts/check_runtime_architecture.py --implementation` | `RESULT: ok` |
| `python3 scripts/check_runtime_structure.py` | **`RESULT: FAILED`**，16 个产品文件 > 500 行（见 G9） |

账目面：18 叶在 `task-graph.json` 全部 `status: "done"`，在两域 `tasks.md` 全部 `- [x]`。其中 05.1/05.2/05.3/06.1-06.4 的 `notes` 均带「中期复审重开（2026-10-15）……原 evidence 作废留档」，即其完成凭据是本轮 M 波重跑后的版本（run_id `dm3-`/`dm5-`/`dm8-`/`dm4-`/`dm6-`/`dm9-`/`dm10-`）。

---

## 一、逐孙任务表（18 行）

判定图例：**✅ 成立**＝要求面有实现落点 + 正式入口/具名锚可复跑；**⚠️ 部分**＝机制成立，但接缝、消费者或口径有具名缺口（挂 G 号）。

| # | 叶 | 任务行要求（要点） | 实现落点 | 本会话凭据 | 判定 |
|---|---|---|---|---|---|
| 1 | 05.1 | agent/session/invocation/task/notice/租约**只读运行总图**；统计从明细派生，不把多维状态相加成"运行数" | `query/inventory.go`（六维 `AgentEntry`…`LeaseEntry`；`InventoryView.Counts()` :338 从明细派生；`AgentCounts{Configured,Resident,Busy}` :383-390 三读数各自保留）；`read.go` `runReading.Published` 读 `r.current`；`run.go:277` `r.current = cand.Plan` | `TestRuntimeInventory -count=10 -json` → 81 pass；负锚 `TestQueryShowsPublishedAgentState`（host，真 `Assemble`+`Publish`）→ `go test -race ./tests/integration/host -run '^TestQueryShowsPublishedAgentState$' -count=1` → `ok … 1.278s` | ✅（租约授权粒度另挂 **G7**） |
| 2 | 05.2 | 授权过滤、**不可变快照**、稳定分页、详情与订阅 gap/reset；游标绑定 revision 与过滤条件；单快照对象/字节预算 | `query/snapshot.go`：`Budget{MaxObjects,MaxBytes}`/`DefaultBudget={4096, 4<<20}`（`types.go:267,280`）、`walkBounded`（limit 先于物化）、`Projection.retained(at)`/`List(at, cursor,…)`；`query/subscribe.go` `GapWindowExpired/GapOverflow/GapDeleted` + `UpdateGap/UpdateReset` | 负锚 `TestSnapshotPagesWithinByteBudget`（`countingSource` 证物化量上界）；`TestQueryAuthorizationAndCursor` 130 pass（evidence）；本会话包级 `ok` | ✅（授权对租约过宽，同 **G7**） |
| 3 | 05.3 | 提交后接 **OTel/审计**，SDK 尝试观察分层；**关闭 OTel 不影响 Query** | `telemetry/otel.go` `NewSDKObserver`/`AuditRecord{Fact,Attempts}`（trace→metric 分层，关闭走 `Skipped` 不调 SDK）；`run_tool.go:75-79` 仅在 `r.observer != nil` 时挂 `observePlugin{plugin.NewCommitObserver(runFacts{run:r}, r.observer)}`；`run.go:216` `SetPresentations(r.takePresentation)` | 负锚 `TestObservationReachDiagnostics` 40 pass（evidence）；本会话 grep：`NewSDKObserver` **仅测试构造**，全仓非测试 `WithTelemetry` 零命中（仅 `skeleton_assert.go:88` 编译断言） | ⚠️ **G4**：机制成立，**产品宿主零装配** |
| 4 | 05.4 | 重启/取消/退役/存储失败下解释每条**阻塞及下一推进者**；连续 unknown 只升级诊断不判死；未止义务/租约保留可解释 | `query/explain.go`：`Blocker`(6f)/`OpenObligation`/`LeaseRetention`(9f)/`LeaseLedgerCounts`(6f)/`BlockerReport`(:153，Counts 授权过滤后派生)；`Run.Explain`；`ExecLeaseExplainer(manager)` nil→nil | `TestExplainRuntimeBlockers` 70 pass（evidence，6 子测+顶层），正锚三包 271 pass 不破；本会话包级 `ok` | ✅（附记：evidence 自述实施方越权自勾 tasks.md，编排者/评估门追认勾选内容属实，后续明令禁写 openspec） |
| 5 | 05.P0.1 | Query 承载类型骨架（只声明、零逻辑） | `query/{types,snapshot,subscribe,skeleton_assert}.go` | `go build`/`go vet`/`gofmt -l query/` 空输出/`comment_policy` 0 findings/`check_runtime_architecture --check-policy` ok（evidence） | ✅（骨架已被 05.1-05.4 覆盖，验收以实作为准） |
| 6 | 05.P0.2 | telemetry 骨架（观察端口/recorder/封账签名） | `telemetry/{port,recorder,skeleton_assert}.go` | 同上命令组（GOPROXY=off 复跑） | ✅（同上） |
| 7 | 05.21 | query 碎接口归并 12→≤4 ＋ `InventoryFaces` 瘦身 | `query/reading.go`：`Reading`(3 方法) / `ReadingSource`(1) / `DetailSource`(2) / `LeaseExplainer`(2)；`Generation`(2f)/`SiteReading`(4f)/`TaskReading`(5f) 取代 `InventoryFaces`(8f，已删) | `--rule iface-methods` → `ok`，`query/` **0 命中**（本会话）；`check_concept_budget` 复跑 by evidence | ✅（G9 附注：`snapshot.go` 996 行/`inventory.go` 887 行超结构门） |
| 8 | 06.1 | 版本化记录接 episode/decision/attempt **实际生产点**；旧格式只读导入；未知谱系显式；新格式明确版本 | `rl/record.go`（三记录 schema，`ModelViewRef.Snapshot` 注释"空引用不是快照"）；`rl/produce.go` `RecorderAdapter`；`agent/loop.go` `recordEpisode`/`recordDecisionAttempt`/`noteRecordingLoss`；`run.go` `WithRecorder→NewRecorderAdapter` | 负锚 `TestFormalInvokeProducesTrainingRecords` 10/10（evidence：三记录 + failingRecorder 下 loss 精确 episode1+decision2+attempt2）；本会话 `-race ./tests/integration/learning` `ok 1.812s` | ✅（谱系恒 `LineageUnknown`，见 **G10**） |
| 9 | 06.2 | 请求边界**安全快照**、原生响应/logprobs 保留、统一终局观察；Headers/URL secrets 不落盘；不重跑 provider options 造快照 | `run_request.go:38-55` `snapshotRequest`（在 `GenerateContent` 前固定）；`run.go:218` 恒挂 face；`agent/loop.go:438` 先于 :440 调用；`rl/capture.go` `SnapshotRequest`+`projectURLs`+`redactHeaders`+`RedactExtraFields` | 负锚 `TestFormalRequestSnapshotHasNoSecrets` 40 pass（evidence：请求字节全等 / USERINFO+QUERY 凭据零泄漏 / provider 实收原凭据）；本会话包级 `ok` | ⚠️ **G2**：快照本体与零泄漏成立，**下游寻址/持久化断裂** |
| 10 | 06.3 | 有界队列、字节预算、**丢失账**、准入与 writer 封账；采集与 model 共享终局；no seal 记 partial | `rl/sink.go`：`CaptureSink` 有界准入 + `CaptureLedger` 分类计数 + `Seal` 同队列屏障 + `!Synced→Partial` + `await` 超时→`abandoned` | 负锚 `TestFormalChainSealsCapture` 40 pass（evidence：真 `Assemble`+`WithRecorder(NewCaptureSink(NewFileCaptureWriter))`+真 Invoke+Seal，`Records==5`/SHA256 对盘/`ParseLine` 逐条） | ⚠️ **G4**：正式链在测试成立，产品宿主零装配 |
| 11 | 06.4 | 授权导出、延迟反馈、**版本化 reward 派生**、逐样本来源 manifest；feedback/reward 分开 | `rl/export.go`（`ErrExportUnauthorized` 先于任何读；`ExportManifest{…,SourceSHA256,Cutoff,Complete}`；`SealSnapshot/Snapshot` 独占创建）；`rl/export_source.go` `CaptureExportSource`（读正式链产物并复核 SHA256） | 负锚 `TestExportTracesFormalRecords` 50 pass（evidence：`src.calls==0` 无授权零读 / 逐字节双向对账 / `failingAfterWriter` 恒等式 / 篡改拒读） | ✅ |
| 12 | 06.5 | 严格转换、消息合法性、动作/观察 mask、**训练用途资格矩阵（不由 SFT 外推）**、episode≠session、**因果依赖分组**防泄漏、v2 只读导入 | `rl/sft.go`（12 种 `SFTGapKind`；`CausalGroupOf`）、`rl/sft_convert.go`、`rl/sft_qualification.go`（3 purpose × `RequiredEvidence/Verdict/MissingReasons` 四要素，`unknown≠ineligible`） | `TestSFTCleaningContract -count=10` → **21 pass（仅 1 子测）**；其余 15 个 `t.Run` 属 `TestSFTCleaningRefusals`(:324)/`…Qualification`(:456)/`…Grouping`(:623)，**不在该命令内** | ⚠️ **G5**（5/12 gap kind 负锚悬空）+ **G6**（数字口径偏差） |
| 13 | 06.6 | 真实 SFT 消费者 tokenize/collate、labels/mask/非空样本与拒绝账；**真实 tokenizer/chat template，不以常量替身结项** | `rl/sft_artifact.go`（`SFTSourceTokenizer{Identity, Encode}` 函数型端口；`SFTLabelIgnoreIndex=-100`；`SFTTokenizerFace{real,deterministic,absent}`）、`rl/sft_consumer.go`（18 具名拒收） | `TestSFTConsumer -count=1` → **25 pass + 1 skip**（skip 面＝真实 tokenizer/制品未见）；仓库**无任何真实 tokenizer 适配器**（grep 零命中） | ⚠️ **G1**：机制面确定，资源面未验证却已勾选——**与任务行"SKIP 不能计完成"直接冲突** |
| 14 | 06.7 | RL HTTP 鉴权、allowlist、**逐跳重定向**、大小限制与反馈归属 | `rl/feedback_endpoint.go`（`NewFeedbackEndpoint(resolver,feedback,rewards,rule,opts…)` + `WithFeedbackToken/WithFeedbackLimits`）、`rl/http.go`（`EndpointAllowlist` 逐跳复核、`RequestLimits`/`DefaultMaxBodyBytes=1<<20`）、`cli.go` `/feedback` | 正锚 `TestFeedbackHTTPContract` 130 pass（evidence：内容寻址幂等 / derive-on-change 不改旧 / 409 零写入 / 越权 hop 零触达 / 双门认证 401 零副作用）；本会话包级 `ok` | ✅（真实端点单次门控挂 09.5，evidence 已声明未验证） |
| 15 | 06.8 | **模型替换租约**贯穿实际请求与流结束；重建失败**不发布错误绑定** | `rl/replacement.go`（`ModelLease.Replace(version,rebuild,publish)`、`ReplacementRecord{State,from,to,Version}`、`ErrLeaseVersionHeld/Missing`、`Restore`）；`rl/record.go`/`rl/produce.go` 版本归属通路 | 正锚 `TestModelReplacementLifecycle` 10/10 + 4 子测（evidence，含 mutation probe：清版本→4 子测红→还原复绿） | ✅（`replacement.go` 635 行，见 G9） |
| 16 | 06.P0.1 | rl 骨架（record/episode/capture） | `rl/{record,episode,capture,skeleton_assert}.go` | `go build`/`vet`/`gofmt -l rl/` 空输出/`comment_policy` 0（evidence） | ✅ |
| 17 | 06.P0.2 | rl 骨架（export/manifest/reward/http） | `rl/{export,manifest,reward,http}.go` | 同上 | ✅ |
| 18 | 06.21 | `FeedbackEndpointConfig` 配置面收敛（6f→零选择构造+`With*`）＋数据面豁免具名登记 | `rl/feedback_endpoint.go` 四位置参数 + 2 `With*`；`FeedbackEndpointConfig` **类型已删**（Go 代码 0 命中，仅存于规划文档） | `--rule struct-fields` → `ok`；本会话 grep：`FeedbackEndpointConfig` 在 `*.go` **零命中** | ✅（数据面 5 项仍以 S2 类别豁免在 baseline，见 §四） |

**逐叶计数（不是总评）**：✅ 13 叶 / ⚠️ 5 叶（05.3、06.2、06.3、06.5、06.6）。5 个 ⚠️ 分别对应 G4×2、G2、G5+G6、G1——**两类问题不可混同**：G2/G4 是"机制对了但链路没通"，G1/G6 是"账目口径与纪律不一致"。

**08.4 交叉面（`Run.Diagnostics`，`diagnostics.go:233-259`）**：`DiagnosticReport{Instance, Switches, Query, Reflection, Training, Telemetry, Lifecycle}`；`Query` 面无开关恒可读，其余四面关闭时给 `reasonNo*` 诚实拒。两域对它的贡献是 `Training.Snapshots = len(r.RequestSnapshots())`（06.2 产物）与 `Telemetry.Presentations/Views/Gaps`（05.3 产物）。**要点**：这两个计数都是"run 内存里持有多少"，不是"数据面/观察面已接通"——在尚未接 recorder/observer 的宿主上它们同样可以在开关打开后读到非零值（`r.snapshots` 恒采），所以**诊断面变绿不等于数据面成立**，这是 G2/G3/G4 在 08.4 上的同一根。

---

## 二、原型对照

### 2.1 `prototype/model.go`「记录完整请求/输出/错误用于训练监控」→ `rl/` 训练数据面

**原型（`prototype/model.go:1,10-21`）**

```go
// 职责：调用框架已确定实例的模型并记录完整请求/输出/错误用于训练监控。
// 未实现：调度、重试、缓存、provider 检测、system 组装、流式与计费。
func CallModel(ctx, model ModelFunc, request ModelRequest, record func(ModelTrace)) (ModelOutput, error) {
    snapshot := cloneRequest(request)
    out, err := model(ctx, request)
    trace := ModelTrace{Request: snapshot, Response: cloneOutput(out)}
    if err != nil { trace.Error = err.Error() }
    if record != nil { record(trace) }
    return out, err
}
```

原型语义有三个硬约束：**深拷贝防回改**、**失败也记录**、**记录是同一次调用内的同步回调**（`contracts.go:59-63` `ModelTrace{Request, Response, Error string}`）。它同时把"完整性"理解为**原样复制**——`cloneRequest` 连凭据一起复制，`Error` 只是一个字符串。

**现树（`rl/`）**

| 原型要素 | 现树对应 | 深化性质 |
|---|---|---|
| 一次调用一条 trace | `EpisodeRecord` → `DecisionRecord` → `AttemptRecord` 三层，`AttemptRecord{Response,Terminal,Usage,Error,Logprobs,LogprobsSource,ModelVersion}` | **结构化**：错误从 `string` 变成带 `Terminal` 语义与 `LogprobsSource` 三态（present/unsupported/unknown）的记录，使"训练可用性"可判定而不只是"出过错" |
| `cloneRequest` 原样复制 | `rl.SnapshotRequest(view, req, policy)`：`projectURLs` + `redactHeaders` + `RedactExtraFields`，同时**保留 provider 实收原值** | **语义迁移**：完整性从"复制原文"升级为"可训练且不可泄密"——同一请求的两种视图，且测试断言两者同源（见 §三.2） |
| `record` 同步回调、无持久化 | `CaptureSink`（有界准入 + `CaptureLedger` 分类丢失 + `Seal` 同队列屏障 + `Partial`）→ `TrainingExporter`（授权 + `SourceSHA256` + manifest）→ `CleanSFT`（12 具名 gap + 资格矩阵） | **边界化**：从"回调给你"到"可封账、可复核、可授权导出、可判能不能训"。这正是"训练数据面"与"打日志"的本质差别 |
| 无 loss 概念 | `RecordingLoss`/`RecordingLosses()`（`agent/loop.go`）+ `CaptureLedger` | **失败本身可读**：原型 `record == nil` 就静默丢掉，现树把"没记上"变成一等事实 |

**两个诚实的断层（不是演化，是接缝丢失）**

1. **请求侧无下游**：`rl/produce.go:174-190` 的 `RecordDecision` 注释明写 *"the snapshot reference stays empty at this production point"*，`ModelViewRef.Snapshot` 恒零值；`MarshalSnapshot/ParseSnapshot/VerifySnapshot` 生产代码零调用（唯一引用是 `rl/skeleton_assert.go:266-269` 的编译断言）；`Run.RequestSnapshots()` 的生产读者只有 `diagnostics.go:306` 的 `len()`。**结果**：原型"一条 trace 里请求与响应同框"的语义，在现树拆成两条从不交汇的路径——响应侧进了训练记录，请求侧留在 run 内存里只能被诊断计数。SFT 需要提示面时，是测试手工从 `run.RequestSnapshots()` 粘到 `rl.SFTPrompt`（`tests/integration/learning/sft_cleaning_test.go:197-200`）。
2. **谱系恒 Unknown**：`produce.go:165` 恒写 `Lineage: LineageUnknown`，`ParentID/DependsOn` 的唯一消费者是 `rl/export.go:551-556`，生产点从不填。**结果**：06.5 的"因果依赖分组防泄漏"在生产链退化为"单 episode 一组"——安全（不会泄漏）但更粗（该合并的没合并）。

### 2.2 `prototype/memory.go`「Recall」→ `query` 读面 / 检索

**原型（`prototype/memory.go:37-46`）**

```go
// Recall 按键返回原文；缺键返回 ErrMemoryMiss。
func (m *Memory) Recall(_ context.Context, key string) (string, error) {
    m.mu.Lock(); defer m.mu.Unlock()
    e, ok := m.data[key]
    if !ok { return "", ErrMemoryMiss }
    return e.Data, nil
}
```

原型是**无授权、无预算、无分页、返回原文文本**的 map 读；文件头明示未实现"落盘持久化、索引、淘汰策略、语义检索"。

**现树拆成两条演化路径**

| 面 | 原型 | 现树 | 演化本质 |
|---|---|---|---|
| 结构读 | `Recall(key) → string` | `Run.Query(ctx, ListFilter) → query.Snapshot`；`Run.Explain(...) → BlockerReport` | 从"取一条"到"取一次**冻结的授权视图**"：`Revision` 锚定、`Cursor` 分页、`Budget` 双界、`Gap/Reset` 增量语义。原型"读就是取原文"的心智被替换为"读是一次可承诺的采样" |
| 检索读 | 同上 | `Run.Recall(ctx, search.Query) → search.Result`：经 `memory.StoreQuerySource` 观察表 + `search.StoreSearcher` 双路融合，返回**引用集**而非文本副本；未装配存储 → `ErrRetrievalUnwired` 具名拒 | 原型 `ErrMemoryMiss`（缺键）→ `ErrRetrievalUnwired`（缺面）：拒绝的语义从"数据不存在"升级为"读的能力不存在"，两者都不伪装成空结果 |
| 时序 | 无 | `supplyRetrieval`：*"a query consumes an event after the table observes it, and never at the moment its bytes happen to be committed"* | 原型没有的一致性语义：**可见性以"被观察"为界**，而非"被写入"为界 |

**红线迁移**：`query/reading.go` 头四条红线——「每次从 owner 现读」「源不可读具名拒」「**不记录/缓存/合成**：读口自身不是第二真源」「不放宽授权：越权对象被排除而非被改写」——正是对原型"map 里存着一切、谁都能 Recall"的**反向约束**。原型的 read 是 storage read，现树的 read 是 **authorized, bounded, reproducible reading**。

---

## 三、辩证分析（穿透行为看本质）

### 3.1 「只读投影单真源」是否本质？

**是本质，但不是"缓存有无"的问题。** 单真源要保障的是两件用户可感知的事：**读的授权可承诺**（越权对象被排除而不是被改写）、**读的时点可复现**（同一 revision 两次读等价）。`query` 满足这两条：`Projection.retained(at)` 只保留**已冻结快照**（`projectionWindow = 8`），而冻结快照是"某 revision 的确定性函数"，重算得同结果——它是**快照缓存**，不是**事实副本**；`runReading` 每次仍回 owner 现读（`read.go:137-190`：`r.current` / `store.Site` / task carriers）。

**但"单真源"在进程内只对读面成立，对 run 的持有物不成立。** 三处 append-only、无 delete、无窗口：

| 持有物 | 写入点 | 语义 | 后果 |
|---|---|---|---|
| `r.facts` | `run_fact.go:53`（每次工具结果提交） | 与 event store **同形**的 `map[EventID]Event` | 事实的第二份全量副本 |
| `r.presented` | `run_fact.go:200`（After pass 每已提交事实一次，`agent/loop.go:553`） | 每次提交的 `Presentation`（views+gaps） | 每次提交一份渲染产物 |
| `r.snapshots` | `run_request.go:54`（**每个 decision 无条件**，`run.go:218` 恒挂 face） | 全量 prompt/skill/投影/transcript 的深拷贝 | 每个 decision 一份完整请求 |

**穿透**：这不是"建了第二真源"（读口仍只读 owner），而是"**run 持有三份无界账**"。它们的增长曲线不在任何门里，也不会被 `Seal`/`Budget`/`Close` 收束。对长跑宿主（唯一产品宿主是 `examples/wechat-bot`），这是内存随时间线性增长。**单真源的工程本质不是"不缓存"，而是"保留必须是有界且可读的事实"**——06.3 的 `CaptureLedger` 已经给出了正确形态（有界 + 分类丢失账），三处 run 持有物缺的正是这一形态。

### 3.2 安全快照的特性本质 = 密钥边界零泄漏？

**零泄漏是必要不充分。** 只做到"快照里没有 secret"是不够的——如果无法证明**训练侧看到的脱敏视图与 provider 收到的原值视图来自同一次请求、同一 revision、同一参数**，脱敏就只是"另外抄了一份看起来像的"，样本与真实行为之间失去可证同源性。

现树把同源性做成了可断言的事实：
- **时点**：`run_request.go:38` 在 `GenerateContent` 之前固定；`agent/loop.go:438` 严格先于 `:440` 的调用；注释明写"model view is fixed before the request starts and is never rebuilt from the options of a finished call"。
- **内容**：`TestFormalRequestSnapshotHasNoSecrets` 断言"JSON 字节全等模型可见 Request 含 prompt/skill/投影/历史全量"，同时断言"USERINFO/QUERY 凭据不进快照（`[redacted]`）而 provider 实收保留原凭据"。
- **边界**：工具 result 不冒充请求快照；失败固定进 `RecordingLoss` 而不是带着半个快照进模型决策。

**于是判定要分两层**：
- **安全属性成立**（零泄漏有强负锚，且证明是"投影"而非"抹掉"）；
- **数据属性不成立**（快照不可寻址：`ModelViewRef.Snapshot` 恒空，`Marshal/Parse/Verify` 无生产调用，`RequestSnapshots()` 只被计数）。

一句话：**06.2 交付了"安全的快照"，没有交付"可训练的快照引用"**。后者才是模型训练侧真正的特性需求（样本必须能回指它当时的输入），它横跨 06.1 的记录字段、06.3 的 writer 落地、06.4 的导出复核——目前这三处都没有接上（G2）。

### 3.3 SFT 资格矩阵「不外推」的业务本质

不是保守，是**训练用途的证据不可迁移**：

- SFT 合格的证据 = 一条完整 observed completion + role mask 合法；
- DPO 合格的证据 = same-input candidate pairs + 偏好比较；
- sampled RL 合格的证据 = 行为策略下的 logprobs + 采样上下文。

三者的证据集合互不包含，所以"SFT 合格"对另外两者**零信息量**。矩阵（`rl/sft_qualification.go`）把每个用途的 `RequiredEvidence` 独立列出，给 `unknown / ineligible / qualified` 三态 + `MissingReasons`，其中 **`unknown ≠ ineligible`** 是关键设计：没观察到证据 ≠ 证据表明不合格。工程形式就是"宁可 unknown，也不给下游一个看起来能用的样本"。

**业务本质**：数据面是**下游训练的准入证明**。一次错误外推会让整批数据被污染，而模型训完通常**不可回滚**——所以不可逆风险的判定必须前移到数据出口。`TrainingUseConsumer`（没有声明消费者不得判合格）是同一逻辑的第二道：**没人要用，就不该判"可训"**。

**残余**：资格判定依赖 `RequiredEvidence` 的文本证据与 logprobs 三态，而 `unknown ≠ not_returned` 在真实 provider 面上仍无门实证（06.6 的真实制品面 skip，挂 09.5）——即"不外推"的规则本身可复跑，其**输入的真实性**未验证。

### 3.4 05.21 / 06.21 概念收敛的价值

**实测**：`query` 4 接口（`Reading` 3 方法 / `ReadingSource` 1 / `DetailSource` 2 / `LeaseExplainer` 2），`--rule iface-methods` 在 `query/` **零命中**；`FeedbackEndpointConfig` 在 `*.go` **零命中**，构造为 `NewFeedbackEndpoint(...)` + `WithFeedbackToken/WithFeedbackLimits`，与 04.21 的 `Open()+With*` 同构。

**穿透**：这不是行数优化。12 个碎接口的本质是"把每个字段组当成一个能力面"——实现者要回答 12 个"我要不要实现这个"，而每个只有 1–2 个方法，认知成本落在**选择**而非**理解**。归并后的 4 个接口把问题压缩成三个必答 + 两个可选：

- 必答：这一代发布了什么（`Published`）/ 现场在哪（`Sites`）/ 任务欠什么（`Tasks`）；
- 可选：详情从哪来（`DetailSource`）/ 租约怎么解释（`LeaseExplainer`）。

价值＝**扩展者的问题数从 12 降到 4**。`FeedbackEndpointConfig` 6f→0 是同一逻辑的配置面版本：6 个导出字段 = 6 个必须填对的选择；零字段 + 2 `With*` = **默认正确、按需覆盖**。对 HTTP 端点这种"安全默认必须关"（allowlist/token）的面，零选择起步是唯一能防"忘配"的形态。

**代价必须一并登记**（否则收敛结论不诚实）：
1. 接口归并后 `Reading` 三方法必须一起实现，对小实现者是**耦合增加**；
2. `Reading` 目前只有 `runReading` 一个实现（`read.go:137` 的 `var _ query.Reading = runReading{}`），"丰富多态"尚未被第二实现者验证——收敛收益要等第二个实现者出现才兑现；
3. 收敛的是**能力面**不是**类型面**：`query` 仍有 54 个导出类型 / 3296 行 / 8 文件。

---

## 四、双维局部评（简单＋清晰）

### 4.1 `query` 12 → 4

| 项 | 收敛前 | 收敛后（本会话实测） | 判定 |
|---|---|---|---|
| 接口数 | 12 碎接口 | **4**（`Reading` 3 / `ReadingSource` 1 / `DetailSource` 2 / `LeaseExplainer` 2，全 ≤3） | ✅ 语义单元归并 |
| `InventoryFaces` | 8f | **已删除**；`Generation` 2f / `SiteReading` 4f / `TaskReading` 5f | ✅ 结构消失而非豁免 |
| 机器门 | — | `--rule iface-methods` → `ok`，`query/` 0 命中 | ✅ 可复跑 |
| 统计口径 | 多维状态相加成"运行数" | `InventoryView.Counts()` 从明细派生；`AgentCounts` 三读数并列（`inventory.go:338-356,383-390`） | ✅ 不合成 |

**扣分项**：`query/snapshot.go` **996 行**、`query/inventory.go` **887 行**（结构门红，G9）；导出类型 **54** 个——收敛的是"必须回答的问题数"，不是"必须认识的类型数"。清晰度收益集中在**入口**，不在**词汇表**。

### 4.2 `FeedbackEndpointConfig` 6f → 0

| 项 | 收敛前 | 收敛后（本会话实测） | 判定 |
|---|---|---|---|
| 配置结构 | 6 导出字段 | **类型已删**，`*.go` 零命中 | ✅ |
| 构造面 | 填 6 个字段 | `NewFeedbackEndpoint(resolver, feedback, rewards, rule, opts…)` + `WithFeedbackToken/WithFeedbackLimits` | ✅ 零选择起步 |
| 机器门 | baseline 记 6f | `--rule struct-fields` → `ok`；baseline 行出表 | ✅ |
| 数据面豁免 | — | `ExportManifest`/`SampleProvenance`/`CaptureLedger`/`Explanation`/`FeedbackSubmission` 以 S2 类别豁免登记 | ⚠️ 具名欠账（非清零）：`struct-fields` 仍报 `CaptureLedger 8f` 等 warn |

**判定**：配置面零债**成立**；数据面"不拆"是有理由的（拆块=序列化复杂化，违数据原样透传），但它是**豁免**不是**收敛**——两者在账上必须分开记，本叶的 evidence 做到了（"口径勘误波28"已把二者分开）。

---

## 五、缺口与待裁（G1–G10，均附可复跑凭据）

> 本节只**列事实 + 待裁选项**，不代裁。凡涉及纪律口径（G1/G6）或跨叶补差（G2/G4），须上级具名裁定。

### G1【06.6 真实 tokenizer：口径冲突，最高优先】
- **事实**：`rl/sft_artifact.go` 的 `SFTSourceTokenizer` 是函数型端口（`Identity` + `Encode SFTEncodeFunc`），仓库**无任何真实 tokenizer 适配器**；`TestSFTConsumer -count=1` 实测 **25 pass + 1 skip**，skip 子测名 `…/the_real_tokenizer_and_artifact_face_is_reported_unseen_until_its_gate_is_set`；`changes/06-learning/tasks.md` 06.6 行原文「（缺凭据/模板不支持/`SKIP` **不能计完成**）」且状态标 `design-blocked`，但复选框为 `[x]`，`task-graph.json` 同叶 `status: "done"`。
- **影响**：SFT 数据出口的真实可用性无实证（tokenizer 身份/模板 collate 均未跑）；机制面（mask 分侧 / 制品 JSONL / 18 具名拒收 / pad 语义）确定。
- **待裁选项**：(a) 06.6 降回 `planned`（勾选撤为 `[ ]`），把机制面拆成独立可完成叶；(b) 保留 `[x]` 但在 tasks.md 与 task-graph 具名登记"资源面未完成、挂 09.5"，即对"SKIP 不能计完成"作出**具名例外裁决**；(c) 提供真实制品后重跑门控再勾。**我不选。**

### G2【06.2 快照无下游：接缝断裂】
- **事实**（四条互相独立，共同构成断链）：
  1. `rl/produce.go:174-190` `RecordDecision` 不填 `ModelViewRef.Snapshot`（注释自认）；
  2. `MarshalSnapshot/ParseSnapshot/VerifySnapshot` 生产代码零调用（`grep -rn "MarshalSnapshot\|ParseSnapshot\|VerifySnapshot"` 除定义外仅 `rl/skeleton_assert.go:266-269`）；
  3. `Run.RequestSnapshots()` 生产读者仅 `diagnostics.go:306` 的 `len()`；
  4. SFT 提示面靠测试手工 glue：`tests/integration/learning/sft_cleaning_test.go:197-200`。
- **影响**：训练样本（`SampleLine`）无法回指其请求快照；"请求/输出/错误同框"的原型语义断成两处；06.4 的 manifest 可溯源到**记录**，但记录里没有请求侧。
- **待裁选项**：(a) 补一条纵链（recorder 从 capture port 取 `SnapshotRef` 写记录 → sink/writer 落地快照字节 → export 时 `VerifySnapshot` 复核），归 06.1/06.3 的 M 波补差；(b) 明确降级为"快照仅诊断可见、训练面不含请求侧"，并在 06.2 tasks 行写死该边界。

### G3【无界内存保留三处】
- **事实**：`r.facts`（`run_fact.go:53`）、`r.presented`（`run_fact.go:200`，`agent/loop.go:553` 每提交一次）、`r.snapshots`（`run_request.go:54`，`run.go:218` 恒挂、**每个 decision 无条件**）全部 append-only，`grep "delete(r\."` 零命中，无窗口。
- **影响**：长跑宿主内存线性增长；`r.snapshots` 在**无 recorder 的产品宿主上也照采全量 prompt/transcript**。
- **待裁选项**：(a) 三处加有界 + 分类丢弃账（同 `CaptureLedger` 形态）；(b) 加"仅诊断/训练开启时采集"的开关；(c) 接受并写入文档（须显式承认长跑风险）。

### G4【产品宿主零装配（05.3 / 06.1 / 06.3 / 08.4 交叉）】
- **事实**：全仓 `Assemble` 调用点仅 2 处（`run.go:154` 定义、`examples/wechat-bot/host_runtime.go:307` 调用）；后者 `opts` 仅 `WithGovernance`（+ 可选 `WithPolicy`）。`grep -rn "WithRecorder\|WithTelemetry\|WithReflectionAnchors" --exclude-dir=tests .` → **零命中**（`skeleton_assert.go:88` 的编译断言除外）。`NewSDKObserver` 仅测试构造。
- **影响**：`Run.Diagnostics` 的 training / telemetry / reflection 三面在唯一产品宿主上恒为 `reasonNoRecorder`/`reasonNoObserver`/`reasonNoAnchors`；05.3/06.1/06.3 的"正式装配"实际只在测试的 `formalRun` 成立。
- **待裁选项**：(a) 要求 wechat 宿主接入（recorder 落盘路径 + telemetry 端点配置）作为三叶的完成条件；(b) 若否，把 evidence 的"正式装配"边界改写为"Run 层可装配，宿主未采用"，并在 08.4 诊断面说明这是产品态。

### G5【06.5 负锚欠账 5/12】
- **事实**：12 种 `SFTGapKind` 中，`unknown_schema`/`body_unreadable`/`identity_mismatch`/`attempt_failed`/`completion_malformed` 在 `tests/` **零引用**（`grep -rn "<kind>" tests/` 全部空）。evidence 自述"负锚覆盖 7/12 gap kind，留 06.6 补齐"，而 06.6 是消费叶，未补。
- **影响**：5 条具名拒只有代码分支、无反向测试；重构可静默改语义而不红。
- **待裁选项**：(a) 补 5 个具名子测（成本低、可当场做）；(b) 明确降级为"分支存在即可"，并把该降级写进 tasks 行。

### G6【06.5 数字口径偏差（S5 类，需更正账目）】
- **事实**：06.5 evidence 记 `TestSFTCleaningContract -count=10` "20 pass/0 fail（**16 子测**+顶层）"。实测该**具名命令**为 **21 pass** = 10 顶层 + 10 子测 + 1 包级，且**只有 1 个子测**（`the formal chain converts strictly and masks actions apart from observations`）。其余 15 个 `t.Run` 位于 `TestSFTCleaningRefusals`（`sft_cleaning_test.go:324`）/`…Qualification`（:456）/`…Grouping`（:623），**不被该命令执行**。
- **影响**："SFT 清洗 16 子测全绿"的印象来自命令范围误并；本次具名命令只证明 1 个子测。属 midterm §一 S5（数字溯源）的残留。
- **待裁选项**：(a) 更正 evidence 数字（不改代码），并把 4 个测试函数分列或纳入 `-run '^TestSFTCleaning.*$'`；(b) 维持并在报告中标注该偏差（本报告即已标注）。

### G7【05.1 租约授权粒度】
- **事实**：`query/snapshot.go:779-782` `scopeCovers` 对 `ObjectLease` 仅要求 `len(scope.Agents) > 0 || len(scope.Sessions) > 0`——**任何已授权读方可见全量租约**（含他 agent 的 model / filesource 资源名）；`tests/integration/inspection/query_auth_cursor_test.go` 的 S1 授权过滤子测对 `ObjectLease` 显式 `continue` 跳过。
- **张力**：05.1 evidence 称"§五.2 待核（Query lease 授权归属）已随 `scopeCovers`/`refAllowed` 收口"；代码语义是**放行**而非"按 owner 收窄"。
- **待裁选项**：(a) 全局可见（运维视角，需在契约写明）；(b) 仅 owner 可见；(c) 按资源种类分级。裁定后决定是否补 S1 断言。

### G8【C3：loss 语义级联（已登记，未执行）】
- **事实**：现树 SFT loss 落 action **全侧**；若 09.5 真实 tokenizer 引入 chat template 后改"仅尾 span"，须同步改 `rl/sft_artifact.go` 的 `Verify` 契约 + 两条断言（evidence 06.6 已登记 C3）。
- **影响**：`t.Skipf(… so the face is hung on 09.5)` 的前置注释与 C3 登记一致，但级联清单未落到可执行的前置项。
- **待裁选项**：(a) 在 09.5 挂具名前置（改契约→改两条断言→重跑 06.6）；(b) 保留为文档级登记。

### G9【结构门红且未入 CI】
- **事实**：`python3 scripts/check_runtime_structure.py` → **`RESULT: FAILED` / violations: 16**：`query/snapshot.go` 996、`agent/loop.go` 926、`query/inventory.go` 887、`rl/export.go` 640、`rl/replacement.go` 635、`rl/capture.go` 560、`rl/skeleton_assert.go` 531 等。该脚本不在 CI workflow（workflow 只跑 plan / concept_budget / test_check_*）。
- **影响**：06.5/06.6 的"拆文件满足 500 行硬门"只对**自身文件**成立；域内既有文件仍红，且无机器门阻止继续增长。
- **待裁选项**：(a) 结构门入 CI（会立刻红，需先拆）；(b) 提高阈值 + 具名豁免；(c) 维持现状，则"500 行硬门"表述降级为"新文件适用"。

### G10【06.1 谱系恒 Unknown】
- **事实**：`rl/produce.go:165` 恒写 `Lineage: LineageUnknown`；`ParentID/DependsOn` 仅被 `rl/export.go:551-556`（分组）与 `rl/import.go`（旧格式）读取，生产点从不填。
- **影响**：06.5 的"基于因果依赖分组防泄漏"在生产链退化为单 episode 组——安全但粒度更粗；测试覆盖的是手工构造的 episode 记录。
- **待裁选项**：(a) 生产点填依赖（成本在 agent loop 侧，需 event 携带父/依赖）；(b) 明确接受"生产链按 episode 分组"，并在 06.5 契约写明。

---

## 六、复跑命令清单（本次全部结论的凭据）

```bash
cd /Users/pengweiye/Documents/codes/tagent

# 编译与全量测试
go build ./...                                                     # BUILD_EXIT=0
go test ./... -count=1                                             # 13 包 ok，无失败行（46 包中其余 no test files）
go test -race ./tests/integration/inspection -count=1              # ok 1.553s
go test -race ./tests/integration/learning   -count=1              # ok 1.812s
go test -race ./tests/integration/host -run '^TestQueryShowsPublishedAgentState$' -count=1   # ok 1.278s（05.1 负锚）

# 计数锚
go test -race ./tests/integration/inspection -run '^TestRuntimeInventory$' -count=10 -json | grep -c '"Action":"pass"'   # 81
go test -race ./tests/integration/learning   -run '^TestSFTCleaningContract$' -count=10 -json | grep -c '"Action":"pass"' # 21（仅 1 子测）
go test ./tests/integration/learning -run '^TestSFTConsumer$' -count=1 -json | grep -c '"Action":"skip"'                # 1（真实 tokenizer 面未见）

# 机器门
python3 scripts/check_concept_budget.py --rule iface-methods        # ok；query/ 0 命中
python3 scripts/check_concept_budget.py --rule struct-fields        # ok（含 baseline warn）
python3 scripts/check_runtime_plan.py                               # ok
python3 scripts/check_runtime_architecture.py --implementation      # ok
python3 scripts/check_runtime_structure.py                          # FAILED，16 文件 >500 行

# 接缝事实（grep 级）
grep -rn "MarshalSnapshot\|ParseSnapshot\|VerifySnapshot" --include="*.go" . | grep -v _test   # 仅定义 + skeleton_assert
grep -rn "RequestSnapshots()" --include="*.go" . | grep -v _test                               # 仅 run_request.go:65 定义 + diagnostics.go:306 len()
grep -rn "WithRecorder\|WithTelemetry\|WithReflectionAnchors" --include="*.go" --exclude-dir=tests .   # 仅 skeleton_assert.go:88
grep -n "LineageUnknown" rl/produce.go                                                          # :165
grep -rn "delete(r\." run*.go                                                                   # 零命中（无界持有）
for k in unknown_schema body_unreadable identity_mismatch attempt_failed completion_malformed; do grep -rn "$k" tests/; done  # 全空
```

---

## 七、本报告明确未覆盖的面（诚实边界）

1. **真实资源面**：真实 OTel collector（`TestRealCollectorDelivery`）、真实 SFT tokenizer/chat template、真实 feedback 端点均未验证（无凭据/无门），其完成性不在本报告判定范围内——本报告只说"机制面可复跑"与"账目与之不符"。
2. **性能/长跑**：三处无界持有的实际增长速率未测（属 soak 面，09.4 范畴）。
3. **9.5/09.21 交叉**：C3 级联的执行路径、golden 四处 drift 的产品侧裁决不在本块。
4. **产品体验**：`examples/wechat-bot` 是否**应该**接 recorder/telemetry 属产品决策（G4 的待裁选项），本报告只陈述"当前未接"。
