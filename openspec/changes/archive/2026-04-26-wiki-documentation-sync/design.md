## 决策

### 决策 1：逐文件全量审查修正

**选择**：依次读取实际代码关键位置（types.go、plugin 实现、agent 实现、tool 实现），对照 wiki 逐段修正。

**备选方案**：仅修正已知错误点（sparse fix）
- 优点：变更量小
- 缺点：可能遗漏其他不一致处，文档信任度仍然受损

**选择理由**：wiki 作为 "single source of truth" 需要完整可信。逐文件审查确保一次性修复所有不一致。

### 决策 2：memory wiki 重写策略 — 保守重写 vs 参考重写

**选择**：**参考重写** — 保留 wiki 现有的节结构（13 个主要章节），只替换过时的数据结构和代码片段。

**备选方案**：完全重写
- 优点：结构可重新优化
- 缺点：风险大，可能引入新的不一致

**选择理由**：当前 wiki 的节结构合理，问题在于内容而非结构。保留结构 + 更新内容是最小风险的路径。

### 决策 3：行数表更新策略

**选择**：使用 `wc -l` 实际测量当前代码行数，替换所有 wiki 中的行数引用。

**理由**：行数是最容易过时的元数据，必须基于实际测量。

## 风险

| 风险 | 可能性 | 影响 | 缓解 |
|------|--------|------|------|
| 修正遗漏 | 中 | 中 | 逐文件对照代码审查，每节检查 |
| 引入新错误 | 低 | 低 | 纯文档修改，零代码影响；修改后人工校对 |
| 与 future 设计冲突 | 低 | 低 | 本次仅同步当前状态，不引入新设计 |

## 逐文件修正计划

### 1. memory-architecture.md

需要修正的节：

| 节 | 修正内容 |
|----|---------|
| §2 文件清单 | 更新 `types.go` 行数为实际值 |
| §3 组件关系图 | EK 节点: `evt_{ts}_{seq}` → `Snowflake int64`；MemoryStore map 类型 |
| §4.1 EventKey | 完全重写: string → Snowflake int64（含位布局图） |
| §4.2 FullEvent | EventKey int64、ParentKey int64、新增 PartitionID |
| §4.3 EventReference | EventKey int64、新增 PartitionID |
| §4.4 关系图 | MemoryStore: `map[key]FullEvent` → `map[partitionID]map[eventKey]FullEvent` |
| §5 因果链 | 示例值: `"evt_..."` → `1234567890123456` |
| §10+ 文件存储 | 目录结构: `dataDir/*` → `dataDir/{partition}/` |
| §13 EventKey 设计 | 全部重写为 Snowflake + PartitionID |

### 2. agent-architecture.md

| 节 | 修正内容 |
|----|---------|
| §2 文件清单 | 更新 `context_intervention.go` 行数 |
| §5 BeforeModel | Compress 签名增加 `inv` 参数，补充 Phase 1 视图转换 |
| §12.5 Snowflake 设计 | "当前...不含分区" → 标记为已实现 |

### 3. plugin-architecture.md

| 节 | 修正内容 |
|----|---------|
| §4 MemoryPlugin 结构体 | `lastEventKey string` → `lastEventKeys map[int]int64` |
| §5 OnEvent 流程 | EventKey 生成改为 `NewSnowflakeEventKey(partitionID, 0)` |
| §5 OnEvent 流程 | StateDelta 写入补充 `partition_id` |
| §6 并发安全 | 更新为分区级别锁描述 |

### 4. tool-architecture.md

| 节 | 修正内容 |
|----|---------|
| §3 组件关系 | RecallAgent 标注为 "完整 TagentAgent + 内部 React" |
| §§ Snowflake/PartitionID | "改进计划" → "已实现" |
| §§ event_key 注入 | "框架级自动注入" → "AgentToolWrapper 自解析" |
