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

### R2a 阶段量义与独立开闭点（量义冻结；重绑定插点待授权）

六段＋安全校验单列。每一段的开点、闭点都由**该段所属真实调用自己的一对时钟读**取，任何一段都不是另两段相减（`rebind=boot-open` 型代数在这一表里无处落地）；父子关系显式声明，嵌套段与父段不得相加。探针串由真实调用驱动：`journalBytes`／次数／常驻规模的逐代序列出自这些调用，不由计时反推。

| 阶段 | 量义 | 开点（独立时钟读） | 闭点 | 所属调用 | 单位 | 冷/热条件 | 父子 |
|---|---|---|---|---|---|---|---|
| 启动总耗时 `boot` | 一次装配的整段 | 进 `assemble` 前 | `assemble` 返回后 | `noTrimChainRun`（anchored_boot_parity_test.go:283）→`assemble`（run.go:504） | ns | 冷＝开档回放了窗口（`JournalBytesOpen()>0`）；热＝0 | 父＝整代；含开档、安全校验、重绑定 |
| 整代 `generation` | 一代的全部工作（装配＋提交＋结算＋两检查点＋关闭） | 代首，在读日志长度之后 | `Close` 返回后 | 夹具一代 | ns | 按该代所开的根 | 根（无父） |
| 提交 `commit` | 一处会话链上一批提交 | 进 `CommitToSession` 前 | 返回后 | `commitSessionTurns`（checkpoint_test.go:1582）→`CommitToSession` | ns | 写视图 | 父＝整代；含目录维护 |
| 按身份点读 `pointRead` | 一个身份的一次读 | 进 `GetEvent` 前 | 返回后 | `tier.Segments.GetEvent` | ns | 冷＝该读回日志取值（`ValueReads` 增量）；热＝常驻集命中 | 父＝整代 |
| 目录维护 `directory` | 地址表重述：`grow`／`rebuild`／`resetTable` | 进维护分支前（journal.go:1379／1384／1229／1170） | 返回到 `fileOnce` | 写视图提交（`fileOnce`） | ns | 写视图（`statesIndexFiles`） | 父＝提交（嵌套，不得与提交相加） |
| 重绑定 `rebind` | 一次装配的恢复/重绑定 | 进 `restoreChains` 前（run.go:1111） | 折叠结束并记链计数后（run.go:1174） | `restoreCheckpointState`（run.go:1077）→`restoreInMemoryState`（:1055）→`restoreChains` | ns | 同 `boot` | 父＝启动总耗时（嵌套，不得相加） |
| 安全校验 `integrity`（**单列**） | 开档的完整帧校验 | 校验段前 | 段后 | `journalKV` 开档 | ns | 读日志全字节 | 父＝启动总耗时；**单列判读**：许可 O(日志字节)，不并入“恢复成本”结论 |

未归因：`整代 − Σ(独占子区间)` 记作 `unattributed`，只陈述“未归因”，**不得命名为 rebind，也不得借用任何具名阶段的名字**。顺序支路按独占区间相扣，并发支路按时间并集，两者不得混用；一次装配的总耗时与整代工作合计不互比。

**现出口状态与缺口**（取证：`grep -n "started := time.Now()\|time.Since(started)" assembly/run.go` → `1111`／`1174`；全树 `StageSample|stageSample|RestoreElapsed|RebindElapsed|BootStageCost` 0 命中；`assembly/host_diagnostics.go` 无任何时长字段）：

- 测试侧可测（无需生产插点）：`generation`、`boot`、`commit`、`pointRead` 四段——包围 `assemble`、`CommitToSession`、`Segments.GetEvent` 三个真实入口。
- 白名单内可补（`journal.go` 只读计量，不改恢复行为）：`directory` 与 `integrity` 各记一对起止，形状照既有 `LoadScan.Elapsed` 先例（runtime/state/durable.go:691-707）。`directory` 由真实提交驱动（`fileOnce` 在 `used*100 >= capacity*journalSlotLoadPercent` 时 `grow`），故其样本“是否发生”由只读计数判定，不以缺样本冒充没发生。
- **缺口（2026-11-19 已由 R309 授权补齐，见下「R2a 落地」）**：`rebind` 的独立起止只存在于 `assembly/run.go` 内。现行唯一出口是 :1174 的日志行 `elapsed=%s`（`Round(time.Microsecond)`）——**只有时长、没有起止**，既不能满足本表“独立起止”，也无法核父子包含。同包的 `restoreCheckpointState` 虽可再调一次，但按 :1074 的契约“The checkpoint is read once per boot”，第二次调用量的是已恢复之后的幂等重读，不是本代的 rebind，以其充数即错归因。故须在 `assembly/run.go` 增一处**只读**阶段出口（`restoreCheckpointState`／`restoreChains` 的起止对＋只读 getter，零行为变更），或由拥有者裁定该段改归他域取样。

