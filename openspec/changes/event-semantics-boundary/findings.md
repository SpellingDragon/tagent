# 规划自审记录（阶段 5，review-checklist 全项）

## 现实对齐层（5.11/5.12）

- 5.11 症状直译锚：proposal 第一行三症状=用户原话+直译测试；00 域三测试第一天红（D1 派发即跑）；F3 回滚即红双证据；必答一问已答（proposal 末节，含诚实边界）。
- 5.12 方案降格：本案输入为三条用户原话+真机红证（20:12:26 复述、20:15:55 失败静默），非交接方案；每域均可溯源到症状。规模 14 叶 < 20，另设 W0 麻雀（D1/D2）。

## 覆盖性（5.1–5.10）

- 5.1 目标-任务：三症状→00 三叶；三静默→01 三叶；失败告知→02 三叶；枚举下沉→03 两叶；合同/CI→04 三叶。全覆盖。
- 5.2 消费者：症状=CI job（4.3 独立 job）；失败 fact=审计面（02.2 明示不进模型/历史/回放）；exec 描述=模型请求；outbound=用户。
- 5.3/5.4 档位与性能主张：不涉。
- 5.5 真实路径：症状 3 以 smoke 确定性 500 注入为真实路径替身；真机红证 20:15:55 入 F3 附件。
- 5.9 能力重叠：命中 `delivery-dispatch-receipts`（内部回执）与 `failure-polarity`（终局判定）；裁定均不扩展，边界声明入一级 D7 与 01/02 域 design。主规格 progress 仅 seq 有序条款（:413），无"作为输入材料"条款——progress 静默不构成 MODIFIED 冲突。
- 5.10 verify 三锚：逐叶具备（正/负/消费者锚随任务行）；04.3 依赖 00 域测试名，已标注。

## 待裁项裁定（P-01–P-07）

P-01 批准（01.3 含 progress 读路径）；P-02 批准（示例层罐头文案去除）；P-03 批准（Turn.Failure/Outcome.Cause 贯通，error 语义保留+负锚防混用）；P-04 批准（A2–A7 修订；v1 S15 改写；v2 零改）；P-05 裁定删 04.4（不建 task-graph）；P-06 记录 v2 合同实测 29 父/143 子（非 141，不改合同）；P-07 叶数 14。

## 残余风险

1. fake 模型下症状 G1/G2 可能天然绿（无判别力）——D1 必须出红证或伪协议对照红证，否则该叶重开。
2. 02 域写集触及 api/model 与 rl/produce（Turn 贯通）——与 event-integrity 的 model 观察面相邻，实施期需对照 terminal 语义单终局（failure-polarity）不破。
3. 04.3 独立 CI job 的必跑地位依赖仓库外部 branch protection（如实登记，仓库内仅证 job 存在）。

## 实施期裁定（D4 后，2026-10-25）

1. **`failed_turn_with_body_delivers_body_only` 结构不可达（R29 备案，待 F3 前用户确认）**：02 域原锚要求"失败但带正文时只投正文"。实测 `runtime/agent/loop.go:932` 构造的 `model.Request` 从不设 `Stream`（恒 false）→ 非流式响应一次只折叠一个 → `KindFailed` 的 outcome 结构上无正文，宿主消费者层无法构造该场景。裁定：条款保留（deliverFresh 的正文优先分支已实现），证据形态改为**不可达性事实钉住**（Stream 恒 false + 非流式折叠语义）；若未来引入流式，该锚转为必测硬门。测试内 `t.Skip` 保留并注明。此项为验收面形态变更，已上报，未偷偷缩水。
2. **`TestLocalSmokeWave2Recall` 既有红（归档前已存在）**：在 67a6399（event-consumption-integrity 归档树）上复现同样红（`recall_recent` 返回 5 条且 truncated，旧期望 2），6cc4414 基线绿（测试尚未存在）→ 定性为既有红、非本变更引入。该测试带 smoke tag，不在 F2 门禁命令集（三模块 `-short`）内；登记为已知问题，不阻塞本案，建议另案修 recall 断言。
3. **D3 自决复核（批准）**：progress 判据升级为 `pipelineProgress`（kind=progress 且 Round>0）——exec 观察复用 NoticeProgress 分类，直接按 kind 跳过会误伤 `TestEventIntegrityExecStateEvents`（实测）；子代理的归因实验（exec-state-round-filter.log exit=0）与基线对照成立。

