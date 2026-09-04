# mcp-call-tool Specification

## Purpose

本规范定义 mcp-call-tool 能力:通用 MCP 执行网关工具 `mcp_call`。其声明(server/tool/args 三参)SHALL 恒定不变,与注册表内容完全解耦——持有它的 agent 保持字节稳定的 tools 前缀,而可达的 MCP 工具面在注册表层自由变化。失败以结果(nil error)形式返回自纠材料,供模型修正后重试(渗透式发现-执行闭环的执行端)。

## Requirements

### Requirement: 恒定声明的通用执行网关
系统 SHALL 提供内置 plain tool `mcp_call`,注册于 `RegisterBuiltinTools`,其 InputSchema 固定为三参:`server`(string, required)、`tool`(string, required)、`args`(object, 自由形态)。该声明 MUST NOT 随注册表内容变化。挂载遵循 tagent 配置规范:由各 agent 的 YAML `tools:` 声明决定持有者,框架不硬编码归属。

#### Scenario: 内置注册可被 YAML 引用
- **WHEN** agent 的 YAML tools 声明 `kind: tool, id: mcp_call`
- **THEN** `ValidateToolAccess` 通过且 agent 构建成功

#### Scenario: 声明与注册表内容解耦
- **WHEN** 注册表中 server 集合发生增删
- **THEN** `mcp_call` 的 `Declaration()` 输出不变

### Requirement: 注册表解析与直调
`mcp_call` SHALL 按 `server` 从注册表解析 toolset,经 `Tools(ctx)` 按 `Declaration().Name` 精确匹配 `tool`,并以 `args` 的 JSON 序列化调用目标 `CallableTool.Call`,透传其返回值。

#### Scenario: 成功调用远程 MCP 工具
- **WHEN** 调用 `mcp_call(server="web-search-prime", tool="web_search_prime", args={"search_query": "golang"})` 且该 server/tool 存在
- **THEN** 返回目标工具的调用结果

### Requirement: 自纠错误反馈
调用失败时 `mcp_call` SHALL 返回可供模型自纠的结构化错误:未知 server 时携带注册表 `Names()` 清单;未知 tool 时携带该 server 的工具名清单;目标工具调用出错时附带该工具的 InputSchema JSON 回显。

#### Scenario: 未知 server 返回可用清单
- **WHEN** 调用 `mcp_call(server="not-exist", ...)`
- **THEN** 错误信息包含当前已注册的全部 server 名

#### Scenario: 未知 tool 返回该 server 工具清单
- **WHEN** server 存在但 `tool` 名不匹配任何工具
- **THEN** 错误信息包含该 server 下全部工具名

#### Scenario: 调用失败回显 InputSchema
- **WHEN** 目标工具因参数错误返回失败
- **THEN** 错误信息附带该工具的 InputSchema JSON

### Requirement: 空注册表行为
注册表为空(未配置 `mcp_servers` 且无运行时注册)时,`mcp_call` SHALL 返回明确的"无可用 MCP server"错误而非 panic;工厂创建 MUST 成功(与 `mcp_discover` 空 stub 行为对齐)。

#### Scenario: 空注册表调用得到明确错误
- **WHEN** 无任何 server 注册时调用 `mcp_call`
- **THEN** 返回提示无可用 server 的错误,进程无异常
