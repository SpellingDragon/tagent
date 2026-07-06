## 1. Compress Fallback Notice (compress-fallback-notice)

- [x] 1.1 在 `agent/smart_compress.go` 中恢复 `compressNotice` 函数，使用 `[Compressed]` 前缀格式
- [x] 1.2 修改 `generateSummary`：LLM 调用失败时调用 `compressNotice` 而非返回 `""`
- [x] 1.3 修改 `generateSummary`：LLM 返回空摘要时调用 `compressNotice`（简化版）
- [x] 1.4 更新 `agent/smart_compress_test.go` 覆盖 fallback 场景

## 2. AgentToolWrapper Tests (tool-agent-wrapper-tests)

- [x] 2.1 创建 `agent/tool_agent_test.go`
- [x] 2.2 测试 `Declaration` 包含 `event_keys` 参数（`eventParams` 含 `"event_key"` 时）
- [x] 2.3 测试 `Declaration` 不含 `event_keys`（`eventParams` 为空时）
- [x] 2.4 测试 `Call` 解析 `event_keys` → 从 mock MemoryStore 获取 FullEvent → 注入子 agent
- [x] 2.5 测试 `Call` 不存在的 event_key 被跳过、无错误
- [x] 2.6 测试 `Call` 无 `event_keys` 时正常执行

## 3. YAML Migration Doc (config-yaml-migration)

- [x] 3.1 创建 `docs/config-migration.md`
- [x] 3.2 包含新旧 YAML 格式完整示例对比
- [x] 3.3 包含 `AgentConfig` 所有字段说明表（字段名、类型、默认值、描述）

## 4. Web Search Safeguards (websearch-safeguards)

- [x] 4.1 在 `tool/websearch/search.go` 中添加 `context.WithTimeout(30s)` 超时控制
- [x] 4.2 添加 HTTP 响应 body 读取 1MB 限制（`io.LimitReader`）
- [x] 4.3 超限时截断并添加 `[truncated at 1MB]` 标注
- [x] 4.4 创建 `tool/websearch/search_test.go`，覆盖 `Declaration` 基础场景

## 5. Plugin Log Restore (plugin-log-restore)

- [x] 5.1 将 `plugin/memory_plugin.go` 存储成功日志从 `log.Infof` 改回 `log.Debugf`
- [x] 5.2 确认存储失败日志仍使用 `log.Errorf`

## 6. Event Key Perf (event-key-perf)

- [x] 6.1 重构 `agent/smart_compress.go` 中 `collectCompressedKeys`：使用 `map[fingerprint]bool` 预建指纹
- [x] 6.2 移除内层 O(n×m) 匹配循环
- [x] 6.3 确保 `seen` set 去重逻辑正确

## 7. Final Verification

- [x] 7.1 运行 `go test ./... -count=1` 确认全部通过
- [x] 7.2 运行 `go build ./...` 确认编译通过
