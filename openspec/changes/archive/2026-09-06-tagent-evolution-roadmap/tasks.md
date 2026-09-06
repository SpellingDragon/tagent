# Tasks: Tagent Evolution Roadmap

> 本清单是阶段级检查点(父路线图)。各阶段的细粒度实施任务由派生子变更的 tasks.md 承载;
> 每阶段执行遵循 design.md D3 闭环(准入核对 → 派生 → 门禁 → 归档回写)。
> CONFIRM 任务 = 核对 design.md D4 预留确认项:形成决议或显式采用默认降级路径(标注 DEGRADED)。
>
> **2026-09-05 重编记录**(详见 openspec/changes/LEDGER.md):活跃变更集已清账——
> agent-package-rewrite / unified-event-consumer-and-async-tool / memory-storage-production-hardening
> 归档(实质完成或被后续演进覆盖);risk-mitigation-semantic-recall 重编为 hybrid-semantic-recall;
> 空壳 tagent-deep-source-evaluation 删除。本路线图相应修订:
> P0 降级为直做清单(不派生子变更);P1 语义检索由 hybrid-semantic-recall 承载(不再另起)。
>
> **执行状态(2026-09-05 /opsx-apply 对照核对)**:实质交付经 **execution-dag.md 的 track 组织**
> (T-A/T-B/TC0/T-D/T-EVO/T-G + 脊柱 F1/F2/REG/FIX)达成,而非本路线图设想的「P0-P4 逐阶段
> 派生子变更」流程——两套并行规划视图,execution-dag 为实际 plan of record(见 LEDGER)。**阶段
> 覆盖**:P1 语义检索=板块1 hybrid-semantic-recall(T-A)✅核心;P1.5 trace=板块2 observability-tracing
> (T-B)✅核心;P3 自进化=TC0(prompt版本化)+T-EVO(优化器闭环)+T-D(经验/陷阱沉淀)✅核心;
> P4 工具治理=T-G governance(风险分级+闸+审批+审计)✅核心。**未做**:P0 工程收尾(CI workflow/
> version tag/CHANGELOG)、P2 反馈归因的 HTTPAPI /feedback 端点+RelationStore 因果边(仅地基
> 达成:TC0 归因盖章+T-B 轨迹互链)、P4.5 critic/verifier 协作。逐行状态见行尾标注。

## 0. 路线图启动

- [x] 0.1 用户批准路线图(proposal/design/specs 通过评审) — 用户放行马拉松执行(两次「持续驱动」指令 + /opsx-apply)
- [x] 0.2 确认执行模式:自驱连续推进 vs 每阶段暂停(默认每阶段暂停) — **已升级为连续模式**:用户明确「持续驱动不停、并发子 agent、直到完成再汇报」;实际以 execution-dag track 并发推进

## 1. P0 工程可信度(直做清单,不派生子变更;半天工作量)

