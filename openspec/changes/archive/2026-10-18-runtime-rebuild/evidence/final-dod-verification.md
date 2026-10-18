# F3 DoD 核验素材 —— 非资源依赖测试收尾终验

> 派发单：**非资源依赖测试收尾终验**（F3 DoD 素材）。工作面 = 六项逐项独立取 `exit`（禁合并计数）＋ DoD 逐条判定。
> 依据：外部批准计划 §9「完整门禁命令」/ §10「验收、未决事项与最终交付」、`task-graph.json` 09.3/09.4/09.5/09.6、`expectations.md` O1–O5、`ORCHESTRATION.md` 台账（批次 36–44）。
> 白名单声明：**本文件是本单唯一写入产物**。未改产品代码/测试/脚本/CI/`openspec/**` 既有 artifact/`task-graph.json`，未 `commit`/`push`/`archive`，未触碰真实资源（微信/模型/embedding/collector/tokenizer/S10）。
> 过程件一律落**仓库外** `/tmp/f3dod/`（R26）。

## 0. 判定（先行）

- **六项逐项：5 项 PASS、1 项「如实红」（设计使然）**。逐项退出码与计数见 §2，无一项被合并或汇总掩盖。
- **非资源依赖测试面：全绿**——两 module `-race` 全量零 `DATA RACE`、检查器自测全族 331+122 tests、golden 确定性档 `drift=0`、14 个点名锚 10 域全覆盖（含零匹配防护）、三门全绿。
- **但 DoD 整体不可判「达成」**：①资源面 8 项**未验证（缺凭据）**；②两项**计划/结构面当前红**（`check_runtime_structure` 16 文件超限、`check_runtime_plan` 72 errors）；③一项**挂账**（`TestRealWechat` 为 `t.Fatalf` 桩且文件被 `.gitignore` 白名单排除，凭据到位也无法验收）。
- **环境异常（必须先看）**：会话期间 `openspec/specs/**` 被**兄弟单「delta-spec 同步」并发写入**（22:29:18–22:32:48，61 改 + 1 新增；归属已具名：`evidence/delta-sync-manifest.md`），非本单所为；本报告的门禁数字均在**写入静默后的同一内容快照**（`sha256=39ddece…`）上取得，并已对 §5 相关 DoD 项降级判定。详见 §6·F-E。

## 1. 环境与源码指纹

| 项 | 值 | 取法 |
|---|---|---|
| 仓库 / 分支 / HEAD | `/Users/pengweiye/Documents/codes/tagent` / `next` / **`7c4c712`**（`feat(runtime): land the full runtime rebuild across ten domains`） | `git rev-parse --short HEAD` |
| Go | `go1.27.1 darwin/arm64`（`go.mod` 要求 1.24） | `go version` |
| Python / openspec | `3.9.6` / `1.2.0` | `python3 --version`；`openspec --version` |
| 平台 | macOS `14.7.2`，Apple M3 Pro | — |
| 工作树 | 会话开始 **1 项**（`? examples/wechat-bot/skills/interview/agent-interview-helper`）；收尾 **63 项**＝61×`M openspec/specs/*/spec.md` + `?? openspec/specs/runtime-inspection/` + 上述 1 项 | `git status --short \| wc -l` |
| `openspec/specs` 内容快照 | `sha256(find … \| xargs shasum \| shasum) = 39ddecef414ef50c3ad169edfdc3f3934cc76000adf2d72482c04174b2d204b3`（门禁前后各取一次，**相同**） | §5 |
| 资源启用变量 | **未设置**（真实资源门因此合法 Skip，见 §4） | `env` 无 `TAGENT_REAL_*`/`TAGENT_WECHAT_*`/`OTEL_*`/`SFT_*` |
| 过程件 | `/tmp/f3dod/`（`T1-*`/`T2-*`/`T3-*`/`A-*`/`T5*`/`C-*`，每条命令一份日志 + 一份 `.exit`） | — |

## 2. 六项逐项结果（命令原文 / exit / 计数）

