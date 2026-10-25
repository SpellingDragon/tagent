# Tasks — 04-contract-gates（契约与门禁收口）

> **状态：恰 3 个叶 checkbox（4.1—4.3），全部勾选。** 4.4 由 P-05 裁删（本变更无 `task-graph.json`，plan 门无判定对象，适配叶为空转；见一级「待裁项裁定」）。残余见「实施回写」§5（doc budget 基线 +28 与 v1 S03/S11 既有漂移），两项都不在本单白名单。
>
> **前置（硬，写在每叶"前置"栏）**：4.1/4.2/4.3 的验收对象是"01/02 行为落地后的语义"，已在 01 域三叶与 02 域三叶全部勾选**之后**启动（一级 2.1/2.2 已勾，HEAD `9cc4b20`）。
>
> **串行**：4.1 → 4.2（同簇：4.2 的修订必须与 4.1 的条款逐句对齐，且 4.2 改名的测试名被金样 `anchor_tests` 引用，必须同批改，否则中间态出现悬空名）。4.3（`.github/` + `scripts/test_check_runtime_workflow.py`）与 4.1/4.2 文件簇不相交，本单合批实施。
>
> **红线**：① `golden/runtime/event_integrity_scenarios.json` 零改动（一级 F1；实测 `git diff --stat` 为空）；② 归档 `openspec/changes/archive/**/task-graph.json` 只读（合同与它双向严格互校）；③ `EVENT_ROLES`/`EVENT_SCOPES` 的既有值不放松；④ S01–S14 与既有 `TestLocalSmoke*` 用例的期望不动；⑤ 不改 01/02 已交付的行为代码（`runtime/agent/loop.go`、`host.go`、`examples/wechat-bot/wechat.go`、`tool/exec/**`）；⑥ 不为本案新建 `task-graph.json`。
>
> **证据口径**：`-json`/日志落仓库外 `/tmp/event-semantics-boundary/04.x/`，仓内不新增证据文件；退出码原样保留（禁 `| tee` 取管道码、禁 `|| true`）。4.2 的九条锚裁决见下「九锚裁决」表（design D2 逐条回填，只写"已核对"不算交付）。
>
> **上报项**：P-04 = 4.2 写集含 `golden/runtime/scenarios.json`（S15 段）与两条既有测试，超出一级 Impact 的列举面，已由一级「待裁项裁定」批准；P-06 = 合同 `notes[0]` 的"141"与实测 143 不符（F1 冻结，仅记录不修）；P-07 = 一级"16 叶"与实测 15 叶的差额；**P-08** = 主规格 +28 行撞满 `scripts/doc_budget.json` 基线（见 §5.1）；**P-09** = v1 金样 S03/S11 在 HEAD 即漂移（见 §5.2）。P-06/P-07/P-08/P-09 均在「实施回写」有复跑命令。

## 三叶账目

- [x] 4.1 主规格按 **D8-5 裁定**新增一条 requirement「循环面的模型感知边界」（`## ADDED Requirements`，不是原计划的 MODIFIED 全文替换；域 delta 与主规格同文，细节引用 `quiet-receipts`/`failure-notice` 不复述）+ 三场景「受理回执不构成模型材料 / progress 只落 fact 不进 intake / 失败回合必达用户告知」—— 验证：`E=/tmp/event-semantics-boundary/04.1; mkdir -p "$E"; openspec validate persistent-event-loop --strict --json > "$E/spec-strict.json"; echo "spec=$?"; openspec change validate event-semantics-boundary/changes/04-contract-gates --strict --json --no-interactive > "$E/delta-strict.json" 2>/dev/null; echo "delta=$?"; grep -c '^#### Scenario:' openspec/specs/persistent-event-loop/spec.md; grep -cE '^#### Scenario: (受理回执不构成模型材料|progress 只落 fact 不进 intake|失败回合必达用户告知)$' openspec/specs/persistent-event-loop/spec.md; grep -c '^### Requirement: 循环面的模型感知边界$' openspec/specs/persistent-event-loop/spec.md`（实测 `spec=0` / `delta=0` / `72` / `3` / `1`；`spec-strict.json` → `summary.totals.passed=1, failed=0`，`delta-strict.json` → `valid=true, errors=0, warnings=0`）；正锚 三新场景各命中恰一次（`3`）、新 requirement 恰一处（`1`），主规格 scenario 总数 69→72；与主规格既有 69 个 scenario 名做重名扫描（`sort | uniq -d`）为空；负锚（形态）：`git diff -U0 openspec/specs/persistent-event-loop/spec.md | grep -c '^+### Requirement:'` = `1`（只增这一条；原"不许新增 requirement"已被 D8-5 取代，理由见 §1）；负控（校验器咬得住）：临时给 delta 追加一条无规范词、无场景的 requirement → `openspec change validate ... --json` 报 2 个 ERROR / `valid=false`，撤回后恢复 `valid=true`；消费者锚 `bash scripts/check-openspec.sh` exit=0（主规格 strict 门的正式入口；Totals `128 passed, 1 failed (129 items)`，唯一失败是既有 `change/post-training-data-loop`，与本改动无关）；**残余 P-08**：主规格 +28 行使 `scripts/lint.sh` 的 markdown doc budget 红，基线不在白名单（§5.1）；前置 01 域三叶与 02 域三叶已勾

