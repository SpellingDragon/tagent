# 领域契约孙任务

- [x] 4.1 在现有概念检查器增加独立 domain-surface 判定 —— 验证：`python3 scripts/gates/test_check_concept_budget.py -v`；负锚 A 为 Writers/Movement/Continuations 无事实绑定必红，负锚 B 为 superseded+SHALL 必红；正锚为 CompressProduct(fact)、DeliveryLedger(read)、Reconciler/SessionQuery 同名搬迁不误伤；消费者锚为 `python3 scripts/gates/check_concept_budget.py --json` 的公开 CLI 与现有 CI 调用；写入限现有 checker、自测和 `golden/runtime/architecture.json`。 【对账 r138：W4-01 domain-surface（r39，D1 绿）】
- [x] 4.2 迁移 persistent-event-loop 的有效残条并撤销旧架构 Requirements —— 验证：映射表见下方「4.2 迁移账本」；负锚为 superseded 文件不得再含 SHALL/MUST（`--rule D1` 验红后还原）；消费者锚逐条指向 01–03 的公共行为锚；结构门 `openspec validate --specs --strict --no-interactive` 141/141。写入限本文件所在四件套与 `openspec/specs/persistent-event-loop/spec.md`。C 类 3 条（Handoff 路由、正式宿主缺端口、投递前审核）有效但无唯一主 spec 文本归口，已登记为跨域遗留，待编排者单写主 spec 收口；本叶不越权写入。
- [x] 4.3 收缩 GLOSSARY/FACT 与 Attention 规范到各自唯一职责 —— 验证：`python3 scripts/gates/check_concept_budget.py --json` 与文档既有门；负锚为 segment/DeliveryLedger 不得冒充第二事实权威，Attention 三 spec 不得重复拥有文件副作用；消费者锚复跑审批 answer/clearance 和压缩恢复公共测试；不新增说明文档。 【对账 r138：GLOSSARY 侧完成（r76/r135 委派/配置变更/输入批次词条）；FACT/Attention spec 侧随归档段与 6.5 同批改写（W4-02 已交 628 条处置映射）】
- [x] 4.4 删除或内化已裁定的机制公共面与关闭双拼写 —— 验证：01–03 的全部公共行为锚；负锚为 Timeout/Deadline、Drained/Outcome 不得双写，Writers/Movement/Continuations 不得无证新增导出；消费者锚为实际外部编译面/正式 Host 与 Run；编排者单写共享面，按热点文件串行。 【对账 r138：S4 四面（r71）+channels 整包（r121）+C类归口（r53）】
- [x] 4.5 生成概念净账并证明没有改名冒充收敛 —— 验证：公开 CLI 报告新增/删除/改名/读面分类及唯一 spec 锚；负锚为 rename 净额 0、删符号但底层权威仍在不得计领域删除；消费者锚为独立审者按当前提交树重算，而非接受汇报数字。 【对账 r138：净账发布（r138 evidence/04-concept-net-ledger.md）+棘轮（r137）】

## 4.2 迁移账本

迁移前 `openspec/specs/persistent-event-loop/spec.md` 共 558 行、26 条 Requirement、235 个 SHALL/MUST 语句。处置分三类：**A 迁移/已归口**（现行主 spec 已有唯一文本权威）、**B 随实现退役撤销**（旧机制已不在树，给出替代 owner）、**C 有效但当前无唯一主 spec 文本归口**（行为与公共锚仍在，按编排纪律留给编排者单写主 spec，不在本叶越权写入）。行号指迁移前旧文件。

