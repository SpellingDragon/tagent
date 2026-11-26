# 01.101 增长构成实测（evidence）

- 基线：`cee4eb66297be36bc4f6e8e0423deb70c6a29af3`（开工 `git rev-parse HEAD` 重核，与派发单一致）
- 写面：仅本文件。探针脚本与运行根均在仓外 `/tmp/bhl101/`，未改任何 `.go`，未触归档目录与未跟踪面
- 本文件的所有数字均来自下文【二 原始输出】里的命令原文；不确定项在【六】逐条标注 ❓

## 〇 结论先行

1. **正文存两处**：同一条事实的正文既在事件体帧内（`<pid>:evt:<window>:<seq>` 的 `message.content`），又在 `blob:<事件键>` 里逐字节重复；两处解码后 8,192 B 全等（见【四】）。按 D1 判据，归档设计前须先立删除项，不得把冗余带进归档单位。
2. **正文占 95.87% 的帧字节**（双份合计 25,805,320 B / 26,901,360 B）；每事实固定开销 899 B（2.56%），事件外壳 406 KB（1.51%）。正文单份化可直接减半 journal（−12,901,876 B，−47.96%）。
3. **cut cadence**：间隔 cut 由提交数阈值主导，实测每次恰 64 提交（572–701 ms），`CheckpointPeriod`(30 s) 在 16 次 cut 中零次触发；关档 cut 0 提交（4.8–16.0 ms）。
4. **开档校验基线**：boot 逐帧 CRC 覆盖**整个 journal**（26,918,264 B，热缓存 5.08–5.25 ms，≈5.1 GB/s）；索引重放窗口为 0 B（本负载每代都干净关档）。热卷字节随代线性增长（6.84 → 13.95 → 21.16 → 28.10 MB），无归档即无平台。

## 一 方法与口径

### 1.1 被测生产路径（未使用测试钩子）

| 环节 | 生产入口 | 位置 |
|---|---|---|
| 开档（boot） | `ledger.OpenTier` → `openJournalView` → `openJournalLogFace`（全日志完整性 pass） | `memory/ledger/config.go:282`、`memory/ledger/journal_kv.go:56`、`memory/ledger/journal.go:661`、`memory/ledger/journal.go:693` |
| 提交一条事实 | `ledger.CommitFact` → `planEventLocked`（evt/idx/meta/seat 四键）+ `planFactLocked`（blob/ref 两键），一批一帧 | `memory/ledger/segment_store.go:791`、`memory/ledger/segment_store.go:613`、`memory/ledger/kv_original_store.go:300` |
| cut 的安静点 | `AppendOrdered`（cut 唯一时点的 marker，无独立 barrier） | `memory/ledger/journal_kv.go:243`、`assembly/checkpoint.go:445` |
| cut 的 checkpoint 事实 | `event.CheckpointFor` → `event.CheckpointPayloadOf` → 同一 `CommitFact` | `api/event/checkpoint.go:113`、`assembly/checkpoint.go:532` |
| 读回（消费者锚） | `tier.Segments.GetEvent` + `tier.Originals.Get` | `memory/ledger/segment_store.go:867`、`memory/ledger/reference.go:177` |
| 开档测量 | `IntegrityBytesScanned` / `IntegrityPassSpan` / `JournalBytesOpen` / `DirectoryMaintenancePasses` | `memory/ledger/journal.go:2723`、`journal.go:2771`、`journal_kv.go:144`、`journal.go:2784` |

### 1.2 合成负载

