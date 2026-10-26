# 1.0 findings — 投递 hold → 批准补投 → 恰一条

## 结论

麻雀闭环已红转绿：审核插件以 `Delivery{Content, Lineage, Target}` 裁决投递；发送插件在 `deliverFresh` 的每一条 Send 前查询裁决；`hold` 只挂起不投，批准后恰一条补投，拒绝零投递并留无载荷审计事实。默认无 ruler = allow，既有用户路径行为不变。

## 红证（先于实现）

- 命令：`go test ./tests/integration/host/ -run '^TestDeliveryReviewHold$' -count=1 -v`
- 证据：`evidence/1.0-red.txt`
- 现场：仅声明 `Delivery`/`DeliveryRuler`/`WithDeliveryRuler`，`deliverFresh` 未查询裁决。测试注入 hold 后仍收到正文：
  - `delivery_review_test.go:54: delivered bodies = ["the body the delivery review holds"], want none while the review holds the body`
  - `delivery_review_test.go:112: held deliveries = [], want the one body the review held`

## 实现要点

- `api/plugin/delivery.go`：`Delivery`（Content/Lineage/Target）与 `DeliveryRuler.Rule(ctx, Delivery) (Disposition, error)`；三态沿用 `DispositionAllow/Record/Hold`，hold 是等待。
- `host.go`：
  - `WithDeliveryRuler(ruler)` 注入；nil ruler 直接投递（D5 默认 allow）。
  - `deliverFresh` 的正文与失败通知都经 `deliverOne`：allow/record 投；hold 调 `holdDelivery` 挂起。
  - 血统取 `InvokeRequest.TriggerSource`（复用 trigger_source）；空串按用户回合记为 `user`。
  - 挂起表 `pendingDeliveries` 为内存表（持久化留 TODO），身份为 host 内唯一的 `delivery-%06d`；`Host.mu` 保护。
  - `HeldDeliveries()` 返回挂起条目副本（内容+血统+目标+ID+挂起时刻），按身份排序。
  - `ApproveDelivery(ctx,id)`：消费挂起后补投一次；重复批准返回 `ErrDeliveryNotFound` 且不再投；通道拒绝发送时把挂起放回，供再次批准。
  - `RefuseDelivery(ctx,id)`：消费挂起、零投递，并提交一条 `TypeFeedback` 审计事实：`trigger_source=host`、`event_summary=delivery refused`、`meta_delivery_id/meta_lineage/meta_target_agent/meta_target_session/meta_content_bytes`，`Message` 为空（无模型载荷）。
  - 裁决返回错误或未知 disposition：挂起（fail closed），不投未经裁决的正文。
  - 批准补投不再查询 ruler：hold 已是该投递的终局裁决，重复裁决会让“批准后补投”可能再次挂起。

## 批准 / 拒绝语义

- 批准 = 消费一条挂起并把原正文原样补投到原目标，恰好一次；不是重新生成，也不是重新裁决。
- 拒绝 = 消费一条挂起并丢弃正文；零投递，审计事实保留投递语义（身份/血统/目标/字节数）但不保留正文。
- 两种决定都只消费一次：重复决定按身份拒绝（`ErrDeliveryNotFound`）。

## 绿证

- `go test ./tests/integration/host/ -run '^TestDeliveryReviewHold$' -count=3` → `ok github.com/SpellingDragon/tagent/tests/integration/host 0.812s`
- 测试覆盖：
  - hold 后用户渠道 0 条；ruler 收到恰一条语义（正文+`user` 血统+显式目标）；挂起条目存在且内容/血统/目标一致。
  - 批准后恰一条补投（同正文），挂起清空；重复批准返回 `ErrDeliveryNotFound` 且仍只有一条。
  - 拒绝后 0 条、挂起清空、审计事实存在且无 `Message` 载荷，元数据可追溯；重复拒绝按身份拒绝。

## 回归

- `go test ./tests/integration/host/ ./runtime/agent/ -count=1` → 两包 ok。
- `go -C examples/wechat-bot test . -run '^TestLocalSmokeGuards$|^TestLocalSmokeSymptom' -count=1 -tags smoke` → ok。
- `go build ./...`、`go vet ./` → 通过。
- `bash scripts/lint.sh` → `lint: ok`。

## 关于既有审批通道的复用判断

