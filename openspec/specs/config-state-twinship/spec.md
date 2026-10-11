# config-state-twinship Specification

## Purpose
TBD - created by archiving change config-state-twinship. Update Purpose after archive.
## Requirements
### Requirement: 生效 system prompt 由 state 持有

AgentState SHALL 保存 agent 当前生效的 system prompt（由 resolve 在 Apply 时写入，nil resolve 取声明原文），模型请求读取该保存值；JSON 快照保存并恢复该值。

#### Scenario: 声明变更经 Reload 落入 state
- **WHEN** 配置含 skill/prompt 且 ReloadConfig 提交
- **THEN** owner.SystemPrompt() 等于组装结果，模型轨迹中的 SystemPrompt 与之相同；未 Reload 时仅改 registry 内容不改变请求所用 prompt

#### Scenario: 快照恢复带出生效值
- **WHEN** 保存快照后另一进程 LoadState
- **THEN** 恢复的 SystemPrompt 为快照时的生效值，无需 registry 即可读取

### Requirement: config→plan→govern→state 管道

配置变更 SHALL 先构建 StatePlan（声明面 Change、Adds/Updates/Removes、下一版生效 prompt），再依序经 Eval→Benchmark→ApplyGate，全过才提交 state；任一段拒绝则 state 保持旧态。nil 治理段恒真。

#### Scenario: 恒真管道
- **WHEN** 以空 Governance 提交合法变更
- **THEN** 三段视为通过，state 按计划更新

#### Scenario: 任一段拒绝
- **WHEN** Eval/Benchmark/ApplyGate 任一返回错误
- **THEN** 定义、生效 prompt 与现场不变，回收不发生，错误上抛

#### Scenario: 段序与计划内容
- **WHEN** 注入记录顺序的治理段
- **THEN** 调用顺序为 Eval→Benchmark→ApplyGate，三段收到同一计划的 Adds/Updates/Removes/Prompts

