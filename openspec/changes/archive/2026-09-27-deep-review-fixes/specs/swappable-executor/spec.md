# swappable-executor Delta

## ADDED Requirements

### Requirement: SwappableModel 迭代路径与 channel 路径错误面一致

SwappableModel 的 `GenerateContentIter`（IterModel 委托直通与 channel 桥接两分支）在内部调用返回错误时 SHALL 记录错误日志，MUST NOT 无声吞掉使传输层错误对上层表现为零响应的「空流成功」——两条路径的错误可观测性 MUST 一致。

#### Scenario: 内层 IterModel 调用失败

- **WHEN** 被包装模型实现 IterModel 且其 GenerateContentIter 返回非 nil error
- **THEN** 日志中出现含错误详情的告警（与 channel 形态 GenerateContent 返回错误的可观测性对齐），迭代序列不产出伪造响应

#### Scenario: 内层 channel 形态调用失败

- **WHEN** 被包装模型仅实现 channel 形态且其 GenerateContent 返回错误或 nil channel
- **THEN** 同样记录错误日志，不静默返回空序列
