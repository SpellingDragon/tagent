## 1. ToolRegistry 与注册基础设施

- [x] 1.1 在 `agent/tool_agent.go` 中新增 `ToolRegistry` struct，持有 `plainToolFactories` 和 `toolAgentFactories` 两个 map，使用 `sync.RWMutex` 保护
- [x] 1.2 实现 `ToolRegistry` 方法：`RegisterPlainTool(id, factory)`、`RegisterToolAgent(id, factory)`、`GetPlainToolFactory(id)`、`GetToolAgentFactory(id)`，重复注册时 panic
- [x] 1.3 在 `tagent` 包中实现 `GetRegistry()` 返回全局单例 `*agent.ToolRegistry`
- [x] 1.4 在 `tagent` 包中实现 `RegisterBuiltinTools() error`：调用 `GetRegistry()` 注册 exec + 调用 `knowledge.RegisterSubTools()` + `recall.RegisterSubTools()`（使用 sync.Once 保证幂等）
- [x] 1.5 在 `tagent` 包中实现 `ValidateToolAccess(cfg *Config) error`：遍历所有 agent 的 tools 列表，检查 `kind: tool` 的 id 是否在 registry 中已注册
- [x] 1.6 扩展 `agent.PlainToolFactoryConfig`：新增 `MemStore memory.MemoryStore`、`SkillRepo tool.SkillRepository`、`MCPToolSets []trpctool.ToolSet`、`ReadPartitionIDs []int` 字段
- [x] 1.7 验证：`go build ./agent/...` 通过

## 2. knowledge sub-tool 拆分为独立工厂

- [x] 2.1 在 `tool/knowledge/knowledge_subtools.go` 中为每个 sub-tool 创建独立工厂函数：`skillSearchFactory`、`skillLoadFactory`、`mcpDiscoverFactory`、`webSearchFactory`、`duckDuckGoFactory`、`memoryQueryFactory`
- [x] 2.2 每个工厂函数从 `PlainToolFactoryConfig` 提取所需依赖，缺失时返回 error（如 skill_search 需要 SkillRepo，memory_query 需要 MemStore）
- [x] 2.3 导出 `RegisterSubTools()` 函数（无参，避免循环依赖），注册 6 个 plain tool factory
- [x] 2.4 保留 `BuildSubTools(cfg Config)` 函数（向后兼容，NewAgent fallback 路径）
- [x] 2.5 验证：`go build ./tool/knowledge/...` 通过

## 3. recall sub-tool 拆分为独立工厂

- [x] 3.1 在 `tool/recall/recall_subtools.go` 中为每个 sub-tool 创建独立工厂函数：`recallQueryFactory`、`recallGetFactory`、`recallRecentFactory`、`recallTraceFactory`
- [x] 3.2 每个工厂函数从 `PlainToolFactoryConfig` 提取 MemStore 和 ReadPartitionIDs，缺失时返回 error
- [x] 3.3 导出 `RegisterSubTools()` 函数（无参，避免循环依赖），注册 4 个 plain tool factory
- [x] 3.4 保留 `buildRecallSubTools` 函数（向后兼容，NewAgent fallback 路径）
- [x] 3.5 验证：`go build ./tool/recall/...` 通过

## 4. knowledge/recall 改为 config-driven

- [x] 4.1 修改 `tool/knowledge/knowledge_agent.go` 的 `Config`：新增 `Tools []tool.Tool` 字段；`NewAgent` 优先使用 `cfg.Tools`，空时 fallback 到 `BuildSubTools`
- [x] 4.2 修改 `tool/recall/recall_agent.go` 的 `Config`：新增 `Tools []tool.Tool` 字段；`NewAgent` 优先使用 `cfg.Tools`，空时 fallback 到 `buildRecallSubTools`
- [x] 4.3 验证：`go build ./tool/knowledge/... ./tool/recall/...` 通过

## 5. builtin.go 重构

