# Orchestration 契约

## ADDED Requirements

### Requirement: 静默红清零

清理 SHALL 使 smoke 全套（tmux 真实执行）与 race（无 U5 豁免）、结构门全部转绿。

#### Scenario: 全套 smoke 绿

- **WHEN** `go -C examples/wechat-bot test -tags smoke . -count=1`
- **THEN** SHALL exit 0