| # | 项 | 命令原文 | EXIT | 关键计数 |
|---|---|---|---|---|
| 1a | race·根 module | `go test -race ./... -count=1 -json` | **0** | 46 包 / 13 个有测试包 / **顶层测试 pass 194 + 子测试 pass 508 = 702** / fail **0** / skip **7**（全为资源门）/ **`DATA RACE` 命中 0** |
| 1b | race·微信 module | `go -C examples/wechat-bot test -race ./... -count=1 -json` | **0** | 1 包 / **顶层 57 + 子测试 55 = 112** / fail **0** / skip **1**（`TestRealWechat` 资源门）/ **`DATA RACE` 命中 0** |
| 2a | 检查器自测（Python 全族） | `PYTHONPATH=scripts python3 -m unittest discover -s scripts -p 'test_*.py'` | **0** | `Ran 331 tests in 10.868s` → `OK` |
| 2b | 检查器自测（Go 全族） | `go test ./scripts/... -count=1 -json` | **0** | `scripts/codetools` pass 65 / `scripts/comment_policy` pass 57 / fail 0 / skip 0 |
| 3a | golden runner·确定性档 | `python3 scripts/run_runtime_scenarios.py --scope deterministic` | **0** | `verdict: match`；`required=9 attested=9 drift=0 unattested=0`；`remaining_required: S10`；run-id `run-20261018T142857Z-deterministic` |
| 3b | golden runner·完整档 | `python3 scripts/run_runtime_scenarios.py --scope complete` | **1**（**设计使然，如实**） | `required=10 attested=9 drift=0 unattested=1`；`S10 unattested — no case ran for tests/integration/acceptance::TestScenario10EpisodeSplitAndModelReplacement`；run-id `run-20261018T142900Z-complete` |
| 3c | runner 证据后置校验（补充锚） | `python3 scripts/check_runtime_evidence.py --evidence <det evidence> --test-log <det test-log> --scope deterministic` | **0** | `RESULT: ok`；`test_log_packages=1 / rows=49 / tests=9`；`scope_is_complete=0` 且具名列出 `remaining=['S10']`；`productVerified: null` |
| 4 | 锚点名复跑抽样（10 域 14 锚） | 见 §3.4 逐条 | **14/14 = 0** | 每锚 `顶层 pass=1`（**零匹配防护通过**，无一名不符导致的空转） |
| 5a | lint 全门 | `bash scripts/lint.sh` | **0** | gofmt/build/vet 静默；`comment_policy: 0 finding(s); 0 beyond baseline`；`name-check` ok；`doc-refs: 43 citation(s) resolve … no dangling`；`gen_godoc: docs/api matches the source (47 packages)`；`proc-refs` ok |
| 5b | openspec 门 | `bash scripts/check-openspec.sh` | **0** | `Totals: 122 passed, 0 failed (122 items)` / `OK: all main specs pass validate --strict.` |
| 5c | openspec 全量 strict | `openspec validate --all --strict` | **0** | `Totals: 122 passed, 0 failed (122 items)` |

> **禁合并计数执行方式**：每条命令各自一个进程、各自一份日志、各自取 `$?`；表中数字均逐条来源于该条命令输出（解析脚本见 §7）。**未用 `grep/head/tail` 退出码替代原命令退出码**。
> `--scope complete` 的 `EXIT=1` **不是回归**：`scenarios.json` 唯一 scope 登记为 S01–S09=`deterministic`、S10=`resource`；完整档缺 S10 必须红（批次 3 P2-3 的合同行为）。**该红项与「S10 未验证（缺凭据）」是同一事实的两面，不得反向读成「只有 S10 未验」。**

## 3. 逐项明细

### 3.1 race（两 module）——零 DATA RACE

