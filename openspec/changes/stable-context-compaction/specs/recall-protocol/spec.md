## MODIFIED Requirements

### Requirement: 索引卡为召回标准协议（输入形态分流）

系统 SHALL 提供主 agent 直持的**统一召回入口 `recall`**（单工具，无多入口区分），按输入形态分流:

- `items: [{key, hint?}]`（索引卡条目）→ 工程化精确召回:按 key 批量 `GetEvent`,原序回补,零 LLM、零幻觉;hint SHALL 原样回显供对账;未命中的 key SHALL 明确报告（不静默省略）
- `query`（自由文本,可带 time/type/keyword filters）→ 语义召回:现状为 QueryOptions keyword 检索,检索层可独立演进（向量等）而入口协议 SHALL 不变
- `turn_key`（EventKey，通常为 `agent_output` 卡片）→ 因果链召回:沿 `GetParent` 逐跳回走至 `external_input`（含）为止，返回该回合全部事件（含被骨架压缩丢弃的 `thinking_plan`/`action_command`），按时间排序
- `orchestrate: true`（显式 opt-in）→ LLM 多跳编排**保留形态**：接入 RecallAgent 编排引擎时升级之；引擎未接线时 SHALL 返回明确的未接线指引（含确定性迭代建议），SHALL NOT 静默降级为某个确定性形态——确定性形态（items/turn_key/query）SHALL 始终走纯工程路径
- items 与 query 同时提供时 items SHALL 优先

输出协议统一:条目 `{key(hex), type, summary, content, time}`。触发 SHALL 保持显式工具调用（不做隐式自动换出）。`memory_recall`/`memory_turn` 工具名 SHALL 退役（能力由 `recall` 的对应参数形态承接，协议输出不变）。

#### Scenario: 卡片票据工程化召回

- **WHEN** 模型从卡片序列抠出 key 构造 items 调用 recall
- **THEN** SHALL 纯函数批量精确回补（无 LLM 调用）,原序返回,未命中项明确标注

#### Scenario: 自由文本语义召回

- **WHEN** 仅提供 query（无 items）
- **THEN** SHALL 走检索层召回;将来检索层升级为向量语义时,调用方协议不变

#### Scenario: 卡片行票据无损

- **WHEN** 卡片序列渲染进上下文
- **THEN** 其中的 `[hex]` key SHALL 可被模型直接抠出构造 items（无需格式转换）

#### Scenario: 因果链召回并入统一入口

- **GIVEN** 一个已完成回合，其 `thinking_plan`/`action_command` 已被骨架压缩丢弃出时间线
- **WHEN** 调用 `recall(turn_key=<agent_output 的 key>)`
- **THEN** SHALL 返回 `[external_input, thinking_plan, action_command, ..., agent_output]` 整轮事件，在到达 `external_input` 后停止回走

#### Scenario: 确定性形态不触发 LLM

- **WHEN** 调用带 items/turn_key/query（无 orchestrate）
- **THEN** 路径 SHALL 为纯函数/工程检索，不经过任何 LLM 编排

#### Scenario: LLM 编排为显式 opt-in 保留形态

- **WHEN** 调用携带 `orchestrate: true`
- **THEN** SHALL 优先于确定性形态被识别；编排引擎已接线时升级 RecallAgent，未接线时返回明确指引
- **AND** 未携带时 SHALL NOT 进入 LLM 路径

### Requirement: RecallAgent 定位收窄

RecallAgent（sub agent）SHALL 保留,定位收窄为 `recall` 工具 `orchestrate` 分支的**内部编排引擎**（复杂检索与多跳编排，如 trace 因果链遍历、跨多轮收窄）;其子工具（recall_query/recall_get/recall_recent/recall_trace/memory_turn）SHALL NOT 再对主 agent 直接暴露（装配面收编为单一 `recall` 工具）。简单精确/单轮语义/因果链召回 SHALL 经 `recall` 的确定性参数形态直达,不绕行编排。

#### Scenario: 确定性路径无概率组件

- **WHEN** 模型持有明确 key 进行召回
- **THEN** 调用路径 SHALL 为纯函数直达,不经过任何 LLM 编排

#### Scenario: 编排引擎内部复用子工具

- **WHEN** `recall(orchestrate: true)` 升级 RecallAgent
- **THEN** RecallAgent SHALL 以既有子工具（query/get/recent/trace/turn）完成编排，主 agent 侧只见 `recall` 单入口

### Requirement: memory_turn 因果链召回工具

（并入统一入口——`recall` 的 `turn_key` 参数形态即原 `memory_turn`，行为契约不变）系统 SHALL 经由 `recall(turn_key=<EventKey>)` 提供因果链召回：给定一个 EventKey（通常为 `agent_output` 卡片），沿 `GetParent` 因果链逐跳回走，直到遇到 `external_input`（含）为止，返回该区间内的全部事件（含被骨架压缩丢弃的 `thinking_plan` / `action_command`），按时间排序。回走 SHALL 以 `external_input` 事件类型作为停止条件，由此圈定"当前任务回合"而无需正向遍历。独立工具名 `memory_turn` SHALL 退役。

#### Scenario: 锚 agent_output 回走重建整轮

- **GIVEN** 一个已完成的回合，其 `thinking_plan` / `action_command` 已被骨架压缩丢弃出时间线
- **WHEN** 调用 `recall(turn_key=<agent_output 的 key>)`
- **THEN** SHALL 返回 `[external_input, thinking_plan, action_command, ..., agent_output]` 整轮事件
- **AND** 在到达 `external_input` 后停止回走

#### Scenario: 丢弃的执行过程可被召回

- **WHEN** 模型需要了解某轮的具体工具执行过程
- **THEN** SHALL 经 `recall(turn_key=...)` 用边界卡片上的 `agent_output` key 一次性召回被丢弃的 tool 步骤
