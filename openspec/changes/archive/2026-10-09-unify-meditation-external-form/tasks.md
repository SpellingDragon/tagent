# Tasks: unify-meditation-external-form

## 1. 引擎单机制化（agent/ + config/ + reliability + inject，单代理）

- [x] 1.1 判据统一与锚删除：meditation.go 移除 lastUserInput/UpdateLastUserInput/双分支 checkAndMeditate/回切注释；inject.go 移除挂臂；MeditationAnchors 删 LastUserInput 字段（旧文件未知键忽略加回归）；观察面缺省=[自身分区]（resolveObserved 缺省路径改自身，不回落 read_namespaces）—— 验证：`go test ./agent ./memory -run '^TestMeditation|^TestCapacityHint|^TestAnchor' -count=1 -race`。

  > 实测（2026-10-09，dev）：`go test ./agent ./memory -run '^TestMeditation|^TestCapacityHint|^TestAnchor' -count=1 -race`
  > → `ok github.com/SpellingDragon/tagent/agent 1.705s` / `ok github.com/SpellingDragon/tagent/memory 1.270s`
  > 基线不改动即绿：`go test ./tests -run '^TestExternalMeditation' -count=1` → ok（3 个 Test 全 PASS）；
  > `go test . -run '^TestDeliverToAgent' -count=1` → ok。
- [x] 1.2 观察面校验调整：显式 observed ⊆ read_namespaces ∪ {自身}（自身恒合法）；混合观察面（自身+他人）合法；越界仍具名拒启；config 层注释同步 —— 验证：`go test ./config -count=1`、`go test . -run '^TestMeditationAssembly' -count=1`。

  > 实测：`go test ./config -count=1` → ok；`go test . -run '^TestMeditationAssembly' -count=1` → ok
  > （缺省自察/无读授权仍自察/自身+他人混合合法/越界具名拒启/显式声明与 deliver_to 搬运 五例全绿）。
- [x] 1.3 测试收敛：双判据组合并为单判据参数化（{自身}/{他人}/{自身+他人}）；删回切组；新增"缺省观察面=自身"in-loop 等效回归（不配 observed、用户事件入库、空闲后触发、反思进本 session）；注入侧符号灭绝编译期断言 —— 验证：`go test ./agent -count=1 -race`、`grep -rn "lastUserInput\|UpdateLastUserInput" agent/ --include='*.go' | grep -v _test | wc -l` =0。

  > 实测：`go test ./agent -count=1 -race` → `ok github.com/SpellingDragon/tagent/agent 83.605s`；
  > `grep -rn "lastUserInput\|UpdateLastUserInput" --include='*.go' agent/ \| grep -v _test \| wc -l` → 0。
  > 收尾门禁：`GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh` → lint: ok（comment_policy 0 findings，docs/api 已随导出面变更重新生成）；
  > `go test ./... -short -count=1` → exit 0（36 个包 ok，无 FAIL）。

## 2. session 安排与示例迁移（依赖 1.2；与 3 并行）

