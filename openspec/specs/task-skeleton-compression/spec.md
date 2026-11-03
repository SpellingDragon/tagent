# task-skeleton-compression Specification

## Purpose

压缩折叠规则：以 `agent_output` 闭合、按其 `input_events` 消费关系归段的任务回合为段，按线性段龄把已闭合段归入新/中/老三带；新带的过程细节折叠为一条段折叠产物，中/老带整段折叠为一条历史综述。折叠只由投影渲染容量触发，判定与执行各成 `compress.request`/`compress.product` 事实，恢复从产物事实重建。

## Requirements

### Requirement: 以 agent_output 为段边界切分任务回合

系统 SHALL 以 `agent_output` 条目为段闭合边界，用 `SegmentsOf` 把投影条目切分为任务回合段，段边界 SHALL 按该输出事实的 `input_events` 消费关系归段：它命名的输入键 SHALL 归入它所在的段并随该段闭合，段内已见但未被它命名的输入 SHALL 滚入下一段，段间的过程条目 SHALL 留在闭合它的段内。`agent_output` 不带 `input_events`（legacy 链）、`input_events` 解析失败或为空数组时，段 SHALL 回退为位置式闭合：输出之前的全部条目归该段，老链 MUST NOT 被重切。尾部没有任何 `agent_output` 闭合的条目 SHALL 成为进行中段。段切分 SHALL 是投影条目的纯函数，MUST NOT 读取消息内容或以消息前缀猜测边界。段龄 SHALL 从最新段向旧计数（0 = 最新段）。

#### Scenario: 完整回合闭合成一个段

- **GIVEN** 条目序列 `[external_input(A), thinking_plan, action_command, agent_output(A), external_input(B), agent_output(B)]`，且两个 `agent_output` 的 `input_events` 各自命名其开段输入
- **WHEN** 执行切段
- **THEN** 产出 2 个已闭合段：段1=`[external_input(A), thinking_plan, action_command, agent_output(A)]`，段2=`[external_input(B), agent_output(B)]`

#### Scenario: 未消费输入滚入下一段

- **GIVEN** 条目序列 `[external_input(1), external_input(2), external_input(3), thinking_plan, agent_output(A)]`，且 `agent_output(A)` 的 `input_events` 只命名 1 与 2
- **WHEN** 执行切段
- **THEN** 段 A SHALL 为 `[external_input(1), external_input(2), thinking_plan, agent_output(A)]`，`external_input(3)` SHALL 成为下一段的首个条目（尚无输出闭合时该段为进行中段）

#### Scenario: legacy 输出回退位置式闭合

- **GIVEN** 条目序列 `[external_input(1), external_input(2), agent_output(A), external_input(3)]`，其中 `agent_output(A)` 不带 `input_events`
- **WHEN** 执行切段
- **THEN** 段 A SHALL 为 `[external_input(1), external_input(2), agent_output(A)]`（位置式闭合），`external_input(3)` SHALL 属于其后的进行中段

#### Scenario: 空或不可解析的 input_events 回退位置式闭合

- **GIVEN** 一个 `agent_output` 的 `input_events` 为空数组、`null` 或不可解析的 JSON
- **WHEN** 执行切段
- **THEN** 该输出 SHALL 按 legacy 位置式闭合处理，MUST NOT 伪造任何输入关系

#### Scenario: 连续输入留在进行中段

- **GIVEN** 条目序列 `[external_input(A), agent_output(A), external_input(B), external_input(C)]`
- **WHEN** 执行切段
- **THEN** 最后一段包含 `[external_input(B), external_input(C)]`，且为进行中段

#### Scenario: 无 agent_output 的尾部为进行中段

- **GIVEN** 条目序列 `[external_input(A), agent_output(A), external_input(B), thinking_plan, action_command]`
- **WHEN** 执行切段
- **THEN** 最后一段为进行中段，其全部条目保留在段内

### Requirement: 新带段折叠过程细节为段折叠产物

对已闭合且处于新带（`BandNew`，段龄 < `KeepRecentTasks`）的段，折叠 SHALL 只把段内过程细节（`thinking_plan` 与 `action_command`）替换为一条 `segment_fold` 产物，开段输入与闭合 `agent_output` SHALL 原样保留。无过程细节的段 SHALL NOT 产生产物；进行中段 SHALL NOT 折叠；已含合成键的条目 MUST NOT 再次折叠。产物正文 SHALL 是过程条目的一行读法（类型 + 摘要），MUST NOT 是 LLM 生成文本。

