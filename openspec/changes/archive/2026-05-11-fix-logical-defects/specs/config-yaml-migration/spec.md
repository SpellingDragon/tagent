## ADDED Requirements

### Requirement: 提供 YAML 配置迁移指南文档
项目 SHALL 提供 `docs/config-migration.md`，帮助用户从旧的扁平 Config 格式迁移到新的 agent-centric Config 格式。

#### Scenario: 迁移文档包含新旧格式对比
- **WHEN** 用户阅读 `docs/config-migration.md`
- **THEN** 文档 SHALL 包含旧格式（`name`, `model`, `tools`）与新格式（`agents`, `entry`）的完整示例对比表
- **AND** 逐字段说明映射关系

#### Scenario: 迁移文档包含 AgentConfig 各字段说明
- **WHEN** 用户需要配置新格式
- **THEN** 文档 SHALL 列出 AgentConfig 的所有字段及其含义、类型、默认值

#### Scenario: 迁移文档可被项目 README 链接
- **WHEN** 文档已创建
- **THEN** 文件名 SHALL 为 `config-migration.md`
- **AND** 存放于项目 `docs/` 目录