- 根 module：46 包、`fail=0`、`skip=7`（§4 全列）、`grep -c "DATA RACE"` → **0**。
- 微信 module：`fail=0`、`skip=1`（`TestRealWechat`）、`grep -c "DATA RACE"` → **0**。
- 与 09.3 报告（`gate09.3-2026-10-18`：688 pass / 7 skip）相比**测试数增加**（702）而 fail/skip 集合不变——增量来自 09.8/09.9 与 00.24–00.26 后端新增用例，非口径变化。
- **口径偏差如实登记**：本单跑的是**不带 `-short` 的全量**（09.3 用 `-short`）。不带 `-short` 只会**更多**用例而非更少；资源门仍合法 Skip（未设置启用变量）。

### 3.2 检查器自测全族

- Python 331 tests 覆盖：`check_runtime_plan` / `check_concept_budget` / `check_runtime_architecture` / `check_runtime_structure` / `check_runtime_evidence` / `test_check_runtime_workflow` / `test_run_runtime_scenarios` / `test_verify_runtime_acceptance` / `test_verify_restart_prefix` / `test_convert_trajectories`。
- Go 侧：`scripts/codetools`（62→现 65 pass，含 `doc-refs`/`name-check`/`merge-check` 正负控）、`scripts/comment_policy`（57 pass，含 ratchet/baseline/scope-guard 负控）。
- **机检口径**：`comment_policy` 全域 0 finding（批次 44 收口的 457 条注释债后），`baseline` 无上调记录（输出 `0 ratchet slot(s) can be lowered`）。

### 3.3 golden runner（两档）

确定性档逐场景（原文摘录）：

```
scope: deterministic
required: S01 S02 S03 S04 S05 S06 S07 S08 S09
remaining_required: S10
verdict: match
S01..S09  match   5/5/12/4/5/5/6/10/6 observation(s) equal
scenarios: required=9 attested=9 drift=0 unattested=0
```

完整档：`S10 unattested`（`no case ran for …TestScenario10EpisodeSplitAndModelReplacement`），`drift=0`——**S01–S09 与 golden 逐键相等**，S10 无记录即无 attest，**不伪造**。

### 3.4 锚点名复跑抽样（10 域 ≥1，实跑 14 锚）

| 域 | 锚（`-run '^Name$'`） | 包 | EXIT | 顶层 pass | 子测试 pass |
|---|---|---|---|---|---|
| 00-foundation | `TestDocRefsAnchorsCitationAtDocumentSnapshot` | `./scripts/codetools` | 0 | 1 | 0 |
| 01-state-binding | `TestSessionAuthority` | `./tests/integration/state` | 0 | 1 | 5 |
| 01-state-binding | `TestStateOwnsPipeline` | `./tests/integration/state` | 0 | 1 | 3 |
| 02-execution | `TestNativeConversationRoundTrip` | `./tests/integration/execution` | 0 | 1 | 3 |
| 02-execution | `TestIdentityAndCommitContract` | `./tests/integration/execution` | 0 | 1 | 6 |
| 03-history | `TestDurableTaskRoundTrip` | `./tests/integration/history` | 0 | 1 | 5 |
| 04-capabilities | `TestNativeToolsAndSkillState` | `./tests/integration/capabilities` | 0 | 1 | 3 |
| 05-inspection | `TestRuntimeInventory` | `./tests/integration/inspection` | 0 | 1 | 7 |
| 05-inspection | `TestObservationDoesNotOwnState` | `./tests/integration/inspection` | 0 | 1 | 5 |
| 06-learning | `TestCaptureFidelityAndRedaction` | `./tests/integration/learning` | 0 | 1 | 11 |
| 07-evolution | `TestGitNativeRefine` | `./tests/integration/evolution` | 0 | 1 | 7 |
| 08-host | `TestPublicRuntimeEntry` | `./tests/integration/host` | 0 | 1 | 4 |
| 08-host | `TestSoakContinuity` | `./tests/integration/host` | 0 | 1 | 4 |
| 09-acceptance | `TestOfflineMigration` | `./tests/integration/acceptance` | 0 | 1 | 4 |

