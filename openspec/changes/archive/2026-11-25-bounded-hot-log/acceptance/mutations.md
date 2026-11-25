# bounded-hot-log F3 变异击杀账

## 方法与边界

- 变异副本、overlay JSON、完整 stdout/stderr 仅在 `/tmp/tagent-bhl-f3*/`；Go `-overlay` 将 `/tmp` 副本映射到绝对源路径，主树生产/tests 零写。
- 每组先跑 `go test -overlay=... -run '^$'` 编译检查，再跑目标锚。七组编译 exit 0，目标锚均 exit 1。
- `acceptance/red/C*.log` 保存原始失败摘录；`acceptance/mutations/C*.diff` 是统一 diff，可由 `claims.py` 在隔离树重放。
- 基线 `bash /Users/pengweiye/.qoder/skills/openspec-multilevel-planning/scripts/validate.sh claims /Users/pengweiye/Documents/codes/tagent/openspec/changes/bounded-hot-log s5` exit 0，C1–C7 全绿。
- F 门会直接 patch 工作树，故在 `/tmp/tagent-final-dod3` 的干净共享克隆中应用当前完整 diff 后执行：

```sh
bash /Users/pengweiye/.qoder/skills/openspec-multilevel-planning/scripts/validate.sh \
  claims /tmp/tagent-final-dod3/openspec/changes/bounded-hot-log f
```

exit 0；C1–C7 均为 `PASS claims:Cn 变异击杀 (failed=1)`，各组先跑当前 command 转绿，再应用对应 mutation 转红。F 门结束后，影子树目标源文件恢复为应用 mutation 前的哈希。

## 总表

| id | 原叶号 | require 符号 | 变异源路径 | mutation diff 摘要 | 目标锚 | compile | red |
|---|---|---|---|---|---|---|---|
| C1 | 03-read-paths-release#03.302 | `hotOverBudget` | `memory/ledger/journal_kv.go` | 删除热窗观测消解时的 `k.hotOverBudget = 0`，使已收敛区间继续累计 | `TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`；`TestQuietPointUnreachableToleratesOverBudget` | 0 | 1 |
| C2 | 02-rotation-archive#02.204 | `ErrArchiveMissing` | `memory/ledger/archive_read.go` | locator 将在线归档也走 `ErrOriginalMissing`，拒绝归档身份读 | `TestIdentityReadCrossesHotAndArchive` | 0 | 1 |
| C3 | 02-rotation-archive#02.203 | `segment.id >= newest` | `memory/ledger/journal_kv.go` | 去掉 newest 排除（以 `false &&` 保留变量引用），候选包含最新段 | `TestHottestSegmentNeverArchived` | 0 | 1 |
| C4 | 03-read-paths-release#03.302 | `IntegrityBytesScanned` | `memory/ledger/journal.go` | 扫描读数叠加 manifest 所列归档段 `stat` 字节 | `TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable` boot 上界 | 0 | 1 |
| C5 | 02-rotation-archive#02.202 | `readArchiveManifest` | `memory/ledger/archive.go` | 删除有效 manifest 对最终名集合的校验，未陈述段/索引重新被忽略 | `TestArchiveManifestRefusesUnstatedNamedArtifact` | 0 | 1 |
| C6 | 03-read-paths-release#03.302 | `ArchiveRoot` | `memory/ledger/journal_kv.go` | 删除 `hotLogMaxBytes > 0` 门，checkpoint 无归档也封段 | `TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable` 无归档臂 | 0 | 1 |
| C7 | 02-rotation-archive#02.204 | `ErrArchiveMissing` | `memory/ledger/archive_read.go` | `archiveFaultOf` 将 `ErrArchiveMissing` 折为 `ErrOriginalMissing` | `TestArchiveMissingIsNamed` | 0 | 1 |

`claims.tsv` 的 `leaf/require=-` 是通用检查器兼容处置：该检查器要求任务行“验证”整段文字逐字出现在命令中，且 require 只能指向 TS 测试路径；Go 任务行是锚描述，仓库无 TS 测试。原叶号与 require 符号保留在本表，实际断言由 `command` 点名 Go 锚和 mutation 击杀承担，未伪造 TS 测试或改 tasks。

## C1

- 编译：`go test -overlay=/tmp/tagent-f3-C1/overlay.json -run '^$' ./assembly` → exit 0。
- 红：`go test -overlay=/tmp/tagent-f3-C1/overlay.json ./assembly -count=1 -run '^(TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable|TestQuietPointUnreachableToleratesOverBudget)$' -v` → exit 1。