- [x] 4.2 按 design D2 的九条锚（A1–A9）逐条裁决并修订过期见证者（`golden/runtime/scenarios.json` S15 `title`/`anchor_tests`/`observations` 三处；`tests/integration/execution/progress_notification_test.go` 父测试改名 `TestProgressNotificationStaysOutOfTheDecision` + 子测试反向改写；`tests/integration/acceptance/scenario_s15_test.go` 注释 + `carried` 见证断言 + fact 落地断言）+ 裁决逐条回填下表 —— 验证：`E=/tmp/event-semantics-boundary/04.2; mkdir -p "$E"; go test ./tests/integration/execution -run '^TestProgressNotificationStaysOutOfTheDecision$' -count=1 -race -json -timeout 20m > "$E/progress.json" 2>&1; echo "progress=$?"; go test ./tests/integration/acceptance -run '^TestScenario15TaskLogicalStatus$' -count=1 -race -json -timeout 20m > "$E/s15.json" 2>&1; echo "s15=$?"; python3 scripts/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/archive/2026-10-18-runtime-rebuild/task-graph.json --json > "$E/contract-v1.json" 2>&1; echo "v1=$?"; python3 scripts/check_runtime_evidence.py --check-contract --contract golden/runtime/event_integrity_scenarios.json --json > "$E/contract-v2.json" 2>&1; echo "v2=$?"; git status --porcelain golden/runtime/event_integrity_scenarios.json | wc -l`（实测 `progress=0` / `s15=0` / `v1=0` / `v2=0` / `0`；progress 的 4 个 pass 事件 = 父测试 + 2 子测试 + 1 包级 pass；s15 的 2 个 pass 事件 = 父测试 + 包级 pass，记录 `progress_reaches_decision:false` 且 `logical_status_ladder:["watch","stable","suspect"]`）；正锚 `TestProgressNotificationStaysOutOfTheDecision/progress_lands_as_a_fact_without_entering_the_decision` 绿（下一请求不含 progress 正文、事实按 `agent.NoticeFactKey` 恰 1 条且内容含 progress 文本、无 task tool 消息）与 `TestScenario15TaskLogicalStatus` 绿（`carried=false`，A7 的见证断言在场；提交面 `scenarioProgressFacts` 断言 fact 恰 1 条）；负锚 子测试 `a_notification_that_is_not_progress_carries_nothing_into_the_decision` 保留且绿（回滚即红：在 `git clone --local` 的 `49fd79f` 克隆里放新测试 → progress 测试 exit=1，报 `want no progress report in the material a decision reads`；S15 exit=1，报 `want the progress of the call carried into no decision` 且记录 `progress_reaches_decision:true`）；消费者锚 `TestScenario15TaskLogicalStatus` 经 `ScenarioOf("S15")` 从 v1 金样取数（生产取数路径），`check_runtime_evidence.py --check-contract --plan-graph <归档 graph>` 是 v1 金样的正式校验入口、exit=0（`scripts/lint.sh` 因无活动 graph 打印 skip，故本条以归档 graph 直调取证）；A8 核对 `TestBackflowNotificationLanes` exit=0（4 子测试 + 父测试共 5 个 PASS，含 `progress_commits_fact_without_model_material` / `unaddressed_progress_commits_nothing`）；A1 零改动：`golden/runtime/event_integrity_scenarios.json` `git diff --stat` 为空；前置 4.1、01.3 已勾（progress 读路径已断）

