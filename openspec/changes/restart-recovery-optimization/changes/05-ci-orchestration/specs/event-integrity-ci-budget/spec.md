# event-integrity-ci-budget Specification

## ADDED Requirements

### Requirement: event-integrity-ci-budget 契约

事件完整性长跑 SHALL 不以固定就绪时间窗判定成败，判定 SHALL 只依据最终数据准确性对账；分片并发度与超时 SHALL 由 03.10 校准导出；任何失败路径 SHALL 在 runner 超时前落具名证据；验收面 1200 输入/120 重启 MUST NOT 缩水。

#### Scenario: 主路径

- **WHEN** 某一代启动显著慢于同场其他代但数据正确
- **THEN** 不判失败，最终以数据准确性对账裁决

#### Scenario: 挂死留证

- **WHEN** 子进程无响应
- **THEN** 在具名 liveness 处失败并落 test-log/evidence，而非泛化超时
