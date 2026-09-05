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

## 重编原则(后续清账沿用)

1. 归档区不改写,裁决集中记录于本账本;
2. "问题是否还在"以代码现状为准,不以任务勾选状态为准(勾选落后于演进是停滞变更的通病);
3. 未动工变更复活前必须做前提核验(挂载点是否已被退役/重构);
4. 活跃集目标规模:≤ 1 张地图 + ≤ 2 个执行中变更(单维护者节奏)。
