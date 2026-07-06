## ADDED Requirements

### Requirement: web_search 工具设置搜索超时
web_search 工具 SHALL 对每次搜索请求使用 context 超时机制，防止恶意或响应缓慢的 URL 阻塞调用方。

#### Scenario: 搜索请求在超时内完成
- **WHEN** 发起 web 搜索请求
- **THEN** HTTP 请求 SHALL 使用 `context.WithTimeout` 设置 30 秒超时
- **AND** 超时未触发时正常返回搜索结果

#### Scenario: 搜索请求超时
- **WHEN** 目标 URL 在 30 秒内未响应
- **THEN** 搜索 SHALL 返回超时错误
- **AND** 不阻塞调用方超过 30 秒

### Requirement: web_search 工具限制响应 body 大小
web_search 工具 SHALL 限制 HTML 响应 body 的最大读取大小，防止大页面导致内存溢出。

#### Scenario: 响应 body 在限制内
- **WHEN** 目标页面 HTML body 小于 1MB
- **THEN** SHALL 正常读取完整 body 内容

#### Scenario: 响应 body 超出限制
- **WHEN** 目标页面 HTML body 超过 1MB
- **THEN** SHALL 截断 body 并在结果中标注 `[truncated at 1MB]`

### Requirement: web_search 提供基础单元测试
web_search 包 SHALL 包含 `search_test.go`，覆盖基本功能验证。

#### Scenario: 工具声明正确
- **WHEN** 调用 `websearch.NewTool().Declaration()`
- **THEN** 返回的 Name SHALL 为 `"web_search"`
- **AND** InputSchema SHALL 包含 `query` 必填参数
