# Tasks: Observability Tracing

> roadmap P1.5 执行体;不依赖 hybrid-semantic-recall,可先行。
> 交接背景与全局上下文见 roadmap design.md「D6 交接须知」;探索证据全录见本变更 design.md Context。
> 预留确认项 T1-T3 见 design.md。
>
> **执行状态(2026-09-05 /opsx-apply 对照核对)**:核心骨架(组2 turn span、组3 task 关联、
> 组4 轨迹互链)已交付并 -race 绿;实现载体为 execution-dag.md 的 T-B 节点(agent/trace.go
> turn root span + trace_id/span_id 经 attribution/Origin 双路径落事件 Metadata 与 trajectory
> LLMCallRecord)。**两处诚实偏离**:(1)组1 spike-first 因环境无 docker Jaeger 降级为「代码走查
> + 已有探索证据」(design.md Context 已录:框架层 span 自动埋点、wechat-bot OTLP 开关);
> (2)组3.2 严格 OTel span link(WithLinks) 降级为「事件 Metadata trace 锚点关联」——达成
> 异步任务关联回触发 turn trace 的目标,但非 Jaeger 可视化的 span-link 边。逐行状态见行尾标注。

## 1. Spike:框架 span 实录(D0,先于一切实现)

- [ ] 1.1 本地起 Jaeger all-in-one(docker)或 OTLP 调试后端;设 OTEL_EXPORTER_OTLP_ENDPOINT 跑 wechat-bot 冒烟一轮 + tests/ 集成测试一轮 — **BLOCKED(环境:无 docker Jaeger)**:降级为代码走查(框架 llmflow/functioncall 自动埋点已确认);实录留待有 OTLP 后端环境
- [ ] 1.2 实录框架层 span 形态(名称/属性/父子/时延)与自动 metrics,评估单 turn span 数量级;顺带评估 langfuse exporter 适配性 — **BLOCKED(同 1.1)**:span 形态经上游源码走查(telemetry/trace)确认,未做运行时实录
- [ ] 1.3 产出 spike-notes.md;与 design D1-D4 冲突处按 T1 规则修订 design 并追加修订记录 — **DEGRADED**:无运行时实录故无 spike-notes.md;探索证据已在 design.md Context 记录;实现未与 D1-D4 冲突(turn span 属性按 D1)
- [ ] 1.4 CONFIRM T3(Config telemetry 段是否首版实施;默认不做,维持 env-only) — **已决议(采用默认)**:维持 env-only(OTEL_EXPORTER_OTLP_ENDPOINT),不新增 Config telemetry 段;与现状一致

## 2. turn root span

- [x] 2.1 agent/event_loop.go:每轮迭代开/关 `tagent.turn` span;属性按 D1(EventKey hex 列表/trigger_source/agent 名/chat_id/退化重试标记);ctx 仅注入 SpanContext 不改 cancel 语义 — agent/trace.go startTurnSpan/endTurnSpan(属性:AgentName/TriggerSource/ChatID/UserID/BatchSize/EventSources;退化重试标记 endTurnSpan 参数);spanCtx 经 RunFlow 传递,不改 cancel
- [x] 2.2 settle turn 与 meditation turn 的 trigger_source 属性正确性测试 — turnSpanAttrs.TriggerSource 由 extractTriggerSource(events) 提取(settle→task/meditation→meditation);agent/trace_test 覆盖
- [x] 2.3 noop 守卫测试:未设 OTLP 时既有集成测试断言零修改全过(行为逐字节一致) — noop 安全(otel 全局 noop provider);全量 23 包 -short 绿(默认无 OTLP,行为不变)

## 3. 异步任务 span link