既有 governance 审批通道（`runtime/task/approval.ApprovalManager` + `runtime/task.HoldBook`）的对象是工具调用：身份由工具名+参数摘要绑定，批准后的执行者是任务执行路径。投递语义没有工具名与参数摘要，且批准后的动作是“把正文补投到目标”，无法由既有通道直接完成；把正文塞进 args digest/preview 会伪造工具调用语义。故 1.0 采用 host 内的最小挂起表，1.1/1.2 再把投递裁决接入 governance 的规则面。

## 偏离与后续

- `run.go` 增加一个 `assemblyConfig.deliveryRuler` 载字段（`HostOption` 的唯一落点），无行为默认。
- 概念预算门要求新增导出接口配 canonical example：新增 `examples/contract/api_plugin/DeliveryRuler_example.go`；`scripts/gen_godoc.sh` 重生成 `docs/api` 三页。
- 挂起表不持久化（TODO）；进程重启会丢未决投递。
- `DispositionRecord` 当前只投递、不写投递审计账；投递账本属 1.1 的 delivery-plugin 能力。

# W1+W2 findings — 出口统一、entry 去 send、症状与合同

## 1.1 出口统一（跨代理 send 与正文同一条投递出口）

- `Boot` 在装配前声明 `var host *Host`，用 `deliverSend` 闭包把 `host.deliverOne` 交给装配面：`doc.nativeAgents(..., deliverSend, ...)` → `hostSendTool{sender, deliver}`。装配早于 `*Host` 发布，闭包在调用时读变量，`host == nil` 时返回 `deliveryRefused`（拒绝发送，不投未裁决正文）。
- `deliverOne` 由“只记日志”改为返回 `(deliveryOutcome, cause)`：`deliverySent / deliveryHeld / deliveryRefused` + 挂起或拒绝的原因。`deliverFresh` 的正文与失败通知、`hostSendTool.Call` 的正文都走这一个函数与同一 `deliveryRuler`，因此三者的 hold/审计语义一致；`[send_held]` / `[send_denied]` 只由返回值渲染，不另建裁决分支。
- 血统：`driveLocked` 以 `withDeliveryLineage(ctx, lineage)` 把回合 `TriggerSource` 打到 ctx，`hostSendTool.Call` 用 `deliveryLineageFrom(ctx)` 读取；`deliveryLineageOf` 仍负责空串归一为 `user`。这样跨代理 send 不再固定 `"send"`，与正文同血统。
- 接线（生产消费者）：`host.go:299-312`（Boot 闭包）→ `doc.nativeAgents`（`host.go` 内 `nativeAgents` 装配 send 面）→ `hostSendTool.Call` → `h.deliver` → `Host.deliverOne` → `sender.Send`。集成测试从 `Host.Invoke` 进，闭包读到的就是 Boot 发布的 host，不是测试手工接线。
- 记账：`DispositionRecord` 已有“投递发生即记账”语义，本单不新增投递账本（最小改动；账本属投递审计的后续能力）。

### 锚

- 正锚 `TestDeliveryReviewSend/an_allowed_send_reaches_the_review_and_the_channel`：reviewed = 2（send 正文在前、closing 在后），delivered = 2，`[send_held]`/`[send_denied]` 都不出现，`HeldDeliveries()==0`。证明 send 正文确实经过裁决且裁决输入含 `{Content, Lineage:"user", Target:addr}`。
- 负锚 `TestDeliveryReviewSend/a_held_send_waits_and_its_receipt_is_not_a_delivery`：hold 时 delivered = 0、模型读到 `[send_held] `、held = 2（send 正文 + closing），批准后 delivered = 1 且正是 send 正文。证明 send 被 hold 时挂起而非投递，且回执不被报告为投递。
- 命令：`go test ./tests/integration/host/ -run '^TestDeliveryReview' -count=1 -v` → `PASS`（Send 两子测试、DefaultAllow、FailureNotice 全过；包 `ok ... 2.048s`）。
- 消费者锚：`HostOption`/`Boot` 正式入口路径（上面的接线链），非测试内直连 `deliverOne`。

## 1.2 governance 收口（默认 allow、规则可 hold）

