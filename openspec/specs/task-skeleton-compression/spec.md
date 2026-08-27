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

当骨架（L2 仅保留边界事件）仍超预算时，SHALL 触发多段压缩归档（L3）：最老段整段离场并由 `buildRetainedRefs` 折叠进滚动摘要。L3 SHALL 仅由预算升级抵达——基础定级封顶 L2（见"段定级采用指数段龄边界"需求），段龄/段数 SHALL NOT 触发归档。L3 折叠为**双层结构**：

- **工程票据层（恒在，零 LLM）**：每个离场骨架事件折叠为一行卡片（`- [time] [evt_key] 骨架摘要`），`[evt_key]` 为召回票据；
- **LLM 滚动综述层（可选，`summary_model` 配置时启用）**：以旧综述 + 本轮新折叠段骨架**原文**（素材律：从 MemoryStore 取真实存储文本，非二手卡片行）为素材，增量合成为单行 `〔历史综述〕`，置于卡片层之上；综述长度与单轮素材量受编译期常量约束（`rollingNarrativeCapChars` / `narrativeSkeletonCapChars` / `narrativeEventCap`），不新增配置旋钮。

降级契约：无模型 / 无新折叠素材 / 调用失败时 SHALL 保留旧综述原样、以纯工程形态继续（票据不丢、压缩不被阻塞）。SHALL NOT 存在 legacy 回退路径（`skeleton_segmentation` 配置及其 L3 LLM 段摘要、`segmentContentHash` 归档缓存、`level3Failed` 降级标记随 legacy 管线一并移除——本综述层是折叠点上的单次增量合成，非 legacy 的逐段摘要+分批管线）。

#### Scenario: L3 双层折叠进滚动摘要

- **GIVEN** 骨架化后仍超预算，最老段被定为 L3，且配置了 summary 模型
- **WHEN** 执行多段压缩
- **THEN** 该段 SHALL 整段离场，工程票据层 SHALL 折叠为滚动摘要卡片行
- **AND** SHALL 发生恰好一次 LLM 综述调用（旧综述 + 新折叠骨架原文 → 新单行综述），输出以 `〔历史综述〕` 前缀置于卡片层之上
- **AND** SHALL NOT 读写归档缓存

#### Scenario: 综述层降级不阻塞折叠

- **GIVEN** summary 模型未配置或调用失败
- **WHEN** 执行多段压缩
- **THEN** 折叠 SHALL 以纯工程形态完成（无综述行或保留旧综述原样），卡片票据完整无损

#### Scenario: 纯携带轮零 LLM 成本

- **GIVEN** 本轮无新折叠事件（仅携带既有滚动摘要）
- **WHEN** `buildRetainedRefs` 执行
- **THEN** SHALL NOT 发起 LLM 综述调用，既有综述与卡片原样携带

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

### Requirement: LLM 文摘作为工程层之上的可选叠加层

骨架压缩（`compressSkeleton`）的定级与丢弃是纯工程、零 LLM；L3 折叠的工程票据层亦恒在。管线中的 LLM 文摘恰有两处，均为可选叠加层，失败或无模型时 SHALL 降级为工程形态：

1. **L3 滚动综述 `synthesizeRollingNarrative`**：折叠点的增量单行综述（见"多段压缩归档"需求）；
2. **卡片浓缩 `condenseCardLines`**（`curateCards` 内）：卡片超 `cardMaxChars` 时 SHALL 用 summary 模型浓缩较旧一半卡片、保留最新卡片原文；无模型时 SHALL 将最旧行沉底为计数（不报错）。

所有 LLM 生成的文摘/浓缩内容 SHALL 保留 `[evt_key]` 召回票据，使卡片始终是召回锚点。

（`compressLegacy` 管线、其 LLM 段摘要、`segmentContentHash` 归档缓存与 `context_compress_summary` 固化物产生源已随本变更移除；存量固化物读路径容错、TTL 自然清退。）

#### Scenario: condenseCardLines 浓缩旧卡且保留票据

- **GIVEN** 滚动摘要卡片超过 `cardMaxChars` 且配置了 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 浓缩较旧一半卡片、保留最新卡片原文与 `[evt_key]` 票据

#### Scenario: 无模型时沉底计数不报错

- **GIVEN** 卡片超 `cardMaxChars` 但无 summary 模型
- **WHEN** 执行 `curateCards`
- **THEN** SHALL 将最旧行沉底为 `(earlier n items)` 计数，SHALL NOT 报错

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

### Requirement: 段定级采用指数段龄边界且封顶于 L2

