# task-skeleton-compression Specification

## Purpose

以 `agent_output` 为段边界、按 `tool > assistant` 优先级丢弃中间事件、保留 `external_input + agent_output` 任务骨架，并在骨架仍超预算时触发多段合并压缩（rolling summary 归档出口）的压缩能力。段 = 一次完整任务回合，骨架 = 用户原话与最终结论。

## ADDED Requirements

### Requirement: 以 agent_output 为段边界切分任务回合

系统 SHALL 以 `agent_output` 事件为段闭合边界，将历史时间线切分为任务回合段：一个段 SHALL 包含 `[external_input, (thinking_plan|action_command)*, agent_output]`。`agent_output` 的识别 SHALL 优先依据消息的 event 类型前缀（`[evt_KEY|agent_output]`），对缺失前缀的输入 SHALL 退回启发式（`assistant` 且无 `tool_calls` 视为回合收尾）。

#### Scenario: 完整回合闭合成一个段

- **GIVEN** 消息序列 `[external_input(A), thinking_plan, action_command, agent_output(A), external_input(B), agent_output(B)]`
- **WHEN** 执行切段
- **THEN** 产出 2 个段：段1=`[external_input(A), thinking_plan, action_command, agent_output(A)]`，段2=`[external_input(B), agent_output(B)]`
- **AND** 两个段均为 `IsComplete=true`

#### Scenario: 连续 external_input 归入同一进行中段

- **GIVEN** 消息序列 `[external_input(A), agent_output(A), external_input(B), external_input(C)]`（用户连发 B、C，agent 未回）
- **WHEN** 执行切段
- **THEN** 段2 包含 `[external_input(B), external_input(C)]`
- **AND** 段2 为 `IsComplete=false`

#### Scenario: 无 agent_output 的尾部为进行中段

- **GIVEN** 消息序列 `[external_input(A), agent_output(A), external_input(B), thinking_plan, action_command]`
- **WHEN** 执行切段
- **THEN** 最后一段 `[external_input(B), thinking_plan, action_command]` 为 `IsComplete=false`

### Requirement: 段内事件按事件类型二分骨架与中间事件

系统 SHALL 将段内事件二分为骨架与中间事件，判定 SHALL 为事件类型的纯函数，SHALL NOT 读取消息内容。骨架为 `external_input` 与 `agent_output`；中间事件为 `action_command` 与 `thinking_plan`。

#### Scenario: 骨架事件识别

- **WHEN** 段内一条消息的事件类型为 `external_input` 或 `agent_output`
- **THEN** 该消息 SHALL 被归类为骨架（保留候选）

#### Scenario: 中间事件识别

- **WHEN** 段内一条消息的事件类型为 `action_command` 或 `thinking_plan`
- **THEN** 该消息 SHALL 被归类为中间事件（可丢弃候选）

### Requirement: 中间事件按 tool > assistant 优先级丢弃

系统在段内压缩中间事件时 SHALL 按 `action_command`（tool）优先于 `thinking_plan`（assistant）的顺序丢弃：先丢弃全部 `action_command`，仍需进一步压缩时才丢弃 `thinking_plan`。骨架事件（`external_input`、`agent_output`）在该级别下 SHALL 保留。

#### Scenario: 第一档丢弃 tool 保留 assistant

- **GIVEN** 段 `[external_input, thinking_plan, action_command, agent_output]` 被定为"丢弃 tool"档
- **WHEN** 执行段内压缩
- **THEN** 保留 `[external_input, thinking_plan, agent_output]`
- **AND** 仅 `action_command` 被丢弃

#### Scenario: 第二档丢弃 tool 与 assistant 仅留骨架

- **GIVEN** 段 `[external_input, thinking_plan, action_command, agent_output]` 被定为"仅留骨架"档
- **WHEN** 执行段内压缩
- **THEN** 保留 `[external_input, agent_output]`
- **AND** `action_command` 与 `thinking_plan` 均被丢弃

### Requirement: 骨架仍超预算时触发多段压缩归档

当所有可压缩段均已压至骨架（仅剩 `external_input` + `agent_output`）仍超出 token 预算时，系统 SHALL 将更老的任务回合段整段移出时间线，并将其 `external_input` 与 `agent_output` 事件汇入 rolling summary（经 `buildRetainedRefs` / `extractCardLine` 生成任务卡片）。多段压缩 SHALL 在无摘要模型时以 engineering 卡片兜底走通，不得依赖 LLM。

#### Scenario: 老骨架段汇入 rolling summary

- **GIVEN** 全部段已压至骨架后仍超预算，且存在 `age >= keepRecent*3` 的老回合段
- **WHEN** 执行多段压缩
- **THEN** 这些老段 SHALL 从输出时间线移除
- **AND** 其 `external_input`/`agent_output` 事件以卡片形式进入 rolling summary，recall 可溯源

#### Scenario: 无摘要模型时走 engineering 卡片

- **GIVEN** 未配置摘要模型（`summaryModel=nil`）
- **WHEN** 触发多段压缩
- **THEN** 系统 SHALL 以 `extractCardLine` 生成的卡片完成归档，不因缺少 LLM 而失败或降级

#### Scenario: external_input 获得归档出口

- **GIVEN** 一段已完成的 `external_input` 经历足够多轮压缩
- **WHEN** 该段进入多段压缩
- **THEN** 其 `external_input` ref SHALL 被收编进 rolling summary，不再作为独立段留存于 projection（段数随之下探）

### Requirement: 进行中段永不归档且始终保留

未完成（`IsComplete=false`）的进行中段 SHALL 不参与任何级别的压缩，其全部消息（含 `external_input` pending 输入）SHALL 完整保留，以驱动 LLM 的当前回合。

#### Scenario: 进行中段完整保留

- **GIVEN** 历史中存在一个 `IsComplete=false` 的进行中段
- **WHEN** 执行任意级别压缩
- **THEN** 该进行中段的全部消息 SHALL 原样保留，不被丢弃、归档或摘要

### Requirement: 保留消息携带 event key 前缀以衔接 rolling summary

压缩输出中所有被保留的消息 SHALL 保留其原 event key 前缀（`[evt_KEY|type]`），使 `buildRetainedRefs` 能据此判定存活 ref：存活 ref 留存 projection，未存活 ref（被丢弃的中间事件与被多段压缩的整段）汇入 rolling summary。

#### Scenario: 存活判定基于 event key

- **GIVEN** 压缩后某条骨架消息仍携带 `[evt_KEY|agent_output]` 前缀
- **WHEN** `buildRetainedRefs` 处理压缩结果
- **THEN** 该 `KEY` 对应的 ref SHALL 被保留在 projection
- **AND** 未出现在压缩结果中的 ref SHALL 被收编进 rolling summary
