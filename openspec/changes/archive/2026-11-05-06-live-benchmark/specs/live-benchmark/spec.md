# live-benchmark Specification（delta）

## ADDED Requirements

### Requirement: 三方对照可判定

对照 harness MUST 以同一任务集、同一模型、等价工具面驱动最小基线、tagent 与 DeepSeek Harness 三方；**等价工具面=能力映射**：每个任务声明的能力 MUST 被三方各自工具面覆盖（映射表登记为证），MUST NOT 以"大体相同"代替可判定的能力覆盖；每个任务 MUST 携带机读验收器，成功判定 MUST NOT 依赖人工目测。五指标 MUST 按同一 schema 落盘（**人工介入次数=审批入口调用数＋blocked 上报数，含 `breakdown` 分项（`approval_calls`/`blocked_reports`）且分项和=总**），且**送达率口径统一为"发送方渠道确认"**：tagent=桥接内渠道 Send 回执，DSH=发送 API 成功，最小基线=任务终态（无渠道）并在报告中注明口径差，MUST NOT 把三种口径当同一指标比较。run 记录 MUST 含 `status` 字段（completed/skipped/blocked）。

#### Scenario: 同任务集三方执行

- **WHEN** 对照 run 启动
- **THEN** 三方跑同一 12 任务集，五指标（成功率/送达率/延迟/token 成本/人工介入）按同一 schema 落盘

### Requirement: 真实资源三态门

真实 LLM 依赖的处置 SHALL 分两层：**run 级（运行级）**三态——无凭据单次 run MUST 记 Skip（不阻断开发与 harness 交付）、有凭据缺配 MUST Fatal 上报、跑通 MUST 附非空转证明（真实调用计数）；**change 级（变更级）** DoD——收尾时仍无凭据 MUST 转 blocked 法定停点上报用户、MUST NOT 结项（run 级 Skip 与 change 级 blocked 两层不混用）。

#### Scenario: 无凭据

- **WHEN** ZAI_API_KEY 未配置
- **THEN** 真实 run Skip 并记账，scripted 自测与 harness 开发不受阻
