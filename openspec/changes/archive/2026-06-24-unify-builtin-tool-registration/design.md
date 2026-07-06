## Context

tagent 的工具体系经历了多轮演进。当前状态：

- **exec**（原 action）：已注册为 plain tool，通过 `RegisterPlainTool("exec", ...)` 注册，config 中 `kind: tool, id: exec` 声明使用
- **knowledge/recall**：仍通过 `RegisterToolAgent` 注册工厂，工厂内部硬编码 sub-tools（`BuildSubTools()` / `buildRecallSubTools()`），无法通过配置灵活组合
- **action/read/write/speak/draw**：config-driven agent，通过 config tools 列表声明子工具
- **编译状态**：broken — 上一轮重构引入了 `RegisterBuiltinTools()`/`GetRegistry()`/`ValidateToolAccess()` 调用但未实现；`AgentConfig` 缺少 `Meditation`/`KeepRecentTasks` 字段；`action.NewActionTool` 签名已改为 `opts ...ActionToolOption`

核心矛盾：knowledge/recall 的 sub-tools 需要 **运行时依赖**（MemStore、SkillRepo、MCPToolSets、ReadPartitionIDs），而当前 `PlainToolFactoryConfig` 只携带 `Properties map[string]any`（序列化配置），无法传递这些运行时对象。

## Goals / Non-Goals

**Goals:**
- 将 knowledge/recall 的 10 个 sub-tools 注册为 plain tool，与 exec 统一
- 移除 knowledge/recall 工厂，使其变为纯 config-driven agent
- 扩展 `PlainToolFactoryConfig` 支持运行时依赖注入
- 修复所有编译错误，恢复 `go build ./...` 通过
- 建立清晰的三阶段工具生命周期

**Non-Goals:**
- 不改变 sub-tool 的内部实现逻辑（搜索算法、查询逻辑等保持不变）
- 不引入新的 sub-tool（仅重组现有工具的注册方式）
- 不改变 agent 间通信机制（AgentToolWrapper、event_key 解析等不变）
- 不修改 memory 存储层

## Decisions

### D1: ToolRegistry 作为实例（非全局 map）

**选择**: 引入 `ToolRegistry` struct，持有 `plainToolFactories` 和 `toolAgentFactories` 两个 map。`GetRegistry()` 返回单例实例。

**理由**: 当前 `agent/tool_agent.go` 中的全局 map（`toolAgentFactories`、`plainToolFactories`）缺乏封装，无法做 ValidateToolAccess。ToolRegistry 提供统一入口，支持注册、查询、验证。

**替代方案**: 保持全局 map + 添加独立验证函数。放弃 — 分散职责，难以测试。

### D2: PlainToolFactoryConfig 扩展运行时依赖字段

**选择**: 在 `PlainToolFactoryConfig` 中新增 4 个可选字段：

```go
type PlainToolFactoryConfig struct {
    ID          string
    Description string
    Properties  map[string]any

    // Runtime dependencies (optional, injected by buildAgent)
    MemStore         memory.MemoryStore    // for memory_query, recall_* tools
    SkillRepo        tool.SkillRepository  // for skill_search, skill_load
    MCPToolSets      []trpctool.ToolSet    // for mcp_discover
    ReadPartitionIDs []int                 // for recall_query, recall_recent
}
```

**理由**: sub-tool 需要运行时对象（MemStore、SkillRepo 等），这些无法序列化到 `Properties`。通过结构体字段传递比 context/interface 注入更明确、类型安全。

**替代方案**:
- (a) 用 `Properties` 传递 interface{} → 失去类型安全，需 type assert
- (b) 为每类 sub-tool 定义独立 factory 接口 → 过度设计，10 个工具 4 类依赖不需要 4 个接口

### D3: buildPlainToolRef 接收运行时依赖

**选择**: `buildPlainToolRef` 签名扩展，接收 agent 级别的运行时依赖：

```go
func buildPlainToolRef(
    tr ToolRef, desc string,
    rc *runtimeConfig,
    memStore memory.MemoryStore,
    readPartitionIDs []int,
) (trpctool.Tool, bool, error)
```

