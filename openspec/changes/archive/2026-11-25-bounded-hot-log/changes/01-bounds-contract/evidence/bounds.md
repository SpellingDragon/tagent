# 01.102 有界判据形式化（evidence）

- 基线：`6e90d6f104cc138b2337d15ac637f7d43fb92342`（开工 `git rev-parse HEAD` 重核，与派发单一致）
- 写面：仅本文件。S1 锚草案与判据草案在**隔离副本** `/tmp/bhl102/`（`git archive HEAD` 解出，非 worktree、主树零写）；本文件所有数字来自【六】【七】【八】的原始终端输出
- 前置读数取自同域 `evidence/growth.md`（01.101）与本叶复测（01.105 单份化后，同一生产路径、同一负载形状）；凡 growth.md 无的项在本叶实测，凡本叶无的项标 ❓
- 判据的 canonical 家：本节（形式与测量定义）；配置字段语义、三态错误、承诺表 canonical 家在 `contracts/archive.md`（01.103），本节只引用不复述

## 〇 结论先行

1. **判据形式**：`hot_bytes(g) ≤ f(window, g) + B_overhead(g)`，其中 `f`＝保留窗内提交量、`B_overhead`＝**两个 cut 间隔的写入量**（索引侧**不进 RHS**——否则 LHS 与 RHS 同含该量、判据自消；索引侧有界于热窗键数由 02.203 锚承担）。基准树（无 `archive_root`）上 `f` 无定义（窗不存在），判据按具名理由 `window-never-fills` 拒，实测热字节 3.69 MB → 15.12 MB（**×4.10**，4 代）——「第一天红」成立。
2. **三层判据**（绝对 / 带 / 参考）＋三层观测（超界状态、开档恢复、分母完整性），逐层具名理由；参考层有前置条件 `W ≥ 10·B_overhead`，条件不成立时该层**声明为空并记录**，不做静默放宽。
3. **构成口径**：`hot_bytes = H + R`，`H`＝恒热集合（最新 cut 段＋开段＋索引侧），`R`＝配置保留旧段。单份化后实测：最新 cut 段 8,439 B／开段 960 B／索引侧 1,131,403 B（第 4 代），即 `H ≈ 1.14 MB`；`B_overhead ≈ 2.41 MB`（两个 cut 间隔，不含索引侧；全部十进制 MB）。
4. **量级**：单份化把每 64 提交段从 2.33 MB 压到 1.21 MB（−48.2%），cut 帧从 14.4 KB 压到 8.4 KB（−41.6%）。判据的鉴别力区间是 `W ∈ [B_overhead, 10·B_overhead] ≈ [2.41, 24.1] MB`；`W` 远大于此区间后带层几乎恒真，只有绝对层（`R ≤ W`）与恢复层在起作用——这是「不以带宽凑平台」的量化理由。
5. **归档面单列**（不参与热卷判据）：归档卷字节、跨档读延迟 p50/p90、开档恢复成本三类分别记录；开档校验字节的实测基线一并入档（本叶复测 3.41→6.94→10.43 MB 随历史线性，与 01.101 §3.6 同结论）。

## 一 平台判据的形式

### 1.1 记号

```
hot_bytes(g) ≤ f(window, g) + B_overhead(g)                      绝对层（W > 0）
max{hot_bytes(g) | g ∈ G} ≤ min{hot_bytes(g) | g ∈ G} × 1.30      带层（|G| ≥ 3）
hot_bytes(g) ≤ hot_bytes(G[0]) × 1.10   ∀g ∈ G\G[0]              参考层（仅当 W ≥ 10·B_overhead）
hot_over_budget(g) = 0                                            观测层（正常负载）
boot_scanned(g) ≤ hot_bytes(g-1)                                  恢复层
commits(g) ≥ commits(G[0]) ∧ cuts(g) ≥ cuts(G[0])                分母完整性层
```

