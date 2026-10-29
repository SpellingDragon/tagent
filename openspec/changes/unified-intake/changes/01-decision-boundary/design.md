# 技术要点与文件清单

- 触碰集：`runtime/agent/loop.go`（Drive 每决策边界重聚合+退出判据扩一项）、`assembly/facts.go`（界标不变量校准）、`assembly/host.go`（Invoke 在途受理+get 段在途 inv 记录）、`assembly/host_face.go`（inject 段 KindUnknown 判读）、`examples/wechat-bot/wechat.go`（Drive 段同判读+mutex 降级）、`tests/integration/execution/unified_intake_test.go`（红锚+场景）、`openspec/changes/.../specs/persistent-event-loop/spec.md`（delta）、`docs/wiki/agent/event-flow.md`（锚页随迁）。
- 风险与回退：无新增面（零新概念），回退=还原 Invoke 门内时序+退出判据一项；既有 `ErrSlotHeld`/`ErrNoticeTurnIncomplete` 测试面兜底。
- 关键不变量：同 session 至多一个在途 turn；decision 输入=其时刻链上事实集（可重放）；受理回执="ok" 最小语义不变。
