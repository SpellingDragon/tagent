## 1. 摘要 prompt 优化

- [x] 1.1 修改 `agent/smart_compress.go` 中 `generateSummary` 的 prompt 第 2 条：从"省略工具调用的原始输出和中间过程细节"改为"保留工具调用的成功/失败状态和关键返回值"

## 2. 结构化执行状态摘要

- [x] 2.1 在 `agent/smart_compress.go` 中新增 `extractExecutionState(segments []*TaskSegment) string` 函数：从 old segments 中提取工具调用名称、参数摘要和结果状态
- [x] 2.2 在 `Compress` 方法中，LLM 摘要之后、recent segments 之前追加执行状态 system message
- [x] 2.3 实现截断策略：每条工具结果截断 100 chars，总执行状态控制在 500 chars 以内

## 3. 验证与测试

- [x] 3.1 运行 `go build ./...` 确保编译通过
- [x] 3.2 运行 `go test ./... -short -timeout 60s` 确保所有测试通过
## 1. 摘要 prompt 优化

- [x] 1.1 修改 `agent/smart_compress.go` 中 `generateSummary` 的 prompt 第 2 条：从"省略工具调用的原始输出和中间过程细节"改为"保留工具调用的成功/失败状态和关键返回值"

## 2. 结构化执行状态摘要

- [x] 2.1 在 `agent/smart_compress.go` 中新增 `extractExecutionState(segments []*TaskSegment) string` 函数：从 old segments 中提取工具调用名称、参数摘要和结果状态
- [x] 2.2 在 `Compress` 方法中，LLM 摘要之后、recent segments 之前追加执行状态 system message
- [x] 2.3 实现截断策略：每条工具结果截断 100 chars，总执行状态控制在 500 chars 以内

## 3. 验证与测试

- [x] 3.1 运行 `go build ./...` 确保编译通过
- [x] 3.2 运行 `go test ./... -short -timeout 60s` 确保所有测试通过
## 1. 摘要 prompt 优化

- [ ] 1.1 修改 `agent/smart_compress.go` 中 `generateSummary` 的 prompt 第 2 条：从"省略工具调用的原始输出和中间过程细节"改为"保留工具调用的成功/失败状态和关键返回值"

## 2. 结构化执行状态摘要

- [ ] 2.1 在 `agent/smart_compress.go` 中新增 `extractExecutionState(segments []*TaskSegment) string` 函数：从 old segments 中提取工具调用名称、参数摘要和结果状态
- [ ] 2.2 在 `Compress` 方法中，LLM 摘要之后、recent segments 之前追加执行状态 system message
- [ ] 2.3 实现截断策略：每条工具结果截断 100 chars，总执行状态控制在 500 chars 以内

## 3. 验证与测试

- [ ] 3.1 运行 `go build ./...` 确保编译通过
- [ ] 3.2 运行 `go test ./... -short -timeout 60s` 确保所有测试通过