- `G` ≔ 满窗代序列 ≔ `{g | cumulative_bytes(g) ≥ W}`，`G[0]` 即首个满窗代；`|G| ≥ 3` 是判据成立的前提（单代数字不构成平台，`spec.md` 已禁）。
- 满窗条件：`cumulative_bytes(g) ≥ W`。窗未满前一律允许增长（窗还要的字节不能搬），故 `G` 之外各代不参与带层与参考层。
- 与 design D2 的关系：`D2` 写作 `max(hot_bytes[2..N]) ≤ min(hot_bytes[2..N]) × 1.3` 且 `hot_bytes[2..N] ≤ hot_bytes[1] × 1.1`，隐含「第 1 代即满窗」；01.102 已裁定把 `[1]` 改为首个满窗代 `G[0]`，本节即该裁定为 `G[0]`、把 `[2..N]` 明确为 `G`，D2 的写法是 `G = gens[0..N]` 时的特例。**口径裁定（本叶自决，理由：D2 的后半式与同段「窗口未满前允许增长」自相矛盾，只有把参照代固定在首个满窗代才自洽）**。
- 参考层前置条件：`W ≥ 10 × B_overhead`。推导：`hot_bytes(G[0]) ≈ W`，参考层容差 `0.1·W` 必须至少容下 `B_overhead` 才有约束力；不满足时本层恒真，其真实约束力为零，故判据**显式声明该层为空**（`ReferenceApplied=false`）而不是假装它通过了。

### 1.2 构成口径

```
hot_bytes(g) = H(g) + R(g)
H(g) = newest_segment(g) + open_segment(g) + index_side(g)        恒热集合
R(g) = journal_bytes(g) − newest_segment(g) − open_segment(g)     配置保留旧段
B_overhead(g) = 2 × max_segment(g)                              两个 cut 间隔（不含索引侧；索引入 H）
```

- `H` 只由水位规则构造：段界 ≤ 倒数第二个 cut 的段可搬，最新 cut 所在段与滚动开段恒热（`design.md` D3、`contracts/archive.md` C3）；`index_side` 是帧空间之外但同在热目录的字节（`order.log`/`order.line`/`slots`/`seat`），**必须计入**，否则「journal 有界而目录无界」（01.101 §3.8）。
- `R` 是唯一受 `hot_log_max_bytes` 约束的部分。绝对层（`H + R ≤ W + 2·max_seg`）代入 `H = newest + open + index_side` 得 `R + index_side + (newest + open − 2·max_seg) ≤ W`；因 `newest, open ≤ max_seg`，末项 ≤ 0，故 `R + index_side ≤ W` 是充分条件（部署直觉式）。两式在实现中并存：代码写字面形式（可实测、无代数假设），本文记录充分条件性（条件充分性由 `contracts/archive.md` C1 承担）。
- `B_overhead` 的「两个间隔」取自 cut 唯一时点：切段与 checkpoint 是同一事件，任一间隔至多一个段；两个间隔＝最新 cut 段＋开段的上限（`design.md` D3，2026-11-23 裁定）。配置上限的实现口径是 `configured = W + 2 × 当前热段最大日志区间`（`contracts/archive.md` C1）；本文的 `B_overhead(g)` 是同一式子的代读数，`max_segment(g)` 取该代收尾热卷内已封段最大日志区间；全史最大只作观测量，不参与 `configured`。

### 1.3 观测项（不作判据、但随代必记）

| 项 | 口径 | 单位 |
|---|---|---|
| `hot_over_budget(g)` | 该代收尾时热字节高于生效上限、或配置上限低于结构下限的状态（0/1）；正常负载必须为 0，**状态为 1 即平台锚失败** | 状态 |
| `longest_over_budget(g)` | 不产出：草案项，实现未采集，判据不依赖；观测面只取 0/1 当前区间状态（`contracts/archive.md` C1） | — |
| `archive_bytes(g)` | 归档卷字节和（不含热卷），随代单调记录并显式声明增长 | B |
| `archive_read_latency(g)` | 跨档 readonly 定位读 p50/p90（仅 02/03 可测）❓ | ms |
| `boot_cost(g)` | 开档校验字节与耗时、索引重放窗口（`JournalBytesOpen`） | B / ms |

`hot_over_budget` 为 1 的形态是**容忍超界**（安静点不可达＝在飞操作不落地），不是改切割时点；该失败形态由 03.302 的 `TestQuietPointUnreachableToleratesOverBudget` 与 S1 成对钉死（`contracts/archive.md` C1 的 `fails_when`）。

## 二 符号的测量定义（每个符号＝可复跑）

