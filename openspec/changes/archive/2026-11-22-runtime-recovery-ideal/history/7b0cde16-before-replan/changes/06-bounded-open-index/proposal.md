# 02 有界开档/索引（boot 路径簇合单域）

## Why

一级 [proposal.md](../../proposal.md) Why 2「journal 开档全读 + 索引全量物化」与 [design.md](../../design.md) D1 P4：只有**正文读**有界，开档 I/O、索引条目、常驻内存三面仍 O(全史)。S2 症状（§六 design.md:93）为共享锚，本域只引用不重定义。

实证（✅ grep + 读码，行号为当前 dev 工作树）：

- **开档走整份文件**：`openJournalLog`（memory/ledger/journal.go:471）在 :490 调 `journalReplay(file, size, log.publish)`；`journalReplay`（journal.go:433）的 `offset := int64(0)` 起 `for offset < size` 逐帧读完。journal.go:40-42 明载 journal.log「appended to and never rewritten」，:495-499 的 `Truncate` 只切撕裂尾，不做任何有界化。
- **帧序永不回收**：`order []string`（journal.go:121）由 `noteOrder`（journal.go:534-539）每帧每键追加，一键在每条触碰它的记录里都被点名；`eachKeyNewestFirst`（journal.go:546-558）逆序走它。常驻帧序规模＝历史触碰键次总数。
- **索引全量物化**：`FileSegmentStore.Load`（memory/ledger/segment_store.go:281）全量读 `{pid}:idx:` 前缀建 positions map（:336），:357-364 全量过滤出 published，:365 `commitOrderOf` 走全 order；`EachIndexNewestFirst`（segment_store.go:709-729）流全 order。
- **四处消费者都先物化再过滤**：`restoreSessionCheckpoints`（assembly/checkpoint.go:865）、`eachFactPastCut`（assembly/checkpoint.go:985）、`seedAnchoredReplay`（assembly/run.go:2565）、`eachCommittedEvent`（assembly/run.go:2304）各自把全 order 收进 `refs []anchorRef` 后才按类型/切面过滤。过滤省的是正文读，不是索引规模与开档 I/O。
- **契约违背**：checkpoint-state delta :15「读取成本 SHALL 不随累积历史线性增长」、:23「boot 恢复成本 SHALL 由最近 checkpoint 与偏移后增量决定」；window-recovery delta :75「一次恢复解码的事件数 MUST NOT 与历史事件总数成正比」。⚠️ 实证勘误：这两份 delta 只存在于冻结归档（`openspec/changes/archive/2026-11-11-hot-update-and-purity-closure/changes/{12-checkpoint-state,06-window-recovery}/specs/`），`openspec/specs/` 下无同名能力，转正归 05；本域引用其 SHALL 原文，不改写归档。
- **既有锚量错面**：`TestSegmentLoadLazyPeak`（memory/ledger/segment_store_test.go:222-244）只钉重开档 heap 增长 ≤4MiB（正文不驻留），`TestSymptomAnchorSessionBootCostFlatAcrossGenerations`（assembly/checkpoint_test.go:1231）只量正文计数。开档字节 / 索引条目 / 帧序规模三面全在锚外。
- **静默降级**：`replayRef`（assembly/run.go:2889）在 :2892 `if !held { return event.CompressProduct{}, false, "", nil }` 静默跳过；同一存储的 `eachCommittedEvent`（run.go:2315-2317）、`eachFactPastCut`（checkpoint.go:1003-1005）、`EachEvent`（segment_store.go:608-610）对同一缺失具名 `ErrSegmentBodyMissing`。两条读路径对同一事实给两种答案，违 `session-checkpoint-material` 的 Scenario「恢复与运行期派生一致」。

## What

承 D1 P4 与 D7（保序陷阱 / 静默降级），本域把开档与索引从「全量重建」改为「派生加载 + 尾增量」：

