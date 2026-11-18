# 09 验收真量 · 设计

> **当前执行口径见 §九/§十及 tasks 的 09.R2a/R2b/R3。** §一–七为原设计，§八/十一/十二为历史取证；其中旧常驻索引、冷层、全 boot 恒定等表述不作当前实施指令。历史 R/G 与旧 ORCHESTRATION 引文均指 [ORCHESTRATION-HISTORY.md](../../ORCHESTRATION-HISTORY.md)，保留原报告文字，不以旧绿推定现树通过。本轮只修计划，未跑测试或校准。

> 静态决策见一级 [design.md](../../design.md) D1（P4）、D4（合单）、D6.6（验收真量）、§五（四量冻结签名）、§六（S2 症状直译锚）。本域只列度量面实施要点与实证触碰集。

## 一、四量探针（承 D6.6，量名冻结于 §五）

| 量 | 语义（本域定义） | 取量点 | 出口 |
|---|---|---|---|
| `journalBytes` | boot **开档解码读入**的 journal 字节 | `journalReplay(file, size, …)` 的入参 `size`／返回 `complete`（memory/ledger/journal.go:433/490） | 今日无出口，需只读探针 |
| `indexEntries` | Load 发布后**常驻索引条目数** | `len(s.index)`（memory/ledger/segment_store.go:373，Load 起于 :281） | 常驻索引规模只读出口，经 `Tier.Segments`（config.go:82）；**非 `EventCount`**（全史标量，§五裁定） |
| `durableLoadCount` | boot 一次 `load` 五前缀**读入键数之和** | 五次 `d.scan`（runtime/state/durable.go:259/270/282/294/305）→ `LoadScan{Keys}`（durable.go:348-362） | 既有 `LoadStats()`（durable.go:209），经 `runCommit.inner`（assembly/commit.go:361-365，包内断言 `*state.DurableCommit`） |
| `wallClock` | 真实 boot 入口一次 assemble 的墙钟 | 夹具在 `assemble` 调用两侧计时（消费者入口 run.go:617/690/2417） | 夹具自计；既有 `TAGENT_BOOT_TRACE`（run.go:572-576）只 env 门控打日志，测试不可读，不改它 |

三条语义裁定（防重演量错面）：

1. **`journalBytes` 不是磁盘厚度**。厚度原是 S1 的面〔**勘误 2026-11-16**：原判「06 退位治」已废——退位整体删除，热层字节增长是显式声明、单独拥有的关切（MUST NOT 被声称有界），S1 只作声明面，不再有界化归属〕；`journalLog.size`（journal.go:501）是 append 偏移（journal.go:582/588 随写增长），**不可复用为开档读量**。探针记开档时刻 `journalReplay` 实际读入的字节，只读、开档后不变。今天二者相等（journal.go:490 整份文件重放）；02 落地后开档读量 < 厚度，06 落地后厚度亦有界——两量分立，各自有锚。
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

## 八、R1 原始 CI 失败清单（HEAD 9547faca）

> 行标识 `CI-1 … CI-8` 在本叶冻结，一行一个失败类。跨域按行引用：04.R1 引 `CI-1`（呈现消费者红复现面）与 `CI-4`（结算应答首因），13.R2 引 `CI-3`／`CI-4`／`CI-5`／`CI-6`（证据绑定字段：headSha、content_hash、退出码、制品路径）。改行文字须同步这两处引用。

**复跑命令（只读，不触发 CI）**

```sh
gh run view 43706847576 --json headSha,conclusion,jobs \
  --jq '{sha:.headSha,conclusion:.conclusion,jobs:[.jobs[]|{n:.name,c:.conclusion}]}'
gh run view 43706843157 --json headSha,conclusion,jobs --jq '{sha:.headSha,conclusion:.conclusion}'
gh run download 43706847576 -n event-integrity-long-seed-1-window-32768 -D /tmp/recovery-09r1/s1w32768
gh run download 43706847576 -n event-integrity-long-seed-1-window-256  -D /tmp/recovery-09r1/s1w256
# 作业日志：CI-1…CI-6 的原文来源（job id 见各行）
gh api repos/SpellingDragon/tagent/actions/jobs/132790514201/logs > /tmp/recovery-09r1/job-132790514201.log  # test
gh api repos/SpellingDragon/tagent/actions/jobs/132790514109/logs > /tmp/recovery-09r1/job-132790514109.log  # race
gh api repos/SpellingDragon/tagent/actions/jobs/132790514302/logs > /tmp/recovery-09r1/job-132790514302.log  # long (1,256)
gh api repos/SpellingDragon/tagent/actions/jobs/132790514209/logs > /tmp/recovery-09r1/job-132790514209.log  # long (7,256)
gh api repos/SpellingDragon/tagent/actions/jobs/132790514272/logs > /tmp/recovery-09r1/job-132790514272.log  # long (42,256)
gh api repos/SpellingDragon/tagent/actions/jobs/132790514349/logs > /tmp/recovery-09r1/job-132790514349.log  # long (1,32768)
gh api repos/SpellingDragon/tagent/actions/jobs/132790514275/logs > /tmp/recovery-09r1/job-132790514275.log  # long (7,32768)
gh api repos/SpellingDragon/tagent/actions/jobs/132790514266/logs > /tmp/recovery-09r1/job-132790514266.log  # long (42,32768)
gh api repos/SpellingDragon/tagent/actions/jobs/132846666017/logs > /tmp/recovery-09r1/job-132846666017.log  # required
```