- **零匹配防护**：每锚均以 14 份 `-json` 日志逐份核对「`Action=pass` 且 `Test` 与 `-run` 点名逐字符相同」→ 全部 `顶层 pass=1`。**无一条因名字漂移而空转 exit 0。**
- 抽样边界如实：本项是**抽样**（10 域 14 锚），**不是** 123 叶逐叶复跑；逐叶锚清单与历次复跑记录在 `09.7-review/*` 与 `ORCHESTRATION.md` 台账。

### 3.5 门禁（全门）

- `bash scripts/lint.sh` → **0**（11 步全走完，非 `set -e` 中断后的假绿：输出含末行 `lint: ok`）。
- `bash scripts/check-openspec.sh` → **0**；`openspec validate --all --strict` → **0**；两者 `122 passed, 0 failed`。
- 与 09.3 的差距归因（存量红已清）：`comment_policy 494 → 0`（批次 44）、`docs/api 27 stale+13 分歧 → fresh（47 包）`（09.8）、`doc-refs 43 dangling → 43 全锚定`（09.8/09.9）、`proc-refs 8 → 0`（09.9）。

### 3.6 补充检查器面（非派发项，为 DoD 判定取证，逐条独立 exit）

| 检查器 | 命令 | EXIT | 结果 |
|---|---|---|---|
| 架构·policy | `python3 scripts/check_runtime_architecture.py --check-policy --json` | **0** | `ok=true`；`policy_packages=20 / policy_edges=20` |
| 架构·implementation | `python3 scripts/check_runtime_architecture.py --implementation` | **0** | `RESULT: ok`；197 条 `external import … not in known_prefixes` 仅 warn |
| 证据合同 | `python3 scripts/check_runtime_evidence.py --check-contract --json` | **0** | `ok=true`；`scenario_count=10 / observation_values=70 / fields=18 / scope_is_complete=1` |
| 概念预算 | `python3 scripts/check_concept_budget.py --json` | **0** | `ok=true` |
| **结构** | `python3 scripts/check_runtime_structure.py` | **1** | **`RESULT: FAILED`；16 个产品文件 > 500 行**（详见 §6·F-A） |
| **计划** | `python3 scripts/check_runtime_plan.py --change-dir openspec/changes/runtime-rebuild --json` | **1** | **`ok=false`；72 errors**（71 ADDED 冲突 + 1 coverage 缺行，详见 §6·F-B） |

## 4. 资源门清单（不得冒充；本次全部合法 Skip）

| # | 门 | 测试（Skip 原文归属） | 状态 | 缺的凭据 |
|---|---|---|---|---|
| R1 | embedding/检索 | `capabilities::TestRecallEvaluation/真实检索资源与启用门:_R24_三态` | **未验证（缺凭据）** | `TAGENT_REAL_EMBEDDING_BASE_URL` / `_API_KEY`（原文：`resource gate R24, 0/3`） |
| R2 | embedding 真实 store | `capabilities::TestSearchRecallsFromRealStore/the_real_embedding_endpoint_carries_the_same_recall` | 未验证（缺凭据） | 同上 |
| R3 | collector/OTel | `inspection::TestRealCollectorDelivery` | 未验证（缺凭据） | 导出端 + 授权 + 预算 |
| R4 | SFT/tokenizer | `learning::TestSFTConsumer/the_real_tokenizer_and_artifact_face_is_reported_unseen_until_its_gate_is_set` | 未验证（缺凭据） | 真实 tokenizer/制品 |
| R5 | 真实模型判据 | `evolution::TestRealLLMJudgeArtifact` | 未验证（缺凭据） | 模型端点 + allowlist |
| R6 | 存储档（rustviking） | `history::TestStorageCapabilities/file_tier_runs_the_real_rustviking_CLI_and_names_its_absence` | 未验证（缺凭据） | rustviking CLI/存储档 |
| R7 | 原型子进程恢复 | `prototype::TestAgentRecoverySubprocess` | **非资源门**（`prototype/` 演示包自述 Skip，不计入产品面） | — |
| R8 | 微信真实往返 | `wechat-bot::TestRealWechat` | **未验证（缺凭据）＋ 挂账**（见 §6·F-C） | `TAGENT_WECHAT_TOKEN` / `_REAL_TARGET` / `_SEND_AUTHORIZED` |
| R9 | S10 学习数据场景 | runner `S10 unattested` | 未验证（缺凭据） | 真实 SFT 批次（不得以合成替身凑） |