- **索引派生加载**：开档读一份**索引切片**（存活键集 key→span、帧序、已提交计数、window meta、切面偏移），不再从偏移 0 全量重建索引。切面是派生载体，可由链重建，缺失或损坏时具名告警并退回全扫。**口径修订（本轮）**：切面的维护 SHALL NOT 只发生在退位重写里（见「本轮重整」节）——切面是引擎自身的常规派生载体，退位只是它的一种触发场景。**勘误（2026-11-16，退位机制整体删除）**：末句的退位触发场景作废——切面维护不依赖任何搬迁；「切面维护是引擎自身职责」这一判据不变，其现行形态待停整篇重放（S3a）定稿后由本域 spec 重述。
- **尾增量索引**：`journalReplay` 从切面偏移起重放尾；`Load` 只补尾（seq > Offset）的 idx/meta。常驻 index/order/windows 三面有界于存活键集 + 尾。
- **静默跳页改响亮失败**：`replayRef` 的 `!held` 分支改具名 `ErrSegmentBodyMissing`，与三条兄弟读路径同口径。
- **保序不变量**：帧序按帧记，任何裁剪或重写存活键集须**逆帧序去重**后反写，既有两枚保序测试保持绿。（**勘误 2026-11-16**：末句的「裁剪或重写存活键集」随退位删除而消失——日志只追加，无裁剪；帧序「最新在后、逆序走访先遇最新」的不变量不变。）
- **年轻链兜底保留**：无切面、无 checkpoint、从未折叠的链仍走全扫，结果与派生路径逐条一致；兜底不是旁路，是同一 g(x) 的退化输入。
- **计数语义不变**：`EventCount`（segment_store.go:584）仍是全部已提交事件数，由切面携带的标量派生，既有计数断言零改。

## Capabilities

- `bounded-recovery`（ADDED）：本域新增「开档/索引有界于存活键集 + 尾」「索引规模不随历史线性增长」「缺页响亮失败」「年轻链全扫兜底仍一致」「帧序保序」五条要求。
- **共享能力面分工**：04 亦写 `bounded-recovery`（四真量探针 / 多代多任务夹具 / S2 锚 / 全史读负锚）。本域只写开档与索引的行为要求，度量面定义归 04；两份 delta 由编排者按 R17 合并，本域不定义四量签名。

## 边界与依赖

