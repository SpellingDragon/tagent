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

- [ ] 1.1 CONFIRM C1(CI 门禁范围;默认 GitHub Actions 最小集 build+vet+短测试,race nightly)、C11(首个 tag;默认 v0.1.0) — **未决议**:CI/tag 属工程收尾,本次马拉松聚焦架构 track,未触及
- [ ] 1.2 examples/wechat-bot/tagent.yaml 硬编码绝对路径(plan description_file)修复为相对路径并验证加载 — **未核对/未做**:本次仅在 tagent.yaml 加 embedding 配置示例(板块1 5.2),plan 绝对路径修复未做
- [ ] 1.3 .github/workflows/ CI 就绪(real-LLM 测试经 Skip 保护不阻塞) — **未做**:CI workflow 属 P0 工程收尾
- [ ] 1.4 README 依赖声明补齐(tmux、Go ≥1.24、rustviking 可选、ZAI_API_KEY);CHANGELOG 建立 — **部分**:FIX 节点标准化 ZAI_API_KEY(GLM Coding Plan)入 README/tests;tmux/Go/rustviking 依赖声明 + CHANGELOG 未系统补齐
- [ ] 1.5 首个 version tag 打出并 push;门禁①通过即算准出 — **未做**:version tag 属 P0 工程收尾(代码已全程 conventional commit + push,但无 tag)

## 2. P1 语义检索 + 评估基座

- [x] 2.1 语义检索:由 hybrid-semantic-recall 承载——放行即开工,准出后回写此处 — **核心交付(T-A)**:解耦缝 C6 + Embedder + InMemoryEngine(hybrid RRF) + engineBridge + KV持久化重建 + recall hybrid + 组8向量可观测;详见板块1 tasks.md(19/24 勾选)
- [ ] 2.2 CONFIRM C4(评估任务集来源与首批规模;默认从 tests/ real-LLM 契约测试提炼 10-20 组件级 case) — **未决议**:评估任务集需真实 LLM 环境(BLOCKED 同板块1 5.1/5.3)
- [ ] 2.3 评估基座派生子变更(evaluation-bootstrap):evals/ 目录 + trpc evaluation 桥接 + 过程指标埋点;real-LLM flaky 治理 — **部分(不同形态)**:未建 evals/ 目录基座;但 T-EVO 后验评估(Evidence/StoreEvidenceSource 过程指标:治理拒绝率/critical率/事件量 + MetricGuardrail + LLMJudgeEvaluator)实现了「过程指标埋点 + 评估」的运行时闭环形态(服务于自进化回滚,非离线评估任务集)
- [ ] 2.4 顺带项:nanobot skills/ 兼容技能并入 examples skills(SKILL.md 格式核对,注明来源) — **未做**:非架构 track,顺带项
- [x] 2.5 两子项各自三道门禁 → commit → archive → 回写本清单 — 语义检索(2.1)门禁✅(build/vet/race+CodeReview gate-3两轮)+commit✅+回写✅;archive 待板块4裁决;评估基座(2.3)未派生

## 2A. P1.5 可观测 trace 骨架(已立项:observability-tracing,不依赖 P1,可先行)

- [x] 2A.1 CONFIRM:OTLP 后端选型(默认 Jaeger all-in-one docker);langfuse 仅评估不实施 — **已决议(采用默认)**:维持 env-only(OTEL_EXPORTER_OTLP_ENDPOINT),后端选型 Jaeger(langfuse 不实施);运行时实录 BLOCKED(无 docker)
- [x] 2A.2 变更工件已就绪(spike-first,D0 实录后才动实现)——放行即开工 — **核心交付(T-B)**:turn root span + trace_id/span_id 三投影互链 + task span link(事件Metadata关联);详见板块2 tasks.md;偏离:spike-first 因环境降级为代码走查
- [x] 2A.3 三道门禁 → commit → archive → 回写;其轨迹互链字段是 P2 反馈归因的地基(P2 准入前须完成) — 门禁✅(build/vet/race+CodeReview)+commit✅;**轨迹互链地基✅**(LLMCallRecord.trace_id/span_id omitempty,P2 归因可消费);archive 待板块4

