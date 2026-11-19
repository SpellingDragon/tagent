# 02 有界开档/索引 · 设计

> 静态决策见一级 [design.md](../../design.md) D1（P1/P2/P4）、D6.7、D7（保序陷阱 / 静默降级）、§五共享面冻结签名、§六 S2。本域只列实施要点与实证触碰集。S2 为共享症状锚，度量面归 04，本域不重定义。
>
> ## ⚠️ 文件级勘误（2026-11-16，用户裁定反转后）
> 本文件横跨两个机制世代：**§一–§十六** 属「退位＋索引切面」世代（`退位` 作为交付、`切面` 作为开档裁剪载体），**§十七起** 属「可寻址索引」世代（R50/R51 采纳可寻址索引、退位整体删除）。读法：
> - **§十一–§十六 中凡「退位／冷层／搬迁／切面读写」的成本读数与设计候选，是已删机制的历史测量记录**，保留为证据（5.8：历史不可变），**不得据以实施**。其读数仍是有效证据的地方在于：它们测出的「整篇重放随历史线性」「索引切面写 O(冷键数)」正是采纳可寻址索引的反面依据。
> - **现行机制**（§十七起与 06.R6／06.R8／S1–S3b 的执行记录）：引擎自持「容量有界槽表 ＋ order 文件」两件载体答键→span 与提交序；开档只读表头恢复水位，窗口＝上次索引陈述水位以来的日志增长，由 64KiB 日志预算（`journalLogSyncBytes`）与索引预算各自界住；停稳根开档读 0 字节。`退位` 与索引切面（`placeIndexCut` 族、`cutStatement`／`IndexCutNotice`）均已删除，**框架零分支**；既有冷档数据由外部脚本 `scripts/fold-cold-root` 折入新根。
> - **派生物不可信时不拒读真相**：索引未陈述／旧格式／表头撕裂 ⇒ 退化读链（整篇重放）＋具名通知，零写盘不变；表头声明字节数大于日志实长（真相已损坏）⇒ 仍 `ErrJournalCorrupt` 拒绝。

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
3. 一次提交写 evt/idx/meta 三键一批（segment_store.go:500-504），事实层存活键集只覆盖投影存活事实，不覆盖三族键，也不覆盖已结事实的身份读（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」要求按身份恒可读）。

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

**机理精确定性（✅ 读码，修正派发单口径）**：`GetEvent`（segment_store.go:527-556）只在**索引未点名该键**时返回 `(false, nil)`（:537-540）；索引点名而正文读不到时已返回 `ErrSegmentBodyMissing`（:545-547）。今日 `Load` 全量发布，`refs` 来自同一 store 的 order，索引恒 ⊇ refs，故 run.go:2892 的静默分支**当前不可达**——它是潜在降级，不是已在丢事实。**派生加载把它激活**：切面存活键集与常驻索引一旦不同步，活兜底（run.go:2685-2706）逐槽读 pre-boundary 正文即落此分支，静默少一条事实，重启后投影与运行期不等（违 `session-checkpoint-material` 的 Scenario「恢复与运行期派生一致」）。

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

## 十一、重整：成本分阶段计量与归因（06.R1 ＝ G3 设计门）

旧设计把「boot 有界」当作一个量，实测证明归因不成立：S2 的 `journalBytes` 从 `[0 134331 326123 557274 827666]` 转绿为 `[0 110 110 110 110]`（ORCHESTRATION R6、run-notes.md:15），其成因是 **08 退位把热层字节变小**，不是 06 的索引机制。故本轮先把计量拆开，再谈实现。

### 十一·一 分阶段计量面（每阶段独立、区间不重叠）

| 阶段 | 量 | 取量点（既有只读出口，需复核） | 与 boot 的关系 |
|---|---|---|---|
| 开档 | `journalBytes`（读入字节） | `journalLog.openBytes`（journal.go:1028 附近） | 属 boot |
| 索引物化 | `indexEntries`＝`len(s.index)`、`orderEntries`＝`len(s.order)`、`prefixPairsRead`（`{pid}:idx:`／`{pid}:meta:` 读到的键数） | `FileSegmentStore.Load`（segment_store.go:346-401） | 属 boot |
| 点读 | 每次按身份读的字节与定位次数（槽表命中／窗口 replay 分列） | `GetEvent`（:527 起）、`journalAddress.read` | 非 boot，长期运行 |
| 提交 | 每笔提交的键数、字节、屏障数 | `StoreEvent`（:560-637）、`Commit` | 非 boot |
| ~~搬迁~~ | **已删**：退位机制整体删除后不再有搬迁阶段。原行测的是 `RetireFacts` 一趟的复制／校验／重写，读数见 §十四·四／§十五·一，仅作历史证据 | — | — |
| 重绑定 | 恢复后重新绑定/重建视图的耗时 | 现有 `wallClock` 之外单列 | 非 boot |
| 总耗时 | 上述区间之外的部分单独记为「其他」 | — | 用于核对不重叠 |

计量纪律：区间不得重叠累计；索引写入不得漏记；`prefixPairsRead` 是**新增的必记量**——今天 `Load` 整表读前缀（:367/:382），把它算进「索引规模」而不是「开档」会掩盖真实成本。

### 十一·二 G3 的必答问题（不答不派 06.R2）

1. **索引载体由谁维护**【问题已由 R50/R51 反转关闭】：原问建立在「切面只在退位时落盘 ⇒ 未退位的部署永远整份读」之上。现行答案是（甲）：**引擎在常规提交点按预算轮换自己的槽表与 order 文件**，不由任何上层机制触发；退位已删，故原候选（丙）无对象、「无切面即整份读」不再是退化路径而是派生物不可信时的**退化读链**。
2. **索引前缀读的取舍**：`Load` 整表读 `{pid}:idx:`/`{pid}:meta:`（:367/:382）。候选：（甲）切面携带 positions 与 window meta，前缀读只补尾（原 2.1 口径）；（乙）保留整表读，但如实标注「规模随存活键集、每键仍整表」；（丙）按需定位（点读时读单键索引）。三者代价不同，不得混称「有界」。
3. **哪些操作不承诺常数**：必须具名列出保留 O(存活键集) 或 O(本链历史) 的操作（例如从未折叠的长链全扫、`EachIndexNewestFirst` 走全序、`Namespaces` 枚举）。
4. **格式与迁移边界**：若切面或索引布局变更，须先完整读取既有 `migrate-kv` 语义、单独迁移、源只读；本轮**不执行**任何迁移，也不在新格式上做就地升级。
5. **冷热界限的语义**：冷热只改变**布局**（哪些字节在热文件里），不删事实；因此「冷读命中缓存」只能改善重复读，不改变首次冷读成本，MUST NOT 被写成 O(1)。

### 十一·三 替代方案与否选理由

- **否「用 S2 单量直接判定本域完成」**【理由随反转改写】：实测该量的红转绿曾来自退位（热层字节被搬小），判定会把别的机制的成果记在 06 账上。现行同一禁令仍然成立、且理由更强：S2 有 `journalBytes`／`indexEntries`／`durableLoadCount` 三道独立界，分属日志层、store 层常驻索引与耐久任务账扫描三层，任何单层转绿都不等于本域兑现——该归因分栏归 09.R2。
- **否「把 `EventCount` 当索引规模」**：全史标量，会把有界实现误判无界（旧冲突已在 04 域归正）。
- **否「先改 `Load` 再说」**：`Load` 的前缀读改造会动 `commitOrderOf` 保序与 6 处计数断言面，收益未量化前属盲改；G3 先出剖面。

## 十二、重整：引擎职责与「不保证 O(1)」（06.R2）

- 索引维护是**引擎职责**：槽表与 order 文件的创建、更新、失效与重建都在 `memory/ledger` 内，消费者不得知道这两个载体的文件名或布局。原括注的 `IndexCutNotice` 例外**已随之删除**（`cutNotice` 无赋值点后成空转，见 R95；06.R9 一并删，`assembly/run.go:617` 的 WARN 消费方同批移除）。
- 交付口径：本域 SHALL 交付一张**具名操作表**，逐项声明「成本随什么增长」（历史／存活键集／尾／常数），未列入者视为未承诺。MUST NOT 用「所有访问 O(1)」概括。
- 【原「冷热界限只改布局（退位控制哪些键在热层）」已废，2026-11-16】退位删除后不存在冷热分层：全部落链事实都在同一个 journal 根里，本域只控制**该根的索引如何加载**。原「两者在 `RetireFacts` 与 `openJournalLog` 交界处必须各自可关闭」随之无对象——`RetireFacts` 不存在，也没有第二个开关。
- 与 11 的接缝：整事实提交的键集（原文族＋事件三键）决定索引必须覆盖的键族；若索引只覆盖事件族，原文族身份读仍会落全扫。此约束不依赖退位，仍成立。

## 十三、重整后的子步序与回退

- 06.R1（只调查/原型）：按 §十一·一 建计量、回答 §十一·二 五问、出 §十一·三 的取舍结论。产出＝本文件与 tasks 执行记录中的证据（命令＋输出＋被测源码身份），不改生产代码。
- 06.R2（依 G3）：按选定候选实现，交付具名操作成本表＋负锚（未维护切面的路径必须可观测地整份读）＋消费者锚（正式 boot 入口）。
- **回退**：整单 revert；索引载体是可丢弃的派生物，回退不删任何已提交事实，也不触发反向迁移。回退后开档回到整篇重放（症状回红，作 F3 证据）。
- **风险**：切面与日志不同步（偏移越过完整尾、存活键集与常驻索引不一致）→ 沿用 §五 的具名拒绝；把别的机制的效果当索引效果 → 用 §十一·一 的分阶段计量挡住；异步维护切面引入新写者 → 必须在同一引擎锁内（`journalKV.mu`）或明确单写者，否则与 11 的提交原子性互相破坏。

## 十四、06.R1（G3）执行记录与判决（2026-11-16）

> **结论勘误（2026-11-18，R262）**：本节原始数值与命令仅保留历史证据。`engineOpen + rebind == bootWall` 由差值定义恒成立，顺序采样的区间和不超过自身总跨度也不证明生产成本没有重复或漏记，故撤销“不重叠实证”的判决。`TestCostPhasesAreMeasuredWithoutOverlap`、`g3CostGenerations`、`g3PrefixPairsRead` 等已删除；旧命令会空跑。`prefixPairsRead` 是测试另行扫描的数量，不等于当前 `Load` 实际读取量。现行计数锚保留，但不能承接完整阶段归因；09.R2 继续待验。本勘误亦适用于下文引用该组读数作归因证据的段落，不改写当时的实验输出。

**源树绑定**：全部读数取自**主树工作树**（HEAD `9547faca` ＋已验证的计量修正：冷读双计已修、`ColdWriteCost()` 已补、S2 非空账户守卫已立、注释门 2 条既存）。**工作树计时**，与绑 HEAD 的 CI 原始记录分列，不混用。生产代码零写；探针经 `go test -overlay` 注入，落 `/tmp/recovery-g3/probes/`，主树只追加三个冻结锚。

**G3 判决：PASS**（计量门四项交付齐、五锚全绿）。**但「恢复成本随历史有界」这一目标未达成**：退位趟的 cut 写实测 O(冷键数)，是具名无界操作，归 06.R2。

### 十四·一 分阶段计量面（七行，含新增必记量）

夹具：`g3CostGenerations` 三代同形链，退位开；末代逐段计时。锚 `TestCostPhasesAreMeasuredWithoutOverlap`。

| 阶段 | 量与读数 | 取量点 | 区间归属 |
|---|---|---|---|
| 开档 | `journalBytes=110`（退位开）／`126865`（退位关，末代） | `journalKV.JournalBytesOpen()` journal_kv.go:103 | boot 内 |
| 索引物化 | `indexEntries=2`、`prefixPairsRead=4`（退位开）／`30`、`52`（退位关） | `Load` segment_store.go:374（idx）、:389（meta）整前缀读 | boot 内 |
| 点读 | 热命中 `2.959µs`；冷首读 `3.293ms`、冷次读起 `4.92µs` | `GetEvent`:651 → `coldEventLocked`:812 | boot 外 |
| 提交 | `82.954ms`/趟；**3 键 179 字节 1 屏障**每笔事实 | `StoreEvent`:567 | boot 外 |
| 搬迁 | `48.141ms`/趟；`coldRead=13.763ms`、`coldWrite=26.658ms` | `RetireFacts`:897、`relocate` journal.go:549 | boot 外 |
| 重绑定 | `11.324ms`（＝boot 14.567ms − 引擎开档 3.243ms） | boot 余量，构造上精确二分 | boot 内 |
| 总耗时 | `1.0013s`；Σ 区间 `145.665ms`；**其他 `855.645ms`** | 夹具全span | Σ≤总 ✅ |

**区间不重叠实证**：`engineOpen + rebind == bootWall`（3.243416＋11.323625＝14.567041ms，逐字相等）；点读/提交/搬迁在 boot 之后顺序测，故互斥。Σ=145.665ms ≤ 总 1.0013s。

**N13 区间重表达（不再沿用「两半」说法）**：实测一趟 `passSpan=25.437ms` 分解为 `coldOpenInterval=6.111ms` ＋ `passRemainderInterval=19.289ms`，和 `25.400ms ≤ passSpan` ✅。另有 `coldMissReadInterval=2.057ms` **在任何趟之外**。故：
- `ColdReadCost` ＝ 冷 miss 读 ∪ 退位开档（实测 8.165ms ＝ 2.054 miss ＋ 6.111 开档）；
- `ColdWriteCost` ＝ 趟 − 已述开档（19.289ms）；
- 二者**不是同一整体的两半**：miss 读不属任何趟。诚实划分是「趟内开档 / 趟内其余 / 趟外 miss 读」三段，前两段之和＝趟span。

**`prefixPairsRead` 取量法（无新增生产出口）**：`Load` 整前缀读且逐 pair 遍历（:374/:378、:389/:393），故前缀 pair 数即实读键数；测试经既有公开 `Store.Read(Fetch{Prefix})` 枚举计数，与 ledger 侧计数替身实测一致（均为 10）。

### 十四·二 五问结论

1. **切面由谁维护**：今日唯一落盘点 `journalLog.rewrite`（journal.go:765）与 `relocate` 的 `cold.placeIndexCut()`（journal.go:589）。实测**退位关＝无切面＝整份读**：`journalBytesOpen=[436 1308 2616]` 对 `commits=[4 12 24]`，严格线性 109 字节/提交（锚 `TestOpenWithoutAnIndexCutPaysTheWholeLog`）。**选丙＋甲**：保留退位触发，但引擎须在常规安静点自持切面，并把「无切面即整份读」写成具名退化（`TailKeys()==nil` 即该退化的既有可观测）。乙（关档更新）不可选：崩溃即无切面。
2. **索引前缀读取舍**：**选乙（保留整表读，如实计量）**。实测退位开时 `prefixPairsRead=4` 恒定（存活键集），退位关时 `[0 22 52]` 随历史线性。故它已有界于存活键集，但**每键仍整表读**，不是按需定位。改甲（切面携带 positions、只补尾）收益未量化前属盲改：会动 `commitOrderOf` 保序与 6 处计数断言。**不得宣称 O(1) 或按需**。
3. **哪些操作不承诺常数**（具名）：①**退位 cut 写 O(冷键数)**——见十四·四；②`EachIndexNewestFirst` 走全合并序 O(热存活＋冷全史)，且 `coldIndexLocked`:1198 每次**整冷前缀读**；③从未折叠长链的全扫 O(本链历史)；④`Namespaces` 枚举 O(命名空间数)；⑤无切面开档 O(整份日志)。
4. **格式与迁移边界**：`journalIndexCutVersion = 1`（journal.go:79），开档拒异版本（:1061）。切面是**可丢弃派生载体**，丢失由链全扫重建，不承载链上没有的事实。本轮**不执行任何迁移**、不在新格式上就地升级；`migrate-kv` 语义须先完整读取、单独迁移、源只读。布局约束：避开 `rejectLegacyShardLayout`（config.go:109-126）与 `snapshot.json`（migrate.go:25）。
5. **冷热只改布局**：✅ 实证。退位不改事实集合也不改提交序——`EventCount` 退位后仍 45（S1 夹具），已搬身份仍按身份可读。**故热冷合并提交序在退位下不变**，这是走查不变量的结构依据（十四·五）。冷读缓存只改善重复读：首读 3.293ms vs 次读 4.92µs（**670 倍**），**MUST NOT 写成 O(1)**——首次冷读仍付整份冷开档。

### 十四·三 引擎候选剖面（同夹具实测）

| 候选 | boot | 提交 | 点读 |
|---|---|---|---|
| **目录保留**（今日整表前缀读） | `433µs`；`journalBytesOpen=110`、`prefixPairsRead=10`、`indexEntries=9`、`EventCount=37` | `5.185ms`/笔；3 键 179 字节 1 屏障 | 热 `10.29µs`；冷首 `3.122ms` |
| **增量维护**（切面＋尾） | 开档字节同上 `110`（切面已覆盖）；前缀读降为尾-only（切面覆盖存活集时 0 pair）——**此格为推导，未建原型** | 同上＋切面维护 O(冷键数) | 同上 |
| **按需定位**（点读读单键） | 不物化索引，boot 近常数；但枚举退化为 N 次单键读 | 同上 | 单键 `13.125µs`(1 pair) vs 整前缀 `37.667µs`(10 pair)，**比 2.87**；枚举 N=10 时 130µs > 37.7µs，**交叉点 N≈3** |

**取舍**：目录保留在枚举上胜（N≥3），按需定位在单点读上胜（2.87×）。今日消费者四处皆先物化再过滤（proposal §Why），故**目录保留是当前形态的正确选择**；按需定位只在点读密集且枚举稀疏时才划算，本轮无此消费者。

### 十四·四 具名操作成本表（草案）与退位 cut 写读数

| 操作 | 成本随何增长 | 实测依据 |
|---|---|---|
| 无切面开档 | **整份历史** | `[436 1308 2616]` / `[4 12 24]` 提交，109 B/提交 |
| 有切面开档 | 切面＋尾（**存活键集＋尾**） | `110` of `498` 字节，`TailKeys()!=nil` |
| `Load` 前缀读 | **存活键集**（退位开）／**整份历史**（退位关） | `4` 恒定 vs `[0 22 52]` |
| 事实提交 | **常数**（3 键 179 B 1 屏障） | `keysPerCommit=3.0 bytesPerCommit=179.0` |
| 热层点读 | **常数** | `2.959µs`／`10.29µs` |
| 冷层首读 | **冷开档**（一次性，后缓存） | `3.293ms` → 次读 `4.92µs` |
| `EachIndexNewestFirst` | **热存活＋冷全史** | `coldIndexRead=2.406→4.692ms`（8→287 冷条目） |
| **退位 cut 写** | **冷键数（无界）** | 见下 |
| `Namespaces` | 命名空间数 | 枚举出口 |

**退位 cut 写＝O(冷键数)，确证**（`placeIndexCut` journal.go:619 → `cutOf()`:627 建整张索引映射 → `writeJournalIndexCut`:1070 整表 JSON）：

| 冷键数 | cut JSON 字节 | `cutOf` 建表 | `json.Marshal` | 落盘 | `placeIndexCut` 合计 |
|---|---|---|---|---|---|
| 100 | 6,594 | 6.29µs | 30.69µs | 1.118ms | 1.292ms |
| 400 | 26,394 | 20.34µs | 94.83µs | 1.074ms | 1.119ms |
| 1,600 | 106,609 | 99.24µs | 394.07µs | 1.642ms | 1.634ms |
| 6,400 | 428,871 | 390.44µs | 1.889ms | 2.860ms | 3.451ms |

**66.0 字节/键**，64× 键数 → 65× 字节、62× 建表、61.5× 序列化，**三者严格线性**。合计墙钟在小额被 ~1ms 文件系统地板掩盖，但 CPU 段（建表＋序列化）线性无掩盖。**这正是 R19 ①最大风险「把正文全扫换成整索引全扫」的兑现**：开档转平的同时，每趟退位付 O(全部冷键)，且冷档只追加故该成本逐趟单调增。外推 10 万冷键 ≈ 6.6MB cut、≈29.5ms 序列化＋6.1ms 建表每趟。**06.R2 必须承接此笔，不得因开档有界即判整体有界。**

### 十四·五 走查不变量的形状裁定（R32 输入）

**缺陷在主树复现**：`eachIndexEntryNewestFirst`:1358-1361 锁内只快照 `total`/`coldBound`，逐步经 `orderEntry`:1425 索引**活的** `s.order`，步间释放 `s.mu`；`RetireFacts`:944-946 整体替换。探针 10 次不同退位落点 → **10/10 走查不一致，累计 65 次重复交付，0 次漏**（漏出现在另一调度，R32 已确证）。

**三形状实测**（热存活 9，冷 8→71→287 条目）：

| 形状 | 8 冷 | 71 冷 | 287 冷 | 堆(287) | 裁定 |
|---|---|---|---|---|---|
| 今日 `coldIndex` 读＋排序（**两形状都已付**） | 2.406ms | 3.507ms | 4.692ms | — | 主导项 |
| **甲 合并序快照** | 4.29µs | 6.88µs | 25.71µs | 24,728B | **推荐** |
| 乙 序号→键映射 | 7.29µs | 13.29µs | 27.54µs | 49,192B | 否 |
| 丙 存储级读写闸 | 走查 152 visit 占 18.223ms | | | O(1) | 否 |

**推荐甲（开局锁内快照合并序，正文按身份读）**，三条实证理由：
1. **成本已被今日走查支付**：`coldIndex` 本就整份物化冷索引并排序（2.4–4.7ms），甲的增量仅 4–26µs，**比主导项小 100–1000×**。R32「快照是 O(全史) 内存」在绝对量上成立，但**冷半今日已付**，甲的**增量内存只有热半 O(存活键集)**（此夹具 9 条）。
2. **甲比乙省一半**：287 条目时堆 24,728B vs 49,192B（**2.0×**），耗时 25.71µs vs 27.54µs。乙还需跨层 seq→key 定位，而该映射本身就是 O(热＋冷) 建表，无成本优势。
3. **不变量实测成立**：退位落在走查中途，快照交付 **52 条目 / 52 身份 / 0 重复 / 0 漏**。**不需要去重集，不需要新锁**，符合 R32 禁令。

**丙（闸）被否，两条实证**：①**重入死锁已复现**——写者待时新读者阻塞，闸内重入永不归零（探针 300ms 超时确证）；②走查持读闸全程 18.223ms，退位趟须等满该span，而 `RetireFacts`:901 全程持 `s.mu`，二者互锁。若仍采闸，必须附重入不死锁实证。

### 十四·六 消费者锚两态读数（布局不冒充机制）

| 量 | 退位开 | 退位关 |
|---|---|---|
| S2 `journalBytes` | `[0 110 110 110 110]` | `[0 134975 327739 559874 831230]` |
| S2 `indexEntries` | `[0 2 2 2 2]` | `[0 43 94 145 196]` |
| S2 `durableLoadCount` | `[0 0 0 0 0]` | **`[0 12 24 36 48]`** |
| S2 `coldReadWallClock` | `[18.30 21.71 23.99 25.68 27.33]ms` | `[0s ×5]` → **冷读守卫触发，锚无法判定** |
| S1 `journal.log` 保留字节 | `[71254 70904 71044 71044 71044]` 平坦 | `[71254 142580 213906 285232 356558]` 线性 |
| 本域 `prefixPairsRead` | `[0 4 4]` | `[0 22 52]` |

**结论**：S2 四量**全部**借退位的果，非索引机制独立兑现（退位关时 `journalBytes`/`indexEntries` 两量在代 2/3/4 均破 2× 界）。**新发现**：`durableLoadCount` 亦随退位开关翻转（0 ↔ 线性），故 N11 的「零是正确有界行为」只在退位开时成立。S2 锚在退位关时**冷读前置守卫先触发**，结构上无法判定该态——须由 09.R2 分栏。

### 十四·七 冻结锚建立与实跑（全部「本轮新建」）

| 锚 | 包 | 状态 | RUN/PASS | 原始 exit |
|---|---|---|---|---|
| `TestCostPhasesAreMeasuredWithoutOverlap` | assembly | **本轮新建** | 1/1 | 0 |
| `TestCostAttributionSeparatesBootFromRelocation` | assembly | **本轮新建** | 1/1 | 0 |
| `TestOpenWithoutAnIndexCutPaysTheWholeLog` | memory/ledger | **本轮新建** | 1/1 | 0 |
| `TestSymptomAnchorBootCostFlatAcrossGenerations` | assembly | 既有复跑 | 1/1 | 0 |
| `TestJournalBytesBoundedByLiveSet` | tests/integration/history | 既有复跑 | 1/1 | 0 |

合并命令实跑：`go test ./assembly/ -run '^TestCostPhasesAreMeasuredWithoutOverlap$|^TestCostAttributionSeparatesBootFromRelocation$|^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1 -v` → **exit 0，3 RUN／3 PASS／0 FAIL**（非空转）。`go build ./...` → **exit 0**。注释门 `comment_policy -no-baseline assembly memory` → **2 findings，均为 03.R1 的 `assembly/recovery_judgment_test.go:102/:124` 既存项，本域新增 0**。

**未量项（如实登记）**：①增量维护候选的前缀读降为尾-only 是**推导非原型**；②cut 单段精确归因需 `RelocateReport` 加字段（journal.go，R1 白名单外）；③`coldIndexLocked` 枚举读仍未计成本（N13 遗留）；④失败退位两半都不计。**G1/G2/G4 未由本叶判定，产品不可发布。**
## 十五、06.R2 执行记录：具名操作成本表与退位 cut 写有界化（2026-11-16）

**源树绑定**：主树工作树（HEAD `9547faca` ＋本轮改动），计时为工作树计时，与绑 HEAD 的 CI 记录分列。探针/日志落 `/tmp/recovery-06r2/`。

**两项交付结论先给**：
- **①切面的维护面**：**不变**（唯一落盘点仍是退位重写与退位趟的冷切面）。G3 的丙＋甲选择按**具名退化**落地：「无退位的部署永远整份读」现由引擎**具名报告**（见下 §十五·三），不是静默整读。
- **②`Load` 的整表前缀读**：**保留**（G3 选乙），本域在成本表内标注为「随存活键集、每键整表读」，**不称 O(1)、不称按需**。
- **退位 cut 写**：**由 O(冷键数) 收为 O(本趟搬迁键数)**，红线—绿对照见 §十五·二。
- **但整趟退位仍是无界的 O(冷键数)**，成因是**两次冷开档**（复制前开、校验前再开），与 cut 写无关。**开档有界不等于趟有界**，本域不得据此宣称「恢复成本随历史有界」已达成。

### 十五·一 具名操作成本表（06.R2 交付③；未列入者视为未承诺）

| 操作 | 成本随何增长 | 实测依据 |
|---|---|---|
| 无切面开档（日志整份重放） | **整份历史** | G3 `[436 1308 2616]` 对 `[4 12 24]` 提交，109 B/提交；本轮 `TestOpenCostIsBoundedByTheCutItKeeps` 无切面根整份读 |
| 有切面开档 | **切面＋尾**（＝切面覆盖的存活键集 ＋ 切面之后的帧） | 本轮 `log bytes [417 834 1251 1668]`、`replayed [89 89 89 89]`（历史 4×增长、读量不变）；冷开档 G3 `110` of `498` |
| `Load` 前缀读（`{pid}:idx:`・`{pid}:meta:`） | **存活键集**；**每键仍整表读**，非按需、非 O(1) | G3 `prefixPairsRead` 退位开 `4` 恒定，退位关 `[0 22 52]` 随历史线性 |
| 事实提交 | **常数**（3 键 179 B 1 屏障） | G3 `keysPerCommit=3.0 bytesPerCommit=179.0` |
| 热层点读 | **常数** | G3 `2.959µs`（∅）／`10.29µs`（剖面夹具） |
| 冷层首读 | **冷开档**（一次性），命中缓存后常数 | G3 `3.293ms` → 次读 `4.92µs`；**MUST NOT 写 O(1)** |
| `NewestOfType`（退位开，复制热序） | **存活集**（非历史） | R36 残留登记，本轮未改 |
| `EachIndexNewestFirst` 全序走查 | **热存活 ∪ 冷全史**（`coldIndexLocked` 每次整冷前缀读） | G3 `coldIndexRead=2.406→4.692ms`（8→287 冷条目） |
| 未折叠长链全扫 | **本链历史** | G3 五问③ |
| `Namespaces` | **命名空间数** | G3 五问③ |
| 退位 cut 写（热） | **存活键集**，且与它伴随的 `rewriteInto` 同阶（每保留键一帧） | 本轮：该写就在 `rewriteInto` 的保留键循环内，无独立加项；冷切面参见下一行 |
| **退位 cut 写（冷）** | **本趟搬迁的键数**（**本轮由冷键数收为此**） | 见 §十五·二 |
| **整趟退位** | **冷键数（仍无界）** | **两次**冷开档各付一次全冷索引（`relocate` 复制前 `openJournalLog`、`verifyColdCopy` 再开），G3 一趟 `48.141ms`（`coldRead=13.763ms`＋`coldWrite=26.658ms`） |

### 十五·二 退位 cut 写：由 O(冷键数) 收为 O(本趟搬迁键数)

**改法**：`journal.index` 由「一整份 JSON」改为**一次落盘一行**（JSON Lines）。退位趟的冷切面由 `placeIndexCut()`（journal.go:619）改为 **`appendJournalIndexCut`**，只陈述**本趟在切面之后追加的键**（`journalLog.tail` 即该键集，`noteTail` 在 `append` 时已记）；开档 `readJournalIndexCut` 逐行解析并归并（`journalCutOrder` 复算「每键一次、按最新帧位」的序，与原单份切面同形）。热日志的 `rewrite` 仍用 `writeJournalIndexCut`（temp＋rename 整份替换），因为它伴随的正是整份重写。

**格式与迁移**：`journalIndexCutVersion` **仍为 1**——单行 JSON 是合并后的一行，旧单份切面文件天然可读（一行即一语句），**无迁移、无就地升级**；切面仍是可丢弃派生载体（缺失/解码失败/解不出即整份读）。

**红线—绿对照（同一夹具、同一命令，`TestColdCutStatesOnlyTheKeysItsPassMoved`）**：五趟、每趟 40 键，冷史 5,140→25,700 B。

| 实现 | 每趟写下的 cut 语句字节 | 整份切面字节 | 退出码 |
|---|---|---|---|
| 改前（每趟整份重述） | **`[2190 4334 6514 8694 10874]`**（随冷史 5.0×） | `[2190 4334 6514 8694 10874]` | **1**（红，原文：`pass 2 wrote 6514 byte(s) of cut beside the 2190 of the first pass, so the statement grows with the cold history behind it`） |
| 改后（本趟增量） | **`[2190 2203 2241 2241 2241]`**（平坦，±2.3%） | `[2190 4334 6514 8694 10874]`（仍随冷键数线性） | **0**（绿） |

按 66 B/键的 G3 口径：本趟 40 键 ≈ 2.2 KB，与冷史无关；G3 的 6,400 键 = 428,871 B/3.451 ms 那一笔记的是**整份重述**，本轮起只出现在「切面丢失后的重建」与**热日志 rewrite** 两处。**归并侧的读不新增成本**：每键只出现在一条语句里（冷档只追加，已持有的键不再追加），故合并仍是 O(冷键数)，与冷开档本身同阶。单次语句计时（同键集）：本趟 40 键 `596.404µs` vs 整份 200 键 `558.392µs`（两者都含一次文件开关的地板，故绝对值不构成结论，**字节序列才是判据**）。

### 十五·三 交付①的具名退化声明（逐字）

> **⚠️ 本节已整段失效（2026-11-16）**：其载体 `cutStatement()`／`IndexCutNotice()` 在 S3a 后无赋值点、成空转，06.R9 已一并删除（R95 裁定，`assembly/run.go:617` 的 WARN 消费方同批移除）。**退位删除后不再有「本档是否维护切面」这个配置事实**——索引由引擎自持，不存在「无退位的部署」这一形态。故下文逐字声明与「09 的 S2 归因据此分栏」中的「退位关态」分栏**均无对象**；S2 的现行分栏依据是三道独立的界（日志层窗口／store 层常驻索引／耐久任务账扫描），见 §二十五与 R102。原文保留为历史证据。

> **a deployment without retirement always reads whole**——无退位的部署永远整份读。引擎在 `journalLog.cutStatement()` 逐字报告：`no index cut beside <dir> and this tier maintains no cut, so the open read the whole log`，经 `IndexCutNotice()` 交装配层（`assembly/run.go:629-634` 以 WARN 陈述）。声明由**存储层**交给引擎：`FileSegmentStore.WithRetirement` 把「本档是否维护切面」状态给引擎（`cutMaintenance`），故「本档不维护切面」是**配置事实**而非引擎猜测；**维护切面但尚未落过盘的首次开档不报警**（那是「一次都没覆盖的日志」而非「本档不维护」）。

**09 的 S2 归因据此分栏**：S2 的 `journalBytes`／`indexEntries`／`durableLoadCount` 在**退位关**态下的线性是**已声明退化**（本档不维护切面 ⇒ 整份读），不得记为 06 未兑现；06 的兑现面只以**退位开**态的「开档＝切面＋尾」与「cut 写＝本趟键数」为凭据。S2 锚在退位关态因冷读前置守卫先触发而结构上无法判定该态，仍归 09.R2。

### 十五·四 冻结锚（三枚本轮新建，一枚既有复跑，一枚他行既有）

| 锚 | 包 | 状态 | 说明 |
|---|---|---|---|
| `TestOpenCostIsBoundedByTheCutItKeeps` | memory/ledger | **本轮新建** | 多代重开档：读入＝切面＋尾（`[89 89 89 89]` 对 `[417 834 1251 1668]`）；无切面根整份读**且具名退化** |
| `TestIndexMaintenanceRunsOutsideTheRelocation` | memory/ledger | **本轮新建** | 退位关：无切面 += 引擎具名「maintains no cut」；退位开：切面可判读、`Offset>0`、notice 为空 |
| `TestColdCutStatesOnlyTheKeysItsPassMoved` | memory/ledger | **本轮新建** | cut 写有界（红线—绿对照见 §十五·二），含整份重述的破坏用例 |
| `TestDerivedIndexNeverSkipsAKeyItPublished` | memory/ledger | **本轮新建** | 切面少陈述一条已发布索引键 ⇒ `commitOrderOf` 具名拒绝（`the engine states the commits of 2 of the 3 index entries it published`） |
| `TestOpenReadsTheLogWholeBesideNoIndexCut` | memory/ledger | 既有，保持绿 | 无切面整份读 + `TailKeys()==nil`（断言未改） |

### 十五·五 本轮未改与残留（如实登记）

- `FileSegmentStore.Load` **未改**（保留整表前缀读）：改它会动 `commitOrderOf` 保序与 6 处计数断言面，收益为零、残余风险为正（G3 乙）。
- 【原「整趟退位仍 O(冷键数)」已无对象，2026-11-16】退位删除后没有「整趟」这个操作；该成本随机制一并消失，其教训（冷开档每次付全冷索引）已由可寻址索引吸收：开档只读表头。
- 原登记的 `cold_archive_test.go:537/:559` 旧文案已随该文件整删（08.R3b）清偿，不再是残留。
- `placeIndexCut` 的冷路径不再经 `*.tmp`＋rename：追加行天然是「读者要么见到旧语句、要么见到新语句」，中途中止只留下一条**撕裂末行**，开档按「末行解不出 ⇒ 停在该行之前」处理（前面语句仍立，未陈述的帧照旧重放）。

