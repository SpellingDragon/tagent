# recall-protocol Delta

## ADDED Requirements

### Requirement: memory_turn 因果链召回工具

系统 SHALL 提供 `memory_turn` 召回工具：给定一个 EventKey（通常为 `agent_output` 卡片），沿 `GetParent` 因果链逐跳回走，直到遇到 `external_input`（含）为止，返回该区间内的全部事件（含被骨架压缩丢弃的 `thinking_plan` / `action_command`），按时间排序。工具 SHALL 以 `external_input` 事件类型作为回走停止条件，由此圈定"当前任务回合"而无需正向遍历。

#### Scenario: 锚 agent_output 回走重建整轮

- **GIVEN** 一个已完成的回合，其 `thinking_plan` / `action_command` 已被骨架压缩丢弃出时间线
- **WHEN** 调用 `memory_turn(key=<agent_output 的 key>)`
- **THEN** SHALL 返回 `[external_input, thinking_plan, action_command, ..., agent_output]` 整轮事件
- **AND** 在到达 `external_input` 后停止回走

#### Scenario: 丢弃的执行过程可被召回

- **WHEN** 模型需要了解某轮的具体工具执行过程
- **THEN** SHALL 经 `memory_turn` 用边界卡片上的 `agent_output` key 一次性召回被丢弃的 tool 步骤

### Requirement: 卡片行标注可追溯提示

`buildRetainedRefs` 压缩时 SHALL 统计每轮被丢弃的 tool 步数，并在 L3（整段离场）回合的 agent_output 卡片行标注"含 N 步工具调用，可用 memory_turn 追溯"的提示，使模型知晓该回合存在可召回的执行过程及其召回入口（agent_output 卡片 key）。

#### Scenario: 含 tool 步骤的回合卡片带追溯提示

- **GIVEN** 一个含 3 步工具调用的回合被压缩成边界卡片
- **WHEN** 渲染该卡片行
- **THEN** SHALL 含"含 3 步工具调用，可用 memory_turn 追溯"或等效提示
