## Why

`SmartCompressor` 的 `collectCompressedKeys` 方法存在严重 bug：Phase 1 事件视图转换已为消息添加 `[evt_<SnowflakeKey>|type]` 前缀，但 Phase 2 压缩时仍用**带前缀的 content** 去做内容指纹匹配 **不带前缀的 Session.Events**，指纹永远不命中，导致"被压缩事件 key 列表"始终为空。LLM 无法引用被压缩的历史事件。同时需评估压缩机整体存废——结论是**不可删除**，核心功能完好，仅事件 key 收集逻辑需修复。

## What Changes

- **修复** `collectCompressedKeys`：从前缀 `[evt_<KEY>|` 直接解析 Snowflake EventKey，替换永不命中的 content:role 指纹匹配
- **清理** 不再需要的 `buildEventMessageIndex` 及其调用链（仅此处使用，已无用）
- **评估后保留** SmartCompressor 全部阶段：Stage 1 段丢弃是 token 预算执行者，Stage 2 LLM 摘要是可选增值，均不可删除
- **不改动** 事件视图前缀格式、压缩触发阈值、Stage 2 摘要逻辑

## Capabilities

### New Capabilities

- `event-key-from-prefix`: 从消息前缀精确提取压缩段的 event_key，使 LLM 能通过 key 回溯被压缩事件

### Modified Capabilities

<!-- 无现有 spec 需要修改 -->

## Impact

- 受影响文件：`agent/smart_compress.go`（~50 行替换）、`agent/context_intervention.go`（~12 行清理 `buildEventMessageIndex`）
- 无 API 变更、无新依赖、无 breaking change
- 正向影响：LLM 在上下文压缩后能准确获取被压缩事件的 key 列表，RecallAgent 可通过 key 还原完整上下文
