# 02 有界开档/索引 · 设计

> 静态决策见一级 [design.md](../../design.md) D1（P1/P2/P4）、D6.7、D7（保序陷阱 / 静默降级）、§五共享面冻结签名、§六 S2。本域只列实施要点与实证触碰集。S2 为共享症状锚，度量面归 04，本域不重定义。

## 一、现状机理（✅ 实证）

boot 的索引面分三层，今日三层皆 O(全史)：

| 层 | 入口 | 全史面 | 有界面 |
|---|---|---|---|
| 引擎开档 | `openJournalLog` journal.go:471 → `journalReplay` :433（`offset := int64(0)`） | 读入字节＝整份 journal.log；`index` map 与 `order []string`（:121）随历史键次增长 | 无 |
| 存储发布 | `FileSegmentStore.Load` segment_store.go:281 | 全量读 `{pid}:idx:`（:311）与 `{pid}:meta:`（:339）前缀；positions map（:336）、published（:357-364）、`commitOrderOf`（:365/389） | 正文不读（:281 注释明载） |
| 恢复消费 | checkpoint.go:865 / :985、run.go:2304 / :2565 | 四处各自把全 order 收进 `refs []anchorRef` 后才过滤 | 过滤后正文读有界 |

`journal.log` 只追加不改写（journal.go:40-42），开档唯一的裁剪是 `Truncate` 切撕裂尾（journal.go:495-499）。`EachIndexNewestFirst`（segment_store.go:709）逆序流全 order，早停只省 visit 次数，不省 order 规模。

## 二、设计闸门：索引切片的载体层（须编排者冻结，R17）

一级 D1 反事实（design.md:20）写「删 journal 全量重建改派生加载——无人坏（checkpoint 已带存活键集索引）」。实证核对后该断言在**引擎层不成立**，三条证据：

1. `anchorEntry`（assembly/compress_consumer.go:300-306）只有 `Key/Type/Summary/Content/Time`，**无 partition/window/seq**；事实层存活键集无法派生引擎层 key→span。
2. checkpoint 事实本身落在 journal.log 里，读它需要引擎索引 → 用事实层 checkpoint 派生引擎索引是**循环依赖**。
3. 一次提交写 evt/idx/meta 三键一批（segment_store.go:500-504），事实层存活键集只覆盖投影存活事实，不覆盖三族键，也不覆盖已结事实的身份读（session-checkpoint-material/spec.md:47 要求按身份恒可读）。

候选与裁决倾向：

- **甲 复用事实层 checkpoint 存活键集作引擎索引源** —— 否。上述三条；且会把引擎层有界化押在 W1 之后才稳定的事实层切片上。
- **乙 引擎层自持索引切片文件（倾向）** —— 安静点落一份派生载体：存活键集 key→span、帧序、已提交计数、window meta、切面偏移；临时文件 + 原子 rename 落盘。journal.log 仍只追加。开档读切面 seeding `index`/`order`，再从切面偏移重放尾。切面缺失或损坏 → 具名告警 + 退回全扫重建（与年轻链兜底同一路径）。
  - 先例：`MigrateShardedRoot`（memory/ledger/migrate.go:64）的「复制 + 校验」半段；`ErrSnapshotCorrupt`（memory/ledger/kv_original_store.go:27）已被 `Load` 用于 window meta（segment_store.go:353），复用不新增词汇。
  - 布局约束：`rejectLegacyShardLayout`（memory/ledger/config.go:109-126）拒绝目录与 `shard_*.json`；切面文件名不得落这两类，`migrate.go:25` 的 `snapshot.json` 亦须避开。journal.go:40-42「the file one journal directory holds」注释须就地归位为「日志文件 + 派生索引切片」。
  - 切面是派生态载体非第二真相：丢失可由链全扫重建，不承载任何链上没有的事实。
- **丙 切面作为 journal.log 的一帧 + 反向扫描定位** —— 备选。无新文件、无格式变更（`journalFormatVersion = 1`，journal.go:45，开档拒绝其他版本），但反向扫描须按 `journalMaxFrameBytes`（journal.go:56，32MiB）重叠回退，或给切面帧另设上限；伪命中靠帧 CRC（journal.go:457）拒绝。复杂度高于乙，收益只是少一个文件。

**闸门**：载体层与切面签名属 §五 共享面（编排者单写）。倾向乙，但**编排者冻结前 2.1 不动工**；冻结结果若为丙，2.1/2.2 的触碰集不变，只换定位机制。

## 三、派生加载与尾增量（承 D1 P4）