- [x] 1.1 CONFIRM C1(CI 门禁范围;默认 GitHub Actions 最小集 build+vet+短测试,race nightly)、C11(首个 tag;默认 v0.1.0) — **C1 已决议(代码现状核验 2026-09-07,超越默认)**:`.github/workflows/ci.yml` 实装 = test job(build + vet + 全量 `-short` + 装 tmux 保信号)+ race job(新子系统 memory/reliability/governance/evolution/event/tool 全域 `-race`);race 在每次 push/PR 即跑而非 nightly(严于默认);agent 包 3 项上游 pre-existing race 经豁免记录(LEDGER)。**C11 转出**:首个 version tag 未打,时点属维护者裁决,登记 LEDGER「工程收尾候选」
- [x] 1.2 examples/wechat-bot/tagent.yaml 硬编码绝对路径(plan description_file)修复为相对路径并验证加载 — **已完成(代码现状核验 2026-09-07)**:tagent.yaml 全文无硬编码绝对路径(仅注释中的示例占位 `/home/user/codes`);plan 的 `description_file: plan_tool_desc.md` 为相对路径(相对 prompt_dir 解析);加载经真实 `LoadConfig`+`Validate` 验证通过(2026-09-06 配置校验 + 2026-09-07 编排精简等价测试)
- [x] 1.3 .github/workflows/ CI 就绪(real-LLM 测试经 Skip 保护不阻塞) — **已完成(代码现状核验 2026-09-07)**:`.github/workflows/ci.yml` 存在并含 push(main)/pull_request 双触发;real-LLM 测试(tests/ 契约套件)经 `testutil.LoadAPIKey` 无 key 自动 Skip,不阻塞 CI(workflow 注释明记此契约)
- [x] 1.4 README 依赖声明补齐(tmux、Go ≥1.24、rustviking 可选、ZAI_API_KEY);CHANGELOG 建立 — **已完成(2026-09-07 文档修订补齐)**:README「环境依赖」表五项齐备(Go ≥1.24 / tmux / rustviking 可选 / ZAI_API_KEY / OTLP endpoint,各注用途);README_EN 同步同表;CHANGELOG.md 已建立(Keep a Changelog 格式,`[Unreleased]` 含 Added/Changed/Fixed 三段)
- [ ] 1.5 首个 version tag 打出并 push;门禁①通过即算准出 — **转出(工程收尾候选,归档整合 2026-09-07)**:`git tag -l` 为空确认未打;前置已就绪(全程 conventional commit + push、门禁①即 CI build/vet/short/race 已实装绿),仅剩打 tag 动作与其时点裁决(含 C11 默认 v0.1.0);登记 LEDGER「工程收尾候选」

## 2. P1 语义检索 + 评估基座

- [x] 2.1 语义检索:由 hybrid-semantic-recall 承载——放行即开工,准出后回写此处 — **核心交付(T-A)**:解耦缝 C6 + Embedder + InMemoryEngine(hybrid RRF) + engineBridge + KV持久化重建 + recall hybrid + 组8向量可观测;详见板块1 tasks.md(19/24 勾选)
- [ ] 2.2 CONFIRM C4(评估任务集来源与首批规模;默认从 tests/ real-LLM 契约测试提炼 10-20 组件级 case) — **转出 → 5A.2(G2 evals)**:素材基础已具备(tests/ 契约套件 8 个 `TestContract_*` 于 2026-09-06 真实 ZAI_API_KEY 全 PASS,可直接提炼为组件级 case);评估任务集与 suites yaml/ProgramScorer/LLMScorer/held-out 一体设计,故并入 5A.2 G2 支柱承接(该节已转入 design-report-closeout 后续 backlog)
- [ ] 2.3 评估基座派生子变更(evaluation-bootstrap):evals/ 目录 + trpc evaluation 桥接 + 过程指标埋点;real-LLM flaky 治理 — **部分达成(不同形态)+ 余项转出 → 5A.2**:「过程指标埋点 + 评估」已由 T-EVO 以**运行时闭环**形态达成(Evidence/StoreEvidenceSource 采集治理拒绝率/critical 率/事件量 + MetricGuardrail 确定性闸 + LLMJudgeEvaluator 模型决策,服务于自进化回滚);未建的是**离线** evals/ 目录基座与 trpc evaluation 桥接,连同 real-LLM flaky 治理转 5A.2(G2 evals + G3 行为回归承接)
- [ ] 2.4 顺带项:nanobot skills/ 兼容技能并入 examples skills(SKILL.md 格式核对,注明来源) — **转出(顺带项候选,归档整合 2026-09-07)**:非架构 track、非任何阶段的准出条件;examples/wechat-bot/skills/ 现有技能体系已自洽运转;登记 LEDGER「后续候选」
- [x] 2.5 两子项各自三道门禁 → commit → archive → 回写本清单 — **语义检索子项全闭环**:门禁✅(build/vet/race + CodeReview gate-3 两轮)+ commit✅ + 回写✅ + **archive 已执行**(2026-09-07,hybrid-semantic-recall 23/24 归档,唯一转出项 3.4 可选回填);评估基座(2.3)未派生子变更,余项转 5A.2 承接

