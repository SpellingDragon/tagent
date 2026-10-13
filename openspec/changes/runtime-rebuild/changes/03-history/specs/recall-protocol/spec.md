# recall-protocol delta（03-history）

> 复用登记（非规范，不改本条语义）：召回为显式工具调用的确定性形态（items/query/turn_key）与 `orchestrate` 显式 opt-in；原文读取与引用关系经事实链 `GetEvent`/`GetParent`。
> 本 delta 修改「索引卡为召回标准协议（输入形态分流）」并新增「原文引用与分段读取保真」；本能力其余 Requirement（RecallAgent 定位收窄、查询类召回结果的诚实截断提示、memory_turn 因果链召回工具、卡片行标注可追溯提示、items 批量水合有界）由主 spec `openspec/specs/recall-protocol/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 索引卡为召回标准协议（输入形态分流）

系统 SHALL 提供主 agent 直持的**统一召回入口 `recall`**（单工具，无多入口区分），按输入形态分流:

- `items: [{key, hint?}]`（索引卡条目）→ 工程化精确召回:按 key 批量 `GetEvent`,原序回补,零 LLM、零幻觉;hint SHALL 原样回显供对账;未命中的 key SHALL 明确报告（不静默省略）
- `query`（自由文本,可带 time/type/keyword filters）→ 语义召回:现状为 QueryOptions keyword 检索,检索层可独立演进（向量等）而入口协议 SHALL 不变
- `turn_key`（EventKey，通常为 `agent_output` 卡片）→ 因果链召回:沿 `GetParent` 逐跳回走至 `external_input`（含）为止，返回该回合全部事件（含被骨架压缩丢弃的 `thinking_plan`/`action_command`），按时间排序
- `orchestrate: true`（显式 opt-in）→ LLM 多跳编排**保留形态**：接入 RecallAgent 编排引擎时升级之；引擎未接线时 SHALL 返回明确的未接线指引（含确定性迭代建议），SHALL NOT 静默降级为某个确定性形态——确定性形态（items/turn_key/query）SHALL 始终走纯工程路径
- items 与 query 同时提供时 items SHALL 优先

输出协议统一:条目 `{key(hex), type, summary, content, time}`。触发 SHALL 保持显式工具调用（不做隐式自动换出）。`memory_recall`/`memory_turn` 工具名 SHALL 退役（能力由 `recall` 的对应参数形态承接，协议输出不变）。

原文引用与引用元数据 SHALL 分离：blob 内容可去重，但 `owner`/`format`/保留策略不同的引用 MUST NOT 互覆盖。系统 MUST NOT 复活多套同义召回接口（多入口并存即分叉）。

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

#### Scenario: 同 blob 不同 owner 不覆盖

- **WHEN** 同一段原文 blob 被 owner/format 不同的两个引用登记
- **THEN** 两个引用 SHALL 各自保留，MUST NOT 因 blob 内容相同而互相覆盖

## ADDED Requirements

### Requirement: 原文引用与分段读取保真

原文读取 SHALL 与原文存储字节一致；`items` 精确召回 SHALL 原序回补、零 LLM，未命中 key SHALL 明确报告（不静默省略）。系统 SHALL 提供**分段读取**入口（按 `start_line`/`num_lines` 行级分页）以消费超大原文，MUST NOT 以「召回即全文回流上下文」为唯一路径；凭票据召回超大事件 SHALL 返回有界版 + 票据，全文经分段读取获取。

原文引用 SHALL 记录 `owner`/`format`/保留策略并以此隔离；这些元数据不同的引用 MUST NOT 被去重合并。

#### Scenario: 分段读取字节一致

- **WHEN** 对一段原文按 `start_line`/`num_lines` 分段读取并拼接
- **THEN** 拼接结果 SHALL 与原文逐字节一致

#### Scenario: 超大原文不全量回流

- **WHEN** 召回一个远超阈值的事件
- **THEN** 返回 SHALL 为有界版 + 票据，全文 SHALL 经分段读取获取，MUST NOT 全量回流上下文

#### Scenario: 未命中显式报告

- **WHEN** `items` 中存在不存在的 key
- **THEN** 未命中 SHALL 被明确标注（不静默省略），命中项 SHALL 原序返回

#### Scenario: 不留多套同义召回接口

- **WHEN** 审计召回入口
- **THEN** 对主 agent SHALL 只暴露统一 `recall` 入口，MUST NOT 并存多套同义召回接口
