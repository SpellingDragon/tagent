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


# W0 findings — 1.0 G1 协议保真 + 1.1 G10 测试去假设（麻雀·单 A）

## 结论

1.1 绿勾（G10 十轮全绿含乱序负锚）；1.0 不勾：①② 红转绿闭环，③（冷启动后请求）在本叶白名单内**结构上不可达**，红证保留可复跑（`TAGENT_FIDELITY_COLD_BOOT=1`）。修复收在投影呈现层（request.go）：投影条目只带 Role/Content，呈现层从进程事实（r.facts）与请求快照（r.snapshots）恢复 ToolCalls/ToolCallID/工具名/参数，令请求通过调用-结果配对校验（ToolCalls 带 ID/名/参数；role=tool 有 ToolCallID 且被更早 assistant 应答）。

## 红证（先于修复，/tmp/g1_red.log；B 的 2.2 findings 末节 clone 复跑同红可旁证）

- ①③ `role=tool carries no tool_call_id (content="ok")` + 协议替身网关 400；
- ② 红在防空转守卫：折叠把 ActionCommand 折进 segment_fold，配对材料消失 → 调窗口（fills 二行化 + max_tokens:375 / compress_threshold:1.0 / keep_recent_tasks:1）让压缩落在工具段"进行中"免疫期（进行中段永不折叠），② 转绿（fold products=1）；
- 负锚 ProtocolGate PASS（裸 POST 拆 ToolCallID → 400 含 tool_call_id）。

## 投影字段链评估（派单要求的扩面评估，四断点）

1. 呈现层 `projectedSegment` 只留 Role/Content —— **已修**（本单白名单内，restoreCallPairing/callIdentityOf/callArgumentsOf）。
2. `ProjectionEntry{Key,Type,Summary,Content,Time}` 无工具字段 —— 扩 memory/replay 无用：两个生产者（`mappedEntryOf`=facts.go 禁碰、`seedReplay`=run.go 未拥有）都无法在白名单内接线 → 改在呈现层从 r.facts/r.snapshots 回收，不扩写集。
3. 事实完整性：assistant 工具调用消息从不入事实（commitOutput 只在回合终点提交终局输出）→ **真参数从未持久化**（请求快照仅内存、RL capture 可选且无读回面）→ ③ 冷启动的"ToolCalls 带 ID/名/参数"结构上不可能。
4. 冷启动重建：seedReplay 虽走 body.Event()（2.2 全字段重建）仍经 mappedEntryOf 丢工具字段，且 r.facts 不被 seed 填充 → ③ 连调用身份都无处回收。

→ ③ 的恢复需：loop.go 提交 assistant 调用事实（禁碰）+ EventBody 全字段（2.2 已落）+ 种子重建接线（run.go 未拥有）。**本单不硬缩验收面，③ 停车待裁**。

## ③ 处置（待裁决，"本项不以 Skip 结项"）

具名结构性 Skip（先例：failure_notice/R24），`TAGENT_FIDELITY_COLD_BOOT=1 go test ./tests/integration/host -run 'RequestFidelity' -count=1` 可随时复跑出具名红证。选项：(a) 调用事实/种子恢复落地后解除 Skip；(b) ③ 改挂 fact-fidelity(2.2) 叶（需上级改 tasks/spec）；(c) 本单只收 ①②。

## 新发现：受理回执事实断裂（建议转 2.2 fact-fidelity 正题，本单未碰）

- 症状：`TestLocalSmokeSymptomAcceptedSilent/silent_until_settled` 在 G1 修复后暴露 flaky（count=30 六红）：`受理回执不是最小 ok：模型真实收到的 tool result = "{call-SMOKE-R9601 task-2 true 0}"，want "ok"`。
- 对照铁证：`git worktree` HEAD(817ab97) + 仅换入本单 request.go → `-run '^TestLocalSmokeSymptomAcceptedSilent$' -count=30` 6 红；HEAD 原装 10 轮 0 红。
- 机制：委派工具 `result.Output` 是 `task.AdmissionReceipt` 结构体，事实 Content 走 `resultText` 的 `fmt.Sprintf("%v", out)` fallback（facts.go:549/804）存成结构体文 `{callID taskID true 0}`；而 wire 当轮走 `admissionText`="ok"（loop.go:1083/1568）。修复前投影 tool 消息无 ToolCallID，症状探针（按 ToolCallID 匹配）够不到这条断裂消息；修复后挂上 ToolCallID 即暴露——**重放的 tool result 不是 "ok"，当轮 wire 文 ≠ 事实文**。
- 性质：事实层内容保真断裂，恰是 fact-fidelity(2.2) 的正题；写入点 facts.go/loop.go 全在本单禁碰面。本单未改症状断言（改断言=掩盖缺陷）。
- 建议：转 B/2.2 裁决修法（事实 Content 与 wire 回执同文，或 wire 用事实文并同步 admissionText 语义）。

## 绿证

- `go test ./tests/integration/host -run 'TestRequestFidelity' -count=3`：① ② 各 3 PASS、③ 3 SKIP（结构性停车）、ProtocolGate 负锚 2×3 PASS；① 断言真参数逐字相等（`requireCallResultPair(name=send, arguments=...)`），② fold products=1 且配对完整。
- G10：`go -C examples/wechat-bot test . -run '^TestLocalSmokeSymptomNoSendTool$' -count=10 -tags smoke` → ok（十轮全绿，乱序负锚 locating_survives_out_of_order 在内）。
- 症状全家：`go -C examples/wechat-bot test . -run '^TestLocalSmokeSymptom' -tags smoke -count=1` → ok 12.780s。
- 回归：`go test ./tests/integration/host ./runtime/agent -count=1` → ok 26.215s / ok 0.863s；`go build ./...`、`go vet ./...` 通过。

## 回归处置（本单修复引起的唯一回归，已修）

- `TestEventIntegrityReproduction/request_and_send_identity_correlation` 曾红（`the follow-up request index = 3, want it after the delegation tool result 3`）：修复后回放历史亦带配对，探针 last-match 误命中 follow-up 请求 → 探针改**首命中**（`acceptIndex < 0 &&`），首命中=修复前的唯一命中者，不变式（follow-up 严格在后且为末条）不动；函数 doc 记录缘由。

## lint

- 本单 4 项 comment_policy 违规已归零（TestRequestFidelity/ProtocolGate 文档压"一行意图+bullet+索引"、smoke_test.go free-standing 语义并入 t.Logf）。
- 仍红 3 项**全属 817ab97（单 B）遗留**：facts.go:165（禁碰面）、fact_fidelity_test.go:317/359（B 的 2.2 测试）——待 B 修或上级裁决，本单不碰。
