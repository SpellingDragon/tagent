# 09.7-review · 块2｜02 state／execution 双域终态回溯

> **审查对象**：`runtime-rebuild` 下 `01-state-binding`（8 叶）＋`02-execution`（15 叶），共 **23 孙任务**。
> **审查方式**：**只读**。逐叶比对 `task-graph.json`（verification／evidence／notes）、两域 `tasks.md` 行原文、`midterm-review-response.md` §二补差合同与 §二·1 具名负锚表、`prototype/` 原型语义、实现落点 `state/`·`agent/`·`task/`·`run*` 族。
> **数字来源纪律**：凡标「**自报**」者＝`task-graph.json` 中该叶 `evidence` 记录的计数（本次未复跑）；凡标「**实测**」者＝本次会话 grep／read／checker 真实输出，命令随文附上。**未运行任何锚测试**（本轮是"对计划逐孙任务拆"的回溯审查，不重复执行域内测试）；因此本报告的判据是**落点结构 + 锚存在性 + 证据自洽性**，不等于测试被重跑过。
> **三轴**：`A` ＝ 原型／规划要求且已实现；`B` ＝ 原型／规划要求当前未实现（缺口）；`C` ＝ 新增演化（原型与规划都没有的新面）。
> **红线执行**：本文**不给 PASS 汇总**。逐叶判据与缺口并列；凡残留、陈旧、不可复跑、口径裁断项一律进 §5，不静默通过。

---

## §1 逐孙任务表（23 行）

> 表格「落点」列的 `file:line` 均为**本次实测**（`grep -n`／`sed -n` 定位，非记忆）；「锚证据」列区分正锚／负锚，并标注自报或实测。

