# 交付闭合（事件化）：孙任务

- [x] 1.1 结算事实族（assembly 包内）：`delivery.settlement` 类型+身份（fact key 确定性导出）+FACT-VOCABULARY 登记行+幂等/同键冲突锁定+**TTL 豁免注册**（注册负 TTL 是前瞻豁免；`TestSettlementTTLEntryRegistered` 断言注册形态并注明 debt=当前无生产挂载；**恒可回读为两段式：当前由 Lifecycle 未挂载保证，挂载后由负 TTL 豁免保证**）—— 验证：`go test ./assembly -v -run 'TestDeliverySettlementFactFamily|TestSettlementTTLEntryRegistered' -count=1` 用例数=2 PASS。
- [x] 1.2 同笔原子：agent_output.Meta 携带 owed 声明；**锚①转绿——注入点随事件化迁至提交边界（W1 内演进；用户可见断言不变=「交付账写不进⇒正文 0 发+提示可达」，收件方收到恰 1 条失败通知——该通知断言由失败告知（支①）承担，r8 F1）**+负锚 `TestObligationAtomicWithOutput`+`TestDeliveryCommitFailureBlocksSend`（提交失败两不立）—— 验证：`go test ./assembly -v -run 'TestSymptomAnchorDeliveryRegistrationStopsSend|TestObligationAtomicWithOutput|TestDeliveryCommitFailureBlocksSend' -count=1` 用例数=3 PASS。
- [x] 1.3 DeliveryConsumer 与错误分类：互斥分类器（拒连非超时=明确失败；纯超时=未知）；锚③转绿（`TestSymptomAnchorRetryAutoRedrive`）+ `TestUnknownResultNeverAutoRedriven`（**纯超时形状**负锚：不自动重发落 held）+ `TestRetryExhaustionFallsToHeld`（重试耗尽落 held(reason=retry-exhausted)，不是独立终态）+ `TestRetryGapWritesHeldRetrying`（**首次重试前写 held(reason=retrying)** 结算事实；成功后 sent 覆盖对账；held 的 reason ∈ {approval, retrying, retry-exhausted, unknown}）+ `TestRetryAttemptsCarryActionIdentity`（每次重试关联原动作身份，未确认 sent 才重发）+ `TestDeliveryConsumerNotSelfTriggered`（DeliveryConsumer 不以 ObserveCommit 自监听；退避是状态机自持计时，结算事实是只读结果面）—— 验证：`go test ./assembly -v -run 'TestSymptomAnchorRetryAutoRedrive|TestUnknownResultNeverAutoRedriven|TestRetryExhaustionFallsToHeld|TestRetryGapWritesHeldRetrying|TestRetryAttemptsCarryActionIdentity|TestDeliveryConsumerNotSelfTriggered' -count=1` 用例数=6 PASS。
- [x] 1.4 恢复只读链（F1/F2/F3/F14/F15 定版）：锚②转绿（`TestSymptomAnchorDeliveryWindowSurvivesRestart`）+ `TestRebuildSkipsSettledBodies`（已结算不重发）+ `TestRecoveryResumesInterruptedRetry`（补送范围=无结算事实 ∪ held(reason=retrying)；重启在退避途中后续跑、不搁浅）+ `TestMultiSessionRecoveryBounded`（**计数口径=返回的匹配事实条目数**：结算事实与 owed 声明均经 `r.facts` 事实窗口内读取；`r.replays` 的映射序列仅用于会话枚举；结算事实经 `memory/query_events.go` 的 `QueryOptions` 未导出 `keyPrefix`/`Prefix()`/`WithKeyPrefix()` 按 `delivery.` 前缀过滤**返回集**；代价 O(事实窗口内)；断言注入无关 agent_output 不抬高匹配数、未清义务全在结果中；不另设扫描量断言）+ `TestSettlementFactsStayHotWhileUnsettled`（未清 held/owed 交付事实驻留事实窗口内、不落事实窗口外；DeliveryConsumer 持有的义务恒驻事实窗口内）—— 验证：`go test ./assembly -v -run 'TestSymptomAnchorDeliveryWindowSurvivesRestart|TestRebuildSkipsSettledBodies|TestRecoveryResumesInterruptedRetry|TestMultiSessionRecoveryBounded|TestSettlementFactsStayHotWhileUnsettled' -count=1` 用例数=5 PASS。
- [x] 1.4b 双断恢复实测：`TestDanglingTurnAfterDualFailure`——先构造真实悬空轮（input 已提交、output 提交前杀驱动）并断言 host 处理后：无通知发出**且**悬空轮在链上可辨识（input 事实存在、无 agent_output）——两个正向断言+一个负向断言；**必须 PASS（无 skip 出口）**；若实测暴露无人知晓的悬空轮→当场转法定停点上报，**不得 skip 结项** —— 验证：`go test ./assembly -v -run 'TestDanglingTurnAfterDualFailure' -count=1` PASS。
- [ ] 1.5 旧 KV 六态迁移：`TestKVDeliveryMigratedToFacts`（**旧 KV 六态**含 headless→retired 不重发+正文留档；重启两次幂等）+ `TestKVRecordUnparseableRefusesBoot` + `TestRecoveryNoLongerScansKVPrefix`（新恢复不扫旧前缀）—— 验证：`go test ./assembly -v -run 'TestKVDeliveryMigratedToFacts|TestKVRecordUnparseableRefusesBoot|TestRecoveryNoLongerScansKVPrefix' -count=1` 用例数=3 PASS。
- [x] 1.6 感知/表达/兜底三层：`TestHeldSettlementReachesModel`（held/retry 结算事件**进入模型请求可见面**——回执同族待遇）+ `TestDeliveryNoiseAbsentFromProjection`（记账噪音不进投影，负锚；**测试须先注入噪音事件再断言缺席**）+ `TestHeldReasonsDistinguishable`（三条 held reason：approval/retrying/retry-exhausted 可分辨）+ `TestNoSilentUndischargedObligation`（兜底两支：①**失败告知**=failed 终态→既有 failure-notice 照发（failed+无正文+有原因，无义务条件）；②**延迟告知**=**held 义务在身 + 驱动轮 completed 无产出**（**驱动轮=当前观察轮，即产生该通知判定的那一轮**）→恰一条最小延迟告知（发送遇阻，稍后必补送），不与模型产出重复、不按失败告知措辞；成功无正文且无未清义务静默；悬空无终态不发通知；failure-notice 的「成功但无正文保持静默」场景以 MODIFIED 豁免句衔接本支）+ `TestApproveRefuseOnFacts` + `TestDelayedNoticeLayeredByReason`（延迟告知按 reason 分层：retrying→稍后必补送；approval/unknown/retry-exhausted→已转人工处置）+ `TestDeliveryConsumerPathEvidence`（Host 公共入口端到端）—— 验证：`go test ./assembly -v -run 'TestHeldSettlementReachesModel|TestDeliveryNoiseAbsentFromProjection|TestHeldReasonsDistinguishable|TestNoSilentUndischargedObligation|TestDelayedNoticeLayeredByReason|TestApproveRefuseOnFacts|TestDeliveryConsumerPathEvidence' -count=1` 用例数=7 PASS。
- [x] 1.6b 类型→角色映射（F13）：`delivery.settlement` 在 request_assembly.go 的 RoleOf 注册（未注册=ErrProjectionTypeUnmapped，感知断言必挂）；**类型字符串常量 `projectionTypeDeliverySettlement` 定义为 runtime/agent 本地常量**（注释指回 assembly/host_delivery.go 的族定义；不 import assembly、api/ 零改动）——`TestSettlementRoleMapped` —— 验证：`go test ./assembly ./runtime/agent -v -run 'TestSettlementRoleMapped' -count=1` PASS。
- [x] 1.6c 纯化（r22）：开关已删，唯一路径负锚替代 —— `TestDeliveryEventificationIsUnconditional`（任何 `TAGENT_EXPERIMENT_*` 值均无效：交付只走结算事实链，旧 KV 写路径不存在） —— 验证：`go test ./assembly -v -run 'TestDeliveryEventificationIsUnconditional' -count=1` PASS。
- [x] 1.7 概念文档同步与文档门：FACT-VOCABULARY 登记行（归属包 assembly；**表尾计数行 9→10 族**；**开头「（九族）」句同步为「（十族）」**）+机制导览/执行核架构（交付 owed 声明+**四结算态+迁移专用 owed 态**→链层；可见性口径=结算事实呈现、owed 随 agent_output）+GLOSSARY（**「已登记九族」两处同步更新为十族**；**GLOSSARY:24「五结算态」改为「四结算态+迁移专用 owed 态」**）+**ADR 0002 注记更新**（兜底两支口径＝失败告知/延迟告知，与 01 spec 对齐）+ **doc-budget 棘轮收口（r9 F10）：本变更新增文档行数以压缩既有文档冗余或具名基线登记补偿压回基线，不得静默上调；当前工作树红=预期（ADR 0002+GLOSSARY 增量未压回），本任务压回；该门纳入 F2 前自检** —— 验证：`python3 scripts/gates/check_doc_links.py` exit 0 + 登记行 grep 命中 + FACT-VOCABULARY 表尾计数行与 GLOSSARY 标语一致 + GLOSSARY:24 括号逐字含「四结算态+迁移专用 owed 态」+ `python3 scripts/gates/check_doc_budget.py` exit 0（压回基线或具名登记后）。
- [x] 1.8 域门禁（lead 核销口径：本域测试全绿 91/0＋跨域锚④（02 域）与⑦（05 域）按变更级验收（F3）计，不阻塞本域）：`go test ./assembly -v -count=1` 全绿；**concept budget 门自清**：`python3 scripts/gates/check_concept_budget.py --json` 的 `errors` 为空（新增字段全部未导出+方法访问：`QueryOptions.keyPrefix`/`Prefix()`/`WithKeyPrefix()`；本域不新增导出类型；`delivery.settlement` 常量未导出）—— 验证：go test exit 0 + concept budget `errors: []`。

