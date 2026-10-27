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

# W1 findings — 2.1 G4 投影会话索引作用域（单 2.1）

## 结论

replays 索引键由 `event.SessionID` 单键改完整地址 `event.Address{AgentID, SessionID}`，两层槽设计：**scoped 生产槽**（生产路径按完整地址注册）+ **legacy 无 agent 槽**（AttachReplay 冻结缝保持原语义）。`requestBoundary` 按 `{boundary.Agent, boundary.Chain.SessionID}` 经 `replaySlotLocked`（exact → legacy fallback）查找。红证先行 → 修后绿 → 负锚还原单键复红同症状，闭环。

## 设计约束（冻结面决定两层键，而非严格分区键）

- `AttachReplay` 签名被骨架断言冻结，且 6 个白名单外冻结测试以任意 PartitionID 注册后经 run.Invoke 消费（execution/cross_session_scoping、learning/formal_request_snapshot:75、history/age_band_request、history/projection_replay_and_budget:391,426、acceptance/scenario_age_fixtures:186、host/request_boundary:195,369）→ 严格按 PartitionIDFromName 匹配会打破它们 → AttachReplay 语义不动（legacy 槽，PartitionID 继续被忽略）。
- `RequestChain{SessionID}` 由 loop.go 构造（PartitionID 恒 0；loop.go 禁碰）→ 查找侧只能从 `boundary.Agent` 推导作用域。
- 会话级读面（ReplayEntries/noticeProjection×2/age_fold ProjectionOf）经 `replayOfSessionLocked`：legacy 优先 → 多 scoped 按 AgentID 排序取首个（确定性降级，SessionFolds 接口签名未动，骨架断言合规）。
- 事实地址派生 `replayAddressOf(evt) = {agentOf(evt), sessionOf(evt)}`：与 transcript history 的地址键同源。

## 机械波及申报（facts.go/age_fold.go；run.go 只取索引键 hunks）

- facts.go 6 处纯索引键适配：recordMapped 槽解析/回写、attachDeclaredReplay（addr 派生 + held=scoped||seam 判定 + attachScopedReplay）、ReplayEntries、noticeProjection.NoticeBoundary/NoticesFrom、CommitHistory mapped 判定。**facts.go:310 `if window.MaxTokens <= 0` 门逐字未动**（G7/4.1 归属，本单未碰）。
- age_fold.go 1 处：ProjectionOf 改会话级读面（一行）。
- run.go 只改 replays 字段/初始化/seedReplay 索引键（seeds/order 键化 + attachScopedReplay 注册）；run.go 其余未提交 hunks（InjectEvent/CommitToSession backfill、seedFact、doc）属并发在途单，git add 时逐 hunk 剔除，未入本单提交。
- facts.go 未提交 hunk（persistFact 注释删除）同样属并发在途，未入本单提交。

## 归因剥离实验（三处确定性红证非本单所致）

