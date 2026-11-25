# ORCHESTRATION · bounded-hot-log

## 一、结论

抵达 DoD 的唯一路径：先实测增长构成并形式化“热卷平台”判据（01），再实现搬迁与定位读（02），然后打通消费面并在冻结候选上取有界/损坏/平台证据（03），最后独立审与归档（F）。当前最大风险：C1 的有效预算/结构下限口径未落进实现与锚，以及 manifest 全丢时无法区分“从未提交的临时段”和“已提交但权威丢失”；两项未闭合前禁止归档。

本文件是唯一派发入口；先读本文件与 [design.md](design.md)。

## 二、编排四问

### 次序与触碰集

| 叶 | 前置 | 触碰集（改写 ∪ 验证读取） | 状态 |
|---|---|---|---|
| 01.101 | 无 | 只读 `memory/ledger/**`、临时脚本仓外 | 待 runtime-coherence CI 收口 |
| 01.102 | 01.101 | 本变更文档 | 待 101 |
| 01.103 | 01.101 | 本变更文档＋`api/config` 只读 | 待 101 |
| 01.104 | 102、103 | 本变更文档 | 待 |
| 02.201a 配置面 | 01.104 签发、01.105 单一化 | `memory/ledger/journal*.go`（轮转/封段）、`api/config/{config,decode,fingerprint,clone,skeleton_assert}.go`（三字段声明/严格解码/声明身份面/克隆/骨架断言）、`memory/ledger/config.go`（StorageConfig 三字段＋Engine seam）、`assembly/run.go`（:3463/:3468/:3490）、`assembly/host_document.go`（:1607）同批、测试 | 已实施 |
| 02.201b 段轮转 | 02.201a | `memory/ledger/journal*.go`（轮转/封段/段头/段索引）、测试 | 待 |
| 02.202 | 02.201 | `memory/ledger/archive*.go`（manifest/搬迁）、测试 | 待 |
| 02.203 | 02.201 | `memory/ledger/{segment_store,original*,reference}.go`（GC/收缩） | 待 |
| 02.204 | 02.202、02.203 | `memory/ledger` 定位读＋`api/durable` 文档 | 待 |
| 03.301 | 02.204 | `tool/recall`、`memory/query`、`features/rl`（导出面） | 待 |
| 03.302 | 03.301 | 锚：平台/损坏/回退（assembly tests、`golden` 只读） | 待 |
| 01.105 | 01.101（方向待裁） | `memory/ledger` 正文写入/召回读面、`assembly/facts.go`、`tool/recall` | 待裁 |
| 04.401 | 无（与 02/03 并行） | `.github/workflows/ci.yml`（cache） | 完成 |
| 04.402 | 04.401 | `scripts/gates/race_check.sh`（shard）、`.github/workflows/ci.yml`（matrix＋aggregate）、`test_race_check.sh` | 完成 |
| 04.403 | 04.402＋全部写单收口 | `changes/04-ci-throughput/evidence/`（真 CI 读数） | 完成（fcee2238：race 385s、run 1263s） |
| F0.1 C1 预算口径 | 04.403 绿＋审计发现 | `memory/ledger/journal_kv.go`、`assembly/checkpoint_test.go`、S1 证据 | 完成，全门通过 |
| F0.2 manifest 去歧义 | F0.1 后串行（同包） | `memory/ledger/archive*.go`、归档恢复与损坏负锚 | 完成，全门通过 |
| F0.3 F1 规格文档 | F0.1/F0.2 契约定稿 | 一级/域 specs、主 spec delta、GLOSSARY/wiki/CHANGELOG/OPERATION | 完成，门禁通过 |
| F0.4 04 证据重绑 | 全部写单收口 | 04 evidence/tasks/design/proposal | 完成，最终 CI 全绿 |
| F0.5 跨档枚举 | F0.2 后串行（同包） | `memory/ledger/{journal_kv,archive_read,journal}.go`、全键/namespace/提交序锚 | 完成，全门通过 |
| F1–F4 | 03.302、04.403、F0.1–F0.5 | 合同、CI、归档 | F1–F4 完成，round-2 frontier 空，准予归档 |

并发准入：`memory/ledger` 文件簇共享写面按单串行；`api/config` 五文件由 02.201 单写（与 01.103 契约的字段落点逐条对应），其它叶只读；`tool/recall` 与 `features/rl` 在 02.204 后可并行。计量/长测独占。**04 域与 02/03 触碰集不相交（只碰 workflow 与 race_check），可全程并行**；04.403 的真 CI 读数排全部写单收口后。

### 授权

- 叶粒度 openspec-apply；写面精确到文件；共享面（`journal.go`、`segment_store.go`）编排者单写或串行。
- 禁：复活 `RetireFacts`/`relocate`/冷档探测谓词；改事实身份；删/改已提交字节（搬迁＝移动，归档可读）。
- 两个配置字段（`archive_root`、`hot_log_max_bytes`，可选 `archive_max_age`）为部署可变面，须进声明面与严格解码，不得测试钩子式绕过。

### 凭据

- 每叶三锚：正锚（行为）、负锚（损坏/拒读）、消费者锚（热段未中→归档读的真实入口）。
- 结构断言（S1 同锚出具）：最新 cut 段＋开段不在 manifest（恒热）、全史封段数＝成功 cut 数（热卷常态余最新段＋开段）、失败 cut 不产段界（残帧留开段）。
- 恢复引用读证据：checkpoint 切片引用的 `blob:/ref:` 正文已归档时经定位读逐字节读回——属常规引用读，不计 `archive-tail-read`（那只属损坏恢复）。
- 有界证据只认**跨代平台**（多代、含分母）；单点读数不作结论；归档增长与跨档延迟单列；`hot_over_budget` 记录当前未收敛区间状态（0/1），不记录历史累计或最长时长。
- 崩溃安全证据：搬迁各切点注入中断（copy 后/fsync 后/manifest 前/删除中）→ 重开一致。
- 承诺面证据：01.103 承诺表（id/claim/condition/fails_when/锚成对/canonical 家）；03.302 的 S1＋`TestQuietPointUnreachableToleratesOverBudget` 成对；F3 逐句审计（强度词扫描 warn-only 入档）。常设 commitment 门独立立项属后续变更，本变更只做一次性审计。
- 吞吐证据：真 CI 为权威面——前后 job 分解表（`gh run view --json jobs` 原文）、缓存命中行、`race-aggregate` 状态；冷/热两轮均绿；分片并集自检与缺包负锚绿；证明面零收缩（轮次/变异/包集不动）。