## 审计后 P0 收口（2026-10-26）

来源：`evidence/grill-audit.md` 的 N1/N2/N3/N5、C 段 `admitNotice` 死代码与 C2 `kindSchema` 矛盾。六件均已落盘；行为变化仅限两处模型面文本（N1）、一处 schema 枚举（C2）与删除零调用者缓存（C1），均与裁定一致。

### 1. N1 委派工具描述与裸 `ok` 回执对齐（2dabf37）

- 改动：`tool/subagent/tool.go` 的 call_subagent 描述由「the receipt carries the task identity, and the progress and terminal of that task arrive as its notifications」改为「the receipt is a bare ok; the terminal of that task arrives as a notification in a follow-up turn of this session」；`tool/subagent/target.go` 的声明目标工具同句一并改（全仓 grep 命中的同类模型面承诺；只修 `tool.go` 会留下另一半）。
- 负锚：`TestSubagentToolDeclaration/the_description_promises_no_receipt_payload` 与 `TestTargetDeclarationPromisesTheBareReceipt` 断言描述含 bare ok、不含 `task identity`/`carries the task`/`progress`，target 仍须写明 terminal notification。
- 红证：旧文本还原 → 两测试均红（`want it to state the bare ok receipt`）；保留 bare ok 再追加 `the receipt carries the task identity` → 负断言红（`want no "task identity" promise`）。恢复后 `go test ./tool/subagent/ -run 'TestSubagentToolDeclaration|TestTargetDeclarationPromisesTheBareReceipt' -count=1` → ok。
- 全仓核对：`grep -rn "task identity\|carries the task\|admission receipt.*task\|progress and terminal" --include="*.go" .` 余下命中均为内部机制注释、错误串或测试断言（`runtime/agent/agent.go:44` 的「progress and terminal arrive as notifications」描述内部通知机制，属实），无模型面承诺；`examples/*/resources/prompts/` 对这几类短语 grep exit=1；`plan_agent.md:54` 的 `resume_task` 是 plan 后端操作名，与受理回执无关，未改。

### 2. N3 设计文档对齐实现与规格（本提交）

- `design.md` D8.4 原文「failure-notice fact 进会话历史/回放（作为用户已见的系统口径事实）……02.2 原『不进历史/回放』表述撤销」与 `changes/02-failure-delivery/specs/failure-notice/spec.md:31` 的 MUST NOT 互斥，代码按 spec 实现。
- 改为：fact **不进任何模型请求、历史 transcript 或回放投影**（不带 assistant/user 载荷与通知元数据），也不作为输入材料唤醒回合；可见面是会话事实链/审计与日志。「用户追问『刚才怎么了』时模型可答」登记为未裁定的产品议题，由后续单独裁定；裁定前不得据此给 fact 加消息载荷。仅改设计文本，未动代码。
- D8.6 同步为 `Enum（kind=["", "tmux"]：省略即空串，选 tmux）`。

### 3. N5 doc budget 入库（c6c99c6）

- 提交前：`tail -c1 scripts/doc_budget.json | xxd` → `0a`；`python3 -c` 解析 `baseline` → `{'files': 197, 'lines': 22220}`（尾换行已补，JSON 合法）。
- 提交后：`git show HEAD:scripts/doc_budget.json | python3 -c "import json,sys;print(json.load(sys.stdin)['baseline'])"` → `{'files': 197, 'lines': 22220}`；同管道 `tail -c1 | xxd` → `0a`。
- clone 级：`git clone --local . /tmp/tagent-p0-clone.ESWyHr`（at c6c99c6）→ `bash scripts/lint.sh` → `lint: ok`；根模块 `go test ./... -short -count=1` 全绿；`examples/wechat-bot` 与 `examples/cli-host` 的 `go test ./... -short -count=1` 均 ok。

### 4. C 段 `admitNotice` 死代码清理（0e64bc3）

