# Run notes: code-vocabulary-alignment

> 收尾 F 任务的逐项核验凭据。命令均在真实仓根 `/Users/pengweiye/Documents/codes/tagent` 跑（除 F2/F3 因果在干净克隆 `/tmp/f2_clean`）。退出码取原始码，非管道吞码。基线＝main `a0ff631b`。

> **甲 修订（取代旧「指纹棘轮＋泛义保留」方案）。** 以下 F0-F3／W0-W3／Issue1-7／R35／提交分离计划系**旧方案**执行凭据，已被甲取代，保留作历史审计（只附勘误不改写）。旧方案据伪前提「user_ruling 泛义保留」（执行期 agent 决定被误标为用户裁定）建 2377 行分类基线豁免 ~340 泛义 carrier；用户明确「我没有裁定过泛义保留」。甲＝carrier 全退役＋纯短语禁门（无基线/分类/棘轮/豁免）＋删基线，净负代码。**甲 轮凭据见文末「甲 修订轮」节**（红锚 390、门重写、A/B1/B2/D/E 轨、验证计划、甲提交分离计划）。旧「20 例 56 断言／28 例 80 断言／baseline=262/263／mirror/exempt」等数字均属旧指纹门；甲门为 **17 例 52 断言、无基线**。旧 Issue3「task_checkpoint_test.go:174 回退泛义」被甲取代（→durable commit）。

## F0 godoc 重生成（编排者单写）

- `bash scripts/gates/gen_godoc.sh` → exit 0，写 80 文件（79 包＋index；含外来 migrate-kv 包）。
- `bash scripts/gates/gen_godoc.sh --check` → exit 0，`docs/api matches the source (79 packages)`。
- 刷新的投影＝本变更注释驱动的 3 文件：`memory__ledger.md`（12 行，源 segment_store.go:678/682）、`memory__replay.md`（20 行，源 age_segments.go:7-8）、`runtime__task.md`（12 行，源 checkpoint.go:8／restore.go:100/178）。
- `docs/api/index.md`（migrate-kv 行＋包总数 78→79）与 `github_com__SpellingDragon__tagent__scripts__migrate-kv.md` 为**外来**派生输出，重生成逐字复现，非本变更词汇内容；提交时不入本变更暂存。
- 时机耦合实证：D-01 改 Go doc 注释后，旧投影 mirror 豁免失效→门余 6 红（全 docs/api）；F0 重生成使投影复位→门转绿。符合 ORCHESTRATION 2.4 释放表（docs/api 待 01+03 .go 改定后编排者 F0 单写）。

## F1 契约零改动断言

- 契约面 `git diff --stat a0ff631b -- api/event api/config golden/runtime/architecture.json` → **空**（exit 0）。
- 冻结契约拼写（GLOSSARY §五 reincarnation 族）：`verify_contracts.py` 实为归档一次性脚本（`openspec/changes/archive/2026-11-08-restart-recovery-optimization/changes/01-vocabulary/verify_contracts.py`，BASE=1c4e165f，6 needle×10 文件），非现仓门。等价断言以直接基线 diff 替代：`git diff --stat a0ff631b -- <该脚本 FILES 列出的 10 文件>` → **空**（exit 0），10 文件逐字未变，冻结拼写零更名。
- 现仓契约门（check_runtime_architecture.py／check_runtime_plan.py／check_runtime_evidence.py／event-ownership）在 F2 全量 lint 内实跑绿。
- 03 授权 golden＝`golden/runtime/window_bounds.json:218/227`（载体→checkpoint），不在 F1 契约面（architecture.json）内；`check_window_bounds.py` exit 0。

## F2 全量门禁

- **干净克隆** `/tmp/f2_clean`＝`git clone` a0ff631b（.git 为目录，dotted-refs 门可跑）＋仅施加本变更（18 跟踪文件 patch＋4 新文件：门/自测/基线/openspec 变更目录），**排除全部外来文件**；克隆内 `gen_godoc.sh` 重生成得 78 包（无外来 migrate-kv）。
- `bash scripts/gates/lint.sh`（克隆，全量）→ **exit 0**，`lint: ok`。
- 消费者锚实证：全量 lint 内 policy 块实跑本门——`test_check_vocabulary_alignment.py` 20 例 56 断言 PASS；`check_vocabulary_alignment.py` `violations=0 baseline=262 stale=0 errors=0` `ok`；`check-openspec.sh` `✓ change/code-vocabulary-alignment`（strict-valid）。
- 克隆自测块 `Ran 271 tests OK`（无外来 test_check_runtime_symptoms.py 的 10 例）。
- **真实仓根** `bash scripts/gates/lint.sh --only policy` → exit 1，唯一失败＝外来未跟踪 `test_check_runtime_symptoms.SymptomCheckerTest.test_registry_matches_the_module`（281 例 1 败，SYMPTOM_PARENTS 注册表失配，纯外来文件内部）。该文件 CI 克隆不存在；它在 lint.sh:98 自测块中止，使本门（lint.sh:128-129）在真树未被到达。归因：外来红，非本变更（本变更未触该文件及其依赖 check_event_semantics_symptoms.py）。干净克隆 exit 0 即 CI 等价证明。
- doc_budget（克隆）RESULT ok：231 文件/21957 行，slack 0；本变更 spec delta 属 openspec/changes 在途，未计入权威 specs，故 pre-archive 不触棘轮。F4 归档合并 delta 入 specs/architecture-guardrails 后行数增，需具名上调（M4 先例，slack 0 无余量）。
- spec_hygiene（克隆）ok：specs=151 requirements=779 violations=285 baseline=286 stale=1 errors=0。

## F3 DoD 六项

### ① 词汇门红转绿因果（症状直译锚）

完整 round-trip，门＝新建，因果取「回退清理」而非「回滚门」：

| 态 | 命令 | 退出码 | 违规 |
|---|---|---|---|
| 清理前（W0 症状锚） | `check_vocabulary_alignment.py`（真树，01 未清） | 1 | 38 具名 |
| 01+F0+基线刷新后 | `check_vocabulary_alignment.py`（真树） | 0 | 0（coverage carrier=389/389） |
| 仅回退 01 清理 | 克隆 `git checkout a0ff631b -- <11 个 01 .go>` 后跑门 | 1 | 38 具名（carrier 38＋compression-generation 1，count 39） |
| 恢复 01 清理 | 克隆 cp 回 11 文件后跑门 | 0 | 0 |

- 回退仅 01 的 11 个 .go（生产 7＋测试 4），保留 D-03 结构改动＋门＋刷新基线；复红 38 与 W0 症状锚同。证明绿由清理致，非基线操纵。
- 负例自测真拒：`test_check_vocabulary_alignment.py` exit 0，20 例 56 断言（4 类植入违规被拒＋3 类合法豁免不误拒）。

### ② 负例自测拒植入违规

