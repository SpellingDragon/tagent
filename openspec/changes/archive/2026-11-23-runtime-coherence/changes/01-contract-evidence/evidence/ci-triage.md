# CI 红项分诊证据（01.101 · W1-E 只读分诊单）

本文件只记 CI run `45004855439`（候选 `52d7b664`）非计时红项的本地复跑、退出码与归因证据。计时项（`TestBootWallClockRejectsUnattributedGrowth`、`TestSymptomAnchorPerGenerationCostFlatOverALongRun`）不属本单，未跑。未改任何代码/测试/CI；未 commit/push；未触碰外来未跟踪面与 `examples/wechat-bot/.wechat-config/data`、`archive/**`。临时 worktree（干净基线复验用）已 `git worktree remove` 清理。

## 基准

- 仓库：`/Users/pengweiye/Documents/codes/tagent`，分支 `dev`，开工 `git rev-parse HEAD` = `52d7b664521554fed8d7a5674ee1f934c410a3e3`（与 CI 候选一致）。
- 本地 `go version` = `go1.27.1 darwin/arm64`；CI（`.github/workflows/ci.yml`）= `go-version: '1.24'`（ubuntu-latest，setup-go 解析到的具体小版本本地不可见），`runtime-contracts.yml` = `">=1.24.1"`。本地 1.27.1 对 CI 1.24.x 为**跨小版本差异**，仅对 shim 以下结论的影响如实标注：四项红/绿判定无一依赖新版本语言/标准库特性，版本差异不构成任何一项归因的充分解释（第 1 项本地稳定红在两个基线提交上均与 go 版本无关地复现/通过）。
- 每条 rc 取命令自身（`> out 2>&1; rc=$?` 独立语句），无管道吞码。日志留存 `/tmp/w1e_*.log`。

## 1. retry 三项 · 产品缺陷（`f13509f9` 引入回归）

- 测试：`TestRetryLeavesPresentedRangeAtTheLatestRound`（红的是 `/folded` 子测；`refuses=false/true` 两子测 PASS）、`TestRetryOfFoldedOutputDoesNotReopenTheRound`、`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`
- 包：`github.com/SpellingDragon/tagent/assembly`（`assembly/checkpoint_test.go`，三项共享夹具 `retryFoldedOutput` + `retryFoldedOutputLeavesTheRangeAlone`）
- 命令与 rc：

```
$ go test ./assembly -run 'TestRetryLeavesPresentedRangeAtTheLatestRound|TestRetryOfFoldedOutputDoesNotReopenTheRound|TestRestartRetryOfFoldedOutputKeepsThePresentedRange' -count=1
rc=1   # /tmp/w1e_retry_c1.log：4 条 --- FAIL（父+folded、其余两项）
$ go test ./assembly -run '…同上…' -count=5
rc=1   # /tmp/w1e_retry_c5.log：20 条 --- FAIL（4×5 全轮全红），0 处 PASS 汇总
```

- 首条失败原文（`-count=1` 首个红项，消息先于 `--- FAIL` 行输出）：

```
2026-11-22T17:37:46+08:00	ERROR	assembly/facts.go:199	tagent: the committed fact retry-presented-0-3 was not folded into the state of its consumers: state: fold the fact retry-presented-0-3 into the "retry-presented" state slice: the consumer refuses to apply the fact retry-presented-0-3
    checkpoint_test.go:3288: the retry of the folded output agent-a120300 moved the completed range from (agent-a122300, open=false) to (agent-a120300, open=false)
--- FAIL: TestRetryLeavesPresentedRangeAtTheLatestRound (0.01s)
    --- FAIL: TestRetryLeavesPresentedRangeAtTheLatestRound/folded (0.00s)
    --- PASS: TestRetryLeavesPresentedRangeAtTheLatestRound/refuses=false (0.00s)
    --- PASS: TestRetryLeavesPresentedRangeAtTheLatestRound/refuses=true (0.00s)
```

  另两项同型：`checkpoint_test.go:3338`（同一句 "moved the completed range from (agent-a122300…) to (agent-a120300…)"）、`checkpoint_test.go:3363`（重启后基线为空 `(, open=false)` 仍被 retry 推进）。
- 红项归属甄别（干净基线，`git worktree add --detach /tmp/...`，跑后已移除）：

```
7b0cde16（三项引入提交）: 同 -run 同 -count=1 → rc=0, ok assembly 0.537s
f13509f9（其后一笔）      : 同命令 → rc=1, 4 条 --- FAIL（与 HEAD 同型）
```

  `f13509f9` 改产品面 `assembly/checkpoint.go`(±162)、`assembly/facts.go`(±146)、`assembly/run.go`(±28)；其 `facts.go` diff 自述新语义："FactAlreadyPresent: … a repeat of a fact this process registered **folds again** and files no second position"。三项测试（`7b0cde16` 钉住"重试已折叠输出不得移动已完成范围/不得二次陈述序列"）恰好钉在该路径的相反判断上。
- 归因：**产品缺陷**——`f13509f9` 的重复投递折叠语义变化打破既有契约测试，本地稳定红（非环境、非夹具自身错；该提交声称"完整矩阵绿"与本复验矛盾，其矩阵当时未覆盖或未如实跑）。最终裁"改产品回到契约"还是"语义有意变更、测试面未同步"归阶段 0；两项基线证据如上。

## 2. TestScenario03ToolReceiptAndBackground · 环境或负载（CI 特有）

