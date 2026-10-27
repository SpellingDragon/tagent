# Orchestration 契约

## ADDED Requirements

### Requirement: 旧路径零残留

迁移 SHALL 使旧顶层包路径在 Go 源、CI、门禁基线与文档中零残留（结构负锚）。

#### Scenario: 旧路径残留即红

- **WHEN** 任一 `tagent/{telemetry,evolution,rl}"` import 或基线键残留
- **THEN** 结构断言 SHALL 红

### Requirement: CI 终态确认

收尾 SHALL 本地全套门禁绿后推送，并确认远端 CI 全绿后方可归档。

#### Scenario: 远端未绿不归档

- **WHEN** 远端 CI 任一 job 红
- **THEN** F6 SHALL NOT 勾选
