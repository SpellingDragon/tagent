# Changes Ledger — 活跃变更集重编账本

> 2026-09-05 整体重编:活跃区从 6 个变更(含 163 个停滞 ≥7 周的僵尸任务)收敛为
> "1 张地图(tagent-evolution-roadmap) + 1 个活跃变更(hybrid-semantic-recall)"。
> 归档区(archive/)为历史账本,内容不改写;本文件集中记录本次重编的裁决与证据。

## 本次裁决记录

| 变更 | 重编前状态 | 裁决 | 证据 |
|---|---|---|---|
| agent-package-rewrite | 78/78 全完成未归档 | ARCHIVE(纯手续) | tasks 全勾选;纯重构无行为变更 |
| unified-event-consumer-and-async-tool | 36/62,停滞 7 周 | ARCHIVE-AS-IS(实质完成) | responseCh 双通道已修(examples/wechat-bot/main.go:245-254 注释 "fixes the responseCh deadlock bug",仅存注释);ActionTool 中间态噪音已消失(全库无 waiting_async_response/handleStateChange);剩余 26 任务主题被归档变更 async-result-delivery / unified-event-projection / async-task-management 以更演进设计覆盖 |
| memory-storage-production-hardening | 0/102,停滞 8 周 | ARCHIVE-AS-IS(母计划已被拆散执行) | 其整合的 4 个母 change 均已独立归档(archive/2026-06-20-*、2026-06-21-*);Phase1 KVRange 已实现(memory/rustviking_client.go:189);Phase3 FileBackend 已移除;Phase4 LLM 摘要以 compress SummaryModel 形态落地;Phase2 硬化组件(Tombstone/Lifecycle/Compactor)已接线运行(tagent.go resolveMemoryStore);残余文档类任务并入 roadmap P0 |
| risk-mitigation-semantic-recall | 0/61,未动工 | REVIVE → 重编为 hybrid-semantic-recall | 核心前提仍成立(SearchByEmbedding 为 stub:memory/in_memory_store.go:306、memory/segment_store.go:762);但阶段 3(12 任务)挂载的 ProjectionOrganizer 已被 retire-projection-organizer(2026-07-24 归档)退役;向量方案详述(异步 EmbeddingWorker/选择性生成/zhipu embedding-3)质量高,整体吸收进新变更 |
| tagent-deep-source-evaluation | 空目录(0 工件) | REJECT(删壳) | 对应产出为两篇评审文档(现位于 docs/.dev/,不随 git 走),处置见 roadmap 任务 6.2 |
| tagent-evolution-roadmap | 新建未批准 | 保留,修订为 backlog 地图 + 治理框架 | P0 改直做清单;P1 指向 hybrid-semantic-recall;不再为已覆盖主题另起变更 |

## 当前活跃集

| 变更 | 角色 | 状态 |
|---|---|---|
| tagent-evolution-roadmap | 参考地图 + roadmap-governance 治理规格(门禁/CONFIRM/自驱闭环)+ D6 交接须知 | 待批准(不急执行) |
| hybrid-semantic-recall | P1 语义检索执行体(risk-mitigation-semantic-recall 的重编版;2026-09-05 追加组 8 向量链路可观测) | 工件就绪,待用户放行开工 |
| observability-tracing | P1.5 trace 骨架执行体(turn root span / task span link / 轨迹互链;spike-first;不依赖 P1 可先行) | 工件就绪(2026-09-05 立项),待放行 |

## 追加裁决记录