`python3 scripts/gates/test_check_vocabulary_alignment.py` → exit 0，`20 case(s) PASS (56 assertions)`。植入检查点义 carrier／跨行 folding generation／api·plugin Product／退役词必拒；泛义 carrier／gen: 锚／instance_id／compress_product 合法不误拒。

### ③ 全量 lint exit 0

见 F2：干净克隆 `bash scripts/gates/lint.sh` exit 0（原始码）。

### ④ docs/api diff 仅 godoc

见 F0：3 投影文件注释驱动刷新；index.md／migrate-kv.md 外来派生复现，提交分离。`gen_godoc.sh --check` exit 0。

### ⑤ 冻结归档仅附勘误

- `13-bounded-compression/tasks.md`：+1 勘误行（TestJournalCarriesTheFrameworkCarrierFrame→…CheckpointFrame），0 删除，原文逐字不变。
- `session-checkpoint-material-unification/tasks.md`：+1 勘误行（TestTheCheckpointCarrierStaysBoundedByItsEntries 非真名→TestTheCheckpointStaysBoundedByItsEntries，assembly/checkpoint_test.go:861），0 删除。
- `git diff --stat` 两归档＝2 files changed, 2 insertions(+)，无 deletion。

### ⑥ 外来未跟踪文件零触碰

- 7 外来未跟踪路径全在且仍未跟踪：`scripts/migrate-kv/`、`scripts/gates/check_event_semantics_symptoms.py`、`scripts/gates/runtime_budgets.py`、`scripts/gates/test_check_runtime_symptoms.py`、`examples/wechat-bot/skills/interview/agent-interview-helper/`、`openspec/changes/runtime-architecture-convergence/`、`docs/api/github_com__SpellingDragon__tagent__scripts__migrate-kv.md`。
- 3 外来跟踪改动（examples/wechat-bot/main.go +5、README.md +4-2、tagent.yaml +9-4）本变更未编辑。
- migrate-kv.md 纯外来派生（migrate-kv 命令文档，ledger.MigrateShardedRoot），无本变更词汇注入；F0 重生成逐字复现。
- 干净克隆 F2 全量 lint 排除全部外来文件仍 exit 0＝本变更独立于外来工作。

## 基线刷新（编排者，F0 后）

- `check_vocabulary_alignment.py --write-baseline` → exit 0，`wrote 262 baseline entr(ies)`。
- 前：300 条（262 generic-carrier＋38 violation）；后：262 条全 generic-carrier，0 violation／0 unreviewed。38 条 stale violation 清除（清理后不再命中）。
- ADR 元数据逐字保留：adr=docs/adr/0006-three-hooks-is-the-core-iteration-principle.md、user_ruling、review_by=2027-06-30、reason、schema_version、doc_glob。

## 编译与测试

- `go build ./...` exit 0；`go vet ./...` exit 0（均 0 行输出）。
- `go test ./memory/ledger/ ./memory/replay/ ./runtime/task/ -count=1` exit 0（ledger 18.339s／replay cached／task 0.615s）。
- `go test ./assembly/ ./runtime/state/ ./tests/integration/execution/ -count=1` exit 0（assembly 256.272s／runtime/state 0.725s／tests/integration/execution 48.438s）——D-01/D-03 改动的测试包独立复验绿。
- 旧名残留 grep（carrier-old／carrier-new／carrierFrameValueBytes／TestJournalCarriesTheFrameworkCarrierFrame／checkpointSliceMailbox）全仓 .go **0 命中**。
- 生产 7 文件 git diff 仅注释行（逻辑零改）逐处实证。

## 评审修复轮（R35 第二轮：独立只读审者发现 2 缺陷，已修＋复验）

独立审者对照原验收面拷问，发现 2 可修缺陷＋1 范围外残留。按「禁追认缩水」「设计意图量化机械检测偏离」收紧检测而非缩 claim，逐处修复并复验。

### Issue 3：过度更名回退（违反本变更自身裁定）

- `assembly/task_checkpoint_test.go:174` 曾被误改「durable carrier」→「durable checkpoint」。指代物 :172＝`booted.commit.inner.(*state.DurableCommit)`，DurableCommit 是泛义提交承载面（durable.go「DurableCommit is the durable carrier behind the task commit port」、commit.go「runCommit is the commit carrier」`inner commitCarrier`），非检查点事实。user_ruling 明保 commitCarrier/DurableCommit/MemoryCommit/MemoryStore。
- 修：回退 :174 为「durable carrier」；基线增 generic-carrier 条目（digest 4335bcdf…）；spec delta L13 从检查点义示例移除「durable carrier to fold the chain」，改列泛义提交承载面（commitCarrier/DurableCommit）。

### Issue 2：门 claim 超出 behavior（收紧检测，非缩 claim）

门 docstring/spec 称扫「标识符与注释」，但禁用短语原仅扫注释散文，Product 原用 `\bProduct\b` 漏驼峰复合物。收紧：
- Product：`\bProduct\b` → 大小写敏感子串 `Product`（命中 MaterialProduct/pluginProduct，不命中 api/plugin 小写 product 散文——实证该目录全小写）。
- 短语正则：`fold\s+generation`→`fold\s*generation`、`folding\s+generation`→`folding\s*generation`（零空格命中驼峰 foldGeneration）。
- scan_line：短语逐行扫全部文本（标识符/字符串字面量/单行注释）。
- scan_prose：只记跨注释续行命中（spans_pieces：match 起止落不同行），单行命中已由 scan_line 记录，避免重复计数；finditer 覆盖一段多跨行。
- scan_tree：.go 与 docs/api 逐行均加 PROSE_RULES。
- docstring 修正：carrier 退役权威改引基线 user_ruling（W8-VOCAB）＋GLOSSARY 泛义保留，删伪引「GLOSSARY line 31」（实为事实族表头）；删已更名示例 carrierFrameValueBytes，改用 commitCarrier/MaterialProduct。

### 连带更名：stripFoldGenerations → stripRecorderGenerations

- 收紧后短语规则在 `tests/integration/history/event_integrity_test.go` 命中 `stripFoldGenerations`（注释/func/2 调用，共 4 处）。该函数剥离的是**记录世代**（gen: 锚令牌），GLOSSARY 行 27 规范词＝记录世代/recorder generation；旧名撞行 41 禁用短语「fold generation」形。
- 裁：按规范词更名 stripRecorderGenerations（甲，纯粹终点：名实相符、消除与禁用形碰撞、短语规则归零空槽），非乙（保留旧名＋legitimate-generation 豁免，留混淆名与基线熵）。
- 实证非冻结：grep openspec/scripts/golden 零引用；非 Test* 冻结名；纯标识符更名，行为不变。

### 复验凭据（修复后，原始退出码）

