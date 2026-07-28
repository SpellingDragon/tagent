# deterministic-compress-level Delta

## MODIFIED Requirements

### Requirement: 确定性压缩分级纯函数

系统 SHALL 提供纯函数 `deterministicLevel(seg, segIdx, totalSegs, keepRecent) int`，给定一个任务回合段（以 `agent_output` 为边界，见 `task-skeleton-compression`）及其在新→旧序列中的位置，返回压缩级别 (0–3)，替代 LLM 价值评估。该函数 SHALL 为纯函数：无副作用、不调用 LLM 或任何外部服务、不读取 MemoryStore 或 Projection，对相同输入 MUST 始终返回相同输出，返回值 MUST 落在 [0,3] 范围内。

段边界由 `agent_output` 界定：段 = `[external_input, (thinking_plan|action_command)*, agent_output]`。判定基于**事件类型**而非消息内容，`HasUserInput` 判据废弃。

压缩级别定义：

| 级别 | 含义 | 段内保留 | 段内丢弃 |
|------|------|---------|---------|
| L0 | 完全保留 | 所有消息 | 无 |
| L1 | 丢 tool | 骨架 + `thinking_plan` | `action_command` |
| L2 | 仅骨架 | `external_input` + `agent_output` | `action_command` + `thinking_plan` |
| L3 | 多段压缩归档 | （整段移出时间线） | 全段 → rolling summary |

分级规则（其中 `age = totalSegs - 1 - segIdx`，0 = 最新段；`keepRecent` 默认 2）：

- 进行中段（`IsComplete=false`）→ 恒 L0（pending 输入永不压缩）
- `age < keepRecent` → L0（最近 N 回合不压缩）
- `age < keepRecent*2` → L1（丢 `action_command`，留 `thinking_plan`）
- `age < keepRecent*3` → L2（仅留骨架 `external_input` + `agent_output`）
- 其余 → L3（多段压缩归档，`external_input`/`agent_output` 汇入 rolling summary）

与旧规则的关键差异：旧规则以 user 切分碎片段、以 `HasUserInput` 为核心判据，导致 `age < keepRecent*3 或 HasUserInput → L2` 恒真、L3 不可达（归档死锁）。新规则以 `agent_output` 界定完整回合、纯 age 驱动，使 L3 归档真实可达，`external_input` 获得归档出口。

执行约束：函数执行时间 SHALL < 1 微秒；SHALL NOT 调用 LLM/外部服务，SHALL NOT 读取 MemoryStore 或 Projection。

#### Scenario: 进行中段恒为 L0

- **GIVEN** keepRecent=2, totalSegs=5, seg.IsComplete=false
- **WHEN** 对任意 segIdx 求级
- **THEN** 返回 0 (L0, 进行中段完整保留)

#### Scenario: 最近回合保留

- **GIVEN** keepRecent=2, totalSegs=5, seg.IsComplete=true
- **WHEN** segIdx=3 (age=1)
- **THEN** 返回 0 (L0, 完全保留)

#### Scenario: 中段丢 tool

- **GIVEN** keepRecent=2, totalSegs=6, seg.IsComplete=true
- **WHEN** segIdx=3 (age=2, 满足 age<keepRecent*2=4)
- **THEN** 返回 1 (L1, 丢 action_command 留 thinking_plan)

#### Scenario: 老段仅留骨架

- **GIVEN** keepRecent=2, totalSegs=8, seg.IsComplete=true
- **WHEN** segIdx=2 (age=5, 满足 age<keepRecent*3=6 但不满足 age<keepRecent*2)
- **THEN** 返回 2 (L2, 仅留 external_input + agent_output 骨架)

#### Scenario: 更老段多段压缩归档

- **GIVEN** keepRecent=2, totalSegs=10, seg.IsComplete=true
- **WHEN** segIdx=0 (age=9, 不满足 age<keepRecent*3)
- **THEN** 返回 3 (L3, 多段压缩归档，骨架汇入 rolling summary)