- [x] 4.3 把三条症状测试接进 CI：`.github/workflows/ci.yml` 新增 job **`event-semantics-symptoms`**（一次 PR / main·dev push 无条件触发；`runs-on: ubuntu-latest`、`go-version: '1.24'` 与既有 job 一致、`timeout-minutes: 20`；跑 `go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' -count=1 -json -timeout 600s`，装 tmux，`-json` 落 `$RUNNER_TEMP/symptom/symptom.json` 并以 `if: always()` + `if-no-files-found: error` 上传 artifact `event-semantics-symptoms`；无 paths filter / 无 job 级 `if` / 无 `continue-on-error`）+ `scripts/test_check_runtime_workflow.py` 增 `assert_symptom_job` 与正/负控（job 存在、三父测试由前缀正则全覆盖且不多吃、`-timeout 600s` 落在 job 预算内、Go 版本与 `test` job 一致、tmux 在场、artifact 在失败时仍上传、selector 窄化即红）—— 验证：`E=/tmp/event-semantics-boundary/04.3; mkdir -p "$E"; go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' -count=1 -json -timeout 600s > "$E/symptom.json" 2>&1; echo "smoke=$?"; python3 -m unittest scripts.test_check_runtime_workflow > "$E/workflow.log" 2>&1; echo "workflow=$?"`（实测 `smoke=0`、`workflow=0`，`Ran 44 tests ... OK` = 原 39 + 本叶 5）；正锚 `symptom.json` 里恰 9 个 pass 事件（3 父测试 + 6 子测试）、0 个 skip、0 个 fail，父测试为 `TestLocalSmokeSymptomAcceptedSilent` / `TestLocalSmokeSymptomSettledOnce` / `TestLocalSmokeSymptomFailureNotice`；负锚 5 条全部咬住（删 job、job 级 `if`、job/step `continue-on-error`、selector 窄化成单条全名或放宽成 `^TestLocalSmoke`、丢 `-timeout 600s` / 预算 45m / Go 版本漂移 / 丢 tmux / artifact 改 `success()`）；既有 `test_event_jobs_have_no_skip_hatch`（全文禁 paths filter 与 continue-on-error）保持绿；消费者锚 `.github/workflows/ci.yml` 的 job 定义本身是生产入口（`actions/checkout → setup-go → apt tmux → go test -tags smoke`），`scripts/test_check_runtime_workflow.py` 解析真实 `ci.yml`（PyYAML）作为机读证据；**外部动作如实登记**：把 `event-semantics-symptoms` 加进 branch protection 的必需检查无法在本仓验证，本叶只证"job 已无条件存在"，job 注释亦写明该点；前置 00 域、01.1、02.3 已勾

## 九锚裁决（design D2 逐条回填）