- **4 代**（N=4 ≥ 3 个 cut 周期）：每代 = 一次真实开档 → 提交 → 关档，代间不迁移、不删字节（现网形态）。
- **每代 4 次 cut**：3 次间隔 cut（由 64 提交的 cadence 触发）+ 1 次关档 cut（`fileClosingCheckpoint` 的等价时序）。
- **事实体（正文）大小混合**：每 10 条中 1 条 64 KiB、1 条 1 KiB，其余 8 KiB；正文 = `result-<序号>|` + 固定字母表，同一输入同字节。
- **checkpoint 切片**：`projection`(36 条 survivor) + `delivery`(2) + `tasks`(2) + `inputs`(空) + `compress`(空)，payload ≈ 4.5 KB（小于生产的 36 轮 64 KiB 场景，见【六】）。
- **其余族**：每代 3 条配置链事实（`tagent/config/v1/fact:*`）+ 3 条任务账户事实（`tagent/task/v1/fact:*`），各一帧。
- 规模：824 帧 = 768 事实帧 + 16 quiet 帧 + 16 checkpoint 帧 + 24 控制帧。

### 1.3 口径

- **帧字节** = 8 B 头（长度 + CRC32C）+ payload；payload 为 JSON 记录，`puts[].value` 是值的 base64 文本。
- **键族**按 put 的 key 分类，字节 = key token + value token 的 JSON 文本字节（含引号）；`overlay` = payload − 各 put token 之和（`{"version":1,...}` 骨架与逗号）。
- **段界** = 该 cut 的 checkpoint 帧（cut ≡ 段边界，2026-11-23 裁定）；**段字节** = 上一段界之后至本段界（含本 cut 的 quiet 帧与 checkpoint 帧）。
- **开档校验** = open 的完整性 pass：字节 = `IntegrityBytesScanned`，耗时 = `IntegrityPassSpan` 两端时钟差。

## 二 原始输出

```sh
# 编译与两次运行（同一输入两棵树）
cd /Users/pengweiye/Documents/codes/tagent
go build -o /tmp/bhl101/probe /tmp/bhl101/probe.go
/tmp/bhl101/probe /tmp/bhl101/rootA /Users/pengweiye/Documents/codes/tagent
/tmp/bhl101/probe /tmp/bhl101/rootB /Users/pengweiye/Documents/codes/tagent
/tmp/bhl101/probe -boot /tmp/bhl101/rootA /Users/pengweiye/Documents/codes/tagent 5
du -sk /tmp/bhl101/rootA /tmp/bhl101/rootA/kv
ls -l /tmp/bhl101/rootA/kv
```

探针源码：`/tmp/bhl101/probe.go`，sha256 `518980b06eb558b43de790850cad2bd1b8ef0648a2961115c3632e6bd444db70`（843 行；按派发单只落 /tmp）。cadence 常量与键前缀由探针**运行时从源码读出**（`assembly/checkpoint.go`、`api/event/checkpoint.go`），不是手抄。

`runA` 原文（`/tmp/bhl101/rootA`）：

