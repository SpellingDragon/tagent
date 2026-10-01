# event-sourced-projection Delta

## ADDED Requirements

### Requirement: 投影的通道分区与统一装配

SessionProjection SHALL 支持通道分区（对话/遥测/反思）：assembleRequest 仍为唯一装配源（不变量 1 不变），按 system + 反思综述 + 对话窗口 + 遥测卡片/看板 + 本轮新事件的次序统一装配。遥测分区的降级/退出操作 SHALL 仅作用于投影（「Compact 只改投影」红线延伸为「通道治理只改投影」）。召回暂存 ref 标记来源并受同一生命周期管理。

#### Scenario: 稳定前缀的字节稳定性

- **WHEN** 两轮装配之间遥测区发生降级、看板刷新
- **THEN** 对话区与反思区的渲染字节保持稳定（prefix-cache 命中不受通道治理波及）
