# Orchestration 契约

## ADDED Requirements

### Requirement: 决策边界消费外部输入

在途 turn SHALL 在其下一 decision 边界聚合链上就绪的外部输入（source==user/渠道）并消费；SHALL NOT 排队至 turn 结束另起 turn；执行中的 decision SHALL NOT 被打断。

#### Scenario: 在途消费

- **WHEN** turn 进行中同一 session 到达一条用户消息
- **THEN** 该消息 SHALL 出现在同 turn 下一 decision 的输入

#### Scenario: 无在途起 turn

- **WHEN** 无在途 turn 时到达用户消息
- **THEN** 消息 SHALL 落链并起新 turn 消费

### Requirement: 冥想节奏门看 session 活跃

节奏门 SHALL 新增两侧判据：同 session 用户事件新鲜度（晚于 last fire 且不足 min_gap→不 fire，冷却自最后用户事件重置）与决策席在途（占用→不 fire，推迟非放弃）；自管谱系回合 SHALL NOT 重置冷却。

#### Scenario: 活跃抑制

- **WHEN** 同 session 有用户事件且距其不足 min_gap
- **THEN** 冥想 SHALL NOT fire

#### Scenario: 在途抑制

- **WHEN** 该 session 决策席被在途 turn 占用
- **THEN** 冥想 SHALL NOT fire，水位不动，下 tick 重评

### Requirement: 三段验收

孙任务 SHALL exit 0 且带正/负/消费者三锚；子域 Scenario 逐条映射测试；变更级 DoD SHALL 含症状锚红转绿与回滚即红双证据。

#### Scenario: 回滚即红

- **WHEN** 完整回滚本变更复跑症状锚
- **THEN** 两条直译测试 SHALL 红
