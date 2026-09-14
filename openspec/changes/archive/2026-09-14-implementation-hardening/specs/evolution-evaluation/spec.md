## ADDED Requirements

### Requirement: guardrail 信号可用性显式声明

MetricGuardrail 的 DenialCount/CriticalCount 判据依赖治理事件流：治理未启用时该两判据 SHALL 在评估输出（evaluation 事件与日志）中显式声明不可用（如附 `governance disabled: denial/critical signals unavailable`），MUST NOT 以静默零值冒充「无劣化信号」；此耦合 SHALL 同步写入相关 wiki/spec 文档。

#### Scenario: 治理关闭下的后验评估

- **WHEN** 治理闸未启用且一次 self-improve 注册进入后验评估窗口
- **THEN** evaluation 输出包含治理信号不可用的显式标注，判读者不会将零拒绝率误读为健康证据