| 项 | 命令 | 退出码 | 关键数 |
|---|---|---|---|
| 门 | `check_vocabulary_alignment.py`（真树） | 0 | hits=390 mirror=49 exempt=341 violations=0 baseline=263 stale=0；carrier=390/390，余规则 0/0 |
| 自测 | `test_check_vocabulary_alignment.py` | 0 | 28 例 80 断言（＋8 负锚：复合物 Product／foldGeneration／generationalBand 标识符／字符串字面量／单行只计一次／小写 product 散文不拒／recorder generation 标识符不拒／doc 短语漂移红） |
| 编译 | `go build ./...` | 0 | — |
| vet | `go vet ./assembly/... ./tests/integration/history/...` | 0 | — |
| 测试 | `go test ./assembly/ -run TestTaskRestoreBoundedByCheckpoint` | 0 | ok 5.591s（实跑非空转） |
| 测试 | `go test ./tests/integration/history/ -run TestEventIntegrityProjection` | 0 | ok 0.760s（实跑非空转） |
| gofmt | `gofmt -l` 两改文件 | 0 | 空（已格式化） |
| 全量 lint | 干净克隆 `bash scripts/gates/lint.sh`（同步 7 改文件后） | 0 | lint: ok；门 ok（787 go/79 docs，violations=0 baseline=263）；自测 28 例 80 断言；check-openspec ✓ change/code-vocabulary-alignment |

- 基线 262→263（＋1 generic-carrier for :174）；carrier 命中 389→390（回退 :174 复现 carrier 子串）。收紧未引入任何意外新命中（compression-generation/plugin-product/retired-word 均 0/0）。
- 真树 `lint.sh --only policy` 仍 exit 1，唯一红＝外来 test_check_runtime_symptoms.py（lint.sh:98 自测块中止，未达本门 :128）；干净克隆（无外来）exit 0 即 CI 等价。
- 本节数取代 F2/F3 原数（baseline 262→263、自测 20→28、carrier 389→390）；F0-F3 历史段保留原貌作审计轨迹。

### Issue 7：范围外残留 → 用户授权修复（W3）

**原 flag（评审第二轮，范围外上报）**：
- `openspec/specs/session-checkpoint-material/spec.md:10/47/49/52` 用检查点义中文「载体」（如「新建载体 SHALL 复用 checkpoint:<partitionSeq>」「载体大小 SHALL 有界于开放项数量」）。
- 双重范围外：①文件属 openspec/specs/，不在门 .go＋docs/api 扫描面；②门 carrier needle＝英文「carrier」，不扫中文「载体」。
- 按 GLOSSARY/user_ruling 检查点义应 align checkpoint，但属**永久 spec 文档**改动＋门外，不自行扩门扫描面（中文/specs），留用户裁是否纳入后续文档词汇对齐。

**用户裁定（Q2）**：「一并改 spec 中文载体」——纯文档对齐达干净终点，门扫描面不变。Q1「先不动我要复核」→不提交/不推送/不归档，工作树留用户复核。

**修复（W3，编排者直管）**：
- 直接编辑 living spec（非 delta）：检查点义中文载体→checkpoint 共 5 处（:10 新建载体／:47 不嵌入载体＋载体大小／:49 Scenario 已结集合不进入载体／:52 载体大小）；:38「端口名 SHALL NOT 是语义的唯一载体」**泛义保留**（carrier-of-meaning，同 commitCarrier/DurableCommit 泛义）。
- 依据 openspec/AGENTS.md：主 spec 禁 delta 语法、须结构有效（Purpose/Requirements/SHALL/Scenario）；词汇规范化语义不变、结构 intact，直接编辑合规。归档记录 session-checkpoint-material-unification **冻结不动**（只改 living spec，非历史审计记录）。
- 复验（原始退出码）：`grep -n 载体` 仅余 :38；`git diff --stat`＝4 行词替换（4+/4-，结构/语义零改）；spec_hygiene exit0（specs151/req779/violations285/baseline286/stale1，与基线无漂移）；doc_budget exit0（231 文件/21957 行 slack0，词替换不增行故不触棘轮）；check-openspec exit0（✓ change/code-vocabulary-alignment，主 spec 结构仍有效）；词汇门 exit0（263/0，face .go＋docs/api 英文不变）。
- 门扫描面**未扩**（中文/specs 仍门外）；本修复是文档词汇对齐，非门能力扩展。F4 architecture-guardrails delta 合并仍需 doc-budget 具名上调（Issue7 不增行，不影响该上调）。

### 独立审者第二轮裁定（R35 末轮：FRONTIER EMPTY）

独立只读审者（general-purpose 子代理，29 工具调用）对照原验收面逐项复验三修，skeptical probing 未发现剩余或新引入缺陷。裁定 **FRONTIER EMPTY**，无 release-blocking／non-blocking 问题。扫描范围＝全仓 .go＋docs/api＋openspec/changes/archive＋golden＋mutation lists＋ci.yml 接线。

