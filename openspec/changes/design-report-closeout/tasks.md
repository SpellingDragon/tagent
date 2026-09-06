# design-report-closeout 任务清单

> 约束：全部为设计内项（docs/.dev 报告 D1-D5 对应章节）；每项须 fail-before/pass-after 回归；门禁 = gofmt/build/vet + 全量 `-short` + 新增面 `-race`。P0 与 P1 各组内部可并行，组间 P0 先行（feedback 判据依赖 bundle_id）。

## 1. P0 先行修复

- [x] 1.1 **F3 WAL 容错**：memory/kv/local_file_kv.go replayWAL 中间坏行跳过+quarantine 计数（log+Stats 暴露）；回归 TestLocalFileKV_MidWALCorruption_SkipsAndQuarantines（fail-before：现版本启动失败）
- [x] 1.2 **F2 outputCh 宽限落盘**：agent/context_manager.go RunFlow 发送 select 加 2s 宽限→OutputOverflow 落盘（tool-output/output-overflow/）+摘要票据；SessionHook default 分支计数；回归 TestRunFlow_SlowConsumer_OverflowsToDisk（fail-before：现版本阻塞）
- [x] 1.3 **bundle_id 归因盖章**：plugin/attribution.go 补 BundleID + RunFlow 读 evoStore active（nil 安全）+ 双路径盖章；evolution Evidence 改 bundle_id 精确 join（时间窗回退）；回归 TestAttribution_BundleIDStamped 双路径 + TestEvidence_BundleJoin
- [x] 1.4 **F5 env 分层标注**：README 环境变量表分组（GLM Coding Plan=ZAI_API_KEY / 混元=TENCENT_HY_API_KEY）+ ci.yml 注释声明双 secret 可选；无代码变更

## 2. D1 feedback 闭环（依赖 1.3）

- [x] 2.1 feedback EventTypeSpec 注册（TTL 默认 30 天可配置、正 key、非 LowValue、Recallable）+ 注册表契约测试
- [x] 2.2 FeedbackBinder（SetParent 因果边 + 结构化 JSON Content）纯函数 + 单测
- [ ] 2.3 OnSettle 自动来源：TaskManager settle completed/failed→对 spawn turn agent_output 写 task_settle feedback（suspect 不写）；回归 TestOnSettle_WritesDeterministicFeedback
- [ ] 2.4 HTTP API `POST /feedback`（event_key+verdict+note，不存在显式错）挂 rl/httpapi + handler 测试
- [ ] 2.5 MetricGuardrail 补 negative_feedback_rate 判据（feedback→因果边 parent→bundle_id join，阈值独立配置）；回归 TestGuardrail_NegativeFeedbackRollback（fail-before：无此判据）

## 3. D1 approve 通道

- [ ] 3.1 ApprovalChannel 接口 + 审批状态机（requested→responded→consumed，重复回应幂等）+ 纯函数单测
- [ ] 3.2 CLI 入口 `tagent approve <digest> [--reject]`（写回应文件，零新服务器）+ CLI 测试
- [ ] 3.3 微信注入：pending→EventBus external_input(source=approval) 渗透 + `approve/reject <digest>` 解析纯函数（框架侧）+ wechat-bot listener 挂接（examples 侧）+ 双测；通道失败不阻塞门（闸不是墙）回归

## 4. D2 巩固触发

- [ ] 4.1 配置结构 `memory.engine.consolidation { capacity_threshold(默认0=关)/min_source_events(3)/snooze }` + Validate
- [ ] 4.2 容量路：引擎 Index 后旁路计数→超阈发 consolidation_hint（EventBus）+ SNOOZED（`0:vmeta:consolidation_state` KV）snooze 窗不重复；回归 TestCapacityHint_TriggerAndSnooze
- [ ] 4.3 冥想 hint 路：meditation digest 附 TopN 可巩固候选；回归 TestMeditationDigest_IncludesCandidates
- [ ] 4.4 min_source_events 硬门控前置 BuildConsolidationEvent；回归 TestConsolidate_MinSourcesReject（fail-before：现宽松放行）

## 5. D3 goal 工具 + 降级行为层

- [ ] 5.1 goal 五工具（goal_declare/goal_list/goal_resolve/denial_query/approval_list）PlainTool 注册，entry only + 治理包裹；goal_declare/resolve 写 governance 事件（subtype）；回归 TestGoalTools_EntryOnly + TestGoalDeclare_PersistsEvent
- [ ] 5.2 GoalRegistry 从 governance 事件回放重建（构造期单线程，对齐 DenialLedger）；回归 TestGoalRegistry_RebuildFromEvents（重启不丢 goal）
- [x] 5.3 governance EventTypeSpec Skeleton→false；回归 TestGovernanceEvent_NotSkeletonized（压缩定级含 governance 段全文保留）
- [ ] 5.4 降级行为层三项（独立配置默认关）：model 退避（runEventLoop turn 间）/ mcp 熔断+半开（mcp_call 入口）/ disk 禁新 spawn（TaskManager.Spawn 前）；各配独立回归（默认关=零行为变化断言必含）
- [ ] 5.5 mem_spill 重放补 projection.Append（失败仅记日志）；回归 TestMemSpillReplay_AppendsProjection（fail-before：现只回灌 StoreEvent）

## 6. 收尾

- [ ] 6.1 LEDGER 五分歧裁定行核对引用（**已于提案期固化**——用户 2026-09-06 确认全部保持实际实现，联动增强项已登记 roadmap §5A，执行时核对无漂移即可）
- [ ] 6.2 文档同步：README（feedback/approve/goal 工具与配置行）、wiki platform 篇（审批通道/降级行为/巩固触发）、wiki memory 篇（巩固触发节）
- [ ] 6.3 门禁全绿 + git commit（conventional,引用本变更）