```text
    checkpoint_test.go:6376: the recovered cut requested=false over_budget=2, want no request and the resolved interval
--- FAIL: TestQuietPointUnreachableToleratesOverBudget (0.37s)
    checkpoint_test.go:7086: archive reason: over-budget: gen1 hot_over_budget=1, want zero
    checkpoint_test.go:7086: archive reason: over-budget: gen2 hot_over_budget=2, want zero
    checkpoint_test.go:7086: archive reason: over-budget: gen3 hot_over_budget=3, want zero
    checkpoint_test.go:7086: archive reason: over-budget: gen4 hot_over_budget=4, want zero
    checkpoint_test.go:7088: the archive workload does not satisfy the plateau criterion
--- FAIL: TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable (1.90s)
FAIL	github.com/SpellingDragon/tagent/assembly	2.817s
```

## C2

- 编译：`go test -overlay=/tmp/tagent-bhl-f3/C2/overlay.json -run '^$' ./memory/ledger ./tool/recall` → exit 0。
- 红：`go test -overlay=/tmp/tagent-bhl-f3/C2/overlay.json ./memory/ledger -count=1 -run '^TestIdentityReadCrossesHotAndArchive$' -v` → exit 1。

```text
=== RUN   TestIdentityReadCrossesHotAndArchive
    archive_read_test.go:71: the archived read of "seg-01-a" = [] want the bytes the commit wrote
--- FAIL: TestIdentityReadCrossesHotAndArchive (0.33s)
FAIL
FAIL	github.com/SpellingDragon/tagent/memory/ledger	0.522s
```

## C3

- 编译：`go test -overlay=/tmp/tagent-bhl-f3/C3/overlay.json -run '^$' ./memory/ledger ./assembly` → exit 0。
- 红：`go test -overlay=/tmp/tagent-bhl-f3/C3/overlay.json ./memory/ledger -count=1 -run '^TestHottestSegmentNeverArchived$' -v` → exit 1。

```text
=== RUN   TestHottestSegmentNeverArchived
    archive_test.go:637: the candidates = [{id:1 path:/var/folders/6x/nhts7g654qd9_91w1c6m2zym0000gn/T/TestHottestSegmentNeverArchived2783219520/001/segments/seg-000001.log begin:0 bytes:368 firstSeq:1 lastSeq:3 headerBytes:217 version:2 commitTimeMinNS:1795530567995146000 commitTimeMaxNS:1795530568004919000} {id:2 path:/var/folders/6x/nhts7g654qd9_91w1c6m2zym0000gn/T/TestHottestSegmentNeverArchived2783219520/001/segments/seg-000002.log begin:368 bytes:368 firstSeq:4 lastSeq:6 headerBytes:217 version:2 commitTimeMinNS:1795530568028423000 commitTimeMaxNS:1795530568037021000} {id:3 path:/var/folders/6x/nhts7g654qd9_91w1c6m2zym0000gn/T/TestHottestSegmentNeverArchived2783219520/001/segments/seg-000003.log begin:736 bytes:368 firstSeq:7 lastSeq:9 headerBytes:217 version:2 commitTimeMinNS:1795530568060936000 commitTimeMaxNS:1795530568070293000}], want only the two intervals older than seg-000003
--- FAIL: TestHottestSegmentNeverArchived (0.16s)
FAIL
FAIL	github.com/SpellingDragon/tagent/memory/ledger	0.356s
```

## C4

- 编译：`go test -overlay=/tmp/tagent-bhl-f3/C4/overlay.json -run '^$' ./memory/ledger ./assembly` → exit 0。
- 红：`go test -overlay=/tmp/tagent-bhl-f3/C4/overlay.json ./assembly -count=1 -run '^TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable$' -v` → exit 1。

```text
=== RUN   TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable
    checkpoint_test.go:6926: boot IntegrityBytesScanned=1443168 > final hot_bytes=833853
    checkpoint_test.go:6928: boot duration=5.401792ms JournalBytesOpen=0 IntegrityBytesScanned=1443168 final_hot_bytes=833853
--- FAIL: TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable (3.14s)
FAIL
FAIL	github.com/SpellingDragon/tagent/assembly	3.519s
```

## C5

- 编译：`go test -overlay=/tmp/tagent-bhl-f3-new/C5/overlay.json -run '^$' ./memory/ledger` → exit 0。
- 红：`go test -overlay=/tmp/tagent-bhl-f3-new/C5/overlay.json ./memory/ledger -count=1 -run '^TestArchiveManifestRefusesUnstatedNamedArtifact$' -v` → exit 1。

