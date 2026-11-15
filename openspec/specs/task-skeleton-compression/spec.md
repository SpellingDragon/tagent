# task-skeleton-compression Specification

## Purpose

压缩折叠规则：以 `agent_output` 闭合、按其 `input_events` 消费关系归段的任务回合为段，按带晋升把已闭合段归入新/中/老三带——近期窗口内的段为新带并原样保留，已离开窗口而未被折叠过的段为中带，其过程细节折叠为一条带事件键票据的段折叠产物，已携带更早折叠产物的段为老带，整段并入一条历史综述。综述正文以机械有界读法为底，会话持有模型时由压缩消费插件产出有界语义综述，字节预算由声明面界定。折叠只由投影渲染容量触发，判定与执行各成 `compress.request`/`compress.product` 事实，恢复从产物事实重建。

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

### Requirement: 中带段的过程细节折叠为带事件键票据的段折叠产物

对已离开近期窗口且尚未被折叠过的段（中带），折叠 SHALL 只把段内过程细节（`thinking_plan` 与 `action_command`）替换为一条 `segment_fold` 产物，开段输入与闭合 `agent_output` SHALL 原样保留。近期窗口内的段与进行中段 SHALL 原样保留，其过程细节 MUST NOT 在窗口内被折叠。无过程细节的段 SHALL NOT 产生产物；已携带更早折叠产物的段 MUST NOT 再次折叠其条目；已含合成键的条目 MUST NOT 再次折叠。产物正文 SHALL 是过程条目的一行读法（类型 + 摘要），每行 SHALL 携带该被折条目自己的事件键票据，MUST NOT 是模型生成文本。

#### Scenario: 中带段折叠为一条段折叠产物

- **GIVEN** 两个已离开近期窗口且过程细节完整的已闭合段，每段各有若干 `thinking_plan` 与 `action_command`
- **WHEN** 投影容量超过阈值并执行折叠
- **THEN** 每段的过程细节 SHALL 各自折叠为一条 `segment_fold` 产物
- **AND** 产物 SHALL 站在原过程条目第一位的位置，输入保持在前、`agent_output` 保持在后
- **AND** 产物正文每行 SHALL 携带该被折条目自己的 `[evt_<KEY>|<type>]` 票据，票据键集 SHALL 恰等于该产物覆盖的条目集，顺序同折前

#### Scenario: 近期窗口内的段不被折叠过程细节

- **GIVEN** 一个仍在近期窗口内、已闭合且含 `thinking_plan` 与 `action_command` 的段
- **WHEN** 执行折叠
- **THEN** 该段 SHALL 原样保留其输入、过程细节与 `agent_output`，MUST NOT 产生段折叠产物

#### Scenario: 无过程细节不产生产物

- **GIVEN** 一个只含 `external_input` 与 `agent_output` 的已闭合新带段
- **WHEN** 执行折叠
- **THEN** 该段 SHALL NOT 产生段折叠产物，条目保持不变

#### Scenario: 进行中段不折叠

- **GIVEN** 一个无 `agent_output` 闭合且含过程细节的进行中段
- **WHEN** 执行折叠
- **THEN** 该段的过程细节 SHALL 原样保留，MUST NOT 产生段折叠产物

### Requirement: 老带段整段折叠为一条历史综述

已携带更早一次折叠产物的段（老带）SHALL 整段离开时间线，并合并为一条 `history_summary` 产物置于投影首位。综述正文 SHALL 以机械有界读法为底：逐条保留被折条目的类型与摘要读法，并服从综述字节预算。会话 agent 持有模型时，压缩消费插件 SHALL 以模型产出的有界语义综述取代该机械正文；无模型、请求失败或答空 SHALL 保留机械正文并记一行可检索告警，MUST NOT 提交空正文、MUST NOT 阻塞折叠、MUST NOT 使正文超过预算。综述字节预算 SHALL 由声明面 `summary_budget_bytes` 界定，未声明 SHALL 读声明缺省；MUST NOT 由机制常量界定，也 MUST NOT 静默抬到缺省。既有历史综述 SHALL 原样并入，MUST NOT 重复嵌套或丢失其正文。被折叠的原始事件 SHALL 仍留在事实链，可经 `[evt_key]` 票据召回。

#### Scenario: 老带段折成一条历史综述