### R2a 落地（2026-11-19，工作树；量义未改）

三处只读出口，零恢复行为变更，导出面未抬：

| 出口 | 落点 | 取法 |
|---|---|---|
| 重绑定 `rebind` | `assembly/run.go` `restoreChains` 首语句与闭合读 | 该调用自己的一对 `time.Now()` 入 `Run.restoreStage`（未导出 `stageSpan`），既有 INFO 行改读该对，文案与 `Round(time.Microsecond)` 不变；`restoreStageSpan()` 未导出，同包测试可读，导出面零新增 |
| 安全校验 `integrity` | `memory/ledger/journal.go` `openJournalLogFace` | `journalReplay` 两侧成对入 `journalLog.integrityBegin/End`，出口 `journalKV.IntegrityPassSpan()`；未创建视图的读面留零对＝该趟未发生 |
| 目录维护 `directory` | `memory/ledger/journal.go` `journalAddress` | `openPass(pass, parent)` 在开点计数、闭点记录（`passes`／`passRecords`），落 `grow`、`rebuild`（`rebuildFrom(parent)`）、`resetTable`（父＝`rebuild`）；出口 `DirectoryMaintenancePasses()`／`DirectoryMaintenancePass(at)` |

**父账户据实现真值分列**：表内「目录维护 父＝提交」只在提交路径成立。`openAddressIndex` 的开档重建（header 不可信／order 落后）走 `rebuild`→`resetTable`，其父是 boot；读视图的 `lookup`／`eachRecord`／`eachNewestFirst` 重建父是 pointRead。故父账户由每次调用显式下传（`state`→`fileCommit`→`file`／`fileOnce`，`derive` 同），不设全局态，也不把开档重建错记为提交成本。此条更正表述、不改量义与判据。

**锚与判定**：`assembly/symptom_anchor_recovery_test.go` 新增 `TestBootStageSamplesHaveIndependentEndpoints`（正锚）与 `TestBootStageSamplesRejectDoubleChargeAndMissingSpan`（负锚），四代真实驱动（`assemble`→`CommitToSession`→`tier.Segments.GetEvent`→registry→`CheckpointSession`／`Checkpoint`／`Close`）；首代以 120 字节键驱动真实 `grow`，第三代开档前删派生 `kv/slots` 驱动真实 `rebuild`+`resetTable`，均非测试直呼维护函数。判定器只判结构：覆盖（样本数＝夹具驱动的调用数，整趟不得缺阶段）、唯一（同阶段同调用一次）、独立起止（不得与他段同对、止必须晚于开）、父子包含；`unattributed = 整代 − Σ(boot,pointRead,commit)` 只陈述未归因。负锚八个注入逐条具名拒，其中漏段与重复两条注入前后 Σ 不变且 ≤ 整代，`sum≤total` 与 `rebind=boot-open` 型判法都会接受——即成对证据。

**如实范围**：夹具每代结清任务，`restoreChains` 折叠的开放账户为空（既有 S2 夹具 260/260 次同样 `chains=0`），rebind 段取真实调用的一对读但未覆盖非空链折叠耗时；非空链耗时归 09.R2b 的隔离对照。

**负锚注入设计**（不依赖 `sum≤total`，也不依赖 `rebind=boot-open`）：判定器只看样本集自身的结构——①**覆盖**：每个必需阶段恰有样本，缺则具名拒“the sample set charges no <阶段> stage”；②**唯一**：同一阶段同一次调用不得记两次费用，重复即具名拒；③**独立起止**：样本起止不得从另一段复制（起止与另一段完全同对，或止不晚于开，则具名拒“not independently opened”）；④**父子包含**。注入用的独立参照＝同一代真实样本序列：重复记账注入使 Σ 仍 ≤ 整代（`sum≤total` 抓不到），漏阶段注入使 Σ 变小（同样抓不到）——两条都只由结构判据具名拒，这才是“可失败”。

