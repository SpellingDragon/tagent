## REMOVED Requirements

### Requirement: Deprecated KnowledgeResult and ExecutionPlan types
**Reason**: `tool/accessor.go` 中的 `KnowledgeResult` 和 `ExecutionPlan` 类型已被 `tool/knowledge/knowledge_subtools.go` 中的同定义取代。knowledge 子包自维护类型，不再依赖公共 accessor 层定义。
**Migration**: 无。这些类型零外部引用，直接删除即可。

### Requirement: Deprecated Recall types (Query/Response/Event/EventDetail)
**Reason**: `recall/recall_subtools.go` 已使用独立定义的 `recallQueryArgs`、`recallQueryResult` 等类型替代这些旧类型。
**Migration**: 无。这些类型零外部引用。

### Requirement: Unused extractKeywords function and stopWords map
**Reason**: `extractKeywords` 和 `stopWords` 仅被 `tool_test.go` 中自测死函数的测试用例引用。无生产代码消费方。
**Migration**: 无。随函数一同移除测试即可。

## ADDED Requirements

### Requirement: tool/accessor.go shall contain only consumed interfaces
`tool/accessor.go` SHALL 仅包含被至少一个外部包实际引用（import + use）的导出符号。

#### Scenario: MemoryStoreAccessor remains accessible
- **WHEN** knowledge 或 recall 包导入 `github.com/SpellingDragon/tagent/tool` 并使用 `tool.MemoryStoreAccessor`
- **THEN** 编译成功，接口方法 `QueryEvents` 和 `GetEvent` 可正常调用

#### Scenario: SkillRepository remains accessible
- **WHEN** knowledge 包或 tagent 根包导入并使用 `tool.SkillRepository`
- **THEN** 编译成功，接口方法 `Summaries` 和 `Get` 可正常调用

#### Scenario: Dead types are no longer accessible
- **WHEN** 任何包尝试引用 `tool.KnowledgeResult`、`tool.RecallQuery` 等已移除类型
- **THEN** 编译报错 `undefined: tool.<TypeName>`