| 符号 | 定义 | 测量与命令（生产路径，禁测试钩子） |
|---|---|---|
| `hot_bytes(g)` | 热卷目录字节和，不含归档卷；取「代收尾（`Close` 后、下一 `Open` 前）」 | `anchorDirBytes(<root>/kv)`＝该目录下全部文件 `stat` 尺寸和；`du -sk <root>/kv` 作块取整交叉核对（本叶复测 `du` 较尺寸和大 <0.2%） |
| `journal_bytes(g)` | 帧日志字节 | `stat <root>/kv/journal.log` |
| `index_side(g)` | `hot_bytes − journal_bytes` | 同上两项相减；逐文件等于 `order.log`+`order.line`+`slots`（+`seat` 若独立成文件） |
| `commits(g)` | **分母（条）**：该代落地的提交批数 | 写入侧计数：每条 `ledger.CommitFact` 与每次 `Store.Commit` 记 1；本锚 202/代＝192 事实＋4 checkpoint 事实＋6 控制事实（与 01.101 §3.7 同口径，quiet marker 不计） |
| `commit_bytes(g)` | **分母（字节）**：该代提交追加的日志字节 | `journal_bytes(close) − journal_bytes(open)` |
| `cumulative_bytes(g)` | 满窗判据的输入 | `Σ commit_bytes(1..g)`，逐代累加 |
| `W` | 保留窗目标 | `hot_log_max_bytes`（`api/config`，`contracts/archive.md` §五）；未声明＝0＝窗不存在 |
| `f(window, g)` | 保留窗内提交量 | `min(cumulative_bytes(g), W)`；窗满时恒为 `W` |
| `cuts(g)` | 成功 cut 数（失败 cut 不封段，不入计） | 每次 cut 记 1：`AppendOrdered(marker)` + checkpoint 事实成功落地才算成功 |
| `max_segment(g)` | 该代收尾热卷内已封段最大日志区间 | 相邻两个 cut 的 checkpoint 帧末尾之间的 `journal` 增量，只在段仍位于热卷时取最大值；段界＝cut（§1.2） |
| `newest_segment(g)` | 最新 cut 段的字节 | 同上，取最后一个 cut 的那一段 |
| `open_segment(g)` | 滚动开段字节 | `journal_bytes(close) − 末 cut 帧末尾偏移` |
| `retained(g)` | `R(g)` | `journal_bytes − newest_segment − open_segment` |
| `R` | 保留窗内旧段字节，≡ `retained(g)`（同一测读，锚的列名是 `retained`） | 同上行，不另测 |
| `hot_over_budget(g)` | 当前未收敛区间状态（0/1） | 热字节高于生效上限、或配置上限低于结构下限时为 1；一次观察同时排除两形态才归 0（实现 `memory/ledger/journal_kv.go:303` `HotLogOverBudget`；本锚在无归档形态恒 0） |
| `boot_scanned(g)` | 开档校验字节 | `IntegrityBytesScanned()`（`memory/ledger/journal.go:2723`）；耗时 `IntegrityPassSpan()`（`:2771`）；索引重放 `JournalBytesOpen()`（`journal_kv.go:144`） |
| `sealed_segments(g)` | 封段数 | 段索引＋manifest 条目计数；HEAD 无该结构⇒0（02 落地后由段账户计） |

grep 自证（本文件落地后执行，见【七】）：本表每行以 `| \`符号\`` 开头，`grep -c '^| `'` 即符号数；判据理由串在 §三 与本表各出现一次，`grep -c` 交叉核对。

## 三 判据的实现形态与逐层理由

判据草案的两个文件（**仅在隔离副本**，主树零写）：

- `/tmp/bhl102/tests/integration/hotlog_platform_test.go`（275 行）：判据本体 `evaluateHotLogPlatform` / `evaluateHotLogPlatformWith`、构成类型、`hotLogMechanismGaps`
- `/tmp/bhl102/tests/integration/hot_log_plateau_test.go`（514 行）：S1 锚
- `/tmp/bhl102/tests/integration/hotlog_platform_negative_test.go`（181 行）：负锚（手写代序列逐层钉死）

生产家由 01.104 签发决定（候选：02 的切割请求侧提供 `hot_over_budget`，03.302 复用同一判据函数）；本叶只冻结形式与理由串。

判据理由串（具名，禁自由文本）与失败层一一对应：`window-never-fills`｜`full-window-generations`｜`generations`｜`absolute-breach`｜`band-breach`｜`reference-growth`｜`over-budget`｜`boot-scan-exceeds-hot`｜`denominator-shrinks`｜`cut-frequency-falls`。后两条即「MUST NOT 以调低 cut 频率／缩小工作量凑平台」的机械落点：同字节平台但分母下降，判据拒。

