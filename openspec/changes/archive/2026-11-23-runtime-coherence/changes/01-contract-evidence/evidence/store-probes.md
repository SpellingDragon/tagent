# 存储疑点证据（01.101 · W1-B 存储取证单）

本文件只记两个存储疑点的取证与判定（D1 表第 3、4 行），对照派单原文逐项给出结论、命令、关键输出与 file:line。两项均为**已复现**，无驳回项。

## 基准与方法

- 仓库：`/Users/pengweiye/Documents/codes/tagent`，分支 `dev`，开工复核 `git rev-parse HEAD` = `52d7b664521554fed8d7a5674ee1f934c410a3e3`（与候选一致）。仓库生产代码**零改动**；本文件是本单唯一仓库写面。
- 一切测量与注入运行在 `/tmp/w1b-copy` 干净副本（`git archive HEAD` 解包，只含跟踪文件）：探针测试文件与插桩只存在于副本，列表：
  - `/tmp/w1b-copy/memory/ledger/w1b_probe_instrument.go` —— 副本专用 slot 访问计数器 `w1bSlotsVisited`。
  - `/tmp/w1b-copy/memory/ledger/journal.go` 两行插桩（`findRecord` 与 `eachRecordOnce` 循环内各一行计数；不改控制流）。
  - `/tmp/w1b-copy/memory/ledger/w1b_probe_test.go` —— 疑点 A 计时/计数探针＋疑点 B 机制探针（包内白盒）。
  - `/tmp/w1b-copy/assembly/w1b_probe_test.go` —— 疑点 B 主探针（`GetParent` vs 全序走查）。
- 环境：darwin 23.6.0 / arm64，go1.27.1（go.mod 声明 1.24.1），负载机单进程。计时为取证测量**非计时门**：绝对值随环境噪声漂移，数量级与 slot 访问数（确定性量）是判定依据。
- 退出码取命令自身（`> log 2>&1; echo rc=$?`），不经管道。

```
$ mkdir -p /tmp/w1b-copy && git -C /Users/pengweiye/Documents/codes/tagent archive HEAD | tar -x -C /tmp/w1b-copy
$ cd /tmp/w1b-copy && go vet ./memory/ledger/ ./assembly/
rc=0
```

## 疑点 A · 已知完整键走 Scan，无点读 —— 已复现

**结论：已复现。** `FactLog` 契约只有 `Put/Scan/Sync`（`api/durable/fact_log.go:24-32`），两个消费者以 `Scan(完整键)` 做点读：`runtime/state/durable.go:807-816` 的 `readReceiptLocked`（`d.log.Scan(key)`）、`features/evolution/governance/denial.go:644-666` 的 `governanceTail`（cut>0 分支逐 seq `log.Scan(完整键)`，:655）。底层 `journalKV` 的 `Read` 对 `Fetch.Key` 走点读（`memory/ledger/journal_kv.go:77-83` → `findRecord` 槽表探测，`journal.go:1766-1787`），对前缀走 `eachHeld`（`journal_kv.go:85-92`）——`eachHeld` 经 `eachRecordOnce` **枚举全部槽位**（`journal.go:2128-2154`、`1846-1866`），键数与排序均随表规模走。实测：同族 N 个兄弟键时，`Scan(完整键)` 每次访问 slot 数＝表容量、耗时随 N 增长；`Get` 访问 1–3 个 slot、与 N 无关。且引擎侧点读面已存在（`journal_kv.go:77-83` 的 `fetch.Key` 分支；`TierFacts.Get` 已在 `memory/ledger/tier_facts.go:51-57`），疑点成立且修复不需要新引擎能力。

- 夹具：`OpenTier(StorageLocalFile, Root=t.TempDir(), WriteRights=true)` → `TierFactLog(tier).Put` 写入 N 个同族键（`w1b{N}/fact/%07d`，值 220 B），关 tier 后重开 journal 视图；`Scan(完整键)`＝`kv.Read(Fetch{Prefix: target})`（`TierFacts.Scan` 的内核，`tier_facts.go:36-46`），测 20 轮；`Get`＝每轮重开视图后 `kv.Read(Fetch{Key: target})`（`TierFacts.Get` 的内核）冷读，测 10 轮。target 取族中点键。

