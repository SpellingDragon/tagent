## Context

代码评审发现 6 个逻辑缺陷，覆盖 3 个层面：

| 层面 | 缺陷数 | 典型问题 |
|------|--------|---------|
| 正确性 | 2 | compress 降级逻辑缺失、web_search 无安全边界 |
| 可维护性 | 2 | AgentToolWrapper 无测试、旧 YAML 无迁移路径 |
| 性能/可观测性 | 2 | O(n²) 匹配、Infof 噪音 |

当前代码已可运行（测试全部通过），但缺陷在异常路径和安全边界上，属于"静默风险"。

## Goals / Non-Goals

**Goals:**
- 修复 SmartCompress Stage 2 LLM 失败时的降级逻辑
- 为 AgentToolWrapper 建立测试基础
- 提供 YAML 配置迁移指南
- 为 web_search 添加超时/大小限制
- 恢复 MemoryPlugin 合理日志级别
- 优化 getCompressedEventKeys 时间复杂度

**Non-Goals:**
- 不重构 SmartCompress 整体架构
- 不修改 AgentToolWrapper 的 API 设计
- 不实现 YAML 自动转换工具（仅提供文档）
- 不修改 web_search 搜索引擎选择逻辑

## Decisions

### 决策 1：Compress Fallback — 恢复 `compressNotice` 但使用 `[Compressed]` 前缀

**选择**：Stage 2 LLM 失败时，返回 `[Compressed: N earlier tasks omitted. Full context available via recall agent.]`

**备选方案**：
- 返回空字符串 → 丢失所有压缩感知 ❌
- 只回退到 Stage 1（token 标记）→ LLM 无上下文理解 ❌

**理由**：保留 fallback 确保 LLM 知道有信息被省略，但不恢复旧的"对话历史摘要"语义（该语义在 Stage 2 成功时由 LLM 生成）。`[Compressed]` 前缀与 `[System]`、`[evt_xxx]` 语法一致。

### 决策 2：AgentToolWrapper 测试范围 — 单元测试 + 集成契约

**选择**：创建 `agent/tool_agent_test.go`，覆盖：
1. `Declaration()` 输出正确的 InputSchema（含 event_keys 参数）
2. `Call()` 正确解析 event_key → 获取 FullEvent → 调用 `IngestExternalEvents`
3. `Call()` 无 event_key 时正常执行（兼容模式）

**备选方案**：
- 仅依赖 `TestTagentAgent_SimpleLLMCall` 间接覆盖 → 覆盖路径不完整 ❌
- 端到端测试 → 太慢，依赖 LLM ❌

**理由**：使用 mock MemoryStore 模拟 event_key 解析，在单元测试层面覆盖核心路径。

### 决策 3：YAML 迁移 — 文档 + schema 标记，不做自动转换

**选择**：创建 `docs/config-migration.md`，包含：
- 新旧格式对比表
- 逐字段迁移示例
- `AgentConfig` 各字段说明

**理由**：当前无生产用户，迁移指南足以。自动转换器的 ROI 不高——YAML 结构变化大，转换逻辑容易出错。

### 决策 4：web_search 安全边界 — context 超时 + 响应 body 限制

**选择**：
1. 使用 `context.WithTimeout` 限制单次搜索 30s
2. HTML 抓取限制 body 大小 1MB
3. 创建 `tool/websearch/search_test.go` 基础测试

**备选方案**：
- 无限制 → 恶意 URL 可导致 OOM ❌
- 限制太严格（10KB）→ 搜索结果不完整 ❌

### 决策 5：日志级别 — Infof→Debugf，保留关键错误日志

**选择**：MemoryPlugin 存储事件从 `log.Infof` 改回 `log.Debugf`

**理由**：每次事件存储都打日志在高频场景下噪音过大。错误路径（`StoreEvent` 失败）仍然使用 `log.Errorf`。

### 决策 6：getCompressedEventKeys 性能 — 预建消息指纹 map

**选择**：遍历 oldMsgs 构建 `map[fingerprint]key`（O(n)），然后单次遍历 Session Events 匹配（O(m)）。总复杂度 O(n+m)。

当前实现：对每个 event 遍历全部 oldMsgs → O(n×m)。

**理由**：在大 Session（数千 events）下，O(n²) 会导致 BeforeModel 明显延迟。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| Fallback notice 格式不被 LLM 理解 | 使用 `[Compressed]` 前缀，与现有事件前缀风格一致 |
| AgentToolWrapper 测试依赖 mock 准确性 | mock 仅模拟 MemoryStore.GetEvent 行为，足够简单 |
| web_search 1MB 限制可能截断长页面 | 1MB 覆盖绝大多数网页 body 内容 |
| 性能优化引入 map 内存开销 | 仅构建一次 map，作用域在 BeforeModel 内 |