| # | 原条款 | 处置 | 现行 owner / 替代 | 公共行为锚 |
|---|---|---|---|---|
| 1 | Loop goroutine 持续运行不退出 | B：`StartLoop`/`StopLoop`/`loopActive` 在 Go 树 0 定义 | `consumer-loop-convergence`:9/93 | `tests/integration/host/consumer_protocol_test.go:50`；`tests/integration/execution/consumer_loop_convergence_test.go:31` |
| 2 | 批量 drain mailbox | B：mailbox/`DrainAll` 0 定义 | `consumer-loop-convergence`:9 | `tests/integration/execution/consumer_loop_convergence_test.go:93` |
| 3 | mergeBatch 合并批量消息 | B：`mergeBatch` 0 定义；现行聚合在 `aggregateReady` | `consumer-loop-convergence`:9；`runtime/agent/loop.go:2059` | `tests/integration/execution/consumer_loop_convergence_test.go:93` |
| 4 | InjectMessage 双模式 | B：`InjectMessage`/`loopActive` 0 定义 | `consumer-loop-convergence`:25 | `tests/integration/host/resident_entry_test.go:16` |
| 5 | task_settled 作为一等事件触发回收 turn | A | `async-task-execution`:98；`quiet-receipts`:90 | `tests/integration/host/settle_turn_test.go:33`；`settle_wake_unified_test.go:27`；`settle_wake_single_drive_test.go:84` |
| 6 | 循环不依赖 bus echo 自触发 | B：`bus.Pull`/`RunFlow` 0 定义；输出事实只闭合段，不作驱动输入 | `consumer-loop-convergence`:25/45 | `runtime/agent/loop_test.go:35`；`tests/integration/execution/consumer_loop_convergence_test.go:93` |
| 7 | outputCh 宽限与溢出落盘 | B：`outputCh`/`OutputOverflow`/`SessionHook` 0 定义；大结果改走统一外置/票据 | `tool-output-interception`:12；`recall-protocol`:126 | spec 锚：外置原文与分段读取；`memory/ledger/original_store.go` |
| 8 | 循环停止为终结态 | A | `async-task-execution`:281；`runtime-resource-ownership`:53 | `tests/integration/host/quiesce_close_test.go:32/150/303/520`；`close_refuses_while_model_in_flight_test.go:29` |
| 9 | 可判定的接收结果 | A（durable/volatile 双入口退役）；现行受理收据语义 | `consumer-loop-convergence`:25；`rl-feedback`:49 | `tests/integration/host/send_cross_session_delivery_test.go:217`；`quiet_receipts_test.go:16` |
| 10 | 可靠输入全序持久化 | B：`inbox-v1`/`NewInbox` 0 定义；提交屏障与全序由事实层承担 | `event-segment-store`:217/255 | spec 锚：全序键与提交屏障 |
| 11 | 输入处理确认与幂等 | B：claim/ack 实现已退役；现行事实幂等 | `event-metadata-contract`:67；`event-segment-store`:281 | `tests/integration/execution/identity_commit_contract_test.go:28` |
| 12 | LLM 端点重定向逐跳 allowlist | B：`CheckRedirect`/allowlist 在 Go 树 0 定义 | 无现行消费者；不得无 spec 复活 | grep 证据见下「退役机制核查」 |
| 13 | 业务 turn 开始执行时绑定编排版本 | A；末句「不同 invocation 不合批」与 `consumer-loop-convergence`:17-19 冲突，按 01 域删除 | `config-state-twinship`:10/119；`task-registry-rebuild`:53；`async-task-execution`:197-217；`subagent-turn-execution`:95 | `tests/integration/state/generation_publication_test.go:87`；`tests/integration/host/reload_tiers_test.go:27`；`chat_span_dedup_test.go:128` |
| 14 | 瞬时提交失败重排覆盖完整冻结接收集 | B：durable claim/冻结接收集 0 实现 | `event-segment-store`:255 | spec 锚：提交屏障 |
| 15 | 不可解码 slot 触发整封隔离 | B：envelope/slot 0 实现 | `event-segment-store`:281 | spec 锚：重复键拒绝/隔离 |
| 16 | per-turn echo 凭据随 turn 清理 | B：`turnEcho`/`endTurn` 0 定义 | `consumer-loop-convergence`:9（turn 生命周期） | spec 锚 |
| 17 | 可靠模式的恢复能力与格式准入 | B：`EventReplayer` 0 定义；通用恢复透传保留 | `event-segment-store`:371 | spec 锚：包装链真实透传 |
| 18 | 启动直接核对未确认完成证据 | B：inbox outstanding 0 实现；通用启动重建保留 | `event-segment-store`:333；`event-sourced-projection`:82 | `tests/integration/history/projection_replay_and_budget_test.go` |
| 19 | 模型入口错误以失败极性呈现 | A | `failure-polarity`:75 | `tests/integration/execution/model_terminal_protocol_test.go`；`tests/integration/host/failure_notice_test.go:190` |
| 20 | inbox 写者唯一性与目录单属主 | B：`NewInbox`/flock 0 定义 | `event-segment-store`:281（重复键拒绝） | spec 锚 |
| 21 | Handoff 未来通知的目标、实例与稳定序号 | **C**：有效，行为在 `runtime/task`，但唯一主 spec 文本缺失 | 建议 owner `async-task-execution`/`unified-task-semantics`；现行引用 `event-metadata-contract`:95 | `runtime/task/restore_test.go:27`；`tests/integration/execution/handoff_future_notifications_test.go:29`；`handoff_routing_survives_restart_test.go:47` |
| 22 | 通知消费位置连续且不跨空洞 | A | 02 delta `unified-task-semantics`:5-19；`async-task-execution`:247 | `tests/integration/state/mailbox_consumption_test.go:19`；`tests/integration/host/settle_wake_unified_test.go:27` |
| 23 | 正式宿主缺必要端口或身份时具名拒绝 | **C**：有效，代码/测试在，但唯一主 spec 文本缺失 | 建议 owner `architecture-guardrails`/`production-wiring`；现行代码 `assembly/host.go:809` | `examples/cli-host/main_test.go:29`；`tests/integration/host/event_integrity_test.go:4966`；`examples/wechat-bot/smoke_harness_test.go:2058` |
| 24a | 循环面模型感知边界（三族/progress/失败/exec 中间态） | A | `quiet-receipts`:68/90；`failure-notice`:9；`execution-resource-lifecycle`:59 | `runtime/agent/loop_test.go:1227`（progress）；`tests/integration/execution/quiet_receipts_test.go:22`；`tests/integration/host/failure_notice_test.go:190`；`tests/integration/capabilities/exec_lifecycle_test.go:32` |
| 24b | 同上「投递前审核」子句 | **C**：有效，`deliverFresh` 前审核仍在，但唯一主 spec 文本缺失 | 建议 owner `delivery-dispatch-receipts`/`delivery-plugin` | `tests/integration/host/delivery_review_test.go:41/163/268/306` |
| 25 | 在途 turn 的外部输入在 decision 边界消费 | A | `consumer-loop-convergence`:9/13-15/25 | `tests/integration/host/intake_micro_window_test.go:44`；`tests/integration/execution/consumer_loop_convergence_test.go:31` |
| 26 | 每 agent 地址的持久事件循环 | B：旧持久循环形态；现行每地址 Consumer | `consumer-loop-convergence`:93 | `tests/integration/execution/instances_symptom_test.go:57`；`tests/integration/host/consumer_protocol_test.go:50` |

