## Context

`unify-builtin-tool-registration` 变更将内置工具注册从 "factory-per-agent" 改为 "统一 plain tool 注册 + config-driven agent 构建"。变更完成后，code review 发现了以下遗留问题：

1. `builtin.go` 中 `decodeProperties[T]` 函数未被使用，且签名与 `ToolRef.Properties` 类型不匹配
2. `actionFactory` 的 properties 字段名从 PascalCase（`Workspace`/`RunAsUser`/`RunAsGroup`）改为 snake_case（`work_dir`/`run_as_user`/`run_as_group`），但未更新文档
3. `buildAgent` 仍保留 `ToolAgentFactory` 检查路径，可能静默覆盖 config-driven 路径
4. read/write/speak/draw agent 的 `NewTool` 便利函数传入 `nil` 作为 `parentMemStore`
5. 测试覆盖有缺口，`go test ./...` 超时（180s）

## Goals / Non-Goals

**Goals:**

- 移除所有死代码，保持代码简洁
- 保护内置 agent name 不被 ToolAgentFactory 覆盖
- 修复或删除有 nil pointer 风险的便利函数
- 补充关键测试覆盖，确保 `go test ./...` 在合理超时内通过
- 补充 action properties 字段名文档

**Non-Goals:**

- 不考虑 backward compatibility（用户明确说不需要）
- 不改变 config-driven 架构本身
- 不引入新的 capability

## Decisions

### 1. 移除 `decodeProperties[T]` 函数

**决策**：直接删除，不保留。

**理由**：
- 全仓库无调用点
- 签名是 `json.RawMessage`，但 `ToolRef.Properties` 是 `map[string]any`，类型不匹配
- `actionFactory` 已经使用直接 map 类型断言，更简捷

### 2. actionFactory 的 properties 字段名

**决策**：保持当前 snake_case 字段名（`work_dir`/`run_as_user`/`run_as_group`），补充文档说明。

**理由**：
- snake_case 与 YAML 风格一致
- 不考虑 backward compatibility
- 需要在 README 或 wiki 中明确这些字段名

### 3. 保护内置 agent name

**决策**：在 `buildAgent` 中检查 agent name，如果是内置 name（`knowledge`/`recall`/`action`/`read`/`write`/`speak`/`draw`），跳过 `ToolAgentFactory` 检查，强制走 config-driven 路径。

**理由**：
- 防止未来有人误注册 ToolAgentFactory 覆盖 config-driven 路径
- 内置 agent 应该始终走统一的 config-driven 路径
- 外部自定义 agent 仍可使用 ToolAgentFactory

**实现**：

```go
// 内置 agent name 集合
var builtinAgentNames = map[string]bool{
    "knowledge": true, "recall": true, "action": true,
    "read": true, "write": true, "speak": true, "draw": true,
}

// buildAgent 中
if !builtinAgentNames[name] {
    if factory, ok := registry.GetToolAgentFactory(name); ok {
        // ToolAgentFactory 路径
        ...
    }
}
// config-driven 路径
...
```

### 4. read/write/speak/draw 的 `NewTool` 便利函数

**决策**：直接删除这些未使用的便利函数。

**理由**：
- 这些函数传入 `nil` 作为 `parentMemStore`，在有 `event_keys` 参数时会 panic
- config-driven 路径不需要它们（`buildAgentToolRef` 会正确传入 `parentMemStore`）
- 外部注册场景极少，保留只会增加维护负担
- 如果未来需要外部注册，可以显式传入 `parentMemStore`

### 5. 测试超时

**决策**：
- 先定位超时原因（哪个测试包挂起）
- 如果是特定测试包（如 tmux 相关），延长该包的超时或修复挂起
- 如果无法快速修复，使用 `go test -timeout 10m ./...` 延长全局超时

**理由**：
- 180s 超时太短，某些集成测试可能需要更长时间
- 但也不应该太长，避免 CI 时间过长

## Risks / Trade-offs

**Risk 1**: 删除 `NewTool` 便利函数后，如果外部代码依赖这些函数，会编译失败。

**Mitigation**: 这些函数目前未在仓库内使用，且 config-driven 路径不需要它们。如果外部有依赖，可以后续显式传入 `parentMemStore` 重新添加。

**Risk 2**: 保护内置 agent name 后，如果用户有合理的理由想覆盖内置 agent 行为，无法通过 ToolAgentFactory 实现。

**Mitigation**: 用户可以通过修改 `AgentConfig.Tools` 列表来改变 agent 使用的工具，这是更灵活的方式。如果确实需要覆盖 agent 创建逻辑，可以 fork 或提 issue。

**Risk 3**: 延长测试超时可能导致 CI 时间变长。

**Mitigation**: 先定位挂起测试，尽量修复而非简单延长超时。如果某些测试确实需要长时间（如 tmux session 监控），可以标记为 `//go:build integration` 并在 CI 中单独运行。
