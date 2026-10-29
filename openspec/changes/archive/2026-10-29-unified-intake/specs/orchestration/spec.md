# Orchestration 契约（只保留编排与验收类；规范正文在各域 delta）

## ADDED Requirements

### Requirement: 三段验收

孙任务 SHALL exit 0 且带正/负/消费者三锚；子域 Scenario 逐条映射测试；变更级 DoD SHALL 含症状锚红转绿与回滚即红双证据。

#### Scenario: 回滚即红

- **WHEN** 完整回滚本变更复跑症状锚
- **THEN** 两条直译测试 SHALL 红