R2b 同时交三类证据：①原始 boot/整代及分阶段墙钟、原 1.3× 判决；②实测安全校验线性项、其他阶段及未归因项，不能仅以“字节相等”推导耗时归因；③保持 CRC 安全检查不变的真实性能退化控制，计数不变但耗时增加也应触发墙钟拒绝。合成线性序列只证明判定器有分辨力，删 CRC 的红只证明安全覆盖，二者不替代生产路径性能反例。

**校准门**：同机/镜像/工具链/输入/代序，冷热分记，独立根配对并交替次序，至少十轮且不与编译/race/其他负载实验并发；预热仅剥离编译，不剥离目标冷启动。1.3× 不放宽、不默默撤销墙钟；保留其实际 PASS/FAIL，并与已允许线性项归因并列。噪声、允许成本与新增回归不能分离时判未证，不因超界就归噪声。只有同条件正案、可失败负案及归因均成立才提交校准结论；若原绝对平坦目标与安全成本冲突，显式回到一级目标校正，不在本叶换宽数放行。

### R2b 校准记录（2026-11-19，工作树；同机、同工具链、同输入/深度/缓存）

**条件**：同一台机、同一 `go test` 二进制（编译缓存热，目标冷启动不剥离）、同一夹具形状（`turns=1`、`perGeneration=1`、代代同工）、同深度 252 代、每轮独立 `t.TempDir()` 根（无跨轮共享缓存）；十轮串行，期间不跑编译/race/其他负载实验。命令与原始输出：`GOFLAGS=-buildvcs=false TMPDIR=/tmp go test ./assembly -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=10 -v -timeout 30m` → **exit 0，10 RUN／10 PASS（10 轮各 ~29–48s）**。下表每格为该轮「参照窗（第 1..5 代）中位数 ÷ 受判窗（末 5 代）中位数」的比值，total 列另给两侧毫秒读数。

| 轮 | total 参照/受判 (ms) | total 比值 | 1.3× 判决 | commits | settling | sessions | frame | closing | boot | integrity | boot−integrity−rebind |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | 83.0 / 110.9 | 1.337 | **越界** | 1.062 | 1.081 | 0.974 | 1.003 | 0.997 | 3.045 | 16.136 | 2.785 |
| 2 | 99.7 / 110.8 | 1.112 | 界内 | 0.979 | 0.969 | 0.846 | 0.838 | 0.938 | 1.978 | 10.698 | 1.843 |
| 3 | 97.7 / 111.7 | 1.144 | 界内 | 0.853 | 0.985 | 0.841 | 0.830 | 0.796 | 3.486 | 11.958 | 3.127 |
| 4 | 92.7 / 117.5 | 1.268 | 界内 | 1.043 | 1.009 | 0.859 | 0.905 | 1.001 | 3.451 | 12.978 | 3.010 |
| 5 | 90.0 / 111.0 | 1.232 | 界内 | 0.966 | 1.008 | 1.006 | 0.981 | 0.987 | 3.455 | 8.190 | 3.102 |
| 6 | 93.9 / 103.8 | 1.105 | 界内 | 0.907 | 0.919 | 0.901 | 0.909 | 0.946 | 1.877 | 10.350 | 1.624 |
| 7 | 96.1 / 98.8 | 1.029 | 界内 | 0.937 | 0.842 | 0.903 | 0.969 | 0.962 | 2.105 | 12.347 | 1.699 |
| 8 | 94.9 / 104.0 | 1.096 | 界内 | 0.922 | 0.914 | 0.788 | 0.827 | 0.953 | 2.960 | 14.594 | 2.653 |
| 9 | 93.8 / 86.1 | 0.917 | 界内 | 0.825 | 0.674 | 0.729 | 0.780 | 0.792 | 1.965 | 7.811 | 1.719 |
| 10 | 73.0 / 99.1 | 1.357 | **越界** | 1.185 | 1.260 | 1.050 | 1.031 | 1.154 | 3.917 | 18.757 | 3.366 |

**判决并列（不合并、不互相豁免）**：

