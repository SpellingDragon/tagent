## ADDED Requirements

### Requirement: 提交点记录即消费值

org 热更在提交点把某 agent 的热参记为 `applied` 时，该 agent 的消费侧（压缩预算/keepRecent 等热参读路径）SHALL 解析到同一值；SHALL NOT 存在第二份可被独立写入而未随提交点刷新的热参权威。构造期值 SHALL 仅作为「源缺席」时的兜底，一旦源提供值，读路径 SHALL 以源为准。同一进程内重复执行（含多轮换代与多次 numeric-only）SHALL 保持该不变量。

#### Scenario: applied 即可被消费读到

- **WHEN** 一次 numeric-only 发布把某 agent 的 keepRecent 从 2 改为 7 并记为 applied
- **THEN** 该 agent 的 `OrgKeepRecent()` 与压缩动作解析到的值均为 7，无一侧仍为 2

#### Scenario: 重复执行下不变量仍成立

- **WHEN** 同一进程内该场景连续执行 12 轮（每轮含一次结构发布与一次 numeric-only）
- **THEN** 每一轮的 applied 值与消费读值都一致，无轮次退化到构造值