## 十六、G5 设计门：索引物化成本实测＋寻址式索引原型＋退位收益审计＋S1 量面审计（2026-11-16）

**性质**：调查与原型计量，非生产实现。不反转 D6.13，不改 S1，不勾任何任务框。

### 十六·〇 隔离树构造与源身份

- **构造法**：`rsync -a` 主树**工作树**（非 HEAD）至 `/tmp/recovery-g5/tree/`，排除 `.git`、`examples/`（独立 Go module，不在根 module 构建闭包）、`docs/.dev`（两个文件读取超时，非 Go 面）、以及未跟踪残渣 `.tmp/`、`.tmux-*.test`（复制后从副本删除，主树未动）。
- **源身份**：HEAD `9547faca2327c31579e91fbe40a27405d3ffdfb3` ＋工作树未提交改动（`memory/ledger`＋`assembly`＋`tests/integration/history` 共 34 文件，+2855/−346）＋未跟踪新文件。`journal.go`/`segment_store.go`/`journal_kv.go`/`config.go` 经 sha256 与主树逐字节相等。
- **验证**：隔离树 `go build ./memory/ledger/ ./assembly/` exit 0、`go vet ./memory/ledger/` exit 0。主树**零 `.go` 写、零 go 运行**。
- **探针**：`memory/ledger/g5_materialization_test.go`（probe A）、`g5_retirement_buys_test.go`（probe C）落隔离树；寻址式原型 `/tmp/recovery-g5/proto/`（独立 module `g5proto`）。日志 `/tmp/recovery-g5/probeA-full.log`、`probeB.log`、`probeC.log`。
- **计量纪律**：每量各给可构造的增长案（见下）；区间不重叠（开档三相位各自独立计时，wholeOpen 另测一次，**不把相位加总冒充 wholeOpen**）；冷开档双计已避开（firstWalk 与 cachedWalk 分列，coldReadCost 单列）；实跑测试数 probe A 4 子测全 PASS（390.5s）、probe C 1 PASS（57.7s）、原型 4 深度全 exit 0。**绝对值在 probe A/B 并发 I/O 竞争下取得，增长轴与平坦轴的对比是判据，绝对毫秒数含负载噪声**。

### 十六·一 索引物化成本随历史增长（probe A，实测，两 tier，与帧重放分离）

夹具：每深度 D 提交 D 个 fact（各 64B），退位开保留末 1 个、其余搬冷。冷键数≈2D（idx＋evt）。深度 400/1600/6400/25600（冷键 798/3198/12798/51198，64× 跨度）。

**冷开档（退位开，冷史=深度）——物化主导，帧重放可忽略**：

| 冷键数 | cut 字节 | cutParse（读+JSON 归并+排序） | seed（建 map+order） | tailReplay | wholeOpen（另测） |
|---|---|---|---|---|---|
| 798 | 52,422 | 1.813ms | 56µs | 26µs | 3.29ms |
| 3,198 | 214,402 | 3.215ms | 188µs | 16µs | 5.75ms |
| 12,798 | 871,605 | 6.863ms | 478µs | 14µs | 8.83ms |
| 51,198 | 3,552,681 | 22.761ms | 1.869ms | 8µs | 25.45ms |

cutParse 1.81→22.76ms（12.6×）、seed 56→1869µs（33×）随冷键 64× 增长；tailReplay 恒 ≤26µs。**冷开档成本是索引物化（cut 解析＋建表），不是帧重放**。wholeOpen 含 ~1.6ms 文件系统地板。

**帧重放分离（冷整份重放，切面丢失重建面；三相位各独立计时）**：

| 冷键数 | readFrames（读+CRC，不解析） | parseSpans（+解析） | fullPublish（+建索引） |
|---|---|---|---|
| 798 | 158µs | 637µs | 1.225ms |
| 3,198 | 188µs | 2.151ms | 2.516ms |
| 12,798 | 356µs | 5.389ms | 6.469ms |
| 51,198 | 1.077ms | 17.185ms | 20.883ms |

readFrames 158µs→1.077ms（6.8×）vs fullPublish 1.225→20.883ms（17×）：**物化（解析+建表）增长远快于裸帧读取**，二者是不同成本。

**热开档（退位开，存活集=3 键）——平坦**：cutKeys 恒 3，wholeOpen 3.90/3.14/2.15/1.65ms（文件系统地板，无增长）。**热层之所以显得有界，是退位把存活集压到 3，不是索引模型有界**。

**热开档（退位关，无切面，整份重放）——随历史增长**：

| 键数 | readFrames | parseSpans | fullPublish | wholeLog 字节 |
|---|---|---|---|---|
| 801 | 241µs | 1.189ms | 1.559ms | 157,420 |
| 3,201 | 354µs | 4.294ms | 4.389ms | 634,425 |
| 12,801 | 1.676ms | 15.538ms | 11.655ms | 2,554,425 |
| 51,201 | 3.474ms | 37.602ms | 40.532ms | 10,374,830 |

fullPublish 1.56→40.53ms（26×）随键 64×。**退位关时热开档物化整份历史——结构缺陷暴露：索引必须全物化才能答任何东西**。

**Load 前缀读面（冷，`Read(Fetch{Prefix})`＝keys 走查+排序+逐键 ReadAt）**：pairs 399→25599，Read 212µs→314µs→6.10ms→21.83ms（103×）；keys 走查 57→3170µs（56×）。

**整趟退位（probe A passWall / report.ColdOpenCost）**：passWall 50ms→121ms→325ms→2.10s；ColdOpenCost（两次冷开档）8.4→13.0→12.8→51.6ms。**整趟退位 O(冷键数) 确证，与 R47 一致**，成因即两次冷开档各付一次全冷索引物化。

### 十六·二 寻址式索引原型（probe B，实测；键→span 按需读，开档只读 root＋尾）

原型结构：append-only `values.log`（帧）＋每键一个 `idx/<hex(key)>` span 文件（提交时 temp+rename）＋独立 append-only `order.log`（每提交一行，**内联 span**＝变体 B）＋`root.json`（seq/size）。**开档只读 root.json，不物化任何索引**。点读＝读 idx 文件＋ReadAt value span（两次小读）。提交＝append 帧+fsync＋写 idx+append order+fsync＋写 root（O(1)）。枚举＝逆序走 order.log（64KB 块 ReadAt），按键去重交付一次。

| 深度 | open | pointOldest | pointNewest | commit | newestOfType(checkpoint) | enumInline(B) | enumIdxLookup(A) | materializingOpen(控制) |
|---|---|---|---|---|---|---|---|---|
| 400 | 3.03ms | 676µs | 689µs | 32.8ms | 75µs walked=2 | 563µs | 200ms | 73ms |
| 1,600 | 3.02ms | 1.00ms | 927µs | 35.8ms | 82µs walked=2 | 1.39ms | 329ms | 293ms |
| 6,400 | 1.72ms | 766µs | 600µs | 14.8ms | 68µs walked=2 | 5.58ms | 1.17s | 1.13s |
| 25,600 | 3.25ms | 1.32ms | 558µs | 15.2ms | 121µs walked=2 | 16.4ms | 5.53s | 4.70s |

- **open 平坦**（3.03/3.02/1.72/3.25ms，64× 键无增长）；**point-read 平坦**（~0.6–1.3ms）；**commit 平坦**（fsync 地板，无增长趋势）；**newestOfType 平坦且有界前缀**（walked=2，~75–121µs）。
- **可失败案（控制）**：`materializingOpen`（开档读全部 idx 文件）73→293→1132→4698ms，64× 键→64× 时间，**线性增长**——证明 open 的平坦不是恒真界，物化式开档确会增长。
- **枚举反对意见的判定**：变体 A（每键按需定位，order 不内联 span）200ms→5.53s，**比变体 B 慢 ~200–350×**——G3 在热路径否掉按需定位的枚举代价**对变体 A 成立**。变体 B（独立 append-only order 内联 span）563µs→16.4ms，是**顺序读**，与今日 cachedWalk（334µs→31.8ms）同阶，**且无需开档物化、无需冷开档**。**独立 append-only order 结构化解了枚举反对意见**：枚举仍 O(历史)（顺序读 order 全文），但常数小、无物化、无排序；newestOfType 只读有界前缀。
- **与今日枚举对比**：今日 firstWalk（付冷开档）5.04→60.2ms，cachedWalk 334µs→31.8ms；原型 enumB 563µs→16.4ms。**全序枚举两者都 O(历史)，谁都不_bound_它**；寻址式索引去掉的是开档物化与冷开档，不是全序枚举的历史跨度。
- **原型 sanity**：delivered=深度+1（=去重后键数），newestOfType 返回最新 checkpoint 键（i%40==39）。**全序与真实引擎热冷合并序的等价未独立验证**（原型单 order log，按构造逆序即提交序；真实引擎的热冷合并、退位中途快照不变量 R36 未在原型复现）。

### 十六·三 退位仍买什么（probe C，实测，深度 6400）

- **磁盘介质**：hotDev=coldDev=16777233，**sameDevice=true**。冷根=`cfg.Root/cold`（config.go:307，唯一生产 WithColdRoot 调用方）。**冷热同一目录树、同一磁盘，无介质分离**。
- **磁盘总量**：退位开 total=2,356,948B（hotTree=814＋coldTree=2,356,134）；退位关 total=2,554,425B；delta=−197,477B。**该差额是冷拷贝按 256 键/帧批处理摊薄帧头的副作用，不是 tiering 的磁盘收益**——同盘上把字节从热搬到冷不省盘。热 journal.log 单独：开 578B vs 关 2,554,425B（**S1 量的正是这个热文件尺寸**）。
- **常驻索引**：开 IndexEntries=1 / EventCount=6400；关 IndexEntries=6400 / EventCount=6400。退位把常驻索引压到存活集。
- **枚举跨度**：全序走查 visits 开=6400 / 关=6400。**退位不 bound 枚举**，走查仍跨全史（与 §十六·一一致）。
- **冷点读**：settled fact 首读 coldReadCost=27.38ms（付全冷开档物化），passColdOpenCost=11.66ms。**今日冷点读首读随冷史增长**（寻址式原型点读 ~0.6–1.3ms 平坦）。

**结论（实测）**：若开档与点读由寻址式索引 bound，退位剩下的收益是——(1) 热文件字节小（S1 磁盘面）；(2) 常驻索引 O(存活集)；(3) 热开档 cut+尾有界。其中 (2)(3) 被寻址式索引**取代**（开档 O(1) root 读、无常驻索引）。(1) 是磁盘尺寸面，非成本症状。**退位不买：介质分离（同盘）、枚举界（走查跨全史）、可归因于 tiering 的磁盘节省（−197KB 是再批处理副作用）**。

### 十六·四 S1 量面审计（读码，不改）

- **S1 量什么**：`TestJournalBytesBoundedByLiveSet`（tests/integration/history/symptom_anchor_journal_test.go:89/:98）用 **`os.Stat(journalPath).Size()`** 取热 `journal.log` 的**文件真实字节**，判据 `retained[last] ≤ 2×appended[last]` 且 `retained[g] ≤ 2×retained[1]`。**量的是磁盘文件尺寸，不是开档读入字节、不是重放字节、不是成本**。
- **S1 文档的消费者声称已陈旧**：:28 写「the bytes are the file the journal engine opens at boot (**openJournalLog replays 0..size**)」。但今日 `openJournalLog`（journal.go:996-1008）在切面可置时从 `cut.Offset` 重放，**只读尾，不读 0..size**。S1 夹具退位开（:48 RetireFacts:true）维护切面，故该「文件字节=开档重放字节」的等式**在现引擎为假**。这是 N6/N11 同类的**陈旧代理**：S1 把磁盘尺寸当作开档成本的代理，而该代理对应的机制（整份重放）已被切面退役。
- **S2 量什么（对照）**：`assembly/symptom_anchor_recovery_test.go` 的 `journalBytes`=`JournalBytesOpen()`（**开档实际重放字节**，journal_kv.go:112）、`indexEntries`=`IndexEntries()`（常驻索引）、`durableLoadCount`=`LoadStats().Scans`、`wallClock`、`coldReadWallClock`=`ColdReadCost()`——**全是成本面**，且有非空账户守卫（judgeSeatedAccount、accounted 逐代增长、journalBytes>0）。
- **判定**：S1钉的是**退位的磁盘尺寸效果**（热文件小），不是用户的成本症状。append-only 日志按本性增长，磁盘尺寸不是症状；成本才是。S1 在退位关时必红（字节线性增长），故它钉的是**退位机制**而非活要求。**建议（非编辑）**：S1 的量面应从磁盘文件尺寸改为成本面（开档重放字节/点读成本），或在 D6.13 复议时与退位机制一并退役；今日不动它，交编排者/用户裁。

### 十六·五 G5 判决

**寻址式索引方向：成立（原型实测支持）**。开档 O(1) root 读、点读 O(1) 两次小读、提交 O(1)，三者随历史 64× 平坦，且各有可构造增长控制案。枚举反对意见**对按需定位（变体 A）成立、对独立 append-only order 内联 span（变体 B）不成立**——order 结构化解之；全序枚举仍 O(历史) 顺序读，但无物化、无冷开档、newestOfType 有界前缀。

**退位是否变得不必要：对成本症状而言，是（实测支持）；但是否退役 D6.13 不由本门反转**。开档/点读被寻址式索引 bound 后，退位的成本相关收益（常驻索引 O(存活)、热开档 cut+尾）全被取代；它不买介质分离（同盘实测）、不买枚举界（走查跨全史实测）、不买可归因 tiering 的磁盘节省。唯一剩余收益是 S1 的热文件磁盘尺寸面——而该面是陈旧代理，非症状。

**已证（实测）**：①索引物化成本随历史增长，两 tier 皆然，与帧重放分离（§十六·一）；②热层有界是退位压小存活集的果，退位关即暴露整份物化（§十六·一）；③寻址式原型 open/point/commit/newestOfType 平坦、枚举变体 B 顺序 O(历史)、变体 A 慢 200–350×、物化开档控制案线性增长（§十六·二）；④冷热同盘、退位不 bound 枚举、冷点读首读随冷史增长（§十六·三）；⑤S1 量 os.Stat 文件字节、其「0..size 重放」消费者声称已陈旧（§十六·四，读码）。

**未证（须另门 settle）**：①原型非真实引擎——把寻址式索引集成进 journalLog/segment_store 并保全部不变量（提交序、R36 退位中途走查快照、崩溃恢复、撕裂尾、flock、originals 族）**未原型**；②原型全序与真实热冷合并序的等价**未独立验证**（单 order log 按构造逆序，推导非实测）；③原型提交成本（~15–35ms，2 fsync+2 rename）高于今日（G3 ~5ms），生产须批处理优化，**未优化**；④idx temp+rename＋order append＋root rename 的崩溃一致性**未崩溃测试**；⑤寻址式索引下是否仍需冷 tier（原型单 log 无冷层，「无冷 tier 则单 order log 足」是设计声称，**未在产规模验证**）；⑥并发 walk/commit 快照不变量在寻址式索引下**未证**。

**未量项（如实登记）**：失败退位两半成本；originals 族（ref/blob）在寻址式索引下的点读；真实 assembly boot 路径（本门用 ledger 层夹具，非正式 boot 入口）；原型枚举的逐键交付序与真实引擎的逐字对照。

**G2/G4 未由本门判定，产品不可发布。**

## 十七、G6：可寻址索引设计（采纳 R50/R51）＋退位彻底删除的零残留清单（2026-11-16）

**性质**：设计与调查，非生产实现。主树**零 `.go` 写、零 go 运行、零删除**；不勾任何任务框，不改 D6.13、S1、任何 spec 或他域计划。本节只追加，§一至§十六 一字未改（历史证据只附勘误不改写；§十五/§十六 中依退位成立的读数与成本行由本节·六标记为待重出，不改写原文）。

### 十七·〇 隔离树与原型源身份

- **构造法**：`rsync -a` 主树**工作树**（非 HEAD）至 `/tmp/recovery-g6/tree/`，排除 `.git`、`examples/`（独立 module）、`docs/.dev`、未跟踪残渣 `.tmp/`、`.tmux-*.test`。
- **源身份**：HEAD `9547faca2327c31579e91fbe40a27405d3ffdfb3` ＋工作树未提交改动（tracked 90 文件 +6883/−609，含 openspec 归档重命名；Go 面为 `memory/ledger`＋`assembly`＋`tests/integration/history`）＋未跟踪 `assembly/fact_commit_atomic_test.go`、`memory/ledger/fact_commit_batch_test.go`。
- **逐字节核对**：`journal.go` `47dc7f7a…`、`segment_store.go` `9a4ae9f7…`、`journal_kv.go` `ad9c028a…`、`config.go` `b1b91ea4…`、`kv_original_store.go` `fabb385d…`、`migrate.go` `28adc909…`、`assembly/checkpoint.go` `43ae8e0c…`、`assembly/run.go` `d7aa81f6…` 与主树 sha256 相同。隔离树 `go vet ./memory/ledger/` exit 0。
- **探针**：①隔离树 `memory/ledger/g6_boot_face_test.go`（`TestG6BootFaceWithAndWithoutRetirement`，PASS，153s）；②独立 module 原型 `/tmp/recovery-g6/proto/`（`addr.go`＋`main.go`），**复用 G5 变体 B**（`/tmp/recovery-g5/proto/main.go`）并**逐字复用** `journal.go` 的 `journalPut`/`journalRecord`/`journalValue`/`journalSpansAt` 及扫描器/`journalFrameOf`/`journalPayloadOf`/`journalReplay`（awk 按行区抽取，非转写），故原型文件的 span 与真引擎同字节口径。
- **计量纪律**：开档三相位各自独立计时（`engineOpen`／`storeLoad`／`tierOpen` 整体），**`storeLoad` 的引擎开档在计时区间外**——首轮曾把 `openJournalKV` 计入 `storeLoad` 造成区间包含，已修正并重跑，下表为第二轮；`min` 与 `median` 并列（syscall 面的最小值噪声最低，中位数并列使负载偏斜可见）；两个可失败控制案；原型崩溃案模拟**进程死亡**（页缓存存续），非掉电。
- **⚠️ 源树漂移（如实声明）**：本节行号与 sha 绑定**rsync 时刻的快照**。收口前复核发现主树 `memory/ledger/config.go` 已被**并发叶**改动（sha `b1b91ea4…`→`46a688cc…`），diff 为新增 `StoreCapabilities.AtomicBatch` 字段与两处 Capabilities 赋值（即 R46 登记的「`file` 档多键具名拒绝归 11.R3」），**与退位面不相交**；`journal.go`/`segment_store.go`/`journal_kv.go`/`kv_original_store.go`/`assembly/checkpoint.go`/`assembly/run.go` 六个文件复核仍逐字节相同。故本节 `config.go` 的行号（:48/:81/:306-310）在并发落地后**下移 4 行**，符号名不变。**派单须按符号名而非行号写白名单，且 S3 与 11.R3 同触 `config.go`，必须串行。**

### 十七·一 新实测：退位删除后 boot 有**两个**全量物化面（G5 只量了一个）

夹具：每深度 D 提交 D 个 fact（64B 正文＋256B 原文，事件族＋原文族），`WriteRights` 声明；退位开态另跑一趟只留最新 checkpoint。三相位独立计时。

| 深度 | 退位 | 热档字节 | engineOpen | engineBytes | storeLoad | indexEntries | tierOpen（整体，另测） |
|---|---|---|---|---|---|---|---|
| 400 | 关 | 474,702 | 5.80ms | 474,702 | 2.02ms | 400 | 5.64ms |
| 1,600 | 关 | 1,907,520 | 13.39ms | 1,907,520 | 4.64ms | 1,600 | 11.32ms |
| 6,400 | 关 | 7,662,876 | 29.15ms | 7,662,876 | 10.40ms | 6,400 | 31.50ms |
| 400 | 开 | 1,397 | 1.93ms | 110 | 22µs | 1 | 2.28ms |
| 1,600 | 开 | 1,415 | 2.61ms | 118 | 25µs | 1 | 2.26ms |
| 6,400 | 开 | 1,417 | 3.15ms | 118 | 15.9µs | 1 | 1.53ms |

**结论（实测，非推导）**：退位关时 **16× 深度 → engineOpen 5.0×、storeLoad 5.1×、indexEntries 16×**。`FileSegmentStore.Load`（`segment_store.go:358`）逐分区读 `{pid}:idx:` 与 `{pid}:meta:` 全前缀并物化 `s.index`/`s.order`/`s.windows`，是**独立于引擎索引的第二个全量物化面**。G5 §十六·一 量的是引擎开档与**冷层** Load 前缀读，未量热层 store Load 在退位关态的增长。

**故：可寻址索引若只做引擎层，boot 仍 O(历史)，核心目标不达成。** 设计必须同时覆盖 store 层（§十七·三）。这是本节相对 G5 的新增约束，不是对 G5 判决的推翻。

附带实测：退位关时 `fullWalk` 19.8µs→199µs（walked=400→6400），退位开时 402µs→8.25ms（付冷开档＋冷索引物化）。**删退位使全序枚举变快**（无冷开档），但两边都 O(历史)，与 R50「不得宣称枚举有界」一致。

**可寻址原型成本（G6 变体 B 扩展：多键提交＋删除＋命名空间目录＋根尾开档；每提交 5 键，checkpoint 每 40 提交一次）**

| 深度 | open min/med | point min/med | newestOfType min/med（walked） | commit | enum（delivered） | 控制①物化开档 | 控制②整份序重放 |
|---|---|---|---|---|---|---|---|
| 400 | 588µs / 660µs | 190µs / 198µs | 9.5µs / 11.7µs（1） | 20.0ms | 762µs（1610） | **430ms** | **948µs** |
| 1,600 | 553µs / 583µs | 181µs / 187µs | 9.2µs / 9.6µs（1） | 20.2ms | 3.06ms（6440） | **1.29s** | **3.04ms** |
| 6,400 | 在跑，未落 | — | — | — | — | — | — |

- **open／point／newestOfType／commit 四面对 4× 深度平坦**（open 588→553µs、point 190→181µs、newest 9.5→9.2µs、commit 20.0→20.2ms）。**两个控制案同轴增长**：物化开档 430ms→1.29s（3.0×）、整份序重放 948µs→3.04ms（3.2×）⇒ **平坦不是恒真界，可失败案已构造并实测**。
- **enum 762µs→3.06ms（4.0× 对 4× 深度）＝线性**，与 R50「全量枚举仍 O(历史)」一致，**不得宣称有界**。
- **计量纪律自纠两处（本轮实测发现，进 13 域假绿形态清单）**：①**区间包含**——首轮 `storeLoad` 把 `openJournalKV` 计在内，已把引擎开档移出计时区间并重跑（§十七·一 表为第二轮）；②**噪声冒充增长**——首轮 open 报 636µs→3.5ms 似有增长，改为「控制案先跑使写回沉降＋9 次重复＋min/med 并列」后为 588→553µs 平坦。**绝对毫秒含负载噪声，增长轴与控制案的对比才是判据**。
- **与 G5 读数的差异（如实）**：G5 open 3.03/3.02/1.72/3.25ms、point ~0.6–1.3ms；G6 open ~0.6ms、point ~0.19ms。差异来源是 G6 的 `idx` 按命名空间分目录（单目录条目数少）且开档在干净尾时**不写任何字节**（G5 原型每次开档后仍持有写路径）。**commit 20ms 高于 G5 的 15–35ms 区间下沿、远高于今日 G3 ~5ms**：三耐久点＋每目录 dir fsync，**未优化**，与 G5 §十六·五 未证③一致。

### 十七·二 引擎层设计：可寻址索引（控制逻辑级）

**盘上结构**（`<root>/kv/` 下）：

| 载体 | 形状 | 地位 |
|---|---|---|
| `journal.log` | **名字与帧格式不变**：8B 头（payload 长度＋CRC32C）＋`journalRecord{version,puts,deletes}`，一帧一提交 | **唯一真相** |
| `order.log` | append-only，**逐行校验和 ＋ 一行一提交语句**，内联每个 write 的值跨度 | 派生（可由 `journal.log` 整份重放重建）；行校验失败即从日志重述，与槽表同形 |
| `idx/<namespace>/<hex(key)>` | 一键一跨度文件；`namespace`＝`keyNamespaceOf(key)`（首个 `:` 前） | 派生；**目录内容恰为持有键集** |
| `root.json` | `{seq, valuesSize, orderSize}` 水位，temp＋rename | 派生（可由最新完整 order 语句重建） |

删除的载体：`journal.index`（索引切面）、`journal.rewrite`（重写临时档）。**冷目录 `cold/` 概念从框架消失**。

**order 语句形状**：`{seq, valuesEnd, puts:[{key,off,len,null}], deletes:[key]}`。**语句不内联自身的 order 字节长度**：一行若陈述自己的长度，陈述行为本身改变该长度（原型初版即因此水位漂移、枚举 JSON 解码失败，已修为写者在行外累计 `orderSize`）。`valuesEnd` 是本提交帧之后的 `journal.log` 字节。

**开档（只读根＋尾）**：
1. 读 `root.json`（一次小读）。
2. `stat journal.log`；`size == root.valuesSize` ⇒ **开档结束，不写任何字节**。这就是「只读索引根＋尾」的常态：尾为空。
3. 否则只读 `[root.valuesSize, size)` 这一段尾：先把 `order.log` 截到 `root.orderSize`（**派生尾一律丢弃重述**，不做对账），再 `journalReplay` 逐完整帧 → `journalSpansAt` 取键与值跨度 → 重述 order 行、重填 `idx`、推进 root；`journalReplay` 答出的 `complete < size` 即撕裂末帧，截断之。
4. `root.json` 缺失／解不出／水位越过日志 ⇒ 从链整份重建派生载体（一次性 O(历史)）。**这不是 legacy 分支**：新根、丢根、旧根走**同一条**「派生载体不可置 ⇒ 从链重建」规则，与今日切面丢失同形；框架不检测任何旧布局、不认任何冷目录。

**点读（键→跨度）**：读 `idx/<ns>/<hex(key)>` 一次小读 → `journal.log.ReadAt(off,len)` 一次小读 → `json.Unmarshal` 得原字节。文件不存在＝不持有。**两次小读，与历史深度无关**；值跨度口径沿用今日 `journalValue`，故小键不为其同帧大键付费。

**提交（O(1)）**：一帧 carrying 整批（puts＋deletes）→ `fsync journal.log` → 追加一行 order 语句 → `fsync order.log` → 填 `idx`（put＝temp＋rename，delete＝unlink；**每个 touched 目录一次 dir fsync，不是每键一次**）→ `root.json` temp＋rename＋dir fsync → 返回成功。三个耐久点，成本与历史无关。

**按提交序枚举（newest-first）**：自 `root.orderSize` 起**逆序 64KB 块 `ReadAt`** `order.log`，一行一提交；按键去重，**一个键的最新语句决定持有性**——put ⇒ 持有并交付其内联 span，delete ⇒ 不持有。故全序枚举**零跨度文件读、零值读、零排序**，仍 **O(历史)**（顺序读 order 全文，R50 明令不得宣称有界）。`NewestOfType` 走有界前缀即停（原型 walked=4，checkpoint 每 40 提交一次）。

**`Keys(prefix)`／`Namespaces()`**：`idx` 目录 `ReadDir`（namespace 一层＋键一层），hex 解码即键名，**零跨度读零值读**。删除即 unlink，故目录恰为持有集，墓碑不需要读文件即可排除。`Namespaces` 成本＝命名空间数（与今日成本行同口径）。

### 十七·三 store 层级联设计：`Load` 去物化（本节新增约束的承接）

引擎可寻址后，`FileSegmentStore` 的常驻态必须同步去物化，否则 §十七·一 的第二个面仍在：

- **`s.index`（map[EventID]indexPosition）删除**。`{pid}:idx:{key}` 本就是持久键，其值 `window:seq:{type,session,commit}` 经既有 `indexPositionOf` 解码即 `indexPosition`。`GetEvent`/`eventByKey`/`planEventLocked` 判重/`orderEntry` 改为**按身份点读该键**（引擎两次小读）。读缓存 `segmentCache` 保留（它已有字节预算，是加速器不是真相）。
- **`s.order`（[]EventID）删除**。枚举直接走引擎 order 结构（`eachCommittedNewestFirst` 过滤 `idx:` 族），`commitOrderOf` 的「引擎陈述数 ≠ 已发布数」具名拒绝随之失去输入面（见·六 cascade）。
- **`s.windows`／`s.count`／`s.commitSeq`**：这三者是**标量与小表**，不是全史索引。`count`/`commitSeq` 由引擎 root 的 `seq` 与 store 自己的席位记录承接；`windows` 是 `(partition,window)` 小表，随窗口数而非事实数增长。**其载体形状属实施期设计分叉，本节不冻结**：候选是「store 席位键（一提交一更新的小记录）」与「由 order 语句的 idx 族内联字段派生」，前者须守「派生载体不得强制全量读取」，后者须守「引擎不解释领域等价」（R46）。**登记为待裁设计点，不擅自选定。**
- **`Load` 的语义保留**：索引与窗口元数据未双到的条目仍不发布（`ErrSegmentBodyMissing` 具名拒绝路径不变），只是判定时机从 boot 全量前缀读改为**按身份点读时**。`EventCount` 的「全部成功提交事件数」口径不变。

**成本声明（须进具名操作成本表）**：boot＝根＋尾（O(1)）；点读＝两次小读（O(1)）；提交＝O(1)（三耐久点，常数高于今日一 fsync，**未优化**）；`NewestOfType`＝有界前缀×每条目一次值跨度读（引擎不内联 type，故比 G5 原型多一次小读，**这是与原型读数的已知差异**）；全序枚举＝O(历史) 顺序读；`Keys(prefix)`＝O(该前缀键数) 目录读；`Namespaces`＝O(命名空间数)。

**第二个承重级联：`TailKeys` → `tailFactKeys` → `DurableCommit.Seat`（grep 实证，不可漏）**

- 生产链路：`journalKV.TailKeys()`（journal_kv.go:123）→ `assembly/checkpoint.go:948 tailFactKeys` → `:925 durable.Seat(ctx, stated, tailFactKeys(tier))` → `runtime/state/durable.go:400 Seat`。
- **`Seat` 的判据是 `tail == nil`**（:409）：nil ⇒ 走 `d.load` 读全账；非 nil ⇒ 按 checkpoint 切片入席，并**只读 `tailTaskIDs(tail)` 点名的年轻任务**（:421/:435）。
- **今日「尾」＝退位切面之后的键**，其边界恰由退位趟放在 checkpoint 处，故「切面尾」与「checkpoint 之后的事实」重合。**退位删除后二者分离**：新引擎的尾是「root 水位之后的字节」，而 root 每提交都写 ⇒ **常态尾为空集（非 nil）**。若直接把空集交给 `Seat`，它会走 checkpoint 路径且**读零个年轻任务** ⇒ **checkpoint 之后提交的任务记录静默不入席**。这是删除退位会引入的真缺陷，不是夹具问题。
- **设计承接（repurpose，不删该面）**：「尾」改由**序结构相对最新框架 checkpoint 的有界前缀**定义——自 order 尾逆序走，收集键直到抵达最新框架 checkpoint 自己的语句为止。checkpoint 键 `checkpoint:<partition>:<partitionSeq>` 本身是 order 里的一条，故**不需要 seq 映射、不需要读值**，且走查长度＝checkpoint 之后的提交数（有界前缀，与 `keptHotKeys` 今日停在框架 checkpoint 同一判据）。引擎不声明序或找不到 checkpoint ⇒ 答 nil，保留今日「读全账」兜底。
- **必须补的负锚**：`TestSeatReadsTheTasksCommittedPastTheCheckpoint`——checkpoint 之后提交若干任务记录，重启入席后这些任务的账必须可读；**可构造失败案**：把「尾」实现为 root 水位之后的键集（常态空），则该锚红（年轻任务零读）。此锚归 05/09 域，本域只供面与判据。

### 十七·四 崩溃一致性（原型实测，非推导）

**写入次序与耐久点**：①append 帧 ②fsync `journal.log` ③append order 行 ④fsync `order.log` ⑤填 `idx` ⑥写 `root.json`。**成功只在⑥之后返回**。①—⑤之间死亡＝`FactUncertain` 窗口，与今日「append 成功但 sync 未返」同语义，不新增第六种结果。

**修复规则一句话**：`journal.log` 是唯一真相，`order.log`/`idx`/`root.json` 全是派生；开档把派生尾**丢弃并从链尾重述**，故载体超前于根（③④⑤后死）与落后于根（⑤前死）收敛到同一状态。

| 案 | 构造 | 实测结果 |
|---|---|---|
| 崩溃步①—⑥ | 120 提交 populated 根，第 121 提交在指定步注入停止（5 键一批），关档后重开 | **6/6 收敛到同一 root（seq=121, valuesSize=89379, orderSize=33046）**；停止的提交 **5/5 键全落或全不落**（实测全落，因页缓存存续且帧完整）；身份读回原 256B 原文与 ref 正文；枚举 488 键、每键恰一次、序非增；`newestOfType` walked=2 |
| 撕裂末帧 | 根水位之后手工追加**少 7 字节**的帧 | 截在水位，root seq=120 **不变**，撕裂键 absent，枚举 483 键正常 |
| 撕裂末帧＋**孤儿 order 语句** | 同上，另在 `order.log` 追加一条**完整**语句命名该撕裂帧 | 同样截在水位、root 不变、键 absent ⇒ **order 载体不被当作第二真相**（负锚成立） |
| 已耐久根覆盖的字节被切 | 切掉 root 已覆盖的 300 字节 | **具名拒绝**：`memory: journal frame is corrupt: values.log holds 88341 bytes beside a root stating 88641`（不静默 serving 前缀） |
| 删除 | 提交 `deletes:[1:idx:…, blob:…]` | 点读 absent、`Keys` 不再列举、**order 仍陈述该删除**、重开后一致（seq=81） |

**掉电边界（如实声明，未测）**：`root.json` 的耐久依赖 dir fsync（原型已做）。若实施期省掉它，掉电后根可能落后多个提交，尾不再是「≤一提交」——**「开档只读根＋尾」的界在掉电态依赖该 dir fsync**。进程死亡态已实测；掉电态未测。

### 十七·五 必须保全的不变量（逐条对设计）

1. **身份读返回原字节**：值跨度指向 `journal.log` 内该键自己的 JSON token，`ReadAt`＋`Unmarshal` 与今日 `journalLog.read` 逐字同路径；派生载体只存位置不存值，故无第二份正文可漂移。原型逐案核 256B 原文与 ref 正文。
2. **全局提交序**：一帧一提交、一行 order 语句一提交，`seq` 单调；order 行序＝帧序。原型核枚举序非增（同一提交内多键共享其 rank，**这是正确语义不是缺陷**：初版断言用严格递减而误红，已改为非增）。
3. **一身份一不可变事实**：`idx` 一键一文件，put 覆盖 rename、delete unlink；order 按键去重故每身份恰交付一次。
4. **领域等价重交＝重复不是冲突**：判重仍在 store 层 `planEventLocked`/`planFactLocked`（R46 次序：领域门先于引擎字节门），引擎不解释领域等价；冷层判重分支删除后，判重读改为按身份点读同一持久键，**语义不变、路径变短**。
5. **flock 写权模型与释放不变量**：一根一写者不变；`order.log`/`idx`/`root.json` 全在**同一根之下**，故 R38 修正的「一根一锁已足、无第二把冷根锁」继续成立，且冷根消失后该结论更强。R45 释放不变量逐字不动：**恰在「本进程可能仍提交的东西无一件在飞」时交出根**（`len(flying)==0 && writers.Stopped()`）。提交多两个耐久点不改变「在飞」的定义，故释放判据无需改。
6. **`CommitFact` 五结果**：`FactCommitted`/`FactAlreadyPresent`/`FactConflict`/`FactUncertain`/`FactRefused` 全部保留。`factCommitRefused` 的判据（`ErrJournalFrameTooLarge`、`ErrCommitAfterClose`）不变——帧界拒绝仍在写前决定；①—⑤窗口归 `FactUncertain`，⑥后归 `FactCommitted`。**五值不增不减**。