- [x] 2.1 wechat-bot：入口 meditation 块保留缺省（自察等效，配置零改动验证）；新增启用 curator agent（observed=业务 agent 分区集、deliver_to=[entry]、file/exec/refine 工具面、memory.type=memory 共享、StartLoop(loopUser, "curation")）；tagent.rl.yaml 同步；main.go 启动第二循环（curator 的 outputCh 消费策略：日志/丢弃卡片原文防泄漏）—— 验证：`cd examples/wechat-bot && go test ./... -short -count=1`；`grep -n "curation" main.go` 命中启动行。

  > 实测（2026-10-09，dev）：`cd examples/wechat-bot && go build ./... && go vet ./... && go test ./... -short -count=1`
  > → `BUILD OK` / `VET OK` / `ok  wechat-bot 0.863s`；根包不受影响：`go test . -short -count=1` → `ok  github.com/SpellingDragon/tagent 58.413s`；
  > 注释门禁 `go run ./scripts/comment_policy . examples/wechat-bot` → `0 finding(s); 0 beyond baseline`。
  > 严格解析（KnownFields=true）两份配置均 OK：入口 `meditation enabled observed=[] deliver_to=[] prompt_file="meditation.md"`（缺省=自察，配置键零改动即原 in-loop 等效），
  > curator `observed=[tagent] deliver_to=[tagent] prompt_file="curator.md" read_ns=[tagent] type=localfile path=.wechat-config/data`。
  > 装配与双循环（stub-model 探针，配置原文不改字段只换 store 目录）：`resident table: knowledge,plan,curator,tagent` →
  > `[StartLoop] ... session=curation` → `loops up: entryActive=true curatorActive=true curatorIsResident=true` → `PROBE OK`；tagent.rl.yaml 同形态亦 PROBE OK。
  > `grep -n "curation" main.go` → 命中 `const curationSession = "curation"`（D4 保留名落档注释）与 `curator.StartLoop(loopUser, curationSession)` 启动行，另有收尾 `curationStop()`。
  > 落地口径与任务文本的差（详见执行战报）：curator 的 memory 取 `type: localfile` 与入口同 path 同指纹（`type: memory` 会把 store kind 换成 mem，两个实例仍对同一目录取 `.tagent-writer.lock` → ErrStoreLocked；另起目录则观察不到业务分区）；
  > refine 面只装配给 entry（build_agent.go 的 evoGit 判定），故 curator 工具面为 file/exec/recall，产物写盘即真源，登记归入口自察线或人。
  > 顺带修：`tagent.rl.yaml` 在 HEAD 即通不过严格解析（15 处 per-tool `max_tool_iterations/max_tokens/temperature`，ToolRef 无这些字段，值与各 agent 自身定义逐字重复 → 删除语义等价）。
  > 既有缺陷（未在本任务白名单内，留给后续）：`tagent.rl.yaml` 引用的 `resources/prompts/speak_tool_desc.md`、`draw_tool_desc.md` 在树内不存在，装配在 HEAD 就止步于此（已对 HEAD 版本 A/B 复现）；RL 形态的 curator 是用探针改写这两个引用后验通的。
- [x] 2.2 e2e/真实模型回归：既有 TestExternalMeditation*/TestRealModel_ExternalMeditation 不改动即通过；锚点 fixture 若含三键更新 —— 验证：`go test ./tests -run '^TestExternalMeditation' -count=1`。

## 3. 文档与规格（与 2 并行；白名单 docs/README，禁 .go）

- [x] 3.1 wiki 单机制改写：§2.14 重写（锚 `#meditation-two-forms`→`#meditation-curator`，叙述"观察面决定自察或策展"）、全库 `契约:` 行同步、行为矩阵/平台子系统对齐；双形态断言清零：`grep -rn "双形态\|in-loop 形态\|in-loop 冥想" docs/wiki/ README.md | wc -l` =0 —— 验证：`GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh`。

  > 实测（2026-10-09，dev）：§2.14 重写为单机制节——`<a id="meditation-two-forms">`→`<a id="meditation-curator">`，
  > 节题改为「2.14 反思与策展：一个机制，两种观察面」；节内叙述覆盖：触发＝向本 agent 循环 session 注入冥想事件、
  > 缺省观察面＝自身（自察，零配置零迁移）、显式列他人＝策展、混合可配、授权边界（⊆ read ∪ {自身}、缺省不回落 read）、
  > session 约定（业务线宿主路由 / 策展线保留名 `curation` 固定单线）、两锚结构与旧三锚多余键忽略、防永动与 fail-closed 原样保留、digest 单一覆盖面。
  > 锚同步：wiki 侧 1 处锚定义 + 10 处引用（9 文件，`grep -rno "meditation-curator" docs/wiki/ | wc -l` → 11）+ `.go` 侧 1 处 `契约:` 行；
  > 旧锚灭绝：`grep -rn "meditation-two-forms" docs/ README.md README_EN.md | wc -l` → 0、`grep -rn "meditation-two-forms" --include='*.go' . | wc -l` → 0。
  > 双形态断言清零：`grep -rn "双形态\|in-loop" docs/wiki/ README.md | grep -vi "refine\|govx" | wc -l` → **0**；
  > 任务原文口径 `grep -rn "双形态\|in-loop 形态\|in-loop 冥想" docs/wiki/ README.md | wc -l` → **0**。
  > 移交项：`delivery_test.go` 2 行 in-loop 叙述注释改单机制措辞（`git diff --numstat delivery_test.go` → `2 2`，**纯注释、测试逻辑零改动**；
  > 移作战报说 3 处，实测 in-loop 叙述仅 2 处，第 3 处即 `.go` 侧 `契约:` 锚行 `tests/real_model_external_meditation_test.go` → `1 1`，一并改）；
  > `go test . -run '^TestDeliverToAgent' -count=1` → `ok github.com/SpellingDragon/tagent 1.079s`；
  > `docs/wiki/examples/wechat-bot-runtime.md` §投递辨析处改写为「入口缺省自察 + curator 策展线（保留 session `curation`）」。
  > 收尾门禁：`GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh` → `lint: ok`（comment_policy `0 finding(s); 0 beyond baseline`——`#meditation-curator`
  > 经 indexAnchor 门可解、`doc-refs: no dangling file citations`、`gen_godoc: docs/api matches the source (40 packages)`）。
