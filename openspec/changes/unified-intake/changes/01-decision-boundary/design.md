# 技术要点与文件清单

- 触碰集：`runtime/agent/loop.go`（Drive 每决策边界 observeReady 增量）、`runtime/agent/invocation.go`（ReadyFeed 面类型注释）、`assembly/run.go`（装配注入 ReadyFeed 实现：会话读 after 之后事实）、`assembly/host.go`（Invoke 受理语义+gate 语义）、`tests/integration/execution/unified_intake_test.go`（红锚+场景）、`openspec/specs/persistent-event-loop/spec.md`（delta 落地后的主规格 ADDED——经本 change delta）、`docs/wiki/agent/event-flow.md`（锚页随迁：e2e 序列+ready 语义）。
- 风险与回退：ReadyFeed nil=现行为（代码级并存）；gate 语义改动以既有 `ErrSlotHeld` 测试面兜底；回退=还原 Invoke 路径+Loop nil 面。
- 关键不变量：同 session 至多一个在途 turn；decision 输入=其时刻链上事实集（可重放）；受理回执="ok" 最小语义不变。
