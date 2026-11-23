# 症状锚证据（01.101 · W1-A 锚点单）

本文件只记两个症状锚的执行证据与判定，不作六疑点取证（W1-B/W1-C 各有单）。

## 基准与写面

- 仓库：`/Users/pengweiye/Documents/codes/tagent`，分支 `dev`，开工复核 `git rev-parse HEAD` = `52d7b664521554fed8d7a5674ee1f934c410a3e3`（与派单候选一致）。
- 新增文件（本单全部写面）：
  - `assembly/symptom_anchor_coherence_test.go`
  - `runtime/task/approval/symptom_anchor_ruling_test.go`
  - 本文件
- 未改任何生产 `.go`；未 commit/push；未触碰冻结归档与外来未跟踪面。
- 每条的退出码取命令自身（`> out 2>&1; echo rc=$?`），不经管道、不串 `||`。

## S1 · 宿主入口不承接插件贡献

- 锚：`TestSymptomAnchorHostBootMountsContributedPlugin`
- 夹具：`t.TempDir()` 临时根＋`generationDocument` 最小宿主文档（`entry: user`、单 provider 指向本地 httptest 模型）；经公开 `Boot(configPath, WithPlugins(...))` 贡献一个 `fact->observation` 实例，随后 `host.Run().Invoke` 驱动一次真实会话。
- 命令与结果：

```
$ cd /Users/pengweiye/Documents/codes/tagent
$ go test ./assembly -run '^TestSymptomAnchorHostBootMountsContributedPlugin$' -count=1
rc=1
--- FAIL: TestSymptomAnchorHostBootMountsContributedPlugin (0.09s)
    symptom_anchor_coherence_test.go:96: the contributed instance observed no fact of the driven turn (its builder ran 0 times): the boot dropped the WithPlugins contribution and the run-level set it published carries the host's own instances alone, or the published binding never dispatched the instance it built
FAIL
```

- 夹具自证（同一进程的日志行，说明 `Boot` 本身成功、回合已跑完，红的只是贡献面）：

```
INFO telemetry/log_observer.go:74 tagent: turn start fact=1-eb58e67f2622d4bf4ae5ba1a98332733 agent=user session=session-symptom-anchor-boot invocation=inv-symptom-anchor-boot
INFO model/observe.go:80       tagent: model response model=model-a@local/model-a terminal=completed ...
INFO telemetry/log_observer.go:74 tagent: turn end   fact=2-7c1ab8d2850f17f3f9b775975578ef30 agent=user session=session-symptom-anchor-boot
```

  测试内先断言 `turn.Terminal == completed` 与 `SessionFacts(addr)` 非空，再断言观察面，故空观察不可能是「回合没跑」。

- 复跑：同命令第二次 `rc=1`，失败行同为 `symptom_anchor_coherence_test.go:96`（非 flaky）。
- 判定：**已复现**。`Boot` 不承接调用者的 `WithPlugins`，贡献工厂一次都没执行（`builds=0`）。
- 机制静态证据（供 02.203 对照，不计为运行证据）：
  - `assembly/host.go:479-482` 把调用者 option 应用到 `cfg`；`assembly/host.go:632-720` 随后**重建**一份 options，其中 `WithPlugins`（`host.go:690`）是宿主自己那一份，`cfg.plugins` 在 `Boot` 内零次转发。
  - 转发面本身是可用的：`Run` 侧 `cfg.plugins(r)` → `r.plugins`（`assembly/run.go:598-604`）；`agentPlugins` 把 `r.plugins` 并入每个 agent 的绑定集（`assembly/plugin_catalog.go:384-407`）；`factPipeline` 用 `binding.Plugins` 建流（`assembly/facts.go:1010-1018`）；`pipeline.After` 派发 `ObserveCommit`（`api/plugin/phases.go:235`）。仓内既有 `Assemble`＋`WithPlugins` 的观察者用例（`tests/integration/history/event_integrity_test.go:934`）走的就是这条派发路径。
- 负锚（测试内，`rc=1` 时未被触达；本单不声称其已绿）：观察到的每个 id 必须能经 `Run.CommittedEvent` 读回，且至少一个属于被驱动的会话——防「派发的不是本次提交」。
- 消费者锚：公开 `Boot` ＋ 它发布的 `Run.Invoke`，无测试内自接线、无 `Assemble` 直调。

## S2 · 裁决写失败后的状态分叉