> 口径：**无门=合法 Skip、有门缺凭据=Fatal、正式必验项不得以 Skip 结项**。本轮全部为「无门合法 Skip」；**均不计完成**。

## 5. DoD 逐条核验（F3 素材）

> 判据来源：外部批准计划 §10（孙任务/二级域/顶层三级验收＋最终交付七项＋归档授权）＋ `tasks.md` 2.3（部署/真实资源/SFT/微信/未决项/正式消费者）＋ `expectations.md` O1–O5。
> 判定三态：**达成**（本轮独立复跑绿有凭据）｜**未验证（缺凭据）**（资源门）｜**挂账**（缺实现或账目需回写）。

| # | DoD 项 | 判据 | 判定 | 凭据 / 缺口 |
|---|---|---|---|---|
| D1 | 孙任务完成需实际产物＋正式消费者＋点名验证 | 123 叶勾选 + 锚复跑 | **达成（抽样口径）** | 叶勾选 `29+8+15+10+18+7+11+7+7+11 = 123`，未勾 0（`grep -c '^- \[x\]'` 逐域）；本单 14 锚 10 域全绿（§3.4）。**未做** 123 叶逐叶复跑，故限"抽样" |
| D2 | 二级域覆盖全部适用场景 | golden S01–S09 确定性 + S10 资源 | **部分达成** | 确定性 9/9 `match`、`drift=0`；S10 `unattested` → 未验证（缺凭据） |
| D3 | 顶层跨域 golden＋双 module＋档位＋真实路径 | 09.1/09.3 | **达成（确定性档）** | runner 两档实跑（§3.3）；两 module race/test/build/vet 全绿（§2、§3.1）；`--scope complete` 红即 S10 缺口凭证 |
| D4 | 可运行公共入口（Assemble 唯一装配入口） | 08.1 | **达成** | `TestPublicRuntimeEntry` 1+4 pass；T1 裁决后死桩已删（`Build/Runtime/errNotImplemented` 零残留，属既有批次 40 凭据） |
| D5 | 微信宿主真实往返 | 08.5 / 09.5 微信子项 | **未验证（缺凭据）＋挂账** | `TestRealWechat` Skip（无凭据）；**且其函数体为 `t.Fatalf("the real send face is not wired yet…")`、文件 `examples/wechat-bot/real_wechat_gate_test.go` 被 `.gitignore` 全忽略白名单排除**（§6·F-C） |
| D6 | 架构与场景「正式档」 | 00.4 | **达成** | `architecture --check-policy ok`（20/20）；`evidence --check-contract ok`（10 场景/70 观测值/18 字段） |
| D7 | 真实运行 golden 及检查器 | 09.1 | **达成（确定性档）** | runner `deterministic` + 后置 evidence 校验 `RESULT: ok`（§2·3c）；检查器自测 331+122（§3.2） |
| D8 | 完整验收报告 | 09.3/09.4/09.5/09.6 | **部分达成** | `09.3-gate-run.md`（16 门逐条）、本文件、`09.6-doc-closure.md`、`09.7-review/*`；**`09.4` 无独立 evidence 文件（仅 `ORCHESTRATION.md` 批次 37 台账行）**；`09.6` 报告为 17:xx 时点、其 V1/V3 现转红（§6·F-D） |
| D9 | SFT 消费/反馈闭环证据 | 06.6/06.7 | **未验证（缺凭据）** | `TestSFTConsumer` 真实 tokenizer 子测 Skip（R4）；T5 裁决维持"勾选+标注"口径 |
| D10 | 明确的未完成或待裁清单 | 09.7 T1–T10 | **达成** | `09.7-review/00-summary.md` T1–T10 裁决书 + `ORCHESTRATION.md` 裁决落地记录；**本单新增 2 条待处置（F-A/F-C）需并入** |
| D11 | 迁移读旧写新、源字节不变 | 09.2 | **达成** | `TestOfflineMigration` 1+4 pass |
| D12 | 完整门禁逐条原始退出码 | §9 门禁 | **部分达成** | race/lint/openspec 全绿；**structure/plan 两红**（§6·F-A/F-B）→ 不满足"全门绿" |
| D13 | 归档仅在用户独立授权后 | F4 | **未做（非失败）** | 未 `archive`、未 `commit`；主规格同步（归档前置）由兄弟单在途完成（`delta-sync-manifest.md` 自述用户已明示归档授权），但 change 目录仍在、delta 未移除 |
| D14 | 三类证据互不替代（plan≠structure≠runtime） | 口径 | **达成** | 全部检查器输出 `productVerified: null`；本报告不将任何 `ok` 读作产品通过 |

