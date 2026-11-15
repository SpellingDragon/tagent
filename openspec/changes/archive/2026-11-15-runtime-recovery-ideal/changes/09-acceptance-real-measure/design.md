# 04 验收真量 · 设计

> 静态决策见一级 [design.md](../../design.md) D1（P4）、D4（合单）、D6.6（验收真量）、§五（四量冻结签名）、§六（S2 症状直译锚）。本域只列度量面实施要点与实证触碰集。

## 一、四量探针（承 D6.6，量名冻结于 §五）

| 量 | 语义（本域定义） | 取量点 | 出口 |
|---|---|---|---|
| `journalBytes` | boot **开档解码读入**的 journal 字节 | `journalReplay(file, size, …)` 的入参 `size`／返回 `complete`（memory/ledger/journal.go:433/490） | 今日无出口，需只读探针 |
| `indexEntries` | Load 发布后**常驻索引条目数** | `len(s.index)`（memory/ledger/segment_store.go:373，Load 起于 :281） | 常驻索引规模只读出口，经 `Tier.Segments`（config.go:82）；**非 `EventCount`**（全史标量，§五裁定） |
| `durableLoadCount` | boot 一次 `load` 五前缀**读入键数之和** | 五次 `d.scan`（runtime/state/durable.go:259/270/282/294/305）→ `LoadScan{Keys}`（durable.go:348-362） | 既有 `LoadStats()`（durable.go:209），经 `runCommit.inner`（assembly/commit.go:361-365，包内断言 `*state.DurableCommit`） |
| `wallClock` | 真实 boot 入口一次 assemble 的墙钟 | 夹具在 `assemble` 调用两侧计时（消费者入口 run.go:617/690/2417） | 夹具自计；既有 `TAGENT_BOOT_TRACE`（run.go:572-576）只 env 门控打日志，测试不可读，不改它 |

三条语义裁定（防重演量错面）：

1. **`journalBytes` 不是磁盘厚度**。厚度是 S1 的面（06 退位治，§六 design.md:92）；`journalLog.size`（journal.go:501）是 append 偏移（journal.go:582/588 随写增长），**不可复用为开档读量**。探针记开档时刻 `journalReplay` 实际读入的字节，只读、开档后不变。今天二者相等（journal.go:490 整份文件重放）；02 落地后开档读量 < 厚度，06 落地后厚度亦有界——两量分立，各自有锚。
2. **`indexEntries` 与分母分立**。分母 `chainIndexSize`（anchored_boot_parity_test.go:254，`EachIndexNewestFirst` 遍历）量的是链索引条目总数，恒随历史增长（链是唯一真相，P1）。今天 `indexEntries == 分母`（segment_store.go:281/373 全量物化）即违背；修复后 `indexEntries` 有界于存活键集＋尾，分母继续长。锚必须同时断言「分母真增长」与「`indexEntries` 不随之线性增长」，否则锚退化为「链没长」的假绿（既有锚已有此防线，checkpoint_test.go:1256-1259，保留）。
3. **`durableLoadCount` 只覆盖 task 五前缀**。governance 全扫（denial.go:304/327 走 `log.Scan`）不经 `DurableCommit.load`，只落 `wallClock`；该层由 03 收编后追加夹具轮次（W2），本域 W1 不声称覆盖。

**探针形态**：沿用既有只读观测先例（`LoadStats` durable.go:201-215；`indexEntries` 取常驻索引规模 `len(s.index)` segment_store.go:373，`EventCount` segment_store.go:584 仅作链分母 `chainIndexSize` 与观测量、**不作 indexEntries**），不新增缝、不改 `AtomicStore` 四方法（atomic_store.go:50-61）。journal 侧在 `journalLog`／`journalKV`（journal_kv.go:20-33）记开档读入字节的只读字段与只读方法，夹具经 `Tier.Store`（config.go:86）类型断言读取；`durableLoadCount`/`indexEntries`/`wallClock` 零生产出口新增。

## 二、多代＋多任务夹具

- 沿用既有骨架：`noTrimChainRun` + `stopCheckpointFiler`（anchored_boot_parity_test.go:269，trim 关使解码深度可读）、`generations = 5`、每代 `commitSessionTurns(…, 6)` + `CheckpointSession` + `Checkpoint` + `Close`（checkpoint_test.go:1236-1254）。
- **每代追加任务**：`settleTasks(t, ctx, boot, taskCheckpointCalls(fmt.Sprintf("gen-%d", generation), n))`（task_checkpoint_test.go:35/55，注册＋settle 各落一条任务事实）。`taskCheckpointCalls` 以 `rune('a'+at)` 造名，单轮上限 26；代前缀区分身份，跨代不重复。
- **夹具自证**（防「锚绿是夹具空转」）：断言 `durableLoadCount > 0` 且随代增长、每代任务身份不重复、分母 `chainIndexSize` 末代 > 第二代。自证段先于四量断言，自证不成立即具名失败（今天的锚缺此段，故 durable 面从未被量）。
- 存储用 `ledger.StorageLocalFile`（checkpoint_test.go:1234）；该 tier 与 `StorageJournal` 同走 `openJournalKV(filepath.Join(cfg.Root, kvDirName))`（config.go:143/147，`kvDirName = "kv"`，config.go:41），故 journal 探针在两种声明下同一取量点。