- **GIVEN** keepRecent=1，历史含 4 个已闭合段，其中 3 个已携带更早一次折叠的产物，容量超过阈值
- **WHEN** 执行折叠
- **THEN** 这 3 个老带段 SHALL 全部离开时间线，合并为投影首位的一条 `history_summary`
- **AND** 综述正文 SHALL 含这些段的类型 + 摘要读法，最新段仍保留输入、段折叠产物与 `agent_output`

#### Scenario: 中带段的过程细节不并入综述

- **GIVEN** 历史含一个已离开近期窗口、尚未被折叠过的段与一个已携带折叠产物的段
- **WHEN** 执行折叠
- **THEN** 前者 SHALL 折为一条段折叠产物留在时间线，MUST NOT 并入历史综述
- **AND** 后者 SHALL 并入历史综述

#### Scenario: 综述正文由模型产出并服从声明预算

- **GIVEN** 声明 `summary_budget_bytes: 512`，会话 agent 持有模型，老带材料折为一条 `history_summary`
- **WHEN** 压缩消费插件执行该判定
- **THEN** 落链 `compress.product` 的正文 SHALL 是模型产出的语义综述，其长度 SHALL 不超过该声明预算
- **AND** 未声明该键时正文长度 SHALL 不超过声明缺省，判定侧与执行侧 SHALL 取同一预算

#### Scenario: 无模型或答空保留机械正文并告警

- **GIVEN** 会话 agent 不持有模型，或摘要请求失败、终态非完成、答空
- **WHEN** 压缩消费插件执行该判定
- **THEN** 综述正文 SHALL 保留折叠自己铸的机械有界读法，折叠 SHALL NOT 被阻塞
- **AND** SHALL 记一行可检索告警，MUST NOT 提交空正文或伪造正文

#### Scenario: 老带段并入同一综述

- **GIVEN** keepRecent=2，历史含多个已携带折叠产物的老带段
- **WHEN** 执行折叠
- **THEN** 老带段 SHALL 一起并入同一条历史综述，MUST NOT 产生第二条综述

#### Scenario: 被折原文仍可召回

- **GIVEN** 一批事件已被折进历史综述
- **WHEN** 用其 `[evt_key]` 票据 recall / GetEvent
- **THEN** SHALL 仍能取回原文（折叠不删事实链原文）

### Requirement: 段龄定级为线性三带

段龄阶梯 SHALL 为线性三带：段龄小于 `KeepRecentTasks` 为最新一档，小于其窗口两倍为第二档，小于其窗口三倍为第三档，其余为最老一档。MUST NOT 使用指数段龄边界，也 MUST NOT 把基础定级封顶于第二档。该阶梯 SHALL 只陈述段与最新段之间的距离读法，MUST NOT 决定段归入哪一带：折叠的带归属 SHALL 由带晋升决定——近期窗口内的段为新带，已携带更早折叠产物的段为老带，其余为中带。进行中段 SHALL 读最新一档。`KeepRecentTasks` 未设置 SHALL 读声明缺省窗口值。

#### Scenario: 缺省窗口的线性边界

- **GIVEN** KeepRecentTasks 未设置（读缺省窗口）
- **WHEN** 计算已闭合段的段龄阶梯与带晋升结果
- **THEN** 相邻两段窗口内的段龄 SHALL 读同一档，阶梯 SHALL 随段龄线性推进，SHALL NOT 出现指数边界的更早降级
- **AND** 段的折叠带 SHALL 只由带晋升给出（窗口内为新带、携带折叠产物为老带、其余为中带），SHALL NOT 由阶梯档位直接给出

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

无 `agent_output` 闭合的进行中段 SHALL 不参与任何折叠，其全部条目（含 pending 输入与过程细节）SHALL 原样保留。最近 `KeepRecentTasks` 个已闭合段（新带）SHALL 原样保留输入、输出骨架与段内过程细节，MUST NOT 在窗口内被折叠。折叠 MUST NOT 移动进行中段、改变其条目顺序或改写其渲染内容。

#### Scenario: 进行中段完整保留

- **GIVEN** 历史中存在一个含 pending 输入与过程细节的进行中段
- **WHEN** 执行任意折叠
- **THEN** 该进行中段的全部条目 SHALL 原样保留，不被丢弃、归档或改写

#### Scenario: 最近窗口原样保留

- **GIVEN** keepRecent=2，历史含数个已闭合段
- **WHEN** 执行折叠且窗口外的段离开时间线
- **THEN** 最近两段的 `external_input`、`agent_output` 与过程细节 SHALL 保持原样，MUST NOT 出现段折叠产物

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
