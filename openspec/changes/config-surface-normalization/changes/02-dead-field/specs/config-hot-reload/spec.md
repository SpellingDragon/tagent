## ADDED Requirements

### Requirement: 死字段清除与指纹归位

无行为消费者的声明字段 SHALL 删除：`AgentSpec.Params`（free-form float map）的全部引用皆是声明面自身 machinery（decode/clone/fingerprint）与一个 reload 日志标签，无独立行为需求，MUST 删除及其伴生引用。删除后规范指纹 SHALL NOT 再含该字段；`ToolRef.Params`（string map，工具引用参数）是独立字段，MUST NOT 连带删除。

预算字段 rename（`max_tokens→replay_window`、`provider_context_window→context_window`）SHALL 改变规范指纹编码，触发结构 republish 属预期；热参语义（折叠节奏）MUST NOT 因改名改变。

#### Scenario: 死字段声明被拒

- **WHEN** 配置声明 `params` 键
- **THEN** `DecodeStrict` SHALL 以 ErrUnknownConfigKey 拒绝（字段已删）

#### Scenario: 指纹负零归一仍成立

- **WHEN** 删除 Params 后以 Temperature 字段测负零归一
- **THEN** 规范指纹 SHALL 仍把 -0 与 +0 归一为同一编码（机制不因 fixture 换字段而失效）

#### Scenario: 工具引用参数保留

- **WHEN** 声明 ToolRef 的 `params`（string map，如 knowledge tool 的 source/tool/max_results）
- **THEN** SHALL 正常解码与消费，MUST NOT 因 AgentSpec.Params 删除而受影响