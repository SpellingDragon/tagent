# task-skeleton-compression（段闭合判据）

## MODIFIED Requirements

### Requirement: 段按实际消费关系闭合（取代「以 agent_output 为段边界」）

任务回合段 SHALL 按「实际消费关系」闭合：一次 `agent_output` 只闭合它实际携带过的输入（依据该事实 Meta 的输入键集合），未被任何输出携带的输入 SHALL 留在进行中段。

段的骨架仍为输入、过程与结论；变化仅在「哪些输入被这次结论闭合」。

#### Scenario: 未消费输入不被闭合
- **WHEN** 链序为 `A → B → 对 A 的回答`，且对 A 的回答未携带 B
- **THEN** 闭合段 SHALL 只含 A；B SHALL 留在进行中段

#### Scenario: 已消费输入闭合
- **WHEN** 一次回答携带了 A
- **THEN** A SHALL 被该回答闭合为一段

#### Scenario: 工具结果不开启新段
- **WHEN** 段内出现工具结果（`action_command`）
- **THEN** 它 SHALL 作为过程材料留在所在段，MUST NOT 开启新段

## ADDED Requirements

### Requirement: 段可从链重建

段判据 SHALL 是链上事实与输出 Meta 的纯函数；重启后同一链 SHALL 重建相同段，MUST NOT 依赖额外持久结构。

#### Scenario: 重启同段
- **WHEN** 进程重启并完成 `seedReplay`
- **THEN** 同一会话的段划分 SHALL 与重启前一致

#### Scenario: 旧链兼容
- **WHEN** `agent_output` 缺消费键 Meta（历史数据）
- **THEN** 段 SHALL 按链序判据解释，并在诊断面标记 legacy；新写入 MUST NOT 缺该 Meta