- 删除：`intake.notices`、`noticeEntry`、`intake.notify`、`intake.takeNotices`（零调用者）；`admitNotice` 改名 `commitProgressFact`，注释改为只陈述「提交事实、不 park」；intake 文档去掉不存在的通知缓存叙述。
- `grep -rn "admitNotice\|takeNotices\|noticeEntry\|in\.notices\|\.notify(" --include="*.go" .` 仅余 `evolution/meditation/driver.go` 的同名无关方法。
- 5 处死面断言：progress 通道的 `cached`、unaddressed 的 `cached`、settled 的 `cached`、accepted 的 `cached`、quiet progress 的 `takeNotices`——全部改为现存语义断言；`unaddressed_progress_commits_nothing` 改为用 `invariantCommitFace` 断言提交 0 条事实，不再借缓存为空间接证明。
- 行为中性：`go test ./runtime/agent/ ./tests/integration/execution/ -count=1` 全绿；G2 `TestLocalSmokeSymptomSettledOnce` 两个子测试 PASS。

### 5. C2 `kindSchema` Enum 与描述一致（9a6bb16）

- 运行事实：`sessionNameOf` 把空 Kind 解为 tmux（`tmux_executor.go:367`）。裁定：`Enum` 由 `[KindTmux]` 改为 `["", KindTmux]`，描述保留「leave it empty」；`exec_test.go` 的 `names_the_only_family` 改为断言枚举恰为 `["", "tmux"]`（空串在前），并对每个枚举值断言 `sessionNameOf` 接受。
- 红证：枚举还原为 `[KindTmux]` → run/release 两条声明的 `names_the_only_family` 与 `production_preset_carries_the_same_text` 均红（`want the empty kind beside [tmux]`）；恢复后绿。
- 同源：`pattern_agrees_with_the_validator` 与 `production_preset_carries_the_same_text` 仍在同一测试内通过，resource Pattern 的上界/字符集仍由 `tmuxSessionMaxName`/`sessionNameCharset` 生成。

### 6. N2 `pipelineProgress` Round 语义显式化（0e64bc3）

- `loop.go` 注释写明：判据 = kind=progress 且 Round>0；Round 判据是分类复用补丁（exec 观察以 `NoticeProgress` 提交且无 round，材料必须进下一请求）；生产侧无真正的 progress 发射点，`Round>0` 分支仅由测试覆盖；畸形 round 保留材料；零 round 形状由 `TestQuietProgressNotice/zero_round_progress_is_material` 钉住。
- 新测试：提交 1 条 `Round=0` 的 progress notice，断言事实已提交，且 `Drive` 的首个请求携带该观察材料（`invariantCarries`）。
- 红证：判据临时改 `round >= 0` → `zero_round_progress_is_material` 红（`want the material host.observeExecState commits…`）；恢复后绿。

### 验证门（2026-10-26）

- `go build ./...`、`go vet ./...`：ok。
- `go test ./runtime/agent/ ./tool/exec/ ./tool/subagent/ ./tests/integration/host/ ./tests/integration/execution/ -count=1`：五包全 ok。
- `bash scripts/lint.sh`：`lint: ok`（含 comment policy、doc budget、doc-refs、gen_godoc、package-layout）。
- G1/G2/G3：`go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' -count=1 -v` → 三父测试/六子测试全部 PASS。

### 自决与残留

- `target.go` 的同类承诺按 N1 同面一并修：它不是 `tool.go:125` 的字面位置，但同为模型面描述且含旧承诺，不修则 grep 仍命中。
- `changes/03-tool-schema/design.md` 的 D1/D2/D3 随枚举裁定同步（旧文写「不把空串加进 Enum」），避免设计与实现再次矛盾。
- `plan_agent.md:54` 的 `resume_task`、`plan_tool_desc.md` 的现有表述均未改：前者是 plan 后端操作名，后者「terminal notification of the admitted task」与现状一致；若前者确为陈旧入口，另案处理。

## exec 中间态根因与修法（2026-10-26 补差）

### 根因（真机 10-26 00:48）

- `host.go:740 observeExecState` 对 `StateStable`/`StateSuspect`/`StateExited` 三态各构造 `execStateText(evt)` 材料，写成 `TypeFeedback + MetaKeyNoticeKind=NoticeProgress + MetaKeyTaskID + Role=user` 的 fact，并 `fireNoticeTurn` 驱动模型回合。
- `runtime/agent/loop.go` 的 `pipelineProgress` 只跳过 `Round>0`；该 fact 无 Round → 材料进入下一请求。真机表现为模型被持续叫醒并播报“剩余后台项…仍在”。

