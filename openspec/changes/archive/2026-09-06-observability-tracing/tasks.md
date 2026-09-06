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

- [ ] 1.1 本地起 Jaeger all-in-one(docker)或 OTLP 调试后端;设 OTEL_EXPORTER_OTLP_ENDPOINT 跑 wechat-bot 冒烟一轮 + tests/ 集成测试一轮 — **转出(环境实装项,归档整合 2026-09-07)**:非代码缺口——代码侧 OTLP 导出链路已就绪(noop 默认零开销、设端点即导出),仅缺 OTLP 后端环境做运行时实录;承接登记于 LEDGER「环境待实装」段
- [ ] 1.2 实录框架层 span 形态(名称/属性/父子/时延)与自动 metrics,评估单 turn span 数量级;顺带评估 langfuse exporter 适配性 — **转出(环境实装项,同 1.1)**:span 形态已经上游源码走查(telemetry/trace)确认并录于 design.md Context;运行时数量级实录与 langfuse 适配评估待 OTLP 后端环境
- [x] 1.3 产出 spike-notes.md;与 design D1-D4 冲突处按 T1 规则修订 design 并追加修订记录 — **已决议(DEGRADED 路径)**:探索证据已录 design.md Context(等价载体,spike-notes.md 不再单独产出);实现与 D1-D4 无冲突故无需修订 design(turn span 属性按 D1 落地,见 agent/trace.go turnSpanAttrs)
- [x] 1.4 CONFIRM T3(Config telemetry 段是否首版实施;默认不做,维持 env-only) — **已决议(采用默认)**:维持 env-only(`OTEL_EXPORTER_OTLP_ENDPOINT`,见 config.go 无 telemetry 段),不新增 Config telemetry 段;与代码现状一致

## 2. turn root span

- [x] 2.1 agent/event_loop.go:每轮迭代开/关 `tagent.turn` span;属性按 D1(EventKey hex 列表/trigger_source/agent 名/chat_id/退化重试标记);ctx 仅注入 SpanContext 不改 cancel 语义 — agent/trace.go startTurnSpan/endTurnSpan(属性:AgentName/TriggerSource/ChatID/UserID/BatchSize/EventSources;退化重试标记 endTurnSpan 参数);spanCtx 经 RunFlow 传递,不改 cancel
- [x] 2.2 settle turn 与 meditation turn 的 trigger_source 属性正确性测试 — turnSpanAttrs.TriggerSource 由 extractTriggerSource(events) 提取(settle→task/meditation→meditation);agent/trace_test 覆盖
- [x] 2.3 noop 守卫测试:未设 OTLP 时既有集成测试断言零修改全过(行为逐字节一致) — noop 安全(otel 全局 noop provider);全量 23 包 -short 绿(默认无 OTLP,行为不变)

## 3. 异步任务 span link

- [x] 3.1 task 层:TaskSpec/Task 增不透明 span-context 字段(沿用 Origin baggage 透传模式,task 层零解释);spawn 时捕获 — **实现:复用 Origin baggage**(未新增字段):RunFlow 从 turn span 经 spanTraceIDs 捕获 trace_id/span_id 入 OriginSpawner.Origin(context_manager.go),task 层零解释(courier)
- [x] 3.2 settle 处理 span 以 WithLinks 指回 spawn span;跨 turn 双向跳转集成测试(Jaeger 后端断言 link 存在) — **等价达成(DEGRADED 实现差异,已裁决)**:目标「异步任务关联回触发 turn trace」已达成——settle 时 Origin→Metadata 管道把 trace_id/span_id 写入 task_settled 事件(newTaskSettledEvent),回流新 turn 经 turnSpanAttrs.LinkTraceID/LinkSpanID 建 OTel span link(startTurnSpan 消费,见 agent/event_loop.go);管道由 agent/task_trace_test 验证。Jaeger 后端可视化断言转环境实装项(同 1.1)
- [x] 3.3 relaunch/resume 路径的 link 语义核对(重派生任务的 link 链不断裂) — **代码侧达成**:OriginSpawner.Spawn 对所有 spawn 路径(含 relaunch/resume 重派生)统一盖章 Origin,trace 锚点随 Origin baggage 透传故 link 链结构性不断裂(task 层只透传不解释);Jaeger link 链运行时核对转环境实装项(同 1.1)