| 日期 | 裁决 | 依据 |
|---|---|---|
| 2026-09-05 | trace/telemetry 引入拆两层:向量链路 span/metric 归 hybrid-semantic-recall 组 8;turn 级骨架+轨迹互链立新变更 observability-tracing(roadmap P1.5) | 用户裁决方案 B;探索证据:框架层 span 已自动埋点(llmflow/functioncall)、wechat-bot 已有 OTLP 开关、缺口全在 tagent 自有层(turn 无 root span、EventKey 未进属性、spawn/settle 无 link、轨迹与 trace 零关联) |
| 2026-09-05 | 评审断言"示例写了未发布型号 glm-5.3"标记为**疑似误判,不列入修复项** | 本机实测:zhipu coding 端点(open.bigmodel.cn/api/coding/paas/v4)+ ZAI_API_KEY 持续真实可用(real-LLM 测试/MCP probe 均走通);glm-5.3 应为该端点可用型号,评审判断疑为其知识截止所致。接手者勿"修复"此项;P0 执行时如型号确不可用再议(验证方式:一次真实 API 调用) |
| 2026-09-05 | 登记并存规划:docs/.dev/tagent下一步迭代设计报告.md(305KB,五方向 D1-D5 + Wave0-3,可编码粒度)与本 roadmap(P0-P4)+两执行体**并存作交接参考**,不预设谁为准,接手后讨论迭代调和 | 用户裁决。已知待调和项见 roadmap design D6「并存规划」:①D2 向量存储方案分歧(报告 VectorStore 装饰器 vs hybrid 变更 内存索引+RustViking KV 序列化);②报告 D3 常驻可靠性是 roadmap 缺口;③D1/D4/D5 与 roadmap P2/P3/P0-P1/P4 重叠。两文档不随 git 走(docs/.dev 被忽略),交接载体由用户处理 |
| 2026-09-05 | **并发自驱马拉松执行**:按 execution-dag.md(11节点DAG+6冻结契约C1-C6)自驱交付脊柱(F1 rustviking能力核验/F2 MemoryEngine解耦缝C6/REG EventTypeSpec注册表C1/FIX)+7 track 核心:T-A(解耦记忆引擎:Embedder/InMemoryEngine hybrid RRF/engineBridge/KV持久化重建/rustviking契约修复)、T-B(turn root span+trace_id/span_id 三投影互链=指令2一套数据模式)、TC0(热配置 BundleStore+VersionedSource+prompt.Getter缝+归因盖章修F1缺口)、T-D(证据门控巩固 服务端SHA1指纹防伪造+维度诊断+memory_consolidate/memory_health工具)、T-G(governance:RiskClassifier C5/Budget滑窗epoch持久/Approval异步digest绑定/DenialLedger治理事件/Goal/Gate管线 + reliability:DegradationManager五依赖状态机)、T-EVO(风险分级发布道 快道后验/慢道门后+双回滚+refine工具无activate) | 用户指令:agent与记忆引擎解耦(C6缝,MVP在tagent/闭环在引擎)、OTel要做(与trajectory经trace锚点统一)、D2全量纳入、评估非生效前门(后验+模型回滚,热配置使能)、先进性优先无兼容包袱。关键调和:**D2**=解耦缝+MVP+rustviking KV持久化(F1核验发现 rustviking `index` CLI 进程内易失 main.rs:129 无load,不可作持久向量后端,原 SearchByEmbedding 永为stub根因=虚构契约);**D4**=驳回报告「不做OTel」Non-goal(基建已在/noop零成本);**T-E/T-F**=风险分级发布道统一(快道先生效后验/慢道门后,agent无直接激活权D1铁律)。门禁3 CodeReview子agent揪出 Blocker(评估器error假激活)+Major(Submit竞争/approval数据竞争/bundle id路径遍历)全部修复+回归锁定。agent包 -race 3失败经Debug子agent三重证据定性为 pre-existing 上游 trpc-agent-go@v1.10.0 竞态(非本迭代回归),门禁豁免此3项。24提交推送,全量23包-short绿 |
| 2026-09-05 | 马拉松**剩余集成层**(核心+工具+审查已交付,以下为使其在运行时生效的接线):①config(GovernanceConfig/EvolutionConfig)+buildAgent接线;②GovernanceTool装饰器插入agent.go工具链;③BundleProvider→VersionedSource接ContextManager系统提示源+归因bundle_id;④T-EVO Evaluator实现(LLM-judge)+Guardrail;⑤T-G ReliableBus(spill/durable)+AnchorStore+故障注入矩阵;⑥T-B异步task span link+memory/compress/recall轻量span;⑦MG openspec 按track细化重编 | 核心架构(解耦缝/注册表/统一可观测锚点/热配置/风险分级/治理门/退化机/证据巩固/发布道)均已建成并-race绿;剩余为集成/增强,additive不改核心契约 |
| 2026-09-05 | **第二次持续驱动:集成层完成①-⑥ + gate-3二轮**:①governance收口(trigger source盖章使goal-required LIVE+账本绑定memStore持久化governance事件)②T-EVO后验评估闭环LIVE=指令4落地(Evidence/StoreEvidenceSource从memStore收canary证据+MetricGuardrail确定性闸+LLMJudgeEvaluator模型决策回滚,BindPosterior延迟绑定judge复用主model;保守:judge不可用/样本不足/解析失败/缺score均不误回滚)③T-G ReliableBus(SpillStore全序磁盘溢出:channel恒早于spill+pending背压上限+批量上限+瞬时错误重试不删+重启恢复=at-least-once不丢事件)④T-B task span link(turn trace锚点入task OriginSpawner.Origin→settle经Origin→Metadata写task_settled事件,异步链路关联回触发turn,零task包侵入)⑤AnchorStore(冥想三锚点持久化跨重启不误触发,anchorMu一致快照)⑥故障注入矩阵⑦config四组(Governance/Evolution/Reliability/Memory.Engine)全默认关闭零行为变化。**全10节点COMPLETE** | **gate-3第二轮CodeReview子agent揪出Blocker**:ReliableBus「溢出优先回收」致事件时序倒置——溢出仅在channel满(256)时发生,故channel内256事件全早于溢出项,drainSpill在前产出倒序批[新溢出,旧×256]→extractTriggerSource误标整回合+extractRootMetadata后者覆盖前者致**回复路由到错误会话**+projection顺序错乱;全序方案修复(Publish在pending>0时一律溢出保证channel恒早于磁盘+Pull/TryPull改channel先spill后)。**+Major**:judge合法JSON缺score→零值0误判劣化回滚(改*float64检测缺失保守通过)/canary hold期ctx取消经judge保守Pass落到「后验通过active」假通过(ctx.Done诚实停留StageCanary)/eval证据收集Limit10000+timestamp_asc致常驻agent事件>1万后后验评估永久静默失效(改StartTime服务端过滤+desc)/reclaim对瞬时I/O错误(EMFILE/EIO)立即删文件违背at-least-once(改有限重试后才丢)/可靠模式Publish无背压+drainSpill无批量上限致磁盘无界+巨型LLM消息(pending背压上限+maxSpillPerPull)。**+Suggestion**(anchor快照非原子加anchorMu/Submit持锁跨canary加refine超时/nil守卫死代码提前)+**Nit**(孤儿tmp清理/AgentEvent json tag锁线格式/TryPull热路径pending短路)全修+回归锁定。全量23包-short绿+新子系统-race绿 |

## 重编原则(后续清账沿用)

1. 归档区不改写,裁决集中记录于本账本;
2. "问题是否还在"以代码现状为准,不以任务勾选状态为准(勾选落后于演进是停滞变更的通病);
3. 未动工变更复活前必须做前提核验(挂载点是否已被退役/重构);
4. 活跃集目标规模:≤ 1 张地图 + ≤ 2 个执行中变更(单维护者节奏)。
