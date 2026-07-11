## ADDED Requirements

### Requirement: extractExecutionState is internal to SmartCompressor

The `extractExecutionState` function SHALL be migrated from a standalone function to a method on SmartCompressor. Its truncation parameters (`maxExecStateChars`, `maxToolResultChars`, `maxToolArgsChars`) SHALL be fields on SmartCompressor, configurable via SmartCompressorOption. The standalone function and package-level constants SHALL be removed.

#### Scenario: extractExecutionState uses configurable parameters

- **WHEN** SmartCompressor is created with `WithMaxExecStateChars(3000)` and `WithMaxToolResultChars(800)`
- **THEN** extractExecutionState SHALL truncate tool results to 800 chars and total execution state to 3000 chars

#### Scenario: Default parameters preserve current behavior

- **WHEN** SmartCompressor is created without explicit compress options
- **THEN** maxExecStateChars SHALL default to 2000, maxToolResultChars to 500, maxToolArgsChars to 80

### Requirement: extractExecutionState extracts async tool results from system messages

extractExecutionState SHALL extract system messages containing `[system] tmux` prefix (ActionTool async results) in addition to RoleTool messages. Each extracted async result SHALL be truncated to `maxToolResultChars` and prefixed with `→ 异步结果:`.

#### Scenario: Async tmux result preserved in execution state

- **WHEN** an old segment contains a system message `[system] tmux session X state changed: running -> completed\nOutput:\n<article>`
- **THEN** extractExecutionState SHALL include `→ 异步结果: [system] tmux session X state changed...` (truncated to maxToolResultChars)
