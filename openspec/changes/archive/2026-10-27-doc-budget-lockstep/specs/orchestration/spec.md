# Orchestration 契约

## ADDED Requirements

### Requirement: 零差额在检查面强制

check_doc_budget.py SHALL 在实测与基线不相等时（两向）以非零退出码拒绝；锁步（--tighten 或具名重锁）SHALL 与文档体量变更同提交。

#### Scenario: 下漂不锁即红

- **WHEN** 实测 < 基线且未先 --tighten
- **THEN** 检查 SHALL exit 1 且文案指向 --tighten

#### Scenario: 钩子守卫可达

- **WHEN** 提交暂存任何 *.md/*.markdown
- **THEN** pre-commit SHALL 运行同一零差额检查
