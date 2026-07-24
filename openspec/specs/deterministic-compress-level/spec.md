# deterministic-compress-level Specification

## Purpose

基于事件类型和段年龄的确定性压缩分级规则，以纯函数替代 LLM 价值评估，为每个 TaskSegment 返回 0–3 的压缩级别，保证相同输入始终产出相同压缩决策。

## Requirements

### Requirement: 确定性压缩分级纯函数

系统 SHALL 提供纯函数 `deterministicLevel(seg, segIdx, totalSegs, keepRecent) int`，给定一个 TaskSegment 及其在时间序列中的位置，返回压缩级别 (0–3)，替代 LLM 价值评估。该函数 SHALL 为纯函数：无副作用、不调用 LLM 或任何外部服务、不读取 MemoryStore 或 Projection，对相同输入 MUST 始终返回相同输出，返回值 MUST 落在 [0,3] 范围内。

压缩级别定义：

| 级别 | 含义 | 保留内容 | 丢弃内容 |
|------|------|---------|---------|
| L0 | 完全保留 | 所有消息 | 无 |
| L1 | 选择性保留 | 用户消息 + 关键工具结果 | 非关键工具消息 |
| L2 | 摘要替代 | 用户消息 + LLM 摘要 | 所有执行细节 |
| L3 | 全段归档 | MemoryStore 归档 + 内联摘要 | 原始消息全部移除 |

分级规则（其中 `age = totalSegs - 1 - segIdx`，0 = 最新段；`keepRecent` 默认 2；`HasUserInput` = 段内含至少一条 RoleUser 消息）：

- `age < keepRecent` → L0（最近 N 段不压缩）
- `age < keepRecent*2` 且 `HasUserInput` → L1
- `age < keepRecent*3` 或 `HasUserInput` → L2
- 其余 → L3

执行约束：函数执行时间 SHALL < 1 微秒；SHALL NOT 调用 LLM/外部服务，SHALL NOT 读取 MemoryStore 或 Projection。

#### Scenario: 最近任务保留

- **GIVEN** keepRecent=2, totalSegs=5
- **WHEN** segIdx=3 (age=1, 倒数第 2 段)
- **THEN** 返回 0 (L0, 完全保留)

#### Scenario: 用户消息段选择性保留

- **GIVEN** keepRecent=2, totalSegs=5, seg.HasUserInput=true
- **WHEN** segIdx=2 (age=2)
- **THEN** 返回 1 (L1, 选择性保留)

#### Scenario: 老段全归档

- **GIVEN** keepRecent=2, totalSegs=10, seg.HasUserInput=false
- **WHEN** segIdx=0 (age=9)
- **THEN** 返回 3 (L3, 全段归档)

#### Scenario: 中等年龄摘要替代

- **GIVEN** keepRecent=2, totalSegs=8, seg.HasUserInput=true
- **WHEN** segIdx=1 (age=6)
- **THEN** 返回 2 (L2, 摘要替代)
