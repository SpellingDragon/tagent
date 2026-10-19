# call-subagent-config

## ADDED Requirements

### Requirement: 双装配源声明

`call_subagent` 工具 SHALL 在其工具描述（tool desc）中声明两种互斥的装配源：`agents_file`（markdown + frontmatter 定义文件路径）或显式参数（`prompt`、`model`、`skills`、`tools`、`mcp`）。两种装配源同时给出时 MUST 具名拒绝，错误零副作用。

#### Scenario: agents 文件装配
- **WHEN** 工具调用携带 `agents_file` 指向一份含 `name/model/prompt/skills/tools/mcp` frontmatter 的 md 文件
- **THEN** 装配器按文件字段构造子 agent，未声明字段取父 agent 同名字段，装配完成后进入执行

#### Scenario: 显式参数装配
- **WHEN** 工具调用直接携带 `prompt/model/skills/tools/mcp` 参数且无 `agents_file`
- **THEN** 装配器按参数构造子 agent，装配完成后进入执行

#### Scenario: 双源同给被拒
- **WHEN** 工具调用同时携带 `agents_file` 与显式参数
- **THEN** MUST 返回具名拒绝，不构造任何子 agent，不产生任务

### Requirement: 装配后执行=task 化委派

子 agent 装配完成后的执行 SHALL 经 `agent.Delegate` 受理为异步 task：调用方立即获得受理回执（Receipt），任务受 task manager 管理，状态变更 MUST 通知发起调用的发送者。

#### Scenario: 受理即返
- **WHEN** 装配完成且执行被受理
- **THEN** 调用方在子 agent 完成前获得 Receipt 与 TaskID，不阻塞等待结果

#### Scenario: 完成通知回调
- **WHEN** 子 agent 任务到达终结状态
- **THEN** 终结通知 MUST 定向投递到发起该调用的发送者地址，且通知身份字段（TaskID/Round/Kind/Seq）保真

### Requirement: 预置工具装配

配置面 SHALL 允许通过 `AgentSpec.Tools` 引用预置 `knowledge`、`recall` 与 `call_subagent` 三类工具；装配时 `knowledge` MUST 复用 tRPC-Agent-Go 的 knowledge 工具实现，`recall` MUST 复用既有 memory 检索面，不新造检索或知识栈。

#### Scenario: 三类预置工具按声明装配
- **WHEN** 配置声明三种工具引用
- **THEN** 装配产物携带对应工具实例，且 knowledge 检索行为来自 tRPC knowledge 工具、recall 行为来自既有 memory 检索

### Requirement: event-loop 扩展点

事件消费扩展 SHALL 经宿主循环（AdmitInbound/mailbox 接纳）承载：自定义事件类型到达后，消费逻辑可调用 LLM 与工具，消费结果经投递面回送。历史冥想/knowledge 专属消费不迁移，作为该扩展点的示例文档化。

#### Scenario: 自定义事件被消费并投递结果
- **WHEN** 宿主循环接纳一条自定义类型事件且消费逻辑调用 LLM/tool
- **THEN** 消费结果 MUST 经投递面回送，且零结果时不得静默（拒绝具名）