**DoD 总判：不可判「达成」。** 确定性测试面可判通过；缺口集中于 **资源门 8 项（含微信锚未接线）＋ 结构门 16 文件 ＋ 计划门 72 errors ＋ 09.4/09.6 证据面账目**。

## 6. 发现与处置建议（F2/F4 前置）

### F-A｜`check_runtime_structure.py` 红：16 个产品文件超 500 行（EXIT=1）

`counts: go_files=377, product_files=280, test_files=97, violations=16, largest_product_lines=996`。清单（原文顺序，行数降序）：

`query/snapshot.go 996`、`agent/loop.go 926`、`query/inventory.go 887`、`rl/export.go 640`、`rl/replacement.go 635`、`memory/localfile.go 635`、`state/migrate.go 580`、`evolution/posterior.go 563`、`rl/capture.go 554`、`agent/delegate.go 551`、`memory/projection.go 535`、`rl/skeleton_assert.go 531`、`config/config.go 530`、`evolution/refine_git.go 509`、`plugin/approval.go 506`、`examples/wechat-bot/host_runtime.go 505`。

- **与 09.6 时点（16 文件、`largest=996`、`go_files=375`）基本一致**：`00.22` 只拆了 `run.go`，未处理这 16 个；`go_files` +2 属新用例文件。
- **建议**：此为 09.6 的 verification 命令之一（V3），**09.6 的勾选当前不被自身验证支持**；F2 若要"全门绿"，须二选一并留痕——(a) 拆包/拆文件降到 500 以下（`00.22` 同类工作，需白名单与回归锚）；(b) 由用户裁定"产品文件上限 500 不适用于这 16 个已收敛文件"并改策略（须正反样例，不得上调 baseline 掩盖）。**本单无权限处置，仅登记。**

### F-B｜`check_runtime_plan.py` 红：72 errors（EXIT=1，`ok=false`）

- **71 项**：`ADDED requirement '…' already exists in main spec (collision; use MODIFIED)`，覆盖 orchestration / architecture-guardrails / config-state-twinship / async-task-execution / storage-lifecycle-completion / runtime-inspection 等 20+ 能力。
- **1 项**：`coverage capability set mismatch: missing=['runtime-inspection'], extra=[]`。
- **根因（有证据）**：`openspec/specs/**` 在 **22:29:18–22:32:48** 被兄弟单「delta-spec 同步」写入（61 文件改 + 新增 `runtime-inspection`，见 `evidence/delta-sync-manifest.md`：71 ADDED 追加 / 57 MODIFIED 替换 / 322→393 条守恒），而 `changes/**` 的 delta 仍是 `ADDED` 语义。证据链：① delta 文件 mtime 仍为 `Oct 13 14:16`（未改）；② 09.6 报告 V1（17:xx）同一命令 `ok=true`；③ `openspec validate --all --strict` 侧证无副作用（连跑前后 spec `sha256` 与 mtime 不变）。
- **性质**：**同步在途的瞬时红**，归档（F4）把 delta 合入并移除 change 目录后消解；但**归档未获授权前它一直是红的**，故 F2"全门绿"不可在此状态声明。
- **建议**：并入 F4 前置清单；另需在 `coverage.md` 补 `runtime-inspection` 行（`missing=['runtime-inspection']`）。