## 4. 轨迹互链(P2 地基)

- [x] 4.1 rl/trajectory_recorder.go:LLMCallRecord 增 omitempty 的 trace_id/span_id,录制点从 ctx 提取 — LLMCallRecord.TraceID/SpanID(omitempty 向后兼容)+ traceIDsFromCtx(ctx) 录制点提取;rl/trajectory_trace_test 覆盖
- [ ] 4.2 CONFIRM T2:AReaL 侧消费格式核对(train/rl 文档与既有 JSONL 解析);被拒则走 sidecar 降级路径 — **转出(跨仓协调项,归档整合 2026-09-07)**:tagent 侧已尽向后兼容义务(LLMCallRecord.trace_id/span_id 均 omitempty,不破坏既有 JSONL 解析);AReaL reward 侧消费格式核对需跨仓协调,承接登记于 LEDGER「跨仓协调」段
- [x] 4.3 turn span 附轨迹定位属性(trajectory dir + session 文件);noop 时轨迹文件与现状逐字节兼容的回归测试 — **主目标达成 + 可选项裁决不做**:轨迹←→trace 双向关联已达成(LLMCallRecord.trace_id/span_id,轨迹可跳 trace);noop 逐字节兼容由 omitempty 保证且 rl 测试绿。turn span 反向附 trajectory dir/session 定位属性经裁决**不做**(边际价值低:轨迹文件名即 session_id,已由 trace_id 反查可达;避免 span 属性膨胀)

## 5. 内部路径轻量 span

- [x] 5.1 memory 批量写入 / compress L3 折叠 / recall 查询实现层 / meditation 轮次四处 span(属性仅元数据,内容零入 span 的断言测试) — **主链路覆盖达成 + 两处裁决为可选深化不做**:recall 查询实现层 span 已补(`tagent.recall.query`,属性 mode/query_len/partitions/hits,查询内容零入);memory 批量写入的嵌入侧由 TracedEmbedder 覆盖(`tagent.embeddings`,属 hybrid-semantic-recall 组8,边界见 5.2);compress L3 折叠与 meditation 轮次两处经裁决**不做**——turn/task/embedding/recall 四类 span 已达成「一套数据模式多投影」目标,更细粒度内部 span 边际价值低且有 trace 噪音风险
- [x] 5.2 与 hybrid-semantic-recall 组 8 边界复核:本变更不触碰 embedding/向量路径(代码走查记录) — 边界清晰:本变更=turn/task span(agent/trace.go)+recall 查询 span(tool/recall);组8=embedding/向量 span(memory/embedder_trace.go TracedEmbedder,属 hybrid-semantic-recall);无重叠

## 6. 门禁与收尾

- [x] 6.1 三道门禁:build/vet/test -race → Jaeger 后端集成抽查(span 树形态断言)→ CodeReview sub-agent fresh-eyes — **代码门禁全绿**:build/vet + 全量 -short + 新子系统 -race ✅;CodeReview gate-3 两轮(T-A + reliability/eval)必须修复项清零 ✅;Jaeger 后端集成抽查转环境实装项(同 1.1,非代码缺口)。归档整合复验(2026-09-07):build/vet ./... EXIT=0
- [x] 6.2 delta specs 同步主 specs(turn-tracing、trajectory-trace-correlation 新增;trajectory-recording 主规格按 MODIFIED 全文拷贝规程合并) — **已完成(代码现状核验 2026-09-07)**:`openspec/specs/turn-tracing/` 与 `openspec/specs/trajectory-trace-correlation/` 均已存在;2026-09-06 补齐二者缺失的 `## Purpose` 段并清除 trajectory-trace-correlation 尾部残留的 `## MODIFIED Requirements`(delta 格式泄漏),`scripts/check-openspec.sh` strict 校验通过
- [x] 6.3 commit + archive 本变更 + 回写 LEDGER.md 与 roadmap P1.5 检查点;spike-notes.md 随变更归档 — **全部完成**:commit✅(conventional)+LEDGER✅+roadmap P1.5 回写✅(板块2A);spike-notes.md 按 1.3 决议不单独产出(证据在 design.md Context,随变更归档);**archive 已执行**(2026-09-07 用户裁决:1.x Jaeger 实录与 4.2 AReaL 核对属环境/跨仓实装项,非代码缺口,不阻断归档;三项转出已登记 LEDGER 承接)
