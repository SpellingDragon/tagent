## 1. Memory Wiki 同步 (wiki-memory-sync)

- [x] 1.1 更新 §2 文件清单表 — `types.go` 行数更新为实际值
- [x] 1.2 重写 §3 组件关系图 — EK 节点标注 Snowflake int64；MemoryStore 类型修正
- [x] 1.3 重写 §4.1 EventKey — string → Snowflake int64（含位布局、NewSnowflakeEventKey 签名）
- [x] 1.4 更新 §4.2 FullEvent — EventKey int64、ParentKey int64、新增 PartitionID
- [x] 1.5 更新 §4.3 EventReference — EventKey int64、新增 PartitionID
- [x] 1.6 更新 §4.4 关系图与对比表 — MemoryStore: `map[partitionID]map[eventKey]`、对比表增加 PartitionID
- [x] 1.7 重写 §5 因果链 — 示例值使用 int64 数字格式、ParentKey 零值为 0
- [x] 1.8 更新文件存储节 — 目录结构 `dataDir/{partition}/` + atomic 写描述
- [x] 1.9 重写 EventKey 设计节（§13 区域）— 完全替换为 Snowflake + PartitionID + PartitionIDFromName

## 2. Agent Wiki 同步 (wiki-agent-sync)

- [x] 2.1 更新 §2 文件清单 — 更新 `context_intervention.go` 等文件行数
- [x] 2.2 修正 §5 BeforeModel 流程 — Compress 签名增加 `inv` 参数
- [x] 2.3 补充 Phase 1 事件视图转换描述 — applyEventView / extractEventInfo / [evt_xxx|type] 前缀
- [x] 2.4 更新 §12.5 Snowflake 设计 — "未来设计" → "已实现"

## 3. Plugin Wiki 同步 (wiki-plugin-sync)

- [x] 3.1 更新 §4 MemoryPlugin 结构体 — `lastEventKey string` → `lastEventKeys map[int]int64`
- [x] 3.2 更新 §5 OnEvent 流程 — EventKey 生成改为 Snowflake、parentKey 获取按分区
- [x] 3.3 更新 §5 OnEvent 流程 — StateDelta 写入补充 `partition_id`
- [x] 3.4 更新 §6 并发安全 — 分区级别锁描述

## 4. Tool Wiki 同步 (wiki-tool-sync)

- [x] 4.1 更新 Snowflake/PartitionID 段落 — "改进计划" → "已实现"
- [x] 4.2 更新 §3 RecallAgent 描述 — "完整 TagentAgent + 内部 React 循环"
- [x] 4.3 更新 event_key 注入描述 — "AgentToolWrapper 自解析" 替代 "框架级自动注入"

## 5. 最终验证

- [x] 5.1 人工校对 4 个 wiki 文件的关键节
- [x] 5.2 运行 `go build ./...` 确认无编译影响（零代码变更）✅