### F-C｜`TestRealWechat` 是 `t.Fatalf` 桩 + 文件被 gitignore（**挂账**）

- 08.5 与 09.5 微信子项的**唯一**验收命令都是 `go -C examples/wechat-bot test ./... -run '^TestRealWechat$' -count=1 -json`。
- 该测试体：无凭据时 `Skip`（合法资源门）；**凭据齐备时直接 `t.Fatalf("the real send face is not wired yet …")`**——即**凭据到位也必然红**，真实发送面尚未接线。
- 且 `examples/wechat-bot/.gitignore` 为**白名单模式**（首行 `*`），放行清单未包含 `real_wechat_gate_test.go` → 该文件**不在版本控制内**（`git check-ignore -v` 命中 `.gitignore:5:*`），提交树中不存在此门。
- **影响**：D5 不能仅以"缺凭据"结案；须登记为**实现缺口 + 版本控制缺口**。**建议**：① 白名单显式放行该文件；② 真实发送路径接线（或以 `Skip`/`BLOCKED` 语义显式替代 `Fatalf` 桩）；③ 在此之前 08.5/09.5 微信子项维持"未验证（缺凭据）+ 未接线"双标注，**不得归档为准入**。

### F-D｜09.6 勾选与其自身 verification 不一致（**账目**）

- `changes/09-acceptance/tasks.md` 09.6 = `[x]`，但：
  - V3 `check_runtime_structure.py` → **exit 1**（F-A）；
  - V1 `check_runtime_plan.py` → **exit 1**（F-B，同步在途引起）；
  - 而 `evidence/09.6-doc-closure.md` §0 明确写"**09.6 不闭合 → 如实保持未完成（不可勾选）**"，其后 09.8/09.9 清掉的 C 轴（docs/api/注释债）**未回写该文件**。
- **建议**：按 T8 先例做**账目勘误**——(a) 更新 `09.6-doc-closure.md` 记录 09.8/09.9 已消解项与现存 V1/V3 红；(b) 在 09.6 行标注"结构门 16 文件与计划门同步在途为已知红，归 F2 前置"，与勾选并存但不得读作全绿。**本单无权限写这些文件。**

### F-E｜`openspec/specs/**` 并发写入（**环境异常，R17**）

- 会话开始工作树 1 项；22:29:18 起 61 份主规格被改写（全部同一 mtime 批次），22:30:19 新增 `openspec/specs/runtime-inspection/`（untracked），22:32:19/22:32:33/22:32:48 又成批改写——**非本单任何命令所为**（本单命令集不含 `openspec/**` 写操作；`openspec validate` 已实测零副作用）。**归属已具名**：兄弟单「delta-spec 同步」（`evidence/delta-sync-manifest.md`，22:34:33 落盘，自述"runtime-rebuild archive 前置；用户明示归档授权"）。
- **处置**：① 本单第一次 `check-openspec`/`validate`（22:29:14–15，`121 items`）落在写入中途，**已作废**；② 22:33:10 与 22:33:17 两次取 `sha256=39ddece…` **相同**，说明该窗口内无写入，门禁 122 items 结果取自该**一致快照**；③ 后续若再有写入，§2·5b/5c 需复跑。
- **建议**：该写入已具名归属（delta-spec 同步单，F4 archive 前置），**不得**把 plan 门红当作产品缺陷；但归档动作完成、change 目录移除之前 plan 门始终红，故 F2「全门绿」不可在本快照声明。若该单后续再写 `openspec/specs/**`，§2·5b/5c 需按 `sha256` 复验后复跑。

## 7. 复跑命令（逐条可复跑；日志落在 `/tmp/f3dod/`）

