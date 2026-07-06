## Why

代码评审中发现 6 个逻辑缺陷，涉及压缩降级、测试覆盖、配置兼容性、新模块验证、日志级别和性能。这些缺陷若不修复，会在生产环境中导致：LLM 丢失压缩上下文、未经测试的新代码上生产、旧配置无法迁移、web 搜索无安全边界。

## What Changes

- **BREAKING** SmartCompress Stage 2 LLM 失败时恢复 fallback notice，避免 LLM 完全不知道有历史被丢弃
- AgentToolWrapper 补充独立单元测试，覆盖 event_key 解析、外部事件注入、InputSchema 声明
- 旧 YAML 格式失效，提供迁移说明和 schema 校验
- web_search 新模块补充超时、大小限制等安全边界及基础测试
- MemoryPlugin 存储日志从 `Infof` 恢复为 `Debugf`，减少生产日志量
- `getCompressedEventKeys` O(n²) 优化为 O(n)，避免大 Session 下 BeforeModel 延迟

## Capabilities

### New Capabilities
- `compress-fallback-notice`: SmartCompress Stage 2 LLM 失败时恢复上下文提示
- `tool-agent-wrapper-tests`: AgentToolWrapper 独立单元测试
- `config-yaml-migration`: YAML 格式迁移指南和 schema 校验
- `websearch-safeguards`: web_search 工具超时、大小限制等安全边界及测试
- `plugin-log-restore`: MemoryPlugin 存储日志恢复为 Debug 级别
- `event-key-perf`: getCompressedEventKeys O(n²)→O(n) 优化

### Modified Capabilities
<!-- None - these are all new capabilities fixing logical defects -->

## Impact

| 文件 | 影响 |
|------|------|
| `agent/smart_compress.go` | 恢复 fallback notice 逻辑 |
| `agent/context_intervention.go` | getCompressedEventKeys 性能优化 |
| `agent/tool_agent.go` | 无变更（补充测试文件） |
| `plugin/memory_plugin.go` | Infof→Debugf |
| `tool/websearch/search.go` | 添加安全边界 |
| `config.go` / `tagent.go` | 无代码变更（补充迁移文档） |
| 新增测试文件 | tool_agent_test.go, websearch_test.go |
| 新增文档 | docs/config-migration.md |