两个 run（`43706847576`＝`event=pull_request`、`43706843157`＝`event=push`，均 `attempt=1`）同为 `conclusion=failure`，`headSha=9547faca2327c31579e91fbe40a27405d3ffdfb3`（PR #39）。作业集合与结论两 run 相同（`test`／`race`／`event-integrity-long` 六片／`event-integrity-required` 红，`soak` skipped，其余 success）。

### 源树绑定（R19：三件分开记，不混用）

| 绑定面 | 值 | 取法 |
|---|---|---|
| run 的 `headSha` | `9547faca2327c31579e91fbe40a27405d3ffdfb3` | `gh run view … --json headSha` |
| 制品自记 `module_fingerprints[0].revision` | `b04a945e1e8d9b65a9e8356c9cae9b61403587b6`（`dir=examples/wechat-bot`） | `repeat-1/evidence.json`；该值是 GitHub 为 `pull_request` 事件合成的合并提交，本地对象库无此对象（`git cat-file -t` 报 not a valid object），故它不是 `9547faca` |
| 制品自记 `content_hash` | `sha256:baf3fdc5…fb8d233` | 同上 |
| HEAD 树按同算法重算 | `sha256:baf3fdc53b0e7d16fda1e8267e6f9c1ee2f13e0ffc56de48d365906f1fb8d233`（25 个条目） | `git archive 9547faca examples/wechat-bot \| tar -x -C /tmp/recovery-09r1/head-tree`，再按 `run_runtime_scenarios.py:_fingerprint_entries`（`.go/.mod/.sum`＋`testdata`，跳过 `vendor` 等）与 `module_fingerprint` 的 `rel + \0 + bytes` 求 sha256 |

结论：**制品内容按 `content_hash` 与 HEAD 的 `examples/wechat-bot` 源码逐字节相等**；因此行绑定用 `headSha` ＋ `content_hash` 两件声明，不以 `revision` 字段声称等于 HEAD。工作树 13 个未提交文件不进入任何行，也不用于解释本清单任一红。

### 失败清单（一行一个失败类）

