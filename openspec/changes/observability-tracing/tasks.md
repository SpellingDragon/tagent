# Tasks: Observability Tracing

> roadmap P1.5 执行体;不依赖 hybrid-semantic-recall,可先行。
> 交接背景与全局上下文见 roadmap design.md「D6 交接须知」;探索证据全录见本变更 design.md Context。
> 预留确认项 T1-T3 见 design.md。

## 1. Spike:框架 span 实录(D0,先于一切实现)

- [ ] 1.1 本地起 Jaeger all-in-one(docker)或 OTLP 调试后端;设 OTEL_EXPORTER_OTLP_ENDPOINT 跑 wechat-bot 冒烟一轮 + tests/ 集成测试一轮
- [ ] 1.2 实录框架层 span 形态(名称/属性/父子/时延)与自动 metrics,评估单 turn span 数量级;顺带评估 langfuse exporter 适配性(仅记录结论)
- [ ] 1.3 产出 spike-notes.md(存本变更目录);与 design D1-D4 冲突处按 T1 规则修订 design 并追加修订记录
- [ ] 1.4 CONFIRM T3(Config telemetry 段是否首版实施;默认不做,维持 env-only)

## 2. turn root span

- [ ] 2.1 agent/event_loop.go:每轮迭代开/关 `tagent.turn` span;属性按 D1(EventKey hex 列表/trigger_source/agent 名/chat_id/退化重试标记);ctx 仅注入 SpanContext 不改 cancel 语义
- [ ] 2.2 settle turn 与 meditation turn 的 trigger_source 属性正确性测试
- [ ] 2.3 noop 守卫测试:未设 OTLP 时既有集成测试断言零修改全过(行为逐字节一致)

## 3. 异步任务 span link

- [ ] 3.1 task 层:TaskSpec/Task 增不透明 span-context 字段(沿用 Origin baggage 透传模式,task 层零解释);spawn 时捕获
- [ ] 3.2 settle 处理 span 以 WithLinks 指回 spawn span;跨 turn 双向跳转集成测试(Jaeger 后端断言 link 存在)
- [ ] 3.3 relaunch/resume 路径的 link 语义核对(重派生任务的 link 链不断裂)

## 4. 轨迹互链(P2 地基)

- [ ] 4.1 rl/trajectory_recorder.go:LLMCallRecord 增 omitempty 的 trace_id/span_id,录制点从 ctx 提取
- [ ] 4.2 CONFIRM T2:AReaL 侧消费格式核对(train/rl 文档与既有 JSONL 解析);被拒则走 sidecar 降级路径
- [ ] 4.3 turn span 附轨迹定位属性(trajectory dir + session 文件);noop 时轨迹文件与现状逐字节兼容的回归测试

## 5. 内部路径轻量 span

- [ ] 5.1 memory 批量写入 / compress L3 折叠 / recall 查询实现层 / meditation 轮次四处 span(属性仅元数据,内容零入 span 的断言测试)
- [ ] 5.2 与 hybrid-semantic-recall 组 8 边界复核:本变更不触碰 embedding/向量路径(代码走查记录)

## 6. 门禁与收尾

- [ ] 6.1 三道门禁:build/vet/test -race → Jaeger 后端集成抽查(span 树形态断言)→ CodeReview sub-agent fresh-eyes
- [ ] 6.2 delta specs 同步主 specs(turn-tracing、trajectory-trace-correlation 新增;trajectory-recording 主规格按 MODIFIED 全文拷贝规程合并)
- [ ] 6.3 commit + archive 本变更 + 回写 LEDGER.md 与 roadmap P1.5 检查点;spike-notes.md 随变更归档(探索证据留存)
