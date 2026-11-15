## ADDED Requirements

### Requirement: godoc 与消费者文档预算键归位

预算字段归位后，生成 godoc（`docs/api/*.md`）SHALL 反映 AgentSpec 新字段名（`ReplayWindow`/`ContextWindow`/输出限字段），MUST NOT 残留 `OutputReserve`/`Params`。消费者文档（`CHANGELOG.md`、`examples/wechat-bot/README.md`、`docs/wiki/platform/org-hot-reload.md`）涉预算键处 SHALL 同步新键与派生语义。文档随代码走，脱节即修；冻结归档 MUST NOT 改写。

#### Scenario: 旧键零残留

- **WHEN** 归位落地后 grep `output_reserve`/`provider_context_window` 于 docs/ 与 examples/
- **THEN** SHALL 零残留（冻结归档除外）

#### Scenario: godoc 反映新字段

- **WHEN** 重生成 docs/api/*.md
- **THEN** SHALL 含 ReplayWindow/ContextWindow/输出限字段注释，MUST NOT 含 OutputReserve/Params