- [x] 5.1 移除 `knowledgeFactory`、`recallFactory` 函数
- [x] 5.2 移除 `agent.RegisterToolAgent("knowledge", ...)` 和 `agent.RegisterToolAgent("recall", ...)` 调用
- [x] 5.3 修复 `actionFactory`：适配 `action.NewActionTool(opts ...ActionToolOption)` 新签名，使用 `action.WithActionWorkspace` 等 option
- [x] 5.4 实现 `RegisterBuiltinTools()` 正确注册 exec + 调用 knowledge/recall 的 RegisterSubTools（registry.go）
- [x] 5.5 验证：`go build ./...` 通过（无编译错误）

## 6. config.go 补全与更新

- [x] 6.1 在 `AgentConfig` 中新增 `Meditation MeditationConfig` 和 `KeepRecentTasks int` 字段
- [x] 6.2 新增 `MeditationConfig` struct（config 层，string duration 字段）：`Enabled bool`、`Interval string`、`MinGap string`、`PromptFile string`
- [x] 6.3 更新 `DefaultConfig()`：knowledge agent 添加 6 个 ToolRef（kind=tool, id=skill_search 等）；recall agent 添加 4 个 ToolRef（kind=tool, id=recall_query 等）
- [x] 6.4 更新 `DefaultConfig()`：tagent agent 的 tools 列表中 action 保持 `kind: tool`（exec 类工具）
- [x] 6.5 验证：`go build ./...` 和 `go vet ./...` 通过

## 7. tagent.go 适配

- [x] 7.1 确保 `New()` 中调用 `RegisterBuiltinTools()` 和 `ValidateToolAccess(&cfg)`
- [x] 7.2 修改 `buildAgent`：使用 `registry.GetToolAgentFactory` / `registry.GetPlainToolFactory`
- [x] 7.3 修改 `buildPlainToolRef` 签名：新增 `rc *runtimeConfig`、`memStore memory.MemoryStore`、`readPartitionIDs []int` 参数
- [x] 7.4 修改 `buildPlainToolRef` 内部：将 `rc.skillRepo`、`rc.mcpToolSets`、`memStore`、`readPartitionIDs` 注入 `PlainToolFactoryConfig`
- [x] 7.5 修改 `buildAgent` 中调用 `buildPlainToolRef` 的位置：传入 `rc`、`memStore`、`readPartitionIDs`
- [x] 7.6 验证：`go build ./...` 和 `go vet ./...` 通过

## 8. action.NewActionTool 签名适配

- [x] 8.1 检查 `action.NewActionTool` 当前签名（`opts ...ActionToolOption`）和可用 option（`WithActionWorkspace`、`WithActionRunAsUser`、`WithActionRunAsGroup`）
- [x] 8.2 修复 `builtin.go` 的 `actionFactory`：使用 option 模式创建 ActionTool
- [x] 8.3 验证：`go build ./...` 通过

## 9. example YAML 配置更新

- [x] 9.1 更新 `examples/wechat-bot/tagent.yaml`：knowledge agent 添加 tools 列表（skill_search、skill_load、mcp_discover、web_search、duckduckgo_search、memory_query）；recall agent 添加 tools 列表（recall_query、recall_get、recall_recent、recall_trace）
- [x] 9.2 更新 `examples/wechat-bot/tagent.rl.yaml`：同步 tagent.yaml 的 knowledge/recall tools 声明
- [x] 9.3 验证：YAML 可被 `LoadConfig` 正确解析（结构匹配 Config schema）

## 10. 测试与验证

- [x] 10.1 编写 `ToolRegistry` 单元测试：注册、查询、幂等性、ValidateToolAccess
- [x] 10.2 编写 sub-tool 工厂测试：依赖注入通过 PlainToolFactoryConfig 传递
- [x] 10.3 更新 `config_test.go`：验证 DefaultConfig 中 knowledge/recall 的 tools 列表正确
- [x] 10.4 运行 `go test -run "TestValidate|TestDefaultConfig|TestToolRegistry|TestRegisterBuiltin|TestMeditationConfig" .` 全部通过
- [x] 10.5 运行 `go build ./...` 和 `go vet ./...` 全部通过