- [x] 3.1 task 层:TaskSpec/Task 增不透明 span-context 字段(沿用 Origin baggage 透传模式,task 层零解释);spawn 时捕获 — **实现:复用 Origin baggage**(未新增字段):RunFlow 从 turn span 经 spanTraceIDs 捕获 trace_id/span_id 入 OriginSpawner.Origin(context_manager.go),task 层零解释(courier)
- [ ] 3.2 settle 处理 span 以 WithLinks 指回 spawn span;跨 turn 双向跳转集成测试(Jaeger 后端断言 link 存在) — **DEGRADED(部分,实现差异)**:用事件 Metadata trace 锚点关联替代 OTel span link——settle 时 Origin→Metadata 管道把 trace_id/span_id 写入 task_settled 事件(newTaskSettledEvent),回流新 turn 可关联回原 trace;Jaeger span-link 边 + 集成测试 BLOCKED(无后端);测试:agent/task_trace_test 验证管道
- [ ] 3.3 relaunch/resume 路径的 link 语义核对(重派生任务的 link 链不断裂) — **部分**:OriginSpawner.Spawn 对所有 spawn(含 relaunch)盖章 Origin(trace 锚点随 Origin 透传不断裂);未做 Jaeger link 链运行时核对(BLOCKED)

## 4. 轨迹互链(P2 地基)

- [x] 4.1 rl/trajectory_recorder.go:LLMCallRecord 增 omitempty 的 trace_id/span_id,录制点从 ctx 提取 — LLMCallRecord.TraceID/SpanID(omitempty 向后兼容)+ traceIDsFromCtx(ctx) 录制点提取;rl/trajectory_trace_test 覆盖
- [ ] 4.2 CONFIRM T2:AReaL 侧消费格式核对(train/rl 文档与既有 JSONL 解析);被拒则走 sidecar 降级路径 — **BLOCKED(需 AReaL 侧核对)**:omitempty 保证新增字段不破坏既有 JSONL 解析(向后兼容);AReaL reward 消费格式核对留待跨仓协调
- [ ] 4.3 turn span 附轨迹定位属性(trajectory dir + session 文件);noop 时轨迹文件与现状逐字节兼容的回归测试 — **部分**:trace_id/span_id 已入 LLMCallRecord(轨迹←→trace 关联达成);turn span 附 trajectory dir/session 定位属性未做(可选);noop 轨迹兼容:omitempty 保证(rl 测试绿)

## 5. 内部路径轻量 span

- [ ] 5.1 memory 批量写入 / compress L3 折叠 / recall 查询实现层 / meditation 轮次四处 span(属性仅元数据,内容零入 span 的断言测试) — **部分(本次补 recall)**:recall 查询实现层 span 已补(memory_recall.go recallByQuery:tagent.recall.query,属性 mode/query_len/partitions/hits,查询内容零入);memory 批量写入的嵌入部分由组8 TracedEmbedder(hybrid-semantic-recall)覆盖;compress L3 折叠 / meditation 轮次两处标注**可选深化**(主链路 turn/task/embedding/recall span 已覆盖指令2「一套数据模式多投影」目标,细粒度内部 span 边际价值低、有 trace 噪音风险)
- [x] 5.2 与 hybrid-semantic-recall 组 8 边界复核:本变更不触碰 embedding/向量路径(代码走查记录) — 边界清晰:本变更=turn/task span(agent/trace.go)+recall 查询 span(tool/recall);组8=embedding/向量 span(memory/embedder_trace.go TracedEmbedder,属 hybrid-semantic-recall);无重叠

## 6. 门禁与收尾

- [ ] 6.1 三道门禁:build/vet/test -race → Jaeger 后端集成抽查(span 树形态断言)→ CodeReview sub-agent fresh-eyes — build/vet/全量23包-short绿+新子系统-race绿✅;CodeReview gate-3(两轮:T-A + reliability/eval)✅;**Jaeger 后端集成抽查 BLOCKED**(无 docker 后端)
- [ ] 6.2 delta specs 同步主 specs(turn-tracing、trajectory-trace-correlation 新增;trajectory-recording 主规格按 MODIFIED 全文拷贝规程合并) — **待板块统一处理**(/opsx-apply 收尾:specs 同步)
- [ ] 6.3 commit + archive 本变更 + 回写 LEDGER.md 与 roadmap P1.5 检查点;spike-notes.md 随变更归档 — commit✅(conventional)+LEDGER✅;**archive 待裁决**(1.x spike/3.2 Jaeger/4.2 AReaL BLOCKED 项是否阻断 archive 由用户定);spike-notes.md 因 1.3 DEGRADED 无产出
