# Run notes: config-surface-normalization

> W3 收尾凭据。复跑命令为唯一判定，非汇报。基线 = f1c51e87（dev HEAD，本变更未提交，92 文件工作树）。
> 证据分级：✅ 实测（复跑 exit 0/1 + 原文）· ⚠️ 提及未确认 · ❓ 推断。

## F1 契约变更面一致断言（非零 diff，恰为 design 所列）

本变更**有意**改契约。`git diff HEAD -- openspec/specs golden docs/GLOSSARY.md` = 5 文件，22 insertions / 28 deletions，逐处对照 design §四/§六/§七：

| 契约文件 | 改动 | design 落点 |
|---|---|---|
| `docs/GLOSSARY.md` | §一「请求预算」行：回放窗 replay_window／预算面 context_window 唯一门源＋max_tokens 输出限／余量派生自有效输出限 | §六 D-03 3.1、§七文档 |
| `golden/runtime/schema_allowlist.json` | generic_keys 删 provider_context_window/output_reserve、增 context_window/replay_window（max_tokens 保留为输出限）＋note 归位 | §七门+注册表（R11 实施期补全缺口） |
| `golden/runtime/window_bounds.json` | classes window/budget 教义乙改写；replay-window name→replay_window；provider-window name→context_window＋source「no default, gate opens only when context_window>0」；output-reserve→output-limit（name max_tokens）；删 output-reserve-divisor 常量条目 | §四 D5、§六 D-01 1.4 |
| `openspec/specs/config-hot-reload/spec.md` | 复核注记附乙勘误（历史正文保留）；requirement body＋4 scenario max_tokens→replay_window；脆弱坐标 tagent.yaml:123→语义指针 | §七文档 |
| `openspec/specs/orchestration/spec.md` | :483 追加乙裁定勘误（boundary-closure 旧「MaxTokens 为正触发门」被取代），:481 历史 body 字节不变 | §七文档（R11 实施期补全缺口） |

- **request-budget-accounting 主 spec**：MODIFIED 在 01 delta（`changes/01-budget-normalization/specs/request-budget-accounting/spec.md`，含「无输出限不预留 c75 症状直译」「输出限真发 provider」「只声明 replay_window 门闭 乙」三 scenario），design §六 D-03 3.2 所列；按 openspec 工作流于 **F4 archive 同步**至主 spec，故 F1 时点主 spec 尚未变（非漂移）。
- **冻结归档零 diff** ✅：`git diff HEAD --stat -- 'openspec/changes/archive/**'` = 空（0 字节）。全 diff 中唯一 archive 路径提及是 GLOSSARY「独立验收」行的**未改动上下文**（引用归档路径，非修改）。
- **§五冻结拼写零 diff** ✅：全 diff 的 +/- 行 grep `reincarnation|REINCARNATION_NOTICE|转世通报|TestScenario05CloseHandoffReincarnation|TestReincarnationNoticePreserved` → **exit 1（零匹配）**。reincarnation 族六拼写无一改动。

## F2 全量门禁（各 exit 0）

| 门 | 命令 | 结果 |
|---|---|---|
| build | `go build ./...` | ✅ BUILD_EXIT=0 |
| smoke build | `go build -tags smoke ./...` | ✅ SMOKE_BUILD_EXIT=0 |
| 核心测试 | `go test ./api/config/... ./assembly/... ./runtime/agent/... ./memory/replay/... -count=1` | ✅ CORE_TEST_EXIT=0（api/config 0.49s、assembly 256.9s、runtime/agent 4.1s、memory/replay 0.79s 全 ok） |
| 集成测试 | `go test ./tests/integration/... -count=1` | ✅ INTEGRATION_EXIT=0（acceptance/capabilities/config/evolution/execution 44.9s/history/hotupdate/inspection/journey/learning/state 全 ok） |
| 远端模块 build/vet | `go -C examples/wechat-bot build ./...` / `vet ./...` | ✅ WECHAT_BUILD_EXIT=0 / WECHAT_VET_EXIT=0 |
| 远端模块短测 | `go -C examples/wechat-bot test -short ./...` | ✅ WECHAT_SHORT_EXIT=0（wechat-bot 3.36s ok） |
| 窗口注册表 | `python scripts/gates/check_window_bounds.py` | ✅ exit 0（4 classes、6 declared、19 constants、68 keys，windows==1） |
| openspec | `bash scripts/gates/check-openspec.sh` | ✅ exit 0（156 passed；2 failed 是外来在途 WIP `event-driven-consumption`/`post-training-data-loop`，not gated，非本变更；all main specs pass validate --strict） |
| godoc 新鲜度 | `bash scripts/gates/gen_godoc.sh --check` | ✅ exit 0（docs/api matches source，80 packages） |
| doc_links | `check_doc_links.py` | ✅ exit 0（42 docs、130 links、56 anchors、0 errors） |
| vocabulary | `check_vocabulary_alignment.py` | ✅ exit 0（790 go_files、81 api_docs、0 violations） |
| feature_boundary | `check_feature_boundary.py` | ✅ exit 0（r3 56 go_files、0 errors） |
| concept_budget | `check_concept_budget.py` | ✅ exit 0（AgentSpec 棘轮 24） |
| schema_allowlist | `check_schema_allowlist.py` | ✅ exit 0（host_documents=2、generic_keys=20、exceptions=1、0 errors） |
| spec_hygiene | `check_spec_hygiene.py` | ✅ exit 0（violations=284、baseline=286、stale=2、0 errors） |
| doc_budget | `check_doc_budget.py` | ✅ exit 0（21998=21998，具名上调 +6 见下） |

