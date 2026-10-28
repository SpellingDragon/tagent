## MODIFIED Requirements

### Requirement: noop 降级零影响
观测组件 SHALL 由装配注入；未配置导出时全部 span/metric 操作 MUST 为 noop，零导出、零可观测开销，事件循环、工具执行与采集行为 MUST 与无观测时一致。注入 MUST NOT 修改进程级默认 logger、TracerProvider 或 MeterProvider。**关闭 OTel MUST NOT 减少 Query 的可用维度或语义。**

#### Scenario: 两个运行时互不污染
- **WHEN** 同进程装配两个使用不同观测实例的运行时
- **THEN** 各自 span 与指标 MUST 只进入各自实例
- **AND** MUST NOT 覆盖进程级默认

#### Scenario: 关闭导出仍可查询
- **WHEN** 未配置 OTLP 端点且执行 Query
- **THEN** 只读运行总图、快照、分页与订阅 MUST 均可用
- **AND** 结果 MUST 与开启时相同
