# Design: MCP Discovery-Execution Loop

## Context

- tagent 遵循"knowledge 发现、action 执行"的原始设计:主 agent 声明区(tools 前缀)恒定,动态能力以工具结果消息渗透进上下文(append-only,前缀缓存友好)。`ExecutionPlan.Function` 早已预留 `"mcp_call"` 枚举([knowledge_subtools.go:29](../../../tool/knowledge/knowledge_subtools.go)),但从未实现。
- 现状缺口:`rc.mcpToolSets` 为构建期静态切片,仅供 `mcp_discover` 列举;发现文案谎称"Callable via command(mode=exec)",对 streamable-http 远程 MCP 不成立;无任何直调路径;server 集合不可运行时增删。
- 上游能力已核实:`trpc-agent-go/tool/mcp` 的 `mcpTool` 是完整 `CallableTool`;`ToolSet.Tools(ctx)` 每次调用会 listTools(server 内部工具变化天然实时);`validateTransport` 只认 `stdio/sse/streamable|streamable_http`;openai 适配器按工具名排序保证声明区 token 稳定。
- 缓存约束(本设计的第一性约束):tools 声明渲染在 prompt 最前部,任何声明集变化使前缀缓存全灭;压缩与 prompt 热载造成的历史区变更为已接受的合法变更。
- 相关既有机制:`prompt.Source`(mtime 惰性热载)、`PlainToolFactoryConfig` 运行时依赖注入、`Closer`/`RegisterCloser` 生命周期、`sync.Once` 幂等内置注册。

## Goals / Non-Goals

**Goals:**

- MCP server 集合可声明(YAML `mcp_servers`)、可运行时增删(配置 mtime 热同步 + Go API),全程零声明区影响、零重启。
- 提供如实可用的执行路径:`mcp_call` 通用网关(声明恒定),`mcp_discover` 输出真实调用指引。
- 每个 agent(主/knowledge/action 一致,同为 TagentAgent)的 prompt 前缀保持恒定。
- 示例落地:wechat-bot 的 knowledge agent 以 zhipu web-search-prime MCP 替代 `web_search`(可回退)。

**Non-Goals:**

- 不把 MCP 工具展开为任何 agent 的原生 function-calling 声明(缓存敌对,拒绝)。
- 不做 pinned-tail 能力看板(能力清单低频变更、过时代价低,不配每 turn 重渲染)。
- 不做注册公告事件(主 agent 经 desc prompt 知晓 knowledge/action 分工即可)。
- 不做 per-agent MCP 授权/白名单(agent 边界即控制面:knowledge 只读发现,持 `mcp_call` 者方可执行)。
- 不做其余配置段(model 参数、agents 图等)的通用热加载——另行变更。
- 不调整 wechat-bot 主 agent 的编排(action agent 复位为主 agent 工具的问题独立决策)。

## Decisions

### D1 渗透式取代声明式/看板式(能力信息的存在方式)

| 备选 | 缓存影响 | 结论 |
|---|---|---|
| A. 原生声明展开(trpc `WithToolSets`/`SetToolSets`) | 增删即前缀全灭;`NamedToolSet` 还会改写工具名 | 拒绝 |
| B. pinned-tail 看板(仿 live task board) | 前缀稳定但每 turn 烧 token 维持"全局在场" | 拒绝 |
| C. 发现渗透(本设计) | 工具知识按需以消息进入历史,append-only,一次发现长期复用,可压缩/召回 | **采用** |

判据:变更频率 × 过时代价。任务状态(高频、过时危险)配看板;能力清单(低频、过时廉价)配发现式。上下文本身即"已发现能力"的缓存。

### D2 全局单注册表,接口/实现分层