## 三、S2 锚断言面（症状直译，W0 红）

S2 症状锚＝`TestSymptomAnchorBootCostFlatAcrossGenerations`（assembly/symptom_anchor_recovery_test.go，W0 新建）：W0 为避开 config-surface 占用的 checkpoint_test.go 另立新锚（去掉 Session 避同名冲突），04 精化其断言面为四真量探针。既有 `TestSymptomAnchorSessionBootCostFlatAcrossGenerations`（checkpoint_test.go:1231，正文计数代理量）保留不动作历史对照（config-surface 占用该文件）。`costs` 由 `CommittedFacts()` 换为四真量序列；正文计数与 `fullReport.Decoded` 对照降为 `t.Logf` 观测量。

- **确定量硬判定**（三量）：末代值对第二代值的比值有界（平坦或近平坦），且每量对分母的比值随代下降至有界。今天 journal.go:490 全读 → `journalBytes` 随代线性增；segment_store.go:373 全量 → `indexEntries` 恒等于分母；durable.go:305 全扫 → `durableLoadCount` 随历史任务总数线性增 ⇒ **三量皆红**。
- **`wallClock` 宽松上界**：墙钟是噪声量，硬判定落在三确定量；墙钟只断言末代 ≤ 第二代 × 宽松倍数（CI 抖动容限），并 `t.Logf` 四量全序列。墙钟单独红而三确定量绿时，按复现率统计定性（≥10 次重复 + 竞态检测），优先怀疑实现，禁改断言绕过。
- **消费者锚**：量取自真实 boot 入口（run.go:690 `restoreCheckpointState` → run.go:2417/2562 `seedAnchoredReplay`），不是手工 rebuild。

## 四、负锚（全史读回归红）

新增 `TestSymptomAnchorBootFullHistoryReadRegression`（assembly/checkpoint_test.go）：同一多代多任务存储上，以 `seedFullReplay`（run.go:2470）走全史并量其四量作对照。

- **对照面自证**（今天绿）：全史四量随代线性增长，证明对照量的是全史而非空走。
- **回归断言**（今天红）：boot 四量 × 2 < 全史对照四量（`journalBytes`/`indexEntries`/`durableLoadCount` 逐项）。01/02 回退为全史读时 boot 四量趋近对照 ⇒ 必红，作一级 F3「完整回滚必红」的度量面证据。

## 五、实证触碰集（✅ grep／读码确认）

- `assembly/checkpoint_test.go:1231-1279`（S2 锚断言面 + 夹具 + 负锚新增）、`:1166`（`commitSessionTurns` 保留）
- `assembly/anchored_boot_parity_test.go:254/269`（`chainIndexSize` 分母、`noTrimChainRun` 保留复用）
- `assembly/task_checkpoint_test.go:35/55`（`settleTasks`/`taskCheckpointCalls` 复用，不新造任务夹具）
- `memory/ledger/journal.go:433/490/501`、`memory/ledger/journal_kv.go:20-33`（开档读量只读探针）
- `memory/ledger/segment_store.go:281/373/584`（只读 `EventCount`，**不改 Load 行为**——Load 有界化属 02）
- `runtime/state/durable.go:203-215/348-362`（只读 `LoadStats`，**不改 load 行为**——改读切片属 01）
- `assembly/run.go:572-576/617/621/690`（只读探针接线点；生产恢复行为与日志面零改动）
- **不改**：`AtomicStore`（atomic_store.go:50-61）、`Tier` 结构（config.go:78-89）、governance（03 域）、退位脚手架（06 域）、主 spec 与 golden（05 域）

## 六、子步序（每步 go build 保持可编译）

4.1 四量探针（只读出口 + 探针自证测试绿）→ 4.2 多代多任务夹具（夹具自证绿）→ 4.3 S2 锚断言面（**W0 红凭据**）→ 4.4 负锚（对照面自证绿 + 回归断言红）。

4.1–4.4 写就即红属 W0 启动门（§六 design.md:95、一级 tasks.md:4）；**转绿在 W1 合单内随 01/02 落地**，本域不得为转绿而放宽断言或改生产恢复行为。

## 七、风险与回退

- **度量扰动 boot**：探针若新增开档 I/O、锁竞争或生产计时面，`wallClock` 就被探针自身污染，重演量错面。约束：只读字段＋只读方法，开档路径零新增 I/O，计时只在夹具侧，run.go:621 既有日志面不变。
- **夹具空转**：任务数为 0 时 `durableLoadCount` 恒 0，锚会假绿。约束：夹具自证段（`durableLoadCount > 0` 且随代增长）先于四量断言，不成立即具名失败。
- **墙钟非确定**：宽松上界 + ≥10 次重复 + 竞态检测（scripts/gates/race_check.sh）；三确定量是主判据，墙钟只作趋势面。
- **语义混淆**：`journalBytes`（开档读量，S2）与 journal 磁盘厚度（S1）、`indexEntries`（常驻索引）与 `chainIndexSize`（链分母）四者分立，注释在取量点固化语义，禁互相顶替。
- **回退**：本域是度量面，回退＝恢复正文计数代理锚（S2 症状回红，作 F3 证据）；生产恢复路径零改动，故与 01/02 的回退（恢复全史读）联动必红。只读探针出口若失去消费方，按 §五 报编排者撤回，不留无消费方的字段。