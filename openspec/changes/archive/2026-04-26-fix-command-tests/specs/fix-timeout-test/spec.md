## ADDED Requirements

### Requirement: Timeout 测试正确触发超时错误
`TestCommandTool_Timeout` 必须能正确触发并捕获 timeout 错误。

#### Scenario: timeout 参数生效
- **WHEN** 运行 command 执行 `sleep 5` 并设置 `timeout: 1`
- **THEN** Call 方法返回非 nil 错误（超时）
- **AND** result 为非 `*CommandExecResult` 或为 nil

#### Scenario: timeout 无代码修改时不引入假阳性
- **WHEN** 排查 timeout 机制后确认根因在测试层
- **THEN** 仅修改测试代码，不改变 command.go 的执行器逻辑