```
== cadence constants (assembly/checkpoint.go CheckpointCommitInterval/CheckpointPeriod) ==
CheckpointCommitInterval=64  CheckpointPeriod=30000000000ns

== boot integrity baseline (per open) ==
open           log_before integrity_bytes    verify_ms   index_open passes
gen1                    0              0        0.014            0      2
gen2              6547428        6547428        3.570            0      0
gen3             13363196       13363196        8.352            0      0
gen4             20064272       20064272       15.040            0      0
final-read       26918264       26918264        5.437            0      0

== key family bytes ==
evt              13307812   49.47%  puts=784
blob             12916867   48.02%  puts=784
ref                274471    1.02%  puts=784
windowmeta         137984    0.51%  puts=784
overlay            102696    0.38%  puts=0
idx                 96340    0.36%  puts=768
seat                59548    0.22%  puts=784
other                2640    0.01%  puts=24
checkpoint           2043    0.01%  puts=16
quiet                 959    0.00%  puts=16
TOTAL            26901360

== content split (正文体 vs 每事实固定开销) ==
decoded content: in evt=9675391  in blob=9675391  (equal per fact)
on-disk content: copy1(blob value token)=12903444  copy2(inside evt, base64 4/3)=12901876
evt envelope=405936 (1.51%)  fixed per-fact cost=690104 (2.56%)  content copies=25805320 (95.87%)
facts=768  av. content/fact=12598  av. fixed/fact=899  av. total/fact=35050

== hot dir bytes per generation (kv dir) ==
gen1        6841418
gen2       13952270
gen3       21162126
gen4       28098914

== per-cut segments ==
gen  cut  reason     commits   frames elapsed_ms        bytes
1    1    interval        64       66 572.119417      2189790
1    2    interval        64       66 658.291209      2151866
1    3    interval        64       66    586.629      2190300
1    4    close            0        2   7.505625        14512
2    5    interval        64       72 671.889041      2305956
2    6    interval        64       66 637.238333      2190304
2    7    interval        64       66 652.209459      2304996
2    8    close            0        2   5.717584        14512
3    9    interval        64       72 701.598917      2191260
3   10    interval        64       66 593.265625      2305000
3   11    interval        64       66 615.094334      2190304
3   12    close            0        2   4.843291        14512
4   13    interval        64       72 685.804166      2325068
4   14    interval        64       66 626.337333      2190300
4   15    interval        64       66 564.475625      2324112
4   16    close            0        2  16.002667        14512

== cut frames ==
gen=1 cut=1 interval  quiet=109B checkpoint=14390B
gen=1 cut=2 interval  quiet=110B checkpoint=14402B
... （cut3–cut15 均为 quiet=110B checkpoint=14402B）
gen=4 cut=16 close     quiet=110B checkpoint=14402B

== redundancy ==
map[body_content_bytes:8192 equal:true event_key:fact-01:0001 event_partition:887
    event_seq:1 event_window:1767225600007 original_content_bytes:8192]
[map[blob_key:blob:fact-01:0001 evt_key:887:evt:1767225600007:1 frame_bytes:23226 frame_offset:0]
 map[blob_key:blob:fact-01:0002 evt_key:887:evt:1767225600014:2 frame_bytes:23226 frame_offset:23226]
 map[blob_key:blob:fact-01:0003 evt_key:887:evt:1767225600019:3 frame_bytes:23226 frame_offset:46452]]
```

`-boot` 连开 5 次（同一棵 26.9 MB 的树，测开档校验的稳态）：

```
open integrity_bytes    verify_ms   index_open passes
1          26918264        5.230            0      0
2          26918264        5.248            0      0
3          26918264        5.218            0      0
4          26918264        5.173            0      0
5          26918264        5.080            0      0
```

热目录落盘（`ls -l /tmp/bhl101/rootA/kv`、`du -sk`）：

```
-rw-------  1 pengweiye  wheel  26918264  journal.log
-rw-------  1 pengweiye  wheel     65536  order.line
-rw-------  1 pengweiye  wheel    328618  order.log
-rw-------  1 pengweiye  wheel    786496  slots
28140	/tmp/bhl101/rootA
28140	/tmp/bhl101/rootA/kv
```

## 三 数字

### 3.1 键族字节（runA，TOTAL 26,901,360 B）

| 族 | 字节 | 占比 | put 数 | 键形 |
|---|---|---|---|---|
| `evt`（事件体） | 13,307,812 | 49.47% | 784 | `<pid>:evt:<window>:<seq>` |
| `blob:`（正文副本） | 12,916,867 | 48.02% | 784 | `blob:<事件键>` |
| `ref:`（引用记录） | 274,471 | 1.02% | 784 | `ref:<事件键>` |
| `windowmeta`（窗口元） | 137,984 | 0.51% | 784 | `<pid>:meta:<window>` |
| `overlay`（记录骨架） | 102,696 | 0.38% | — | `{"version":1,"puts":[...]}` |
| `idx`（事件索引） | 96,340 | 0.36% | 768 | `<pid>:idx:<事件键>` |
| `seat`（存储座） | 59,548 | 0.22% | 784 | `store:seat`（覆盖写） |
| `other`（配置链/任务账户） | 2,640 | 0.01% | 24 | `tagent/config/v1/fact:*`、`tagent/task/v1/fact:*` |
| `checkpoint:`（该族索引 put） | 2,043 | 0.01% | 16 | `<pid>:idx:checkpoint:<pid>:<seq>` |
| `quiet:` 标记 | 959 | 0.00% | 16 | `tagent/checkpoint/v1/quiet:<pid>:<seq>` |

