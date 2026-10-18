# degradation-behaviors Specification

## Purpose

依赖退化行为响应能力:DegradationManager 启用时对 model/mcp/disk 三类依赖退化提供独立可配置的警告级行为响应(turn 间退避、mcp_call 熔断快失败加半开探测、disk 退化拒绝新 spawn),每项默认关闭(零行为变化),依赖恢复即自动回正常路径。

## Requirements

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

### Requirement: 退化状态与阻塞可解释

退化行为（model 退避 / mcp 熔断半开 / disk 禁 spawn）SHALL 在 Query 读面以具名状态可查询；退化造成的拒收 SHALL 产生可查询记录，并给出**下一推进者**（恢复探测/退避结束等）；每项退化默认关闭（零行为变化）。

#### Scenario: 退器具名可查

- **WHEN** DepMCP degraded 且 mcp_call 被熔断快失败
- **THEN** Query 可读到该退化状态与熔断拒收记录
- **AND** 给出下一推进者（半开探测）

#### Scenario: 恢复自动回正常（重启）

- **WHEN** 依赖恢复上报
- **THEN** 退化状态清除，Query 反映正常路径
- **AND** 不残留拒收记录为当前状态