| 面 | 十轮读数 | 1.3× 判决 |
|---|---|---|
| 五个工作阶段（锚的受判面） | commits 0.825–1.185、settling 0.674–1.260、sessions 0.729–1.050、frame 0.780–1.031、closing 0.792–1.154 | **10/10 轮在界内**（PASS） |
| boot 配对轴（同深度、同工、两独立根，中位数） | 控制对 median 0.939–1.096（p10 0.755–0.878、p90 1.100–1.497） | **10/10 轮在界内**（PASS）；噪声中位数峰值距界 19% |
| boot 深度分离（深根 p10 vs 控制 p90） | held p10 2.196–3.874 全部高于 control p90 1.163–1.428 | **10/10 轮分离**（增长与噪声可分） |
| 安全完整帧校验（许可的线性项） | 3.27–3.92ms vs 参照 0.13–0.40ms，比值 7.811–18.757 | 线性项实测成立，按 O(日志字节) 单列，不承担其他面的豁免 |
| **boot 减 integrity 减 rebind（未归因残项）** | 比值 1.624–3.366（中位 2.719） | **不豁免**：这截增长不被许可的线性项解释，如实记作未归因 |
| **total（boot＋工作，非锚的判据）** | 比值 0.917–1.357，2/10 轮越过 1.3 | **并列留档**：越界成因是 boot 的未归因残项，不是任一工作阶段（其各阶段均 <1.3） |

**结论与如实边界**：
- 判据（工作阶段 ＋ boot 配对轴）在十轮里都有明确 PASS，且增长侧（深度分离）十轮都在噪声之上 ⇒ **噪声与增长可分**，不属于「区分不了即未证」。
- 但 **total 不是锚的判据**（boot 的声明读随历史增长，故 boot 不被要求平坦），其 2/10 越界是自洽的：越界由 boot 的未归因残项带入。该残项当前**无名无主**，交 06/09.R3 归因，**不得**记入「已接受的线性成本」。
- settling 单轮 1.260 距界仅 3%，且落在该阶段自身跨轮区间（0.674–1.260）内 ⇒ 属亚毫秒阶段的噪声；本表如实标注这一薄余量，不据此把界放宽，也不宣称该阶段有增长余量。

**性能反例（可红；两类注入，安全面不变）**：

1. **计数不变、独立阶段耗时增长**（`TestBootWallClockRejectsUnattributedGrowth`，本叶负锚）。held 根在真实读里重复工作（每 16 字节日志一步），`judgeCountedFacesUnmoved` 逐代核过全部计数面未动（`restored`／index walk visits／value bytes／value reads／slot scan bytes／addressed reads），完整性仍 1:1。十轮结果：控制对 median 0.939–1.095 被接受，held 对 median 1.602–2.334 全部越过 1.3 被**具名拒**（"…not subsumed by the counts"）。命令 `-run '^TestBootWallClockRejectsUnattributedGrowth$' -count=10 -v` → **exit 0，10/10 PASS**（refusal 断言在测内触发；若注入未被拒，该测即 `t.Fatal` 变红）。
2. **全史枚举注入的红/绿对照**（隔离 overlay，真实数据零写）。同一命令 `-run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1`：
   - **红**：overlay 让深根走装饰引擎（`walkLongRunGenerationsOver(..., repeatedWorkSteps)`，深度 24）→ **exit 1**，具名拒 "generation 1 read 196608 slot table bytes scanned, want none: the boot addresses the keys it names, and a full-table enumeration pays the capacity of every family…"；同轮 `integrityBytesScanned` 与 `journalLogBytes` 逐代相等（安全面未变）。
   - **绿**：同一命令在未改树上 → **exit 0**（十轮见上表）。
   overlay 只替换 `assembly/symptom_anchor_longrun_test.go` 的副本（`/tmp/r2b-mutant/`＋`/tmp/r2b-overlay.json`），工作树零写。
3. **不替代项**：合成线性序列只证判定器有分辨力；上表第 2 条的红是**计数面**被枚举顶穿，不是耗时代替计数。真实工作阶段回归的反例在本树尚不存在（无退化实现可注入），故阶段轴的可失败性由合成控制案证明，**这一半不冒充生产路径性能反例**。

**注释形状迁移（原两 `Test` 头注的细节从测试文件搬到本节）**：`assembly/symptom_anchor_longrun_test.go` 的 `TestBootWallClockRejectsUnattributedGrowth` 与 `TestSymptomAnchorPerGenerationCostFlatOverALongRun` 头注此前有 6 行与 4 行超出 160 字符形状上限，按 `comment_policy` 拆短。搬出的细节即：反例三根形状与「两根本实为同一控制带」的用途；held 根每 16 字节一步的注入量与「步数只交给该根」的传递方式；配对读数为何奇偶反转、为何取 21 对；噪声带定义（控制对 p10–p90）与「增长只在中位数清过带时才算分离」；boot 深度分离**只报不判**的理由（restart 读的是链陈述的账户，故 boot 只受计数面与配对轴约束）；长跑锚的单句意图（工作在任意深度等价，boot 的声明读随历史）。

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