- 复核麻雀实现：`deliverOne` 在 `h.deliveryRuler == nil` 时不查询裁决直接 `sender.Send`；ruler 返回 `DispositionHold` 时 `holdDelivery` 并返回 `deliveryHeld`；`Rule` 报错或未知 disposition 也挂起（fail closed）。
- 补默认 allow 断言 `TestDeliveryReviewDefaultAllow`：无 ruler 时 send 正文与 closing 正文各投一条（`bodies[0]=send body`、`bodies[1]=closing`），`HeldDeliveries()==0`。该测试同时覆盖“send 面在无 ruler 时不因新出口而丢正文”。

## 1.3 entry 去 send + 测试迁移

### 生产与台架文档

- `examples/wechat-bot/tagent.yaml`：entry agent 的 `tools` 移除 `{ kind: tool, id: send }`；`send` 工具定义保留（跨代理工具，只是不装配到 entry）。
- `examples/wechat-bot/smoke_harness_test.go`（原 :196）与 `smoke_exec_test.go`（原 :196）的文档配置：移除 `tools` 里的 send 与 `risk_rules` 的 `{ tool: send, level: critical }`。
- 审批通道仍需一个 critical 工具来练：新增 `smokeApprovalDocument` 夹具，在默认（无 send）文档上重声明 send 与 critical 规则；只有审批类场景用它（`smokeErrorsShortTTLDocument`，以及 `bootSmokeIntegrityHost` 这条 event-integrity 控制测试的共用装配）。这样生产入口无 send，审批通道的覆盖不缩水。

### 迁移台账（3 例）

| 原测试 | 迁移后 | 对象替换 | 断言语义（保持） | 结果 |
| --- | --- | --- | --- | --- |
| `Wave2Exec/watchdog_closing_round_refuses_the_third_send_call` | `Wave2Exec/watchdog_closing_round_refuses_the_third_tool_call`（`smokeExecWatchdogRefusal`） | 拒绝对象 send → `exec_run` | 3 次决策、第 3 次带 closing 指令、`exec_run` 的拒绝回执进 transcript（`"this call was not admitted"`）、无审批登记、exec 输出 0 条到达用户 | PASS |
| `Wave2Exec/watchdog_closing_round_delivers_its_closing_body`（同名） | 同名（`smokeExecWatchdogClosing`） | “收尾轮 send 被拒” → “收尾正文投递经审核出口” | closing 正文先 0 条；`smokeReviewRuler{holdPrefix:"closing summary "}` 确实见到正文（`Lineage=="user"`）；`waitHeldDelivery` 得到挂起；Approve 后恰 1 条；3 次决策；无审批登记；无拒绝回执 | PASS |
| `Wave2Errors/expired_approval_keeps_the_retried_call_held` | **未迁移（待裁）** | — | 场景断言依赖 `pending.ExpiresMs-CreatedMs`、`ErrApprovalExpired`、`ApprovalManager.Lookup(sendToolID, digest)`、重试调用仍持旧 digest —— 投递语义没有 TTL、摘要与“重试执行” | 保留在审批通道（当前绿），见“2.1 待裁” |

迁移前后对照（基线 e6b67aa vs 去 send 后）：三例在基线全 PASS（`/tmp/baseline_wave2.log`：expired 30.12s、exec refusal 30.09s、exec closing 30.08s）；去 send 后 expired 与两例 exec 全红（entry 工具面无 send，脚本的 send 调用被拒/无登记）。两例 exec 以“换对象”迁移转绿；expired 因语义不可等价迁移保留。

### 台架带来的两处必要改动

- `smokeProvider` 现在记录每次请求的 `Tools`（工具名），`smokeModelRequest.Tools`，供 G-A 断言 entry 工具面。这是旁路记录，不改变脚本回答。
- `bootSmokeRigWithPort` / `reboot` 把 `cfg.DeliveryRuler` 透传为 `tagent.WithDeliveryRuler`，用于收尾轮与 G-B 注入 hold 规则。

### `TestLocalSmokeEntryHasNoSendTool` 的命名

树中不存在该测试名（`grep -n "TestLocalSmokeEntryHasNoSendTool" examples/wechat-bot/*.go` 无命中）；其语义由 G-A 的 `TestLocalSmokeSymptomNoSendTool` 承载，修掉了此前的两个失败原因：
- 消息含 `smoke delegate` 触发词（会让 entry 转委派，工具面与正文都不是本症状要的形态）→ 改为 `SMOKE-R97xx entry has no send tool`，不含触发词；
- marker 不是 `SMOKE-R<数字>`（台架 `smokeMarkerRE` 不认）→ 改为 `SMOKE-R9701` 起的数字 marker。
负锚：同一断言里要求 entry 工具面仍含 `smoke-worker`（否则“工具面为空”也能假通过）。

