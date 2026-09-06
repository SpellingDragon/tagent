# design-report-closeout 设计

## Context

本变更承接 docs/.dev/tagent下一步迭代设计报告.md（D1-D5 五方向）与 2026-09 大迭代交付代码的差距核对结论（五路 sub-agent 逐项判定，证据在会话记录与 LEDGER）：D1 ~60%、D2 核心 90%/策展 40%、D3 ~55%。用户裁决：**单容器 P0+P1**、approve 设计统一实现优先「微信消息注入+CLI」、D5 前置（http_api_test 恢复/jsonschema 决策/报告勘误）随 D5 M2 走不在本变更。约束：不做设计外变更；设计内项须保障实现成熟度（fail-before/pass-after 回归 + 门禁）。

## Goals / Non-Goals

**Goals**：P0 四项先行修复（F3/F2/bundle_id/F5）+ P1 五块核心承诺收尾（feedback 闭环、approve 通道、巩固触发、goal 工具+降级行为层、mem_spill 双写+governance 非骨架）。
**Non-Goals**：D4 三支柱（obs/evals/行为回归）、D5 M1-M4（含 http_api_test 恢复与 jsonschema 决策）、cassette+replay 门、故障注入矩阵、novelty 巩固触发（依赖 agent 篇缺口 4）、报告正文改写（勘误附录随 D5 M2）。

## 决策

### D1-A feedback 闭环（依据报告 §4.8，适配现状）
- **类型**：`feedback` 经 EventTypeSpec 注册（正 key / Role=system / TTL 可配置默认 30 天 / 不进 LowValue / Recallable=true）；subtype 经 Metadata（`user` / `task_settle` / `api`）。
- **绑定**：FeedbackBinder 纯函数——`feedback(child_key, parent_key)` 经 RelationStore.SetParent 建因果边（零新索引，复用既有因果链）；feedback 事件 Content 存结构化 JSON（verdict/rating/note/source）。
- **来源两路先行**：①OnSettle——TaskManager settle 时对 spawn turn 的 agent_output 自动记 task_settle verdict（completed=positive/failed=negative，suspect 不记）；②HTTP API `POST /feedback`（body: event_key+verdict+note）挂 rl/httpapi。自评来源不做（随冥想智能化）。
- **消费**：MetricGuardrail canary 窗口补 `negative_feedback_rate` 判据（join 键=feedback 事件 → 因果边 parent 的 bundle_id 盖章，依赖 D1-B）。

### D1-B bundle_id 归因盖章（F1 尾款）
- plugin/attribution.go Attribution 结构补 `BundleID string`；RunFlow 组装 Attribution 时从 evoStore.ActiveBundleID() 读取（nil 安全——evolution 未启用时为空串，不写键）；MemoryPlugin 构造期盖章与 persistBusEvent 双路径同步；evolution 的 EvidenceSource 归因从「ActivationLog 时间窗近似」升级为 bundle_id 精确 join（时间窗保留为缺章回退）。

### D1-C approve 通道（用户裁决：设计统一，实现优先 C+A）
- **统一抽象**：ApprovalChannel 接口（Deliver(request) error）+ 审批状态机（requested→responded(approve/reject)→consumed）；审批请求经既有 ApprovalManager 落盘（digest 绑定不变）。
- **入口 A：CLI**——`tagent approve <digest> [--reject]`：直接对 approvals 目录的 pending 请求写回应文件（零新服务器）。
- **入口 C：微信消息注入**——pending 审批达到时经 EventBus 发布 external_input（source=approval）渗透给用户；用户回复 `approve <digest>` / `reject <digest>` 由 wechat-bot 侧 listener 解析并写回应文件（listener 挂 examples/wechat-bot，框架只提供 ApprovalChannel 抽象与解析纯函数）。
- 通道失败不阻塞审批门：Deliver 失败仅记日志，审批仍可经文件/CLI 完成（闸不是墙）。