- 包：`github.com/SpellingDragon/tagent/tests/integration/acceptance`（`scenario_s03_test.go`）
- 命令与 rc：

```
$ go test ./tests/integration/acceptance -run 'TestScenario03ToolReceiptAndBackground' -count=1 -v
rc=0   # --- PASS (0.00s)；观测记录 TAGENT-SCENARIO-RECORD 打印 receipt_before_terminal=true、receipts_per_call=1
$ go test ./tests/integration/acceptance -run 'TestScenario03ToolReceiptAndBackground' -count=5
rc=0   # ok 0.339s，0 处 FAIL
```

- 归因：**环境或负载**——本地 `count=1`、`count=5` 均绿，无法复现 CI 红；无本地失败原文可录。CI 侧原始失败日志未随派单提供，本单未取（如需，可对 run `45004855439` 的 `test`/`race` job 取 `--log-failed`）。

## 3. TestNativeConversationRoundTrip · 本地间歇复现（非 CI 特有；竞态待裁）

- 包：`github.com/SpellingDragon/tagent/tests/integration/execution`（`native_conversation_roundtrip_test.go`）
- 命令与 rc：

```
$ go test ./tests/integration/execution -run 'TestNativeConversationRoundTrip' -count=1 -v
rc=0   # 3 子测全 PASS
$ go test ./tests/integration/execution -run 'TestNativeConversationRoundTrip' -count=5
rc=1   # 首轮即红（/tmp/w1e_ncrt_c5.log）
$ 独立进程复跑 10 次（-run 'TestNativeConversationRoundTrip/a_native_multi-turn' -count=1 ×10）
rc: 0,0,1,0,1,1,0,0,0,0 → fails=3/10
```

- 首条失败原文（三次失败同断言 `native_conversation_roundtrip_test.go:93`，call-1/call-2 交替缺席；`provider refused` 是另一子测故意注入，与本失败无关）：

```
    native_conversation_roundtrip_test.go:93: settled observation(s) of [call-2] never reached a model request in 10s (#0 [user/:hello]; #1 [user/:hello | assistant/: | tool/call-1:ok | tool/call-2:ok | user/call-1:out-a]; #2 [user/:[evt_…] hell… | … | tool/call-1:[evt_settle…] | tool/call-2:[evt_settle…] | assistant/:[evt_5…] settle…]), want each result body read under its call identity
```

  run6 形态更极端：view #2 中 call-1 的 action_command/settle 材料整体缺席，turn 仍 completed——并发双工具 settle 后，观测进入"下一模型请求"的时机存在竞态（turn 已终态，`awaitRoundTripResults` 10s 等待必扑空）。
- 归因：**本地绿/间歇（3/10）**——既非本地稳定红（不满足产品缺陷判据），也非"本地全绿、CI 特有"（本地可复现）。定性：settle 观测送达下一决策的时序竞态，产品侧（观测入列时机）或夹具侧（等待/终止判定）待阶段 0 裁决；非 go 版本差异所致（同一本地版本内自现）。

## 4. event-integrity-short shard 6 · 环境或负载（CI 特有）

- 命令与 rc（与 CI 步骤同形，`ci.yml:274` `--repeat-index 6`；合同 digest `sha256:e1dfb96b…3f8b3f` 与 CI 一致）：

```
$ python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat-index 6 --out /tmp/w1e-short6
rc=0   # verdict: match；repeat-6/10 run_id=cc736993-… exit=0；evidence:/tmp/w1e-short6/repeat-6/evidence.json
        # 该轮实际为完整 scope 一次调用：go test <7 包> -run '^(20 用例)$' -count=1 -timeout 900s -race -json, exit_code=0
```

- CI 侧失败原文（`gh run view 45004855439 --log-failed`，只读取回）：

```
verdict: unattested
repeat-6/10 run_id=261d512e-a607-4f79-b7a1-57974c35f5df verdict=unattested exit=1
  01.3/TestEventIntegrityTaskContinuation    unattested  github.com/SpellingDragon/tagent::tests/integration/host::TestEventIntegrityTaskContinuation recorded fail
##[error]Process completed with exit code 1.
（artifact event-integrity-short-6/repeat-6/test-log.jsonl 首条红）
--- FAIL: TestEventIntegrityTaskContinuation/notice_input_without_projection/no_declared_window (0.08s)
    event_integrity_test.go:5403: user turn "delegate the projectionless continuation task" = (completed, 3 decisions), want the completed turn of 2 decisions
```

- 本地定向复跑 CI 红用例：

```
$ go test ./tests/integration/host -run '^TestEventIntegrityTaskContinuation$' -count=1 -race -v
rc=0   # --- PASS (4.70s)，含 -race
```

- 归因：**环境或负载**——同一分片命令本地 `rc=0`（verdict=match，含 `-race`），CI 红源为 `tests/integration/host` 的 `TestEventIntegrityTaskContinuation/no_declared_window`（CI 得 3 decisions、期望 2；本地含 race 绿）。多出一轮决策在 CI 负载下的时序敏感性是候选解释，未在本地复现，不写为事实。

## 附：CI run 45004855439 其余已知红（不在本单范围）

`TestBootWallClockRejectsUnattributedGrowth`、`TestSymptomAnchorPerGenerationCostFlatOverALongRun`（计时项，收口后独占串行重取）；event-integrity-required；archive-preflight evidence-binding exit 1 / spec-hygiene exit 2（`openspec CLI not found`，CI 接线问题）。本单未触碰。