| # | 锚 | 裁决 | 依据与证据 |
|---|---|---|---|
| A1 | v2 合同 `01.1/TestEventIntegrityNoticeIdentity::multiple_progress_ordered_by_seq` | **保留，零改动** | 断 `Seq` 稳定有序与 `runNoticeProgressOrder` 的 fact 层语义，01.3 未动 `commitNotice`/幂等；`git diff --stat golden/runtime/event_integrity_scenarios.json` 空；v2 `--check-contract` exit=0 |
| A2 | v1 金样 S15 `title` | **修订** | 改为"＋progress 只落 fact、不进下一次决策且不答调用"；旧标题"回调抵达下一次决策"在 01.3 后成假话 |
| A3 | v1 金样 S15 `anchor_tests[2]` | **修订**（与 A5 同批） | 改指 `tests/integration/execution:TestProgressNotificationStaysOutOfTheDecision`；`grep -rn 'TestProgressNotificationReachesTheAgent' --exclude-dir=.git .` 在代码、金样与规格中 0 命中（仅本表与 design D2 的历史记录保留旧名；另有一个被 `.gitignore` 的本地测试二进制 `.tmux-int-linux.test` 命中，不入库） |
| A4 | v1 金样 S15 `observations.progress_reaches_decision` | **修订** | `true`→`false`；键名保留（问句是"是否抵达"，false 语义精确，键集不动） |
| A5 | `tests/integration/execution/progress_notification_test.go` 父/子测试 | **修订** | 父测试改名；子测试改 `progress_lands_as_a_fact_without_entering_the_decision`；断言翻转为"下一请求不含 progress 正文 + `NoticeFactKey` 恰 1 条 fact + 无 task tool 消息"；`-race` exit=0；回滚即红见 4.2 |
| A6 | 同文件"非 progress 通知不改变 decision"子测试 | **保留** | 原样保留，反向边界哨兵仍绿（回滚克隆中该子测试也绿，证明红只来自 A5 翻转） |
| A7 | `tests/integration/acceptance/scenario_s15_test.go` 注释与 `carried` | **修订（并补齐见证结构）** | 注释改新语义；补 `if carried { t.Errorf(...) }`、`NoticeFactKey` fact 断言、`scenarioProgressFacts` 提交面。**结构改为两次 Drive**：01.3 后 `Backflow` 早于首次 Drive 会因 range 未寻址而丢弃 progress，`carried` 恒 false，见证会空转；先 Drive 开 range 再 Backflow，才真正见证"只落 fact、不进决策" |
| A8 | `runtime/agent/loop_test.go` `TestBackflowNotificationLanes` | **核对：绿**（不在本域写集） | 01.3 已反向改写为 `progress_commits_fact_without_model_material` / `unaddressed_progress_commits_nothing`；本轮 `go test ./runtime/agent -run '^TestBackflowNotificationLanes$' -count=1` exit=0，`-v` 5 个 PASS |
| A9 | 主规格「多 progress 稳定 seq 有序」 | **保留，零改动** | fact 层断言；4.1 的 ADDED requirement 不触碰 `Handoff 未来通知的目标、实例与稳定序号`；该 requirement 块 `git diff` 无 `-` 行 |

## 实施回写（D6，2026-10-25）

### §1 4.1 的形态由 MODIFIED 改为 ADDED（D8-5）

一级 `design.md` D8-5 裁定：主规格以 `## ADDED Requirements` 新增一条「循环面的模型感知边界」（三场景），细节引用 `quiet-receipts`/`failure-notice`，**不复述**两 capability 的正文。这与本域 design D1（原计划 MODIFIED 两条既有 requirement、不新增 requirement）冲突，按一级裁定执行；理由是：把三族边界挂到既有两个 requirement 下会迫使主规格在"通知驱动"与"失败极性"两处各写半份同义条款，而 ADDED 一条边界 requirement 才能让"循环面只看边界、语义看两 capability"的单一事实源成立。docs 侧改动仅主规格 +28 行。

### §2 4.3 的 job 名与命令以派发单为准（覆盖 design D3）

- job 名：`symptom-anchors`（design D3）→ **`event-semantics-symptoms`**（派发单指定；同名也是 artifact 名，且不匹配 `event-integrity-*` 下载 glob，不会被 event summary 吸进 bundle）。
- 命令：design D3 的 `-race -json -timeout 30m` → 派发单的 `-count=1 -json -timeout 600s`（保留 `-json` 以留下逐测试 pass/skip 证据；实测本机 smoke 全套 ~4.5s，600s 为冷编译留足余量，job 预算 20m）。
- `scripts/preflight.sh` **未改**：其 `(cd examples/wechat-bot && go test -tags smoke ./... -run TestLocalSmoke ...)` 已用前缀覆盖三条症状测试，再加一份会变成第二条入口。

### §3 写集申报（超出派发单白名单的两处）

派发单白名单未列 `tests/integration/execution/progress_notification_test.go` 与 `tests/integration/acceptance/scenario_s15_test.go`，但 4.2 的 A5/A7 明确要求改这两个文件，且 4.2 的验收命令点名新测试名与 `carried=false`；不改则 01.3 后 `go test ./... -short` 必红。已按最小改动执行，并在 P-04 的批准范围内（一级「待裁项裁定」P-04 明列两条测试）。未触碰任何产品代码（`runtime/**`、`host.go`、`examples/wechat-bot/wechat.go`、`tool/exec/**` 零 diff）。

### §4 验证矩阵（原始退出码）

