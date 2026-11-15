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
- **静默降级**：`replayRef`（assembly/run.go:2889）在 :2892 `if !held { return event.CompressProduct{}, false, "", nil }` 静默跳过；同一存储的 `eachCommittedEvent`（run.go:2315-2317）、`eachFactPastCut`（checkpoint.go:1003-1005）、`EachEvent`（segment_store.go:608-610）对同一缺失具名 `ErrSegmentBodyMissing`。两条读路径对同一事实给两种答案，违 session-checkpoint-material/spec.md:31-34「恢复与运行期派生一致」。

## What

承 D1 P4 与 D7（保序陷阱 / 静默降级），本域把开档与索引从「全量重建」改为「派生加载 + 尾增量」：

- **索引派生加载**：开档读一份**索引切片**（存活键集 key→span、帧序、已提交计数、window meta、切面偏移），不再从偏移 0 全量重建索引。切面是派生载体，可由链重建，缺失或损坏时具名告警并退回全扫。
- **尾增量索引**：`journalReplay` 从切面偏移起重放尾；`Load` 只补尾（seq > Offset）的 idx/meta。常驻 index/order/windows 三面有界于存活键集 + 尾。
- **静默跳页改响亮失败**：`replayRef` 的 `!held` 分支改具名 `ErrSegmentBodyMissing`，与三条兄弟读路径同口径。
- **保序不变量**：帧序按帧记，任何裁剪或重写存活键集须**逆帧序去重**后反写，既有两枚保序测试保持绿。
- **年轻链兜底保留**：无切面、无 checkpoint、从未折叠的链仍走全扫，结果与派生路径逐条一致；兜底不是旁路，是同一 g(x) 的退化输入。
- **计数语义不变**：`EventCount`（segment_store.go:584）仍是全部已提交事件数，由切面携带的标量派生，既有计数断言零改。

## Capabilities

- `bounded-recovery`（ADDED）：本域新增「开档/索引有界于存活键集 + 尾」「索引规模不随历史线性增长」「缺页响亮失败」「年轻链全扫兜底仍一致」「帧序保序」五条要求。
- **共享能力面分工**：04 亦写 `bounded-recovery`（四真量探针 / 多代多任务夹具 / S2 锚 / 全史读负锚）。本域只写开档与索引的行为要求，度量面定义归 04；两份 delta 由编排者按 R17 合并，本域不定义四量签名。

## 边界与依赖

- **依赖**：§五冻结的 存活 权威（`live = checkpoint.Survivors ∪ {ref : ref.seq > Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)`，design.md:84）；Stateful 接缝（runtime/state/checkpoint.go:36-46）；checkpoint 存活键集切片（assembly/checkpoint.go:66 `projection`、:1039-1044 `projectionSliceSession`）；`AtomicStore` 缝（memory/ledger/atomic_store.go）；`commitOrder` 引擎能力先例（segment_store.go:224-227）。
- **合单原子（W1）**：与 01（task-custody）、04（acceptance-real-measure）同触 boot 路径簇（assembly/run.go 的 `restoreCheckpointState`:1048 / `seedReplay`:2416 / `seedAnchoredReplay`:2562、memory/ledger/journal.go open、segment_store.go Load），一级 D4（design.md:45）与一级 tasks.md:32 裁定**必须合单串行，不可并行派发**。
- **被依赖方**：06 退位（本域建立的存活键集索引是搬迁补集的权威输入）、04 真量锚（journalBytes / indexEntries 由本域转绿）、远端 wechat-bot 部署（重启墙钟与磁盘）。
- **接口面**：索引切片的载体层与签名属 §五 共享面（R17 编排者单写）。一级 D1 反事实（design.md:20）写「checkpoint 已带存活键集索引」，实证在引擎层不成立，本域 design.md §二列候选与倾向并设**设计闸门**：编排者冻结前 2.1 不动工。
- **禁止事项**：
  - **SHALL NOT 删除、覆盖或改写已提交事实**（openspec/specs/session-checkpoint-material/spec.md:47；一级 design.md 引作 spec.md:44，行号勘误为 :47）。journal.log 保持只追加，切面只写独立派生载体。
  - **保序**：不得改帧序的相对顺序；不得以键排序替代帧序（`commitOrderOf` 的排序分支只服务无 `commitOrder` 能力的引擎，segment_store.go:414-424）。
  - 不触外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）。
  - 不改写冻结归档（openspec/changes/archive/** 只附勘误）。
  - 不触 config-surface-normalization 未提交变更（同 dev 分支，独立 change）。
  - 不读 examples/wechat-bot/.wechat-config/data。
  - 不新增恢复旁路、不改 `AtomicStore` 四方法、不新增词汇（沿用 checkpoint / 切片 / 安静点 / 索引 / 尾 / g(x) / 存活键集 / 切面 / 帧）。
  - 本文档阶段**只规划不实施**。