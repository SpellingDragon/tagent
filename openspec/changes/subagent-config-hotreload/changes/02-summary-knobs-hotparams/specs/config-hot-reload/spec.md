## ADDED Requirements

### Requirement: 摘要策略热参

摘要策略 knob SHALL 经源拉取面热更：配置字段解析入 `OrgHotParams`，摘要动作发生处 SHALL 在动作时拉取当前值（与压缩预算同构的读取时机）；零值 SHALL 保持现行为（向后兼容）；回滚 SHALL 随源同携新字段恢复旧值。生效语义：变更后下一次摘要动作起用新值，在途动作不回溯，存量摘要产物不重写。

#### Scenario: 热变更下一次摘要即生效

- **WHEN** numeric-only 发布修改摘要 knob，随后发生一次摘要动作
- **THEN** 该动作使用新值，且诊断回执/日志携带新旧值可见

#### Scenario: 零值兼容与回滚恢复

- **WHEN** 配置未设置摘要 knob（零值），或执行 Rollback
- **THEN** 零值时行为与引入前逐位相同；回滚后摘要动作使用回滚目标的旧值