| 项 | 命令 | 结果 |
|---|---|---|
| 主规格 strict | `openspec validate persistent-event-loop --strict --json` | exit=0，passed=1 / failed=0 |
| 域 delta strict | `openspec change validate event-semantics-boundary/changes/04-contract-gates --strict --json --no-interactive` | exit=0，valid=true / errors=0 |
| 主规格计数 | `grep -c '^#### Scenario:' …/persistent-event-loop/spec.md` | 72（69+3）；新 requirement 1 处 |
| v2 合同零改 | `git diff --stat golden/runtime/event_integrity_scenarios.json` | 空 |
| v1 只动 S15 | `git diff golden/runtime/scenarios.json` | 仅 S15 的 `title` / `anchor_tests[2]` / `progress_reaches_decision` 三行 |
| v1 合同 | `check_runtime_evidence.py --check-contract --plan-graph <归档 graph>` | exit=0 |
| v2 合同 | `check_runtime_evidence.py --check-contract --contract golden/runtime/event_integrity_scenarios.json` | exit=0 |
| workflow 自测 | `python3 -m unittest scripts.test_check_runtime_workflow` | exit=0，44 tests OK（39+5） |
| evidence 自测 | `python3 -m unittest scripts.test_check_runtime_evidence` | exit=0，95 tests OK |
| 症状 smoke（CI 口径） | `go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' -count=1 -json -timeout 600s` | exit=0，9 pass / 0 skip / 0 fail（3 父 + 6 子） |
| 全量 lint | `bash scripts/lint.sh` | **exit=1，唯一点：markdown doc budget +28（P-08）；其余已过的门见 §5.1** |

### §5 残余（两项，均需上级裁定；不在本单白名单）

#### §5.1 P-08：主规格 +28 行撞满 doc budget 基线

现状：`scripts/doc_budget.json` 基线 `197 files / 22192 lines`（`d4e92d8` 刚 ratchet 到实测值，余量 0）；主规格新增 28 行后实测 `22220`，`check_doc_budget.py` 报 `slack.lines: -28` 并 exit 1，`scripts/lint.sh` 在该门中止（`set -e`）。复跑：`python3 scripts/check_doc_budget.py --json`。

处置选项（择一，需授权 `scripts/doc_budget.json` 或等量的长期文档压缩面）：
1. **具名落基线**（建议）：`baseline.lines` 22192→22220，并在 `rebaseline_reason` 追加一条"04.1 主规格 ADDED「循环面的模型感知边界」+28 行，属批准的契约交付面"，与既往 ㉕ 的记账方式一致；
2. 同批压掉 28 行长期文档（白名单外，且本单无此授权面）；
3. 4.1 不落主规格、只留 delta（与 4.1 的正锚和 F1 的"主规格变更仅限 delta"冲突，不建议）。

`scripts/doc_budget.json` 不在派发单白名单，故本单未改；F2「全量门禁」前必须落定，否则 lint 无法绿。

#### §5.2 P-09：v1 金样 S03/S11 在 HEAD 即漂移（既有，非本单引入）

`python3 scripts/run_runtime_scenarios.py --contract golden/runtime/scenarios.json --scope deterministic --out …` 在本工作区 exit=3，drift 为 S03/S11（S15 已 attested，0 diff）；在 `git clone --local` 的 HEAD `9cc4b20` 克隆里跑同样 exit=3，drift 为 S03/S11/S15 —— 即 S03/S11 是 01.x 落地后既有的金样过期（receipt 不再携带 task id 一类），S15 由红转绿。本单白名单只许动 S15，S03/S11 未修；须在 F2 前由编排者裁定是否补一单改写其过期文案。

### §6 证据路径

`/tmp/event-semantics-boundary/04.1/{spec-strict.json,delta-strict.json}`、`/tmp/event-semantics-boundary/04.2/{progress.json,s15.json,contract-v1.json,contract-v2.json,backflow.log,backflow-v.log}`、`/tmp/event-semantics-boundary/04.3/{symptom.json,workflow.log,evidence.log}`、`/tmp/event-semantics-boundary/04.lint.log`、`/tmp/event-semantics-boundary/04.policy.log`；回滚克隆 `/tmp/tagent-pre-injection-49fd79f`。