`deterministicLevel` 的段龄阈值 SHALL 为指数边界 `keepRecent × 2^level`（即 L0 < k、L1 < 2k、L2 ≥ 2k），而非线性 `{k, 2k, 3k}`，以使段在每个老化级别驻留更久（前级渲染变化更少，缓存更稳定）。指数底数 SHALL 固定为 2。

**封顶律（触发器单维化对齐）**：基础定级 SHALL 封顶于 L2——段龄 SHALL NOT 触发 L3 归档，否则"段数够多"成为隐性第二触发器，在预算宽裕时执行有损归档（连带 LLM 综述调用）。L3 SHALL 仅由预算升级路径抵达（见"多段压缩归档"需求）。`keepRecent` 为整理后状态约束：最近 k 段在一切路径（含升级）下恒为 L0。

#### Scenario: 指数边界老化定级

- **GIVEN** keepRecent=2
- **WHEN** 计算段龄 age 的基础级别
- **THEN** age=3 SHALL 为 L1、age=5 SHALL 为 L2、age=7 SHALL 为 L2、age=8 SHALL 为 L2（基础阶梯永不抵达 L3；线性下 age=5/6 已为最高档）

#### Scenario: 预算升级才抵达 L3

- **GIVEN** keepRecent=2，历史含 10 个完整段且总 token 超预算
- **WHEN** 执行骨架压缩，骨架化后仍超预算
- **THEN** 预算升级 SHALL 自最老段起逐段升 L3（age ≥ keepRecent 才可升级），达标即停
- **AND** 同样历史在预算内时 SHALL 全部保持在基础老化级别（无 L3、无归档）

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

### Requirement: 纯工具调用 thinking_plan 生成工具调用摘要

`GenerateEventSummary` 对 `thinking_plan` 且 `Content` 为空且 `ToolCalls` 非空的事件，SHALL 用 ToolCalls 生成 `调用 <工具名>` 摘要（工程提取、零 LLM），使老化渲染不再退化为空摘要占位符。`Content` 非空的 thinking_plan SHALL 仍取原文。

#### Scenario: 纯工具调用生成摘要

- **GIVEN** 一个 thinking_plan 事件，`Content=""` 且 `ToolCalls=[read_file, grep]`
- **WHEN** 生成 EventSummary
- **THEN** SHALL 返回 `调用 read_file、grep`，而非空字符串

### Requirement: 老化工具运行折叠为工具链合成引用

骨架压缩 SHALL 在**整理轮**（容量触发的压缩路径内）把**连续的老化完整工具对**（thinking_plan + action_command 序列，中间不被 external_input/agent_output 打断，且处于位置式尾部窗口之外——最近 `recent_full_count` 条 refs 不折叠）折叠为一个 `tool_chain` 合成引用（负 key），其 EventSummary 为一行工具链（`- 工具链: name1→name2→…（N步）[evt_first→evt_last]`）。原工具事件 refs SHALL 从投影移除、由该合成引用替代（无双重表示）。该合成引用 SHALL 使用独立的 `tool_chain` 事件类型（区别于 `context_compress`，不被吸收进滚动摘要计数），且 `buildRetainedRefs` SHALL 一律保留它。工具名 SHALL 取自 ref.EventSummary（无需回取全文）。整理间（未触发轮）SHALL NOT 执行折叠——老化区间外的工具对按 EventSummary 渲染（有界且字节稳定），待下次整理统一折叠。

#### Scenario: 连续工具对折叠为单行工具链

- **GIVEN** 整理轮的老化区间内有连续 3 对工具事件（read_file/grep/edit 及其结果）
- **WHEN** 执行整理路径内的工具链折叠
- **THEN** SHALL 折叠为一个 `tool_chain` 合成引用，EventSummary 为 `- 工具链: read_file→grep→edit（3步）[evt_first→evt_last]`
- **AND** 原 6 条工具事件 refs SHALL 从投影移除

#### Scenario: 不跨回合边界折叠

- **GIVEN** 工具运行中间隔着一个 agent_output 边界事件
- **WHEN** 折叠
- **THEN** SHALL 在边界处断开，两侧分别折叠，不跨回合合并

#### Scenario: 未触发轮不折叠

- **GIVEN** 摘要区间内有连续工具对，但 `usedTokens` 低于阈值（未触发整理）
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** 工具对 SHALL 保持原生 refs（按 EventSummary 渲染），SHALL NOT 产生或扩展 `tool_chain` 合成引用

### Requirement: 活跃前沿与近期配对不折叠