| id | 作业 | 结论 | 实际命令／选择器 | 退出码 | 首因一句话 | 证据路径 |
|---|---|---|---|---|---|---|
| `CI-1` | `test`（job 132790514201，步骤 `Test (short)`） | failure | ci.yml:63-64 `go test ./... -short -count=1` | 1 | `recovery_judgment_test.go:117` 判定答出 2 个产物（同名测试的 `:110` 在 `race` 再现） | `job-132790514201.log:2135-2136`、`:2984-2985`、`:3022` |
| `CI-2` | `race`（job 132790514109） | failure | ci.yml:146-147 `./scripts/gates/race_check.sh . ./assembly/... …` | 1 | 同 `TestRestartExecutesTheChainJudgmentOnce`，`recovery_judgment_test.go:110`；无 DATA RACE 报告 | `job-132790514109.log:2297-2298`、`:3130`、`:3174` |
| `CI-3` | `event-integrity-long (1,256)`（job 132790514302）、`(7,256)`（132790514209）、`(42,256)`（132790514272） | failure | ci.yml:320-325 `python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-long --seed <S> --window 256 --repeat 1 --timeout 6270 …` → 内层 `go -C examples/wechat-bot test . -run ^(TestEventIntegrityLongRun)$ -count=1 -timeout 6270s -tags smoke -race -json -args -seed=<S> -window=256` | 内层 124（绑定耗尽）／外层步骤 1 | `ran without a terminal pass`，`repeats: passed=0/1`；超时，非断言红 | `job-132790514302.log:285-290`；制品 `s1w256/repeat-1/{evidence.json,test-log.jsonl}` |
| `CI-4` | `event-integrity-long (1,32768)`（job 132790514349） | failure | 同上，`--window 32768` | 内层 1／外层步骤 1 | gen8 答不出 `settle-ack`：`answer generation 8 session smoke-chat-b round 24: no text … (recorded=25)` | `s1w32768/repeat-1/test-log.jsonl` 第 4 行（首条失败输出＝`smoke_test.go:3578: generation 8 failed: …`）与第 30 行（子日志内的 `smoke_test.go:3571: answer generation 8 …`）；`job-132790514349.log:285-289` |
| `CI-5` | `event-integrity-long (*,32768)`（三片同上） | failure | 同上 | 内层 1 | `durable_scan: unattested`：三前缀键数为零（证据口径，与 `CI-4` 断言红不同类） | `s1w32768/repeat-1/evidence.json.durable_scan`；`job-132790514349.log:289`（三片同文） |
| `CI-6` | `event-integrity-required`（job 132846666017） | failure | ci.yml:379-391（上游结论门）＋ci.yml:406-408 `check_runtime_evidence.py --bundle … --json` | 1（两步各一次） | `errors[0]`＝`evidence.go_test_exit_code must be successful integer exit 0`；`evidence.verdict must be 'match', got 'unattested'` 共六条 | `job-132846666017.log:160`、`:280-345`、`:431` |
| `CI-7` | `soak`（job 132790515186） | skipped | ci.yml:196-198 `if: github.event_name == 'workflow_dispatch'` | — | 该 run 是 `pull_request`，按声明跳过；不是失败也不是通过 | `gh run view … --json jobs` |
| `CI-8` | `event-integrity-short (1..10)`、`event-integrity-mutations`、`openspec`、`validators`、`event-semantics-symptoms` | success | 各自 ci.yml 步骤 | 0 | — | 同上（对照组） |

**逐行原文**（表内简写，判据以此为准；行号均按本地日志文件计）

`CI-1`
```
--- FAIL: TestRestartExecutesTheChainJudgmentOnce (0.07s)
    recovery_judgment_test.go:117: the judgment cmp-e401321cc0412ce0 answers 2 products after the second commit, want one
FAIL	github.com/SpellingDragon/tagent/assembly	171.936s
--- FAIL: TestScenario03ToolReceiptAndBackground (15.00s)
    scenario_s03_test.go:96: the settled bodies of map[call-a:background-marker-a call-b:background-marker-b] did not reach a model view within the wait; recorded views: view 0 [user//"hello"] | view 1 […] | view 2 […]
FAIL	github.com/SpellingDragon/tagent/tests/integration/acceptance	18.051s
##[error]Process completed with exit code 1.
```
【勘误】原行注「另含 bot 与 cli-host 两模块、`GOMAXPROCS=1` execution witnesses、`scripts/lint.sh`」被读成同批已跑：本 run 中 `Test (short)` 先红，其后 `Build/Vet/Test (bot module)`、`Build/Vet/Test (cli-host module)`、`Test (execution witnesses at the scheduling floor)`（ci.yml:100-101）、`Lint`（ci.yml:109）**均为 skipped**（`gh api …/jobs/132790514201` 的 steps 结论）。故本行只覆盖 `go test ./... -short -count=1` 的两处红，其余步骤在本次 run 无证据。

`CI-2`
```
--- FAIL: TestRestartExecutesTheChainJudgmentOnce (0.22s)
    recovery_judgment_test.go:110: the judgment cmp-e401321cc0412ce0 answers 2 products, want one
FAIL	github.com/SpellingDragon/tagent/assembly	324.588s
race_check: FAIL — go test exited 1 with no DATA RACE report (compile error / bad package / timeout / assertion failure). Full output above.
```

`CI-3`（种子 1 原文；7/42 两片同文同码）
```
verdict: unattested
repeat-1/1 run_id=edd57875-248a-4318-9cc0-ee57c5bcb603 verdict=unattested exit=124 log=…/seed-1-window-256/repeat-1/test-log.jsonl evidence=…/seed-1-window-256/repeat-1/evidence.json
  03.11/TestEventIntegrityLongRun            unattested  wechat-bot::.::TestEventIntegrityLongRun ran without a terminal pass
repeats: passed=0/1
##[error]Process completed with exit code 1.
```
种子 7／42 的 `run_id` 分别是 `e9689507-0afd-4a90-9068-19ab08cd2cf5`／`b507a672-5db1-4416-9d74-feab5a30c0a0`，同 `exit=124`、同 `ran without a terminal pass`、同 `passed=0/1`。种子 1 制品有 `gen00.log … gen83.log`（84 代），`gen83.log` 末行 `tagent: calibration heartbeat generation=83 phase=boot processed=0 elapsed=30s`。**退出码分层**：124 是内层 `go test` 的码（`evidence.json.go_test_exit_code: 124`，`liveness.state: expired`），外层步骤报 1；两者不得混写为同一码。