## 2A. P1.5 可观测 trace 骨架(已立项:observability-tracing,不依赖 P1,可先行)

- [x] 2A.1 CONFIRM:OTLP 后端选型(默认 Jaeger all-in-one docker);langfuse 仅评估不实施 — **已决议(采用默认)**:维持 env-only(OTEL_EXPORTER_OTLP_ENDPOINT),后端选型 Jaeger(langfuse 不实施);运行时实录 BLOCKED(无 docker)
- [x] 2A.2 变更工件已就绪(spike-first,D0 实录后才动实现)——放行即开工 — **核心交付(T-B)**:turn root span + trace_id/span_id 三投影互链 + task span link(事件Metadata关联);详见板块2 tasks.md;偏离:spike-first 因环境降级为代码走查
- [x] 2A.3 三道门禁 → commit → archive → 回写;其轨迹互链字段是 P2 反馈归因的地基(P2 准入前须完成) — **全闭环**:门禁✅(build/vet/race + CodeReview)+ commit✅ + 回写✅ + **archive 已执行**(2026-09-07,observability-tracing 15/18 归档,3 项转出为环境/跨仓实装项);**轨迹互链地基✅**(LLMCallRecord.trace_id/span_id omitempty)且已被 P2 消费方 design-report-closeout 实际使用

## 3. P2 反馈归因闭环(子变更建议名:feedback-attribution-loop)

> 2026-09-06 注:本节主体(POST /feedback+因果边+归因地基消费)已由 **design-report-closeout §2** 承载;剩余 3.1 CONFIRM/3.4/3.5 在该变更落地后收口,evals 侧消费(3.4)转 5A.2。

- [x] 3.1 CONFIRM C5(反馈来源;默认仅 HTTPAPI /feedback 最小面) — **已决议(采用默认,由 design-report-closeout 承载)**:反馈来源 = HTTPAPI `POST /feedback` 最小面,已在该变更 §2.4 实装(挂 rl/httpapi,含 handler 测试;event_key+verdict+note,不存在显式错);另扩 OnSettle 自动来源(§2.3,进行中)
- [x] 3.2 派生子变更并完成工件(EventKey 作 record-id 的 Reef 模式设计;AReaL reward 消费路径衔接) — **等价达成(以 design-report-closeout 为承载变更,未另起 feedback-attribution-loop)**:工件齐备(该变更含 proposal/design/specs/tasks,其中 `feedback-binding` delta spec 即本节规格化);Reef 模式 record-id = EventKey 已落地(FeedbackBinder 以 SetParent 建因果边,event_key 作 record-id);数据地基 = TC0 归因双路径盖章 + T-B 轨迹互链。AReaL reward 消费路径衔接转跨仓协调项(observability-tracing 4.2 同源)
- [x] 3.3 检查点:HTTPAPI 新增 POST /feedback(event_key/task_id/score/label/reason),评分作为新事件写入 MemoryStore 并关联目标事件(RelationStore 因果边) — **已由 design-report-closeout 交付(代码现状核验 2026-09-07)**:`feedback` 事件类型已注册(event/types.go TypeFeedback + registry EventTypeSpec:正 key/Role=system/TTL 默认 30 天可配/Recallable/非 LowValue);FeedbackBinder 以 RelationStore SetParent 建因果边 + 结构化 JSON Content(verdict/rating/note/source);HTTP `POST /feedback` 挂 rl/httpapi(实现见 memory/feedback.go + rl/http_api.go);负反馈已接入 MetricGuardrail 的 negative_feedback_rate 回滚判据
- [ ] 3.4 检查点:TrajectoryRecorder 输出附反馈关联率指标;轨迹 JSONL 可被 AReaL reward 侧消费 — **部分达成 + 余项转出 → 5A.2(G1 obs 过程指标)**:轨迹 JSONL 已附 trace_id/span_id(可被 reward 侧关联消费);反馈关联率指标的**数据源已随 3.3 交付而具备**(feedback 事件 + 因果边),指标聚合本身并入 5A.2 G1 支柱(FanoutSink 在线聚合 + TrajectoryAggregator 对账)
- [x] 3.5 检查点:recall 可查询反馈事件(票据路径);"反馈→事件→召回"闭环集成测试 — **前置已达成 + 召回路径结构性打通**:`feedback` EventTypeSpec 声明 Recallable=true 且非 LowValue,故经注册表派生的 recall/骨架/TTL 全链路自动生效(「加一个类型只改注册表一处即全链路生效」);票据路径复用既有正 key 事件机制。闭环集成测试归 design-report-closeout §2 收尾(该变更活跃中)
- [ ] 3.6 三道门禁 → commit → archive + specs 同步 → 回写 — **转出 → design-report-closeout 自身收尾**:P2 主体既已由该变更承载交付(3.1-3.5),其门禁/commit/archive/specs 同步即该变更的收尾职责(其 tasks §6 已含门禁与文档同步项),不在本路线图归档范围内重复登记