```sh
cd /Users/pengweiye/Documents/codes/tagent

# 1 race 两 module（各自取 exit；DATA RACE 独立 grep）
go test -race ./... -count=1 -json > /tmp/f3dod/T1-root-race.json; echo $?
go -C examples/wechat-bot test -race ./... -count=1 -json > /tmp/f3dod/T1-bot-race.json; echo $?
grep -c "DATA RACE" /tmp/f3dod/T1-root-race.json /tmp/f3dod/T1-bot-race.json

# 2 检查器自测全族
PYTHONPATH=scripts python3 -m unittest discover -s scripts -p 'test_*.py'; echo $?
go test ./scripts/... -count=1 -json > /tmp/f3dod/T2-go-scripts.json; echo $?

# 3 golden runner 两档（deterministic 期望 0；complete 期望 1 且 S10 unattested）
python3 scripts/run_runtime_scenarios.py --scope deterministic; echo $?
python3 scripts/run_runtime_scenarios.py --scope complete; echo $?

# 4 锚抽样（示例两条；全表见 §3.4）
go test ./tests/integration/state -run '^TestSessionAuthority$' -count=1 -json; echo $?
go test ./tests/integration/host -run '^TestPublicRuntimeEntry$' -count=1 -json; echo $?

# 5 门禁
bash scripts/lint.sh; echo $?
bash scripts/check-openspec.sh; echo $?
openspec validate --all --strict; echo $?

# 6 补充（DoD 判定用）
python3 scripts/check_runtime_structure.py; echo $?          # 当前 1（16 文件）
python3 scripts/check_runtime_plan.py --change-dir openspec/changes/runtime-rebuild --json; echo $?  # 当前 1（72 errors）
python3 scripts/check_runtime_architecture.py --check-policy --json; echo $?
python3 scripts/check_runtime_architecture.py --implementation; echo $?
python3 scripts/check_runtime_evidence.py --check-contract --json; echo $?
python3 scripts/check_concept_budget.py --json; echo $?
```

**JSON 计数解析口径（本报告数字的来源，禁"看输出猜数"）**：

```sh
python3 - <<'PY'
import json
for f in ('/tmp/f3dod/T1-root-race.json','/tmp/f3dod/T1-bot-race.json'):
    top=set(); sub=fail=pp=0; sk=[]
    for line in open(f):
        line=line.strip()
        if not line: continue
        e=json.loads(line); a=e.get('Action'); t=e.get('Test')
        if not t:
            pp += (a=='pass'); continue
        if a=='pass': sub += ('/' in t); top.add(t.split('/')[0])
        elif a=='fail': fail += 1
        elif a=='skip': sk.append(t)
    print(f, 'top', len(top), 'sub', sub, 'fail', fail, 'skip', len(sk), 'pkg_pass', pp)
PY
```

## 8. 未改声明与凭据落点

- **唯一写入**：本文件 `openspec/changes/runtime-rebuild/evidence/final-dod-verification.md`（写后校验：`wc -l` / 段落锚 grep / `git status --short` 仅此新增，见下方"写后校验"节）。
- **未改**：产品代码、测试、`scripts/**`、CI、`openspec/changes/**` 既有 artifact、`task-graph.json`、`golden/**`、`docs/**`；未 `commit`/`push`/`archive`；未启用任何真实资源变量。
- **外部变更（非本单）**：`openspec/specs/**` 61 改 + 1 新增（§6·F-E，归属=兄弟单 delta-spec 同步，见 `evidence/delta-sync-manifest.md`），仅登记不处置。
- **过程件**：`/tmp/f3dod/`（`T1-root-race.json|.exit`、`T1-bot-race.json|.exit`、`T2-unittest.out|.exit`、`T2-go-scripts.json|.exit`、`T3-runner-det.out|.exit`、`T3-runner-complete.out|.exit`、`T3-followup.out`、`A-00…A-09.json|.err`、`T5-lint.out|.exit`、`T5b-check-openspec.out`、`T5b-validate.out`、`T5b-plan.json`、`C-structure.out`、`C-arch-policy.json`、`C-arch-impl.out`、`C-evidence.json`、`C-concept.json`、`specs-hash-before.txt`）。