按 tasks 的七族归并（同一测值，不重算）：

| tasks 族名 | 组成 | 字节 | 占比 |
|---|---|---|---|
| 事件键 | `evt` + `idx` | 13,404,152 | 49.82% |
| window 元键 | `windowmeta` | 137,984 | 0.51% |
| `blob:` | `blob:` | 12,916,867 | 48.02% |
| `ref:` | `ref:` | 274,471 | 1.02% |
| `checkpoint:` | checkpoint 索引 put（正文在 evt/blob 族内，见 3.5） | 2,043 | 0.01% |
| `quiet:` 标记 | quiet put | 959 | 0.00% |
| 其余 | `seat` + `other` + `overlay` | 164,884 | 0.61% |

### 3.2 三分：正文副本 / 事件外壳 / 固定开销

| 部分 | 字节 | 占比 | 口径 |
|---|---|---|---|
| 正文副本 1（`blob:` value token） | 12,903,444 | 47.96% | 值的 base64 文本 |
| 正文副本 2（`evt` 内 `message.content`） | 12,901,876 | 47.95% | 内容长度 ×4/3（base64 膨胀），边界误差 ≤4 B/put |
| 事件外壳（`evt` 除内容外） | 405,936 | 1.51% | `evt` − 副本 2 |
| 每事实固定开销 | 690,104 | 2.56% | `idx`+`windowmeta`+`ref`+`seat`+`checkpoint`+`quiet`+`other`+`overlay`+`blob:` 键头 |

每事实：正文 12,598 B、固定开销 899 B、落盘合计 35,050 B（固定开销明细/事实：`ref` 357 B、`windowmeta` 180 B、`idx` 125 B、`seat` 78 B、`blob` 键头 17 B、骨架 ≈133 B/帧、`other`+`checkpoint`+`quiet` ≈4 B）。

正文单份化收益：journal −12,901,876 B（**−47.96%**），每事实落盘 35,050 → 18,251 B。

### 3.3 cut cadence（实测触发）

| 项 | 测值 |
|---|---|
| `CheckpointCommitInterval` / `CheckpointPeriod`（源码读出） | 64 / 30 s（`assembly/checkpoint.go:49`、`:58`） |
| cut 次数 | 16（4 代 × [3 间隔 + 1 关档]） |
| 间隔 cut 的提交数 | **恒为 64**（提交数阈值先到） |
| 间隔 cut 的时间窗 | 564.5–701.6 ms（多数 560–670 ms）→ 8.8–11.0 ms/提交 |
| 关档 cut | 0 提交，4.8–16.0 ms |
| `CheckpointPeriod` 触发次数 | **0 / 16**；按实测速率，period 先到需提交间隔 > 469 ms（30 s ÷ 64），即吞吐 <2.1 提交/秒 |

### 3.4 每 cut 段字节（段界 = 该 cut 的 checkpoint 帧）

| gen | cut | 触发 | 提交数 | 帧数 | 段字节 |
|---|---|---|---|---|---|
| 1 | 1 | interval | 64 | 66 | 2,189,790 |
| 1 | 2 | interval | 64 | 66 | 2,151,866 |
| 1 | 3 | interval | 64 | 66 | 2,190,300 |
| 1 | 4 | close | 0 | 2 | 14,512 |
| 2 | 5 | interval | 64 | 72 | 2,305,956 |
| 2 | 6 | interval | 64 | 66 | 2,190,304 |
| 2 | 7 | interval | 64 | 66 | 2,304,996 |
| 2 | 8 | close | 0 | 2 | 14,512 |
| 3 | 9 | interval | 64 | 72 | 2,191,260 |
| 3 | 10 | interval | 64 | 66 | 2,305,000 |
| 3 | 11 | interval | 64 | 66 | 2,190,304 |
| 3 | 12 | close | 0 | 2 | 14,512 |
| 4 | 13 | interval | 64 | 72 | 2,325,068 |
| 4 | 14 | interval | 64 | 66 | 2,190,300 |
| 4 | 15 | interval | 64 | 66 | 2,324,112 |
| 4 | 16 | close | 0 | 2 | 14,512 |