### 十七·六 零残留删除清单（穷举，grep 实证）

**处置只有两种：delete（删）／repurpose（改指新机制）。无一项标「为兼容保留」。**

**A. 生产符号（`memory/ledger`＋`assembly`）**

| 符号 | 文件 | 处置 | 理由 |
|---|---|---|---|
| `RelocateReport`（含 `Settled`/`Skipped`/`Batches`/`ColdOpenCost`） | journal.go:174-189 | delete | 唯一消费方是退位趟与其计量 |
| `journalLog.relocate` | journal.go:564 | delete | 退位三步本体 |
| `journalLog.copyToCold` | journal.go:683 | delete | 冷复制 |
| `journalLog.verifyColdCopy` | journal.go:728 | delete | 冷校验（第二次冷开档） |
| `journalLog.placeIndexCut` | journal.go:640 | delete | 切面落盘点；新机制无切面 |
| `journalLog.cutOf` | journal.go:649 | delete | 只被 `placeIndexCut` 消费 ⇒ **级联单使用方** |
| `journalLog.rewrite`／`rewriteInto`／`reopen` | journal.go:777/800/896 | delete | 三者只服务退位重写；`reopen` 只被 `rewrite` 消费 ⇒ 级联 |
| `journalRewriteFileName` | journal.go:74 | delete | 伴生常量 |
| `journalIndexCutFileName`／`journalIndexCutVersion`／`journalIndexCut`／`journalCutValue`／`cutValue`／`journalCutValue.value` | journal.go:83-137 | delete | 切面载体整族；`root.json` 取代 |
| `readJournalIndexCut`／`writeJournalIndexCut`／`appendJournalIndexCut`／`journalCutOrder` | journal.go:1115/1191/1215/1165 | delete | 切面读写与归并；R47 的 JSON-Lines 增量**一并删除** |
| `journalLog.seedIndexCut`／`tail`／`cutOffset`／`cutNotice`／`maintainsCut`／`noteTail`／`tailKeys`／`statesCutMaintenance`／`cutStatement` | journal.go:1029/234-250/1239/1041/1063/1072 | delete | 切面伴生族。**`openBytes`/`openReadBytes` repurpose**：改指「根＋尾」读入字节（S2 的 `journalBytes` 面继续有机制可钉） |
| `journalLog.index`／`order`／`namespaceSeen`／`namespaceOrder`／`indexRecord`／`noteOrder`／`noteNamespace`／`eachKeyNewestFirst`／`keys`／`namespaces` | journal.go | repurpose | 常驻物化改为盘上可寻址载体；`eachKeyNewestFirst` 改走 order 逆序块读，`keys`/`namespaces` 改走 `idx` 目录读 |
| `coldRetirement` 接口／`journalKV.retireSettled` | journal_kv.go:242/255 | delete | 引擎退位能力 |
| `journalKV.IndexCutNotice`／`statesCutMaintenance` | journal_kv.go:136/148 | delete | 切面声明面；`assembly/run.go:635` 的 WARN 消费方、`segment_store.go:1054` 的调用与 `:1064` 的接口声明随之删 ⇒ 级联 |
| `journalKV.TailKeys`／`journalLog.tailKeys`／`tail` | journal_kv.go:123／journal.go:1041/234 | **repurpose（不删）** | **有生产消费方**：`assembly/checkpoint.go:948 tailFactKeys` → `:925 durable.Seat`。改指「序结构相对最新框架 checkpoint 的有界前缀」，见·三第二个承重级联 |
| `assembly.tailFactKeys` | assembly/checkpoint.go:948 | repurpose | 同上；其 nil／空集语义是 `Seat` 的判据，不可改为常空 |
| `FileSegmentStore.coldCommit`／`coldRoot`／`cold`／`retire`／`retired`／`coldReadCost`／`coldWriteCost` | segment_store.go:297-332 | delete | 冷层常驻态整族 |
| `coldCommitOf`／`retiredCountOf` | segment_store.go:458/480 | delete | 只被 `Load` 的退位分支消费 |
| `segmentSettledKey`／`segmentSettledCommitKey` | segment_store.go:210/216 | delete | `settled:events`／`settled:commit` 两个持久键；**`kvOriginalStore.relocatedLocked` 是第二消费方，同批删** |
| `WithColdRoot`／`WithRetirement`／`RetireFacts`／`keepKeysLocked` | segment_store.go:1037/1051/1083/1198 | delete | 退位公开入口与 keep 集判据 |
| `ColdReadCost`／`ColdWriteCost` | segment_store.go:1149/1165 | delete | 冷计量面 |
| `ErrColdArchiveUnavailable` | segment_store.go:1032 | delete | 退位具名错误 |
| `coldViewLocked`／`coldEventLocked`／`coldEvent`／`coldEventKeys`／`coldIndex`／`coldIndexLocked`／`coldIndexEntry` | segment_store.go:958/983/1330/1340/1376/1384/1367 | delete | 冷读与冷枚举整族；`coldIndexEntry` 被 R36 冻结半快照复用 ⇒ 该快照机制同批删 |
| `eachIndexEntryNewestFirst` 的**热冷归并＋冻结半**（`frozen`/`stated`/`settled`/`readSettled`/`coldBound`/`takeSettled`） | segment_store.go:1546-1625 | delete（归并部分）／repurpose（走查本体） | 单日志后无两序可归并；R36 的「冻结热半＋身份集」机制**失去被测对象**，其锚同批删 |
| `published` | segment_store.go:1451 | **delete** | grep 实证两个消费方均为退位路径：`:1312`（`EachEvent` 的冷键循环）与 `:1610`（热冷归并）⇒ 双级联 |
| `snapshotPositions`／`orderEntry` | segment_store.go:1426/1634 | repurpose | `snapshotPositions` 的生产消费方 `EachEvent`(:1288) 存活，故随 `s.index` 去物化改指盘点读；`orderEntry` 改走序结构 |
| `EachEvent` 的冷键循环（`relocated`/`coldEventKeys`/`coldEvent`） | segment_store.go:1304-1333 | delete | 单日志后无冷键可补走 |
| `IndexEntries()` | segment_store.go:1270 | **delete** | grep 实证：删 `cold_archive_test.go` 后**唯一消费方是 S2**（`symptom_anchor_recovery_test.go:99`）⇒ 测量-only 导出，属 N2 自证结构；S2 改锚后无消费方 |
| `kvOriginalStore.coldRoot`／`cold`／`WithColdRoot`／`coldViewLocked`／`relocatedLocked`／`coldEntryLocked` | kv_original_store.go:54-61/177/187/210/230 | delete | 原文族冷读整族 |
| `planFactLocked` 的 `settled` 变量 | kv_original_store.go:341-389 | delete（级联简化） | `settled` 恒 false 后 `blobStaged = !held \|\| entry.blob == nil`、`staged = !held`；**不是保留一个恒假标记** |
| `FileSegmentStore.Close` 的冷视图关闭分支 | segment_store.go:1665-1676 | delete | R41 修的 `s.cold` fd 泄漏随冷视图消失 |
| `coldDirName`／`StorageConfig.RetireFacts`／`OpenTier` 的 cold 接线 | config.go:48/81/306-310 | delete | 配置字段与唯一生产 `WithColdRoot` 调用方 |
| `Run.relocateSettledFacts`／`Run.keptHotKeys` | assembly/checkpoint.go:394/432 | delete | 装配侧退位趟；`keptHotKeys` 的全史走查消费者随之消失（R49 附带收益） |
| `checkpointLocked` 的退位调用与 `assemblyError` 文案 | assembly/checkpoint.go:377-379 | delete | 唯一调用方 |
| `relocatableStorage` | assembly/run.go:506 | delete | 强制 `RetireFacts=true` 的唯一生产开关；`assemble` 调用点同批删 |
| `run.go:635-637` 的 `IndexCutNotice` WARN | assembly/run.go | delete | 伴生消费方 |
| `errStopIndexWalk` | assembly/checkpoint.go | **保留** | grep 须复核：若仅 `keptHotKeys` 消费则级联删；其他走查（:970/:1006/:1126）仍用 ⇒ 保留 |

**B. 明确不删（不同义项／不同机制，误删即越界）**

- `assembly/run.go` 的**代际退役**整族：`generationRetireDimension`/`generationRetirePrefix`/`generationRetireKey`/`generationRetirementRecord`/`recordGenerationRetirement`/`convergeGenerationRetirements`/`ObserveRetirements`；`runtime/state/retire.go`；`assembly/retired_state_hygiene_test.go`（主题是配置事实与代际，非存储退位）。这是 GLOSSARY:14 的本义「岗位交出去档案留在册」。
- `memory/ledger/retention.go` 的 `Retention.Retire`（租约释放）、`delivery.settlement` 的 `headless-retired`、`statusProgressRetired`、`SummaryPlugin` 退位：均为本义或另一机制。
- `migrate.go`／`MigrateShardedRoot`／`rejectLegacyShardLayout`／`ErrJournalLegacyLayout`／`ErrMigrateMismatch`：**分片快照旧布局**机制，与冷层无关。`ErrMigrateMismatch` 在 `journal.go` 的三处冷校验引用随冷校验删，但**错误本体保留**（`migrate.go` 仍是其使用方）。
- `segment_store_test.go` 的 `TestSegmentGetEventColdMiss`/`TestColdEnumerationReadsEachEventOnce`/`TestColdReadAfterRestart`/`TestSegmentColdReadFailureSurfaces`：此处 cold＝**读缓存驱逐**义（`dropSegmentCache`），非冷层。保留；词汇上须在·七登记两义并存。
- `journal_kv.go:19`「a cold value is evicted from memory」：同上，缓存义，保留。

**C. 测试（删除，不适配）**

| 文件 | 处置 |
|---|---|
| `memory/ledger/cold_archive_test.go`（565 行，9 测） | **整文件删** |
| `memory/ledger/retirement_snapshot_test.go`（286 行，3 测） | **整文件删** |
| `memory/ledger/journal_index_cut_test.go`（649 行，8 测） | **整文件删**（切面与重写机制整体被取代；含 R47 的 `TestColdCutStatesOnlyTheKeysItsPassMoved`、06.R2 的 `TestOpenCostIsBoundedByTheCutItKeeps`/`TestIndexMaintenanceRunsOutsideTheRelocation`） |
| `tests/integration/history/recover_kv_relocated_root_test.go` | **整文件删** |
| `tests/integration/history/symptom_anchor_journal_test.go`（S1） | **删或改锚**，见·八 |
| `memory/ledger/cold_archive_gate_test.go` | 删 `TestColdArchiveHoldsTheExclusiveGate`；**保留** `TestSecondProcessOnOneRootIsRefusedWriteRights`/`TestWriteRightsAdmissionReportsTheRootByItsPath`（丙′ flock 锚，R37/R42）。文件须**改名**以匹配存活责任（冷名文件装 flock 锚＝词汇残留＋`responsibility-fragmentation` 风险） |
| `memory/ledger/segment_store_test.go` | 删 4 测：`TestColdReadCostCountsEachReadOnce`(:628)、`TestOpenWithoutAnIndexCutPaysTheWholeLog`(:676)、`TestEachIndexNewestFirstStatesOneIdentityPerCommitAcrossARetirement`(:789，R36 冻结锚)、`TestDerivedIndexNeverSkipsAKeyItPublished`(:911，用 `placeIndexCut`/`readJournalIndexCut`)。**级联删测试 helper `judgeOpenIsBounded`(:657)**：grep 实证其唯一两个调用方(:719/:774)都在被删测内。保留 `TestLoadRefusesAnOrderThatMissesPublishedEntries` 等（须复核其输入面是否随 `commitOrderOf` 改动） |
| `memory/ledger/journal_kv_test.go` | 删 `TestRetirementDropsTheSettledKeysFromTheResidentSet`(:123) |
| `assembly/recovery_judgment_test.go` | :130-147 的**退位交错腿**（`keep`/`land`/`settled` 守卫）随机制删；`control` 腿主题不变。**这是覆盖面缩小，须显式申报，不得静默吸收** |
| `assembly/symptom_anchor_recovery_test.go`（S2） | `coldReadWallClock`/`coldRead`/`coldWrite` 面与 `WithRetirement(retire)`(:597)、`ColdReadCost`/`ColdWriteCost` 探针删；`indexEntries` 面随 `IndexEntries()` 删而**失去被测机制**；`journalBytes` 面 repurpose 到「根＋尾」。**S2 整体须由 09 域重出判据**，本域不改 |
| `assembly/checkpoint_test.go`／`compress_checkpoint_test.go`／`host_delivery*_test.go` 等 | grep 未命中退位符号；若其夹具经 `Assemble` 间接受 `relocatableStorage` 影响，属行为面复跑而非删测 |

**D. spec 条款**

| 文件 | 处置 |
|---|---|
| `openspec/specs/session-checkpoint-material/spec.md`（Requirement「派生态有界且不删除已提交事实」） | **重写该 Requirement 的热层半**：删「热层 SHALL 同样有界／搬迁出热层／退位判据 SHALL 是水位制／透明回冷层」；保留「checkpoint 大小有界于开放项」「SHALL NOT 删除覆盖改写已提交事实」「按身份读返回原字节」。Scenario「水位前已结事实搬迁而尾段留热」**删**；「已搬迁事实按身份仍可读」**改写为按身份读恒返回原字节**（去冷层 AND 腿）；「有界化不打断全史读者」**保留**（它正是禁止删事实的红线） |
| `openspec/specs/compaction-scheduler/spec.md`（Purpose 下的 superseded 备注与「历史条款已退役」） | **附勘误**：该 superseded 历史记录称「物理有界走退位」，机制删后该现行声称失实 ⇒ 改指「磁盘增长是显式声明、单独拥有的关切；成本有界走可寻址索引」。历史记录本体不改写 |
| `changes/08-fact-retirement/specs/fact-retirement/spec.md`（208 行，7 Requirement／31 Scenario） | **整 capability REMOVED** |
| `changes/08-fact-retirement/specs/storage-lifecycle-completion/spec.md`（Requirement「保留保护与回收协调」、「TTL check uses actual event timestamp」） | 删「租约不构成退位判据」「回收判据 SHALL 是水位制 keep 集」两处的退位耦合；保留租约/TTL 本体 |
| `changes/06-bounded-open-index/specs/bounded-recovery/spec.md`（12 处） | 切面→根＋尾；删「判据在搬迁两态下分别成立」「退位开启不冒充索引兑现」「冷层索引点名而正文读不出」「无切面的退化被具名」等退位/切面 Scenario；保留「开档读入字节 SHALL NOT 随历史线性」「静默跳页改响亮失败」「全扫兜底」 |
| `changes/09-acceptance-real-measure/specs/bounded-recovery/spec.md`（8 处） | 删「退位漏记写侧」「冷读成本与 boot 分列」的退位区间；`journalBytes` 面改指根＋尾；阶段计量去「搬迁」一项 |
| `changes/11-atomic-fact-commit/specs/fact-vocabulary/spec.md`（Requirement「事实类别契约」） | 「已结事实搬迁后热层未命中是常态」前提消失 ⇒ 改写为「稳定身份重交按身份点读判重」；Scenario 保留主题去搬迁措辞 |
| `changes/12-consumer-state-lifecycle/specs/session-checkpoint-material/spec.md`（Requirement「派生态有界且不删除已提交事实」） | 与主 spec 同批改写（镜像条款） |
| `openspec/specs/memory-curation/spec.md`（Requirement「记忆三原语与固化级联」）、`task-skeleton-compression/spec.md` | **不动**（前者是 SummaryPlugin 本义，后者命中是「回退」误匹配） |

**E. 文档／godoc／词汇**

- `docs/wiki/memory/memory-architecture.md`：**零冷层/退位引用**（grep 实证），故无删除项；但 :160-161（`journalLog.order` 常驻、逆序走）、:181（第二次打开重放日志索引）、:253（「打开时重放整份日志建索引」）须**改写为可寻址索引的当期契约**——:253 在今日切面下已陈旧，属既有脱节。§十二「已知缺口」的「追加日志只追加，没有压实」行**保留并升格**为显式声明的磁盘关切。
- `docs/GLOSSARY.md:14,:40`：**保留**（本义）。存储义「退位」在 wiki 无条目，故词汇清除面是**代码注释与 openspec 草稿**，不是 GLOSSARY。
- 代码注释：`journal.go`/`segment_store.go`/`journal_kv.go`/`config.go`/`kv_original_store.go`/`assembly/checkpoint.go`/`assembly/run.go` 中全部退位/冷层叙述随符号删；`config.go:177`「冷日志在同一根之下…一根一锁」须改写为真话（冷日志不存在，一根一锁仍成立）。
- openspec 草稿：`runtime-recovery-ideal/design.md`（22 处，含 D6.1/D6.2/D6.7/D6.11/D6.13、§退位/恢复、回退、共享面、集成次序）、`proposal.md`(5)、`tasks.md`(1)、`run-notes.md`(6)、`specs/orchestration/spec.md`(2)、08 域 design(29)/proposal(19)/tasks(16)、06 域 design(72)/tasks(20)/proposal(10)、09 域 design(7)/proposal(2)/tasks(1)、11 域 design(11)/tasks(11)/proposal(5)、10 域 tasks(3)/proposal(2)/design(2)、05 域 design(3)/proposal(2)/tasks(1)、07 域 proposal(2)/design(1)、12 域 design(2)/tasks(1)、02/04 域 tasks(各1)。**ORCHESTRATION.md(94) 是历史裁决记录，只附勘误不改写**（R50/R51 已自带反转声明）。
- **`settleGate` 与 `ErrColdArchiveNotQuiet` 在 Go 树零定义**（grep 实证）：只存在于 ORCHESTRATION:152/158 与 08 域 tasks/proposal 的历史叙述。故二者是**文档残留而非代码残留**，处置＝随 08 域文档改写清除，不建也不删代码。

**F. 具名操作成本表（§十五·一） cold/退位行**

删 4 行：「冷层首读」「`NewestOfType`（退位开，复制热序）」「退位 cut 写（热）」「退位 cut 写（冷）」「整趟退位」（共 5 行，其中 `NewestOfType` 行**改写**为有界前缀而非删）。改 3 行：「无切面开档」→「开档＝根＋尾」、「有切面开档」→ 合并入前者、「`Load` 前缀读」→「按身份点读 O(1)」＋「store 席位/窗口表」（形状待·三设计点裁定）。保留：「事实提交」（常数，但须注明三耐久点）、「热层点读」、「`EachIndexNewestFirst` 全序走查」→ **O(历史) 顺序读，无冷开档**、「未折叠长链全扫」、「`Namespaces`」。**新增一行：磁盘增长＝O(历史)，显式声明、单独拥有，本变更不承诺有界。**

### 十七·七 落地次序（每步保绿；不可保绿者显式标注）

**必须原子的簇**（删机制与删其锚同批，否则编译红或锚钉空机制）：

1. **S0 设计冻结（本节的·三 store 席位/窗口表载体形状）**——**这是真停点**：`count`/`commitSeq`/`windows` 的载体未裁，实施期不得凭直觉选。裁定前 S2 无法重出判据。
2. **S1 引擎可寻址索引落地（新增，不删旧）**：`order.log`/`idx`/`root.json` 与新的 open/point/commit/walk 路径**与今日切面路径并存**，新锚（开档只读根＋尾、点读、提交序、崩溃六步、撕裂帧、孤儿语句、删除）同批建立。**此步可保绿**：旧路径未动，旧锚仍绿。新增载体在旧根上首次开档触发整份重建（一次性 O(历史)），须在该步声明并计量。
3. **S2 store 层去物化**（`s.index`/`s.order` 改盘点读、`Load` 改判定时机）＋其锚。**此步可保绿**当且仅当 S1 已落地且·三载体已裁；否则 `EventCount`/`commitSeq` 无来源 ⇒ 红。
4. **S3 退位整套删除（原子，不可拆）**：A 表全部生产符号 ＋ C 表全部测试删 ＋ `cold_archive_gate_test.go` 改名 ＋ `assembly/recovery_judgment_test.go` 交错腿删 ＋ `relocatableStorage`/`IndexCutNotice` WARN 删。**此步不能与 S1/S2 分开保绿**：删 `RetireFacts` 而留其锚＝编译红；留 `RetireFacts` 而删其锚＝锚钉空机制（残留）。故 S3 是一次原子落地。
   - **勘误（08.R3 执行，2026-11-16）**：C 表把 `journal_index_cut_test.go` 记为整文件删，实际只删了其中依赖退位的 5 枚锚与 `cutFixture`。原因：`placeIndexCut` 已在生产面删除，故该文件只剩 `TestOpenReadsTheLogWholeBesideNoIndexCut` 一枚可编译的锚；切面写入侧（`writeJournalIndexCut`／`appendJournalIndexCut`）删后零调用，其删除归 S4，本单留作 S4 的现存消费面。`symptom_anchor_journal_test.go`（S1）按·八建议整文件删。
5. **S4 切面机制删除（原子）**：`journal.index` 整族 ＋ `journal_index_cut_test.go` 整文件 ＋ `rewrite`/`rewriteInto`/`reopen` ＋ `TestDerivedIndexNeverSkipsAKeyItPublished`。**须在 S3 之后**：`placeIndexCut` 的唯一剩余调用方是 `relocate`，S3 未删则 S4 编译红。
6. **S5 S1/S2 症状锚重出**（09 域拥有，本域只供面）：S1 按·八处置；S2 去 `coldRead`/`coldWrite`/`indexEntries` 面、`journalBytes` 改指根＋尾、补新非空守卫。**此步不能保绿于 S3 之前**（`ColdReadCost` 等符号尚在时改锚＝钉将删机制）。
7. **S6 spec/文档/词汇/成本表同步**（D/E/F 表）＋`scripts/gates/check-openspec.sh`＋注释门（零容忍＋棘轮）＋`go build ./...`。**可保绿**，但 08 域整 capability REMOVED 须走 `openspec archive` 而非 `--skip-specs`。
8. **S7 全树复跑**：`./tests/...` 全量 ＋ `./assembly/` ＋ `./memory/ledger/` ＋ `./scripts/recover-kv/` ＋ `./runtime/... ./features/... ./tool/...` ＋ 走查/跨进程锚 `-race -count=10` ＋ 两注释门。**R43 教训：四套绿≠树绿**，凡改存储开档语义须覆盖所有开根包。

**关键顺序约束**：S3 与 S4 不可交换（S4 依赖 S3 先删 `relocate`）；S3 内部「删符号」与「删锚」不可交换（同一原子步）；S5 依赖 S3；S2 依赖 S0 裁定。

### 十七·八 S1 处置：**建议删除**（不改锚）

**理由（三条，逐条可核）**：
1. **其量面是磁盘字节**（`os.Stat(journalPath).Size()`，:89/:98），而 R50 已把磁盘增长裁为**显式声明、单独拥有、本变更不承诺有界**的关切。改锚到成本面后它量的东西与 S2 完全重合。
2. **改锚到成本面必然与 S2 冗余**：S2 已有 `journalBytes`（引擎开档读入字节）＋`wallClock`＋非空账户守卫，且 S2 是**多代真实 assemble** 夹具；S1 是 ledger 层夹具。把 S1 改锚到「开档读入字节有界」＝在更弱的夹具上重钉 S2 已钉的面，属第二真相。
3. **其 doc 理由已过期**（:28「openJournalLog replays 0..size」），且其夹具的 `RetireFacts:true`(:48)／`RetireFacts`(:95)／`keep` 集都是被删机制的入口。保留一个钉着已删机制的锚本身就是残留（R51 末条）。

**若编排者仍要保留一枚 ledger 层成本锚**（非本建议），可失败案必须是**新机制**的：`TestOpenReadsOnlyRootAndTail`——多代重开档，量 `JournalBytesOpen()`（repurpose 后＝根＋尾读入字节），判据 `retained[g] ≤ 2×retained[1]` 且历史 4× 增长；**可构造失败案**：把开档改为「root 缺失即整份重放」并**删掉 root.json**，则读入字节随历史线性 ⇒ 红。与 S2 的区分必须写明：S1 钉**引擎层字节面**（ledger 夹具，无 assembly），S2 钉**真实 boot 的复合面**（assemble 墙钟＋任务前缀扫描）。**但本建议是删除**，因为该区分在 S2 已有 `journalBytes` 面时不成立为独立必要。

### 十七·九 外部脚本建议（框架零分支）

**建议新增 `scripts/fold-cold-root`（一次性工具，形态复用 `scripts/recover-kv`）**〔已交付，见 R99〕：默认干跑、`-restore` 才写、`WriteRights: restore`（干跑可 beside 活运行时读，restore 被活运行时具名拒绝）、写后经**新开引擎**逐身份回读校验、不可重建者具名报告并非零退出。

**它做什么**：把既有 `<root>/cold/kv/journal.log` 的事实折回一个**新根**，按**原全局提交序**重述帧。序的来源是数据自身：`{pid}:idx:` 值携 `segmentIndexCarried.Commit`，热档 `settled:commit` 陈述已搬最大序号，故跨两档的全序可由这些字段重建，**不需要框架提供任何冷读能力**。校验形态复用 `MigrateShardedRoot`：目标根非空即拒、逐键字节比对、`ref:` 无配对 `blob:` 具名拒绝。**源根只读，绝不改写**。

**为何在框架外**：它是一次性数据搬运，消费者是操作者不是运行时；做成框架开档分支即 R51 明禁的兼容残留。框架侧**不检测冷目录、不认冷布局、不留具名错误、不留开关**。

**诚实的最小答案**：若操作者的根是开发scratch根、其冷档事实无保留价值，**新根就是诚实答案**，脚本不必跑。但凡该根承载真实已提交事实，**脚本是发布前的操作前置**——退位在删除前对任何持久 journal 根强制开启（`relocatableStorage`，该符号已随退位删除），dev 分支跑过的根都可能已填充冷档。**本设计不依赖「是否存在这种根」的答案**（该答案被明禁以检查用户数据取得）：框架零分支，脚本按操作者需要跑。

**旧根残留文件**：`journal.index`（索引切面，读路径已改由槽表表头回答水位，切面族随 08.R4a 删除）与 `cold/`（退位的冷层根）在新引擎下不被读取。它们是**盘上数据残留**而非代码残留，清理由该脚本承担（或操作者手工），框架不为它们留任何分支。

### 十七·十 计划回写：哪些域/叶断裂（供编排者单写）

| 域/叶 | 断裂内容 |
|---|---|
| **08 全域** | 6.1/6.2/6.3/6.5 的主题（退位单能力、排他安静点、透明回冷、S1 转绿）**整体消失**；7.1（重写后句柄悬空）随 `rewrite` 删而失去对象；7.2（驻留集不随日志收缩）随 `retireSettled` 删；7.3/7.4/7.5/7.6 本就是「证伪不改」记录，只附勘误；6.4（lifecycle/retention/guard 接线）的「oracle 改链可达」须去退位耦合；08.R1/08.R2 前置与交付全部重写。域标题由「事实退位」改为「退位彻底删除与冷档外部处置」 |
| **06** | 2.1/2.2（索引切片＋尾增量）改为根＋尾可寻址索引；2.4 负锚改为「开档物化回归→红」；06.R2 已勾但其交付（cut 写有界化、具名退化声明、成本表）**被取代**，须附勘误而非撤销勾选；§十五·三 的「无退位的部署永远整份读」具名退化**失去机制**（新引擎无「不维护切面」态） |
| **09** | 4.1 四量探针的 `journalBytes` 口径改指根＋尾；4.3 S2 判据去 `coldReadWallClock`/`indexEntries` 面（`IndexEntries()` 被删）；4.4 负锚须改钉新机制；09.R2 阶段计量去「退位」区间、去「冷读与 boot 分列」；09.R3 CI 矩阵须覆盖新载体。**S1 建议删除**（·八），编排者单写。**新增负锚归属**：`TestSeatReadsTheTasksCommittedPastTheCheckpoint`（·三第二个承重级联） |
| **11** | 11.R2 已落地的 `planEventLocked`/`planFactLocked` 冷层判重分支删除（行为不变、路径变短）；11.R3 的「多键具名拒绝」与 `file` 档逐键提交余项不受影响；`fact-vocabulary` spec 的搬迁前提改写 |
| **05／12** | 其 design 已自带「退役相关性（R51）」节并判定**无断裂**（05：七枚锚不断言退位，「五族永不搬冷」前提删后平凡为真；12：四枚锚不引用退位）。本节复核**同意该判定**，不重复回写。**但 05 须新增一叶**：`DurableCommit.Seat` 的「尾」判据随 `TailKeys` repurpose 而改指 checkpoint 有界前缀（·三），并补该负锚——这是**新增工作**，不是对其既有锚判定的推翻 |
| **07** | **未做 R51 相关性评估**（grep 该域 design 无「退役相关性」节）⇒ 编排者须补评。其 G2 判据若含「治理历史边界由约定改机制」，须核是否隐含热层有界前提 |
| **10** | 5.1.8 词汇登记须加「存储义退位已删除，GLOSSARY 本义保留」；5.1.5/5.1.6 的 spec 同步面扩大（本节 D 表）；5.3.x 死码删除面新增 A 表全部符号 |
| **13** | 假绿形态清单新增两例（本轮实测）：**区间包含**（`storeLoad` 曾含引擎开档）与**自指长度**（order 语句内联自身字节长度导致水位漂移）；13.R2 证据聚合须绑新载体 |
| **G2 判据** | 「任务五族留热」前提**平凡为真**（无冷可搬）；`keptHotKeys` 消费者消失；R36 走查快照不变量（冻结热半＋身份集）**失去被测机制**，其锚 `TestEachIndexNewestFirstStatesOneIdentityPerCommitAcrossARetirement` 删，**走查保序仍须由新锚钉**（单 order 序，无归并） |
| **D6.13／S1** | 由编排者单写反转与处置；本节不编辑 |

### 十七·十一 未判定／未量项（如实）

- **·三 的 store 席位/窗口表载体形状未裁**（S0 停点）。`EventCount`/`commitSeq`/`windows` 在去物化后的来源是本设计唯一未闭合处。
- **生产集成未原型**：真引擎 `journalLog`/`journalKV`/`FileSegmentStore` 的改造、`assembly` 正式 boot 路径、`CommitFact` 五结果在新路径下的红绿对，均未跑。
- **提交成本未优化**：原型 commit 实测 20.0ms（深度 400）／20.2ms（深度 1600），三耐久点＋每目录 dir fsync，高于今日 G3 ~5ms。批处理/组提交未做。
- **原型成本第三深度（6400）未落**：本轮收口时该深度仍在跑（并发叶占用 I/O），§十七·一 表该行标「在跑，未落」。**平坦结论由 400→1600 的 4× 轴与两个增长控制案支撑，64× 轴由 G5 §十六·二 承担**；不得把 4× 读数宣称为 64× 已验。
- **掉电态未测**（只测进程死亡）；`root.json` 的 dir fsync 是尾有界的前提，省掉它的后果未量。
- **原型枚举序与真引擎热冷归并序的逐字对照未做**：单 order 日志按构造逆序即提交序（推导），真引擎归并删除后是否逐字等价须实施期锚钉。
- **`errStopIndexWalk` 与 `TestLoadRefusesAnOrderThatMissesPublishedEntries` 的存活消费方须实施期 grep 复核**，本节按当前 grep 判定保留。
- **既有根是否已填充冷档：未判定，且被明禁以检查用户数据判定**。设计不依赖该答案。
- **G2/G4 未由本节判定，产品不可发布。**

## 十八、S1 执行记录：可寻址索引落地（共存、绿）＋提交成本实测（2026-11-16）

**性质**：实施（S1 单单元）。不动 §十七 冻结的设计，不删退位、不删切面、不勾任何任务框。**裁决中止点**：提交成本实测为今日 5–6 倍，装配消费面已因此转红（见·四），故**生产接线未启用**，交编排者裁（见·五）。

### 十八·一 落地面（三个文件＋一个新增锚组）

| 文件 | 改动 |
|---|---|
| `memory/ledger/journal.go` | 新增三个载体与引擎：`order.log`（一提交一行、内联值 span）、`idx/<namespace>/<hex(key)>`（目录即持有键集）、`root.json`（`{seq,valuesSize,orderSize}` 水位）；`journalAddress` 的 open／derive／rebuild／state（提交）／read（点读）／keys／namespaces／eachNewestFirst（枚举）／retire＋reopened（重写处置）；`journalLog.append` 记 pending、`sync` 在日志屏障返回后 `confirm`、`close` 收 handle、`rewrite` 先 retire 后 reopened |
| `memory/ledger/journal_kv.go` | `statesAddressMaintenance(on)` 转发（声明点，无生产调用者，见·五） |
| `memory/ledger/config.go` | `rejectLegacyShardLayout` 不再把本构建自己的 `idx/` 目录判为退位分片布局（唯一一处，末条备注） |
| `memory/ledger/journal_test.go` | 新增 8 枚锚（文件头锚 `#local-file-kv`，末枚 `#store-instance-sharing`），与既有文件同锚但该文件镜像生产文件 `journal.go`，不触发 `responsibility-fragmentation` |

**共存不变量（逐条对齐 §十七·五）**：`journal.log` 名字与帧格式未动、仍是唯一真相；派生载体只存位置不存值；`l.index`／`l.tail`／切面仍是**生产读路径的唯一来源**——`read`／`keys`／`namespaces`／`eachKeyNewestFirst`／`journalKV.Read` 一字未改，**没有任何生产读路径改走新结构**；`CommitFact` 五结果、槽与 `EventCount` 语义、flock 与 R45 释放判据、R36 走查快照均未触碰。

### 十八·二 与 §十七·二 的一处偏离（有论证，非省事）

§十七·二 冻结 `root.json` 为「temp＋rename」，§十七·四 列 root 的 dir fsync 为尾有界前提。实施改为**原地单次小写＋truncate，不 rename、不 fsync**，理由是可证的：

- order 文件**每条语句内联 `valuesEnd`**，故「最后一条完整语句」自证水位；root 只是**免去回扫一屏 order 文件的加速器**。root 丢失／撕裂／落后 ⇒ 从链重述（§十七·二 第 4 步同一条规则），不损失事实。
- 因此 root 不需要自己的耐久点：它的字节失效只花「重述水位之后的提交」，不花重建历史。
- order 文件**保留 fsync**：它是派生水位的耐久承重件。去掉它，掉电后水位可落后整段进程生命，重建需按 key 创建十万级文件（实测 1.4ms/key，见·三），这是不可接受的放大。
- `idx` 的每命名空间目录 fsync **保留**：目录即持有键集，条目丢失**无可检测面**（点读会静默答 absent），故必须耐久。