## 四 机理断言（随 S1 同锚，不另立锚）

| # | 断言 | 测量 | 今天 |
|---|---|---|---|
| M1 | 最新 cut 段与开段不在 manifest（恒热） | manifest 条目集合 ∩ {最新 cut 段, 开段} = ∅ | HEAD 无 manifest；无归档配置下「无段被陈述为已归档」平凡成立，`sealed=0` 如实打印 |
| M2 | 封段数＝成功 cut 数（失败 cut 不封段） | `sealed_segments(g) == cuts(g)` | **0 ≠ 4**/代 ⇒ 具名结构缺口（`segments-sealed`），随锚红 |
| M3 | 开档校验字节有界于热窗；归档段读时校验、不预扫 | `boot_scanned(g) ≤ hot_bytes(g-1)` | 今天等式成立（`boot_scanned` = 该代开档时的整个 journal），归档后若预扫则越界即红 |

M2 的口径裁定（本叶自决）：「热段数＝成功 cut 数」按**封段侧的 1:1 对应**读——成功 cut 数 = 全史封段数，可搬集合 = 段界 ≤ 倒数第二个 cut 的段，故热卷常态只余 {最新 cut 段, 开段} 两段；若按「热卷内段数」直读则恒为 2 而与 cut 数无关，不自洽。写法以本表为准，02 签名时回填实测名。

## 五 归档声明与热卷有界的分离（四条 MUST NOT）

1. **归档卷增长显式声明**：`archive_bytes(g)` 单调记录，并在证据中点明「历史量级 × 单份化后每事实字节」的线性关系。MUST NOT 以归档卷增长替代热卷有界：两个数字必须同时出现，热卷数字缺一即为未证。
2. **跨档读延迟单列**：`archive_read_latency` 的 p50/p90 与热读延迟分开报（p90 必须报，禁只报均值）。MUST NOT 把跨档延迟混入「热读不变」的结论。
3. **开档恢复成本单列**：`boot_cost` 与 01.101 §3.6 基线同表对比（本叶复测见【六】）。MUST NOT 以「热盘有界」代替「开档有界」——二者同源但不同量：热盘有界不蕴含 `boot_scanned` 不随归档增长。
4. **MUST NOT 以调低 cut 频率凑平台**：cut cadence 是界的部署参数（`design.md` D3），不是可调的自由变量。判据以 `cut-frequency-falls` 与 `denominator-shrinks` 两层机械拒绝该凑法，证据表必须并列 `cuts(g)` 与 `commits(g)`。

## 六 量级预估（基于 01.101 单份化后构成）

### 6.1 单份化后的实测构成（本叶复测，4 代 202 提交/代，段界＝cut）

```
gen    hot_bytes     journal index_side commits commit_bytes cuts    max_seg  newest_seg  open_seg    retained sealed boot_scanned
1        3687157     3405081     282076     202     3405081    4    1138507        8439       960     3395682      0            0
2        7509433     6944705     564728     202     3539624    4    1195859        8439       960     6935306      0      3405081
3       11487526    10426981    1060545     202     3482276    4    1195859        8439       960    10417582      0      6944705
4       15117116    13985713    1131403     202     3558732    4    1205415        8439       960    13976314      0     10426981
```

与 01.101（双份）同形状对照：

| 项 | 01.101 双份 | 本叶单份化后 | 比 |
|---|---|---|---|
| 第 4 代 journal | 26,918,264 B | 13,985,713 B | **51.96%** |
| 每 64 提交最大段 | 2,325,068 B | 1,205,415 B | 51.84% |
| cut 帧（quiet+checkpoint） | 14,390–14,402 B | 8,439 B | 58.6% |
| 每代提交字节 | 6.84–7.21 MB | 3.41–3.56 MB | ≈50% |
| 索引侧（第 4 代） | 1,180,650 B（`order.log`+`slots`+`order.line`） | 1,131,403 B | 95.8%（未单份化，符合预期） |

`newest_seg = 8,439 B` 与 growth.md §五「cut 帧从 14.4 KB 降到 ≈8.3 KB」的预测一致（实测 8.4 KB）。

### 6.2 判据的数量级