- **引擎层**：`openJournalLog`（journal.go:471-503）读切面 seeding `index`/`order`/`size`；`journalReplay`（:433）的起始偏移由 0 改为切面偏移，尾增量照旧走 `publish`（:508）→ `indexRecord`（:523）→ `noteOrder`（:534）。撕裂尾 `Truncate`（:495-499）语义不变。切面偏移 > 文件完整尾 → 具名拒绝，不静默全扫。
- **存活键集裁剪**：`order` 只保留存活键集键 + 尾键，按**逆帧序去重**后反写（见 §四）。`index` 只保留存活键集 key→span；已结事实的身份读由 06 退位的透明回冷层承接（§五 design.md:85），本域不删其链上事实。
- **存储层**：`Load`（segment_store.go:281-380）不再全量读 idx/meta 前缀；published 集 = 切面携带的存活键集 positions ∪ 尾（seq > Offset）解析结果。`s.count`（:373）与 `s.windows`（:372/375-378）由切面携带的标量与表派生。
- **计数语义不变**：`EventCount`（segment_store.go:584）仍是全部已提交事件数。故 segment_store_test.go:237/284/290/310/323/383 与 tests/integration/history/storage_capabilities_test.go:80/86/98/175/278/293/346/363 的计数断言**零改**。
- **常驻内存**：`index` / `order` / `windows` 三面有界于存活键集 + 尾；正文仍不驻留，`TestSegmentLoadLazyPeak`（segment_store_test.go:222）保持绿。
- **共享面冲突已报编排者裁定（已解决）**：04 原 proposal.md:15 曾把 `indexEntries` 定义为 `EventCount`（全史标量，量它会把有界实现误判无界）；编排者已裁定 `indexEntries`＝常驻索引条目（`len(s.index)` / `len(s.order)`），**非 `EventCount`**（一级 §五），04 的 proposal/design/spec/tasks 四处已同步归正。本域 2.4 同口径。

## 四、保序不变量（D7 保序陷阱）

`order` 按帧记（journal.go:115-121 注释 + `noteOrder`:534-539）：一键在每条触碰它的记录里都被点名，最新在后；逆序走（`eachKeyNewestFirst`:546-558）先遇最新。任何裁剪或重写存活键集必须**逆帧序去重后反写**，否则认错最新 checkpoint。

钉住的两枚测试（保持绿，不改断言）：

- `TestEachIndexNewestFirstStreamsTheCommitOrder`（segment_store_test.go:508-537）：跨分区提交序 `third/second/first`，早停不排序未达条目。
- `TestJournalStatesItsCommitOrderNewestFirst`（journal_kv_test.go:72-104）：帧序 `1:idx:newer / 1:evt:1:1 / 1:idx:older`，删除键跳过，早停生效。

既有不变量须保持：`commitOrderOf`（segment_store.go:408-410）在 order 条目数少于 published 时具名失败；`TestLoadRefusesAnOrderThatMissesPublishedEntries`（segment_store_test.go:569）钉它。派生路径不得以「切面已给序」为由绕过该检查。无 `commitOrder` 能力的引擎仍走键排序分支（segment_store.go:414-424），本域不改该分支。

## 五、静默跳页改响亮失败（D7 静默降级）

`replayRef`（assembly/run.go:2889-2901）的 `if !held` 由 `return event.CompressProduct{}, false, "", nil` 改为具名 `ledger.ErrSegmentBodyMissing`，与 run.go:2315-2317、checkpoint.go:1003-1005、segment_store.go:608-610 同口径。

**机理精确定性（✅ 读码，修正派发单口径）**：`GetEvent`（segment_store.go:527-556）只在**索引未点名该键**时返回 `(false, nil)`（:537-540）；索引点名而正文读不到时已返回 `ErrSegmentBodyMissing`（:545-547）。今日 `Load` 全量发布，`refs` 来自同一 store 的 order，索引恒 ⊇ refs，故 run.go:2892 的静默分支**当前不可达**——它是潜在降级，不是已在丢事实。**派生加载把它激活**：切面存活键集与常驻索引一旦不同步，活兜底（run.go:2685-2706）逐槽读 pre-boundary 正文即落此分支，静默少一条事实，重启后投影与运行期不等（违 session-checkpoint-material/spec.md:31-34）。

**反事实检验**：删掉静默跳过谁坏？无人坏——`refs` 的键全部来自索引点名，索引点名而读不到即缺陷，不是合法裁剪。合法裁剪只发生在索引**不点名**的键上，而那些键根本不会进 `refs`。故响亮失败不拒任何合法 boot。

**边界区分**：`restoreSurvivors`（run.go:2758-2796）的 not-held 分支已具名 `ErrCheckpointSurvivorUnstated`（run.go:2785，声明 assembly/checkpoint.go:95），且折叠产物凭 payload 携带的正文合法重建（run.go:2787-2794）——该路径**不改**，它不是静默跳过。

## 六、年轻链全扫兜底（保留，不删）

- 无切面（首次开档）或切面损坏 → 全扫重建索引，结果与派生路径逐条一致；损坏具名告警，不静默。
- 无 checkpoint 的年轻会话 → 本链全量回放（checkpoint-state delta :21 已 SHALL）；`restoreChains`（run.go:1073）对切面未点名的链整链折叠，是保持两路一致的兜底。
- `seedAnchoredReplay` 的活兜底（run.go:2685-2706）保留：从未折叠的链 legitimately 需要全扫，这是同一 g(x) 的退化输入，不是第二派生实现。派生加载后 `refs` 只含存活键集 + 尾，兜底遍历的规模已有界。
- **自然成本边界（如实标注）**：从未折叠且历史很长的链，其存活键集＝全部落链事实，索引仍随该链历史增长。这是 O(N) 扫本链的自然成本，不是可消旁路；其有界化靠折叠 cadence 与 06 退位，本域不强裁、不把它写成「已达成有界」。