**计量收益**：本条改动把「§十七·二 字面序列」的 31.0ms 降到 24–26ms（省 ~13ms），实测见·三。

### 十八·三 提交成本实测（同机同夹具，每提交 5 键，`go test ./memory/ledger/ -run TestZZS1CommitCost`，中位数）

| 深度 | 今日（无索引） | S1（全耐久点） | 差 |
|---|---|---|---|
| 0 | 4.19ms | 26.14ms | 6.2× |
| 400 | 4.11ms | 24.06ms | 5.9× |
| 1,600 | 5.01ms | 25.23ms | 5.0× |

**归因（逐项实测，非推断）**：移除单项后重测同一夹具。

| 项 | 实测成本（深度 400） |
|---|---|
| `journal.log` fsync（今日唯一耐久点） | 4.11ms |
| `order.log` fsync | ~6.0ms（去掉后 24.06→18.06） |
| `idx` 每键条目创建（temp＋rename，5 键） | ~14.7ms（去掉条目写后 24.06→9.44） |
| `idx` 命名空间目录 fsync | ~7.0ms（去掉后 24.06→17.05） |
| `root.json` 原地小写 | ~0（open/write/truncate/close 计在噪声内） |
| 两项 fsync 全去（仅条目写＋journal fsync） | 16.37ms |

**修正 R57／§十七·一 的归因**：G6 判「per-directory fsync 是 4× 的likely cause」**不成立**。真正的主项是**冻结载体形态本身**——`idx/<ns>/<hex(key)>` 一键一文件，APFS 实测 0.45–1.4ms/文件（随目录条目数上升），5 键提交即 15ms，与历史深度无关但与「一提交几键」成正比。per-dir fsync 只占 ~7ms/24ms。**此发现属设计面**：若要让提交回到 1.2×，必须改载体形态（如每命名空间一个 span 文件、或 idx 惰性物化），二者都推翻 §十七·二 的冻结形态，非 S1 可自裁。

### 十八·四 装配消费面实测：成本已使两个测试转红（因果已隔离）

`go test ./assembly/ -count=1`（索引接线开启时）：5 FAIL，其中四枚是成本导致的截止：

| 测试 | 接线开 | 接线关（同树、同夹具） | 判定 |
|---|---|---|---|
| `TestCheckpointCarriesOpenObligationAcrossFold` | FAIL 18.89s | **PASS 1.11s** | 本单成本 |
| `TestRestartRecoveryPinsBeforeTrim` | FAIL 120.49s（`commit filler 1359: context deadline exceeded`） | **PASS 13.03s** | 本单成本 |
| `TestSymptomAnchorDeliveryOutsideWindowSurvivesRestart` | FAIL 120.22s | **PASS 16.03s** | 本单成本 |
| `TestDeliveryOwedTrimNegative` | FAIL 90.47s | **PASS 18.14s** | 本单成本 |
| `TestGovernanceSliceWiredInProduction` | FAIL 1.20s | **FAIL 0.19s**（同样红） | **非本单**（`runtime/task/approval` 与 `features/evolution/governance` 正被并发叶改写；`git status` 实证 +118/−26） |

**因果隔离法**：不改断言、不放宽截止，只把生产接线开关关掉重跑同一命令（`-run` 逐字符同名），四绿一红。故红项归属清楚：**四枚由提交成本、一枚由并发叶**。装配全量另有 `panic: test timed out after 10m0s`，是这四枚长跑拖出的总体超时，接线关后不再出现（同上命令 63.2s 收口）。

### 十八·五 S1 的止点与须裁事项（交编排者）

**已落但未接生产**：`journalLog.statesAddressMaintenance` 由 `journalKV` 转发，**生产 open（`OpenAtomicStore`）未调用**。理由即·四：接线即令四枚装配测试转红，不能同时兑现「共存」与「树绿」。S1 只交付「结构与证明」；接线属 S2/S3，且前置是下面这条裁定。

**须裁（三选一，本单不自裁）**：
1. **接受 5–6× 接受成本，接线并改装配截止**——须 09/03 域改锚（转成有界前缀夹具），且须说明生产提交从 ~5ms 变 ~25ms。
2. **改冻结载体形态**：`idx` 由「一键一文件」改为「一命名空间一 span 文件（按键偏移索引）」或「order.log 唯一 + idx 惰性物化」。前者保留 Keys() 的目录面与点读 O(1)，实测预期回到 1.2×；后者牺牲 Keys() 的有界性。**推翻 §十七·二 的载体表，须重新过设计门**。
3. **保留现形态并接受「idx 只在退位重写/开档时物化」**——即提交只写 order＋root（实测 9.4ms，2.3×），idx 在开档与点读未命中时补；代价是 `Keys(prefix)` 失去目录级有界性（退化为 order 回扫）。

**未做（如实）**：生产接线；`FileSegmentStore.Load` 去物化（S2，属 `segment_store.go`）；退位与切面删除（S3/S4）；spec/词汇/成本表同步（S6）；`SessionEventMap` 无涉。**未测**：掉电态（只测进程死亡，与 §十七·十一 同口径）；冷日志不开索引（有意：冷目录在 S3 消失）；读者视图的水位落后推导（S3 问题，读数视图无法写载体）。

**锚与变异证据（每枚都有杀它的变异）**：

| 锚 | 杀它的变异（改实现→红） | 红原文 |
|---|---|---|
| `TestAddressableIndexOpenReadsTheRootAndTheTail` | 开档恒重建 | `the clean open read [6495 25847 103249] bytes … want the root alone` |
| `TestAddressableIndexPointReadAnswersTheValueAtItsAddress` | 点读忽略 span 偏移 | `read the key its address locates: held=false err=… is corrupt` |
| `TestAddressableIndexStatesOneCommitOrNoneAcrossAStop` | 只填首个键条目 | `1 of 5 key(s) landed after the stop` |
| `TestAddressableIndexDiscardsAStatementItsFrameNeverCarried` | 信任 order 尾（不截到水位） | `the walk named the orphan key: a statement whose frame never landed is not a commit` |
| `TestAddressableIndexRefusesALogThatLostTheBytesItsRootStates` | 水位越界改为重建 | `the open of the cut log = <nil>, want the named corruption` |
| `TestAddressableIndexEnumeratesFromTheOrderFileAlone` | 枚举顺手读值 | `the walk read 2 value(s), want none` |
| `TestAddressableIndexLocatesTheLogARewriteReplaced` | 重写后不重建载体 | `the kept key answered "" (held=false …)` |
| `TestAddressableIndexIsKeptOnlyByTheViewThatDeclaresIt` | 声明点恒 no-op | `the view that declared the index kept none` |

**崩溃注入点与状态映射（如实）**：§十七·四 的六个注入点里，②④（两处 fsync 完成）在**进程死亡模型下与①③字节全等**（fsync 不改内容），故六个注入点落成**五个可区分字节状态**，锚逐状态跑：`before_the_frame`（0/5）、`after_the_frame`、`after_the_order_statement`、`after_the_key_entries`、`after_the_watermark`（后四者 5/5，水位收敛到同一 root）。掉电态才是这四处可区分的场景，未测。

## 十九、S0 设计冻结：store 的 `count`/`commitSeq`/`windows` 载体（2026-11-16）

**性质**：S0 停点裁定（§十七·三 留为未裁的唯一处）。设计与隔离树原型，非生产实现。主树**零 `.go` 写、零 go 运行、零删除**；不勾任何任务框，不改任何 spec、生产文件、既有测试或他域计划。本节只追加，§一至§十七 一字未改。**引擎设计不在本节裁量内**——G6 §十七·二 已冻结，本节只裁 store 载体。

### 十九·〇 隔离树与源身份

- **构造法**：原型为独立 module `/tmp/recovery-s0/proto/`，**逐字复用 G6 引擎原型** `/tmp/recovery-g6/proto/addr.go`（sha256 `b1b5fea296c00cca…`）与 `go.mod`（`bbb41012200c9d2c…`），新增 `store.go`（席位载体＋store 语义）与 `main.go`（探针）。引擎即可寻址索引的冻结设计原型，非主树在制品。
- **主树源身份（只读核对，sha256 全量记于 `/tmp/recovery-s0/ledger-source-sha256.txt`）**：HEAD `9547faca2327c31579e91fbe40a27405d3ffdfb3` ＋工作树未提交改动。`segment_store.go` `9a4ae9f706c56ba2…` **与 G6 快照逐字节相同**，故 §十七·一 的 `storeLoad` 读数（2.02→10.40ms）可归因于当前源。`kv_original_store.go` `fabb385d…`、`migrate.go` `28adc909…` 亦与 G6 相同。
- **⚠️ 源树漂移（如实声明）**：`journal.go`（`601fcc49…`，G6 为 `47dc7f7a…`）、`journal_kv.go`（`b8a0f83a…`）、`atomic_store.go`（`e072765e…`）已被并发 S1 单改动；`config.go` 在本单期间由 `9f8a29f4…` 再漂至 `126c6e86…`（S1 的 `rejectLegacyShardLayout` 调整）。**S1 已落地并自记于本文件 §十八**（共存、绿、生产接线未启用）：新增 `journalAddress`／`order.log`／`root.json`／`statesAddressMaintenance`，与旧切面机制并存，其 `read(key)`／`keys(prefix)`／`namespaces()`／`eachNewestFirst` 面与本设计消费的引擎面一致。`segment_store.go` 复核仍 `9a4ae9f7…` 未动、零席位引用 ⇒ **S2 未开工，S0 停点仍未裁，本单范围完整**。**本节行号按符号名引用，不按漂移行号。**
- **与 S1 止点的关系（不越界）**：S1 §十八·五 因提交成本 5–6× 停在生产接线前，并提出 idx 载体形态三选一（接受成本／改一命名空间一 span 文件／idx 惰性物化）。**S0 载体独立于该分叉**：席位是链内键、窗口 meta 是既有链键，二者只消费引擎的「点读／order 走查」接缝，无论 idx 形态如何裁定都成立。S1 §十八·五 登记的「读视图无法写载体」问题，席位的实例由本节重推规则的写权约束承接（§十九·一）。S1 §十八·二 把 `root.json` 改为无独立耐久点的加速器（order.log 保留 fsync）——**席位不依赖 root.json**，它随事实帧落在 `journal.log`，耐久点是链的 fsync。
- **计量纪律**：store 面与引擎面**分列**——`engineOpen` 单独计时，`storeLoad` 区间**只含席位点读＋解码**，引擎开档在区间外（承 §十七·〇 的区间包含教训）。增长控制案先跑使写回沉降，syscall 面 9 次重复取 min/med 并列。原型崩溃案模拟**进程死亡**（页缓存存续），非掉电。原型值 token 为 base64 JSON（`journalPut.Value []byte`），点读经 `pointRead` 解码回原字节，与真引擎 raw JSON token 仅差一层 base64，span 机制同构。

### 十九·一 冻结的载体形状

**一句话**：`count`/`commitSeq` 由**一个链内席位键** `store:seat` 承接，**与每个事实族同帧写入**、开档一次点读；`windows` **不新增任何载体**——既有 `{pid}:meta:{window}` 链键即真相，按需点读，常驻表删除。

| 量 | 载体 | 地位 | 更新时机 | 开档读法 | 重推时机 |
|---|---|---|---|---|---|
| `count` | `store:seat` 值 JSON `{count,commitSeq}` 的 `count` | **链内派生聚合**（不是链外文件） | 每次 `publishEventLocked`：`count+1`，随事实族**同一帧**落盘 | 一次点读（idx span 文件＋一次 `ReadAt`） | 席位缺失／解不出／`count<0`／`commitSeq<count` ⇒ 从链 idx 族整份重推 |
| `commitSeq` | 同键 `commitSeq` | 同上 | 每次发布：`commitSeq=plan.commit`，同帧 | 同一次点读 | 同上 |
| `windows` | **既有 `{pid}:meta:{window}` 链键，无新载体** | 链本身 | 每次提交随事实族同帧写该窗口 meta（今日已然） | **开档零窗口读**；`nextWindowMeta` 提交时点读该窗口键 | 无需重推（链键即真相，引擎开档已修复其尾） |

**席位键形状**：键 `store:seat`（命名空间 `store`，非整数分区，`Load` 的分区发现与任何剩余 `Namespaces` 消费者都不把它当分区）；值 `{"count":N,"commitSeq":M}`。`count` 与 `commitSeq` **两标量都携带，不互相推导**——孤儿条目（idx 在而窗口 meta 缺）使两者分叉，正是今日 `commitSeq` 注释（`segment_store.go:292-295`）所述语义，席位如实保留。

**发布判定移到点读时**（承 §十七·三 已冻结方向）：一条 idx 发布当且仅当 **idx 持有 ∧ 其窗口 meta 持有**。判重（`planEventLocked`）、读（`GetEvent`/`eventByKey`）、重推扫描都用同一判定。idx 在而 meta 缺的条目**不算持有记录**：重交它规划新族（覆写 idx、落 meta、发布），与今日常驻索引路径逐字同行为（原型差分实证，§十九·三）。

**重推规则（一条，无 legacy 分支）**：席位不可用 ⇒ 枚举分区（`Namespaces` 取整数命名空间）→ 逐 idx 点读解码（复用既有 `indexPositionOf`）→ 按窗口 meta 持有过滤发布集 → `count=len(published)`、`commitSeq=max(count, max published commit)`（**逐字镜像今日 `Load` 的 seed＋max**，`segment_store.go:437-445`，退位删后 `retired=0`）→ **若引擎持有任何键且本视图持写权**则把席位作一帧重述。新根（空）零写入（首个事实提交自带席位）；丢席位根、旧根、坏席位根走**同一条**重推，与 G6 §十七·二 步骤 4 的引擎根重建同形。重推是**一次性 O(历史)**，其后每次开档平坦。**读视图 beside 写者不写**（承 R45/R38 flock 模型）：重推的内存计算任何视图都做，席位帧重述只在持写权的视图发生；读视图遇无席位根就地算出标量、不落盘，由写者开档重述。

**常驻态**：`s.index`／`s.order`／`s.windows` 三张常驻表全删（§十七·三 已裁前两张）；`s.count`/`s.commitSeq` 两标量保留，开档由席位填入。读缓存 `segmentCache` 保留（字节预算加速器，非真相）。

### 十九·二 为何是链内席位键（推导，非直觉选定）

G6 §十七·三 列两候选：「store 席位键」与「由 order 语句 idx 族内联字段派生」。本节裁前者，并裁明后者与「链外席位文件」为何不可：

1. **链外席位文件违崩溃一致性纯度**。若席位是 `journal.log` 之外的派生文件（如 `store.json`），则事实帧与席位写之间存在漂移窗口：崩溃落在两者之间 ⇒ 席位落后于链 ⇒ 开档须重推，而 `count`/`commitSeq` 是**全史聚合**，尾增量补不出 ⇒ **每次该窗口崩溃都强制一次 O(历史) 全读**。这恰是「派生载体不得强制全量读取」判据要删的缺陷。链内席位与事实**同帧原子**，漂移窗口为零，崩溃一致性由引擎帧原子性免费承接。
2. **order 内联派生违引擎分层**。引擎不解释值（R46「引擎不解释领域等价」的推广：引擎按字节存值，身份读回原字节）。让引擎 root 聚合「idx 族净键数」须引擎认识 idx 族语义＝领域解释；且 `count` 仍需全 order 走查除非 root 聚合，而 root 聚合 idx 族即分层穿透。命名空间亦不可代用：`{pid}:idx:`/`evt:`/`meta:` 共享分区命名空间，引擎的通用命名空间计数分不出 idx。故 order 内联派生不可行。
3. **链内席位不是第二真相**。席位是**链的一条陈述**，由唯一写者在根 flock 下、与它所聚合的事实**同一帧**产出；框架内不存在「席位与链不一致」的状态（不一致只能来自违反写协议的外部 raw 写，见 §十九·八）。席位永不参与身份/内容判定（判重读 idx 与 body，不读席位）；席位不可用即从链 idx 族重推，链 idx 族仍是个体事实的真相。这与今日 `settled:events`/`settled:commit`（退位写的链内聚合键，开档信任读）同形——席位是退位删除后承接其聚合角色的链内键，非新机制类。
4. **windows 不立载体是同一判据的推论**。窗口表随窗口数增长，而生产 `WindowTS=evt.Timestamp.UnixMilli()`（`assembly/facts.go:377`）≈每事实一窗口 ⇒ 窗口表最坏 O(历史)。把整张窗口表持久化为一个 blob 并在开档整读＝以较小常数重造「强制全读」缺陷。既有 `{pid}:meta:{window}` 已是每窗口一条链键，可寻址引擎使按需点读 O(1)；唯一消费者 `nextWindowMeta`（退位删后，grep 实证 `s.windows` 仅剩此一处生产读）每提交读一窗口即可。故**删常驻表、不立新载体**才是纯解。

### 十九·三 实测（隔离树原型，非推导）

**深度基准（store 面与引擎面分列；控制案同轴增长证界可失败）**：

| 深度 | engineOpen min/med | **storeLoad min/med**（席位点读） | seatTail min/med（walked） | commit（store 路径） | 控制①物化 Load | 控制②窗口表整读 | 控制③全 order 尾 | 控制④整表每提交重写（墙钟／表字节） |
|---|---|---|---|---|---|---|---|---|
| 400 | 2.41/4.95ms | **310.8/466.6µs** | 9.6/10.4µs（1） | 1.43ms | **223ms** | **148ms** | 770µs | 32ms／**37,161 B** |
| 1,600 | 1.09/1.31ms | **185.0/188.8µs** | 9.8/10.3µs（1） | 860µs | **782ms** | **358ms** | 3.22ms | 48ms／**152,361 B** |
| 6,400 | 0.91/1.12ms | **168.0/172.4µs** | 8.5/9.5µs（1） | 735µs | **3.12s** | **1.39s** | 12.2ms | 26ms／**622,761 B** |

- **storeLoad 对 16× 深度平坦**（min 310.8→185.0→168.0µs；绝对值含负载噪声，**增长轴与控制案的对比才是判据**）。**控制案同轴增长证界可失败**：①物化 Load 223ms→782ms→**3.12s**（14×，今日 `Load` 面，证 store 开档界可失败）；②窗口表整读 148→358→**1,386ms**（9.4×，证「windows 立整表载体并开档整读」可失败）；③全 order 尾 770µs→3.22→**12.2ms**（15.9×，证尾有界可失败）；④整表每提交重写的**表字节 37,161→152,361→622,761 B**（4.1×/4.1×＝线性，确定性增长轴；其墙钟 32→48→26ms 为单发 fsync 噪声主导，**字节序列才是判据**，承 §十五·二 教训），证每提交 O(1) 可失败。**平坦非恒真界。**
- **engineOpen 单列**，不计入 storeLoad（区间不重叠）。commit 为单发计时（含判重点读＋窗口 meta 点读＋一帧含席位），轴平坦（1.43→0.86→0.735ms）。
- **差分等价（seat 开档 == 今日物化 Load，逐夹具）**：clean、孤儿尾、孤儿中、孤儿重交、崩溃六步、撕裂±孤儿语句、无席位重推、坏席位——`assertAgreement` 逐案核 `count`/`commitSeq` 两标量与物化 Load 逐字相等。**孤儿尾**：席位{100,100}==Load{100,100}（孤儿未发布、其秩不被占）；重交孤儿以新秩发布后席位==Load 再成立。**孤儿中**：max published commit 越过孤儿秩，两开档相等。

**崩溃一致性（6/6 注入点，6 键事实帧含席位）**：见 §十九·四。

**重推与零写入**：200 事实无席位根（外部 fold 脚本形态）重推 228–719µs（一次性 O(历史)，绝对值含负载噪声）、席位重述、其后开档 min/med 192–198/199–213µs 平坦；坏席位（`{"count":`）走同一重推规则恢复 {200,200}；**新空根 seatLoad 零写入**（root seq=0、无席位键），首个事实提交自带席位。

### 十九·四 崩溃一致性（原型实测）

席位与事实族同帧，故引擎六注入点即席位注入点，无独立席位漂移窗口。六步实测均收敛同一 root（与 S1 §十八 末「进程死亡模型下②④与①③字节全等、六步落五个可区分字节状态」一致：fsync 不改内容，故席位在各步的落点只由帧是否完整决定）。