12 个间隔段：均值 2,238,272 B、min 2,151,866、max 2,325,068。66 帧的段 = 64 事实帧 + quiet 帧 + checkpoint 帧；72 帧的段多 6 帧（代边界上该代的 3 条配置链 + 3 条任务账户事实落在首段）。关档段恒为 14,512 B（quiet 110 B + checkpoint 14,402 B）。

### 3.5 checkpoint 帧与 quiet 标记帧的字节大小（含帧内分解）

| 帧 | 总计 | 分解 |
|---|---|---|
| quiet 标记帧 | 109–110 B | `quiet:` put 61–62 B + 骨架 48 B |
| checkpoint 帧（间隔/关档同形） | 14,390–14,402 B | `evt` 7,460–7,465（checkpoint 正文副本 1）+ `blob:` 6,054–6,055（副本 2）+ `ref:` 357–358 + `windowmeta` 178 + 骨架 138 + `checkpoint:` 索引 125–130 + `seat` 78 |

即：一次 cut 在帧空间里花 14.5 KB，其中 **≈84% 是同一份 checkpoint payload 的两份拷贝**（payload ≈ 4,540 B）。

### 3.6 开档校验基线（全日志逐帧 CRC）

| 开档 | 校验前日志 | 校验字节 | 校验耗时 | 索引重放 | 维护 pass |
|---|---|---|---|---|---|
| gen1（空树） | 0 | 0 | 0.014 ms | 0 | 2（建表 + 扩表） |
| gen2 | 6,547,428 | 6,547,428 | 3.570 ms | 0 | 0 |
| gen3 | 13,363,196 | 13,363,196 | 8.352 ms | 0 | 0 |
| gen4 | 20,064,272 | 20,064,272 | 15.040 ms | 0 | 0 |
| final-read（末态） | 26,918,264 | 26,918,264 | 5.437 ms | 0 | 0 |

- 校验字节 = **整个 journal**（`journalReplay(file, 0, size, …)`，`memory/ledger/journal.go:694`），与 watermark 无关 → boot 成本随历史线性。
- 稳态（连开 5 次、热页缓存）：26,918,264 B 用 **5.08–5.25 ms**，≈5.1–5.3 GB/s。
- 代内那几次（3.57–15.04 ms）是被本代刚写完的脏页挤占的读数，不是稳定基线；两次独立运行给出 3.57–15.04 ms 的散布，只能作上限参考。
- **索引重放窗口恒为 0 B**（`JournalBytesOpen`）：本负载每代都干净关档，index 文件已覆盖到日志末尾。崩溃恢复态的窗口未在本叶测量（见【六】）。

### 3.7 每代热卷字节（现网形态：全部留存）

| 代 | kv 目录字节 | 增量 |
|---|---|---|
| gen1 | 6,841,418 | — |
| gen2 | 13,952,270 | +7,110,852 |
| gen3 | 21,162,126 | +7,209,856 |
| gen4 | 28,098,914 | +6,936,788 |

每代 202 条提交（192 事实 + 4 checkpoint + 6 控制）≈ **35 KB/提交**。无归档时 `hot_bytes` 单调线性，不构成平台（这是 01.102 判据的对照基线，本叶不下判据）。

### 3.8 索引侧文件（帧空间之外，但同在热目录）

| 文件 | 字节 | 增长口径 |
|---|---|---|
| `journal.log` | 26,918,264 | 随帧追加 |
| `order.log` | 328,618 | 一提交一行 ≈ **399 B/提交**（824 行） |
| `slots` | 786,496 | 64 B 头 + 8,192 槽 × 96 B（容量按 50% 装载翻倍） |
| `order.line` | 65,536 | 容量 × 8 B |