- **doc_budget 具名上调（+6 行）**：baseline.lines 21992→21998（files 231 不变），按 M4 先例「权威文档/合法变更文档增长具名上调 manifest，不靠删散文对冲」。明细：CHANGELOG.md +4（[Unreleased] 四要素条目）、orchestration/spec.md +2（契约守护乙勘误）。其余 W2 文档净 0，godoc docs/api 在排除区。`--tighten` 机械拒升，故直编 manifest 并注记。
- **spec_hygiene stale=2**：config-hot-reload:10 脆弱坐标改语义指针后旧基线条目陈旧（reported，非 error），留未来 `--write-baseline` 清理（reviewed tightening）。

## F3 DoD 逐项核验 + 症状锚红转绿

### ① S1（c75 隔离复现）完整回滚必红 / 修复必绿 —— 两次证据

**回滚红**（干净基线 worktree，完整回滚本变更）✅：
- 构造：`git worktree add --detach /tmp/tagent-baseline f1c51e87`（隔离检出，不动工作树）；于其中写 `assembly/c75_rollback_test.go`，把 S1 锚 scenario 移植到基线 API（`MaxTokens`=回放窗、`ProviderContextWindow`=预算窗，无 OutputReserve），prompt 385600 bytes 校准入 c75 带。
- 复跑：`go -C /tmp/tagent-baseline test ./assembly -run TestSymptomAnchorPhantomReserveRefusesFittingRequest -count=1 -v` → **exit 1（FAIL）**。
- 红因原文（行为红，非编译错）：
  > `agent: the request exceeds the declared input budget: invocation session-c75-symptom#c1 agent agent-budget estimated 96413 input tokens over the declared input budget 96000 (provider window 128000, output reserve 32000) with no degradable material left`
- 机理：基线 `budgetReserve`→`agent.OutputReserveOf(window)`=`ceil(128000/4)`=**32000**（凭空 window/4，`DefaultOutputReserveDivisor=4`）→ budget=128000−32000=**96000** → 96413>96000 **拒发**。直译用户原话 c75（「这里没有配置为什么有这样的控制」＋96407 拒发；96413 vs 96407 为隔离复现 prompt marker 差异，同带同机理，design §五「不依赖 c75 完整轨迹」）。

**修复绿**（当前工作树）✅：
- 复跑：`go test ./assembly -run 'TestSymptomAnchorPhantomReserveRefusesFittingRequest|...' -count=1 -v` → **exit 0**，`--- PASS: TestSymptomAnchorPhantomReserveRefusesFittingRequest`。
- 机理（读码实证 request.go:852/862）：`budgetWindow()=spec.ContextWindow`=**128000**（唯一门源，去 `|| MaxTokens` 派生链）；`budgetReserve()=spec.MaxTokens`=**0**（未声明输出限=不预留，与 decorate 同源）→ budget=128000−0=**128000** → 96413<128000 **放行**（turn completed，整包 fixed prompt 入请求）。

### ② S2/S3/S4 + 门语义 + 输出余量 全绿 ✅

