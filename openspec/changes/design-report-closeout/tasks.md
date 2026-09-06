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
- [x] 2.3 OnSettle 自动来源：TaskManager settle completed/failed→对 spawn turn agent_output 写 task_settle feedback（suspect 不写）；回归 TestOnSettle_WritesDeterministicFeedback — **探明结论(2026-09-06)**:spawn 时 Origin 无法携带 spawn turn 的 agent_output key(turn 尚未结束);候选锚点=feedback.parent=task_settled 事件自身(结算即任务产出,join 用其 bundle_id 章),需维护者确认语义微调后实施(TaskManager FeedbackHook+wire)
- [x] 2.4 HTTP API `POST /feedback`（event_key+verdict+note，不存在显式错）挂 rl/httpapi + handler 测试
- [x] 2.5 MetricGuardrail 补 negative_feedback_rate 判据（feedback→因果边 parent→bundle_id join，阈值独立配置）；回归 TestGuardrail_NegativeFeedbackRollback（fail-before：无此判据）

## 3. D1 approve 通道

- [x] 3.1 ApprovalChannel 接口 + 审批状态机（requested→responded→consumed，重复回应幂等）+ 纯函数单测
- [x] 3.2 CLI 入口 `tagent approve <digest> [--reject]`（写回应文件，零新服务器）+ CLI 测试
- [x] 3.3 微信注入：pending→EventBus external_input(source=approval) 渗透 + `approve/reject <digest>` 解析纯函数（框架侧）+ wechat-bot listener 挂接（examples 侧）+ 双测；通道失败不阻塞门（闸不是墙）回归

## 4. D2 巩固触发

- [x] 4.1 配置结构 `memory.engine.consolidation { capacity_threshold(默认0=关)/min_source_events(3)/snooze }` + Validate
- [x] 4.2 容量路：引擎 Index 后旁路计数→超阈发 consolidation_hint（EventBus）+ SNOOZED（`0:vmeta:consolidation_state` KV）snooze 窗不重复；回归 TestCapacityHint_TriggerAndSnooze
- [x] 4.3 冥想 hint 路：meditation digest 附 TopN 可巩固候选；回归 TestMeditationDigest_IncludesCandidates
- [x] 4.4 min_source_events 硬门控前置 BuildConsolidationEvent；回归 TestConsolidate_MinSourcesReject（fail-before：现宽松放行）

## 5. D3 goal 工具 + 降级行为层

- [x] 5.1 goal 五工具（goal_declare/goal_list/goal_resolve/denial_query/approval_list）PlainTool 注册，entry only + 治理包裹；goal_declare/resolve 写 governance 事件（subtype）；回归 TestGoalTools_EntryOnly + TestGoalDeclare_PersistsEvent
- [x] 5.2 GoalRegistry 从 governance 事件回放重建（构造期单线程，对齐 DenialLedger）；回归 TestGoalRegistry_RebuildFromEvents（重启不丢 goal）
- [x] 5.3 governance EventTypeSpec Skeleton→false；回归 TestGovernanceEvent_NotSkeletonized（压缩定级含 governance 段全文保留）
- [x] 5.4 降级行为层三项（独立配置默认关）：model 退避（runEventLoop turn 间）/ mcp 熔断+半开（mcp_call 入口）/ disk 禁新 spawn（TaskManager.Spawn 前）；各配独立回归（默认关=零行为变化断言必含）
- [x] 5.5 mem_spill 重放补 projection.Append（失败仅记日志）；回归 TestMemSpillReplay_AppendsProjection（fail-before：现只回灌 StoreEvent）

## 6. 收尾

- [x] 6.1 LEDGER 五分歧裁定行核对引用（**已于提案期固化**——用户 2026-09-06 确认全部保持实际实现，联动增强项已登记 roadmap §5A，执行时核对无漂移即可）
- [x] 6.2 文档同步：README（feedback/approve/goal 工具与配置行）、wiki platform 篇（审批通道/降级行为/巩固触发）、wiki memory 篇（巩固触发节）
- [x] 6.3 门禁全绿 + git commit（conventional,引用本变更）