热目录文件字节和 28,098,914 B；`du` 报 28,140 KiB（块取整）。**这些结构按历史单调增长，且不在 journal 的帧空间内**——02 域的分段设计要把它们一并纳入“有界”的口径，否则帧空间有界而目录仍无界。

## 四 正文冗余判定（负锚）

**结论：正文存两处。**

同一帧（`fact-01:0001`，journal 偏移 `[0, 23226)`）内并列两条 put：

| 位置 | 键 | 内容 |
|---|---|---|
| journal 字节 30 | `887:evt:1767225600007:1` | `message.content` = 8,192 B |
| journal 字节 11,893 | `blob:fact-01:0001` | 8,192 B 原始正文 |

位置证据（`grep -abo`，同一目录 `/tmp/bhl101/rootA/kv`）：

```sh
grep -abo '"key":"blob:fact-01:0001"' journal.log      # 11893:"key":"blob:fact-01:0001"
grep -abo '"key":"887:evt:1767225600007:1"' journal.log # 30:"key":"887:evt:1767225600007:1"
grep -aoc 'blob:fact-01:0001' journal.log               # 1
grep -aoc '887:evt:1767225600007:1' journal.log          # 1
```

读路径证据（产品自己的两个读面返回同一份字节）：

```
map[body_content_bytes:8192 equal:true event_key:fact-01:0001 event_partition:887
    event_seq:1 event_window:1767225600007 original_content_bytes:8192]
```

该等值性的来源（源码位置）：`assembly/facts.go:483` 把 `memory.BodyFromEvent` 的整份事件体（含 `Message`）写进 `:evt:` 记录，同一调用的 `commitFactNoCadence` 又把 `[]byte(body.Message.Content)` 作为 `FactCommit.Body` 交给 `ledger.CommitFact`（`assembly/facts.go:511`），后者在 `kv_original_store.go:330` 写成 `blob:<事件键>`。checkpoint 事实走同一条路（`assembly/checkpoint.go:532` → `CommitFact`），所以 checkpoint payload 也是两份（3.5 已量化）。

覆盖面：768/768 事实帧与 16/16 checkpoint 帧都是这个形态（`blob` put 数 784 = 768 + 16，逐帧成对出现，见 3.1）。

按 D1 判据：正文不是“只存一份”，归档单位不能直接取“帧+其正文”而不先单一化。按 D5，处置是**先立删除项**（本叶只读取证，不动代码）。

## 五 可搬性结论（按族）

| 族 | 随段走 | 恒热集合 | 依据（实测/结构） |
|---|---|---|---|
| `evt`（事件体） | 是（段界 = cut） | 最新 cut 段 + 开段 | 帧自含；boot 只读索引与尾（本负载 `journal_bytes_open` 恒 0） |
| `idx`（事件索引） | 是 | 同上 | 与 `evt` 同帧写入，移动即整帧移动 |
| `windowmeta` | 是 | 同上 | 每窗口一条，随该窗口的事件同段 |
| `blob:` / `ref:` | 是 | 同上 | 与事件同批同帧（`CommitFact` 一批六键） |
| `checkpoint:`（事实 + 正文 + 索引） | 旧 cut 的随段；**最新与次新 cut 的恒热** | 是 | `newestCheckpointRef`/`restoreCheckpoint` 与 `tailFactKeys` 都只读最新；D6 定的搬迁水位＝倒数第二个 cut |
| `quiet:` 标记 | 旧 cut 的随段；**最新 cut 的恒热** | 是 | `tailFactKeys` 逆序走到最新 marker 才停（`assembly/checkpoint.go:1077`）；marker 在 cut 内，故随段天然满足 |
| `store:seat` | **否** | 恒热 | 单键每提交覆盖写（784 put / 768 提交），最新值即 count/commitSeq/partitions |
| `overlay`（记录骨架） | 随帧 | — | 102,696 B / 824 帧 ≈ 125 B/帧 |
| 索引侧文件（`order.log`/`slots`/`order.line`） | 不属于帧空间，不能按段切 | 今日恒热 | 与 log 同生命周期；399 B/提交 + 容量结构 |

