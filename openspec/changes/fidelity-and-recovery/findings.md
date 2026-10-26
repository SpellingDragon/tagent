# 2.2 findings — G2 完整事实一致性 + G3 提交错误传播（合单）

## 结论

红转绿已闭环：一个提交过的事实跨存储关闭与重装配**全字段相等**（元数据、纯 ToolCalls、InvocationID/TaskID/DecisionID/EpisodeID/exec meta 全在）；同键同内容=幂等（返回原身份、恰一条记录）；同键异内容=具名冲突 `memory.ErrFactKeyConflict`（不写不改、内存不更新、两次调用一成功一报错，不得双成功）。`commitFact` 失败沿调用链传播到 `Run.Invoke` 返回 error 且 terminal 为 failed，绝不 completed；被否方案（静默补投义务）未实现（D6-1）。

## 红证（先于修复，四条症状锚）

- 命令：`go test ./tests/integration/host -run '^TestFactFidelity$' -count=1 -v`（修复前）
- ①重装配丢字段：restored meta 只剩 `event_summary`、Message 空、InvocationID/TaskID/DecisionID/EpisodeID/InstanceID/AttemptID 六 ID 全丢（EventBody 轻字段编解码）。
- ②同键双写：`the chain holds 3 records of 2-6c67119325b39e2e770ddbed29a3e560, want one record`（重复键跳写盘仍更新内存）。
- ③同键异内容双成功：`the second content of fact-fidelity-conflict answered success beside fact-fidelity-conflict, want a named conflict`。
- ④吞错假完成：`the turn beside a held segment barrier returned no failure beside the terminal completed`（commitOutput 调用方仍返回成功）。

## 实现要点

- `memory/event_body.go`：`EventBody` 全字段编解码（meta/message/source/target + invocation/task/instance/episode/decision/attempt/call/approval 九 ID + format/retention）；`Event()` 从持久 body 重建完整事件（Message 空且 Content 非空→`model.Message{Content}` 兜底；缺 summary 键回填 meta）；`SameFact`/`canonical` 以 canonical 编码字节比较判定"同一事实"，对 JSON 归一化（nil/空 slice、base64→string Arguments）免疫。
- **持久格式向后兼容**：legacy 字段（`event_key/event_type/event_summary`）照读；旧数据缺字段=absent 语义（空 ID/空地址），不 panic、不发明身份。
- `facts.go`：`commitFact`→`(EventID, error)`；`commitFactChecked` 内存侧键判定、`persistFact` 盘侧键判定（内存与盘同语义）：同键同内容=幂等返回原身份，同键异内容=`fmt.Errorf("%w: %s", memory.ErrFactKeyConflict, key)` 不写不改；`StoreEvent` 的 `ErrContentConflict` 竞态窗口（预检与写入之间）→ 冲突后再取比对，映射到同一具名冲突。
- `runtime/agent/loop.go`：`FactCommit` 签名改 `func(evt event.Event) (event.EventID, error)`（3 红线 doc）；Drive 两处 `commitOutput` 失败→`failCommit`（terminal=failed + 返回 error）；`commitOutput/commitNotice/commitFact/commitFactKey/CommitFactFor` 全链返错；无返回通道的 `commitSettled/commitProgressFact` 走 `consumerWarn`（WARN 报具名原因，不吞错、不假失败交付）。
- 生产读路径重装配：`run.go` `CommittedEvent`/`seedReplay` 改用 `body.Event()` 重建 + Key 覆盖（读回即全字段）。
- 机械签名适配（白名单外、必要）：`runtime/agent/skeleton_assert.go`（断言）、`run.go`/`tools.go`（调用点）、`loop_test.go`/`scenario_s15_test.go`/`progress_notification_test.go`（commit 桩）。

## 键语义（D6-2 裁定 + 自决的排除面）

- 同键同内容=幂等：返回原身份，盘上恰一条记录；同键异内容=`memory.ErrFactKeyConflict`（定于 memory 包，调用方 `errors.Is` 判定）。
- `SameFact` **排除戳记字段**：Timestamp（提交路径盖 now）、InvocationID（CommitToSession 空则每次 mint `#delivery-` 新后缀）、InstanceID（每 run 盖 r.instance）、Format/RetentionPolicy。依据：`event_integrity_test.go` `commitIdentityRows`（4011-4032）冻结语义——同键同 body 新戳重发=幂等返回原身份。其余字段全比，内容差异一律冲突。

## 错误传播（D6-1 裁定）

- 提交失败→`Invoke` 返回 error，terminal=failed（`failCommit` 设置 `turn.Failure`），调用方绝不见 completed。被否的"静默补投义务"未实现。
- 负锚：`factFidelityCommitFailureFailsTurn` 断 `root/kv/<partition>/snapshot.json` 原子 rename 屏障（换目录致 rename 失败），hold 落在输入已提交、只拦输出提交 → `Invoke` 返回 error 且 `terminal != completed`；正锚：正常路径仍 completed。

## 绿证

- `go test ./tests/integration/host -run '^TestFactFidelity$' -count=3` → ok（4 子测：重装配全字段/幂等/冲突/错误传播）。
- `go test ./memory/ -count=1` → ok（三锚：`TestEventBodyKeepsEveryFieldAcrossTheRoundTrip`/`TestEventBodyLegacyBytesReadAsAbsentFields`/`TestSameFactJudgesTheFactAndNotTheCommit`）。
- 消费者锚：`Host.Invoke`（`invoke.go` 直接透传 Drive 返回）→ 断屏障时返回 error；`CommitToSession`/`CommitFactFor` 正式入口返错并被 run.go 包装。

## 回归（既有测试影响核对）

- `go test ./tests/integration/execution ./runtime/agent -count=1` → ok。
- event_integrity/identity 系列 16 项全绿（`-run 'Integrity|Identity|Commit'`）：TestEventIntegrityReproduction/TaskContinuation/Delivery/ReflectionDelivery/Replacement/IdentityMatrix/DecisionFlowMatrix/FormalAssembly/Harness、TestProjectionCarriesTheCommittedSequence、TestAgeFoldFiresFromCommits、history 侧 ConsumptionRecovery/Projection/Restart/Persistence/RestoreUse——既有"重复提交"假设经核对与新键语义一致（重发同键同内容仍幂等返回原身份），无需修改。
- `go build ./...`、`go vet ./...` 通过；`bash scripts/lint.sh` 五关中 gofmt/modules/build/vet 过，comment policy 仅剩并发单 A 的 `examples/wechat-bot/smoke_test.go` 4 项 free-standing（勿碰，留给 A）；我方注释违规已归零。
- `go test ./...` 全仓 37 包 ok；`tests/integration/host` 仅 `TestRequestFidelity` 三红（`tool_call_then_next_turn`/`request_after_compression`/`request_after_cold_boot`）——clone worktree（`git worktree add /tmp/tagent-attr-8afa6cf 8afa6cf`）复跑同样三红，证明是并发单 A 的 G1 红证，非本单回归。

## 兼容性边界（已知，有意如此）

- legacy 轻 body 与全字段重发在**同一键**下会具名冲突：旧数据缺的字段按 absent 语义读回，若用全字段事件重新提交同键，内容判定不等→`ErrFactKeyConflict`。这是 D6-2 语义的正确行为（同键必须同内容），旧数据读路径不受影响。
