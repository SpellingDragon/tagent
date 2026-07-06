## ADDED Requirements

### Requirement: WorkDir 测试兼容 macOS 符号链接
`TestCommandTool_WorkDir` 的路径断言必须兼容 macOS 下 `/var → /private/var` 符号链接。

#### Scenario: 符号链接规范化后断言
- **WHEN** 在 macOS 上运行 `TestCommandTool_WorkDir`
- **THEN** expected 和 got 路径均通过 `filepath.EvalSymlinks` 规范化后比较
- **AND** 测试通过而非因 `/var` vs `/private/var` 差异失败