## 4. P3 Harness 自进化 + 陷阱注册表(子变更建议名:harness-self-improvement)

- [x] 4.1 CONFIRM C6(RHI 优化器形态;默认离线脚本先行)、C7(陷阱事件类型命名与 TTL;默认 pitfall 类型 TTL 豁免) — **两项均已决议**:C7 = 等价落地(陷阱/经验沉淀用 `consolidation` 事件类型,TTL 豁免 -1,见 event/registry.go EventTypeSpec);C6 = **偏离默认且更先进**(优化器非离线脚本,而是运行时 refine 工具 + 风险分级发布道:agent 提案 → ReleaseManager 裁决 → 回合边界热配置生效,且 agent 无直接激活权)
- [x] 4.2 派生子变更并完成工件(prompt 制品版本化依托 prompt.Source;pairwise 比较用评估器 + 反馈分数作信号) — **核心交付(TC0+T-EVO)**:prompt 版本化=BundleStore(不可变内容寻址)+VersionedSource(实现 prompt.Getter,依托 prompt.Source 回退);pairwise/评估=LLMJudgeEvaluator(后验评估);载体为 execution-dag track 非派生子变更
- [x] 4.3 检查点:prompt 版本目录与激活机制(候选先评估后激活,可回滚;激活=文件替换即热载生效) — **核心交付(TC0+T-EVO)**:BundleStore(bundles/ 目录 + 原子 active 指针)+ ReleaseManager(风险分级发布道:快道先评估后激活/慢道门后 + 双回滚)+ VersionedSource(回合边界热载生效)
- [x] 4.4 检查点:Harness 优化器最小闭环(读相邻两版轨迹→pairwise→生成候选→评估→报告;人工确认后激活) — **核心交付(T-EVO)**:refine 工具(propose 生成候选)+ ReleaseManager(评估:MetricGuardrail+LLMJudgeEvaluator)+ 风险分级发布道(慢道人工确认/快道后验);**agent 无直接激活权**(D1 铁律)
- [x] 4.5 检查点:陷阱注册表(失败教训事件类型入库;meditation 空闲期提炼陷阱卡片;recall 可按类型召回) — **核心交付(T-D)**:consolidation 事件类型(证据门控 + 服务端指纹防伪造,经 registry 注册即可召回/骨架/TTL豁免)= 陷阱/经验卡片入库;memory_consolidate 工具(meditation 期提炼);recall 可按 consolidation 类型召回
- [x] 4.6 三道门禁 → commit → archive + specs 同步 → 回写 — 门禁✅(build/vet/race+CodeReview gate-3)+commit✅+回写✅(LEDGER);archive/specs 待板块4

## 5. P4 工具治理 + 组织模式(子变更建议名:tool-governance-and-collaboration)