## 2.2 症状 G-A / G-B

- G-A `TestLocalSmokeSymptomNoSendTool`（子测试 `entry tool face has no send` + `one automatic delivery`）：正向——entry 的模型请求工具面含 `smoke-worker`、不含 `send`；正文恰一条到达渠道且来自自动投递（无 send 调用）。负向——工具面含 `smoke-worker` 防“空工具面”假通过。消费者——从 `tagent.Boot` + 真实 wechat 入口驱动。
- G-B `TestLocalSmokeSymptomDeliveryHold`：
  - `hold waits then approves once`：注入 `smokeReviewRuler{holdPrefix: [body]}` → outbound 0 条 → `waitHeldDelivery` → `ApproveDelivery` → 恰 1 条；
  - `refuse delivers nothing and audits`：`RefuseDelivery` → 0 条 + 审计事实（`delivery refused`，无 `Message` 载荷，元数据含投递身份/血统/目标/字节数）。
- 两者都进入 `^TestLocalSmokeSymptom` 前缀；`scripts/test_check_runtime_workflow.py` 的 `SYMPTOM_PARENTS` 已登记这两个父测试，CI symptom job 会带上它们。

## 2.3 合同

- 路径选择：**直接改主规格 + 同步 delta**（二选一中的前者）。理由：`循环面的模型感知边界` 已在主规格存在，本次只加一个场景，直接改主规格最小；同时把完整 MODIFIED requirement 正文写进 `openspec/changes/delivery-review-plugins/specs/persistent-event-loop/spec.md`，保证 `openspec validate --strict` 与归档合并一致。
- 新场景“投递前审核是投递的必要条件”：正文/失败告知/跨代理 send 三条来源；allow/record 恰一次；hold 不投、挂起、批准后原样恰一次且不被报告为投递；拒绝零投递 + 无载荷审计；无审核时默认 allow。
- v1/v2 合同逐行核对（`golden/runtime/scenarios.json`、`golden/runtime/event_integrity_scenarios.json`）：
  - v1 `scenarios.json`：无审批或 send 相关场景（`grep -i "approv|send|hold"` 只命中 `memory/replay` 的无关词）。零改。
  - v2 `02.5/TestEventIntegrityApproval`：子场景 `approved_then_executes_once`、`changed_args_requires_new_approval`、`expired_response_refused`、`unapproved_never_executes`、`rejected_never_executes`、`duplicate_response_no_second_execution`、`wrong_target_no_overreach`、`channel_delivery_failure_does_not_block`、`formal_governance_task_wrapper`——断言对象是工具调用审批（digest/TTL/执行），审批通道本次未删除，测试仍绿。零改。
  - v2 `02.6/TestEventIntegrityApprovalWeChat`：`approval_request_fanout`、`parse_approve_reject`、`ambiguous_prefix_refused`、`non_approver_refused`、`unknown_digest_refused`——微信审批解析面，不依赖 entry 是否声明 send。零改。
  - v2 `03.4/TestEventIntegrityApprovalControl`（smoke）：`positive/negative/consumer` 三子测试走 `smokeApprovalDocument`（重声明 send 的审批夹具），断言语义未变。零改。
  - 结论：合同零改；`golden` 两文件未出现在 diff 中即证据。
- doc budget 具名调整：`scripts/doc_budget.json` lines `22291→22295`（+4 = 新场景 4 行），`files` 不变；`rebaseline_reason` 末尾追加 `2026-10-26 delivery-review-plugins 2.3: +4 lines ... no other document touched, batch-end reconciliation`。`--tighten` 拒绝上升，故具名记账后落基线。
- 概念预算：`scripts/check_concept_budget.py` 的 S3 ctx-bare 登记新增 `host.go:*` 1 处（`deliveryLineageFrom`：回合血统经类型化 accessor 落到 send 投递），属新 accessor 的合法单次 `ctx.Value`。

## TestLocalSmoke 冥想检查的修正（台架稳定性）

