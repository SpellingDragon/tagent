# recovery-timing-evidence Specification

## ADDED Requirements

### Requirement: recovery-timing-evidence 契约

恢复路径 SHALL 暴露五次 Scan 与 fold 的分段计数/耗时，且该观测 SHALL 写入 CI 可上传产物；测量锚 SHALL 在阶段 2 先行产出并留基线红证。

#### Scenario: 主路径

- **WHEN** 一次重启发生
- **THEN** 产物含 5 段 Scan 计数与 1 段 fold 计数，缺段或全零即红