## 3. P2 反馈归因闭环(子变更建议名:feedback-attribution-loop)

- [ ] 3.1 CONFIRM C5(反馈来源;默认仅 HTTPAPI /feedback 最小面) — **未决议**:P2 反馈端点未启动(仅地基达成)
- [ ] 3.2 派生子变更并完成工件(EventKey 作 record-id 的 Reef 模式设计;AReaL reward 消费路径衔接) — **地基达成,未派生**:TC0 归因地基(Attribution 双路径盖章,EventKey 入事件 Metadata)+ T-B 轨迹互链(LLMCallRecord trace 锚点)= P2 归因的数据地基;但未派生 feedback-attribution-loop 子变更、未做 Reef 模式 record-id 设计
- [ ] 3.3 检查点:HTTPAPI 新增 POST /feedback(event_key/task_id/score/label/reason),评分作为新事件写入 MemoryStore 并关联目标事件(RelationStore 因果边) — **未做**:HTTPAPI /feedback 端点 + RelationStore 因果边属 P2 主体,本次马拉松未覆盖(注:T-D consolidation 的收据指纹机制是「事件关联」的相邻能力,可复用)
- [ ] 3.4 检查点:TrajectoryRecorder 输出附反馈关联率指标;轨迹 JSONL 可被 AReaL reward 侧消费 — **部分**:轨迹 JSONL 已附 trace_id/span_id(T-B 4.1,可被 reward 侧关联);反馈关联率指标未做(无 /feedback 数据源)
- [ ] 3.5 检查点:recall 可查询反馈事件(票据路径);"反馈→事件→召回"闭环集成测试 — **未做**:依赖 3.3 /feedback 端点
- [ ] 3.6 三道门禁 → commit → archive + specs 同步 → 回写 — **未做**:P2 主体未启动

## 4. P3 Harness 自进化 + 陷阱注册表(子变更建议名:harness-self-improvement)

- [ ] 4.1 CONFIRM C6(RHI 优化器形态;默认离线脚本先行)、C7(陷阱事件类型命名与 TTL;默认 pitfall 类型 TTL 豁免) — **C7 已决议(等价)**:陷阱/经验沉淀用 consolidation 事件类型(TTL 豁免 -1,见 event/registry.go)= C7「pitfall 类型 TTL 豁免」的等价落地;**C6 偏离**:优化器非「离线脚本」而是**运行时 refine 工具 + 风险分级发布道**(T-EVO,更先进:agent 提案→发布道裁决→热配置生效)
- [x] 4.2 派生子变更并完成工件(prompt 制品版本化依托 prompt.Source;pairwise 比较用评估器 + 反馈分数作信号) — **核心交付(TC0+T-EVO)**:prompt 版本化=BundleStore(不可变内容寻址)+VersionedSource(实现 prompt.Getter,依托 prompt.Source 回退);pairwise/评估=LLMJudgeEvaluator(后验评估);载体为 execution-dag track 非派生子变更
- [x] 4.3 检查点:prompt 版本目录与激活机制(候选先评估后激活,可回滚;激活=文件替换即热载生效) — **核心交付(TC0+T-EVO)**:BundleStore(bundles/ 目录 + 原子 active 指针)+ ReleaseManager(风险分级发布道:快道先评估后激活/慢道门后 + 双回滚)+ VersionedSource(回合边界热载生效)
- [x] 4.4 检查点:Harness 优化器最小闭环(读相邻两版轨迹→pairwise→生成候选→评估→报告;人工确认后激活) — **核心交付(T-EVO)**:refine 工具(propose 生成候选)+ ReleaseManager(评估:MetricGuardrail+LLMJudgeEvaluator)+ 风险分级发布道(慢道人工确认/快道后验);**agent 无直接激活权**(D1 铁律)
- [x] 4.5 检查点:陷阱注册表(失败教训事件类型入库;meditation 空闲期提炼陷阱卡片;recall 可按类型召回) — **核心交付(T-D)**:consolidation 事件类型(证据门控 + 服务端指纹防伪造,经 registry 注册即可召回/骨架/TTL豁免)= 陷阱/经验卡片入库;memory_consolidate 工具(meditation 期提炼);recall 可按 consolidation 类型召回
- [x] 4.6 三道门禁 → commit → archive + specs 同步 → 回写 — 门禁✅(build/vet/race+CodeReview gate-3)+commit✅+回写✅(LEDGER);archive/specs 待板块4