`buildAgent` 在遍历 `acfg.Tools` 时，将 agent 的 MemStore、ReadPartitionIDs 和 `rc.skillRepo`、`rc.mcpToolSets` 传入。

**理由**: 运行时依赖在 `buildAgent` 时才确定（MemStore 刚创建、ReadPartitionIDs 从 config 解析），必须在构建工具时注入。

### D4: sub-tool 工厂函数模式

**选择**: 每个 sub-tool 在各自包中导出 `RegisterXxxTool(registry *ToolRegistry)` 函数：

```go
// tool/knowledge/knowledge_subtools.go
func RegisterSubTools(registry *agent.ToolRegistry) {
    registry.RegisterPlainTool("skill_search", skillSearchFactory)
    registry.RegisterPlainTool("skill_load", skillLoadFactory)
    // ...
}
```

每个工厂函数从 `PlainToolFactoryConfig` 提取所需依赖，如果依赖缺失则返回 error。

**理由**: 工厂函数保持简单（一个函数创建一个工具），依赖检查在工厂内部完成，调用方不需要知道每个工具需要什么依赖。

### D5: knowledge/recall 移除工厂，变为 config-driven

**选择**:
- 移除 `builtin.go` 中的 `knowledgeFactory`、`recallFactory`
- 移除 `agent.RegisterToolAgent("knowledge", ...)` 和 `agent.RegisterToolAgent("recall", ...)`
- `DefaultConfig` 中 knowledge/recall agent 显式声明 tools 列表
- `knowledge.NewAgent` / `recall.NewAgent` 改为接收外部传入的 `Tools []tool.Tool`（不再内部调用 `BuildSubTools`）

**理由**: 统一架构 — 所有 agent 都是 config-driven，没有特殊路径。`buildAgent` 中的 factory 检查分支可以保留用于未来扩展，但内置 agent 不再使用。

**替代方案**: 保留 factory 但让 factory 从 config 读取 tools 列表 → 增加复杂度，不如直接 config-driven。

### D6: RegisterBuiltinTools 统一注册入口

**选择**: `RegisterBuiltinTools()` 在 `builtin.go` 中实现，调用各 tool 包的 `RegisterSubTools()` + 注册 exec：

```go
func RegisterBuiltinTools() error {
    registry := GetRegistry()
    registry.RegisterPlainTool("exec", actionFactory)
    knowledge.RegisterSubTools(registry)
    recall.RegisterSubTools(registry)
    return nil
}
```

**理由**: 单一入口，`tagent.New()` 调用一次即可完成所有注册。各 tool 包负责自己的注册逻辑，`builtin.go` 只做编排。

### D7: ValidateToolAccess 验证 config 引用的 tool 已注册

**选择**: `ValidateToolAccess(cfg *Config)` 遍历所有 agent 的 tools 列表，检查 `kind: tool` 的 `id` 是否在 registry 中已注册。

**理由**: 在 `New()` 中提前失败，而不是在 `buildAgent` 时才发现未注册的 tool。

## Risks / Trade-offs

- **[风险] knowledge/recall NewAgent 的 Config.Tools 字段变为必填** → `NewAgent` 不再内部构建 sub-tools，调用方必须显式传入。但 config-driven 路径（`buildAgent`）会自动处理，仅影响直接调用 `NewAgent` 的代码。

- **[风险] sub-tool 工厂依赖缺失时如何处理** → 工厂返回 error（如 "skill_search requires SkillRepo"）。config 中声明了 tool 但运行时未提供依赖时，`buildAgent` 会返回清晰错误信息。

- **[权衡] PlainToolFactoryConfig 变胖** → 新增 4 个字段，但都是可选的（指针/slice 零值为 nil）。大多数 plain tool（如 exec）不使用这些字段，无影响。

- **[风险] 全局 registry 单例的并发安全** → ToolRegistry 内部使用 `sync.RWMutex` 保护 map，与当前 `agent/tool_agent.go` 的模式一致。

- **[权衡] knowledge.NewAgent/recall.NewAgent 的向后兼容** → `BuildSubTools` 函数保留但不再被 `NewAgent` 内部调用。外部代码如果直接调用 `BuildSubTools` 仍可工作，但推荐通过 config 驱动。
