# bounded-trajectory-capture delta

## MODIFIED Requirements

### Requirement: 轨迹捕获常驻有界

TrajectoryCapture 的常驻快照集合 SHALL 受字节预算约束（缺省 64MB，机制常量，不在声明面）：超出预算时最旧快照 SHALL 换出落盘，seal/导出 SHALL 能按需读回且逐字段等价。seal 后整集 SHALL 释放。

#### Scenario: 预算下常驻不超界
- **WHEN** 累计快照字节超过预算（如 1MB 预算下 50 个 >50KB 快照）
- **THEN** 常驻集合字节 SHALL ≤ 预算，且全部快照可从换出存储读回

#### Scenario: 导出等价
- **WHEN** 训练导出经公共读面读取含换出快照的会话
- **THEN** 导出样本数与 decision 数一致，字段无损失