| 量 | 实测/推导 | 含义 |
|---|---|---|
| `H`（第 4 代） | 8,439 + 960 + 1,131,403 = **1,140,802 B ≈ 1.14 MB** | 恒热集合；其中 99.2% 是索引侧 |
| `B_overhead`（第 4 代） | 2×1,205,415 = **2,410,830 B ≈ 2.41 MB** | 两个 cut 间隔（索引侧归 H） |
| `B_overhead`（第 1 代） | 2×1,138,507 = 2,277,014 B ≈ 2.28 MB | 同左 |
| 参考层生效门 | `W ≥ 10·B_overhead ≈ 24.1 MB` | 本负载形状下 `W` < 24.1 MB 时参考层为空 |
| 带层容差 | `0.3·W` | `W = 14 MB`（4 代窗）→ 4.2 MB，与 `B_overhead` 同量级（两层同时约束）；`W = 64 MB` → 19.2 MB ≫ `B_overhead`（带层近恒真，绝对层与恢复层主承） |

结论：**判据的鉴别力区间是 `W ∈ [B_overhead, 10·B_overhead] ≈ [2.41 MB, 24.1 MB]`**；`W` 的部署取值决定哪一层真正生效，故证据必须写清 `W` 并声明参考层是否为空（判据字段 `ReferenceApplied`）。❓ `W` 的部署取值未定（仓库禁硬编码 tunable，无内建默认）。

## 七 S1 锚草案与红证

- 锚名（冻结）：`TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`
- 草案位置：`/tmp/bhl102/tests/integration/hot_log_plateau_test.go`（隔离副本，`git archive HEAD` 解出；主树零写）
- 负载：4 代 ×（3 次间隔 cut × 64 提交 + 1 次关档 cut），每代另 6 条控制事实；正文混载 8 KiB 为主、每 10 条 1 条 64 KiB、2 条 1 KiB（与 01.101 §1.2 同形状）；cadence 与两个键前缀**运行时从产品源码读出**（`assembly/checkpoint.go` 的 `CheckpointCommitInterval`/`quietPointKeyPrefix`、`api/event/checkpoint.go` 的 `checkpointKeyPrefix`），不是手抄
- 断言四组：①平台判据（§一 全层）；②老键逐字节读回（第 1 代 `fact-01:0001` 的事件体与原文体各 8,192 B 全等）；③机理断言 M1–M3；④分母与 cut 数随代记录

**红证（正锚，rc=1）** —— 命令与原文：

```sh
cd /tmp/bhl102 && go test ./tests/integration/ \
  -run '^TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable$' -v
# rc=1
```

```
=== RUN   TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable
    hot_log_plateau_test.go:215: old fact fact-01:0001: event body 8192 B == committed 8192 B, original body 8192 B == committed 8192 B
    hot_log_plateau_test.go:223: == bounded-hot-log S1 plateau anchor ==
    hot_log_plateau_test.go:223: window: hot_log_max_bytes=0 (0 states an undeclared window); full-window generations=0
    hot_log_plateau_test.go:223: gen    hot_bytes     journal index_side commits commit_bytes cuts    max_seg  newest_seg  open_seg    retained sealed boot_scanned
    hot_log_plateau_test.go:223: 1        3687157     3405081     282076     202     3405081    4    1138507        8439       960     3395682      0           0
    hot_log_plateau_test.go:223: 2        7509433     6944705     564728     202     3539624    4    1195859        8439       960     6935306      0      3405081
    hot_log_plateau_test.go:223: 3       11487526    10426981    1060545     202     3482276    4    1195859        8439       960    10417582      0      6944705
    hot_log_plateau_test.go:223: 4       15117116    13985713    1131403     202     3558732    4    1205415        8439       960    13976314      0     10426981
    hot_log_plateau_test.go:223: plateau verdict: REJECT
    hot_log_plateau_test.go:223:   reason: window-never-fills: hot_log_max_bytes is undeclared, so no generation fills a window and every byte of history stays hot; hot_bytes ran 3687157 B -> 15117116 B (x4.10) over 4 generations
    hot_log_plateau_test.go:223:   structural gap: segments-sealed: gen1 sealed 0 != 4 successful cuts
    hot_log_plateau_test.go:223:   structural gap: segments-sealed: gen2 sealed 0 != 4 successful cuts
    hot_log_plateau_test.go:223:   structural gap: segments-sealed: gen3 sealed 0 != 4 successful cuts
    hot_log_plateau_test.go:223:   structural gap: segments-sealed: gen4 sealed 0 != 4 successful cuts
    hot_log_plateau_test.go:225: the hot log does not plateau: 1 reasons, 4 structural gaps
--- FAIL: TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable (6.31s)
FAIL
FAIL	github.com/SpellingDragon/tagent/tests/integration	6.678s
```