- 命令与两轮原始输出：

```
$ cd /tmp/w1b-copy && go test ./memory/ledger/ -run 'TestW1BScanFullKeyVersusGet' -v -count=1
（第 1 轮）
    w1b_probe_test.go:106: N=1000 table_capacity(probed_by_scan)=2048 | Scan(full key): avg=94.573µs slots_visited=2048 pairs=1 | Get(key): avg=18.92µs slots_visited=3..3 rounds=10/20
    w1b_probe_test.go:106: N=5000 table_capacity(probed_by_scan)=16384 | Scan(full key): avg=935.962µs slots_visited=16384 pairs=1 | Get(key): avg=28.037µs slots_visited=1..1 rounds=10/20
--- PASS: TestW1BScanFullKeyVersusGet (27.95s)
（第 2 轮，同命令）
    w1b_probe_test.go:106: N=1000 table_capacity(probed_by_scan)=2048 | Scan(full key): avg=57.793µs slots_visited=2048 pairs=1 | Get(key): avg=10.116µs slots_visited=3..3 rounds=10/20
    w1b_probe_test.go:106: N=5000 table_capacity(probed_by_scan)=16384 | Scan(full key): avg=405.25µs slots_visited=16384 pairs=1 | Get(key): avg=14.883µs slots_visited=1..1 rounds=10/20
--- PASS: TestW1BScanFullKeyVersusGet (37.57s)
rc=0（两轮均 PASS；探针不做计时断言，只输出数字）
```

- 读数（按「是否随 N 增长」表述）：
  - `Scan(完整键)` slot 访问数＝表容量：2048（N=1000）→ 16384（N=5000），随 N 增长 8 倍；耗时 57.8–94.6µs → 405–936µs，随 N 增长约 4–10 倍（两轮绝对值漂移来自负载噪声）。`pairs=1`：命中只有目标键一枚，全表枚举是纯开销。
  - `Get` slot 访问数 1–3（probe 链长，与表规模无关）；耗时 10.1–28.0µs，波动来自冷读一次值 span（含页缓存），不随 N 增长。
- 测量面声明：Scan 在**写视图**重开后测（`openJournalKV`）；生产 `readReceiptLocked` 多以读视图读（`openJournalView(dir,false)` 多 fold 一个空 window——writer 关闭时 `flush(true)` 已把 watermark 推满，见 `journal.go:2371-2389`），数量级不受影响，但未在只读视图逐数复测（见未查面）。

## 疑点 B · 派生索引零偏移改变答案 —— 已复现

**结论：已复现。** `orderLineOnce` 只拒绝 `journalOrderLineUnknown(-1)` 与 `> orderSize`，`0` 被接受为合法 seek（`memory/ledger/journal.go:1716-1726`；`-1` 哨兵定义 :98-102，word 宽 8 B :103-106，文件 `order.line` 每 slot 一 word :86-97）。`GetParent` 经 `EachIndexNewestFirstFrom` 起点 seek 提供（`assembly/read.go:277-323`，闭包起点 :286；`segment_store.go:1182-1200` → `indexEntrySeek` :1206-1213 → `OrderLineEnd`）。walk 循环 `for pos := from; pos > 0;`（`journal.go:2287`）在 `from=0` 时一次都不执行——零偏移把「进入点=该键的 commit」变成「进入点=最老端之前」，key 自身与其前驱都从 walk 消失，`GetParent` 静默答「无前驱」；全序走查（从 `orderSize` 入口）仍答出真实前驱。**答案不同＝已复现**，且是静默少答而非具名错误。

- 机制探针（ledger 包内白盒，两次 commit、把最新键的 lineEnd word `170 → 0`）：