- 现象：迁移审批轮后 `TestLocalSmoke` 在“冥想摘要落入用户会话链”一步超时（`reflect requests=2`；基线同测试 PASS、`reflect requests=4`）。
- 根因（临时诊断取证后已还原 `evolution/meditation/driver.go`，工作树无残留）：审批轮从“send 调用→hold→批准→retry 执行”多回合缩短为“投递 hold→批准”单回合，泵时长从约 66s 降到约 8s。一次反思在泵开始前 fire（`12:36:09.79`，泵 start `12:36:10.30`），摘要投给当时唯一活跃会话；重启（`12:36:15.48`）后 bindings 不持久，旧会话链不可见；`min_gap=10s` 到 `12:36:19.79` 才满，而泵已于 `12:36:18.46` 结束，驱动是事件驱动（无新 host-facing 提交不再 fire），原 120s 死等永远等不到。
- 修法：等待循环改为“每轮先扫三条会话链的事实；未见到就驱动一条真实探针回合（`SMOKE-R75NN meditation probe`，轮询 a/b/c）并等其 answer，再 sleep 1s”。断言语义不变（摘要必须落入用户会话链、且由下一轮请求读到），只是把“等事件”改成“造事件”。
- 结果：`^TestLocalSmoke$` PASS 41.78s（`/tmp/smoke_fix1.log`），摘要落入 a/b/c 三条链，`reflect requests=4`。

## TestLocalSmokeWave2Recall 定性（既有红）

- 基线 `e6b67aa` 复现（`git worktree add /tmp/tagent-baseline e6b67aa`）：
  - `go -C examples/wechat-bot test . -run '^TestLocalSmokeWave2Recall$' -count=1 -tags smoke -timeout 600s`
  - → `smoke_recall_test.go:140: no text to smoke-chat-a carrying "recall recent count=2" after 30s (recorded=2)`；`--- FAIL: TestLocalSmokeWave2Recall (30.15s)`；`FAIL wechat-bot 30.560s`。
- 定性：既有红，与 entry 去 send / 投递审核无关（基线同样红，报错相同）。本次验收面照实报告该例外，未以任何方式改动该测试或放宽断言。测试脚本 `recall_recent {"limit":5}` 期望 `count=2`，实际返回 5，属 recall 工具语义与脚本期望不一致的独立缺陷。

## 2.1 待裁

- 事实：`Wave2Errors/expired_approval_keeps_the_retried_call_held` 的断言全部落在工具审批对象上（approval TTL、`ArgsDigest`、`ExpiresMs`、`ErrApprovalExpired`、重试仍持旧 digest）；投递审核的 `Delivery{Content, Lineage, Target}` 没有 TTL、摘要、也没有“重试执行”这一步。语义无法等价迁移。
- 处置（按“语义无法等价迁移→停报待裁，不删场景”）：场景保留在审批通道，当前绿；2.1 不勾。
- 选项：
  - A. 接受“2.1 部分迁移”：两例 exec 已换对象，expired 留在审批通道并在 tasks 注明理由；
  - B. 给投递挂起补 TTL + 摘要（超出本 change 的 Delivery 契约，需先改 `api/plugin/delivery.go` 与规格）；
  - C. 把 expired 场景交给后续 approval→delivery 迁移单（若产品决定投递挂起也需要过期语义）。

## 门禁

- `go -C examples/wechat-bot test . -run '^TestLocalSmoke' -count=1 -tags smoke -timeout 1800s` → 117.208s，除 `TestLocalSmokeWave2Recall` 外全绿（`/tmp/smoke_full2.log`）；`TestLocalSmoke` 41.81s、`SymptomNoSendTool` 0.41s、`SymptomDeliveryHold` 4.57s。
- 五包集成：`go test ./tests/integration/{host,journey,capabilities,evolution,execution}/ -count=1 -timeout 1800s` → 五包 `ok`（23.029s / 12.202s / 1.595s / 1.556s / 12.947s）。
- `go build ./...` → ok；`go vet ./...` → exit 0；`bash scripts/lint.sh` → `lint: ok`；`openspec validate delivery-review-plugins --strict` → `Change 'delivery-review-plugins' is valid`。

## 2.1 裁定（编排者，2026-10-26）

选项 A 采纳：Wave2Exec 两例等价迁移（对象 send→exec_run 与投递 hold）；`Wave2Errors/expired_approval_keeps_the_retried_call_held` 保留在工具审批通道——投递挂起不实现 TTL/参数摘要/重试执行，那是**工具审批通道**的固有语义；两个通道并存（工具审批 vs 投递审核）是本案的设计结果，不是遗漏。