- [x] 3.2 README 双语：特性表/核心特性第 6 节/配置行（observed 缺省=自身）/现状边界段改单机制；session 安排一句话（反思线=本 agent session，策展线保留名 curation）—— 验证：lint + 中英镜像 grep。

  > 实测（2026-10-09，dev）：中英成对改写六处——核心特性节题+正文（缺省配置即自察、显式观察面＝策展、混合可配、session 落点一句话）、
  > 深入阅读表行（`冥想两形态`→`冥想（单机制：缺省自察，显式列他人即策展）`）、配置参考 agent 级 `meditation` 行
  > （默认列补 **`observed_namespaces` 缺省＝`[自身分区]`**，说明列改「不写即自察／显式列他人＝同一机制的跨域策展」）、
  > 现状与边界段收敛（跨域不是第二个开关，是观察面配置）、设计承诺 `外部策展`→`跨域策展`、
  > wechat-bot 示例行提 curator 策展线并校正 agent 计数（`tagent.yaml` 实有 6 个：入口 + knowledge/recall/action/plan + curator，
  > 反思固定跑在保留 session `curation`）。措辞对齐 meditation-idle-gating delta（缺省＝[自身分区]、他人须 ⊆ read_namespaces、注入本 agent 循环 session）。
  > 不动项：`## 🔧 配置参考` 标题与 `README.md#-配置参考` 引用（`docs/config-migration.md:4` 仍可解，lint doc-refs 0）；readme-enhancement 要求的架构/数据流/快速启动节逐字保留。
  > 中英镜像核对：`wc -l` → 240/240；`grep -c '^## '` → 12/12；`grep -c '^### '` → 12/12；`grep -c '^| '` → 43/43；`grep -c observed_namespaces` → 3/3；
  > 残留：`grep -n "双形态\|in-loop\|两形态\|两种形态\|外部冥想\|two meditation forms" README.md README_EN.md | wc -l` → 0。
  > 门禁：`GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh` → `lint: ok`。

## 4. 收尾 F

- [x] F1 非触碰面断言：`git diff fab5c94..HEAD -- openspec/specs/cross-session-delivery openspec/specs/event-sourced-projection agent/compress memory/segment_store.go delivery.go` 为空。

  > 复跑（2026-10-09，dev）：`git diff --name-only fab5c94..HEAD -- openspec/specs/cross-session-delivery openspec/specs/event-sourced-projection agent/compress memory/segment_store.go delivery.go | wc -l` → **0**。
  > 补一条工作区口径（本变更全程未提交，`..HEAD` 只覆盖已提交面）：`git diff --name-only fab5c94 -- <同五面> | wc -l` → **0**。
