# Design: Observability Tracing

## Context(交接必读:探索证据全录)

2026-09-05 探索核实的四层现状(全部有 file:line 证据,接手者可复核):

| 层 | 现状 | 证据 |
|---|---|---|
| 后端接线 | wechat-bot 已有 OTLP 开关:`OTEL_EXPORTER_OTLP_ENDPOINT` 非空时 `telemetrytrace.Start(WithEndpoint, WithServiceName("tagent-wechat-bot"))`,未设时 noop 零开销 | examples/wechat-bot/main.go:200-214 |
| 框架层埋点(自动) | trpc-agent-go 在 llmflow(invoke_agent/chat)、functioncall processor(execute_tool)、a2a/chain/graph agent 埋 span;internal/telemetry 有 chat/tool/agent 三类 metric 与 trace_embedding | trpc-agent-go internal/flow/llmflow/llmflow.go、internal/flow/processor/functioncall.go、internal/telemetry/*.go |
| 上游语义约定 | GenAI semconv 已定义 embedding 属性(gen_ai.embeddings.dimension.count 等)与 workflow/server 约定;另有 langfuse exporter(LLM 观测后端,完整 span processor) | trpc-agent-go telemetry/semconv/trace/embedding.go、telemetry/langfuse/ |
| tagent 自有层 | **全缺口**:turn 无 root span(runEventLoop 持 loopCtx background);EventKey/chat_id/trigger_source 未进属性;task spawn/settle 跨 turn 无 link;TrajectoryRecorder 与 trace 零关联 | agent/event_loop.go:17/89、agent/lifecycle.go:92(loopCtx 创建)、rl/trajectory_recorder.go:30-68 |

tagent 调用链(交接者需理解的骨架):`StartLoop → runEventLoop(bus.Pull 批量拉事件)→ ContextManager.RunFlow(ctx,msg) → runner.Run → llmagent(框架 span 在此层自动产生)→ 工具执行`。子 agent 经 AgentToolWrapper.Call 递归走同一链;异步任务经 task.TaskSpawner 在工具 Call 内 spawn,settle 后经 bus 以 task_settled 事件回到**新的** turn。

TrajectoryRecorder 现有形态:包装 model.Model,每次 LLM 调用录 LLMCallRecord(request/response/endpoint)到 {dir}/{session_id}.jsonl;RL 侧(AReaL)消费。它与 trace 的关系是"同一事实的两种投影":轨迹面向训练(全量 IO),trace 面向运维(因果树+时延)——互链后任一世界可跳转到另一世界。

## Goals / Non-Goals

**Goals:**

- 一个 turn = 一棵 trace:root span 串起框架自动 span,EventKey 等事件驱动维度可查询。
- 异步任务因果不断:spawn↔settle 经 span link 可双向跳转。
- RL 轨迹与运维 trace 双向互链,为 roadmap P2(反馈归因,Reef 式 record-id)预铺基础设施。
- 未设 OTLP 时:noop、零开销、零行为变化(现状语义不变)。

**Non-Goals:**

- 不扩展 metrics 体系(上游自动 metrics 先经 spike 验证够用与否,不够再立项)。
- 不接入 langfuse(spike 中评估并记录结论,实施另议)。
- 不做向量链路 span(归 hybrid-semantic-recall 组 8,但本变更 spike 结论与其共享)。
- 不做 UI/面板;不改任何工具 Declaration。

## Decisions

### D0 spike 先行(任务 1.x,design 修订门)

本地 Jaeger(all-in-one docker)或 OTLP 调试后端,跑 wechat-bot 冒烟 + tests/ 集成测试各一轮,实录:框架 span 名称/属性/父子形态、既有 metrics、noop 行为。产出 spike 记录(存本变更目录 spike-notes.md)。D1-D4 的属性命名与挂点若与实录冲突,以实录为准修订本 design(修订记录追加在文末)。理由:框架 span 形态只有运行才可见,先写死后返工违背"前提核验"清账原则。

### D1 turn-as-trace:root span 挂点与属性

- 挂点:runEventLoop 每轮迭代(bus.Pull 返回非空批后、RunFlow 前)开 span,轮结束(settle/退化重试后)关闭;span 名 `tagent.turn`。
- 属性(初版,spike 后可调):`tagent.turn.event_keys`(批内 EventKey hex 列表)、`tagent.turn.trigger_source`、`tagent.turn.chat_id`/`user_id`(metadata 有则加)、`tagent.turn.degenerate_retry`(bool)、`tagent.agent.name`。
- ctx 传播:span ctx 替换 RunFlow 入参 ctx 的 trace 部分(仅 WithSpanContext,不改 cancel/timeout 语义)——框架层 span 自动挂为子树。
- 退化重试(event_loop.go:107 一带)不另开 root span,以事件+属性标记(同一 turn 语义)。

### D2 异步任务 span link

- spawn 侧:ActionTool/AgentToolWrapper 经 TaskSpawner.Spawn 时,把当前 span context 序列化存入 Task(新增不透明字段,task 层不解释——与既有 Origin baggage 同模式,task.TaskSpec 已有先例)。
- settle 侧:task_settled 触发的新 turn 中,处理该任务的 span 以 `otel trace.WithLinks` 指回 spawn span context。
- 语义:link 而非 parent(settle turn 有自己的 root;跨 turn 因果用 link 是 otel 正规表达)。

### D3 轨迹互链(P2 地基,最小面)

- TrajectoryRecorder 录制点从 ctx 提取 trace_id/span_id(otel trace.SpanContextFromContext),LLMCallRecord 增可选字段 `trace_id`/`span_id`(空=noop 或未启用,JSON omitempty——旧消费者向后兼容)。
- 反向:turn root span 属性加 `tagent.trajectory.dir` 与 `tagent.trajectory.session_file`(启用 TrajectoryDump 时)。
- 不做:record-id 发放/feedback 绑定——那是 P2 本体,此处只铺关联字段。

### D4 memory/压缩/meditation 轻量 span

- 三处:StoreEvent 批(memory 写入路径)、L3 压缩折叠(compress)、recall 查询(tool/recall 内部实现层);meditation 轮次(agent/meditation)。
- 纪律:span 全部开在实现内部,不进工具 Declaration 与 LLM 可见面;属性只放非敏感元数据(条数/耗时/分区 id),**不放事件内容**(隐私与体积)。

### D5 配置与降级

- 启用机制不变:env `OTEL_EXPORTER_OTLP_ENDPOINT`(宿主应用调 telemetrytrace.Start);tagent 框架侧只消费 ctx 中的 tracer(noop 默认)。
- Config 可选 `telemetry:` 段(service_name/sampling_ratio),仅当宿主未显式 Start 时由 tagent.New 按配置代启(便利项,可后置);缺省不启用。
- 采样:默认 parentbased_always_on;高流量场景由宿主 env/配置调整(不进本变更)。

### 预留确认项(执行时核对)

| # | 决策点 | 默认降级路径 |
|---|---|---|
| T1 | spike 实录的框架 span 形态与 D1 属性方案冲突处 | 以实录为准修订 design(修订记录追加) |
| T2 | 轨迹互链字段是否会被 AReaL 侧消费格式校验拒绝 | 字段 omitempty + 先在 rl/ 侧回归测试;被拒则改为 sidecar 文件记录映射 |
| T3 | Config telemetry 段是否首版实施 | 不实施→维持 env-only(现状),段设计留档 |

## Risks / Trade-offs

- [span 量级:每 turn 一棵树,高频 bot 下导出压力] → 采样配置留口(D5);属性不含内容,单 span 体积小;spike 实测单 turn span 数后评估。
- [ctx 改造引入回归:RunFlow ctx 被替换 trace 部分] → 仅注入 SpanContext,cancel/value 语义不动;集成测试覆盖 noop 与启用双态。
- [轨迹格式变更影响 RL 消费] → omitempty 增量字段 + T2 降级路径。
- [task 层新增字段违背"task 层不解释 baggage"原则] → 沿用 Origin 同款不透明透传模式,task 层零解释逻辑。
- [与 hybrid 变更的组 8 边界模糊] → 本变更不碰 embedding/向量路径;两变更各自 spec 的"声明区零变化"守卫测试互为佐证。

## Migration Plan

纯增量:未设 OTLP 时零行为变化(与现状一致);轨迹新字段 omitempty 向后兼容;回滚 = revert(无状态迁移)。

## Open Questions

- 全部转化为 T1-T3。langfuse 评估结论在 spike-notes.md 中记录,不阻塞本变更。