`CI-4`（种子 1 制品；`repeat-1/test-log.jsonl` 按出现顺序）
```
smoke_test.go:3578: generation 8 failed: the generation ended on a failed assertion; the child log names it
…（子日志尾部，日志第 29 行起）
smoke_test.go:3571: answer generation 8 session smoke-chat-b round 24: no text to smoke-chat-b carrying "settle-ack SMOKE-R10097" after 1m0s (recorded=25)
recent outbound:
- text -> smoke-chat-a: settle-ack SMOKE-R10096
- text -> smoke-chat-b: delegate ack SMOKE-R10097   （同文出现两次）
recent requests:
(this process hosts no provider; the requests live in the parent)
```
请求正文（`.tail[1].bodies[102]`，其中 `.tail[1].index == 139`）原文：
```
[evt_settle-7461736b2d35386565643034326639333231383136333063383064663461613265633931333a313a736574746c65643a30|task_settled] tagent: task task-58eed042f932181630c80df4aa2ec913 settled
result:
worker result SMOKE-R10097
```
【勘误】原行写「断言站点是 `smoke_test.go:3571`」。在 HEAD `9547faca` 的 `examples/wechat-bot/smoke_test.go`，`3571` 行原文是 `\t\tsmokeLongHostChild(t)`，`3578` 行是 `\treport, reportPath := smokeLongPrimary(t)`——两者都是**调用点**，不是断言。`smoke_test.go:3571:` 是 Go 测试框架的报位：真正的 `t.Fatalf("answer generation %d session %s round %d: %v", …)` 在 `examples/wechat-bot/smoke_harness_test.go:4149`（函数 `smokeLongHostChild`，其 `:4018` 为 `t.Helper()`），`t.Helper()` 使报位冒泡到非 helper 帧的调用点 `smoke_test.go:3571`；`smoke_test.go:3578` 同理由 `smokeLongPrimary`（`:4472`，`t.Helper()` 在 `:4473`）冒泡，被冒泡的失败文案来自 `smoke_harness_test.go:3508` 的 `t.Fatalf("generation %d failed: %s\n%s", …)`（函数 `smokeCalibrationRunGenerations`，`t.Helper()` 在 `:3456`），其 `%s` 是 `:4060` 写入的 `report.Failure`，其错误正文由 `awaitText`（`smoke_harness_test.go:1591`）产出。故本行同时给出三件：制品报位 `smoke_test.go:3571`、该报位在 HEAD 的原文 `smokeLongHostChild(t)`、以及真正的断言站点 `smoke_harness_test.go:4149`。04.R1 改的是匹配器与会话呈现面，**不要按制品报位去 `smoke_test.go:3571` 找断言**。

`CI-5`（三片同文，只在种子 1 上有制品级佐证）
```
durable_scan: unattested logs=9 durable load scan loaded zero keys for ['tagent/task/v1/spec:', 'tagent/task/v1/consume:', 'tagent/task/v1/fact:']
```
制品侧可见：`evidence.json.durable_scan.state = unattested`，五前缀各 `reads=1`、`keys=0`、`bytes=0`；`logs/gen00.log` 里五条 `tagent: durable load scan prefix=…: keys=0 bytes=0`（空根首启），`gen01–gen08.log` 一条也没有（`gens_without_durable_scan=8/9`；对照同 run 的 256 片为 37/84 且扫描到非零键）。ci.yml:317-319 声明「前缀行缺失或带数据前缀加载零键即判红」，故这是**证据口径的独立失败类**，与 `CI-4` 的断言红不互抵。其「究竟缺日志行还是真零键」本叶未定位，不外推；留给 09.R2／13.R2 按各自判据处理。

`CI-6`
```
errors[0]: "evidence.go_test_exit_code must be successful integer exit 0"
errors[1]: "evidence.verdict must be 'match', got 'unattested'"     ← 全文共六条
errors[2..]: "evidence evidence.json: package wechat-bot::. missing terminal event" / "record 03.11/TestEventIntegrityLongRun: wechat-bot::.::TestEventIntegrityLongRun has no run+pass (run=1, pass=0)" / … "subtests[0..2]: mandatory anchor … verdict 'missing'" …
##[error]Process completed with exit code 1.   （14:39:59 上游结论门）
##[error]Process completed with exit code 1.   （14:40:01 bundle 门）
```
【勘误】原行把 `evidence.verdict …` 记作本行唯一具名失败。实际 `errors` 数组**首条**是 `evidence.go_test_exit_code must be successful integer exit 0`，`verdict` 是第二条；且该作业有两个失败步骤——`Check upstream event results`（ci.yml:379-391，因 `LONG_RESULT=failure`）与 `Check event evidence bundle`（ci.yml:406-408）。六条 `verdict` 计数已核（`grep -c` = 6）。