## scenario ↔ 测试映射（D5，F32）

| spec scenario | 测试 |
|---|---|
| 提交失败两不立 | 锚① + TestObligationAtomicWithOutput |
| 异常必被模型感知 | TestHeldSettlementReachesModel |
| 记账噪音隔离 | TestDeliveryNoiseAbsentFromProjection（须先注入噪音再断言缺席） |
| 区分等待与放弃 | TestHeldReasonsDistinguishable |
| 瞬时明确失败 | 锚③（拒连非超时形状） |
| 纯超时归未知 | TestUnknownResultNeverAutoRedriven（纯超时形状） |
| 失败终态照发既有通知 | TestNoSilentUndischargedObligation |
| held 义务未清且本轮无面向用户产出（延迟告知） | TestNoSilentUndischargedObligation |
| 成功但无正文静默 | 并入 TestNoSilentUndischargedObligation（failure-notice MODIFIED 豁免句：无未清义务才静默） |
| 迁移幂等 | TestKVDeliveryMigratedToFacts |
| headless 不重发 | TestKVDeliveryMigratedToFacts |
| 旧记录损坏 | TestKVRecordUnparseableRefusesBoot |
| 多会话恢复有界 | TestMultiSessionRecoveryBounded（计数口径=返回的匹配事实条目数） |
| 中断重试恢复后续跑 | TestRecoveryResumesInterruptedRetry |
| 延迟告知按 reason 分层 | TestDelayedNoticeLayeredByReason |
| 消费挂点不自监听 | TestDeliveryConsumerNotSelfTriggered |

> **验证约定（r4 F14）**：本文件所有 `go test -run` 验证命令默认附加 `-v`；“用例数=N”=输出中 `--- PASS:` 行数；零匹配（ok [no tests to run]）视为空转不算通过。
