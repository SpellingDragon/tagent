# CI 吞吐真 CI 证据（04.403）

取证仓库：`SpellingDragon/tagent`。以下读数来自 `gh run list/view`，不使用 ORCH 的近似墙钟替代原始 JSON。

## 读数口径

- run 墙钟＝该 attempt 的 `updatedAt - startedAt`；job 墙钟＝`completedAt - startedAt`。
- `jobs` 数含 skipped；rerun 行的“本轮执行”只计该 attempt 实际重跑的 job，继承自上一 attempt 的 job 不计入最慢 job。
- 原始 jobs JSON 与日志保存在 `/tmp/tagent-bounded-hot-log-04.403/`，不入库。
- 基线 `bde7f394` 的 workflow 无 `cache:`，且只有一个 `race` job；以下墙钟按原始时间重算，不沿用 proposal 的近似值。

## Run 分解

| SHA | run_id / attempt | workflow | event | status / conclusion | jobs 总（skipped；本轮执行） | 关键路径最慢 job / 墙钟 | run 总墙钟 |
|---|---|---|---|---|---|---|---|
| `bde7f3940f0afa16e7b98cf639b9839088ad1f8e` | [45051706184](https://github.com/SpellingDragon/tagent/actions/runs/45051706184) / 1 | CI | push | completed / failure | 25（1；24） | `race` 12:47（767s） | 21:47（1307s） |
| `bde7f3940f0afa16e7b98cf639b9839088ad1f8e` | [45051711035](https://github.com/SpellingDragon/tagent/actions/runs/45051711035) / 1 | CI | pull_request | completed / failure | 25（1；24） | `race` 14:57（897s） | 25:39（1539s） |
| `2521a5134cc1f10b9fe0a6e7776923f41ff83138` | [45225552144](https://github.com/SpellingDragon/tagent/actions/runs/45225552144) / 1 | CI | push | completed / success | 25（1；24） | `race` 13:51（831s） | 23:27（1407s） |
| `2521a5134cc1f10b9fe0a6e7776923f41ff83138` | [45225552346](https://github.com/SpellingDragon/tagent/actions/runs/45225552346) / 1 | runtime-contracts | push | completed / success | 2（1；1） | 无 `race`；`format-and-contract` 1:12（72s） | 1:33（93s） |
| `06fae63fa08403c3b94339611ba0f372859a90a4` | [45227354132](https://github.com/SpellingDragon/tagent/actions/runs/45227354132) / 1 | CI | push | completed / failure | 25（1；24） | `race` 15:21（921s） | 21:58（1318s） |
| `06fae63fa08403c3b94339611ba0f372859a90a4` | [45227354132](https://github.com/SpellingDragon/tagent/actions/runs/45227354132) / 2 | CI | push | completed / failure | 25（1；1） | `test` 4:39（279s） | 4:54（294s） |
| `06fae63fa08403c3b94339611ba0f372859a90a4` | [45227354144](https://github.com/SpellingDragon/tagent/actions/runs/45227354144) / 1 | runtime-contracts | push | completed / success | 2（1；1） | 无 `race`；`format-and-contract` 0:50（50s） | 0:54（54s） |
| `60d6ca64e73068c3eefd21fa3177e108da7ee683` | [45319647171](https://github.com/SpellingDragon/tagent/actions/runs/45319647171) / 1 | CI | push | completed / failure | 25（1；24） | `race` 19:28（1168s） | 21:43（1303s） |
| `e0981aa11c4efa524b7ca4df14a80061f0f19035` | [45322587406](https://github.com/SpellingDragon/tagent/actions/runs/45322587406) / 1 | CI | push | completed / success | 25（1；24） | `race` 14:24（864s） | 20:17（1217s） |
| `e0981aa11c4efa524b7ca4df14a80061f0f19035` | [45322587418](https://github.com/SpellingDragon/tagent/actions/runs/45322587418) / 1 | runtime-contracts | push | completed / success | 2（1；1） | 无 `race`；`format-and-contract` 1:13（73s） | 1:17（77s） |
| `2f9594afe0a055f1a6dfdc43b02458c3b0735c26` | [45342689398](https://github.com/SpellingDragon/tagent/actions/runs/45342689398) / 1 | CI | push | completed / failure | 25（1；24） | `race` 13:43（823s） | 21:14（1274s） |
| `2f9594afe0a055f1a6dfdc43b02458c3b0735c26` | [45342689398](https://github.com/SpellingDragon/tagent/actions/runs/45342689398) / 2 | CI | push | completed / success | 25（1；5） | `race` 15:12（912s） | 15:17（917s） |
| `977f495dd7f5fdbc318bba4d40c20595cba88cce` | [45386819170](https://github.com/SpellingDragon/tagent/actions/runs/45386819170/attempts/1) / 1 | CI | push | completed / success | 28（1；27） | `race (1)` 10:21（621s）；含排队 10:24（624s） | 20:14（1214s） |

台账核对：

- ORCH 的 `06fae63f`“一红一绿”两条均为 `push`：红的是 CI `45227354132`，绿的是 runtime-contracts `45227354144`，不是同一 workflow 的重跑转绿。
- ORCH 的 `e0981aa1` run `45322587418` 是 runtime-contracts；同 SHA 的 CI 是 `45322587406`，consumer 隔离证据取后者。
- `2521a513` 的“热轮”`45225552346` 也是 runtime-contracts，不是完整 CI 热轮。

## 04.403 完整分片 CI（`977f495d`）

原始 JSON 归属：run [`45386819170`](https://github.com/SpellingDragon/tagent/actions/runs/45386819170/attempts/1)，workflow `CI`，attempt `1`，run number `586`，event `push`，branch `dev`，SHA `977f495dd7f5fdbc318bba4d40c20595cba88cce`，`completed / success`；`startedAt=2026-11-24T11:43:41Z`，`updatedAt=2026-11-24T12:03:55Z`。28 个 job 中 `soak` skipped，本轮实际执行 27 个。

排队＝`job.startedAt - run.startedAt`；job 墙钟＝`completedAt - startedAt`。

| job | databaseId | 排队 | job 墙钟 | conclusion |
|---|---:|---:|---:|---|
| `race (1)` | `138419779508` | 3s | 10:21（621s） | success |
| `race (2)` | `138419779284` | 3s | 6:21（381s） | success |
| `race (3)` | `138419779313` | 3s | 1:44（104s） | success |
| `race-aggregate` | `138425333871` | 10:26（626s） | 2:13（133s） | success |
| consumer（job 名为 `test`） | `138423334949` | 6:42（402s） | 9:09（549s） | success |
| 独立 `wechat` job | — | — | — | 不存在；不能记作 skipped |

wechat 在该 workflow 中不是独立 job，而是 `test` 与 `race-aggregate` 内的 step；consumer 也有独立 step。以下 step 墙钟取原始 jobs JSON，`go test` 读数取日志原文。

| 所属 job | step | step 墙钟 | `go test` 报告 | conclusion |
|---|---|---:|---:|---|
| `test` | `Test (response-call cap fuse consumer path)` | 2s | 0.491s | success |
| `test` | `Test (bot module, short)` | 3s | 1.174s | success |
| `race-aggregate` | `Race (response-call cap fuse consumer path)` | 84s | 2.033s | success |
| `race-aggregate` | `Race (wechat-bot module)` | 26s | 2.716s | success |

关键路径按 04.403 的 race 口径计算，不取全 run 最晚完成的归档 job：

- 最慢 race shard＋排队＝`race (1)` 621s＋3s＝**624s（10:24）**。
- 对基线 push `45051706184` 的单 `race` job 767s（12:47）：624/767＝**81.36%**；60% 门槛为 `<460.2s`，**未达**。
- 对基线 pull_request `45051711035` 的单 `race` job 897s（14:57）：624/897＝**69.57%**；60% 门槛为 `<538.2s`，**未达**。
- 含 `race-aggregate` 的 race 链完成偏移为 626s＋133s＝**759s（12:39）**，对两条基线分别为 98.96%/84.62%，同样未达；该数不替代上面的 shard 关键路径口径。
- run 墙钟 1214s（20:14）对基线 1307s（21:47）减少 93s（**7.12%**），对 1539s（25:39）减少 325s（**21.12%**）。run 墙钟下降达成，关键路径未达，故 **04.403 整体 pending/未满足**。

这是跨 SHA 的真 CI 观测；基线两条均为 failure，新 run 为 success。墙钟差包含代码、缓存、runner 与排队变化，不把全部差值归因于 race 分片。

关键原始行：

```text
race-aggregate	Check race shard results	2026-11-24T11:54:12.1190162Z   RACE_RESULT: success
race-aggregate	Check race shard package union	2026-11-24T11:54:27.0623884Z race_check: shard artifact union OK (24 packages across 3 shards)
test	Test (response-call cap fuse consumer path)	2026-11-24T11:55:00.8013919Z   TMPDIR: /dev/shm
test	Test (response-call cap fuse consumer path)	2026-11-24T11:55:02.6026492Z ok  	github.com/SpellingDragon/tagent/tests/integration/execution	0.491s
race-aggregate	Race (response-call cap fuse consumer path)	2026-11-24T11:54:27.0697844Z   TMPDIR: /dev/shm
race-aggregate	Race (response-call cap fuse consumer path)	2026-11-24T11:55:51.5028318Z ok  	github.com/SpellingDragon/tagent/tests/integration/execution	2.033s
race-aggregate	Race (wechat-bot module)	2026-11-24T11:56:16.4560009Z ok  	wechat-bot	2.716s
```

## 缓存原始日志行

冷态 miss→save：run `45225552144`，job `event-semantics-symptoms`（`137870945942`）。miss 行本身不打印 key；同 job 的 save 行给出 key。

```text
event-semantics-symptoms	Run actions/setup-go@v5	2026-11-23T17:15:09.6355921Z Cache is not found
event-semantics-symptoms	Post Run actions/setup-go@v5	2026-11-23T17:16:32.4707130Z Cache saved with the key: setup-go-Linux-x64-ubuntu26-go-1.24.13-6f1572d13161ea818b7b54d4f5bc4ea7767dd5e311da6a8705a0dfdfe2380550
```

热态 exact hit：同一 `CI` workflow 的 run `45228095983`，job `race`，使用与冷态 save 逐字相同的 key。

```text
race	Run actions/setup-go@v5	2026-11-23T17:28:58.9212028Z Cache hit for: setup-go-Linux-x64-ubuntu26-go-1.24.13-6f1572d13161ea818b7b54d4f5bc4ea7767dd5e311da6a8705a0dfdfe2380550
race	Run actions/setup-go@v5	2026-11-23T17:29:01.5611706Z Cache restored successfully
race	Run actions/setup-go@v5	2026-11-23T17:29:01.5689378Z Cache restored from key: setup-go-Linux-x64-ubuntu26-go-1.24.13-6f1572d13161ea818b7b54d4f5bc4ea7767dd5e311da6a8705a0dfdfe2380550
```

两轮均属 `CI` workflow、Go 1.24.13、同一 cache key，且两轮 conclusion 均为 success。缓存键只随三模块 `go.sum` 与 toolchain，不随产品 `.go` 内容变化，因此构成同工作流、同键的冷→热对照；早先引用的 `45225552346` 属 runtime-contracts 且 Go 1.27.4，保留为历史旁证，不作本判据。

## consumer 365s 隔离前后

| 路径 | 隔离前 | 先行隔离（`e0981aa1` CI `45322587406`） | tmpfs（`977f495d` CI `45386819170`） |
|---|---|---|---|
| short `test` | `06fae63f` attempt-1，job `137881928696`：consumer 子测 365.02s 红 | job `138206833188`：主命令精确 `-skip`；独立 step 同子测 1.270s 绿 | job `138423334949` success；consumer step 2s，`TMPDIR=/dev/shm`，同子测 0.491s 绿 |
| `race` | `60d6ca64`，job `138194825614`：consumer 子测 365.03s 红 | job `138204417685`：主 race 用 `GOFLAGS` 精确 skip；独立 `-race -p 1` step 同子测 2.179s 绿 | `race-aggregate` job `138425333871` success；consumer race step 84s，`TMPDIR=/dev/shm`，同子测 2.033s 绿 |

关键原始行：

```text
test	Test (short)	2026-11-23T17:40:07.7169735Z     --- FAIL: TestResponseCallCapFuse/consumer_path_holds_the_batch_for_approval (365.02s)
test	Test (short)	2026-11-23T17:40:07.7170360Z         response_call_cap_fuse_test.go:314: no request of the host entry carried a tool receipt within 1m0s, so the storm round never reached a second decision
race	Race (every first-party package + all integration suites)	2026-11-24T03:47:02.2000962Z     --- FAIL: TestResponseCallCapFuse/consumer_path_holds_the_batch_for_approval (365.03s)
race	Race (every first-party package + all integration suites)	2026-11-24T03:47:02.2001830Z         response_call_cap_fuse_test.go:314: no request of the host entry carried a tool receipt within 1m0s, so the storm round never reached a second decision
test	UNKNOWN STEP	2026-11-24T04:05:04.5889389Z ok  	github.com/SpellingDragon/tagent/tests/integration/execution	1.270s
race	UNKNOWN STEP	2026-11-24T04:07:27.8705824Z ok  	github.com/SpellingDragon/tagent/tests/integration/execution	2.179s
```

`race` job 总墙钟观测为 19:28→14:24，但其中包含整个 race suite 与共享 runner 波动，不能把全部差值归因于 consumer 隔离，也不能据此宣称稳定性。

## restart / race flake 观测

- `06fae63f` 同一 test job 两次红面漂移：attempt-1 是 consumer 365.02s；attempt-2 改为 `TestEventIntegrityRestart/consumer`，`after_commit` restart 驱动 0 turn。
- `2f9594af` attempt-1 的 `TestToolRoundLeavesADebugTrail` 在 race job 内 0.00s 红，缺 `consumer segment close`；attempt-2 同 SHA race job 转绿。
- `2f9594af` attempt-1 的 event-short 2/8 均记录 `TestEventIntegrityRestart` `unattested`；attempt-2 两条同分片均 `verdict=match exit=0`。
- `e0981aa1` 未复现 restart 红不等于稳定；`2f9594af` 又复现。单次重跑转绿只记录观测，不解除稳定性债，也不单独证明根因。

```text
test	Test (short)	2026-11-23T17:53:48.6112432Z --- FAIL: TestEventIntegrityRestart (2.65s)
test	Test (short)	2026-11-23T17:53:48.6112880Z     --- FAIL: TestEventIntegrityRestart/consumer (0.00s)
test	Test (short)	2026-11-23T17:53:48.6114039Z         event_integrity_test.go:2797: the after_commit restart drove 0 turn(s) with "what do you still owe?" out of 1 request(s), want its first actual request carrying the restart input
race	Race (every first-party package + all integration suites)	2026-11-24T06:58:10.1822957Z --- FAIL: TestToolRoundLeavesADebugTrail (0.00s)
race	Race (every first-party package + all integration suites)	2026-11-24T06:58:10.1823491Z     tool_observability_test.go:99: the debug trail lacks "tagent: consumer segment close invocation=session-observability#";
event-integrity-short (2)	Event integrity short (one independent round)	2026-11-24T06:47:58.4967789Z   03.9/TestEventIntegrityRestart             unattested  github.com/SpellingDragon/tagent::tests/integration/history::TestEventIntegrityRestart recorded fail
event-integrity-short (8)	Event integrity short (one independent round)	2026-11-24T06:47:56.6198572Z   03.9/TestEventIntegrityRestart             unattested  github.com/SpellingDragon/tagent::tests/integration/history::TestEventIntegrityRestart recorded fail
race	UNKNOWN STEP	2026-11-24T07:26:05.2269213Z race_check: OK (go test passed)
event-integrity-short (2)	UNKNOWN STEP	2026-11-24T07:15:04.9187632Z repeat-2/10 run_id=91f586f0-ae2d-434d-9bc0-3b07df568cdb verdict=match exit=0 log=/home/runner/work/_temp/event-short/repeat-2/test-log.jsonl evidence=/home/runner/work/_temp/event-short/repeat-2/evidence.json
event-integrity-short (8)	UNKNOWN STEP	2026-11-24T07:15:04.9838538Z repeat-8/10 run_id=ab9eb642-e061-47bb-9d1a-e7f328e4ee0e verdict=match exit=0 log=/home/runner/work/_temp/event-short/repeat-8/test-log.jsonl evidence=/home/runner/work/_temp/event-short/repeat-8/evidence.json
```

- `977f495d` 完整 CI `45386819170` 为 success，本 run **未复现 restart 红**；event-short 2/8 的 `test_ids` 原文均含 `03.9/TestEventIntegrityRestart`，两条均为 `verdict=match exit=0`，所取 consumer/race 日志也无 restart 失败签名。单轮未复现只记观测，不解除稳定性债。

```text
event-integrity-short (2)	Event integrity short (one independent round)	2026-11-24T11:47:18.8870783Z repeat-2/10 run_id=4e1b55fe-faa6-4af8-8f26-d8f02fac57c2 verdict=match exit=0 log=/home/runner/work/_temp/event-short/repeat-2/test-log.jsonl evidence=/home/runner/work/_temp/event-short/repeat-2/evidence.json
event-integrity-short (8)	Event integrity short (one independent round)	2026-11-24T11:47:12.7215074Z repeat-8/10 run_id=582d1d88-3b1b-4c75-a7c2-0dddb570e110 verdict=match exit=0 log=/home/runner/work/_temp/event-short/repeat-8/test-log.jsonl evidence=/home/runner/work/_temp/event-short/repeat-8/evidence.json
```

## 04.402/04.403 状态

- `04.402` 已在 tasks 勾选。`2f9594af` 的历史 workflow 只有一个 `race` job、无 `race-aggregate`；`977f495d` 的 `45386819170` 已观测到 3 个 race shard、24 包并集校验通过和 `race-aggregate` success。该 run 提供真实聚合消费者锚，但不替代本地负锚或 fail-closed 负向证据。
- `04.403` **已完成**：最终冻结树 `fcee2238` 的完整 CI `45625674535` 全绿，统一关键路径 385s，对基线 767s/897s 为 50.20%/42.92%，run 墙钟 1263s 对基线 1307s/1539s 下降 3.37%/17.94%；历史 `977f495d` 的 759s 仅作未达标前记录。
- 完整 run 墙钟下降已达成：20:14 对 21:47/25:39 分别下降 7.12%/21.12%。`2f9594af` attempt-2 的 15:17 仍只是失败项重跑墙钟，不是完整 25-job run，不能用于宣布完整 run 下降。
- 缓存与 consumer 隔离证据不能替代 race 分片、并集保真或 fail-closed 聚合证据；关键路径未达时不得以 run 下降或单轮全绿宣布 04.403 完成。

## 复核命令

```bash
REPO=SpellingDragon/tagent
OUT=/tmp/tagent-bounded-hot-log-04.403
mkdir -p "$OUT"

gh run list -R "$REPO" -L 100 \
  --json databaseId,headSha,event,status,conclusion,workflowName,attempt,startedAt,updatedAt,url

for item in \
  45051706184:1 45051711035:1 \
  45225552144:1 45225552346:1 \
  45227354132:1 45227354132:2 45227354144:1 \
  45319647171:1 45322587406:1 45322587418:1 \
  45342689398:1 45342689398:2 45386819170:1
do
  id=${item%:*}; attempt=${item#*:}
  gh run view "$id" -R "$REPO" --attempt "$attempt" \
    --json databaseId,number,headSha,event,status,conclusion,jobs,startedAt,updatedAt,workflowName,url,headBranch,displayTitle \
    > "$OUT/$id-a$attempt.json"
done

gh run view 45225552144 -R "$REPO" --attempt 1 --log --job 137870945942
gh run view 45225552346 -R "$REPO" --attempt 1 --log --job 137870946585
gh run view 45227354132 -R "$REPO" --attempt 1 --log --job 137881928696
gh run view 45227354132 -R "$REPO" --attempt 2 --log --job 137892681799
gh run view 45319647171 -R "$REPO" --attempt 1 --log --job 138194825614
gh run view 45322587406 -R "$REPO" --attempt 1 --log --job 138206833188
gh run view 45322587406 -R "$REPO" --attempt 1 --log --job 138204417685
gh run view 45342689398 -R "$REPO" --attempt 1 --log-failed
gh run view 45342689398 -R "$REPO" --attempt 2 --log --job 138280842468
gh run view 45386819170 -R "$REPO" --attempt 1 --log --job 138423334949
gh run view 45386819170 -R "$REPO" --attempt 1 --log --job 138425333871
gh run view 45386819170 -R "$REPO" --attempt 1 --log --job 138419778572
gh run view 45386819170 -R "$REPO" --attempt 1 --log --job 138419778502

git show bde7f3940f0afa16e7b98cf639b9839088ad1f8e:.github/workflows/ci.yml
git show 2f9594afe0a055f1a6dfdc43b02458c3b0735c26:.github/workflows/ci.yml
git show 2f9594afe0a055f1a6dfdc43b02458c3b0735c26:scripts/gates/race_check.sh
git show 977f495dd7f5fdbc318bba4d40c20595cba88cce:.github/workflows/ci.yml
```

## 第三轮：assembly 测试拆分

run `45406503959`（SHA `314de795`）的 race job 全绿：`race (1)` 251s、`race (2)` 384s、`race (3)` 409s、`race-aggregate` 17s。关键路径约 426s，对基线 767s/897s 为 55.9%/47.8%，达到 `<60%`。该 run 唯一红是 test job 的旧 wechat 负锚，已在 `327a11c4` 修复；完整 CI `45446101670`（SHA `5383546b`）随后全绿，race 分片 370/371/348s、aggregate 18s，从 run 开始到 aggregate 结束约 408s，仍低于基线 60%。

assembly 336 个顶层测试按首字母奇偶拆为 143/193 两个互补选择器，包并集按包去重并要求 odd/even selector 成对；wechat 独立 race step 移入 shard-3，aggregate 只做结果、manifest 下载与并集校验。

## consumer storm：原子接受与认领

失败日志为 `state: the mailbox entry is already claimed`。`bookNotice` 原先分两次调用 `AcceptNotice` 与 `Claim`，并发 flush 的普通 acceptance 与 resident consumer 可在两步间抢走席位；`AcceptNoticeClaimed` 现在在同一临界区完成 acceptance 与 claim，返回实际 `WorkSeat`，消费和释放均使用该站点与序列。重放只认领原站点仍 pending 的条目，已认领或已消费返回 `ErrClaimConflict`，不再继续重复 handover。

本地 `TestAcceptNoticeClaimed*` 在 `-race -count=10` 下通过；consumer 定向路径连续三轮通过（17.551s）。完整 CI `45446101670`（`5383546b`）全绿，365s consumer 红未复现；F0.1/F0.2/F0.5 后续改动仍须在最终冻结树再取一次完整 CI。

## 关键路径口径勘误与后续候选

统一口径为 `race-aggregate.completedAt - run.startedAt`，包含分片排队、分片执行与聚合等待；不再用最慢分片完成偏移或剔除聚合的读数宣布达标。

| SHA / run | conclusion | race shard 墙钟 | race gate 关键路径 | 对 push 767s | 对 PR 897s | run 墙钟 |
|---|---|---|---:|---:|---:|---:|
| `314de795` / `45406503959` | failure（旧 wechat 负锚） | 251/384/409s；aggregate 17s | 433s | 56.45% | 48.27% | 1147s |
| `5383546b` / `45446101670` | success | 370/371/348s；aggregate 18s | 466s | 60.63% | 51.95% | 1453s |
| `a6e953ee` / `45596219143` | failure | 313/354/393s；aggregate 8s | 未完成有效放行 | — | — | 1258s |
| `9f86cfef` / `45616240415` | success | 301/426/373s；aggregate 29s | 462s | 60.23% | 51.51% | 1287s |
| `59d918f1` / `45620610167` | success | 283/436/408s；aggregate 16s | 457s | 59.58% | 50.95% | 1318s |
| `fcee2238` / `45625674535` | success | 293/360/355s；aggregate 18s | 385s | 50.20% | 42.92% | 1263s |

- `45406503959` 的 race 门达到 `<60%`，但整轮因旧 wechat 负锚失败，不能作为完整 CI 放行。
- `45446101670` 的 408s 是 `race (3)` 相对 run 开始的完成偏移，不是聚合门关键路径；统一口径为 466s，对 push 基线为 60.63%，对 PR 基线为 51.95%。run 墙钟 1453s 对 push 基线 1307s 反而上升，故 `04.403` 仍未满足。
- `45596219143` 的 `race (3)` 与 `test` 同红于 `TestExportReadsArchivedFactsByIdentity`：夹具 `HotLogMaxBytes=1` 已低于结构下限，只告警、不请求 cut。夹具已改为合法 128KiB 预算、3×64KiB 正文与真实 `Run.Checkpoint`，定向测试 ×10 通过；该旧 run 不作为吞吐达标证据。
- `9f86cfef` 完整 CI 全绿，run 墙钟 1287s 对 push 基线 1307s 下降 20s（1.53%）、对 PR 基线 1539s 下降 252s（16.37%）；但 race 门 462s 对 push 基线为 60.23%，仍高出门槛 1.8s，故 `04.403` 保持 pending。
- 第二轮再平衡：把 `memory/...` 从 shard-2 移回 shard-1，包并集与测试选择不变，`test_race_check.sh` 全部通过，`--shard-union-check` 精确并集为空 diff。`59d918f1` 的 race 门 457s 达到 `<60%`，但 shard-2 实为 436s，未观察到 memory 移回带来的确定性降幅；不把单轮差异写成因果结论。
- `fcee2238` 完整 CI 全绿：race 门 385s，对 push/PR 基线 767s/897s 为 50.20%/42.92%，均低于 60%；run 墙钟 1263s，对 push 基线 1307s 下降 44s（3.37%）、对 PR 基线 1539s 下降 276s（17.94%）。archive-preflight 与 event-integrity-required 同于 13:59:21Z 启动、分别于 13:59:36Z/13:59:32Z 完成，串行等待已消除；workflow 正负锚 76 项通过。`04.403` 两项吞吐判据均达成。