`CI-7`／`CI-8`：`soak` 与十份 short／mutations／openspec／validators／event-semantics-symptoms 见上表，`gh run view … --json jobs` 一次复跑即得，无首因可记。

**本轮未验证项（如实登记，不按已核处理）**：①`CI-3` 的 7／42 两片、`CI-4`／`CI-5` 的 7／42 两片只取到作业日志级原文（首因文案与码已核），未下载其制品，故其 `test-log.jsonl`／`evidence.json` 内部行序与请求正文未复算；②`CI-5` 缺日志行与真零键的归属未定位（见上）；③原行引用的 `smoke_test.go:535`／`smoke_harness_test.go:447` 保留为历史注记，本叶在 HEAD 上实证到的匹配器是 `smokeSettleNoticeOf`（`smoke_test.go:535`，前缀判据在 `:541`）。

**32768 失败的首因（产物实证，非推断）**：`generations/provider-requests-failed.json` 的 `.tail[1]`（`.tail[1].index == 139`）在 `bodies[102]` 处携带的请求正文是

```
[evt_settle-7461736b…313a30|task_settled] tagent: task task-58eed042f932181630c80df4aa2ec913 settled
result:
worker result SMOKE-R10097
```

即**通知已到模型**，且已带票据前缀；而测试侧匹配器 `smokeSettleNoticeOf`（smoke_test.go:535-547，前缀判据在 `:541-542`）要求 `strings.HasPrefix(content, "tagent: task")`，带票据的正文一条也匹配不上，脚本模型因此走到 `smokeEndedWithTool` 分支改答 `delegate ack SMOKE-R10097`（同日志 recorded 列表可见两次）。据此：

- 原 E1「大窗口三种子均在 gen8 结算应答失败」**成立**，但归因须修正为「测试侧裸前缀匹配器失配」，不是运行时没投递；修复面在 04（呈现消费者红复现）与 no-prefix 交接，不在本域。
- E1 原记的 `smoke_test.go:535` / `smoke_harness_test.go:447` 是辅助函数与脚本模型分支的行号；制品报位是 `smoke_test.go:3571`，但按 `CI-4` 的【勘误】该位置在 HEAD 原文为 `smokeLongHostChild(t)`，真正断言站点是 `smoke_harness_test.go:4149`。
- 256 三片是**超时**不是断言红：`--timeout 6270`（ci.yml:325）与作业 `timeout-minutes: 110`（ci.yml:280）并存，84 代仍未跑完。它与 32768 的断言红是两类失败，不得互抵，也不得据此称 flaky。
- E2 的 `3.42/125.58/445.80s`、启动 `0.018/3.83/17.09s`、重绑定 `0.043/16.35/59.88s` 来自 run43706847576 的小窗口seed1制品14162083707内 gen0/40/80报告，是同次运行的增长证据；不是主干与候选的性能对照，后者待R3同条件取得。复算口径：`3.42/125.58/445.80s`＝`gen{00,40,80}.json` 的 `inputs[].ms` 之和（各 37 项，`3421.22/125575.73/445802.82 ms`）；`0.018/3.83/17.09s`＝同三份的 `boot_ms`（`17.640733/3833.277214/17087.681540 ms`）；`0.043/16.35/59.88s`＝同三份四条 `rebinds[].ms` 之和（`43.03/16348.34/59882.30 ms`）。

## 九、R2 当前计量与校准（未完成；旧义务不因删锚消失）

**旧项承接**：4.1 的取量可靠性、4.3 的启动墙钟与计数、旧 R2 的重复/漏记负例归 R2a；4.4 的隔离退化对照、R4 的长跑转绿与性能可失败性归 R2b。冷读双计与 cut 写漏计的原对象已删，不复活 `ColdReadCost`/`RetireFacts`。历史 [R260–R262](../../ORCHESTRATION-HISTORY.md) 的 `rebind=boot-open`、`open+rebind==boot`、顺序切片 `sum≤totalSpan` 均不能检测生产退化；删掉该测试及专用 helper 只撤伪证据，未交付独立阶段归因。

### 原测量缺陷报告（保留原文；对象已删，不作当前代码描述）

