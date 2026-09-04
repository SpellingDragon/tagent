# MCP Discovery-Execution Loop

## Why

tagent 当前的 MCP 集成只有"发现"没有"执行":`WithMCPToolSets` 注入的 toolset 仅供 `mcp_discover` 列举,发现结果却谎称"可经 exec 调用"——对 streamable-http 远程 MCP(如智谱 web-search-prime)shell 根本调不通;且 server 集合是构建期静态切片,无法运行时增删。若按 trpc-agent-go 原生方式把 MCP 工具展开为原生声明,工具集任何变化都会使 prompt 前缀(tools 区)整体失效,摧毁前缀缓存。需要一条与"knowledge 发现、action 执行"原始设计一致、对前缀缓存零影响的 MCP 直调路径:工具知识按需渗透进上下文(append-only 消息),而非常驻声明区或每 turn 重渲染的看板。

## What Changes

- 新增 MCP server 注册表(`MCPRegistry`):并发安全的 name→ToolSet 表,支持 Add/Remove/List、统一 Close;由顶层 YAML `mcp_servers` 声明填充,并基于配置文件 mtime 惰性热同步(增/删/改 server 即时生效,零重启、零声明区影响)。
- 新增内置 plain tool `mcp_call`:声明恒定的通用 MCP 执行网关(参数 `server`/`tool`/`args`),从注册表解析目标工具并直调;未知 server/tool 与参数校验失败时返回带可用清单/InputSchema 的详细错误供模型自纠。遵循 tagent 配置规范——由 YAML 决定挂载到哪个 agent(典型为 action;knowledge 亦可按需声明)。
- 改造 `mcp_discover`:改为持注册表、调用时实时读取 server 集合;修正输出文案为如实的 `mcp_call(server=..., tool=..., args=...)` 调用指引(含 InputSchema)。
- 配置结构:`Config` 新增顶层 `MCPServers map[string]MCPServerConfig`(transport/url/headers/api_key_env/command/args/timeout);transport 归一化兼容 `streamable-http`/`streamable_http` 写法;`api_key_env` 运行时读环境变量拼 `Authorization: Bearer`。
- 生命周期与兼容:注册表实现 `Closer`,在 entry agent 上注册统一关闭;`WithMCPToolSets` 注入的 toolset 以其 `Name()` 并入注册表(向后兼容,`mcp_discover`/`mcp_call` 只读注册表单一来源)。
- 依赖:引入 `trpc.group/trpc-go/trpc-agent-go/tool/mcp`(传递依赖 `trpc-mcp-go v0.0.10`,与 trpc-agent-go v1.10.0 对齐)。
- 示例落地(wechat-bot):YAML 声明 `web-search-prime` server;knowledge agent 以 `mcp_call` 替代 `web_search`(旧实现代码保留可回退,`duckduckgo_search` 作无 key 回退);action agent 补挂 `mcp_call`;`knowledge_agent.md` 更新联网搜索调用指引。
- 明确不做:MCP 工具不展开为任何 agent 的原生声明;无 pinned-tail 能力看板;无注册公告事件(主 agent 经 desc prompt 知晓 knowledge 有发现能力、action 有执行能力);无 per-agent MCP 授权(agent 边界即控制面:knowledge 只读发现,持 `mcp_call` 者方可执行)。

## Capabilities

### New Capabilities

- `mcp-server-registry`: MCP server 的声明式注册与运行时生命周期——YAML `mcp_servers` 解析、transport 归一化、认证 header 组装、惰性连接、基于配置 mtime 的热同步(diff 增删改)、统一关闭、`WithMCPToolSets` 兼容并入。
- `mcp-call-tool`: 通用 MCP 执行网关工具——恒定声明(server/tool/args 三参)、注册表解析与直调、未知目标/调用失败时的自纠错误反馈(可用 server/tool 清单、InputSchema 回显)。
- `mcp-discovery`: 基于实时注册表的 MCP 工具发现——按 query 匹配 name/description、输出如实的 mcp_call 调用指引与 InputSchema、注册表为空时的空结果行为。

### Modified Capabilities

<!-- 无:unified-tool-registry 的注册/查找/校验需求不变(mcp_call 只是新增一个内置注册项);现有 mcp_discover 行为此前无规格,其更新后契约由新 mcp-discovery 规格承载 -->

## Impact

- 代码:`config.go`(MCPServerConfig/MCPServers/校验)、`tagent.go`(注册表构建与注入、entry Closer 注册)、`registry.go`(注册 mcp_call)、`agent/tool_agent.go`(PlainToolFactoryConfig 增注册表字段)、`tool/accessor.go`(MCPRegistry 接口)、新增 `tool/mcp/`(注册表实现 + mcp_call)、`tool/knowledge/knowledge_subtools.go`(mcp_discover 改造与文案修正)。
- 依赖:`go.mod` 新增 `trpc-mcp-go v0.0.10`(经 trpc-agent-go/tool/mcp)。
- 示例与提示词:`examples/wechat-bot/tagent.yaml`、`resources/prompts/knowledge_agent.md`(及 knowledge_tool_desc.md 轻量提及发现能力)。
- 运行环境:示例需设置 GLM Coding Plan 专属 key(如 `ZHIPU_CODING_PLAN_API_KEY`,与平台 `ZAI_API_KEY` 不通用)。
- 缓存行为:所有 agent 声明区保持恒定;MCP 增删只影响发现/调用结果内容,prompt 前缀零失效。