| 案 | 构造 | 实测 |
|---|---|---|
| 崩溃步①—⑥ | 120 提交 populated 根，第 121 提交（6 键：evt/idx/meta/ref/blob/**seat**）在指定步注入进程停止，重开 | **6/6 收敛**：停止提交 **6/6 键全落**（页缓存存续、帧完整），席位{121,121}==物化 Load，root seq=121；身份读回原 256B 原文 |
| 撕裂末帧 | 根水位后手工追加少 7 字节帧（含 idx＋seat） | 截在水位，root seq=120 不变，席位{120,120}不变，撕裂键 absent |
| 撕裂末帧＋孤儿 order 语句 | 同上，另追加完整 order 语句命名该撕裂帧 | 同样截在水位、席位不变、键 absent ⇒ **order 与席位均不被当第二真相** |
| 无席位根 | 200 事实 raw 提交（无席位） | 开档重推 {200,200}==Load，席位重述，其后平坦 |
| 坏席位 | raw 覆写席位为不可解析字节 | 同一重推规则恢复 {200,200} |
| 新空根 | `addrCreate` 后 seatLoad | 零写入（seq=0），首事实提交带席位 |

**停机多键提交全落或全不落**：6 键一帧，实测全落；席位 never 站在半个事实旁（同帧原子）。**掉电未测**（只测进程死亡）：席位随事实帧落在 `journal.log`，其耐久点是链的 fsync（S1 保留）；`root.json` 经 S1 §十八·二 已是无独立耐久点的加速器，席位不依赖它。掉电边界与 G6 §十七·四／S1 §十八 同口径。

### 十九·五 `Seat` 尾谓词与负锚（05 域新叶的面与判据，本域只供不实现）

**链路（grep 实证）**：`journalKV.TailKeys()`（journal_kv.go:123）→ `assembly/checkpoint.go:948 tailFactKeys` → `:925 durable.Seat(ctx, stated, tailFactKeys(tier))` → `runtime/state/durable.go:427 Seat`，**`Seat` 按 `tail == nil` 分支**（:436）。`TailKeys` 生产消费者**仅此一条链**（grep 全仓实证）。

**G6 规格的结构成立，但「不需要读值」一句须勘误**：

- **成立**：尾改由「序结构相对最新框架 checkpoint 的有界前缀」定义；checkpoint 是 order 里的一条语句，无需 seq 映射；走查长度＝checkpoint 之后的提交数；引擎不声明序或找不到框架 checkpoint ⇒ nil（保留今日读全账兜底）。
- **勘误（承重）**：G6 谓词按键拼写 `checkpoint:<partition>:<seq>` 识别锚、声称「不需要读值」。**框架 checkpoint 与 session checkpoint 共用同一键拼写**（`api/event/checkpoint.go:75-81` `CheckpointKey`，`assembly/checkpoint.go:354` 与 `:503` 同用），且该 API 文档明令「**The family is read back by the frozen event type, never by this spelling**」。故锚识别**必须读 idx 携带值**判 `type==event.TypeCheckpoint ∧ session==""`（框架 checkpoint 的 `Owner.SessionID=""`，`checkpointOwnerLocked` 返回 `Address{AgentID}`；session checkpoint 带 `SessionID`，`sessionOf` `facts.go:539` 取 Owner/Target/Source 首个非空）。引擎不内联 type/session，故走查中**每个 idx 条目一次携带值小读**（inline span 直读，非整值读）。

**精确谓词**（repurpose `TailKeys`，journal 档可寻址引擎）：
```
自 root.orderSize 逆序走 order，逐语句：
  若该语句某 idx 族键的携带值满足 type==TypeCheckpoint ∧ session==""：
      停；尾 = 严格 newer 于该语句的各语句之全部键（puts∪deletes）；返回非 nil 尾
  否则：收集该语句全部键入尾，继续
走到 order 起点仍未遇框架 checkpoint ⇒ 返回 nil（读全账兜底，绝不返回空集）
引擎不声明序能力 ⇒ nil
```
- **覆盖无漏论证**：`Seat` 的 `stated` 来自 boot 恢复的最新 checkpoint（`restoreNewestCheckpoint`→`newestCheckpointRef`=`NewestOfType(TypeCheckpoint)`）。**session checkpoint 不携带 tasks 切片**（`tasksStateSlice` 无 `SnapshotSessionState` 方法，`StateRegistry.SnapshotSessionState` 只问 `sessionSnapshotter`，grep 实证）⇒ `stated` 非空当且仅当恢复的是框架 checkpoint F ⇒ 此时 F 即最新框架 checkpoint，尾锚于 F 恰覆盖 F 之后全部任务记录。`stated` 空 ⇒ `Seat` 走 `d.load` 全读，尾无关。故锚 ≤ stated 的 checkpoint 恒成立，无静默漏座。
- **跨分区**：锚是全局序中的单条语句，其后所有分区的键都收集；其前由 checkpoint 的全局 tasks 账覆盖（账是全局非分区）。durable 族键（`tagent/task/v1/{fact,spec,consume,attempt,reject}:`）经 `TierFactLog` 走**同一 journal** 的 raw 提交，order 每帧一条语句 ⇒ 走查看得见（故尾必须定义在**引擎 order 全键**上，而非 store idx 族走查——后者漏 raw durable 提交）。

**负锚（三枚，皆可构造失败）**：
1. **空尾陷阱（G6 原锚）**：把尾实现为「root values 水位之后的键集」⇒ 干净根常态**空非 nil** ⇒ `Seat` 走 checkpoint 路径读零年轻任务。原型实证：watermark 尾命名年轻任务 **0/3**。锚 `TestSeatReadsTheTasksCommittedPastTheCheckpoint`：checkpoint 后提交任务记录、重启入席，账必须可读；此实现 ⇒ 红。
2. **拼写陷阱（本节新增）**：锚识别只按键拼写 `checkpoint:` 前缀、不读 type/session ⇒ 停在更晚的 **session checkpoint** ⇒ 漏掉它与框架 checkpoint 之间的任务记录。原型实证：spelling 尾 walked=2，**丢 taskA/taskB**（仅命名 taskC）；framework 尾 walked=5 命名 taskA/taskB/taskC 全。此陷阱在 production boot 首 Seat 路径今日被掩（最新为 session checkpoint 时 stated 空 ⇒ 全读），但谓词契约在 Seat 接缝处必须成立，且 frozen-type 规则独立禁拼写识别。
3. **nil≠空集**：无框架 checkpoint 时答 nil（全读兜底）而非空集。原型实证：无框架 checkpoint 根 ⇒ nil，walked=61（O(历史)，声明）。

**成本**：尾走查＝O(最新框架 checkpoint 之后的提交数) 语句×每 idx 条目一次携带值小读；nil 兜底＝O(历史) 走查＋随后 `d.load` 全读（与今日整读同阶，声明不得称有界）。

### 十九·六 不变量逐条对设计

1. **身份读回原字节**：席位只存两标量，不存值；判重/读仍点读 idx＋body，`ReadAt`＋`Unmarshal` 同今日路径。原型逐案核 256B 原文。
2. **全局提交序**：一帧一提交、一 order 语句一提交；席位随帧，不引入第二序。
3. **一身份一不可变事实**：idx 一键一文件，席位不参与身份判定。
4. **领域等价重交＝重复**：判重仍在 store 层点读 idx＋body 比对（R46 次序：领域门先于引擎字节门），引擎不解释领域等价；席位不参与。
5. **`EventCount`／槽位语义零改**：`count` 由席位承接，口径＝全部成功提交事件数（发布集大小）；槽位 (partition,window,seq) 识别与排序不变。差分实证 seat 开档 == 今日 Load。
6. **`commitSeq` 不回归、不被缺窗口 meta 愚弄**：席位只在发布时推进，跨重启读回最后发布秩 ⇒ 不回归；孤儿（idx 在 meta 缺）未发布 ⇒ 席位不为它推进 ⇒ `commitSeq`=max published rank，与今日 `max(count, max published commit)` 逐字等（退位删后 count≤maxRank）。两标量独立携带，保留分叉语义。差分实证孤儿尾/中/重交三态。
7. **flock 写权与 R45 释放不变量**：席位在**同一根**链内，一根一写者不变；席位随事实帧，不新增耐久点、不改「在飞」定义 ⇒ 释放判据（`len(flying)==0 && writers.Stopped()`）逐字不动。
8. **`CommitFact` 五结果**：席位随事实帧 ⇒ Committed 帧含席位；AlreadyPresent/Conflict/Refused 无帧；Uncertain 窗口（引擎①—⑤）帧可能含席位整体落地，重开席位与 idx 同帧一致 ⇒ 五值不增不减，无第六结果。

### 十九·七 具名操作成本表（S0 新增行；未列入者视为未承诺）

| 操作 | 成本随何增长 | 实测依据 |
|---|---|---|
| store 开档（席位点读） | **常数**（一次点读，与历史无关） | §十九·三 storeLoad 310.8→185.0→168.0µs 对 400→1600→6400 |
| 窗口 meta 读（`nextWindowMeta`） | **常数**（每提交一窗口点读）；开档**零窗口读** | 控制②窗口表整读 148→358→1386ms 证整表载体可失败 |
| 事实提交（store 路径） | **常数**（判重点读＋meta 点读＋一帧含席位；席位零新增耐久点） | commit 1.43→0.86→0.735ms；控制④整表字节 37,161→152,361→622,761B 线性证可失败 |
| 席位重推（席位不可用） | **一次性 O(历史)**（全 idx 族点读＋meta 持有判定） | 200 事实无席位根 228–719µs（负载噪声），其后平坦 |
| `Seat` 尾走查 | **最新框架 checkpoint 之后的提交数**；nil 兜底 O(历史) | seatTail walked=1（cadence 内）；控制③全 order 尾 770µs→3.22→12.2ms |
| 全序枚举 | **O(历史)**（承 R50，不得称有界） | 引擎 order 全走 |
| `EachIndexNewestFirst` 兜底（引擎不声明 `CommitOrder` 与 `indexKeyOrder`） | **O(已发布索引条目) 全量物化＋排序**；早停只约束 visit 数，不约束读取量；**现键布局下本征，非可修缺陷** | 锚 `TestEachIndexNewestFirstFallbackMaterializesTheWholeIndex`（memory/ledger/segment_store_test.go）：首 visit 即停的走查在深度 200 读 225 条、深度 2000 读 2250 条（idx 对＋窗口 meta 键），严格线性；控制案（引擎只答有界前缀）读 41/266，具名红 |
| store 开档完整性校验（全 log 帧 CRC） | **O(日志字节)**，**每次开档付**；与索引回放窗口是两个量，窗口在停稳根为 0 而本行不为 0 | `journal.go:615` `journalReplay(file, 0, size, no-op)`；`JournalBytesOpen` 只报索引窗口，故 `assembly/symptom_anchor_recovery_test.go:465` 的 `journalBytes==0` 锚**量不到本行成本、恒绿**（R182 立案：须限定其语义并另立钉住本行线性事实的量与锚，**待派**）；墙钟 `TestSymptomAnchorBootCostFlatAcrossGenerations` 实测仍 0（CRC32C 硬件加速），**但墙钟绿是成本读数，不解除本行声明义务** |
| 已结任务按名索取（懒水合首次） | **O(历史键容量)×5，逐任务付一次**；此后 `d.facts` 命中为常数 | G37 journal tier 独立程序实测（生产接线 OpenTier→TierFactLog→EmptyDurable＋RestoreChainSource＋RestoreCheckpoint）：200 已结＝首次 456µs／5 次前缀 scan（spec 1 键 271B、fact 3 键 1267B、余三前缀 0 键），每 scan 顺序读整槽表 4096 槽×96B=393216B，×5≈1.97MB；命中 250ns／0 scan。1000 已结＝槽表 16384（4×）、首次 2.6ms（≈4×）、×5≈7.86MB；命中 167ns。**checkpoint 体两深度恒 973B**（已结不回检查点）。锚：`runtime/task` 八个 `TestRestart*` 正锚＋陌生身份负锚，回退水合各具名红 8/8 |


**懒水合首次索取行声明（本征成本；声明，不是放宽）**：

- **机理**：`Records`→`readTaskLocked`（`runtime/state/durable.go`）做 **5 次前缀 scan**（spec/fact/consume/attempt/reject），`eachHeld`（`memory/ledger/journal.go`）→`eachRecordOnce` **整表顺序读 `a.capacity` 槽**再 HasPrefix 过滤 ⇒ 单次 scan 成本随**槽表容量**（非存活键数）线性，五次叠加。首次按名索取付该成本，此后 `d.facts` 命中不再读。
- **归索取者，不摊进 boot**：boot 只落座仍欠义务的任务（`settledAtCut` 语义），**已结任务不被 eager 恢复**；成本由**命名该任务的读者**付（`Obligations`/`Handoff`/`Flush`/`HandoffOf`/`IssueNotice`/`RecordStop`/`ProgressReported`/`Logical` 八面，G37 已统一走 `hydrateLocked`）。**遍历常驻集的面一律不加水合**（`Watch`/`Unfinished`/`Owed`/`Retained`/`RecoveryMaterial`/`activeCountLocked`/`pruneTerminalLocked`），否则会把 O(历史) 摊进 boot/close。
- **本征还是可修**：现布局下 scan 须顺序读整槽表 ⇒ **本征**；若日后要有界，修复面在**槽表按前缀可收窄**（引擎侧寻址能力），不是放宽本声明，也不是给遍历面加水合。
- **判据**：R121.3「把本征成本放在真需要它的消费者那里，而不是摊进每次 boot」、R135⑤「本征成本归索取者并显式声明」。**本行此前未列入任何成本表＝未声明（R161 立案），现据 G37 实测补声明。**

**开档完整性校验行声明（安全必需的本征成本；声明，不是放宽）**：

- **为何保留全扫而非改读时校验（R182 裁定，G29 取证）**：①**存在承重绕过点**——`addr.value`（journal.go 约 :1654）直接 `ReadAt`＋`json.Unmarshal` **无 CRC**，而它是一切取值的唯一出口（点读／前缀／遍历全经它），故「每条读路径都过 CRC」今日为假；②**锚的变异落在值粒度之外**——`journalTestFlipLastByte` 翻的是文件末字节（payload 的 `}`），落在一切 value span 之外，值粒度校验原理上看不见它，改读时校验会使该变异恒绿＝假锚，故校验粒度必须是**帧**；③**冻结契约面要求开档即拒**——`journal.go:4-9` 包 doc（HEAD 逐字同）、`docs/wiki/memory/memory-architecture.md`「完整帧校验不过即拒开」、`golden/runtime/durable_truth.json`、`TestJournalFraming/rejects_corrupt_middle_frame`、`storage_capabilities_test.go` 的 damaged-record 子测试。
- **水位为何不能背书**：水位只在 `fileCommit` 前进，其调用方在 `file.Sync()` 之后、`derive` 在 CRC 回放之后 ⇒ 无推过未 sync 字节的路径；**但「写入时正确」≠「此刻完整」**，水位不为其下字节的当前完整性背书。
- **改读时校验的代价（若日后重开）**：`journalAddressSpan` 只有 `{Offset,Length,Null}` 无帧起点 ⇒ 帧级 CRC 须读整帧，点读 V→F（小值约 6×，checkpoint 帧近 32MiB 时读一个值付整帧），且须 `journalSlotVersion` 2→3 格式升级（span/spanOf/encodeSlotRecord/decodeSlotRecord/journalSpansAt/orderStatementOf 全链）。替代变体（值 CRC 入 span）保 O(V) 点读，但**在同一批字节上立第二份校验和，撞判据③无第二真相／④更少特异化**。
- **已知代价（须一并声明，不得只说本行安全）**：`journal.go:544` 把「长度域超上限或剩余不足」一律当 torn tail `return offset, nil`，写面随后 `Truncate` **静默切掉该帧及其后全部历史**；而 `journalPayloadOf:506` 对同一情形判 `ErrJournalCorrupt`——**两函数判据相反**。该洞 HEAD 同形（既有缺口，另立变更处置），**全扫把它的暴露面从窗口扩到全历史**。
- **判据**：R135 ⑤「本征成本归索取者并显式声明」、R121.3。**本行是摊进 boot 的成本，故必须显式声明而非声称有界**；索取者是「开档须拒绝损坏」这一安全契约本身，不是某个可选报告。

**兜底行声明（本征成本；声明，不是放宽）**：

- **机理（读码）**：引擎不声明序能力时，`eachIndexEntryNewestFirst`（segment_store.go:1215-1235）走 `publishedEntriesOf`（:407）：每分区一次前缀读取回**全部** idx 对（:411）＋一次窗口 meta 键 `Keys` 枚举（:418），物化全部条目并按索引序排序（:443）；兜底分支再按 `indexPosition.commit` 降序稳定排序（:1219）后逐条 visit。早停只终止 visit 循环（:1222-1234），不约束读取量。
- **本征，非可修缺陷**：键＝`分区:idx:事件键`（:647），session/type/commit 全在携带值里（:651-655），前缀扫描无法按会话/类型收窄，排序须读全部值。现布局下此成本本征；若日后需要有界，修复面在引擎序能力（声明 `CommitOrder`），不是放宽本声明。
- **现构建无可达的耐久受害者**：①journal 档实现 `indexKeyOrder`/`CommitOrder`（journal_kv.go:268/:294），走有界前缀；②file 档（rustviking）`StoreEvent` 恒 4 键批（segment_store.go:664-670），`rustvikingKV.Commit` 拒 >1 键批（rustviking_kv.go:150-152，`ErrBatchNotAtomic`），由 `TestStorageCapabilities` 子测试 "file tier runs the real rustviking CLI and names the batch it cannot carry"（tests/integration/history/storage_capabilities_test.go:346）钉死 ⇒ file 档事实层不可用，索引恒空；③memory 档非耐久（config.go:296 `Durable: cfg.Type != StorageMemory`），跨进程退出持零，boot 无历史可物化。memory 档进程中途的走查（查询/回放通道）由索取该读法的消费者付费。
- **观测缺口（如实）**：`indexWalkValueReads`/`indexWalkValueBytes` 只在两条引擎序路径计数（segment_store.go:1183-1184/:1204-1205），兜底分支只计 `indexWalkVisits`（:1226）。锚故以测试侧 `seatReads` 接缝计数器量（前缀读返回对数＋`Keys` 枚举键数，segment_store_test.go:684）；生产计数面未改。
- **重开条件**：若日后批准 file 档事实层可用（批能力或提交形状变更），此界重开，届时须同批交付引擎序能力；兜底成本不得被耐久 boot 静默继承。
- **判据**：ORCHESTRATION.md R121.3「把本征成本放在真需要它的消费者那里，而不是摊进每次 boot 与每刀 checkpoint」、R135 ⑤「本征成本归索取者并显式声明」、R123「boot 不该为没人索取的报告枚举历史」——本行是兜底走查本征成本的显式声明，不声称兜底有界，也不把该成本摊进 boot 的许可。

### 十九·八 未证／未量项（如实）

- **生产集成未原型**：真 `FileSegmentStore.Load`/`planEventLocked`/`publishEventLocked`/`nextWindowMeta` 改造、`CommitFact` 五结果在新路径的红绿对、assembly 正式 boot，均未跑。原型是独立 module 对冻结引擎设计的镜像，值格式逐字对齐 `segment_store.go`，但非在制品。
- **席位「可解析但陈旧」态框架不守卫**：唯一写者＋同帧原子使框架内不可达；外部工具向**活根** raw 写事件族（绕过 store）会使席位陈旧——这是写协议违例，框架不检测（flock 挡第二写者；`recover-kv` restore 只写 `blob:` 孤儿原文，grep 实证 `main.go:162`，不写事件族；`fold-cold-root` 按 G6 §十七·九 写**新根**，首开档重推）。**外部脚本须守「raw 事件族只写新根」契约**，否则席位陈旧不被框架纠正。此边界声明，不称穷尽。
- **掉电未测**（只测进程死亡）；席位耐久依赖事实帧的 `journal.log` fsync（S1 保留），不依赖已改为加速器的 `root.json`。
- **提交成本未优化**：原型 commit 含 store 面两次点读，绝对值高于今日一 fsync 路径；批处理/组提交未做。
- **`indexPositionOf` 容错差异**：真实现容忍 idx 值无携带 JSON（`len(fields)==2`，commit=0）；原型重推跳过此形态。框架内 `planEventLocked` 恒写携带 JSON ⇒ 不可达；实施期重推**复用既有 `indexPositionOf`**（非转写）以保真。
- **Seat 谓词的 production 接线未实现**（归 05 域新叶）；本节只供面、判据与负锚。
- **G2/G4 未由本节判定，产品不可发布。**
## 二十、R67 派单：四案载体形态实测与裁定建议（2026-11-16）

**性质**：测量（隔离树）＋裁定建议。主树**零 `.go` 写**、零 go 测试运行、零删除、不勾任何任务框；§一至§十九 一字未改。本节的读数回答 R67 的三问：选项 2／选项 3／编排者自选项 4 各付什么代价、`idx` 是否可整体删除、按读数＋纯度该取哪一形。

### 二十·〇 隔离树、源身份与测量层

- **构造法**：`rsync -a` 主树**工作树**（非 HEAD）至 `/tmp/recovery-carrier/tree/`，排除 `.git`、`examples/`、`docs/.dev`、`.tmp`、`node_modules`。
- **源身份**：HEAD `9547faca` ＋工作树未提交改动。逐字节核对并落 `/tmp/recovery-carrier/source-sha256.txt`：`journal.go` `601fcc49…`、`journal_kv.go` `b8a0f83a…`、`config.go` `126c6e86…`、`segment_store.go` `9a4ae9f7…`、`journal_test.go` `688c536b…`——**与 S1（§十八）／S0（§十九）记载逐字节相同**，故 §十八·三 的基线读数可直接对照。隔离树 `go vet ./memory/ledger/` exit 0，基线套件 **73 顶层＋37 子、0 FAIL**（31.6s）。
- **测量层**：`journalAddress` 加一个 `shape` 字段与四个分支点（open／state／read／keys＋namespaces／flush），形状由 `TAGENT_ADDRESS_SHAPE` 或 `statesAddressShape` 选择。**这是测量开关，永不落地**；赢家落地时须是不带开关的单一形态。
- **计量纪律（承 §十七·〇／§十九·〇）**：开档**分面计时**——`openJournalLog`（引擎开档，今日 O(历史) 的常驻索引重放）在计时区间**外**，只量 `openJournalAddress`；提交取 24 次样本中位数、前置 2×深度 预热；点读／开档各 9 次、控制案 3 次取中位数。**绝对毫秒含并发负载噪声**（同期另有单在跑 `runtime/state`、`scripts/gates`），故**增长轴与控制案的对比才是判据**，单点绝对值不可外推。
- **控制案构造**：①开档——删 `root.json` 后**每次**取样前重删，使每样本都付重建；②点读——读一个从未提交的键（答案必为 absent，走查不早停）；③枚举——同夹具两次调用，第一／第二次分列。

### 二十·一 提交成本：回归有两个独立来源，各案只消其一

夹具每提交 N 键、64B 正文，`depth` 行是已完成的历史。中位数毫秒。

| 深度／键数 | 今日（无索引） | S1 一键一文件 | 选项2 `keys.log` | 选项3 惰性 | 选项4 只 order |
|---|---|---|---|---|---|
| 0／1 键 | **4.796** | 18.241 | 12.983 | 9.032 | 9.432 |
| 0／5 键 | **4.105** | 19.816 | 14.584 | 9.020 | 8.994 |
| 400／1 键 | **4.002** | 15.867 | 14.602 | 9.863 | 9.078 |
| 400／5 键 | **4.144** | **25.102（6.1×）** | 13.439（3.2×） | 9.056（2.2×） | 9.051（2.2×） |

**屏障对照（同一形状，只关 order 屏障）**：选项4 `order-sync=true` **9.076／8.958**（1／5 键）→ `order-sync=false` **4.803／4.872**。**order 屏障一项＝4.2ms。**

**原语对照（`TestZZPrimitiveCost`，同机）**：journal 1000B＋fsync **5.036ms**（即今日一次提交的屏障本体）；追加载体文件 5 行＋fsync **5.162ms**；就地槽写 5×40B **无 fsync 0.007ms**、**有 fsync 5.031ms**；每键文件（stage＋rename）5 键＋目录 fsync **48.906ms（空目录）／23.042ms（目录已 32,000 条目）**。

**归因（实测，非推导）**：一次 fsync 在本机约 5ms，故

- **今日 4.1–5.0ms ＝ 一道屏障。**
- **选项2／3／4 的 9.0–9.4ms ＝ 两道屏障**（journal＋载体各一道）。**这是任何"自带耐久点的派生载体"的常数下限，与载体形状无关**；选项3／4 已把每键文件全部去掉，仍停在 2.2×，正是因为它。
- **S1 的 5–6× ＝ 两道屏障 ＋ 每键文件创建**（5 键实测 +15～21ms，与 §十八·三 的 1.4ms/键同向，本机更慢）。
- **单点小写的成本近零**（5×40B 就地写 0.007ms），故"就地槽表"这一形态在提交面上不是瓶颈；**瓶颈是屏障的道数**。

### 二十·二 开档：四形状同界，控制案可失败

只量地址开档（引擎开档在区间外）。`bytes` ＝ `addr.openBytes`（本次开档读入字节）。

| 深度 | S1 文件 | 选项2 | 选项3 | 选项4 | 控制（删根，每次重删） S1 ／ 选项4 |
|---|---|---|---|---|---|
| 400 | 1.359ms／50B | 0.614／50 | 1.499／50 | 1.053／50 | **1,712.9ms**／260,000B ｜ 12.247／260,000 |
| 1,600 | 1.068／52 | 1.758／52 | 1.040／52 | 1.002／52 | **6,148.4ms**／1,040,000B ｜ 15.398／1,040,000 |
| 6,400 | 2.680／53 | 1.617／53 | 1.209／53 | 0.922／53 | **23,805.9ms**／4,160,000B ｜ 42.378／4,160,000 |

- 干净开档四形状**读入字节 50→52→53B 恒定**（16× 深度），墙钟 0.6–2.7ms 无增长轴。
- **控制案同轴增长且确定性**：字节 260,000→1,040,000→4,160,000（16× 深度＝16×，**线性**）⇒ 界可失败，平坦不是恒真界。控制案墙钟选项2/3/4 为 16.0→29.2→67.5／12.0→17.1→40.8／12.2→15.4→42.4ms；**S1 为 1.7s→6.1s→23.8s**（每键建 32,000 个文件）。

### 二十·三 点读：只有"每键一份可寻址位置"是常数；其余随距离增长

中位数毫秒。`newest`＝最后一次提交写的键；`mid`＝深度中点的键；`oldest`＝最早提交的键；`absent`＝从未提交的键（**控制：走查不早停**）。

| 深度 | 形状 | newest | mid | oldest | absent（控制） |
|---|---|---|---|---|---|
| 400 | S1 文件 | 1.333 | 0.696 | 0.486 | **0.004** |
| 400 | 选项2 | 0.060 | 1.023 | 1.772 | 1.459 |
| 400 | 选项3（未物化） | 0.009 | 0.325 | 0.664 | 0.724 |
| 400 | 选项4 | 0.041 | 0.915 | 1.571 | 1.340 |
| 1,600 | S1 文件 | 1.024 | 0.902 | 0.523 | **0.005** |
| 1,600 | 选项2／3／4 | 0.048／0.028／0.027 | 2.792／2.761／2.667 | 3.669／3.461／3.504 | 3.206／3.044／2.958 |
| 6,400 | S1 文件 | 0.919 | 0.960 | 0.645 | **0.003** |
| 6,400 | 选项2／3／4 | 0.023／0.014／0.015 | 6.360／6.024／6.123 | **11.513／10.803／10.431** | **11.798／10.347／10.526** |

- **S1 的每键文件是唯一与历史深度无关的点读**（oldest 0.486→0.523→0.645ms 平坦；absent 0.003–0.005ms 判"不持有"而不走查）。
- **选项2／3／4 的点读成本＝返回键的最新语句的距离**：oldest 1.57→3.5→10.4ms（6.6× 对 16× 深度）；absent（控制）1.34→2.96→10.53ms，与 oldest 同阶 ⇒ **键不存在时必走完整份 order**，这是走查型载体的结构性质，不是夹具假象。
- **选项3 在物化后能回到 S1 的读数**（未在本表采样：物化前它等同选项4），代价见·四的突发。

### 二十·四 枚举：四形状都 O(历史)，选项3 把成本搬成突发

`keys("0:evt:")` 第一次／第二次，`namespaces()`；深度 6,400 时持有 32,000 键。

| 深度 | 形状 | keys 第一次 | keys 第二次 | namespaces |
|---|---|---|---|---|
| 400 | S1 文件 | 3.889 | 3.698 | 0.836 |
| 400 | 选项2 | 1.918 | 2.589 | 2.044 |
| 400 | 选项3 | **1,418.853** | 1.228 | 0.173 |
| 400 | 选项4 | 3.830 | 3.273 | 2.627 |
| 1,600 | S1 文件 | 6.779 | 7.840 | 1.235 |
| 1,600 | 选项3 | **4,536.398** | 4.243 | 0.219 |
| 1,600 | 选项4 | 11.600 | 7.430 | 6.243 |
| 6,400 | S1 文件 | 28.718 | 20.926 | 1.102 |
| 6,400 | 选项2 | 36.754 | 15.066 | 15.544 |
| 6,400 | 选项3 | **15,634.207** | 17.935 | 0.234 |
| 6,400 | 选项4 | 25.942 | 16.225 | 15.140 |

- **四形状的枚举都随历史／存活键集增长**（25.9–36.8ms @32,000 键），承 R50 **不得宣称有界**。选项4 的 order 走查（25.9ms）**不劣于**目录扫描（28.7ms），且 `namespaces` 由走查派生（15.1ms vs 目录 1.1ms——**这一项更慢**，如实记）。
- **选项3 把每键文件的 1.4ms/键 从提交搬到首次枚举**：32,000 键⇒**15.6s 单次突发**，其后 17.9ms。这不是消除成本，是改交付时机（且该突发落在读路径上，无写权视图无法物化 ⇒ 读视图每次开档重付）。

### 二十·五 不变量与崩溃一致性：四形状跑同一套锚

- **同一 73 顶层＋37 子套件在四种形状下全绿**（env 选形；`TestAddressableIndex*` 八锚、`rewrite`/`reopen`、flock 两根、`CommitFact` 五值、`EventCount`、原字节、领域等价重交）：`keyfiles` 68.6s、`keyslog` 34.9s（放宽夹具后重跑）、`lazy` 23.1s、`orderonly` 23.4s，**四者 exit 0、0 FAIL**。⇒ **四形状都不破坏套件断言的不变量**（这是形状无关性证据，不是接线证据）。
- **一处夹具缺口（如实）**：S1 的崩溃锚 `TestAddressableIndexStatesOneCommitOrNoneAcrossAStop` 的注入点把"键条目已落"写成「`idx/` 前缀下新增文件」（`addressableTestEntries`）。**载体是文件时该注入点无从表达**，故 `keyslog` 首轮红：`the watermark states the commit at 2835 beside 0 of 5 held key(s)`。**这是夹具按 S1 形状写死，不是锚判据错**；把该 helper 放宽到「`idx/` 前缀 **或** `keys.log`」后 **73/37 全绿**。断言一字未改。
- **撕裂／孤儿的键线（本节新增注入）**：在载体尾部追加一条完整语句与一条少 7 字节的语句，各形状均要求「键不可读、走查不命名、列举不命名、root 水位不变」。`keyfiles`／`lazy`／`orderonly` 直接绿（键载体是 order 或目录，order 由 root 水位截断）；**`keys.log` 首轮红**：`keys.log states no key line: unexpected end of JSON input` ⇒ **选项2 需要一条额外规则**——「一行完整当且仅当其后跟换行」，否则撕裂尾线使**开档拒绝**而非丢弃。补此规则后两注入点皆绿。
  - **这是选项2 相对选项4 的实质复杂性代价**：order 的撕裂线由**独立水位**（root 的 `OrderSize`）截断，不需要行内自证；键线文件没有独立水位可依（它的水位就是自己的最后一行的 `seq`），故必须自带"完整行"规则。**选项4 白得一条规则，选项2 要自己写一条。**
- **六注入点**：S1 的六步在进程死亡模型下落五个可区分字节态（②④ fsync 不改内容，与①③字节全等，承 §十八·四），随形状跑均收敛（四形状套件绿即含此锚）。**掉电未测**，与 §十七·四／§十八 同口径。
- **`-race -count=10`（选项4，八锚＋两枚 flock 锚）**：**100 PASS／0 DATA RACE**，59.3s，exit 0。

### 二十·六 裁定：逐案按读数＋纯度

| 案 | 提交 | 开档 | 点读 | 枚举 | 纯度 | 判 |
|---|---|---|---|---|---|---|
| S1 一键一文件 | 5–6× | 0.9–2.7ms 恒定 | **0.5–1.3ms 恒定** | 28.7ms／O(存活) | 三载体，目录即集 | **禁**（R67 选项1：改锚迁就回归） |
| 选项2 `keys.log` | 3.2× | 0.6–1.8ms 恒定 | 1.8–11.5ms **随距离** | 36.8ms | **同一持有集的第二份表示**（order 已陈述同一信息）＋需自带完整行规则 | **否**：纯度与成本双输 |
| 选项3 惰性物化 | 2.2× | 1.0–1.5ms 恒定（未物化时点读仍走查） | 物化后同 S1；物化前 10.8ms | **首次 15.6s 突发** | 仍是同一集的第二份表示，只是晚建 | **否**：不消除成本，只搬时机，且搬上了读路径 |
| 选项4 只 order | **2.2×** | 0.9–2.7ms 恒定 | **10.4ms 且随历史线性** | **25.9ms／O(历史)** | **单载体，最纯** | **枚举半取，点读半否** |

**选项4 的「`idx` 可整体删除」判定＝半真**：

- **真**：`Keys(prefix)`／`Namespaces()` 可从 order 派生，且**不劣于**目录扫描（25.9 vs 28.7ms @32,000 键）⇒「目录即持有键集」这一**性质**确实冗余，可删。
- **假**：`idx` 同时是**点地址**。删掉它，点读＝逆向走查，实测 **10.4ms@6,400 且随历史线性**，**absent 键（控制）同一读数**。按 §十九·一，store 的判重（`planEventLocked`）**每提交一次**点读一个通常 absent 的 `{pid}:idx:` 键 ⇒ 选项4 下每次提交额外付**整份 order 走查**。故选项4 不是"更省的结构"，而是**把有界点读换成无界走查**：它换来的是 5–6×→2.2×，代价是 S2 的判重面从 O(1) 退回 O(历史)。

**故：三案无一可直接落地。** 选项2／3 纯度不过（第二份表示）；选项4 破 §十七·二 冻结的「点读与历史深度无关」；S1 破成本。**主树保持 S1 原状（共存、绿、未接线）**，不落地任何一案。

### 二十·七 建议形状（须设计门，本节不实现）

**读数指向的唯一自洽组合**：三件事各归其位，没有一个载体兼任两职。

| 面 | 载体 | 依据（实测） |
|---|---|---|
| 唯一真相 | `journal.log` | 不变 |
| 水位＋提交序＋全序枚举 | `order.log`（选项4 的**枚举半**） | 枚举 25.9ms 不劣于目录 28.7ms；撕裂线由 root 水位截断，白得完整性规则 |
| 点地址 | **容量有界槽表**（一个文件，就地写定长记录；不用每键文件） | 就地槽写 5×40B **0.007ms**（每键文件 5 键 23–49ms） |
| 耐久 | **按载体字节预算屏障**（界＝一个 `journalOrderChunkBytes`），不用每提交一道 | order 屏障 4.2ms；去掉后 **4.803/4.872ms ≈ 1.15×** |

推算（**推算，非实测**）：提交 ≈ 今日 1 道屏障 ＋ 近零就地写 **≈1.0–1.2×**；开档读入字节 ≤ root ＋ 一个 chunk（界可失败，控制案已备）；点读 1 次小读（常数）；枚举 O(历史)（承 R50，不得称有界）；槽表丢写 ⇒ 表水位落后 ⇒ 从 order 重述该段（**不得静默答 absent**，这是槽表形态的唯一承重规则）。

**为何须设计门而非本单自裁**：它推翻 §十七·二 冻结的载体表（`idx/<ns>/<hex(key)>` 与"目录即持有键集"），并新增两条承重规则（槽表水位、预算屏障），按 R17／§十七·七 属共享面，非实施单可裁。

### 二十·八 未证／未量（如实）

- **生产接线未启用，且不在本单写入面**：`statesAddressMaintenance` 今日**零生产调用者**（grep 实证，仅 `_test.go` 四条与 `journal_kv.go` 的声明转发）；真正的接线点会落在 `atomic_store.go`／`segment_store.go`，**二者均不在 R67 白名单**（后者是 S2 的面）。故 `./assembly/` 与 `./tests/...` **未跑**（无接线即无消费面变化），2.2× 是否令四枚装配锚转绿**未证**。
- **掉电未测**：只测进程死亡（页缓存存续）。预算屏障的「界＝一个 chunk」在掉电态成立的前提是「k 行完整 ＋ root 水位」，该前提未实跑。
- **槽表形态零原型**：0.007ms 是原语读数（`WriteAt` 到已预分配文件），不是槽表引擎原型；容量扩容、探测链、删除后簇修复、表水位重述**均未实现未测**。
- **绝对毫秒不可外推**：同期有并发单在跑（`runtime/state`、`scripts/gates`），一次 fsync 5ms、一次文件创建 4–10ms 均高于 §十八·三 的 1.4ms/键；**只有增长轴与控制案的对比是判据**。
- **选项3 的物化后点读未采样**（物化前后两态分列需再一轮），其代价已由 15.6s 突发定性。
- **本次未改任何 anchor**，也未启用 `--update-baseline`；**G2/G4 未由本节判定，反转未完成，产品不可发布。**

## 二十一、S1c 载体形态冻结：容量有界槽表＋按载体字节预算屏障（2026-11-16）

**性质**：设计冻结＋原型实测＋**落地（绿、未接线）**。本节把 §二十·七 的建议形状做成一个形状、量它、冻它。主树写面＝`memory/ledger/journal.go`（载体引擎）、`memory/ledger/config.go`（收回 `rejectLegacyShardLayout` 对自有 `idx/` 的唯一例外）、`memory/ledger/journal_test.go`（两枚锚随载体改名、崩溃锚夹具放宽、新增三枚锚）；`atomic_store.go`／`segment_store.go` **未触**，生产接线**未落**（·十）。§一–§二十 一字未改（历史证据只附勘误不改写；§二十·八 的三条未证项由本节逐条兑现或改判，不改写原文）。

### 二十一·〇 隔离树、源身份与计量纪律

- **构造法**：`rsync -a` 主树**工作树**（非 HEAD）至 `/tmp/recovery-s1c/tree/`，排除 `.git`、`examples/`、`docs/.dev`、`.tmp`、`node_modules`。`memory/ledger/*.go` 与主树逐字节相同，sha256 落 `/tmp/recovery-s1c/ledger-source-sha256.txt`：`journal.go` `601fcc49…`、`config.go` `126c6e86…`、`journal_test.go` `688c536b…`、`segment_store.go` `9a4ae9f7…`、`atomic_store.go` `e072765e…`——**与 §十八／§二十 记载逐字节同**，故 §二十 的基线读数可直接对照。
- **基线实跑**：未改动的隔离树 `go vet ./memory/ledger/` **0**；`go test ./memory/ledger/ -count=1` **0（35.662s）**，**73 顶层＋53 子**（§二十 记 37 子：本轮的计数按 `=== RUN` 含 `/` 的行取，口径不同，如实分列）。
- **计量纪律（承 §十七·〇／§十九·〇／§二十·〇）**：引擎开档 `openJournalLog` 在地址层计时区间**外**（读数是 `TestZZOpenAddressLayer`，直接用 `openJournalAddress` 计时）；提交取 200 次样本的 median／mean／最慢三列（预算把屏障摊到多次提交，均值才是它的成本，最慢是表翻倍）；点读９样本中位数；每深度先以同形状预热。**绝对毫秒含并发负载噪声**，判据是增长轴与控制案。
- **测量层永不落地**：`memory/ledger/zz_slot_cost_test.go`（7 枚 `TestZZ*` 探针）留在隔离树，**未进主树**；落地的是不带任何形状开关的单一形态。

### 二十一·一 冻结载体表：两件，不是三件

| 载体 | 形状 | 地位 | 依据 |
|---|---|---|---|
| `journal.log` | 名字与帧格式不变（8B 头＋`journalRecord`，一帧一提交） | **唯一真相** | 未动 |
| `order.log` | append-only，一行一提交语句，内联每个写键的值 span 与 `valuesEnd` | 派生；水位／提交序／全序枚举 | §二十·七 的**枚举半**（25.9ms 不劣于目录 28.7ms，撕裂线由水位截断白得完整性） |
| `slots` | 一个文件：64B 表头 ＋ N 个定长记录，就地写 | 派生；点地址 ＋ 持有键集 ＋ 命名空间 | §二十·七 的**点地址**（就地小写 0.007ms/5 键） |

**删除的载体**：`idx/<ns>/<hex(key)>`（一键一文件，APFS 1.4ms/键 ⇒ 6.1×）与 `root.json`（水位并入 `slots` 表头）。

**为何 root.json 并入表头（纯度论证，非省事）**：表头必须陈述「本表读到哪个提交」才能知道落后多少；表头里已有 `seq`／`valuesSize`／`orderSize` 三个水位字段，而 `root.json` 陈述的正是同三个数。两份同数即一份冗余表示；**一份**即可，因为表的可修复性不依赖 `root.json`：表撕裂后整表重述要读完整 order，留 `root.json` 也省不下这一步。故载体三件降为两件，水位只陈述一次。

**收回 `config.go` 的例外**：`rejectLegacyShardLayout` 原有一行「`idx/` 是本构建自己的载体，读过去而不是拒绝」。`idx/` 消失后该例外失去对象，**删除**（`if entry.IsDir()` 分支恢复无条件拒绝）。这是零残留的执行项，不是兼容分支。

### 二十一·二 记录与表头布局

**表头（64B，偏移 0）**：`magic(4) | version(4) | capacity(8) | stride(8) | seq(8) | valuesSize(8) | orderSize(8) | crc32c(4) | 补齐`。末尾 crc 覆盖前 48B，故撕裂表头读作**没有表头**（⇒ 从链重述），不读作一个没人写过的水位。

**槽记录（stride=96B）**：`crc32c(4) | state(1) | keyLen(2) | offset(8) | length(4) | null(1) | key(stride-20)`。

- **键整名内联，不用摘要**：记录若只存键的摘要，两个键的摘要相同即令一键答另一键的值——派生载体一旦答错就是第二真相。96B 步长给键留 76B；`blob:`＋事件 id、`{pid}:evt:{window}:{seq}`、`store:seat` 都在 76B 内。
- **键长超界即加宽步长**（·四），不是拒绝提交，也不是截断键名。
- **三态**：`empty`（无人取用，探测到此为止）／`held`（键由本次提交持有，记录它值的 span）／`absent`（键被删除，**保留键名**，使它所处的探测链仍可走完，并答不存在）。

### 二十一·三 键→槽映射：实测推翻了 crc32c

`slot = mix(key) mod capacity`，`mix` ＝ FNV-1a 64 ＋ splitmix64 终结（乘法—异或移位）。地址只由键决定，不含进程种子，故一个进程写的表是另一个进程读的表。

**实证（这条是原型首版判红逼出来的）**：首版用 `crc32c(key)`（复用包内已有的表）。本引擎的键是**同族**键——`0:evt:%06d:%d`，只在尾随计数器上变——而 CRC 是 GF(2) 线性函数，同族键的输出互为**常差**，于是同一提交的 5 键落在相邻槽、相邻提交整体平移，链迅速聚成一簇。实测：**第 4534 次提交处具名拒绝** `slots holds no free slot within 64 of "0:evt:004534:1"`（当时容量 32,768、占用 69%）。换成混合哈希后，同规模最长链 **54→12**。锚 `TestAddressableIndexKeepsItsAddressIndependentOfTheKeyFamily` 钉住这条：20,000 同族键在 65,536 槽下最长链必须远小于探测界。

### 二十一·四 冲突、负载与两条生长轴

- **冲突**：线性探测。探测到 `empty` 即为该键的位置（这就是「无偏移表」）；探测到同键即就地替换；探测到 `absent` 且键名不同则继续。
- **负载界 50%**：占用过半个容量即翻倍。**这是实测校正的**——首版取 70%，32,000 键在 32,768 槽下最长链实测 **54**，逼近当时 64 的探测界（表还没翻倍就够不着了）。50% 下同一规模的最终容量不变（都是 65,536），链长减半。
- **探测界 128**：65,536 槽下实测最长链 **12**，余量十倍。越过即报具名损坏——链那么长只可能是一张没人写过的表，不是一张满了的表。
- **生长轴一：容量 ×2**。实测 5 键/提交、起点 1,024 槽，**第 144 次提交**触发。
- **生长轴二：步长 ×2**。键长超出现有记录的余量时触发；实测一次 `blob:`＋96 字节键 ⇒ 步长 96→192，该提交 **18.067ms**（整表重述 1,024 槽＋一次 fsync）。
- **生长的形状**：写 `slots.stage`（新表头＝**已耐久水位**，非当前水位）→ 逐条重放旧表记录 → fsync → `rename` 覆盖 → 重开 handle。停在任一步都留旧表。**表头写耐久水位而非当前水位**：当前水位含尚未 fsync 的帧，若崩在 rename 之后，表头就会声称日志并不持有的字节 ⇒ 具名拒绝一次假损坏。
- **声明（这是本节的成本偏离项）**：**这两类提交不是 O(触碰键)**，而是 O(持有键)。全生命周期共 O(log) 次（容量倍增 + log₂(最长键长/76)），故摊还常数；但**单次提交可以到 18ms**。不承认这一点就是假报。

### 二十一·五 写序、水位与一条重推规则

**一次提交的写序**（`journalLog.append` → `sync`）：

1. 追加帧到 `journal.log`
2. 追加一行 order 语句
3. 就地写本提交触碰的每个槽记录
4. 〔预算到〕`fsync journal.log` → `fsync order.log` → `fsync slots` → **写表头（推进水位）**

提交在这条链返回成功；`CommitFact` 的五值不变（⑤前＝`FactUncertain`，其后＝`FactCommitted`）。

**为何表头在最后**：表头是「派生载体读到哪儿」的唯一陈述。它只在**两边都 fsync 之后**推进，故**表永不超前于水位的耐久性**；表落后只需**前向重放**（order 语句是幂等的纯函数：同一语句重放两次得到同一记录），永不需要撤销。原地覆盖式映射一旦超前就无法撤销（旧值已被覆盖），这是本设计把表头放到最后、且把 order 语句放在记录之前的唯一理由。

**重推规则（一句）**：表头＝唯一水位。表不可读／某记录解不出／order 短于表头水位 ⇒ **从链整份重述**（一次性 O(历史)）；表头之后还有提交（`valuesSize < log.size`）或表落后于水位 ⇒ **从 order 前向重述该窗口**（有界＝预算窗口，`log.append` 的既有 `derive`）。

### 二十一·六 屏障预算规则与全有或全无

**规则**：派生载体每 **staged ≥ `journalIndexSyncBytes`（64 KiB）** 取一道屏障（order 语句字节 ＋ 每条记录 stride）。一次提交**只为日志**取屏障。实测定标：**一道载体屏障覆盖 65 次五键提交**，载体字节/提交＝**1,008**（order 行＋5×96B）。

**预算不削弱耐久性的证明**（三条，逐条对应一个失效面）：

1. **事实的耐久与派生载体无关**。一帧＝一次 `Write`＋CRC32C＋长度头，`journal.log` 的 fsync 返回即「这一帧的全部 puts 与 deletes」耐久；`FactCommitted` 仍只在它之后报。派生载体落后只是「索引尚未定位该提交」，开档前向重述把它补上——**它是加速器，不是承重件**。
2. **多键提交的全有或全无由帧给出，不由派生载体给出**。撕裂帧按水位截断 ⇒ 落 5/5 或 0/5。派生记录是**逐键**写的，半途停会留下 3 条记录、2 条不落；但表头（水位）没推进，开档前向重述**幂等地**把 5 条全部重写一遍 ⇒ 收敛到全有，绝不收敛到「3 of 5」。崩溃锚的每个注入态都断言这一点（`held != 0 && held != len(keys)` 即红）。
3. **预算界同时就是开档重述的上界**。最坏停在两次载体屏障之间 ⇒ 窗口内载体字节 ≤ 64 KiB ⇒ 开档的增量读有界。这就是「界＝一个 chunk」的全部含义。

### 二十一·七 成本实测（400／1,600／6,400 深度，同机同夹具）

**提交（每提交 5 键、64B 正文、200 次样本）**

| 深度 | 今日（无索引）median／mean／最慢 | 槽表 median／mean／最慢 | 倍率（median／mean） |
|---|---|---|---|
| 400 | 4.887／4.870／9.1ms | 4.808／4.860／26.4ms | **0.98×／1.00×** |
| 1,600 | 4.167／4.522／9.9ms | 4.558／4.796／46.6ms | **1.09×／1.06×** |
| 6,400 | 4.058／4.218／5.9ms | 4.123／4.907／87.0ms | **1.02×／1.16×** |

- **中位数即稳态提交成本：1.02–1.09×**（§二十·七 推算 1.0–1.2×，兑现）。**6,400 的均值 1.16× 与最慢 87.0ms** 是**表容量翻倍**落在那 200 次采样窗口内（32,000 键正好越过 65,536 的 50% 界）——如实归因，不是常态。
- 对照：S1 同夹具 **25.1ms（6.1×）**，四案下沿（选项2/3/4）**9.0–13.4ms（2.2–3.2×）**。

**点读（中位数，ms）**

| 深度 | newest | mid | oldest | **absent（控制）** | 控制：order 走查（absent） |
|---|---|---|---|---|---|
| 400 | 0.0024 | 0.0019 | 0.0022 | **0.0045** | 0.735（walked 2,000） |
| 1,600 | 0.0018 | 0.0039 | 0.0048 | **0.0045** | 3.490（8,000） |
| 6,400 | 0.0018 | 0.0070 | 0.0071 | **0.0060** | 13.577（32,000） |

- 四个位置在 16× 深度上都落在 0.002–0.007ms：**与历史深度无关**（§十七·二 冻结的要求）。
- **absent 与 present 同阶**（0.0060 vs 0.0071）⇒ 本形状的 absent 不是「走完整份 order 的伪装」；它与同深度的 present 同成本，因为两者都是一次记录读＋判空。**可失败控制**：同一键改走 order 逆向走查，0.011→0.735→13.577ms（16× 深度＝1,235×／absent 轴 18×）⇒ 测量装置能看见增长，故上面的平坦不是恒真界。

**开档（地址层单独计时）**

| 深度 | 地址层 open | 读入字节 | 控制（删表头） |
|---|---|---|---|
| 400 | 1.721ms | **64B** | 46.3ms／260,000B |
| 1,600 | 1.163ms | **64B** | 107.6ms／1,040,000B |
| 6,400 | 2.875ms | **64B** | 253.1ms／4,160,000B |

- **读入字节 64B 恒定**（16× 深度）；控制案同轴 **260,000→4,160,000B（16×，线性）** ⇒ 界可失败。
- 墙钟 1.2–2.9ms 无增长轴（噪声区间内，承 §十七·一 的噪声纪律）。**注**：把 `openJournalLog` 计入会得到 3.98→26.69ms——那是**引擎自己**的 O(历史) 重放（§十九 的面），不计入本节读数（§二十·〇 的区间纪律）。

**枚举（不宣称有界，承 R50）**

| 深度 | `keys("0:evt:")` | `namespaces()` | 全序 newest-first |
|---|---|---|---|
| 400 | 0.65ms（2,000 键） | 0.262ms | 1.95ms |
| 1,600 | 2.61ms（8,000 键） | 0.747ms | 5.00ms |
| 6,400 | 6.18ms（32,000 键） | 1.757ms | 14.12ms |

四列全随存活键集／历史增长。`keys` 与 `namespaces` 是**读整张表**（32,000 键时 65,536 槽×96B＝6MB 顺序读），比 S1 的目录扫描（28.7ms）快，但**仍是 O(存活键)**，**MUST NOT 写有界**。

### 二十一·八 崩溃一致性：槽表要什么规则，与另两个载体各不相同

**新写序下的可区分态**（进程死亡模型）：①前帧 ②帧在日志 ③order 语句在 ④槽记录在（表头未推进）⑤表头推进。②与④的 fsync 不改内容，故①③与②④字节全等——**五注入点落四态**（§十七·四 的六落五同理）。崩溃锚逐态断言「全有或全无」＋水位收敛，**五个子测试全绿**。

**撕裂槽记录（新增注入）**：把一条记录的中段翻一字节。规则与两个同类载体**各不相同，这是实测的分野**：

| 载体 | 完整性靠什么 | 修复是什么 |
|---|---|---|
| `order.log` | **自带行程**（每条语句一行）＋水位截断；撕裂线由表头水位的 `OrderSize` 截掉 ⇒ **白得一条规则** | 截到水位 |
| `keys.log`（选项2） | **没有独立水位可依**（它的水位就是自己最后一行的 seq）⇒ 必须自写「一行完整当且仅当其后跟换行」 | 补规则后丢弃尾线 |
| **`slots`** | **既无行结构也无截断点**：记录是**原地覆盖**的，水位只陈述「读到哪个提交」，不陈述「哪条记录是整条」⇒ 只能**记录级 crc32c** | **整表从 order 重述**——撕裂记录的**键不可知**，无法只修这一槽 |

锚 `TestAddressableIndexStatesTheTableAgainBesideARecordItCannotPlace`：点读该键仍答原字节、列举仍答 20 键、表被整表重述（字节不再相同、长度不变）。**修复落在读到它的那一刻**，不是开档扫描整表——扫全表会毁掉 64B 的开档界；视图持写权时重述，不持写权时具名报错（无处可修）。

**孤儿槽记录（新增注入）**：手写一条**crc 合格**的记录，其 span 越过水位（即「帧没落地的那次提交」）。规则＝`stated(span)` **水位守卫**：`offset+length ≤ 本视图陈述的 valuesSize`。锚 `TestAddressableIndexDiscardsARecordItsFrameNeverCarried` 断言点读答不存在、列举不命名它、命名空间不多出它、举数列恰为 20。**守卫在三个读点各判一次**（点读／列举／命名空间），因为它们各自独立读表；这不是冗余防御，是同一条规则在三个入口的同一判定。

**掉电未测**（与 §十七·四／§二十 同口径）：进程死亡模型下页缓存存续。预算屏障的「界＝一个 chunk」在掉电态成立的前提是「表头所陈述的字节真的落盘」，即第 4 步的两个 fsync 与表头写之间无重排；**未实跑**。

### 二十一·九 S1 的八枚具名变异按新形状复跑（全数转红，复原后全绿）

| 锚 | 变异（改实现） | 红原文 |
|---|---|---|
| `…OpenReadsTheHeaderAndTheTail` | 开档恒重建 | `the clean open read [6514 25864 103264] bytes … want the header alone at every depth` |
| `…PointReadAnswersTheValueAtItsAddress` | 点读忽略记录陈述的 span | `read the key its address locates: held=false err=… invalid character '\x00' looking for beginning of value` |
| `…StatesOneCommitOrNoneAcrossAStop` | 一提交只写首个键的记录 | `1 of 5 key(s) landed after the stop, want the commit whole or absent` |
| `…DiscardsAStatementItsFrameNeverCarried` | 信任 order 尾（不截到水位） | `the order file states &{order.log 1219 …} byte(s), want the 1134 before the orphan statement` |
| `…RefusesALogThatLostTheBytesItsHeaderStates` | 水位越界改为重建 | `the open of the cut log = <nil>, want the named corruption` |
| `…EnumeratesFromTheOrderFileAlone` | 枚举顺手读值 | `the walk read 2 value(s), want none: the order file states the spans` |
| `…LocatesTheLogARewriteReplaced` | 重写后不重建载体 | `the kept key answered "" (held=false …), want its bytes after the rewrite` |
| `…IsKeptOnlyByTheViewThatDeclaresIt` | 声明点恒 no-op | `the view that declared the index kept none` |

- **两处名称随载体改**：`…TheRootAndTheTail` → `…TheHeaderAndTheTail`、`…LostTheBytesItsRootStates` → `…LostTheBytesItsHeaderStates`（被陈述的水位载体从 `root.json` 换成表头，锚名跟着对象走，断言未改）。
- **一处夹具放宽、断言一字未改**：崩溃锚原本把「键条目已落」写成「`idx/` 前缀下新增文件」。非文件载体表达不了这件事，改为 `addressableTestStale`——**取已落的表、把表头切回前态**（恰好就是「记录到了、水位没到」这个注入态）。新增的 `stopped_after_the_slot_records` 子测试与 `stopped_after_the_watermark` 因此是两个不同的字节态，断言面与 S1 逐字相同。
- **M4 的红点位移（如实）**：S1 时该变异先触发「走查命名了孤儿键」，本形状先触发「order 文件未被截回水位」。原因是枚举**已经**被 `a.orderSize` 上界约束，走不到水位之后的语句；故同一变异的第一个红点是另一条同义断言。这是形状带来的位移，不是断言变弱。

### 二十一·十 接线判定：四枚装配锚在成本面已通过，主树仍未接

在隔离树把 writer 面接上（`config.go` 的 `openJournalKV` 之后对 `cfg.WriteRights` 调 `statesAddressMaintenance(true)`，**一处**），实跑：

| 测试 | S1（接线开） | **S1c（接线开，同机）** |
|---|---|---|
| `TestCheckpointCarriesOpenObligationAcrossFold` | FAIL 18.89s | **PASS 1.61s** |
| `TestRestartRecoveryPinsBeforeTrim` | FAIL 120.49s（`commit filler 1359: context deadline exceeded`） | **PASS 13.47s** |
| `TestSymptomAnchorDeliveryOutsideWindowSurvivesRestart` | FAIL 120.22s | **PASS 14.08s** |
| `TestDeliveryOwedTrimNegative` | FAIL 90.47s | **PASS 14.27s** |

- **全量 `./assembly/`（接线开）246.0s，唯一红 `TestGovernanceSliceWiredInProduction`**；同树同命令**接线关仍红**（`governance_checkpoint_test.go:147: governance state of the first cut = version 1 tail 2 over 0 records`）⇒ **归属并发叶，不是本载体**（与 R67 的归属一致）。
- **`./tests/...`（接线开）**：唯一红 `TestRecallPromptNamesMatchTheToolSurface`，原文 `open ../../../examples/wechat-bot/resources/prompts/recall_agent.md: no such file or directory` ⇒ **隔离树构造产物**（rsync 排除 `examples/`，且该目录含真实用户数据根，按令不得读取、也不得复制）。
- **主树未落接线**：白名单把接线点指向 `atomic_store.go`／`segment_store.go` 并要求「须先上报」。本轮把接线缩到 `config.go` 一处并实测全绿，但**接生产提交面是共享面决定**（R17／R22），且代价是真金白银的 `fsync` 语义变化。**故只报不落**：所需即那一行，证据即上表，**待编排者裁**。

### 二十一·十一 未证／未做（如实）

- **掉电未测**（仅进程死亡）。预算屏障「界＝一个 chunk」在掉电态依赖第 4 步两处 fsync 与表头写不被重排。
- **接线未落主树**，故 `./assembly/`／`./tests/...` **未在主树跑**（无接线即无消费面变化）。
- **读路径未切换**：`journalLog.read`／`keys`／`namespaces`／`eachKeyNewestFirst`／`journalKV.Read` **一字未改**，生产读仍走 `l.index`。载体与引擎只做到「共存、绿、成本达标」，切读属 S2。
- **`namespaces()` 语义有位移**：表的扫描只答**持有键**的命名空间，今日 `journalLog.namespaces()` 答的是**曾写过的**命名空间（`namespaceSeen` 不删）。一个键全被删除的命名空间在新形状下消失；因该命名空间对任何前缀都答不出键，列举结果相同，但**断言命名空间列表本身**的锚会看到差别。切换到该读点时必须先定谁对。**登记，未裁。**
- **容量／步长生长的单次提交非 O(触碰键)**（实测 18.067ms 一次），已声明为摊还常数。
- **`journalSlotStride=96` 的键上界 76B 未由契约钉住**：超界即加宽步长（不是拒绝），故无功能上界，但**步长加宽次数**随最长键长走，O(log)。
- **G2/G4 未由本节判定，反转未完成，产品不可发布。**

## 二十二、06.R4 执行记录：座位落地、接线启用、N6 日志行、namespaces 消费者枚举（2026-11-16）

**性质**：实施＋实测。主树写面＝`memory/ledger/segment_store.go`（座位、窗口键按需读、常驻窗口表删除、Load 的计数来源）、`memory/ledger/config.go`（接线一处＋字段契约注释）、`memory/ledger/segment_store_test.go`（三锚新增＋计数探针）、`memory/ledger/cold_archive_test.go`／`fact_commit_batch_test.go`（各一行：提交键集增加 `store:seat`，断言面不缩）、`assembly/checkpoint.go`（N6 日志行）。`journal.go`／`journal_kv.go`／`atomic_store.go` **未触**。§一–§二十一 一字未改。

### 二十二·一 落地形状（逐条对 §十九 冻结形状）

| 交付 | 落地 |
|---|---|
| `count`/`commitSeq` 链内座位 | 键 `store:seat`，值 `{"count":N,"commitSeq":M}`；`planEventLocked` 把座位与自己那一族放进**同一个 Batch**（`{pid}:evt:`／`{pid}:idx:`／`{pid}:meta:`／`store:seat`），故一帧一提交、零新增耐久点 |
| 开档一次点读 | `Load` 首动作 `readSeatLocked`：`kv.Read(Fetch{Key: storeSeatKey})` 一次单键读＋解码；计数与提交序直接取座位两标量 |
| 座位不可用⇒统一重推 | 缺失／解不出／`count<0`／`commitSeq<count` 四态一律落到今日 `Load` 的算式：`count=len(published)+retired`、`commitSeq=max(count, max published commit)` |
| 重述仅在持写权 | **开档不写任何键**：重述由下一次事件提交的那一帧承担（只有持写权的视图能提交；读视图永不写链）。故既守「读视图不写」，也不新增耐久点 |
| `windows` 不新增载体 | 常驻 `s.windows` **删除**；`nextWindowMeta` 按 `{pid}:meta:{window}` 链键**点读一次/提交**，解不出即以 `ErrSnapshotCorrupt` 具名（该具名判定从 `Load` 移到点读处，未丢） |
| 常驻态 | `s.index`／`s.order` **保留**（理由见·三）；`s.count`／`s.commitSeq` 两标量由座位填入 |

**座位身份的纯度核对**：`store:seat` 的命名空间是 `store`，非整数 ⇒ `Load` 的分区发现与任何 `Namespaces` 消费者都不把它当分区；`keepKeysLocked` 的四族判定（idx／evt／meta／原文）都不认它 ⇒ 退位趟**永不把座位搬进冷档**，它始终随热链。差分：退位趟只改 `s.retired`／`s.coldCommit` 与两张常驻表的删减，**不改 `s.count`／`s.commitSeq`**；`count = published + retired` 在搬迁前后守恒，故座位在搬迁后仍与重推算式同值。

### 二十二·二 实测：座位的面是常数，store 开档仍随历史增长（如实）

**夹具**（`/tmp/recovery-06r4/probes/zz_06r4_cost_test.go`，经 `-overlay` 注入，未进主树；引擎开档在计时区间外，9 次重复取 min／med）：每事件 64B 正文，`commitSegmentEvents` 每窗口 4 事件。

| 深度 | store 开档 min／med（本叶） | 座位单键读次数 | 开档读到的条目数 | 对照：无座位无接线的同形开档 min／med |
|---|---|---|---|---|
| 400 | 845µs／1.416ms | **1** | 500 | 656µs／748µs |
| 1,600 | 2.984ms／3.858ms | **1** | 2,000 | 2.958ms／3.114ms |
| 6,400 | 10.69ms／11.382ms | **1** | 8,000 | 11.751ms／12.168ms |

- **座位自身的面是常数**：三个深度都恰 **1 次单键读**，与历史无关（锚 `TestStoreSeatIsReadWithOnePointReadAtOpen` 钉住，变异「读两次」即红）。
- **⚠️ store 开档仍 O(历史)**：845µs→10.69ms（16× 深度＝12.6×），与对照同量级。**本叶把 `count`/`commitSeq` 从聚合搬到座位，但开档的条目读没有减少**，因为 `s.index` 仍由 `{pid}:idx:` 前缀读物化（见·三）。**§十九·三 的「storeLoad 310.8→185.0→168.0µs 平」未在主树复现**，且不得声称复现。
- 窗口 meta 由「读每窗口值」改为「只枚举窗口键」（同一判定，零值读）：条目数 500／2000／8000 中窗口键占 100／400／1600，与本改动前同数量（判定需要知道哪些窗口有 meta），省下的是每窗口一次值读；实测该项在总时长上落在噪声内。

### 二十二·三 为何 `s.index`／`s.order` 未在本叶删除：三条硬约束（均有实证）

§十九「常驻态」把三张表全删。本次只删 `s.windows`，`s.index`／`s.order` 保留，理由三条，**任一条都需编排者裁定才能跨**：

1. **按身份点读缺分区**。`GetEvent`／`eventByKey`／`planEventLocked` 的入参是 `event.EventID`，而持久 idx 键是 `{partition}:idx:{eventKey}`，**分区只存在于常驻索引里**（`PartitionID` 来自 `PartitionIDFromName(agentID)`，与事件身份无关）。S0 原型从未做身份读（它的 `rederiveSeat` 与 `pointRead` 都按**全键**点读），故冻结形状未覆盖这一面。要删 `s.index` 必须新定「身份→分区」的载体（例如把分区移进 idx 值、键改为 `idx:{eventKey}`），那是**持久布局决定**，不属本叶。

2. **`IndexEntries()` 的消费者在装配测试里，且重表达归 09**。`assembly/symptom_anchor_recovery_test.go` 的 S2 量面读 `IndexEntries()`（`indexEntriesOf`）并有「零即失败」守卫；删 `s.index` 使其恒 0 ⇒ 该锚转红，而该文件按派单**不在本叶写集**，且 §十七·六 已把 S2 改锚归 09 域（S5）。

3. **引擎开档仍 O(历史)，面在 `journal.go`**。`openJournalLog` 每次开档重放帧（无切面时整份），§二十一·七 已声明该项「不计入地址层读数」；把开档变「只读表头＋尾」要切 `journalLog.read/keys/namespaces/eachKeyNewestFirst` 到地址索引（读路径切换），`journal.go`／`journal_kv.go` **不在本叶写集**。故即便 store 面清零，boot 也不由本叶变平。

**结论**：本叶交付的是 S2 的**载体与窗口去物化**，不是 §十七·三 的全部去物化；`06.R4` 的框**未勾**，因为冻结验收里的「storeLoad 平坦」未兑现。

### 二十二·四 提交成本（同机 A/B，三形状）

**夹具**：`CommitFact`（事件族＋原文族＝5 键，正文 64B／原文 64B），先填到深度再取 200 次样本。三形状同机同夹具：**A＝无座位无接线**（`-overlay` 反接线并去座位 put）、**B＝只接线无座位**、**C＝本叶现状**。

| 深度 | A median（三次） | B median | C median（三次） | C／A |
|---|---|---|---|---|
| 400 | 4.056／4.956／4.128 | 4.922 | 4.951／4.999／4.871 | 1.10–1.22× |
| 1,600 | 4.082／4.711／4.139 | 4.853 | 4.903／13.026（负载毛刺）／4.916 | 1.10–1.20× |
| 6,400 | 4.456／4.757／4.466 | 4.927 | 4.894／4.917／4.217 | 0.94–1.10× |

- **座位自己的提交代价≈0**：B（4.922／4.853／4.927）与 C（4.87–5.00／4.85–4.92／4.22–4.93）在噪声内同值 ⇒「座位增加零个耐久点」有实测支撑。
- **接线的那一份是载体写**：A→B 的差（≈1.1×）由 order 语句＋6 条槽记录承担，与 §二十一·七 的 1.02–1.09× 同量级。
- 对派单给的基线 4.144ms 之比：**≈1.18×**（实测中位数 ~4.9ms）。本机三次运行最慢一次提交 115–136ms（并发负载），故只报中位数，绝对值不作跨机比较。

### 二十二·五 冻结锚与变异（三锚新建，四条变异各自转红）

| 锚（memory/ledger） | 断言面 |
|---|---|
| `TestStoreSeatIsReadWithOnePointReadAtOpen` | 开档对座位恰 **1 次单键读**（三深度不变）；store 答的数＝座位说的数（座位说 7 而链只提交 3 ⇒ 答 7）；控制一：开档读到的条目数随深度增长（该界可失败）；控制二：无座位的链答其条目数（5） |
| `TestACorruptStoreSeatRederivesFromTheChain` | 坏座位（`{"count":`）⇒ count＝4（**不是 0**）、四事件按身份全读到、**读视图不写链**（座位字节仍是坏值）、下一次提交的那一帧重述座位、新开档答 5 |
| `TestASeatBelowItsCountRederivesTheRank` | 座位 `{count:9,commitSeq:2}` ⇒ 答 4（重推），且下一提交取的秩 > 已发布的最高秩（信任坏座位会与其撞秩） |

**变异（`-overlay`，四次各自红）**：①座位恒不可用 ⇒ `answered 3, want the seat to be the count`；②去掉 `commitSeq<count` 判定 ⇒ `answered 9 event(s), want the 4`；③坏座位答零 ⇒ `answered 0 event(s), want the 4`；④座位读两次 ⇒ `read the seat 2 time(s), want the one read`。

### 二十二·六 N6 日志行（逐字）与为何它不是库存计数

```go
assemblyInfo("tagent: boot seated the task account tasks=%d tail=%s reads=%d keys=%d",
	len(durable.TaskIDs()), cut, len(scans), keys)
```
`cut` 是 `"none"` 或尾的键数；`reads`／`keys` 是 `LoadStats().Scans` 的条目数与键数之和。它陈述的是**这次入席按身份读回了什么**（账户任务数、尾声明、读次数、读到键数），不是「库里有几族／几键」的库存盘点。`tail=none` 明确区分「整族回退读」与「按身份读」两态，使 13.R2 重写 attestation 时不至于把回退读当托管读。

### 二十二·七 `namespaces()` 消费者枚举与裁定

**枚举（grep 全仓，`Namespaces(` 与 `namespaces()` 两形）**：引擎实现三处（`journalKV`／`memKV`／`rustVikingKV`）；生产消费者五处——`FileSegmentStore.Load` 的分区发现、`coldEventLocked`、`coldIndexLocked`、`assembly` 的 G3 成本探针（`g3PrefixPairsRead`，只取整数命名空间）、`migrate_test`（比对迁移副本的命名空间）；`scripts/recover-kv` **不读** `Namespaces`（它用 `Keys`）。`a.namespaces()`（地址索引，只答持有键）**当前只有 `journal_test.go` 两处测试调用**。

**裁定依据**：全部消费者只用命名空间做**分区发现**（一个键都不持有的分区没有任何条目可读）或**迁移副本比对**（比的是持有键的命名空间）。**无一处依赖「曾写过」语义**；`memKV`／`rustVikingKV` 本来就答持有键，故改为持有键是让三个实现同一规则。

**未落地（须裁）**：改动点是 `journalLog.namespaces()`（`namespaceSeen` 需换为持有计数，`journal.go`）与切换点 `journalKV.Namespaces`（`journal_kv.go`），**两者都不在本叶写集**。本叶只交枚举与裁定，不动语义。

### 二十二·八 未做／未证（如实）

- `s.index`／`s.order` 未删（·三三条约束）；`06.R4` 框未勾。
- 引擎读路径未切（`journal.go` 不在写集）；引擎开档仍 O(历史)。
- `namespaces()` 语义改动未落地（写集外）。
- 掉电未测（只测进程死亡模型）。
- `assembly/symptom_anchor_recovery_test.go` 的 `g3PrefixPairsRead` 仍按「idx＋meta 前缀对」计量 `Load` 的读面，而 `Load` 已不再读 meta **值**；该探针口径的精度属 09 域（S5/S2 改锚），本叶不改。

## 二十三、06.R4b 收尾记录：`namespaces()` 落地、提交成本与耐久点、增长控制案、症状锚成本上升归因（2026-11-16）

**性质**：补 §二十二 未做的交付④与三项验证。写面＝`memory/ledger/journal.go`（`namespaces()` 一处）、`memory/ledger/journal_test.go`（一锚）、`memory/ledger/segment_store_test.go`（两锚各加一控制＋一个 helper）、本域 `design.md`／`tasks.md`。**`assembly/`、`config.go`、`segment_store.go` 生产码零写**。

### 二十三·一 交付④：`namespaces()` 改为只答持有键（消费者枚举在先）

**实现**：`journalLog.namespaces()` 由 `namespaceSeen`（「曾写过」集）改为按 `l.index` 的**持有键**（`value.deleted` 跳过）当场求集合并排序；`namespaceSeen`／`namespaceOrder`／`noteNamespace` **三处结构删除**（含 `seedIndexCut`／`indexRecord` 两处调用）。**不保留排序缓存**：tombstone 令集合可**缩小**，而缓存只在「新增命名空间」时失效，保缓存即需在删除路径再失效；且无调用方在同一次写入序列里重复枚举（`Load` 一次、g3 探针每代一次）。

**消费者枚举（grep `.Namespaces(` 与 `namespaces()` 两形，全仓）**：

| 消费者 | 用法 | 语义改动的影响 |
|---|---|---|
| `segment_store.go:365`（`Load`） | 取整数命名空间做分区发现，再读 `{pid}:idx:`／`{pid}:meta:` 前缀 | 已退位分区不再列出，而它热链上已无 idx／meta 键可读（读也答 0 对）⇒ 结果不变 |
| `segment_store.go:1033`（`coldEventLocked`） | 逐分区找该键的冷 idx 条目 | 被查的键必为持有键，其命名空间必在列 ⇒ 结果不变 |
| `segment_store.go:1427`（`coldIndexLocked`） | 冷日志分区发现 | 冷日志**无删除路径**（`grep Deletes` 于 `segment_store.go` 为 0；退位只向冷日志写、热日志整份重写）⇒ 冷日志两语义同值 |
| `assembly/symptom_anchor_recovery_test.go:566`（G3 探针） | 与 `Load` 同一分区发现 | 与 `Load` 同步，读数口径仍一致 |
| `migrate_test.go:151`／`atomic_store_test.go:168` | 迁移副本比对／装饰器夹具断言 | 比的都是持有键的命名空间 |
| `config.go:403`（`memKV`）／`rustviking_kv.go:125` | — | **本就答持有键** ⇒ 改动后三个实现同一规则 |
| `scripts/recover-kv` | — | 不读 `Namespaces`（用 `Keys`），grep 0 |

**裁定**：无一处依赖「曾写过」。**旧语义的实际残留点**正是退位重写——`journalLog.rewrite` 用 keep 集重建 `l.index` 却从不重置 `namespaceSeen`，故被整体退位的分区永久留在枚举里。

**锚**：`memory/ledger/journal_test.go` 新增 `TestJournalNamespacesAnswerOnlyTheHeldKeys`（tombstone 掉 `ref` 唯一键 ⇒ 答 `[0]`；同视图与**重开档**两向都断言；再写一个 `ref` 持有键 ⇒ 答 `[0 ref]`）。`-count=10` exit 0。
**变异**：去掉 `if value.deleted { continue }` ⇒ 红且原文 `the namespaces of the view that committed the tombstone = [0 ref], want 0: the only ref key is tombstoned`——即**旧语义被这枚锚具名钉住**。

### 二十三·二 提交成本与耐久点：座位零新增屏障（同机三形状，200 次提交/深度）

**夹具**：`-overlay` 注入 `/tmp/recovery-06r4b/probes/zz_06r4b_cost_test.go`（`CommitFact` 事件族＋原文族＝5 键），先填深度再取 200 次样本；**同时对 `journal.go` 的每一处 `Sync()` 与 `append()` 计数**（overlay 副本插入计数器，主树未动）。

| 深度 | A 基线（无座位无接线） | B 接线无座位 | C 本树（座位＋接线） | C／A | C／冻结基线 4.144ms |
|---|---|---|---|---|---|
| 400 | 4.872ms | 4.163ms | 4.830ms | **0.99×** | 1.17× |
| 1,600 | 4.136ms | 4.846ms | 4.202ms | **1.02×** | 1.01× |
| 6,400 | 4.076ms | 4.762ms | 4.261ms | **1.05×** | 1.03× |

| 形状 | 200 次提交的 `Sync()` | 每提交屏障 | `append()` |
|---|---|---|---|
| A 基线 | 200 | **1.0000** | 200（1.00/提交） |
| B 接线无座位 | 205／207／207 | 1.025–1.035 | 200 |
| C 本树 | 207／207／207 | **1.0350** | 200 |

- **座位零耐久点**：座位键与事件族在**同一个 Batch**（`planEventLocked`），故一帧一提交；`append()` 三形状皆 **200/200＝1.00/提交**。屏障数与无座位形状**同值**（207 对 205–207，差在载体的字节预算触发点，不由座位决定）；座位每提交只多 40B 值。
- **接线那一份仍是载体屏障**：A→B 每 200 提交多 5–7 道（摊还 0.025–0.035/提交，按一次 fsync≈5ms 折 ≈0.13–0.18ms/提交），与 §二十一·七／§二十二·四 同量级。
- **对冻结基线 4.144ms**：本树 1.01–1.05×（400 深度那一行 1.17× 的**分母是本轮 A 自己** 4.872ms——首深度取样含预热，故同轮 A／B／C 对比才是判据）。
- 环境：本轮无其他测试进程，机器仍有用户应用占 CPU（load average 4.1–5.2），故只报**同轮中位数**与同轮比值，绝对值不跨机比较。

### 二十三·三 两枚新锚的增长控制案（此前只有平坦读数，无对照）

**A. `TestStoreSeatIsReadWithOnePointReadAtOpen` 新增控制案**（原控制只数条目，不量「座位省下的那份聚合」）：删掉座位后按「重推」开档，深度 40／160／640 取 3 次最快值 ⇒ **291.8µs → 823µs → 2.827ms**（16× 深度＝9.7×），断言最深 ≥ 2× 最浅。原条目读数 500／2000／8000 保留。
**B. `TestACorruptStoreSeatRederivesFromTheChain` 新增控制案**：坏座位下逐深度重推，**答案恒等于链的条数**（40／160／640）而**读到的条目 50 → 200 → 800**（16×）⇒ 重推确在读链，而非「平坦地从别处取一个数」。

**变异（两枚锚各自转红，原文）**：
- 开档把座位读两次 ⇒ `the open of a 40-event chain read the seat 2 time(s), want the one read the seat is filed under`。
- 把「座位读不出」当**零**信任 ⇒ `the open beside an unreadable seat answered 0 event(s), want the 4 its entries state`（即「不得静默答零」有牙）。
- `commitSeq<count` 由既有具名锚 `TestASeatBelowItsCountRederivesTheRank` 覆盖（本轮复跑 PASS）：重推后答案等于条目数，且下一提交取的秩越过已发布最高秩。

### 二十三·四 ⚠️ 症状锚绝对成本上升：归因于**接线**，是回归，须重估（不是负载，也不是座位）

**三形状同机同夹具（252 代，取第 1–5 代与最后 5 代中位数，单位 ms）**：

| 形状 | boot | commits | settling | sessions | frame | closing | total | 比值 | 单次耗时 |
|---|---|---|---|---|---|---|---|---|---|
| 无座位无接线（＝§二十二 前） | 13.35／34.82 | 42.44／56.39 | 13.94／15.01 | 5.01／6.93 | 31.25／71.77 | 35.21／76.81 | **139.59／264.16** | 1.89 | 53.24s |
| 接线、无座位 | 16.04／39.32 | 41.78／55.10 | 15.99／12.37 | 5.87／6.15 | **62.67／116.06** | **77.09／131.11** | **212.98／360.01** | 1.69 | 78.08s |
| 本树（座位＋接线） | 16.21／42.26 | 46.44／54.71 | 14.99／12.99 | 5.96／6.15 | 64.96／119.88 | 80.20／138.90 | **223.93／379.96** | 1.70 | 77.75s |

**判别结论**：
1. **不是并发负载（否定假设 a）**：同一台机器、同一负载窗口内复跑**改动前形状**得 139.59／264.16，与 R76 记录的 131.75／242.9 同带（53.24s 对 48.44s）——负载不能解释本树两侧同升 ~57%。
2. **是改动本身（成立 b），但归因到接线而非座位**：接线、无座位形状（212.98／379.96，78.08s）与本树（223.93／379.96，77.75s）在噪声内同值 ⇒ **座位份量≈0**，与·二 的屏障数同值互证。
3. **成本落在 `frame`（框架检查点）与 `closing`（`Close`）两条路径**：frame ×2.00（31.25→62.67）、closing ×2.19（35.21→77.09）；`commits` 全程无回归（42.44→41.78→46.44 首段／56.39→55.10→54.71 末段）。按一次 fsync≈5ms 折，frame 多 ~6 道、closing 多 ~8 道屏障——**这是推断（未逐道计数），不是实测**。

**回归声明（不隐瞒）**：**开启地址维护使一代的绝对成本升至 ~1.53–1.60×**，代价在检查点与关档的载体屏障上。**S1c 的 0.98–1.09× 只在「一次事件提交」这个操作上成立并已由本轮 0.99／1.02／1.05× 复现；它不延伸到「一代」**——把接线视作「提交成本不回归」即可放行是不成立的。反转若要成立一代成本有界，须在载体屏障预算或检查点批的形状上重估（归 08 域与 09 域），**不得以「比值从 1.89 降到 1.70」当作改善**：比值降而两侧绝对值同升，正是本条要报的形态。

### 二十三·五 未做／未证（如实）

- **store 开档仍随历史增长**（§二十二·二 未变，本次未动 `s.index`／`journalLog` 读路径）：`06.R4` 冻结验收里的「storeLoad 平坦」仍未兑现。
- **掉电未测**（只测进程死亡模型）。
- **检查点／关档的屏障道数未直接计数**（三形状墙钟对比是实测，屏障归因是推断）。
- `namespaces()` 改动后，**已整体退位的分区不再出现在热链 `Namespaces`**：本轮以代码路径与三个消费者用法论证结果不变，**未新增专门锚**钉「退位后热枚举缩小」这一面（属 08 域退位删除的删面）。

## 二十四、06.R5 执行记录：屏障预算覆盖检查点与关档，回归主项是「重写后整表重述」（2026-11-16）

**性质**：实施＋**直接测量**＋回归收窄（未闭）。主树写面＝`memory/ledger/journal.go`（载体预算与重述面）、`memory/ledger/journal_test.go`（一枚新锚＋一并读取水位的夹具）。`segment_store.go`／`journal_kv.go`／`assembly/`／`config.go` **零写**（`config.go` 与 `journal.go` 的「无接线／改前形状」探针经 `-overlay` 注入 `/tmp/recovery-06r5/probes/`，不进主树）。§一–§二十三 一字未改。

### 二十四·〇 三形状、探针与计量纪律

- **探针**：`journal.go` 的计数副本经 `-overlay` 注入（`logSync/orderSync/slotsSync/forced/budget/rewriteSync/tableSync/rebuilds/restates/deriveFrames/deriveBytes/relocations`＋五个嵌套纳秒累计器），`assembly/zz_probe_r06r5_test.go` 由 overlay 新增，按 `walkLongRunGenerations` 同形逐代重走并在每个相位前后取计数快照。主树二进制零改动。
- **绝对读数含并发负载**：本机 load 4.9–5.0（Qoder／VPN 等常驻，非测试进程）。故**改前／改后取自相邻两次同命令复跑**，并同时报症状锚自身的离散控制（两独立根同形状 141.6/144.4ms、153.1/160.7ms、158.7/159.1ms——本机当日离散 ≈2–24%）。
- **一次 fsync 并非恒定 5ms**：R71 的 5.0ms 是同负载下的中位；本轮实测小文件重建的 `out.Sync()` 可低到 0.12ms（§二十四·三 的改前形状 `nsrewrite≈0.1–1.8ms`）。故屏障**道数与重述工作量**分列读数，不互相折算。

### 二十四·一 直接测量：frame／closing 的屏障道数与耗时（改前，接线开）

`PROBE_GENERATIONS=8`，逐代取 frame（框架检查点）与 closing（`Close`）前后的计数差：

| 相位 | 墙钟 | 屏障道数 | 分解 | 重述工作量 |
|---|---|---|---|---|
| frame | 57.7–76.7ms | **7** | log=2 order=1 slots=1 rewrite=1 **table=2** | rebuilds=1，回放 frames=16–43／bytes=12.9–76.3KB |
| closing | 67.0–89.1ms | **9** | log=2 order=2 slots=2 rewrite=1 **table=2** | rebuilds=1，回放 frames=18–38 |

- **改前形状（接线关）同形对照**：frame **3** 道（log=2 rewrite=1）、closing **3** 道，rebuilds=0、回放 0 帧。
- ⇒ **R78 的 ×2.00／×2.19 归因成立，但归因成分要改写**：多出的不是「每载体一道」的常数，而是**每次日志重写都触发一次整表重述**——`retire()` 删掉载体后 `reopened()` 走 `rebuild()`＝`resetTable`（1 道 table 屏障）＋**全量回放新日志**（16–43 帧，随存活键数增长）＋该回放在撞上长任务键时 `grow(96→192)` **再整表重述一遍**（第 2 道 table 屏障）＋`flush(true)` 的 order＋slots 两道。frame 与 closing 各含一次 `relocate→rewrite`，故各付一份。
- **`commits` 无回归**的原因也在此：提交路径只走 `note/confirm→flush(false)`，预算 64KiB 之内零载体屏障（与 S1c 一致）。

### 二十四·二 落地的形状：重述按重写所载语句，不重放

1. **`rewriteInto` 一并答出它写下的语句**：每个保留键一帧 ⇒ 一条 `journalOrderStatement`（`ValuesEnd`＝该帧结束字节），replaced 帧同样一条。`state()` 与它共用 `orderStatementOf`，故回放写出的语句与重述写出的语句**逐字节同形**。
2. **`reopened(statements)` → `restate(statements)`**：整份重述＝`shapeFor` 一次定宽定容（步长＝能容下最长键的那一档，容量＝同一记录数下增量增长会到达的那一档，故表形状与增量增长**完全同形**）＋逐条 `fileCommit`。**不读新日志的任何一帧**。
3. **重述的字节按预算取屏障**（不 `flush(true)`）：`restate` 只把字节 staged；屏障由下一次 `flush`（下一次提交撞预算／关档）取。这恰好就是 S1c 冻结的「按载体字节预算屏障」规则本身——**改前让重写路径自取一道强制屏障，才是对该规则的例外**。
4. **`flush(force)` 在 `staged==0` 时直接返回**：强制只意味着「不等预算」，不意味着「为零字节取屏障对」。改前的 `close()` 因此每次都白付 order＋slots 两道。

**生长轴不变**：表的容量／步长仍由「记录数＋最长键」决定且与增量增长同形；开档的 `derive(valuesSize,false)` 与 `derive(0,true)` 两条路径**未改**（开档仍取自己的屏障）。

### 二十四·三 读数：同命令相邻复跑，三形状对照

| 形状 | 首窗 total（frame／closing） | 末窗 total（frame／closing） |
|---|---|---|
| 改前（接线关＝06.R4 前） | **147.80**（31.96／39.46） | **262.36**（72.57／77.89） |
| 06.R5 改前（重写后走 rebuild） | **230.98**（79.30／71.94） | **356.09**（112.93／135.01） |
| 06.R5 改后 · 第 1 跑 | **177.94**（42.24／49.77） | **323.87**（85.07／116.80） |
| 06.R5 改后 · 第 2 跑 | **108.92**（20.04／29.92） | **304.79**（74.33／112.58） |
| 06.R5 改后 · 第 3 跑（全包同跑后，`./assembly/` 内） | **174.64**（39.20／54.87） | **271.05**（68.35／95.62） |
| 06.R5 改后 · 首跑（负载尖峰） | 169.24（39.52／54.73） | 584.76（153.04／242.83） |

- **改前形状的 147.80／262.36 与 R78 记的 139.59／264.16 同带** ⇒ 本机今日可比，读数是同量级复现。
- **frame 已回到改前量级**：末窗 74.33／85.07 对 72.57；首窗 20.04–42.24 对 31.96。
- **closing 仍高于改前**：末窗 112.58／116.80 对 77.89（+35ms 量级）；首窗 29.92–49.77 对 39.46（同带）。
- **故判据的兑现程度随跑次在 3%–23% 之间摆动，不能宣布兑现**：末窗 total **271.05／304.79／323.87**（另一次 584.76 为负载尖峰）对 262.36，即 **+3.3%／+16.2%／+23.5%**；首窗 108.92／174.64／177.94 对 147.80（区间已覆盖，方差 > 差额）。**本机离散（同日两独立根 141.6/144.4、153.1/160.7、158.7/159.1）不足以把 +3.3% 判为达标，也不足以把 +23.5% 判为回归**——故按「不宣布达标」处置。

### 二十四·四 未闭的差额与其归因（须具名上报，不得以比值改善冒充）

**剩余差额全在 `closing`，且已被本轮改动搬到同一处**：frame 的重述不再取屏障，其 staged 字节（本锚末窗约 2,700 键 ⇒ 表 1.5MB 填充＋2,700 条记录＋2,700 行语句）由**关档的那次强制 `flush`** 确认，故 closing 付「重述的 2 道屏障＋1.5MB 脏页落盘」。

- **不是屏障预算漏配**：改后 frame=3 道、closing=5 道（log=2、rewrite=1、order=1、slots=1），而改前是 7／9。剩下的 2 道是**任何「整份重述」都要付的一次屏障对**（两个载体文件 ⇒ 两次 fsync，无法合并）。
- **三案已排除**（本轮逐条推理，未再实测）：
  - ①「重述也不取屏障、留待预算」＝已落地；再进一步只能**不写表头**，而表头缺失即开档重放整份新日志（＝把 closing 的 35ms 换成 boot 的 O(存活键) 回放），**净损**。
  - ②「就地表头失效再原地打补丁记录」：改前表头陈述的 `ValuesSize` 大于重写后的日志 ⇒ 开档按 `ErrJournalCorrupt` **具名拒绝**（该规则正是丢日志的检测面），除非先为零表头取一道屏障——**与省下的同价**。
  - ③「重写不重述、开档从 cut 重述」：成本形状与①同，只是把同一份重述搬到 boot。
- **净结论**：`06.R5` 把「每次重写→整表重放＋步长重述」的**增长项**消掉了（末窗 frame 112.93→74–85，closing 135→112–117），**但没有把一代的绝对成本送回改前量级**。差额＝**存活键集的重述工作 × 2 次重写/代**，是「载体必须是日志的派生」这一冻结形状的**内在价**，只能由「重写次数」或「载体数」来降，而那两项都不在本叶白名单内（前者属 08 域退位删除，后者属 §二十一 冻结载体表）。
- **比值不进结论**：末窗/首窗在改后为 1.82／2.80，改前为 1.54／1.99——**比值变化不构成改善证据**（首窗方差本身就 ±65%）。

### 二十四·五 新锚 `TestCheckpointAndClosePayOneIndexBarrier`（两向＋增长控制）

- **观察面**：屏障本身在生产里没有计数器（不新增测试后门），故钉的是**屏障唯一可观察的后果**：重述**读了多少日志字节**、**是否陈述了它到达的水位**（表头＝屏障对之后才写的那个陈述）、**staged 是否非空**；加上「一次关档之后表头陈述该水位、下一次开档只读 64B 表头」。
- **正锚**：三次深度（4／16／64 记录＝20／80／320 键）各做一次 `relocate`（＝检查点落盘的搬迁）＋一次 `close`：搬迁后 bytes=0、stated=false、staged>0；关档后表头＝该次到达的 `Seq`／`ValuesSize`；再开档只读 64B。
- **增长控制（证明界可失败）**：同三深度改走 `rebuild()`（＝改前形状）⇒ bytes=1,530／…、stated=true，判定以 **one barrier per index file** 具名拒绝，且 bytes 随深度增长（`controls[last].bytes > controls[0].bytes`）。
- **负锚**：把预算规则退回「重写自取强制屏障」（overlay 换回 `reopened→rebuild` 且去掉 `staged==0` 短路）⇒ 同一命令 **exit 1**，原文 `the restatement at depth 4 read 1530 byte(s) of the log it wrote, want none: … one barrier per index file …`。
- **`-count=10`**：ok 11.241s（10/10）。

**勘误（08.R3 执行，2026-11-16）**：本节锚已**删除**。其正锚的搬迁步是 `log.relocate`，负锚是「把重述退回 `reopened→rebuild`」，两者都在退位生产面删除后不存在；锚与 helper `judgeOneIndexBarrier`／`oneIndexBarrierReading`／`controlReadingLine` 同批删除。本节记录的读数保留为历史证据，屏障面（frame／closing）须由 06.R5 按存活路径重出新锚。

### 二十四·六 未做／未证（如实）

- **末窗绝对成本未回到改前量级**（304.79／323.87 对 262.36）：见·四 的归因与三案排除；**本叶按派发令不勾**。
- **症状锚仍红**（整体反转未落），未声称转绿。
- **改后读数方差大**（首窗 108.92–177.94），本机 load 4.9–5.0 且非测试进程；单跑读数不足以支撑 ±10% 级结论。
- **掉电未测**：重述把「表头在最后」的规则保持原样，但「重写后到下一次载体屏障之间」表头陈述为空集，掉电窗口比 §二十一·六 的「一个 chunk」宽，**未实跑**。
- **`assembly/` 只跑一次**（与并发单共享包），两具名预期红之外的归属见本轮战报。

## 二十五、06.R7a 读侧取证：`l.index`／`l.order`／`l.tail` 消费者枚举、addr 覆盖判定、只读视图开档语义、迁移序列（2026-11-16）

**性质**：只读取证与设计。主树零 `.go` 写、零删除；探针经 `go test -overlay` 跑在 `/tmp/recovery-06r7a/`，仓内零落盘。本节只追加，§一至§二十四 一字未改。结论逐条标注 **读码确证**／**实测确认**／**推断**。

### 二十五·〇 命令、退出码与红锚原文

| 命令 | 退出码 | 结论 |
|---|---|---|
| `go build ./...` | 0 | 所读的是可编译的树 |
| `go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1` | 1 | 红锚今日复现 |
| `go test -overlay /tmp/recovery-06r7a/overlay.json ./memory/ledger/ -run '^TestProbeR7a' -count=1 -v` | 0 | 两个探针 PASS，读数见·三 |

红锚原文（本次复跑，`journalBytes` 与派单所记差 12 字节属日志本体长度抖动）：

```
the boot of generation 2 replays 331331 journal bytes beside the 136617 of generation 1, over the bound 273234: the open read grows with the history instead of staying bounded (journalBytes=[0 136617 331331 565604 839090] indexEntries=[0 43 94 145 196] durableLoadCount=[0 12 24 36 48] durableAccount=[4 8 12 16 20])
```

### 二十五·一 `l.index`／`l.order`／`l.tail` 的全部生产消费者与读形状（读码确证）

**生产（`memory/ledger`）**：

| # | 位置 | 读什么 | 读形状 | 频次量级 |
|---|---|---|---|---|
| 1 | `journal.go:2065` `l.read` → `l.index[key]` | 位置＋`deleted`＋`valueNull` | 点读／判墓碑／判 null 值 | 每事件正文缓存未命中一次（`segment_store.go:1109`）＋每开档一次席位读（`:404`） |
| 2 | `journal.go:2086` `l.keys` → `range l.index`，跳 `deleted` | 键集＋`deleted` | 判存在＋判墓碑＋枚举全键（不读值） | **每开档每分区两次**（`segment_store.go:341` 的 `N:idx:` 与 `:356` 的 `N:meta:`）；`journalKV.Read{Prefix}` 另走一次（`journal_kv.go:66`）＋逐键取值 |
| 3 | `journal.go:2101` `l.namespaces` → `range l.index`，跳 `deleted` | 命名空间集 | 枚举（派生自全键） | 每开档一次（`segment_store.go:317`） |
| 4 | `journal_kv.go:243` `k.log.index[key]` | 存在＋`deleted` | 按提交序走查时的逐键判存在／判墓碑 | 每开档一次走查（`segment_store.go:445`），走到集齐已发布条目 |
| 5 | `journal.go:1990` `l.eachKeyNewestFirst` → `l.order` 逆序 | 提交序键序列 | 按提交序走查（可提前停） | 同上（#4 的唯一调用方链） |
| 6 | `journal.go:702` `tailKeys` → `l.tail` | 尾键集 | 取尾集（nil＝整篇） | 每开档一次（`checkpoint.go:846`），**唯一消费是日志行**（`checkpoint.go:870` 的 `tail=%s`） |

**写点（逐个判独立存在理由）**：

| # | 位置 | 触发 | 删掉读权威后是否仍有独立理由 |
|---|---|---|---|
| 7 | `journal.go:1968`／`:1971` `indexRecord` ← `publish:756`（开档逐帧）＋`append:2029`（每提交） | 开档／每提交 | **无**。`l.index` 的两个写者只服务上表 #1—#4 的读；#1—#4 切走后写点失去唯一目的 |
| 8 | `journal.go:688` `seedIndexCut` ← `openJournalLog:660` | 开档 | **无，且今日即死**：`writeJournalIndexCut`（`:851`）与`appendJournalIndexCut`（`:875`）全仓零调用方（读码确证，`.go` 全量 grep 只命中定义），故 `readJournalIndexCut` 恒答 not held、`from` 恒 0、`l.tail` 恒 nil |
| 9 | `journal.go:1979`／`:1981` `noteOrder` ← `publish`＋`append` | 开档／每提交 | **无**。#5 读的是 `l.order` 内存切片；`eachNewestFirst` 读的是 order 文件，其行由 `addr.fileCommit`（`:1276`）写，与 #9 无关 |
| 10 | `journal.go:1956` `noteTail` ← `publish`＋`append` | 开档／每提交 | **无，且今日零写**：`noteTail` 首行 `if l.tail == nil` 恒真（#8 使 `l.tail` 恒 nil），故 `l.tail` 恒 nil、`TailKeys` 恒答 nil |

**测量面**：#11 `journal.go:712` `openReadBytes` ← `journal_kv.go:116` ← **唯一消费方是 `assembly/symptom_anchor_recovery_test.go:83`**（测量-only，生产零消费）。`IndexCutNotice` 的生产消费方是 `assembly/run.go:617`。

**测试（本单只登记，不改）**：`journal_test.go:456`（`reopened.index[...]` 直读内存 map）、`:795/:803/:812/:871/:987/:1100/:1161/:1224/:1292/:1525`（`addr.read`）、`:990/:1083/:1232/:1296`（`addr.keys`）、`:1090/:1308`（`addr.namespaces`）、`:618/:1036/:1118/:1136/:1157`（`statesAddressMaintenance`）、`:1166`（`addr.openBytes`）、`:1380/:1387/:1406`（经 `AtomicStore` 的 `Namespaces`／`Keys`）；`journal_index_cut_test.go:49/52`（`JournalBytesOpen`／`TailKeys`）；`journal_kv_test.go:90/106`；`segment_store_test.go:564`（替身 `silentOrderKV`）；`assembly/symptom_anchor_recovery_test.go:83/449/485/489/493`。

### 二十五·二 `journalAddress` 逐形状覆盖判定（读码确证）

| 读形状（上表 #） | addr 已有方法 | 判定 |
|---|---|---|
| 点读＋判墓碑＋判 null（#1） | `read`（`:1617`）→`lookup`（`:1374`）→`lookupOnce`（`:1387`）→`value`（`:1627`） | **能服务**。两次小读，与历史无关 |
| 判存在（#2/#4） | `lookup` 只答 `held` | **能服务** |
| 枚举全键＋前缀（#2） | `keys`（`:1646`）：`eachRecord`（`:1452`）扫表，只收 `journalSlotHeld` 且 `stated` | **能服务**；成本＝表字节数（capacity×stride），与历史长度无关，随**持有键数**增长（与 `l.index` 同一增长的另一种计量） |
| 枚举命名空间（#3） | `namespaces`（`:1665`） | **能服务**，同上 |
| 按提交序走查（#5） | `eachNewestFirst`（`:1690`）：自 `orderSize` 逆序 64KB 读 order 文件，按键去重，delete 记入已见且不交付 | **能服务，且形状更贴**：今日 #4 的判存在／判墓碑是 `eachKeyNewestFirst` 交付重复键后由调用方逐键查 `l.index` 过滤；`eachNewestFirst` 自带去重与墓碑排除，`journal_kv.go:243` 的过滤整条消失 |
| `Read{Prefix}` 的键＋值（#2 的成对形态） | `keys` ＋逐键 `read` | **需新增一个方法**：现有组合对每键做两次探测（`keys` 的一次扫表＋`read` 的一次探链）。开档对每个 `N:idx:` 键都要取值，故建议加 `eachHeld(prefix, visit func(key, span))`（扫表一趟交付键与 span），`keys`／`namespaces`／`Read{Prefix}` 三处共用。**若不新增**：功能仍可服务，成本为每键多一次探链（可接受但属已知多余） |
| 取尾集（#6） | 无 | **本单不判**：`TailKeys` 的重新定义归 §十七·三 的 05/09 叶（「序结构相对最新框架 checkpoint 的有界前缀」，配 `TestSeatReadsTheTasksCommittedPastTheCheckpoint` 负锚）。本单只须保证它仍答 nil —— 今日生产也是 nil，非行为改变 |

**墓碑与 null 值的逐行比对（不假设等价）**：

| 语义 | `l.index` | addr | 判定 |
|---|---|---|---|
| 墓碑 | `indexRecord:1971` 写 `journalValue{deleted:true}`；`read:2066` 答 `(nil,false,nil)`；`keys:2088` 跳过 | `fileCommit:1288` 写 `journalSlotAbsent`；`lookupOnce:1405` 答 `(span{},false,nil)`；`keys:1649` 只收 `journalSlotHeld` | **等价**，且墓碑的键在两侧都永久保留（`l.index` 不清键，槽位也不回收），故「持有键集」口径一致 |
| null 值 | `journalScanPuts:375` 以 `valueNull:true` 起始，只有 `"` 开头的值才改写；`read:2069` 答 `(nil,true,nil)` | `spanOf:979` 携带 `Null`；`value:1628` 答 `(nil,true,nil)` | **等价**。附注：该扫描对**任何**非字符串值都记 `Null`（数字、布尔、对象同此），两侧同一路径，故不引入差异 |
| 越界守卫 | 无（`l.index` 由重放构建，天然在界内） | `stated:1421`：`span.Offset+Length <= a.valuesSize`；`lookupOnce:1408` 对越界 span 答「不持有」 | **不等价，是新增约束**。`a.valuesSize` 是**本视图的水位**：写视图每提交推进（`fileCommit:1294`），读视图只从表头取（见·三）。下面·三给出这条守卫在读视图上的后果 |

### 二十五·三 只读视图开 addr 是否写盘：三条写路径（实测确认＋读码确证）

**探针读数**（`/tmp/recovery-06r7a/probe_test.go`，`go test -overlay`，两次提交各两键，同进程）：

```
PROBE writer inMemory{seq=2 valuesSize=224 orderSize=256 staged=640} header{seq=0 valuesSize=0 orderSize=0} orderFile=256 logSize=224
PROBE second open: order 256->256 bytes (sameBytes=true), slotTable 98368->98368 bytes (modMoved=true), secondAddr.openBytes=288 secondHeader{seq=2 valuesSize=224 orderSize=256}
PROBE an unstated root costs a rebuild at open: order=69 bytes slotTable=98368 bytes openBytes=57 seq=1
```

**三条写路径（逐条给读码依据）**：

1. **句柄创建**：`openOrder:1114` 与 `openSlots:1124` 都用 `O_RDWR|O_CREATE`，故打开一棵没有索引文件的根就**创建两个文件**；探针第三行实测：一棵从未陈述索引的根，开一次 addr 后落盘 `slots` 98,368 字节（`journalSlotCapacity×journalSlotStride＋64`）与 `order` 69 字节。
2. **截断再重述**：`openJournalAddress:1088` 判 `!stated || header.OrderSize > orderSize` 走 `rebuild()`；`:1097` 判 `orderSize > a.orderSize` 先 `Truncate`，再在 `:1103` 判 `valuesSize < log.size` 走 `derive`。实测：活写视图的表头停在 `{seq=0,valuesSize=0,orderSize=0}`（`flush:1600` 在 `staged < journalIndexSyncBytes` 时提前返回，而 `fileCommit:1279` 每次都推进内存里的 `orderSize`），于是第二个开档把 256 字节 order 行截到 0 再从日志整篇重述 —— `secondAddr.openBytes=288`（＝日志 224B 全长＋表头 64B）就是「整篇重放」的量，`modMoved=true` 是那次 98KB 重写的痕迹。
3. **表头写入**：`derive`→`flush(true)`→`writeHeader`（`:1579`）在 `slots` 首 64 字节写入水位。

**结论**：只读视图开今日的 `openJournalAddress` 会在活写者脚下（a）清掉它尚未 flush 的 order 行、（b）整表重写 slot table、（c）整篇重放日志。三者都在 `acquireRootWriteLock`（`config.go:180`）之外，直接违反 `config.go:270` 的「一目录一写者」不变量。**故只读视图不得走这条路径。**

**冻结的读面（本单对本域最关键的设计决定）**：

- **水位只取表头**：`readHeader`（`:1760`）后 `a.seq/valuesSize/orderSize ← header`，读面无内存水位推进。
- **窗口**：`W = header.ValuesSize`，`S = stat(journal.log).Size`。`journalReplay`（`:591`）只读 `[W, S)` 一趟，把每条完整帧的 span 与提交序记进视图私有的 `overlay`（键→span、墓碑集、提交序序列）；`complete < S` 即撕裂末帧，**读视图不截断**，只把 `complete` 当自己的可见末端。窗口长度由 `journalIndexSyncBytes`（64KB 的 order＋槽字节）界住，即**最大几百个提交、与历史长度无关**。停稳的写者在关档时 `close:1750` 走 `flush(true)` 把表头推到日志末，故停稳根的窗口为空——探针实测 `addr.openBytes=64`、两文件尺寸与 mtime 不变（`wrote=false`）。
- **四种读形状先查 overlay、再查表**：点读与判存在先看 overlay（墓碑⇒不持有；持有⇒按其 span 取值）；`keys`／`namespaces` 取「表的行 ∪ overlay 的键，减 overlay 墓碑」；`eachNewestFirst` 先把 overlay 的提交序逆序交付、再用已见集跳过表侧旧行。`stated()` 的界改用 `complete`，故写者为窗口内提交写入的槽记录（其值越过 `W`）由 overlay 作答，而不落到 `lookupOnce:1408` 的「不持有」。**这一条是必需的**，否则 `lookupOnce:1408` 会把窗口内的键静默答成不持有。
- **为什么窗口必须重放日志帧而不是只读 order 行**：`memory/ledger/fact_commit_batch_test.go:253` 这条既有锚要求：一次返回 `ErrCommitUncertain` 的提交（帧已落盘、`confirm` 未跑、表头从未陈述它），一个独立只读开档必须读回该记录。order 行在这条路径上根本不存在（`fileCommit` 未执行），只有日志帧携带它。**故 overlay 的输入是帧，不是语句。**
- **读面的三条禁用动作**：不 `O_CREATE`（缺文件 ⇒ 具名拒绝 `ErrAddressUnstated`，报文指向「由持写权的视图陈述」）；不 `Truncate`；不 `rebuild`／`derive`。表头缺失、解码失败、`ValuesSize > S` ⇒ 同一具名拒绝（与 `openJournalAddress:1083` 今天的「表头陈述日志没有的字节」同口径）。表头**落后**日志不是拒绝条件——那正是窗口。
- **写面保持今日路径**：`openJournalAddress` 的 `rebuild`／`truncate`／`derive` 一字不改，由 `WriteRights` 门控（`config.go:238`）。
- **一处只读面与写面的语义差异，须具名承认**：写视图的读看得到它自己尚未 flush 的提交（`a.valuesSize` 内存推进）；只读视图看到的是「表头＋窗口」，与今日「整篇重放至日志末」在**活写者**下等价（窗口覆盖活写者未 flush 的全部提交），在**死写者**下等价（窗口空、表头在日志末）。两者在 `S` 与 `W` 之间没有第三种状态。**唯一残留**：`rollback:1241` 把 order 文件截回水位后，被丢弃提交的槽记录仍指向越过水位的 span；读视图对这些键的答案是 overlay（帧仍在日志里），故不落空——这条留待·七 实测确认。

**`config.go` 接线点**：`:225` 仅在 `cfg.WriteRights` 时取 flock；`:238` 把 `cfg.WriteRights` 直接传给 `statesAddressMaintenance`。故读面与写面的分派点就是这一个布尔，`StorageConfig.WriteRights`（`:64-70`）的语义「声明写权者写、其余读」不需要新字段：读视图拿到的是 addr 的读面，不是没有 addr。生产侧的写视图只有 `assembly/run.go:605`（无条件置 `true`）；**唯一的生产只读视图是 `scripts/recover-kv/main.go:104`（`WriteRights: restore`，dry-run 时为 false）**，其 `:100` 的注释明确要求 dry-run 在活运行时旁读——故读面必须能开，且必须不写。

### 二十五·四 冻结的迁移序列（每步后 `go build ./...` 与 `go vet ./memory/ledger/` 保绿）

| 步 | 文件 | 改什么 | 删什么 | 锚（本步新增） |
|---|---|---|---|---|
| **S1** | `journal.go`、`journal_kv.go`、`config.go` | 把 `openJournalAddress` 拆成写面（今日路径）与读面（表头＋`[W,S)` 重放进 overlay，只读句柄、禁三条写动作）；`statesAddressMaintenance(on)` 改为「两视图都开 addr，`on` 只决定读面／写面」；`journalKV` 透传 `addr` 的读面 | 无 | ①读面开档零写（两文件 size＋mtime＋内容不变）②缺表头的根开读面 ⇒ `ErrAddressUnstated` 原文 ③读面在活写者未 flush 的窗口内点读到该窗口的键（对照同根写视图的 `l.index`） |
| **S2** | `journal.go`、`journal_kv.go` | `l.read`／`l.keys`／`l.namespaces`／`eachKeyNewestFirst` 四处的**函数体**切到 addr（写面用其内存水位，读面用 overlay）；`journal_kv.go:243` 的 `k.log.index` 过滤删掉（`eachNewestFirst` 自带） | 无（`l.index`／`l.order` 仍在写，只是零读） | ④墓碑＋null 值在两种形状下的逐键等价断言 ⑤`Read{Prefix}` 键集与值逐对相等 ⑥`eachCommittedNewestFirst` 的键序与今日逐一相等 |
| **S3** | `journal.go`、`journal_kv.go`、`assembly/run.go` | `openJournalLog` 停整篇重放：读表头取 `W` → 单趟 `[W,S)` → 写面交 `derive` 落盘、读面进 overlay → `openBytes = S-W` | `l.index`、`l.order`、`publish`、`indexRecord`、`noteOrder`、`eachKeyNewestFirst`、`l.keys`、`l.namespaces`、`seedIndexCut`、`journalIndexCut`／`journalCutValue`／`cutValue`、`readJournalIndexCut`／`writeJournalIndexCut`／`appendJournalIndexCut`／`journalCutOrder`、`journalIndexCutFileName`／`journalIndexCutVersion`、`l.cutOffset`／`l.cutNotice`／`cutStatement`、`journalKV.IndexCutNotice`、`assembly/run.go:617-621` 的 WARN 消费方 | ⑦`openBytes` 随历史平坦且窗口外为零 ⑧`journal.index` 停写后同一根的开档与今日读回逐键相等 |

- **S3 的边界（须写在任务行里，否则锚会被读成它没量到的东西）**：`openJournalLog:672` 的 `Truncate(complete)` 是**日志文件**上的写。若·九.3 的读视图日志截断缺陷不在 S1／S3 内一并处置，则读面「零写」锚只覆盖 `order.log` 与 `slots` 两个索引文件，不覆盖 `journal.log`；届时 `journal_test.go:158-173`（直开 `openJournalLog` 断言撕裂尾被截）必须写明它开的是哪一面。**两项处置各写一条锚，不得用一条锚冒充两面。**

- **S3 必须原子（不可再拆）的理由**：`l.index` 一旦只由窗口构建就是残缺的（只含窗口内键），任何仍读它的路径立即答错。故「停整篇重放」与「删掉 `l.index` 的全部读者」必然同批；S2 已经把四个读者全部切走，S3 因此只剩删除，仍可在单批内保绿。
- **S1／S2 可分别独立上线**：两者都不改任何一条读路径的**答案**，只改答案的来源，故回退粒度到单步。
- **与并发单的触碰集**：`config.go` 与 11.R3 同触（§十七·〇 已登记），须串行；`journal.go`／`journal_kv.go`／`assembly/run.go` 三文件本域独占。

### 二十五·五 `l.openBytes` 的语义重述与既有锚逐条处置

- **新语义（S3 起）**：`l.openBytes` 量「开档读入的 journal 字节」＝`S - W`，即**表头水位之后到日志末**的一段。停稳根为 0；崩溃根的窗口＝一次 flush 预算内的提交；它再也不等于日志总长。`addr.openBytes` 保持量「表头 64B＋本视图重放的窗口」，故 `JournalBytesOpen` 报 `l.openBytes`（窗口只计一次，不重复计表头）。
- **一句必须写进方法注释的判据**：这是「一次 boot 付的 journal 读」，且**它可为 0**；「有没有历史可读」由日志长度陈述，这个计数只陈述读入量，因此可为 0。

| 既有判据 | 处置 | 理由 |
|---|---|---|
| `symptom_anchor_recovery_test.go:485`（`journalBytes` ≤ 2×第二代） | **判据不变**，改绑更硬的常量界：`journalBytes ≤ journalSlotHeaderBytes + journalIndexSyncBytes` | 原判据是「不随历史增长」，改绑常量界是**收紧**（2× 一个小数会随噪声抖，常量界不会） |
| `:449`（第二代 `journalBytes != 0`，否则判夹具不成立） | **必须重述**：改为「日志长度（stat）非零且随代增长」 | 开档读入被界住之后，停稳根**正确地**读 0 字节；此判据照旧写会把正确实现永久判红。它原本要守的是「有真历史可读」，改由日志长度陈述，语义不减 |
| `:489`（`indexEntries` ≤ 2×第二代） | **本单不解，保持红** | 它量的是 `FileSegmentStore.s.index`（`segment_store.go:389/593`），由 `Load` 的前缀读物化，属 §十七·三 的 store 面；本单只动引擎开档 |
| `:493`（`durableLoadCount`） | 不动 | 与引擎索引无关 |
| `journal_test.go:1166`（`addr.openBytes < journalOrderChunkBytes`） | 不动 | 量的就是窗口，语义与·五一致 |
| `journal_index_cut_test.go:49/52`（`JournalBytesOpen == stat.Size()`、`TailKeys == nil`） | **改** | 它钉「无切面 ⇒ 整篇重放」，正是本单推翻的形状；改为「无切面 ⇒ 窗口 0、`TailKeys` 仍 nil」 |

**因此本单的 DoD 只能写「症状锚的 `journalBytes` 判据转绿」，不得写「症状锚转绿」**（`indexEntries` 面仍红，见上是）。验收面按原样冻结、不缩：改绑常量界是收紧，`:449` 重述是同一守卫换一个观察点。

### 二十五·六 风险与回退

- **最坏情形**：读面的 overlay 与表的拼接在某个形状上错位（例如 `keys` 的并集把 overlay 的墓碑漏掉），使只读视图多答一个墓碑键。**判别测试**：S2 的锚④⑤⑥逐形状对照写视图的 `l.index` 答案，并要求一具名负锚——把 overlay 的墓碑集短路成空集，锚④必须转红（证明它有牙）。
- **次坏情形**：`stated()` 的界取错（用表头 `W` 而非窗口末端 `complete`），则活写者窗口内的键在只读视图上答「不持有」。判别同上锚③，负锚：把 `complete` 换回 `W`，锚③转红。
- **第三情形**：`openJournalLog` 只重放窗口之后，撕裂末帧的定位必须仍在窗口内完成（`journalReplay:591` 的 `size-offset < journalFrameHeaderBytes`、`:604` 的 `size-offset-journalFrameHeaderBytes < length` 与 `complete < size` 三条判据照旧生效）。判别：现有撕裂尾锚（`journal_test.go:161/192`）在 S3 后必须仍绿。
- **回退**：S1／S2 各自独立可回退（只换来源，不删面）。S3 的回退单位是一个提交内的整批删除；回退后 `l.index` 整篇重放复原，症状锚的 `journalBytes` 面随之回红——这正是「红转绿」证据的另一半。

### 二十五·七 读码不能确定、须实施单实测的问题（不假装闭合）

1. **窗口的实测长度**：`journalIndexSyncBytes` 界住的是 order＋槽字节，不是日志字节；一个含大 checkpoint 的提交可能让窗口达数百 KB。真夹具下每代开档窗口的字节数与提交数**未测**（红锚的平坦性依赖它）。
2. **`rollback:1241` 窗口内只读视图的答案**：读码推导为「overlay 覆盖」（帧仍在日志、语句被截），但需一个把 `state()` 打成失败的探针实测。
3. **生产只读视图的实际成本**：唯一生产读面是 `recover-kv` dry-run；`assembly/` 开档路径全为写视图，故读面在真实链路上的成本**零样本**。
4. **`keys(prefix)` 的绝对成本**：`capacity×stride`（今日 1,024×96；键数过 512 即翻倍）随持有键数增长，与 `l.index` 的内存增长同轴换了个介质。6,400 键下的一次扫表字节数与耗时**未测**。
5. **掉电序**：`writeHeader:1579` 自带「不取屏障」的论证（表头是加速器）；开档界改由表头陈述后，掉电可能留下「表头指出窗口起点、槽记录未落盘」的根，靠 `decodeSlotRecord` 失败→`rebuild` 自愈。自愈路径的成本**未测**，§二十一·六 的「一个 chunk」论证需按新开档形状复核。
6. **`eachHeld` 是否值得新增**（见·二「需新增」行）：`Read{Prefix}` 每键多一次探链的绝对成本未测，故「新增一个方法」与「接受多一次探链」之间本单只给倾向，不给判决。

### 二十五·八 未做／未证（如实）

- 零 `.go` 改、零删除、零提交。探针只在 `/tmp/recovery-06r7a/`，经 `-overlay` 注入，仓内未落盘任何文件。
- 未跑 `./assembly/` 全量、`./tests/...`、`scripts/lint.sh`（按派发令）。
- `indexEntries` 面归属 store 域、`TailKeys` 归属 05/09 域，本单只给接口面，不实施。

### 二十五·九 对派单前提的三处更正（具名反驳，各给依据）

1. **「依症状锚即可判本单完成」不成立**：红锚三个量化面里本单只能转绿一个。`:489` 的 `indexEntries` 量的是 `s.index`（store 面的物化），与本单的引擎开档不相交；实测读数 `indexEntries=[0 43 94 145 196]` 在删除 `l.index` 后仍会按历史增长。**DoD 必须按 ·五 的写法分面落**，否则实施单会以「锚绿」冒充完成。
2. **「`l.index` 与 `a.slots` 是第二真相」须限定在「重复物化」而非「重复持久」**：`l.index` 无任何持久形态，它整篇由日志重放得出；真正被持久陈述两次的是 `order.log` 的行与 slot table 的记录（同一 span 两处），而这一对是 §十七·二／§二十一·一 有意冻结的分工（一处答枚举、一处答点读）。故「单一权威」的正确落点是「日志是唯一持久真相，两个索引文件是它的派生」，删掉 `l.index` **不减少任何持久陈述**，只减少一次开档物化。
3. **「只读视图照今日写权门控就够」不成立，且缺陷比派单描述更宽**：不只是「读侧没有 addr」，而是 `openJournalLog` 本身（`:642` `O_RDWR|O_CREATE|O_APPEND`、`:672` `Truncate(complete)`）就让只读视图**写日志文件**。活写者写到一半时开一个只读视图，`journalReplay` 会把它正在写的那帧读成撕裂尾并**截掉**（`:672`），随后写者的 `O_APPEND` 落在新的 EOF 上。这条路径与索引无关、今日即存在，唯一的生产触发者是 `recover-kv` dry-run（`main.go:100` 明确要求「在活运行时旁读」）。**本单不修（越界）**，登记为独立缺陷，建议并入本域 S1 一并处置（读面同样改为不截断、只把 `complete` 当可见末端，与·三 的 overlay 判据同形）。

## 二十六、06.R3 执行记录：点查询与尾查询的历史轴（2026-11-16）

**性质**：交付①—⑥。写面＝`memory/ledger/segment_store.go`（一个出口＋一个方法）、`assembly/read.go`、`assembly/checkpoint.go`、`assembly/run.go`、`assembly/read_test.go`、`assembly/checkpoint_test.go`、`tool/recall/trace_test.go`（新文件）、本域 `design.md`／`tasks.md`。`memory/ledger/journal*.go`、`runtime/`、`features/`、`tests/`、`scripts/`、`golden/`、`examples/` **零写**；零 git 写。

### 二十六·一 出口形状：`EachIndexNewestFirstUntil`（交付①）

- **形状**：`visit func(key, partition, seq, typ, session) (bool, error)`。visit 答 `false` ⇒ 走查就地结束并答 `nil`（停不是失败，调用方无需辨认哨兵）；visit 答 `(_, err)` 或引擎报错 ⇒ 走查结束并把该失败原样上报。`memory/ledger/segment_store.go:1181`。
- **旧形保留**：`EachIndexNewestFirst` 成为该出口的**全走查**包装（`segment_store.go:1195`），因为白名单外有 7 个文件按错误面签名调用：`assembly/{anchored_boot_parity,compress_consumer,recovery_judgment,summary_gist}_test.go`、`examples/wechat-bot/smoke_test.go`、`scripts/fold-cold-root/{main,main_test}.go`。改签名＝越界，故新增出口而非改签名（方法不算新接口类型，棘轮 81/85 不动）。
- **哨兵删除**：`var errStopIndexWalk`（`assembly/checkpoint.go:796-799` 原址）与其三处使用（`walkIndexPastCut` 内）全部删除；`grep -rn errStopIndexWalk --include=*.go .` **零命中**。
- **两处全走查是有意的**：`eachCommittedEvent`（`run.go:2346`，全序重放的读缝）与 `walkIndexWhole`（`run.go:2661`，无切面兜底）**命名每一个条目**，留在旧形；新出口的文档把「命名每个条目」写成旧形的适用条件，二者不并存于同一查询。

### 二十六·二 四处查询改走有界前缀（交付②③④）

| 查询 | 改动前走多少 | 改动后走多少 | 位置 |
|---|---|---|---|
| `GetParent` 快路 | 找到键后仍走到索引末尾（visit 恒「继续」），即**整份索引** | 从最新提交倒走到该键，再走到**第一条同链更旧条目**即停：成本＝键与前驱之间的提交数 | `read.go:272` |
| `parentFromBodies` 回退 | 收集该分区**全部** refs，再逐条读正文 | 走到该键后**边读边判**，命中同链即停；不预警收集分区条目 | `read.go:325` |
| `eachFactPastCut` | 全史走查后再 `pastCut` 过滤 | 从水位起走到**尾**即停；`walkIndexPastCut` 的 `bounded` 与计数同一性 `past == EventCount()-coveredEntries(cut)` 同时成立才采用，否则整读兜底 | `checkpoint.go:1074` |
| `newestSessionCheckpointRef`／`restoreSessionCheckpoints` | 全史走查 | 该会话最新检查点／**最新框架检查点**（`session == ""`）即停 | `checkpoint.go:912`／`:947` |
| `seedAnchoredReplay` | 消费带哨兵的切面走查 | 消费 `bootIndexWalk`（`run.go:2598`）的切面走查，经新出口 | `run.go:2690` |

### 二十六·三 具名操作成本表（06.R3 行：本征 O(历史) 与可修项分列）

| 操作 | 分类 | 成本随何增长 | 依据／锚 |
|---|---|---|---|
| `GetParent`（有同链前驱） | **可修，已修** | 该键到同链前驱之间的**提交数**（跨分区、跨会话的同窗口提交都算，因为引擎只答「从最新提交倒走」） | 锚 `TestParentLookupCostsItsOwnChainNotTheHistory`：named=**4** @ 背景 400／1600／6400（比 1.00×） |
| `GetParent`（链根，无前驱） | **本征 O(历史)，现布局** | 整份索引——「无前驱」只有走完更旧条目才能断言；修复面＝引擎**按键位置**寻址（`CommitOrder` 今日只答从最新倒走） | 同上锚的控制段：同夹具链根 named=**412／1612／6412**（15.6×），证明该界可失败 |
| `parentFromBodies`（索引无 session 的回退） | **可修，已修** | 该键之后到同链条目之间的正文读次数 | 读码＋既有锚 `TestTierParentFallsBackToBodiesForMissingSession` |
| `eachFactPastCut`（切面命名链上全部分区） | **可修，已修** | **尾条目数**（＋每命名分区一条位点） | 锚 `TestTailWalkStopsAtTheWatermarkAndTheTailEnd`：600 覆盖＋6 尾 ⇒ named=**8**、visited=**6**；overlay 强制整读 ⇒ named=**615** 具名红 |
| `eachFactPastCut`（切面漏某分区，或计数同一性不成立） | **一次性具名降级** | 整份索引 | 同锚负向段（`beside` 分区）：答案集合不变，成本退回整读 |
| `newestSessionCheckpointRef`／`restoreSessionCheckpoints` | **可修，已修** | 最新框架检查点之后的提交数 | 读码＋既有锚 `TestBootRestoresTheCheckpointOnce` |
| `QueryEvents`（调用方命名授权分区） | **可收窄，已收窄到命名空间粒度** | 所命名分区的索引条目各一次＋其正文各一次；**未按 type／时间／游标预收窄**，故是 **O(命名空间条目)**，**不是 O(匹配集)** | `read.go:106` → `EachEventOfPartitions`（`segment_store.go:1093`）→ `publishedEntriesOf` 每分区一次 `{pid}:idx:` 前缀读（`:412`） |
| 全序导出／全链枚举（`eachCommittedEvent`、`Events`、`walkIndexWhole`） | **本征 O(历史)** | 整份索引／整链正文 | 承 §十九·七「全序枚举」行；本叶不改其读法 |
| 跨分区全量查询（调用方显式命名**全部**分区） | **本征 O(历史)** | 全部命名空间的条目与正文 | `queryPartitionsOf`（`read.go:138`）：空授权集答空答案（零读），全量只出现在调用方显式枚举全部 |
| 席位重推（席位不可用） | **一次性具名降级**（承 §十九·七） | 全 idx 族点读＋meta 持有判定 | §十九·七 同名行 |
| 无切面整读（`cut == nil` → `walkIndexWhole`） | **一次性具名降级** | 整份索引 | `run.go:2598-2606`：无切面＝无界，兜底非常态 |
| 兜底物化（引擎不声明序能力） | **本征，现布局** | 全部已发布索引条目 | §十九·七 兜底行（本叶不动该分支） |

### 二十六·四 未做／未证（如实）

- **交付②的字面「从该键位置反向走」在现布局不可达**：引擎只声明「从最新提交倒走」的序（`CommitOrder`／`indexKeyOrder`），没有按键起点寻址。落地的性质是可达的等价形：成本＝该键到同链前驱之间的提交数，**不随前驱之后（更旧）的历史增长**；「链根无前驱」因此是本征整读，已单列一行并把「引擎起点寻址」登记为修复面，不声称点查询全轴有界。
- **`QueryEvents` 未按 type／时间收窄**：事件类型与时间只在索引**携带值**里，读面能收窄的粒度是分区前缀；要在读正文前按类型收窄需要引擎「按携带值收窄」的前缀能力，今日不存在。故声明轴是 O(命名空间条目)，不冒充 O(匹配集)。
- **锚的绝对耗时不入表**：夹具每提交约 5ms（含 fsync），三深度构建约 40s；判据只用增长轴与控制案比值。
- **未跑**：`go test ./tests/... -count=1` 全树（按派发令只跑指定面）。

## 二十七、06.R5.a 执行记录：旧屏障锚按存活路径重出判据（2026-11-18）

§二十四·五「屏障本身在生产里没有计数器（不新增测试后门）」是本叶的硬约束：判据只能读引擎**自身已持**的状态，不加计数器，也不能从总 fsync 次数反推阶段。结论：**无需生产修改**——`journalLog.unsynced`（journal.go:2194 增、:2231 归零）与 `journalAddress.staged`／`stagedLog`（journal.go:1222 增、:1686 归零、:1253 回滚）已足以命名「本次提交覆盖了哪个耐久点」。

### 二十七·一 旧锚 → 现行存活路径映射（逐一）

| 旧锚（08.R3 已删） | 旧判据 | 现行路径（行号） | 承接方式 |
|---|---|---|---|
| `TestCheckpointAndClosePayOneIndexBarrier` 的 frame 段 | 退位搬迁步 `log.relocate` 的重述条数 | 写入：`journalKV.Commit`（journal_kv.go:188）→ `journalLog.append`（journal.go:2194）→ `journalLog.sync`（journal.go:2231，日志屏障 `file.Sync`）→ `addr.confirm`（journal.go:1222）→ `flush(false)`（journal.go:1686，两索引文件屏障，仅当 `staged`／`stagedLog` 越 64 KiB） | 正锚 `TestCommitBarrierMeasurementNamesItsPhase`：按 `unsynced`／`staged`／`stagedLog` 归零情况命名**阶段**（`log`／`index`／`log+index`／`none`），不数总数 |
| 同上 closing 段 | 关档时退位重写的重述条数 | 关档：`Run.Close`（assembly/invoke.go:118，defer 并入 `closeStorage`）→ `Tier.Close`（config.go:316）→ `FileSegmentStore.Close`（segment_store.go:1316，空批只需完成屏障）→ `journalKV.Close`（journal_kv.go:229）→ `journalLog.close`（journal.go:2275）→ `flush(true)`（强制两索引文件屏障） | 负锚 `TestCloseBarrierFailureStaysVisible`：`order`／`slots` 各自换成 `os.Pipe` 写端（写成功、`Sync` 失败）后，`journalKV.Close`／`FileSegmentStore.Close` 必须答 `ErrJournalBarrier`，且不得是 `ErrCommitAfterClose`／`ErrCommitWithoutWriteRights`；空账关档须答 nil，并**陈述**它 owed none（nil 是读数而非吞掉） |
| 负锚「把重述退回 `reopened→rebuild`」 | 退位不生效 | 无对应机制（退位已随 08.R3 删除，见本文件:1533 勘误） | **不承接**：错误路径随机制消失，不造替代假设 |
| helper `judgeOneIndexBarrier`／`oneIndexBarrierReading`／`controlReadingLine` | 计数读数 | 无（随机制删除） | `journalBarrierStateOf`（memory/ledger/journal_test.go:3023）持 `kv.mu`（提交同锁）读 `unsynced`／`staged`／`stagedLog`／`seq`／`statedSeq`；失败点由 `journalBarrierFailure`（:3061）命名（`unsynced > 0` ⇒ 日志屏障，否则 ⇒ 索引屏障） |
| 旧「一条屏障计数」判据 | 总 fsync 次数 | — | 明令禁止：新锚一律读「哪个耐久点仍欠账」，不读次数 |

### 二十七·二 运行凭据（原始退出码，非管道退出码）

| 命令 | 结果 |
|---|---|
| `go test ./memory/ledger -run '^(TestCommitBarrierMeasurementNamesItsPhase\|TestCloseBarrierFailureStaysVisible)$' -count=10 -race -v` | exit **0**；顶层 PASS 20（2×10），子测试 PASS 70（4+3 各 ×10） |
| `go test ./assembly -run '^(TestFactCommitLandsOriginalAndRecordInOneCommit\|TestCloseFilesTheCheckpointTheNextBootReads)$' -count=10 -v` | exit **0**；顶层 PASS 20（2×10） |
| `go test ./assembly -run '^TestCloseReportsTheActualBarrierFailure$' -race -count=10 -v` | exit **0**；顶层 PASS 10，子测试 PASS 20 |
| `go test ./memory/ledger -count=1`（整包） | exit **0**，76.9s |
| `go test ./assembly -count=1`（整包） | exit **0**，315.4s |
| `go build ./...` | exit **0** |
| `go vet ./memory/ledger/`、`go vet ./assembly/` | exit **0** |

### 二十七·三 可红演示（变异→红→复原→绿，sha256 一致）

- **阶段计量坍缩为总计数**：把 `journalBarrierPhase` 改成「凡欠账归零即 `log+index`」（取消两点区分）⇒ `go test ./memory/ledger -run '^TestCommitBarrierMeasurementNamesItsPhase$'` exit **1**，红项 `a_commit_inside_both_budgets_pays_the_logs_barrier_alone`（「the commit covered the "log+index" barrier, want the log's alone」）与 `a_failed_index_barrier_is_named_the_indexs`。复原后 exit **0**，`journal_test.go` sha256 = `fe594dc12a9055314a5777a25a5976f16c3ae325669a69785b4cc4a93667d8ed`（变异前同值）。
- **包装层提前拒绝**：让装配探针在屏障批上答 `ErrJournalFrameTooLarge`（写入前的具名拒绝）⇒ `go test ./assembly -run '^TestCloseReportsTheActualBarrierFailure$'` exit **1**（「answered memory: journal record is over the frame bound: …, want memory: journal barrier failed」）。复原后 exit **0**，`assembly/checkpoint_test.go` sha256 = `c8e94458aa9962c8413a2dfbb8131ca857d7cfd6b9293accb13596dd9af7e27b`（变异前同值）。

### 二十七·四 承接判据的组成（每条都非空转）

- **提交屏障阶段**（`TestCommitBarrierMeasurementNamesItsPhase`，4 段）：`log` 段（67 B 提交只付日志屏障，索引字节仍 staged）；`log+index` 段（65,536 B 值使帧越过 `journalLogSyncBytes`，同一次提交付两个屏障，`seq == 1` 落定）；`log` 失败段（`log.file` 换管道 ⇒ `ErrJournalBarrier` 且 `ErrCommitUncertain`，阶段 `none`，失败点 `log`，索引字节与 `seq` 未动）；`index` 失败段（`addr.order` 换管道 ⇒ 阶段 `log`，失败点 `index`，`seq`／`statedSeq` 回滚到水位）。每段都断言「本次提交覆盖什么」而非「总数是多少」。
- **关闭屏障失败可见**（`TestCloseBarrierFailureStaysVisible`，3 段）：视图关档（`order` 管道）+ 存储关档（`slots` 管道）各自具名 `ErrJournalBarrier`、排除两类写入前拒绝、失败后新开视图仍读回已落事实；空账关档答 nil 且先陈述 owed＝0。
- **生产关档路径可见**（`TestCloseReportsTheActualBarrierFailure`，2 段）：走 `Run.Close` 正式入口；探针记录「带键批」与「仅完成屏障批」，`failed > 0` 证明关档确实到达屏障调用；`afterFailure() == 0` 证明**该失败批是关档交给引擎的最后一批**（诊断跑观测到的调用序列 `w b w w w w w b`，7 次写 + 2 次屏障，armed 失败落在最后一格）；`CheckpointFiled() != ""` 证明关档已越过检查点阶段（错误不是更早一层）；排除 `ErrCommitAfterClose`／`ErrCommitWithoutWriteRights`／`ErrJournalFrameTooLarge` 三类「写前拒绝」；失败关档后根仍可被 `WriteRights: true` 新开，且探针记下的键在该根上仍读得到（失败是派生文件的屏障，不是日志损坏）；控制臂（不 armed）答 nil 且同样取到至少一次屏障调用。

### 二十七·五 未做／未证（如实）

- **装配层注入点不是 OS 级 fsync**：`memory/ledger` 不导出日志／索引文件句柄，装配层只能经 `StorageConfig.Engine`（config.go:64）这一条**契约明写的注入缝**（atomic_store.go「Failure injection belongs at this seam」）在「提交／完成屏障」调用处失败，测不到真正 `Sync(2)` 的 EINVAL。真 OS 级屏障失败由 ledger 层两枚锚（`os.Pipe` 写端）负责；装配层锚的判据是「错误确实是屏障的名字、且是关档最后一批」，不外推到 syscall 层。
- **探针答的哨兵是产品哨兵**：探针答 `ErrJournalBarrier`（`journalLog.sync` 与 `flush` 在真实失败时答的同一枚），因此 `errors.Is(err, ErrJournalBarrier)` 断言的是产品契约而不是自造名字；判据强度由「排除写前拒绝 + 最后一批 + 检查点已落 + 根已释放 + 事实仍在」五条共同保证。
- **未跑**：`go test ./tests/... -count=1` 全树、`go test ./... -race` 全包（按派发令只跑指定面与两整包）。
- **`comment_policy` 现状**：`scripts/comment_policy/baseline.json` 的 `counts` 为空 ⇒ 全树任何 finding 都算「超基线」，该闸今日在**本变更之前**即为红（与本叶无关，且改基线被明令禁止）。已用 `go run ./scripts/comment_policy -v -no-baseline memory assembly` 逐条核对：新增的三枚锚与其 helper（`journalBarrierStateOf`／`journalBarrierPhase`／`journalBarrierFailure`／`journalFailingBarrier`／`closeBarrierEngine`／`closeBarrierRun`／`errInjectedCloseBarrier`）**零 finding**；残留 finding 全部落在本叶写面之外的既有行（如 checkpoint_test.go:3048／3291／4178）。

## 二十八、06.R5 接续记录：08.R3 退位删除后的重测、存活路径屏障判据与核销（2026-11-18）

**性质**：只读取证＋重测＋判据重钉。主树**零 `.go` 写**（生产与测试文件均未改），计量与突变经 `-overlay` 注入 `/tmp/r5continue/probes/`。§一–§二十七 一字未改。

### 二十八·〇 时点变化（重测前提）

- **08.R3 退位机制已删除**：`rewriteInto`／`reopened(`／`restate(`／`keptHotKeys`／`RetireFacts`／`retireSettled`／`.relocate(` 全树 Go 源**零命中**（grep 实证）。即 §二十四·四 具名的「差额唯一降法①：少一次重写」**已由 08 域兑现**，本叶不必扩白名单。
- **现行屏障面只有 5 个 `Sync()`**（`memory/ledger/journal.go`）：日志（:2233）、`flush` 的 order（:1693）与 slots（:1696）、`resetTable`（:1642）、`grow`（:1609）。改前 frame/closing 多出的 `rewrite`／`table` 道数**无载体**。
- `flush(force)` 的零字节短路仍在树（:1687-1689）；预算常量 `journalIndexSyncBytes`／`journalLogSyncBytes`＝64 KiB（:127／:138），判定在 :1690。
- 产物：`journalAddress.close()`（:1961-1968）→ `flush(true)`；夹具无重述步可走。

### 二十八·一 正锚重测（5 跑独立进程，全绿）

命令 `go test ./assembly/ -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1 -v`，每跑 **exit 0**（24.96s／25.36s（`ok`）／25.84s／26.48s／26.09s；252 代／窗口 5／界 13 tenths 未改）：

| 跑次 | 首窗 total | 末窗 total | 末/首 | frame 首→末 | closing 首→末 |
|---|---|---|---|---|---|
| 1 | 81.85 | 91.13 | 1.113 | 5.05→3.91 | 14.00→13.14 |
| 2 | 80.85 | 94.93 | 1.174 | 5.03→5.08 | 13.15→13.99 |
| 3 | 86.77 | 89.04 | 1.026 | 4.97→3.82 | 15.07→12.22 |
| 4 | 82.07 | 85.82 | 1.046 | 5.01→3.88 | 15.88→12.12 |
| 5 | 75.00 | 87.83 | 1.171 | 5.11→4.10 | 13.82→12.88 |

- **跑间中位**：首窗 81.85／末窗 89.04／比值 1.113。**区间**：首窗 75.00–86.77、末窗 85.82–94.93、frame 4.97–5.11→3.82–5.08、closing 13.15–15.88→12.12–13.99（单位 ms，均为窗口内五代中位）。
- **逐阶段全平**（frame 不增反降、closing 平），没有阶段随链增长；判据是绝对读数与逐阶段窗口，**比值只如实并列、不作结论**。
- **与 §二十四 的对照口径**：该节的「改前 147.80／262.36」与「改后 271.05–323.87」都是**退位世代**的形状（frame/closing 内嵌 `relocate→rewrite` 的整表重述）。退位删除后该带**无对应机制**，故「绝对成本回到该带」不再是可测命题；现读数**低于该带两端**（首窗 81.85 对 132–140、末窗 89.04 对 243–264），§二十四·三 记的有害方向（+3.3%…+23.5%）**消失**。
- **不是夹具变轻**：同跑计数面 `journalLogBytes` 19619→8227792 逐代恒等于 `integrityBytesScanned`、`slotTableBytesScanned` 恒 0、`addressedValueReads` 第 2 代起恒 31，与 ORCHESTRATION-HISTORY:5167 记的绿读数逐项相同；夹具形状 `turns=1, perGeneration=1` 与 HISTORY:1338 同形。

### 二十八·二 屏障道数重测（存活路径，08.R3 后）

探针：`-overlay` 把 `memory/ledger/journal.go` 换成计数副本（`probeBarrierCounts`：Log／Order／Slots／Reset／Grow）＋新增 `assembly/zz_probe_r5c_test.go`（8 代，按症状锚同形逐代走，在 `Checkpoint` 与 `Close` 前后取计数差）。命令 `go test -overlay /tmp/r5continue/probes/overlay.json ./assembly/ -run '^TestProbeR5CloseFrameBarriers$' -count=1 -v` → **exit 0**。

| 相位 | 道数 | 分解 | 耗时 | 8 代间 |
|---|---|---|---|---|
| frame（框架检查点落盘） | **1** | log=1 order=0 slots=0 | 4.68–6.09ms | 恒定 |
| closing（`Run.Close`） | **3** | log=1 order=1 slots=1 | 12.68–15.92ms | 恒定 |

- `reset=0 grow=0` 逐代：closing 的 order+slots **不是整表重述**，是最后若干次提交暂存的增量索引字节被关档强制确认（`flush(true)`），即 S1c 字节预算规则本身。
- 对照：§二十四·一 改前 frame **7**（log=2 order=1 slots=1 rewrite=1 table=2）／closing **9**；§二十四·二 改后 frame **3**／closing **5**。现 frame **1**／closing **3** — 多出的道数随退位机制一并消失，**只剩 closing 的 order+slots 一对＝每载体一道的本征价**（§二十四·四 三案已排除「不写表头」「原地补丁」「搬到开档重述」）。

### 二十八·三 判据按存活路径重出（取代已删的 `TestCheckpointAndClosePayOneIndexBarrier`）

复用 06.R5.a 两锚（§二十七）**不重复造**，逐条映射本叶要的「一次检查点落盘与一次关闭各付的载体屏障」：

| 判据 | 承接锚（现有、已跑） | 观察面（不新增生产计数器） |
|---|---|---|
| 一次提交只付日志屏障，索引字节留暂存（＝预算覆盖提交路径） | `TestCommitBarrierMeasurementNamesItsPhase/a_commit_inside_both_budgets_pays_the_logs_barrier_alone` | `unsynced==0` 且 `staged>0`／`stagedLog>0` |
| 越过预算才付索引对 | 同锚 `a_commit_past_the_log_budget_pays_the_index_barrier_beside_it` | `staged==stagedLog==0`、`seq==1` |
| 两索引文件的屏障各自具名失败、不提前拒绝 | 同锚两失败段＋`TestCloseBarrierFailureStaysVisible` 三段（order 管道／slots 管道／空账 nil） | `errors.Is(ErrJournalBarrier)`、排除 `ErrCommitAfterClose`／`ErrCommitWithoutWriteRights`；空账关档先陈述 owed=0 再答 nil |
| **界可失败**（「每载体一道」的增长控制） | 待测——本轮以 overlay 突变补足，见·四 | frame 道数与耗时 |

- 关档相位由 `TestCloseBarrierFailureStaysVisible` 覆盖（两个索引文件各被点名为失败点、空账关档不取屏障）；**提交相位与帧落盘相位共用同一条 `journalKV.Commit`→`journalLog.sync`→`addr.confirm`→`flush` 路径**（`assembly/checkpoint.go` 无独立屏障点），故不为「检查点落盘」再造第二枚同形锚（§〇元规则二：结构不得自证）。

### 二十八·四 「每载体一道」的增长控制案（突变→红→复原→绿，实测）

突变：把 `flush` 的字节预算判定去掉（索引文件凡有暂存即取屏障对），即派发令的负锚形状「预算规则不再覆盖检查点/关闭路径」。

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go test -overlay /tmp/r5continue/probes/overlay-mutant.json ./memory/ledger -run '^TestCommitBarrierMeasurementNamesItsPhase$' -count=1 -v` | **1** | 红文 `journal_test.go:3105: the commit covered the "log+index" barrier, want the log's alone: the barrier over the index files waits for its budget`；3 子测试绿、1 红，失败面精确落在预算判定 |
| `go test -overlay /tmp/r5continue/probes/overlay-mutant-counted.json ./assembly -run '^TestProbeR5CloseFrameBarriers$' -count=1 -v` | 0 | 探针读数 frame **1→3 道**（log=1 order=1 slots=1）、耗时 **5.0→21.0–35.1ms**；closing 仍 3 道 |
| `go test ./memory/ledger -run '^(TestCommitBarrierMeasurementNamesItsPhase|TestCloseBarrierFailureStaysVisible)$' -count=1 -v`（复原） | **0** | 两锚 PASS，`ok … 0.551s` |

⇒ 「frame 只付日志屏障」这条界**能失败、失败有名字有读数**，不是恒真。

### 二十八·五 归属判定与核销依据

- §二十四·四 具名的两条降法：**①少一次重写＝08 域退位删除，已在树**（·〇 grep 零命中）；**②少一个载体＝§二十一 冻结载体表，不需动用**。
- 重测后 frame/closing 的差额为 **0**（逐阶段窗口全平，绝对读数低于退位世代两端）；派发令的停报条件（剩余差额降法在白名单外）**不成立**。
- 白名单内无残留可消除浪费：`flush` 是唯一索引屏障点、零字节短路在树、关档不重述（`reset=grow=0`）、`Reset`／`Grow` 只在重建与生长路径。故**本轮零生产改动**。

### 二十八·六 命令与退出码（原始退出码，非管道退出码）

| 命令 | 退出码 | 读数 |
|---|---|---|
| `go test ./assembly/ -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1 -v`（×5） | 0／0／0／0／0 | 见·一；24.96–26.48s |
| `go test ./memory/ledger/ -count=1` | **0** | `ok … 80.136s` |
| `go test ./assembly/ -count=1` | **0** | `ok … 317.545s` |
| `go build ./...` | **0** | — |
| `go vet ./memory/ledger/` | **0** | — |
| `gofmt -l memory/ledger/{journal,journal_kv,segment_store,journal_test,segment_store_test}.go` | 空 | — |
| `go run ./scripts/codetools tracked-hygiene` | **0** | — |
| `go run ./scripts/comment_policy -no-baseline assembly api runtime memory features tool internal cli` | 0（测量模式） | **63 finding(s)**，其中 memory 17，全部落在 `kv_original_store_test.go`／`segment_store_test.go` 等其他叶写面（本叶零 `.go` 写，见·七） |

### 二十八·七 未做／未证（如实）

- `go test ./tests/... -count=1` 全树未跑（派发令只点名两整包）；`-race` 本轮未重跑（06.R5.a §二十七·二 已记 `-race -count=10` 绿）。
- **`comment_policy` 零 findings 闸今日为红**（63 条，基线 `counts` 为空、改基线被明令禁止）：本叶未改任何 `.go`，故这些 finding 与本次重测无关，逐条未处置。
- 探针样本是 **8 代**（与 §二十四·〇 的 8 代取样同法）；逐代恒定已示无增长，但「代数极大时的常数」未另取读。
- 掉电窗口未测；本轮无重述路径，§二十四·六 的「宽窗口」问题随机制消失，但不因此声明掉电安全已证。
- 正锚的读数含本机负载（同日 load 波动未标定），故并列区间与跑间中位，不单取一跑。

## 二十九、06.R3.a 执行记录：旧键前驱定位的完整成本轴（2026-11-18）

**性质**：计量补差，**恢复行为零改**。写面＝`assembly/read.go`（`GetParent` 成本断言归真，一行注释）、`assembly/read_test.go`（三枚新锚＋夹具 helper `commitParentFact` 提取）、本域 `design.md`／`tasks.md`。引擎零改：既有计数面 `IndexWalkReads()`（`segment_store.go:1070`）已同时覆盖两段——`indexWalkVisits` 在每次 visit **之前**累加（`:1250`／`:1286`），故「最新→目标」与「目标→前驱」两侧同计入。据此**不需新引擎接口**，未触白名单外的共享面冻结程序。零 git 写。

### 二十九·一 两段成本轴（实测，固件判据）

夹具 `parentOldTargetTier`：同链三事实（`chain-root`→`chain-mid`→`chain-target`，分区 11／会话 `session-chain`）先落，随后落「目标之后」的 `later-*` 背景事实（分区 10／会话 `session-history`）；**目标与前驱间距恒为 1 条目**，唯一变动量＝目标之后的历史。

| 目标之后历史 | 旧目标 named | 同档最新事实 named |
|---|---|---|
| 16 | **18** | 2 |
| 64 | **66** | 2 |
| 256 | **258** | 2 |

⇒ `named = 之后历史 + 2`（目标本身＋前驱），与之后历史 **1:1**；同 store 同档「最新事实」的查找恒为 **2**。两者同夹具同命令并列，故「只量后一段」的读法（恒 2）无法解释 18/66/258。

**§二十六·三 该行由此改口径**（原行只写段乙，读来像「旧键常数」）：`GetParent` 成本＝**【段甲】**最新记录→目标键的提交数（＝目标之后的历史）＋**【段乙】**目标键→同链更旧首条目的提交数（跨分区、跨会话被跳过的条目都算）。**段甲随「目标之后的历史」线性**，是旧键定位的真实成本；§二十六·三／·四「不随前驱之后（更旧）的历史增长」仍成立且仅覆盖段乙。

### 二十九·二 原生成本 vs 可消除扫描（独立审查用）

- **段甲＝现布局本征**。引擎只声明「从最新提交倒走」的序（`CommitOrder`／`indexKeyOrder` → `journal_kv.go:279` `eachCommittedNewestFirst` → `log.eachKeyNewestFirst`），**无按键位置起点**的可寻址走查；可寻址点读（`{pid}:idx:{key}`，`segment_store.go:543`）能 O(分区数) 答出目标自身的位点与会话，但**更旧条目只能从最新倒走才可达**，故点读不能消去段甲。修复面仍是 §二十六·四 登记的「引擎按起点寻址」，超本叶白名单，未动。
- **段乙＝不可消**：隔离要求把跨分区／跨会话的条目走过，跳过的条目都在账上（负锚 named=5＝目标＋三条被跳过＋前驱）。
- **可消除但本叶未取**：索引不带会话的回退 `parentFromBodies`（`read.go:325`）在快路已定位到键之后**再从最新倒走一遍**（`read.go:343`），段甲因此付**两次**。一次性 overlay 探针（**未入树**，夹具可重建）：目标身后 64 条新历史时回退 named=**132**（＝2×64＋4），同形状快路 named=**69**；树内负锚的同路径读数为 `fallback-lookup-named=4`（该夹具无更旧历史，故只有 1＋3 两段可见）。**不改理由**：只影响索引不带会话的旧形状，且合并两趟走查要重排快路与回退的边界，未见第二个使用方要求；登记为后续可修项，由审批决定是否立叶。
- **链根无前驱**：本征整读（§二十六·三 原行保持，未改）。

### 二十九·三 三锚（原始退出码，非管道退出码）

| 命令 | 退出码 | 读数 |
|---|---|---|
| `go test ./assembly -run '^(TestParentLookupCountsTheSearchBeforeTheTarget\|TestParentLookupKeepsSessionIsolation)$' -count=10 -v` | **0** | 20 PASS；18/66/258 与 named=5、fallback=4 逐跑复现 |
| `go test ./assembly -run '^TestRecallTraceThroughTierCountsOldTargetLookup$' -count=10 -v` | **0** | 10 PASS；旧目标 named=**398**，最新 named=**2** |
| `go test ./assembly -run '^(TestTierParentSurvivesDurableReopen\|TestRecallPortLateBindsParentReader)$' -count=10 -v` | **0** | 20 PASS（101.078s） |
| `go test ./tool/recall -run '^TestTraceOfDepthDPaysDLocalLookupsNotDHistoryWalks$' -count=10 -v` | **0** | 10 PASS |
| `go test ./assembly -count=1` | **0** | `ok … 337.185s`，零具名红 |
| `go build ./...`／`go vet ./assembly ./tool/recall ./memory/ledger`／`gofmt -l assembly tool/recall memory/ledger` | **0**／**0**／空 | — |

**可红演示（overlay 突变，主树零改，原始退出码 1）**：

- 只量后一段（`segment_store.go` 的 `indexWalkVisits` 改为仅 visit 答 false 时累加）：正锚 exit **1**，红文 `beside 16 facts newer than the target the lookup named 1 index entries and the newest lookup named 1, want 18 and 2`；消费者锚 exit **1**，红文 `the old target's walk of three steps named 2 index entries beside 128 facts newer than it, want 398`。
- 跨会话合并（`read.go` 去掉 `sess != session` 判据）：负锚 exit **1**，红文 `the lookup joined "iso-b2" to iso-a2: a foreign session of its own partition or a foreign partition of its own session is never the parent`。

### 二十九·四 消费者锚：正式 recall_trace ＋ 真实 Tier

- **路径与生产同构**：`NewRecallPort(nil)` → `Bind(run)` → `port.Reads()`（`reads.Table=port`）→ `recall.New(reads, memory.PartitionsOf("agent-a", nil))` → 取 `recall.TraceToolName` → `Call`，与 `hostDocument.toolsFor`（`host_document.go:775-782`）同一构造。**未用替身恒定 `GetParent`**（`tool/recall` 的 `TestTraceOfDepthDPaysDLocalLookupsNotDHistoryWalks` 用恒等替身，只量工具层，本叶不据此放行引擎成本）。
- **读数**：Tier＝`StorageLocalFile` 真实根；history=128、链 3 条、另有 2 条同分区更新事实 ⇒ 三步走查 named=**398**＝3×(128+2)+8（每步都付段甲，第一步另付目标＋前驱 2 条，其后两步各 3 条）；同一 store 的最新事实一次 step named=**2**。**D=3 而 visits=398**，即工具层「D 次本地查找」与引擎完整成本是两个数，前者不能冒充后者。
- 答案面同时校验：`count=3`、`complete=true`、`items` 逐项＝`old-target`→`old-mid`→`old-root`（走查跟链不跟周围历史）。

### 二十九·五 未做／未证（如实）

- `-race` 本叶未跑（三锚均无并发共享态；负锚的 4 与正锚的 18/66/258 为确定性计数）。
- 段甲的消除（引擎按起点寻址）**未原型、未计量**，只登记修复面。
- 回退「段甲付两次」的树内读数是对应夹具形状的（无更旧历史）；132 那个数来自一次性 overlay 探针，**未入树、不可由树内命令复跑**。
- `gofmt`／`vet` 只覆盖三个被读包；`go test ./tool/...` 全包未跑，只跑 `./tool/recall` 单锚（派发令点名面）。
