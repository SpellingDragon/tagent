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

### Requirement: 折叠清理投影原文副本

AgeFold 折叠重写投影时 SHALL 同步删除 `originals/seen/consumed` 中被折叠条目的键。`Run.facts` 内存面 SHALL 以投影为界：带正回放窗的会话链 MUST 释放投影已不再呈现的原文副本，保留投影仍呈现的原文、进行中段与 pinned 事实；不带回放窗的会话链（不折叠）MUST 按机制常量 `noWindowResidentFacts` 兜底保留最近条目。释放的副本 SHALL 仍可按身份从持久链读回，重启恢复路径不变。

#### Scenario: 折叠后无悬挂原文
- **WHEN** 一次折叠覆盖 N 个条目
- **THEN** originals/seen/consumed 中被覆盖键全部消失，链上事实与投影呈现不回归

#### Scenario: 常驻随投影而非历史
- **WHEN** 一条带窗会话链折过多次，投影只剩最近若干段与进行中段的原文
- **THEN** `Run.facts` 只保留这些原文与 pinned 事实，更早的会话事实可按身份从持久链读回
