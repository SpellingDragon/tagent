## Why

`unify-builtin-tool-registration` 变更完成后，code review 发现了几个需要清理的遗留问题：死代码（`decodeProperties` 函数）、`actionFactory` 的 properties 字段静默重命名、`ToolAgentFactory` 路径对内置 agent name 的覆盖风险、read/write/speak/draw agent 的 `NewTool` 便利函数传入 nil `parentMemStore`、测试覆盖缺口、以及 `go test ./...` 超时问题。这些问题需要在不引入 backward compatibility 负担的前提下快速修复。

## What Changes

- **移除死代码**：删除 `builtin.go` 中未使用的 `decodeProperties[T]` 函数（签名与 `ToolRef.Properties` 类型不匹配）
- **修复 `actionFactory` properties 处理**：旧代码使用 `action.ActionProperties` 结构体（字段名 `Workspace`/`RunAsUser`/`RunAsGroup`），新代码直接对 `map[string]any` 做类型断言，字段名改为 `work_dir`/`run_as_user`/`run_as_group`。**BREAKING**：恢复使用 `action.ActionProperties` 结构体或保持当前 snake_case 字段名但补充文档说明
- **保护内置 agent name**：在 `buildAgent` 中对内置 agent name（`knowledge`/`recall`/`action`/`read`/`write`/`speak`/`draw`）禁止通过 `ToolAgentFactory` 覆盖，防止静默覆盖 config-driven 路径
- **修复 read/write/speak/draw 的 `NewTool` nil pointer 风险**：这些便利函数传入 `nil` 作为 `parentMemStore`，在有 `event_keys` 参数时会 panic。要么要求传入 `parentMemStore`，要么直接删除这些未使用的便利函数
- **补充测试覆盖**：
  - `actionFactory` 的 properties 处理测试
  - `MeditationManager` 行为测试（MinGap、Interval、Stop/Start）
  - `buildPlainToolRef` 的依赖注入测试（MemStore/SkillRepo/ReadPartitionIDs 传递）
  - 新 agent（read/write/speak/draw）的构建测试
  - 完整 `tagent.New()` 集成测试（验证 knowledge/recall 作为子 agent 运行）
- **修复测试超时**：定位 `go test ./...` 超时原因（180s），延长测试超时或修复挂起测试

## Capabilities

### New Capabilities

（无新增 capability）

### Modified Capabilities

- `unified-tool-registry`：加固 `buildAgent` 对内置 agent name 的保护规则，确保 config-driven 路径不被 ToolAgentFactory 覆盖

## Impact

- **代码变更**：`builtin.go`（删除死代码）、`tagent.go`（buildAgent 保护）、`tool/read/write/speak/draw`（NewTool 修复或删除）、`action/action_tool.go`（properties 处理）
- **测试变更**：新增多个测试文件，可能调整现有测试超时
- **文档变更**：补充 action properties 字段名文档
- **API 兼容性**：本次变更不考虑 backward compatibility，直接采用最简方案
