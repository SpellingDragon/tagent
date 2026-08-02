# recall-protocol Specification

## Purpose

本规范定义召回标准协议:索引卡为召回票据,memory_recall 纯函数工具按输入形态分流（items 工程化精确召回 / query 语义召回）,RecallAgent 收窄为复杂检索。

## Requirements
### Requirement: 索引卡为召回标准协议（输入形态分流）

系统 SHALL 提供主 agent 直持的纯函数召回工具 `memory_recall`（无 LLM 中间层）,按输入形态分流:

- `items: [{key, hint?}]`（索引卡条目）→ 工程化精确召回:按 key 批量 `GetEvent`,原序回补,零幻觉;hint SHALL 原样回显供对账;未命中的 key SHALL 明确报告（不静默省略）
- `query`（自由文本,可带 time/type/keyword filters）→ 语义召回:现状为 QueryOptions keyword 检索,检索层可独立演进（向量等）而入口协议 SHALL 不变
- items 与 query 同时提供时 items SHALL 优先

输出协议统一:条目 `{key(hex), type, summary, content, time}`。触发 SHALL 保持显式工具调用（不做隐式自动换出）。

#### Scenario: 卡片票据工程化召回

- **WHEN** 模型从卡片序列抠出 key 构造 items 调用 memory_recall
- **THEN** SHALL 纯函数批量精确回补（无 LLM 调用）,原序返回,未命中项明确标注

#### Scenario: 自由文本语义召回

- **WHEN** 仅提供 query（无 items）
- **THEN** SHALL 走检索层召回;将来检索层升级为向量语义时,调用方协议不变

#### Scenario: 卡片行票据无损

- **WHEN** 卡片序列渲染进上下文
- **THEN** 其中的 `[hex]` key SHALL 可被模型直接抠出构造 items（无需格式转换）

### Requirement: RecallAgent 定位收窄

RecallAgent（sub agent）SHALL 保留,定位收窄为复杂检索与多跳编排（如 trace 因果链遍历、跨多轮收窄）;其子工具不变。简单精确/单轮语义召回 SHALL 经 memory_recall 直达,不再绕行 sub agent。

#### Scenario: 确定性路径无概率组件

- **WHEN** 模型持有明确 key 进行召回
- **THEN** 调用路径 SHALL 为纯函数直达,不经过任何 LLM 编排

### Requirement: 查询类召回结果的诚实截断提示

查询类召回工具（`memory_query`、`memory_recent`、`memory_recall` 的 query 模式）在返回结果数恰好等于 limit 时，SHALL 在返回 message 中注明结果可能被截断（如"已达 limit，更旧的匹配未返回；可缩小时间范围或增大 limit"），SHALL NOT 让调用方（LLM）把"返回 N 条"误读为"全量只有 N 条"。结果数少于 limit 时 SHALL NOT 附加该提示。

#### Scenario: 达到 limit 时提示可能截断

- **GIVEN** 库中匹配事件多于 limit
- **WHEN** 查询类召回工具返回恰好 limit 条结果
- **THEN** 返回 message SHALL 包含截断提示与缩小范围/翻页的建议

#### Scenario: 未达 limit 时不提示

- **GIVEN** 库中匹配事件少于 limit
- **WHEN** 查询类召回工具返回全部匹配
- **THEN** 返回 message SHALL NOT 包含截断提示

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

`buildRetainedRefs` 压缩时 SHALL 统计每轮被丢弃的 tool 步数，并在 L3（整段离场）回合的 agent_output 卡片行标注"含 N 步工具调用，可用 memory_turn 追溯"的提示，使模型知晓该回合存在可召回的执行过程及其召回入口（agent_output 卡片 key）。计数 SHALL 只在 agent_output（回合结束）重置，SHALL NOT 被回合中途 bus 注入的 external_input（task_settled 等）误清零。

#### Scenario: 含 tool 步骤的回合卡片带追溯提示

- **GIVEN** 一个含 3 步工具调用的回合被压缩成边界卡片
- **WHEN** 渲染该卡片行
- **THEN** SHALL 含"含 3 步工具调用，可用 memory_turn 追溯"或等效提示
