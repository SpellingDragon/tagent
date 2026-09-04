# Tasks: MCP Discovery-Execution Loop

## 1. 依赖与配置层

- [x] 1.1 `go get trpc.group/trpc-go/trpc-mcp-go@v0.0.10` + `go mod tidy`,审查 go.mod/go.sum diff(与 trpc-agent-go v1.10.0 对齐)
- [x] 1.2 config.go 新增 `MCPServerConfig` 结构体(transport/url/headers/api_key_env/command/args/timeout)与 `Config.MCPServers map[string]MCPServerConfig`(yaml `mcp_servers`)
- [x] 1.3 `Config.Validate()` 增加 mcp_servers 校验:transport 归一化后校验合法性;sse/streamable 必填 url;stdio 必填 command
- [x] 1.4 config 测试:YAML 解析 mcp_servers、三类校验失败场景(缺 url、缺 command、非法 transport)——见 mcp_wiring_test.go

## 2. 注册表(tool/mcp 包)

- [x] 2.1 tool/accessor.go 定义 `MCPRegistry` 接口:`Get(name) (trpctool.ToolSet, bool)`、`List() []trpctool.ToolSet`、`Names() []string`
- [x] 2.2 新建 tool/mcp 包:并发安全 Registry 实现(map + Mutex),Add/Remove(Remove 时 Close toolset)/Close(幂等关全部),List/Names 按名排序
- [x] 2.3 实现 `NormalizeTransport`(streamable-http/streamable_http/http → streamable;stdio/sse/streamable 原样)+ 表驱动单测
- [x] 2.4 实现 server 配置 → trpc `mcp.NewMCPToolSet` 构造:归一化 transport、api_key_env → `Authorization: Bearer` header(显式 headers 同名优先)、timeout 解析、`mcp.WithName(name)`;env 缺失不阻断注册
- [x] 2.5 实现配置文件热同步:注册表持有配置路径,Get/List/Names 入口惰性 mtime 检查 → 重解析 mcp_servers 段 → diff-apply(新增 Add/删除 Close+Remove/变更重建);解析失败保留现状
- [x] 2.6 Registry 单测:Add/Remove/Close 生命周期、排序确定性、热同步场景(新增/删除/变更重建/坏 YAML 保持现状/manual 条目存活/Seed mtime 基线)、并发安全(go test -race 通过)

## 3. mcp_call 网关工具

- [x] 3.1 tool/mcp 包实现 `mcp_call` CallableTool:恒定三参 InputSchema(server/tool required, args object);调用流 registry.Get → Tools(ctx) → 名称精确匹配 → Call(marshal(args))
- [x] 3.2 实现自纠错误:未知 server 附 Names() 清单;未知 tool 附该 server 工具名清单;调用失败附目标工具 InputSchema JSON;空注册表返回明确"无可用 server"错误
- [x] 3.3 mcp_call 单测:成功直调、三类自纠错误、空注册表、args 透传/空 args 默认 {}、声明恒定性(注册表增删前后 Declaration 不变)

## 4. 装配与注入

- [x] 4.1 agent/tool_agent.go `PlainToolFactoryConfig` 新增 `MCPRegistry` 字段(保留 MCPToolSets,注册表优先);评审加项:`ToolAgentFactoryConfig` 同步补 `MCPRegistry`(自定义 tool agent 一致性)
- [x] 4.2 registry.go `RegisterBuiltinTools` 注册 `mcp_call` 工厂(空注册表时工厂仍创建成功)
- [x] 4.3 tagent.go `New()`:从 cfg.MCPServers 构建注册表(LoadConfig 记录 ConfigPath 以启用热同步);`WithMCPToolSets` 的 toolset 以 Name() 并入;`buildPlainToolRef` nil-guard 注入;entry agent `RegisterCloser(registry)`
- [x] 4.4 装配测试:buildPlainToolRef 注入验证(mcp_call 错误清单含注册表 server)、无注册表 stub 行为、mcp_discover 工厂消费实时注册表;WithMCPToolSets 兼容经 tests/TestTagentNew_Success 覆盖

## 5. mcp_discover 改造

- [x] 5.1 knowledge_subtools.go:`NewMCPDiscoverToolWithRegistry`(每次调用实时 List)+ 工厂注册表优先,保留旧 MCPToolSets 路径作兼容回退
- [x] 5.2 修正发现文案:`Callable via mcp_call(server=..., tool=..., args={...})` + InputSchema;移除 "via command(mode=exec)" 表述(Search agent 全库清扫确认无遗留)
- [x] 5.3 discover 单测:运行时 Add/Remove 即时可见、文案断言(含 mcp_call 指引、不含 command(mode="exec"))、单 server 空/故障不中断、空注册表空结果;集成验证发现即修:朴素连续子串匹配漏配自然语言 query("web search" 不中 web_search_prime)→ 增加分词 AND 回退匹配 mcpQueryMatches + TestMCPDiscover_NaturalLanguageQuery

## 6. 示例与提示词落地(独立提交,可回退)

- [x] 6.1 examples/wechat-bot/tagent.yaml:顶层加 `mcp_servers.web-search-prime`(streamable-http;按用户确认直接复用 ZAI_API_KEY——zhipu provider 即 coding 端点,同一把 key);knowledge tools 移除 web_search、新增 mcp_call;action agent 补挂 mcp_call
- [x] 6.2 实测完成(临时 probe,复用 testutil 的 ~/.zshrc key 提取):真实工具名为 `web_search_prime`(下划线,非文档风格 webSearchPrime,prompt/yaml 已据实修正);InputSchema 核实(search_query required + content_size/location/search_domain_filter/search_recency_filter 可选);并经 CallableTool.Call(mcp_call 同路径)真实调用一次搜索成功返回结果
- [x] 6.3 resources/prompts/knowledge_agent.md:Job A 增加 "Web search (via MCP)" 小节(mcp_call 用法/自纠/duckduckgo 回退/mcp_discover 长尾发现);knowledge_tool_desc.md 补 MCP 发现能力一条
- [x] 6.4 端到端验证(以 tests/mcp_llm_test.go 两级集成测试固化):工具层 TestMCPIntegration_DiscoverAndCall(真网络:发现指引如实/直调返回真实搜索结果/错误自纠携正确清单)✅;LLM 层 TestRealLLM_KnowledgeMCPSearchFlow(真实 glm-4.7 + 真实 knowledge_agent.md:6/6 次正确路由 server/tool、search_query 非空、自发使用 content_size 可选参、返回含项目主页链接)✅;热改免重启(HotSync 单测)与关停释放(Close 幂等 + RegisterCloser 装配)已由单测覆盖;wechat-bot 微信通道本身与 MCP 链路无关

## 7. 收尾

- [x] 7.1 `go build ./...` + `go vet ./...` 全绿;`go test -race` 通过:root 包、tool/mcp、tool/knowledge;全量 `go test ./...`:除 tests/ 包 3 个 real-LLM 契约测试外全绿——该 3 例经 Debug agent 诊断为 pre-existing flaky(LLM 行为抖动;零代码交集/失败点跨运行漂移/git 历史三重证据),非本变更回归
- [x] 7.2 复核缓存不变量:Search agent 全库审计通过(无任何运行时改变 agent 声明集合的路径;无 AddToolSet/SetToolSets 调用;mcp_call/mcp_discover Declaration 与注册表解耦)+ Declaration 恒定性单测;CodeReview agent 评审通过(无必须修复缺陷)
