## ADDED Requirements

### Requirement: 工具输出大小拦截包装器

tagent.go SHALL 在 `buildToolFromRef` 中为所有工具（agent-kind 和 tool-kind）添加输出大小拦截包装器（`OutputLimitTool`）。包装器 SHALL 拦截工具 `Call()` 方法的返回值，当序列化后的输出字符数超过阈值时，截断输出并附加错误提示信息。

#### Scenario: 输出未超限正常返回

- **WHEN** 工具执行返回结果，序列化后字符数为 500，阈值为 8000
- **THEN** 原始结果正常返回，不截断

#### Scenario: 输出超限截断并附加错误

- **WHEN** 工具执行返回结果，序列化后字符数为 15000，阈值为 8000
- **THEN** 结果被截断为前 8000 字符
- **AND** 附加 `[ERROR: Tool output exceeded 8000 characters, truncated. Total: 15000 characters. Consider optimizing your command or using more specific queries.]`

#### Scenario: agent-kind 工具也被拦截

- **WHEN** agent-kind 工具（如 knowledge、recall）通过 AgentToolWrapper 返回超长结果
- **THEN** 包装器同样拦截并截断输出

#### Scenario: nil 结果不拦截

- **WHEN** 工具执行返回 nil
- **THEN** 原始 nil 结果正常返回，不触发拦截

### Requirement: 拦截阈值与 MaxTokens 对齐

拦截阈值 SHALL 为 `MaxTokens / 2 * 4`（字符数），其中 MaxTokens 来自 AgentConfig.MaxTokens，1 token 估算为 4 字符。当 AgentConfig.MaxTokens 为 0 时，SHALL 使用默认值 32000（对应 64000 字符阈值）。

#### Scenario: 标准配置计算阈值

- **WHEN** AgentConfig.MaxTokens = 16000
- **THEN** 拦截阈值 = 16000 / 2 * 4 = 32000 字符

#### Scenario: MaxTokens 为 0 使用默认值

- **WHEN** AgentConfig.MaxTokens = 0
- **THEN** 使用默认 MaxTokens = 32000
- **AND** 拦截阈值 = 32000 / 2 * 4 = 64000 字符

### Requirement: OutputLimitTool 实现 trpctool.Tool 接口

`OutputLimitTool` SHALL 实现 `trpctool.Tool` 接口，包装内部工具的 `Definition()` 和 `Call()` 方法。`Definition()` SHALL 原样返回内部工具的定义。`Call()` SHALL 执行内部工具后拦截返回值。

#### Scenario: Definition 透传

- **WHEN** 调用 OutputLimitTool.Definition()
- **THEN** 返回内部工具的 Definition，不做修改

#### Scenario: Call 拦截返回值

- **WHEN** 调用 OutputLimitTool.Call(ctx, args)
- **THEN** 先调用内部工具的 Call(ctx, args)
- **AND** 对返回值进行序列化（json.Marshal）
- **AND** 检查字符数是否超过阈值
- **AND** 超限时截断并附加错误信息