- **依赖**：§五冻结的 存活 权威（`live = checkpoint.Survivors ∪ {ref : ref.seq > Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)`，design.md:84）；Stateful 接缝（runtime/state/checkpoint.go:36-46）；checkpoint 存活键集切片（assembly/checkpoint.go:66 `projection`、:1039-1044 `projectionSliceSession`）；`AtomicStore` 缝（memory/ledger/atomic_store.go）；`commitOrder` 引擎能力先例（segment_store.go:224-227）。
- **合单原子（W1）**：与 01（task-custody）、04（acceptance-real-measure）同触 boot 路径簇（assembly/run.go 的 `restoreCheckpointState`:1048 / `seedReplay`:2416 / `seedAnchoredReplay`:2562、memory/ledger/journal.go open、segment_store.go Load），一级 D4（design.md:45）与一级 tasks.md:32 裁定**必须合单串行，不可并行派发**。
- **被依赖方**：06 退位（本域建立的存活键集索引是搬迁补集的权威输入）、04 真量锚（journalBytes / indexEntries 由本域转绿）、远端 wechat-bot 部署（重启墙钟与磁盘）。**勘误（2026-11-16，退位机制整体删除）**：首项作废——不存在搬迁，存活键集索引不为任何搬迁补集服务；本域产出改由 04 真量锚与症状锚消费。
- **接口面**：索引切片的载体层与签名属 §五 共享面（R17 编排者单写）。一级 D1 反事实（design.md:20）写「checkpoint 已带存活键集索引」，实证在引擎层不成立，本域 design.md §二列候选与倾向并设**设计闸门**：编排者冻结前 2.1 不动工。
- **禁止事项**：
  - **SHALL NOT 删除、覆盖或改写已提交事实**（`openspec/specs/session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」；旧行号引用 `:44`／`:47` 已漂移，一律改用条款名锚）。journal.log 保持只追加，切面只写独立派生载体。
  - **保序**：不得改帧序的相对顺序；不得以键排序替代帧序（`commitOrderOf` 的排序分支只服务无 `commitOrder` 能力的引擎，segment_store.go:414-424）。
  - 不触外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）。
  - 不改写冻结归档（openspec/changes/archive/** 只附勘误）。
  - 不触 config-surface-normalization 未提交变更（同 dev 分支，独立 change）。
  - 不读 examples/wechat-bot/.wechat-config/data。
  - 不新增恢复旁路、不改 `AtomicStore` 四方法、不新增词汇（沿用 checkpoint / 切片 / 安静点 / 索引 / 尾 / g(x) / 存活键集 / 切面 / 帧）。
  - 本文档阶段**只规划不实施**。

## 本轮重整（06.R1 / 06.R2）

一级 proposal「本轮重整」与 design D8–D11 已撤销旧整体完成结论。本域据此重整，旧编号 2.1–2.5 与其实施记录作为**历史凭据保留**，但当前完成状态一律回退为待复验。

### 本轮已核出的口径偏差（✅ 读码）

> **勘误（2026-11-16）**：下表是**规划时点**的口径偏差记录，其判据均以退位与索引切面存在为前提。反转后：第 1 行「切面只在退位重写时落盘」的对象（切面族）已零调用并随 08.R4 删除，开档 `from` 改由槽表表头水位回答；第 3 行「绿借了退位的果」的归因前提消失（退位不存在，热层字节增长已是显式声明的关切）；第 4 行「冷读缓存」无对象（无冷层）。**第 2 行「每个存活键仍整表读写」仍成立**，归 06.R3（store 常驻索引去物化）。原文保留为历史证据。

| 偏差 | 实证 | 影响 |
|---|---|---|
| 切面只在退位重写时落盘 | `journalLog.rewrite`（memory/ledger/journal.go:750-767）是唯一 `writeJournalIndexCut` 调用点；`openJournalLog`（:968）读不到切面即整份读 | 未开启退位时**没有切面**，开档仍是整份 journal → 2.1 的「派生加载」只在退位路径成立 |
| 索引前缀仍是整表读 | `FileSegmentStore.Load`（memory/ledger/segment_store.go:346-401）仍按分区读整份 `{pid}:idx:` 与 `{pid}:meta:` 前缀 | 索引规模随存活键集而非历史（退位后），但**每个存活键仍整表读写**；不是按需/增量 |
| boot 量的「绿」借了退位的果 | 旧 S2 绿日志 `journalBytes=[0 110 110 110 110]`（run-notes.md:15）；退位关时同一命令为 `[0 134331 326123 557274 827666]`（ORCHESTRATION R6） | 「boot 有界」的红转绿由 08 退位把热层变小造成，**不是** 06 索引机制独立兑现；不分开量就无法归因 |
| 冷读缓存不等于常数成本 | `coldViewLocked` 缓存（`segment_store.go:779` 附近）只省重复打开；冷层按身份读仍要读冷层索引并定位 | 「有缓存」MUST NOT 被写成「所有访问 O(1)」 |

### 本域重整后的两叶

- **06.R1 成本与引擎能力设计门（G3，只调查/原型）**：分别计量开档、索引物化、点读、提交、重绑定与总耗时〔**搬迁段已删**：退位机制删除后无搬迁阶段，见上节勘误〕；给出剖面后再选引擎实现（目录保留／增量维护／按需定位）；决定「切面由谁在什么时刻维护」这一签名。G3 未过不派 06.R2。
- **06.R2 按引擎职责实施索引与冷热（依 G3）**：把索引维护下沉为引擎职责（不依赖任何上层搬迁机制触发），点读与枚举的成本口径按 G3 的剖面取舍；**不保证所有操作 O(1)**，只保证「不随历史线性增长的操作集」被具名列出，其余如实标注为自然成本。

### 边界与依赖（本轮增补）

> **勘误（2026-11-16，退位机制整体删除）**：本节「依赖」中的 `journalLog.rewrite` 安静点与「08 域的存活/水位 keep 集合」两项、「被依赖方」中的 08.R1 项、以及上一条禁止事项中的「冷读缓存」「退位掩盖」措辞，均以退位存在为前提，按反转作废：日志只追加、无重写安静点，无搬迁故无 keep 集合可裁。存活条目（G3 计量点、11 域提交键集形状、不宣称 O(1)、不篡改四量口径）不变。

- **依赖**：G3 的计量点（`JournalBytesOpen`/`IndexEntries` 既有只读出口，见 04 域 4.1 的执行勘误）；`journalLog.rewrite` 的安静点与锁（`journalKV.mu`，memory/ledger/journal_kv.go:105）；11 域的整事实提交键集形状（决定切面要覆盖哪些族）；08 域的存活/水位 keep 集合（决定切面可以裁到多小）。
- **被依赖方**：08.R1（退位与回退、身份读；切面覆盖不足会让退位后的身份读落到全扫）、09 的 S2 四真量（本域提供 `journalBytes`/`indexEntries` 的真实归因，不再借退位掩盖）、11.R2（提交点与切面写入点同在引擎锁内）。
- **禁止事项（本轮增补）**：不宣称「所有访问 O(1)」；不把冷读缓存命中当作 boot 有界证据；不在消费者里特判 journal 文件布局；不做就地格式迁移（`migrate-kv` 先完整读取、单独迁移、源只读，本轮不执行迁移）；不篡改 P4 的四量口径把提交/退位塞进「启动」。