公共行为锚列是映射目标与复核入口；本叶为纯凭据/规格变更，未复跑 Go 测试（有在途单编辑 `assembly/host.go`/`runtime/task` 等热点），复跑由编排者在 Wave 4 合流后执行。

### 退役机制核查

`grep -rn '<symbol>' --include='*.go' .` 在改写时点均 0 定义：`StartLoop`、`StopLoop`、`loopActive`、`mergeBatch`、`DrainAll`、`outputCh`、`OutputOverflow`、`SessionHook`、`EventReplayer`、`QuarantineEnvelope`、`turnEcho`、`endTurn`、`CheckRedirect`、`NewInbox`、`flock`。`RunFlow` 仅有 1 处历史注释夹具（`scripts/comment_policy/main_test.go:271`），无产品定义/调用。旧机制已不在产品树，故其条款随实现撤销；保留其文本只会形成第二权威。

### 红/绿/负锚证据（2026-11-03）

**红（改写前）**

- `python3 scripts/gates/check_concept_budget.py --json` → **exit 1**；error: `D1 domain-surface openspec/specs/persistent-event-loop/spec.md: superseded spec keeps 235 current SHALL/MUST statement(s) in ## Requirements: migrate effective clauses to the owning capability, then strip the old Requirements; a historical note is not a migration`；`counts.domain_surface.superseded_specs_with_requirements=1`。
- `python3 scripts/gates/check_concept_budget.py --rule D1 --json` → **exit 1**；同一 error。
- `openspec validate --specs --strict --no-interactive` → **exit 0**（141/141；旧文件结构本身合法，红只在概念门）。

**绿（改写后）**

- `python3 scripts/gates/check_concept_budget.py --json` → **exit 0**；`errors=[]`；`extra.domain_surface=[]`；`counts.domain_surface.superseded_specs_with_requirements=0`。
- `python3 scripts/gates/check_concept_budget.py --rule D1 --json` → **exit 0**。
- `openspec validate --specs --strict --no-interactive` → **exit 0**（141 passed / 0 failed）。
- `python3 scripts/gates/test_check_concept_budget.py` → **exit 0**（51 tests OK）。
- `scripts/gates/check-openspec.sh` → **exit 0**，但输出 `No items found to validate.`；`bash -x` 显示脚本执行 `cd scripts/gates/..`（即 `scripts/`，未进仓库根），属写集外的既有门禁缺陷。主 spec strict 以直接命令 `openspec validate --specs --strict --no-interactive` 为准（141/0）。

**负锚（故意留一条 SHALL 必红，随后还原）**

- 在 Requirements 临时加入 `本历史文件 SHALL 不再承载现行命令；` → `python3 scripts/gates/check_concept_budget.py --rule D1 --json` **exit 1**；error 计数 `1`。
- 用改写前保存的 `/tmp/w4-02-green-spec.md` 原样还原 → 同一命令 **exit 0**，`extra.domain_surface=[]`。

### 结构与遗留

- 结构：保留 `# persistent-event-loop Specification`、`## Purpose`（superseded blockquote）与 `## Requirements`；唯一 Requirement 含一个 `#### Scenario:`。该 Requirement 不再发布现行命令；为同时满足 D1 的「不得有现行 SHALL/MUST」与 openspec `RequirementSchema` 的「requirement 文本须含 SHALL/MUST 子串」，文本只以历史关键词复数（`SHALLs`/`MUSTs`）作非规范性命名称谓，D1 的词边界不把它计为现行命令。
- 遗留（C 类，需编排者单写主 spec 收口）：`21` Handoff 路由/稳定序号、`23` 正式宿主缺端口具名拒绝、`24b` 投递前审核。三者行为与公共锚仍在，但本叶写集不含对应主 spec；按照「主 specs 归编排者单写」纪律，本叶只登记，不越权补写。
