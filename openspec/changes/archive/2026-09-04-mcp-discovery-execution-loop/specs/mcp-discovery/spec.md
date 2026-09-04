# mcp-discovery Specification (Delta)

## ADDED Requirements

### Requirement: 基于实时注册表的发现
`mcp_discover` SHALL 从注入的 `MCPRegistry` 在每次调用时实时读取 server 集合(而非构建期快照),对每个 server 经 `Tools(ctx)` 拉取工具并按 query 与 name/description 做大小写不敏感匹配。运行时注册的 server MUST 无需任何重建即可被发现。

#### Scenario: 运行时新注册 server 立即可发现
- **WHEN** 经注册表 Add 新 server 后调用 `mcp_discover(query=<其工具名>)`
- **THEN** 返回结果包含该 server 的匹配工具

#### Scenario: 移除的 server 不再出现
- **WHEN** 注册表 Remove 某 server 后以其工具名查询
- **THEN** 返回结果不含该 server 的任何工具

### Requirement: 如实的调用指引
发现结果的 content SHALL 包含真实可执行的调用指引 `mcp_call(server="<toolset 名>", tool="<工具名>", args={...})` 及该工具的 InputSchema JSON;MUST NOT 再输出"经 command/exec 调用"的指引。source 字段保持 `mcp:<toolset 名>` 格式。

#### Scenario: 发现结果携带 mcp_call 指引与 schema
- **WHEN** `mcp_discover` 命中某工具
- **THEN** 该结果 content 包含 `mcp_call(server=` 指引与 InputSchema JSON,且不包含 `command(mode="exec"` 字样

### Requirement: 空注册表与无匹配行为
注册表为空或无匹配时,`mcp_discover` SHALL 返回空结果集(count=0)而非错误;单个 server 拉取工具失败 MUST NOT 中断对其余 server 的发现。

#### Scenario: 空注册表返回空结果
- **WHEN** 无任何 server 注册时调用 `mcp_discover`
- **THEN** 返回 `tools=[], count=0`,无错误

#### Scenario: 单 server 故障不中断整体发现
- **WHEN** 两个 server 中一个连接失败
- **THEN** 另一个 server 的匹配工具仍正常返回
