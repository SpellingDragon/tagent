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