## 十三、09.R3a 零填充尾的 torn 判据（09.R3 两片红的取证与修）

**症状链**：(42,256) 与 (7,256) 的 `boot the host: memory: journal frame is corrupt: …/storage/kv/journal.log: record carries no object`（`journal.go:271` 经 `journalSpansAt`，由 `journalReplay` `journal.go:567-586` 交来空 payload）。

**根因（已实测，非推断）**：`journalReplay` 的 torn 判据漏了「长度 0 的帧头」。空 payload 的 CRC32C 为 0，与零填充区的 CRC 字段相等，故全零帧头被当作**零长合法帧**接受：完整性一趟（`visit` 为空操作）静默吃掉整段零填充、`complete == size` 故 `openJournalLogFace` 不判 torn 也不截断；随后真正消费 payload 的重放（`replayWindow`／`derive`／`rebuildFrom`）拿到空 payload，在 `journalSpansAt` 报「record carries no object」。即**同一日志的两个读者对同一段字节判定不一致**：完整性趟说合法，跨距重放说损坏。

**修法（最小）**：`journalReplay` 在 `length == 0 && crc == 0` 时不再当帧：零填充之后（读到日志尾）若全为 0，则该处即 torn 尾并交回 `offset`（写权视图按既有逻辑截断）；若零填充之后还有非零字节，则具名 `ErrJournalCorrupt`（`frame at offset %d carries no payload`），不静默吃、也不越权截断其后字节。

**被否方案**：①在 `journalSpansAt` 拒空 payload——把可自愈的 torn 尾变成永久 boot 失败，且不解决完整性趟与重放趟的分歧；②把任何长度 0 当 torn 直接截断——删掉「零填充后仍有帧」这一真损坏形态的拒绝面，等于放宽判据凑绿。

**取证与验证**：
- 负锚（可红）`TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast`（`memory/ledger/journal_test.go`）：整帧＋零填充（8 字节／4096 字节两形态）两面皆须开成 torn 尾于整帧末尾、写权视图截断、地址索引重建后可读回记录；零填充后接真帧两面皆须 `ErrJournalCorrupt` 且不截其后字节。**修前 overlay 复原同一测试 → exit 1**，两条 torn 子案原文即产品同一句 `…: record carries no object`；**修后同一测试 exit 0**。
- 正锚 `GOFLAGS=-buildvcs=false go test ./memory/ledger/ -count=1` → **exit 0（83.797s）**；`go build ./...` → **exit 0**。
- 未证（不按已核处理）：**零字节尾的来源**未证。本次 (42,256) 复跑（`/tmp/ri-42-256`，wall 293.02s、exit 1、`verdict=unattested`）保留根 `/tmp/ri-keep-42-256`，其 `storage/kv/journal.log` 逐帧核过：**12690 帧、CRC32C 全对、零长帧 0、≥8 字节零游程 0、尾部 0 字节、文件尾与最后一帧尾对齐**——本次红**没有**出现零填充尾，也没有 `record carries no object`。故「零填充尾由崩溃/延迟分配产生」仍只是机理推断，缺一次带零尾的实际进程崩溃取证。
- **本次红是另一症状**（同一 (42,256) 组合）：`smoke_test.go:3643 input identity does not reconcile: missing=[SMOKE-R420560 … SMOKE-R420596]`（37/1200 输入）+ 一条重复动作 + `drain.drained=false`，`unsettled: task=task-f7623ff8 reason=the task stands at accepted`，收尾 `The close reached its budget before the final checkpoint was filed: context deadline exceeded` ＋ `cut with 1 unsettled item(s) on the chain`（`gen120.log:141-142`）。缺失集恰为 gen56 全部 37 条输入：`gen56.json` 记它们每条都 drive 成功且等到应答（ms 36–50，全代存活 1.1s），但 `gen56.log` 仅 144 行、四条 turn 全是启动重绑标记、无一条输入标记。定位到 `smokeCalibrationReconcile` 以「入口模型请求的最后一条 user 正文」计数（`smoke_harness_test.go:744-760`），而该代日志被截断，无法判是子进程侧丢输入还是对账口径错配；该链路的写入面不在本域白名单（`smoke_*`／任务链义务结清在 agent 侧），且 `gen56` 的 `recovered_tasks=38`／`restore folded chains=39` 未见任何 journal 报错。**因果未定 ⇒ 停产上报**（见 tasks.md 09.R3a 状态）。
