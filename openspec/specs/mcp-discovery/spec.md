# mcp-discovery Specification

## Purpose

本规范定义 mcp-discovery 能力:基于实时注册表的 MCP 工具发现(`mcp_discover`)。发现结果 SHALL 携带如实可执行的 `mcp_call` 调用指引与 InputSchema——工具知识按需以工具结果消息渗透进上下文,而非常驻声明区(渗透式发现-执行闭环的发现端)。匹配 SHALL 对 LLM 实际发出的自然语言 query 友好(分词回退)。

## Requirements

### Requirement: 基于实时注册表的发现
`mcp_discover` SHALL 从注入的 `MCPRegistry` 在每次调用时实时读取 server 集合(而非构建期快照),对每个 server 经 `Tools(ctx)` 拉取工具并按 query 与 name/description 做大小写不敏感匹配;朴素子串不中时 SHALL 回退分词 AND 匹配(按空格/下划线/连字符切词,全 token 命中 name+description 联合文本即匹配)。运行时注册的 server MUST 无需任何重建即可被发现。

#### Scenario: 运行时新注册 server 立即可发现
- **WHEN** 经注册表 Add 新 server 后调用 `mcp_discover(query=<其工具名>)`
- **THEN** 返回结果包含该 server 的匹配工具

#### Scenario: 移除的 server 不再出现
- **WHEN** 注册表 Remove 某 server 后以其工具名查询
- **THEN** 返回结果不含该 server 的任何工具

#### Scenario: 自然语言 query 分词匹配
- **WHEN** 以空格分隔的自然语言 query(如 "web search")查询下划线命名的工具(如 web_search_prime)
- **THEN** 分词 AND 回退使其命中,返回该工具

### Requirement: 如实的调用指引
发现结果的 content SHALL 包含真实可执行的调用指引 `mcp_call(server="<toolset 名>", tool="<工具名>", args={...})` 及该工具的 InputSchema JSON;MUST NOT 输出"经 command/exec 调用"的指引。source 字段保持 `mcp:<toolset 名>` 格式。

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