### 修法

- `observeExecState` 重写：三态不 `fireNoticeTurn`、不写 notice-kind fact、`Message.Content` 为空；只 `CommitToSession` 落 `exec-state: <phase>` 的无载荷 feedback 审计事实（`meta_resource`/`meta_phase`，confirmed 另带 `meta_exit_code`），保留一条 `assemblyDebug`；拒绝提交只记 warning，不重试、不驱动回合。
- `execStateText` 删除。全仓引用面：`grep -rn 'execStateText' --include='*.go' .` exit=1（无命中）；查询面/看板没有该符号消费者，故不存在“静默破坏查询面”。
- `runtime/agent/loop.go` 两处注释改为当前事实：progress 判据是分类守卫，exec 不再是 producer；逻辑未改。
- 反向改 `TestEventIntegrityExecStateEvents`（不删不 skip）：三态后 provider 请求数与 outbound 均不增；三态均有载荷为空的审计事实；下一请求不含 `reports state`/`exec-state: ` 材料。状态机可能把 stable 观察两次（stable→suspect→stable→exited），断言按相位覆盖而不按总条数钉死，避免把合法重复观察判成失败。子测试名与 `consumer_anchor` 保持冻结合同原名（`state_change_emits_followup_notice` / `next_turn_consumes_exec_state`），因为归档 `task-graph.json` 与 v2 合同双向互校；改名需同批改归档图，本轮不做，测试 doc 已明写名字与新语义不一致。
- 新增 G4 `TestLocalSmokeSymptomExecStateSilent`：`rig.drive` 真实 boot 入站；正锚 `three_states_leave_no_turn`（三态各观察后 `requests=2`、`outbound=1` 不变），负锚 `state_still_recorded`（三态审计事实仍落盘、正文为空、无通知分类/任务号，允许重复观察），消费者锚 `Host.Invoke → exec_run → observeExecState → CommitToSession`。

### 契约与预算

- 04 域 delta 的「循环面的模型感知边界」新增第四场景「exec 中间态不构成模型材料、不驱动回合」；主规格同步，scenario 72→73。
- doc budget 基线 22220、slack 0，`--tighten` 拒绝上调：同块合并等量列表项并去掉新场景标题后的空行，主规格净 -1 行；`--tighten` 将 baseline.lines 锁到 22219。
- `golden/runtime/event_integrity_scenarios.json` 保持零 diff；归档 `task-graph.json` 未改。
- `openspec/specs/execution-resource-lifecycle/spec.md` 的冲突条款同步改为“审计事实/正式查询面读取、不驱动回合、不进模型请求”，`下一轮消费执行状态` 场景改名 `状态变化只落审计事实`（+6/-6，总行数不变）。
- `scripts/test_check_runtime_workflow.py` 的 `SYMPTOM_PARENTS` 扩为四锚，`ci.yml` 注释/step 名同步；没有新增 job、没有放宽 no-skip-hatch 断言。

### 验证（原始退出码）

- 集成：`go test ./tests/integration/execution/ -run '^TestEventIntegrityExecStateEvents$' -count=1 -race -json` → exit=0；6 子测试全 pass、0 fail；`-count=5` → exit=0（5/5）。
- G4：`go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptomExecStateSilent$' -count=1 -race -json` → exit=0；`three_states_leave_no_turn` 与 `state_still_recorded` 均 pass；`-count=3` → exit=0（3/3）。
- 四症状：`go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptom' -count=1 -v` → exit=0；4 父测试 + 8 子测试全 PASS。
- 包测：`go test ./runtime/agent/ ./tests/integration/host/ ./tests/integration/execution/ -count=1` → exit=0（三包 ok）。
- 构建/静态：`go build ./...`、根/微信/cli-host 三模块 `go vet ./...` 全 exit=0。
- 全量门：`bash scripts/lint.sh` → exit=0（`lint: ok`）；doc budget 197 篇/22219 行、slack 0。
- 规格：`openspec validate persistent-event-loop --strict --json` → exit=0；scenario 计数 73。