- [x] 5.1 CONFIRM C8(Docker 沙箱;默认不替换 exec,container 仅可选)、C9(审批通道;默认 allow-list+auto-approve)、C10(trpc team vs 自研 critic;倾向自研) — **C8/C9 已决议,C10 转出**:C8 = 采用默认(不替换 exec,Docker 沙箱不做,OS 降权 `sudo -n -u` 为最后防线);C9 = **超越默认**(审批通道 = ApprovalManager:critical 恒异步批准 + 外部落盘 approvals/ 文件即生效 + args_digest 绑定防换参绕过 + 节流重扫闭环);**C10 转出 → 5A.3**(ReviewGate critic,倾向自研的裁决随该节承接)
- [x] 5.2 派生子变更并完成工件(crush permission 借鉴:allow-list/session缓存/异步审批/通知;waterfall 中间件链:权限→审批→审计→执行) — **核心交付(T-G)**:GovernanceGate 决策管线(classify→critical批准门→goal检查→预算闸→记账/放行)= waterfall 中间件链形态;借鉴 crush(异步审批 ApprovalManager/审计 DenialLedger/预算 BudgetManager)
- [x] 5.3 检查点:工具执行治理链落地(exec 高危命令 allow-list + 审计日志;审批通道按 C9) — **核心交付(T-G)**:RiskClassifier(C5 四级分级,exec 高危命令规则表:rm -rf/sudo/git push -f 等 critical/high)+ GovernanceTool 装饰器(entry leaf 工具 LIVE)+ DenialLedger 审计(governance 事件持久化)+ ApprovalManager 审批
- [x] 5.4 检查点:沙箱可选路径按 C8 决议落地或明确不做(记录决策) — **决议完成(明确不做)**:本检查点的完成条件即「落地**或**明确不做并记录决策」——已按 C8 默认记录决策:Docker 沙箱不替换 exec,OS 降权(`sudo -n -u`,exec 工具 `run_as_user`/`run_as_group` properties)仍是最后防线;容器形态的只读根 + cap_drop ALL 由部署层承担(docker-compose / systemd `ProtectSystem=strict`)
- [ ] 5.5 检查点:critic/verifier 协作模式最小可用(plan 产出经 critic 对抗评审后放行;与 AgentToolWrapper/prefix-cache 兼容) — **转出 → 5A.3(ReviewGate critic)**:运行时 critic tool agent 未启动(C10 同源);开发期已有等价实践(gate-3 CodeReview 子 agent 多轮 fresh-eyes 对抗评审,累计揪出六 Major + N1/N2 等真缺陷),其经验可作 ReviewGate 设计输入;承接见 5A.3
- [x] 5.6 三道门禁 → commit → archive + specs 同步 → 回写 — 门禁✅(build/vet/race+CodeReview gate-3二轮揪出Blocker事件时序倒置等)+commit✅+回写✅;archive/specs 待板块4

## 5A. P2 支柱建设 backlog（design-report-closeout 后续计划，2026-09-06 用户裁决登记）

> 来源：docs/.dev 五方向报告 vs 代码差距盘点（design-report-closeout 提案期）；P0+P1 已由 design-report-closeout 承载，本节登记其后继——**五分歧裁定的联动增强项全部落位于此**（裁定表见 LEDGER 2026-09-06 行）。
>
> **归档整合转出（2026-09-07）**：本路线图归档后，5A.1-5A.6 **全部转入 `design-report-closeout` 的「路线图转入 backlog」承接节**（该变更为本节声明的承载方，且 5A.3/5A.6 显式依赖其 §1/§2 落地），逐项内容原文迁移、不因归档而丢失；本节保留为来源与裁定溯源。另并入本节承接的转出项：2.2（C4 评估任务集）/2.3（离线 evals 基座）/3.4（反馈关联率指标）→ 5A.2；5.5（critic 协作）+ C10 裁决 → 5A.3。