逐项证据（审者独立复跑，非信汇报）：
- 命令全 exit 0：门 `go_files=788 api_docs=80 hits=390 mirror=49 exempt=341 violations=0 baseline=263 stale=0`；自测 28 例 80 断言；`go build ./...`＝0；`go vet ./assembly/... ./tests/integration/history/...`＝0。
- FIX 1（回退）：DurableCommit 泛义提交承载面实证（durable.go:159「durable carrier behind the task commit port」、:217-218 `var _ task.Commit`；commit.go:346/355/362 commitCarrier/runCommit/inner）；:172 指代物＝`booted.commit.inner.(*state.DurableCommit)`，:174「durable carrier」正确；基线 generic-carrier 条目 preview 匹配回退文本；`git diff HEAD` 净空；spec delta L13 检查点例＝「framework carrier」、泛义例＝commitCarrier/DurableCommit，「durable carrier to fold the chain」已移除。
- FIX 2（收紧非缩 claim）：独立逐指纹探针——单行＝1、跨行＝1（捕获）、一全行＋一跨行＝**2 不同指纹**（真两次出现，非重复计数）、MaterialProduct＋pluginProduct＝2；recorder generation／gen:／stripRecorderGenerations **零命中**；小写 product 散文不拒；基线零漂移（stale=0 violations=0，263 条全 carrier/generic-carrier，compression-generation＆plugin-product coverage 0/0＝语料真净）；docstring/spec 与 behavior 相符。
- FIX 3（更名）：.go 零 stripFoldGenerations 残留；4 处更名（2 调用 :2093、注释 :5318、定义 :5324）；函数体纯 `|gen:`→`|gen:?]` 令牌剥离，行为不变；旧名仅现于本变更 run-notes.md:104/106 更名叙事，**不在** archive／golden／mutation list（archive grep rc=1）；自测 fixture 用新名；收紧短语规则不命中。
- 接线：ci.yml:109 → scripts/lint.sh:10（exec gates/lint.sh）→ gates/lint.sh:128-129，在 :80 开启的 --only policy 块内。正式 CI 消费者确认。
- Issue 7 范围外确认：session-checkpoint-material/spec.md:10/47/49/52 中文「载体」；doc_glob=docs/api/*.md 排除该 openspec .md，且载体≠英文 carrier——双重门外。非阻塞。

## 提交分离计划（待用户明示后执行）

本变更暂存面（排除外来）：
- 跟踪改动 18 文件：assembly/checkpoint.go、assembly/checkpoint_test.go、assembly/invoke.go、golden/runtime/window_bounds.json、memory/ledger/{journal.go,journal_test.go,segment_store.go,segment_store_test.go}、memory/replay/age_segments.go、runtime/state/memory.go、runtime/task/{checkpoint.go,restore.go}、scripts/gates/{lint.sh,test_check_runtime_plan.py}、tests/integration/execution/settle_wake_account_test.go、tests/integration/history/event_integrity_test.go、两冻结归档 tasks.md。
- docs/api 3 投影：memory__ledger.md、memory__replay.md、runtime__task.md。
- 永久 spec 词汇对齐（Issue7／W3，用户授权）：`openspec/specs/session-checkpoint-material/spec.md`（检查点义中文载体→checkpoint，4 行词替换；直接编辑主 spec 非 delta，F4 归档不合并此文件，作为独立跟踪改动随本变更提交）。
- 新文件：scripts/gates/check_vocabulary_alignment.py、scripts/gates/test_check_vocabulary_alignment.py、golden/runtime/vocabulary_alignment.json、openspec/changes/code-vocabulary-alignment/**。
- **评审修复轮对暂存面的净影响**（取代 F2 历史「18 跟踪」组成）：①`assembly/task_checkpoint_test.go` Issue 3 回退后对 main `a0ff631b` 净 diff 为空（`git diff --stat` 实证），**不入暂存**；②`tests/integration/history/event_integrity_test.go` 连带更名 stripFoldGenerations→stripRecorderGenerations（4 处），**新增入暂存**。一出一入，跟踪改动仍 18（代码/测试/golden/脚本/归档）＋3 docs/api 投影＝21，＋Issue7 永久 spec 1＝**22 跟踪改动文件**。
- **不入暂存（外来）**：docs/api/index.md、docs/api/github_com__…migrate-kv.md、examples/wechat-bot/{main.go,README.md,tagent.yaml}、scripts/migrate-kv/、scripts/gates/{check_event_semantics_symptoms.py,runtime_budgets.py,test_check_runtime_symptoms.py}、examples/wechat-bot/skills/interview/agent-interview-helper/、openspec/changes/runtime-architecture-convergence/。
---

## 甲 修订轮（取代旧指纹棘轮方案；权威映射见 design.md D6）

### 前提勘误

旧方案把「GLOSSARY 保留泛义 carrier、仅检查点义退役」当立案前提，据此建 2377 行分类基线豁免 ~340 泛义 carrier＋550 行指纹棘轮门。该前提系执行期 agent 决定被误标为 user_ruling（W8-VOCAB）；用户明确「我没有裁定过泛义保留」。GLOSSARY 行 3「描述机制一律用代码实名」未登记 carrier；§五冻结仅 reincarnation 族。甲＝carrier **全退役**（每处改既有精确实名，不造新概念）＋**纯短语禁门**（无基线/分类/棘轮/镜像豁免）＋删基线，净负代码。旧已落地的检查点义改名／age_segments folding generation→band promotion／stripFoldGenerations→stripRecorderGenerations／03 结构 golden 经复核映射正确，保留。

### 红锚捕获（症状现，A/B 派发前）

- `python3 scripts/gates/check_vocabulary_alignment.py` → **exit 1**，`go_files=790 api_docs=80 violations=390 carrier=390 compression-generation=0 plugin-product=0 retired-word=0`。
- 拆分（--json 实证）：**341 .go＋49 docs/api**＝390，全 carrier。生产 .go 24 文件 97 命中、测试 .go 21 文件 244 命中、docs/api 6 文件 49 命中。
- 存证：`openspec/changes/code-vocabulary-alignment/red-anchor-390.log`（原始输出）。
- 派发前基线：`go build ./...` → **exit 0**（绿，A/B 失败可归因）。

### C 轨（门重写）验证 —— 编排者独立复跑，非信汇报

- 门 `check_vocabulary_alignment.py` **550→266 行**（−284）：纯短语禁，删 load_registry/baseline_entry/write_baseline/verdict/EXEMPT_CLASSES/CLASSIFICATIONS/fingerprint sha256/--registry/--write-baseline/镜像逻辑；保留 scan_files/joined_pieces/piece_line/go_prose_paragraphs/doc_paragraphs/scan_line/scan_prose/scan_tree。四规则：carrier 大小写不敏感子串（*.go＋docs/api）／compression-generation 短语 `generational|fold\s*generation|folding\s*generation`（逐行＋归并散文跨行，单行只计一次）／plugin-product 大小写敏感 Product 仅 api/plugin／retired-word `读透视图|链权威答案`。
- 自测 `test_check_vocabulary_alignment.py` **480→215 行**（−265）：`python3 …` → **exit 0，17 例 52 断言**（tempfile+subprocess 真退出码）。负锚全覆盖：carrier 标识符 commitCarrier／carrier 散文／跨行 folding generation（bands.go:3）／单行 folding generation（excludes compression-generation=2，只计一次）／foldGeneration 驼峰／generationalBand／字符串字面量／api/plugin Product＋MaterialProduct／pluginProduct／读透视图／链权威答案；合法形不误拒：gen:／instance_id／recorder generation／api/event compress.product／api/plugin 小写 product／动词 carries/carried/carrying；缺 docs/api 红。
- 基线 `golden/runtime/vocabulary_alignment.json` **2377→0（删除）**：`ls` 不存在（原为未跟踪工作树产物，HEAD 无词汇块，删除即移除未跟踪文件，无需 git rm）。
- **净负代码**：门＋自测＋基线 3407→481，**净 −2926 行**。
- lint.sh：注释 125-127 改纯短语禁（无基线/豁免）；wiring 128-129（test+check 对，无参）**字节不变**。
- 消费者锚：`.github/workflows/ci.yml:109 run: bash scripts/lint.sh` → scripts/lint.sh → gates/lint.sh policy 块词汇门（test+check）。门红则 CI 红。
- 偏差（C agent 报告，编排者复核合理）：门 266/自测 215 高于 ~150/~120 估算——保留跨行注释归并提取器（go_prose_paragraphs 54 行，症状载荷）＋准确 docstring＋--json 分支所致；仍 ~52-55% 缩减，净负达成，非缩水验收（纯短语禁/全负锚真拒/净负三验收均满足）。删 prosaic（镜像逻辑移除后死代码）。

### D 轨（golden/wiki/GLOSSARY 漂移）验证 —— 编排者直管

- `golden/runtime/architecture.json:1206`「while the carrier rebuilds」→「while the commit rebuilds」；`:1211`「set of the carrier rebuild」→「of the commit rebuild」。
- `golden/runtime/durable_truth.json:204`「the carrier tables they rebuild」→「the commit tables they rebuild」。
- `docs/wiki/agent/task-lifecycle.md:9`「迁移先经 commit carrier 落成事实」→「迁移先经 commit 落成事实」。
- `docs/GLOSSARY.md` 行 3 既有退役词子句内增 carrier 全退役注（按义改既有实名 commit/DurableCommit/credential/URL/projection/envelope/fact/record，不另造新词）；**不增行**（wc -l 68 不变，doc-budget 棘轮不触）；GLOSSARY 在门扫描面 docs/api 之外，退役注为合法提及。
- 残留：`grep -ni carrier architecture.json durable_truth.json task-lifecycle.md` → **0**（exit 1）。
- golden 耦合裁定：check_runtime_architecture.py（结构/边/event-ownership）与 check_durable_truth_registry.py（前缀 name+value）**不逐字比对概念散文**→D 独立于 A 无门耦合（编排者于验证轮复核两门 exit 0）。

### A/B1/B2 轨（carrier 全退役）—— 已完成（编排者复验）

- 触碰集不相交实证：A 全生产 .go（24 文件 97）、B1 测试（durable_test.go 109＋task_checkpoint_test.go 1＋host_tools_test.go 1＋handoff_test.go 2＝113）、B2 测试（runtime/task *_test.go＋tests/integration/** 17 文件 131＋3 行 pre-existing generation）。级联符号均包内私有：newCarrier 仅 durable.go、commitCarrier 仅 commit.go、MemoryRegistry.carrier 字段仅 lifecycle.go（测试 logicalRig.carrier 是测试自有字段，不引用生产私有字段）；NewMemoryRegistry 调用按位置传参，参名改不破调用方→**A/B 独立可编译，无跨轨级联**。
- 三轨并发派发（background general-purpose，嵌入 D6 映射＋白名单＋约束）已完成。编排者复验（非信汇报）：全仓 .go＋_test.go `grep -rni carrier` **0 命中**；私有标识符定义与引用同轨改齐（newDurableCommit/commitLedger/commit 字段/result 局部）；导出 API 字节不变（NewDurableCommit 等）。
- **词汇 .go 净零改名**：`git diff --numstat` 排除 7 Phase2 文件＋外来 main.go 后＝**389/389**（纯改名，逻辑零改）。B2 含 stripFoldGenerations→stripRecorderGenerations（3 行，compression-generation 规则要求，旧已落甲保留）。

### 甲 验证结果（A/B 全落后编排者复跑，原始退出码）

**权威编译/测试**（真树，混合工作树）：
- `go build ./...` → **exit 0**（空输出）；`go vet ./...` → **exit 0**（空输出）。
- `go test ./...` → **exit 0**（TEST_EXIT=0，0 FAIL；/tmp/jia_test.log）。

**F0/E godoc 重生成**（编排者单写）：
- `bash scripts/gates/gen_godoc.sh` → exit 0；`--check` → exit 0（docs/api matches source）。
- 词汇门 docs/api carrier 归 **0**（真树 go_files=790 api_docs=81 violations=0；克隆 go_files=787 api_docs=79 violations=0）。
- 8 docs/api 散文页随注释刷新：api__event／features__rl／memory__ledger／memory__query／memory__replay／runtime__agent／runtime__state／runtime__task。**index.md 词汇零改**（包名未变，基线 78 包；真树 index.md 80 包系外来 migrate-kv＋Phase2 recover-kv 增包，非词汇）。

**F1 契约零改动**（实测）：
- 10 冻结 reincarnation 族文件（verify_contracts.py FILES）＋`api/config` 对基线 `git diff --stat a0ff631b` → **为空**（exit 0），逐字未变。
- `api/event` diff 仅两类：`types.go` **1 注释行**词汇（`fact carrier`→`fact contract`，非签名/标识符）＋`checkpoint.go`/`checkpoint_test.go` 的 **Phase2** `CheckpointKey(seq)`→`CheckpointKey(partition,seq)` 签名修复（独立工作，词汇面分离）。导出 API 标识符零更名。

**F2 全量门禁**：
- **词汇提交树**（干净克隆 `/tmp/jia_f2_clean`＝clone a0ff631b＋仅词汇面 73 跟踪文件＋2 门 .py＋变更目录＋克隆内 godoc 重生成，排除 Phase2/外来；.git 为目录）`bash scripts/gates/lint.sh` → **exit 0**（`lint: ok`，原始码）。comment_policy **0 findings**、词汇门 ok、自测 17 例 52 断言、check-openspec ✓、doc_budget ok、spec_hygiene ok。＝该 commit 的 CI 等价证明。
- **当前混合工作树** lint 红，归属已核 100% 非词汇：①**Phase2 comment_policy 棘轮 9 findings**（free-standing 0→5、test-doc-not-one-sentence 0→4；全在 api/event/checkpoint_test.go:64、assembly/checkpoint_test.go:223/229/261/338/365/409、memory/ledger/migrate_test.go:217/231；4 个被 flag Test 函数 `TestCheckpointOfReadsBackTheKeyItWasFiledUnder`/`TestCheckpointKeySurvivesTheOwnerMovingToASmallerAgent`/`TestCheckpointKeysNameTheOwnerPartition`/`TestMigrateShardedRootOriginalPairing` 对基线 grep **ABSENT_IN_HEAD**＝Phase2 引入，+391/−13 across 3 文件）；②外来未跟踪 `test_check_runtime_symptoms.py` 1 fail（CI 克隆不存在）。**词汇引入零 comment_policy 回归**。Phase2 债系独立 finding，用户「欠债都修」授权后已修（见末节「Phase2 comment_policy 债修复」）；修后真树 comment_policy **0 findings**，混合工作树 lint 余红仅外来未跟踪 `test_check_runtime_symptoms.py`（CI 克隆不存在）。

**F3 DoD 七项**：
- ① **红转绿因果**：甲-start 红锚 **390**（341 .go＋49 docs/api，全 carrier，red-anchor-390.log）→ A/B/D/E 清理后 **0 绿** → **回退全词汇面至基线**门复红 **437**（432 carrier＋5 compression-generation，/tmp/jia_revert_gate.log）＝绿由清理致非门操纵。（390＝甲-start A/B 待落态；437＝全基线回退态，含旧检查点义改名一并复原，故 >390。）
- ② **负例自测真拒**：`test_check_vocabulary_alignment.py` → exit 0，**17 例 52 断言**（tempfile+subprocess 真退出码；4 类植入违规必拒＋合法形不误拒）。
- ③ **全量 lint exit 0**：见 F2 克隆。
- ④ **docs/api diff 仅 godoc**：见 F0；8 散文页注释驱动，index.md 词汇零改。
- ⑤ **冻结归档仅附勘误**：13-bounded-compression/tasks.md＋session-checkpoint-material-unification/tasks.md 各 +1 勘误行，0 删除，原文逐字不变（git diff 2 files 2 insertions）。
- ⑥ **外来零触碰**：7 外来未跟踪路径全在仍未跟踪；3 外来跟踪改动（main.go/README.md/tagent.yaml）本变更未编辑；克隆排除全部外来仍 exit 0。
- ⑦ **净负代码**：门 550→**266**、自测 480→**215**、基线 2377→**0（删除）**；门＋自测＋基线 3407→**481**，净 **−2926 行**。

**R35 独立只读放行审**：见下节（甲轮独立审，对照甲验收面，证据取自词汇提交树）。

### R35 独立只读放行审（甲轮第一轮，general-purpose 子代理，证据取自词汇提交树）

独立审者对照甲验收面 skeptical 复跑（非信汇报），确认 F0-F3 全部成立：克隆 full lint exit 0「lint: ok」comment_policy 0 findings；红锚 390→绿 0→审者独立 `git archive a0ff631b`＋新门回退复红 **437**（432 carrier＋5 generation，逐字吻合）；自测 17 例 52 断言＋审者自植违规真拒（carrier 标识符/散文/跨行 folding generation/docs/api/api·plugin Product/退役词均 exit 1，合法形 gen:/instance_id/recorder generation/carries 动词/小写 product 均 exit 0）；F1 10 冻结文件＋api/config 空 diff、导出声明审计 NON_TEST_EXPORTED_CHANGES=0；词汇 .go numstat **389/389**（54 文件）、265 替换组仅 3 非 carrier 族＝stripFoldGenerations→stripRecorderGenerations；冻结归档 numstat 2 文件各 1/0 零删除；7 外来路径全 ??；门 266＋自测 215＝481、基线 JSON 不存在；无隐藏 baseline/exemption/mirror，无存活伪 user_ruling 泛义保留，spec:38 系语义判断。**边界诚实**：550/480/2377「before」草稿系未跟踪已删，不可独立复测，−2926 为变更内净负（对 HEAD 门基础设施＋481 新行）。

审者发现 **3 非阻塞缺陷**，编排者逐处独立复核＋修复＋复验：
- **缺陷1（staging plan 失实，已修）**：旧 run-notes 把 assembly/checkpoint.go（03.2 checkpointSliceMailbox 删除）列入词汇 commit，但该文件实为 Phase2 排除（同文件混 Phase2 CheckpointKey 签名）。实证：克隆该文件 diff 空、checkpointSliceMailbox 仍在（基线 3→真树 0＝03.2 真树已做但克隆排除）。**03.2 与 Phase2 同文件纠缠**，文件粒度下随 Phase2 提交；词汇 commit 交付 03.2 须 hunk 拆分——A3 裁定。下方 plan 已更正（并修 golden 路径笔误 golden/→golden/runtime/）。
- **缺陷2（.py 泛义 carrier 残留，已修）**：scripts/gates/check_event_integrity_mutations.py:1183/1184/1188 局部 carrier（＝by_id.get(carrying) 的 mutation 对象）未退役，在门扫描面（.go＋docs/api）外故绿，但甲意图 carrier 全退役。裁：carrier→mutation（精确实名，3 处，净零）；复验 `grep carrier`＝0、`py_compile` OK、`test_check_event_integrity_mutations.py` **exit 0（15 tests OK，行为不变）**。纳入词汇 commit，克隆同步＋full lint 复跑。
- **缺陷3（冻结归档勘误行号 off-by-one，已修）**：session-checkpoint-material-unification/tasks.md:35 勘误（本变更所附）引 assembly/checkpoint_test.go:861，基线实际声明 **:860**（真树 :1079 因 Phase2 增行）。更正 861→860（仅改本变更自附勘误，原文 5.2 :859 冻结不动）；实质 claim（真名无 Carrier）本就正确。

**第二轮独立复核**（general-purpose 子代理，read-only）：逐处复验三修均正确完整（缺陷1 clone assembly/checkpoint.go diff 0 字节＋真树 checkpointSliceMailbox 0/基线 3＋tasks.md:49 归属注；缺陷2 carrier 0＋py_compile OK＋自测 15 tests OK＋克隆同步；缺陷3 勘误 :860＝基线声明、原文 5.2 :859 冻结不动、numstat 1/0）。**frontier 扫描**（全 tracked＋untracked＋gitignored carrier 普查）：tracked 38 命中/18 文件全合法（14 冻结归档＋docs/archive/wiki/PRE-REWRITE-MANIFEST.md:128 attribution-carrier 历史插件名＋GLOSSARY:3 退役注＋lint.sh:125 门注释＋2 归档勘误）；**.go/.py/.sh 零泛义 carrier 残留**（含 scripts/recover-kv/）；无存活伪 user_ruling 泛义保留；门无 baseline/exempt/mirror/registry/fingerprint/ratchet，自植 6 违规全 exit 1、合法形 exit 0；克隆 full lint exit 0「lint: ok」＋go build/vet exit 0＋gen_godoc --check exit 0（78 包）；staging 交叉核 73＝65＋8、12 声明项全在、无 Phase2/外来泄漏、.go numstat 389/389。

第二轮发现 **缺陷4（非阻塞，staging plan 失实，同缺陷1 类，已修）**：旧 run-notes 称 api__event.md 为唯一 Phase2 污染 docs/api 页、「其余 7 页真树＝克隆」失实——`memory__ledger.md` 为第二污染页（真树 :744-746 多 Phase2 `ErrSnapshotCorrupt` 孤儿拒绝散文，源 migrate.go Phase2 排除）。编排者逐页 diff 实证：**恰 2 页污染**（api__event 24 行差＋memory__ledger 4 行差）、**6 页纯**（0 行差）；克隆 migrate.go diff 0 字节。修：staging plan 改「2 页取克隆版＋其余 6 页真树＝克隆」（上方已更正）。归档勘误真树＝克隆（diff 0 字节，均 :860，缺陷3 修后已同步克隆），无 stale。

**第三轮独立复核**（general-purpose 子代理，read-only）：缺陷4 修正确（逐页 diff 实证 2 污染/6 纯，克隆 migrate.go diff 0 字节）；克隆 full lint exit 0「lint: ok」comment_policy 0 findings、自测 17/52、.py carrier 0（15 tests OK）、缺陷1 修持（克隆 assembly/checkpoint.go diff 0 字节＋真树 checkpointSliceMailbox 0/基线 3＋golden/runtime 路径）。发现 **2 非阻塞 doc-precision 缺陷**（同缺陷1/4 类，均在编排者自撰 staging plan）：
- **缺陷5（克隆文件数 stale，已修）**：run-notes:221 定义克隆为「仅词汇面 **72** 跟踪文件」，实测克隆 `git diff --name-only HEAD`＝**73**（65 非 docs/api＋8 docs/api），:252 亦称 73。stale 自缺陷2 纳入 check_event_integrity_mutations.py 后未更新计数。修 72→73。
- **缺陷6（克隆 stale 误述＋staging 源未指明，已修）**：run-notes:246 旧称「克隆 doc 微 stale（归档勘误 :861 vs 真树 :860）」**失实**——归档勘误已同步（真树＝克隆 diff 0 字节，均 :860）。克隆真 stale 在**变更目录 3 文件**（tasks.md 缺缺陷1 的 03.2 归属注／01-tasks 01.1/01.2 仍 [ ]／run-notes.md），系缺陷1-4 修前态。修：①更正 :246 误述；②同步真树变更目录→克隆（克隆即词汇 commit 精确树）；③staging plan 指明暂存源权威（下方）。

六缺陷均非 release-blocking，逐处独立复核＋修复＋复验；F0-F3 验收面经三轮独立复跑全部成立。末轮（第四轮）frontier-空确认见下。

### 甲 提交分离计划（精确清单已 finalized；待用户明示 A3 后执行）

**词汇 commit 暂存面**（＝克隆树＝词汇 commit 精确树：73 跟踪文件＋2 新门 .py＋变更目录）：

> **暂存源权威（缺陷6 修后）**：克隆 `/tmp/jia_f2_clean` 变更目录已同步真树修后态，故克隆＝词汇 commit 精确树。逐字核验：65 非 docs/api＋6 纯 docs/api＋2 新门 .py＋变更目录＋2 归档勘误**真树＝克隆**；**唯 2 污染 docs/api 页（api__event／memory__ledger）真树混 Phase2、取克隆纯词汇版**。真树为混合工作树（另含 7 Phase2＋3 外来跟踪＋index.md，均排除）。⇒ 从克隆取全量即得正确词汇 commit，克隆 full lint exit 0 即该 commit 的 CI 等价证明。
- 跟踪改动 **73 文件**＝65 代码/测试/golden/脚本/归档/spec ＋ 8 docs/api 散文页。其中：A 生产 24＋B1/B2 测试 21＋D 轨 golden/runtime/{architecture,durable_truth,window_bounds}.json、docs/wiki/agent/task-lifecycle.md、docs/GLOSSARY.md＋scripts/gates/{lint.sh,test_check_runtime_plan.py,check_event_integrity_mutations.py（缺陷2 carrier→mutation）}＋2 冻结归档 tasks.md＋openspec/specs/session-checkpoint-material/spec.md（Issue7 中文载体 5 处）。
- 新文件：`scripts/gates/check_vocabulary_alignment.py`（266）、`test_check_vocabulary_alignment.py`（215）、`openspec/changes/code-vocabulary-alignment/**`。**基线 JSON 已删**（不入暂存）。
- **docs/api 取克隆重生成版（纯词汇）**：真树 **2 页混 Phase2，不可直接入词汇暂存**（逐页 diff 实证）——①`api__event.md`（Phase2 `CheckpointKey(partition,seq)` 签名＋注释，24 行差）；②`memory__ledger.md`（Phase2 `ErrSnapshotCorrupt` 孤儿拒绝散文 :744-746，源 `memory/ledger/migrate.go` Phase2 排除〔克隆 diff 0 字节〕，4 行差）。词汇 commit 这 2 页须取**克隆版**（纯词汇：api__event＝`fact contract` 散文＋基线 `CheckpointKey(seq)`；memory__ledger＝`ErrMigrateMismatch` 止于基线句，无 orphan 散文）。**其余 6 页真树＝克隆**（features__rl／memory__query／memory__replay／runtime__agent／runtime__state／runtime__task，0 行差，实证）。**index.md 不入词汇暂存**（词汇零改；真树改动系外来+Phase2 增包）。
- `assembly/task_checkpoint_test.go:174` 甲改名→durable commit（**入暂存**，取代旧 Issue3 回退）。

**03.2 纠缠（A3 裁定）**：03.2（checkpointSliceMailbox 重复常量删除＋消费者 checkpoint_test.go:1151 改 StateSliceMailbox）真树已完成（03.2 [x]），但其两文件 assembly/checkpoint.go＋checkpoint_test.go 同载 Phase2 CheckpointKey 签名修复，文件粒度下归 Phase2 排除。故词汇 commit（文件粒度）**不含 03.2**；克隆 F2 证明覆盖 01+02+03.3+D 词汇核（CI-green），03.2 结构去重经真树 go build/vet/test exit 0 独立验证。03.2 hunk 与 Phase2 hunk 不相交可拆——**A3 请用户裁：03.2 随 Phase2 提交，还是 hunk 拆入词汇 commit**。

**不入词汇暂存**：
- **Phase2（7 跟踪文件，独立 commit 或按用户裁）**：api/event/checkpoint.go、api/event/checkpoint_test.go、assembly/checkpoint.go、assembly/checkpoint_test.go、assembly/compress_checkpoint_test.go、memory/ledger/migrate.go、memory/ledger/migrate_test.go（7 文件基线 carrier 均 0＝无词汇重叠，实证）＋scripts/recover-kv/（未跟踪恢复工具）＋docs/api/…recover-kv.md（未跟踪派生）。
- **外来（零触碰）**：examples/wechat-bot/{main.go,README.md,tagent.yaml,skills/interview/agent-interview-helper/}、scripts/migrate-kv/、docs/api/…migrate-kv.md、docs/api/index.md（migrate-kv/recover-kv 行）、scripts/gates/{check_event_semantics_symptoms.py,runtime_budgets.py,test_check_runtime_symptoms.py}、openspec/changes/runtime-architecture-convergence/。

**不提交/不推送/不归档**（A3 发布授权法定停点，待用户明示；docs 推送授权不延伸至词汇）。

### R35 第四轮（末轮）独立 frontier-空确认

**审者**：general-purpose 只读子代理，skeptical 自跑证据（非信汇报），扫描范围＝真树＋克隆＋新构基线 `/tmp/r4_baseline`（`git archive a0ff631b`），文件类型 *.go/*.py/*.sh/docs/api/*.md/golden/runtime/*.json/openspec/**；两树零写入。

**VERDICT：FRONTIER EMPTY**（release-blocking 0／non-blocking 0）。14 项逐条 PASS（原始退出码/计数）：
1. 克隆 full lint **exit 0**「lint: ok」；comment_policy **0 findings**（棘轮 invocation＋product-package zero-findings invocation 双绿）；词汇门 `go_files=787 api_docs=79 violations=0 carrier=0`；自测 **17 例 52 断言**；gen_godoc 78 包 matches；go build/vet exit 0；1682 行零 FAIL/ERROR/Traceback。
2. 缺陷5：克隆 `git diff --name-only HEAD`＝**73**（65 非 docs/api＋8 docs/api）；run-notes:221 称 73，无存活 72。
3. 缺陷6a：归档勘误真树＝克隆 diff **exit 0**，均引 :860（克隆 assembly/checkpoint_test.go:860＝TestTheCheckpointStaysBoundedByItsEntries，真树 :1079 因 Phase2 增行）；:246 已标旧 :861 claim 失实。
4. 缺陷6b：变更目录 `diff -r` 真树 vs 克隆 **exit 0**，18 文件全等。
5. **暂存不变量**：75 词汇文件（73 跟踪＋2 门 .py）逐文件 diff，**恰 2 页异**（api__event／memory__ledger），余全等。
6. 污染页克隆纯净：api__event:319＝`CheckpointKey(seq int64)`＋:67 `fact contract`（真树 :319＝`CheckpointKey(partition int, seq int64)`）；memory__ledger:744 止于 `ErrMigrateMismatch`（真树多 3 行 ErrSnapshotCorrupt orphan :744-746）；内容行差 24／4 吻合。
7. 排除：7 Phase2＋3 外来＋index.md 均不在克隆 diff（grep exit 1）；真树该 11 文件 diff carrier 行 **0**；index.md 仅 2 增包行＋78→80。
8. carrier 残留：.go/.py/.sh **18 命中两树同**，仅 3 文件（门 rule/needle/docstring 10＋自测 fixtures 7＋lint.sh:125-127 注释 1），**.go 零命中**；全 tracked 普查 38 命中/18 文件全合法（15 冻结归档＋PRE-REWRITE-MANIFEST attribution-carrier＋GLOSSARY 退役注＋lint.sh）。
9. 门纯粹：266 行 4 规则，`baseline|exempt|mirror|ratchet|fingerprint|registry|classif|allowlist` 仅命中 docstring 免责句；基线 JSON 三树皆无（never tracked）；门 266＋自测 215＝**481**；自测真 subprocess 退出码，手计数 52 断言。
10. 无伪权威：门/自测/lint.sh 无 user_ruling/泛义保留；存活 user_ruling 仅 guard_checklib.py 通用字段＋fixture 串；变更目录命中均撤回 banner 或 ORCHESTRATION:77 冻结台账；spec:38「端口名 SHALL NOT 是语义的唯一载体」系泛义语义判断无裁定归属，diff 5 处载体→checkpoint。
11. 冻结归档：两文件 `git diff --numstat a0ff631b`＝**1 0**（两树），各 +1 勘误行零删除原文不动。
12. F1：10 冻结 reincarnation 文件＋api/config `git diff --stat` **空**（两树）；types.go 仅 1 注释行；54 .go diff 导出声明扫描＝struct 字段/interface 方法/常量改动 **0**（仅测试名＋NewMemoryRegistry 参名 carrier→commit，型/名不变）。
13. 红转绿：新构基线＋仅 2 新门 → **exit 1 violations=437 carrier=432 generation=5**；克隆 → 0 exit 0；自测基线亦 17/52。因果独立复现。
14. staging plan：73＝65＋8✓、2 克隆页/6 逐字✓、排除清单✓、.go numstat **389/389 across 54**✓、checkpointSliceMailbox 基线 3/克隆 3/真树 0✓、task_checkpoint_test.go:174＝durable commit✓、真树门 go_files=790 api_docs=81 violations=0✓、A24＋B1 4＋B2 17＝21 逐文件核✓。

**2 候选缺陷经证据驳回**：①run-notes:63 引 :861 在 line-5 banner 冻结历史块内（只附勘误不改写，更正在 :242），改写违 append-only＝非缺陷；②run-notes:259「其中」枚举 56/65（9 保留 pre-甲 .go 未列），每个数字均真且权威指令＝「从克隆取全量」，部分枚举非失实 claim＝非缺陷。

**结论**：克隆＝词汇 commit 精确树，其 lint.sh exit 0 为有效 CI 等价证明。R35 放行门满足（独立只读审者末轮显式 frontier 空＋扫描范围＋逐项证据）。**A3 发布授权法定停点**：不提交/不推送/不归档，待用户明示。

### Phase2 comment_policy 债修复（用户「欠债都修」授权）

**授权与边界**：用户「hunk是什么意思？欠债都修」明示修齐 Phase2 遗留的 comment_policy 债。授权范围＝**仅注释形状**；Phase2 运行时逻辑仍零触碰（Error1/Error2 修复不动）。

**9 findings 两类**（修前 ratchet exit 1，/tmp/r5_cp_ratchet.log）：
- free-standing 5：正文注释挂在 `:=`／`for` 前，二者无 Doc 字段 → 非 doc slot，门判游离。
- test-doc-not-one-sentence 4：Test 函数 doc 意图句跨 2+ 物理行，续行非 `- ` bullet → 门判多句。

**合规修法**（对齐 assembly/checkpoint_test.go TestBootRefusesAnUnreadableCheckpoint 范式：一意图句＋`- ` bullets＋index 行，零游离正文注释）：
- 4 意图句合并/重塑为单行，原 prose 降为 `- ` bullets 保全：api/event/checkpoint_test.go:58 TestCheckpointOfReadsBackTheKeyItWasFiledUnder；memory/ledger/migrate_test.go:211 TestMigrateShardedRootOriginalPairing；assembly/checkpoint_test.go:215 TestCheckpointKeySurvivesTheOwnerMovingToASmallerAgent、:324 TestCheckpointKeysNameTheOwnerPartition。
- 5 游离正文注释删除（assembly 4 处＋memory/ledger 1 处）；其信息由 doc bullets＋断言文案＋wiki 承载（platform-subsystems.md#framework-checkpoint 已述分区 Seq 单调／landedSeq／commitFactNoCadence 落链机理），删除无损。

**验证**（修后，本段复跑确认）：
- comment_policy ratchet **9→0**，exit 0「0 finding(s); 0 beyond baseline」（/tmp/r5_cp_confirm.log）；product 面 -no-baseline 0 findings。
- go build ./... exit 0；go vet（api/event・assembly・memory/ledger）exit 0；go test 三包 exit 0（0.457s／253.617s／22.398s，/tmp/r5_test.log）；gofmt -l 空。
- 词汇门真树 go_files=790 api_docs=81 violations=0（/tmp/r5_vocab_confirm.log）。
- **仅注释、零逻辑改动**：3 文件均 Phase2-excluded，克隆 vocab face `git diff --stat` 该 3 文件**空**、克隆 73 文件 diff 不变 → 词汇 commit 纯度与克隆 CI 等价不受影响。

**真树 full lint 余红**：exit 1 唯外来未跟踪 `scripts/gates/test_check_runtime_symptoms.py`（test_registry_matches_the_module：registry 列 TestLocalSmokeSymptomBootDeployed 而模块异）；零触碰、CI 克隆不含此文件 → 非本变更债。