- 锚：`TestSymptomAnchorFailedRulingKeepsTheHold`
- 夹具：复用 `approvalWriteSeam`（`runtime/task/approval/approval_store_test.go:404-470`）注入；`Request` → 把失败点 arm 在裁决的下一次写（`puts+1`，即 `attention.answer` 那条写）→ `Decide` 失败 → `SnapshotEvidence` → 新 manager `BindAttention`＋`RestoreEvidence` → `Pending`／`Lookup`。
- 命令与结果：

```
$ go test ./runtime/task/approval -run '^TestSymptomAnchorFailedRulingKeepsTheHold$' -count=1
rc=1
ERROR approval/attention_log.go:270 tagent: attention fact not persisted key=attention:60c0d9d773ef9290 type=attention.answer: ... the store refused the write at tagent/attention/v1/fact:attention:60c0d9d773ef9290
--- FAIL: TestSymptomAnchorFailedRulingKeepsTheHold (0.00s)
    symptom_anchor_ruling_test.go:48: the cut states [] under [], want the one hold the refused ruling left owed: ...
FAIL
```

- 复跑：同命令第二次 `rc=1`，同一失败行（非 flaky）。
- 判定：**已复现**，且成因具体为**切面丢项**而非误放行：
  - 误放行一侧未成立——失败行之前的 `Lookup` 断言通过（`Decide` 失败后 `Lookup` 不返回 approved；`stateOf` 按链上事实折叠，`runtime/task/approval/attention_store.go:221-234`）。
  - 丢项一侧成立——`Decide` 在 `fileAnswer` 之前就把 `req.Status` 改成裁决值（`runtime/task/approval/approval_store.go:457-462`），写失败后不回滚；`SnapshotEvidence` 对已绑面（`factLog != nil`）只陈述「未决义务」，于是这条被就地改过状态的请求被跳过（`approval_store.go:194-221` ＋ `:266-271`），切面因此为空。后续 `Pending`／`RestoreEvidence` 断言因切面为空而未被触达。
- 负锚（`rc=1` 时已通过的部分）：同一 book 的 `Lookup` 不得把失败裁决读成批准。
- 消费者锚：`ApprovalManager.Request/Decide/SnapshotEvidence/RestoreEvidence/Pending/Lookup` 六个真实入口，无 helper 替代。

## 未写项 · Stateful 消费者重启恢复断言（夹具障碍，非产品结论）

派单允许「同一夹具可行则附带，不可行则拆独立测试」。本单判定**当前公开入口不可构造**，故未写，理由如下（均为源码实证）：

- `HostOption` 与 `Option` 是同一别名 `func(*assemblyConfig)`（`assembly/host.go:151`、`assembly/run.go:1404`）；`assemblyConfig`（`assembly/run.go:1330-1392`）**没有任何状态切片/Stateful 贡献字段**——公开入口能贡献的只有插件工厂、治理面、审批簿、存储、模型租约等。
- 唯一公开的状态切片门是 `Run.RegisterStateSlice`（`assembly/checkpoint.go:287-292`），只能**在 boot 之后**调用；而恢复发生在 `assemble` 内，`Boot` 在 `assembly/host.go:778-781` 就把「切面陈述了无持有者的切片」具名拒绝，Boot 返回后 `PendingRestore` 已为空。
- 结论：经公开入口「贡献一个有状态消费者 → 重启 → 断言恢复」这条链今天不存在可写的入口；把它补出来属 02.204（写面 `runtime/task/*`、`runtime/state/durable.go`）与 01.102 契约范围，不是本单可改的生产代码。故 S1 只证插件观察面，缺片/恢复语义交由 01.102（新消费者缺片）与 W1-C 状态取证。

## 边界与复跑清单

| 命令 | rc |
|---|---|
| `go test ./assembly -run '^TestSymptomAnchorHostBootMountsContributedPlugin$' -count=1` | 1（红，预期） |
| `go test ./runtime/task/approval -run '^TestSymptomAnchorFailedRulingKeepsTheHold$' -count=1` | 1（红，预期） |
| `go build ./assembly ./runtime/task/approval` | 0 |
| `go vet ./assembly ./runtime/task/approval` | 0 |
| `gofmt -l` 两文件 | 空 |

- 按要求**未跑**两包其余测试（无 `-run` 全量、无 `./...`）。
- 未改生产 `.go`；若 S1 之后仍红，首个被怀疑对象应是 02.203 的 `Boot`→`WithPlugins` 转交，而不是本锚的断言。