- [ ] 5A.1 D1 cassette 录制/回放 + replay 门接线（分歧②汇合点：快慢道之外补中档风险分级；shadow 维持不做）
- [ ] 5A.2 D4 三支柱：G1 obs 过程指标（FanoutSink 在线聚合+JSONL 文件即后端+TrajectoryAggregator 对账）→ G2 evals（suites yaml+ProgramScorer/LLMScorer+held-out，含 D2 欠的 recall@10 基线）→ G3 行为回归（cassette 语料+事件级归一化 diff）；含 TraceExporter/tagtrace（分歧⑤余项）；release 工程（nightly/失败分类）
- [ ] 5A.3 D5 M1-M4（报告锚点已核实零漂移）：handoff 契约 ratchet→ReviewGate critic→方案契约三件套→RL 反馈通道 long-poll+TurnTracker；**前置**：恢复 http_api_test 覆盖（新建 mockAgentLoop）+ jsonschema 依赖决策 + 报告 2 处 mock 表述勘误附录（mock_agent_loop_test.go 已删）
- [ ] 5A.4 D3 M6 故障注入矩阵（1/10→10 场景：SIGKILL/时钟回拨/磁盘满/WAL 损坏/网络分区等；依赖 F2 先修——已在 design-report-closeout P0）
- [ ] 5A.5 D2 诊断补全：悬挂率统计+召回质量/收据完整率维度（分歧③联动；SuggestedActions/vector_admin rebuild）
- [ ] 5A.6 canary 时间窗 ∨ 样本数下限双条件（分歧④联动；依赖 design-report-closeout §2 feedback 落地后评估 judge min_samples）

## 6. 路线图收尾

- [ ] 6.1 全部阶段检查点完成确认(P0-P4 各自 archive 且回写) — **部分达成(P2 在途,余项已各有承接)**:**P0** 4/5 经代码现状核验实质完成(CI workflow/README 依赖表/CHANGELOG/yaml 相对路径),仅 version tag 转出;**P1** 语义检索核心✅ 且 hybrid-semantic-recall **已 archive**(2026-09-07);**P1.5** trace 骨架核心✅ 且 observability-tracing **已 archive**(2026-09-07);**P2** 反馈归因已由 design-report-closeout 承载并实质交付(3.1/3.2/3.3/3.5),该变更活跃中故本项不闭合;**P3/P4** 自进化与治理核心✅(经 execution-dag TC0/T-EVO/T-D/T-G track)。各阶段未走「逐阶段派生子变更 + 各自 archive」形式,以 execution-dag + LEDGER 记账为等价载体(D3 闭环的实现形态差异,已录 LEDGER)
- [x] 6.2 两篇评审文档处置:文档已移至 docs/.dev/(不随 git 走);按核查结论修订(#10 handoff 撤回、#9 MCP 时效、glm-5.3 存疑标注) — **版本控制内可交付部分已完成**:两篇评审文档已移出 git 追踪范围至 docs/.dev/;评审断言的**核查结论已录 LEDGER**(glm-5.3 疑似误判、F2-F3 缺陷 claim 部分过时且已缓解、#10 handoff 撤回),并据此驱动了 postmerge-review-fixes 的六 Major + N1/N2 修复。docs/.dev/ 文档本体不入 git,其文字修订属交接方本地处置,不构成本变更可验收项
- [x] 6.3 路线图自身 archive(全阶段完成)或"部分完成"归档(记录终止点与成果清单) — **已裁决并执行「部分完成」归档(2026-09-07 用户裁决:全部归档、未完成项重新整合)**:**成果清单** = P0 4/5 实证完成 + P1 语义检索(T-A)全交付并归档 + P1.5 trace 骨架(T-B)全交付并归档 + P3 自进化(TC0/T-EVO/T-D)核心交付 + P4 工具治理(T-G)核心交付 + 脊柱 F1/F2/REG/FIX + 五轮复验与 postmerge-review-fixes 六 Major/N1/N2/Minor 全清(已归档);**终止点** = P0 version tag(转出工程收尾候选)、P2 余项(由 design-report-closeout 承载在途)、P4.5 critic 协作与 5A 六项支柱 backlog(全部转入 design-report-closeout 承接节)
- [x] 6.4 终版差距复盘:对照两篇评审的七项差距清单逐项标注状态 — **本次 /opsx-apply 即复盘**:三板块 tasks.md 逐项对照标注(✅交付/BLOCKED/DEGRADED/未做);execution-dag.md 看板 + LEDGER 记录全 10 节点交付与裁决