1. **内外计时重复**：`coldEventLocked`（segment_store.go:803-805）的外层窗口包住 `coldViewLocked`（:786-791）内部已计的开档段，开档成本因此被计两次；`RetireFacts` 再把 `report.ColdOpenCost` 累加进同一计数器（:939）。一次冷 miss 读的实际成本与该计数不是同一量。
2. **漏 cut 写**【对象已删，2026-11-16】：原件登记的是「退位只累加两次冷开档，`copyToCold` → `verifyColdCopy` → 热档重写的耗时不在任何阶段里」。退位删除后 `RelocateReport`／`copyToCold`／`verifyColdCopy` 均不存在，该盲区随机制消失。**仍成立的教训**：分阶段计量必须让写侧成本有名有户，不得只计读侧。

两处都是**测量缺陷**，修复不改恢复行为（只读出口的记账面）；判据见 tasks 的 09.R2 负锚。

### 量与成本边界

| 面 | 当前源码事实与后续判据 |
|---|---|
| 原 `indexEntries` | 原义是常驻规模 `len(s.index)`；现 `symptom_anchor_recovery_test.go:122-126` 读 `IndexEntriesRead()`，其 `segment_store.go:1047-1057` 返回 load 枚举次数 `entriesRead`。枚举为零不证明常驻内存为零；R2a 分列两量，常驻载体已删则给删除/剩余缓存上界证据，不把历史序列直接拼接。 |
| replay / integrity / disk | `journalBytes` 是窗口回放量；`integrityBytesScanned` 是完整帧安全校验量；`journalLogBytes` 是磁盘历史规模。已接受后两者增长及安全校验 O(日志字节)，不要求全 boot 恒定，也不能把其他全史读藏进该许可。 |
| 索引/点读/账户 | 枚举次数、walk visits/value bytes/value reads、slot 扫描字节、addressed value reads、durable load 各按实际动作计，不能互相替代。无切片/派生损坏降级另分组，不冒充正常启动。 |
| 时间 | `wallClock`＝一次真实 assemble；`generationWallClock`＝完整代。开档、完整帧校验、提交、身份点读、目录维护、重绑定均记录独立起止、单位 ns、所属调用与冷/热条件；提交/结算/会话与框架 Checkpoint/Close 保持各自墙钟。 |

现 `symptom_anchor_longrun_test.go:232-238` 跳过 boot 平坦判定，`:309-337` 只钉完整性字节 1:1；这两件不能证明“boot 增长全部由 CRC 造成”。同文件 `work` 区间还含 `HistoryTaskIDs` 探针（:117-145），后续应把测量自身成本明确隔离或单列，不能默认五段之和就是整段。

### 独立采样与可失败性

R2a 在真实操作前后独立采样开档、校验、恢复/重绑定、提交、点读、目录维护；保留原始区间，按独占时间或时间并集合计，未归因部分不以差值命名为某阶段。独立外层观测与阶段采样对照，并注入重复记账、漏阶段反例；单纯验证时间片恒等式无效。新锚全名、写面和命令见 tasks 的 R2a；若现出口不能支持采样，明确缺失插点与所属文件后再派，不凭计数推算墙钟。

R2b 同时交三类证据：①原始 boot/整代及分阶段墙钟、原 1.3× 判决；②实测安全校验线性项、其他阶段及未归因项，不能仅以“字节相等”推导耗时归因；③保持 CRC 安全检查不变的真实性能退化控制，计数不变但耗时增加也应触发墙钟拒绝。合成线性序列只证明判定器有分辨力，删 CRC 的红只证明安全覆盖，二者不替代生产路径性能反例。

**校准门**：同机/镜像/工具链/输入/代序，冷热分记，独立根配对并交替次序，至少十轮且不与编译/race/其他负载实验并发；预热仅剥离编译，不剥离目标冷启动。1.3× 不放宽、不默默撤销墙钟；保留其实际 PASS/FAIL，并与已允许线性项归因并列。噪声、允许成本与新增回归不能分离时判未证，不因超界就归噪声。只有同条件正案、可失败负案及归因均成立才提交校准结论；若原绝对平坦目标与安全成本冲突，显式回到一级目标校正，不在本叶换宽数放行。

## 十、R3 最终候选矩阵与同条件对照

**矩阵面（与 `ci.yml` 逐作业对齐）**：`test`、`race`、`openspec`、`validators`、`soak`（仅明确启用时纳入额外档；默认跳过不伪称已跑）、`event-integrity-short (1..10)`、`event-integrity-long (<1|7|42>,<256|32768>)`、`event-integrity-mutations`、`event-integrity-required`、`event-semantics-symptoms`。每项绑定：受测模块及实际本地依赖闭包的 revision＋内容摘要、运行器/检查器身份、命令与参数、退出码、合同摘要（`contract_digest`）、证据产物路径。`examples/wechat-bot/go.mod:5` replace 到根模块，六长也要绑定根源码；当前 runner 仅按所选 module 取指纹不够，实施归 13.R2a。

