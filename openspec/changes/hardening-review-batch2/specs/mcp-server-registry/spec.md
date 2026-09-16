# mcp-server-registry Delta

## MODIFIED Requirements

### Requirement: 配置文件热同步
registry 监听的配置文件 MUST 支持完整项目配置形态（含 entry / agents / providers / mcp_servers 等根字段）：热同步解析 MUST 仅对 mcp_servers 子树执行严格解码，完整文件中的其他合法根字段 MUST NOT 被判为 unknown 而导致同步失败；解析失败时保留旧 registry 并告警的行为不变。

#### Scenario: 修改完整配置中的 MCP 声明
- **WHEN**运维在真实项目配置文件（含 agents/providers 等根字段）中新增一个 MCP server 声明并保存
- **THEN** 热同步 MUST 成功识别变更并更新 registry，MUST NOT 因根字段被判 unknown 而静默保留旧表