| 叶 | 要求（tasks.md 原文要点） | 落点（file:line，实测） | 轴 | 锚证据 |
|---|---|---|---|---|
| **01.P0.1** | config 纯声明数据骨架：`Declaration`/`AgentSpec`/文件引用 + `Load`／指纹签名（零解析、零指纹逻辑） | `config/config.go`、`config/fingerprint.go`、`config/skeleton_assert.go` | A（骨架层，已被 01.1 覆盖） | `skeletonA-01p01`：`GOPROXY=off go build ./...`／`go vet ./...`／`gofmt -l <pkg>`（空）／`comment_policy -no-baseline` 0 findings，exit 全 0（自报）。**命令含 `<pkg>` 占位符，不可复跑**（G5） |
| **01.P0.2** | state 数据/接口骨架：`Store`/`Session`/`AgentBinding`/`StatePlan`/`OrgPlan`/发布记录/借用/`NoticePort`（零逻辑） | `state/store.go`、`state/session.go`、`state/binding.go`、`state/skeleton_assert.go` | A＋**B**（陈旧文档残留） | `skeletonC-01p02` 同上四项 exit 0（自报）。**`state/store.go:10` 至今写 "This is the phase 1 skeleton: it declares shapes and holds no behaviour"**，与本域 8 叶全部落实现矛盾 → G4 |
| **01.1** | config 严格解析、适用默认值、纯组织声明规范化指纹 | `config/source.go`、`config/defaults.go`、`config/clone.go`、`config/fingerprint.go`；测 `tests/integration/config/config_contract_test.go:21` | A | 正锚 `TestConfigurationContract`（`config_contract_test.go:21`，本次 grep 命中）。自报 **281 pass / 0 fail**，命令 `go test TestConfigurationContract -count=10`——**缺包路径与 `-json`，不可复跑**（G5） |
| **01.2** | 建立 agent/session、可信身份与投递/读取授权（不猜默认来源） | `state/authority.go`（`GrantRead`/`CheckRead`）、`state/session.go:13 Session`、`state/memory.go`、`state/mailbox.go`；消费站点 `run_invoke.go:50 GrantRead` | A＋**C**（读授权为新增演化：原型无授权模型） | 正锚 `TestSessionAuthority`（`tests/integration/state/session_authority_test.go:23`）。自报 61 pass / 0 fail；**evidence 命令缺 `-race`**，与 tasks.md 行内验证（`go test -race ... -count=10 -json`）不一致 → G5 |
| **01.3** | mailbox 接纳/认领/消费完成**分别记账** ＋ 同 session 执行占用（接纳不阻塞慢模型） | `state/mailbox.go:59 AcceptNotice`／`111 appendEntry`（赋 `AcceptSeq`）／`176 Claim`／`201 Consume`／`236 Reclaim`；`state/occupancy.go:61 Occupy`／`81 Vacate`／`98 Slot`／`116 Withdraw`／`134 Withdrawn`；`state/session.go:30 Consumed`（最长连续完成前缀，不越空洞） | A＋**C**（三时点＋空洞语义为新增：原型 `Next` 只做整批取走，`prototype/state.go:117`） | 正锚 `TestMailboxConsumption`（`mailbox_consumption_test.go:19`）。自报 41 pass / 0 fail，**此叶 evidence 带 `-race`**（口径正例） |
| **01.4** | 组合根准备原生模型/工具/插件活绑定，state 持有并按代借出（候选失败逐项释放） | `run.go:154 Assemble`（**`:163 ErrGovernanceMissing`**）；`state/binding.go:358 Hold`／`383 Borrow`／`410 BorrowDerived`／`468 Release`／`189 BindingRef`；`state/retire.go:84 Publisher.Borrow`（借用即带退役排空）；`run.go:213 NewLoop(r.publisher,…)`；`prepare.go:136 registerNativeTools`→`179 admitTools`；`read.go:144-147 Published()` 读当前代 | A＋**C**；M1 三项补差逐条可指认 | 正锚 `TestStateOwnsPipeline`（`state_owns_pipeline_test.go:75`）**40 pass / 0 fail**；**负锚** `TestFormalAssemblyRequiresGovernance`（`tests/integration/host/formal_assembly_requires_governance_test.go:57`）**40 pass / 0 fail**——3 子测：无治理 `ErrGovernanceMissing` 且 `run==nil`／`rejectPolicy`→`state.ErrCheckRefused`／`passPolicy`→`Invoke` 至 `KindCompleted`（本次已读全文：`tagent.Assemble` 正式入口、`errors.Is` 具名断言，**非手工补线**） |
| **01.5** | 组织发布、基线复核、同内容不换代、在途旧绑定/资源退役 | `state/publish.go:261 Publish`／`272 Rollback`／`287 publishLocked`／**`348 evaluate`**／**`385 commitRecord`**（evaluate 在 commitRecord 之前＝"缺必需检查即拒"的结构证据） | A＋**C**（发布前置契约、`Exempt` 须 reason+scope 为新增） | 正锚 `TestGenerationPublication`（`generation_publication_test.go:86`）10/10；**负锚** `TestFormalPublishRefusesMissingEvidence`（`host/formal_publish_refuses_missing_evidence_test.go:78`）10/10（缺 eval／零裁决均 `ErrRequiredCheckMissing`、旧代不变、代记账无新记录；变异负控：门固定通过必红）。**evidence 明示 `state/publish.go` 零改动** → 该叶属**证据型完成**（以负锚证明既有实现已对），非改动型完成 |
| **01.6** | prompt/skill 直接热读、结构变更与回滚分路、错误原因链保留 | `state/filesource.go`（`Fixate` 请求边界逐文件新读 + `contentVersion` 独立哈希）、`state/channels.go:193 Route`／`:203 ErrStructuralViaFile`／`226 Router`；`run_request.go:255 requestBoundary`→`BoundaryView.Messages`；`agent/loop.go:434-437` 边界消息前置拼入 `model.Request` | A＋**C** | 正锚 `TestFileAndStructuralChange`（`file_and_structural_change_test.go:151`）10/10；**负锚** `TestFileChangesReachNextRequest`（`host/request_boundary_test.go:28`）10/10（文件改动后下一请求 `Request` 携带新版本正文，假 provider 断言）。**本域"正文真进 GenerateContent"的闭环点** |
| **02.P0.1** | event 身份/载荷骨架：9 个 ID 类型、`Address`、`Event` 载荷、`Type*`/`MetaKey*` 常量 | `event/types.go`、`event/meta.go`、`event/skeleton_assert.go` | A（骨架层，已被 02.1 覆盖） | `skeletonA-02p01`：build/vet/gofmt/comment_policy exit 全 0（自报）。**命令占位符，不可复跑**（G5） |
| **02.P0.2** | task 骨架：`Spec`/`Status`/`AdmissionReceipt`/**三层结果**/`Notice`/`Registry`/`Manager`/`Commit` 端口签名 | `task/spec.go`、`task/commit.go`、`task/registry.go`、`task/skeleton_assert.go` | A（骨架层） | `skeletonB-02p02` 同上（自报） |
| **02.P0.3** | model 骨架：原生类型薄别名、单一终局归约入口签名、`kind` 枚举 | `model/aliases.go`、`model/terminal.go`、`model/skeleton_assert.go` | A（骨架层） | `skeletonA-02p03` 同上（自报） |
| **02.P0.4** | agent 骨架：`Agent` 接口、invocation 调度类型、委派/关闭签名 | `agent/agent.go`、**`agent/invocation.go`**、`agent/delegate.go`、`agent/skeleton_assert.go` | A＋**B（残留）** | `skeletonD-02p04` 五项 exit 全 0（含 `check_runtime_architecture --check-policy` ok，自报）。**但 `agent/invocation.go:45 Aggregate()` 今日仍 `return errNotImplemented`，且 `Invocation.Slot DecisionSlot`（`:34`）全树零写零读** → G1 |
| **02.1** | 事件身份与原生载荷、task 提交/消费端口（内存载体 + 持久适配合同），**三层结果不合并单 enum** | `event/types.go`、`event/meta.go`、`task/commit.go`、`task/admission.go`；`state/durable.go:FactLog` | A＋**C** | 正锚 `TestIdentityAndCommitContract`（`identity_commit_contract_test.go:28`）。自报 61 pass / 0 fail；**命令不可复跑**（G5） |
| **02.2** | task 受理、限额、取消、幂等与可解释义务明细（受理成功 ≠ 义务结清） | `task/registry.go`（`Registry`／`Obligations{Count,Owner,Blocked,ExitReason,Open}`／`ObligationKind`）、`task/lifecycle.go`、`task/cancel.go`、`task/manager.go:52 Admit`／`:58 Obligations` | A＋**C** | 正锚 `TestTaskLifecycle`（`task_lifecycle_test.go:21`）。自报 81 pass / 0 fail；**evidence 命令缺 `-race`**（tasks.md 行要求 `-race`）→ G5 |
| **02.3** | 注册期原生 wrapper、审批等待/拒绝、**一次受理回执**、后台结果**独立回流** | `task/wrapper.go:165 Call`（`:166 CallFrameFrom` 缺失→`ErrCallFrameMissing`）／`202 ruling`／`213 run`（Admit→Dedup 直返→`go execute`）／`237 hold`／`269 deny`／`285 execute`（`context.WithoutCancel`）；`run_tool.go:98 wrap`→`task.Wrap(...WithApprover)`；`run_tool.go:232 denyWithoutApprover`（strict 无 approver 拒）；`prepare.go:139/151/165` **contributed／native set／loader 三族同经 `admitTools`→`wrap`** | A＋**C** | 正锚 `TestNativeToolAdmission`（`native_tool_admission_test.go:23`）**70 pass / 0 fail**；**负锚** `TestFormalWrapperAdmission`（`capabilities/formal_wrapper_admission_test.go:53`）**30 pass / 0 fail**（strict 无 approver 拒绝零执行零 task；有 approver 时三族真实 Receipt 结算原文） |
| **02.4** | 模型单一 channel/iterator 终局处理（usage 尾包、错误/取消、生产者结束） | `model/terminal.go`（`Kind` 5 值且 `Unknown` 为零值、`Signal` 6 态、`Reducer.Observe/ProducerDone/Cancel/Outcome`、`ReduceChannel`/`ReduceSeq`：nil 流→`KindFailed`/`AlertNilStream`，cancel→`KindCancelled`）、`model/usage.go`；`agent/loop.go` 只调用该单一归约 | A | 正锚 `TestModelTerminalProtocol`（`model_terminal_protocol_test.go:23`）。自报 121 pass / 0 fail；**命令不可复跑**（G5） |
| **02.5** | 根包最小公共装配入口驱动 Agent、从 state 借**同代**绑定、按 invocation 处理事件 | `run.go:154 Assemble`／`run_invoke.go:33 Invoke`／`:96 Close`；`agent/loop.go:131 Drive`（`:134` 注册 drive 级 producer）／`400 decide`／`488 dispatch`；`:664 aggregateReady`；`:721 paired`／`:856 admissionText`／`:868 observationOf` | A＋**C** | 正锚 `TestNativeConversationRoundTrip`（`native_conversation_roundtrip_test.go:49`）**41 pass**；**负锚** `TestReceiptAndBackgroundStaySeparate`（`receipt_background_separation_test.go:28`）**11 pass**（慢工具在途后续已受理 Receipt 可见／二次 Invoke 含前次历史／mailbox `DetailRetired` 记账／receipt 无结算正文无前缀） |
| **02.6** | 插件执行前**恰一次**、最终参数执行、denied-only 可收束、提交后呈现不丢事实 | `agent/loop.go:488-557`（`plugin.NewPipeline(decision.Binding.Plugins)`；`Before` 每 call 在受理前、`After` 只对已提交事实）；`run_fact.go:197 takePresentation`／`:207 Presentations`；`run_tool.go:203 policyRuling`（`Governor`→`Approver`）；`run_tool.go:79-86` 装配 `observePlugin` | A＋**C** | 正锚 `TestPluginPhaseOrdering`（`plugin_phase_ordering_test.go:29`）**80 pass / 0 fail**；**负锚** `TestFormalLoopEnforcesPolicy`（`capabilities/formal_loop_enforces_policy_test.go:27`）**30 pass / 0 fail**（strict 违规正式 Loop 零执行具名／judge 恰一次／After 缺口经 `Run.Presentations` 可读；**变异负控：摘 CallPass 接线必红**） |
| **02.7** | 子委派的调用关联、进度与终局（复用应用选择的目标 session） | `agent/delegate.go:213 Delegate`（先 `BorrowDerived` 钉代→`Admit`，非 Fresh/拒则 `withdrawBooking`+`Release`）／`:504/:512 withdrawBooking`（只撤本次预登记）／`:478 TurnBorrow`+`:534/:545`（强制同代）／`:283 Observe`／`:320 Complete`／`:376 Reconcile`／`:394 land`／`:435 View`／`:454 OwnerOf`／`:467 Pending` | A（**M 回归重验**，实现零适配） | 正锚 `TestDelegationCompletion`（`delegation_completion_test.go:26`）**40 pass / 0 fail**；`run_id mreg2-02.7`，evidence 明示"上游 02.6 收口后重跑，实现不判错零适配" |
| **02.8** | Handoff 目标实例与通知重试（已开始者固定旧目标【源实例】，未来通知转新目标【目标实例】；关闭未交付保留） | `task/handoff.go:70 handoffPrefix`／`:77 handoffMutation`（`handoff:<slot>:<json>`）／`:88 handoffTargetOf`（唯一解析点）／`:114 applyHandoff`（live 与 fold 同一函数）／`:142 CloseInstance`／`:153 Retained`／`:175 RecoveryMaterial`；`task/manager.go:88 Handoff`／**`:192 deliveryContext`（从持久 `Spec` 重建 `CallFrame`）**；`task/handoff.go:239 IssueNotice`（冻结 envelope） | A＋**C**（持久 mutation 编码为新增） | 正锚 `TestHandoffFutureNotifications`（`handoff_future_notifications_test.go:31`）**70 pass**；**负锚** `TestHandoffRoutingSurvivesRestart`（`handoff_routing_survives_restart_test.go:46`）**10/10**（重启后新通知投新实例／旧通知冻结 envelope 不改投／归属取持久 Spec 非运输 ctx／重复运输幂等） |
| **02.9** | 关闭/排空/**真实停止确认**（未收束义务与非合作 producer 不得伪称结束） | `agent/shutdown.go:135 Watch`／`:153 Track`／`:166 Admit`（refusing 即拒、只计 Fresh）／`:220 Close`／`:256 drain`／**`:284 settlePass`**（inflight>0 不结；Flush+Reconcile+Obligations；producers 轮询；`Reconciler.Pending`）／`:336 unsettled`（含 `reasonProducerNotStopped`）；`run_invoke.go:96 Close`／`:135 settleOwed`；`run_tool.go:146 toolProduction.Stopped`／`:178 toolProducer.Call`（`CallFrameFrom`→`admission.Lookup`→`Track/Watch`→执行→`stop()`）；`agent/loop.go:134`+`:441` producer 登记 | A＋**C** | 正锚 `TestCloseDrainsObligations`（`close_drains_obligations_test.go:31`）**60 pass / 0 fail**；**负锚** `TestCloseRefusesWhileModelInFlight`（`host/close_refuses_while_model_in_flight_test.go:27`）**50 pass / 0 fail**——4 子测（模型在途：2 条 `Unsettled` 且无 task／cancel 后 `tool.finished=false` 且 drain 不收敛／producer 恒 false 时 Close 200ms 不返回、解锁后 `Drained`／排空前拒新且排空后仍拒）。本次已读全文：全部走 `tagent.Assemble`→`Invoke`→`Close` 正式链路 |
| **02.21** | ctx 契约表 + 类型化 accessor 入签名（裸 `ctx.Value` = 0），`Governor.Judge/JudgePass` 二合一 | `task/wrapper.go:53 CallFrameFrom`（**`:54` 是全树唯一裸读之一**）、`plugin/policy.go:77 CallPassFrom`（**另一处**）、`contracts.md` 缝表、`plugin/phases.go`/`plugin/policy.go` 签名 | A＋**C**（契约表＋"MUST NOT 新增第三缝"为新增） | 静态锚：`python3 scripts/check_concept_budget.py --rule ctx-bare` → **实测** `findings.ctx-bare: 2`，两处 warn 分别点名 `plugin/policy.go` 与 `task/wrapper.go`（各 1，`baselined at 1`），`RESULT: ok`；交叉实测 `grep -rn "ctx.Value(" --include=*.go . \| grep -v _test` → **仅** `plugin/policy.go:77`、`task/wrapper.go:54`（都在 accessor 实现体内）。级联锚 `TestFormalWrapperAdmission` |
| **02.22** | agent 端口面审视合并（**≤5 接口**） | `agent/agent.go:32 Agent`、`agent/delegate.go:101 DelegationTasks`、`agent/loop.go:47 Recorder`、`agent/loop.go:882 HistoryFace`、`agent/shutdown.go:52 Reconciler`；`Producer`/`ProducerWatch` 删并为 `agent/shutdown.go:135 Watch(taskID, func() bool)` | A＋**C** | 静态锚：`--rule iface-methods` **实测** `RESULT: ok`；`grep -rn "^type .* interface {" agent/` **实测恰 5 条**（上列）。生产装配实测：`run.go:212-223` 五端口全接线（`SetHistory(r)`／`SetWatch`／`SetPresentations`／`SetReconciler(r.delegations)`／`Recorder` 为 `cfg.recorder != nil` 时条件接线）。回归锚 `TestNativeConversationRoundTrip`／`TestFormalInvokeProducesTrainingRecords`／`TestCloseRefusesWhileModelInFlight`／`TestFormalChainSealsCapture` |

### 表注（逐条可复验）

1. **23 叶 status 全 `done`**（`task-graph.json`），8 叶带 M 重开/回归标注（01.4/01.5/01.6/02.3/02.5/02.6/02.8/02.9＋02.7），且这 8 叶的 `verification` 均有**第二行负向锚**、`evidence.commands` 均**点名该负锚测试**——符合 `midterm-review-response.md` §二·1 的机器硬门（锚存在＋evidence 点名）。本次 grep 逐个确认 22 个具名测试函数**全部存在**且文件:行可定位。
2. **A 轴无空缺，但"完成类型"分两种**：01.5 是**证据型完成**（`state/publish.go` 零改动，靠负锚证明既有实现已达合同）；01.4/01.6/02.3/02.5/02.6/02.8/02.9 是**补差型完成**（改动了 `run.go`/`loop.go`/`task/*`/`prepare.go` 与新增负锚测试）；02.7 是**回归重验型完成**（零适配）。三种类型的验收强度不同，不可混计为"同等完成"。
3. **B 轴（本域内确实未实现/未清）**：`agent/invocation.go:45 Aggregate()` 桩 + `Invocation.Slot` 零引用（G1）；`state/store.go:10` 与 `tagent.go:11,33,79,88` 的 phase-1 陈旧文案（G4）；`tagent.Build`/`Runtime` 全桩且全树零消费者（G3）。
4. **证据可复跑性分档**：M 波叶（01.4-01.6、02.3、02.5、02.6、02.7、02.8、02.9、02.21、02.22）命令含包路径＋`-run ^Name$`＋`-count=10`＋`-json`＋计数，**可复跑**；早期叶与全部 P0 叶（01.1-01.3、02.1、02.2、02.4、8 个 P0）命令为简写或含 `<pkg>` 占位符，**不可复跑**（G5）。

---

## §2 原型对照：agent／state／task_manager 三件原型 vs 实现

### 2.1 特性级对照

| 原型特性 | 原型位置 | 实现落点 | 判定 |
|---|---|---|---|
| **事件消费壳**：`DefaultRun` 取批 → `DefaultOnEvents` 按类型分派（Before 插件→核心（ToolCall→spawn／Compact→Compact）→After 插件→内核追加） | `prototype/agent.go:39`、`:57` | `agent/loop.go:131 Drive`（取历史+ready → 逐 decision 循环 → receipts/observations 入 transcript → 无新增即 commit）+ `:488 dispatch`（`pipeline.Before` 每 call → 受理 → await → `pipeline.After` 仅对已提交事实） | **A 保留**：分派骨架同构；但顺序被提升为契约——Before **恰一次且在受理前**、After **仅在 canonical commit 之后**（02.6 负锚证实），原型无此约束 |
| **模型产出全部回队列**：`invokeModel` 把 Output/Reasoning 作 `EventModelDelta` 回送、逐个 `Publish` ToolCall、不发 Reply | `prototype/agent.go:107` | `model.ReduceChannel` 单一归约 → `Decision.Terminal`；工具结果经 `observationOf` 作**独立 observation** 入 transcript（`loop.go:868`） | **A 改造**：原型"回队列"的同步心智被"归约终局 + 观察回流"替代；见 2.2 专节 |
| **消费现场**：`AgentState` = 一个 agent id 的完整现场（queue／wake／busy／removed＋`Tasks`/`Memory` 创建时绑定＋`Context/ReplaceContext`＋`Begin/End` 独占消费＋`detach`） | `prototype/state.go:32`、`:70`、`:117`、`:174`、`:188`、`:195` | **被切分**：`state.Session{Address,Instance,Mailbox,Consumed,Invocations}`（接纳日志＋消费位＋决策槽投影，**不含会话正文**）＋`state.Occupancy`（同 session 至多一个在飞决策）＋`state.Bindings`（代＋借用＋退役）＋`run.history map[event.Address][]model.Message`（**会话正文在 run 内**）＋`agent/loop.go intake`（按 invocation 的 intake） | **A（部分）／B（一体性丢失）**：现场语义的三块（接纳、互斥、代）都还原了且更严；但"完整现场"的**一体性**被拆到 `state` 与 `run` 两处，会话正文只有 run 内存一份 → §5 G6 待裁 |
| **`State.Apply`**：validate → buildStatePlan（Before=current／After=candidate）→ `runEvolution`（Eval→Benchmark→ApplyGate，nil 段恒真）→ 同 id 换 def+prompt／新 id 建现场／缺 id `detach`+`Close`（锁外） | `prototype/state.go:270`、`:314`、`:351`、`:365` | `state/publish.go:261 Publish` → `287 publishLocked`（recheckBaseline → 同指纹 Unchanged → `348 evaluate` → `385 commitRecord` → `Hold` → retire track/sweep） | **A 保留＋C 强化**：三段演进（Eval/Benchmark/ApplyGate）被显式化为 `PublishContract{Required}` + `CheckVerdict{Unanswered,Pass,Reject,Exempt}`；原型的 **"nil 段恒真"被反转为"必需检查缺失即拒"**——这是本域最重要的一次语义反转（01.5 负锚 `ErrRequiredCheckMissing` 直接钉它） |
| **checkpoint**：`Save`/`LoadState` JSON 快照（Definition/Prompt/Queue/Context/Tasks/Memory），`Restore` 把 running 转 unknown | `prototype/state.go:424`、`:444`、`:410` | durable 事实链（`state.NewDurableCommit`）+ `run.go:287 restoreChains` + `task.HandoffRecord`/`RecoveryMaterial` + `Retained`（关闭实例未交付保留） | **A（任务域）／B（会话正文域）**：任务与通知路由可恢复（02.8 负锚证）；**会话正文无持久账**——`run_commit.go` 的 `memoryPublications` 注释自述 "durable ledger of the history domain is not wired into this minimal entry yet"。承 03/08 域，本域只旁证（G7） |
| **`TaskManager`**：`Spawn`（running 通知＋自持 ctx 协程）、`Complete`（terminal 保留旧事实）、`notify/flush`（**条数窗攒批 + `PublishBatch` 原子批**，注释："逐条 Publish 会让消费端在批中间被唤醒 pull"）、`Close`（cancel+`wg.Wait`+flush 残余） | `prototype/task_manager.go:35`、`:71`、`:140`、`:160`、`:129` | `task.Wrapper.Call`（一次受理→`go execute`）／`task.MemoryManager`（`Admit`/`Obligations`/`Flush`/`Handoff`）／`task.MemoryRegistry`（`Obligations` 明细）／`agent/shutdown.go`（真实停止确认） | **A 保留＋C 强化**：条数窗"攒批唤醒"的**意图**被 `Obligations.Count + Blocked + ExitReason` 的可解释义务账取代（更可查）；`Close` 的 `wg.Wait` 被升级为**producer 真实停止确认**（`stopped()==true`，非合作 producer 恒 false 不得伪结束）——原型 `Close` 只等 `wg`，不存在"producer 拒绝确认"这回事 |
| **`AgentTask` 子委派**：子 agent 委派，**首个 Reply 即结果**，cancel 停壳不关子 owner 资源 | `prototype/task_manager.go:171` | `agent/delegate.go:213 Delegate`（钉代）／`:320 Complete`／`:376 Reconcile`／`turnBorrow` 强制同代 | **A 改造**：原型"首个 Reply 即结果"是同步心智；实现改为 **AdmissionReceipt + 通知/终局 + owner 视图**，且递进为"多级任务重入定位所属 owner"（02.7 正锚） |
| **明示未实现**：背压、流式、持久化、强杀（`agent.go:2`）；生产 WAL/事务/多代租约、任意时刻崩溃一致性、按年龄判僵尸、函数序列化、队列背压（`state.go:6`）；重试、TTL、强杀、跨进程重挂（`task_manager.go:2`） | 三处文件头 | 同样未实现（承 09.7 总报告 B 类） | **B（继承边界）**：原型自陈的边界，实现同样未跨过；**不得**因为"原型也没做"就当作已满足——它们仍是 B |

### 2.2 专节：受理协议 Receipt／观察分离的特性本质

**原型的心智是同步的**：`invokeModel` 一旦拿到带 tool_call 的响应，就 `Publish` 每个 ToolCall 并回送 delta；`TaskManager.Spawn` 返回 `int` 任务 ID，`Complete` 直接把输出推回队列；`AgentTask` 干脆**把"首个 Reply"当作结果**。整条链的隐含假设是：**调用 → 结果**是一条同步的往返，结果的形状就是 Reply。

**实现的心智是异步事实**，并且把"一次调用"拆成**三个在语义上不可合并的时点**：

| 时点 | 承载体 | 时机 | 不可合并的理由（实现证据） |
|---|---|---|---|
| **受理回执** | `task.AdmissionReceipt`（`Accepted` + `Kind: Fresh\|Dedup\|Rejected`），在 `dispatch` 里先全部收齐（`agent/loop.go:501-556`） | 在**任何 await 之前** | 调用方需要**立即**拿到可去重、可挂起、可拒绝的凭证；它与"执行是否成功"无关。`admissionText`（`loop.go:856`）明说"result will arrive as a notification when it settles"，**不引导轮询** |
| **存储提交** | `task.CommitResult{Committed\|NotCommitted\|Uncertain}`（`task/commit.go`／`state/durable.go`） | 执行后、投递前 | 幂等提交 ≠ 已交付；`Uncertain` 是真实存在的一态（崩在提交与确认之间） |
| **投递接纳** | `task.DeliveryAcceptance`（`task/registry.go`） | 沿 envelope 冻结路由尝试投递时 | 投递可失败/可重复，且**路由在 envelope 冻结那一刻就不许再解析"当前目标"**（`task/handoff.go:266-275`） |

**观察分离**：结算后的结果以**独立 observation** 回流到发起该 call 的 invocation（`agent/loop.go:868 observationOf` → `run_fact.go:65 sink` → `loop.Backflow`（`:168`）→ `intakeFor(invocationID).deliver`），**不是**对同一 `CallID` 的第二次同步 response。这条正是 02.5 的 M2 补差合同第 2 条，负锚 `TestReceiptAndBackgroundStaySeparate`（11 pass）与 `TestCloseRefusesWhileModelInFlight`（模型在途时**没有任何 task**、只有两条 `Unsettled`）从两侧钉死了它。

**本质一句话**：**并发执行下的可承诺性**。同步心智只能承诺"我调了、它返回了"；异步事实要承诺三件不同的事——"**我受理了**（可去重/可挂起/可拒绝）"、"**我提交了**（可幂等/可 Uncertain）"、"**我送达了**（路由不可再解析）"。三层结果**不得塌缩为单 enum**（02.1 的设计闸 G1 原文即此），因为一旦塌缩，`Dedup` 会被读成"成功"、`Uncertain` 会被读成"失败"、`Accepted` 会被读成"已完成"——三种误读都会让关闭/排空/去重失真。

**由此派生的两处"看似不一致、实为必然"**：

1. `observationOf` 用 `model.RoleUser` + `ToolID` 保真（`loop.go:868-870`），而回放路径 `RoleOf` 用 tool 角色（`agent/request_assembly.go:86`）。**活观察是新输入**（它要驱动下一次决策，语义上等同用户新消息），**回放是历史原貌**（历史里它就是 tool 结果）。两条路径**刻意分叉**（R30 裁断，02.5 evidence 有记录）——不能以"角色名不统一"判缺陷。
2. `observation` 的正文是工具结算内容，**不带**受理前缀：`admissionText` 只进 receipt（`paired`，`loop.go:721`），不污染结算正文（负锚第 4 条断言）。

---

## §3 辩证分析

### 3.1 本域契约的**本质 vs 手段**

| 契约 | 本质（不可让） | 手段（可替换，且已换过） | 判读 |
|---|---|---|---|
| **发布唯一入口**（01.5） | 候选成为可执行绑定必须**先过治理裁决、再切代**，且"检查缺失"与"检查通过"必须可辨 | 原型是 `runEvolution(plan, evo)` 三段函数（nil 段恒真）；实现是 `Publisher` + 可注入 `Governance` + `PublishContract{Required}` + `Exempt{reason,scope}` | **本质保留并被收紧**：手段从"函数三段"换成"契约+裁决"，且**反转了默认**（原型 nil→恒真；实现 Unanswered→拒）。这是本域最正确的改造 |
| **同代借用**（01.4/02.5/02.7） | 一次决策执行期间的模型/工具/插件**必须同代**，发布不得中途改变在飞决策的绑定 | 原型无"代"概念（`State.Apply` 换 def 即生效）；实现用 `BindingRef{AgentID,InstanceID,Generation}` + `Borrow/BorrowDerived` + `Release`（CAS 一次）+ 退役随最后持有者 | **C（真新增）**：原型连"在途请求持旧代"这一需求都不存在（它同步、无借用）。本域把它做成了不可猜、不跨 hold 解析的身份 |
| **一次受理**（02.3/02.5） | 受理 ≠ 执行成功；受理回执**只给一次**，后台结果作独立观察 | 原型：`Spawn` 返 int + `Complete` 推回队列（同步 Reply 心智） | **本质还原**（见 2.2）；手段从"队列回推"换成"Receipt + observation + mailbox 接纳" |
| **关闭以真实收束为准**（02.9） | 未收束义务与非合作 producer **不得伪称结束** | 原型：`Close` = cancel + `wg.Wait` + flush；实现：refusing gate → `Obligations.Count==0` → `producer.stopped()==true` → 截止具名 `Unsettled` | **本质保留并被收紧**：`wg.Wait` 只保证"协程退了"，不保证"义务清了"；实现把两件事拆开且分别取证 |
| **文件热读 ≠ 结构换代**（01.6） | 内容在**请求边界**读新，结构代**不变** | 原型无此区分（config 整体 Apply）；实现三通道矩阵 `FP/SRC/FILE/RESTART` + `Route()` 硬拦"结构改动借文件通道"（`ErrStructuralViaFile`）+ `contentVersion` 独立哈希 | **C（真新增）**，且是本域"正文真进 GenerateContent"能成立的前提 |
| **通知身份与冻结路由**（02.8） | 一条通知恒属**一个**实例；路由在发 envelope 时冻结，重试不得再解析"当前目标" | 原型：`notify` 只有 `TaskNotice(int TaskID)`（无实例、无 round、无 seq） | **C（真新增）**：`(TaskID, Round, Kind, Seq)` + `HandoffRecord` 源/目标实例分离 + 持久 mutation `handoff:<slot>:<json>` 让重启后仍走新目标 |

**手段被换得最多的地方，恰好是本质被守得最紧的地方**：三段演进 → 契约+裁决（01.5）、队列回推 → Receipt+观察（02.3/02.5）、`wg.Wait` → 义务+producer 双取证（02.9）。这三处若只看代码形状会觉得"改得面目全非"，但对着本质看是**同一个承诺换了更可靠的兑现方式**。

### 3.2 golden S03 drift 的**本域归因**

09.7 总报告二-2 的 4 项 drift 中，**2 项属本域**（02.5），且两项都不是欠实现：

| drift | golden 期望 | 实现 | 本域归因 |
|---|---|---|---|
| **S03 `model_message_roles`** | 4 元，含 assistant 连排 | **6 消息**（receipt／observation 分立） | 02.5 的 M2 补差合同**明文要求**"Receipt 进 transcript；后台结果独立观察回流"。一次带 1 个 call 的决策因此产生 **assistant（含 tool_calls）＋ receipt（准入回执行）＋ observation（结算正文）** 三条，而非同步想象下的两条。**golden 的 4 元是规划期按同步心智写的观察想象**，实现穿透后的 6 消息形态才是真实语义 |
| **S03 `receipts[1].decision`** | `decision-b` | `decision-a` | 一次 assistant 消息携带**双 call**，两笔受理同属**同一个决策**——`receipts[i].decision` 本应都指向 `decision-a`。golden 把第二笔错误地分配到下一个决策（臆想分配） |

**归因结论**：这两项 drift 是 **golden 需对齐**，不是产品回退（总报告已判"golden 该改，产品不回退"）。但**必须诚实标注**：这是**规划期判据被实现穿透**，不是"实现有错"；同时 S03 golden **至今未改**（本次未查到对齐记录），故它仍然是一个**待办的对齐动作**，不能当作已收口（§5 G8）。

### 3.3 本域"过往行为/既有契约**不作**金标准"的两个案例

1. **`State.Apply` 的"nil 段恒真"**（`prototype/state.go:351`）。若把原型行为当金标准，则"没有 Eval 段就自动通过"是合法行为；但本域 01.5 的设计闸把默认反转了：**必需检查缺失即拒绝发布**（`evaluate`：空契约拒／`Unanswered` 的必需项拒／`Exempt` 须 reason+scope）。负锚 `TestFormalPublishRefusesMissingEvidence` 用"缺 eval 证据→`ErrRequiredCheckMissing`、旧代不变"钉住了反转后的行为。**结论：原型行为在此处是被明确否决的，不是被继承的。**
2. **`observationOf` 的 `RoleUser` vs `RoleOf` 的 `RoleTool`**。若以"同一份数据必须同一角色名"为金标准，会判这是缺陷；实际是**活观察（新输入，驱动下一决策）**与**回放（历史原貌）**两个不同语义位置。**结论：一致性不能跨语义位置强加。**
3. （旁证）**`TaskManager.notify` 的条数窗攒批**：原型注释给出了理由（"逐条 Publish 会让消费端在批中间被唤醒 pull"），实现**没有保留批的机制**（改为逐条 `Obligations` 记账 + `Flush`）。这不是遗漏——条数窗是在"消费端被唤醒即 pull"的模型下的优化，而实现的 `Drive` 是**拉取式**（每次决策重新 `sessionHistory`+`aggregateReady`），不存在"批中间被唤醒"的问题。**结论：手段的前提消失后，手段不必保留；但此点须在 §5 挂"不做判据"的显式说明，避免后续审查把它当遗漏。**

---

## §4 双维局部评（概念数／职责／多态）

### 4.1 简单维（概念统一、收敛）——实测数字

| 指标 | 实测值 | 命令 | 判读 |
|---|---|---|---|
| 全仓接口数 / 包数 / go 文件 | **74 / 18 / 176** | `python3 scripts/check_concept_budget.py --rule iface-methods` | 宏观规模 |
| `agent` 包接口数 | **5**（`Agent`/`DelegationTasks`/`Recorder`/`HistoryFace`/`Reconciler`） | `grep -rn "^type .* interface {" agent/` | **达标**：02.22 的 ≤5 硬指标成立，且 `Producer`/`ProducerWatch` 已并入 `Shutdown.Watch`（全树零残留，本次 grep 未见旧名） |
| 裸 `ctx.Value` 调用点 | **2**，且**都在 accessor 实现体内**（`plugin/policy.go:77`、`task/wrapper.go:54`） | 上述 checker `--rule ctx-bare` + 交叉 grep | **口径达成但非"零"**：checker 口径是"调用点=0 + accessor 实现体各 1 按文件具名登记"，输出 `RESULT: ok` 且带 2 条 `warn`。**不得表述为"裸读已归零"**，只能表述为"契约内合法落点=2，第三缝零新增" |
| `Invocation` 导出字段数 | **6**（含 **`Slot`**）> 阈值 5 | `--rule struct-fields`（`baselined at 6`） | **已登记为已知债但未收敛**；且 `Slot` 零写零读（G1）——checker 只查字段个数，不查"字段是否有生产者"，故**该缺口对现有检查器不可见** |
| 三层结果是否合并 | **未合并**：`AdmissionReceipt` / `CommitResult` / `DeliveryAcceptance` 三类型分立 | `task/commit.go`、`task/registry.go`、`task/admission.go` | **本质项守住** |
| 日志/事件单一来源 | `event.Type*`/`MetaKey*` 单一常量源；`model` 单一归约入口 | `event/meta.go`、`model/terminal.go`、`agent/loop.go` 仅调用 `ReduceChannel` | **无影子副本**（`agent` 未自造 second reduction） |
| 缺陷 | 早期叶 evidence 非规范命令（G5）；`tagent.Build`/`Runtime` 与 `tagent.Assemble`/`Run` **两套公开入口并存**（G3） | — | **概念统一在本域有一处未收口** |

**简单维小结**：三个"单一来源"（身份常量、终局归约、三层结果）都真的单一；端口面 ≤5 达标；但 **公开入口面是双的**（`Build`/`Runtime` 全桩零消费者 vs `Assemble`/`Run` 真实现），这在"简单"维上是明确扣分项，且是**声明面残件**而非有意分层。

### 4.2 清晰维（职责、单一抽象、丰富多态）

| 层面 | 判读 | 证据 |
|---|---|---|
| **职责边界** | **清**：`state` 持绑定代/接纳日志/决策槽/授权；`task` 持受理/义务/通知/路由；`agent` 持决策循环/关闭/委派；`run*` 持装配与派生读面。四层无反向依赖（`state` 不 import `agent`，`agent` 借 `state.Borrow` 接口） | import 实测：`agent/loop.go` 头部仅 import `state`/`task`/`model`/`plugin`；`state` 侧无 `agent` 依赖 |
| **单一抽象** | 每层有且仅有一个主轴：`Bindings`（代）／`Mailbox`（接纳）／`Publisher`（发布）／`Shutdown`（关闭）／`Delegator`（委派）／`Wrapper`（受理）／`MemoryManager`（义务+路由） | 见 §1 落点列 |
| **多态** | **有实义的多态**：`Governance.Check`（策略注入：`passPolicy`/`rejectPolicy` 在负锚里真的改变结果）、`plugin.Plugin` 相位分派、`Shutdown.Watch(taskID, func() bool)` 以**函数**而非接口表达"停止确认"（比接口更轻，且 02.22 明确删了 `Producer`/`ProducerWatch` 两个接口）、`state.Borrow` 有 `Bindings`/`Publisher`（带退役）两实现、`task.Approver` 有 `policyRuling`/`denyWithoutApprover` 两实现 | 负锚 `TestFormalAssemblyRequiresGovernance` 的 pass/reject 双 policy 是**多态被真用**的硬证据（不是空接口） |
| **抽象浪费/空转** | **有 3 处**：`Invocation.Aggregate`（桩、零调用）、`Invocation.Slot`（零写零读）、`tagent.Runtime` 四方法+`Build`（全桩、零消费者） | G1／G3 |
| **过度抽象** | **1 处需裁**：`DecisionSlot` 类型（`agent/invocation.go:56`）整体只被 `Invocation.Slot` 引用，而 `Invocation.Slot` 无人写 → `DecisionSlot` 是"为投影准备但投影从未发生"的类型 | G1 |
| **抽象不足** | **1 处需裁**：会话正文只有 `run.history` 一份且在 `map[Address][]model.Message` 里，**没有抽象成 state 侧的现场**；`state.Session` 有 `Mailbox`/`Consumed`/`Invocations` 却无正文 → 同一"会话现场"被两个所有者各持一半 | G6 |

**清晰维小结**：四层职责与七个主轴是清且少的；多态是**被真用的**（policy 注入、函数式停止确认、Borrow 两实现），不是为多态而多态。扣分集中在**声明面残件**（3 处零引用抽象）与**会话现场的所有权二分**（1 处）。

---

## §5 缺口与待裁

> 以下每条都给**可复跑命令**。**没有一条是 PASS**；G1/G3/G4 是可以直接修的残件，G6/G8 需要上级裁决。

### G1｜`agent/invocation.go` 三处声明面零行为（**B 轴残件，最硬的一条**）

- **事实（实测）**：
  - `agent/invocation.go:45-47` `func (inv *Invocation) Aggregate(ready []event.Event) error { return errNotImplemented }`；`grep -rn "inv.Aggregate\|Aggregate(ready"` → **仅定义处 1 条，零调用**。真实聚合在 `agent/loop.go:664 aggregateReady(ready []event.Event) []model.Message`。
  - `agent/invocation.go:34` `Slot DecisionSlot`；`grep -rn "Slot:" --include=*.go` → **仅 `agent/skeleton_assert.go:41`**（编译断言）。生产零写、全树零读。
  - `agent/invocation.go:56` `type DecisionSlot struct{...}` 只被上面这个零引用字段引用。
- **为什么该被本域发现却没被发现**：`--rule struct-fields` 把 `Invocation` 6 字段登记为 `baselined at 6`（已知债），而**规则只查个数、不查"字段有无生产者"**；02.22 的端口审视只数 `interface`（≤5 达标），不看 struct 字段的死活；02.P0.4 的"零逻辑骨架"验收口径是"grep `phase 2: not implemented` 覆盖全部 stub"——而这个 stub 的文案是 `errNotImplemented`，**不在该 grep 的 pattern 里**，于是骨架门与实现验收**双双漏掉**。
- **待裁**：`(a)` 删（`Aggregate`/`Slot`/`DecisionSlot` 三件一起删，`Invocation` 降到 5 字段，顺带把 baseline 6→5 收紧）；`(b)` 接线（让 `Loop.Drive` 经 `Invocation.Aggregate` 聚合、让决策槽经 `Invocation.Slot` 投影）。**建议 (a)**：`state.Occupancy` 已是唯一槽源，`aggregateReady` 已在跑，接 (b) 会造第二源，违反本域"不建镜像"的红线。

```
grep -rn "inv.Aggregate\|Aggregate(ready" --include=*.go .     # 期望：仅 agent/invocation.go:45
grep -rn "Slot:" --include=*.go .                              # 期望：仅 agent/skeleton_assert.go:41
python3 scripts/check_concept_budget.py --rule struct-fields | grep Invocation   # baselined at 6
```

### G2｜`01.2`／`02.2` 证据命令**缺 `-race`**（与 tasks.md 行内验证不一致）

- **事实**：`01.2` evidence = `go test TestSessionAuthority -count=10 (61)`；`02.2` evidence = `go test TestTaskLifecycle -count=10 (81)`。两叶 `tasks.md` 行内验证均为 `go test -race ... -count=10 -json`。「并发/状态任务加 `-race`」是本域 `tasks.md` 头部的硬口径。
- **影响**：这两叶都在"并发/状态"类（session 授权、task 生命周期），**无 `-race` 的 61/81 计数不能证明竞态下正确**。
- **待裁**：补跑 `-race` 并把计数回填 evidence（推荐），或在 evidence 里显式注明"本轮未加 `-race`，理由 X"。

```
go test -race ./tests/integration/state -run '^TestSessionAuthority$' -count=10 -json | tail -3
go test -race ./tests/integration/execution -run '^TestTaskLifecycle$' -count=10 -json | tail -3
```

### G3｜两套公开入口并存：`tagent.Build`/`Runtime` 全桩零消费者 vs `tagent.Assemble`/`Run` 真实现（**B 轴残件**）

- **事实（实测）**：
  - `build.go:78 func Build(deps Deps) (*Runtime, error) { return nil, errNotImplemented }`；`tagent.go:59 CloseSession`、`:70 Shutdown`、`:78 Query` 全 `return errNotImplemented`；`:92 Bridge` 返回 `ErrBridgeNotAssembled`。
  - 消费者：`grep -rn "tagent.Build(\|\.Bridge(\|tagent.Runtime\|ErrBridgeNotAssembled"` → **仅 `skeleton_assert.go:28-36`（编译断言）与 `docs/.dev/REPORT.md:447`（历史报告）**，**零生产消费者、零测试消费者**。
  - 真入口：`run.go:154 Assemble` → `*Run`，被全部 host 负锚与 execution 正锚使用。
- **判读**：08.1 已裁"唯一 Assemble 四面"，即 `Build`/`Runtime` 是**被取代后未清理的声明面**。这与 `tagent.go:11,33,79,88` 的"phase 1 skeleton"文案同源。
- **待裁**：`(a)` 删 `build.go`/`tagent.go` 的 `Runtime`+`Build`+`Deps`+`ModelFactory`（保留 `Assemble`/`Run` 为唯一门），同步更新 `docs/api/**` 生成物；`(b)` 保留但改注释为"reserved, superseded by Assemble（08.1 裁断）"。**建议 (a)**，但**这是删公开符号**，须上级批准后另开授权（本报告只登记，不动手）。

```
grep -rn "tagent.Build(\|\.Bridge(\|tagent.Runtime\|ErrBridgeNotAssembled" --include=*.go tests/ *.go
sed -n '78,80p' build.go ; sed -n '59,60p;70,71p;78,79p;92,93p' tagent.go
```

### G4｜phase-1 陈旧文案（文档债，**B**）

- `state/store.go:10` "This is the phase 1 skeleton: it declares shapes and holds no behaviour"——`state` 包现有 9 个实现文件（binding/publish/mailbox/occupancy/session/authority/store/filesource/channels），该句**已成假**。
- `tagent.go:11,33,79,88` 同上（与 G3 同源）。
- **待裁**：删句或改写为现状描述。属"注释勘误"，风险低，但**本报告只读不动**。

```
sed -n '8,12p' state/store.go ; sed -n '9,12p;31,35p;77,82p;86,91p' tagent.go
```

### G5｜早期叶与全部 P0 叶 evidence **不可复跑**（数字无法溯源）

| 叶 | evidence 命令 | 缺什么 |
|---|---|---|
| 01.1 | `go test TestConfigurationContract -count=10` | 包路径、`-json` |
| 01.2 | `go test TestSessionAuthority -count=10` | 包路径、`-json`、**`-race`**（G2） |
| 02.1 | `go test TestIdentityAndCommitContract -count=10` | 包路径、`-json` |
| 02.2 | `go test TestTaskLifecycle -count=10` | 包路径、`-json`、**`-race`**（G2） |
| 02.4 | `go test TestModelTerminalProtocol -count=10` | 包路径、`-json` |
| 8 个 P0 | `gofmt -l <pkg>`、`comment_policy ... <pkgs>` | **含 `<pkg>`/`<pkgs>` 占位符**，命令原文不可执行 |

- 对比：M 波叶（01.4-01.6、02.3、02.5-02.9、02.21、02.22）命令**完整可复跑**（包路径＋`-run ^Name$`＋`-count`＋`-json`＋计数）。**同一 change 内两种证据标准**本身就是 `midterm` §一 S5"勾选证据缺原始口径"的残留。
- **待裁**：回填可复跑命令与原始计数（不必重跑实现，只需按现有命令跑一遍并登记输出摘要）。P0 叶另需注明"骨架门为历史门，包内已无 stub，不可重放"。

### G6｜**会话现场的所有权二分**（待裁，本域最需要判的一条）

- **事实**：`state.Session`（`state/session.go:13`）持 `Mailbox`/`Consumed`/`Invocations`，**无会话正文**；会话正文在 `run.history map[event.Address][]model.Message`（`run.go:120`），经 `run_fact.go:122 SessionHistory` / `:132 CommitHistory` 读写，由 `agent/loop.go:137-138` 与 `:151` 使用。
- **与原型对照**：`prototype/state.go:32` 的 `AgentState` 是"一个 agent id 的**完整**消费现场"（queue + context + tasks + memory 一体），`Save/LoadState`（`:424`/`:444`）把 `Context` 一起快照。实现把现场拆成 `state`（接纳/槽/代）＋`run`（正文/事实索引/呈现/快照）两处。
- **是否违反红线**：**不违反**。`tagent.go:8-11` 的红线是"keeps no second copy of the live scene：effective bindings 归 state owner、obligations 归 task 层、readings 派生自 facts"——`history`/`facts`/`presented`/`snapshots` 属**派生读面**，不是活动绑定/义务副本。
- **但**：`history` 是会话正文的**唯一源**，`state` 侧不持有；`Save/Restore` 语义因此只能覆盖任务域（02.8 做到了通知路由），**正文历史无持久账**（`run_commit.go` `memoryPublications` 注释自述 "durable ledger of the history domain is not wired into this minimal entry yet"）。
- **待裁**：`(a)` 接受二分（会话正文归 run/会话层，state 只管接纳与代），并在 `contracts.md` 显式写"会话现场由两所有者分持，正文不入 state"；`(b)` 把正文上收为 `state.Session.Transcript`（回归原型"完整现场"语义，但会让 state 依赖 `model.Message`）。**建议 (a)+显式契约**：`state` 现无 `model` 依赖，上收会引入跨层依赖；(a) 的代价是必须承认"完整现场可快照"这一原型特性**未继承**，应计入 B 轴而非含糊带过。

```
grep -n "type Session struct" -A 22 state/session.go        # 无 transcript 字段
grep -n "history *map\[event.Address\]" run.go              # run 内唯一正文源
grep -n "durable ledger of the history domain" run_commit.go
```

### G7｜持久化/背压/流式的边界继承（**B**，与 09.7 总报告 B 类同源）

- 原型三处文件头明示未实现（`agent.go:2`、`state.go:6`、`task_manager.go:2`）：背压、流式、持久化、强杀、WAL/事务/多代租约、崩溃一致性、TTL、跨进程重挂。
- 实现同样未跨过：`run_commit.go` 的 `memoryPublications` 只有内存账；`model.ReduceChannel` 不做流式透传（归约终局）；无背压门。
- **本域归因**：这些**不在 23 叶的合同范围内**（02.4 只要求"单一终局归约"，02.9 只要求"真实停止确认"），因此**不构成本域 23 叶的未完成**；但它们是**域级 B 轴**（原型要求/规划要求未实现），必须在总账里保留，不得因"原型也没做"而消失。

### G8｜S03 golden 至今未对齐（**流程缺口**）

- 09.7 总报告二-2 已裁"S03 该改、产品不回退"，但本次**未检索到 S03 golden 的对齐记录**（`evidence/` 下无对应 run）。
- **待裁**：开一条对齐任务（改 golden 的 `model_message_roles` 为 6 消息形态、`receipts[1].decision` 改回 `decision-a`）并附 run_id；否则每次回归都会把这两处 drift 重新报成"失败"，等于**用一个已知错误的判据反复告警**。

```
grep -rn "model_message_roles" openspec/changes/runtime-rebuild/evidence/ tests/ | head
```

### G9｜`02.22` notes 的"生产装配缺口挂账"前提**在当前代码上已不成立**（台账销账项）

- `02.22` notes 写："生产装配缺口具名挂账(08.4 前承接: 装配或收缩纯测试面)"。
- **实测**：`run.go:212-223` 五端口**全部接线**——`SetHistory(r)`／`SetWatch(r.shutdown.Watch)`／`SetPresentations(r.takePresentation)`／`SetReconciler(r.delegations)`（`run.go:211`）／`Recorder` 在 `cfg.recorder != nil` 时接线；`DelegationTasks` 经 `closeFace` 注入 `Shutdown`（`run.go:210`）。
- **判读**：挂账前提**已消失**（`Recorder` 的条件接线是"nil 则不记录"的**配置缺省语义**，不是缺口）。**待裁**：请编排者确认挂账所指是否为别的面；若是本条，则应在 `task-graph` 里销账，避免下次审查把它当未收口。

```
grep -n "SetHistory\|SetWatch\|SetPresentations\|SetRequestFace\|SetRequestSnapshotFace\|SetRecorder\|SetReconciler" run.go
```

### 缺口汇总（不构成 PASS，仅索引）

| 编号 | 类别 | 轴 | 阻断性 | 建议处置 |
|---|---|---|---|---|
| G1 | `invocation.go` 三处声明面零行为 | B | 中（不阻断运行，阻断"概念收敛"验收） | 删（推荐）或接线 |
| G2 | 01.2／02.2 证据缺 `-race` | B（证据） | 中 | 补跑回填 |
| G3 | `Build`/`Runtime` 全桩零消费者（双入口） | B | 中 | 删（推荐，需授权） |
| G4 | phase-1 陈旧文案 | B（文档） | 低 | 注释勘误 |
| G5 | 早期叶/P0 叶 evidence 不可复跑 | B（证据） | 中 | 回填可复跑命令 |
| G6 | 会话现场所有权二分 | 待裁 | 高（涉及契约表述） | (a) 显式契约（推荐） |
| G7 | 持久化/背压/流式边界 | B（域级，继承） | — | 总账保留，不计入 23 叶未完成 |
| G8 | S03 golden 未对齐 | 待裁（流程） | 中 | 开对齐任务 |
| G9 | 02.22 挂账前提消失 | 待裁（台账） | 低 | 销账 |

---

## 附：本次审查的复跑命令清单

```bash
# 台账与合同
python3 -c "import json;g=json.load(open('openspec/changes/runtime-rebuild/task-graph.json'));print(len(g['tasks']))"
sed -n '/## 二/,/## 三/p' openspec/changes/runtime-rebuild/midterm-review-response.md

# 概念预算（两轴实测）
python3 scripts/check_concept_budget.py --rule iface-methods   # RESULT: ok
python3 scripts/check_concept_budget.py --rule ctx-bare        # findings.ctx-bare: 2
python3 scripts/check_concept_budget.py --rule struct-fields   # Invocation baselined at 6

# G1 零引用
grep -rn "inv.Aggregate\|Aggregate(ready" --include=*.go .
grep -rn "Slot:" --include=*.go .

# G3 双入口
grep -rn "tagent.Build(\|\.Bridge(\|tagent.Runtime\|ErrBridgeNotAssembled" --include=*.go . | grep -v skeleton_assert
sed -n '78,80p' build.go

# G6 会话现场二分
grep -n "type Session struct" -A 22 state/session.go
grep -n "history *map\[event.Address\]" run.go

# G9 五端口装配
grep -n "SetHistory\|SetWatch\|SetPresentations\|SetRecorder\|SetReconciler" run.go
```
