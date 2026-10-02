## MODIFIED Requirements

### Requirement: 无锚恢复不静默截断

无锚回放 MUST 分页扫描并先过滤非投影事件，有效投影事件全量复原、MUST NOT 施加数量截断上限（丢掉最旧有效事件属于数据丢失，不是内存治理）；中间内存 SHALL 有界（靠分页扫描约束，而非截断历史）。status=partial 与相应不完整计数（missing_keys、pages_failed、batch_errors、payload_errors）SHALL 仅源于扫描、载荷解析或水合的真实不完整，不源于数量护栏；partial/failed SHALL 进入 diagnostics 并可辨。被过滤的 task/receipt/快照记录 SHALL NOT 计入投影事件集。

#### Scenario: 超护栏长链冷启动全量复原

- **WHEN** 无 anchor 且有 600 条有效事件及 600 条非投影记录
- **THEN** 600 条有效事件全量复原（status=full，无截断计数），内部记录不挤掉有效上下文

#### Scenario: 不完整来源如实上报

- **WHEN** 无锚回放中 tail 分页部分失败或快照槽读失败
- **THEN** 结果 status=partial 且对应不完整计数非零并进入 diagnostics，不静默吞、不伪装 full