## 7. 路线图转入 backlog（tagent-evolution-roadmap 归档承接，2026-09-07）

> 来源：`tagent-evolution-roadmap` §5A（其标题即「design-report-closeout 后续计划，2026-09-06 用户裁决登记」）
> 随该路线图「部分完成」归档转入本变更承接——**逐项内容原文保留，不因归档而丢失**；五分歧裁定表见 LEDGER 2026-09-06 行。
> 推进时机：本变更 §2-§5 主体落地后（7.3/7.6 显式依赖 §1/§2）。
> 复活前须按 LEDGER 重编原则 #3 做前提核验（挂载点是否已被退役/重构）。

- [ ] 7.1 （原 5A.1）D1 cassette 录制/回放 + replay 门接线（分歧②汇合点：快慢道之外补中档风险分级；shadow 维持不做）
- [ ] 7.2 （原 5A.2）D4 三支柱：G1 obs 过程指标（FanoutSink 在线聚合+JSONL 文件即后端+TrajectoryAggregator 对账）→ G2 evals（suites yaml+ProgramScorer/LLMScorer+held-out，含 D2 欠的 recall@10 基线）→ G3 行为回归（cassette 语料+事件级归一化 diff）；含 TraceExporter/tagtrace（分歧⑤余项）；release 工程（nightly/失败分类）
      - 并入 roadmap 转出项：**2.2**（CONFIRM C4 评估任务集来源与首批规模；素材已备=tests/ 8 个 `TestContract_*` 真实 ZAI_API_KEY 全 PASS，可直接提炼 10-20 组件级 case）、**2.3**（离线 `evals/` 目录基座 + trpc evaluation 桥接 + real-LLM flaky 治理；过程指标埋点已由 T-EVO 以运行时闭环形态达成）、**3.4**（TrajectoryRecorder 反馈关联率指标；数据源已随本变更 §2 交付而具备）
- [ ] 7.3 （原 5A.3）D5 M1-M4（报告锚点已核实零漂移）：handoff 契约 ratchet→ReviewGate critic→方案契约三件套→RL 反馈通道 long-poll+TurnTracker；**前置**：恢复 http_api_test 覆盖（新建 mockAgentLoop）+ jsonschema 依赖决策 + 报告 2 处 mock 表述勘误附录（mock_agent_loop_test.go 已删）
      - 并入 roadmap 转出项：**5.5**（critic/verifier 协作模式最小可用：plan 产出经 critic 对抗评审后放行，与 AgentToolWrapper/prefix-cache 兼容）+ **C10 裁决**（trpc team vs 自研 critic，倾向自研）；开发期等价实践 = gate-3 CodeReview 子 agent 多轮 fresh-eyes 对抗评审（累计揪出六 Major + N1/N2 等真缺陷），可作 ReviewGate 设计输入
- [ ] 7.4 （原 5A.4）D3 M6 故障注入矩阵（1/10→10 场景：SIGKILL/时钟回拨/磁盘满/WAL 损坏/网络分区等；依赖 F2 先修——已在本变更 §1.2 完成）
- [ ] 7.5 （原 5A.5）D2 诊断补全：悬挂率统计+召回质量/收据完整率维度（分歧③联动；SuggestedActions/vector_admin rebuild）
      - 并入 hybrid-semantic-recall 转出项：**3.4**（历史事件一次性向量回填命令/工具：KVRange 全量 → 批量 embed；可选增强，新事件已自动索引，非主链路验收必需）