## 七、第二视图无锁（config.go:174）

`OpenTier` 注释（memory/ledger/config.go:170-176）明载：同一目录的第二运行时实例由**拥有该 root 的生产开档路径**拒绝，不是这里的排他锁；进程内同 root 检查看测试套件；durability 靠整帧追加与日志自身 sync。

故切面不得假设排他：

- 切面落盘在安静点、持 `journalKV.mu`、临时文件 + 原子 rename；写切面不改 journal.log 一个字节。
- 第二视图开档读到的切面可能落后于 journal.log 尾 → 切面偏移必须 ≤ 真实完整尾，尾重放覆盖其后全部帧。
- 切面偏移 > 文件完整尾（外部截断等）→ 具名拒绝开档，不静默全扫、不静默采信。
- 本域不新增排他锁，不改 `OpenTier` 的拒绝归属。

## 八、实证触碰集（✅ grep + 读码）

- `memory/ledger/journal.go`：:40-42（目录注释归位）、:115-121（order 契约注释）、:433-466（`journalReplay` 起始偏移）、:471-503（`openJournalLog` 读切面）、:508-539（publish/indexRecord/noteOrder 尾增量复用）、:546-558（逆序走）
- `memory/ledger/segment_store.go`：:281-380（`Load` 派生）、:389-424（`commitOrderOf` 保序与缺条目检查）、:500-504（三键一批，切面须覆盖）、:527-556（`GetEvent` held 语义）、:584（`EventCount` 语义不变）、:709-729（`EachIndexNewestFirst`）
- `memory/ledger/journal_kv.go`：:33（`openJournalKV`）、:150-162（`eachCommittedNewestFirst`）
- `memory/ledger/config.go`：:109-126（`rejectLegacyShardLayout` 布局约束）、:170-176（第二视图无锁注释）
- `assembly/run.go`：:2304（`eachCommittedEvent`）、:2565（`seedAnchoredReplay` 索引走）、:2685-2706（活兜底）、:2889-2901（`replayRef` 静默分支）
- `assembly/checkpoint.go`：:865（`restoreSessionCheckpoints`）、:985（`eachFactPastCut`）
- 新增切面落盘/读取文件（载体层待 §二 冻结后定名，倾向 `memory/ledger/journal.go` 内就地承载，不新增第二引擎）
- 测试：`memory/ledger/segment_store_test.go`、`journal_test.go`、`journal_kv_test.go`、`assembly/anchored_boot_parity_test.go`、`assembly/checkpoint_test.go`
- **不改**：`memory/ledger/atomic_store.go:50-61`（四方法）、`assembly/run.go:2758-2796`（`restoreSurvivors` 已具名）、`memory/ledger/migrate.go`（只作先例引用）、`runtime/state/durable.go`（归 01）、`features/evolution/governance/**`（归 03）、`openspec/specs/**`（归 05）

## 九、子步序（每步 go build 保持可编译）

2.1 索引切片载体 + 派生加载（闸门冻结后）→ 2.2 尾增量 + 保序裁剪 → 2.3 静默跳页改响亮失败 → 2.4 负锚 + 消费者锚。

2.3 可与 2.1/2.2 并行编写但**合单内串行落地**：响亮失败在派生加载落地后才成为可达路径的守卫，先落 2.3 不改今日行为（分支不可达），先落 2.1 则短暂存在静默降级窗口 → 顺序取 2.1 → 2.2 → 2.3，窗口内由 2.4 负锚钉住。

## 十、风险与回退

- **保序陷阱**（D7）：裁剪存活键集若顺帧序去重会认错最新 checkpoint → 逆帧序去重反写，两枚保序测试 + `TestLoadRefusesAnOrderThatMissesPublishedEntries` 三钉保持绿。
- **切面与链不同步**：切面偏移或存活键集与 journal.log 实际不符 → 具名拒绝（`ErrSnapshotCorrupt` / `ErrJournalCorrupt`），不静默采信、不静默全扫掩盖。
- **计数面误判（已解决）**：`EventCount` 是全史标量，作 `indexEntries` 会把有界实现误判无界 → 编排者已裁定 `indexEntries`＝`len(s.index)`（§五），04 四处已归正，见 §三。
- **年轻链自然成本**：从未折叠的长链索引仍随其历史增长 → §六 如实标注为自然成本，不声称本域已使其有界。
- **回退**：本域是增量派生加载，回退＝关闭切面读写、`journalReplay` 起始偏移回 0、`Load` 回全量前缀扫（症状回红，S2 必红作 F3 证据）。journal.log 全程只追加，回退不删任何已提交事实，切面文件留着不影响全扫路径。与 01/04 合单原子，回退按整单 revert，不单独回退本域造成 boot 路径半新半旧。