### 释放

| 资源 | 持有者 | 释放条件 | 等待者 |
|---|---|---|---|
| `memory/ledger` journal 簇 | 02.201 | 单完+锚绿 | 02.202/02.203 |
| 归档 manifest 格式 | 01.104 冻结 | 独立审过 | 02.202 起 |
| 编译闭包/计时机 | 各验证叶 | 所依赖写单收口 | 03.302、F2 |

## 三、明细

- 合单判据：`journal.go`＋`journal_kv.go` 同簇；`segment_store.go` 与 GC 叶同簇。
- 预算：01 为只读取证＋文档；02 为核心实现（预计 3–4 单）；03 消费面＋锚（2 单）；04 workflow＋分片（2–3 单）；F 段一次冻结全取。
- 升级说明：契约措辞修订（“恒可定位读取”）须同批写 `docs/upgrad*` 与主 spec 修订记录。

## 四、台账

| 事项 | 事实 |
|---|---|
| 授权 | 用户裁定 A（2026-11-22）；实施授权待 runtime-coherence 收口后按 R31 连续推进 |
| 基线 | `bde7f394` 起（实施时重核） |
| 前置 | runtime-coherence CI 红三簇修复在飞（04.404）；本变更不夹带 |
| 禁止面 | 外来未跟踪九项、真实 `.wechat-config/data`、冻结 archive |
| 切割原语裁定（2026-11-23） | checkpoint cut≡段边界，**同一事件**；唯一切割时点；两投影（切片/段界）；搬迁水位＝倒数第二个 cut；字节超界仅请求提前 cut；安静点不可达→容忍超界入 `hot_over_budget` 观测（用户裁定“等”），禁非安静点封段。初稿 D6 三特判（含锚段不搬的内容检查/跨档尾读常态/窗下限校验）均已废除 |
| 语义偏移检测落点（2026-11-23） | 技能已增 c11（条件强度：canonical 家唯一＋条件句＋成对见证锚＋发现清单）与 ⚙ 工具边界（结构门绿 ⊘ 语义正确）；本变更＝承诺表（01.103）＋两锚成对（03.302）＋F3 逐句审计；强度词扫描器 warn-only 不作门、常设 commitment 门不立项（后续变更候选） |
| 04 CI 吞吐域纳入（2026-11-23） | 用户裁定「与新变更一起纳入计划」。归因：关键路径＝race 单 job 13–15m；资源大头＝event 家族 17 job；浪费＝无缓存/双触发/未分片（run 墙钟 22–26m＝最慢 job 15m＋排队 8–10m）。04 域：04.401 缓存（只加速不判定）→04.402 race 分片（包集单点、并集保真、聚合 fail-closed）→04.403 真 CI 读数。显式不做：轮次/变异不动、双触发去重不碰（另裁）。验收以真 CI 为权威面；与 02/03 并行 |
| journal 优化与 CI 的关系（2026-11-23） | 直接收益≈0：现有 CI 测试不配置 `archive_root`（P5 零迁移），长跑锚 boot 成本不变；有界是部署面收益。条件性收益须先补语义（本轮已补）：开档完整性校验覆盖热段＋manifest，**归档段读时校验、不预扫**——否则热盘有界但 boot 成本仍 O(全史)。补后开归档路径的开档校验/索引重建有界于热窗（S1 断言）；CI 若切归档测试需实测搬迁 IO 净收益，不与 04 域替代 |
| 02 实施进度（2026-11-23） | **02.201a 已实施并提交 bee50839**（三字段/四向拒/位置约束；两锚 PASS）。CI@bee50839 red 归因：①test＝concept-budget 三条（MemorySpec 7>5、StorageConfig 10>baseline7、SegmentCut 新导出类型无 concept binding）；②race＝TestResponseCallCapFuse 95s（本地 `-race` 9.6s 绿＝负载 flake）。**处置**：SegmentCut 已**内部化**——封段触发点移入 `journalKV.Commit`（batch 含 `checkpoint:` 或 `:idx:checkpoint:` 键即在该帧末封段，持同一锁、无并发窗口），删除导出类型与 assembly 的 sealer 接线；**struct 内聚待做**（MemorySpec 收 `Archive *ArchiveSpec` 至 5 字段；StorageConfig 收 Archive 子结构并合并 RustViking 两字段至 7，需级联 rustviking 配置构造点）。**02.201b1 半成品在工作树（未提交）**：`journal_segment.go`（段文件+段头+跨段开档、偏移=原文件逻辑偏移、不截断开段）、Commit 内封段；测试已适配（段含 cut 帧、commits 跨度 1..2/3..4）；ledger 全包绿 90s、assembly TestSegmentRotation 两锚绿。b2（段索引/重建）/b3（失败 cut 负锚）待派；concept 内聚完成后一并提交 |
| 02 进展更新（2026-11-23 晚） | **concept-budget 已清零**：MemorySpec 嵌入 archiveFields（NumField 5）、StorageConfig 嵌入 archiveOptions+RustVikingOptions（7=baseline）；YAML 平铺声明面不变、strict 展平 inline、指纹/克隆/桥级联；四向拒＋位置约束锚 PASS。**02.201b1 已并入提交 3a305649**：封段触发移入 `journalKV.Commit`（checkpoint 键及 `:idx:` 形态识别、持同一锁=无并发窗口、段界=cut 帧末）；段文件+段头+跨段开档；SegmentCut 内部化。门：concept/doc budget ok、comment_policy 0、build/vet/gofmt 干净。**新增验收面**：技能 `validate.sh claims <dir> s5` 要求 `acceptance/claims.tsv`（承诺表机判：命令/red 日志/变异 diff）——按新技能 t7 F3 须 `⚙verify f` 报告；两者登记为 **F 前置**（F3 时产出，格式见技能 selftest）。**残留登记**：concept baseline 1 条 stale（StorageConfig 现≤5，删条目须改 gate 脚本，非叶写面）；`docs/wiki/memory` config.go 行号既有过期。下一步：b2 段索引/重建 → b3 失败 cut 负锚 → 02.202–204 → 03/04。
| CI@3a305649 归因（2026-11-23） | test＝`TestEventIntegrityRestart/positive/after_claim`（owed 1 未结清）＋negative；race＝`TestCompressionCheckpointTracksPendingWork`（alloc 2898>2249）。本地各 `-count=3` 全绿（history 9.7s / assembly 0.36s）⇒ CI 侧非确定性，`gh run rerun --failed` 重跑链；若复现归为独立案件（事件重启 owed 面与 alloc 断言），不混入 201b 判定。
| CI@4db8555b/f8c98697 归因（2026-11-23） | ①test＝`package-layout: memory_declaration_test.go test-mirror-missing`（02.201a 新测试无镜像生产文件）→ 已并入 run_test.go（f8c98697，本地门 812 文件绿、两锚 PASS）；②test＝`TestAnchoredRebuildReplaysTheTail`（decoded 91=91）与 `TestEventIntegrityRestart/before_confirmation`（owed 1）→ 均属既有负载/时序敏感面（前者 04.404c 已登记同形；后者子测在 after_claim/before_confirmation 间漂移）；③**本地 CI 同构复现**：`go test ./... -short` 并发下 `tool/exec TestEventIntegrityExecLifecycle` 红（5.94s），串行 `-count=3` 绿（45.7s）⇒ 既有并发负载敏感面，与 b1 budget 路径无交集（未配置 HotLogMaxBytes 的测试不触发封段）。处置：按「负载 flake 记录后重跑链不修不放宽」rerun；**长期稳定性债登记**：tool/exec 事件生命周期等待面须在 04-ci-throughput 的 CI 稳定性条目承接（分片/等待谓词）。


