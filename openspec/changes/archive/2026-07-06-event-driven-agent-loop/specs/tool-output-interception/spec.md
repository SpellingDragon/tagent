## MODIFIED Requirements

### Requirement: 工具输出大小拦截包装器

tagent SHALL 在 `NewTagentAgent` 中为所有工具添加输出大小拦截包装器（`OutputLimitTool`）。包装器 SHALL 拦截工具 `Call()` 方法的返回值，当序列化后的输出字符数超过阈值时，截断输出并附加错误提示信息。

在事件驱动架构下，工具的 `Call()` 返回值不再直接进入 LLM context，而是作为 `external_input` 事件的载荷发布到 EventBus。OutputLimitTool 的拦截逻辑不变，仍然在 `Call()` 返回时执行截断，确保回写到 bus 的 external_input 事件载荷不超过大小限制。

#### Scenario: 输出未超限正常返回

- **WHEN** 工具执行返回结果，序列化后字符数为 500，阈值为 8000
- **THEN** 原始结果正常返回，不截断
- **AND** 结果作为 external_input 载荷发布到 bus

#### Scenario: 输出超限截断并附加错误

- **WHEN** 工具执行返回结果，序列化后字符数为 15000，阈值为 8000
- **THEN** 结果被截断为前 8000 字符
- **AND** 附加 `[ERROR: Tool output exceeded 8000 characters, truncated. Total: 15000 characters. Consider optimizing your command or using more specific queries.]`

#### Scenario: agent-kind 工具也被拦截

- **WHEN** agent-kind 工具（如 knowledge、recall）通过异步 goroutine 返回超长结果
- **THEN** 包装器同样拦截并截断输出
- **AND** 截断后的结果作为 external_input 回写 bus

#### Scenario: nil 结果不拦截

- **WHEN** 工具执行返回 nil
- **THEN** 原始 nil 结果正常返回，不触发拦截
