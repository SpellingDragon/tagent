## Why

`tool/accessor.go` 包含 64 行死代码——这些类型在早期设计迭代中定义，随后被各子包独立定义所取代，但原定义未被移除。这些死代码造成误导：开发者在 `accessor.go` 中看到已废弃的类型定义，可能误以为它们是当前使用的 contract。清理它们使 `accessor.go` 回归其本质角色——纯接口定义层。

## What Changes

- **移除** `tool/accessor.go` 中的 `KnowledgeResult`、`ExecutionPlan` 结构体（`knowledge/knowledge_subtools.go` 有独立定义）
- **移除** `tool/accessor.go` 中的 `RecallQuery`、`RecallResponse`、`RecallEvent`、`RecallEventDetail` 结构体（`recall/recall_subtools.go` 有独立定义）
- **移除** `tool/accessor.go` 中的 `extractKeywords` 函数及 `stopWords` 变量（无任何生产代码引用）
- 保留 `MemoryStoreAccessor`、`SkillRepository` 接口——这些是实际被消费的 contract

## Capabilities

### New Capabilities

无。本次变更为纯死代码清理，不引入新功能。

### Modified Capabilities

无。不修改任何现有 spec 级别的行为。

## Impact

- 影响的代码文件：`tool/accessor.go`（减 ~64 行）、`tool/tool_test.go`（移除针对死函数的测试）
- 无 API 变更，无依赖变更，无对外影响
- 编译兼容：所有现有 `import "github.com/SpellingDragon/tagent/tool"` 继续工作（保留的接口未被改变）