- 接口 `MCPRegistry` 定义在 `tool/accessor.go`(与 `SkillRepository` 同址):`Get(name) (trpctool.ToolSet, bool)`、`List() []trpctool.ToolSet`、`Names() []string`。
- 实现在新包 `tool/mcp`(内部 import trpc `tool/mcp`,别名区分):并发安全 map + `Add/Remove/Close`,`List/Names` 输出按 name 排序(保证 discover 输出与错误清单的确定性)。
- 注入沿用既有模式:`agent.PlainToolFactoryConfig` 新增 `MCPRegistry tagenttool.MCPRegistry` 字段,`buildPlainToolRef` 注入 `rc.mcpRegistry`。
- 依赖方向不变:tagent(root) → tool/mcp → memory 无涉;agent → tool 接口,无环。
- 备选"per-agent 注册表切片"被拒:授权由 agent 边界承担,全局单表最简。

### D3 `mcp_call`:声明恒定的通用执行网关

- InputSchema 三参:`server`(string, required)、`tool`(string, required)、`args`(object, 自由形态)。声明永不随注册表内容变化。
- 调用流:`registry.Get(server)` → `ts.Tools(ctx)` → 按 `Declaration().Name == tool` 匹配 → `CallableTool.Call(ctx, marshal(args))`。
- 自纠错误(渗透哲学的一部分——错误信息就是模型的学习材料):
  - 未知 server → 错误携带 `Names()` 清单;
  - 未知 tool → 错误携带该 server 的工具名清单;
  - 调用失败 → 错误附带目标工具 InputSchema JSON 回显,供模型修正 args 重试。
- 挂载遵循 tagent 配置规范:`mcp_call` 只是注册进 `RegisterBuiltinTools` 的普通 plain tool,由各 agent 的 YAML `tools:` 决定谁持有(示例:knowledge 与 action)。框架不硬编码归属。
- 备选"每 server 生成一个网关工具"被拒:server 增删又会改声明区,回到缓存问题。

### D4 配置热同步:mtime 惰性检查(与 `prompt.Source` 同型)

- 注册表持有配置文件路径;`Get/List/Names` 入口先做 mtime 检查,变化则重新解析 `mcp_servers` 段并 diff-apply:新增 → Add;删除 → Close+Remove;字段变更 → Close 旧 + 重建。
- 触发点选惰性而非后台 watcher goroutine:与 `prompt.Source` 机制同构;不用 MCP 时过期无害;Add 本身无 I/O(惰性连接),同步开销可忽略。
- 解析失败 → 保留现有注册表(graceful degradation,同 `prompt.Source`)。
- 生效语义遵循用户决策:内容变更(registry 增删)即时热载生效,mid-turn 亦可——它不触碰 tagent 结构与声明区;无需 turn 边界冻结。
- 另暴露 Go API(`Add/Remove`)供程序化注册(测试、宿主应用)。

### D5 连接策略:惰性连接,放弃启动 fail-fast

- 不在启动期 `Init()` 预连接。server 可达性是运行时事实:`mcp_discover`/`mcp_call` 调用时经 `ts.Tools(ctx)` 触发连接,失败以工具错误形式渗透进上下文,模型可改道(如 fallback 到 duckduckgo_search)。
- 与早期静态方案的 fail-fast 相反,理由:热同步语义下"配置写入时刻"与"能力可用时刻"解耦,启动期阻断与动态注册哲学冲突;且 trpc toolset 自带失败回退缓存与重连模式。

### D6 配置结构与归一化

```yaml
mcp_servers:
  web-search-prime:
    transport: streamable-http          # 归一化: streamable-http|streamable_http|http → streamable
    url: https://open.bigmodel.cn/api/mcp/web_search_prime/mcp
    api_key_env: ZHIPU_CODING_PLAN_API_KEY   # 运行时 → Authorization: Bearer <env>
    # headers: {...}                    # 显式 header,与 api_key_env 叠加(显式优先)
    # timeout: 30s                      # duration 字符串
    # command/args:                     # stdio transport 专用
```

- `Config.Validate()`:sse/streamable 必填 `url`;stdio 必填 `command`;transport 非法值报错(在归一化之后校验)。
- `api_key_env` 在**创建 toolset 时**读取环境变量组装 header;env 缺失时仍注册(惰性连接会在使用时以 401 类错误渗透),不阻断启动。

### D7 兼容与单一来源