**对照规则**：baseline固定9547faca，candidate固定待验提交，两份结果各自绑定真实源码身份；两者指纹本应不同，禁止的是冒充另一树的结果而不是合法前后对照。环境、工作量、输入、代序及缓存条件按比较目的保持相同或分栏说明；不可比较的数据只作观察。候选矩阵既要内部同源，还要由 13.R2b 对放行时目标树重算比对；现 `check_bundle_module_fingerprints` 只比证据之间 digest，`match` 不足以表示现树验收闭合。合法 A/B、control/mutant 各按自己的目标树验，不互相要求摘要相同。最终矩阵与正式入口的消费证据归 09.R3/13.R4，不替代独立性能归因。

**权限**：CI 触发与分支保护均属外部写权限，执行前须获明示授权；本轮不执行、不声称已配置。

## 十一、校准记录（旧行与新证据的差）

- **4.2 自证面与代码不符**：行文写「`durableLoadCount > 0` 且逐代增长」；现行夹具（symptom_anchor_recovery_test.go:148-179）改按「每代 settle 数 == 声明 ∧ 身份跨代不重复 ∧ `durable.TaskIDs()` 逐代增长 ∧ `journalBytes != 0` ∧ 分母末代 > 第二代」判，并明写「本夹具把 checkpoint 排在最后，因此该账户由 checkpoint 整体陈述、不付任何 durable 前缀键」，durable 面留给 `TestTaskRestoreBoundedByCheckpoint`。**交付仍成立（复跑 exit 0）**，须按代码改写行文，勿以旧字面复跑。
- **4.3 状态已变**：行文记「01·1.2/1.3、02·2.1/2.2 未落地，正锚仍红」；今日复跑 S2 锚 exit 0（15.820s）。正锚由红转绿属实测，但该行仍不勾：其负锚（以正文计数顶替真量即红）未取证，`-count=10` 与 race 统计未做，且 §九 口径纠正落地前锚的判定面本身要改。
- **4.4 仍不可判定**：`journalBytes`/`indexEntries`/`durableLoadCount` 三量是开档时刻量，boot 与 `seedFullReplay` 走同一 `OpenTier`，两值恒等，「boot × 2 < 对照」在任何实现下恒假；该行须由编排者重裁，不得以调断言迁就。

## 十二、09.R4 长跑每代成本锚（症状直译；退位反转的前置）

### 为什么必须现在立

现 S2 `TestSymptomAnchorBootCostFlatAcrossGenerations` 的非空转守卫 `if observed[1].coldReadWallClock <= 0` **要求必须读到冷层**；退位机制彻底删除后冷层不存在，该量恒 0 ⇒ S2 永久红。故新锚是反转的**前置条件**。它的目标面不再是 boot 平坦，而是**每代总成本随链增长而平坦**（用户原症状：长时间压力测试越跑越慢直到超时）。旧 S2 保留（boot 四量判据为追加项，5.11 翻译不可替换），其冷读守卫已就地标为依赖退位。

### 夹具与判定（`assembly/symptom_anchor_longrun_test.go`）

| 项 | 取值 | 依据 |
|---|---|---|
| 入口链 | `noTrimChainRun`（`assemble` → `restoreCheckpointState` → `seedAnchoredReplay`）→ `CommitToSession` → `settleTasks`（registry `Register`/`Advance`）→ `CheckpointSession` → `Checkpoint` → `Close` | 四个消费者都是产品正式入口，无手工 rebuild |
| 每代工作量 | `turns=1`、`perGeneration=1`，代代相同 | 成本比较必须同工同酬；工作量恒定才使「总成本」可比 |
| 代序深度 | `generations=252` | = 超时那份长跑（CI-3，`gen00..gen83`）的 **3 倍**；下表是实测推导 |
| 计时面 | 两段互不重叠：`assemble` 段；提交→结算→两次检查点→关闭段。两个计数探针读在两段之间，不进任何一段 | 探针自身不得进入成本（§七 度量扰动） |
| 分母自证 | 每代 `EventCount` 增量 > 0；每代 `restored` 严格大于上一代；每代恢复的任务账户 **恰好** = `perGeneration × 代序`；任务身份跨代不重复 | 空转夹具即具名拒绝，故「平坦」不是「什么都没做」 |
| 判定统计量 | 参照窗 = 第 1..5 代中位数（**跳过 0 号**：它开空根、只陈述无历史时的成本）；受判窗 = 末 5 代中位数；受判 ≤ 参照 × 界 | 中位数抗单次调度抖动 |
| 界 | **1.3（13 tenths），候选值** | 见「界的状态」 |