推论（供 02 域取用，不在本叶实施）：

1. 可搬单位是**整帧**：一条事实的六个键在同一帧里，按帧搬即天然成组，不存在跨帧引用。
2. 恒热的下界（本负载实测）= 最新 cut 段 2.15–2.33 MB + 开段（≤ 一个 cut 的提交量）+ `seat`/索引侧文件；即 `hot_bytes` 的平台由 cut 周期与索引容量决定，不由历史长度决定——前提是索引侧文件也能随段收缩（3.8）。
3. 正文单份化会把每段从 ≈2.24 MB 降到 ≈1.16 MB，并把 cut 帧从 14.4 KB 降到 ≈8.3 KB。

## 六 不确定项与口径边界（❓）

1. ❓ **`ref:` 族字节不完全可复现**：`kv_original_store.go:320` 的 `StoredAt: time.Now()` 以 RFC3339Nano 文本入值，末位小数位数随纳秒变。runA/runB 两次独立运行：`ref` 274,471 vs 274,479 B（差 8 B），TOTAL 26,901,360 vs 26,901,368；其余九族**逐字节相同**，段字节差 ≤4 B 且同源于此。表内的百分比按 runA 计，抖动 <0.00004%。
2. ❓ **cut 的触发是谓词复算，不是 filer 实跑**：`noteCheckpointCommit`（`assembly/checkpoint.go:580`）需要一个装配好的 `Run`（含 model provider），不在本叶能力面内。本叶用生产常量 + 同一谓词（`commits >= 64 || since(at) >= 30s`）在真实提交时间流上复算，因此“提交数阈值先到”这一结论可信，但“filer 在真实 agent 负载下的睡眠/合并行为”未实测。
3. ❓ **开档校验耗时只有热缓存读数**：26.9 MB / 5.1 ms 是连开 5 次的中位数；冷缓存需 `purge`（本机需特权，未执行），冷盘量级只按顺序读 IO 估算，未实测。
4. ❓ **崩溃恢复态的索引重放窗口未测**：本负载每代都 `Close`（`Segments.Close` → 空批 commit → watermark 前移），故 `JournalBytesOpen` 恒 0；非干净退出下的窗口（`journalLogSyncBytes` = 64 KiB 上限）本叶无数据。
5. ❓ **负载规模小于生产最坏态**：checkpoint payload 4.5 KB（生产注释里的 36 轮 × 64 KiB 场景）、事实体最多 64 KiB（生产 `journalMaxFrameBytes` 32 MiB）。正文/固定开销的**比例**由本负载的形状决定，绝对字节随正文体线性放大；固定开销 899 B/事实与正文无关，可直接外推。
6. ⚠️ **`passes=2` 出现在空树首开**（建表 + 扩表），后续各次开档均为 0——说明本负载的槽表在一次运行内即稳定；`slots` 786,496 B 对应 8,192 容量，是装载率翻倍的结果，不是每代重建。

## 七 对 01.102 / 01.103 的输入（本叶只提供测值，不下判据）

- 平台判据的**分母**：本负载每代 196 提交 / 6.84–7.21 MB 增量；每 cut 段 2.15–2.33 MB；这两组数是 01.102 写“窗口内提交字节”时可引用的实测分母（不得用假设值）。
- 恒热集合的**组成**（D2 需要）：最新 cut 段 + 开段 + `store:seat` + 索引侧文件（`order.log`/`slots`/`order.line`），并在 02 域决定索引侧文件的收缩方式，否则 §3.8 的单调增长会把“热盘有界”打穿。
- 归档单位（D1/D3 需要）：正文两处冗余 → 先立删除项再定“帧+正文”单位；`checkpoint:` 族的正文与副本同理。
- 开档校验口径（D3 需要）：boot 的实算量 = 整个 journal（实测 26.9 MB / 5.1 ms 热缓存），故“按段校验、下界＝热窗字节”是唯一能把该成本压到有界的路径；索引重放窗口在本负载为 0，不能作为 boot 成本的替代口径。