| CRC 策略裁定（2026-11-23） | 机制保留（真相的具名损坏依赖帧 CRC；成本 4B/帧＋硬件加速），**策略收紧**：开档全校验 O(历史) 是目标优化面（按段校验、下界＝热窗字节，禁以段头自述摘要替代实算）；显式不做：校验和链/Merkle、索引双份冗余、每次读全校验。四层校验职责入 01 契约（帧 CRC/order 行/slot 记录/manifest sha256 各防不同失效）；01.101 增开档校验基线、03.302 读数增第四类开档恢复成本 |
| CI@0cfdceb3 归因与处置（2026-11-23） | race＝`TestResponseCallCapFuse/consumer_path_holds_the_batch_for_approval` 125.03s：awaitAsync 90→120 后仍恰好贴窗缘，失败面为「第二轮请求（回执）在断言时不在 provider 记录里」；test＝`TestInstancesSymptomMultiTargetLedger` 0.13s：账本立即查得 0 条 peer 交付（同刻仅 meditator 一条 settle）。定性：**断言对象未加等待＋风暴窗按空闲机校准**，非产品语义缺陷（DeliveryLedger 每次现查 durable chain，等待可收敛；consumer 路径第二轮决策在批量执行之后落账）。修法（断言值一律不动）：①`stormCallWindow=5m` 作为 200-call 批量的端到端执行预算（ctx 与 awaitAsync 同源）；②`consumerStormReceipts` 轮询至带 tool 回执的请求出现（`consumerReceiptWindow=60s`），超窗具名红；③`deliveryAwaitTarget` 先等目标记录出现再断言计数，MultiTarget 与 TwoEntries 两处接入。验收面：三用例 `-race -count=10` 绿后方可提交 |
| CI@5fa5fab3（2026-11-23） | **全绿**（run 45202110483，含 race/test/event-integrity）：等待面三修复（风暴窗 5m / 回执轮询 60s / 账本目标等待）生效；同 SHA 另有 queued 双触发 run（45202110585）按 04 域「显式不做双触发去重」不处置。主线续：b2 段索引派发中 → b3 失败 cut 负锚（同包须串行）→ 02.202–204 → 03/04 → F |
| 02.201b2/b3 完成（2026-11-23） | **b2 段索引**（子代理实施、编排者复核）：`segments/seg-<id>.idx` 单帧（magic/version/segment_id/entries{key,offset,length,sum}，sum 即帧自身 CRC），tmp+rename 原子；`sealedSegmentIndexOf` 读旁置索引，缺失/不认可则 `rebuildSegmentIndex` 扫段（不落盘），不可重建具名 `ErrSegmentIndexUnrebuildable`；**开档不读索引**（boot 成本不变，索引仅定位加速面，02.204 消费）。**b3 负锚**（编排者）：`TestFailedCutLeavesNoSegmentBoundary`——quiet 标记提交（无 checkpoint 键）不封段、重开仍读回开段帧、后到的 checkpoint 提交段覆盖两提交且段头 cut 正确。**编排者收口**：①Commit 内封段失败改包 `ErrCommitUncertain`（barrier 已过，事实已落，仅边界未立）；②assembly `sealedSegmentStates` 只收 `.log`（`.idx` 旁置文件不得当段读）；③docs/api 重生（新增导出错误）。验证：ledger 全包 ok；`TestSealedSegmentIndexLocatesEveryKey`/`TestUnrebuildableSegmentIndexIsRefusedByName`/`TestFailedCutLeavesNoSegmentBoundary` ×10 绿；concept/dead-producers(3583)/durable-truth/vocabulary/comment(=0)/package-layout(812)/doc-budget/godoc 全绿 |
| CI@0d85d93b（2026-11-23） | **全绿**（run 45205122658）：b2/b3 批次放行。02.201 叶整体闭合（a 配置面、b1 段文件/封段、b2 索引、b3 失败 cut 负锚）。下一叶 02.202 搬迁与 manifest：写面 `memory/ledger/archive*.go` 新文件；manifest 格式以 01.104 契约（contracts/archive.md、signing.md）为准，实施前须通读两契约与 D2 四切点；锚 `TestArchiveCrashRecoveryIsIdempotent`（四注入）、`TestCorruptManifestRefusesArchiveRead` |
| 02.202 完成（2026-11-23） | **搬迁与 manifest**（新文件 `memory/ledger/archive.go`＋镜像测试）：manifest 双槽 generation swap（写新代→barrier→另一槽同代；读者取可解码的较大代，同代异文＝`ErrArchiveCorrupt`）；四态归属严格分离（两槽皆无＝空归档零段；有文件不可读＝Corrupt；陈述段缺＝`ErrArchiveMissing`；字节数/实算 sha256 不符＝Corrupt）。搬迁固定序 copy+索引→barrier→manifest→删热段（段与索引均 tmp+rename）；重复搬迁幂等（已陈述段跳过、热副本仍删）。`recoverArchive` 四切点自愈：未陈述半/整文件删除、已陈述段校验 bytes+sha256（实算）后删热副本、再跑零变化。字段齐契约：key_domain（partition+首末键+键集 sha256）、cut{partition,seq,frame_offset}、offset_map{first/last_seq,frame_count,index_path,index_sha256}、watermark。锚 `TestArchiveCrashRecoveryIsIdempotent`（四切点各含二次幂等）、`TestCorruptManifestRefusesArchiveRead`（空/异文/单槽坏/段缺四态）。注册面：`ledger.archive-manifest`＋`content_addressed_artifacts`（ticket=seg-，sha256 实算校验）。验证：两锚 ×10 绿 7.7s、ledger 全包 89.1s、concept/dead-producers(3605)/durable-truth(0)/vocabulary/comment(0)/package-layout(814)/doc-budget/godoc 绿 |
| CI@86350a03（2026-11-24） | **全绿**（run 45211224594）：02.202 放行。02.203 已派子代理：热盘 GC 与索引收缩（slots 头 Seq/ValuesSize/OrderSize 按新热文件重述、order.log 重写为热段交集、删除只在 manifest 已陈述后、热段键收缩后仍可读）；三锚 `TestHotIndexShrinksAfterArchive`/`TestSlotWatermarkRestatesOnShrunkHotFiles`/`TestGCNeverDeletesASegmentTheManifestDoesNotState`；写面 memory/ledger 最小侵入，若须改读路径则停并报证据。归档键跨档读仍属 02.204 |
| 02.203 完成（2026-11-24） | **热盘 GC 与索引收缩**（子代理实施、编排者复核）：新增 `memory/ledger/hot_shrink.go`（`shrinkHotIndexAfterArchive`）——校验（陈述集=热段前缀、现存段必被陈述、并集无洞、保留段非空）→仅删陈述过的热副本→临时日志+fsync→前缀逐字节校验→剪切→slots 头按新热文件重述→rename→开档重建并核对 manifest 热窗；重复/崩溃以同一前缀判定幂等，manifest 只读不写。具名错误 `errHotSegmentUnstated`/`errHotWindowUnstated`。`journal.go` 最小改（+19）：`baseSeq`＝首段 firstSeq−1，重建索引按段头绝对提交序编号（否则下次 seal 拒封）；无归档根恒 0＝零 diff。三锚 `TestHotIndexShrinksAfterArchive`/`TestSlotWatermarkRestatesOnShrunkHotFiles`/`TestGCNeverDeletesASegmentTheManifestDoesNotState`（另含「已陈述热副本必须删除」正向对照）×10 绿 10.3s；assembly 段轮转锚绿；durable-truth 将 hot_shrink.go 并入 `memory.ledger.local-file-kv` 面；comment(0)/layout(816)/doc-budget/godoc 绿。归档键跨档定位读仍属 02.204 |
| CI@35bec478（2026-11-24） | **全绿**（run 45215327580）：02.203 放行。02.204 子代理定位读/recover-kv 在跑（热索引未中→manifest 段域→归档索引→归档字节校验；三态分离；四锚），其回报后由编排者复核跑门再提交。02 域最后叶＝02.204 |
| 02.204 完成（2026-11-24） | **定位读路径**（子代理实施、编排者复核）：`journalKV.valueLocked` 热槽未中且 archive 非 nil→`archiveReadLocator.read`；manifest 双槽取较大自洽代→段按段号新→旧、key_domain 保守区间→段文件存在/字节数/段头→每视图一次实算 sha256→旁置索引（纯文件名、摘要、帧、条目数）→按条目读帧验 CRC+entry.Sum→取值。三态：无段域命中＝`ErrOriginalMissing`（valueLocked 折为 not-held）；声明而字节不在＝`ErrArchiveMissing`；摘要/帧/段头/条目不符＝`ErrArchiveCorrupt`。热命中/无 archive_root 零 diff。recover-kv 新增 `--archive-root`，经同一定位读校验引用，输出格式不变。锚 `TestIdentityReadCrossesHotAndArchive`/`TestArchiveMissingIsNamed`/`TestArchiveCorruptIsNamed`/`TestRecoverKVVerifiesArchivedReferencesByIdentity`（另加最新写覆盖锚）×10 绿；ledger+recover-kv 全包 89.1s/2.6s；comment(0)/layout(818)/dead(3631)/concept/durable/vocab/godoc/doc-budget 绿 |
| **生产接线债（2026-11-24 新发现，禁止静默）** | ①`moveSealedSegmentsToArchive`/`shrinkHotIndexAfterArchive` 仍无生产调用方（仅测试），运行期不会自动搬迁；②`archive_max_age` 要段的提交时间范围，段头当前无时间元数据；③`shrinkHotIndexAfterArchive` 在活体 `journalKV.Commit` 内直接调用会剪切/rename 日志，而 `k.log.file` 仍指旧 inode，必须 close+reopen 或设计等效所有权转移，否则后续 append 丢数据；④`hot_log_max_bytes` 超界只应**请求提前 cut**，当前未接 assembly checkpoint 触发面；⑤无 Deletes 生产写入方，分段索引无墓碑今日不可达，但未来引入 Deletes 前必须补墓碑契约。**02 域不得宣称闭环，03 S1 不得放行**；下一步先定生产接线设计（cut 提交后维护、活体重载、age 元数据、超界请求），再实施并补真实生产消费者锚 |
| CI@55d08def（2026-11-24） | **全绿**（run 45219461282）：02.204 放行。|
| 02.205 完成（2026-11-24） | **段时间与 age**：record/segment writer v2、reader 保 v1 successor；Commit 每 batch 注入 `committed_at_ns`，段头 `commit_time_min_ns/max_ns`；`StorageConfig.ArchiveMaxAge` 透传；`journalSealedSegment.archiveMovableAt(now,age)`＝age≤0 恒真、未知/未来/max 不严格早于 now-age 则假。**编排者补红锚并修**：`TestArchiveAgeIgnoresOrderedAppendTime` 首跑 0..0 红——v2 `AppendOrdered` 的 quiet 标记（未确认 commit，时间 0）曾把整段污染为未知；修为仅 v1 缺字段算未知 commit，v2 zero 忽略；v1 未知仍保守保留。四锚 ×10 绿 2.5s；ledger 全包/comment(0)/layout(818)/dead/concept/durable/vocab/godoc/doc-budget 绿 |
| 04.401 实现完成、待真 CI 冷/热验证（2026-11-24） | 7 个 Go job 的 setup-go 均开 `cache: true`，`cache-dependency-path` 覆盖 root/wechat-bot/cli-host 三模块 `go.sum`；setup-go 默认 hash 以这些文件内容为键源。workflow 结构门新增正锚 `TestGoCacheCoversThreeModules` 与三路负锚（逐个剥离 go.sum，assertRaisesRegex 真拒绝）；unittest 12/12 OK。缓存不参与判定、不改测试命令。**DoD 未闭合**：须 push 后取两次真 run 的 cache restore/save 原文，冷/热两轮均绿才勾 04.401；fork PR 只读缓存不影响判定 |
| 走查 round-1（2026-11-24，HEAD 2521a513，三路只读审计） | **P0**：①热墓碑未中回落归档旧值→删除复活（`journal_kv.go:351-356`＋`journal.go:1838-1840`＋`journal_segment.go:670-671`）；②删段后 boot 不依 manifest 收敛→错读（`archive.go:403-405`＋`journal.go:732-760`，02.206 承接）；③超限帧长被当 torn tail 截断（`journal.go:663-664,778-784`）；④sync 后 seal 失败 resident 未发布→旧值/重复写（`journal_kv.go:256`，02.206 承接）。**P1**：生产 move/shrink 仅测试（02.206–208）；race waiver 混合 `[build failed]` 返回 0（修复中）；workflow 只钉 15/24 包＋DoD 无 04 真 CI 门（04.402/403）；`KeysSHA` 未验，manifest/索引冲突漏检。**P2**：cut frame_offset 取排序后首个 checkpoint 而非对应 cut；delivery 等待 `>0` 精度不足（已改按期望条数、待提交）；cache 门只查已有 setup-go；04 evidence/冷态/读数口径过期；budget>0 无 archiveRoot 的内部封段边界需澄清。**计划层**：boot「sha256 与 stat 一致」与「禁预扫归档字节」字面冲突，应收紧为「boot 验结构/stat，sha256 读时实算」。处置顺序：206 收口→02.209 墓碑/超限帧/KeysSHA/cut offset→race waiver→04.402/403；同包串行 |
| 02.206 完成（2026-11-24） | **活体维护与 boot 增量收敛**：writer boot 在开 log 前 `convergeArchiveStructure`（manifest 自洽＋段 stat＋删已陈述热副本＋必要 shrink；不读归档正文/不算 digest；完整 `recoverArchive` 保留给搬迁恢复）；`maintainAfterCutLocked(now)` 按最新段 id/manifest 排除/连续前缀/age 选段（预算判定由调用方负责，本叶不自动触发），close old log→move→shrink→重开→替换整个 log/addr，清空 resident 与 verified；不可恢复失败 `ErrJournalMaintenanceFailed` 拒后续 Commit/Read，Close 可释放；可恢复失败 `ErrCommitUncertain` 同 view 可续。**编排者补审计 P0**：resident 发布移到 sync 后、seal 前；新增 `TestSealFailureKeepsCommittedValuePublished`（seal 失败仍 ErrCommitUncertain，但已落值不撤回）×10 绿。验证：三锚 ×10 6.8s、assembly 段轮转绿、八门＋godoc/doc-budget 绿 |
| 02.209 完成（2026-11-24，子代理轮次上限后编排者接管） | **删除墓碑与损坏拒绝**：段索引 entry 增 `Deleted`（omitempty，旧索引兼容）；delete 覆盖旧值、put 覆盖墓碑；热 `journalAddress.lookup` 返回 held/absent/empty 三态，`valueLocked` 遇 absent 直接 not-held、不回落归档；归档读新→旧遇最新墓碑立即 not-held、不继续旧段。超限帧长 >journalMaxFrameBytes 归 `ErrJournalCorrupt`，仅长度合法但尾部不足走 torn-tail；索引解码后按排序键集（含墓碑）实算 `KeysSHA` 与 manifest 比对；`cut.frame_offset` 按 `checkpointCutOfKey` 精确匹配段头 cut，多个同 cut 帧偏移须一致，缺 checkpoint 具名 Corrupt。四锚 ×10 绿 8.96s、ledger 全包 93.5s、gofmt/vet/comment(0)/layout(818)/dead/concept/durable/vocab/godoc/doc-budget 全绿 |
| CI@06fae63f 红归因（2026-11-24） | 双触发中 run 45227354144 绿、45227354132 红：`TestResponseCallCapFuse/consumer_path_holds_the_batch_for_approval` 365.02s，最终失败为 `consumerStormReceipts` 60s 内无第二请求。定性：**共享 runner 负载饥饿复发**（200 次真实 MCP 调用顺序执行，stormCallWindow=5m 仍贴边），非断言错误；禁止继续扩窗/放宽断言。处置：`gh run rerun --failed` 复核；04.402/403 增独立 step/job 或隔离该重路径的稳定性债与真 CI 负载锚 |
| 04.401 冷/热证据（2026-11-24） | run 45225552144（2521a513）success，`event-semantics-symptoms` post setup-go 出现 `Cache saved with the key: setup-go-Linux-x64-ubuntu26-go-1.24.13-...`＝冷态写入；run 45225552346 success，`format-and-contract` setup-go 出现 `Cache hit for: setup-go-Linux-x64-ubuntu26-go-1.27.4-...`＋`Cache restored successfully`＝热态命中。两轮均绿，缓存不参与判定；04.401 可勾（原始 run/log 行已引） |
| CI@06fae63f attempt-2 归因（2026-11-24） | 仍红但失败面漂移：首轮 consumer 风暴 365s（receipts 60s 超时），attempt-2 改红 `TestEventIntegrityRestart/consumer`（after_commit restart drove 0 turns，2.65s）。同一 test job 两次不同红＝共享 runner 非确定性/负载族，非单一产品缺陷；禁止据一次重跑绿宣称稳定。处置：两项均登记 04.402/403 稳定性债；consumer 重路径需独立 step/job，restart 等待面按实际请求计数补有界等待（断言值不变）；当前 3adf2204/57870dd3 的 CI 结果独立观察，不继承 06fae63f |
| CI@57870dd3（2026-11-24） | **两个 run 均全绿**（45231683128、45231683145）：02.209 放行。3adf2204 两个 run 亦全绿（45228095983、45228095997）：02.206 放行。06fae63f 的失败不继承到当前 HEAD；其两条稳定性债继续修 |
| consumer 风暴隔离完成（2026-11-24） | Test(short) root 命令精确 `-skip` consumer 子测试，同 job 下一独立 step 串行复跑（不加 -short、不扩窗、不新增 job）；workflow 门同步两条命令，缺失/重复/迁移负锚真拒绝，12/12 OK。restart attempt-2 红发生在子进程退出后的报告（Requests=0），非父测试少等；当前 HEAD 两轮 CI 绿，证据不足改为等待面，保留 04.403 专项稳定性债 |
| race consumer 隔离完成（2026-11-24） | 60d6ca64 的 test job 已绿（short 隔离生效），race job 仍因同一 consumer 路径 365.03s 红。race 主 step 用 GOFLAGS 精确 skip，下一独立 step `go test -race -count=1 -p 1` 串行复跑；workflow 门新增 race 正锚与删 skip/删独立 step 负锚，14/14 OK。断言/超时不动 |
| CI@e0981aa1（2026-11-24） | **全绿**（run 45322587418）：short/race 两级 consumer 隔离均生效；365s 重路径不再拖垮共享 job。restart 子进程报告红未在本轮复现，保留 04.403 专项债 |
| 02.207 完成（2026-11-24，子代理滞留后编排者接管） | **超界请求提前 cut**：`hotLogBytesLocked` 统计物理热卷（journal＋所有未搬热段/索引＋order/slots/order.line），阈值 `W+2·max_segment`（max 取已封段最大日志区间）；`observeHotBudgetLocked` 超界锁存 `cutRequested` 并计连续区间 `hotOverBudget`，回界清区间；公开 `HotLogCutRequested()`/`HotLogOverBudget()`。普通 Commit 观察；真 checkpoint Commit seal 后清锁存，若此前 requested 且 archive 启用则同锁 `maintainAfterCutLocked` 自动 move+shrink+重开。assembly `noteCheckpointCommit` 查询 probe，超界不等 cadence 直接 `requestCheckpoint`；quiet 不可达只等、不封段，恢复后 cut 清请求但保留观测。两锚 ×10 绿 6.0s、ledger 全包 92.7s、assembly 段轮转绿、八门/godoc/doc-budget 绿 |
| 02.208 完成（2026-11-24） | **生产消费者锚** `TestCommitCutDrivesArchiveAndShrink`：真实 `windowChainRun`＋`commitOnly`，W=1 下两次超界请求 cut，第二次自动写非空 manifest 并收缩 journal（不直调 move/shrink）；同 view 继续提交新事件，旧归档键与新热键均按身份读回逐字节正文；`endRun` 后同配置 OpenTier 重开，两键仍可读。×10 绿 4.25s；负锚沿用 `TestGCNeverDeletesASegmentTheManifestDoesNotState`。至此 02 域生产链路闭合 |
| CI@2f9594af 红归因（2026-11-24） | race＝`TestToolRoundLeavesADebugTrail` 0.00s 缺全局 capture 的 segment close 行；event-short 2/8＝`TestEventIntegrityRestart` unattested exit1，required 连坐。本地 race 观测锚 `-race×10` 绿 1.7s、restart `×10` 绿 30.2s；均为共享 runner 负载/全局 logger 族，非 02.207 回归（未配归档时零 diff）。未改断言，`gh run rerun --failed` attempt-2 in_progress；若复现再将 restart 隔离出短分片并发 |\n| closure 账进度（2026-11-24） | 新增 `acceptance/closure.tsv`/`derived.tsv`，覆盖 10/11 产品叶（segmentIndex/slots-order/residentSet/verifiedCache 四派生）；新门只余 `03.301` 缺账（未实施，三个消费锚不存在），按预期 FAIL，不伪造证据 |
| 03.301 设计门裁定（2026-11-24） | Plan 实证：recall_get 冷读已跨档；trace 父链只走热 order、错误折叠；Run.SessionFacts 来自内存 factOrder/facts；warm resident/segment cache 与 locator verified 缓存可在归档改字节后服务旧副本。按 03 D3 开设计门：扩展写面到 ledger/assembly/recall/rl；FactReader 单契约改 ctx/error＋assembly durable adapter；trace 父链回落 GetEvent；归档来源禁入暖缓存，locator digest 缓存绑定 stat 签名。否决双 reader 并存与吞错 adapter。先 recall/缓存波，再 export API 波 |
| run 归属勘误（2026-11-24，04.403 原始 JSON 复核） | 2521a513 冷轮＝CI `45225552144`（miss→save），热轮＝runtime-contracts `45225552346`（exact hit），两者 key 不同，不宣称同 workflow/key 冷→热；06fae63f 绿轮 `45227354144`＝runtime-contracts，CI `45227354132` attempt-2 仍 failure；e0981aa1 完整 CI＝`45322587406`（`45322587418` 是 runtime-contracts）；2f9594af CI attempt-2＝`45342689398` success；1a9baa98 完整 CI＝`45341892067` failure（event-short 4/8/9 与 test 的 restart 族），后续 2f 才绿。历史行不重写，以此勘误为准 |
| 03.301a 完成（2026-11-24） | **recall＋归档缓存波**：journal resident 只缓存热来源；segment cache 对归档来源绕过命中与写入；locator verified 绑定 `Size+ModTimeNano`，改字节后重算 sha256；前缀读合并归档 index，`EachEvent` 全史枚举。assembly 父链热 order 未中经 GetEvent＋EachEvent 找同 partition/session 最大较新 seq，错误直返；trace 对 ErrArchiveMissing/Corrupt 用 %w 上返，不折 reason。三锚 `TestRecallGetCrossesArchive`/`TestRecallTraceReportsArchiveMissing`/`TestRecallRefusesArchiveBytesThatChanged` 基线红→修后 ×10 绿；ledger/recall 全包 100.9s/1.1s、assembly 读锚 14.2s、八门/godoc 绿 |
| restart 夹具竞态修复（2026-11-24） | `recoverRestartProcess` 对有恢复通知的 phase（after_commit/after_claim/before_confirmation）先等通知模型请求与 sender 答案，再发显式 restart invocation；after_confirmation 已确认不重投，保持直调。等待复用 ctx/restartDrainWindow，超时具名失败；生产/报告字段/断言值不变。`TestEventIntegrityRestart` ×10 绿（编排者复跑 ×3 10.1s）、gofmt/comment(0) |
| 04.402 实现完成、待真 CI（2026-11-24） | race 包集单点 3 分片；`--shard-union-check` 静态/产物并集精确 diff，缺包/重包/空产物红；CI matrix＋`race-aggregate` fail-closed，consumer 与 wechat 各一次；旧单 job 覆盖门迁移到 shard union/aggregate（79 项相关门全绿）。consumer 独立 step 加 `TMPDIR=/dev/shm`，针对 200 次顺序 fsync 的 365s 超时，禁加超时/改断言。全量 runtime 333 项仅余范围外 ignored symptom 注册表漂移。真 CI 分片墙钟/聚合读数待 04.403 补，叶不勾 |
| 03.301 完成（2026-11-24） | **消费面跨档一致性**：03.301a recall 冷/暖/父链＋缓存抗坏；03.301b `FactReader.SessionFacts(ctx,addr)([]event.Event,error)` 单契约，`Run.DurableFactReader` 经 EachSessionFact/GetEvent，全部生产/测试/示例消费者迁移，旧签名零残留。`TestExportReadsArchivedFactsByIdentity` 真实 OpenTier/CommitFact/自动 cut 后导出老事件逐字节一致，缺段/损坏错误原样上返；×10 绿 7.7s，learning 全包 2.8s，RL/示例包与八门/godoc/doc-budget/doc-links 绿。closure 账补 03.301，verifiedCache 失效/重建证据更新为 archiveFileSignature/readInSegment |
| CI@977f495d（2026-11-24） | **完整 CI 全绿**（run 45386819170，28 jobs）：race 3 分片＋race-aggregate、consumer tmpfs 独立 step、restart 顺序修复、03.301b 同批放行。04.402 叶可勾；04.403 分片前后读数由证据代理补算 |
| 04.403 首轮分片读数与重平衡（2026-11-24） | run 45386819170：race 621/381/104s、aggregate 133s，关键路径 624s＝基线 767/897s 的 81.36%/69.57%，**未达 <60%**；run 墙钟降 7.12%/21.12%。包耗时实测 assembly 336s、host 181s、execution 34.6s、history 21.9s、memory 45.3s。重平衡：assembly 独占 shard-1，runtime/memory/tool/execution/history/host 组 shard-2，其余 shard-3；aggregate 删非 smoke 无用 tmux 安装。selftest ALL PASS、79 门绿；新真 CI 读数待取，04.403 不勾 |
| 03.302 完成（2026-11-24） | **S1 多代平台锚**：4 代×4×64KiB 真实 commitOnly/Checkpoint，hot_bytes 830658→833857、archive 0→1091609、cuts 1→4、每代热段恒 1；老键/新键经 GetEvent＋recall_get 逐字节读回；重开 JournalBytesOpen=0、IntegrityBytesScanned=361667≤833857；无归档臂 468514→1556757，同判据以 window-never-fills 拒绝且零 segments/manifest。**S1 暴露真实语义错**：hot_over_budget 原累计历史超界，成功 cut 后仍 1；修为当前连续区间，seal 成功清零，quiet 不可达等待期间非零、cut 后归零。S1/quiet ×10 绿 27.3s、ledger 全包 99.0s、八门/godoc 绿；closure 账补 03.302 |
| race 关键路径第二轮优化（2026-11-24） | ed08b000 CI 45394045955 全绿，但分片 390/484/189s、aggregate 171s；aggregate step 实测 consumer 冷编译 117s、wechat 26s。第二轮：host 移 shard-3；consumer 独立步骤移入已编译 execution 的 shard-2（matrix.shard==2、TMPDIR=/dev/shm），aggregate 只留并集＋wechat；预期关键路径约 430s。race selftest ALL PASS、79 门绿；新真 CI 读数待取，04.403 仍不勾 |
| F1/F3 预审（2026-11-24） | F1 审计：bde7f394 早于四域契约，不能作冻结基线；01.104 冻结点 a4931ded，03 D4 为授权修订。发现 bee50839 把 signing.md 冻结标题改成实施进度，已恢复；以本修复提交重新绑定 F1 基线。F3 结构账新增 claims.tsv/claim-index/round-1，`claims s5`/`verify s5` 均 exit0；frontier 仍含 04.403 收益、canonical 锚缺失、p50/p90、当前树红日志/变异击杀，verdict=不放行 |
| race 关键路径第三轮（2026-11-24） | f6b481f3 CI 45399428118 全绿，但分片 480/293/378s、aggregate 138s（wechat 冷编译 114s），仍未达 60%。第三轮：assembly 按顶层测试名首字母奇偶拆为互补选择器（143/193，零遗漏零重叠），包并集去重、selector 成对校验；wechat 移 shard-3，aggregate 只做结果/下载/并集。race selftest ALL PASS、79 门绿；新真 CI 读数待取，04.403 不勾 |
| 03.302 canonical 锚补齐（2026-11-24） | `TestMissingArchiveIsNamedNotAbsent`/`TestCorruptArchiveIsNamedNotAbsent` 薄封装既有具名错误 fixture；`TestHottestSegmentNeverArchived` 验证候选集排除最新 cut 段；`TestCheckpointSliceReadsArchivedBodyByIdentity` 用真实 checkpoint slice＋OriginalRef，自动搬迁后 OpenTier 按身份读回正文，热卷无旧段副本。四锚 ×10 绿 25.3s、ledger 全包 94.4s、八门/godoc 绿，无需生产改动 |
| F0 独立审计 remediation（2026-11-25） | 完整 CI `45446101670`（5383546b）全绿，consumer 365s 红消失；同 workflow 冷热缓存对照补齐。审计发现并修复：C1 有效预算/结构下限与观测口径、manifest 全丢歧义、跨档 Keys/Namespaces/CommitOrder 漏档及并发快照、热预算生产告警。C1/C6 mutation 已重绑，C1–C7 在最终隔离树全部击杀；主 spec 转正与最终冻结树 CI 仍未完成，禁止归档 |
| 最终归档前安全审计（2026-11-25） | 有效 manifest 下未陈述最终名制品原会被点读忽略；boot 仅按 size 删热副本/缩 journal 可能毁掉唯一良好副本。修复为 manifest 裁决时校验最终名集合，崩溃残留删除前只对对应本代段／索引验摘要，稳态不预扫。CI `45596219143`（a6e953ee）红项 `TestExportReadsArchivedFactsByIdentity` 归因于夹具 `HotLogMaxBytes=1` 已低于结构下限；夹具改为合法 128KiB 预算、3×64KiB 正文与真实 `Run.Checkpoint`，自动搬迁/导出/缺失/损坏断言不变，聚焦测试已绿。新候选仍须完整 CI |
| 04 关键路径口径勘误（2026-11-25） | 统一为 `race-aggregate.completedAt - run.startedAt`，排队与聚合等待不得剔除。`45446101670` 为 466s，对 push 基线 767s 为 60.63%、未达 `<60%`；此前 408s 只是最慢分片完成偏移。`04.403` 保持 pending，最终冻结树须按同一公式重算，禁止用有利口径放行。`9f86cfef` race 门 462s 未达标；`59d918f1` race 门 457s 已达标，但 run 墙钟 1318s 仍比 push 基线高 11s。第三轮把 archive-preflight 改为直接依赖三个证据生产者、与 required 并行，workflow 正负锚 76 项通过；`fcee2238` 完整 CI 全绿，race 门 385s（50.20%）、run 墙钟 1263s（较 push 基线降 44s），`04.403` 两项判据均达成 |
| 最终本地验证（2026-11-25） | ledger 全包 91.368s、assembly 全包复跑 exit 0、learning 全包 2.834s；新增归档安全锚与 S1/quiet/early-cut 锚 ×10 通过，归档安全锚 `-race ×10` 通过。`claims s5` 与隔离树 `claims f` 的 C1–C7 变异全部击杀；当前树 DoD 变异证据 9/9，`lint.sh --no-preflight` exit 0，OpenSpec strict、spec-hygiene、doc-links、doc-budget 全绿。assembly 首轮曾红既有 `TestAnchoredRebuildReplaysTheTail`（91=91），当前树与干净基线聚焦 ×10 均绿，整包复跑绿，未改断言 |