## 5. P4 工具治理 + 组织模式(子变更建议名:tool-governance-and-collaboration)

- [ ] 5.1 CONFIRM C8(Docker 沙箱;默认不替换 exec,container 仅可选)、C9(审批通道;默认 allow-list+auto-approve)、C10(trpc team vs 自研 critic;倾向自研) — **C9 已决议(超越默认)**:审批通道=T-G ApprovalManager(critical 异步文件通道批准 + args_digest 绑定防换参),比「allow-list+auto-approve」更完整;**C8 采用默认**(不替换 exec,沙箱未做);**C10 未决议**(critic 未做)
- [x] 5.2 派生子变更并完成工件(crush permission 借鉴:allow-list/session缓存/异步审批/通知;waterfall 中间件链:权限→审批→审计→执行) — **核心交付(T-G)**:GovernanceGate 决策管线(classify→critical批准门→goal检查→预算闸→记账/放行)= waterfall 中间件链形态;借鉴 crush(异步审批 ApprovalManager/审计 DenialLedger/预算 BudgetManager)
- [x] 5.3 检查点:工具执行治理链落地(exec 高危命令 allow-list + 审计日志;审批通道按 C9) — **核心交付(T-G)**:RiskClassifier(C5 四级分级,exec 高危命令规则表:rm -rf/sudo/git push -f 等 critical/high)+ GovernanceTool 装饰器(entry leaf 工具 LIVE)+ DenialLedger 审计(governance 事件持久化)+ ApprovalManager 审批
- [ ] 5.4 检查点:沙箱可选路径按 C8 决议落地或明确不做(记录决策) — **明确不做(C8 默认)**:Docker 沙箱不替换 exec;OS 降权(sudo -n -u)仍是最后防线;记录决策=不纳入本次
- [ ] 5.5 检查点:critic/verifier 协作模式最小可用(plan 产出经 critic 对抗评审后放行;与 AgentToolWrapper/prefix-cache 兼容) — **未做**:critic/verifier 协作(C10)未启动;注:gate-3 CodeReview 子 agent 是「开发期对抗评审」的实践,但非运行时 critic tool agent
- [x] 5.6 三道门禁 → commit → archive + specs 同步 → 回写 — 门禁✅(build/vet/race+CodeReview gate-3二轮揪出Blocker事件时序倒置等)+commit✅+回写✅;archive/specs 待板块4

## 6. 路线图收尾

- [ ] 6.1 全部阶段检查点完成确认(P0-P4 各自 archive 且回写) — **部分**:P1/P1.5/P3/P4 核心✅(实质交付经 execution-dag track);P0(工程收尾)/P2(反馈端点)未做;各阶段未走 archive 流程(以 execution-dag + LEDGER 记账替代)
- [ ] 6.2 两篇评审文档处置:文档已移至 docs/.dev/(不随 git 走);按核查结论修订(#10 handoff 撤回、#9 MCP 时效、glm-5.3 存疑标注) — **部分**:评审断言的核查结论已录 LEDGER(glm-5.3 疑似误判/F2-F3 缺陷 claim 部分过时已缓解);docs/.dev 文档修订由交接方处理(不随 git)
- [ ] 6.3 路线图自身 archive(全阶段完成)或"部分完成"归档(记录终止点与成果清单) — **待板块4裁决**:建议「部分完成」归档(P1/P1.5/P3/P4 核心成果 + P0/P2 终止点),或保持 active 待 P0/P2 续做
- [x] 6.4 终版差距复盘:对照两篇评审的七项差距清单逐项标注状态 — **本次 /opsx-apply 即复盘**:三板块 tasks.md 逐项对照标注(✅交付/BLOCKED/DEGRADED/未做);execution-dag.md 看板 + LEDGER 记录全 10 节点交付与裁决