**深度实测推导**（本机、工作树、同形状；比值 = 末窗中位数 ÷ 首窗中位数）：

| 形状 | 深度 | 比值（多次实测） | 单次墙钟 |
|---|---|---|---|
| `turns=2, perGen=2` | 84 | 1.26 / 1.29 / 1.63 | 19–21s |
| `turns=1, perGen=1` | 168 | 1.43 / 1.72 / 1.99 | 27–31s |
| `turns=1, perGen=1` | 252 | **1.61 / 2.12 / 2.30** | 47–48s |

比值随深度线性上升、与每代工作量无关（增长项 ∝ 累计历史 ∝ 代序 × 每代事实；固定项 ∝ 每代事实，二者同比例缩放故比值只由代序定）。84 代（症状自身的起点）实测只到 1.26–1.29，与同条件噪声同量级 ⇒ **不足以判定**；252 代把最低实测值抬到 1.61，才在界 1.3 之上留下余量。深度与窗口的最终取值归 09.R2 校准。

### 三个控制案（同一锚内，先于受判断言执行）

1. **平坦接受**：把每代成本填成常数，判定必须**不拒**——否则判定是「见谁都拒」的恒真界。
2. **随历史线性即拒**：用本夹具实测的 `restored` 作比例，把受测序列缩放成「每代成本正比于其开档历史」，判定必须**具名拒**。这条与真实序列是否平坦无关，故反转后仍具鉴别力（证明界可失败，非恒真）。
3. **同条件离散守卫**：两个独立根、同一形状、同一深度各走一次，中位数之比必须落在界内；超界即具名声明「界落在本机噪声内，序列判定不了任何增长」，不得伪装成产品增长。

### 界的状态（**候选，不是冻结裁决**）

R19 N1 已撤回「DoD 命名了 2×」这一伪造权威，故本锚**不沿用 2×**。1.3 的实测两侧余量：

- **噪声侧**：同条件两次独立根同形状的比值实测 **1.025 / 1.068 / 1.130**（锚内控制案，每次复跑打印这两个读数），故 1.3 在噪声之上约 15%。
- **增长侧**：本树在 252 代上的受判比值实测 **1.655 / 1.694 / 1.954**（末轮 `-count=3`），故 1.3 在其下且最低余量约 27%。
- **未结**：界的校准（含深度/窗口再推导）归 09.R2；若反转后判定为平坦而比值仍超界，说明界落在噪声内，须按同条件校准重定界，**不得改锚迁就**。

### 旧 S2 的处置（保留，不替换）

`TestSymptomAnchorBootCostFlatAcrossGenerations`（`assembly/symptom_anchor_recovery_test.go`）原样保留：四量判据（`journalBytes`/`indexEntries`/`durableLoadCount`/wallClock）仍是追加项，新锚不得替掉它。其冷读守卫上方已加注释声明**该面只存在于退位旁**：不搬冷的 tier 在此答 0，守卫会具名失败 ⇒ 反转落地时该守卫必须随 S2 一并重定，不得静默留雷。

### 未证（不按已核处理）

- **反转后转绿未验**：本叶只交红证据。用 `boot.tier.Segments.WithRetirement(false)` 做的旁证显示：关掉退位后 boot 面 9.7ms→73.2ms（7.5×）、每代总成本 1.75× —— **仅删退位不足以转绿**，开档/点读仍随历史增长（R72 N24 同族）。故绿的条件是可寻址索引落地，非「删退位」本身。该旁证用 `WithRetirement` 开关，**不进锚文件**（该符号随反转删除）。
- **本机绝对数受负载影响**：比值随深度与统计窗稳定，绝对 ms 不稳定，故判据只用比值。
- **深度/窗口的最终取值**、以及 `wallClock` 是否需按 09.R2 分阶段口径重算，归 09.R2。

### 红证据（原文，工作树 `assembly/`；HEAD `9547faca`＋未提交候选）

```
go test ./assembly/ -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1
EXIT=1
--- FAIL: TestSymptomAnchorPerGenerationCostFlatOverALongRun (48.44s)
    symptom_anchor_longrun_test.go:283: the per-generation cost grows with the chain: generation 247 beside generations 1-5:
    the middle generation of the last 5 costs 226.843584ms beside the 133.895583ms of the first 5,
    over the bound of 174.064257ms (13 tenths of the reference), so the whole cost of a generation grows
    with the chain it walked (the series below is indexed by generation, 0..251)
```