- `WithMCPToolSets` 保留:注入的 toolset 在 `New()` 中以 `Name()` 并入注册表。
- `mcp_discover` 工厂改为消费 `cfg.MCPRegistry`(替代静态 `MCPToolSets` 切片);`PlainToolFactoryConfig.MCPToolSets` 字段保留一个版本周期但注册表优先。
- `mcp_discover` 输出文案修正为:`Callable via mcp_call(server="<ts.Name()>", tool="<decl.Name>", args={...})` + InputSchema(替换现有 "via command(mode=exec)" 谎言)。

### D8 生命周期

- 注册表实现 `Close() error`(关闭全部 toolset,幂等);`tagent.New()` 在 entry agent 构建成功后 `entryAgent.RegisterCloser(registry)`——与 TrajectoryRecorder 同位置、同模式,进程级注册一次。

### D9 示例落地(wechat-bot)

- YAML:顶层加 `mcp_servers.web-search-prime`;knowledge 移除 `web_search`、新增 `mcp_call`(保留 `mcp_discover`、`duckduckgo_search`);action agent 补挂 `mcp_call`。
- `knowledge_agent.md`:Job A 联网搜索指引改为 `mcp_call(server="web-search-prime", tool="webSearchPrime", args={"search_query": ...})`,并保留 "skills first, web second" 与 fallback 次序(mcp 失败 → duckduckgo_search)。核心工具的"pinned 知识"由热载 prompt 承载——能力更新改 prompt 即可,无需动声明区。
- `websearch.go` 与 `web_search` 工厂注册**保留不删**:YAML 加回一行即回退。

## Risks / Trade-offs

- [精度损失:`args` 无模型层 schema 强制,错误后置到调用时] → mcp_call 失败回显 InputSchema + 可用清单,依赖模型自纠重试;发现结果预先携带 schema;若特定高频工具错误率仍高,后续变更可引入"pinned 原生适配器"(如手写 webSearchPrime 工具),属预留逃生门。
- [首跳延迟:惰性连接使首次 discover/call 承担建连成本] → trpc toolset 会话复用,后续调用无感;可接受。
- [热同步竞态:调用进行中 server 被删/重建,in-flight 调用可能失败] → 注册表互斥保护 map 一致性;in-flight 失败以工具错误渗透,模型重试即命中新实例;不做调用期引用计数(复杂度不值)。
- [zhipu key 体系混淆:web-search-prime 需 GLM Coding Plan 专属 key,与 `ZAI_API_KEY` 不通用] → 示例用独立 env 名 `ZHIPU_CODING_PLAN_API_KEY` 并注释说明。
- [`webSearchPrime` 入参 schema 未在官方文档全文公开] → 实施期先经 `mcp_discover` 实测 InputSchema,再定 prompt 指引中的参数示例;prompt 热载可随时修正。
- [新增传递依赖 trpc-mcp-go 及其间接依赖] → 与 trpc-agent-go v1.10.0 锁定 v0.0.10,`go mod tidy` 审查 diff。
- [三跳延迟(主→knowledge 发现→主→action 执行)] → 哲学代价,已知接受;二次使用起发现结果已在历史,跳数下降;高频核心工具走 prompt pinned 指引直调(knowledge 持 mcp_call 即一跳)。

## Migration Plan

1. 纯增量落地:注册表/网关/发现改造全部为新增或内部改造,无 BREAKING;`mcp_servers` 段缺省时行为与现状一致(空注册表 → discover 空结果、mcp_call 报无 server)。
2. 示例切换独立提交:YAML + prompt 变更与框架代码分离,回退 = 恢复 YAML 中 `web_search` 一行(工厂仍注册)。
3. 回滚策略:框架层无状态迁移,revert 即回滚;注册表 Close 幂等,不残留连接。

## Open Questions

- wechat-bot 是否将 exec 从主 agent 收回、复位 action agent 为唯一执行中枢(原始设计形态)——独立编排变更,不阻塞本变更。
- `mcp_call` 是否需要流式(StreamableCall)支持——trpc mcpTool 当前仅 Call,首版不做,观察需求。
- 高频 MCP 工具的"pinned 原生适配器"是否值得引入——待示例运行后依据 args 错误率决定。
