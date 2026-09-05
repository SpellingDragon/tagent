# Observability Tracing

> roadmap P1.5 执行体(2026-09-05 立项,归属裁决见 openspec/changes/LEDGER.md)。
> 不依赖 hybrid-semantic-recall,可先行;向量链路自身的 span/metric 归 hybrid 变更组 8,本变更只做 tagent 自有层的 trace 骨架与跨世界关联。
> 交接背景与全局上下文见 roadmap design.md「D6 交接须知」。

## Why

探索(2026-09-05)证实了一个反直觉现状:**tagent 的框架层 trace 已通电但从未被验证**——tagent 经 ContextManager.RunFlow → runner.Run 走 trpc-agent-go 框架路径,上游在 llmflow(invoke_agent/chat span)、processor/functioncall(execute_tool span)、internal/telemetry(chat/tool/agent metrics)已自动埋点;wechat-bot main.go:200-214 已有 OTLP 导出开关(OTEL_EXPORTER_OTLP_ENDPOINT,未设时 noop 零开销)。但 tagent 自有层完全缺席:①turn 无 root span(runEventLoop 持 loopCtx=background,agent/event_loop.go:17),框架 span 串不成"一次对话轮"的因果树;②事件驱动独有维度(EventKey/chat_id/trigger_source)未进任何 span 属性;③异步任务(tmux spawn 与 settle 分属不同 turn)无 span link,因果断裂;④TrajectoryRecorder(RL 世界,全量 JSONL)与 telemetry(运维世界,span 树)互不引用——而两篇外部评审的 P2 建议(反馈归因闭环,Reef 式 record-id)恰恰需要这个互链锚点作为基础设施。外部评审(A 篇主线 6)同时指出行业"可观测性危机"是 32% Agent 项目无法进生产的主因。

## What Changes

- **spike 先行**(设计前提):本地起 Jaeger(或 OTLP 调试后端),设 OTEL_EXPORTER_OTLP_ENDPOINT 跑 wechat-bot/集成测试,实录框架层 span 真实形态(名称/属性/父子关系),据此锁定本变更的属性方案——design.md 的 span 规划以 spike 结论为准修订。
- **turn root span**:runEventLoop 每轮开 root span(建议名 `tagent.turn`),属性:EventKey(canonical hex)、trigger_source、chat_id/user_id(如有)、turn 序号、degenerate-retry 标记;RunFlow 及其下框架 span 自动成为子树。
- **异步任务 span link**:ActionTool spawn 时在 task 上记录当前 span context;task_settled 处理 turn 开 span 时以 otel span link 指回 spawn 处(link 而非 parent——settle 属于新 turn,父子关系不成立)。
- **轨迹互链(P2 地基)**:TrajectoryRecorder 的 LLMCallRecord 附 trace_id/span_id(从 ctx 提取);span 属性反向携带 session_id 与轨迹文件定位(dir+session);noop 时字段为空,零行为变化。
- **memory/压缩轻量 span**:StoreEvent 批量、压缩折叠(L3)、recall 查询三处加 span(仅 OTLP 启用时有效);meditation 轮次加 span。
- **配置面**:保持 env 开关为唯一启用机制(与现状一致);Config 可选 `telemetry:` 段仅承载 service_name/采样率等元数据,缺省 noop。
- 明确不做:metrics 体系扩展(上游自动 metrics 已够用,spike 验证后再议)、langfuse 后端接入(spike 中评估记录结论,不实施)、可视化面板(评审 P5 级,roadmap 未排期)。

## Capabilities

### New Capabilities

- `turn-tracing`: turn 级 trace 骨架——root span 生命周期与属性集、异步任务 span link 语义、noop 降级(未设 OTLP 时零开销零行为变化)。
- `trajectory-trace-correlation`: RL 轨迹与运维 trace 的双向关联——LLMCallRecord 携 trace_id/span_id、span 携轨迹定位属性;关联字段在 noop 时为空且不影响既有轨迹格式兼容性。

### Modified Capabilities

<!-- trajectory-recording(openspec/specs/trajectory-recording/)的 LLMCallRecord 增加可选关联字段:向后兼容(新增字段,旧消费者忽略);delta spec 随本变更提交 -->

## Impact

- 代码:agent/event_loop.go(turn span)、agent/tool_agent.go 或 task 层(spawn 时 span context 捕获)、agent/context_manager.go(settle turn 的 link)、rl/trajectory_recorder.go(trace_id 字段)、memory/ 与 agent/compress/(轻量 span)、config.go(可选 telemetry 段)。
- 依赖:otel API 已在依赖树(go.mod indirect:otel v1.38.0 等);trpc telemetrytrace 已被 wechat-bot 使用——预期零新增直接依赖,若需 otel SDK 直接引用则升为 direct(执行时核对)。
- 不变量:全部观测点在 Engine 侧(loop/runner/store),不触碰任何工具 Declaration(prefix-cache 零影响);noop 默认态下行为与现状逐字节一致;轨迹格式仅增量字段(RL 消费方向后兼容)。
- 验证:spike 实录 + 集成测试(Jaeger 后端断言 span 树形态;noop 断言零导出)。