#### Scenario: 新带段折叠为一条段折叠产物

- **GIVEN** 两个已闭合的新带段，每段各有若干 `thinking_plan` 与 `action_command`
- **WHEN** 投影容量超过阈值并执行折叠
- **THEN** 每段的过程细节 SHALL 各自折叠为一条 `segment_fold` 产物
- **AND** 产物 SHALL 站在原过程条目第一位的位置，输入保持在前、`agent_output` 保持在后

#### Scenario: 无过程细节不产生产物

- **GIVEN** 一个只含 `external_input` 与 `agent_output` 的已闭合新带段
- **WHEN** 执行折叠
- **THEN** 该段 SHALL NOT 产生段折叠产物，条目保持不变

#### Scenario: 进行中段不折叠

- **GIVEN** 一个无 `agent_output` 闭合且含过程细节的进行中段
- **WHEN** 执行折叠
- **THEN** 该段的过程细节 SHALL 原样保留，MUST NOT 产生段折叠产物

### Requirement: 中带与老带整段折叠为一条历史综述

段龄达到 `KeepRecentTasks` 的段（`BandMid` 与 `BandOld`）SHALL 整段离开时间线，并合并为一条 `history_summary` 产物置于投影首位。综述正文 SHALL 逐条保留被折条目的类型与摘要读法，MUST NOT 改写为 LLM 生成文本；既有历史综述 SHALL 原样并入，MUST NOT 重复嵌套或丢失其正文。被折叠的原始事件 SHALL 仍留在事实链，可经 `[evt_key]` 票据召回。

#### Scenario: 中带段折成一条历史综述

- **GIVEN** keepRecent=1，历史含 4 个已闭合段，容量超过阈值
- **WHEN** 执行折叠
- **THEN** 段龄 1–3 的段 SHALL 全部离开时间线，合并为投影首位的一条 `history_summary`
- **AND** 综述正文 SHALL 含这些段的类型 + 摘要读法，最新段仍保留输入、段折叠产物与 `agent_output`

#### Scenario: 老带段并入同一综述

- **GIVEN** keepRecent=2，历史含段龄 6 及以上的段
- **WHEN** 执行折叠
- **THEN** 老带段 SHALL 与中带段一起并入同一条历史综述，MUST NOT 产生第二条综述

#### Scenario: 被折原文仍可召回

- **GIVEN** 一批事件已被折进历史综述
- **WHEN** 用其 `[evt_key]` 票据 recall / GetEvent
- **THEN** SHALL 仍能取回原文（折叠不删事实链原文）

### Requirement: 段龄定级为线性三带

折叠 SHALL 使用 `LevelOfAge`/`BandOfLevel` 的线性段龄规则：`age < KeepRecentTasks` 为 L0（新带），`age < 2×KeepRecentTasks` 为 L1（中带），`age < 3×KeepRecentTasks` 为 L2（中带），其余为 L3（老带）。进行中段 SHALL 读 L0/新带。MUST NOT 使用指数段龄边界（`keepRecent × 2^level`），也 MUST NOT 把基础定级封顶于 L2。`KeepRecentTasks < 1` SHALL 读默认值 2。

#### Scenario: keepRecent=2 的线性边界

- **GIVEN** KeepRecentTasks=2
- **WHEN** 计算已闭合段的级别与带
- **THEN** age=1 SHALL 为 L0/新带，age=2 与 age=4 SHALL 为 L1/L2（中带），age=6 SHALL 为 L3/老带
- **AND** SHALL NOT 出现指数边界的更早降级（例如 age=4 仍应为 L2 而非 L3）

#### Scenario: 未设置窗口读默认值

- **GIVEN** KeepRecentTasks=0 或未设置
- **WHEN** 计算段龄或规划折叠
- **THEN** SHALL 以默认 keepRecent=2 计算，而不是返回错误或零值窗口

### Requirement: 压缩触发器单维化（token 容量阈值）

压缩整理 SHALL 只由投影渲染容量超过声明阈值触发；候选是否实际存在 SHALL 只作为执行门，不得成为第二容量阈值。容量未超阈时 MUST 提前返回且不触碰投影；容量超阈但无新候选时 SHALL no-op，不写空 `compress.request`/`compress.product`。存在可折候选时，不得因其材料量单独未超阈而抑制整理。段数、段龄与完整回合计数 SHALL NOT 作为触发条件，只作为保留窗口的状态约束。