```text
=== RUN   TestArchiveManifestRefusesUnstatedNamedArtifact
=== RUN   TestArchiveManifestRefusesUnstatedNamedArtifact/segment
    archive_test.go:895: the unstated segment reads found=true err=<nil>, want ErrArchiveCorrupt
=== RUN   TestArchiveManifestRefusesUnstatedNamedArtifact/index
    archive_test.go:895: the unstated index reads found=true err=<nil>, want ErrArchiveCorrupt
--- FAIL: TestArchiveManifestRefusesUnstatedNamedArtifact (0.31s)
FAIL
FAIL	github.com/SpellingDragon/tagent/memory/ledger	0.525s
```

该变异直接删除 C5 的 manifest 最终名集合校验；同一 `claims.tsv` C5 完整命令也会因这两子测失败而 exit 1。帧 CRC 的既有负锚仍由 C4 命令覆盖，不重复计 C5 变异。

## C6

- 编译：`go test -overlay=/tmp/tagent-f3-C6/overlay.json -run '^$' ./memory/ledger ./assembly` → exit 0。
- 红：`go test -overlay=/tmp/tagent-f3-C6/overlay.json ./assembly -count=1 -run '^TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable$' -v` → exit 1。

```text
=== RUN   TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable
    checkpoint_test.go:7161: no-archive gen1 produced hot_segments=1 manifest_segments=0 archive_bytes=0
    checkpoint_test.go:7161: no-archive gen2 produced hot_segments=2 manifest_segments=0 archive_bytes=0
    checkpoint_test.go:7161: no-archive gen3 produced hot_segments=3 manifest_segments=0 archive_bytes=0
    checkpoint_test.go:7161: no-archive gen4 produced hot_segments=4 manifest_segments=0 archive_bytes=0
    checkpoint_test.go:7171: the no-archive root produced archive artifacts
--- FAIL: TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable (1.91s)
FAIL
FAIL	github.com/SpellingDragon/tagent/assembly	2.267s
```

## C7

- 编译：`go test -overlay=/tmp/tagent-red-C7/overlay.json -run '^$' ./memory/ledger ./tool/recall` → exit 0。
- 红：`go test -overlay=/tmp/tagent-red-C7/overlay.json ./memory/ledger -count=1 -run '^TestArchiveMissingIsNamed$' -v` → exit 1。

```text
=== RUN   TestArchiveMissingIsNamed
    archive_read_test.go:114: the read of the stated key "seg-01-a" = ([], <nil>), want ErrArchiveMissing
--- FAIL: TestArchiveMissingIsNamed (0.19s)
FAIL
FAIL	github.com/SpellingDragon/tagent/memory/ledger	0.603s
```

## forbid 字面

以下字面逐字取 `changes/01-bounds-contract/contracts/archive.md§六` 对应 `fails_when`，未使用旧占位或自编负向词。

| id | claims.tsv forbid | canonical fails_when 来源 |
|---|---|---|
| C1 | `` `hot_over_budget` 非零 `` | `hot_over_budget` 非零即 C1 不可宣称 |
| C2 | `答「不存在」` | MUST NOT 答「不存在」 |
| C3 | `按内容找安静点/切片` | 把水位判据改写成「按内容找安静点/切片」即违约 |
| C4 | `预扫归档卷` | 预扫归档卷……任一即违约 |
| C5 | `第二份正文` | 出现第二份正文……任一即违约 |
| C6 | `未配置却轮转` | 未配置却轮转、封段、搬迁或删除任何字节即违约 |
| C7 | `Missing→absent` | Corrupt→absent、Missing→absent 的任一折叠即违约 |

## Frontier 与复核

- C1 的 forbid 是运行时「非零」条件，静态缺席只证明没有该断言文案；真正判据是 C1 变异下 S1 四代 `hot_over_budget=1` 与 quiet 负锚恢复后仍为 1。
- C5 变异删除 `readArchiveManifest` 的最终名集合校验；它直接击杀未陈述段/索引锚，不宣称覆盖索引层全部损坏形态。
- 主树目标源文件复核：`memory/ledger/journal_kv.go`、`memory/ledger/archive_read.go`、`memory/ledger/journal.go`、`memory/ledger/archive.go` 的 SHA-256 前后完全一致；影子树 F 门应用并还原后也与基线一致。
- 最终主树复跑时曾出现并发外部改动：`runtime/state/store.go` 新增 `Store.AcceptNoticeClaimed`，而 `MemoryStore` 尚未实现，assembly 编译失败。该竞争已收口：`Store` 公共接口保持 4 个方法，原子 handover 保留为 `MemoryStore` 方法；当前主树 `claims f`、`closure`、`review` 均 exit 0。
- 本证据生成时未改生产、tests、tasks、ORCHESTRATION，未提交、未推送；随后仅补充 mailbox 修复与门卫生，并随本候选提交。