红因归属（逐条，避免「红了就算数」）：①`window-never-fills`＋`hot_bytes ×4.10`＝**无轮转**，即本变更要解决的问题本身；②`segments-sealed 0 != 4`＝**无段账户**（02 未落地）；③老键读回与 `boot_scanned ≤ 前代热字节` 两项当天**绿**，故红不是环境或编译问题。同一草案连跑 3 次，结论与各列逐字节一致（抖动见 §九 ❓1）。

## 八 负锚：判据须拒（成对见证）

命令与原文（隔离副本内）：

```sh
cd /tmp/bhl102 && go test ./tests/integration/ -run 'TestHotLogPlatformCriterion' -v
# rc=0
```

```
--- PASS: TestHotLogPlatformCriterionRejectsWhatItMust (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/normal (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/one_generation_over_the_band (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/hot_volume_over_the_window_plus_its_overhead (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/growth_the_band_tolerates_and_the_reference_does_not (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/an_over-budget_interval_rejects_flat_bytes (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/a_boot_scanning_past_the_prior_hot_window (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/a_flat_series_at_a_shrinking_workload (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/a_flat_series_at_a_falling_cut_count (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/an_undeclared_window_never_states_a_plateau (0.00s)
    --- PASS: TestHotLogPlatformCriterionRejectsWhatItMust/a_sequence_shorter_than_the_judged_generations (0.00s)
--- PASS: TestHotLogPlatformCriterionBandLayerIsLoadBearing (0.00s)
PASS
ok  	github.com/SpellingDragon/tagent/tests/integration	0.353s
```

「一代增长 > 平台带」的探针即 `one_generation_over_the_band`：把第 3 代热字节抬到 90 MiB（`W = 32 MiB`，第 1 代为 28 MiB），判据以 `band-breach` 拒并点名该代，原文形如

```
band-breach: gen3 94371840 B > gen1 29360128 B x 1.30
```

**变异红证（判据被削弱即红）**：把 `hotLogBandFactor` 由 `1.3` 改为 `0`（带层失效），同命令 rc=1：

```
--- FAIL: TestHotLogPlatformCriterionRejectsWhatItMust/one_generation_over_the_band
    hotlog_platform_negative_test.go:82: no band-breach rejection among
    [absolute-breach: gen3 hot 94371840 B > window 33554432 B + overhead 2621440 B
     reference-growth: gen3 94371840 B > gen1 29360128 B x 1.10]
--- FAIL: TestHotLogPlatformCriterionBandLayerIsLoadBearing
```

复原后 rc=0（`ok github.com/SpellingDragon/tagent/tests/integration 0.359s`）。故「判据拒这一代」依赖带层本身，不是该序列碰巧被别的层拦下。

## 九 不确定项与口径边界（❓）

1. ❓ **字节抖动 ≤28 B**：`ref:` 值含 `StoredAt`（RFC3339Nano），末位小数位数随纳秒变，抖动随累计 ref 条数增长（每条 0–2 B）。同一输入多次运行的 `hot_bytes`（第 4 代）落在 15,117,104–15,117,132 B，`journal` 同幅，其余各列逐字节相同——与 01.101 §六❓1 同源。判据不受影响（差异相对 1.3 带宽 <1e-6）。
2. ❓ **索引侧文件的收缩方式未定**：本锚 HEAD 上 `index_side` 单调增长（282 KB → 1,131 KB，随槽表容量阶跃）。02 MUST 给出「随段收缩」的方式，否则 §1.2 的 `H` 仍无界（01.101 §3.8 打穿点）。
3. ✅ **`hot_over_budget` 形态已实测**：无归档配置下恒 0；03.302 实测其为 0/1 当前未收敛区间状态，成功 cut 本身不归零，seal 后观察同时排除「高于生效上限」与「配置低于结构下限」才归 0（`TestQuietPointUnreachableToleratesOverBudget` 与 S1 成对）。本叶只定其判据地位。
4. ❓ **跨档读延迟 p50/p90、归档卷字节增长**：本叶无归档实现，无数据；02/03 实测后回填 §1.3。
5. ❓ **负载规模小于生产最坏态**：checkpoint payload 4.5 KB（生产注释里的 36 轮 × 64 KiB）、事实体最多 64 KiB。绝对字节随正文体线性放大；`H` 中的索引侧占比（本形状 99.2% 的 `H`）会随负载形状变化，故 §6.2 的数量级以本形状为条件。
6. ❓ **开档校验耗时只有热缓存读数**（与 01.101 §六❓3 同）：本叶复测只取字节数（3,405,081 / 6,944,705 / 10,426,981 B），未测冷缓存。
7. ⚠️ **判据层的有效性依赖 `configured` 与 `H` 的关系**：`configured = hot_log_max_bytes + 2×当前热段最大日志区间 < H` 时绝对层以下限为准（`contracts/archive.md` §五），参考层为空；此时「平台」只由带层与恢复层说话，证据必须显式写 `ReferenceApplied=false`，不得读成「三层全过」。