全量 `go test ./tests/... -count=1` 红 3：capabilities TestFormalLoopEnforcesPolicy（presentations 5 条 want 4）、history TestEventIntegrityRestoreUse/next_request_uses_restored_material（message 1 = assistant "looking" want tool）、inspection TestObservationReachDiagnostics（观察 thinking_plan 不在四类冻结类型）。**剥离**：还原本单 4 文件（request.go/run.go/facts.go/age_fold.go）至 HEAD、仅留并发在途改动（runtime/agent/loop.go +29、memory/event_body.go SameFact 语义、tool/exec/*）→ 三测逐字同红；HEAD 纯净 clone（/tmp/tagent-head-check@5f78bdf）三测绿 → 红来自并发在途改动，非本单索引键改动（剥离后恢复本单文件，红不变）。execution 包全量并行一次 480s FAIL / 一次 24s ok（单跑 21s ok）→ 负载型 flaky，本次全量绿。

## lint

- 本单 3 项 comment policy 违规（agent_session_isolation_test.go：doc-not-brief/doc-not-name-prefixed/test-doc-not-one-sentence）已修归零。
- 残余 6 项全属 tool/exec/exec_test.go（free-standing ×4 + test-doc-not-one-sentence ×2，并发 3.3 在途，白名单外勿碰）。

## 三锚与绿证（可复跑）

- 正锚：`go test ./tests/integration/host -run 'TestAgentSessionIsolation|TestRequestFidelity' -count=3` → TestAgentSessionIsolation 3×PASS（requests_stay_scoped + detector_flags_foreign_marker）、TestRequestFidelity 3×PASS（tool_call_then_next_turn/request_after_compression/request_after_cold_boot）、TestRequestFidelityProtocolGate 3×PASS；ok 1.868s。
- 负锚：replayAddressOf 临时还原 `event.Address{SessionID: sessionOf(evt)}` 单键 → `requests_stay_scoped` 红（`request 2 (model model-beta) carries the foreign markers [alpha-history-marker-A11 alpha-answer-marker]`）→ 恢复后绿。
- 消费者锚：Boot→Invoke 真实请求（bootIntegrityHostWithAgents + driveIntegrityTurn→host.Invoke，scriptedProvider servedRequests 读回 wire 形态；标记从 provider 真实收到的请求文本读出，非测试手工接线）。
- 回归：`go test ./tests/integration/host ./memory/... ./runtime/... -count=1` → 9 包全 ok（host 26.8s）；`go build ./...`、`go vet ./...` 干净；全量 `go test ./tests/... -count=1` 除上述 3 处并发单红证外包 ok（acceptance/config/evolution/execution/host/journey/learning/state）。

## 3.3 G6 执行最终输出保存与接管（D6-4 三态树）

### 症状与修复

- G6 缺陷钉在 state.go:241 `exitObserved` → `release()`：退出释放前不保存最终输出，退出后捕获 0 次、再读 ErrResourceUnknown。修后 `release()` 在 `backend.Stop` **前**有界捕获最终帧（窗口 `finalOutputCaptureWindow` 5s 默认，`WithFinalOutputCaptureWindow` 可调），确认停止后写入 `m.final[key]`（进程内 map、每键最后一条）。
- 读面：`Manager.FinalOutput(key)`（held→ErrFinalOutputUnsettled；无记录→ErrFinalOutputUnrecorded）、`Manager.Query(ctx,key)`（直通 Probe）、`Manager.held(key)`；docs/api 已随 `gen_godoc.sh` 再生成。
- 消费路径：`ReleaseTool` 答案追加 `final output: ...`；`RunTool.Call` 三态树（插在 Acquire **前**）：未 held 且可服务的键 → Query → ProbeAlive=接管（Adopt+Capture+`reattached`）、ProbeUnknown=ErrAdoptUnconfirmed 具名拒绝（不盲目接管/不新开/不宣称结束）、ProbeDead+记录=`settled` 报告（不重跑不覆盖）、ProbeDead 无记录=Acquire 新开（原语义）。三态树只放 RunTool.Call 不放 Manager.Acquire：capabilities 白名单外冻结测试 pin 了 Acquire 在 Unknown 探针后成功的语义。

### 红证先行与文件归并

- evidence/3-3-final-output-red.txt：修复前「最终输出捕获次数 = 0」、exec_release 答案无 omega——与 G6 症状逐字吻合；修后绿。该红证行号指向原 tool/exec/final_output_test.go，已按 lint responsibility-fragmentation 出口(1) 并入 tool/exec/exec_test.go（行号失效，测试本体逐字在 exec_test.go 尾部）。

### 回归发现与修复（均为本单实现缺陷）

- release() 初版用 `context.WithoutCancel(ctx)` 调 backend.Capture → leaseExecutor.Capture 阻塞等 gate 的场景把释放无限挂起（TestRenewRefusesExpiredLease 挂死，-timeout dump 证实）→ 修：有界捕获窗口 + 负锚 `a_hung_capture_never_holds_the_release`（hangCaptures+窗口 50ms → 释放照常、ErrFinalOutputUnrecorded 不谎报）。
- 测试替身 exitFrameExecutor.Capture 正常路径漏 unlock 致死锁（dump：Probe/finalFrames 全等互斥锁）→ 补 `defer e.mu.Unlock()`。

### 设计决策（自行决策，申报）

- settled 不重跑：同键已结算永远报告旧记录（记录永存、读回可靠），引导模型换键重跑；未采用「decl 一致性才报告、decl 不同则新开覆盖」（会覆盖记录丢最终输出）。
- final 记录为进程内 map：G6 症状是同进程「执行进程退出」而非 tagent 重启；跨重启最终输出持久化归 3.2/主规格持久层，不在本单。
- 接管可读性走 run.go 既有 `ExecState(key)→Explanation.Adopted`，未动 host.go（白名单未扩）。

### lint 与形态

- comment policy 6 处违规（free-standing ×4 + test-doc ×2，并入 exec_test.go 带入）已修归零：函数体内/游离注释并入 Call doc bullets 与测试 doc bullets（带 `契约: docs/wiki/tool/tool-architecture.md#action-tool` 索引行），`default: // ProbeDead` 改显式 `case ProbeDead:`。
- 当前 `bash scripts/lint.sh` 在 concept budget 阶段 exit=1：`memory/event_body.go:EventBody` 20 exported fields 超基线 8——属并发 2.2「EventBody 全字段」在途改动（非本单面，白名单外勿碰）。

### 环境与断言形态

- 本地 tmux 可用（new-session/kill/has-session 全通），真实 tmux 双宿主接管测试本地实跑绿，与派单预期「Operation not permitted」不符；tmux 路径测试保留（CI 有 tmux）。
- 该宿主 wire 形态：当轮工具结果=`ok`（受理回执），工具输出以 user 消息到达、下回合历史回放完整文本（前序 findings 已记的既有断裂形态）——集成断言按 `LastIndex` 于模型会话文本定位 settled 报告，不改宿主。

### 三锚与绿证（可复跑）

- 正锚：`go test ./tests/integration/execution ./tool/exec -count=1` → execution ok 22.2s（TestEventIntegrityExecStateEvents 8 子测，含 `restart_takes_over_the_running_session` 真实 tmux 双宿主接管与 `final_output_survives_the_exit`）、tool/exec ok 12.5s（TestExecFinalOutputSurvivesTheExit×3 + TestExecTakeoverOrNamedReport×4）；`-race` 一次两包 ok（13.9s/23.4s）。
- 负锚：`a_hung_capture_never_holds_the_release`（挂起捕获不得拖延释放）绿；`names_the_unconfirmed_state_without_taking_over`（未知探针只具名拒绝、零接管零新开、错误不含 released/stopped/exit=）绿；既有 `unconfirmed_restart_not_reported_stopped` 保留绿。
- 消费者锚：ReleaseTool 正式入口答案携带 `final output: omega`；`runEventIntegrityExecFinalOutputSurvives` 经 host.Invoke 真实模型回合读回 settled 报告（LastIndex 定位防第一回合误报）；`runEventIntegrityExecRestartTakesOver` 回合文本含 `reattached `+key 且 `restarted.Run().ExecState(key)`→Held&&Adopted。
- clone 级：`git clone` 干净树（本提交，无共享工作区并发在途改动）→ `go build ./...` 好；两包 ok（execution 21.5s / tool/exec 12.0s）；并发面 3 红证测试（capabilities TestFormalLoopEnforcesPolicy、history TestEventIntegrityRestoreUse|next_request_uses_restored_material、inspection TestObservationReachDiagnostics）在同一 clone 全绿 → 三红归共享工作区并发在途改动，非本单。

# W0+W1 findings — 1.2 调用事实入链 + 2.3 戳记边界（合单 facts/loop/run 簇）

## 结论

两叶合单落地：①assistant 工具调用消息（ToolCalls/ToolCallID/逐字参数）由 commitCalls 提交为调用事实、先于 result 入链，seedReplay 经 seedFact 把存储事实回填活账本——投影/恢复即重配对（restoreCallPairing 的数据源补齐）；②提交协议改"零值回填"（InjectEvent/CommitToSession 非零不覆写 Source/Target/Owner/InvocationID），SameFact 排除面机制化为协议生成戳记（Timestamp/InvocationID/InstanceID/Format/Retention 出比较面），Source/Target/TaskID 调用方提供即入（absent-wildcard：未声明侧匹配一切，两侧均声明则精确比较）。Q5/Q17 裁定兑现。

## 红证（先于修复，③ 冷启动）

`TAGENT_FIDELITY_COLD_BOOT=1 go test ./tests/integration/host -run 'RequestFidelity/request_after_cold_boot' -count=1 -v` → FAIL（单 A 的 Skip 红证形态复现）：`request 3 (model model-entry): message 2: role=tool carries no tool_call_id (content="ok")`——调用事实不入链，冷启动投影只恢复 result 侧，配对断裂。

## 实现要点

- **1.2**：`loop.go` 新增 `commitCalls`（TypeThinkingPlan + assistantOf 全 ToolCalls），接线在 decide 非 final 工具调用分支 `dispatch` 前——**顺序关键**（call 事实先于 result 入链，投影序 [call, result]，否则冷启动 mint 路径给 "{}" 参数）；提交失败以具名错误结束决策、任何 call 不执行（D6-1 形态）。`run.go` `seedReplay` 循环 `r.seedFact(evt)`（新 helper）把 tier 事实回填 `r.facts/r.factOrder`（恢复配对数据源 + 跨重启再提交幂等，防 recordMapped 双登记）。
- **2.3**：`run.go` InjectEvent/CommitToSession 四字段零值回填；InjectEvent 以 `evt.InvocationID`（回填后）驱动 Drive 保持范围身份一致（自决项）；InstanceID 保持协议盖戳（禁覆写）。`memory/event_body.go` canonical 加 Source/Target/TaskID 清零，SameFact 加 pairwise absent-wildcard（`unstatedRange`：零值或 ==Owner ≡ 未声明，与回填默认值同源）。
- **不动**：fold.go（TypeThinkingPlan 折入摘要由 mint 路径兜底，② 活进程 snapshots 有真参数；③ 32000 窗口种子侧不折叠）、request.go（禁碰面）。

## 三处旧假设测试期望扩容（申报，非验收面缩水）

1.2 使回合事实集合新增调用事实一类，三处"call 不入链"旧世界断言转红（与 2.1 归因剥离实验结论一致：该三红来自本合单在途改动）：
- `capabilities/formal_loop_enforces_policy_test.go`：presentations 计数 4→5（断言语义"每已提交事实一个 presentation"不变，枚举扩容 +assistant call）。
- `inspection/observation_reach_diagnostics_test.go`：obsKindCoverage 覆盖枚举加 TypeThinkingPlan 分支（断言 call 身份 ToolCalls）；四类→五类。
- `history/event_integrity_test.go`：restoreAssertNextRequest 期望序 [user, tool, assistant] → [user, assistant call, tool, assistant]，并加 carriedCalls 断言（ToolCalls 携带 restoreCallID）；persistenceCallText 提为常量（渲染器与断言单一真源）。
三处均为核心验收意图不变/增强（材料全带、每事实全达、配对可断），非删断言。

## 白名单外触碰（派单明示，申报）

`memory/event_body.go`（SameFact/canonical 边界机制化，Q17 裁定直接要求面）；其余均在白名单（runtime/agent/loop.go、facts.go、run.go、tests/integration/**、docs/api/**）。tools.go 未动（确认 1.2 无需扩白名单）。

## 三锚与绿证（可复跑）

- 红→绿锚：`TAGENT_FIDELITY_COLD_BOOT=1 go test ./tests/integration/host -run 'RequestFidelity/request_after_cold_boot' -count=1 -v` → 修后 PASS（`[fidelity 3] positive: the cold-boot request carries the complete pair of "call-fidelity-cold-boot"`），Skip 已解除（无环境变量无条件跑同绿）。
- 2.3 正锚：`factFidelityUndeclaredStampIsIdempotent`（带 Source=X 提交→Source 存活入事实（修复前被 CommitToSession 覆写）；同 key 一次带 Source 一次不带=幂等、单条记录、戳记不动）。
- 2.3 负锚：`factFidelityStampOverwriteRefused`（Source/TaskID 覆写→ErrFactKeyConflict 具名冲突、存储保持首次声明值）。
- 消费者锚：Boot→Invoke 真实请求（RequestFidelity 三场景 + ProtocolGate 400 门；scriptedProvider servedRequests 读回 wire 形态）。
- 回归：`go test ./tests/integration/host -run 'TestFactFidelity|TestRequestFidelity' -count=3` → ok 1.365s；`go test ./tests/... -count=1` → 11 包全 ok（host 26.7s）；`go -C examples/wechat-bot test . -run '^TestLocalSmokeSymptom' -tags smoke -count=1` → ok 15.756s；`go build ./...`、`go vet ./...` 干净；gen_godoc --check 绿（68 包）。
- lint：本合单文件 comment_policy findings 归零（顺手修 facts.go:165 free-standing 2.2 遗留、fact_fidelity_test.go 两处 free-standing 并入 doc）；残余 9 项全属并发在途（agent_session_isolation_test.go 2.1 项、tool/exec 3.3 项），非本单面。
- lint 收尾（clone 级）：371ce82 修本单新写 bullet 超长（`五类事实全达` 169 runes>testDocMaxLineRunes 160）压至 149；clone 复验 comment_policy `0 finding(s); 0 beyond baseline`，并发在途 9 项已被各自单清零。
- lint 残红归属确认：concept budget `memory/event_body.go:EventBody 20 exported fields exceeds baseline 8`（scripts/check_concept_budget.py:131）自 817ab97（2.2「EventBody 全字段」）即存在——`git show 817ab97:memory/event_body.go` EventBody 20 字段、`git show 817ab97 --name-only | grep -c golden`=0（2.2 漏同步 ratchet 基线）；本单 6e0500b 只改 SameFact/canonical/helpers 未加字段。3.3 findings「lint 与形态」已裁定同一红「非本单面，白名单外勿碰」，本单依裁定不碰 scripts 基线，留 2.2/编排处置。
- clone 级绿证（HEAD=371ce82）：`TAGENT_FIDELITY_COLD_BOOT=1 go test ./tests/integration/host -run RequestFidelity -count=1` → ok 0.527s；`go test ./tests/integration/host -run 'TestFactFidelity|TestRequestFidelity' -count=3` → ok 1.078s；`go test ./tests/integration/inspection -run TestObservationReachDiagnostics -count=1` → ok 0.362s。

## 环境与并发申报

- 共享工作区并发在途：request.go/age_fold.go/tool/exec/*（3.3）、tests/integration/execution/event_integrity_test.go（3.3）等非本单面未碰；2.1 已提交 e46708d 并逐 hunk 剔除本单 hunks（对称：本单提交逐 hunk 剔除 2.1/3.3 的账目 hunks）。
- 终端会话被并发 agent 共享，验证输出以重定向文件为准。
- known flaky（findings W0 已记，超范围未修）：受理回执事实断裂（TestLocalSmokeSymptomAcceptedSilent）本次症状全家 6/30 观察面下全过。

# W2 findings — 3.1 G5 交付恢复（单 3.1）

## 结论

G5 两半落地：①待审批投递跨重启恢复——`pendingDeliveries` 每笔义务在内存表之外写持久记录（复用 localfile 持久层 `consume:` 回执同族：`ledger.TierFactLog` 同一 kv 后端，键 `tagent/task/v1/delivery:<id>`，字段=身份/内容/血统/目标/状态/HeldAt），Boot 经 `restorePendingDeliveries` 重建（`held`/`retry` 恢复、`sent`/`refused` 回执不恢复、坏记录具名拒启）；②发送失败持久重试义务——`deliverOne` 发送失败把正文停为 `retry` 义务（回执文案不动），经审批面补投一次幂等。

## 红证（先于修复，三子测全红）

`go test ./tests/integration/host -run '^TestDeliveryRecovery$' -count=1 -v` → 三子测 FAIL（证据 `evidence/3-1-delivery-recovery-red.txt`）：`held deliveries after the restart = []`（G5「重启后 0 条」）、`owed deliveries beside the refused send = []`（G5「失败即丢」）。修后同命令三子测全 PASS。

## 实现要点

- `host.go`：`hostDeliveryRecord`（未导出，JSON 落盘）+ 状态常量 `held/retry/sent/refused`；`recordDeliveryState` Put+Sync，写失败仅 WARN 不丢进程内义务；`holdDelivery`/`restoreHeldDelivery` 带状态入表并落盘；`ApproveDelivery` 成功→`sent` 回执、无 sender/发送失败→`retry` 义务回表；`RefuseDelivery` 先 `refused` 回执再审计 fact；`Boot` 在 face 启动前恢复，`deliveryHeldSeq` 从恢复 ID 推进 heldSeq 防身份冲突。
- **冻结面保持**：`TestEventIntegrityDelivery`（失败不自动复投、回执恒 `[send_denied]`）不动——补投只走审批面，`deliverOne` 失败仍返回 `deliveryRefused, err.Error()`。

## 自行决策与已知边界（申报）

- 「发送失败转持久重试义务」实现为**审批面补投义务**（status=retry 进 pendingDeliveries，HeldDeliveries 可见、Approve 补投一次）而非自动复投：依据冻结面（不自动复投）+ 负锚「批准两次第二次 ErrDeliveryNotFound」（复投恰一次）。
- at-least-once 边界：ApproveDelivery 先 Send 后记 `sent` 回执，进程在两步之间崩溃则该义务重演（可重复补投一次）；与 `consume:` 回执族同语义。
- 非 durable tier 时持久层退化为进程内表（tier 已有「keeps no record across process exit」告警），重启恢复为 no-op。

## 三锚与绿证（可复跑）

- 正锚：`go test ./tests/integration/host -run 'DeliveryRecovery|DeliveryReview' -count=3` → ok 5.967s（TestDeliveryRecovery 三子测 + TestDeliveryReview 全绿 ×3）。
- 负锚：`a_refused_delivery_stays_refused_and_audited`（拒绝后不复投+审计 fact 带 delivery_id+再重启 0 条+ErrDeliveryNotFound）绿；两处 `the repeated approval = ErrDeliveryNotFound`（批准两次第二次具名拒绝、无第二份正文）绿。
- 消费者锚：正式 `tagent.Boot` 入口（bootDeliveryRecoveryHost → Boot → Invoke 真实回合）；恢复链证据=`tagent: the delivery delivery-000001 stands owed beside the restart: retry`（run tier localfile durable=true 同 root 二次 Boot）；批准链=`the held delivery of delivery-000001 was approved and sent`。
- 回归：`go test ./tests/integration/host ./runtime/... -count=1` → 4 包全 ok（host 26.260s，含 TestEventIntegrityDelivery 冻结面）；`go build ./...`、`go vet ./...` 干净；`bash scripts/lint.sh` → `lint: ok`（gen_godoc 68 包匹配、doc-refs/package-layout 过）。
- docs/api：导出 doc（HeldDeliveries/ApproveDelivery/RefuseDelivery）随语义微调，`bash scripts/gen_godoc.sh` 再生成，仅 `docs/api/github_com__SpellingDragon__tagent.md` 变动。

## 白名单

全部改动在白名单内：host.go、tests/integration/host/delivery_recovery_test.go、openspec/changes/fidelity-and-recovery/**、docs/api/**。

# W3 findings — 4.1 G7 历史恢复与预算解耦（单 4.1）

## 结论

MaxTokens 三身份按 Q2 裁定拆开落地：①注册门删除（`facts.go` attachDeclaredReplay 不再以 MaxTokens<=0 拒注册投影、`request.go` attachReplayAt 不再拒零窗口、`run.go` seedReplay 不再跳过零窗口）；②fold 阈值与呈现预算各用各的——fold 侧零窗口用配置缺省窗口兜底（`run.go` restoreFoldForm：`defaultFoldWindowTokens = 32000`），呈现侧零预算=不限预算（`request.go` projectionBudget：预算从材料全量测得 +1 token 余量，同一 AssembleBudgetPrefix 装配通道，不降级不溢出）；③max_tokens=0 与 32000 恢复路径形态收敛。症状（max_tokens=0 落盘事实重启后请求无旧历史）红→绿。

## 红证（先于修复，evidence/4-1-history-restore-red.txt）

`go test ./tests/integration/host -run TestHistoryRestoresWithoutBudget -count=1` 修复前：
- `history_restores_without_budget`：`the request carries no restored history ... absent from the wire, which carried 1 messages`；
- `presentation_is_unlimited_at_zero`：`tail marker absent ... wire carries 38 chars`；
- `budget_window_still_folds_and_shapes`：修复前即绿（预算窗口折叠面未受损，作防回退锚）。

## 实现要点（五处，均白名单内）

- `facts.go` attachDeclaredReplay：删注册门（`window.MaxTokens <= 0 → attachNoticeFeed return`），零窗口会话与预算会话同样注册 mapped 序列；注释改写为三身份陈述。
- `request.go` attachReplayAt：删 `MaxTokens<=0` 一票否决（保留 nil projection 拒绝）；AttachReplay/attachReplayAt doc 随语义更新。
- `run.go` seedReplay：删 `seed.window.MaxTokens <= 0 → continue`，恢复读取与预算解耦。
- `run.go` restoreFoldForm：`measured.MaxTokens <= 0 → defaultFoldWindowTokens (32000)`，fold 阈值（VolumeThresholdBytes）由缺省窗口兜底，活/恢复折叠共用一把尺。
- `request.go` projectedSegment/projectionBudget（新 helper）：`MaxTokens>0` 照旧按预算呈现（预算照旧影响呈现=负锚）；零窗口预算=投影材料全量（含 RecoveryHint、每条 entry 的 kept original 取大）/BudgetCharsPerToken+1，走同一 AssembleBudgetPrefix，Full 形态呈现不设限。

## 三处旧假设测试期望更新（申报，非验收面缩水）

Q2「无预算=注册投影」直接后果，双面互斥（no fact reaches the model from two faces）下历史从 transcript 面迁至投影面：
- `event_integrity_test.go` runNoticeInputWithoutProjection 状态表：三态 projection 期望 false→true，防注册门回退锚（continuationSingleCopy 单份断言仍过）。
- `mapped_sequence_and_environment_block_test.go` TestProjectionAbsentKeepsTheTranscript：transcript 4 条→0 条（one face per fact），第二请求 3 条映射消息逐条断言（carried==1、无环境块断言保留）；对偶 TestProjectionCarriesTheCommittedSequence 不动仍绿。
- 父 doc bullets 改为现状契约陈述。
函数名保留（无外部引用，1.2 先例）。

## 三锚与绿证（可复跑）

- 红→绿锚：`go test ./tests/integration/host -run HistoryRestores -count=1` → 3 子测 PASS（重启后请求含 turn-one input/answer；159753-char 巨回合全文呈现；32000 窗口 1 synthetic+ticket+尾切）。
- 负锚：`budget_window_still_folds_and_shapes`（max_tokens=32000 压缩照旧生效：synthetic>0、probe 请求含 "[ticket"、尾 marker 被切）+ `presentation_is_unlimited_at_zero`（max_tokens=0 呈现不设限：全文、无 "[ticket"）。
- 消费者锚：`tagent.Boot(configPath)`（max_tokens=0 装配）→ `Host.Invoke` 请求含旧历史（scriptedProvider servedRequests 读 wire 形态）。
- 门禁：`go test ./tests/integration/host -run 'HistoryRestores|RequestFidelity' -count=3` → 9×PASS（RequestFidelity 三场景 + ProtocolGate 400 门同跑）；`go test ./tests/integration/host -run TestEventIntegrityTaskContinuation -count=1` → 10 子测 PASS；host/runtime/memory 回归全 ok；`go build ./...`、`go vet ./...` 干净；comment_policy 0 finding；gen_godoc 再生成仅 docs/api/github_com__SpellingDragon__tagent.md 变动；主仓 dotted-refs 绿（worktree `.git` 为文件时假红，findings 先例）。

## 回归红待裁（白名单外，未触碰、未勾 4.1）

clone 级全量回归唯一红：`tests/integration/execution/TestReceiptAndBackgroundStaySeparate`（第二 invocation 断言 8 条 ledger 含 2 条 "ok" admission receipts、settled body 不以 tool 角色）。归因硬证：**HEAD 纯净树 da2c8d5（零本单改动）把 decl 改 `MaxTokens: 32000` 同测同红、失败形态逐字节一致**（6 条：turn-1、assistant call turn、tool bg-fast/bg-slow、done-a、turn-2）——该断言钉的是 transcript 面形态（旧「无预算=不注册投影」专属路径），与投影面冻结契约天然冲突：
- `runtime/agent/request_assembly.go` RoleOf 契约原文「a replayed settled fact is history replayed as it stood, so it renders under the tool role its result fact answered a call with」——恢复历史渲染 tool 角色是冻结契约（request_fidelity 的 call-result pairing 负锚同向）；
- admission receipt（"ok"）产生于 `runtime/agent/loop.go` admissionText（内存 wire 确认），非提交事实（record 只提交 settled body），投影面无从恢复。

Q2 删除注册门后无预算会话形态必然收敛到预算会话形态（派单「设 32000 后恢复」恢复的正是投影面形态）。处置多解且代价大，**按白名单与验收面红线停止待裁**：
- (a) 推荐：扩白名单，把该测试 requests[2] 断言随 RoleOf 契约迁移为投影面形态（receipt 断言保留在 in-flight 面 requests[1]，该面修复后仍绿）；
- (b) admission receipt 落为提交事实并入投影（需动 runtime/agent/loop.go 与事实链语义，与「receipt 是 admission 确认非历史事实」设计冲突，G2 键语义风险）；
- (c) 维持现状留后续单随裁决处置。

## 自行决策与已知边界（申报）

- projectionBudget 零预算测法=材料全量/4+1 token：经同一装配通道保 Full 形态、防 1-token 边界误降级；未动 AssembleBudgetPrefix 冻结语义（budget<1 → ErrBudgetInvalid 不变）。
- live 侧折叠在 max_tokens=0 下仍不挂载/不触发（tools.go 挂载门、age_fold.go 零阈值、SessionFolds 冻结红线，均白名单外）；活/恢复折叠在 0 下不对称（restore 用 32000 兜底、live 永不折）——已知边界，非本单缺陷面。
- `api/config/config.go` MaxTokens 文档过期未改（白名单外）；委派 refs 预算零值语义独立不在 G7 呈现面。
- G2 键语义（SameFact）与 2.3 零值回填未触碰。

## 白名单

改动：facts.go、run.go、request.go、tests/integration/host/{history_restore_budget_test,event_integrity_test,mapped_sequence_and_environment_block_test}.go、openspec/changes/fidelity-and-recovery/**、docs/api/**。tests/integration/execution/** 未触碰（归因实验仅在 /tmp worktree 副本）。

# 4.2 + 4.3（G8 向量候选完整检索 + G9 完整请求预算，memory 簇合单）

## 结论

G8：vectorLeg 不再按 recent 预截断检索候选——候选完整读、完整打分排序后才按 `vector_top_k` 取最佳命中。G9：呈现决定改**条件重用**（事实基准不变且视图放得下才复用），预算收缩→瞬时降级（决策与 settle fact 不动），事实变化→重算并替换决策；整请求（含 noticeCard、recovery hint）全程硬约束 `≤ budget × 4` 字节。

## 红证（先于修复，症状逐字）

- G8 `go test ./memory/search -run '^TestVectorCandidateCompleteness$'` 修复前：`vector_top_k=1 answered []`（=proposal G8 行「vector_top_k=1 后零命中」）。
- G9 `go test ./memory/replay -run '^TestRequestBudgetEnforced$'` 修复前三子测红：`the request carried 200 bytes under a 4-byte budget`（=G9 行「200 字节内容在预算缩至 4 字节后仍完整保留」）、`second assembly = "yyy…" want … not the stale preview`（无条件重用 stale preview）、`the request carried 302 bytes under an 80-byte budget`（noticeCard 越预算）。修后四子测全 PASS。

## 实现要点

- `memory/search/store_searcher.go` vectorLeg：`opts.Limit = 0`（完整读候选，去掉读侧按 (Timestamp,Key) 的页界预截断），`vector_top_k` 改为打分排序后截取 `out = out[:s.vectorTopK]`；语义从「最近 n 条里打分」变为「全候选打分后取最佳 n」。
- `memory/replay/replay_budget.go`：
  - 新增 `settled map[event.EventID]string`：决策绑定时的事实原文基准（recordLocked 绑定、Append 补绑、replaceLocked 重绑），区分「预算变化」与「事实变化」；
  - 条件复用三分支：基准不变且 `len(view) <= room` → 复用 recorded view；基准不变放不下 → `belowForm(decision.Mode)` 瞬时降级（`settleNone`，决策/settle fact 不动，预算回升即复用原视图）；基准不符 → 全形态重算（`settleReplace`，覆盖决策 + settle 事实按身份去重落地）；
  - 硬约束：装配全程 `Σ len(Content) ≤ budget*budgetCharsPerToken`——recovery hint 经 `cutBytes` 收缩到 limit、noticeCard 收缩到剩余 room（字节真界、rune 安全），其余形态放不下即 skip；
  - 形态链抽为 `presentWithin`（full→ticket→summary→card，card 仅 TypeFeedback，room>0 必产出）。

## 自行决策与已知边界（申报）

- 「top_k=0 拒绝」负锚歧义：既有语义 0=腿不截断（config「0 takes the runtime default」、既有测试钉零调谐=无界），负调谐才具名拒绝 `ErrRetrievalTuning`。负锚按既有语义覆盖三者：空查询 `ErrQueryTextMissing`、负调谐 `ErrRetrievalTuning`、top_k=0 无界不截断。
- 瞬时降级不覆盖决策：预算收缩不改写 recorded decision 与 settle fact（保住「a recorded preview is never promoted back to the full text」，且多预算消费者并存互不棘轮）；只有事实变化才替换决策。
- noticeCard 在 room≤0（预算一字节不剩）时跳过：硬约束赢于「通知不丢弃」的未定义角。
- 事实变化测试用白盒接缝（直改 `projection.originals`）表达 fact 层替换原文；card 形态决策 Preview=标准 300 界 card、展示=收缩到 room 的瞬时视图。

## R23 适配（骨架断言改写，验收意图保留）

`TestStoreSearcherHonoursRetrievalTuning`：`vectorLimit != 3`（钉被修缺陷机制=读侧截断）→ `vectorLimit != 0`（读侧无界、打分后截取）；向量 fixture 改递减余弦使 top-3=最旧三候选；`len(result.Refs) == 3` 冻结断言不动。

## 三锚与绿证（可复跑）

- G8 正锚：`go test ./memory/search -run '^TestVectorCandidateCompleteness$' -count=1 -v` → 三子测 PASS（unbounded 命中旧相关记录 / top_k=1 仍命中 / 具名拒绝与零界）。
- G8 负锚：`the_named_refusals_and_the_zero_bound_stand`（ErrQueryTextMissing、ErrRetrievalTuning、top_k=0 无界）PASS。
- G8 消费者锚：`read.go:96 supply.searcher = search.NewStoreSearcher(...)` → `StoreSearcher.Search`（vectorLeg 在内，生产读取面）；`go test ./memory/... -run 'Vector' -count=3` 全 ok。
- G9 正锚：`go test ./memory/replay -run '^TestRequestBudgetEnforced$' -count=1 -v` → 四子测 PASS（200→4 字节真缩减且预算恢复复用原视图 / 事实变化重算 / 整请求含 noticeCard 与 hint ≤预算 / 0 预算具名拒绝）。
- G9 负锚：`a_non-positive_budget_is_refused_by_name`（`ErrBudgetInvalid`）PASS。
- G9 消费者锚：正式入口 `request.go:701 projectedSegment → projection.AssembleBudgetPrefix`（Boot→Invoke 真实请求）；`go test ./tests/integration/host -run 'Budget|Projection'` → `TestRequestCarriesProjectedBudget`、`TestProjectionAssemblyServesTheThreeUses`、`TestProjectionCarriesTheCommittedSequence` 全 PASS。
- 定向：`go test ./memory/... ./tests/integration/host -run 'Vector|Budget|FactFidelity' -count=3` → 全 ok。
- 回归：`go test ./memory/... ./tests/... -count=1` → memory 簇 + tests/integration/history 全 ok；`go build ./...`、`go vet ./...` 干净；`bash scripts/lint.sh` → `lint: ok`（comment_policy 0 findings、gen_godoc 一致）。

## 白名单

全部改动在白名单内：memory/search/**、memory/replay/**、openspec/changes/fidelity-and-recovery/**、docs/api/**。

# 3.2（G6b 唤醒统一：SettleDriver retained 降解为 mailbox 统一待消费，Q6/Q8 裁定）

## 结论

retained/firing/rearm 三态内存队列删除，通知待消费责任归 mailbox 统一账：通知本体持久化于 durable owed（task.CommitRecord.Outbox）、消费动作=启动回合 Claim→drive→Consume 同账（durable consume 回执为完成证据）、被停/失败回合 Release 回 pending 由下次 wake 重试（回调重试=事件驱动非定时轮询）、未消费通知跨 Stop 与重启保留。「还有没有工作」由 mailbox 一处判断（Unconsumed），driver 只做 pump。

## 红证（先于修复，症状逐字）

- evidence/3-2-wake-unified-red.txt：`entry model turns = 3, want the delegating decision beside exactly one follow-up turn`（①重复 fire 双回合，retained 语义）；`timed out waiting for the resumed follow-up to deliver its answer`（②重启后续跑不起来，内存队列丢）；`timed out waiting for both retained notices to resume`（③Stop 清队=丢）。
- evidence/3-2-consume-repeat-red.txt：`repeat the consumption = <nil>, want … refusing the repeat by name`（重复 Consume 幂等通过，无具名拒绝）。
- ②③ 排查期根因铁证（DBG 诊断行已删）：`DBG receipt task-2:1:settled:0` 落在 `turn end #2 terminal=cancelled` 之后——被取消的随动回合 driveLocked 返回 err==nil（Terminal=KindCancelled 无错误），fireNoticeTurn 原样 return nil，driveSeat 误判成功落消费回执，boot2 账空。

## 实现要点

- `settle_turn.go`：retained/firing/rearm/userAddressOf 全删；pump 单 goroutine 从 `wake.Unconsumed()` 读待消费席位，Claim 独占后 `driveSeat`（lookup settle fact→fire→Consume）；同轮 `tried` map 防重复尝试；失败/被停回合 `releaseSeat`（Release 回 pending）；Consume 用 `context.WithoutCancel` 写回执（完成证据不停机丢）。
- `runtime/state/mailbox.go`：Consume 具名拒绝（consumed→`ErrClaimSettled`、非 claimed→`ErrNotClaimed`）；Release（consumed→`ErrClaimSettled`、pending→`ErrNotClaimed`）；durableConsumption 接口（回执落盘后才改内存态）。
- `runtime/state/memory.go`：WorkSeat/Unconsumed/RestoreOwed/restoreSeat（启动时 durable owed 折回席位）。
- `runtime/state/durable.go`：OwedNotices（load 自 record.Outbox 建 owed、按 consume 回执剥除）——回执族复用既有 consume: 语义，**未扩持久语义、无新回执族**（任务书申报问的答复）。
- `host.go`（白名单外触点，申报）：fireNoticeTurn 消费完成点=回合真正交付（`turn.Terminal != model.KindCompleted` → `ErrNoticeTurnIncomplete`，不落回执）。

## 删并三问（3.2 收尾必答）

1. **删了什么状态**：SettleDriver.retained（完整 event.Event 队列）、firing、rearm 三态内存队列与 Stop 清队点、userAddressOf 路由判门。通知本体唯一承载=durable owed（Outbox 持久化）+ mailbox 三态（pending/claimed/consumed）。
2. **责任归谁**：mailbox 一处判「还有没有工作」（Unconsumed 唯一问询点）；Claim 独占保证一通知一回合；Consume+durable 回执=完成证据（不变量3 的「完成证据」面）；Release=未完成回合的可恢复未结状态（不变量3 的另一面）；driver 只做 pump。
3. **验收仍成立**：三锚全绿（重复 fire 不双回合/跨重启可恢复/Stop 保留）+ 负锚全绿（已消费不重放、重复 Consume→`ErrClaimSettled`），具名测试见下。

## 自行决策与已知边界（申报）

- 消费完成点解读：裁定「Claim→drive→Consume 同账」中 drive 完成=回合真正交付（Terminal==KindCompleted）；被停/失败回合不落回执、Release 重试——与 durable consume 语义一致（「the receipt MUST follow the handover, and a stop between the two leaves the notification owed」）。
- fire 失败重试=下次 wake（事件驱动），未引入定时轮询。
- wrapper 包装任务（task-1）的 sink 同步 Consume（bookNotice→Backflow→Consume）是 G1-G4 既有语义，非本单面，不动。
- `runtime/state/durable_test.go` 冻结断言改造（申报）：重复消费原期待幂等通过→改为期待 `ErrClaimSettled`（与任务书负锚一致，R23 骨架断言改写、验收意图保留）。
- userAddressOf 门随 retained 删除：fire 直接用 seat.Address（路由冻结于 notice 本体）。

## 三锚与绿证（可复跑）

- 红→绿锚：`go test ./tests/integration/host -run '^TestSettleWakeUnified$' -count=1` → 4 子测 PASS（重复 fire 恒一回合/重启续跑 0.23s/Stop 保留 0.24s/不重放 guard）；红证见 evidence/3-2-wake-unified-red.txt。
- driver 级三锚：`go test ./tests/integration/execution -run '^TestSettleWakeAccount$' -count=1` → 3 子测 PASS（真实 durable 账：Write 工具一次报错重试成功）。
- 负锚：`a consumed notice never replays and the repeated consumption is refused by name`（`state.ErrClaimSettled`）+ host 级 `a consumed notice never replays`（guard 回合数 0）。
- 消费者锚：正式入口 `tagent.Boot(configPath, WithSender)`→`Host.Invoke` 委派→deliver（TrackDelivery+AcceptNotice）→settle fact→Wake→driver→fireNoticeTurn→driveLocked 真回合→Consume（TestSettleWakeUnified 全链实测；TestSettleWakeAccount 经 ledger.OpenTier→NewDurableCommit→Commit 真账）。
- 门禁：`go test ./tests/integration/execution ./tests/integration/host ./runtime/... -count=1` 与 `-race` 一次（结果见战报/提交说明）；`go build ./...`、`go vet ./...` 干净；`./scripts/lint.sh` 全绿（comment_policy 0 findings）；症状全家 `go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom'` → ok 12.4s；clone 级见提交后复验。

## 回归红（既有，归因 G7，非本单面）

`tests/integration/execution/TestReceiptAndBackgroundStaySeparate`：/tmp worktree 检出 0f96892（无本单改动）同测红、失败形态逐字一致（`second invocation messages = 6 …`），5f78bdf（G7 前）通过——G7 提交引入的既有红，未触碰、待裁面同 4.1 节。

## 白名单

白名单内：settle_turn.go、runtime/state/{mailbox,memory,durable}.go、tests/integration/{host,execution}/**、openspec/changes/fidelity-and-recovery/**。白名单外触点（申报）：host.go（fireNoticeTurn 终态检查与 doc 合并、Boot 恢复后 Wake）、runtime/state/durable_test.go（冻结断言改造）。

## 清障裁定（编排者三项已批裁定的落地记录）

来源：fidelity-and-recovery 清障单，HEAD d248d05；批准依据=architectural-subtraction D3.4
（改语义断言=编排者批准+登记）/ Q16；掩盖缺陷的改断言为禁止项，三裁定落地均逐行登记、
负锚意图逐字保留。F4（归档+导览）留授权，不在本清障单内。

### 裁定① golden 合同锚随语义演进（登记 evidence/golden-migration.txt）
- 三个合同锚（01.1 NoticeIdentity / 01.7 CLIAdapter / 03.9 Restart）随 G6b（mailbox 统一）
  与 G7（MaxTokens 注册门删除、one face per fact）合法修订断言形态；golden JSON 零改动、
  0 个场景删除、0 行无法等价迁移。逐行修订登记=R1/R2/R3（旧行为→新行为→裁定依据）。
- F1 断言口径改为「golden 修订仅限本变更声明的语义变更（逐行登记）」（tasks.md 已更新）。
- 可回滚点：三处断言/读出面修改全在 tests/integration/{execution,history} 与
  examples/cli-host/main.go，revert 三文件即回到修订前形态，无数据迁移面。

### 裁定② long 干净重采（登记 F2-gates 清障段 + /tmp/tagent-clear-evidence）
- gitlink examples/wechat-bot/skills/interview/agent-interview-helper 指针未被改动
  （888fc16=888fc16，porcelain=v2 可复核）；内部 untracked 杂项（plan.md/question.md）
  按裁定「untracked 杂项不动」留置。干净树长跑六片（seed 1/7/42 × window 256/32768，
  --repeat 1 --timeout 1200，串行防 2 核互抢假红）。
- 可回滚点：纯证据采集，无代码改动，无需回滚；证据目录 /tmp/tagent-clear-evidence。

### 裁定③ mutations registry anchor 迁移（登记 evidence/mutation-anchor-migration.txt）
- MUT-EVID-02/03/04 共 4 处失效锚迁到 G6b/G7 语义后的等价新锚（M1 host.go turn 形态、
  M2 settle_turn.go busy 丢通知注入形态重构为 4 补丁组、M3 request.go 全地址键+run 句柄）；
  缺陷主张与 expect_assertion 全部冻结不变；其余 8 mutation 未动。
- 可回滚点：仅 scripts/check_event_integrity_mutations.py 的 registry 行，revert 即回
  旧锚形态；产品面零写入（写树纪律 R26 不变）。

### 清障实测数字（2026-10-27，逐条可复跑，完整退出码 evidence/F2-gates.txt 清障段）
- 裁定①：`python3 scripts/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-short --repeat 10 --out /tmp/tagent-clear-evidence/event-short` → passed=10/10 全 verdict=match（F2 期 0/10 → 清障后全绿）；golden 逐行登记 3 行（R1/R2/R3）、0 行无法等价迁移、0 场景删除、golden JSON 零改动（evidence/golden-migration.txt）。
- 裁定②：干净树六片（seed 1/7/42 × window 256/32768）6/6 verdict=fail——smoke_test.go:2894 settle notifications=51/50/50/51/50/51 每 47（want 47）；受控重跑 1 片仍 fail（49/47），累计 7/7 同向非 flake；「六片 match」目标落空，待裁。
- 裁定③：`python3 scripts/check_event_integrity_mutations.py --all --json` → 11/11 killed（全 named_assertion 命中、control_tree_green=true）；registry 迁移 M1/M2/M3 + M4 注入补全（M4 超裁定③字面、已申报）；静态锚复核 24 处 patch 锚逐处 hits=1（evidence/mutation-anchor-migration.txt）。
- 收尾门禁：三模块 build/vet/test -short、lint、selftests（Ran 233 tests / OK）、check-openspec（本单 ✓）、症状 job（ok wechat-bot 12.944s）全 RC=0；bundle 总门 RC=1（17 证据结构过：short=10/long=6/mutation=1，红源=long 透传）。
- 证据根：/tmp/tagent-clear-evidence（run-long.sh / event-short / event-long（含 rerun 片）/ event-integrity-mutations / bundle / gates.log）。

### 裁定②终清障归因核实（2026-10-27 二次派发，结论：duplicate≠0 → 按裁定停报）
- 编排者裁定②假设「超发=settled 任务多（合法增长）」，并预设 duplicate=0。实测证伪：
  13/13 超发 run 全部 notif_req = 47 + dup_count，unique taskids 恒 47（零新增任务）；
  重复对同 TaskID 同正文（task-58/87/95/164… 各 ×2，请求序号相邻），是同一结算通知
  双驱动（重复投递/G6b 违约签名），非新增结算。missing 恒 0。
- 3/4 重复对紧邻 rebind（重启代际边界）后 4 个请求内，指向恢复-Wake 与直接 fire 双触发。
- 测试级 TestSettleWakeUnified 4/4 PASS（含 a_repeated_fire…）而长程生产路径双发 =
  消费者锚缺口（R30 签名）。裁定引的 3-2-wake-unified-red.txt 实为红证（非绿证）。
- 处置：smoke_test.go:2894 未改（修订=掩盖真缺陷，禁止项）；六片重采/全绿采集未执行
  （停报分支）。逐条数据与复跑命令：evidence/duplicate-settle-attribution.txt（未提交，待裁）。
- 待裁决：修 G6b/G7 双触发（dup=0 + notif=47 为转绿验收）或重裁前提——见证据文件 §七。

## 缺陷修复：双驱动（c2 同屏障 + c1 前置 attempt + e 通知身份去分域，2026-10-27）

来源：evidence/dual-drive-attribution.txt（460 行归因）+ 上级 S1–S3 裁定。基线 HEAD b0af340。
修复证据（代码锚/三锚/回滚即红/六片逐片数字/门禁 RC/flake 台账）：evidence/F3b-dual-drive-fix.txt。

### 归因结论（本单只按此三处落地）

- E1 并发 claim 原子：64 并发 1 winner，`ErrClaimConflict` 63——重复不是 TOCTOU，不加 CAS。
- E2 无双 Accept：产品 AcceptNotice 三处入库点同键幂等；38/38 重复对正文逐字相同，absent-wildcard 未参与（d 单开）。
- E3 唯一 fence = 消费回执；窗口 = 整个回合；38/38 重复只落在该代最后一轮（round%10==9，紧跟 restart），mid-generation 363 个结算 0 重复。
- 4.5 E2f：`NoticeFactKey(invocationID, notice)` 以 invocation 分域 → 同通知换进程算出不同 EventID → transcript 追加第二条相同指令。

### 三裁定落地

- **S1（at-least-once 保留 + 幂等兑现到「回合驱动恰一次」）**：仍允许跨进程重放（in-flight 的 owed 照旧驱动），
  但「已落终态的回合」不再驱动第二个回合，只补回执；`TestSettleWakeUnified/an_unconsumed_notice_resumes_across_a_restart`
  的 `entryCalls>=3` 合法面原样保留。
- **S2（跨进程双驱动 = 缺陷）**：Boot 的 RestoreOwed+Wake 读 durable attempt（c1）与消费回执（c2）两个 fence，
  同一通知跨进程只驱动恰一次。
- **S3（dup=0 / notif=47 / transcript 无重复）**：03.11 长程六片 6/6 verdict=match，每片 notif=47、dup=0、
  action 1728/1728；`TestSettleWakeSingleDrive` ①重启后 entryCalls/followUps 双冻结且不补发答复。

### 窗口收窄语义（与归因 4.3 的对齐与差额）

- 旧窗口 = 整个回合（「通知已交给回合」→「回执落盘」）：崩在窗口内 → 重启再驱动 → 用户看见第二次 ack / 模型预算双花。
- 新窗口 = **回合终态落定那一刻的同事务边界**（host.driveLocked: run.Invoke 返回 → settleTerminal 落 attempt 终态 +
  消费回执 → 才 deliverFresh）：终态与回执同屏障，先于任何返回与下一次调度。
- 收窄后的代价（如实登记）：崩溃若落在「终态/回执已落」与「答复已交出」之间，重启读到 landed completed →
  补回执、**不再重驱**，这一段是 at-most-once（答案丢失而非重复）。这是 S1「重放前先查 in-flight 标记」在本实现的
  取舍方向：宁可这一窄段不重交，也不把「已完成回合的答案」重投第二遍。
- 反向窗口（答复已交出、回执未落）在新形态里不再存在：回执落盘先于交付，故不会出现「已交付→重放→再交付」。

### e 的身份改动（不改 NoticeFactKey 签名）

- follow-up 回合的 invocation 由通知本体派生：`noticeInvocation(notice) = "notice:" + notice.FactKey()`
  （task:round:kind:seq，无 invocation 计数、无 instance）；事实键仍是 `NoticeFactKey(invocationID, notice)` 的形状。
- 同通知重放 → 同键 → `CommitToSession` 幂等命中 → transcript 只有一条指令（回滚 e → notice 事实 2 条，实测红）。
- EVID 家族合同锚未改写：断言读事实键本体与 meta（MetaKeyNoticeKind/TaskID/TriggerSource），不读 invocation 前缀；
  `settleDeliveryKey(notice)` 按四元组寻回通知事实，本就与 invocation 无关。故不需要裁定①的逐行登记程序，
  且 event-short 十轮 9/10 match（唯一红是既有 03.6 FoldControl，基线 worktree 复现同一句）。

### 回滚即红（三点，逐条原文见 F3b-ddual-drive-fix.txt §三）

- 还原 c2 → `follow-up turns after the restart = 2, want the 1 the landed attempt closes`
- 还原 c1 → `follow-up turns beside a landed attempt = 1, want none`
- 还原 e → `notice facts after the resume = 2, want the one instruction`

### 未纳入本单（各单开）

- d) absent-wildcard 收紧（归因 4.4：0/38，与本病无关）。
- 「重启重放委派 admission 冲突 → ack 缺」（`ErrSpecConflict`：Spec 带进程级 InvocationID/CallID）——
  03.11 seed42/window32768 首跑即此形态，受控重跑转绿；基线期 10 片命中同一行（含 G6b 前），判既有面。
- 03.6 FoldControl/negative（重启后重折重铸 synthetic 键）——基线 worktree 同样红，归档 EVID-05 同一句。