- [x] F2 全量门禁：build/vet/`./... -short`/GOMAXPROCS=1/bot 模块/改动域 `-race`/lint(Go1.24)/check-openspec 全 0。

  > 复跑（2026-10-09，dev，逐条 rc=0；数字取自当场输出，命令可复跑）：
  > `go build ./...`+`go vet ./...` → 0；`go test ./... -short -count=1` → 0（**36 个包 ok、FAIL 计数 0**）；
  > `GOMAXPROCS=1 go test . -short -count=1` → `ok github.com/SpellingDragon/tagent 59.449s`；
  > bot 模块 `cd examples/wechat-bot && go build ./... && go vet ./... && go test ./... -short -count=1` → `ok wechat-bot 0.950s`；
  > 改动域 race：`./scripts/race_check.sh ./agent ./config ./memory .` → `race_check: OK (go test passed)`（agent 82.862s / config 1.425s / memory 3.390s / 根包 74.607s）、
  > `go test -race -count=1 -short ./tests` → `ok github.com/SpellingDragon/tagent/tests 8.334s`；
  > `GOTOOLCHAIN=go1.24.1 bash scripts/lint.sh` → `lint: ok`（comment_policy `0 finding(s); 0 beyond baseline`、doc-refs 无悬挂、godoc 新鲜 40 包）；
  > `bash scripts/check-openspec.sh` → `Totals: 115 passed, 0 failed (115 items)` + `OK: all main specs pass validate --strict`。
  > 过程申报：首轮 race 误用了 CI 未含的 `./tests` 且漏 `-short`，包内 10m 测试时限未到期被我终止（rc=143，非回归）；改按 CI 口径重跑即全 0。
- [x] F3 DoD：四 delta 全 Scenario→实存测试对账；真实模型用例确认 PASS 于本地最近 run 记录（不重跑、省配额）。

  > 对账（四 delta 全 Scenario → 实存测试，2026-10-09）：
  > **meditation-agent-partition**：缺省自察→`agent.TestMeditationDefault_ObservesOwnPartition`（观察面长度 1 且 id=`PartitionIDFromName(自身)`，用户入库后同 session 出反思回合）+ 根包 `TestMeditationAssemblyObservationSurface`；混合观察面→`agent.TestMeditationGate_ObservedSurfaceMatrix`（{自身}/{他人}/{自身+他人} 参数化）；越界授权拒绝→`TestMeditationAssemblyObservationSurface`（具名拒启）；锚点跨重启→`agent.TestMeditationManager_AnchorStoreRestore` + `agent/reliability.TestAnchorStore_LegacyFileIgnoresUnknownKey`（旧 `{"last_user_input":…}` 键忽略）；触发后自锁→`TestMeditationManager_WatermarkAdvancesOnFire`；跨分区用户事件触发→`TestMeditationManager_CrossPartitionNoveltyGate`；防永动→`TestMeditationManager_SelfManagedOutputIsNotNovelty` + `tests.TestExternalMeditation_NoPerpetualMotion`；读失败不猜测→`TestMeditationManager_CrossPartitionNoveltyGate` 的 `read failure keeps the gate closed`／`observation surface without a reader keeps the gate closed` 子测试 + `TestMeditationManager_EmptySurfaceIssuesNoRead`；注入目标恒为本循环→`TestMeditationGate_InjectsUnderMeditationSource`；**策展线固定 session→无入库断言**（`examples/wechat-bot/main.go` 的 `curationSession` 常量 + 本地探针；该模块 `main_test.go` 处于 git-ignored，W2 已申报，语义已落 §2.14 与 spec）。
  > **meditation-idle-gating**：三门齐备→`TestMeditationGate_ObservedSurfaceMatrix`；缺省等效→`TestMeditationDefault_ObservesOwnPartition`；投递风暴只推迟→`TestDeliverToAgent_NoveltyNotRearmedByDelivery` + `tests.TestExternalMeditation_YieldToUser`；回调与锚点解耦→`TestOnEventCallback_DoesNotTouchMeditationAnchors`；唯一读径→`TestMeditationManager_ReaderWiredAtConstruction` + `TestMeditationManager_UnknownLineageNotCounted` + 注入侧符号灭绝（`grep -rn "lastUserInput\|UpdateLastUserInput" agent/ --include='*.go' \| grep -v _test \| wc -l` → 0）。
  > **meditation-self-state-digest**：概况为主→`TestMeditation_DigestObservedScan_PerPartitionLineageCounts`/`_RecentActivityAndUnknownLineage`；自察近况→观察面扫描渲染族 `TestMeditation_DigestObservedScan_*`（单分区＝自身时同一段落）+ 缺省面回归 `TestMeditationDefault_ObservesOwnPartition`；**无独立命名用例钉自察 digest 文本**（薄弱点申报）；自身无任务不报错→`TestRenderSelfStateDigest_EmptyDegrades`/`TestMeditation_NoDigestWhenNoController`/`TestMeditation_ExternalDigestWithoutOwnTaskLayer`；单次扫描复用→`TestMeditation_DigestFromLiveExternalScan`（digest 由判据那次扫描渲染）。
  > **self-improvement-meditation**：登记闭环→`evolution.TestGitRegister_*`（登记义务与反思主体无关）；申报缺口：**无"策展主体未登记提醒"专项测试**（refine 工具按装配只挂入口，W2 已申报：产物写盘即真源，登记归入口自察线或人）；叙事与单机制一致→本波文档断言可复跑：`grep -rn "双形态\|in-loop" docs/wiki/ README.md | grep -vi "refine\|govx" | wc -l` → 0、`grep -rn "meditation-two-forms" docs/ --include='*.md' | wc -l` → 0、lint 的 indexAnchor 门放行 `#meditation-curator`。
  > 真实模型：按口径不重跑。本地最近 run 记录 `/tmp/w4b-real.json` → `"Action":"pass","Package":"github.com/SpellingDragon/tagent/tests","Test":"TestRealModel_ExternalMeditation"`（2026-10-08T23:51+08:00，包级 `ok … 55.092s`）；
  > **申报时限差**：该 PASS 早于 W1 引擎改动（W1 未提交，2026-10-09 落档）。可采信依据：外部形态判据即当前唯一判据、`tests/` 用例逻辑零改动（`git diff --numstat tests/real_model_external_meditation_test.go` → `1 1`，且该行是 `契约:` 注释）；若需闭环，重跑一次 `TestRealModel_ExternalMeditation` 约 3 次调用预算。