## 十 对下游叶的输入

- **02.201**：段界＝cut；段字节口径取 §二 `max_segment`；封段数须可按段账户读出（M2）。
- **02.202/02.203**：`hot_bytes` 的构成中 `H` 含索引侧，搬迁与 GC 必须同时收缩它（❓2）。
- **02.204**：`hot_over_budget` 的状态与告警落点（§1.3，02.207 落地），并供给判据函数所需的代读数。
- **03.302**：直接采用本节 §一 的形式与 §三 的理由串；S1 锚草案升格为实存测试时，把 `sealed_segments`/`manifest` 两项从「结构缺口」换成真实段账户读数；负锚草案升格为永驻单测。
- **01.104**：决定判据函数与 `hot_over_budget` 的生产家（候选：`memory/ledger` 切割请求侧 + 复用同一函数），并在签发区登记本叶的 §1.1 口径裁定与 M2 口径裁定。

## 十一 自证命令（本叶收口时执行，输出原文如下）

```sh
cd openspec/changes/bounded-hot-log/changes/01-bounds-contract/evidence

# A 每个符号一行：抽出表行首符号名
grep -o '^| `[A-Za-z_][A-Za-z_]*' bounds.md | sed 's/^| `//' | sort -u
# B_overhead H R W archive_bytes archive_read_latency boot_cost boot_scanned
# commit_bytes commits cumulative_bytes cuts f hot_bytes hot_over_budget
# index_side journal_bytes longest_over_budget max_segment newest_segment
# open_segment retained sealed_segments          （23 个）

# B 判据符号全覆盖：缺项才打印名字
syms=$(grep -o '^| `[A-Za-z_][A-Za-z_]*' bounds.md | sed 's/^| `//' | sort -u)
for s in hot_bytes journal_bytes index_side commits commit_bytes cumulative_bytes \
         W f cuts max_segment newest_segment open_segment retained hot_over_budget \
         boot_scanned sealed_segments H R B_overhead; do
  echo "$syms" | grep -qx "$s" || echo "MISSING $s"
done
# 无输出＝全覆盖

# C 判据理由串在 §三 逐个在场（限定 §三，避开本文自指）
reasons=$(sed -n '/^## 三/,/^## 四/p' bounds.md)
for r in window-never-fills full-window-generations absolute-breach band-breach \
         reference-growth over-budget boot-scan-exceeds-hot denominator-shrinks \
         cut-frequency-falls; do printf '%s:%s ' "$r" "$(printf '%s' "$reasons" | grep -c -- "$r")"; done
# window-never-fills:1 full-window-generations:1 absolute-breach:1 band-breach:1
# reference-growth:1 over-budget:1 boot-scan-exceeds-hot:1 denominator-shrinks:1
# cut-frequency-falls:1

# D 分离声明在场（限定 §五，避开本文自指）
sed -n '/^## 五/,/^## 六/p' bounds.md | grep -c 'MUST NOT'    # 5＝标题 1＋四条 4
```

四条分离声明与判据层一一对应：`archive_bytes` 对绝对层、`archive_read_latency` 与 `boot_cost` 对恢复层、`cut-frequency-falls`/`denominator-shrinks` 对分母完整性层——每层既有声明又有测量，不存在「只声明不测」的层。
