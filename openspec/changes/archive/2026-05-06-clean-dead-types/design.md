## Context

`tool/accessor.go` 定位为跨模块的公共抽象接口层，当前却混入了各子包已独立定义的死类型。`knowledge/` 和 `recall/` 通过内聚重构，已将各自的请求/响应类型移入子包内，但 `accessor.go` 的原定义未被同步清理。

## Goals / Non-Goals

**Goals:**
- 移除 `tool/accessor.go` 中所有死类型和死函数，使其回归为纯粹的接口定义文件
- 同步移除 `tool/tool_test.go` 中针对死函数（`extractKeywords`）的测试用例
- 保证 `go build ./...` 和 `go test ./...` 全部通过

**Non-Goals:**
- 不修改任何保留的接口签名（`MemoryStoreAccessor`、`SkillRepository`）
- 不移动或重命名任何文件
- 不改变任何功能行为

## Decisions

1. **直接删除，不保留 deprecated 标记**：这些类型已完全无引用（grep 验证零外部消费），保留 deprecated 注释只会增加噪音。受影响类型：
   - `KnowledgeResult` / `ExecutionPlan`：`knowledge/knowledge_subtools.go` 有独立同定义
   - `RecallQuery` / `RecallResponse` / `RecallEvent` / `RecallEventDetail`：`recall/recall_subtools.go` 有独立定义
   - `extractKeywords` / `stopWords`：仅 `tool_test.go` 引用，移除死函数后相应测试一并删除

2. **保留 `// ==================== Knowledge Types ====================` 等注释分隔符**，将其调整为接口说明而非类型定义区域，保持文件结构清晰。

3. **测试同步清理**：`tool_test.go` 中的 `TestExtractKeywords` 系列测试随函数移除一并删除。

## Risks / Trade-offs

- [Risk] 外部项目直接引用 `tool.KnowledgeResult` 等死类型 → 零风险：grep 确认这些符号仅有定义处引用，零外部消费
- [Risk] 文档引用过时符号 → 低风险：`docs/wiki/` 中的引述均为架构说明性质，无需同步修正