```
$ cd /tmp/w1b-copy && go test ./memory/ledger/ -run 'TestW1BOrderLineZeroWordSeeksAndWalksNothing' -v -count=1
    w1b_probe_test.go:156: slot 885 of ".../order.line": lineEnd word 170 -> 0 (orderSize=170)
    w1b_probe_test.go:174: walk entered at the zeroed lineEnd named 0 key(s) []
    w1b_probe_test.go:185: the undamaged seek of the same key names "w1b:new" first; the zeroed word removed both the key and the fact behind it from the walk
--- PASS (0.06s)
```

- 主探针（assembly 层，链 `probe-b1 → probe-b2`、同 partition 同 session 走 index 路径；损坏＝把 `order.line` 中全部 `lineEnd=521`（＝orderSize，最后一次 commit）的 word 写 0，本例 4/1024 枚；重开写视图后对照 `GetParent` 与同闭包全序走查 `EachIndexNewestFirstUntil`）：

```
$ cd /tmp/w1b-copy && go test ./assembly/ -run 'TestW1BGetParentAgreesWithFullWalkAfterZeroOrderLine' -v -count=1
    w1b_probe_test.go:130: undamaged: GetParent(probe-b2) = probe-b1
    w1b_probe_test.go:134: order.line: 4 of 1024 word(s) stated lineEnd=521 and now state 0
    w1b_probe_test.go:155: zeroed: GetParent(probe-b2) = "", full-order walk = "probe-b1"
    w1b_probe_test.go:157: REPRODUCED: after the last commit's order-line word was zeroed, GetParent(probe-b2)="" while the full-order walk answers "probe-b1"
--- FAIL: TestW1BGetParentAgreesWithFullWalkAfterZeroOrderLine (0.07s)
rc=1（断言「两者相等」失败＝复现原文，保留）
```

- 复跑：同命令第二次 `rc=1`，同一失败行（确定性损坏，非 flaky）。另注意 `eachNewestFirstAt` 对 `from` 的越界重置只处理 `<0 || >orderSize`（`journal.go:2235-2238`），`0` 在合法域内，无任何校验/修复路径拦它——与「missing/torn/behind watermark 会 rebuild」的注释承诺（:86-97）之间存在设计差距：word 值本身无 checksum，损坏为 0 不触发 rebuild。
- 副作用范围备注：同一 word 清零对 `OrderLineEnd` 接受面（`journal_kv.go:350-364`）与 `EachCommittedNewestFirstFrom`（`journal_kv.go:370-384`）生效，即所有依赖该 seek 的走查入口（不止 `GetParent`）在零偏移下都进入空 walk。

## 边界与复跑清单

| 命令（工作目录 /tmp/w1b-copy） | rc |
|---|---|
| `go vet ./memory/ledger/ ./assembly/` | 0 |
| `go test ./memory/ledger/ -run 'TestW1B' -count=1`（A＋B 机制） | 0 |
| `go test ./memory/ledger/ -run 'TestW1BScanFullKeyVersusGet' -v -count=1` | 0（两轮） |
| `go test ./assembly/ -run 'TestW1BGetParentAgreesWithFullWalkAfterZeroOrderLine' -v -count=1` | 1（红＝复现，两轮同） |

- 探针在副本内，`/tmp/w1b-copy` 清理后可由本文件的夹具描述＋`git archive 52d7b664` 重建。

## 未查面（如实标注）

- A：未在只读视图（`openJournalView(dir,false)`，多 fold window）逐数复测；未用真实 `.wechat-config/data` 键族规模（N 为合成 1000/5000）；Scan 计时轮内值读命中 resident（每轮仍付全表枚举＋排序＋建 map，已由 slots_visited=capacity 证实）；未涉 mem 引擎（疑点本就限定 journalKV 面）。
- B：只注入 `0` 一种偏移；未测「非零错位」「越界但 <orderSize」形态与读视图重开下的同一损坏（代码路径同一 `orderLineOnce`，判定可迁移但未实测）；未测 session 空走 `parentFromBodies` 的 fallback 路径；未测该损坏经 `ChainFailure`/corruption 具名面的暴露情况（`order.line` word 无 checksum，静态上无具名路径）。
- 两项均未在 CI 矩阵环境复跑；负载机计时数量级结论，非门。