#### Scenario: 未超阈值时完全直通

- **GIVEN** 投影渲染容量低于阈值
- **WHEN** 执行压缩判定
- **THEN** SHALL 直接返回，不折叠、不定级、不重建 refs

#### Scenario: 超阈值但无可折候选

- **GIVEN** 投影容量超过阈值且没有本轮新候选
- **WHEN** 执行压缩判定
- **THEN** SHALL 不写压缩事实、不重写投影；实际模型请求仍由既有预算装配约束

#### Scenario: 超阈值且候选材料自身较小

- **GIVEN** 总容量超过阈值，可折过程材料自身低于阈值
- **WHEN** 执行压缩判定
- **THEN** SHALL 仍产生该真实候选，不得用第二材料阈值拒绝它

#### Scenario: 计划与直接折叠等价

- **WHEN** 同一投影分别经 PlanByAge+ApplyProducts 与 FoldByAge 处理
- **THEN** 两者的产物、覆盖键与最终投影 SHALL 等价，重复执行不得新增同义事实

### Requirement: 进行中段与近期窗口保持原样

无 `agent_output` 闭合的进行中段 SHALL 不参与任何折叠，其全部条目（含 pending 输入与过程细节）SHALL 原样保留。最近 `KeepRecentTasks` 个已闭合段 SHALL 保持输入与输出骨架；只有其段内过程细节可被折为段折叠产物。折叠 MUST NOT 移动进行中段、改变其条目顺序或改写其渲染内容。

#### Scenario: 进行中段完整保留

- **GIVEN** 历史中存在一个含 pending 输入与过程细节的进行中段
- **WHEN** 执行任意折叠
- **THEN** 该进行中段的全部条目 SHALL 原样保留，不被丢弃、归档或改写

#### Scenario: 最近窗口骨架保留

- **GIVEN** keepRecent=2，历史含 3 个已闭合段
- **WHEN** 执行折叠且最老段进入历史综述
- **THEN** 最近 2 段的 `external_input` 与 `agent_output` SHALL 保持原样，仅其过程细节可被折为段折叠产物

### Requirement: 压缩窗口由三字段界定

压缩参数 SHALL 由 `FoldWindow` 的 `MaxTokens`、`CompressThreshold`、`KeepRecentTasks` 三字段界定；`VolumeThresholdBytes` SHALL 按 `MaxTokens × BudgetCharsPerToken × CompressThreshold` 派生折叠触发的渲染字节阈值，`CompressThreshold` 非法或未设置 SHALL 读默认值。`KeepRecentTasks` 未设置 SHALL 读默认值 2。卡片字段（`card_max_chars`/`compact_keys_listed`）MUST NOT 再作为压缩配置存在。

#### Scenario: 阈值按窗口派生

- **GIVEN** MaxTokens=128000、CompressThreshold 为默认值
- **WHEN** 读取折叠窗口的阈值
- **THEN** `VolumeThresholdBytes` SHALL 等于按预算字符/令牌比换算后的窗口份额，而不是固定的卡片预算

#### Scenario: 显式窗口优先

- **GIVEN** 显式设置 KeepRecentTasks=7 且 MaxTokens 为正
- **WHEN** 规划折叠
- **THEN** SHALL 使用 7 作为保留窗口，而不是默认的 2

### Requirement: 上下文维护五项不变量

上下文维护 SHALL 满足：I1 有界（折叠后投影条目数与段数、窗口成界，不随历史长度无界增长）；I2 稠密（不产生空摘要或零信息占位符）；I3 锚定（历史综述常驻投影首位并参与装配）；I4 无损（被折原文永在事实链，可经 `[evt_key]` 票据召回）；I5 原生前沿（进行中段与最近窗口保持原生条目，不改写为摘要）。

#### Scenario: 长上下文有界且无占位符

- **GIVEN** 一个含大量历史回合与一个进行中段的投影，容量超过阈值
- **WHEN** 执行折叠并装配上下文
- **THEN** 历史回合 SHALL 收敛为少量历史综述/段折叠条目，而非逐条重放
- **AND** 上下文中 SHALL NOT 出现空摘要或零信息占位符
