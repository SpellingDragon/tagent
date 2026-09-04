# mcp-server-registry Specification (Delta)

## ADDED Requirements

### Requirement: 声明式 MCP server 配置
`Config` SHALL 支持顶层 `mcp_servers` 映射(name → MCPServerConfig),字段包含 `transport`、`url`、`headers`、`api_key_env`、`command`、`args`、`timeout`。配置校验 SHALL 在 transport 归一化后执行:`sse`/`streamable` 必填 `url`,`stdio` 必填 `command`,非法 transport 报错。

#### Scenario: 合法 streamable-http 配置通过校验
- **WHEN** YAML 声明 `transport: streamable-http` 且提供 `url`
- **THEN** `LoadConfig` 成功,server 进入 `Config.MCPServers`

#### Scenario: 缺失 url 的远程 transport 校验失败
- **WHEN** YAML 声明 `transport: sse` 但未提供 `url`
- **THEN** `Config.Validate()` 返回包含 server 名与缺失字段的错误

#### Scenario: 非法 transport 校验失败
- **WHEN** YAML 声明 `transport: websocket`
- **THEN** `Config.Validate()` 返回不支持 transport 的错误

### Requirement: transport 归一化
注册表创建 toolset 时 SHALL 将 `streamable-http`、`streamable_http`、`http` 归一化为 trpc 认可的 `streamable`;`stdio`/`sse`/`streamable` 保持原样。

#### Scenario: 连字符写法归一化
- **WHEN** server 配置 `transport: streamable-http`
- **THEN** 创建的 `mcp.ConnectionConfig.Transport` 为 `streamable`

### Requirement: 认证 header 组装
当配置 `api_key_env` 时,注册表 SHALL 在创建 toolset 时读取该环境变量并组装 `Authorization: Bearer <value>` header;显式 `headers` 与之叠加且同名时显式值优先。环境变量缺失 MUST NOT 阻断注册(错误在惰性连接使用时渗透)。

#### Scenario: api_key_env 生成 Bearer header
- **WHEN** 配置 `api_key_env: ZHIPU_CODING_PLAN_API_KEY` 且该环境变量已设置
- **THEN** toolset 连接配置包含 `Authorization: Bearer <该值>`

#### Scenario: 环境变量缺失不阻断注册
- **WHEN** `api_key_env` 指向的环境变量未设置
- **THEN** server 仍注册成功,注册表可 List 到该 server

### Requirement: 运行时注册表
系统 SHALL 提供并发安全的 `MCPRegistry`:`Get(name)`、`List()`、`Names()`(输出按名排序)、`Add(name, toolset)`、`Remove(name)`(Remove 时关闭对应 toolset)。注册表操作 MUST NOT 改变任何 agent 的工具声明集合。

#### Scenario: 运行时新增 server 即时可见
- **WHEN** 经 Go API `Add` 注册新 toolset 后调用 `List()`
- **THEN** 返回包含新 server,且无需重建任何 agent

#### Scenario: 移除 server 关闭连接
- **WHEN** 调用 `Remove(name)`
- **THEN** 对应 toolset 的 `Close()` 被调用且 `Get(name)` 返回不存在

### Requirement: 配置文件热同步
注册表 SHALL 在 `Get/List/Names` 入口惰性检查配置文件 mtime;变化时重新解析 `mcp_servers` 段并 diff-apply:新增项 Add、删除项 Close+Remove、字段变更项 Close 旧实例后重建。解析失败 SHALL 保留现有注册表内容。

#### Scenario: 配置新增 server 热生效
- **WHEN** 向 tagent.yaml 的 `mcp_servers` 添加新 server 并保存,随后调用 `List()`
- **THEN** 返回包含新 server,进程未重启

#### Scenario: 配置删除 server 热移除
- **WHEN** 从 tagent.yaml 删除某 server 并保存,随后调用 `Names()`
- **THEN** 该 server 不再出现,且其 toolset 已被关闭

#### Scenario: 配置解析失败保持现状
- **WHEN** tagent.yaml 被写入非法 YAML 后调用 `List()`
- **THEN** 返回变更前的 server 集合,不 panic 不清空

### Requirement: 构建期装配与生命周期
`tagent.New()` SHALL 从 `Config.MCPServers` 构建注册表并经 `PlainToolFactoryConfig.MCPRegistry` 注入工具工厂;`WithMCPToolSets` 注入的 toolset SHALL 以其 `Name()` 并入注册表;注册表 SHALL 实现 `Closer` 并在 entry agent 上注册,进程关闭时统一关闭全部 toolset(幂等)。

#### Scenario: WithMCPToolSets 兼容并入
- **WHEN** 经 `WithMCPToolSets` 注入名为 mock 的 toolset 且 YAML 无 `mcp_servers`
- **THEN** 注册表 `Get("mock")` 命中该 toolset

#### Scenario: 关停统一释放
- **WHEN** entry agent `Close()` 被调用
- **THEN** 注册表内全部 toolset 的 `Close()` 各被调用一次
