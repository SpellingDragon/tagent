## ADDED Requirements

### Requirement: 观测组件实例注入
日志、指标与链路 MUST 由装配注入实例，MUST NOT 修改进程级默认 logger、TracerProvider 或 MeterProvider。

#### Scenario: 两个实例互不污染
- **WHEN** 同一进程装配两个使用不同观测实例的运行时
- **THEN** 各自的日志与指标 MUST 只进入各自实例
- **AND** MUST NOT 覆盖进程级默认

### Requirement: 关键运行指标
系统 MUST 从正式入口暴露请求终态计数、阶段耗时、token 用量、未结算义务数量与年龄、采集损失、订阅丢弃。指标标签 MUST 限于有限枚举集合。

#### Scenario: 高基数身份不进标签
- **WHEN** 记录一次带会话、事件与调用身份的请求
- **THEN** 指标 MUST 只使用有限枚举标签
- **AND** MUST NOT 以会话 ID、事件 key 或调用 ID 作为标签

#### Scenario: 缺失用量不记为零
- **WHEN** provider 未返回用量
- **THEN** 指标 MUST 记录为未知
- **AND** MUST NOT 以零值计入用量统计

### Requirement: 观测关闭不影响本地可查
未配置 OTLP 导出时，结构化日志与运行查询 MUST 保持可用且语义不变。

#### Scenario: 关闭导出仍可诊断
- **WHEN** 未配置 OTLP 端点并完成一次失败调用
- **THEN** 结构化日志 MUST 含该次失败的可关联身份与原因
- **AND** 运行查询 MUST 返回该次调用的终态