`go test ./assembly -run 'TestSymptomAnchorPhantomReserveRefusesFittingRequest|TestSymptomAnchorOutputLimitServed|TestSymptomAnchorReplayWindowAloneOpensNoGate|TestProviderWindowGateSemantics|TestSymptomAnchorUnifiedRequestBudget|TestBudgetReservesOutputAllowance|TestFoldWindowSemanticsUnchanged' -count=1 -v` → **F3_GREEN_EXIT=0**：
- S2 `TestSymptomAnchorOutputLimitServed` PASS（两子例：声明 max_tokens=8000→served GenerationConfig.MaxTokens=8000；未声明→nil）。
- S3 `TestFoldWindowSemanticsUnchanged` PASS（replay_window 驱动折叠，行为与旧 max_tokens 一致，回归锚）。
- S4 `TestSymptomAnchorReplayWindowAloneOpensNoGate` PASS（只声明 replay_window=64 无 context_window→门闭整包发，乙）。
- 门正锚 `TestProviderWindowGateSemantics` PASS（context_window>0 门开）；`TestSymptomAnchorUnifiedRequestBudget` PASS（固定提示全量入请求，预算只约束历史投影）。
- 负锚 `TestBudgetReservesOutputAllowance` PASS（输出限挤占输入预算：max_tokens=window→拒发）。

### ③ 远端 yaml strict 解码 + 预算门开（context_window>0）实测 ✅

- 部署声明（grep）：`examples/wechat-bot/tagent.yaml:109 replay_window: 128000`、`:110 context_window: 128000`（>0→门开），无 max_tokens 输出限声明（reserve=0）。`tagent.rl.yaml` 为最小 proxy 变体，agent 块与基线字节一致、从不声明预算窗（宿主不读），仅 :87 待接线注释 max_tokens→replay_window。
- **strict 解码 + 全装配 Boot 绿**：真实 `assembly.Boot("examples/wechat-bot/tagent.yaml")` 路径，仅把 journal 数据根换成干净 temp dir（隔离本地陈旧数据，见下），预算声明与其余键字节不变 → **exit 0**，storage tier opened（journal durable）、durable load scan、restore folded chains 全过，入口面=[action knowledge plan recall]、反思面=[…send]（face split 正确）。一次性证据测试跑毕即删（temp yaml 由 defer 清除），不留永久抽象。
- **本地 smoke boot 环境阻塞归因（非本变更回归）**：`TestLocalSmokeSymptomBootDeployed` 在本地直跑 → exit 1，红因 `memory: data directory holds the retired shard layout, open a new directory: .wechat-config/data/kv/...`（`ErrJournalLegacyLayout`，memory/ledger/config.go:101）。归因三证：(a) `.wechat-config/data` 是 **gitignore 本地数据**（`git check-ignore` exit 0）；(b) 本变更 diff **不触碰** memory/ledger 守卫（grep diff `retired shard|shard layout|open a new directory` exit 1 零匹配）；(c) 红发生在 yaml strict 解码**之后**的 memory 子系统 open（Boot 已越过配置解析）。即陈旧本地数据根（Error2 recover-kv 待用户跑）撞未改动的基线守卫，与预算键归位正交。远端微信助手实际运行中（数据根已迁移），本地为陈旧副本。干净数据根 Boot 绿（上）即配置面归位的消费者锚证据。

## F4 归档 + 导览 —— 待用户明示（R31 法定停点「提交归档需明示」）

- 计划：`openspec archive config-surface-normalization`（不 --skip-specs，01 delta request-budget-accounting MODIFIED 同步至主 spec）；归档后 `check-openspec.sh` 全绿；导览链接域→证据。
- **停点**：F1–F3 全绿，F4 归档与 commit/push 须用户明示授权，未执行。

## 实施期计划缺口回写（R11/R29，已落 design/二级 tasks）

- **R11 消费者漏枚举 ×2**：`golden/runtime/schema_allowlist.json`（宿主文档 agent 键白名单，W2 远端 yaml 新键触发门红）、`openspec/specs/orchestration/spec.md`（boundary-closure 旧预算门触发语义被乙取代）——原 design §七清单漏列，实施期补全并回写 §七，编排者共享面单写（R17）。
- **R29 锚精化 ×2（非缩水）**：04 4.1 原正锚 `TestHostDocument` 不存在（-run 零匹配空转 exit 0）→改真 assembly 门锚＋smoke boot；原负锚「max_tokens→unknown field 拒」伪（max_tokens 是合法输出限键）→改真预算挤占负锚 `TestBudgetReservesOutputAllowance`＋门闭负锚 `TestSymptomAnchorReplayWindowAloneOpensNoGate`。05 5.1 原 `grep -rn` 命中 gitignore 二进制＋docs/.dev 超时 exit 2→改 `git grep -nI`（仅跟踪源，exit 1 零残留）。
- **D6 独立纯度裁定（非用户裁定）**：params 删后 canonicalFloat 余消费方皆 omitempty（-0 已丢弃），探针 `/tmp/negzero_probe.go` 证冗余→随 params 删（行为保持，sectionsFingerprint f834ae0b→3a1232e6 纯由 params 恒编码项消失）。design D6 已附勘误。