# F 收口（2026-10-26）

## F1 契约零改动

- `e6b67aa..HEAD`：`golden/runtime/scenarios.json` + `golden/runtime/event_integrity_scenarios.json` 合计 diff **0 字节**（v1/v2 均零改）。
- 主规格只有 `openspec/specs/persistent-event-loop/spec.md` 一个文件进入 diff，`+4/-0`：新场景「投递前审核是投递的必要条件」；既有 Requirement/Scenario 未改。
- `openspec validate delivery-review-plugins --strict` → `Change 'delivery-review-plugins' is valid`（exit 0）。
- 证据：`evidence/F1-contract.txt`。

## F2 全量门禁（9 项，逐条原始退出码）

| # | 门 | 结果 |
| --- | --- | --- |
| 1 | `bash scripts/lint.sh` | exit 0，`lint: ok` |
| 2 | 根 `go build/vet/test ./... -short` | 0 / 0 / 0（38 包 ok、0 FAIL） |
| 3 | 微信 + cli-host 三连 | 两模块各 0 / 0 / 0 |
| 4 | short 十轮（`--repeat 10`） | exit 0；10 个互异 run_id；每轮 28 test_id / 140 子测试；≈28m |
| 5 | long 六片（1/7/42 × 256/32768，`--timeout 1200`） | 6/6 `verdict=match`；每片 1200 inputs/120 restarts，合计 7200/720 |
| 6 | mutations `--all` | exit 0；control 树先绿；11/11 killed；`gate.ok=true` |
| 7 | bundle 总门（`--bundle`） | `ok=true`；17 份证据（10 short + 6 long + 1 mutation）；9 包/180 测试 |
| 8 | 症状 job（`^TestLocalSmokeSymptom`） | exit 0；6 父测试 / 10 子测试全 pass、0 skip（G1–G4 + G-A/G-B） |
| 9 | `test_check_runtime_*.py` + `check-openspec.sh` | 233 tests OK（exit 0）；main specs 全 strict（exit 0） |

- **一次受控重跑（红证保留）**：7/256 首轮 `unattested`——`generation 56 / round 149` 的 60s 墙钟等待超时（`recorded=41`），语义断言未失败；同树同合同另两片 match，当时 3 long + short 十轮并发且宿主常驻负载高，定性为并发加压下的等待超时。红证原样留在 `long-s7-w256/`；并行面清空后**独占重跑一次** → `verdict=match`（run_id `b6ae2edf-f99c-4466-b75c-33e8ce2a5e1a`）。bundle 用重跑证据，红证不入 bundle。
- bundle 首次组装只复制 evidence.json 时 exit 1（16 条 `evidence.test_log not found`：checker 按证据目录解析 test_log），补齐每目录 `test-log.jsonl` 后复跑 exit 0；过程如实记入证据。
- 证据：`evidence/F2-gates.txt`（原始日志在 `$TMPDIR/drp-final/`）。

## F3 回滚即红（两红两绿，DoD 已由 F1/F2 覆盖）

- a）entry 去 send 回滚：`tagent.yaml` + 台架默认文档临时恢复 `- { kind: tool, id: send }` →
  - G-A 红 1：`smoke_test.go:3635` 部署入口仍声明 send（exit 1）；
  - G-A 红 2：仅保留台架文档 send → `smoke_test.go:3653` 入口工具面仍授予 send（exit 1）；
  - 还原：两文件 `git checkout --` → blob==HEAD、shasum 与基线一致、diff 0 字节 → G-A exit 0。
- b）投递裁决点短路：`host.go` `deliverOne` 将 `ruler != nil` 改为 `false && ruler != nil`（跳过审核查询）→
  - G-B 红：两个子测试都等不到挂起条目（`smoke_test.go:3688/3715`，`held=[]`，exit 1，60.25s）；
  - 还原：blob==HEAD、shasum `6c70ea08…`、diff 0 字节 → G-B exit 0（4.93s）。
- 证据：`evidence/F3-redgreen.txt`（含两次破坏的 diff、编译退出码、具名断言行、还原三重校验）。

## F4

- 未做：归档与导览留主代理授权；`tasks.md` 的 F4 保持未勾。

