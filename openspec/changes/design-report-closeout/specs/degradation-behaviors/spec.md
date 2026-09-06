# degradation-behaviors 规格增量

## ADDED Requirements

### Requirement: 依赖退化行为响应（警告级、默认关）
系统 MUST 提供三项独立可配置行为，仅在 DegradationManager 启用时生效，每项默认关闭（零行为变化）：model 退化→runEventLoop turn 间退避（`degradation_model_backoff`，默认 0 关）；mcp 退化→mcp_call 熔断快失败（返回含自纠材料的 result）+ 半开探测（`degradation_mcp_probe_every`）；disk 退化→TaskManager 拒绝新 spawn（进行中任务不受影响）。恢复即自动回正常路径。

#### Scenario: model 退化退避
- **WHEN** DepModel degraded 且配置退避 5s
- **THEN** 下一 turn 启动前等待 5s；上报恢复后不再等待

#### Scenario: mcp 熔断半开
- **WHEN** DepMCP degraded
- **THEN** mcp_call 前 4 次直接返回熔断 result，第 5 次放行探测；探测成功触发恢复上报

#### Scenario: disk 禁 spawn
- **WHEN** DepDisk degraded
- **THEN** 新 Spawn 被拒并返回可读原因；已 spawn 任务的 settle/轮询不受影响