- [ ] 7.6 （原 5A.6）canary 时间窗 ∨ 样本数下限双条件（分歧④联动；依赖本变更 §2 feedback 落地后评估 judge min_samples）
- [ ] 7.7 roadmap §3 P2 余项收口：**3.5** 的「反馈→事件→召回」闭环集成测试（依赖本变更 §2.3 OnSettle 自动来源）；本变更 §6.3 门禁+commit+archive 即等价于 roadmap **3.6**（其转出标注已指向本变更收尾）
- [ ] 7.8 工程收尾候选（roadmap **1.5** / CONFIRM **C11**）：首个 version tag（默认 v0.1.0）打出并 push——前置已就绪（全程 conventional commit + push、CI 门禁 build/vet/short/race 实装绿），仅剩打 tag 动作与时点裁决
      - **用户裁决（2026-09-07）：暂不打 tag**——保持 pre-release 状态，不对外暗示版本稳定性；前置继续维持就绪，待后续显式发起时再打（届时 CHANGELOG 的 `[Unreleased]` 一次收口为正式版本段）
- [ ] 7.9 后续候选（非任何阶段准出条件，登记以防丢失）：
      - roadmap **2.4**：nanobot `skills/` 兼容技能并入 examples skills（SKILL.md 格式核对，注明来源）
      - observability-tracing **1.1/1.2**（环境实装项，非代码缺口）：起 Jaeger all-in-one 或 OTLP 调试后端，设 `OTEL_EXPORTER_OTLP_ENDPOINT` 跑冒烟 + 集成测试，实录框架层 span 形态（名称/属性/父子/时延）与自动 metrics、评估单 turn span 数量级、顺带评估 langfuse exporter 适配性（代码侧导出链路已就绪：noop 默认零开销，设端点即导出）
        - **已具备执行路径（用户裁决 2026-09-07：远端部署时顺带起 Jaeger）**：`docker-compose.yml` 已内置 `jaeger` 服务（归入 `observability` profile，默认不启动故不影响既有部署）；`deploy/README.md` §九 含启动命令、端点取值（裸机 `127.0.0.1:4317` / 容器 `jaeger:4317`）、**五项 span 形态实录清单**与排障表——部署时照单执行即可闭合本条与 4.2 之外的两项
      - observability-tracing **4.2**（跨仓协调项）：AReaL reward 侧消费格式核对（tagent 侧已尽向后兼容义务——`LLMCallRecord.trace_id/span_id` 均 omitempty，不破坏既有 JSONL 解析）
- [ ] 7.10 文档全量同步（**用户裁决 2026-09-07：本轮不改机制、不随组同步，待本变更 §2-§5 落地后统一做一次全量代码交叉印证修订**）
      - 已知滞后面（本次归档整合时量化）：`consolidation_hint` / `capacity_threshold` / `min_source_events` / `ApprovalChannel` / `negative_feedback_rate` 五项能力在 README、README_EN 与 docs/wiki 全 9 篇中**0 篇命中**；`goal_declare` 五工具与 `feedback` 事件类型亦仅零星提及
      - 待同步文档面：README 配置表（`memory.engine.consolidation` 段 + goal/feedback 相关）、wiki platform 篇（审批通道 / 降级行为 / 巩固触发）、wiki memory 篇（巩固触发节）、wiki event 篇（若新增事件类型）、tests/README（若新增契约测试）
      - 本变更 §6.2 已登记同一意图；本条为其**范围与方法的细化**（按 2026-09-07 全量审计流程：机械化核查标识符/文件引用/清单完整性 + 逐段语义核对 + 门禁复验）

## 8. 第六轮 review 遗留(2026-09-07,双 CodeReview sub-agent + 主线程亲验;archive 前须关闭 Blocker+Major)

