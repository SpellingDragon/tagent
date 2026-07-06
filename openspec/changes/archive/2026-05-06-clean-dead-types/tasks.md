## 1. 移除 tool/accessor.go 中的死代码

- [x] 1.1 移除 `KnowledgeResult` 和 `ExecutionPlan` 类型定义（约 20 行）
- [x] 1.2 移除 `RecallQuery`、`RecallResponse`、`RecallEvent`、`RecallEventDetail` 类型定义（约 30 行）
- [x] 1.3 移除 `extractKeywords` 函数和 `stopWords` 变量（约 33 行）
- [x] 1.4 调整保留的 `// ====` 分隔注释，反映仅剩接口的现实

## 2. 移除 tool/tool_test.go 中的死测试

- [x] 2.1 移除 `TestExtractKeywords` 及相关的 `extractKeywords`/`stopWords` 测试用例

## 3. 验证

- [x] 3.1 `go build ./...` 通过
- [x] 3.2 `go test ./...` 通过
