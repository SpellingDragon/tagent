# task-skeleton-compression Specification

## Purpose

以 `agent_output` 为段边界、按 `tool > assistant` 优先级丢弃中间事件、保留 `external_input + agent_output` 任务骨架，并在骨架仍超预算时触发多段合并压缩（rolling summary 归档出口）的压缩能力。段 = 一次完整任务回合，骨架 = 用户原话与最终结论。

## Requirements

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

当骨架（L2 仅保留边界事件）仍超预算时，SHALL 触发多段压缩归档（L3）：最老段整段离场并由 `buildRetainedRefs` 折叠进滚动摘要。L3 段摘要（legacy 路径）SHALL 优先复用 `segmentContentHash` 归档缓存，缓存未命中且配置了 summary 模型时调用模型生成，模型缺失时标记 `level3Failed` 并回退为纯骨架折叠（不视为错误降级）。

#### Scenario: L3 优先归档缓存，模型缺失回退骨架

- **GIVEN** 一个 L3 段，归档缓存未命中且无 summary 模型
- **WHEN** 执行多段压缩
- **THEN** SHALL 以纯骨架方式折叠该段进滚动摘要，标记 `level3Failed`，SHALL NOT 报错或中断压缩

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

### Requirement: 压缩触发器多维化（token 阈值或完整段超龄）

`ContextCompressor.Compress` SHALL 在 token 阈值之外，增加**完整任务段超龄**作为独立触发维度：当 refs 中 `agent_output` 段边界计数（完整任务回合数）超过 `keepRecent` 时，即使 `usedTokens <= threshold` SHALL 调用 `SmartCompressor.Compress`。完整段计数 SHALL 复用现有的 ref 遍历统计（零额外扫描成本），SHALL NOT 通过每轮全量分段来获得。稳态收敛到 ~3×keepRecent 个 retained agent_output（L0/L1/L2 段都保留边界事件），长会话里触发持续——LSM 式连续维护滚动摘要的设计意图。

#### Scenario: 完整段超龄时即使 under budget 也触发

- **GIVEN** 会话有 5 个完整任务回合（keepRecent=2），`usedTokens` 低于阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL 调用 `SmartCompressor.Compress`（此前因 under budget 直接返回，压缩从未运行）
- **AND** 老回合 SHALL 按段龄被 L1 丢弃 tool 事件、L3 折叠进滚动摘要

#### Scenario: 完整段未超龄时短路返回

- **GIVEN** 会话仅有 1 个完整任务回合，且 `usedTokens` 低于阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL NOT 调用 `SmartCompressor.Compress`，直接返回原始 refs

#### Scenario: token 阈值触发保持有效（回归）

- **GIVEN** `usedTokens` 超过阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL 调用 `SmartCompressor.Compress`（与既有行为一致）

### Requirement: LLM 文摘作为卡片之上的可选叠加层

骨架压缩（`compressSkeleton`）是纯工程、零 LLM：L3 = 整段离场→`buildRetainedRefs` 折叠成滚动摘要卡片，**不做 LLM 段摘要**。骨架管线的 LLM 文摘是 `condenseCardLines`（`curateCards` 内）：卡片超 `cardMaxChars` 时 SHALL 用 summary 模型浓缩较旧一半卡片、保留最新卡片原文；无模型时 SHALL 将最旧行沉底为计数（不报错）。LLM 段摘要 + `segmentContentHash` 归档缓存（同内容复用、落库 `context_compress_summary` 固化物、TTL 豁免）是 `compressLegacy`（`skeleton_segmentation:false` 回退路径）独有。所有 LLM 生成的文摘/浓缩内容 SHALL 保留 `[evt_key]` 召回票据，使卡片始终是召回锚点。

#### Scenario: condenseCardLines 浓缩旧卡且保留票据

- **GIVEN** 滚动摘要卡片超过 `cardMaxChars` 且配置了 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 浓缩较旧一半卡片、保留最新卡片原文与 `[evt_key]` 票据

#### Scenario: 无模型时沉底计数不报错

- **GIVEN** 卡片超 `cardMaxChars` 但无 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 将最旧行沉底为 `(earlier n items)` 计数，SHALL NOT 报错

#### Scenario: legacy L3 摘要经归档缓存复用（素材律）

- **GIVEN** `skeleton_segmentation:false` 回退路径下，一个 L3 段内容未变
- **WHEN** 再次执行压缩
- **THEN** SHALL 复用 `segmentContentHash` 归档缓存的摘要，SHALL NOT 重复调用 summary 模型

### Requirement: 滚动摘要消息豁免 L3 压缩（常驻可见）

骨架压缩 SHALL 把领先的 `context_compress` 滚动摘要消息从段结构中摘出（类比 `SplitSystemMessage`），使其不参与分段与定级，并在压缩后无条件回填到结果最前（紧随系统消息）。由此滚动摘要消息 SHALL 在任何段龄/级别下都保留在模型上下文中，SHALL NOT 被 L3 整段丢弃。

#### Scenario: K≥7 段0 升 L3 时摘要仍可见

- **GIVEN** 投影含滚动摘要 + 8 个完整任务回合（段0 段龄达 L3 阈值）
- **WHEN** 执行骨架压缩
- **THEN** `result.Messages` SHALL 仍含 `context_compress` 滚动摘要消息（位于系统消息之后）
- **AND** 段0 SHALL NOT 包含该滚动摘要消息（它已被摘出，不参与定级）

#### Scenario: 摘要 ref 仍被投影携带

- **GIVEN** 骨架压缩保护了滚动摘要消息
- **WHEN** `buildRetainedRefs` 构建 RetainedRefs
- **THEN** SHALL 仍吸收并重建滚动摘要 ref（负 key），投影照常携带到下一轮

### Requirement: 段定级采用指数段龄边界

`deterministicLevel` 的段龄阈值 SHALL 为指数边界 `keepRecent × 2^level`（即 L0 < k、L1 < 2k、L2 < 4k、L3 ≥ 4k），而非线性 `{k, 2k, 3k}`，以使段在每个级别驻留更久、被折叠进滚动摘要的频率降低（前缀缓存更稳定）。指数底数 SHALL 固定为 2。

#### Scenario: 指数边界定级

- **GIVEN** keepRecent=2
- **WHEN** 计算段龄 age 的级别
- **THEN** age=3 SHALL 为 L1、age=5 SHALL 为 L2、age=7 SHALL 为 L2、age=8 SHALL 为 L3（线性下 age=5/6 已为 L3）

### Requirement: 压缩配置参数公式化默认值

压缩相关配置 SHALL 以 `max_tokens`（M）与 `keep_recent_tasks`（k）为主变量，未显式设置时其余参数按简单公式派生默认值：token 阈值 = `compress_threshold × M`；`recent_full_count` = `k × 每轮引用数`；`card_max_chars` = `M / 20`；`compact_keys_listed` = `card_max_chars / 200`。显式设置的值 SHALL 优先于公式默认值（向后兼容）。

#### Scenario: 未设置时按公式派生

- **GIVEN** M=128000、k=2，且 card_max_chars / compact_keys_listed 未显式设置
- **WHEN** 初始化压缩器
- **THEN** card_max_chars SHALL 默认 6400（M/20）、compact_keys_listed SHALL 默认 32（6400/200）

#### Scenario: 显式设置优先

- **GIVEN** 显式设置 card_max_chars=6000
- **WHEN** 初始化压缩器
- **THEN** SHALL 使用 6000 而非公式默认值