- [x] 8.1 **Blocker**:disk spawn gate 时序错位——ActionTool `startSession` 已真起 tmux、子 agent `NewFuncSettleDetector` 构造即 `go fn()` 之后才进 Spawn,gate 拒绝时工作**已在执行**,却返回「命令未执行/子任务被暂停」(对模型谎报)且 detector 未 Cancel(tmux 孤儿会话/失控后台子 agent)。修:①Blocked 分支必须 `detector.Cancel()`;②文案改「已执行但未纳管(结果不经任务层跟踪)」;③更优:gate 前置为起会话前的预检(TaskSpawner 暴露 WouldBlock() 或工具侧先查)
- [x] 8.2 **Major(安全)**:微信审批回复无身份校验——任意可达 bot 的用户发 "approve <digest>"(digest 随 approval_request 明文送达)即可批准 critical。修:审批人白名单配置(app.wechat 或 governance 段),非白名单回复拒绝并留痕
- [x] 8.3 **Major**:POST /feedback 生产链路未接线——SetFeedbackStore 全仓仅测试调用,example 装配缺失→端点恒 503,2.4 闭环未通。修:examples/wechat-bot main.go RL 模式装配处注入 entry memStore
- [x] 8.4 **Major**:feedback 事件不继承 parent 的 bundle_id 章→guardrail join 退化时间窗,跨 bundle 误归因可致误回滚。修:BindFeedback 从 parent.Metadata 复制 bundle_id
- [x] 8.5 **Major**:http /feedback 把 BindFeedback 全部错误映射 404——SetParent 失败时事件已落库却报 404,客户端重试→重复 feedback→NegFeedback 虚高。修:区分 sentinel 错误(parent-miss=404;已落库+边失败=201+warning)
- [x] 8.6 **Major**:spawn gate 前置于 dedup——同 Key 在飞任务被误报 Blocked,违背「进行中任务不受影响」。修:gate 移到 dedup 短路之后
- [x] 8.7 **Major**:writeGoalEvent 在锁外生成 Timestamp/EventKey——Declare/Resolve 并发时 declared 可后落库,rebuild 后已关闭 goal 复活为 active(治理门重开)。修:g.mu 内分配 Timestamp+Key 再锁外写
- [x] 8.8 **Major**:goal rebuild 用 sort.Slice(不稳定)且两 store 已按 (Timestamp,EventKey) 全序返回——同毫秒抹掉 EventKey 兜底序。修:改 SliceStable+EventKey 次序键(或直接信任 store 序)
- [x] 8.9 **Major**:consolidationMinSources 用整体 Validate 一票否决——snooze 拼错即静默关闭 min_source 硬门控(安全闸被无关字段拖垮)。修:仅校验 MinSourceEvents,其余字段独立降级
- [x] 8.10 **Major**:wireMemoryEngine 的 embedding 构建失败降级路径 `return store` 丢掉 capacityHook——与「巩固触发不依赖 embedding」相悖。修:降级路径也走 capacity-only bridge
- [ ] 8.11 Minor 批量(13 项):①verdict 子串判定脆弱(eval.go,建议 Metadata 冗余 verdict)②deliverEvent send/timer 同时就绪的双投递窗口③RespondFile 同前缀多文件字母序遮蔽 pending+L105 死分支④digest 未验 hex 字符集⑤WalQuarantined 无消费方(接 diagnostics)⑥MaxNegFbRate<=0 即启用默认值,禁用须 >1 反直觉⑦goal rebuild Limit:10000 在 subtype 过滤前,治理事件多时截掉 goal~~⑧tracker 触发清 counts 不清 recent~~(已修:同置 nil)⑨SetOnHint 晚于 NewTagentAgent,窗口内触发丢提示(清零+snooze 已记)⑩gate.go 拒绝文案仍写「goal_declare 尚未交付」(已交付)+五工具落 default medium 非 low(占预算,strict+预算耗尽时 goal_declare 自我拒绝)⑪engineBridge 不透传 KVProvider(capacity-only 包裹后 ETS.KVBackend 由可用变 nil,当前无消费方)~~⑫model backoff 在 retry 循环内每次重试叠加~~(已修:attempt==0 条件)⑬mcp 熔断 probeCount 恢复后残留(相位偏移,无正确性问题)
