# compression-recovery-ticket Specification

## ADDED Requirements

### Requirement: compression-recovery-ticket 契约

恢复态 SHALL 由链上票据事实按内容寻址引用外部只读 blob；票据 SHALL 带完整性信息与游标；MUST NOT 以链外独立状态作为可写第二真相；任何票据/blob 缺失、截断、校验失败或版本不认识 SHALL 退回全量事件流且结果不丢义务；全量重放与 dump+增量 SHALL 逐位一致。

#### Scenario: 主路径

- **WHEN** 恢复时票据存在且校验通过
- **THEN** 加载 blob 并补放其后增量，结果与全量重放逐位一致

#### Scenario: 校验失败退回全量

- **WHEN** blob 被截断或指纹不符
- **THEN** 读失败并入日志，改走全量事件流，义务不丢