- [x] F4 归档：strict → check-openspec → `openspec archive --yes`（MODIFIED/REMOVED 标题与主 spec 逐字一致）。

  > **交接（本波未执行，白名单外）**：前置两项已由本波复跑背书——`openspec validate unify-meditation-external-form --strict` → `Change 'unify-meditation-external-form' is valid`；`bash scripts/check-openspec.sh` → 115 passed / 0 failed。
  > `openspec archive --yes` 会改写 `openspec/specs/**` 并把变更目录移入 `openspec/changes/archive/`，超出本波白名单（docs/wiki/**、README.md、README_EN.md、delivery_test.go 注释、tasks.md），故留给编排者核销后执行；MODIFIED/REMOVED 标题与主 spec 的逐字一致性需在归档时由 CLI 比对确认。

> 编排者核销（2026-10-09 四查复跑）：`go test ./tests -run '^TestExternalMeditation' -count=1` → ok（**测试文件零改动**=单机制行为基线等效性的最强证据）；anchor fixture 经 W1 判定无需更新；TestRealModel_* 按 F3 口径不重跑（本地 PASS 记录在案 /tmp/w4b-real.json）。

> 编排者终核（2026-10-09）：真实模型重跑取新证据（旧 PASS 早于 W1 引擎改动，时间差由 W3 申报）——`TAGENT_REQUIRE_REAL_MODEL=1 go test ./tests -run '^TestRealModel_ExternalMeditation$' -count=1 -json` → **pass 45.95s**，预算 ledger `calls=3`，无门复跑合法 SKIP（/tmp/w5-real.json、样本 /tmp/w5-real-run）。F4 归档：strict ✓ → `openspec archive --yes` → 五能力 sync（+1 ADDED「反思事件注入本 agent 循环 session」/~7 MODIFIED/-2 REMOVED），check-openspec **115/0**，lint(Go1.24)=0，活跃变更清零。delivery_test.go 描述字面量经裁决同步「跨域策展冥想」，TestDeliverToAgent ok。