工具链折叠 SHALL 只作用于已老化的完整工具对；最近 `recentFullCount` 条（full=true）消息、当前进行中的未完成 tool_call（无 result）、以及边界事件 SHALL 不折叠、保持原生协议形态（tool_call 配对合法）。

#### Scenario: 活跃前沿保持原生

- **GIVEN** 进行中段的最近若干工具对处于 full=true 区间，且存在一个无 result 的未完成 tool_call
- **WHEN** 折叠
- **THEN** 这些消息 SHALL 保持原生（含 tool_calls），不被折叠
- **AND** 渲染后 tool_call 配对合法性保持

### Requirement: 上下文维护五项不变量

上下文维护 SHALL 满足：I1 有界（进行中段工具历史折叠为 O(工具链行数)，与循环长度解耦）；I2 稠密（无空摘要零信息占位符）；I3 锚定（滚动摘要常驻可见）；I4 无损（工具事件本体永在 MemoryStore，工具链行带票据可经 memory_turn 取回完整链）；I5 原生前沿（活跃前沿与近期配对保持原生）。

#### Scenario: 长进行中段上下文有界且无占位符

- **GIVEN** 一个 ~60 步工具调用的进行中段
- **WHEN** 渲染模型上下文
- **THEN** 工具历史 SHALL 收敛为少量工具链行（而非 ~130 条逐条消息）
- **AND** 上下文中 SHALL NOT 出现 `(历史事件摘要为空，可用 recall 检索)` 占位符

### Requirement: 压缩触发器单维化（token 容量阈值）

`ContextCompressor.Compress` SHALL 以**单一维度**触发整理：仅当 `usedTokens > compress_threshold × max_tokens` 时才调用 `SmartCompressor.Compress` 与投影重写；未超阈值时 SHALL pass-through——直接返回按投影渲染的消息且**不触碰投影**（不折叠、不定级、不重建 refs）。完整回合计数 SHALL NOT 参与触发判断。`SmartCompressor.Compress` 内部提前退出 SHALL 同样仅以 token 预算判断（完整段数/段龄 SHALL NOT 作为触发条件，仅作为 keepRecent 保留保护的状态约束）。

#### Scenario: 未超阈值时完全直通

- **GIVEN** 会话已有远超 `keep_recent_tasks` 的完整回合数，但 `usedTokens` 低于阈值
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL NOT 调用 `SmartCompressor.Compress`，SHALL NOT 执行工具链折叠，投影 refs SHALL 原样返回

#### Scenario: 超阈值但折叠后低于预算则 SmartCompressor 原样返回

- **GIVEN** `usedTokens` 超过阈值触发整理，但工具链折叠后渲染 token 已低于 `SmartCompressor` 预算
- **WHEN** 执行 `SmartCompressor.Compress`
- **THEN** SHALL 原样返回消息（不老化、不定级、不归档），投影仅发生工具链折叠

#### Scenario: 超阈值触发整理（回归）

- **GIVEN** `usedTokens` 超过 `compress_threshold × max_tokens`
- **WHEN** 执行 `ContextCompressor.Compress`
- **THEN** SHALL 调用 `SmartCompressor.Compress`（与既有 token 触发行为一致）

### Requirement: 整理间渲染冻结（前缀字节稳定）

整理（容量触发的压缩轮）之间，投影渲染 SHALL 是 **append-only 稳定**的：全文解析窗口（`recent_full_count`）SHALL 在整理轮锚定为投影内的边界 key（最近 `recent_full_count` 条 retained refs 的最前一条），整理间该边界 SHALL NOT 随 refs 追加而移动。整理后新增事件 SHALL 全文渲染（活跃前沿），既有 refs 的渲染方式（全文/摘要）SHALL 冻结不变——连续未触发轮发出的消息序列 SHALL 保持公共前缀字节级相同。

#### Scenario: 整理间追加新事件不改变旧渲染

- **GIVEN** 上次整理后投影含 refs R1..R50，其中 R35..R50 为全文窗口
- **WHEN** 新事件 R51..R53 追加且未触发整理，渲染下一轮请求
- **THEN** R1..R50 的渲染输出 SHALL 与上一轮逐字节相同（R35..R50 仍全文，R1..R34 仍摘要）
- **AND** R51..R53 SHALL 全文渲染（活跃前沿）

#### Scenario: 整理轮重设边界

- **GIVEN** token 超阈值触发整理
- **WHEN** `buildRetainedRefs` 产出新的 retained refs
- **THEN** 全文窗口边界 SHALL 重设为最近 `recent_full_count` 条 retained refs 的最前一条
- **AND** 之后整理间的渲染方式以新边界冻结