### D2-A 巩固触发（依据报告 §4 巩固节，两路先行）
- 配置：`memory.engine.consolidation: { capacity_threshold: 200, min_source_events: 3, snooze: "24h" }`（全零值=禁用，默认零值=纯 manual 行为不变）。
- 容量路：store 写入旁路计数（引擎 Index 后 hook）超阈值→生成冥想 hint 消息（经 EventBus source=consolidation_hint，非自动执行）。
- 冥想 hint 路：meditation digest 附「可巩固候选」清单（容量 TopN 未巩固边界事件）；SNOOZED 状态记于 `0:vmeta:consolidation_state`（KV，建议记录 digest 防重复打扰）。
- 执行权仍在 LLM+memory_consolidate 工具（建议式不变）；min_source_events 硬门控在 BuildConsolidationEvent 前置校验。

### D3-A goal 工具五件套 + 持久化
- 五工具走既有 PlainTool 注册（entry only，治理闸包裹与 refine 同级）；goal_declare/goal_resolve 写 governance 事件（subtype=goal_declared/goal_resolved，审计持久随 Ledger）+ GoalRegistry 内存态刷新；GoalRegistry 启动时从 governance 事件回放重建（rebuild 模式对齐 DenialLedger.BindStore）。

### D3-B 降级行为层（警告级，默认可配置关闭）
- model：runEventLoop 检测 DepModel degraded→下一 turn 前退避（`degradation_model_backoff: "5s"`，默认 0=关闭）；恢复即正常。
- mcp：mcp_call 入口检测 DepMCP degraded→直接返回熔断 result（含自纠材料「MCP 依赖降级中，稍后重试」）+ 半开探测（每 N 次放行一次，N=`degradation_mcp_probe_every` 默认 5）。
- disk：TaskManager Spawn 前检测 DepDisk degraded→拒绝新 spawn（返回可读原因，进行中任务不受影响）。
- 全部行为仅在上报层（DegradationManager）已启用时生效；每项独立配置开关，默认关闭（零行为变化原则）。

### P0 三项修复细节
- **F3**：replayWAL 中间坏行→跳过+`wal_quarantine` 计数（log warn + Stats 暴露）；尾部坏行保持现行为（截断）；**坏行导致 kv.json 本体损坏仍启动失败**（fail-fast 正确语义，报告同界）。
- **F2**：outputCh 发送 select 加 `time.After(2s)`→超限走 OutputOverflow 落盘（`<workspace>/tool-output/output-overflow/`，与 task_settled 转储同构）+ 返回摘要票据事件；SessionHook default 丢弃分支加计数（metrics 可见）。不阻塞、不丢弃、不无限等。
- **F5**：不统一（hy3 语义正确）；README 环境变量表分组「GLM Coding Plan（ZAI_API_KEY）/ 混元（TENCENT_HY_API_KEY）」+ ci.yml 注释声明两个 secret 均可选。

### 五分歧裁定（随本变更固化 LEDGER）
①engineBridge 替代 VectorStore（保持）②快慢道替代五态（保持；replay 门留 cassette 汇合）③同步 VectorRemover 替代三层惰性（保持；悬挂率统计并入 D2 诊断后续）④时间窗 canary（保持；feedback 落地后可加样本下限）⑤OTel 路线（保持，超越报告 non-goal）。

## 风险与开放问题

| 风险 | 缓解 |
|---|---|
| feedback 自动来源误报污染 guardrail | OnSettle 只记确定性 verdict（completed/failed），suspect 不记；rate 判据阈值独立配置 |
| 微信审批消息打扰 | 仅 pending>0 时渗透；digest 短格式；SNOOZED 类比不重复 |
| 降级行为层误伤正常流 | 三行为全部默认关 + 独立开关 + 行为注释「闸不是墙」 |
| mem_spill 双写引入投影污染 | 重放路径 Append 失败仅记日志（与主链路同语义）；回归测试覆盖 |
| goal 事件回放重建与运行时竞态 | 构造期单线程 rebuild（对齐 DenialLedger 先例） |

## 迁移与兼容

全部增量、零值默认=行为不变；governance 非骨架标记改变压缩行为（该类事件保留全文，量小）；无存储 schema 变更（feedback 走既有 FullEvent）。
